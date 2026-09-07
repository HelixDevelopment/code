package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"dev.helix.code/internal/llm"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// wire_facade.go — dual OpenAI-style + Anthropic-style wire facade in front
// of HelixCode's EXISTING LLM routing.
//
// Origin: Provider-Coverage Expansion Plan v2 §3 Phase D
// (docs/research/07.2026/06_providers_coverage/EXPANSION_PLAN_v2.md). A route
// grep on this package (`grep -rn "chat/completions\|/v1/messages"
// helix_code/internal/server/*.go`) previously returned zero hits: HelixCode's
// own server answered only its own custom /api/v1/llm/generate shape, never
// an OpenAI-compatible or Anthropic-compatible wire to external callers. That
// meant neither an OpenAI SDK client nor Claude Code itself (which speaks the
// Anthropic Messages wire) could point at a HelixCode server as a drop-in
// endpoint.
//
// This file adds EXACTLY TWO net-new HTTP surfaces:
//
//	POST /v1/chat/completions  — OpenAI Chat Completions wire shape
//	POST /v1/messages          — Anthropic Messages wire shape
//
// Anti-bluff (CONST-035 / CONST-036 / §11.4.74 extend-don't-reimplement):
// NEITHER handler reimplements provider routing. Both:
//  1. translate their wire request into the EXISTING internal
//     *llm.LLMRequest,
//  2. resolve a provider via the EXISTING package-level llmProviderResolver
//     seam (the identical seam generateLLM/streamLLM in llm_generate.go use —
//     HELIX_LLM_PROVIDER / default local Ollama, per resolveLLMProvider's
//     doc-comment),
//  3. call the EXISTING resolveDefaultModel + provider.Generate /
//     GenerateStream,
//  4. translate the REAL *llm.LLMResponse back into the caller's wire shape.
//
// There is no new routing table, no new provider construction path, and no
// fabricated response of any kind — every byte returned originates from the
// same provider.Generate/GenerateStream call the pre-existing
// /api/v1/llm/generate surface makes.
//
// Known, documented scope limits (honest, not silently omitted — §11.4.6):
//   - AUTH (RESOLVED): both routes are gated by server.go's
//     s.wireFacadeAuthMiddleware(), NOT the internal-user JWT
//     authMiddleware()/VerifyJWTWithDB. Genuine OpenAI clients send
//     `Authorization: Bearer sk-...` and genuine Anthropic clients send
//     `x-api-key: ...` — neither maps onto this server's session JWT, so
//     wireFacadeAuthMiddleware accepts either header shape and checks it
//     against the operator-configured cfg.Auth.WireFacadeAPIKeys list
//     (HELIX_WIRE_FACADE_API_KEYS). This closed a real security finding: an
//     independent security review + the dual-wire review both flagged that
//     these two routes previously had NO authentication at all while every
//     shipped config profile (including config/production-config.yaml)
//     binds server.address to 0.0.0.0 — an unauthenticated, real-provider,
//     token-consuming surface reachable on any interface. See
//     wireFacadeAuthMiddleware's doc-comment in server.go for the fail-closed
//     rationale and wire_facade_auth_test.go for the RED->GREEN regression
//     guard.
//   - Anthropic's `system` field is accepted in BOTH wire shapes: a plain
//     string AND the array-of-blocks form (used for cache_control
//     annotations) that real Claude Code clients always send. Text blocks
//     are concatenated in order; non-text blocks are ignored, never
//     rejected. See anthropicSystemPrompt.
//   - Multi-modal content parts (image_url, image, audio) are not translated;
//     only "text" parts are extracted from either wire's content-array shape.
//   - tool_result content blocks are translated when their `content` is a
//     plain string (the common case); a nested content-block array inside a
//     tool_result is not recursively translated.

// ---------------------------------------------------------------------------
// OpenAI Chat Completions wire types (request)
// ---------------------------------------------------------------------------

// openAIMessageContent accepts the two content shapes the OpenAI Chat
// Completions wire format permits for a message: a plain string, or an array
// of typed content parts (e.g. [{"type":"text","text":"..."}]). Only "text"
// parts are translated — see the file-level doc-comment's scope limits.
type openAIMessageContent struct {
	text string
}

func (c *openAIMessageContent) UnmarshalJSON(data []byte) error {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 || string(trimmed) == "null" {
		c.text = ""
		return nil
	}
	if trimmed[0] == '"' {
		var s string
		if err := json.Unmarshal(data, &s); err != nil {
			return fmt.Errorf("invalid message content string: %w", err)
		}
		c.text = s
		return nil
	}
	var parts []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if err := json.Unmarshal(data, &parts); err != nil {
		return fmt.Errorf("unsupported message content shape (must be a string or a content-part array): %w", err)
	}
	var sb strings.Builder
	for _, p := range parts {
		if p.Text == "" {
			continue
		}
		if sb.Len() > 0 {
			sb.WriteString("\n")
		}
		sb.WriteString(p.Text)
	}
	c.text = sb.String()
	return nil
}

// Text returns the flattened text extracted from the message content.
func (c openAIMessageContent) Text() string { return c.text }

// openAIWireToolCallIn is the request-side shape of an assistant turn's prior
// tool_calls entry. NOTE the wire arguments field is a JSON-ENCODED STRING —
// this is a genuine, load-bearing wire-shape divergence from Anthropic's
// tool_use.input (a JSON OBJECT), not merely a field-name difference.
type openAIWireToolCallIn struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

