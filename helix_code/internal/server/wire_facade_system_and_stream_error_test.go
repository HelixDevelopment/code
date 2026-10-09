package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"dev.helix.code/internal/llm"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// wire_facade_system_and_stream_error_test.go — regression guards for the two
// defects found by a LIVE experiment against real Claude Code v2.1.261 against
// the Anthropic-wire facade (`POST /v1/messages`).
//
// DEFECT 1 (blocking): `anthropicMessagesRequest.System` was declared `string`,
// so the array-of-blocks `system` shape that real Claude Code ALWAYS sends was
// rejected at the door:
//
//	API Error: 400 invalid request body: json: cannot unmarshal array into Go
//	struct field anthropicMessagesRequest.system of type string
//
// Measured: a string `system` returned 200; the byte-identical request with an
// array `system` returned 400. Everything else Claude Code sends (array-form
// message content with cache_control, thinking, ?beta=true, multi-turn
// tool_use/tool_result history) was already accepted.
//
// DEFECT 2 (CONST-035 / §11.4 anti-bluff): the streaming path DRAINED and
// DISCARDED the terminal error from provider.GenerateStream. Measured: a
// request that returns `502 ... API returned status 404` on the NON-streaming
// path returned `HTTP/1.1 200 OK` with a well-formed but EMPTY SSE event
// sequence when `stream:true` — a failure rendered to the client as a
// successful empty turn, which is a PASS-bluff at the wire layer.
//
// Every guard below is GREEN-by-default and names the exact single-edit
// mutation to wire_facade.go that makes it FAIL (§1.1), so the guard is
// provably falsifiable rather than decorative.

// ---------------------------------------------------------------------------
// Fakes (CONST-050(A) — fakes permitted only in unit-test sources)
// ---------------------------------------------------------------------------

// wireStreamErrProvider is a deterministic, network-free llm.Provider whose
// GenerateStream reproduces the two real provider-failure timings:
//
//   - preChunks empty  => the provider fails BEFORE producing anything, so the
//     handler has written no byte yet and the real HTTP status is still
//     reachable (the "provider returned 404" case the live experiment hit).
//   - preChunks non-empty => the provider emits real content and THEN fails,
//     so the SSE headers are already committed and the failure can only be
//     surfaced inside the stream.
//
// It honours the CHANNEL-OWNERSHIP CONTRACT in llm.Provider: it is the sole
// closer of ch, on every return path.
type wireStreamErrProvider struct {
	preChunks []string
	streamErr error

	// generateErr is what the NON-streaming Generate returns. The guards use it
	// to prove the two paths agree: the same provider failure that yields 502
	// without `stream:true` must not become a 200 with it.
	generateErr error
}

func (p *wireStreamErrProvider) GetType() llm.ProviderType              { return llm.ProviderTypeOllama }
func (p *wireStreamErrProvider) GetName() string                        { return "fake-wire-stream-err" }
func (p *wireStreamErrProvider) GetModels() []llm.ModelInfo             { return nil }
func (p *wireStreamErrProvider) GetCapabilities() []llm.ModelCapability { return nil }
func (p *wireStreamErrProvider) IsAvailable(ctx context.Context) bool   { return true }
func (p *wireStreamErrProvider) GetContextWindow() int                  { return 8192 }
func (p *wireStreamErrProvider) CountTokens(text string) (int, error)   { return len(text) / 4, nil }
func (p *wireStreamErrProvider) Close() error                           { return nil }

func (p *wireStreamErrProvider) GetHealth(ctx context.Context) (*llm.ProviderHealth, error) {
	return &llm.ProviderHealth{Status: "healthy", LastCheck: time.Now()}, nil
}

func (p *wireStreamErrProvider) Generate(ctx context.Context, req *llm.LLMRequest) (*llm.LLMResponse, error) {
	if p.generateErr != nil {
		return nil, p.generateErr
	}
	return &llm.LLMResponse{ID: uuid.New(), RequestID: req.ID, Content: "", CreatedAt: time.Now()}, nil
}

