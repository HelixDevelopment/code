package server

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"dev.helix.code/internal/config"
	"dev.helix.code/internal/database"
	"dev.helix.code/internal/llm"
	"dev.helix.code/internal/redis"
)

// cloud_gate_isolation_test.go — DETERMINISM fix for the one process-global
// write the server constructor performs.
//
// THE DEFECT (D-1). server.New() calls llm.SetCloudEnabled(cfg.LLM.Cloud.Enabled)
// (server.go, "W2c-1 cloud gate"). That write is CORRECT in production: it is
// how the daemon applies the operator's llm.cloud.enabled policy at process
// start, and cmd/server/main.go — verified by grep to contain ZERO occurrences
// of "cloud" — has no other call that would set it. Removing the write from
// New would silently close the cloud gate for the shipped daemon, so the
// production line stays exactly as it is (no §11.4.108 SOURCE→RUNTIME change).
//
// The determinism defect is that TESTS inherit that write. Every *_test.go in
// this package that constructs a server leaves the process-wide gate at
// whatever its cfg said, for the remainder of the test BINARY. It is currently
// masked only because every fixture cfg happens to leave Cloud.Enabled at its
// false zero value, which coincides with the gate's own default — so the leak
// is invisible until one test constructs a server with cloud ENABLED, at which
// point every later test in the binary observes an OPEN gate and the outcome
// depends on -shuffle ordering. Every OTHER gate write in this tree
// (internal/llm/*_test.go, applications/terminal_ui/*_test.go,
// internal/server/provider_status_locality_test.go) already snapshots and
// restores; the New() path was the one that did not.
//
// THE FIX. newTestServer below is the single construction seam for tests: it
// snapshots the gate, registers the restore on t.Cleanup, and then calls the
// REAL New(). It is not a stub or a reimplementation — the production
// constructor still runs in full, including its gate write; only the leak past
// the test's own lifetime is removed. TestServerTests_ConstructViaIsolatedHelper
// makes that mechanically un-forgettable rather than remembered, so a future
// test cannot silently reintroduce the leak by calling New() directly.

// newTestServer builds a *Server through the real production constructor while
// containing New's one process-global side effect (the llm cloud gate) to the
// calling test's lifetime.
//
// Takes testing.TB, not *testing.T, so benchmarks (which also construct
// servers in this package) get the same isolation.
func newTestServer(tb testing.TB, cfg *config.Config, db *database.Database, rds *redis.Client) *Server {
	tb.Helper()
	isolateCloudGate(tb)
	return New(cfg, db, rds)
}

// isolateCloudGate snapshots the process-global llm cloud gate and restores it
// when tb finishes. It is the isolation half of newTestServer, exposed
// separately for the one construction that cannot go through the helper: a
// benchmark that calls New() inside its b.N loop, where per-iteration
// tb.Cleanup registration would accumulate b.N closures. Such a function calls
// isolateCloudGate ONCE before the loop; TestServerTests_ConstructViaIsolatedHelper
// accepts a bare New() only in a function that does exactly that.
func isolateCloudGate(tb testing.TB) {
	tb.Helper()
	prev := llm.CloudEnabled()
	tb.Cleanup(func() { llm.SetCloudEnabled(prev) })
}

// cloudEnabledTestConfig is a minimal server config with the cloud gate
// EXPLICITLY OPEN — the case that makes the leak observable. Nothing else in
// this package's fixtures sets it, which is precisely why the defect was
// latent.
func cloudEnabledTestConfig() *config.Config {
	return &config.Config{
		Server: config.ServerConfig{
			Address: "localhost",
			Port:    8080,
		},
		Logging: config.LoggingConfig{
			Level: "info",
		},
		LLM: config.LLMConfig{
			Cloud: config.CloudConfig{Enabled: true},
		},
	}
}

