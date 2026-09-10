# HelixLLM gateway — model-pin enforcement finding

Run: 2026-09-08, agent-owned resource `https://127.0.0.1:8443` (§11.4.119).
helix_llm HEAD: `144e051`. Investigation only — no file modified, gateway not restarted.

## VERDICT: (c) — a REAL facade that FAILS OPEN on unrecognised model ids

Not (a): the echo is not merely cosmetic — the requested name is genuinely
discarded at routing time for any name that is not a known identifier.
Not (b): the field is not "ignored entirely" — it IS validated for shape, and
the advertised id IS genuinely translated to the backend name.

The truth is worse than (a) and narrower than (b): the pin is honoured when it
happens to be right, and silently ignored when it is wrong. Observationally, on
this single-backend host, that is indistinguishable from (b).

## Decisive evidence — probes (see 10..18, 17_negative_controls.log)

| request `model` | HTTP | response `model` |
|---|---|---|
| `helixllm-anton-qwen2-5-coder-3b-instruct-q4_k_m-f6771589d190` (advertised) | 200 | `qwen2.5-coder-3b-instruct-q4_k_m` |
| `definitely-not-a-real-model-zzz` | **200** | `qwen2.5-coder-3b-instruct-q4_k_m` |
| `""` (empty) | 200 | `qwen2.5-coder-3b-instruct-q4_k_m` |
| `gpt-4o` | **200** | `qwen2.5-coder-3b-instruct-q4_k_m` |
| `claude-opus-4` | **200** | `qwen2.5-coder-3b-instruct-q4_k_m` |
| `llama3.3:70b` | **200** | `qwen2.5-coder-3b-instruct-q4_k_m` |
| `../../etc/passwd` | 400 | — (`model name must not contain a ".." path segment`) |

Control needles (§11.4.273) — the endpoint CAN refuse, so the 200s are real:
* bad bearer token -> **401** `invalid credentials`
* malformed JSON -> **400** `invalid request body: malformed JSON`
* missing `messages` -> **400** `messages must contain at least one message`
* traversal model -> **400** (proves model-string validation runs at all)

## Source citations

1. `internal/gateway/requestvalidate.go:76-80` — model existence is
   deliberately NOT validated, in an explicit "What is deliberately NOT
   validated" block:
   > "Model EXISTENCE. A well-formed but unrecognised model still reaches
   > dispatch. ... This file rejects names that are not model NAMES, never
   > names that are merely unknown."
   The intended home for the 404 is named as `/v1/models/:id`.

2. `internal/brain/naming.go:196` `ResolveModelName` — the real facade mapping,
   advertised id -> provider model name. Its own doc comment names this exact
   failure mode (naming.go:190-193):
   > "without this a client that listed an identifier and then asked for it
   > would miss every exact match and fall through to whichever provider the
   > router reached last — **a silent misroute**."
   Unknown names are returned unchanged, `ok=false`, and continue.

3. `internal/brain/router.go:79-123` `Router.Route` — the fail-open. After the
   exact-match (step 2) and prefix (step 3) arms miss, **step 4 falls back to
   the fallback provider and step 5 to ANY available provider**; only step 6
   errors, and only when nothing at all is available. So an unresolvable name
   is served rather than refused — precisely the "silent misroute" the
   naming.go comment warns about, left unguarded for non-identifier names.

4. `internal/gateway/openai.go:915-917` `internalToOpenAI` —
   `if resp.Model != "" { model = resp.Model }` overwrites the client's
   requested name with the backend's, which is why the response echoes
   `qwen2.5-coder-3b-instruct-q4_k_m`. That echo is the only reason this is
   visible at all; without it the misroute would be silent.

## Why this matters (§11.4)

An alias whose model pin is not enforced server-side is a pin in name only. Any
caller — a mis-typed alias, a stale config, a different toolkit provider record
pointed at this base_url — gets a confident HTTP 200 from a model it did not
ask for. `~/Projects/claude_toolkit/scripts/tests/test_helixllm_facade_model_reality.sh`
already guards the CLIENT half (the alias must not name a model the host does
not serve); nothing guards the SERVER half (the host must refuse a model it
does not serve). This finding is the complement of that test, not a duplicate.
