# HelixAgent + HelixLLM models × Claude Toolkit — capability matrix

**Run id:** `2026-09-07-helix-models-toolkit`
**Captured:** 2026-09-07 09:41Z – 09:48Z (UTC), all probes live against the running services
**Scope:** every model id served by the three local Helix endpoints, and whether each is
(a) genuinely working and (b) recognised + usable through the Claude Toolkit.
**Evidence:** every cell below cites a file in `./evidence/`. Nothing here is inferred.

---

## 1. Verdict

**The operator's goal is NOT met. It is partially met, and the shortfall is larger than a
registration gap.**

| | count |
|---|---|
| Model ids served across the three endpoints | 7 |
| Genuinely generate a real answer | **4** |
| Return HTTP 200 but no generated text (canned string) | **3** |
| **READY** — working *and* reachable *and* usable through the toolkit | **1** |
| Registered toolkit aliases pointing at a model id that does not exist | **2** |

Only **`helixagent-llm`**, via the `helixagent` alias, is READY end to end.

The 7 ids are fewer than 7 distinct capabilities. Proven from source (§4, §7): `helix-llm` and
`helixagent-llm` are **one code path under two names**, and `helixagent-debate` /
`helixagent-ensemble` / `helix-debate` are **one code path under three names**. So the 7 ids
resolve to **3 distinct backends**: the llama.cpp coder model (reachable twice — direct and via
the gateway), the HelixAgent provider chain, and the debate/ensemble engine. Two of those three
work; the debate engine has no LLM connected to it at all.

Two problems dominate, and neither is fixed by registering more aliases:

1. **The HelixLLM gateway silently truncates every prompt to ~1016 tokens** while the toolkit
   advertises 229,376. The one HelixLLM model route that passes every verifier is therefore
   unusable for real work. §5.
2. **Three of the five HelixAgent model ids never generate text.** They return a hardcoded
   status string with `usage` all zeros. §4.

---

## 2. Capability matrix

`WORKS` = a real completion was captured. `BLOCKED` = measured failure, cause stated.
`UNTESTED` = not exercised (there are none — every cell was probed).

| # | Model id | Endpoint | Model works? (evidence) | Toolkit route | Toolkit status | Usable? |
|---|---|---|---|---|---|---|
| 1 | `qwen2.5-coder-3b-instruct-q4_k_m` | coder `:18434` | **WORKS** — `completion_coder_qwen25coder3b.json`, 200, content `HELIX-OK-7`, 7 completion tokens, 0.85 s | *no alias* | n/a | **BLOCKED — not registered.** See §6 |
| 2 | `helixllm-anton-qwen2-5-coder-3b-instruct-q4_k_m-f6771589d190` | gateway `:8443` | **WORKS for tiny prompts only** — `completion_gateway_helixllm_anton.json`, 200, `HELIX-OK-7` | alias `helixllm-anton-…` | `verified` (restored this run — `verify_anton.txt`) | **BLOCKED — silent truncation at ~1016 tokens.** §5 |
| 3 | `helixagent-llm` | helixagent `:7061` | **WORKS** — `completion_helixagent-llm.json`, 200, `HELIX-OK-7`, usage 18/8/26, 11.6 s; toolkit score **50**, `tool_call:true` | alias `helixagent` (strong + fast) | `verified` | **READY** |
| 4 | `helix-llm` | helixagent `:7061` | **WORKS** — `completion_helix-llm.json`, 200, `HELIX-OK-7`, usage 18/8/26, 11.3 s; toolkit score **55**, `tool_call:true` | *no alias* | n/a | **BLOCKED — not registered**, but it is the *same code path* as #3, so no capability is lost. §7 |
| 5 | `helixagent-debate` | helixagent `:7061` | **BLOCKED — no generation.** §4 | *no alias* | n/a | NOT READY |
| 6 | `helixagent-ensemble` | helixagent `:7061` | **BLOCKED — no generation.** §4 | *no alias* | n/a | NOT READY |
| 7 | `helix-debate` | helixagent `:7061` | **BLOCKED — no generation.** §4 | *no alias* | n/a | NOT READY |
| — | `helixllm-multi` *(not served by anything)* | pinned at `:8443` | **BLOCKED — HTTP 503**, `control_pair_helixllm-multi_vs_anton.txt` | aliases `helixllm-gateway`, `helixagent-native` | `unverified` / `existence` | **Phantom registration.** §8 |

