#!/usr/bin/env bash
# scripts/verify-all-constitution-rules.sh
#
# Post-Constitution-Pull validation sweep — the enforcement engine
# CONST-055 / §11.4.32 names canonical. Without this script, every
# new constitution rule cascades as a decorative anchor; with it,
# every implementable rule gate runs against the post-pull tree
# and produces a directed FAIL on violation.
#
# Gates included (each scoped to what's mechanically checkable):
#   G1  Governance cascade        — scripts/verify-governance-cascade.sh
#                                    (§11.9 + CONST-047..059, 14 anchors × 36 files)
#   G2  CONST-035 anti-bluff smoke — grep for production bluff markers
#                                    in helix_code/internal + helix_code/cmd
#   G3  CONST-050(A) mock-from-prod — grep for internal/mocks imports
#                                    in non-test production code
#   G4  CONST-051(C) nested-own-org — each owned submodule's .gitmodules
#                                    must contain zero own-org references
#   G5  CONST-053 .gitignore audit  — every owned submodule MUST have
#                                    a .gitignore at its root; no `.env`,
#                                    `*.pem`, `*.key`, `id_rsa*` tracked
#   G6  CONST-052 case-conformance  — soft warning for directories at
#                                    HelixCode root using PascalCase /
#                                    kebab-case (renames are phased per
#                                    CONST-052; this gate just surfaces
#                                    candidates, never fails for layout)
#   G7  §11.4.83 docs/qa evidence    — enforcing, baseline-scoped: every
#                                    post-baseline feature commit must carry
#                                    docs/qa/<run-id>/ (delegates to
#                                    verify_qa_evidence.sh --enforce; HXC-019)
#   G8  §11.4.90 Obsolete-Details    — every Obsolete-status tracker item
#                                    carries a valid Obsolete-Details line
#                                    (delegates to obsolete_details_gate.sh; HXC-018)
#   G9  §11.4.91 summary clarity     — no anti-pattern one-liners in the
#                                    summary docs (delegates to
#                                    summary_clarity_gate.sh; HXC-018)
#   G10 §11.4.81 cross-platform parity — no uname-dispatch script drops a
#                                    manifest platform without honest-gap
#                                    citation (cross_platform_parity_gate.sh; HXC-015)
#   G11 §11.4.93/95 workable-items   — docs/workable_items.db validates + is
#                                    byte-identically in sync with Issues.md/
#                                    Fixed.md (workable_items_sync_gate.sh; HXC-026)
#                                    AND that gate's drift ADVICE stays
#                                    direction-neutral, so following it cannot
#                                    delete the SSoT (HXC-252, §9.2;
#                                    sync_gate_direction_neutrality_meta_test.sh)
#   G12 §11.4.12/53 summary freshness — docs/Issues_Summary.md + Fixed_Summary.md
#                                    are a fresh mechanical projection of the
#                                    §11.4.93/95 SQLite SSoT docs/workable_items.db
#                                    (summary_sync_gate.sh → `workable-items
#                                    export`; reconciled per §11.4.120 2026-07-27,
#                                    superseding the text-derived legacy
#                                    generate_{issues,fixed}_summary.sh --check)
#   G13 §11.4.99 sources-verified    — operator-facing docs carry a
#                                    `## Sources verified` footer (advisory
#                                    coverage report; sources_verified_gate.sh;
#                                    --enforce blocks at 100% per HXC-030)
#   G14 §11.4.106 docs_chain verify  — governance docs tree in-sync
#                                    (md→html→pdf hashes checked by the
#                                    docs_chain engine via `verify --all`);
#                                    SKIP-with-reason if engine absent;
#                                    uses `go run` fallback so no pre-built
#                                    binary required
#   G30 §11.4.135 HXC-229 guard      — gateway serves in Gin RELEASE mode (live process)
#   G31 §11.4.135 HXC-233 guard      — completion path returns a REAL generation (live e2e)
#   G32 §11.4.135 HXC-244 guard      — health endpoint names the components it checked
#   G34 §11.4.111 endpoint agreement — configured Helix endpoints agree across sources
#                                      AND reach the service they name (live, 3-state)
#   G35 CONST-042 HXC-168 guard     — no tracked file hands over a DB credential; DB ports bind loopback
#   G36 HXC-168 regression guard  — the standing RED_MODE=0 polarity guard for the HXC-168 exposure class
#
# REGISTRATION DRIFT IS NOW SELF-REPORTING (review R4, 2026-08-10).
# --explain lists the entries above; the sweep defines gates as `want_gate GN`
# blocks. Nothing kept the two in step, so the list stalled at G14 while the
# sweep grew to G32 — and the commit that added G30-G32 initially widened the
# gap by three without noticing. A drifted --explain is quietly wrong in the
# worst way: it answers confidently and omits, so a reader concludes a gate does
# not exist. Rather than re-sync by hand and re-drift next time, --explain now
# diffs itself against the live gate set and names what it cannot describe.
# G15-G29 remain undescribed and are reported as such on every invocation.
#
# (The G30-G32 entries were first inserted BETWEEN G14's heading and its own
# continuation lines, orphaning four lines of G14's description behind this
# paragraph — harmless to --explain, which greps `^#   G[0-9]`, but wrong for
# every human reader. Restored above: continuation lines belong to the heading
# they continue.)
#
# Per CONST-055 anti-bluff: this sweep MUST be paired with a meta-test
# that plants a known violation per gate and asserts the sweep reports
# FAIL. That meta-test is scripts/test-verify-all-constitution-rules.sh
# (paired-mutation per §1.1).
#
# Exit codes:
#   0  — all gates green
#   1  — at least one gate FAIL
#   2  — script setup error (missing dependency)
#
# Usage:
#   bash scripts/verify-all-constitution-rules.sh
#   bash scripts/verify-all-constitution-rules.sh --quiet     # only print failures
#   bash scripts/verify-all-constitution-rules.sh --gate=G2   # run only one gate
#   bash scripts/verify-all-constitution-rules.sh --explain   # print gate descriptions then exit

set -uo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

QUIET=0
ONLY_GATE=""
for arg in "$@"; do
    case "$arg" in
        --quiet)         QUIET=1 ;;
        --gate=*)        ONLY_GATE="${arg#--gate=}" ;;
        --explain)
            grep -E '^#   G[0-9]' "$0" | sed 's/^#   //'
            # Self-audit: every `want_gate GN` block must have a description
            # line above, or --explain silently under-reports the sweep.
            _described="$(grep -oE '^#   G[0-9]+' "$0" | awk '{print $2}' | sort -u)"
            _defined="$(grep -oE '^if want_gate G[0-9]+' "$0" | awk '{print $3}' | sort -u)"
            _missing="$(comm -13 <(printf '%s\n' "$_described") <(printf '%s\n' "$_defined") | tr '\n' ' ')"
            if [ -n "${_missing// /}" ]; then
                echo
                echo "NOTE: $(printf '%s' "$_missing" | wc -w) gate(s) run in this sweep but have NO description above: ${_missing%% }"
                echo "      They still EXECUTE and are still release-blocking — only this listing is incomplete."
                echo "      Tracked as a documentation gap; add a '#   GN <rule> — <what it proves>' line to close each."
            fi
            exit 0 ;;
        *)               echo "unknown arg: $arg" >&2; exit 2 ;;
    esac
done

OWNED_FILE="$ROOT/docs/improvements/submodule_owned.txt"
ORG_PATTERN='vasic-digital|HelixDevelopment|red-elf|ATMOSphere1234321|Bear-Suite|BoatOS123456|Helix-Flow|Helix-Track|Server-Factory'
FAILURES=0
GATES_RUN=0
declare -a GATE_RESULTS=()

# ---------------------------------------------------------------------------
# Helpers
# ---------------------------------------------------------------------------
gate_header() {
    [[ "$QUIET" -eq 1 ]] && return 0
    echo
    echo "=== $1 ==="
}

gate_pass() {
    local id="$1" desc="$2"
    GATE_RESULTS+=("$id|PASS|$desc")
    [[ "$QUIET" -eq 1 ]] && return 0
    echo "  PASS: $desc"
}

gate_fail() {
    local id="$1" desc="$2" fix="$3"
    GATE_RESULTS+=("$id|FAIL|$desc")
    FAILURES=$((FAILURES + 1))
    echo "  FAIL ($id): $desc"
    echo "    Canonical fix: $fix"
}

want_gate() {
    [[ -z "$ONLY_GATE" || "$ONLY_GATE" == "$1" ]]
}

# ---------------------------------------------------------------------------
# G1 — Governance cascade
# ---------------------------------------------------------------------------
if want_gate G1; then
    GATES_RUN=$((GATES_RUN + 1))
    gate_header "G1 — Governance cascade (§11.9 + CONST-047..059)"
    if bash scripts/verify-governance-cascade.sh > /tmp/g1-cascade.out 2>&1; then
        gate_pass G1 "all 14 anchors present across owned submodules + root"
    else
        gate_fail G1 "cascade verifier reported failures (see /tmp/g1-cascade.out)" \
            "inspect output; add missing anchor(s) and re-run"
    fi
fi

# ---------------------------------------------------------------------------
# G2 — CONST-035 anti-bluff smoke (production code only)
# ---------------------------------------------------------------------------
if want_gate G2; then
    GATES_RUN=$((GATES_RUN + 1))
    gate_header "G2 — CONST-035 anti-bluff smoke (production code)"
    # Production = helix_code/{internal,cmd}/**/*.go excluding *_test.go.
    bluff_hits=$(
        find helix_code/internal helix_code/cmd -type f -name "*.go" \
            ! -name "*_test.go" 2>/dev/null | \
        xargs grep -lE "(simulated|TODO implement|fake response|in production this would)" 2>/dev/null
    )
    if [[ -z "$bluff_hits" ]]; then
        gate_pass G2 "zero production bluff markers in helix_code/{internal,cmd}"
    else
        gate_fail G2 "production code contains bluff markers" \
            "files: $bluff_hits — replace simulation with real implementation"
    fi
fi

# ---------------------------------------------------------------------------
# G3 — CONST-050(A) mock-from-production
# ---------------------------------------------------------------------------
if want_gate G3; then
    GATES_RUN=$((GATES_RUN + 1))
    gate_header "G3 — CONST-050(A) mock-from-production audit"
    mock_hits=$(
        find helix_code/cmd helix_code/applications -type f -name "*.go" \
            ! -name "*_test.go" 2>/dev/null | \
        xargs grep -lE 'dev\.helix\.code/internal/mocks' 2>/dev/null
    )
    # internal/* production code (non _test.go) must also not import internal/mocks
    internal_mock_hits=$(
        find helix_code/internal -type f -name "*.go" \
            ! -path "*/mocks/*" ! -name "*_test.go" 2>/dev/null | \
        xargs grep -lE 'dev\.helix\.code/internal/mocks' 2>/dev/null
    )
    all_hits="$mock_hits"$'\n'"$internal_mock_hits"
    all_hits=$(printf '%s\n' "$all_hits" | grep -v '^$' || true)
    if [[ -z "$all_hits" ]]; then
        gate_pass G3 "no production code imports internal/mocks"
    else
        gate_fail G3 "production code imports mocks: $all_hits" \
            "refactor to constructor-injected real implementation; mocks ONLY in *_test.go"
    fi
fi

