package agent

// T-P5.03 (HXC-159 Phase P5) — unify the user skills directory.
//
// # RUNTIME SIGNATURE (§11.4.108)
//
// Before this fix, cmd/cli/main.go resolved the user skills directory as
// os.UserConfigDir()/helixcode/skills, while applications/terminal_ui/main.go
// and internal/clientcore/agentic.go resolved it as $HOME/.helix/skills — two
// disjoint directories, hardcoded independently in three places. A skill
// installed under one convention was invisible to a front-end using the
// other. The observable that proves this fix is genuinely wired: a single
// skill file, installed ONCE under either directory, is discoverable through
// UserSkillsDirs() regardless of which one it landed in.
//
// # Historical split (always true, independent of this fix)
//
// TestHistoricalSplit_CLIConventionVsTUIConvention reproduces the OLD, raw
// path constructions each front-end used BEFORE T-P5.03, directly (not
// through any shared helper), and proves they name different directories and
// that a skill installed under one is invisible when scanned via the other's
// convention alone. This documents WHY the unification is needed; it is not
// itself a polarity test (the split described is real and permanent — it is
// literally what each front-end's old inline code did) — the fix in this
// package does not change what those two raw paths name, it changes what the
// unified UserSkillsDirs() helper returns and what the call sites now scan.
//
// # RED → GREEN
//
// Before UserSkillsDirs/CanonicalUserSkillsDir/LegacyUserSkillsDir existed,
// this file did not compile — captured verbatim in
// qa-results/hxc159/skills_dir_unify/<run-id>/red_compile_fail.txt. That is
// the RED reproduction (§11.4.115): there is no runtime polarity switch for a
// helper that does not exist yet. TestUnifiedHelper_SeesEitherLegacyOrCanonical
// is the standing GREEN guard.

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// writeMinimalSkill writes a minimal packaged SKILL.md fixture at
// <dir>/<name>/SKILL.md — the same on-disk shape commands.SkillLoader already
// parses, so this fixture is indistinguishable from a real user install.
func writeMinimalSkill(t *testing.T, dir, name string) {
	t.Helper()
	skillDir := filepath.Join(dir, name)
	require.NoError(t, os.MkdirAll(skillDir, 0o755))
	content := "---\ndescription: fixture skill " + name + "\ntriggers:\n  - \"(?i)^" + name + "$\"\n---\nrendered body for " + name + "\n"
	require.NoError(t, os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(content), 0o644))
}

// isolateHomeAndConfig sets HOME and XDG_CONFIG_HOME to two SEPARATE
// temporary roots so CanonicalUserSkillsDir() and LegacyUserSkillsDir()
// resolve to two genuinely distinct directories, reproducing the exact
// real-world split (a developer's real $HOME and real XDG_CONFIG_HOME are
// independent paths). Returns (legacyDir, canonicalDir).
func isolateHomeAndConfig(t *testing.T) (legacyDir, canonicalDir string) {
	t.Helper()
	tmpHome := t.TempDir()
	tmpConfig := t.TempDir()
	t.Setenv("HOME", tmpHome)
	t.Setenv("XDG_CONFIG_HOME", tmpConfig)

	legacyDir = LegacyUserSkillsDir()
	require.NotEmpty(t, legacyDir, "LegacyUserSkillsDir must resolve under an isolated HOME")
	require.Equal(t, filepath.Join(tmpHome, ".helix", "skills"), legacyDir)

	canon, err := CanonicalUserSkillsDir()
	require.NoError(t, err)
	require.NotEmpty(t, canon)
	require.Equal(t, filepath.Join(tmpConfig, "helixcode", "skills"), canon)
	canonicalDir = canon

	require.NotEqual(t, legacyDir, canonicalDir, "the two directories must be genuinely distinct for this test to mean anything")
	return legacyDir, canonicalDir
}

