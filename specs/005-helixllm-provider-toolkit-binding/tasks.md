# Tasks: HelixLLM Provider Toolkit Binding

**Input**: Design documents from `/specs/005-helixllm-provider-toolkit-binding/`

**Prerequisites**: plan.md (required), spec.md (required for user stories)

**Tests**: The examples below include test tasks. Tests are REQUIRED - all acceptance scenarios must have corresponding verification.

**Organization**: Tasks are grouped by acceptance scenario to enable independent implementation and testing of each scenario.

## Format: `[ID] [P?] [Scenario] Description`

- **[P]**: Can run in parallel (different files, no dependencies)
- **[Scenario]**: Which acceptance scenario this task belongs to (e.g., AS1, AS2...)
- Include exact file paths in descriptions

---

## Phase 1: Setup & Heroku Removal (Blocking Prerequisite)

**Purpose**: Remove Heroku MCP submodule and prepare clean working tree

- [ ] T001 [AS1] Remove Heroku MCP submodule from `.gitmodules` and working tree
  - Run: `git submodule deinit -f .gitmodules/heroku-mcp` && `git rm -f .gitmodules/heroku-mcp` && `rm -rf .gitmodules/heroku-mcp`
- [ ] T002 [AS1] Verify `git submodule update --recursive` completes with zero SSH errors
  - Run: `git submodule update --recursive --init` and confirm no heroku entries in output
- [ ] T003 [AS1] Remove Heroku MCP references from `.mcp.json` if present
  - Edit: `.mcp.json` to remove any heroku-mcp server entries

**Checkpoint**: Fresh clone + `git submodule update --recursive` reports zero SSH errors and `grep -r heroku .gitmodules` returns empty

---

## Phase 2: Provider Enumeration Bridge (AS2)

**Purpose**: Implement provider-to-Toolkit bridge exposing each HelixLLM provider as distinct Toolkit entry

- [ ] T004 [AS2] Create HelixLLM provider adapter in `helix_code/internal/provider/bridge.go`
  - Implement `ProviderBridge` struct with `ListProviders()` returning all HelixLLM providers
  - Use real HelixLLM `ModelManager` to enumerate providers (no stubs)
- [ ] T005 [AS2] Implement provider metadata extraction (context window, capabilities)
  - Use live HelixLLM registry as source of truth per CONST-036
- [ ] T006 [AS2] [P] Create Toolkit provider registry entries in `claude_toolkit/providers/helixllm/`
  - Generate one JSON config per HelixLLM provider with name, endpoint, capabilities
- [ ] T007 [AS2] Write integration test for provider enumeration in `helix_code/internal/provider/bridge_test.go`
  - Test: `ListProviders()` returns ≥2 distinct HelixLLM providers
  - Test: Each provider has correct name, type, endpoint, capabilities

**Checkpoint**: Toolkit provider enumeration lists ≥2 distinct HelixLLM providers with correct metadata

---

## Phase 3: Toolkit → HelixLLM Routing (AS3, AS4)

**Purpose**: Implement routing so selecting a provider triggers real inference via HelixLLM

- [ ] T008 [AS3] Implement routing adapter in `helix_code/internal/provider/router.go`
  - Route Toolkit completion requests to HelixLLM `ModelManager.Generate()`
  - Return genuine provider response (not stub)
- [ ] T009 [AS4] Implement model metadata endpoint in `helix_code/internal/provider/metadata.go`
  - Expose context window, capabilities per provider matching live registry
- [ ] T010 [AS3,AS4] Write integration tests for routing correctness
  - Test: `openai` provider selection returns response with OpenAI signature (not stub)
  - Test: Model metadata matches live HelixLLM registry for each provider

**Checkpoint**: Completion request routed to `openai` provider returns response originating from OpenAI via HelixLLM (verified by response signature), not a stub. Model metadata matches live registry.

---

## Phase 4: Alias Resolution Fix (AS5)

**Purpose**: Fix `kimi`/`kimi1`/`kimi2` aliases to resolve to valid endpoints, eliminate context-compacting loop

- [ ] T011 [AS5] Investigate `kimi`/`kimi1`/`kimi2` alias regression
  - Trace alias resolution path in helix_code and claude_toolkit
  - Identify cause of endless context-compacting loop
- [ ] T012 [AS5] Fix alias resolution to point to valid Moonshot/Kimi endpoints
  - Update alias mapping in `helix_code/config/model-aliases.yaml` or equivalent
  - Ensure health-check endpoint returns 200 OK
- [ ] T013 [AS5] Add regression test for alias resolution
  - Test: `kimi`, `kimi1`, `kimi2` resolve to valid endpoints
  - Test: Health-check endpoint returns 200 OK
  - Test: No context-compacting loop triggered during resolution

**Checkpoint**: Alias `kimi` (and `kimi1`, `kimi2`) resolves to valid endpoint; health-check returns 200 OK; no context-compacting loop triggered

---

## Phase 5: Provider-Aliases Regeneration (AS6)

**Purpose**: Regenerate provider-aliases file from live HelixLLM registry via single command

