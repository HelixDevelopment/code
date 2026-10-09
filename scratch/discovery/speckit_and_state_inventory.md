# HelixCode — SpecKit, planning state, and Claude provider work inventory

**Date**: 2026-09-03  
**Project root**: `/home/milosvasic/Projects/helix_code`  
**Meta repo branch**: `main` (`HelixDevelopment/code.git`)  
**Meta HEAD**: `317492e0` (`2026-09-03 19:14:50 +0200`) — `chore(submodules): bump helix_llm to b83babf`

---

## 1. SpecKit / `.specify/` state

`.specify/` is the SpecKit working directory. It does **not** contain the
project specs themselves; those live at `specs/001-helixskills-incorporation/`
and `specs/002-adaptive-local-model-serving/`. `.specify/` contains the
extension runtime, configuration, and the pointer to the active feature.

### 1.1 Core `.specify/` files

| File | Purpose |
|------|---------|
| `/home/milosvasic/Projects/helix_code/.specify/feature.json` | Active feature directory: `specs/002-adaptive-local-model-serving` |
| `/home/milosvasic/Projects/helix_code/.specify/init-options.json` | `ai: claude`, `integration: claude`, `speckit_version: 1.0.4.dev0`, `script: sh` |
| `/home/milosvasic/Projects/helix_code/.specify/integration.json` | Installed integrations: `["claude"]`; default integration `claude` |
| `/home/milosvasic/Projects/helix_code/.specify/memory/constitution.md` | Deliberate **pointer only** (CONST-059). It says the real constitution is `constitution/Constitution.md` and the project extensions are `CLAUDE.md` / `AGENTS.md` / `QWEN.md` / `GEMINI.md`. |
| `/home/milosvasic/Projects/helix_code/.specify/workflows/speckit/workflow.yml` | Speckit workflow definition |
| `/home/milosvasic/Projects/helix_code/.specify/extensions.yml` | Extension registry |
| `/home/milosvasic/Projects/helix_code/.specify/extensions/superspec/` | Installed `superspec` extension (with its own commands, templates, and an example static-landing-page spec) |

### 1.2 Active project specs

| Spec | Location | Status | Tasks |
|------|----------|--------|-------|
| 001 HelixSkills incorporation | `specs/001-helixskills-incorporation/` | Complete | `4/4` checked (`specs/001-helixskills-incorporation/tasks.md`) |
| 002 Adaptive local model serving | `specs/002-adaptive-local-model-serving/` | **Active programme** | `89/97` checked, `8` pending (`specs/002-adaptive-local-model-serving/tasks.md`) |

### 1.3 Spec 002 detail

- **Feature branch**: `002-adaptive-local-model-serving` is declared in `plan.md`; `feature.json` says the spec directory is `specs/002-adaptive-local-model-serving`. `spec.md` notes no separate Git branch was created because the extension hook that would create one is not registered.
- **Plan**: `specs/002-adaptive-local-model-serving/plan.md` (15 141 bytes, 2026-09-02 19:25).
- **Tasks**: `specs/002-adaptive-local-model-serving/tasks.md` (26 562 bytes, 2026-09-02 21:55).
- **Progress ledger**: `specs/002-adaptive-local-model-serving/progress.yml` (246 142 bytes, 2026-09-03 18:50) — authoritative record of what is broken/proven.
- **Research**: six raw research docs under `research/` plus consolidated `research.md`.
- **Contracts**: `contracts/consumer-export.md`, `contracts/model-listing.md`, `contracts/selection.md`, `contracts/README.md`.
- **Quickstart**: `quickstart.md` with runnable scenarios 1–7.

### 1.4 Spec 002 progress snapshot (from `progress.yml`)

```yaml
feature: 002-adaptive-local-model-serving
branch: main
started: 2026-09-02T19:24+02:00
total_tasks: 97
current_phase: 2
phase_name: Foundational (Blocking Prerequisites)
in_flight_agents: 6
tasks_count: 6          # phase-2 task subset tracked here, all "complete"
findings_count: 155
open_like_count: ~49
gates:
  baseline_build: PASS
  baseline_unit_tests: PASS
```

The `progress.yml` `tasks` dictionary only tracks the current phase-2 subset
(T001–T005, T013); all are `complete`. The full 97-task backlog is in
`tasks.md`. The `findings` ledger is the live record of open/closed issues.

