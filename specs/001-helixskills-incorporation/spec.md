# Feature Specification: HelixSkills incorporation into HelixCode and HelixAgent

**Feature Branch**: `001-helixskills-incorporation`
**Created**: 2026-07-29
**Status**: IMPLEMENTED (P0–P8, B1–B3 executed 2026-09-13→23); Phase P9 closure in progress — T-P9.01 independent review verdict recorded in `tasks.md` §P9 (2026-09-23). This line was stale ("implementation NOT started") until the T-P9.01 review corrected it (§11.4.186).
**Work item**: HXC-159 (Task · In-progress · High)
**Phase**: 2 — specification + implementation plan (Phases 1a inventory and 1b research/risk are COMPLETE)
**Revision**: 2
**Last modified**: 2026-09-23
**Author**: `(T1/main - claude1 - opus - xhigh)`
**Input**: Operator description — *"Fully incorporate into HelixCode and HelixAgent the HelixSkills System for all work with Skills: git@github.com:HelixDevelopment/skills.git, we MUST fully incorporate all HelixSkills power-features fully without nothing leftout!"* plus the 2026-07-29 Bridge addendum.

**Evidence base**: this specification is derived entirely from the committed Phase-1
research tree
`docs/research/fully_incorporate_the_helixskills_system_into_helixcode_heli_20260728T192622Z_2557683/`
(commits `019dcc86`, `c3f9689`). Every factual claim below traces to a cited document
section there. Nothing here was re-derived, and nothing here was assumed (§11.4.6).

---

## Table of contents

