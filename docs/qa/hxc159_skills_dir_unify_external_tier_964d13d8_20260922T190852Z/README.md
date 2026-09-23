# HXC-159 T-P5.03 + T-P5.05 — unify user skills dir + wire external tier — QA evidence (§11.4.83)

**Run ID:** `hxc159_skills_dir_unify_external_tier_964d13d8_20260922T190852Z`
**Fix commit:** `964d13d8ec203443926ad5023cf30a3ed0b01ae7`
**Captured (evidence originally produced):** 2026-09-22 18:56–19:08
**This docs/qa record authored:** 2026-09-23 (T-P9.03, HXC-159 F-20 remediation)

## What this commit did

**T-P5.03:** `cmd/cli/main.go` resolved the user skills directory as
`os.UserConfigDir()/helixcode/skills`, while `applications/terminal_ui/main.go`
and `internal/clientcore/agentic.go` used `$HOME/.helix/skills` — two
disjoint directories hardcoded independently in three places, so a skill
installed under one front-end's convention was invisible to the other. Adds
`internal/agent/skill_dirs.go` as the single source of truth
(`CanonicalUserSkillsDir()`, `LegacyUserSkillsDir()`, `UserSkillsDirs()`);
all four call sites now converge on it.

**T-P5.05:** adds a further "external tier" — `ExternalSkillsDir()`,
config-injected via `HELIXCODE_EXTERNAL_SKILLS_DIR` (default
`.claude/skills`, the directory `scripts/register_skills.sh` already
provisions) — and `AllSkillsDirs(projectDir)` returning the full
`[legacy, canonical, external, project]` order. Before this fix a skill
provisioned externally (e.g. via the T-P1.04 registrar) was reachable from
NO front-end's `skills list`/dispatcher.

## How this evidence was produced

Real command captures, RED-first (§11.4.115), in two sub-scopes:

### `captures/skills_dir_unify/`
1. `red_compile_fail.txt` — RED: `go vet ./internal/agent/...` fails with
   `undefined: LegacyUserSkillsDir` before the helper exists.
2. `green_pass.txt` — GREEN: unit tests (isolated
   `HOME`/`XDG_CONFIG_HOME`/`HELIXCODE_EXTERNAL_SKILLS_DIR`,
   precedence-collision assertions) plus the full `internal/agent/...`
   package tree, all green.
3. `call_sites_build.txt` / `call_sites_test.txt` — the four converged call
   sites (CLI buildSubsystems, CLI `skills` dispatcher, TUI, clientcore)
   build and test green against the unified helper.
4. `cli_build.txt` — real CLI binary build.
5. `e2e_cli_skills_list.txt` — real end-to-end `skills list` run confirming
   the unified resolution order.

### `captures/helixcode_wire/`
1. `red_cli_skills_list.txt` — RED: a pre-fix real CLI binary run against a
   fixture skill (`external-e2e-skill`) provisioned only in the T-P5.05
   external tier — the fixture skill does NOT appear in `skills list`
   output (grep count: `0`, "NOT FOUND (expected RED)").
2. `green_build.txt` / `cli_rebuild.txt` — real rebuild after the fix.
3. `green_unit_tests.txt` — unit-level GREEN for the wiring.
4. `green_cli_e2e.txt` — post-fix real CLI run: `helixcode skills invoke
   external-e2e-skill` renders the REAL body with an argument substituted
   ("EXTERNAL E2E RENDERED BODY, arg was hello-world-arg" — not a stub, not
   an error) — this is RS-08 satisfied end-to-end.
5. `full_regression.txt` — full-module regression green post-fix.

Copied verbatim from
`qa-results/hxc159/skills_dir_unify/20260922T185626Z/` and
`qa-results/hxc159/helixcode_wire/20260922T190852Z/`, which are this
commit's own cited evidence paths.

## Files

- `captures/skills_dir_unify/{red_compile_fail,green_pass,call_sites_build,call_sites_test,cli_build,e2e_cli_skills_list}.txt`
- `captures/helixcode_wire/{red_cli_skills_list,green_build,cli_rebuild,green_unit_tests,green_cli_e2e,full_regression}.txt`

## Result

Both fixes are genuinely wired: the user-skills-directory convergence is
proven by unit tests + a real CLI list run, and the external-tier wiring is
proven RED (fixture invisible pre-fix) → GREEN (fixture's real rendered
body, with argument substitution, returned post-fix by a rebuilt binary).

**Honest boundary carried over from the commit itself:** T-P5.05.4 (assert
the activation allowlist/ceiling is honoured at this specific seam) could
not be verified at the time of this commit — no such mechanism existed
anywhere in `helix_code/` yet (grep-confirmed). That gap was subsequently
closed by F-01 (`e2135a17`, see
`docs/qa/hxc159_f01_external_tier_policy_wiring_e2135a17_20260923T092700Z/`),
which wired the policy gate this commit's external tier lacked.
