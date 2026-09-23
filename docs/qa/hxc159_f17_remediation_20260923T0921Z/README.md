# HXC-159 F-17 remediation evidence (2026-09-23)

Real, captured evidence for the F-17 recalibration (round-3 review BLOCKING
finding: SimilarityThreshold=0.0256 was derived by mixing corpus-1 with
corpus-2, refusing the real, frontmatter-complete corpus-1 tier at both
consumers).

## Files

- `threshold.json` — real output of
  `scripts/benchmark_skills/similarity.py --calibrate`, run against the
  real, now-frontmatter-complete corpus-1 (9 skills: the 8
  `constitution/skills/*/SKILL.md` skills + `speckit-analyze`, corpus-2's 4
  `postgres-mcp` skills excluded from calibration per `CorpusTwoPolicy`).
  `separable: true`, `threshold: 0.0709`, `fp_risk_accepted: false`.
- `similarity_matrix.json` — the full real pairwise Jaccard matrix across
  all 13 corpus entries (corpus-1 + corpus-2), same script,
  `--matrix --out`.
- `helixcode_recalibrated_run.txt` — real `helixcode skills list` output
  from a binary rebuilt against the recalibrated `SimilarityThreshold =
  0.0709` (`submodules/skills/pkg/skills/similarity.go`), run from the
  repo root against the real `.claude/skills/` external tier. The tier is
  still refused, on the pair `action-prefix-system` ||
  `reporting-workable-items` at score 0.1045 (a genuine, content-verified
  confusable pair — see "Residual finding" below), NOT on the F-17
  defect (the F-16/F-01 frontmatter-missing refusal is gone).
- `helix_agent_native_recalibrated_run.txt` — real output of a throwaway
  Go program (`cmd/hxc159_f17_verify_tmp2`, built + run + removed in this
  session, never committed) calling
  `helix_agent`'s native, non-importing `ExternalSkillSource.Load()`
  (`internal/skills/external_policy.go`) against the real
  `constitution/skills/` directory. Same pair, same score (0.1045), same
  recalibrated threshold (0.0709), same refusal — confirms the mirrored
  Go port stayed in lockstep with `pkg/skills`.

## Recalibration methodology (same rule T-P2.04 originally used)

`separable := max(known_distinct) < min(known_confusable)`; if separable,
`threshold := (max(known_distinct) + min(known_confusable)) / 2` with
`fp_risk_accepted=false`; else fallback `threshold := min(known_confusable)`
with `fp_risk_accepted=true`. Unchanged rule — only the CORPUS changed:
`scripts/benchmark_skills/similarity.py`'s `CONFUSABLE`/`DISTINCT` lists
now derive ONLY from corpus-1 (the skills either consumer's default
external-tier activation actually co-activates), excluding corpus-2's
unrelated `postgres-mcp` skills (`TrustVendored`, never in a default
allowlist per `pkg/skills/activate.go`'s `CorpusTwoPolicy` — they were
never a real co-activation risk for the tier F-17 found broken). Every
CONFUSABLE/DISTINCT classification was made by reading the actual
descriptions and inspecting the real shared-token set per pair (not a
quantile split); see `scripts/benchmark_skills/similarity.py`'s inline
comments for the per-pair rationale.

Before (measured, mixed corpus-1+corpus-2): threshold 0.0256,
NON-SEPARABLE fallback, `fp_risk_accepted=true`, min(known_confusable)
0.0256 (a postgres/hypertable-domain pair, not a corpus-1 pair) — this
threshold refused the real corpus-1 tier on the project's own,
now-frontmatter-complete governance-skill descriptions (the F-17 defect).

After (measured, corpus-1-only): threshold 0.0709, SEPARABLE,
`fp_risk_accepted=false`, max(known_distinct) 0.0625 < min(known_confusable)
0.0794, margin 0.0169.

## Residual finding (reported, not silently threshold-hidden)

The real corpus-1 external tier is STILL refused after recalibration,
because it genuinely contains a confusable pair:
`action-prefix-system` || `reporting-workable-items`, score 0.1045,
shared content tokens `{action, add, bug, forms, issue, operator, task}` —
both skills' real descriptions genuinely discuss ISSUE/BUG/TASK
action-prefix handling, a real domain overlap an agent could plausibly
confuse. This is NOT a metric defect and NOT a miscalibration: raising the
threshold further to admit this pair would also un-flag the other two
genuinely-confusable pairs (0.0806, 0.0794), defeating the shadowing-gate's
purpose. See the F-17 subagent's final report for the three remediation
options considered (per-skill/partial admission instead of whole-tier
fail-closed; advisory-with-audit instead of hard refusal; an
operator-authorized content edit to reduce wording overlap between the two
descriptions) and why none was applied unilaterally in this narrowly-scoped
fix.
