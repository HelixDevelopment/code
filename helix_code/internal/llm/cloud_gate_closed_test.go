package llm

import (
	"errors"
	"testing"
)

// cloud_gate_closed_test.go — standing regression guard for the W2c-1 cloud
// gate (operator mandate 2026-09-05: local-only adaptive serving —
// everything served through HelixLLM runs locally; cloud providers disabled
// by default via the config key llm.cloud.enabled, default FALSE).
//
// §11.4.115 polarity is DELIBERATELY NOT implemented in this file — see the
// explanation below. This test asserts the gate's steady-state behaviour
// only: with the process-wide cloudGate at its zero value (closed, the
// mandated default), NewCloudProvider refuses every hosted provider type
// with ErrCloudDisabled while the two local types (Ollama, LlamaCpp) remain
// exempt and construct normally.
//
// Why no RED_MODE here (§11.4.115 honest-boundary note): the historical
// defect this guards against was the total ABSENCE of a gate check inside
// NewCloudProvider — hosted providers constructed unconditionally whenever a
// key was present, regardless of any config value. The fix
// (provider_factory.go:124) added an UNCONDITIONAL guard clause —
// `if !cloudGate.Load() && !isLocalProviderType(t) { return
// nil, fmt.Errorf(...ErrCloudDisabled...) }` — gated solely by the
// process-wide cloudGate var, checked before any per-type construction arm.
// On the fixed source there is no legitimate real-input toggle that
// reproduces "gate closed, but a hosted provider still constructs anyway":
// the only lever available from a test in this package is
// SetCloudEnabled(true), which OPENS the real gate — that is the intended,
// correct "operator explicitly enabled cloud" behaviour, not the historical
// defect (see the sibling cloud_gate_open_test.go, which already exercises
// exactly that case). Reproducing the true historical defect (an
// unconditional guard clause that is simply absent from the source) is only
// possible by deleting that clause from provider_factory.go itself — out of
// scope for a test file, and doing so via a locally re-implemented replica
// of the pre-fix logic is exactly what §11.4.115 forbids ("a RED branch
// that tests a copy of the bug proves nothing and is itself the bluff"). Per
// the honest-boundary escape clause, this file does not claim a polarity
// switch it cannot honestly implement, rather than leave an unbacked
// §11.4.115 claim in its header (as the prior revision of this file did: it
// read a RED_MODE env var into a local, then discarded it with `_ =
// redMode`, so both "modes" ran the identical assertions below — a
// documentation bluff this revision removes).
func TestNewCloudProvider_RefusesCloudProvidersWhenGateClosed(t *testing.T) {
	// ESTABLISH THE PRECONDITION, do not inherit it. This test's entire
	// subject is "gate CLOSED", and until this call it relied on the package
	// zero value of cloudGate being false and on every sibling that opens the
	// gate restoring it on cleanup. That happens to hold today — but it makes
	// the precondition a property of OTHER files: one t.Cleanup dropped
	// anywhere in the package, or one test that opens the gate without
	// restoring, and this test silently changes meaning (every hosted
	// construction would succeed and the assertions below would fail with a
	// confusing "constructed although the gate is closed" — pointing at the
	// gate rather than at the leaked state that actually broke it).
	// closeCloudGateForTest (cloud_gate_endpoint_locality_test.go) sets the
	// gate closed and restores the prior value on cleanup, so the precondition
	// is now stated here, in the test that depends on it.
	closeCloudGateForTest(t)

	cloudTypes := []ProviderType{
		ProviderTypeAnthropic,
		ProviderTypeOpenAI,
		ProviderTypeGemini,
	}
	for _, pt := range cloudTypes {
		_, err := NewCloudProvider(pt, ProviderConfigEntry{Type: pt, Enabled: true})
		if err == nil {
			t.Fatalf("%s constructed although the cloud gate is closed "+
				"(llm.cloud.enabled defaults false; operator mandate 2026-09-05 "+
				"local-only serving): hosted provider construction must refuse "+
				"even when API keys are present", pt)
		}
		// errors.Is on the sentinel, NOT a substring match on the rendered
		// message: a message-text assertion passes for ANY error whose prose
		// happens to contain "disabled" (a provider reporting "model disabled
		// by the upstream account", say) and so cannot distinguish a gate
		// refusal from an unrelated failure — it would keep reporting PASS if
		// the gate were removed and replaced by any similarly-worded error.
		// The sentinel is the identity callers actually branch on
		// (applications/terminal_ui/env_providers.go does exactly this).
		if !errors.Is(err, ErrCloudDisabled) {
			t.Fatalf("%s refusal error = %v, want it to wrap ErrCloudDisabled", pt, err)
		}
	}

	// Local types MUST still construct with the gate closed — the gate exists
	// to keep local serving working, not to break it.
	localTypes := []ProviderType{ProviderTypeOllama, ProviderTypeLlamaCpp}
	for _, pt := range localTypes {
		prov, err := NewCloudProvider(pt, ProviderConfigEntry{Type: pt, Enabled: true})
		if err != nil {
			t.Fatalf("local provider %s refused by the cloud gate: %v (the gate must "+
				"only block hosted providers)", pt, err)
		}
		if prov != nil {
			_ = prov.Close()
		}
	}
}

