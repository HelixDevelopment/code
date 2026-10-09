# Data Model — Adaptive Local Model Serving

**Date**: 2026-09-02 | **Plan**: [plan.md](./plan.md) | Entities derive from spec.md § Key Entities.

Field-level notes carry the requirement that forced them, so a reviewer can trace each field to a
reason rather than inferring intent.

---

## Host Capability Profile

What a candidate serving machine can currently support. Produced by measurement, never configured.

| Field | Notes |
|---|---|
| `host_identity` | Stable identity of the machine, used in the naming scheme (FR-014). |
| `cpu` | Core count and instruction-set features that gate CPU-served options. |
| `memory_total` / `memory_available` | Total vs **currently available**; selection uses available. |
| `accelerators[]` | Zero or more. Empty is a valid, first-class state — not an error (FR-002). |
| `accelerators[].identity` | **Stable device identity — never an enumeration index (§11.4.111).** A second card or a boot-order change silently re-points an index binding. |
| `accelerators[].memory_total` / `memory_available` | Usable accelerator memory, not nameplate. |
| `accelerators[].api` | Which acceleration interface this device offers (CUDA / Metal / ROCm). |
| `storage_available` | **Independent of memory (D2).** A model's disk footprint is not implied by its memory figure. |
| `measured_at` | Freshness. A stale profile must not silently drive a fresh selection. |
| `measurement_complete` | If measurement failed or was partial, selection MUST refuse rather than fall back to a default (FR-056). |

**Validation**: an incomplete profile is not a profile. Consumers of it must distinguish
"no accelerator present" (valid, drives CPU-only offers) from "accelerator state unknown" (refuse).

## Model Option

A runnable choice, produced by joining the catalogue against a Host Capability Profile.

| Field | Notes |
|---|---|
| `identity` | `helixllm/<host>/<model>[:<variant>]` — a **value**, never a consumer identifier (D7, FR-014). |
| `derived_identifier` | Charset-safe identifier for a specific consumer, derived from `identity`. Mapping recorded so the two cannot drift (FR-014). |
| `family` | Capability family — text, vision, image-gen, STT, TTS, audio, vector, embedding. |
| `description` | Human-meaningful, composed from catalogue + measurement — not a fixed string (CONST-046). |
| `memory_required` | Checked against `memory_available`. |
| `storage_required` | Checked against `storage_available` — **separately** (D2). |
| `requires_accelerator` | Whether any accelerator is mandatory for acceptable service. |
| `usage_terms` | The terms the model may be used under. Gates offers (FR-054, D4). |
| `runtime` | Which runtime serves it (in-memory or streaming). |
| `streaming_eligible` | Roster membership, **not** an architecture predicate (D1). |
| `expected_capability` | What the user gets, in comparable terms (FR-005). |
| `integrity_expectation` | Expected value the weight file is verified against before load (SC-011). |

**State**: an option is `offered`, or `withheld` with exactly one reason from
`{insufficient_resources, unsupported_configuration, excluded_by_usage_terms}` — these have
different remedies and must not collapse into one generic unavailability (FR-055, D6).

## Model Selection

A user's chosen set of options and their current state.

| Field | Notes |
|---|---|
| `selected[]` | Chosen options and the host each runs on. |
| `pinned[]` | Deliberate user pins. **A pin constrains selection; it never bypasses measurement** — refused with the insufficient resource named if the host cannot run it (FR-056). |
| `declared_usage` | How output will be used. Drives the `usage_terms` filter (FR-054). |
| `running_state` | Per selection: not started / starting / serving / stopped, with the reason on failure. |

## Serving Instance

A reachable provider of models — this host, the local network, or remote.

| Field | Notes |
|---|---|
| `endpoint` | How to reach it. |
| `reachability` | local-host / local-network / remote. |
| `trusted` | An instance that cannot present the pre-shared secret is never trusted as a model source and never receives request content (FR-024/FR-025). |
| `served_options[]` | What it currently serves — one consumer entry per model per host (D8). |
| `health` | Liveness, so a dead instance is not exported as available. |

**Secret handling**: supplied via environment or `.env`, never logged, never committed (§11.4.10).

## Consumer Configuration

The provider definition handed to a consuming tool.

| Field | Notes |
|---|---|
| `consumer` | Which tool this targets — their identifier rules differ and must be respected as they stand (FR-014a). |
| `identifier` | The `derived_identifier` for this consumer. Must satisfy that consumer's existing validation — **never** obtained by widening it (D7). |
| `model_field` | Carries the human-readable `identity`. |
| `endpoint` | Resolved from the Serving Instance. |
| `availability` | How this consumer represents an unavailable option, including the withheld reason. |

---

## Relationships

```text
Host Capability Profile ──measured──┐
                                    ├──> Model Option[] ──chosen──> Model Selection
Catalogue (roster, terms, sizes) ───┘                                      │
                                                                           v
Consumer Configuration[] <──exported── Serving Instance <──runs────────────┘
```

The join is one-directional: measurement and catalogue produce options; options never write back
into measurement. This keeps selection a pure function of (host, catalogue, declared usage) — the
property that makes it testable without hardware.
