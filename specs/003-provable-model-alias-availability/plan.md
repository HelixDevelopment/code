# Implementation Plan: Provable Model Alias Availability

**Branch**: `003-provable-model-alias-availability` | **Date**: 2026-09-08 | **Spec**: [spec.md](./spec.md)
**Input**: Feature specification from `specs/003-provable-model-alias-availability/spec.md`

## Summary

Make every alias the toolkit presents genuinely usable, and make the claim checkable. The measured
baseline is 8 of 27 usable (30%); the target is 100% with no capability withdrawn (FR-020, operator
decision). The work divides into four independent streams: repair the unusable aliases, make the
per-alias verdict deterministic and machine-evidenced, hold the active capability surface to the
minimum, and keep the navigation index honest. The unifying technical approach is that **every
capability claim is backed by a replayable artifact rather than a live sample**, because the one
backend involved is measurably non-deterministic and the operator's mandate is that validation be
fully deterministic.

## Technical Context

**Language/Version**: Bash 5 (toolkit + gates), Go 1.26 (`helix_code`, `helix_agent`, `helix_llm`),
Python 3.11 (`providers_resolve.py`, validation harnesses)
**Primary Dependencies**: no application framework — `jq`, `curl`, `sqlite3`; the HelixLLM gateway
serving both an OpenAI-shaped and an Anthropic-shaped wire on one endpoint
**Storage**: provider records `~/.local/share/claude-multi-account/providers/*.env` + `status.json`
(verdict cache, carries `checked_at`); `models.dev.cache.json` (24h TTL, 213 providers);
`docs/workable_items.db` (SQLite, tracked per §11.4.95); recorded wire corpus under
`helix_code/testdata/toolcall_wire_corpus/` (sha256-pinned)
**Testing**: bash suites under `scripts/tests/` (69 files, auto-discovered by `run-all.sh`, each with
its own sandboxed `HOME`); Go via `make test-unit` / `make test`; paired §1.1 mutations per guard
**Target Platform**: Linux x86-64 (verified). macOS is declared-supported and **UNVERIFIED** — the
`Darwin*` date branches and the `uname -s` cache have no runtime evidence (§11.4.81)
**Project Type**: CLI tooling over a local model gateway — no UI, no network service of our own
**Performance Goals**: alias listing under 1 second at current scale (SC-011). Measured baseline
2.83s at 40 rows after the verdict-age column, against 1.64s before it
**Constraints**: no credential in any verdict, log, evidence file or diagnostic (FR-019); fail-closed
on missing auth; verdicts identical across repeated runs against unchanged state (FR-006); no
operator-visible capability withdrawn without explicit confirmation (§11.4.122)

## Constitution Check

*GATE: Must pass before proceeding. Re-checked after design phase.*

Assessed against `constitution/Constitution.md` (the pointer at `.specify/memory/constitution.md`
deliberately carries no principles of its own — CONST-059). Only anchors that actually bear on this
plan are listed; an all-PASS table would be less useful than an honest one.

| Principle | Status | Notes |
|-----------|--------|-------|
| §11.4 / §11.4.5 — captured evidence per PASS | PASS | Every SC is measurable and cites an artifact. FR-008 forbids config-only, absence-of-error and name-presence as proof. |
| §11.4.6 — no guessing | **NEEDS ATTENTION** | 11 of 20 orphaned providers have **no diagnosed cause**. Phase 0 must settle it by running the resolver; until then no repair for that class may be planned, only investigated. |
| §11.4.50 — deterministic results | PASS | FR-006/FR-007 require replay as the default verdict path; the live probe is retained only as an opt-in distribution check. |
| §11.4.201 — a guard asserts the REAL condition | **NEEDS ATTENTION** | This session produced four guards that passed while their subject was broken. Every new guard in this feature needs a paired mutation proving the failure direction, and the mirror direction too. |
| §11.4.273 — control-needled census | PASS | Newly minted from this session's ten instrument errors; every inventory query in this plan carries a positive and a negative control. |
| §11.4.274 — mechanical work extracted | **NEEDS ATTENTION** | The extraction tooling is in flight, not landed. Tasks that would otherwise hand-execute mutation loops must consume it once it exists, or record why not. |
| §11.4.115 — RED on the broken artifact first | PASS | Every repair carries a polarity test reproducing the defect before the fix. |
| §11.4.134 / §11.4.142 — independent review to a clean GO | PASS | Applies per change; this session took six rounds on one change and each round found something real. |
| §11.4.169 — mandatory test-type coverage | **NEEDS ATTENTION** | Unit, integration and e2e are straightforward here. DDoS, chaos and scaling need an honest §11.4.3 SKIP-with-reason for a single-model local gateway rather than a fabricated suite. |
| §11.4.10 / CONST-042 — credentials | PASS | FR-019. This session's key comparisons were done by hash throughout; that discipline is carried into the tasks. |
| §11.4.122 — no silent capability removal | PASS | FR-020 repairs rather than withdraws; the un-repairable case returns as an operator decision. |
| §11.4.81 — cross-platform parity | **VIOLATION (accepted, tracked)** | macOS branches exist but are unexercised. See Complexity Tracking. |

