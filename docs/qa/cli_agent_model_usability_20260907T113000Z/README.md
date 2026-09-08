# QA evidence — CLI agent x Helix surface usability

| Field | Value |
|-------|-------|
| Run id | `cli_agent_model_usability_20260907T113000Z` |
| Date | 2026-09-07 |
| Host | anton |
| Question | Can the exposed HelixLLM models actually be used from the CLI coding agents? |
| Verdict | Partly. One combination works fully (`crush` → HelixAgent). One works with a documented trim (`pi --no-skills` → Coder). The rest are blocked, each for a measured reason. |

Anti-bluff note (§11.4, §11.4.6): every ✅ below is an agent process actually run
against a real endpoint with the **content** of its answer checked. Every ❌
carries the verbatim rejection or the observed wrong answer. Nothing is inferred.

---

## 1. Method

Endpoint-reported token counts, not estimates. Each agent was pointed at a local
capture server that records the exact request body and returns a valid minimal
completion; the recorded body was then replayed against the real endpoint so
the prompt-token figure comes from llama.cpp / HelixAgent itself
(`usage.prompt_tokens` on success, `n_prompt_tokens` on a 400).

Operator configs were never modified: `pi` ran under a copied
`PI_CODING_AGENT_DIR`, `crush` under a sandbox `XDG_CONFIG_HOME`, `opencode`
under a sandbox `OPENCODE_CONFIG`. The two headline `crush` results in §3 are the
exception — they were deliberately run against the **real, unmodified** operator
config, because that is the claim that matters.

Measured request shapes: [`measured_request_shapes.json`](measured_request_shapes.json).

---

## 2. Measured baseline prompt sizes (before any user turn)

| Agent | Prompt tokens | `<skill>` blocks | Tools | Content form | Fits 32,768? |
|---|---|---|---|---|---|
| `pi` default | 86,411 | 950 | 4 | array | ❌ |
| `pi --no-skills` | 1,480 | 0 | 4 | array | ✅ |
| `crush` default | 96,564 | 949 | 26 | string | ❌ |
| `crush` no host skills | 11,342 | 4 | 26 | string | ✅ |
| `opencode` default | 134,043 | 979 | 120 | array | ❌ |
| `opencode --pure` | 132,058 | 965 | 120 | string | ❌ |

Root cause of the large figures: ~950 skills installed under `~/.claude/skills`
(955), `~/.config/crush/skills` (954) and `~/.pi/agent/skills` (954), inlined
into each agent's system prompt.

Falsification check that these numbers are real and not a config artefact: with
a fresh `HOME`, crush's request drops 401,406 → 48,104 bytes (11,342 tokens),
and `crush.json` was independently proven to be read at all by disabling the
`sourcegraph` tool and observing `n_tools` fall 26 → 25.

---

## 3. Working combinations — captured runs

### 3.1 `crush` → HelixAgent, real operator config, full 950-skill prompt ✅

```console
$ crush run -q --cwd <neutral> --data-dir <sandbox> -m helixagent/helixagent-llm \
    "Reply with exactly the word: BANANA7"
BANANA7
rc=0
```

Real tool use through the same path (the model had to call `view` to answer):

```console
$ echo "MARKER_FILE_CONTENT_ZQ7" > probe_marker.txt
$ crush run -q --cwd <neutral> --data-dir <sandbox> -m helixagent/helixagent-llm \
    "Read the file probe_marker.txt in the current directory and reply with exactly its contents, nothing else."
I'll read the probe marker file.MARKER_FILE_CONTENT_ZQ7
rc=0
```

### 3.2 `pi --no-skills` → Coder ✅

```console
$ pi -p --no-skills --provider helixllm-coder --model qwen2.5-coder-3b-instruct-q4_k_m \
    "Reply with exactly the word: HELIXOK"
HELIXOK
rc=0
```

Same body replayed directly: `prompt_tokens=1480`, answer `'HELIXOK'`.

---

## 4. Blocked combinations — captured failures

### 4.1 Context ceiling (Coder and Gateway, 32,768 hard limit)

```console
$ crush run -m helixllm-coder/qwen2.5-coder-3b-instruct-q4_k_m "Reply with exactly the word: BANANA7"
ERROR
Agent processing failed: failed to start agent processing stream: bad request:
request (96565 tokens) exceeds the available context size (32768 tokens), try increasing it.
```

Replay of `pi`'s default body: `request (86411 tokens) exceeds the available context size (32768 tokens)`.
Replay of `opencode`'s default body: `request (134043 tokens) exceeds ... (32768 tokens)`.

32,768 is this model's `n_ctx_train` (confirmed from `/v1/models`:
`"n_ctx":32768,"n_ctx_train":32768`) — it cannot be raised.

### 4.2 HelixAgent rejects array-form content (blocks `pi`, and `opencode` without `--pure`)

