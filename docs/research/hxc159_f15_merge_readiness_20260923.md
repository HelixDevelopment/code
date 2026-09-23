# HXC-159 F-15 Merge-Readiness Investigation (2026-09-23)

**Status:** Investigation only. No merge, rebase, commit (other than this
document), or push was performed in any of the four real checkouts. All
`git fetch` calls are safe (they do not touch a working tree or advance a
local branch ref) and are the only mutating operation run against the real
repositories. One simulated `git merge --no-commit --no-ff` was run inside a
disposable, fully-deleted clone under the session scratchpad and never
touched the real `constitution/` checkout.

**Purpose:** F-15 (in `specs/001-helixskills-incorporation/tasks.md`, amended
row, 2026-09-23 ~07:2xZ) found that all four repos carry unpushed local
commits (correctly held per operator instruction until GO), and that the
`constitution` submodule is simultaneously ahead of AND behind all 8 of its
configured upstream remotes. This document produces a concrete,
merge-simulation-backed readiness report per the caller's request, without
executing the merge.

## 1. Fresh fetch results (this session, 2026-09-23) — supersedes F-15's snapshot

`git fetch --all --prune --tags` was run in all four repos. Numbers below are
measured directly from the post-fetch remote-tracking refs (`git rev-list
--left-right --count HEAD...<remote>/<branch>` per remote), not copied from
F-15.

| Repo | Ahead of every remote | Behind any remote | Notes |
|---|---|---|---|
| meta-repo (`helix_code`) | 56 | 0 (all 4 remotes: origin/github/upstream/gitlab) | Consistent 56/0 across all 4 remotes. F-15 recorded 42 ahead — 14 more local commits landed since. |
| `constitution/` | 12 | 14 (all 8 remotes: origin/gitflic/upstream/github/gitlab/gitverse/vasic_digital_github/vasic_digital_gitlab) | **Confirmed both ahead AND behind, on every single configured remote — all 8 remote `main` refs point to the identical SHA `eba38e8`.** F-15 recorded 9 ahead / 11 behind; the upstream side grew by 3 commits and the local side by 3 commits since F-15's snapshot (same-day, later timestamps). |
| `submodules/helix_agent/` | 14 | 0 (all 4 remotes: origin/githubhelixdevelopment/upstream/github) | F-15 recorded 12 ahead (11 + `5f3124aa`); 2 more local commits landed since. Not behind anywhere. |
| `submodules/skills/` | 10 (origin, github) / 33 (gitflic, upstream, gitlab, gitverse) | 0 on **every** remote | Repo is in a **detached HEAD** state (`HEAD detached from 7cce3c2`), though a local `main` branch exists and points to the exact same commit as HEAD (`b165b9f`) — cosmetic housekeeping item, not a blocker, but worth reattaching (`git checkout main`) before the eventual push so `git push` doesn't need an explicit `HEAD:main` refspec. The uneven ahead-count (10 vs 33) reflects that `origin`/`github` already carry more of skills' recent local history pushed to them than `gitflic`/`upstream`/`gitlab`/`gitverse` do — it is **not** a behind condition; both `gitflic/main` and `github/main` were confirmed (`git merge-base --is-ancestor`) to be ancestors of local HEAD. |

**Bottom line:** Only `constitution/` needs an actual merge. The meta-repo,
`helix_agent`, and `skills` are strictly ahead of every configured remote —
there is nothing upstream to merge in for those three. F-15's core finding
(constitution ahead-and-behind) is reconfirmed with updated, larger counts;
nothing about the *shape* of the finding has changed.

## 2. The 14 constitution upstream-only commits (`HEAD..origin/main`, all 8 remotes identical at `eba38e8`)

Listed newest-first (the order `git log HEAD..origin/main` returns):