type openAIChatMessageRequest struct {
	Role       string                 `json:"role"`
	Content    openAIMessageContent   `json:"content"`
	Name       string                 `json:"name,omitempty"`
	ToolCallID string                 `json:"tool_call_id,omitempty"`
	ToolCalls  []openAIWireToolCallIn `json:"tool_calls,omitempty"`
}

// openAIChatCompletionRequest is the JSON body accepted by
// POST /v1/chat/completions. Tools reuses llm.Tool directly: the OpenAI wire
// tool shape ({"type":"function","function":{"name","description",
// "parameters"}}) is BYTE-IDENTICAL to the internal llm.Tool struct tags, so
// no translation function is needed for the request-side tools array.
type openAIChatCompletionRequest struct {
	Model       string                     `json:"model"`
	Messages    []openAIChatMessageRequest `json:"messages"`
	Stream      bool                       `json:"stream"`
	MaxTokens   int                        `json:"max_tokens"`
	Temperature float64                    `json:"temperature"`
	Tools       []llm.Tool                 `json:"tools"`
	ToolChoice  interface{}                `json:"tool_choice"`
}

// ---------------------------------------------------------------------------
// OpenAI Chat Completions wire types (response)
// ---------------------------------------------------------------------------

type openAIUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

type openAIWireFunctionOut struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type openAIWireToolCallOut struct {
	ID       string                `json:"id"`
	Type     string                `json:"type"`
	Function openAIWireFunctionOut `json:"function"`
}

type openAIChatMessageResponse struct {
	Role      string                  `json:"role"`
	Content   string                  `json:"content"`
	ToolCalls []openAIWireToolCallOut `json:"tool_calls,omitempty"`
}

type openAIChatChoice struct {
	Index        int                       `json:"index"`
	Message      openAIChatMessageResponse `json:"message"`
	FinishReason string                    `json:"finish_reason"`
}

type openAIChatCompletionResponse struct {
	ID      string             `json:"id"`
	Object  string             `json:"object"`
	Created int64              `json:"created"`
	Model   string             `json:"model"`
	Choices []openAIChatChoice `json:"choices"`
	Usage   openAIUsage        `json:"usage"`
}

// openAIChatDelta / openAIChatChunkChoice / openAIChatCompletionChunk are the
// streaming ("stream":true) SSE chunk shapes.
type openAIChatDelta struct {
	Role    string `json:"role,omitempty"`
	Content string `json:"content,omitempty"`
}

type openAIChatChunkChoice struct {
	Index        int             `json:"index"`
	Delta        openAIChatDelta `json:"delta"`
	FinishReason *string         `json:"finish_reason"`
}

type openAIChatCompletionChunk struct {
	ID      string                  `json:"id"`
	Object  string                  `json:"object"`
	Created int64                   `json:"created"`
	Model   string                  `json:"model"`
	Choices []openAIChatChunkChoice `json:"choices"`
}

// ---------------------------------------------------------------------------
// Anthropic Messages wire types (request)
// ---------------------------------------------------------------------------

// anthropicContentBlock is one element of an Anthropic message's content
// array. Only Type + the fields relevant to that type are populated by the
// wire on any given block.
type anthropicContentBlock struct {
	Type      string                 `json:"type"`
	Text      string                 `json:"text,omitempty"`
	ID        string                 `json:"id,omitempty"`          // tool_use
	Name      string                 `json:"name,omitempty"`        // tool_use
	Input     map[string]interface{} `json:"input,omitempty"`       // tool_use
	ToolUseID string                 `json:"tool_use_id,omitempty"` // tool_result
	Content   json.RawMessage        `json:"content,omitempty"`     // tool_result (string, common case)
}

// anthropicMessageContent accepts the two content shapes the Anthropic
// Messages wire format permits: a plain string (shorthand for a single text
// block), or an array of typed content blocks.
type anthropicMessageContent struct {
	blocks []anthropicContentBlock
}

func (c *anthropicMessageContent) UnmarshalJSON(data []byte) error {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 || string(trimmed) == "null" {
		return nil
	}
	if trimmed[0] == '"' {
		var s string
		if err := json.Unmarshal(data, &s); err != nil {
			return fmt.Errorf("invalid message content string: %w", err)
		}
		c.blocks = []anthropicContentBlock{{Type: "text", Text: s}}
		return nil
	}
	var blocks []anthropicContentBlock
	if err := json.Unmarshal(data, &blocks); err != nil {
		return fmt.Errorf("unsupported message content shape (must be a string or a content-block array): %w", err)
	}
	c.blocks = blocks
	return nil
}

type anthropicMessageWire struct {
	Role    string                  `json:"role"`
	Content anthropicMessageContent `json:"content"`
}

// anthropicToolWire is the Anthropic wire tool shape — NOTE the key is
// `input_schema` (not `parameters`) and there is no `type`/`function`
// wrapper, unlike the OpenAI wire (a genuine, documented wire-shape fork
// point per the Provider-Coverage Expansion Plan v2 §3 Phase D R4 list).
type anthropicToolWire struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description,omitempty"`
	InputSchema map[string]interface{} `json:"input_schema"`
}

