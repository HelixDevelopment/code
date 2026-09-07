package server

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"dev.helix.code/internal/config"
	"dev.helix.code/internal/llm"
	"dev.helix.code/internal/rag"
	"github.com/gin-gonic/gin"
)

// errUnknownProvider is returned by resolveLLMProvider when the request (or
// HELIX_LLM_PROVIDER) explicitly names a provider that llm.Select cannot
// resolve. It is distinct from a construction/availability failure: the user
// supplied an invalid provider name, so the handler answers 400 (client error)
// rather than silently falling back to the local Ollama default — that silent
// fallback turned a provider typo into a misleading Ollama 404 (server
// defect #4). The named provider is wrapped so the error body can echo it.
var errUnknownProvider = errors.New("unknown provider")

// llmProviderEnv is the environment variable that names the provider when the
// request body does not. Declared as a named constant (rather than repeated as
// a literal in envLLMProvider) so an error message can cite the exact key an
// operator has to correct — see unknownProviderError.
const llmProviderEnv = "HELIX_LLM_PROVIDER"

// configDefaultProviderKey is the config-file key that supplies the
// lowest-precedence provider name (internal/config: llm.default_provider,
// read here through configDefaultProviderFunc). Cited verbatim in the
// misconfiguration error so an operator can find the line to fix.
const configDefaultProviderKey = "llm.default_provider"

// cloudEnabledConfigKey is the config-file key that governs the W2c-1 cloud
// gate (internal/llm: llm.cloud.enabled, default FALSE -- see
// llm.ErrCloudDisabled). Cited verbatim in the gate-refusal error so whoever
// reads the response -- a caller told 403, or an operator told 500 -- is
// pointed at the exact key that decides it, rather than at a bare status.
const cloudEnabledConfigKey = "llm.cloud.enabled"

// errServerProviderMisconfigured is returned by resolveLLMProvider when an
// unresolvable provider name reached it from a SERVER-SIDE source — the config
// file's llm.default_provider, or the server process's HELIX_LLM_PROVIDER —
// rather than from the caller's own request.
//
// The distinction is the entire point. errUnknownProvider means the CALLER
// typed a bad name, and 400 is an honest answer they can act on. But when the
// caller named no provider at all, a 400 blames them for a fault they cannot
// see, cannot fix, and did not cause — and every request to that deployment
// fails identically until an operator edits the server's own configuration.
// That is a server fault, so it answers 5xx and the message names the
// offending key. (Historically HELIX_LLM_PROVIDER=<typo> already behaved this
// way; threading llm.default_provider into resolution — HXC-002-F3-01 — widened
// the same mis-attribution to the config file, which is what makes it worth
// fixing at the source rather than per-source.)
//
// 500 rather than 503, deliberately: 503 advertises a TEMPORARY condition and
// is the status that carries Retry-After, so a well-behaved client would
// retry-loop forever against a deterministic configuration typo that no amount
// of waiting clears. 500 is the honest "the server is broken, retrying will not
// help" signal. The sibling 503 in providerResolveStatus stays 503 because
// construction / credential / endpoint failures genuinely can clear on their
// own (a backend coming back up, a key rotated into the environment).
var errServerProviderMisconfigured = errors.New("server LLM provider misconfigured")

// llm_generate.go — real LLM generation surface over HTTP.
//
// Anti-bluff (CONST-035 / BLUFF-001 / Article XI §11.9): these handlers make
// REAL calls to a REAL provider via the existing llm.Provider interface
// (Generate / GenerateStream). There is NO simulation, NO hardcoded canned
// response, NO print-and-sleep. Every byte returned to the caller originates
// from a provider's Generate / GenerateStream return value.
//
// Provider resolution mirrors cmd/cli/main.go exactly (no invented provider
// API): a cloud provider is selected via llm.Select (flag/env/config
// precedence) + constructed with llm.NewCloudProvider when a provider is
// named (request body `provider` field or HELIX_LLM_PROVIDER); otherwise the
// handler falls back to a local Ollama provider on the standard port — the
// same default NewCLI() and the subagent path use. Provider constructors
// resolve their own credentials from the process environment (loaded at
// server startup by secrets.LoadAPIKeys), so no key value is ever read,
// logged, or persisted here (CONST-042 / §12.1).
//
// The Server.llm field stays nil by design (see server.go New()): the
// provider is constructed PER REQUEST so a key rotated into the environment,
// or a different `provider` per call, is honoured without a server restart,
// and so a missing-key provider surfaces a real runtime auth error from the
// provider call rather than a fabricated "available" status.

// llmGenerateRequest is the JSON body accepted by POST /api/v1/llm/generate
// and POST /api/v1/llm/stream.
type llmGenerateRequest struct {
	// Prompt is the user message. Required. Either Prompt or a non-empty
	// Messages slice must be supplied.
	Prompt string `json:"prompt"`
	// Messages is an optional full chat transcript. When supplied it takes
	// precedence over Prompt (Prompt is appended as a trailing user turn if
	// both are present and non-empty).
	Messages []llm.Message `json:"messages"`
	// Model is the model id to target (e.g. "llama3.2", "claude-3-5-sonnet").
	// Optional — when empty the provider's default model is used.
	Model string `json:"model"`
	// Provider optionally names the provider to use (e.g. "anthropic",
	// "ollama"). When empty, HELIX_LLM_PROVIDER / local-Ollama default apply.
	Provider string `json:"provider"`
	// MaxTokens caps the response length. Optional (0 ⇒ provider default).
	MaxTokens int `json:"max_tokens"`
	// Temperature controls sampling. Optional (0 ⇒ provider default).
	Temperature float64 `json:"temperature"`
}

// buildLLMRequest converts the wire request into an llm.LLMRequest, applying
// the prompt/messages precedence rule. Returns an error string (empty when ok).
func (r *llmGenerateRequest) buildLLMRequest(stream bool) (*llm.LLMRequest, string) {
	messages := make([]llm.Message, 0, len(r.Messages)+1)
	messages = append(messages, r.Messages...)
	if strings.TrimSpace(r.Prompt) != "" {
		messages = append(messages, llm.Message{Role: "user", Content: r.Prompt})
	}
	if len(messages) == 0 {
		return nil, "request must include a non-empty 'prompt' or 'messages'"
	}
	return &llm.LLMRequest{
		Model:       r.Model,
		Messages:    messages,
		MaxTokens:   r.MaxTokens,
		Temperature: r.Temperature,
		Stream:      stream,
	}, ""
}

// llmProviderResolver is the indirection the handlers call to obtain a provider
// for a request. It defaults to the real resolveLLMProvider below. It is a
// package-level var (not a hardcoded call) ONLY so unit tests can substitute a
// real-but-deterministic provider to exercise the streaming goroutine's
// channel-ownership behaviour without a live network/Ollama — production code
// never reassigns it, so the default real path is always what ships.
var llmProviderResolver = resolveLLMProvider

// configDefaultProviderFunc supplies the config file's llm.default_provider
// as the LOWEST-precedence provider-selection source (flag > env > config,
// the precedence llm.Select already honours at provider_factory.go). This is
// the HXC-002-F3-01 fix: historically resolveLLMProvider hardcoded
// SelectorInput.Config = "", so a provider-less request ignored the
// config-declared default entirely and silently fell through to Ollama on
// :11434 instead of the "local" coder route on :18434. Package-level var so
// unit tests can pin the value without a config file; production reads the
// cached process config (config.Get()).
var configDefaultProviderFunc = func() string {
	cfg, err := config.Get()
	if err != nil || cfg == nil {
		return ""
	}
	return strings.TrimSpace(cfg.LLM.DefaultProvider)
}

// ResolveLLMProvider is the exported form of resolveLLMProvider for
// out-of-package callers that must share the server's EXACT provider
// resolution semantics — currently cmd's `helix generate` (HXC-002-F3-04),
// so the CLI and the HTTP API cannot drift apart on which local route a
// default request takes.
func ResolveLLMProvider(providerName, model string) (llm.Provider, error) {
	return resolveLLMProvider(providerName, model)
}

// IsProviderMisconfiguration reports whether err is a ResolveLLMProvider
// failure caused by the RESOLVING PROCESS'S OWN configuration — an
// unresolvable llm.default_provider / HELIX_LLM_PROVIDER, or a hosted default
// named while the cloud gate is shut — as opposed to anything the caller
// supplied.
//
// Exported for out-of-package callers that must render the SAME condition in a
// different context. `helix generate` (cmd/other_commands.go) shares this
// resolution path but has no server and no HTTP status: it needs to tell the
// user their own config/environment is wrong, which is the same predicate the
// HTTP handlers turn into a 5xx.
func IsProviderMisconfiguration(err error) bool {
	return errors.Is(err, errServerProviderMisconfigured)
}