# ---------------------------------------------------------------------------
# G4 — CONST-051(C) nested-own-org submodule chains
# ---------------------------------------------------------------------------
if want_gate G4; then
    GATES_RUN=$((GATES_RUN + 1))
    gate_header "G4 — CONST-051(C) nested-own-org submodule chains"
    if [[ ! -f "$OWNED_FILE" ]]; then
        gate_fail G4 "owned-submodule list missing at $OWNED_FILE" \
            "create docs/improvements/submodule_owned.txt"
    else
        total_nested=0
        offenders=""
        while IFS=' |' read -r sm rest; do
            [[ -z "$sm" ]] && continue
            gm="$sm/.gitmodules"
            [[ ! -f "$gm" ]] && continue
            cnt=$(grep -cE "$ORG_PATTERN" "$gm" 2>/dev/null) || cnt=0
            cnt=$(printf '%s' "$cnt" | tr -d ' \n\r')
            [[ -z "$cnt" ]] && cnt=0
            if [[ "$cnt" -gt 0 ]] 2>/dev/null; then
                total_nested=$((total_nested + cnt))
                offenders="$offenders $sm($cnt)"
            fi
        done < "$OWNED_FILE"
        if [[ "$total_nested" -eq 0 ]]; then
            gate_pass G4 "no nested own-org submodule chains in any owned submodule"
        else
            gate_fail G4 "nested own-org submodule chains found:$offenders" \
                "move each to parent root per CONST-051(C); track as Task #254-style remediation"
        fi
    fi
fi

# ---------------------------------------------------------------------------
# G5 — CONST-053 .gitignore audit + sensitive-file presence
# ---------------------------------------------------------------------------
if want_gate G5; then
    GATES_RUN=$((GATES_RUN + 1))
    gate_header "G5 — CONST-053 .gitignore + sensitive-file audit"
    missing_gitignore=""
    tracked_sensitive=""
    # 1. Every owned submodule MUST have a .gitignore.
    if [[ -f "$OWNED_FILE" ]]; then
        while IFS=' |' read -r sm rest; do
            [[ -z "$sm" ]] && continue
            [[ ! -d "$sm" ]] && continue
            if [[ ! -f "$sm/.gitignore" ]]; then
                missing_gitignore="$missing_gitignore $sm"
            fi
        done < "$OWNED_FILE"
    fi
    # 2. Tracked sensitive files at meta-repo level (not in third-party trees).
    # We scan ls-files for canonical patterns; allowlist .env.example / .env.sample.
    tracked_sensitive=$(
        git ls-files 2>/dev/null | \
        grep -E '(^|/)\.env$|(^|/)\.env\.[^/]+$|\.pem$|\.key$|id_rsa(\.|$)|id_ed25519(\.|$)' | \
        grep -vE '\.env\.example$|\.env\.sample$|\.env\.template$' | \
        grep -vE '\.env\.full-test$|\.env\.test$|\.env\.ci$' | \
        grep -vE '^(HelixAgent|cli_agents|cli_agents_resources|dependencies/LLama_CPP|dependencies/Ollama|dependencies/HuggingFace_Hub|helix_qa/tools|panoptic/tests|panoptic/docs)' || true
    )
    if [[ -z "$missing_gitignore" && -z "$tracked_sensitive" ]]; then
        gate_pass G5 "every owned submodule has .gitignore + no sensitive files tracked at owned-paths"
    else
        if [[ -n "$missing_gitignore" ]]; then
            gate_fail G5 "owned submodules missing .gitignore:$missing_gitignore" \
                "create .gitignore in each per CONST-053; minimum: build/, *.log, .env, .DS_Store"
        fi
        if [[ -n "$tracked_sensitive" ]]; then
            gate_fail G5 "tracked sensitive files found: $tracked_sensitive" \
                "git rm --cached + add to .gitignore + rotate the secret per CONST-042"
        fi
    fi
fi

# ---------------------------------------------------------------------------
# G6 — CONST-052 case-conformance (soft, surfaces candidates only)
# ---------------------------------------------------------------------------
#
# Operator safety mandate (2026-05-15): "double check that snake_case
# renaming is not applied to codebase which is by convention non-snake-case
# — we MUST NOT break the System and working building process".
#
# Per CONST-052's own "common-sense exceptions (technology-preserving)"
# clause, every directory whose name is mandated by language / tool /
# framework convention is exempt from the rename. G6 enforces those
# exemptions: it ONLY enumerates dirs that aren't already exempt, and
# it NEVER fails the build (soft surface — the actual rename is phased
# per Task #252 with full test-execution before each batch).
#
# NEVER_RENAME_PATTERNS — directories that MUST NOT be renamed even if
# their name violates snake_case, because renaming them would break the
# tooling that depends on the exact filename:
NEVER_RENAME_PATTERNS=(
    # Language / framework dirs that mandate specific case
    'gradlew*'              # Gradle wrapper (case-sensitive)
    'gradle'                # Gradle config dir
    'Cargo.toml' 'Cargo.lock'   # Rust
    'Gemfile' 'Gemfile.lock'    # Ruby
    'Makefile' 'GNUmakefile'    # GNU make
    'Dockerfile' 'Containerfile'
    'CMakeLists.txt'
    'build.gradle' 'build.gradle.kts'
    'pom.xml'
    'package.json' 'package-lock.json' 'pnpm-lock.yaml'
    'tsconfig.json' 'jsconfig.json'
    'pyproject.toml' 'setup.py' 'setup.cfg' 'requirements.txt'
    'go.mod' 'go.sum'
    # Android AOSP-mandated names (renaming = build break)
    'AndroidManifest.xml'
    'Android.bp' 'Android.mk'
    'AndroidTest.xml'
    # Apple framework / Xcode-mandated names
    'Info.plist'
    'Podfile' 'Podfile.lock'
    '*.xcodeproj' '*.xcworkspace'
    # AOSP top-level directories (the Android build system depends on these)
    'art' 'bionic' 'bootable' 'bootloader' 'build' 'cts' 'dalvik'
    'developers' 'development' 'device' 'docs' 'external' 'frameworks'
    'hardware' 'kernel' 'kernel-5.10' 'libcore' 'libnativehelper'
    'ndk' 'out' 'packages' 'pdk' 'platform_testing' 'prebuilts'
    'sdk' 'system' 'test' 'toolchain' 'tools' 'vendor'
    # Build / cache / generated artefacts (kept by tooling convention)
    'node_modules' '__pycache__' '.gradle' '.idea' '.vscode'
    'target' 'dist' 'out' 'build'
    # VCS + governance
    '.git' '.github' '.gitlab' '.svn'
    # Names that fail snake_case but ARE the canonical owned-project names
    # — these will be renamed in a deliberate phased batch per Task #252,
    # but the rename includes coordinated upstream-repo renames + every
    # consumer's .gitmodules update, so the local-dir-only check should
    # NOT flag them as "easy fixes". They're explicitly tracked.
    'helix_code' 'challenges' 'containers' 'dependencies'
    'github_pages_website' 'helix_agent' 'helix_qa' 'security' 'panoptic'
    'upstreams' 'assets' 'mcp_servers'
)

is_protected_name() {
    local name="$1"
    for pattern in "${NEVER_RENAME_PATTERNS[@]}"; do
        # shellcheck disable=SC2053
        [[ "$name" == $pattern ]] && return 0
    done
    return 1
}

if want_gate G6; then
    GATES_RUN=$((GATES_RUN + 1))
    gate_header "G6 — CONST-052 case-conformance (soft — phased per task #252)"
    # Soft gate: list top-level dirs that aren't snake_case AND aren't
    # protected by the never-rename patterns above. Operator safety
    # mandate honoured: AOSP / framework / build-system dirs are never
    # flagged because renaming them would break the build.
    candidates=""
    while IFS= read -r name; do
        [[ -z "$name" ]] && continue
        # Already snake_case (lowercase + underscores + digits)?
        if [[ "$name" =~ ^[a-z0-9_]+$ ]]; then continue; fi
        if is_protected_name "$name"; then continue; fi
        candidates="$candidates $name"
    done < <(find . -maxdepth 1 -mindepth 1 -type d \
                 ! -path "./.git" ! -path "./.github" \
                 -printf "%f\n" 2>/dev/null)
    candidate_count=$(printf '%s\n' "$candidates" | tr ' ' '\n' | grep -v '^$' | wc -l | tr -d ' ')
    # Also enumerate protected (for forensic visibility — show what we
    # deliberately did NOT flag, so the operator can confirm the
    # exemption coverage matches their mental model).
    protected_count=$(
        while IFS= read -r name; do
            [[ -z "$name" ]] && continue
            if [[ "$name" =~ ^[a-z0-9_]+$ ]]; then continue; fi
            if is_protected_name "$name"; then echo "$name"; fi
        done < <(find . -maxdepth 1 -mindepth 1 -type d \
                     ! -path "./.git" ! -path "./.github" \
                     -printf "%f\n" 2>/dev/null) | wc -l | tr -d ' '
    )
    if [[ "$candidate_count" -eq 0 ]]; then
        gate_pass G6 "all top-level directories snake_case OR protected ($protected_count protected)"
    else
        # NOT a failure — phased rename per CONST-052 + Task #252.
        gate_pass G6 "$candidate_count unprotected rename candidates ($protected_count protected from rename)"
    fi
fi

# ---------------------------------------------------------------------------
# G7 — §11.4.83 docs/qa end-user evidence (enforcing, baseline-scoped)
# ---------------------------------------------------------------------------
if want_gate G7; then
    GATES_RUN=$((GATES_RUN + 1))
    gate_header "G7 — §11.4.83 docs/qa/ end-user evidence (HXC-019)"
    # Baseline = the historical line below which feature commits are exempt;
    # the gate ENFORCES docs/qa/<run-id>/ for every commit ABOVE it.
    #
    # BASELINE BUMP — 2026-06-22 (G7 remediation, §11.4.83 / §11.4.6):
    #   The original baseline was the commit that ADDED docs/qa/README.md
    #   (ed84f90e, 2026-05-28). By 2026-06-22 the ed84f90e..HEAD range had
    #   accumulated 118 pre-existing, already-pushed feature commits with NO
    #   docs/qa/<run-id>/ transcript. Those transcripts cannot be honestly
    #   retro-captured (the end-to-end runtime evidence §11.4.83 demands never
    #   existed for those commits; fabricating 118 after-the-fact transcripts
    #   would be a §11.4 PASS-bluff). The honest remediation is to exempt the
    #   118 as historical debt and KEEP the gate enforcing for every NEW commit
    #   — so the baseline is bumped to the 2026-06-22 HEAD. This moves the
    #   historical line forward ONLY; it does NOT weaken forward enforcement
    #   (every commit after the baseline still requires its docs/qa/<run-id>/
    #   directory or a [no-qa-evidence] opt-out token). The QA_EVIDENCE_BASELINE
    #   env var still overrides for a future re-baseline.
    #     Prior baseline: ed84f90e7471fb683f7779bac80cdfd169620159 (118 violations)
    qa_baseline="${QA_EVIDENCE_BASELINE:-925169c98945ca0fee1e84dae53ad494e4897832}"
    # Survive history edits: if the bumped SHA is unreachable in this checkout,
    # fall back to the commit that introduced the convention (pre-bump behaviour).
    if ! git -C "$ROOT" rev-parse --verify --quiet "${qa_baseline}^{commit}" >/dev/null 2>&1; then
        qa_baseline="$(git -C "$ROOT" log --diff-filter=A --format=%H -- docs/qa/README.md 2>/dev/null | tail -1)"
        [[ -z "$qa_baseline" ]] && qa_baseline="ed84f90e"
    fi
    if bash "$ROOT/scripts/verify_qa_evidence.sh" --enforce --since "$qa_baseline" >/tmp/g7-qa.out 2>&1; then
        gate_pass G7 "every post-baseline feature commit carries docs/qa/<run-id>/ evidence"
    else
        gate_fail G7 "feature commit(s) lack docs/qa/<run-id>/ evidence (see /tmp/g7-qa.out)" \
            "$(tail -5 /tmp/g7-qa.out)"
    fi
