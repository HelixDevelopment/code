package config

import (
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// load_once_test.go — speed-programme Phase 2 task P2-T07.
//
// Proves the three P2-T07 invariants:
//   1. Get() reads the config file exactly ONCE no matter how many times it is
//      called (sync.Once memoisation) — drops repeat YAML reads.
//   2. defaults < file < env precedence is unchanged by the per-call local
//      *viper.Viper instance migration (no behaviour regression).
//   3. Concurrent Load() calls are race-free — the old process-global
//      viper.SetDefault panicked with "concurrent map writes". This closes the
//      P0-T02 latent bug. Run with `-race` for the proof.

// writeTestConfig writes a minimal valid YAML config to a fresh temp file and
// returns its path. The JWT secret is >=32 chars so validateConfig passes.
func writeTestConfig(t *testing.T, port int) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	content := `version: "1.0.0"
application:
  name: "HelixCode"
  environment: "development"
server:
  address: "0.0.0.0"
  port: ` + itoa(port) + `
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
logging:
  level: "info"
  format: "text"
  output: "stdout"
`
	require.NoError(t, os.WriteFile(path, []byte(content), 0644))
	return path
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	if neg {
		b = append([]byte{'-'}, b...)
	}
	return string(b)
}

// silenceTestOutput redirects BOTH stdout and stderr to /dev/null for the
// duration of a test. Load() emits its "using config file" / "no config file"
// notices on STDERR (they were moved off stdout because stdout is a machine
// protocol for the subagent-helper JSON and the ACP JSON-RPC transport — see
// Load() in config.go). stdout is still redirected so that a regression that
// puts a notice back on stdout does not spam the test log; the falsifiable
// assertion that stdout stays byte-empty lives in
// TestLoad_EmitsNoBytesOnStdout (stdout_protocol_guard_test.go).
func silenceTestOutput(t *testing.T) {
	t.Helper()
	origOut, origErr := os.Stdout, os.Stderr
	devnull, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	require.NoError(t, err)
	os.Stdout = devnull
	os.Stderr = devnull
	t.Cleanup(func() {
		os.Stdout = origOut
		os.Stderr = origErr
		_ = devnull.Close()
	})
}

// TestGet_ReadsConfigFileExactlyOnce proves the load-once invariant: calling
// Get() N times reads the config file off disk exactly once.
func TestGet_ReadsConfigFileExactlyOnce(t *testing.T) {
	silenceTestOutput(t)
	path := writeTestConfig(t, 8080)
	t.Setenv("HELIX_CONFIG", path)
	t.Setenv("HELIX_AUTH_JWT_SECRET", "")

	// Clean slate — discard any memoised config from earlier tests.
	resetForTest()
	// DETERMINISM (§11.4.50): also reset on the way OUT. resetForTest() at
	// entry gives THIS test a clean slate, but without a matching cleanup the
	// package-level loadOnce/cachedCfg singleton is left populated with this
	// test's t.TempDir()-backed config — a directory the framework then
	// deletes. Any sibling that calls Get() without resetting first would
	// inherit that stale, dangling cache, and whether it did so would depend
	// on test ORDER. Leaving the singleton empty makes the package
	// order-independent.
	t.Cleanup(resetForTest)
	before := readInConfigCalls()

	const n = 50
	var last *Config
	for i := 0; i < n; i++ {
		cfg, err := Get()
		require.NoError(t, err)
		require.NotNil(t, cfg)
		if last != nil {
			// Every caller MUST receive the SAME cached struct pointer.
			assert.Same(t, last, cfg, "Get() must return the identical cached *Config")
		}
		last = cfg
	}

	delta := readInConfigCalls() - before
	assert.Equal(t, int64(1), delta,
		"Get() called %d times must read the config file exactly once, got %d disk reads", n, delta)
	t.Logf("P2-T07 evidence: %d Get() calls -> %d ReadInConfig disk read(s)", n, delta)
}

