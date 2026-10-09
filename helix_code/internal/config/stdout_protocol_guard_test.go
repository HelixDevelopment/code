// stdout_protocol_guard_test.go — standing regression guard (§11.4.115 /
// §11.4.135) for the "config diagnostics corrupt a machine protocol" defect.
//
// THE DEFECT (regression introduced by the cloud-gate wiring batch):
// config.Load() wrote its informational / warning lines to STDOUT
// (fmt.Println at config.go:471, :474 and the inert-key loop at :487). Two
// callers own stdout as a MACHINE PROTOCOL:
//
//   1. The subagent helper (internal/agent/subagent/helper_mode.go) writes its
//      SubagentResult JSON to stdout and the parent unmarshals the ENTIRE
//      buffer (subprocess_spawner.go — json.Unmarshal(bytes.TrimSpace(stdout))).
//      A prepended "INFO: Using config file: …" line makes every
//      subprocess-spawned subagent fail with "invalid helper output".
//   2. `helixcode acp` wires os.Stdout as the line-delimited ACP JSON-RPC
//      transport (cmd/cli/acp_cmd.go). A prepended line is a malformed frame.
//
// The fix routes those diagnostics to STDERR — same content, same conditions,
// different destination — so operators still see them.
//
// FALSIFYING MUTATION (§11.4.115 / §1.1): change any of the three
// fmt.Fprintln(os.Stderr, …) calls in config.Load() back to fmt.Println. The
// bytes then land on stdout, `stdout` below is non-empty, and
// TestLoad_EmitsNoBytesOnStdout FAILS. Restoring the fix makes it pass again.
// The paired stderr assertion is what stops the mutation "silence the line
// entirely" from passing: deleting the diagnostic empties stderr and the test
// FAILS on the second assertion.
package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// captureStdStreams runs fn with os.Stdout and os.Stderr each redirected to a
// private temp file and returns what each received. Temp files rather than
// os.Pipe: a pipe with no concurrent reader deadlocks once the 64 KiB kernel
// buffer fills, and this guard must never hang a test run.
func captureStdStreams(t *testing.T, fn func()) (stdout string, stderr string) {
	t.Helper()
	dir := t.TempDir()

	outFile, err := os.Create(filepath.Join(dir, "stdout.cap"))
	require.NoError(t, err)
	errFile, err := os.Create(filepath.Join(dir, "stderr.cap"))
	require.NoError(t, err)

	origOut, origErr := os.Stdout, os.Stderr
	os.Stdout, os.Stderr = outFile, errFile
	func() {
		defer func() {
			os.Stdout, os.Stderr = origOut, origErr
		}()
		fn()
	}()

	require.NoError(t, outFile.Close())
	require.NoError(t, errFile.Close())

	outBytes, err := os.ReadFile(filepath.Join(dir, "stdout.cap"))
	require.NoError(t, err)
	errBytes, err := os.ReadFile(filepath.Join(dir, "stderr.cap"))
	require.NoError(t, err)
	return string(outBytes), string(errBytes)
}

// TestLoad_EmitsNoBytesOnStdout is the root-cause guard: config.Load() must
// put ZERO bytes on stdout on every branch, while still telling the operator
// what it did on stderr.
func TestLoad_EmitsNoBytesOnStdout(t *testing.T) {
	t.Run("config_file_found", func(t *testing.T) {
		path := writeTestConfig(t, 8080)
		t.Setenv("HELIX_CONFIG", path)
		t.Setenv("HELIX_AUTH_JWT_SECRET", "")

		var cfg *Config
		var loadErr error
		stdout, stderr := captureStdStreams(t, func() {
			cfg, loadErr = Load()
		})

		require.NoError(t, loadErr)
		require.NotNil(t, cfg)

		assert.Equal(t, "", stdout,
			"config.Load() must write NOTHING to stdout — stdout is the subagent-helper "+
				"JSON and ACP JSON-RPC transport; got %q", stdout)
		// Content preserved, only the destination changed: the operator must
		// still be told which file was used.
		assert.Contains(t, stderr, path,
			"the 'using config file' diagnostic must still reach the operator on stderr")
	})

	t.Run("inert_key_warning", func(t *testing.T) {
		// `llm.timeout` is a registered inert key (strict.go inertConfigKeys),
		// so its presence fires the third print site — the inert-warning loop.
		dir := t.TempDir()
		path := filepath.Join(dir, "config.yaml")
		content := `version: "1.0.0"
application:
  name: "HelixCode"
  environment: "development"
server:
  address: "0.0.0.0"
  port: 8081
database:
  host: "localhost"
  port: 5432
  dbname: "helixcode"
  user: "helixcode"
auth:
  jwt_secret: "test-jwt-secret-32-chars-long-for-testing"
workers:
  health_check_interval: 30
  max_concurrent_tasks: 10
tasks:
  max_retries: 3
  checkpoint_interval: 300
llm:
  default_provider: "local"
  max_tokens: 4096
  temperature: 0.7
  timeout: 30
logging:
  level: "info"
  format: "text"
  output: "stdout"
`
		require.NoError(t, os.WriteFile(path, []byte(content), 0644))
		t.Setenv("HELIX_CONFIG", path)
		t.Setenv("HELIX_AUTH_JWT_SECRET", "")

		var loadErr error
		stdout, stderr := captureStdStreams(t, func() {
			_, loadErr = Load()
		})
		require.NoError(t, loadErr)

		assert.Equal(t, "", stdout,
			"the inert-key warning must not land on stdout; got %q", stdout)
		assert.Contains(t, stderr, "llm.timeout",
			"the inert-key warning must still reach the operator on stderr")
	})

	t.Run("no_config_file_found", func(t *testing.T) {
		// An empty cwd + empty HOME so viper's search path finds nothing and
		// the "no config file, using defaults" branch fires. Load() may return
		// a validation error here (no JWT secret) — that is precisely the
		// reported shape of the defect: the print happened BEFORE validation,
		// so it polluted stdout even on the error path. The assertion is on
		// stdout, not on the error.
		empty := t.TempDir()
		t.Chdir(empty)
		t.Setenv("HOME", empty)
		t.Setenv("HELIX_CONFIG", "")
		t.Setenv("HELIX_CONFIG_PATH", "")

		stdout, _ := captureStdStreams(t, func() {
			_, _ = Load()
		})

		assert.Equal(t, "", stdout,
			"config.Load() must write NOTHING to stdout even when no config file is found; got %q",
			stdout)
	})
}