// ragAdapterResolver constructs the RAG (Retrieval-Augmented Generation)
// adapter for a request. It defaults to rag.NewFromEnv(os.Getenv) — a
// fresh Adapter per request, default-OFF unless HELIXCODE_RAG_ENABLED is
// truthy — mirroring cmd/cli/main.go handleGenerate's HXC-118 RAG wiring
// exactly (see applyRAGContext below). It is a package-level var — the
// same test-injection pattern as llmProviderResolver above — ONLY so unit
// tests can substitute a deterministic, enabled Adapter (backed by a
// fixture retriever.Retriever) without a live Ollama embeddings endpoint;
// production code never reassigns it, so the default rag.NewFromEnv path
// is always what ships.
var ragAdapterResolver = func() *rag.Adapter {
	return rag.NewFromEnv(os.Getenv)
}

// applyRAGContext wires HXC-118 Retrieval-Augmented Generation into the
// HTTP server's generate/stream endpoints, mirroring cmd/cli/main.go
// handleGenerate's RAG wiring (Phase 2/3) so a user calling
// POST /api/v1/llm/generate or /api/v1/llm/stream gets the SAME
// retrieval-augmentation an equivalent CLI `helix generate` invocation
// gets — closing the confirmed HXC-118 gap (internal/server had ZERO RAG
// integration prior to this change).
//
// When the adapter is DISABLED (the default — HELIXCODE_RAG_ENABLED unset
// or falsy), this is a documented no-op: Adapter.Enabled() short-circuits
// BEFORE Adapter.Retrieve is ever called, so llmReq is left byte-identical
// to the request buildLLMRequest produced — no HTTP call, no allocation,
// no behavior change versus a server that never imported internal/rag.
//
// When ENABLED, the query used for retrieval is the content of the LAST
// message in llmReq.Messages — the message that carries the request's
// `prompt` field per buildLLMRequest (or the trailing turn of a supplied
// `messages` transcript), i.e. the user's current turn. On a successful,
// non-empty retrieval that message's Content is replaced with the
// rag.PrependContext-augmented version, so the provider call the caller
// (generateLLM / streamLLM) makes next sees the retrieved context
// verbatim ahead of the original prompt — identical in shape to the CLI's
// effectivePrompt substitution.
//
// ANTI-BLUFF graceful degrade (§11.4.6): a retrieval error is logged and
// the request proceeds on the ORIGINAL, unaugmented prompt. RAG failure
// MUST NEVER fail — or silently corrupt — the user's generate/stream
// request; the worst case of a broken retriever is "no RAG context this
// turn," never a 5xx the user did not cause and never a degraded/garbled
// prompt reaching the provider.
func applyRAGContext(ctx context.Context, adapter *rag.Adapter, llmReq *llm.LLMRequest) {
	if adapter == nil || !adapter.Enabled() || llmReq == nil || len(llmReq.Messages) == 0 {
		return
	}
	last := len(llmReq.Messages) - 1
	query := llmReq.Messages[last].Content
	if strings.TrimSpace(query) == "" {
		return
	}
	ragDocs, ragRan, ragErr := adapter.Retrieve(ctx, query, rag.RetrieveOptionsFromEnv(os.Getenv))
	if ragErr != nil {
		log.Printf("rag: retrieval failed, continuing without RAG context: %v", ragErr)
		return
	}
	if ragRan && len(ragDocs) > 0 {
		llmReq.Messages[last].Content = rag.PrependContext(query, ragDocs)
	}
}

// resolveLLMProvider constructs a real llm.Provider for this request.
//
// It reuses the exact construction path cmd/cli/main.go uses:
//   - When `providerName` (or HELIX_LLM_PROVIDER) names a known provider,
//     llm.Select resolves the ProviderType and llm.NewCloudProvider builds it.
//   - When neither flag nor env names a provider, the config file's
//     llm.default_provider is consulted (HXC-002-F3-01) — a "local"/
//     "helixllm" default resolves to the llama.cpp coder route below, and
//     any other configured name flows through llm.Select's Config slot.
//   - Only when ALL of flag/env/config are empty does the request fall back
//     to a local Ollama provider on the standard port, mirroring NewCLI()'s
//     default so an out-of-the-box server with Ollama running can generate
//     with zero configuration.
//
// The provider is the caller's responsibility to Close().
func resolveLLMProvider(providerName, model string) (llm.Provider, error) {
	sel := llm.SelectorInput{
		Flag:   strings.TrimSpace(providerName),
		Env:    "", // HELIX_LLM_PROVIDER picked up below only when Flag empty
		Config: "",
	}
	// Honour HELIX_LLM_PROVIDER only when the request did not name a provider,
	// matching the flag>env precedence cmd/cli applies.
	if sel.Flag == "" {
		sel.Env = strings.TrimSpace(envLLMProvider())
	}

	// The name that will actually be resolved, PLUS where it came from. The
	// source is load-bearing rather than bookkeeping: it decides whether an
	// unresolvable name is the caller's fault (400) or the deployment's own
	// (5xx). See unknownProviderError.
	requested := strings.TrimSpace(sel.Flag)
	source := providerSourceRequest
	if requested == "" {
		requested = strings.TrimSpace(sel.Env)
		source = providerSourceEnv
	}

	// HXC-002-F3-01: the config file's llm.default_provider is the
	// lowest-precedence source. Thread it into SelectorInput.Config so
	// llm.Select's flag > env > config precedence actually sees it, and let
	// the local-route checks below match it — config `default_provider:
	// "local"` MUST resolve to the helixllm coder route, not silently fall
	// through to the Ollama default.
	if requested == "" {
		sel.Config = strings.TrimSpace(configDefaultProviderFunc())
		requested = sel.Config
		source = providerSourceConfig
	}
	if requested == "" {
		// Nothing named anywhere: the zero-config Ollama fallback at the end of
		// this function. Recorded explicitly so the source can never read as
		// "the request asked for the empty string".
		source = providerSourceNone
	}

	// Local HelixLLM coder route (the in-repo llama.cpp OpenAI-compatible
	// sidecar — see resolveHelixLLMLocalProvider's doc-comment). Checked
	// BEFORE llm.Select/llm.NewCloudProvider (the F12 direct-cloud-provider
	// path) because that path's parseCloudProviderType does not — and MUST
	// NOT, per its own doc-comment scoping it to the four Feature-12 cloud
	// backends plus Ollama/llamacpp — recognise "helixllm"/"local"; without
	// this early check the request would be rejected as errUnknownProvider
	// even though the coder is genuinely reachable.
	// Local HelixLLM GATEWAY route (HXC-002-F3-03). Deliberately checked
	// BEFORE the "helixllm"/"local" coder check immediately below.
	//
	// ORDERING, measured rather than assumed: the coder check uses
	// strings.EqualFold, which is WHOLE-STRING equality — EqualFold(
	// "helixllm-gateway", "helixllm") is false — so as the code stands today
	// the gateway name is NOT shadowed regardless of order. The order is
	// still fixed here because that guarantee is one refactor deep: rewriting
	// the check as a prefix / HasPrefix / strings.Contains match (a natural
	// "accept helixllm variants" change) would immediately swallow
	// "helixllm-gateway" and route it to the coder — the caller would get the
	// backend that CANNOT emit structured tool calls while believing it had
	// asked for the one that can, with no error anywhere. Matching the more
	// specific name first makes that class of regression impossible.
	switch strings.ToLower(requested) {
	case "gateway", "helixllm-gateway":
		return resolveHelixLLMGatewayProvider(model)
	}

	if strings.EqualFold(requested, "helixllm") || strings.EqualFold(requested, "local") {
		return resolveHelixLLMLocalProvider(model)
	}

	// Local llama.cpp route. Checked BEFORE llm.Select/llm.NewCloudProvider
	// for the same reason the helixllm check above is: that path DOES resolve
	// these names (parseCloudProviderType maps llamacpp/llama-cpp/llama.cpp →
	// ProviderTypeLlamaCpp) but constructs an *llm.LlamaCPPProvider that
	// cannot serve a request from THESE handlers — see
	// resolveLlamaCppLocalProvider's doc-comment for the two measured
	// reasons. Routing here instead gives the caller the llama.cpp backend
	// they asked for, over the endpoint llama.cpp actually serves.
	switch strings.ToLower(requested) {
	case "llamacpp", "llama-cpp", "llama.cpp":
		return resolveLlamaCppLocalProvider(model)
	}

	ptype, selErr := llm.Select(sel)
	switch {
	case selErr == nil:
		// A provider was named and resolved to a known type — construct it.
		entry := llm.ProviderConfigEntry{Type: ptype, Enabled: true}
		if strings.TrimSpace(model) != "" {
			entry.Models = []string{model}
		}
		provider, cErr := llm.NewCloudProvider(ptype, entry)
		if cErr != nil {
			// The W2c-1 cloud gate (llm.cloud.enabled, default false) refuses
			// hosted construction outright. That refusal is DETERMINISTIC —
			// no retry clears it — so it must not travel the generic
			// construction path below, whose 503 invites exactly that retry.
			// Classified here because this is the one place the provenance
			// (`source`) is still in scope; see cloudDisabledError.
			if errors.Is(cErr, llm.ErrCloudDisabled) {
				return nil, cloudDisabledError(string(ptype), source, cErr)
			}
			return nil, fmt.Errorf("failed to construct provider %q: %w", ptype, cErr)
		}
		if provider != nil {
			return provider, nil
		}
		// Defensive: NewCloudProvider returned (nil, nil) — treat as a real
		// construction failure rather than silently masking it as Ollama.
		return nil, fmt.Errorf("provider %q constructed nil without an error", ptype)

	case errors.Is(selErr, llm.ErrNoProviderConfigured):
		// No provider named anywhere — fall through to the local Ollama default
		// below (out-of-the-box behaviour for a zero-config server with Ollama).

	default:
		// A provider WAS named somewhere but llm.Select could not resolve it
		// (unknown/unsupported provider string). Do NOT silently fall back to
		// Ollama — that masks the typo as an unrelated Ollama 404 (server
		// defect #4). Which error is honest depends on WHO named it: the
		// caller gets a 400 they can fix, a server-side source gets a 5xx that
		// names the key an operator must fix.
		return nil, unknownProviderError(requested, source)
	}

	// Default: local Ollama on the standard port (mirrors NewCLI()).
	defaultModel := strings.TrimSpace(model)
	if defaultModel == "" {
		defaultModel = "llama3.2"
	}
	provider, err := llm.NewOllamaProvider(llm.OllamaConfig{
		DefaultModel: defaultModel,
		// HELIX_OLLAMA_HOST when set, else the standard port — the default
		// this route has always used, unchanged. See envOllamaHost.
		BaseURL:       envOllamaHost(),
		StreamEnabled: true,
	})
	if err != nil {
		return nil, fmt.Errorf("default Ollama provider construction failed: %w", err)
	}
	return provider, nil
}

