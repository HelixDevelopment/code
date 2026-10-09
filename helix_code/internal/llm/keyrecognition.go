package llm

import (
	"os"
	"strings"
)

// keyrecognition.go — decoupled multi-alias provider→env-var recognition table
// (SP1 plan T1.1.2, DECISION-4(b)).
//
// CONST-051(B) decoupling: this is a SELF-CONTAINED, data-only copy of the
// {provider → []envVarAlias} shape lifted from helix_agent
// `internal/verifier/provider_types.go:349` SupportedProviders.EnvVars. It does
// NOT import helix_agent (that would need the `replace dev.helix.agent`
// directive — D-7, owned by SP4/SP5 and not yet wired). When D-7 lands, this
// table should converge onto the shared substrate.
//   Catalogue-Check: extend helix_agent (provider_types.go SupportedProviders.EnvVars)
//
// CONST-036 note: this is a provider→credential-env-var map, NOT a model-metadata
// source. It answers only "does the user have a key for provider X?" — the
// authoritative WORKING-model list still flows through the verifier funnel.
//
// CONST-042/§12.1 no-secret-leak: this file NEVER logs or persists any key VALUE;
// it only reads presence + rejects placeholders.

// ProviderEnvAliases returns the decoupled multi-alias recognition table:
// for each supported provider, the ordered list of environment-variable names
// any of which, when set to a non-placeholder value, marks that provider's key
// as PRESENT.
//
// Returned as a fresh map each call so callers cannot mutate the canonical table.
func ProviderEnvAliases() map[ProviderType][]string {
	return map[ProviderType][]string{
		ProviderTypeOpenAI:     {"OPENAI_API_KEY", "ApiKey_OpenAI"},
		ProviderTypeAnthropic:  {"ANTHROPIC_API_KEY", "CLAUDE_API_KEY"},
		ProviderTypeGemini:     {"GEMINI_API_KEY", "GOOGLE_API_KEY", "ApiKey_Gemini"},
		ProviderTypeDeepSeek:   {"DEEPSEEK_API_KEY", "ApiKey_DeepSeek"},
		ProviderTypeMistral:    {"MISTRAL_API_KEY", "ApiKey_Mistral_AiStudio"},
		ProviderTypeGroq:       {"GROQ_API_KEY", "ApiKey_Groq"},
		ProviderTypeXAI:        {"XAI_API_KEY", "GROK_API_KEY", "ApiKey_xAI"},
		ProviderTypeOpenRouter: {"OPENROUTER_API_KEY", "ApiKey_OpenRouter"},
		ProviderTypeQwen:       {"QWEN_API_KEY", "DASHSCOPE_API_KEY", "ApiKey_Qwen"},
		ProviderTypeCerebras:   {"CEREBRAS_API_KEY", "ApiKey_Cerebras"},
		ProviderTypeReplicate:  {"REPLICATE_API_KEY", "REPLICATE_API_TOKEN", "ApiKey_Replicate"},
		ProviderTypeXiaomi:     {"XIAOMI_MIMO_API_KEY", "ApiKey_Xiaomi_MiMo"},
		ProviderTypeCopilot:    {"GITHUB_COPILOT_TOKEN", "COPILOT_API_KEY"},
	}
}

// PresentProviders scans the process environment and returns the set of
// providers whose key is recognised as PRESENT (any alias set to a
// non-placeholder, non-blank value).
//
// This is the key-presence gate the working-model funnel uses so a user who
// configured ONE provider key does not see EVERY provider's models.
func PresentProviders() map[ProviderType]bool {
	present := make(map[ProviderType]bool)
	for pt, aliases := range ProviderEnvAliases() {
		for _, alias := range aliases {
			v, ok := os.LookupEnv(alias)
			if !ok {
				continue
			}
			if isPlaceholderKey(v) {
				continue
			}
			present[pt] = true
			break
		}
	}
	return present
}

// PresentProviderNames is the string-keyed view of PresentProviders, suitable
// for direct use as the `present` argument of
// verifier.(*Adapter).GetWorkingModels (whose signature is
// map[string]bool keyed by provider name, e.g. "anthropic", "openai").
//
// ProviderType's underlying type is string with values that string-equal the
// verifier's VerifiedModel.Provider field, so this is a faithful, lossless
// key conversion — it is the single bridge both the CLI model-listing path and
// the server model-listing path consume so the working-model funnel
// (key-present ∧ Verified ∧ status=="verified" ∧ score>=min) is exercised
// identically across both surfaces (no duplicated provider→env table).
func PresentProviderNames() map[string]bool {
	src := PresentProviders()
	out := make(map[string]bool, len(src))
	for pt, ok := range src {
		if ok {
			out[string(pt)] = true
		}
	}
	return out
}

// IsProviderKeyPresent reports whether a single provider's key is recognised.
func IsProviderKeyPresent(pt ProviderType) bool {
	aliases, ok := ProviderEnvAliases()[pt]
	if !ok {
		return false
	}
	for _, alias := range aliases {
		if v, ok := os.LookupEnv(alias); ok && !isPlaceholderKey(v) {
			return true
		}
	}
	return false
}

// placeholderTokens are case-insensitive substrings that mark a value as a
// placeholder rather than a real credential. Lifted (decoupled) from
// helix_agent startup.go:1703 isPlaceholder.
var placeholderTokens = []string{
	"your-api-key", "your_api_key", "api_key_here", "insert_key",
	"placeholder", "changeme", "example-key",
	"sk-xxx", // specific placeholder marker; bare "xxx" removed (a real key may contain it)
}

// isPlaceholderKey reports whether a value is blank/whitespace-only OR matches a
// known placeholder token. A placeholder is NOT a working key (§11.4 anti-bluff).
func isPlaceholderKey(value string) bool {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return true
	}
	lower := strings.ToLower(trimmed)
	for _, p := range placeholderTokens {
		if strings.Contains(lower, p) {
			return true
		}
	}
	return false
}
