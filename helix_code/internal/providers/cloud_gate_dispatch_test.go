// cloud_gate_dispatch_test.go — standing regression guard (§11.4.115 /
// §11.4.135) for HXC-002-F3-08's sibling defect HXC-002-F3-07: a hosted
// provider constructed on a path that never consults the process-global cloud
// gate.
//
// THE DEFECT. The W2c-1 cloud gate (operator mandate 2026-09-05,
// local-only-by-default serving) is enforced in exactly two places inside
// internal/llm: NewCloudProvider (provider_factory.go:357) and NewProvider
// (factory.go:113). Both refuse a hosted type with ErrCloudDisabled while
// llm.cloud.enabled is false — the default.
//
// (*AIIntegration).createAIProvider consulted NEITHER. Its dispatch switch
// reached the concrete hosted constructors directly, and three of its arms —
// and exactly three, established by reading every arm's body rather than by
// assuming which types "sound hosted" — delegate to a concrete hosted
// llm.New<X>Provider:
//
//	openai    -> providers.NewOpenAIProvider    -> llm.NewOpenAIProvider
//	anthropic -> providers.NewAnthropicProvider -> llm.NewAnthropicProvider
//	gemini    -> providers.NewGeminiProvider    -> llm.NewGeminiProvider
//
// The other ten arms (cohere, huggingface, mistral, gemma, llamaindex, memgpt,
// crewai, characterai, replika, anima) return newNotImplementedProvider — they
// construct nothing and dial nothing, so they are not gate-relevant and must
// keep constructing with the gate closed. TestCreateAIProvider_* asserts BOTH
// halves, because a gate that refuses everything is a §11.4.201 FAIL-bluff
// just as surely as one that refuses nothing is a PASS-bluff.
//
// WHY A GUARD FOR A LATENT PATH. Verified 2026-09-06 by execution: package
// internal/providers has exactly ONE non-test importer (internal/i18nwiring/
// wire.go:169) which uses exactly one symbol from it (SetTranslator, :530) and
// constructs no provider; NewAIIntegration — the only route to
// createAIProvider, via (*AIIntegration).Initialize:250 — has ZERO non-test
// callers. So no shipping path reaches the ungated dispatch today. That is
// precisely when the gap is cheapest to close and most likely to be forgotten:
// the day someone wires AIIntegration in, the gate is bypassed silently, with
// API keys in hand and no error to notice.
//
// FALSIFYING MUTATION (§1.1): delete the cloudGateRefusal block at the top of
// createAIProvider. The hosted subtests then get a nil error and FAIL.
// Inverting the predicate so it gates everything makes the local-arm positive
// control FAIL. Hard-refusing regardless of gate state makes the gate-open
// subtest FAIL. All three directions are covered.
package providers

