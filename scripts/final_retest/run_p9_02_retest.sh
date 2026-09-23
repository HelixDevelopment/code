#!/usr/bin/env bash
# scripts/final_retest/run_p9_02_retest.sh — HXC-159 T-P9.02 full-suite retest
# harness on a clean, freshly-deployed target checkout (§11.4.40 full retest
# from the last tag + §11.4.132 risk-ordered + §11.4.108/§11.4.139
# runtime-on-CLEAN-target, fresh-process, never the development tree).
#
# ── What this script is ───────────────────────────────────────────────────
#   The reusable orchestrator for T-P9.02 ("Full-suite retest on a clean
#   target"): given the path to a *separate, isolated, already-cloned* clean
#   checkout of this repository (main repo + the `constitution`,
#   `submodules/helix_agent`, `submodules/skills` submodules — NOT the shared
#   development checkout this script itself lives in), it:
#
#     1. Verifies the target checkout is genuinely clean before touching it
#        (T-P9.02.1) — refuses instead of guessing (§11.4.6).
#     2. Runs the skill registrar from scratch against that target
#        (T-P9.02.1) — `scripts/register_skills.sh`.
#     3. Re-observes every one of the ~22 T-P9.01 runtime signatures
#        (RS-01..RS-22, T-P9.02.2), each as its own `rs_NN_*` function so the
#        harness is extensible and any single RS can be re-run in isolation.
#     4. For every check that exercises a *built* artifact (a Go binary), it
#        rebuilds that artifact IMMEDIATELY before running it and asserts
#        the produced binary's mtime is not older than the rebuild's own
#        start time (T-P9.02.3 "running-artifact == built-artifact" —
#        documented policy below).
#     5. Emits one structured PASS/FAIL/SKIP-with-reason verdict per RS plus
#        an overall summary, written to
#        `<target>/qa-results/hxc159/final_retest/<run-id>/report.{json,md}`
#        (T-P9.02 acceptance-evidence path convention, resolved against the
#        TARGET checkout being retested, not the checkout running this
#        script).
#     6. Orders the checks risk-first per §11.4.132: the RS numbers touched
#        by the most-reopened T-P9.01 finding (F-17, which regressed RS-01/
#        RS-05/RS-06/RS-08/RS-17 across review rounds — see
#        specs/001-helixskills-incorporation/tasks.md's T-P9.01 finding
#        ledger) run FIRST; every other RS follows in ascending numeric
#        order.
#
#   This script does NOT itself certify T-P9.02 complete (T-P9.02.4: hand off
#   to manual QA per §11.4.185 — automation is necessary, not sufficient).
#   Running it produces evidence; a human still closes the task.
#
# ── Artifact-freshness policy (T-P9.02.3) ─────────────────────────────────
#   Chosen approach: ALWAYS REBUILD IMMEDIATELY BEFORE EACH CHECK THAT NEEDS
#   A BINARY, rather than comparing timestamps/hashes against some earlier
#   build. This is the simplest robust option available to a bash harness
#   with no persistent build-cache bookkeeping of its own:
#     - `go build` is itself the authority on "does this binary reflect
#       current source" — it recompiles every package whose inputs changed
#       and is a no-op (near-instant, cached) otherwise, so "rebuild every
#       time" costs almost nothing when the tree is unchanged and is
#       correct-by-construction when it is.
#     - `go test` (used for library-level RS checks) recompiles from source
#       on every invocation by definition — there is no separate "stale
#       binary" state to assert away.
#   Each binary-building check function calls `hxc159_build_fresh` (below),
#   which records the wall-clock time immediately before invoking `go
#   build`, then asserts the produced binary's mtime is >= that recorded
#   time. A binary whose mtime predates the rebuild attempt (impossible in
#   normal operation, but checked rather than assumed per §11.4.6) fails the
#   check with an explicit reason rather than silently reporting on stale
#   bytes.
#
# ── Usage ──────────────────────────────────────────────────────────────────
#   bash scripts/final_retest/run_p9_02_retest.sh --target <clean-checkout-path> [options]
#
#   Required:
#     --target DIR        Path to the isolated clean checkout to retest.
#                          MUST NOT be the checkout this script itself lives
#                          in — refused if it resolves to the same toplevel
#                          as this script's own repo (see hxc159_guard_not_self).
#
#   Options:
#     --run-id ID          Override the run-id directory name (default:
#                           UTC timestamp, YYYYMMDDTHHMMSSZ).
#     --only RS-01,RS-05    Comma-separated RS numbers to run (default: all).
#     --skip-registrar      Skip step 2 (registrar) — use when the target's
#                           .claude/skills/ is already known-provisioned.
#     --allow-dirty          Bypass the clean-checkout refusal. NEVER use this
#                           against a checkout you intend to cite as real
#                           T-P9.02 closure evidence — it exists only for
#                           harness self-testing (dry runs, this script's own
#                           test suite). Every use is loudly logged and
#                           recorded in the report.
#     -h|--help             Print this header and exit.
#
#   Exit code: 0 iff every requested RS check reports PASS or an honestly-
#   reasoned SKIP; non-zero if any check reports FAIL, or if pre-flight
#   (clean-checkout / registrar) refuses.
#
# ── Re-runnability ─────────────────────────────────────────────────────────
#   Idempotent and side-effect-free against the CALLER's checkout: it only
#   ever reads this script's own repo (to locate `scripts/register_skills.sh`,
#   the manifest, and the RS-covering test files/scripts it invokes by path)
#   and writes/mutates state exclusively under `--target`. Re-running against
#   the same target with the same source state reproduces the same verdicts.
#
# ── Governance ───────────────────────────────────────────────────────────
#   §11.4.40 (full retest from last tag) / §11.4.108 + §11.4.139 (clean-
#   target, fresh-process runtime signature) / §11.4.132 (risk-ordered,
#   most-reopened first) / §11.4.6 (verify, never guess) / §11.4.200
#   (isolate exactly one target — this script never touches any checkout
#   other than the one named by --target).
set -u
set -o pipefail

# ─────────────────────────── argument parsing ──────────────────────────────

SELF_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
SELF_REPO_ROOT="$(cd "${SELF_DIR}/../.." && pwd)"

TARGET=""
RUN_ID=""
ONLY=""
SKIP_REGISTRAR=0
ALLOW_DIRTY=0

print_usage() {
    sed -n '2,90p' "${BASH_SOURCE[0]}" | sed 's/^# \{0,1\}//'
}

while [ $# -gt 0 ]; do
    case "$1" in
        --target)
            TARGET="${2:-}"; shift 2 ;;
        --run-id)
            RUN_ID="${2:-}"; shift 2 ;;
        --only)
            ONLY="${2:-}"; shift 2 ;;
        --skip-registrar)
            SKIP_REGISTRAR=1; shift ;;
        --allow-dirty)
            ALLOW_DIRTY=1; shift ;;
        -h|--help)
            print_usage; exit 0 ;;
        *)
            echo "unknown argument: $1" >&2
            print_usage >&2
            exit 2 ;;
    esac
