# Implementation Plan: Adaptive Local Model Serving

**Branch**: `002-adaptive-local-model-serving` | **Date**: 2026-09-02 | **Spec**: [spec.md](./spec.md)
**Input**: Feature specification from `specs/002-adaptive-local-model-serving/spec.md`

## Summary

Replace static, environment-variable model selection with measurement-driven selection: probe the
host's real capabilities and headroom, offer only model options that host can actually serve well,
and propagate the chosen configuration outward through HelixAgent to every consumer (Claude Toolkit,
HelixCode, OpenCode) under a stable, recognisable naming scheme.

The technical approach is **extension, not replacement**, at three seams that already exist:

1. **Measurement + selection** — a new capability probe and catalogue feed the existing
   `vrambroker` lease mechanism, which already arbitrates accelerator memory by class.
2. **Naming emission** — `Brain.Models()` is the single point where model identity is published to
   every consumer; correcting it there propagates for free.
3. **Consumer export** — Claude Toolkit already detects HelixAgent/HelixLLM for a single model per
   host; it needs fanning out to one entry per served model per host, not rewriting.

## Technical Context

**Language/Version**: Go 1.26.1 (`helix_llm`, module `github.com/HelixDevelopment/HelixLLM`);
Go 1.26 (`helix_agent`); Python 3 for two existing model services (`services/imagegen`,
`container/whisper_stt_server.py`); Bash for Claude Toolkit provider export.
**Primary Dependencies**: internal `digital.vasic.*` submodules (config, eventbus, observability,
ratelimiter, i18n, mcp, embeddings, toon); llama.cpp incl. `libmtmd` for multimodal; diffusers +
Nunchaku (image generation); faster-whisper (speech-to-text). Colibri is NOT yet a dependency —
incorporating it is in scope.
**Storage**: Model weight files on local disk plus a catalogue describing them; a per-host
measurement record. No relational store is introduced.
**Testing**: Go standard `testing` across 189 existing test files, `testify` in 19; existing build
tags `integration`, `e2e`, `performance`, `monitoring` are the separation mechanism and are reused
rather than replaced.
**Target Platform**: Linux host primarily; macOS required by cross-platform parity (§11.4.81).
Accelerator paths: CPU-only, CUDA, Metal, ROCm.
**Project Type**: Multi-repo change — a service + CLI (`helix_llm`), a propagation layer
(`helix_agent`), and a shell-based exporter (Claude Toolkit). All three are owned submodules and
carry equal engineering obligation under CONST-051(A).
**Performance Goals**: Per spec Success Criteria — selection must not offer an option that cannot
meet its family's usable threshold on the measured host; measurement itself must be cheap enough to
run at every selection rather than being cached into staleness.
**Constraints**:
- Selection is measured, never configured (FR-056). No fixed fallback model.
- Naming is a value, never a consumer identifier; no consumer validation may be weakened (FR-014a).
- Usage terms gate offers (FR-054); non-commercial models withheld from declared commercial use.
- The non-empty-options guarantee holds per family, not universally (FR-002 scenario 2).
- Streaming-path eligibility is a named-roster check, not an architecture predicate.
- Memory and storage headroom are independent axes.

## Constitution Check

*GATE: Must pass before proceeding. Re-check after design phase.*

Evaluated against `constitution/Constitution.md` and repo-root `CLAUDE.md`. The file at
`.specify/memory/constitution.md` is a deliberate inheritance pointer carrying no principles of its
own (CONST-059), so it is not the gate source.

