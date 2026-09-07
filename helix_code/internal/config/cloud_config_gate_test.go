package config

import (
	"os"
	"path/filepath"
	"testing"
)

// cloud_config_gate_test.go — config-layer guard for the W2c-1 cloud gate:
// the new key llm.cloud.enabled MUST exist and MUST default to FALSE
// (operator mandate 2026-09-05: local-only adaptive serving; cloud providers
// disabled by default even when API keys are present).

// TestCloudGateConfig_DefaultsClosed: with no config file at all (pure
// defaults), the gate is closed.
func TestCloudGateConfig_DefaultsClosed(t *testing.T) {
	// Force the no-file path: HELIX_CONFIG empty AND $HOME redirected to an
	// empty temp dir (the operator machine may carry a stale
	// ~/.config/helixcode/config.json whose keys fail the strict check —
	// viper would pick it up via the $HOME fallback and error the load).
	// JWT secret via env so the default-only load passes validation.
	t.Setenv("HELIX_CONFIG", "")
	t.Setenv("HOME", t.TempDir())
	t.Setenv("HELIX_AUTH_JWT_SECRET", "test-secret-not-the-default")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() with defaults: %v", err)
	}
	if cfg.LLM.Cloud.Enabled {
		t.Fatalf("llm.cloud.enabled must DEFAULT to false (local-only serving " +
			"mandate 2026-09-05); got open")
	}
}

// TestCloudGateConfig_ExplicitEnable: an operator who sets
// llm.cloud.enabled: true in their config gets the gate opened.
func TestCloudGateConfig_ExplicitEnable(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	// Minimal VALID config — the loader validates required fields, so a
	// bare llm block alone fails; supply the small set validateConfig needs.
	minimal := `version: "1.0.0"
database:
  host: "localhost"
  dbname: "helixcode_test"
auth:
  jwt_secret: "test-secret-not-the-default"
llm:
  cloud:
    enabled: true
`
	if err := os.WriteFile(path, []byte(minimal), 0o600); err != nil {
		t.Fatalf("write temp config: %v", err)
	}
	t.Setenv("HELIX_CONFIG", path)
	t.Setenv("HOME", dir)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() with explicit enable: %v", err)
	}
	if !cfg.LLM.Cloud.Enabled {
		t.Fatalf("llm.cloud.enabled: true in config did not open the gate")
	}
}
