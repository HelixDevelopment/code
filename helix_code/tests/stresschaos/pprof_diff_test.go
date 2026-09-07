// Build-tagged OUT of the default `go test ./...` unit pass.
//
// WHY (measured, not assumed): these suites generate load. A full-suite run was
// measured driving the host ephemeral port range to 100% occupancy (28,221 of
// 28,232 ports) held by ~29,600 TIME-WAIT sockets, for 60-90s at a time. While
// that window is open, ANY concurrently-running package that binds `:0` or
// `127.0.0.1:0` fails with EADDRINUSE — which is exactly how ~7 unrelated unit
// tests in internal/discovery, internal/tools/shell and internal/tools/web
// began failing in the suite while passing in isolation.
//
// The kernel arithmetic: 28,232 ports / 60s TIME_WAIT = ~470 socket
// close-cycles/sec before total saturation. `go test` defaults `-p` to NPROC
// (16 here), so the unit pass and these load suites were competing for that one
// budget. SO_REUSEADDR does not help (bind(0) treats every occupied port as a
// hard conflict) and tcp_tw_reuse is connect()-path only.
//
// This is NOT a removal (§11.4.122): the suites still run, via
//     make test-loadtest
// which runs them SERIALLY (-p 1) so they cannot flood each other either, and
// which is wired into `make test-complete`.
//
// Full evidence: docs/qa/2026-09-07-ephemeral-port-exhaustion/ANALYSIS.md
//go:build loadtest

package stresschaos

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
	"runtime"
	"runtime/pprof"
	"strconv"
	"strings"
	"testing"
	"time"
)

// This file is the HXC-144 anti-bluff oracle: a named-goroutine-stack diff
// (via runtime/pprof) that DEFINITIVELY distinguishes "net/http
// connection-teardown timing artifact" from "genuine application-code
// goroutine leak" — the disambiguation the original HXC-144 investigation
// could not resolve by static code reading alone.
//
// It reproduces the exact flood shape TestServer_Stress_ConcurrentDDoSFlood
// uses (internal/server/server_stress_test.go: 16 goroutines x 40 iterations,
// each real HTTP GET with its own *http.Client, round-robining across 5
// endpoints) against a self-contained httptest.NewServer — no external infra,
// no real internal/server, no Postgres/Redis — per the explicit no-infra
// constraint on this session.

// httpTransportPersistConnPattern matches goroutine stack frames belonging to
// net/http connection-lifecycle machinery (persistConn read/write loops,
// dialConn) — the class of goroutine the HXC-144 investigation identified as
// the likely source of the flagged "leak": asynchronous connection teardown,
// not an application-level leak.
var httpTransportPersistConnPattern = regexp.MustCompile(
	`net/http\.\(\*persistConn\)|net/http\.\(\*Transport\)\.(dialConn|readLoop|writeLoop)`)

// appLeakPattern matches goroutine stack frames rooted in this module's own
// production packages. It deliberately excludes tests/stresschaos itself
// (this harness legitimately runs its own goroutines — startGate fan-out,
// wg.Wait() pump — as part of the measurement, and those are not leaks).
var appLeakPattern = regexp.MustCompile(`dev\.helix\.code/(internal|cmd|applications)/`)

// goroutineDump is a named-goroutine-count snapshot: classification key ->
// count of currently-live goroutines whose stack matched that classification.
type goroutineDump map[string]int

// captureGoroutineDump takes a full named goroutine-stack dump via
// runtime/pprof (NOT just runtime.NumGoroutine()) and reduces it to a
// classification -> count map, so two dumps can be diffed by function
// identity instead of a bare integer, which cannot distinguish "N more
// net/http teardown goroutines" from "N more leaked application goroutines".
func captureGoroutineDump(t testing.TB) goroutineDump {
	t.Helper()
	var buf bytes.Buffer
	if err := pprof.Lookup("goroutine").WriteTo(&buf, 1); err != nil {
		t.Fatalf("stresschaos: pprof goroutine dump failed: %v", err)
	}
	return parseGoroutineDump(buf.String())
}

var (
	blockCountRe = regexp.MustCompile(`^(\d+) @`)
	frameRe      = regexp.MustCompile(`^#\s+0x[0-9a-fA-F]+\s+(\S+)\+0x`)
)

// parseGoroutineDump parses the runtime/pprof "goroutine" profile text format
// (debug=1): a header line, then repeated blocks of the shape
//
//	<N> @ 0xADDR 0xADDR ...
//	#	0xADDR	some/pkg.Func+0xNN	/path/file.go:LINE
//	#	0xADDR	some/pkg.Caller+0xNN	/path/file.go:LINE
//	...
//	<blank line>
//
// Each block represents N goroutines that share an identical stack.
func parseGoroutineDump(text string) goroutineDump {
	dump := goroutineDump{}
	var count int
	var frames []string
	flush := func() {
		if count == 0 || len(frames) == 0 {
			return
		}
		key := classifyFrames(frames)
		dump[key] += count
	}
	for _, line := range strings.Split(text, "\n") {
		if m := blockCountRe.FindStringSubmatch(line); m != nil {
			flush()
			count, _ = strconv.Atoi(m[1])
			frames = nil
			continue
		}
		if m := frameRe.FindStringSubmatch(line); m != nil {
			frames = append(frames, m[1])
		}
	}
	flush()
	return dump
}

