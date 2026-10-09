#!/usr/bin/env bash
# Probe the facade with the REQUEST SHAPES Claude Code actually sends, per
# https://code.claude.com/docs/en/llm-gateway-protocol (accessed 2026-09-05):
#   - system as an ARRAY of blocks (attribution block first, cache_control)
#   - message content as an array of typed blocks
#   - thinking: {"type":"adaptive"}  (sent for models it does not recognise)
#   - anthropic-version / anthropic-beta headers, /v1/messages?beta=true path
#   - HEAD /api/hello connection-warming probe
# Ephemeral instance only; never touches the live :8080 unit.
set -uo pipefail

ROOT=/home/milosvasic/Projects/helix_code
OUT="$ROOT/docs/qa/claude_code_anthropic_facade"
BIN="$ROOT/helix_code/bin/helixcode"
SRC_CFG="$ROOT/helix_code/config/config.yaml"
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
    { print }
' "$SRC_CFG" > "$CFG"

( cd "$ROOT/helix_code" && env HELIX_CONFIG="$CFG" HELIX_AUTOBOOT_INFRA=false \
    HELIX_LLM_PROVIDER=local HELIX_WIRE_FACADE_API_KEYS="$EPH_KEY" "$BIN" server ) \
    > "$OUT/ephemeral-server.log" 2>&1 &
PID=$!
trap 'kill "$PID" 2>/dev/null; sleep 1; kill -9 "$PID" 2>/dev/null' EXIT
for _ in $(seq 1 45); do
    kill -0 "$PID" 2>/dev/null || { echo "BOOT FAILED"; tail -3 "$OUT/ephemeral-server.log"; exit 1; }
    curl -sS -m 2 -o /dev/null "http://127.0.0.1:${PORT}/health" 2>/dev/null && break
    sleep 1
done
echo "-- ephemeral (HELIX_LLM_PROVIDER=local, facade key configured) on 127.0.0.1:${PORT}"
echo

H=(-H 'content-type: application/json' -H "x-api-key: ${EPH_KEY}"
   -H 'anthropic-version: 2023-06-01'
   -H 'anthropic-beta: context-management-2025-06-27,fine-grained-tool-streaming-2025-05-14'
   -H 'x-claude-code-session-id: probe-session-0001')

run() { # run <label> <path> <body>
    echo "--- $1"
    curl -sS -i -m 120 -X POST "http://127.0.0.1:${PORT}$2" "${H[@]}" -d "$3" \
      | sed -e "s|${EPH_KEY}|<EPHEMERAL_KEY_REDACTED>|g" \
      | grep -vE '^(Access-Control|Strict-Transport|X-Content-Type|X-Frame|X-Xss|Date:)' | head -c 900
    echo; echo
}

echo "--- C0  HEAD /api/hello  (connection-warming probe; docs say safe to reject)"
curl -sS -i -m 10 -I "http://127.0.0.1:${PORT}/api/hello" | head -3
echo

run "C1  system as a plain STRING (the only shape the facade documents)" \
  "/v1/messages?beta=true" \
  '{"model":"claude-sonnet-4-20250514","max_tokens":32,"system":"You are Claude Code.","messages":[{"role":"user","content":"Reply with exactly: FACADE_OK"}]}'

run "C2  system as an ARRAY of blocks — THE SHAPE CLAUDE CODE ACTUALLY SENDS" \
  "/v1/messages?beta=true" \
  '{"model":"claude-sonnet-4-20250514","max_tokens":32,"system":[{"type":"text","text":"You are Claude Code, Anthropic'\''s official CLI for Claude."},{"type":"text","text":"You are an interactive CLI tool.","cache_control":{"type":"ephemeral"}}],"messages":[{"role":"user","content":"Reply with exactly: FACADE_OK"}]}'

run "C3  message content as an ARRAY of typed blocks + cache_control" \
  "/v1/messages?beta=true" \
  '{"model":"claude-sonnet-4-20250514","max_tokens":32,"messages":[{"role":"user","content":[{"type":"text","text":"Reply with exactly: FACADE_OK","cache_control":{"type":"ephemeral"}}]}]}'

run "C4  thinking:{\"type\":\"adaptive\"} (sent for unrecognised model names)" \
  "/v1/messages?beta=true" \
  '{"model":"claude-sonnet-4-20250514","max_tokens":32,"thinking":{"type":"adaptive"},"messages":[{"role":"user","content":"Reply with exactly: FACADE_OK"}]}'

run "C5  FULL Claude-Code-shaped request: array system + array content + tools + thinking + stream" \
  "/v1/messages?beta=true" \
  '{"model":"claude-sonnet-4-20250514","max_tokens":128,"stream":true,"thinking":{"type":"adaptive"},"system":[{"type":"text","text":"You are Claude Code."}],"tools":[{"name":"Bash","description":"Run a shell command","input_schema":{"type":"object","properties":{"command":{"type":"string"}},"required":["command"]}}],"messages":[{"role":"user","content":[{"type":"text","text":"Run ls using the Bash tool."}]}]}'

run "C6  tool_result turn (multi-turn agentic loop, content-block array)" \
  "/v1/messages?beta=true" \
  '{"model":"claude-sonnet-4-20250514","max_tokens":64,"messages":[{"role":"user","content":"Run ls"},{"role":"assistant","content":[{"type":"tool_use","id":"toolu_01","name":"Bash","input":{"command":"ls"}}]},{"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_01","content":"a.txt b.txt"}]}]}'

echo "-- real-shape probe complete"
