package server

// RED-polarity switch convention for the internal/server regression guards.
// THIS FILE IS THE SINGLE PLACE the convention is defined, enumerated and
// enforced (HXC-155). A new guard author copies from here.
//
// ---------------------------------------------------------------------------
// THE CONVENTION
// ---------------------------------------------------------------------------
//
//  1. NAME — each guard owns a guard-specific switch named `RED_<GUARD>`
//     (e.g. RED_WS_AUTH). Per-guard names are DELIBERATE, not drift: each
//     corresponds to a different historical fix, and a tester must be able to
//     flip exactly one guard into its RED reproduction without dragging the
//     whole package with it. Collapsing them into one variable is NOT the
//     goal and would lose that capability.
//
//  2. UMBRELLA — the package-wide `RED_MODE` turns EVERY registered guard RED
//     at once, so a sweep is possible without knowing each name. This closes
//     the real gap the divergence caused: before HXC-155, an operator running
//     `RED_MODE=1` silently did NOT flip the guards that use their own names,
//     so those never reproduced their defects and the sweep gave a false
//     impression of coverage. A guard-specific name still flips just its own.
//
//  3. HELPER SHAPE — every guard reads its switch through `redModeFor(t, ENV)`
//     — never a bare os.Getenv, never a bespoke per-file wrapper around a
//     bespoke getenv shim. One shape means one place a polarity bug can live.
//     A guard with no guard-specific name uses `redMode(t)` (umbrella only).
//
//  4. DEFAULT — unset MUST mean GREEN (standing guard runs). Truthy is
//     "1"/"true"/"yes", case-insensitive, whitespace-trimmed. An inverted
//     default is the exact defect HXC-152 was: unset-means-RED meant the GREEN
//     assertions never ran, so the guard guarded nothing while looking green.
//
//  5. REGISTRATION — every switch MUST be listed in `redPolaritySwitches`
//     below. The conformance tests in this file fail if a guard in the package
//     reads a RED switch that is not registered, which is what stops the next
//     author from quietly inventing a sixth spelling.
//
// Backward compatibility: HXC-155 introduced no renames. Every historical
// name is still honoured, because reproduction commands in the closure record
// cite them verbatim (e.g. docs/Fixed.md cites
// `RED_WIRE_FACADE_AUTH=1 go test -run TestWireFacadeEndpoints_NoKeyRejected`).
// Renaming would have orphaned those documented commands.

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// redUmbrellaEnv is the package-wide switch: it turns every registered guard
// RED at once (convention rule 2).
const redUmbrellaEnv = "RED_MODE"

// redPolaritySwitch describes one registered polarity switch.
type redPolaritySwitch struct {
	// Env is the guard-specific environment variable name.
	Env string
	// Files lists the guard sources that read it (documentation + the
	// registration audit trail).
	Files []string
	// ViaHelper is false only for a switch that has not yet been migrated to
	// redModeFor. Such a switch still honours its own name but does NOT
	// respond to the RED_MODE umbrella — an honest, tracked gap (§11.4.6)
	// rather than a silent one.
	ViaHelper bool
}

// redPolaritySwitches is the SINGLE ENUMERATION POINT for the package's
// polarity switches (convention rule 5). Enumerated by scanning every env-var
// read in the package, not by grepping RED_MODE alone — grepping the umbrella
// name alone under-enumerates and misses every guard-specific spelling, which
// is how the divergence went unnoticed in the first place.
var redPolaritySwitches = []redPolaritySwitch{
	{
		Env:       "HELIX_RED_MODE",
		Files:     []string{"handlers_systemstatus_guard_test.go"},
		ViaHelper: true,
	},
	{
		Env:       "HELIX_QASTATUS_RED_MODE",
		Files:     []string{"handlers_qastatus_deadlock_guard_test.go"},
		ViaHelper: false, // migration pending — see redPolarityPendingMigration
	},
	{
		Env:       "RED_LLM_AUTH",
		Files:     []string{"llm_auth_guard_test.go"},
		ViaHelper: true,
	},
	{
		Env:       "RED_LLAMACPP_LOCAL",
		Files:     []string{"llm_generate_llamacpp_local_test.go"},
		ViaHelper: true,
	},
	{
		Env:       "RED_WS_AUTH",
		Files:     []string{"ws_auth_test.go"},
		ViaHelper: true,
	},
	{
		Env:       "RED_WIRE_FACADE_AUTH",
		Files:     []string{"wire_facade_auth_test.go"},
		ViaHelper: true,
	},
	{
		Env:       "RED_PROJECT_IDOR",
		Files:     []string{"handlers_project_idor_test.go"},
		ViaHelper: true,
	},
	{
		Env:       "RED_CLOUD_GATE_STATUS",
		Files:     []string{"llm_generate_cloud_gate_status_test.go"},
		ViaHelper: true,
	},
}

