#!/usr/bin/env bash
# scripts/register_mcp.sh — manifest-driven, idempotent MCP-server registrar
# (HXC-159 T-P1.05; the second registrar §11.4.164 calls).
#
# ── Manifest schema (scripts/mcp_manifest.tsv) ─────────────────────────────
#   name<TAB>fragment<TAB>owner — one row per MCP server. name: ^[a-z0-9][a-z0-9_-]*$.
#   fragment: constitution-relative JSON path holding the exact server object
#   under .mcpServers.<name> (flat fragments without the wrapper are REFUSED).
#   owner: only `constitution` is writable here.
#
# ── Semantics ──────────────────────────────────────────────────────────────
#   Managed names (manifest rows) are (re)generated from fragments into the
#   target .mcp.json (default <root>/.mcp.json). Unmanaged third-party entries
#   (codegraph, open-design, …) are NEVER written and NEVER removed — preserved
#   byte-identically (verified by canonical-JSON comparison before/after).
#   Re-running in any manifest order yields a byte-identical target (rows are
#   applied in byte-sorted name order; output is jq -S canonical form).
#   Source roots are config-injected: $HELIX_MCP_CONSTITUTION_DIR (default
#   <root>/constitution). No hardcoded paths. No secrets in output (asserted).
#   A flock(1) lock serialises concurrent runs; writes are temp+rename.
#
# ── Usage ──────────────────────────────────────────────────────────────────
#   register_mcp.sh --root <dir> [--manifest <tsv>] [--target <mcp.json>]
#                     [--const-dir <dir>] [--check] [--probe]
#   --check: verify generated entries equal fragments (+ unmanaged preserved),
#            change nothing. --probe: also execute each generated entry's probe
#            via scripts/lib/mcp_probe_lib.sh (RS-17).
#
# Dependencies: bash, jq, python3, flock. Sandbox-safe: all writes under --root.
set -u
SELF_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck disable=SC1091
source "$SELF_DIR/lib/mcp_probe_lib.sh"

ROOT=""; MANIFEST=""; TARGET=""; CONST_DIR=""; CHECK=0; PROBE=0
while [ $# -gt 0 ]; do case "$1" in
  --root) ROOT="$2"; shift 2;;
  --manifest) MANIFEST="$2"; shift 2;;
  --target) TARGET="$2"; shift 2;;
  --const-dir) CONST_DIR="$2"; shift 2;;
  --check) CHECK=1; shift;;
  --probe) PROBE=1; shift;;
  *) echo "[register-mcp] unknown arg: $1" >&2; exit 2;;
esac done
[ -n "$ROOT" ] || { echo "[register-mcp] --root required" >&2; exit 2; }
MANIFEST="${MANIFEST:-$ROOT/scripts/mcp_manifest.tsv}"
TARGET="${TARGET:-$ROOT/.mcp.json}"
CONST_DIR="${CONST_DIR:-${HELIX_MCP_CONSTITUTION_DIR:-$ROOT/constitution}}"
LOCK="$ROOT/.claude/.register_mcp.lock"

fail() { echo "[register-mcp] ERROR: $*" >&2; }
ok() { echo "[register-mcp] $*"; }

[ -f "$MANIFEST" ] || { fail "manifest absent: $MANIFEST"; exit 1; }