fi

# ---------------------------------------------------------------------------
# G8 — §11.4.90 Obsolete-Details presence on Obsolete-status items
# ---------------------------------------------------------------------------
if want_gate G8; then
    GATES_RUN=$((GATES_RUN + 1))
    gate_header "G8 — §11.4.90 Obsolete-Details (HXC-018)"
    if bash "$ROOT/scripts/gates/obsolete_details_gate.sh" >/tmp/g8-obs.out 2>&1; then
        gate_pass G8 "every Obsolete-status item carries a valid Obsolete-Details line"
    else
        gate_fail G8 "Obsolete item(s) missing/invalid Obsolete-Details (see /tmp/g8-obs.out)" \
            "$(tail -5 /tmp/g8-obs.out)"
    fi
fi

# ---------------------------------------------------------------------------
# G9 — §11.4.91 summary-doc clarity (no anti-pattern one-liners)
# ---------------------------------------------------------------------------
if want_gate G9; then
    GATES_RUN=$((GATES_RUN + 1))
    gate_header "G9 — §11.4.91 summary-doc clarity (HXC-018)"
    if bash "$ROOT/scripts/gates/summary_clarity_gate.sh" >/tmp/g9-sum.out 2>&1; then
        gate_pass G9 "every summary one-liner is self-contained (no anti-pattern rows)"
    else
        gate_fail G9 "summary anti-pattern one-liner(s) present (see /tmp/g9-sum.out)" \
            "$(tail -5 /tmp/g9-sum.out)"
    fi
fi

# ---------------------------------------------------------------------------
# G10 — §11.4.81 cross-platform parity (HXC-015)
# ---------------------------------------------------------------------------
if want_gate G10; then
    GATES_RUN=$((GATES_RUN + 1))
    gate_header "G10 — §11.4.81 cross-platform parity (HXC-015)"
    if bash "$ROOT/scripts/gates/cross_platform_parity_gate.sh" >/tmp/g10-cpp.out 2>&1; then
        gate_pass G10 "no multi-platform script drops a manifest platform without honest-gap citation"
    else
        gate_fail G10 "uname-dispatch script(s) omit a manifest platform (see /tmp/g10-cpp.out)" \
            "$(tail -5 /tmp/g10-cpp.out)"
    fi
fi

# ---------------------------------------------------------------------------
# G11 — §11.4.93/95 workable-items md↔db in sync (HXC-013/HXC-026)
# ---------------------------------------------------------------------------
if want_gate G11; then
    GATES_RUN=$((GATES_RUN + 1))
    gate_header "G11 — §11.4.93/95 workable-items md↔db sync (HXC-026)"
    bash "$ROOT/scripts/gates/workable_items_sync_gate.sh" >/tmp/g11-wi.out 2>&1
    g11_sync=$?

    # HXC-252 DRIFT-ADVICE GUARD, co-located here on purpose.
    #
    # The sync gate's DETECTION was never wrong; its ADVICE was. It asserted a
    # staleness DIRECTION that a symmetric `diff -q` cannot compute and
    # prescribed the remedy that follows from it, which in the DB-newer case
    # overwrites the §11.4.95 SSoT with a stale document. The guard for that is
    # a §11.4.115 polarity test that was shipped with a standing-guard header
    # and ZERO invocation sites — a §11.4.135 standing guard that nothing runs
    # is not standing (independent review round 5, F3). This is the invocation.
    #
    # It runs UNCONDITIONALLY, not only on the sync-pass path: the advice is
    # static text, and it is most load-bearing exactly when drift EXISTS and a
    # maintainer is about to act on it.
    #
    # Reported ahead of drift because a wrong-direction prescription is a §9.2
    # data-destruction defect, while drift is a hygiene defect.
    bash "$ROOT/scripts/tests/sync_gate_direction_neutrality_meta_test.sh" >/tmp/g11-advice.out 2>&1
    g11_adv=$?

    if [[ "$g11_adv" -eq 2 ]]; then
        gate_fail G11 "the drift-advice guard could not RUN (exit 2, §11.4.3 environment SKIP) — it certified nothing, so the gate's advice is unverified; this is NOT a detected regression (see /tmp/g11-advice.out)" \
            "$(tail -2 /tmp/g11-advice.out)"
    elif [[ "$g11_adv" -ne 0 ]]; then
        gate_fail G11 "the sync gate's DRIFT ADVICE again prescribes a direction it never computed — a maintainer who follows it while the DB is the newer side DELETES items from the §11.4.95 SSoT (HXC-252, §9.2; see /tmp/g11-advice.out)" \
            "$(grep 'NOT OK' /tmp/g11-advice.out | head -4)"
    elif [[ "$g11_sync" -ne 0 ]]; then
        gate_fail G11 "docs/workable_items.db drifted from Issues.md/Fixed.md (see /tmp/g11-wi.out)" \
            "$(tail -3 /tmp/g11-wi.out)"
    else
        gate_pass G11 "$(tail -1 /tmp/g11-wi.out | sed 's/^CM-WORKABLE-ITEMS-MD-DB-IN-SYNC: //') + drift advice direction-neutral ($(grep -c '^  ok ' /tmp/g11-advice.out) assertions)"
    fi
fi

# ---------------------------------------------------------------------------
# G12 — §11.4.12/53 summary freshness (Issues_Summary/Fixed_Summary vs the
#       §11.4.93/95 SQLite SSoT docs/workable_items.db). Reconciled per §11.4.120
#       on 2026-07-27: the text-derived legacy generators were superseded by the
#       DB-derived `workable-items export` — see scripts/gates/summary_sync_gate.sh.
# ---------------------------------------------------------------------------
if want_gate G12; then
    GATES_RUN=$((GATES_RUN + 1))
    gate_header "G12 — §11.4.12/53 summary-doc freshness vs SQLite SSoT (CM-{ISSUES,FIXED}-SUMMARY-SYNC)"
    if bash "$ROOT/scripts/gates/summary_sync_gate.sh" >/tmp/g12-summary.out 2>&1; then
        gate_pass G12 "$(tail -1 /tmp/g12-summary.out | sed 's/^CM-SUMMARY-SYNC: //')"
    else
        gate_fail G12 "summary docs drifted from the SQLite SSoT (docs/workable_items.db) — regenerate via the §11.4.93 binary (HXC-201: use ABSOLUTE paths — go run -C relocates the child process's cwd, so a relative --db/--out-dir silently writes inside the tool's own directory): go run -C constitution/scripts/workable-items ./cmd/workable-items export --db \"$ROOT/docs/workable_items.db\" --out-dir \"$ROOT/docs\" (see /tmp/g12-summary.out)" \
            "$(tail -6 /tmp/g12-summary.out)"
    fi
fi

# ---------------------------------------------------------------------------
# G13 — §11.4.99 Sources-verified footer coverage (ENFORCING since HXC-030 closed)
# ---------------------------------------------------------------------------
if want_gate G13; then
    GATES_RUN=$((GATES_RUN + 1))
    gate_header "G13 — §11.4.99 Sources-verified footers (CM-SOURCES-VERIFIED; HXC-030)"
    # HXC-030 reached 100% operator-instruction coverage (2026-05-29), so this is
    # now ENFORCING: any operator-facing doc lacking a `## Sources verified`
    # footer FAILs the sweep (a new/un-verified operator doc must be §11.4.99-verified).
    if bash "$ROOT/scripts/gates/sources_verified_gate.sh" --enforce >/tmp/g13-sv.out 2>&1; then
        gate_pass G13 "$(grep -oE '[0-9]+/[0-9]+ operator-facing docs footered \([0-9]+%\)' /tmp/g13-sv.out | head -1)"
    else
        gate_fail G13 "operator-facing doc(s) lack a §11.4.99 Sources-verified footer (see /tmp/g13-sv.out)" \
            "$(grep -E '    - |FAIL' /tmp/g13-sv.out | head -8)"
    fi
fi

# ---------------------------------------------------------------------------
# G14 — §11.4.106 docs_chain verify — governance docs tree in-sync
# ---------------------------------------------------------------------------
if want_gate G14; then
    GATES_RUN=$((GATES_RUN + 1))
    gate_header "G14 — §11.4.106 docs_chain verify (CM-COVENANT-114-106-PROPAGATION)"

    # Locate the docs_chain engine.  Strategy (in order of preference):
    #   1. Pre-built binary at .docs_chain/bin/docs_chain (optional, fast)
    #   2. `docs_chain` on PATH (if globally installed)
    #   3. go run ../docs_chain/cmd/docs_chain  (sibling project — no prebuilt needed)
    # If NONE is available: honest SKIP-with-reason per §11.4.3 + §11.4.106.
    # NEVER fake-pass when the tool is absent (§11.4.106 anti-bluff invariant).
    DC_CMD=""
    DC_ROOT_ARGS=(--root "$ROOT")

    if [[ -x "$ROOT/.docs_chain/bin/docs_chain" ]]; then
        DC_CMD="$ROOT/.docs_chain/bin/docs_chain"
    elif command -v docs_chain &>/dev/null; then
        DC_CMD="docs_chain"
    elif [[ -f "$ROOT/../docs_chain/cmd/docs_chain/main.go" ]]; then
        # go run path — build into a temp binary so we can time it once per sweep
        _dc_tmp="/tmp/_docs_chain_gate14"
        if go build -o "$_dc_tmp" "$ROOT/../docs_chain/cmd/docs_chain" 2>/tmp/g14-build.log; then
            DC_CMD="$_dc_tmp"
        else
            gate_fail G14 \
                "docs_chain build from sibling ../docs_chain failed (see /tmp/g14-build.log)" \
                "run: cd /Volumes/T7/Projects/docs_chain && go build -o /tmp/docs_chain ./cmd/docs_chain"
        fi
    fi

    if [[ -z "$DC_CMD" ]]; then
        # SKIP-with-reason: engine not available — do NOT fake-pass (§11.4.106 §11.4.3)
        GATE_RESULTS+=("G14|SKIP|docs_chain engine absent (not on PATH, no sibling ../docs_chain, no .docs_chain/bin/docs_chain) — SKIP-OK: install engine to enforce §11.4.106")
        [[ "$QUIET" -eq 0 ]] && echo "  SKIP (G14): docs_chain engine absent — §11.4.106 cannot be mechanically enforced until engine is installed. SKIP-OK: §11.4.3"
    else
        if "$DC_CMD" verify --all "${DC_ROOT_ARGS[@]}" >/tmp/g14-verify.log 2>&1; then
            gate_pass G14 \
                "docs_chain contexts all in-sync (governance md→html→pdf hashes verified)"
        else
            gate_fail G14 \
                "docs_chain verify --all reported drift or error — run: docs_chain sync --all --root . (see /tmp/g14-verify.log)" \
                "$(tail -6 /tmp/g14-verify.log)"
            cat /tmp/g14-verify.log >&2
        fi
    fi
fi

