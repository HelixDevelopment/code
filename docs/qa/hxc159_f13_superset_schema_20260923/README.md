# HXC-159 F-13 — Superset Frontmatter Schema (Option C) — Evidence Transcript

| Field | Value |
|---|---|
| Item | HXC-159 finding F-13 (`specs/001-helixskills-incorporation/tasks.md`) |
| Date | 2026-09-23 |
| Option chosen | Option C — superset schema, single canonical source (per the operator's decision, following `docs/research/hxc159_f13_architectural_options_20260923.md`) |
| Result | FIXED — `constitution/skills/*/SKILL.md`'s frontmatter now optionally carries `triggers:`/`requires_isolation:` alongside `name:`/`description:`/`version:`; `builtin_skills/` is generated from it; the generator's regenerate mode is safe; a permanent drift-regression guard (`TestBuiltinSkills_NoDrift`) is wired into the normal `go test` run. |

## Superset schema designed

```yaml
---
name: <string>                 # native (Claude Code semantic discovery)
description: <string>          # native — ALSO read by the builtin REPL loader
version: <semver>              # native
triggers:                      # OPTIONAL — builtin REPL loader only, ignored elsewhere
  - "<regex>"
  - ...
requires_isolation: <bool>     # OPTIONAL — builtin REPL loader only, ignored elsewhere; default false
---
```

Both consumers verified by reading their actual parsing code (not assumed):

- `helix_code/internal/commands/markdown_skills.go`'s `skillFrontmatter` struct only
  declares `description`/`triggers`/`variables`/`requires_isolation` — `name`/`version`
  are unmapped YAML keys, silently dropped by `gopkg.in/yaml.v3`'s default (non-strict)
  `Unmarshal`. No `KnownFields(true)`/strict-decoder usage exists anywhere in the
  codebase (`grep -rn "KnownFields\|yaml.NewDecoder"` — zero hits).
- `submodules/skills/pkg/skills/frontmatter.go`'s stdlib-only parser explicitly
  models BOTH `triggers` and `requires_isolation` as first-class known keys in its
  `partition()` function, and `loader.go:218-220` populates `prof.Triggers` /
  `prof.RequiresIsolation` from them — `requires_isolation: false` leaves
  `RequiresIsolation` at its zero-value `false` (no behaviour change), and
  `prof.Triggers` is populated but read nowhere else in the current tree
  (`grep -rn "\.Triggers\b" submodules/skills/pkg/skills/*.go` — one hit, the
  assignment itself) — inert for the external tier today, confirming the F-17
  sibling stream's in-flight admission work is unaffected by this change.

## RED — regenerate mode fails on the CURRENT (pre-superset) constitution frontmatter

Run for real on the live tree (constitution/skills frontmatter still native-only at
this point), then reverted before any commit:

```
$ cd /home/milosvasic/Projects/helix_code/helix_code
$ go test -count=1 -tags nogui -run TestGovernanceSkills_RegisteredAndInvocable ./internal/commands/...
ok  	dev.helix.code/internal/commands	0.037s        # baseline, pre-experiment
ok  	dev.helix.code/internal/commands/builtin	0.031s

$ cd /home/milosvasic/Projects/helix_code
$ bash helix_code/scripts/generate_builtin_skills.sh
[generate-builtin-skills] synced action-prefix-system <- constitution/skills/action-prefix-system
[generate-builtin-skills] SKIP conventional-commit (no constitution/skills/conventional-commit — not constitution-sourced)
[generate-builtin-skills] synced media-validator <- constitution/skills/media-validator
[generate-builtin-skills] synced multitrack <- constitution/skills/multitrack
[generate-builtin-skills] synced reporting-workable-items <- constitution/skills/reporting-workable-items
[generate-builtin-skills] synced scheduled-work-queue <- constitution/skills/scheduled-work-queue
[generate-builtin-skills] synced session-sync <- constitution/skills/session-sync
[generate-builtin-skills] synced workable-item-lifecycle <- constitution/skills/workable-item-lifecycle
[generate-builtin-skills] done — synced=7 skipped=1

$ cd helix_code
$ go test -count=1 -tags nogui -run TestGovernanceSkills_RegisteredAndInvocable -v ./internal/commands/...
--- FAIL: TestGovernanceSkills_RegisteredAndInvocable (0.00s)
    --- FAIL: TestGovernanceSkills_RegisteredAndInvocable/action-prefix-system (0.00s)
        governance_skills_registered_test.go:123: ... governance skill "action-prefix-system"
        declares no triggers, so auto-trigger can never fire
    [... all 7 subtests FAIL identically ...]
FAIL
```

**Reverted immediately** (index.lock was live from a concurrent sibling session, so
reverted via `git show HEAD:<path> > <path>` — a read-only git op needing no lock —
rather than `git checkout --`), verified by sha256 comparison against the
pre-experiment baseline: **identical**.

```
$ go test -count=1 -tags nogui -run TestGovernanceSkills_RegisteredAndInvocable ./internal/commands/...
ok  	dev.helix.code/internal/commands	0.078s        # GREEN again post-revert
```

## Schema implemented — 7 `constitution/skills/*/SKILL.md` files edited (frontmatter only)

`action-prefix-system`, `media-validator`, `multitrack`, `reporting-workable-items`,
`scheduled-work-queue`, `session-sync`, `workable-item-lifecycle` — each gained
`triggers:` + `requires_isolation: false` in its frontmatter block, body untouched.
The trigger regex lists were carried forward VERBATIM from the pre-existing
`builtin_skills/<name>/SKILL.md` copies (diffed byte-for-byte against the new
constitution source below — identical), never invented (§11.4.6).

## GREEN — regenerate mode is now safe on the superset source

```
$ bash helix_code/scripts/generate_builtin_skills.sh
[generate-builtin-skills] synced action-prefix-system <- constitution/skills/action-prefix-system
[generate-builtin-skills] SKIP conventional-commit (no constitution/skills/conventional-commit — not constitution-sourced)
[generate-builtin-skills] synced media-validator <- constitution/skills/media-validator
[generate-builtin-skills] synced multitrack <- constitution/skills/multitrack
[generate-builtin-skills] synced reporting-workable-items <- constitution/skills/reporting-workable-items
[generate-builtin-skills] synced scheduled-work-queue <- constitution/skills/scheduled-work-queue
[generate-builtin-skills] synced session-sync <- constitution/skills/session-sync
[generate-builtin-skills] synced workable-item-lifecycle <- constitution/skills/workable-item-lifecycle
[generate-builtin-skills] done — synced=7 skipped=1

$ cd helix_code && go test -count=1 -tags nogui -run TestGovernanceSkills_RegisteredAndInvocable -v ./internal/commands/...
--- PASS: TestGovernanceSkills_RegisteredAndInvocable (0.00s)
    --- PASS: .../action-prefix-system (0.00s)
    --- PASS: .../media-validator (0.00s)
    --- PASS: .../multitrack (0.00s)
    --- PASS: .../reporting-workable-items (0.00s)
    --- PASS: .../scheduled-work-queue (0.00s)
    --- PASS: .../session-sync (0.00s)
    --- PASS: .../workable-item-lifecycle (0.00s)
PASS
ok  	dev.helix.code/internal/commands	0.047s

$ go test -count=1 -tags nogui ./internal/commands/...      # full package suite
ok  	dev.helix.code/internal/commands	3.474s
ok  	dev.helix.code/internal/commands/builtin	0.745s

$ bash helix_code/scripts/generate_builtin_skills.sh --check
[generate-builtin-skills] PASS — 0 drifted, 1 skipped (non-constitution-sourced)
```

## Trigger-phrase / description content diff — confirming what did and did not change

```
$ diff <(git show HEAD:helix_code/internal/commands/builtin_skills/<name>/SKILL.md | sed -n '/^triggers:/,/^requires_isolation:/p') \
       <(sed -n '/^triggers:/,/^requires_isolation:/p' helix_code/internal/commands/builtin_skills/<name>/SKILL.md)
IDENTICAL          # for all 7 skills — trigger regex + requires_isolation unchanged
```

`description:` and the body DID change — from the old short REPL-specific text to the
richer, actively-maintained constitution documentation. This is the intended,
deliberate consequence of unifying the source of truth (closing the staleness gap the
original F-13 investigation's §7 flagged — e.g. `action-prefix-system`'s constitution
description had already grown two invocation forms the old builtin copy never learned
about). The task's "no user-visible behaviour change" constraint is scoped to REPL
**trigger phrases** (which utterance routes to which skill) — proven unchanged above —
not to the skill's description/body text, which is the entire point of Option C.

## Real build + run of the actual `helixcode` binary (not just `go test`)

```
$ cd helix_code && go build -tags nogui -o /tmp/.../helixcode_cli ./cmd/cli/
$ echo exit=$?
exit=0
$ ls -la /tmp/.../helixcode_cli
-rwxrwxr-x 1 ... 127519448 Sep 23 14:18 helixcode_cli

$ mkdir -p /tmp/.../clean_run_dir && cd /tmp/.../clean_run_dir   # EMPTY dir, no .helix/skills
$ HOME=/tmp/.../clean_run_dir_home /tmp/.../helixcode_cli skills list
NAME                      DESCRIPTION                                    TRIGGERS                                          SOURCE
action-prefix-system      Use when a prompt begins with an UPPERCASE ... (?i)^what does the ... | (?i)^(?:what|how)...      builtin:action-prefix-system/SKILL.md
conventional-commit       Draft a Conventional Commits message ...      (?i)^commit message for ...                        builtin:conventional-commit/SKILL.md
media-validator            ...                                          (?i)^validate (?:the )?...                        builtin:media-validator/SKILL.md
multitrack                 ...                                         (?i)^(?:how do i |how to )?...                     builtin:multitrack/SKILL.md
reporting-workable-items   ...                                         (?i)^report (?:a |an )?...                         builtin:reporting-workable-items/SKILL.md
scheduled-work-queue       ...                                         (?i)^remind me to (?P<task>.+)$ | ...              builtin:scheduled-work-queue/SKILL.md
session-sync                ...                                        (?i)^(?:sync|pull|push) ...                        builtin:session-sync/SKILL.md
workable-item-lifecycle     ...                                        (?i)^how do i (?:properly )?...                    builtin:workable-item-lifecycle/SKILL.md

$ HOME=/tmp/.../clean_run_dir_home /tmp/.../helixcode_cli skills show scheduled-work-queue
Name: scheduled-work-queue
Description: Teaches an agent to record background/deferred work via the scheduled-work MCP server ...
Source: builtin:scheduled-work-queue/SKILL.md
Requires isolation: false
Triggers:
  (?i)^remind me to (?P<task>.+)$
  (?i)^(?:schedule|queue|defer) (?:this |that )?(?:work |task )?(?:to |for )?(?P<task>.+)$

--- Body ---
# Scheduled-Work Queue Skill
## Purpose
Teaches an agent to use the **scheduled-work** MCP server ...
[full constitution-sourced documentation body, confirming the generated content]
```

All 8 built-in skills (including `conventional-commit`, untouched) resolve from the
embedded `builtin:` tier with `SourcePath` proving zero-filesystem provisioning — the
exact HXC-163 runtime signature `TestGovernanceSkills_RegisteredAndInvocable` already
asserts, now independently reproduced at the compiled-binary level.

`agent.SkillDispatcher.Match` (`helix_code/internal/agent/skill_dispatcher.go`) — the
production REPL auto-trigger entry point — calls `registry.FindMatching(input)` then
`skill.RenderWithCaptures(...)`, the EXACT same call sequence
`TestGovernanceSkills_RegisteredAndInvocable` exercises; confirmed by reading the
source (not assumed), so the Go test's runtime signature already covers the real
production auto-trigger code path.

## Drift-regression guard — RED (real injected drift) then GREEN

New test: `helix_code/internal/commands/builtin_skills_drift_test.go` —
`TestBuiltinSkills_NoDrift`, wired into the normal `go test ./internal/commands/...`
run (never requiring a manual `go generate` invocation, unlike the pre-existing
`//go:generate ... --check` directive).

```
$ echo '# stray drift line' >> internal/commands/builtin_skills/multitrack/SKILL.md
$ go test -count=1 -tags nogui -run TestBuiltinSkills_NoDrift -v ./internal/commands/...
=== RUN   TestBuiltinSkills_NoDrift
    builtin_skills_drift_test.go:59: internal/commands/builtin_skills has drifted from
        constitution/skills (HXC-159 F-13 regression: ...)
        [generate-builtin-skills] DRIFT multitrack — differs from constitution/skills/multitrack:
            differs: SKILL.md
        [generate-builtin-skills] FAIL — 1 drifted (run without --check to regenerate)
--- FAIL: TestBuiltinSkills_NoDrift (0.58s)
FAIL

$ bash ../scripts/generate_builtin_skills.sh   # from repo root; resyncs, wipes the stray line
[generate-builtin-skills] synced multitrack <- constitution/skills/multitrack

$ go test -count=1 -tags nogui -run TestBuiltinSkills_NoDrift -v ./internal/commands/...
--- PASS: TestBuiltinSkills_NoDrift (0.17s)
PASS
```

## Final full-suite verification

```
$ go test -count=1 -tags nogui ./internal/commands/...
ok  	dev.helix.code/internal/commands	1.632s
ok  	dev.helix.code/internal/commands/builtin	0.104s
?   	dev.helix.code/internal/commands/builtin/i18n	[no test files]
?   	dev.helix.code/internal/commands/i18n	[no test files]

$ bash helix_code/scripts/generate_builtin_skills.sh --check
[generate-builtin-skills] PASS — 0 drifted, 1 skipped (non-constitution-sourced)

$ gofmt -l helix_code/internal/commands/markdown_skills.go helix_code/internal/commands/builtin_skills_drift_test.go
[empty — clean]

$ bash -n helix_code/scripts/generate_builtin_skills.sh
[exit 0]
```

## What was not fully resolved / honestly deferred

- The F-17 sibling stream (per-skill admission redesign for the external tier) is a
  concurrently-owned, unrelated finding — untouched by this work. Verified inert
  (constitution/skills's new `triggers`/`requires_isolation` fields are read but
  unused elsewhere in `pkg/skills` today), but a future F-17 landing that begins
  consuming `prof.Triggers` for real should independently re-verify these 7 skills'
  regex lists don't introduce new similarity-detector collisions.
- No second consumer's own test suite (Claude Code itself, HelixAgent) was run as
  part of this task — verified instead by reading their parsing code, per the task's
  "or at minimum confirm the actual parsing code doesn't have a strict-unknown-keys
  mode" allowance.
