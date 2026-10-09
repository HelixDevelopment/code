package memory

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestConfig holds the configuration for memory tests
type TestConfig struct {
	BaseURL     string
	AdminToken  string
	Timeout     time.Duration
	Iterations  int
	Concurrency int
}

// getTestBaseURL returns the HelixCode server URL to test against, honoring
// the HELIXCODE_TEST_URL override (same convention as tests/security and
// tests/e2e/test_bank) so the suite can be pointed at a real running server.
func getTestBaseURL() string {
	if baseURL := os.Getenv("HELIXCODE_TEST_URL"); baseURL != "" {
		return baseURL
	}
	return "http://localhost:8080"
}

// DefaultTestConfig returns a default test configuration
func DefaultTestConfig() *TestConfig {
	return &TestConfig{
		BaseURL:     getTestBaseURL(),
		AdminToken:  "test-admin-token",
		Timeout:     30 * time.Second,
		Iterations:  100,
		Concurrency: 10,
	}
}

// MemoryStats captures memory statistics at a point in time
type MemoryStats struct {
	Alloc      uint64    // bytes allocated and in use
	TotalAlloc uint64    // bytes allocated (even if freed)
	Sys        uint64    // bytes obtained from system
	NumGC      uint32    // number of GC runs
	HeapAlloc  uint64    // bytes allocated and in heap
	HeapSys    uint64    // bytes obtained from system for heap
	HeapIdle   uint64    // bytes in idle spans
	HeapInuse  uint64    // bytes in in-use spans
	StackInuse uint64    // bytes in stack spans
	Timestamp  time.Time // when stats were captured
}

// CaptureMemoryStats captures current memory statistics
func CaptureMemoryStats() *MemoryStats {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	return &MemoryStats{
		Alloc:      m.Alloc,
		TotalAlloc: m.TotalAlloc,
		Sys:        m.Sys,
		NumGC:      m.NumGC,
		HeapAlloc:  m.HeapAlloc,
		HeapSys:    m.HeapSys,
		HeapIdle:   m.HeapIdle,
		HeapInuse:  m.HeapInuse,
		StackInuse: m.StackInuse,
		Timestamp:  time.Now(),
	}
}

// MemoryDelta calculates the difference between two memory stats
type MemoryDelta struct {
	AllocDelta      int64
	TotalAllocDelta uint64
	HeapAllocDelta  int64
	GCRuns          uint32
	Duration        time.Duration
}

// CalculateDelta calculates the difference between before and after stats
func CalculateDelta(before, after *MemoryStats) *MemoryDelta {
	return &MemoryDelta{
		AllocDelta:      int64(after.Alloc) - int64(before.Alloc),
		TotalAllocDelta: after.TotalAlloc - before.TotalAlloc,
		HeapAllocDelta:  int64(after.HeapAlloc) - int64(before.HeapAlloc),
		GCRuns:          after.NumGC - before.NumGC,
		Duration:        after.Timestamp.Sub(before.Timestamp),
	}
}

// heapTrendSignal fits a least-squares line to the time-ordered live-heap samples
// and returns a spike-robust leak signal: the coefficient of determination R²
// (fraction of the sample variance explained by a straight-line trend, 0..1)
// SIGNED by the slope direction. It also returns the slope (bytes/sample) and the
// total rise the line predicts across the window (slope*(n-1) bytes) for logging.
//
//   - signedR2 ≈ 0   → the variance is NOISE, not a trend: bounded steady-state,
//     no matter how violently individual post-GC snapshots swing
//     (e.g. 6 MB..142 MB). This is the bounded-heap case.
//   - signedR2 → +1  → a CONSISTENT upward climb explains the variance: a leak.
//   - signedR2 < 0   → a consistent downward trend (heap being reclaimed): bounded.
//
// Why signed-R² rather than a ratio of half-means (the previous, flaky approach):
// post-GC HeapAlloc readings oscillate by 20x on a perfectly bounded run, so any
// statistic built from a few raw point-snapshots (half-mean ratio, single delta)
// crosses a fixed band by CHANCE depending on where the spikes land — it is
// non-deterministic (§11.4.50). R² instead asks "is the variance a trend or is it
// noise?" using ALL points: pure noise → R²≈0 regardless of spike magnitude; a
// real climb → R²→1. It is dimensionless and independent of the host's absolute
// heap size, GOGC, or scavenger timing (§11.4.6: signal-vs-noise, not a hardcoded
// MB band). Empirically bounded-noisy data scores ~0.00, a slow leak buried in big
// jitter still scores ~0.5, and a clean ramp scores ~1.0 — a wide, deterministic
// separation around the 0.5 bound.
func heapTrendSignal(samples []uint64) (signedR2, slope, rise float64) {
	n := len(samples)
	if n < 2 {
		return 0, 0, 0
	}
	var sumX, sumY float64
	xs := make([]float64, n)
	ys := make([]float64, n)
	for i, v := range samples {
		xs[i] = float64(i)
		ys[i] = float64(v)
		sumX += xs[i]
		sumY += ys[i]
	}
	meanX := sumX / float64(n)
	meanY := sumY / float64(n)

	var covXY, varX, varY float64
	for i := 0; i < n; i++ {
		dx := xs[i] - meanX
		dy := ys[i] - meanY
		covXY += dx * dy
		varX += dx * dx
		varY += dy * dy
	}
	if varX == 0 {
		return 0, 0, 0
	}
	slope = covXY / varX
	rise = slope * float64(n-1)

	if varY == 0 {
		// Perfectly flat samples: zero variance, hence zero trend signal.
		return 0, slope, rise
	}
	r2 := (covXY * covXY) / (varX * varY) // 0..1, fraction of variance explained
	signedR2 = math.Copysign(r2, slope)   // carry the slope direction
	return signedR2, slope, rise
}

// heapLeakSignalBound is the signed-R² threshold at and above which heapTrendSignal
// flags a live-heap upward trend as a possible leak. Below it the variance is noise
// (bounded steady-state). Asserted identically here and in the GC-pressure test.
const heapLeakSignalBound = 0.5

// heapLeakMinRiseFraction guards against R²'s scale-invariance on LIGHT finite
// workloads. R² answers "is the variance a trend or noise?" — but on a tiny,
// near-flat sub-MB heap a few-KB consistent creep explains a high fraction of the
// (tiny) variance and scores a high R² even though the heap is, in absolute terms,
// dead flat (observed e.g. a ~2.7 KB rise on a ~620 KB heap scoring R²≈0.87). That
// is bounded steady-state, NOT a leak. The GC-pressure test was originally exempted
// from this guard on the premise that its workload produced tens-of-MB samples;
// measured on this tree its samples are ~1 MB and it hit exactly this false positive
// (signed-R² 0.6182 on a 12%-of-mean drift), so it now applies the same guarded
// predicate. The leak verdict therefore requires BOTH conditions: a consistent
// upward trend (signedR2 >= heapLeakSignalBound) AND a MATERIAL magnitude — the
// least-squares line's predicted rise across the window is at least this fraction
// of the mean live heap. A real leak's rise is ~1x..2x the mean (the live set
// roughly doubles+ across the window); bounded steady-state creep is a tiny
// fraction. 0.25 (25% growth) sits in the wide deterministic gap between the two
// (observed false-positive creeps were <6%; the self-validation leak series rise
// ~1.8x their mean). §11.4.6: signal-vs-noise AND magnitude, not a hardcoded MB band.
const heapLeakMinRiseFraction = 0.25

// heapTrendIsLeak reports whether a time-ordered post-GC live-heap series shows a
// genuine leak: a consistent upward trend (signedR2 >= heapLeakSignalBound) that is
// ALSO materially large (predicted rise >= heapLeakMinRiseFraction of the mean live
// heap). It reuses heapTrendSignal for the trend statistic — it does NOT reinvent
// it — and adds only the scale guard the light finite workloads need. Returns the
// signal/slope/rise and the rise-as-fraction-of-mean for logging. A series too short
// to fit a trend (n<4) is never a leak here (callers use the gross-runaway backstop).
func heapTrendIsLeak(samples []uint64) (isLeak bool, signedR2, slope, rise, riseFraction float64) {
	if len(samples) < 4 {
		return false, 0, 0, 0, 0
	}
	signedR2, slope, rise = heapTrendSignal(samples)
	var sum float64
	for _, v := range samples {
		sum += float64(v)
	}
	mean := sum / float64(len(samples))
	if mean > 0 {
		riseFraction = rise / mean
	}
	isLeak = signedR2 >= heapLeakSignalBound && riseFraction >= heapLeakMinRiseFraction
	return isLeak, signedR2, slope, rise, riseFraction
}

