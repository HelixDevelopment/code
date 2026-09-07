# Evidence — coder context 4096→32768 + llmsverifier enabled

| Field | Value |
|-------|-------|
| Revision | 1 |
| Created | 2026-09-05 |
| Last modified | 2026-09-05T20:56Z |
| Status | active |
| Status summary | Both operator-requested changes applied to TRACKED sources and proven live: the llama.cpp coder now runs at n_ctx 32768 (verified by a 12,058-token request that the previous 4096 ceiling made impossible), and llmsverifier.service is enabled with its own boot-graph entry. Full stack healthy after the restart; gateway end-to-end re-proven. |

Operator instruction (2026-09-05): *"raise ctx-size to 32768 and enable llmsverifier"*.

## 1. Why the context raise was necessary — not a tuning preference

At `--ctx-size 4096` the endpoint was **registrable but unusable** by CLI agents. Every
mainstream agent sends a system prompt of tool definitions and instructions far
exceeding 4096 tokens, so provider registration succeeded while every real request
failed at the first turn. Measured before the change, a real `pi` request was rejected
by this server:

```
request (141440 tokens) exceeds the available context size (4096 tokens)
```

Only a deliberately minimal prompt (`crush run -q`) fit — which is exactly why a smoke
test could pass against an endpoint that was in practice unusable. Registration proves
plumbing, not usability (§11.4).

## 2. Hardware-safety arithmetic done BEFORE the change (§11.4.133)

Measured at the time of the change, not assumed:

```
NVIDIA GeForce RTX 3060, 12288 MiB total, 2276 MiB used, 9634 MiB free
Host RAM: 30 GiB total, 18 GiB available
```

Qwen2.5-Coder-3B f16 KV cache ≈ 36 KB/token
(36 layers × 2 KV heads × 128 head_dim × 2 tensors × 2 bytes).
At 32768 tokens that is **≈1.2 GB** on top of the ~2 GB of Q4_K_M weights — several GB
of headroom remain. Raising further, or moving to a larger model, must re-do this
arithmetic against free VRAM at that time.

## 3. Changes — made in TRACKED sources so a fresh `setup.sh` reproduces them

| File | Change |
|---|---|
| `scripts/systemd/helixllm-coder-native.service` | `--ctx-size 4096` → `--ctx-size 32768`; stale comment ("stays at 4096") rewritten with the measured rationale and VRAM arithmetic |
| `scripts/install_systemd_units.sh` | `llmsverifier.service` added to `ENABLE_UNITS`; the "three installed units are not part of the deployed topology" note corrected to "two" and the stale `":8100 is unbound"` bullet replaced with the real root cause |

Enabling the unit only with `systemctl --user enable` would have regressed on the next
install; editing only the installed unit copy would have drifted from the template it is
generated from. Both were therefore changed at the source.

`bash -n scripts/install_systemd_units.sh` → OK (§11.4.67).

## 4. Live verification — captured

### 4a. Runtime context actually in effect

Journal, on the restarted unit (PID 818219, `ActiveEnterTimestamp` 2026-09-05 22:55:50 CEST):

```
srv    load_model: initializing, n_slots = 4, n_ctx_slot = 32768, kv_unified = 'true'
srv  llama_server: model loaded
srv  llama_server: listening on http://127.0.0.1:18434
```

API self-report:

```
$ curl -s http://127.0.0.1:18434/v1/models | ... meta
n_ctx: 32768   n_ctx_train: 32768
```

### 4b. §11.4.108 runtime signature — a request impossible at 4096 now succeeds

A 54,102-character prompt was sent with an exact-token echo instruction:

```
prompt_tokens: 12058
content: 'CTX32K-PROOF-OK'
RESULT: PASS
```

12,058 prompt tokens is ~2.9× the previous 4096 ceiling. This is the load-bearing
proof: not that configuration *declares* 32768, but that a request which provably could
not have been served before is served now, returning the exact expected token.

### 4c. llmsverifier enabled and in the boot graph

```
is-enabled: enabled
is-active : active
$ ls ~/.config/systemd/user/helix.target.wants/
helixagent.service  helixcode-server.service  helixllm-coder-native.service
helixllm-gateway.service  llmsverifier.service
$ loginctl show-user $USER -p Linger
Linger=yes
```

It now has its **own** boot-graph entry. Previously it ran only because
`helixllm-gateway.service` declares `Wants=llmsverifier.service` — a runtime `Wants=`
pulls a unit in regardless of enablement — so the CONST-036 source of truth would have
vanished whenever the gateway was stopped or reordered.

### 4d. No downstream breakage from the coder restart

```
helix.target               active
helixagent                 active
helixcode-server           active
helixllm-coder-native      active
helixllm-gateway           active
llmsverifier               active
```

Gateway end-to-end re-proven through the restarted coder (self-signed TLS, scoped
`--cacert`, no global verification disable):

```
gateway content: 'GATEWAY-OK'
```

## 5. Honest boundary (§11.4.6)

- Proven: the coder serves 32768-token contexts, and llmsverifier is enabled, active,
  and independently present in the boot graph.
- **Not** proven here: that any CLI agent now completes a full session against this
  endpoint. Agent-side configuration is separate work; a raised context removes the
  blocker but does not by itself wire any agent.
- **Not** proven here: reboot survival by observation. `Linger=yes` plus
  `helix.target.wants/` membership are the mechanisms, and both are verified — but no
  reboot was performed, so survival remains inferred from configuration rather than
  witnessed.
- The `NRestarts=2964` counter on llmsverifier is historical (from the pre-build
  crash loop) and persists across the successful start; `systemctl --user reset-failed
  llmsverifier.service` clears it cosmetically.
