#!/usr/bin/env bash
# skill_manifest_complete_meta_test.sh — paired-mutation meta-test (§1.1) for the
# CM-SKILL-MANIFEST-COMPLETE gate (scripts/gates/skill_manifest_complete_gate.sh).
#
# Each mutation must flip the gate to FAIL; the repaired tree must PASS.
# Sandbox-only (mktemp fakeproj + --root): NEVER touches the live tree.
set -euo pipefail

ROOT="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd)"
GATE="$ROOT/scripts/gates/skill_manifest_complete_gate.sh"

TMP="$(mktemp -d)"
cleanup() { rm -rf "$TMP"; }
trap cleanup EXIT

SANDBOX="$TMP/fakeproj"
mkdir -p "$SANDBOX/scripts" "$SANDBOX/constitution/skills"
# Two-row manifest; both sources exist with exact-case SKILL.md.
printf 'alpha\tconstitution/skills/alpha\tconstitution\nbeta\tconstitution/skills/beta\tconstitution\n' \
  > "$SANDBOX/scripts/skills_manifest.tsv"
for s in alpha beta; do
  mkdir -p "$SANDBOX/constitution/skills/$s"
  printf -- '---\nname: %s\ndescription: fixture\n---\n# %s\n' "$s" "$s" > "$SANDBOX/constitution/skills/$s/SKILL.md"
done

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

# Baseline.
assert_pass "clean sandbox (2 manifest rows, 2 exact-case manifests)"

# Mutation 1: delete a manifest-row SKILL.md -> FAIL (T-P1.01.5's literal mutation).
rm "$SANDBOX/constitution/skills/alpha/SKILL.md"
assert_fail "manifest row 'alpha' lost its SKILL.md"
printf -- '---\nname: alpha\ndescription: fixture\n---\n# alpha\n' > "$SANDBOX/constitution/skills/alpha/SKILL.md"
assert_pass "alpha SKILL.md restored"

# Mutation 2: case split — rename SKILL.md -> skill.md (R-05, case-exact probe).
mv "$SANDBOX/constitution/skills/beta/SKILL.md" "$SANDBOX/constitution/skills/beta/skill.md"
assert_fail "beta manifest is lowercase skill.md"
mv "$SANDBOX/constitution/skills/beta/skill.md" "$SANDBOX/constitution/skills/beta/SKILL.md"
assert_pass "beta case restored"

# Mutation 3: an unlisted constitution skill dir carrying register.sh but no SKILL.md
# (the measured `multitrack` defect, FR-010 / RS-04) -> FAIL.
mkdir -p "$SANDBOX/constitution/skills/gamma"
printf '#!/bin/sh\nexit 0\n' > "$SANDBOX/constitution/skills/gamma/register.sh"
assert_fail "gamma has register.sh but no SKILL.md"
printf -- '---\nname: gamma\ndescription: fixture\n---\n# gamma\n' > "$SANDBOX/constitution/skills/gamma/SKILL.md"
assert_pass "gamma manifest authored"

# Mutation 4: manifest row whose source dir does not exist -> FAIL.
printf 'delta\tconstitution/skills/delta\tconstitution\n' >> "$SANDBOX/scripts/skills_manifest.tsv"
assert_fail "manifest row 'delta' points at a missing directory"
sed -i '/^delta\t/d' "$SANDBOX/scripts/skills_manifest.tsv"
assert_pass "dangling row removed"

if [ "$fail" -eq 0 ]; then
  echo "META-TEST PASS: 4/4 mutations flip CM-SKILL-MANIFEST-COMPLETE, repaired tree passes"
else
  echo "META-TEST FAIL"
fi
exit "$fail"
