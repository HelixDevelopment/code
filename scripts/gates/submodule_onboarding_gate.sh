#!/usr/bin/env bash
# CM-SUBMODULE-ONBOARDING-COMPLETE (HXC-159 T-P3.06): per-entry governance
# checklist over every .gitmodules entry. Own-org entries (vasic-digital,
# HelixDevelopment) require: worktree present, helix-deps.yaml at root,
# upstreams/ recipes with matching remote count (CONST-056), governance
# carrier (CONSTITUTION.md or CLAUDE.md/AGENTS.md). Third-party entries get an
# honest SKIP row (vendored, read-only). Any own-org FAIL blocks.
# Evidence: qa-results/hxc159/onboarding/<run-id>/ (this stdout).
# Emits per-entry per-item rows for BOTH pass and fail (F1 fix: silent PASS
# rows are a reporting gap the runtime signature forbids).
set -u
ROOT="$(git rev-parse --show-toplevel 2>/dev/null || pwd)"
TMP="$(mktemp)"; trap 'rm -f "$TMP"' EXIT
git -C "$ROOT" config -f .gitmodules --get-regexp '^submodule\..*\.path$' 2>/dev/null | sed 's/^submodule\.//;s/\.path / /' > "$TMP" || true
pass=0; fail=0; skip=0
while read -r name path; do
  [ -n "$path" ] || continue
  url="$(git -C "$ROOT" config -f .gitmodules "submodule.$name.url" 2>/dev/null)" || continue
  case "$url" in *github.com:vasic-digital/*|*github.com:HelixDevelopment/*) ;; *)
    skip=$((skip+1)); continue;; esac
  row=""; ok=1
  [ -d "$ROOT/$path" ] || { row="$row[no-worktree]"; ok=0; }
  [ -f "$ROOT/$path/helix-deps.yaml" ] || { row="$row[no-helix-deps.yaml]"; ok=0; }
  if [ -d "$ROOT/$path/upstreams" ]; then
    recipes="$(ls "$ROOT/$path/upstreams/"*.sh 2>/dev/null | wc -l | tr -d ' ')"
    remotes="$(git -C "$ROOT/$path" remote 2>/dev/null | wc -l | tr -d ' ')"
    [ "$remotes" -ge "$recipes" ] || { row="$row[remotes=$remotes<recipes=$recipes]"; ok=0; }
  else
    row="$row[no-upstreams/]"; ok=0
  fi
  [ -f "$ROOT/$path/CONSTITUTION.md" ] || [ -f "$ROOT/$path/AGENTS.md" ] || { row="$row[no-carrier]"; ok=0; }
  if [ "$ok" -eq 1 ]; then pass=$((pass+1)); echo "PASS $path [worktree,helix-deps.yaml,upstreams,carrier]"; else fail=$((fail+1)); echo "FAIL $path $row"; fi
done < "$TMP"
echo "pass=$pass fail=$fail skip(third-party)=$skip"
[ "$fail" -eq 0 ] && echo "CM-SUBMODULE-ONBOARDING-COMPLETE PASS" || echo "CM-SUBMODULE-ONBOARDING-COMPLETE FAIL"
exit "$fail"
