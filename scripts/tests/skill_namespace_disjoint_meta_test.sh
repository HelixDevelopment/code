#!/usr/bin/env bash
# skill_namespace_disjoint_meta_test.sh — paired-mutation meta-test (§1.1) for
# the CM-SKILL-NAMESPACE-DISJOINT gate (scripts/gates/skill_namespace_disjoint_gate.sh).
#
# Each mutation must flip the gate to FAIL; the repaired tree must PASS.
# Sandbox-only (mktemp fakeproj + --root): NEVER touches the live tree.
set -euo pipefail

ROOT="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd)"
GATE="$ROOT/scripts/gates/skill_namespace_disjoint_gate.sh"

TMP="$(mktemp -d)"
cleanup() { rm -rf "$TMP"; }
trap cleanup EXIT

SANDBOX="$TMP/fakeproj"
mkdir -p "$SANDBOX/scripts" "$SANDBOX/.claude/skills"
cp "$ROOT/scripts/skills_manifest.tsv" "$SANDBOX/scripts/skills_manifest.tsv"
mkdir -p "$SANDBOX/constitution/skills/action-prefix-system"
printf '# fixture\n' > "$SANDBOX/constitution/skills/action-prefix-system/SKILL.md"

fail=0
assert_fail() {
  if "$GATE" --root "$SANDBOX" >/dev/null 2>&1; then
    echo "ASSERT-FAIL: gate PASSED but should have FAILED ($1)"; fail=1
  else
    echo "ASSERT-OK:   gate FAILED as expected ($1)"
  fi
}
assert_pass() {
  if "$GATE" --root "$SANDBOX" >/dev/null 2>&1; then
    echo "ASSERT-OK:   gate PASSED as expected ($1)"
  else
    echo "ASSERT-FAIL: gate FAILED but should have PASSED ($1)"; fail=1
  fi
}

# Baseline: manifest names absent from the tree -> PASS.
assert_pass "clean sandbox"

# Mutation 1: planted real dir colliding with a manifest-owned name -> FAIL.
mkdir -p "$SANDBOX/.claude/skills/media-validator"
printf '# planted\n' > "$SANDBOX/.claude/skills/media-validator/SKILL.md"
assert_fail "real-dir collision on manifest name 'media-validator'"
rm -rf "$SANDBOX/.claude/skills/media-validator"
assert_pass "collision removed"

# Mutation 2: manifest-named symlink pointing away from its declared source -> FAIL.
mkdir -p "$TMP/elsewhere"
ln -s "$TMP/elsewhere" "$SANDBOX/.claude/skills/action-prefix-system"
assert_fail "mispointed manifest-named symlink"
rm -f "$SANDBOX/.claude/skills/action-prefix-system"
assert_pass "mispointed link removed"

# Mutation 3: speckit-* row smuggled into the constitution manifest -> FAIL.
printf 'speckit-plan\tconstitution/skills/action-prefix-system\tconstitution\n' >> "$SANDBOX/scripts/skills_manifest.tsv"
assert_fail "speckit-* row in constitution manifest"
sed -i '/^speckit-plan\t/d' "$SANDBOX/scripts/skills_manifest.tsv"
assert_pass "smuggled row removed"

exit "$fail"
