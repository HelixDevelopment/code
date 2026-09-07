package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"dev.helix.code/internal/config"
	"dev.helix.code/internal/database"
	"dev.helix.code/internal/llm"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// wire_facade_test.go — RED-first (§11.4.115) coverage for the dual
// OpenAI-style + Anthropic-style wire facade added in front of HelixCode's
// existing LLM routing (Provider-Coverage Expansion Plan v2 §3 Phase D).
//
// Anti-bluff (CONST-035 / CONST-036 / §11.4.74): these handlers MUST NOT
// duplicate provider-routing logic. They translate a wire request into the
// EXISTING internal llm.LLMRequest, resolve a provider via the EXISTING
// package-level llmProviderResolver seam (the same one generateLLM/streamLLM
// use), call the EXISTING provider.Generate, and translate the response back.
// The fake provider below records exactly what the handler passed to
// Generate — the same "prove real routing was reused, not reimplemented"
// pattern as llm_default_model_regression_test.go's modelRecordingProvider.

// wireFacadeFakeProvider is a deterministic, network-free llm.Provider that
// records the *llm.LLMRequest it receives and returns a fixed, realistic
// LLMResponse (text + optionally a tool call). It lives ONLY in this
// *_test.go file (CONST-050(A) — mocks/fakes permitted only in unit tests).
type wireFacadeFakeProvider struct {
	gotReq    *llm.LLMRequest
	content   string
	toolCalls []llm.ToolCall
	finish    string
	usage     llm.Usage

	// servedModel is the concrete model this fake reports as having served the
	// request, mirroring a real backend that resolves an alias ("default") to a
	// concrete model and reports it back. Zero value means "provider reports no
	// model", which is the shape every provider produced before CONST-036/037
	// propagation existed — so leaving it unset keeps every pre-existing test's
	// expectations byte-identical.
	servedModel string
}

func (p *wireFacadeFakeProvider) GetType() llm.ProviderType              { return llm.ProviderTypeOllama }
func (p *wireFacadeFakeProvider) GetName() string                        { return "fake-wire-facade" }
func (p *wireFacadeFakeProvider) GetModels() []llm.ModelInfo             { return nil }
func (p *wireFacadeFakeProvider) GetCapabilities() []llm.ModelCapability { return nil }
func (p *wireFacadeFakeProvider) IsAvailable(ctx context.Context) bool   { return true }
func (p *wireFacadeFakeProvider) GetContextWindow() int                  { return 8192 }
func (p *wireFacadeFakeProvider) CountTokens(text string) (int, error)   { return len(text) / 4, nil }
func (p *wireFacadeFakeProvider) Close() error                           { return nil }

func (p *wireFacadeFakeProvider) GetHealth(ctx context.Context) (*llm.ProviderHealth, error) {
	return &llm.ProviderHealth{Status: "healthy", LastCheck: time.Now()}, nil
}

func (p *wireFacadeFakeProvider) Generate(ctx context.Context, req *llm.LLMRequest) (*llm.LLMResponse, error) {
	p.gotReq = req
	return &llm.LLMResponse{
		ID:           uuid.New(),
		RequestID:    req.ID,
		Content:      p.content,
		ToolCalls:    p.toolCalls,
		Usage:        p.usage,
		FinishReason: p.finish,
		Model:        p.servedModel,
		CreatedAt:    time.Now(),
	}, nil
}

func (p *wireFacadeFakeProvider) GenerateStream(ctx context.Context, req *llm.LLMRequest, ch chan<- llm.LLMResponse) error {
	p.gotReq = req
	defer close(ch)
	if p.content != "" {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case ch <- llm.LLMResponse{ID: uuid.New(), Content: p.content, Model: p.servedModel, CreatedAt: time.Now()}:
		}
	}
	return nil
}

// wireFacadeRoutesTestAPIKey is the fixed test-only key configured on the
// server built by newTestServerForWireFacade, so requests exercised through
// the REAL router (srv.router.ServeHTTP — as opposed to the direct-handler
// postJSON helper used by the shape/streaming tests below, which bypass
// middleware entirely) satisfy s.wireFacadeAuthMiddleware() (see
// wire_facade_auth_test.go for the dedicated auth-guard coverage; this file
// is about route WIRING + wire-shape translation, not auth).
const wireFacadeRoutesTestAPIKey = "test-only-route-registration-key-not-a-real-secret"

