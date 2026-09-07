package llm

import (
	"errors"
	"log"
	"strings"
	"time"

	"dev.helix.code/internal/verifier"
)

// dynamicProviderTimeout matches the hosted-catalogue provider timeout so a
// verifier-sourced provider behaves identically to a catalogue-sourced one.
const dynamicProviderTimeout = 120 * time.Second

// verifier_dynamic_catalogue.go — the DYNAMIC OpenAI-compatible provider builder.
//
// CONST-036 (LLMsVerifier is the single source of truth) + CONST-046 (no
// hardcoded model/URL in the primary path): this builder constructs each
// provider's *OpenAICompatibleProvider using the api_url FROM THE VERIFIER
// RECORD — there is NO hardcoded base URL anywhere here. The hardcoded
// HostedOpenAICompatibleCatalogue() in openai_compatible_catalogue.go is kept
// ONLY as a degraded offline fallback (used when the verifier is unreachable);
// THIS file is the primary path.
//
// CONST-042/§12.1 no-secret-leak: key resolution reuses keyrecognition.go's
// present/placeholder logic and NEVER logs or persists a key VALUE.

// dynamicNativelyWiredProviders are the providers HelixCode already wires through
// its dedicated direct-env-key path (registerEnvProviders). The dynamic builder
// MUST NOT duplicate them, or a single provider would be registered twice.
var dynamicNativelyWiredProviders = map[string]bool{
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
	"xiaomi":    true,
}

// DeriveKeyEnvAliases derives the ordered list of environment-variable names
// whose presence marks a provider's key as configured, purely from the provider
// NAME (CONST-046 — no hardcoded per-provider table on the primary path):
//
//  1. <UPPER_SNAKE>_API_KEY      — the canonical convention (e.g. cerebras → CEREBRAS_API_KEY)
//  2. ApiKey_<TitleCase>         — the legacy ~/api_keys.sh convention
//  3. any extra aliases the keyrecognition table already knows for that name
//     (so a provider with a non-obvious alias like ZHIPU_API_KEY still resolves)
//
// Non-alphanumeric characters in the name (hyphen, dot) become underscores in
// the uppercase form so e.g. "z-ai" → "Z_AI_API_KEY".
func DeriveKeyEnvAliases(name string) []string {
	upper := envSanitize(name)
	aliases := []string{
		upper + "_API_KEY",
		"ApiKey_" + titleCaseProvider(name),
	}

	// Fold in any extra aliases the hosted catalogue already documents for this
	// name (e.g. nvidia → NGC_API_KEY, zai → ZHIPU_API_KEY/GLM_API_KEY). This
	// REUSES that data without hardcoding a URL — only the key aliases are read.
	for _, h := range HostedOpenAICompatibleCatalogue() {
		if strings.EqualFold(h.Name, name) {
			aliases = append(aliases, h.KeyEnvAliases...)
		}
	}

	return dedupeStrings(aliases)
}

// BuildDynamicOpenAICompatibleProviders constructs, for every verifier provider
// record whose key is present (non-placeholder) AND that carries a verifier
// api_url AND that is not already natively wired, a real *OpenAICompatibleProvider
// whose BaseURL is the verifier's api_url. Providers with absent/placeholder
// keys, empty api_url, or natively-wired names are skipped (never built).
func BuildDynamicOpenAICompatibleProviders(recs []verifier.VerifierProvider) []Provider {
	built := make([]Provider, 0, len(recs))
	seen := map[string]bool{}
	// Tally of providers the W2c-1 cloud gate refused, reported as ONE summary
	// line after the loop. Silently dropping these (§11.4.6) left an operator
	// with a configured key watching a provider simply not appear, with nothing
	// anywhere saying why. One line per BUILD, not per provider, so a large
	// verifier catalogue cannot turn a deliberate policy into log spam. Mirrors
	// the pattern in applications/terminal_ui/env_providers.go.
	cloudGateRefused := 0
	for _, rec := range recs {
		name := strings.TrimSpace(rec.Name)
		if name == "" {
			continue
		}
		if dynamicNativelyWiredProviders[strings.ToLower(name)] {
			continue // keep its dedicated direct-env-key path; never double-register
		}
		if seen[strings.ToLower(name)] {
			continue
		}
		if strings.TrimSpace(rec.APIURL) == "" {
			continue // no verifier base URL → nothing to build on the no-hardcode path
		}

		apiKey, ok := firstPresentHostedKey(DeriveKeyEnvAliases(name))
		if !ok {
			continue // key absent / placeholder — skip, never build with a blank key
		}

		provider, err := NewOpenAICompatibleProvider(name, OpenAICompatibleConfig{
			BaseURL:          rec.APIURL, // ← the verifier's api_url, NOT a hardcoded literal
			APIKey:           apiKey,
			Timeout:          dynamicProviderTimeout,
			StreamingSupport: true,
			ModelEndpoint:    dynamicModelEndpoint(rec.APIURL),
			ChatEndpoint:     dynamicChatEndpoint(rec.APIURL, rec.Endpoint),
		})
		if err != nil {
			if errors.Is(err, ErrCloudDisabled) {
				// Deliberate operator policy (llm.cloud.enabled=false, the
				// default), not a construction defect: this provider's endpoint
				// is hosted and the gate is closed. Counted, not logged
				// individually, and never with the key value (CONST-042).
				cloudGateRefused++
			}
			continue
		}
		built = append(built, provider)
		seen[strings.ToLower(name)] = true
	}

	if cloudGateRefused > 0 {
		log.Printf("ℹ️  LLM: %d verifier-catalogue provider(s) with credentials present were NOT registered because llm.cloud.enabled is false (the default). Set llm.cloud.enabled: true to permit hosted providers — local routes (helixllm coder, llama.cpp, Ollama) are unaffected.", cloudGateRefused)
	}

	return built
}

