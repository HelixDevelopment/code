# Contracts (Phase 1) — HTTP/OpenAPI + JSON Schemas

**Transport decision**: D-A RESOLVED 2026-09-12 — **reuse existing transports**.
`helix_llm` uses HTTP/3 QUIC + HTTP/2 TLS (Gin), WebSocket, `shared/rpc` JSON-RPC,
and A2A JSON-RPC. **No `.proto` and no gRPC surface is introduced.** These contracts
therefore describe the delta payloads as JSON Schemas; existing endpoints
(`internal/discovery`, `internal/catalogue`, `internal/brain`) are consumed as-is
and are **not** re-specified here (§11.4.74).

## Files

| File | Describes | Status |
|---|---|---|
| `model-listing.schema.json` | the `GET /v1/models` entry **as consumed** by `helixllm --list-models` | **bound to code** — `TestModelListingContractMatchesDecoder` compares the documented field set against the decoder's JSON tags in both directions |
| `hardware-profile.schema.json` | hardware profile payload | design sketch — reconciles to `capability.HostCapabilityProfile` |
| `model-name-parse.schema.json` | parse result | design sketch — superseded by `naming.Reference` |
| `endpoint-registration.schema.json` | endpoint lifecycle | design sketch — superseded by the `discovery.Tracker` API |

## Reconciliation note (2026-09-13, T005)

Running a consistency check (§11.4.75) established that **the first three schemas
described payloads that no Go type implements**: the feature reuses existing
in-process types (`naming.Reference`, `capability.Cache`, `discovery.Tracker`)
and adds no new wire payloads of its own. A contract naming a type that does not
exist is decoration, so:

- `model-listing.schema.json` was added and is **bound to the real decoder** by a
  test that fails if either side drifts — a promised field with no decoder tag,
  or a decoder field with no promise.
- `model-name-parse` and `endpoint-registration` are marked superseded rather
  than deleted (§11.4.124 — investigate before removing) and are kept only as a
  record of the earlier design intent.

No new HTTP endpoint was introduced by this feature (decision D-A: reuse existing
transports), which is why only ONE of the four describes a real wire contract.


## Reused (not redefined)

- Discovery/health: `helix_llm/internal/discovery` (`Discoverer`, `health.Tracker`).
- Catalogue: `helix_llm/internal/catalogue` + `internal/brain/models`.
- Model listing HTTP surface: `GET /v1/models` (existing).
- `helix_agent` catalog/grpc: `internal/catalog`, `cmd/grpc-server` — unchanged.

## Honest boundary (§11.4.6)

These schemas pin the **delta** payloads only. They must be reconciled with the
actual exported Go structs during T014/T023/T033; a schema field with no
corresponding struct field (or vice versa) is a finding, resolved by extending the
struct, not by inventing a parallel type. No runtime endpoint is claimed to exist
until its handler is implemented and tested.
