# Colibri integration research — authoritative facts

**Date:** 2026-09-05 · **Task:** R1 (Wave 2) · **Upstream:** https://github.com/JustVugg/colibri
**Method:** fetched GitHub README, releases page, `c/Makefile`, `c/openai_server.py`, `docs/api.md` (raw where possible). Every claim carries URL + access date. Unverifiable items are marked `UNCONFIRMED:`.

---

## 1. What colibri is

Pure-C MoE disk-streaming inference engine. One `.c` file per model family over shared single headers; no BLAS, no Python at runtime, no GPU required. Treats VRAM/RAM/NVMe as a single expert-placement hierarchy (routed experts streamed from disk on demand). The launcher (`coli`) and the HTTP gateway (`openai_server.py`) are Python 3; the engine itself is dependency-free C.

- Source: [README — repo layout](https://github.com/JustVugg/colibri) (accessed 2026-09-05); [raw README.md](https://raw.githubusercontent.com/JustVugg/colibri/main/README.md) (accessed 2026-09-05).
- **No Go binding exists.** Integration shape is launched-process + HTTP client (see §5).

## 2. Build facts

| Fact | Value | Source |
|---|---|---|
| Toolchain | `gcc` (or clang) **with OpenMP**; macOS uses clang + Homebrew `libomp` (else builds single-threaded with a warning); Windows MinGW-w64/MSYS2 (gcc + libgomp + winpthreads) | [c/Makefile](https://raw.githubusercontent.com/JustVugg/colibri/main/c/Makefile) (accessed 2026-09-05); [README "Get started"](https://raw.githubusercontent.com/JustVugg/colibri/main/README.md) (accessed 2026-09-05) |
| Build-from-source command | `git clone https://github.com/JustVugg/colibri && cd colibri/c && ./setup.sh` (checks gcc/OpenMP, builds, self-tests) | [README](https://raw.githubusercontent.com/JustVugg/colibri/main/README.md) (accessed 2026-09-05) |
| Per-family build targets | `make -C c glm` (GLM-5.2), `make -C c glm53`, `make -C c inkling`, `make -C c kimi_k3`, `make -C c deepseek-v4`, `make -C c qwen38` (CPU only), `make -C c qwen36` (`CUDA=1` optional), `make -C c olmoe`. Root `make`/`make check`/`make clean` delegate to `c/Makefile` | [README model table](https://raw.githubusercontent.com/JustVugg/colibri/main/README.md) + [c/Makefile targets](https://raw.githubusercontent.com/JustVugg/colibri/main/c/Makefile) (accessed 2026-09-05) |
| Produced binaries (path) | `c/colibri` (GLM-5.2, from `colibri.c`), `c/glm53`, `c/inkling`, `c/kimi_k3`, `c/deepseek_v4`, `c/qwen36`, `c/qwen38`, `c/olmoe` — built in-place in `c/` (`-o <name>`); `make install` ships `coli` to `BINDIR` and engines to `LIBEXECDIR` | [c/Makefile build rules](https://raw.githubusercontent.com/JustVugg/colibri/main/c/Makefile) (accessed 2026-09-05) |
| Prebuilt alternative | Release archives (Linux/macOS/Windows) contain the engine(s) + `coli` launcher + Python helpers; `coli` finds the engine next to itself. Python 3 required for launcher/gateway | [README "Get colibri"](https://raw.githubusercontent.com/JustVugg/colibri/main/README.md) (accessed 2026-09-05) |
| `coli` on PATH | `pip install -e .` from a checkout registers the launcher (editable install; engine still lives in `c/`) | [README](https://raw.githubusercontent.com/JustVugg/colibri/main/README.md) (accessed 2026-09-05) |

`UNCONFIRMED:` exact compiler version floor (Makefile shows `-O3 -Wall -Wextra`, OpenMP; no documented minimum gcc/clang version in fetched sources).

## 3. Versioning

| Fact | Value | Source |
|---|---|---|
| Latest release tag | **v1.10.1** — packaging repair (missing Python files broke `coli convert`/`eval`/`mirror` in v1.10.0 archives); **"if you build from source, v1.10.1 is v1.10.0"** — no engine changes | [Releases](https://github.com/JustVugg/colibri/releases) (accessed 2026-09-05) |
| v1.10.0 highlights | Eighth family Qwen3.8-Flash-Next (engine + tool calling + vision); Qwen3.6 2.10× CPU decode fix | [Releases](https://github.com/JustVugg/colibri/releases) (accessed 2026-09-05) |
| Recommended commit | `UNCONFIRMED:` — the release page text does not expose the tag's commit SHA in the fetched content. Recommend: pin `v1.10.1` (or its SHA via `git rev-parse v1.10.1` after fetch) at integration time |
| Banner version string | `colibri v1.10.1` (README example) | [README](https://raw.githubusercontent.com/JustVugg/colibri/main/README.md) (accessed 2026-09-05) |

## 4. CLI invocation to serve / run a model

### 4.1 Launcher vs raw engine
- `coli` (Python) is the user-facing launcher: reads the model's `config.json`, picks the matching engine binary, renders that family's chat template. Flags style: `./coli chat|serve|web|plan|doctor|tune|convert|eval --model /nvme/glm52_i4` or env `COLI_MODEL=/nvme/glm52_i4`.
- Raw engines take the snapshot dir in the `SNAP` env var and the per-layer cache as a **positional** argument (e.g. `./qwen38 32`). Windows: `.exe` engines started alone exit immediately (no model bound). — [README](https://raw.githubusercontent.com/JustVugg/colibri/main/README.md) + [v1.10.0 release notes](https://github.com/JustVugg/colibri/releases) (accessed 2026-09-05).

### 4.2 Serve (the integration-relevant mode)
```bash
cd c
COLI_MODEL=/nvme/glm52_i4 COLI_API_KEY=local-secret ./coli serve \
  --host 127.0.0.1 --port 8000 --model-id glm-5.2-colibri
```
- `--model` required unless `COLI_MODEL` set; `--host` default `127.0.0.1`; `--port` default **8000**; `--model-id` default from family registry / `COLI_MODEL_ID`; `--api-key` from `COLI_API_KEY` (no key required by default — any non-empty string satisfies clients); `--max-queue` default 8 (`COLI_MAX_QUEUE`); `--queue-timeout` default 300 s (`COLI_QUEUE_TIMEOUT`); `--kv-slots` 1–16 (default 1, `COLI_KV_SLOTS`); `--cap`, `--max-tokens` (default 1024), `--cors-origin` (repeatable), `--allowed-host` / `COLI_ALLOWED_HOSTS`, `--engine`, `--arch auto`.
- Source: [docs/api.md](https://raw.githubusercontent.com/JustVugg/colibri/main/docs/api.md) and [c/openai_server.py argparse block](https://raw.githubusercontent.com/JustVugg/colibri/main/c/openai_server.py) (accessed 2026-09-05).

### 4.3 Model-path argument format
Path to a **directory** holding the converted safetensors container + `config.json` (e.g. `/nvme/glm52_i4`). Kimi K3, Qwen3.8-Flash-Next and DeepSeek V4 Flash stream the **original HF checkpoint** directly (no conversion). The launcher resolves the family from `config.json` `model_type`. — [README "Get the model" + model table](https://raw.githubusercontent.com/JustVugg/colibri/main/README.md) (accessed 2026-09-05).

## 5. HTTP surface (gateway = `c/openai_server.py`, Python stdlib only)

| Endpoint | Notes | Source |
|---|---|---|
| `GET /v1/models`, `GET /v1/models/{model}` | OpenAI model list | [docs/api.md](https://raw.githubusercontent.com/JustVugg/colibri/main/docs/api.md) (2026-09-05) |
| `POST /v1/chat/completions` | JSON + SSE streaming, usage, `max_tokens`/`max_completion_tokens`, `temperature`, `top_p`, up to 4 `stop`; OpenAI `tools`/`tool_choice` on GLM-5.2, DeepSeek V4, Kimi K3 | same |
| `POST /v1/completions` | legacy | same |
| `POST /v1/messages` | **Anthropic Messages API on the same port** (streaming with named events, `x-api-key` or Bearer auth); works with Claude Code via `ANTHROPIC_BASE_URL` | same |
| `GET /health` | Always `200 {"status":"ok"}` (liveness public); scheduler counters / kv_slots / tiers / hwinfo included only when authed (`COLI_API_KEY` set and presented) | [c/openai_server.py `/health` handler](https://raw.githubusercontent.com/JustVugg/colibri/main/c/openai_server.py) (2026-09-05) |
| `/experts`, static dashboard | routing telemetry (authed); dashboard is a pure OpenAI-API client | same; [docs/api.md](https://raw.githubusercontent.com/JustVugg/colibri/main/docs/api.md) (2026-09-05) |

Concurrency model: **one generation at a time** — requests queue through a bounded FIFO admission scheduler (default depth 8, 300 s timeout; saturated/timed-out requests get OpenAI-shaped HTTP **429** before streaming headers). Single process hosts one model. — [docs/api.md](https://raw.githubusercontent.com/JustVugg/colibri/main/docs/api.md) (2026-09-05).

Auth & bind safety: default bind is loopback; binding beyond localhost **without** `COLI_API_KEY` is refused (exit 1) unless `COLI_ALLOW_INSECURE_BIND=1`. DNS-rebinding guard via `--allowed-host`. — [c/openai_server.py `serve()`](https://raw.githubusercontent.com/JustVugg/colibri/main/c/openai_server.py) (2026-09-05).

## 6. Serving semantics (HelixCode launched-process integration facts)

| Concern | Verified fact | Source |
|---|---|---|
| Process model | Gateway (`openai_server.py`) spawns the C engine as a child process and speaks a line protocol over its stdio (`READY`/`END` frames); port bind happens **before** engine load so an occupied port fails in milliseconds | [c/openai_server.py `Engine.__init__` + `serve()`](https://raw.githubusercontent.com/JustVugg/colibri/main/c/openai_server.py) (2026-09-05) |
| Readiness signal (primary) | Gateway prints to **stderr**: `OpenAI-compatible API listening on http://{host}:{port}/v1` — emitted only **after** the engine's `READY` frame is consumed, i.e. after model load completes. This is the line HelixCode should tail before marking the server up | same |
| Readiness signal (engine stdout) | Interactive banner: `🐦 colibri v1.10.1 — GLM-5.2 · 744B MoE · int4 · streaming CPU` then `✓ ready in 32s · resident 9.9 GB` | [README](https://raw.githubusercontent.com/JustVugg/colibri/main/README.md) (2026-09-05) |
| `/health` semantics caveat | `/health` returns 200 `{"status":"ok"}` as soon as the port is bound — which occurs **before** the model finishes loading. It is gateway liveness, not model readiness. Model readiness = the stderr "listening" line above | [c/openai_server.py ordering: `APIServer((host, port), ...)` precedes `Engine(...)`](https://raw.githubusercontent.com/JustVugg/colibri/main/c/openai_server.py) (2026-09-05) |
| Daemonize | **None.** `coli serve` runs in the foreground (`server.serve_forever()`); no `--daemon`/detach flag exists in the gateway argparse. External supervisor (nohup/systemd/HelixCode process manager) is required | [c/openai_server.py `main()`/`serve()`](https://raw.githubusercontent.com/JustVugg/colibri/main/c/openai_server.py) (2026-09-05) |
| Graceful shutdown | **SIGTERM** → handler runs `server.shutdown()` on a daemon thread; **SIGINT/Ctrl-C** caught via `KeyboardInterrupt`; `finally` block closes the scheduler, the HTTP server, and `runtime.close()` (engine child). v1.6.0 release notes: "#850 — SIGTERM is handled". KV state persists across restarts (`.coli_kv`), conversations reopen warm | [c/openai_server.py `serve()`](https://raw.githubusercontent.com/JustVugg/colibri/main/c/openai_server.py); [Releases v1.6.0](https://github.com/JustVugg/colibri/releases); [README](https://raw.githubusercontent.com/JustVugg/colibri/main/README.md) (all 2026-09-05) |
| Engine crash mid-serve | Gateway surfaces engine `ERROR` frames (e.g. `CONTEXT_EXCEEDED` → HTTP 400 with `context_length_exceeded` code). `UNCONFIRMED:` exact HTTP behavior on unexpected engine child death (dispatcher_error path exists but was not fully traced) | [c/openai_server.py `_engine_error`](https://raw.githubusercontent.com/JustVugg/colibri/main/c/openai_server.py) (2026-09-05) |
| Load-time failure exit | Windows `.exe` engines "started on their own … exit immediately" (no model). Gateway fails model resolution at argparse time (`parser.error`); engine load failure before READY would block/deny the "listening" line — exact exit code `UNCONFIRMED:` | [README](https://raw.githubusercontent.com/JustVugg/colibri/main/README.md); [c/openai_server.py](https://raw.githubusercontent.com/JustVugg/colibri/main/c/openai_server.py) (2026-09-05) |
| Cluster mode (optional) | `./coli cluster coordinator --host 0.0.0.0 --port 8765`; workers `./coli cluster worker --model <dir> --port 9100 --coordinator http://COORD:8765 --advertise-host IP`; serve with `--cluster-coordinator http://127.0.0.1:8765` or `--cluster-workers HOST:PORT,...`. Transport disabled unless workers configured | [README "Local cluster mode"](https://raw.githubusercontent.com/JustVugg/colibri/main/README.md) (2026-09-05) |

## 7. License and model-family roster

- **License: Apache 2.0** (README "License" section). Note GLM-5.2 *weights* are MIT per Z.ai; other families' weights carry their own licenses (not enumerated upstream). — [README](https://raw.githubusercontent.com/JustVugg/colibri/main/README.md) (2026-09-05).
- **Closed roster of supported model families (8)** — each is one C file, same `coli chat`/`coli serve`/`coli web` front end:
  1. **GLM-5.2** (744B/40B active; `make -C c glm`; reference model)
  2. **GLM-5.3-Flash** (321B/40B, vision; `make -C c glm53`)
  3. **Inkling** (975B/41B; `make -C c inkling`)
  4. **Kimi K3** (2.8T/104B, native MXFP4; `make -C c kimi_k3`)
  5. **DeepSeek V4 Flash** (284B/13B, native fp4; `make -C c deepseek-v4`)
  6. **Qwen3.8-Flash-Next** (125B+51B n-gram/6B; `make -C c qwen38`; **CPU only, no GPU tier**)
  7. **Qwen3.6-35B-A3B** (35B/3B; `make -C c qwen36`; optional CUDA VRAM tier)
  8. **OLMoE** (7B/1B; `make -C c olmoe`)
- README: "Eight families run today … one C file each, the same `coli chat` / `coli serve` / `coli web` front end." MiniMax named as a candidate, not shipped. — [README](https://raw.githubusercontent.com/JustVugg/colibri/main/README.md) (2026-09-05).

## 8. Practical notes for the HelixCode adapter

1. **Integration shape:** spawn `coli serve --model <dir> --host 127.0.0.1 --port <p>` (from `c/` or an installed `coli` on PATH), wait for stderr line `OpenAI-compatible API listening on http://...`, then drive it with a standard OpenAI-compatible client (`/v1/chat/completions`) or Anthropic client (`/v1/messages`). No Go binding needed.
2. **Runtime deps:** Python 3 for launcher/gateway (stdlib-only gateway); engine itself is pure C. `COLI_API_KEY` should be set when binding beyond loopback (fail-closed otherwise).
3. **Readiness:** tail stderr for the "listening" line; do **not** treat `GET /health` 200 as model-ready (port is bound before weights load; a 372 GB GLM-5.2 load takes tens of seconds; `.coli_kv` warm restarts skip re-prefill).
4. **Shutdown:** send SIGTERM (or SIGINT) — graceful path closes scheduler, HTTP server, and engine child, persisting `.coli_kv`.
5. **Concurrency ceiling:** one generation at a time, FIFO queue depth 8 default → 429 under saturation. HelixCode's provider adapter must handle 429 + `x-colibri-queue-wait-ms` rather than retry-storm.
6. **Model family dispatch:** the launcher picks the engine from `config.json`; HelixCode only passes the model directory (`COLI_MODEL`/`--model`).
7. **Version pinning:** latest tag v1.10.1 (accessed 2026-09-05). Pin exact SHA after clone (`UNCONFIRMED:` here, resolve at build time).

## 9. UNCONFIRMED items (not verifiable from fetched sources)

- Exact commit SHA of tag `v1.10.1` (not exposed in fetched release-page text).
- Minimum gcc/clang version floor.
- Exit code / stdout behavior when the engine child dies unexpectedly mid-serve (dispatcher path seen, full trace not done).
- Exact exit code when model load fails under `coli serve` (READY never arrives; documented line only says engines without a model "exit immediately").
- Whether `/v1/models` lists a fixed id or the `--model-id` value (implied `--model-id`; not explicitly asserted in docs).
- Website docs at https://justvugg.github.io/colibri (README's Mintlify site) — not fetched; README + raw files used instead (README is canonical per repo).
