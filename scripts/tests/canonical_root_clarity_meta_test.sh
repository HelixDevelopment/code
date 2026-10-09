#!/usr/bin/env bash
# canonical_root_clarity_meta_test.sh — paired-mutation meta-test (§1.1) for
# scripts/gates/canonical_root_clarity_gate.sh (CM-CANONICAL-ROOT-CLARITY).
#
# Builds a throwaway fixture tree, plants one known violation at a time, and
# asserts the gate FAILs; repairs it and asserts the gate PASSes. A gate whose
# mutation does not make it FAIL is decoration (§1.1), so every clause the gate
# claims to enforce gets its own mutation here.
#
# M6 is the FALSE-POSITIVE guard: constitution/CLAUDE.md and GEMINI.md legitimately
# contain `## INHERITED FROM ...` inside ```markdown fences as EXAMPLES for
# consumers. Clause (c) must NOT fire on those. A gate that refuses a correct tree
# is a FAIL-bluff (§11.4.201), so this asserts PASS rather than FAIL.
set -uo pipefail

ROOT="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd)"
GATE="$ROOT/scripts/gates/canonical_root_clarity_gate.sh"

TMP="$(mktemp -d)"
cleanup() { rm -rf "$TMP"; }
trap cleanup EXIT

fail=0
assert_fail() {
    if bash "$GATE" "$TMP" >/dev/null 2>&1; then
        echo "ASSERT-FAIL: gate PASSED but should have FAILED ($1)"; fail=1
    else
        echo "ASSERT-OK:   gate FAILED as expected ($1)"
    fi
}
assert_pass() {
    if bash "$GATE" "$TMP" >/dev/null 2>&1; then
        echo "ASSERT-OK:   gate PASSED as expected ($1)"
    else
        echo "ASSERT-FAIL: gate FAILED but should have PASSED ($1)"; fail=1
    fi
}

build_fixture() {
    rm -rf "$TMP"; mkdir -p "$TMP/constitution" "$TMP/.specify/memory"
    for c in Constitution CLAUDE AGENTS QWEN GEMINI; do
        printf '# Helix Constitution — %s\n\nCanonical root. Inherits from nothing.\n' "$c" \
            > "$TMP/constitution/$c.md"
    done
    printf '# Consumer\n\n## INHERITED FROM constitution/CLAUDE.md (submodule)\n\nProject rules.\n' \
        > "$TMP/CLAUDE.md"
    printf '# Pointer — NOT a source of truth\n\n## INHERITED FROM constitution/Constitution.md\n\nZero authority.\n' \
        > "$TMP/.specify/memory/constitution.md"
}

# --- Baseline: a correct tree must PASS ------------------------------------
build_fixture
assert_pass "baseline clean fixture"

# --- M1 [T-P1.06.4]: strip the SpecKit pointer heading -> FAIL --------------
build_fixture
printf '# Pointer\n\nheading removed\n' > "$TMP/.specify/memory/constitution.md"
assert_fail "M1 clause(d) .specify pointer heading stripped"

# --- M2 [T-P1.06.4]: pointer grows its own principles -> FAIL --------------
build_fixture
cat >> "$TMP/.specify/memory/constitution.md" <<'INNER'

## Core Principles

### I. Something
Rules invented here.
INNER
assert_fail "M2 clause(d) .specify pointer declares Core Principles"

# --- M3 [T-P1.06.4]: stock SpecKit template restored over it -> FAIL --------
build_fixture
printf '# [PROJECT_NAME] Constitution\n\n### [PRINCIPLE_1_NAME]\n\n**Version**: [CONSTITUTION_VERSION]\n' \
    > "$TMP/.specify/memory/constitution.md"
assert_fail "M3 clause(d) unfilled stock template placeholders"

# --- M4: consumer loses its inheritance pointer -> FAIL --------------------
build_fixture
printf '# Consumer\n\nProject rules with no pointer.\n' > "$TMP/CLAUDE.md"
assert_fail "M4 clause(b) consumer CLAUDE.md has no inheritance pointer"

# --- M5: a canonical carrier gains a LIVE inheritance block -> FAIL ---------
# This is the mutation that a `| grep -q` implementation silently MISSED
# (SIGPIPE + pipefail inverted the condition) — a PASS-bluff. Keep it.
build_fixture
printf '\n## INHERITED FROM somewhere/else.md\n' >> "$TMP/constitution/CLAUDE.md"
assert_fail "M5 clause(c) canonical carrier contains a live INHERITED FROM block"

# --- M6: FALSE-POSITIVE GUARD — fenced example must NOT trip clause (c) -----
build_fixture
{
    printf '\nConsumers should open with:\n\n'
    printf '```markdown\n'
    printf '## INHERITED FROM constitution/CLAUDE.md\n'
    printf '```\n'
} >> "$TMP/constitution/CLAUDE.md"
assert_pass "M6 clause(c) fenced EXAMPLE is exempt (no false-positive FAIL)"

# --- M7: canonical carrier missing entirely -> FAIL -------------------------
build_fixture
rm -f "$TMP/constitution/GEMINI.md"
assert_fail "M7 clause(a) five-carrier set incomplete (GEMINI.md missing)"

# --- M8: anti-tautology tripwire — empty tree must FAIL, never vacuous PASS --
rm -rf "$TMP"; mkdir -p "$TMP"
assert_fail "M8 empty tree refuses a vacuous PASS"

echo
if [[ "$fail" -ne 0 ]]; then
    echo "META-TEST FAIL: CM-CANONICAL-ROOT-CLARITY did not behave as specified"
    exit 1
fi
echo "META-TEST PASS: every mutation flips the gate as specified (8 assertions)"
exit 0
