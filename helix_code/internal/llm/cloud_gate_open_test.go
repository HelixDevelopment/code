package llm

import (
	"errors"
	"testing"
)

// cloud_gate_open_test.go — positive-side guard for the W2c-1 cloud gate:
// when an operator explicitly enables llm.cloud.enabled, hosted provider
// construction must proceed past the gate (it may still fail on real missing
// credentials — that is provider-level honesty — but the refusal MUST NOT be
// the gate's).

func TestCloudGate_OpenPermitsCloudConstruction(t *testing.T) {
	// §11.4.120 test-isolation fix: restore the PREVIOUS gate state, not a
	// hardcoded `false`. A hardcoded restore is a latent test-isolation leak
	// the moment any outer test (or a future TestMain) opens the gate before
	// this test runs — this cleanup would then silently CLOSE a gate that was
	// open on entry, corrupting whatever ran after it. Snapshotting `prev`
	// makes this test's side effect on the shared package-global exactly
	// self-cancelling, regardless of what the gate's state was before it ran.
	prev := CloudEnabled()
	SetCloudEnabled(true)
	t.Cleanup(func() { SetCloudEnabled(prev) })

	if !CloudEnabled() {
		t.Fatalf("CloudEnabled() = false after SetCloudEnabled(true)")
	}

	// errors.Is on the sentinel rather than a substring match on the message:
	// a `strings.Contains(err.Error(), "disabled")` test cannot tell a gate
	// refusal apart from any other error whose prose happens to contain the
	// word, so it would keep passing even if the gate stopped using its own
	// sentinel. ErrCloudDisabled is the identity real callers branch on.
	_, err := NewCloudProvider(ProviderTypeAnthropic, ProviderConfigEntry{Type: ProviderTypeAnthropic, Enabled: true})
	if err != nil && errors.Is(err, ErrCloudDisabled) {
		t.Fatalf("gate refused a hosted provider although it is open: %v", err)
	}
}

// TestCloudGate_DefaultStateIsClosed reads the process-global cloudGate with
// no isolation of its own (no SetCloudEnabled / t.Cleanup pair) and is
// therefore order-dependent by construction: it only observes the mandated
// default (closed) correctly when every OTHER test in this package that
// flips the gate restores it afterwards. That save/restore discipline —
// applied consistently by every gate-flipping test in this package (see
// TestCloudGate_OpenPermitsCloudConstruction above and the
// openCloudGateForTest helpers in provider_factory_test.go and
// applications/terminal_ui/env_providers_test.go) — is what keeps this
// assertion meaningful; it is load-bearing, not incidental.
func TestCloudGate_DefaultStateIsClosed(t *testing.T) {
	// Package zero value (no SetCloudEnabled call in this test): closed.
	if CloudEnabled() {
		t.Fatalf("cloud gate default state must be CLOSED (llm.cloud.enabled " +
			"defaults false — operator mandate 2026-09-05)")
	}
}
