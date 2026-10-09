# Research (Phase 0): Catalogue-First Ground-Truth Inventory

**Feature**: `006-exhaustive-hardware-aware-model-management`
**Date**: 2026-09-12
**Method**: read-only inventory of the three target submodules per §11.4.74
(reuse-before-reimplement) and §11.4.102 (investigate before fixing). Three
parallel exploration passes; every claim below is cited to a concrete path.

**Revision note (2026-09-12)**: this file replaces the Phase-0 artifact that did
not exist when `/speckit.superspec.execute` first ran. Its purpose is to correct
the *pre-inventory* plan, which assumed the three submodules were near-empty and
proposed new packages that already exist under other names.

---

## 0. Verdict summary

| Submodule | Declared shape by old plan | Actual shape |
|---|---|---|
| `submodules/helix_llm` | new `internal/hardware|registry|lifecycle` | **Most capability already exists** — `capability`, `selection`, `vrambroker`, `catalogue`, `naming`, `lifecycle`, `runtime`, `brain`, `discovery`, `knowledge` |
| `submodules/helix_agent` | new `internal/connection`, `internal/debate` | **Neither exists**; pooling/retry/CB/health exist elsewhere; `helixagent-debate`/`-llm` are model IDs, not packages |
| `submodules/claude-toolkit` | Go `internal/{router,session,request,providers}/*.go` | **Not a Go module** — Bash/Python/JSON in `scripts/`; the only Go fix lands in `scripts/../submodules/claude-code-router/internal/gateway/` |

**Consequence**: the old plan would have created duplicate implementations
(§11.4.74) and Go files at non-existent paths. Feature 006 must be re-scoped as
an **extension** of existing code plus a small set of genuine gaps.

---

## 1. Module facts (measured)

| Submodule | Module path | Go | gRPC | cobra | goose | gopsutil | go-nvml |
|---|---|---|---|---|---|---|---|
| `helix_llm` | `github.com/HelixDevelopment/HelixLLM` | 1.26.1 | indirect only, **no usage** | **absent** | **absent** | **absent** | **absent** (shells `nvidia-smi`) |
| `helix_agent` | `dev.helix.agent` | 1.26 | present + used (`cmd/grpc-server`) | — | — | — | — |
| `claude-toolkit` | *(no root go.mod)* | — | Go only in nested submodules | — | — | — | — |

`helix_llm` deliberately shells `nvidia-smi` and reads `/proc` `/sys` rather than
linking `go-nvml` (`internal/vrambroker/doc.go:19`, `budget.go:21`). Any plan that
adds `go-nvml`/`gopsutil` would reverse a deliberate decision and must justify it.

---

## 2. `helix_llm` inventory — reuse / extend / no-match

| Spec-006 capability | Verdict | Evidence |
|---|---|---|
| CPU detection | **reuse** | `internal/capability/measure.go` `MeasureCPU`; `measure_linux.go`; `internal/shared/hardware/profiler.go` |
| GPU/NVIDIA VRAM + accelerator memory | **reuse** | `internal/capability/measure_accelerator.go` `MeasureAccelerators`, `Accelerator{MemoryTotal,MemoryAvailable}`; `measure_linux.go` `nvidiaSMIProbe`; `internal/vrambroker/budget.go` |
| RAM detection | **reuse** | `internal/capability/measure_linux.go` `platformMemory` (`/proc/meminfo`) |
| Disk/storage detection | **reuse** | `internal/capability/measure_storage.go` `MeasureStorage`, `StoragePathForWeights` |
| Hardware profile entity | **reuse** | `internal/capability/profile.go` `HostCapabilityProfile` (+`Validate`, `Age`, `AcceleratorByIdentity`); `freshness.go` `FreshnessPolicy` |
| Hardware profile **persistence / periodic refresh cache** | **extend (gap)** | `freshness.go` re-measures; **no store/DB** |
| Model catalogue + registry | **reuse** | `internal/catalogue/` (`load.go`, `entry.go`); `internal/brain/models/catalog.go`, `registry.go` |
| Catalogue **durable store + migrations** | **no-match (gap)** | YAML + in-memory only; no goose/pgx model store |
| Clean model-name derivation | **reuse** | `internal/naming/derive.go` `Derive`, `sanitise`, `Registry`; `identity.go` |
| Arbitrary upstream-name **parser** (prefix/hash/quant strip) | **extend (gap)** | only colon-variant split `brain/naming.go` `splitModelVariant`; no HF prefix/hash parser |
| Model type classification / metadata | **reuse** | `catalogue/entry.go` `CapabilityFamily` (text, vision, tts, stt, embedding, …) |
| Hardware compatibility / fit / placement / refusal | **reuse** | `internal/selection/` (`select.go`, `fit.go`, `placement.go`, `terms.go`, `refusal_test.go`) |
| Lifecycle TTL / evict / unload | **reuse** | `internal/lifecycle/` (`idle.go`, `evict.go`, `notify.go`) |
| Warm tier / load decision | **reuse** | `internal/vrambroker` `ClassAgent`; `internal/laneboot/laneboot.go` |
| VRAM budgeting + admission control | **reuse** | `internal/vrambroker/broker.go` `Admits`, `HeadroomBytes`; `runtime/lease.go` |
| Backend choose (in-memory vs streaming) + colibri | **reuse** | `internal/runtime/choose.go`, `colibri.go`, `colibri_process.go` |
| llama.cpp provider / loader / presets | **reuse** | `internal/brain/llamacpp.go`; `brain/models/preset.go`; `brain/downloader.go` |
| Embedding / reranker | **reuse** | `internal/knowledge/llama_embedder.go`, `reranker.go` |
| Distribution endpoint discovery / health | **reuse** | `internal/discovery/discover.go`, `health.go` (`Tracker`, `DefaultHealthTTL`) |
| Endpoint **deregistration** endpoint | **extend (gap)** | TTL expiry is implicit; no explicit deregister |
| gRPC registration / `.proto` | **no-match (gap)** | zero `.proto`, zero `grpc.NewServer`; transports are HTTP/3 QUIC + HTTP/2 TLS (Gin), WebSocket, `shared/rpc` JSON-RPC |
| Model version migration / aliases | **reuse + extend** | `naming/derive.go` retired-identifier registry; `brain/naming.go` `PinModel`; no migration framework |
| CLI `list-models` / `detect-hardware` / `model-status` | **extend (gap)** | `cmd/helixllm/main.go` stdlib `flag` (`--capability` prints profile); model list is HTTP `GET /v1/models` |

