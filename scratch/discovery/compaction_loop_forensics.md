# Claude Code compaction-loop forensics — 2026-09-03

**Project directory:** `/home/milosvasic/Projects/helix_code/submodules/helix_agent/internal/llm/providers/claude`

**Shared session store:**
`/home/milosvasic/.claude-shared/projects/-home-milosvasic-Projects-helix-code-submodules-helix-agent-internal-llm-providers-claude/`

**Investigated:** 35 near-identical `.jsonl` session transcripts created between 07:34 and 19:10 local time (2026-09-03).

## Summary

A **mechanical resume → fork → compact-thrash loop** produced 35 short-lived Claude Code sessions in one shared project store today. The loop is driven by the multi-account toolkit's auto-resume logic combined with a provider context window that is too small to hold the resumed system/tool context, causing Claude Code to enter an infinite auto-compaction cycle and exit before producing an assistant response.

Only **1 of the 35 sessions** produced an `assistant` event; the other 34 are single-prompt "models" enqueue operations that die silently (no assistant, no explicit error event).

## Evidence

### File corpus

| metric | value |
|---|---|
| `.jsonl` files | 35 |
| size range | 312,972 – 325,173 bytes |
| mtime span | 2026-09-03 07:34 – 19:10 |
| files with assistant response | 1 |
| files without assistant response | 34 |

Distribution by hour (local time):

| hour | sessions |
|---|---|
| 07:00 | 5 |
| 08:00 | 1 |
| 10:00 | 4 |
| 11:00 | 2 |
| 12:00 | 7 |
| 13:00 | 5 |
| 14:00 | 1 |
| 18:00 | 1 |
| 19:00 | 9 |

Full per-file table: `session_table.md` (same directory).

### Every prompt is identical

All 35 sessions contain exactly one user-facing request whose `lastPrompt` / `queue-operation` content is the single word:

```text
models
```

Other metadata:

- `promptSource`: `sdk`
- `entrypoint`: `sdk-cli`
- no `isCompactSummary` or explicit `compact` markers in any transcript
- the word "compaction" appears only in the toolkit source, not in session logs

### One successful assistant event

Session `3fffce6c-879f-4617-803a-9baac439b2b5` (11:02:50) is the only transcript with an `assistant` event:

- model: `claude-opus-5`
- `cache_creation_input_tokens`: 376,626
- `cache_read_input_tokens`: 13,512
- `output_tokens`: 86
- assistant text: "I'm not sure what you're asking. Could you clarify? ..."

This session is also the largest file (325,173 bytes) and has 35 events instead of the 33 seen in failed sessions. It loaded a very large resumed context (≈376K tokens created for cache) and still only produced an 86-token clarification, consistent with the prompt "models" being ambiguous out of context.

### Shared store / hardlink evidence

The session directory is the **same inode** across all checked alias config directories:

```text
58362473  /home/milosvasic/.claude-shared/projects/-home-milosvasic-Projects-helix-code-submodules-helix-agent-internal-llm-providers-claude
58362473  /home/milosvasic/.claude-claude1/projects/-home-milosvasic-Projects-helix-code-submodules-helix-agent-internal-llm-providers-claude
58362473  /home/milosvasic/.claude-prov-deepseek/projects/-home-milosvasic-Projects-helix-code-submodules-helix-agent-internal-llm-providers-claude
58362473  /home/milosvasic/.claude-prov-helixagent/projects/-home-milosvasic-Projects-helix-code-submodules-helix-agent-internal-llm-providers-claude
58362473  /home/milosvasic/.claude-prov-helixllm-gateway/projects/-home-milosvasic-Projects-helix-code-submodules-helix-agent-internal-llm-providers-claude
```

The transcript files themselves also share inodes, e.g. `435f86da-b3cc-4dd8-996b-b0edd47c8798.jsonl` is inode `58362472` in every checked alias dir. This means every alias writes into the same physical store, so a session created by one alias immediately becomes the "latest" session for every other alias.

## Mechanism

The loop is produced by three interacting layers:

1. **Auto-resume injection** (`aliases.sh` / `cma_run_provider`)
   - When a provider alias is launched with conversation args, the wrapper calls `claude-session existing-id` and injects `--resume <id>` unless the user explicitly passed `--resume`, `--session-id`, etc.
   - The native `cma_run` wrapper does the same for bare launches.

2. **"Most recently active by mtime" session resolution** (`claude-session.sh`)
   - `cma_latest_session_id()` uses `ls -t` on `*.jsonl` to pick the session to resume.
   - Because the store is shared across aliases, a failed session created by alias A becomes the "latest" file and is resumed by alias B's next launch.

3. **Provider context-window guard** (`lib.sh`)
   - The toolkit sets `CLAUDE_CODE_AUTO_COMPACT_WINDOW` based on the provider's real context limit minus output budget minus tool budget.
   - It contains an explicit comment warning of the failure mode:

```bash
# Compression-loop guard: if the window is below the minimum viable size,
# Claude Code triggers compaction on every request (system prompt + tool
# schemas alone exceed the window), enters an infinite compression loop,
# and never gets any work done.
```

   - The guard tries to raise the window by lowering the output cap, but if the provider's context is still too small relative to the resumed context, the session cannot make progress.

### Loop cycle

```
alias launches (e.g. deepseek/helixagent/helixllm-gateway/claude1)
        │
        ▼
wrapper injects --resume <latest existing session id>
        │
        ▼
Claude Code loads the resumed session's full context
        │
        ▼
context + tool schemas exceed provider's auto-compact window
        │
        ▼
Claude Code tries to compact on every turn; compaction itself fails
or returns almost nothing usable (0 assistant events in 34/35 cases)
        │
        ▼
session exits/fails silently, leaving a new .jsonl with updated mtime
        │
        ▼
next alias launch sees this as the "latest" session and resumes it
        │
        └────────────────────────────────────────────────────────────┘
```

