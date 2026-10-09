package llm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// Missing types for command line interface

// ModelCapability represents the capabilities of an LLM model
type ModelCapability string

// Model capability constants
const (
	CapabilityTextGeneration ModelCapability = "text_generation"
	CapabilityCodeGeneration ModelCapability = "code_generation"
	CapabilityCodeAnalysis   ModelCapability = "code_analysis"
	CapabilityPlanning       ModelCapability = "planning"
	CapabilityDebugging      ModelCapability = "debugging"
	CapabilityRefactoring    ModelCapability = "refactoring"
	CapabilityTesting        ModelCapability = "testing"
	CapabilityVision         ModelCapability = "vision"
	CapabilityReasoning      ModelCapability = "reasoning"
	CapabilityAnalysis       ModelCapability = "analysis"
	CapabilityWriting        ModelCapability = "writing"
	CapabilityDocumentation  ModelCapability = "documentation"
)

// ProviderType represents the type of LLM provider
type ProviderType string

// Provider type constants
const (
	ProviderTypeOpenAI     ProviderType = "openai"
	ProviderTypeAnthropic  ProviderType = "anthropic"
	ProviderTypeGemini     ProviderType = "gemini"
	ProviderTypeVertexAI   ProviderType = "vertexai"
	ProviderTypeAzure      ProviderType = "azure"
	ProviderTypeBedrock    ProviderType = "bedrock"
	ProviderTypeGroq       ProviderType = "groq"
	ProviderTypeQwen       ProviderType = "qwen"
	ProviderTypeCopilot    ProviderType = "copilot"
	ProviderTypeOpenRouter ProviderType = "openrouter"
	ProviderTypeCerebras   ProviderType = "cerebras"
	// ProviderTypeReplicate is the async-prediction-API hosted provider
	// (https://replicate.com). Unlike ProviderTypeCerebras / HuggingFace /
	// Together — all already served by the data-driven
	// HostedOpenAICompatibleCatalogue() in openai_compatible_catalogue.go
	// because Replicate's wire protocol is NOT the OpenAI chat-completions
	// contract (it is a create-prediction-then-poll flow, see
	// internal/llm/replicate_provider.go), Replicate genuinely has no
	// catalogue substitute and is wired through its own constructor
	// (NewReplicateProvider) instead. §11.4.124 investigation, resolved.
	ProviderTypeReplicate   ProviderType = "replicate"
	ProviderTypeXAI         ProviderType = "xai"
	ProviderTypeOllama      ProviderType = "ollama"
	ProviderTypeLocal       ProviderType = "local"
	ProviderTypeLlamaCpp    ProviderType = "llamacpp"
	ProviderTypeVLLM        ProviderType = "vllm"
	ProviderTypeLocalAI     ProviderType = "localai"
	ProviderTypeFastChat    ProviderType = "fastchat"
	ProviderTypeTextGen     ProviderType = "textgen"
	ProviderTypeLMStudio    ProviderType = "lmstudio"
	ProviderTypeJan         ProviderType = "jan"
	ProviderTypeKoboldAI    ProviderType = "koboldai"
	ProviderTypeGPT4All     ProviderType = "gpt4all"
	ProviderTypeTabbyAPI    ProviderType = "tabbyapi"
	ProviderTypeMLX         ProviderType = "mlx"
	ProviderTypeMistralRS   ProviderType = "mistralrs"
	ProviderTypeMemGPT      ProviderType = "memgpt"
	ProviderTypeCrewAI      ProviderType = "crewai"
	ProviderTypeCharacterAI ProviderType = "characterai"
	ProviderTypeReplika     ProviderType = "replika"
	ProviderTypeAnima       ProviderType = "anima"
	ProviderTypeGemma       ProviderType = "gemma"
	ProviderTypeLlamaIndex  ProviderType = "llamaindex"
	ProviderTypeCohere      ProviderType = "cohere"
	ProviderTypeHuggingFace ProviderType = "huggingface"
	ProviderTypeMistral     ProviderType = "mistral"
	ProviderTypeDeepSeek    ProviderType = "deepseek"
	ProviderTypeXiaomi      ProviderType = "xiaomi"
	ProviderTypeClickHouse  ProviderType = "clickhouse"
	ProviderTypeSupabase    ProviderType = "supabase"
	ProviderTypeDeepLake    ProviderType = "deeplake"
	ProviderTypeChroma      ProviderType = "chroma"
	ProviderTypeAgnostic    ProviderType = "agnostic"
)