func (p *wireStreamErrProvider) GenerateStream(ctx context.Context, req *llm.LLMRequest, ch chan<- llm.LLMResponse) error {
	defer close(ch)
	for _, c := range p.preChunks {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case ch <- llm.LLMResponse{ID: uuid.New(), Content: c, CreatedAt: time.Now()}:
		}
	}
	return p.streamErr
}

// postSSE drives a handler through a bare gin engine and returns the recorder
// plus the raw body. Unlike postJSON it never assumes the body is JSON, which
// is the whole point: these guards must be able to tell an SSE body apart from
// a JSON error body.
func postSSE(t *testing.T, route string, h gin.HandlerFunc, body string) (*httptest.ResponseRecorder, string) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.POST(route, h)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, route, bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)
	return w, w.Body.String()
}

// ---------------------------------------------------------------------------
// DEFECT 1 — `system` accepts BOTH the string and the array-of-blocks shapes
// ---------------------------------------------------------------------------

// TestAnthropicSystem_PlainString_PreservedExactly is the no-regression half of
// the fix: the shape that ALREADY worked must keep working byte-for-byte,
// including through the real handler (a leading role:"system" internal
// message carrying the verbatim string).
//
// MUTATION THAT MAKES THIS FAIL (§1.1): in anthropicSystemPrompt.UnmarshalJSON,
// delete the `if trimmed[0] == '"'` plain-string branch — the string body then
// falls through to the block-array decode, fails, and the request 400s.
func TestAnthropicSystem_PlainString_PreservedExactly(t *testing.T) {
	const body = `{"model":"m","max_tokens":16,"system":"You are terse.","messages":[{"role":"user","content":"hi"}]}`

	var req anthropicMessagesRequest
	require.NoError(t, json.Unmarshal([]byte(body), &req),
		"the plain-string `system` shape must still parse")
	assert.Equal(t, "You are terse.", req.System.Text())

	llmReq, errStr := anthropicRequestToLLMRequest(req)
	require.Empty(t, errStr)
	require.Len(t, llmReq.Messages, 2, "system must be promoted to a leading internal message")
	assert.Equal(t, "system", llmReq.Messages[0].Role)
	assert.Equal(t, "You are terse.", llmReq.Messages[0].Content,
		"the string form must be carried through VERBATIM — no re-wrapping, no separator injection")

	// And through the real handler, so the guard covers the wire, not only the
	// translation function.
	fake := &wireFacadeFakeProvider{content: "ok", finish: "end_turn"}
	withFakeResolver(t, fake)
	srv := &Server{}
	w, decoded := postJSON(t, "/v1/messages", srv.anthropicMessages, body)
	require.Equal(t, http.StatusOK, w.Code, "body=%v", decoded)
	require.NotNil(t, fake.gotReq)
	assert.Equal(t, "You are terse.", fake.gotReq.Messages[0].Content)
}

// TestAnthropicSystem_BlockArray_ConcatenatedInOrder is the RED-reproducing
// guard for DEFECT 1: this is the exact shape real Claude Code sends (text
// blocks, one carrying a cache_control annotation), and against the pre-fix
// artifact the very first line here FAILED with
// `json: cannot unmarshal array into Go struct field
// anthropicMessagesRequest.system of type string`.
//
// MUTATION THAT MAKES THIS FAIL (§1.1): change the `System` field in
// anthropicMessagesRequest back to `string`. The json.Unmarshal below then
// returns the historical type error and the require.NoError fires. (A second,
// subtler mutation: reverse the append order in the concat loop — the
// in-order assertion catches it.)
func TestAnthropicSystem_BlockArray_ConcatenatedInOrder(t *testing.T) {
	// Byte-shape taken from a real Claude Code request.
	const body = `{"model":"m","max_tokens":16,"system":[` +
		`{"type":"text","text":"FIRST block."},` +
		`{"type":"text","text":"SECOND block.","cache_control":{"type":"ephemeral"}}` +
		`],"messages":[{"role":"user","content":"hi"}]}`

	var req anthropicMessagesRequest
	require.NoError(t, json.Unmarshal([]byte(body), &req),
		"the array-of-blocks `system` shape real Claude Code always sends MUST parse")

	assert.Equal(t, "FIRST block.\nSECOND block.", req.System.Text(),
		"text blocks must be concatenated IN ORDER, newline-separated")

	llmReq, errStr := anthropicRequestToLLMRequest(req)
	require.Empty(t, errStr)
	require.Len(t, llmReq.Messages, 2)
	assert.Equal(t, "system", llmReq.Messages[0].Role)
	assert.Equal(t, "FIRST block.\nSECOND block.", llmReq.Messages[0].Content)
	// Order is load-bearing: a reversed concat would still contain both
	// substrings, so assert the prefix explicitly.
	assert.True(t, strings.HasPrefix(llmReq.Messages[0].Content, "FIRST block."),
		"block order must be preserved; got %q", llmReq.Messages[0].Content)
}

