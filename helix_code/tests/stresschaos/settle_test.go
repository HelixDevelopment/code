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
	"runtime"
	"sync"
	"testing"
	"time"
)

// TestSettle_PollUntilStable_WaitsOutDelayedGoroutineExit is the deterministic
// RED/GREEN regression guard for the HXC-144 settle-logic fix in
// settleGoroutines() / RunConcurrent (§11.4.115 polarity).
//
// RED (reproduced inline below, not by reverting production code): the
// original RunConcurrent settle step was a single
// `time.Sleep(50*time.Millisecond); runtime.GC()` sample. net/http
// client/server connection-teardown goroutines (persistConn.readLoop /
// writeLoop) exit ASYNCHRONOUSLY, independently of each other, and their exit
// is scheduler-timed, not synchronous with Close() — so goroutines that take
// longer than the fixed window to actually exit are still counted as
// "leaked" at snapshot time. This test parks goroutines so that all of them
// are provably present at the 50ms snapshot, then releases them to exit with
// STAGGERED delays (20ms..120ms after release, in 20ms steps — the same
// independently-timed-exit shape real persistConn teardown has, deliberately
// NOT a simultaneous burst, which would let any stable-streak poll coincide
// with the burst window), and proves the OLD single-fixed-sleep protocol counts
// every one of them as present at the 50ms mark (a false/flaky leak signal,
// exactly the HXC-144 measurement-window artifact class; see golang/go#25621,
// golang/go#9092).
//
// Determinism note (§11.4.50) — two wall-clock couplings removed, no assertion
// weakened:
//
//  1. The baseline is sampled with settleGoroutines() instead of a bare
//     runtime.GC(). The bare GC did not wait out asynchronous net/http
//     persistConn teardown left over from pprof_diff_test.go:142, which runs
//     immediately before this test in the same binary, so a contaminant could
//     exit mid-window and deflate the delta.
//  2. The planted goroutines are parked on a gate opened only AFTER the RED
//     snapshot. Previously they were released BEFORE the snapshot with their
//     smallest delay (60ms) beating the 50ms snapshot sleep by just 10ms;
//     time.Sleep guarantees a lower bound only, so on a CPU-saturated host the
//     snapshot sleep overshot and 2-3 planted goroutines exited before being
//     counted (measured: before=2 after=6 delta=4, before=2 after=5 delta=3,
//     failing 5/5 runs). `before` was a settled 2 throughout, which is what
//     proves the early exits were PLANTED goroutines, not baseline
//     contaminants.
//
// Both halves still assert exactly what they asserted before: RED that the old
// protocol reports delta >= the planted count, GREEN that settleGoroutines()
// brings the delta back within goroutineLeakTolerance. Neither threshold moved.
//
// GREEN: settleGoroutines() (the function RunConcurrent now uses) polls up to
// settlePollBudget and correctly waits out every staggered exit, reporting
// the TRUE post-settle count.
func TestSettle_PollUntilStable_WaitsOutDelayedGoroutineExit(t *testing.T) {
	// oldProtocolWindow is the single fixed sleep the pre-HXC-144 RunConcurrent
	// settle step used, reproduced verbatim in the RED half below.
	const oldProtocolWindow = 50 * time.Millisecond

	// Staggered so each goroutine's exit is independently observable — the
	// same shape real net/http connection teardown has (each persistConn exits
	// on its own schedule, not in lockstep). The delays are measured from the
	// release gate, which opens immediately after the RED snapshot, so they
	// describe the exit pattern GREEN's settleGoroutines() must wait out.
	delays := []time.Duration{
		20 * time.Millisecond,
		40 * time.Millisecond,
		60 * time.Millisecond,
		80 * time.Millisecond,
		100 * time.Millisecond,
		120 * time.Millisecond,
	}
	delayedExitGoroutines := len(delays)
	const stagger = 20 * time.Millisecond

	// Two fixture invariants that make the GREEN half meaningful, asserted
	// STATICALLY rather than left to a wall-clock race (§11.4.50):
	//
	//  (1) The stagger must be strictly smaller than settlePollInterval. Every
	//      poll interval therefore contains at least one exit while exits
	//      remain, so no run of settleStableStreak equal samples can form
	//      early — settleGoroutines() cannot "settle" before the last exit.
	//      This is precisely the property it is being tested for, and it holds
	//      independently of how far host load stretches the poll interval
	//      (stretching it only puts MORE exits inside each interval).
	//  (2) The longest delay must outlast oldProtocolWindow, so the exits GREEN
	//      waits out are genuinely ones the old single-fixed-sleep protocol
	//      would have missed.
	if stagger >= settlePollInterval {
		t.Fatalf("fixture self-inconsistent: stagger %v must be < settlePollInterval %v, otherwise a stable streak could form while exits are still pending and GREEN would prove nothing",
			stagger, settlePollInterval)
	}
	for i, d := range delays {
		if i > 0 && d-delays[i-1] != stagger {
			t.Fatalf("fixture self-inconsistent: delays must be evenly staggered by %v, got %v after %v", stagger, d, delays[i-1])
		}
	}
	if maxDelay := delays[len(delays)-1]; maxDelay <= oldProtocolWindow {
		t.Fatalf("fixture self-inconsistent: longest planted delay %v must outlast the old fixed window %v, otherwise GREEN is not waiting out anything the old protocol missed",
			maxDelay, oldProtocolWindow)
	}

	// Sample the baseline only after the process-global goroutine population
	// has genuinely SETTLED. A bare runtime.GC() does not wait out asynchronous
	// net/http persistConn readLoop/writeLoop teardown left over from earlier
	// tests in this same binary — pprof_diff_test.go:142
	// (TestGoroutineLeakOracle_HTTPFlood_...) drives 16x40=640 requests through
	// the shared http.DefaultTransport and runs immediately before this test in
	// file order. Any such leftover that exits DURING the measurement window
	// deflates the observed delta below the planted count. settleGoroutines()
	// (stresschaos.go) closes idle HTTP connections and polls until the count is
	// stable, so `before` is a real floor rather than a mid-teardown sample.
	before := settleGoroutines()

	// releaseAfterRedSnapshot parks every planted goroutine until the RED
	// snapshot has been taken. A parked goroutine is still counted by
	// runtime.NumGoroutine, so all six are present at the snapshot BY
	// CONSTRUCTION rather than by winning a race.
	//
	// This replaces a release-before-the-snapshot form whose smallest planted
	// delay (60ms) beat the 50ms snapshot sleep by only 10ms. time.Sleep
	// guarantees a lower bound only, so under host load that sleep overshot and
	// planted goroutines exited before being counted — MEASURED on a
	// CPU-saturated 64-core host as before=2 after=6 delta=4 and before=2
	// after=5 delta=3 (i.e. 2 and 3 planted goroutines gone early), failing 5/5
	// runs. Note `before` was a settled 2 in every one of those runs, which is
	// what identifies the early exits as PLANTED goroutines rather than
	// baseline contaminants.
	releaseAfterRedSnapshot := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(delayedExitGoroutines)
	for _, d := range delays {
		d := d
		go func() {
			defer wg.Done()
			<-releaseAfterRedSnapshot
			time.Sleep(d) // mimics asynchronous, independently-timed persistConn teardown
		}()
	}

	// --- RED: reproduce the exact OLD RunConcurrent settle protocol inline.
	// Every planted goroutine is parked on releaseAfterRedSnapshot, so the old
	// protocol necessarily counts all of them as present — which is exactly the
	// false "leak" signal it produced for real persistConn teardown that had not
	// finished inside its fixed window. ---
	time.Sleep(oldProtocolWindow)
	runtime.GC()
	oldProtocolAfter := runtime.NumGoroutine()
	oldProtocolDelta := oldProtocolAfter - before
	t.Logf("RED (old fixed-50ms-sleep protocol): before=%d after=%d delta=%d",
		before, oldProtocolAfter, oldProtocolDelta)
	if oldProtocolDelta < delayedExitGoroutines {
		t.Fatalf("RED reproduction failed to reproduce the HXC-144 artifact: expected the old fixed-50ms-sleep protocol to still count all %d staggered-exit (%v..%v) goroutines as present (delta>=%d), got delta=%d — the old-protocol simulation itself is broken, not the phenomenon it's meant to demonstrate",
			delayedExitGoroutines, delays[0], delays[len(delays)-1], delayedExitGoroutines, oldProtocolDelta)
	}
	if oldProtocolDelta > goroutineLeakTolerance {
		t.Logf("RED CONFIRMED: old fixed-sleep protocol's delta (%d) exceeds goroutineLeakTolerance (%d) — this is the exact false-leak signal HXC-144 diagnosed",
			oldProtocolDelta, goroutineLeakTolerance)
	}

	// Open the gate: from here the six goroutines exit asynchronously at
	// 20ms..120ms in 20ms steps — deliberately NOT a simultaneous burst, and
	// staggered more finely than settlePollInterval so settleGoroutines() below
	// is forced to keep polling until the last one is gone.
	close(releaseAfterRedSnapshot)

	// --- GREEN: the current (fixed) poll-until-stable settle logic ---
	stableAfter := settleGoroutines()
	wg.Wait() // the planted goroutines must have already exited by now; this is just a safety net
	stableDelta := stableAfter - before
	t.Logf("GREEN (poll-until-stable settle): before=%d after=%d delta=%d",
		before, stableAfter, stableDelta)

	if stableDelta > goroutineLeakTolerance {
		t.Fatalf("settleGoroutines() did not wait out the planted staggered-exit (%v..%v) goroutines: before=%d after=%d delta=%d > tolerance %d",
			delays[0], delays[len(delays)-1], before, stableAfter, stableDelta, goroutineLeakTolerance)
	}
}
