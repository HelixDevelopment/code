package llm

import (
	"errors"
	"net/url"
	"strings"
	"testing"
)

// openai_compatible_catalogue_test.go — RED-first coverage for the data-driven
// hosted OpenAI-Chat-Completions-compatible provider catalogue (CONST-074
// reuse-don't-reimplement: base URLs lifted from helix_agent's
// providerMappings table; CONST-042 anti-secret-leak: never prints key VALUES).

// alreadyWiredProviderNames is the set of providers HelixCode already wires
// natively (keyrecognition.go ProviderEnvAliases + dedicated providers). The
// hosted catalogue MUST NOT collide with any of these.
var alreadyWiredProviderNames = map[string]bool{
	"openai":     true,
	"anthropic":  true,
	"gemini":     true,
	"deepseek":   true,
	"mistral":    true,
	"groq":       true,
	"xai":        true,
	"openrouter": true,
	"qwen":       true,
	"copilot":    true,
	"ollama":     true,
}

func TestHostedOpenAICompatibleCatalogue_NonEmptyAndWellFormed(t *testing.T) {
	cat := HostedOpenAICompatibleCatalogue()
	if len(cat) == 0 {
		t.Fatalf("catalogue is empty; expected ≥1 hosted OpenAI-compatible provider")
	}
	seen := map[string]bool{}
	for _, h := range cat {
		if strings.TrimSpace(h.Name) == "" {
			t.Errorf("entry has empty Name: %+v", h)
		}
		if strings.TrimSpace(h.BaseURL) == "" {
			t.Errorf("%s: empty BaseURL", h.Name)
		}
		if !strings.HasPrefix(h.BaseURL, "https://") {
			t.Errorf("%s: BaseURL %q is not https", h.Name, h.BaseURL)
		}
		if len(h.KeyEnvAliases) == 0 {
			t.Errorf("%s: no KeyEnvAliases", h.Name)
		}
		for _, a := range h.KeyEnvAliases {
			if strings.TrimSpace(a) == "" {
				t.Errorf("%s: blank key env alias", h.Name)
			}
		}
		if seen[h.Name] {
			t.Errorf("%s: duplicate catalogue entry", h.Name)
		}
		seen[h.Name] = true
	}
}

func TestHostedOpenAICompatibleCatalogue_NoCollisionWithWiredProviders(t *testing.T) {
	for _, h := range HostedOpenAICompatibleCatalogue() {
		if alreadyWiredProviderNames[strings.ToLower(h.Name)] {
			t.Errorf("%s collides with an already-wired provider", h.Name)
		}
	}
}

// TestHostedOpenAICompatibleCatalogue_ModelsURLComposition asserts the
// URL-composition gotcha is handled: for every entry the effective models URL
// MUST contain exactly one "/v1/models" (or the documented variant) — never
// "/v1/v1/models".
func TestHostedOpenAICompatibleCatalogue_ModelsURLComposition(t *testing.T) {
	for _, h := range HostedOpenAICompatibleCatalogue() {
		modelsURL := h.ComposedModelsURL()
		if _, err := url.Parse(modelsURL); err != nil {
			t.Errorf("%s: composed models URL %q does not parse: %v", h.Name, modelsURL, err)
		}
		if strings.Contains(modelsURL, "/v1/v1/") {
			t.Errorf("%s: composed models URL has doubled /v1: %q", h.Name, modelsURL)
		}
		if strings.Count(modelsURL, "/models") != 1 {
			t.Errorf("%s: composed models URL must end in exactly one /models: %q", h.Name, modelsURL)
		}
		// When the base URL ends in /v1, the composed URL must be exactly
		// "<base>/models" → ".../v1/models".
		base := strings.TrimSuffix(h.BaseURL, "/")
		if strings.HasSuffix(base, "/v1") && !strings.HasSuffix(modelsURL, "/v1/models") {
			t.Errorf("%s: base %q ends /v1 but composed models URL %q is not <base>/models",
				h.Name, h.BaseURL, modelsURL)
		}
	}
}

