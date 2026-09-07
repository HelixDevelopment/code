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

package scaling

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// §1.1 paired-mutation meta-tests prove the scaling harness cannot bluff: each
// plants a degraded pool driver and asserts RunScaleSweep DETECTS it (the
// detection path is t.Fatalf, captured via a failTB stand-in).

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

func runWithFailTB(body func(tb testing.TB)) (failed bool, msg string) {
	f := &failTB{TB: &testing.T{}}
	func() {
		defer func() {
			if r := recover(); r != nil {
				if _, ok := r.(sentinelFatal); !ok {
					panic(r)
				}
			}
		}()
		body(f)
	}()
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.failed, f.msg
}

func isolatedEvidence(t *testing.T) {
	t.Helper()
	tmp := t.TempDir()
	old := os.Getenv("SCALING_EVIDENCE_ROOT")
	os.Setenv("SCALING_EVIDENCE_ROOT", tmp)
	t.Cleanup(func() { os.Setenv("SCALING_EVIDENCE_ROOT", old) })
}

// serialPoolDriver IGNORES added workers: regardless of n it serialises all assigns
// through a single lock, so throughput stays flat as N grows. This is the planted
// "pool that ignores added workers" defect (SP7-plan A2).
type serialPoolDriver struct {
	mu sync.Mutex
}

func (d *serialPoolDriver) SetupN(t testing.TB, n int) func() { return func() {} }
func (d *serialPoolDriver) ProcessTask(ctx context.Context) bool {
	// Holds a GLOBAL lock for the whole service window regardless of N — only one
	// task is ever in flight, so throughput is flat no matter how many workers are
	// "registered". This is the planted "pool that ignores added workers" defect.
	d.mu.Lock()
	defer d.mu.Unlock()
	busyWait(ServiceTime)
	return true
}
func (d *serialPoolDriver) Utilization() float64 { return 50 }

// degradingPoolDriver gets SLOWER as N grows (lock-convoy analogue): per-assign
// cost scales with the registered worker count, so throughput regresses.
type degradingPoolDriver struct {
	mu sync.Mutex
	n  int
}

func (d *degradingPoolDriver) SetupN(t testing.TB, n int) func() {
	d.mu.Lock()
	d.n = n
	d.mu.Unlock()
	return func() {}
}
func (d *degradingPoolDriver) ProcessTask(ctx context.Context) bool {
	d.mu.Lock()
	n := d.n
	d.mu.Unlock()
	// Per-task service window grows super-linearly with N (lock-convoy analogue) so
	// throughput regresses as N grows — the planted degradation defect.
	busyWait(time.Duration(n*n) * 40 * time.Microsecond)
	return true
}
func (d *degradingPoolDriver) Utilization() float64 { return 50 }

// TestMeta_RunScaleSweep_DetectsFlatThroughput plants a pool that ignores added
// workers and asserts the harness FAILS the scale-out gate.
func TestMeta_RunScaleSweep_DetectsFlatThroughput(t *testing.T) {
	isolatedEvidence(t)
	failed, _ := runWithFailTB(func(tb testing.TB) {
		RunScaleSweep(tb, "meta-flat", &serialPoolDriver{}, SweepConfig{
			NValues: []int{1, 2, 4, 8}, TasksPerStep: 240, Parallelism: 10,
		})
	})
	if !failed {
		t.Fatal("meta: RunScaleSweep did NOT detect flat throughput from a pool that ignores added workers — harness is a bluff")
	}
}

// TestMeta_RunScaleSweep_DetectsDegradation plants a pool whose throughput drops
// as N grows and asserts the monotonic-non-degradation assertion fails.
func TestMeta_RunScaleSweep_DetectsDegradation(t *testing.T) {
	isolatedEvidence(t)
	failed, _ := runWithFailTB(func(tb testing.TB) {
		RunScaleSweep(tb, "meta-degrade", &degradingPoolDriver{}, SweepConfig{
			NValues: []int{1, 2, 4, 8}, TasksPerStep: 240, Parallelism: 10,
		})
	})
	if !failed {
		t.Fatal("meta: RunScaleSweep did NOT detect throughput degradation as N grows — harness is a bluff")
	}
}

// TestMeta_RunScaleSweep_RejectsBelowFloor asserts the harness refuses a sweep
// below the §11.4.85 parallelism floor.
func TestMeta_RunScaleSweep_RejectsBelowFloor(t *testing.T) {
	isolatedEvidence(t)
	failed, _ := runWithFailTB(func(tb testing.TB) {
		RunScaleSweep(tb, "meta-floor", NewRealPoolDriver(), SweepConfig{
			NValues: []int{1, 2}, TasksPerStep: 100, Parallelism: 3,
		})
	})
	if !failed {
		t.Fatal("meta: RunScaleSweep accepted parallelism below §11.4.85 floor — harness is a bluff")
	}
}

// TestMeta_PositivePathWritesEvidence proves the real pool path writes a non-empty
// scaling_throughput.json (the artefact really exists, not a claimed PASS).
func TestMeta_PositivePathWritesEvidence(t *testing.T) {
	isolatedEvidence(t)
	rep := RunScaleSweep(t, "meta-positive", NewRealPoolDriver(), SweepConfig{
		NValues: []int{1, 2, 4, 8}, TasksPerStep: 320, Parallelism: 12,
	})
	if rep.GainAtMaxN < MinThroughputGainAtMaxN {
		t.Fatalf("meta: real pool gain %.2fx below floor", rep.GainAtMaxN)
	}
	path := filepath.Join(EvidenceRoot(), "meta-positive", "scaling_throughput.json")
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("meta: scaling_throughput.json not written: %v", err)
	}
	if info.Size() == 0 {
		t.Fatal("meta: scaling_throughput.json is empty — would be a hollow PASS")
	}
}