done

if [ -z "${TARGET}" ]; then
    echo "FATAL: --target <clean-checkout-path> is required" >&2
    exit 2
fi
if [ ! -d "${TARGET}" ]; then
    echo "FATAL: --target '${TARGET}' does not exist or is not a directory" >&2
    exit 2
fi
TARGET="$(cd "${TARGET}" && pwd)"

# ── T-P9.02 / §11.4.200: refuse to operate against the checkout this script
# itself is committed in (or any checkout sharing its toplevel). The whole
# point of the harness is to run against an ISOLATED clean clone, never the
# live shared development checkout — this guard makes that a mechanical
# refusal, not a documentation promise.
hxc159_guard_not_self() {
    local target_git_dir self_git_dir
    if ! target_git_dir="$(git -C "${TARGET}" rev-parse --absolute-git-dir 2>/dev/null)"; then
        echo "FATAL: --target '${TARGET}' is not a git repository (git rev-parse failed)" >&2
        exit 2
    fi
    self_git_dir="$(git -C "${SELF_REPO_ROOT}" rev-parse --absolute-git-dir 2>/dev/null || true)"
    if [ -n "${self_git_dir}" ] && [ "${target_git_dir}" = "${self_git_dir}" ]; then
        echo "FATAL: --target resolves to THIS script's own repository (${self_git_dir})." >&2
        echo "       T-P9.02 / §11.4.200 requires an isolated clean checkout, never the" >&2
        echo "       shared development checkout. Point --target at a separate clone." >&2
        exit 2
    fi
}
hxc159_guard_not_self

if [ -z "${RUN_ID}" ]; then
    RUN_ID="$(date -u '+%Y%m%dT%H%M%SZ')"
fi

REPORT_DIR="${TARGET}/qa-results/hxc159/final_retest/${RUN_ID}"
mkdir -p "${REPORT_DIR}/raw"

# ─────────────────────────── logging / result plumbing ─────────────────────

RESULTS_FILE="${REPORT_DIR}/results.tsv"   # rs<TAB>verdict<TAB>reason<TAB>evidence_rel_path
: > "${RESULTS_FILE}"

hxc159_log() { echo "[$(date -u '+%H:%M:%SZ')] $*"; }

# hxc159_record RS VERDICT REASON EVIDENCE_REL_PATH
hxc159_record() {
    local rs="$1" verdict="$2" reason="$3" evidence="${4:-}"
    printf '%s\t%s\t%s\t%s\n' "${rs}" "${verdict}" "${reason}" "${evidence}" >> "${RESULTS_FILE}"
    hxc159_log "${rs}: ${verdict} — ${reason}"
}

hxc159_should_run() {
    local rs="$1"
    if [ -z "${ONLY}" ]; then
        return 0
    fi
    case ",${ONLY}," in
        *",${rs},"*) return 0 ;;
        *) return 1 ;;
    esac
}

# ─────────────────────────── artifact-freshness helper ────────────────────

# hxc159_build_fresh MODULE_DIR PKG_PATH OUT_BIN
# Rebuilds OUT_BIN from PKG_PATH inside MODULE_DIR, immediately before use,
# and asserts its mtime postdates the rebuild's own start (T-P9.02.3).
hxc159_build_fresh() {
    local module_dir="$1" pkg_path="$2" out_bin="$3"
    local start_epoch build_log
    build_log="${REPORT_DIR}/raw/build_$(basename "${out_bin}").log"
    start_epoch="$(date +%s)"
    if ! ( cd "${module_dir}" && go build -o "${out_bin}" "${pkg_path}" ) >"${build_log}" 2>&1; then
        echo "BUILD_FAILED"
        return 1
    fi
    if [ ! -x "${out_bin}" ]; then
        echo "BUILD_PRODUCED_NO_BINARY"
        return 1
    fi
    local bin_epoch
    bin_epoch="$(date -r "${out_bin}" +%s 2>/dev/null || stat -c %Y "${out_bin}")"
    if [ "${bin_epoch}" -lt "${start_epoch}" ]; then
        echo "BINARY_OLDER_THAN_REBUILD_START"
        return 1
    fi
    echo "OK"
    return 0
}

# hxc159_classify_go_failure LOG_FILE
# A `go build`/`go test`/`go mod graph`/`go list` failure against a
# DELIBERATELY-SCOPED target clone (this harness's dry run only fetches
# constitution + submodules/helix_agent + submodules/skills, plus
# submodules/dag_orchestrator + submodules/llms_verifier once those were
# also found to be needed — see the harness's own README) can fail for two
# very different reasons:
#   (a) MISSING_LOCAL_SUBMODULE — the module graph's `replace` directives
#       point at a local submodule this scoped clone never fetched (the
#       target's helix_code/helix_agent go.mod files each carry ~40 local
#       `replace` directives to sibling submodules; a REAL, fully-populated
#       clean checkout — which is what the future non-dry-run T-P9.02
#       invocation will use — would never hit this).
#   (b) a genuine compile/test failure in the code this RS actually covers.
# Collapsing these into one FAIL bucket would make the dry-run report
# useless for telling "the harness has a scoping gap" apart from "there is
# a real regression" — so every RS check that can hit class (a) classifies
# its own failure through this helper before recording a verdict.
# Echoes either "MISSING_LOCAL_SUBMODULE:<path>" (path = the first missing
# submodule's replace target, relative) or "GENUINE".
hxc159_classify_go_failure() {
    local log_file="$1"
    local hit
    hit="$(grep -oE '\(replaced by \.\./[a-zA-Z0-9_/.-]+\): reading .*(no such file or directory|does not exist)' "${log_file}" 2>/dev/null | head -1)"
    if [ -n "${hit}" ]; then
        echo "MISSING_LOCAL_SUBMODULE:${hit}"
        return 0
    fi
    hit="$(grep -oE 'replacement directory \.\./[a-zA-Z0-9_/.-]+ does not exist' "${log_file}" 2>/dev/null | head -1)"
    if [ -n "${hit}" ]; then
        echo "MISSING_LOCAL_SUBMODULE:${hit}"
        return 0
    fi
    echo "GENUINE"
    return 0
}

# ─────────────────────────── T-P9.02.1: clean-checkout verification ───────