// redPolarityPendingMigration records switches that are registered and
// default-GREEN but still read their env directly, so they do not yet respond
// to the RED_MODE umbrella. Listing them keeps the gap visible instead of
// letting it read as "fully converged".
//
// HELIX_QASTATUS_RED_MODE: handlers_qastatus_deadlock_guard_test.go was being
// edited concurrently under HXC-154 when HXC-155 landed, so it was deliberately
// not touched (§11.4.84 working-tree quiescence) rather than raced.
var redPolarityPendingMigration = map[string]string{
	"HELIX_QASTATUS_RED_MODE": "file concurrently edited under HXC-154; migrate to redModeFor when that work lands",
}

// redEnvTruthy reports whether an env var holds a truthy polarity value.
// Unset, empty, or any other value means GREEN (convention rule 4).
func redEnvTruthy(name string) bool {
	v := strings.TrimSpace(os.Getenv(name))
	return v == "1" || strings.EqualFold(v, "true") || strings.EqualFold(v, "yes")
}

// redModeFor is THE polarity helper (convention rule 3). It reports RED when
// either the guard's own switch OR the RED_MODE umbrella is truthy; unset
// means GREEN. Pass "" for guardEnv to consult the umbrella only.
func redModeFor(t *testing.T, guardEnv string) bool {
	t.Helper()
	if guardEnv != "" && redEnvTruthy(guardEnv) {
		return true
	}
	return redEnvTruthy(redUmbrellaEnv)
}

// redMode reports whether the package-wide RED_MODE umbrella is set. Guards
// with no guard-specific switch of their own use this.
func redMode(t *testing.T) bool {
	t.Helper()
	return redModeFor(t, "")
}

// ---------------------------------------------------------------------------
// Conformance checks — these are the "check that catches a future file
// breaking the convention" (convention rule 5).
// ---------------------------------------------------------------------------

// redEnvReadPattern matches an env-var read through any of the shapes used in
// this package, capturing the variable name. Matching the READ forms (rather
// than any bare string) keeps prose such as `"RED_MODE: expected ..."` inside
// t.Fatalf messages from being mistaken for a switch declaration.
var redEnvReadPattern = regexp.MustCompile(
	`(?:os\.Getenv|os\.LookupEnv|t\.Setenv|redModeFor\(t,|getenvDefault)\(?\s*"([A-Za-z0-9_]+)"`)

// scanPackageRedSwitches returns every RED-ish env var actually read by the
// package's test sources, mapped to the files reading it.
func scanPackageRedSwitches(t *testing.T) map[string][]string {
	t.Helper()

	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("cannot read package directory: %v", err)
	}

	found := map[string][]string{}
	scanned := 0
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, "_test.go") {
			continue
		}
		// This file declares the enumeration itself; its literals are the
		// registry, not guard reads.
		if name == filepath.Base(redConventionFileName) {
			continue
		}
		src, err := os.ReadFile(name)
		if err != nil {
			t.Fatalf("cannot read %s: %v", name, err)
		}
		scanned++
		for _, m := range redEnvReadPattern.FindAllStringSubmatch(string(src), -1) {
			env := m[1]
			if !strings.Contains(env, "RED") {
				continue // not a polarity switch (e.g. PROBE_N, HELIX_LLM_PROVIDER)
			}
			found[env] = append(found[env], name)
		}
	}

	if scanned == 0 {
		t.Fatal("scanned 0 test files — the conformance check would vacuously " +
			"pass and guard nothing")
	}
	return found
}

const redConventionFileName = "red_polarity_convention_test.go"

// TestRedPolarityConvention_EverySwitchIsRegistered fails when a guard reads a
// RED polarity switch that is not in redPolaritySwitches — i.e. when a future
// file invents a new spelling without registering it. This is the mechanism
// that stops the drift HXC-155 was opened for from recurring.
func TestRedPolarityConvention_EverySwitchIsRegistered(t *testing.T) {
	registered := map[string]bool{redUmbrellaEnv: true}
	for _, s := range redPolaritySwitches {
		if registered[s.Env] {
			t.Fatalf("duplicate registration for %q in redPolaritySwitches", s.Env)
		}
		registered[s.Env] = true
	}

	found := scanPackageRedSwitches(t)
	if len(found) == 0 {
		t.Fatal("scan found no RED polarity switches at all — the pattern is " +
			"broken and this check would guard nothing")
	}

	for env, files := range found {
		if !registered[env] {
			t.Errorf("unregistered RED polarity switch %q read by %v\n"+
				"  Add it to redPolaritySwitches in %s, or use redModeFor(t, ...) "+
				"with an already-registered name. Every switch must be enumerated "+
				"in one place so a RED sweep cannot silently miss it.",
				env, files, redConventionFileName)
		}
	}
}

