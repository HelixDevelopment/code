# Data Model — Provable Model Alias Availability

**Date**: 2026-09-08 | **Plan**: [plan.md](./plan.md) | **Spec**: [spec.md](./spec.md)

Derived from the spec's Key Entities, with fields and rules taken from the functional requirements
they must satisfy. Storage locations are recorded because the state is on disk, not in a database.

## Alias

The name an operator uses to select a model.

| Field | Type | Rules |
|---|---|---|
| `id` | string | Stable identifier. Also the record filename and the Kimi twin's suffix. |
| `key_var` | string | NAME of the credential variable — never the value (FR-019). Several aliases may share one. |
| `transport` | enum `native` \| `router` | Decides the wire; `native` iff the provider speaks the Anthropic API natively. MUST NOT be inferred from the URL's shape (FR-003). |
| `base_url` | string | Passed through verbatim. No `/v1` surgery (FR-003). |
| `model`, `fast_model` | string | The served model identifiers. |
| `state` | enum | Closed set: `verified`, `failed`, `orphaned`, `no-twin`. **Only `verified` is "presented as available"** (FR-001, Q1). |
| `layer` | string | Which check failed, when `state` is not `verified`. |

**Storage**: `~/.local/share/claude-multi-account/providers/<id>.env`.

**Pairing invariant (FR-002)**: an alias and its supporting configuration are created and removed
together. It MUST NOT be possible, through any path that creates, restores or refreshes them, to
reach a state where one exists without the other. The measured violation this feature repairs is 19
of 27 Kimi twins holding an alias with no `config.toml`.

**State transitions**: `(absent) → orphaned|failed|verified` on resolution + verification;
`verified → stale:verified` by the passage of time alone, with no re-probe (see Verdict);
`verified → failed` on a failing re-probe; any state `→ (absent)` only on explicit removal, which for
an operator-visible capability requires confirmation (§11.4.122).

## Verdict

A recorded judgement about one alias.

| Field | Type | Rules |
|---|---|---|
| `status` | enum | `verified`, `failed`, `pending`, prefixed `stale:` when past the horizon. |
| `checked_at` | ISO-8601 UTC | The moment the judgement was made. |
| `age` | derived | Rendered `42s`/`9m`/`5h`/`3d`, or `?` when `checked_at` is absent or unparseable. |
| `failing_layer` | string | Which check produced a non-`verified` status. |
| `evidence_path` | path | Points at the artifact behind the verdict (FR-008). |

**Storage**: `status.json`, one entry per alias id. Twins share one record — a claude alias and its
Kimi twin are two views of one verdict.

**Rules.** Age is derived, never stored (FR-004). An UNKNOWN age MUST NOT render as fresh (FR-005) —
`?` and `0s` are different claims. Staleness is measured against `CMA_STATUS_TTL`, which inherits
`CMA_MODELS_DEV_TTL` and defaults to 24h. A future timestamp is clock skew: clamp to `0`, never emit
a negative that would compare as fresh everywhere downstream. Listing NEVER re-probes — the age is
local and free; re-verifying on read would put a network round-trip per alias behind a command
operators run reflexively.

## Evidence

The captured material a verdict rests on.

| Field | Type | Rules |
|---|---|---|
| `path` | path | Must exist and be non-empty for a PASS (FR-008). |
| `kind` | enum | `replayed-wire`, `live-probe`, `census`, `render`. |
| `sha256` | string | Pins a recorded corpus so a replay is provably the bytes captured. |
| `controls` | record | Positive and negative control results for any census (§11.4.273). |

**Rule.** A `live-probe` may support an opt-in distribution check but MUST NOT be the default
verdict path for a backend measured as non-deterministic (FR-007). The default is `replayed-wire`:
real captured bytes replayed through the unmodified production path.

## Capability

An extension the operator can activate or deactivate.

| Field | Type | Rules |
|---|---|---|
| `name` | string | Catalogue-unique. |
| `state` | enum `active` \| `inactive` | Exactly two states. |
| `restore_command` | string | Must be reachable without knowing an installation-specific path (FR-013). |

**Rule.** An explicit opt-out survives session restarts and MUST NOT be silently undone by a routine
refresh (FR-012).

## Catalogue

The inventory answering "what could I use here".

| Field | Type | Rules |
|---|---|---|
| `entries` | list | Every capability with its state — active AND inactive (FR-011). |
| `cache_age` | duration | Carried wherever data derived from it is used (FR-023). |
| `stale` | bool | When true, every derived result announces it. A silent fallback is forbidden (FR-023). |

**Storage**: `models.dev.cache.json`, 24h TTL, 213 providers at measurement.
