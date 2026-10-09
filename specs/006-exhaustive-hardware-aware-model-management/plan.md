# Implementation Plan: Hardware-Aware Model Management — EXTEND existing HelixLLM/HelixAgent capabilities + fix 5 Claude-Toolkit integration issues

**Branch**: `006-exhaustive-hardware-aware-model-management` | **Date**: 2026-09-12 | **Spec**: `specs/006-exhaustive-hardware-aware-model-management/spec.md`
**Ground truth**: `specs/006-exhaustive-hardware-aware-model-management/research.md` (Phase-0 catalogue-first inventory, §11.4.74)

> **Revision 2 (2026-09-12) — grounding correction.** Revision 1 assumed the three
> submodules were near-empty and proposed new packages (`internal/hardware`,
> `internal/registry`, `internal/lifecycle` in helix_llm; `internal/connection`,
> `internal/debate` in helix_agent; `internal/{router,session,request,providers}/*.go`
> in claude-toolkit) plus CGO llama.cpp/colibri wrappers and a gRPC/protobuf surface.
> The Phase-0 inventory proved those assumptions wrong. This plan is re-scoped to
> **extend-don't-reimplement** (§11.4.74) and places every fix on its real surface
> (§11.4.102). Pre-revision text is superseded.

## Summary

Extend the **existing** HelixLLM capability stack and wire the **existing**
HelixAgent connection stack, then fix the 5 Claude-Toolkit integration issues on
their **real** surfaces (Bash/Python/JSON in `scripts/`, Go in
`submodules/claude-code-router`). The work is deliberately small relative to
Revision 1: the inventory found that hardware measurement, model fit/placement,
VRAM admission, lifecycle TTL/eviction, backend choice, llama.cpp provider,
embedding/reranker, and endpoint health already exist and must be reused.

Genuine deltas: (1) in-memory hardware-profile cache + periodic refresh; (2) arbitrary upstream model-name parser; (3) CLI list/status surface; (4) explicit endpoint deregistration; (5) HelixAgent shared-pool wiring; (6) the 5 toolkit fixes. Decisions D-A…D-D are **resolved 2026-09-12** (Appendix A): reuse HTTP/JSON-RPC, keep `nvidia-smi`+`/proc`, extend the stdlib-flag CLI, reuse YAML+in-memory (no DB).

## Technical Context

**Languages**: Go (helix_llm `github.com/HelixDevelopment/HelixLLM` @ go 1.26.1;
helix_agent `dev.helix.agent` @ go 1.26; claude-code-router
`github.com/vasic-digital/claude-code-router` @ go 1.26.4); Bash + Python + JSON
(claude-toolkit `scripts/`).

**Reuse (do NOT rebuild)** — helix_llm: `internal/capability` (CPU/RAM/storage/
accelerator measurement), `internal/selection` (fit/placement/refusal),
`internal/vrambroker` (admission/headroom), `internal/catalogue` +
`internal/brain/models` (catalogue/registry), `internal/naming` (derived
identifiers/aliases), `internal/lifecycle` (TTL/evict/unload),
`internal/runtime` (in-memory vs streaming choice, colibri process),
`internal/brain/llamacpp.go`, `internal/discovery` (health), `internal/knowledge`
(embedder/reranker). helix_agent: `internal/http/pool.go`,
`internal/transport/http3_client.go`, `internal/llm/{retry,circuit_breaker,
health_monitor}.go`, `internal/services/provider_registry.go`,
`internal/containers` + `internal/adapters/containers` (already use
`digital.vasic.containers`).

**Dependencies**: no new deps assumed. `gopsutil`, `go-nvml`, `goose`, `cobra` are
**absent** in helix_llm and their addition requires a decision (Appendix A).
`nvidia-smi` + `/proc` + `/sys` is the deliberate existing measurement path.

