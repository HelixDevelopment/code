# Root Cause Analysis: Native → Provider Alias Session Compaction Loop

**Date:** 2026-09-03  
**Session:** `9bf7353c-587d-44d6-9612-7d643b56c74d.jsonl`  
**Location:** `~/.claude-shared/projects/-home-milosvasic-Projects-helix-code/`  
**Total messages in session:** 18,468  
**Target alias transition:** native `claude1` → provider `deepseek`  

---

## Executive Summary

When a Claude Code session that grew to ~1 M tokens under a **native alias** is resumed under a **provider alias** (`deepseek`), the provider alias enforces a much smaller context window guard (`CLAUDE_CODE_AUTO_COMPACT_WINDOW`). This causes the resumed session to immediately compact, and then compact again on every substantive turn, because normal tool-call results quickly refill the context past the forced window.

**This is not an error/retry loop.** The compaction requests themselves succeed. It is a **window-mismatch shock** produced by the alias subsystem's token-guard math.

**Recommendation:** Do not resume very large native sessions under provider aliases whose `CMA_AUTO_COMPACT_CAP` is smaller than the session's current context. Either raise the cap for 1 M-context providers, warn the operator, or treat such resumes as an unsupported transition.

---

## Key Findings

### 1. Native alias removes context-token guards

`scripts/lib.sh:1149` inside the native alias launcher (`cma_run()`) explicitly unsets the three context guards:

```bash
unset CLAUDE_CODE_AUTO_COMPACT_WINDOW
unset CLAUDE_CODE_MAX_OUTPUT_TOKENS
unset CLAUDE_CODE_MAX_CONTEXT_TOKENS
```

This allows a native session to grow naturally until the model/backend enforces its own limit (observed compacts at ~977 k–1,002 k tokens).

### 2. Provider alias computes aggressive guards from provider metadata

`scripts/lib.sh:1345–1542` inside `cma_run_provider()` derives the guards from the provider's declared context and output limits.

For `deepseek` the provider metadata reports:

- `CMA_PROVIDER_CONTEXT_LIMIT = 1000000` (1 M tokens)
- `CMA_PROVIDER_MAX_OUTPUT = 128000` (128 k tokens)

The alias then sets:

- `CLAUDE_CODE_MAX_OUTPUT_TOKENS = 128000`
- `CLAUDE_CODE_AUTO_COMPACT_WINDOW = 176000`

The 176 k compact window comes from the alias's `CMA_AUTO_COMPACT_CAP` of 200 k tokens, minus a 24 k safety margin.

### 3. The resumed session was far above the new compact window

The session under analysis had been compacting under the native alias at pre-compaction sizes around **977 k–1,002 k tokens**. When resumed under the `deepseek` provider alias, the effective compact window dropped to **176 k tokens**, creating an immediate context-window shock.

### 4. Two rapid compacts were observed within ~80 seconds

| Message index | Timestamp (UTC) | Pre-compact tokens | Post-compact tokens | Notes |
|---|---:|---:|---:|---|
| `[18411]` | 2026-09-03T17:37:50Z | 977,450 | 35,493 | First compact after resume under deepseek |
| `[18443]` | 2026-09-03T17:39:11Z | 190,884 | 33,137 | Second compact ~80 s later |

The first compact is explainable by the window drop. The second compact is explainable by normal turn traffic (tool calls + results) refilling the context from 35 k back past the 176 k window in roughly one turn.

### 5. Compaction requests succeed; no retry storm

The Claude Code Router service log for deepseek (`~/.claude-code-router/deepseek/service.log`) shows `POST /v1/messages` returning HTTP 200 with ~62 kB responses. There are no retries, no errors, and no rate-limit responses. The compactions are functionally successful.

### 6. Tool-schema shrinkage is not the driver

Plugin/tool count dropped from 274 under the native alias to 4 under the provider alias. However, the final compaction metadata lists only four discovered tools (`Monitor`, `SendMessage`, `WebFetch`, `WebSearch`), confirming that the reduced tool set is stable and not causing repeated schema-related compactions.

### 7. Shared-state mechanism is confirmed

Both alias profiles share the same project entry in their `.claude.json` state files:

- `~/.claude-claude1/.claude.json`
- `~/.claude-prov-deepseek/.claude.json`

