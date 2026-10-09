package cmd

import (
	"context"
	"strings"
	"testing"

	cmdi18n "dev.helix.code/cmd/i18n"
)

// generate_misconfig_preamble_test.go — guard for the
// cmd_generate_provider_misconfigured preamble.
//
// The defect: server.IsProviderMisconfiguration was widened to cover ENDPOINT
// faults as well as provider-selection faults, but the CLI preamble it drives
// still named only HELIX_LLM_PROVIDER / llm.default_provider /
// llm.cloud.enabled. A user running `helix generate --provider local` with a
// REMOTE HELIX_LLM_LOCAL_OPENAI_ENDPOINT was therefore sent to check three
// settings that are all innocent — misdirection, not merely an omission.
//
// The assertions below render through the REAL embedded bundle (not the
// sentinel/Noop translator), so they prove what a user actually sees, and they
// pin BOTH halves: the provider-selection keys must survive (the widening did
// not make them wrong) AND the endpoint keys must be named.
func TestGenerateMisconfiguredPreamble_NamesProviderAndEndpointKeys(t *testing.T) {
	tr8, err := cmdi18n.NewTranslator()
	if err != nil {
		t.Fatalf("real bundle failed to load: %v", err)
	}
	SetTranslator(tr8)
	t.Cleanup(func() { SetTranslator(nil) })

	const sentinelErr = "SENTINEL_DETAIL_LINE"
	got := tr(context.Background(), "cmd_generate_provider_misconfigured",
		map[string]any{"Error": sentinelErr})

	if got == "cmd_generate_provider_misconfigured" {
		t.Fatalf("key was not resolved by the real bundle (loud-echo fallback): %q", got)
	}
	for _, want := range []string{
		// Provider-selection settings — must NOT be lost by the widening.
		"HELIX_LLM_PROVIDER",
		"llm.default_provider",
		"llm.cloud.enabled",
		// Endpoint settings the widened classifier now also covers.
		"HELIX_LLM_LOCAL_OPENAI_ENDPOINT",
		"HELIX_LLM_GATEWAY_ENDPOINT",
		"HELIX_LLAMA_CPP_HOST",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("preamble does not name %q, so a user hitting that fault is misdirected.\nrendered: %s",
				want, got)
		}
	}
	// The Details suffix carries the specific cause; interpolation must survive.
	if !strings.Contains(got, sentinelErr) {
		t.Errorf("{{.Error}} interpolation lost — the specific cause never reaches the user.\nrendered: %s", got)
	}
}
