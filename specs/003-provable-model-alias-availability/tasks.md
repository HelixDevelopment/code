---
description: "Task list for Provable Model Alias Availability"
---

# Tasks: Provable Model Alias Availability

**Plan**: [plan.md](./plan.md) | **Spec**: [spec.md](./spec.md) | **Research**: [research.md](./research.md)
**Branch**: `003-provable-model-alias-availability` | **Date**: 2026-09-08

> **For agentic workers:** REQUIRED SUB-SKILL — use `superpowers:subagent-driven-development`
> (recommended) or `superpowers:executing-plans` to implement this task-by-task. Steps use checkbox
> syntax for tracking. Decomposition follows the `writing-plans` methodology, adapted to the
> superspec template per `.specify/extensions/superspec/references/superpowers-bridge.md`.

**Goal**: every alias the toolkit presents is genuinely usable, and the claim is checkable.

**Architecture**: four independent streams over three unmerged repositories. Every capability claim
is backed by a REPLAYABLE artifact rather than a live sample, because the one backend involved is
measurably non-deterministic and the operator's mandate is fully deterministic validation.

**Tech Stack**: Bash 5 + `jq` + `curl` (toolkit), Go 1.26 (`helix_code`), Python 3.11 (resolver),
SQLite (workable items). No application framework.

## Task Format

```
[ID] [markers] [Story] Description
```

**Markers**: `[P]` parallelisable · `[TDD]` RED-GREEN-REFACTOR · `[REVIEW]` review before proceeding · `[SUBAGENT]` delegable

## Path Conventions

Three repositories, deliberately not merged. No task spans two of them in one commit.

- `TK/` = `~/Projects/claude_toolkit/` — sibling repo, own 4 upstreams
- `HC/` = `helix_code/` — the Go application
- `CN/` = `constitution/` — consumed by reference, never copied (§11.4.177)

## Global Constraints

Project-wide requirements. Every task's requirements implicitly include this section. Values are
copied verbatim from the spec, the plan and this session's measurements — not restated from memory.

- **No credential value** in any listing, verdict, log, evidence file, proof artifact or diagnostic
  (FR-019). Compare by hash. The one secret-bearing artifact is the per-twin `config.toml`, written
  `0600` inside a `0700` directory via umask + `mktemp` + atomic rename — never a post-hoc `chmod`.
- **Seven-column order is the contract**: `ALIAS PROVIDER STATUS CHECKED LAYER STRONG_MODEL`, with
  `AGENT` prepended in the Kimi view. It is parsed positionally by `TK/scripts/kimi-providers.sh`.
- **Exit codes**: `0` ran (including "found nothing"), `1` ran and failed, `3` engine unreachable.
  "Found nothing" and "could not look" MUST be distinguishable without parsing output.
- **Only `verified` is "presented as available"** (Q1). `failed`, `orphaned`, `no-twin` are shown
  with state and reason and never counted toward FR-001 or SC-001.
- **Listing performs no network I/O.** Re-verification is `verify`, and only `verify`.
- **Verdict horizon**: `CMA_STATUS_TTL`, inheriting `CMA_MODELS_DEV_TTL`, default `86400`.
- **Performance**: alias listing under 1 second at current scale. Baseline 2.83s at 40 rows.
- **Test hermeticity**: suites run under `env -u KIMI_API_KEY -u HELIXLLM_GATEWAY_KEY` — the
  operator's shell exports both as live values, so an unscrubbed run is not hermetic.
- **Mutation env knob differs per suite**: `test_alias_file_concurrency.sh` reads
  `CMA_SCRIPTS_UNDER_TEST`; the others read `CMA_TEST_SCRIPTS_DIR`. The wrong knob silently tests
  the REAL tree and returns a meaningless green.
- **No task spans two repositories in one commit.**
- **Every census carries a positive and a negative control** (§11.4.273); the positive control must
  lie inside the instrument's declared scope and must not be the thing being measured.

## File Structure

Mapped before task decomposition, because this is where the boundaries get locked in.