// resolveDefaultModel fills in a request that OMITTED the model with a
// VERIFIED-AVAILABLE model from the provider's own catalog.
//
// CONST-036 / CONST-037: LLMsVerifier is the single source of truth and every
// model surfaced MUST be verified-available, so the default is sourced from
// provider.GetModels() (which, for the OpenAI-compatible cloud providers,
// refreshes LIVE from the provider's own `GET /models` on first call) — never
// a hardcoded literal. The first catalog entry is the provider's currently
// served, leading model.
//
// The historical defect this guards (server defect: empty/default-model
// Generate → upstream 400 → API 502): a Generate that omitted the model left
// llm.LLMRequest.Model == "" all the way to the wire. DeepSeek (and any
// provider that does not synthesise its own default) then rejected the empty
// model — e.g. `400: "The supported API model names are deepseek-v4-pro or
// deepseek-v4-flash, but you passed ."`. The fix never lets an empty model
// reach the provider when the catalog can supply a verified one.
//
// When the request already names a model, or the catalog is empty (offline /
// unreachable provider — that staleness is the verifier's concern, not the
// server's to mask), the model is left unchanged and the provider's own
// default-handling / honest error path takes over. This is the same behaviour
// as before for those cases — the change is strictly additive on the
// previously-broken empty-model-against-a-reachable-catalog path.
func resolveDefaultModel(provider llm.Provider, requested string) string {
	if strings.TrimSpace(requested) != "" {
		return requested
	}
	for _, m := range provider.GetModels() {
		// Prefer the verifier-facing Name; fall back to the catalog ID when a
		// provider populates only ID. Skip blank entries defensively.
		if name := strings.TrimSpace(m.Name); name != "" {
			return name
		}
		if id := strings.TrimSpace(m.ID); id != "" {
			return id
		}
	}
	// Catalog empty (offline/unreachable). Leave it empty: the provider's own
	// default-or-honest-error path handles it — we do NOT invent a model.
	return ""
}

// generateLLM handles POST /api/v1/llm/generate — a real, non-streaming
// completion. It returns the provider's actual response Content plus usage.
func (s *Server) generateLLM(c *gin.Context) {
	var req llmGenerateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"status": "error",
			"error":  fmt.Sprintf("invalid request body: %v", err),
		})
		return
	}

	llmReq, validationErr := req.buildLLMRequest(false)
	if validationErr != "" {
		c.JSON(http.StatusBadRequest, gin.H{"status": "error", "error": validationErr})
		return
	}

	provider, err := llmProviderResolver(req.Provider, req.Model)
	if err != nil {
		c.JSON(providerResolveStatus(err), gin.H{"status": "error", "error": err.Error()})
		return
	}
	defer func() { _ = provider.Close() }()

	// CONST-036/037: when the request omitted the model, resolve it to a
	// verified-available model from the provider's catalog so an empty model is
	// never sent upstream (server defect: empty-model → provider 400 → API 502).
	llmReq.Model = resolveDefaultModel(provider, llmReq.Model)

	ctx, cancel := context.WithTimeout(c.Request.Context(), 120*time.Second)
	defer cancel()

	// HXC-118: mirror the CLI's RAG wiring — no-op unless HELIXCODE_RAG_ENABLED.
	applyRAGContext(ctx, ragAdapterResolver(), llmReq)

	resp, genErr := provider.Generate(ctx, llmReq)
	if genErr != nil {
		// Real provider error (auth failure, model not found, network) —
		// surfaced honestly, never masked as success (CONST-035).
		c.JSON(http.StatusBadGateway, gin.H{
			"status":   "error",
			"error":    fmt.Sprintf("generation failed: %v", genErr),
			"provider": provider.GetName(),
		})
		return
	}

	// Report the model the provider ACTUALLY served. llmReq.Model is the
	// caller's REQUESTED string, which is an alias ("default", "local", a
	// routing name) whenever the backend resolved it to a concrete model —
	// echoing it back tells the client a model identity that never served the
	// request (CONST-036 / CONST-037). Fall back to the requested model only
	// when the provider reported none, so the field is never empty. Mirrors
	// llmResponseToOpenAI in wire_facade.go exactly — the same defect existed
	// on this native API surface as on the OpenAI/Anthropic wire facades.
	servedModel := llmReq.Model
	if resp.Model != "" {
		servedModel = resp.Model
	}

	c.JSON(http.StatusOK, gin.H{
		"status":   "success",
		"content":  resp.Content,
		"provider": provider.GetName(),
		"model":    servedModel,
		"usage": gin.H{
			"prompt_tokens":     resp.Usage.PromptTokens,
			"completion_tokens": resp.Usage.CompletionTokens,
			"total_tokens":      resp.Usage.TotalTokens,
		},
		"finish_reason": resp.FinishReason,
	})
}

