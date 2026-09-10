---
# Specification metadata — populate before speckit.clarify
spec_id: "005"
title: "HelixLLM Provider-Toolkit Binding"
status: "draft"
priority: "critical"
created: "2026-09-10"
authors: ["operator", "agent"]
---

## Background

Spec 004 established the initial HelixLLM ↔ Claude Toolkit integration path (provider registry sync, alias bridging, local-LLM command delegation). However, during subsequent live-validation cycles, five concrete gaps surfaced:

1. **Heroku MCP Submodule** — a dead submodule still tracked in `.gitmodules`, consuming clone time and producing SSH failures on every `git submodule update --recursive`. Must be removed.
2. **Provider Exposure Gap** — HelixLLM's registered providers (15+) are not surfacing their available models through the Claude Toolkit provider list. The Toolkit sees "HelixLLM" as a single opaque provider, not as a multi-provider registry.
3. **Claude Toolkit Binding Gap** — No mechanism allows the Toolkit to enumerate, select, or route inference through individual HelixLLM providers (e.g., `anthropic`, `openai`, `ollama`) as distinct entries.
4. **Kimi Alias Regression** — The `kimi` model alias, previously functional, now resolves to a non-existent endpoint, breaking all sessions that use it.
5. **Documentation Drift** — Provider-capability docs, README references, and the provider-aliases file are out of sync with the actual provider registry.

This spec closes all five gaps as a single atomic deliverable, ensuring Helix family solutions survive reboot, expose all providers/models through Claude Toolkit, and pass exhaustive deterministic validation.

## User Stories

### P1 — Critical Path (must ship)

**US-001: Remove Heroku MCP submodule from tracked state**
> As a developer cloning HelixCode, I want the Heroku MCP submodule removed from `.gitmodules` and the tree, so that `git submodule update --recursive` completes without SSH failures.

Given the repository is freshly cloned
When `git submodule update --recursive` is run
Then no SSH connection errors occur related to Heroku
And no Heroku-related entries remain in `.gitmodules`

**US-002: Expose all HelixLLM providers as distinct Toolkit entries**
> As a Claude Toolkit user, I want each HelixLLM provider (anthropic, openai, ollama, etc.) to appear as a separate, selectable provider in the Toolkit's provider list, so I can choose which backend to route inference through.

Given HelixLLM is running and registered
When the Claude Toolkit queries available providers
Then each HelixLLM provider appears as a distinct entry with its provider name
And each entry lists its available models with correct context-window and capability metadata

**US-003: Bind Claude Toolkit provider selection to HelixLLM routing**
> As a Claude Toolkit user, I want selecting a HelixLLM provider in the Toolkit to route my inference request through that provider's actual endpoint, so I get real model responses, not stubs.

Given provider `ollama` is selected in the Toolkit
When a completion request is sent
Then the request is routed to HelixLLM's ollama provider endpoint
And the response is a genuine model output from the ollama-hosted model
And the response includes provider attribution metadata

**US-004: Fix kimi model alias resolution**
> As a user with existing sessions referencing the `kimi` alias, I want the alias to resolve to a valid endpoint, so my sessions do not break.

Given a session references the `kimi` model alias
When the alias is resolved
Then it maps to a valid, reachable HelixLLM provider endpoint
And the endpoint returns a successful health-check within 5 seconds

### P2 — Important (should ship)

**US-005: Synchronize provider-aliases file with live registry**
> As a developer, I want the provider-aliases file to match the actual provider registry exactly, so documentation and tooling do not reference stale or missing aliases.

Given the HelixLLM provider registry is running
When the provider-aliases file is regenerated
Then every alias in the file maps to a provider present in the live registry
And no alias in the file is missing from the live registry

**US-006: Update README and provider-capability docs**
> As a contributor, I want the README and provider-capability documentation to accurately reflect all available providers, their models, and routing behavior.

Given the provider integration is complete
When I read `README.md` or `docs/providers/`
Then the listed providers match the live registry
And the described routing behavior matches the implemented behavior
And no references to Heroku remain

**US-007: Survive reboot with persistent provider registration**
> As an operator, I want all HelixLLM providers to re-register automatically after a system reboot, so the Toolkit's provider list is restored without manual intervention.

Given all HelixLLM providers were registered before a reboot
When the system reboots and HelixLLM starts
Then all providers re-register within 30 seconds of startup
And the Claude Toolkit can enumerate all providers within 60 seconds of HelixLLM reaching healthy state

### P3 — Nice to have (may defer)

**US-008: Model-capability verification per provider**
> As a developer debugging provider integration, I want a deterministic script that queries each provider for its model list and verifies capability metadata matches expectations.

Given HelixLLM is running
When the verification script is executed
Then it produces a pass/fail report per provider
And each pass includes the provider name, model count, and a sample response hash