| Principle | Status | Notes |
|-----------|--------|-------|
| **CONST-051(A)** equal-codebase | PASS | Touches three owned submodules (`helix_llm`, `helix_agent`, `claude_toolkit`). All three get equal test, doc and review obligation — not "main repo plus two dependencies". |
| **§11.4.111** resolve-by-stable-name, not enumeration index | **NEEDS ATTENTION** | Directly load-bearing here. Accelerator selection MUST bind by stable device identity, never `device 0`. A second GPU or a boot-order change silently re-points an index binding. Design must make this explicit; see Complexity Tracking. |
| **§11.4.224** TDD, ≥85% coverage | PASS | Every task starts RED. The measurement layer is pure-function-friendly (host facts in, option set out), so coverage is achievable without hardware in unit scope. |
| **§11.4.169** mandatory test types | PASS | Unit / integration / e2e / stress / chaos / concurrency / memory / benchmark all apply. Hardware-dependent types SKIP-with-reason where no accelerator is present, never silently. |
| **§11.4.108** four-layer verification | PASS | Source → artifact → runtime-on-clean-host → user-visible. Each fix declares one machine-checkable runtime signature. |
| **§11.4.5 / §11.4.69** captured evidence | PASS | A selection PASS must cite a real measurement and a real served response, never a config read. Precedent exists: the vision path already carries a measured 4.14 GiB on-GPU capture. |
| **Rule 2** no mocks in production | PASS | Fake hosts are a unit-test fixture only. Integration and above measure real hardware. |
| **CONST-046** no hardcoded user-facing content | PASS | Refusal reasons ("host lacks X", "excluded by usage terms") are composed from measurement + catalogue data, not fixed English strings. |
| **§11.4.10** credentials | PASS | The pre-shared discovery secret comes from environment/`.env`; never logged, never committed. |
| **§11.4.74** catalogue-first | PASS | Extends existing `vrambroker`, `Brain.Models()`, and the toolkit's detector rather than reimplementing. Colibri is adopted as an external runtime, not rebuilt. |
| **§11.4.76 / §11.4.161** containers, rootless | PASS | Existing model services are already containerised; new serving paths use the containers submodule, rootless. |
| **§11.4.122** no silent removal | **NEEDS ATTENTION** | `config.yaml:155-166` declares `helix-llm`/`helix-debate` provider types that no Go code loads (`LLMConfig` has no `providers` field). Dead, but removal requires investigation + operator confirmation, not a quiet delete. |

**Gate result: PROCEED** — two NEEDS ATTENTION items, both tracked in Complexity Tracking with a
concrete discharge path. Neither is an unjustified violation.

## Project Structure

### Documentation (this feature)

```text
specs/002-adaptive-local-model-serving/
├── spec.md                 # 57 FRs, 20 SCs
├── plan.md                 # this file
├── research.md             # Phase 0 — consolidated decisions
├── data-model.md           # Phase 1 — entities
├── quickstart.md           # Phase 1 — runnable validation
├── contracts/              # Phase 1 — interface contracts
├── checklists/
│   └── requirements.md     # 16/16
└── research/               # raw evidence, 5 passes
    ├── 01-text-llm.md          02-vision-image.md
    ├── 03-speech-audio.md      04-runtimes.md
    └── 05-consumer-integration.md
```

### Source Code (repository root)

```text
submodules/helix_llm/               # module github.com/HelixDevelopment/HelixLLM
├── internal/
│   ├── capability/         # NEW — host measurement (CPU, memory, accelerator, storage)
│   ├── catalogue/          # NEW — model roster + usage terms + resource requirements
│   ├── selection/          # NEW — measured host + catalogue -> offered option set
│   ├── runtime/            # NEW — llama.cpp vs streaming-runtime decision + launch
│   ├── lifecycle/          # NEW — idle unload, eviction policy, never-evict-while-serving
│   ├── telemetry/          # NEW — per-model memory/latency/throughput; feeds current-measurement refusals
│   ├── failover/           # NEW — host lost mid-request; retry policy; user notification
│   ├── vrambroker/         # EXISTING — lease arbitration; selection feeds it
│   └── brain/              # EXISTING — Models() is the naming emission point (FR-014)
├── cmd/
│   ├── visiongen-boot/     # EXISTING — currently env-var selected; migrates to measured
│   ├── imagegen-boot/      # EXISTING — same migration
│   ├── videogen-boot/      # EXISTING — see Complexity Tracking (not yet in spec scope)
│   └── helixllm/           # EXISTING — serving entry point
├── services/               # EXISTING — imagegen, ocr, vectorize, videogen
└── container/              # EXISTING — whisper_stt_server.py (STT is NOT greenfield)

submodules/helix_agent/                 # propagation to upper layers
claude_toolkit/scripts/claude-providers.sh   # EXISTING detector -> fan out per model per host
```

## Execution Strategy

### TDD Requirements

Every task is RED-first on the pre-change artifact (§11.4.115), with a polarity switch so the same
test source becomes the standing regression guard once green. Specifically:

- **Selection logic** is a pure function of (measured host, catalogue, declared usage) → option set.
  This is the highest-value test surface and needs no hardware: fixture hosts drive it.
- **The refusal paths are tests, not afterthoughts.** "Host cannot serve this family",
  "excluded by usage terms", "pin refused, resource named" each get an explicit RED.
- **Hardware-dependent layers** (actual measurement, actual serving) SKIP-with-reason when the
  required accelerator is absent, and are tracked for promotion — never silently passed.

### Parallel Execution Opportunities

The three seams are genuinely independent and can proceed concurrently:

| Stream | Scope | Contends with |
|---|---|---|
| A | `capability` + `catalogue` + `selection` (pure logic) | nothing |
| B | `runtime` selection + Colibri incorporation | A, at the interface only |
| C | `Brain.Models()` naming + toolkit export fan-out | A, at the naming contract only |

