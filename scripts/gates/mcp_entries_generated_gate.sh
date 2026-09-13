#!/usr/bin/env bash
# CM-MCP-ENTRIES-GENERATED (HXC-159 T-P1.05.4): every manifest-managed MCP name
# in the live .mcp.json must equal its fragment (canonical JSON), and every
# managed name must be present. Hand-editing a generated path MUST FAIL.
# RED: hand-edit one generated path -> gate MUST FAIL (exit != 0).
# Read-only: never writes. --root/--manifest/--target/--const-dir overridable.
set -u
ROOT="$(git rev-parse --show-toplevel 2>/dev/null || pwd)"
MANIFEST=""; TARGET=""; CONST_DIR=""
while [ $# -gt 0 ]; do case "$1" in
  --root) ROOT="$2"; shift 2;;
  --manifest) MANIFEST="$2"; shift 2;;
  --target) TARGET="$2"; shift 2;;
  --const-dir) CONST_DIR="$2"; shift 2;;
  *) echo "unknown arg: $1" >&2; exit 2;;
esac done
MANIFEST="${MANIFEST:-$ROOT/scripts/mcp_manifest.tsv}"
TARGET="${TARGET:-$ROOT/.mcp.json}"
CONST_DIR="${CONST_DIR:-${HELIX_MCP_CONSTITUTION_DIR:-$ROOT/constitution}}"
SELF_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
LIB="$SELF_DIR/../lib"
fail=0; checked=0
while IFS="$(printf '\t')" read -r name fragment _owner; do
  case "$name" in \#*|"") continue;; esac
  checked=$((checked + 1))
  rendered="$(python3 "$LIB/mcp_render.py" "$CONST_DIR/$fragment" "$name" "$ROOT" "$CONST_DIR" 2>/dev/null)" \
    && got="$(jq -S -c --arg n "$name" '.mcpServers[$n] // empty' "$TARGET" 2>/dev/null)" \
    && want="$(printf '%s' "$rendered" | jq -S -c '.' 2>/dev/null)" \
    && [ -n "$got" ] && [ "$got" = "$want" ] \
    || { echo "FAIL drift: $name"; fail=1; }
done < <(awk -F'\t' '!/^#/ && NF' "$MANIFEST")
echo "checked=$checked fail=$fail"
[ "$fail" -eq 0 ] && echo "CM-MCP-ENTRIES-GENERATED PASS" || echo "CM-MCP-ENTRIES-GENERATED FAIL"
exit "$fail"
