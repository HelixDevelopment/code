# Implementation Plan: HelixLLM Provider Toolkit Binding

**Branch**: `005-helixllm-provider-toolkit-binding` | **Date**: 2026-09-10 | **Spec**: specs/005-helixllm-provider-toolkit-binding/spec.md
**Input**: Feature specification from `specs/005-helixllm-provider-toolkit-binding/spec.md`

## Summary

Expose all HelixLLM providers (15+) as distinct Claude Toolkit entries with real routing (no stubs). Fix `kimi`/`kimi1`/`kimi2` alias regression (endless context-compacting loop). Remove Heroku MCP submodule. Persist provider registration across reboot. Provide deterministic verification script.

## Technical Context

**Language/Version**: Go 1.24 (helix_code), TypeScript/Node (Claude Toolkit)
**Primary Dependencies**: HelixLLM (internal/llm), Claude Toolkit provider registry
**Storage**: File-based provider-aliases.yaml, systemd service for persistence
**Testing**: Go test (helix_code), Node test (Claude Toolkit), deterministic verification script
**Target Platform**: Linux (primary), cross-platform via containers
**Project Type**: CLI tool binding / provider integration
**Performance Goals**: Provider registration <30s post-reboot; verification script <10s full run
**Constraints**: No upstream Claude Toolkit code changes; no upstream HelixLLM core changes; only binding layer

## Constitution Check

*GATE: Must pass before proceeding. Re-check after design phase.*

| Principle | Status | Notes |
|-----------|--------|-------|
| Anti-bluff (CONST-035) | PASS | All verification uses real HelixLLM calls, no stubs |
| LLMsVerifier SSoT (CONST-036) | PASS | Provider metadata sourced from live HelixLLM registry |
| Real-time model status (CONST-038) | PASS | Health checks use live endpoints |
| All providers integration (CONST-039) | PASS | Bridge exposes all 15+ HelixLLM providers |
| No hardcoded content (CONST-046) | PASS | Aliases regenerated from live registry |
| Submodule hygiene (CONST-053) | PASS | Heroku submodule removed, no build artifacts tracked |

## Project Structure

### Documentation (this feature)

```text
specs/005-helixllm-provider-toolkit-binding/
├── spec.md              # Feature specification
├── plan.md              # This file
├── tasks.md             # Task breakdown
└── checklist-*.md       # Generated checklists
```

### Source Code (repository root)

```text
helix_code/
├── internal/llm/              # HelixLLM provider implementations
├── internal/provider/         # Provider abstractions
├── internal/tools/            # Tool ecosystem (for verification script)
└── cmd/helix-config/          # Config management CLI (for alias regen)

claude_toolkit/
├── providers/                 # Provider registry entries
├── config/                    # Configuration management
└── verification/              # Verification scripts
```

**Structure Decision**: Bridge lives in helix_code as adapter layer; Toolkit entries are generated configs in claude_toolkit/providers/

## Execution Strategy

### TDD Requirements

- [ ] Provider enumeration: Must verify ≥2 providers returned via real HelixLLM call
- [ ] Routing correctness: Must verify OpenAI response signature, not stub
- [ ] Alias resolution: Must verify kimi/kimi1/kimi2 resolve to valid endpoints with 200 OK health
- [ ] Verification script: Must fail on stub, pass on real response

### Parallel Execution Opportunities

- [ ] Heroku submodule removal (independent git operation)
- [ ] Provider-aliases regeneration (independent CLI command)
- [ ] Documentation updates (independent files)
- [ ] Verification script development (independent test file)

### Human Checkpoints

1. After Heroku removal — verify `git submodule update --recursive` clean
2. After bridge implementation — verify provider enumeration returns ≥2 providers
3. After alias fix — verify kimi/kimi1/kimi2 resolve + health check passes
4. After verification script — run full script, all providers must pass
5. Before merge — final review against spec acceptance scenarios

### Review Gates

- [ ] Provider enumeration API contract: Review before Toolkit integration
- [ ] Alias resolution logic: Review before session persistence
- [ ] Verification script: Review before CI integration

## Complexity Tracking

> No constitution violations - all requirements align with constraints