// Package clientcore holds the SHARED, UI-toolkit-agnostic capability core that
// every HelixCode client (terminal UI, desktop GUI, web, …) wires to reach
// parity. It contains NO tview/Fyne types — only plain function calls over the
// internal/{llm,agent,tools,mcp,plugins,verifier} helpers — so the same wiring
// is reused, never reimplemented (§11.4.74), and stays decoupled (CONST-051(B)).
//
// The provider half (this file) is the verifier-driven LLM provider
// registration path (CONST-036/037/040): it wires LLMsVerifier as the single
// source of truth, then registers every cloud provider whose API key is present
// in the environment, sources the OpenAI-compatible providers DYNAMICALLY from
// the verifier, registers the multi-provider Helix Agent ensemble, and registers
// the HelixAgent adapter when its server is reachable. NO hardcoded model list,
// NO fabricated provider — a no-key environment honestly registers zero
// providers (anti-bluff §11.4 / BLUFF-002).
//
// This is the exact logic the terminal UI used in applications/terminal_ui/
// env_providers.go (package-main, un-importable). It is promoted here so the
// desktop GUI calls the SAME code instead of its old hardcoded provider list.
package clientcore

import (
	"context"
	"errors"
	"log"
	"os"
	"strings"
	"time"

	"dev.helix.code/internal/config"
	"dev.helix.code/internal/llm"
	"dev.helix.code/internal/llm/providers/helixagent"
	"dev.helix.code/internal/verifier"
)

// WireVerifierAdapter bootstraps the LLMsVerifier subsystem (CONST-036/040 — the
// single source of truth for verified model/provider metadata + capability
// flags) and injects its Adapter into the llm package + ModelManager. This makes
// the Helix Agent ensemble resolve each member's model from VERIFIED,
// chat-capable catalogue entries instead of a blind catalogue[0] — fully
// dynamic, zero hardcoded model names. It is an honest no-op (the ensemble
// degrades to the provider's own capability-filtered catalogue) when the
// verifier is disabled/unreachable — never fabricated (§11.4.6). Returns true
// when the verifier adapter was actually wired.
func WireVerifierAdapter(manager *llm.ModelManager, cfg *config.Config) bool {
	if cfg == nil || cfg.Verifier == nil || !cfg.Verifier.Enabled {
		return false
	}
	res, err := verifier.Bootstrap(cfg.Verifier)
	if err != nil || res == nil || res.Adapter == nil {
		if err != nil {
			log.Printf("⚠️  clientcore: verifier bootstrap failed: %v (ensemble uses capability-filtered catalogue)", err)
		}
		return false
	}
	llm.SetVerifierAdapter(res.Adapter)
	if manager != nil {
		manager.SetVerifierAdapter(res.Adapter)
	}
	return true
}

// envProviderCandidate is one cloud provider that can be constructed from an API
// key alone (no extra endpoint/region/project wiring).
type envProviderCandidate struct {
	providerType llm.ProviderType
}

// envProviderCandidates is the ordered, curated set of cloud providers
// auto-registered from environment keys. Order is deterministic so the captured-
// evidence probe and any digit-shortcut picker stay stable across runs.
//
// Scope note: only key-only constructible providers are listed. Bedrock,
// VertexAI, and Azure are intentionally excluded — they require region /
// project / deployment wiring beyond a bare API key, so auto-registering them
// from a single env var would produce a half-configured provider (an anti-bluff
// violation). Those remain reachable via explicit config.yaml / the server path.
var envProviderCandidates = []envProviderCandidate{
	{llm.ProviderTypeDeepSeek},
	{llm.ProviderTypeMistral},
	{llm.ProviderTypeGroq},
	{llm.ProviderTypeOpenRouter},
	{llm.ProviderTypeOpenAI},
	{llm.ProviderTypeAnthropic},
	{llm.ProviderTypeXAI},
	{llm.ProviderTypeQwen},
}

