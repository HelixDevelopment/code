#!/bin/sh
# HXC-159 F-17 residual remediation (2026-09-23): per-skill (partial)
# admission cross-implementation parity check.
#
# Builds BOTH independently-maintained similarity-gate implementations —
# submodules/skills/cmd/similarity-probe (the canonical pkg/skills library)
# and submodules/helix_agent/cmd/similarity-probe (HelixAgent's D-4
# non-importing native port, internal/skills/external_policy.go) — runs
# each against the IDENTICAL on-disk SKILL.md fixture this script writes,
# and diffs their JSON output. Exit 0 + "PARITY: IDENTICAL" only when both
# implementations reach byte-identical admit/exclude decisions on the same
# input; any mismatch is a regression and exits non-zero with the diff.
#
# This is a real, re-runnable regression test (not a one-off probe): run it
# any time either implementation's activate.go / external_policy.go
# changes, to prove the two ports stayed in lockstep.
#
# Usage: sh docs/qa/hxc159_f17_partial_admission_20260923T120446Z/parity_check.sh
# Must be run from the repository root (helix_code).

set -eu

ROOT="$(pwd)"
SKILLS_MOD="$ROOT/submodules/skills"
AGENT_MOD="$ROOT/submodules/helix_agent"

if [ ! -d "$SKILLS_MOD/cmd/similarity-probe" ]; then
  echo "parity_check: $SKILLS_MOD/cmd/similarity-probe not found — run from the helix_code repo root" >&2
  exit 2
fi
if [ ! -d "$AGENT_MOD/cmd/similarity-probe" ]; then
  echo "parity_check: $AGENT_MOD/cmd/similarity-probe not found — run from the helix_code repo root" >&2
  exit 2
fi

WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT

FIXTURE="$WORK/fixture"
mkdir -p "$FIXTURE"

write_skill() {
  # $1 = skill dir/name, $2 = description
  d="$FIXTURE/$1"
  mkdir -p "$d"
  printf -- '---\nname: %s\ndescription: %s\n---\n\n# %s\n' "$1" "$2" "$1" > "$d/SKILL.md"
}

# The REAL project corpus scenario that motivated F-17 (tasks.md F-17 ledger
# row): action-prefix-system / reporting-workable-items genuinely conflict
# (score 0.3846, both discuss issue/bug/task/operator-form handling), while
# media-validator / multitrack / session-sync share zero vocabulary with
# either and with each other.
write_skill "action-prefix-system" "Recognize ACTION_NAME prefixes for issue bug task reporting operator forms"
write_skill "reporting-workable-items" "Create workable items for issue bug task reports from operator forms"
write_skill "media-validator" "Validate recorded video screenshots against expected on screen text patterns"
write_skill "multitrack" "Coordinate parallel git worktree tracks for concurrent development streams"
write_skill "session-sync" "Persist and restore terminal session state across agent restarts"

echo "== building + running pkg/skills' similarity-probe =="
( cd "$SKILLS_MOD" && go run ./cmd/similarity-probe "$FIXTURE" ) > "$WORK/pkgskills.json"
cat "$WORK/pkgskills.json"

echo "== building + running helix_agent's similarity-probe =="
( cd "$AGENT_MOD" && go run ./cmd/similarity-probe "$FIXTURE" ) > "$WORK/helixagent.json"
cat "$WORK/helixagent.json"

echo "== diff =="
if diff -u "$WORK/pkgskills.json" "$WORK/helixagent.json"; then
  echo "PARITY: IDENTICAL"
  exit 0
else
  echo "PARITY: MISMATCH — the two implementations diverged, see diff above" >&2
  exit 1
fi