// TestLoad_PrecedenceUnchanged proves defaults < file < env precedence is
// preserved by the local-viper-instance migration (no-regression proof).
func TestLoad_PrecedenceUnchanged(t *testing.T) {
	silenceTestOutput(t)

	t.Run("defaults_fill_unset_fields", func(t *testing.T) {
		// A minimal config file that supplies ONLY the validation-required
		// fields. Every field NOT in the file must come from the defaults
		// layer — proving the lowest precedence tier is applied.
		dir := t.TempDir()
		path := filepath.Join(dir, "config.yaml")
		minimal := `version: "1.0.0"
application:
  name: "HelixCode"
  environment: "development"
server:
  port: 8080
database:
  host: "localhost"
  dbname: "helixcode"
auth:
  jwt_secret: "minimal-config-jwt-secret-32-chars-x"
workers:
  health_check_interval: 30
  max_concurrent_tasks: 10
llm:
  default_provider: "local"
  max_tokens: 4096
  temperature: 0.7
`
		require.NoError(t, os.WriteFile(path, []byte(minimal), 0644))
		t.Setenv("HELIX_CONFIG", path)
		t.Setenv("HELIX_AUTH_JWT_SECRET", "")

		cfg, err := Load()
		require.NoError(t, err)
		// server.read_timeout is NOT in the file — must come from the
		// defaults layer (setDefaultsOn sets it to 30).
		assert.Equal(t, 30, cfg.Server.ReadTimeout, "unset field must fall back to its default")
		// auth.bcrypt_cost default is 12.
		assert.Equal(t, 12, cfg.Auth.BcryptCost, "unset auth.bcrypt_cost must default to 12")
		// logging.level default is "info".
		assert.Equal(t, "info", cfg.Logging.Level, "unset logging.level must default to info")
	})

	t.Run("file_overrides_default", func(t *testing.T) {
		path := writeTestConfig(t, 9191)
		t.Setenv("HELIX_CONFIG", path)
		t.Setenv("HELIX_AUTH_JWT_SECRET", "")

		cfg, err := Load()
		require.NoError(t, err)
		assert.Equal(t, 9191, cfg.Server.Port, "config file value must override the 8080 default")
	})

	t.Run("env_overrides_file", func(t *testing.T) {
		// File sets jwt_secret to the in-file value; env must win.
		path := writeTestConfig(t, 8080)
		t.Setenv("HELIX_CONFIG", path)
		const envSecret = "env-wins-over-file-jwt-secret-32chars"
		t.Setenv("HELIX_AUTH_JWT_SECRET", envSecret)

		cfg, err := Load()
		require.NoError(t, err)
		assert.Equal(t, envSecret, cfg.Auth.JWTSecret,
			"HELIX_AUTH_JWT_SECRET env var must override the config-file value")
	})
}

// TestLoad_ConcurrentIsRaceFree proves concurrent Load() calls are race-free.
// Before P2-T07, Load() mutated the process-global viper singleton via
// viper.SetDefault — concurrent construction panicked "concurrent map writes".
// Run: go test -race -run TestLoad_ConcurrentIsRaceFree ./internal/config/
func TestLoad_ConcurrentIsRaceFree(t *testing.T) {
	silenceTestOutput(t)
	path := writeTestConfig(t, 8080)
	t.Setenv("HELIX_CONFIG", path)
	t.Setenv("HELIX_AUTH_JWT_SECRET", "")

	const goroutines = 64
	var wg sync.WaitGroup
	var failures int64
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			cfg, err := Load()
			if err != nil || cfg == nil || cfg.Server.Port != 8080 {
				atomic.AddInt64(&failures, 1)
			}
		}()
	}
	wg.Wait()
	assert.Equal(t, int64(0), atomic.LoadInt64(&failures),
		"%d concurrent Load() calls must all succeed without panic or wrong value", goroutines)
	t.Logf("P2-T07/P0-T02 evidence: %d concurrent Load() calls completed race-free", goroutines)
}