func newTestServerForWireFacade(t *testing.T) *Server {
	t.Helper()
	cfg := &config.Config{
		Server:  config.ServerConfig{Address: "localhost", Port: 0},
		Logging: config.LoggingConfig{Level: "error"},
		Auth:    config.AuthConfig{WireFacadeAPIKeys: wireFacadeRoutesTestAPIKey},
	}
	db := (*database.Database)(nil)
	srv := newTestServer(t, cfg, db, nil)
	require.NotNil(t, srv)
	return srv
}

// TestDualWireFacade_RoutesRegistered proves BOTH facade endpoints are wired
// on HelixCode's own server (the confirmed gap this task closes: a route
// grep on server.go previously returned zero hits for "chat/completions" and
// "/v1/messages"). A 404 here means the facade is NOT registered.
func TestDualWireFacade_RoutesRegistered(t *testing.T) {
	fake := &wireFacadeFakeProvider{content: "4", finish: "stop"}
	withFakeResolver(t, fake)

	srv := newTestServerForWireFacade(t)

	t.Run("openai_chat_completions", func(t *testing.T) {
		w := httptest.NewRecorder()
		body := `{"model":"test-model","messages":[{"role":"user","content":"What is 2+2?"}]}`
		req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+wireFacadeRoutesTestAPIKey)
		srv.router.ServeHTTP(w, req)

		require.NotEqual(t, http.StatusNotFound, w.Code,
			"POST /v1/chat/completions must be a registered route on HelixCode's own server (Phase D gap)")
		require.Equal(t, http.StatusOK, w.Code, "body=%s", w.Body.String())
	})

	t.Run("anthropic_messages", func(t *testing.T) {
		w := httptest.NewRecorder()
		body := `{"model":"test-model","max_tokens":128,"messages":[{"role":"user","content":"What is 2+2?"}]}`
		req := httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+wireFacadeRoutesTestAPIKey)
		srv.router.ServeHTTP(w, req)

		require.NotEqual(t, http.StatusNotFound, w.Code,
			"POST /v1/messages must be a registered route on HelixCode's own server (Phase D gap)")
		require.Equal(t, http.StatusOK, w.Code, "body=%s", w.Body.String())
	})
}

// TestChatCompletions_OpenAIShape drives the real chatCompletions handler
// (via a bare router, mirroring llm_generate_test.go's postJSON pattern) and
// asserts the response is genuinely OpenAI Chat-Completions shaped:
// choices[0].message.content + usage.{prompt,completion,total}_tokens.
func TestChatCompletions_OpenAIShape(t *testing.T) {
	fake := &wireFacadeFakeProvider{
		content: "The answer is 4.",
		finish:  "stop",
		usage:   llm.Usage{PromptTokens: 10, CompletionTokens: 5, TotalTokens: 15},
	}
	withFakeResolver(t, fake)

	srv := &Server{}
	w, body := postJSON(t, "/v1/chat/completions", srv.chatCompletions,
		`{"model":"test-model","messages":[{"role":"user","content":"What is 2+2?"}]}`)

	require.Equal(t, http.StatusOK, w.Code, "body=%v", body)
	require.NotNil(t, fake.gotReq, "handler must call the EXISTING provider.Generate via llmProviderResolver — no reimplemented routing")
	require.Len(t, fake.gotReq.Messages, 1)
	assert.Equal(t, "user", fake.gotReq.Messages[0].Role)
	assert.Equal(t, "What is 2+2?", fake.gotReq.Messages[0].Content)

	choices, ok := body["choices"].([]interface{})
	require.True(t, ok, "response must have an OpenAI-shaped choices[] array; body=%v", body)
	require.Len(t, choices, 1)
	choice0, _ := choices[0].(map[string]interface{})
	msg, _ := choice0["message"].(map[string]interface{})
	require.NotNil(t, msg)
	assert.Equal(t, "assistant", msg["role"])
	assert.Equal(t, "The answer is 4.", msg["content"])
	assert.Equal(t, "stop", choice0["finish_reason"])

	usage, ok := body["usage"].(map[string]interface{})
	require.True(t, ok, "response must have an OpenAI-shaped usage object; body=%v", body)
	assert.EqualValues(t, 10, usage["prompt_tokens"])
	assert.EqualValues(t, 5, usage["completion_tokens"])
	assert.EqualValues(t, 15, usage["total_tokens"])
}

