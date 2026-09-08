#!/usr/bin/env bash
# scripts/gates/no_hardcoded_db_credential_gate.sh
#
# HXC-168 / CONST-042 (Article XII §12.1) regression guard: no database (or
# adjacent service) credential may appear as a LITERAL in the setup and
# container files this project publishes. Those files must read the value from
# the private, gitignored configuration source (.env -> environment).
#
# Why this gate exists (§11.4.135): a published credential cannot be un-published
# — it lives in git history on every mirror forever. The only remedy for a leak
# is rotation, so the ONLY thing code can guarantee is that a new literal never
# lands again. That is exactly what this gate pins.
#
# Three independent checks:
#
#   CHECK 1 — historical-literal denylist.
#       BOTH credential literals HXC-168 has exposed so far must not reappear
#       in ANY tracked file, outside third-party submodules, documentation
#       prose (prose that DISCUSSES the incident is legitimate; a config that
#       USES the value is not — and since 2026-09-08 that distinction is made
#       by LINE SHAPE, not by file extension: see
#       _helix_drop_documentation_prose, which replaced a blanket `.md|.html|.pdf`
#       exemption that had been letting 36 copy-pasteable credential lines in 16
#       documentation files sit under a green gate), captured docs/qa/ evidence
#       transcripts
#       (§11.4.83 self-test output legitimately shows the planted literal
#       being caught — it is not a live usage of the credential), and the one
#       explicitly-tracked mock-only fixture carved out below.
#
#       NOTE (2026-09-02 orchestrator finding): a prior remediation pass fixed
#       only 2 of 18 tracked files carrying the SECOND literal
#       (HISTORICAL_LITERAL_2 below — do NOT spell it out contiguously in a
#       comment, or this gate flags its OWN source; that is exactly why the
#       value is assembled from two fragments, never written whole, anywhere
#       in this file, including comments) because this gate's CHECK 1 only
#       ever hunted the FIRST literal (HISTORICAL_LITERAL_1). The gate
#       reported PASS ("literal absent") while a plain-text search for the
#       second literal across tracked files still returned 18 hits. That was
#       a §11.4.1 PASS-bluff. CHECK 1 now hunts BOTH literals so this class
#       of regression cannot repeat silently.
#
#   CHECK 2 — credential-sourcing check (generic, forward-looking).
#       In the deployed setup/container/config scope, every credential assignment
#       must reference an environment variable (`${VAR}` / `${VAR:?...}`) rather
#       than carry a literal. This catches a BRAND-NEW hardcoded password, not
#       only the historical one — a denylist alone would not.
#
#   CHECK 3 — database ports bind loopback (HXC-168 operator option [C]).
#       Every scoped database port publish must carry an explicit 127.0.0.1 bind
#       address. A publish written as "HOST:CONTAINER" with no bind address
#       defaults to the wildcard, so the database answers on every interface the
#       host has, which on this host means the LAN. This check controls
#       REACHABILITY only: it reduces who can reach the database and does NOT
#       invalidate a credential that is already published (§11.4.6).
#
# Usage:
#   no_hardcoded_db_credential_gate.sh              # run the gate (exit 1 on any finding)
#   no_hardcoded_db_credential_gate.sh --self-test  # §1.1 paired mutation: prove the
#                                                   # gate FAILS on a planted literal
#                                                   # and PASSES on the real tree.
#
# The self-test NEVER writes inside the repository (§11.4.84 working-tree
# quiescence): it plants its mutation in a mktemp directory only.
set -uo pipefail

ROOT="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT" || exit 2

RED=$'\033[31m'; GREEN=$'\033[32m'; YELLOW=$'\033[33m'; BOLD=$'\033[1m'; OFF=$'\033[0m'

