#!/usr/bin/env bash
# CM-NO-NESTED-OWN-ORG-CHAIN (HXC-159 T-P3.05.5): ratchet against nested
# owned-org chains. Any MATERIALIZED nested owned-org checkout NOT listed in
# nested_chain_grandfathered.tsv FAILs. Grandfathered rows shrink only
# (disabling a chain removes its row). Disabled-but-present paths
# (update=none + unmaterialized, e.g. submodules/skills/constitution) pass.
# RED: initialise submodules/skills/constitution -> gate MUST FAIL.
set -u
ROOT="$(git rev-parse --show-toplevel 2>/dev/null || pwd)"
GF="$ROOT/scripts/gates/nested_chain_grandfathered.tsv"
TMP="$(mktemp)"; trap 'rm -f "$TMP"' EXIT
git -C "$ROOT" config -f .gitmodules --get-regexp '^submodule\..*\.path$' 2>/dev/null | sed 's/^submodule\.//;s/\.path / /' > "$TMP" || true
fail=0; checked=0
while read -r name path; do
  [ -n "$path" ] || continue
  url="$(git -C "$ROOT" config -f .gitmodules "submodule.$name.url" 2>/dev/null)" || continue
  case "$url" in *github.com:vasic-digital/*|*github.com:HelixDevelopment/*) ;; *) continue;; esac
  [ -f "$ROOT/$path/.gitmodules" ] || continue
  while read -r k2 p2; do
    [ -n "$p2" ] || continue
    n2="$k2"
    u2="$(git -C "$ROOT" config -f "$ROOT/$path/.gitmodules" "submodule.$n2.url" 2>/dev/null)" || continue
    case "$u2" in *github.com:vasic-digital/*|*github.com:HelixDevelopment/*) ;; *) continue;; esac
    checked=$((checked + 1))
    if [ -d "$ROOT/$path/$p2/.git" ] || [ -f "$ROOT/$path/$p2/.git" ]; then
      if awk -F'\t' -v k="$path/$p2" '$1==k{found=1} END{exit !found}' "$GF" 2>/dev/null; then
        echo "GRANDFATHERED (materialized, tracked): $path/$p2"
      else
        echo "FAIL new nested chain materialized: $path/$p2"; fail=1
      fi
    else
      echo "OK disabled/absent: $path/$p2"
    fi
  done < <(git -C "$ROOT" config -f "$ROOT/$path/.gitmodules" --get-regexp '^submodule\..*\.path$' 2>/dev/null | sed 's/^submodule\.//;s/\.path /:/' | awk -F: '{print $1" "$2}')
done < "$TMP"
echo "checked=$checked fail=$fail"
[ "$fail" -eq 0 ] && echo "CM-NO-NESTED-OWN-ORG-CHAIN PASS" || echo "CM-NO-NESTED-OWN-ORG-CHAIN FAIL"
exit "$fail"
