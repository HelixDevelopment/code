package main

import (
	"os"
	"testing"

	"dev.helix.code/internal/llm"
)

// clearAllProviderKeys unsets every credential env var the auto-registration
// path recognises, so a test starts from a known no-key baseline regardless of
// the developer's shell environment. t.Setenv restores the prior values on
// cleanup.
func clearAllProviderKeys(t *testing.T) {
	t.Helper()
	for _, aliases := range llm.ProviderEnvAliases() {
		for _, alias := range aliases {
			if _, ok := os.LookupEnv(alias); ok {
				t.Setenv(alias, "")
			}
		}
	}
	// Also clear the hosted OpenAI-compatible catalogue's key aliases — the
	// operator's shell exports the full ~/api_keys.sh set, so without this the
	// catalogue providers would register from the inherited env and the
	// no-key-path assertions would not hold. Stays auto-synced with the catalogue.
	for _, h := range llm.HostedOpenAICompatibleCatalogue() {
		for _, alias := range h.KeyEnvAliases {
			if _, ok := os.LookupEnv(alias); ok {
				t.Setenv(alias, "")
			}
		}
	}
	// Pin the dynamic-catalogue verifier endpoint at an unreachable address so
	// these hermetic tests never contact a real LLMsVerifier that may be running
	// on the developer's machine (localhost:8095). With the verifier unreachable,
	// buildOpenAICompatibleProviders deterministically uses the offline fallback
	// catalogue path, whose providers are themselves key-gated by clearAllProviderKeys.
	t.Setenv("HELIX_VERIFIER_ENDPOINT", "http://127.0.0.1:1")
	// Pin the HelixAgent base URL at an unreachable address too, so the
	// no-key-path assertions hold even if a real HelixAgent server happens to be
	// running on the developer's machine (default localhost:7061). With it
	// unreachable, registerHelixAgentProvider deterministically returns 0 and
	// registers nothing.
	t.Setenv("HELIXAGENT_BASE_URL", "http://127.0.0.1:1")
}

// openCloudGateForTest opens the W2c-1 cloud gate (llm.cloud.enabled) for the
// duration of one test, exactly as the production TUI startup path now does
// from cfg.LLM.Cloud.Enabled before calling registerEnvProviders. Restores
// the PREVIOUS gate state (never a hardcoded value) via t.Cleanup so tests
// stay order-independent regardless of what ran before them — the identical
// idiom already used by internal/llm/provider_factory_test.go's own
// openCloudGateForTest helper.
func openCloudGateForTest(t *testing.T) {
	t.Helper()
	prev := llm.CloudEnabled()
	llm.SetCloudEnabled(true)
	t.Cleanup(func() { llm.SetCloudEnabled(prev) })
}

// TestRegisterEnvProviders_RegistersWhenKeyPresent proves that, when a provider
// credential env var is set AND the cloud gate is open, registerEnvProviders
// registers that provider and the manager's GetAvailableModels() returns a
// non-empty list.
//
// §11.4.120 reconciliation note: this test predates the W2c-1 cloud gate
// (internal/llm/provider_factory.go), whose default-closed policy now
// refuses hosted-provider construction unless llm.cloud.enabled is true. The
// test's ORIGINAL mechanism under verification — that a present credential
// drives real provider registration and a real, correctly-attributed model
// list — is still correct and still needs coverage, so the gate is opened
// explicitly here (mirroring what production startup does) rather than
// weakening or deleting the assertions below. Gate *policy* — that a closed
// gate refuses registration even with the same credential present — is
// asserted separately by the companion negative-polarity test
// TestRegisterEnvProviders_GateClosedRegistersNone.
//
// §1.1 paired-mutation framing: the GREEN assertion is count > 0 / models > 0.
// If registerEnvProviders were mutated to skip the RegisterProvider call (the
// "empty ModelManager" bug this fixes), the count would stay 0 and BOTH
// assertions below would FAIL — the test cannot pass on the broken behaviour.
func TestRegisterEnvProviders_RegistersWhenKeyPresent(t *testing.T) {
	clearAllProviderKeys(t)
	openCloudGateForTest(t)
	// A syntactically-valid, non-placeholder fake key. The provider constructs
	// from it (no network at construction — the seed model list is offline);
	// we assert on GetAvailableModels(), which returns the seed catalogue
	// without making a live call.
	t.Setenv("DEEPSEEK_API_KEY", "sk-test-deepseek-real-looking-credential-0123456789")

	manager := llm.NewModelManager()
	got := registerEnvProviders(manager, nil)

	if got < 1 {
		t.Fatalf("registerEnvProviders registered %d providers, want >= 1 when DEEPSEEK_API_KEY is set", got)
	}

	models := manager.GetAvailableModels()
	if len(models) == 0 {
		t.Fatalf("GetAvailableModels() returned 0 models after registering DeepSeek; want > 0 (the empty-ModelManager bug)")
	}

	// Anti-bluff: the registered models must actually belong to the provider we
	// registered — not a phantom/hardcoded entry from an unrelated source.
	foundDeepSeek := false
	for _, m := range models {
		if m.Provider == llm.ProviderTypeDeepSeek {
			foundDeepSeek = true
			break
		}
	}
	if !foundDeepSeek {
		t.Fatalf("GetAvailableModels() returned %d models but none from DeepSeek; provider was not really wired", len(models))
	}
}