```console
$ curl -sS -X POST http://127.0.0.1:7061/v1/chat/completions -H 'Content-Type: application/json' \
    -d '{"model":"helixagent-llm","messages":[{"role":"user","content":"say OK"}],"max_tokens":8}'
{"…","message":{"role":"assistant","content":"OK! 😊 Let me know"}…}

$ curl -sS -X POST http://127.0.0.1:7061/v1/chat/completions -H 'Content-Type: application/json' \
    -d '{"model":"helixagent-llm","messages":[{"role":"user","content":[{"type":"text","text":"say OK"}]}],"max_tokens":8}'
{"error":{"code":400,"message":"Invalid request format: json: cannot unmarshal array into Go struct field OpenAIMessage.messages.content of type string","type":"invalid_request"}}
```

Reproduced through the agent itself:

```console
$ pi -p --no-skills --provider helixagent --model helixagent-llm "Reply with exactly the word: BANANA7"
400: {"code":400,"message":"Invalid request format: json: cannot unmarshal array into Go struct field OpenAIMessage.messages.content of type string","type":"invalid_request"}
```

Controls: the Coder and the Gateway both accept the array form (the Gateway then
discards it — §4.3).

### 4.3 CRITICAL — the Gateway silently discards array-form content

Identical request body, only the endpoint changed. This body is the verbatim one
`pi --no-skills` sent:

```
CODER   :18434  prompt_tokens=1480  ->  'HELIXOK'                                   ← correct
GATEWAY :8443   prompt_tokens=577   ->  'Yes, I can help with that. What would…'    ← instruction gone
```

903 prompt tokens — the entire user instruction — were dropped, and the response
was `200 OK`. Minimal reproduction:

```console
$ curl -sS --cacert <cert> -X POST https://127.0.0.1:8443/v1/chat/completions \
   -H 'Content-Type: application/json' \
   -d '{"model":"helixllm-anton-…-f6771589d190","messages":[{"role":"user","content":[{"type":"text","text":"Reply with exactly the word: BANANA7"}]}],"max_tokens":16}'
content: "Hello! I'm here to help you with any code-related questions or tasks you"
usage: {'prompt_tokens': 58, ...}

# same request, content as a plain string:
content: 'BANANA7'
usage: {'prompt_tokens': 68, ...}
```

This is a false-null (§11.4.201): a smoke test that only checks for a non-error
reply **passes** against a request whose content never reached the model.

---

## 5. HelixAgent capacity — measured, not assumed

`/v1/models` on `:7061` publishes no context figure, so it was measured directly.
A 900,312-byte request with a marker planted at the very front:

```
payload bytes: 900312
elapsed 25.1s
usage: {'prompt_tokens': 200046, 'completion_tokens': 18, 'total_tokens': 200064}
answer: 'The passphrase is **SECRET_FRONT_MARKER_QX42**.'
```

Accepted at 200,046 prompt tokens, and the *front* of the prompt survived — so
the surface neither refused nor silently truncated. This is what justifies the
installer declaring 131072 (a deliberately conservative claim) rather than the
32768 it previously shared with the llama.cpp surfaces.

## 6. HelixAgent tool-calling — measured

Previously documented as "not probed". All three surfaces, same tool definition:

```
CODER    :18434  finish_reason=stop        tool_calls=null   content='```json\n{"name": "get_weather", …}```'
GATEWAY  :8443   finish_reason=tool_calls  tool_calls=[{"id":"call_get_weather",…}]
HELIXAGENT:7061  finish_reason=tool_calls  tool_calls=[{"id":"DSnPDcL5U",…}]
```

HelixAgent emits structured `tool_calls`, like the Gateway and unlike the raw
Coder.

---

## 7. Gate and test status after the change

```
CM-AGENT-CONFIG-FANOUT: PASS — green=7 skipped=0 findings=0
TEST-AGENT-CONFIG-FANOUT: run=9 pass=8 fail=0 skip=1 — PASS
```

Neither gate nor test asserts context values, so the installer change is not
covered by an existing assertion; it is covered by §5 above and by the sandbox
install verification (`contextWindow: 131072` for the five HelixAgent models,
`32768` unchanged for the two llama.cpp surfaces).

---

## 8. Not established

- `opencode --pure` → HelixAgent: attempted twice, **no verdict either way**.
  Both runs were killed by their own timeout (300 s and 500 s, `SIGTERM`,
  exit 143) having produced no output, and the request-capture proxy recorded
  no request at all — so `opencode` stalled before sending. HelixAgent was
  answering a small request in 2.2 s at the same moment, so this is client-side,
  not a server refusal. **UNTESTED** — it is the one combination that could
  work (string content + sufficient capacity) and it is not claimed here.
- `crush` → Gateway over TLS end to end: crush's trust of the self-signed
  certificate did not complete within 110 s. `curl --cacert` against the same
  endpoint answers in 0.79 s, so this is crush-side TLS, not server latency.
  No verdict either way.
- HelixCode `:8080` and Claude Code: out of scope for this pass.
- HelixAgent's actual context ceiling above 200,046 tokens: not probed.
