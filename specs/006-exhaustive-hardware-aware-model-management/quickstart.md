# Quickstart & Validation Guide (Phase 1)

**Feature**: `006-exhaustive-hardware-aware-model-management` (Revision 2)
**Purpose**: reproducible commands to validate each workstream. Every command must
produce captured output (evidence), not a claim.

---

## 0. Prerequisites

```bash
cd /home/milosvasic/Projects/helix_code
# Submodules present
for d in submodules/helix_llm submodules/helix_agent submodules/claude-toolkit submodules/helix_qa submodules/containers; do
  [ -d "$d" ] && echo "OK  $d" || echo "MISSING $d"
done
# Module facts (expect helix_llm 1.26.1, helix_agent 1.26)
grep -m1 '^module' submodules/helix_llm/go.mod
grep -m1 '^go' submodules/helix_llm/go.mod
grep -m1 '^module' submodules/helix_agent/go.mod
```

## 1. WS-A — verify REUSED helix_llm capabilities exist (not reimplemented)

```bash
# Hardware measurement already exists
ls submodules/helix_llm/internal/capability/{measure.go,measure_accelerator.go,measure_storage.go,profile.go,freshness.go}
# Fit/placement/refusal already exists
ls submodules/helix_llm/internal/selection/{select.go,fit.go,placement.go,terms.go}
# VRAM admission already exists
ls submodules/helix_llm/internal/vrambroker/{broker.go,budget.go}
# Lifecycle + backend choice already exist
ls submodules/helix_llm/internal/lifecycle/{idle.go,evict.go,notify.go}
ls submodules/helix_llm/internal/runtime/{choose.go,colibri.go}
# llama.cpp provider already exists
ls submodules/helix_llm/internal/brain/llamacpp.go
```

**Expected**: every path exists. Any missing path invalidates the "reused" claim in
`tasks.md` and must be raised before proceeding.

```bash
# Existing profile CLI still works
cd submodules/helix_llm && go run ./cmd/helixllm --capability | head -40
```

## 2. WS-A deltas (after implementation)

```bash
cd submodules/helix_llm
go test ./internal/capability/... ./internal/naming/... ./internal/discovery/... -run 'Cache|Parse|Dereg' -v
go test -race ./internal/capability/...
go run ./cmd/helixllm --list-models | head
go run ./cmd/helixllm --model-status | head
```

**Expected**: RED→GREEN evidence per task; the name parser maps
`helixllm-anton-qwen2-5-coder-3b-instruct-q4_k_m-f6771589d190` →
`qwen2-5-coder-3b-instruct` and preserves `q4_k_m` as metadata.

## 3. WS-B — helix_agent wiring (after implementation)

```bash
cd submodules/helix_agent
go build ./...
go test -race ./internal/http/... ./internal/llm/...
# 200 sequential requests to both model IDs (real server) — evidence captured
go test -tags=integration ./tests/integration/... -run TestHelixAgentConnection -v
```

**Expected**: zero production importers warning resolved (`internal/http/pool.go`
now imported by the helixllm provider); 200/200 success.

## 4. WS-C — the 5 toolkit fixes (after implementation)

Each fix follows: root-cause record → `RED_MODE=1` reproduction on the broken
artifact → fix → `RED_MODE=0` GREEN on a clean target.

```bash
cd submodules/claude-toolkit
# Issue 1 ca-bundle (Bash)
bash scripts/tests/test_ca_bundle_atomic.sh
# Issue 2 session resume (Bash)
bash scripts/tests/test_session_resume.sh
# Issue 3 32MB body limit (Go, claude-code-router)
(cd submodules/claude-code-router && go test ./internal/gateway/... -run BodyLimit -v)
# Issue 4 Pi alias (Bash)
bash scripts/tests/test_pi_alias.sh
# Issue 5 gateway endpoint (Bash)
bash scripts/tests/test_gateway_endpoint.sh
```

**Expected**: each RED run fails on the pre-fix artifact and passes after; the
mutation for each gate makes it fail.

## 5. Live CLI validation (WS-D)

```bash
# Per-test rootless containers via the containers submodule (no ad-hoc podman)
cd /home/milosvasic/Projects/helix_code
go test -tags=integration ./tests/live-cli/... -v   # Claude Code, Pi CLI, Helix TUI
# Evidence lands under docs/qa/<run-id>/
ls -1 docs/qa/ | tail
```

## 6. Release (WS-E)

```bash
# fetch-first, ff-only, never force (§11.4.71/§11.4.113)
git fetch --all --prune
# project-prefixed tags (§11.4.151) on every affected submodule
# helix_llm, helix_agent, claude-toolkit, helix_qa
```

## Honest boundaries (§11.4.6)

- Section 1 verifies **existence**, not correctness of reused code. Each FR claimed
  satisfied by reuse must still pass its own tests (T044 / WS-D).
- Commands in sections 2–5 reference **to-be-written** tests; they are the
  acceptance targets, not evidence that the work is done.
- No command here may be reported as PASS without pasted output from the actual run.
