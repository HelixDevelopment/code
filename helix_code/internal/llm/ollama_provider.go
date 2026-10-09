package llm

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
)

// OllamaProvider implements the LLM provider interface for Ollama
type OllamaProvider struct {
	config    OllamaConfig
	apiClient *http.Client
	models    []OllamaModel

	// isRunning is read by Generate/GenerateStream/IsAvailable/GetHealth from
	// arbitrary caller goroutines and flipped to false by Close(); it MUST be
	// accessed atomically. The prior plain bool was a data race (the -race
	// detector flagged concurrent IsAvailable read vs the Close write) — a real
	// defect under any concurrent use of the provider, e.g. a load balancer
	// health-checking on one goroutine while another Closes it.
	isRunning atomic.Bool

	// discoverOnce guards lazy model discovery so the blocking HTTP
	// round-trip to /api/tags runs at most once, on first real use —
	// never in the constructor (speed programme P1-T02 / R1 B02).
	discoverOnce sync.Once
}

// OllamaConfig holds configuration for Ollama
type OllamaConfig struct {
	BaseURL       string        `json:"base_url"`
	DefaultModel  string        `json:"default_model"`
	Timeout       time.Duration `json:"timeout"`
	KeepAlive     time.Duration `json:"keep_alive"`
	StreamEnabled bool          `json:"stream_enabled"`
}

// OllamaModel represents an Ollama model
type OllamaModel struct {
	Name       string             `json:"name"`
	ModifiedAt time.Time          `json:"modified_at"`
	Size       int64              `json:"size"`
	Digest     string             `json:"digest"`
	Details    OllamaModelDetails `json:"details"`
}

// OllamaChatMsg is the `message` object returned by Ollama's POST /api/chat.
// Its Content field carries the assistant completion text for chat-style
// requests (the /api/generate endpoint uses the top-level `response` field
// instead — see OllamaAPIResponse doc comment).
type OllamaChatMsg struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// completionText returns the generated text from an Ollama response,
// transparently handling BOTH endpoint shapes: /api/chat populates
// message.content, /api/generate populates the top-level response. Preferring
// message.content (this provider's actual endpoint) and falling back to
// response means the provider yields the REAL completion for the end user
// regardless of which endpoint produced it.
func (r *OllamaAPIResponse) completionText() string {
	if r.Message.Content != "" {
		return r.Message.Content
	}
	return r.Response
}

// OllamaModelDetails contains model-specific details
type OllamaModelDetails struct {
	Format            string   `json:"format"`
	Family            string   `json:"family"`
	Families          []string `json:"families"`
	ParameterSize     string   `json:"parameter_size"`
	QuantizationLevel string   `json:"quantization_level"`
}

// OllamaAPIRequest represents a request to the Ollama API
type OllamaAPIRequest struct {
	Model    string                 `json:"model"`
	Prompt   string                 `json:"prompt"`
	Messages []Message              `json:"messages"`
	Stream   bool                   `json:"stream"`
	Options  map[string]interface{} `json:"options"`
}

// OllamaAPIResponse represents a response from the Ollama API.
//
// Ollama has TWO generation endpoints with DIFFERENT response shapes:
//   - POST /api/generate (prompt-based) puts the completion in `response`.
//   - POST /api/chat     (messages-based) puts the completion in
//     `message.content` and leaves `response` EMPTY.
//
// This provider posts to /api/chat (it sends Messages), so the completion
// arrives in Message.Content — NOT in Response. The struct previously only
// decoded `response`, so every real /api/chat call decoded successfully but
// produced an EMPTY Content for the end user (a CONST-035 / Article XI §11.9
// bluff: unit tests mocked the `response` field and passed, while real
// generation returned nothing). Message is now decoded and consumed via
// completionText(); `response` is still honoured as a fallback so a future
// switch to /api/generate keeps working.
type OllamaAPIResponse struct {
	Model     string        `json:"model"`
	CreatedAt string        `json:"created_at"`
	Response  string        `json:"response"`
	Message   OllamaChatMsg `json:"message"`
	Done      bool          `json:"done"`
	// DoneReason is set by Ollama 0.1.30+ on the terminal frame and
	// indicates why generation stopped: "stop" (clean end-of-turn),
	// "length" (num_predict / max-tokens reached → ErrResponseTruncated),
	// "load" / "unload" (model lifecycle), "" (older Ollama versions).
	// Round-46 LLMResponse.Err wiring consumes this field; older versions
	// fall through to no Err since the truncation signal is not knowable
	// from the on-wire payload.
	DoneReason         string `json:"done_reason,omitempty"`
	Context            []int  `json:"context"`
	TotalDuration      int64  `json:"total_duration"`
	LoadDuration       int64  `json:"load_duration"`
	PromptEvalCount    int    `json:"prompt_eval_count"`
	PromptEvalDuration int64  `json:"prompt_eval_duration"`
	EvalCount          int    `json:"eval_count"`
	EvalDuration       int64  `json:"eval_duration"`
}