// anthropicSystemPrompt accepts the two shapes the Anthropic Messages wire
// format permits for the TOP-LEVEL `system` field: a plain string, or an array
// of typed content blocks (`[{"type":"text","text":"..."}]`, typically carrying
// `cache_control` annotations). It mirrors openAIMessageContent above — the
// same string-or-array UnmarshalJSON pattern already used for message content.
//
// This is not a theoretical shape: real Claude Code clients ALWAYS send the
// array form, so a `System string` field made every request from that client
// fail at the door with `json: cannot unmarshal array into Go struct field
// anthropicMessagesRequest.system of type string` (HTTP 400), while the plain
// string form returned 200 — i.e. the facade was unusable by the one client
// the Anthropic wire exists to serve.
//
// Translation rules:
//   - Plain string: taken verbatim (existing behaviour preserved exactly).
//   - Block array: the `text` of every text block is concatenated IN ORDER,
//     separated by "\n" — the same separator openAIMessageContent uses for
//     multi-part content, so both wires flatten multi-part text identically.
//   - Non-text blocks are IGNORED, never an error. Anthropic adds block types
//     over time, and rejecting an unknown block would resurrect exactly this
//     class of total failure. A block with an empty/absent `type` is treated
//     as text (some clients omit it on a lone text block).
type anthropicSystemPrompt struct {
	text string
}

func (s *anthropicSystemPrompt) UnmarshalJSON(data []byte) error {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 || string(trimmed) == "null" {
		s.text = ""
		return nil
	}
	if trimmed[0] == '"' {
		var str string
		if err := json.Unmarshal(data, &str); err != nil {
			return fmt.Errorf("invalid system prompt string: %w", err)
		}
		s.text = str
		return nil
	}
	// Decode into a MINIMAL {type,text} shape rather than the full
	// anthropicContentBlock: an unknown future block whose extra fields happen
	// to collide with a typed field (e.g. an `input` that is an array rather
	// than an object) would otherwise fail the whole decode — the same
	// total-failure mode this type exists to remove.
	var blocks []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if err := json.Unmarshal(data, &blocks); err != nil {
		return fmt.Errorf("unsupported system prompt shape (must be a string or a content-block array): %w", err)
	}
	var sb strings.Builder
	for _, b := range blocks {
		if b.Type != "" && b.Type != "text" {
			continue
		}
		if b.Text == "" {
			continue
		}
		if sb.Len() > 0 {
			sb.WriteString("\n")
		}
		sb.WriteString(b.Text)
	}
	s.text = sb.String()
	return nil
}

// Text returns the flattened system-prompt text.
func (s anthropicSystemPrompt) Text() string { return s.text }

// anthropicMessagesRequest is the JSON body accepted by POST /v1/messages.
// System accepts BOTH the plain-string and the array-of-blocks wire shapes —
// see anthropicSystemPrompt.
type anthropicMessagesRequest struct {
	Model       string                 `json:"model"`
	System      anthropicSystemPrompt  `json:"system"`
	Messages    []anthropicMessageWire `json:"messages"`
	MaxTokens   int                    `json:"max_tokens"`
	Temperature float64                `json:"temperature,omitempty"`
	Tools       []anthropicToolWire    `json:"tools,omitempty"`
	Stream      bool                   `json:"stream,omitempty"`
}

// ---------------------------------------------------------------------------
// Anthropic Messages wire types (response)
// ---------------------------------------------------------------------------

type anthropicContentBlockOut struct {
	Type  string                 `json:"type"`
	Text  string                 `json:"text,omitempty"`
	ID    string                 `json:"id,omitempty"`
	Name  string                 `json:"name,omitempty"`
	Input map[string]interface{} `json:"input,omitempty"`
}

type anthropicUsage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}

type anthropicMessagesResponse struct {
	ID           string                     `json:"id"`
	Type         string                     `json:"type"`
	Role         string                     `json:"role"`
	Model        string                     `json:"model"`
	Content      []anthropicContentBlockOut `json:"content"`
	StopReason   string                     `json:"stop_reason"`
	StopSequence *string                    `json:"stop_sequence"`
	Usage        anthropicUsage             `json:"usage"`
}

// ---------------------------------------------------------------------------
// Request-side translation: wire shape -> the EXISTING internal LLMRequest.
// ---------------------------------------------------------------------------

// openAIRequestToLLMRequest converts an OpenAI Chat Completions wire request
// into the internal *llm.LLMRequest the EXISTING routing consumes. Returns a
// non-empty validation-error string (no llmReq) when the request is invalid.
func openAIRequestToLLMRequest(req openAIChatCompletionRequest) (*llm.LLMRequest, string) {
	if len(req.Messages) == 0 {
		return nil, "request must include a non-empty 'messages' array"
	}
	messages := make([]llm.Message, 0, len(req.Messages))
	for _, m := range req.Messages {
		msg := llm.Message{
			Role:       m.Role,
			Content:    m.Content.Text(),
			Name:       m.Name,
			ToolCallID: m.ToolCallID,
		}
		for _, tc := range m.ToolCalls {
			args := map[string]interface{}{}
			if strings.TrimSpace(tc.Function.Arguments) != "" {
				// Best-effort decode: an OpenAI-wire tool_call's arguments is a
				// JSON-ENCODED STRING. A malformed string degrades to an empty
				// map rather than rejecting the whole request (the assistant
				// turn is still valid history even if one tool call's
				// arguments could not be parsed).
				_ = json.Unmarshal([]byte(tc.Function.Arguments), &args)
			}
			msg.ToolCalls = append(msg.ToolCalls, llm.ToolCall{
				ID:   tc.ID,
				Type: "function",
				Function: llm.ToolCallFunc{
					Name:      tc.Function.Name,
					Arguments: args,
				},
			})
		}
		messages = append(messages, msg)
	}
	return &llm.LLMRequest{
		Model:       req.Model,
		Messages:    messages,
		MaxTokens:   req.MaxTokens,
		Temperature: req.Temperature,
		Stream:      req.Stream,
		Tools:       req.Tools,
		ToolChoice:  req.ToolChoice,
	}, ""
}

