package server

// wire_facade_rag_test.go — HXC-148: reproduce-first (§11.4.115) coverage
// proving RAG retrieval-augmentation is applied to the OpenAI/Anthropic
// wire-facade endpoints (/v1/chat/completions, /v1/messages) when enabled,
// and NOT applied when disabled (the default).
//
// Mirrors llm_rag_test.go's pattern but targets the compat endpoints.
// Uses the existing wireFacadeFakeProvider (gotReq captures the prompt).

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"dev.helix.code/internal/rag"
	"digital.vasic.rag/pkg/retriever"

	"github.com/stretchr/testify/require"
)

// lastPrompt extracts the prompt content from the fake provider's captured request.
func lastPrompt(fake *wireFacadeFakeProvider) string {
	if fake.gotReq != nil && len(fake.gotReq.Messages) > 0 {
		return fake.gotReq.Messages[len(fake.gotReq.Messages)-1].Content
	}
	return ""
}

// TestChatCompletions_RAG_DisabledByDefault_PromptByteIdentical proves
// that with RAG disabled (default), the OpenAI compat endpoint passes
// the user's prompt through unchanged.
func TestChatCompletions_RAG_DisabledByDefault_PromptByteIdentical(t *testing.T) {
	unsetEnvForTest(t, "HELIXCODE_RAG_ENABLED")

	fake := &wireFacadeFakeProvider{content: "ok", finish: "stop"}
	withFakeResolver(t, fake)

	ret := &fakeRAGRetriever{docs: []retriever.Document{{Content: "SHOULD NOT APPEAR"}}}
	adapter := rag.NewAdapter(ret)
	adapter.SetEnabled(false)
	withRAGAdapter(t, adapter)

	srv := newTestServerForWireFacade(t)
	w := httptest.NewRecorder()
	body := `{"model":"test-model","messages":[{"role":"user","content":"original prompt"}]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+wireFacadeRoutesTestAPIKey)
	srv.router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code, "body=%s", w.Body.String())
	require.Empty(t, ret.queries, "RAG retriever must NOT be called when disabled")
	require.Contains(t, lastPrompt(fake), "original prompt",
		"prompt must be unchanged when RAG disabled")
	require.NotContains(t, lastPrompt(fake), "SHOULD NOT APPEAR",
		"RAG doc must NOT appear when disabled")
}

// TestChatCompletions_RAG_Enabled_AugmentsPrompt proves that with RAG
// enabled, the OpenAI compat endpoint augments the prompt with retrieved
// context before sending it to the provider.
func TestChatCompletions_RAG_Enabled_AugmentsPrompt(t *testing.T) {
	// t.Setenv restores the PRIOR state (value or absence); the previous
	// os.Setenv + t.Cleanup(os.Unsetenv) pair restored to ABSENCE
	// unconditionally, clobbering any pre-existing value.
	t.Setenv("HELIXCODE_RAG_ENABLED", "true")

	fake := &wireFacadeFakeProvider{content: "ok", finish: "stop"}
	withFakeResolver(t, fake)

	ret := &fakeRAGRetriever{docs: []retriever.Document{
		{Content: "HXC-148 RETRIEVED CONTEXT"},
	}}
	adapter := rag.NewAdapter(ret)
	adapter.SetEnabled(true)
	withRAGAdapter(t, adapter)

	srv := newTestServerForWireFacade(t)
	w := httptest.NewRecorder()
	body := `{"model":"test-model","messages":[{"role":"user","content":"original prompt"}]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+wireFacadeRoutesTestAPIKey)
	srv.router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code, "body=%s", w.Body.String())
	require.NotEmpty(t, ret.queries, "RAG retriever must be called when enabled")
	require.Contains(t, lastPrompt(fake), "HXC-148 RETRIEVED CONTEXT",
		"RAG-retrieved context must appear in the prompt sent to the provider")
}

// TestAnthropicMessages_RAG_DisabledByDefault_PromptByteIdentical proves
// the Anthropic compat endpoint also respects the default-OFF RAG posture.
func TestAnthropicMessages_RAG_DisabledByDefault_PromptByteIdentical(t *testing.T) {
	unsetEnvForTest(t, "HELIXCODE_RAG_ENABLED")

	fake := &wireFacadeFakeProvider{content: "ok", finish: "stop"}
	withFakeResolver(t, fake)

	ret := &fakeRAGRetriever{docs: []retriever.Document{{Content: "SHOULD NOT APPEAR"}}}
	adapter := rag.NewAdapter(ret)
	adapter.SetEnabled(false)
	withRAGAdapter(t, adapter)

	srv := newTestServerForWireFacade(t)
	w := httptest.NewRecorder()
	body := `{"model":"test-model","max_tokens":128,"messages":[{"role":"user","content":"original prompt"}]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+wireFacadeRoutesTestAPIKey)
	srv.router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code, "body=%s", w.Body.String())
	require.Empty(t, ret.queries, "RAG retriever must NOT be called when disabled")
	require.Contains(t, lastPrompt(fake), "original prompt")
	require.NotContains(t, lastPrompt(fake), "SHOULD NOT APPEAR")
}

// TestAnthropicMessages_RAG_Enabled_AugmentsPrompt proves the Anthropic
// compat endpoint applies RAG when enabled.
func TestAnthropicMessages_RAG_Enabled_AugmentsPrompt(t *testing.T) {
	// t.Setenv restores the PRIOR state (value or absence); the previous
	// os.Setenv + t.Cleanup(os.Unsetenv) pair restored to ABSENCE
	// unconditionally, clobbering any pre-existing value.
	t.Setenv("HELIXCODE_RAG_ENABLED", "true")

	fake := &wireFacadeFakeProvider{content: "ok", finish: "stop"}
	withFakeResolver(t, fake)

	ret := &fakeRAGRetriever{docs: []retriever.Document{
		{Content: "HXC-148 ANTHROPIC RETRIEVED CONTEXT"},
	}}
	adapter := rag.NewAdapter(ret)
	adapter.SetEnabled(true)
	withRAGAdapter(t, adapter)

	srv := newTestServerForWireFacade(t)
	w := httptest.NewRecorder()
	body := `{"model":"test-model","max_tokens":128,"messages":[{"role":"user","content":"original prompt"}]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+wireFacadeRoutesTestAPIKey)
	srv.router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code, "body=%s", w.Body.String())
	require.NotEmpty(t, ret.queries, "RAG retriever must be called when enabled")
	require.Contains(t, lastPrompt(fake), "HXC-148 ANTHROPIC RETRIEVED CONTEXT",
		"RAG-retrieved context must appear in the prompt sent to the Anthropic provider")
}

// unsetEnvForTest removes key from the process environment for the duration of
// t and restores exactly what was there before — the prior VALUE if it was set,
// or absence if it was not. There is no t.Unsetenv, so this is the
// save/restore counterpart of t.Setenv; a bare os.Unsetenv leaks to every
// later test in the binary and makes verdicts depend on -shuffle ordering.
func unsetEnvForTest(t *testing.T, key string) {
	t.Helper()
	prev, had := os.LookupEnv(key)
	if err := os.Unsetenv(key); err != nil {
		t.Fatalf("os.Unsetenv(%q) failed: %v", key, err)
	}
	t.Cleanup(func() {
		if had {
			_ = os.Setenv(key, prev)
		} else {
			_ = os.Unsetenv(key)
		}
	})
}
