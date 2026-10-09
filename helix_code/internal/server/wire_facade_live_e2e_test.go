package server

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"dev.helix.code/internal/config"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// wire_facade_live_e2e_test.go — full-HTTP round-trip proof (§11.4.5 /
// §11.4.69 / §11.4.107 / §11.4.108) that an authenticated OpenAI/Anthropic-
// shaped request to HelixCode's OWN server flows through the REAL
// wireFacadeAuthMiddleware + the REAL chatCompletions/anthropicMessages
// handlers + the REAL resolveLLMProvider("helixllm") routing, all the way to
// the REAL local HelixLLM coder (llama.cpp OpenAI-compatible sidecar,
// default http://localhost:18434), and back — closing the full-HTTP gap the
// routing-to-coder task (commit a21ad7ca, llm_generate_helixllm_test.go /
// llm_generate_helixllm_live_test.go) deferred: that work proved the
// resolveLLMProvider("helixllm") SEAM reaches the coder directly; this file
// proves the SAME thing is reachable by an actual OpenAI/Anthropic client
// speaking real HTTP to this server's real routes.
//
// STAND-UP PATH: httptest.NewServer wrapping the REAL *Server built via
// server.New()-equivalent construction (&Server{config,router} +
// srv.setupRoutes(), the identical fixture wire_facade_auth_test.go already
// uses) — NOT the full daemon binary, which requires a live PostgreSQL +
// Redis this environment does not provision. This is not a shortcut: gin's
// router, wireFacadeAuthMiddleware, chatCompletions/anthropicMessages, and
// llmProviderResolver are ALL the real, unmodified production code; only the
// database/redis-dependent route groups (auth/users/tasks/etc., which the
// wire-facade routes do not depend on) are absent because db==nil. The HTTP
// transport itself is genuinely real: httptest.NewServer opens a real TCP
// listener and serves real net/http requests, and every request below is
// driven by the actual `curl` binary via os/exec — a real HTTP client
// process, not an in-process handler call.
//
// Anti-bluff (§11.4/§11.4.1): no handler-direct call bypassing the router or
// the auth middleware; every assertion below is against real curl output
// captured from a real TCP round trip to a real gin.Engine talking to the
// real, live coder process on :18434 (read-only against the coder — a single
// Generate() call per subtest, no config changes, no writes, §11.4.122). The
// whole test honestly SKIPs (never fake-PASSes) when the coder is
// unreachable, mirroring llm_generate_helixllm_live_test.go's established
// pattern in this same package.
//
// Run:
//
//	cd helix_code && go test -v -run TestWireFacade_FullHTTP_E2E_LiveRoundTrip ./internal/server/...

const wireFacadeE2ETestAPIKey = "test-only-fullhttp-e2e-wire-facade-key-not-a-real-secret"

// --- per-request time budget -------------------------------------------------
//
// wireFacadeE2ECurlBudgetEnv / wireFacadeE2ECurlBudgetDefault replace what was
// a hardcoded `--max-time 60` inside curlCapture. That literal was not merely
// "a bit tight" — on a CPU-only coder it is STRUCTURALLY unreachable, so the
// test could only ever fail, never pass:
//
//	Measured on this host 2026-09-03 (llama-server /v1/chat/completions
//	`timings`, model qwen2.5-coder-3b-instruct-q4_k_m, `--n-gpu-layers 0`):
//	    predicted_per_second =  0.43 tok/s
//	    prompt_per_second    =  2.56 tok/s
//	A 32-token completion therefore needs ~74 s of generation ALONE, before
//	prompt processing — i.e. the 60 s ceiling expired mid-generation on every
//	run. The request SHOULD be that slow: this is a 3B GGUF doing CPU-only
//	inference, not a stalled request, so the correct fix is a budget sized to
//	the deployed hardware, NOT a retry and NOT a smaller assertion.
//
// The budget is env-overridable so a GPU-backed coder can tighten it (a fast
// coder SHOULD fail fast rather than wait two minutes) and a slower host can
// widen it, without editing this file (§11.4.28 decoupling posture applied to
// a test fixture, matching toolCallingCoderEndpointEnv's contract).
const (
	wireFacadeE2ECurlBudgetEnv     = "HELIX_E2E_CURL_MAX_SECONDS"
	wireFacadeE2ECurlBudgetDefault = 180
)