// TestServerNew_DoesNotLeakProcessCloudGate is the §11.4.115 RED→GREEN
// polarity test for D-1. ONE source, TWO roles, selected by RED_MODE:
//
//   - RED_MODE=1 reproduces the defect on the PRE-FIX construction path: it
//     calls New() directly, exactly as all 26 pre-fix call sites in this
//     package did, and asserts the gate is left OPEN afterwards. It PASSES
//     only while the defect is genuinely present, so it is the captured proof
//     that the leak is real and not hypothetical.
//   - RED_MODE=0 (the default, and the standing regression guard) drives the
//     FIXED path — newTestServer — and asserts the gate is restored.
//
// DETERMINISM of the test itself: the "sibling observer" is a subtest whose
// parent inspects the gate after the subtest's cleanups have run. That is the
// same observation a later top-level test in the binary would make, but it
// does not itself depend on -shuffle ordering to fail — it fails on EVERY run
// of the broken path and passes on EVERY run of the fixed one. A pair of
// order-dependent sibling top-level tests would have reproduced the defect
// only ~50% of the time, which is the very non-determinism being removed.
func TestServerNew_DoesNotLeakProcessCloudGate(t *testing.T) {
	// Snapshot at this test's own boundary too: whichever branch runs, this
	// test must not itself become the next leaker.
	outer := llm.CloudEnabled()
	t.Cleanup(func() { llm.SetCloudEnabled(outer) })

	if os.Getenv("RED_MODE") == "1" {
		t.Run("pre_fix_bare_New_with_cloud_enabled", func(t *testing.T) {
			srv := New(cloudEnabledTestConfig(), nil, nil)
			if srv == nil {
				t.Fatal("New returned nil")
			}
		})
		if !llm.CloudEnabled() {
			t.Fatal("RED_MODE: expected bare New(cfg with cloud enabled) to LEAK the " +
				"process-global cloud gate past the constructing test, but the gate was " +
				"closed afterwards — defect NOT reproduced, so this RED capture is blind")
		}
		t.Log("RED_MODE: defect reproduced — bare New() left llm.CloudEnabled()==true " +
			"after the constructing subtest finished; every later test in this binary " +
			"would observe an OPEN cloud gate")
		return
	}

	before := llm.CloudEnabled()
	t.Run("constructs_server_with_cloud_enabled", func(t *testing.T) {
		srv := newTestServer(t, cloudEnabledTestConfig(), nil, nil)
		if srv == nil {
			t.Fatal("newTestServer returned nil")
		}
		// The production write really did happen inside the subtest — this
		// asserts the helper isolates rather than suppresses it.
		if !llm.CloudEnabled() {
			t.Fatal("New(cfg with llm.cloud.enabled=true) did not open the cloud gate " +
				"during construction — the helper must ISOLATE the production write, " +
				"not neutralise it")
		}
	})

	if after := llm.CloudEnabled(); after != before {
		t.Fatalf("server construction leaked the process-global cloud gate to sibling "+
			"tests: llm.CloudEnabled() was %v before construction and %v after the "+
			"constructing subtest completed", before, after)
	}
}

// bareNewCall matches a call to this package's own New( — i.e. a New(
// not qualified by a selector (pkg.New) and not the tail of a longer
// identifier (httptest.NewServer, uuid.New, errors.New all fail to match).
var bareNewCall = regexp.MustCompile(`(?:^|[^.\w])New\(`)

// bareNewValue matches this package's New taken as a VALUE rather than called
// — `ctor := New`, `fns := []func(...){New}`, `defer run(New)`. The
// indirection defeats bareNewCall (there is no "New(" anywhere on the line)
// while constructing exactly the same leaking server one hop later.
//
// It requires New to be followed by something that is NOT an opening paren and
// not an identifier character, so `New(` and `NewThing` do not match twice.
var bareNewValue = regexp.MustCompile(`(?:^|[^.\w])New(?:$|[^(\w])`)

// cloudGateIsolationOwnFile is the ONE file permitted to call New() directly:
// this one, which owns the isolating helper and the RED reproduction branch.
const cloudGateIsolationOwnFile = "cloud_gate_isolation_test.go"

