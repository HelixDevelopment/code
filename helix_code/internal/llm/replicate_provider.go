package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/google/uuid"
)

// replicate_provider.go — the genuine factory wiring for Replicate
// (https://replicate.com), closing the §11.4.124 "never-completed wiring
// from birth" gap for the ONE catalogued provider that has no live path
// through HostedOpenAICompatibleCatalogue() (openai_compatible_catalogue.go):
// Replicate's wire protocol is an async create-prediction-then-poll flow,
// not the OpenAI chat-completions contract every catalogue entry assumes.
//
// Reuse-before-reimplement note (§11.4.74 / CONST-051(B)): a fuller bespoke
// client already exists at internal/llm/providers/replicate (client.go) and
// implements this exact wire protocol. It is NOT imported here because doing
// so would create a Go import cycle: package
// dev.helix.code/internal/llm/providers/replicate already imports package
// dev.helix.code/internal/llm (for *llm.LLMRequest / *llm.LLMResponse), so
// this package (llm) importing it back would be llm -> providers/replicate
// -> llm, which the Go compiler rejects. That existing package is left
// untouched (still directly usable, still covered by its own tests); this
// file re-implements the same small (~100-line) wire protocol natively in
// package llm — the same pattern every other in-package provider
// (groq_provider.go, xiaomi_provider.go, ...) already follows, and the only
// way to satisfy the full llm.Provider interface without the cycle.
//
// The sibling submodule helix_agent (a SEPARATE Go module, so no import
// cycle applies there) already carries an authoritative, working Replicate
// provider at internal/llm/providers/replicate/replicate.go
// (dev.helix.agent module). Per CONST-051(B) copying DATA (not code) across
// module boundaries is permitted — the env-var aliases
// {"REPLICATE_API_KEY", "REPLICATE_API_TOKEN", "ApiKey_Replicate"} and the
// fallback model list below are lifted from that file's EnvVars /
// FallbackModels tables.

const (
	replicateDefaultBaseURL = "https://api.replicate.com/v1"
	replicateDefaultModel   = "meta/meta-llama-3-70b-instruct"
	replicateDefaultTimeout = 300 * time.Second // predictions can have cold starts
	replicatePollInterval   = 2 * time.Second
	replicateMaxPollCycles  = 30 // 30 * 2s = 60s ceiling, matches the pre-existing bespoke client
)

// replicateSeedModels is the offline fallback model list, data-lifted from
// helix_agent's replicate.Provider FallbackModels table (CONST-051(B)).
// CONST-036: GetModels() here is this seed list — Replicate has no
// lightweight, unauthenticated model-catalogue endpoint suitable for the
// same live-refresh treatment other providers get, so the seed list is the
// authoritative-for-this-provider set until a Phase 3 live-catalogue lands.
var replicateSeedModels = []ModelInfo{
	{
		Name:        "meta/meta-llama-3.1-405b-instruct",
		Provider:    ProviderTypeReplicate,
		ContextSize: 128000,
		MaxTokens:   4096,
		Description: "Meta Llama 3.1 405B Instruct on Replicate",
	},
	{
		Name:        "meta/meta-llama-3-70b-instruct",
		Provider:    ProviderTypeReplicate,
		ContextSize: 8192,
		MaxTokens:   4096,
		Description: "Meta Llama 3 70B Instruct on Replicate",
	},
	{
		Name:        "meta/meta-llama-3-8b-instruct",
		Provider:    ProviderTypeReplicate,
		ContextSize: 8192,
		MaxTokens:   4096,
		Description: "Meta Llama 3 8B Instruct on Replicate",
	},
	{
		Name:        "meta/llama-2-70b-chat",
		Provider:    ProviderTypeReplicate,
		ContextSize: 4096,
		MaxTokens:   4096,
		Description: "Meta Llama 2 70B Chat on Replicate",
	},
	{
		Name:        "mistralai/mixtral-8x7b-instruct-v0.1",
		Provider:    ProviderTypeReplicate,
		ContextSize: 32768,
		MaxTokens:   4096,
		Description: "Mistral Mixtral 8x7B Instruct on Replicate",
	},
}

