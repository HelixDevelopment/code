#!/usr/bin/env bash
# CM-UNIQUE-MODULE-PATHS — no two LIVE Go modules may declare the same module path.
#
# Authored in T-P0.03.5. Wired into the pre-build sweep as of HXC-159 F-12
# (T-P9.01) via a GRANDFATHERED-ALLOWLIST (scripts/gates/unique_module_paths_grandfathered.tsv),
# mirroring the RS-13 CM-NO-NESTED-OWN-ORG-CHAIN precedent
# (specs/001-helixskills-incorporation/tasks.md:509-512): every duplicate this
# gate finds is checked against that list; a module path whose EVERY declaring
# file is grandfathered is reported informationally and does NOT fail the
# gate; a module path with ANY non-grandfathered declarer still FAILs. The
# grandfather list shrinks only (§11.4.135 ratchet) — a NEW duplicate anywhere
# in the tree is never covered by it and always fails.
#
#   scripts/gates/unique_module_paths_gate.sh          # gate
#   scripts/gates/unique_module_paths_gate.sh --list   # show the live module set
#
# WHY THE SCOPING IS NOT OPTIONAL (measured, not assumed):
# a naive "collect every `module` line and fail on duplicates" gate fails on this
# repo for reasons that are not defects — archived source snapshots under docs/,
# per-run QA harness copies that legitimately reuse a module path, third-party
# vendored fixtures, and generated test-result trees. A gate that cries wolf gets
# disabled in its first week, so it scopes to LIVE modules: those reachable from
# a build, excluding documentation, captured evidence, test output and vendored
# third-party trees.
#
# Exit 0 = no duplicate live module paths. Exit 1 = duplicates found.
set -uo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$REPO_ROOT" || exit 1

# Non-live trees. Each entry is justified; do not extend without one.
#   docs/            — archived source snapshots + captured QA harnesses
#   qa-results/      — captured evidence (§11.4.83)
#   test-results/    — generated challenge output
#   cli_agents/      — third-party reference CLI agents (vendored, not ours)
#   cli_agents_resources/ — third-party reference resources (vendored)
#   node_modules/ vendor/ .git/ — tool-mandated / vendored
is_live() {
  case "$1" in
    docs/*|*/docs/*) return 1 ;;
    qa-results/*|*/qa-results/*) return 1 ;;
    test-results/*|*/test-results/*) return 1 ;;
    cli_agents/*|cli_agents_resources/*) return 1 ;;
    */node_modules/*|*/vendor/*|*/.git/*) return 1 ;;
  esac
  return 0
}

declare -a live_paths=() live_files=()
while IFS= read -r f; do
  rel="${f#./}"
  is_live "$rel" || continue
  m="$(grep -m1 '^module ' "$f" 2>/dev/null | awk '{print $2}')"
  [[ -n "$m" ]] || continue
  live_paths+=("$m"); live_files+=("$rel")
done < <(find . -name go.mod -not -path '*/.git/*' 2>/dev/null | sort)

if [[ "${1:-}" == "--list" ]]; then
  printf '%-46s %s\n' "MODULE PATH" "go.mod"
  for i in "${!live_paths[@]}"; do printf '%-46s %s\n' "${live_paths[$i]}" "${live_files[$i]}"; done
  echo "(${#live_paths[@]} live modules)"
  exit 0
fi

dupes="$(printf '%s\n' "${live_paths[@]}" | sort | uniq -d)"

if [[ -z "$dupes" ]]; then
  echo "PASS CM-UNIQUE-MODULE-PATHS — ${#live_paths[@]} live modules, all paths distinct"
  exit 0
fi

# Split the duplicate set into GRANDFATHERED (every declarer of this module
# path is a pre-existing, allowlisted collision — see the header) and NEW
# (at least one declarer is not covered by the allowlist). Only NEW dupes
# fail the gate; grandfathered ones are reported informationally so the
# allowlist can never silently hide the class.
GRANDFATHER="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)/unique_module_paths_grandfathered.tsv"
is_grandfathered_path() {
  local p="$1"
  [[ -f "$GRANDFATHER" ]] || return 1
  awk -F'\t' -v p="$p" '!/^#/ && NF && $1==p {found=1} END{exit !found}' "$GRANDFATHER"
}

declare -a new_dupes=() grandfathered_dupes=()
while IFS= read -r d; do
  [[ -n "$d" ]] || continue
  if is_grandfathered_path "$d"; then
    grandfathered_dupes+=("$d")
  else
    new_dupes+=("$d")
  fi
done <<< "$dupes"

if [[ "${#grandfathered_dupes[@]}" -gt 0 ]]; then
  echo "INFO CM-UNIQUE-MODULE-PATHS — ${#grandfathered_dupes[@]} duplicate module path(s) grandfathered per scripts/gates/unique_module_paths_grandfathered.tsv (HXC-159 F-12; not caused by this repo, tracked, never silently expanded):"
  for d in "${grandfathered_dupes[@]}"; do
    echo "  '$d' (grandfathered) declared by:"
    for i in "${!live_paths[@]}"; do
      [[ "${live_paths[$i]}" == "$d" ]] && echo "    - ${live_files[$i]}"
    done
  done
fi

if [[ "${#new_dupes[@]}" -eq 0 ]]; then
  echo "PASS CM-UNIQUE-MODULE-PATHS — ${#live_paths[@]} live modules; ${#grandfathered_dupes[@]} pre-existing duplicate(s) grandfathered, zero NEW duplicates"
  exit 0
fi

echo "FAIL CM-UNIQUE-MODULE-PATHS — ${#new_dupes[@]} NEW (non-grandfathered) duplicate module path(s) among ${#live_paths[@]} live modules:"
for d in "${new_dupes[@]}"; do
  echo "  '$d' declared by:"
  for i in "${!live_paths[@]}"; do
    [[ "${live_paths[$i]}" == "$d" ]] && echo "    - ${live_files[$i]}"
  done
  # Name the concrete hazard: import paths that resolve to different packages.
  for i in "${!live_paths[@]}"; do
    [[ "${live_paths[$i]}" == "$d" ]] || continue
    base="$(dirname "${live_files[$i]}")"; [[ "$base" == "." ]] && base=""
    for sub in $(find "${base:-.}/internal" -maxdepth 1 -mindepth 1 -type d 2>/dev/null); do
      echo "      colliding import candidate: $d/internal/$(basename "$sub")  <- ${base:-<root>}"
    done
  done
done
echo
echo "Fix per spec.md D-7: rename the thin root module; bind nothing new to it."
echo "If this NEW duplicate is a genuine pre-existing own-org double-vendor (mirroring"
echo "the known submodules/claude-toolkit/** class), add it to"
echo "scripts/gates/unique_module_paths_grandfathered.tsv with a cited reason instead"
echo "of silently ignoring this failure."
exit 1
