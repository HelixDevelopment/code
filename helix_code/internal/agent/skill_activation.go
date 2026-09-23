package agent

import (
	"context"
	"fmt"

	"dev.helix.code/internal/commands"
)

// loadSkillLoader is the SINGLE canonical sequence every front-end
// (interactive REPL bootstrap, `skills` CLI subcommand,
// LoadSkillsAndDispatcher / LoadSkillsAndDispatcherWithLoader) now shares
// to build a correctly-ordered, correctly-precedenced commands.SkillLoader
// + commands.SkillRegistry pair (HXC-159 T-P9.01 round-5 NEW-2/NEW-4
// remediation, 2026-09-23).
//
// Before this fix each front-end hand-rolled its OWN version of this
// sequence — the exact "3+ independently hand-rolled" shape the round-4
// N1/N2 fix's own independent reviewer identified as the root cause of how
// N1/N2/builtin-shadow drifted apart in the first place. Two concrete
// regressions resulted: cmd/cli/main.go's two hand-rolled sequences loaded
// the external tier BEFORE the built-in tier existed in the registry at
// all (the reverse of every other front-end's order), so a builtin-named
// skill re-added moments later by the (then still separately-scoped)
// legacy-dir load silently clobbered an admitted external-tier skill of
// the same name — the same skill name resolved to a DIFFERENT source
// depending on which front-end was asked (NEW-2). And the ONE loader
// instance the CLI kept wired to its filesystem watcher and its `/skills
// reload` / `skills reload` commands used a per-instance
// SkipBuiltins(true) flag that, once set, ALSO silently disabled built-in
// re-registration on every SUBSEQUENT Reload of that same instance — so
// deleting a project override of a builtin and reloading permanently lost
// the builtin with nothing left to restore it (NEW-4).
//
// skillsDirs is the SAME ordered, lowest-to-highest-precedence directory
// list every caller already documents (conventionally
// agent.AllSkillsDirs's [legacy user, canonical user, external, project] —
// see that function's doc comment). A directory equal to
// ExternalSkillsDir() at ANY position in this list is loaded through the
// shared pkg/skills policy gate at exactly that position (never
// unconditionally) via externalTierExtraTier; every other entry is
// scanned as an ordinary on-disk directory. The returned
// *commands.SkillLoader owns the FULL sequence — built-in tier, every
// on-disk directory, and the external tier's policy-gated position — as
// ONE loader instance, so every subsequent Reload() (fsnotify-triggered or
// `/skills reload`/`skills reload`-triggered) replays the IDENTICAL
// sequence, including unconditionally re-establishing the built-in base
// layer, rather than a caller-assembled patchwork of independent loader
// instances whose reload wiring only ever covered a subset of the tiers.
func loadSkillLoader(skillsDirs []string) (*commands.SkillRegistry, *commands.SkillLoader, error) {
	reg := commands.NewSkillRegistry()

	// HXC-159 T-P9.01/F-01: the external tier (ExternalSkillsDir) is
	// populated by something OTHER than this consumer's own project/user
	// authoring, so — unlike the legacy/canonical user and project
	// directories — it MUST cross the shared pkg/skills policy engine
	// (allowlist + activation ceiling + description-similarity + C5
	// sandbox precondition) before any of its skills are exposed to this
	// consumer's SkillRegistry/dispatcher.
	externalDir := ExternalSkillsDir()

	loader := commands.NewMultiDirSkillLoader(reg, skillsDirs)
	if externalDir != "" {
		loader.SetExtraTier(externalDir, externalTierExtraTier{dir: externalDir})
	}

	if err := loader.Load(); err != nil {
		// commands.SkillLoader.Reload() logs and skips every per-tier
		// failure internally (never returns non-nil in practice — see its
		// doc comment) — this branch is defensive, matching the
		// pre-existing "a single broken skill file never blocks the
		// front-end" contract: return the loader/registry as far as they
		// got, never nil, so a caller can still degrade gracefully.
		return reg, loader, fmt.Errorf("skill activation: load skills: %w", err)
	}
	return reg, loader, nil
}

// LoadSkillsAndDispatcherWithLoader is loadSkillLoader plus a ready-to-use
// SkillDispatcher, for callers (cmd/cli/main.go's two front-ends) that also
// need the *commands.SkillLoader handle itself — e.g. to wire a
// commands.SkillsWatcher or a `/skills reload`/`skills reload` command,
// both of which require the concrete loader, not merely a registry +
// dispatcher pair.
func LoadSkillsAndDispatcherWithLoader(skillsDirs []string) (*commands.SkillRegistry, *commands.SkillLoader, *SkillDispatcher, error) {
	reg, loader, err := loadSkillLoader(skillsDirs)
	disp := NewSkillDispatcher(reg, nil)
	if err != nil {
		return reg, loader, disp, err
	}
	return reg, loader, disp, nil
}

// LoadSkillsAndDispatcher loads every `.md`/`SKILL.md` skill found under the
// supplied directories into a fresh SkillRegistry and returns that registry
// together with a ready-to-use SkillDispatcher.
//
// skillsDirs is an ordered list of directories scanned for skill files.
// Later directories override earlier ones on name collision (project dir should
// come last). Non-existent directories are skipped silently; per-file parse
// errors are logged at WARN by the underlying SkillLoader and do NOT fail the
// load (a single broken skill file never blocks the front-end).
//
// A front-end (CLI or TUI) calls this ONCE at startup, then on each user prompt
// runs DispatchSkill against the returned dispatcher. The returned registry is
// retained by the caller for `/skills` introspection and hot-reload.
//
// Callers that also need the underlying *commands.SkillLoader (to wire a
// filesystem watcher or a reload command — cmd/cli/main.go's two
// front-ends) use LoadSkillsAndDispatcherWithLoader instead; both share the
// SAME canonical sequence (loadSkillLoader) — see its doc comment.
func LoadSkillsAndDispatcher(skillsDirs []string) (*commands.SkillRegistry, *SkillDispatcher, error) {
	reg, _, disp, err := LoadSkillsAndDispatcherWithLoader(skillsDirs)
	return reg, disp, err
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