# ---------------------------------------------------------------------------
# G15 — §11.4.153/§11.4.86 feature-Status video-evidence durability
# ---------------------------------------------------------------------------
if want_gate G15; then
    GATES_RUN=$((GATES_RUN + 1))
    gate_header "G15 — §11.4.153/§11.4.86 feature video-evidence durability (CM-FEATURE-STATUS-VIDEO-CONFIRMED)"
    if bash "$ROOT/scripts/gates/feature_video_evidence_gate.sh" >/tmp/g15-fve.out 2>&1; then
        gate_pass G15 "$(tail -1 /tmp/g15-fve.out | sed 's/^GATE PASS: //')"
    else
        gate_fail G15 "docs/features/Status.md confirmed row cites missing/rotatable evidence or roster drifted (see /tmp/g15-fve.out)" \
            "$(tail -5 /tmp/g15-fve.out)"
    fi
fi

# ---------------------------------------------------------------------------
# G16 — §11.4.135 Android challenge-matrix per-OS-dispatch contract guard
# ---------------------------------------------------------------------------
if want_gate G16; then
    GATES_RUN=$((GATES_RUN + 1))
    gate_header "G16 — §11.4.135 challenge-matrix runner contract (CM-REGRESSION-GUARD; HXC-108/112)"
    if bash "$ROOT/helix_code/scripts/tests/run_challenge_matrix_test.sh" >/tmp/g16-rcm.out 2>&1; then
        gate_pass G16 "$(grep -oE 'RESULT: [0-9]+ passed, [0-9]+ failed' /tmp/g16-rcm.out | tail -1) — run-challenge-matrix dispatch/preflight contract pinned"
    else
        gate_fail G16 "run-challenge-matrix.sh dispatch/preflight/honest-SKIP contract regressed (see /tmp/g16-rcm.out)" \
            "$(tail -6 /tmp/g16-rcm.out)"
    fi
fi

# ---------------------------------------------------------------------------
# G17 — §11.4.108/§11.4.110 fyneui main-goroutine dispatch contract
#
# internal/fyneui documents in PROSE that Do/DoAndWait must be called only from
# a spawned goroutine. Prose is not enforcement, and the failure it guards is
# silent at compile time: under glfw, a DoAndWait issued FROM the main goroutine
# queues onto the very FIFO the main goroutine drains, then blocks — a permanent
# UI freeze with no panic, no log line, no failing test.
#
# Honest boundary (§11.4.6): the gate is TEXTUAL. Go hides goroutine identity
# and fyne's IsMainGoroutine is internal/, so a runtime assertion is genuinely
# unavailable. Read a PASS as "no textually-detectable main-goroutine call site",
# never as "misuse is impossible" — the gate header enumerates what it cannot
# catch. Registered here because an unregistered gate enforces nothing.
# ---------------------------------------------------------------------------
if want_gate G17; then
    GATES_RUN=$((GATES_RUN + 1))
    gate_header "G17 — §11.4.108 fyneui main-goroutine dispatch (CM-FYNEUI-UITHREAD-DISPATCH)"
    if bash "$ROOT/scripts/gates/fyneui_uithread_dispatch_gate.sh" >/tmp/g17-fyneui.out 2>&1; then
        gate_pass G17 "$(grep -oE 'call sites: [0-9]+ \| goroutine-dispatched: [0-9]+ \| violations: [0-9]+' /tmp/g17-fyneui.out | tail -1) — every fyneui.Do/DoAndWait site is goroutine-dispatched"
    else
        gate_fail G17 "a fyneui.Do/DoAndWait call site is not goroutine-dispatched — main-goroutine misuse self-deadlocks the UI under glfw (see /tmp/g17-fyneui.out)" \
            "$(tail -6 /tmp/g17-fyneui.out)"
    fi
fi

# ---------------------------------------------------------------------------
# G18 — §11.4.12/§11.4.120 Fixed-summary item visibility
#
# CM-FIXED-SUMMARY-ITEM-VISIBILITY (formerly CM-FIXED-H2-PIPE-ROW-PARITY).
# Asserts every Fixed.md closure — H2 section, ticket-id pipe row, and every
# Obsolete item — is present in Fixed_Summary.md. Ids are bounded-matched so
# ATM-97 is not satisfied by ATM-970.
#
# Why it was reconciled rather than retired: the OLD invariant demanded both
# representations per item, which was a proxy for the pipe-table-only legacy
# generator. That generator was superseded (it now exits 2 and writes nothing)
# by a representation-agnostic DB-derived renderer, so the proxy went stale
# while its PURPOSE — no closed item missing from the summary — went unguarded.
# Measured against 0a4df699, the commit that actually corrupted the summary and
# dropped 156 items: the OLD invariant flagged 0 visibility failures (its exit 1
# was pipe-row noise), the reconciled one flags 246. A permanently-red gate does
# not merely fail to help — it MASKS the real failure (§11.4.120).
#
# Registered here because an unregistered gate enforces nothing; this is the
# second gate found standalone-only today (§11.4.227).
# ---------------------------------------------------------------------------
if want_gate G18; then
    GATES_RUN=$((GATES_RUN + 1))
    gate_header "G18 — §11.4.12/§11.4.120 Fixed-summary item visibility (CM-FIXED-SUMMARY-ITEM-VISIBILITY)"
    if bash "$ROOT/scripts/gates/fixed_h2_pipe_row_parity_gate.sh" >/tmp/g18-fsv.out 2>&1; then
        gate_pass G18 "$(grep -oE 'checked [0-9]+ H2 closure section\(s\), [0-9]+ ticket-id pipe row\(s\), [0-9]+ Obsolete item\(s\)' /tmp/g18-fsv.out | tail -1) — all present in Fixed_Summary.md"
    else
        gate_fail G18 "a Fixed.md closure is MISSING from Fixed_Summary.md — a closed item is invisible in the summary (see /tmp/g18-fsv.out)" \
            "$(tail -6 /tmp/g18-fsv.out)"
    fi
fi

# ---------------------------------------------------------------------------
# G19 — §11.4.108 per-commit compile integrity (recent window)
#
# Binds `verify-compile-tests` to each COMMIT rather than the working tree.
# The gap it closes: an author whose tree carries an uncommitted symbol sees a
# truthfully-green working-tree gate and ships a commit that compiles for
# nobody else. Exactly that shipped THREE non-compiling commits in this delta
# (3fd55a4d/3c8197cf/905a0b0a — a test referencing a struct field committed
# only later). `go build` never sees _test.go files, so only a test-COMPILING
# check catches it.
#
# WHY BOUNDED TO --last 3, stated honestly rather than hidden: the check costs
# ~50-80s per compile-relevant commit, so sweeping a 67-commit delta is ~30min
# — too expensive for a sweep run this often. The gate's author therefore left
# it deliberately unwired, which an independent review correctly raised as
# Critical: a gate invoked by nothing enforces nothing, and this one exists
# specifically to stop a failure that already happened (§11.4.227).
#
# A bounded window is the honest middle: it enforces on the commits where new
# breakage actually appears, at proportional cost. It does NOT prove the whole
# delta compiles — run the full sweep explicitly for that:
#   bash scripts/gates/commit_compile_integrity_gate.sh --range <base>..HEAD
# Deliberately NOT wired into pre-push: a multi-minute blocking hook would
# violate §11.4.234 (the commit/push mechanism must always stay unblocked).
#
# Further honest boundaries inherited from the gate: -tags=nogui is pinned (no
# X11/GL headers here), so the three !nogui GUI packages are NOT compile-
# verified; and submodules/ is held at live working-tree state, so this proves
# main-repo delta integrity, not main x submodule pin-pair integrity.
# ---------------------------------------------------------------------------
if want_gate G19; then
    GATES_RUN=$((GATES_RUN + 1))
    gate_header "G19 — §11.4.108 per-commit compile integrity, last 3 (CM-COMMIT-COMPILE-INTEGRITY)"
    # HXC-215: the gate now separates "a commit really does not compile" (exit 1,
    # backed by a real compiler diagnostic) from "the build failed for a host or
    # unrecognised reason" (exit 3, which proves NOTHING about the source). Both
    # block, but reporting the second as a broken commit is the false accusation
    # that made this gate untrustworthy — so branch on the code, never on
    # truthiness. Capture rc directly: `if cmd` would collapse 1 and 3 together.
    bash "$ROOT/scripts/gates/commit_compile_integrity_gate.sh" --last 3 >/tmp/g19-cci.out 2>&1
    g19_rc=$?
    if [ "$g19_rc" -eq 0 ]; then
        # Surface the compile-checked COUNT, never a bare "compiles". A window
        # of docs-only commits legitimately checks ZERO, and a pass message that
        # says "each of the last 3 compiles" would then be a bluff at the
        # registration layer — indistinguishable from a window that really did
        # type-check three commits (§11.4 / §11.4.1).
        gate_pass G19 "$(grep -oE 'commits in range: [0-9]+ \| compile-checked: [0-9]+ \| skipped \(no inner Go\): [0-9]+' /tmp/g19-cci.out | tail -1) — no non-compiling commit in the bounded window (see gate comment for what this does NOT cover)"
    elif [ "$g19_rc" -eq 1 ]; then
        gate_fail G19 "a recent commit does NOT compile — it is broken for every checkout but the author's (see /tmp/g19-cci.out)" \
            "$(tail -8 /tmp/g19-cci.out)"
    elif [ "$g19_rc" -eq 3 ]; then
        # Exit 3 is the gate's INCONCLUSIVE verdict: the build failed but emitted
        # no compiler diagnostic, so nothing was proven about any commit's
        # source. Wording this as a broken commit is the exact false accusation
        # HXC-215 exists to remove — mirrors the §11.4.3 SKIP branch of G21/G25/
        # G26. Still a FAIL, because a gate that could not reach a verdict has
        # certified nothing.
        gate_fail G19 "per-commit compile check INCONCLUSIVE (exit 3) — the build failed with no compiler diagnostic, so this is NOT a claim that any commit is broken and NOT a pass (see /tmp/g19-cci.out)" \
            "read the 'full log:' path printed in the output — it is preserved outside the gate's workdir — and classify the failure before blaming any commit"
    else
        # Neither a verdict nor an inconclusive build: the gate itself did not
        # run to completion (2 = usage error, 130/143 = interrupted, anything
        # else = malfunction). Distinct from exit 3 because there may be no build
        # log at all to read.
        gate_fail G19 "per-commit compile-integrity gate did not run to completion (exit $g19_rc — usage error, interrupted, or malfunction); it certified nothing and this is NOT an accusation against any commit (see /tmp/g19-cci.out)" \
            "$(tail -10 /tmp/g19-cci.out)"
    fi
fi

# ---------------------------------------------------------------------------
# G20 — §11.4.106/§11.4.157 superseded-generator naming
#
# The shell summary generators were superseded by the DB exporter, but ~20
# governance manuals kept naming them as the canonical way to regenerate the
# tracker summaries. That was not stale prose — it was actively harmful
# instructions, and it caused real damage TWICE: a gate's own failure message
# recommended running them (which would have rewritten 344 SQLite-derived items
# down to 188, destroying 156 items of tracked state), and on 2026-07-29 an
# agent followed that advice by hand and did exactly that before it was caught.
#
# The generators now refuse to run (exit 2, writing nothing), so the immediate
# hazard is contained. This gate holds the DOCUMENTATION side: every reference
# must name the DB exporter and mark the shell scripts SUPERSEDED, across all
# five governance carriers in lockstep (§11.4.157 — GEMINI.md has silently
# drifted 14 mandates behind before, so partial propagation is itself a
# violation).
#
# Registered here because an unregistered gate enforces nothing (§11.4.227).
# This is the fourth gate found standalone-only in this cycle.
# ---------------------------------------------------------------------------
if want_gate G20; then
    GATES_RUN=$((GATES_RUN + 1))
    gate_header "G20 — §11.4.106/§11.4.157 superseded-generator naming (CM-SUPERSEDED-GENERATOR-NAMING)"
    if bash "$ROOT/scripts/gates/superseded_generator_naming_gate.sh" >/tmp/g20-sgn.out 2>&1; then
        gate_pass G20 "$(grep -oE '[0-9]+ correctly-marked reference\(s\)' /tmp/g20-sgn.out | tail -1) — every reference names the DB exporter, five-carrier lockstep intact"
    else
        gate_fail G20 "a governance doc names the SUPERSEDED shell generators as canonical — following it destroys tracked items (see /tmp/g20-sgn.out)" \
            "$(tail -6 /tmp/g20-sgn.out)"
    fi
