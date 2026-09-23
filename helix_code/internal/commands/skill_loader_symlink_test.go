package commands

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// HXC-159 T-P9.01 independent-review finding (2026-09-23): the external tier
// (.claude/skills, T-P5.05) is populated by scripts/register_skills.sh
// (T-P1.04) as SYMLINKS to constitution/skills/<name>. os.ReadDir's
// DirEntry.IsDir() is false for a symlink, so the packaged-manifest pass
// skipped every registrar-produced entry: RS-01 (registrar ->
// `helixcode skills list`) was never true end-to-end even though each half
// passed in isolation (the T-P5.05 e2e fixture was a real directory). Measured
// on the live tree with the review build: a symlinked `skill-catalog` loads 0
// times, a real-directory copy loads 1 time
// (qa-results/hxc159/review/20260923T_review1/11_external_tier_symlink_isolation.log).
//
// RED_MODE=1 asserts the defect (symlinked skill dir invisible) and therefore
// FAILS on the fixed tree; RED_MODE=0 (default) is the standing guard
// (§11.4.135 / §11.4.115).
func TestSkillLoader_FollowsSymlinkedSkillDirectory(t *testing.T) {
	target := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(target, "linked-skill"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(target, "linked-skill", "SKILL.md"),
		[]byte("---\ndescription: reached through a symlink\ntriggers: [\"^linked\"]\n---\nlinked body"), 0o644))

	skills := filepath.Join(t.TempDir(), ".claude", "skills")
	require.NoError(t, os.MkdirAll(skills, 0o755))
	require.NoError(t, os.Symlink(filepath.Join(target, "linked-skill"), filepath.Join(skills, "linked-skill")))

	reg := NewSkillRegistry()
	loader := NewSkillLoader(reg, skills, "")
	require.NoError(t, loader.Load())
	_, ok := reg.Get("linked-skill")
	if os.Getenv("RED_MODE") == "1" {
		require.False(t, ok, "RED_MODE=1 expects the defect (symlinked skill dir invisible); it was loaded — defect absent, fix present")
		return
	}
	require.True(t, ok, "a symlinked skill directory (the registrar's output shape) must be discovered by the packaged-manifest pass")

	// A dangling symlink must be skipped silently, never crash or register.
	require.NoError(t, os.Symlink(filepath.Join(target, "does-not-exist"), filepath.Join(skills, "dangling")))
	require.NoError(t, loader.Reload())
	_, ok = reg.Get("dangling")
	require.False(t, ok, "a dangling symlink must not register a skill")
	// A symlink to a plain FILE (not a directory) is likewise not a packaged skill.
	require.NoError(t, os.WriteFile(filepath.Join(target, "plain.txt"), []byte("x"), 0o644))
	require.NoError(t, os.Symlink(filepath.Join(target, "plain.txt"), filepath.Join(skills, "filelink")))
	require.NoError(t, loader.Reload())
	_, ok = reg.Get("filelink")
	require.False(t, ok, "a symlink to a file must not register a skill")
}
