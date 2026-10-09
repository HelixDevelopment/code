# Local-Only Adaptive Serving — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Prove with machine evidence that all HelixLLM→HelixAgent→HelixCode LLM execution runs locally via llama.cpp + Colibri (combinable, dynamically chosen from measured host hardware), close every identified gap with full test coverage, land everything on main of every owned repo, push ff-only to all upstreams, and deliver complete systemctl --user integration with static reboot-survival proof.

**Architecture:** Extend spec 002's existing seams (`runtime.Chooser`, `streaming_launch.go` Process/HealthProbe, `vrambroker` leases, `Brain.Models()`) — no rewrites. Colibri adopted as an external pinned-C dependency launched under the same lease/health discipline as llama-server. Cloud providers stay in code behind a disabled-by-default config gate. systemd user units + one consolidated setup script provide the boot chain.

**Tech Stack:** Go 1.24 (testify, table-driven tests), pure-C vendored Colibri, systemd --user units, podman compose, bash.

**Spec:** `docs/superpowers/specs/2026-09-05-local-adaptive-serving-design.md`

## Global Constraints

- All work lands on `main` of each repo; pushes are **ff-only to every configured upstream**, force-push forbidden (§11.4.113).
- Before any push: `git fetch --all --prune`, merge latest main, re-run suites.
- TDD for every change (§11.4.224); every bluff fix carries a RED polarity guard (§11.4.115, env `RED_MODE`, default 1 = reproduce).
- Bare `t.Skip()` forbidden without `// SKIP-OK: #<ticket>` marker.
- No hardcoded hosts/paths/secrets; deployment values come from config/env (CONST-046, §11.4.10).
- Evidence ledger root: `docs/qa/2026-09-05-local-adaptive-serving/` (main repo) and per-repo `docs/qa/<run-id>/`.
- Go commands run from the owning module dir (e.g. `cd submodules/helix_llm && go test ./...`).
- Third-party submodules are read-only; Colibri is vendored new at `dependencies/colibri/` (pinned commit, recorded in `dependencies/README.md` or equivalent ledger).
- CONST-033: never execute host power transitions; boot-survival proof is static + operator runbook.
- Commit style: conventional prefix, cite finding/FR IDs (e.g. `fix(helixllm): T055-3 identity collision`).

## Wave map (dependency order)

| Wave | Streams | Repos | Parallel? |
|---|---|---|---|
| 1 | F1–F5 forensic extraction + R1 research | all | yes (read-only + web) |
| 2 | W2a helix_llm fixes; W2b helix_agent; W2c helix_code; W2d toolkit; W2e systemd/setup (main repo root) | per repo | W2a/b/c/d parallel; W2e sequential after W2c (same working tree) |
| 3 | G1–G5 gauntlet per repo | all | yes, after that repo's W2 |
| 4 | P1 push all repos | all | sequential per repo, parallel across repos |
| 5 | B1–B3 boot proof + runbook | main repo | last |

---

## Wave 1 — Forensic extraction (read-only, parallel)

### Task F1: helix_llm findings extraction ledger

**Files:**
- Read: `specs/002-adaptive-local-model-serving/progress.yml`, `specs/002-adaptive-local-model-serving/tasks.md`
- Create: `specs/002-adaptive-local-model-serving/findings-ledger-2026-09-05.md`

- [ ] **Step 1: Extract every open finding.** Parse `progress.yml`; list each finding with status != closed/resolved: id, severity, file:line, one-line defect, blocking task.
- [ ] **Step 2: Extract T055 defect details.** Find the T055 export-review report (naming defects, 7 critical/high); enumerate each as T055-N with file:line and proposed fix direction.
- [ ] **Step 3: Extract remaining open spec-002 tasks** (97-task list, 89 done) — the 8 outstanding.
- [ ] **Step 4: Write the ledger** as a markdown table + machine-readable fenced JSON block (array of `{id, repo, severity, file, defect, fix_direction, test_plan}`). Commit: `docs(spec002): findings ledger 2026-09-05`.

### Task F2: helix_agent gap ledger

**Files:**
- Read: `submodules/helix_agent/**` (AGENTS.md first), specs 002 FR-019
- Create: `submodules/helix_agent/docs/qa/2026-09-05-gap-ledger.md`

- [ ] **Step 1: FR-019 state.** Locate `Models(host/avail/withheld)` declaration vs call sites; record wiring gaps with file:line.
- [ ] **Step 2: Provider bluff evidence.** Read `internal/provider/openai_compatible.go` (~line 2925, nonce echo); capture exact behavior, callers, and why it is a bluff.
- [ ] **Step 3: Default-provider check.** Determine how helix_agent chooses its LLM endpoint today; record whether cloud paths are reachable by default.
- [ ] **Step 4: Write ledger** (same schema as F1) + commit.

