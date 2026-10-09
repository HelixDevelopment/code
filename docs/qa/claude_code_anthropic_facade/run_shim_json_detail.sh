#!/usr/bin/env bash
# Follow-up: Claude Code exits 0 through the shim but prints no text.
# Capture the structured result + the exact upstream response bodies.
set -uo pipefail
ROOT=/home/milosvasic/Projects/helix_code
OUT="$ROOT/docs/qa/claude_code_anthropic_facade"
BIN="$ROOT/helix_code/bin/helixcode"
SRC_CFG="$ROOT/helix_code/config/config.yaml"
THROWAWAY="$OUT/throwaway-claude-config"; WORKDIR="$OUT/throwaway-workdir"
SHIMLOG="$OUT/shim-detail.log"
set -a; . "$ROOT/.env" >/dev/null 2>&1; set +a
rm -rf "$THROWAWAY" "$WORKDIR" "$SHIMLOG" "$OUT/sse-dump.txt"; mkdir -p "$THROWAWAY" "$WORKDIR"
echo "hello from the throwaway workdir" > "$WORKDIR/probe.txt"

EPH_KEY="eph-$(head -c 16 /dev/urandom | od -An -tx1 | tr -d ' \n')"
fp() { python3 -c 'import socket;s=socket.socket();s.bind(("127.0.0.1",0));print(s.getsockname()[1]);s.close()'; }
PORT="$(fp)"; SHIMPORT="$(fp)"; CFG="$OUT/ephemeral-config.yaml"
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
PID=$!; SHIMPID=""
trap 'kill "$PID" "$SHIMPID" 2>/dev/null; sleep 1; kill -9 "$PID" "$SHIMPID" 2>/dev/null' EXIT
for _ in $(seq 1 45); do
    kill -0 "$PID" 2>/dev/null || { echo BOOT_FAILED; exit 1; }
    curl -sS -m 2 -o /dev/null "http://127.0.0.1:${PORT}/health" 2>/dev/null && break; sleep 1
done
SHIM_UPSTREAM="http://127.0.0.1:${PORT}" SHIM_LOG="$SHIMLOG" SHIM_SSE_DUMP="$OUT/sse-dump.txt" \
    python3 "$OUT/system_flatten_shim.py" "$SHIMPORT" & SHIMPID=$!
sleep 2
BASE="http://127.0.0.1:${SHIMPORT}"
printf '{ "env": { "ANTHROPIC_BASE_URL": "%s" } }\n' "$BASE" > "$THROWAWAY/settings.json"

echo "### Claude Code, --output-format json (structured result)"
( cd "$WORKDIR" && env -u ANTHROPIC_API_KEY CLAUDE_CONFIG_DIR="$THROWAWAY" \
    ANTHROPIC_BASE_URL="$BASE" ANTHROPIC_AUTH_TOKEN="$EPH_KEY" \
    ANTHROPIC_MODEL="helix-local" ANTHROPIC_SMALL_FAST_MODEL="helix-local" \
    CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC=1 CLAUDE_CODE_ATTRIBUTION_HEADER=0 \
    timeout 240 claude -p "Reply with exactly: FACADE_OK" --output-format json ) 2>&1 \
  | sed -e "s|${EPH_KEY}|<EPHEMERAL_KEY_REDACTED>|g" | tail -c 2500
echo; echo

echo "### The exact response the facade returned to Claude Code (replayed with the SAME body Claude Code sent)"
# Replay: reconstruct a minimal Claude-Code-shaped request and show the full body.
curl -sS -m 120 -X POST "http://127.0.0.1:${PORT}/v1/messages?beta=true" \
  -H 'content-type: application/json' -H "x-api-key: ${EPH_KEY}" \
  -H 'anthropic-version: 2023-06-01' \
  -d '{"model":"helix-local","max_tokens":512,"system":"You are Claude Code, Anthropic'\''s official CLI for Claude.","messages":[{"role":"user","content":[{"type":"text","text":"Reply with exactly: FACADE_OK"}]}],"tools":[{"name":"Read","description":"Read a file","input_schema":{"type":"object","properties":{"file_path":{"type":"string"}},"required":["file_path"]}}]}' \
  | sed -e "s|${EPH_KEY}|<EPHEMERAL_KEY_REDACTED>|g" | head -c 1200
echo; echo

echo "### shim log"; tail -20 "$SHIMLOG"
echo; echo "### facade access log"; grep -E '\[GIN\]' "$OUT/ephemeral-server.log" | tail -15

echo; echo "### SSE the facade actually streamed to Claude Code"; head -c 3000 "$OUT/sse-dump.txt"
