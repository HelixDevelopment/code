#!/usr/bin/env bash
# CM-NO-ABSOLUTE-OR-BROKEN-SYMLINKS (HXC-159 T-P1.03.4): every tracked symlink
# must resolve (readlink -e) and must not point at an absolute target.
# RED: recreate the /Volumes/T7 link or a dangling link -> gate MUST FAIL.
set -u
ROOT="$(git rev-parse --show-toplevel 2>/dev/null || pwd)"
fail=0; total=0
while read -r mode _ _ path; do
  [[ "$mode" == "120000" ]] || continue
  total=$((total+1))
  target="$(git -C "$ROOT" cat-file -p ":$path" 2>/dev/null)"
  if [[ "$target" == /* ]]; then
    echo "FAIL absolute: $path -> $target"; fail=1; continue
  fi
  if [[ ! -e "$ROOT/$path" ]]; then
    echo "FAIL missing-in-worktree: $path -> $target"; fail=1; continue
  fi
  if [[ -z "$(readlink -e "$ROOT/$path" 2>/dev/null)" ]]; then
    echo "FAIL dangling: $path -> $target"; fail=1; continue
  fi
done < <(git -C "$ROOT" ls-files -s)
echo "checked=$total fail=$fail"
[[ "$fail" == "0" ]] && echo "CM-NO-ABSOLUTE-OR-BROKEN-SYMLINKS PASS" || echo "CM-NO-ABSOLUTE-OR-BROKEN-SYMLINKS FAIL"
exit "$fail"
