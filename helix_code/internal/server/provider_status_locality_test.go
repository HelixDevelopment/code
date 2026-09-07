package server

import (
	"testing"

	"github.com/gin-gonic/gin"

	"dev.helix.code/internal/llm"
	"dev.helix.code/internal/verifier"
)

// provider_status_locality_test.go — regression guard for a §11.4 status
// bluff in buildProvidersFromVerifiedModels (internal/server/handlers.go):
// the function used to classify a provider as "local" whenever the MODEL's
// weights were open-source (VerifiedModel.OpenSource), rather than by WHERE
// the model is actually served. internal/verifier/fallback_models.go's
// "deepseek-chat" entry (Provider: "deepseek", OpenSource: true) is the
// canonical counter-example: DeepSeek is a hosted API, but the pre-fix code
// reported it "type":"local","status":"available" while
// llm.NewCloudProvider(ProviderTypeDeepSeek) refuses to construct it with
// llm.ErrCloudDisabled whenever the W2c-1 cloud gate is closed (the
// mandated default, llm.cloud.enabled: false). The API told the operator a
// provider was available that construction would immediately refuse.
//
// Fix under test: locality is now decided by isLocalProviderID(m.Provider)
// — provider IDENTITY, matching internal/llm's (unexported)
// isLocalProviderType cloud-gate exemption list (Ollama, LlamaCpp only) —
// never by OpenSource.
//
// findProviderByID is a small local helper: buildProvidersFromVerifiedModels
// returns []gin.H in map-iteration order, which is non-deterministic, so
// tests must look entries up by "id" rather than assume a slice position.
func findProviderByID(t *testing.T, providers []gin.H, id string) gin.H {
	t.Helper()
	for _, p := range providers {
		if p["id"] == id {
			return p
		}
	}
	t.Fatalf("provider %q not found in result set %v", id, providers)
	return nil
}

// TestBuildProvidersFromVerifiedModels_HostedOpenWeightsProviderReportedAsCloud
// is the primary regression guard for the bluff: with the cloud gate CLOSED,
// a hosted provider serving an open-weights model (mirroring the real
// "deepseek-chat" fallback row) MUST be reported "type":"cloud" and
// "status":"disabled" — never "local"/"available", which is what
// construction (llm.NewCloudProvider) would in fact refuse.
//
// Mutation that makes this test FAIL: reintroducing `m.OpenSource ||` (or
// any OpenSource-based branch) into buildProvidersFromVerifiedModels's
// providerType classification — the exact pre-fix line — flips the
// "deepseek" provider back to "type":"local","status":"available" and this
// test's assertions on providerType/"local" and status/"available" fail.
func TestBuildProvidersFromVerifiedModels_HostedOpenWeightsProviderReportedAsCloud(t *testing.T) {
	prev := llm.CloudEnabled()
	llm.SetCloudEnabled(false)
	t.Cleanup(func() { llm.SetCloudEnabled(prev) })

	models := []*verifier.VerifiedModel{
		{
			ID:         "deepseek-chat",
			Name:       "DeepSeek Chat",
			Provider:   "deepseek",
			OpenSource: true, // mirrors internal/verifier/fallback_models.go
		},
	}

	providers := buildProvidersFromVerifiedModels(models)
	if len(providers) != 1 {
		t.Fatalf("buildProvidersFromVerifiedModels returned %d providers, want 1: %v", len(providers), providers)
	}
	deepseek := findProviderByID(t, providers, "deepseek")

	if got := deepseek["type"]; got != "cloud" {
		t.Fatalf(`deepseek provider "type" = %v, want "cloud" (hosted API — `+
			`OpenSource must not make a hosted provider report as "local")`, got)
	}
	if got := deepseek["status"]; got != "disabled" {
		t.Fatalf(`deepseek provider "status" = %v, want "disabled" (cloud gate `+
			`is closed; llm.NewCloudProvider(ProviderTypeDeepSeek) would refuse `+
			`construction with llm.ErrCloudDisabled — the status surface must `+
			`agree with the construction path)`, got)
	}
	if got := deepseek["open_source"]; got != true {
		t.Fatalf(`deepseek provider "open_source" = %v, want true (the model's `+
			`open-weights fact is still surfaced — just as its own field, `+
			`never conflated with hosting locality)`, got)
	}
}

// TestBuildProvidersFromVerifiedModels_GenuinelyLocalProviderStaysAvailable
// proves the fix did not over-correct: with the cloud gate closed, a
// genuinely local provider (Ollama, LlamaCpp — the only two types
// internal/llm's isLocalProviderType/NewCloudProvider treat as exempt from
// the cloud gate) must still report "type":"local","status":"available".
// Also includes a plainly-hosted, non-open-source provider (openai) as a
// second cloud-classified control.
//
// Mutation that makes this test FAIL: narrowing isLocalProviderID's exemption
// list — e.g. dropping the ollama/llamacpp case entirely, or making
// isLocalProviderID always return false — flips ollama/llamacpp to
// "type":"cloud","status":"disabled" and fails these assertions.
func TestBuildProvidersFromVerifiedModels_GenuinelyLocalProviderStaysAvailable(t *testing.T) {
	prev := llm.CloudEnabled()
	llm.SetCloudEnabled(false)
	t.Cleanup(func() { llm.SetCloudEnabled(prev) })

	models := []*verifier.VerifiedModel{
		{ID: "llama-3.2-3b", Name: "Llama 3.2 3B", Provider: "ollama", OpenSource: true},
		{ID: "coder-local", Name: "HelixLLM Coder", Provider: "llamacpp", OpenSource: true},
		{ID: "gpt-4o", Name: "GPT-4o", Provider: "openai", OpenSource: false},
	}

	providers := buildProvidersFromVerifiedModels(models)
	if len(providers) != 3 {
		t.Fatalf("buildProvidersFromVerifiedModels returned %d providers, want 3: %v", len(providers), providers)
	}

	for _, id := range []string{"ollama", "llamacpp"} {
		p := findProviderByID(t, providers, id)
		if got := p["type"]; got != "local" {
			t.Fatalf(`%s provider "type" = %v, want "local"`, id, got)
		}
		if got := p["status"]; got != "available" {
			t.Fatalf(`%s provider "status" = %v, want "available" (local `+
				`providers are exempt from the cloud gate)`, id, got)
		}
	}

	openai := findProviderByID(t, providers, "openai")
	if got := openai["type"]; got != "cloud" {
		t.Fatalf(`openai provider "type" = %v, want "cloud"`, got)
	}
	if got := openai["status"]; got != "disabled" {
		t.Fatalf(`openai provider "status" = %v, want "disabled" (cloud gate closed)`, got)
	}
	if got := openai["open_source"]; got != false {
		t.Fatalf(`openai provider "open_source" = %v, want false`, got)
	}
}