**Transport**: helix_llm uses HTTP/3 QUIC + HTTP/2 TLS (Gin), WebSocket,
`shared/rpc` JSON-RPC, A2A JSON-RPC — **no gRPC, no `.proto`**. helix_agent has a
gRPC server (`cmd/grpc-server`, `pkg/api/*.pb.go`). See decision D-A.

**Explicitly OUT of scope after grounding**:
- **CGO wrappers are not applicable.** helix_llm's llama.cpp provider is an HTTP
  client (`internal/brain/llamacpp.go`) and colibri is a launched process talked
  to over HTTP (`internal/runtime/colibri.go` `ExecProcess`/`HTTPHealth`). There is
  no in-process CGO boundary to harden. Revision 1's CGO-safety tasks are void.
- **New `internal/hardware`, `internal/registry`, `internal/connection`,
  `internal/debate` packages** — all duplicate existing code.

**Testing**: 13 constitution test types (§11.4.169) with paired §1.1 mutations at
every layer; TDD (§11.4.224); real infrastructure for non-unit layers (§11.4.27);
captured runtime evidence for every PASS (§11.4/§11.4.5/§11.4.69).

**Constraints**: no force-push (§11.4.113); no CI/CD (§11.4.156); rootless
containers via `digital.vasic.containers` (§11.4.76/§11.4.161), already used by
helix_agent; no hardcoded model lists (CONST-036); systematic debugging per
§11.4.102 before each fix; four-layer verification per §11.4.108.

## Constitution Check

*GATE: must pass before implementation; re-check after design.*

| Principle | Status | Notes |
|---|---|---|
| §11.4/§11.4.1/§11.4.123 anti-bluff (captured runtime evidence) | PASS | Every fix carries RED→GREEN evidence; live validation on real CLIs |
| §11.4.6 no guessing | PASS | Inventory is cited to paths; unknowns marked (Appendix A) |
| §11.4.74 reuse-before-rewrite | **PASS (now)** | Revision 1 violated it; Revision 2 reuses existing packages |
| §11.4.102 systematic debugging | PASS | Root-cause record precedes each of the 5 fixes |
| §11.4.108 four-layer verification | PASS | Pre-build, artifact byte-check, clean-target runtime signature, user-visible |
| §11.4.169 13 test types | PASS | Per-workstream coverage below |
| §11.4.224 TDD ≥85% | PASS | Tests first for every code change |
| §11.4.76/§11.4.161 containers | PASS | helix_agent already consumes `digital.vasic.containers` rootless |
| §11.4.125/§11.4.134/§11.4.142 review | PASS | Independent review per workstream, iterated to zero findings |
| CONST-036 LLMsVerifier SSoT | PASS | Dynamic discovery via `catalogue`/`llm` providers; no literals |
| §1.1 mutations at all layers | PASS | Paired mutation per gate |

## Workstreams

### WS-A — helix_llm extensions (module `github.com/HelixDevelopment/HelixLLM`)

| # | Delta | Target (real files) |
|---|---|---|
| A1 | In-memory hardware-profile cache + periodic refresh | extend `internal/capability/freshness.go`, `profile.go`; new sibling cache file in `internal/capability/` (no DB — D-D) |
| A2 | Arbitrary upstream-name parser (prefix/hash/quant strip) | extend `internal/naming/derive.go` (add parser; keep `Derive`/`Registry`) |
| A3 | CLI `list-models` / `detect-hardware` (profile) / `model-status` | extend `cmd/helixllm/main.go` (stdlib `flag`) — decision D-C |
| A4 | Endpoint deregistration + capability advertisement | extend `internal/discovery/health.go`, `discover.go` |
| A5 | Catalogue store | **NOT_APPLICABLE** — D-D resolved to reuse YAML+in-memory; no durable store, no migrations |
| A6 | Cross-consumer interface | **RESOLVED (D-A)**: reuse existing HTTP/3 + HTTP/2 + JSON-RPC; no gRPC/`.proto` added |

### WS-B — helix_agent wiring (module `dev.helix.agent`)

