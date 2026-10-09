package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

// helixllm_reachable_determinism_test.go — the §11.4.115 RED→GREEN polarity
// proof for D-3: the shared local-coder reachability precondition must decide
// run-vs-skip from the TRANSPORT'S OWN classification, not from a race against
// a flat clock.
//
// THE DEFECT. helixLLMLocalReachable used to be a flat 2s probe returning a
// bare bool. A bool collapses two genuinely different states into one answer:
//
//	"the coder is DOWN"                 -> skip is CORRECT
//	"the coder is UP but slow right now" -> skip LOSES the coverage
//
// On this host (~3.5x CPU oversubscription from a foreign workload) the second
// state is routine, so whether the live proofs executed at all was a function
// of ambient load: the same command produced a PASS on an idle host and a SKIP
// on a busy one. That is the load-dependent verdict the operator mandate
// forbids.
//
// WHY THIS IS NOT "WIDEN THE TIMEOUT". A wider flat timeout moves the race, it
// does not remove it — there is always a load level that exceeds any fixed
// bound. The dependence is removed by making the DECIDABLE cases decide
// immediately from the transport (connection refused / DNS failure = DOWN,
// definitively; HTTP 200 = UP, definitively) and letting the budget bound ONLY
// the genuinely ambiguous case (a timeout, which by construction cannot be
// classified from a single observation). TestHelixLLMLocalReachable_DownIsFast
// below asserts exactly that: the fix must NOT have made the absent-service
// case slower, which is what a widened bound would have done.
//
// HERMETIC: both cases are driven by a local httptest.Server, so this proof
// needs no real coder, no network, and is identical on every host. It is a
// unit test of the precondition, not a live test.

// legacyFlatCoderProbe is the VERBATIM pre-fix body of helixLLMLocalReachable —
// a flat 2s deadline collapsed into a bool. It exists solely so RED_MODE can
// reproduce the defect against the real behaviour it had, rather than against
// a description of it.
func legacyFlatCoderProbe(t *testing.T) bool {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, envHelixLLMLocalEndpoint()+"/v1/models", nil)
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