// RegisterEnvProviders registers, into manager, every cloud provider in
// envProviderCandidates whose API key is present in the environment (looked up
// via llm.IsProviderKeyPresent, which honours the multi-alias table and rejects
// placeholder values), then the verifier-sourced OpenAI-compatible providers,
// the Helix Agent ensemble, and the reachable HelixAgent adapter. It returns the
// number of providers successfully registered.
//
// A construction or registration error for one provider is logged and skipped
// (it never aborts the others), so a single misbehaving key cannot take the
// whole chat surface down.
func RegisterEnvProviders(manager *llm.ModelManager, cfg *config.Config) int {
	if manager == nil {
		return 0
	}

	// ensembleMembers collects every successfully-constructed cloud provider so
	// the Helix Agent ensemble can fan a prompt across all of them. They are the
	// SAME provider instances registered individually below — the ensemble adds
	// zero new credential requirements (§11.4 / CONST-036).
	ensembleMembers := make([]llm.Provider, 0, len(envProviderCandidates))

	registered := 0
	cloudGateRefused := 0
	for _, cand := range envProviderCandidates {
		if !llm.IsProviderKeyPresent(cand.providerType) {
			continue
		}

		provider, err := llm.NewCloudProvider(cand.providerType, llm.ProviderConfigEntry{
			Type:    cand.providerType,
			Enabled: true,
		})
		if err != nil {
			if errors.Is(err, llm.ErrCloudDisabled) {
				// Deliberate operator policy (llm.cloud.enabled=false, the
				// default) — not a construction defect. Tally it for the
				// single summary line emitted after the loop instead of the
				// scary per-key "construction failed" warning below.
				cloudGateRefused++
				continue
			}
			log.Printf("⚠️  clientcore: skipping provider %s (construction failed: %v)", cand.providerType, err)
			continue
		}

		if err := manager.RegisterProvider(provider); err != nil {
			log.Printf("⚠️  clientcore: skipping provider %s (registration failed: %v)", cand.providerType, err)
			continue
		}

		ensembleMembers = append(ensembleMembers, provider)
		registered++
	}

	if cloudGateRefused > 0 {
		log.Printf("ℹ️  clientcore: %d hosted provider(s) with credentials present were NOT registered because llm.cloud.enabled is false (the default). Set llm.cloud.enabled: true to permit hosted providers — local routes (helixllm coder, llama.cpp, Ollama) are unaffected.", cloudGateRefused)
	}

	// PRIMARY PATH (CONST-036 / CONST-046): source the OpenAI-compatible
	// providers DYNAMICALLY from LLMsVerifier's /api/providers — each provider's
	// base URL is the verifier's api_url (NO hardcoded URL). When the verifier is
	// reachable, this REPLACES the hardcoded HostedOpenAICompatibleCatalogue() as
	// the source of these providers. The hosted catalogue is engaged ONLY as a
	// degraded offline fallback below, when the verifier is unreachable.
	hostedProviders, usedDynamic := buildOpenAICompatibleProviders(cfg)
	if usedDynamic {
		log.Printf("✅ clientcore: OpenAI-compatible providers sourced DYNAMICALLY from LLMsVerifier (verifier api_url is the base URL — no hardcoded catalogue)")
	} else {
		log.Printf("⚠️  clientcore: LLMsVerifier unreachable — using the hardcoded HostedOpenAICompatibleCatalogue() as a DEGRADED OFFLINE FALLBACK only")
	}
	for _, provider := range hostedProviders {
		if err := manager.RegisterProvider(provider); err != nil {
			log.Printf("⚠️  clientcore: skipping hosted provider %s (registration failed: %v)", provider.GetName(), err)
			continue
		}
		ensembleMembers = append(ensembleMembers, provider)
		registered++
	}

	registered += registerEnsembleProvider(manager, ensembleMembers)

	// The HelixAgent adapter is its OWN ensemble; registered standalone so its
	// logical models appear in the picker on their own.
	registered += registerHelixAgentProvider(manager)

	return registered
}

// registerHelixAgentProvider registers the HelixAgent adapter (the full-capacity
// HelixAgent engine + ensemble, consumed over its running REST server) into
// manager — but ONLY when that server is actually reachable. The base URL is
// resolved from HELIXAGENT_BASE_URL (falling back to the adapter's documented
// DefaultBaseURL), and a short-timeout IsAvailable health probe gates the
// registration so the picker never lists a dead "HelixAgent" entry when the
// agent isn't running (an anti-bluff guarantee — §11.4 / CONST-035). On success
// it returns 1; otherwise it logs a single honest info line and returns 0.
func registerHelixAgentProvider(manager *llm.ModelManager) int {
	if manager == nil {
		return 0
	}

	baseURL := strings.TrimSpace(os.Getenv("HELIXAGENT_BASE_URL"))
	if baseURL == "" {
		baseURL = helixagent.DefaultBaseURL
	}

	provider := helixagent.New(baseURL)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if !provider.IsAvailable(ctx) {
		log.Printf("ℹ️  clientcore: HelixAgent not reachable at %s — skipping (start the HelixAgent server to make its full engine selectable)", baseURL)
		return 0
	}

	if err := manager.RegisterProvider(provider); err != nil {
		log.Printf("⚠️  clientcore: skipping HelixAgent provider (registration failed: %v)", err)
		return 0
	}

	log.Printf("✅ clientcore: registered HelixAgent provider (full-capacity engine + ensemble) at %s", baseURL)
	return 1
}

