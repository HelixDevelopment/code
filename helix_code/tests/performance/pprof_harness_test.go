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

// Package performance hosts the speed-programme measurement harness.
//
// pprof_harness_test.go is the P0-T01 deliverable (R4 phased plan
// docs/research/speed/04-phased-implementation-plan.md §3): it runs the four
// canonical hot-path scenarios S1-S4 under CPU profiling and writes .pprof
// files that become the Phase 0 baseline evidence. It also carries the
// CONST-050 unit + integration coverage for the pprof wiring:
//
//   - TestPprofutil_FlagWiringProducesNonEmptyProfile (unit) — proves the
//     pprofutil.Start/Stop wiring (the same code the --pprof CLI flag drives)
//     produces a non-empty, parseable CPU profile.
//   - TestPprofHTTPEndpoint_ServesValidProfile (integration) — proves the
//     net/http/pprof mount on the real server router serves a valid profile.
//
// The harness itself (TestPprofHarness_CaptureBaseline) is invoked explicitly
// via `go test -run TestPprofHarness_CaptureBaseline` with HELIX_PPROF_OUT set
// to the baseline directory; under a bare `go test` it writes profiles to a
// temp dir so the suite stays self-contained.
package performance

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime/pprof"
	"strings"
	"testing"
	"time"

	googlepprof "github.com/google/pprof/profile"

	"dev.helix.code/internal/config"
	"dev.helix.code/internal/pprofutil"
	"dev.helix.code/internal/server"
	"dev.helix.code/tests/performance/scenarios"
)

// HarnessOutEnvVar lets the operator direct the harness's .pprof output at the
// committed baseline directory. When unset the harness uses t.TempDir().
const HarnessOutEnvVar = "HELIX_PPROF_OUT"

// hotPathWork executes representative CPU work for one scenario so the captured
// CPU profile reflects a real code path rather than an idle process. It reuses
// the P0-T04 scenario runner + fixture generator.
func hotPathWork(t *testing.T, id, fixtureRoot string, manifest *scenarios.Manifest) scenarios.Result {
	t.Helper()
	spec, ok := manifest.Scenario(id)
	if !ok {
		t.Fatalf("scenario %s absent from manifest", id)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	return scenarios.RunScenario(ctx, spec, scenarios.RunOptions{
		FixtureRoot: fixtureRoot,
		Manifest:    manifest,
	})
}

// TestPprofHarness_CaptureBaseline runs S1-S4 under CPU profiling and writes a
// <id>-cpu.pprof + <id>-heap.pprof pair per scenario via pprofutil — the exact
// capture path the --pprof CLI flag drives. The profiles are the Phase 0
// baseline evidence (CONST-035): each is parsed back and asserted non-empty.
func TestPprofHarness_CaptureBaseline(t *testing.T) {
	outDir := strings.TrimSpace(os.Getenv(HarnessOutEnvVar))
	if outDir == "" {
		outDir = t.TempDir()
	}
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		t.Fatalf("create out dir %s: %v", outDir, err)
	}

	manifest, err := scenarios.LoadManifest("")
	if err != nil {
		t.Fatalf("load scenarios manifest: %v", err)
	}

	// S3/S4 need a fixture. Generate the canonical deterministic fixture once.
	fixtureRoot := filepath.Join(t.TempDir(), "speed-fixture")
	cfg := scenarios.DefaultFixtureConfig(manifest)
	fileCount, markerCount, genErr := scenarios.GenerateFixture(fixtureRoot, cfg)
	if genErr != nil {
		t.Fatalf("generate fixture: %v", genErr)
	}
	t.Logf("fixture: %d files generated, %d carry the search marker", fileCount, markerCount)

	// Per-scenario iteration count. S1 (process spawn) and S2 (network
	// dispatch / skipped without a real provider) do almost no *in-process*
	// CPU work per run — the cost is in a child process or on the wire — so
	// they need far more iterations to accumulate parent-process CPU samples.
	// S3/S4 do real in-process walk+parse work and saturate the sampler fast.
	iterations := map[string]int{"S1": 400, "S2": 400, "S3": 12, "S4": 12}

	for _, id := range []string{"S1", "S2", "S3", "S4"} {
		id := id
		t.Run(id, func(t *testing.T) {
			cap, startErr := pprofutil.Start(outDir, id)
			if startErr != nil {
				t.Fatalf("start pprof capture for %s: %v", id, startErr)
			}
			if cap == nil {
				t.Fatalf("pprofutil.Start returned nil capture for non-empty dir %s", outDir)
			}

			// Run the scenario enough times that the CPU profile accumulates
			// real samples from the genuine code path.
			var lastResult scenarios.Result
			n := iterations[id]
			for i := 0; i < n; i++ {
				lastResult = hotPathWork(t, id, fixtureRoot, manifest)
			}

			elapsed, heapPath, stopErr := cap.Stop(id)
			if stopErr != nil {
				t.Fatalf("stop pprof capture for %s: %v", id, stopErr)
			}

			cpuPath := filepath.Join(outDir, id+"-cpu.pprof")
			// The CPU profile MUST parse as a valid pprof profile (anti-bluff:
			// proves real capture machinery). For S3/S4 the in-process walk+
			// parse work MUST also yield at least one CPU sample. S1/S2 spend
			// their time in a child process / on the network, so the parent
			// CPU profile is asserted valid+heap-non-empty rather than
			// requiring parent CPU samples — captured honestly per CONST-035.
			switch id {
			case "S3", "S4":
				assertProfileNonEmpty(t, cpuPath)
			default:
				assertProfileValid(t, cpuPath)
			}
			assertProfileNonEmpty(t, heapPath)

			if lastResult.Skipped {
				t.Logf("%s scenario skipped (%s) — profile still captured over harness work",
					id, lastResult.SkipReason)
			}
			t.Logf("%s: %d iterations, profiled %s of work — cpu=%s heap=%s detail=%q",
				id, n, elapsed, cpuPath, heapPath, lastResult.Detail)
		})
	}
}