Endpoint listings: `models_coder_18434.json`, `models_gateway_8443.json`, `models_helixagent_7061.json`.

---

## 3. Independent confirmation by the toolkit's own verifier

The toolkit ships an anti-bluff verifier (`model_verify.py`) whose probe demands an exact
sentinel token in the reply — a 200 without it is rejected as "a reply, not one from the
requested model". Run against `:7061` exactly as `cmd_sync_multi` invokes it
(`helixagent_verified.json`):

```
✓ helix-llm:           score=55  tool_call=true
✓ helixagent-llm:      score=50  tool_call=true
✗ helixagent-debate:   score=5   sentinel VERIFY_OK missing from response
✗ helixagent-ensemble: score=5   sentinel VERIFY_OK missing from response
✗ helix-debate:        score=5   sentinel VERIFY_OK missing from response
verified_count=2
```

This agrees with the manual probes on all five, independently. `min-score` is 25, so the three
rejected ids would not earn an alias even if the generator were pointed at them.

**`claude-verify-providers` (the LLMsVerifier path) could NOT be run** — it requires a Go build
(`claude-verify-providers.sh:65`) and no cached binary exists at
`claude_toolkit/.local-cache/code-verification`. Building was outside this run's constraints.
Reported as a gap, not worked around. The layer 1–3 checker `claude-providers verify` was used
instead (`verify_anton.txt`, `verify_others.txt`) and is cited throughout.

---

## 4. BLOCKED: three HelixAgent ids return a canned string, not a completion

`helixagent-debate`, `helixagent-ensemble` and `helix-debate` all return **HTTP 200** with:

```json
{"model":"helixagent-ensemble",
 "choices":[{"message":{"content":"Comprehensive debate completed with 3 rounds"},"finish_reason":""}],
 "usage":{"prompt_tokens":0,"completion_tokens":0,"total_tokens":0},
 "created":-62135596800,
 "agentic":{"agents_spawned":0,"tasks_completed":0,"total_duration_ms":0}}
```

Measured facts:

- The content is **byte-identical for completely different prompts** — proven by re-probing with
  "What is the capital city of France?" and getting the same sentence
  (`completion_helixagent-debate_substantive.json`, `completion_helixagent-ensemble_substantive.json`).
- All three collapse to `"model":"helixagent-ensemble"` regardless of which was requested.
- `usage` is all zeros, `agents_spawned: 0`, `total_duration_ms: 0`, `finish_reason: ""`,
  `created: -62135596800` (the Go zero time).

### Root cause — traced end to end (all paths in `submodules/helix_agent`)

**Why all three collapse to one.** `UnifiedHandler.ChatCompletions`
(`internal/handlers/openai_compatible.go:566-599`) puts `helixagent-debate`, `helixagent-ensemble`
and `helix-debate` in a **single `case` arm that only logs and falls through**, reaching
`processWithEnsemble` (`:695`) → `AgenticEnsemble.Process`. The response model field is then
hardcoded:

```go
internal/handlers/openai_compatible.go:3949
    Model: "helixagent-ensemble", // Always show ensemble model
```