// TestAnthropicSystem_UnknownBlockType_IsIgnoredNotRejected proves the
// forward-compatibility rule: Anthropic adds block types over time, and
// rejecting an unknown block would resurrect exactly the total-failure class
// DEFECT 1 was. The unknown block is skipped; the text blocks around it still
// concatenate in order.
//
// MUTATION THAT MAKES THIS FAIL (§1.1): in anthropicSystemPrompt.UnmarshalJSON,
// replace `continue` in the non-text branch with
// `return fmt.Errorf("unsupported block type %q", b.Type)` — json.Unmarshal
// then errors and the require.NoError fires.
func TestAnthropicSystem_UnknownBlockType_IsIgnoredNotRejected(t *testing.T) {
	// A future/unknown block ("thinking") plus a multi-modal-ish block whose
	// extra fields do NOT match the text schema — neither may break the decode.
	const body = `{"model":"m","max_tokens":16,"system":[` +
		`{"type":"text","text":"KEEP one."},` +
		`{"type":"thinking","thinking":"internal reasoning that is not a system prompt"},` +
		`{"type":"future_block","payload":{"nested":["a","b"]},"input":[1,2,3]},` +
		`{"type":"text","text":"KEEP two."}` +
		`],"messages":[{"role":"user","content":"hi"}]}`

	var req anthropicMessagesRequest
	require.NoError(t, json.Unmarshal([]byte(body), &req),
		"an unknown system block type must be IGNORED, never rejected")

	got := req.System.Text()
	assert.Equal(t, "KEEP one.\nKEEP two.", got,
		"text blocks must survive around an unknown block; got %q", got)
	assert.NotContains(t, got, "internal reasoning",
		"a non-text block must contribute nothing to the system prompt")

	// End-to-end through the real handler + real router (auth middleware
	// included), because the live defect was observed as an HTTP 400 on the
	// wire, not as a translation-unit failure.
	fake := &wireFacadeFakeProvider{content: "ok", finish: "end_turn"}
	withFakeResolver(t, fake)
	srv := newTestServerForWireFacade(t)

	w := httptest.NewRecorder()
	httpReq := httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewBufferString(body))
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("x-api-key", wireFacadeRoutesTestAPIKey)
	srv.router.ServeHTTP(w, httpReq)

	require.Equal(t, http.StatusOK, w.Code,
		"an array-form `system` must NOT 400 — this is the exact live Claude Code failure; body=%s", w.Body.String())
	require.NotNil(t, fake.gotReq)
	assert.Equal(t, "KEEP one.\nKEEP two.", fake.gotReq.Messages[0].Content)
}