// assertProfileValid asserts a .pprof file exists, is non-empty bytes and
// parses as a valid pprof profile. Unlike assertProfileNonEmpty it does NOT
// require CPU samples — used for the inherently-IO-bound S1/S2 CPU profiles.
func assertProfileValid(t *testing.T, path string) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("profile %s missing: %v", path, err)
	}
	if info.Size() == 0 {
		t.Fatalf("profile %s is zero bytes", path)
	}
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open profile %s: %v", path, err)
	}
	defer f.Close()
	prof, err := googlepprof.Parse(f)
	if err != nil {
		t.Fatalf("profile %s does not parse as a valid pprof profile: %v", path, err)
	}
	t.Logf("profile %s: %d bytes, %d samples (IO-bound scenario — parent CPU samples optional)",
		path, info.Size(), len(prof.Sample))
}

// assertProfileNonEmpty parses a .pprof file with the canonical pprof profile
// parser and fails the test unless it contains at least one sample. A profile
// that parses but has zero samples is a bluff — it would not prove the harness
// profiled real code.
func assertProfileNonEmpty(t *testing.T, path string) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("profile %s missing: %v", path, err)
	}
	if info.Size() == 0 {
		t.Fatalf("profile %s is zero bytes", path)
	}
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open profile %s: %v", path, err)
	}
	defer f.Close()
	prof, err := googlepprof.Parse(f)
	if err != nil {
		t.Fatalf("profile %s does not parse as a valid pprof profile: %v", path, err)
	}
	if len(prof.Sample) == 0 {
		t.Fatalf("profile %s parsed but contains zero samples — harness captured nothing", path)
	}
	t.Logf("profile %s: %d bytes, %d samples, %d functions",
		path, info.Size(), len(prof.Sample), len(prof.Function))
}

// TestPprofutil_FlagWiringProducesNonEmptyProfile is the CONST-050 unit test:
// it exercises the exact pprofutil.Start/Stop path the --pprof CLI flag drives
// and proves it produces a non-empty, parseable CPU profile file.
func TestPprofutil_FlagWiringProducesNonEmptyProfile(t *testing.T) {
	// ResolveDir: flag value wins; env var is the fallback; "" disables.
	if got := pprofutil.ResolveDir("/tmp/from-flag", func(string) string { return "/tmp/from-env" }); got != "/tmp/from-flag" {
		t.Fatalf("ResolveDir: flag should win, got %q", got)
	}
	if got := pprofutil.ResolveDir("", func(string) string { return "/tmp/from-env" }); got != "/tmp/from-env" {
		t.Fatalf("ResolveDir: env fallback failed, got %q", got)
	}
	if got := pprofutil.ResolveDir("", func(string) string { return "" }); got != "" {
		t.Fatalf("ResolveDir: disabled case should yield \"\", got %q", got)
	}

	// Disabled path: Start("") returns a nil capture and Stop is a safe no-op.
	nilCap, err := pprofutil.Start("", "")
	if err != nil {
		t.Fatalf("Start(\"\") should not error: %v", err)
	}
	if nilCap != nil {
		t.Fatalf("Start(\"\") should return nil capture")
	}
	if _, _, err := nilCap.Stop(""); err != nil {
		t.Fatalf("Stop on nil capture should be a no-op: %v", err)
	}

	// Enabled path: profile a small CPU-bound loop and assert the file exists,
	// is non-empty and parses with at least one sample.
	dir := t.TempDir()
	cap, err := pprofutil.Start(dir, "unit")
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if cap == nil {
		t.Fatalf("Start returned nil capture for a non-empty dir")
	}
	burnCPU(150 * time.Millisecond)
	elapsed, heapPath, err := cap.Stop("unit")
	if err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if elapsed <= 0 {
		t.Fatalf("Stop reported non-positive elapsed: %v", elapsed)
	}
	assertProfileNonEmpty(t, filepath.Join(dir, "unit-cpu.pprof"))
	assertProfileNonEmpty(t, heapPath)
}