// streamLLM handles POST /api/v1/llm/stream — a real, streaming completion
// emitted as Server-Sent Events. Each chunk's Content is forwarded as it
// arrives from the provider's GenerateStream channel; a terminal `[DONE]`
// event closes the stream.
func (s *Server) streamLLM(c *gin.Context) {
	var req llmGenerateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"status": "error",
			"error":  fmt.Sprintf("invalid request body: %v", err),
		})
		return
	}

	llmReq, validationErr := req.buildLLMRequest(true)
	if validationErr != "" {
		c.JSON(http.StatusBadRequest, gin.H{"status": "error", "error": validationErr})
		return
	}

	provider, err := llmProviderResolver(req.Provider, req.Model)
	if err != nil {
		c.JSON(providerResolveStatus(err), gin.H{"status": "error", "error": err.Error()})
		return
	}
	defer func() { _ = provider.Close() }()

	// CONST-036/037: same verified-available default-model resolution as the
	// non-streaming path — an omitted model must not reach the provider empty.
	llmReq.Model = resolveDefaultModel(provider, llmReq.Model)

	c.Writer.Header().Set("Content-Type", "text/event-stream")
	c.Writer.Header().Set("Cache-Control", "no-cache")
	c.Writer.Header().Set("Connection", "keep-alive")

	ctx, cancel := context.WithTimeout(c.Request.Context(), 120*time.Second)
	defer cancel()

	// HXC-118: mirror the CLI's RAG wiring — no-op unless HELIXCODE_RAG_ENABLED.
	applyRAGContext(ctx, ragAdapterResolver(), llmReq)

	// CHANNEL-OWNERSHIP CONTRACT (see llm.Provider.GenerateStream interface doc):
	// the PROVIDER is the SENDER and the SOLE closer of chunkChan — it closes the
	// channel on every return path (success, error, ctx-cancel). This consumer
	// MUST NOT close chunkChan. Closing it here too would be a double-close, which
	// panics ("close of closed channel") inside this producer goroutine; that
	// panic is NOT recoverable by gin.Recovery and crashes the whole server
	// process — a single client request could remotely kill the server
	// (server defect #5; CONST-035 / Article XI §11.9). The provider's guaranteed
	// close is what lets streamProviderToSSE observe the drain, emit the terminal
	// `data: [DONE]` frame, and return without waiting for the 120s ctx deadline.
	chunkChan := make(chan llm.LLMResponse, 100)
	errCh := make(chan error, 1)
	go func() {
		errCh <- provider.GenerateStream(ctx, llmReq, chunkChan)
	}()

	// c.Stream pumps the provider channel to the client. Returning false from
	// the step function ends the stream. Each real chunk is forwarded as an
	// SSE `data:` frame; provider errors and EOF terminate honestly.
	streamErr := streamProviderToSSE(c, chunkChan, errCh)
	if streamErr != nil {
		// Best-effort error frame; the stream may already be partially
		// written, so we cannot change the status code here.
		fmt.Fprintf(c.Writer, "event: error\ndata: %s\n\n", streamErr.Error())
		c.Writer.(interface{ Flush() }).Flush()
	}
}

// streamProviderToSSE forwards provider chunks to the SSE writer until the
// channel closes or the provider reports an error. Returns the provider error
// (if any) once streaming completes.
func streamProviderToSSE(c *gin.Context, chunkChan <-chan llm.LLMResponse, errCh <-chan error) error {
	flusher, _ := c.Writer.(interface{ Flush() })
	for {
		select {
		case <-c.Request.Context().Done():
			return c.Request.Context().Err()
		case chunk, ok := <-chunkChan:
			if !ok {
				// Channel drained — collect the provider's terminal error.
				fmt.Fprint(c.Writer, "data: [DONE]\n\n")
				if flusher != nil {
					flusher.Flush()
				}
				if perr := <-errCh; perr != nil && perr != io.EOF {
					return fmt.Errorf("streaming generation failed: %w", perr)
				}
				return nil
			}
			if chunk.Content != "" {
				fmt.Fprintf(c.Writer, "data: %s\n\n", sseEscape(chunk.Content))
				if flusher != nil {
					flusher.Flush()
				}
			}
			if chunk.Err != nil {
				return fmt.Errorf("streaming generation failed: %w", chunk.Err)
			}
		}
	}
}

// sseEscape replaces newlines in a chunk so a multi-line token does not break
// the SSE framing (each `data:` line is a single logical field).
func sseEscape(s string) string {
	return strings.ReplaceAll(s, "\n", "\\n")
}

// providerResolveStatus maps a resolveLLMProvider error to the right HTTP
// status. The mapping turns on WHO is at fault, not merely on what failed:
//
//   - A provider name the CALLER supplied (the request body's `provider`
//     field) that llm.Select cannot resolve is a client error: 400. The
//     caller typed an invalid provider name and fixes it by typing a valid
//     one.
//   - The SAME unresolvable name arriving from a SERVER-SIDE source (the
//     config file's llm.default_provider, or the server process's
//     HELIX_LLM_PROVIDER) is not the caller's fault — they named no provider
//     at all — so it is 500, with a message naming the offending key. See
//     errServerProviderMisconfigured for why 500 and not 503.
//   - A refusal by the W2c-1 cloud gate (llm.ErrCloudDisabled — the caller
//     named a hosted provider while llm.cloud.enabled is false) is 403
//     Forbidden. The request is well-formed and the name is valid, so 400
//     ("you sent something malformed") would misdescribe it; the server
//     understood it and refuses on policy, which is precisely 403. The
//     server-sourced half of that same refusal is carried by the
//     misconfiguration sentinel above and answers 500 — see cloudDisabledError.
//   - Any other resolution failure (construction / credentials / endpoint) is
//     a 503 the operator can act on and which may genuinely clear by itself.
//
// Why the gate is NOT left on the 503 fall-through (the defect this branch
// fixes): 503 advertises a TEMPORARY condition and is the status that carries
// Retry-After, so a well-behaved client retry-loops forever against a closed
// gate — a deterministic configuration state no amount of waiting clears. That
// is the same reasoning errServerProviderMisconfigured records for choosing
// 500 over 503, applied to the other deterministic refusal on this path.
//
// llm.ErrCloudDisabled has a SECOND producer, and it does NOT reach the 403
// above. NewOpenAICompatibleProvider refuses on ENDPOINT LOCALITY, so the local
// routes (helixllm / gateway / llamacpp) also raise it when their endpoint env
// var has been pointed at a REMOTE host while the gate is shut. An earlier
// revision left those on the 403, reasoning that "a 5xx would need provenance
// those constructors do not carry". That reasoning was measured and found
// FALSE: the resolvers read those endpoints from the SERVER PROCESS's own
// environment (envHelixLLMLocalEndpoint and its siblings in this file are
// os.Getenv calls), so the provenance is not merely available — it is
// server-side by construction. The caller named a LOCAL route and supplied no
// endpoint; the remoteness is entirely the deployment's own. Those refusals
// therefore wrap errServerProviderMisconfigured IN ADDITION to the cause and
// land on the 500 below, exactly as a hosted default with the gate shut does.
// See localRouteRemoteEndpointError.
//
// Order matters twice over: the misconfiguration sentinel is checked FIRST, so
// a future change that wraps both sentinels cannot silently re-demote a server
// fault back to a 400 or a 403 — a server-sourced gate refusal deliberately
// wraps BOTH errServerProviderMisconfigured and llm.ErrCloudDisabled, and the
// 500 must win. The gate check then precedes errUnknownProvider because a name
// the gate rejected is a KNOWN name, never an unresolvable one.
func providerResolveStatus(err error) int {
	if errors.Is(err, errServerProviderMisconfigured) {
		return http.StatusInternalServerError
	}
	if errors.Is(err, llm.ErrCloudDisabled) {
		return http.StatusForbidden
	}
	if errors.Is(err, errUnknownProvider) {
		return http.StatusBadRequest
	}
	return http.StatusServiceUnavailable
}

// providerSource records WHERE the provider name resolveLLMProvider is about to
// resolve came from. Only providerSourceRequest is the caller's own input; the
// other two name the deployment's own configuration, and an unresolvable name
// from those is a server fault rather than a client error.
type providerSource int

const (
	// providerSourceNone: no provider named anywhere — the zero-config Ollama
	// fallback path, which never reaches unknownProviderError.
	providerSourceNone providerSource = iota
	// providerSourceRequest: the request body's `provider` field (or the
	// providerName argument of the exported ResolveLLMProvider, which the CLI
	// fills from its --provider flag).
	providerSourceRequest
	// providerSourceEnv: the SERVER PROCESS's HELIX_LLM_PROVIDER — server-side
	// state the HTTP caller cannot see or influence.
	providerSourceEnv
	// providerSourceConfig: the config file's llm.default_provider.
	providerSourceConfig
)