// mapOllamaDoneReasonToErr returns the round-46 sentinel matching an
// Ollama done_reason, or nil if the reason indicates a clean stop or
// is empty (older Ollama versions). Ollama exposes no content-filter
// hook on the wire, so ErrResponseContentBlocked is not reachable here.
// Reference: https://github.com/ollama/ollama/blob/main/docs/api.md
func mapOllamaDoneReasonToErr(reason string) error {
	switch reason {
	case "length":
		return ErrResponseTruncated
	default:
		return nil
	}
}

// NewOllamaProvider creates a new Ollama provider.
//
// Speed programme P1-T02 (R1 B02): the constructor performs ZERO network
// I/O. Model discovery (the blocking GET /api/tags round-trip) is deferred
// to first real use — see ensureModelsDiscovered — so every CLI start (even
// `--help`) no longer pays a synchronous Ollama round-trip. Discovery still
// runs exactly once, on first GetModels/GetHealth/getModelName call, and
// behaves identically to the previous eager path thereafter.
func NewOllamaProvider(config OllamaConfig) (*OllamaProvider, error) {
	provider := &OllamaProvider{
		config: config,
		apiClient: &http.Client{
			Timeout: config.Timeout,
			// Bounded connection pool. The previous zero-value client used the
			// shared http.DefaultTransport, which has no per-host idle-connection
			// cap (DefaultMaxIdleConnsPerHost == 2 but MaxConnsPerHost == 0, i.e.
			// unbounded), so under concurrent load every in-flight Get opened a
			// fresh keep-alive connection and each connection's read/write loop
			// goroutine lingered — a connection/goroutine explosion (≈1 pair per
			// concurrent request) against a single local Ollama endpoint. A
			// bounded, provider-owned Transport caps total + per-host connections
			// so concurrent GetHealth/IsAvailable/Generate reuse a small pool
			// instead of leaking goroutines.
			Transport: &http.Transport{
				MaxIdleConns: 16,
				// Keep the per-host idle pool small (2) so a burst of concurrent
				// health/generate calls does not leave a large fleet of idle
				// keep-alive connections (and their per-connection reader
				// goroutines) lingering after the burst. Two pooled connections
				// to a single local Ollama endpoint is ample for reuse.
				MaxIdleConnsPerHost: 2,
				// Short idle timeout so pooled connections (and their per-conn
				// reader goroutines) reap promptly after a burst of concurrent
				// requests instead of lingering for the default 90s. NOTE:
				// MaxConnsPerHost is intentionally NOT set — capping live
				// connections while a caller closes a response body without
				// draining it can wedge the pool (request goroutines block
				// forever waiting for a connection that never returns). Bounding
				// only the IDLE pool gives connection reuse without that hazard.
				IdleConnTimeout: 2 * time.Second,
			},
		},
	}
	provider.isRunning.Store(true)

	return provider, nil
}

// ensureModelsDiscovered runs model discovery lazily, at most once, on the
// first real use of the provider. The blocking HTTP round-trip to /api/tags
// is paid here instead of in the constructor (speed programme P1-T02). A
// discovery failure is logged (same posture as the previous eager path) and
// leaves p.models empty; a subsequent successful call cannot re-run because
// sync.Once fires only once — discovery is best-effort exactly as before.
func (p *OllamaProvider) ensureModelsDiscovered() {
	p.discoverOnce.Do(func() {
		if err := p.discoverModels(); err != nil {
			log.Printf("Warning: Failed to discover Ollama models: %v", err)
		}
		log.Printf("✅ Ollama provider discovered %d models", len(p.models))
	})
}