fi

# ---------------------------------------------------------------------------
# G21 — §11.4.10/CONST-042 QA-transcript redaction, fail-closed
#
# The security regression guard for HXC-167. Recorded QA transcripts are
# scrubbed of secrets before being committed; that control had TWO independent
# ways to let a secret through while reporting success:
#   (a) the secret was interpolated into sed AS A REGEX with only `|` escaped,
#       so a key containing an unbalanced `[` made sed fail — and the error was
#       swallowed by `2>/dev/null || true`. Measured on the pre-fix artifact:
#       the secret SURVIVED at rc=0.
#   (b) a greedy `.*` extracted only the LAST credential on a line, silently
#       leaving every earlier one in place.
#
# It was NOT registered when it first landed, deliberately: its own check (0)
# was fail-open by the very defect class it guards — `sed | grep -qF` returns
# sed's 141 under pipefail because `grep -q` SIGPIPEs its producer, so a
# PRESENT construct read as absent and RED_MODE SKIPped (rc=2), meaning the
# half that proves the gate can DETECT was not running. It hid because the
# interactive shell it was first tested in resolves `grep` to a ugrep shim that
# drains input, while the gate runs as a script where GNU grep does not.
# Registering it then would have wired a gate whose falsifiability proof did
# not execute. Both polarities now reach real verdicts, so it is wired here.
# ---------------------------------------------------------------------------
if want_gate G21; then
    GATES_RUN=$((GATES_RUN + 1))
    gate_header "G21 — §11.4.10/CONST-042 QA-transcript redaction fail-closed (CM-QA-TRANSCRIPT-REDACTION-FAIL-CLOSED)"
    bash "$ROOT/scripts/gates/qa_transcript_redaction_gate.sh" >/tmp/g21-red.out 2>&1
    g21_rc=$?
    if [[ "$g21_rc" -eq 0 ]]; then
        gate_pass G21 "transcript redaction is literal, loud on failure, and post-write-scanned for server-originated secrets"
    elif [[ "$g21_rc" -eq 2 ]]; then
        # Exit 2 is the gate's §11.4.3 environment SKIP (e.g. the pre-fix
        # artifact is unreachable from history), NOT a detected leak. Reporting
        # it with the leak wording would be a false alarm about a credential
        # reaching a public remote — and a gate that cries wolf gets disabled,
        # which is worse than none (§11.4.201). Still a FAIL, because a
        # security guard that cannot run has not certified anything.
        gate_fail G21 "redaction gate could not RUN (exit 2, §11.4.3 environment SKIP) — it certified nothing; this is not a detected leak (see /tmp/g21-red.out)" \
            "resolve the environment blocker so the gate executes, then re-run"
    else
        gate_fail G21 "transcript redaction can leak a secret while reporting success — a captured credential may reach a public remote (see /tmp/g21-red.out)" \
            "$(tail -8 /tmp/g21-red.out)"
    fi
fi

# ---------------------------------------------------------------------------
# G22 — CONST-046 toolschema i18n default resolves real text (runtime probe)
#
# CM-TOOLSCHEMA-I18N-DEFAULT-RESOLVES. Guards the user-visible defect that
# `digital.vasic.toolschema` shipped silently from the 2026-05-20 tr() migration
# until 8cec90b: the package-default Translator was NoopTranslator{}, a raw
# message-ID echo, so every unwired consumer saw "toolschema_git_desc_commit"
# where "Create git commit" belonged — including user-visible ToolResult.Error
# payloads. Only a consumer's own test ever surfaced it.
#
# RECONCILED per §11.4.120, not fake-passed and not reverted: the gate was
# authored against the PRE-FIX shape (it hard-required the literal
# `activeTr Translator = NoopTranslator{}` and demanded a composition root call
# SetTranslator). 8cec90b fixed the same defect by a better mechanism — a
# go:embed bundle translator as the default — which removed that literal and
# made the composition-root requirement WRONG (no wiring is needed any more).
# The gate now asserts the invariant that actually protects users: the zero-
# wired default resolves REAL bundle text, and NoopTranslator still echoes so
# the seam stays overridable and the guard stays falsifiable.
#
# Upgraded from grep to RUNTIME EVIDENCE (§11.4.5): the old file conceded its
# runtime half was "deliberately not implemented ... no observable subject yet".
# There is one now, so the gate executes tr() in a fresh process via
# `go test -overlay` — which injects the probe WITHOUT writing into the module
# tree — and compares the rendered output against the on-disk bundle rather
# than a hardcoded string. Expected strings are not pinned in the gate, so a
# legitimate bundle-text edit cannot false-FAIL it (§11.4.201).
#
# Exit-code handling mirrors G21 deliberately: 2 is the §11.4.3 environment
# SKIP (no Go toolchain, module tree absent, probe did not execute) and MUST
# NOT be reported with the substantive-violation wording — a gate that cries
# "users see raw message IDs" when it merely could not run gets disabled, which
# is worse than none. It is still a FAIL, because a guard that cannot run has
# certified nothing.
# ---------------------------------------------------------------------------
if want_gate G22; then
    GATES_RUN=$((GATES_RUN + 1))
    gate_header "G22 — CONST-046 toolschema i18n default resolves real text (CM-TOOLSCHEMA-I18N-DEFAULT-RESOLVES)"
    bash "$ROOT/scripts/gates/toolschema_i18n_seam_wired_gate.sh" >/tmp/g22-i18n.out 2>&1
    g22_rc=$?
    if [[ "$g22_rc" -eq 0 ]]; then
        gate_pass G22 "$(grep -m1 -oE 'default_resolves=[a-z]+ interpolation_ok=[a-z]+ noop_echoes=[a-z]+ restore_ok=[a-z]+' /tmp/g22-i18n.out) — runtime-probed in a fresh process, submodule tree unchanged"
    elif [[ "$g22_rc" -eq 2 ]]; then
        gate_fail G22 "i18n default gate could not RUN (exit 2, §11.4.3 environment SKIP) — it certified nothing; this is NOT a detected regression (see /tmp/g22-i18n.out)" \
            "resolve the environment blocker (Go toolchain / submodule checkout) so the probe executes, then re-run"
    else
        gate_fail G22 "toolschema tr() no longer resolves real text with zero wiring — unwired consumers get raw message IDs in user-visible output (see /tmp/g22-i18n.out)" \
            "$(tail -8 /tmp/g22-i18n.out)"
    fi
fi

# ---------------------------------------------------------------------------
# G23 — HXC-194 mock-service CORS origin allowlist (runtime probe)
#
# CM-MOCK-SERVICES-CORS-ALLOWLIST. Guards the hole HXC-194 closed: both e2e
# mock services hand-rolled their Gin CORS middleware, emitted a blanket
# "Access-Control-Allow-Origin: *", and never inspected the request Origin — so
# any web page on any site, opened by anyone able to route to the service, could
# drive every route and read the responses (on the Slack mock: read back every
# captured message and webhook body, and clear them via DELETE).
#
# Deliberately asserts OUR behaviour, never a dependency version. The
# look-alike gin-contrib/cors advisory concerns a package neither module has
# ever depended on; a version-based gate would have gone green on an upgrade
# that never touched this hand-rolled code.
#
# RUNTIME EVIDENCE (§11.4.5), not a grep verdict: the gate executes the guard
# suite in both modules at RED_MODE=0 (must pass), re-executes the SAME source
# at RED_MODE=1 (must FAIL — the §1.1 paired mutation is built into the test's
# polarity switch, so a passing RED means the hole is back or the guard went
# blind), then checks no wildcard literal has crept back into service source.
#
# Exit-code handling mirrors G21/G22: 2 is the §11.4.3 environment SKIP (no Go
# toolchain / module tree absent) and MUST NOT be worded as a detected hole — a
# gate that cries "any website can drive this service" when it merely could not
# run gets disabled, which is worse than none. It is still a FAIL, because a
# guard that cannot run has certified nothing.
# ---------------------------------------------------------------------------
if want_gate G23; then
    GATES_RUN=$((GATES_RUN + 1))
    gate_header "G23 — HXC-194 mock-service CORS origin allowlist (CM-MOCK-SERVICES-CORS-ALLOWLIST)"
    bash "$ROOT/scripts/gates/mock_services_cors_allowlist_gate.sh" >/tmp/g23-cors.out 2>&1
    g23_rc=$?
    if [[ "$g23_rc" -eq 0 ]]; then
        gate_pass G23 "$(grep -m1 -oE 'green=[0-9]+/[0-9]+ red_falsifiable=[0-9]+/[0-9]+ source_clean=[0-9]+/[0-9]+' /tmp/g23-cors.out) — hostile origin denied on simple AND preflight, permitted origin still accepted, guard proven falsifiable"
    elif [[ "$g23_rc" -eq 2 ]]; then
        gate_fail G23 "mock-service CORS gate could not RUN (exit 2, §11.4.3 environment SKIP) — it certified nothing; this is NOT a detected regression (see /tmp/g23-cors.out)" \
            "resolve the environment blocker (Go toolchain / mock module checkout) so the probe executes, then re-run"
    else
        gate_fail G23 "a mock service accepts cross-origin requests from any website again — any page a developer visits could drive it and read its responses (see /tmp/g23-cors.out)" \
            "$(tail -8 /tmp/g23-cors.out)"
    fi
fi