// TestHostedOpenAICompatibleCatalogue_LiveVerifiedHosts regression-guards the
// §11.4.99 live-verified (2026-06-14) host corrections: siliconflow MUST use the
// international .com host (the .cn host rejected the key with HTTP 401 while .com
// returned HTTP 200 + 69 models for the SAME key) and kimi MUST use the
// api.moonshot.ai host (the official OpenAPI server URL; .cn is a separate
// China-mainland platform). A regression back to the .cn hosts would silently
// break model listing for these providers.
func TestHostedOpenAICompatibleCatalogue_LiveVerifiedHosts(t *testing.T) {
	want := map[string]string{
		"siliconflow": "https://api.siliconflow.com/v1",
		"kimi":        "https://api.moonshot.ai/v1",
	}
	got := map[string]string{}
	for _, h := range HostedOpenAICompatibleCatalogue() {
		got[h.Name] = h.BaseURL
	}
	for name, wantBase := range want {
		if got[name] != wantBase {
			t.Errorf("%s BaseURL = %q, want live-verified %q", name, got[name], wantBase)
		}
		if strings.Contains(got[name], ".cn/") {
			t.Errorf("%s BaseURL %q regressed to the .cn host that rejects the key", name, got[name])
		}
	}
}

// TestHostedOpenAICompatibleCatalogue_NoModelsEndpointProvidersExcluded guards
// the §11.4.99 live-verified (2026-06-14) exclusions: codestral.mistral.ai and
// api.upstage.ai expose NO GET /v1/models listing endpoint, so they cannot be
// catalogue-listed and MUST stay out of the catalogue. Re-adding either without
// a real models endpoint would make NewHostedOpenAICompatibleProvider fail at
// discoverModels for every operator who configures the key.
func TestHostedOpenAICompatibleCatalogue_NoModelsEndpointProvidersExcluded(t *testing.T) {
	excluded := map[string]bool{"codestral": true, "upstage": true}
	for _, h := range HostedOpenAICompatibleCatalogue() {
		if excluded[strings.ToLower(h.Name)] {
			t.Errorf("%s is in the catalogue but has no GET /models endpoint (live-verified 2026-06-14) — it must stay excluded", h.Name)
		}
	}
}

func TestNewHostedOpenAICompatibleProvider_ErrorWhenKeyAbsent(t *testing.T) {
	h := HostedOpenAICompatible{
		Name:          "fireworks",
		BaseURL:       "https://api.fireworks.ai/inference/v1",
		KeyEnvAliases: []string{"FIREWORKS_API_KEY", "ApiKey_Fireworks"},
	}
	// Ensure every alias is unset for this test.
	for _, a := range h.KeyEnvAliases {
		t.Setenv(a, "")
	}
	if _, err := NewHostedOpenAICompatibleProvider(h); err == nil {
		t.Fatalf("expected error when key env absent, got nil")
	}
}

func TestNewHostedOpenAICompatibleProvider_RejectsPlaceholder(t *testing.T) {
	h := HostedOpenAICompatible{
		Name:          "fireworks",
		BaseURL:       "https://api.fireworks.ai/inference/v1",
		KeyEnvAliases: []string{"FIREWORKS_API_KEY"},
	}
	t.Setenv("FIREWORKS_API_KEY", "your-api-key-here")
	if _, err := NewHostedOpenAICompatibleProvider(h); err == nil {
		t.Fatalf("expected error for placeholder key, got nil")
	}
}

func TestNewHostedOpenAICompatibleProvider_BuildsProviderWhenKeyPresent(t *testing.T) {
	// §11.4.120: NewHostedOpenAICompatibleProvider builds through
	// NewOpenAICompatibleProvider, which now gates on endpoint locality
	// (openai_compatible_provider.go) — a remote https:// BaseURL like
	// fireworks's is refused while the W2c-1 cloud gate is closed (the
	// default). This test's subject is the catalogue/builder wiring, not
	// gate policy — the gate's own closed/open behavior is covered by
	// cloud_gate_closed_test.go / cloud_gate_open_test.go /
	// cloud_gate_endpoint_locality_test.go — so it opens the gate to reach
	// the assertion it actually tests: successful construction when a
	// present, non-placeholder key exists.
	openCloudGateForTest(t)

	h := HostedOpenAICompatible{
		Name:          "fireworks",
		BaseURL:       "https://api.fireworks.ai/inference/v1",
		KeyEnvAliases: []string{"FIREWORKS_API_KEY"},
	}
	// Dummy non-placeholder value; never a real key (CONST-042). Offline:
	// discoverModels will fail silently, provider still constructs.
	t.Setenv("FIREWORKS_API_KEY", "fw-dummy-real-looking-value-12345")
	p, err := NewHostedOpenAICompatibleProvider(h)
	if err != nil {
		t.Fatalf("unexpected error building provider with present key: %v", err)
	}
	if p.GetName() != "fireworks" {
		t.Errorf("GetName()=%q, want fireworks", p.GetName())
	}
	if got := string(p.GetType()); got != "fireworks" {
		t.Errorf("GetType()=%q, want distinct type \"fireworks\"", got)
	}
}