// GetType returns the provider type
func (p *OllamaProvider) GetType() ProviderType {
	return ProviderTypeLocal
}

// GetName returns the provider name
func (p *OllamaProvider) GetName() string {
	return "ollama"
}

// BaseURL returns the provider's configured base URL. Exposed so
// out-of-package callers (and tests) can assert which endpoint a provider was
// actually constructed with — the same accessor, for the same reason, that
// OpenAICompatibleProvider.BaseURL already provides: it is what lets a guard
// prove an endpoint came from configuration rather than a literal baked into
// the call site (CONST-046 / §11.4.111). Carries no credential (CONST-042);
// Ollama's local HTTP API is unauthenticated.
func (p *OllamaProvider) BaseURL() string {
	return p.config.BaseURL
}

// GetModels returns available models. Discovery is triggered lazily on the
// first call (speed programme P1-T02) and the result is reused thereafter.
func (p *OllamaProvider) GetModels() []ModelInfo {
	p.ensureModelsDiscovered()

	var modelInfos []ModelInfo

	for _, model := range p.models {
		modelInfos = append(modelInfos, ModelInfo{
			Name:        model.Name,
			Provider:    ProviderTypeLocal,
			ContextSize: 4096, // Default context size
			MaxTokens:   4096,
			Description: fmt.Sprintf("Ollama model: %s", model.Name),
		})
	}

	for i := range modelInfos {
		EnrichModelInfo(&modelInfos[i])
	}

	return modelInfos
}

// GetCapabilities returns model capabilities
func (p *OllamaProvider) GetCapabilities() []ModelCapability {
	return []ModelCapability{
		CapabilityTextGeneration,
		CapabilityCodeGeneration,
		CapabilityCodeAnalysis,
		CapabilityPlanning,
		CapabilityDebugging,
		CapabilityRefactoring,
		CapabilityTesting,
	}
}

// Generate generates a response using Ollama
func (p *OllamaProvider) Generate(ctx context.Context, request *LLMRequest) (*LLMResponse, error) {
	if !p.isRunning.Load() {
		return nil, ErrProviderUnavailable
	}

	// Prepare API request
	apiRequest := OllamaAPIRequest{
		Model:    p.getModelName(request.Model),
		Messages: request.Messages,
		Stream:   false,
		Options: map[string]interface{}{
			"temperature": request.Temperature,
			"top_p":       request.TopP,
			"num_predict": request.MaxTokens,
		},
	}

	// Make API call
	startTime := time.Now()
	response, err := p.makeAPIRequest(ctx, apiRequest)
	if err != nil {
		return nil, fmt.Errorf("API request failed: %w", err)
	}

	processingTime := time.Since(startTime)

	return &LLMResponse{
		ID:        uuid.New(),
		RequestID: request.ID,
		// /api/chat puts the completion in message.content (not the top-level
		// `response` field) — completionText() reads the correct one so the end
		// user receives the REAL generated text (CONST-035 / Article XI §11.9).
		Content: response.completionText(),
		Usage: Usage{
			PromptTokens:     response.PromptEvalCount,
			CompletionTokens: response.EvalCount,
			TotalTokens:      response.PromptEvalCount + response.EvalCount,
		},
		FinishReason:   response.DoneReason,
		ProcessingTime: processingTime,
		CreatedAt:      time.Now(),
		// Round-46 LLMResponse.Err wiring (CONST-035 / Article XI §11.9):
		// Ollama signals truncation via done_reason="length". See
		// mapOllamaDoneReasonToErr doc comment for the closed mapping.
		Err: mapOllamaDoneReasonToErr(response.DoneReason),
	}, nil
}

