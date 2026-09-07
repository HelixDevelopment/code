# Operator decisions — 2026-09-03

| Field | Value |
|-------|-------|
| Revision | 2 |
| Created | 2026-09-03 |
| Last modified | 2026-09-03 |
| Status | current |

Twelve decisions taken by the operator in one sitting, in response to a request to
surface every blocked item as an interactive question. Recorded here because a
decision that lives only in a conversation is not a decision anyone can act on
later (§11.4.148 — items carry their decisions; §11.4.21 — a blocked item needs its
unblock choices enumerated).

Each row states what was asked, what was chosen, and — the part that matters for
whoever reads this next — **what the choice rejected**, so the reasoning survives.

---

## 1 · Release and merge posture

| | |
|---|---|
| **Question** | The Claude Toolkit endpoint fix sits on `fix/helixllm-export-review-findings`, pushed to all four providers, never merged to main. How should it land? |
| **Decision** | **Merge after the fix and tests are clean.** |
| **Rejected** | Keeping it on the branch indefinitely for personal review; merging the current state immediately and landing the endpoint fix separately. |
| **Consequence** | `main` only ever receives a verified state. The merge is gated on the branch's own regression test being green AND the suite not regressing past its 2279-pass / 12-fail baseline (those 12 being two known-unrelated causes). Force-push remains forbidden; a non-fast-forward merge must stop and report rather than force. |

## 2 · Dormant systemd units

| | |
|---|---|
| **Question** | `helixcode-infra`, `llmsverifier` (:8100) and `helixllm-coder` (podman, Qwen3-Coder-30B) are installed but not enabled. Which should be live? |
| **Decision** | **Leave all three dormant.** |
| **Rejected** | Enabling `llmsverifier` alone; swapping the native CPU 3B for the podman 30B; enabling all three. |
| **Why it is the right call on this host** | `helixcode-infra` would start a DUPLICATE postgres+redis pair alongside the `helixcode-autoboot-*` set the server already owns — the competing-orchestrator conflict that previously crash-looped postgres with `could not bind IPv4 "0.0.0.0": Address in use`. And `helixllm-coder` would load a 30B model through a CPU-only llama.cpp on a box whose swap is already fully consumed. |
| **Reversibility** | Nothing was removed. Each is one `systemctl --user enable` away, and the units stay tracked in `scripts/systemd/`. |

## 3 · Defect priority

| | |
|---|---|
| **Question** | Three real helix_llm defects are open — CRITICAL-4 (video service has no GGUF load path), CRITICAL-2 (streaming admits hosts that cannot run the model), OPEN-3 (agentgen-boot lane never migrated). Which first? |
| **Decision** | **All three in parallel.** |
| **Rejected** | Serialising by severity. |
| **Outcome so far** | OPEN-3 turned out **stale** — already fixed in `0c43b11` / `abbc0d2`; every specific claim in the finding was false and only the ledger was never updated. That discovery cost a full agent round and is itself the reason decision 12's sweep exists. CRITICAL-2 and CRITICAL-4 remain in flight on disjoint file scopes. |

## 4 · Tests that read the operator's real `$HOME`

| | |
|---|---|
| **Question** | `cli_schema_validation_test.go:166` reads the operator's actual `~/.config/opencode/opencode.json` and hard-fails when absent; `test_claude.sh:7` reads their live alias file. |
| **Decision** | **Sandbox both.** |
| **Rejected** | Skip-with-reason when the operator config is absent; leaving them as-is. |
| **The real defect this fixes** | `test_claude.sh` sources only `lib/assert.sh` and never the `lib/sandbox.sh` that exists for exactly this purpose. Its `xiaomi uses cma_run_provider` failure was traced to a subtler cause than "operator lacks the provider": a xiaomi key var DOES resolve, but the sync reports `provider 'xiaomi' FAILED verification — alias NOT activated`, so no alias is written — while `poe` also fails verification yet its stale alias persists from an earlier sync. The assertion was reading a MIXTURE of current and historical state, so it could fail on a transient upstream outage and pass on a stale alias. Sandboxing removes both failure modes. |

## 5 · GPU backend