// TestNewHostedOpenAICompatibleProvider_RefusedWhenCloudGateClosed is the
// negative counterpart §11.4.120 requires alongside the reconciled positive
// test above: with the W2c-1 cloud gate CLOSED (the default), the hosted
// catalogue constructor MUST refuse a real, remote catalogue entry with an
// error wrapping ErrCloudDisabled — proving the gate-closed path this test
// file's subject (NewHostedOpenAICompatibleProvider) actually reaches is
// still correctly enforced, not merely bypassed by the positive test's
// gate-open helper. This does not duplicate
// TestCloudGateClosed_RemoteOpenAICompatibleRefused in
// cloud_gate_endpoint_locality_test.go, which exercises the shared
// constructor NewOpenAICompatibleProvider directly with hand-built configs;
// this test exercises the catalogue-specific WRAPPER
// (NewHostedOpenAICompatibleProvider) that is this file's subject.
//
// Mutation that would make this test FAIL: remove the endpoint-locality gate
// check in NewOpenAICompatibleProvider (openai_compatible_provider.go), or
// change NewHostedOpenAICompatibleProvider to bypass it — either would let a
// hosted, remote catalogue entry construct with the gate closed, and this
// test's err == nil / errors.Is(err, ErrCloudDisabled) assertion would fail.
func TestNewHostedOpenAICompatibleProvider_RefusedWhenCloudGateClosed(t *testing.T) {
	closeCloudGateForTest(t) // shared helper, cloud_gate_endpoint_locality_test.go

	h := HostedOpenAICompatible{
		Name:          "fireworks",
		BaseURL:       "https://api.fireworks.ai/inference/v1",
		KeyEnvAliases: []string{"FIREWORKS_API_KEY"},
	}
	t.Setenv("FIREWORKS_API_KEY", "fw-dummy-real-looking-value-12345")

	p, err := NewHostedOpenAICompatibleProvider(h)
	if err == nil {
		if p != nil {
			_ = p.Close()
		}
		t.Fatalf("hosted catalogue provider %q constructed although the cloud gate "+
			"is closed — the catalogue wrapper must refuse a remote endpoint exactly "+
			"like the underlying constructor", h.Name)
	}
	if !errors.Is(err, ErrCloudDisabled) {
		t.Fatalf("refusal error = %v, want it to wrap ErrCloudDisabled so callers "+
			"can branch on one sentinel", err)
	}
	if p != nil {
		t.Fatalf("hosted catalogue provider %q returned non-nil alongside the gate "+
			"refusal — a refused construction must yield nothing usable", h.Name)
	}
}

// TestHostedProvider_GetTypeDistinctAcrossCatalogue is the load-bearing
// GetType-collision guard: two different catalogue providers must NOT report the
// same ProviderType, and neither may report the generic "local" type.
func TestHostedProvider_GetTypeDistinctAcrossCatalogue(t *testing.T) {
	// §11.4.120: both entries below use real, remote https:// BaseURLs and
	// build through NewHostedOpenAICompatibleProvider →
	// NewOpenAICompatibleProvider, which is now gated on endpoint locality.
	// This test's subject is GetType() distinctness across the catalogue,
	// not gate policy, so it opens the gate to reach construction.
	openCloudGateForTest(t)

	fw := HostedOpenAICompatible{Name: "fireworks", BaseURL: "https://api.fireworks.ai/inference/v1", KeyEnvAliases: []string{"FIREWORKS_API_KEY"}}
	ds := HostedOpenAICompatible{Name: "deepseek-fake", BaseURL: "https://api.deepseek.com/v1", KeyEnvAliases: []string{"FIREWORKS_API_KEY"}}
	t.Setenv("FIREWORKS_API_KEY", "fw-dummy-real-looking-value-12345")

	pfw, err := NewHostedOpenAICompatibleProvider(fw)
	if err != nil {
		t.Fatalf("build fireworks: %v", err)
	}
	pds, err := NewHostedOpenAICompatibleProvider(ds)
	if err != nil {
		t.Fatalf("build deepseek-fake: %v", err)
	}
	if pfw.GetType() == ProviderTypeLocal {
		t.Errorf("fireworks reports generic ProviderTypeLocal — names would collide")
	}
	if pfw.GetType() == pds.GetType() {
		t.Errorf("fireworks and deepseek-fake report the same ProviderType %q — collision", pfw.GetType())
	}
}

