# User skills directory unification (T-P5.03 + T-P5.05, HXC-159 Phase P5)

**Revision**: 2 · **Last modified**: 2026-09-22 · **Status**: current

## What changed

Before this fix, HelixCode's three front-ends resolved the *user-level*
skills directory (as opposed to the per-project `.helix/skills/` directory)
two different ways, hardcoded independently:

| Front-end | Old user-skills directory |
|---|---|
| CLI (`helixcode` / `cmd/cli`) | `os.UserConfigDir()/helixcode/skills` (e.g. `$XDG_CONFIG_HOME/helixcode/skills` on Linux, `~/Library/Application Support/helixcode/skills` on macOS, `%AppData%/helixcode/skills` on Windows) |
| TUI (`applications/terminal_ui`) | `$HOME/.helix/skills` |
| Desktop (`internal/clientcore`, shared TUI wiring) | `$HOME/.helix/skills` |

A skill a developer installed for one front-end was **invisible** to the
other two — installing a skill via the TUI's documented path never showed up
in `helixcode skills list`, and vice versa.

## What it is now

`internal/agent/skill_dirs.go` is the single source of truth for both
directories:

- `agent.CanonicalUserSkillsDir()` — `os.UserConfigDir()/helixcode/skills`.
  This is the **canonical, forward-looking** location. It was chosen over the
  legacy directory because it is what `os.UserConfigDir()` already uses for
  the CLI's sibling user-commands directory (`.../helixcode/commands`), it
  respects platform config-directory conventions on every OS, and unlike
  `$HOME`-based paths it does not silently produce an empty string on
  platforms where `HOME` is unset.
- `agent.LegacyUserSkillsDir()` — `$HOME/.helix/skills`, retained as a
  **read-compatibility** tier so a skill installed here before this fix
  remains visible without any manual action.
- `agent.UserSkillsDirs()` — returns `[legacy, canonical]`, the ordered pair
  every front-end now scans. The **canonical** directory takes precedence
  over the legacy one on a name collision (encourages migrating forward
  without silently freezing an old copy as authoritative).

All three front-ends (`cmd/cli/main.go`'s `buildSubsystems`/`ensureSubsystems` path,
`cmd/cli/main.go`'s standalone `helixcode skills <subcommand>` dispatcher,
`applications/terminal_ui/main.go`, and `internal/clientcore/agentic.go`) now scan
`[legacy, canonical, project]` in that order, so a skill installed under **either**
user directory is visible from **every** front-end, and the per-project
`.helix/skills/` directory continues to win on any collision.

**T-P5.05 addendum**: a fourth tier — `agent.ExternalSkillsDir()`, conventionally
`.claude/skills` relative to the working directory, config-injected via the
`HELIXCODE_EXTERNAL_SKILLS_DIR` environment variable — was added between the
user tiers and the project tier for skills provisioned by something *other*
than a front-end's own project/user directory (e.g. `scripts/register_skills.sh`,
which links constitution-owned governance skills into `.claude/skills/` by
reference). The full precedence order every front-end now scans is
`[legacy, canonical, external, project]` — see `agent.AllSkillsDirs`.

## Do I need to do anything?

**No — this is a read-compatibility fix, not a breaking change.** If you
already have skills installed under `$HOME/.helix/skills`, they continue to
work from every front-end with no action required.

**If you want to migrate forward** (optional): move your skill files from
`$HOME/.helix/skills/<name>/SKILL.md` (or the legacy flat `$HOME/.helix/skills/<name>.md`
form) to the canonical directory:

- **Linux**: `${XDG_CONFIG_HOME:-$HOME/.config}/helixcode/skills`
- **macOS**: `~/Library/Application Support/helixcode/skills`
- **Windows**: `%AppData%\helixcode\skills`

(This is exactly what Go's standard `os.UserConfigDir()` resolves to on each
platform — there is currently no dedicated `helixcode skills` flag that
prints it; `helixcode skills list` shows registered skills regardless of
which of the two user directories they came from, which is the point of this
fix — you do not need to know the exact path to confirm a skill is visible.)

## Known limitation

Hot-reload (the `fsnotify`-backed watcher that live-updates the registry when
a skill file changes on disk) watches the **project** and **canonical**
directories, matching the pre-fix watched set. The **legacy** directory is
scanned once at startup as a static compatibility tier; a change made to a
skill file under the legacy directory while a front-end is already running
requires a restart to be picked up. This is a deliberate, documented scope
boundary (§11.4.6) rather than an oversight — new work should target the
canonical directory, which is fully hot-reloadable.

## Cross-references

- Constitution §11.4.108 (runtime-signature-as-definition-of-done) — the
  observable proving this fix: a single skill file, installed once under
  either directory, is discoverable from every front-end.
- `specs/001-helixskills-incorporation/tasks.md`, T-P5.03.
- `internal/agent/skill_dirs.go`, `internal/agent/skill_dirs_test.go`.
