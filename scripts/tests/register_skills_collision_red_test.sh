#!/usr/bin/env bash
# register_skills_collision_red_test.sh — RED test (§11.4.115 / §11.4.224) for
# HXC-159 T-P1.04 (scripts/register_skills.sh with install-time collision detection).
#
# Defect: two independent installers provisioning into one `.claude/skills/` tree
# collide in the filesystem and Claude Code resolves the collision SILENTLY
# (no error, no warning; upstream namespacing closed as not-planned). No
# install-time detector exists, so a planted foreign entry is never refused.
#
# Polarity switch (§11.4.115): RED_MODE=1 (default) reproduces the defect on the
# current artifact and exits 0 while the defect is PRESENT (exit 1 once fixed —
# a RED test passing on a fixed artifact is a blind test). RED_MODE=0 is the
# standing regression guard (§11.4.135): the registrar MUST refuse a planted
# foreign collision with non-zero exit having changed nothing.
#
# Sandbox-only: operates on a mktemp fakeproj via --root/--manifest overrides.
# NEVER touches the live .claude/skills/.
set -u

RED_MODE="${RED_MODE:-1}"
ROOT="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd)"
REGISTRAR="$ROOT/scripts/register_skills.sh"

TMP="$(mktemp -d)"
cleanup() { rm -rf "$TMP"; }
trap cleanup EXIT

SANDBOX="$TMP/fakeproj"
mkdir -p "$SANDBOX/.claude/skills/media-validator"
printf '# foreign skill body (not ours)\n' > "$SANDBOX/.claude/skills/media-validator/SKILL.md"
mkdir -p "$SANDBOX/constitution/skills/media-validator"
printf '# ours\n' > "$SANDBOX/constitution/skills/media-validator/SKILL.md"
printf 'media-validator\tconstitution/skills/media-validator\tconstitution\n' > "$TMP/manifest.tsv"
BEFORE="$(cd "$SANDBOX" && find .claude/skills -mindepth 1 | sort)"

if [ "$RED_MODE" = "1" ]; then
  # RED: reproduce silent resolution on the CURRENT artifact.
  if [ ! -x "$REGISTRAR" ]; then
    echo "RED reproduced: $REGISTRAR absent — no install-time detector exists;"
    echo "a planted foreign '.claude/skills/media-validator' entry would resolve silently."
    exit 0
  fi
  if "$REGISTRAR" --root "$SANDBOX" --manifest "$TMP/manifest.tsv" >/dev/null 2>&1; then
    echo "RED reproduced: registrar exited 0 over a planted foreign collision (silent resolution)."
    exit 0
  fi
  AFTER="$(cd "$SANDBOX" && find .claude/skills -mindepth 1 | sort)"
  if [ "$BEFORE" = "$AFTER" ]; then
    echo "RED not reproduced: registrar refused the collision and changed nothing (fixed)."
    exit 1
  fi
  echo "RED reproduced: registrar exited non-zero BUT mutated the tree."
  exit 0
else
  # GREEN guard: registrar MUST refuse the planted collision, changing nothing.
  if [ ! -x "$REGISTRAR" ]; then
    echo "GUARD FAIL: registrar absent — collision would resolve silently."
    exit 1
  fi
  if "$REGISTRAR" --root "$SANDBOX" --manifest "$TMP/manifest.tsv" >/dev/null 2>&1; then
    echo "GUARD FAIL: registrar exited 0 over a planted foreign collision."
    exit 1
  fi
  AFTER="$(cd "$SANDBOX" && find .claude/skills -mindepth 1 | sort)"
  if [ "$BEFORE" != "$AFTER" ]; then
    echo "GUARD FAIL: registrar refused but mutated the tree."
    exit 1
  fi
  echo "GUARD PASS: registrar refused the planted collision and changed nothing."
fi

# F1 regression (2026-09-13): manifest-shrink removes the stale own pair
# instead of aborting. Runs only in guard mode (registrar present).
if [ "${RED_MODE:-1}" = "0" ] && [ -x "$REGISTRAR" ]; then
  SHRINK="$TMP/shrink"; rm -rf "$SHRINK"; mkdir -p "$SHRINK"
  for s in action-prefix-system media-validator multitrack reporting-workable-items scheduled-work-queue session-sync skill-catalog workable-item-lifecycle; do
    mkdir -p "$SHRINK/constitution/skills/$s"; printf '# %s fixture\n' "$s" > "$SHRINK/constitution/skills/$s/SKILL.md"
    printf '%s\tconstitution/skills/%s\tconstitution\n' "$s" "$s"
  done > "$SHRINK/manifest.tsv"
  "$REGISTRAR" --root "$SHRINK" --manifest "$SHRINK/manifest.tsv" >/dev/null 2>&1
  grep -v "session-sync" "$SHRINK/manifest.tsv" > "$SHRINK/manifest.shrunk.tsv"
  "$REGISTRAR" --root "$SHRINK" --manifest "$SHRINK/manifest.shrunk.tsv" >/dev/null 2>&1
  rc=$?
  if [ "$rc" -ne 0 ]; then echo "STALE-FAIL: shrink run exited $rc"; exit 1; fi
  if [ -L "$SHRINK/.claude/skills/session-sync" ] || [ -f "$SHRINK/.claude/skills/.manifest/session-sync" ]; then
    echo "STALE-FAIL: stale pair not reaped"; exit 1
  fi
  echo "STALE-OK: manifest-shrink reaped the stale own pair (F1 fix proven)"
fi