### Post-design re-evaluation (2026-09-08, after Phase 0 + Phase 1)

Re-checked as the template requires. Two verdicts moved; one deliberately did not.

| Principle | Was | Now | What changed |
|-----------|-----|-----|--------------|
| §11.4.169 — test-type coverage | NEEDS ATTENTION | **PASS** | [research.md](./research.md) records an honest §11.4.3 SKIP-with-reason for DDoS and scaling (no flood surface, no horizontal dimension on a single-model loopback gateway) and a PARTIAL for chaos (process-death and network-fault apply; disk-full and OOM do not). Fabricating those suites would have produced green tests asserting nothing. |
| §11.4.273 — control-needled census | PASS | **PASS, now demonstrated** | The anchor earned itself three times inside Phase 0 alone: a resolver invocation whose positive control caught a wrong argument type; a second whose positive control was out of the instrument's declared scope and produced a false broken-instrument signal; and a key-visibility census whose "positive control" was the very question under test. All three are recorded. |
| §11.4.6 — no guessing | NEEDS ATTENTION | **NEEDS ATTENTION (unchanged, deliberately)** | Phase 0 ELIMINATED four candidate causes for the 20 orphans by measurement — missing credentials, stale catalogue, missing pins, and key-variable visibility (the last refuted by experiment after looking decisive). It did NOT diagnose the cause. The leading survivor — that `list-all` computes orphan status from a cached rather than a live resolution — is recorded as a hypothesis. Tasks that would REPAIR the orphan class therefore stay blocked behind the first task of the next research pass; tasks that do not depend on the cause proceed. Marking this PASS would be the guess the anchor forbids. |

**Gate verdict**: PROCEED to `/speckit-tasks`, with the orphan-repair stream explicitly gated on the
open diagnosis rather than planned on an assumption.

## Project Structure

### Documentation (this feature)

```text
specs/003-provable-model-alias-availability/
├── spec.md                  # Feature specification (24 FR, 11 SC, 4 clarifications)
├── plan.md                  # This file
├── research.md              # Phase 0 — the 11 undiagnosed orphans, and the SKIP-justified test types
├── data-model.md            # Phase 1 — Alias / Verdict / Evidence / Capability / Catalogue
├── contracts/               # Phase 1 — the CLI surfaces this feature promises
├── quickstart.md            # Phase 1 — runnable proof the feature works end to end
├── tasks.md                 # /speckit-tasks output
└── checklists/requirements.md
```

### Source Code (repository root)

```text
~/Projects/claude_toolkit/                  # SIBLING repo, its own 4 upstreams
├── scripts/claude-providers.sh             # the engine: resolve, verify, list, export
├── scripts/kimi-providers.sh               # the Kimi-framed view over that engine
├── scripts/providers_resolve.py            # catalogue match + pins -> resolved records
├── scripts/providers/overrides.json        # 34 manual pins (FR-024 generates the missing ones)
├── scripts/lib.sh                          # verdict age, staleness, alias/config writers
└── scripts/tests/                          # 69 auto-discovered suites + proof/ artifacts

helix_code/                                 # the Go application
├── internal/llm/                           # provider factory, endpoint redaction, cloud gate
├── internal/server/                        # wire facades (OpenAI + Anthropic)
└── testdata/toolcall_wire_corpus/          # recorded real wire bytes, sha256-pinned

constitution/scripts/                       # project-agnostic, inherited BY REFERENCE (§11.4.177)
├── hooks/                                  # the six PreToolUse guards (all now wired)
└── <extraction tooling from §11.4.274>     # mutation harness, census, waiter — in flight
```