// wireFacadeE2ECurlBudgetSeconds resolves the per-request curl budget: env
// override first, measured-and-documented default second.
func wireFacadeE2ECurlBudgetSeconds(t *testing.T) int {
	t.Helper()
	if v := strings.TrimSpace(os.Getenv(wireFacadeE2ECurlBudgetEnv)); v != "" {
		n, err := strconv.Atoi(v)
		require.NoErrorf(t, err, "%s must be an integer number of seconds, got %q",
			wireFacadeE2ECurlBudgetEnv, v)
		require.Positivef(t, n, "%s must be positive, got %d", wireFacadeE2ECurlBudgetEnv, n)
		return n
	}
	return wireFacadeE2ECurlBudgetDefault
}

// wireFacadeE2EServedModelID asks the LIVE coder which model it is actually
// serving, via GET /v1/models.
//
// This replaces a hardcoded `require.Contains(model, "qwen3-coder")`
// assertion. That literal was brittle in exactly the way §11.4.111 forbids:
// it bound the assertion to one model NAME rather than to the identity the
// endpoint reports, so it broke the moment the deployment changed model —
// measured on this host 2026-09-03 the coder serves
// "qwen2.5-coder-3b-instruct-q4_k_m", which does NOT contain "qwen3-coder",
// and the test failed for a reason that had nothing to do with the wire
// facade it exists to guard.
//
// Asserting response.model == the id /v1/models reports keeps the REAL
// invariant this subtest cares about — that the completion came from the coder
// the route resolved to, and that the handler propagates that id faithfully
// rather than echoing back the caller's string or inventing one — while
// surviving any model swap.
func wireFacadeE2EServedModelID(t *testing.T) string {
	t.Helper()

	req, err := http.NewRequest(http.MethodGet, envHelixLLMLocalEndpoint()+"/v1/models", nil)
	require.NoError(t, err)

	client := &http.Client{Timeout: time.Duration(wireFacadeE2ECurlBudgetSeconds(t)) * time.Second}
	resp, err := client.Do(req)
	require.NoErrorf(t, err, "GET /v1/models against the live coder failed")
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.Equalf(t, http.StatusOK, resp.StatusCode,
		"GET /v1/models must return 200, got %d body=%s", resp.StatusCode, string(body))

	var parsed struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	require.NoErrorf(t, json.Unmarshal(body, &parsed),
		"/v1/models must return OpenAI-shaped JSON: %s", string(body))
	require.NotEmptyf(t, parsed.Data, "/v1/models reported no models: %s", string(body))
	require.NotEmptyf(t, parsed.Data[0].ID, "/v1/models reported an empty model id: %s", string(body))

	return parsed.Data[0].ID
}

// wireFacadeE2ENonce returns a fresh per-run nonce token (same technique as
// llm_generate_helixllm_live_test.go's providerLiveNonce / nonceBuf): a
// cached/mocked/hardcoded response cannot possibly contain a token that did
// not exist until this call executed.
func wireFacadeE2ENonce(t *testing.T) string {
	t.Helper()
	buf := make([]byte, 6)
	_, err := rand.Read(buf)
	require.NoError(t, err, "nonce generation failed")
	return "HELIXCODE-FULLHTTP-E2E-" + hex.EncodeToString(buf)
}

// wireFacadeE2EEvidenceEnv opts a run in to promoting its captured transcript
// into the committed docs/qa tree.
const wireFacadeE2EEvidenceEnv = "HELIX_QA_EVIDENCE"