| # | Delta | Target (real files) |
|---|---|---|
| B1 | Wire the existing shared pool into the LLM path | `internal/http/pool.go` (currently tests-only) → used by `internal/llm/providers/helixllm/provider.go` |
| B2 | Health-gated dial + HTTP/3 fallback | `internal/transport/http3_client.go` + `internal/llm/health_monitor.go` |
| B3 | Route-level resilience for `helixagent-debate`/`-llm` | `internal/handlers/openai_compatible.go` (`processWithProviderChain` :3293, `processWithEnsemble` :2745) |
| B4 | gRPC health checking (`grpc_health_v1`) | **NOT_APPLICABLE** — D-A resolved to reuse existing transports; no `grpc_health_v1` addition |

**Not applicable**: no new `internal/connection` or `internal/debate`.

### WS-C — claude-toolkit 5 fixes (real surfaces)

| Issue | Real target | Language |
|---|---|---|
| 1 ca-bundle overwrite | `scripts/lib.sh` (L1857-1895 atomic write + lock) | Bash |
| 2 session resume | `scripts/claude-session.sh`, `scripts/lib.sh` `_cma_session_flags` | Bash |
| 3 32MB limit + compaction | `submodules/claude-code-router/internal/gateway/messages.go` + `openai_inbound.go`; compaction `scripts/lib.sh` L1140-1569 | Go + Bash |
| 4 Pi alias verification | `scripts/claude-providers.sh` (L2389/L2404) + verify scripts | Bash |
| 5 helixllm-gateway endpoint | `scripts/providers/helixllm-gateway.json` + `claude-providers.sh` | JSON + Bash |

**Not applicable**: no `internal/{router,session,request,providers}/*.go`.

### WS-D — validation (all workstreams)

Live CLI validation (Claude Code, Pi CLI, Helix TUI) with real models via
per-test rootless containers (containers submodule); 13 test types per
workstream; benchmark infra for load/switch/filter; evidence under
`docs/qa/<run-id>/` (§11.4.83).

### WS-E — release & docs

Transactional commit/push per §11.4.71/§11.4.113; project-prefixed tags
(§11.4.151) via gh/glab; changelogs; docs + diagrams (§11.4.257/§11.4.258);
README as canonical entry point (§11.4.212); four-format exports (§11.4.65).

## Project Structure (revised — existing + deltas only)

```text
submodules/helix_llm/                      # github.com/HelixDevelopment/HelixLLM (go 1.26.1)
├── internal/capability/                   # REUSE: CPU/RAM/storage/accelerator measure
│   ├── profile.go, freshness.go           #   EXTEND: in-memory cache + periodic refresh (A1)
│   └── (new) cache.go                     #   NEW: in-memory profile cache (A1, no DB)
├── internal/naming/derive.go              # EXTEND: upstream-name parser (A2)
├── internal/discovery/{health,discover}.go# EXTEND: deregistration + capabilities (A4)
├── internal/catalogue/, internal/brain/models/  # EXTEND (conditional A5)
├── cmd/helixllm/main.go                   # EXTEND: list/detect/status flags (A3)
└── (reused, untouched): selection/, vrambroker/, lifecycle/, runtime/, brain/llamacpp.go, knowledge/

submodules/helix_agent/                    # dev.helix.agent (go 1.26)
├── internal/http/pool.go                  # WIRE into production (B1)
├── internal/llm/providers/helixllm/provider.go # REPLACE bare transport (B1/B2)
├── internal/handlers/openai_compatible.go # reuse routes, add resilience (B3)
└── cmd/grpc-server/, pkg/api/             # EXTEND grpc_health_v1 (B4, conditional)

submodules/claude-toolkit/                 # Bash/Python/JSON; NO root go.mod
├── scripts/lib.sh                         # FIX ca-bundle (I1) + compaction (I3)
├── scripts/claude-session.sh              # FIX resume (I2)
├── scripts/claude-providers.sh            # FIX Pi alias (I4) + gateway pin (I5)
├── scripts/providers/helixllm-gateway.json# gateway endpoint (I5)
└── submodules/claude-code-router/internal/gateway/  # FIX 32MB limit/compaction (I3, Go)

specs/006-exhaustive-hardware-aware-model-management/
├── spec.md, plan.md, tasks.md
├── research.md                            # WRITTEN — Phase-0 inventory
├── data-model.md                          # Phase-1 entities (delta-scoped)
├── contracts/                             # Phase-1 interfaces (transport per D-A)
└── quickstart.md                          # validation guide
```

