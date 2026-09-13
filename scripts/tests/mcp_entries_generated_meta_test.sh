#!/usr/bin/env bash
# mcp_entries_generated_meta_test.sh — paired §1.1 mutations for
# CM-MCP-ENTRIES-GENERATED (HXC-159 T-P1.05.4). Each mutation MUST flip the
# gate; repairs MUST restore PASS. Sandbox-only (mktemp + production-layout
# symlink mirror, mirroring register_mcp_red_test.sh).
set -euo pipefail
ROOT="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd)"
GATE="$ROOT/scripts/gates/mcp_entries_generated_gate.sh"
MANIFEST="$ROOT/scripts/mcp_manifest.tsv"
REGISTRAR="$ROOT/scripts/register_mcp.sh"
TMP="$(mktemp -d)"; trap 'rm -rf "$TMP"' EXIT
SB="$TMP/sb"; mkdir -p "$SB"; ln -s "$ROOT/constitution" "$SB/constitution"
printf '{"mcpServers":{}}' > "$SB/.mcp.json"
"$REGISTRAR" --root "$SB" --manifest "$MANIFEST" --target "$SB/.mcp.json" >/dev/null
fail=0
assert_pass() { "$GATE" --root "$SB" --manifest "$MANIFEST" --target "$SB/.mcp.json" >/dev/null 2>&1 && echo "ASSERT-OK:   gate PASSED as expected ($1)" || { echo "ASSERT-FAIL: gate passed expected but failed ($1)"; fail=1; }; }
assert_fail() { "$GATE" --root "$SB" --manifest "$MANIFEST" --target "$SB/.mcp.json" >/dev/null 2>&1 && { echo "ASSERT-FAIL: gate failed expected but passed ($1)"; fail=1; } || echo "ASSERT-OK:   gate FAILED as expected ($1)"; }

assert_pass "clean generation"
# Mutation 1: hand-edit a generated path.
cp "$SB/.mcp.json" "$TMP/clean.json"
python3 - "$SB/.mcp.json" <<'PYEOF'
import json, sys
p = sys.argv[1]; d = json.load(open(p))
d["mcpServers"]["scheduled-work"]["args"] = ["HAND-EDITED"]
json.dump(d, open(p, "w"), indent=2, sort_keys=True)
PYEOF
assert_fail "hand-edited generated path"
cp "$TMP/clean.json" "$SB/.mcp.json"
assert_pass "hand-edit reverted"
# Mutation 2: delete a managed entry.
python3 - "$SB/.mcp.json" <<'PYEOF'
import json, sys
p = sys.argv[1]; d = json.load(open(p))
del d["mcpServers"]["media-validator"]
json.dump(d, open(p, "w"), indent=2, sort_keys=True)
PYEOF
assert_fail "managed entry deleted"
cp "$TMP/clean.json" "$SB/.mcp.json"
assert_pass "entry restored"
# Mutation 3: unmanaged third-party entry added — gate must STAY PASS.
python3 - "$SB/.mcp.json" <<'PYEOF'
import json, sys
p = sys.argv[1]; d = json.load(open(p))
d["mcpServers"]["third-party"] = {"command": "tp", "args": []}
json.dump(d, open(p, "w"), indent=2, sort_keys=True)
PYEOF
assert_pass "unmanaged entry preserved"
exit "$fail"