Both reference `/home/milosvasic/Projects/helix_code`, which is how the same session state is visible after the alias switch.

---

## Token-Guard Math for Deepseek Provider Alias

```
CMA_PROVIDER_CONTEXT_LIMIT = 1,000,000 tokens
CMA_PROVIDER_MAX_OUTPUT    =   128,000 tokens
CMA_AUTO_COMPACT_CAP       =   200,000 tokens   (alias-level cap)
SAFETY_MARGIN              =    24,000 tokens

CLAUDE_CODE_MAX_OUTPUT_TOKENS  = CMA_PROVIDER_MAX_OUTPUT
                               = 128,000 tokens

CLAUDE_CODE_AUTO_COMPACT_WINDOW = CMA_AUTO_COMPACT_CAP - SAFETY_MARGIN
                                = 200,000 - 24,000
                                = 176,000 tokens
```

So the provider alias intentionally compacts whenever the context exceeds **176 k tokens**, regardless of the model's nominal 1 M-token context limit.

---

## Contrast with Native Behavior

| Dimension | Native alias (`claude1`) | Provider alias (`deepseek`) |
|---|---|---|
| `CLAUDE_CODE_AUTO_COMPACT_WINDOW` | **unset** | **176,000** |
| `CLAUDE_CODE_MAX_OUTPUT_TOKENS` | **unset** | **128,000** |
| `CLAUDE_CODE_MAX_CONTEXT_TOKENS` | **unset** | derived from provider |
| Observed compact trigger | ~977 k–1,002 k tokens | 176 k tokens |
| Plugin/tool count | 274 | 4 |

Because the native alias unsets the guards, the session grew to nearly 1 M tokens. Switching to the provider alias suddenly imposes a 176 k window, so every turn that adds meaningful content crosses the threshold.

---

## Why This Looks Like an "Endless" Loop

After the first forced compaction reduces the context to ~35 k tokens, a single substantial turn can add ~150 k tokens of tool results, system state, and reasoning. That pushes the context back above 176 k, triggering another compaction. From the operator's perspective this resembles a loop, but it is the expected behavior of a session whose natural working set is larger than the configured compact window.

The session was exited at message `[18467]`, so only two compacts were captured, but the pattern is consistent with **"compact on every substantive turn"** rather than a deterministic infinite loop.

---

## Recommendations

1. **Raise or per-provider the `CMA_AUTO_COMPACT_CAP`.** For providers advertising 1 M-token context windows (deepseek, and likely xiaomi/openrouter/nvidia with similar metadata), a 200 k cap minus 24 k safety is overly aggressive. Consider a higher cap or a cap that is a function of the provider's context limit rather than a fixed global value.

2. **Warn or block resume when context exceeds the target alias window.** Before switching a native session to a provider alias, compare the session's current token count to the provider alias's `CLAUDE_CODE_AUTO_COMPACT_WINDOW`. If the session is larger, warn the operator that repeated compactions will occur, or refuse the resume.

3. **Document the alias-specific guard behavior.** The difference between native and provider alias context management (`scripts/lib.sh:1149` vs. `scripts/lib.sh:1345–1542`) should be documented so operators understand why a resumed session compacts aggressively.

4. **Audit other 1 M-context providers.** The same provider math applies to any provider whose metadata declares a large context limit but whose alias still uses `CMA_AUTO_COMPACT_CAP=200000`. Check xiaomi, openrouter, nvidia, and similar aliases for the same risk.

5. **Add observability.** Emit a one-time warning when a resumed session's pre-compact token count is more than, say, 2× the configured compact window, so the operator is not surprised by repeated compactions.

---

## Evidence Summary

- `~/.claude-shared/projects/-home-milosvasic-Projects-helix-code/9bf7353c-587d-44d6-9612-7d643b56c74d.jsonl` — 18,468 messages; deepseek messages at end.
- `scripts/lib.sh:1149` — native alias unsets context guards.
- `scripts/lib.sh:1345–1542` — provider alias computes and exports guards.
- `~/.claude-code-router/deepseek/service.log` — successful `POST /v1/messages` compactions, no retries.
- `~/.claude-claude1/.claude.json` and `~/.claude-prov-deepseek/.claude.json` — shared project entry.