// anthropicRequestToLLMRequest converts an Anthropic Messages wire request
// into the internal *llm.LLMRequest the EXISTING routing consumes. Returns a
// non-empty validation-error string (no llmReq) when the request is invalid.
func anthropicRequestToLLMRequest(req anthropicMessagesRequest) (*llm.LLMRequest, string) {
	messages := make([]llm.Message, 0, len(req.Messages)+1)
	// Anthropic's `system` is a TOP-LEVEL field, unlike OpenAI's
	// role:"system" message — promote it to a leading internal message so
	// downstream provider routing sees it the same way regardless of which
	// wire shape the caller used.
	if sys := req.System.Text(); strings.TrimSpace(sys) != "" {
		messages = append(messages, llm.Message{Role: "system", Content: sys})
	}

	for _, m := range req.Messages {
		var textParts []string
		var toolCalls []llm.ToolCall
		for _, b := range m.Content.blocks {
			switch b.Type {
			case "text":
				if b.Text != "" {
					textParts = append(textParts, b.Text)
				}
			case "tool_use":
				toolCalls = append(toolCalls, llm.ToolCall{
					ID:   b.ID,
					Type: "function",
					Function: llm.ToolCallFunc{
						Name:      b.Name,
						Arguments: b.Input,
					},
				})
			case "tool_result":
				// Anthropic feeds a tool result back as a user-role content
				// block; the internal convention (mirroring the rest of this
				// package's OpenAI-style Message shape) is its own
				// role:"tool" message carrying ToolCallID. Content is decoded
				// as a plain string (the common case — see file-level scope
				// limits for the nested-block-array case).
				var resultText string
				if len(b.Content) > 0 {
					var s string
					if err := json.Unmarshal(b.Content, &s); err == nil {
						resultText = s
					} else {
						resultText = string(b.Content)
					}
				}
				messages = append(messages, llm.Message{
					Role:       "tool",
					Content:    resultText,
					ToolCallID: b.ToolUseID,
				})
			}
		}
		if len(textParts) > 0 || len(toolCalls) > 0 {
			messages = append(messages, llm.Message{
				Role:      m.Role,
				Content:   strings.Join(textParts, "\n"),
				ToolCalls: toolCalls,
			})
		}
	}

	if len(messages) == 0 {
		return nil, "request must include a non-empty 'messages' array"
	}

	tools := make([]llm.Tool, 0, len(req.Tools))
	for _, t := range req.Tools {
		tools = append(tools, llm.Tool{
			Type: "function",
			Function: llm.ToolFunction{
				Name:        t.Name,
				Description: t.Description,
				Parameters:  t.InputSchema,
			},
		})
	}

	return &llm.LLMRequest{
		Model:       req.Model,
		Messages:    messages,
		MaxTokens:   req.MaxTokens,
		Temperature: req.Temperature,
		Stream:      req.Stream,
		Tools:       tools,
	}, ""
}

// ---------------------------------------------------------------------------
// Response-side translation: the EXISTING internal LLMResponse -> wire shape.
// ---------------------------------------------------------------------------

// normalizeFinishReasonOpenAI best-effort-maps a finish/stop reason from
// WHATEVER underlying provider actually served the request (Ollama,
// Anthropic-native, OpenAI-compatible, etc. — heterogeneous vocabularies)
// into the OpenAI wire's closed vocabulary. This is a documented heuristic,
// not an authoritative per-provider mapping table.
func normalizeFinishReasonOpenAI(reason string, hasToolCalls bool) string {
	switch strings.ToLower(strings.TrimSpace(reason)) {
	case "", "stop", "end_turn":
		if hasToolCalls {
			return "tool_calls"
		}
		return "stop"
	case "length", "max_tokens":
		return "length"
	case "tool_use", "tool_calls", "function_call":
		return "tool_calls"
	case "content_filter", "safety", "refusal":
		return "content_filter"
	default:
		if hasToolCalls {
			return "tool_calls"
		}
		return "stop"
	}
}

// normalizeFinishReasonAnthropic is the Anthropic-wire mirror of
// normalizeFinishReasonOpenAI — same heuristic-mapping documentation applies.
func normalizeFinishReasonAnthropic(reason string, hasToolCalls bool) string {
	switch strings.ToLower(strings.TrimSpace(reason)) {
	case "", "stop", "end_turn":
		if hasToolCalls {
			return "tool_use"
		}
		return "end_turn"
	case "length", "max_tokens":
		return "max_tokens"
	case "tool_use", "tool_calls", "function_call":
		return "tool_use"
	case "content_filter", "safety", "refusal":
		return "refusal"
	default:
		if hasToolCalls {
			return "tool_use"
		}
		return "end_turn"
	}
}

