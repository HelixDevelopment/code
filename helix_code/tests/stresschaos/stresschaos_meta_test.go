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
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// The §1.1 paired-mutation meta-tests prove the harness itself cannot bluff:
// each one plants a known violation (a deadlocking fn, a leaking fn, a crashing
// op, a high error rate) and asserts the harness DETECTS it. The detection path
// in production helpers is `t.Fatalf`, so these meta-tests drive the helpers with
// a *capturing* testing.TB stand-in (failTB) and assert it recorded a failure.

// failTB is a minimal testing.TB that records Fatalf/Errorf instead of aborting,
// so a meta-test can assert "the helper would have failed a real test here".
type failTB struct {
	testing.TB
	mu     sync.Mutex
	failed bool
	msg    string
}

func (f *failTB) Helper() {}

func (f *failTB) Fatalf(format string, args ...interface{}) {
	f.mu.Lock()
	f.failed = true
	f.msg = format
	f.mu.Unlock()
	// Unlike the real testing.TB, do NOT call runtime.Goexit — raise a sentinel
	// panic the runner swallows, so the helper unwinds and we can inspect state.
	panic(sentinelFatal{})
}

func (f *failTB) Errorf(format string, args ...interface{}) {
	f.mu.Lock()
	f.failed = true
	f.msg = format
	f.mu.Unlock()
}

func (f *failTB) Logf(format string, args ...interface{}) {}

type sentinelFatal struct{}

// runWithFailTB runs body with a failTB, swallowing the sentinel panic that the
// fake Fatalf raises, and reports whether the helper signalled failure.
func runWithFailTB(body func(tb testing.TB)) (failed bool, msg string) {
	f := &failTB{TB: &testing.T{}}
	func() {
		defer func() {
			if r := recover(); r != nil {
				if _, ok := r.(sentinelFatal); !ok {
					panic(r) // a real panic — re-raise
				}
			}
		}()
		body(f)
	}()
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.failed, f.msg
}

// isolatedEvidence points the harness at a throwaway temp dir so meta-test
// artefacts never pollute the real qa-results tree.
func isolatedEvidence(t *testing.T) {
	t.Helper()
	tmp := t.TempDir()
	old := os.Getenv("STRESSCHAOS_EVIDENCE_ROOT")
	os.Setenv("STRESSCHAOS_EVIDENCE_ROOT", tmp)
	t.Cleanup(func() { os.Setenv("STRESSCHAOS_EVIDENCE_ROOT", old) })
}

// TestMeta_RunConcurrent_DetectsDeadlock plants a deadlocking fn and asserts the
// concurrency harness reports a deadlock (proving the timeout guard is real).
func TestMeta_RunConcurrent_DetectsDeadlock(t *testing.T) {
	isolatedEvidence(t)
	block := make(chan struct{}) // never closed -> permanent block
	failed, _ := runWithFailTB(func(tb testing.TB) {
		RunConcurrent(tb, "meta-deadlock", ConcurrencyConfig{
			Parallelism:            MinParallelism,
			IterationsPerGoroutine: 1,
			Timeout:                500 * time.Millisecond,
		}, func(g, it int) error {
			<-block // deadlock: blocks forever
			return nil
		})
	})
	if !failed {
		t.Fatal("meta: RunConcurrent did NOT detect the planted deadlock — harness is a bluff")
	}
	close(block) // release the parked goroutines so the test process can exit
}

// TestMeta_RunConcurrent_DetectsGoroutineLeak plants a fn that spawns a leaking
// goroutine per call and asserts the harness reports a goroutine leak.
func TestMeta_RunConcurrent_DetectsGoroutineLeak(t *testing.T) {
	isolatedEvidence(t)
	leakHold := make(chan struct{})
	failed, _ := runWithFailTB(func(tb testing.TB) {
		RunConcurrent(tb, "meta-leak", ConcurrencyConfig{
			Parallelism:            MinParallelism,
			IterationsPerGoroutine: 5,
			Timeout:                10 * time.Second,
		}, func(g, it int) error {
			go func() { <-leakHold }() // leak a goroutine that never returns
			return nil
		})
	})
	if !failed {
		t.Fatal("meta: RunConcurrent did NOT detect the planted goroutine leak — harness is a bluff")
	}
	close(leakHold) // clean up leaked goroutines
}

