# HXC-159 F-13 — builtin_skills divergence investigation — QA evidence (§11.4.83)

**Run ID:** `hxc159_f13_builtin_skills_investigation_18ea70e5_20260923T064844Z`
**Fix commit:** `18ea70e515010affaf7f7d052f877b42b6422b7f`
**Captured (evidence originally produced):** 2026-09-23 06:44–06:48
**This docs/qa record authored:** 2026-09-23 (T-P9.03, HXC-159 F-20 remediation)

## What this commit did

Investigated the earlier T-P9.01 F-13 finding ("7 constitution skills
embedded and diverged byte-for-byte since 2026-06-14, contradicting
source-is-authoritative") by diffing both frontmatter AND body content, not
just file bytes. Root cause: the two `SKILL.md` trees are NOT drifted copies
of the same artifact — `constitution/skills/<name>/SKILL.md` is a
Claude-Code-native Agent Skill, while
`helix_code/internal/commands/builtin_skills/<name>/SKILL.md` is a
HelixCode-internal prompt TEMPLATE for its own regex-triggered REPL
completion feature, consumed by a different loader with different
frontmatter and purpose. This was proven, not guessed: the commit wrote
`scripts/generate_builtin_skills.sh`, ran its regenerate mode for real
against the live tree, and re-ran `go test ./internal/commands/...` — all 7
`TestGovernanceSkills_RegisteredAndInvocable` subtests FAILED (overwriting
the builtin format with the constitution-native one destroys the
auto-trigger completion feature — a real capability regression per
§11.4.122). The regenerated content was reverted (`git checkout --`) and is
NOT part of the committed tree; `go test ./internal/commands/...` is green
again on the committed tree. `scripts/generate_builtin_skills.sh` was kept as
real, working drift-DETECTION tooling (`--check` mode only, wired via a
`//go:generate` directive that never auto-regenerates).

## How this evidence was produced

Real command captures from the commit's own investigation, run against the
live tree (not synthetic/mocked):

1. `01_check_before.log` — `--check` mode run before any change, over the
   full tree.
2. `02_check_before_scoped.log` — `--check` mode scoped to the 7 named
   entries.
3. `03_regenerate.log` — the regenerate mode run for real, overwriting the
   7 `builtin_skills/<name>/SKILL.md` files with the constitution-native
   format (a deliberate, reverted probe to prove the regression, not a
   proposed fix).
4. `04_check_after.log` — `--check` mode re-run confirming the regenerated
   tree now reports clean (proving the tool's own detection logic works).
5. `06_tests_after_revert.log` — `go test ./internal/commands/...` re-run
   AFTER `git checkout --` reverted the regenerated files, confirming the
   committed tree is green (7/7 `TestGovernanceSkills_RegisteredAndInvocable`
   subtests PASS on the actual shipped source).
6. `05_summary.md` — the investigation's own written summary of the root
   cause and evidence, produced as part of the original F-13 work.

Copied verbatim from `qa-results/hxc159/f13_builtin_skills/20260923T064412Z/`,
which is this commit's own cited evidence path (`18ea70e5`'s commit message:
"Evidence: qa-results/hxc159/f13_builtin_skills/").

## Files

- `captures/01_check_before.log`
- `captures/02_check_before_scoped.log`
- `captures/03_regenerate.log`
- `captures/04_check_after.log`
- `captures/06_tests_after_revert.log`
- `captures/05_summary.md`

## Result

The two `SKILL.md` trees serve genuinely distinct purposes and are not a
sync bug; a naive resync would silently break a shipped, tested capability
(the REPL auto-trigger completion feature). The commit's own regenerate
probe confirms this via a real, captured test-suite regression (7/7 FAIL),
reverted cleanly, tests green again on the committed tree. Reconciling the
two formats or retiring the internal template mechanism is filed as
requiring an explicit architectural decision — not executed unilaterally by
this investigation, consistent with §11.4.122 (no silent removal of an
existing shipped component without operator confirmation).
