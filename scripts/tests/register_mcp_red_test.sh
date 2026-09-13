#!/usr/bin/env bash
# register_mcp_red_test.sh — RED/GUARD for scripts/register_mcp.sh (HXC-159 T-P1.05.1).
# RED_MODE=1: reproduce the defect (registrar absent -> §11.4.164 hook contract
# broken: constitution/mcp/*.json fragments exist but nothing generates entries
# from a manifest). RED_MODE=0: guard (generation + drift detection + probes).
# Sandbox-only (mktemp): NEVER touches the live .mcp.json.
set -euo pipefail
ROOT="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd)"
REGISTRAR="$ROOT/scripts/register_mcp.sh"
MANIFEST="$ROOT/scripts/mcp_manifest.tsv"
TMP="$(mktemp -d)"; trap 'rm -rf "$TMP"' EXIT

if [ "${RED_MODE:-1}" = "1" ]; then
  if [ ! -x "$REGISTRAR" ]; then
    echo "RED reproduced: scripts/register_mcp.sh absent — fragments exist but no manifest-driven generator (hook contract broken)."
    exit 0
  fi
  echo "RED not reproduced: registrar present (fixed)."
  exit 1
fi

# ── GUARD MODE ──
# Sandbox mirrors production layout: $SB/constitution symlinks the real tree
# so the default --const-dir (<root>/constitution) resolves exactly as in
# production (relpath prefix = "constitution"). The registrar only WRITES
# $SB/.mcp.json + $SB/.claude/ lock — never through the symlink.
[ -x "$REGISTRAR" ] || { echo "GUARD FAIL: registrar absent."; exit 1; }
SB="$TMP/sb"; mkdir -p "$SB"
ln -s "$ROOT/constitution" "$SB/constitution"
printf '{"mcpServers":{"third-party":{"command":"tp","args":[]}}}' > "$SB/.mcp.json"
REG_ARGS=(--root "$SB" --manifest "$MANIFEST" --target "$SB/.mcp.json")
# 1. Generate into sandbox target.
"$REGISTRAR" "${REG_ARGS[@]}" >/dev/null
[ "$(jq -r '.mcpServers["media-validator"].command' "$SB/.mcp.json")" = "bash" ] || { echo "GUARD FAIL: media-validator not generated."; exit 1; }
[ "$(jq -r '.mcpServers["scheduled-work"].command' "$SB/.mcp.json")" = "constitution/scripts/scheduled-work-engine/bin/scheduled-work" ] || { echo "GUARD FAIL: scheduled-work not generated."; exit 1; }
[ "$(jq -r '.mcpServers["scheduled-work"].args[0]' "$SB/.mcp.json")" = "mcp-stdio" ] || { echo "GUARD FAIL: bare subcommand rewritten."; exit 1; }
[ "$(jq -r '.mcpServers["third-party"].command' "$SB/.mcp.json")" = "tp" ] || { echo "GUARD FAIL: unmanaged third-party entry not preserved."; exit 1; }
echo "GUARD PASS: generation + unmanaged preservation."
# 2. Determinism: re-run byte-identical.
sha1="$(sha256sum "$SB/.mcp.json" | cut -d' ' -f1)"
"$REGISTRAR" "${REG_ARGS[@]}" >/dev/null
[ "$(sha256sum "$SB/.mcp.json" | cut -d' ' -f1)" = "$sha1" ] || { echo "GUARD FAIL: re-run not byte-identical."; exit 1; }
echo "GUARD PASS: re-run byte-identical."
# 3. Drift detection: --check passes clean, fails on hand-edit, passes on restore.
"$REGISTRAR" "${REG_ARGS[@]}" --check >/dev/null \
  || { echo "GUARD FAIL: --check failed on clean tree."; exit 1; }
cp "$SB/.mcp.json" "$TMP/clean.json"
python3 - "$SB/.mcp.json" <<'PYEOF'
import json, sys
p = sys.argv[1]
d = json.load(open(p))
d["mcpServers"]["media-validator"]["args"] = ["HAND-EDITED/stale.sh"]
json.dump(d, open(p, "w"), indent=2, sort_keys=True)
PYEOF
"$REGISTRAR" "${REG_ARGS[@]}" --check >/dev/null 2>&1 \
  && { echo "GUARD FAIL: --check passed over a hand-edited path."; exit 1; }
cp "$TMP/clean.json" "$SB/.mcp.json"
"$REGISTRAR" "${REG_ARGS[@]}" --check >/dev/null \
  || { echo "GUARD FAIL: --check failed after restore."; exit 1; }
echo "GUARD PASS: drift detected and restored."
# 4. Probes execute (RS-17) against the sandbox-generated entries.
"$REGISTRAR" "${REG_ARGS[@]}" --probe 2>&1 | tail -3
echo "GUARD PASS: registrar present, generation + preservation + drift proven (probes above)."
exit 0
