package llm

import (
	"strings"
	"testing"

	"dev.helix.code/internal/verifier"
)

// verifier_dynamic_catalogue_test.go — RED-first coverage for the DYNAMIC
// OpenAI-compatible provider builder. Given verifier provider records (whose
// api_url is the verifier's, NOT a hardcoded literal — CONST-036/CONST-046) plus
// the present env keys, it constructs *OpenAICompatibleProvider per provider
// whose key is present, using the api_url FROM THE VERIFIER.

// TestDeriveKeyEnvAliases asserts the env-var derivation from a provider name:
// uppercase + "_API_KEY" is always first, plus the ApiKey_<Name> convention,
// plus any extra aliases the keyrecognition table knows for that name.
func TestDeriveKeyEnvAliases(t *testing.T) {
	aliases := DeriveKeyEnvAliases("cerebras")
	if len(aliases) == 0 {
		t.Fatalf("expected aliases for cerebras, got none")
	}
	if aliases[0] != "CEREBRAS_API_KEY" {
		t.Errorf("first derived alias = %q, want CEREBRAS_API_KEY", aliases[0])
	}
	// The ApiKey_<Name> capitalised convention must be present.
	found := false
	for _, a := range aliases {
		if a == "ApiKey_Cerebras" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected ApiKey_Cerebras among %v", aliases)
	}

	// A hyphenated/dotted name normalises to an underscore-uppercase env var.
	zai := DeriveKeyEnvAliases("z-ai")
	if zai[0] != "Z_AI_API_KEY" {
		t.Errorf("z-ai first alias = %q, want Z_AI_API_KEY", zai[0])
	}
}

// TestBuildDynamicProviders_UsesVerifierAPIURL is the load-bearing assertion:
// the constructed providers use the EXACT api_url from the verifier records (no
// hardcoded URL), and only providers whose key is present are built.
func TestBuildDynamicProviders_UsesVerifierAPIURL(t *testing.T) {
	// §11.4.120: BuildDynamicOpenAICompatibleProviders builds through
	// NewOpenAICompatibleProvider, which now gates on endpoint locality
	// (openai_compatible_provider.go) — the verifier-supplied api_url below
	// is a remote https:// host, so it is refused while the W2c-1 cloud gate
	// is closed (the default). This test's subject is the dynamic
	// builder/catalogue — that it wires the EXACT verifier api_url as the
	// constructed provider's BaseURL — not gate policy; the gate's own
	// closed/open behavior for this exact builder is already covered by
	// TestCloudGateClosed_DynamicCatalogueBuildsNothing /
	// TestCloudGateOpen_DynamicCatalogueBuildsProvider in
	// cloud_gate_endpoint_locality_test.go, so this test opens the gate to
	// reach the assertion it actually tests.
	openCloudGateForTest(t)

	// cerebras key present, groq key absent → only cerebras is built.
	t.Setenv("CEREBRAS_API_KEY", "cb-dummy-real-looking-value-123")
	t.Setenv("GROQ_API_KEY", "")
	t.Setenv("ApiKey_Groq", "")

	recs := []verifier.VerifierProvider{
		{Name: "cerebras", APIURL: "https://verifier-says.cerebras.example/v1", IsActive: true, Status: "active"},
		{Name: "groq", APIURL: "https://api.groq.com/openai/v1", IsActive: true, Status: "active"},
	}

	built := BuildDynamicOpenAICompatibleProviders(recs)
	if len(built) != 1 {
		t.Fatalf("expected exactly 1 provider built (cerebras), got %d", len(built))
	}
	p := built[0]
	if p.GetName() != "cerebras" {
		t.Errorf("built provider name = %q, want cerebras", p.GetName())
	}
	// The base URL MUST be the verifier's api_url, not a hardcoded one.
	oc, ok := p.(*OpenAICompatibleProvider)
	if !ok {
		t.Fatalf("built provider is not *OpenAICompatibleProvider: %T", p)
	}
	if oc.config.BaseURL != "https://verifier-says.cerebras.example/v1" {
		t.Errorf("BaseURL = %q, want the verifier api_url https://verifier-says.cerebras.example/v1", oc.config.BaseURL)
	}
}

// TestBuildDynamicProviders_SkipsAbsentKeys asserts that a provider with no
// present key is skipped (never built with a blank/placeholder credential).
func TestBuildDynamicProviders_SkipsAbsentKeys(t *testing.T) {
	t.Setenv("SAMBANOVA_API_KEY", "")
	t.Setenv("ApiKey_SambaNova", "")

	recs := []verifier.VerifierProvider{
		{Name: "sambanova", APIURL: "https://api.sambanova.ai/v1", IsActive: true},
	}
	built := BuildDynamicOpenAICompatibleProviders(recs)
	if len(built) != 0 {
		t.Fatalf("expected 0 providers (no key present), got %d", len(built))
	}
}

// TestBuildDynamicProviders_RejectsPlaceholder asserts placeholder values are
// rejected exactly like the rest of the package (keyrecognition.go).
func TestBuildDynamicProviders_RejectsPlaceholder(t *testing.T) {
	t.Setenv("NOVITA_API_KEY", "your-api-key-here")

	recs := []verifier.VerifierProvider{
		{Name: "novita", APIURL: "https://api.novita.ai/v3/openai", IsActive: true},
	}
	built := BuildDynamicOpenAICompatibleProviders(recs)
	if len(built) != 0 {
		t.Fatalf("expected 0 providers (placeholder key), got %d", len(built))
	}
}

// TestBuildDynamicProviders_SkipsEmptyAPIURL asserts a provider record with no
// api_url is skipped — without a verifier-supplied base URL there is nothing to
// build on the dynamic (no-hardcode) path.
func TestBuildDynamicProviders_SkipsEmptyAPIURL(t *testing.T) {
	t.Setenv("CEREBRAS_API_KEY", "cb-dummy-real-looking-value-123")
	recs := []verifier.VerifierProvider{
		{Name: "cerebras", APIURL: "", IsActive: true},
	}
	built := BuildDynamicOpenAICompatibleProviders(recs)
	if len(built) != 0 {
		t.Fatalf("expected 0 providers (empty api_url), got %d", len(built))
	}
}

// TestBuildDynamicProviders_SkipsNativelyWiredProviders asserts the dynamic
// catalogue does NOT duplicate the providers HelixCode already wires natively
// (deepseek/groq/mistral/openrouter/openai/anthropic/...) — they keep their
// dedicated direct-env-key path and must not be double-registered here.
func TestBuildDynamicProviders_SkipsNativelyWiredProviders(t *testing.T) {
	t.Setenv("DEEPSEEK_API_KEY", "ds-dummy-real-looking-value-123")

	recs := []verifier.VerifierProvider{
		{Name: "deepseek", APIURL: "https://api.deepseek.com/v1", IsActive: true},
	}
	built := BuildDynamicOpenAICompatibleProviders(recs)
	for _, p := range built {
		if strings.EqualFold(p.GetName(), "deepseek") {
			t.Errorf("deepseek must not be built dynamically — it is natively wired")
		}
	}
}