// TestAnthropicSystem_Absent_LeavesBehaviourUnchanged pins the third shape: no
// `system` key at all must produce NO leading system message (the pre-fix
// behaviour, which must not drift into an empty system turn).
//
// MUTATION THAT MAKES THIS FAIL (§1.1): drop the `strings.TrimSpace(sys) != ""`
// guard in anthropicRequestToLLMRequest — an empty system message is then
// prepended and the message count becomes 2.
func TestAnthropicSystem_Absent_LeavesBehaviourUnchanged(t *testing.T) {
	for name, body := range map[string]string{
		"absent":       `{"model":"m","max_tokens":16,"messages":[{"role":"user","content":"hi"}]}`,
		"json_null":    `{"model":"m","max_tokens":16,"system":null,"messages":[{"role":"user","content":"hi"}]}`,
		"empty_string": `{"model":"m","max_tokens":16,"system":"","messages":[{"role":"user","content":"hi"}]}`,
		"empty_array":  `{"model":"m","max_tokens":16,"system":[],"messages":[{"role":"user","content":"hi"}]}`,
	} {
		t.Run(name, func(t *testing.T) {
			var req anthropicMessagesRequest
			require.NoError(t, json.Unmarshal([]byte(body), &req))
			assert.Equal(t, "", req.System.Text())

			llmReq, errStr := anthropicRequestToLLMRequest(req)
			require.Empty(t, errStr)
			require.Len(t, llmReq.Messages, 1,
				"no system prompt must mean NO leading system message")
			assert.Equal(t, "user", llmReq.Messages[0].Role)
		})
	}
}

// ---------------------------------------------------------------------------
// DEFECT 2 — the streaming path must never render a provider failure as an
// empty HTTP 200
// ---------------------------------------------------------------------------

// TestAnthropicMessages_Streaming_ProviderErrorBeforeAnyBytes_IsNotEmpty200 is
// the primary anti-bluff guard for DEFECT 2 on the wire Claude Code speaks.
// The provider fails without producing a single chunk — exactly the measured
// live case (`API returned status 404`). Nothing has been flushed, so the
// genuine error status is still reachable and MUST be used.
//
// It also pins the cross-path agreement the defect broke: the SAME provider
// failure is asserted to yield 502 on the NON-streaming path, so `stream:true`
// can no longer silently upgrade a failure to a success.
//
// MUTATION THAT MAKES THIS FAIL (§1.1): in streamAnthropicMessages, replace
// `if streamErr := <-errCh; streamErr != nil { failStream(streamErr); return }`
// with the pre-fix `<-errCh` discard. The handler then emits the well-formed
// empty event sequence with HTTP 200 and every assertion below fires.
func TestAnthropicMessages_Streaming_ProviderErrorBeforeAnyBytes_IsNotEmpty200(t *testing.T) {
	providerFailure := errors.New("upstream provider unreachable: API returned status 404")
	fake := &wireStreamErrProvider{streamErr: providerFailure, generateErr: providerFailure}
	withFakeResolver(t, fake)

	srv := &Server{}
	const body = `{"model":"m","max_tokens":16,"stream":true,"messages":[{"role":"user","content":"hi"}]}`
	w, raw := postSSE(t, "/v1/messages", srv.anthropicMessages, body)

	require.NotEqual(t, http.StatusOK, w.Code,
		"a provider failure with NO bytes written must NOT be reported as success; body=%s", raw)
	assert.Equal(t, http.StatusBadGateway, w.Code,
		"it must carry the same 502 the non-streaming path returns; body=%s", raw)

	assert.NotContains(t, w.Header().Get("Content-Type"), "text/event-stream",
		"nothing was streamed, so the response must not claim to be an SSE stream")

	// The failure must be legible to the client.
	assert.Contains(t, raw, "404",
		"the REAL provider error text must reach the client; body=%s", raw)

	// And it must not look like a completed turn.
	assert.NotContains(t, raw, "message_stop",
		"a failed turn must not emit the success terminator; body=%s", raw)
	assert.NotContains(t, raw, "content_block_stop", "body=%s", raw)

	var decoded map[string]interface{}
	require.NoError(t, json.Unmarshal([]byte(raw), &decoded),
		"with nothing streamed the body must be the ordinary JSON error object; body=%s", raw)
	assert.Equal(t, "error", decoded["type"])

	// Cross-path agreement: the identical failure on the non-streaming path.
	wNoStream, decodedNoStream := postJSON(t, "/v1/messages", srv.anthropicMessages,
		`{"model":"m","max_tokens":16,"messages":[{"role":"user","content":"hi"}]}`)
	require.Equal(t, http.StatusBadGateway, wNoStream.Code, "body=%v", decodedNoStream)
	assert.Equal(t, wNoStream.Code, w.Code,
		"stream:true must not change a 502 into a 200 for the same provider failure")
}

