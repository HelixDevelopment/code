#!/usr/bin/env bash
# register_skills_determinism_test.sh — install-order determinism + idempotence
# test for scripts/register_skills.sh (HXC-159 T-P1.04.5 / T-P1.04.8).
#
# Asserts: (a) re-running the registrar on the same sandbox is byte-identical;
# (b) running with manifest rows in REVERSE order into a fresh sandbox yields
# a byte-identical tree (order-independent regeneration, §11.4.50 / §11.4.77).
# Snapshot = sorted (path, readlink-target, marker-content) listing + sha256.
# Sandbox-only (mktemp): NEVER touches the live .claude/skills/.
set -euo pipefail

ROOT="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd)"
REGISTRAR="$ROOT/scripts/register_skills.sh"

TMP="$(mktemp -d)"
cleanup() { rm -rf "$TMP"; }
trap cleanup EXIT

SKILLS="action-prefix-system media-validator multitrack reporting-workable-items scheduled-work-queue session-sync skill-catalog workable-item-lifecycle"

stage() { # $1 = sandbox dir; stages fixture sources for all 8 skills
  for s in $SKILLS; do
    mkdir -p "$1/constitution/skills/$s"
    printf '# %s fixture\n' "$s" > "$1/constitution/skills/$s/SKILL.md"
  done
  {
    for s in $SKILLS; do printf '%s\tconstitution/skills/%s\tconstitution\n' "$s" "$s"; done
  } > "$1/manifest.tsv"
}

snapshot() { # $1 = sandbox dir; deterministic tree fingerprint to stdout
  ( cd "$1/.claude/skills" && find . -mindepth 1 | sort | while IFS= read -r p; do
      if [ -L "$p" ]; then printf 'L %s -> %s\n' "$p" "$(readlink "$p")"
      elif [ -f "$p" ]; then printf 'F %s [%s]\n' "$p" "$(cat "$p")"
      else printf '? %s\n' "$p"; fi
    done )
}

rev_lines() { if command -v tac >/dev/null 2>&1; then tac "$1"; else tail -r "$1" 2>/dev/null || awk '{a[NR]=$0} END{for(i=NR;i>0;i--) print a[i]}' "$1"; fi; }
fp_hash() { if command -v sha256sum >/dev/null 2>&1; then sha256sum < "$1" | cut -d' ' -f1; else shasum -a 256 < "$1" | cut -d' ' -f1; fi; }
A="$TMP/a"; B="$TMP/b"; mkdir -p "$A" "$B"
stage "$A"; stage "$B"
rev_lines "$A/manifest.tsv" > "$B/manifest.tsv" # reverse install order

"$REGISTRAR" --root "$A" --manifest "$A/manifest.tsv" >/dev/null
snapshot "$A" > "$TMP/snap_a1.txt"
"$REGISTRAR" --root "$A" --manifest "$A/manifest.tsv" >/dev/null
snapshot "$A" > "$TMP/snap_a2.txt"

"$REGISTRAR" --root "$B" --manifest "$B/manifest.tsv" >/dev/null
snapshot "$B" > "$TMP/snap_b1.txt"

fail=0
if cmp -s "$TMP/snap_a1.txt" "$TMP/snap_a2.txt"; then
  echo "ASSERT-OK:   re-run is byte-identical (idempotent)"
else
  echo "ASSERT-FAIL: re-run changed the tree"; diff "$TMP/snap_a1.txt" "$TMP/snap_a2.txt" || true; fail=1
fi
if cmp -s "$TMP/snap_a1.txt" "$TMP/snap_b1.txt"; then
  echo "ASSERT-OK:   reversed manifest order is byte-identical (order-independent)"
else
  echo "ASSERT-FAIL: install order changed the tree"; diff "$TMP/snap_a1.txt" "$TMP/snap_b1.txt" || true; fail=1
fi
echo "--- snapshot (sha256 $(fp_hash "$TMP/snap_a1.txt")) ---"
cat "$TMP/snap_a1.txt"
exit "$fail"
