package server

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"dev.helix.code/internal/llm"
	"github.com/google/uuid"
)

// llm_generate_helixllm_live_test.go — LIVE round-trip proof (§11.4.5 /
// §11.4.69 / §11.4.107) that a REAL completion flows through
// resolveLLMProvider's new "helixllm" local-coder route all the way to the
// actual llama.cpp OpenAI-compatible sidecar (default
// http://localhost:18434), closing the gap
// scratchpad/phase1_dual_wire_facade.md flagged: "resolveLLMProvider ... has
// NO path to the LOCAL HelixLLM coder".
//
// This test is READ-ONLY against the coder — a single real Generate() call,
// no config changes, no writes (§11.4.122) — and honestly SKIPs (never
// fake-PASSes, CONST-035/§11.4.3) when the coder is not reachable, so it runs
// safely as part of the default test invocation in every environment. No
// build tag is used: unlike the CONST-039 hosted-cloud harness in
// internal/llm/provider_live_proof_test.go (which build-tags itself out of
// default runs to avoid real API cost), a local loopback call to this coder
// carries zero API cost.
//
// Run:
//
//	cd helix_code && <toolchain> test -v -run TestResolveLLMProvider_HelixLLMLocal_LiveRoundTrip ./internal/server/...

// helixLLMLocalProbeBudgetEnv overrides how long the shared reachability
// precondition will wait before calling an AMBIGUOUS endpoint unreachable.
const helixLLMLocalProbeBudgetEnv = "HELIX_LLM_LOCAL_PROBE_BUDGET_SECONDS"

// helixLLMLocalReachable is the ONE run-or-skip decision every live test that
// needs the local HelixLLM coder shares. It CLASSIFIES the outcome instead of
// racing a clock (D-3).
//
// THE DEFECT IT REPLACES. This helper used to probe the coder with a flat 2s
// timeout and report a bare bool. A bool cannot distinguish "the coder is
// DOWN" from "the coder is UP but momentarily slow", and on a host running at
// ~3.5x CPU oversubscription those two cases are routinely within 2s of each
// other. So whether the live proofs ran at all was decided by ambient load:
// under load they SKIPped and silently lost their coverage, and the same
// command produced different verdicts on different runs. That is precisely
// the non-determinism the operator mandate forbids. The fix is NOT a wider
// timeout — widening only moves the race — it is to remove the dependence by
// deciding on the transport's own classification.
//
// This precondition separates the two cases that are genuinely decidable from
// the one that is not:
//
//   - a NON-timeout transport error (connection refused, DNS failure, TLS
//     failure) is DEFINITIVE — the coder is not there. Skip immediately, with
//     no waiting at all. This is the fast path when nothing is running.
//   - HTTP 200 is DEFINITIVE — the coder is there. Run.
//   - a TIMEOUT is genuinely ambiguous, and is the ONLY case that consumes the
//     budget: retry until it resolves one way or the other. A live coder
//     answers; a black hole does not.
//
// So the budget is not a discrimination threshold between "up" and "down" —
// both of those are answered immediately by the transport. It bounds only the
// ambiguous case, and its expiry is reported honestly as ambiguous rather than
// as "not running".
//
// Returns (true, "") to RUN, or (false, reason) to SKIP with that reason. The
// reason is never empty on a false return — an honest SKIP (§11.4.3) must say
// what was actually observed, never merely "not reachable".
func helixLLMLocalReachable(t *testing.T) (bool, string) {
	t.Helper()
	endpoint := envHelixLLMLocalEndpoint()
	budget := 30 * time.Second
	if raw := strings.TrimSpace(os.Getenv(helixLLMLocalProbeBudgetEnv)); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n > 0 {
			budget = time.Duration(n) * time.Second
		}
	}

	deadline := time.Now().Add(budget)
	attempts := 0
	var lastErr error
	for {
		attempts++
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint+"/v1/models", nil)
		if err != nil {
			cancel()
			return false, fmt.Sprintf("malformed endpoint %q: %v", endpoint, err)
		}
		resp, err := http.DefaultClient.Do(req)
		if err == nil {
			status := resp.StatusCode
			_ = resp.Body.Close()
			cancel()
			if status == http.StatusOK {
				return true, ""
			}
			return false, fmt.Sprintf("coder at %s answered HTTP %d (not 200) for /v1/models — "+
				"it is reachable but not serving, so this proof cannot run", endpoint, status)
		}
		cancel()
		lastErr = err

		// Definitive negative: the transport reached a conclusion that is not
		// about time. Do not burn the budget on it.
		var nerr net.Error
		if !(errors.As(err, &nerr) && nerr.Timeout()) {
			return false, fmt.Sprintf("local HelixLLM coder not reachable at %s (%v) — set "+
				"HELIX_LLM_LOCAL_ENDPOINT/HELIX_LLM_LOCAL_OPENAI_ENDPOINT or start the coder "+
				"to exercise this proof", endpoint, err)
		}
		if time.Now().After(deadline) {
			return false, fmt.Sprintf("local HelixLLM coder at %s neither answered nor refused "+
				"within %s (%d attempts, last error: %v) — AMBIGUOUS, not proven absent; raise "+
				"%s if this host is heavily loaded", endpoint, budget, attempts, lastErr,
				helixLLMLocalProbeBudgetEnv)
		}
		time.Sleep(250 * time.Millisecond)
	}
}