| | |
|---|---|
| **Question** | The RTX 3060 (12 GB) is unusable: `/usr/bin/llama-server` is the Debian CPU-only build — `--list-devices` lists nothing, only `libggml-cpu-*.so` is present. The 3B therefore runs on CPU at 2-10 tok/s, RAM-bound with swap fully consumed. |
| **Decision** | **Install a CUDA-enabled llama.cpp.** |
| **Rejected** | Containerising it via the Containers submodule with nvidia-ctk CDI; accepting CPU-only. |
| **Constraints carried into the work** | Keep the Debian CPU binary as a fallback — do not remove an existing capability (§11.4.122). Do not escalate privileges unasked: if the only viable path needs root, stop and hand the operator the exact command. Prove the GPU is genuinely usable by captured `--list-devices` output AND observed layer offload, then report tok/s as a MEASURED before/after rather than a claim. |
| **Knock-on** | This may change decision 9's answer — models unmeasurable on CPU may become measurable once 12 GB is addressable. |

## 6 · Tracking the local-backend units

| | |
|---|---|
| **Question** | The live backend runs from `~/.config/systemd/user/helixllm-coder-native.service`, and a drop-in `10-local-model.conf` sets `HELIX_MODELS_AUTO_DOWNLOAD=false`. Neither is tracked, so neither reproduces from a fresh clone. |
| **Decision** | **Track both as templates plus installer.** |
| **Rejected** | Tracking the unit while folding the drop-in away; leaving both host-local. |
| **Shape** | Follow the project's existing convention — `scripts/systemd/*.service` templated with `@HELIX_ROOT@`, installed by `scripts/systemd/install_systemd_units.sh` — rather than inventing a new one. Fold the drop-in's settings into the tracked unit so there is ONE config mechanism; note the `.env` at the repo root already carries `HELIX_MODELS_AUTO_DOWNLOAD`, `HELIX_LLM_LOCAL_MODEL` and `HELIX_LLAMA_SERVER_EMBEDDED`, so check before duplicating them into a unit. |

## 7 · Exposed credentials

| | |
|---|---|
| **Question** | 13 credentials found exposed earlier in this programme remain unrotated. The standing decision was "just keep the record" — does it still hold? |
| **Decision** | **Keep the record only — unchanged.** |
| **Rejected** | Rotating them now; purging them from git history. |
| **Note on the third option** | Purging from history requires rewriting published history, which §11.4.113 forbids absolutely with no operator-approval path. Had it been chosen, the honest scope would have been rotation plus forward-only removal, and the rewrite itself declined. |
| **Standing obligation** | No rotation tooling is to be built. The documented list is kept current, and that is all. |

## 8 · JWT authentication in helix_llm

| | |
|---|---|
| **Question** | `docs/manual/security.md` documented JWT auth that does not exist. The docs are now honest, but `Auth.JWTSecret` is a field nothing reads. |
| **Decision** | **Implement JWT auth for real.** |
| **Rejected** | Leaving it unimplemented with honest docs; removing the dead field. |
| **The trap briefed into the work** | `/v1/models` and `/v1/chat/completions` are consumed RIGHT NOW by the toolkit's provider verification and by helix_code. Unconditional auth breaks both live consumers. Enforcement is therefore opt-in — active only when a secret is configured — and an unset secret must log loudly at startup rather than silently mean no protection. A placeholder or empty secret with auth enabled must be a loud refusal, matching the posture the server already takes on unexpanded `${...}` credentials. |
| **Bar for the tests** | Negative cases are mandatory: wrong signature, expired token, missing token, `alg=none`, wrong audience. A JWT implementation tested only on the happy path is a bluff. |

## 9 · The agent lane's three absent models (OPEN-24)

| | |
|---|---|
| **Question** | Mistral-Nemo-Instruct-2407, GLM-4.7-Flash and DeepSeek-Coder-V2-Lite are absent from the catalogue, so the lane REFUSES them — a real §11.4.122 narrowing for an operator holding a working GGUF. |
| **Decision** | **Measure them properly and add catalogue entries.** |
| **Rejected** | Accepting the narrowed set; populating the catalogue from vendor-published spec figures. |
| **Honest feasibility caveat, stated before the work began** | Mistral-Nemo is ~12B and DeepSeek-Coder-V2-Lite ~16B MoE — roughly 7 GB and 10 GB at Q4_K_M. With ~11 GB RAM free, swap fully consumed and a 3B already resident, the larger two may not load on this host until decision 5's CUDA work lands. The instruction is explicit: report "unmeasurable here" rather than estimate. Guessing a footprint is what SELF-7 punished, and the rejected third option is exactly that failure re-entering through the front door. |

## 10 · `/v1/models` listing semantics (OPEN-1)

