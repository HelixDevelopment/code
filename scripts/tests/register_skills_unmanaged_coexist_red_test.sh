#!/usr/bin/env bash
# register_skills_unmanaged_coexist_red_test.sh — RED test (§11.4.115 / §11.4.224)
# for HXC-159 T-P9.01 finding F-04.
#
# Defect: register_skills.sh's find_foreign() treats ANY name present in
# .claude/skills/ that is NOT in this registrar's manifest as "foreign" and
# aborts the whole run — even though a name we never intend to write can
# never collide with anything we are trying to manage. Per T-P1.04.3 other
# tools/prefixes (e.g. SpecKit's `speckit-*`) own their own reserved names;
# T-P1.04.4 as literally implemented contradicts that by refusing to run
# whenever such a name is merely PRESENT. On THIS repo's real checkout this
# means the registrar can never complete: SpecKit's own `speckit-analyze`
# skill link is always present, and the §11.4.272 activation engine leaves
# pre-existing, correctly-resolved-but-unmarked links for names that ARE in
# our manifest (action-prefix-system, skill-catalog, workable-item-lifecycle)
# — neither case is a real collision with anything the registrar itself
# writes, yet both currently abort the run.
#
# Polarity switch (§11.4.115): RED_MODE=1 (default) reproduces the defect on
# the current artifact and exits 0 while the defect is PRESENT (exits 1 once
# fixed — a RED test that still passes on the fixed artifact is a blind
# test). RED_MODE=0 is the standing regression guard (§11.4.135): the
# registrar MUST coexist peacefully with (a) a name never in its manifest
# and (b) a pre-existing, unmarked, correctly-resolved symlink for a name
# that IS in its manifest — completing successfully and touching neither.
#
# A genuine collision (a manifest-owned name already occupied by something
# that does NOT resolve to our intended target) MUST still be refused — that
# case is covered by register_skills_collision_red_test.sh and is
# re-asserted here as a non-regression check in GUARD mode.
#
# Sandbox-only: operates on mktemp fakeproj trees via --root/--manifest.
# NEVER touches the live .claude/skills/.
set -u

RED_MODE="${RED_MODE:-1}"
ROOT="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd)"
REGISTRAR="$ROOT/scripts/register_skills.sh"

TMP="$(mktemp -d)"
cleanup() { rm -rf "$TMP"; }
trap cleanup EXIT

fail=0

# ── Case A: a name never in our manifest (SpecKit-style foreign skill) ──────
A="$TMP/case_a"
mkdir -p "$A/.claude/skills/speckit-analyze"
printf '# not ours — owned by SpecKit\n' > "$A/.claude/skills/speckit-analyze/SKILL.md"
mkdir -p "$A/constitution/skills/skill-catalog"
printf '# ours\n' > "$A/constitution/skills/skill-catalog/SKILL.md"
printf 'skill-catalog\tconstitution/skills/skill-catalog\tconstitution\n' > "$A/manifest.tsv"

# ── Case B: a manifest-owned name already correctly symlinked but with no
#            marker (mirrors the §11.4.272 activation engine's pre-existing
#            links on the live checkout). ─────────────────────────────────
B="$TMP/case_b"
mkdir -p "$B/constitution/skills/action-prefix-system"
printf '# ours\n' > "$B/constitution/skills/action-prefix-system/SKILL.md"
printf 'action-prefix-system\tconstitution/skills/action-prefix-system\tconstitution\n' > "$B/manifest.tsv"
mkdir -p "$B/.claude/skills"
( cd "$B/.claude/skills" && ln -s ../../constitution/skills/action-prefix-system action-prefix-system )
# No .manifest/action-prefix-system marker written — exactly the live state.