## Execution Strategy

### TDD

Strict RED→GREEN→REFACTOR for: name parser (A2), profile store (A1), CLI (A3),
deregistration (A4), pool wiring + dial path (B1/B2), and every toolkit fix
(C1–C5). For each of the 5 toolkit issues: root-cause record first (§11.4.102),
then a §11.4.115 RED-baseline reproduction on the broken artifact, then the fix.

### Parallelism

WS-A (helix_llm) and WS-C (claude-toolkit) are independent. Within WS-A, A1/A2/A3/A4
touch disjoint files. WS-B depends only on itself. WS-D spans all.

### Human Checkpoints

1. After WS-A deltas — verify against a real host (profile + parser + CLI).
2. After WS-B wiring — 200-request success on `helixagent-debate`/`-llm`.
3. After each WS-C fix — RED→GREEN on a clean target.
4. Before release — full 13-type suite + live CLI validation + review GO.

### Review Gates

- A2 parser + A1 store interfaces reviewed before consumers.
- B1/B2 transport wiring reviewed (connection-error blast radius).
- Each WS-C fix reviewed (security-sensitive: CA bundle, session tokens, body limits).
- Independent review iterated to zero findings per §11.4.134.

## Complexity Tracking

| Item | Why needed | Simpler alternative rejected because |
|---|---|---|
| Revision 2 re-scope | Revision 1 duplicated existing packages (§11.4.74) | Building parallel `hardware/`/`registry/`/`lifecycle/` would fragment the codebase and break existing FR/SC lineage |
| Dropping CGO tasks | No CGO boundary exists (providers are HTTP/process) | Adding CGO would introduce a risk surface the codebase deliberately avoids |
| Deferring gRPC (D-A) | helix_llm has no proto/grpc; HTTP/QUIC already serves | Adding gRPC is a large new surface with no named consumer yet |
| Conditional A5 store (D-D) | Persistence requirement unproven | Building a DB layer before a proven need violates minimal-change |

## Appendix A — Design decisions (RESOLVED 2026-09-12)

| # | Decision | Resolution |
|---|---|---|
| D-A | gRPC/`.proto` vs existing HTTP/QUIC+JSON-RPC | **Reuse existing transports** — no gRPC/`.proto` in helix_llm; contracts are HTTP/OpenAPI/JSON schemas |
| D-B | Add `gopsutil`/`go-nvml` vs keep `nvidia-smi`+`/proc` | **Keep the existing measured approach** — no new hardware-binding dependency |
| D-C | Add `cobra` vs extend stdlib-`flag` CLI | **Extend the existing stdlib-`flag` CLI** |
| D-D | Durable SQLite store + `goose` vs YAML+in-memory | **Reuse YAML + in-memory; no DB** — A1 is an in-memory cache |
| D-E | Fix locations | **Resolved** — WS-C table above |

## Appendix B — Honest boundaries (§11.4.6)

- This plan is grounded in **existence and location** of code, not in proof that
  the reused code is correct or tested. Per-requirement behavioural verification is
  required before claiming any spec FR is satisfied by reuse.
- `helix_llm` carries an existing FR/SC lineage; 006 must reconcile with it, not
  overwrite it.
- Revision-1 tasks that assumed new packages, CGO, or gRPC are void pending WS-A/WS-B
  and decisions D-A/D-D.
