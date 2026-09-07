#!/usr/bin/env bash
# Model-id behaviour experiment against the Anthropic facade.
# Two ephemeral instances, identical except HELIX_LLM_PROVIDER:
#   ROUND A: provider unset  -> resolves to Ollama  (strict model registry)
#   ROUND B: HELIX_LLM_PROVIDER=local -> llama.cpp coder (ignores model id)
# Never touches the live :8080 unit; only signals PIDs this script launched.
set -uo pipefail

ROOT=/home/milosvasic/Projects/helix_code
OUT="$ROOT/docs/qa/claude_code_anthropic_facade"
BIN="$ROOT/helix_code/bin/helixcode"
SRC_CFG="$ROOT/helix_code/config/config.yaml"

set -a; . "$ROOT/.env" >/dev/null 2>&1; set +a

PID=""
stop() { [ -n "$PID" ] && kill "$PID" 2>/dev/null; sleep 1; [ -n "$PID" ] && kill -9 "$PID" 2>/dev/null; PID=""; }
trap stop EXIT

boot() { # boot <extra env assignments...>
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
        HELIX_WIRE_FACADE_API_KEYS="$EPH_KEY" "$@" "$BIN" server ) > "$OUT/ephemeral-server.log" 2>&1 &
    PID=$!
    for _ in $(seq 1 45); do
        kill -0 "$PID" 2>/dev/null || { echo "BOOT FAILED: $(tail -2 "$OUT/ephemeral-server.log")"; return 1; }
        curl -sS -m 2 -o /dev/null "http://127.0.0.1:${PORT}/health" 2>/dev/null && return 0
        sleep 1
    done
    return 1
}

msg() { # msg <label> <json-body>
    echo "--- $1"
    curl -sS -m 120 -X POST "http://127.0.0.1:${PORT}/v1/messages" \
        -H 'content-type: application/json' -H "x-api-key: ${EPH_KEY}" -d "$2" \
      | sed -e "s|${EPH_KEY}|<EPHEMERAL_KEY_REDACTED>|g" | head -c 700
    echo; echo
}

EPH_KEY="eph-$(head -c 16 /dev/urandom | od -An -tx1 | tr -d ' \n')"

echo "=========================================================================="
echo "ROUND A — HELIX_LLM_PROVIDER unset (default resolution)"
echo "=========================================================================="
if boot; then
    echo "(ephemeral on 127.0.0.1:${PORT})"; echo
    msg "A1 model=claude-sonnet-4-20250514 (a Claude Code model id)" \
      '{"model":"claude-sonnet-4-20250514","max_tokens":32,"messages":[{"role":"user","content":"Reply with exactly: FACADE_OK"}]}'
    msg "A2 model=qwen2.5:3b-instruct-q4_K_M (a model Ollama actually has)" \
      '{"model":"qwen2.5:3b-instruct-q4_K_M","max_tokens":32,"messages":[{"role":"user","content":"Reply with exactly: FACADE_OK"}]}'
    msg "A3 model field OMITTED (resolveDefaultModel picks from the catalog)" \
      '{"max_tokens":32,"messages":[{"role":"user","content":"Reply with exactly: FACADE_OK"}]}'
    msg "A4 model=\"\" (empty string)" \
      '{"model":"","max_tokens":32,"messages":[{"role":"user","content":"Reply with exactly: FACADE_OK"}]}'
fi
stop

echo "=========================================================================="
echo "ROUND B — HELIX_LLM_PROVIDER=local (llama.cpp coder sidecar :18434)"
echo "=========================================================================="
if boot HELIX_LLM_PROVIDER=local; then
    echo "(ephemeral on 127.0.0.1:${PORT})"; echo
    msg "B1 model=claude-sonnet-4-20250514 (a Claude Code model id)" \
      '{"model":"claude-sonnet-4-20250514","max_tokens":32,"messages":[{"role":"user","content":"Reply with exactly: FACADE_OK"}]}'
    msg "B2 model=claude-3-5-haiku-20241022 (Claude Code'\''s small/background model)" \
      '{"model":"claude-3-5-haiku-20241022","max_tokens":32,"messages":[{"role":"user","content":"Reply with exactly: FACADE_OK"}]}'
    msg "B3 model=totally-made-up-model-xyz (arbitrary id)" \
      '{"model":"totally-made-up-model-xyz","max_tokens":32,"messages":[{"role":"user","content":"Reply with exactly: FACADE_OK"}]}'
    echo "--- B4 stream:true with a claude-* id (Claude Code always streams)"
    curl -sS -N -m 120 -X POST "http://127.0.0.1:${PORT}/v1/messages" \
      -H 'content-type: application/json' -H "x-api-key: ${EPH_KEY}" \
      -d '{"model":"claude-sonnet-4-20250514","max_tokens":48,"stream":true,"messages":[{"role":"user","content":"Reply with exactly: FACADE_OK"}]}' \
      | sed -e "s|${EPH_KEY}|<EPHEMERAL_KEY_REDACTED>|g" | head -c 1800
    echo; echo
    echo "--- B5 system + tools[] (the shape Claude Code actually sends)"
    curl -sS -m 120 -X POST "http://127.0.0.1:${PORT}/v1/messages" \
      -H 'content-type: application/json' -H "x-api-key: ${EPH_KEY}" \
      -d '{"model":"claude-sonnet-4-20250514","max_tokens":200,"system":"You are Claude Code.","tools":[{"name":"Bash","description":"Run a shell command","input_schema":{"type":"object","properties":{"command":{"type":"string"}},"required":["command"]}}],"messages":[{"role":"user","content":"List files in the current directory using the Bash tool."}]}' \
      | sed -e "s|${EPH_KEY}|<EPHEMERAL_KEY_REDACTED>|g" | head -c 900
    echo; echo
fi
stop
echo "-- model-id experiment complete"