type ModelInfo struct {
	ID             string                 `json:"id"`
	Name           string                 `json:"name"`
	Provider       ProviderType           `json:"provider"`
	ContextSize    int                    `json:"context_size"`
	MaxTokens      int                    `json:"max_tokens"`
	Capabilities   []ModelCapability      `json:"capabilities"`
	SupportsTools  bool                   `json:"supports_tools"`
	SupportsVision bool                   `json:"supports_vision"`
	Description    string                 `json:"description"`
	Format         ModelFormat            `json:"format"`
	Size           int64                  `json:"size"`
	Metadata       map[string]interface{} `json:"metadata"`
}

type ProviderConfigEntry struct {
	Type       ProviderType           `json:"type"`
	Endpoint   string                 `json:"endpoint"`
	APIKey     string                 `json:"api_key"`
	Models     []string               `json:"models"`
	Enabled    bool                   `json:"enabled"`
	Parameters map[string]interface{} `json:"parameters"`
}

type FunctionDefinition struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	Parameters  map[string]interface{} `json:"parameters"`
}

type ProviderHealth struct {
	Status     string        `json:"status"`
	LastCheck  time.Time     `json:"last_check"`
	Latency    time.Duration `json:"latency"`
	ModelCount int           `json:"model_count"`
	ErrorCount int           `json:"error_count"`
	Message    string        `json:"message"`
}

type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
	Name    string `json:"name,omitempty"`
	// ToolCallID links a role:"tool" result message back to the assistant's
	// tool_calls[].id it answers. The OpenAI/Groq protocol REQUIRES it on
	// every tool-result message ("for 'role:tool' the 'tool_call_id' is
	// missing" otherwise). omitempty ⇒ plain chat/system/user messages
	// serialise byte-identically to the pre-tool-loop wire.
	ToolCallID string `json:"tool_call_id,omitempty"`
	// ToolCalls carries the assistant turn's requested tool calls so the next
	// Generate sees the model's own tool-call request in context. Required so
	// each fed-back role:"tool" message has a matching assistant tool_call.
	// omitempty ⇒ messages without tool calls serialise byte-identically.
	ToolCalls []ToolCall `json:"tool_calls,omitempty"`
}

type Tool struct {
	Type     string       `json:"type"`
	Function ToolFunction `json:"function"`
}

type ToolFunction struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	Parameters  map[string]interface{} `json:"parameters"`
}

type ToolCall struct {
	ID       string       `json:"id"`
	Type     string       `json:"type"`
	Function ToolCallFunc `json:"function"`
}

type ToolCallFunc struct {
	Name      string                 `json:"name"`
	Arguments map[string]interface{} `json:"arguments"`
}

type LLMRequest struct {
	ID               uuid.UUID              `json:"id"`
	Model            string                 `json:"model"`
	Messages         []Message              `json:"messages"`
	MaxTokens        int                    `json:"max_tokens"`
	Temperature      float64                `json:"temperature"`
	TopP             float64                `json:"top_p"`
	Stream           bool                   `json:"stream"`
	Tools            []Tool                 `json:"tools"`
	ToolChoice       interface{}            `json:"tool_choice"`
	Stop             []string               `json:"stop"`
	ThinkingBudget   int                    `json:"thinking_budget"`
	CacheConfig      *CacheConfig           `json:"cache_config"`
	Reasoning        *ReasoningConfig       `json:"reasoning"`
	ProviderMetadata map[string]interface{} `json:"provider_metadata"`
}

type LLMResponse struct {
	ID               uuid.UUID              `json:"id"`
	RequestID        uuid.UUID              `json:"request_id"`
	Content          string                 `json:"content"`
	ToolCalls        []ToolCall             `json:"tool_calls"`
	Usage            Usage                  `json:"usage"`
	FinishReason     string                 `json:"finish_reason"`
	ProcessingTime   time.Duration          `json:"processing_time"`
	CreatedAt        time.Time              `json:"created_at"`
	ProviderMetadata map[string]interface{} `json:"provider_metadata"`

	// Model is the model the PROVIDER actually served this response with, as
	// reported by the provider itself — NOT the model string the caller asked
	// for. The two differ whenever the caller uses an alias ("default",
	// "local", a routing name): the backend resolves the alias to a concrete
	// model and reports that concrete identity back.
	//
	// Without this field the wire facade could only echo the REQUEST's model,
	// so an OpenAI-wire client was told `"model":"default"` while a
	// Qwen3-Coder gguf actually served the request — a model identity that is
	// not the real one (CONST-036 / CONST-037: model metadata shown to users
	// is provider/verifier-sourced, never a placeholder or a request echo).
	//
	// Empty when the provider reports no model; callers MUST then fall back to
	// the requested model rather than emitting an empty identity.
	Model string `json:"model,omitempty"`

	// Err carries a partial-error or non-fatal warning surfaced by the LLM
	// provider (e.g., truncation, content-filter block, mid-stream parse
	// error). nil when the response succeeded cleanly. Round 46 added this
	// field — round 33 anchored the prior limitation in tool_provider.go
	// :201 / :251 (the streaming channel had no error-frame mechanism, so
	// callers could not distinguish "ok empty chunk" from "mid-stream
	// failure"). Round 46 closes that gap.
	//
	// IMPORTANT: even when Err != nil, Content may still hold a valid
	// partial output (e.g., the first N tokens before the response was
	// truncated by max-tokens). Callers SHOULD inspect Content AND Err
	// rather than discarding the whole response on a non-nil Err.
	//
	// JSON serialization: errors do not naturally JSON-marshal, so Err is
	// elided with `omitempty` on the standard struct tag. Custom marshal/
	// unmarshal logic in this package (MarshalJSON / UnmarshalJSON below)
	// serializes Err as a `{"error_message": "...", "error_type": "..."}`
	// envelope and reconstructs it on decode via errors.New(msg) with
	// sentinel-mapping for the well-known round-46 sentinels
	// (ErrResponseTruncated, ErrResponseContentBlocked).
	Err error `json:"-"`
}

