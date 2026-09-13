#!/usr/bin/env bash
# CM-WORKABLE-ITEMS-SSOT-UNFORKED (HXC-159 T-P1.07.3): no forbidden name may be
# registered in any registrar manifest. RED: re-register taskstoissues
# unmodified -> gate MUST FAIL.
set -u
ROOT="$(git rev-parse --show-toplevel 2>/dev/null || pwd)"
DENY="$ROOT/scripts/forbidden_registrations.tsv"
fail=0; checked=0
while IFS="$(printf '\t')" read -r name _reason _target; do
  case "$name" in \#*|"") continue;; esac
  checked=$((checked + 1))
  for m in "$ROOT/scripts/skills_manifest.tsv" "$ROOT/scripts/mcp_manifest.tsv"; do
    [ -f "$m" ] || continue
    if awk -F'\t' -v n="$name" '!/^#/ && NF && $1==n {found=1} END{exit !found}' "$m"; then
      echo "FAIL fork: '$name' registered in $m"; fail=1
    fi
  done
done < "$DENY"
echo "checked=$checked fail=$fail"
[ "$fail" -eq 0 ] && echo "CM-WORKABLE-ITEMS-SSOT-UNFORKED PASS" || echo "CM-WORKABLE-ITEMS-SSOT-UNFORKED FAIL"
exit "$fail"