// TestAnthropicMessages_AnthropicShape drives the real anthropicMessages
// handler and asserts the response is genuinely Anthropic Messages shaped:
// content[] array of blocks + usage.input_tokens/output_tokens + stop_reason.
func TestAnthropicMessages_AnthropicShape(t *testing.T) {
	fake := &wireFacadeFakeProvider{
		content: "The answer is 4.",
		finish:  "end_turn",
		usage:   llm.Usage{PromptTokens: 12, CompletionTokens: 6, TotalTokens: 18},
	}
	withFakeResolver(t, fake)

	srv := &Server{}
	w, body := postJSON(t, "/v1/messages", srv.anthropicMessages,
		`{"model":"test-model","max_tokens":256,"system":"You are terse.","messages":[{"role":"user","content":"What is 2+2?"}]}`)

	require.Equal(t, http.StatusOK, w.Code, "body=%v", body)
	require.NotNil(t, fake.gotReq, "handler must call the EXISTING provider.Generate via llmProviderResolver — no reimplemented routing")
	// system + the one user turn.
	require.Len(t, fake.gotReq.Messages, 2)
	assert.Equal(t, "system", fake.gotReq.Messages[0].Role)
	assert.Equal(t, "You are terse.", fake.gotReq.Messages[0].Content)
	assert.Equal(t, "user", fake.gotReq.Messages[1].Role)
	assert.Equal(t, "What is 2+2?", fake.gotReq.Messages[1].Content)

	assert.Equal(t, "message", body["type"])
	assert.Equal(t, "assistant", body["role"])

	content, ok := body["content"].([]interface{})
	require.True(t, ok, "response must have an Anthropic-shaped content[] array; body=%v", body)
	require.Len(t, content, 1)
	block0, _ := content[0].(map[string]interface{})
	assert.Equal(t, "text", block0["type"])
	assert.Equal(t, "The answer is 4.", block0["text"])
	assert.Equal(t, "end_turn", body["stop_reason"])

	usage, ok := body["usage"].(map[string]interface{})
	require.True(t, ok, "response must have an Anthropic-shaped usage object; body=%v", body)
	assert.EqualValues(t, 12, usage["input_tokens"])
	assert.EqualValues(t, 6, usage["output_tokens"])
}

// TestDualWireFacade_ShapeDivergence is the mandated divergence test: the
// SAME internal llm.LLMResponse (content + a tool call) MUST translate to two
// DIFFERENT wire bodies — different field names, different tool-call
// encodings (OpenAI's function.arguments is a JSON-ENCODED STRING; Anthropic's
// tool_use.input is a JSON OBJECT) — never an identical body.
func TestDualWireFacade_ShapeDivergence(t *testing.T) {
	resp := &llm.LLMResponse{
		ID:      uuid.New(),
		Content: "",
		ToolCalls: []llm.ToolCall{{
			ID:   "call_1",
			Type: "function",
			Function: llm.ToolCallFunc{
				Name:      "get_weather",
				Arguments: map[string]interface{}{"city": "Paris"},
			},
		}},
		FinishReason: "tool_calls",
		Usage:        llm.Usage{PromptTokens: 20, CompletionTokens: 8, TotalTokens: 28},
	}

	openaiResp := llmResponseToOpenAI(resp, "test-model")
	anthropicResp := llmResponseToAnthropic(resp, "test-model")

	openaiJSON, err := json.Marshal(openaiResp)
	require.NoError(t, err)
	anthropicJSON, err := json.Marshal(anthropicResp)
	require.NoError(t, err)

	require.NotEqual(t, string(openaiJSON), string(anthropicJSON),
		"OpenAI-shape and Anthropic-shape bodies for the SAME internal response must NOT be byte-identical")

	// Field-name divergence: OpenAI uses "choices"/"finish_reason"/
	// "prompt_tokens"; Anthropic uses "content"/"stop_reason"/"input_tokens".
	assert.Contains(t, string(openaiJSON), `"choices"`)
	assert.Contains(t, string(openaiJSON), `"finish_reason"`)
	assert.Contains(t, string(openaiJSON), `"prompt_tokens"`)
	assert.NotContains(t, string(openaiJSON), `"stop_reason"`)
	assert.NotContains(t, string(openaiJSON), `"input_tokens"`)

	assert.Contains(t, string(anthropicJSON), `"stop_reason"`)
	assert.Contains(t, string(anthropicJSON), `"input_tokens"`)
	assert.Contains(t, string(anthropicJSON), `"content"`)
	assert.NotContains(t, string(anthropicJSON), `"choices"`)
	assert.NotContains(t, string(anthropicJSON), `"prompt_tokens"`)

	// Tool-call encoding divergence (a REAL wire-shape difference, not just
	// field renaming): OpenAI's function.arguments is a JSON-encoded STRING;
	// Anthropic's tool_use.input is a JSON OBJECT.
	require.Len(t, openaiResp.Choices, 1)
	require.Len(t, openaiResp.Choices[0].Message.ToolCalls, 1)
	argsStr := openaiResp.Choices[0].Message.ToolCalls[0].Function.Arguments
	assert.IsType(t, "", argsStr, "OpenAI tool_call.function.arguments must be a JSON string")
	var decodedArgs map[string]interface{}
	require.NoError(t, json.Unmarshal([]byte(argsStr), &decodedArgs),
		"OpenAI arguments string must itself be valid JSON")
	assert.Equal(t, "Paris", decodedArgs["city"])

	require.Len(t, anthropicResp.Content, 1)
	assert.Equal(t, "tool_use", anthropicResp.Content[0].Type)
	assert.Equal(t, "Paris", anthropicResp.Content[0].Input["city"],
		"Anthropic tool_use.input must be a JSON object (map), not a JSON-encoded string")
}

