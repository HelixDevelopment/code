# Evidence — Claude Code driving the local Helix stack, after rebuild + restart

| Field | Value |
|-------|-------|
| Revision | 1 |
| Created | 2026-09-06 |
| Last modified | 2026-09-06T07:30Z |
| Status | active |
| Status summary | The wire-facade `system` fix was rebuilt into the binary, the live server restarted onto it, and real Claude Code v2.1.263 driven against `http://127.0.0.1:8080` returned the exact requested token in three consecutive runs. The request that previously returned HTTP 400 now returns 200. Honest boundary: single-turn text works; the agentic tool loop does not, because the served model emits no native `tool_use`. |

## 1. The four-layer verification (§11.4.108)

Each layer asserted, not assumed to propagate from the one below.

| Layer | Check | Result |
|---|---|---|
| SOURCE | `system` accepts string **and** block-array; 9 guards with 10 paired mutations all verified to FAIL | GREEN |
| ARTIFACT | `bin/helixcode` md5 `8e05991c…` → **`b39385008580ccf255128cc3e8af6cbb`**; `strings bin/helixcode \| grep 'unsupported system prompt shape'` = 1 | GREEN — the fix's BYTES are in the binary |
| RUNTIME (clean target) | `md5sum /proc/<MainPID>/exe` == on-disk md5 == `b3938500…`; MainPID 6563 → 3845991 | GREEN — no stale shadow |
| USER-VISIBLE | real Claude Code returns the exact token, ×3 | GREEN |

The ARTIFACT row is the one that usually gets skipped. A source-only check would have passed
identically against the *old* running binary, which is precisely the gap §11.4.108 exists to close.

## 2. The blocker, before and after

Previously (measured, pre-rebuild):

```
POST /v1/messages?beta=true   with  "system": [{"type":"text","text":"..."}]
HTTP 400
{"error":{"message":"invalid request body: json: cannot unmarshal array into Go
 struct field anthropicMessagesRequest.system of type string"}}
```

Now, same request, live server:

```
HTTP 200
{"id":"msg_0a1915b3-…","type":"message","role":"assistant",
 "model":"qwen2.5-coder-3b-instruct-q4_k_m",
 "content":[{"type":"text","text":"FACADE_OK"}],
 "stop_reason":"end_turn","usage":{"input_tokens":25,"output_tokens":5}}
```

Note the served model: **`qwen2.5-coder-3b-instruct-q4_k_m`**, i.e. the llama.cpp coder. Before the
HXC-002-F3-01 config-default fix this route resolved to Ollama, which 404s any unknown model id and
forced an explicit `HELIX_LLM_PROVIDER=local`. That override is no longer needed — an independent
confirmation that F3-01 is live.

## 3. Real Claude Code, end to end

Real binary, **throwaway `CLAUDE_CONFIG_DIR`** (the operator's `~/.claude*` tree and this session's
own auth were never read or written), credential supplied via `apiKeyHelper` and never inlined:

```
=== invoking real Claude Code v2.1.263 (Claude Code) ===
exit: 0
  type: 'result'          subtype: 'success'      is_error: False
  stop_reason: 'end_turn' terminal_reason: 'completed'   num_turns: 1
  result: 'CLAUDE-E2E-OK'
```

Reproduced three consecutive times, identical (§11.4.50):

```
run 1:  result: 'CLAUDE-E2E-OK'   PASS
run 2:  result: 'CLAUDE-E2E-OK'   PASS
run 3:  result: 'CLAUDE-E2E-OK'   PASS
```

This is stronger than the pre-fix prediction. A diagnostic shim that flattened `system` had produced
`result: ""` with zero output tokens; the real fix returns actual assistant text.

`[claude-code:unrecognized_model] {"model":"helix-local"}` on stderr is expected and benign — Claude
Code does not know this id and assumes a default context window, which
`CLAUDE_CODE_MAX_CONTEXT_TOKENS=32768` corrects to the coder's real `n_ctx`.

## 4. No collateral damage

```
helix.target  helixagent  helixcode-server  helixllm-coder-native  helixllm-gateway  llmsverifier
   active       active         active              active                active          active
```

And the other wired agents still generate through the restarted stack:

```
$ crush run -q "Reply with exactly the word: STILL-OK"
STILL-OK
```

## 5. Configuration change made

`HELIX_WIRE_FACADE_API_KEYS` was absent, and the facade is **fail-closed** — with no key it 401s every
request, which is why it was inert. A generated key was appended to `.env` (mode 0600, gitignored per
CONST-053). The value was never printed to any log or transcript.

## 6. Honest boundary (§11.4.6) — what is NOT proven

- **The agentic loop does not work.** Claude Code drives tools via native `tool_use` blocks. The coder
  emits tool calls as a fenced JSON blob in `content` with `finish_reason:"stop"` — proven by probing
  `:18434` directly, below the facade. So single-turn text works; anything requiring a tool call does
  not. The gateway on `:8443` **does** emit structured `tool_calls` for the same model, so routing the
  facade there (gap-ledger **HXC-002-F3-03**) is the plausible path — untested here.
- **Vendor position unchanged.** Anthropic does not support routing Claude Code to non-Claude models
  through a gateway. This documents what the wire does, not that the arrangement is supported.
- **Security posture worth acting on.** `server.address` is `0.0.0.0` (`helix_code/config/config.yaml:5`)
  and the facade is plain HTTP, so the credential now crosses in clear on every interface. It was
  harmless while the facade was inert; it is not now. Recommend binding `127.0.0.1` or fronting with
  TLS before this host is on an untrusted network.
- **Only the happy path was exercised.** The new streaming-error guards are unit-proven but were not
  re-exercised live against the rebuilt binary.
