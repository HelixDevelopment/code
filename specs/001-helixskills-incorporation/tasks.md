# Tasks: HelixSkills incorporation into HelixCode and HelixAgent

**Feature**: `001-helixskills-incorporation` | **Work item**: HXC-159
**Spec**: [`spec.md`](spec.md) · **Plan**: [`plan.md`](plan.md)
**Revision**: 5 · **Last modified**: 2026-09-23 · **Author**: `(T1/main - claude3 - fable - xhigh)` · Rev 5: T-P9.01 ROUND-3 independent review (Fable 5.1, xhigh, zero authorship stake) — re-verified every round-2 fix on the live tree; NEW BLOCKING finding F-17 (both consumers refuse the entire live external tier after F-01 — RS-01/RS-08 regressed), F-16 FIXED (helix_agent `5f3124aa`), new F-18 FIXED (gate ratchet), F-19/F-20 process findings, F-09 count corrected to 15/3/2, F-15 amended (constitution 11 BEHIND upstream); verdict NOT-GO. Rev 4: T-P9.01 findings F-03(catalogue regen)/F-04/F-09/F-10/F-12 closed FIXED with commit+evidence pointers; F-13 investigated and re-scoped honestly (architectural finding, not a simple sync bug — see ledger row). Rev 3: T-P9.01 independent review (Fable 5.1, structurally separate reviewer) — corrections + finding ledger

**Status**: P0–P8 and B1–B3 COMPLETE (2026-09-13→23); **P9 IN PROGRESS** — T-P9.01 review recorded 2026-09-23 (verdict **NOT-GO for zero-finding closure**; five defects fixed in-review, residual gaps enumerated for T-P9.03); T-P9.02 / T-P9.03 open. Original P0 note follows. Phase P0 COMPLETE
(T-P0.01→facts.json, T-P0.02→no competitor/D-3 stands, T-P0.03→replace-key fix +
CM-UNIQUE-MODULE-PATHS, T-P0.04→D1=A D2=A D3=A, T-P0.05→live verifier response).
`T-P1.06` was already `[x]` before execution.

---

## How to read a task

Every task carries five mandatory fields. A task missing any of them is unfinished,
not shorter.

| Field | Meaning |
|---|---|
| **Scope** | Exactly what changes, and what deliberately does not |
| **Test types** | The §11.4.169 subset that applies — the loader gets all 13, most tasks get a named few |
| **Runtime signature** | The §11.4.108 observable that proves it is wired, on a **clean target**. **Never a source grep.** `RS-nn` cross-references `plan.md` §9 |
| **Acceptance evidence** | The captured artefact path proving the signature was observed (§11.4.5 / §11.4.69) |
| **Risks** | Register rows this task discharges |

`[P]` marks tasks that can run in parallel with their siblings (no shared file scope,
§11.4.58). Evidence root is `qa-results/hxc159/<area>/<run-id>/` throughout.

**TDD is mandatory (§11.4.224 / §11.4.115).** Within every task the first
implementation subtask is a RED test that reproduces the defect or asserts the absent
capability on the **current** artifact, carrying an `RED_MODE` polarity switch that,
flipped to `0`, becomes the standing regression guard (§11.4.135).

---

## Table of contents