func init() {
	for i := range replicateSeedModels {
		EnrichModelInfo(&replicateSeedModels[i])
	}
}

// ReplicateProvider implements the llm.Provider interface for Replicate's
// async prediction API (https://replicate.com/docs/reference/http).
type ReplicateProvider struct {
	apiKey       string
	baseURL      string
	defaultModel string
	httpClient   *http.Client
	models       []ModelInfo
	pollInterval time.Duration // test-only override; 0 -> replicatePollInterval
}

// replicateAPIKeyFromEnv resolves an API key from the recognised Replicate
// env-var aliases (keyrecognition.go's ProviderEnvAliases table), honouring
// the same present/non-placeholder rule every other hosted provider in this
// package uses. Returns "" when none is present.
func replicateAPIKeyFromEnv() string {
	for _, alias := range ProviderEnvAliases()[ProviderTypeReplicate] {
		v, ok := os.LookupEnv(alias)
		if !ok {
			continue
		}
		if isPlaceholderKey(v) {
			continue
		}
		return v
	}
	return ""
}

// NewReplicateProvider creates a new Replicate provider. The API key
// resolves from config.APIKey first, then from the recognised env-var
// aliases (REPLICATE_API_KEY / REPLICATE_API_TOKEN / ApiKey_Replicate) —
// the same config-then-env precedence NewGroqProvider / NewXiaomiProvider
// use. Returns an error when no key is present anywhere (anti-bluff: never
// constructs a provider that could not possibly authenticate).
func NewReplicateProvider(config ProviderConfigEntry) (*ReplicateProvider, error) {
	apiKey := config.APIKey
	if apiKey == "" || isPlaceholderKey(apiKey) {
		apiKey = replicateAPIKeyFromEnv()
	}
	if apiKey == "" {
		return nil, fmt.Errorf("replicate API key not provided (set REPLICATE_API_KEY, REPLICATE_API_TOKEN, or ApiKey_Replicate)")
	}

	baseURL := config.Endpoint
	if baseURL == "" {
		baseURL = replicateDefaultBaseURL
	}

	defaultModel := replicateDefaultModel
	if len(config.Models) > 0 && config.Models[0] != "" {
		defaultModel = config.Models[0]
	}

	timeout := replicateDefaultTimeout
	if val, ok := config.Parameters["timeout"].(float64); ok {
		timeout = time.Duration(val) * time.Second
	}

	provider := &ReplicateProvider{
		apiKey:       apiKey,
		baseURL:      baseURL,
		defaultModel: defaultModel,
		httpClient:   &http.Client{Timeout: timeout},
		models:       replicateSeedModels,
	}

	log.Printf("✅ Replicate provider initialized with %d models", len(provider.models))
	return provider, nil
}

// GetType returns the provider type.
func (p *ReplicateProvider) GetType() ProviderType { return ProviderTypeReplicate }

// GetName returns the provider name.
func (p *ReplicateProvider) GetName() string { return "replicate" }

// GetModels returns the known Replicate models.
func (p *ReplicateProvider) GetModels() []ModelInfo { return p.models }

// GetCapabilities returns the provider-level capabilities.
func (p *ReplicateProvider) GetCapabilities() []ModelCapability {
	return []ModelCapability{
		CapabilityTextGeneration,
		CapabilityCodeGeneration,
		CapabilityReasoning,
	}
}

type replicateInput struct {
	Prompt      string  `json:"prompt"`
	MaxTokens   int     `json:"max_tokens,omitempty"`
	Temperature float64 `json:"temperature,omitempty"`
}

type replicateRequest struct {
	Input replicateInput `json:"input"`
}

type replicatePrediction struct {
	ID     string      `json:"id"`
	Status string      `json:"status"`
	Output interface{} `json:"output,omitempty"`
	Error  string      `json:"error,omitempty"`
}