| File | Responsibility | Tasks |
|---|---|---|
| `TK/scripts/lib.sh` | verdict age + staleness helpers, alias/config writers, launch wrapper | T006, T012, T021, T027 |
| `TK/scripts/claude-providers.sh` | the engine — resolve, verify, list, export, orphan detection | T005, T007, T012, T015 |
| `TK/scripts/kimi-providers.sh` | the Kimi-framed view; the positional consumer of the engine table | T007, T008 |
| `TK/scripts/providers_resolve.py` | catalogue match + pins → resolved records | T013, T020 |
| `TK/scripts/providers/overrides.json` | manual pins; T013 adds generated ones, distinguishably | T013 |
| `TK/scripts/tests/` | 69 auto-discovered suites; every RED test lands here | T008–T011, T017, T018, T022, T025, T028 |
| `HC/testdata/toolcall_wire_corpus/` | recorded real wire bytes, sha256-pinned — the replay source | T019 |
| `CN/scripts/mutation/`, `census/`, `wait/` | the §11.4.274 extracted tools, inherited by reference | T001–T004 |
| `CN/scripts/hooks/` | the six wired PreToolUse guards | T024 |
| `CN/scripts/codegraph_validate.sh` | index validation; hardcoded exclusions to generalise | T029 |

**Single-owner note (§11.4.119)**: `TK/scripts/lib.sh` appears in four tasks across three phases.
Those four are explicitly NOT parallel with one another, and the Parallel Opportunities section says
so. This table exists so that constraint is visible before someone dispatches them concurrently.

## Phase 1: Setup (Shared Infrastructure)

- [ ] T001 [P] Land the §11.4.274 mutation harness at `CN/scripts/mutation/` with docs, tests and control needles; its acceptance test is reproducing a measured outcome from this session (toolkit D5e must report FAIL, unmutated must report 58/0), not a synthetic one
- [ ] T002 [P] Land the census helper at `CN/scripts/census/` enforcing §11.4.273 — every query takes a positive and a negative needle and REFUSES to emit a result if the positive is not found
- [ ] T003 [P] Land the bounded completion waiter at `CN/scripts/wait/` reporting which outcome it exited on, so a timeout can never read as success
- [ ] T004 Wire T001–T003 into `TK/scripts/tests/run-all.sh` discovery and document them in `CN/docs/` per §11.4.18

## Phase 2: Foundational (Blocking Prerequisites)

**These block every user story. T005 blocks US1 specifically and absolutely.**

- [ ] T005 [REVIEW] **DIAGNOSIS GATE.** Determine why 20 providers report `orphaned`. Four causes are already eliminated by measurement (research.md); test the surviving hypothesis that `list-all` computes orphan status from a cached rather than a live resolution, by instrumenting `cma_find_orphans` in `TK/scripts/claude-providers.sh` to record the resolved set it compares against. **No US1 repair task may start until this returns a cause.** Planning a repair on an unknown cause is the §11.4.102 violation this gate exists to prevent
- [ ] T006 [P] [TDD] Add the evidence-path schema to `TK/scripts/lib.sh` so a PASS verdict without a non-empty evidence path is impossible to record (FR-008)
- [ ] T007 [P] [TDD] Implement the three-state exit-code contract across `TK/scripts/claude-providers.sh` and `TK/scripts/kimi-providers.sh` per `contracts/cli-surfaces.md` — `0` ran, `1` failed, `3` engine unreachable (FR-009). RED must reproduce a "found nothing" indistinguishable from "could not look"
- [ ] T008 [REVIEW] Add a contract guard asserting the seven-column order in `TK/scripts/tests/` — the column order is the contract, and this session shipped a defect by inserting a column without updating its positional consumer

## Phase 3: User Story 1 — Everything listed is usable (Priority: P1) MVP

**Goal**: every alias the toolkit presents completes a round trip. Baseline 8 of 27 (30%) → 100%.
**Independent test**: enumerate presented aliases, attempt a minimal round trip through each, compare the sets.
**BLOCKED BY T005.**

### Tests for User Story 1

