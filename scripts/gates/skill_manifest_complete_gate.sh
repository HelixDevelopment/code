#!/usr/bin/env bash
# CM-SKILL-MANIFEST-COMPLETE (HXC-159 T-P1.01.5 / FR-008 / RS-04): every registered
# constitution skill source directory MUST contain an EXACT-CASE `SKILL.md`.
#
# Two populations are checked, and both must be complete:
#   1. every row of scripts/skills_manifest.tsv (the registrar's input) — its
#      declared source directory must hold `SKILL.md` (exact case);
#   2. every directory under constitution/skills/ — a directory that carries a
#      `register.sh` (wired, not dead — §11.4.124) but no exact-case `SKILL.md` is
#      the R-05 defect class and FAILs; a directory holding a lowercase/mixed-case
#      variant (`skill.md`, `Skill.md`) FAILs even on a case-insensitive filesystem,
#      because the probe is `ls -1 | grep -x 'SKILL.md'`, never a `-f` test that a
#      case-folding FS could satisfy by accident (T-P1.01 runtime signature).
#
# Read-only: this gate never writes. Exit 0 = PASS, 1 = FAIL, 2 = usage/precondition.
# Paired §1.1 mutation: scripts/tests/skill_manifest_complete_meta_test.sh (delete a
# SKILL.md -> FAIL; lowercase it -> FAIL; register.sh-without-SKILL.md -> FAIL;
# restore -> PASS).
#
# Authored during the HXC-159 T-P9.01 independent review: the gate was NAMED by
# T-P1.01.5 / plan.md §7 (R-05) but never implemented (§11.4.227(A)).
set -u
ARG="${1:-}"
ROOT="$(git rev-parse --show-toplevel 2>/dev/null || pwd)"
case "$ARG" in
  --root) ROOT="${2:?--root requires a directory}" ;;
  "") ;;
  *) ROOT="$ARG" ;;
esac
MANIFEST="$ROOT/scripts/skills_manifest.tsv"
SKILLS_ROOT="$ROOT/constitution/skills"
fail=0; checked=0

[ -f "$MANIFEST" ] || { echo "FAIL manifest missing: $MANIFEST"; exit 2; }
[ -d "$SKILLS_ROOT" ] || { echo "FAIL constitution skills root missing: $SKILLS_ROOT"; exit 2; }

# exact_case_manifest DIR -> 0 iff DIR holds a file literally named SKILL.md.
exact_case_manifest() {
  ls -1 -- "$1" 2>/dev/null | grep -qx 'SKILL.md'
}

# Population 1: manifest rows.
lineno=0
while IFS= read -r line || [ -n "$line" ]; do
  lineno=$((lineno + 1))
  case "$line" in ''|\#*) continue ;; esac
  name="$(printf '%s' "$line" | cut -f1)"
  source="$(printf '%s' "$line" | cut -f2)"
  checked=$((checked + 1))
  dir="$ROOT/$source"
  if [ ! -d "$dir" ]; then
    echo "FAIL manifest line $lineno ('$name'): source dir missing: $source"; fail=1; continue
  fi
  if ! exact_case_manifest "$dir"; then
    echo "FAIL manifest line $lineno ('$name'): no exact-case SKILL.md in $source"; fail=1
  fi
done < "$MANIFEST"

# Population 2: every constitution skill directory.
for dir in "$SKILLS_ROOT"/*/; do
  [ -d "$dir" ] || continue
  base="$(basename "$dir")"
  checked=$((checked + 1))
  if ! exact_case_manifest "$dir"; then
    if [ -e "$dir/register.sh" ]; then
      echo "FAIL constitution/skills/$base: register.sh present but no exact-case SKILL.md (wired-not-dead, R-05)"; fail=1
    else
      variant="$(ls -1 -- "$dir" | grep -ix 'skill.md' | head -1)"
      if [ -n "$variant" ]; then
        echo "FAIL constitution/skills/$base: manifest is '$variant', not exact-case 'SKILL.md' (case split, R-05)"; fail=1
      else
        echo "FAIL constitution/skills/$base: no SKILL.md at all"; fail=1
      fi
    fi
  fi
done

echo "checked=$checked fail=$fail"
[ "$fail" -eq 0 ] && echo "CM-SKILL-MANIFEST-COMPLETE PASS" || echo "CM-SKILL-MANIFEST-COMPLETE FAIL"
exit "$fail"