### 1.5 Pending spec-002 tasks (unchecked in `tasks.md`)

| ID | Story | Description |
|----|-------|-------------|
| T037 | US1 | Independent Fable/xhigh review of the US1 diff, iterated to GO |
| T052 | US2 | Execute `claude_toolkit/scripts/claude-providers.sh` helixllm-export and **confirm** the result |
| T053 | US2 | Live-validate HelixAgent + HelixLLM through a synchronised alias |
| T054 | US2 | Release a new Claude Toolkit version (operator checkpoint) |
| T055 | US2 | Independent review of US2 diff, attention to consumer-name validation |
| T068 | US3 | Independent security review of US3 diff (credentials, trust boundary) |
| T084 | US5 | Independent review of US5 diff (no family retains static selection) |
| T097 | All | Final independent review across all phases |

---

## 2. Resumption / handoff documents

### 2.1 `docs/CONTINUATION.md`

- **Path**: `/home/milosvasic/Projects/helix_code/docs/CONTINUATION.md`
- **Size/mtime**: 386 034 bytes, `2026-09-03 14:40`
- **Status**: Present and recently updated. A 2026-09-03 correction in the TL;DR
  (`docs/CONTINUATION.md:378`) explicitly says the old "no active programme"
  line was false: **feature 002 `adaptive-local-model-serving` is the active
  programme**, mid-execution at 89/97 tasks with ~20 open findings.
- **Key historical sections**:
  - `## Active Work — Xiaomi MiMo Integration (2026-06-19)` — Status `IMPLEMENTED`.
  - `## 2026-07-27 — Test-suite remediation cycle (PAUSED mid-flight, operator session limit)` — superseded; the work was later committed and pushed.
- **Persistent blockers listed at end** (`docs/CONTINUATION.md:1670–1681`):
  - Port collision: registry says HelixAgent owns `8100`, `configs/development.yaml` says `7061`, LLMsVerifier occupies `8100`.
  - Constitution pin is 6 commits behind (checkout `32d75788` vs pin `731bf1d3`), which is why the six root carriers still top out at §11.4.234 instead of §11.4.235 — a §11.4.157 lockstep gap.
  - Canon defect: §11.4.64 / §11.4.205 / §11.4.206 cited but never defined.
  - `reports/latency/p99-baseline-2026-03-16.txt` and `releases/.version-data/helixagent.last-hash` are tracked files rewritten by tests (CONST-053).
  - `.docs_chain/contexts/{issues,fixed}.yaml` still point legacy summary generators at Markdown.

### 2.2 `RESUME.md` — standing session-resumption file (§11.4.131)

- **Path**: `/home/milosvasic/Projects/helix_code/RESUME.md`
- **Rev 23**, `2026-09-03 ~14:00 CEST`.
- **Most important current facts**:
  - Active programme is **feature 002**.
  - **The naming/export batch is NO-GO** — independent re-review confirmed four findings by running code, three of them created by the fixes.
  - `89/97` tasks complete; ~93 findings in `progress.yml`, roughly 20 open.
  - **Everything is pushed** as of ~13:45 CEST:
    - meta `b752a807`
    - `helix_llm` `1efda3b5`
    - `helix_agent` `64cf8921`
    - `claude_toolkit` `328cf27b` on branch `fix/helixllm-export-review-findings` (not merged to `main`)
  - System **boots and serves** on the native route (`make build` → `./bin/helixcode`); the containerised route (`./helix start`) is still broken (`BOOT-4`).
  - Full retest: `helix_llm` green (54 packages), `helix_agent` green (289 packages), `helix_code` 7 failures (3 GUI build gap, 4 load-sensitive packages), `claude_toolkit` 12 assertion failures = 2 root causes.
  - Open decisions needing operator input are listed in `RESUME.md` §5 (selection VRAM headroom, concurrent placements, exposed credentials, container boot, etc.).
- **Resume prompt** (verbatim from `RESUME.md:430`):
  > Read RESUME.md then specs/002-adaptive-local-model-serving/progress.yml, run
  > `git fetch --all --prune --tags`, and continue feature 002. Everything is
  > pushed as of rev 23 (meta b752a807, helix_llm 1efda3b5, helix_agent 64cf8921,
  > toolkit 328cf27b on a branch) and the system BOOTS — start it with §2b, not
  > `./helix start`, which is still broken (BOOT-4).

