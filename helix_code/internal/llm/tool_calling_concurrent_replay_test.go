package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"dev.helix.code/internal/testutil/wirecorpus"
)

// tool_calling_concurrent_replay_test.go — the DETERMINISTIC half of the D6
// tool-calling type-validation guard.
//
// ============================================================================
// WHY THIS FILE EXISTS — DO NOT REPOINT IT AT A LIVE MODEL
// ============================================================================
//
// TestToolCalling_ConcurrentLoad_ArgumentsAreTypedJSON fires N=16 concurrent
// tool-calling requests at a LIVE backend and asserts every response's
// arguments decode as a well-typed JSON object. Its verdict is not
// deterministic, and cannot be made so:
//
//   - BEFORE baseline, five back-to-back identical invocations against the
//     configured coder (localhost:18434): SKIP, FAIL, FAIL, FAIL, FAIL. TWO
//     different verdicts from identical commands. The SKIP came from the
//     guard's own feasibility check (measured 5.954 tok/s across 4 slots needs
//     ~2m14s > the 1m30s client budget) — a throughput measurement that varies
//     with host load, so whether the guard runs at all is a function of what
//     else the machine is doing.
//
//   - When it DOES run, it fails 16/16 for a reason that is not a
//     tool-calling defect: the coder never emits native tool_calls. Measured:
//     `no tool_calls returned (finish_reason="stop"
//     content="```json\n{\"name\": \"add\", \"arguments\": {\"a\": 1000,
//     \"b\": 2000}}\n```")`. That is stable and reproducible — the assertion
//     is simply unsatisfiable against that backend.
//
//   - Repointing it at the GATEWAY, which DOES emit native tool_calls, trades
//     an unsatisfiable assertion for a non-deterministic one: MEASURED
//     2026-09-07, twelve byte-identical requests at temperature 0 gave 8/12
//     with tool_calls PRESENT and 4/12 with finish_reason=length and
//     tool_calls ABSENT.
//
// This guard therefore replays SIXTEEN DISTINCT REAL gateway responses — one
// per concurrent slot, each recorded live for its own (a,b) pair by
// helix_code/testdata/toolcall_wire_corpus/capture.py and sha256-pinned in
// provenance.json — through the same strict decoder the live guard uses. The
// replay backend routes each in-flight request to the recording for ITS OWN
// (a,b), so a response landing on the wrong request is still mechanically
// detectable.
//
// HONEST BOUNDARY (§11.4.6) — read this before assuming more than it proves.
// Because the backend is a recording, this guard detects cross-contamination
// in OUR client path (request/response mismatching, shared-state races across
// the 16 goroutines, decoder state leaking between calls). It CANNOT detect
// cross-contamination inside the model server's own scheduler — that requires
// a live backend, and lives in the opt-in probe
// TestToolCalling_ConcurrentLoad_ArgumentsAreTypedJSON. What this guard does
// prove deterministically is the whole #1809 bug class: arguments arriving as
// a bare array, double-encoded, or type-confused are rejected by
// decodeAddArguments on every run, against real recorded bytes.
// ============================================================================

// replayAddPrompt extracts the (a,b) pair from a recorded-shape request so the
// replay backend can answer each concurrent request with ITS OWN recording
// rather than a single shared body. Matching on the request's own content is
// what makes a mis-routed response detectable.
var replayAddPrompt = regexp.MustCompile(`compute (\d+) plus (\d+)`)

// toolCallingReplayBackend serves the recorded corpus over a real TCP socket.
type toolCallingReplayBackend struct {
	*httptest.Server
	corpus *wirecorpus.Corpus

	mu       sync.Mutex
	served   map[string]int // recording name -> times served
	unmapped int            // requests whose (a,b) matched no recording
}

