# Design — Local-Only Adaptive Serving: Verification, Gap-Close & Boot Integration

**Date:** 2026-09-05
**Status:** Approved (operator, in-conversation)
**Classification:** Architectural (extends spec 002 — Adaptive Local Model Serving)
**Track:** (T1/main - claude) main-stream programme work
**Operator decisions captured:**
1. Runtime = **llama.cpp + Colibri** (spec 002 as written; Ollama NOT adopted as a backend).
2. **Colibri adoption authorized** — T070 checkpoint cleared.
3. Cloud providers: **local-default + config gate** (cloud code remains, disabled by default).
4. **All deferrals cleared** — T055 naming defects, ~21 open findings, toolkit review branch, dead config block.
5. Reboot-survival: **static proof only** (CONST-033 bans agent-executed power transitions).

---

## 1. Goal

Confirm, with machine-produced evidence, that **all** LLM execution routed through
HelixLLM and exposed through HelixAgent and HelixCode runs **locally** via
**llama.cpp** (in-memory GGUF serving) and **Colibri** (disk-streaming MoE), combinable
on one host, with models **dynamically chosen from measured host hardware** (CPU, RAM,
GPU/VRAM, storage class/headroom, IO). Every gap, shortcoming, danger zone, weak spot or
issue found MUST be root-caused, fixed, and covered with the full supported test-type
set. All changes land on **main** of every owned repo (submodules recursively + main
repo) and are pushed to **all upstreams**. The full stack MUST be bootable via
`setup.sh` with complete `systemctl --user` integration that survives host reboots
(proven statically; operator reboot runbook documented).

## 2. Scope & repos (CONST-051(A) equal codebase)

| Repo | Role |
|---|---|
| `submodules/helix_llm` | Core: runtimes, selection, Colibri adoption, T055, findings |
| `submodules/helix_agent` | Propagation: FR-019, provider bluff fix, local-gateway default |
| `helix_code/` (main module) + repo root | Consumption: local-default enforcement, config, systemd/setup |
| `submodules/claude_toolkit` + exporter | Consumer export: FR-037, review-branch merge |

Third-party submodules are read-only and untouched.

## 3. Runtime architecture — combined llama.cpp + Colibri

- **Decision stays in `internal/runtime.Chooser`** (pure, fail-closed, zero I/O):
  1. In-memory llama.cpp path preferred whenever weights + KV fit VRAM+RAM (FR-026).
  2. Colibri streaming admitted only for the **named 8-family roster**
     (`StreamingEligible()` — architecture predicates forbidden) with per-family
     resident-RAM floor (`ResidentMemoryBytes`) and full on-disk footprint check.
  3. Otherwise a closed-set refusal (`insufficient_resources` /
     `unsupported_configuration` / `host_not_measured`) with a distinct operator remedy.
- **Colibri adoption (T070, authorized 2026-09-05)**:
  - Vendor `github.com/JustVugg/colibri` (pure C, no Go binding) at a pinned commit
    under `dependencies/colibri/` (root layout per CONST-051(C)).
  - Build wired into `setup.sh` (gcc; zero-dependency upstream).
  - Wire the existing seam: `streaming_launch.go` `Process`/`HealthProbe` interfaces
    implemented via the real `ExecProcess`/`HTTPHealth` primitives
    (`internal/runtime/colibri.go`).
  - Binary path, args, health URL = deployment-supplied config, never hardcoded.
- **Combined serving**: per-model, the chooser output decides the backend; both backend
  families run under the same `vrambroker` lease + health-budget + idle-unload
  (`internal/lifecycle`) discipline. A supervisor unit (`helixllm-serve` family) holds
  leases and launches llama-server and/or Colibri processes per admitted model.

## 4. Dynamic selection enforcement (traceability)

Every model exposed via `Brain.Models()` / gateway `/v1/models` MUST trace to:
measured `HostCapabilityProfile` → catalogue entry → `selection.Select` → broker
lease → live local process.