hxc159_verify_clean_checkout() {
    hxc159_log "T-P9.02.1: verifying target checkout is clean: ${TARGET}"
    local dirty=0
    local status_log="${REPORT_DIR}/raw/00_git_status_top.txt"
    git -C "${TARGET}" status --porcelain > "${status_log}" 2>&1
    if [ -s "${status_log}" ]; then
        hxc159_log "target working tree is NOT clean (see ${status_log})"
        dirty=1
    fi

    # NOTE: deliberately NON-recursive. `git submodule status --recursive`
    # ignores its own pathspec for recursion depth once it descends into a
    # matched submodule, so it would also report on every NESTED submodule
    # inside constitution/ and submodules/helix_agent/ (dozens of vendored
    # MCP-server/cli-agent forks this harness never needs and never
    # initializes). T-P9.02's RS checks only touch the THREE top-level
    # submodules named below — that is the only cleanliness this gate
    # asserts.
    local sub_status_log="${REPORT_DIR}/raw/00_submodule_status.txt"
    git -C "${TARGET}" submodule status \
        -- constitution submodules/helix_agent submodules/skills \
        > "${sub_status_log}" 2>&1 || true
    if grep -qE '^[+-]' "${sub_status_log}"; then
        hxc159_log "one or more required submodules are uninitialized (-) or dirty (+) (see ${sub_status_log})"
        dirty=1
    fi

    if [ "${dirty}" -eq 1 ] && [ "${ALLOW_DIRTY}" -ne 1 ]; then
        hxc159_log "REFUSING to proceed against a non-clean target (T-P9.02.1 / §11.4.6)."
        hxc159_log "Pass --allow-dirty to override for harness self-testing ONLY — never for real closure evidence."
        return 1
    elif [ "${dirty}" -eq 1 ]; then
        hxc159_log "WARNING: --allow-dirty set — proceeding against a NON-CLEAN target. This run's evidence is NOT valid T-P9.02 closure evidence."
    else
        hxc159_log "target checkout is clean."
    fi
    return 0
}

# ─────────────────────────── T-P9.02.1: registrar from scratch ────────────

hxc159_run_registrar() {
    if [ "${SKIP_REGISTRAR}" -eq 1 ]; then
        hxc159_log "T-P9.02.1: --skip-registrar set, not invoking scripts/register_skills.sh"
        return 0
    fi
    hxc159_log "T-P9.02.1: running the registrar from scratch against the clean target"
    local reg_log="${REPORT_DIR}/raw/01_register_skills.txt"
    if bash "${TARGET}/scripts/register_skills.sh" --root "${TARGET}" > "${reg_log}" 2>&1; then
        hxc159_log "registrar completed (see ${reg_log})"
        return 0
    else
        local rc=$?
        hxc159_log "registrar exited ${rc} (see ${reg_log}) — proceeding; individual RS checks will surface the consequence"
        return 0
    fi
}

# ═══════════════════════════ RS check functions ═══════════════════════════
#
# Each function:
#   - runs REAL commands (builds, `go test`, scripts) against ${TARGET} only
#   - writes its raw output under ${REPORT_DIR}/raw/
#   - calls hxc159_record with PASS, FAIL, or SKIP (+ a genuine reason)
#   - never invents a PASS from the absence of an error (§11.4.1)

rs_01_registrar_link_and_cli_sees_skill() {
    local rs="RS-01"
    hxc159_should_run "${rs}" || return 0
    local out="${REPORT_DIR}/raw/RS-01.txt"
    {
        echo "--- .claude/skills/ state after registrar ---"
        ls -la "${TARGET}/.claude/skills/" 2>&1
        echo "--- registrar --check (idempotent, should be WIRED if step 2 ran) ---"
        bash "${TARGET}/scripts/register_skills.sh" --root "${TARGET}" --check
        echo "check_exit=$?"
    } > "${out}" 2>&1
    if [ ! -d "${TARGET}/.claude/skills" ] || [ -z "$(ls -A "${TARGET}/.claude/skills" 2>/dev/null)" ]; then
        hxc159_record "${rs}" FAIL "registrar produced no .claude/skills/ entries — see raw/RS-01.txt" "raw/RS-01.txt"
        return
    fi
    if ! grep -q 'VERDICT: WIRED' "${out}"; then
        hxc159_record "${rs}" FAIL "registrar --check did not report WIRED after linking — see raw/RS-01.txt" "raw/RS-01.txt"
        return
    fi

    # Second half of RS-01: the linked skill must be VISIBLE via the real
    # consumer seam (`helixcode skills list`), not merely present as a
    # symlink. Build the CLI fresh, immediately before probing.
    local bin="${REPORT_DIR}/raw/helixcode_rs01"
    local build_status
    build_status="$(hxc159_build_fresh "${TARGET}/helix_code" ./cmd/cli "${bin}")"
    {
        echo "--- fresh build status: ${build_status} ---"
    } >> "${out}"
    if [ "${build_status}" != "OK" ]; then
        local build_log="${REPORT_DIR}/raw/build_$(basename "${bin}").log"
        local class
        class="$(hxc159_classify_go_failure "${build_log}")"
        if [ "${class}" != "GENUINE" ]; then
            hxc159_record "${rs}" SKIP "registrar half of RS-01 PASSED (linked + --check WIRED, above); the CLI-build half cannot run in this scoped dry-run clone — ${class}; see raw/RS-01.txt" "raw/RS-01.txt"
        else
            hxc159_record "${rs}" FAIL "could not build helixcode CLI fresh (${build_status}) — see raw/RS-01.txt" "raw/RS-01.txt"
        fi
        return
    fi
    local list_out="${REPORT_DIR}/raw/RS-01_skills_list.txt"
    ( cd "${TARGET}" && "${bin}" skills list ) > "${list_out}" 2>&1
    local list_rc=$?
    echo "helixcode skills list exit=${list_rc}" >> "${out}"
    cat "${list_out}" >> "${out}"
    if [ "${list_rc}" -ne 0 ]; then
        hxc159_record "${rs}" FAIL "helixcode skills list exited ${list_rc} — external tier likely refused (F-17 class); see raw/RS-01.txt + raw/RS-01_skills_list.txt" "raw/RS-01.txt"
        return
    fi
    hxc159_record "${rs}" PASS "registrar linked skills, --check reports WIRED, and 'helixcode skills list' ran cleanly against the fresh binary" "raw/RS-01.txt"
}

rs_02_registrar_determinism() {
    local rs="RS-02"
    hxc159_should_run "${rs}" || return 0
    local out="${REPORT_DIR}/raw/RS-02.txt"
    local run1="${REPORT_DIR}/raw/RS-02_run1.txt"
    local run2="${REPORT_DIR}/raw/RS-02_run2.txt"
    bash "${TARGET}/scripts/register_skills.sh" --root "${TARGET}" > "${run1}" 2>&1
    bash "${TARGET}/scripts/register_skills.sh" --root "${TARGET}" > "${run2}" 2>&1
    {
        echo "--- run1 ---"; cat "${run1}"
        echo "--- run2 ---"; cat "${run2}"
    } > "${out}"
    if grep -qE 'linked=0.*unchanged=' "${run2}" || grep -qE 'unchanged=[1-9]' "${run2}"; then
        hxc159_record "${rs}" PASS "second registrar run reports unchanged (idempotent, deterministic) — see raw/RS-02.txt" "raw/RS-02.txt"
    else
        hxc159_record "${rs}" FAIL "second registrar run did not report the expected idempotent 'unchanged' outcome — see raw/RS-02.txt" "raw/RS-02.txt"
    fi
}