// unknownProviderError builds the error for a provider name llm.Select could
// not resolve, choosing the sentinel by SOURCE so the status the handler
// returns is honest about who has to fix it.
//
// The message wording is deliberately CONTEXT-NEUTRAL ("config key",
// "environment variable" — not "the server's"). This resolution path is shared
// verbatim with the CLI: cmd's `helix generate` calls the exported
// ResolveLLMProvider, where there is no server and HELIX_LLM_PROVIDER is the
// USER'S OWN shell environment, so "the server's environment variable ... a
// server-side configuration fault" was simply untrue at that call site. Nothing
// load-bearing is lost server-side: the message still names the offending key,
// echoes the unresolvable value, states it is a configuration fault rather than
// a client error, and says what to correct. The CLI additionally re-frames it
// as the user's own configuration — see cmd/other_commands.go.
//
// Names that land here from a server-side source are not only typos: the F12
// direct-cloud path deliberately rejects "vllm", "localai" and "lmstudio"
// (internal/llm/provider_factory.go parseCloudProviderType), so a config that
// declares one of those as llm.default_provider is a perfectly plausible
// deployment that used to answer every provider-less request with "400 invalid
// request".
func unknownProviderError(requested string, source providerSource) error {
	switch source {
	case providerSourceConfig:
		return fmt.Errorf(
			"%w: config key %s names provider %q, which cannot be resolved. The "+
				"request did not name a provider, so this is a configuration "+
				"fault rather than a client error — correct %s in the active "+
				"configuration (or set %s)",
			errServerProviderMisconfigured, configDefaultProviderKey, requested,
			configDefaultProviderKey, llmProviderEnv)
	case providerSourceEnv:
		return fmt.Errorf(
			"%w: environment variable %s names provider %q, which cannot be "+
				"resolved. The request did not name a provider, so this is a "+
				"configuration fault rather than a client error — correct %s in "+
				"the environment of the process resolving it",
			errServerProviderMisconfigured, llmProviderEnv, requested, llmProviderEnv)
	default:
		// The caller named it: their typo, their 400 — and never a silent
		// Ollama fallback (server defect #4).
		return fmt.Errorf("%w: %q", errUnknownProvider, requested)
	}
}

// cloudDisabledError builds the error for a hosted provider refused by the
// W2c-1 cloud gate, choosing the sentinel by SOURCE exactly as
// unknownProviderError does — the same providerSource enum, not a second one.
//
// The split is the same question in both cases: WHO named the provider the
// deployment will not serve?
//
//   - The CALLER named it (request body `provider` / the CLI's --provider).
//     Their input is well-formed and the provider name is valid; the
//     deployment simply refuses to serve hosted providers. That is a policy
//     refusal of an understood request — 403 — reached by leaving the error
//     carrying ONLY llm.ErrCloudDisabled (via %w on the cause).
//   - A SERVER-SIDE source named it (llm.default_provider, or the process's
//     HELIX_LLM_PROVIDER). The caller named no provider at all, so they can
//     neither see nor fix this, and EVERY provider-less request to that
//     deployment fails identically until an operator reconciles two of its own
//     settings: a hosted default with the cloud gate shut. That is a
//     self-contradictory configuration, so it wraps
//     errServerProviderMisconfigured (→ 500) IN ADDITION to the cause, and
//     providerResolveStatus's check order makes the 500 win.
//
// providerSourceNone cannot reach here (nothing named ⇒ the Ollama fallback,
// which is a local type and gate-exempt); it is folded into the server-side
// branch defensively rather than defaulting to the caller-facing 403, because
// an absent name is by definition not the caller's input.
func cloudDisabledError(requested string, source providerSource, cause error) error {
	if source == providerSourceRequest {
		return fmt.Errorf(
			"%w: the request named the hosted provider %q, but this deployment "+
				"serves local providers only (%s is false). Retrying will not "+
				"change this — name a local route (local/helixllm, llamacpp, "+
				"ollama) or ask an operator to set %s: true",
			cause, requested, cloudEnabledConfigKey, cloudEnabledConfigKey)
	}
	sourceKey := configDefaultProviderKey
	if source == providerSourceEnv {
		sourceKey = llmProviderEnv
	}
	return fmt.Errorf(
		"%w: %w: the request named no provider, so the hosted provider %q came "+
			"from %s while %s is false — two settings of this deployment that "+
			"contradict each other. Correct %s to a local route "+
			"(local/helixllm, llamacpp, ollama) or set %s: true",
		errServerProviderMisconfigured, cause, requested, sourceKey,
		cloudEnabledConfigKey, sourceKey, cloudEnabledConfigKey)
}

// localRouteRemoteEndpointError builds the error for a LOCAL route whose
// ENDPOINT variable has been pointed at a remote host while the W2c-1 cloud
// gate is shut. It is the third member of the family cloudDisabledError and
// unknownProviderError belong to, and it answers the same question they do:
// WHO has to fix this?
//
// The caller named a LOCAL route — "local"/"helixllm", "gateway", "llamacpp" —
// and supplied no endpoint at all; neither wire shape on this surface has an
// endpoint field. The remoteness is introduced ENTIRELY by a server-side
// variable (HELIX_LLM_LOCAL_OPENAI_ENDPOINT / HELIX_LLM_GATEWAY_ENDPOINT /
// HELIX_LLAMA_CPP_HOST). So this is the same shape as a hosted default with the
// gate shut: two of the deployment's own settings contradicting each other,
// with the caller unable to see or influence either.
//
// Leaving it on the bare llm.ErrCloudDisabled answered 403 Forbidden, which an
// OpenAI SDK surfaces to the user as PermissionDeniedError — "you are not
// permitted" — for a fault only an operator can fix, and which no amount of
// re-authenticating or retrying will clear. Wrapping errServerProviderMisconfigured
// IN ADDITION to the cause moves it to 500 through providerResolveStatus's
// existing check order (the misconfiguration sentinel is tested FIRST), with no
// change to that function.
//
// The doc-comment on providerResolveStatus previously justified the 403 here on
// the grounds that "a 5xx would need provenance those constructors do not
// carry". That was measured to be false: these resolvers read the endpoint from
// the SERVER PROCESS's own environment (envHelixLLMLocalEndpoint and siblings
// are os.Getenv calls in this file), so the provenance is not merely available
// — it is server-side by construction, and every value the message quotes comes
// from this process rather than from the request.
//
// sourceKey names the variable that ACTUALLY supplied the value, which is not
// always the route's primary key: llamacpp falls through to the project-wide
// HELIX_LLM_LOCAL_OPENAI_ENDPOINT when its own key is unset, and naming the
// wrong one would send an operator to edit a variable that is not set. An empty
// sourceKey means the value came from the compiled-in default — reported
// honestly rather than blamed on a variable nobody set (§11.4.6). That case is
// unreachable today because every default is a loopback address; it is handled
// rather than assumed away.
// CONST-042 / Article XII §12.1: the endpoint is quoted through
// llm.RedactEndpointForMessage, NEVER verbatim. This error is wrapped as a 500
// and its text reaches the HTTP response body, so an operator who points a
// local route at "https://user:pw@host/v1" would otherwise hand that password
// to whoever can reach the endpoint. Redaction preserves scheme, host and port
// so the message keeps naming WHICH endpoint is misconfigured; an unparseable
// value is replaced wholesale rather than echoed. The wrapped `cause` — the
// constructor's own gate refusal — is redacted at its own site for the same
// reason, so neither half of this message can carry the credential.
func localRouteRemoteEndpointError(route, sourceKey, overrideKey, endpoint string, cause error) error {
	origin := fmt.Sprintf("environment variable %s", sourceKey)
	if strings.TrimSpace(sourceKey) == "" {
		origin = "this build's compiled-in default"
	}
	return fmt.Errorf(
		"%w: %w: the request named the LOCAL route %q and supplied no endpoint, "+
			"but %s points that route at %q, which is not a local endpoint, while "+
			"%s is false. Those are two settings of this deployment contradicting "+
			"each other, not a client error — point %s at a local endpoint, or set "+
			"%s: true",
		errServerProviderMisconfigured, cause, route, origin,
		llm.RedactEndpointForMessage(endpoint),
		cloudEnabledConfigKey, overrideKey, cloudEnabledConfigKey)
}

// envLLMProvider reads HELIX_LLM_PROVIDER. Factored out so the resolution path
// has a single, testable env touch point, and reads the same llmProviderEnv
// constant the error messages cite so the two cannot drift apart.
func envLLMProvider() string {
	return os.Getenv(llmProviderEnv)
}

// helixLLMLocalOpenAIEndpointEnv is the SAME env var the sibling
// submodules/helix_agent HelixLLM provider adapter
// (internal/llm/providers/helixllm/provider.go:41, EnvLocalOpenAIEndpoint)
// and the submodules/llms_verifier helixllm ProviderConfig row
// (llm-verifier/providers/config.go:15) already read — the single
// established, project-wide convention for pointing at the in-repo
// llama.cpp OpenAI-compatible coder sidecar (CONST-036/§11.4.74: reuse the
// existing convention, do not invent a new env var). Base URL only, with NO
// trailing "/v1" — the llama-server always answers under "/v1/..." and the
// OpenAICompatibleConfig ChatEndpoint/ModelEndpoint defaults already carry
// that prefix.
const helixLLMLocalOpenAIEndpointEnv = "HELIX_LLM_LOCAL_OPENAI_ENDPOINT"