// TestHeapTrend_FlagsMonotonicLeak is the SELF-VALIDATION (§1.1 / §11.4.107(10))
// for the heapTrendSignal analyzer used by the GC-pressure test. It proves the
// statistic, deterministically:
//
//	(a) PASSES (signal < bound) for a bounded but VERY noisy steady-state series —
//	    the exact pattern that defeated the previous ratio-of-half-means invariant
//	    (post-GC snapshots swinging ~6 MB..142 MB);
//	(b) FAILS (signal >= bound) for synthetic monotonically-climbing live heaps —
//	    real leaks, including a slow leak buried in heavy jitter.
//
// If either polarity flips, the production invariant is bluffing and this test
// FAILS, so the analyzer provably cannot silently pass a leak or fail bounded load.
func TestHeapTrend_FlagsMonotonicLeak(t *testing.T) {
	// (a) Golden-GOOD: bounded steady-state with violent oscillation, no trend.
	// Mirrors a real captured run ([16M,66M,65M,65M,120M,142M,6M,55M,107M]-style):
	// huge spikes, but the live set is reclaimed and does NOT climb across time.
	boundedNoisy := []uint64{
		16_000_000, 66_000_000, 65_000_000, 65_000_000, 120_000_000,
		142_000_000, 6_000_000, 55_000_000, 107_000_000, 30_000_000,
		95_000_000, 12_000_000, 88_000_000, 40_000_000,
	}
	goodSignal, _, _ := heapTrendSignal(boundedNoisy)
	t.Logf("bounded-noisy series signed-R2 = %.4f (must be < %.2f)", goodSignal, heapLeakSignalBound)
	assert.Less(t, goodSignal, heapLeakSignalBound,
		"analyzer FALSE-POSITIVE: flagged bounded noisy steady-state as a leak (signal %.4f)", goodSignal)

	// (b) Golden-BAD: a real leak — live heap climbs monotonically with modest
	// jitter (the live set is retained and grows, GC cannot reclaim).
	monotonicLeak := []uint64{
		10_000_000, 18_000_000, 31_000_000, 42_000_000, 55_000_000,
		63_000_000, 78_000_000, 89_000_000, 101_000_000, 115_000_000,
		122_000_000, 138_000_000, 149_000_000, 161_000_000,
	}
	badSignal, _, _ := heapTrendSignal(monotonicLeak)
	t.Logf("monotonic-leak series signed-R2 = %.4f (must be >= %.2f)", badSignal, heapLeakSignalBound)
	assert.GreaterOrEqual(t, badSignal, heapLeakSignalBound,
		"analyzer FALSE-NEGATIVE: failed to flag a monotonic leak (signal %.4f)", badSignal)

	// (b2) Golden-BAD: a SLOW leak buried in heavy jitter — the hard case. It
	// climbs across the window but every other sample dips, so a half-mean ratio
	// would miss it; signed-R² still recognises the trend.
	slowNoisyLeak := []uint64{
		20_000_000, 80_000_000, 30_000_000, 90_000_000, 40_000_000,
		100_000_000, 55_000_000, 115_000_000, 70_000_000, 130_000_000,
		85_000_000, 150_000_000,
	}
	slowSignal, _, _ := heapTrendSignal(slowNoisyLeak)
	t.Logf("slow-noisy-leak series signed-R2 = %.4f (must be >= %.2f)", slowSignal, heapLeakSignalBound)
	assert.GreaterOrEqual(t, slowSignal, heapLeakSignalBound,
		"analyzer FALSE-NEGATIVE: failed to flag a slow leak buried in jitter (signal %.4f)", slowSignal)

	// (b3) A downward trend (heap reclaimed) must NOT be flagged (signal < 0).
	declining := []uint64{
		160_000_000, 140_000_000, 120_000_000, 100_000_000,
		80_000_000, 60_000_000, 40_000_000, 20_000_000,
	}
	declineSignal, _, _ := heapTrendSignal(declining)
	t.Logf("declining series signed-R2 = %.4f (must be < %.2f)", declineSignal, heapLeakSignalBound)
	assert.Less(t, declineSignal, heapLeakSignalBound,
		"analyzer FALSE-POSITIVE: flagged a declining heap as a leak (signal %.4f)", declineSignal)
}

// TestHeapTrendIsLeak_RequiresTrendAndMagnitude is the SELF-VALIDATION (§1.1 /
// §11.4.107(10)) for heapTrendIsLeak — the trend+magnitude verdict used by the
// finite leak-detection tests. It proves, deterministically, that the magnitude
// guard added on top of heapTrendSignal does NOT bluff in either direction:
//
//	(a) a tiny, near-flat sub-MB heap with a faint-but-consistent few-KB creep —
//	    which scores a HIGH R² purely from R²'s scale-invariance — is NOT a leak
//	    (this is the exact false-positive the light finite workloads produced);
//	(b) a real monotonic leak (live set roughly doubles+ across the window) IS a
//	    leak (high R² AND material rise), including a slow leak buried in jitter;
//	(c) a declining heap is never a leak.
//
// If any polarity flips, the finite-workload invariant is bluffing and this test
// FAILS, so the verdict provably cannot silently pass a leak or fail bounded load.
func TestHeapTrendIsLeak_RequiresTrendAndMagnitude(t *testing.T) {
	// (a) Golden-GOOD: a real captured false-positive — ~620 KB live heap, a
	// consistent few-KB creep across the window. signedR2 is high (~0.87) BUT the
	// rise (~2.5 KB) is a negligible fraction of the mean, so it is NOT a leak.
	tinyFlatCreep := []uint64{
		619576, 619608, 619784, 620312, 619944,
		620952, 620728, 621880, 622264, 621768,
	}
	leak, sig, _, rise, frac := heapTrendIsLeak(tinyFlatCreep)
	t.Logf("tiny-flat-creep: signed-R2=%.4f rise=%.0fB riseFraction=%.4f (must NOT be a leak)", sig, rise, frac)
	assert.False(t, leak,
		"verdict FALSE-POSITIVE: flagged a negligible %.0f-byte creep (%.2f%% of mean) as a leak despite high R²=%.4f",
		rise, frac*100, sig)

	// (b) Golden-BAD: a real leak — live heap climbs monotonically and MATERIALLY
	// (the live set more than doubles across the window).
	monotonicLeak := []uint64{
		10_000_000, 18_000_000, 31_000_000, 42_000_000, 55_000_000,
		63_000_000, 78_000_000, 89_000_000, 101_000_000, 115_000_000,
		122_000_000, 138_000_000, 149_000_000, 161_000_000,
	}
	leak, sig, _, rise, frac = heapTrendIsLeak(monotonicLeak)
	t.Logf("monotonic-leak: signed-R2=%.4f rise=%.0fB riseFraction=%.4f (must BE a leak)", sig, rise, frac)
	assert.True(t, leak,
		"verdict FALSE-NEGATIVE: failed to flag a material monotonic leak (R²=%.4f, rise %.2f%% of mean)", sig, frac*100)

	// (b2) Golden-BAD: a slow leak buried in heavy jitter — climbs materially
	// across the window though every other sample dips.
	slowNoisyLeak := []uint64{
		20_000_000, 80_000_000, 30_000_000, 90_000_000, 40_000_000,
		100_000_000, 55_000_000, 115_000_000, 70_000_000, 130_000_000,
		85_000_000, 150_000_000,
	}
	leak, sig, _, rise, frac = heapTrendIsLeak(slowNoisyLeak)
	t.Logf("slow-noisy-leak: signed-R2=%.4f rise=%.0fB riseFraction=%.4f (must BE a leak)", sig, rise, frac)
	assert.True(t, leak,
		"verdict FALSE-NEGATIVE: failed to flag a material slow leak buried in jitter (R²=%.4f, rise %.2f%% of mean)", sig, frac*100)

	// (c) A bounded, violently-noisy steady-state (the GC-pressure pattern) — huge
	// spikes but no net climb — is NOT a leak.
	boundedNoisy := []uint64{
		16_000_000, 66_000_000, 65_000_000, 65_000_000, 120_000_000,
		142_000_000, 6_000_000, 55_000_000, 107_000_000, 30_000_000,
		95_000_000, 12_000_000, 88_000_000, 40_000_000,
	}
	leak, sig, _, rise, frac = heapTrendIsLeak(boundedNoisy)
	t.Logf("bounded-noisy: signed-R2=%.4f rise=%.0fB riseFraction=%.4f (must NOT be a leak)", sig, rise, frac)
	assert.False(t, leak,
		"verdict FALSE-POSITIVE: flagged bounded noisy steady-state as a leak (R²=%.4f, rise %.2f%% of mean)", sig, frac*100)

	// (d) A declining heap (reclaimed) is never a leak.
	declining := []uint64{
		160_000_000, 140_000_000, 120_000_000, 100_000_000,
		80_000_000, 60_000_000, 40_000_000, 20_000_000,
	}
	leak, sig, _, _, frac = heapTrendIsLeak(declining)
	t.Logf("declining: signed-R2=%.4f riseFraction=%.4f (must NOT be a leak)", sig, frac)
	assert.False(t, leak, "verdict FALSE-POSITIVE: flagged a declining heap as a leak (R²=%.4f)", sig)
}