### 2.3 `.remember/remember.md`

- **Global project file**: `/home/milosvasic/Projects/helix_code/.remember/remember.md` **does not exist**.
- **Provider-local `.remember`**: the Claude provider integration area has its own
  `.remember/` at
  `/home/milosvasic/Projects/helix_code/submodules/helix_agent/internal/llm/providers/claude/.remember/`.
  - Log: `.remember/logs/memory-2026-09-03.log` — 38 `session-start` hook entries
    from `07:34:09` through `19:10:02` on 2026-09-03.
  - Session slug: `.remember/tmp/session-slug` records
    `session_id=c3b10350-c599-4457-b7da-b4949e9e9fd6`.

---

## 3. Claude Code `plans` and `todos` directories

- `/home/milosvasic/.claude-claude1/plans/` — **empty** (only `.` and `..`).
- `/home/milosvasic/.claude-claude1/todos/` — **empty** (only `.` and `..`).

No plan or todo files related to `helix_code` or the Claude provider integration
were found in the Claude-1 config directory.

---

## 4. Git state

### 4.1 `/home/milosvasic/Projects/helix_code` (meta repo)

```
branch: main
HEAD:   317492e0 2026-09-03 19:14:50 +0200 chore(submodules): bump helix_llm to b83babf — the measured Mistral-Nemo entry
```

Recent commits:

```
317492e0 2026-09-03 19:14:50 +0200 chore(submodules): bump helix_llm to b83babf — the measured Mistral-Nemo entry
6f701458 2026-09-03 19:12:53 +0200 docs(measurement): OPEN-24 record now says APPLIED, because it is
252b2066 2026-09-03 19:06:08 +0200 chore(submodules): bump helix_llm to 7dd5835 — CRITICAL-2, CRITICAL-4, OPEN-2, JWT auth
0bd6624d 2026-09-03 19:04:07 +0200 measure(catalogue): OPEN-24 — run the agent lane's three candidates instead of guessing them
bc193667 2026-09-03 18:55:25 +0200 feat(systemd): unlock the RTX 3060 for local inference; track the native coder unit
```

`git status --short`:

```
 m submodules/helix_agent
?? docs/qa/phase1_fullhttp_e2e_20260903T154920Z/
?? docs/qa/phase1_fullhttp_e2e_20260903T162443Z/
```

- `submodules/helix_agent` is modified (submodule pointer not yet committed).
- Two new QA evidence dirs are untracked.

### 4.2 `submodules/helix_agent`

```
HEAD:   64cf8921 2026-09-03 13:31:58 +0200 fix(mcp): 5 npm names that resolve but install nothing, and the guard that missed them
status: 101 commits ahead of tag helix-code-1.0.0-dev-0.0.1
```

Recent commits:

```
64cf8921 2026-09-03 13:31:58 +0200 fix(mcp): 5 npm names that resolve but install nothing, and the guard that missed them
34340f5b 2026-09-03 12:56:10 +0200 feat(mcp): restore 13 integrations on verified community/vendor packages
913d1f02 2026-09-03 12:33:11 +0200 fix(tests): validate the OpenCode config this repo ships, not the operator's
ce9c4eb9 2026-09-03 11:09:39 +0200 fix(mcp): 35 non-existent npm packages in Go-generated MCP configs
3b414cc7 2026-09-03 10:06:35 +0200 fix(tests): resolve spec-kit submodule from the repo that declares it
```

`git status --short` (modified files):