// llmResponseJSON is the on-wire JSON shape of LLMResponse. It mirrors
// every public field but replaces the un-marshallable `error` interface
// with a serializable {error_message, error_type} envelope (per the
// round-46 LLMResponse.Err doc comment).
type llmResponseJSON struct {
	ID               uuid.UUID              `json:"id"`
	RequestID        uuid.UUID              `json:"request_id"`
	Content          string                 `json:"content"`
	ToolCalls        []ToolCall             `json:"tool_calls"`
	Usage            Usage                  `json:"usage"`
	FinishReason     string                 `json:"finish_reason"`
	ProcessingTime   time.Duration          `json:"processing_time"`
	CreatedAt        time.Time              `json:"created_at"`
	ProviderMetadata map[string]interface{} `json:"provider_metadata"`
	Model            string                 `json:"model,omitempty"`
	Err              *llmErrorEnvelope      `json:"err,omitempty"`
}

// llmErrorEnvelope is the JSON shape of LLMResponse.Err on the wire.
// `error_type` permits the unmarshal path to map back to the canonical
// round-46 sentinel (ErrResponseTruncated / ErrResponseContentBlocked) so
// `errors.Is(...)` comparisons survive a JSON round-trip; other values
// reconstruct as generic errors.New(error_message).
type llmErrorEnvelope struct {
	Message string `json:"error_message"`
	Type    string `json:"error_type"`
}

const (
	llmErrTypeResponseTruncated      = "ResponseTruncated"
	llmErrTypeResponseContentBlocked = "ResponseContentBlocked"
	llmErrTypeGeneric                = "Generic"
)

// MarshalJSON implements json.Marshaler for LLMResponse.
// Round-46 adds Err handling — the field is otherwise un-marshallable.
func (r LLMResponse) MarshalJSON() ([]byte, error) {
	envelope := llmResponseJSON{
		ID:               r.ID,
		RequestID:        r.RequestID,
		Content:          r.Content,
		ToolCalls:        r.ToolCalls,
		Usage:            r.Usage,
		FinishReason:     r.FinishReason,
		ProcessingTime:   r.ProcessingTime,
		CreatedAt:        r.CreatedAt,
		ProviderMetadata: r.ProviderMetadata,
		Model:            r.Model,
	}
	if r.Err != nil {
		envelope.Err = &llmErrorEnvelope{
			Message: r.Err.Error(),
			Type:    llmErrTypeForSentinel(r.Err),
		}
	}
	return json.Marshal(envelope)
}

// UnmarshalJSON implements json.Unmarshaler for LLMResponse.
// Round-46 reconstructs Err from the {error_message, error_type} envelope,
// mapping known type names back to the canonical sentinels.
func (r *LLMResponse) UnmarshalJSON(data []byte) error {
	var envelope llmResponseJSON
	if err := json.Unmarshal(data, &envelope); err != nil {
		return err
	}
	r.ID = envelope.ID
	r.RequestID = envelope.RequestID
	r.Content = envelope.Content
	r.ToolCalls = envelope.ToolCalls
	r.Usage = envelope.Usage
	r.FinishReason = envelope.FinishReason
	r.ProcessingTime = envelope.ProcessingTime
	r.CreatedAt = envelope.CreatedAt
	r.ProviderMetadata = envelope.ProviderMetadata
	r.Model = envelope.Model
	if envelope.Err != nil {
		r.Err = sentinelForLLMErrType(envelope.Err.Type, envelope.Err.Message)
	}
	return nil
}

// llmErrTypeForSentinel returns the wire-format `error_type` label for a
// known round-46 sentinel, or "Generic" otherwise. Used by MarshalJSON.
func llmErrTypeForSentinel(e error) string {
	switch {
	case errors.Is(e, ErrResponseTruncated):
		return llmErrTypeResponseTruncated
	case errors.Is(e, ErrResponseContentBlocked):
		return llmErrTypeResponseContentBlocked
	default:
		return llmErrTypeGeneric
	}
}