// =============================================================================
// Memory Leak Detection Tests
// =============================================================================

// TestMemory_LeakDetection_RepeatedRequests tests for memory leaks during repeated API requests
func TestMemory_LeakDetection_RepeatedRequests(t *testing.T) {
	config := DefaultTestConfig()
	client := &http.Client{Timeout: config.Timeout}

	// Skip if server is not available
	resp, err := client.Get(config.BaseURL + "/health")
	if err != nil {
		t.Skip("Server not available, skipping memory leak test") // SKIP-OK: #legacy-untriaged
	}
	resp.Body.Close()

	// Force GC before test to get clean baseline
	runtime.GC()
	runtime.GC()

	beforeStats := CaptureMemoryStats()

	// Perform many repeated requests, SEGMENTED so we can sample the live
	// (post-GC) heap as the workload progresses. A genuine leak manifests as a
	// monotonically growing live-heap baseline across the segments; bounded
	// steady-state settles to a roughly flat (noisy) baseline.
	//
	// Anti-bluff (§11.4.6 / §11.4.50): the old invariant asserted a single
	// before/after post-GC HeapAllocDelta against a hardcoded 10 MB cap. Post-GC
	// HeapAlloc snapshots oscillate wildly (observed 6 MB..142 MB on the SAME
	// bounded run as the scavenger reclaims spans), so a single delta crosses any
	// fixed band by CHANCE — it is non-deterministic. The real invariant is "the
	// live heap does not grow without bound", captured here as a spike-robust
	// signed-R² trend over multi-sample data via heapTrendSignal (same statistic
	// + bound the GC-pressure test uses; self-validated by
	// TestHeapTrend_FlagsMonotonicLeak). We segment a finite workload (rather than
	// use a wall-clock ticker) so the sample count is deterministic regardless of
	// host speed — no flaky "too few samples" race.
	iterations := config.Iterations * 10
	const segments = 10
	segSize := iterations / segments
	if segSize < 1 {
		segSize = 1
	}
	var heapSamples []uint64
	done := 0
	for s := 0; s < segments && done < iterations; s++ {
		end := done + segSize
		if s == segments-1 || end > iterations {
			end = iterations
		}
		for i := done; i < end; i++ {
			resp, err := client.Get(config.BaseURL + "/health")
			if err != nil {
				continue
			}
			io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
		}
		done = end
		runtime.GC()
		heapSamples = append(heapSamples, CaptureMemoryStats().HeapAlloc)
	}

	// Force GC after test
	runtime.GC()
	runtime.GC()

	afterStats := CaptureMemoryStats()
	delta := CalculateDelta(beforeStats, afterStats)

	// Log memory statistics
	t.Logf("Memory delta after %d requests:", iterations)
	t.Logf("  Alloc delta: %d bytes", delta.AllocDelta)
	t.Logf("  Heap alloc delta (point snapshot): %d bytes", delta.HeapAllocDelta)
	t.Logf("  Total allocated: %d bytes", delta.TotalAllocDelta)
	t.Logf("  GC runs: %d", delta.GCRuns)
	t.Logf("  Live-heap samples (post-GC, bytes): %v", heapSamples)

	// REAL INVARIANT: the live heap must stay BOUNDED across the run — no
	// unbounded monotonic growth. Spike-robust trend statistic, not a noisy
	// point-snapshot cap. See heapTrendSignal + TestHeapTrend_FlagsMonotonicLeak.
	if len(heapSamples) >= 4 {
		isLeak, signedR2, slope, rise, frac := heapTrendIsLeak(heapSamples)
		t.Logf("  Live-heap trend signal (signed-R2): %.4f (leak if >= %.2f, slope %.0f B/sample, rise %.0f B = %.2f%% of mean)",
			signedR2, heapLeakSignalBound, slope, rise, frac*100)
		assert.False(t, isLeak,
			"Potential memory leak: live heap shows a consistent, material upward trend across %d requests (signed-R2 %.4f >= %.2f AND rise %.0f bytes = %.2f%% of mean >= %.0f%%)",
			iterations, signedR2, heapLeakSignalBound, rise, frac*100, heapLeakMinRiseFraction*100)
	} else {
		// Too few samples to fit a trend (tiny Iterations). Generous gross-runaway
		// backstop sized well above observed bounded steady-state — only fires on
		// a gross runaway, never on normal host variance. Coarse, NOT the primary.
		const grossRunawayCeiling = int64(512 * 1024 * 1024)
		assert.Less(t, delta.HeapAllocDelta, grossRunawayCeiling,
			"Heap delta %d bytes exceeds gross-runaway sanity ceiling after %d requests",
			delta.HeapAllocDelta, iterations)
	}
}

// dedicatedHealthTarget starts a DEDICATED, in-process HTTP server owned solely
// by the calling test and returns its base URL. It serves the same /health
// payload shape as the HelixCode server ({"status","timestamp","version"}), so
// the per-request allocation profile the waves exercise is unchanged.
//
// DETERMINISM (§11.4.50) — why the two live-heap trend tests no longer drive
// the shared localhost:8080 server:
//
// These tests sample runtime.ReadMemStats of THE TEST PROCESS. The external
// HelixCode server runs in a DIFFERENT process, so its heap was never visible
// to the measurement: it contributed nothing to the quantity under test, but
// everything to whether a wave could finish. Every wave must land an IDENTICAL
// number of successful requests (runRequestWave), because a short wave
// allocates less than its siblings and makes the trend statistic meaningless.
// Under `go test ./...` the toolchain runs packages in PARALLEL and 20+ other
// test files in this module drive that SAME single server; requests then fail
// past their retry ceiling, the equal-wave precondition is violated, and the
// tests fail — while both pass in isolation. The precondition was satisfiable
// only by luck.
//
// MEASURED mechanism (not inferred): a TCP connection is identified by its
// 4-tuple, so every client connection to the one destination 127.0.0.1:8080
// consumes an entry from the host's single ephemeral-port space for that
// destination, and each closed connection holds its entry in TIME-WAIT
// afterwards. Sampled live during a parallel suite run on this host, TIME-WAIT
// entries toward 127.0.0.1:8080 peaked at 27986 against an
// ip_local_port_range of 32768-60999 — 28232 ports, i.e. 99.1% of that space
// consumed. With the space full, connect() returns EADDRNOTAVAIL, so a burst
// of retries fails as fast as it is issued. That is why PACKAGE PARALLELISM,
// not host CPU load, is the differentiator: the exhaustion is driven by the
// aggregate connection churn of many packages toward ONE destination port, and
// a single package in isolation never approaches it. Two full-suite runs
// produced 110 and 23 unfinishable requests in these two tests respectively.
//
// Widening a bound, lowering a wave count, or gating on an env var would hide
// that. Instead the precondition is made satisfiable BY CONSTRUCTION: a
// per-test httptest.Server listens on its OWN ephemeral port, so it draws from
// a different 4-tuple space entirely — one only this test uses — and its
// capacity cannot be consumed by another package, another test, or anything
// else on the host. The real net/http client path, real TCP connections, real
// connection pooling and real response bodies — everything the waves actually
// allocate — are untouched.
//
// Honest boundary (§11.4.6): these tests never measured the HelixCode server's
// memory and could not (wrong process). What they measured, and still measure,
// is that THIS process's live heap stays bounded across identical waves of real
// HTTP request work. The handler now allocates in-process too, so its own
// (bounded) per-request allocation is included in the sampled series as well —
// strictly more of the request path is under observation, not less.
func dedicatedHealthTarget(t *testing.T) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"status":"healthy","timestamp":"`+
			time.Now().UTC().Format(time.RFC3339Nano)+`","version":"1.0.0"}`)
	}))
	t.Cleanup(srv.Close)
	return srv.URL
}