// Generate creates a Replicate prediction for the request's last message and
// polls until the prediction reaches a terminal status. Mirrors the wire
// protocol of internal/llm/providers/replicate.Client.Generate (see the
// package doc comment above for why that client is not imported directly).
func (p *ReplicateProvider) Generate(ctx context.Context, request *LLMRequest) (*LLMResponse, error) {
	startTime := time.Now()

	prompt := ""
	if len(request.Messages) > 0 {
		prompt = request.Messages[len(request.Messages)-1].Content
	}
	model := request.Model
	if model == "" {
		model = p.defaultModel
	}

	body := replicateRequest{
		Input: replicateInput{
			Prompt:      prompt,
			MaxTokens:   request.MaxTokens,
			Temperature: request.Temperature,
		},
	}
	if body.Input.MaxTokens == 0 {
		body.Input.MaxTokens = 4096
	}
	if body.Input.Temperature == 0 {
		body.Input.Temperature = 0.7
	}

	data, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshal replicate request: %w", err)
	}

	predURL := p.baseURL + "/models/" + model + "/predictions"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, predURL, bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("build replicate prediction request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+p.apiKey)

	resp, err := p.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("replicate request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return nil, fmt.Errorf("replicate API error: HTTP %d", resp.StatusCode)
	}

	var pred replicatePrediction
	if err := json.NewDecoder(resp.Body).Decode(&pred); err != nil {
		return nil, fmt.Errorf("decode replicate prediction: %w", err)
	}

	output, finalStatus, finalErrMsg, err := p.waitForCompletion(ctx, pred)
	if err != nil {
		return nil, err
	}

	return &LLMResponse{
		ID:             uuid.New(),
		RequestID:      request.ID,
		Content:        output,
		ProcessingTime: time.Since(startTime),
		CreatedAt:      time.Now(),
		Model:          model,
		Err:            mapReplicateStatusToLLMErr(finalStatus, finalErrMsg),
	}, nil
}

// waitForCompletion polls the Replicate predictions endpoint until terminal
// status, starting from the prediction already returned by the create call
// (which may already be terminal for very fast models).
func (p *ReplicateProvider) waitForCompletion(ctx context.Context, pred replicatePrediction) (string, string, string, error) {
	if terminal, output, status, errMsg := replicateTerminalResult(pred); terminal {
		return output, status, errMsg, nil
	}

	interval := p.pollInterval
	if interval == 0 {
		interval = replicatePollInterval
	}

	for i := 0; i < replicateMaxPollCycles; i++ {
		select {
		case <-ctx.Done():
			return "", "", "", ctx.Err()
		case <-time.After(interval):
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.baseURL+"/predictions/"+pred.ID, nil)
		if err != nil {
			return "", "", "", fmt.Errorf("build replicate poll request: %w", err)
		}
		req.Header.Set("Authorization", "Bearer "+p.apiKey)

		resp, err := p.httpClient.Do(req)
		if err != nil {
			return "", "", "", fmt.Errorf("replicate poll request: %w", err)
		}
		var polled replicatePrediction
		decodeErr := json.NewDecoder(resp.Body).Decode(&polled)
		resp.Body.Close()
		if decodeErr != nil {
			return "", "", "", fmt.Errorf("decode replicate poll response: %w", decodeErr)
		}

		if terminal, output, status, errMsg := replicateTerminalResult(polled); terminal {
			return output, status, errMsg, nil
		}
	}
	return "", "", "", fmt.Errorf("replicate prediction timed out after %s", time.Duration(replicateMaxPollCycles)*replicatePollInterval)
}

// replicateTerminalResult reports whether pred has reached a terminal
// status and, if so, the (output, status, errorMessage) triple to surface.
func replicateTerminalResult(pred replicatePrediction) (terminal bool, output, status, errMsg string) {
	switch pred.Status {
	case "succeeded":
		return true, fmt.Sprintf("%v", pred.Output), pred.Status, ""
	case "failed":
		return true, "", pred.Status, pred.Error
	case "canceled":
		return true, "", pred.Status, ""
	default:
		return false, "", "", ""
	}
}