- [ ] T009 [P] [TDD] [US1] RED: reproduce the alias/config half-wired state (19 of 27) in `TK/scripts/tests/`, asserting an alias exists whose `config.toml` does not (FR-002)
- [ ] T010 [P] [TDD] [US1] RED: reproduce an auto-generated pin that cannot be verified being presented as available rather than as a named error (FR-024)
- [ ] T011 [P] [TDD] [US1] RED: reproduce a non-`verified` alias being counted toward "presented as available" (FR-001, Q1)

### Implementation for User Story 1

- [ ] T012 [US1] Enforce the alias/config pairing invariant in `TK/scripts/claude-providers.sh` across every path that creates, restores or refreshes them (FR-002)
- [ ] T013 [US1] Implement pin generation for records sharing a credential in `TK/scripts/providers/overrides.json` + `TK/scripts/providers_resolve.py`; generated pins MUST be distinguishable from operator-authored ones (FR-024)
- [ ] T014 [REVIEW] [US1] Apply the T005 cause to the remaining orphan class. Shape unknown until T005 returns — this task is a placeholder for a repair that MUST NOT be designed before the diagnosis
- [ ] T015 [US1] Restrict "presented as available" to `verified` in `TK/scripts/claude-providers.sh` list rendering; other states show state + reason (FR-001)
- [ ] T016 [REVIEW] [US1] `huggingface`: its key variable is absent from the catalogue entirely. If un-repairable, surface as an operator decision per FR-020 — never withdraw it unilaterally (§11.4.122) (FR-018)

## Phase 4: User Story 2 — A verdict that means the same thing twice (Priority: P1)

**Goal**: repeated validation against unchanged state yields identical verdicts, each stating its age.
**Independent test**: run validation repeatedly, compare byte-for-byte; confirm each verdict states its age.

- [ ] T037 [P] [TDD] [US2] RED: reproduce a model judged READY on the SHAPE of its reply rather than its CORRECTNESS (FR-021) — the RED fixture is a reply that matches the expected form while being obtainable WITHOUT processing the request; it MUST be judged not-ready. Covers analyze finding E1.
- [ ] T038 [US2] Replace form-matching with a correctness oracle in the readiness harness (FR-021). The two measured not-READY causes are distinct and MUST be separated: a small model answering procedurally is a CORRECTNESS failure; per-call usage variance on a route whose usage is a sum over a variable number of internal calls is NOT — conflating them makes a working model look broken.
- [ ] T017 [P] [TDD] [US2] RED: reproduce a verdict that varies between runs against unchanged state (FR-006)
- [ ] T018 [P] [TDD] [US2] RED: reproduce stale data being served without announcement (FR-023)
- [ ] T019 [US2] Make the replayed-wire corpus the DEFAULT verdict path in `HC/testdata/toolcall_wire_corpus/`; the live probe becomes an opt-in distribution check (FR-007)
- [ ] T020 [US2] Implement announced stale-catalogue fallback in `TK/scripts/providers_resolve.py` — auto-refresh on expiry, fall back to stale, and carry the cache age into every derived result (FR-023)
- [ ] T021 [P] [US2] Bring alias listing under 1 second at current scale (SC-011) — measured 2.83s at 40 rows; hoist the per-row `jq` out of the loop in `TK/scripts/lib.sh`

## Phase 5: User Story 3 — Honest behaviour without the governance corpus (Priority: P2)

**Goal**: every entry point works or refuses loudly, naming what is unavailable.
**Independent test**: run each entry point from a directory with no corpus present.

- [ ] T022 [P] [TDD] [US3] RED: reproduce a silent no-op where the corpus is absent (FR-010)
- [ ] T023 [P] [US3] Audit every `CN/scripts/` entry point for the mode-A/B/C degradation pattern already modelled by `skill-activate`; each must name what is unavailable, what still works, and how to fix it
- [ ] T024 [P] [US3] Extend the §11.4.109 guard test suite in `CN/scripts/hooks/` to cover corpus-absent invocation for all six wired guards

## Phase 6: User Story 4 — Only what is needed is loaded (Priority: P2)

**Goal**: minimum active capability surface; everything else discoverable and one command away.
**Independent test**: measure active count and governance volume in a fresh session; restore any inactive capability in one command.