// steadyStateWarmUp performs a DISCARDED workload pass so the sampled window
// that follows reflects steady state rather than a cold ramp.
//
// DETERMINISM (§11.4.50): the first pass over a fresh http.Client pays
// one-time costs no later pass repeats — the connection pool is populated
// (one keep-alive connection per concurrent worker, each retaining read and
// write buffers) and every lazily-initialised global the handler touches is
// allocated. That is a MONOTONIC live-heap ramp, which is precisely the shape
// the leak statistic exists to flag. Measured on this tree, the FIRST sampled
// run of TestMemory_LeakDetection_ConcurrentRequests produced
//
//	[730736 728304 791672 799992 795864 831024 844664 864216 885336 897976]
//	signed-R² = 0.9583, rise = 20.94% of mean
//
// — it cleared the trend bound outright and missed the 25% magnitude guard by
// four points — while runs 2-5 of the SAME binary scored 0.1185, 0.0178,
// -0.0173 and -0.2828. The verdict was being decided by warm-up state, not by
// the code under test.
//
// The fix removes the ramp from the sampled window instead of widening the
// bound the ramp crossed. This is a real discarded workload, never a sleep.
func steadyStateWarmUp(client *http.Client, url string, workers, perWorker int) {
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < perWorker; j++ {
				resp, err := client.Get(url)
				if err != nil {
					continue
				}
				io.Copy(io.Discard, resp.Body)
				resp.Body.Close()
			}
		}()
	}
	wg.Wait()
	runtime.GC()
	runtime.GC()
}

// waveRequestMaxAttempts bounds the per-request retry loop in runRequestWave.
// Against a dedicated, uncontended target (dedicatedHealthTarget) a request has
// no legitimate reason to fail at all, so this is a safety net for a genuinely
// starved host (transient EMFILE / ephemeral-port pressure) rather than a
// load-tolerance mechanism — it is what makes the equal-wave precondition
// robust instead of hopeful. It stays BOUNDED (never "retry forever") so a real
// defect in the request path still fails the test loudly instead of hanging the
// suite, and it carries no sleep, so nothing on this path depends on wall-clock
// timing.
const waveRequestMaxAttempts = 20

// runRequestWave performs EXACTLY perWorker SUCCESSFUL requests on each of
// `workers` goroutines and returns how many could not be completed.
//
// DETERMINISM (§11.4.50): the previous shape swallowed transport errors with a
// bare `continue`, so a saturated host quietly did LESS work per sample than a
// quiet one and the sampled series described the host's ability to land HTTP
// calls rather than the server's retention behaviour. Bounding each wave by a
// COUNT of successful requests makes the allocation volume behind every sample
// identical on any host. A wave that cannot reach its count is reported to the
// caller and fails the test loudly rather than silently shrinking the workload.
func runRequestWave(client *http.Client, url string, workers, perWorker int) int64 {
	var failed int64
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < perWorker; j++ {
				ok := false
				for attempt := 0; attempt < waveRequestMaxAttempts && !ok; attempt++ {
					resp, err := client.Get(url)
					if err != nil {
						continue
					}
					io.Copy(io.Discard, resp.Body)
					resp.Body.Close()
					ok = true
				}
				if !ok {
					atomic.AddInt64(&failed, 1)
				}
			}
		}()
	}
	wg.Wait()
	return failed
}

// TestMemory_LeakDetection_ConcurrentRequests tests for memory leaks under
// concurrent load.
func TestMemory_LeakDetection_ConcurrentRequests(t *testing.T) {
	config := DefaultTestConfig()
	client := &http.Client{Timeout: config.Timeout}

	// Dedicated, uncontended request target (see dedicatedHealthTarget). This
	// test no longer depends on an externally-running server, so it also no
	// longer silently SKIPs when one is absent (§11.4.3) — it always runs.
	healthURL := dedicatedHealthTarget(t) + "/health"

	// Wave geometry, declared before the warm-up because the warm-up is sized
	// from it (a function-scoped const is only in scope after its declaration).
	const (
		waves             = 10
		requestsPerWorker = 10
	)

	// Discarded steady-state warm-up (see steadyStateWarmUp) — keeps the cold
	// connection-pool / lazy-global ramp OUT of the sampled window, so the
	// trend statistic below scores retention behaviour instead of warm-up
	// state. Without it the first run of this binary scored signed-R² 0.9583.
	//
	// SIZING RULE (by construction, not a tuned constant): the discarded pass
	// performs a FULL sampled window's worth of work — waves*requestsPerWorker
	// per worker, exactly what the sampled window will perform. Any one-time
	// state that stabilises within one window's worth of requests is therefore
	// established BEFORE the first sample is taken, whatever that state is. The
	// previous 3-waves'-worth sizing left a measurable systematic residual (a
	// cold process scored signed-R2 0.7105 with a rise of 20.93% of mean, four
	// points under the magnitude guard); a window's worth removes it rather
	// than tolerating it. The bound itself is untouched.
	steadyStateWarmUp(client, healthURL, config.Concurrency, waves*requestsPerWorker)

	// Force GC before test
	runtime.GC()
	runtime.GC()

	beforeStats := CaptureMemoryStats()

	// Run concurrent requests in waves, sampling the live (post-GC) heap after
	// EACH wave. The wave loop is already the natural workload segmentation, so
	// we get a deterministic 10-sample series with no wall-clock ticker race. A
	// genuine leak grows the post-wave live-heap baseline monotonically; bounded
	// steady-state stays flat-but-noisy.
	//
	// Anti-bluff (§11.4.6 / §11.4.50): the old invariant was a single
	// before/after HeapAllocDelta vs a hardcoded 20 MB cap — env-sensitive and
	// non-deterministic because post-GC HeapAlloc oscillates ~20x on a bounded
	// run. heapTrendSignal asks "is the variance a trend or noise?" using ALL
	// samples (same statistic + bound as the GC-pressure test; self-validated by
	// TestHeapTrend_FlagsMonotonicLeak).
	var heapSamples []uint64
	var failedRequests int64
	for wave := 0; wave < waves; wave++ {
		// Counted wave (see runRequestWave): every sample is backed by the
		// SAME number of completed requests regardless of host load.
		failedRequests += runRequestWave(client, healthURL, config.Concurrency, requestsPerWorker)
		runtime.GC()
		heapSamples = append(heapSamples, CaptureMemoryStats().HeapAlloc)
	}
	// A wave that could not complete its request count did less allocation
	// than its siblings, so the series is not comparable. Report it rather
	// than scoring a shrunken workload as if it were the intended one.
	require.Zero(t, failedRequests,
		"%d requests could not be completed after retries — the sampled waves are not equal-sized, so the trend statistic is not meaningful for this run", failedRequests)

	// Force GC after test
	runtime.GC()
	runtime.GC()

	afterStats := CaptureMemoryStats()
	delta := CalculateDelta(beforeStats, afterStats)

	t.Logf("Memory delta after concurrent requests:")
	t.Logf("  Heap alloc delta (point snapshot): %d bytes", delta.HeapAllocDelta)
	t.Logf("  GC runs: %d", delta.GCRuns)
	t.Logf("  Live-heap samples (post-GC per wave, bytes): %v", heapSamples)

	// REAL INVARIANT: the per-wave live heap must stay BOUNDED — no unbounded
	// monotonic climb across the waves. Spike-robust trend, not a noisy delta cap.
	if len(heapSamples) >= 4 {
		isLeak, signedR2, slope, rise, frac := heapTrendIsLeak(heapSamples)
		t.Logf("  Live-heap trend signal (signed-R2): %.4f (leak if >= %.2f, slope %.0f B/sample, rise %.0f B = %.2f%% of mean)",
			signedR2, heapLeakSignalBound, slope, rise, frac*100)
		assert.False(t, isLeak,
			"Potential memory leak during concurrent requests: live heap shows a consistent, material upward trend across %d waves (signed-R2 %.4f >= %.2f AND rise %.0f bytes = %.2f%% of mean >= %.0f%%)",
			waves, signedR2, heapLeakSignalBound, rise, frac*100, heapLeakMinRiseFraction*100)
	} else {
		const grossRunawayCeiling = int64(512 * 1024 * 1024)
		assert.Less(t, delta.HeapAllocDelta, grossRunawayCeiling,
			"Heap delta %d bytes exceeds gross-runaway sanity ceiling during concurrent requests", delta.HeapAllocDelta)
	}
}

