package server

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"
)

// llm_generate_llamacpp_live_test.go — LIVE round-trip proof (§11.4.5 /
// §11.4.69 / §11.4.107) that a REAL completion flows through
// resolveLLMProvider's `llamacpp` route to an ACTUAL local llama.cpp
// `llama-server`, not merely to the httptest stub that
// llm_generate_llamacpp_local_test.go uses for the wire contract.
//
// Written to the same shape as the sibling
// llm_generate_helixllm_live_test.go: READ-ONLY against the server (a single
// real Generate() call — no config change, no writes, §11.4.122), no build
// tag (a local loopback call carries zero API cost), and an honest SKIP
// (never a fake PASS — CONST-035 / §11.4.3) when no local llama-server is
// reachable, so it is safe in the default `go test ./internal/server/...`
// invocation on any host.
//
// Anti-bluff: the prompt carries a per-run nonce generated microseconds
// before the call, so a cached, mocked, replayed or hardcoded response cannot
// possibly contain it (§11.4.2 / §11.4.5) — the same technique
// internal/llm/provider_live_proof_test.go's providerLiveNonce uses.
//
// Run:
//
//	cd helix_code && go test -v -run TestGenerateLLM_LlamaCppLocal_LiveRoundTrip ./internal/server/...
func llamaCppLocalReachable(t *testing.T) bool {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, envLlamaCppHost()+"/v1/models", nil)
	if err != nil {
		return false
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return false
	}
	defer func() { _ = resp.Body.Close() }()
	return resp.StatusCode == http.StatusOK
}

// TestGenerateLLM_LlamaCppLocal_LiveRoundTrip drives the REAL generateLLM
// HTTP handler — the full end-user surface, POST /api/v1/llm/generate with
// `"provider":"llamacpp"` — against a REAL local llama-server, and asserts the
// JSON response body carries a genuine completion echoing the run's nonce.
//
// This is the strongest available evidence class for this change: it exercises
// the handler, the provider-resolution route, the endpoint configuration, and
// a real GGUF-backed generation in one pass.
func TestGenerateLLM_LlamaCppLocal_LiveRoundTrip(t *testing.T) {
	if !llamaCppLocalReachable(t) {
		t.Skip("SKIP: no local llama.cpp server reachable at " + envLlamaCppHost() +
			" (set " + llamaCppHostEnv + " or start llama-server to exercise this proof)")
	}

	nonceBuf := make([]byte, 6)
	if _, err := rand.Read(nonceBuf); err != nil {
		t.Fatalf("nonce generation failed: %v", err)
	}
	nonce := "HELIXCODE-LLAMACPP-ROUTE-" + hex.EncodeToString(nonceBuf)

	prompt := fmt.Sprintf(
		"This is an automated liveness probe for the HelixCode llamacpp route. "+
			"Reply with EXACTLY this token and nothing else: %s", nonce)

	// Marshal through encoding/json so the nonce cannot break the JSON body.
	bodyBytes, err := json.Marshal(map[string]any{
		"provider":    "llamacpp",
		"prompt":      prompt,
		"max_tokens":  64,
		"temperature": 0,
	})
	if err != nil {
		t.Fatalf("request marshal failed: %v", err)
	}

	srv := &Server{}
	w, decoded := postJSON(t, "/api/v1/llm/generate", srv.generateLLM, string(bodyBytes))

	if w.Code != http.StatusOK {
		t.Fatalf("POST /api/v1/llm/generate with provider=llamacpp returned HTTP %d against a live "+
			"llama-server at %s; body=%v", w.Code, envLlamaCppHost(), decoded)
	}
	if got, _ := decoded["status"].(string); got != "success" {
		t.Fatalf(`expected status "success", got %q; body=%v`, got, decoded)
	}
	if got, _ := decoded["provider"].(string); got != "llamacpp" {
		t.Fatalf("the response must name the provider the caller actually chose; got %q", got)
	}

	content, _ := decoded["content"].(string)
	if strings.TrimSpace(content) == "" {
		t.Fatalf("live llama-server returned empty content — no real completion produced; body=%v", decoded)
	}
	if !strings.Contains(content, nonce) {
		t.Fatalf("response did not echo nonce %q (got %q) — cannot prove this is a live, non-cached answer",
			nonce, content)
	}

	t.Logf("PASS: POST /api/v1/llm/generate provider=llamacpp endpoint=%s -> REAL completion: "+
		"model=%v finish_reason=%v usage=%v content=%q",
		envLlamaCppHost(), decoded["model"], decoded["finish_reason"], decoded["usage"], content)
}
