package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// repoSkillsDir resolves the real `.helix/skills` directory shipped in the repo
// (two levels up from internal/agent). The sample skill explain-arch.md lives
// there so the activation path is demonstrated against a REAL on-disk skill,
// not a synthetic in-memory fixture.
func repoSkillsDir(t *testing.T) string {
	t.Helper()
	dir := filepath.Join("..", "..", ".helix", "skills")
	abs, err := filepath.Abs(dir)
	require.NoError(t, err)
	if _, err := os.Stat(filepath.Join(abs, "explain-arch.md")); err != nil {
		t.Fatalf("sample skill not found at %s: %v", abs, err)
	}
	return abs
}

func TestLoadSkillsAndDispatcher_RealSampleSkillMatchesAndRenders(t *testing.T) {
	reg, disp, err := LoadSkillsAndDispatcher([]string{repoSkillsDir(t)})
	require.NoError(t, err)
	require.NotNil(t, reg)
	require.NotNil(t, disp)

	// The real sample skill is registered under its filename stem.
	s, ok := reg.Get("explain-arch")
	require.True(t, ok, "explain-arch skill loaded from .helix/skills")
	assert.NotEmpty(t, s.Description())

	// DispatchSkill runs the regex trigger and renders the body with the named
	// capture group topic=ensemble substituted.
	rendered, matched := DispatchSkill(disp, "explain ensemble architecture")
	require.True(t, matched, "the sample skill's trigger matched the prompt")
	assert.True(t, strings.Contains(rendered, "Architecture: ensemble"),
		"rendered heading carries the captured topic=ensemble (got: %q)", rendered)
	// The frontmatter default variable audience=engineer is substituted too.
	assert.True(t, strings.Contains(rendered, "engineer audience"),
		"rendered body substituted the default audience variable")
	// No raw {{ARG.topic}} placeholder must remain after rendering.
	assert.False(t, strings.Contains(rendered, "{{ARG.topic}}"),
		"no unrendered {{ARG.topic}} placeholder remains")
}

func TestDispatchSkill_NoMatchFallsThrough(t *testing.T) {
	_, disp, err := LoadSkillsAndDispatcher([]string{repoSkillsDir(t)})
	require.NoError(t, err)

	rendered, matched := DispatchSkill(disp, "what is the weather today")
	assert.False(t, matched, "non-triggering prompt does not match any skill")
	assert.Empty(t, rendered)
}

func TestDispatchSkill_NilDispatcherSafe(t *testing.T) {
	rendered, matched := DispatchSkill(nil, "explain ensemble architecture")
	assert.False(t, matched)
	assert.Empty(t, rendered)
}

// TestLoadSkillsAndDispatcher_EarlierDirOverrideOfBuiltinSurvivesLaterDirLoad
// is the "builtin-shadow-prune-ordering" investigation LoadExternalTierIntoRegistry
// resolved (HXC-159 T-P9.01 round-4/F-21 reviewer's related-but-unverified
// concern, 2026-09-23): does LoadSkillsAndDispatcher's tier loop correctly
// keep an EARLIER directory's override of a builtin-named skill after a
// LATER directory's independent commands.SkillLoader pass runs?
//
// Root cause (broader than the F-21 reviewer's specific prune-step
// question): commands.SkillLoader.Reload() unconditionally re-registers the
// WHOLE embedded built-in tier on EVERY call (loadBuiltinSkills(want) runs
// first, then `for name, s := range want { l.registry.Add(s) }` Adds every
// builtin name unconditionally). LoadSkillsAndDispatcher constructs a FRESH
// commands.SkillLoader PER DIRECTORY in its loop — so every directory AFTER
// the first one that overrides a builtin-named skill silently REVERTS that
// override back to the builtin value, even when the later directory does
// NOT itself define that name, because each fresh loader instance's own
// removal-tracking (l.loaded) starts empty and its unconditional builtin
// re-Add clobbers whatever was already in the shared registry.
//
// This is directly the mechanism the F-21 reviewer's ordering question was
// worried about (a later builtin-reload silently undoing an earlier tier's
// work) — proven here with the general "any later directory, not just the
// external tier's own prune step" shape, and confirmed on the real embedded
// builtin corpus (conventional-commit, HelixCode's own unconditional
// built-in per markdown_skills.go's doc comment).
//
// RED on the pre-fix code: legacyDir overrides "conventional-commit"; a
// SUBSEQUENT, unrelated canonicalDir (which does NOT itself mention
// "conventional-commit") is then loaded and silently reverts the override
// back to the builtin body. GREEN post-fix (commands.SkillLoader.SkipBuiltins):
// only the FIRST loader instance in the sequence registers builtins; every
// later directory's independent pass never re-touches a builtin name it does
// not itself define, so the earlier override survives.
func TestLoadSkillsAndDispatcher_EarlierDirOverrideOfBuiltinSurvivesLaterDirLoad(t *testing.T) {
	const builtinName = "conventional-commit"

	legacyDir := t.TempDir()
	overrideDir := filepath.Join(legacyDir, builtinName)
	require.NoError(t, os.MkdirAll(overrideDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(overrideDir, "SKILL.md"),
		[]byte("---\ndescription: overridden conventional-commit\n---\nOVERRIDDEN BODY\n"), 0o644))

	// canonicalDir is a genuinely UNRELATED directory: it does not mention
	// builtinName at all, yet its independent load pass must not silently
	// revert legacyDir's override.
	canonicalDir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(canonicalDir, "unrelated-skill"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(canonicalDir, "unrelated-skill", "SKILL.md"),
		[]byte("---\ndescription: unrelated\n---\nunrelated body\n"), 0o644))

	reg, _, err := LoadSkillsAndDispatcher([]string{legacyDir, canonicalDir})
	require.NoError(t, err)

	s, ok := reg.Get(builtinName)
	require.True(t, ok, "sanity: %s must be registered", builtinName)
	assert.Equal(t, "OVERRIDDEN BODY", s.Body(),
		"an earlier directory's override of a builtin-named skill must survive a LATER, unrelated directory's independent load pass — got body %q (reverted to builtin)", s.Body())

	// Sanity: the unrelated directory's own skill still loaded correctly.
	_, ok = reg.Get("unrelated-skill")
	assert.True(t, ok, "the later directory's own skill must still load")
}

func TestLoadSkillsAndDispatcher_EmptyAndMissingDirsAreSafe(t *testing.T) {
	reg, disp, err := LoadSkillsAndDispatcher([]string{"", filepath.Join(t.TempDir(), "does-not-exist")})
	require.NoError(t, err)
	require.NotNil(t, reg)
	require.NotNil(t, disp)
	// Built-in (bundled, //go:embed) skills are always present regardless of the
	// on-disk dirs. Empty/missing dirs must therefore contribute NO on-disk skill:
	// every skill in the registry must be a built-in (SourcePath "builtin:...").
	// This still FAILs if an on-disk skill ever leaked from an empty/missing dir
	// (the original invariant this test guards), so it is not a tautology.
	for _, s := range reg.List() {
		assert.True(t, strings.HasPrefix(s.SourcePath(), "builtin:"),
			"empty/missing dirs must load no on-disk skill; got %q from %q", s.Name(), s.SourcePath())
	}
}
