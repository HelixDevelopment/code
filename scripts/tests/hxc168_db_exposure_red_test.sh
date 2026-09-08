#!/usr/bin/env bash
# scripts/tests/hxc168_db_exposure_red_test.sh
#
# HXC-168 §11.4.115 RED-baseline / GREEN-guard polarity test — ONE source, two
# roles, selected by RED_MODE:
#
#   RED_MODE=1 (default) — REPRODUCE the defect on the CURRENT artifact.
#                          Exit 0 means "the defect is PRESENT", which is the
#                          proof this test is not blind.
#   RED_MODE=0           — the standing regression guard (§11.4.135).
#                          Exit 0 means "the defect is ABSENT".
#
# WHY THIS EXISTS
# ---------------
# HXC-168 exposed a database password in tracked, published files. Commit
# 11861996 removed the FIRST literal; commit 81da08c9 removed the SECOND from
# live config and source. Three things survived that work, and each is a
# §11.4.201 "the guard does not assert the real condition" defect rather than a
# fresh leak:
#
#   D1  The second literal still sits inside INSTRUCTION-SHAPED blocks in
#       tracked docs — `POSTGRES_PASSWORD: <value>`, `postgresql://u:<value>@`,
#       `HELIX_DATABASE_PASSWORD=<value>` — i.e. lines a reader copy-pastes into
#       a real config. no_hardcoded_db_credential_gate.sh CHECK 1 exempts every
#       `.md`/`.html`/`.pdf` file as "documentation prose", so it reports PASS
#       over them. Prose that DISCUSSES the incident is legitimate; a config
#       block that HANDS OVER the value is not, and the exclusion could not tell
#       them apart.
#
#   D2  The gate is wired into no sweep. `verify-all-constitution-rules.sh`
#       invokes 24 gates and this is not one of them, so nothing runs it unless
#       a human remembers to.
#
#   D3  The database port publishes bind the wildcard address, so the DB is
#       reachable from the LAN rather than from loopback only. Operator-approved
#       remedy [C].
#
# HONEST BOUNDARY (§11.4.6): passing this test does NOT mean the credential is
# safe. The value is in git history on four mirrors and REMAINS VALID until the
# operator rotates it. This test pins that no tracked file HANDS IT OUT again
# and that the listener is not LAN-reachable. Nothing here withdraws the key.
#
# Usage:
#   RED_MODE=1 scripts/tests/hxc168_db_exposure_red_test.sh   # reproduce (pre-fix)
#   RED_MODE=0 scripts/tests/hxc168_db_exposure_red_test.sh   # guard (post-fix)
#
# The literal is NEVER written whole in this file (§11.4.10 / CONST-042): it is
# assembled from two fragments at runtime, and every report line is masked, so
# this test's own output can be committed as evidence without republishing the
# credential.
set -uo pipefail

ROOT="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT" || exit 2

RED_MODE="${RED_MODE:-1}"
RED=$'\033[31m'; GREEN=$'\033[32m'; BOLD=$'\033[1m'; OFF=$'\033[0m'

# Assembled at runtime — see the header note. MASK is applied to every line this
# test prints, so a finding is reportable without disclosing the value.
LIT_1="helix""pass"
LIT_2="helixcode_test_""password"
mask() { sed -e "s/${LIT_2}/<REDACTED-L2>/g" -e "s/${LIT_1}/<REDACTED-L1>/g"; }

SWEEP="scripts/verify-all-constitution-rules.sh"
GATE="scripts/gates/no_hardcoded_db_credential_gate.sh"

# The DB port publishes named by the HXC-168 operator-block details as option
# [C]'s scope: file, and the publish line that must bind loopback.
PORT_TARGETS=(
  "compose.helixcode-infra.yml|5433:5432"
  "helix_code/docker/autoboot/docker-compose.autoboot.yml|55432:5432"
  "helix_code/docker/docker-compose.yml|5432:5432"
)

findings=0
checks=0
finding() { printf '  %sFOUND%s %s\n' "$RED" "$OFF" "$1" | mask; findings=$((findings + 1)); checks=$((checks + 1)); }
clean()   { printf '  %sclean%s %s\n' "$GREEN" "$OFF" "$1" | mask; checks=$((checks + 1)); }
# Neither PASS nor regression: the SOURCE fix has landed but the RUNNING
# artifact has not picked it up, and the step that would is operator-gated.
# Reported loudly, counted separately, does NOT flip the verdict -- calling a
# never-yet-fixed runtime a "regression" is false (11.4.6), and silently
# passing it would be the false-null this test exists to catch (11.4.21).
blocked=0
op_blocked() { printf '  %sOPERATOR-BLOCKED%s %s\n' "$BOLD" "$OFF" "$1" | mask; blocked=$((blocked + 1)); checks=$((checks + 1)); }