- **Traceability gate** (test + pre-build gate): asserts no exposed model bypasses
  measurement (FR-056: no env-var-named model, no fixed fallback).
- **Local-default**: cloud providers remain in code but are disabled by default
  (`llm.cloud.enabled: false`); HelixCode `internal/llm` default provider resolves to
  the HelixLLM gateway; enabling cloud requires explicit config and is surfaced in
  status/docs.

## 5. Per-repo work packages

### 5.1 helix_llm
- Colibri adoption per §3 (vendor, build, seam wiring, compose service, systemd unit, tests).
- T055 naming defects (7 critical/high from export review) — apply with tests.
- ~21 open findings from `specs/002-adaptive-local-model-serving/progress.yml`.
- Docs + `docs/test-coverage.md` ledger update.

### 5.2 helix_agent
- FR-019 `Models(host/avail/withheld)` wiring.
- Provider bluff fix: nonce echo at `openai_compatible.go:2925` — RED polarity guard
  per §11.4.115.
- Enforce local-gateway default (cloud gated).

### 5.3 helix_code
- Local-default enforcement in `internal/llm` + config (`llm.cloud.enabled` gate).
- Remove dead `helix-llm`/`helix-debate` provider block in `config/config.yaml`
  (§11.4.122 operator decision: remove).
- Verify server routes to gateway by default; status surfaces cloud-gated state.

### 5.4 claude_toolkit + exporter
- FR-037: resolve `claude_toolkit` vs `claude-toolkit` repo-name inconsistency.
- Review + merge `fix/helixllm-export-review-findings` after review passes (§11.4.134).

### 5.5 systemd + setup (main repo)
- Add user units for HelixLLM media/capability services (audio, imagegen, ocr, tts,
  vectorize, videogen) and HelixQA.
- Consolidate the two divergent legacy installers (`scripts/setup.sh` root-legacy,
  `submodules/helix_llm/scripts/setup.sh` placeholder gap) into the single
  `scripts/install_systemd_units.sh` + root `setup.sh` path; retire/redirect legacy.
- Fix stale `:8081` completion banner (real port `:8080`).
- `docs/SYSTEMD.md` sync; cold-boot evidence under `docs/qa/<run-id>/`.

## 6. Test & review gauntlet

- Every fix TDD-first (§11.4.224); RED polarity guards for bluff fixes (§11.4.115);
  paired §1.1 mutations for every gate; full supported test types per repo
  (§11.4.169) as wired.
- Independent code review on Fable/xhigh (Opus/xhigh fallback) iterated to
  zero-finding/zero-warning GO (§11.4.134/§11.4.209).
- Deep-research pass on Colibri integration specifics (§11.4.150) — cited artefact.
- Evidence ledger committed under `docs/qa/<run-id>/` (§11.4.83).

## 7. Push & boot-survival proof

Per repo, in order: `git fetch --all --prune` → merge latest `main` → full suites
re-run → **ff-only push to every upstream** (§11.4.113; force-push forbidden
everywhere). Submodules recursively, then main repo.

Boot proof (static, per operator decision):
- Run `setup.sh`; `systemctl --user` start the full unit set; capture per-unit
  readiness evidence (real readiness gates, not process-alive).
- Prove the boot chain: `loginctl show-user $USER` Linger=yes;
  `default.target.wants → helix.target → unit wants` symlinks.
- Document an operator reboot runbook (CONST-033: agent never executes power
  transitions).

## 8. Honest boundaries

- Colibri's 8-family roster is upstream-closed; dense models can never stream —
  refusals name this plainly.
- Static boot proof verifies the chain, not a physical reboot (operator-executed).
- Cloud-provider code remains (CONST-039 multi-provider mandate preserved behind
  the config gate).
- A feature may be honestly SKIP-justified (§11.4.3) only for genuinely absent
  hardware/credentials — never silently.