- [ ] T014 [AS6] Implement `helix config regenerate-aliases` command in `helix_code/cmd/helix-config/`
  - Read live HelixLLM provider registry
  - Generate `config/provider-aliases.yaml` with all providers and aliases
- [ ] T015 [AS6] [P] Test regeneration command produces output matching live registry exactly
  - Run command and diff output against live registry source

**Checkpoint**: Provider-aliases regeneration command completes and output file matches live registry content exactly

---

## Phase 6: Documentation Sync (AS7)

**Purpose**: Update README and provider-capability docs

- [ ] T016 [AS7] [P] Update `helix_code/README.md` with current provider list
- [ ] T017 [AS7] [P] Update `claude_toolkit/docs/providers.md` with HelixLLM provider capabilities
- [ ] T018 [AS7] [P] Remove any stale provider references from docs

**Checkpoint**: All providers listed in docs; no stale references

---

## Phase 7: Reboot Persistence (AS8)

**Purpose**: Persist provider registration across reboot

- [ ] T019 [AS8] Create systemd service unit for HelixLLM provider registration
  - File: `/etc/systemd/system/helixllm-provider-registration.service`
  - ExecStart: provider registration command
  - After: network.target, helixllm.service
- [ ] T020 [AS8] Implement provider registration on startup in `helix_code/internal/provider/startup.go`
  - Register all providers within 30s of HelixLLM start
- [ ] T021 [AS8] Test reboot persistence
  - Simulate reboot (restart service) and verify all providers registered within 30s

**Checkpoint**: After system reboot + HelixLLM start, all providers show registered status within 30s

---

## Phase 8: Deterministic Verification Script (AS9)

**Purpose**: Provide verification script that produces pass/fail per provider; all pass when HelixLLM running

- [ ] T022 [AS9] Create verification script `helix_code/scripts/verify-providers.sh`
  - Check each provider: enumeration, metadata, routing, health
  - Exit 0 if all pass, non-zero on any failure
  - Output: pass/fail per provider with details
- [ ] T023 [AS9] Add verification script to CI pipeline
  - File: `.github/workflows/verify-providers.yml` (if CI enabled per CONST-052)
- [ ] T024 [AS9] Test verification script
  - Run with HelixLLM running: all providers must pass
  - Run with stub/broken provider: script must fail with clear error

**Checkpoint**: Verification script exits 0 with all provider checks passing; non-zero exit on any failure

---

## Phase 9: Polish & Cross-Cutting

**Purpose**: Improvements affecting multiple scenarios

- [ ] T025 [P] Run full test suite: `make test` in helix_code, `npm test` in claude_toolkit
- [ ] T026 [P] Run lint: `make lint` in helix_code
- [ ] T027 [P] Verify no hardcoded provider lists remain (grep for hardcoded models)
- [ ] T028 [P] Verify no Heroku references remain in codebase
- [ ] T029 [P] Update `docs/qa/` with test evidence per §11.4.153

---

## Dependencies & Execution Order

### Phase Dependencies

- **Phase 1 (Heroku Removal)**: No dependencies - can start immediately
- **Phase 2 (Enumeration Bridge)**: Depends on Phase 1 completion
- **Phase 3 (Routing)**: Depends on Phase 2 completion
- **Phase 4 (Alias Fix)**: Can start after Phase 1 (independent)
- **Phase 5 (Alias Regen)**: Depends on Phase 2 (needs live registry)
- **Phase 6 (Docs)**: Can run in parallel after Phase 2-4
- **Phase 7 (Reboot Persistence)**: Depends on Phase 2 (needs registration logic)
- **Phase 8 (Verification Script)**: Depends on Phase 2-4 (needs all providers working)
- **Phase 9 (Polish)**: Depends on all previous phases

### Within Each Phase

- Tasks marked [P] can run in parallel
- Tests MUST be written and FAIL before implementation (TDD)

---

## Parallel Example: Phase 2 Provider Enumeration

```bash
# Launch all tasks together:
Task: "Create HelixLLM provider adapter in helix_code/internal/provider/bridge.go"
Task: "Create Toolkit provider registry entries in claude_toolkit/providers/helixllm/"
Task: "Write integration test for provider enumeration in helix_code/internal/provider/bridge_test.go"
```

---

## Implementation Strategy

### MVP First (Scenarios AS1-AS4)

1. Complete Phase 1: Heroku Removal
2. Complete Phase 2: Enumeration Bridge
3. Complete Phase 3: Routing
4. **STOP and VALIDATE**: Run verification script, confirm ≥2 providers work end-to-end
5. Complete Phase 4: Alias Fix
6. **STOP and VALIDATE**: Confirm kimi/kimi1/kimi2 work

### Incremental Delivery

1. Phases 1-3 → Foundation ready (provider enumeration + routing)
2. Phase 4 → Alias fix adds critical user-facing capability
3. Phase 5 → Alias regen adds automation
4. Phase 6 → Docs add usability
5. Phase 7 → Reboot persistence adds reliability
6. Phase 8 → Verification script adds confidence
7. Phase 9 → Polish adds production readiness