// TestServerTests_ConstructViaIsolatedHelper is the mechanical enforcement half
// of the D-1 fix. Restoring the existing call sites fixes today; this stops
// tomorrow's test from silently reintroducing the leak, which a convention
// documented only in a comment would not.
//
// The rule is function-scoped, not file-scoped: a bare New( is permitted only
// inside a function that itself calls isolateCloudGate( — the escape hatch the
// b.N-loop benchmark needs, and the ONLY one. Anything else must use
// newTestServer.
//
// Honest boundary (§11.4.6): this is a SOURCE-layer check (§11.4.108 layer 1).
// It proves no test function reaches the leaking constructor without arranging
// restoration; the proof that the isolation actually WORKS at runtime is
// TestServerNew_DoesNotLeakProcessCloudGate above, plus the package's
// -shuffle=on -count=5 runs.
//
// ROUND-8 N-3, part 1 — two bypasses, now closed. The scan partitioned the
// file starting at the FIRST top-level `func ` line, so a package-level
// initializer above it (`var _ = New(cfg, db, rds)`) was never inspected; and
// it matched only a literal `New(`, so taking the constructor as a value
// (`ctor := New` … `ctor(cfg, db, rds)`) walked straight past. Both were
// contrived rather than present, and both are cheap to close, so they are
// closed: the preamble is scanned as its own unisolated region, and
// bareNewValue matches the value form.
//
// ROUND-8 N-3, part 2 — the RESIDUAL, declared rather than left unknown.
// This guard is scoped to THIS PACKAGE's test files (it globs "*_test.go" in
// the package directory). The same cloud-gate leak class exists UNGUARDED in
// five other test binaries that construct a server directly:
//
// five other test binaries that construct a server directly. MEASURED, not
// estimated — `grep -rn 'server\.New(' tests/` on the round-8 tree reports 17
// construction sites across 8 files:
//
//	tests/regression/critical_paths_test.go        7
//	tests/integration/llm_generate_e2e_test.go     2
//	tests/integration/llm_stream_e2e_test.go       2
//	tests/integration/specify_server_e2e_test.go   2
//	tests/integration/realdb_auth_helper_test.go   1
//	tests/integration/web_browser_e2e_test.go      1
//	tests/ddos/ddos_flood_test.go                  1
//	tests/performance/pprof_harness_test.go        1
//
// Those are currently HARMLESS for one reason only, and it is a property of
// their fixtures rather than of their code: nothing under tests/ sets the
// cloud gate on (`grep -rn 'Cloud.*Enabled' tests/ --include=*.go` is empty),
// so server.New's llm.SetCloudEnabled call writes the value the gate already
// holds and nothing leaks. The day one of those configs
// enables cloud — for a cloud-provider regression test, say — that binary
// acquires the exact order-dependent leak this guard exists to prevent, and
// nothing will say so.
//
// This is stated here as a DECLARED BOUNDARY, not a to-do disguised as
// coverage: the fix belongs with whoever owns tests/, and a guard that silently
// implied it covered them would be the bluff. What this file guarantees is
// scoped to internal/server and nothing more.
func TestServerTests_ConstructViaIsolatedHelper(t *testing.T) {
	entries, err := filepath.Glob("*_test.go")
	if err != nil {
		t.Fatalf("globbing package test files failed: %v", err)
	}
	if len(entries) == 0 {
		t.Fatal("no *_test.go files found in the package directory — this guard " +
			"would silently pass over an empty set, so treat it as a failure")
	}

	var offenders []string
	scanned := 0
	for _, f := range entries {
		if filepath.Base(f) == cloudGateIsolationOwnFile {
			continue
		}
		raw, err := os.ReadFile(f)
		if err != nil {
			t.Fatalf("reading %s failed: %v", f, err)
		}
		scanned++
		lines := strings.Split(string(raw), "\n")

		// Partition the file into top-level function bodies: a function starts
		// at a line beginning with "func " and runs to the line before the next
		// such line. Closures (t.Run subtests) are nested inside, so they
		// inherit their enclosing function's isolation, which is the correct
		// scope for a t.Cleanup registration.
		var starts []int
		for i, line := range lines {
			if strings.HasPrefix(line, "func ") {
				starts = append(starts, i)
			}
		}

		// Regions to inspect: the PREAMBLE (everything above the first
		// function — package-level var/const initializers, which run before
		// any test and can never call isolateCloudGate) followed by each
		// top-level function body.
		type region struct{ from, to int }
		regions := []region{}
		firstFunc := len(lines)
		if len(starts) > 0 {
			firstFunc = starts[0]
		}
		if firstFunc > 0 {
			regions = append(regions, region{0, firstFunc})
		}
		for si, from := range starts {
			to := len(lines)
			if si+1 < len(starts) {
				to = starts[si+1]
			}
			regions = append(regions, region{from, to})
		}

		for _, reg := range regions {
			from := reg.from
			body := lines[reg.from:reg.to]
			isolated := false
			for _, line := range body {
				if strings.HasPrefix(strings.TrimSpace(line), "//") {
					continue
				}
				if strings.Contains(line, "isolateCloudGate(") {
					isolated = true
					break
				}
			}
			if isolated {
				continue
			}
			for off, line := range body {
				if strings.HasPrefix(strings.TrimSpace(line), "//") {
					continue
				}
				if bareNewCall.MatchString(line) || bareNewValue.MatchString(line) {
					offenders = append(offenders, fmt.Sprintf("%s:%d: %s", f, from+off+1, strings.TrimSpace(line)))
				}
			}
		}
	}
	if scanned == 0 {
		t.Fatal("no test files were actually scanned — the guard proved nothing")
	}
	if len(offenders) > 0 {
		t.Fatalf("%d test call site(s) construct a server via bare New(), which leaks "+
			"the process-global llm cloud gate (llm.SetCloudEnabled in server.New) to "+
			"every later test in this binary and makes results depend on -shuffle "+
			"ordering. Use newTestServer(t, cfg, db, rds) — or isolateCloudGate(tb) "+
			"once per function if the construction must stay bare — from %s:\n  %s",
			len(offenders), cloudGateIsolationOwnFile, strings.Join(offenders, "\n  "))
	}
	t.Logf("PASS: %d test file(s) scanned, zero unisolated New() server constructions — "+
		"every test-constructed server restores the process-global cloud gate", scanned)
}
