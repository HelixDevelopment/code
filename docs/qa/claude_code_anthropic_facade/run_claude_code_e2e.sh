#!/usr/bin/env bash
# REAL Claude Code (v2.1.261) driven against the HelixCode Anthropic facade.
#
# SAFETY (task constraint 4): a THROWAWAY CLAUDE_CONFIG_DIR under this QA
# directory is used for every run. The operator's live ~/.claude* dirs are
# never read, written, or referenced. The live :8080 unit is never touched;
# only the ephemeral PID this script launched is signalled (§11.4.174).
set -uo pipefail

ROOT=/home/milosvasic/Projects/helix_code
OUT="$ROOT/docs/qa/claude_code_anthropic_facade"
BIN="$ROOT/helix_code/bin/helixcode"
SRC_CFG="$ROOT/helix_code/config/config.yaml"
THROWAWAY="$OUT/throwaway-claude-config"
WORKDIR="$OUT/throwaway-workdir"

set -a; . "$ROOT/.env" >/dev/null 2>&1; set +a

rm -rf "$THROWAWAY" "$WORKDIR"; mkdir -p "$THROWAWAY" "$WORKDIR"

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
BASE="http://127.0.0.1:${PORT}"
echo "-- ephemeral HelixCode facade up at ${BASE} (pid ${PID}); HELIX_LLM_PROVIDER=local"
echo "-- throwaway CLAUDE_CONFIG_DIR = ${THROWAWAY}  (operator's ~/.claude* untouched)"
echo

# settings.json for the throwaway dir. The credential is supplied via the
# environment at run time and NEVER written into this file (§11.4.10).
cat > "$THROWAWAY/settings.json" <<JSON
{
  "env": {
    "ANTHROPIC_BASE_URL": "${BASE}",
    "ANTHROPIC_MODEL": "helix-local",
    "ANTHROPIC_SMALL_FAST_MODEL": "helix-local",
    "CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC": "1"
  },
  "permissions": { "allow": [], "deny": [] }
}
JSON

runcc() { # runcc <label> <extra env assignments...>
    local label="$1"; shift
    echo "=============================================================="
    echo "RUN: $label"
    echo "=============================================================="
    ( cd "$WORKDIR" && env -u ANTHROPIC_API_KEY \
        CLAUDE_CONFIG_DIR="$THROWAWAY" \
        ANTHROPIC_BASE_URL="$BASE" \
        ANTHROPIC_AUTH_TOKEN="$EPH_KEY" \
        ANTHROPIC_MODEL="helix-local" \
        ANTHROPIC_SMALL_FAST_MODEL="helix-local" \
        CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC=1 \
        "$@" \
        timeout 180 claude -p "Reply with exactly: FACADE_OK" --output-format text ) 2>&1 \
      | sed -e "s|${EPH_KEY}|<EPHEMERAL_KEY_REDACTED>|g" | tail -40
    echo "[claude exit: $?]"
    echo
}

runcc "R1  default Claude Code -> facade"
runcc "R2  with CLAUDE_CODE_ATTRIBUTION_HEADER=0 (drops the attribution system block)" \
      CLAUDE_CODE_ATTRIBUTION_HEADER=0
runcc "R3  with CLAUDE_CODE_DISABLE_EXPERIMENTAL_BETAS=1 as well" \
      CLAUDE_CODE_ATTRIBUTION_HEADER=0 CLAUDE_CODE_DISABLE_EXPERIMENTAL_BETAS=1

echo "=============================================================="
echo "SERVER-SIDE VIEW: what the facade logged / rejected"
echo "=============================================================="
grep -iE '"/v1/messages"|/v1/messages|400|401|404|502' "$OUT/ephemeral-server.log" | tail -30
echo
echo "-- claude-code e2e complete"