// wireFacadeE2EEvidenceDir returns the directory this run writes its
// end-to-end transcript into (§11.4.83).
//
// DETERMINISM (§11.4.50): this used to MkdirAll a fresh timestamped
// docs/qa/phase1_fullhttp_e2e_<TS>/ on EVERY invocation, unconditionally, so
// the repository state after a run depended on how many times the test had
// ever been run. Measured on this tree that had accumulated 161 directories
// and 74 MB since July, nearly all of them identical passing transcripts. A
// test whose side effects are a function of its invocation count is not
// deterministic, and it makes `git status` a poor signal for everyone else.
//
// The transcript is now always captured, but into a RUN-SCOPED staging dir,
// and promoted into the committed tree only when the evidence is actually
// worth something:
//
//   - the test FAILED — this is exactly when the transcript is diagnostic, so
//     promotion is automatic and needs no flag; or
//   - the operator asked for it with HELIX_QA_EVIDENCE=1 — the release/QA
//     capture path (§11.4.83 obligation is that the shipped feature HAS a
//     committed transcript, not that every invocation mints another copy).
//
// A passing default run therefore leaves the working tree byte-identical,
// while a failing run still lands its full bidirectional transcript on disk.
func wireFacadeE2EEvidenceDir(t *testing.T) string {
	t.Helper()
	staging := t.TempDir()
	promoteRequested := os.Getenv(wireFacadeE2EEvidenceEnv) == "1"

	// Registered AFTER t.TempDir()'s own cleanup, so it runs BEFORE it (LIFO)
	// and the staged files still exist when we copy them.
	t.Cleanup(func() {
		reason := ""
		switch {
		case t.Failed():
			reason = "test FAILED — transcript retained for diagnosis"
		case promoteRequested:
			reason = wireFacadeE2EEvidenceEnv + "=1 — operator-requested capture"
		default:
			t.Logf("evidence captured under %s (run-scoped; not promoted into docs/qa — set %s=1 to keep it)",
				staging, wireFacadeE2EEvidenceEnv)
			return
		}
		dest, err := promoteE2EEvidence(staging)
		if err != nil {
			t.Logf("evidence promotion failed (%v) — transcript remains only in %s", err, staging)
			return
		}
		t.Logf("evidence promoted to %s (%s)", dest, reason)
	})
	return staging
}

// promoteE2EEvidence copies the staged transcript files into the committed
// docs/qa tree. Path is relative to this package directory
// (helix_code/internal/server) up to the meta-repo root:
// internal/server -> helix_code (inner app root) -> meta-repo root -> docs/qa.
func promoteE2EEvidence(staging string) (string, error) {
	base := filepath.Join("..", "..", "..", "docs", "qa")
	ts := time.Now().UTC().Format("20060102T150405Z")
	dest := filepath.Join(base, "phase1_fullhttp_e2e_"+ts)
	// Two promotions inside the same second must not merge into one directory.
	for n := 2; ; n++ {
		if _, err := os.Stat(dest); os.IsNotExist(err) {
			break
		}
		dest = filepath.Join(base, fmt.Sprintf("phase1_fullhttp_e2e_%s_%d", ts, n))
		if n > 100 {
			return "", errors.New("could not find a free evidence directory name")
		}
	}
	if err := os.MkdirAll(dest, 0o755); err != nil {
		return "", err
	}
	entries, err := os.ReadDir(staging)
	if err != nil {
		return "", err
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		data, readErr := os.ReadFile(filepath.Join(staging, e.Name()))
		if readErr != nil {
			return "", readErr
		}
		if writeErr := os.WriteFile(filepath.Join(dest, e.Name()), data, 0o644); writeErr != nil {
			return "", writeErr
		}
	}
	return dest, nil
}