func newToolCallingReplayBackend(t *testing.T, corpus *wirecorpus.Corpus) *toolCallingReplayBackend {
	t.Helper()

	// Build the (a,b) -> recording index from the corpus itself, using the
	// SAME index-derived pairs the live guard sends.
	index := map[string]string{}
	for idx := 0; idx < toolCallingConcurrency; idx++ {
		a := 1000 + idx*7
		b := 2000 + idx*11
		index[fmt.Sprintf("%d/%d", a, b)] = wirecorpus.GatewayAdd(idx)
	}

	b := &toolCallingReplayBackend{corpus: corpus, served: map[string]int{}}
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/models", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(corpus.Body(t, wirecorpus.GatewayModels))
	})
	mux.HandleFunc("/v1/chat/completions", func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		m := replayAddPrompt.FindStringSubmatch(string(body))
		if m == nil {
			b.mu.Lock()
			b.unmapped++
			b.mu.Unlock()
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":{"message":"replay backend: request carries no recognisable add(a,b) prompt"}}`))
			return
		}
		name, ok := index[m[1]+"/"+m[2]]
		if !ok {
			b.mu.Lock()
			b.unmapped++
			b.mu.Unlock()
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":{"message":"replay backend: no recording for that (a,b) pair"}}`))
			return
		}
		b.mu.Lock()
		b.served[name]++
		b.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(corpus.Body(t, name))
	})

	b.Server = httptest.NewServer(mux)
	t.Cleanup(b.Server.Close)
	return b
}

// TestToolCalling_ConcurrentReplay_ArgumentsAreTypedJSON is the DETERMINISTIC
// standing D6 regression guard: N=16 genuinely concurrent HTTP requests, each
// answered with the real gateway response recorded for that request's own
// (a,b), every one asserted to decode as a correctly-typed JSON object with no
// cross-request mismatch.
//
// Same verdict on every run, with the live services up OR down.
func TestToolCalling_ConcurrentReplay_ArgumentsAreTypedJSON(t *testing.T) {
	corpus := wirecorpus.Load(t)
	backend := newToolCallingReplayBackend(t, corpus)

	client := &http.Client{Timeout: 30 * time.Second}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// Reuses the LIVE guard's own request builder and strict decoder — the
	// code under test is identical, only the peer is a recording.
	results := make([]toolCallingRequestResult, toolCallingConcurrency)
	var wg sync.WaitGroup
	wg.Add(toolCallingConcurrency)
	start := time.Now()
	for i := 0; i < toolCallingConcurrency; i++ {
		go func(idx int) {
			defer wg.Done()
			results[idx] = fireOneToolCallingRequest(ctx, client, backend.URL, "replay", idx)
		}(i)
	}
	wg.Wait()
	elapsed := time.Since(start)

	var failures, crossContaminate []string
	passCount := 0
	for _, r := range results {
		label := fmt.Sprintf("req[%d] (expected a=%d,b=%d)", r.index, r.expectedA, r.expectedB)
		switch {
		case r.err != nil:
			failures = append(failures, fmt.Sprintf("%s: transport/parse error: %v", label, r.err))
		case r.httpStatus != http.StatusOK:
			failures = append(failures, fmt.Sprintf("%s: HTTP %d body=%s", label, r.httpStatus, r.rawBody))
		case r.toolCallLen != 1:
			failures = append(failures, fmt.Sprintf("%s: expected exactly 1 tool_call, got %d", label, r.toolCallLen))
		case r.funcName != "add":
			failures = append(failures, fmt.Sprintf("%s: unexpected function name %q", label, r.funcName))
		case r.decodeErr != nil:
			// The OpenCode #1809 bug class: arguments as a bare array, as a
			// double-encoded string, or with a type-confused field.
			failures = append(failures, fmt.Sprintf(
				"%s: ARRAY-AS-STRING / TYPE-CONFUSION BUG CLASS — raw arguments=%q decode error: %v",
				label, r.rawArgs, r.decodeErr))
		case r.decoded.A != r.expectedA || r.decoded.B != r.expectedB:
			crossContaminate = append(crossContaminate, fmt.Sprintf(
				"%s: got a=%d,b=%d (raw=%q) — this response does not belong to this request",
				label, r.decoded.A, r.decoded.B, r.rawArgs))
		default:
			passCount++
		}
	}

	if len(failures) > 0 {
		t.Fatalf("D6 replay guard: %d/%d requests FAILED type-validation:\n%s",
			len(failures), toolCallingConcurrency, strings.Join(failures, "\n"))
	}
	if len(crossContaminate) > 0 {
		t.Fatalf("D6 replay guard: %d/%d responses were matched to the WRONG request:\n%s",
			len(crossContaminate), toolCallingConcurrency, strings.Join(crossContaminate, "\n"))
	}
	if passCount != toolCallingConcurrency {
		t.Fatalf("D6 replay guard: expected %d passes, got %d", toolCallingConcurrency, passCount)
	}

	// The backend's own bookkeeping: every recording used exactly once, and no
	// request arrived that the corpus could not answer. Without this, a bug
	// that sent all 16 requests with identical content would still show 16
	// green decodes.
	backend.mu.Lock()
	served, unmapped := backend.served, backend.unmapped
	backend.mu.Unlock()
	if unmapped != 0 {
		t.Fatalf("D6 replay guard: %d request(s) carried no recognisable (a,b) — the 16 concurrent "+
			"requests must be distinguishable, or cross-contamination is undetectable", unmapped)
	}
	if len(served) != toolCallingConcurrency {
		t.Fatalf("D6 replay guard: expected all %d recordings to be exercised, %d were",
			toolCallingConcurrency, len(served))
	}
	for name, n := range served {
		if n != 1 {
			t.Fatalf("D6 replay guard: recording %s was served %d times, expected exactly 1", name, n)
		}
	}

	t.Logf("D6 REPLAY GUARD PASS: N=%d simultaneous requests, each answered with its OWN real gateway "+
		"recording (corpus captured %s from %s); every tool_call.function.arguments decoded as a "+
		"correctly-typed {a:int,b:int} object, all %d recordings exercised exactly once, wall-clock %v",
		toolCallingConcurrency, corpus.Provenance.CapturedAtUTC, corpus.Provenance.GatewayEndpoint,
		len(served), elapsed)
}