# ── Validate whole manifest BEFORE touching anything ──
NAMES="$(mktemp)"; trap 'rm -f "$NAMES"' EXIT
awk -F'\t' '!/^#/ && NF {print}' "$MANIFEST" | sort > "$NAMES"
[ -s "$NAMES" ] || { fail "manifest empty: $MANIFEST"; exit 1; }
while IFS="$(printf '\t')" read -r name fragment owner; do
  case "$name" in ''|[!a-z0-9]*|*[!a-z0-9_-]*) fail "bad name: '$name'"; exit 1;; esac
  [ "$owner" = "constitution" ] || { fail "'$name': owner '$owner' not writable here"; exit 1; }
  case "$fragment" in /*) fail "'$name': absolute fragment refused"; exit 1;; esac
  [ -f "$CONST_DIR/$fragment" ] || { fail "'$name': fragment absent: $CONST_DIR/$fragment"; exit 1; }
  python3 - "$CONST_DIR/$fragment" "$name" <<'PYEOF' || exit 1
import json, sys
fpath, name = sys.argv[1], sys.argv[2]
try:
    f = json.load(open(fpath))
except Exception as e:
    print("[register-mcp] fragment %s: JSON parse error: %s" % (fpath, e)); sys.exit(1)
if not isinstance(f.get("mcpServers"), dict) or name not in f["mcpServers"]:
    print("[register-mcp] fragment %s: no .mcpServers.%s wrapper (flat refused)" % (fpath, name)); sys.exit(1)
PYEOF
done < "$NAMES"
dupes="$(cut -f1 "$NAMES" | sort | uniq -d)"; [ -z "$dupes" ] || { fail "duplicate manifest names: $dupes"; exit 1; }

# Snapshot unmanaged entries (canonical form) for preservation proof.
[ -f "$TARGET" ] || { [ "$CHECK" -eq 1 ] && { fail "target absent: $TARGET"; exit 1; }; echo '{"mcpServers":{}}' > "$TARGET"; }
UNMANAGED_BEFORE="$(mktemp)"; trap 'rm -f "$NAMES" "$UNMANAGED_BEFORE"' EXIT
python3 - "$TARGET" "$NAMES" > "$UNMANAGED_BEFORE" <<'PYEOF' || exit 1
import json, sys
t = json.load(open(sys.argv[1]))
managed = set(l.split("\t")[0] for l in open(sys.argv[2]) if l.strip())
rest = {k: v for k, v in t.get("mcpServers", {}).items() if k not in managed}
print(json.dumps(rest, sort_keys=True, ensure_ascii=False))
PYEOF

if [ "$CHECK" -eq 1 ]; then
  rc=0
  while IFS="$(printf '\t')" read -r name fragment _owner; do
    rendered="$(python3 "$SELF_DIR/lib/mcp_render.py" "$CONST_DIR/$fragment" "$name" "$ROOT" "$CONST_DIR")" || { ok "drift: $name (fragment unrenderable)"; rc=1; continue; }
    got="$(jq -S -c --arg n "$name" '.mcpServers[$n] // empty' "$TARGET" 2>/dev/null)"
    want="$(printf '%s' "$rendered" | jq -S -c '.' 2>/dev/null)"
    [ -n "$got" ] && [ "$got" = "$want" ] || { ok "drift: $name"; rc=1; }
  done < "$NAMES"
  [ "$rc" -eq 0 ] && ok "VERDICT: WIRED" || fail "VERDICT: NOT-WIRED"
  exit "$rc"
fi

command -v flock >/dev/null 2>&1 || { fail "flock(1) missing"; exit 1; }
mkdir -p "$(dirname "$LOCK")"
exec 9>"$LOCK"; flock 9 || { fail "cannot acquire lock"; exit 1; }

TMP_T="$(mktemp)"; trap 'rm -f "$NAMES" "$UNMANAGED_BEFORE" "$TMP_T"' EXIT
cp "$TARGET" "$TMP_T"
# Fragments are constitution-relative (e.g. `skills/media-validator/...`
# inside constitution/mcp/); generated entries are root-relative, so a relative
# `command` (when path-like, i.e. contains a /) and a path-like `args[0]` are
# prefixed with the constitution dir's root-relative path at merge time.
# Bare subcommands (e.g. `mcp-stdio`) and `env` values are NEVER rewritten.
# Absolute command/path-like-args[0] values are REFUSED (fail closed).
while IFS="$(printf '\t')" read -r name fragment _owner; do
  rendered="$(python3 "$SELF_DIR/lib/mcp_render.py" "$CONST_DIR/$fragment" "$name" "$ROOT" "$CONST_DIR")" || exit 1
  python3 - "$TMP_T" "$name" "$rendered" <<'PYEOF' || exit 1
import json, sys
tpath, name, rendered = sys.argv[1], sys.argv[2], sys.argv[3]
t = json.load(open(tpath))
t.setdefault("mcpServers", {})[name] = json.loads(rendered)
json.dump(t, open(tpath, "w"), indent=2, sort_keys=True, ensure_ascii=False)
open(tpath, "a").write("\n")
PYEOF
done < "$NAMES"
# Preservation proof: unmanaged entries unchanged by the merge (compares the
# canonical unmanaged set before/after — a vacuous "ok" here would be a bluff).
python3 - "$TMP_T" "$UNMANAGED_BEFORE" "$NAMES" <<'PYEOF' || exit 1
import json, sys
t = json.load(open(sys.argv[1]))
before = json.loads(open(sys.argv[2]).read())
managed = set(l.split("\t")[0] for l in open(sys.argv[3]) if l.strip())
rest = {k: v for k, v in t.get("mcpServers", {}).items() if k not in managed}
if json.dumps(rest, sort_keys=True) != json.dumps(before, sort_keys=True):
    print("[register-mcp] unmanaged entries changed by merge"); sys.exit(1)
print("[register-mcp] unmanaged preserved (%d entries)" % len(rest))
PYEOF
# Secret audit on the STAGED content before rename (fail-closed ordering: a
# secret-positive run must never land secret-bearing bytes in the target).
if grep -rniE '"(api_key|api-key|token|secret|password)"\s*:\s*"[^"]{4,}"' "$TMP_T" | grep -viE '"(disabled|description)"' >/dev/null 2>&1; then
  fail "secret-like value in generated output"; exit 1
fi
mv "$TMP_T" "$TARGET"
ok "generated $(wc -l < "$NAMES" | tr -d ' ') server(s) into $TARGET (unmanaged preserved)"

if [ "$PROBE" -eq 1 ]; then
  rc=0
  while IFS="$(printf '\t')" read -r name _frag _own; do
    mcp_probe_entry "$ROOT" "$CONST_DIR" "$TARGET" "$name" || rc=1
  done < "$NAMES"
  [ "$rc" -eq 0 ] && ok "PROBES: all generated servers answer" || { fail "PROBES: a generated server did not answer"; exit 1; }
fi
ok "done"