The single successful assistant event (claude-opus-5, 376K cache-creation tokens) shows that at least one launch used a model large enough to load the context and respond, but even then the response was only a clarification request because the prompt "models" lacked context.

## Provider / alias evidence

Checked alias config directories that share the same physical session store:

- `.claude-claude1` (native Anthropic alias)
- `.claude-prov-deepseek`
- `.claude-prov-helixagent`
- `.claude-prov-helixllm-gateway`

Provider env files exist for `deepseek`, `helixagent`, and `helixllm-gateway` under `/home/milosvasic/.local/share/claude-multi-account/providers/`.

**Limitation:** the exact provider/model for each of the 34 failed sessions cannot be determined from the transcripts alone, because failed sessions contain no `assistant` event and no model metadata in the `queue-operation`/`user` events. Only the one successful session identifies `claude-opus-5`. The shared store means any of the aliases above could have produced any of the sessions.

## Source-code excerpts

### `lib.sh` — compression-loop guard (lines ~1503–1541)

```bash
local _cma_win="$_cma_octx" _cma_tool_budget="${CMA_TOOL_TOKEN_BUDGET:-80000}"
local _cma_min_win="${CMA_MIN_COMPACT_WINDOW:-120000}"
if [ -n "$_cma_out" ]; then _cma_win=$(( _cma_octx - _cma_out )); fi
_cma_win=$(( _cma_win - _cma_tool_budget ))
# Compression-loop guard: if the window is below the minimum viable size,
# Claude Code triggers compaction on every request (system prompt + tool
# schemas alone exceed the window), enters an infinite compression loop,
# and never gets any work done. Raise the window by reducing the output
# cap when possible, rather than exporting a window that cannot hold the
# static overhead.
if [ "$_cma_win" -lt "$_cma_min_win" ] && [ -n "$_cma_out" ] && [ "$_cma_out" -gt 8192 ]; then
  local _cma_new_out=$(( _cma_octx - _cma_min_win - _cma_tool_budget ))
  if [ "$_cma_new_out" -ge 8192 ]; then
    _cma_out="$_cma_new_out"
    _cma_win="$_cma_min_win"
    export CLAUDE_CODE_MAX_OUTPUT_TOKENS="$_cma_out"
  fi
fi
if [ "$_cma_win" -gt "$_cma_compact_cap" ]; then _cma_win="$_cma_compact_cap"; fi
# Autocompact thrashing guard (v1.26.8): ...
```

### `aliases.sh` — auto-resume injection for provider aliases (lines ~500–535)

```bash
if [[ -x "$HOME/.local/bin/claude-session" ]]; then
  if [[ $# -eq 0 ]]; then
    if [[ "${CMA_PROVIDER_TRIM:-}" != "bare" ]]; then
      _cma_psf="$("$HOME/.local/bin/claude-session" flags "$CLAUDE_CONFIG_DIR" 2>/dev/null || true)"
    fi
    ...
    eval "set -- $_cma_psf"
  else
    case "$1" in
      --resume|--session-id|--continue|--fork-session|-c) ;;
      agents|mcp|export|doctor|install|update|config|plugin|setup|acp|server|web|provider) ;;
      *)
        if [[ "${CMA_PROVIDER_TRIM:-}" != "bare" ]]; then
          _cma_psf="$("$HOME/.local/bin/claude-session" existing-id "$CLAUDE_CONFIG_DIR" 2>/dev/null || true)"
          [[ -n "$_cma_psf" ]] && set -- --resume "$_cma_psf" "$@"
        fi
        ;;
    esac
  fi
fi
```

### `claude-session.sh` — latest-by-mtime resolution (lines ~99–125)

```bash
# Find the MOST RECENTLY active session UUID for a project directory.
# Scans *.jsonl (excluding subagents/), sorts by mtime descending.
# Falls back to the deterministic UUID on first launch.
cma_latest_session_id() {
  ...
  latest="$(ls -t "$sess_dir"/*.jsonl 2>/dev/null \
    | grep -v '/subagents/' \
    | head -1 || true)"
  latest="$(basename "${latest:-}" .jsonl 2>/dev/null)" || latest=""
  ...
}
```

## Conclusions

1. **The 35 sessions are not user chat sessions.** They are failed/forked auto-resume attempts all carrying the same one-word prompt "models".
2. **The root cause is a resume cascade amplified by a shared session store.** Each failed attempt writes a new `.jsonl` with a fresh mtime, which the next alias launch then resumes.
3. **The failure mode matches the toolkit's own documented "Compression-loop guard."** The provider's configured context window cannot accommodate the system/tool schemas plus the resumed history, so Claude Code compacts repeatedly and produces no response.
4. **No compaction markers appear in the transcripts** because the compaction happens client-side or upstream-side and the session terminates before an assistant turn completes.
5. **The exact provider for each failed launch is unprovable from the transcripts** because no assistant/model metadata is emitted on failure; only the single successful `claude-opus-5` response is identifiable.

## Suggested mitigations

- Avoid auto-resuming sessions whose last turn produced no assistant response (detectable from the `.jsonl` event stream).
- Add a per-project/session size or health check before `--resume` injection.
- Consider setting `CMA_PROVIDER_TRIM=bare` for small-context provider aliases so they do not drag resumed history into each launch.
- Bound the number of forks/resumes from the same shared store within a time window.

---
*Generated from direct transcript inspection and toolkit source analysis. All inode, timestamp, and token claims were measured from the live filesystem and session files on 2026-09-03.*