func TestResolveLLMProvider_HelixLLMLocal_LiveRoundTrip(t *testing.T) {
	// Single shared reachability precondition, evaluated ONCE before any
	// work, so this proof runs or skips WHOLLY and for a stated reason —
	// never because the host happened to be busy (§11.4.3 / §11.4.50).
	if ok, why := helixLLMLocalReachable(t); !ok {
		t.Skip("SKIP-OK: " + why)
	}

	// Exercise the EXACT production seam the HTTP handlers use
	// (llmProviderResolver, not resolveLLMProvider directly) so this proof
	// covers the real dispatch path generateLLM/streamLLM/wire_facade calls.
	provider, err := llmProviderResolver("helixllm", "")
	if err != nil {
		t.Fatalf("resolveLLMProvider(\"helixllm\", \"\") failed although the coder answered /v1/models: %v", err)
	}
	defer func() { _ = provider.Close() }()

	model := resolveDefaultModel(provider, "")
	if model == "" {
		t.Fatalf("resolveDefaultModel returned empty model although the coder's /v1/models catalog should be non-empty")
	}

	// Fresh per-run nonce (§11.4.2/§11.4.5 anti-bluff, same technique as
	// internal/llm/provider_live_proof_test.go's providerLiveNonce): a
	// cached/mocked/hardcoded response cannot possibly contain a token that
	// did not exist until this call executed.
	nonceBuf := make([]byte, 6)
	if _, err := rand.Read(nonceBuf); err != nil {
		t.Fatalf("nonce generation failed: %v", err)
	}
	nonce := "HELIXCODE-CODER-ROUTE-" + hex.EncodeToString(nonceBuf)

	req := &llm.LLMRequest{
		ID: uuid.New(),
		Messages: []llm.Message{{
			Role: "user",
			Content: fmt.Sprintf(
				"This is an automated liveness probe for the HelixCode->coder route. "+
					"Reply with EXACTLY this token and nothing else: %s", nonce),
		}},
		Model:       model,
		MaxTokens:   32,
		Temperature: 0,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	resp, genErr := provider.Generate(ctx, req)
	if genErr != nil {
		t.Fatalf("provider.Generate against the live coder failed: %v", genErr)
	}
	if resp.Content == "" {
		t.Fatalf("live coder returned empty content — no real completion produced")
	}
	if !strings.Contains(resp.Content, nonce) {
		t.Fatalf("response did not echo nonce %q (got %q) — cannot prove this is a live, non-cached answer", nonce, resp.Content)
	}

	t.Logf(
		"PASS: HelixCode resolveLLMProvider(\"helixllm\") -> REAL completion from the live coder: "+
			"model=%q finish_reason=%q tokens(prompt=%d,completion=%d,total=%d) content=%q",
		model, resp.FinishReason, resp.Usage.PromptTokens, resp.Usage.CompletionTokens, resp.Usage.TotalTokens, resp.Content,
	)
}