### Task F3: helix_code consumption ledger

**Files:**
- Read: `helix_code/internal/llm/**`, `helix_code/config/config.yaml`, `helix_code/internal/config/**`
- Create: `helix_code/docs/qa/2026-09-05-gap-ledger.md`

- [ ] **Step 1: Default provider resolution.** Trace `llm.default_provider` → actual provider construction; record what happens when it is `local`/empty (file:line).
- [ ] **Step 2: Dead config block.** Capture `config/config.yaml` lines ~155–166 (`helix-llm`/`helix-debate` provider block) verbatim for removal.
- [ ] **Step 3: Gateway wiring.** Find how the server would route to the HelixLLM gateway (`:8443`) — present? config keys? Record gaps.
- [ ] **Step 4: Write ledger** (same schema) + commit.

### Task F4: toolkit/exporter ledger

**Files:**
- Read: `submodules/claude_toolkit/**` (AGENTS.md first), `submodules/claude-toolkit` if present, spec 002 FR-037
- Create: `submodules/claude_toolkit/docs/qa/2026-09-05-gap-ledger.md`

- [ ] **Step 1: FR-037.** Record both repos' remotes, HEADs, divergence; which name is canonical; merge plan.
- [ ] **Step 2: Review branch state.** `git -C submodules/claude_toolkit log main..fix/helixllm-export-review-findings --oneline` and diffstat; list what merging entails.
- [ ] **Step 3: Write ledger** + commit.

### Task F5: current-state verification evidence (the "confirm" baseline)

**Files:**
- Create: `docs/qa/2026-09-05-local-adaptive-serving/00-baseline-evidence.md` (main repo)

- [ ] **Step 1: Gateway model listing.** With the local stack running (or `systemctl --user start helixllm-gateway.service helixllm-coder-native.service`), capture `curl -sk https://127.0.0.1:8443/v1/models` output. If a model is listed, trace its origin (config vs measurement) and record the trace.
- [ ] **Step 2: Process tree.** `ps -eo pid,cmd | grep -E 'llama-server|colibri|helixllm'` captured; record which runtimes are actually live.
- [ ] **Step 3: Hardware probe.** Run the capability measurement (or read `capability.Measure` output via a small `go run` harness in helix_llm) and capture the profile JSON.
- [ ] **Step 4: Verdict.** Baseline doc states plainly: what is proven local+dynamic today, what is not (Colibri seam, media services, helix_qa boot, cloud default paths). Commit.

### Task R1: Colibri integration research (web)

**Files:**
- Create: `specs/002-adaptive-local-model-serving/research/07-colibri-integration-2026-09-05.md`

- [ ] **Step 1: Fetch upstream docs.** `FetchURL` https://github.com/JustVugg/colibri — build instructions, server/CLI invocation, model-path argument format, any HTTP endpoint/port, health signal, license, latest release tag/commit.
- [ ] **Step 2: Serving semantics.** How a model is loaded/served; whether it daemonizes; stdout/stderr readiness signals; graceful shutdown behavior.
- [ ] **Step 3: Cite.** Every claim carries URL + access date (§11.4.99); unknowns recorded as `UNCONFIRMED:` — never guessed. Commit.

---

## Wave 2 — Fixes (parallel per repo)

### Task W2a-1: Colibri adoption — vendor + build (helix_llm, main repo root coordination)

**Files:**
- Create: `dependencies/colibri/` (git submodule pinned to researched commit — R1)
- Modify: `setup.sh` (root), `dependencies/README.md` (or create ledger entry)

**Interfaces:**
- Consumes: R1 research doc (exact build/invocation semantics).
- Produces: `dependencies/colibri/build/colibri` binary path convention; `HELIX_COLIBRI_BIN` env var contract consumed by W2a-2.

- [ ] **Step 1: Add submodule.** `git submodule add https://github.com/JustVugg/colibri dependencies/colibri && cd dependencies/colibri && git checkout <pinned-commit>`; record pin in ledger.
- [ ] **Step 2: Build.** Follow R1 build steps; produce `dependencies/colibri/build/colibri`; capture build log as evidence.
- [ ] **Step 3: setup.sh wiring.** Add a `build_colibri()` step (gcc presence check, idempotent, SKIP-OK marker when toolchain absent) before the systemd-unit install step.
- [ ] **Step 4: Evidence.** `dependencies/colibri/build/colibri --help` (or equivalent) captured into `docs/qa/2026-09-05-local-adaptive-serving/`. Commit: `feat(dependencies): vendor colibri <commit> + build wiring`.