```
M internal/handlers/completion.go
M internal/handlers/completion_test.go
M internal/handlers/openai_compatible.go
M internal/handlers/openai_compatible_test.go
M internal/llm/providers/azure/azure.go
M internal/llm/providers/claude/claude.go
M internal/llm/providers/claude/claude_test.go
M internal/llm/providers/gemini/gemini_api.go
M internal/llm/providers/generic/generic.go
M internal/llm/providers/lmstudio/lmstudio.go
M internal/llm/providers/ollama/ollama.go
M internal/llm/providers/openrouter/openrouter.go
M internal/llm/providers/vertex/vertex.go
M internal/models/types.go
M internal/router/router.go
?? internal/handlers/usage_token_split_red_test.go
?? internal/llm/providers/azure/usage_metadata_test.go
?? internal/llm/providers/gemini/usage_metadata_test.go
?? internal/llm/providers/generic/usage_metadata_test.go
?? internal/llm/providers/lmstudio/usage_metadata_test.go
?? internal/llm/providers/ollama/usage_metadata_test.go
?? internal/llm/providers/openrouter/usage_metadata_test.go
?? internal/models/token_split_test.go
?? internal/router/ensemble_usage_test.go
```

### 4.3 `/home/milosvasic/Projects/claude_toolkit`

```
branch: fix/helixllm-export-review-findings
HEAD:   fdeef17 2026-09-03 19:12:49 +0200 feat(providers-verify): flag a 200 that comes from the WRONG SERVICE
status: clean
main:   75d25ab [origin/main: behind 1]
```

Recent commits:

```
fdeef17 2026-09-03 19:12:49 +0200 feat(providers-verify): flag a 200 that comes from the WRONG SERVICE
267182b 2026-09-03 18:59:55 +0200 fix(providers): repoint the three Helix endpoints at ports that answer, and stop reporting every transport failure as one cause
328cf27 2026-09-03 13:29:42 +0200 test(providers): pin the provider-id charset as a security control; refresh proofs
e8e877a 2026-09-03 11:02:03 +0200 test(helixllm-export): the withheld guard could be deleted and this repo would not notice
37c4f48 2026-09-03 10:01:38 +0200 docs(helixllm-export): the reason absence is safe has changed
```

---

## 5. Claude provider integration area

### 5.1 Files in `submodules/helix_agent/internal/llm/providers/claude/`

| File | Size | Mtime |
|------|------|-------|
| `claude.go` | 28 366 bytes | 2026-09-03 18:39 |
| `claude_test.go` | 59 953 bytes | 2026-09-03 18:55 |
| `claude_cli.go` | 25 188 bytes | 2026-08-31 20:05 |
| `claude_cli_test.go` | 21 844 bytes | 2026-08-31 20:05 |
| `README.md` | 1 971 bytes | 2026-08-31 20:05 |

### 5.2 Design / README docs

- `submodules/helix_agent/internal/llm/providers/claude/README.md` — basic provider overview,
  supported models, auth (`CLAUDE_API_KEY`), configuration snippet, features (tool calling,
  vision, streaming, system prompts), rate limits, error handling.
- Other Claude-related docs in `helix_agent`:
  - `submodules/helix_agent/skills/claude-code/command-development/README.md`
  - `submodules/helix_agent/skills/claude-code/plugin-structure/README.md`
  - `submodules/helix_agent/docs/cli-agents/claude-plugins/README.md`
  - `submodules/helix_agent/docs/cli-agents/claude-code-source/README.md`
  - `submodules/helix_agent/docs/cli-agents/claude-code/README.md`
  - `submodules/helix_agent/docs/cli-agents/claude-squad/README.md`
  - `submodules/helix_agent/docs/research/CLAUDE-CODE-SYSTEM-PROMPT/README.md`

### 5.3 What was being implemented in the Claude provider directory

The uncommitted diff in `claude.go` and `claude_test.go` is a **token-usage
telemetry correctness fix**, part of a broader cross-provider pass that also
modifies `openai_compatible.go`, `models/types.go`, and several other providers.

Specific changes in `submodules/helix_agent/internal/llm/providers/claude/claude.go`:

- `TokensUsed` in `convertResponse()` now returns the **total** tokens
  (`InputTokens + OutputTokens`) instead of only `OutputTokens`.
- `Metadata` now records **both** `input_tokens` and `output_tokens` (previously
  `output_tokens` was discarded).

Corresponding test update in `claude_test.go`:

- Assert `TokensUsed == 15` (10 input + 5 output) instead of 5.
- Assert `Metadata["output_tokens"] == 5`.
- Comments explicitly mark this as a stale-gate reconciliation per §11.4.120.

The broader diff shows this fix is wired end-to-end:

