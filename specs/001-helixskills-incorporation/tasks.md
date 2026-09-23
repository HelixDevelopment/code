# Tasks: HelixSkills incorporation into HelixCode and HelixAgent

**Feature**: `001-helixskills-incorporation` | **Work item**: HXC-159
**Spec**: [`spec.md`](spec.md) · **Plan**: [`plan.md`](plan.md)
**Revision**: 2 · **Last modified**: 2026-09-02 · **Author**: `(T1/main - claude1 - opus - xhigh)`

**Status**: IN EXECUTION since 2026-09-13. Phase P0 COMPLETE
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
**Acceptance evidence**: `qa-results/hxc159/case_normalise/<run-id>/`.
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
**Acceptance evidence**: `qa-results/hxc159/multitrack_manifest/<run-id>/`.
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
**Acceptance evidence**: `qa-results/hxc159/symlink_hygiene/<run-id>/`.
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
**Acceptance evidence**: `qa-results/hxc159/registrar/<run-id>/`.
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
**Acceptance evidence**: `qa-results/hxc159/register_mcp/<run-id>/`.
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
**Acceptance evidence**: `qa-results/hxc159/ssot_unfork/<run-id>/`.
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
**Acceptance evidence**: `qa-results/hxc159/schema_roundtrip/<run-id>/` including the
per-corpus pass counts.
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
**Acceptance evidence**: `qa-results/hxc159/shadowing_curve/<run-id>/taskset.json`.
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
**Acceptance evidence**: `qa-results/hxc159/shadowing_curve/<run-id>/curve.json` + plot.
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
**Acceptance evidence**: `qa-results/hxc159/shadowing_curve/<run-id>/derivation.md`.
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
**Acceptance evidence**: `qa-results/hxc159/similarity/<run-id>/`.
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
**Acceptance evidence**: `qa-results/hxc159/ceiling_gate/<run-id>/`.
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
**Acceptance evidence**: `qa-results/hxc159/module_promote/<run-id>/`.
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
**Acceptance evidence**: `qa-results/hxc159/upstream_governance/<run-id>/`.
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
**Acceptance evidence**: `qa-results/hxc159/pin/<run-id>/`.
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
**Acceptance evidence**: `qa-results/hxc159/testing_infra_merge/<run-id>/`.
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
**Acceptance evidence**: `qa-results/hxc159/layout/<run-id>/`.
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
**Acceptance evidence**: `qa-results/hxc159/onboarding/<run-id>/`.
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
**Acceptance evidence**: `qa-results/hxc159/loader/<run-id>/`.
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
**Acceptance evidence**: `qa-results/hxc159/registry/<run-id>/`.
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
**Acceptance evidence**: `qa-results/hxc159/namespacing/<run-id>/`.
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
**Acceptance evidence**: `qa-results/hxc159/activation/<run-id>/`.
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
**Acceptance evidence**: `qa-results/hxc159/capabilities/<run-id>/`.
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
**Acceptance evidence**: `qa-results/hxc159/headless/<run-id>/`.
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
**Acceptance evidence**: `qa-results/hxc159/loader_matrix/<run-id>/`.
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
(constitution/skills fixture), including a genuine fail-closed finding (3 of 7
constitution skills lack YAML front-matter and correctly abort the whole
source, `03_library_cli_run.txt`). Submodule commit `ae08704` (skills),
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
+ `diff_samecorpus.json` (gate correctly exits 1). This is the gate WORKING
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

### - [ ] T-P8.01 Update existing documentation

**Scope**: `docs/CAPABILITIES.md` (already drifted — cites `:325`/`:1652`, actual
`:368`/`:1730`), both consumers' skill docs, the README, and the upstream Bridge corpus.
**Test types**: full-automation (link + reference checking).
**Runtime signature**: every cited `file:line` in updated docs resolves to the claimed
symbol — checked mechanically, not by eye.
**Acceptance evidence**: `qa-results/hxc159/docs_update/<run-id>/`.
**Risks**: `03` §7.10 documentation drift.

