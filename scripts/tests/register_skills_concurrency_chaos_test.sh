#!/usr/bin/env bash
# register_skills_concurrency_chaos_test.sh — concurrency + chaos tests for
# scripts/register_skills.sh (HXC-159 T-P1.04.6 / T-P1.04.7).
#
# Concurrency: two simultaneous invocations against one sandbox must serialise
# on the flock and leave a consistent tree (--check exits 0).
# Chaos: SIGKILL mid-registration must leave only reaped temp residue; the
# next run recovers to a consistent tree (--check exits 0).
# Sandbox-only (mktemp): NEVER touches the live .claude/skills/.
set -euo pipefail

ROOT="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd)"
REGISTRAR="$ROOT/scripts/register_skills.sh"

TMP="$(mktemp -d)"
cleanup() { rm -rf "$TMP"; }
trap cleanup EXIT

SKILLS="action-prefix-system media-validator multitrack reporting-workable-items scheduled-work-queue session-sync skill-catalog workable-item-lifecycle"

stage() { # $1 = sandbox dir
  for s in $SKILLS; do
    mkdir -p "$1/constitution/skills/$s"
    printf '# %s fixture\n' "$s" > "$1/constitution/skills/$s/SKILL.md"
  done
  {
    for s in $SKILLS; do printf '%s\tconstitution/skills/%s\tconstitution\n' "$s" "$s"; done
  } > "$1/manifest.tsv"
}

fail=0

# ── Concurrency: 2 simultaneous runs, same sandbox ───────────────────────────
C="$TMP/concurrent"; mkdir -p "$C"; stage "$C"
"$REGISTRAR" --root "$C" --manifest "$C/manifest.tsv" >"$TMP/c1.log" 2>&1 &
p1=$!
"$REGISTRAR" --root "$C" --manifest "$C/manifest.tsv" >"$TMP/c2.log" 2>&1 &
p2=$!
rc1=0; wait "$p1" || rc1=$?; rc2=0; wait "$p2" || rc2=$?
if [ "$rc1" -eq 0 ] && [ "$rc2" -eq 0 ] && "$REGISTRAR" --root "$C" --manifest "$C/manifest.tsv" --check >/dev/null 2>&1; then
  echo "ASSERT-OK:   2 simultaneous runs serialised to a consistent tree (rc=$rc1,$rc2, --check 0)"
else
  echo "ASSERT-FAIL: concurrent runs inconsistent (rc=$rc1,$rc2)"; fail=1
fi

# ── Chaos: SIGKILL mid-registration, then recover ────────────────────────────
# Part 1 is a best-effort real kill (timing-dependent by nature — the run takes
# milliseconds, so the signal may land mid-run or just after; EITHER landing
# must converge on the next run). Part 2 deterministically simulates a killed
# run's leftovers (stale temp residue + a half-written entry: marker present,
# link missing) and proves the next run reaps + converges.
K="$TMP/chaos"; mkdir -p "$K"; stage "$K"
"$REGISTRAR" --root "$K" --manifest "$K/manifest.tsv" >/dev/null 2>&1 &
kp=$!
kill -9 "$kp" 2>/dev/null || true
wait "$kp" 2>/dev/null || true
# Simulate the worst case a kill can leave: stale temp residue + half-write.
ln -s "../nowhere" "$K/.claude/skills/.tmp.register.99999.media-validator" 2>/dev/null || true
rm -f "$K/.claude/skills/session-sync"
if "$REGISTRAR" --root "$K" --manifest "$K/manifest.tsv" >/dev/null 2>&1 \
  && "$REGISTRAR" --root "$K" --manifest "$K/manifest.tsv" --check >/dev/null 2>&1; then
  residue="$(find "$K/.claude/skills" -name '.tmp.register.*' 2>/dev/null | wc -l)"
  if [ "$residue" -eq 0 ]; then
    echo "ASSERT-OK:   post-SIGKILL run recovered to a consistent tree, no temp residue"
  else
    echo "ASSERT-FAIL: temp residue remains after recovery"; fail=1
  fi
else
  echo "ASSERT-FAIL: post-SIGKILL recovery run did not reach a consistent tree"; fail=1
fi

exit "$fail"