- [ ] T025 [P] [TDD] [US4] RED: reproduce an explicit opt-out being silently undone by a routine refresh (FR-012)
- [ ] T026 [P] [US4] Put `~/.claude-shared/bin` on PATH via the documented shell-rc mechanism so `skill-activate` is reachable without a full path (FR-013) (FR-011)
- [ ] T027 [US4] Correct the activation tool's "live in the running session now" message — hot-load is eventually-consistent across a tool round, and two "Unknown skill" errors currently read as activation failure (§11.4.201: a message must assert the real condition)

## Phase 7: User Story 5 — Navigation that reflects the real codebase (Priority: P3)

**Goal**: owned-submodule symbols resolve; vendored third-party code excluded.
**Independent test**: query a symbol existing only inside an owned submodule; query vendored code.

- [ ] T028 [P] [TDD] [US5] RED: reproduce an empty or stale index returning "no matches" rather than reporting its emptiness (FR-015)
- [ ] T029 [P] [SUBAGENT] [US5] Generalise the hardcoded third-party exclusion list in `CN/scripts/codegraph_validate.sh` to read from configuration rather than four inline patterns (FR-014)
- [ ] T030 [P] [SUBAGENT] [US5] Add an index-freshness probe that distinguishes "index empty" from "no matches" at the query surface

## Requirement traceability (added 2026-09-08, analyze remediation)

Every FR in `spec.md` must reach a task, be recorded as already shipped, or be
named here as a known gap. A requirement that is silently uncited is how E1/E2
went unnoticed: the analyze pass reported 2 zero-coverage FRs, but an explicit
`comm` of declared-vs-cited ids showed **10** uncited, of which 2 were genuine
gaps, 3 were already shipped, 3 were covered by task text that simply never
named the id, and 2 were partial. The ids are now cited on their covering
tasks so the same `comm` is a mechanical check rather than a judgement call.

| FR | Disposition | Where |
|---|---|---|
| FR-021 | GAP → now covered | T037 (RED), T038 (correctness oracle) |
| FR-022 | GAP → now covered | T039 (document), T040 (executable proof) |
| FR-003 | Already shipped — wire selected from the declared `transport` field, carried into the provider record, not inferred from URL shape | verify at T034 |
| FR-004 | Already shipped — `cma_status_age_human` present in 3 files | verify at T034 |
| FR-005 | Already shipped — `cma_status_is_stale` present in 3 files, so unknown age is distinguishable from recent | verify at T034 |
| FR-011 | Covered by description | T025, T026, T027 |
| FR-014 | Covered by description | T029 |
| FR-018 | Covered by description | T016 |
| FR-016 | PARTIAL — the mutation harness (T001) supplies the mechanism, and T035 pairs mutations for the new adjacent tests, but nothing yet asserts the property holds for EVERY acceptance criterion | open |
| FR-017 | PARTIAL — T002 control-needles the census helper specifically; no task extends golden-good/golden-bad validation to every instrument | open |

**FR-016 and FR-017 are recorded as PARTIAL, not closed.** They are the
self-referential requirements — the ones demanding that checks and instruments
be falsifiable — and this session produced twelve separate instrument errors
(a too-narrow grep, a `comm` on unsorted input, an exit code read after a
pipeline, and so on), each a confident wrong reading indistinguishable from a
real finding. Marking them covered on the strength of two tasks would be the
exact failure they exist to prevent. Closing them needs its own scope decision,
which is why they are named here rather than quietly absorbed.

**Shipped-status verification (§11.4.6).** FR-003/004/005 are recorded shipped
on the strength of symbol presence confirmed against a positive control (a
deliberately absent name returned 0 files, proving the search discriminates).
Presence is not behaviour: T034's full-suite retest is what converts these
three from "the code is there" to "it works".

## Phase 8: Polish & Cross-Cutting Concerns

