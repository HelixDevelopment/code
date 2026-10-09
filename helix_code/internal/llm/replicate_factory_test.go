package llm

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

// replicate_factory_test.go — §11.4.124 wiring-verification tests for the
// four never-wired provider packages investigated ahead of this change:
// cerebras / replicate / together / huggingface.
//
// The decision recorded here (and asserted, not merely claimed):
//   - cerebras / together / huggingface are ALREADY served through the
//     data-driven HostedOpenAICompatibleCatalogue() (openai_compatible_catalogue.go)
//     — the live path applications/terminal_ui and internal/clientcore both
//     consume. Wiring their bespoke internal/llm/providers/{cerebras,together,
//     huggingface} clients into factory.go/provider_factory.go on TOP of that
//     would create a SECOND, competing construction path for the same
//     provider name — a defect, not a feature (§11.4.6 honest-boundary +
//     the task's explicit instruction). They are therefore NOT added to
//     either factory switch; TestFactory_* below asserts that absence stays
//     true so a future edit cannot silently create the duplicate path
//     without a test noticing.
//   - replicate genuinely has NO live path (its wire protocol — an async
//     create-prediction-then-poll flow — is not the OpenAI chat-completions
//     contract every catalogue entry assumes), so it is wired for real in
//     replicate_provider.go and proven reachable end-to-end THROUGH the
//     factory (not by constructing the client directly) below.

// ---------------------------------------------------------------------------
// Replicate: genuinely wired — prove it end-to-end through the factory.
// ---------------------------------------------------------------------------

// newTestReplicatePredictionServer starts an httptest server implementing
// just enough of Replicate's create-prediction + poll-until-terminal wire
// protocol to prove a factory-constructed provider can drive a REAL HTTP
// round trip end to end (no mocked internals — a real net/http.Server, a
// real net/http.Client, real JSON encode/decode).
func newTestReplicatePredictionServer(t *testing.T, wantAuth string, output string) *httptest.Server {
	t.Helper()
	var calls int
	mux := http.NewServeMux()
	mux.HandleFunc("/models/", func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/predictions") || r.Method != http.MethodPost {
			http.NotFound(w, r)
			return
		}
		if wantAuth != "" && r.Header.Get("Authorization") != wantAuth {
			t.Errorf("prediction-create request Authorization = %q, want %q", r.Header.Get("Authorization"), wantAuth)
		}
		calls++
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":     "pred-123",
			"status": "processing",
		})
	})
	mux.HandleFunc("/predictions/pred-123", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.NotFound(w, r)
			return
		}
		if wantAuth != "" && r.Header.Get("Authorization") != wantAuth {
			t.Errorf("poll request Authorization = %q, want %q", r.Header.Get("Authorization"), wantAuth)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":     "pred-123",
			"status": "succeeded",
			"output": output,
		})
	})
	return httptest.NewServer(mux)
}

// TestFactory_ReplicateProvider_ReachableThroughFactory_Generate is the
// load-bearing anti-bluff test: it constructs the provider via NewProvider
// (the factory switch), NOT via any direct `&ReplicateProvider{...}` or
// direct client construction, and then drives a real Generate() call
// against a real httptest server — proving the factory path is genuinely
// wired end to end, not merely that the package compiles.
func TestFactory_ReplicateProvider_ReachableThroughFactory_Generate(t *testing.T) {
	// §11.4.120 gate reconciliation: NewProvider now enforces the W2c-1 cloud
	// gate (factory.go), closing the ungated back door around NewCloudProvider's
	// guard. This test's subject is provider CONSTRUCTION and behaviour, which
	// predates the gate — so the refusal it now hits is the fix working, not a
	// regression, and the honest response is to open the gate here rather than
	// weaken either side. Gate POLICY itself is asserted by
	// cloud_gate_closed_test.go / cloud_gate_open_test.go.
	openCloudGateForTest(t)

	server := newTestReplicatePredictionServer(t, "Bearer factory-test-key", "hello from replicate")
	defer server.Close()

	provider, err := NewProvider(ProviderConfigEntry{
		Type:     ProviderTypeReplicate,
		APIKey:   "factory-test-key",
		Endpoint: server.URL,
		Enabled:  true,
	})
	if err != nil {
		t.Fatalf("NewProvider(ProviderTypeReplicate) failed: %v", err)
	}
	if provider.GetType() != ProviderTypeReplicate {
		t.Fatalf("GetType() = %q, want %q", provider.GetType(), ProviderTypeReplicate)
	}

	rp, ok := provider.(*ReplicateProvider)
	if !ok {
		t.Fatalf("NewProvider(ProviderTypeReplicate) returned %T, want *ReplicateProvider", provider)
	}
	rp.pollInterval = 5 * time.Millisecond // speed up the terminal-status poll for the test

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	resp, err := provider.Generate(ctx, &LLMRequest{
		ID:       uuid.New(),
		Model:    "meta/meta-llama-3-70b-instruct",
		Messages: []Message{{Role: "user", Content: "say hi"}},
	})
	if err != nil {
		t.Fatalf("Generate() through the factory-constructed provider failed: %v", err)
	}
	if resp.Content != "hello from replicate" {
		t.Fatalf("Generate() Content = %q, want %q (real HTTP round trip via the factory-constructed provider)", resp.Content, "hello from replicate")
	}
	if resp.Err != nil {
		t.Fatalf("Generate() Err = %v, want nil for a succeeded prediction", resp.Err)
	}
}