// slowButHealthyCoder stands up a local endpoint that behaves exactly like a
// coder under load: it DOES serve /v1/models with 200, but only after delay —
// longer than the pre-fix flat 2s budget, shorter than the classifier's
// per-attempt 5s budget.
func slowButHealthyCoder(t *testing.T, delay time.Duration) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" {
			http.NotFound(w, r)
			return
		}
		time.Sleep(delay)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"object":"list","data":[{"id":"slow-but-serving","object":"model"}]}`))
	}))
	t.Cleanup(srv.Close)
	return srv
}

// TestHelixLLMLocalReachable_SlowServiceIsNotMistakenForAbsent is the polarity
// test. ONE source, TWO roles:
//
//   - RED_MODE=1 drives the PRE-FIX flat 2s bool probe against a coder that is
//     genuinely UP (it answers 200, just 3s late) and asserts the probe says
//     "unreachable". It PASSES only while the defect is real: a healthy service
//     misclassified as absent, which is the lost coverage.
//   - RED_MODE=0 (default, standing regression guard) drives the FIXED
//     classifying precondition against the SAME endpoint and asserts it
//     correctly reports reachable.
//
// Deterministic in both polarities: the delay is produced by the test's own
// server, so neither branch depends on ambient host load.
func TestHelixLLMLocalReachable_SlowServiceIsNotMistakenForAbsent(t *testing.T) {
	const delay = 3 * time.Second // > pre-fix flat 2s budget, < classifier's 5s per attempt

	srv := slowButHealthyCoder(t, delay)
	t.Setenv(helixLLMLocalOpenAIEndpointEnv, srv.URL)
	// Keep the ambiguity budget generous enough to cover the delay but far
	// below the 30s default, so a genuine hang in this test surfaces quickly.
	t.Setenv(helixLLMLocalProbeBudgetEnv, "20")

	if os.Getenv("RED_MODE") == "1" {
		if legacyFlatCoderProbe(t) {
			t.Fatalf("RED_MODE: expected the pre-fix flat 2s bool probe to misclassify a "+
				"coder that answers 200 after %s as UNREACHABLE, but it reported reachable "+
				"— defect NOT reproduced, so this RED capture is blind", delay)
		}
		t.Logf("RED_MODE: defect reproduced — the endpoint at %s answers HTTP 200 for "+
			"/v1/models (after %s), yet the pre-fix flat 2s probe reported it UNREACHABLE. "+
			"Every live proof gated on that bool would have SKIPped, losing its coverage, "+
			"purely because the service was slow rather than absent.", srv.URL, delay)
		return
	}

	ok, why := helixLLMLocalReachable(t)
	if !ok {
		t.Fatalf("the classifying precondition reported the coder unreachable although it "+
			"answers HTTP 200 for /v1/models after %s: %s", delay, why)
	}
	if why != "" {
		t.Fatalf("a reachable verdict must carry no skip reason, got %q", why)
	}
	t.Logf("PASS: a service that answers 200 after %s is classified REACHABLE — the "+
		"run/skip decision no longer depends on how loaded the host is", delay)
}

// TestHelixLLMLocalReachable_DownIsFast is the other half of the D-3 claim, and
// the one that distinguishes this fix from "widen the timeout". A genuinely
// absent service must still be classified as absent IMMEDIATELY — the 30s
// ambiguity budget must never be spent on a connection that was definitively
// refused.
//
// If someone later "fixes" a flake here by turning the classifier back into a
// flat wait, this test fails — not because the elapsed time grows, but because
// the classifier would then report the AMBIGUOUS budget-expiry reason instead
// of the definitive "not reachable" one. The verdict keys on WHICH PATH ran,
// which is what the claim is actually about; timing it was only ever a proxy,
// and a load-sensitive one.
func TestHelixLLMLocalReachable_DownIsFast(t *testing.T) {
	// A server that is created and immediately closed leaves a port nothing is
	// listening on — the transport answers "connection refused", which is a
	// DEFINITIVE negative, not a timeout.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	closedURL := srv.URL
	srv.Close()

	t.Setenv(helixLLMLocalOpenAIEndpointEnv, closedURL)
	t.Setenv(helixLLMLocalProbeBudgetEnv, "30")

	start := time.Now()
	ok, why := helixLLMLocalReachable(t)
	elapsed := time.Since(start)

	if ok {
		t.Fatalf("classified a closed port at %s as reachable", closedURL)
	}
	if why == "" {
		t.Fatal("an unreachable verdict must state what was observed — an empty skip " +
			"reason is exactly the uninformative bool this fix removed")
	}
	// CAUSAL WITNESS, not a clock. The property under test is "a refused
	// connection takes the DEFINITIVE path and never spends the ambiguity
	// budget". The classifier already distinguishes those two paths by the
	// reason it returns, so assert on that directly:
	//
	//   definitive refusal -> "local HelixLLM coder not reachable at <ep> (<err>)"
	//   budget expiry      -> "... neither answered nor refused ... AMBIGUOUS,
	//                          not proven absent"
	//
	// The previous form asserted `elapsed > 2*time.Second`, which is the exact
	// wall-clock-as-proxy pattern this batch removes everywhere else (§11.4.50):
	// on a loaded host a single refused loopback dial can exceed 2s purely from
	// scheduler delay, so it FAILED on host state rather than on behaviour. The
	// witness below cannot be moved by load — a refused dial reports "not
	// reachable" whether it took 1ms or 10s.
	if !strings.Contains(why, "not reachable") {
		t.Fatalf("a refused connection must be classified by the DEFINITIVE path, "+
			"whose reason says %q; got: %s", "not reachable", why)
	}
	if strings.Contains(why, "AMBIGUOUS") {
		t.Fatalf("a definitively-refused connection consumed the ambiguity budget — "+
			"that budget must bound ONLY the timeout case; reason was: %s", why)
	}
	t.Logf("PASS: closed port classified unreachable in %s (budget was 30s, untouched) "+
		"with reason: %s", elapsed.Round(time.Millisecond), why)
}