// llmResponseToOpenAI converts the EXISTING internal *llm.LLMResponse
// (produced by the SAME provider.Generate call generateLLM already makes)
// into the OpenAI Chat Completions wire response shape.
func llmResponseToOpenAI(resp *llm.LLMResponse, model string) openAIChatCompletionResponse {
	msg := openAIChatMessageResponse{Role: "assistant", Content: resp.Content}
	for _, tc := range resp.ToolCalls {
		// OpenAI wire's function.arguments is a JSON-ENCODED STRING (the
		// genuine wire-shape divergence from Anthropic's input object).
		argsBytes, err := json.Marshal(tc.Function.Arguments)
		if err != nil {
			argsBytes = []byte("{}")
		}
		msg.ToolCalls = append(msg.ToolCalls, openAIWireToolCallOut{
			ID:   tc.ID,
			Type: "function",
			Function: openAIWireFunctionOut{
				Name:      tc.Function.Name,
				Arguments: string(argsBytes),
			},
		})
	}
	// Report the model the provider ACTUALLY served. The `model` parameter is
	// the caller's REQUESTED string, which is an alias ("default", "local", a
	// routing name) whenever the backend resolved it to a concrete model —
	// echoing it back tells the client a model identity that never served the
	// request (CONST-036 / CONST-037). Fall back to the requested model only
	// when the provider reported none, so the field is never empty.
	servedModel := model
	if resp.Model != "" {
		servedModel = resp.Model
	}
	return openAIChatCompletionResponse{
		ID:      "chatcmpl-" + uuid.New().String(),
		Object:  "chat.completion",
		Created: time.Now().Unix(),
		Model:   servedModel,
		Choices: []openAIChatChoice{{
			Index:        0,
			Message:      msg,
			FinishReason: normalizeFinishReasonOpenAI(resp.FinishReason, len(resp.ToolCalls) > 0),
		}},
		Usage: openAIUsage{
			PromptTokens:     resp.Usage.PromptTokens,
			CompletionTokens: resp.Usage.CompletionTokens,
			TotalTokens:      resp.Usage.TotalTokens,
		},
	}
}

// llmResponseToAnthropic converts the EXISTING internal *llm.LLMResponse into
// the Anthropic Messages wire response shape.
func llmResponseToAnthropic(resp *llm.LLMResponse, model string) anthropicMessagesResponse {
	var blocks []anthropicContentBlockOut
	if resp.Content != "" {
		blocks = append(blocks, anthropicContentBlockOut{Type: "text", Text: resp.Content})
	}
	for _, tc := range resp.ToolCalls {
		// Anthropic wire's tool_use.input is a JSON OBJECT (the genuine
		// wire-shape divergence from OpenAI's JSON-encoded-string arguments).
		blocks = append(blocks, anthropicContentBlockOut{
			Type:  "tool_use",
			ID:    tc.ID,
			Name:  tc.Function.Name,
			Input: tc.Function.Arguments,
		})
	}
	if len(blocks) == 0 {
		blocks = []anthropicContentBlockOut{{Type: "text", Text: ""}}
	}
	// Same model-identity rule as the OpenAI facade: report what the provider
	// ACTUALLY served, never the requested alias (CONST-036 / CONST-037). The
	// Anthropic surface is a second client-visible wire, so echoing here would
	// leave the identical defect one function away from the fixed one.
	servedModel := model
	if resp.Model != "" {
		servedModel = resp.Model
	}
	return anthropicMessagesResponse{
		ID:         "msg_" + uuid.New().String(),
		Type:       "message",
		Role:       "assistant",
		Model:      servedModel,
		Content:    blocks,
		StopReason: normalizeFinishReasonAnthropic(resp.FinishReason, len(resp.ToolCalls) > 0),
		Usage: anthropicUsage{
			InputTokens:  resp.Usage.PromptTokens,
			OutputTokens: resp.Usage.CompletionTokens,
		},
	}
}

// ---------------------------------------------------------------------------
// Handlers
// ---------------------------------------------------------------------------

// chatCompletions handles POST /v1/chat/completions (OpenAI Chat Completions
// wire shape). It reuses the EXISTING internal LLM routing — see file-level
// doc-comment.
// openAIErrorType and anthropicErrorType derive the wire `type` field from the
// HTTP status actually being sent.
//
// Both facades used to emit ONE constant type for every provider-resolution
// failure — "server_error" on the OpenAI shape, "api_error" on the Anthropic
// shape — regardless of status. That was already inconsistent with the 400s the
// same two handlers emit a few lines earlier ("invalid_request_error"), and it
// became actively wrong once providerResolveStatus started returning 403: a
// permission refusal arrived carrying a 5xx-flavoured type, so a client that
// branches on `type` (both SDKs do) read a policy refusal as a server outage
// and retried it.
//
// The two mappings differ ONLY in the 5xx name, because the two APIs genuinely
// differ there: OpenAI names its server-side class "server_error" and Anthropic
// names its "api_error". Preserving each keeps both facades faithful to the API
// they imitate, and keeps the 5xx body byte-identical to what shipped before —
// the only bodies this change alters are the 400 and the 403.
func openAIErrorType(status int) string {
	if status >= 500 {
		return "server_error"
	}
	return sharedWireErrorType(status)
}

func anthropicErrorType(status int) string {
	if status >= 500 {
		return "api_error"
	}
	return sharedWireErrorType(status)
}