// TestFactory_ReplicateProvider_EnvKeyResolution proves config/env
// resolution actually works THROUGH the factory: no APIKey in the config,
// only the REPLICATE_API_KEY env var, and the real HTTP request the
// factory-constructed provider makes must carry that env-sourced key.
func TestFactory_ReplicateProvider_EnvKeyResolution(t *testing.T) {
	// §11.4.120 gate reconciliation: NewProvider now enforces the W2c-1 cloud
	// gate (factory.go), closing the ungated back door around NewCloudProvider's
	// guard. This test's subject is provider CONSTRUCTION and behaviour, which
	// predates the gate — so the refusal it now hits is the fix working, not a
	// regression, and the honest response is to open the gate here rather than
	// weaken either side. Gate POLICY itself is asserted by
	// cloud_gate_closed_test.go / cloud_gate_open_test.go.
	openCloudGateForTest(t)

	t.Setenv("REPLICATE_API_KEY", "env-sourced-key")
	t.Setenv("REPLICATE_API_TOKEN", "")
	t.Setenv("ApiKey_Replicate", "")

	server := newTestReplicatePredictionServer(t, "Bearer env-sourced-key", "env resolved")
	defer server.Close()

	provider, err := NewProvider(ProviderConfigEntry{
		Type:     ProviderTypeReplicate,
		Endpoint: server.URL,
		Enabled:  true,
	})
	if err != nil {
		t.Fatalf("NewProvider(ProviderTypeReplicate) with no config.APIKey failed to resolve from env: %v", err)
	}
	rp := provider.(*ReplicateProvider)
	rp.pollInterval = 5 * time.Millisecond

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	resp, err := provider.Generate(ctx, &LLMRequest{ID: uuid.New(), Messages: []Message{{Role: "user", Content: "hi"}}})
	if err != nil {
		t.Fatalf("Generate() failed: %v", err)
	}
	if resp.Content != "env resolved" {
		t.Fatalf("Generate() Content = %q, want %q", resp.Content, "env resolved")
	}
}

// TestFactory_NewProvider_ReplicateMissingKey_Errors is the anti-bluff
// negative case: with no config key and no recognised env alias present,
// the factory MUST refuse to construct a provider that could never
// authenticate — never a silently-broken provider.
func TestFactory_NewProvider_ReplicateMissingKey_Errors(t *testing.T) {
	// §11.4.120 gate reconciliation: with the W2c-1 cloud gate CLOSED this test
	// would still PASS — but for the wrong reason. It asserts only "an error
	// occurred", and the gate's ErrCloudDisabled satisfies that while the error
	// this test actually exists to observe (a Replicate key absent from every env alias) would never be
	// reached. Opening the gate keeps the assertion about its real subject.
	openCloudGateForTest(t)

	t.Setenv("REPLICATE_API_KEY", "")
	t.Setenv("REPLICATE_API_TOKEN", "")
	t.Setenv("ApiKey_Replicate", "")

	_, err := NewProvider(ProviderConfigEntry{Type: ProviderTypeReplicate, Enabled: true})
	if err == nil {
		t.Fatal("NewProvider(ProviderTypeReplicate) with no key anywhere: want error, got nil")
	}
}

