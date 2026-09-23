package commands

import (
	"os/exec"
	"runtime"
	"testing"
)

// TestBuiltinSkills_NoDrift is the HXC-159 F-13 drift-regression guard.
//
// Background (§11.4.77 regeneration-mechanism + §11.4.108 source-of-truth
// discipline): internal/commands/builtin_skills/<name>/SKILL.md, for the 7
// entries that also exist under constitution/skills/<name>/, is now
// GENERATED from constitution/skills/<name>/SKILL.md's superset frontmatter
// (see markdown_skills.go's builtinSkillsFS doc comment and
// scripts/generate_builtin_skills.sh). Before this fix, the two sides were
// two structurally different artifact types that could never be
// byte-identical (the constitution side carried no `triggers:`), so no
// automated check ran the sync verification — it existed only as a
// `//go:generate ... --check` directive, which nobody runs automatically
// (`go generate` is a manual, opt-in step). That left builtin_skills free to
// silently drift from constitution/skills again via any future hand-edit
// or partial regeneration.
//
// This test shells out to generate_builtin_skills.sh's SAFE, detection-only
// `--check` mode (never the destructive regenerate mode — a test MUST NOT
// mutate the working tree) and FAILS the moment builtin_skills/<name>/
// diverges from constitution/skills/<name>/ for any constitution-sourced
// skill, giving the sync a real, always-on regression guard instead of a
// discretionary generator flag.
//
// RED reproduction (manual, §11.4.115 polarity — never automated here since
// that would require mutating the live tree, which a test must not do):
//
//	echo '# stray drift line' >> internal/commands/builtin_skills/multitrack/SKILL.md
//	go test -run TestBuiltinSkills_NoDrift ./internal/commands/...
//	# FAILS, naming "multitrack" as drifted.
//	git checkout -- internal/commands/builtin_skills/multitrack/SKILL.md
//
// This exact RED reproduction was run manually while authoring this guard
// (HXC-159 F-13, 2026-09-23) and confirmed FAIL before being reverted; see
// docs/qa/hxc159_f13_superset_schema_20260923/ for the captured transcript.
func TestBuiltinSkills_NoDrift(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("SKIP-OK: generate_builtin_skills.sh is a bash script, not exercised on windows hosts")
	}
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("SKIP-OK: bash not on PATH")
	}

	// Relative to this package's directory (internal/commands/), matching
	// how `go test` sets the process cwd; the script itself resolves every
	// other path (SOURCE_DIR, BUILTIN_DIR) from its own BASH_SOURCE, so this
	// relative script path is the only path this test needs to supply.
	script := "../../scripts/generate_builtin_skills.sh"
	cmd := exec.Command("bash", script, "--check")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("internal/commands/builtin_skills has drifted from constitution/skills "+
			"(HXC-159 F-13 regression: builtin_skills must stay generated from "+
			"constitution/skills, never hand-edited) - run "+
			"`bash helix_code/scripts/generate_builtin_skills.sh` from the repo root to "+
			"resync, then re-run this test:\n%s", out)
	}
}