## Acceptance Scenarios

| Scenario | Given | When | Then |
|----------|-------|------|------|
| No Heroku remnants | Fresh clone | `git submodule update --recursive` | Zero SSH errors; no heroku entry in `.gitmodules` |
| Provider enumeration | HelixLLM running | Toolkit queries providers | ≥2 distinct HelixLLM providers listed |
| Model metadata | Provider listed | Toolkit reads model list | Context-window, capabilities match live registry |
| Routing correctness | `openai` provider selected | Send completion request | Response from OpenAI via HelixLLM, not stub |
| Alias resolution | Session uses `kimi` alias | Resolve alias | Valid endpoint returned; health-check OK |
| Reboot persistence | Pre-reboot state saved | System reboot + HelixLLM start | All providers re-registered within 30s |
| Documentation sync | Provider list complete | Check README/docs | All providers listed; no stale references |
| Verification script | HelixLLM running | Run script | Pass/fail per provider; all pass |

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: Remove Heroku MCP submodule from `.gitmodules` and working tree; `git submodule update --recursive` completes with zero SSH errors and no heroku entry remains
- **FR-002**: Implement provider-to-Toolkit bridge exposing each HelixLLM provider as a distinct Claude Toolkit entry; Toolkit queries return ≥2 distinct HelixLLM providers
- **FR-003**: Implement Toolkit → HelixLLM routing so selecting a provider (e.g., `openai`) triggers real inference via HelixLLM, returning a genuine provider response, not a stub
- **FR-004**: Expose model metadata (context window, capabilities) for each listed provider matching the live HelixLLM registry
- **FR-005**: Fix `kimi`/`kimi1`/`kimi2` aliases to resolve to valid endpoints with passing health checks; eliminate the endless context-compacting loop regression
- **FR-006**: Regenerate provider-aliases file from the live HelixLLM registry via a single command
- **FR-007**: Persist provider registration across reboot; all providers re-registered within 30s of HelixLLM start after system reboot
- **FR-008**: Provide a deterministic verification script that produces pass/fail per provider; all providers must pass when HelixLLM is running

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: `git submodule update --recursive` on a fresh clone reports zero SSH errors and `grep -r heroku .gitmodules` returns empty
- **SC-002**: Toolkit provider enumeration lists ≥2 distinct HelixLLM providers with correct names
- **SC-003**: Model metadata query returns context-window and capability fields matching the live HelixLLM registry for each provider
- **SC-004**: Completion request routed to `openai` provider returns a response originating from OpenAI via HelixLLM (verified by response signature), not a stub
- **SC-005**: Alias `kimi` (and `kimi1`, `kimi2`) resolves to a valid endpoint; health-check endpoint returns 200 OK; no context-compacting loop triggered
- **SC-006**: Provider-aliases regeneration command completes and output file matches live registry content exactly
- **SC-007**: After system reboot + HelixLLM start, all providers show registered status within 30s
- **SC-008**: Verification script exits 0 with all provider checks passing; non-zero exit on any failure

## Scope

### In Scope
- Remove Heroku MCP submodule from `.gitmodules` and working tree
- Implement provider-to-Toolkit bridge (each HelixLLM provider → distinct Toolkit entry)
- Implement Toolkit → HelixLLM routing (selection triggers real inference)
- Fix `kimi` alias to point to valid endpoint
- Regenerate provider-aliases file from live registry
- Update README and provider-capability docs
- Persist provider registration across reboot
- Deterministic verification script

### Out of Scope
- Changes to upstream Claude Toolkit code (consumed as-is)
- Changes to upstream HelixLLM core (only binding layer)
- New LLM providers beyond those already in HelixLLM registry
- GUI/TUI modifications

## Technical Notes
- The provider bridge must be a thin adapter layer — no business logic, no caching, no transformation beyond format mapping
- Alias resolution must use the same source of truth as the live registry (no hardcoded alias tables)
- Provider-aliases regeneration must be a single-command operation (`make sync-aliases` or equivalent)
- Verification script must be runnable in CI and locally; exit code 0 = all pass, non-zero = failures
- Reboot persistence requires systemd-style or equivalent service registration for HelixLLM

## Risks
- Removing Heroku submodule may affect any downstream scripts that reference it (mitigate: grep for references before removal)
- Provider bridge adds a new failure surface between Toolkit and HelixLLM (mitigate: health-check + timeout + error propagation)
- Alias fix may change behavior for existing sessions (mitigate: verify no external consumers depend on current broken behavior)

## Dependencies
- HelixLLM provider registry (existing, 15+ providers)
- Claude Toolkit provider interface (existing)
- `.mcp.json` configuration (existing, may need unwiring of Heroku references)