// TestMemory_LeakDetection_JSONParsing tests for memory leaks in JSON parsing
func TestMemory_LeakDetection_JSONParsing(t *testing.T) {
	config := DefaultTestConfig()
	client := &http.Client{Timeout: config.Timeout}

	// Skip if server is not available
	resp, err := client.Get(config.BaseURL + "/health")
	if err != nil {
		t.Skip("Server not available, skipping memory leak test") // SKIP-OK: #legacy-untriaged
	}
	resp.Body.Close()

	runtime.GC()
	runtime.GC()

	beforeStats := CaptureMemoryStats()

	// Create and parse many JSON requests, SEGMENTED so we sample the live
	// (post-GC) heap as the JSON-marshal/decode workload progresses. A leak in
	// the parse path grows the live-heap baseline across segments; bounded
	// behaviour stays flat-but-noisy.
	//
	// Anti-bluff (§11.4.6 / §11.4.50): the old invariant was a single
	// before/after HeapAllocDelta vs a hardcoded 15 MB cap — env-sensitive and
	// flaky because post-GC HeapAlloc oscillates ~20x on a bounded run. We use the
	// spike-robust signed-R² trend over multi-sample data (heapTrendSignal — same
	// statistic + bound as the GC-pressure test, self-validated by
	// TestHeapTrend_FlagsMonotonicLeak), segmenting a finite workload for a
	// deterministic sample count (no wall-clock race).
	iterations := config.Iterations
	const segments = 10
	segSize := iterations / segments
	if segSize < 1 {
		segSize = 1
	}
	var heapSamples []uint64
	done := 0
	for s := 0; s < segments && done < iterations; s++ {
		end := done + segSize
		if s == segments-1 || end > iterations {
			end = iterations
		}
		for i := done; i < end; i++ {
			// Create a project request with large payload
			project := map[string]interface{}{
				"name":        fmt.Sprintf("test-project-%d", i),
				"description": string(make([]byte, 1024)), // 1KB description
				"tags":        []string{"tag1", "tag2", "tag3"},
				"metadata": map[string]string{
					"key1": "value1",
					"key2": "value2",
				},
			}

			jsonData, err := json.Marshal(project)
			require.NoError(t, err)

			req, err := http.NewRequest("POST", config.BaseURL+"/api/v1/projects", bytes.NewBuffer(jsonData))
			require.NoError(t, err)
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Authorization", "Bearer "+config.AdminToken)

			resp, err := client.Do(req)
			if err != nil {
				continue
			}

			var result map[string]interface{}
			json.NewDecoder(resp.Body).Decode(&result)
			resp.Body.Close()
		}
		done = end
		runtime.GC()
		heapSamples = append(heapSamples, CaptureMemoryStats().HeapAlloc)
	}

	runtime.GC()
	runtime.GC()

	afterStats := CaptureMemoryStats()
	delta := CalculateDelta(beforeStats, afterStats)

	t.Logf("Memory delta after JSON parsing tests:")
	t.Logf("  Total allocated: %d bytes", delta.TotalAllocDelta)
	t.Logf("  Heap alloc delta (point snapshot): %d bytes", delta.HeapAllocDelta)
	t.Logf("  Live-heap samples (post-GC, bytes): %v", heapSamples)

	// REAL INVARIANT: the live heap must stay BOUNDED across the parse workload —
	// no unbounded monotonic growth. Spike-robust trend, not a noisy delta cap.
	if len(heapSamples) >= 4 {
		isLeak, signedR2, slope, rise, frac := heapTrendIsLeak(heapSamples)
		t.Logf("  Live-heap trend signal (signed-R2): %.4f (leak if >= %.2f, slope %.0f B/sample, rise %.0f B = %.2f%% of mean)",
			signedR2, heapLeakSignalBound, slope, rise, frac*100)
		assert.False(t, isLeak,
			"Potential memory leak in JSON parsing: live heap shows a consistent, material upward trend (signed-R2 %.4f >= %.2f AND rise %.0f bytes = %.2f%% of mean >= %.0f%%)",
			signedR2, heapLeakSignalBound, rise, frac*100, heapLeakMinRiseFraction*100)
	} else {
		const grossRunawayCeiling = int64(512 * 1024 * 1024)
		assert.Less(t, delta.HeapAllocDelta, grossRunawayCeiling,
			"Heap delta %d bytes exceeds gross-runaway sanity ceiling in JSON parsing", delta.HeapAllocDelta)
	}
}

// =============================================================================
// Memory Allocation Tests
// =============================================================================

// TestMemory_Allocation_LargePayloads tests memory handling with large payloads
func TestMemory_Allocation_LargePayloads(t *testing.T) {
	config := DefaultTestConfig()
	client := &http.Client{Timeout: config.Timeout}

	// Skip if server is not available
	resp, err := client.Get(config.BaseURL + "/health")
	if err != nil {
		t.Skip("Server not available, skipping allocation test") // SKIP-OK: #legacy-untriaged
	}
	resp.Body.Close()

	payloadSizes := []int{
		1024,            // 1KB
		10 * 1024,       // 10KB
		100 * 1024,      // 100KB
		1024 * 1024,     // 1MB
		5 * 1024 * 1024, // 5MB
	}

	for _, size := range payloadSizes {
		t.Run(fmt.Sprintf("PayloadSize_%d", size), func(t *testing.T) {
			runtime.GC()
			beforeStats := CaptureMemoryStats()

			// Create large payload
			payload := make([]byte, size)
			for i := range payload {
				payload[i] = byte('a' + (i % 26))
			}

			project := map[string]interface{}{
				"name":        "large-payload-test",
				"description": string(payload),
			}

			jsonData, err := json.Marshal(project)
			require.NoError(t, err)

			req, err := http.NewRequest("POST", config.BaseURL+"/api/v1/projects", bytes.NewBuffer(jsonData))
			require.NoError(t, err)
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Authorization", "Bearer "+config.AdminToken)

			resp, err := client.Do(req)
			if err != nil {
				t.Skipf("Request failed for size %d: %v (SKIP-OK: #unmarked-skip-needs-ticket)", size, err)
				return
			}
			io.Copy(io.Discard, resp.Body)
			resp.Body.Close()

			runtime.GC()
			afterStats := CaptureMemoryStats()
			delta := CalculateDelta(beforeStats, afterStats)

			t.Logf("Memory for %d byte payload:", size)
			t.Logf("  Allocated: %d bytes", delta.TotalAllocDelta)

			// Memory allocation should be reasonable (not more than 100x payload size)
			// HTTP requests involve JSON encoding, buffers, headers, etc. which add overhead
			maxExpectedAlloc := uint64(size * 100)
			assert.Less(t, delta.TotalAllocDelta, maxExpectedAlloc,
				"Excessive memory allocation for payload size %d", size)
		})
	}
}