// TestOpenAIRequestToLLMRequest_TableTest is a direct table test of the
// request-side translation function, both for plain-string content and the
// OpenAI multi-part content-array shape, plus a tool-calls round trip.
func TestOpenAIRequestToLLMRequest_TableTest(t *testing.T) {
	tests := []struct {
		name        string
		body        string
		wantErr     bool
		wantRole    string
		wantContent string
	}{
		{
			name:        "plain_string_content",
			body:        `{"model":"m","messages":[{"role":"user","content":"hello"}]}`,
			wantRole:    "user",
			wantContent: "hello",
		},
		{
			name:        "content_parts_array",
			body:        `{"model":"m","messages":[{"role":"user","content":[{"type":"text","text":"hello"},{"type":"text","text":"world"}]}]}`,
			wantRole:    "user",
			wantContent: "hello\nworld",
		},
		{
			name:    "empty_messages_rejected",
			body:    `{"model":"m","messages":[]}`,
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var req openAIChatCompletionRequest
			require.NoError(t, json.Unmarshal([]byte(tc.body), &req))
			llmReq, errStr := openAIRequestToLLMRequest(req)
			if tc.wantErr {
				require.NotEmpty(t, errStr)
				return
			}
			require.Empty(t, errStr)
			require.NotNil(t, llmReq)
			require.Len(t, llmReq.Messages, 1)
			assert.Equal(t, tc.wantRole, llmReq.Messages[0].Role)
			assert.Equal(t, tc.wantContent, llmReq.Messages[0].Content)
		})
	}

	// Tool-calls round trip: an assistant message carrying a tool_call whose
	// wire `arguments` is a JSON-ENCODED STRING must decode into the internal
	// map[string]interface{} shape.
	toolBody := `{"model":"m","messages":[
		{"role":"user","content":"weather in paris?"},
		{"role":"assistant","content":"","tool_calls":[{"id":"call_1","type":"function","function":{"name":"get_weather","arguments":"{\"city\":\"Paris\"}"}}]},
		{"role":"tool","tool_call_id":"call_1","content":"22C sunny"}
	]}`
	var req openAIChatCompletionRequest
	require.NoError(t, json.Unmarshal([]byte(toolBody), &req))
	llmReq, errStr := openAIRequestToLLMRequest(req)
	require.Empty(t, errStr)
	require.Len(t, llmReq.Messages, 3)
	require.Len(t, llmReq.Messages[1].ToolCalls, 1)
	assert.Equal(t, "get_weather", llmReq.Messages[1].ToolCalls[0].Function.Name)
	assert.Equal(t, "Paris", llmReq.Messages[1].ToolCalls[0].Function.Arguments["city"])
	assert.Equal(t, "call_1", llmReq.Messages[2].ToolCallID)
	assert.Equal(t, "tool", llmReq.Messages[2].Role)
}

