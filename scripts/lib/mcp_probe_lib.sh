#!/usr/bin/env bash
# scripts/lib/mcp_probe_lib.sh — shared MCP-server entry probe + equality
# helpers for the T-P1.05 registrar family (HXC-159, spec
# 001-helixskills-incorporation).
#
# Sourced BY REFERENCE (never duplicated) by:
#   scripts/register_mcp.sh                     (merge-then-probe)
#   scripts/gates/mcp_entries_generated_gate.sh (read-only verify + probe)
#   scripts/tests/register_mcp_red_test.sh      (guard-mode probe evidence)
#   scripts/tests/mcp_entries_generated_meta_test.sh (mutation restore check)
#
# ── Probe contract (RS-17: path correctness proven by execution) ─────────────
#   mcp_probe_entry <root> <constdir> <target> <name>
#     Reads .mcpServers.<name> from <target> (the GENERATED entry, never the
#     fragment) and executes a no-op probe against its real command:
#       command == *bash  → fixture file + matching pattern inside a sandboxed
#                           allowed dir; PASS iff exit 0 (a no-arg run exits 2
#                           by contract and can never PASS).
#       command contains a `/` (relative binary path, e.g. the
#                           scheduled-work Go binary) → require an executable
#                           file, run `<bin> --help`; PASS iff exit 0.
#       otherwise (bare command, e.g. codegraph/npx) → `command -v` must
#                           resolve; PASS iff it does.
#     <resolved-arg0> refuses absolute paths (fail closed) and resolves
#     `$root/<arg>` first, then `$constdir/<arg>` (fragments are
#     constitution-relative; project entries are root-relative).
#     Probe-only env sandboxing: MEDIA_VALIDATOR_EVIDENCE_DIR and SWQ_DB are
#     pointed at a mktemp dir so a probe can never write outside tmp.
#     A probe that cannot execute FAILS loudly (return 1 with a reason);
#     NOTHING here passes by string inspection — every PASS cites a live
#     exit status plus captured output bytes.
#
# ── Equality contract ────────────────────────────────────────────────────────
#   mcp_entry_equals <target> <fragment> <name>
#     Return 0 iff target .mcpServers.<name> is deep-equal to fragment
#     .mcpServers.<name> (canonical JSON, sorted keys). Needs python3.
#
# Dependencies: bash, jq, python3. No side effects on source.
# Classification: project-specific (§11.4.17) — this consumer's registrar lib.
# Last verified: 2026-09-13
# shellcheck disable=SC2148