// TestToolCallingReplayCorpus_IsRealCapturedEvidence asserts the 16 add
// recordings are genuine, distinct, correctly-typed captured bytes — so a
// green replay above cannot come from a degenerate or hand-written corpus
// (§11.4.107(10)).
func TestToolCallingReplayCorpus_IsRealCapturedEvidence(t *testing.T) {
	corpus := wirecorpus.Load(t) // sha256-verifies every recorded body

	seen := map[string]bool{}
	for idx := 0; idx < toolCallingConcurrency; idx++ {
		name := wirecorpus.GatewayAdd(idx)
		raw := corpus.Body(t, name)

		var parsed toolCallingChatResponse
		if err := json.Unmarshal(raw, &parsed); err != nil {
			t.Fatalf("%s is not valid JSON: %v", name, err)
		}
		if len(parsed.Choices) != 1 || len(parsed.Choices[0].Message.ToolCalls) != 1 {
			t.Fatalf("%s does not carry exactly one recorded tool call — the replay would assert nothing", name)
		}
		tc := parsed.Choices[0].Message.ToolCalls[0]
		if tc.Function.Name != "add" {
			t.Fatalf("%s records function %q, expected \"add\"", name, tc.Function.Name)
		}
		// The recorded wire value MUST be a JSON-encoded STRING per the OpenAI
		// spec — the strict decoder is what turns it into typed fields.
		decoded, err := decodeAddArguments(tc.Function.Arguments)
		if err != nil {
			t.Fatalf("%s recorded arguments %q do not decode as a typed object: %v",
				name, tc.Function.Arguments, err)
		}
		wantA, wantB := 1000+idx*7, 2000+idx*11
		if decoded.A != wantA || decoded.B != wantB {
			t.Fatalf("%s records a=%d,b=%d but was captured for a=%d,b=%d",
				name, decoded.A, decoded.B, wantA, wantB)
		}
		key := strconv.Itoa(decoded.A) + "/" + strconv.Itoa(decoded.B)
		if seen[key] {
			t.Fatalf("%s duplicates an earlier recording's (a,b)=%s — the 16 recordings must be "+
				"distinct or cross-contamination is undetectable", name, key)
		}
		seen[key] = true
	}

	t.Logf("PASS: %d distinct real add-tool recordings verified (corpus captured %s from %s)",
		len(seen), corpus.Provenance.CapturedAtUTC, corpus.Provenance.GatewayEndpoint)
}
