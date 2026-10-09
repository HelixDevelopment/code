# SpecKit 003 — Phase 1 (Setup) execution evidence

**Run id:** `20260908T202407Z` (UTC) · **Feature:** `specs/003-provable-model-alias-availability/`
**Scope executed:** T001, T002, T003, T004 (Phase 1: Setup). **Phase 2 not started.**
**Committed:** no. Every change is awaiting independent review (§11.4.142).

All files here are raw captured command output (§11.4.262). Nothing in this
directory is a narration of a run that did not happen.

| File | What it proves |
|---|---|
| `mechanical_suite_baseline.txt` | Pre-change state of the mechanical library: `ALL SUITES: 181 passed, 0 failed` |
| `t001_baseline.txt` | T001 acceptance, half 1 — unmutated toolkit suite reproduces the measured `0 failed / 58 passed [OK]`; source-tree fingerprint unchanged |
| `t001_d5e_mutation.txt` | T001 acceptance, half 2 — mutation D5e applied (`replacements=1`) turns the same suite red: `23 failed / 35 passed [OK] EXPECTED-FAIL` |
| `t002_RED.txt` | T002 RED, observed BEFORE implementation: `27 failed, 4 passed` |
| `t002_GREEN.txt` | T002 GREEN after implementation: `31 passed, 0 failed` |
| `t002_paired_mutations.txt` | Two §1.1 mutations OBSERVED making the new gate fail (`8 failed`, `3 failed`) — the gate is validated instrumentation, not decoration |
| `t002_mechanical_suite_after.txt` | Full library after the change: `212 passed, 0 failed`, identical across two consecutive runs (§11.4.50) |
| `t003_await_condition_verify.txt` | T003 verified in BOTH directions: satisfied → exit 0 `SATISFIED`; expired → exit 3 `TIMEOUT`; live pid → TIMEOUT, never "gone" |
| `t004_toolkit_acceptance_fast.txt` | The new toolkit suite: `17 passed, 0 failed` |
| `t004_discovery_and_degradation.txt` | Auto-discovery by `run-all.sh` (72 files; positive control listed, fabricated control absent) and the honest SKIP when no corpus is reachable |
| `t004_export_validation.txt` | §11.4.168-lite: the rendered PDF/HTML carry the content and leak no raw markup |
| `credential_audit.txt` | §11.4.10: 0 credential-shaped strings in evidence and new source — **including a first attempt that correctly REFUSED**, because the negative needle chosen was genuinely present in the RED log |
| `residue_scan.txt` | §11.4.84: 0 mutation residue in the three authored files, by two independent instruments — **including a correction**, the first toolkit scan's `rc=0` was `tail`'s exit code, not the scan's (§11.4.273(g)) |

## Two instrument errors this run made, and did not hide

Both are recorded above rather than quietly re-run, because §11.4.273 exists
precisely because these are easy to make and easier to paper over.

1. **A negative control that was genuinely present.** The first credential audit
   used `zzz_fabricated_needle` as its must-not-be-found needle — a string that
   really does occur in `t002_RED.txt`, since the RED log echoes the test's own
   command lines. `census_query.sh` refused to emit a count. That refusal is the
   tool working; the rerun uses a needle verified absent.
2. **`$?` read after a pipeline.** A toolkit residue scan piped through `tail`
   reported `rc=0`, which was `tail`'s exit code, not the scan's. Redone without
   a pipeline.

## Files changed (none committed)

**`constitution/`** — `scripts/mechanical/census_query.sh` (new),
`scripts/mechanical/tests/test_census_query.sh` (new),
`scripts/mechanical/README.md` (Rev 1 → 2),
`docs/scripts/mechanical_tools.{md,html,docx,pdf}` (new).

**`claude_toolkit/`** — `scripts/tests/test_mechanical_tools_acceptance.sh` (new).

**`helix_code/`** — `specs/003-provable-model-alias-availability/tasks.md`
(T001–T004 marked done with a closure note).

**Not mine.** The working trees carry other modifications this dispatch did not
make: `constitution/scripts/hooks/credential_scan_lib.sh` and its test (mtimes
22:12 and 22:15 local, both BEFORE this run began at 22:24 — measured, not
assumed), plus `helix_code` changes to `docs/Issues*`, `docs/requests/*`,
`docs/workable_items.db`, `specs/.../research.*` and several submodule pointers.