# ---------------------------------------------------------------------------
# G24 — HXC-201 workable-items export writes to its DOCUMENTED destination
# (runtime probe)
#
# CM-EXPORT-OUTPUT-LOCATION. Guards the hole HXC-201 closed: the documented
# regeneration command — `go run -C constitution/scripts/workable-items
# ./cmd/workable-items export --db docs/workable_items.db --out-dir docs` —
# relocates the CHILD PROCESS's cwd to the tool's own directory (a `go run
# -C <dir>` property), so relative --db/--out-dir silently wrote INSIDE
# constitution/scripts/workable-items/docs/ instead of the caller's real
# docs/ tree while printing success-looking "wrote docs/Issues.md" lines and
# materialising a fresh, zero-row workable_items.db at the wrong location.
#
# RUNTIME EVIDENCE (§11.4.5), not a grep verdict: the gate reproduces the
# documented invocation VERBATIM inside a disposable scratch project root
# (never the real docs/ tree) and asserts the regenerated docs land at the
# real destination, non-empty, and nothing lands inside the relocated tool
# directory. RED-verified against the pre-fix tool source (this session,
# HXC-201): all four docs missing/empty at the documented destination and a
# fresh stub materialised inside constitution/scripts/workable-items/docs/ —
# then GREEN after the fix, proving the guard is falsifiable per §11.4.115.
#
# Exit-code handling mirrors G22/G23: 2 is the §11.4.3 environment SKIP (no
# Go toolchain / workable-items source / tracked DB present) and MUST NOT be
# worded as a detected regression — a gate that cries "output goes to the
# wrong place" when it merely could not run gets disabled, which is worse
# than none. It is still a FAIL, because a guard that cannot run has
# certified nothing.
# ---------------------------------------------------------------------------
if want_gate G24; then
    GATES_RUN=$((GATES_RUN + 1))
    gate_header "G24 — HXC-201 workable-items export output location (CM-EXPORT-OUTPUT-LOCATION)"
    bash "$ROOT/scripts/gates/export_output_location_gate.sh" >/tmp/g24-export-loc.out 2>&1
    g24_rc=$?
    if [[ "$g24_rc" -eq 0 ]]; then
        gate_pass G24 "documented export invocation wrote all four docs to the caller's real docs/ tree; nothing landed inside the relocated tool directory"
    elif [[ "$g24_rc" -eq 2 ]]; then
        gate_fail G24 "export-output-location gate could not RUN (exit 2, §11.4.3 environment SKIP) — it certified nothing; this is NOT a detected regression (see /tmp/g24-export-loc.out)" \
            "resolve the environment blocker (Go toolchain / constitution submodule checkout / tracked DB) so the probe executes, then re-run"
    else
        gate_fail G24 "the documented export invocation wrote into the relocated tool directory (or omitted a doc) instead of the caller's real docs/ tree — HXC-201 regressed (see /tmp/g24-export-loc.out)" \
            "$(tail -8 /tmp/g24-export-loc.out)"
    fi
fi

# ---------------------------------------------------------------------------
# G25 — HXC-193 + HXC-195 mcp-server port agreement (runtime bind probe)
#
# CM-MCP-SERVER-PORT-AGREEMENT. Two tickets, ONE root cause: the shipped
# `plugins/mcp-server` service read NOTHING from its environment. So MCP_PORT
# was an inert knob an operator could edit with no change and no error
# (HXC-195), the transport stayed `stdio` and nothing ever called listen(), and
# the health check probed a port where nothing could answer — reporting the
# container unhealthy no matter how well the service worked, forever, to
# anything that replaces unhealthy containers (HXC-193).
#
# The gate asserts BIND == PUBLISHED == PROBED, and it observes BIND at RUNTIME
# by starting the built artifact under the container's own environment rather
# than parsing it out of source. That distinction is the point: a source-only
# assertion is precisely what let this ship (§11.4.108 — SOURCE green says
# nothing about RUNTIME). Its RED_MODE case B proves the runtime layer alone is
# falsifiable, so the gate cannot silently decay into a config-only check.
#
# Ports are never pinned to a literal, so deliberately moving the service to a
# different port cannot false-FAIL this (§11.4.201); it fails only on DISAGREEMENT.
#
# Exit 2 is the §11.4.3 environment SKIP (no node, unbuilt artifact, submodule
# not checked out) and MUST NOT be worded as a detected regression — it means
# the guard certified nothing, which is still a FAIL but a different one.
# ---------------------------------------------------------------------------
if want_gate G25; then
    GATES_RUN=$((GATES_RUN + 1))
    gate_header "G25 — HXC-193/HXC-195 mcp-server port agreement (CM-MCP-SERVER-PORT-AGREEMENT)"
    bash "$ROOT/scripts/gates/mcp_server_port_agreement_gate.sh" >/tmp/g25-mcp-port.out 2>&1
    g25_rc=$?
    if [[ "$g25_rc" -eq 0 ]]; then
        gate_pass G25 "$(grep -m1 -oE 'bind=[0-9]+ published=[0-9]+ probed=[A-Za-z_-]+ transport=[a-z]+' /tmp/g25-mcp-port.out) — bind observed at runtime under the container's own environment"
    elif [[ "$g25_rc" -eq 2 ]]; then
        gate_fail G25 "mcp-server port-agreement gate could not RUN (exit 2, §11.4.3 environment SKIP) — it certified nothing; this is NOT a detected regression (see /tmp/g25-mcp-port.out)" \
            "resolve the environment blocker (node on PATH / 'npm run build' in submodules/helix_agent/plugins/mcp-server / submodule checked out), then re-run"
    else
        gate_fail G25 "mcp-server bind/published/probed ports no longer agree — the container health check cannot answer and the service is unreachable on its published port (see /tmp/g25-mcp-port.out)" \
            "$(tail -8 /tmp/g25-mcp-port.out)"
    fi
fi

# ---------------------------------------------------------------------------
# G26 — HXC-212 mcp-server SSE CORS allowlist (CM-MCP-SERVER-CORS-ALLOWLIST)
#
# Pairs with G25 and guards what G25's own fix ACTIVATED. The SSE transport
# answered every browser with a wildcard Allow-Origin on both the preflight and
# the response path. That hole was DORMANT while the service only spoke stdio
# and never opened a socket — commit 30c81925 (G25's fix) correctly repaired the
# inert-config defect, and in doing so set MCP_TRANSPORT=sse, which made the
# service listen and the wildcard reachable on a published port.
#
# The gate checks BOTH layers per §11.4.108, because the tracked dist/ carried
# the wildcard independently of src/: a source-only check would pass while a
# fresh clone running `node dist/index.js` without building still served it.
# ---------------------------------------------------------------------------
if want_gate G26; then
    GATES_RUN=$((GATES_RUN + 1))
    gate_header "G26 — HXC-212 mcp-server SSE CORS allowlist (CM-MCP-SERVER-CORS-ALLOWLIST)"
    bash "$ROOT/scripts/gates/mcp_server_cors_allowlist_gate.sh" >/tmp/g26-mcp-cors.out 2>&1
    g26_rc=$?
    if [[ "$g26_rc" -eq 0 ]]; then
        gate_pass G26 "$(grep -m1 -oE 'green=[0-9]+/[0-9]+ red_falsifiable=[0-9]+/[0-9]+ source_clean=[0-9]+/[0-9]+ artifact_clean=[0-9]+/[0-9]+' /tmp/g26-mcp-cors.out) — hostile origin denied on preflight AND simple, permitted origin echoed with Vary: Origin, guard proven falsifiable"
    elif [[ "$g26_rc" -eq 2 ]]; then
        gate_fail G26 "mcp-server CORS gate could not RUN (exit 2, §11.4.3 environment SKIP) — it certified nothing; this is NOT a detected regression (see /tmp/g26-mcp-cors.out)" \
            "resolve the environment blocker (node/npm on PATH, submodules/helix_agent checked out, test:cors script present), then re-run"
    else
        gate_fail G26 "the mcp-server SSE transport no longer enforces its origin allowlist — a wildcard Allow-Origin is reachable on the published port (see /tmp/g26-mcp-cors.out)" \
            "$(tail -10 /tmp/g26-mcp-cors.out)"
    fi
fi

# ---------------------------------------------------------------------------
# G27 — HXC-186 G7 RULE 3 citation-vs-coincidence (CM-QA-EVIDENCE-DECLARED-CITATION)
#
# Guards the ENFORCING §11.4.83 gate (G7) against being satisfied by accident.
# G7's RULE 3 cleared a feature commit whenever any tracked docs/qa file
# contained its 8-hex prefix ANYWHERE. Evidence files also record provenance —
# qa_capture_grounding() stamps `## repo HEAD` with `git log --oneline -1`,
# which abbreviates to exactly 8 chars here — so an evidence directory captured
# while HEAD sat on an unrelated commit could clear that commit while
# demonstrating something else. A gate satisfiable by coincidence provides
# false assurance precisely where assurance is the point.
#
# The guard is the §11.4.115 polarity test: RED_MODE=1 reproduces the defect on
# a pre-fix artifact, RED_MODE=0 (used here) asserts it is absent. Its two
# positive controls also assert that a genuine declared citation and RULE 1
# still clear — so a fix that simply rejected everything would FAIL this gate.
# ---------------------------------------------------------------------------
if want_gate G27; then
    GATES_RUN=$((GATES_RUN + 1))
    gate_header "G27 — HXC-186 G7 RULE 3 declared-citation guard (CM-QA-EVIDENCE-DECLARED-CITATION)"
    RED_MODE=0 bash "$ROOT/scripts/tests/qa_evidence_citation_coincidence_meta_test.sh" \
        >/tmp/g27-qa-citation.out 2>&1
    g27_rc=$?
    if [[ "$g27_rc" -eq 0 ]]; then
        gate_pass G27 "$(grep -cE '^  PASS:' /tmp/g27-qa-citation.out) assertion(s) green — a bare provenance stamp cannot clear a commit, while declared citations and RULE 1 still do"
    elif [[ "$g27_rc" -eq 2 ]]; then
        gate_fail G27 "citation guard could not RUN (exit 2, §11.4.3 environment SKIP) — it certified nothing; this is NOT a detected regression (see /tmp/g27-qa-citation.out)" \
            "resolve the environment blocker (git on PATH, scripts/verify_qa_evidence.sh present), then re-run"
    else
        gate_fail G27 "G7 RULE 3 can again be satisfied by a coincidental SHA — the §11.4.83 release gate would accept evidence that documents a different commit (see /tmp/g27-qa-citation.out)" \
            "$(grep -E '^  FAIL:' /tmp/g27-qa-citation.out | head -4)"
    fi
fi

# ---------------------------------------------------------------------------
# G28 — HXC-199 module-identity exact-match regression guard
# (CM-MODULE-IDENTITY-EXACT-MATCH)
#
# HXC-187 renamed the thin root module to `dev.helix.code/meta` so it would
# stop colliding with the inner application module (`dev.helix.code`).
# scripts/probes/hxc159_env_facts.sh originally re-derived that collision with
# a substring test (`[[ "$root_mod" == *"dev.helix.code"* ]]`); since
# `dev.helix.code` is a literal PREFIX of `dev.helix.code/meta`, that test
# still matched after the fix landed and kept reporting the collision as
# unfixed — a false positive that could mislead a reader into reverting
# already-correct work. The fix replaced the substring test with an exact
# string-equality comparison (`module_paths_identical()` in
# scripts/lib/module_identity.sh), but landed without a permanent §11.4.135
# regression guard. This gate is that guard: RED_MODE=1 reproduces the false
# positive on the current tree; the default RED_MODE=0 asserts the root and
# inner modules stay exact-match distinct, with both-direction falsifiability
# checks (a genuine recurrence IS caught; a prefix-only lookalike is NOT) —
# the lookalike and historical-R-26-pair fixtures are exercised in BOTH
# argument orders, because a substring regression can be reintroduced as
# either `"$a" == *"$b"*` or `"$b" == *"$a"*` and a single-order fixture is
# blind to one of them (measured 2026-08-05; see the gate's header).
# ---------------------------------------------------------------------------
if want_gate G28; then
    GATES_RUN=$((GATES_RUN + 1))
    gate_header "G28 — HXC-199 module-identity exact-match regression guard (CM-MODULE-IDENTITY-EXACT-MATCH)"
    RED_MODE=0 bash "$ROOT/scripts/gates/hxc199_module_identity_exact_match_gate.sh" \
        >/tmp/g28-module-identity.out 2>&1
    g28_rc=$?
    if [[ "$g28_rc" -eq 0 ]]; then
        gate_pass G28 "root go.mod and inner helix_code/go.mod module paths verified exact-match distinct via module_paths_identical() — both-direction falsifiability (genuine recurrence caught, prefix lookalike not falsely caught) confirmed (see /tmp/g28-module-identity.out)"
    elif [[ "$g28_rc" -eq 2 ]]; then
        gate_fail G28 "module-identity guard could not RUN (exit 2, §11.4.3 environment SKIP) — it certified nothing; this is NOT a detected regression (see /tmp/g28-module-identity.out)" \
            "resolve the environment blocker (scripts/lib/module_identity.sh, root go.mod, helix_code/go.mod all present), then re-run"
    else
        gate_fail G28 "the root and inner Go modules are no longer exact-match distinct (or the guard's falsifiability preconditions failed) — see /tmp/g28-module-identity.out" \
            "$(tail -10 /tmp/g28-module-identity.out)"
    fi