// TestGet_ConcurrentIsRaceFree proves concurrent Get() calls share the single
// sync.Once-guarded load without data race and all observe the same struct.
func TestGet_ConcurrentIsRaceFree(t *testing.T) {
	silenceTestOutput(t)
	path := writeTestConfig(t, 8080)
	t.Setenv("HELIX_CONFIG", path)
	t.Setenv("HELIX_AUTH_JWT_SECRET", "")
	resetForTest()
	// DETERMINISM (§11.4.50): also reset on the way OUT. resetForTest() at
	// entry gives THIS test a clean slate, but without a matching cleanup the
	// package-level loadOnce/cachedCfg singleton is left populated with this
	// test's t.TempDir()-backed config — a directory the framework then
	// deletes. Any sibling that calls Get() without resetting first would
	// inherit that stale, dangling cache, and whether it did so would depend
	// on test ORDER. Leaving the singleton empty makes the package
	// order-independent.
	t.Cleanup(resetForTest)

	const goroutines = 64
	var wg sync.WaitGroup
	results := make([]*Config, goroutines)
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func(idx int) {
			defer wg.Done()
			cfg, err := Get()
			require.NoError(t, err)
			results[idx] = cfg
		}(i)
	}
	wg.Wait()

	for i := 1; i < goroutines; i++ {
		assert.Same(t, results[0], results[i],
			"all concurrent Get() callers must observe the identical cached *Config")
	}
}

// BenchmarkLoad measures a full fresh Load() (YAML read + viper churn) — the
// per-call cost Get() amortises to once-per-process.
func BenchmarkLoad(b *testing.B) {
	dir := b.TempDir()
	path := filepath.Join(dir, "config.yaml")
	content := `version: "1.0.0"
application:
  name: "HelixCode"
  environment: "development"
server:
  port: 8080
database:
  host: "localhost"
  port: 5432
  dbname: "helixcode"
  user: "helixcode"
auth:
  jwt_secret: "bench-jwt-secret-32-chars-long-for-tests"
workers:
  health_check_interval: 30
  max_concurrent_tasks: 10
tasks:
  max_retries: 3
llm:
  default_provider: "local"
  max_tokens: 4096
  temperature: 0.7
logging:
  level: "info"
`
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		b.Fatal(err)
	}
	b.Setenv("HELIX_CONFIG", path)
	b.Setenv("HELIX_AUTH_JWT_SECRET", "")

	origOut, origErr := os.Stdout, os.Stderr
	devnull, _ := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	os.Stdout = devnull
	os.Stderr = devnull
	defer func() { os.Stdout = origOut; os.Stderr = origErr; _ = devnull.Close() }()

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		cfg, err := Load()
		if err != nil || cfg == nil {
			b.Fatal("Load failed")
		}
	}
}

// BenchmarkGet measures the cached Get() path — first call loads, every
// subsequent call is a sync.Once fast-path returning the cached *Config.
func BenchmarkGet(b *testing.B) {
	dir := b.TempDir()
	path := filepath.Join(dir, "config.yaml")
	content := `version: "1.0.0"
application:
  name: "HelixCode"
  environment: "development"
server:
  port: 8080
database:
  host: "localhost"
  port: 5432
  dbname: "helixcode"
  user: "helixcode"
auth:
  jwt_secret: "bench-jwt-secret-32-chars-long-for-tests"
workers:
  health_check_interval: 30
  max_concurrent_tasks: 10
tasks:
  max_retries: 3
llm:
  default_provider: "local"
  max_tokens: 4096
  temperature: 0.7
logging:
  level: "info"
`
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		b.Fatal(err)
	}
	b.Setenv("HELIX_CONFIG", path)
	b.Setenv("HELIX_AUTH_JWT_SECRET", "")
	resetForTest()
	// DETERMINISM (§11.4.50): also reset on the way OUT. resetForTest() at
	// entry gives THIS benchmark a clean slate, but without a matching cleanup the
	// package-level loadOnce/cachedCfg singleton is left populated with this
	// test's t.TempDir()-backed config — a directory the framework then
	// deletes. Any sibling that calls Get() without resetting first would
	// inherit that stale, dangling cache, and whether it did so would depend
	// on test ORDER. Leaving the singleton empty makes the package
	// order-independent.
	b.Cleanup(resetForTest)

	origOut, origErr := os.Stdout, os.Stderr
	devnull, _ := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	os.Stdout = devnull
	os.Stderr = devnull
	defer func() { os.Stdout = origOut; os.Stderr = origErr; _ = devnull.Close() }()

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		cfg, err := Get()
		if err != nil || cfg == nil {
			b.Fatal("Get failed")
		}
	}
}