// TestAnthropicMessages_Streaming_ProviderErrorAfterStreamBegan_EmitsErrorEvent
// covers the other half of DEFECT 2: once real content has been flushed the
// HTTP status is committed (gin writes the header on the first Write), so the
// failure can only be surfaced INSIDE the stream. Anthropic's streaming
// protocol defines an `error` event for exactly this, and the client must be
// able to tell it apart from a short-but-successful turn.
//
// MUTATION THAT MAKES THIS FAIL (§1.1): in streamAnthropicMessages, delete the
// `emit("error", ...)` call in failStream's already-written branch (leaving a
// bare `return`). The stream then truncates silently and both the
// `event: error` and the "no success terminator" assertions fire.
func TestAnthropicMessages_Streaming_ProviderErrorAfterStreamBegan_EmitsErrorEvent(t *testing.T) {
	fake := &wireStreamErrProvider{
		preChunks: []string{"partial answer"},
		streamErr: errors.New("upstream provider died mid-stream: API returned status 500"),
	}
	withFakeResolver(t, fake)

	srv := &Server{}
	w, raw := postSSE(t, "/v1/messages", srv.anthropicMessages,
		`{"model":"m","max_tokens":16,"stream":true,"messages":[{"role":"user","content":"hi"}]}`)

	// Honest boundary: the header is already committed, so 200 is correct here
	// — the failure has to live in the event stream.
	require.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, raw, "message_start", "the stream genuinely began; body=%s", raw)
	assert.Contains(t, raw, "partial answer", "real content must still reach the client; body=%s", raw)

	assert.Contains(t, raw, "event: error",
		"a mid-stream provider failure MUST surface as an Anthropic `error` event, not a silent truncation; body=%s", raw)
	assert.Contains(t, raw, "500",
		"the REAL provider error text must reach the client; body=%s", raw)
	assert.NotContains(t, raw, "message_stop",
		"a failed stream must NOT emit the success terminator — that is what made it indistinguishable from a complete turn; body=%s", raw)
}

// TestChatCompletions_Streaming_ProviderErrorBeforeAnyBytes_IsNotEmpty200 is
// the OpenAI-wire mirror of the before-any-bytes guard. The same discard bug
// lived in streamOpenAIChatCompletion, one function away, so fixing only the
// Anthropic path would have left the identical bluff on the sibling surface.
//
// MUTATION THAT MAKES THIS FAIL (§1.1): in streamOpenAIChatCompletion, restore
// the pre-fix `<-errCh` discard in the channel-closed branch. The handler then
// emits `data: {...finish_reason:"stop"...}` + `data: [DONE]` with HTTP 200.
func TestChatCompletions_Streaming_ProviderErrorBeforeAnyBytes_IsNotEmpty200(t *testing.T) {
	providerFailure := errors.New("upstream provider unreachable: API returned status 404")
	fake := &wireStreamErrProvider{streamErr: providerFailure, generateErr: providerFailure}
	withFakeResolver(t, fake)

	srv := &Server{}
	w, raw := postSSE(t, "/v1/chat/completions", srv.chatCompletions,
		`{"model":"m","stream":true,"messages":[{"role":"user","content":"hi"}]}`)

	require.Equal(t, http.StatusBadGateway, w.Code,
		"a provider failure with NO bytes written must be a real error status, not an empty 200; body=%s", raw)
	assert.NotContains(t, raw, "data: [DONE]",
		"a failed turn must not emit the success terminator; body=%s", raw)
	assert.NotContains(t, raw, "chat.completion.chunk", "body=%s", raw)
	assert.Contains(t, raw, "404", "the REAL provider error text must reach the client; body=%s", raw)
}