**Why the content is a status string.** The chain is
`toolAugmentedDebate` (`internal/services/agentic_ensemble.go:170-190`) →
`ConductDebate` (`internal/services/debate_service.go:710`) →
`conductComprehensiveDebate` (`internal/services/debate_service_comprehensive.go:27-98`).
The orchestrator's reply *does* carry per-round agent text
(`Phases[].Responses[].Content`, `debate_orchestrator/orchestrator/types.go:80-134`), and
`conductComprehensiveDebate` **discards all of it** — it reads only `len(compResp.Phases)` as a
counter, sets `Participants: []ParticipantResponse{}`, and manufactures:

```
internal/services/debate_service_comprehensive.go:72-73   (and :163-164, streaming)
    FinalPosition:  fmt.Sprintf("Comprehensive debate completed with %d rounds", compResp.RoundsConducted),
    Summary:        fmt.Sprintf("Comprehensive debate completed with %d rounds", compResp.RoundsConducted),
```

`debateResultToEnsemble` (`agentic_ensemble.go:556-614`) finds `BestResponse == nil` and falls to
the `Consensus.FinalPosition` branch (`:575-582`), building an `LLMResponse` literal that sets no
`CreatedAt`, no `FinishReason` and no token fields — which is exactly why the wire shows
`created: -62135596800`, `finish_reason: ""` and `usage` all zeros. The adapter then emits it:

```go
internal/handlers/openai_compatible.go:3939
    Content: selected.Content,   // == Consensus.FinalPosition == the canned string
```

### The severity is worse than "an answer is thrown away"

Fixing the discard at `:72-73` would **not** produce a real answer. The comprehensive debate
orchestrator is constructed **with no provider registry at all**:

```go
submodules/debate_orchestrator/comprehensive/manager.go:47
    orch := orchestrator.NewOrchestrator(nil, nil, ocfg)   // registry == nil
```

so `invokeAgent` (`debate_orchestrator/orchestrator/orchestrator.go:453-458`) takes its
`o.invoker == nil` branch and returns `synthesiseContent` (`:499-506`) — a deterministic,
hash-derived placeholder that the package labels itself:

> `"[synthesised round=%d agent=%s digest=%s] Position on %q: deterministic-stub-content awaiting provider wiring."`

and the file's own doc comment (`orchestrator/types.go:1-6`) states it *"synthesises agent content
from a hash of (topic, agentID) rather than calling real LLM providers."*

**No LLM is wired into the debate/ensemble pipeline.** Surfacing the discarded content would only
replace one placeholder with another. Making these three ids work requires wiring a
`ProviderInvoker` into the orchestrator (the `WithProviderInvoker` option is never passed —
`debate_service.go:334-341`, `comprehensive/manager.go:41-53`) **and** fixing the discard.

`agents_spawned: 0` / `tasks_completed: 0` are likewise not evidence of a failed run: the
Reason-mode conversion path never populates them on any branch (`agentic_ensemble.go:606-611`);
only the Execute-mode loop does (`:294-297`), and debate ids never reach it.

This is a code defect in `submodules/helix_agent` plus an unwired dependency in
`submodules/debate_orchestrator`. **No toolkit registration can make these three ids work.**

> A naive smoke test asserting `http==200 && content != ""` marks all three GREEN. The toolkit's
> sentinel gate catches them; a hand-rolled check would not.

---

## 5. BLOCKED (most serious): the HelixLLM gateway silently truncates prompts to ~1016 tokens

This is the finding that matters most, and it makes the one "verified" HelixLLM route unusable.

### Measured — graded prompt-size sweep through the gateway (`graded_prompt_size_sweep.txt`)

| input words | request bytes | gateway `usage.prompt_tokens` | HTTP |
|---|---|---|---|
| 200 | 1,447 | 369 | 200 |
| 2,000 | 12,787 | **1016** | 200 |
| 8,000 | 50,587 | **1016** | 200 |
| 20,000 | 126,187 | **1016** | 200 |
| 40,000 | 252,187 | **1016** | 200 |

A **20× increase in input produces byte-identical token accounting** — and a confident
`HELIX-OK-7` every time. No error, no warning, no truncation notice.