| Commit | Date | Summary |
|---|---|---|
| `eba38e8` | 2026-09-23 15:07 | fix(gates): CM-DANGEROUS-COMBINATION-FAIL-CLOSED stops flagging fail-closed refuse-shaped guards (BOB-189) — call-site-aware discrimination so an SSRF guard's own `except: return False` (a refuse-shaped escape every caller gates on) is no longer misreported as a fail-open. Gate + mutation-test file only. |
| `6f6fb55` | 2026-09-23 11:34 | fix(gates): CM-DANGEROUS-COMBINATION-FAIL-CLOSED closes 2 more coverage gaps (BOB-199/200) — narrow-`contextlib.suppress` + irreversible-capability detection with a `# guardrails:allow <reason>` waiver, plus a `find(1)` exit-status bug that silently degraded a permission-denied scan into a false "nothing in scope" SKIP. Gate + mutation-test file only. |
| `2718966` | 2026-09-23 10:33 | fix(gates): CM-DANGEROUS-COMBINATION-FAIL-CLOSED closes 3 coverage gaps (BOB-213/214/216) — adds sh/bash to the gate's scanned extensions (198 previously-invisible tracked `.sh` files), adds `--max-depth`, and fixes AST-mode false positives on comments/docstrings that merely quote the anti-pattern text. Gate + mutation-test file only. |
| `10aa984` | 2026-09-22 22:53 | feat(skill-activation): lands the host-CLI wrapper (`scripts/skill_activation/skill-activate-wrapper.sh`) + its auto-install seam (`skills/skill-catalog/register.sh`) for §11.4.272 clause G — these files existed hand-installed on one host but were never committed to the tracked source; this commit makes §11.4.272's "works on a fresh host" promise real. |
| `34e42f0` | 2026-09-22 21:47 | fix(gates): resolve CM-GATE-LEDGER-RATCHET FAIL — registers a gate-rename citation for the §11.4.209 amendment's gate-name change and bumps the gate-ledger baseline 397→403 to absorb upstream corpus growth (13 new `CM-*` gate names, none locally introduced). Also carries a `submodules/design-toolkit` pointer bump picked up incidentally by the mandatory fetch-first step. |
| `e7ef51c` | 2026-09-22 12:45 | fix(gates): enables `shopt -s globstar` so `OD_VISREG_GLOBS`'s `**` patterns actually recurse (bash degrades unmarked `**` to a single path component) — a 2-level-deep visual-regression suite was silently missed before this fix. |
| `61b0c69` | 2026-09-19 10:11 | Merge commit reconciling `4698c6f` (§11.4.184(I) extension) with `8935d99` (a large prior upstream merge) — no independent content. |
| `4698c6f` | 2026-09-19 10:09 | **feat(11.4.184): extends §11.4.184 (SonarQube-CLI mandate) with HawkScan + OWASP ZAP + gitleaks + Trivy tooling.** Touches `Constitution.md`, `AGENTS.md`, `CLAUDE.md`, `GEMINI.md`, `QWEN.md` plus 4 new tool-installer/runner script directories (`scripts/gitleaks/`, `scripts/hawkscan/`, `scripts/trivy/`, `scripts/zap/`, 19 files / 967 lines total). This is the largest governance-text change in the batch besides the §11.4.209 amendment. |
| `8935d99` | 2026-09-17 22:50 | Merge commit reconciling a prior upstream advance (28 commits, including a design-toolkit gitlink fix) with a locally-mislabeled-but-harmless pointer-bump commit. Large diffstat (81 files / ~7,377 insertions) carrying, among other things: 17 new `submodules/*` nested-submodule additions (verification/verify/repo-qa/repo-proof/donespec/kedge/agentic-validation/skill-doctor/verfix/MVT/wave-dpctf/mcp-audio-tweaker/video-quality-mcp/polyscreen-mcp/claude-video/watch-skill/anti_bluff family — most of these landed as pointer-only adds in the constituent commits below), the `scripts/mechanical/` mechanical-work-extraction toolkit (§11.4.274), `scripts/skill_activation/` (the skill-activation engine proper, §11.4.272), `scripts/multitrack/` updates, `scripts/hooks/guard-forbidden-commands.sh` + credential-scan-lib hardening, and doc regenerations under `docs/scripts/`. |
| `454e331` | 2026-09-17 15:43 | docs: captures real layer-4 Superpowers-TUI verification evidence for 3 llmctl provider profiles (small/vision/moe-fast) — an honestly-disclosed finding (context-budget too small once Claude Code's system prompt + Superpowers plugin are injected), not a fix. Only touches the `submodules/design-toolkit` pointer (pulled in by the same evidence-capture session). |
| `6bf5805` | 2026-09-15 00:00 | chore: commits nested-submodule pointer bumps (14 `submodules/*` pointers) after inheritance-pointer wiring. |
| `c7064fa` | 2026-09-14 23:59 | chore: commits `CLAUDE.md`/`AGENTS.md` inheritance-pointer files inside 14 nested submodules. |
| `16af233` | 2026-09-14 21:23 | Adds 17 new validation/verification nested submodules to `submodules/` (gitlinks + `.gitmodules` entries) plus a verification harness (`scripts/validation/run_verification.sh`) and its paired meta-test. |

### The §11.4.209 reviewer-tier amendment (`f9e99be`) — **this is not one of the 14 "behind" commits; it is already an ancestor of local HEAD**

Important correction to the task's framing: `f9e99be` ("amend §11.4.209:
Opus-xhigh default reviewer, Fable-xhigh for big/complex") is **already
merged into local HEAD** — it is the second parent of the `8935d99` merge
commit, which *is* one of the 14 upstream-only commits, but `f9e99be` itself
predates the point where local and upstream diverged and is already present
in the local `constitution` history. Confirmed:

```
$ git merge-base --is-ancestor f9e99be HEAD && echo "already in local HEAD"
already in local HEAD
```

**What the amendment actually changed (full diff read from `f9e99be`,
operator mandate dated 2026-09-15, superseding the prior 2026-07-15
mandate):**

- **Old rule (2026-07-15, now withdrawn):** mandatory independent code
  review MUST run on **Fable at `xhigh`** by default; Opus at `xhigh` only
  as a fallback when Fable is genuinely unavailable.
- **New rule (2026-09-15, current, and already present in this session's
  local `constitution` checkout):** mandatory independent code review MUST
  run on **Opus at `xhigh` by default**; Fable at `xhigh` is reserved for
  reviews classified — by the operator, or by the dispatching agent with a
  stated, evidenced reason — as **extremely big or complex** (very large
  diff, cross-cutting/architectural blast radius, a governance/gate-authoring
  change, or anything the operator explicitly flags). `xhigh` effort is
  required in both cases.
- The recommended gate was renamed from `CM-CODE-REVIEW-FABLE-XHIGH` to
  `CM-CODE-REVIEW-OPUS-XHIGH-DEFAULT-FABLE-ESCALATION` in the same commit —
  this rename is exactly what produced the `CM-GATE-LEDGER-RATCHET` failure
  that upstream commit `34e42f0` (one of the genuinely-behind 14) fixes with
  an explicit removal citation.
- Also folds in a new clause (D): any plaintext credential value that has
  ever appeared in documentation/spec/commit history must be treated as
  compromised from the moment of commit.

**Material relevance to this session's work:** the task description states
"this session has been dispatching reviews on the Fable model per that
anchor." Since `f9e99be` is *already* in this session's local `constitution`
checkout (it is not something the eventual merge will newly introduce), the
Opus-default/Fable-escalation rule has been the *locally governing* text
throughout this session already — the merge does not change this session's
obligations going forward. If prior review dispatches in this session
defaulted to Fable without an operator/reviewer-stated big-or-complex
classification reason, those dispatches were not compliant with the rule
already present locally (a fact worth flagging to the operator independent
of the merge question), but that is a pre-existing local-governance fact,
not a consequence of merging the 14 upstream-only commits. **Going forward,
independent code-review dispatches in this codebase should default to Opus
at `xhigh`, escalating to Fable at `xhigh` only with a recorded big/complex
classification reason** — this was already true before any merge and remains
true after.

## 3. Do any of the 14 upstream-only commits touch files this session's HXC-159 work also touched?

**No file-level overlap at all.** Computed precisely: `git diff --name-only
$(git merge-base HEAD origin/main)..HEAD` (74 files, the local-only side) vs
`git diff --name-only $(git merge-base HEAD origin/main)..origin/main` (49
files, the upstream-only side), both `LC_ALL=C`-sorted and compared with
`comm -12` — **empty result, zero shared paths.**

Specifically, none of the paths this session's HXC-159 work touched —
`docs/research/extensions/speckit_superpowers/implementation/*` (the Bridge
design corpus), `scripts/extensions/*_bridge*`, `scripts/gates/cm_bridge_*`,
`scripts/gates/cm_no_nested_markdown_fence_leak*`,
`skills/{action-prefix-system,media-validator,multitrack,
reporting-workable-items,scheduled-work-queue,session-sync,
workable-item-lifecycle}/SKILL.md`, `scripts/scheduled-work-engine/*`,
`scripts/skill_activation/README.*`, `scripts/post_update_hook.sh` — appear
anywhere in the upstream-only file set. The upstream-only file set is
`Constitution.md`/`AGENTS.md`/`CLAUDE.md`/`GEMINI.md`/`QWEN.md`,
`.gitmodules`, `scripts/gates/cm_dangerous_combination_fail_closed*`,
`scripts/gates/cm_opendesign_ui_system.sh`, `scripts/gates/gate_ledger_*`,
`scripts/gitleaks/*`, `scripts/hawkscan/*`, `scripts/trivy/*`,
`scripts/zap/*`, `scripts/skill_activation/skill-activate-wrapper.sh`,
`scripts/validation/*`, `skills/skill-catalog/register.sh`, and 17
`submodules/*` nested-submodule gitlinks (plus `submodules/design-toolkit`
pointer bumps and `submodules/clickup_sync/{AGENTS,CLAUDE}.md`).

This predicts, and the simulated merge (§4) confirms, a **clean, conflict-free
automatic merge**.

## 4. Simulated merge — result: CLEAN, zero conflicts

Performed entirely inside a disposable clone at
`$SCRATCHPAD/constitution_merge_sim` (`git clone --no-hardlinks
/home/milosvasic/Projects/helix_code/constitution ...`), never touching the
real checkout:

```
$ git merge --no-commit --no-ff eba38e8938f0ec76eadd02baa124c87e10132061
Automatic merge went well; stopped before committing as requested
$ echo $?
0
```

`git status` after the merge showed `All conflicts fixed but you are still
merging` with **zero** entries under "Unmerged paths" — every one of the 49
changed files (12 modified, 37 new) staged cleanly as `modified:` or `new
file:` with no manual resolution needed. A recursive grep for conflict
markers (`<<<<<<<`, `=======`, `>>>>>>>`) across the merged tree returned
nothing. `git diff --cached --stat` showed 49 files changed, 2,848
insertions(+), 66 deletions(-) — consistent with a fast, mechanical merge of
two disjoint change sets.

The merge was then aborted (`git merge --abort`) and the entire disposable
clone directory deleted. Verified afterward against the **real**
`constitution/` checkout: `git status` still reports the pre-existing
`ahead 12, behind 14` divergence with a clean working tree and nothing
staged; the throwaway `real-upstream-tip` branch ref only ever existed
inside the (now-deleted) disposable clone, `git branch --list
real-upstream-tip` in the real checkout returns nothing; `git worktree list`
in the real checkout shows only the single expected entry
(`.git/modules/constitution e72175f [main]`) — no stray worktree was left
behind.

**Conclusion: the eventual real merge of `origin/main` (`eba38e8`) into local
`constitution` `main` is expected to be clean with no manual conflict
resolution required**, barring any upstream advance between now and when the
real merge is actually performed (a fresh `git fetch` + re-check is still
required immediately before doing it, per §11.4.37/§11.4.71).

## 5. Recommended merge order across the four repos

1. **`constitution/` first.** It is the only one of the four that is behind
   anything, so it is the only one requiring an actual merge step before any
   push. Procedure (per §11.4.113, when actually executed — not part of this
   investigation): `git fetch --all --prune --tags` (again, immediately
   before merging, to catch anything landed since this investigation) → `git
   merge origin/main` (or whichever remote is the most-advanced tip; all 8
   are currently identical at `eba38e8`) onto local `main` → resolve
   (expected: none, per §4) → verify (`pre_build_verification.sh` / gate
   suite) → commit the merge → push to all 8 configured upstreams (each push
   is a fast-forward since the merge commit descends from every remote tip).

2. **Then the meta-repo (`helix_code`), `submodules/helix_agent/`, and
   `submodules/skills/` in any order relative to each other** — none of
   these three needs a merge (they are not behind any remote), so their
   push readiness does not depend on constitution's merge completing. **A
   dependency caveat does exist, though:** the meta-repo currently pins
   `constitution/` at `e72175f` (verified: `git ls-tree HEAD constitution`
   in the meta-repo matches `constitution/`'s current local HEAD exactly).
   Once `constitution/` is merged, its HEAD SHA will change to a new merge
   commit — the meta-repo's submodule pointer will then be one commit stale
   relative to the freshly-merged `constitution/` HEAD until a follow-up
   commit in the meta-repo bumps the gitlink. This does not block pushing
   the meta-repo's *current* 56 local commits (they reference the
   already-correct, already-committed `e72175f`), but the pointer-bump
   follow-up should be sequenced **after** the constitution merge+push, as
   its own small commit, to avoid the meta-repo momentarily pointing at a
   constitution commit that no longer exists as a reachable tip anywhere
   convenient (it still exists in history, just superseded).
   `submodules/helix_agent/` and `submodules/skills/` have no analogous
   dependency on `constitution/`'s SHA (verified: `helix_agent` has no
   `.gitmodules`; `skills` has its own *dormant, uninitialized* nested
   `constitution` gitlink at an unrelated, much older commit `68875c7` — see
   §6 — which is unaffected either way).