# Discriminator between "config landed, awaiting recreate" and a REAL
# regression. NOT a hardcoded exemption: a port whose source still publishes
# the wildcard remains a finding.
source_declares_loopback() {
  local port="$1" entry file mapping
  for entry in "${PORT_TARGETS[@]}"; do
    file="${entry%%|*}"; mapping="${entry##*|}"
    case "$mapping" in
      "$port":*) [ -f "$file" ] && grep -q "127\.0\.0\.1:${port}:" "$file" && return 0 ;;
    esac
  done
  return 1
}

# --- D1: instruction-shaped credential blocks in tracked docs ---------------
# DISCRIMINATOR (§11.4.201 — the rule must assert the REAL condition, so that
# neither a false-negative pass nor a false-positive refusal is possible):
#
#   INSTRUCTION-SHAPED (a finding) — the line, once HTML tags are stripped and
#     leading whitespace / a YAML "- " item marker / an "export " keyword are
#     removed, BEGINS with an assignment (`IDENT:` or `IDENT=`) or with a bare
#     `postgres://` connection string, AND carries a historical literal. Such a
#     line is copy-pasteable straight into a real config: it HANDS THE VALUE OVER.
#
#   PROSE (not a finding) — narrative text that merely names the value mid-
#     sentence, e.g. "The compose default `HELIX_POSTGRES_PASSWORD:-<lit>` and …".
#     Documentation that DISCUSSES the incident is legitimate and must not be
#     refused; refusing it would be the FAIL-bluff §11.4.201 forbids. The
#     line-start anchor is what separates the two, and it is applied to the
#     tag-stripped text so a rendered .html code block is judged by the same
#     rule as its .md source rather than escaping through markup.
#
# Captured §11.4.83 evidence under docs/qa/ is excluded: those transcripts record
# the literal being CAUGHT (including this project's own guard self-test output).
# Editing them would falsify historical evidence, and they are not a live usage.
printf '%s== D1: instruction-shaped credential blocks in tracked docs ==%s\n' "$BOLD" "$OFF"
d1_hits="$(
  git ls-files -z \
    | xargs -0 grep -InE -e "$LIT_1" -e "$LIT_2" 2>/dev/null \
    | grep -vE '^(submodules|cli_agents|cli_agents_resources|dependencies)/' \
    | grep -vE '^docs/qa/' \
    | awk -F: '{
        path=$1; ln=$2;
        text=$0; sub(/^[^:]*:[^:]*:/, "", text);
        gsub(/<[^>]*>/, "", text);              # judge rendered .html like its .md source
        sub(/^[[:space:]]+/, "", text);
        sub(/^-[[:space:]]+/, "", text);        # YAML sequence item
        sub(/^export[[:space:]]+/, "", text);   # shell export
        if (text ~ /^[A-Za-z_][A-Za-z0-9_]*[[:space:]]*[:=]/ || text ~ /^postgres(ql)?:\/\//)
          print path ":" ln;
      }' \
    || true
)"
if [ -n "$d1_hits" ]; then
  while IFS= read -r h; do
    [ -n "$h" ] && finding "D1 $h — credential handed over in a copy-pasteable config line"
  done <<< "$d1_hits"
else
  clean "D1 — no tracked file hands a historical literal over in a config-shaped line"
fi

# --- D2: the gate is wired into the sweep ----------------------------------
printf '\n%s== D2: credential gate wired into the pre-build sweep ==%s\n' "$BOLD" "$OFF"
if [ ! -f "$SWEEP" ]; then
  finding "D2 $SWEEP — sweep script missing"
elif grep -q 'no_hardcoded_db_credential_gate' "$SWEEP"; then
  clean "D2 — $SWEEP invokes the credential gate"
else
  finding "D2 $SWEEP — credential gate is invoked by no sweep; nothing runs it automatically"
fi

