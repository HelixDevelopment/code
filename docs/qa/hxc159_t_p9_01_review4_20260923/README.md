# HXC-159 T-P9.01 — round-4 independent review evidence (2026-09-23)

| Field | Value |
|---|---|
| Revision | 1 |
| Created | 2026-09-23 |
| Last modified | 2026-09-23 |
| Status | active |

**Reviewer:** Fable 5.1, xhigh, zero authorship stake in any HXC-159 change (§11.4.240 producer ≠ verifier).
**Scope:** every commit since the round-3 review (`770c87ae`) across the four repos — meta `4144cab6..8bdb46ac`, constitution `24ae426`/`35413ac`/`e72175f`, helix_agent `15cc2fcf`/`5afc7d30`, skills `941f1d4`/`b165b9f` — with the F-17 Option A per-skill admission and the F-13 Option C superset schema re-derived from scratch.
**Fix commit:** `1130788b` (meta) — the round-4 F-21 fix + permanent guard, authored by the reviewer and therefore itself still owed an independent review (§11.4.142). (`2f4cd66c`, which landed concurrently at 18:21 while this review was blocked on `.git/index.lock`, is ANOTHER session's F-15 merge-readiness research note, not this review's.)
**Verdict:** NOT-GO (see `specs/001-helixskills-incorporation/tasks.md` Rev 6, "T-P9.01 ROUND-4 findings").

## What was re-derived (never re-read)

| Capture | What it proves |
|---|---|
| `captures/00_commits_since_round3.txt` | The 8 post-round-3 commits enumerated per repo; submodule pointers match HEADs (`01_submodule_pointers.txt`). |
| `captures/08_f15_fetch_state.txt` | `git fetch --all` on all four repos: meta 56 ahead; constitution 12 ahead AND 14 BEHIND (upstream moved again); helix_agent 14 ahead; skills 10/33 ahead. Nothing pushed. |
| `captures/11_tests_*.txt` | `pkg/skills` (`-race`), helix_agent `internal/skills` + `skillconformance` + `tests/compliance` (`-race`), helix_code `internal/agent`, `internal/commands` — all GREEN at `8bdb46ac`. |
| `captures/12_f19_g57_gate.txt`, `16_f19_f18_mutation_selftests.txt` | F-19's G57 real run: PASS, 18 files, 0 findings; its mutation self-test 5/5. F-18's `RED_MODE=0` guard (i)(ii)(iii) PASS → `VERDICT: WIRED`. |
| `captures/13_f18_unique_module_paths.txt` | F-18 gate live: 155 modules, 15 grandfathered, zero NEW duplicates. |
| `captures/14_f13_generator_drift.txt`, `15_f13_triggers_old_vs_new.txt` | F-13: `generate_builtin_skills.sh --check` PASS; independent byte-diff — all 7 constitution-sourced builtins IDENTICAL to `constitution/skills/`; regenerate-to-temp byte-identical to committed; all 7 `triggers:` lists and `requires_isolation` unchanged old→new (no REPL trigger behaviour change). |
| `captures/20_real_cli_skills_list_live_tree.txt` | Rebuilt `helixcode` CLI (`go build -tags nogui ./cmd/cli/`, binary `05c55350…`) run from the repo root against the real `.claude/skills/`: external tier evaluated, exactly two exclusions — `external.Reporting Workable Items` (0.1045 vs `Action Prefix System`) and `external.session-sync` (0.0794 vs `Multitrack`) — `skill-catalog` resolves and invokes from `.claude/skills/` (RS-01/RS-08 true). |
| `captures/21_real_corpus_probes_both_impls.txt` | Both `similarity-probe`s on the REAL corpora: pkg/skills and helix_agent produce identical admitted/excluded sets + scores on `constitution/skills/` (real-corpus parity, not the synthetic parity fixture). Last block: helix_agent on the symlink-populated `.claude/skills/` → `admitted=[]` silently (finding F-23). |
| `captures/22_RED_prune_name_mismatch_real_cli.txt` | **F-21 RED on the real binary:** an external tier whose excluded skills carry front-matter `name:` ≠ directory name — `zz-beta` logged EXCLUDED yet `skills show zz-beta` registered and `skills invoke zz-beta` renders; the name==dir control `zz-gamma` correctly pruned. |
| `captures/23_f21_RED_unit_test.txt` | The new guard `TestLoadExternalTierIntoRegistry_PrunesByDirectoryIdentityNotFrontMatterName` FAILs on the pre-fix code (`8bdb46ac`). |
| `captures/24_f21_GREEN_unit_tests.txt` | Same test + full `./internal/agent` suite GREEN on the fixed code (`2f4cd66c`). |
| `captures/25_f21_GREEN_real_cli_rebuilt.txt` | CLI rebuilt from the fixed source (binary `11725715…`): same fixture → `zz-beta` "not found" (exit 1) for `show` and `invoke`; live tree unchanged (same two exclusions, `skill-catalog` invokes). |
| `captures/30_full_gate_sweep_verdicts.txt`, `31_sweep_g19_reviewer_abort.txt` | 57-gate sweep: identical to the round-3 baseline for every gate except G19 (FAIL = interrupted, exit 143 — the reviewer stopped that compile stage under §12.6 host pressure, load >100 / <1 GB available; it certified nothing) and G57 (new, PASS). G37–G56 = 15 PASS / 3 SKIP / 2 FAIL, matching F-20's corrected count. |

## Findings (details in the tasks.md Rev 6 ledger)

- **F-21 (BLOCKING, fixed in `2f4cd66c`, fix not yet independently reviewed):** F-17 call-site prune keyed on the front-matter name — silent no-op for 4 of 9 real skills.
- **F-22 (ledger/doc accuracy):** `builtin` overrides same-named external entries (loaded last), not "lower-precedence"; catalogue HelixCode cells for the two excluded skills say "via external tier".
- **F-23 (latent, §11.4.201 false-null):** helix_agent `ParseDirectory` does not follow symlinked skill dirs — silently empty on the registrar's output shape.
- **F-24 (CONST-044):** `docs/CONTINUATION.md` never mentions HXC-159.
- **F-15:** publication still held; constitution now 14 behind — §11.4.113 merge-onto-latest-main remains mandatory before any push.

Raw captures (git-ignored): `qa-results/hxc159/review/20260923T_review4/`.