3. Minor housekeeping, non-blocking: reattach `submodules/skills/`'s
   detached HEAD to its local `main` branch (`git checkout main` — both
   point to the identical commit `b165b9f`, so this is a no-op content-wise)
   before the eventual push, to avoid needing an explicit `HEAD:main`
   refspec.

## 6. Other observations (not part of a merge-conflict question, noted for completeness)

- `submodules/skills/` carries its own **nested, uninitialized** `constitution`
  submodule reference at `68875c7ace02d83e15f53828a911fb46a97dd9e3` — a
  different, much older commit than the constitution checkout this session
  and this report otherwise refer to (`e72175f` / soon `eba38e8`-merged).
  The directory is empty (`git submodule status` shows the `-` "not
  initialized" prefix). This is dormant and out of scope for the current
  investigation (it is not one of the four checkouts named in the task), but
  is flagged since it is a second, separate reference to "constitution" that
  a future maintainer could otherwise confuse with the real one.
- The meta-repo's `origin` remote has three distinct push/fetch URL entries
  (`git@github.com:HelixDevelopment/code.git`,
  `git@github.com:HelixDevelopment/Helix-CLI.git`,
  `git@gitlab.com:helixdevelopment1/HelixCode.git`) consistent with the
  project's documented multi-upstream fan-out push policy (§2.1) — not a
  discrepancy, noted only for completeness.
- The pre-existing uncommitted working-tree change in the meta-repo
  (`M helix_code/internal/agent/skill_external_policy_test.go`, per the
  session's git-status snapshot) is untouched by this investigation and is
  unrelated to any of the four repos' remote-divergence question.

## Sources verified

All findings in this document are derived directly from `git fetch`, `git
log`, `git show`, `git diff --stat`, `git rev-list --left-right --count`,
`git merge-base`, and one disposable-clone `git merge --no-commit --no-ff`
run in this session on 2026-09-23, against the actual repository states at
that time. No external documentation was consulted.