// GenerateStream generates a streaming response
func (p *OllamaProvider) GenerateStream(ctx context.Context, request *LLMRequest, ch chan<- LLMResponse) error {
	// Channel-ownership contract (see Provider.GenerateStream interface doc):
	// the provider (the SENDER) is the SOLE closer of ch, and MUST close it on
	// every return path — success, error, and ctx-cancel. The consumer never
	// closes ch; a double-close would panic in a spawned goroutine and crash the
	// process (server defect #5). defer guarantees close on every return path.
	defer close(ch)
	if !p.isRunning.Load() {
		return ErrProviderUnavailable
	}

	// Prepare streaming API request
	apiRequest := OllamaAPIRequest{
		Model:    p.getModelName(request.Model),
		Messages: request.Messages,
		Stream:   true,
		Options: map[string]interface{}{
			"temperature": request.Temperature,
			"top_p":       request.TopP,
			"num_predict": request.MaxTokens,
		},
	}

	// Make streaming request
	return p.makeStreamingRequest(ctx, apiRequest, ch)
}

// IsAvailable checks if the provider is available
func (p *OllamaProvider) IsAvailable(ctx context.Context) bool {
	if !p.isRunning.Load() {
		return false
	}

	// Test API endpoint
	resp, err := p.apiClient.Get(p.getAPIURL("/api/tags"))
	if err != nil {
		return false
	}
	// Drain then close so the keep-alive connection is returned to the pool for
	// reuse (an unread body forces net/http to abandon the connection, which
	// under concurrent health checks causes connection/goroutine churn).
	defer func() {
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
	}()

	return resp.StatusCode == http.StatusOK
}

// GetHealth returns provider health status. ModelCount reflects the
// discovered model list, so discovery is ensured first (speed programme
// P1-T02) — preserving the pre-change ModelCount semantics.
func (p *OllamaProvider) GetHealth(ctx context.Context) (*ProviderHealth, error) {
	p.ensureModelsDiscovered()

	if !p.isRunning.Load() {
		return &ProviderHealth{
			Status:     "unhealthy",
			LastCheck:  time.Now(),
			ErrorCount: 1,
		}, nil
	}

	// Test API endpoint
	start := time.Now()
	resp, err := p.apiClient.Get(p.getAPIURL("/api/tags"))
	latency := time.Since(start)

	// Drain + close the body when the request succeeded so the keep-alive
	// connection is returned to the pool. The previous code never closed the
	// body on EITHER path — a connection/file-descriptor leak that, under
	// repeated health checks, exhausted the pool and spawned new connections
	// (and their goroutines) indefinitely. resp is only non-nil when err == nil.
	if resp != nil {
		defer func() {
			_, _ = io.Copy(io.Discard, resp.Body)
			_ = resp.Body.Close()
		}()
	}

	if err != nil || resp.StatusCode != http.StatusOK {
		return &ProviderHealth{
			Status:     "degraded",
			Latency:    latency,
			LastCheck:  time.Now(),
			ErrorCount: 1,
			ModelCount: len(p.models),
		}, nil
	}

	return &ProviderHealth{
		Status:     "healthy",
		Latency:    latency,
		LastCheck:  time.Now(),
		ErrorCount: 0,
		ModelCount: len(p.models),
	}, nil
}

// Close stops the Ollama provider
func (p *OllamaProvider) Close() error {
	p.isRunning.Store(false)
	log.Println("✅ Ollama provider closed")
	return nil
}

// GetContextWindow returns the model's context window size in tokens.
// Default: 200_000 — local Ollama models vary; 200k is a safe upper bound
// pending a Phase 3 upgrade to query /api/show for the actual context length.
func (p *OllamaProvider) GetContextWindow() int {
	return 200_000
}

// CountTokens returns an estimated token count for text.
// Uses char-based fallback (1 token ≈ 3.5 chars) — Phase 3 will upgrade
// to query the Ollama tokenize endpoint when available.
func (p *OllamaProvider) CountTokens(text string) (int, error) {
	return CharBasedTokenCount(text)
}

// Private helper methods

