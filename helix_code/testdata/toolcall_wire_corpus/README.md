# Tool-calling wire corpus — real captured evidence, not fixtures

| | |
|---|---|
| Revision | 1 |
| Created | 2026-09-07 |
| Last modified | 2026-09-07 |
| Status | active |

Every `.json` file here is a **verbatim HTTP response body recorded from a live
backend** — the HelixLLM gateway (`https://127.0.0.1:8443/v1`) and the local
llama.cpp coder (`http://localhost:18434`). None of it was hand-written.

## Table of contents

- [Why this corpus exists](#why-this-corpus-exists)
- [What is recorded](#what-is-recorded)
- [Integrity](#integrity)
- [Regenerating](#regenerating)
- [Who consumes it](#who-consumes-it)

## Why this exists

Three guards asserted on a **live** model's tool-calling output. That output is
not deterministic. Measured 2026-09-07 — twelve byte-identical POSTs to the
gateway's `/v1/chat/completions`, temperature 0, `max_tokens` 64, one fixed
prompt and one fixed nonce:

```
8/12  finish_reason=tool_calls   tool_calls PRESENT
4/12  finish_reason=length       tool_calls ABSENT
```

Two distinct outcomes for an identical request at temperature 0. Reproduced by
running the affected guard five times back to back: PASS, FAIL, PASS, PASS,
PASS. Retries and bigger budgets cannot fix it — the non-determinism is in the
system under test.

Replaying these recorded bytes through the **same production parsing and wire-
facade code** keeps the anti-bluff property (the bytes are real captured
evidence) while making the verdict a function of our code alone.

## What is recorded

| File | What it is |
|---|---|
| `gateway_models.json` | gateway `GET /v1/models` |
| `gateway_weather_tool_calls.json` | gateway response **with** structured `tool_calls` (the 8/12 branch) |
| `gateway_weather_no_tool_calls.json` | gateway response **without** them, `finish_reason=length` (the 4/12 branch) |
| `coder_models.json` | coder `GET /v1/models` |
| `coder_weather_fenced_stop.json` | coder response — the tool call arrives as a fenced ```json blob with `finish_reason=stop` |
| `gateway_add_00.json` … `gateway_add_15.json` | sixteen distinct `add(a,b)` tool-call responses, one per concurrent slot of the D6 guard |
| `provenance.json` | capture timestamp, endpoints, models, frozen nonce, the non-determinism measurement, and a sha256 per body |

Both gateway branches are kept deliberately. A corpus that recorded only the
convenient outcome would quietly assert that the backend is deterministic.

## Integrity

`internal/testutil/wirecorpus.Load` verifies every body's sha256 against
`provenance.json` on each load and **fails the test** on any mismatch. Editing a
recording by hand is exactly the failure mode this design exists to prevent, so
it is refused loudly rather than accepted silently. Proven by executed mutation:
altering one recorded city string produced

```
wirecorpus: recorded body "gateway_weather_tool_calls.json" has been MODIFIED since capture
```

## Regenerating

```bash
python3 helix_code/testdata/toolcall_wire_corpus/capture.py            # re-record from live services
python3 helix_code/testdata/toolcall_wire_corpus/capture.py --verify   # re-hash only
```

The recorder lives inside the corpus it regenerates, so the two can never drift
apart or be relocated independently.

The recorder verifies the gateway's TLS certificate against the in-repo CA
(`submodules/helix_llm/certs/cert.pem`) — the same trust anchor the production
route uses. It never skips verification, and it refuses to write a corpus in
which the gateway produced no tool call at all.

## Who consumes it

- `internal/server/llm_generate_toolcall_replay_test.go`
- `internal/server/wire_facade_toolcall_replay_test.go`
- `internal/llm/tool_calling_concurrent_replay_test.go`

The complementary question a recording cannot answer — *is the live gateway
still capable of this today?* — is asked by the opt-in probes behind
`HELIX_LIVE_TOOLCALL_PROBE=1`.