### Attribution — it is the gateway, not the backend (`truncation_attribution_control.txt`)

Same 2,000-word prompt, two paths:

| path | `prompt_tokens` | result |
|---|---|---|
| direct to llama.cpp `:18434` | **3040** | 200, answers |
| through gateway `:8443` | **1016** | 200, answers |

The backend accepts 3,040 tokens without complaint (well under its 32,768 ceiling), so the loss
is introduced by the gateway.

### The honest path, for contrast (`oversize_context_coder_direct.json`)

The same oversized prompt sent **direct** to llama.cpp:

```json
{"error":{"code":400,"message":"request (60040 tokens) exceeds the available context size (32768 tokens), try increasing it",
          "type":"exceed_context_size_error","n_prompt_tokens":60040,"n_ctx":32768}}
```

**This reframes the context-size blocker in the brief.** The `141440 tokens exceeds 4096` error
was the *honest* behaviour. Raising `--ctx-size` and putting the gateway in front did not fix it —
it converted a loud, correct refusal into **silent context loss**. For a coding agent that is
strictly worse: it would answer about code it never received.

### Why this defeats the "verified" status

The alias advertises `CMA_PROVIDER_CONTEXT_LIMIT='229376'`
(`~/.local/share/claude-multi-account/providers/helixllm-anton-….env`), which Claude Code exports
as its auto-compact window. The route actually delivers ~1,016 tokens — a **226× overstatement**.
Every verifier passes because every verifier probe is tiny. This is exactly the trap the
`helixllm-coder-native.service` unit warns about: *registration proves plumbing, not usability.*

### Model ceiling, for the record (`models_coder_18434.json`)

`n_ctx: 32768`, `n_ctx_train: 32768`. 32,768 is the model's trained ceiling — there is no headroom
above it. A client needing 141,440 tokens cannot use this 3B model at all, however it is
configured. That part is **BLOCKED BY MODEL CAPACITY** and is not a bug.

---

## 6. `qwen2.5-coder-3b-instruct-q4_k_m` (:18434) — deliberately not registered

The brief expected this endpoint to be registered. It was **not**, and forcing it would have been
wrong. Established:

- `helixllm-export` **refuses it by design**. Its selector requires a non-empty `model_identity`
  field (`claude-providers.sh:752`), which only the HelixLLM gateway emits. A dry run across all
  three hosts (`helixllm-export_dryrun_3hosts.txt`) reports the coder and helixagent hosts as
  contributing nothing — correct behaviour, not a failure.
- `:18434` is the **upstream backend the gateway fronts**, not a peer. Proof: a completion sent to
  the gateway's `helixllm-anton-…` id comes back with `"model":"qwen2.5-coder-3b-instruct-q4_k_m"` —
  the same model.
- Hand-writing a provider `.env` for it would be exactly the drift the toolkit's generators exist
  to prevent, and the next `sync` would demote it (§8).

**However** — given §5, a direct `:18434` route is now genuinely worth having, because it is the
only path to this model that does not silently truncate and that reports honest errors. The
sanctioned way to add it is `claude-providers add --from-key <VAR> --id <id>` plus an entry in
`scripts/providers/overrides.json`. **Not done in this run** — it adds a route whose behaviour
differs from the existing one, and that is an operator decision.

---

## 7. `helix-llm` — works, scores highest, unreachable

`helix-llm` scores **55** (the highest of the five) with `tool_call: true`, and returns real text.
It has no alias, because the single `helixagent` alias carries only two model slots and
`scripts/providers/helixagent.json` pins **both** to `helixagent-llm`:

```json
{ "strong_model": "helixagent-llm", "fast_model": "helixagent-llm" }
```

The sanctioned fix is that pins file (the detector validates the pinned id against the live
`/v1/models` listing before using it, `claude-providers.sh:362-388`).

**Not changed in this run, and it should NOT be changed — now established as fact.** The two ids
are the *same code path*, not two backends. They sit in one `case` arm and make one identical
call, with no branching on which literal was sent:

```go
internal/handlers/openai_compatible.go:573-576
case "helixagent-llm", "helixagent/helixagent-llm",
     "helix-llm",      "helixagent/helix-llm":
    h.processWithProviderChain(c, &req)
    return
```

`req.Model` is never read again inside `processWithProviderChain` to distinguish them. That is
precisely why the two probes returned numerically identical results — usage `{18, 8, 26}`,
`system_fingerprint: "fp_helixllm_v1"`, times within 0.3 s.

**So `helix-llm` being "unreachable" costs nothing**: the `helixagent` alias already reaches that
exact code path via `helixagent-llm`. The differing verifier scores (55 vs 50) are latency-band
noise between two probes of one backend, not a capability difference. Splitting the alias slots
would add a second name for the same thing. The row stays BLOCKED in the matrix for accuracy, but
it is the one blocker with **no user-visible impact**.

`claude-providers sync --multi` **cannot** solve this: it enumerates models from the models.dev
catalog, which has no `helixagent` entry — the run fails with
`no models specified and no catalog available` (reproduced this run). Self-hosted providers whose
models are not in models.dev get no per-model aliases. That is a toolkit gap worth its own item.

---

## 8. Two phantom registrations, and a sync/export conflict

### 8a. `helixllm-multi` is not a real model

`helixllm-gateway` and `helixagent-native` are both pinned to `strong_model: "helixllm-multi"`.
That id returns **HTTP 503** — while, in the *same second*, the working id returns 200
(`control_pair_helixllm-multi_vs_anton.txt`). The backend is up; the id simply is not served.

`grep -rn 'helixllm-multi'` over `submodules/helix_llm` returns **zero matches**. It exists only
in the toolkit, as a hardcoded default (`claude-providers.sh:479-480, 513-514`) and in
`providers/helixllm-gateway.json` / `providers/helixagent-native.json`. It is an invented id.

The toolkit's own verifier diagnoses it precisely (`verify_others.txt`):

> `REACHABLE BUT UNABLE: … answered HTTP 503, so the endpoint is correct and the service is up …
> Fix the backend (upstream/model availability), not the base_url.`

Worth flagging: `scripts/tests/proof/93-helixagent-pins-survive-live-sync.txt` **records both as
`(unverified/existence)` as expected output** — the broken state is enshrined in the toolkit's own
proof corpus.

### 8b. `helixllm-export --apply` and `sync` undo each other

`cmd_sync` calls `cma_demote_orphans "$resolved_ids"` (`claude-providers.sh:2082`). `resolved_ids`
comes from key-var resolution plus the helixagent/native detectors. Records written by
`helixllm-export --apply` are in **neither** set — they derive from the live gateway listing. So
`cma_find_orphans` reports them, and every `sync` demotes them to `orphaned`, which the launch
gate refuses (only `verified` passes).

Observed exactly that: the anton record was written 2026-09-04 08:52; `status.json` showed
`"status":"orphaned","failing_layer":"orphan","checked_at":"2026-09-07T08:50:56Z"` — today's sync
demoted the only working HelixLLM route.

`claude-providers verify helixllm-anton-…` restored it to `verified` this run (`verify_anton.txt`,
`claude-providers_list-all_after.txt`) — but **the next `sync` will demote it again**. This is a
workaround, not a fix. The fix is to have `cmd_sync` treat records carrying
`CMA_PROVIDER_SOURCE='helixllm-export'` as resolved. That is a change to `claude_toolkit`'s core
script and was **not** made here.

---

## 9. What changed on disk during this run

Backup taken first: `~/.claude-toolkit-backups/helix-models-20260907T094116Z/`
(contains `providers/`, `aliases.sh`, `claude-code-router/`).