// burnCPU does deterministic CPU work for at least d so a CPU profile captures
// real samples. It uses a result sink to defeat dead-code elimination.
//
//go:noinline
func burnCPU(d time.Duration) {
	deadline := time.Now().Add(d)
	var sink uint64
	for time.Now().Before(deadline) {
		for i := uint64(0); i < 2_000_000; i++ {
			sink = sink*6364136223846793005 + 1442695040888963407
		}
	}
	cpuSink = sink
}

var cpuSink uint64

// TestPprofHTTPEndpoint_ServesValidProfile is the CONST-050 integration test:
// it builds a real server router with the net/http/pprof mount enabled and
// asserts /debug/pprof/profile serves a valid, parseable CPU profile and the
// pprof index is reachable. No mocks — this is the real Gin router.
func TestPprofHTTPEndpoint_ServesValidProfile(t *testing.T) {
	t.Setenv(server.PprofHTTPEnvVar, "1")

	cfg := &config.Config{}
	cfg.Logging.Level = "debug"
	// New(cfg, nil, nil): the pprof mount does not depend on DB/Redis, so a
	// nil-infra server is sufficient to exercise the debug endpoints.
	srv := server.New(cfg, nil, nil)
	handler := srv.Handler()
	if handler == nil {
		t.Fatalf("server.Handler() returned nil")
	}
	ts := httptest.NewServer(handler)
	defer ts.Close()

	// 1. The pprof index must be reachable (proves the mount happened).
	idxResp, err := http.Get(ts.URL + "/debug/pprof/")
	if err != nil {
		t.Fatalf("GET /debug/pprof/: %v", err)
	}
	defer idxResp.Body.Close()
	if idxResp.StatusCode != http.StatusOK {
		t.Fatalf("/debug/pprof/ returned status %d, want 200 — endpoint not mounted", idxResp.StatusCode)
	}

	// 2. A short live CPU profile must serve and parse as a real profile.
	//    CPU work runs concurrently during the profile window so the served
	//    profile carries real samples — proving the endpoint profiles live
	//    process activity, not just an empty header.
	stopBurn := make(chan struct{})
	burnDone := make(chan struct{})
	go func() {
		defer close(burnDone)
		var sink uint64
		for {
			select {
			case <-stopBurn:
				cpuSink = sink
				return
			default:
				for i := uint64(0); i < 1_000_000; i++ {
					sink = sink*6364136223846793005 + 1442695040888963407
				}
			}
		}
	}()
	profResp, err := http.Get(ts.URL + "/debug/pprof/profile?seconds=2")
	close(stopBurn)
	<-burnDone
	if err != nil {
		t.Fatalf("GET /debug/pprof/profile: %v", err)
	}
	defer profResp.Body.Close()
	if profResp.StatusCode != http.StatusOK {
		t.Fatalf("/debug/pprof/profile returned status %d, want 200", profResp.StatusCode)
	}
	prof, err := googlepprof.Parse(profResp.Body)
	if err != nil {
		t.Fatalf("/debug/pprof/profile body is not a valid pprof profile: %v", err)
	}
	if len(prof.Sample) == 0 {
		t.Fatalf("/debug/pprof/profile parsed but carried zero samples — endpoint did not profile live work")
	}
	t.Logf("/debug/pprof/profile served a valid profile: %d samples, %d locations",
		len(prof.Sample), len(prof.Location))

	// 3. The heap profile must also serve and parse.
	heapResp, err := http.Get(ts.URL + "/debug/pprof/heap")
	if err != nil {
		t.Fatalf("GET /debug/pprof/heap: %v", err)
	}
	defer heapResp.Body.Close()
	if heapResp.StatusCode != http.StatusOK {
		t.Fatalf("/debug/pprof/heap returned status %d, want 200", heapResp.StatusCode)
	}
	if _, err := googlepprof.Parse(heapResp.Body); err != nil {
		t.Fatalf("/debug/pprof/heap body is not a valid pprof profile: %v", err)
	}
}

// staticCheck keeps the named runtime profiles importable so a refactor that
// drops one is caught at compile time.
var _ = pprof.Profiles