// helixLLMLocalDefaultEndpoint is the sane out-of-the-box default matching
// the coder's actual listening port. §11.4.28: this is the ONLY hardcoded
// host in this route, and it is overridable by every deployment via
// helixLLMLocalOpenAIEndpointEnv.
const helixLLMLocalDefaultEndpoint = "http://localhost:18434"

// envHelixLLMLocalEndpoint reads HELIX_LLM_LOCAL_OPENAI_ENDPOINT, falling
// back to helixLLMLocalDefaultEndpoint when unset or blank.
func envHelixLLMLocalEndpoint() string {
	endpoint, _ := envHelixLLMLocalEndpointWithSource()
	return endpoint
}

// envHelixLLMLocalEndpointWithSource is envHelixLLMLocalEndpoint plus the NAME
// of the variable the value came from (empty when it fell back to the
// compiled-in default). The two share one body so the endpoint reported in an
// error can never drift from the endpoint actually dialled.
func envHelixLLMLocalEndpointWithSource() (string, string) {
	if v := strings.TrimSpace(os.Getenv(helixLLMLocalOpenAIEndpointEnv)); v != "" {
		return v, helixLLMLocalOpenAIEndpointEnv
	}
	return helixLLMLocalDefaultEndpoint, ""
}

// resolveHelixLLMLocalProvider constructs the local HelixLLM coder route: a
// REAL *llm.OpenAICompatibleProvider (internal/llm/openai_compatible_provider.go)
// — the same generic OpenAI-compatible HTTP client HelixCode already uses
// for VLLM/LMStudio/LocalAI/etc. — pointed at the local llama.cpp sidecar's
// base URL. This is deliberately NOT llm.NewLlamaCPPProvider: that adapter
// always POSTs to "/v1/completions" even when the request carries
// request.Messages (a chat-shaped payload), which a real llama.cpp server
// rejects with `400 key 'prompt' not found` (verified live against the
// coder during this change) — OpenAICompatibleProvider correctly POSTs
// messages to "/v1/chat/completions". No API key: the coder is a
// loopback/LAN service with no auth, so nothing is read or leaked
// (CONST-042/§12.1).
func resolveHelixLLMLocalProvider(model string) (llm.Provider, error) {
	endpoint, sourceKey := envHelixLLMLocalEndpointWithSource()
	cfg := llm.OpenAICompatibleConfig{
		BaseURL:          endpoint,
		DefaultModel:     strings.TrimSpace(model),
		Timeout:          120 * time.Second,
		StreamingSupport: true,
	}
	provider, err := llm.NewOpenAICompatibleProvider("helixllm", cfg)
	if err != nil {
		// A cloud-gate refusal on a LOCAL route is a server-side
		// contradiction, never the caller's fault — see
		// localRouteRemoteEndpointError.
		if errors.Is(err, llm.ErrCloudDisabled) {
			return nil, localRouteRemoteEndpointError(
				"local/helixllm", sourceKey, helixLLMLocalOpenAIEndpointEnv, endpoint, err)
		}
		return nil, fmt.Errorf("failed to construct helixllm local provider: %w", err)
	}
	if provider == nil {
		return nil, fmt.Errorf("helixllm local provider constructed nil without an error")
	}
	return provider, nil
}

// helixLLMGatewayEndpointEnv points at the HelixLLM GATEWAY's
// OpenAI-compatible base URL. It is a NEW key rather than a reuse of
// helixLLMLocalOpenAIEndpointEnv because the two name genuinely different
// services that a deployment may run SIMULTANEOUSLY (they do here: the coder
// on :18434 and the gateway on :8443), so collapsing them onto one variable
// would make the two routes mutually exclusive. The name follows the
// established HELIX_LLM_* shape of its siblings (§11.4.74 — extend the
// existing convention, do not invent a new one).
//
// Base URL only. A trailing "/v1" is TOLERATED and stripped — see
// envHelixLLMGatewayEndpoint.
const helixLLMGatewayEndpointEnv = "HELIX_LLM_GATEWAY_ENDPOINT"

// helixLLMGatewayDefaultEndpoint is the gateway's actual OpenAI-compatible
// base URL, verified live during this change (GET .../v1/models over TLS with
// the in-repo CA answered 200). §11.4.28: the ONLY hardcoded host in this
// route, and every deployment overrides it via helixLLMGatewayEndpointEnv.
//
// NOTE the INCLUDED "/v1", which is the opposite of the sibling coder and
// llama.cpp routes, and is deliberate. Those two hand the OpenAI-compatible
// provider a bare host and rely on its DEFAULT endpoint paths, which already
// carry the "/v1" prefix ("/v1/models", "/v1/chat/completions"). This route
// instead uses the endpoint form the gateway itself advertises — the "/v1"
// lives in the base URL, and resolveHelixLLMGatewayProvider sets the
// provider's ChatEndpoint / ModelEndpoint to the REMAINDER ("/chat/completions",
// "/models") so the concatenated URL is identical either way.
//
// The two spellings are NOT interchangeable if only one half is changed:
// base-with-"/v1" combined with the DEFAULT endpoints yields
// ".../v1/v1/models". Measured against the live gateway: "/v1/models" -> 200,
// "/v1/v1/models" -> 404. envHelixLLMGatewayEndpoint normalises any operator
// input to the with-"/v1" form so the pairing below always holds.
const helixLLMGatewayDefaultEndpoint = "https://127.0.0.1:8443/v1"

// helixLLMGatewayCACertEnv overrides the CA certificate used to verify the
// gateway's TLS certificate. The gateway is served with a SELF-SIGNED
// certificate, so the host's system trust store cannot verify it and an
// https:// dial fails with an unknown-authority error unless this CA is
// trusted. The value is a PUBLIC trust anchor, not a credential — nothing
// secret is read, logged, or persisted (CONST-042 / §12.1).
const helixLLMGatewayCACertEnv = "HELIX_LLM_GATEWAY_CA_CERT"

// helixLLMGatewayCACertRelPath is where the gateway's CA lives INSIDE THIS
// REPOSITORY, expressed relative to the REPO ROOT (one level above this Go
// module). It is resolved at runtime by walking up from the process's working
// directory and from the binary's own location — never a hardcoded absolute
// path, which would bake one operator's checkout into tracked code
// (§11.4.28 / §11.4.177).
const helixLLMGatewayCACertRelPath = "submodules/helix_llm/certs/cert.pem"

// envHelixLLMGatewayEndpoint resolves the gateway base URL from
// HELIX_LLM_GATEWAY_ENDPOINT (falling back to
// helixLLMGatewayDefaultEndpoint) and NORMALISES it to the canonical
// with-"/v1" form: exactly one trailing "/v1", no trailing slash.
//
// Both spellings an operator might plausibly supply are accepted:
//
//	"https://host:8443"      -> "https://host:8443/v1"   ("/v1" appended)
//	"https://host:8443/v1"   -> "https://host:8443/v1"   (already canonical)
//	"https://host:8443/v1/"  -> "https://host:8443/v1"   (slash trimmed)
//
// This is not cosmetic. The gateway documents and prints its endpoint WITH
// "/v1", while the sibling coder/llama.cpp env vars are documented WITHOUT
// it, so both habits are live in this repository. Whichever an operator
// pastes, the concatenation with the endpoint paths set in
// resolveHelixLLMGatewayProvider lands on the URL the gateway actually
// serves — instead of a "/v1/v1/..." 404 that reads exactly like "the
// gateway is down".
func envHelixLLMGatewayEndpoint() string {
	endpoint, _ := envHelixLLMGatewayEndpointWithSource()
	return endpoint
}

// envHelixLLMGatewayEndpointWithSource is envHelixLLMGatewayEndpoint plus the
// NAME of the variable the value came from (empty when it fell back to the
// compiled-in default). One body, so the endpoint an error quotes is
// byte-for-byte the normalised endpoint the provider will dial.
func envHelixLLMGatewayEndpointWithSource() (string, string) {
	sourceKey := helixLLMGatewayEndpointEnv
	raw := strings.TrimSpace(os.Getenv(helixLLMGatewayEndpointEnv))
	if raw == "" {
		raw = helixLLMGatewayDefaultEndpoint
		sourceKey = ""
	}
	trimmed := strings.TrimRight(raw, "/")
	if strings.HasSuffix(trimmed, "/v1") {
		return trimmed, sourceKey
	}
	return trimmed + "/v1", sourceKey
}