# The historical literals, each assembled at runtime from fragments so this
# gate file does not itself contain a credential (the gate would otherwise
# flag itself, and committing the value here would re-publish it).
#   - HISTORICAL_LITERAL_1: the credential the original HXC-168 leak exposed.
#   - HISTORICAL_LITERAL_2: the literal a first HXC-168 remediation pass left
#     behind, tracked in 18 files, undetected because CHECK 1 only hunted
#     HISTORICAL_LITERAL_1 (see the NOTE above run_gate's CHECK 1 header).
HISTORICAL_LITERAL_1="helix""pass"
HISTORICAL_LITERAL_2="helixcode_test_""password"
HISTORICAL_LITERALS=("$HISTORICAL_LITERAL_1" "$HISTORICAL_LITERAL_2")

# Backward-compatible alias: self_test()'s CHECK-2-only self-tests (2 and 3,
# which exercise scan_file_for_literals — a generic literal-shape detector,
# not the denylist) plant HISTORICAL_LITERAL_1 specifically, matching the
# original HXC-168 incident value.
HISTORICAL_LITERAL="$HISTORICAL_LITERAL_1"

# Deployed setup / container / config scope for CHECK 2. Explicit rather than
# globbed so the scope is reviewable and cannot silently drift.
SCOPE_FILES=(
  "Dockerfile"
  "docker-compose.helix.yml"
  "compose.helixcode-infra.yml"
  ".env.example"
  "helix_code/docker-compose.yml"
  "helix_code/docker-compose.builder.yml"
  "helix_code/docker/docker-compose.yml"
  "helix_code/docker/autoboot/docker-compose.autoboot.yml"
  "helix_code/config/config.yaml"
  "helix_code/config/production-config.yaml"
  "helix_code/config/working-config.yaml"
  "helix_code/config/fixed-config.yaml"
  "helix_code/config/minimal-test-config.yaml"
  "docs/distribution/docker-compose.mistborn.yml"
  "tests/e2e/challenges/helix_qa_live_anti_bluff.sh"
)

# Credential keys whose assigned value must be env-sourced.
CRED_KEY_RE='(POSTGRES_PASSWORD|HELIX_DATABASE_PASSWORD|HELIX_AUTOBOOT_PG_PASSWORD|HELIX_POSTGRES_PASSWORD|HELIX_REDIS_PASSWORD|GF_SECURITY_ADMIN_PASSWORD|SONAR_JDBC_PASSWORD)'

# CHECK 3 scope: the database port publishes named by the HXC-168
# operator-block details. A publish with no bind address defaults to the
# wildcard, so the database answers on every interface the host has; binding
# 127.0.0.1 shrinks that to loopback. This is a REACHABILITY control and is
# independent of the credential: it reduces WHO CAN REACH the database, and does
# NOT invalidate a password that is already published (§11.4.6).
PORT_TARGETS=(
  "compose.helixcode-infra.yml|5433:5432"
  "helix_code/docker/autoboot/docker-compose.autoboot.yml|55432:5432"
  "helix_code/docker/docker-compose.yml|5432:5432"
)

# scan_file_for_wildcard_publish FILE MAPPING — returns 1 (and prints) when the
# mapping is published without a 127.0.0.1 bind address, or when the publish line
# cannot be found at all. An unfindable line is reported rather than silently
# passed: "I could not check" is not "it is fine" (§11.4.201(6)).
scan_file_for_wildcard_publish() {
  local f="$1" mapping="$2"
  if [ ! -f "$f" ]; then
    printf '  %s%s%s  scoped file missing (scope drift?)\n' "$BOLD" "$f" "$OFF"
    return 1
  fi
  if grep -qE "^[[:space:]]*-[[:space:]]*\"?127\.0\.0\.1:${mapping}\"?[[:space:]]*$" "$f"; then
    return 0
  fi
  if grep -qE "^[[:space:]]*-[[:space:]]*\"?${mapping}\"?[[:space:]]*$" "$f"; then
    printf '  %s%s%s  publishes %s on the WILDCARD address (LAN-reachable)\n' \
      "$BOLD" "$f" "$OFF" "$mapping"
  else
    printf '  %s%s%s  no recognisable publish line for %s (cannot certify the bind)\n' \
      "$BOLD" "$f" "$OFF" "$mapping"
  fi
  return 1
}

