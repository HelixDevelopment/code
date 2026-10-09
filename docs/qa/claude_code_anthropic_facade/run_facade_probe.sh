#!/usr/bin/env bash
# Ephemeral, key-configured HelixCode instance -> Anthropic Messages facade probe.
# NEVER touches the live :8080 unit. Only signals the PID this script launched
# (§11.4.174). Secrets are supplied from the operator .env at run time and are
# never echoed (§11.4.10).
set -uo pipefail

ROOT=/home/milosvasic/Projects/helix_code
OUT="$ROOT/docs/qa/claude_code_anthropic_facade"
BIN="$ROOT/helix_code/bin/helixcode"
SRC_CFG="$ROOT/helix_code/config/config.yaml"   # same template the live :8080 unit loads

set -a
# shellcheck disable=SC1091
. "$ROOT/.env" >/dev/null 2>&1
set +a

PORT="$(python3 -c 'import socket;s=socket.socket();s.bind(("127.0.0.1",0));print(s.getsockname()[1]);s.close()')"
EPH_KEY="eph-$(head -c 16 /dev/urandom | od -An -tx1 | tr -d ' \n')"
CFG="$OUT/ephemeral-config.yaml"
LOG="$OUT/ephemeral-server.log"

# Rewrite ONLY server.port/address (bind loopback so the ephemeral instance is
# never externally reachable) and disable redis: no redis is listening on this
# host right now, and the facade path under test does not use it.
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
    HELIX_WIRE_FACADE_API_KEYS="$EPH_KEY" "$BIN" server ) > "$LOG" 2>&1 &
PID=$!
trap 'kill "$PID" 2>/dev/null; sleep 1; kill -9 "$PID" 2>/dev/null' EXIT

for _ in $(seq 1 45); do
    kill -0 "$PID" 2>/dev/null || { echo "SERVER EXITED DURING BOOT; tail:"; tail -5 "$LOG"; exit 1; }
    curl -sS -m 2 -o /dev/null "http://127.0.0.1:${PORT}/health" 2>/dev/null && break
    sleep 1
done
echo "-- ephemeral server healthy on 127.0.0.1:${PORT} (pid ${PID}), facade key configured"
echo

probe() { # <label> <method> <path> <extra curl args...>
    local label="$1" method="$2" path="$3"; shift 3
    echo "### $label"
    echo "\$ curl -sS -i -X $method http://127.0.0.1:PORT$path  [args elided; key via \$EPH_KEY]"
    curl -sS -i -m 90 -X "$method" "http://127.0.0.1:${PORT}${path}" "$@" \
      | sed -e "s|${EPH_KEY}|<EPHEMERAL_KEY_REDACTED>|g" \
      | grep -vE '^(Access-Control|Strict-Transport|X-Content-Type|X-Frame|X-Xss|Date:)'
    echo; echo
}

CJ='content-type: application/json'

probe "T1  POST /v1/messages  model=claude-sonnet-4-20250514  (what Claude Code would send)" \
  POST /v1/messages -H "$CJ" -H "x-api-key: ${EPH_KEY}" \
  -d '{"model":"claude-sonnet-4-20250514","max_tokens":64,"system":"You are terse.","messages":[{"role":"user","content":"Reply with exactly: FACADE_OK"}]}'

probe "T2  POST /v1/messages  model=totally-made-up-model-xyz  (arbitrary id)" \
  POST /v1/messages -H "$CJ" -H "x-api-key: ${EPH_KEY}" \
  -d '{"model":"totally-made-up-model-xyz","max_tokens":32,"messages":[{"role":"user","content":"Say OK"}]}'

probe "T3  POST /v1/messages  Authorization: Bearer form (ANTHROPIC_AUTH_TOKEN mapping)" \
  POST /v1/messages -H "$CJ" -H "Authorization: Bearer ${EPH_KEY}" \
  -d '{"model":"claude-opus-4-1","max_tokens":32,"messages":[{"role":"user","content":"Say OK"}]}'

probe "T4  POST /v1/messages/count_tokens  (Claude Code may call this)" \
  POST /v1/messages/count_tokens -H "$CJ" -H "x-api-key: ${EPH_KEY}" \
  -d '{"model":"claude-sonnet-4-20250514","messages":[{"role":"user","content":"hi"}]}'

probe "T5  GET /v1/models?limit=1000  (gateway model discovery)" \
  GET "/v1/models?limit=1000" -H "x-api-key: ${EPH_KEY}"

probe "T6  POST /v1/messages  stream:true  (Claude Code always streams)" \
  POST /v1/messages -H "$CJ" -H "x-api-key: ${EPH_KEY}" \
  -d '{"model":"claude-sonnet-4-20250514","max_tokens":48,"stream":true,"messages":[{"role":"user","content":"Count: 1 2 3"}]}'

probe "T7  POST /v1/messages  wrong key on a key-configured instance (negative control)" \
  POST /v1/messages -H "$CJ" -H "x-api-key: definitely-the-wrong-key" \
  -d '{"model":"claude-sonnet-4-20250514","max_tokens":16,"messages":[{"role":"user","content":"hi"}]}'

probe "T8  POST /v1/messages  tools[] present (Claude Code sends its tool schemas)" \
  POST /v1/messages -H "$CJ" -H "x-api-key: ${EPH_KEY}" \
  -d '{"model":"claude-sonnet-4-20250514","max_tokens":128,"tools":[{"name":"get_weather","description":"Get weather","input_schema":{"type":"object","properties":{"city":{"type":"string"}},"required":["city"]}}],"messages":[{"role":"user","content":"What is the weather in Paris? Use the tool."}]}'

echo "-- probes complete; stopping ephemeral pid ${PID}"
