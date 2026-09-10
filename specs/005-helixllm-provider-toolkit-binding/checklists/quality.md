# Quality Checklist — 005: HelixLLM Provider Toolkit Binding

## Spec Metadata
| Field | Value |
|-------|-------|
| Feature | 005-helixllm-provider-toolkit-binding |
| Date | 2026-09-10 |
| Checklist template | 40-line, CHK### items, category-grouped |

---

## 1. Heroku MCP Submodule Removal (GAP-1)

- **CHK001** Heroku submodule directory removed from `.gitmodules` and working tree
- **CHK002** `.git/config` cleaned of heroku-related entries
- **CHK003** `git submodule deinit` + `git rm` executed for the heroku submodule
- **CHK004** No references to heroku MCP submodule remain in source files, docs, or configs
- **CHK005** `.mcp.json` contains no heroku-related server entries
- **CHK006** Build/test passes after removal (`make build` + `make test`)
- **CHK007** Commit includes submodule removal diff and updated `.gitmodules`

## 2. Provider Exposure (GAP-2)

- **CHK008** `internal/llm/providers/` directory contains provider implementations for all listed providers
- **CHK009** Each provider implementation exposes a functional `GetModels()` or equivalent endpoint
- **CHK010** Provider health-check endpoints return real status (not stubbed/hardcoded)
- **CHK011** Model listing endpoints return models from live registry, not hardcoded arrays
- **CHK012** Alias resolution uses live registry as single source of truth (no static fallback lists)
- **CHK013** HelixAgent and HelixLLM appear in provider model listings when running
- **CHK014** Provider bridge adapter layer is thin (no business logic duplication)

## 3. Claude Toolkit Binding (GAP-3)

- **CHK015** Claude Toolkit config (`.mcp.json` or equivalent) points to HelixLLM provider bridge
- **CHK016** opencode CLI can discover HelixLLM providers via the bridge
- **CHK017** pi CLI can discover HelixLLM providers via the bridge
- **CHK018** Model selection from Claude Toolkit correctly resolves to HelixLLM provider
- **CHK019** End-to-end request: Claude Toolkit → bridge → HelixLLM provider → LLM response works
- **CHK020** No hardcoded host/port in bridge configuration (config-driven)

## 4. Kimi Alias Regression (GAP-4)

- **CHK021** Kimi alias resolves to correct model ID in alias registry
- **CHK022** Kimi alias resolution produces expected output in provider dispatch
- **CHK023** No other aliases regressed during Kimi fix (full alias registry test)
- **CHK024** Alias registry test covers all current aliases with positive + negative cases

## 5. Documentation Drift (GAP-5)

- **CHK025** `docs/` updated to reflect removed heroku submodule
- **CHK026** Provider exposure docs match actual running providers
- **CHK027** Claude Toolkit integration docs match actual bridge behavior
- **CHK028** Alias documentation matches registry entries
- **CHK029** No stale references to removed/replaced components

## 6. Verification Script (CI-runnable)

- **CHK030** Verification script exists and is executable (`scripts/verify-provider-availability.sh` or equivalent)
- **CHK031** Script exits 0 on success, non-zero on failure (exit-code semantics)
- **CHK032** Script can be run via CI pipeline without manual intervention
- **CHK033** Script checks all P1 user stories pass before exit 0
- **CHK034** Script output includes per-provider status (available/unavailable + evidence)

## 7. Reboot Survival

- **CHK035** HelixLLM provider service survives host reboot (systemd unit or equivalent)
- **CHK036** Provider bridge reconnects after restart without manual intervention
- **CHK037** Claude Toolkit discovers providers after fresh boot (no stale state)

## 8. Release & Tagging

- **CHK038** All commits pushed to all upstreams recursively
- **CHK039** Version tag created on helix_code with proper semver bump
- **CHK040** Version tag created on affected submodules
- **CHK041** Release notes capture all 5 gaps addressed

---

## Checklist Summary
| Category | Items | Covered Stories |
|----------|-------|----------------|
| Heroku removal | CHK001–CHK007 | GAP-1 |
| Provider exposure | CHK008–CHK014 | GAP-2 |
| Claude Toolkit binding | CHK015–CHK020 | GAP-3 |
| Kimi alias regression | CHK021–CHK024 | GAP-4 |
| Documentation drift | CHK025–CHK029 | GAP-5 |
| Verification script | CHK030–CHK034 | CI requirement |
| Reboot survival | CHK035–CHK037 | Reliability |
| Release & tagging | CHK038–CHK041 | Delivery |
| **Total** | **41 items** | **8 user stories** |
