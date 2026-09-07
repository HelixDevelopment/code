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
//go:build integration || loadtest

// HXC-150: unit tests proving envOrHelix / envOrIntHelix read HELIX_ vars
// first (.env.full-test convention), then fall back to legacy TEST_* vars,
// then to the hardcoded default. No infra needed — pure env-var tests.
package ddos

import (
	"os"
	"testing"
)

func TestEnvOrHelix_ReadsHelixFirst(t *testing.T) {
	t.Setenv("HELIX_DATABASE_USER", "helixcode")
	t.Setenv("TEST_PG_USER", "legacy_helix")
	if got := envOrHelix("HELIX_DATABASE_USER", "TEST_PG_USER", "fallback"); got != "helixcode" {
		t.Errorf("expected helixcode from HELIX_, got %q", got)
	}
}

func TestEnvOrHelix_FallsBackToLegacy(t *testing.T) {
	unsetEnvForTest(t, "HELIX_DATABASE_USER")
	t.Setenv("TEST_PG_USER", "legacy_helix")
	if got := envOrHelix("HELIX_DATABASE_USER", "TEST_PG_USER", "fallback"); got != "legacy_helix" {
		t.Errorf("expected legacy_helix from TEST_, got %q", got)
	}
}

func TestEnvOrHelix_FallsBackToDefault(t *testing.T) {
	unsetEnvForTest(t, "HELIX_DATABASE_USER")
	unsetEnvForTest(t, "TEST_PG_USER")
	if got := envOrHelix("HELIX_DATABASE_USER", "TEST_PG_USER", "fallback"); got != "fallback" {
		t.Errorf("expected fallback default, got %q", got)
	}
}

func TestEnvOrIntHelix_ReadsHelixFirst(t *testing.T) {
	t.Setenv("HELIX_DATABASE_PORT", "5433")
	t.Setenv("TEST_PG_PORT", "9999")
	if got := envOrIntHelix("HELIX_DATABASE_PORT", "TEST_PG_PORT", 5432); got != 5433 {
		t.Errorf("expected 5433 from HELIX_, got %d", got)
	}
}

func TestEnvOrIntHelix_FallsBackToLegacy(t *testing.T) {
	unsetEnvForTest(t, "HELIX_DATABASE_PORT")
	t.Setenv("TEST_PG_PORT", "9999")
	if got := envOrIntHelix("HELIX_DATABASE_PORT", "TEST_PG_PORT", 5432); got != 9999 {
		t.Errorf("expected 9999 from TEST_, got %d", got)
	}
}

func TestEnvOrIntHelix_FallsBackToDefault(t *testing.T) {
	unsetEnvForTest(t, "HELIX_DATABASE_PORT")
	unsetEnvForTest(t, "TEST_PG_PORT")
	if got := envOrIntHelix("HELIX_DATABASE_PORT", "TEST_PG_PORT", 5432); got != 5432 {
		t.Errorf("expected 5432 default, got %d", got)
	}
}

// unsetEnvForTest removes key from the process environment for the duration of
// t and restores exactly what was there before — the prior VALUE if it was set,
// or absence if it was not. There is no t.Unsetenv, so this is the
// save/restore counterpart of t.Setenv; a bare os.Unsetenv leaks to every
// later test in the binary and makes verdicts depend on -shuffle ordering.
func unsetEnvForTest(t *testing.T, key string) {
	t.Helper()
	prev, had := os.LookupEnv(key)
	if err := os.Unsetenv(key); err != nil {
		t.Fatalf("os.Unsetenv(%q) failed: %v", key, err)
	}
	t.Cleanup(func() {
		if had {
			_ = os.Setenv(key, prev)
		} else {
			_ = os.Unsetenv(key)
		}
	})
}
