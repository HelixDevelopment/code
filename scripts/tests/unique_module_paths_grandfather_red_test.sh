#!/usr/bin/env bash
# unique_module_paths_grandfather_red_test.sh — RED test (§11.4.115 / §11.4.224)
# for HXC-159 T-P9.01 finding F-12.
#
# Defect (pre-fix): scripts/gates/unique_module_paths_gate.sh has NO
# grandfather/allowlist mechanism at all — it FAILs on every live duplicate
# module path unconditionally, including the 15 pre-existing
# submodules/claude-toolkit/** duplicate groups this repo does not own and
# was explicitly told not to restructure (F-12's own scope note). The gate's
# own header even says it was "Deliberately NOT wired into any pre-build
# sweep yet: it currently FAILS (by design ...)". Meanwhile
# specs/001-helixskills-incorporation/progress.yml and earlier task records
# (T-P0.03) claim this gate PASSES — a claim that is currently FALSE on the
# live tree (T-P9.01 F-12).
#
# Polarity switch (§11.4.115): RED_MODE=1 (default) reproduces the defect —
# proves the CURRENT gate has no way to distinguish a grandfathered
# pre-existing duplicate from a genuinely NEW one, so it cannot be wired into
# a standard sweep without either (a) permanently blocking on debt this repo
# does not own, or (b) someone disabling the whole gate. Exits 0 while the
# defect is PRESENT. RED_MODE=0 is the standing regression guard (§11.4.135):
# after the fix, (i) every KNOWN submodules/claude-toolkit/** duplicate group
# must be grandfathered (gate exits 0, mentioning them as grandfathered, NOT
# silently dropped from output), and (ii) a genuinely NEW duplicate (a
# fixture module path collision OUTSIDE the grandfather list) must still FAIL
# the gate — the ratchet must never mask a fresh regression.
set -u

RED_MODE="${RED_MODE:-1}"
ROOT="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd)"
GATE="$ROOT/scripts/gates/unique_module_paths_gate.sh"
GRANDFATHER="$ROOT/scripts/gates/unique_module_paths_grandfathered.tsv"

if [ "$RED_MODE" = "1" ]; then
  if [ ! -x "$GATE" ]; then
    echo "RED reproduced: $GATE absent."
    exit 0
  fi
  if [ -f "$GRANDFATHER" ] && grep -q "digital.vasic.challenges" "$GRANDFATHER" 2>/dev/null; then
    echo "RED not reproduced: a grandfather list already exempts the known claude-toolkit duplicates (fixed)."
    exit 1
  fi
  # Reproduce: the live tree's known pre-existing duplicates make the
  # unmodified gate FAIL, with no mechanism to distinguish "known, not ours"
  # from "new regression".
  if bash "$GATE" >/tmp/f12_red.out 2>&1; then
    echo "RED not reproduced: gate currently exits 0 on the live tree (fixed, or dupes cleared)."
    exit 1
  fi
  if grep -q "digital.vasic.challenges" /tmp/f12_red.out; then
    echo "RED reproduced: gate FAILs unconditionally on the known submodules/claude-toolkit/** duplicates"
    echo "with no grandfather mechanism to distinguish them from a new regression:"
    sed -n '1,3p' /tmp/f12_red.out | sed 's/^/    /'
    exit 0
  fi
  echo "RED not reproduced: gate FAILs but not on the expected known-duplicate set (investigate)."
  exit 1
else
  # GREEN guard.
  fail=0
  if [ ! -x "$GATE" ] || [ ! -f "$GRANDFATHER" ]; then
    echo "GUARD FAIL: gate or grandfather list missing."
    exit 1
  fi

  # (i) The live tree, with its known pre-existing claude-toolkit duplicates,
  # MUST now pass (grandfathered), and the grandfathered set MUST be named
  # in the output — never silently dropped.
  if ! out="$(bash "$GATE" 2>&1)"; then
    echo "GUARD FAIL: gate still FAILs on the live tree after grandfathering:"
    echo "$out" | head -20 | sed 's/^/    /'
    fail=1
  elif ! printf '%s\n' "$out" | grep -qi "grandfathered"; then
    echo "GUARD FAIL: gate PASSed but did not name the grandfathered duplicate set (silent, not honest):"
    echo "$out" | head -10 | sed 's/^/    /'
    fail=1
  else
    echo "GUARD PASS (i): live tree passes with known claude-toolkit duplicates honestly grandfathered."
  fi

  # (ii) Non-regression: a genuinely NEW duplicate (outside the grandfather
  # list) must still FAIL the gate. The gate resolves REPO_ROOT from its OWN
  # script location (never cwd), so it always scans the real tree — a
  # sandboxed copy cannot be scanned in isolation. Instead, plant a transient
  # fixture pair under a LIVE (non-excluded per is_live()) path inside the
  # real tree, run the real gate against it, then remove the fixture
  # unconditionally (trap) before this test exits, so nothing is left behind
  # and nothing is ever staged/committed.
  FIX_A="$ROOT/scripts/testing/.f12_fixture_new_dup_a"
  FIX_B="$ROOT/scripts/testing/.f12_fixture_new_dup_b"
  cleanup_fixture() { rm -rf "$FIX_A" "$FIX_B"; }
  trap cleanup_fixture EXIT
  mkdir -p "$FIX_A" "$FIX_B"
  printf 'module f12.fixture.new.duplicate\n\ngo 1.21\n' > "$FIX_A/go.mod"
  printf 'module f12.fixture.new.duplicate\n\ngo 1.21\n' > "$FIX_B/go.mod"
  if bash "$GATE" >/tmp/f12_sandbox.out 2>&1; then
    echo "GUARD FAIL: a genuinely NEW duplicate (outside the grandfather list) was NOT caught — ratchet is masking regressions:"
    cat /tmp/f12_sandbox.out | sed 's/^/    /'
    fail=1
  elif ! grep -q "f12.fixture.new.duplicate" /tmp/f12_sandbox.out; then
    echo "GUARD FAIL: gate FAILed but not on the planted new-duplicate fixture (investigate):"
    cat /tmp/f12_sandbox.out | sed 's/^/    /'
    fail=1
  else
    echo "GUARD PASS (ii): a genuinely new, non-grandfathered duplicate still FAILs the gate."
  fi
  cleanup_fixture
  trap - EXIT

  [ "$fail" -eq 0 ] && echo "VERDICT: WIRED" || echo "VERDICT: NOT-WIRED"
  exit "$fail"
fi