// TestChatCompletions_Streaming_ProviderErrorAfterStreamBegan_EmitsErrorFrame
// is the OpenAI-wire mirror of the after-stream-began guard. OpenAI's SSE
// protocol carries a mid-stream failure as a `data:` frame whose payload is an
// `error` object.
//
// MUTATION THAT MAKES THIS FAIL (§1.1): in streamOpenAIChatCompletion, delete
// the error-frame json.Marshal/Fprintf inside failStream's already-written
// branch — the stream then ends with a bare `[DONE]` and the `"error"`
// assertion fires.
func TestChatCompletions_Streaming_ProviderErrorAfterStreamBegan_EmitsErrorFrame(t *testing.T) {
	fake := &wireStreamErrProvider{
		preChunks: []string{"partial answer"},
		streamErr: errors.New("upstream provider died mid-stream: API returned status 500"),
	}
	withFakeResolver(t, fake)

	srv := &Server{}
	w, raw := postSSE(t, "/v1/chat/completions", srv.chatCompletions,
		`{"model":"m","stream":true,"messages":[{"role":"user","content":"hi"}]}`)

	require.Equal(t, http.StatusOK, w.Code, "the header is already committed once a frame is flushed")
	assert.Contains(t, raw, "partial answer", "real content must still reach the client; body=%s", raw)
	assert.Contains(t, raw, `"error"`,
		"a mid-stream provider failure MUST surface as an OpenAI SSE error frame; body=%s", raw)
	assert.Contains(t, raw, "500", "the REAL provider error text must reach the client; body=%s", raw)
	assert.NotContains(t, raw, `"finish_reason":"stop"`,
		"a failed stream must NOT claim a clean stop; body=%s", raw)
}

// TestWireFacade_Streaming_SuccessPathUnchanged is the no-regression companion
// to the two error guards: with a healthy provider BOTH wires must still emit
// their full, ordinary success sequences. Without this, the error handling
// could have been "fixed" by breaking the happy path.
//
// MUTATION THAT MAKES THIS FAIL (§1.1): in streamAnthropicMessages, drop the
// `ensurePreamble()` call in the channel-closed branch — a zero-chunk success
// then emits content_block_stop with no preceding message_start.
func TestWireFacade_Streaming_SuccessPathUnchanged(t *testing.T) {
	t.Run("anthropic_with_content", func(t *testing.T) {
		withFakeResolver(t, &wireStreamErrProvider{preChunks: []string{"hi"}})
		srv := &Server{}
		w, raw := postSSE(t, "/v1/messages", srv.anthropicMessages,
			`{"model":"m","max_tokens":16,"stream":true,"messages":[{"role":"user","content":"hi"}]}`)
		require.Equal(t, http.StatusOK, w.Code)
		for _, want := range []string{"message_start", "content_block_start", "content_block_delta", "content_block_stop", "message_delta", "message_stop"} {
			assert.Contains(t, raw, want, "body=%s", raw)
		}
		assert.NotContains(t, raw, "event: error", "a healthy stream must not carry an error event; body=%s", raw)
	})

	t.Run("anthropic_zero_chunks_still_well_formed", func(t *testing.T) {
		withFakeResolver(t, &wireStreamErrProvider{})
		srv := &Server{}
		w, raw := postSSE(t, "/v1/messages", srv.anthropicMessages,
			`{"model":"m","max_tokens":16,"stream":true,"messages":[{"role":"user","content":"hi"}]}`)
		require.Equal(t, http.StatusOK, w.Code,
			"a genuinely EMPTY-but-successful turn stays a 200 — only a provider ERROR changes the outcome")
		for _, want := range []string{"message_start", "content_block_start", "content_block_stop", "message_stop"} {
			assert.Contains(t, raw, want, "body=%s", raw)
		}
	})

	t.Run("openai_with_content", func(t *testing.T) {
		withFakeResolver(t, &wireStreamErrProvider{preChunks: []string{"hi"}})
		srv := &Server{}
		w, raw := postSSE(t, "/v1/chat/completions", srv.chatCompletions,
			`{"model":"m","stream":true,"messages":[{"role":"user","content":"hi"}]}`)
		require.Equal(t, http.StatusOK, w.Code)
		assert.Contains(t, raw, "chat.completion.chunk", "body=%s", raw)
		assert.Contains(t, raw, `"content":"hi"`, "body=%s", raw)
		assert.Contains(t, raw, "data: [DONE]", "body=%s", raw)
		assert.NotContains(t, raw, `"error"`, "a healthy stream must not carry an error frame; body=%s", raw)
	})
}