# ---------------------------------------------------------------------------
# CHECK 2 implementation, factored so --self-test can run it against a planted
# copy in a temp dir without touching the working tree.
# Prints one finding per line; returns 1 if any finding was printed.
# ---------------------------------------------------------------------------
scan_file_for_literals() {
  local f="$1" label="${2:-$1}" found=0 line lineno value

  [ -f "$f" ] || return 0

  # (a) KEY=value / KEY: value assignments.
  while IFS=: read -r lineno line; do
    [ -n "${lineno:-}" ] || continue
    # Strip a leading comment / YAML-comment line.
    case "$(printf '%s' "$line" | sed 's/^[[:space:]]*//')" in
      '#'*) continue ;;
    esac
    # An `${...}` anywhere on the line means the value is env-sourced. Testing
    # for containment (rather than parsing the value out) is deliberate: a
    # `${VAR:?long message}` default contains both `:` and the key name again,
    # which defeats any greedy value-extraction regex.
    case "$line" in
      *'${'*) continue ;;
    esac
    # No env reference on the line — the value is a literal unless it is an
    # explicitly allowed placeholder.
    value="$(printf '%s' "$line" | sed -E "s/^.*${CRED_KEY_RE}[[:space:]]*[:=][[:space:]]*//")"
    # Order matters: drop the shell line-continuation, trailing comment and
    # trailing whitespace BEFORE unquoting, or an intentionally-empty `""`
    # value is mis-read as a literal.
    value="$(printf '%s' "$value" | sed -E 's/[[:space:]]*\\$//; s/[[:space:]]*#.*$//; s/[[:space:]]*$//; s/^["'\'']//; s/["'\'']$//')"
    case "$value" in
      ''|CHANGE_ME*|'<REDACTED>') continue ;;
    esac
    printf '  %s%s:%s%s  credential key assigned a LITERAL value\n' "$BOLD" "$label" "$lineno" "$OFF"
    found=1
  done < <(grep -nE "${CRED_KEY_RE}[[:space:]]*[:=]" "$f" 2>/dev/null)

  # (b) postgres:// URLs carrying an inline password that is not an env ref.
  while IFS=: read -r lineno line; do
    [ -n "${lineno:-}" ] || continue
    case "$(printf '%s' "$line" | sed 's/^[[:space:]]*//')" in
      '#'*) continue ;;
    esac
    printf '  %s%s:%s%s  connection URL embeds a LITERAL password\n' "$BOLD" "$label" "$lineno" "$OFF"
    found=1
  done < <(grep -nE 'postgres(ql)?://[A-Za-z0-9_.-]+:[^$@[:space:]]+@' "$f" 2>/dev/null)

  return $found
}