| | |
|---|---|
| **Question** | FR-019 requires consumers to distinguish available from unavailable models, but `/v1/models` is an OpenAI-COMPATIBLE endpoint where clients expect a flat, all-usable list. Today HelixLLM adds `availability` / `withheld_reason` to it. |
| **Decision** | **Keep the extra fields on `/v1/models`.** |
| **Rejected** | Serving only available models there with a Helix-native endpoint for the full list; dropping availability entirely. |
| **Rationale** | Additive fields are a superset, not a spec violation — OpenAI clients ignore unknown keys — and the Claude Toolkit already reads servability from exactly there. The third option would have reintroduced the offer-what-cannot-start defect this whole feature exists to prevent. |

## 11 · The Go/Python selector divergence (OPEN-2)

| | |
|---|---|
| **Question** | `container/helix_model_gate.py`'s `gate.select()` ranks admissible entries cheapest-first; the Go `internal/selection` does not agree. Two implementations of one policy, free to drift. |
| **Decision** | **Make Go authoritative and bind Python to it by test.** |
| **Rejected** | A one-off alignment with no binding test; deleting one implementation. |
| **Precedent to follow, not reinvent** | `internal/serviceconfig/precision_agreement_test.go` already solves this shape — it binds Go's `servingPrecisions` to Python's `_UNIMPLEMENTED_PRECISIONS` and prints the comparison it made. The new test must follow that approach and must be proven falsifiable by a paired §1.1 mutation, not merely exist. |
| **Escape hatch deliberately left open** | If the evidence shows Go's ranking is itself the defect, the agent is to report that rather than propagate it. The operator chose Go as *owner*, not as *correct by fiat*. |

## 12 · The nested constitution copy

| | |
|---|---|
| **Question** | A duplicate of the just-fixed port mislabel survives at `submodules/claude-toolkit/constitution/scripts/helix_code/helix_code_services.sh:146`, and will keep reporting HelixAgent DOWN. |
| **Decision** | **Fix the copy now, flag the layout.** |
| **Rejected** | Replacing the copy with a reference to the real submodule; leaving it as known-stale. |
| **The layout problem being flagged rather than fixed** | The copy exists because claude-toolkit carries a COPY of the constitution instead of referencing it, which is what let one defect become two. CONST-051(C) forbids nested own-org submodule chains. Restructuring submodule wiring is a coordinated change and claude-toolkit is mid-merge, so it is recorded for a future round. |
| **Paired with** | A sweep of the 153-row findings ledger for rows closed in code but still open in the ledger — prompted by OPEN-3, where a stale row dispatched an agent to redo completed work. The sweep's rule is deliberately conservative: ambiguity means LEAVE IT, because a false "closed" hides real work and is worse than a stale "open". |

---

## 13 · The video GGUF load path (CRITICAL-4, second round)

| | |
|---|---|
| **Question** | The video service genuinely has no GGUF path — `videogen_server.py` builds its pipeline with one call shape, `from_pretrained` at lines 314/324, which resolves a diffusers repo via `model_index.json` and cannot read a single-file `.gguf` (asserted from the AST, not the file text). The bad catalogue data is fixed, so both `gguf-q4` entries are recorded-and-refused. Separately, video generation is impossible on this host regardless: `videogen-boot plan` withholds all three entries on MEMORY at exit 22 — 5.3 GiB usable after the responsiveness reserve against 8-10 GiB required — and the service docstring targets an RTX 5090. |
| **Decision** | **Implement the GGUF path.** |
| **Rejected** | Leaving it as-is with one servable option and honest refusals; re-recording `ltx-video-13b` against a diffusers repo; marking both entries Obsolete per §11.4.90. |
| **Recorded disagreement, and why it is proceeding anyway** | The agent that root-caused this DECLINED to implement it, and its reasoning was sound: it cannot be executed even once on this host, so it would ship unverified, and "implementing what cannot be executed once would have been a third bluff rather than a fix". The operator has chosen implementation with that fully stated. Per the standing rule that a reaffirmed decision is the operator's to make, it proceeds — but the constraint travels with it: **the §11.4.108 layer-3 runtime signature is NOT obtainable here.** Layer-1 (source) and layer-2 (the shipped loader reading the shipped file) are. Whatever lands must say plainly that it is unexecuted on this hardware, and must not carry a PASS that implies otherwise. |
| **Scope when it runs** | `from_single_file` + `GGUFQuantizationConfig`, with encoder/VAE/tokenizer/scheduler sourced separately and the pipeline assembled from components; then re-measure both memory figures. Recording a real GGUF `source:` is only permissible once measured — the data defect just closed was precisely a `source:` URL pointing at a repo containing zero `.gguf` files. |

## 14 · The 22 production-config keys (HXC-prodconfig)