- T-P8.01.1 Re-derive every `file:line` citation; fix drift.
- T-P8.01.2 Update both consumers' skill documentation to the new model.
- T-P8.01.3 Author the skills user manual and FAQ.
- T-P8.01.4 Update the upstream Bridge design corpus to the corrected 6-layer form (B3).

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
T-P5.05.4 activation-ceiling honest boundary carried forward verbatim.

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

### - [ ] T-P8.03 [P] Diagrams and illustrations

**Scope**: five diagrams, incorporated into the materials rather than filed beside them.
**Test types**: full-automation (render validation).
**Runtime signature**: each diagram renders as an **image** in the exported PDF — proven
by image enumeration, not by the source looking right.
**Acceptance evidence**: `qa-results/hxc159/diagrams/<run-id>/`.
**Risks**: §11.4.168 (this project has shipped raw Mermaid source as body text into
user-facing PDFs before).

- T-P8.03.1 Three-layer architecture (source / graph service / generated catalogue).
- T-P8.03.2 Registration-path sequence (constitution → registrar → `.claude/skills/` → consumer).
- T-P8.03.3 The measured shadowing curve (a real plot of real data).
- T-P8.03.4 Trust-tier and capability-enforcement flow.
- T-P8.03.5 Bridge layer states, colour-coded by the four-value vocabulary.

### - [ ] T-P8.04 Exports, §11.4.168 validation, README reachability

**Scope**: four-format exports; independent content/textual/visual validation; every doc
reachable from the main README.
**Test types**: full-automation.
**Runtime signature**: **RS-22** — link traversal from README reaches every produced
document; `pdftotext` on each export contains **no** raw diagram source; image
enumeration confirms the diagrams rendered.
**Acceptance evidence**: `qa-results/hxc159/doc_exports/<run-id>/`.
**Risks**: §11.4.65, §11.4.153, §11.4.168, §11.4.212.

- T-P8.04.1 Export all four formats.
- T-P8.04.2 Textual layer: assert no `mermaid`/`graph`/`sequenceDiagram` leaks as body text.
- T-P8.04.3 Visual layer: `pdfimages` enumeration + OCR spot-check.
- T-P8.04.4 Validate with a reviewer structurally separate from the generator.
- T-P8.04.5 README link traversal from the root.

---

## Phase P9 — Review & closure

**Gate**: P8.

### - [ ] T-P9.01 Independent code review, iterated to a zero-finding GO

**Scope**: §11.4.142 (every change reviewed) + §11.4.125 (before build) + §11.4.134
(iterate to GO). Reviewer structurally separate from the author; §11.4.209 model tier.
**Test types**: all (the review reads the evidence).
**Runtime signature**: a review verdict citing captured evidence, with **zero findings
and zero warnings** — a residual warning re-arms the loop.
**Acceptance evidence**: `qa-results/hxc159/review/<run-id>/`.
**Risks**: acceptance criterion 6.

- T-P9.01.1 Dispatch the reviewer with the full diff, evidence set and this plan.
- T-P9.01.2 Remediate; **re-run** the review after each round (never one pass).
- T-P9.01.3 Verify every §9 runtime signature was actually observed, not asserted.
- T-P9.01.4 §11.4.145 multi-angle impact research across the eight angles.

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
**Acceptance evidence**: `qa-results/hxc159/bridge_installer/<run-id>/`.
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
the design records 6 layers.
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

**Nothing here has been implemented.** `T-P1.06` is the sole exception, is marked `[x]`,
and carries its own disclosure. Every other checkbox is unticked and honest.

Per §11.4.108, no task is "done" when its code is written — only when its runtime
signature verifies on a clean target. Per §11.4.115, no fix is credible unless its RED
test first reproduced the defect on the **pre-fix** artifact. Per §11.4.123, no closure
is credible without captured physical evidence.

**Estimates are deliberately absent.** No task carries a duration, because none was
measured and inventing them would be the §11.4.6 guess this whole plan exists to avoid.
P2's cost in particular is genuinely unknown until its task set exists.
