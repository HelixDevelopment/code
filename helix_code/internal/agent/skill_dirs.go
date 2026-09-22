package agent

import (
	"os"
	"path/filepath"
)

// T-P5.03 (HXC-159 Phase P5) — unify the user skills directory.
//
// Before this fix, cmd/cli/main.go used os.UserConfigDir()/helixcode/skills
// while applications/terminal_ui/main.go and internal/clientcore/agentic.go
// used $HOME/.helix/skills — two independent, non-overlapping directories.
// A skill a user installed for one front-end (e.g. via the TUI's documented
// path) was silently invisible to the other (e.g. `helixcode skills list`).
//
// This file is the single, front-end-neutral source of truth for BOTH
// directories, so every call site (CLI, TUI, desktop) converges on the same
// list. See docs/guides/skills_directory_migration.md for the operator-facing
// migration note.

// CanonicalUserSkillsDir returns the canonical, forward-looking user-level
// skills directory: os.UserConfigDir()/helixcode/skills (XDG_CONFIG_HOME on
// Linux, ~/Library/Application Support on macOS, %AppData% on Windows — the
// same base os.UserConfigDir() already uses for the CLI's user commands
// directory, .../helixcode/commands). This is the directory every front-end
// should treat as the primary install target going forward.
func CanonicalUserSkillsDir() (string, error) {
	cfg, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(cfg, "helixcode", "skills"), nil
}

// LegacyUserSkillsDir returns the pre-unification directory the TUI and
// desktop front-ends used exclusively: $HOME/.helix/skills. It is retained as
// a lower-precedence, read-compatibility tier so a skill installed there
// before T-P5.03 remains visible from every front-end without requiring a
// manual move. New installs should target CanonicalUserSkillsDir(); this
// function exists purely for backward-compatible discovery.
//
// Falls back to os.UserHomeDir() when $HOME is unset (covers e.g. some
// Windows shells where USERPROFILE is the populated variable instead), and
// returns "" only when neither resolves — every consumer of this value
// (commands.SkillLoader.Reload, LoadSkillsAndDispatcher) already silently
// skips an empty/nonexistent directory, so callers do not need to filter it.
func LegacyUserSkillsDir() string {
	home := os.Getenv("HOME")
	if home == "" {
		if h, err := os.UserHomeDir(); err == nil {
			home = h
		}
	}
	if home == "" {
		return ""
	}
	return filepath.Join(home, ".helix", "skills")
}

// UserSkillsDirs returns the ordered pair of user-level skill directories a
// front-end should scan, LOWEST precedence first: [legacy, canonical]. Any
// caller assembling a full ordered directory list (for LoadSkillsAndDispatcher
// or an equivalent sequence of commands.SkillLoader passes) should place the
// project directory AFTER this pair, so the existing project > user > built-in
// precedence is preserved with "user" now meaning "legacy, then canonical
// overriding legacy on a name collision".
//
// A caller that only has room for a SINGLE user directory (the CLI's
// two-argument commands.NewSkillLoader(reg, projectDir, userDir) call shape)
// should instead load the legacy directory via a separate, EARLIER
// commands.NewSkillLoader(reg, legacyDir, "").Load() pass, then its own
// (projectDir, canonicalUserDir) pass second — the second pass's `Add` calls
// override same-named entries the legacy pass registered, reproducing the
// same [legacy, canonical, project] precedence without changing
// SkillLoader's two-directory signature. cmd/cli/main.go does exactly this.
func UserSkillsDirs() []string {
	canonical, _ := CanonicalUserSkillsDir() // error → "" → silently skipped downstream
	return []string{LegacyUserSkillsDir(), canonical}
}

// externalSkillsDirEnvVar is the config-injection seam for
// ExternalSkillsDir. Named, not hardcoded, so a caller can point this
// consumer at any provisioner's output directory without a code change —
// per D-3 / CONST-051(B): this consumer must never re-couple itself to one
// specific project layout for a skill source outside its own tree.
const externalSkillsDirEnvVar = "HELIXCODE_EXTERNAL_SKILLS_DIR"

// ExternalSkillsDir returns the "external tier" skills directory (T-P5.05,
// HXC-159 Phase P5): a source of skills provisioned by something OTHER than
// a user hand-authoring files directly into a front-end's own project/user
// directory — e.g. scripts/register_skills.sh (T-P1.04), which links
// constitution-owned governance skills into .claude/skills/ by reference.
//
// Config-injected via the HELIXCODE_EXTERNAL_SKILLS_DIR environment
// variable — never hardcoded to any one provisioner's layout. When unset,
// defaults to ".claude/skills" relative to the current working directory,
// mirroring the existing ".helix/skills" project-dir convention and matching
// the directory scripts/register_skills.sh already provisions in this
// repository. Downstream consumers (commands.SkillLoader,
// LoadSkillsAndDispatcher) already silently skip a nonexistent directory, so
// callers do not need to check existence themselves.
func ExternalSkillsDir() string {
	if v := os.Getenv(externalSkillsDirEnvVar); v != "" {
		return v
	}
	return filepath.Join(".", ".claude", "skills")
}

// AllSkillsDirs returns the FULL ordered directory list a front-end should
// scan for on-disk skills, LOWEST precedence first:
//
//	[legacy user, canonical user, external, projectDir]
//
// projectDir is the caller's own per-project skills directory (conventionally
// ".helix/skills", resolved relative to the caller's own working directory
// convention — this function does not resolve it itself since different
// front-ends may already have their own projectDir value in hand).
// projectDir stays LAST/highest so a project-specific skill still overrides
// a same-named user, external, or built-in skill, matching the precedence
// every existing call site already documented before this tier existed.
// The built-in (embedded) tier is not part of this list — every
// commands.SkillLoader pass reloads it unconditionally regardless of which
// directory it is given, so it is always present at the lowest rank
// implicitly.
func AllSkillsDirs(projectDir string) []string {
	return append(UserSkillsDirs(), ExternalSkillsDir(), projectDir)
}