// sentinelForLLMErrType reconstructs a round-46 sentinel from its
// wire-format `error_type` label, falling back to errors.New(message)
// when the type is unknown or Generic. Used by UnmarshalJSON.
func sentinelForLLMErrType(typ, message string) error {
	switch typ {
	case llmErrTypeResponseTruncated:
		return ErrResponseTruncated
	case llmErrTypeResponseContentBlocked:
		return ErrResponseContentBlocked
	default:
		if message == "" {
			return nil
		}
		return errors.New(message)
	}
}

type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

// Common errors
var (
	ErrModelNotFound       = fmt.Errorf("model not found")
	ErrInvalidRequest      = fmt.Errorf("invalid request")
	ErrRateLimited         = fmt.Errorf("rate limited")
	ErrContextTooLong      = fmt.Errorf("context too long")
	ErrProviderUnavailable = fmt.Errorf("provider unavailable")

	// Round-46 LLMResponse.Err sentinels — see LLMResponse.Err doc comment.
	// Providers populate one of these when a response succeeds at the HTTP
	// layer but the provider has flagged a partial-success condition on
	// the payload (truncation, content-filter block, etc.). Callers compare
	// via errors.Is(resp.Err, ErrResponseTruncated) etc.

	// ErrResponseTruncated indicates the provider stopped generation due
	// to the max-tokens limit (OpenAI finish_reason="length",
	// Anthropic stop_reason="max_tokens"). Content holds the partial
	// output up to the limit; callers SHOULD treat it as usable but
	// known-incomplete.
	ErrResponseTruncated = errors.New("llm response: truncated due to max-tokens limit; Content contains partial output")

	// ErrResponseContentBlocked indicates the provider's content-safety
	// filter rejected (part of) the response (OpenAI
	// finish_reason="content_filter", Anthropic stop_reason values like
	// "safety" or "refusal"). Content may be empty or partial.
	ErrResponseContentBlocked = errors.New("llm response: blocked by content-safety filter; Content may be empty or partial")

	// ErrReplicatePredictionFailed is the round-54 sentinel for a Replicate
	// prediction-completion API response whose `status` field is `"failed"`.
	// The provider's raw `error` field is wrapped via fmt.Errorf with `%w` so
	// callers can both `errors.Is(err, ErrReplicatePredictionFailed)` AND
	// inspect the human-readable upstream message. Replicate does not expose
	// truncation or content-filter signals natively on its
	// prediction-completion envelope — those still degrade to nil Err unless
	// the underlying model writes them into the output payload. See
	// helix_code/internal/llm/providers/replicate/client.go for the wire.
	ErrReplicatePredictionFailed = errors.New("llm response: Replicate prediction status=failed")
)

// Provider interface
type Provider interface {
	GetType() ProviderType
	GetName() string
	GetModels() []ModelInfo
	GetCapabilities() []ModelCapability
	Generate(ctx context.Context, request *LLMRequest) (*LLMResponse, error)
	// GenerateStream streams the completion by sending each LLMResponse chunk on
	// ch as it is produced. CHANNEL-OWNERSHIP CONTRACT: the provider is the SENDER
	// and the SOLE closer of ch. The implementation MUST close ch exactly once, on
	// EVERY return path (success, error, and ctx-cancel) — `defer close(ch)` at
	// the top of the method is the canonical way to satisfy this. The CONSUMER
	// (e.g. server.streamLLM) MUST NOT close ch: a double-close panics
	// (`close of closed channel`) inside the producer goroutine, which gin's
	// Recovery middleware cannot catch — it crashes the whole process. The
	// guaranteed close is also what lets the consumer observe the channel-drain
	// and emit its terminal frame without waiting for the context deadline.
	GenerateStream(ctx context.Context, request *LLMRequest, ch chan<- LLMResponse) error
	IsAvailable(ctx context.Context) bool
	GetHealth(ctx context.Context) (*ProviderHealth, error)
	Close() error

	// GetContextWindow returns the maximum number of tokens the active model
	// can hold in a single context window. Used by the auto-compaction system
	// to compute the 80%-trigger threshold.
	GetContextWindow() int

	// CountTokens returns an estimated token count for the given text.
	// Implementations SHOULD use the provider's native tokenizer when available
	// (e.g., tiktoken for OpenAI, anthropic-tokenizer for Anthropic) and MUST
	// fall back to a conservative char-based estimate (1 token ≈ 4 chars) when
	// no native tokenizer is reachable. Returns 0 + nil for empty text.
	CountTokens(text string) (int, error)
}