// sharedWireErrorType maps the sub-500 statuses, whose type names OpenAI and
// Anthropic spell identically. Anything else 4xx falls back to
// "invalid_request_error", which is what both APIs use as the generic
// client-fault class.
func sharedWireErrorType(status int) string {
	switch status {
	case http.StatusUnauthorized:
		return "authentication_error"
	case http.StatusForbidden:
		return "permission_error"
	case http.StatusNotFound:
		return "not_found_error"
	case http.StatusTooManyRequests:
		return "rate_limit_error"
	default:
		return "invalid_request_error"
	}
}

func (s *Server) chatCompletions(c *gin.Context) {
	var req openAIChatCompletionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": gin.H{"message": fmt.Sprintf("invalid request body: %v", err), "type": "invalid_request_error"},
		})
		return
	}

	llmReq, convErr := openAIRequestToLLMRequest(req)
	if convErr != "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"message": convErr, "type": "invalid_request_error"}})
		return
	}

	// Provider resolution reuses the EXACT SAME seam generateLLM/streamLLM
	// use (llmProviderResolver / resolveDefaultModel), per HELIX_LLM_PROVIDER
	// / default-local-Ollama precedence documented in llm_generate.go. There
	// is no "provider" field on either wire shape (neither the OpenAI nor the
	// Anthropic API has one), so it is intentionally not threaded through.
	provider, err := llmProviderResolver("", llmReq.Model)
	if err != nil {
		status := providerResolveStatus(err)
		c.JSON(status, gin.H{"error": gin.H{"message": err.Error(), "type": openAIErrorType(status)}})
		return
	}
	defer func() { _ = provider.Close() }()
	llmReq.Model = resolveDefaultModel(provider, llmReq.Model)

	// HXC-148: apply RAG retrieval-augmentation (mirrors generateLLM in
	// llm_generate.go). Disabled by default (HELIXCODE_RAG_ENABLED unset/false);
	// graceful degrade on retrieval error — never fails the request.
	ctx, cancel := context.WithTimeout(c.Request.Context(), 120*time.Second)
	defer cancel()
	applyRAGContext(ctx, ragAdapterResolver(), llmReq)

	if req.Stream {
		s.streamOpenAIChatCompletion(c, provider, llmReq)
		return
	}

	resp, genErr := provider.Generate(ctx, llmReq)
	if genErr != nil {
		c.JSON(http.StatusBadGateway, gin.H{
			"error": gin.H{"message": fmt.Sprintf("generation failed: %v", genErr), "type": "server_error"},
		})
		return
	}

	c.JSON(http.StatusOK, llmResponseToOpenAI(resp, llmReq.Model))
}

// anthropicMessages handles POST /v1/messages (Anthropic Messages wire
// shape). It reuses the EXISTING internal LLM routing — see file-level
// doc-comment.
func (s *Server) anthropicMessages(c *gin.Context) {
	var req anthropicMessagesRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"type":  "error",
			"error": gin.H{"type": "invalid_request_error", "message": fmt.Sprintf("invalid request body: %v", err)},
		})
		return
	}

	llmReq, convErr := anthropicRequestToLLMRequest(req)
	if convErr != "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"type":  "error",
			"error": gin.H{"type": "invalid_request_error", "message": convErr},
		})
		return
	}

	provider, err := llmProviderResolver("", llmReq.Model)
	if err != nil {
		status := providerResolveStatus(err)
		c.JSON(status, gin.H{
			"type":  "error",
			"error": gin.H{"type": anthropicErrorType(status), "message": err.Error()},
		})
		return
	}
	defer func() { _ = provider.Close() }()
	llmReq.Model = resolveDefaultModel(provider, llmReq.Model)

	// HXC-148: apply RAG retrieval-augmentation (mirrors generateLLM in
	// llm_generate.go). Disabled by default; graceful degrade on error.
	ctx, cancel := context.WithTimeout(c.Request.Context(), 120*time.Second)
	defer cancel()
	applyRAGContext(ctx, ragAdapterResolver(), llmReq)

	if req.Stream {
		s.streamAnthropicMessages(c, provider, llmReq)
		return
	}

	resp, genErr := provider.Generate(ctx, llmReq)
	if genErr != nil {
		c.JSON(http.StatusBadGateway, gin.H{
			"type":  "error",
			"error": gin.H{"type": "api_error", "message": fmt.Sprintf("generation failed: %v", genErr)},
		})
		return
	}

	c.JSON(http.StatusOK, llmResponseToAnthropic(resp, llmReq.Model))
}

// ---------------------------------------------------------------------------
// Streaming (SSE) — both reuse the EXISTING provider.GenerateStream channel
// contract (see llm_generate.go's CHANNEL-OWNERSHIP CONTRACT doc-comment: the
// provider is the sender and sole closer of the channel).
// ---------------------------------------------------------------------------