import (
	"errors"
	"testing"

	"dev.helix.code/internal/llm"
	memproviders "dev.helix.code/internal/memory/providers"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// withCloudGate sets the process-global cloud gate for the duration of a test
// and restores the prior value. The gate is process-wide state (an atomic.Bool
// in internal/llm), so these subtests must not run in parallel with anything
// that reads it.
func withCloudGate(t *testing.T, enabled bool) {
	t.Helper()
	prev := llm.CloudEnabled()
	llm.SetCloudEnabled(enabled)
	t.Cleanup(func() { llm.SetCloudEnabled(prev) })
}

// hostedAIConfig is a provider config shaped the way a real operator config
// would arrive: enabled, with credentials present. Credentials present is the
// load-bearing part — the gate must refuse EVEN WITH a usable API key, which
// is the whole point of a local-only-serving control.
func hostedAIConfig(t memproviders.ProviderType) *AIProviderConfig {
	return &AIProviderConfig{
		Type:    t,
		Enabled: true,
		Model:   "test-model",
		Config: map[string]interface{}{
			"api_key": "test-key-not-a-real-credential",
		},
	}
}

func TestCreateAIProvider_RefusesHostedWhileCloudGateClosed(t *testing.T) {
	hosted := []memproviders.ProviderType{
		memproviders.ProviderTypeOpenAI,
		memproviders.ProviderTypeAnthropic,
		memproviders.ProviderTypeGemini,
	}

	for _, pt := range hosted {
		t.Run(string(pt), func(t *testing.T) {
			withCloudGate(t, false)

			ai := NewAIIntegration(nil)
			provider, err := ai.createAIProvider(hostedAIConfig(pt))

			require.Error(t, err,
				"hosted provider %q must NOT be constructible while the cloud gate "+
					"is closed — createAIProvider bypasses llm.NewCloudProvider and "+
					"llm.NewProvider, so it must apply the gate itself", pt)
			assert.True(t, errors.Is(err, llm.ErrCloudDisabled),
				"the refusal must carry the ONE gate error identity so callers can "+
					"branch on errors.Is(err, llm.ErrCloudDisabled); got %v", err)
			assert.Nil(t, provider,
				"a refused hosted provider must not be handed back to the caller")
		})
	}
}

// TestCreateAIProvider_ConstructsNonHostedWhileCloudGateClosed is the positive
// control: the gate must discriminate. gemma is dispatched to a
// newNotImplementedProvider arm that dials nothing, so a closed gate has no
// business refusing it — a local route must keep working under a control whose
// entire purpose is to keep serving local-by-default.
func TestCreateAIProvider_ConstructsNonHostedWhileCloudGateClosed(t *testing.T) {
	withCloudGate(t, false)

	ai := NewAIIntegration(nil)
	provider, err := ai.createAIProvider(&AIProviderConfig{
		Type:    memproviders.ProviderTypeGemma,
		Enabled: true,
	})

	require.NoError(t, err,
		"the cloud gate must not refuse a non-hosted dispatch arm — it exists to "+
			"keep local serving working, not to disable the dispatch wholesale")
	assert.NotNil(t, provider,
		"a non-hosted arm must still return a usable AIProvider with the gate closed")
	assert.False(t, errors.Is(err, llm.ErrCloudDisabled),
		"a non-hosted arm must never surface the cloud-gate error identity")
}

// TestCreateAIProvider_AllowsHostedWhenCloudGateOpen proves the check is a
// GATE and not a hard block: with llm.cloud.enabled true, the operator has
// explicitly permitted hosted providers and construction must proceed.
func TestCreateAIProvider_AllowsHostedWhenCloudGateOpen(t *testing.T) {
	withCloudGate(t, true)

	ai := NewAIIntegration(nil)
	provider, err := ai.createAIProvider(hostedAIConfig(memproviders.ProviderTypeOpenAI))

	// Construction may still degrade to a not-implemented placeholder offline
	// (providers.NewOpenAIProvider folds an llm-side init failure into one),
	// which is a nil-error path either way. The assertion that matters is that
	// no GATE refusal is raised once the operator has opened the gate.
	assert.NoError(t, err,
		"with the cloud gate OPEN the hosted arm must construct, not refuse")
	assert.False(t, errors.Is(err, llm.ErrCloudDisabled),
		"an open gate must never yield ErrCloudDisabled; got %v", err)
	assert.NotNil(t, provider,
		"an open gate must hand back a provider")
}

// TestExportedHostedConstructors_RespectCloudGateOnDirectCall covers the entry
// point the tests above do NOT: a caller outside this package that skips
// createAIProvider entirely and calls the EXPORTED constructor directly.
//
// THE GAP. The gate in createAIProvider protects the dispatch switch. It does
// nothing for `providers.NewOpenAIProvider(cfg)` — an exported symbol reachable
// from any package. Before the fix those three functions called the concrete
// llm.New<X>Provider, which is itself ungated (the gate lives in
// llm.NewProvider and llm.NewCloudProvider), so this path constructed a live
// hosted provider with a credential in hand and no gate anywhere on it.
// Latent, not shipped — internal/providers currently has one non-test importer
// and it constructs no provider — which is exactly why it was cheap to close
// and easy to forget.
//
// THE FIX under test: the three constructors now go through llm.NewProvider,
// the canonical gated factory, which dispatches to the same concrete
// constructors with the same config once the gate permits it.
//
// Their signature returns a bare AIProvider with no error, so a refusal
// necessarily surfaces as the NotImplementedProvider placeholder this file
// already uses for undelegated arms — this test asserts on the CONCRETE TYPE,
// which is what distinguishes "refused" from "constructed", rather than on
// message prose.
//
// FALSIFYING MUTATION (§1.1): change any of the three back to
// `llm.New<X>Provider(llmConfig)`. That constructor ignores the gate, so with
// a key present it succeeds, the function returns an *LLMProviderAdapter, and
// the gate-closed subtest FAILs.
func TestExportedHostedConstructors_RespectCloudGateOnDirectCall(t *testing.T) {
	ctors := map[string]func(*AIProviderConfig) AIProvider{
		"NewOpenAIProvider":    NewOpenAIProvider,
		"NewAnthropicProvider": NewAnthropicProvider,
		"NewGeminiProvider":    NewGeminiProvider,
	}
	types := map[string]memproviders.ProviderType{
		"NewOpenAIProvider":    memproviders.ProviderTypeOpenAI,
		"NewAnthropicProvider": memproviders.ProviderTypeAnthropic,
		"NewGeminiProvider":    memproviders.ProviderTypeGemini,
	}

	for name, ctor := range ctors {
		t.Run(name+"/gate closed refuses", func(t *testing.T) {
			withCloudGate(t, false)

			// Credentials present is load-bearing: the gate must refuse even
			// when the call would otherwise succeed. Without a key these
			// constructors fail for an unrelated reason and the subtest would
			// pass without testing the gate at all.
			provider := ctor(hostedAIConfig(types[name]))

			require.NotNil(t, provider, "constructors never return a nil AIProvider")
			_, isAdapter := provider.(*LLMProviderAdapter)
			assert.Falsef(t, isAdapter,
				"%s returned a live *LLMProviderAdapter with the cloud gate CLOSED and "+
					"an API key present — the direct-call path bypassed the gate. It "+
					"must route through llm.NewProvider, not llm.New<X>Provider", name)
			assert.IsTypef(t, &NotImplementedProvider{}, provider,
				"%s must degrade to the not-implemented placeholder when refused; got %T",
				name, provider)
		})

		t.Run(name+"/gate open constructs", func(t *testing.T) {
			// Positive control (§11.4.201): a gate that refuses in BOTH states
			// is not a gate, and routing through llm.NewProvider must not have
			// broken ordinary construction. llm.New<X>Provider performs no I/O
			// at construction — it only requires a key — so with the gate open
			// and a key supplied this must yield a real adapter offline.
			withCloudGate(t, true)

			provider := ctor(hostedAIConfig(types[name]))

			require.NotNil(t, provider)
			assert.IsTypef(t, &LLMProviderAdapter{}, provider,
				"%s must construct a real adapter once the operator has opened the "+
					"gate; got %T (routing through llm.NewProvider must be "+
					"behaviour-preserving apart from the gate)", name, provider)
		})
	}
}