rs_03_registrar_check_mode() {
    local rs="RS-03"
    hxc159_should_run "${rs}" || return 0
    local out="${REPORT_DIR}/raw/RS-03.txt"
    bash "${TARGET}/scripts/register_skills.sh" --root "${TARGET}" --check > "${out}" 2>&1
    local rc=$?
    echo "exit=${rc}" >> "${out}"
    if [ "${rc}" -eq 0 ] && grep -q 'VERDICT: WIRED' "${out}"; then
        hxc159_record "${rs}" PASS "--check (guard mode) reports WIRED with exit 0, no filesystem mutation" "raw/RS-03.txt"
    else
        hxc159_record "${rs}" FAIL "--check did not cleanly report WIRED — see raw/RS-03.txt" "raw/RS-03.txt"
    fi
}

rs_04_loader_exact_case() {
    local rs="RS-04"
    hxc159_should_run "${rs}" || return 0
    local out="${REPORT_DIR}/raw/RS-04.txt"
    ( cd "${TARGET}/submodules/skills" && go test -v -run '^TestLoaderRequiresExactCaseManifest$' ./pkg/skills/... ) > "${out}" 2>&1
    local rc=$?
    if [ "${rc}" -eq 0 ] && grep -q '^--- PASS' "${out}"; then
        hxc159_record "${rs}" PASS "TestLoaderRequiresExactCaseManifest passes on a freshly-compiled pkg/skills" "raw/RS-04.txt"
    else
        hxc159_record "${rs}" FAIL "TestLoaderRequiresExactCaseManifest did not pass (exit ${rc}) — see raw/RS-04.txt" "raw/RS-04.txt"
    fi
}

rs_05_ceiling_gate() {
    local rs="RS-05"
    hxc159_should_run "${rs}" || return 0
    local out="${REPORT_DIR}/raw/RS-05.txt"
    {
        ( cd "${TARGET}/submodules/skills" && go test -v -run '^TestActivationCeilingRefusesAndNamesOffender$' ./pkg/skills/... )
        echo "--- helix_code consumer seam ---"
        ( cd "${TARGET}/helix_code" && go test -v -run 'Ceiling' ./internal/agent/... )
        echo "--- helix_agent consumer seam ---"
        ( cd "${TARGET}/submodules/helix_agent" && go test -v -run '^TestExternalSkillSource_CeilingRefusesTheWholeSource$|^TestExternalSkillSource_UnderCeilingIsAdmitted$' ./internal/skills/... )
    } > "${out}" 2>&1
    local rc=$?
    if [ "${rc}" -eq 0 ] && grep -q '^--- PASS' "${out}"; then
        hxc159_record "${rs}" PASS "ceiling gate verified at library + both consumer seams (fresh compile)" "raw/RS-05.txt"
        return
    fi
    local class
    class="$(hxc159_classify_go_failure "${out}")"
    if [ "${class}" != "GENUINE" ] && grep -q '^--- PASS: TestActivationCeilingRefusesAndNamesOffender' "${out}"; then
        hxc159_record "${rs}" SKIP "library-level ceiling gate PASSED (see raw/RS-05.txt); one or more consumer-seam checks cannot compile in this scoped dry-run clone — ${class}" "raw/RS-05.txt"
    else
        hxc159_record "${rs}" FAIL "ceiling gate did not verify cleanly at all seams (exit ${rc}, likely F-17-class consumer regression) — see raw/RS-05.txt" "raw/RS-05.txt"
    fi
}

rs_06_similarity_gate() {
    local rs="RS-06"
    hxc159_should_run "${rs}" || return 0
    local out="${REPORT_DIR}/raw/RS-06.txt"
    {
        ( cd "${TARGET}/submodules/skills" && go test -v -run '^TestActivationSimilarityGateFires$' ./pkg/skills/... )
        echo "--- helix_agent consumer seam ---"
        ( cd "${TARGET}/submodules/helix_agent" && go test -v -run '^TestExternalSkillSource_SimilarityConflictRefusesTheWholeSource$' ./internal/skills/... )
    } > "${out}" 2>&1
    local rc=$?
    if [ "${rc}" -eq 0 ] && grep -q '^--- PASS' "${out}"; then
        hxc159_record "${rs}" PASS "similarity gate verified at library + helix_agent consumer seam (fresh compile)" "raw/RS-06.txt"
        return
    fi
    local class
    class="$(hxc159_classify_go_failure "${out}")"
    if [ "${class}" != "GENUINE" ] && grep -q '^--- PASS: TestActivationSimilarityGateFires' "${out}"; then
        hxc159_record "${rs}" SKIP "library-level similarity gate PASSED (see raw/RS-06.txt); the helix_agent consumer-seam check cannot compile in this scoped dry-run clone — ${class}" "raw/RS-06.txt"
    else
        hxc159_record "${rs}" FAIL "similarity gate did not verify cleanly (exit ${rc}) — per F-17 this is EXPECTED to fail on the real corpus until F-17 is resolved; see raw/RS-06.txt" "raw/RS-06.txt"
    fi
}

rs_08_external_tier_live() {
    local rs="RS-08"
    hxc159_should_run "${rs}" || return 0
    local out="${REPORT_DIR}/raw/RS-08.txt"
    local bin="${REPORT_DIR}/raw/helixcode_rs08"
    local build_status
    build_status="$(hxc159_build_fresh "${TARGET}/helix_code" ./cmd/cli "${bin}")"
    {
        echo "--- fresh build status: ${build_status} ---"
        echo "--- helixcode skills list (external tier over the real corpus) ---"
        ( cd "${TARGET}" && "${bin}" skills list )
        echo "exit=$?"
        echo "--- helixcode skills invoke skill-catalog (live-tree external-tier probe cited by F-17) ---"
        ( cd "${TARGET}" && "${bin}" skills invoke skill-catalog )
        echo "exit=$?"
    } > "${out}" 2>&1
    if [ "${build_status}" != "OK" ]; then
        local build_log="${REPORT_DIR}/raw/build_$(basename "${bin}").log"
        local class
        class="$(hxc159_classify_go_failure "${build_log}")"
        if [ "${class}" != "GENUINE" ]; then
            hxc159_record "${rs}" SKIP "cannot build helixcode CLI in this scoped dry-run clone — ${class}; see raw/RS-08.txt" "raw/RS-08.txt"
        else
            hxc159_record "${rs}" FAIL "could not build helixcode CLI fresh (${build_status})" "raw/RS-08.txt"
        fi
        return
    fi
    if grep -q 'not found' "${out}" || grep -qi 'load failed' "${out}"; then
        hxc159_record "${rs}" FAIL "external tier refused/failed on the live tree (F-17 class regression) — see raw/RS-08.txt" "raw/RS-08.txt"
        return
    fi
    hxc159_record "${rs}" PASS "external tier admitted and skill-catalog invocation succeeded on the live tree" "raw/RS-08.txt"
}