# scan_repo_for_historical_literal implements CHECK 1 against an arbitrary repo
# root, so --self-test can exercise the SAME pipeline against a throwaway repo
# containing a planted literal. Prints matching "path:line:text" rows.
#
# NOTE: this gate file deliberately assembles each literal from two fragments
# at runtime, so its own bytes never contain either, and it needs no
# self-exclusion — a self-exclusion would be a hole an attacker could hide a
# credential in.
#
# Exclusions applied, each narrow and justified (§11.4.135 — a broad/vague
# exclusion is itself a way to re-hide a bluff):
#   1. submodules/       — third-party code we do not own or control.
#   2. *.md / *.html / *.pdf — documentation prose. Prose that DISCUSSES the
#      incident (e.g. this very gate's own commit history, root-cause docs)
#      legitimately names the literal; a config or source file that USES the
#      value as a real credential does not get this exemption (those file
#      types are never .md/.html/.pdf).
#   3. docs/qa/          — captured §11.4.83 QA-evidence transcripts (e.g.
#      this gate's own --self-test output committed as proof it once caught
#      the literal). Editing them would falsify historical evidence; they are
#      not a live usage of the credential.
#   4. helix_code/.env.full-test — a SINGLE, EXACT, already-tracked path. Per
#      helix_code/.gitignore's own documented `!.env.full-test` carve-out,
#      this file holds ONLY mock/test values (every credential in it is
#      `*_test_password` / `mock-*-for-testing`), is required untracked-free
#      by the Makefile's `. ./.env.full-test` sourcing, and is EXPLICITLY
#      OUT OF SCOPE for this remediation pass (HXC-168 task instructions:
#      "DO NOT act on .env.full-test unilaterally... Report only"). Excluding
#      it here is a conscious, narrow, single-path decision — NOT a wildcard
#      — so it cannot silently swallow a literal introduced anywhere else.
scan_repo_for_historical_literal() {
  local repo="$1"
  local -a grep_args=()
  local lit
  for lit in "${HISTORICAL_LITERALS[@]}"; do
    grep_args+=(-e "$lit")
  done
  # -H is load-bearing, not decoration: grep prefixes the filename only when it
  # is handed MORE THAN ONE file, so a final xargs batch containing exactly one
  # path (or a small repo) yields "LINE:text" with no path at all. That silently
  # broke two things — the finding became unattributable, and the path-keyed
  # prose filter below saw the line NUMBER as its path. -H forces the prefix
  # unconditionally. (Found 2026-09-08 by the gate's own SELF-TEST 5, whose
  # planted repo has a single tracked file.)
  git -C "$repo" ls-files -z 2>/dev/null \
    | (cd "$repo" && xargs -0 grep -HIn "${grep_args[@]}" 2>/dev/null) \
    | grep -vE '^submodules/' \
    | grep -vE '^docs/qa/' \
    | _helix_drop_documentation_prose \
    || true
}

# _helix_drop_documentation_prose — the §11.4.201 discriminator that replaced a
# blanket `.md|.html|.pdf` exclusion.
#
# WHY THE BLANKET EXCLUSION WAS A DEFECT (measured 2026-09-08): exempting every
# documentation file by EXTENSION meant CHECK 1 reported PASS while 36 tracked
# lines in 16 `.md`/`.html` files still handed the credential over in
# copy-pasteable config blocks — `POSTGRES_PASSWORD: <lit>`,
# `postgresql://user:<lit>@host`, `HELIX_DATABASE_PASSWORD=<lit>`. A reader
# following those documents pastes a published credential into a real config.
# The gate was green over the exact carrier the item was still open about, which
# is the §11.4.201(1)/§11.4.1 shape: a guard that does not assert the real
# condition.
#
# The condition that is actually meant is NOT "which file type is this" but
# "does this line HAND THE VALUE OVER". So:
#
#   FLAGGED   — the line, once HTML tags are stripped and leading whitespace, a
#               YAML `- ` item marker or an `export ` keyword are removed, BEGINS
#               with an assignment (`IDENT:` / `IDENT=`) or a bare
#               `postgres://` URL. That line is copy-pasteable into a real config.
#   EXEMPT    — narrative prose that merely NAMES the value mid-sentence, e.g.
#               "The compose default `HELIX_POSTGRES_PASSWORD:-<lit>` and …".
#               Documentation that DISCUSSES the incident is legitimate; refusing
#               it would be the FAIL-bluff §11.4.201 equally forbids.
#
# Tags are stripped before the test so a rendered `.html` code block is judged by
# the same rule as the `.md` it was generated from, instead of escaping through
# markup. Non-documentation files are never exempted by this filter at all — they
# pass straight through, exactly as before.
_helix_drop_documentation_prose() {
  awk -F: '{
    path=$1;
    if (path !~ /\.(md|html|pdf)$/) { print; next }   # only docs are ever exempt
    text=$0; sub(/^[^:]*:[^:]*:/, "", text);
    gsub(/<[^>]*>/, "", text);
    sub(/^[[:space:]]+/, "", text);
    sub(/^-[[:space:]]+/, "", text);
    sub(/^export[[:space:]]+/, "", text);
    if (text ~ /^[A-Za-z_][A-Za-z0-9_]*[[:space:]]*[:=]/ || text ~ /^postgres(ql)?:\/\//)
      print;                                          # hands the value over
  }'
}