// TestMemory_Allocation_ConnectionPooling tests connection pool memory efficiency
func TestMemory_Allocation_ConnectionPooling(t *testing.T) {
	config := DefaultTestConfig()

	// Create custom transport with connection pooling
	transport := &http.Transport{
		MaxIdleConns:        100,
		MaxIdleConnsPerHost: 100,
		IdleConnTimeout:     90 * time.Second,
	}
	client := &http.Client{Transport: transport, Timeout: config.Timeout}
	defer transport.CloseIdleConnections()

	// Skip if server is not available
	resp, err := client.Get(config.BaseURL + "/health")
	if err != nil {
		t.Skip("Server not available, skipping connection pool test") // SKIP-OK: #legacy-untriaged
	}
	resp.Body.Close()

	runtime.GC()
	runtime.GC()

	beforeStats := CaptureMemoryStats()

	// Make many requests to test connection reuse
	for i := 0; i < 500; i++ {
		resp, err := client.Get(config.BaseURL + "/health")
		if err != nil {
			continue
		}
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
	}

	runtime.GC()
	runtime.GC()

	afterStats := CaptureMemoryStats()
	delta := CalculateDelta(beforeStats, afterStats)

	t.Logf("Connection pool memory efficiency:")
	t.Logf("  Total allocated: %d bytes", delta.TotalAllocDelta)
	t.Logf("  Heap delta: %d bytes", delta.HeapAllocDelta)

	// Connection pooling should keep allocations low
	maxAllowedAlloc := uint64(50 * 1024 * 1024) // 50MB
	assert.Less(t, delta.TotalAllocDelta, maxAllowedAlloc,
		"Connection pooling not effective, excessive allocations")
}

// =============================================================================
// GC Pressure Tests
// =============================================================================

// TestMemory_GCPressure_HighAllocationRate tests behavior under high allocation rate
func TestMemory_GCPressure_HighAllocationRate(t *testing.T) {
	config := DefaultTestConfig()
	client := &http.Client{Timeout: config.Timeout}

	// Dedicated, uncontended request target (see dedicatedHealthTarget). This
	// test no longer depends on an externally-running server, so it also no
	// longer silently SKIPs when one is absent (§11.4.3) — it always runs.
	healthURL := dedicatedHealthTarget(t) + "/health"

	// Discarded steady-state warm-up (see steadyStateWarmUp) — the cold
	// connection-pool ramp is a monotonic climb and must not enter the
	// sampled window. Its size is fixed by the SIZING RULE stated at the call
	// site below.
	//
	// ROUND-7: this const block is declared HERE, above the warm-up call,
	// because a function-scoped const is only in scope AFTER its declaration.
	// It previously sat below, next to the wave loop, while the warm-up above
	// already referenced gcRequestsPerWorker — so the package did not compile
	// and `make verify-compile` failed at verify-compile-tests.
	const (
		gcWaves             = 15
		gcRequestsPerWorker = 40
	)
	// SIZING RULE (see the concurrent-requests test): a FULL sampled window's
	// worth of discarded work — gcWaves*gcRequestsPerWorker per worker — so any
	// one-time state that stabilises within one window is established before the
	// first sample. The previous 2-waves'-worth sizing left a systematic +8%..
	// +15%-of-mean residual rise measured across 8 cold processes.
	steadyStateWarmUp(client, healthURL, config.Concurrency, gcWaves*gcRequestsPerWorker)

	runtime.GC()
	beforeStats := CaptureMemoryStats()

	startTime := time.Now()

	// Heavy allocation workload, sampling the live (post-GC) heap after EACH
	// wave. We force GC before every sample so the reading reflects the live
	// (retained) heap, not transient allocation. A genuine leak manifests as a
	// monotonically growing live-heap baseline; bounded steady-state load
	// settles roughly flat regardless of the host's absolute heap size, GOGC,
	// or scavenger timing.
	//
	// DETERMINISM (§11.4.50): this workload used to run for a WALL-CLOCK 30s
	// with a 2s time.Ticker driving the sampler, so both the amount of work
	// done and the NUMBER of samples were decided by how much CPU the host
	// happened to spare. Measured across five consecutive runs of identical
	// code the sampler produced 15, 15, 15, 15 and then 14 samples, and the
	// request volume behind each sample moved with ambient load; a starved
	// enough run drops below the 4-sample floor and silently falls through to
	// the coarse gross-runaway backstop instead of asserting the real
	// invariant. The workload is now bounded by a COUNT of completed requests
	// and the series length is a constant, so every run allocates the same
	// volume and yields the same number of samples on any host.
	//
	// Nothing measured is lost: the two invariants are "the collector runs"
	// and "the live heap stays bounded under a heavy allocation workload".
	// Neither is a per-second quantity. Requests/sec is still logged, but as
	// host-dependent diagnostics only — it is asserted on nowhere.
	//
	// Anti-bluff (§11.4.6): an absolute MB cap on a single post-GC HeapAlloc
	// snapshot is an env-sensitive hardcoded-from-literature threshold — the
	// same product code measured ~23 MB on one host and ~125 MB on another
	// purely from runtime/scavenger differences while GC kept up the whole
	// time. The real invariant is "the live heap does not grow without bound
	// across the run", which is what actually detects a leak.
	var heapSamples []uint64
	var failedRequests int64
	requestCount := int64(0)
	for wave := 0; wave < gcWaves; wave++ {
		failedRequests += runRequestWave(client, healthURL, config.Concurrency, gcRequestsPerWorker)
		requestCount += int64(config.Concurrency * gcRequestsPerWorker)
		runtime.GC()
		heapSamples = append(heapSamples, CaptureMemoryStats().HeapAlloc)
	}
	duration := time.Since(startTime)
	require.Zero(t, failedRequests,
		"%d requests could not be completed after retries — the sampled waves are not equal-sized, so the trend statistic is not meaningful for this run", failedRequests)

	runtime.GC()
	runtime.GC()

	afterStats := CaptureMemoryStats()
	delta := CalculateDelta(beforeStats, afterStats)

	t.Logf("GC pressure test results:")
	t.Logf("  Duration: %v", duration)
	t.Logf("  Requests: %d", requestCount)
	t.Logf("  Requests/sec: %.2f", float64(requestCount)/duration.Seconds())
	t.Logf("  GC runs: %d", delta.GCRuns)
	t.Logf("  GC runs/sec: %.2f", float64(delta.GCRuns)/duration.Seconds())
	t.Logf("  Heap delta (point snapshot): %d bytes", delta.HeapAllocDelta)
	t.Logf("  Live-heap samples (post-GC, bytes): %v", heapSamples)

	// The collector must actually run. runtime.GC() BLOCKS until its
	// collection completes, and the wave loop calls it exactly gcWaves times,
	// so a functioning collector deterministically advances NumGC by at least
	// gcWaves on any host — a stuck or disabled collector cannot. The previous
	// `> 0` form depended on the automatic collector firing inside a
	// wall-clock window, which a sufficiently starved host need not do.
	//
	// Honest boundary (§11.4.6): this asserts the collector RUNS AND
	// COMPLETES, not that the automatic (GOGC-triggered) collector kept pace
	// with an allocation RATE — that is a per-second property and cannot be
	// asserted deterministically under ambient load. The automatic collections
	// beyond our forced ones are logged as diagnostics instead.
	assert.GreaterOrEqual(t, delta.GCRuns, uint32(gcWaves),
		"collector did not complete the %d forced collections (observed %d) — GC is stuck or disabled", gcWaves, delta.GCRuns)
	t.Logf("  GC runs beyond the %d forced collections (automatic, diagnostics only): %d", gcWaves, int64(delta.GCRuns)-int64(gcWaves))

	// REAL INVARIANT: the live heap must stay BOUNDED across the run — no
	// unbounded monotonic growth. We use a SPIKE-ROBUST trend statistic, not a
	// ratio of two noisy point-snapshots.
	//
	// Why not first-half-mean vs second-half-mean: post-GC HeapAlloc snapshots
	// oscillate wildly (observed e.g. 6 MB .. 142 MB on the SAME bounded run as
	// the scavenger reclaims spans between samples). A ratio of half-means
	// crosses any fixed band purely by WHERE the large spikes happen to land,
	// so it is non-deterministic (§11.4.50) — it fails by chance, not by leak.
	//
	// Instead we fit a least-squares line to the time-ordered samples and ask
	// "is the variance a TREND or is it NOISE?" via the signed coefficient of
	// determination (signed-R², heapTrendSignal). A genuine leak is a CONSISTENT
	// upward climb the line explains (signed-R² → +1). Bounded steady-state load
	// has its variance dominated by spike noise, so the line explains almost
	// nothing (signed-R² ≈ 0) — even when individual snapshots swing by 20x. The
	// statistic uses ALL points and is dimensionless (§11.4.6: signal-vs-noise,
	// not a hardcoded MB band). See TestHeapTrend_FlagsMonotonicLeak for the
	// analyzer self-validation that proves these polarities.
	if len(heapSamples) >= 4 {
		// DETERMINISM (§11.4.50): this used to score the series with the BARE
		// trend statistic heapTrendSignal, while its sibling
		// TestMemory_LeakDetection_ConcurrentRequests scored the same KIND of
		// series with heapTrendIsLeak — trend AND magnitude. The bare
		// statistic is scale-invariant, so a negligible absolute creep on a
		// small heap scores a high R²; heapLeakMinRiseFraction documents
		// exactly that failure mode, and TestHeapTrendIsLeak_RequiresTrendAndMagnitude
		// self-validates the guarded predicate against a real captured
		// false positive.
		//
		// The omission was justified in-comment by the claim that this test's
		// "heavy 30s workload produces tens-of-MB samples with real spike
		// noise". Measured on this tree that premise is false: the samples are
		// ~1 MB. A run captured here scored signed-R² 0.6182 and FAILED on
		//   [914296 887072 975208 915400 959488 888536 958400 971104 949240
		//    1001040 1005904 1074568 1015296 991936 1025864]
		// — a 112 KB drift on a ~960 KB mean, i.e. 12%, bounded steady-state by
		// any reading — while the other four runs of the same binary scored
		// 0.1284, -0.0765, 0.0122 and 0.0016. The verdict was noise.
		//
		// This is NOT a widened bound: heapLeakSignalBound and
		// heapLeakMinRiseFraction are unchanged. It applies the already-
		// self-validated leak PREDICATE the sibling test uses, so the two tests
		// now genuinely share one statistic, as their comments already claimed.
		isLeak, signedR2, slope, rise, frac := heapTrendIsLeak(heapSamples)
		t.Logf("  Live-heap trend slope: %.0f bytes/sample", slope)
		t.Logf("  Live-heap trend rise (slope*window): %.0f bytes = %.2f%% of mean", rise, frac*100)
		t.Logf("  Live-heap trend signal (signed-R2): %.4f (leak if >= %.2f AND rise >= %.0f%% of mean)", signedR2, heapLeakSignalBound, heapLeakMinRiseFraction*100)

		// A real leak's consistent climb drives signed-R² toward +1 (the line
		// explains the variance). Bounded steady-state keeps signed-R² near 0
		// (variance is noise, not trend) or negative (heap reclaimed). The 0.5
		// bound sits in the wide deterministic gap between bounded-noisy data
		// (~0.00) and even a slow leak buried in jitter (~0.5+) — calibrated
		// against captured real runs, NOT a hardcoded MB figure. Proven to FAIL
		// on synthetic monotonic-growth series by TestHeapTrend_FlagsMonotonicLeak.
		assert.False(t, isLeak,
			"Live heap shows a consistent AND material upward trend (signed-R2 %.4f >= %.2f AND rise %.0f bytes = %.2f%% of mean >= %.0f%%): possible leak",
			signedR2, heapLeakSignalBound, rise, frac*100, heapLeakMinRiseFraction*100)
	} else {
		// Not enough samples to fit a trend (short/interrupted run). Fall back
		// to a deliberately GENEROUS absolute sanity ceiling sized well above
		// observed bounded steady-state (~125 MB worst-case host) so it only
		// fires on a gross runaway, never on normal host variance. Coarse
		// backstop, NOT the primary invariant.
		const grossRunawayCeiling = int64(512 * 1024 * 1024)
		assert.Less(t, delta.HeapAllocDelta, grossRunawayCeiling,
			"Heap delta %d bytes exceeds gross-runaway sanity ceiling", delta.HeapAllocDelta)
	}
}

