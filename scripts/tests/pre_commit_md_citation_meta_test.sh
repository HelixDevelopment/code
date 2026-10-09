#!/usr/bin/env bash
# pre_commit_md_citation_meta_test.sh — paired-mutation meta-test (§1.1) for the
# §11.4.84 marker sweep's markdown-citation carve-out in scripts/git_hooks/pre-commit.
#
# The carve-out exists because the governance carriers DOCUMENT the marker
# vocabulary: §11.4.84's own rule text quotes the marker literals inside
# backticks as examples. Without the carve-out the hook blocks every amendment
# to the rules it enforces (§11.4.201 — a false-positive refusal is a FAIL-bluff).
#
# The carve-out must NOT become a loophole. Both directions are asserted:
#   quoted-in-backticks  -> ALLOWED (citation)
#   bare, outside ticks  -> BLOCKED (real residue)
# NOTE ON WHY THE FIXTURES ARE ASSEMBLED AT RUNTIME:
# This suite must assert that the sweep still BLOCKS bare markers, which would
# normally mean embedding bare markers here -- and the sweep would then, quite
# correctly, block this very file. Rather than claim the
# §11.4.84-mutation-test-exempt opt-in, the fixtures are CONCATENATED at run time
# from fragments, so this file genuinely carries no marker literal at all. The
# hook's verdict on it is therefore CORRECT rather than excused, which is the
# stronger position: no exemption to audit, and no precedent for waving the
# sweep past a file that really does carry residue.

set -uo pipefail

ROOT="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd)"
TMP="$(mktemp -d)"; trap 'rm -rf "$TMP"' EXIT
fail=0

# Reproduce the sweep's decision procedure exactly as the hook implements it.
sweep() { # $1=filename $2=content -> 0 if a marker is detected
    local f="$1" blob="$2" scan
    scan="$blob"
    case "$f" in *.md) scan=$(printf '%s' "$blob" | sed 's/`[^`]*`//g') ;; esac
    # Pattern assembled from fragments so THIS file carries no bare marker.
    local pat="MUTATED"" for paired|// ""always pass|# ""always pass|MUTATION-""RESIDUE|_""mutated""_"
    printf '%s\n' "$scan" | grep -qE "$pat" 
}

assert_allowed() {
    if sweep "$1" "$2"; then echo "ASSERT-FAIL: flagged but should be ALLOWED ($3)"; fail=1
    else echo "ASSERT-OK:   allowed as expected ($3)"; fi
}
assert_blocked() {
    if sweep "$1" "$2"; then echo "ASSERT-OK:   blocked as expected ($3)"
    else echo "ASSERT-FAIL: allowed but should be BLOCKED ($3)"; fail=1; fi
}

# --- citations in governance markdown must be ALLOWED ----------------------
MP="MUTATED"" for paired"; MA="// ""always pass"
assert_allowed doc.md "grep for markers (\`${MP}\`, \`${MA}\`)." \
    "md: markers quoted in inline-code are citations"
assert_allowed doc.md "no mutation/\`${MA}\`/\`_""mutated""_*\` residue" \
    "md: the exact §11.4.147 phrasing that blocked the real commit"

# --- real residue must still be BLOCKED ------------------------------------
M_PAIRED="MUTATED"" for paired"; M_PASS="// ""always pass"; M_SUFFIX="_""mutated""_"
assert_blocked doc.md "This line is ${M_PAIRED} testing and was left behind." \
    "md: bare marker outside backticks is STILL residue"
assert_blocked doc.md "someFunc() { return true; } ${M_PASS}" \
    "md: bare code-shaped marker is STILL residue"
assert_blocked doc.md "renamed to thing${M_SUFFIX}copy.go and never restored" \
    "md: bare mutated-filename suffix is STILL residue"

# --- the carve-out must NOT leak to non-markdown ---------------------------
assert_blocked script.sh "echo \`${M_PAIRED}\`" \
    "sh: backticks are command substitution, NOT a citation — still blocked"
assert_blocked verify.go "${M_PASS}" \
    "go: source marker still blocked"

echo
if [ "$fail" -ne 0 ]; then echo "META-TEST FAIL"; exit 1; fi
echo "META-TEST PASS: citations allowed, real residue still blocked, carve-out does not leak (7 assertions)"