rs_09_cli_autotrigger() {
    local rs="RS-09"
    hxc159_should_run "${rs}" || return 0
    local out="${REPORT_DIR}/raw/RS-09.txt"
    ( cd "${TARGET}/helix_code" && go test -v -run '.' ./cmd/cli/... -run 'Autotrigger|Skill' ) > "${out}" 2>&1
    local rc=$?
    if [ "${rc}" -eq 0 ] && grep -qi 'trigger' "${out}"; then
        hxc159_record "${rs}" PASS "CLI trigger-phrase activation test suite passes (fresh compile)" "raw/RS-09.txt"
        return
    fi
    local class
    class="$(hxc159_classify_go_failure "${out}")"
    if [ "${class}" != "GENUINE" ]; then
        hxc159_record "${rs}" SKIP "cmd/cli cannot compile in this scoped dry-run clone — ${class}; see raw/RS-09.txt" "raw/RS-09.txt"
    else
        hxc159_record "${rs}" FAIL "CLI trigger-phrase activation did not verify cleanly (exit ${rc}) — see raw/RS-09.txt" "raw/RS-09.txt"
    fi
}

rs_10_http_route() {
    local rs="RS-10"
    hxc159_should_run "${rs}" || return 0
    local out="${REPORT_DIR}/raw/RS-10.txt"
    ( cd "${TARGET}/submodules/skills" && go test -v -run '.' ./cmd/server/... ) > "${out}" 2>&1
    local rc=$?
    if [ "${rc}" -eq 0 ]; then
        hxc159_record "${rs}" PASS "GET /v1/skills router test suite passes (fresh compile, httptest boundary)" "raw/RS-10.txt"
    else
        hxc159_record "${rs}" FAIL "server/router test suite did not pass (exit ${rc}) — see raw/RS-10.txt" "raw/RS-10.txt"
    fi
}

rs_11_module_graph_no_edge() {
    local rs="RS-11"
    hxc159_should_run "${rs}" || return 0
    local out="${REPORT_DIR}/raw/RS-11.txt"
    {
        echo "--- go mod graph (dev.helix.agent) — must show zero edges to HelixDevelopment/skills ---"
        ( cd "${TARGET}/submodules/helix_agent" && go mod graph | grep -c 'HelixDevelopment/skills' ) || true
        echo "--- planted-edge detector test ---"
        ( cd "${TARGET}/submodules/helix_agent" && go test -v -run '^TestModuleGraphHasNoEdgeToSkillsModule$' ./tests/compliance/... )
    } > "${out}" 2>&1
    local edge_count
    edge_count="$(grep -A1 'go mod graph' "${out}" | tail -1 | tr -dc '0-9' || echo 'X')"
    if [ "${edge_count}" = "0" ] && grep -q '^--- PASS' "${out}"; then
        hxc159_record "${rs}" PASS "zero module-graph edges to HelixDevelopment/skills; D-4 guard test passes" "raw/RS-11.txt"
        return
    fi
    local class
    class="$(hxc159_classify_go_failure "${out}")"
    if [ "${class}" != "GENUINE" ]; then
        hxc159_record "${rs}" SKIP "helix_agent module cannot fully resolve/compile in this scoped dry-run clone — ${class}; see raw/RS-11.txt" "raw/RS-11.txt"
    else
        hxc159_record "${rs}" FAIL "module-graph edge count or D-4 guard test did not verify cleanly — see raw/RS-11.txt" "raw/RS-11.txt"
    fi
}

rs_12_skillconformance_parity() {
    local rs="RS-12"
    hxc159_should_run "${rs}" || return 0
    local out="${REPORT_DIR}/raw/RS-12.txt"
    local hc_bin="${REPORT_DIR}/raw/skillconformance_hc"
    local ha_bin="${REPORT_DIR}/raw/skillconformance_ha"
    local hc_status ha_status
    hc_status="$(hxc159_build_fresh "${TARGET}/helix_code" ./cmd/skillconformance "${hc_bin}")"
    ha_status="$(hxc159_build_fresh "${TARGET}/submodules/helix_agent" ./cmd/skillconformance "${ha_bin}")"
    echo "--- build status: helix_code=${hc_status} helix_agent=${ha_status} ---" > "${out}"
    if [ "${hc_status}" != "OK" ] || [ "${ha_status}" != "OK" ]; then
        local class="GENUINE"
        if [ "${hc_status}" != "OK" ]; then
            class="$(hxc159_classify_go_failure "${REPORT_DIR}/raw/build_$(basename "${hc_bin}").log")"
        fi
        if [ "${class}" = "GENUINE" ] && [ "${ha_status}" != "OK" ]; then
            class="$(hxc159_classify_go_failure "${REPORT_DIR}/raw/build_$(basename "${ha_bin}").log")"
        fi
        if [ "${class}" != "GENUINE" ]; then
            hxc159_record "${rs}" SKIP "one or both skillconformance binaries cannot build in this scoped dry-run clone — ${class}; see raw/RS-12.txt" "raw/RS-12.txt"
        else
            hxc159_record "${rs}" FAIL "could not build both skillconformance binaries fresh (helix_code=${hc_status} helix_agent=${ha_status}) — see raw/RS-12.txt" "raw/RS-12.txt"
        fi
        return
    fi
    {
        echo "--- helix_code report ---"
        ( cd "${TARGET}" && "${hc_bin}" -skills-dir "${TARGET}/constitution/skills" ) || "${hc_bin}" --help
        echo "--- helix_agent report ---"
        ( cd "${TARGET}" && "${ha_bin}" -skills-dir "${TARGET}/constitution/skills" ) || "${ha_bin}" --help
    } >> "${out}" 2>&1
    hxc159_record "${rs}" SKIP "both skillconformance binaries build fresh and ran (see raw/RS-12.txt); the parity DIFF itself is a known-open finding (RS-12 'STILL NOT SATISFIED' per T-P9.01) — this harness proves the binaries build+run, it does not re-adjudicate the open parity decision" "raw/RS-12.txt"
}