- `internal/models/types.go` — adds `LLMResponse.TokenSplit()` which reads real
  per-direction counts from `Metadata` (OpenAI-shaped `prompt_tokens`/`completion_tokens`
  and Anthropic-shaped `input_tokens`/`output_tokens`) and never invents a half.
- `internal/handlers/openai_compatible.go` — replaces the old `TokensUsed/2`
  estimation in `convertSingleResponseToOpenAI()` and `convertToOpenAIChatResponse()`
  with `resp.TokenSplit()`.
- `processToolResultsWithLLM()` now returns the provider's `*models.LLMResponse`
  so tool-result synthesis can report real usage instead of `len(reply)/4`.
- Related providers (`azure`, `gemini_api`, `generic`, `lmstudio`, `ollama`,
  `openrouter`, `vertex`) were modified to emit the usage metadata needed by
  `TokenSplit()`.
- New untracked tests:
  - `internal/handlers/usage_token_split_red_test.go`
  - `internal/llm/providers/*/usage_metadata_test.go`
  - `internal/models/token_split_test.go`
  - `internal/router/ensemble_usage_test.go`

**What is unfinished**: the token-usage telemetry batch is **uncommitted** in
`helix_agent`. It has not yet passed the mandatory independent code review
(§11.4.142 → §11.4.134) and has not been pushed. The Claude provider portion is
one piece of this batch.

---

## 6. Where the work stands

### 6.1 Active programme

- **Feature 002 `adaptive-local-model-serving`** is the only active programme.
- US1 (measured local model selection) implementation is largely complete; the
  pending work is independent review (T037) and the fixes it will likely surface.
- US2 (consumer export / Claude Toolkit naming) is **NO-GO** — a re-review found
  four issues, three created by the fixes. The branch `fix/helixllm-export-review-findings`
  in `claude_toolkit` is the current work site.
- US3, US5, and the final cross-phase review tasks are also pending.

### 6.2 Claude provider / token-usage work

- A correctness fix is staged (uncommitted) in `helix_agent`.
- It fixes under-reported Claude token totals and missing `output_tokens` metadata.
- It depends on the new `LLMResponse.TokenSplit()` accessor and new tests.
- Next step: independent review, then commit, then push.

### 6.3 Open blockers

| Blocker | Evidence / location |
|---------|---------------------|
| **Anthropic weekly limit / endless context compaction loop** | Provider-dir session log shows **35 sessions** created under `.../-home-milosvasic-Projects-helix-code-submodules-helix-agent-internal-llm-providers-claude/` on 2026-09-03, last `c3b10350-c599-4457-b7da-b4949e9e9fd6` at 19:10. Each latest JSONL is only 33 lines with no assistant message — the session is failing to start. |
| **Claude Toolkit naming/export batch is NO-GO** | `RESUME.md:62` and `progress.yml` findings `T055-ROUND3-NOGO`, `T055-REREVIEW-NOGO`, `F1-F4-W1-resolved`, `WITHHELD-on-wire`. |
| **Port collision / wrong HelixAgent port** | `docs/CONTINUATION.md:1677` and `RESUME.md` §5: registry says 8100, `configs/development.yaml` says 7061, LLMsVerifier occupies 8100; generated OpenCode config points at wrong service. |
| **Containerised boot still broken** | `BOOT-4` in `progress.yml` / `RESUME.md` §5: 43 `replace` directives in `helix_code/go.mod` point outside the Docker build context. |
| **Constitution pin 6 commits behind** | `docs/CONTINUATION.md:1678`; carriers top out at §11.4.234, canon has §11.4.235+. |
| **13 exposed credentials unrotated** | `RESUME.md` §5 — operator decision is to keep the record, not rotate. |
| **Open findings needing operator/design decisions** | `progress.yml` open-like count ≈ 49, including `OPEN-17`/`OPEN-18` (VRAM headroom / concurrent placement), `OPEN-24` (three former agent-lane models missing from catalogue), `OPEN-25` (text entries skip device axis), `OPEN-33` (vision lane same RAM-vs-VRAM question), `OBS-1` (`GET /api/v1/auth/me` registered but 404), `ROOT-1` (toolkit points Helix providers at wrong ports), etc. |

---

## 7. Next actionable items