// isScannedConfigSource reports whether a directory entry in the config
// package is a source file this guard must scan.
//
// TWO shapes qualify, and the second is why this predicate exists as a named
// function rather than an inline suffix check (HXC-002-F3-08):
//
//  1. A compiled production source — `*.go`, excluding `*_test.go`.
//
//  2. A Go-source SHADOW — a file whose name carries `.go` but whose FINAL
//     extension is something else, so the toolchain never compiles it:
//     config.go.clean, config.go.bak, config.go.orig, config.go.disabled.
//     These are invisible to the compiler, to `gofmt`, and to every vet-class
//     analyser — and they were invisible to THIS guard too, because it keyed
//     on HasSuffix(name, ".go"). config.go.clean sat in this package for ten
//     months carrying the exact fmt.Println diagnostics that the runtime guard
//     above exists to prevent; had anyone restored it (`mv config.go.clean
//     config.go`, the obvious use of a file named `.clean`), the stdout defect
//     would have returned with NOTHING firing. A shadow is a fix-in-waiting
//     for the very defect this file guards, so it is scanned as source.
//
// The predicate keys on the SHAPE (`.go` present, not final) rather than on a
// list of known shadow extensions, so a `.go.old` nobody anticipated is
// covered the moment it is written.
func isScannedConfigSource(name string, isDir bool) bool {
	if isDir || strings.HasSuffix(name, "_test.go") {
		return false
	}
	if strings.HasSuffix(name, ".go") {
		return true
	}
	// Shadow: ".go" appears as an interior extension segment.
	return strings.Contains(name, ".go.")
}

// TestLoad_NoFmtPrintlnOnStdoutInSource is the cheap source-layer companion to
// the runtime guard above: it fails fast and names the offending construct if
// anyone reintroduces a bare fmt.Print* (which targets stdout) into the
// config package's production sources — or into a never-compiled Go-source
// shadow of one, from which a single `mv` reintroduces the defect wholesale.
// The runtime test above remains the load-bearing proof; this one only makes
// the diagnosis obvious, and reaches the one place the runtime test cannot
// (a file the compiler never sees cannot be exercised at runtime).
func TestLoad_NoFmtPrintlnOnStdoutInSource(t *testing.T) {
	entries, err := os.ReadDir(".")
	require.NoError(t, err)

	for _, e := range entries {
		name := e.Name()
		if !isScannedConfigSource(name, e.IsDir()) {
			continue
		}
		src, err := os.ReadFile(name)
		require.NoError(t, err)
		for i, line := range strings.Split(string(src), "\n") {
			trimmed := strings.TrimSpace(line)
			if strings.HasPrefix(trimmed, "//") {
				continue // doc-comment examples are not code
			}
			for _, bad := range []string{"fmt.Println(", "fmt.Printf(", "fmt.Print("} {
				assert.NotContains(t, line, bad,
					"%s:%d writes to stdout via %s — config diagnostics MUST go to stderr "+
						"(stdout is a machine protocol for the subagent helper and ACP)",
					name, i+1, bad)
			}
		}
	}
}