// TestAnthropicRequestToLLMRequest_TableTest mirrors the OpenAI table test
// for the Anthropic wire shape: plain string content, content-block arrays,
// system field promotion, and a tool_use/tool_result round trip.
func TestAnthropicRequestToLLMRequest_TableTest(t *testing.T) {
	tests := []struct {
		name         string
		body         string
		wantErr      bool
		wantMessages []llm.Message
	}{
		{
			name:         "plain_string_content",
			body:         `{"model":"m","max_tokens":16,"messages":[{"role":"user","content":"hello"}]}`,
			wantMessages: []llm.Message{{Role: "user", Content: "hello"}},
		},
		{
			name:         "content_block_array",
			body:         `{"model":"m","max_tokens":16,"messages":[{"role":"user","content":[{"type":"text","text":"hello"}]}]}`,
			wantMessages: []llm.Message{{Role: "user", Content: "hello"}},
		},
		{
			name:    "empty_messages_rejected",
			body:    `{"model":"m","max_tokens":16,"messages":[]}`,
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var req anthropicMessagesRequest
			require.NoError(t, json.Unmarshal([]byte(tc.body), &req))
			llmReq, errStr := anthropicRequestToLLMRequest(req)
			if tc.wantErr {
				require.NotEmpty(t, errStr)
				return
			}
			require.Empty(t, errStr)
			require.NotNil(t, llmReq)
			require.Len(t, llmReq.Messages, len(tc.wantMessages))
			for i, want := range tc.wantMessages {
				assert.Equal(t, want.Role, llmReq.Messages[i].Role)
				assert.Equal(t, want.Content, llmReq.Messages[i].Content)
			}
		})
	}

	// System field promotion: Anthropic's top-level `system` string becomes a
	// leading role:"system" internal message (OpenAI puts it IN messages[];
	// Anthropic does not — this is exactly the fork point the plan flagged).
	sysBody := `{"model":"m","max_tokens":16,"system":"be terse","messages":[{"role":"user","content":"hi"}]}`
	var sysReq anthropicMessagesRequest
	require.NoError(t, json.Unmarshal([]byte(sysBody), &sysReq))
	llmReq, errStr := anthropicRequestToLLMRequest(sysReq)
	require.Empty(t, errStr)
	require.Len(t, llmReq.Messages, 2)
	assert.Equal(t, "system", llmReq.Messages[0].Role)
	assert.Equal(t, "be terse", llmReq.Messages[0].Content)

	// tool_use / tool_result round trip: Anthropic's `input` is a JSON OBJECT
	// on the wire (unlike OpenAI's JSON-STRING `arguments`).
	toolBody := `{"model":"m","max_tokens":16,"messages":[
		{"role":"user","content":"weather in paris?"},
		{"role":"assistant","content":[{"type":"tool_use","id":"call_1","name":"get_weather","input":{"city":"Paris"}}]},
		{"role":"user","content":[{"type":"tool_result","tool_use_id":"call_1","content":"22C sunny"}]}
	]}`
	var toolReq anthropicMessagesRequest
	require.NoError(t, json.Unmarshal([]byte(toolBody), &toolReq))
	llmReq2, errStr2 := anthropicRequestToLLMRequest(toolReq)
	require.Empty(t, errStr2)
	require.Len(t, llmReq2.Messages, 3)
	require.Len(t, llmReq2.Messages[1].ToolCalls, 1)
	assert.Equal(t, "get_weather", llmReq2.Messages[1].ToolCalls[0].Function.Name)
	assert.Equal(t, "Paris", llmReq2.Messages[1].ToolCalls[0].Function.Arguments["city"])
	assert.Equal(t, "call_1", llmReq2.Messages[2].ToolCallID)
	assert.Equal(t, "tool", llmReq2.Messages[2].Role)
	assert.Equal(t, "22C sunny", llmReq2.Messages[2].Content)
}