// registerEnsembleProvider registers the "Helix Agent ensemble" meta-provider —
// a REAL multi-provider ensemble (llm.EnsembleProvider) that fans each prompt to
// every member and returns the voted/combined response — so it appears in the
// picker alongside the individual cloud models.
//
// It registers ONLY when there are at least two members: an "ensemble" of one
// provider would be a single-provider pass-through dressed up as orchestration,
// which is the exact anti-bluff trap §11.4 forbids. With <2 members the function
// is a no-op and returns 0.
func registerEnsembleProvider(manager *llm.ModelManager, members []llm.Provider) int {
	if manager == nil || len(members) < 2 {
		return 0
	}

	ensemble := llm.NewEnsembleProvider(llm.EnsembleProviderConfig{
		Members:  members,
		Strategy: "confidence_weighted",
	})
	if err := manager.RegisterProvider(ensemble); err != nil {
		log.Printf("⚠️  clientcore: skipping Helix Agent ensemble (registration failed: %v)", err)
		return 0
	}

	// Belt-and-suspenders warm-cache at registration time (background,
	// non-blocking): pre-resolve each member's working chat model so the
	// cold-start discovery storm is avoided. WarmCache is idempotent + safe to
	// call concurrently.
	go ensemble.WarmCache(context.Background())

	log.Printf("✅ clientcore: registered Helix Agent ensemble over %d providers", len(members))
	return 1
}

// buildOpenAICompatibleProviders is the PRIMARY/FALLBACK switch for the hosted
// OpenAI-Chat-Completions-compatible providers.
//
//   - PRIMARY (returns usedDynamic=true): if a LLMsVerifier endpoint is
//     configured/reachable, query /api/providers and build each provider from the
//     verifier's api_url (CONST-036 single source of truth; CONST-046 no hardcoded
//     URL). Only providers whose key is present are built.
//   - FALLBACK (returns usedDynamic=false): if the verifier is unreachable, fall
//     back to the hardcoded HostedOpenAICompatibleCatalogue() — clearly a degraded
//     offline safety net, NOT the primary source.
//
// The fallback is gated STRICTLY on verifier reachability: GetProviders returns
// an error (never a silent empty slice) when the verifier is unreachable, so a
// reachable-but-empty verifier does NOT silently re-engage the hardcoded list.
func buildOpenAICompatibleProviders(cfg *config.Config) (providers []llm.Provider, usedDynamic bool) {
	endpoint, apiKey, timeout := resolveVerifierEndpoint(cfg)
	if endpoint != "" {
		client := verifier.NewClient(endpoint, apiKey, timeout)
		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		defer cancel()
		recs, err := client.GetProviders(ctx)
		if err == nil {
			// Verifier reachable → dynamic path is authoritative, even if it
			// yields zero providers for the present key set.
			return llm.BuildDynamicOpenAICompatibleProviders(recs), true
		}
		log.Printf("⚠️  clientcore: LLMsVerifier GetProviders failed (%v) — falling back to hardcoded catalogue", err)
	}

	// Degraded offline fallback: hardcoded catalogue.
	fallback := make([]llm.Provider, 0)
	for _, h := range llm.HostedOpenAICompatibleCatalogue() {
		provider, err := llm.NewHostedOpenAICompatibleProvider(h)
		if err != nil {
			continue // key absent / placeholder — silent skip, never aborts the rest
		}
		fallback = append(fallback, provider)
	}
	return fallback, false
}

// resolveVerifierEndpoint resolves the LLMsVerifier REST endpoint (+ api key +
// timeout) the dynamic catalogue queries, in precedence order:
//  1. an explicit, enabled config.Verifier endpoint (mode "remote");
//  2. the HELIX_VERIFIER_ENDPOINT env var;
//  3. the conventional local default http://localhost:8095.
func resolveVerifierEndpoint(cfg *config.Config) (endpoint, apiKey string, timeout time.Duration) {
	timeout = 5 * time.Second
	if cfg != nil && cfg.Verifier != nil && cfg.Verifier.Enabled {
		apiKey = cfg.Verifier.APIKey
		if cfg.Verifier.Timeout > 0 {
			timeout = cfg.Verifier.Timeout
		}
		if e := strings.TrimSpace(cfg.Verifier.Endpoint); e != "" {
			return e, apiKey, timeout
		}
	}
	if e := strings.TrimSpace(os.Getenv("HELIX_VERIFIER_ENDPOINT")); e != "" {
		return e, apiKey, timeout
	}
	return "http://localhost:8095", apiKey, timeout
}
