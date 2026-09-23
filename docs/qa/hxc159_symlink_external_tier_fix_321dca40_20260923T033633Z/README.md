# HXC-159 T-P9.01 — external tier follows symlinked skill directories (P1↔P5 seam defect) — QA evidence (§11.4.83)

**Run ID:** `hxc159_symlink_external_tier_fix_321dca40_20260923T033633Z`
**Fix commit:** `321dca40251096cd4d13f63d97e443530ab508e0`
**Captured (evidence originally produced):** 2026-09-23 (round-1 independent review)
**This docs/qa record authored:** 2026-09-23 (T-P9.03, HXC-159 F-20 remediation)

## What this commit did

Fixed an independent-review finding at the seam between Phase P1 and Phase
P5: the registrar (`scripts/register_skills.sh`, T-P1.04) provisions
`.claude/skills/` as SYMLINKS to `constitution/skills/<name>`; helix_code's
packaged-manifest pass in `commands.SkillLoader` used `os.ReadDir` +
`DirEntry.IsDir()`, which is `false` for a symlink, so EVERY
registrar-produced entry was silently invisible to the external tier
(T-P5.05). Each half passed in isolation — the T-P5.05 e2e fixture used a
real directory, not a symlink — but RS-01 (registrar -> `helixcode skills
list`) was never actually true end-to-end. Fix: follow symlinks via
`os.Stat` in the packaged-manifest pass; dangling links and links to plain
files are skipped, never registered.

## How this evidence was produced

Real command captures from the round-1 independent review, `-race`
throughout, driven RED-first (§11.4.115):

1. `10_rs08_live_tree_skills_list.log` — pre-fix live tree: symlinked
   `skill-catalog` invisible to `helixcode skills list`.
2. `11_external_tier_symlink_isolation.log` — isolated probe: a symlinked
   skill directory loads 0 times; a real-directory copy of the same content
   loads 1 time — proving the defect is specifically about symlink handling,
   not content.
3. `15_symlink_red_prefix.log` / `16_symlink_red_mode1_prefix.log` —
   `skill_loader_symlink_test.go`'s behavioural RED on the pre-fix tree
   (§11.4.115 RED-baseline-on-the-broken-artifact).
4. `17_symlink_green_postfix.log` — the same test GREEN after the fix.
5. `18_symlink_red_mode1_postfix.log` — `RED_MODE=1` correctly FAILs
   post-fix (the polarity switch proving the RED test genuinely detects the
   pre-fix defect and not something else).
6. `19_commands_agent_race_postfix.log` — `internal/commands` +
   `internal/agent` full suite green under `-race` and `go vet`, post-fix.
7. `21_rs01_rs08_live_tree_postfix.log` — re-observed on the LIVE checkout
   with a rebuilt binary: `skill-catalog` now lists with
   `SOURCE .claude/skills/skill-catalog/SKILL.md` and `skills invoke
   skill-catalog` renders the real constitution-sourced body.

Copied verbatim from
`qa-results/hxc159/review/20260923T_review1/{10_rs08_live_tree_skills_list.log,11_external_tier_symlink_isolation.log,15_symlink_red_prefix.log,16_symlink_red_mode1_prefix.log,17_symlink_green_postfix.log,18_symlink_red_mode1_postfix.log,19_commands_agent_race_postfix.log,21_rs01_rs08_live_tree_postfix.log}`,
which is this commit's own cited evidence directory.

## Files

- `captures/10_rs08_live_tree_skills_list.log`
- `captures/11_external_tier_symlink_isolation.log`
- `captures/15_symlink_red_prefix.log`
- `captures/16_symlink_red_mode1_prefix.log`
- `captures/17_symlink_green_postfix.log`
- `captures/18_symlink_red_mode1_postfix.log`
- `captures/19_commands_agent_race_postfix.log`
- `captures/21_rs01_rs08_live_tree_postfix.log`

## Result

RS-01 (registrar -> `helixcode skills list`) is genuinely true end-to-end
post-fix on a rebuilt binary against the live checkout: a symlinked skill
directory is now followed, loaded, listed, and invokable. RED->GREEN,
RED_MODE=1 polarity-switch, and full-package race-clean confirmation are
all captured. Per the commit's own note (§11.4.6): the tier now also
surfaces whatever else is symlinked into `.claude/skills/` — the absence of
a consumer-side activation allowlist (FR-015) remains a separate, tracked
T-P9.03 follow-up, not resolved by this fix.