// TestRegisterEnvProviders_GateClosedRegistersNone is the negative-polarity
// companion to TestRegisterEnvProviders_RegistersWhenKeyPresent, added per
// §11.4.120 reconciliation: with the exact same DeepSeek credential present
// but the W2c-1 cloud gate CLOSED (the mandated default — llm.cloud.enabled
// defaults false, operator mandate 2026-09-05, local-only adaptive serving),
// registerEnvProviders must register ZERO providers and the manager must
// report ZERO available models. This proves gate *policy* is enforced at the
// registration layer, not merely that construction happens to succeed when
// the gate is open.
func TestRegisterEnvProviders_GateClosedRegistersNone(t *testing.T) {
	clearAllProviderKeys(t)

	// Explicitly close the gate rather than relying on the package zero
	// value or prior-test ordering: snapshot the current value and restore
	// it on cleanup, so this test's own state is self-contained regardless
	// of what ran before it in the same process.
	prev := llm.CloudEnabled()
	llm.SetCloudEnabled(false)
	t.Cleanup(func() { llm.SetCloudEnabled(prev) })

	t.Setenv("DEEPSEEK_API_KEY", "sk-test-deepseek-real-looking-credential-0123456789")

	manager := llm.NewModelManager()
	got := registerEnvProviders(manager, nil)

	if got != 0 {
		t.Fatalf("registerEnvProviders registered %d providers with the cloud gate closed, want 0 (DEEPSEEK_API_KEY is present but llm.cloud.enabled=false must refuse hosted-provider construction)", got)
	}

	models := manager.GetAvailableModels()
	if len(models) != 0 {
		t.Fatalf("GetAvailableModels() returned %d models with the cloud gate closed, want 0", len(models))
	}

	// Anti-bluff follow-up: a zero total count alone does not prove the
	// picker is clean of the specific provider under test — walk whatever
	// models (if any) are present and assert none of them is DeepSeek,
	// closing the "some OTHER path silently registered a DeepSeek model
	// while the count still happens to read zero" gap.
	for _, m := range models {
		if m.Provider == llm.ProviderTypeDeepSeek {
			t.Fatalf("GetAvailableModels() returned a DeepSeek model (%+v) although the cloud gate is closed; hosted provider construction must not succeed through a back door", m)
		}
	}
}

// TestRegisterEnvProviders_RegistersNoneWhenUnset proves the honest no-key path:
// with every recognised credential env var cleared, registerEnvProviders
// registers ZERO providers and the manager reports ZERO available models — the
// picker honestly shows "no models available" rather than a fabricated list.
func TestRegisterEnvProviders_RegistersNoneWhenUnset(t *testing.T) {
	clearAllProviderKeys(t)

	manager := llm.NewModelManager()
	got := registerEnvProviders(manager, nil)

	if got != 0 {
		t.Fatalf("registerEnvProviders registered %d providers with all keys cleared, want 0", got)
	}
	if n := len(manager.GetAvailableModels()); n != 0 {
		t.Fatalf("GetAvailableModels() returned %d models with all keys cleared, want 0", n)
	}
}

// TestRegisterEnvProviders_NilManagerSafe guards the defensive nil path.
func TestRegisterEnvProviders_NilManagerSafe(t *testing.T) {
	if got := registerEnvProviders(nil, nil); got != 0 {
		t.Fatalf("registerEnvProviders(nil, nil) = %d, want 0", got)
	}
}