// streamOpenAIChatCompletion re-frames the provider's real streamed chunks as
// OpenAI `chat.completion.chunk` SSE frames, terminated by `data: [DONE]`.
func (s *Server) streamOpenAIChatCompletion(c *gin.Context, provider llm.Provider, llmReq *llm.LLMRequest) {
	c.Writer.Header().Set("Content-Type", "text/event-stream")
	c.Writer.Header().Set("Cache-Control", "no-cache")
	c.Writer.Header().Set("Connection", "keep-alive")

	ctx, cancel := context.WithTimeout(c.Request.Context(), 120*time.Second)
	defer cancel()

	chunkChan := make(chan llm.LLMResponse, 100)
	errCh := make(chan error, 1)
	go func() { errCh <- provider.GenerateStream(ctx, llmReq, chunkChan) }()

	flusher, _ := c.Writer.(interface{ Flush() })
	id := "chatcmpl-" + uuid.New().String()
	created := time.Now().Unix()

	// Same model-identity rule as the non-stream path: report what the provider
	// ACTUALLY served, not the requested alias (CONST-036 / CONST-037). Seeded
	// with the request so early frames are never empty, then upgraded as soon
	// as a chunk reports the concrete model.
	servedModel := llmReq.Model

	// wroteAny tracks whether ANY byte of the SSE body has been flushed. gin
	// commits the HTTP status on the first Write, so once a frame is out a
	// provider error can only be surfaced INSIDE the stream; before that, the
	// genuine error status is still reachable.
	wroteAny := false

	writeChunk := func(delta openAIChatDelta, finishReason *string) {
		frame := openAIChatCompletionChunk{
			ID:      id,
			Object:  "chat.completion.chunk",
			Created: created,
			Model:   servedModel,
			Choices: []openAIChatChunkChoice{{Index: 0, Delta: delta, FinishReason: finishReason}},
		}
		b, err := json.Marshal(frame)
		if err != nil {
			return
		}
		fmt.Fprintf(c.Writer, "data: %s\n\n", b)
		wroteAny = true
		if flusher != nil {
			flusher.Flush()
		}
	}

	// failStream surfaces a REAL provider error. Previously the terminal error
	// from GenerateStream was drained and DISCARDED, so a provider failure
	// reached the client as a successful, empty turn — a CONST-035 / §11.4
	// PASS-bluff at the wire layer (a failure rendered as success).
	failStream := func(streamErr error) {
		msg := fmt.Sprintf("generation failed: %v", streamErr)
		if !wroteAny {
			// Nothing flushed yet, so the SSE headers set above have NOT been
			// committed and the genuine status is still available. Content-Type
			// must be overwritten explicitly: gin's writeContentType only fills
			// an EMPTY Content-Type, so c.JSON would otherwise keep
			// text/event-stream. Same body shape the non-stream path returns.
			c.Writer.Header().Del("Cache-Control")
			c.Writer.Header().Del("Connection")
			c.Writer.Header().Set("Content-Type", "application/json; charset=utf-8")
			c.JSON(http.StatusBadGateway, gin.H{
				"error": gin.H{"message": msg, "type": "server_error"},
			})
			return
		}
		// The stream has already begun and the status is committed. The OpenAI
		// SSE protocol carries a mid-stream failure as a `data:` frame whose
		// payload is an `error` object — that is what lets a client tell a
		// failure apart from a normal short turn.
		if b, err := json.Marshal(gin.H{
			"error": gin.H{"message": msg, "type": "server_error"},
		}); err == nil {
			fmt.Fprintf(c.Writer, "data: %s\n\n", b)
		}
		fmt.Fprint(c.Writer, "data: [DONE]\n\n")
		if flusher != nil {
			flusher.Flush()
		}
	}

	for {
		select {
		case <-c.Request.Context().Done():
			return
		case chunk, ok := <-chunkChan:
			if !ok {
				// Drain the sender's terminal error BEFORE emitting the terminal
				// frames: a non-nil error here means the provider produced
				// nothing usable and the turn FAILED.
				if streamErr := <-errCh; streamErr != nil {
					failStream(streamErr)
					return
				}
				stop := "stop"
				writeChunk(openAIChatDelta{}, &stop)
				fmt.Fprint(c.Writer, "data: [DONE]\n\n")
				if flusher != nil {
					flusher.Flush()
				}
				return
			}
			// Upgrade to the provider-reported model before emitting the frame,
			// so the very first content chunk already carries the real identity.
			if chunk.Model != "" {
				servedModel = chunk.Model
			}
			if chunk.Content != "" {
				writeChunk(openAIChatDelta{Content: chunk.Content}, nil)
			}
			if chunk.Err != nil {
				failStream(chunk.Err)
				return
			}
		}
	}
}

// anthropicSSEEvent writes one `event: <name>\ndata: <json>\n\n` frame.
func anthropicSSEEvent(c *gin.Context, flusher interface{ Flush() }, name string, payload interface{}) {
	b, err := json.Marshal(payload)
	if err != nil {
		return
	}
	fmt.Fprintf(c.Writer, "event: %s\ndata: %s\n\n", name, b)
	if flusher != nil {
		flusher.Flush()
	}
}