1. **Resume with a working model/alias.** The native `claude1` alias hit the
   Anthropic weekly limit. Start a fresh session under a provider alias that
   currently has quota (e.g. `deepseek`, `helixagent`, `helixllm-gateway`) or
   wait for the Anthropic window to reset. Use the resume prompt from
   `RESUME.md:430`.

2. **Reconcile and commit the token-usage telemetry batch in `helix_agent`.**
   - Run the new tests: `go test -v ./internal/llm/providers/claude/... ./internal/models/... ./internal/handlers/... ./internal/router/...`.
   - Dispatch an independent code-review agent on the Fable/xhigh substrate
     (§11.4.209) and iterate to zero findings (§11.4.134).
   - Stage by path (`git commit -- <paths>`) to avoid sweeping other agents'
     in-flight work.
   - Bump the `helix_agent` submodule pointer in the meta repo and push
     fast-forward only (§11.4.113).

3. **Continue feature 002.**
   - Re-run the quickstart scenarios 1–4 in `specs/002-adaptive-local-model-serving/quickstart.md`.
   - Drive T052–T055 (Claude Toolkit export, live validation, publish, review).
   - Address the NO-GO findings in `claude_toolkit` branch
     `fix/helixllm-export-review-findings`.
   - Schedule T037, T068, T084, T097 independent reviews.

4. **Resolve operational blockers.**
   - Decide and fix the HelixAgent port collision (`8100` vs `7061`).
   - Fix `BOOT-4` container build (layout + scoped `.dockerignore`).
   - Advance the constitution submodule pin to close the §11.4.157 lockstep gap.
   - Resolve `OBS-1` (`/api/v1/auth/me` 404) — either register or publish the correct path.
   - Correct `ROOT-1` toolkit base URLs pointing at ports nothing listens on.

5. **Observe the host-load / process-ownership rules.** This host runs other
   projects (`kfl`, `MainThread`, qbittorrent) at ~50% background load. Verify
   process ownership by `cwd` before acting on any process (§11.4.174). Do not
   run the test suites while the server is up; do not run multiple suites at
   once (`METHOD-1`).

6. **Run `git fetch --all --prune --tags` first** in the meta repo and every
   touched submodule before making any local edit (§11.4.60 / §11.4.71).

---

## 8. File index / evidence paths

- SpecKit config: `/home/milosvasic/Projects/helix_code/.specify/feature.json`
- SpecKit integration: `/home/milosvasic/Projects/helix_code/.specify/integration.json`
- SpecKit constitution pointer: `/home/milosvasic/Projects/helix_code/.specify/memory/constitution.md`
- Spec 002 tasks: `/home/milosvasic/Projects/helix_code/specs/002-adaptive-local-model-serving/tasks.md`
- Spec 002 progress: `/home/milosvasic/Projects/helix_code/specs/002-adaptive-local-model-serving/progress.yml`
- Spec 002 plan: `/home/milosvasic/Projects/helix_code/specs/002-adaptive-local-model-serving/plan.md`
- Spec 002 quickstart: `/home/milosvasic/Projects/helix_code/specs/002-adaptive-local-model-serving/quickstart.md`
- Resumption file: `/home/milosvasic/Projects/helix_code/RESUME.md`
- Continuation doc: `/home/milosvasic/Projects/helix_code/docs/CONTINUATION.md`
- Claude provider code: `/home/milosvasic/Projects/helix_code/submodules/helix_agent/internal/llm/providers/claude/claude.go`
- Claude provider tests: `/home/milosvasic/Projects/helix_code/submodules/helix_agent/internal/llm/providers/claude/claude_test.go`
- Claude provider README: `/home/milosvasic/Projects/helix_code/submodules/helix_agent/internal/llm/providers/claude/README.md`
- Provider-local session log: `/home/milosvasic/Projects/helix_code/submodules/helix_agent/internal/llm/providers/claude/.remember/logs/memory-2026-09-03.log`
- Provider-local session slug: `/home/milosvasic/Projects/helix_code/submodules/helix_agent/internal/llm/providers/claude/.remember/tmp/session-slug`
- Claude Code session JSONL dir: `/home/milosvasic/.claude-claude1/projects/-home-milosvasic-Projects-helix-code-submodules-helix-agent-internal-llm-providers-claude/`
- Last session JSONL: `c3b10350-c599-4457-b7da-b4949e9e9fd6.jsonl`