// curlCapture drives a REAL POST request via the actual `curl` binary
// (os/exec — a genuine external HTTP client process, not an in-process
// handler call) against the given httptest.Server URL, capturing the HTTP
// status code and response body. headers is a flat list of "Name: Value"
// strings passed via repeated curl -H flags.
func curlCapture(t *testing.T, url string, headers []string, body string) (status int, respBody string, rawTranscript string) {
	t.Helper()

	bodyFile, err := os.CreateTemp("", "wire-facade-e2e-body-*.json")
	require.NoError(t, err)
	defer func() { _ = os.Remove(bodyFile.Name()) }()
	require.NoError(t, bodyFile.Close())

	args := []string{
		"-s", "-S",
		"-X", "POST",
		"-o", bodyFile.Name(),
		"-w", "%{http_code}",
		"--max-time", strconv.Itoa(wireFacadeE2ECurlBudgetSeconds(t)),
		"-H", "Content-Type: application/json",
	}
	for _, h := range headers {
		args = append(args, "-H", h)
	}
	args = append(args, "-d", body, url)

	cmd := exec.Command("curl", args...)
	out, err := cmd.CombinedOutput()
	require.NoErrorf(t, err, "curl invocation failed: %v (output=%s)", err, string(out))

	statusStr := strings.TrimSpace(string(out))
	status, err = strconv.Atoi(statusStr)
	require.NoErrorf(t, err, "curl did not report a numeric HTTP status (got %q)", statusStr)

	respBytes, err := os.ReadFile(bodyFile.Name())
	require.NoError(t, err)
	respBody = string(respBytes)

	var reqHeaderLines strings.Builder
	for _, h := range headers {
		reqHeaderLines.WriteString(h)
		reqHeaderLines.WriteString("\n")
	}
	rawTranscript = fmt.Sprintf(
		"=== REQUEST ===\nPOST %s\nContent-Type: application/json\n%s\n%s\n\n=== RESPONSE ===\nHTTP %d\n%s\n",
		url, reqHeaderLines.String(), body, status, respBody)
	return status, respBody, rawTranscript
}

// wireFacadeE2EFixture builds a real *Server (the SAME construction
// wire_facade_auth_test.go's wireFacadeAuthFixture uses — real gin router +
// real setupRoutes(), so the two wire-facade routes, wireFacadeAuthMiddleware,
// and every other db/redis-independent route are ALL genuinely wired) and
// wraps it in a real httptest.Server (a real TCP listener + real net/http
// serving).
func wireFacadeE2EFixture(t *testing.T) *httptest.Server {
	t.Helper()
	gin.SetMode(gin.TestMode)

	cfg := &config.Config{
		Auth: config.AuthConfig{
			JWTSecret:         "test-secret-key-for-testing-only",
			TokenExpiry:       3600,
			BcryptCost:        4,
			WireFacadeAPIKeys: wireFacadeE2ETestAPIKey,
		},
		Logging: config.LoggingConfig{Level: "error"},
	}

	srv := &Server{
		config: cfg,
		router: gin.New(),
	}
	srv.setupRoutes()

	ts := httptest.NewServer(srv.router)
	t.Cleanup(ts.Close)
	return ts
}