**Existing spec lineage in code**: FR-002, FR-005, FR-012, FR-014/015, FR-018/019,
FR-021–025, FR-027, FR-033, FR-044–047, FR-055, FR-056; SC-007, SC-011, SC-018;
D1–D7. Master design: `docs/superpowers/specs/2026-04-04-helixllm-master-design.md`.

---

## 3. `helix_agent` inventory — reuse / extend / no-match

| Area | Verdict | Evidence |
|---|---|---|
| `internal/connection/` (old plan) | **no-match** | absent |
| `internal/debate/` (old plan) | **no-match** | absent; real code `internal/services/debate_*` + `internal/services/debate_integration/` + `digital.vasic.debate` (`replace` → `../debate_orchestrator`) |
| `helixagent-debate` / `helixagent-llm` | **reuse (as model IDs)** | `internal/handlers/openai_compatible.go:39-45,584-623` dispatch; `cmd/helixagent/main.go:3116-3183` advertises them |
| HTTP connection pooling | **extend (wiring gap)** | `internal/http/pool.go` `HTTPClientPool` exists but has **zero production importers** |
| HTTP/3 client pooling | **reuse** | `internal/transport/http3_client.go` `HTTP3ClientConfig{MaxIdleConns:100}` |
| Retry / backoff / jitter | **reuse** | `internal/llm/retry.go` `DefaultRetryConfig` (3 retries, jitter 0.1) |
| Circuit breaker | **reuse** | `internal/llm/circuit_breaker.go`; `services/provider_registry.go:194-340` |
| Provider health | **reuse** | `internal/llm/health_monitor.go`; `services/provider_registry.go:1287,1603`; `internal/health/liveness.go` |
| gRPC health checking | **extend** | app-level `/llm.LLMFacade/HealthCheck` in `pkg/api/llm-facade_grpc.pb.go`; **no** `grpc_health_v1` |
| Containers submodule use | **reuse** | `internal/containers/lazy_integration.go`; `internal/adapters/containers/adapter.go` import `digital.vasic.containers/pkg/*` |
| Underlying connection-error origin | — | `helixllm` provider uses a **bare** `&http.Transport{}` (`internal/llm/providers/helixllm/provider.go:213-235`) and dials `:8443`; errors surface at `openai_compatible.go:3338/3293` |

**Real gap (Issue 5)**: wire the existing shared pool + health-gated dial + HTTP/3
fallback into the `helixllm` provider and the two model routes — not build a new
`connection` package.

---

## 4. `claude-toolkit` inventory — real surfaces for the 5 issues