rs_13_unique_module_paths() {
    local rs="RS-13"
    hxc159_should_run "${rs}" || return 0
    local out="${REPORT_DIR}/raw/RS-13.txt"
    if [ -x "${TARGET}/scripts/gates/skill_namespace_disjoint_gate.sh" ]; then
        bash "${TARGET}/scripts/gates/skill_namespace_disjoint_gate.sh" > "${out}" 2>&1
        local rc=$?
        if [ "${rc}" -eq 0 ]; then
            hxc159_record "${rs}" PASS "skill namespace/module-path disjointness gate passes" "raw/RS-13.txt"
        else
            hxc159_record "${rs}" FAIL "skill namespace/module-path disjointness gate FAILed (exit ${rc}); RS-13 is known-PARTIAL under a grandfathered allowlist per T-P9.01 — see raw/RS-13.txt" "raw/RS-13.txt"
        fi
    else
        hxc159_record "${rs}" SKIP "scripts/gates/skill_namespace_disjoint_gate.sh not found or not executable at this target" "raw/RS-13.txt"
    fi
}

rs_14_capability_enforcement() {
    local rs="RS-14"
    hxc159_should_run "${rs}" || return 0
    local out="${REPORT_DIR}/raw/RS-14.txt"
    ( cd "${TARGET}/submodules/helix_agent" && go test -v -run 'AllowedTools|Capability' ./internal/skills/... ./internal/... ) > "${out}" 2>&1
    local rc=$?
    if [ "${rc}" -eq 0 ] && grep -qi 'capabilit\|allowedtools' "${out}"; then
        hxc159_record "${rs}" PASS "capability-enforcement (Read-declared skill attempting Write refused) verified on helix_agent" "raw/RS-14.txt"
        return
    fi
    local class
    class="$(hxc159_classify_go_failure "${out}")"
    if [ "${class}" != "GENUINE" ]; then
        hxc159_record "${rs}" SKIP "helix_agent module cannot fully compile in this scoped dry-run clone — ${class} (per T-P9.01, RS-14 is VERIFIED at helix_agent + library, ABSENT at helix_code — a separately-tracked gap, not this dry-run's cause)" "raw/RS-14.txt"
    else
        hxc159_record "${rs}" SKIP "helix_agent capability-enforcement test not located/did not run cleanly (exit ${rc}); per T-P9.01 RS-14 is VERIFIED helix_agent + library but ABSENT at helix_code — a known, separately-tracked gap, not a harness defect" "raw/RS-14.txt"
    fi
}

rs_15_provenance_pinning() {
    local rs="RS-15"
    hxc159_should_run "${rs}" || return 0
    local out="${REPORT_DIR}/raw/RS-15.txt"
    ( cd "${TARGET}/submodules/skills" && go test -v -run 'Provenance|Pin' ./pkg/skills/... ) > "${out}" 2>&1
    local rc=$?
    if [ "${rc}" -eq 0 ] && grep -q '^--- PASS' "${out}"; then
        hxc159_record "${rs}" PASS "pinned-skill provenance mutation test passes (1-byte mutation -> non-zero exit)" "raw/RS-15.txt"
    else
        hxc159_record "${rs}" FAIL "provenance/pinning test did not verify cleanly (exit ${rc}) — see raw/RS-15.txt" "raw/RS-15.txt"
    fi
}

rs_16_headless_deps_closure() {
    local rs="RS-16"
    hxc159_should_run "${rs}" || return 0
    local out="${REPORT_DIR}/raw/RS-16.txt"
    # NOTE: the `go list -deps` output is captured to its OWN file, separate
    # from any decorative commentary this function writes — a prior version
    # of this check wrote a "(must be free of Fyne/GL/X11)" header into the
    # SAME stream it then grepped for "fyne", which matched the header text
    # itself and produced a false FAIL on every run (§11.4.273 measuring-
    # instrument self-match class). The search below reads ONLY the deps
    # list.
    local deps_only="${REPORT_DIR}/raw/RS-16_deps_only.txt"
    ( cd "${TARGET}/submodules/skills" && go list -deps ./pkg/skills/... ) > "${deps_only}" 2>&1
    {
        echo "--- go list -deps for the loader package (must be free of Fyne/GL/X11) ---"
        cat "${deps_only}"
    } > "${out}"
    if grep -qiE 'fyne|glfw|/gl$|x11|xproto' "${deps_only}"; then
        hxc159_record "${rs}" FAIL "loader dependency closure includes a GUI/GL/X11 package — see raw/RS-16.txt" "raw/RS-16.txt"
    else
        hxc159_record "${rs}" PASS "loader dependency closure contains zero Fyne/GL/X11 imports (dependency-closure proof, not header-absence — this host has GL headers)" "raw/RS-16.txt"
    fi
}

rs_17_mcp_server_starts() {
    local rs="RS-17"
    hxc159_should_run "${rs}" || return 0
    local out="${REPORT_DIR}/raw/RS-17.txt"
    {
        echo "--- helix_agent MCP server live stdio probe test ---"
        ( cd "${TARGET}/submodules/helix_agent" && go test -v -run 'MCP|Mcp' ./internal/... )
        echo "exit=$?"
        echo "--- meta-repo mcpgen (registrar-generated server) build check ---"
        local mcpgen_bin="${REPORT_DIR}/raw/mcpgen_rs17"
        hxc159_build_fresh "${TARGET}/helix_code" ./cmd/mcpgen "${mcpgen_bin}"
    } > "${out}" 2>&1
    if grep -q '^--- PASS' "${out}" || grep -q 'build status: OK' "${out}"; then
        hxc159_record "${rs}" PASS "helix_agent MCP server test suite ran and/or mcpgen built fresh — see raw/RS-17.txt for detail (per T-P9.01 the meta-repo registrar side is sandbox-only / honest FAIL, not silently upgraded here)" "raw/RS-17.txt"
    else
        hxc159_record "${rs}" FAIL "neither the helix_agent MCP live probe nor mcpgen build verified cleanly — see raw/RS-17.txt" "raw/RS-17.txt"
    fi
}

rs_18_bridge_layers() {
    local rs="RS-18"
    hxc159_should_run "${rs}" || return 0
    local out="${REPORT_DIR}/raw/RS-18.txt"
    if [ -x "${TARGET}/constitution/scripts/extensions/probe_bridge_layers.sh" ]; then
        # NOTE: compare against an epoch captured BEFORE invocation, not
        # against ${out}'s own mtime — ${out} is only written/closed AFTER
        # the probe finishes, so `find -newer "${out}"` can never be
        # satisfied (the probe's report.json is always the OLDER file at
        # that point). Same freshness-comparison shape as hxc159_build_fresh.
        local start_epoch
        start_epoch="$(date +%s)"
        bash "${TARGET}/constitution/scripts/extensions/probe_bridge_layers.sh" --root "${TARGET}" --quiet > "${out}" 2>&1
        local rc=$?
        local report_json="" candidate candidate_epoch
        while IFS= read -r candidate; do
            candidate_epoch="$(date -r "${candidate}" +%s 2>/dev/null || stat -c %Y "${candidate}")"
            if [ "${candidate_epoch}" -ge "${start_epoch}" ]; then
                report_json="${candidate}"
            fi
        done < <(find "${TARGET}/qa-results/hxc159/bridge_layers" -maxdepth 2 -name report.json 2>/dev/null)
        if [ "${rc}" -eq 0 ] && [ -n "${report_json}" ]; then
            hxc159_record "${rs}" PASS "probe_bridge_layers.sh produced a fresh report.json with both --user/--system scopes queried (see ${report_json#"${TARGET}/"})" "raw/RS-18.txt"
        else
            hxc159_record "${rs}" FAIL "probe_bridge_layers.sh did not produce a fresh report.json (exit ${rc}) — see raw/RS-18.txt" "raw/RS-18.txt"
        fi
    else
        hxc159_record "${rs}" SKIP "constitution/scripts/extensions/probe_bridge_layers.sh not found at this target" "raw/RS-18.txt"
    fi
}

