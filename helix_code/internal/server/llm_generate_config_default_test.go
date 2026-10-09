package server

import (
	"os"
	"testing"
)

// llm_generate_config_default_test.go — §11.4.115 RED→GREEN polarity
// regression guard for HXC-002-F3-01 (gap ledger docs/qa/2026-09-05-gap-ledger.md):
//
// Config `llm.default_provider` ships as "local" (internal/config/config.go
// SetDefault), but resolveLLMProvider historically hardcoded
// SelectorInput.Config = "", so a provider-less request silently fell through
// to the Ollama default on :11434 while the config-declared "local" route (the
// llama.cpp coder sidecar on :18434) was reachable only by explicitly naming
// "local"/"helixllm".
//
// The config SOURCE is the package-level configDefaultProviderFunc var (the
// same test-injection pattern as llmProviderResolver): this test pins it to
// the value a config file carrying llm.default_provider: "local" would
// supply. The config-file→default loading itself is guarded in
// internal/config (SetDefault + YAML parse tests); the defect under test
// HERE is the threading of that value into SelectorInput.Config — on pre-fix
// code the pinned value was ignored and the request fell to Ollama.
// Pinning (rather than reading the real config) also keeps the guard
// deterministic regardless of suite order or operator-machine config state:
// config.Get() memoizes process-wide, and an earlier test's memoized load
// must not decide this guard's verdict.
//
// §11.4.115 polarity design (both branches drive the REAL threading line):
// BOTH modes pin configDefaultProviderFunc to the SAME real, non-empty
// declared default ("local") and drive the SAME real code path in
// resolveLLMProvider (sel.Config = strings.TrimSpace(configDefaultProviderFunc())
// followed by the local-route dispatch below it) — they differ ONLY in
// which resulting route they assert is correct. Pinning to "" instead (an
// earlier draft of this polarity switch, and the technique suggested at
// dispatch time) was considered and REJECTED: with a genuinely empty config
// default, falling back to Ollama is the CORRECT behaviour under BOTH the
// historical hardcoded-"" code and today's fixed code — an assertion built
// on an empty pin therefore cannot tell "config ignored" (the actual
// historical defect) apart from "config genuinely absent" (never a defect):
// it would pass either way and prove nothing, which is exactly the
// decorative-test failure mode §11.4.115 forbids ("a RED branch that tests
// a copy of the bug proves nothing"). Feeding a REAL non-empty declared
// default through the SAME real threading line and asserting on the
// resulting route is what actually discriminates fixed code from broken
// code.
//
// RED_MODE=1: reproduces the defect on the pre-fix artifact — historically
// SelectorInput.Config was hardcoded "" regardless of what
// configDefaultProviderFunc returned, so resolveLLMProvider("","") always
// fell through to the Ollama fallback even with a "local" default pinned.
// This branch asserts exactly that Ollama-fallback outcome. On fixed code
// this branch is DESIGNED to fail — the fix threads configDefaultProviderFunc()
// into SelectorInput.Config, so a pinned "local" now correctly resolves to
// the coder route instead. That failure-on-fixed-code is the intended,
// designed behaviour of this branch (the point of a RED test per §11.4.115)
// and is NOT asserted here as an observed fact: no build or test run was
// performed while authoring this file — see the accompanying task report
// for the anti-bluff disclosure.
// RED_MODE=0 or unset (default): the standing GREEN regression guard — the
// same real code path, same "local" pin, asserting the request lands on the
// coder route (the fix's intended, correct behaviour).
//
// One source, two roles (§11.4.115): this test IS the bug-catcher and the
// regression guard.
func TestResolveLLMProvider_ConfigDefaultLocalRoutesToCoder(t *testing.T) {
	redMode := os.Getenv("RED_MODE") == "1" // default (unset or "0"): GREEN guard

	// Pin every higher-precedence source empty so ONLY the config default
	// can name the provider (flag > env > config; here flag and env are both
	// deliberately blank).
	t.Setenv("HELIX_LLM_PROVIDER", "")

	// Pin the coder ENDPOINT too (§11.4.50 determinism). This test's verdict
	// must depend on the code under test, never on the machine it runs on,
	// and HELIX_LLM_LOCAL_OPENAI_ENDPOINT is read by
	// envHelixLLMLocalEndpointWithSource on the very path being exercised. An
	// operator whose shell points the coder route at a NON-LOOPBACK host
	// (a shared box on the LAN, a tunnel) makes this test fail without any
	// defect: NewOpenAICompatibleProvider self-gates on endpoint locality, so
	// resolveHelixLLMLocalProvider returns an error and the t.Fatalf below
	// fires on the resolve — reporting a routing bug that does not exist.
	// Pinning to the compiled-in default (the same constant the resolver
	// falls back to when the variable is unset) keeps the assertion about
	// routing rather than about host state, and matches how the sibling
	// gateway-route guard pins its own sources.
	t.Setenv(helixLLMLocalOpenAIEndpointEnv, helixLLMLocalDefaultEndpoint)

	// Pin the config source to the value this guard protects: a config file
	// declaring llm.default_provider: "local". Production reads the real
	// config via the same var; restoring it keeps the suite order-independent.
	// Pinned to the SAME value in BOTH modes — see the §11.4.115 polarity
	// design note above for why RED_MODE does NOT pin to "" instead.
	prev := configDefaultProviderFunc
	configDefaultProviderFunc = func() string { return "local" }
	t.Cleanup(func() { configDefaultProviderFunc = prev })

	provider, err := resolveLLMProvider("", "")
	if err != nil {
		t.Fatalf("resolveLLMProvider(\"\", \"\") failed: %v", err)
	}
	defer func() { _ = provider.Close() }()

	// Both the coder route (*llm.OpenAICompatibleProvider) and the Ollama
	// fallback (*llm.OllamaProvider) expose BaseURL(), so this type
	// assertion alone cannot distinguish fix from defect — only the
	// concrete endpoint value asserted below (per mode) can.
	bu, ok := provider.(interface{ BaseURL() string })
	if !ok {
		t.Fatalf("provider-less request constructed %T — neither the local "+
			"HelixLLM coder route nor the Ollama fallback exposes BaseURL()",
			provider)
	}

	if redMode {
		// RED_MODE=1: the historical defect is "config default silently
		// discarded, request falls to Ollama" — assert exactly that. Uses
		// the SAME envOllamaHost() helper the real Ollama-fallback
		// construction path (below resolveLLMProvider's local-route checks)
		// calls, so this stays correct regardless of HELIX_OLLAMA_HOST.
		want := envOllamaHost()
		if bu.BaseURL() != want {
			t.Fatalf("RED_MODE: BaseURL = %q, want the pre-fix Ollama fallback "+
				"%q — expected the historical defect (config default "+
				"silently discarded by a hardcoded empty "+
				"SelectorInput.Config) to be observable; got a different "+
				"route instead. On fixed code this branch is DESIGNED to "+
				"fail (see file header) because the fix correctly threads "+
				"the config default into provider selection",
				bu.BaseURL(), want)
		}
		return
	}

	// RED_MODE=0 (default): the standing GREEN regression guard.
	// The coder route is built on *llm.OpenAICompatibleProvider, which
	// exposes BaseURL(). The Ollama fallback constructs on :11434 — so on
	// pre-fix code this assertion fails with the Ollama endpoint, which is
	// exactly the defect HXC-002-F3-01 describes.
	if want := envHelixLLMLocalEndpoint(); bu.BaseURL() != want {
		t.Fatalf("BaseURL = %q, want the coder endpoint %q — config "+
			"llm.default_provider must resolve to the local route "+
			"(HXC-002-F3-01)", bu.BaseURL(), want)
	}
}