// envHelixLLMGatewayCACert resolves the CA certificate path:
// HELIX_LLM_GATEWAY_CA_CERT when set, otherwise the in-repo certificate
// located by walking up from the runtime working directory / binary
// location. Returns "" when neither is available — the caller MUST treat
// that as a hard error rather than dialling without a trust anchor.
func envHelixLLMGatewayCACert() string {
	if v := strings.TrimSpace(os.Getenv(helixLLMGatewayCACertEnv)); v != "" {
		return v
	}
	return findRepoRelativeFile(helixLLMGatewayCACertRelPath)
}

// findRepoRelativeFile locates a file given by its path RELATIVE TO THE REPO
// ROOT, without knowing where the repo root is.
//
// It walks upward from two independent starting points and returns the first
// existing match:
//
//  1. the process working directory — covers `make dev`, test runs, and any
//     invocation from inside the checkout;
//  2. the directory of the running executable (symlinks resolved) — covers a
//     binary started from elsewhere, e.g. a service unit whose
//     WorkingDirectory is unrelated, as long as the binary still lives under
//     the checkout (bin/helixcode does).
//
// Returning "" ("not found") is a first-class, honest outcome: a binary
// copied outside the repository genuinely has no in-repo certificate to find,
// and the caller reports that plainly instead of guessing a path. Directories
// already visited are skipped, so the second walk costs nothing when it
// shares a suffix with the first.
//
// SEARCH ORDER, STATED EXPLICITLY (it is a filesystem search, so it should not
// have to be reverse-engineered from the loop): working directory first, then
// executable directory; within each, the starting directory itself, then each
// parent in turn up to the filesystem root; the FIRST existing regular file at
// <dir>/<rel> wins. Directories already examined by an earlier walk are
// skipped, so the two walks together visit each directory at most once.
//
// WHY THIS IS SAFE despite being an upward search — three properties, all
// required, none incidental:
//
//  1. IT IS THE FALLBACK, NEVER THE OVERRIDE. Every caller consults its
//     environment variable first (envHelixLLMGatewayCACert returns immediately
//     on a non-empty HELIX_LLM_GATEWAY_CA_CERT) and only reaches here when the
//     operator has expressed no preference. A planted file can therefore never
//     displace an explicitly configured path.
//
//  2. `rel` IS A COMPILED-IN CONSTANT, NEVER CALLER- OR REQUEST-DERIVED. The
//     only argument passed today is helixLLMGatewayCACertRelPath, a const in
//     this file. Nothing in an HTTP request influences which name is sought,
//     so this is not a traversal surface. Keep it that way: passing a value
//     derived from untrusted input would turn a fixed lookup into an arbitrary
//     upward file probe.
//
//  3. THE WORST CASE IS BOUNDED AND USELESS TO AN ATTACKER. The value found is
//     used as a TLS trust anchor for exactly one destination — the loopback
//     gateway at 127.0.0.1:8443. Someone able to plant a CA in an ancestor
//     directory of the checkout would have to ALSO control that loopback
//     listener to gain anything, and anyone who controls a loopback listener
//     in this process's own namespace has already won by easier means. A
//     wrong-but-unattacked file simply fails the handshake, loudly.
//
// §11.4.28 / §11.4.177: no operator-specific absolute path appears in tracked
// code, and the result is always overridable by env.
func findRepoRelativeFile(rel string) string {
	starts := make([]string, 0, 2)
	if wd, err := os.Getwd(); err == nil && strings.TrimSpace(wd) != "" {
		starts = append(starts, wd)
	}
	if exe, err := os.Executable(); err == nil && strings.TrimSpace(exe) != "" {
		if resolved, lerr := filepath.EvalSymlinks(exe); lerr == nil && resolved != "" {
			exe = resolved
		}
		starts = append(starts, filepath.Dir(exe))
	}

	relPath := filepath.FromSlash(rel)
	seen := make(map[string]bool)
	for _, start := range starts {
		dir := start
		for {
			if seen[dir] {
				// Everything above this directory was already checked by an
				// earlier walk.
				break
			}
			seen[dir] = true
			candidate := filepath.Join(dir, relPath)
			if info, statErr := os.Stat(candidate); statErr == nil && !info.IsDir() {
				return candidate
			}
			parent := filepath.Dir(dir)
			if parent == dir {
				break
			}
			dir = parent
		}
	}
	return ""
}

// resolveHelixLLMGatewayProvider constructs the local HelixLLM GATEWAY route:
// a REAL *llm.OpenAICompatibleProvider (the same generic OpenAI-compatible
// HTTP client the coder and llama.cpp routes use — reused, not rewritten, per
// CONST-036 / §11.4.74) pointed at the gateway's TLS endpoint, with the
// gateway's self-signed CA added to the client's trust pool.
//
// WHY THIS ROUTE EXISTS AT ALL — the capability difference is the whole
// point (HXC-002-F3-03). Probed live against both backends with identical
// tool-carrying requests:
//
//	gateway  https://127.0.0.1:8443/v1 -> finish_reason "tool_calls", with a
//	                                      populated structured tool_calls[]
//	                                      array the caller can execute.
//	coder    http://localhost:18434     -> finish_reason "stop", with a fenced
//	                                      json blob buried in the message
//	                                      content that nothing downstream
//	                                      parses.
//
// SAME underlying model — the gateway performs the translation. Any caller
// that needs executable tool calls (every agentic client speaking the OpenAI
// wire shape) is broken on the coder route and works on this one.
//
// TLS IS MANDATORY HERE, AND SO IS THE CA. The gateway serves a self-signed
// certificate, so without the CA the dial fails with an unknown-authority
// error. A missing CA is therefore reported as a construction error naming
// the file it could not find and the env var that overrides it — it is NEVER
// downgraded to an unverified connection, and it is NEVER quietly turned into
// a fall-through to the coder route. That fall-through is precisely the
// failure this function must not have: the caller would receive a plausible
// 200 carrying a fenced json blob and no tool calls, with no way to tell that
// the gateway never answered (CONST-035 / §11.4.6).
//
// HONEST FAILURE, END TO END. Every failure mode below surfaces as a real
// error that names its cause:
//
//	CA not found            -> error naming helixLLMGatewayCACertRelPath and
//	                           HELIX_LLM_GATEWAY_CA_CERT (this function).
//	CA unreadable / not PEM -> error from newOpenAICompatibleHTTPClient naming
//	                           the path (internal/llm).
//	gateway down / TLS fail -> the provider's own dial error, surfaced by the
//	                           handler as 502 with
//	                           "provider":"helixllm-gateway", naming the
//	                           backend the caller actually chose.
//
// No API key: the gateway is an unauthenticated loopback service (verified —
// GET /v1/models answers 200 with no Authorization header), so nothing is
// read or leaked (CONST-042 / §12.1).
//
// The coder route (resolveHelixLLMLocalProvider) is untouched and remains
// reachable under "helixllm"/"local" exactly as before — this is strictly an
// ADDED capability, never a replacement (§11.4.122).
func resolveHelixLLMGatewayProvider(model string) (llm.Provider, error) {
	caPath := envHelixLLMGatewayCACert()
	if caPath == "" {
		return nil, fmt.Errorf(
			"helixllm gateway route selected but its CA certificate could not be "+
				"located: no %s set, and %q was not found by walking up from the "+
				"working directory or the binary's location. The gateway serves a "+
				"self-signed certificate, so it cannot be verified without this CA "+
				"— set %s to the certificate's absolute path. (TLS verification is "+
				"NOT skipped, and this request is NOT silently rerouted to the "+
				"local coder.)",
			helixLLMGatewayCACertEnv, helixLLMGatewayCACertRelPath,
			helixLLMGatewayCACertEnv)
	}

	endpoint, sourceKey := envHelixLLMGatewayEndpointWithSource()
	cfg := llm.OpenAICompatibleConfig{
		BaseURL:      endpoint,
		DefaultModel: strings.TrimSpace(model),
		Timeout:      120 * time.Second,
		// The base URL already ends in "/v1" (the form the gateway
		// advertises), so the endpoint paths must be the REMAINDER — the
		// provider's defaults are "/v1/models" and "/v1/chat/completions",
		// which would double the prefix into ".../v1/v1/models" (measured:
		// 404). Setting both explicitly keeps the pair consistent at the one
		// place the base URL is chosen.
		ModelEndpoint:    "/models",
		ChatEndpoint:     "/chat/completions",
		StreamingSupport: true,
		CACertFile:       caPath,
	}
	provider, err := llm.NewOpenAICompatibleProvider("helixllm-gateway", cfg)
	if err != nil {
		if errors.Is(err, llm.ErrCloudDisabled) {
			return nil, localRouteRemoteEndpointError(
				"gateway", sourceKey, helixLLMGatewayEndpointEnv, endpoint, err)
		}
		return nil, fmt.Errorf("failed to construct helixllm gateway provider: %w", err)
	}
	if provider == nil {
		return nil, fmt.Errorf("helixllm gateway provider constructed nil without an error")
	}
	return provider, nil
}

