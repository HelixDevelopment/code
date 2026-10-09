#!/usr/bin/env bash
# canonical_root_clarity_gate.sh — CONST-059 / §11.4.35 canonical-root inheritance
# clarity, plus the T-P1.06.4 extension to `.specify/memory/`.
#
# GATE ID: CM-CANONICAL-ROOT-CLARITY
#
# WHAT THIS ASSERTS (CONST-059 states clauses a-c; clause (d) is T-P1.06.4):
#   (a) The canonical root is present: constitution/{Constitution,CLAUDE,AGENTS,
#       QWEN,GEMINI}.md — the §11.4.157 five-carrier lockstep set.
#   (b) The consumer's repo-root CLAUDE.md OPENS with an inheritance pointer —
#       either the Claude-Code-native `@constitution/CLAUDE.md` import or the
#       portable `## INHERITED FROM constitution/...` heading.
#   (c) The constitution submodule's OWN carriers contain NO inheritance block.
#       Those files ARE the source of truth; a pointer in them would mean the
#       canonical root inherits from something, which is incoherent.
#   (d) [T-P1.06.4] `.specify/memory/constitution.md` — the file SpecKit reads at
#       its /speckit-plan "Constitution Check" gate — carries the
#       `## INHERITED FROM constitution/Constitution.md` heading and declares NO
#       independent principles. Populating it (e.g. by running
#       /speckit-constitution against it) would create a THIRD constitution in
#       this repository and re-open risk R-23.
#
# WHY CLAUSE (c) STRIPS FENCED CODE BLOCKS (§11.4.201 — assert the REAL condition).
# constitution/CLAUDE.md:91 and constitution/GEMINI.md:33 both contain the literal
# line `## INHERITED FROM constitution/<carrier>.md` INSIDE a ```markdown fence —
# they are EXAMPLES telling consumers what pointer to write, not inheritance
# blocks. A naive `grep '^## INHERITED FROM'` FAILs on both and is a
# false-positive refusal: a FAIL-bluff, the mirror of a PASS-bluff. Every clause
# below therefore matches against fence-stripped content.
#
# WHY EVERY CLAUSE CAPTURES TO A VARIABLE INSTEAD OF PIPING INTO `grep -q`.
# `defence "$f" | grep -qE ...` is WRONG under `set -o pipefail`: grep -q exits at
# the FIRST match, awk then dies of SIGPIPE (141), and pipefail promotes that to
# the pipeline's status. The condition inverts exactly when a match exists —
# clause (b) false-FAILed with the pointer present, and clause (c) would have
# false-PASSed with a real violation present (a PASS-bluff). Both were caught by
# running the gate against the real tree before wiring it. Keep the herestrings.
#
# USAGE:  canonical_root_clarity_gate.sh [ROOT]
#         ROOT defaults to the repository root. The meta-test passes a fixture
#         tree so mutations never touch real governance files.
set -uo pipefail

GATE="CM-CANONICAL-ROOT-CLARITY"
ROOT="${1:-$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd)}"

CARRIERS=(Constitution CLAUDE AGENTS QWEN GEMINI)
SPECKIT_POINTER=".specify/memory/constitution.md"

FAILURES=0
CHECKED=0
fail() { echo "$GATE: FAIL — $1" >&2; FAILURES=$((FAILURES + 1)); }

# Emit $1 with fenced code blocks removed, so examples inside ``` are not
# mistaken for live directives.
defence() { awk 'BEGIN{f=0} /^[[:space:]]*```/{f=!f; next} !f{print}' "$1" 2>/dev/null; }

# --- (a) canonical root present -------------------------------------------
for c in "${CARRIERS[@]}"; do
    CHECKED=$((CHECKED + 1))
    [[ -f "$ROOT/constitution/$c.md" ]] \
        || fail "(a) canonical root missing: constitution/$c.md — the §11.4.157 five-carrier set is incomplete"
done

# --- (b) consumer CLAUDE.md opens with the inheritance pointer -------------
CONSUMER="$ROOT/CLAUDE.md"
if [[ -f "$CONSUMER" ]]; then
    CHECKED=$((CHECKED + 1))
    CONSUMER_TEXT="$(defence "$CONSUMER")"
    if ! grep -qE '^## INHERITED FROM constitution/|^`?@constitution/CLAUDE\.md`?' <<< "$CONSUMER_TEXT"; then
        fail "(b) consumer CLAUDE.md carries no inheritance pointer — it must open with '## INHERITED FROM constitution/CLAUDE.md' or the '@constitution/CLAUDE.md' import"
    fi
else
    fail "(b) consumer CLAUDE.md not found at $CONSUMER"
fi

# --- (c) canonical carriers must NOT inherit -------------------------------
for c in "${CARRIERS[@]}"; do
    f="$ROOT/constitution/$c.md"
    [[ -f "$f" ]] || continue
    CHECKED=$((CHECKED + 1))
    CARRIER_TEXT="$(defence "$f")"
    if grep -qE '^## INHERITED FROM ' <<< "$CARRIER_TEXT"; then
        fail "(c) constitution/$c.md contains a live '## INHERITED FROM' block — the canonical root inherits from nothing (fenced examples are exempt and were stripped)"
    fi
done

# --- (d) T-P1.06.4 — SpecKit pointer stays a pointer ------------------------
SP="$ROOT/$SPECKIT_POINTER"
if [[ -f "$SP" ]]; then
    CHECKED=$((CHECKED + 1))
    SP_TEXT="$(defence "$SP")"
    grep -qE '^## INHERITED FROM constitution/Constitution\.md' <<< "$SP_TEXT" \
        || fail "(d) $SPECKIT_POINTER lost its '## INHERITED FROM constitution/Constitution.md' heading — it is a CONST-059 pointer, not a constitution (re-opens R-23)"
    if grep -qE '^## Core Principles' <<< "$SP_TEXT"; then
        fail "(d) $SPECKIT_POINTER declares its own '## Core Principles' — that is a THIRD constitution in this repository (CONST-059); principles belong in constitution/Constitution.md"
    fi
    if grep -qE '\[PRINCIPLE_[0-9]+_NAME\]|\[CONSTITUTION_VERSION\]' <<< "$SP_TEXT"; then
        fail "(d) $SPECKIT_POINTER contains unfilled SpecKit template placeholders — the stock template was restored over the pointer"
    fi
else
    fail "(d) $SPECKIT_POINTER is missing — SpecKit's Constitution Check reads this path and would fall back to the stock template"
fi

# --- anti-tautology tripwire (§11.4.1) -------------------------------------
# If nothing was examined the checks have silently stopped matching and a PASS
# would be vacuous. Refuse it.
if [[ "$CHECKED" -eq 0 ]]; then
    echo "$GATE: FAIL — examined 0 file(s); a PASS here would be vacuous (wrong ROOT, or layout drift?)" >&2
    exit 1
fi

echo "$GATE: checked $CHECKED item(s) across ${#CARRIERS[@]} canonical carrier(s), the consumer CLAUDE.md and $SPECKIT_POINTER"
if [[ "$FAILURES" -gt 0 ]]; then
    echo "$GATE: FAIL — $FAILURES canonical-root clarity violation(s)" >&2
    exit 1
fi
echo "$GATE: PASS — canonical root intact, consumer inherits, canonical carriers inherit from nothing, and the SpecKit pointer declares no principles of its own"
exit 0
