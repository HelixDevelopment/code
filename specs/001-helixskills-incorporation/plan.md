# Implementation Plan: HelixSkills incorporation into HelixCode and HelixAgent

**Branch**: `001-helixskills-incorporation` | **Date**: 2026-07-29 | **Spec**: [`spec.md`](spec.md)
**Work item**: HXC-159 · **Revision**: 2 (2026-09-23, T-P9.01 review corrections) · **Author**: `(T1/main - claude1 - opus - xhigh)`
**Input**: [`spec.md`](spec.md) + the Phase-1 research tree
`docs/research/fully_incorporate_the_helixskills_system_into_helixcode_heli_20260728T192622Z_2557683/`

**Status**: EXECUTED — every phase P0–P8 and B1–B3 is ticked in `tasks.md` with evidence pointers; P9 closure in progress (T-P9.01 review 2026-09-23 found and fixed real defects and recorded the residual gaps — see `tasks.md` §P9). Per §11.4.108 no item is "done" until its runtime signature verifies on a clean target: T-P9.02 (clean-target retest) is still open. (This line read "PLAN ONLY" until the review corrected it, §11.4.186.)

---

## Table of contents

1. [Summary](#1-summary)
2. [Technical context](#2-technical-context)
3. [Constitution check](#3-constitution-check)
4. [Structure decision](#4-structure-decision)
5. [Phase architecture and why it is ordered this way](#5-phase-architecture-and-why-it-is-ordered-this-way)
6. [How each of the five constraining findings is handled](#6-how-each-of-the-five-constraining-findings-is-handled)
7. [Risk → mitigation → task coverage](#7-risk--mitigation--task-coverage)
8. [Test strategy — making §11.4.169 affordable](#8-test-strategy--making-1141169-affordable)
9. [Runtime-signature registry](#9-runtime-signature-registry)
10. [Complexity tracking](#10-complexity-tracking)
11. [Documentation and diagram deliverables](#11-documentation-and-diagram-deliverables)
12. [Honest boundary](#12-honest-boundary)

---

## 1. Summary

Make skills authored once reach both HelixCode and HelixAgent automatically, safely,
and **without degrading either agent**.

The plan's shape is forced, not chosen. Four measurements decide almost everything:

1. The upstream payload is **not consumable where it sits** — 237 Go files and 755
   exported symbols exist, but at `docs/research/mvp/…/project/` with a module path
   that disagrees with the repository URL. So the sequence is
   **build-then-incorporate**, and no incorporation phase may be scheduled before a
   loader exists.
2. **Skill libraries get measurably worse as they grow** (−21 % at 202 active skills,
   68 % of it shadowing, context cost zero). The local corpus is 1 174. So activation
   is an explicit allowlist behind a **measured** ceiling — and the measurement is a
   deliverable, not an input.
3. **HelixAgent is already declared a dependency *of* HelixSkills.** So the edge
   cannot be inverted; HelixAgent becomes a client over the wire.
4. **Two of the Bridge's absent layers are obtainable, one is not, and one is a build
   flag.** So the Bridge work is three different tasks, not one installer.

Everything else follows from those four.

---

## 2. Technical context

**Language/Version**: Go — root meta-module `go 1.25.2`, inner `helix_code/` `go 1.26`,
`submodules/helix_agent` `go 1.26`, upstream `skills` module `go 1.25.5`.

**Primary dependencies**: the skills corpus is **pure data** (Markdown + YAML
front-matter) with **no Go dependencies at all** — this is deliberate and load-bearing
(it is what makes R-25's `replace`-outside-main-module problem a non-issue). The
out-of-process Layer-2 service depends on pgx/v5, pgvector, go-redis/v9, Gin, mark3labs
mcp-go, Cobra, Bubbletea, robfig/cron.

**Storage**: none added to either consumer. Layer-2 state is PostgreSQL + pgvector,
out of process. Both consumers have **no skill persistence today** and this plan does
not add any — it makes the *source* authoritative instead.

**Testing**: Go `testing` + testify; the §11.4.169 thirteen-type matrix; Challenges
(`vasic-digital/challenges`); HelixQA banks (`HelixDevelopment/helix_qa`).

**Target platform**: Linux (this host), macOS, Windows for the consumers. The host
**lacks X11/GL headers**, which is why the loader must be GUI-free by construction —
the constraint is turned into a gate rather than a caveat.

**Project type**: multi-module monorepo with submodules; a shared pure-data library
plus two independent consumers.

**Performance goals**: loader cold-enumeration budget set from a captured baseline on
this host (NFR-001) — *not* asserted in advance. The shadowing benchmark's runtime is
itself a budgeted cost, since it re-runs on every corpus change.

**Constraints**: no nested own-org submodule chains (CONST-051(C)); exactly one
`constitution/` gitlink; no Go import of HelixSkills `internal/` from either consumer;
no force-push (§11.4.113); the workable-items SQLite DB is a shared binary with
concurrent writers.

**Scale/scope**: 3 skill corpora (7 + 1 174 + 0-at-root); 57 upstream capability rows;
23 constitution-derived extensions; 7 Bridge layers; 29 risks; 2 consumers.

---

## 3. Constitution check

*GATE: must pass before Phase 0 research. Re-checked after design.*

Evaluated against `constitution/Constitution.md` — **not** against
`.specify/memory/constitution.md`, which is a pointer with no independent authority
(CONST-059).

| Anchor | Requirement | How this plan satisfies it | Verdict |
|---|---|---|---|
| §11.4 / §11.4.1 / §11.4.123 | No PASS without captured runtime evidence | Every task carries a runtime signature (§9) and an evidence path; no task closes on a source grep | PASS |
| §11.4.6 | No guessing | 9 `UNCONFIRMED:` items registered in `spec.md` §12, each with what would settle it; the shadowing threshold is explicitly *not* chosen | PASS |
| §11.4.108 | Four-layer verification | §9 registry is the definition of done; source-layer gates are explicitly labelled insufficient | PASS |
| §11.4.110 | Pre-build readiness + clash detection | P1 lands the clash-class gates (pin uniqueness, module-path uniqueness, symlink hygiene, namespace disjointness) before any build depends on them | PASS |
| §11.4.169 | 13 mandatory test types | §8 scopes the matrix by architectural surface, so all 13 apply to the loader while 1 177 skills get one parameterised contract test | PASS |
| §11.4.224 | Test-first, ≥85 % coverage | Every task's test types are listed before its implementation subtasks; RED-first per §11.4.115 | PASS |
| §11.4.74 / §11.4.28 | Reuse before rewrite; submodules stay project-agnostic | Catalogue survey produced 9 verdicts; the upstream module is **extended**, never forked; `skills` resolves its constitution by config injection | PASS |
| CONST-051(C) | No nested own-org chains | FR-003 + T-P3.02: the nested pin is never initialised; gate asserts exactly one gitlink | PASS |
| §11.4.122 | No silent removal | The 1 174-file corpus is explicitly **not** moved (D-6); its relocation is an operator question, not a plan step | PASS |
| §11.4.113 | No force-push | No history rewrite anywhere in the plan; upstream changes land by merge | PASS |
| §11.4.65 / §11.4.212 | Four-format exports, README-reachable | P8 deliverables | PASS |
| §1.1 | Paired mutation per gate | Every gate in §7 names its mutation | PASS |

**One deliberate deviation, declared rather than hidden**: the operator's phrase
"nothing left out" is honoured on the capability axis and **explicitly limited** on the
simultaneous-activation axis. Justification is measurement (§1 item 2 above), and it is
argued in the open in `spec.md` §1 rather than absorbed silently. If the operator
rejects the limitation, the ceiling becomes a parameter — the machinery does not change.

---

## 4. Structure decision

```text
<repo-root>/
├── constitution/                    ← THE single constitution gitlink (unchanged)
├── submodules/
│   ├── skills/                      ← NEW: HelixDevelopment/skills @ raw SHA on main
│   │   └── constitution/            ← NEVER initialised (submodule…update = none)
│   ├── helix_agent/                 ← consumer 2; gains NO Go dep on skills
│   └── skill_registry/              ← EXISTING but mis-wired; investigated in P0
├── helix_code/                      ← consumer 1 (inner Go module)
├── scripts/
│   ├── register_skills.sh           ← NEW (FR-037) manifest-driven, idempotent
│   └── register_mcp.sh              ← NEW (FR-037)
├── docs/extensions/
│   └── EXTENSIONS_CATALOG.md        ← NEW (FR-039, §11.4.228)
├── specs/001-helixskills-incorporation/
│   ├── spec.md  plan.md  tasks.md   ← this feature
└── qa-results/hxc159/<area>/<run-id>/   ← captured evidence root
```

**Upstream** (`HelixDevelopment/skills`, extended not forked):

```text
skills/
├── go.mod                           ← MOVED to root; module path reconciled
├── helix-deps.yaml                  ← NEW at root (CONST-054)
├── CONSTITUTION.md                  ← NEW at root
├── internal/ cmd/ migrations/       ← MOVED up from docs/research/mvp/…/project/
└── pkg/skills/                      ← NEW: the pure-data loader (the consumable artefact)
```

**Why `submodules/skills/` and not the root**: the grouped layout is what upstream's own
`helix-deps.yaml` declares (`layout: grouped`) and what 20+ existing `.gitmodules`
entries already use. Consistency here is not cosmetic — R-11's onboarding gate asserts
the whole checklist across all entries at once.

**Why a `pkg/` loader rather than importing `internal/`**: `03` §2 measured that
HelixAgent's public Go surface is 4 files, so its `internal/skills` is unreachable from
`dev.helix.code`. `pkg/` or over-the-wire are the only two proven cross-module seams,
and this plan uses both — `pkg/` for enumeration, MCP for the graph service.

---

## 5. Phase architecture and why it is ordered this way

Ten phases in two tracks. **Track A** is the incorporation; **Track B** is the Bridge.
They are genuinely independent and run concurrently (§11.4.103), joining only where
marked.

```
Track A  P0 ─ P1 ─ P2 ─ P3 ─ P4 ─┬─ P5 ─┐
                                  └─ P6 ─┴─ P7 ─ P8 ─ P9
Track B       B1 ─ B2 ─ B3 ─────────────────────┘
              (concurrent with P1…P6; joins at P7)
```

| Phase | Name | Gated by | Why here and nowhere else |
|---|---|---|---|
| **P0** | Decisions locked & preconditions probed | — | The six `03` §8 decisions are taken in `spec.md` §2. P0 closes the three unknowns that would change a decision if they came back differently (U-5 competing abstractions, the `skill_registry` module's real intent, U-6 CONST-040). Cheap, and each could invalidate a later phase's design. |
| **P1** | Hygiene & registrars | P0 | Every item here is a **live defect today**, independent of HelixSkills: 0 of 7 skills registered, a broken foreign-OS symlink, missing registrars that §11.4.164 already calls, a case-split corpus, a skill with no manifest. Fixing them first means later phases build on a known-good base instead of debugging two things at once. All are small and independently valuable. |
| **P2** | Shadowing benchmark & calibration | P1 | **Must precede any activation policy.** Choosing a ceiling before the curve exists would be guessing (§11.4.6). This is the single most schedule-surprising ordering decision in the plan, and it is the one the measurement forces most directly. |
| **P3** | Upstream extension (module promotion) | P0 | The payload must become consumable before anything can consume it. Independent of P1/P2, so it runs concurrently with them. |
| **P4** | The loader | P3 (+ P2 for thresholds) | R-01's mitigation is explicit: *"an incorporation phase MUST NOT be scheduled before Phase C exists."* The loader is that phase. |
| **P5** | Incorporate into HelixCode | P4 | Consumer 1. Chosen first because its attachment point is the lowest-friction of the eight ranked (an ordered directory list already exists). |
| **P6** | Incorporate into HelixAgent | P4 | Consumer 2, over the wire. Independent of P5 — deliberately, so parity is proven by two independent integrations rather than one copied twice. |
| **P7** | Parity, security & conformance | P5+P6+B3 | Parity can only be measured once both consumers exist. Security enforcement lands here because it must apply to both uniformly. |
| **P8** | Documentation, diagrams, exports | P7 | Documents a system that exists; documenting a planned one is how design docs come to describe uninstalled software. |
| **P9** | Review, coverage ledger, closure | P8 | §11.4.125/§11.4.134/§11.4.142 iterate to a zero-finding GO. |
| **B1** | Bridge layer-health probe | — | Runs immediately; everything else in Track B depends on knowing the real per-layer state, and the four-value vocabulary must exist before any phase can declare a layer dependency. |
| **B2** | Author the L3/L4 installer | B1 | Only the obtainable layers. Explicitly **not** L5. |
| **B3** | Delete L5 upstream; record the 6-layer architecture | B1 | Independent of B2 — it is a design correction, not an installation. |

**Three orderings that are load-bearing and easy to get wrong:**

- **P2 before any activation policy.** Anywhere else and the threshold is a guess.
- **P1 before P4.** The loader's fail-closed behaviour (hard error on a missing
  `SKILL.md`) will *immediately* fail against today's corpus, because `multitrack` has
  no manifest. Normalising first means P4's first red is a real defect, not a known one.
- **P5 and P6 in parallel, never sequential-copy.** Parity proven by two independent
  integrations is evidence; parity proven by copying one integration is a tautology.

---

## 6. How each of the five constraining findings is handled

### Finding 1 — the upstream payload

**Correction recorded (§11.4.6).** The brief characterised the repo as *"nearly empty
of implementation"*. Measured against `02` §1 and §4: the repo contains **237 Go files,
1 418 top-level declarations, 755 exported symbols, 26 packages, 13 extension-point
interfaces, 12 migrations, 0 parse errors** — a production-shaped service. What is true
is the narrower, sharper statement: **0 `.go` files outside `docs/`**, no root `go.mod`,
no `SKILL.md` at root, no release tags, and a declared module path
(`github.com/helixdevelopment/skill-system`) that disagrees with the repository URL
(`github.com/HelixDevelopment/skills`) in **both name and case**.

The difference matters to the plan. "Nearly empty" implies *write it*; the measurement
says *relocate and reconcile it* — which is smaller, safer, and preserves 755 exported
symbols instead of reinventing them. **Handled by**: P3 (promote the module, reconcile
the path, add root `helix-deps.yaml` + `CONSTITUTION.md`, merge `feature/testing-infra`)
and by the "extend, never fork" rule in §4. The raw-SHA pin requirement stands
unchanged — there genuinely are no tags.

### Finding 2 — the dependency-edge inversion

Upstream's `helix-deps.yaml` declares `HelixDevelopment/HelixAgent` as a dependency
*of* HelixSkills ("multi-provider LLM + embeddings client used by the skill-graph
service"). **Handled at the layout level, as required**: decision D-4 makes HelixAgent a
**client**, never an importer. P6 wires it through
`RegisterExternalToolSource(name, fetcher)` and the MCP config — neither of which
creates a Go module edge. The gate is mechanical, not a convention: **T-P6.05** asserts
HelixAgent's module graph contains no edge on HelixSkills, with the paired mutation
adding a `require` line and expecting FAIL. Combined with FR-003's single-constitution
layout, the cycle CONST-051(C) forbids becomes structurally impossible rather than
merely avoided.

### Finding 3 — skill shadowing

Treated as the plan's primary risk, and the only one where **a gate is not sufficient
proof** — the harm is behavioural, so the proof must be behavioural. **Handled by P2 as
a deliverable**: a local SkillsBench analogue (fixed task set, known-correct skill
selection, run at increasing active-library sizes) emitting a pass-rate-per-size curve.
The ceiling (FR-016) and similarity threshold (FR-017) are **read off that curve**;
neither is written into this plan, because writing them here would be the guess §11.4.6
forbids. Four cooperating mitigations ship together: opt-in subsets, hard ceiling,
description-disambiguation gate, namespacing. The curve re-runs as a performance-type
test on every corpus change, and the paired mutation raises the ceiling past the
measured knee and requires the benchmark to show the regression.

**Explicitly rejected**: trimming prose, shortening bodies, lazy-loading. Measured
context overhead is statistically zero; those fix the wrong variable.

### Finding 4 — the Bridge

**Corrected mid-plan** and now handled as **three different problems**, because the
remedy is mixed:

| Layer(s) | Class | Handled by |
|---|---|---|
| L1, L2, L6 | **WIRED** | Reused as-is. No task. L2 initialisation is **not re-planned** — the item's own environment facts are stale (`spec.md` §12) |
| L3, L4 | **ABSENT-BUT-OBTAINABLE** | **B2** authors the installer that genuinely fixes them — with the `--from` release-URL path, because the README's bare `specify extension add` fails on a default install (only 4 first-party catalogue entries, all `spec-kit-core`) |
| L5 | **NONEXISTENT** | **B3 deletes the tier.** No installer is planned, because there is nothing to install. The substitution is designed explicitly: Spec Kit + Superpowers + Claude Code's own skill/subagent machinery, all already WIRED |
| L7 | **ABSENT-BUT-BUILDABLE** | Out of scope here; recorded as a **build-flag** task (`-DGGML_RPC=ON`, submodule already in-tree) rather than a procurement item, so it is not mis-scheduled later |

The plan **includes authoring the installer** (FR-044, B2) as the operator requires, and
**states honestly** which layers can be stood up: L1/L2/L6 today, L4 pinned, L3 only
behind an org mirror, L5 never. Supply-chain tiering (FR-049) is carried into the
installer's own logic, not left as advice: SuperSpec's mutable tag-archive install is
rejected by construction, and `erophames/superpowers-mcp` is prohibited outright.

### Finding 5 — three schemas, and the `internal/` wall

**Schema**: D-1 picks the **Agent Skills open standard** — settled, as the brief says,
by the standard's existence (~45 implementations) rather than by preference. Both
proprietary dialects become profiled extensions with round-trip converters (FR-007),
authored in **P1** so the loader in P4 has a well-defined input.

**The wall**: HelixAgent's 4-file public surface makes `internal/skills` unreachable
from `dev.helix.code`. Handled by using **both** proven seams and neither unproven one:
`pkg/` for pure-data enumeration, MCP for the service. The existing-but-mis-wired
`submodules/skill_registry` (a dangling `replace` with a module path that disagrees with
its replace key, required by nobody) is **investigated in P0 before being designed
around** — `03` §7.4 flags it as possibly the intended third-module answer, and
designing around it without looking would be the same error as assuming the Bridge
existed.

---

## 7. Risk → mitigation → task coverage

All **29** register rows. 26 map to concrete tasks; 3 are already resolved upstream in
the register itself. **No row is left as a note.**

| Risk | Sev | Mitigation (from the register) | Task | Gate | Paired §1.1 mutation |
|---|---|---|---|---|---|
| **R-19** shadowing | CRIT | opt-in subsets + calibrated ceiling + similarity gate + namespacing | T-P2.01…05 | `CM-ACTIVE-SKILL-CEILING` | raise ceiling past knee → benchmark MUST show regression |
| **R-01** premise/payload | CRIT | build-then-incorporate; promote module upstream | T-P3.01…04 | `CM-HXC159-SOURCE-CONSUMABLE` | point gate at pre-P3 SHA → FAIL |
| **R-02** nested chain | CRIT | single-root layout; nested pin never initialised | T-P3.05 | `CM-NO-NESTED-OWN-ORG-CHAIN` | init the nested pin → FAIL |
| **R-03** 46-commit skew | CRIT | one constitution ⇒ skew impossible by construction | T-P3.05 | `CM-CONSTITUTION-PIN-SINGLE` | reset a pin to `68875c7a` → FAIL |
| **R-21** L5 nonexistent | CRIT | **delete the tier**; substitute already-wired execution | T-B3.01…03 | `CM-BRIDGE-LAYER-PRESENCE` | declare a dependency on L5 → FAIL |
| **R-04** 3 disjoint corpora | HIGH | single registry, typed sources, fail-closed collision | T-P4.02 | `CM-SKILL-SOURCE-REGISTRY` | collision fixture → typed error, never silent resolve |
| **R-05** case split + missing manifest | HIGH | normalise case; author `multitrack/SKILL.md`; case-exact fail-closed loader | T-P1.01, T-P1.02 | `CM-SKILL-MANIFEST-COMPLETE` | remove a `SKILL.md` → FAIL |
| **R-06** broken foreign-OS symlink | HIGH | repo-relative or delete; ban absolute targets | T-P1.03 | `CM-NO-ABSOLUTE-OR-BROKEN-SYMLINKS` | recreate the `/Volumes/T7/…` link → FAIL |
| **R-07** no parity mechanism | HIGH | library-owned conformance suite; both consumers publish reports | T-P7.01…02 | `CM-CROSS-CONSUMER-PARITY` | register in one consumer only → FAIL |
| **R-08** test-surface explosion | HIGH | scope by architectural surface, not skill count (§8) | T-P7.04 | coverage ledger | blank ledger cell → FAIL |
| **R-14** skills as attack surface | HIGH | trust tiers + declared capabilities + content-hash + shell-exec off | T-P7.05…08 | `CM-SKILL-CAPABILITY-DECLARED` | undeclared capability → refusal required |
| **R-20** L3/L4 designed-only | HIGH | separate artifact-discipline from execution; layer-presence precondition | T-B1.01…03, T-B2.01…04 | `CM-BRIDGE-LAYER-PRESENCE` | depend on a non-WIRED layer → FAIL |
| **R-22** `.claude/skills/` collision | HIGH | disjoint prefixes + pre-write check + manifest regeneration | T-P1.04 | `CM-SKILL-NAMESPACE-DISJOINT` | plant a colliding entry → FAIL |
| **R-22b** biting case undocumented | MED | same rules + install-order determinism | T-P1.04 | (as above) | run registrars in both orders → byte-identical required |
| **R-09** no X11/GL headers | MED | loader GUI-free by construction | T-P4.06 | `CM-SKILLS-LOADER-HEADLESS` | add a Fyne import → FAIL |
| **R-10** no extensions catalogue | MED | author it with per-platform columns | T-P8.02 | `CM-EXTENSIONS-CATALOG-PRESENT-AND-COMPLETE` | register a skill with no row → FAIL |
| **R-11** half-done onboarding | MED | one gate asserting the whole checklist | T-P3.06 | `CM-SUBMODULE-ONBOARDING-COMPLETE` | delete the new `helix-deps.yaml` → FAIL |
| **R-12** `.mcp.json` hard-codes paths | MED | generate entries from the manifest; diff-check | T-P5.04 | `CM-MCP-ENTRIES-GENERATED` | hand-edit a path → FAIL |
| **R-13** stale/decoy branches | MED | pin raw SHA on `main`; reconcile `feature/*`; retire only with proof | T-P3.03 | `CM-SUBMODULE-PIN-ON-MAIN` | repoint to `helix_skills` → FAIL |
| **R-23** third constitution | MED | inheritance pointer in `.specify/memory/` | **T-P1.06 — done 2026-07-29** | `CM-CANONICAL-ROOT-CLARITY` | strip the pointer heading → FAIL |
| **R-24** registrars absent | MED | author `register_skills.sh` + `register_mcp.sh` | T-P1.05 | `CM-CONSTITUTION-AUTO-PROPAGATION` | remove a registrar → FAIL |
| **R-25** `replace` outside main module | MED | keep the shared library dependency-free (pure data) | T-P4.01 | `CM-CONSUMER-VERSION-PARITY` | bump one consumer only → FAIL |
| **R-26** duplicate module path | MED | decide explicitly; do not build the seam on the ambiguity | T-P0.03 | `CM-UNIQUE-MODULE-PATHS` | reintroduce the duplicate → FAIL |
| **R-27** taskstoissues forks SSoT | MED | exclude from the manifest or re-point at the DB | T-P1.07 | `CM-WORKABLE-ITEMS-SSOT-UNFORKED` | re-register unmodified → FAIL |
| **R-28** corpus not reusable as-is | MED | keep `vendored`-tier, unactivated; relocation is an operator question | T-P0.04 | `CM-SKILL-SOURCE-AT-ROOT` | register a source via `submodules/helix_agent/skills` → FAIL |
| R-15 | — | **RESOLVED** → pure-data skills, `plugin` eliminated by its own docs | design input to P4 | — | — |
| R-16 | — | **RESOLVED, milder** → became R-23 | see R-23 | — | — |
| R-17 | — | **RESOLVED** → became R-25 | see R-25 | — | — |

**Coverage: 26 open risks → 26 mitigations → 26 task groups → 26 gates, each with a
paired mutation. 3 resolved rows carried as design inputs. Zero unmitigated rows.**

### Risks added during planning (not in the Rev-2 register)

| ID | Risk | Sev | Mitigation | Task |
|---|---|---|---|---|
| **R-29** | **Two independent installers provision into one `.claude/skills/` tree.** Claude Code **neither errors nor warns** on a skill-name collision — it resolves silently — and the upstream namespacing request was **closed as not planned**. The collision here happens in the *filesystem*, before Claude Code reads the directory, so no upstream mechanism sees it. §11.4.8: **`NO external solution found — original work`** | HIGH | Install-time collision detection shipped by this feature: pre-write enumeration, abort-never-overwrite, reserved prefixes, manifest-driven regeneration. Narrower than first feared (Superpowers is plugin-namespaced; SuperB is a spec-kit extension, not a `.claude/skills/` entry) but genuinely uncovered | T-P1.04 |
| **R-30** | **Scope-limited probes yield confident false negatives.** L6 was read as absent because `systemctl list-unit-files` was run at *system* scope while all seven Helix units are *user*-scope (§11.4.201 false-null class) | MED | Every presence probe MUST enumerate all applicable scopes, and the probe records which scopes it checked. Encoded in the probe, not remembered | T-B1.01 |
| **R-31** | **The work item's own environment facts are stale** — HXC-159 states `.specify/` does not exist; it does. A plan built on the description rather than the artifact would re-plan a completed step | MED | Re-derive environment facts from the artifact at phase start; record contradictions rather than silently reconciling them | T-P0.01 |

---

## 8. Test strategy — making §11.4.169 affordable

Naive application of 13 test types to 1 177 skills is ~15 000 test units (R-08). The
scoping rule is **by architectural surface, not by skill count** — and this is genuine
full coverage, not a reduction: every skill *is* tested, while the expensive types land
once on the component that can actually fail in those ways. A skill cannot deadlock;
the loader can.

| Layer | Population | Test types applied |
|---|---|---|
| Loader / registry / resolver | 1 component | **all 13**: unit, integration, e2e, full-automation, Challenges, HelixQA, DDoS, security, stress+chaos, concurrency/atomicity, race/deadlock, memory, benchmarking |
| Per-skill | 1 177 skills | **1** parameterised contract test — schema-valid, discoverable, non-colliding, capability-declared — table-driven: one function, N cases |
| Representative skills | ~5, one per source dialect | e2e + Challenge + HelixQA bank |
| Activation policy | 1 curve | performance-type benchmark (P2), re-run on every corpus change |

**Type-by-type, where each actually lands:**

- **Race/deadlock** — concurrent registrar invocations and concurrent loader reads over
  one registry (`-race`).
- **Concurrency/atomicity** — two registrars writing `.claude/skills/` simultaneously;
  the pre-write check must hold under contention, not only in sequence.
- **Stress+chaos** — SIGKILL mid-registration (partial-write recovery); a source root
  deleted under an in-flight enumeration; disk-full during manifest regeneration.
- **DDoS/load** — the MCP surface under sustained tool-call load.
- **Memory** — enumeration of the full 1 174-file corpus under a peak-RSS ceiling; leak
  census across repeated reloads.
- **Benchmarking** — the NFR-001 cold-enumeration budget, measured before it is set.
- **Security** — the FR-022…FR-027 battery: undeclared capability refused, content-hash
  drift detected, shell execution off by default, credential scan clean.
- **Challenges + HelixQA** — the P5/P6 user journeys end to end, per consumer.

Each gate ships its paired §1.1 mutation, listed in §7.

---

## 9. Runtime-signature registry

Per §11.4.108 this registry — **not** any source-level gate — is the definition of
done. Each signature is a **runtime observable on a clean target**. None is a grep.

Two signatures were verified as *reachable surfaces* in this session so that the
registry names real commands rather than plausible ones:
`helix_code/cmd/cli/main.go:3306` (the `skills` subcommand dispatcher) and
`submodules/helix_agent/internal/router/router.go:1363-1368`
(`GET /v1/skills`, `/categories`, `/:category`, `POST /match`).

| # | Capability | Runtime signature (observable on a clean target) |
|---|---|---|
| RS-01 | Registrars work | On a clean checkout, running `scripts/register_skills.sh` then `helixcode skills list` prints ≥1 allowlisted constitution-sourced skill under its namespaced name |
| RS-02 | Idempotence | Running both registrars twice, in both orders, yields a byte-identical `.claude/skills/` tree (checksum equality, §11.4.50) |
| RS-03 | Collision fail-closed | With a planted foreign entry, the registrar exits non-zero, changes nothing, and names the colliding path |
| RS-04 | Manifest completeness | Loader startup against a source dir containing `register.sh` and no `SKILL.md` exits non-zero naming that path |
| RS-05 | Ceiling enforced | Loader startup with an activation set of ceiling+1 refuses to activate and names the offending skill |
| RS-06 | Disambiguation | With two active skills above the similarity threshold, the gate exits non-zero naming both |
| RS-07 | Shadowing curve exists | A curve artefact with ≥3 sampled sizes and per-size pass-rate is present, and the configured ceiling equals the value derived from it |
| RS-08 | HelixCode reachability | `helixcode skills list` shows the skill; invoking it emits the skill's rendered output (not a stub, not an error) |
| RS-09 | HelixCode auto-trigger repaired | A trigger phrase entered in the **CLI** activates the skill (works in TUI/desktop today, dead in CLI) |
| RS-10 | HelixAgent reachability | `GET /v1/skills` returns the externally-sourced skill in its JSON body |
| RS-11 | Edge direction preserved | `go mod graph` for `dev.helix.agent` contains **no** edge to the skills module |
| RS-12 | Parity | Both capability reports emitted; their diff is empty modulo manifest-marked consumer-optional entries |
| RS-13 | Single constitution | `find . -name Constitution.md -path '*/constitution/*'` returns exactly one path on a fresh recursive checkout |
| RS-14 | Capability refusal | A fixture skill requesting an undeclared capability is refused at load time, and the refusal appears in the audit record |
| RS-15 | Content-hash pin | Mutating a pinned skill's bytes makes the verification exit non-zero |
| RS-16 | Headless | The full loader suite exits 0 on this host, which has no X11/GL headers |
| RS-17 | Generated MCP entries | Each generated `.mcp.json` server **starts and answers a probe** — path correctness proven by execution, not by the string looking right |
| RS-18 | Bridge layer report | The probe emits per-layer `WIRED/PARTIAL/DESIGNED-ONLY/NONEXISTENT` with the captured command, checking **both** systemd scopes |
| RS-19 | L3/L4 installed | After the installer, `specify extension list` is non-empty and names the pinned version |
| RS-20 | L5 deleted, not stubbed | The Bridge design records 6 live layers and every remaining SuperBridge reference sits under a dated `ARCHITECTURE AMENDMENT` banner marking it retired design (README.md redrawn; 7 other documents banner-amended). **Amended 2026-09-23 by the T-P9.01 review to match what B3 delivered:** the original wording "no artefact references a SuperBridge MCP endpoint" is NOT literally satisfied — `EXTENSION_DEVELOPMENT.md` Part C / §D config blocks (`superbridge-mcp` `.mcp.json` entries, `SUPERBRIDGE_MCP_PORT=9595`) and `APPENDIX.md` §F still carry the retired server's endpoint text as historical design under the banner; retiring or inline-marking those blocks is a T-P9.03 follow-up |
| RS-21 | Catalogue complete | Every registered skill has a row with non-empty per-platform columns |
| RS-22 | Docs reachable | Every produced document is reachable from the main README by link traversal |

---

## 10. Complexity tracking

| Deviation | Why needed | Simpler alternative rejected because |
|---|---|---|
| A **third** artifact (`pkg/skills` loader) rather than extending a consumer's existing registry | Measured: HelixAgent's public Go surface is 4 files, so `internal/skills` is unreachable across modules; `pkg/` or the wire are the only proven seams | Extending either consumer's registry directly cannot serve the other — it would guarantee the asymmetry FR-035 exists to prevent |
| Out-of-process Layer-2 rather than embedding the graph | Embedding drags pgx + pgvector + Redis + Gin into both consumers, and re-opens R-25 (`replace` ignored outside the main module) | Embedding is simpler *until* the second consumer, at which point it is strictly worse |
| A benchmark as a **deliverable** rather than a chosen constant | The effect is established; its local magnitude is not. A constant would be a guess (§11.4.6) | Borrowing the paper's 202 would import a threshold measured on a different corpus, different models, different tasks |
| Two registrar scripts rather than one | They provision different targets (skills tree vs MCP config) and §11.4.164 already calls both by name | One script would have to branch on target anyway, and the hook's contract names two |
| Authoring a Bridge installer for only **2 of 5** absent layers | L5 does not exist; L7 is a build flag, not an install | A uniform installer would necessarily contain a step that can never succeed |

---

## 11. Documentation and diagram deliverables

The operator requires existing documentation **extended and updated**, not merely
new documents added, with illustrations incorporated into all materials.

| Deliverable | Kind | Anchor |
|---|---|---|
| `docs/extensions/EXTENSIONS_CATALOG.md` | NEW — per-platform compatibility matrix | §11.4.228 (FR-039) |
| `docs/CAPABILITIES.md` | **UPDATE** — line references are already drifted (cites `:325`/`:1652`; actual `:368`/`:1730`) | §11.4.12 |
| README | **UPDATE** — every produced doc reachable by link traversal | §11.4.212 |
| Skills user manual + FAQ | NEW + **UPDATE** of existing skill docs in both consumers | operator requirement |
| Bridge design corpus | **UPDATE upstream** — 7-layer → corrected 6-layer | FR-047 |
| Three-layer architecture diagram | Mermaid — source / graph service / generated catalogue | §11.4.153 |
| Registration-path sequence diagram | Mermaid — constitution → registrar → `.claude/skills/` → consumer | — |
| Shadowing curve | Plotted artefact from real measurement | FR-019 |
| Trust-tier / capability-enforcement diagram | Mermaid | FR-022…FR-026 |
| Bridge layer-state diagram | Mermaid — colour-coded by the four-value vocabulary | FR-046 |

All four-format exported per §11.4.65 / §11.4.153, and validated per §11.4.168 —
including the check that Mermaid renders as **images, not as raw source text leaking
into the PDF**, which is a defect this project has shipped before.

---

## 12. Honest boundary

**What this plan is**: a design derived from measurements taken in Phase 1 and
re-verified where cheap. Every factual claim traces to a cited research-tree section or
to a probe captured in this session.

**What it is not** (as written 2026-07-29): proof that any of it works — at planning time nothing had been implemented, no gate existed and no runtime signature had been observed. **Status 2026-09-23 (T-P9.01 review):** the plan HAS been executed; the §9 signatures were re-verified by the review with the results recorded in `tasks.md` §P9 (notably RS-01 was NOT true end-to-end until the review fixed the P1↔P5 symlink seam; RS-05/RS-06/RS-14(loader) hold only inside `pkg/skills`, which neither consumer imports; RS-20 holds in its amended form below). Per §11.4.108 none is "done" until T-P9.02 re-observes it on a clean target.

**Two things were written, not merely planned**, and both are disclosed rather than
folded in: `.specify/memory/constitution.md` (the R-23 pointer — a governance document,
not production code, replaced because the stock placeholder was live and read by every
`/speckit-plan` run) and this `specs/` tree. **No production code was modified.**

**Where the plan is weakest**, stated plainly:

- **P2's cost is unknown.** Building a local SkillsBench analogue is the least-scoped
  phase here, and its effort is genuinely hard to estimate before the task set exists.
  It is nonetheless load-bearing: without it every activation threshold is a guess.
- **U-5 could reshape P6.** HelixAgent's `Toolkit/`, its 693 `MCP/` files and 21
  `mcp-servers/` directories were never surveyed. If a competing skill or tool
  abstraction lives there, P6's attachment point changes. P0 probes this first for
  exactly that reason.
- **A-2 is an assumption about write access**, not a verified fact. If upstream is not
  writable by this team, P3 is not an extension but a mirror, and the plan's shape
  changes at its root.
- **The `feature/*` branch reconciliation touches an upstream repository** whose review
  process is outside this plan's control.

Nine `UNCONFIRMED:` items are registered in `spec.md` §12, two of which were resolved
during planning (L6 → WIRED; the L5 substitute → no longer load-bearing). The rest
remain open with what would settle each.