### Task W2a-2: Colibri launch seam wiring (helix_llm)

**Files:**
- Read first: `submodules/helix_llm/internal/runtime/streaming_launch.go` (Process interface ~:107, HealthProbe ~:124, Launcher.Launch ~:315, T070 deferral notes :26-37), `internal/runtime/colibri.go` (ExecProcess :52, HTTPHealth :213)
- Create: `submodules/helix_llm/internal/runtime/colibri_process.go`, `internal/runtime/colibri_process_test.go`
- Modify: config surface ( wherever `streaming_launch` gets its deployment config — record exact file in F1 ledger if unclear ), `deploy/compose.yaml`, `docs/test-coverage.md`

**Interfaces:**
- Consumes: `ExecProcess`/`HTTPHealth` from `colibri.go`; `Process`/`HealthProbe` interfaces from `streaming_launch.go`; `HELIX_COLIBRI_BIN` env contract from W2a-1.
- Produces: `ColibriProcess` (implements `Process`) and `ColibriHealth` (implements `HealthProbe`) constructors taking binary path/args/health URL from config; wired into `Launcher.Launch` behind the T070 gate — gate removed, replaced by config presence check (binary configured → live launch; absent → honest `OPERATOR-BLOCKED`-style refusal, never fake-serve).

- [ ] **Step 1: Failing test.** In `colibri_process_test.go`: (a) `TestColibriProcessImplementsInterfaces` — compile-time `var _ Process = (*ColibriProcess)(nil)` + `var _ HealthProbe = (*ColibriHealth)(nil)`; (b) `TestLauncherRefusesWithoutBinary` — Launch with no binary configured returns the closed-set refusal (not a launched fake); (c) `TestLauncherLaunchesWithBinary` — using a fake binary script (`#!/bin/sh\nwhile true; do sleep 1; done`) + stub health server, Launch starts the process, health-budget passes, Stop tears down (skip with SKIP-OK if platform forbids subprocess in test env — but subprocess tests are used elsewhere in this repo per `colibri_test.go` conventions).
- [ ] **Step 2: Run — verify RED.** `cd submodules/helix_llm && go test ./internal/runtime/ -run Colibri -v` → compile failure / undefined.
- [ ] **Step 3: Implement.** `ColibriProcess` wraps `ExecProcess` (config-supplied path/args, graceful signal→grace→kill via existing primitive); `ColibriHealth` wraps `HTTPHealth`; Launcher wiring: admit → resolve binary from config → start → health budget → serve → stop → lease release on every exit path (existing invariant).
- [ ] **Step 4: Run — verify GREEN.** Full `go test ./internal/runtime/ -v`.
- [ ] **Step 5: Compose service.** Add `colibri` service to `deploy/compose.yaml` following the existing `llamacpp` service pattern (image or build from `dependencies/colibri`, healthcheck, no hardcoded host paths).
- [ ] **Step 6: Coverage ledger + commit** (`feat(helixllm): T070 colibri launch seam wired`).

### Task W2a-3: T055 naming defects (helix_llm)

**Files:**
- Per F1 ledger entries T055-1..7
- Test: follow each ledger entry's `test_plan`

- [ ] **Step 1: For each T055-N:** write the failing test exactly as the ledger's test_plan specifies (identity-collision, export dead-code, model-ID resolution tests).
- [ ] **Step 2: Verify RED per defect** (run the single test; capture failure).
- [ ] **Step 3: Fix minimally** at the ledger's file:line.
- [ ] **Step 4: Verify GREEN** — `go test ./...` in helix_llm.
- [ ] **Step 5: Commit per defect or per coherent group** (`fix(helixllm): T055-N ...`).

### Task W2a-4: Open findings burn-down (helix_llm)

**Files:** Per F1 ledger findings entries.

- [ ] **Step 1–4 per finding:** same TDD cycle as W2a-3 (failing test → RED evidence → fix → GREEN → commit citing finding id).
- [ ] **Step 5: Ledger update.** Mark each finding closed with evidence pointer; commit ledger.

### Task W2b-1: helix_agent FR-019 + bluff fix

**Files:**
- Per F2 ledger; known anchors: `submodules/helix_agent/internal/provider/openai_compatible.go` (~:2925)
- Create: RED polarity test per §11.4.115

