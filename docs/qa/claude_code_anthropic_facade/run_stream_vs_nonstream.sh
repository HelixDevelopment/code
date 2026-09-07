#!/usr/bin/env bash
# Isolate the empty-stream defect: identical request body, stream:false vs
# stream:true, through the facade, at a Claude-Code-sized system prompt and
# with tools present. Ephemeral instance only.
set -uo pipefail
ROOT=/home/milosvasic/Projects/helix_code
OUT="$ROOT/docs/qa/claude_code_anthropic_facade"
BIN="$ROOT/helix_code/bin/helixcode"; SRC_CFG="$ROOT/helix_code/config/config.yaml"
set -a; . "$ROOT/.env" >/dev/null 2>&1; set +a
EPH_KEY="eph-$(head -c 16 /dev/urandom | od -An -tx1 | tr -d ' \n')"
PORT="$(python3 -c 'import socket;s=socket.socket();s.bind(("127.0.0.1",0));print(s.getsockname()[1]);s.close()')"
CFG="$OUT/ephemeral-config.yaml"
awk -v port="$PORT" '
    /^server:/ { blk="server"; print; next }
    /^redis:/  { blk="redis";  print; next }
    /^[a-z_]+:/ { blk="" }
    blk=="server" && /^[[:space:]]+port:/    { sub(/port:.*/, "port: " port); print; next }
    blk=="server" && /^[[:space:]]+address:/ { sub(/address:.*/, "address: \"127.0.0.1\""); print; next }
    blk=="redis"  && /^[[:space:]]+enabled:/ { sub(/enabled:.*/, "enabled: false"); print; next }
    { print }' "$SRC_CFG" > "$CFG"
( cd "$ROOT/helix_code" && env HELIX_CONFIG="$CFG" HELIX_AUTOBOOT_INFRA=false \
    HELIX_LLM_PROVIDER=local HELIX_WIRE_FACADE_API_KEYS="$EPH_KEY" "$BIN" server ) \
    > "$OUT/ephemeral-server.log" 2>&1 &
PID=$!; trap 'kill "$PID" 2>/dev/null; sleep 1; kill -9 "$PID" 2>/dev/null' EXIT
for _ in $(seq 1 45); do kill -0 "$PID" 2>/dev/null || { echo BOOT_FAILED; exit 1; }
  curl -sS -m 2 -o /dev/null "http://127.0.0.1:${PORT}/health" 2>/dev/null && break; sleep 1; done
echo "-- ephemeral on :${PORT}"; echo

python3 - "$PORT" "$EPH_KEY" <<'PY'
import json, sys, urllib.request, urllib.error
port, key = sys.argv[1], sys.argv[2]
url = f"http://127.0.0.1:{port}/v1/messages?beta=true"

# ~9k-char system prompt, the size Claude Code sends (measured: 9036 chars).
sysprompt = ("You are Claude Code, Anthropic's official CLI for Claude. You are an "
             "interactive CLI tool that helps users with software engineering tasks. "
             "Use the instructions below and the tools available to you to assist the user. ")
sysprompt = (sysprompt * 40)[:9036]

tools = [{"name": "Read", "description": "Read a file from the filesystem",
          "input_schema": {"type": "object", "properties": {"file_path": {"type": "string"}},
                           "required": ["file_path"]}}]

def post(body, stream):
    req = urllib.request.Request(url, data=json.dumps(body).encode(),
        headers={"content-type": "application/json", "x-api-key": key,
                 "anthropic-version": "2023-06-01"})
    try:
        r = urllib.request.urlopen(req, timeout=180)
        return r.status, r.read().decode("utf-8", "replace")
    except urllib.error.HTTPError as e:
        return e.code, e.read().decode("utf-8", "replace")

cases = [
    ("D1 short system, NO tools ", {"system": "You are terse.", "tools": None, "max_tokens": 512}),
    ("D2 short system, WITH tools", {"system": "You are terse.", "tools": tools, "max_tokens": 512}),
    ("D3 9036-char system, NO tools ", {"system": sysprompt, "tools": None, "max_tokens": 512}),
    ("D4 9036-char system, WITH tools", {"system": sysprompt, "tools": tools, "max_tokens": 512}),
    ("D5 9036-char system, WITH tools, max_tokens=32000", {"system": sysprompt, "tools": tools, "max_tokens": 32000}),
]
for label, c in cases:
    for stream in (False, True):
        body = {"model": "helix-local", "max_tokens": c["max_tokens"], "system": c["system"],
                "stream": stream,
                "messages": [{"role": "user", "content": "Reply with exactly: FACADE_OK"}]}
        if c["tools"]:
            body["tools"] = c["tools"]
        st, txt = post(body, stream)
        if stream:
            deltas = txt.count("content_block_delta")
            text = "".join(json.loads(l[6:])["delta"]["text"]
                           for l in txt.splitlines()
                           if l.startswith("data: ") and '"content_block_delta"' in l)
            print(f"{label} | stream=True  | HTTP {st} | content_block_delta frames={deltas} | text={text!r}")
        else:
            try:
                blocks = json.loads(txt).get("content", [])
                text = "".join(b.get("text", "") for b in blocks)
            except Exception:
                text = txt[:200]
            print(f"{label} | stream=False | HTTP {st} | text={text!r}")
    print()
PY
echo "-- stream-vs-nonstream complete"