run_gate() {
  local rc=0 f hits

  printf '%s== CHECK 1: historical-literal denylist ==%s\n' "$BOLD" "$OFF"
  # Tracked files only; exclude third-party submodules and documentation prose.
  hits="$(scan_repo_for_historical_literal "$ROOT")"
  if [ -n "$hits" ]; then
    printf '%sFAIL%s — the HXC-168 credential literal is present in tracked config/source:\n' "$RED" "$OFF"
    printf '%s\n' "$hits" | sed 's/^/  /'
    rc=1
  else
    printf '%sPASS%s — literal absent from tracked config/source.\n' "$GREEN" "$OFF"
  fi

  printf '\n%s== CHECK 2: credentials are env-sourced in the deployed scope ==%s\n' "$BOLD" "$OFF"
  local check2_rc=0
  for f in "${SCOPE_FILES[@]}"; do
    if [ ! -f "$f" ]; then
      printf '%sWARN%s — scoped file missing (scope drift?): %s\n' "$YELLOW" "$OFF" "$f"
      continue
    fi
    scan_file_for_literals "$f" || check2_rc=1
  done
  if [ "$check2_rc" -ne 0 ]; then
    printf '%sFAIL%s — the lines above must read the value from .env, e.g.\n' "$RED" "$OFF"
    printf '        POSTGRES_PASSWORD: ${HELIX_DATABASE_PASSWORD:?set it in .env}\n'
    rc=1
  else
    printf '%sPASS%s — every scoped credential assignment is env-sourced.\n' "$GREEN" "$OFF"
  fi

  printf '\n%s== CHECK 3: database port publishes bind loopback ==%s\n' "$BOLD" "$OFF"
  local check3_rc=0
  for t in "${PORT_TARGETS[@]}"; do
    scan_file_for_wildcard_publish "${t%%|*}" "${t##*|}" || check3_rc=1
  done
  if [ "$check3_rc" -ne 0 ]; then
    printf '%sFAIL%s — a database port is published on the wildcard address, making it\n' "$RED" "$OFF"
    printf '        reachable from the LAN. Bind it to loopback, e.g. "127.0.0.1:5433:5432".\n'
    rc=1
  else
    printf '%sPASS%s — every scoped database port publish binds 127.0.0.1.\n' "$GREEN" "$OFF"
  fi

  return $rc
}