func TestWireFacade_FullHTTP_E2E_LiveRoundTrip(t *testing.T) {
	// Single explicit reachability precondition, evaluated ONCE before any
	// request is issued, so this test either runs in full or skips in full —
	// never partially (§11.4.3 / §11.4.50). See helixLLMLocalReachable in
	// llm_generate_helixllm_live_test.go
	// for why this does not reuse the shared 2s-timeout helper.
	if ok, why := helixLLMLocalReachable(t); !ok {
		t.Skipf("SKIP-OK (§11.4.3): %s", why)
	}

	// Route resolveLLMProvider's provider selection to the coder: server.go's
	// wire-facade handlers call llmProviderResolver("", llmReq.Model) — an
	// EMPTY provider-name argument — so resolution falls through to
	// HELIX_LLM_PROVIDER (see resolveLLMProvider's doc-comment: "Honour
	// HELIX_LLM_PROVIDER only when the request did not name a provider").
	t.Setenv("HELIX_LLM_PROVIDER", "helixllm")
	t.Setenv("HELIX_LLM_LOCAL_OPENAI_ENDPOINT", envHelixLLMLocalEndpoint())

	ts := wireFacadeE2EFixture(t)
	evidenceDir := wireFacadeE2EEvidenceDir(t)

	// Discovered ONCE from the live coder, then asserted against below, so the
	// model-identity assertion tracks whatever this deployment actually serves
	// instead of a name baked into the source (§11.4.111).
	servedModel := wireFacadeE2EServedModelID(t)
	t.Logf("live coder at %s reports served model id = %q (per-request curl budget %ds via %s)",
		envHelixLLMLocalEndpoint(), servedModel, wireFacadeE2ECurlBudgetSeconds(t), wireFacadeE2ECurlBudgetEnv)

	t.Run("no_auth_chat_completions_401", func(t *testing.T) {
		body := `{"messages":[{"role":"user","content":"hi"}]}`
		status, respBody, transcript := curlCapture(t, ts.URL+"/v1/chat/completions", nil, body)
		_ = os.WriteFile(filepath.Join(evidenceDir, "01_no_auth_401.txt"), []byte(transcript), 0o644)

		require.Equalf(t, 401, status, "unauthenticated POST /v1/chat/completions must be rejected 401 (fail-closed) — got %d body=%s", status, respBody)

		var errBody map[string]interface{}
		require.NoError(t, json.Unmarshal([]byte(respBody), &errBody))
		errObj, ok := errBody["error"].(map[string]interface{})
		require.True(t, ok, "expected an {error:{...}} body, got %s", respBody)
		require.Equal(t, "authentication_error", errObj["type"])

		t.Logf("PASS: no-auth request to /v1/chat/completions correctly rejected: status=%d body=%s", status, respBody)
	})

	t.Run("openai_chat_completions_authenticated_200", func(t *testing.T) {
		nonce := wireFacadeE2ENonce(t)
		reqBody := fmt.Sprintf(`{"messages":[{"role":"user","content":"This is an automated full-HTTP e2e liveness probe. Reply with EXACTLY this token and nothing else: %s"}],"max_tokens":32,"temperature":0}`, nonce)

		headers := []string{"Authorization: Bearer " + wireFacadeE2ETestAPIKey}
		status, respBody, transcript := curlCapture(t, ts.URL+"/v1/chat/completions", headers, reqBody)
		_ = os.WriteFile(filepath.Join(evidenceDir, "02_openai_chat_completions_200.txt"), []byte(transcript), 0o644)

		require.Equalf(t, 200, status, "authenticated POST /v1/chat/completions must succeed — got %d body=%s", status, respBody)

		var parsed struct {
			ID      string `json:"id"`
			Object  string `json:"object"`
			Model   string `json:"model"`
			Choices []struct {
				Message struct {
					Role    string `json:"role"`
					Content string `json:"content"`
				} `json:"message"`
				FinishReason string `json:"finish_reason"`
			} `json:"choices"`
			Usage struct {
				PromptTokens     int `json:"prompt_tokens"`
				CompletionTokens int `json:"completion_tokens"`
				TotalTokens      int `json:"total_tokens"`
			} `json:"usage"`
		}
		require.NoErrorf(t, json.Unmarshal([]byte(respBody), &parsed), "response body must be valid OpenAI-shaped JSON: %s", respBody)

		require.Equal(t, "chat.completion", parsed.Object, "must be OpenAI Chat Completions wire shape")
		require.NotEmpty(t, parsed.ID)
		require.Len(t, parsed.Choices, 1)
		require.Equal(t, "assistant", parsed.Choices[0].Message.Role)
		require.Containsf(t, parsed.Choices[0].Message.Content, nonce,
			"real coder completion must echo the fresh nonce (proves a live, non-cached answer): got %q", parsed.Choices[0].Message.Content)
		require.Equalf(t, strings.ToLower(servedModel), strings.ToLower(parsed.Model),
			"resolved model must be the id the live coder reports at /v1/models (%q), got %q — "+
				"the handler must propagate the coder's own model identity, not echo the caller's "+
				"string or invent one", servedModel, parsed.Model)
		require.Greater(t, parsed.Usage.PromptTokens, 0, "usage.prompt_tokens must be a real positive count")
		require.Greater(t, parsed.Usage.CompletionTokens, 0, "usage.completion_tokens must be a real positive count")

		t.Logf("PASS: OpenAI-shaped 200 via real HTTP+curl+router+middleware+coder: model=%q finish_reason=%q usage=%+v content=%q",
			parsed.Model, parsed.Choices[0].FinishReason, parsed.Usage, parsed.Choices[0].Message.Content)
	})

	t.Run("anthropic_messages_authenticated_200", func(t *testing.T) {
		nonce := wireFacadeE2ENonce(t)
		reqBody := fmt.Sprintf(`{"messages":[{"role":"user","content":"This is an automated full-HTTP e2e liveness probe. Reply with EXACTLY this token and nothing else: %s"}],"max_tokens":32,"temperature":0}`, nonce)

		headers := []string{"x-api-key: " + wireFacadeE2ETestAPIKey}
		status, respBody, transcript := curlCapture(t, ts.URL+"/v1/messages", headers, reqBody)
		_ = os.WriteFile(filepath.Join(evidenceDir, "03_anthropic_messages_200.txt"), []byte(transcript), 0o644)

		require.Equalf(t, 200, status, "authenticated POST /v1/messages must succeed — got %d body=%s", status, respBody)

		var parsed struct {
			ID      string `json:"id"`
			Type    string `json:"type"`
			Role    string `json:"role"`
			Model   string `json:"model"`
			Content []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
			StopReason string `json:"stop_reason"`
			Usage      struct {
				InputTokens  int `json:"input_tokens"`
				OutputTokens int `json:"output_tokens"`
			} `json:"usage"`
		}
		require.NoErrorf(t, json.Unmarshal([]byte(respBody), &parsed), "response body must be valid Anthropic-shaped JSON: %s", respBody)

		require.Equal(t, "message", parsed.Type, "must be Anthropic Messages wire shape")
		require.Equal(t, "assistant", parsed.Role)
		require.NotEmpty(t, parsed.ID)
		require.NotEmpty(t, parsed.StopReason)
		require.GreaterOrEqual(t, len(parsed.Content), 1)

		var fullText strings.Builder
		for _, block := range parsed.Content {
			fullText.WriteString(block.Text)
		}
		require.Containsf(t, fullText.String(), nonce,
			"real coder completion must echo the fresh nonce (proves a live, non-cached answer): got %q", fullText.String())
		require.Equalf(t, strings.ToLower(servedModel), strings.ToLower(parsed.Model),
			"resolved model must be the id the live coder reports at /v1/models (%q), got %q — "+
				"the handler must propagate the coder's own model identity, not echo the caller's "+
				"string or invent one", servedModel, parsed.Model)
		require.Greater(t, parsed.Usage.InputTokens, 0, "usage.input_tokens must be a real positive count")
		require.Greater(t, parsed.Usage.OutputTokens, 0, "usage.output_tokens must be a real positive count")

		t.Logf("PASS: Anthropic-shaped 200 via real HTTP+curl+router+middleware+coder: model=%q stop_reason=%q usage=%+v content=%q",
			parsed.Model, parsed.StopReason, parsed.Usage, fullText.String())
	})

	// tool_calls_shape_divergence_live is the LIVE proof (not merely a
	// static code-read of llmResponseToOpenAI/llmResponseToAnthropic) of the
	// genuine, documented wire-shape fork point between the two facades
	// (wire_facade.go file-level doc-comment "Multi-modal content parts..."
	// section + llmResponseToOpenAI/llmResponseToAnthropic doc-comments):
	// the OpenAI wire's tool_calls[].function.arguments is a
	// JSON-ENCODED STRING, while the Anthropic wire's
	// content[].{type:"tool_use"}.input is a JSON OBJECT. Both requests
	// below drive the SAME real coder through the SAME tool-definition and
	// SAME nonce-bearing prompt so the divergence is observed on genuinely
	// live, non-cached tool-call output — not two independently-fabricated
	// fixtures.
	//
	// OPT-IN SINCE 2026-09-07 — and here is exactly why (§11.4.6, do not
	// silently re-enable this by default):
	//
	//   - Against the CODER this subtest configures, it FAILED 5/5 in a
	//     back-to-back baseline, always for the same reason: the coder never
	//     emits native tool_calls, it returns the call as a fenced ```json blob
	//     with finish_reason "stop". Measured: `expected: "tool_calls" /
	//     actual: "stop"` (OpenAI facade) and `expected: "tool_use" / actual:
	//     "max_tokens"` (Anthropic facade). The assertion is unsatisfiable
	//     there — that is a stable property of the coder, not a flake.
	//
	//   - Repointing it at the GATEWAY (the direction ledger item
	//     HXC-002-F3-09 originally recorded) trades an unsatisfiable assertion
	//     for a NON-DETERMINISTIC one. MEASURED 2026-09-07: twelve
	//     byte-identical POSTs at temperature 0 gave 8/12 with tool_calls
	//     PRESENT and 4/12 with finish_reason=length and tool_calls ABSENT.
	//     Running the sibling live guard five times reproduced the split
	//     directly: PASS, FAIL, PASS, PASS, PASS.
	//
	// The wire-shape divergence itself is NOT unguarded as a result. It is
	// asserted on EVERY default run, deterministically, through this same full
	// HTTP path (real curl -> real router -> real middleware -> real handlers
	// -> real provider) with REAL captured gateway bytes replayed in place of
	// the model, by TestWireFacade_FullHTTP_E2E_ReplayToolCallShapes in
	// wire_facade_toolcall_replay_test.go.
	t.Run("tool_calls_shape_divergence_live", func(t *testing.T) {
		if !liveToolCallProbeEnabled() {
			// SKIP-OK (§11.4.3): opt-in because no live backend can satisfy
			// this assertion deterministically (see the block above). Nothing
			// is suppressed — the identical wire-shape assertions run on every
			// default invocation against real recorded bytes in
			// TestWireFacade_FullHTTP_E2E_ReplayToolCallShapes.
			t.Skip("SKIP-OK (§11.4.3): live tool-call shape probe is opt-in — set " +
				liveToolCallProbeEnv + "=1 (and point " + helixLLMLocalOpenAIEndpointEnv +
				" at a backend that emits native tool_calls, e.g. the gateway) to run it. " +
				"The deterministic replay of these same two wire-shape assertions runs on " +
				"every invocation: TestWireFacade_FullHTTP_E2E_ReplayToolCallShapes.")
		}
		nonce := wireFacadeE2ENonce(t)
		toolsJSON := `[{"type":"function","function":{"name":"get_weather","description":"Get the current weather for a city","parameters":{"type":"object","properties":{"city":{"type":"string"}},"required":["city"]}}}]`
		prompt := fmt.Sprintf("What is the weather in the city named exactly %q? You MUST call the get_weather tool with that exact city string.", nonce)

		t.Run("openai_tool_calls_arguments_is_json_string", func(t *testing.T) {
			reqBody := fmt.Sprintf(`{"messages":[{"role":"user","content":%q}],"tools":%s,"max_tokens":64,"temperature":0}`, prompt, toolsJSON)
			headers := []string{"Authorization: Bearer " + wireFacadeE2ETestAPIKey}
			status, respBody, transcript := curlCapture(t, ts.URL+"/v1/chat/completions", headers, reqBody)
			_ = os.WriteFile(filepath.Join(evidenceDir, "04_openai_tool_calls_200.txt"), []byte(transcript), 0o644)
			require.Equalf(t, 200, status, "authenticated tool-calling POST /v1/chat/completions must succeed — got %d body=%s", status, respBody)

			var parsed struct {
				Choices []struct {
					Message struct {
						ToolCalls []struct {
							Type     string `json:"type"`
							Function struct {
								Name      string `json:"name"`
								Arguments string `json:"arguments"` // MUST be a JSON-encoded STRING on this wire
							} `json:"function"`
						} `json:"tool_calls"`
					} `json:"message"`
					FinishReason string `json:"finish_reason"`
				} `json:"choices"`
			}
			require.NoErrorf(t, json.Unmarshal([]byte(respBody), &parsed), "response must be valid OpenAI-shaped JSON: %s", respBody)
			require.Len(t, parsed.Choices, 1)
			require.Equal(t, "tool_calls", parsed.Choices[0].FinishReason, "coder must have actually invoked the tool for this proof to be meaningful")
			require.Len(t, parsed.Choices[0].Message.ToolCalls, 1)
			tc := parsed.Choices[0].Message.ToolCalls[0]
			require.Equal(t, "get_weather", tc.Function.Name)

			// The wire-shape assertion: Arguments MUST decode as a Go string
			// field straight off the JSON (proving the wire byte sequence was
			// `"arguments":"{...}"` — a quoted, JSON-ENCODED STRING), and that
			// string itself MUST re-parse as its own JSON object containing
			// the live nonce.
			require.NotEmpty(t, tc.Function.Arguments, "OpenAI wire tool_calls[].function.arguments must be a non-empty JSON-encoded string")
			var argsObj map[string]interface{}
			require.NoErrorf(t, json.Unmarshal([]byte(tc.Function.Arguments), &argsObj),
				"function.arguments must itself be a valid JSON object once string-decoded (JSON-STRING-of-JSON wire shape): %q", tc.Function.Arguments)
			require.Containsf(t, fmt.Sprintf("%v", argsObj["city"]), nonce, "tool arguments must echo the live nonce city: %v", argsObj)

			t.Logf("PASS (OpenAI wire): tool_calls[0].function.arguments is a JSON-ENCODED STRING = %q (decodes to %v)", tc.Function.Arguments, argsObj)
		})

		t.Run("anthropic_tool_use_input_is_json_object", func(t *testing.T) {
			reqBody := fmt.Sprintf(`{"messages":[{"role":"user","content":%q}],"tools":[{"name":"get_weather","description":"Get the current weather for a city","input_schema":{"type":"object","properties":{"city":{"type":"string"}},"required":["city"]}}],"max_tokens":64,"temperature":0}`, prompt)
			headers := []string{"x-api-key: " + wireFacadeE2ETestAPIKey}
			status, respBody, transcript := curlCapture(t, ts.URL+"/v1/messages", headers, reqBody)
			_ = os.WriteFile(filepath.Join(evidenceDir, "05_anthropic_tool_use_200.txt"), []byte(transcript), 0o644)
			require.Equalf(t, 200, status, "authenticated tool-calling POST /v1/messages must succeed — got %d body=%s", status, respBody)

			var parsed struct {
				StopReason string `json:"stop_reason"`
				Content    []struct {
					Type  string                 `json:"type"`
					Name  string                 `json:"name"`
					Input map[string]interface{} `json:"input"` // MUST be a JSON OBJECT on this wire
				} `json:"content"`
			}
			require.NoErrorf(t, json.Unmarshal([]byte(respBody), &parsed), "response must be valid Anthropic-shaped JSON: %s", respBody)
			require.Equal(t, "tool_use", parsed.StopReason, "coder must have actually invoked the tool for this proof to be meaningful")

			var toolBlock *struct {
				Type  string                 `json:"type"`
				Name  string                 `json:"name"`
				Input map[string]interface{} `json:"input"`
			}
			for i := range parsed.Content {
				if parsed.Content[i].Type == "tool_use" {
					toolBlock = &parsed.Content[i]
					break
				}
			}
			require.NotNilf(t, toolBlock, "response content must include a tool_use block: %s", respBody)
			require.Equal(t, "get_weather", toolBlock.Name)

			// The wire-shape assertion: Input decoded straight into a Go map
			// off the raw response bytes — proving the wire byte sequence was
			// `"input":{...}` — a genuine JSON OBJECT, never a quoted string.
			require.NotEmptyf(t, toolBlock.Input, "Anthropic wire tool_use.input must be a non-empty JSON OBJECT")
			require.Containsf(t, fmt.Sprintf("%v", toolBlock.Input["city"]), nonce, "tool input must echo the live nonce city: %v", toolBlock.Input)
			require.NotContainsf(t, respBody, fmt.Sprintf(`"input":"{`), "tool_use.input must be a raw JSON object on the wire, never a JSON-encoded string")

			t.Logf("PASS (Anthropic wire): content[].input is a JSON OBJECT = %v", toolBlock.Input)
		})
	})

	t.Logf("evidence captured under %s", evidenceDir)
}