fi

# ---------------------------------------------------------------------------
# G29 — HXC-237: the SHIPPED workable-items binary carries the closure-evidence
# invariant. The validator everyone runs is a TRACKED PREBUILT binary; on
# 2026-08-06 a new invariant landed in its SOURCE and the artifact was never
# rebuilt, so `validate` reported OK on records the source would have rejected
# (measured 2026-08-08: shipped exit 0 / from-source exit 1 on one identical
# corrupted copy). This gate asserts the REAL condition — the artifact's
# BEHAVIOUR on a golden-bad fixture — rather than an mtime proxy a `touch`
# would satisfy (§11.4.201), with a golden-good fixture as the
# false-positive guard (§11.4.201(1)) and a durable §11.4.115 RED that runs
# the actual pre-fix binary blob from git.
# ---------------------------------------------------------------------------
if want_gate G29; then
    GATES_RUN=$((GATES_RUN + 1))
    gate_header "G29 — HXC-237 workable-items binary freshness (CM-WORKABLE-ITEMS-BINARY-FRESH)"
    RED_MODE=0 bash "$ROOT/scripts/gates/hxc237_workable_items_binary_freshness_gate.sh" \
        >/tmp/g29-wi-binary-fresh.out 2>&1
    g29_rc=$?
    if [[ "$g29_rc" -eq 0 ]]; then
        gate_pass G29 "shipped constitution/scripts/workable-items/bin/workable-items behaviourally carries the closure-evidence-resolvability invariant — reports it on the golden-bad fixture, stays silent on the golden-good one (see /tmp/g29-wi-binary-fresh.out)"
    elif [[ "$g29_rc" -eq 2 ]]; then
        gate_fail G29 "binary-freshness guard could not RUN (exit 2, §11.4.3 environment SKIP) — it certified nothing; this is NOT a detected regression (see /tmp/g29-wi-binary-fresh.out)" \
            "resolve the environment blocker (sqlite3 on PATH, schema.sql present, bin/workable-items present+executable), then re-run"
    else
        gate_fail G29 "the shipped workable-items binary is STALE relative to its source — it does not carry an invariant its source defines (see /tmp/g29-wi-binary-fresh.out)" \
            "cd constitution/scripts/workable-items && go build -o bin/workable-items ./cmd/workable-items"
    fi
fi

# ---------------------------------------------------------------------------
# G30-G32 — the live-service standing guards (§11.4.135)
#
# WHY THESE THREE ARRIVE TOGETHER
# -------------------------------
# Measured 2026-08-08, and the reason this block exists: this sweep invoked 21
# entries under scripts/gates/ and ZERO of the three scripts/testing/guard_hxc*
# guards. There is no auto-discovery glob here — a guard is executed if and only
# if some caller names it — so all three had been written, reviewed, and left
# unreferenced by anything. HXC-244 was closed against a guard that had never
# run in any suite.
#
# That is precisely the §11.4.226 finding: registration is not coverage, and a
# guard nothing executes cannot fail. It is also the §11.4.135 forensic anchor
# in its purest form — a defect "fixed" with one-off runtime evidence and no
# standing check, so the next recurrence is silent. Wiring them here is what
# converts three inert files into a regression barrier.
#
# THE EXIT CONTRACT THESE THREE SHARE  (§11.4.201 / §11.4.3)
# -----------------------------------------------------------
#   0  GREEN — a LIVE subject was interrogated and carries the invariant
#   1  FAIL  — a LIVE subject was interrogated and VIOLATES it (a regression)
#   2  SKIP  — the subject was ABSENT; the guard certified nothing and says so
#
# Exit 2 is handled DIFFERENTLY here than in G21-G29, deliberately. There, exit
# 2 means an in-tree dependency was missing, which is itself a defect, so those
# gates call gate_fail. These three interrogate a RUNNING SERVICE, which is
# legitimately not deployed on every host that runs this sweep. Failing because
# the gateway is not started would be the §11.4.201 false-positive refusal — as
# forbidden as a false pass, and worse in practice, because a gate that is red
# on every developer machine gets muted and takes the real signal with it. So
# exit 2 records SKIP (the G14 docs_chain precedent): never counted as PASS,
# always printed with its reason, never counted as a failure.
#
# The SKIP branch is built not to fail open (§11.4.69 CM-NO-FAIL-OPEN-SKIP):
# each guard keys SKIP to provable absence only — curl exit 6/7 for the HTTP
# guards (with the proxy bypassed, so 7 is a fact about the TARGET), and an
# uninstalled or stopped-by-choice unit for the systemd one. A unit that is
# installed and CRASHED (ActiveState=failed) FAILs. Anything that answers,
# however badly (500, TLS failure, hang, reset, 200 with an error envelope),
# FAILs.
#
# That claim is not asserted here in prose — it is RE-RUNNABLE:
#
#     bash scripts/testing/guard_live_service_falsification.sh
#
# The battery constructs its own subjects (HTTP-200 stubs, transient systemd
# units, a deliberately crashed analyzer) and asserts the full three-way
# contract per guard, including regression cases for three holes an independent
# review found in the first revision of this wiring: a leaked proxy variable
# turning a LIVE subject into SKIP, a deployed-and-crashed unit reading as
# absence, and a crashed analyzer reporting "RED confirmed". An earlier revision
# of this comment stated a bare "Verified ... 9 FAIL, 0 SKIP" with no artifact
# behind it — exactly the uncited-claim class this block's own §11.4.226
# rationale argues against.
#
# The env vars that select each guard's SUBJECT are pinned below alongside
# RED_MODE. Without that, an exported HXC233_URL or HXC229_UNIT in a caller's
# environment silently retargets a release-wired gate at a stub — a gate that
# passes judgement on the wrong subject is not a gate.
# ---------------------------------------------------------------------------
if want_gate G30; then
    GATES_RUN=$((GATES_RUN + 1))
    gate_header "G30 — HXC-229 gateway serves in Gin RELEASE mode (live process)"
    RED_MODE=0 HXC229_UNIT=helixllm-gateway \
        bash "$ROOT/scripts/testing/guard_hxc229_gateway_release_mode.sh" \
        >/tmp/g30-hxc229-release-mode.out 2>&1
    g30_rc=$?
    if [[ "$g30_rc" -eq 0 ]]; then
        gate_pass G30 "$(head -1 /tmp/g30-hxc229-release-mode.out)"
    elif [[ "$g30_rc" -eq 2 ]]; then
        # Carry the guard's OWN reason into the durable row. A canned string here
        # asserts world-state the guard never established — an earlier revision
        # printed "not running on this host" for every SKIP cause alike.
        GATE_RESULTS+=("G30|SKIP|$(head -1 /tmp/g30-hxc229-release-mode.out) — SKIP-OK: §11.4.3")
        [[ "$QUIET" -eq 0 ]] && echo "  SKIP (G30): $(head -1 /tmp/g30-hxc229-release-mode.out) — certified nothing; this is NOT a detected regression. SKIP-OK: §11.4.3"
    else
        # Carry the guard's own line here too, not a canned sentence: this gate
        # now FAILs for release-mode recurrence AND for a crashed / crash-looping
        # unit, so a fixed string would misdescribe two of the three causes.
        gate_fail G30 "$(head -1 /tmp/g30-hxc229-release-mode.out)" \
            "if debug-mode: check GIN_MODE in the rendered unit AND in any operator .env that EnvironmentFile= loads after it, then systemctl --user daemon-reload && systemctl --user restart helixllm-gateway. If crashed/crash-looping: journalctl --user -u helixllm-gateway -n 100"
    fi
fi

if want_gate G31; then
    GATES_RUN=$((GATES_RUN + 1))
    gate_header "G31 — HXC-233 completion path returns a REAL generation (live end-to-end)"
    RED_MODE=0 HXC233_URL=https://localhost:8443/v1/chat/completions \
        HXC233_MODEL=qwen HXC233_TIMEOUT=90 \
        bash "$ROOT/scripts/testing/guard_hxc233_completion_path_live.sh" \
        >/tmp/g31-hxc233-completion.out 2>&1
    g31_rc=$?
    if [[ "$g31_rc" -eq 0 ]]; then
        gate_pass G31 "$(head -1 /tmp/g31-hxc233-completion.out)"
    elif [[ "$g31_rc" -eq 2 ]]; then
        GATE_RESULTS+=("G31|SKIP|$(head -1 /tmp/g31-hxc233-completion.out) — SKIP-OK: §11.4.3")
        [[ "$QUIET" -eq 0 ]] && echo "  SKIP (G31): $(head -1 /tmp/g31-hxc233-completion.out) — certified nothing; this is NOT a detected regression. SKIP-OK: §11.4.3"
    else
        gate_fail G31 "the product's primary capability is dead or degraded — the completion endpoint answered, but not with a real generation (see /tmp/g31-hxc233-completion.out)" \
            "verify HELIX_LLM_LOCAL_RPC_HOST/PORT in the unit point at the port the model actually serves (the default 50052 is NOT it), then restart the gateway"
    fi
fi

if want_gate G32; then
    GATES_RUN=$((GATES_RUN + 1))
    gate_header "G32 — HXC-244 health endpoint names the components it checked"
    RED_MODE=0 HXC244_URL=https://localhost:8443/internal/health HXC244_TIMEOUT=15 \
        bash "$ROOT/scripts/testing/guard_hxc244_health_components_registered.sh" \
        >/tmp/g32-hxc244-health.out 2>&1
    g32_rc=$?
    if [[ "$g32_rc" -eq 0 ]]; then
        gate_pass G32 "$(head -1 /tmp/g32-hxc244-health.out)"
    elif [[ "$g32_rc" -eq 2 ]]; then
        GATE_RESULTS+=("G32|SKIP|$(head -1 /tmp/g32-hxc244-health.out) — SKIP-OK: §11.4.3")
        [[ "$QUIET" -eq 0 ]] && echo "  SKIP (G32): $(head -1 /tmp/g32-hxc244-health.out) — certified nothing; this is NOT a detected regression. SKIP-OK: §11.4.3"
    else
        gate_fail G32 "/internal/health reports a verdict it never earned — the component list is empty or unnamed, so the endpoint answers 'healthy' no matter what is broken (see /tmp/g32-hxc244-health.out)" \
            "register the real dependency checks on the health.Checker in helix_llm cmd/helixllm/main.go (see commit 8260cf8), rebuild, and restart the gateway"
    fi
fi

