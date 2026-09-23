# HXC-159 T-P9.01 round-5: NEW-1/NEW-2/NEW-4/NEW-5 remediation

**Date:** 2026-09-23
**Scope:** Fixes the four regressions the round-4 N1/N2 fix's (`97e8c852`)
own required independent review found in that same commit — recorded in
`specs/001-helixskills-incorporation/tasks.md`'s NEW-1..5 finding row.

## What changed

- `helix_code/internal/commands/markdown_skills.go`:
  - Exported `ParseSkillFile` (production yaml.v3-based parser, alongside
    the pre-existing test-only `ParseSkillForTest`, now a thin alias).
  - Generalised `SkillLoader` from a fixed two-slot (`projectDir`,
    `userDir`) shape to an arbitrary ordered `dirs []string` list
    (`NewMultiDirSkillLoader`), with the built-in tier loaded
    UNCONDITIONALLY on every `Reload()` — removing the `SkipBuiltins(bool)`
    sticky-flag design that caused NEW-4.
  - Added the `ExtraTier` interface + `SetExtraTier` so a caller-owned,
    policy-gated tier (the external skills tier, which cannot live in this
    package without an import cycle) can be folded into the SAME `Reload()`
    sequence at its declared position — closing NEW-2's ordering
    divergence.
- `helix_code/internal/agent/skill_external_policy.go`:
  - `skillsFromPolicySet` no longer builds a `*commands.Skill` from
    pkg/skills' own already-parsed struct fields (`commands.NewSkill` +
    `XHelixCode.Triggers`/`RequiresIsolation`). It now re-reads each
    admitted skill's OWN manifest from disk, verifies its SHA-256 against
    the `Hash` pkg/skills computed at evaluation time (fail-closed on
    mismatch — a free provenance check), and parses it through
    `commands.ParseSkillFile` — the SAME production yaml.v3 parser every
    other tier already uses. This is NEW-1/NEW-5's fix.
  - Added `externalTierSkills` (shared by `LoadExternalTierIntoRegistry`'s
    pre-existing one-shot form and the new `externalTierExtraTier`
    `commands.ExtraTier` adapter) so there is exactly one implementation of
    "what does the external tier currently admit."
- `helix_code/internal/agent/skill_activation.go`:
  - New `loadSkillLoader` / `LoadSkillsAndDispatcherWithLoader` — the ONE
    canonical sequence every front-end now shares. `LoadSkillsAndDispatcher`
    is now a thin wrapper over it.
- `helix_code/cmd/cli/main.go`:
  - Both hand-rolled `[external, legacy, canonical+project]` sequences
    (interactive REPL bootstrap + the `skills` Cobra subcommand dispatcher)
    replaced with a single call to
    `agent.LoadSkillsAndDispatcherWithLoader(agent.AllSkillsDirs(...))` —
    the exact divergence NEW-2 exploited no longer exists; the reload path
    NEW-4 exploited is now owned by the SAME loader as every other tier.
- New/extended tests: `helix_code/internal/agent/skill_new1_new5_test.go`,
  `helix_code/internal/agent/skill_activation_test.go` (NEW-2/NEW-4 guards),
  `helix_code/internal/agent/skill_external_policy_test.go` (minor-item
  Dir-collision coverage gap).

## Captured evidence (this directory)

- `RED_new1_new5_v2.txt` — NEW-1 + NEW-5 reproduced at RUNTIME against the
  real, unmodified `97e8c852` code (source files stashed to HEAD, new test
  file kept in place, `go test` run, restored afterward — see the task's
  final report for the exact procedure).
- `GREEN_new1_new5_v2.txt` — the SAME tests, fixed code restored: both PASS.
- `RED_new4_old_api_realtopology.txt` — NEW-4 reproduced at RUNTIME using
  ONLY the pre-existing `97e8c852` `commands.SkillLoader`/`SkipBuiltins`
  API, in the CLI's real two-loader topology: after deleting a project
  override and reloading the loader the CLI actually keeps wired, the
  builtin is provably GONE from the registry (`ok=false`), matching NEW-4's
  "permanently removes" description exactly.
- `GREEN_new2_new4.txt` — the NEW-2 (external overrides builtin on a
  genuine, non-excluded collision) and NEW-4 (reload after override-removal
  restores the builtin) regression guards, both PASS against the fixed code.
  Honest note: the NEW-2 test, run in isolation against `97e8c852`'s
  `LoadSkillsAndDispatcher`, does NOT reproduce RED there — `97e8c852`'s
  own `LoadSkillsAndDispatcher` was never the buggy party; only
  `cmd/cli/main.go`'s hand-rolled sequences were. It is committed as a
  correctness regression guard for the now-single canonical sequence, not
  as a RED-on-old-code NEW-2 reproduction; the real-binary evidence below
  is NEW-2's end-to-end CLI-path proof.
- `minor_item_dupdir_test.txt` — the duplicate-leaf-directory-name
  Dir-collision fail-closed path, previously uncovered (paired mutation m4
  survived), now exercised and PASSing.
- `real_binary_skills_show_action-prefix-system.txt` — a genuinely built
  `helixcode` binary (this fix's code), run against the REAL
  `.claude/skills/` corpus at the meta-repo root, `skills show
  action-prefix-system`: the rendered trigger patterns show correctly
  single-escaped `\b` (not the doubled-backslash-and-quote-wrapped broken
  form) for a REAL, currently-shipped skill (5 of the real corpus's 7
  external-tier skills carry this exact escaping shape).
- `real_corpus_dispatch_check.txt` — a small in-module driver program
  (built temporarily under `helix_code/cmd/`, removed before commit) loads
  the REAL `.claude/skills` corpus via `agent.LoadSkillsAndDispatcher` and
  drives real natural-language prompts through `agent.DispatchSkill` for
  `action-prefix-system`, `media-validator` (both external-tier, both
  escaped-backslash triggers) and `reporting-workable-items` (fell back to
  its builtin in this run, itself also escaped-backslash) — all three
  genuinely MATCH and render, end to end, on the real corpus.
- `full_suite_run1.txt` — `go test ./internal/agent/... ./internal/commands/...
  ./cmd/cli/...` after the fix: 100% PASS, including every pre-existing
  N1/N2/F-21/builtin-shadow/similarity-conflict/ceiling/C5-sandbox test.

## What was NOT changed (deliberate)

- The Dir-collision whole-tier-refusal behaviour itself (fail-closed,
  never partial) — kept as-is; only its test coverage gap was closed. See
  the task's final report for the reasoning.
- A root-level `SKILL.md` registering under the external tier's own root
  directory basename — investigated, found benign (no collision with any
  other identity space; matches the pre-existing "name = containing
  directory" convention every other tier already uses), not changed.