// classifyFrames reduces one goroutine-block's full frame list to a single
// diagnostic classification key, in priority order:
//  1. an application-code frame (names the exact leaking site) -> "APP-LEAK:<frame>"
//  2. a net/http connection-teardown frame (the HXC-144 artifact class) -> "http-transport-teardown:<frame>"
//  3. otherwise the innermost (top) frame, as a catch-all bucket -> "other:<frame>"
func classifyFrames(frames []string) string {
	for _, f := range frames {
		if appLeakPattern.MatchString(f) {
			return "APP-LEAK:" + f
		}
	}
	for _, f := range frames {
		if httpTransportPersistConnPattern.MatchString(f) {
			return "http-transport-teardown:" + f
		}
	}
	if len(frames) > 0 {
		return "other:" + frames[0]
	}
	return "other:<empty>"
}

// diffGoroutineDumps returns, for every classification key whose count
// increased from before to after, the positive delta -- these are the
// goroutines that persisted (or were newly created and not yet reaped) across
// the measurement window.
func diffGoroutineDumps(before, after goroutineDump) map[string]int {
	diff := map[string]int{}
	for k, a := range after {
		if b := before[k]; a > b {
			diff[k] = a - b
		}
	}
	return diff
}

// TestGoroutineLeakOracle_HTTPFlood_PprofDiffDistinguishesArtifactFromLeak is
// the HXC-144 anti-bluff oracle. See file doc comment above for scope.
func TestGoroutineLeakOracle_HTTPFlood_PprofDiffDistinguishesArtifactFromLeak(t *testing.T) {
	endpoints := []string{
		"/health",
		"/api/v1/server/info",
		"/api/v1/metrics",
		"/api/v1/llm/providers",
		"/api/v1/memory/stats",
	}

	mux := http.NewServeMux()
	for _, ep := range endpoints {
		mux.HandleFunc(ep, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"status":"ok"}`))
		})
	}
	srv := httptest.NewServer(mux)
	defer srv.Close()

	runtime.GC()
	before := captureGoroutineDump(t)

	rep := RunConcurrent(t, "hxc144_pprof_diff_oracle", ConcurrencyConfig{
		Parallelism:            16,
		IterationsPerGoroutine: 40,
		Timeout:                60 * time.Second,
	}, func(g, iter int) error {
		// Each goroutine gets its own client (mirrors distinct attackers), same
		// as internal/server/server_stress_test.go's ConcurrentDDoSFlood, so we
		// genuinely exercise the shared http.DefaultTransport connection pool.
		client := &http.Client{Timeout: 10 * time.Second}
		ep := endpoints[(g+iter)%len(endpoints)]
		resp, err := client.Get(srv.URL + ep)
		if err != nil {
			return err
		}
		defer resp.Body.Close()
		_, _ = io.Copy(io.Discard, resp.Body)
		if resp.StatusCode != http.StatusOK {
			return fmt.Errorf("g=%d iter=%d %s -> %d", g, iter, ep, resp.StatusCode)
		}
		return nil
	})

	// RunConcurrent already settled (settleGoroutines()) before returning;
	// capture the after-dump now so it reflects the same settled point
	// RunConcurrent's own GoroutineDelta was computed from.
	after := captureGoroutineDump(t)
	diff := diffGoroutineDumps(before, after)

	t.Logf("HXC-144 oracle: RunConcurrent report gDelta=%d totalCalls=%d durMs=%.1f; pprof-diff residual classes=%d",
		rep.GoroutineDelta, rep.TotalCalls, rep.DurationMs, len(diff))

	var appLeaks, artifactClasses, otherClasses []string
	for k, n := range diff {
		switch {
		case strings.HasPrefix(k, "APP-LEAK:"):
			appLeaks = append(appLeaks, fmt.Sprintf("%s (x%d)", k, n))
		case strings.HasPrefix(k, "http-transport-teardown:"):
			artifactClasses = append(artifactClasses, fmt.Sprintf("%s (x%d)", k, n))
		default:
			otherClasses = append(otherClasses, fmt.Sprintf("%s (x%d)", k, n))
		}
	}

	if len(appLeaks) > 0 {
		t.Fatalf("HXC-144 REAL LEAK: %d application-code goroutine class(es) persisted past settle: %s",
			len(appLeaks), strings.Join(appLeaks, "; "))
	}

	if len(otherClasses) > 0 {
		// Not a dev.helix.code frame and not a recognised net/http teardown
		// frame either -- log plainly per §11.4.6 rather than silently
		// bucketing it as "fine". Not fatal: it is proven NOT to be
		// application code, but naming it keeps the finding honest.
		t.Logf("HXC-144 oracle: unclassified residual goroutine class(es) (not dev.helix.code, not recognised net/http teardown): %s",
			strings.Join(otherClasses, "; "))
	}

	switch {
	case len(artifactClasses) > 0:
		t.Logf("HXC-144 CONFIRMED ARTIFACT: residual goroutines are exclusively net/http connection-teardown frames, matching the investigation's diagnosis: %s",
			strings.Join(artifactClasses, "; "))
	case len(diff) == 0:
		t.Logf("HXC-144: zero residual goroutine classes after settle -- clean this run, no artifact signal at all")
	}
}