// TestFactory_ReplicateProvider_FailedPrediction_PopulatesErr proves the
// factory-constructed provider surfaces ErrReplicatePredictionFailed on a
// real failed-status prediction, matching the sibling bespoke client's
// round-54 contract (mapReplicateStatusToLLMErr mirrors
// providers/replicate.mapReplicateStatusToErr).
func TestFactory_ReplicateProvider_FailedPrediction_PopulatesErr(t *testing.T) {
	// §11.4.120 gate reconciliation: NewProvider now enforces the W2c-1 cloud
	// gate (factory.go), closing the ungated back door around NewCloudProvider's
	// guard. This test's subject is provider CONSTRUCTION and behaviour, which
	// predates the gate — so the refusal it now hits is the fix working, not a
	// regression, and the honest response is to open the gate here rather than
	// weaken either side. Gate POLICY itself is asserted by
	// cloud_gate_closed_test.go / cloud_gate_open_test.go.
	openCloudGateForTest(t)

	mux := http.NewServeMux()
	mux.HandleFunc("/models/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]any{"id": "pred-fail", "status": "processing"})
	})
	mux.HandleFunc("/predictions/pred-fail", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"id": "pred-fail", "status": "failed", "error": "boom"})
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	provider, err := NewProvider(ProviderConfigEntry{
		Type:     ProviderTypeReplicate,
		APIKey:   "k",
		Endpoint: server.URL,
		Enabled:  true,
	})
	if err != nil {
		t.Fatalf("NewProvider failed: %v", err)
	}
	rp := provider.(*ReplicateProvider)
	rp.pollInterval = 5 * time.Millisecond

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	resp, err := provider.Generate(ctx, &LLMRequest{ID: uuid.New(), Messages: []Message{{Role: "user", Content: "hi"}}})
	if err != nil {
		t.Fatalf("Generate() returned a hard error, want a response with Err set: %v", err)
	}
	if !errors.Is(resp.Err, ErrReplicatePredictionFailed) {
		t.Fatalf("resp.Err = %v, want errors.Is(..., ErrReplicatePredictionFailed)", resp.Err)
	}
}

