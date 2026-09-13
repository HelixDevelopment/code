#!/usr/bin/env bash
# CM-SKILL-NAMESPACE-DISJOINT (HXC-159 T-P1.04.8): the .claude/skills/ tree must
# contain no silent-shadowing collision — every manifest-owned name present as a
# real directory/file (not a symlink), every manifest-named symlink pointing
# anywhere but its manifest-declared source, and every speckit-* row in the
# constitution manifest is a FAIL. Read-only: this gate never writes.
# Boundary (§11.4.201 false-positive guard): other-named real directories are
# NOT failures here — native Claude Code skills install as real dirs, and the
# registrar (not this gate) refuses foreign entries at install time.
# RED: plant a real dir named like a manifest skill, repoint a skill link, or
# add a speckit-* manifest row -> gate MUST FAIL.
set -u
ARG="${1:-}"
ROOT="$(git rev-parse --show-toplevel 2>/dev/null || pwd)"
case "$ARG" in
  --root) ROOT="${2:?--root requires a directory}" ;;
  "") ;;
  *) ROOT="$ARG" ;;
esac
MANIFEST="$ROOT/scripts/skills_manifest.tsv"
DST="$ROOT/.claude/skills"
fail=0; checked=0

[ -f "$MANIFEST" ] || { echo "FAIL manifest missing: $MANIFEST"; exit 1; }

names="$(mktemp)"; trap 'rm -f "$names"' EXIT
lineno=0
while IFS= read -r line || [ -n "$line" ]; do
  lineno=$((lineno + 1))
  case "$line" in ''|\#*) continue ;; esac
  name="$(printf '%s' "$line" | cut -f1)"
  source="$(printf '%s' "$line" | cut -f2)"
  owner="$(printf '%s' "$line" | cut -f3)"
  case "$name" in
    speckit-*) echo "FAIL manifest line $lineno: SpecKit-reserved prefix '$name'"; fail=1 ;;
  esac
  [ "$owner" = "constitution" ] || { echo "FAIL manifest line $lineno: foreign owner '$owner'"; fail=1; }
  printf '%s\t%s\n' "$name" "$source" >> "$names"
done < "$MANIFEST"
dupes="$(cut -f1 "$names" | sort | uniq -d)"
[ -z "$dupes" ] || { echo "FAIL duplicate manifest name(s): $dupes"; fail=1; }

owned() { grep -q "$(printf '^%s\t' "$1")" "$names"; }
owned_source() { grep "$(printf '^%s\t' "$1")" "$names" | cut -f2; }

if [ -d "$DST" ]; then
  for entry in "$DST"/* "$DST"/.*; do
    [ -e "$entry" ] || [ -L "$entry" ] || continue
    base="$(basename "$entry")"
    case "$base" in .|..|.manifest|.register.lock|.tmp.register.*) continue ;; esac
    checked=$((checked + 1))
    case "$base" in
      speckit-*) continue ;; # other registrar's reservation — disjoint by construction
    esac
    if owned "$base"; then
      [ -L "$entry" ] || { echo "FAIL collision: '$base' is not a symlink (would shadow the managed skill)"; fail=1; continue; }
      want="$ROOT/$(owned_source "$base")"
      got="$(cd -- "$DST" >/dev/null 2>&1 && cd -- "$(dirname "$(readlink "$entry")")" >/dev/null 2>&1 && pwd)/$(basename "$(readlink "$entry")")" || got="UNRESOLVABLE"
      [ "$got" = "$want" ] || { echo "FAIL mispointed: '$base' -> '$got' (manifest: '$want')"; fail=1; }
    fi
  done
fi

echo "checked=$checked fail=$fail"
[ "$fail" -eq 0 ] && echo "CM-SKILL-NAMESPACE-DISJOINT PASS" || echo "CM-SKILL-NAMESPACE-DISJOINT FAIL"
exit "$fail"