# ---------------------------------------------------------------------------
# --self-test: §1.1 paired mutation. Proves the gate is falsifiable — a gate
# that cannot be shown to FAIL is a bluff gate (§11.4 / §11.4.107(10)).
# ---------------------------------------------------------------------------
self_test() {
  local tmp rc_clean rc_planted overall=0
  tmp="$(mktemp -d)" || { printf 'cannot mktemp\n' >&2; exit 2; }
  # shellcheck disable=SC2064
  trap "rm -rf -- '$tmp'" EXIT

  printf '%s== SELF-TEST 1/8: gate PASSES on the real tree (golden-good) ==%s\n' "$BOLD" "$OFF"
  if run_gate >/dev/null 2>&1; then
    rc_clean=0; printf '%sPASS%s — gate reports clean on the working tree.\n\n' "$GREEN" "$OFF"
  else
    rc_clean=1
    printf '%sFAIL%s — gate does not pass on the working tree; full output:\n' "$RED" "$OFF"
    run_gate || true
    overall=1
  fi

  printf '%s== SELF-TEST 2/8: gate FAILS on a planted literal (golden-bad) ==%s\n' "$BOLD" "$OFF"
  # Plant a credential literal in a COPY, in the temp dir only.
  printf 'services:\n  postgres:\n    environment:\n      POSTGRES_PASSWORD: %s\n' \
    "$HISTORICAL_LITERAL" > "$tmp/planted-compose.yml"
  if scan_file_for_literals "$tmp/planted-compose.yml" "planted-compose.yml" >"$tmp/out" 2>&1; then
    rc_planted=0
    printf '%sFAIL%s — gate did NOT flag a planted credential literal. The gate is a bluff.\n' "$RED" "$OFF"
    overall=1
  else
    rc_planted=1
    printf '%sPASS%s — gate flagged the planted literal:\n' "$GREEN" "$OFF"
    sed 's/^/    /' "$tmp/out"
  fi
  printf '\n'

  printf '%s== SELF-TEST 3/8: gate FAILS on a planted inline-URL password ==%s\n' "$BOLD" "$OFF"
  printf 'services:\n  app:\n    environment:\n      - DB=postgres://helix:s3cr3t-planted@postgres:5432/db\n' \
    > "$tmp/planted-url.yml"
  if scan_file_for_literals "$tmp/planted-url.yml" "planted-url.yml" >"$tmp/out2" 2>&1; then
    printf '%sFAIL%s — gate did NOT flag an inline-URL credential.\n' "$RED" "$OFF"
    overall=1
  else
    printf '%sPASS%s — gate flagged the planted inline-URL credential:\n' "$GREEN" "$OFF"
    sed 's/^/    /' "$tmp/out2"
  fi

  printf '\n%s== SELF-TEST 4/8: CHECK 1 FAILS on a tracked planted literal ==%s\n' "$BOLD" "$OFF"
  # Exercise the REAL CHECK 1 pipeline (git ls-files + grep + exclusions) against
  # a throwaway repository, so the denylist half is proven falsifiable without
  # ever dirtying this working tree (§11.4.84 — other agents share this checkout).
  mkdir -p "$tmp/repo"
  git -C "$tmp/repo" init -q 2>/dev/null
  printf 'POSTGRES_PASSWORD: %s\n' "$HISTORICAL_LITERAL" > "$tmp/repo/compose.yml"
  # A prose file must NOT trip the gate — discussing the incident is legitimate.
  printf 'The leaked value was `%s`.\n' "$HISTORICAL_LITERAL" > "$tmp/repo/NOTES.md"
  git -C "$tmp/repo" add compose.yml NOTES.md 2>/dev/null
  git -C "$tmp/repo" -c user.email=gate@test -c user.name=gate commit -qm planted 2>/dev/null

  local planted_hits
  planted_hits="$(scan_repo_for_historical_literal "$tmp/repo")"
  if [ -z "$planted_hits" ]; then
    printf '%sFAIL%s — CHECK 1 did NOT flag a tracked planted literal. The denylist is a bluff.\n' "$RED" "$OFF"
    overall=1
  elif printf '%s' "$planted_hits" | grep -q '^NOTES\.md:'; then
    printf '%sFAIL%s — CHECK 1 flagged documentation prose; the exclusion is broken.\n' "$RED" "$OFF"
    overall=1
  else
    printf '%sPASS%s — CHECK 1 flagged the tracked config and correctly ignored prose:\n' "$GREEN" "$OFF"
    printf '%s\n' "$planted_hits" | sed 's/^/    /'
  fi

  printf '\n%s== SELF-TEST 5/8: CHECK 1 catches the SECOND (remediation-pass-1) literal ==%s\n' "$BOLD" "$OFF"
  # This is the exact regression class the orchestrator caught (2026-09-02):
  # a prior fix pass introduced/left HISTORICAL_LITERAL_2 in tracked config
  # while CHECK 1 only ever hunted HISTORICAL_LITERAL_1. Prove the SECOND
  # literal is independently detected — a single-literal denylist would pass
  # this planted repo despite the leak still being present.
  mkdir -p "$tmp/repo2"
  git -C "$tmp/repo2" init -q 2>/dev/null
  printf 'POSTGRES_PASSWORD: %s\n' "$HISTORICAL_LITERAL_2" > "$tmp/repo2/compose.yml"
  git -C "$tmp/repo2" add compose.yml 2>/dev/null
  git -C "$tmp/repo2" -c user.email=gate@test -c user.name=gate commit -qm planted2 2>/dev/null

  local planted_hits2
  planted_hits2="$(scan_repo_for_historical_literal "$tmp/repo2")"
  if [ -z "$planted_hits2" ]; then
    printf '%sFAIL%s — CHECK 1 did NOT flag the second literal. This is the exact bluff HXC-168 caught.\n' "$RED" "$OFF"
    overall=1
  else
    printf '%sPASS%s — CHECK 1 flagged the second literal too:\n' "$GREEN" "$OFF"
    printf '%s\n' "$planted_hits2" | sed 's/^/    /'
  fi

  printf '\n%s== SELF-TEST 6/8: .env.full-test is scanned like any other tracked file ==%s\n' "$BOLD" "$OFF"
  # 2026-09-02 root-cause fix: this used to prove a narrow exclusion for
  # helix_code/.env.full-test was exact-path. The exclusion existed because that
  # tracked file genuinely CARRIED the literal — the live infra stack
  # (compose.helixcode-infra.yml, LAN-reachable :5433) and the ephemeral
  # full-test stack (docker-compose.full-test.yml, :5432) shared ONE password
  # value, with identical DB name and user, so a tracked test fixture disclosed
  # the live credential.
  #
  # The reuse was broken at the source instead: .env.full-test now carries a
  # distinct test-only value. That made the exclusion dead code — and a dead
  # exclusion is a standing loophole, because a REAL credential placed there
  # later would go unseen. So the exclusion is removed and this test now asserts
  # the OPPOSITE: planting the literal in that exact path MUST be flagged, like
  # anywhere else.
  mkdir -p "$tmp/repo3/helix_code" "$tmp/repo3/other"
  git -C "$tmp/repo3" init -q 2>/dev/null
  printf 'HELIX_DATABASE_PASSWORD=%s\n' "$HISTORICAL_LITERAL_2" > "$tmp/repo3/helix_code/.env.full-test"
  printf 'HELIX_DATABASE_PASSWORD=%s\n' "$HISTORICAL_LITERAL_2" > "$tmp/repo3/other/config.yml"
  git -C "$tmp/repo3" add helix_code/.env.full-test other/config.yml 2>/dev/null
  git -C "$tmp/repo3" -c user.email=gate@test -c user.name=gate commit -qm planted3 2>/dev/null

  local planted_hits3
  planted_hits3="$(scan_repo_for_historical_literal "$tmp/repo3")"
  if ! printf '%s' "$planted_hits3" | grep -q '^helix_code/\.env\.full-test:'; then
    printf '%sFAIL%s — .env.full-test was NOT flagged; an exclusion has been reintroduced and it is a loophole.\n' "$RED" "$OFF"
    overall=1
  elif ! printf '%s' "$planted_hits3" | grep -q '^other/config\.yml:'; then
    printf '%sFAIL%s — an unrelated tracked file was NOT flagged; the scan is broken.\n' "$RED" "$OFF"
    overall=1
  else
    printf '%sPASS%s — no path is exempt: both .env.full-test and other/config.yml flagged:\n' "$GREEN" "$OFF"
    printf '%s\n' "$planted_hits3" | sed 's/^/    /'
  fi

  printf '\n%s== SELF-TEST 7/8: CHECK 1 tells a config handover from incident prose ==%s\n' "$BOLD" "$OFF"
  # The 2026-09-08 root-cause fix. CHECK 1 used to exempt every .md/.html/.pdf by
  # EXTENSION, so a documentation file could hand the credential over in a
  # copy-pasteable config block under a green gate. The exemption is now made by
  # LINE SHAPE. Prove BOTH directions in one planted repo, because a shape rule
  # that only ever flags is a false-positive machine (§11.4.201) and one that only
  # ever exempts is the bluff this replaced.
  mkdir -p "$tmp/repo4"
  git -C "$tmp/repo4" init -q 2>/dev/null
  # (a) a config handover inside a .md — MUST be flagged.
  printf '```yaml\n    environment:\n      POSTGRES_PASSWORD: %s\n```\n' \
    "$HISTORICAL_LITERAL_2" > "$tmp/repo4/HANDOVER.md"
  # (b) the same value named mid-sentence — MUST NOT be flagged.
  printf 'The compose default `HELIX_POSTGRES_PASSWORD:-%s` was removed in 11861996.\n' \
    "$HISTORICAL_LITERAL_1" > "$tmp/repo4/INCIDENT.md"
  # (c) the rendered .html twin of (a) — MUST be flagged through the markup.
  printf '<span class="fu">POSTGRES_PASSWORD</span><span class="kw">:</span> %s\n' \
    "$HISTORICAL_LITERAL_2" > "$tmp/repo4/HANDOVER.html"
  git -C "$tmp/repo4" add HANDOVER.md INCIDENT.md HANDOVER.html 2>/dev/null
  git -C "$tmp/repo4" -c user.email=gate@test -c user.name=gate commit -qm planted4 2>/dev/null

  local planted_hits4
  planted_hits4="$(scan_repo_for_historical_literal "$tmp/repo4")"
  if ! printf '%s' "$planted_hits4" | grep -q '^HANDOVER\.md:'; then
    printf '%sFAIL%s — a copy-pasteable credential line in a .md was NOT flagged; the\n' "$RED" "$OFF"
    printf '       extension-based exemption is back and the gate is green over a leak.\n'
    overall=1
  elif ! printf '%s' "$planted_hits4" | grep -q '^HANDOVER\.html:'; then
    printf '%sFAIL%s — the rendered .html twin was NOT flagged; markup is an escape hatch.\n' "$RED" "$OFF"
    overall=1
  elif printf '%s' "$planted_hits4" | grep -q '^INCIDENT\.md:'; then
    printf '%sFAIL%s — incident PROSE was flagged; that is a false-positive refusal, itself\n' "$RED" "$OFF"
    printf '       a FAIL-bluff (§11.4.201(1)).\n'
    overall=1
  else
    printf '%sPASS%s — handover flagged (.md and .html), incident prose exempt:\n' "$GREEN" "$OFF"
    printf '%s\n' "$planted_hits4" | cut -d: -f1,2 | sed 's/^/    /'
  fi

  printf '\n%s== SELF-TEST 8/8: CHECK 3 flags a wildcard publish, not a loopback one ==%s\n' "$BOLD" "$OFF"
  printf 'services:\n  postgres:\n    ports:\n      - "5433:5432"\n' > "$tmp/wildcard.yml"
  printf 'services:\n  postgres:\n    ports:\n      - "127.0.0.1:5433:5432"\n' > "$tmp/loopback.yml"
  if scan_file_for_wildcard_publish "$tmp/wildcard.yml" "5433:5432" >"$tmp/out8a" 2>&1; then
    printf '%sFAIL%s — CHECK 3 did NOT flag a wildcard publish. The check is a bluff.\n' "$RED" "$OFF"
    overall=1
  elif ! scan_file_for_wildcard_publish "$tmp/loopback.yml" "5433:5432" >"$tmp/out8b" 2>&1; then
    printf '%sFAIL%s — CHECK 3 flagged a correct loopback publish; false-positive refusal.\n' "$RED" "$OFF"
    overall=1
  else
    printf '%sPASS%s — wildcard flagged, loopback accepted:\n' "$GREEN" "$OFF"
    sed 's/^/    /' "$tmp/out8a"
  fi

  printf '\n%s== SELF-TEST RESULT ==%s\n' "$BOLD" "$OFF"
  if [ "$overall" -eq 0 ]; then
    printf '%sPASS%s — gate proven in BOTH directions (clean tree: PASS, planted literals: FAIL).\n' "$GREEN" "$OFF"
  else
    printf '%sFAIL%s — see above.\n' "$RED" "$OFF"
  fi
  # Guard against a vacuous self-test.
  if [ "${rc_clean:-1}" -eq 0 ] && [ "${rc_planted:-0}" -eq 0 ]; then
    printf '%sFAIL%s — self-test is vacuous: the gate never failed on the mutation.\n' "$RED" "$OFF"
    overall=1
  fi
  return $overall
}

case "${1:-}" in
  --self-test) self_test; exit $? ;;
  -h|--help)   sed -n '2,32p' "$0"; exit 0 ;;
  '')          run_gate; exit $? ;;
  *)           printf 'unknown option: %s (try --help)\n' "$1" >&2; exit 2 ;;
esac