func (p *OllamaProvider) discoverModels() error {
	resp, err := p.apiClient.Get(p.getAPIURL("/api/tags"))
	if err != nil {
		return fmt.Errorf("failed to fetch models: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("API returned status %d", resp.StatusCode)
	}

	var response struct {
		Models []OllamaModel `json:"models"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		return fmt.Errorf("failed to decode models response: %w", err)
	}

	p.models = response.Models
	return nil
}

func (p *OllamaProvider) getModelName(requestedModel string) string {
	if requestedModel != "" {
		return requestedModel
	}

	if p.config.DefaultModel != "" {
		return p.config.DefaultModel
	}

	// Fall back to the first discovered model — ensure discovery has run
	// (speed programme P1-T02) so this fallback path still works when no
	// model was explicitly requested and no DefaultModel is configured.
	p.ensureModelsDiscovered()
	if len(p.models) > 0 {
		return p.models[0].Name
	}

	return "llama2" // Fallback default
}

func (p *OllamaProvider) getAPIURL(path string) string {
	baseURL := p.config.BaseURL
	if baseURL == "" {
		baseURL = "http://localhost:11434"
	}

	return strings.TrimSuffix(baseURL, "/") + path
}

func (p *OllamaProvider) makeAPIRequest(ctx context.Context, request OllamaAPIRequest) (*OllamaAPIResponse, error) {
	url := p.getAPIURL("/api/chat")

	requestBody, err := json.Marshal(request)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	// speed programme P2-T02 / R1 B11: read the marshalled bytes directly via
	// bytes.NewReader — the prior strings.NewReader(string(requestBody))
	// round-tripped []byte→string→reader, allocating a redundant copy of the
	// whole request body. Wire bytes are byte-identical.
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(requestBody))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	resp, err := p.apiClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("API request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API returned status %d", resp.StatusCode)
	}

	var response OllamaAPIResponse
	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &response, nil
}

func (p *OllamaProvider) makeStreamingRequest(ctx context.Context, request OllamaAPIRequest, ch chan<- LLMResponse) error {
	// Ollama streaming wire-format: NDJSON (newline-delimited JSON). Each line
	// is a complete OllamaAPIResponse; the terminal frame carries Done=true.
	// We line-iterate via bufio.Scanner and forward each chunk as a separate
	// LLMResponse so consumers see incremental output (round-33 §11.4 fix —
	// previous body only decoded ONE frame, fabricating streaming for callers
	// while silently discarding subsequent chunks; CONST-035 / Article XI §11.9).
	url := p.getAPIURL("/api/chat")

	requestBody, err := json.Marshal(request)
	if err != nil {
		return fmt.Errorf("failed to marshal request: %w", err)
	}

	// speed programme P2-T02 / R1 B11: bytes.NewReader on the marshalled bytes
	// directly — no []byte→string→reader round-trip copy. Wire-identical.
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(requestBody))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/x-ndjson")

	resp, err := p.apiClient.Do(req)
	if err != nil {
		return fmt.Errorf("API request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("API returned status %d", resp.StatusCode)
	}

	scanner := bufio.NewScanner(resp.Body)
	// Ollama chunks can carry full prompt-eval echoes; raise the buffer cap.
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var chunk OllamaAPIResponse
		if err := json.Unmarshal(line, &chunk); err != nil {
			return fmt.Errorf("failed to decode NDJSON chunk: %w", err)
		}
		// ANTI-BLUFF: Send REAL streaming chunk from Ollama (one per NDJSON line).
		// Round-46 LLMResponse.Err wiring (CONST-035 / Article XI §11.9):
		// the terminal frame (chunk.Done==true) carries done_reason; map
		// it into Err so downstream stream consumers can distinguish a
		// clean stop from truncation. FinishReason is also preserved.
		out := LLMResponse{
			ID: uuid.New(),
			// /api/chat streams each token in message.content; completionText()
			// reads message.content (falling back to response for /api/generate)
			// so streamed chunks carry the REAL token text, not an empty string
			// (CONST-035 / Article XI §11.9).
			Content:      chunk.completionText(),
			CreatedAt:    time.Now(),
			FinishReason: chunk.DoneReason,
		}
		if chunk.Done {
			out.Err = mapOllamaDoneReasonToErr(chunk.DoneReason)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case ch <- out:
		}
		if chunk.Done {
			break
		}
	}
	if err := scanner.Err(); err != nil && err != io.EOF {
		return fmt.Errorf("streaming read failed: %w", err)
	}

	return nil
}