- [ ] T031 [P] Record honest §11.4.3 SKIP-with-reason entries for DDoS, scaling and the inapplicable chaos subtypes per research.md, so absent coverage is declared rather than implied
- [ ] T035 [P] [TDD] For EVERY §11.4.3 SKIP recorded by T031, add the §11.4.81(C) ADJACENT-EQUIVALENT test exercising the closest invariant this platform CAN enforce, in `TK/scripts/tests/` — a SKIP alone leaves §11.4.81 HALF-satisfied (analyze finding D1). Each adjacent test carries its own paired §1.1 mutation, so it provably fails when its subject breaks (FR-016).
- [ ] T036 [P] Give every T031 SKIP the two fields §11.4.81(C) requires beyond the reason — the exact platform limitation and a runnable reproducer — plus a link to the honest-gap doc; a SKIP without a reproducer is unfalsifiable and cannot be re-tested when the platform changes.
- [ ] T032 [P] [REVIEW] Author the recommended gates named but NOT shipped: `CM-CENSUS-CONTROL-NEEDLED`, `CM-MECHANICAL-WORK-EXTRACTED`, `CM-EXTRACTED-TOOL-DOCUMENTED-AND-TESTED`, each with a paired §1.1 mutation
- [ ] T033 [P] Re-scan for mechanical work still performed by agents and extract it (§11.4.274(c)) — a recurring obligation, not a one-off
- [ ] T039 [P] Document the path for exposing an ADDITIONAL model end to end (FR-022) in `TK/docs/` — every file touched, both wires, and the Kimi twin (alias AND config), so adding one is routine rather than archaeology. Covers analyze finding E2.
- [ ] T040 [TDD] Make T039 an EXECUTABLE path, not prose (FR-022): a test that adds a throwaway model by following the documented steps, asserts it reaches `verified` on both wires with its twin present, then removes it — leaving the tree byte-identical. Doc drift then fails the test instead of surfacing months later as a broken alias.
- [ ] T034 [REVIEW] Full-suite retest (§11.4.40) and the review loop to a clean GO (§11.4.134) before merge

## Dependencies & Execution Order

### Phase Dependencies

- Phase 1 (T001–T004) blocks nothing but makes every later phase cheaper — the harness is what stops the hand-driven fix→verify loop that produced five consecutive defective fixes this session.
- Phase 2 blocks all user stories. **T005 blocks US1 absolutely.**
- US1 and US2 are both P1 and share `TK/scripts/lib.sh` — they are NOT parallel with each other.
- US3, US4, US5 touch disjoint trees and are mutually parallel once Phase 2 lands.

### Within Each User Story

RED test → implementation → review. No implementation task starts before its RED test fails on the current artifact (§11.4.115).

### Parallel Opportunities

- T001, T002, T003 — three different tools, no shared files.
- US3, US4, US5 — different repositories and different data stores.
- Within US1: T009, T010, T011 are independent RED tests.
- **Not parallel**: anything touching `TK/scripts/lib.sh` (T006, T012, T021) — one owner at a time (§11.4.119).

## Superpowers Execution

### Execution Discipline by Marker

- **[TDD]**: RED-GREEN-REFACTOR. The RED must reproduce the defect on the CURRENT artifact and carry a polarity switch (§11.4.115) — a test that passes on the broken artifact is blind.
- **[SUBAGENT]**: dispatch per `subagent-driven-development`. Scope must be disjoint (§11.4.58) and the tree quiescent before commit (§11.4.84).
- **[REVIEW]**: pause. Independent review on Fable at xhigh (§11.4.209), iterated to zero findings AND zero warnings (§11.4.134). This session's evidence: six rounds on one change, five of which found a defect the previous fix introduced.
- **[P]**: parallel dispatch where file scopes are disjoint.

### Checkpoint Protocol

At every phase boundary: summarise, run applicable tests, report results, and ask before proceeding.
**One addition specific to this feature**: at the Phase 2 boundary the T005 diagnosis is presented to
the operator BEFORE any US1 repair is designed, because the repair shape depends entirely on it.

## Notes

- **MVP** is Phase 1 + Phase 2 + US1: a list you can trust. US2 makes the claim checkable, and is
  the reason US1's completion can be believed — but US1 alone delivers standalone value.
- Task count: 34. US1: 8 · US2: 5 · US3: 3 · US4: 3 · US5: 3 · setup/foundational/polish: 12.
- T014 is deliberately a placeholder with no design. Filling it before T005 returns would be the
  guess this plan exists to avoid.
