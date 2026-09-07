package llm

import (
	"os"
	"testing"
)

// xiaomi_keyrecognition_test.go — Xiaomi MiMo key-recognition coverage.
//
// DETERMINISM (D-2). The _Present and _Absent cases below mutate the PROCESS
// environment. Before the fix they did so without saving prior state:
// _Present used os.Setenv + `defer os.Unsetenv` (which restores to UNSET, not
// to whatever was there before — clobbering an operator's real
// XIAOMI_MIMO_API_KEY for every later test in the binary), and _Absent called
// bare os.Unsetenv on BOTH aliases with no restore at all. Either way a
// sibling test that expects those variables set passes or fails purely on
// execution ORDER, which -shuffle=on makes visible and which is exactly the
// non-determinism the operator mandate forbids.
//
// Both now go through save/restore: t.Setenv for the set case (the standard
// library restores prior state, including prior absence, on test end) and
// withoutEnvVar for the unset case (there is no t.Unsetenv, so this mirrors
// the withoutAzureEnv idiom already used elsewhere in this package).

// withoutEnvVar removes key from the process environment for the duration of
// t, then restores exactly what was there before — the prior VALUE if it was
// set, or absence if it was not. This distinction matters: restoring an
// originally-absent variable by setting it to "" would leave PresentProviders
// (which uses os.LookupEnv) seeing a variable that did not previously exist.
func withoutEnvVar(t *testing.T, key string) {
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

// xiaomiKeyAliases mirrors the aliases asserted by
// TestXiaomiKeyRecognition_Aliases; kept as one list so the isolation helpers
// and the assertions cannot drift apart.
var xiaomiKeyAliases = []string{"XIAOMI_MIMO_API_KEY", "ApiKey_Xiaomi_MiMo"}

func TestXiaomiKeyRecognition_Aliases(t *testing.T) {
	aliases := ProviderEnvAliases()
	xiaomiAliases, ok := aliases[ProviderTypeXiaomi]
	if !ok {
		t.Fatal("ProviderTypeXiaomi not found in ProviderEnvAliases")
	}
	expected := xiaomiKeyAliases
	if len(xiaomiAliases) != len(expected) {
		t.Fatalf("expected %d aliases, got %d", len(expected), len(xiaomiAliases))
	}
	for i, a := range expected {
		if xiaomiAliases[i] != a {
			t.Errorf("alias[%d] = %q, want %q", i, xiaomiAliases[i], a)
		}
	}
}

func TestXiaomiKeyRecognition_Present(t *testing.T) {
	// t.Setenv restores the PRIOR state (value or absence) at test end — the
	// `defer os.Unsetenv` this replaced restored to absence unconditionally.
	t.Setenv("XIAOMI_MIMO_API_KEY", "sk-test123")

	present := PresentProviders()
	if !present[ProviderTypeXiaomi] {
		t.Fatal("Xiaomi should be present when XIAOMI_MIMO_API_KEY is set")
	}
}

func TestXiaomiKeyRecognition_Absent(t *testing.T) {
	for _, alias := range xiaomiKeyAliases {
		withoutEnvVar(t, alias)
	}

	present := PresentProviders()
	if present[ProviderTypeXiaomi] {
		t.Fatal("Xiaomi should NOT be present when no key is set")
	}
}

// TestXiaomiKeyRecognition_AbsentDoesNotLeakEnv is the §11.4.115 RED→GREEN
// polarity test for D-2. ONE source, TWO roles, selected by RED_MODE:
//
//   - RED_MODE=1 drives the PRE-FIX body of TestXiaomiKeyRecognition_Absent
//     verbatim — bare os.Unsetenv on both aliases — and asserts the parent's
//     value is GONE afterwards. It PASSES only while the leak is genuinely
//     present, so it is the captured proof the defect was real.
//   - RED_MODE=0 (default, standing regression guard) drives the FIXED body
//     and asserts the parent's value survived intact.
//
// The observer is this test's own parent scope rather than a separate
// top-level test, so the check is deterministic: it fails on EVERY run of the
// broken body and passes on EVERY run of the fixed one, instead of depending
// on -shuffle to place an observer after the leaker (~50% of orderings).
func TestXiaomiKeyRecognition_AbsentDoesNotLeakEnv(t *testing.T) {
	const sentinel = "sk-d2-sentinel-must-survive"
	const key = "XIAOMI_MIMO_API_KEY"

	// Establish the state a sibling test would rely on. t.Setenv restores
	// whatever was here before when THIS test ends, so the probe is itself
	// hermetic.
	t.Setenv(key, sentinel)

	red := os.Getenv("RED_MODE") == "1"

	t.Run("absent_case_body", func(t *testing.T) {
		if red {
			// Verbatim pre-fix body: unset with no save.
			os.Unsetenv("XIAOMI_MIMO_API_KEY")
			os.Unsetenv("ApiKey_Xiaomi_MiMo")
		} else {
			for _, alias := range xiaomiKeyAliases {
				withoutEnvVar(t, alias)
			}
		}
		// Both bodies must genuinely establish absence — the fix isolates the
		// mutation, it does not weaken the assertion the test makes.
		if PresentProviders()[ProviderTypeXiaomi] {
			t.Fatal("Xiaomi still reported present although both key aliases were unset")
		}
	})

	got, stillSet := os.LookupEnv(key)
	if red {
		if stillSet {
			t.Fatalf("RED_MODE: expected the pre-fix bare os.Unsetenv body to LEAK — "+
				"%s should be gone after the subtest — but it is still set to %q. "+
				"Defect NOT reproduced, so this RED capture is blind", key, got)
		}
		t.Logf("RED_MODE: defect reproduced — %s was set to %q before the subtest and "+
			"is UNSET afterwards; any later test in this binary that expects it set "+
			"would fail purely because of execution order", key, sentinel)
		// Put it back so the RED run itself does not poison the rest of the
		// binary while proving that the pre-fix body did.
		t.Setenv(key, sentinel)
		return
	}
	if !stillSet {
		t.Fatalf("the absent-case body leaked: %s was set to %q before it ran and is "+
			"UNSET afterwards — sibling tests now observe a different environment "+
			"depending on execution order", key, sentinel)
	}
	if got != sentinel {
		t.Fatalf("the absent-case body clobbered %s: want %q, got %q", key, sentinel, got)
	}
}