| change | how | reversible |
|---|---|---|
| `status.json`: `helixllm-anton-…` `orphaned` → `verified` | `claude-providers verify <id>` (sanctioned) | yes — restore backup, or it self-reverts on next sync |
| `status.json`: `checked_at` refreshed for `helixagent`, `helixllm-gateway`, `helixagent-native` | `claude-providers verify` | yes |
| `providers/helixllm-models.json` re-merged (content unchanged — same single entry) | `helixllm-export --dry-run` writes the catalogue before honouring the flag | yes |
| `providers/helixagent_verified.json` (new, in `./evidence/`) | `model_verify.py` output | n/a |

**No configuration file was hand-edited.** No `.env`, no ccr `config.json`, no pins file, no
`overrides.json`. Every state change went through a toolkit command.

---

## 10. What remains, in priority order

1. **Gateway prompt truncation (§5)** — release-blocking. The gateway drops everything beyond
   ~1016 tokens and returns 200. Until this is fixed, no HelixLLM gateway route is usable by
   Claude Code regardless of how it is registered.
2. **`CMA_PROVIDER_CONTEXT_LIMIT` overstates capacity 226×** — the pins claim 229,376 for a model
   whose real ceiling is 32,768 and whose live route delivers ~1,016.
3. **No LLM is wired into the debate/ensemble pipeline (§4)** — two changes are needed, not one:
   pass a `ProviderInvoker` into the orchestrator (`comprehensive/manager.go:47`), *and* stop
   discarding `compResp.Phases` (`debate_service_comprehensive.go:66, 72-73, 163-164`). Fixing
   only the second swaps one placeholder for another.
4. **Two phantom aliases (§8a)** — repoint or retire `helixllm-gateway` / `helixagent-native`; and
   correct the proof file that enshrines the broken state as expected output.
5. **sync/export conflict (§8b)** — `cmd_sync` must not orphan `helixllm-export`-sourced records.
6. **No per-model aliases for self-hosted providers (§7)** — `sync --multi` depends on models.dev,
   which has no entry for local providers.
7. **`claude-verify-providers` unrunnable without a Go build (§3)** — no cached binary.
8. *(closed, no action)* `helix-llm` unreachable — proven to be the same code path as
   `helixagent-llm`, which is already reachable. No capability is missing.

---

## 11. Evidence index

| file | what it proves |
|---|---|
| `models_coder_18434.json` | coder listing; `n_ctx 32768`, `n_ctx_train 32768` |
| `models_gateway_8443.json` | gateway serves exactly one model id |
| `models_helixagent_7061.json` | helixagent serves exactly five model ids |
| `completion_coder_qwen25coder3b.json` | coder generates real text |
| `completion_gateway_helixllm_anton.json` | gateway route generates real text (small prompt) |
| `completion_helixagent-llm.json`, `completion_helix-llm.json` | the two working HelixAgent ids |
| `completion_helixagent-debate.json`, `…-ensemble.json`, `completion_helix-debate.json` | the canned string, three ids |
| `completion_helixagent-debate_substantive.json`, `…-ensemble_substantive.json` | identical output for a different prompt |
| `helixagent_verified.json` | toolkit sentinel verifier: 2 pass, 3 rejected |
| `completion_helixllm-multi.json`, `control_pair_helixllm-multi_vs_anton.txt` | 503 vs 200 in the same second |
| `graded_prompt_size_sweep.txt` | truncation pinned at 1016 tokens across a 20× input range |
| `truncation_attribution_control.txt` | 3040 direct vs 1016 through gateway — same prompt |
| `oversize_context_coder_direct.json` | honest 400 from llama.cpp: 60040 > 32768 |
| `oversize_context_gateway.json` | 200 + answer for the same oversized prompt |
| `helixllm-export_dryrun_3hosts.txt` | export contributes nothing from `:18434` / `:7061` |
| `verify_anton.txt`, `verify_others.txt` | toolkit layer 1–3 verdicts |
| `claude-providers_list-all.txt`, `…_after.txt` | alias status before and after |