**Structure Decision**: the feature spans three repositories that are deliberately NOT merged. The
toolkit is a standalone sibling with its own upstreams and must stay project-agnostic (§11.4.177);
the constitution is consumed by reference and never copied; `helix_code` holds the gateway-facing Go
code. Work is therefore partitioned by repository, and no task may span two of them in one commit —
that partition is also what makes the four streams below genuinely parallel.

## Execution Strategy

### TDD Requirements

- [ ] **Pin generation (FR-024)**: a wrong auto-pin fails while LOOKING like a working alias — the
      worst failure shape in this feature. RED must reproduce an unverifiable generated pin
      surfacing as a named error, not as an available alias.
- [ ] **Stale-catalogue fallback (FR-023)**: the announced-fallback requirement is precisely what
      stops a silent one. RED must show the system serving stale data WITHOUT announcing it.
- [ ] **Verdict determinism (FR-006/007)**: RED must reproduce a verdict that varies between runs.
- [ ] **Alias/config pairing (FR-002)**: RED must reproduce the 19-of-27 half-wired state.
- [ ] **Exit-code semantics (FR-009)**: RED must reproduce a "found nothing" indistinguishable from
      "could not look" — this session shipped exactly that defect twice.

### Parallel Execution Opportunities

- [ ] **Stream A (orphan repair, toolkit)** and **Stream B (skill/extension surface, constitution)**
      share no files and no repository.
- [ ] **Stream C (indexing: CodeGraph + Lumen)** is independent of both — different tool, different
      data store.
- [ ] **Stream D (validation harness + evidence layout)** must land BEFORE A's repairs can be proven,
      so it is a prerequisite of A, not a peer.
- [ ] Streams A and B may proceed only after Phase 0 closes the 11-orphan question; running them on
      an unknown cause is the §11.4.102 violation this plan exists to avoid.

### Human Checkpoints

1. **After Phase 0 research** — the operator sees the diagnosed cause of the 11 orphans before any
   repair is attempted, because the repair shape depends entirely on that cause.
2. **Before any capability is withdrawn** — FR-020's un-repairable case is an operator decision
   (§11.4.122), never an agent's.
3. **After each stream** — acceptance measured against the SC it claims, with the artifact.
4. **Before merge** — full-suite retest (§11.4.40) and the review loop to a clean GO (§11.4.134).

### Review Gates

- [ ] **Every change** (§11.4.142, no exception), on Fable at xhigh (§11.4.209), iterated to zero
      findings AND zero warnings (§11.4.134). This session's evidence for why: six rounds on one
      change, five of which found a defect the previous round's fix had introduced.
- [ ] **Pin generation and the credential path** — reviewed before anything consumes them.
- [ ] **Every new guard** — reviewed specifically for the mirror direction. Four guards this session
      passed while their subject was broken; three of those were caught only because a reviewer
      mutated the case the author had not thought of.

## Complexity Tracking

| Violation | Why Needed | Simpler Alternative Rejected Because |
|-----------|------------|-------------------------------------|
| §11.4.81 cross-platform parity: macOS `Darwin*` branches unexercised | The code must run on both platforms and the branches are written; only the runtime evidence is missing | Deleting the branches would break macOS outright. Claiming them verified would be a §11.4 bluff. The honest position is a declared, tracked gap until a Darwin host is available — and this plan states it rather than letting an all-PASS table imply coverage that does not exist. |
| §11.4.169: DDoS / chaos / scaling suites | The anchor requires the full test-type set | A single-model local gateway has no meaningful DDoS or horizontal-scaling surface. Fabricating those suites would produce green tests that assert nothing — the exact PASS-bluff the anchor exists to prevent. Phase 0 records an honest §11.4.3 SKIP-with-reason per absent type instead. |