// TestHostedOpenAICompatibleCatalogue_HuggingFaceAndTogetherPresent is the
// §11.4.124 catalogue-first regression guard for the huggingface + together
// capability gap: both rows MUST be present with the §11.4.99 live-verified
// (2026-07-11) CURRENT hosted-router base URLs — never the RETIRED hosts the
// old bespoke clients (internal/llm/providers/huggingface,
// internal/llm/providers/together) hardcode
// (api-inference.huggingface.co/models/<model> for huggingface;
// mistralai/Mixtral-8x22B-Instruct-v0.1 as together's default model, which is
// no longer served). This is a §1.1 paired-mutation load-bearing test: delete
// either row from HostedOpenAICompatibleCatalogue() (or point BaseURL back at
// a retired host) and this test MUST fail.
func TestHostedOpenAICompatibleCatalogue_HuggingFaceAndTogetherPresent(t *testing.T) {
	want := map[string]string{
		"huggingface": "https://router.huggingface.co/v1",
		"together":    "https://api.together.ai/v1",
	}
	got := map[string]HostedOpenAICompatible{}
	for _, h := range HostedOpenAICompatibleCatalogue() {
		got[h.Name] = h
	}

	for name, wantBase := range want {
		entry, ok := got[name]
		if !ok {
			t.Errorf("catalogue missing required row %q (§11.4.124 capability gap not closed)", name)
			continue
		}
		if entry.BaseURL != wantBase {
			t.Errorf("%s BaseURL = %q, want §11.4.99 live-verified %q", name, entry.BaseURL, wantBase)
		}
		// Both hosted routers already end in /v1 — the composition gotcha
		// documented at the top of openai_compatible_catalogue.go requires
		// explicit ModelEndpoint/ChatEndpoint so the composed URL is
		// "<base>/models", never a doubled "/v1/v1/models".
		if entry.ModelEndpoint != "/models" {
			t.Errorf("%s ModelEndpoint = %q, want \"/models\"", name, entry.ModelEndpoint)
		}
		if entry.ChatEndpoint != "/chat/completions" {
			t.Errorf("%s ChatEndpoint = %q, want \"/chat/completions\"", name, entry.ChatEndpoint)
		}
		if len(entry.KeyEnvAliases) == 0 {
			t.Errorf("%s has no KeyEnvAliases", name)
		}
	}

	// Regression-guard the specific RETIRED endpoints this task closed: the
	// composed models URL for huggingface must never resolve through the
	// retired api-inference.huggingface.co host, and together's BaseURL must
	// never regress to a URL that still names the retired
	// mistralai/Mixtral-8x22B-Instruct-v0.1 model (that string has no business
	// appearing anywhere in a catalogue row).
	if hf, ok := got["huggingface"]; ok {
		if strings.Contains(hf.BaseURL, "api-inference.huggingface.co") {
			t.Errorf("huggingface BaseURL %q regressed to the retired api-inference.huggingface.co host", hf.BaseURL)
		}
		if !strings.Contains(hf.ComposedModelsURL(), "router.huggingface.co") {
			t.Errorf("huggingface composed models URL %q does not use the router.huggingface.co host", hf.ComposedModelsURL())
		}
	}
	if tg, ok := got["together"]; ok {
		if strings.Contains(tg.BaseURL, "Mixtral-8x22B") {
			t.Errorf("together BaseURL %q references the retired Mixtral-8x22B model", tg.BaseURL)
		}
	}
}

// TestOpenAICompatibleProvider_KnownLocalNamesUnchanged guards the GetType fix:
// the known local backends MUST keep their existing distinct types.
func TestOpenAICompatibleProvider_KnownLocalNamesUnchanged(t *testing.T) {
	cases := map[string]ProviderType{
		"vllm":     ProviderTypeVLLM,
		"localai":  ProviderTypeLocalAI,
		"lmstudio": ProviderTypeLMStudio,
		"jan":      ProviderTypeJan,
	}
	for name, want := range cases {
		p := &OpenAICompatibleProvider{name: name}
		if got := p.GetType(); got != want {
			t.Errorf("GetType() for %q = %q, want %q", name, got, want)
		}
	}
}