1. [Why this specification does not say "register everything"](#1-why-this-specification-does-not-say-register-everything)
2. [Scope decisions taken](#2-scope-decisions-taken)
3. [User Scenarios & Testing](#3-user-scenarios--testing)
4. [Requirements](#4-requirements)
5. [Complete power-feature inventory with per-feature disposition](#5-complete-power-feature-inventory-with-per-feature-disposition)
6. [Constitution-derived extension dispositions](#6-constitution-derived-extension-dispositions)
7. [Bridge layer dispositions](#7-bridge-layer-dispositions)
8. [Key entities](#8-key-entities)
9. [Success criteria](#9-success-criteria)
10. [Assumptions](#10-assumptions)
11. [Out of scope](#11-out-of-scope)
12. [UNCONFIRMED register](#12-unconfirmed-register)

---

## 1. Why this specification does not say "register everything"

The operator's instruction is *"all power-features fully without nothing leftout"*.
This specification honours that on the **feature** axis — every one of the 57
enumerated upstream capability rows carries an explicit disposition in §5, and every
one of the 23 constitution-derived extensions carries one in §6 — while **refusing**
the one reading of it that measurement says would damage the product.

**The measured finding (`04_deep_research.md` §5, `06_risk_register.md` R-19).**
Databricks, *More Skills, Worse Agents?* (arXiv:2605.24050v1, SkillsBench, 38 oracle
task/model pairs, 2 545 trajectories, Claude Haiku 4.5 + Sonnet 4.6):

| Active library size | Pass-rate change |
|---|---|
| 52 skills | −8 % |
| 102 skills | −14 % |
| 202 skills | −21 % |

Skill shadowing accounts for up to **68 %** of that degradation, while **context
overhead is statistically indistinguishable from zero**. The local corpus measured
in `05_catalogue_survey.md` is **1 174 `SKILL.md`** files in
`submodules/helix_agent/skills/` — **5.8× past the worst point ever measured**, on a
monotonically worsening curve.

Two consequences shape this entire specification:

1. **Mounting the whole library is the configuration the literature says degrades
   the agent** — and it would degrade it *while every unit, integration and gate test
   stayed green*, because pass-rate loss of this kind appears only in end-to-end task
   success. That is a false-green of exactly the class §11.4 exists to prevent.
2. **The intuitive mitigations fix the wrong variable.** Trimming prose, shortening
   `SKILL.md` bodies and lazy-loading are token-cost fixes; token cost is measured at
   zero. The damage is *semantic interference between competing `description`
   fields*, and Level-1 descriptions are always loaded and always compete.

Therefore: every feature is **incorporated and reachable**; **activation** is an
explicit, calibrated, per-consumer allowlist. "Nothing left out" applies to
capability. It cannot apply to simultaneous activation, and this specification says
so in the open rather than discovering it at runtime.

**The threshold is not chosen here.** Its magnitude on *this* corpus is unknown
(§11.4.6). Deriving it is a first-class deliverable — see **FR-019** and Phase P2 of
the plan — and no activation policy may be set before that curve exists.

---

## 2. Scope decisions taken

`03_consumer_integration_surfaces.md` §8 lists six decisions Phase 2 must take.
All six are taken here. Each is a decision, recorded with its forcing evidence, and
each is reversible only by an explicit amendment to this specification.

| # | Decision | Resolution | Forced by |
|---|---|---|---|
| D-1 | **Canonical skill schema** | **Agent Skills open standard** (`agentskills.io`) — YAML front-matter `name` + `description`, uppercase `SKILL.md`, name matches directory. HelixCode's `triggers`/`variables`/`requires_isolation` and HelixSkills' TOML become **profiled extensions** carried in a reserved namespace, never required for portability. Converters authored in both directions with round-trip tests. | `04` A1: Agent Skills is now an open cross-vendor standard with ~45 implementations. Choosing either proprietary dialect strands the other; choosing the standard strands neither and buys §11.4.228 cross-agent portability for free. |
| D-2 | **Which upstream layer is in scope** | **All three, in strict order: Layer 1 (registration) → Layer 3 (catalogue) → Layer 2 (graph service, as an out-of-process dependency only).** Layer 2 is **never** linked into either consumer's Go binary. | `02` §2 three-layer architecture; `03` §2 `internal/` visibility constraint; R-25 (`replace` ignored outside the main module). |
| D-3 | **How the contract crosses the module boundary** | **Over the wire (MCP), plus a thin pure-data `pkg/` loader.** No Go import of HelixSkills `internal/` from either consumer. | `03` §2 + §6 rank 4 and rank 8: `pkg/` or over-the-wire are the only two proven cross-module seams; `03b` §5 names the MCP-config path "the cleanest cross-consumer integration path in the entire analysis". |
| D-4 | **Direction of the `helix_agent` edge** | **HelixAgent does NOT depend on HelixSkills.** HelixSkills already declares `helix_agent` as *its* dependency. HelixAgent becomes a **client** of a skills source (registered external tool source / MCP client), never an importer. | `02` §4.2: upstream `helix-deps.yaml` declares `HelixDevelopment/HelixAgent` as a dependency of HelixSkills. Inverting it risks a cycle; CONST-051(C) forbids the nested chain that would result. |
| D-5 | **Fix `03` §7.1 / §7.2 first?** | **Yes — both are Phase P1 preconditions.** No exported `Skill` constructor blocks the loader seam; the discarded CLI dispatcher means auto-trigger is dead in the CLI surface where the runtime signature will be taken. | `03` §7.1 (`grep "func NewSkill\b"` → zero hits), §7.2 (`cmd/cli/main.go:1060` constructs and discards the dispatcher). |
| D-6 | **Does the 1 174-file corpus migrate?** | **NO — not in this feature.** It stays `vendored`-tier, registered-but-**unactivated**. Moving or removing it is a §11.4.122 event requiring explicit operator confirmation, which this specification does **not** presume. | R-28 + R-19 + §11.4.122. Extracting the corpus removes a tree from HelixAgent; the operator must be asked *before*, not told after. |
| **D-7** *(taken in P0, T-P0.03.2)* | **Which module identity does the skills seam bind to, given `dev.helix.code` is declared twice?** | **Rename the thin root module to `dev.helix.code/meta`. Bind the skills library to the inner module (`helix_code/`) and to `submodules/skill_registry` — never to the root.** Deferred, deliberately: whether the orphaned root stub is *deleted* (T-P0.03.4 forbids removal in this phase; the §11.4.124 record is captured so a later removal-only commit can cite it). | Measured, not inferred. The duplicate is a **live package collision**: `dev.helix.code/internal/theme` names two disjoint packages — a 204-byte, 5-constant stub at root vs the 52 KB real subsystem inner (`diff -rq` → **zero files in common**). Which one an importer gets depends only on which module directory the build starts from. The rename is safe because the root's *entire* package surface is that one stub and it has **zero importers**; and `git log` shows it is a survivor of `437714ac chore(meta): W7D — delete superseded zero-consumer root Go cluster (~2978 LOC)`, i.e. already-condemned code the sweep missed. Renaming the inner module instead would touch every import in the application. **Reversed by**: evidence that the root identity is load-bearing for a consumer outside the scope searched — no `go.work` exists anywhere and no importer was found, but `cli_agents/` (third-party, not ours to bind) was excluded. |

---

## 3. User Scenarios & Testing

Stories are ordered by priority. Each is independently testable and independently
valuable — implementing only P1 still yields a usable product increment.

### User Story 1 — A HelixCode user invokes a governance skill that was never manually installed (Priority: P1)

A developer working in a HelixCode checkout runs `helixcode skills list` and sees the
constitution's governance skills (e.g. `reporting-workable-items`) alongside the
built-ins. They run the skill and it executes. They never symlinked anything, never
edited a config, and never learned where `constitution/skills/` lives.

**Why this priority**: it is the smallest slice that delivers the operator's actual
goal — skills authored once, reaching a consumer automatically — and it exercises
registration, namespacing, the activation allowlist and the loader end to end.
Today **not one** of the seven constitution skills is registered in this checkout
(`03b` §3.1), so this story currently fails.

**Independent Test**: on a clean checkout, run the registrar, then
`helixcode skills list`; assert a constitution-sourced skill appears and that
invoking it produces its rendered output.

**Acceptance Scenarios**:

1. **Given** a clean checkout with `constitution/` at the root pin, **When** the
   registrar runs, **Then** `helixcode skills list` includes each allowlisted
   constitution skill under its namespaced name and excludes every non-allowlisted one.
2. **Given** the registrar has already run, **When** it runs a second time, **Then**
   the resulting `.claude/skills/` tree is byte-identical (idempotent, §11.4.50).
3. **Given** a skill whose name collides with an existing entry the registrar did not
   write, **When** the registrar runs, **Then** it **aborts with a typed error** and
   changes nothing — never overwrites (fail-closed).
4. **Given** a directory containing `register.sh` but no `SKILL.md`, **When** the
   loader runs, **Then** it fails loudly with that path named — never silently skips.

---

### User Story 2 — A HelixAgent operator sees the same skill set over HTTP (Priority: P1)

An operator queries HelixAgent's `GET /v1/skills` and receives the same allowlisted
skill set HelixCode exposes, from the same source of truth, with identical names.

**Why this priority**: cross-consumer parity is an explicit operator requirement and
an acceptance criterion. A capability landing in one consumer and not the other is an
incompleteness to be tracked, not silently accepted.

**Independent Test**: start HelixAgent, `GET /v1/skills`, diff the returned name set
against HelixCode's `helixcode skills list` output; assert the sets match modulo
explicitly consumer-optional entries.

**Acceptance Scenarios**:

1. **Given** both consumers on the same source pin, **When** each is queried,
   **Then** the two capability reports differ only in entries the manifest explicitly
   marks consumer-optional.
2. **Given** a skill registered in one consumer only, **When** the parity gate runs,
   **Then** it FAILS and names the asymmetric entry.
3. **Given** HelixAgent, **When** its module graph is inspected, **Then** it contains
   **no** dependency edge on HelixSkills Go code (D-4 edge direction preserved).

---

### User Story 3 — Adding a skill cannot silently degrade either agent (Priority: P1)

A maintainer adds skills to an activated set. Before the change can land, the
shadowing benchmark re-runs and the activation gate either passes or refuses the
addition, naming the offending skill pair.

**Why this priority**: this is the only defence against R-19, the single most
dangerous item in the register, and it must exist **before** any activation policy is
chosen — otherwise the threshold would be guessed (§11.4.6).

**Independent Test**: run the local benchmark at increasing active-library sizes and
capture the pass-rate curve; then add a skill whose `description` closely paraphrases
an active one and assert the disambiguation gate FAILs.

**Acceptance Scenarios**:

1. **Given** the benchmark corpus, **When** it runs at N ∈ {baseline, …, ceiling+k},
   **Then** a pass-rate-per-size curve is emitted as a captured artefact.
2. **Given** an activation set exceeding the calibrated ceiling, **When** the loader
   starts, **Then** it **refuses to activate** and names the offender.
3. **Given** two active skills whose descriptions exceed the calibrated similarity
   threshold, **When** the disambiguation gate runs, **Then** it FAILs naming both.
4. **Given** the ceiling is raised past the measured knee (paired §1.1 mutation),
   **When** the benchmark re-runs, **Then** it MUST show the regression.

---

### User Story 4 — A skill from an untrusted source cannot silently gain capabilities (Priority: P2)

A skill arriving from a `third-party`-tier source declares no filesystem or network
capability. At load time it attempts one. The loader refuses.

**Why this priority**: `04` A4 established that skill content is executable
instruction, that **36.8 %** of published skills carry flaws, and that the ecosystem
state of the art is *"No code signing. No security review. No sandbox by default."*
HelixAgent's `AllowedTools` is parsed today and enforced **nowhere** (`03` §7.6).

**Independent Test**: register a fixture skill declaring no capabilities, have it
request one, assert a typed refusal and a captured audit record.

**Acceptance Scenarios**:

1. **Given** a skill with an undeclared capability, **When** it loads, **Then** the
   loader **refuses** (not warns) and records the refusal.
2. **Given** any skill containing `!`-dynamic-context or `allowed-tools`, **When** it
   is registered, **Then** registration requires an explicit review marker; absent it,
   registration fails.
3. **Given** a pinned skill whose content changes upstream, **When** the content-hash
   check runs, **Then** it FAILs (the `postmark-mcp` defence: 15 benign releases, then
   a backdoor).

---

### User Story 5 — A maintainer runs the SpecKit → Superpowers workflow through the Bridge (Priority: P2)

A maintainer drives this feature's own implementation through
`/speckit-specify → /speckit-plan → /speckit-tasks → /speckit-implement`, with the
discipline layers actually present rather than assumed.

**Why this priority**: mandated by the operator addendum. It is P2 not P1 because
**5 of the Bridge's 7 layers are absent today** and one of them does not exist as
software at all (§7) — so the deliverable is the *installer and an honest layer
report*, not the layers themselves.

**Independent Test**: run the layer-presence probe; assert it emits a machine-readable
report; assert any phase declaring a dependency on an absent layer FAILs.

**Acceptance Scenarios**:

1. **Given** the probe, **When** it runs, **Then** it emits per-layer
   present/absent with the captured probe command — never an inferred verdict.
2. **Given** a phase declaring a dependency on an absent layer, **When** the gate
   runs, **Then** it FAILs loudly rather than degrading silently.
3. **Given** an already-initialised L2 (`.specify/` + `speckit-*` skills present),
   **When** the Bridge installer runs, **Then** it is idempotent and does not clobber
   or double-install.

---

### Edge Cases

- A skill exists in `constitution/skills/` with a `register.sh` but **no** `SKILL.md`
  (measured: `multitrack` — R-05) → hard error naming the path, never a silent skip.
- `SKILL.md` filename case is split across the corpus (R-05) → the loader is
  **case-exact**; a case-insensitive filesystem must not be able to fake a pass.
- A tracked symlink resolves to a foreign-OS absolute path (measured:
  `skills/media-validator` → `/Volumes/T7/…`, a macOS path on a Linux host — R-06) →
  banned by gate; repo-relative only.
- Two registrars (SpecKit's and the constitution's) write into `.claude/skills/`
  (R-22) → reserved disjoint prefixes; pre-write collision check aborts. **Measured
  upstream behaviour: Claude Code neither errors nor warns on a skill-name collision —
  it resolves silently**, so the failure mode is silent shadowing, and the upstream
  issue requesting plugin-skill namespacing was **closed as not planned**. The risk is
  nonetheless *narrower* than first feared, because Superpowers installs as a
  namespaced plugin and SuperB as a spec-kit extension (`/speckit.superb.*` commands,
  not `.claude/skills/` entries). The genuinely uncovered case — and a
  **`NO external solution found — original work`** declaration per §11.4.8 — is **two
  independent installers provisioning into one `.claude/skills/` tree**: a *filesystem*
  collision resolved before Claude Code ever reads the directory. Nothing upstream
  provides a detector, so **this feature MUST ship its own install-time collision
  detection** (FR-014 applied at the registrar layer, T-P1.04).
- `.claude/skills/` is gitignored (`01` §7.1) → a fresh clone has `.specify/` but no
  skills; the registrar is the documented §11.4.77 regeneration mechanism.
- The upstream skills repo has **no release tags** (`02` §3) → pin by raw SHA on
  `main`, never a bare branch name.
- Two `constitution/` trees would exist if `skills` were added naively, pinned 46
  commits apart with measurable payload difference (R-02/R-03) → single-root layout;
  the nested pin is never initialised.
- Both consumers have **no skill persistence at all** (`03` §7.9) → usage telemetry
  is lost on restart; this specification does not add persistence to the consumers,
  it makes the *source* authoritative.
- Root and inner Go modules both declare `dev.helix.code` (R-26) → the skills seam
  must not be built on that ambiguity.

---

## 4. Requirements

### Functional requirements — source and layout

- **FR-001**: The system MUST consume `HelixDevelopment/skills` pinned to an explicit
  **raw SHA** on `main` with `branch = main` recorded, never a bare branch name
  (no release tags exist upstream).
- **FR-002**: The system MUST place the dependency at `submodules/skills/`, the
  grouped layout already used by 20+ existing entries.
- **FR-003**: The nested `constitution` submodule inside `skills` MUST NOT be
  initialised. Exactly **one** `constitution/` gitlink may exist in the whole tree.
- **FR-004**: `skills` MUST resolve its constitution through a **config-injected**
  path (env var, defaulting to a documented relative location) — never a nested
  checkout — keeping it project-not-aware per §11.4.28(B).
- **FR-005**: The upstream Go module MUST be promoted from
  `docs/research/mvp/Agent_AI_Skill_Tree_Development/project/` to the repository
  root, with its declared module path reconciled against the repository URL (they
  currently disagree in both name and case), plus a root `helix-deps.yaml` and
  `CONSTITUTION.md`. This is an **upstream extension**, never a HelixCode-local fork.
- **FR-006**: `feature/testing-infra` (4 stress/chaos/fuzz suites + 28 HelixQA
  entries) MUST be merged upstream before or as part of the incorporation — taking
  `main` alone inherits a *weaker* test posture than already exists.

### Functional requirements — schema and corpus

- **FR-007**: The canonical schema MUST be the Agent Skills open standard (D-1).
  Both other dialects MUST have converters with **round-trip** tests in both
  directions.
- **FR-008**: Every registered source directory MUST contain an exact-case
  `SKILL.md`. A directory with `register.sh` and no `SKILL.md` is a **hard error**.
- **FR-009**: The corpus case split MUST be normalised upstream (`skill.md` →
  `SKILL.md`) with every reference updated in the same commit (§11.4.29).
- **FR-010**: The missing `multitrack/SKILL.md` MUST be **authored**, not deleted —
  it has a `register.sh`, so it is wired, not dead (§11.4.124).
- **FR-011**: No tracked symlink may be absolute or unresolvable.

### Functional requirements — registry, loading and activation

- **FR-012**: A **single authoritative registry** MUST declare typed sources
  (name, root, format dialect, precedence, trust tier). Trees are never merged
  blindly.
- **FR-013**: Every skill MUST be namespaced `<source>.<name>`, so growth in one
  source cannot retroactively shadow another.
- **FR-014**: On a name collision the loader MUST return a typed error and **refuse
  to start** — never last-writer-wins.
- **FR-015**: Registration MUST be an explicit **allowlist**. The default active set
  is **empty**. A directory walk MUST NOT imply activation.
- **FR-016**: The loader MUST enforce a **hard active-count ceiling** at load time
  and name the offending skill when exceeded.
- **FR-017**: A **description-disambiguation gate** MUST compute pairwise similarity
  across active descriptions and FAIL above the calibrated threshold.
- **FR-018**: The ceiling (FR-016) and threshold (FR-017) MUST be **calibrated on this
  project's own fixtures**. Borrowing the paper's numbers is forbidden (§11.4.6).
- **FR-019**: A **local shadowing benchmark** — a fixed task set with known-correct
  skill selection, run at increasing active-library sizes — MUST exist and emit a
  pass-rate-per-size curve as a captured artefact. It re-runs on every corpus change
  as a performance-type test.
- **FR-020**: The loader MUST be **GUI-free by construction** (no Fyne/GL/X11 in its
  dependency graph) so the entire suite runs headless — the host lacks X11/GL headers
  (R-09).
- **FR-021**: The loader MUST be **pure-data**. Go's `plugin` package is excluded by
  its own documentation (platform-limited, single-build-unit).

### Functional requirements — security

- **FR-022**: Every source MUST carry a trust tier: `first-party` / `vendored` /
  `third-party`.
- **FR-023**: Each skill MUST declare required capabilities (filesystem, network,
  shell). The loader MUST **refuse** an undeclared capability, not warn.
- **FR-024**: Skills shipping executable scripts MUST be enumerated in the extensions
  catalogue so the executable surface is reviewable as a **set**.
- **FR-025**: Provenance MUST be pinned by **content hash**, so a skill's text cannot
  change under a fixed pin.
- **FR-026**: Shell execution from skill dynamic-context MUST be **disabled by
  default**; `allowed-tools` and `!`-dynamic-context are review-gated privileged
  constructs.
- **FR-027**: The existing constitution credential scanner MUST be reused unchanged —
  HelixSkills genuinely handles GitHub tokens, LLM provider keys, a Postgres DSN and
  an API-key middleware.

### Functional requirements — consumers

- **FR-028**: HelixCode MUST expose registered skills through its existing
  `helixcode skills` command group and its dispatcher-driven surfaces.
- **FR-029**: HelixCode's dead CLI auto-trigger MUST be repaired — the dispatcher is
  currently constructed and discarded, so auto-trigger works in TUI and desktop but
  not CLI.
- **FR-030**: HelixCode MUST expose a constructor for its skill type; today no
  `*Skill` can be built from outside its package except by writing a file to disk.
- **FR-031**: HelixCode's CLI and TUI MUST agree on the user skills directory (they
  currently disagree, so a skill installed for one surface is invisible to the other).
- **FR-032**: HelixAgent MUST consume skills as a **registered external source**, and
  MUST NOT acquire a Go dependency on HelixSkills (D-4).
- **FR-033**: HelixAgent's `AllowedTools` MUST become **enforcing** — it is parsed
  today and enforced nowhere.
- **FR-034**: HelixAgent MUST NOT silently drop malformed skills; a parse failure MUST
  be operator-visible.
- **FR-035**: A **shared conformance suite owned by the library** MUST be executable by
  both consumers, each publishing a capability report; a parity gate diffs them and
  FAILs on unexplained asymmetry.

### Functional requirements — governance, docs and tracking

- **FR-036**: `.specify/memory/constitution.md` MUST carry an inheritance pointer to
  `constitution/Constitution.md` and MUST NOT contain independent principles
  (CONST-059). *(Satisfied 2026-07-29 — see §12 disclosure.)*
- **FR-037**: `scripts/register_skills.sh` and `scripts/register_mcp.sh` MUST exist,
  be idempotent, manifest-driven and prefix-scoped. §11.4.164's auto-propagation
  currently calls consumer registrars that **do not exist**.
- **FR-038**: `.mcp.json` skill entries MUST be **generated** from the registry
  manifest, not hand-authored, and diff-checked in a gate.
- **FR-039**: `docs/extensions/EXTENSIONS_CATALOG.md` MUST exist with per-platform
  compatibility declarations (§11.4.228); it does not exist in HelixCode today.
- **FR-040**: `speckit-taskstoissues` MUST be excluded from the registered manifest or
  re-pointed at the workable-items DB — it pushes tasks to GitHub Issues and would fork
  the §11.4.93/§11.4.95 single source of truth.
- **FR-041**: A submodule-onboarding gate MUST assert the whole governance checklist
  at once, so partial onboarding is impossible.
- **FR-042**: Module paths MUST be unique across the repo; root and inner modules both
  declaring `dev.helix.code` MUST be resolved explicitly, not left to resolution order.
- **FR-043**: All documentation MUST be updated (not merely added), exported in the
  mandated formats, carry the produced diagrams, and be reachable from the main README
  (§11.4.65 / §11.4.153 / §11.4.212).

### Functional requirements — Bridge

- **FR-044**: `constitution/scripts/extensions/install_speckit_superpowers_bridge.sh`
  and `validate_bridge.sh` MUST be **authored** — they are named by the Bridge design
  and neither they nor their parent directory exist. Their scope is **L3 + L4 only**
  (the absent-but-obtainable layers); they MUST NOT attempt L5 (FR-047).
- **FR-045**: The installer MUST be **idempotent over an already-initialised L2** —
  `.specify/` and the ten `speckit-*` skills are already present from `specify init`,
  and L2 initialisation MUST NOT be re-planned as an open task.
- **FR-046**: A **layer-presence probe** MUST emit a machine-readable per-layer report
  with the captured probe command, using the four-value vocabulary
  `WIRED / PARTIAL / DESIGNED-ONLY / NONEXISTENT`. Any phase declaring a dependency on
  a non-`WIRED` layer MUST fail. The probe MUST check **both system and user systemd
  scopes** — a system-scope-only probe produced a confident **false negative** on L6
  (all seven Helix units are user-scope), and that trap MUST be encoded in the probe
  rather than remembered.
- **FR-047**: Layer 5 (SuperBridge MCP) MUST be **deleted from the architecture**, not
  installed and not stubbed. It does not exist as software — independently re-derived
  twice (`total_count: 0` on GitHub, zero npm MCP packages). No installer can help.
  The **substitution MUST be designed explicitly**: SuperB's own README states it
  *"owns no plan, task store, execution lifecycle, completion state, or convergence
  command"*, so the real execution tier is **Spec Kit + Superpowers + Claude Code's own
  skill/subagent machinery — already WIRED in this repository**. A 7-layer diagram with
  a phantom middle tier is a 6-layer architecture drawn wrong, and the corrected
  6-layer form MUST be recorded upstream in the Bridge design documents.
- **FR-048**: L3/L4 installation MUST go through an **explicit `--from` release URL**.
  The README's bare `specify extension add superspec` **FAILS on a default install** —
  spec-kit's first-party `catalog.json` holds only 4 entries, all `spec-kit-core`, and
  neither bridge is among them; they live in the *community* catalog.
- **FR-049**: Third-party Bridge dependencies MUST be tiered by **measured** health,
  and the tiering MUST be recorded where an implementer will see it:
  | Dependency | Measured | Risk | Disposition |
  |---|---|---|---|
  | `obra/superpowers` | Anthropic official marketplace; already installed here | LOW | **Adopt via the marketplace channel** (already satisfied) |
  | SuperB | 1 human + 2 of his own bots; **12 tagged releases**; **immutable release-asset `.zip`**; the only bridge shipping `tests/` | MEDIUM | **Adoptable, pinned to the immutable release asset** |
  | SuperSpec | **1 human, 10 lifetime commits, 0 watchers**, last push 2026-06-02; **v1.0.0 was broken** (v1.0.1 fixes an id that made installation fail); installs by **mutable tag-archive URL — no checksum, no signature** — dropping shell scripts an agent then executes | **HIGH** | **Not adoptable as-is.** Requires an org mirror plus immutable-artifact pinning, or is not adopted |
  | `erophames/superpowers-mcp` | no LICENSE file; dead ~4.5 months; **`git pull`s `obra/superpowers` daily via `execFile`, unsandboxed** | 🔴 **PROHIBITED** | **Never adopt.** An unattended auto-update of remote instructions the agent then executes is worse than having no L5 at all |
- **FR-050**: Provenance claims about the Bridge ecosystem MUST NOT be sourced from
  catalogue presence or org-styling. `speckit-community` is **a personal user account
  styled as an organisation** (`/orgs/` 404s; `type: User`, 4 repos, 3 followers), whose
  own site states it is *"not hosted, maintained, or affiliated with GitHub, Inc."*, and
  spec-kit states in its own words that maintainers *"do not review, audit, endorse, or
  support the extension code itself"* — **catalogue listing is schema validation, not a
  code audit**. Citing either as an authority is citing one individual.

### Non-functional requirements

- **NFR-001**: The loader's cold enumeration of all registered sources MUST complete
  within a documented budget measured on this host; the budget is set from a captured
  baseline, not asserted.
- **NFR-002**: Loading MUST be deterministic — running registrars in either order MUST
  produce byte-identical results (§11.4.50).
- **NFR-003**: The full loader test suite MUST run headless on this host (FR-020).
- **NFR-004**: No secret may appear in any manifest, catalogue, generated `.mcp.json`
  or captured evidence artefact (§11.4.10 / CONST-042).

---

## 5. Complete power-feature inventory with per-feature disposition

**Completeness basis** (`02` §11): full-tree AST enumeration of 237 Go files,
1 418 top-level declarations, **755 exported symbols**, 26 packages, **0 parse
errors**, cross-checked against independent sources. `02` treats the **57 enumerated
rows** as authoritative (37 distinct features after merging transport/serialisation
variants that share a parent). Every row below therefore has a disposition — silence
would be a §11.4.118 violation.

**Disposition vocabulary**

| Code | Meaning |
|---|---|
| **INC** | Incorporated — reachable from at least one consumer in this feature |
| **DEP** | Deployed as an out-of-process dependency; consumers reach it over the wire, never by Go import |
| **UPS** | Upstream-only — completed inside `HelixDevelopment/skills`, not surfaced to consumers here |
| **DEF** | Deferred with a named reason and a tracked follow-on item |
| **N/S** | Not in scope, with the reason recorded |

### 5.A Core graph engine (10)

| # | Feature | Upstream state | Disposition | Reason |
|---|---|---|---|---|
| A1 | Skill CRUD store | Complete | **DEP** | Requires PostgreSQL; consumers reach it via MCP (D-3). |
| A2 | Dependency-graph writes | Complete | **DEP** | Same. |
| A3 | Tree traversal / closure resolver | Complete | **DEP** | Same; surfaced through MCP tools. |
| A4 | Cycle prevention | Complete | **DEP** | Server-side invariant. |
| A5 | Granularity / composition | Complete | **DEP** | Server-side model. |
| A6 | TOML import/export round-trip | **Partial** — cross-skill edge-write unimplemented | **UPS** | Load-bearing for D-1 converters; the partiality must be closed upstream first. |
| A7 | Evidence attachment (10 methods) | Complete | **DEP** | Aligns with §11.4.5 captured evidence. |
| A8 | Resource attachment + revalidation | Complete | **DEP** | — |
| A9 | Registry health / coverage | Complete | **INC** | Feeds the capability report behind FR-035 parity. |
| A10 | Scheduled registry review | Complete | **DEF** | Scheduling overlaps the constitution `scheduled-work-queue`; §11.4.74 duplication decision deferred to a tracked item (see §6 row 5). |

### 5.B Search & embeddings (5)

| # | Feature | Upstream state | Disposition | Reason |
|---|---|---|---|---|
| B1 | Hybrid text search (pg_trgm) | Complete | **DEP** | — |
| B2 | Vector / semantic search (pgvector) | Complete | **DEP** | — |
| B3 | `Embedder` interface | Complete — extension point | **INC** | The consumer-supplied provider seam. |
| B4 | Batch embedding with progress | Complete | **DEP** | — |
| B5 | Write-through embedding cache | Complete | **DEP** | — |

### 5.C Intelligence pipelines (10)

| # | Feature | Upstream state | Disposition | Reason |
|---|---|---|---|---|
| C1 | LLM auto-expansion pipeline | Complete | **DEP** | — |
| C2 | Multi-provider LLM client | Complete — extension point | **INC** | Consumers supply their own provider; composes with CONST-036/039. |
| C3 | Validation pipeline | Complete | **DEP** | — |
| C4 | LLM jury validation | Complete — extension point | **INC** | Seam only. |
| C5 | Isolated/sandboxed execution | Complete — **default impl is a skip** | **UPS** | A skip-by-default sandbox cannot back FR-023/FR-026. A real executor must land upstream before any `third-party`-tier activation. |
| C6 | Tree-sitter code analysis | **Partial** — 4 native paths unimplemented | **DEF** | Not on the critical path; tracked. |
| C7 | Project analyzer → skill mapping | Complete | **DEP** | — |
| C8 | CodeGraph pattern extraction | Complete — extension point | **DEP** | Composes with §11.4.78–80 CodeGraph mandates. |
| C9 | CodeGraph index manager + sync | Complete — 2 extension points | **DEP** | Serves the §11.4.79/§11.4.80 index-sync obligation. |
| C10 | CodeGraph MCP client | Complete | **DEP** | — |

### 5.D Infrastructure (10)

| # | Feature | Upstream state | Disposition | Reason |
|---|---|---|---|---|
| D1 | PostgreSQL pool + tx | Complete | **DEP** | — |
| D2 | Cache abstraction | Complete — extension point | **DEP** | — |
| D3 | Prometheus metrics | Complete | **DEP** | — |
| D4 | Per-tenant metrics | Complete | **DEP** | — |
| D5 | Audit log (10 functions) | Complete | **DEP** | Backs FR-023's refusal records. |
| D6 | Multi-tenancy | Complete | **DEP** | — |
| D7 | Tenant audit logger | Complete — extension point | **DEP** | — |
| D8 | Background worker (6 job types) | Complete | **DEF** | Same scheduling-duplication decision as A10. |
| D9 | TOON serialization | Complete | **N/S** | No consumer requirement identified; recorded rather than omitted. |
| D10 | TOML config + `${VAR}` interpolation | Complete | **DEP** | The `${VAR}` form is how FR-027 keeps secrets out of files. |

### 5.E Ingestion & sourcing (10)

| # | Feature | Upstream state | Disposition | Reason |
|---|---|---|---|---|
| E1 | Skill-source registry | Complete | **INC** | Directly realises FR-012 typed sources. |
| E2 | Sync orchestrator (3 seams) | Complete | **DEP** | — |
| E3 | Source events | Complete | **DEP** | — |
| E4 | Generic `Source` abstraction | Complete — extension point | **INC** | How `constitution/skills/` and the vendored corpus register as sources. |
| E5 | Filesystem source | **Partial** — `Watch` unimplemented | **INC (degraded)** | Enumeration is what FR-012 needs; hot-reload-by-watch is **DEF** with the gap stated, never implied working. |
| E6 | GitHub source client | Complete | **DEP** | Token handling is exactly why FR-027 reuses the credential scanner. |
| E7 | Ingestion pipeline | Complete | **DEP** | — |
| E8 | `SKILL.md` parser | Complete | **INC** | The canonical-schema reader behind D-1. |
| E9 | Source → model mapper | Complete | **INC** | Half of the FR-007 converter pair. |
| E10 | Dedup classifier | Complete | **INC** | Supports FR-014 collision handling and R-04 reconciliation. |

### 5.F Agent-facing surfaces (12)

| # | Feature | Upstream state | Disposition | Reason |
|---|---|---|---|---|
| F1 | MCP server (13 tools) | Complete | **DEP** — *primary integration surface* | `03b` §5: the cleanest cross-consumer path; requires no Go coupling in either consumer. |
| F2 | MCP stdio transport | Complete | **DEP** | The transport the generated `.mcp.json` entries use. |
| F3 | MCP HTTP transport | Complete | **DEP** | — |
| F4 | ACP adapter | Complete | **DEP** | Composes with CONST-040. |
| F5 | Agent config emitters | Complete | **INC** | Directly serves §11.4.228 cross-agent portability. |
| F6 | MCP system prompts | Complete | **DEP** | — |
| F7 | REST API (12 handlers) | Complete | **DEP** | — |
| F8 | HTTP middleware stack | Complete | **DEP** | — |
| F9 | HTTP/2 + HTTP/3 + Brotli | Complete | **N/S** | Transport tuning; no consumer requirement. Recorded, not omitted. |
| F10 | Cobra CLI (27 subcommands) | Complete | **DEP** | Operator-facing, runs against the service. |
| F11 | Bubbletea TUI | Complete | **N/S** | Would add a competing TUI to a project that already ships one. |
| F12 | Catalogue generator (Layer 3) | Complete | **INC** | Generates HelixCode's own skills catalogue; §11.4.86 fingerprint discipline. |

### 5.G The 13 extension points

All 13 exported interfaces are **INC as seams** — they are the mechanism by which a
consumer plugs in without forking, and they are the strongest single argument for
reuse over reimplementation:
`api.AuditLogger`, `autoexpand.LLMClient`, `cache.Cache`, `codegraph.SkillSearcher`,
`codegraph.EvidenceStore`, `codegraph.SkillRegistry`, `db.Embedder`, `source.Source`,
`skillsource.SourceStoreReader`, `skillsource.SkillStoreWriter`, `skillsource.Fetcher`,
`validation.LLMValidator`, `validation.IsolatedExecutor`.

### 5.H Tally

| Disposition | Count |
|---|---|
| INC | 12 |
| DEP | 33 |
| UPS | 2 |
| DEF | 6 |
| N/S | 4 |
| **Total rows** | **57** |

Plus 13 extension-point seams (§5.G). **No row is undispositioned.**

---

## 6. Constitution-derived extension dispositions

The operator addendum requires that every constitution-derived extension carry an
explicit disposition — *"silence is not an answer"*. `03b` produced 23 rows; all are
carried forward unchanged, and each maps to a requirement or a named deferral here.

| Verdict | Count | Members | Where it lands in this spec |
|---|---|---|---|
| **REUSE** | 7 | `credential_scan_lib`, `guard-forbidden-commands`, `guard-track-branch-label`, `guard-branch-consistency`, `guard-work-track-binding`, SpecKit, Superpowers | FR-027 (scanner); the four guards apply unchanged to all work; SpecKit/Superpowers drive the workflow (US-5) |
| **EXTEND** | 1 | `actions/registry.yaml` | The *only* true extend. Any operator-facing skill directive is added as a **row** here — a data change, never new grammar in HelixCode |
| **COMPOSE** | 9 | `action-prefix-system`, `reporting-workable-items`, `scheduled-work-queue`, `workable-item-lifecycle`, `scheduled-work-mcp.json`, `helix` plugin, `scheduled-work` plugin, `action_prefix_expand.sh`, `post-merge` | Hand-off, not merge. `post-merge` is the §11.4.164 seam FR-037's registrars must be reachable from |
| **ORTHOGONAL** | 5 | `media-validator`, `multitrack`, `session-sync`, `media-validator-mcp.json`, `subagent_tiering.yaml` | Each carries an explicit reason in `03b` §4–§7; none is a silent omission |
| **BLOCKED** | 1 | Bridge L3/L4/L5 | §7 below |

**The one genuine duplication risk is scheduling** — the constitution's
`scheduled-work-queue`/`scheduled-work-mcp`/`scheduled-work` plugin versus HelixSkills'
6 cron job types (A10, D8). `03b` deliberately flagged this as a decision, not a
finding. **This specification defers it** to a tracked follow-on item rather than
inventing an answer: in-service cron jobs plausibly stay internal while
agent-triggered re-syncs plausibly surface as queue entries, but "plausibly" is not a
determination (§11.4.6).

**Two live defects in the registration path must be fixed before any of this
composes**: not one of the seven constitution skills is registered in this checkout,
and the single existing symlink is broken and points at a foreign OS (FR-011).

---

## 7. Bridge layer dispositions

Operator requirement: *"Make sure that SpecKit and Superpowers are used with Bridge
extension and with all extensions we have created derived from constitution
Submodule!"*

**Measured state (dedicated layer-health probe, 2026-07-29): `WIRED: L1, L2, L6` ·
`PARTIAL: L7` · `DESIGNED-ONLY: L3, L4` · `NONEXISTENT: L5`.** This is the §11.4.108
SOURCE-vs-RUNTIME gap in its purest form — the design corpus is real, complete and
high quality (8 documents × 4 formats), and that is **not** the same as the layer
being installed.

**The remedy is MIXED, not uniform.** This is the load-bearing correction: three
different absences need three different responses, and treating them alike would
produce an installer that cannot succeed.

| Layer | Design | Measured state | Remedy class | Disposition |
|---|---|---|---|---|
| **L1** Developer workstation | CLI agent + five carriers | **WIRED** | — | Reuse as-is |
| **L2** Spec-Kit core | `specify` + `.specify/` + 10 skills | **WIRED** — `.specify/` present with full scaffolding, 10 `speckit-*` skills registered | — | **Reuse now.** The layer this specification runs on. **Initialisation is DONE and MUST NOT be re-planned** (FR-045) |
| **L3** SuperSpec (orchestration) | `WangX0111/superspec` | **DESIGNED-ONLY** | **ABSENT-BUT-OBTAINABLE** — upstream is real and MIT; an installer genuinely fixes this | **Not adoptable as-is** — HIGH supply-chain risk (FR-049); requires org mirror + immutable pin |
| **L4** SuperB (discipline) | SuperB spec-kit extension | **DESIGNED-ONLY** — `specify extension list` → *"No extensions installed."* | **ABSENT-BUT-OBTAINABLE** — real, MIT, 12 tagged releases | **Adoptable, pinned to the immutable release asset** (FR-049) |
| **L5** SuperBridge MCP (execution) | MCP server, npm-built | **NONEXISTENT** | **NOT A PROCUREMENT PROBLEM — an installer cannot help; there is nothing to install** | **DELETE THE TIER** (FR-047) |
| **L6** Helix LLM (inference) | Gateway/Brain/Knowledge | **WIRED** — enabled, active, running, listening on `:8443` as a systemd **user** service | — | Reuse as-is |
| **L7** llama.cpp RPC cluster | distributed nodes | **PARTIAL** | **ABSENT-BUT-BUILDABLE** — `rpc-server` is a stock llama.cpp CMake target (`-DGGML_RPC=ON`); the submodule is already in-tree and already builds `llama-server` | A **build-flag task**, not dependency acquisition. Out of scope for *this* feature; tracked separately |

**Both L3/L4 installers are still unwritten**, as is their parent directory
`constitution/scripts/extensions/`. The gap is not "installed but stale" — the
installation path has never been authored, and FR-044 makes authoring it a deliverable
of this feature rather than an assumption of it. Critically, FR-048 records that the
README's bare `specify extension add superspec` **fails on a default install**: the
first-party catalogue carries only 4 `spec-kit-core` entries and neither bridge is
among them.

**On L5 — why deletion rather than substitution-in-place.** SuperB's own README states
it *"owns no plan, task store, execution lifecycle, completion state, or convergence
command."* So the execution role in the real architecture is filled by **Spec Kit +
Superpowers + Claude Code's own skill/subagent machinery — which is already WIRED in
this repository**. A 7-layer diagram with a phantom middle tier is a 6-layer
architecture drawn wrong. "Route through SuperBridge" is **unexecutable as written**,
and the corrected 6-layer form must be recorded upstream rather than worked around.

**A false negative worth encoding, not remembering.** L6 was initially read as absent
because `systemctl list-unit-files | grep -i helix` at **system** scope returns
nothing — all seven Helix units are **user**-scope. A system-scope-only probe yields a
*confident* wrong answer, which is precisely the §11.4.201 false-null class. FR-046
therefore requires the probe to check both scopes.

**The honest statement of what can be stood up**: L1, L2 and L6 today; L4 with an
immutable pin; L3 only behind an org mirror; L7 by a build flag when scheduled;
**L5 never, because it is not a thing**. Any plan phase routing work through L5 could
never honestly close.

---

## 8. Key entities

- **Skill** — a unit of agent instruction. Identity is `<source>.<name>`; `name`
  matches its directory; content lives in an exact-case `SKILL.md` with YAML
  front-matter. Carries a trust tier, a declared capability set and a content hash.
- **Source** — a registered corpus: name, root, format dialect, precedence, trust
  tier. The three measured corpora are `constitution/skills/` (7), the vendored
  HelixAgent tree (1 174), and the upstream graph (0 `SKILL.md` at root today).
- **Registry** — the single authoritative enumeration over all sources. Fail-closed
  on collision.
- **Activation set** — the explicit per-consumer allowlist of skills that are *live*.
  Distinct from the registry: everything is registered, few are active.
- **Capability report** — a consumer's published list of registered/active skills plus
  the loader API surface it guarantees. The parity gate diffs two of these.
- **Shadowing curve** — pass-rate per active-library-size, measured on this project's
  own fixtures; the calibration evidence for the ceiling and the similarity threshold.
- **Layer-presence report** — machine-readable per-Bridge-layer present/absent with
  the captured probe command.

---

## 9. Success criteria

Measurable, technology-agnostic, each provable by a captured artefact.

- **SC-001**: On a clean checkout, a user reaches an allowlisted governance skill from
  **both** consumers without any manual installation step. Today: **0 of 7**
  constitution skills are registered.
- **SC-002**: The two consumers' capability reports differ **only** in entries the
  manifest explicitly marks consumer-optional; the parity gate FAILs on any other
  asymmetry.
- **SC-003**: A pass-rate-per-active-size curve exists for this project's own corpus,
  and the activation ceiling and similarity threshold are **derived from it**, with the
  derivation shown. No threshold is asserted without its curve.
- **SC-004**: Adding a skill whose description paraphrases an active one FAILs the
  disambiguation gate, naming both.
- **SC-005**: Exactly **one** `constitution/` gitlink exists in the tree; the
  46-commit pin skew is structurally impossible, not merely currently absent.
- **SC-006**: Every registered source directory contains an exact-case `SKILL.md`;
  a directory with `register.sh` and none FAILs the gate.
- **SC-007**: No tracked symlink is absolute or unresolvable.
- **SC-008**: Running the registrars twice, in either order, yields byte-identical
  results.
- **SC-009**: A skill requesting an undeclared capability is **refused**, with the
  refusal captured.
- **SC-010**: The full loader suite runs green **headless** on this host.
- **SC-011**: The Bridge layer-presence report is emitted with per-layer captured
  probes, and no phase depends on an absent layer.
- **SC-012**: Every one of the 57 inventory rows and 23 extension rows carries a
  disposition; a coverage ledger shows feature × test-type × evidence state with no
  blank cells.
- **SC-013**: Every user-visible capability claim carries a §11.4.108 runtime
  signature verified on a clean target — never a source grep.
- **SC-014**: Independent code review reaches a zero-finding, zero-warning GO.

---

## 10. Assumptions

Each is an assumption, labelled as such, with what would falsify it.

- **A-1**: The operator accepts *capability-complete, activation-limited* as the
  correct reading of "nothing left out". **Falsified by**: an operator instruction to
  activate the full corpus — which would require re-opening R-19 with the curve in hand.
- **A-2**: Upstream `HelixDevelopment/skills` is writable by this team, so FR-005's
  module promotion is an upstream extension rather than a fork. **Falsified by**: a
  push rejection; the fallback is a tracked vendor-and-mirror path.
- **A-3**: PostgreSQL + pgvector are acceptable as an out-of-process dependency for
  Layer-2 features (DEP rows). **Falsified by**: a deployment constraint forbidding
  them; the fallback is that DEP rows become DEF and only INC rows ship.
- **A-4**: The measured 1 174-file corpus is representative enough that a benchmark
  built on it generalises to consumer workloads. **Falsified by**: a curve that is flat
  — which would itself be a valuable finding and must be reported, not buried.
- **A-5**: `.claude/skills/` remains gitignored (Option B of `01` §7.1), with the
  registrar as the documented §11.4.77 regeneration mechanism. **Falsified by**: a
  decision to track the directory, which changes FR-037's shape but not its existence.

---

## 11. Out of scope

Recorded explicitly so the boundary is visible rather than implied.

- Migrating or relocating HelixAgent's 1 174-file corpus (D-6) — a §11.4.122 event
  requiring operator confirmation this specification does not presume.
- Standing up L7 (llama.cpp RPC cluster).
- Adding skill **persistence** to either consumer. Both lack it entirely today; this
  feature makes the *source* authoritative instead.
- TOON serialization (D9), HTTP/3+Brotli tuning (F9) and the upstream Bubbletea TUI
  (F11) — each recorded with a reason in §5 rather than silently dropped.
- Replacing either consumer's existing tool or plugin abstractions. Three mutually
  incompatible plugin contracts exist across the two consumers; unifying them is a
  separate, larger piece of work.

---

## 12. UNCONFIRMED register

Per §11.4.6 — every item states what would settle it. None is filled with
plausible-sounding design.

| ID | Statement | What would settle it |
|---|---|---|
| ~~**U-1**~~ | ~~Whether **L6 Helix LLM** runs as a systemd service on this host.~~ **RESOLVED 2026-07-29 → WIRED.** Enabled, active, running, listening on `:8443` as a systemd **user** service. The earlier "not on PATH" reading was a **false negative** caused by probing system scope only. | Settled by a user-scope probe. The trap is now encoded in FR-046 rather than left to memory. |
| ~~**U-2**~~ | ~~Whether `HelixDevelopment/specifier` can fill the struck L5 execution role.~~ **NO LONGER LOAD-BEARING.** L5 is deleted rather than substituted (FR-047): the execution role is filled by Spec Kit + Superpowers + Claude Code's own skill/subagent machinery, all already WIRED. `specifier` remains unprobed and is now optional rather than blocking. | Would only need settling if the deletion decision were reversed. |
| **U-3** | Whether the shadowing effect's **magnitude** on this corpus matches the published curve. The *effect* is established; the local magnitude is not. | FR-019's benchmark. **No threshold may be set before this exists.** |
| ~~**U-4**~~ | ~~Whether the GitLab `HelixDevelopment` group holds a skills-related repo the catalogue survey missed (the query shape failed).~~ **RESOLVED 2026-07-29 → NOTHING MISSED.** The query failed because the GitLab group slug is **`helixdevelopment1`**, not `HelixDevelopment` (`groups/HelixDevelopment/projects` → `404 Group Not Found`; `groups?search=helix` → the real slug). Enumerated: 34 projects, exactly one skills-matching — `helixdevelopment1/helix_skills` — and `git ls-remote` shows **all 8 head SHAs byte-identical** to GitHub `HelixDevelopment/skills` (`main` = `315b56ce` on both). It is a **mirror**, not a second repo. `vasic-digital` (100 projects) yields only `caf-codex-skills`, already known and already excluded as a different repository. | Settled. |
| ~~**U-5**~~ | ~~Whether HelixAgent's `Toolkit/`, its 693 `MCP/` files and 21 `mcp-servers/` dirs carry a **competing** skill or tool abstraction.~~ **RESOLVED 2026-07-29 → none of the three competes, but D-3 must still re-open.** `Toolkit/` = `unrelated` (a two-provider LLM SDK; registry/client/executor for *providers*, `Toolkit/pkg/toolkit/toolkit.go:8-11`; **no `.go` outside the module imports it**). `MCP/` = `unrelated` (44 vendored third-party repos; the only skills content is upstream **SEP-2640**, marked *"Status: not yet implemented"* at `MCP/submodules/python-sdk/examples/stories/skills/README.md:7`). `mcp-servers/` = `complementary` (**19** dirs, not 21 — see U-10; they *carry* skills as assets, serve none; five ship a `skill-adapter/` scaffold with zero executable files). **The competing abstraction is elsewhere**: `submodules/helix_agent/internal/skills/` already IS what D-3 proposes to build, and a skills→MCP projection already exists but is **unwired** (`internal/skills/protocol_adapter.go:46,124,173,203,627`). See the D-3 re-opening below. | Settled for the three trees; D-3 re-opened explicitly rather than routed around. |
| ~~**U-6**~~ | ~~CONST-040 conformance: per-capability Skills/Plugins booleans could not be confirmed (only `CapabilityScore float64` was found), and HelixCode's embedded verifier comments that no runtime probe exists.~~ **RESOLVED 2026-07-29 → NO VIOLATION; flags exist and are sourced correctly.** Discrete booleans exist on both sides with identical JSON tags: `helix_code/internal/verifier/types.go:134-139` (`SupportsSkills`/`SupportsPlugins`) and upstream `llms_verifier/.../llmverifier/models.go:193-194`. They are populated by **real probes**, not defaults — `llmverifier/verifier.go:655-656` dispatching `TestSkills` (`:1551`) / `TestPlugins` (`:1586`), where `TestSkills` issues a live `ChatCompletion` and requires the model to reproduce a sentinel token. A **live verifier was captured**: `GET /api/models` → HTTP 200 carrying `"supports_skills"` / `"supports_plugins"` as first-class booleans (`qa-results/hxc159/const040/20260728T210957Z/verifier_response.json`), refuting the "only a float score" premise on the wire. `grep -rnE "Supports(Skills\|Plugins)\s*[:=]\s*true" --include=*.go . \| grep -v _test.go` → **zero hits**: no hardcoded flags in production code. The "no runtime probe" comment (`internal/verifier/embedded_server.go:153`) is **correctly scoped to the embedded fallback path only** and remains accurate; U-6 had lost that scoping. **HelixAgent has no Skills/Plugins capability sites at all** — a documented absence, not a violation. | Settled by schema read + live wire capture. One follow-on: stale doc comments at `types.go:45-53` and `types.go:124-133` now contradict the shipped code (see U-11). |
| **U-7** | Whether the six unread Bridge documents (NANO_TASK_ENGINE, EXTENSION_DEVELOPMENT, TDD_INTEGRATION, CONSTITUTION_INTEGRATION, SECURITY, APPENDIX) impose obligations not reflected in FR-044…FR-048. | Read all six before the Bridge installer is authored. |
| **U-8** | Whether the four remaining stale `skills` branches (`helix_skills`, three `worktree-agent-*`) contain anything not already in `main`. Each is measured 0 commits ahead, but their diffs were not read. **NARROWED 2026-07-29:** the full remote ref inventory is captured (`qa-results/hxc159/env_facts/20260728T211327Z/upstream_heads.txt`) and shows the **three `worktree-agent-*` refs share one identical SHA** (`25cb8ca0`), so this is **two** distinct diffs to read, not four: `25cb8ca0` and `helix_skills` @ `06d77bd8`, both against `main` @ `315b56ce`. Still open — no diff has been read. | `git log main..<ref>` for the two distinct SHAs — required by §11.4.124 before any retirement. Needs a fetch; not done in P0 because retirement is not a P0 action. |
| ~~**U-9**~~ | ~~The upstream module's true maturity — its path says `research/mvp` while its content is production-shaped. The naming and the maturity disagree.~~ **RESOLVED 2026-07-29 → the framing inverts: `research/mvp` is ACCURATE, and the outlier is one stale README line.** U-9 asked for *"an upstream decision recorded in the repo"*, and one exists: `CONTINUATION.md` (repo root, HEAD, Rev 12, 2026-07-18) §1 — *"The MVP skill-graph system … is in **active development**"* — and the research-area `CONTINUATION.md` — *"**PHASE:** P0.5 critical remediation (security + correctness) before feature phases."* The path and upstream's governance docs **agree**; the only contradicting text is `README.md:11` (*"is a production-grade Go application"*) whose build badge points at a URL that is not this repo. **Work-shape verdict: `relocate-and-reconcile`, not `rewrite`** — measured in a `/tmp` clean room, never in the shared tree: `go build ./...` → rc=0 with empty output, `go vet` → rc=0, four binaries link (server 57 MB, worker 31 MB, cli 12 MB, tui 11 MB), and `go test ./...` → rc=0 across 27 of 29 packages. Anti-bluff on that green suite: `-json` per-test verdicts are **950 pass / 119 skip**, the skips explicitly infra-gated on a PostgreSQL that was not booted — green by passing, not by skipping. Module-path coupling is **mechanical**: 139 files / 264 import lines, and **zero** non-`.go` files (Makefile, Dockerfile, yaml, sh) carry the path, so the rename is a substitution rather than a semantic edit. All four corrected Phase-1 findings CONFIRMED (237 tracked `.go`, **0 outside `docs/`** — verified across all 60 refs; no root `go.mod`, and `git check-ignore` rc=1 closes the gitignored-file hypothesis; module `github.com/helixdevelopment/skill-system` differs from canonical `github.com/HelixDevelopment/skills` in **name and case**; **0 tags**, with `heads_count=8` as the connectivity proof that the empty tag list is a real absence and not a silently-failed query). | Settled. Two carry-overs below (U-13, U-14) and a repo-name correction: the local checkout's remote is `HelixDevelopment/**helix_skills**`, which `gh` canonicalises to `HelixDevelopment/**skills**` — same repository via rename-redirect. **Pin `.gitmodules` to the canonical `skills` URL** so the pin does not depend on a redirect surviving. |
| **U-13** | Upstream's own gaps register is **internally stale**, so its headline cannot be quoted as fact. Its scope header (audit 2026-07-15) says *"53 `.go` files, 0 tests"* while HEAD (2026-07-18) has **237 `.go` and 145 test files**; its summary tables stack three mutually contradictory count blocks (`FIXED 155/TOTAL 157`, `FIXED 40/TOTAL 136`, `FIXED 43`), and G03 appears simultaneously as OPEN-CRITICAL and FIXED. The authoritative per-item `STATUS` lines show **all four CRITICALs closed** (G01 `FIXED 2026-07-18`, G02 `Fixed 2026-07-16`, G03 `FULLY LANDED 2026-07-17` — independently corroborated by importer counts, G04 refuted by direct measurement). **Do not cite "95 open / 2 CRITICAL" as a property of HEAD.** The 60+ HIGH items were not re-audited. | A per-item re-audit of the HIGH band against HEAD, before P3 treats any of them as open work. |
| **U-14** | Whether upstream's passing suite is **bluff-free**. 950 tests genuinely execute and pass, but **no paired §1.1 mutations were run**, so it is unproven that those assertions *can* fail. Separately, 119 DB-gated tests never ran against a real PostgreSQL and the 12 migrations were never applied. Also unassessed: architectural fit of upstream's data model to the `pkg/skills/` contract — this probe measured buildability and reuse **cost**, not fit. Governance note: 23 bare `t.Skip()` carry no reason and would fail this repo's `make no-silent-skips`. | P3's acceptance gate: paired mutations over the upstream suite, plus a migration run against a booted PostgreSQL. |
| **U-10** | The `mcp-servers/` count. The plan states **21** directories; a direct census (hidden entries included) measured **19**. Plausible-but-unverified reconciliation: the original count added `external/mcp-servers` + `docs/mcp-servers`. Nothing depends on the number, but an unreconciled count is a stale fact by §11.4.6. | The plan's own name list, or `git log --diff-filter=D -- mcp-servers/` to see whether two were deleted. |
| **U-11** | HelixCode's verifier doc comments at `internal/verifier/types.go:45-53` and `types.go:124-133` assert *"no populator in this codebase sets these fields yet"* and *"client.go's VerifyModel does a bare `json.Unmarshal`"*. **Both clauses are contradicted by the shipped code** (`client.go:432-443` does a struct-embedded decode plus alias reconciliation) and by the live wire capture under U-6. A stale honesty-comment that *understates* real capability is the mirror image of a PASS-bluff and should not drift. | Nothing — this is settled enough to act on. It needs a low-severity documentation `Task`, not further probing. Deliberately **not** filed here: `docs/workable_items.db` is a shared SSoT with six concurrent writers and tracker regeneration is DEFERRED by the coordinator, so this is disclosed rather than written (§11.4.6 over convenience). |
| **U-12** | `submodules/skill_registry`'s module identity cannot be fixed in isolation. Four names disagree: repo `vasic-digital/**SkillRegistry**`, module path `dev.helix.**agent**/skillregistry`, `helix_agent`'s replace key `digital.**vasic**.skillregistry` (`submodules/helix_agent/go.mod:279`), checkout path `submodules/skill_**registry**`. Reconciling the module path toward `digital.vasic.skillregistry` — which matches the org **and** every sibling replace in `helix_agent` — would repair `helix_agent`'s replace but simultaneously **break** `helix_llm`'s currently-*correct* one (`submodules/helix_llm/go.mod:108`). The two cannot be changed independently. | A coupled decision taken in the phase that adopts the module, editing both consumers' `go.mod` in one change. Not takeable in P0: it edits submodule source, and P0 is probes only. |

### D-3 is re-opened (T-P0.02.4) — the seam it proposes to build largely exists

P0 was asked to re-open D-3 explicitly if a competing abstraction turned up rather
than silently route around it. One did — **twice**, and neither instance is in the
three trees U-5 named.

**(1) `submodules/helix_agent/internal/skills/` already is the abstraction.** It is
not dormant: it is what backs the four live `/v1/skills` routes, through
`router.go:1361` → `router.go:341` → `router.go:333-334` → `service.go:64` →
`registry.go:79` → `parser.go:345`, which walks the tree for
`strings.ToUpper(info.Name()) == "SKILL.MD"` and loads **1 174** `SKILL.md` files
from `submodules/helix_agent/skills/` across 15 categories. Both of D-3's clauses
are affected: it is `internal/`-scoped rather than the "thin pure-data `pkg/`
loader", so either its pure-data half is lifted to `pkg/` or the seam is
duplicated; and a **skills→MCP projection already exists but is unwired**
(`internal/skills/protocol_adapter.go:46` `MCPSkillTool`, `:124`
`registerSkillAsMCPTool`, plus `:173`, `:203`, `:627`). P6's attachment point is
therefore plausibly *wire the existing adapter and repoint `SkillsDirectory`*,
not *build a new seam*.

D-3 also says "over the wire (MCP)" without saying **which** wire. That now needs
stating: MCP's own skills proposal (**SEP-2640**, `skill://index.json`,
`@skill`/`@skillDir`) is **unratified and unimplemented** upstream — blocked on
SEP-2133's `extensions` capability map. What is actually available today is
HelixAgent's skill-as-ordinary-MCP-tool projection. Building against SEP-2640
would be building against a proposal.

**(2) `submodules/skill_registry` is a working, tested skills module that nothing
requires** — a §11.4.74 catalogue hit against P4's plan to build `pkg/skills` from
scratch. `go test ./...` → `ok dev.helix.agent/skillregistry 0.121s`, exit 0,
unmodified. It ships `SkillManager` (Register/Get/List/Search/Filter/Enable/Disable/
Execute/**LoadFromDirectory**/**LoadFromFile**/metrics/handlers/hooks), `SkillExecutor`
(handler registry, pre/post hooks, timeout, bounded concurrency), `SkillValidator`
with dependency **cycle detection**, a `SkillStorage` interface with in-memory *and*
PostgreSQL backends, and i18n. Its `loader.go:26-46` dispatches on format and, for a
directory, looks for **uppercase `SKILL.md`** — precisely the D-1 canonical schema —
then unmarshals YAML front-matter into a `Skill` whose tags are `name`,
`description`, `version`, `category`, **`triggers`**, `tags`, `author`: the Agent
Skills open standard **plus** HelixCode's `triggers` dialect, already carried as a
first-class field. That is the D-1 union, already implemented.

**(3) And `pkg/skills` does not exist upstream to be relocated.** `ls
…/project/pkg` → no such file; zero tracked paths. Under U-9's
`relocate-and-reconcile` verdict for the *service*, the plan's "consumable
artefact" is the one genuinely **new-build** sliver — one small package out of 29
— and it should be a thin façade over the ~1 416 already-exported declarations,
not a re-implementation. Which sharpens the question P4 must answer: a façade over
*upstream*, or adoption of `skill_registry`, which already exposes that surface
and already passes its tests.

**Consequence for the plan, stated plainly:** across the two consumers plus this
module there are now **three** skills implementations that each parse `SKILL.md`.
The incorporation is materially more *reconcile-and-wire* than *build*. P4's design
must dispose of `skill_registry` explicitly — adopt, extend, or supersede with a
recorded reason — because building `pkg/skills` beside it without that disposal is
reimplementation where extension was available (§11.4.74). **No such disposal is
taken here**: P0 is probes and decisions, and this decision belongs to the phase
that owns the seam.

**How `skill_registry` became unrequired** (§11.4.124 investigate-before-remove,
recorded now so no later phase has to re-derive it — and note that **nothing is
proposed for removal**): `helix_agent` commit `415e138a` — *"complete extraction of
ToolSchema, SkillRegistry, ConversationContext modules"* — extracted it as
`digital.vasic.skillregistry` and left behind
`submodules/helix_agent/go.mod:279 replace digital.vasic.skillregistry => ../skill_registry`.
The extracted repository's `go.mod:1` declares a **different** path,
`dev.helix.agent/skillregistry`. A `replace` whose key matches no module path can
never bind; no `require` was ever added either, so the directive is doubly inert. A
second replace with the **correct** key exists at `submodules/helix_llm/go.mod:108`
(from `9a98f5a`), also without a `require`, also inert — evidence that consumption
was intended and that someone later got the key right. Meanwhile `helix_agent` kept
its own `internal/skills/`, so nothing ever depended on the extracted module and
nothing regressed. The extraction was simply never finished.

**Two further §11.4.124-class findings, recorded not acted on:** `SkillLoader`
(`internal/skills/loader.go:29`) has 7 call sites, **all inside `loader_test.go`** —
the live path bypasses it; and `ProtocolSkillAdapter` is constructed only in
`protocol_adapter_test.go` and a `doc.go:73` comment. Whether the adapter was ever
wired and then unwired is **UNCONFIRMED** — no `git log -S` history search was run
on it.

### Recorded contradiction — the work item's own environment facts are stale

HXC-159's `PRE-VERIFIED ENVIRONMENT FACTS` block states that SpecKit *"is NOT yet
initialized in this repo (no `.specify/` and no `specs/` directory present)"* and that
*"Initialization is therefore an explicit early task."* **That fact is now stale.**
`.specify/` exists with full scaffolding (mtime 2026-07-29 00:31) and ten `speckit-*`
skills are registered; the item's own later addendum says so explicitly. The tracker
therefore contradicts itself, and **the later addendum is correct**.

Consequence for this specification: **no L2 initialisation task is planned** (FR-045).
Recorded here rather than silently reconciled, because a plan that re-planned a
completed step would be doing work against a description instead of against the
artifact — the same failure mode §11.4.6 exists to prevent.

#### T-P0.01 re-measurement, 2026-07-29 — three more contradictions

The contradiction above is now machine-checked rather than remembered, by a
re-runnable probe: `scripts/probes/hxc159_env_facts.sh`, emitting
`qa-results/hxc159/env_facts/<run-id>/facts.json` (11 rows, `{assertion, stated,
measured, verdict, scope_searched, independent_methods}`). Re-run it at the start
of every phase — facts decay (T-P0.01.5). The first run
(`20260728T211327Z`, HEAD `833c8d51`) returns:

| Verdict | Rows | Detail |
|---|---|---|
| `CONFIRMED` | 5 | `specify` binary present; upstream reachable over SSH (8 heads, **0 tags**, default `main`); `HelixDevelopment/skills` genuinely not a submodule; both consumers declare the stated module identities |
| `STALE` | 3 | `.specify/` **PRESENT** (mtime 2026-07-29 00:31); `specs/` **PRESENT**; **10** `speckit-*` skills registered where the item implies zero |
| `CONTRADICTED` | 1 | *"the only 'skills'-matching `.gitmodules` entry is `cli_agents/codex-skills`"* — there are **two**. The second is `[submodule "dependencies/vasic-digital/skill_registry"]` → `path = submodules/skill_registry`, `url = git@github.com:vasic-digital/SkillRegistry.git` (`.gitmodules:393-395`). The item's own fact block missed the most directly relevant existing asset in the repo — the module the D-3 re-opening above turns on. |
| `GAP-IN-ITEM` | 1 | The item states the two consumers' module identities but is silent on the **thin root module**, which declares `dev.helix.code` — identical to the inner module (R-26). |
| `PARTIAL` | 1 | U-8 branch inventory captured; per-ref diffs still unread |

**A fourth contradiction, in this repo's own governance rather than in the item:**
`CLAUDE.md` §3.2 states the root holds `internal/{fix,security,testing,theme}` and
`cmd/security_test/`. Measured: the root holds **`internal/theme` only** — `fix` and
`security` live in the inner module, `testing` exists in neither, and there is no
root `cmd/` at all (`go list ./cmd/...` → `lstat ./cmd/: no such file or directory`).
Recorded, not corrected: `CLAUDE.md` is shared governance with six concurrent
agents live, and editing it is outside a probes-only phase.

### Disclosure — one file was written, not merely planned

`.specify/memory/constitution.md` was **replaced** during this planning phase, taking
FR-036 from planned to satisfied. Rationale: the file is a governance pointer, not
production code; the stock 2 346-byte placeholder was live on disk and is read by
every `/speckit-plan` Constitution Check run, so leaving it would have let a
placeholder shadow `constitution/Constitution.md` during this very feature's own
workflow. The unmodified SpecKit template remains at
`.specify/templates/constitution-template.md`. No other file outside `specs/` and this
research tree was modified, and **no production code was touched**.

---

## Sources

All facts trace to the committed Phase-1 research tree
`docs/research/fully_incorporate_the_helixskills_system_into_helixcode_heli_20260728T192622Z_2557683/`:
`01_speckit_init.md` (SpecKit install + Bridge conformance),
`02_upstream_feature_inventory.md` (57-row inventory, 3-layer architecture, branches,
buried module, `helix-deps.yaml` edge),
`03_consumer_integration_surfaces.md` (15 divergences, 8 ranked attachment points,
10 live defects, 6 Phase-2 decisions),
`03b_extension_integration.md` (23-row extension matrix, registration census),
`04_deep_research.md` (4 research angles, shadowing measurement, dependency health),
`05_catalogue_survey.md` (9 catalogue verdicts, corpus counts, required layout),
`06_risk_register.md` (29 risks, 27 evidenced, 5 critical, each with a designed
mitigation).

Runtime observables verified directly in this session (2026-07-29):
`helix_code/cmd/cli/main.go:3306` — `skills` subcommand dispatcher;
`submodules/helix_agent/internal/router/router.go:1363-1368` — `GET /v1/skills`,
`GET /v1/skills/categories`, `GET /v1/skills/:category`, `POST /v1/skills/match`.
