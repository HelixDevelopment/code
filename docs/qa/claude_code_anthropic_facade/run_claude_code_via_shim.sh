#!/usr/bin/env bash
# Claude Code -> system-flatten shim -> HelixCode Anthropic facade.
# Answers: once the ONE identified defect (array-form `system` rejected 400) is
# neutralised, does Claude Code actually work against this stack — and if not,
# what is the NEXT blocker? Throwaway CLAUDE_CONFIG_DIR only; live :8080 untouched.
set -uo pipefail

ROOT=/home/milosvasic/Projects/helix_code
OUT="$ROOT/docs/qa/claude_code_anthropic_facade"
BIN="$ROOT/helix_code/bin/helixcode"
SRC_CFG="$ROOT/helix_code/config/config.yaml"
THROWAWAY="$OUT/throwaway-claude-config"
WORKDIR="$OUT/throwaway-workdir"
SHIMLOG="$OUT/shim.log"
set -a; . "$ROOT/.env" >/dev/null 2>&1; set +a

rm -rf "$THROWAWAY" "$WORKDIR" "$SHIMLOG"; mkdir -p "$THROWAWAY" "$WORKDIR"
echo "hello from the throwaway workdir" > "$WORKDIR/probe.txt"

EPH_KEY="eph-$(head -c 16 /dev/urandom | od -An -tx1 | tr -d ' \n')"
freeport() { python3 -c 'import socket;s=socket.socket();s.bind(("127.0.0.1",0));print(s.getsockname()[1]);s.close()'; }
PORT="$(freeport)"; SHIMPORT="$(freeport)"
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
SHIMPID=""
trap 'kill "$PID" "$SHIMPID" 2>/dev/null; sleep 1; kill -9 "$PID" "$SHIMPID" 2>/dev/null' EXIT
for _ in $(seq 1 45); do
    kill -0 "$PID" 2>/dev/null || { echo "BOOT FAILED"; tail -3 "$OUT/ephemeral-server.log"; exit 1; }
    curl -sS -m 2 -o /dev/null "http://127.0.0.1:${PORT}/health" 2>/dev/null && break
    sleep 1
done

SHIM_UPSTREAM="http://127.0.0.1:${PORT}" SHIM_LOG="$SHIMLOG" \
    python3 "$OUT/system_flatten_shim.py" "$SHIMPORT" &
SHIMPID=$!
sleep 2
BASE="http://127.0.0.1:${SHIMPORT}"
echo "-- facade :${PORT} <- shim :${SHIMPORT} <- claude code"
echo

cat > "$THROWAWAY/settings.json" <<JSON
{ "env": { "ANTHROPIC_BASE_URL": "${BASE}" } }
JSON

echo "=============================================================="
echo "S1  claude -p (no tools needed)"
echo "=============================================================="
( cd "$WORKDIR" && env -u ANTHROPIC_API_KEY CLAUDE_CONFIG_DIR="$THROWAWAY" \
    ANTHROPIC_BASE_URL="$BASE" ANTHROPIC_AUTH_TOKEN="$EPH_KEY" \
    ANTHROPIC_MODEL="helix-local" ANTHROPIC_SMALL_FAST_MODEL="helix-local" \
    CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC=1 CLAUDE_CODE_ATTRIBUTION_HEADER=0 \
    timeout 240 claude -p "Reply with exactly: FACADE_OK" --output-format text ) 2>&1 \
  | sed -e "s|${EPH_KEY}|<EPHEMERAL_KEY_REDACTED>|g" | tail -25
echo "[claude exit: $?]"; echo

echo "=============================================================="
echo "S2  claude -p requiring a TOOL CALL (the agentic path)"
echo "=============================================================="
( cd "$WORKDIR" && env -u ANTHROPIC_API_KEY CLAUDE_CONFIG_DIR="$THROWAWAY" \
    ANTHROPIC_BASE_URL="$BASE" ANTHROPIC_AUTH_TOKEN="$EPH_KEY" \
    ANTHROPIC_MODEL="helix-local" ANTHROPIC_SMALL_FAST_MODEL="helix-local" \
    CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC=1 CLAUDE_CODE_ATTRIBUTION_HEADER=0 \
    timeout 240 claude -p "Read the file probe.txt in the current directory and tell me its contents." \
      --output-format text --allowedTools Read ) 2>&1 \
  | sed -e "s|${EPH_KEY}|<EPHEMERAL_KEY_REDACTED>|g" | tail -25
echo "[claude exit: $?]"; echo

echo "=============================================================="
echo "SHIM LOG (what the shim translated / what upstream answered)"
echo "=============================================================="
tail -40 "$SHIMLOG" 2>/dev/null
echo
echo "=============================================================="
echo "FACADE ACCESS LOG"
echo "=============================================================="
grep -E '\[GIN\]' "$OUT/ephemeral-server.log" | tail -25
echo
echo "-- shim e2e complete"