| Issue | Real surface (path, language) | Verdict |
|---|---|---|
| 1. ca-bundle.pem overwrite | `scripts/lib.sh:1857-1895` (Bash `cat >` combined bundle → `~/.claude-code-router/<id>/ca-bundle.pem`, `SSL_CERT_FILE`); verify CA `scripts/providers-verify.sh`, `scripts/model_verify.py:83-166`; tests `test_ccr_upstream_ca.sh`, `test_model_verify_tls.sh` | **reuse / extend (Bash)** — no Go `internal/router/ca_bundle.go` |
| 2. session persistence / `--resume` | `scripts/claude-session.sh` (259 L) incl. `cma_existing_session_id()` + `CMA_SESSION_DEAD_BYTES`; `scripts/claude-sync-state.sh`; `scripts/lib.sh` `_cma_session_flags` | **reuse / extend (Bash)** — "No conversation found" already guarded |
| 3. 32MB request limit + compaction | Go: `submodules/claude-code-router/internal/gateway/messages.go:24-36,199-221` (`maxRequestBodyBytes = 32<<20`), `internal/gateway/openai_inbound.go:69-74`; Bash compaction `scripts/lib.sh:1140-1569` | **extend** — Go fix belongs in `claude-code-router/internal/gateway`, not `internal/request` |
| 4. Pi CLI alias verification | dynamic: `scripts/claude-providers.sh:2389,2404` emits `pi-<id>` alias + `~/.pi-prov-<id>/config.toml`; launcher `scripts/lib.sh:4088-4175`; verifiers `claude-verify-providers.sh`, `providers-verify.sh` (546 L), `providers-semantic.sh` | **PARTIAL** — no static `pi-helixllm-gateway`, no `pi-providers` command |
| 5. helixllm-gateway endpoint | `scripts/providers/helixllm-gateway.json` (`https://127.0.0.1:8443/v1`, transport `router`), `helixagent*.json`, `helixcoder.json`, `helixllm/*.json`; Bash `claude-providers.sh` | **reuse / extend (JSON+Bash)** |

**Go surface that matters**: `submodules/claude-code-router` (`github.com/vasic-digital/claude-code-router`, go 1.26.4) with `internal/{gateway,proxy,router,cache,config,metrics,translate}`.

---

## 5. Genuine gap delta — what feature 006 actually adds

1. **Hardware-profile persistence + periodic refresh cache** (extend `capability/freshness`).
2. **Durable ModelCatalog store + schema migrations** (extend `catalogue`/`brain/models`).
3. **Arbitrary upstream-name parser** (prefix/hash/quantization stripping) (extend `naming`).
4. **CLI surface** for `list-models` / `detect-hardware` / `model-status` (extend `cmd/helixllm`).
5. **Endpoint deregistration + capability advertisement** (extend `discovery`).
6. **Cross-consumer interface decision** — the old plan mandated gRPC/`.proto`; `helix_llm` uses HTTP/3+HTTP/2/JSON-RPC and has none (see §6).
7. **`helix_agent` connection wiring** — shared pool + health-gated dial + HTTP/3 fallback into `providers/helixllm` and the two model routes.
8. **`claude-toolkit` fixes in their real homes** — Bash `lib.sh`/`claude-session.sh`/`claude-providers.sh` + `scripts/providers/*.json` + `claude-code-router/internal/gateway` (Go).

Everything else the old plan proposed already exists and must be **reused, not rebuilt**.

---

## 6. Design decisions (RESOLVED 2026-09-12)

| # | Decision | Resolution | Rationale |
|---|---|---|---|
| D-A | gRPC/`.proto` vs existing HTTP/QUIC + JSON-RPC | **RESOLVED: reuse existing transports.** No `.proto`, no new gRPC surface in `helix_llm`; contracts are HTTP/OpenAPI/JSON schemas. `helix_agent` keeps its existing app-level gRPC health; no `grpc_health_v1` addition. | §11.4.74 extend-don't-reimplement; `helix_llm` has zero proto today; no named consumer requires gRPC |
| D-B | Add `gopsutil`/`go-nvml` vs keep `nvidia-smi` + `/proc` + `/sys` | **RESOLVED: keep the existing measured approach.** No new hardware-binding dependency. | deliberate existing decision (`internal/vrambroker/doc.go:19`) |
| D-C | Add `cobra` vs extend stdlib-`flag` CLI | **RESOLVED: extend the existing stdlib-`flag` CLI** (`cmd/helixllm/main.go`) with `--list-models` and `--model-status` alongside `--capability`. | avoids a new dependency; matches the existing CLI |
| D-D | Add `goose`/SQLite durable store vs extend YAML + in-memory catalogue | **RESOLVED: reuse YAML + in-memory; no DB.** No `goose`/SQLite. The A1 profile layer is an **in-memory cache + periodic refresh**, not a durable store. | minimal change; no proven persistence requirement |
| D-E | Where do the 5 fixes live? | **RESOLVED** (inventory §4): Bash/JSON in `claude-toolkit/scripts`; Go in `submodules/claude-code-router/internal/gateway`; wiring in `helix_agent/internal/{http,llm,transport,handlers}` | §11.4.102 investigate-first |

---

## 7. Honest boundaries (§11.4.6)

- This inventory records **existence and location** of code, derived by reading
  files. It does **not** assert that any existing capability is correct, complete,
  or passing tests — no runtime evidence was captured.
- "No-match" entries (gRPC in `helix_llm`, `internal/connection`, `internal/debate`)
  mean *not found by this pass*; they are findings, not proofs of impossibility.
- Behavioural equivalence between an existing package and a spec-006 requirement
  (e.g. `selection` vs the proposed "compatibility algorithm") must be verified
  per-requirement during the re-plan, not assumed from names.
- `helix_llm`'s existing FR/SC lineage suggests a **prior spec** governed this
  area; the re-plan must reconcile 006 with that lineage rather than overwrite it.
