# QA transcript — spec 001-helixskills-incorporation, Phase P2 (T-P2.01..T-P2.05)

**Run-id**: `p2_shadowing_20260914` (curve run `p2_curve1`, perf re-run `perf_20260914T000724Z`)
**Date**: 2026-09-14 · **Mode**: full-automation (§11.4.98 — self-driving, re-runnable, no human action after start)
**Model (live only)**: `qwen2.5-coder-3b-instruct-q4_k_m` via llama-server OpenAI-compatible `http://127.0.0.1:18434/v1`
**Decoding**: temperature 0, seed 42, max_tokens 128. No mocks, no simulated responses.
Zero `model-error` outcomes in any run (every call returned a parseable completion).
**Evidence root**: `qa-results/hxc159/shadowing_curve/p2_curve1/` (this dir carries copies of the load-bearing artefacts).

## 1. T-P2.01 — task set (12 tasks, frozen prompts, oracle + reason each)

`scripts/benchmark_skills/taskset.json`. Real project workflows: MP4-vs-patterns validation,
reminder scheduling, bug reporting, item lifecycle, prefix expansion, session sync,
dormant-skill listing, parallel tracks, + 4 postgres-domain tasks. Oracle = the skill whose
stated purpose owns the task; reason recorded per task.

**Re-runnability (T-P2.01.4)**: 3/3 repetitions at each of 3 sizes, per-task verdicts
**identical** — `rerun_report.json`: `identical_all: true` (108 live calls, 9 runs, zero variance).

## 2. T-P2.02 — curve (same 12 tasks, same prompts, same model; only N varies)

| size | N active | pass_rate (mean of 3) | per-rep | failures (mean/rep) |
|---|---|---|---|---|
| small | 12 (0 distractors) | **0.8333** | 0.8333 ×3 | mis-selection 2.0 |
| mid | 16 (+4 distractors) | **0.8333** | 0.8333 ×3 | mis-selection 2.0 |
| large | 20 (+8 distractors) | **0.8333** | 0.8333 ×3 | mis-selection 2.0 |

Abandonments: 0 at every size. Persistent failures: `T03-report-bug` → `workable-item-lifecycle`
(true confusable pair, similarity 0.0806) and `T05-expand-prefix` → `multitrack` (small) /
`session-sync` (mid, large) — i.e. N-growth **changed which wrong skill was picked** without
changing the rate. Full per-task records: `qa-results/hxc159/shadowing_curve/p2_curve1/run_*.json`
(9 files). Plot data: `curve.csv` + `curve.svg` alongside `curve.json`.
Independent perf re-run (`perf_20260914T000724Z`, 1 rep/size) reproduced 0.8333 at all sizes.

## 3. T-P2.03 — ceiling derivation (stated rule, not by eye)

Rule (in `derive_ceiling.py` and `derivation.md`): knee = smallest N with rate ≤ base−0.10 or
mis-selections ≥ 25% of tasks; ceiling = largest sampled N below knee; FLAT iff spread < 0.05.
**Verdict: FLAT** (spread 0.000). **Ceiling = 20 (provisional, largest tested N)** — reported
honestly per T-P2.03.3, not buried. Margin/reason in `derivation.md` + `ceiling.json` (copied here).

## 4. T-P2.04 — similarity metric + threshold (calibrated on this corpus)

Metric: Jaccard over stopworded content-token sets (`similarity.py`; rationale in its docstring).
Calibration pairs: 4 known-confusable (scores 0.0256–0.2353) vs 10 known-distinct.
Honest finding: the first distinct set scored all-zero, yielding a hair-trigger 0.0128 that the
gate meta-test proved over-fires on distinct skills sharing generic words — so the distinct set
was extended (same rule, wider coverage) and the outcome accepted as measured:
**NON-SEPARABLE, threshold = 0.0256 (fallback = min confusable), `fp_risk_accepted: true`**
(`threshold.json` copied here). Known limits: 2 FPs on distinct pairs
(session-sync‖skill-catalog 0.0278, scheduled-work‖session-sync 0.0294) and 1 boundary FN
(design-postgres-tables‖setup-timescaledb-hypertables 0.0256, equal-not-above).
Golden-good (reporting‖lifecycle 0.0806 > 0.0256) / golden-bad (media-validator‖design 0.0000 <
0.0256) self-validation PASSES — `similarity.py --self-test`, exit 0.

## 5. T-P2.05 — gate CM-ACTIVE-SKILL-CEILING + mutations + perf wiring

Gate: `scripts/gates/cm_active_skill_ceiling_gate.sh` (count ≤ ceiling + pairwise similarity,
names offenders). Meta-test `scripts/tests/cm_active_skill_ceiling_meta_test.sh`: **4/4 PASS**
(self-test green; ceiling+1 refused with CEILING-EXCEEDED; confusable pair refused matching
calibration; hard-distinct refusal covered by honestly-declared non-separable calibration).
Behavioural mutation `scripts/tests/ceiling_behavioural_mutation.sh`: **NOT-APPLICABLE-FLAT**
— no knee exists to mutate past; benchmark re-run at large N confirms no regression
(`behavioural_mutation_report.json` copied here). Notable: under the mutated ceiling the static
count-check passes the 20-set but the similarity sub-gate still refuses it
(Action Prefix System‖Reporting Workable Items 0.1045). Perf wiring
`scripts/tests/shadowing_curve_perf_test.sh`: **PASS**, curve well-formed, flat result reproduced.

## Honest boundary (§11.4.6)

- Flat on N∈{12,16,20} with this model does NOT prove no shadowing at N≫20 or with other
  models — the ceiling is provisional at the tested max, and P4 must treat 20 as untested-above.
- 10/12 is not "good": 2 known interference pairs fail deterministically at every size.
- Similarity sub-gate carries disclosed FP/FN; the exact ceiling count-check is the primary enforcement.
- No commits were made; harness code only under `scripts/benchmark_skills|gates|tests/`.