# ---------------------------------------------------------------------------
# G33 — CONST-059 / §11.4.35 canonical-root inheritance clarity
#       (CM-CANONICAL-ROOT-CLARITY) + the T-P1.06.4 `.specify/memory/` extension
#
# CONST-059 names this gate; until now NOTHING implemented it — the anchor was
# restated across six governance carriers while the check itself did not exist
# (§11.4.227: a named gate must be implemented or a registered deferral).
#
# Clause (d) is the reason it was finally built. `.specify/memory/constitution.md`
# is a deliberate CONST-059 POINTER (task T-P1.06, done 2026-07-29) that SpecKit
# reads at every /speckit-plan Constitution Check. Running /speckit-constitution
# against it would overwrite the pointer with generated principles, creating a
# THIRD constitution in this repository and re-opening risk R-23. Clause (d)
# makes that mechanically detectable instead of relying on the file's own prose
# asking future agents not to do it.
#
# Fenced code blocks are stripped before matching: constitution/CLAUDE.md:91 and
# GEMINI.md:33 carry `## INHERITED FROM ...` inside ```markdown fences as
# EXAMPLES for consumers, and refusing a correct tree is a FAIL-bluff (§11.4.201).
# Paired mutation: scripts/tests/canonical_root_clarity_meta_test.sh (8 mutations,
# incl. M6 which asserts the fenced example does NOT trip the gate).
# ---------------------------------------------------------------------------
if want_gate G33; then
    GATES_RUN=$((GATES_RUN + 1))
    gate_header "G33 — CONST-059 canonical-root inheritance clarity (CM-CANONICAL-ROOT-CLARITY)"
    if bash "$ROOT/scripts/gates/canonical_root_clarity_gate.sh" >/tmp/g33-crc.out 2>&1; then
        gate_pass G33 "$(grep -oE 'checked [0-9]+ item\(s\).*' /tmp/g33-crc.out | tail -1)"
    else
        gate_fail G33 "canonical-root inheritance clarity is broken — a consumer stopped inheriting, a canonical carrier started inheriting, or .specify/memory/constitution.md stopped being a pointer (see /tmp/g33-crc.out)" \
            "$(tail -6 /tmp/g33-crc.out)"
    fi
fi

# ---------------------------------------------------------------------------
# G34 — Helix service endpoint agreement + liveness (CM-HELIX-ENDPOINT-AGREEMENT)
#
# WHY THIS EXISTS
# ---------------
# Measured 2026-09-02: `claude-providers list` showed 21 verified providers and
# NOT ONE HelixAgent or HelixLLM entry, because the Claude Toolkit's three Helix
# provider rows named ports nothing served (:18434/:18435 — really the coder and
# embeddings containers) while the services answered on :7061 and :8443. Nothing
# failed loudly: that listing filters to verified-only, verification could not
# connect, so no `*_verified.json` was written and the providers simply VANISHED.
# A dead endpoint and a never-configured provider looked identical.
#
# It was DRIFT. The tracked tree already knew the right ports — the systemd
# units declare them, the agentic challenge drives them — and only the toolkit's
# copy was wrong. One fact recorded in several places with nothing holding the
# copies equal is the gap this gate closes.
#
# TWO HALVES, DELIBERATELY INDEPENDENT
#   (A) AGREEMENT — grouped by (service, ROLE), every record of one endpoint
#       must name one port. Runs with every service DOWN, so it is enforceable
#       on a laptop. Role-grouping is load-bearing: helixagent serves its API
#       and its liveness probe on DIFFERENT ports by design, and comparing per
#       service alone flagged that correct arrangement as drift.
#   (B) LIVENESS — does the configured endpoint reach the service it names?
#
# THE THREE-STATE CONTRACT, AND WHY EXIT 2 IS A SKIP HERE
#   0  GREEN — records agree and the endpoint reaches its service
#   1  FAIL  — records contradict, or the endpoint misses a live service that
#              answers on another port (the defect)
#   2  SKIP  — nothing certifiable (no python3, no declarations)
# Like G30-G32 this interrogates RUNNING SERVICES, which a developer host
# legitimately does not run, so exit 2 records SKIP rather than a failure: a
# gate that is red on every machine gets muted and takes the real signal with
# it (§11.4.201 — a false refusal is as forbidden as a false pass). Absence is
# keyed to PROVABLE absence — the service's owner, derived from its unit's
# ExecStart rather than guessed, is not running.
#
# NO PORT IS PINNED TO A LITERAL, so deliberately moving a service cannot
# false-FAIL this gate; hardcoding the answer would relocate the drift into the
# guard. Ownership is verified from /proc (§11.4.174 — this is a shared host and
# an open port is not proof of whose it is), and body SHAPE is asserted rather
# than status code, because two ports here serve SPAs that answer 200 for any
# path. A 503 carrying a well-formed health envelope PASSES: the wire landing on
# the right service is the assertion, and service HEALTH is G30-G32's question.
#
# Polarity proof (§11.4.115): RED_MODE=1 synthesizes the pre-fix provider set on
# COPIES and asserts this same checker FAILs on it, citing the drift.
# ---------------------------------------------------------------------------
if want_gate G34; then
    GATES_RUN=$((GATES_RUN + 1))
    gate_header "G34 — Helix endpoint agreement + liveness (CM-HELIX-ENDPOINT-AGREEMENT)"
    RED_MODE=0 bash "$ROOT/scripts/gates/helix_endpoint_agreement_gate.sh" \
        >/tmp/g34-endpoint-agreement.out 2>&1
    g34_rc=$?
    if [[ "$g34_rc" -eq 0 ]]; then
        gate_pass G34 "$(grep -m1 -oE 'green=[0-9]+ skipped=[0-9]+ findings=[0-9]+ records=[0-9]+ services=[0-9]+' /tmp/g34-endpoint-agreement.out) — every recorded endpoint agrees across sources and reaches its service (ownership verified from /proc, body shape asserted)"
    elif [[ "$g34_rc" -eq 2 ]]; then
        # Carry the gate's OWN reason, never a canned sentence: a fixed string
        # would assert world-state the gate never established.
        GATE_RESULTS+=("G34|SKIP|$(grep -m1 'SKIP —' /tmp/g34-endpoint-agreement.out) — SKIP-OK: §11.4.3")
        [[ "$QUIET" -eq 0 ]] && echo "  SKIP (G34): $(grep -m1 'SKIP —' /tmp/g34-endpoint-agreement.out) — certified nothing; this is NOT a detected regression. SKIP-OK: §11.4.3"
    else
        gate_fail G34 "a configured Helix endpoint has drifted — records contradict each other, or an endpoint does not reach the service it names while that service answers elsewhere (see /tmp/g34-endpoint-agreement.out)" \
            "$(grep 'FAIL:' /tmp/g34-endpoint-agreement.out | head -4)"
    fi
fi

# ---------------------------------------------------------------------------
# G35 — HXC-168 no hardcoded DB credential + loopback DB bind
#       (CONST-042 / Article XII §12.1 / §11.4.135)
#
# WHY THIS EXISTS
# ---------------
# scripts/gates/no_hardcoded_db_credential_gate.sh has existed since 11861996 and
# was invoked by NOTHING. Grepping the tree on 2026-09-08 found it referenced only
# by its own source, an Issues entry describing it, and one Go test — no sweep, no
# hook, no Makefile target. A guard nobody runs is a guard that cannot catch a
# regression, which is the §11.4.226 result restated: it is the EVIDENCE CLASS at
# the seam that predicts whether a fix holds, and an unwired gate produces none.
#
# The gate itself is falsifiable in both directions — `--self-test` runs eight
# paired §1.1 mutations, including one that plants a copy-pasteable credential line
# in a .md (must FAIL) alongside incident prose naming the same value (must PASS),
# and one that plants a wildcard port publish (must FAIL) beside a loopback publish
# (must PASS). Wiring it here is what makes that falsifiability load-bearing.
#
# HONEST BOUNDARY (§11.4.6): green here means no tracked file HANDS OVER a
# historical credential in a copy-pasteable line and every scoped database port
# binds loopback. It does NOT mean the credential is safe. The value is in git
# history on four mirrors and REMAINS VALID until it is rotated at the database;
# no gate can withdraw it.
# ---------------------------------------------------------------------------
if want_gate G35; then
    GATES_RUN=$((GATES_RUN + 1))
    gate_header "G35 — HXC-168 no hardcoded DB credential + loopback DB bind (CONST-042)"
    if bash "$ROOT/scripts/gates/no_hardcoded_db_credential_gate.sh" >/tmp/g35-dbcred.out 2>&1; then
        gate_pass G35 "no tracked file hands over a historical DB credential and every scoped DB port publish binds 127.0.0.1 — NOTE: this does not rotate the published value"
    else
        gate_fail G35 "a database credential literal is back in tracked config/source, or a database port is published on the wildcard address (see /tmp/g35-dbcred.out)" \
            "$(grep -E '^(FAIL|  )' /tmp/g35-dbcred.out | head -6)"
    fi
fi

# ---------------------------------------------------------------------------
# G36 — HXC-168 standing regression guard (§11.4.135 / §11.4.115)
#
# WHY THIS EXISTS
# ---------------
# scripts/tests/hxc168_db_exposure_red_test.sh was authored as the §11.4.115
# polarity test for HXC-168 and, like the gate above before G35, was invoked by
# NOTHING — a tree-wide grep on 2026-09-08 returned zero references outside its
# own source. §11.4.135 requires a closed defect's guard to be REGISTERED in the
# standing suite, not merely to exist; an unregistered guard cannot catch the
# recurrence it was written for.
#
# It is NOT a duplicate of G35. G35 checks tracked SOURCE. This runs the same
# class against the RUNNING artifact too (§11.4.108 layer 3), which is how the
# two disagreed at review time: G35 was green while a wildcard listener was live.
#
# HONEST BOUNDARY (§11.4.6): the runtime leg distinguishes three states, not
# two. A listener that is non-loopback while its SOURCE already binds 127.0.0.1
# is reported OPERATOR-BLOCKED and does not fail the gate — the running
# container predates the fix and podman cannot rebind a published port in place,
# so it closes only on an operator-gated recreate. Calling that a regression
# would be false (§11.4.6); passing it silently would be the false-null this
# test exists to catch (§11.4.21). If the source does NOT declare loopback, it
# is a finding and the gate FAILS — verified by a paired §1.1 mutation that
# reverts the source publish to the wildcard and turns the report from
# OPERATOR-BLOCKED into two findings.
# ---------------------------------------------------------------------------
if want_gate G36; then
    GATES_RUN=$((GATES_RUN + 1))
    gate_header "G36 — HXC-168 standing regression guard (RED_MODE=0)"
    if RED_MODE=0 bash "$ROOT/scripts/tests/hxc168_db_exposure_red_test.sh" >/tmp/g36-hxc168.out 2>&1; then
        gate_pass G36 "the HXC-168 exposure class has not regressed in source or at runtime — see /tmp/g36-hxc168.out for any OPERATOR-BLOCKED items, which are source-fixed but not yet live and are NOT certified closed"
    else
        gate_fail G36 "the HXC-168 exposure class has regressed (see /tmp/g36-hxc168.out)" \
            "$(grep -E 'FOUND|GREEN FAIL' /tmp/g36-hxc168.out | head -6)"
    fi
fi

# ---------------------------------------------------------------------------
# Summary
# ---------------------------------------------------------------------------
echo
echo "=== verify-all-constitution-rules.sh summary ==="
echo "Gates run: $GATES_RUN"
echo "Failures:  $FAILURES"
if [[ "$QUIET" -eq 0 ]]; then
    for line in "${GATE_RESULTS[@]}"; do
        IFS='|' read -r id status desc <<< "$line"
        printf "  %s  %-4s  %s\n" "$id" "$status" "$desc"
    done
fi
echo
if [[ "$FAILURES" -gt 0 ]]; then
    echo "FAIL: $FAILURES gate(s) violate the constitution"
    exit 1
fi
echo "PASS: every implementable gate green — anti-bluff covenant honoured"
exit 0