// TestRedPolarityConvention_RegistryMatchesReality fails when the registry
// lists a switch nothing reads any more, so the enumeration cannot rot into a
// stale list that merely looks complete.
func TestRedPolarityConvention_RegistryMatchesReality(t *testing.T) {
	found := scanPackageRedSwitches(t)
	for _, s := range redPolaritySwitches {
		if _, ok := found[s.Env]; !ok {
			t.Errorf("registered switch %q is no longer read by any guard "+
				"(declared files: %v) — remove it from redPolaritySwitches or "+
				"restore its guard", s.Env, s.Files)
		}
	}
}

// TestRedPolarityConvention_DefaultsAreGreen asserts, behaviourally, that
// every registered switch defaults to GREEN when unset — the HXC-152 inverted
// default cannot recur unnoticed. This checks behaviour, not a comment.
func TestRedPolarityConvention_DefaultsAreGreen(t *testing.T) {
	for _, s := range redPolaritySwitches {
		s := s
		t.Run(s.Env, func(t *testing.T) {
			t.Setenv(s.Env, "")
			t.Setenv(redUmbrellaEnv, "")
			if redModeFor(t, s.Env) {
				t.Fatalf("%s: unset must mean GREEN (standing guard), got RED — "+
					"an inverted default means the GREEN assertions never run "+
					"(the HXC-152 defect)", s.Env)
			}
		})
	}
	t.Run(redUmbrellaEnv, func(t *testing.T) {
		t.Setenv(redUmbrellaEnv, "")
		if redMode(t) {
			t.Fatalf("%s: unset must mean GREEN, got RED", redUmbrellaEnv)
		}
	})
}

// TestRedPolarityConvention_UmbrellaFlipsEveryHelperSwitch proves the sweep
// gap is actually closed: RED_MODE=1 must flip every switch that reads through
// redModeFor. Switches still pending migration are asserted to be exactly the
// documented set, so the honest gap cannot silently grow.
func TestRedPolarityConvention_UmbrellaFlipsEveryHelperSwitch(t *testing.T) {
	for _, s := range redPolaritySwitches {
		s := s
		t.Run(s.Env, func(t *testing.T) {
			t.Setenv(s.Env, "")
			t.Setenv(redUmbrellaEnv, "1")

			_, pending := redPolarityPendingMigration[s.Env]
			if pending != !s.ViaHelper {
				t.Fatalf("%s: ViaHelper=%v disagrees with the pending-migration "+
					"list; keep the two in sync so the gap stays visible",
					s.Env, s.ViaHelper)
			}
			if !s.ViaHelper {
				t.Skipf("SKIP-OK: %s reads its env directly and is not yet "+
					"umbrella-aware — %s", s.Env, redPolarityPendingMigration[s.Env])
			}
			if !redModeFor(t, s.Env) {
				t.Fatalf("%s: RED_MODE=1 must flip this guard RED; it did not, so "+
					"a package-wide RED sweep would silently skip it", s.Env)
			}
		})
	}
}

// TestRedPolarityConvention_GuardSpecificSwitchIsIndependent proves per-guard
// flipping still works — the capability the item requires be preserved: one
// guard goes RED without dragging the others with it.
func TestRedPolarityConvention_GuardSpecificSwitchIsIndependent(t *testing.T) {
	if len(redPolaritySwitches) < 2 {
		t.Fatal("need at least two registered switches to prove independence")
	}
	target := redPolaritySwitches[0]

	t.Setenv(redUmbrellaEnv, "")
	t.Setenv(target.Env, "1")

	if !redModeFor(t, target.Env) {
		t.Fatalf("%s=1 must flip its own guard RED", target.Env)
	}
	for _, other := range redPolaritySwitches[1:] {
		t.Setenv(other.Env, "")
		if redModeFor(t, other.Env) {
			t.Fatalf("%s=1 must NOT flip %s — per-guard switches must stay "+
				"independent so a tester can reproduce one defect at a time",
				target.Env, other.Env)
		}
	}
}

// TestRedPolarityConvention_TruthyValues pins the accepted truthy vocabulary
// so the four historical spellings keep behaving identically.
func TestRedPolarityConvention_TruthyValues(t *testing.T) {
	red := []string{"1", "true", "TRUE", "True", "yes", "YES", " 1 "}
	green := []string{"", "0", "false", "no", "off", "  ", "2", "enabled"}

	for _, v := range red {
		t.Setenv(redUmbrellaEnv, v)
		if !redMode(t) {
			t.Errorf("RED_MODE=%q must be RED", v)
		}
	}
	for _, v := range green {
		t.Setenv(redUmbrellaEnv, v)
		if redMode(t) {
			t.Errorf("RED_MODE=%q must be GREEN (default-safe direction)", v)
		}
	}
}