// TestMeta_RunSustainedLoad_DetectsHighErrorRate plants an always-failing fn and
// asserts the sustained-load harness fails on the error rate.
func TestMeta_RunSustainedLoad_DetectsHighErrorRate(t *testing.T) {
	isolatedEvidence(t)
	failed, _ := runWithFailTB(func(tb testing.TB) {
		RunSustainedLoad(tb, "meta-errrate", SustainedConfig{N: MinSustainedN, MaxErrorRate: 0.01},
			func(i int) error { return errors.New("planted failure") })
	})
	if !failed {
		t.Fatal("meta: RunSustainedLoad did NOT detect the planted error rate — harness is a bluff")
	}
}

// TestMeta_RunSustainedLoad_RejectsBelowFloor asserts the harness refuses a run
// below the §11.4.85 N>=100 floor (so callers cannot quietly under-test).
func TestMeta_RunSustainedLoad_RejectsBelowFloor(t *testing.T) {
	isolatedEvidence(t)
	failed, _ := runWithFailTB(func(tb testing.TB) {
		RunSustainedLoad(tb, "meta-floor", SustainedConfig{N: 10},
			func(i int) error { return nil })
	})
	if !failed {
		t.Fatal("meta: RunSustainedLoad accepted N below §11.4.85 floor — harness is a bluff")
	}
}

// TestMeta_ChaosKillDuring_DetectsPanic plants an op that panics under the fault
// and asserts the chaos recorder classifies it Fatal and fails.
func TestMeta_ChaosKillDuring_DetectsPanic(t *testing.T) {
	isolatedEvidence(t)
	failed, _ := runWithFailTB(func(tb testing.TB) {
		ChaosKillDuring(tb, "meta-chaos-panic", 50*time.Millisecond,
			func(ctx context.Context, rec *ChaosRecorder) {
				panic("planted crash under chaos")
			})
	})
	if !failed {
		t.Fatal("meta: ChaosKillDuring did NOT detect the planted panic — harness is a bluff")
	}
}

// TestMeta_ChaosCorruptInputDuring_DetectsPanic asserts a feeder that panics on
// corrupt input is classified Fatal.
func TestMeta_ChaosCorruptInputDuring_DetectsPanic(t *testing.T) {
	isolatedEvidence(t)
	failed, _ := runWithFailTB(func(tb testing.TB) {
		ChaosCorruptInputDuring(tb, "meta-chaos-input", [][]byte{{0xff, 0x00}},
			func(in []byte) error { panic("planted crash on corrupt input") })
	})
	if !failed {
		t.Fatal("meta: ChaosCorruptInputDuring did NOT detect the planted panic — harness is a bluff")
	}
}

// TestMeta_PositivePathWritesEvidence proves the happy path actually writes a
// non-empty latency.json (the artefact really exists, not a claimed PASS).
func TestMeta_PositivePathWritesEvidence(t *testing.T) {
	isolatedEvidence(t)
	rep := RunSustainedLoad(t, "meta-positive", SustainedConfig{N: MinSustainedN},
		func(i int) error { return nil })
	if rep.N < MinSustainedN {
		t.Fatalf("meta: expected N>=%d, got %d", MinSustainedN, rep.N)
	}
	path := filepath.Join(EvidenceRoot(), "meta-positive", "latency.json")
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("meta: latency.json not written: %v", err)
	}
	if info.Size() == 0 {
		t.Fatal("meta: latency.json is empty — would be a hollow PASS")
	}
}