// dynamicModelEndpoint mirrors the openai_compatible_catalogue.go URL-composition
// gotcha: getAPIURL composes TrimSuffix(BaseURL,"/") + endpoint, with a default
// endpoint of "/v1/models". When the verifier api_url ALREADY ends in a version
// segment (/v1, /v3, /v4, /paas/v4, …) we return "/models" so the URL does not
// become a doubled ".../v1/v1/models"; otherwise we return "" so the default
// "/v1/models" applies.
func dynamicModelEndpoint(baseURL string) string {
	if hasVersionSuffix(baseURL) {
		return "/models"
	}
	return ""
}

// dynamicChatEndpoint resolves the chat path. If the verifier supplies an
// explicit chat endpoint we honour it; otherwise we apply the same
// version-suffix composition rule as dynamicModelEndpoint.
func dynamicChatEndpoint(baseURL, verifierEndpoint string) string {
	if e := strings.TrimSpace(verifierEndpoint); e != "" {
		if !strings.HasPrefix(e, "/") {
			e = "/" + e
		}
		return e
	}
	if hasVersionSuffix(baseURL) {
		return "/chat/completions"
	}
	return ""
}

// hasVersionSuffix reports whether the (trailing-slash-trimmed) base URL's final
// path segment looks like an API version marker (v1, v2, v3, v4, …), in which
// case appending the default "/v1/..." endpoints would double the version.
func hasVersionSuffix(baseURL string) bool {
	trimmed := strings.TrimSuffix(baseURL, "/")
	idx := strings.LastIndex(trimmed, "/")
	if idx < 0 {
		return false
	}
	last := trimmed[idx+1:]
	if len(last) < 2 || last[0] != 'v' {
		return false
	}
	for _, r := range last[1:] {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// envSanitize converts a provider name into an UPPER_SNAKE env-var stem: every
// run of non-alphanumeric characters becomes a single underscore, uppercased.
func envSanitize(name string) string {
	var b strings.Builder
	prevUnderscore := false
	for _, r := range name {
		switch {
		case (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9'):
			b.WriteRune(r)
			prevUnderscore = false
		default:
			if !prevUnderscore {
				b.WriteByte('_')
				prevUnderscore = true
			}
		}
	}
	return strings.ToUpper(strings.Trim(b.String(), "_"))
}

// titleCaseProvider produces the ApiKey_<TitleCase> stem (e.g. "cerebras" →
// "Cerebras"). Multi-segment names join with the first letter of each segment
// capitalised (e.g. "z-ai" → "ZAi").
func titleCaseProvider(name string) string {
	fields := strings.FieldsFunc(name, func(r rune) bool {
		return !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9'))
	})
	var b strings.Builder
	for _, f := range fields {
		if f == "" {
			continue
		}
		b.WriteString(strings.ToUpper(f[:1]))
		if len(f) > 1 {
			b.WriteString(f[1:])
		}
	}
	return b.String()
}

// dedupeStrings removes duplicate aliases while preserving order.
func dedupeStrings(in []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(in))
	for _, s := range in {
		if s == "" || seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	return out
}