if [ "$RED_MODE" = "1" ]; then
  if [ ! -x "$REGISTRAR" ]; then
    echo "RED reproduced: $REGISTRAR absent — coexistence cannot be exercised."
    exit 0
  fi

  a_ok=1
  if "$REGISTRAR" --root "$A" --manifest "$A/manifest.tsv" >"$TMP/a.out" 2>&1; then
    a_ok=0
  fi
  if [ "$a_ok" -eq 1 ]; then
    echo "RED reproduced (case A): registrar aborted over a name never in its own manifest:"
    sed 's/^/    /' "$TMP/a.out"
    exit 0
  fi

  b_ok=1
  if "$REGISTRAR" --root "$B" --manifest "$B/manifest.tsv" >"$TMP/b.out" 2>&1; then
    b_ok=0
  fi
  if [ "$b_ok" -eq 1 ]; then
    echo "RED reproduced (case B): registrar aborted over its OWN pre-existing,"
    echo "correctly-resolved-but-unmarked link instead of adopting it:"
    sed 's/^/    /' "$TMP/b.out"
    exit 0
  fi

  echo "RED not reproduced: registrar coexisted with both unmanaged and pre-existing"
  echo "correctly-resolved names without aborting (fixed)."
  exit 1
else
  if [ ! -x "$REGISTRAR" ]; then
    echo "GUARD FAIL: registrar absent."
    fail=1
  else
    # Case A: must succeed and must NOT touch the foreign speckit-analyze entry.
    before_a="$(cd "$A" && find .claude/skills -mindepth 1 | sort)"
    if ! "$REGISTRAR" --root "$A" --manifest "$A/manifest.tsv" >"$TMP/a.out" 2>&1; then
      echo "GUARD FAIL (case A): registrar refused to coexist with an unmanaged name:"
      sed 's/^/    /' "$TMP/a.out"; fail=1
    else
      if [ ! -e "$A/.claude/skills/speckit-analyze/SKILL.md" ]; then
        echo "GUARD FAIL (case A): foreign speckit-analyze entry was touched/removed."; fail=1
      elif [ ! -L "$A/.claude/skills/skill-catalog" ]; then
        echo "GUARD FAIL (case A): managed name skill-catalog was not linked."; fail=1
      else
        echo "GUARD PASS (case A): coexisted with unmanaged name, linked the managed one."
      fi
    fi

    # Case B: must succeed, adopt the pre-existing correct link (write the
    # marker) and leave its target unchanged.
    before_target="$(readlink "$B/.claude/skills/action-prefix-system")"
    if ! "$REGISTRAR" --root "$B" --manifest "$B/manifest.tsv" >"$TMP/b.out" 2>&1; then
      echo "GUARD FAIL (case B): registrar refused its own pre-existing correct link:"
      sed 's/^/    /' "$TMP/b.out"; fail=1
    else
      after_target="$(readlink "$B/.claude/skills/action-prefix-system")"
      if [ "$before_target" != "$after_target" ]; then
        echo "GUARD FAIL (case B): pre-existing link target changed ($before_target -> $after_target)."; fail=1
      elif [ ! -f "$B/.claude/skills/.manifest/action-prefix-system" ]; then
        echo "GUARD FAIL (case B): pre-existing link was not adopted (no marker written)."; fail=1
      else
        echo "GUARD PASS (case B): adopted pre-existing correctly-resolved unmarked link."
      fi
    fi
  fi

  # Non-regression: a genuine collision (manifest-owned name already occupied
  # by something that does NOT resolve to our target) MUST still be refused.
  C="$TMP/case_c"
  mkdir -p "$C/.claude/skills/media-validator"
  printf '# foreign content, wrong target entirely\n' > "$C/.claude/skills/media-validator/SKILL.md"
  mkdir -p "$C/constitution/skills/media-validator"
  printf '# ours\n' > "$C/constitution/skills/media-validator/SKILL.md"
  printf 'media-validator\tconstitution/skills/media-validator\tconstitution\n' > "$C/manifest.tsv"
  before_c="$(cd "$C" && find .claude/skills -mindepth 1 | sort)"
  if [ -x "$REGISTRAR" ] && "$REGISTRAR" --root "$C" --manifest "$C/manifest.tsv" >/dev/null 2>&1; then
    echo "GUARD FAIL (case C non-regression): genuine collision on a manifest-owned name was NOT refused."
    fail=1
  else
    after_c="$(cd "$C" && find .claude/skills -mindepth 1 | sort)"
    if [ "$before_c" != "$after_c" ]; then
      echo "GUARD FAIL (case C non-regression): refused but mutated the tree."
      fail=1
    else
      echo "GUARD PASS (case C non-regression): genuine collision still refused, tree unchanged."
    fi
  fi

  [ "$fail" -eq 0 ] && echo "VERDICT: WIRED" || echo "VERDICT: NOT-WIRED"
  exit "$fail"
fi
