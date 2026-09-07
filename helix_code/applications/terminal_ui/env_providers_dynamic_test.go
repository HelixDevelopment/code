package main

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"dev.helix.code/internal/config"
	"dev.helix.code/internal/llm"
)

// env_providers_dynamic_test.go — proves the PRIMARY path sources the
// OpenAI-compatible providers DYNAMICALLY from a LLMsVerifier /api/providers
// endpoint (CONST-036/CONST-046: the verifier's api_url is the base URL, NOT a
// hardcoded literal), and that the hardcoded catalogue is engaged ONLY as a
// degraded fallback when the verifier is unreachable.
//
// §11.4.120 reconciliation note (W2c-1 cloud gate closed the
// NewOpenAICompatibleProvider bypass — see internal/llm/openai_compatible_provider.go
// and internal/llm/provider_factory.go): every URL these tests deal in
// (verifier-supplied AND hardcoded-catalogue) is a REMOTE https:// endpoint, so
// hosted-provider construction through either path now additionally requires
// llm.cloud.enabled=true (the process-wide cloud gate, default false). No test below calls
// t.Parallel(): the gate is a process-global every test observes or flips.

// newFakeVerifier stands up an httptest LLMsVerifier serving /api/providers with
// the real envelope shape (name + api_url + models + is_active + status).
func newFakeVerifier(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/providers" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"providers": [
				{"id":1,"name":"cerebras","api_url":"https://verifier.cerebras.example/v1","status":"active","is_active":true,"reliability_score":9.1,"models":["m1"]},
				{"id":2,"name":"sambanova","api_url":"https://verifier.sambanova.example/v1","status":"active","is_active":true,"reliability_score":8.7,"models":["m2"]},
				{"id":3,"name":"novita","api_url":"https://verifier.novita.example/v3/openai","status":"active","is_active":true,"reliability_score":8.0,"models":["m3"]}
			],
			"count": 3
		}`))
	}))
}

// TestBuildOpenAICompatibleProviders_DynamicUsesVerifierAPIURL asserts the
// PRIMARY path: when the verifier is reachable, providers are built from its
// api_url (NOT the hardcoded catalogue URL) and only present-key providers come up.
//
// §11.4.120 reconciliation: this test's genuine subject is URL PROVENANCE on
// the dynamic path — that BuildDynamicOpenAICompatibleProviders threads
// rec.APIURL (the verifier's own value) into the constructed provider rather
// than any hardcoded literal. That property can only be observed once
// construction actually succeeds, and every verifier-supplied URL here
// (verifier.cerebras.example, verifier.novita.example) is a remote https://
// host, so it now also has to clear the W2c-1 cloud gate. The gate is opened
// explicitly (mirroring what production startup does from
// cfg.LLM.Cloud.Enabled, and the identical idiom already used by
// TestRegisterEnvProviders_RegistersWhenKeyPresent in env_providers_test.go)
// rather than weakening the URL-provenance assertions below. Gate *policy*
// itself (closed-by-default refusal of remote OpenAI-compatible endpoints) is
// already covered independently by
// internal/llm/cloud_gate_endpoint_locality_test.go's
// TestCloudGateClosed_RemoteOpenAICompatibleRefused and by this package's own
// TestRegisterEnvProviders_GateClosedRegistersNone — duplicating that concern
// here would add nothing and would cost this test its only real value: proving
// the dynamic builder never falls back to a hardcoded URL when the verifier is
// reachable.
func TestBuildOpenAICompatibleProviders_DynamicUsesVerifierAPIURL(t *testing.T) {
	clearAllProviderKeys(t)
	openCloudGateForTest(t)
	srv := newFakeVerifier(t)
	defer srv.Close()
	t.Setenv("HELIX_VERIFIER_ENDPOINT", srv.URL)

	// Only cerebras + novita keys present → sambanova must be skipped.
	t.Setenv("CEREBRAS_API_KEY", "cb-dummy-real-looking-value-123")
	t.Setenv("NOVITA_API_KEY", "nv-dummy-real-looking-value-123")

	providers, usedDynamic := buildOpenAICompatibleProviders(nil)
	if !usedDynamic {
		t.Fatalf("expected usedDynamic=true when verifier is reachable")
	}

	byName := map[string]llm.Provider{}
	for _, p := range providers {
		byName[p.GetName()] = p
	}
	if _, ok := byName["sambanova"]; ok {
		t.Errorf("sambanova has no present key — must NOT be built")
	}
	for name, wantURL := range map[string]string{
		"cerebras": "https://verifier.cerebras.example/v1",
		"novita":   "https://verifier.novita.example/v3/openai",
	} {
		p, ok := byName[name]
		if !ok {
			t.Fatalf("expected %s to be built from verifier record", name)
		}
		oc, ok := p.(*llm.OpenAICompatibleProvider)
		if !ok {
			t.Fatalf("%s: not *OpenAICompatibleProvider: %T", name, p)
		}
		if got := oc.BaseURL(); got != wantURL {
			t.Errorf("%s BaseURL = %q, want the VERIFIER api_url %q (no hardcoded URL)", name, got, wantURL)
		}
	}
}

// TestBuildOpenAICompatibleProviders_FallbackWhenVerifierUnreachable asserts
// the FALLBACK SWITCH: when the verifier is unreachable, usedDynamic is false.
//
// §11.4.120 reconciliation (the "open the gate everywhere" trap): this test is
// DELIBERATELY NOT reconciled the same way as
// TestBuildOpenAICompatibleProviders_DynamicUsesVerifierAPIURL above.
//
// The scenario this test models — HELIX_VERIFIER_ENDPOINT unreachable — is the
// everyday, out-of-the-box case (no LLMsVerifier running), carrying no signal
// that the operator has opted into cloud usage. Opening the cloud gate here
// just to keep the ORIGINAL assertion ("fallback registers a real cerebras
// provider from the hardcoded catalogue") alive would hide the actual, now-
// current production behaviour for this exact scenario: the hardcoded
// HostedOpenAICompatibleCatalogue() entries are themselves all remote https://
// endpoints (e.g. https://api.cerebras.ai/v1), so they route through the SAME
// NewOpenAICompatibleProvider endpoint-locality gate as the dynamic path
// (openai_compatible_provider.go) and are refused identically while
// llm.cloud.enabled stays at its default false. That is precisely the
// bypass this whole effort closed — a credential present via the fallback
// catalogue used to silently yield a real hosted provider even with no cloud
// opt-in. So the gate is left at its real default (closed) here, snapshotted
// and restored explicitly — mirroring TestRegisterEnvProviders_GateClosedRegistersNone
// in env_providers_test.go — and the assertion is updated to expect ZERO
// fallback providers, not one.
//
// This does not lose meaningful coverage: the fallback catalogue's own
// URL-provenance concern (does NewHostedOpenAICompatibleProvider actually wire
// through h.BaseURL) is exercised independently, with the gate legitimately
// open, by internal/llm's TestNewHostedOpenAICompatibleProvider_BuildsProviderWhenKeyPresent;
// gate-closed refusal of that exact wrapper is proven by the paired
// TestNewHostedOpenAICompatibleProvider_RefusedWhenCloudGateClosed. What THIS
// test uniquely proves is that buildOpenAICompatibleProviders's own fallback
// loop — this file's actual subject — does not leak a hosted provider around
// the gate when a key happens to be present.
func TestBuildOpenAICompatibleProviders_FallbackWhenVerifierUnreachable(t *testing.T) {
	clearAllProviderKeys(t)

	// Explicitly close the gate rather than relying on the package zero value
	// or prior-test ordering (identical idiom to
	// TestRegisterEnvProviders_GateClosedRegistersNone): snapshot the current
	// value and restore it on cleanup, so this test's own state is
	// self-contained regardless of what ran before it in the same process.
	prev := llm.CloudEnabled()
	llm.SetCloudEnabled(false)
	t.Cleanup(func() { llm.SetCloudEnabled(prev) })

	// Unreachable verifier endpoint.
	t.Setenv("HELIX_VERIFIER_ENDPOINT", "http://127.0.0.1:1")

	// A hardcoded-catalogue provider key present — this is exactly the
	// historical bypass scenario: CEREBRAS_API_KEY set, cloud gate left at
	// its default (closed), no verifier configured/reachable. Every fallback
	// catalogue entry is a remote https:// endpoint, so with the gate closed
	// NewHostedOpenAICompatibleProvider (via NewOpenAICompatibleProvider's
	// endpoint-locality check) must refuse ALL of them.
	t.Setenv("CEREBRAS_API_KEY", "cb-dummy-real-looking-value-123")

	providers, usedDynamic := buildOpenAICompatibleProviders(nil)
	if usedDynamic {
		t.Fatalf("expected usedDynamic=false when verifier is unreachable")
	}
	if len(providers) != 0 {
		t.Fatalf("buildOpenAICompatibleProviders returned %d fallback provider(s) with "+
			"the cloud gate closed (the default), want 0: the hardcoded catalogue's "+
			"providers are all remote endpoints and must be refused by the same "+
			"endpoint-locality gate as the dynamic path, not silently registered via "+
			"the fallback back door", len(providers))
	}
	// Anti-bluff follow-up (mirrors TestRegisterEnvProviders_GateClosedRegistersNone):
	// a zero total count alone does not prove the specific provider under test
	// is absent — walk whatever providers (if any) are present and assert none
	// of them is cerebras.
	for _, p := range providers {
		if p.GetName() == "cerebras" {
			t.Fatalf("fallback registered a cerebras provider (%+v) although the cloud "+
				"gate is closed; hosted provider construction must not succeed through "+
				"the fallback catalogue back door", p)
		}
	}
}

// TestRegisterEnvProviders_DynamicEndToEnd proves the full wiring: registerEnvProviders
// with a reachable verifier registers the dynamically-built providers into the
// ModelManager (CONST-036 primary path is live end-to-end).
//
// §11.4.120 reconciliation: same reasoning as
// TestBuildOpenAICompatibleProviders_DynamicUsesVerifierAPIURL above — this is
// an explicit-verifier-reachable, dynamic-primary-path scenario (a reachable
// fake LLMsVerifier IS configured here, same as production would require an
// operator to actually stand one up), and its subject is end-to-end
// registration wiring, not gate policy. The gate is opened explicitly, mirroring
// TestRegisterEnvProviders_RegistersWhenKeyPresent's own §11.4.120 footnote for
// the direct-cloud-provider path.
func TestRegisterEnvProviders_DynamicEndToEnd(t *testing.T) {
	clearAllProviderKeys(t)
	openCloudGateForTest(t)
	srv := newFakeVerifier(t)
	defer srv.Close()
	t.Setenv("HELIX_VERIFIER_ENDPOINT", srv.URL)
	t.Setenv("CEREBRAS_API_KEY", "cb-dummy-real-looking-value-123")

	manager := llm.NewModelManager()
	cfg := &config.Config{} // no explicit verifier config → env endpoint resolves

	got := registerEnvProviders(manager, cfg)
	if got < 1 {
		t.Fatalf("registerEnvProviders registered %d providers, want >= 1 (dynamic cerebras)", got)
	}
}