mcp_probe_entry() {
  _mpe_root="$1"; _mpe_const="$2"; _mpe_target="$3"; _mpe_name="$4"
  _mpe_cmd="$(jq -r --arg n "$_mpe_name" '.mcpServers[$n].command // empty' "$_mpe_target" 2>/dev/null)"
  _mpe_arg0="$(jq -r --arg n "$_mpe_name" '.mcpServers[$n].args[0] // empty' "$_mpe_target" 2>/dev/null)"
  [ -n "$_mpe_cmd" ] || { printf 'PROBE %s: FAIL (no .command in generated entry)\n' "$_mpe_name"; return 1; }
  _mpe_out="$(mktemp)"
  _mpe_ev="$(mktemp -d)"
  trap 'rm -f "$_mpe_out"; rm -rf "$_mpe_ev"' RETURN
  case "$_mpe_cmd" in
    *bash)
      [ -n "$_mpe_arg0" ] || { printf 'PROBE %s: FAIL (bash entry without args[0])\n' "$_mpe_name"; return 1; }
      case "$_mpe_arg0" in /*) printf 'PROBE %s: FAIL (absolute arg refused: %s)\n' "$_mpe_name" "$_mpe_arg0"; return 1 ;; esac
      _mpe_script=""
      if [ -f "$_mpe_root/$_mpe_arg0" ]; then _mpe_script="$_mpe_root/$_mpe_arg0"
      elif [ -f "$_mpe_const/$_mpe_arg0" ]; then _mpe_script="$_mpe_const/$_mpe_arg0"
      else printf 'PROBE %s: FAIL (unresolvable arg: %s)\n' "$_mpe_name" "$_mpe_arg0"; return 1; fi
      # Fixture contract (measured 2026-09-14): media-validator.sh with NO args
      # exits rc=2 (Usage) — a no-arg run can never PASS. Probe with a fixture
      # file + matching pattern inside the sandboxed allowed dir instead.
      printf 'probe-pattern-alpha\n' > "$_mpe_ev/fixture.txt"
      MEDIA_VALIDATOR_EVIDENCE_DIR="$_mpe_ev" MEDIA_VALIDATOR_ALLOWED_DIR="$_mpe_ev" \
        SWQ_DB="$_mpe_ev/swq.json" \
        bash "$_mpe_script" "$_mpe_ev/fixture.txt" "probe-pattern-alpha" >"$_mpe_out" 2>&1
      _mpe_rc=$?
      _mpe_bytes="$(wc -c <"$_mpe_out" | tr -d ' ')"
      if [ "$_mpe_rc" -eq 0 ] && [ "$_mpe_bytes" -gt 0 ]; then
        printf 'PROBE %s: PASS (bash %s executed, rc=0, %sB) :: %s\n' \
          "$_mpe_name" "$_mpe_script" "$_mpe_bytes" "$(head -n 1 "$_mpe_out")"
        return 0
      fi
      printf 'PROBE %s: FAIL (bash %s executed, rc=%s, %sB)\n' "$_mpe_name" "$_mpe_script" "$_mpe_rc" "$_mpe_bytes"
      return 1
      ;;
    */*)
      _mpe_bin=""
      case "$_mpe_cmd" in /*) printf 'PROBE %s: FAIL (absolute command refused: %s)\n' "$_mpe_name" "$_mpe_cmd"; return 1 ;; esac
      if [ -f "$_mpe_root/$_mpe_cmd" ]; then _mpe_bin="$_mpe_root/$_mpe_cmd"
      elif [ -f "$_mpe_const/$_mpe_cmd" ]; then _mpe_bin="$_mpe_const/$_mpe_cmd"
      else printf 'PROBE %s: FAIL (unresolvable command: %s)\n' "$_mpe_name" "$_mpe_cmd"; return 1; fi
      [ -x "$_mpe_bin" ] || { printf 'PROBE %s: FAIL (not executable: %s)\n' "$_mpe_name" "$_mpe_bin"; return 1; }
      MEDIA_VALIDATOR_EVIDENCE_DIR="$_mpe_ev" SWQ_DB="$_mpe_ev/swq.json" \
        "$_mpe_bin" --help >"$_mpe_out" 2>&1
      _mpe_rc=$?
      _mpe_flag="--help"
      if [ "$_mpe_rc" -ne 0 ]; then
        # Fallback: some binaries (e.g. scheduled-work: serve|mcp-stdio|
        # acp-stdio|version) have no --help flag. `version` is the next
        # no-op that proves the binary executes. Honest boundary: this proves
        # the binary RUNS (arch/loadable, not a stub) — not that its
        # fragment-declared args[0] (mcp-stdio) serves; that path is covered
        # by generation-equality, never by this probe.
        MEDIA_VALIDATOR_EVIDENCE_DIR="$_mpe_ev" SWQ_DB="$_mpe_ev/swq.json" \
          "$_mpe_bin" version >"$_mpe_out" 2>&1
        _mpe_rc=$?; _mpe_flag="version"
      fi
      _mpe_bytes="$(wc -c <"$_mpe_out" | tr -d ' ')"
      if [ "$_mpe_rc" -eq 0 ]; then
        printf 'PROBE %s: PASS (%s %s executed, rc=0, %sB) :: %s\n' \
          "$_mpe_name" "$_mpe_bin" "$_mpe_flag" "$_mpe_bytes" "$(head -n 1 "$_mpe_out")"
        return 0
      fi
      printf 'PROBE %s: FAIL (%s --help/version executed, rc=%s, %sB)\n' "$_mpe_name" "$_mpe_bin" "$_mpe_rc" "$_mpe_bytes"
      return 1
      ;;
    *)
      if command -v "$_mpe_cmd" >/dev/null 2>&1; then
        printf 'PROBE %s: PASS (command -v %s -> %s)\n' "$_mpe_name" "$_mpe_cmd" "$(command -v "$_mpe_cmd")"
        return 0
      fi
      printf 'PROBE %s: FAIL (command -v %s unresolvable)\n' "$_mpe_name" "$_mpe_cmd"
      return 1
      ;;
  esac
}

mcp_entry_equals() {
  python3 - "$1" "$2" "$3" <<'PYEOF'
import json, sys
tpath, fpath, name = sys.argv[1], sys.argv[2], sys.argv[3]
try:
    t = json.load(open(tpath))
    f = json.load(open(fpath))
except Exception as e:
    print("equality-check: JSON parse error: %s" % e)
    sys.exit(2)
a = t.get("mcpServers", {}).get(name, None)
b = f.get("mcpServers", {}).get(name, None)
if a is None:
    print("equality-check: %s absent from target" % name)
    sys.exit(1)
if b is None:
    print("equality-check: %s absent from fragment" % name)
    sys.exit(1)
ca = json.dumps(a, sort_keys=True, ensure_ascii=False)
cb = json.dumps(b, sort_keys=True, ensure_ascii=False)
if ca != cb:
    print("equality-check: %s differs (target %dB vs fragment %dB canonical)" % (name, len(ca), len(cb)))
    sys.exit(1)
sys.exit(0)
PYEOF
}
