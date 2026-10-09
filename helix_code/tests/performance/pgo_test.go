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

// pgo_test.go — speed-programme Phase 4 task P4-T01.
//
// Profile-Guided Optimization (PGO) coverage. Go's toolchain applies PGO
// automatically when a `default.pgo` file sits next to a package's
// `main.go`. This file's tests assert the committed PGO profiles are
// present, valid, and actually picked up by the build:
//
//   - TestPGO_DefaultProfilesPresent (unit) — both cmd/cli/default.pgo and
//     cmd/server/default.pgo exist and are non-empty committed build inputs.
//   - TestPGO_DefaultProfilesAreValidPprof (unit) — each default.pgo parses
//     as a valid pprof CPU profile with sample data (not an empty stub).
//   - TestPGO_BuildPicksUpProfile (integration) — `go build -pgo=auto`
//     against the real cmd/ packages invokes the toolchain `preprofile`
//     step on the committed default.pgo, proving every build is
//     PGO-optimised with no extra flags.
//
// PGO only changes code generation, never behaviour — so no behavioural
// assertion belongs here; the no-regression guarantee is the existing
// cmd/ test suites passing identically with the .pgo present.
package performance

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/pprof/profile"
)

// moduleRootForPGO walks up from the test's working directory to the inner
// Go module root (the directory containing go.mod with module dev.helix.code).
func moduleRootForPGO(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for i := 0; i < 8; i++ {
		gomod := filepath.Join(dir, "go.mod")
		if data, err := os.ReadFile(gomod); err == nil &&
			strings.Contains(string(data), "module dev.helix.code") {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	t.Fatal("could not locate inner Go module root (go.mod with module dev.helix.code)")
	return ""
}

// pgoProfilePaths returns the two committed default.pgo paths.
func pgoProfilePaths(t *testing.T) []string {
	t.Helper()
	root := moduleRootForPGO(t)
	return []string{
		filepath.Join(root, "cmd", "cli", "default.pgo"),
		filepath.Join(root, "cmd", "server", "default.pgo"),
	}
}

// TestPGO_DefaultProfilesPresent asserts both committed default.pgo build
// inputs exist and are non-empty. A missing or empty default.pgo means the
// build is silently NOT PGO-optimised.
func TestPGO_DefaultProfilesPresent(t *testing.T) {
	for _, p := range pgoProfilePaths(t) {
		info, err := os.Stat(p)
		if err != nil {
			t.Errorf("PGO profile missing — %s: %v", p, err)
			continue
		}
		if info.Size() == 0 {
			t.Errorf("PGO profile is empty (zero bytes) — %s: builds would not be PGO-optimised", p)
			continue
		}
		t.Logf("PGO profile present: %s (%d bytes)", p, info.Size())
	}
}

// TestPGO_DefaultProfilesAreValidPprof asserts each default.pgo parses as a
// valid pprof CPU profile carrying actual sample data — not an empty stub
// that would make PGO a no-op.
func TestPGO_DefaultProfilesAreValidPprof(t *testing.T) {
	for _, p := range pgoProfilePaths(t) {
		f, err := os.Open(p)
		if err != nil {
			t.Errorf("open %s: %v", p, err)
			continue
		}
		prof, err := profile.Parse(f)
		_ = f.Close()
		if err != nil {
			t.Errorf("%s is not a valid pprof profile: %v", p, err)
			continue
		}
		if len(prof.Sample) == 0 {
			t.Errorf("%s has zero samples — PGO would be a no-op", p)
			continue
		}
		if len(prof.SampleType) == 0 {
			t.Errorf("%s has no sample types", p)
			continue
		}
		t.Logf("PGO profile %s — %d samples, %d locations, sample types: %v",
			filepath.Base(filepath.Dir(p))+"/"+filepath.Base(p),
			len(prof.Sample), len(prof.Location), sampleTypeNames(prof))
	}
}

func sampleTypeNames(p *profile.Profile) []string {
	names := make([]string, 0, len(p.SampleType))
	for _, st := range p.SampleType {
		names = append(names, st.Type)
	}
	return names
}

// TestPGO_BuildPicksUpProfile is an integration test: it runs the real Go
// toolchain in dry-run mode against the cmd/ packages and asserts the
// `preprofile` step (which the toolchain runs ONLY when a PGO profile is in
// effect) is invoked on the committed default.pgo. This proves every build
// of cmd/cli and cmd/server is PGO-optimised with no extra flags.
func TestPGO_BuildPicksUpProfile(t *testing.T) {
	if testing.Short() {
		t.Skip("SKIP-OK: #P4-T01 — go-toolchain integration test skipped in -short mode")
	}
	root := moduleRootForPGO(t)
	for _, pkg := range []string{"./cmd/cli/", "./cmd/server/"} {
		// `-pgo=auto -a -n` prints the FULL build command set without
		// executing it. `-a` forces every package to be (dry-run) rebuilt
		// so the toolchain's `preprofile` step is always emitted — without
		// `-a` a fully-cached build prints nothing and the check is moot.
		cmd := exec.Command("go", "build", "-pgo=auto", "-a", "-n", pkg)
		cmd.Dir = root
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("go build -pgo=auto -n %s failed: %v\n%s", pkg, err, out)
		}
		s := string(out)
		if !strings.Contains(s, "preprofile") {
			t.Errorf("%s build did not invoke the PGO `preprofile` step — PGO not picked up", pkg)
			continue
		}
		if !strings.Contains(s, "default.pgo") {
			t.Errorf("%s build did not reference default.pgo", pkg)
			continue
		}
		t.Logf("%s: PGO active — toolchain runs `preprofile` on the committed default.pgo", pkg)
	}
}