**Interfaces:**
- Consumes: F2 ledger (exact FR-019 signatures), helix_llm `Brain.Models()` naming scheme `helixllm/<host>/<model>[:<variant>]`.
- Produces: `Models(host, avail, withheld)` behavior; nonce-echo removed → real challenge-response (or honest removal of the fake path).

- [ ] **Step 1: RED guard for the bluff.** Test asserting the nonce is NOT echoed back as authentication (modeled on the ledger's captured behavior); `RED_MODE=1` reproduces on pre-fix code.
- [ ] **Step 2: Verify RED** on current code (evidence).
- [ ] **Step 3: Fix** per F2's fix_direction (real verification or delete the fake capability — never keep a decorative check).
- [ ] **Step 4: FR-019 wiring** per ledger; tests.
- [ ] **Step 5: GREEN** — full agent suite `go test ./...` in `submodules/helix_agent`. Commit(s).

### Task W2c-1: helix_code local-default + cloud gate + dead config removal

**Files:**
- Per F3 ledger; anchors: `helix_code/internal/llm/`, `helix_code/config/config.yaml` (dead block ~:155-166), `helix_code/internal/config/`

- [ ] **Step 1: Failing tests.** (a) default provider resolves to local HelixLLM gateway when `llm.cloud.enabled` unset/false; (b) cloud provider construction refuses (clear error) when gate is false even if API keys present; (c) config loads with the dead block removed.
- [ ] **Step 2: RED evidence.**
- [ ] **Step 3: Implement gate** (`llm.cloud.enabled`, default false, surfaced in status) + default wiring to gateway + remove dead config block.
- [ ] **Step 4: GREEN** — `cd helix_code && go test ./internal/llm/... ./internal/config/... -v` then broader suite.
- [ ] **Step 5: Commit** (`feat(helixcode): local-default llm routing + cloud config gate`).

### Task W2d-1: toolkit FR-037 + review-branch merge

**Files:** Per F4 ledger; `submodules/claude_toolkit`

- [ ] **Step 1: Resolve canonical name** per ledger (remote rename/ff merge of the duplicate; record decision doc).
- [ ] **Step 2: Merge `fix/helixllm-export-review-findings`** into main after running toolkit suite (`go test ./...`); resolve conflicts on Fable/xhigh per §11.4.211 if any.
- [ ] **Step 3: GREEN suite + commit + push** (this repo pushes in Wave 4 with the rest).

### Task W2e-1: systemd — media services + HelixQA units

**Files:**
- Create: `scripts/systemd/helixllm-audio.service`, `helixllm-imagegen.service`, `helixllm-ocr.service`, `helixllm-tts.service`, `helixllm-vectorize.service`, `helixllm-videogen.service`, `helixqa.service`
- Modify: `scripts/install_systemd_units.sh` (add to install/enable sets), `docs/SYSTEMD.md`

**Interfaces:**
- Consumes: existing unit template conventions (`@HELIX_ROOT@` placeholders, `wait-http-ready.sh`, `helix.target` umbrella).
- Produces: units joining `helix.target` via `WantedBy=`, readiness-gated.

- [ ] **Step 1: Discover each service's start command + port.** Read `submodules/helix_llm/services/{audio,imagegen,ocr,tts,vectorize,videogen}` and `submodules/helix_qa` READMEs/Containerfiles; record cmd + health endpoint per service in the task's working notes.
- [ ] **Step 2: Write units** following `helixcode-server.service` template exactly (placeholder expansion, ExecStartPost readiness gate, WantedBy lines).
- [ ] **Step 3: Installer wiring** — add units to the install list; enable-set policy: enabled by default ONLY if their runtime deps (models/binaries) are present at install time, else installed-not-enabled with a documented reason (mirrors existing llmsverifier precedent).
- [ ] **Step 4: Validate.** Run `scripts/install_systemd_units.sh` (idempotent), `systemd-analyze --user verify` each unit (or `--no-pager` equivalent), capture output.
- [ ] **Step 5: Docs + commit** (`feat(systemd): media services + helixqa user units`).

### Task W2e-2: setup consolidation

**Files:**
- Modify: `setup.sh` (banner port fix :8081→:8080), `scripts/setup.sh` (legacy), `submodules/helix_llm/scripts/setup.sh` (placeholder gap)
- Create: redirection notices

- [ ] **Step 1: Fix banner** in root `setup.sh`.
- [ ] **Step 2: Retire legacy `scripts/setup.sh`** — replace body with an exec redirect to the canonical path (or delete + pointer doc, per repo convention; deletion needs no operator re-confirmation here — operator mandate 2026-09-05 supersedes §11.4.122 for these two installer scripts).
- [ ] **Step 3: Fix `submodules/helix_llm/scripts/setup.sh`** — either invoke `scripts/install_systemd_units.sh` from the main repo by relative discovery, or expand placeholders itself; never install a unit with literal `@...@`.
- [ ] **Step 4: Test** — run both scripts with `--help`/dry modes; capture evidence. Commit.

---

## Wave 3 — Gauntlet (per repo, after its W2 completes)

### Task G1: helix_llm gauntlet
- [ ] Full suite: `cd submodules/helix_llm && go test ./...` + `go vet ./...` — capture.
- [ ] Independent code review subagent (Fable/xhigh; Opus/xhigh fallback) over the full diff since baseline; findings fixed + re-reviewed to zero-finding GO (§11.4.134).
- [ ] Deep-research spot-check on Colibri claims in new code vs R1 doc URLs.
- [ ] Commit evidence to `docs/qa/2026-09-05-local-adaptive-serving/`.

### Task G2: helix_agent gauntlet — same shape as G1.
### Task G3: helix_code gauntlet — same shape as G1 (include `make test` scope feasible in-session; at minimum `go test ./internal/llm/... ./internal/config/...` + full suite if time-feasible — full suite is the mandate, run it).
### Task G4: toolkit gauntlet — same shape as G1.
### Task G5: traceability gate (new, cross-cutting)
- [ ] **Step 1: Failing test** in helix_llm: enumerate `Brain.Models()` output; assert every entry resolves to a measured-selection trace (catalogue entry + host profile age within policy + lease class). RED on any static/env-named model.
- [ ] **Step 2: RED, fix, GREEN** (FR-056 enforcement point is `Brain.Models()` emission — fix there).
- [ ] **Step 3: Commit + include in G1 evidence.**

---

## Wave 4 — Push (ff-only, all upstreams)

### Task P1: push all owned repos in scope

For each repo in [helix_llm, helix_agent, claude_toolkit(+duplicate resolution), main repo]:

- [ ] **Step 1:** `git fetch --all --prune`; merge latest main (ff or merge commit, never force).
- [ ] **Step 2:** Re-run that repo's suite (must be green post-merge).
- [ ] **Step 3:** Push `main` to **every** push remote (`git remote -v | grep push`). Note: GitHub reports main repo moved to `HelixDevelopment/code.git` — update the remote URL with `git remote set-url` for affected remotes (record the change; install_upstreams per CONST-056 governs long-term).
- [ ] **Step 4:** Verify with `git ls-remote <remote> main` matching local HEAD per remote.
- [ ] **Step 5:** Update `docs/CONTINUATION.md` + RESUME.md in the same final main-repo commit.

---

## Wave 5 — Boot proof (static) + runbook

### Task B1: setup.sh end-to-end
- [ ] Run `./setup.sh` (or `--skip-build` if binaries already current — record which); capture full log; verify exit 0.
### Task B2: full unit set start + readiness evidence
- [ ] `systemctl --user daemon-reload`; start `helix.target` and every enabled unit; capture per-unit `wait-http-ready` gate outputs (real readiness, not process-alive).
- [ ] Confirm `:8443/v1/models` lists locally-served models traceable per F5 method; capture.
### Task B3: static reboot-survival proof + runbook
- [ ] `loginctl show-user $USER -p Linger` = yes (evidence).
- [ ] Symlink chain evidence: `ls ~/.config/systemd/user/default.target.wants/ ~/.config/systemd/user/helix.target.wants/`.
- [ ] Write `docs/SYSTEMD.md` reboot runbook section (operator-executed steps + expected checks) — CONST-033 compliant.
- [ ] Commit evidence + docs.

---

## Self-review notes (inline)

- Spec §5.1 Colibri/T055/findings → W2a-1..4 + R1 + G5. §5.2 → W2b-1. §5.3 → W2c-1. §5.4 → W2d-1. §5.5 → W2e-1/2. §6 → G1–G5. §7 → P1, B1–B3. No spec section lacks a task.
- Ledger-driven tasks (W2a-3, W2a-4, W2b-1, W2c-1, W2d-1) deliberately defer exact test code to Wave-1 ledgers — the findings' file:line content is not knowable until F1–F4 read it; each such task pins the TDD cycle and schema so no placeholder remains at execution time.
- Type/signature consistency: `Process`/`HealthProbe` (streaming_launch.go) consumed by W2a-2 exactly as read in Wave 1; `Brain.Models()` naming scheme consumed by W2b-1/W2d-1; `HELIX_COLIBRI_BIN` produced W2a-1 → consumed W2a-2. Verified against exploration report anchors.
