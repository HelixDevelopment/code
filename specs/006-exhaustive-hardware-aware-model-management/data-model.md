# Data Model (Phase 1) — Revision 2, delta-scoped

**Feature**: `006-exhaustive-hardware-aware-model-management`
**Ground truth**: `research.md` (Phase-0 inventory). This file defines **only the
entities feature 006 adds or extends**. Existing entities are referenced, not
redefined, per §11.4.74 (extend-don't-reimplement).

---

## 1. Existing entities (REFERENCE — do not redefine)

| Entity | Owner package (reused) | Role |
|---|---|---|
| `HostCapabilityProfile` | `helix_llm/internal/capability/profile.go` | Measured CPU/RAM/storage/accelerator profile |
| `Catalogue.Entry` + `CapabilityFamily` | `helix_llm/internal/catalogue/entry.go` | Runnable-model family + requirements |
| `brain/models.Catalog` / `Registry` | `helix_llm/internal/brain/models/` | In-memory model catalogue + status |
| `selection.Request/Result` | `helix_llm/internal/selection/` | Fit/placement/refusal |
| `vrambroker.Broker/Lease` | `helix_llm/internal/vrambroker/` | VRAM admission |
| `lifecycle.Manager` | `helix_llm/internal/lifecycle/` | Idle/TTL evict/unload |
| `runtime.Chooser` | `helix_llm/internal/runtime/choose.go` | In-memory vs streaming path |
| `discovery.Instance` / `health.Tracker` | `helix_llm/internal/discovery/` | Endpoint discovery + liveness |
| `catalog.Entry` | `helix_agent/internal/catalog/catalog.go` | Selectable target catalogue |
| `HTTPClientPool` | `helix_agent/internal/http/pool.go` | Connection pool (currently tests-only) |

---

## 2. New / extended entities

### 2.1 `HardwareProfileCache` (A1 — extend `internal/capability`)

**Purpose**: hold the last measured `HostCapabilityProfile` with an explicit
freshness stamp so callers avoid re-measuring on every request. **In-memory only**
(D-D resolved: no durable store, no DB).

| Field | Type | Notes |
|---|---|---|
| `Profile` | `HostCapabilityProfile` | reused entity |
| `MeasuredAt` | `time.Time` | from `capability.FreshnessPolicy` |
| `Source` | `enum{local, remote}` | local host vs distribution endpoint |
| `EndpointID` | `string` (optional) | set when `Source=remote` |

**Invariants**
- `Profile.Validate()` must pass before the cache stores it.
- A read older than `FreshnessPolicy` triggers re-measure; a failed re-measure
  keeps the last good profile and records a warning (no silent zeroing).
- Concurrent reads during refresh must be atomic (race-free).

**State machine**: `empty → fresh → stale → (refreshing → fresh | stale+warn)`.

### 2.2 `ModelNameParseResult` (A2 — extend `internal/naming`)

**Purpose**: the deterministic result of parsing an arbitrary upstream model name
into a clean identifier plus recovered metadata. Extends `naming.Derive`.

| Field | Type | Notes |
|---|---|---|
| `Raw` | `string` | e.g. `helixllm-anton-qwen2-5-coder-3b-instruct-q4_k_m-f6771589d190` |
| `CleanName` | `string` | e.g. `qwen2-5-coder-3b-instruct` |
| `VendorPrefix` | `string` (optional) | stripped prefix, retained for provenance |
| `Quantization` | `string` (optional) | e.g. `q4_k_m` — recovered, not discarded |
| `VariantHash` | `string` (optional) | trailing build hash |
| `DerivedID` | `string` | from existing `naming.Derive`/`Registry` (collision-safe) |

**Invariants**
- Parse is total: any input yields a result or a typed error — never a panic.
- `CleanName` never contains a provider prefix, hash suffix, or quant token.
- `Quantization`/`VariantHash` are preserved as metadata, never silently dropped.
- A colliding `DerivedID` is resolved via the existing `Registry` (no overwrite).

### 2.3 `EndpointRegistration` (A4 — extend `internal/discovery`)

**Purpose**: make endpoint lifecycle explicit (registration, capability
advertisement, health, deregistration) where today only TTL liveness exists.

| Field | Type | Notes |
|---|---|---|
| `Instance` | `discovery.Instance` | reused |
| `Capabilities` | `[]CatalogueCapability` | advertised model/backend capabilities |
| `RegisteredAt` | `time.Time` | |
| `LastSeen` | `time.Time` | from `health.Tracker` |
| `State` | `enum{registered, unhealthy, deregistered}` | explicit deregister (new) |

**Transitions**: `registered → unhealthy → registered` (recovery);
`registered|unhealthy → deregistered` (explicit or TTL expiry).
**Invariant**: a deregistered endpoint is never returned by discovery; a stale
capability advertisement is not served after deregistration.

---

## 3. Relationships

```text
HostCapabilityProfile 1──1 HardwareProfileCache
        │
        └──(fit)──> selection.Request ──> selection.Result        [reused]
                            │
Catalogue.Entry ────────────┘
        │
        └──> ModelNameParseResult (clean name + metadata)          [new, A2]

discovery.Instance 1──1 EndpointRegistration ──> Capabilities      [new, A4]
```

## 4. Validation rules

| Entity | Rule |
|---|---|
| `HardwareProfileCache` | store only a validated profile; never store on failed measure |
| `ModelNameParseResult` | total function; quant/hash preserved; collision-safe ID |
| `EndpointRegistration` | no discovery hit after `deregistered`; capabilities cleared on deregister |

## 5. Explicitly NOT modelled (D-D resolved)

- No durable SQLite tables, no `goose` migrations, no `CatalogStoreRecord`.
- No `HardwareProfileCache` persistence across process restarts.
- Catalogue/naming remain YAML + in-memory.

## 6. Honest boundaries (§11.4.6)

- Field names above are **design intent**; they must be reconciled with the actual
  exported symbols in `capability`, `naming`, and `discovery` during T014/T023/T033
  before the types are committed. Any name collision with an existing exported type
  must be resolved by extension, not by adding a parallel type.
- Behavioural requirements (freshness thresholds, collision policy) must be read
  from the existing code and captured as tests, not assumed here.