// TestMemory_GCPressure_BurstTraffic tests GC behavior during burst traffic
func TestMemory_GCPressure_BurstTraffic(t *testing.T) {
	config := DefaultTestConfig()
	client := &http.Client{Timeout: config.Timeout}

	// Skip if server is not available
	resp, err := client.Get(config.BaseURL + "/health")
	if err != nil {
		t.Skip("Server not available, skipping burst traffic test") // SKIP-OK: #legacy-untriaged
	}
	resp.Body.Close()

	runtime.GC()
	runtime.GC()

	beforeStats := CaptureMemoryStats()

	// Simulate burst traffic: periods of high activity followed by quiet periods
	for burst := 0; burst < 5; burst++ {
		// High activity burst
		var wg sync.WaitGroup
		for i := 0; i < config.Concurrency*2; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for j := 0; j < 50; j++ {
					resp, err := client.Get(config.BaseURL + "/health")
					if err == nil {
						io.Copy(io.Discard, resp.Body)
						resp.Body.Close()
					}
				}
			}()
		}
		wg.Wait()

		// Quiet period - let GC catch up
		time.Sleep(500 * time.Millisecond)
	}

	runtime.GC()
	runtime.GC()

	afterStats := CaptureMemoryStats()
	delta := CalculateDelta(beforeStats, afterStats)

	t.Logf("Burst traffic GC test:")
	t.Logf("  GC runs: %d", delta.GCRuns)
	t.Logf("  Heap delta: %d bytes", delta.HeapAllocDelta)

	// Memory should be reclaimed during quiet periods
	maxAllowedGrowth := int64(25 * 1024 * 1024)
	assert.Less(t, delta.HeapAllocDelta, maxAllowedGrowth,
		"Memory not properly reclaimed during burst traffic quiet periods")
}

// =============================================================================
// Resource Cleanup Tests
// =============================================================================

// TestMemory_ResourceCleanup_Goroutines tests for goroutine leaks
func TestMemory_ResourceCleanup_Goroutines(t *testing.T) {
	config := DefaultTestConfig()
	client := &http.Client{Timeout: config.Timeout}

	// Skip if server is not available
	resp, err := client.Get(config.BaseURL + "/health")
	if err != nil {
		t.Skip("Server not available, skipping goroutine leak test") // SKIP-OK: #legacy-untriaged
	}
	resp.Body.Close()

	initialGoroutines := runtime.NumGoroutine()
	t.Logf("Initial goroutines: %d", initialGoroutines)

	// Run many concurrent operations
	for round := 0; round < 5; round++ {
		var wg sync.WaitGroup
		for i := 0; i < config.Concurrency; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for j := 0; j < 20; j++ {
					resp, err := client.Get(config.BaseURL + "/health")
					if err == nil {
						io.Copy(io.Discard, resp.Body)
						resp.Body.Close()
					}
				}
			}()
		}
		wg.Wait()
	}

	// Give time for goroutines to clean up
	time.Sleep(2 * time.Second)
	runtime.GC()

	finalGoroutines := runtime.NumGoroutine()
	goroutineDelta := finalGoroutines - initialGoroutines

	t.Logf("Final goroutines: %d (delta: %d)", finalGoroutines, goroutineDelta)

	// Allow for some goroutine growth but not excessive
	maxAllowedGrowth := 50
	assert.Less(t, goroutineDelta, maxAllowedGrowth,
		"Potential goroutine leak: %d goroutines created but not cleaned up", goroutineDelta)
}

// TestMemory_ResourceCleanup_FileDescriptors tests for file descriptor leaks
func TestMemory_ResourceCleanup_FileDescriptors(t *testing.T) {
	config := DefaultTestConfig()
	client := &http.Client{Timeout: config.Timeout}

	// Skip if server is not available
	resp, err := client.Get(config.BaseURL + "/health")
	if err != nil {
		t.Skip("Server not available, skipping file descriptor test") // SKIP-OK: #legacy-untriaged
	}
	resp.Body.Close()

	// Make many requests, properly closing all resources
	for i := 0; i < config.Iterations*2; i++ {
		resp, err := client.Get(config.BaseURL + "/health")
		if err != nil {
			continue
		}
		// Ensure body is fully read and closed
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
	}

	// This test mainly ensures no panic from too many open files
	// The test passing indicates proper resource cleanup
	t.Log("File descriptor cleanup test passed")
}

