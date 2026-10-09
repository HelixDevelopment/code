// helper_stdout_protocol_test.go — standing regression guard (§11.4.115 /
// §11.4.135) proving the subagent subprocess protocol is not corrupted by
// diagnostics that a provider-factory's config load emits.
//
// THE DEFECT (regression introduced by the cloud-gate wiring batch): the
// parent's SubprocessSpawner unmarshals the child's ENTIRE stdout buffer —
//
//	json.Unmarshal(bytes.TrimSpace(stdout), &decoded)   // subprocess_spawner.go
//
// so ANY byte the child writes to stdout before/around its SubagentResult
// breaks decoding with "invalid helper output". The batch added a
// config.Get() call to cmd/cli/main.go's buildSubagentLLMProvider — the
// factory this helper runs — and config.Load() at the time wrote
// "INFO: Using config file: …" to STDOUT. Every subprocess-spawned subagent
// therefore failed.
//
// This guard reproduces the real production wiring rather than the
// convenient one: RunAsSubagent passes os.Stdout, so the test redirects
// os.Stdout to a temp file and hands THAT SAME os.Stdout to
// runAsSubagentWithWriter. Handing it a private bytes.Buffer (as the other
// tests in this package do) would NOT reproduce the defect, because the
// config diagnostics go to the process stdout, not to the buffer.
//
// The factory deliberately calls config.Load() — the same loader
// buildSubagentLLMProvider reaches through config.Get() — so any writer the
// config package points at stdout shows up here. Load() rather than Get() on
// purpose: Get() memoises behind a sync.Once, so in a test binary where some
// earlier test already loaded config it would print nothing and this guard
// would pass vacuously with the bug still present.
//
// FALSIFYING MUTATION (§11.4.115 / §1.1): change the
// fmt.Fprintln(os.Stderr, …) diagnostics in internal/config/config.go's
// Load() back to fmt.Println. The captured stream then begins with
// "INFO: Using config file: …", the first non-space byte is no longer '{',
// json.Unmarshal fails, and this test FAILS with the same "invalid helper
// output" shape the parent spawner reports in production.
package subagent

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"dev.helix.code/internal/config"
	"dev.helix.code/internal/llm"
)

// writeSubagentTestConfig writes a minimal VALID config (JWT secret >= 32
// chars so validateConfig passes) and points HELIX_CONFIG at it, so
// config.Load() takes the "config file found" branch — the branch that emits
// the informational line this guard is about.
func writeSubagentTestConfig(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	content := `version: "1.0.0"
application:
  name: "HelixCode"
  environment: "development"
server:
  address: "0.0.0.0"
  port: 8080
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
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("write test config: %v", err)
	}
	t.Setenv("HELIX_CONFIG", path)
	t.Setenv("HELIX_AUTH_JWT_SECRET", "")
	return path
}

// TestRunAsSubagent_StdoutStaysPureJSON_WhenFactoryLoadsConfig drives the real
// production writer (os.Stdout, redirected to a temp file) through a factory
// that loads config, and asserts the resulting stream is EXACTLY one JSON
// document — nothing prepended, nothing appended.
func TestRunAsSubagent_StdoutStaysPureJSON_WhenFactoryLoadsConfig(t *testing.T) {
	cfgPath := writeSubagentTestConfig(t)

	task := SubagentTask{
		ID:          "task-stdout-protocol-1",
		Description: "stdout protocol integrity",
		Prompt:      "hello-protocol",
		Isolation:   IsolationNone,
	}
	payload, err := json.Marshal(task)
	if err != nil {
		t.Fatalf("marshal task: %v", err)
	}
	setHelperEnv(t, string(payload))

	// Mirror cmd/cli/main.go buildSubagentLLMProvider: load config (for the
	// process-global cloud gate) and then build the provider.
	configLoadingFactory := func(ctx context.Context) (llm.Provider, error) {
		cfg, cfgErr := config.Load()
		if cfgErr != nil {
			t.Errorf("factory: config.Load() failed for %s: %v", cfgPath, cfgErr)
		}
		if cfg == nil {
			t.Errorf("factory: config.Load() returned nil config for %s", cfgPath)
		}
		return NewFakeLLMProvider(map[string]string{
			"hello-protocol": "protocol-canned-1",
		}), nil
	}

	dir := t.TempDir()
	capPath := filepath.Join(dir, "stdout.cap")
	capFile, err := os.Create(capPath)
	if err != nil {
		t.Fatalf("create capture file: %v", err)
	}

	origOut, origErr := os.Stdout, os.Stderr
	devnull, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	if err != nil {
		t.Fatalf("open devnull: %v", err)
	}
	os.Stdout = capFile
	os.Stderr = devnull // operator diagnostics belong here; keep the test log clean

	// Production passes os.Stdout (see RunAsSubagent). Pass the SAME writer so
	// the config diagnostics and the protocol payload contend for one stream,
	// exactly as they do in the re-exec'd helper process.
	exitCode := runAsSubagentWithWriter(os.Stdout, configLoadingFactory)

	os.Stdout, os.Stderr = origOut, origErr
	_ = capFile.Close()
	_ = devnull.Close()

	raw, err := os.ReadFile(capPath)
	if err != nil {
		t.Fatalf("read capture file: %v", err)
	}

	if exitCode != 0 {
		t.Fatalf("expected exit 0, got %d (stdout=%q)", exitCode, string(raw))
	}

	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		t.Fatalf("helper wrote nothing to stdout")
	}
	if trimmed[0] != '{' {
		t.Fatalf("helper stdout is NOT pure JSON: first non-space byte is %q, want '{'. "+
			"Something (most likely a config diagnostic) was written to stdout ahead of "+
			"the SubagentResult; the parent spawner would reject this as "+
			"\"invalid helper output\". stdout=%q", trimmed[0], string(raw))
	}

	// The parent's exact decode step (subprocess_spawner.go): unmarshal the
	// WHOLE trimmed buffer, not just a prefix.
	var got SubagentResult
	if err := json.Unmarshal(trimmed, &got); err != nil {
		t.Fatalf("parent-side decode of helper stdout failed: %v (stdout=%q)", err, string(raw))
	}
	if got.TaskID != task.ID {
		t.Fatalf("expected TaskID %q round-trip, got %q", task.ID, got.TaskID)
	}
	if got.State != StateSucceeded {
		t.Fatalf("expected StateSucceeded, got %q (err=%q)", got.State, got.Error)
	}
	if got.Output != "protocol-canned-1" {
		t.Fatalf("expected Output=protocol-canned-1, got %q", got.Output)
	}
}