// TestHistoricalSplit_CLIConventionVsTUIConvention documents the real,
// pre-T-P5.03 defect: a loader built the way the CLI used to build it
// (canonical dir alone) cannot see a skill installed the way a TUI user used
// to install it (legacy dir alone), and vice versa.
func TestHistoricalSplit_CLIConventionVsTUIConvention(t *testing.T) {
	legacyDir, canonicalDir := isolateHomeAndConfig(t)

	writeMinimalSkill(t, legacyDir, "tui-installed-only")
	writeMinimalSkill(t, canonicalDir, "cli-installed-only")

	// Old CLI convention: canonical dir alone.
	cliOnlyReg, _, err := LoadSkillsAndDispatcher([]string{canonicalDir})
	require.NoError(t, err)
	_, foundTUISkillFromCLI := cliOnlyReg.Get("tui-installed-only")
	assert.False(t, foundTUISkillFromCLI,
		"the CLI's pre-fix convention (canonical dir alone) must NOT see a skill installed under the legacy TUI directory")
	_, foundOwnSkillFromCLI := cliOnlyReg.Get("cli-installed-only")
	assert.True(t, foundOwnSkillFromCLI, "sanity: the CLI convention sees its own installs")

	// Old TUI/desktop convention: legacy dir alone.
	tuiOnlyReg, _, err := LoadSkillsAndDispatcher([]string{legacyDir})
	require.NoError(t, err)
	_, foundCLISkillFromTUI := tuiOnlyReg.Get("cli-installed-only")
	assert.False(t, foundCLISkillFromTUI,
		"the TUI/desktop's pre-fix convention (legacy dir alone) must NOT see a skill installed under the canonical CLI directory")
	_, foundOwnSkillFromTUI := tuiOnlyReg.Get("tui-installed-only")
	assert.True(t, foundOwnSkillFromTUI, "sanity: the TUI convention sees its own installs")
}

// TestUnifiedHelper_SeesEitherLegacyOrCanonical is the standing GREEN
// regression guard: a skill installed under EITHER directory is visible
// through UserSkillsDirs(), from a single unified scan.
func TestUnifiedHelper_SeesEitherLegacyOrCanonical(t *testing.T) {
	legacyDir, canonicalDir := isolateHomeAndConfig(t)

	writeMinimalSkill(t, legacyDir, "installed-via-legacy")

	reg, _, err := LoadSkillsAndDispatcher(UserSkillsDirs())
	require.NoError(t, err)
	_, found := reg.Get("installed-via-legacy")
	assert.True(t, found, "a skill installed only under the legacy directory must be visible via UserSkillsDirs()")

	// Symmetric direction: install under canonical only, re-scan, still found.
	require.NoError(t, os.RemoveAll(filepath.Join(legacyDir, "installed-via-legacy")))
	writeMinimalSkill(t, canonicalDir, "installed-via-canonical")

	reg2, _, err := LoadSkillsAndDispatcher(UserSkillsDirs())
	require.NoError(t, err)
	_, found2 := reg2.Get("installed-via-canonical")
	assert.True(t, found2, "a skill installed only under the canonical directory must be visible via UserSkillsDirs()")
}

// TestExternalSkillsDir_DefaultsToDotClaudeSkills proves the documented
// zero-config default (T-P5.05, HXC-159 Phase P5): with no
// HELIXCODE_EXTERNAL_SKILLS_DIR set, ExternalSkillsDir resolves to
// ".claude/skills" relative to the working directory — the directory
// scripts/register_skills.sh (T-P1.04) already provisions.
func TestExternalSkillsDir_DefaultsToDotClaudeSkills(t *testing.T) {
	t.Setenv(externalSkillsDirEnvVar, "")
	got := ExternalSkillsDir()
	want := filepath.Join(".", ".claude", "skills")
	if got != want {
		t.Fatalf("ExternalSkillsDir() = %q, want %q", got, want)
	}
}

