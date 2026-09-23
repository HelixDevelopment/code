# HXC-159 P7 (helix_code side) — skillconformance / skillparity / coverage_ledger / security scripts — QA evidence (§11.4.83)

**Run ID:** `hxc159_p7_tooling_0b97f2f4_20260922T212742Z`
**Fix commit:** `0b97f2f425f2798889536d4a57708bde9bc293f6`
**Captured (evidence originally produced):** 2026-09-22 21:27–21:52
**This docs/qa record authored:** 2026-09-23 (T-P9.03, HXC-159 F-20 remediation)

## What this commit did

Implemented T-P7.01/T-P7.02/T-P7.04/T-P7.05 tooling: `skillconformance`
(capability manifest + report, RS-12), `skillparity` (diff + the
`CM-CROSS-CONSUMER-PARITY` gate, RS-12 — this pass surfaced a real
capability-declaration asymmetry vs `helix_agent`), the `coverage_ledger`
generator/validator (`CM-COVERAGE-LEDGER-NO-BLANK`, 57+23 rows, 8 covered /
72 honest skip), and the credential-scan + executable-script-enumeration
security scripts. Also closed a small pre-existing CONST-053 gap
(`helix_code/.gitignore` had no `__pycache__/*.pyc` rule).

## How this evidence was produced

Real `go test` runs and real gate-script invocations, RED-first
(§11.4.115) for each of the four tool packages, all sharing the same
capture run-id `20260922T212742Z` (matching the commit's own local timestamp
cluster):

- **conformance/** — `01_red_library_compile_fail.txt` (RED: library
  package didn't exist yet), `02_green_library_conformance.txt` /
  `06_red_helixcode_compile_fail.txt` / `07_green_helixcode_conformance.txt`
  / `09_red_helixagent_compile_fail.txt` / `10_green_helixagent_conformance.txt`
  (RED→GREEN for all three `skillconformance` report generators — library,
  helix_code, helix_agent), `03_library_cli_run.txt` /
  `04_library_cli_run_wellformed.txt` / `08_helixcode_cli_run.txt` /
  `11_helixagent_cli_run.txt` (real CLI runs producing capability-manifest
  JSON), `05_full_module_green.txt` (full module green),
  `library_report.json` / `helix_code_report.json` / `helix_agent_report.json`
  (the real generated manifests).
- **parity/** — `01_red_parity_compile_fail.txt` (RED), `02_green_parity.txt`
  (GREEN — `TestDiff_SkillRegisteredInOnlyOneConsumerFails`,
  `TestDiff_MarkedSkillAsymmetryPasses`,
  `TestDiff_OneSidedMarkIsNotEnough`, all PASS),
  `03_real_parity_run_stderr.txt` / `04_real_parity_run_samecorpus_stderr.txt`
  (real diff runs against real reports), `05_gate_script_run.txt` (the
  `CM-CROSS-CONSUMER-PARITY` gate script's real output), `diff.json` /
  `diff_samecorpus.json` (the real diffs — genuinely surfacing an asymmetry
  vs helix_agent, as the commit describes).
- **security/** — `03_red_c5_gate_compile_fail.txt` (RED),
  `04_green_c5_gate.txt` / `05_green_security_review_gate.txt` (GREEN —
  `TestEnforceSecurityReview_RefusesUnreviewedDynamicContextSkill` /
  `_AllowsWithExplicitReviewMarker` / `_AllowsPlainSkillsRegardlessOfReviewMap`,
  all PASS), `01_credential_scan_skills_library.txt` /
  `02_credential_scan_helixagent_skills_corpus.txt` (real credential-scan
  runs over the real skills corpus), `06_executable_script_skills_enumeration.txt`
  (real executable-script enumeration).
- **coverage_ledger/** — `01_generate_run.txt` (real ledger generation),
  `02_validate_pass.txt` (validator PASS), `03_paired_mutation_fail.txt`
  (the paired §1.1 mutation correctly FAILs the validator), `ledger.md` /
  `ledger.json` (the real generated ledger, 57+23 rows),
  `04_every_disposition_has_ledger_row.txt` — confirms every one of
  `spec.md` §5's 57 disposition rows has a matching ledger row (missing:
  NONE, extra: NONE).

Copied verbatim from
`qa-results/hxc159/{conformance,parity,security,coverage_ledger}/20260922T212742Z/`.

## Files

See `captures/conformance/`, `captures/parity/`, `captures/security/`,
`captures/coverage_ledger/` for the full file sets listed above.

## Result

All four tool packages: RED-first, GREEN post-implementation, real CLI runs
producing genuine (not fabricated) capability-manifest JSON, and gate
scripts exercised against real generated data — including a paired-mutation
proof for the coverage-ledger validator. `skillparity`'s real diff run
genuinely surfaced a capability-declaration asymmetry against helix_agent,
which the commit message states explicitly rather than suppressing.