Agreeing the two interfaces (option set shape, naming contract) up front lets all three run in
parallel. Hardware-touching validation is single-owner per device (§11.4.119).

### Human Checkpoints

- **Before Colibri is adopted as a dependency** — it is a new external runtime in the serving path.
- **Before the dead `config.yaml` provider block is touched** — §11.4.122 requires confirmation.
- **Before video generation is added to scope** — see Complexity Tracking.

### Review Gates

Independent review before build (§11.4.125/§11.4.142), iterated to a zero-finding GO (§11.4.134),
on the Fable substrate at xhigh effort (§11.4.209). Specific things this review must catch:

- any accelerator binding by index rather than stable identity (§11.4.111);
- any path where a configuration value ends up naming the model (FR-056);
- any widening of a consumer's identifier validation (FR-014a);
- any option offered whose usage terms exclude the declared usage (FR-054).

## Complexity Tracking

| Item | Why it is not simply "do it" | Discharge path |
|---|---|---|
| **Accelerator binding identity** (§11.4.111) | The obvious implementation binds `device 0`. A second card, a boot-order change, or a hot-plug silently re-points it — the exact failure §11.4.111 exists to forbid. | Bind by stable device identity (UUID/PCI address as the platform exposes); make the binding's runtime signature assert the bound identity, and test with the device order reversed. |
| **Dead `helix-llm` provider config** (§11.4.122) | `config.yaml:155-166` declares provider types nothing loads. It looks live and misleads. Deleting it silently is forbidden; so is leaving a decoy. | Investigate via git history how it became dead (§11.4.124), then ask the operator whether to wire or remove. Not bundled with other work. |
| **Video generation — RESOLVED, in scope** | Operator decision 2026-09-02: video generation is in scope for this feature. `services/videogen/` ships a real WAN 2.2 / LTX-Video diffusion path with its own captured-evidence harness at `docs/qa/phase4_videogen_20260707/` and a `.gitignore-meta/wan_ltx_gguf.yaml` weight-regeneration manifest — so this is a migration onto measured selection, not a new build. | Catalogue entries + a Phase 7 migration task, mirroring vision and image generation. Its existing analyzer harness is reused for captured evidence rather than rebuilt (§11.4.74). |
| **Operational packages added post-analysis** | The initial decomposition was derived from US1's needs, so eviction, telemetry and failover — specified in FR-030..033 and FR-044..050 — had no home in the structure. The task list then inherited the hole while looking complete: tasks that faithfully implement a plan cannot reveal what the plan omitted. | `lifecycle/`, `telemetry/` and `failover/` added above. FR-033 (refusals from *current* measurements) moves to Phase 2, because selection's correctness depends on it rather than on a start-up reading. |
| **Colibri has no Go binding today** | It is a C runtime with a closed 8-family model roster. Incorporation means a launch/lifecycle integration, not a library import. | Treat as a runtime process under the same lease + health discipline as the existing llama.cpp path; roster membership is catalogue data, not code. |

## Constitution Re-Check (post-design)

*Re-evaluated after Phase 0 + Phase 1. Only changed rows shown; all other rows hold as above.*

| Principle | Before | After | What discharged it |
|-----------|--------|-------|---------------------|
| **§11.4.111** resolve-by-stable-name | NEEDS ATTENTION | **PASS** | Design now binds explicitly: `Host Capability Profile.accelerators[].identity` is specified as a stable device identity with "never an enumeration index" stated at the field, and quickstart Scenario 5 tests it adversarially — with two devices present, reversing enumeration order must leave the binding on the same physical device. The failure mode is now a test, not a hope. |
| **§11.4.122** no silent removal | NEEDS ATTENTION | **NEEDS ATTENTION** (unchanged, correctly) | Design cannot discharge this — it is an operator decision by construction. Recorded in Complexity Tracking with an investigate-then-ask path; deliberately not bundled into other work. |

**Post-design gate result: PROCEED.** One item remains open, and it is open because it *should* be —
it needs a decision, not a design.

### What the design phase surfaced that the plan phase did not

- **Selection is a pure function**, and keeping the measurement→option join one-directional is what
  makes it so. That property is worth defending in review: it is the difference between a refusal
  surface that can be exhaustively tested from fixtures and one that needs hardware to exercise.
- **Refusals are the majority of the test surface.** Four of seven quickstart scenarios assert that
  the system says *no* correctly. A selection layer that cannot say no is not doing its job, so the
  refusal paths carry as much test weight as the offers.