// streamAnthropicMessages re-frames the provider's real streamed chunks as
// the Anthropic Messages SSE event sequence: message_start,
// content_block_start, one-or-more content_block_delta, content_block_stop,
// message_delta (stop_reason + usage), message_stop.
func (s *Server) streamAnthropicMessages(c *gin.Context, provider llm.Provider, llmReq *llm.LLMRequest) {
	c.Writer.Header().Set("Content-Type", "text/event-stream")
	c.Writer.Header().Set("Cache-Control", "no-cache")
	c.Writer.Header().Set("Connection", "keep-alive")

	ctx, cancel := context.WithTimeout(c.Request.Context(), 120*time.Second)
	defer cancel()

	chunkChan := make(chan llm.LLMResponse, 100)
	errCh := make(chan error, 1)
	go func() { errCh <- provider.GenerateStream(ctx, llmReq, chunkChan) }()

	flusher, _ := c.Writer.(interface{ Flush() })
	msgID := "msg_" + uuid.New().String()

	// wroteAny tracks whether ANY byte of the SSE body has been flushed. It is
	// the load-bearing discriminator for honest failure reporting: gin commits
	// the HTTP status on the first Write, so once a frame is out a provider
	// error can only be surfaced INSIDE the stream — before that, the genuine
	// error status is still reachable.
	wroteAny := false
	emit := func(name string, payload interface{}) {
		anthropicSSEEvent(c, flusher, name, payload)
		wroteAny = true
	}

	// MODEL IDENTITY (CONST-036 / CONST-037) + honest-failure precondition.
	// The Anthropic wire requires `message_start` to be the FIRST event, but at
	// handler entry nothing has been served, so an EAGERLY emitted message_start
	// could only carry the caller's REQUESTED alias. The preamble is therefore
	// emitted LAZILY, at the first event that genuinely needs it: by then the
	// first chunk (when there is one) has reported the concrete model, so the
	// SERVED identity is available and reported.
	//
	// The lazy preamble is ALSO what makes honest error reporting possible: a
	// provider error arriving before any chunk now finds NOTHING written, so
	// failStream can still answer with the real HTTP status instead of the
	// former well-formed-but-EMPTY HTTP-200 event sequence (a CONST-035 / §11.4
	// PASS-bluff at the wire layer — a failure rendered as a successful empty
	// turn). The cost is that message_start is delayed behind first-token
	// latency; that is a deliberate, documented trade (§11.4.6) — reporting a
	// failure as success is strictly worse than a later first event.
	servedModel := llmReq.Model
	preambleSent := false
	ensurePreamble := func() {
		if preambleSent {
			return
		}
		preambleSent = true
		emit("message_start", gin.H{
			"type": "message_start",
			"message": gin.H{
				"id":      msgID,
				"type":    "message",
				"role":    "assistant",
				"model":   servedModel,
				"content": []anthropicContentBlockOut{},
				"usage":   anthropicUsage{},
			},
		})
		emit("content_block_start", gin.H{
			"type": "content_block_start", "index": 0,
			"content_block": gin.H{"type": "text", "text": ""},
		})
	}

	// failStream surfaces a REAL provider error. Previously the terminal error
	// from GenerateStream was drained and DISCARDED, so a provider failure (e.g.
	// the upstream answering 404) reached the client as a successful, empty
	// turn — the silent-empty-200 defect this replaces.
	failStream := func(streamErr error) {
		msg := fmt.Sprintf("generation failed: %v", streamErr)
		if !wroteAny {
			// Nothing flushed yet, so the SSE headers set above have NOT been
			// committed and the genuine status is still available. Content-Type
			// must be overwritten explicitly: gin's writeContentType only fills
			// an EMPTY Content-Type, so c.JSON would otherwise keep
			// text/event-stream. Same body shape the non-stream path returns.
			c.Writer.Header().Del("Cache-Control")
			c.Writer.Header().Del("Connection")
			c.Writer.Header().Set("Content-Type", "application/json; charset=utf-8")
			c.JSON(http.StatusBadGateway, gin.H{
				"type":  "error",
				"error": gin.H{"type": "api_error", "message": msg},
			})
			return
		}
		// The stream has already begun and the status is committed. Anthropic's
		// streaming protocol defines an `error` event for exactly this case, so
		// the client can tell a failure from a truncated-but-successful turn.
		emit("error", gin.H{
			"type":  "error",
			"error": gin.H{"type": "api_error", "message": msg},
		})
	}

	var lastFinish string
	var lastUsage llm.Usage
	for {
		select {
		case <-c.Request.Context().Done():
			return
		case chunk, ok := <-chunkChan:
			if !ok {
				// Drain the sender's terminal error BEFORE emitting terminal
				// frames: a non-nil error here means the provider produced
				// nothing usable and the turn FAILED.
				if streamErr := <-errCh; streamErr != nil {
					failStream(streamErr)
					return
				}
				ensurePreamble()
				emit("content_block_stop", gin.H{"type": "content_block_stop", "index": 0})
				emit("message_delta", gin.H{
					"type": "message_delta",
					"delta": gin.H{
						"stop_reason": normalizeFinishReasonAnthropic(lastFinish, false),
					},
					"usage": anthropicUsage{OutputTokens: lastUsage.CompletionTokens},
				})
				emit("message_stop", gin.H{"type": "message_stop"})
				return
			}
			// Upgrade to the provider-reported model BEFORE the preamble is sent,
			// so message_start already carries the real served identity.
			if chunk.Model != "" {
				servedModel = chunk.Model
			}
			if chunk.Content != "" {
				ensurePreamble()
				emit("content_block_delta", gin.H{
					"type": "content_block_delta", "index": 0,
					"delta": gin.H{"type": "text_delta", "text": chunk.Content},
				})
			}
			if chunk.FinishReason != "" {
				lastFinish = chunk.FinishReason
			}
			lastUsage = chunk.Usage
			if chunk.Err != nil {
				failStream(chunk.Err)
				return
			}
		}
	}
}