# --- D3: DB port publishes bind loopback, not the wildcard -----------------
printf '\n%s== D3: DB port publishes bind loopback ==%s\n' "$BOLD" "$OFF"
for t in "${PORT_TARGETS[@]}"; do
  f="${t%%|*}"; mapping="${t##*|}"
  if [ ! -f "$f" ]; then
    finding "D3 $f — scoped file missing (scope drift?)"
    continue
  fi
  # A wildcard publish is the bare "HOSTPORT:CONTPORT" form with no bind address.
  if grep -qE "^[[:space:]]*-[[:space:]]*\"?${mapping}\"?[[:space:]]*$" "$f"; then
    finding "D3 $f — publishes ${mapping} on the WILDCARD address (LAN-reachable)"
  elif grep -qE "^[[:space:]]*-[[:space:]]*\"?127\.0\.0\.1:${mapping}\"?[[:space:]]*$" "$f"; then
    clean "D3 $f — ${mapping} bound to 127.0.0.1"
  else
    finding "D3 $f — no recognisable publish line for ${mapping}"
  fi
done

# --- D3-runtime: the RUNNING listener, not just the source (§11.4.108) ------
# A config edit that never reached the running container is the failure mode
# this clause exists to catch. Absence of the listener is NOT evidence of a
# loopback bind, so it is reported as its own honest state, never as a pass.
printf '\n%s== D3-runtime: running listener binds ==%s\n' "$BOLD" "$OFF"
if command -v ss >/dev/null 2>&1; then
  for port in 5433 55432; do
    binds="$(ss -ltnH 2>/dev/null | awk -v p=":$port" '$4 ~ p"$" {print $4}')"
    if [ -z "$binds" ]; then
      printf '  %s—%s D3-runtime :%s not listening (certifies nothing; SKIP-OK §11.4.3)\n' "$BOLD" "$OFF" "$port"
    elif printf '%s\n' "$binds" | grep -qvE '^(127\.0\.0\.1|\[::1\]):'; then
      if source_declares_loopback "$port"; then
        op_blocked "D3-runtime :$port still NON-loopback ($(printf '%s' "$binds" | tr '\n' ' ')) while the SOURCE already binds 127.0.0.1 — the running container predates the fix. Podman cannot rebind a published port in place, so this closes only on an operator-gated recreate. NOT a regression."
      else
        finding "D3-runtime :$port NON-loopback ($(printf '%s' "$binds" | tr '\n' ' ')) AND the source does not declare loopback either"
      fi
    else
      clean "D3-runtime :$port listening on loopback only ($(printf '%s' "$binds" | tr '\n' ' '))"
    fi
  done
else
  printf '  %s—%s D3-runtime ss(8) not on PATH (certifies nothing; SKIP-OK §11.4.3)\n' "$BOLD" "$OFF"
fi

# --- machine-readable summary ----------------------------------------------
# Counts are ALWAYS in guard polarity (a finding counts as "failed"), regardless
# of RED_MODE, so an external harness reads one stable contract. In RED_MODE=1 a
# non-zero "failed" is the DESIRED outcome — read the VERDICT line below for the
# polarity-aware result, never this line alone.
printf '\nSUMMARY: %d passed, %d failed, %d operator-blocked (guard polarity; RED_MODE=%s)\n' \
  "$((checks - findings - blocked))" "$findings" "$blocked" "$RED_MODE"

# --- polarity verdict -------------------------------------------------------
printf '\n%s== VERDICT (RED_MODE=%s) ==%s\n' "$BOLD" "$RED_MODE" "$OFF"
if [ "$RED_MODE" = "1" ]; then
  if [ "$findings" -gt 0 ]; then
    printf '%sRED PASS%s — defect REPRODUCED on the current artifact: %d finding(s).\n' "$GREEN" "$OFF" "$findings"
    exit 0
  fi
  printf '%sRED FAIL%s — no finding on an artifact believed defective. Either the defect\n' "$RED" "$OFF"
  printf '           is already fixed, or this test is blind (§11.4.115 honest boundary).\n'
  exit 1
fi
if [ "$findings" -eq 0 ]; then
  if [ "$blocked" -gt 0 ]; then
    printf '%sGREEN PASS (with %d OPERATOR-BLOCKED)%s — no regression, but NOT all clear:\n' "$GREEN" "$blocked" "$OFF"
    printf '             the item(s) above are source-fixed and NOT yet live, pending an\n'
    printf '             operator-gated recreate. They are not certified closed (11.4.21).\n'
  else
    printf '%sGREEN PASS%s — defect ABSENT.\n' "$GREEN" "$OFF"
  fi
  printf '             NOTE: the published credential REMAINS VALID until the operator\n'
  printf '             rotates it; this guard does not close that.\n'
  exit 0
fi
printf '%sGREEN FAIL%s — %d finding(s); in the HXC-168 exposure class.\n' "$RED" "$OFF" "$findings"
exit 1