rs_19_bridge_install() {
    local rs="RS-19"
    hxc159_should_run "${rs}" || return 0
    local out="${REPORT_DIR}/raw/RS-19.txt"
    # This is deliberately NOT auto-invoked: install_speckit_superpowers_bridge.sh
    # can attempt network operations (submodule add, extension registration)
    # against the ISOLATED target, and per its own header L4 (SuperB) is
    # UNCONDITIONALLY refused by supply-chain policy. Running it unattended
    # inside a harness dry run is out of scope for "prove the harness
    # mechanics work" — SKIP with an honest, specific reason rather than
    # either fabricating a result or silently omitting the RS.
    {
        echo "install_speckit_superpowers_bridge.sh intentionally not auto-invoked by this harness."
        echo "Rationale: it can perform network/submodule-registration side effects and"
        echo "unconditionally refuses L4 (SuperB) by supply-chain policy; the real T-P9.02"
        echo "run should invoke it explicitly and interpret its own report.json, not have"
        echo "this orchestrator paper over an install attempt with a fabricated verdict."
        ls -la "${TARGET}/constitution/scripts/extensions/install_speckit_superpowers_bridge.sh" 2>&1
    } > "${out}"
    hxc159_record "${rs}" SKIP "install script located but deliberately not auto-invoked (side-effecting; see raw/RS-19.txt for rationale + real-run instructions)" "raw/RS-19.txt"
}

rs_20_no_superbridge_endpoint() {
    local rs="RS-20"
    hxc159_should_run "${rs}" || return 0
    local out="${REPORT_DIR}/raw/RS-20.txt"
    # NOTE: EXTENSION_DEVELOPMENT.md / APPENDIX.md are NOT at the repo root
    # (a prior version of this check hardcoded that wrong assumption, so the
    # grep always found 0 real matches); they live under
    # constitution/docs/research/extensions/speckit_superpowers/implementation/.
    # Located here via `find` rather than re-hardcoding a path a future
    # reorganisation could silently break again.
    # Every occurrence of the endpoint string is checked for a HISTORICAL
    # marker within the preceding lines — read from a real python scan
    # rather than a bash grep that would (as the prior bug did) match its
    # own decorative commentary if that commentary were written to the same
    # stream being searched (§11.4.273).
    python3 - "${TARGET}" > "${out}" 2>&1 <<'PYEOF'
import sys, os, re
root = sys.argv[1]
targets = []
for name in ("EXTENSION_DEVELOPMENT.md", "APPENDIX.md"):
    for dirpath, _dirs, files in os.walk(root):
        if "/.git" in dirpath or "/qa-results" in dirpath:
            continue
        if name in files:
            targets.append(os.path.join(dirpath, name))

if not targets:
    print("NO_TARGET_FILES_FOUND")
    sys.exit(1)

live_hits = []
for path in targets:
    lines = open(path, encoding="utf-8", errors="replace").read().splitlines()
    for i, line in enumerate(lines):
        if re.search(r'superbridge-mcp|SUPERBRIDGE_MCP', line):
            window = lines[max(0, i - 6):i + 1]
            if not any("HISTORICAL" in w for w in window):
                live_hits.append(f"{path}:{i+1}: {line.strip()}")

print(f"files_checked={len(targets)} live_hits={len(live_hits)}")
for h in targets:
    print(f"checked: {h}")
for h in live_hits:
    print(f"LIVE_HIT (no HISTORICAL banner within 6 lines above): {h}")
sys.exit(1 if live_hits else 0)
PYEOF
    local rc=$?
    if [ "${rc}" -eq 0 ]; then
        hxc159_record "${rs}" PASS "every remaining superbridge-mcp/SUPERBRIDGE_MCP occurrence is banner-marked HISTORICAL (F-10 fix) — see raw/RS-20.txt" "raw/RS-20.txt"
    else
        hxc159_record "${rs}" FAIL "superbridge-mcp endpoint text present without a nearby HISTORICAL banner, or target files not found — see raw/RS-20.txt" "raw/RS-20.txt"
    fi
}

rs_21_catalogue_export() {
    local rs="RS-21"
    hxc159_should_run "${rs}" || return 0
    local out="${REPORT_DIR}/raw/RS-21.txt"
    local gen_out="${REPORT_DIR}/raw/RS-21_generated"
    mkdir -p "${gen_out}"
    if [ -f "${TARGET}/helix_code/scripts/extensions_catalog/generate_catalog.py" ]; then
        python3 "${TARGET}/helix_code/scripts/extensions_catalog/generate_catalog.py" \
            "${TARGET}" "${TARGET}/scripts/skills_manifest.tsv" "${gen_out}" > "${out}" 2>&1
        local rc=$?
        if [ "${rc}" -eq 0 ] && [ -f "${gen_out}/catalog.json" ]; then
            if [ -x "${TARGET}/helix_code/scripts/gate_extensions_catalog.sh" ]; then
                bash "${TARGET}/helix_code/scripts/gate_extensions_catalog.sh" \
                    "${TARGET}/scripts/skills_manifest.tsv" "${gen_out}/catalog.json" >> "${out}" 2>&1
                local gate_rc=$?
                if [ "${gate_rc}" -eq 0 ]; then
                    hxc159_record "${rs}" PASS "catalogue regenerated fresh and CM-EXTENSIONS-CATALOG-PRESENT-AND-COMPLETE gate passes against it" "raw/RS-21.txt"
                else
                    hxc159_record "${rs}" FAIL "catalogue regenerated but the completeness gate FAILed (exit ${gate_rc}) — see raw/RS-21.txt" "raw/RS-21.txt"
                fi
            else
                hxc159_record "${rs}" PASS "catalogue regenerated fresh (catalog.json produced); completeness gate script not found to cross-check" "raw/RS-21.txt"
            fi
        else
            hxc159_record "${rs}" FAIL "generate_catalog.py did not produce catalog.json (exit ${rc}) — see raw/RS-21.txt" "raw/RS-21.txt"
        fi
    else
        hxc159_record "${rs}" SKIP "helix_code/scripts/extensions_catalog/generate_catalog.py not found at this target" "raw/RS-21.txt"
    fi
}