// TestMemory_ResourceCleanup_Contexts tests context cancellation cleanup
func TestMemory_ResourceCleanup_Contexts(t *testing.T) {
	config := DefaultTestConfig()
	client := &http.Client{Timeout: config.Timeout}

	// Skip if server is not available
	resp, err := client.Get(config.BaseURL + "/health")
	if err != nil {
		t.Skip("Server not available, skipping context cleanup test") // SKIP-OK: #legacy-untriaged
	}
	resp.Body.Close()

	runtime.GC()
	beforeStats := CaptureMemoryStats()
	initialGoroutines := runtime.NumGoroutine()

	// Create and cancel many contexts, SEGMENTED so we sample the live (post-GC)
	// heap as the create/cancel workload progresses. A context/request leak (e.g.
	// timers or goroutines not released on cancel) grows the live-heap baseline
	// across segments; correct cleanup keeps it flat-but-noisy.
	//
	// Anti-bluff (§11.4.6 / §11.4.50): the old invariant was a single
	// before/after HeapAllocDelta vs a hardcoded 10 MB cap — env-sensitive and
	// flaky (post-GC HeapAlloc oscillates ~20x on a bounded run). We use the
	// spike-robust signed-R² trend over multi-sample data (heapTrendSignal — same
	// statistic + bound as the GC-pressure test, self-validated by
	// TestHeapTrend_FlagsMonotonicLeak). The goroutine-cleanup check this test
	// genuinely validates is preserved below.
	iterations := config.Iterations
	const segments = 10
	segSize := iterations / segments
	if segSize < 1 {
		segSize = 1
	}
	var heapSamples []uint64
	done := 0
	for s := 0; s < segments && done < iterations; s++ {
		end := done + segSize
		if s == segments-1 || end > iterations {
			end = iterations
		}
		for i := done; i < end; i++ {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			req, _ := http.NewRequestWithContext(ctx, "GET", config.BaseURL+"/health", nil)

			resp, err := client.Do(req)
			if err == nil {
				io.Copy(io.Discard, resp.Body)
				resp.Body.Close()
			}
			cancel() // Always cancel to clean up
		}
		done = end
		runtime.GC()
		heapSamples = append(heapSamples, CaptureMemoryStats().HeapAlloc)
	}

	time.Sleep(time.Second)
	runtime.GC()
	runtime.GC()

	afterStats := CaptureMemoryStats()
	finalGoroutines := runtime.NumGoroutine()
	delta := CalculateDelta(beforeStats, afterStats)

	t.Logf("Context cleanup test:")
	t.Logf("  Goroutine delta: %d", finalGoroutines-initialGoroutines)
	t.Logf("  Heap delta (point snapshot): %d bytes", delta.HeapAllocDelta)
	t.Logf("  Live-heap samples (post-GC, bytes): %v", heapSamples)

	// REAL INVARIANT: the live heap must stay BOUNDED across the context
	// create/cancel workload — no unbounded monotonic growth. Spike-robust trend,
	// not a noisy delta cap.
	if len(heapSamples) >= 4 {
		isLeak, signedR2, slope, rise, frac := heapTrendIsLeak(heapSamples)
		t.Logf("  Live-heap trend signal (signed-R2): %.4f (leak if >= %.2f, slope %.0f B/sample, rise %.0f B = %.2f%% of mean)",
			signedR2, heapLeakSignalBound, slope, rise, frac*100)
		assert.False(t, isLeak,
			"Context cleanup may have issues: live heap shows a consistent, material upward trend (signed-R2 %.4f >= %.2f AND rise %.0f bytes = %.2f%% of mean >= %.0f%%)",
			signedR2, heapLeakSignalBound, rise, frac*100, heapLeakMinRiseFraction*100)
	} else {
		const grossRunawayCeiling = int64(512 * 1024 * 1024)
		assert.Less(t, delta.HeapAllocDelta, grossRunawayCeiling,
			"Heap delta %d bytes exceeds gross-runaway sanity ceiling in context cleanup", delta.HeapAllocDelta)
	}
}

// =============================================================================
// Memory Profiling Helpers
// =============================================================================

// TestMemory_Profiling_Baseline establishes baseline memory usage
func TestMemory_Profiling_Baseline(t *testing.T) {
	runtime.GC()
	runtime.GC()

	stats := CaptureMemoryStats()

	t.Logf("Baseline memory statistics:")
	t.Logf("  Allocated: %d bytes (%.2f MB)", stats.Alloc, float64(stats.Alloc)/1024/1024)
	t.Logf("  Total allocated: %d bytes (%.2f MB)", stats.TotalAlloc, float64(stats.TotalAlloc)/1024/1024)
	t.Logf("  System: %d bytes (%.2f MB)", stats.Sys, float64(stats.Sys)/1024/1024)
	t.Logf("  Heap allocated: %d bytes (%.2f MB)", stats.HeapAlloc, float64(stats.HeapAlloc)/1024/1024)
	t.Logf("  Heap system: %d bytes (%.2f MB)", stats.HeapSys, float64(stats.HeapSys)/1024/1024)
	t.Logf("  Heap idle: %d bytes (%.2f MB)", stats.HeapIdle, float64(stats.HeapIdle)/1024/1024)
	t.Logf("  Heap in use: %d bytes (%.2f MB)", stats.HeapInuse, float64(stats.HeapInuse)/1024/1024)
	t.Logf("  Stack in use: %d bytes (%.2f MB)", stats.StackInuse, float64(stats.StackInuse)/1024/1024)
	t.Logf("  Number of GC runs: %d", stats.NumGC)
	t.Logf("  Number of goroutines: %d", runtime.NumGoroutine())
}

// TestMemory_Profiling_IdleServerMemory tests memory usage of an idle server
func TestMemory_Profiling_IdleServerMemory(t *testing.T) {
	config := DefaultTestConfig()
	client := &http.Client{Timeout: config.Timeout}

	// Skip if server is not available
	resp, err := client.Get(config.BaseURL + "/health")
	if err != nil {
		t.Skip("Server not available, skipping idle memory test") // SKIP-OK: #legacy-untriaged
	}
	resp.Body.Close()

	// Wait for server to settle
	time.Sleep(2 * time.Second)
	runtime.GC()
	runtime.GC()

	stats := CaptureMemoryStats()

	t.Logf("Server memory (idle after health check):")
	t.Logf("  Heap in use: %d bytes (%.2f MB)", stats.HeapInuse, float64(stats.HeapInuse)/1024/1024)
	t.Logf("  Goroutines: %d", runtime.NumGoroutine())

	// Idle server should not use excessive memory
	maxIdleHeap := uint64(100 * 1024 * 1024) // 100MB
	assert.Less(t, stats.HeapInuse, maxIdleHeap,
		"Idle server using too much memory")
}

// =============================================================================
// Stress Tests
// =============================================================================

// TestMemory_Stress_SustainedLoad tests memory stability under sustained load
func TestMemory_Stress_SustainedLoad(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping sustained load test in short mode") // SKIP-OK: #short-mode
	}

	config := DefaultTestConfig()
	client := &http.Client{Timeout: config.Timeout}

	// Skip if server is not available
	resp, err := client.Get(config.BaseURL + "/health")
	if err != nil {
		t.Skip("Server not available, skipping sustained load test") // SKIP-OK: #legacy-untriaged
	}
	resp.Body.Close()

	runtime.GC()
	runtime.GC()

	initialStats := CaptureMemoryStats()
	samples := make([]*MemoryStats, 0)

	// Run sustained load for 60 seconds, sampling every 10 seconds
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	var wg sync.WaitGroup
	done := make(chan struct{})

	// Worker goroutines
	for i := 0; i < config.Concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-done:
					return
				default:
					resp, err := client.Get(config.BaseURL + "/health")
					if err == nil {
						io.Copy(io.Discard, resp.Body)
						resp.Body.Close()
					}
				}
			}
		}()
	}

	// Sampling goroutine
	go func() {
		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				samples = append(samples, CaptureMemoryStats())
			}
		}
	}()

	<-ctx.Done()
	close(done)
	wg.Wait()

	runtime.GC()
	runtime.GC()

	finalStats := CaptureMemoryStats()

	t.Logf("Sustained load test results:")
	t.Logf("  Initial heap: %d bytes", initialStats.HeapInuse)
	for i, sample := range samples {
		t.Logf("  Sample %d heap: %d bytes", i+1, sample.HeapInuse)
	}
	t.Logf("  Final heap: %d bytes", finalStats.HeapInuse)

	// Check that memory doesn't grow unbounded
	delta := CalculateDelta(initialStats, finalStats)
	maxAllowedGrowth := int64(100 * 1024 * 1024) // 100MB
	assert.Less(t, delta.HeapAllocDelta, maxAllowedGrowth,
		"Memory grew unbounded during sustained load")
}