// TestOpenAIRequestToLLMRequest_ToolsPassthrough proves an OpenAI-wire
// `tools[]` array (whose JSON shape is IDENTICAL to the internal llm.Tool
// type — {"type":"function","function":{...}}) reaches the internal request
// unchanged, and an Anthropic-wire `tools[]` array (different key:
// `input_schema` instead of `parameters`, no `type`/`function` wrapper) is
// correctly translated to the SAME internal llm.Tool shape.
func TestToolsPassthroughBothShapes(t *testing.T) {
	openaiBody := `{"model":"m","messages":[{"role":"user","content":"hi"}],
		"tools":[{"type":"function","function":{"name":"get_weather","description":"gets weather","parameters":{"type":"object","properties":{"city":{"type":"string"}}}}}]}`
	var oreq openAIChatCompletionRequest
	require.NoError(t, json.Unmarshal([]byte(openaiBody), &oreq))
	ollmReq, errStr := openAIRequestToLLMRequest(oreq)
	require.Empty(t, errStr)
	require.Len(t, ollmReq.Tools, 1)
	assert.Equal(t, "function", ollmReq.Tools[0].Type)
	assert.Equal(t, "get_weather", ollmReq.Tools[0].Function.Name)

	anthropicBody := `{"model":"m","max_tokens":16,"messages":[{"role":"user","content":"hi"}],
		"tools":[{"name":"get_weather","description":"gets weather","input_schema":{"type":"object","properties":{"city":{"type":"string"}}}}]}`
	var areq anthropicMessagesRequest
	require.NoError(t, json.Unmarshal([]byte(anthropicBody), &areq))
	allmReq, aerrStr := anthropicRequestToLLMRequest(areq)
	require.Empty(t, aerrStr)
	require.Len(t, allmReq.Tools, 1)
	assert.Equal(t, "function", allmReq.Tools[0].Type)
	assert.Equal(t, "get_weather", allmReq.Tools[0].Function.Name)
	assert.Equal(t, "object", allmReq.Tools[0].Function.Parameters["type"])
}

// TestChatCompletions_RejectsEmptyMessages proves the OpenAI facade rejects
// an empty messages[] with a real 400 (client error), no fabricated success.
func TestChatCompletions_RejectsEmptyMessages(t *testing.T) {
	srv := &Server{}
	w, _ := postJSON(t, "/v1/chat/completions", srv.chatCompletions, `{"model":"m","messages":[]}`)
	require.Equal(t, http.StatusBadRequest, w.Code)
}

// TestAnthropicMessages_RejectsEmptyMessages mirrors the above for /v1/messages.
func TestAnthropicMessages_RejectsEmptyMessages(t *testing.T) {
	srv := &Server{}
	w, _ := postJSON(t, "/v1/messages", srv.anthropicMessages, `{"model":"m","max_tokens":16,"messages":[]}`)
	require.Equal(t, http.StatusBadRequest, w.Code)
}

// TestChatCompletions_Streaming proves the OpenAI facade's stream:true path
// emits real SSE chunks (not a single monolithic body) terminated by the
// standard `data: [DONE]` frame.
func TestChatCompletions_Streaming(t *testing.T) {
	fake := &wireFacadeFakeProvider{content: "hi"}
	withFakeResolver(t, fake)

	srv := &Server{}
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.POST("/v1/chat/completions", srv.chatCompletions)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions",
		bytes.NewBufferString(`{"model":"m","stream":true,"messages":[{"role":"user","content":"hi"}]}`))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	body := w.Body.String()
	assert.Contains(t, body, "chat.completion.chunk")
	assert.Contains(t, body, `"content":"hi"`)
	assert.Contains(t, body, "data: [DONE]")
}

// TestAnthropicMessages_Streaming proves the Anthropic facade's stream:true
// path emits the real Anthropic SSE event sequence (message_start ...
// content_block_delta ... message_stop), not a single monolithic body.
func TestAnthropicMessages_Streaming(t *testing.T) {
	fake := &wireFacadeFakeProvider{content: "hi"}
	withFakeResolver(t, fake)

	srv := &Server{}
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.POST("/v1/messages", srv.anthropicMessages)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/messages",
		bytes.NewBufferString(`{"model":"m","max_tokens":16,"stream":true,"messages":[{"role":"user","content":"hi"}]}`))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	body := w.Body.String()
	assert.Contains(t, body, "message_start")
	assert.Contains(t, body, "content_block_delta")
	assert.Contains(t, body, "message_stop")
}
