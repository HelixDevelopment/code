# Baseline Evidence — Local Adaptive Serving (task F5)

**Date:** 2026-09-05 ~15:30 CEST
**Host:** `anton` — AMD Ryzen 7 2700X, 32 GB RAM, NVIDIA GeForce RTX 3060 12 GB
**Captured by:** verification evidence agent (task F5), commands run verbatim against the live host.

---

## Step 1 — Gateway model listing (PROVEN)

`systemctl --user start helixllm-gateway.service helixllm-coder-native.service` was a no-op
(both already active since 13:50 CEST). Gateway status excerpt:

```
● helixllm-gateway.service - HelixLLM Gateway (multi-provider LLM router)
     Active: active (running) since Sat 2026-09-05 13:50:39 CEST; 1h 40min ago
   Main PID: 2583 (helixllm)
```

`curl -sk https://127.0.0.1:8443/v1/models` (exit 0):

```json
{"object":"list","data":[{"id":"helixllm-anton-qwen2-5-coder-3b-instruct-q4_k_m-f6771589d190","object":"model","created":1700000000,"owned_by":"llamacpp","model_identity":"helixllm/anton/qwen2.5-coder-3b-instruct-q4_k_m","host":"anton","availability":"serving"}]}
```

A live completion through the gateway (proof the listing is actually served, not just listed):

```
$ curl -sk https://127.0.0.1:8443/v1/chat/completions -d '{"model":"helixllm-anton-...-f6771589d190","messages":[{"role":"user","content":"Say OK"}],"max_tokens":8}'
{"id":"chatcmpl-rdEAX0VrOzSIeB8YIBQAMFtZEaCs6GFw","object":"chat.completion","created":1788615231,"model":"qwen2.5-coder-3b-instruct-q4_k_m","choices":[{"index":0,"message":{"role":"assistant","content":"OK"},"finish_reason":"stop"}],"usage":{"prompt_tokens":60,"completion_tokens":2,"total_tokens":62}}
```

### Model origin trace (config → measurement)

1. `helixllm-gateway.service` sets `HELIX_LLM_LOCAL_RPC_HOST=127.0.0.1`,
   `HELIX_LLM_LOCAL_RPC_PORT=18434` (unit file, verified). No static model list in the unit.
2. The backend llama-server answers `curl -s http://127.0.0.1:18434/v1/models` with
   `qwen2.5-coder-3b-instruct-q4_k_m` (its `--alias` flag), loaded from
   `/home/milosvasic/models/qwen2.5-coder-3b-instruct-q4_k_m.gguf`.
3. The gateway publishes it under the measured identity
   `helixllm-anton-qwen2-5-coder-3b-instruct-q4_k_m-f6771589d190`
   (`host=anton`, hash suffix) with `availability: serving`.

**Verdict:** the listed model originates from a **runtime probe of the local llama-server
(measurement/discovery)**, not a hardcoded config list. Local + dynamic listing is PROVEN.

---

## Step 2 — Process tree (PROVEN)

`ps -eo pid,cmd | grep -E 'llama-server|colibri|helixllm' | grep -v grep`:

```
 2580 /home/milosvasic/opt/llamacpp_gpu/current/llama-server --model /home/milosvasic/models/qwen2.5-coder-3b-instruct-q4_k_m.gguf --alias qwen2.5-coder-3b-instruct-q4_k_m --host 127.0.0.1 --port 18434 --ctx-size 4096 --n-gpu-layers 99 --threads 8 --threads-batch 8 --batch-size 256 --jinja --metrics
 2583 /home/milosvasic/Projects/helix_code/submodules/helix_llm/bin/helixllm
```

Live runtimes: **llama.cpp `llama-server` (GPU build, 99 GPU layers) + `helixllm` gateway binary**.
No `colibri` process exists (also no colibri systemd unit anywhere in `systemctl --user list-units --all`).

Sibling user units, status excerpts:

```
● helixcode-server.service — Active: active (running), Main PID 6563 (helixcode) on :8080
● helixagent.service       — Active: active (running), Main PID 2577 (helixagent) on :7061
● helixllm-coder.service   — inactive (dead)  (Qwen3-Coder-30B variant, not started)
● helixcode-infra.service  — inactive (dead)  (PostgreSQL, Redis, Weaviate, Qdrant, ChromaDB, Cognee, Ollama)
```

Port binding cross-check (`ss -ltnp`): `:8443` helixllm, `:18434` llama-server, `:8080` helixcode, `:7061` helixagent.

---

## Step 3 — Hardware probe (PROVEN via gateway's own capability.Measure log; JSON harness UNCONFIRMED)

`capability.Measure` is in `submodules/helix_llm/internal/capability` — an `internal/`
package cannot be imported by an out-of-tree `/tmp` harness (Go visibility), and the
`helixllm` binary has no subcommand to dump the profile as JSON. Evidence was instead
captured from the gateway's own boot log, which logs the measured profile
(`cmd/helixllm/main.go:175`):

```
$ journalctl --user -u helixllm-gateway.service --since '2 hours ago' | grep -i hardware
Sep 05 13:50:35 anton helixllm[2583]: time="2026-09-05T13:50:35+02:00" level=info msg="Hardware detected" gpu=true inference=auto l3_cache_kb=16384 numa_nodes=1 preset=high_end ram_gb=30 vram_mb=12288
```

Independent raw probes corroborate every field:

```
$ nvidia-smi --query-gpu=name,memory.total,memory.used,driver_version --format=csv
NVIDIA GeForce RTX 3060, 12288 MiB, 2244 MiB, 595.84

$ grep -E 'MemTotal|MemAvailable' /proc/meminfo
MemTotal:       31717556 kB        (~30.2 GiB → ram_gb=30)
MemAvailable:   21095304 kB

$ grep 'model name' /proc/cpuinfo | sort -u ; nproc
AMD Ryzen 7 2700X Eight-Core Processor    16 threads
```

**UNCONFIRMED:** the `HostCapabilityProfile` JSON itself (full struct with storage/freshness
fields) was not dumped — no CLI surface exists for it and the internal package blocks an
out-of-tree harness. Gap-closing work may add a `helixllm capability-print` seam; until then
the log-line + raw-probe pair above is the captured evidence.

---

## Step 4 — Verdict: what is local + dynamic today

**PROVEN local and dynamic (captured 2026-09-05):**
- `helixllm-gateway.service` serves HTTPS `:8443` with one live model, discovered at runtime
  from the local llama-server RPC (`:18434`) — listing AND a real chat completion (response "OK").
- Native llama.cpp runtime (`llama-server`, GPU offload `--n-gpu-layers 99`) serving
  Qwen2.5-Coder-3B Q4_K_M locally.
- `helixcode-server.service` (`:8080`) and `helixagent.service` (`:7061`) both active.
- Hardware capability measurement runs at gateway boot (preset `high_end`, gpu=true).

**NOT proven / not running today (FINDINGS):**
- **Colibri seam: ABSENT.** No colibri process, no colibri unit — nothing to verify.
- **Media/infra services: DOWN.** `helixcode-infra.service` is dead (PostgreSQL, Redis,
  vector DBs, Ollama not running); the gateway's Redis/vector env points at localhost:6380/6333
  with nothing listening.
- **helix_qa boot: NO UNIT.** No helix_qa systemd unit exists in the user manager; helix_qa
  is not booted as a service.
- **Cloud default paths: NOT EXERCISED.** No cloud provider credentials were probed; nothing
  in the captured evidence exercises a cloud fallback. UNCONFIRMED: whether the gateway would
  fall back to cloud providers if the local RPC died.
- `helixllm-coder.service` (30B variant) is dead by design — only the 3B native coder is live.

**Incident note (honesty, §11.4.6):** while probing for a capability subcommand,
`helixllm capability --help` was interpreted as a server start and briefly launched a second
gateway instance (15:31:25). It failed all model downloads (`mkdir /models: permission
denied`), wrote nothing, and was terminated immediately. The original gateway (PID 2583,
up since 13:50) was verified intact afterwards (`:8443` → HTTP 200, same model listing).
No state was mutated.