// TestExternalSkillsDir_ConfigInjected proves the directory is
// config-injected (D-3 / CONST-051(B)) rather than hardcoded: setting
// HELIXCODE_EXTERNAL_SKILLS_DIR overrides the default entirely.
func TestExternalSkillsDir_ConfigInjected(t *testing.T) {
	custom := filepath.Join(t.TempDir(), "some-other-external-source")
	t.Setenv(externalSkillsDirEnvVar, custom)
	got := ExternalSkillsDir()
	if got != custom {
		t.Fatalf("ExternalSkillsDir() = %q, want the injected override %q", got, custom)
	}
}

// TestAllSkillsDirs_ExternalTierIsVisibleAndOverridableByProject is the
// T-P5.05 standing GREEN guard: a skill installed ONLY under the external
// tier is visible through AllSkillsDirs, and a same-named project skill
// still wins over it (project stays highest precedence).
func TestAllSkillsDirs_ExternalTierIsVisibleAndOverridableByProject(t *testing.T) {
	isolateHomeAndConfig(t) // keep legacy/canonical dirs empty and irrelevant here

	externalDir := t.TempDir()
	t.Setenv(externalSkillsDirEnvVar, externalDir)
	writeMinimalSkill(t, externalDir, "external-only-skill")

	projectDir := t.TempDir()
	dirs := AllSkillsDirs(projectDir)

	reg, _, err := LoadSkillsAndDispatcher(dirs)
	require.NoError(t, err)
	_, found := reg.Get("external-only-skill")
	assert.True(t, found, "a skill installed only under the external tier must be visible via AllSkillsDirs")

	// Same name in both external and project dirs — project must win.
	writeMinimalSkill(t, externalDir, "shared-across-external-and-project")
	extDir := filepath.Join(externalDir, "shared-across-external-and-project")
	require.NoError(t, os.WriteFile(filepath.Join(extDir, "SKILL.md"),
		[]byte("---\ndescription: external version\n---\nEXTERNAL BODY\n"), 0o644))

	projSkillDir := filepath.Join(projectDir, "shared-across-external-and-project")
	require.NoError(t, os.MkdirAll(projSkillDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(projSkillDir, "SKILL.md"),
		[]byte("---\ndescription: project version\n---\nPROJECT BODY\n"), 0o644))

	reg2, _, err := LoadSkillsAndDispatcher(AllSkillsDirs(projectDir))
	require.NoError(t, err)
	s, ok := reg2.Get("shared-across-external-and-project")
	require.True(t, ok)
	assert.Equal(t, "project version", s.Description(), "project directory must win over the external tier on a name collision")
}

// TestUserSkillsDirs_CanonicalOverridesLegacyOnCollision proves the
// documented precedence: when the SAME skill name exists under both
// directories, the canonical (forward-looking) directory wins — encouraging
// migration rather than silently freezing the legacy copy as authoritative
// forever.
func TestUserSkillsDirs_CanonicalOverridesLegacyOnCollision(t *testing.T) {
	legacyDir, canonicalDir := isolateHomeAndConfig(t)

	// Same name, different bodies, so the winner is unambiguous.
	legacySkillDir := filepath.Join(legacyDir, "shared-name")
	require.NoError(t, os.MkdirAll(legacySkillDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(legacySkillDir, "SKILL.md"),
		[]byte("---\ndescription: legacy version\n---\nLEGACY BODY\n"), 0o644))

	canonicalSkillDir := filepath.Join(canonicalDir, "shared-name")
	require.NoError(t, os.MkdirAll(canonicalSkillDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(canonicalSkillDir, "SKILL.md"),
		[]byte("---\ndescription: canonical version\n---\nCANONICAL BODY\n"), 0o644))

	reg, _, err := LoadSkillsAndDispatcher(UserSkillsDirs())
	require.NoError(t, err)
	s, ok := reg.Get("shared-name")
	require.True(t, ok)
	assert.Equal(t, "canonical version", s.Description(), "canonical directory must win over legacy on a name collision")
}