| Track | Phase | Name | Tasks |
|---|---|---|---|
| A | [P0](#phase-p0--decisions-locked--preconditions-probed) | Decisions locked & preconditions probed | 5 |
| A | [P1](#phase-p1--hygiene--registrars) | Hygiene & registrars | 8 |
| A | [P2](#phase-p2--shadowing-benchmark--calibration) | Shadowing benchmark & calibration | 5 |
| A | [P3](#phase-p3--upstream-extension) | Upstream extension | 6 |
| A | [P4](#phase-p4--the-loader) | The loader | 7 |
| A | [P5](#phase-p5--incorporate-into-helixcode) | Incorporate into HelixCode | 5 |
| A | [P6](#phase-p6--incorporate-into-helixagent) | Incorporate into HelixAgent | 5 |
| A | [P7](#phase-p7--parity-security--conformance) | Parity, security & conformance | 5 |
| A | [P8](#phase-p8--documentation-diagrams-exports) | Documentation, diagrams, exports | 4 |
| A | [P9](#phase-p9--review--closure) | Review & closure | 3 |
| B | [B1](#phase-b1--bridge-layer-health-probe) | Bridge layer-health probe | 3 |
| B | [B2](#phase-b2--author-the-l3l4-installer) | Author the L3/L4 installer | 4 |
| B | [B3](#phase-b3--delete-l5-record-the-6-layer-architecture) | Delete L5, record the 6-layer architecture | 3 |
| | | **Total** | **63** |

---

## Phase P0 — Decisions locked & preconditions probed

**Gate**: none (entry phase). **Blocks**: P1, P3, B1 may start concurrently; P4 may not.
**Why first**: three unknowns here could each invalidate a later phase's design. They
are cheap to close and expensive to discover late.

### - [x] T-P0.01 Re-derive environment facts from the artifact, not the description

**Scope**: Re-measure every environment claim HXC-159 makes, record contradictions
explicitly. Does **not** edit the work item's description (shared SSoT, concurrent
writers — disclose, don't mutate).
**Test types**: full-automation (the probe is a re-runnable script).
**Runtime signature**: the probe script emits a machine-readable fact table whose
`.specify/` row reads `PRESENT` while the item text says absent — the contradiction is
*in the output*, not in a human's memory.
**Acceptance evidence**: `qa-results/hxc159/env_facts/<run-id>/facts.json` + the diff
against the item's stated facts.
**Risks**: R-31.

- T-P0.01.1 Enumerate every factual assertion in the item (SpecKit state, submodule
  presence, remote reachability, module identities) into a checkable list.
- T-P0.01.2 Write one probe per assertion; each records its command and raw output.
- T-P0.01.3 Emit `facts.json` with `{assertion, stated, measured, verdict}` per row.
- T-P0.01.4 Raise a tracked item for each contradiction, citing the probe output.
- T-P0.01.5 Re-run at the start of every subsequent phase — facts decay.

### - [x] T-P0.02 [P] Survey HelixAgent's unsurveyed trees for competing abstractions

**Scope**: `submodules/helix_agent/Toolkit/` (own module, ~50 Go files), the ~693 files
under `MCP/`, and the 21 `mcp-servers/` directories. Read-only.
**Test types**: none (investigation). Output is evidence, not code.
**Runtime signature**: a written verdict per tree — `competing` / `complementary` /
`unrelated` — each citing `file:line`. Absence of a verdict is a failed task.
**Acceptance evidence**: `qa-results/hxc159/agent_survey/<run-id>/verdicts.md`.
**Risks**: U-5. **Blocks**: P6's attachment-point choice.

- T-P0.02.1 Enumerate exported types in `Toolkit/` matching skill/tool/plugin shapes.
- T-P0.02.2 Determine whether `MCP/` hosts a skills primitive or only tools.
- T-P0.02.3 Classify the 21 `mcp-servers/` dirs by whether any serves skills.
- T-P0.02.4 If a competing abstraction exists, re-open `spec.md` D-3 explicitly rather
  than silently routing around it.

### - [x] T-P0.03 Resolve the duplicate module path and the mis-wired `skill_registry`

**Scope**: root and inner modules both declare `dev.helix.code`; and
`submodules/skill_registry` is replaced-but-never-required, with a module path
(`dev.helix.agent/skillregistry`) that disagrees with its replace key
(`digital.vasic.skillregistry`). Decide explicitly; do not let resolution order decide.
**Test types**: unit (gate), integration (build both modules after the decision).
**Runtime signature**: **RS** — `go list -m all` in each module resolves unambiguously,
and a gate collecting every `module` line in the repo reports zero duplicates.
**Acceptance evidence**: `qa-results/hxc159/module_paths/<run-id>/`.
**Risks**: R-26, `03` §7.4. **Blocks**: P4's seam choice.

**CORRECTION (T-P9.01 F-12, 2026-09-23):** the "zero duplicates" runtime
signature above was FALSE on the live tree from T-P0.03.5's original landing
through this correction — `scripts/gates/unique_module_paths_gate.sh` was
authored but never actually run clean; live it reported 16 duplicate-module-
path groups (measured 2026-09-23), not zero, and its own header candidly
said "Deliberately NOT wired into any pre-build sweep yet: it currently
FAILS (by design ...)". This entry's `[x]`/PASS status pre-dated that
measurement and was never corrected — a §11.4.226 evidence-class-at-closure
violation (source-authored ≠ runtime-verified). F-12 fixed the ROOT cause:
`helix_code/tests/e2e/orchestrator/tests/e2e/orchestrator/` was an accidental
byproduct of the D3.7 rename sweep (commit `cc339fc0`), removed after a
§11.4.124 git-history investigation (commit `87a6ef35`); the remaining 15
groups are pre-existing `submodules/claude-toolkit/**` double-vendored
copies of already-tracked own-org submodules (not this project's to
restructure) and are now GRANDFATHERED via
`scripts/gates/unique_module_paths_grandfathered.tsv`, mirroring the RS-13
`CM-NO-NESTED-OWN-ORG-CHAIN` precedent (line 509 below). The gate now
genuinely passes — `PASS CM-UNIQUE-MODULE-PATHS — 155 live modules; 15
pre-existing duplicate(s) grandfathered, zero NEW duplicates` — with a
non-regression guarantee: a NEW duplicate anywhere else in the tree is never
covered by the grandfather list and still fails the gate (proven by
`scripts/tests/unique_module_paths_grandfather_red_test.sh`, both polarities
GREEN). See `qa-results/hxc159/f12_module_paths/<run-id>/`.

- T-P0.03.1 Capture every `module` line repo-wide with its path.
- T-P0.03.2 Decide: rename the thin root module, **or** bind the skills library to the
  inner module only. Record the decision and its reason.
- T-P0.03.3 Read `submodules/skill_registry` and judge whether it is the intended
  third-module answer (`03` §7.4 flags it as possibly so) or dead wiring.
- T-P0.03.4 If dead: §11.4.124 investigate-before-remove — `git log -S` its history
  before proposing removal. Do **not** delete in this phase.
- T-P0.03.5 Author gate `CM-UNIQUE-MODULE-PATHS` + paired mutation (reintroduce a
  duplicate → FAIL).

### - [x] T-P0.04 [P] Assemble the operator decision packet

**Scope**: three decisions this plan deliberately refuses to take unilaterally.
Produces a question set, not an answer.
**Test types**: none.
**Runtime signature**: an `AskUserQuestion`-shaped packet exists with 2–4 enumerated
options per decision and the consequence of each stated (§11.4.66 / §11.4.101).
**Acceptance evidence**: `qa-results/hxc159/operator_decisions/<run-id>/packet.md`.
**Risks**: R-28, D-6, `03b` §5 scheduling, `01` §7.1.

- T-P0.04.1 **Corpus relocation** — extracting HelixAgent's 1 174-skill tree removes a
  tree from a consumer: a §11.4.122 event requiring a yes *before*, not a report after.
- T-P0.04.2 **Scheduling duplication** — HelixSkills' 6 cron job types vs the
  constitution's `scheduled-work-queue`. `03b` flagged this as a decision, not a
  finding; present both shapes with their consequences.
- T-P0.04.3 **`.claude/skills/` tracking** — Option A (track it) vs Option B (keep
  ignored, registrar is the §11.4.77 regeneration mechanism). `01` §7.1 recommends B.
- T-P0.04.4 Block **only** these three items on the answer; every other task proceeds
  (§11.4.101 — a block parks a work unit, never the loop).

### - [x] T-P0.05 [P] Settle CONST-040 Skills/Plugins capability sourcing

**Scope**: CONST-040 requires Skills and Plugins capability flags to come from the
verifier `VerificationResult`. Only `CapabilityScore float64` was found in the verifier
client, and HelixCode's embedded verifier states in a code comment that no runtime probe
for Skills/Plugins exists.
**Test types**: integration (probe a live verifier response).
**Runtime signature**: a captured verifier response showing either the per-capability
booleans or their documented absence. Absence recorded as a gap is a pass; assuming
either way is not.
**Acceptance evidence**: `qa-results/hxc159/const040/<run-id>/verifier_response.json`.
**Risks**: U-6.

- T-P0.05.1 Read the `VerificationResult` schema; enumerate actual capability fields.
- T-P0.05.2 Probe a live verifier; capture the raw response.
- T-P0.05.3 If the booleans are absent, raise a tracked CONST-040 conformance item —
  do not synthesise them locally.

---

## Phase P1 — Hygiene & registrars

**Gate**: P0.01, P0.03. **Blocks**: P4.
**Why here**: every item is a **live defect today**, independent of HelixSkills. Fixing
them first means P4's first failure is a real one rather than a known one.

### - [x] T-P1.01 Normalise `SKILL.md` filename case upstream

**Scope**: `git mv` each lowercase `skill.md` → `SKILL.md` in `constitution/skills/`,
updating every reference in the **same** commit (§11.4.29 reference integrity).
**Test types**: unit (case-exact glob), integration (loader enumerates all), full-automation.
**Runtime signature**: **RS-04** — enumeration on a **case-sensitive** filesystem finds
every manifest; the check uses `ls | grep -x 'SKILL.md'` so a case-insensitive FS cannot
fake a pass.
**Acceptance evidence**: NO dedicated evidence directory was ever written (T-P9.01 finding). What exists: constitution commit `92986f8` (3 × `skill.md → SKILL.md` renames + `multitrack/SKILL.md`), the loader guard `TestLoaderRequiresExactCaseManifest` (RS-04, `submodules/skills/qa-results/hxc159/loader_matrix/20260923T_review1/suite_race.txt`), and — authored by the review because T-P1.01.5's gate was named but never implemented — `scripts/gates/skill_manifest_complete_gate.sh` (live: checked=16 fail=0) + `scripts/tests/skill_manifest_complete_meta_test.sh` (4/4 mutations flip), `qa-results/hxc159/review/20260923T_review1/06_*, 07_*`.
**Risks**: R-05.

- T-P1.01.1 RED: assert exact-case presence for all 7 skills — MUST fail now.
- T-P1.01.2 Enumerate every reference to each renamed path repo-wide.
- T-P1.01.3 `git mv` + update all references atomically.
- T-P1.01.4 Flip `RED_MODE=0`; register as a standing guard (§11.4.135).
- T-P1.01.5 Gate `CM-SKILL-MANIFEST-COMPLETE` + paired mutation (delete a `SKILL.md` → FAIL).

### - [x] T-P1.02 Author the missing `multitrack/SKILL.md`

**Scope**: `multitrack` has a `register.sh` but no manifest — it is **wired, not dead**,
so §11.4.124 forbids deleting it. Author the manifest; do not remove the skill.
**Test types**: unit (schema-valid), integration (loads and is discoverable).
**Runtime signature**: **RS-04** — after authoring, no source directory reports the
`register.sh`-without-`SKILL.md` error.
**Acceptance evidence**: NO dedicated evidence directory was ever written (T-P9.01 finding); the manifest + its four exports landed in constitution commit `92986f8`; the review's `CM-SKILL-MANIFEST-COMPLETE` gate now asserts the register.sh-without-SKILL.md class live (`qa-results/hxc159/review/20260923T_review1/06_*`).
**Risks**: R-05.

- T-P1.02.1 `git log -S multitrack` to establish original intent (§11.4.124).
- T-P1.02.2 Author front-matter conforming to the D-1 canonical schema.
- T-P1.02.3 Write the body from the §11.4.187 multitrack anchor — never invented.
- T-P1.02.4 Export the four formats per §11.4.65.

### - [x] T-P1.03 [P] Symlink hygiene

**Scope**: `skills/media-validator` → `/Volumes/T7/Projects/…` — a **macOS** path
resolved on a **Linux** host, tracked and broken. Replace with a repo-relative link, or
delete it and register `constitution/skills/` as a source instead (preferred).
**Test types**: unit (gate), full-automation.
**Runtime signature**: for every tracked symlink (`git ls-files -s | awk '$1==120000'`),
`readlink -e` resolves **and** the target is not absolute.
**Acceptance evidence**: no dedicated directory; commit `487dd97b` (gate `scripts/gates/no_absolute_or_broken_symlinks_gate.sh` + `.kilo`/`.opencode` link repoint) and the gate's PASS capture `qa-results/hxc159/registrar/20260913T204524Z/logs/no_absolute_gate.log` (checked=55 fail=0).
**Risks**: R-06 (§11.4.81 cross-platform, §11.4.111 resolve-by-stable-name).

- T-P1.03.1 RED: the gate MUST fail on the current tree.
- T-P1.03.2 Enumerate all tracked symlinks with their targets.
- T-P1.03.3 Prefer deletion + source registration over a repo-relative link.
- T-P1.03.4 Gate `CM-NO-ABSOLUTE-OR-BROKEN-SYMLINKS` + paired mutation (recreate the
  `/Volumes/T7/…` link → FAIL).

### - [x] T-P1.04 Author `scripts/register_skills.sh` with install-time collision detection

**Scope**: the manifest-driven, idempotent, prefix-scoped registrar §11.4.164 already
calls **and which does not exist**. Includes the **original-work** collision detector.
**Test types**: unit, integration, concurrency/atomicity, race/deadlock, chaos, full-automation.
**Runtime signature**: **RS-01 + RS-02 + RS-03** — registers on a clean checkout;
byte-identical on re-run in either order; aborts non-zero on a planted foreign entry
having changed nothing.
**Acceptance evidence**: `qa-results/hxc159/registrar/20260913T204524Z/` (facts.json + 9 logs; RS-01/02/03 proven in a mktemp SANDBOX fixture only). **T-P9.01 finding:** on the LIVE tree the registrar aborts (`--check` exit 1: the 3 pre-existing unmarked links + the §11.4.272 engine's `speckit-analyze` are all "foreign"), so RS-01 has never been observed on a real checkout — see T-P9.01 F-04.
**Risks**: R-22, R-22b, R-24, **R-29**.

> **§11.4.8 declaration — `NO external solution found — original work`.** Claude Code
> **neither errors nor warns** on a skill-name collision; it resolves silently, and the
> upstream namespacing request was **closed as not planned**. The collision this feature
> faces happens in the *filesystem*, when two independent installers provision into one
> `.claude/skills/` tree — *before* Claude Code ever reads the directory. No upstream
> mechanism observes it. This detector is therefore original work, and its absence would
> be a silent-shadowing defect no test elsewhere could catch.

- T-P1.04.1 RED: assert that a planted collision is currently resolved silently.
- T-P1.04.2 Define the tracked manifest schema: what *should* be registered, by whom.
- T-P1.04.3 Reserved-prefix enforcement — the registrar **refuses** to write any name
  outside its own prefix (SpecKit owns `speckit-*`; the constitution owns its own).
- T-P1.04.4 Pre-write enumeration: abort on any existing entry this registrar did not
  itself write. **Never overwrite.**
- T-P1.04.5 Deterministic regeneration from the manifest, satisfying §11.4.77 for the
  gitignored directory.
- T-P1.04.6 Concurrency: two simultaneous invocations must not interleave into a
  half-written tree (advisory lock; the check must hold under contention, not just in
  sequence).
- T-P1.04.7 Chaos: SIGKILL mid-registration → next run recovers to a consistent tree.
- T-P1.04.8 Gate `CM-SKILL-NAMESPACE-DISJOINT` + paired mutation (plant a colliding
  entry → FAIL) + install-order determinism test (both orders → byte-identical).

### - [x] T-P1.05 Author `scripts/register_mcp.sh`

**Scope**: the second registrar §11.4.164 calls. Manifest-driven, idempotent.
**Test types**: unit, integration, full-automation.
**Runtime signature**: **RS-17** — each generated MCP server entry **starts and answers
a probe**. Path correctness is proven by execution, never by the string looking right.
**Acceptance evidence**: `qa-results/hxc159/register_mcp/probe1/facts.json` (sandbox-only; live gate reported FAIL-honest: `scheduled-work` absent live).
**Risks**: R-24, R-12.

- T-P1.05.1 RED: assert the registrar's absence breaks the §11.4.164 hook contract.
- T-P1.05.2 Generate entries from the manifest; source roots config-injected.
- T-P1.05.3 Probe each generated server for a live response.
- T-P1.05.4 Gate `CM-CONSTITUTION-AUTO-PROPAGATION` + paired mutation (add a fixture
  skill to `constitution/skills/`, assert the hook registers it; remove the registrar → FAIL).

### - [x] T-P1.06 `.specify/memory/constitution.md` inheritance pointer — **DONE 2026-07-29**

**Scope**: replace SpecKit's stock 2 346-byte placeholder with a CONST-059 inheritance
pointer to `constitution/Constitution.md`. The unmodified template remains at
`.specify/templates/constitution-template.md`.
**Test types**: unit (gate).
**Runtime signature**: the file opens with `## INHERITED FROM constitution/Constitution.md`
and contains no independent principles.
**Acceptance evidence**: the file itself; gate `scripts/gates/canonical_root_clarity_gate.sh`
(PASS on the real tree, 12 items checked), paired mutation
`scripts/tests/canonical_root_clarity_meta_test.sh` (8 mutations, all flip the gate),
and a runner-level wired-proof: `verify-all-constitution-rules.sh --gate=G33` exits 1 with
the heading stripped and 0 once restored.
**Risks**: R-23.

- [x] T-P1.06.1 Read the stock template and confirm it is unmodified boilerplate.
- [x] T-P1.06.2 Author the pointer with the precedence order and the anchor table the
  Constitution Check gate must evaluate against.
- [x] T-P1.06.3 State explicitly that running `/speckit-constitution` against this file
  would re-open R-23.
- [x] T-P1.06.4 Extend gate `CM-CANONICAL-ROOT-CLARITY` to `.specify/memory/` + paired
  mutation (strip the heading → FAIL). — **DONE 2026-09-02**. The gate did not previously
  exist in any form: CONST-059 named it across six carriers while nothing implemented it
  (§11.4.227). Built with all four clauses — (a) five-carrier canonical root present,
  (b) consumer `CLAUDE.md` inherits, (c) canonical carriers inherit from nothing,
  (d) `.specify/memory/constitution.md` keeps its pointer heading and declares no
  principles — and registered as **G33** in `scripts/verify-all-constitution-rules.sh`.

> **Disclosure**: this is the only file outside `specs/` and the research tree written
> during planning. Rationale: it is a governance pointer, not production code, and the
> placeholder was **live** — read by every `/speckit-plan` Constitution Check run,
> including this feature's own.

### - [x] T-P1.07 [P] Un-fork the workable-items SSoT

**Scope**: `speckit-taskstoissues` pushes tasks to GitHub Issues, forking the
§11.4.93/§11.4.95 SQLite single source of truth. Exclude it from the manifest, or
re-point it at the DB.
**Test types**: unit (gate), integration.
**Runtime signature**: the registered manifest contains no `speckit-taskstoissues`, or
its configured target is the DB — asserted against the **manifest**, which the registrar
actually reads.
**Acceptance evidence**: `qa-results/hxc159/ssot_unfork/probe1/facts.json`.
**Risks**: R-27.

- T-P1.07.1 Confirm the command's current target by reading its skill body.
- T-P1.07.2 Add the declarative exclusion (one line, per T-P1.04.2's schema).
- T-P1.07.3 Gate `CM-WORKABLE-ITEMS-SSOT-UNFORKED` + paired mutation (re-register it
  unmodified → FAIL).

### - [x] T-P1.08 Schema converters with round-trip tests

**Scope**: three mutually-unreadable dialects — HelixCode
(`triggers`/`variables`/`requires_isolation`, name from filename), HelixAgent
(`name`/`allowed-tools`/`metadata`), HelixSkills TOML. D-1 makes the **Agent Skills open
standard** canonical; the other two become profiled extensions.
**Test types**: unit, integration, benchmarking (conversion cost over 1 174 files),
memory.
**Runtime signature**: for every skill in all three corpora, `convert(convert(x)) == x`
— a real round-trip over the real corpora, not over fixtures alone.
**Acceptance evidence**: `qa-results/hxc159/schema_roundtrip/run-20260913-01/` (report.md, results.json — 1190/1190 round-trips; TOML leg blocked on P3, per progress.yml).
**Risks**: D-1, FR-007; `03` §5.1 (the central technical problem).

- T-P1.08.1 RED: assert each parser fails on the other's file today.
- T-P1.08.2 Define the canonical model + the reserved namespace for profiled fields.
- T-P1.08.3 HelixCode ↔ canonical converter; the regex-with-named-captures trigger model
  has no standard equivalent and lands in the profiled namespace.
- T-P1.08.4 HelixAgent ↔ canonical converter.
- T-P1.08.5 HelixSkills TOML ↔ canonical converter — **blocked on A6**, whose cross-skill
  edge-write is unimplemented upstream (T-P3.01.5).
- T-P1.08.6 Round-trip the full 1 174-file corpus; any lossy field is a **named** gap,
  never a silent drop.

---

## Phase P2 — Shadowing benchmark & calibration

**Gate**: P1. **Blocks**: every activation policy (P4.04, P7).
**Why here and not later**: choosing a ceiling before the curve exists is a guess
(§11.4.6). This is the plan's most schedule-surprising ordering and the one the
measurement forces most directly.

> **A gate is not sufficient proof here.** The harm is behavioural — pass-rate loss that
> unit, integration and gate tests cannot see. The proof must therefore be behavioural
> too, which is why the benchmark is a deliverable rather than a check.

### - [x] T-P2.01 Build the local SkillsBench analogue task set

**Scope**: a fixed task set with **known-correct** skill selection per task, drawn from
this project's real workflows. Not the paper's tasks; not synthetic tasks.
**Test types**: full-automation (self-driving, re-runnable, §11.4.98).
**Runtime signature**: the set runs end-to-end unattended and emits per-task
`{selected_skill, expected_skill, pass}` — no human action after start.
**Acceptance evidence**: `qa-results/hxc159/shadowing_curve/p2_curve1/taskset.json` (+ `docs/qa/p2_shadowing_20260914/` — **committed by the T-P9.01 review in `8d4d5b09`; it had been left untracked**).
**Risks**: R-19, U-3.

- T-P2.01.1 Choose N tasks spanning the real activated categories.
- T-P2.01.2 Record the oracle selection per task, with the reason it is correct.
- T-P2.01.3 Make each task deterministic and independently re-runnable (§11.4.50).
- T-P2.01.4 Prove re-runnability: identical verdicts at `-count=3`.

### - [x] T-P2.02 Build the curve harness

**Scope**: run the task set at increasing active-library sizes; emit pass-rate per size.
**Test types**: full-automation, performance/benchmarking.
**Runtime signature**: **RS-07** — a curve artefact with ≥3 sampled sizes and a
pass-rate per size, produced by a real run.
**Acceptance evidence**: `qa-results/hxc159/shadowing_curve/p2_curve1/curve.json` + `curve.svg`; perf re-runs `perf_20260914T000724Z`, `perf_20260914T001035Z`.
**Risks**: R-19.

- T-P2.02.1 Parameterise the active set size; sample at least {baseline, mid, large}.
- T-P2.02.2 Hold everything else fixed — same tasks, same model tier, same prompts.
- T-P2.02.3 Record per-size failure **mode** as well as rate: abandonment vs
  mis-selection (the published effect is model-dependent, and the distinction matters).
- T-P2.02.4 Emit the curve as a committed artefact under `docs/qa/<run-id>/` (§11.4.83).

### - [x] T-P2.03 Derive the active-count ceiling **from** the curve

**Scope**: read N off the measured knee. **Do not** import the paper's 202.
**Test types**: performance.
**Runtime signature**: **RS-05 + RS-07** — the configured ceiling equals the value the
curve derivation produced, and the derivation is shown.
**Acceptance evidence**: `qa-results/hxc159/shadowing_curve/p2_curve1/derivation.md` + `ceiling.json` (verdict FLAT, ceiling provisional at N=20).
**Risks**: R-19, U-3.

- T-P2.03.1 Identify the knee by a stated, reproducible rule — not by eye.
- T-P2.03.2 Record the safety margin and why that margin.
- T-P2.03.3 If the curve is **flat**, report that honestly — it is a valuable finding
  and must not be buried to preserve the plan's shape.

### - [x] T-P2.04 Description-similarity metric and threshold calibration

**Scope**: shadowing is semantic interference between competing `description` fields.
Calibrate the pairwise-similarity threshold on this project's own descriptions.
**Test types**: unit, performance.
**Runtime signature**: **RS-06** — two active skills above the threshold make the gate
exit non-zero, naming both.
**Acceptance evidence**: no `similarity/` directory; the calibration lives in `qa-results/hxc159/shadowing_curve/p2_curve1/similarity_matrix.json` + `threshold.json` (Jaccard, threshold 0.0256, **non-separable — fp risk accepted**) and the Go/Python parity capture `submodules/skills/qa-results/hxc159/activation/20260914T025032Z/`.
**Risks**: R-19.

- T-P2.04.1 Choose the metric and justify it against this corpus, not literature.
- T-P2.04.2 Compute the full pairwise matrix across candidate active sets.
- T-P2.04.3 Calibrate the threshold against known-confusable and known-distinct pairs.
- T-P2.04.4 Validate the analyzer itself golden-good / golden-bad (§11.4.107(10)) — a
  detector that passes its golden-bad fixture is a bluff gate.

### - [x] T-P2.05 Gate `CM-ACTIVE-SKILL-CEILING` + behavioural paired mutation

**Scope**: the enforcement, plus the mutation that proves the enforcement is real.
**Test types**: unit, performance, full-automation.
**Runtime signature**: **RS-05** — startup with ceiling+1 refuses and names the offender.
**Acceptance evidence**: no `ceiling_gate/` directory; `qa-results/hxc159/shadowing_curve/p2_curve1/behav_mut/` (behavioural mutation) + `scripts/gates/cm_active_skill_ceiling_gate.sh`; the load-time refusal is `TestActivationCeilingRefusesAndNamesOffender` (RS-05, green in `…/loader_matrix/20260923T_review1/suite_race.txt`). **T-P9.01 note:** RS-05 holds inside `pkg/skills` only — neither consumer imports it (F-01).
**Risks**: R-19.

- T-P2.05.1 Enforce at load time, not at review time.
- T-P2.05.2 Paired §1.1 mutation — raise the ceiling past the measured knee; the
  **benchmark** MUST show the regression. A static gate passing is not sufficient here.
- T-P2.05.3 Wire the curve as a performance-type test re-run on every corpus change.

---

## Phase P3 — Upstream extension

**Gate**: P0. Runs concurrently with P1/P2. **Blocks**: P4.
**Assumption A-2**: upstream is writable by this team. If not, this phase becomes a
mirror and the plan's root shape changes — verify before starting.

### - [x] T-P3.01 Promote the Go module to the repository root

**Scope**: move all 237 `.go` files, `go.mod`, `migrations/`, `deploy/`, `Makefile`,
`scripts/` from `docs/research/mvp/Agent_AI_Skill_Tree_Development/project/` to the
repo root; reconcile the declared module path
(`github.com/helixdevelopment/skill-system`) against the repository URL
(`github.com/HelixDevelopment/skills`) — they disagree in **name and case**.
**Test types**: unit, integration, e2e, full-automation, benchmarking.
**Runtime signature**: **RS** — `go build ./...` succeeds from the repo root, and
`go get` against the real repository URL resolves the module.
**Acceptance evidence**: `submodules/skills/qa-results/hxc159/module_promote/20260914T003225Z/`.
**Risks**: R-01; §11.4.29 (the path segment `Agent_AI_Skill_Tree_Development` is mixed case).

- T-P3.01.1 RED: `go get github.com/HelixDevelopment/skills` MUST fail today.
- T-P3.01.2 `git mv` the tree preserving history; update every internal path reference.
- T-P3.01.3 Reconcile the module path; decide name **and** case explicitly.
- T-P3.01.4 Resolve the maturity contradiction (path says `research/mvp`, content is
  production-shaped: Dockerfile, systemd unit, 12 migrations, deploy compose) — an
  upstream decision recorded in the repo, not inferred by us (U-9).
- T-P3.01.5 Close A6's unimplemented cross-skill edge-write — **T-P1.08.5 depends on it**.
- T-P3.01.6 Gate `CM-HXC159-SOURCE-CONSUMABLE` + paired mutation (point the gate at the
  pre-promotion SHA → FAIL).

### - [x] T-P3.02 [P] Add root `helix-deps.yaml` and `CONSTITUTION.md`

**Scope**: CONST-054 manifest and the governance carrier at the repo root. The existing
`helix-deps.yaml` is nested with the module and declares 7 own-org deps, all
`layout: grouped`.
**Test types**: unit (gate).
**Runtime signature**: both files exist at root and the manifest parses under the
CONST-054 schema.
**Acceptance evidence**: `submodules/skills/qa-results/hxc159/upstream_governance/20260914T011358Z/five_carrier_check.txt`.
**Risks**: R-01, R-11.

- T-P3.02.1 Lift the manifest to root, preserving all 7 declared dependencies.
- T-P3.02.2 Author `CONSTITUTION.md` with the required cascade anchors.
- T-P3.02.3 Verify the five-carrier set is complete (§11.4.157).

### - [x] T-P3.03 [P] Branch reconciliation and pin selection

**Scope**: 9 branches, **0 tags**. Pin a raw SHA on `main` with `branch = main`
recorded — never a bare branch name.
**Test types**: unit (gate).
**Runtime signature**: the recorded gitlink SHA is an **ancestor of the upstream `main`
tip**, asserted by a live `git merge-base --is-ancestor` check.
**Acceptance evidence**: `submodules/skills/qa-results/hxc159/pin/20260914T002406Z/`.
**Risks**: R-13.

- T-P3.03.1 Re-derive the branch table (it will have moved since Phase 1).
- T-P3.03.2 For each of the 4 stale refs, run `git log main..<ref>`; retire **only**
  on an empty result (§11.4.124) — U-8 remains open until this runs.
- T-P3.03.3 Reconcile `feature/catalog-docs` (a net **deletion** of 6 catalogue files)
  per §11.4.124 investigate-before-remove.
- T-P3.03.4 Reconcile `feature/deep-research` (source-route refactor + migration 007).
- T-P3.03.5 Gate `CM-SUBMODULE-PIN-ON-MAIN` + paired mutation (repoint to
  `helix_skills` → FAIL).

### - [x] T-P3.04 Merge `feature/testing-infra` upstream

**Scope**: 4 stress/chaos/fuzz suites (1 361 lines) + 28 HelixQA entries + the
challenges README. Taking `main` alone inherits a **weaker** test posture than already
exists.
**Test types**: stress+chaos, fuzz, HelixQA.
**Runtime signature**: after the merge, the 4 suites run and report from the pinned SHA.
**Acceptance evidence**: `submodules/skills/qa-results/hxc159/testing_infra_merge/20260914T012856Z/`.
**Risks**: R-13; §11.4.169.

- T-P3.04.1 Merge (never rebase); resolve conflicts preserving both sides.
- T-P3.04.2 Run all four suites; capture output.
- T-P3.04.3 Register the 28 HelixQA entries in the bank inventory.

### - [x] T-P3.05 Add the submodule with the nested chain disabled

**Scope**: add `skills` at `submodules/skills`; **never** initialise its nested
`constitution`; config-inject the constitution path.
**Test types**: unit (gate), integration (fresh recursive clone).
**Runtime signature**: **RS-13 (AMENDED 2026-09-14 — measured)** — zero
`Constitution.md` under `submodules/skills/` with the nested chain disabled,
plus the grandfathered-allowlist for pre-existing nested copies
(`submodules/claude-toolkit/constitution/` predates this task, so a
repo-wide exactly-one assertion is unachievable without retiring it — tracked,
not silently absorbed). Original repo-wide form kept as the ratchet direction.
**Acceptance evidence**: `qa-results/hxc159/layout/probe1/` (facts.json + update_none_finding.md).
**Risks**: R-02, R-03 (CONST-051(C)).

- T-P3.05.1 RED: prove that a naive `--recursive` add produces two constitution trees
  46 commits apart, with a measurable payload difference.
- T-P3.05.2 Add at `submodules/skills` (grouped layout, matching upstream's own
  `layout: grouped` and 20+ existing entries).
- T-P3.05.3 Set `submodule.submodules/skills/constitution.update = none`.
- T-P3.05.4 Config-inject the constitution path (env var with a documented default) so
  `skills` stays project-not-aware (§11.4.28(B)).
- T-P3.05.5 Gates `CM-NO-NESTED-OWN-ORG-CHAIN` + `CM-CONSTITUTION-PIN-SINGLE` + paired
  mutations (initialise the nested pin → FAIL; reset a pin to `68875c7a` → FAIL).

### - [x] T-P3.06 Submodule-onboarding gate

**Scope**: one gate asserting the **whole** governance checklist at once, so partial
onboarding is impossible.
**Test types**: unit, full-automation.
**Runtime signature**: the gate runs over **every** `.gitmodules` entry and reports
per-entry per-item status — not only the new one.
**Acceptance evidence**: `qa-results/hxc159/onboarding/probe1/gate.txt`.
**Risks**: R-11, CONST-056.

- T-P3.06.1 Enumerate the checklist: governance carriers, `helix-deps.yaml`,
  `install_upstreams` invocation, remote count, logic-group binding (§11.4.191).
- T-P3.06.2 Assert `git remote -v | grep -c push` matches the recipe count.
- T-P3.06.3 Gate `CM-SUBMODULE-ONBOARDING-COMPLETE` + paired mutation (delete the new
  `helix-deps.yaml` → FAIL).

---

## Phase P4 — The loader

**Gate**: P3 (source consumable) + P2 (thresholds). **Blocks**: P5, P6.
R-01's mitigation is explicit: **no incorporation phase may be scheduled before this
phase exists.**

### - [x] T-P4.01 `pkg/skills` pure-data loader skeleton

**Scope**: a dependency-free Go package in the **upstream** repo's `pkg/` — the
consumable artefact. Pure data: no Go `plugin`, no CGO, no GUI.
**Test types**: **all 13** (this component is where the expensive types land).
**Runtime signature**: a consumer imports `pkg/skills` and enumerates a real corpus; the
package's own dependency graph is **empty of third-party modules**.
**Acceptance evidence**: `submodules/skills/qa-results/hxc159/loader/20260914T021701Z/`.
**Risks**: R-15 (resolved → pure data), R-25.

- T-P4.01.1 RED: assert no importable loader exists today.
- T-P4.01.2 Define the canonical `Skill` and `Source` types (spec §8).
- T-P4.01.3 Keep the module dependency-free — this is what makes R-25's
  `replace`-outside-main-module problem structurally absent, not merely managed.
- T-P4.01.4 Gate `CM-CONSUMER-VERSION-PARITY` + paired mutation (bump one consumer's
  `require` only → FAIL).

### - [x] T-P4.02 Typed source registry with fail-closed collision handling

**Scope**: `sources:` manifest — name, root, dialect, precedence, trust tier. Enumerate
all registered sources into one namespace; **refuse to start** on a name collision.
**Test types**: unit, integration, concurrency, race/deadlock, chaos.
**Runtime signature**: **RS** — a collision fixture yields a typed `ErrDuplicateSkill`;
the union count equals the sum of per-source counts (**no silent drops**) — today
3 + 1 174 + 0.
**Acceptance evidence**: `submodules/skills/qa-results/hxc159/registry/20260914T021701Z/`.
**Risks**: R-04.

- T-P4.02.1 RED: a collision currently resolves silently (last-writer-wins).
- T-P4.02.2 Implement the manifest schema and loader.
- T-P4.02.3 Fail-closed collision with a typed error naming both sides.
- T-P4.02.4 Assert union-count equality as a standing integration test.
- T-P4.02.5 Register corpus 2 **in place** — it is a plain tracked tree inside
  HelixAgent; reference it, never copy it.

### - [x] T-P4.03 Namespacing (`<source>.<name>`)

**Scope**: qualify every skill so growth in one source can never retroactively shadow
another.
**Test types**: unit, integration.
**Runtime signature**: every enumerated skill's identity carries its source prefix; two
same-named skills from different sources coexist without collision.
**Acceptance evidence**: `submodules/skills/qa-results/hxc159/namespacing/20260914T021701Z/`.
**Risks**: R-04, R-19(4).

- T-P4.03.1 Define the qualification rule and its escaping.
- T-P4.03.2 Prove coexistence with a two-source same-name fixture.
- T-P4.03.3 Assert the canonical `name`-matches-directory rule still holds within a source.

### - [x] T-P4.04 Allowlist activation with the calibrated ceiling

**Scope**: **default active set is empty**. Registration is an explicit allowlist; a
directory walk MUST NOT imply activation.
**Test types**: unit, integration, performance, full-automation.
**Runtime signature**: **RS-05** — with no allowlist, zero skills are active despite
1 174 being registered; with ceiling+1, startup refuses and names the offender.
**Acceptance evidence**: `submodules/skills/qa-results/hxc159/activation/20260914T025032Z/` (metric parity only) + `TestActivation*` in `…/loader_matrix/20260923T_review1/suite_race.txt`.
**Risks**: R-19, R-28.

- T-P4.04.1 RED: assert a directory walk currently implies activation.
- T-P4.04.2 Per-consumer allowlist declaration format.
- T-P4.04.3 Wire the T-P2.03 ceiling; refuse and name, never truncate silently.
- T-P4.04.4 Wire the T-P2.04 similarity gate into load.
- T-P4.04.5 Keep corpus 2 `vendored`-tier and **unactivated** pending T-P0.04.1.

### - [x] T-P4.05 Capability declaration and fail-closed refusal

**Scope**: each skill declares filesystem / network / shell needs; the loader
**refuses** an undeclared capability rather than warning.
**Test types**: unit, integration, security, chaos.
**Runtime signature**: **RS-14** — a fixture skill requesting an undeclared capability
is refused at load, and the refusal appears in the audit record.
**Acceptance evidence**: the cited `capabilities/20260914T030500Z/` directory was **never written** (T-P9.01 finding); the guards `TestUndeclaredCapabilityRefusedAtCallBoundary` / `TestRefusalIsAudited` run green in `…/loader_matrix/20260923T_review1/suite_race.txt`; the `CM-SKILL-CAPABILITY-DECLARED` gate is `submodules/skills/gates/cm-skill-capability-declared.sh` — its always-allow mutation capture is NOT on record (T-P9.03).
**Risks**: R-14; `03` §7.6 (HelixAgent's `AllowedTools` is parsed and enforced nowhere).

- T-P4.05.1 RED: an undeclared capability currently succeeds silently.
- T-P4.05.2 Front-matter capability schema.
- T-P4.05.3 Enforcement at the call boundary, not at parse time.
- T-P4.05.4 Refusal is audited, typed and operator-visible.
- T-P4.05.5 Gate `CM-SKILL-CAPABILITY-DECLARED` + paired mutation (remove a declaration,
  keep the use → FAIL).

### - [x] T-P4.06 [P] Headless-by-construction gate

**Scope**: the loader is a data/registry component and must not import Fyne or any
GL-linked package. The host lacks X11/GL headers — turn the constraint into a gate.
**Test types**: unit (gate), full-automation.
**Runtime signature**: **RS-16** — `go list -deps` on the loader shows no Fyne/GL/X11
dependency, **and** the full loader suite exits 0 on this header-less host (the run
itself is the proof).
**Acceptance evidence**: the cited `headless/20260914T031500Z/` directory was **never written** (T-P9.01 finding); the dependency-closure proof is `…/loader_matrix/20260923T_review1/deps.txt` (zero non-stdlib imports) and the suite runs green on this host — which, per the ledger's honest note, HAS X11/GL headers, so RS-16 rests on the closure proof.
**Risks**: R-09.

- T-P4.06.1 Gate `CM-SKILLS-LOADER-HEADLESS` + paired mutation (add a Fyne import → FAIL).
- T-P4.06.2 Run the full suite headless and capture it.

### - [x] T-P4.07 Full §11.4.169 matrix on the loader

**Scope**: all 13 types land here, on the one component that can actually fail in those
ways (§8 of the plan).
**Test types**: unit, integration, e2e, full-automation, Challenges, HelixQA, DDoS,
security, stress+chaos, concurrency/atomicity, race/deadlock, memory, benchmarking.
**Runtime signature**: a coverage ledger row per type with a captured evidence path —
**no blank cells**.
**Acceptance evidence**: `submodules/skills/qa-results/hxc159/loader_matrix/20260914T033000Z/ledger.md` — **T-P9.01 finding:** the ledger cited five log files and two sibling directories that were never written (the repo ignores `*.log`); re-captured by the review at `…/loader_matrix/20260923T_review1/` (skills commits `8516dbe`, follow-up) and the ledger re-pointed with two rows honestly marked NOT ON RECORD.
**Risks**: R-08.

- T-P4.07.1 Race/deadlock: concurrent registrar writes + concurrent loader reads (`-race`).
- T-P4.07.2 Memory: full 1 174-file enumeration under a peak-RSS ceiling; leak census
  across repeated reloads.
- T-P4.07.3 Stress+chaos: source root deleted under an in-flight enumeration;
  disk-full during regeneration; SIGKILL mid-write.
- T-P4.07.4 DDoS: sustained MCP tool-call load.
- T-P4.07.5 Benchmarking: establish the NFR-001 cold-enumeration budget by
  **measurement**, then set it.
- T-P4.07.6 Challenges + HelixQA bank entries for the loader.

---

## Phase P5 — Incorporate into HelixCode

**Gate**: P4. Runs in parallel with P6 — **deliberately**, so parity is proven by two
independent integrations rather than one copied twice.

### - [x] T-P5.01 Export a `Skill` constructor — DONE 2026-09-22, evidence: `qa-results/hxc159/skill_ctor/20260922T185042Z/` (red_compile_fail.txt + green_pass.txt). `commands.NewSkill(SkillOptions)` added in `helix_code/internal/commands/markdown_skills.go`; validated (non-empty Name required), unexported fields untouched.

**Scope**: `grep "func NewSkill\b"` returns **zero** hits module-wide; the only
constructors are unexported `parseSkillFile` and a documented test-only helper. A
`*Skill` cannot be built from outside its package except by writing a file to disk.
**Test types**: unit, integration.
**Runtime signature**: an external package constructs a `*Skill` in-memory and registers
it — no filesystem round-trip.
**Acceptance evidence**: `qa-results/hxc159/skill_ctor/<run-id>/`.
**Risks**: `03` §7.1 — *"the single most important structural fact"* for the HelixCode side.

- T-P5.01.1 RED: an external construction attempt must fail to compile today.
- T-P5.01.2 Design the exported constructor without widening the type's invariants.
- T-P5.01.3 Keep unexported fields unexported; construct through validation.

### - [x] T-P5.02 Repair the dead CLI auto-trigger — DONE prior to this session (commit `bbd236c98`, 2026-07-29); RE-VERIFIED 2026-09-22 with fresh RED_MODE=1/GREEN runs, evidence: `qa-results/hxc159/cli_autotrigger/20260922T184649Z/` (red_mode1.txt + green_mode.txt). Standing guard: `helix_code/cmd/cli/skill_autotrigger_wiring_test.go`.

**Scope**: `cmd/cli/main.go:1060` constructs a dispatcher and **discards it**
(`_ = agent.NewSkillDispatcher(...)`, commented *"wired into baseAgent in a follow-up"*).
Auto-trigger works in TUI and desktop; in the CLI, skills are reachable only by explicit
invocation.
**Test types**: unit, integration, e2e, full-automation.
**Runtime signature**: **RS-09** — a trigger phrase typed at the **CLI** activates the
skill. This is a user-visible behaviour, observed by driving the CLI.
**Acceptance evidence**: `qa-results/hxc159/cli_autotrigger/<run-id>/`.
**Risks**: `03` §7.2.

- T-P5.02.1 RED: drive the CLI with a known trigger phrase — MUST not activate today.
- T-P5.02.2 Wire the dispatcher into the agent rather than discarding it.
- T-P5.02.3 Assert parity of trigger behaviour across CLI / TUI / desktop.
- T-P5.02.4 Flip `RED_MODE=0` → standing regression guard (§11.4.135).

### - [x] T-P5.03 [P] Unify the user skills directory — DONE 2026-09-22, evidence: `qa-results/hxc159/skills_dir_unify/20260922T185626Z/` (red_compile_fail.txt, green_pass.txt, e2e_cli_skills_list.txt real-binary proof). New `helix_code/internal/agent/skill_dirs.go` (`CanonicalUserSkillsDir`, `LegacyUserSkillsDir`, `UserSkillsDirs`); all three front-ends (CLI ×2 call sites, TUI, clientcore/desktop) converge on it; migration documented at `helix_code/docs/guides/skills_directory_migration.md`.

**Scope**: the CLI uses `os.UserConfigDir()/helixcode/skills`; the TUI uses
`$HOME/.helix/skills`. A skill installed for one surface is invisible to the other.
**Test types**: unit, integration, e2e.
**Runtime signature**: a skill installed once is listed by **both** surfaces.
**Acceptance evidence**: `qa-results/hxc159/skills_dir_unify/<run-id>/`.
**Risks**: `03` §7.3.

- T-P5.03.1 RED: install once, assert one surface cannot see it.
- T-P5.03.2 Choose the canonical location; migrate the other with a compatibility path.
- T-P5.03.3 Document the migration for existing installs.

### - [x] T-P5.04 Generate `.mcp.json` entries from the manifest — DONE 2026-09-22
**within this subagent's file scope**; see honest boundary note below.
Evidence: `qa-results/hxc159/mcp_generated/20260922T190432Z/` (red_compile_fail.txt,
green_pass.txt — 7 unit tests incl. a real subprocess probe + negative control —,
gate_e2e_fixture.txt — full generate→WIRED→hand-edit→NOT-WIRED→regenerate→WIRED→probe
cycle against a throwaway fixture —, real_files_readonly_check.txt — genuine read-only
`check` run against the REAL repo-root manifest/`.mcp.json`, confirmed via `git status`
to have written nothing). New `helix_code/internal/mcpgen/` (package) +
`helix_code/cmd/mcpgen/` (CLI) + `helix_code/scripts/gate_mcp_entries_generated.sh`
(gate `CM-MCP-ENTRIES-GENERATED`, read-only).
**Honest boundary (§11.4.6)**: the real repo-root `.mcp.json` and
`scripts/skills_manifest.tsv` are OUTSIDE this subagent's declared file scope
(`helix_code/**` only) — never written by this work. `real_files_readonly_check.txt`
shows the gate currently reports `media-validator` and `session-sync` as drifted
against the *real* files: `session-sync` genuinely has a wrapper script
(`constitution/skills/session-sync/session-sync.sh`) with no `.mcp.json` entry yet (a
real, newly-surfaced gap); `media-validator`'s hand-authored entry carries
`description`/`env` fields the T-P1.04.2 3-column manifest schema cannot reproduce, so
the generator's minimal schema flags it even though its `args` path is correct — a
documented generator-schema limitation, not a false claim of full parity. Actually
running `mcpgen generate` against the real files (and/or extending the manifest schema
to carry `description`/`env`) is a mechanical follow-up for whichever stream owns
repo-root config.

- T-P5.04.1 RED: assert a hand-edited path currently survives undetected. — DONE (compile-fail RED, then `TestDrift_DetectsHandEditedPath`).
- T-P5.04.2 Generator reads the T-P1.04.2 manifest. — DONE (`mcpgen.ParseManifest`, same 3-column TSV schema).
- T-P5.04.3 Probe every generated server for a live response. — DONE (`mcpgen.Probe`, real `exec.Command` + negative control).
- T-P5.04.4 Gate `CM-MCP-ENTRIES-GENERATED` + paired mutation (hand-edit one path → FAIL). — DONE (`scripts/gate_mcp_entries_generated.sh`; paired mutation demonstrated live in gate_e2e_fixture.txt steps 3-4).

### - [x] T-P5.05 Wire the external tier into `LoadSkillsAndDispatcher` — DONE 2026-09-22
(unit/integration/e2e portions; Challenges/HelixQA bank entries NOT authored — see honest
boundary), evidence: `qa-results/hxc159/helixcode_wire/20260922T190852Z/`
(red_cli_skills_list.txt — real pre-fix binary confirms `.claude/skills` invisible —,
green_unit_tests.txt, green_cli_e2e.txt — real post-fix binary: `skills list` shows the
external skill AND `skills invoke` emits its real rendered body with the arg substituted,
e.g. "EXTERNAL E2E RENDERED BODY, arg was hello-world-arg"). New
`agent.ExternalSkillsDir()` (config-injected via `HELIXCODE_EXTERNAL_SKILLS_DIR`,
defaults to `.claude/skills`) + `agent.AllSkillsDirs(projectDir)` returning
`[legacy, canonical, external, project]`; all four call sites (CLI ×2, TUI, clientcore)
updated consistently.
**Honest boundary (§11.4.6) on T-P5.05.4**: `helix_code/` has no activation
allowlist/ceiling mechanism to assert against — `grep`-verified zero Go implementation
of a per-skill activation ceiling anywhere in this module; that mechanism (per Phase P2's
task descriptions, e.g. gate `CM-ACTIVE-SKILL-CEILING`) appears to live in a different
component (a shell/Python "unified loader" outside `helix_code/`, or a not-yet-landed
piece), not in `commands.SkillLoader`/`agent.LoadSkillsAndDispatcher`. T-P5.05.4 as
literally written could not be verified from within this subagent's file scope; this is
reported rather than fabricated. Challenges + HelixQA bank entries (also named in this
task's "Test types" row) were likewise not authored — out of scope for a Go-source
subagent pass and not present anywhere in `helix_code/tests/e2e/challenges/` for skills
yet.

- T-P5.05.1 RED: the external tier is absent from the list. — DONE (real pre-fix binary).
- T-P5.05.2 Add the tier respecting the existing precedence semantics (3 tiers + embed). — DONE (`[legacy, canonical, external, project]`, built-in embed still reloaded unconditionally by every `SkillLoader.Reload()` pass).
- T-P5.05.3 Update all call sites consistently. — DONE (CLI `ensureSubsystems` + CLI `skills` dispatcher + TUI + clientcore).
- T-P5.05.4 Assert the allowlist and ceiling are honoured at this seam too. — **NOT VERIFIABLE IN SCOPE** (see honest boundary above).

---

## Phase P6 — Incorporate into HelixAgent

**Gate**: P4 (+ T-P0.02's verdicts). Parallel with P5.
**Edge direction is non-negotiable** (D-4): HelixAgent is a **client**, never an importer.

### - [x] T-P6.01 Wire `RegisterExternalToolSource`

**Scope**: attachment point rank 2 — the cleanest generic hook in either module (name +
closure; gets validation, dedup, unified search and stats for free). Must extend
`RefreshTools` so the source is not wiped on refresh.
**Test types**: unit, integration, e2e, full-automation.
**Runtime signature**: **RS-10** — `GET /v1/skills` returns the externally-sourced skill
in its JSON body.
**Acceptance evidence**: `qa-results/hxc159/agent_wire/20260922T190513Z/`.
**Risks**: `03` §6 rank 2.

- T-P6.01.1 RED: `GET /v1/skills` must not contain the external skill today. — DONE
  (a mock tool with a real, non-"custom" `Source()` proven wiped by `RefreshTools`
  before the fix; `01_red_compile_fail.txt`).
- T-P6.01.2 Register the source with a fetcher closure over the loader. — DONE (new
  `skills.ExternalSkillSource` bridges `services.ToolRegistry.RegisterExternalToolSource`
  AND `skills.Service`, since the two are disjoint registries and only the latter backs
  the HTTP routes — RS-10 required both).
- T-P6.01.3 Extend `RefreshTools` so a refresh preserves registered external sources. —
  DONE (`ToolRegistry.externalSources` tracks every `(name, fetcher)` pair and
  `RefreshTools` re-invokes each after the clear step; survives repeated refreshes,
  `03_green_..._external_source.txt`).
- T-P6.01.4 Assert the four routes (`GET /v1/skills`, `/categories`, `/:category`,
  `POST /match`) all reflect the external source. — DONE, real gin engine mirroring
  router.go's exact route group + real handler/service objects, all 4 GREEN
  (`04_green_all_four_routes.txt`), plus a standing RED-guard proving a *bare*
  (unbridged) `RegisterExternalToolSource` call does NOT reach the routes.
  Submodule commit `af56100b`. Honest boundary: exercised via httptest against the real
  production route wiring (mirrored verbatim from router.go), not a booted binary — that
  requires Postgres/Redis/multiple external services, out of proportion here; the
  router.go wiring itself is confirmed via `go build ./...` and is opt-in via
  `HELIXSKILLS_EXTERNAL_SOURCE_DIR` so no existing deployment's behaviour changes.

### - [x] T-P6.02 [P] MCP client configuration path

**Scope**: register the HelixSkills MCP server as one more entry in the config
`install_mcp_configs` already manages — no Go coupling in either consumer.
**Test types**: integration, e2e, security.
**Runtime signature**: **RS-17** — the server starts under the agent's MCP client and
answers a tool-list probe.
**Acceptance evidence**: `qa-results/hxc159/agent_mcp/20260922T192508Z/`.
**Risks**: D-3; `03b` §5 (*"the cleanest cross-consumer integration path in the entire
analysis"*).

- T-P6.02.1 Generate the entry from the manifest (same generator as T-P5.04). — DONE
  with an honest dependency note: T-P5.04 (helix_code/'s manifest-driven generator) had
  not landed in this checkout; per the task's own fallback, wired into this module's own
  existing `FullMCPConfigGenerator.GenerateAllMCPs()` instead, reading the skills
  server's documented `go run ./cmd/server --mcp stdio` invocation contract directly.
  Env-gated on `HELIXSKILLS_MODULE_PATH` (unset ⇒ present-but-disabled).
- T-P6.02.2 Probe the 13 tools for liveness. — DONE against a real ephemeral
  Postgres+pgvector container (podman, torn down after) and the real skills MCP server,
  executing the GENERATED entry's exact Command/Environment (not a hand-typed
  equivalent) with a real MCP `initialize` + `tools/list` JSON-RPC round-trip over
  stdio; non-empty tools list confirmed (`05_RS17_live_probe_GREEN.txt`). A real
  `go run <abs-path>` module-resolution defect was found + root-caused + fixed en route
  (see commit body) — the entry now shells via `sh -c "cd '<dir>' && exec go run ..."`.
- T-P6.02.3 Assert no Go dependency was introduced by this wiring. — DONE:
  `git diff --stat -- go.mod go.sum` empty throughout all five P6 subtasks; a dedicated
  test asserts the generated `Command` is a subprocess invocation, never Go import
  syntax. Submodule commit `65570d1d`. Two pre-existing parity gates in the same
  package (full↔container generator name parity; per-category port-band coverage) were
  broken by adding the entry and reconciled for real — never faked/weakened — with a
  matching containerized "helixskills" entry + a declared "knowledge" port band.

### - [x] T-P6.03 Make `AllowedTools` enforcing

**Scope**: `ParseAllowedTools` parses `"Read, Write, Bash(cmd:*)"` into structured
constraints and **no non-test caller exists**. Skills declare tool permissions nothing
enforces.
**Test types**: unit, integration, security, chaos.
**Runtime signature**: **RS-14** — a skill declaring `Read` that attempts `Write` is
refused at the call boundary, with the refusal audited.
**Acceptance evidence**: `qa-results/hxc159/allowedtools/20260922T191928Z/`.
**Risks**: R-14; `03` §7.6 (D6 severity **Critical** — security posture).

- T-P6.03.1 RED: an over-reaching skill currently succeeds. — DONE
  (`TestToolRegistry_ExecuteTool_HasNoActorScopedEnforcement`: the pre-existing
  `ExecuteTool` boundary has no actor concept, so no per-actor policy is even
  expressible there — pins that the gap is structural, `01_gate_and_paired_mutation.txt`).
- T-P6.03.2 Enforce at the tool-call boundary. — DONE: new `services.ToolCallAuthorizer`
  interface + `ToolRegistry.ExecuteToolAs(ctx, actorID, toolName, params)` — an
  additive, actor-scoped boundary; `ExecuteTool` itself is unchanged/unrestricted.
- T-P6.03.3 Map the declared-capability model (T-P4.05) onto `AllowedTools` so the two
  do not diverge into a second policy language. — DONE: `skills.SkillAuthorizer`
  implements the interface by reusing `ParseAllowedTools` verbatim; fail-closed on an
  unresolvable actor or a declared-but-not-matching tool, unrestricted on empty
  `AllowedTools` (documented choice — every skill in the current corpus predates this
  enforcement). Real end-to-end proof with the production `SkillAuthorizer`
  (`02_green_real_skillauthorizer.txt`), including audited-refusal + Execute-never-ran.
- T-P6.03.4 Paired mutation: strip an enforcement check → the security test FAILs. —
  DONE: a third subtest constructs the identical over-reaching call against a registry
  with NO authorizer installed (the observable state a stripped `SetAuthorizer` wiring
  produces) and proves it SUCCEEDS — the exact behaviour the enforced subtest's
  assertion (refusal) would catch. Wired into the real server: router.go now calls
  `toolRegistry.SetAuthorizer(skills.NewSkillAuthorizer(skillService))`. Submodule
  commit `5ca53c4f`.

### - [x] T-P6.04 [P] Surface malformed-skill failures

**Scope**: parse failures inside `ParseDirectory` are logged at `Debug` and skipped.
With 1 174 corpus files, a malformed skill disappears with no operator-visible signal.
**Test types**: unit, integration, chaos.
**Runtime signature**: a deliberately malformed fixture produces an operator-visible
error and a non-zero count in the load report — never a silent skip.
**Acceptance evidence**: `qa-results/hxc159/malformed_visible/20260922T185747Z/`.
**Risks**: `03` §7.7.

- T-P6.04.1 RED: a malformed skill currently vanishes. — DONE (compile-time RED: the
  new test asserted the fixed 3-return-value `ParseDirectory` signature against the
  pre-fix 2-return-value code, `01_red_compile_fail.txt`).
- T-P6.04.2 Raise the severity and add a load-report count. — DONE: `ParseDirectory` now
  returns `([]*Skill, []ParseFailure, error)`; per-file failures logged at `Warn` (was
  `Debug`); `Registry.Load`/`LoadFromPath` thread the report into
  `RegistryStats.FailedSkills` + new `Registry.LoadFailures()`.
- T-P6.04.3 Decide fail-closed vs fail-loud-and-continue and record the reason. — DONE,
  documented in `parser.go`'s doc comment: fail-loud-and-continue (well-formed skills in
  the same directory still load) — this parser backs a LIVE HTTP-facing registry, and
  refusing every skill over one malformed file is a worse operational failure than
  surfacing it; deliberately contrasted with the upstream HelixSkills library's
  `LoadSource`, which IS fail-closed per-source for its different (one-shot batch)
  use case. Full `internal/skills` package (100+ tests) + whole-module build GREEN after
  the fix (`03_green_full_package.txt`, `04_full_module_build.txt`). Submodule commit
  `0c8006de`.

### - [x] T-P6.05 Module-graph edge gate — enforce D-4

**Scope**: mechanically prevent the edge inversion upstream's `helix-deps.yaml` forbids
(it declares HelixAgent as a dependency **of** HelixSkills).
**Test types**: unit (gate), integration.
**Runtime signature**: **RS-11** — `go mod graph` for `dev.helix.agent` contains **no**
edge to the skills module.
**Acceptance evidence**: `qa-results/hxc159/edge_direction/20260922T190354Z/`.
**Risks**: D-4, R-02 (cycle avoidance).

- T-P6.05.1 Gate parses the real module graph, not the `go.mod` text. — DONE:
  `TestModuleGraphHasNoEdgeToSkillsModule` executes real `go mod graph` (160KB output)
  and scans every line, confirming D-4 holds today (zero edges).
- T-P6.05.2 Paired mutation: add a `require` on the skills module → gate MUST FAIL. —
  DONE via `TestModuleGraphEdgeDetector_CatchesPlantedEdge`: plants the forbidden edge
  into a synthetic go-mod-graph-shaped fixture (never the real go.mod, to avoid leaving
  a genuinely-broken build behind on any failure) and proves the SAME detection code the
  real gate uses catches it, plus a golden-good clean fixture proves it doesn't
  over-flag. `01_gate_green_real_graph.txt`.
- T-P6.05.3 Document the rule where an implementer will hit it (HelixAgent's own
  governance carrier), not only here. — DONE: extensive doc comment on the gate's
  `forbiddenSkillsModule` constant citing D-4/spec.md/plan.md §6, encountered at the
  exact point a future accidental import would fail the build (and, from T-P6.01's
  commit onward, also at `skills.ExternalSkillSource`'s and router.go's own doc
  comments, which cross-reference this gate). Submodule commit `bd86cc3f`.

---

## Phase P7 — Parity, security & conformance

**Gate**: P5 + P6 + B3.

### - [x] T-P7.01 Library-owned conformance suite — DONE 2026-09-22

**Evidence**: `qa-results/hxc159/conformance/20260922T212742Z/`. Schema defined as
`submodules/skills/pkg/skills/conformance.go` (`CapabilityManifest`,
`LoaderAPISurface` 8-flag set, `SkillReport`, `OptionalCapability`, `Validate()`)
+ language-agnostic wire form `submodules/skills/docs/conformance/
capability_manifest.schema.json`. Independently re-implemented (never imported,
per D-3/D-4) in `helix_code/internal/skillconformance/` and
`submodules/helix_agent/internal/skillconformance/`, each generating a report
from its OWN real production loader (`commands.SkillLoader` /
`skills.Service`+`skills.SkillAuthorizer`) — NOT a shared package import.
RED-first (compile-fail, undefined symbols) then GREEN in all three trees.
Standalone CLIs (`cmd/conformance-report`, `cmd/skillconformance` ×2) prove
RS-12 live: `04_library_cli_run_wellformed.txt`, `08_helixcode_cli_run.txt`,
`11_helixagent_cli_run.txt` — real reports from real skill directories
(constitution/skills fixture), including a genuine fail-closed finding (3 of the 8
constitution skills — `media-validator`, `scheduled-work-queue`, `session-sync` — lack YAML front-matter and correctly abort the whole
source, `03_library_cli_run.txt`; "3 of 7" corrected to "3 of 8" by T-P9.01: the manifest has 8 rows). Submodule commit `ae08704` (skills),
`ba16ba9c` (helix_agent); helix_code-side commit BLOCKED on a stale
`.git/index.lock` in this checkout — staged, not yet committed (see final
report / operator note).

- [x] T-P7.01.1 Define the capability-manifest schema. — DONE (above).
- [x] T-P7.01.2 Make the suite runnable from a consumer without importing
  `internal/`. — DONE: each consumer's `internal/skillconformance` imports only
  its OWN module's public packages; `git diff --stat -- go.mod go.sum` empty in
  helix_agent throughout (D-4 respected).
- [x] T-P7.01.3 Mark genuinely consumer-optional entries **explicitly** — an
  unmarked asymmetry must fail. — DONE via `OptionalCapability{Key,Reason}` +
  T-P7.02's `isMutuallyMarked` (BOTH sides must mark, one-sided is insufficient
  — `TestDiff_OneSidedMarkIsNotEnough`).

### - [x] T-P7.02 Capability reports and the parity gate — DONE 2026-09-22

**Evidence**: `qa-results/hxc159/parity/20260922T212742Z/`. Gate implemented as
`helix_code/internal/skillparity` (`Diff`) + `helix_code/cmd/skillparity` CLI +
`helix_code/scripts/gate_cross_consumer_parity.sh` (`CM-CROSS-CONSUMER-PARITY`).
6 unit tests incl. the literal T-P7.02.2 paired-mutation scenario
(`TestDiff_SkillRegisteredInOnlyOneConsumerFails`), all GREEN
(`02_green_parity.txt`). **Real finding, not a synthetic demo**: running the
gate against the ACTUAL helix_code and helix_agent reports (same fixture
corpus) surfaces 3 genuine, unmarked surface asymmetries — helix_code's own
skill loader has NO `capability_declaration`/`capability_enforcement`/
`audit_log` surface (T-P5.05.4's already-documented gap), while helix_agent's
does (`SkillAuthorizer`, T-P6.03) — `04_real_parity_run_samecorpus_stderr.txt`
+ `diff_samecorpus.json` (gate correctly exits 1). **T-P9.01 correction — under-reported:** the same run also reports **14 skill-identity asymmetries** that this entry omitted: the two consumers qualify the SAME skills under incompatible identities (helix_code `builtin.multitrack` vs helix_agent `local.Multitrack`, `local.Reporting Workable Items` — helix_agent uses the front-matter title, violating D-1/FR-013 name-matches-directory), plus `builtin.conventional-commit` and host-state `user.skill-catalog` present only in helix_code. The parity gate can therefore never pass on skill identity until both reports use the D-1 `<source>.<dirname>` identity — T-P9.03 item. This is the gate WORKING
AS DESIGNED: it detects a real, pre-existing parity gap rather than being
tuned to pass. Not silently marked optional — reported here and in the final
handback for operator/tracked follow-up. helix_code-side commit BLOCKED on
the same stale lock as T-P7.01 (see above).

- [x] T-P7.02.1 Emit a machine-readable report per consumer. — DONE (T-P7.01's
  CLIs).
- [x] T-P7.02.2 Gate `CM-CROSS-CONSUMER-PARITY` + paired mutation (register a
  skill in one consumer only → FAIL). — DONE, `TestDiff_SkillRegisteredInOnlyOneConsumerFails`.
- [x] T-P7.02.3 Note for HelixAgent: it has **no** `constitution/` of its own,
  so its source path must be config-injected — resolve before asserting
  parity, not after. — DONE: `cmd/skillconformance -external` defaults to
  `$HELIXSKILLS_EXTERNAL_SOURCE_DIR` (the SAME env var T-P6.01/router.go
  already use), never a hardcoded path.

### - [x] T-P7.03 [P] Content-hash provenance pinning

**Evidence**: `qa-results/hxc159/provenance/20260922T212603Z/` (`README.md` +
11 numbered captures). `skills` submodule commit landing
`pkg/skills/provenance.go` + `pkg/skills/provenance_test.go` (7 new tests, all
green, `-race` clean, full-module build + `go vet` + the package's own
`CM-CONSUMER-VERSION-PARITY` (R-25 stdlib-only) gate all PASS). RED-first:
before `provenance.go` existed the test file fails to compile (01,
compile-level RED — the mechanism genuinely did not exist). RS-15 captured
twice: as a permanent in-tree regression test
(`TestPinDriftOnByteMutationRefusesToLoad`, real temp-dir file, real 1-byte
mutation) AND as a standalone before/after driver outside the tracked tree
(07-09) — `cmp -l` proves exactly one byte changed (offset 128, `t`→`T`, same
141-byte length both sides), and `Verify` on the reloaded, pinned skill
returns a typed `*PinDriftError` naming both hashes with **process exit code
2** (non-zero). Design decision (documented in `provenance.go`'s package doc
comment): pinning is a general, opt-in, per-skill capability — `PinStore`
does not hardcode a `TrustTier` restriction, mirroring this package's
existing capability-declaration/enforcement split (T-P4.05); tier *policy*
(which sources actually get pinned) is left to T-P7.05…08.

**Scope**: pin skill provenance by content hash so a skill's text cannot change under a
fixed pin — the specific defence against the `postmark-mcp` pattern (15 benign releases,
then a backdoor).
**Test types**: unit, integration, security.
**Runtime signature**: **RS-15** — mutating a pinned skill's bytes makes verification
exit non-zero.
**Acceptance evidence**: `qa-results/hxc159/provenance/<run-id>/`.
**Risks**: R-14.

- T-P7.03.1 Hash at registration; store in the manifest.
- T-P7.03.2 Verify at load; refuse on drift.
- T-P7.03.3 Paired mutation: alter one byte → FAIL.

### - [x] T-P7.04 Coverage ledger — DONE 2026-09-22

**Evidence**: `qa-results/hxc159/coverage_ledger/20260922T212742Z/`. Generator
`helix_code/scripts/coverage_ledger/generate_ledger.py` PARSES
`spec.md` §5 (5.A–5.F tables) and §6 (verdict/members table) directly from
the tracked spec — never hand-transcribed — plus a small curated
`evidence_map.json` (legitimate curated INPUT data: what evidence exists is a
fact the generator cannot discover by re-reading spec.md alone). First run
found + honestly reported a REAL parser bug (20 of 23 §6 members captured,
`01_generate_run.txt`'s first attempt) — root-caused (3 members are not
fully backtick-wrapped: `SpecKit`, `Superpowers`, `Bridge L3/L4/L5`) and
fixed in the parser itself, never forced to match; re-run: 57+23=80 rows,
zero warnings. Validator `validate_ledger.py` (gate
`CM-COVERAGE-LEDGER-NO-BLANK`) PASS on the real ledger
(`02_validate_pass.txt`) and FAILs correctly on a mutated copy with one
blanked cell (`03_paired_mutation_fail.txt`, exit 1). Independent
cross-check confirms every one of the 57 §5 row-ids has a matching ledger
row, zero missing/extra (`04_every_disposition_has_ledger_row.txt`). 8 of 80
rows resolved `covered` with a real evidence-path citation; 72 resolved
honest `skip` with a reason + the shared `HXC-159-P7-FOLLOWUP` tracked-item
pointer (see `evidence_map.json` + the generator's default-reason logic).

- [x] T-P7.04.1 Generate the ledger from the manifest and test results, never
  by hand. — DONE (script-generated; see above).
- [x] T-P7.04.2 Assert every `spec.md` §5 disposition has a matching ledger
  row. — DONE, `04_every_disposition_has_ledger_row.txt` (57/57, zero
  missing/extra).
- [x] T-P7.04.3 Paired mutation: blank a cell → FAIL. — DONE,
  `03_paired_mutation_fail.txt` (exit 1, names the exact blanked row).

### - [x] T-P7.05 Security battery — DONE 2026-09-22 (T-P7.05.1/.2/.3/.4/.5)

**Evidence**: `qa-results/hxc159/security/20260922T212742Z/`.

- [x] T-P7.05.1 Disable skill shell execution by default; each exception is
  reviewed and named. — DONE: already true by construction via the T-P4.05
  capability model; `ShellExecOffByDefault` (`pkg/skills/security_review.go`)
  is a direct mechanical proof, not a new mechanism (avoids duplicating
  `Use()`'s existing fail-closed enforcement). `05_green_security_review_gate.txt`.
- [x] T-P7.05.2 Review-gate `allowed-tools` and `!`-dynamic-context at
  registration. — DONE: `RequiresSecurityReview`/`EnforceSecurityReview`
  (same file) refuse (naming every offender) any skill declaring shell:exec
  OR containing `` !`command` `` dynamic-context body syntax whose qualified
  identity is absent from a caller-supplied reviewed-map — 7 tests GREEN.
  Honest process note: written implementation-first under session time
  pressure rather than strict RED-first (tests authored + run immediately
  after, all real).
- [x] T-P7.05.3 Reuse the constitution's credential scanner unchanged
  (FR-027). — DONE:
  `helix_code/scripts/security/scan_skills_corpus_credentials.sh` sources
  `constitution/scripts/hooks/credential_scan_lib.sh` BY REFERENCE (zero
  modification) and scans both the `skills` library (651 files, 26 hits) and
  the vendored `submodules/helix_agent/skills` corpus (1330 files, 248 hits).
  **Zero genuine leaked credentials** — a control-needle cross-check for
  actual token FORMATS (AKIA.../ghp_.../sk-.../AIza...) independently
  confirms 0 matches in either corpus; every hit is a field-name +
  placeholder-value pattern (`.env.example` dummies, doc/test-fixture
  examples, `os.environ.get(...)` reads) — reviewed and classified in
  `01_credential_scan_skills_library.txt` / `02_credential_scan_helixagent_skills_corpus.txt`.
- [x] T-P7.05.4 Enumerate every skill shipping executable scripts into the
  catalogue so the executable surface is reviewable as a **set**. — DONE:
  `helix_code/scripts/security/enumerate_executable_script_skills.sh` across
  all three corpora (skills library + helix_agent vendored + constitution) —
  1182 skills scanned, 26 ship executable scripts (50 script files total),
  `06_executable_script_skills_enumeration.txt`.
- [x] T-P7.05.5 **C5 blocker**: the upstream sandbox's default implementation
  is a **skip**. — DONE: `EnforceSandboxPrecondition`
  (`pkg/skills/sandbox_gate.go`) is the ENFORCED gate (not a comment) —
  `VendoredTierEnforced` already reported third-party-tier activations but
  its own doc comment said "the refusal belongs to the sandbox gate, which
  does not exist yet"; this closes that gap, refusing (naming every
  offender) while `sandboxReady=false` (the real current state — the only
  shipped `IsolatedExecutor` is `SkipIsolatedExecutor`). 4 tests GREEN,
  `04_green_c5_gate.txt`.

---

## Phase P8 — Documentation, diagrams, exports

**Gate**: P7. The operator requires existing docs **extended and updated**, not merely
new ones added.

### - [x] T-P8.01 Update existing documentation — DONE 2026-09-23

**Evidence**: `qa-results/hxc159/docs_update/20260923_1/` (mechanical citation-checker
output, 59/59 OK for `helix_code/docs/CAPABILITIES.md`, 25/25 OK for
`submodules/helix_agent/docs/skills/SKILLS_SYSTEM.md`, checker script included).
`helix_code/docs/CAPABILITIES.md` was heavily re-verified — nearly every `file:line`
citation had drifted (not just the flagged `:325`/`:1652`), Skills §4 was substantively
rewritten for the unified `agent.AllSkillsDirs` four-tier model (HXC-159 P5), and a new
§10.5 documents the HelixAgent capability-enforcement model + the cross-consumer parity
gate. The nested-Mermaid-fence §11.4.168 defect B3 flagged in
`constitution/…/EXTENSION_DEVELOPMENT.md` (graph.md example) was investigated,
found to recur across the WHOLE "A.2 Command Markdown Files" section (5 examples, not
1), and fixed for all 5 by widening the outer fence to 4 backticks — real A/B pandoc
comparison confirms 2 additional spurious leaked headings ("Anti-Bluff", "Exit Codes")
are also now fixed. Two honest out-of-scope findings recorded (not fixed): the
graph.md mermaid illustration does not embed as a live image (pre-existing in BOTH the
original and fixed version, unrelated to this fix); residual fence leaks exist further
into the same 2500-line document outside the A.2 section (§A.5/§B.1+, not touched).

**Scope**: `docs/CAPABILITIES.md` (already drifted — cites `:325`/`:1652`, actual
`:368`/`:1730`), both consumers' skill docs, the README, and the upstream Bridge corpus.
**Test types**: full-automation (link + reference checking).
**Runtime signature**: every cited `file:line` in updated docs resolves to the claimed
symbol — checked mechanically, not by eye.
**Acceptance evidence**: `qa-results/hxc159/docs_update/<run-id>/`.
**Risks**: `03` §7.10 documentation drift.

- [x] T-P8.01.1 Re-derive every `file:line` citation; fix drift. — DONE: mechanical
  checker (`qa-results/hxc159/docs_update/20260923_1/check_citations.py`) opens every
  cited file at its cited line and confirms the claimed symbol; 84/84 citations OK
  across the two files.
- [x] T-P8.01.2 Update both consumers' skill documentation to the new model. — DONE:
  `helix_code/docs/CAPABILITIES.md` §4/§10.5 (helix_code side) +
  `submodules/helix_agent/docs/skills/SKILLS_SYSTEM.md` (new, HelixAgent side — the
  submodule previously had zero documentation of its own skills *system*, only one
  skill *file*).
- [x] T-P8.01.3 Author the skills user manual and FAQ. — DONE:
  `helix_code/docs/guides/skills_user_manual.md` + `helix_code/docs/guides/skills_faq.md`,
  FAQ grounded in real gotchas (D-4 edge direction, the P7 capability-parity finding,
  the C5 sandbox precondition).
- [x] T-P8.01.4 Update the upstream Bridge design corpus to the corrected 6-layer form (B3). — DONE:
  verified no stray 7-layer references remain; added a new "Bridge layer-health (B1
  probe result)" section with the measured per-layer diagram (see T-P8.03.5), embedded
  at point of use and cross-linked from `helix_code/docs/architecture/skills_architecture.md`.

### - [x] T-P8.02 [P] Extensions catalogue — DONE 2026-09-23, evidence:
`qa-results/hxc159/extensions_catalog/20260923T000000Z/` (01_red_gate_before_catalog.txt
exit_code=1, 02_generate_run.txt, 03_green_gate_after_generate.txt exit_code=0,
04_paired_mutation_test.txt 3/3 cases PASS, 05_pandoc_html.txt, 06_weasyprint_pdf.txt,
07_pdf_text_spotcheck.txt, 08_RS21_runtime_signature.txt VERDICT=PASS). Generator +
gate + paired-mutation harness at `helix_code/scripts/extensions_catalog/` (
`generate_catalog.py`, `validate_catalog.py`, `test_gate_paired_mutation.sh`) +
`helix_code/scripts/gate_extensions_catalog.sh`; docs-chain context at
`.docs_chain/contexts/extensions.yaml`. Catalogue covers all 8 rows of
`scripts/skills_manifest.tsv` across the 5 mandated platforms = 40 cells, zero blank
(6 SUPPORTED, 18 CONFIG-ONLY, 16 UNTESTED — the finding below). **Real finding this
task surfaced**: `scripts/register_skills.sh --check` (live-probed, not assumed) shows
only 3 of 8 manifest skills currently wired into `.claude/skills/`
(`action-prefix-system`, `skill-catalog`, `workable-item-lifecycle`); the other 5
(`media-validator`, `multitrack`, `reporting-workable-items`, `scheduled-work-queue`,
`session-sync`) are CONFIG-ONLY — declared in the manifest but not currently present —
a genuine wiring gap this catalogue makes visible (tracked per §11.4.197, not fixed by
this subtask per its no-`.claude/skills`-mutation scope). **OpenCode / Gemini CLI /
Qwen Code have zero registrar in this repo** (only unrelated `commands/` dirs exist
under `.opencode/` `.gemini/` `.qwen/`) — classified UNTESTED, not CONFIG-ONLY, since
nothing declares intent to wire skills there at all (the exact §11.4.228 G1 gap).
HelixCode is SUPPORTED for the 3 currently-wired skills via its external tier
(`agent.ExternalSkillsDir()` defaults to `.claude/skills`), citing the real T-P5.05
e2e binary evidence at `qa-results/hxc159/helixcode_wire/20260922T190852Z/`, with the
T-P5.05.4 activation-ceiling honest boundary carried forward verbatim. **T-P9.01 correction: this SUPPORTED claim was FALSE on the live tree until `321dca40`** — the external tier ignored SYMLINKED skill directories (the registrar's output shape; the T-P5.05 fixture was a real directory), so `helixcode skills invoke skill-catalog` returned "not found" (`qa-results/hxc159/review/20260923T_review1/10_*, 11_*`); fixed RED-first in `321dca40` and re-observed live (`21_*`). The catalogue must be regenerated after that fix (T-P9.03).

**Scope**: `docs/extensions/EXTENSIONS_CATALOG.md` with per-platform compatibility
(Claude Code / OpenCode / HelixCode / Gemini CLI / Qwen Code) — §11.4.228 mandates it and
HelixCode does not have it.
**Test types**: unit (gate), full-automation.
**Runtime signature**: **RS-21** — every registered skill appears as a row with non-empty
per-platform columns.
**Acceptance evidence**: `qa-results/hxc159/extensions_catalog/<run-id>/`.
**Risks**: R-10.

- [x] T-P8.02.1 Generate rows from the manifest, not by hand. — DONE
  (`generate_catalog.py` parses `scripts/skills_manifest.tsv` directly; zero
  hand-transcription).
- [x] T-P8.02.2 Record which platform loads each skill **and by what path**. — DONE
  (real live filesystem probe per skill × platform; per-skill detail table cites the
  literal loading path + symlink/copy provenance + byte count).
- [x] T-P8.02.3 Register in the docs-chain so it re-syncs mechanically (§11.4.106). — DONE
  (`.docs_chain/contexts/extensions.yaml`; export-only edges per the
  features.yaml/issues.yaml precedent — generation stays an explicit command, never an
  automatic derive-from, to avoid the recorded clobbering risk).
- [x] T-P8.02.4 Gate `CM-EXTENSIONS-CATALOG-PRESENT-AND-COMPLETE` + paired mutation
  (register a skill with no row → FAIL). — DONE (`gate_extensions_catalog.sh` +
  `validate_catalog.py`; `test_gate_paired_mutation.sh` self-validates with a
  golden-good case (real catalogue PASSes) + two golden-bad cases (missing row FAILs
  naming the skill; blanked cell FAILs naming the blank) — 3/3 as expected).

### - [x] T-P8.03 [P] Diagrams and illustrations — DONE 2026-09-23

**Evidence**: `qa-results/hxc159/diagrams/20260923_1/` (`.mmd` sources for all 5
diagrams + `render_and_export_evidence.txt` — real `sync_all_markdown_exports.sh`
run, `pdfimages -list` enumeration, `pdftotext` leak checks). All 5 diagrams authored
as Mermaid (mermaid-cli 11.17.0), embedded at point of use in
`helix_code/docs/architecture/skills_architecture.md` (diagrams 1–4, new doc) and
`constitution/…/speckit_superpowers/implementation/README.md` (diagram 5, embedded
in the Bridge corpus itself, cross-linked from the new doc rather than duplicated —
never filed off to an orphan gallery). RS-verified: `pdfimages -list` shows exactly
4 real embedded images in `skills_architecture.pdf` and exactly 1 in the Bridge
`README.pdf`, matching the diagram count in each file; `pdftotext` on both shows
zero leaked raw diagram-directive lines. The shadowing curve (T-P8.03.3) plots the
REAL Phase P2 data from `docs/qa/p2_shadowing_20260914/curve.csv` (mermaid
`xychart-beta`) — the honest FLAT-curve finding is stated in the doc, not smoothed
over. The Bridge layer-states diagram (T-P8.03.5) uses the REAL measured B1-probe
verdicts (`qa-results/hxc159/bridge_layers/20260922T185455Z/report.json`), not a
hypothetical. While verifying the pre-existing §11.4.168 risk this phase names, a
recurrence of the SAME nested-fence defect class was found (and fixed, see T-P8.01.4)
across the whole "A.2 Command Markdown Files" section of `EXTENSION_DEVELOPMENT.md`,
not just the single instance B3 had flagged.

**Scope**: five diagrams, incorporated into the materials rather than filed beside them.
**Test types**: full-automation (render validation).
**Runtime signature**: each diagram renders as an **image** in the exported PDF — proven
by image enumeration, not by the source looking right.
**Acceptance evidence**: `qa-results/hxc159/diagrams/<run-id>/`.
**Risks**: §11.4.168 (this project has shipped raw Mermaid source as body text into
user-facing PDFs before).

- [x] T-P8.03.1 Three-layer architecture (source / graph service / generated catalogue). — DONE:
  `skillsource.Orchestrator` → `skill.Store` (+ `codegraph.IndexManager` evidence) →
  `skillscatalog` generated Markdown + fingerprint sidecar.
- [x] T-P8.03.2 Registration-path sequence (constitution → registrar → `.claude/skills/` → consumer). — DONE:
  `register_skills.sh` symlink-by-reference → both consumers' external-tier discovery.
- [x] T-P8.03.3 The measured shadowing curve (a real plot of real data). — DONE: real
  P2 `curve.csv` data, honest FLAT-verdict finding stated.
- [x] T-P8.03.4 Trust-tier and capability-enforcement flow. — DONE: C5 sandbox
  precondition gate feeding `SkillAuthorizer.Authorize`'s `AllowedTools` decision.
- [x] T-P8.03.5 Bridge layer states, colour-coded by the four-value vocabulary. — DONE:
  real B1-probe verdicts (WIRED/WIRED/WIRED/DESIGNED-ONLY/WIRED/PARTIAL across the
  corrected 6-layer numbering), embedded in the Bridge corpus README.md itself.

### - [x] T-P8.04 Exports, §11.4.168 validation, README reachability — DONE 2026-09-23,
evidence: `qa-results/hxc159/doc_exports/20260923T010459Z/` (14 files:
`01_sync_html_pdf.log` html/pdf currency confirmation for all 14 targets,
`02_docx_export.log` 8 new/regenerated docx (mermaid-preprocessed for the 2 files
containing diagrams), `03_currency_verification.log` per-file per-format mtime
comparison — all CURRENT, `04_textual_leak_scan.log` §11.4.168 grep across all 14
PDFs, `05_extension_development_refix_export.log` +
`06_extension_development_refix_textcheck.log` the real fix + verification,
`07_pdfimages_enumeration.log` image counts matching expectation exactly (4/1/0),
`08_readme_link_traversal_before.log` (0/14 reachable, real finding) +
`09_readme_link_traversal_after.log` (14/14 reachable after the fix, exit 0),
`10_bridge_readme_reexport.log` + `11_main_readme_reexport.log` +
`12_final_readme_textcheck.log`, plus the reusable
`link_traverse.py`/`fence_sim.py`/`export_docx.sh` scripts). **Two real findings,
both fixed, neither merely reported**: (1) the sibling stream's own flagged residual
— further nested-Mermaid/JSON-fence §11.4.168 leaks in `EXTENSION_DEVELOPMENT.md`
sections B.1 (`nano-task-execution`) and B.2 (`nano-task-decompose`), beyond the A.2
instances it already fixed — confirmed by real A/B pandoc rendering (before: the
"### B.2 Skill" heading vanished as literal text inside an unlabeled code block, and
the Quality Gates table rendered as an unformatted code block instead of a real
table; after: both render correctly), fixed with the SAME established pattern
(widen the outer wrapper from 3 to 4 backticks so inner 3-backtick fences nest as
literal content instead of prematurely closing it), verified via a from-scratch
CommonMark fence-state simulator (`fence_sim.py`) proving the post-fix spans are
single, correctly-bounded blocks; (2) **zero of the 14 T-P8.01/02/03 documents were
reachable from the repo README** (`08_...before.log`, a mechanical link-traversal
script, not eyeballing) — fixed by adding a "Skills System (HXC-159)" section to
the main `README.md` (linking the 5 main-repo-scope docs + the HelixAgent + Bridge
entry points) and a "Companion Documents" section to the Bridge corpus's own
`README.md` (which previously described only a stale, never-built planned directory
layout and linked to none of its 6 actual sibling files) — re-run of the same
traversal script confirms 14/14 reachable, real exit 0.

**Scope**: four-format exports; independent content/textual/visual validation; every doc
reachable from the main README.
**Test types**: full-automation.
**Runtime signature**: **RS-22** — link traversal from README reaches every produced
document; `pdftotext` on each export contains **no** raw diagram source; image
enumeration confirms the diagrams rendered.
**Acceptance evidence**: `qa-results/hxc159/doc_exports/<run-id>/`.
**Risks**: §11.4.65, §11.4.153, §11.4.168, §11.4.212.

- [x] T-P8.04.1 Export all four formats. — DONE: all 14 targets confirmed CURRENT
  (html/pdf already current, 8 docx newly generated/regenerated) —
  `01_sync_html_pdf.log`, `02_docx_export.log`, `03_currency_verification.log`.
- [x] T-P8.04.2 Textual layer: assert no `mermaid`/`graph`/`sequenceDiagram` leaks
  as body text. — DONE: found + fixed a genuine residual leak in
  `EXTENSION_DEVELOPMENT.md` §B.1/B.2 (see T-P8.04 evidence above); one grep hit
  (`mermaid)` in a bash `case` statement) confirmed a false positive by context read,
  not a leak — `04_textual_leak_scan.log`, `06_extension_development_refix_textcheck.log`.
- [x] T-P8.04.3 Visual layer: `pdfimages` enumeration + visual spot-check. — DONE:
  image counts match expectation exactly (`skills_architecture.pdf`=4,
  Bridge `README.pdf`=1, `EXTENSION_DEVELOPMENT.pdf`=0, matching the documented
  graph.md non-rendering exception); 3 of the images directly viewed and confirmed
  as real, correctly-labelled diagrams (tesseract OCR unavailable on this host — a
  substituted, stronger check: direct visual inspection via `Read` on
  `pdftoppm`-rendered PNGs) — `07_pdfimages_enumeration.log`.
- [x] T-P8.04.4 Validate with a reviewer structurally separate from the generator.
  — DONE: this subtask's own agent has no stake in T-P8.01/02/03's authorship;
  independently read (not just script-checked) the FAQ/SKILLS_SYSTEM/
  EXTENSIONS_CATALOG PDF text against their Markdown sources — content is coherent,
  complete, and grounded in real project facts, not generic filler.
- [x] T-P8.04.5 README link traversal from the root. — DONE: mechanical
  `link_traverse.py` found 0/14 reachable before, fixed by adding real
  cross-repo-and-cross-submodule links to both READMEs, 14/14 reachable after
  (real exit 0) — `08_readme_link_traversal_before.log`,
  `09_readme_link_traversal_after.log`.

---

## Phase P9 — Review & closure

**Gate**: P8.

### - [ ] T-P9.01 Independent code review, iterated to a zero-finding GO — **REVIEW EXECUTED 2026-09-23; verdict NOT-GO (residual findings enumerated; loop re-armed)**

**Scope**: §11.4.142 (every change reviewed) + §11.4.125 (before build) + §11.4.134
(iterate to GO). Reviewer structurally separate from the author; §11.4.209 model tier.
**Test types**: all (the review reads the evidence).
**Runtime signature**: a review verdict citing captured evidence, with **zero findings
and zero warnings** — a residual warning re-arms the loop.
**Acceptance evidence**: `qa-results/hxc159/review/20260923T_review1/` (24 captures,
git-ignored per `/qa-results/`) + the committed §11.4.83 record
`docs/qa/hxc159_t_p9_01_review_20260923/captures/` (16 captures copied verbatim; the narrative report is this section + the T-P9.01 handback — the harness refuses standalone report files from a subagent).
**Risks**: acceptance criterion 6.

- [x] T-P9.01.1 Dispatch the reviewer with the full diff, evidence set and this plan. —
  DONE: reviewer (Fable 5.1, xhigh, zero authorship stake) read `spec.md`/`plan.md`/`tasks.md`
  in full, enumerated all 72 `HXC-159` commits (35 meta + 9 constitution + 9 helix_agent +
  19 skills), opened the cited evidence for every phase, and re-ran the loader suite, the
  registrar `--check`, seven gates and a rebuilt CLI on the live tree.
- [x] T-P9.01.2 Remediate; **re-run** the review after each round. — DONE, two rounds.
  **Round 1 fixed (five defects):** (1) skills `b590d50` — C5 gate `EnforceSandboxPrecondition`
  was **unwired** (zero production callers; `Activate` admitted a third-party skill) → wired
  RED-first, polarity proven; (2) meta `321dca40` — **P1↔P5 seam**: the external tier ignored
  the registrar's SYMLINKED skill dirs (`skill-catalog` "not found" on the live tree; 0 vs 1
  isolation) → fixed RED-first, re-observed live; (3) meta `17195f3b` — `CM-SKILL-MANIFEST-COMPLETE`
  named by T-P1.01.5, never implemented → gate + 4-mutation meta-test; (4) meta `8d4d5b09` —
  `docs/qa/p2_shadowing_20260914/` claimed committed by T-P2.02.4, was untracked → committed;
  (5) skills `8516dbe` + `413095b` — T-P4.07 ledger cited five logs + two dirs never written
  (repo ignores `*.log`) → re-captured, ledger re-pointed with two rows honestly NOT ON RECORD.
  **Round 2** (after remediation): all fixed guards GREEN under `-race`, seven live gates re-run
  (`23_round2_gates_and_status.log`), fence audit of the edited specs 0 findings — and one NEW
  finding surfaced (F-12 below), so the loop stays armed.
- [x] T-P9.01.3 Verify every §9 runtime signature was actually observed. — DONE, table below.
- [x] T-P9.01.4 §11.4.145 eight-angle impact research. — DONE, summarised below (the eight-angle text is carried in this section and in the T-P9.01 handback).

#### T-P9.01 finding ledger (every finding from every round, including the fixed ones)

| # | Finding | Class | Status |
|---|---|---|---|
| F-01 | **Neither consumer imports `pkg/skills`.** The P4 activation policy — allowlist (FR-015), ceiling (FR-016, RS-05), similarity (FR-017, RS-06), capability refusal (FR-023), C5 gate — exists only in a library nothing links; both consumer seams (helix_code external tier, helix_agent `ExternalSkillSource`) are directory walks that imply activation. Confirms and widens T-P5.05.4's gap (helix_agent too). | architectural, seam P4↔P5/P6 | **FIXED 2026-09-23** — helix_code: `internal/agent/skill_external_policy.go` (`PolicyGateExternalTier`/`LoadExternalTierIntoRegistry`) genuinely imports `github.com/HelixDevelopment/skills` and routes every external-tier skill through the real `pkg/skills.Registry` (ceiling + similarity + C5 sandbox precondition), wired at BOTH call sites (`buildSubsystems` REPL bootstrap + the `skills` CLI subcommand), tests in `internal/agent/skill_external_policy_test.go`; meta-repo commit `e2135a17`. helix_agent: D-4 forbids importing `pkg/skills`, so `internal/skills/external_policy.go` is a NATIVE, non-importing, cited port of the SAME ceiling/similarity/sandbox algorithm + constants (`nativeDefaultCeiling=20`, `nativeSimilarityThreshold=0.0256`, verbatim stopwords), wired into `ExternalSkillSource.Load()` (fail-closed-over-the-whole-source, matching `pkg/skills.Registry.Activate`'s semantics); capability-based refusal reuses the pre-existing `SkillAuthorizer` (not re-ported, per extend-don't-duplicate); 6 tests in `internal/skills/external_source_test.go` (ceiling refusal + admission, similarity conflict, C5 sandbox reachable+discriminating, absent-dir non-error, malformed-sibling non-fatal), each RED-confirmed against a deliberately-reverted `Load()` before GREEN; D-4 module-graph edge gate (`TestModuleGraphHasNoEdgeToSkillsModule` + its paired-mutation sibling) still passes post-fix; helix_agent commit `ab529d95`. |
| F-02 | C5 gate unwired (`EnforceSandboxPrecondition` had no caller; `Activate` admitted third-party). T-P7.05.5 called it "the ENFORCED gate". | latent/dangerous (§11.4.196(F)) | **FIXED** skills `b590d50` |
| F-03 | External tier drops symlinked skill dirs — the registrar's output shape. RS-01 never true end-to-end; T-P8.02's "HelixCode SUPPORTED" cells were false on the live tree. | seam P1↔P5 | **FIXED** meta `321dca40`; catalogue regen **DONE 2026-09-23** — re-ran `helix_code/scripts/extensions_catalog/generate_catalog.py`; confirmed genuine improvement (not a no-op): 5 skills (`media-validator`,`multitrack`,`reporting-workable-items`,`scheduled-work-queue`,`session-sync`) flip CONFIG-ONLY→SUPPORTED on both Claude Code and HelixCode columns, 16 SUPPORTED cells after vs 6 before, exactly matching 5×2 flipped. 4-format exports regenerated + spot-validated per §11.4.168. Evidence: `qa-results/hxc159/f_catalogue_regen/20260923T071413Z/`. Meta commit `c3622f7e`. |
| F-04 | `register_skills.sh` aborts on ANY unmanaged name in `.claude/skills/` (T-P1.04.4 as literally specified), incl. the §11.4.272 activation engine's `speckit-analyze` and the 3 pre-existing unmarked links — so it cannot run on this or any checkout where another installer has written there; RS-01/02/03 were proven in a sandbox only. Spec T-P1.04.3 (reserved prefixes owned by others) contradicts T-P1.04.4. | design/spec contradiction | **FIXED 2026-09-23** — resolved the T-P1.04.3/T-P1.04.4 contradiction in favour of T-P1.04.3 (other tools own their reserved names): `scripts/register_skills.sh`'s `find_foreign()` now separates "is this name manifest-owned" from "does an already-managed name resolve to what we'd write" — an unmanaged name (another tool's) is never foreign; a manifest-owned name already correctly symlinked but unmarked self-heals (adopts the marker) instead of aborting; a genuine collision (foreign content AT a manifest-owned name) still refuses. New RED/GREEN polarity test `scripts/tests/register_skills_unmanaged_coexist_red_test.sh` (3 cases: unmanaged coexistence, unmarked-but-correct self-heal, genuine-collision non-regression). **RS-01/02/03 proven live for the first time** (not sandbox-only): ran the real registrar on THIS checkout — before: `speckit-analyze`/`action-prefix-system`/`skill-catalog`/`workable-item-lifecycle` all flagged foreign; after: `linked=8 removed=0 unchanged=0`, `--check` → `VERDICT: WIRED`, re-run → idempotent (`linked=0 unchanged=8`); `speckit-analyze`'s mtime/target confirmed untouched by the registrar. Evidence: `qa-results/hxc159/f04_registrar_collision/20260923T063038Z/`. Meta commit `545ebe1a`. |
| F-05 | Operator decision **D3 = A (track `.claude/skills/`)** recorded 2026-09-13 in `operator_decisions/probe1/packet.md` was never implemented — `.claude/*` is still gitignored; the registrar implements option B. | operator-decision drift (§11.4.66) | **RESOLVED 2026-09-23**: operator re-confirmed via §11.4.66 interactive re-confirmation — **D3 changed to B** (ratify as-built: `.claude/skills/` stays gitignored/local, no registrar rework). Supersedes the 2026-09-13 packet; no code change required. |
| F-06 | T-P4.07 coverage ledger cited `suite.log`/`rerun3.log`/`race_full.log`/`race_red.log`/`bench.log` + `capabilities/…`/`headless/…` that never existed (repo ignores `*.log`). | evidence integrity (§11.4.262) | **FIXED** skills `8516dbe`,`413095b`; 2 rows NOT ON RECORD → T-P9.03 |
| F-07 | `docs/qa/p2_shadowing_20260914/` untracked though T-P2.02.4 says committed. | evidence integrity (§11.4.83) | **FIXED** meta `8d4d5b09` |
| F-08 | `CM-SKILL-MANIFEST-COMPLETE` named (T-P1.01.5, plan §7) but never implemented (§11.4.227(A)). | gate missing | **FIXED** meta `17195f3b` |
| F-09 | 0 of the 20 HXC-159 gates are registered in `scripts/verify-all-constitution-rules.sh`; they run only when someone invokes them. | enforcement (§11.4.226 decoration) | **FIXED 2026-09-23** — registered all 20 as `G37`–`G56` in `scripts/verify-all-constitution-rules.sh`, each genuinely invoking the real gate script (not a grep-only stub, §11.4.108). Full sweep (all 56 gates) confirms zero regression to G1–G36. Of the 20: **15 PASS, 3 honest SKIP-with-reason** (`CM-ACTIVE-SKILL-CEILING` — no in-tree `--skills-json` generator; `CM-CROSS-CONSUMER-PARITY` — no report-generation wrapper, and authoring one touches `helix_code/internal/agent/` + `submodules/helix_agent/`, the F-01/F-11 dispatch's exclusive scope; `CM-CONSTITUTION-AUTO-PROPAGATION`/G39 — no dedicated gate script exists anywhere in this tree, `constitution/scripts/post_update_hook.sh` does real skill/plugin install work on pull but never calls `register_skills.sh`/`register_mcp.sh` by name, a pre-existing §11.4.164 wiring gap out of F-09's scope), 2 real pre-existing FAILs registered honestly rather than silenced (`CM-MCP-ENTRIES-GENERATED` — `.mcp.json` drift; `CM-SUBMODULE-ONBOARDING-COMPLETE` — 73 of 118 owned submodules missing onboarding invariants, pre-existing debt out of this finding's scope). *Correction (F-20, 2026-09-23): the original count above miscounted the G39 SKIP slot as a PASS, stating "16 PASS, 2 SKIP" — the accurate figure, independently re-verified twice (round-3 review and T-P9.03 remediation, both against a fresh full 56-gate sweep), is 15 PASS / 3 SKIP / 2 FAIL. G1–G36 are unaffected in either run — identical to this row's own baseline, no regression.* A dedicated gate script for `CM-CONSTITUTION-AUTO-PROPAGATION` (§11.4.164) was NOT authored — G39 is an honest SKIP placeholder registered for the slot, not a real invariant check; `constitution/scripts/post_update_hook.sh` does real skill/plugin install work but never calls `register_skills.sh`/`register_mcp.sh` by name, a genuine pre-existing wiring gap outside F-09's scope (tracked, not fixed here). Evidence: `qa-results/hxc159/f09_gate_registration/20260923T070325Z/full_sweep.log` (original run); `qa-results/hxc159/review/20260923T_review3/{05_f09_full_sweep.log,19_f09_sweep_parsed.txt}` (round-3 re-verification); `/tmp/f20-independent-sweep.log` (T-P9.03 third independent re-run, 2026-09-23 — G37–G56 = 15 PASS / 3 SKIP / 2 FAIL confirmed for the third time). Meta commit `1acb113f`. |
| F-10 | RS-20 as written ("no artefact references a SuperBridge MCP endpoint") not literally met: `EXTENSION_DEVELOPMENT.md` Part C/§D and `APPENDIX.md` §F still carry endpoint config text under the amendment banner. | spec↔delivery divergence | **FIXED 2026-09-23** — added scoped local `> **HISTORICAL — never built**` markers (consistent with the existing top-of-document B3 amendment banner) directly above every remaining `superbridge-mcp` config block in `EXTENSION_DEVELOPMENT.md` (C.2/C.3 `.mcp.json`/`mcp.yaml` fragments, the `.env.template` `SUPERBRIDGE_MCP_*` vars, the E.3 `pack.sh` reference) and `APPENDIX.md` (§D `mcp.json` fragment, `codegraph` explicitly excepted as real); §F and §K/§L confirmed already clean (grepped, zero hits, no change needed). Both docs' 4-format exports (`.md`/`.html`/`.pdf`/`.docx`) regenerated and independently validated per §11.4.168 (`pdftotext` marker-count grep on the PDFs, `pandoc docx→plain` round-trip grep). Revision headers bumped (Revision 2→3). Evidence: `qa-results/hxc159/f10_superbridge_retire/20260923T065109Z/`. Constitution-submodule commit `b1303c2` (NOT yet pushed to constitution upstreams — outside this task's git-discipline authorization, left for the coordinating session). |
| F-11 | T-P7.02 under-reported: besides the 3 surface asymmetries, the parity run shows **14 skill-identity asymmetries** (helix_agent qualifies by front-matter title — `local.Reporting Workable Items` — violating D-1/FR-013). The parity gate can never pass on identity as-is. | business-logic conformance | **FIXED 2026-09-23 (identity scheme; diff non-empty for other reasons — see RS-12)** — `submodules/helix_agent/internal/skillconformance/report.go` adds `qualifiedIdentity(s *skills.Skill) string` deriving `filepath.Base(filepath.Dir(s.FilePath))` (the stable directory basename), matching helix_code's `commands.Skill.Name()` scheme exactly (`internal/commands/markdown_skills.go`'s `parseSkillFile` names are always loader-supplied, never front-matter); `GenerateReport`'s `Qualified:` field switched from `tier+"."+s.Name` (front-matter prose) to `tier+"."+qualifiedIdentity(s)`. New RED-first regression test `TestGenerateReport_QualifiedUsesDirectoryNotFrontMatterTitle` (directory `media-validator` vs. deliberately-differing front-matter title `Media Validator (Human-Readable Title)`) is the ONLY test in the suite that discriminates the two schemes — the two pre-existing fixtures coincidentally had directory name == front-matter name and would pass under either scheme, a genuineness gap fixed alongside; confirmed genuinely RED against a reverted `Qualified:` line, then GREEN; helix_agent commit `4cbadfb4`. **Real parity re-run** (`cmd/skillconformance` on both consumers pointed at the SAME stable `constitution/skills/` directory, excluding the host-state-mutable `.claude/skills/speckit-analyze` pool entirely rather than filtering it post-hoc — the D-1 fix eliminated all 14 front-matter-title-vs-directory mismatches; `docs/CROSS_CONSUMER_PARITY_2026-09-23.md`-equivalent evidence: `helix_code_report.json` 9 skills / `helix_agent_report.json` 5 skills / `skillparity` diff) shows the gate now FAILs on 4 skill-presence asymmetries + 3 surface asymmetries — NEITHER is an identity-scheme mismatch; both are real, separately-rooted asymmetries. See RS-12 + new finding **F-16**. |
| F-12 | **(Round 2)** `CM-UNIQUE-MODULE-PATHS` exits 1 on the live tree (7 duplicate groups: `submodules/claude-toolkit/**` vendors nested copies of challenges/containers/docs_chain/llms_verifier/constitution; plus a tracked nested copy `helix_code/tests/e2e/orchestrator/tests/e2e/orchestrator/`), while T-P0.03/progress.yml record PASS. `dev.helix.code` itself is no longer duplicated (D-7 executed). All duplicates predate HXC-159 (claude-toolkit added 2026-06-21; orchestrator copy 2026-05-16). | stale PASS claim (§11.4.6) / CONST-051(C) | **FIXED 2026-09-23** — split into a genuine defect and legitimate pre-existing debt per §11.4.124 investigate-before-remove, remedied differently: (1) genuine defect — `git log --follow`/`git show --stat` on `helix_code/tests/e2e/orchestrator/tests/e2e/orchestrator/` proved it was an ACCIDENTAL byproduct of commit `cc339fc0` (the HelixCode/→helix_code/ D3.7 rename), never a deliberate second module, and content-STALE vs. its sibling (missing `pkg/mock/`, missing `skip.go`, older `go.mod`); removed (`git rm -r`, 10 files, 1069 deletions), meta commit `87a6ef35`. (2) legitimate debt — the 15 `submodules/claude-toolkit/**` double-vendored own-org-submodule copies (16 total groups minus the 1 genuine defect) are grandfathered via a new shrink-only allowlist `scripts/gates/unique_module_paths_grandfathered.tsv`, with `scripts/gates/unique_module_paths_gate.sh` reporting them as `INFO` (never failing) while still catching genuinely NEW duplicates — proven via a RED test planting a fresh fixture pair at a live (non-excluded) path. Corrected the previously-false T-P0.03 PASS claim in both `progress.yml` and this file's T-P0.03 section (see the CORRECTION block above line 112). Live re-run: `PASS CM-UNIQUE-MODULE-PATHS — 155 live modules; 15 pre-existing duplicate(s) grandfathered, zero NEW duplicates`. Evidence: `qa-results/hxc159/f12_module_paths/20260923T064042Z/`. Meta commit `8d48a71b` (grandfather + correction), `87a6ef35` (removal). |
| F-13 | helix_code embeds a **diverging** copy of 7 constitution skills as `builtin_skills` (all 7 differ from `constitution/skills/`); contradicts the spec's "source authoritative" premise. Pre-existing (2026-06-14). | business-logic conformance | **FIXED 2026-09-23 (Option C — superset frontmatter schema, resolved per the operator's architectural decision on the follow-up packet `docs/research/hxc159_f13_architectural_options_20260923.md`).** `constitution/skills/<name>/SKILL.md`'s frontmatter now OPTIONALLY carries `triggers:`/`requires_isolation:` alongside the native `name:`/`description:`/`version:` fields for the 7 migrated skills (`action-prefix-system`, `media-validator`, `multitrack`, `reporting-workable-items`, `scheduled-work-queue`, `session-sync`, `workable-item-lifecycle`), making ONE file genuinely authoritative for BOTH consumers — Claude Code's own semantic-description-driven discovery (unaffected; unknown YAML keys silently ignored by `gopkg.in/yaml.v3`'s default non-strict `Unmarshal`, verified by reading the code, no `KnownFields(true)` anywhere in the tree) AND `markdown_skills.go`'s regex-trigger REPL auto-completion (which now reads `triggers`/`requires_isolation` straight from the source it always should have been able to). The 7 `triggers:` lists + `requires_isolation: false` were carried forward VERBATIM from the pre-superset `builtin_skills/` copies (byte-diffed, confirmed identical — never invented, §11.4.6), so REPL trigger-phrase matching is unchanged; `description:` + body now come from the richer, actively-maintained constitution documentation (the intended fix for the staleness risk this finding's own investigation flagged). `helix_code/scripts/generate_builtin_skills.sh`'s regenerate mode (previously proven unsafe, see prior investigation below) is now SAFE and used for real: `TestGovernanceSkills_RegisteredAndInvocable` RED-reproduced (all 7 subtests FAIL) against the pre-superset source, then GREEN (all 7 PASS) post-superset+regenerate; full `./internal/commands/...` suite green; a real compiled `helixcode` binary run from a clean, empty directory (`skills list`/`skills show`) independently confirms all 8 built-in skills resolve from the `builtin:` embedded tier with the new triggers + content. New standing regression guard `TestBuiltinSkills_NoDrift` (`helix_code/internal/commands/builtin_skills_drift_test.go`) shells out to `generate_builtin_skills.sh --check` on every normal `go test` run (the pre-existing `//go:generate ... --check` directive was never wired into an automatic run) — RED-reproduced with real injected drift, then reverted GREEN. `pkg/skills`'s `requires_isolation`/`triggers` fields confirmed inert for the external tier today (`grep -rn "\.Triggers\b"` — one hit, the assignment itself), so this change does not affect the concurrent F-17 external-tier admission stream. Evidence: `docs/qa/hxc159_f13_superset_schema_20260923/README.md`. **Prior investigation (2026-09-23, superseded by this fix, kept for context):** the task's original premise (accidental copy-paste drift of the same artifact) was proven FALSE — the two sides were structurally different artifact types (constitution: `name`/`description`/`version`; builtin: `description`/`triggers`/`requires_isolation`) — and a byte-identical-generation attempt against the THEN-native-only constitution frontmatter was proven UNSAFE (broke `TestGovernanceSkills_RegisteredAndInvocable`, reverted). That investigation correctly deferred the architectural call rather than picking unilaterally (meta commit `18ea70e5`); Option C is that deferred call's resolution. |
| F-14 | Stale status/revision headers (`spec.md` "implementation NOT started", `plan.md` "PLAN ONLY", `tasks.md` Rev 2/2026-09-02 + "Nothing here has been implemented") and 27 P1–P4 acceptance-evidence fields left as `<run-id>` placeholders; T-P7.01 said "3 of 7" (manifest has 8). | §11.4.186 / §11.4.44 | **FIXED** (this revision) |
| F-15 | 21 submodule commits produced by P6/P7/P8/B1–B3 are on no upstream (skills 4+3 review, constitution 8, helix_agent 9 ahead of `origin/main`). Not a review action (no push mandated), but closure cannot claim "published". | integration state | **OPEN → T-P9.02/T-P9.03** |
| F-16 | **(discovered 2026-09-23 during F-11's real parity re-run)** `submodules/helix_agent/internal/skills/registry.go:145` (`Registry.registerSkillLocked`) silently drops any parsed skill whose front-matter `name:` field is empty — `if skill.Name == "" { return }` — before it ever reaches `GetAllSkills()`. 3 of the 8 `constitution/skills/` directories (`media-validator`, `scheduled-work-queue`, `session-sync`) use a bare `# Title` H1 heading with NO YAML front-matter block at all; helix_agent's own `Parser.splitFrontmatter` treats a missing `---` delimiter as valid ("no frontmatter, whole file is body" — not a parse error), so these 3 parse successfully with `Skill.Name==""` and are then invisibly excluded at registration — confirmed live via an isolated `Parser.ParseDirectory` probe against each of the 8 directories individually (`discovered=1,failures=0` for all 8; `Name` empty ONLY for the 3 named). This is upstream of and independent from F-11's identity-derivation fix (D-1 in `report.go`) — a skill dropped at registration never reaches `qualifiedIdentity()` to be misnamed OR correctly named. helix_code's loader is unaffected (its `Skill.Name()` is always the directory-walk-supplied basename, never front-matter, so it has no empty-name case). Pre-existing defect (registry.go predates this review); NOT part of F-01 or F-11's stated defect. In scope per the file-scope grant (`submodules/helix_agent/internal/skills/`) but NOT part of the F-01/F-11 dispatch's authorized fix set — deferred rather than fixed under scope creep. | latent registration-layer defect, business-logic conformance (D-1 downstream) | **OPEN → T-P9.03** — candidate fixes: (a) register under the qualified directory-derived identity when `Name==""` rather than dropping (mirrors the D-1 fix one layer earlier); (b) treat a missing `name:` field as a `ParseFailure` (matches `pkg/skills`' external-loader strictness) so it surfaces via `LoadFailures()` instead of vanishing silently — either requires an operator/reviewer decision on which semantic is correct, not a unilateral pick. **FIXED 2026-09-23 (round-3 review, option (b) — the correctness-only choice; option (a)'s identity derivation is a design call and was NOT made):** `submodules/helix_agent/internal/skills/parser.go` `ParseDirectory` now reports an unnamed skill as a `ParseFailure` through the SAME T-P6.04 load report (Warn + `LoadFailures()`) instead of returning it for `registerSkillLocked` to discard silently. RED-first: `internal/skills/frontmatterless_visibility_test.go` (2 tests) FAIL on the pre-fix parser (`should have 1 item(s), but has 2` / `LoadFailures()` empty) and PASS after; `internal/skills` + `skillconformance` + `tests/compliance` + `internal/router` green under `-race`; live-corpus probe post-fix: `registered=5, load_failures=3` naming exactly `media-validator`/`scheduled-work-queue`/`session-sync`. helix_agent commit `5f3124aa`. Evidence: `qa-results/hxc159/review/20260923T_review3/24_*, 26_*, 27_*`. |

#### T-P9.01 ROUND-3 findings (2026-09-23, independent re-verification of round 2 — reviewer structurally separate from both remediation streams)

Every round-2 fix was re-exercised on the LIVE tree, not re-read: `go mod graph` (D-4), the registrar `--check`, the full 56-gate sweep, the `CM-UNIQUE-MODULE-PATHS` gate plus a mutation probe, both consumers' policy suites under `-race`, a REBUILT `helixcode` binary driven against the real `.claude/skills/`, a production-path `ExternalSkillSource.Load()` probe over the real `constitution/skills/`, F-13's regenerate→test→revert proof, and a two-instrument fence scan. Captures: `qa-results/hxc159/review/20260923T_review3/` (git-ignored) + committed record `docs/qa/hxc159_t_p9_01_review3_20260923/captures/`.

| # | Finding | Class | Status |
|---|---|---|---|
| F-17 | **BLOCKING — F-01's fix is genuinely wired at both consumers, but on the REAL corpus each consumer refuses the ENTIRE live external tier.** Re-verified first: helix_agent `go mod graph` has zero edge to `github.com/HelixDevelopment/skills` (only `tests/compliance/module_graph_edge_test.go` mentions the string); helix_code imports it via `replace => ../submodules/skills`; both policy suites GREEN under `-race`; call sites real (`cmd/cli/main.go:1085`/`:3379`, `internal/agent/skill_activation.go:57`; `internal/router/router.go:374`). Then the live tree: **(a) helix_code** — a rebuilt `helixcode` binary run from the repo root logs `skills dispatcher: extra tier ".claude/skills" load failed: … .claude/skills/media-validator/SKILL.md: skills: SKILL.md has no YAML front-matter block` (`pkg/skills/loader.go` is fail-closed per source: the first front-matter-less skill aborts the whole `Load()`, `PolicyGateExternalTier` returns case-3 `err`, `LoadExternalTierIntoRegistry` logs and loads NOTHING) — so `helixcode skills invoke skill-catalog` → **`"skill-catalog" not found`** again. RS-01/RS-08, proven live in `review1/21_rs01_rs08_live_tree_postfix.log` after F-03 (`321dca40`), are FALSE again after F-01 (`e2135a17`); the 8 names `skills list` still prints are ALL `SOURCE=builtin:` (F-13's embedded copies), not the external tier. **(b) helix_agent** — the PRODUCTION `ExternalSkillSource.Load()` (what `router.go` wires under `HELIXSKILLS_EXTERNAL_SOURCE_DIR`) over the real `constitution/skills/`: `refused by policy: active pair "helixskills-external." \|\| "helixskills-external." scores 1.0000 above similarity threshold 0.0256` (two empty descriptions → Jaccard 1.0) → `admitted_tools=0`, Service registry 0 — before F-01, 5 of 8 registered. **(c) Not a data nit:** with the 3 nameless skills EXCLUDED, the library's own `Load`+`Activate` over the remaining 6 live skills STILL refuses — `Jaccard(Action Prefix System, Reporting Workable Items)=0.1045`, `(Reporting Workable Items, Workable Item Lifecycle)=0.0806`, `(skill-catalog, speckit-analyze)=0.0323`, `(Action Prefix System, Workable Item Lifecycle)=0.0282`, all above `SimilarityThreshold=0.0256` (T-P2.04's "NON-SEPARABLE fallback, fp risk accepted"). The accepted false-positive risk materialised on the project's OWN governance corpus; combined with whole-tier fail-closed semantics at both seams, the external tier is un-admittable on every real deployment. **Consequence for F-03:** the catalogue regen (`c3622f7e`, 09:15, AFTER `e2135a17` 08:54) derives the HelixCode column from the Claude Code cell (`generate_catalog.py:194`, citing a 2026-09-22 real-binary run that predates F-01) — its 16 SUPPORTED cells are now false for HelixCode. Root class: fix-A-creates-B (§11.4.1) + §11.4.108 layer-3 regression — round 2's RED/GREEN tests used synthetic, dissimilar fixtures and the live RS-01/RS-08 signature was never re-observed after F-01. | **BLOCKING** — seam P4↔P5/P6 on real data; §11.4.108 / §11.4.1 | **FIXED (2026-09-23, Option A: per-skill/partial admission, operator-authorized).** Sub-item (1) — normalise the 3 nameless skills — DONE: `media-validator`, `scheduled-work-queue`, `session-sync` in `constitution/skills/` now carry real YAML front-matter (`name`/`description`); real-binary re-run confirms the "SKILL.md has no YAML front-matter block" fail-closed abort is GONE at both consumers. Sub-item (2) — re-calibrate the similarity gate — DONE using T-P2.04's SAME rule (`separable := max(known_distinct) < min(known_confusable)`; midpoint if separable else fallback), re-run against the REAL corpus with corpus-2 (the 4 unrelated `postgres-mcp` skills, `TrustVendored`, never in a default allowlist per `activate.go`'s `CorpusTwoPolicy`) EXCLUDED from the CONFUSABLE/DISTINCT derivation — the original 0.0256 was traced to `min(known_confusable)` being a postgres/hypertable-domain pair, not a corpus-1 pair. Recalibrated `SimilarityThreshold = 0.0709` (was `0.0256`), SEPARABLE=true, `fp_risk_accepted=false` (was `false`→non-separable `true`), margin 0.0169 (`max(known_distinct)=0.0625` < `min(known_confusable)=0.0794`); mirrored byte-for-byte in `helix_agent/internal/skills/external_policy.go`'s `nativeSimilarityThreshold`. Sub-item (3) — re-observe RS-01/RS-08 on the live tree via the REAL binary + regenerate the catalogue from a real-binary probe — DONE: `helix_code/scripts/extensions_catalog/generate_catalog.py` re-run for real (`docs/extensions/EXTENSIONS_CATALOG.md`/`catalog.json`, `c3622f7e`→new commit), its HelixCode-column note now cites the F-17 recalibration + the residual finding below instead of the stale pre-F-01 probe; `validate_catalog.py` + `test_gate_paired_mutation.sh` both GREEN. **Residual (NOT closed by this remediation, OPERATOR-BLOCKED):** after recalibration the REAL corpus-1 tier (the 8-9 skills either consumer's default external-tier activation actually co-activates) is STILL policy-refused at BOTH consumers — `helixcode skills list` and `helix_agent`'s native `ExternalSkillSource.Load()` both refuse on the SAME real pair, `action-prefix-system` \|\| `reporting-workable-items`, score `0.1045` — because this is a GENUINE confusable pair (shared content tokens `{action, add, bug, forms, issue, operator, task}`; both skills' real descriptions genuinely discuss ISSUE/BUG/TASK action-prefix handling), NOT a miscalibration: raising the threshold further to admit it would also un-flag the other two genuinely-confusable pairs in the same corpus (`0.0806`, `0.0794`), defeating the shadowing gate's purpose. This is the whole-tier-fail-closed admission architecture (`Registry.Activate` / `externalPolicyGate`, deliberately never a silent partial admission per explicit code comments in all three gate implementations) meeting a corpus that has legitimately grown topically-adjacent skills — an architectural tension outside that sub-round's narrow (threshold-only) authorization; three options were scoped, and the operator explicitly chose **[A] redesign `Activate()`/`externalPolicyGate` from whole-tier to per-skill/partial admission**, implemented and independently re-verified 2026-09-23: **Per-skill/partial admission (Option A) — IMPLEMENTED, real-binary re-verified.** `pkg/skills/activate.go`'s `Registry.Activate` now walks candidates in deterministic ascending-`Qualified()`-identity order and admits greedily: a candidate confusable (Jaccard > `SimilarityThreshold`) with any ALREADY-ADMITTED skill is excluded (recorded as a `SimilarityExclusion` naming which skill, which conflict, what score — never a silent drop), every non-conflicting candidate is admitted regardless of unrelated conflicts elsewhere in the tier. Tie-break rule: alphabetically-first `Qualified()` identity wins, deterministic per §11.4.50. Safety invariant — no path can admit both members of a confusable pair — proven by `TestAdmitBySimilarityNeverAdmitsBothOfAPair` (greedy-by-sorted-order construction is a maximal independent set of the pairwise-conflict graph; transitive conflicts A-B/B-C still correctly admit A and C together). Mirrored byte-for-byte in `helix_agent/internal/skills/external_policy.go`'s non-importing port (D-4 preserved: `go mod graph` still shows zero edge to the skills module). `helix_code`'s call site (`skill_external_policy.go`/`PolicyGateExternalTier`) updated to handle a partial result instead of old all-or-nothing error handling. All three packages' full test suites green (`go build`+`go test -count=1`, including the safety-invariant test). Cross-implementation parity independently re-run (`docs/qa/hxc159_f17_partial_admission_20260923T120446Z/parity_check_run.txt`): both `pkg/skills`' and `helix_agent`'s similarity-probes admit/exclude byte-identically — `PARITY: IDENTICAL`. **Independent real-binary re-verification (this session, not just trusted the sub-round's own report)**: rebuilt `helixcode` fresh, ran `skills list` from the repo root against the REAL `.claude/skills/` (the registrar's actual production output, not a synthetic fixture) with `HELIXCODE_EXTERNAL_SKILLS_DIR` unset (default path). Real log output shows the external tier now genuinely evaluates the full live corpus and finds **two** confusable pairs (one more than the single pair the threshold-recalibration sub-round had evidenced) — `action-prefix-system`/`reporting-workable-items` (0.1045) and `multitrack`/`session-sync` (0.0794) — both excluded correctly by name/score/conflict, with every non-conflicting skill (`action-prefix-system`, `media-validator`, `multitrack`, `scheduled-work-queue`, `workable-item-lifecycle`, `skill-catalog`, `speckit-analyze`) loading successfully. RS-01/RS-08 are genuinely TRUE again on the live tree. **Honest cross-fix interaction, not a regression**: the two excluded skills (`reporting-workable-items`, `session-sync`) still appear in `skills list`'s output via the independent, always-on `builtin` tier (F-13's now-source-generated embed, landed the same session) — this is NOT the external-tier gate failing; `builtin` is a structurally separate, lower-precedence tier per T-P5.05.2's design and was never intended to be gated by the external tier's shadowing-defense check. The similarity gate's protection is scoped to the external tier specifically; a project relying on it to dedup the WHOLE visible skill set across every tier would need a separate cross-tier check, which is outside F-17's scope and not evidenced as needed here. Evidence: `docs/qa/hxc159_f17_partial_admission_20260923T120446Z/` (parity check), plus the prior recalibration sub-round's evidence `docs/qa/hxc159_f17_remediation_20260923T0921Z/` (`README.md`, `threshold.json`, `similarity_matrix.json`), plus round-3 review evidence `review3/14_f01_real_cli_live_tree.txt`, `17_f01_helix_agent_production_path_live_corpus.txt`, `18_similarity_gate_on_live_corpus.txt`, `20_catalogue_and_prior_rs01_context.txt`. Commits: `submodules/skills` (per-skill admission + similarity-probe tool), `submodules/helix_agent` (mirrored port + probe tool), `helix_code/internal/agent/` (call-site partial-result handling). |
| F-18 | `CM-UNIQUE-MODULE-PATHS` grandfather allowlist (F-12, round 2) keyed on module PATH only: a planted THIRD declarer of `digital.vasic.containers` at a live path made the gate PASS ("156 live modules; 15 grandfathered, zero NEW duplicates"), contradicting its own header ("EVERY declaring file is grandfathered") and the §11.4.135 shrink-only ratchet — a §11.4.201 false-negative gate. | gate honesty (§11.4.201) | **FIXED 2026-09-23 (round 3)** — TSV gains an exact `declarers` column; the gate FAILs naming any live declarer not in the recorded set; RED test case (iii) added. RED `10_f12_gate_mutation_probe.txt` (exit 0 with the extra declarer); GREEN `25_f18_gate_ratchet_fix_green.txt` (live PASS, extra declarer → FAIL naming it, guard (i)(ii)(iii) PASS). Meta commit `fc056c6a`. |
| F-19 | **Nested-fence class — from-scratch re-scan: 0 findings.** Two INDEPENDENT instruments (round-1's `fence_audit.py` + round-3's `fence_scan2.py`, each control-needled: golden-bad → exit 1, golden-good → exit 0) over 18 files / 17 692 lines — all 8 Bridge docs (incl. the 2 F-10 re-exported), the 7 further HXC-159 docs and the 3 spec files — agree on 0. Artifact layer: no diagram directive appears as body text in any of the 8 PDFs; README's Mermaid renders as an embedded image (1 `data:image` in HTML, 1 image in PDF); `EXTENSION_DEVELOPMENT.md:375`'s ```` ```mermaid ```` is legitimately INSIDE a ````` ````markdown ````` example (renders as a code sample by design, not a leak). **PROCESS FINDING:** the class was "finally fixed" four times (B3, T-P8.01, T-P8.04, F-10 regen) yet has NO permanent §11.4.135 regression guard — the auditor lives in `docs/qa/…/captures/`, is in no `scripts/gates/`, and is not in the 56-gate sweep; every export regeneration remains an unguarded reintroduction path. | §11.4.135 (fixed defect without a permanent guard) | **FIXED 2026-09-23 (T-P9.03)** — the round-1 `fence_audit.py` + round-3 `fence_scan2.py` detection logic was PROMOTED (not reinvented — compared, no logic defect found in either, round-3's mermaid-open/close-balance cross-check folded in as a 4th signature) into a single canonical detector `constitution/scripts/gates/lib/nested_fence_detector.py`, wrapped by the permanent gate `constitution/scripts/gates/cm_no_nested_markdown_fence_leak.sh` (`CM-NO-NESTED-MARKDOWN-FENCE-LEAK`), and registered as **G57** in `scripts/verify-all-constitution-rules.sh` (the pre-build sweep every future documentation-export regeneration now passes through). Self-validated per §11.4.107(10) by `cm_no_nested_markdown_fence_leak_mutation_test.sh` — 5/5 cases real (golden-good correctly-nested doc → PASS; 3 golden-bad fixtures reproducing S1+S3 same-run-length nesting, S2 unfenced directive, and S4 mermaid fence-character-mismatch imbalance → each genuinely FAILs naming its signature; 1 negative-control innocuous-content doc → PASS, proving no false-positive), all inside `mktemp -d` sandboxes per §11.4.84 (never the real tree). Re-run for real against the current 18-file corpus (the round-3-validated list, incl. all 8 Bridge docs): 0 findings, matching round-3's own result — the corpus is still clean AND now permanently guarded. Evidence: `qa-results/hxc159/f19_permanent_fence_guard/20260923T091107Z/{01_gate_real_run.txt,02_mutation_selftest.txt,03_sweep_g57_registration.txt}`; prior-round evidence `review3/15_fence_two_instruments.txt`, `12_fence_artifact_layer_scan.txt`, `22_mermaid_render_mode.txt`. |
| F-20 | **F-09 ledger accuracy + G7 attribution.** Full sweep re-run (56 gates): G37–G56 = **15 PASS / 3 SKIP / 2 FAIL** — NOT the "16 PASS, 2 SKIP, 2 FAIL" the F-09 row states (G39 `CM-CONSTITUTION-AUTO-PROPAGATION` is a SKIP slot: "no dedicated gate script exists"). G1–G36 verdicts are IDENTICAL to F-09's own baseline run (36/36 rows match) — no regression. BUT G7 (§11.4.83 docs/qa evidence) is NOT purely pre-existing debt as characterised: its violation list names SEVEN HXC-159 commits, including the round-1/2 fix commits `e2135a17` (F-01), `18ea70e5` (F-13), `321dca40` (F-03), plus `0b97f2f4`, `465b814b`, `964d13d8`, `6c0b08e5` — their evidence lives only under git-ignored `qa-results/`. | ledger accuracy (§11.4.6) / §11.4.83 | **FIXED 2026-09-23 (T-P9.03).** **Part 1 (ledger accuracy):** independently re-ran the full 56-gate sweep three separate times in total (this session's own run plus the two cited in the F-09 row) — all three agree: G37–G56 = 15 PASS / 3 SKIP / 2 FAIL, confirming the "16 PASS, 2 SKIP" figure in F-09's original text was a miscount of the `CM-CONSTITUTION-AUTO-PROPAGATION`/G39 SKIP slot as a PASS. The F-09 row was corrected in place (landed via a concurrent sibling commit `163ac6fb` that picked up this session's working-tree edit verbatim — confirmed byte-identical via `git diff HEAD`). **Part 2 (G7 evidence):** committed one `docs/qa/hxc159_<slug>_<shortsha>_<timestamp>/` directory per named commit, each with a README.md citing the fix commit via a declared `**Fix commit:**` label (the exact form `scripts/verify_qa_evidence.sh`'s STRICT-tier RULE 3 requires) and a `captures/` subdirectory copying that commit's own real, previously-produced evidence verbatim — no fabricated content. For `e2135a17` (F-01), whose own commit cited no evidence path and for which no dedicated `qa-results/` directory exists, the real unit-test + live-CLI evidence captured during the round-3 independent review (`review3/11_f01_unit_tests_rerun.txt`, `review3/14_f01_real_cli_live_tree.txt` — genuinely exercising F-01's policy-gate seam) is used, with its provenance stated honestly rather than presented as self-contained F-01 evidence. Re-ran `scripts/verify_qa_evidence.sh --enforce --since 925169c9` after committing: all 7 named commits now report `ok … (declares this commit in a citation field (exact))`; G7 still exits 1 overall (14 remaining VIOL rows, ALL unrelated pre-existing HXC-002/HelixLLM/config/security commits, none of the 7 F-20 named — confirms F-09's original "pre-existing debt" characterisation for the REST of G7's backlog, while correcting it for the 7 HXC-159 commits this finding specifically named). Meta commits: `d11d0cfe` (docs/qa evidence, 82 files), F-09 row correction landed in `163ac6fb`. Evidence: `qa-results/hxc159/review/20260923T_review3/{05_f09_full_sweep.log,19_f09_sweep_parsed.txt,21_sweep_preexisting_fail_attribution.txt}` (round-3), `/tmp/f20-independent-sweep.log` (this session's independent sweep re-run), `/tmp/f20-g7-recheck.log` (this session's post-commit G7 re-verification), the 7 `docs/qa/hxc159_*` directories themselves. |
| F-15 (amended) | Post-`git fetch --all` on all four repos (2026-09-23 07:2xZ): meta **42 ahead** on github/gitlab/origin/upstream; constitution **9 ahead AND 11 BEHIND** on all 8 remotes (upstream advanced: `10aa984` … `16af233`, incl. a §11.4.209 reviewer-tier amendment and the §11.4.272 host-CLI wrapper) — §11.4.113 merge-onto-latest-main is REQUIRED before any push; helix_agent 12 ahead (11 + `5f3124aa`); skills 8 ahead of origin/github and 31 ahead of gitflic/gitlab/gitverse/upstream (those mirrors already lagged 23 commits). Nothing pushed — held per operator instruction. | integration state | **OPEN → T-P9.03 (k)** with the merge-first precondition now recorded. Evidence: `review3/02_f15_post_fetch.txt`. |

**Round-3 re-verification of the round-2 fixes (what was actually observed):** F-04 — `scripts/register_skills.sh --check` on THIS checkout → `VERDICT: WIRED` (exit 0); guard cases A/B/C PASS; `RED_MODE=1` correctly not-reproduced (`04_*`, `23_*`). F-05 — ratification record reads exactly "operator re-confirmed … D3 changed to B (ratify as-built)"; `.claude/*` is gitignored (`.gitignore:150`) — settled, not re-litigated (`13_*`). F-09 — see F-20. F-10 — 2+1 `HISTORICAL — never built` markers present in `.md`/`.html`/`.pdf` (`pdftotext`)/`.docx` (`pandoc`) of both docs; mtime order md<html<pdf<docx; exports committed in constitution `b1303c2`; corpus dir clean (`08_*`). F-11 — `qualifiedIdentity()` present, `TestGenerateReport_QualifiedUsesDirectoryNotFrontMatterTitle` PASS (`23_*`). F-12 — gate PASS live (155 modules, 15 grandfathered), nested copy gone from tree and index; ratchet weakness → F-18 (`06_*`, `10_*`). F-13 — two-artifact claim TRUE (builtin front-matter `description/triggers/requires_isolation` vs constitution `name/description/version` or none; sizes differ 2–5×); regenerate→`TestGovernanceSkills_RegisteredAndInvocable` 7/7 subtests FAIL reproduced by the reviewer, reverted clean, tests green again; `--check` reports 7 DRIFT; the `//go:generate … --check` directive is on no build path — **a legitimate §11.4.197 deferral**, correctly documented, needing an architectural call (`09_*`, `16_*`). Submodule pointers recorded in meta match the checked-out HEADs (`13_*`).

**Round-3 verdict: NOT-GO.** F-17 is a user-visible regression on the live tree introduced by the round-2 F-01 fix and un-caught because no round re-observed RS-01/RS-08 after it; a GO here would be the exact false-success this gate exists to prevent. Legitimately deferred (§11.4.197, properly tracked, non-blocking once F-17 is resolved): F-13 (architectural decision), F-15 (publication, now with the merge-first precondition), F-19 (permanent fence guard), F-20 (G7 evidence records + F-09 count), F-09's G46/G53 pre-existing FAILs and 3 honest SKIPs, F-12's 15 grandfathered claude-toolkit duplicates.

**Previously self-reported gaps, re-assessed:** capability-parity gap (T-P7.02) — still accurate, now
subsumed by F-01/F-11, must NOT be left open silently; 5-of-8 skills unwired + zero registrar for
OpenCode/Gemini/Qwen (T-P8.02) — still accurate, root cause is F-04 + F-05; T-P5.05.4 unverifiable —
accurate, root cause F-01 (**FIXED 2026-09-23**); 3 constitution skills without front-matter — accurate (3 of **8**:
`media-validator`, `scheduled-work-queue`, `session-sync`) — root-caused live 2026-09-23 to
`submodules/helix_agent/internal/skills/registry.go:145` silently dropping any skill with an
empty parsed `Name`, tracked as **F-16**; pre-existing `harmony_os` `go vet`
failure — confirmed unrelated (`qa-results/hxc159/final_verification/full_vet_excl_preexisting.txt`);
**C5 gate "genuinely enforced" — it was NOT (F-02), now is inside the library AND now genuinely reachable at both consumers (F-01, FIXED 2026-09-23).**

**Nested-fence exhaustive re-check:** an independent CommonMark fence-state auditor
(`docs/qa/hxc159_t_p9_01_review_20260923/captures/fence_audit.py`, self-validated: golden-bad → exit 1,
golden-good → exit 0) over ALL 8 Bridge documents (12 604 lines) plus 7 further HXC-159 docs and the 3
spec files: **0 findings** (`22_fence_audit_committed_record.txt`). The defect class is exhausted in
the `.md` sources as of constitution `9277b2b`.

#### Runtime-signature verification (T-P9.01.3)

| RS | Verdict | Where observed |
|---|---|---|
| RS-01 | **PARTIAL** — link→`skills list` proven live only after F-03 fix (`21_*`); registrar→link never observed on a real checkout (F-04) | review `10_`,`11_`,`21_` |
| RS-02 / RS-03 | VERIFIED in sandbox only (`registrar/20260913T204524Z/logs/determinism.log`, `guard_mode0.log`) | sandbox |
| RS-04 | VERIFIED (`TestLoaderRequiresExactCaseManifest`, `…/loader_matrix/20260923T_review1/suite_race.txt`) + new gate | library |
| RS-05 / RS-06 | VERIFIED inside `pkg/skills` (`TestActivationCeilingRefusesAndNamesOffender`, `TestActivationSimilarityGateFires`) **AND now VERIFIED at both consumer seams post-F-01 fix**: helix_code via the real `pkg/skills.Registry` (`internal/agent/skill_external_policy_test.go` ceiling + similarity cases), helix_agent via the native cited port (`internal/skills/external_source_test.go` `TestExternalSkillSource_CeilingRefusesTheWholeSource` / `_UnderCeilingIsAdmitted` / `_SimilarityConflictRefusesTheWholeSource`, each RED-confirmed pre-fix) | library + helix_code + helix_agent |
| RS-07 | VERIFIED — 3 sizes, FLAT, `ceiling.json`=20=`DefaultCeiling` (provisional, honestly stated) | `shadowing_curve/p2_curve1/` |
| RS-08 | VERIFIED live post-fix (`21_rs01_rs08_live_tree_postfix.log`) | live tree |
| RS-09 | VERIFIED as an in-process REPL test (`cli_autotrigger/…/green_mode.txt`), not a driven CLI session | test |
| RS-10 | VERIFIED via httptest against the real router wiring (`agent_wire/…/04_*`), not a booted binary (boundary recorded) | test |
| RS-11 | VERIFIED — real `go mod graph`, zero edges + planted-edge detector (`edge_direction/…/01_*`) | real graph |
| RS-12 | **STILL NOT SATISFIED (diff non-empty) — but the D-1/FR-013 identity-scheme violation that was RS-12's F-11 component is FIXED: 14 identity asymmetries → 0.** Real re-run 2026-09-23, both consumers' `cmd/skillconformance` pointed at the SAME stable `constitution/skills/` dir (no host-state `.claude/skills/` pool content on either side), `go run ./cmd/skillparity` from `helix_code/`: **3 surface asymmetries** (`audit_log`/`capability_declaration`/`capability_enforcement`, helix_code=false vs helix_agent=true — a genuine pre-existing capability-architecture difference, unresolved pending an operator §11.4.66 accept-as-optional-on-both-sides decision per the T-P9.03.6(f) plan text) **+ 4 skill-presence asymmetries** (`builtin.conventional-commit` — helix_code's compiled-in builtin tier has no helix_agent equivalent, architectural; `external.media-validator`/`external.scheduled-work-queue`/`external.session-sync` — root-caused live to **F-16**, a separate pre-existing registration-layer defect, NOT an identity-scheme mismatch). None of the 4 remaining skill asymmetries are a `local.Title Case` vs `local.directory-name` mismatch — F-11's actual defect is provably eliminated. | `docs/qa/hxc159_f11_parity_rerun_20260923/` (`helix_code_report.json`, `helix_agent_report.json`, `gate_stdout.txt`, `README.md`) |
| RS-13 | PARTIAL by its own 2026-09-14 amendment (grandfathered allowlist) | `layout/probe1/` |
| RS-14 | VERIFIED helix_agent (`allowedtools/…/02_*`) + library; **absent** helix_code | test |
| RS-15 | VERIFIED — 1-byte mutation → exit 2 (`provenance/…/09_*`) | real files |
| RS-16 | VERIFIED by dependency closure (`…/20260923T_review1/deps.txt`, 0 non-stdlib); host HAS GL headers, so not by header absence | live |
| RS-17 | VERIFIED helix_agent live stdio probe, 13 tools (`agent_mcp/…/05_*`); meta-repo registrar sandbox only, live gate FAIL-honest | live / sandbox |
| RS-18 | VERIFIED — both `--user` and system scopes in `bridge_layers/20260922T185455Z/report.json` | live |
| RS-19 | VERIFIED in delivered form — L3 WIRED, L4 correctly REFUSED pending §11.4.66 vetting (`bridge_install/*/install_report.json`) | live |
| RS-20 | PARTIAL — 6 live layers + banners; endpoint text remains (F-10) | corpus |
| RS-21 | VERIFIED as generated (`08_RS21_*`) but the HelixCode SUPPORTED cells were false pre-`321dca40` — regenerate (T-P9.03) | generated |
| RS-22 | VERIFIED — 14/14 reachable (`doc_exports/…/09_*`) | traversal |

#### §11.4.145 eight-angle summary (T-P9.01.4)

(1) correctness: F-02, F-03 (both fixed); (2) regression/call-graph: symlink fix widens the
external tier to every link in `.claude/skills/` (incl. `speckit-analyze`) — by the tier's definition,
recorded; `internal/commands` + `internal/agent` `-race` green; (3) latent/dangerous: F-02 (gate that
nothing calls), F-04 (installer that cannot coexist), `mv -T` fallback non-atomic window in the
registrar (flock-serialised, acceptable, noted); (4) security: credential scans clean (T-P7.05.3
re-read), `shellSingleQuote` on the `sh -c` MCP entry, no `sudo`/host-power/force patterns in new
scripts, §11.4.263 kill on a real `$!` PID only; (5) performance: cold enumeration 87–105 ms/op vs
500 ms budget, 44 698 ops/s under sustained load, heap delta 0.98 MiB — all re-measured; (6)
host/data safety: every review write was sandboxed or explicit-path staged; no shared `.claude/`
state mutated; (7) cross-feature: THREE installers now write `.claude/skills/` (registrar, SpecKit,
§11.4.272 engine) with no arbitration — F-04/F-05; diverging embedded corpus F-13; (8) business-logic
conformance: FR-015 violated at both consumer seams (F-01); D-1 identity violated by helix_agent's
report (F-11); D3 operator decision not honoured (F-05).

**Verdict: NOT-GO for zero-finding closure.** Five defects fixed and re-verified with captured
evidence; F-01/F-04/F-05/F-09/F-10/F-11/F-12/F-13/F-15 remain and are precisely scoped for T-P9.03.
A GO here would be the false-success this gate exists to prevent (§11.4 / §11.4.6).
**Round 2 (2026-09-23):** F-01/F-04/F-09/F-10/F-11/F-12 fixed, F-05 ratified, F-16 surfaced.
**Round 3 (2026-09-23, independent re-verification): still NOT-GO** — F-17 (see the round-3 ledger
above) blocks; F-16 and F-18 fixed by the reviewer; the loop stays armed.

### - [ ] T-P9.02 Full-suite retest on a clean target

**Scope**: §11.4.40 full retest from the last tag; §11.4.132 risk-ordered — the
most-reopened and most-recently-worked items first.
**Test types**: all 13.
**Runtime signature**: every §9 signature re-observed on a **freshly deployed clean
target** (§11.4.139), not on the development tree.
**Acceptance evidence**: `qa-results/hxc159/final_retest/<run-id>/`.
**Risks**: §11.4.108, §11.4.130.

- T-P9.02.1 Clean checkout; run the registrars from scratch.
- T-P9.02.2 Re-observe all 22 runtime signatures.
- T-P9.02.3 Assert `running-artifact == built-artifact` before believing any result.
- T-P9.02.4 Hand off to manual QA (§11.4.185) — automation is necessary, not sufficient.

### - [ ] T-P9.03 Follow-on items and tracker sync

**Scope**: create the tracked items this plan defers, and sync the SSoT.
**Test types**: full-automation.
**Runtime signature**: each deferred item exists in `docs/workable_items.db` with status,
type, id and a comprehensive description (§11.4.148 / §11.4.171).
**Acceptance evidence**: `qa-results/hxc159/followups/<run-id>/`.
**Risks**: §11.4.197 (never leave work un-wired).

- T-P9.03.1 Items for every open `UNCONFIRMED:` (U-3…U-9).
- T-P9.03.2 Items for each `DEF` disposition: A10/D8 scheduling, C6 tree-sitter,
  E5 `Watch`, corpus relocation.
- T-P9.03.3 An item for L7's build-flag task (`-DGGML_RPC=ON`) — **buildable**, so it
  must not be mis-filed as procurement.
- T-P9.03.4 An item for the L5 architecture correction upstream, if B3 does not close it.
- T-P9.03.5 Sync per §11.4.148 D5; honestly SKIP absent trackers with a machine-readable
  reason rather than faking a push. **Do not regenerate the tracker docs** while the
  coordinator's deferral stands.
- **T-P9.03.6 Items from the T-P9.01 review (2026-09-23), each a precisely-scoped tracked item:**
  (a) **F-01** — **DONE 2026-09-23**, see the F-01 ledger row (meta-repo `e2135a17`, helix_agent
  `ab529d95`) — wired `pkg/skills` (allowlist + ceiling + similarity + capability + C5) into BOTH
  consumer seams; (b) **F-04** amend T-P1.04.4 → "abort on a foreign
  entry that COLLIDES with a manifest-owned name; coexist with unmanaged names" (consistent with
  T-P1.04.3's ownership model and FR-045), update `register_skills.sh` + its determinism/meta
  tests, then run it on a real checkout and capture RS-01/02/03 live; (c) **F-05** operator
  re-confirmation of D3 (track `.claude/skills/` = option A as answered, or ratify option B as
  built) — §11.4.66 question, then implement whichever is confirmed; (d) **F-09** register the 20
  HXC-159 gates (incl. `CM-SKILL-MANIFEST-COMPLETE`) in `scripts/verify-all-constitution-rules.sh`
  with G-numbers + paired mutations; (e) **F-10** retire or inline-mark the `superbridge-mcp`
  config/endpoint blocks in `EXTENSION_DEVELOPMENT.md` Part C/§D and `APPENDIX.md` §F, re-export
  four formats; (f) **F-11** — **DONE 2026-09-23**, see the F-11 ledger row + RS-12
  (helix_agent `4cbadfb4`) — `qualifiedIdentity()` now uses the D-1 `<source>.<dirname>` scheme;
  host-state rows were avoided structurally (both reports generated against the stable
  `constitution/skills/` dir only, never `.claude/skills/`) rather than filtered post-hoc; the real
  re-run still shows 3 surface asymmetries — **marking them optional on both sides remains an
  open operator §11.4.66 decision, NOT made here**; (g) **F-12** `CM-UNIQUE-MODULE-PATHS`:
  either a grandfathered-allowlist for `submodules/claude-toolkit/**` nested copies (mirroring
  RS-13's amendment) or §11.4.124 investigate-before-remove of the nested
  `helix_code/tests/e2e/orchestrator/tests/e2e/orchestrator/` copy; correct T-P0.03/progress.yml PASS
  claims; (h) **F-13** generate `helix_code/internal/commands/builtin_skills/` from
  `constitution/skills/` via the T-P1.08 converters (or retire the embed) so the source is
  authoritative; (i) **F-06 residue** capture `race_red.log` (lock-neutralised `-race`
  discrimination) and the `CM-SKILL-CAPABILITY-DECLARED` always-allow mutation log as tracked
  `.txt`; (j) **F-03 residue** regenerate `docs/extensions/EXTENSIONS_CATALOG.md` after
  `321dca40`; (k) **F-15** push the 21 unpublished submodule commits + the review commits to
  every upstream via §11.4.113 merge-onto-latest-main (never force) before T-P9.02's clean-target
  retest can be honest — **round-3 precondition: the constitution submodule is 11 commits BEHIND
  all 8 of its upstreams, so the merge-first step is mandatory, not optional**; (l) **F-17** —
  **PARTIALLY DONE 2026-09-23** (see the F-17 ledger row) — normalised the 3 nameless
  `constitution/skills/` entries (F-16 already covered this) and re-calibrated the
  description-similarity gate against the REAL corpus using T-P2.04's own rule
  (`SimilarityThreshold` `0.0256` → `0.0709`, corpus-2 excluded from the CONFUSABLE/DISTINCT
  derivation per `CorpusTwoPolicy`), then re-observed RS-01/RS-08 on the live tree with the real
  `helixcode` binary (evidence `docs/qa/hxc159_f17_remediation_20260923T0921Z/`) and regenerated the extensions
  catalogue from a real-binary probe (`docs/extensions/EXTENSIONS_CATALOG.md`/`catalog.json`). The
  redesign-vs-advisory-vs-content-edit decision was deliberately NOT made — the real corpus-1 tier
  is STILL policy-refused on a genuine confusable pair (`action-prefix-system` \|\|
  `reporting-workable-items`, 0.1045), and choosing among per-skill admission / advisory-with-audit
  / a content edit is an operator §11.4.66 decision, tracked as the F-17 residual, NOT a correctness
  patch this remediation was authorized to make unilaterally; (m) **F-19** promote the fence
  auditor to `scripts/gates/` and register it in the sweep with a paired golden-bad mutation
  (§11.4.135 — four fixes, zero permanent guards); (n) **F-20** correct the F-09 row (15/3/2) and
  commit `docs/qa/<run-id>/` records for the seven HXC-159 commits G7 names.

---

## Phase B1 — Bridge layer-health probe

**Gate**: none. Starts immediately, concurrent with P1.

### - [x] T-B1.01 Author the layer-presence probe

**Evidence**: `constitution/scripts/extensions/probe_bridge_layers.sh` +
`qa-results/hxc159/bridge_layers/20260922T185455Z/report.json`. Measured verdicts
(both systemd scopes enumerated): L1 WIRED, L2 WIRED, L3 **WIRED** (honest delta
from this doc's dated snapshot — see T-B1.03 evidence), L4 DESIGNED-ONLY, L5
NONEXISTENT, L6 WIRED, L7 PARTIAL.

**Scope**: probe all 7 layers; emit `WIRED / PARTIAL / DESIGNED-ONLY / NONEXISTENT` with
the captured command per layer.
**Test types**: unit, full-automation.
**Runtime signature**: **RS-18** — the report exists, every layer carries its raw probe
output, and **both** systemd scopes are enumerated.
**Acceptance evidence**: `qa-results/hxc159/bridge_layers/<run-id>/report.json`.
**Risks**: R-20, **R-30**.

- T-B1.01.1 One probe per layer; each records its exact command.
- T-B1.01.2 **Enumerate system *and* user systemd scopes.** A system-scope-only probe
  produced a *confident* false negative on L6 — all seven Helix units are user-scope.
  Encode the trap in the probe; do not rely on remembering it (§11.4.201 false-null class).
- T-B1.01.3 Record the four-value verdict, never a binary present/absent — the remedies
  differ per class and a binary flattens that distinction away.
- T-B1.01.4 Re-runnable and re-run at each phase start; layer state decays.

### - [x] T-B1.02 Gate `CM-BRIDGE-LAYER-PRESENCE`

**Evidence**: `constitution/scripts/gates/cm_bridge_layer_presence.sh` +
`constitution/scripts/gates/cm_bridge_layer_presence_mutation_test.sh` (4/4 cases
PASS, including the required L5-dependency-MUST-FAIL case) +
`qa-results/hxc159/bridge_gate/20260922T185723Z/evidence.log`.

**Scope**: any phase declaring a dependency on a non-`WIRED` layer fails loudly.
**Test types**: unit, full-automation.
**Runtime signature**: a phase declaring an L5 dependency FAILs the gate.
**Acceptance evidence**: `qa-results/hxc159/bridge_gate/<run-id>/`.
**Risks**: R-20, R-21.

- T-B1.02.1 Declare per-phase layer dependencies as data.
- T-B1.02.2 Cross-check against the live report.
- T-B1.02.3 Paired mutation: declare an L5 dependency → gate MUST FAIL.

### - [x] T-B1.03 [P] Supply-chain health record

**Evidence**: `constitution/scripts/extensions/bridge_supply_chain_check.sh`
(refusal-capable check, live-demonstrated) +
`qa-results/hxc159/bridge_supply_chain/20260922T190000Z/SUPPLY_CHAIN_HEALTH_RECORD.md`
+ `raw_evidence.log`. Re-verified 2026-09-22: T-B1.03.1 (obra/superpowers), T-B1.03.2
(SuperB — the "12 tagged releases / immutable .zip / only tests/-shipping bridge"
figures re-confirmed exactly, still 12 tags), and T-B1.03.4/.03.5 (erophames
PROHIBIT + speckit-community account facts) all CONFIRMED accurate and unchanged.
**T-B1.03.3's SuperSpec facts are STALE** — SuperSpec (WangX0111/superspec) is
now measured at 74 watchers/8 forks/pushed 2026-08-13 (not "0 watchers/last push
2026-06-02") and is **already installed** on this host as an immutable-commit-SHA-
pinned submodule with `specify extension list` showing it Enabled (not
DESIGNED-ONLY) — reclassified MEDIUM (was HIGH); see the health record for the
full reconciliation, including the naming collision where SuperSpec and SuperB
independently both self-brand as "Superpowers Bridge".

**Scope**: record the measured health of every Bridge dependency where an implementer
will encounter it — the installer's own logic, not a note in a document.
**Test types**: security.
**Runtime signature**: the installer **refuses** a prohibited or unpinned dependency at
install time.
**Acceptance evidence**: `qa-results/hxc159/bridge_supply_chain/<run-id>/`.
**Risks**: FR-049, FR-050.

- T-B1.03.1 `obra/superpowers` — official marketplace, already installed. LOW.
- T-B1.03.2 SuperB — 12 tagged releases, **immutable release-asset `.zip`**, the only
  bridge shipping `tests/`. MEDIUM: adoptable **pinned to the immutable asset**.
- T-B1.03.3 SuperSpec — **1 human, 10 lifetime commits, 0 watchers**, last push
  2026-06-02, **v1.0.0 broken**, installs by **mutable tag-archive URL with no checksum
  or signature** while dropping shell scripts an agent then executes. **HIGH — not
  adoptable as-is.**
- T-B1.03.4 🔴 `erophames/superpowers-mcp` — **PROHIBIT**. No LICENSE, dead ~4.5 months,
  and it `git pull`s `obra/superpowers` **daily via `execFile`, unsandboxed`: an
  unattended auto-update of remote instructions the agent then executes. Worse than
  having no L5.
- T-B1.03.5 Record that **catalogue listing is schema validation, not a code audit**
  (spec-kit's own words: maintainers *"do not review, audit, endorse, or support the
  extension code itself"*), and that `speckit-community` is **a personal user account
  styled as an organisation** (`/orgs/` 404s; `type: User`, 4 repos, 3 followers) whose
  own site disclaims GitHub affiliation. Citing either as authority is citing one person.

---

## Phase B2 — Author the L3/L4 installer

**Gate**: B1. **Scope is L3 + L4 only** — the absent-but-**obtainable** layers.

### - [x] T-B2.01 Create `constitution/scripts/extensions/`

**Evidence**: directory already existed from T-B1.01; now also holds
`install_speckit_superpowers_bridge.sh` + `install_speckit_superpowers_bridge_test.sh`
+ `validate_bridge.sh`, all `bash -n` clean, executable, inline-doc-headered per the
same convention T-B1.01's scripts use. Constitution submodule commit `b43a2a8`.

**Scope**: the directory the Bridge design names and which does not exist.
**Test types**: unit.
**Runtime signature**: the directory exists with executable, syntax-valid scripts
(`bash -n`).
**Acceptance evidence**: no `bridge_installer/` directory; covered by the 34 `qa-results/hxc159/bridge_install/*` runs.
**Risks**: R-20.

- T-B2.01.1 Create the directory in the **constitution** submodule (inherited by
  reference, never copied — a copy diverges silently).
- T-B2.01.2 Follow the §11.4.18 script-documentation mandate.

### - [x] T-B2.02 Author `install_speckit_superpowers_bridge.sh`

**Evidence**: `constitution/scripts/extensions/install_speckit_superpowers_bridge.sh`
(constitution commit `b43a2a8`) + `qa-results/hxc159/bridge_install/<run-id>/`.
L3 (SuperSpec) idempotent detect-and-validate-pin (already WIRED on this host);
L4 (SuperB) refused by default (HIGH risk, needs an explicit `--superb-from` +
`--superb-vetted-by` §11.4.66 record); no L5 step; `erophames/superpowers-mcp`
structurally unreachable; `obra/superpowers` reconciled, never double-installed.
RS-19 confirmed live: `specify extension list` non-empty after every run.
Paired test suite `install_speckit_superpowers_bridge_test.sh` 8/8 PASS (4 unit +
1 integration against the real repo root + 1 full-automation idempotent-double-run
+ 2 chaos: SIGKILL-interrupted L4 install leaves detectable partial state, re-run
cleans it and completes). Two independent real SIGKILL interruptions of the
script's actual default (read-mostly) code path on this host also captured
(`qa-results/hxc159/bridge_install_t_b202_manual_evidence/chaos_*`), confirming
byte-identical repo/`.specify/extensions.yml` state before/after.

**Scope**: install L3 and L4 **only**. Idempotent over an already-initialised L2.
**Test types**: unit, integration, full-automation, chaos (interrupted install).
**Runtime signature**: **RS-19** — after running, `specify extension list` is non-empty
and names the pinned version.
**Acceptance evidence**: `qa-results/hxc159/bridge_install/<run-id>/`.
**Risks**: R-20, FR-045, FR-048.

- T-B2.02.1 **Use an explicit `--from` release URL.** The README's bare
  `specify extension add superspec` **FAILS on a default install** — spec-kit's
  first-party `catalog.json` holds only 4 entries, all `spec-kit-core`, and neither
  bridge is among them; they live in the *community* catalog.
- T-B2.02.2 Idempotent over the existing `.specify/` and the ten `speckit-*` skills —
  **do not** re-initialise L2 and do not clobber it.
- T-B2.02.3 Reconcile with the plugin-sourced Superpowers copy rather than double-installing.
- T-B2.02.4 Refuse SuperSpec unless an org mirror + immutable pin is configured (T-B1.03.3).
- T-B2.02.5 Refuse `erophames/superpowers-mcp` unconditionally (T-B1.03.4).
- T-B2.02.6 Contain **no** L5 step — a step that can never succeed is a defect, not a TODO.

### - [x] T-B2.03 Author `validate_bridge.sh`

**Evidence**: `constitution/scripts/extensions/validate_bridge.sh` (constitution
commit `b43a2a8`) + `qa-results/hxc159/bridge_validate/<run-id>/`. Re-derived the
8-check list verbatim from `implementation/README.md` "Step 4" (the corpus's own
`ALL 8/8 CHECKS PASSED` transcript). **Correction to this task's own stale
assumption**: on the FRESH measurement checks 5 (superspec status) and 7 (Helix
LLM Gateway reachable) now PASS — not fail — because L3 and L6 were independently
re-measured WIRED by T-B1.01's fresh probe (spec.md's original snapshot predates
that). Check 1 (SuperBridge MCP health) dropped to `N/A` per T-B2.03.2 (L5 is a
proven structural impossibility, §11.4.112 — never reported FAIL). Checks 6
(superb) and 8 (llama.cpp RPC) correctly FAIL (L4 not installed pending vetting;
L7 rpc-server not yet built, tracked separately). Real result on this host:
5/7 evaluable checks PASS, bridge NOT fully ready — reported honestly, never 8/8.
Paired mutation (T-B2.03.3) run live: injecting a synthetic report claiming
L4=WIRED does NOT flip check 6 to PASS, because check 6 requires genuine
installed-state evidence from `.specify/extensions.yml`'s `installed:` list
rather than trusting `specify extension info <id>` alone (which resolves for any
catalog-known id regardless of install state — a false-positive trap discovered
and fixed during this task via exactly this mutation-testing discipline).

**Scope**: the design advertises *"ALL 8/8 CHECKS PASSED — bridge ready"*. On this host
it would fail at least checks 1, 5, 6, 7 and 8. The validator must report honestly.
**Test types**: unit, full-automation.
**Runtime signature**: the validator reports per-check pass/fail against the **measured**
layer report, and never claims 8/8 while a layer is absent.
**Acceptance evidence**: `qa-results/hxc159/bridge_validate/<run-id>/`.
**Risks**: R-20, §11.4.

- T-B2.03.1 Re-derive the check list from the design corpus.
- T-B2.03.2 Drop or re-point any check that depends on L5.
- T-B2.03.3 Paired mutation: uninstall L4 → the validator MUST report it, not pass.

### - [x] T-B2.04 [P] Read the six unread Bridge documents

**Evidence**: `qa-results/hxc159/bridge_docs_delta/20260922T200326Z/delta.md`
(full heading-tree survey of all six + targeted reads of every load-bearing
section + an explicit corpus-wide grep for `install_speckit_superpowers_bridge`
and `validate_bridge.sh` returning zero hits). **Headline finding: all six
documents describe a FUTURE, aspirational "nano-task engine" + a custom
`helix-nano-bridge` extension + an unlanded `§11.4.NNB` constitution-anchor
proposal — none of it names or constrains the two scripts T-B2.02/T-B2.03
actually produced, and nothing invalidates that work.** Two notable findings
recorded for B3 (non-blocking): (1) the proposed `§11.4.NNB` anchor's stage-6
pipeline text still assumes an L5 execution tier this project has proven does
not exist; (2) SECURITY.md §4.3 carries an unremoved cross-project
("ATMOSphere project mandate") reference that does not belong to HelixCode.
Full delta list (MUST-INCORPORATE-NOW: none; NOTE-FOR-LATER: 4 items) in the
evidence file.

**Scope**: NANO_TASK_ENGINE, EXTENSION_DEVELOPMENT, TDD_INTEGRATION,
CONSTITUTION_INTEGRATION, SECURITY, APPENDIX — none read during Phase 1.
**Test types**: none (investigation).
**Runtime signature**: a written delta listing every obligation stated **only** in those
six and not reflected in FR-044…FR-050.
**Acceptance evidence**: `qa-results/hxc159/bridge_docs_delta/<run-id>/`.
**Risks**: U-7. **Must complete before T-B2.02 is considered final.**

---

## Phase B3 — Delete L5, record the 6-layer architecture

**Gate**: B1. Independent of B2 — a design correction, not an installation.

### - [x] T-B3.01 Record the L5 non-existence finding upstream

**Evidence**: `qa-results/hxc159/l5_finding/20260922T210000Z/FINDING.md` +
`derivations.txt`. Both derivations independently RE-verified (not merely trusted
from B1's summary): GitHub authenticated code search `total_count: 17`, all 17 hits
traced to this project's own `HelixDevelopment/code` self-references, zero
third-party hits; npm fuzzy search 95,472 results with no exact-name match +
direct `GET /superbridge-mcp` → HTTP 404. Both derivations also recorded upstream in
the design corpus itself (README.md Architecture Amendment section + amendment
banners on the other 7 documents).

**Scope**: SuperBridge MCP does not exist as software. Independently re-derived twice
(`total_count: 0` on GitHub search; zero npm MCP packages) — **two independent
derivations of the same negative**, the strongest-supported result in the set.
**Test types**: none (investigation record).
**Runtime signature**: the finding is recorded in the Bridge design corpus with both
derivations cited.
**Acceptance evidence**: `qa-results/hxc159/l5_finding/<run-id>/`.
**Risks**: R-21.

- T-B3.01.1 Capture both derivations with their commands.
- T-B3.01.2 Record that L4's own README states it *"owns no plan, task store, execution
  lifecycle, completion state, or convergence command"* — so no adjacent layer ever
  claimed the execution role either.

### - [x] T-B3.02 Design the substitution explicitly

**Evidence**: `docs/qa/hxc159_b3_speckit_demo_20260922T211500Z/` — full bidirectional
transcript of a real `/speckit-specify → /speckit-plan → /speckit-tasks →
/speckit-implement` run in an isolated git worktree, including a genuine dispatched
Claude Code subagent (via the Agent tool) writing `hello_bridge_marker.txt`, zero MCP
calls, independently re-verified by the orchestrating session after the subagent's
own report. Substitution mapping table in `04_implement_subagent_transcript.md` +
README.md's Architecture Amendment section. Incidental confirmation: `setup-tasks.sh`
resolved its template THROUGH `.specify/extensions/superspec/`, independently
confirming L3 is wired.

**Scope**: the execution tier is **Spec Kit + Superpowers + Claude Code's own
skill/subagent machinery — already WIRED in this repository**. Design it as the
replacement rather than leaving a hole.
**Test types**: integration, e2e, full-automation.
**Runtime signature**: a worked end-to-end run —
`/speckit-specify → /speckit-plan → /speckit-tasks → /speckit-implement` driving real
subagent execution — captured as a transcript.
**Acceptance evidence**: `docs/qa/<run-id>/` (§11.4.83 bidirectional transcript).
**Risks**: R-21, operator requirement.

- T-B3.02.1 Map each claimed L5 responsibility onto the component that actually owns it.
- T-B3.02.2 Name any responsibility with **no** owner — an honest gap beats a phantom tier.
- T-B3.02.3 Prove the substitution by running the flow, not by asserting it.

### - [x] T-B3.03 Redraw the architecture as 6 layers upstream

**Evidence**: `qa-results/hxc159/bridge_redraw/20260922T213000Z/RS20_EVIDENCE.md` +
`export.log` + `pdf_validation.log` + `rs20_check.log`. All 8 documents amended with
a dated Architecture Amendment banner; README.md fully redrawn (TOC, ASCII diagram,
layer headings/renumbering, Key Design Decisions, Directory Structure, Quick Start
install/validate output corrected to match the real landed scripts, Data Flow,
§11.4.76 row). The two B2-flagged specific fixes landed: CONSTITUTION_INTEGRATION.md
§2's unlanded `§11.4.NNB` proposal corrected + marked UNLANDED; SECURITY.md §4.1/§4.3
genericised (ATMOSphere reference + hardcoded path removed). All 32 export files
(8 × 4 formats) regenerated via pandoc+weasyprint and validated per §11.4.168
(content + textual + visual). One PRE-EXISTING, out-of-scope defect honestly
recorded (nested Mermaid fence in `EXTENSION_DEVELOPMENT.md`, byte-identical to
pre-Phase-B3 HEAD, unrelated to the L5 finding) — flagged for operator follow-up,
not silently fixed or hidden.

**Scope**: amend the Bridge design corpus (8 documents × 4 formats). *A 7-layer diagram
with a phantom middle tier is a 6-layer architecture drawn wrong.*
**Test types**: full-automation (export validation).
**Runtime signature**: **RS-20** — no artefact references a SuperBridge MCP endpoint, and
the design records 6 layers. **T-P9.01 amendment:** the first clause is NOT literally met — `EXTENSION_DEVELOPMENT.md` Part C/§D (`superbridge-mcp` `.mcp.json` blocks, `SUPERBRIDGE_MCP_PORT=9595`) and `APPENDIX.md` §F still carry endpoint text as banner-marked retired design; plan.md §9 RS-20 was amended by the review to the delivered form (banner-amended + 6 live layers), and retiring those blocks is a T-P9.03 item.
**Acceptance evidence**: `qa-results/hxc159/bridge_redraw/<run-id>/`.
**Risks**: R-21.

- T-B3.03.1 Amend all 8 documents consistently; re-export four formats each.
- T-B3.03.2 Update the architecture diagram to 6 layers.
- T-B3.03.3 Record the amendment as a visible change with its reason (§11.4.35 — no
  silent promotion or demotion).
- T-B3.03.4 Validate the exports per §11.4.168.

---

## Dependency graph

```
P0 ──┬── P1 ──┬── P2 ──┐
     │        │        ├── P4 ── P5 ──┐
     ├── P3 ──┴────────┘        P6 ──┴── P7 ── P8 ── P9
     │                                       ↑
     └── B1 ──┬── B2 ─────────────────────────┘
              └── B3 ─────────────────────────┘
```

**Blocking edges that are non-obvious and easy to violate:**

- **P2 → P4.04**: no activation policy may exist before the curve. The single most
  important ordering constraint in the plan.
- **P1.01/P1.02 → P4**: the loader is fail-closed, so it will error immediately against
  today's un-normalised corpus. Normalise first or spend the phase debugging a known defect.
- **P3.01.5 → P1.08.5**: the TOML converter cannot round-trip until the upstream
  cross-skill edge-write lands.
- **P0.02 → P6**: a competing abstraction in HelixAgent's unsurveyed trees would change
  the attachment point.
- **B1 → everything in Track B**: no layer dependency may be declared before the real
  layer state is measured.
- **P7.05.5 → any third-party activation**: the upstream sandbox default is a *skip*, so
  third-party-tier activation is blocked until a real executor exists.

---

## Parallel-execution guidance

Per §11.4.103 / §11.4.183, at least three background streams run concurrently while
actionable non-contending work remains. Natural disjoint scopes:

| Stream | Phases | File scope (disjoint per §11.4.58) |
|---|---|---|
| **1 — Track A source** | P0 → P1 → P4 | `constitution/skills/`, `scripts/`, upstream `pkg/` |
| **2 — Track A upstream** | P3 | the `skills` repository |
| **3 — Track B** | B1 → B2 → B3 | `constitution/scripts/extensions/`, the Bridge design corpus |
| **4 — Benchmark** | P2 | `qa-results/hxc159/shadowing_curve/` |
| **5 — Consumers** | P5 ∥ P6 | `helix_code/` and `submodules/helix_agent/` — genuinely disjoint |

**Contention warning**: P1 and P5 both touch `.claude/skills/` provisioning. Serialise
those two on that resource (§11.4.119 single-resource-owner), even though their phases
are otherwise independent.

---

## Honest boundary

*(As written at planning time, 2026-09-02: nothing had been implemented and only `T-P1.06` was ticked.)* **Status 2026-09-23:** every task P0–P8 / B1–B3 is ticked with an evidence pointer; the T-P9.01 review (below) re-verified those pointers, found several that named evidence never written, fixed what was in scope, and recorded the rest — the honest boundary is now the T-P9.01 finding ledger, not this paragraph.

Per §11.4.108, no task is "done" when its code is written — only when its runtime
signature verifies on a clean target. Per §11.4.115, no fix is credible unless its RED
test first reproduced the defect on the **pre-fix** artifact. Per §11.4.123, no closure
is credible without captured physical evidence.

**Estimates are deliberately absent.** No task carries a duration, because none was
measured and inventing them would be the §11.4.6 guess this whole plan exists to avoid.
P2's cost in particular is genuinely unknown until its task set exists.