// mapReplicateStatusToLLMErr mirrors
// providers/replicate.mapReplicateStatusToErr, reusing the same
// llm.ErrReplicatePredictionFailed sentinel declared in missing_types.go so
// callers can errors.Is() against it regardless of which Replicate code path
// produced the response.
func mapReplicateStatusToLLMErr(status, errMsg string) error {
	if status == "failed" {
		if errMsg == "" {
			return ErrReplicatePredictionFailed
		}
		return fmt.Errorf("%w: %s", ErrReplicatePredictionFailed, errMsg)
	}
	return nil
}

// GenerateStream drives the same create-then-poll flow as Generate and
// emits the result as a single chunk. Replicate's HTTP API has no
// token-level SSE stream for arbitrary community models (the only
// streaming surface Replicate exposes is a per-prediction "stream" URL that
// is model-specific and not guaranteed present) — a single real chunk is an
// honest realization of the interface contract, not a simulation: it is the
// same real network round trip Generate makes, not synthesized content.
func (p *ReplicateProvider) GenerateStream(ctx context.Context, request *LLMRequest, ch chan<- LLMResponse) error {
	defer close(ch)
	resp, err := p.Generate(ctx, request)
	if err != nil {
		return err
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case ch <- *resp:
	}
	return nil
}

// IsAvailable reports whether the provider has a usable API key. This
// mirrors GroqProvider.IsAvailable's "key present" check rather than
// spending a real prediction (predictions are billed) on every
// availability probe.
func (p *ReplicateProvider) IsAvailable(ctx context.Context) bool {
	return p.apiKey != ""
}

// GetHealth performs a real, free (non-billed) authenticated request against
// Replicate's account endpoint (GET /v1/account) to verify the configured
// key is actually accepted by the API — a genuine HTTP round trip, never a
// simulated result, and cheaper than GetHealth implementations (e.g. Groq's)
// that spend a real generation on every health check.
func (p *ReplicateProvider) GetHealth(ctx context.Context) (*ProviderHealth, error) {
	start := time.Now()
	health := &ProviderHealth{
		LastCheck:  time.Now(),
		ModelCount: len(p.models),
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.baseURL+"/account", nil)
	if err != nil {
		health.Status = "unhealthy"
		health.ErrorCount = 1
		health.Message = err.Error()
		health.Latency = time.Since(start)
		return health, err
	}
	req.Header.Set("Authorization", "Bearer "+p.apiKey)

	resp, err := p.httpClient.Do(req)
	if err != nil {
		health.Status = "unhealthy"
		health.ErrorCount = 1
		health.Message = err.Error()
		health.Latency = time.Since(start)
		return health, err
	}
	defer resp.Body.Close()

	health.Latency = time.Since(start)
	if resp.StatusCode == http.StatusOK {
		health.Status = "healthy"
		return health, nil
	}
	health.Status = "unhealthy"
	health.ErrorCount = 1
	health.Message = fmt.Sprintf("replicate account endpoint returned HTTP %d", resp.StatusCode)
	return health, fmt.Errorf("%s", health.Message)
}

// Close releases resources held by the provider.
func (p *ReplicateProvider) Close() error {
	p.httpClient.CloseIdleConnections()
	return nil
}

// GetContextWindow returns the maximum context window across known models.
func (p *ReplicateProvider) GetContextWindow() int {
	maxCtx := 0
	for _, m := range p.models {
		if m.ContextSize > maxCtx {
			maxCtx = m.ContextSize
		}
	}
	if maxCtx == 0 {
		maxCtx = 8192
	}
	return maxCtx
}

// CountTokens returns an estimated token count for text using the shared
// char-based fallback (1 token ~= 3.5 chars) — Replicate exposes no
// tokenizer endpoint of its own.
func (p *ReplicateProvider) CountTokens(text string) (int, error) {
	return CharBasedTokenCount(text)
}