// TestFactory_ReplicateProvider_GetHealth exercises a real (non-billed)
// authenticated GET against the provider's account endpoint through the
// factory-constructed provider.
func TestFactory_ReplicateProvider_GetHealth(t *testing.T) {
	// §11.4.120 gate reconciliation: NewProvider now enforces the W2c-1 cloud
	// gate (factory.go), closing the ungated back door around NewCloudProvider's
	// guard. This test's subject is provider CONSTRUCTION and behaviour, which
	// predates the gate — so the refusal it now hits is the fix working, not a
	// regression, and the honest response is to open the gate here rather than
	// weaken either side. Gate POLICY itself is asserted by
	// cloud_gate_closed_test.go / cloud_gate_open_test.go.
	openCloudGateForTest(t)

	mux := http.NewServeMux()
	mux.HandleFunc("/account", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer k" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.WriteHeader(http.StatusOK)
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	provider, err := NewProvider(ProviderConfigEntry{
		Type:     ProviderTypeReplicate,
		APIKey:   "k",
		Endpoint: server.URL,
		Enabled:  true,
	})
	if err != nil {
		t.Fatalf("NewProvider failed: %v", err)
	}

	health, err := provider.GetHealth(context.Background())
	if err != nil {
		t.Fatalf("GetHealth() failed: %v", err)
	}
	if health.Status != "healthy" {
		t.Fatalf("GetHealth().Status = %q, want %q", health.Status, "healthy")
	}
}

// TestProviderFactory_NewCloudProvider_CreatesReplicateProvider proves the
// CLI/wizard cloud-selector path (provider_factory.go) also constructs a
// working Replicate provider, and that "replicate" resolves through
// ParseCloudProviderType / Select exactly like every other supported alias.
func TestProviderFactory_NewCloudProvider_CreatesReplicateProvider(t *testing.T) {
	openCloudGateForTest(t)
	pt, err := ParseCloudProviderType("replicate")
	if err != nil {
		t.Fatalf("ParseCloudProviderType(\"replicate\") failed: %v", err)
	}
	if pt != ProviderTypeReplicate {
		t.Fatalf("ParseCloudProviderType(\"replicate\") = %q, want %q", pt, ProviderTypeReplicate)
	}

	selected, err := Select(SelectorInput{Flag: "replicate"})
	if err != nil {
		t.Fatalf("Select({Flag: \"replicate\"}) failed: %v", err)
	}
	if selected != ProviderTypeReplicate {
		t.Fatalf("Select({Flag: \"replicate\"}) = %q, want %q", selected, ProviderTypeReplicate)
	}

	provider, err := NewCloudProvider(ProviderTypeReplicate, ProviderConfigEntry{APIKey: "k"})
	if err != nil {
		t.Fatalf("NewCloudProvider(ProviderTypeReplicate, ...) failed: %v", err)
	}
	if provider.GetType() != ProviderTypeReplicate {
		t.Fatalf("GetType() = %q, want %q", provider.GetType(), ProviderTypeReplicate)
	}
}

// ---------------------------------------------------------------------------
// Cerebras / HuggingFace / Together: already served by the hosted catalogue
// — assert the catalogue serves them AND that the generic factories
// deliberately do NOT, so a future change cannot silently introduce a
// second, competing construction path for the same provider name.
// ---------------------------------------------------------------------------

func TestCatalogue_CerebrasHuggingFaceTogether_ServedByHostedCatalogue(t *testing.T) {
	names := map[string]bool{}
	for _, h := range HostedOpenAICompatibleCatalogue() {
		names[h.Name] = true
	}
	for _, want := range []string{"cerebras", "huggingface", "together"} {
		if !names[want] {
			t.Errorf("HostedOpenAICompatibleCatalogue() missing entry %q — the live catalogue path no longer serves it", want)
		}
	}
	// replicate's wire protocol (async prediction) is not the OpenAI
	// chat-completions contract every catalogue entry assumes — it must
	// NOT appear here (it has its own dedicated factory wiring instead).
	if names["replicate"] {
		t.Error(`HostedOpenAICompatibleCatalogue() unexpectedly contains "replicate" — its async-prediction wire protocol has no catalogue substitute; it must be constructed via NewReplicateProvider instead`)
	}
}

func TestFactory_CerebrasHuggingFaceTogether_NotDoubleWiredInGenericFactory(t *testing.T) {
	// §11.4.120 gate reconciliation: with the W2c-1 cloud gate CLOSED this test
	// would still PASS — but for the wrong reason. It asserts only "an error
	// occurred", and the gate's ErrCloudDisabled satisfies that while the error
	// this test actually exists to observe (no factory.go switch arm for these catalogue-served types) would never be
	// reached. Opening the gate keeps the assertion about its real subject.
	openCloudGateForTest(t)

	// §11.4.6 honest-boundary lock: these three are ALREADY reachable via
	// the hosted catalogue (asserted above). Adding them to factory.go's
	// switch on top of that would create TWO construction paths for the
	// same provider name — worse than the status quo, per the task's
	// explicit instruction. This test fails loudly if a future edit adds
	// that duplicate path without updating this decision record.
	for _, pt := range []ProviderType{ProviderTypeCerebras, ProviderTypeHuggingFace} {
		_, err := NewProvider(ProviderConfigEntry{Type: pt, APIKey: "k", Enabled: true})
		if err == nil {
			t.Errorf("NewProvider(%q) unexpectedly succeeded — %q is served by HostedOpenAICompatibleCatalogue(); "+
				"wiring it into factory.go too creates a second, competing construction path (see replicate_factory_test.go doc comment)", pt, pt)
		}
	}
	// "together" has no ProviderType constant at all (it was never even
	// declared beyond its bespoke, catalogue-superseded client) — confirm
	// that remains the case rather than silently gaining one.
	if _, err := NewProvider(ProviderConfigEntry{Type: ProviderType("together"), APIKey: "k", Enabled: true}); err == nil {
		t.Error(`NewProvider(ProviderType("together")) unexpectedly succeeded — "together" is served by HostedOpenAICompatibleCatalogue() and has no dedicated ProviderType constant`)
	}
}