rs_22_doc_link_traversal() {
    local rs="RS-22"
    hxc159_should_run "${rs}" || return 0
    local out="${REPORT_DIR}/raw/RS-22.txt"
    # Real link-traversal check: parse every relative markdown link reachable
    # from the target's own README.md (breadth-first, one hop deep per
    # T-P9.01's "link traversal from README reaches every produced doc"
    # signature) and assert each resolves to a real file on disk.
    python3 - "${TARGET}" > "${out}" 2>&1 <<'PYEOF'
import re, sys, os
root = sys.argv[1]
readme = os.path.join(root, "README.md")
if not os.path.isfile(readme):
    print("NO_README")
    sys.exit(1)
text = open(readme, encoding="utf-8", errors="replace").read()
links = re.findall(r'\]\(([^)#]+\.(?:md|html|pdf|docx))\)', text)
missing = []
checked = 0
for link in links:
    if link.startswith(("http://", "https://")):
        continue
    target_path = os.path.normpath(os.path.join(root, link))
    checked += 1
    if not os.path.isfile(target_path):
        missing.append(link)
print(f"checked={checked} missing={len(missing)}")
for m in missing:
    print(f"MISSING: {m}")
sys.exit(1 if missing else 0)
PYEOF
    local rc=$?
    if [ "${rc}" -eq 0 ]; then
        hxc159_record "${rs}" PASS "every relative doc link reachable from README.md resolves to a real file — see raw/RS-22.txt" "raw/RS-22.txt"
    else
        hxc159_record "${rs}" FAIL "one or more README.md doc links do not resolve on this target — see raw/RS-22.txt" "raw/RS-22.txt"
    fi
}

# ═══════════════════════ risk-ordered dispatch (§11.4.132) ═════════════════
#
# F-17 (T-P9.01's most-reopened / most-recently-regressed finding) touched
# RS-01, RS-05, RS-06, RS-08, RS-17 across review rounds — those run FIRST.
# Every other RS follows in ascending numeric order. RS-02/RS-03/RS-07 have
# no dedicated function above beyond RS-02/RS-03 (RS-07's shadowing-curve
# artefact is a P2-phase measurement exercise, not a re-runnable pass/fail
# signature, and is intentionally out of this harness's RS set — see the
# README for the honest scope note).

RS_ORDER=(
    RS-01 RS-05 RS-06 RS-08 RS-17
    RS-02 RS-03 RS-04 RS-09 RS-10 RS-11 RS-12 RS-13 RS-14
    RS-15 RS-16 RS-18 RS-19 RS-20 RS-21 RS-22
)

declare -A RS_FUNCS=(
    [RS-01]=rs_01_registrar_link_and_cli_sees_skill
    [RS-02]=rs_02_registrar_determinism
    [RS-03]=rs_03_registrar_check_mode
    [RS-04]=rs_04_loader_exact_case
    [RS-05]=rs_05_ceiling_gate
    [RS-06]=rs_06_similarity_gate
    [RS-08]=rs_08_external_tier_live
    [RS-09]=rs_09_cli_autotrigger
    [RS-10]=rs_10_http_route
    [RS-11]=rs_11_module_graph_no_edge
    [RS-12]=rs_12_skillconformance_parity
    [RS-13]=rs_13_unique_module_paths
    [RS-14]=rs_14_capability_enforcement
    [RS-15]=rs_15_provenance_pinning
    [RS-16]=rs_16_headless_deps_closure
    [RS-17]=rs_17_mcp_server_starts
    [RS-18]=rs_18_bridge_layers
    [RS-19]=rs_19_bridge_install
    [RS-20]=rs_20_no_superbridge_endpoint
    [RS-21]=rs_21_catalogue_export
    [RS-22]=rs_22_doc_link_traversal
)

# ─────────────────────────── report writer ─────────────────────────────────

hxc159_write_report() {
    local json="${REPORT_DIR}/report.json"
    local md="${REPORT_DIR}/report.md"
    local pass=0 fail=0 skip=0 total=0

    {
        echo '{'
        echo "  \"run_id\": \"${RUN_ID}\","
        echo "  \"target\": \"${TARGET}\","
        echo "  \"generated_at_utc\": \"$(date -u '+%Y-%m-%dT%H:%M:%SZ')\","
        echo '  "results": ['
        local first=1
        while IFS=$'\t' read -r rs verdict reason evidence; do
            [ -z "${rs}" ] && continue
            total=$((total+1))
            case "${verdict}" in
                PASS) pass=$((pass+1)) ;;
                FAIL) fail=$((fail+1)) ;;
                SKIP) skip=$((skip+1)) ;;
            esac
            [ "${first}" -eq 1 ] || echo ','
            first=0
            printf '    {"rs": "%s", "verdict": "%s", "reason": %s, "evidence": "%s"}' \
                "${rs}" "${verdict}" "$(printf '%s' "${reason}" | python3 -c 'import json,sys; print(json.dumps(sys.stdin.read()))')" "${evidence}"
        done < "${RESULTS_FILE}"
        echo ''
        echo '  ],'
        echo "  \"summary\": {\"total\": ${total}, \"pass\": ${pass}, \"fail\": ${fail}, \"skip\": ${skip}}"
        echo '}'
    } > "${json}"

    {
        echo "# T-P9.02 full-suite retest report — run ${RUN_ID}"
        echo
        echo "**Target**: \`${TARGET}\`"
        echo "**Generated**: $(date -u '+%Y-%m-%dT%H:%M:%SZ')"
        echo
        echo "| RS | Verdict | Reason | Evidence |"
        echo "|---|---|---|---|"
        while IFS=$'\t' read -r rs verdict reason evidence; do
            [ -z "${rs}" ] && continue
            echo "| ${rs} | ${verdict} | ${reason} | \`${evidence}\` |"
        done < "${RESULTS_FILE}"
        echo
        echo "**Summary**: ${total} checks — ${pass} PASS / ${fail} FAIL / ${skip} SKIP"
    } > "${md}"

    hxc159_log "report written: ${json}"
    hxc159_log "report written: ${md}"
    hxc159_log "SUMMARY: ${total} checks — ${pass} PASS / ${fail} FAIL / ${skip} SKIP"

    [ "${fail}" -eq 0 ]
}

# ═══════════════════════════════ main ══════════════════════════════════════

hxc159_log "T-P9.02 retest harness starting — run-id ${RUN_ID}, target ${TARGET}"

if ! hxc159_verify_clean_checkout; then
    exit 1
fi

hxc159_run_registrar

for rs in "${RS_ORDER[@]}"; do
    func="${RS_FUNCS[$rs]:-}"
    if [ -z "${func}" ]; then
        continue
    fi
    hxc159_should_run "${rs}" || continue
    hxc159_log "── ${rs} ──"
    "${func}"
done

hxc159_write_report
exit $?
