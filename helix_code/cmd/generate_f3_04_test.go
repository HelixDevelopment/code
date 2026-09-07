package cmd

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"dev.helix.code/internal/llm"
)

// generate_f3_04_test.go — §11.4.135 standing regression guard for
// HXC-002-F3-04 (gap ledger docs/qa/2026-09-05-gap-ledger.md): `helix
// generate` must route through the SAME provider resolution semantics as the
// server (local default → the llama.cpp coder sidecar) and select from a
// REAL provider catalog — the pre-fix flow queried a ModelManager with zero
// registered providers and therefore always failed ("no models available"),
// and the command itself was never even registered on rootCmd (captured
// pre-fix: `unknown command "generate" for "helix"`).
//
// RED evidence (defect-present, captured 2026-09-05 before the fix):
//
//	$ helix generate "say ok"
//	Error: unknown command "generate" for "helix"
//
// GREEN: with a fake local coder (OpenAI-shaped /v1/models), the manager
// registers the resolved provider and SelectOptimalModel returns a real
// catalog model.
func TestGenerateManager_LocalDefaultRegistersRealCatalog(t *testing.T) {
	// Fake llama.cpp coder: OpenAI-compatible /v1/models catalog.
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"data":[{"id":"fake-coder-model"}]}`)
	}))
	t.Cleanup(fake.Close)
	t.Setenv("HELIX_LLM_LOCAL_OPENAI_ENDPOINT", fake.URL)
	t.Setenv("HELIX_LLM_PROVIDER", "")

	mgr, prov, err := newGenerateManager("local")
	if err != nil {
		t.Fatalf("newGenerateManager(\"local\"): %v (the generate path must "+
			"resolve the local coder route, HXC-002-F3-04)", err)
	}
	defer func() { _ = prov.Close() }()

	modelInfo, err := mgr.SelectOptimalModel(llm.ModelSelectionCriteria{
		TaskType:          "text-generation",
		QualityPreference: "balanced",
	})
	if err != nil {
		t.Fatalf("SelectOptimalModel over the registered coder catalog: %v "+
			"(pre-fix this always failed: no models available)", err)
	}
	if modelInfo == nil || modelInfo.Name == "" {
		t.Fatalf("SelectOptimalModel returned an empty model from a live catalog")
	}

	serving, err := mgr.GetProviderForModel(modelInfo.Name, prov.GetType())
	if err != nil {
		t.Fatalf("GetProviderForModel(%q, %s): %v", modelInfo.Name, prov.GetType(), err)
	}
	if serving == nil {
		t.Fatalf("GetProviderForModel returned nil for a model the manager selected")
	}
}

// TestGenerateCmd_Registered: the command tree must actually expose
// `generate` (pre-fix it was defined but never added to rootCmd).
func TestGenerateCmd_Registered(t *testing.T) {
	found := false
	for _, c := range rootCmd.Commands() {
		if c.Name() == "generate" {
			found = true
		}
	}
	if !found {
		t.Fatalf("generateCmd is not registered on rootCmd — `helix generate` " +
			"answers 'unknown command' (HXC-002-F3-04)")
	}
}