| | |
|---|---|
| **Question** | `production-config.yaml` had an AI transcript pasted into it plus 210 lines YAML was silently discarding. The invalid-YAML damage is fixed and the parsed config was proven byte-identical before and after — but 22 keys remain whose intent nobody has confirmed. |
| **Decision** | **Audit the 22 keys against what the code actually reads.** |
| **Rejected** | Deleting the keys nothing reads; leaving them and moving on. |
| **Method** | Per key, determine from source whether anything consumes it, then either keep it with a comment saying what reads it, or mark it dead. There is precedent and a mechanism already: the server WARNs at startup about no-effect keys (`config key llm.providers has no effect in this build — no field on LLMConfig; the whole provider block is discarded`), so honest reporting of dead config is an established pattern here rather than a new one. |
| **Explicitly not chosen** | Deletion. §11.4.124 would require a git-history investigation per key first — why it exists, whether a reader was deleted by mistake — and each removal wants its own commit. The audit may recommend deletions afterwards; it may not perform them as a side effect. |

---

## Execution status of decisions 13 and 14

Both were recorded but **deliberately not dispatched** at the time of the decision, on host-safety grounds (§12.6, which yields unconditionally):

```
load average 96.33 on 16 CPUs   (~6x oversubscribed)
RAM           3 GB available of 30 GB
swap          8191/8191 MB — fully consumed
in-flight     12 subagents
```

Dispatching further work into that state risked OOM, would have contaminated the
timing-sensitive measurements several in-flight streams were taking, and could
have destabilised unrelated operator workloads on this shared host (§11.4.174).
The same discipline being applied to test measurements all session applies to
dispatch decisions.

They are queued, not dropped.

---

## What was NOT decided

Recorded so nobody mistakes silence for a decision:

- **`HXC-prodconfig`** — 22 config keys in `production-config.yaml` still await a call. The file's invalid-YAML and pasted-transcript damage was fixed; the key-by-key question was not asked.
- **`EX-22`** — the dead `helix-llm` / `helix-debate` provider block in helix_code's config, investigated under §11.4.124 but not resolved **as of 2026-09-03**. **Superseded by `HXC-002-F3-02`** (gap ledger `helix_code/docs/qa/2026-09-05-gap-ledger.md`), which tracks it as *critical* and prescribes the removal verbatim: drop the block from `config/config.yaml` and drop the dead keys `HELIX_LLM_ENDPOINT` / `HELIX_LLM_API_KEY` from `.env.example`. That removal is now in the working tree and is guarded by a purpose-built two-layer gate (`internal/config/dead_providers_gate_test.go`): the shipped config must contain neither string and must still `Load()` cleanly, plus a comment-stripped grep gate for non-comment Go references. Re-verified 2026-09-07: `internal/config.LLMConfig` declares no `providers` field, so the block never decoded — it was inert, not load-bearing, and nothing regressed by removing it.
  - **Still genuinely open, and wider than the block:** the same defect covers `llm.providers` *as a whole* and `llm.selection` — both are declared in the shipped YAML and decode into nothing. The open operator question is whether to honour those keys, remove them, or make the loader **reject** unknown keys the way the catalogue loader does (the option that would have prevented all four of `providers`, `selection`, `timeout`, `max_retries` going silently undecoded). The strict-loader option has consequences beyond this file, so it remains an operator call.
  - **Replica profiles are outside `HXC-002-F3-02`'s stated scope** and were left present-but-inert rather than removed (no silent removal beyond the tracked item, §11.4.122). `replica-8081.yaml` already carried a full dead-block annotation with `enabled: false`; `replica-8082.yaml` has been brought into line with it (annotated, `enabled: false`, misleading literal endpoints dropped — `:8081` is helixcode-server itself and `:8082` is ChromaDB). Both retain the block pending this same operator decision.
  - **Context worth having before deciding:** `helix-llm` and `helix-debate` are NOT fictional names — they are live *model ids* served by HelixAgent at `http://127.0.0.1:7061/v1`, confirmed GREEN by `scripts/gates/agent_config_fanout_gate.sh` on 2026-09-07. What was dead is the *provider-type* spelling in `llm.providers`, not the capability itself, which is reachable today through the CLI-agent fan-out. So the "wire it properly" option means adding a real factory `ProviderType` (per `HXC-002-F3-03`), not resurrecting the removed YAML.
- **`CRITICAL-4`'s "data decision"** — the finding's own status says "contained — data decision outstanding". Whether that decision is still live is part of the dispatched work; if it is, it comes back as a question rather than a guess.
- **A reboot test.** systemd units are `enabled` with `Linger=yes` and were proven to start from a fully stopped state, but no actual reboot was performed. Reboot survival is inferred from those three facts, not observed.
