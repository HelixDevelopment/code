package agent

import (
	"context"
	"fmt"
	"log"

	"dev.helix.code/internal/commands"
)

// LoadSkillsAndDispatcher loads every `.md` skill found under the supplied
// directories into a fresh SkillRegistry and returns that registry together
// with a ready-to-use SkillDispatcher built on top of it.
//
// skillsDirs is an ordered list of directories scanned for `*.md` skill files.
// Later directories override earlier ones on name collision (project dir should
// come last). Non-existent directories are skipped silently; per-file parse
// errors are logged at WARN by the underlying SkillLoader and do NOT fail the
// load (a single broken skill file never blocks the front-end).
//
// A front-end (CLI or TUI) calls this ONCE at startup, then on each user prompt
// runs DispatchSkill against the returned dispatcher. The returned registry is
// retained by the caller for `/skills` introspection and hot-reload.
func LoadSkillsAndDispatcher(skillsDirs []string) (*commands.SkillRegistry, *SkillDispatcher, error) {
	reg := commands.NewSkillRegistry()

	// HXC-159 T-P9.01/F-01: the external tier (ExternalSkillsDir) is
	// populated by something OTHER than this consumer's own project/user
	// authoring, so — unlike the legacy/canonical user and project
	// directories — it MUST cross the shared pkg/skills policy engine
	// (allowlist + activation ceiling + description-similarity + C5
	// sandbox precondition) before any of its skills are exposed to this
	// consumer's SkillRegistry/dispatcher. Resolved once, compared by
	// value against each directory in the ordered list below.
	externalDir := ExternalSkillsDir()

	// The embedded built-in tier is registered EXPLICITLY, exactly once,
	// UP FRONT — regardless of what skillsDirs contains (including the
	// degenerate case where skillsDirs holds ONLY the external tier, or is
	// entirely empty/nonexistent). Built-in skills are "always present
	// regardless of the on-disk dirs" (see
	// TestLoadSkillsAndDispatcher_EmptyAndMissingDirsAreSafe) — a contract
	// this explicit up-front load makes unconditional rather than an
	// incidental side effect of however the loop below happens to be
	// structured.
	builtinLoader := commands.NewSkillLoader(reg, "", "")
	if err := builtinLoader.Load(); err != nil {
		return nil, nil, fmt.Errorf("skill activation: load built-in tier: %w", err)
	}

	// SkillLoader's two-slot (userDir, projectDir) contract overrides user with
	// project on collision. Generalise to an arbitrary ordered list by reloading
	// once per directory with the SAME registry: each Reload reconciles the
	// registry for that directory's files, and because each loader tracks only
	// the files IT loaded, later directories add/override without removing the
	// skills contributed by earlier directories.
	//
	// Every per-directory loader below calls SkipBuiltins(true): the
	// built-in tier was already registered above, and
	// commands.SkillLoader.Reload() would otherwise unconditionally re-Add
	// the WHOLE built-in tier on EVERY call — a fresh loader instance per
	// directory (as this loop constructs) would let each directory AFTER
	// the first one silently REVERT any earlier directory's override of a
	// builtin-named skill back to the builtin value, even when that later
	// directory does not itself mention the overridden name at all (HXC-159
	// T-P9.01 round-4 investigation, 2026-09-23; the general form of the
	// F-21 reviewer's unverified "builtin-shadow-prune-ordering" concern).
	// The external tier never participates in this at all:
	// LoadExternalTierIntoRegistry registers ONLY the pkg/skills-admitted
	// skill set directly (see its doc comment) and never re-invokes
	// commands.SkillLoader, so it can never re-touch a builtin name either.
	for _, dir := range skillsDirs {
		if dir == "" {
			continue
		}
		if dir == externalDir {
			// A malformed skill anywhere in the external tier (pkg/skills'
			// Load is fail-closed over the WHOLE source, T-P6.04's
			// operator-visible-failures direction) or a genuine policy
			// refusal MUST NOT abort loading the legacy/canonical/project
			// tiers — this consumer's pre-existing, documented contract
			// ("a single broken skill file never blocks the front-end")
			// applies across tiers, not only within one. Log and continue,
			// matching the existing per-directory WARN-and-skip philosophy
			// commands.SkillLoader already uses for every other tier.
			if err := LoadExternalTierIntoRegistry(reg, dir); err != nil {
				log.Printf("⚠️  skill activation: external tier %q load failed: %v", dir, err)
			}
			continue
		}
		loader := commands.NewSkillLoader(reg, dir, "")
		loader.SkipBuiltins(true)
		if err := loader.Load(); err != nil {
			return nil, nil, fmt.Errorf("skill activation: load %q: %w", dir, err)
		}
	}

	disp := NewSkillDispatcher(reg, nil)
	return reg, disp, nil
}

// DispatchSkill runs the dispatcher's trigger matching against a raw user
// prompt and returns the rendered skill body when a trigger matches.
//
// On a match it returns (renderedBody, true): the matched skill's body with its
// default variables and the regex's named capture groups substituted. The
// front-end injects this rendered body into the prompt sent to the LLM (or runs
// it as a sub-prompt). On no match — or any render error — it returns ("", false)
// so the caller falls through to the ordinary prompt path unchanged.
//
// selection/currentFile are not part of this convenience signature; the
// dispatcher's Match is called with empty selection/currentFile. Callers needing
// editor context should call dispatcher.Match directly.
func DispatchSkill(dispatcher *SkillDispatcher, prompt string) (rendered string, matched bool) {
	if dispatcher == nil {
		return "", false
	}
	out, _, _, ok, err := dispatcher.Match(context.Background(), prompt, "", "")
	if err != nil || !ok {
		return "", false
	}
	return out, true
}