// TestNewProvider_RefusesHostedProvidersWhenGateClosed closes the SECOND
// bypass: NewProvider (factory.go) is the package's catch-all constructor and
// it built twelve hosted backends — OpenAI, Anthropic, Gemini, Qwen, XAI,
// OpenRouter, Copilot, Azure, Bedrock, VertexAI, Groq, Replicate — with NO
// gate check at all, while provider_factory.go's NewCloudProvider comment
// claimed the gate is "checked BEFORE any per-type switch arm so no hosted arm
// can construct by a back door". NewProvider WAS that back door: an exported
// symbol of the gated package, pointed at by provider_factory.go's own doc
// comments and advertised by doc.go as the canonical constructor.
//
// Honest scope note (§11.4.6): no PRODUCTION caller reached it at the time
// this guard was written — InitializeModelManager is its only in-tree caller
// and has no non-test callers — so the hole was LATENT, not live. A latent
// hole in exported API is still a hole: the next caller wired to the
// "canonical constructor" would have inherited an ungated path silently.
//
// Anthropic is used as the representative hosted type because it needs no
// per-provider environment setup to reach the gate; the gate is checked before
// any credential resolution, so this test neither needs nor uses a key.
func TestNewProvider_RefusesHostedProvidersWhenGateClosed(t *testing.T) {
	closeCloudGateForTest(t) // shared helper, cloud_gate_endpoint_locality_test.go

	prov, err := NewProvider(ProviderConfigEntry{
		Type:    ProviderTypeAnthropic,
		Enabled: true,
	})
	if err == nil {
		if prov != nil {
			_ = prov.Close()
		}
		t.Fatal("NewProvider(anthropic) CONSTRUCTED a hosted provider although the " +
			"cloud gate is closed — NewProvider is an ungated back door around the " +
			"guard NewCloudProvider applies")
	}
	if !errors.Is(err, ErrCloudDisabled) {
		t.Fatalf("NewProvider(anthropic) refusal error = %v, want it to wrap "+
			"ErrCloudDisabled so callers branch on the SAME sentinel they use for "+
			"NewCloudProvider", err)
	}
	if prov != nil {
		t.Fatal("NewProvider returned a non-nil provider alongside the gate refusal — " +
			"a refused construction must yield nothing usable")
	}
}

// TestNewProvider_LocalTypesExemptWhenGateClosed is the paired local-serving
// guard for the gate added to NewProvider: closing the back door must not close
// the local routes the gate exists to protect. Ollama and llama.cpp are exempt
// by provider IDENTITY (the same rule NewCloudProvider applies), and the
// OpenAI-compatible family is exempt at THIS layer because it self-gates one
// level down on endpoint LOCALITY inside NewOpenAICompatibleProvider — so a
// local vLLM constructs while a hosted one is still refused there.
//
// RECONCILED (§11.4.120): the KoboldAI row below is retained but its meaning
// has CHANGED, and the change is the point. KoboldAI was originally listed here
// as identity-exempt, which made this row pass trivially — it would have passed
// for ANY endpoint, including a hosted one, because nothing gated KoboldAI at
// all. KoboldAI now self-gates on endpoint locality inside NewKoboldAIProvider
// (it attaches an `Authorization: Bearer` credential, so identity-exemption was
// a credential-leak primitive), which makes this row ENDPOINT-DEPENDENT: it
// asserts that a KoboldAI provider at 127.0.0.1 still constructs, and it would
// now FAIL if the new gate were over-broad enough to refuse a local one. The
// matching negative — a KoboldAI provider at a REMOTE endpoint must be REFUSED
// — is asserted in cloud_gate_koboldai_test.go, which also covers the
// empty-endpoint fallback to KoboldAI's own localhost default. The two files
// together are what make this row meaningful rather than vacuous.
func TestNewProvider_LocalTypesExemptWhenGateClosed(t *testing.T) {
	closeCloudGateForTest(t)

	locals := []ProviderConfigEntry{
		{Type: ProviderTypeOllama, Endpoint: "http://localhost:11434", Enabled: true},
		{Type: ProviderTypeLlamaCpp, Enabled: true},
		{Type: ProviderTypeKoboldAI, Endpoint: "http://127.0.0.1:5001", Enabled: true},
		// Self-gating OpenAI-compatible family, pointed at a loopback address
		// with nothing listening: the constructor's model-discovery probe is
		// best-effort and only logged, so a refused connection is not a
		// construction failure. What matters here is that the GATE does not
		// refuse.
		{Type: ProviderTypeVLLM, Endpoint: "http://127.0.0.1:1", Enabled: true},
	}

	for _, cfg := range locals {
		prov, err := NewProvider(cfg)
		if err != nil {
			t.Fatalf("local provider %s (endpoint %q) refused with the gate closed: %v "+
				"— gating NewProvider must never break local serving", cfg.Type, cfg.Endpoint, err)
		}
		if prov == nil {
			t.Fatalf("local provider %s constructed nil without an error", cfg.Type)
		}
		_ = prov.Close()
	}
}

// TestNewProvider_HostedTypesConstructWhenGateOpen is the positive side that
// keeps the two assertions above honest: it proves the refusals are caused by
// the GATE and not by some unrelated failure that would reject these types in
// any state. With the gate open the same call must not be refused by the gate
// — it may still fail for a genuine provider-level reason, which is reported
// distinctly here.
func TestNewProvider_HostedTypesConstructWhenGateOpen(t *testing.T) {
	openCloudGateForTest(t) // shared helper, provider_factory_test.go

	prov, err := NewProvider(ProviderConfigEntry{
		Type:    ProviderTypeAnthropic,
		APIKey:  "test-key-not-a-real-credential",
		Enabled: true,
	})
	if err != nil && errors.Is(err, ErrCloudDisabled) {
		t.Fatalf("gate refused a hosted provider through NewProvider although "+
			"llm.cloud.enabled is true: %v", err)
	}
	if err != nil {
		t.Fatalf("NewProvider(anthropic) failed for a non-gate reason with the gate "+
			"open: %v — if this is the only way the closed-gate assertions can fail, "+
			"they prove nothing about the gate", err)
	}
	if prov == nil {
		t.Fatal("NewProvider(anthropic) constructed nil without an error with the gate open")
	}
	_ = prov.Close()
}