// llamaCppHostEnv is the env var this repository ALREADY documents for the
// local llama.cpp server: root `.env.example:89` ships
// `HELIX_LLAMA_CPP_HOST=http://localhost:18434` (fixed from the historical
// `:8080` self-POST hazard by commit a74ae7cb — see envLlamaCppHost's
// doc-comment below for why `:8080` is wrong) and the LLMsVerifier
// integration plan tabulates it as llamacpp's host binding. Until this change
// it was a DEAD key — a repo-wide grep found ZERO Go readers, so an operator
// who set it got silence rather than a redirected endpoint. Reusing the
// established name rather than minting a new one is CONST-036 / §11.4.74.
//
// Base URL only, with NO trailing "/v1": llama-server answers under "/v1/..."
// and OpenAICompatibleConfig's ChatEndpoint / ModelEndpoint defaults already
// carry that prefix.
const llamaCppHostEnv = "HELIX_LLAMA_CPP_HOST"

// ollamaHostEnv is the sibling dead key from the same `.env.example` block
// (`HELIX_OLLAMA_HOST=http://localhost:11434`, line 54) — documented,
// tabulated, and likewise read by no Go code until this change. Honouring it
// is the same one-line defect class as llamaCppHostEnv; the default when it is
// unset is byte-identical to what this route always used, so no existing
// deployment's behaviour changes.
const ollamaHostEnv = "HELIX_OLLAMA_HOST"

// ollamaDefaultHost is Ollama's standard local endpoint — the value this
// route hardcoded before ollamaHostEnv was honoured, preserved exactly so the
// zero-config default is unchanged.
const ollamaDefaultHost = "http://localhost:11434"

// envOllamaHost reads HELIX_OLLAMA_HOST, falling back to ollamaDefaultHost.
func envOllamaHost() string {
	if v := strings.TrimSpace(os.Getenv(ollamaHostEnv)); v != "" {
		return v
	}
	return ollamaDefaultHost
}

// envLlamaCppHost resolves the local llama.cpp base URL, in precedence order:
//
//  1. HELIX_LLAMA_CPP_HOST — the llama.cpp-specific key `.env.example`
//     documents, so an operator who wants llama.cpp on its own host/port sets
//     exactly one obvious variable.
//  2. HELIX_LLM_LOCAL_OPENAI_ENDPOINT — the project-wide local-OpenAI endpoint
//     convention the sibling helixllm route and submodules/helix_agent already
//     read. Falling through to it means a deployment that already points
//     HelixCode at its local OpenAI-compatible server does not have to
//     configure the same address twice.
//  3. helixLLMLocalDefaultEndpoint (http://localhost:18434) — the project's
//     LIVE local llama.cpp port, per config/llmsverifier/config.yaml's
//     `llamacpp` row (`http://localhost:18434/v1`, served by
//     helixllm-coder.service).
//
// Note on the default (§11.4.6 — recorded rather than silent): root
// `.env.example` now documents `HELIX_LLAMA_CPP_HOST=http://localhost:18434`
// (fixed by commit a74ae7cb from the historical `:8080`), matching this
// function's own default — but `configs/verifier.yaml` still shows
// `http://localhost:8080`, which is llama-server's UPSTREAM default. 8080 is
// also the port HelixCode's OWN API server listens on, so that value makes
// the server POST completions to itself. Measured pre-fix against a live
// server: the request came back
// `502 {"error":"generation failed: llama.cpp returned status 404",
// "provider":"llama-cpp"}` — a 404 from HelixCode's own router, which has no
// /v1/completions route. 18434 both avoids that collision and agrees with the
// sibling helixllm route's default, so the two local routes cannot disagree
// about where "the local server" is. An operator wanting the upstream 8080
// still gets it by setting HELIX_LLAMA_CPP_HOST explicitly.
func envLlamaCppHost() string {
	host, _ := envLlamaCppHostWithSource()
	return host
}

// envLlamaCppHostWithSource is envLlamaCppHost plus the NAME of the variable
// the value came from. The distinction is load-bearing rather than cosmetic:
// this route falls through to the project-wide
// HELIX_LLM_LOCAL_OPENAI_ENDPOINT when its own key is unset, so an error that
// always blamed HELIX_LLAMA_CPP_HOST would send an operator to edit a variable
// they never set. Empty means the compiled-in default supplied the value.
func envLlamaCppHostWithSource() (string, string) {
	if v := strings.TrimSpace(os.Getenv(llamaCppHostEnv)); v != "" {
		return v, llamaCppHostEnv
	}
	return envHelixLLMLocalEndpointWithSource()
}

// resolveLlamaCppLocalProvider constructs the local llama.cpp route as a REAL
// *llm.OpenAICompatibleProvider (internal/llm/openai_compatible_provider.go)
// — the same generic OpenAI-compatible HTTP client HelixCode already ships and
// already uses for VLLM / LMStudio / LocalAI (reused, not rewritten, per
// CONST-036 / §11.4.74) — pointed at envLlamaCppHost().
//
// WHY NOT llm.NewLlamaCPPProvider. That adapter still exists, is still
// registered in the provider factory (newLlamaCPPFromEntry), and is NOT
// removed or disabled by this change (§11.4.122) — it remains the right
// client for llama.cpp's legacy `/completion` surface and keeps its own
// callers and tests. It is simply not usable from THESE handlers, for two
// independently-measured reasons:
//
//	(a) WRONG ENDPOINT SHAPE. LlamaCPPProvider.Generate always POSTs to
//	    `/v1/completions`, and when request.Messages is non-empty it sends a
//	    `messages` key there. A real llama-server rejects that with
//	    `400 key 'prompt' not found` — already recorded verbatim in
//	    resolveHelixLLMLocalProvider's doc-comment from a live verification.
//	    buildLLMRequest builds a message list unconditionally, so EVERY
//	    request from this surface takes that failing shape.
//	    OpenAICompatibleProvider POSTs messages to `/v1/chat/completions`,
//	    which is what llama.cpp's OpenAI-compatible server serves.
//
//	(b) UNCONFIGURABLE HOST. resolveLLMProvider builds its
//	    ProviderConfigEntry without an Endpoint, so newLlamaCPPFromEntry gets
//	    ServerHost == "" and Generate falls back to its hardcoded
//	    `http://localhost:8080`. There was no way to point it elsewhere from
//	    this surface, and that literal collides with our own listener (see
//	    envLlamaCppHost).
//
// This is NOT a fallback: a caller who names llamacpp gets llama.cpp, never a
// different backend silently substituted. When the configured endpoint is
// unreachable the provider surfaces the real dial error and the handler
// answers 502 with `"provider":"llamacpp"` — an honest failure naming the
// backend the caller actually chose (CONST-035 / §11.4.6).
//
// No API key: a local llama-server is an unauthenticated loopback/LAN
// service, so nothing is read, logged or leaked (CONST-042 / §12.1).
func resolveLlamaCppLocalProvider(model string) (llm.Provider, error) {
	endpoint, sourceKey := envLlamaCppHostWithSource()
	cfg := llm.OpenAICompatibleConfig{
		BaseURL:          endpoint,
		DefaultModel:     strings.TrimSpace(model),
		Timeout:          120 * time.Second,
		StreamingSupport: true,
	}
	provider, err := llm.NewOpenAICompatibleProvider("llamacpp", cfg)
	if err != nil {
		if errors.Is(err, llm.ErrCloudDisabled) {
			// overrideKey is the variable an operator should set to correct
			// THIS route specifically, which is llamacpp's own key even when
			// the offending value arrived via the shared fallback named by
			// sourceKey.
			return nil, localRouteRemoteEndpointError(
				"llamacpp", sourceKey, llamaCppHostEnv, endpoint, err)
		}
		return nil, fmt.Errorf("failed to construct llamacpp local provider: %w", err)
	}
	if provider == nil {
		return nil, fmt.Errorf("llamacpp local provider constructed nil without an error")
	}
	return provider, nil
}
