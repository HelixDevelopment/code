# llamacpp route — post-restart end-to-end verification

Verifies commit `a74ae7cb` ("serve completions from a local llama.cpp endpoint
via the llamacpp route") is live in the running `helixcode-server` service and
serves a real local completion through HelixCode's public API.

## 1. The running binary was pre-fix (RED baseline)

| fact | value |
|---|---|
| commit `a74ae7cb` authored | 2026-09-03 17:59:34 +0200 |
| binary mtime before rebuild | 2026-09-03 13:42:38 |
| binary md5 before rebuild | `3f93fe526a59b84b90c81a4068c9e67a` |
| old service PID / start | 3530070, started 17:52:45 |

The process was started **before the commit existed**, so it could not contain
the fix. `HELIX_LLAMA_CPP_HOST` occurrences in the old binary: 0.

## 2. Rebuild (artifact layer, §11.4.108 L2)

`make build` → `go build -ldflags="… -X main.gitCommit=a74ae7cb"`

| fact | value |
|---|---|
| binary mtime after rebuild | 2026-09-03 18:06:01 |
| binary md5 after rebuild | `470cba80980f751db1b6b0f983edb6bb` |
| size | 113072288 → 113077168 |
| embedded `gitCommit` literal | `a74ae7cb` present |
| `HELIX_LLAMA_CPP_HOST` in binary | present, adjacent to `/v1/chat/completions` |

Note: an anchored `strings | grep '^KEY$'` finds these at count 0 even in the
fixed binary — Go concatenates const strings into one blob. Substring matching
is the correct measurement.

## 3. Config defect found and corrected (SOURCE→RUNTIME gap)

The commit corrected the tracked template `.env.example:75` to
`HELIX_LLAMA_CPP_HOST=http://localhost:18434`, but the live `.env` still carried
the pre-fix literal `http://localhost:8080` — HelixCode's **own** listener.
Since `HELIX_LLAMA_CPP_HOST` has the highest precedence in the new resolver,
the route would have POSTed completions to HelixCode itself (404 → 502): the
exact self-collision the commit documents as the pre-fix defect.

Verified the running service really received it:
`/proc/3530070/environ` → `HELIX_LLAMA_CPP_HOST=http://localhost:8080`.

Corrected `.env` line 55 only (91 lines before and after; mode 0600 preserved;
backup taken first). `HELIX_LLAMA_CPP_HOST` has zero readers in
`submodules/helix_llm`, so no other consumer was affected.

## 4. Restart via systemd

`systemctl --user restart helixcode-server` → 3.2s (ExecStartPost health gate passed).

| fact | value |
|---|---|
| new PID / start | 3950600, started 18:07:54 |
| cgroup | `…/app.slice/helixcode-server.service` (systemd-owned) |
| ppid | 4489 (`systemd --user`) — not a shell, so not a nohup orphan |
| helixcode processes | exactly 1 |
| owns :8080 | yes |
| effective env | `HELIX_LLAMA_CPP_HOST=http://localhost:18434` |

## 5. Nonce-proofed completion through the public API

Auth: `POST /api/v1/llm/generate` without a token → **401**
`{"message":"Authorization header required","status":"error"}`;
`POST /api/v1/auth/register` → **201**; `POST /api/v1/auth/login` → **200** (`.token`).

Nonce minted `16:14:01.168386Z`; request sent `16:14:01.186287Z` (18 ms later):

```
POST /api/v1/llm/generate   Authorization: Bearer <redacted>
{"provider":"llamacpp","prompt":"Repeat this number exactly, nothing else: 906335","max_tokens":8,"temperature":0}
```

Response — **HTTP 200 (25.78s)**, verbatim:

```json
{"content":"906335","finish_reason":"stop","model":"qwen2.5-coder-3b-instruct-q4_k_m","provider":"llamacpp","status":"success","usage":{"completion_tokens":7,"prompt_tokens":44,"total_tokens":51}}
```

`.content` is exactly the freshly-minted nonce, with a real model id and real
token usage — not reproducible from a cache or replay.

## 6. Default path unchanged (non-regression)

No provider named, and explicit `provider":"ollama"` — both **502**, verbatim:

```json
{"error":"generation failed: API request failed: API request failed: Post \"http://localhost:11434/api/chat\": dial tcp 127.0.0.1:11434: connect: connection refused","provider":"ollama","status":"error"}
```

Names **ollama**, dials Ollama's native `/api/chat` at the historical default
port (not `/v1/chat/completions`), and ollama is not installed on this host —
so the 502 is the correct answer and nothing silently re-routed to llamacpp.

## 7. Defect found, NOT fixed here: `write_timeout: 30` truncates slow generations

A first attempt with `max_tokens=64` returned `curl: (52) Empty reply from
server` at 100.7s while the server logged
`[GIN] … | 200 | 1m40s | POST "/api/v1/llm/generate"`.

Root cause: `config/config.yaml:8` sets `write_timeout: 30`, so Go's
`http.Server` write deadline expires ~30s after headers are read. The handler
writes nothing until generation completes at 100s; that write then fails and
the connection closes. **Gin logs 200 while the client receives no body** —
a silent-failure surface for any generation slower than `write_timeout`.
Not changed here: it is shipped tracked config outside this task's scope.
