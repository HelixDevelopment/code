#!/usr/bin/env bash
# =============================================================================
# test_agent_configs.sh — anti-bluff behavioural test suite for the
# CLI-agent config fan-out (scripts/install_agent_configs.sh)
# =============================================================================
#
# WHAT IS UNDER TEST
# ------------------
# A (concurrently-authored, not yet landed as of this writing) installer,
# `scripts/install_agent_configs.sh`, that merges Helix's keyless local LLM
# surfaces into every installed CLI agent's OWN config file:
#
#   provider id         base URL                              model id(s)
#   helixllm-coder      http://127.0.0.1:18434/v1              qwen2.5-coder-3b-instruct-q4_k_m
#   helixllm-gateway    https://127.0.0.1:8443/v1 (self-signed, helixllm-anton-qwen2-5-coder-3b-instruct-q4_k_m-f6771589d190
#                       CA at submodules/helix_llm/certs/cert.pem)
#   helixagent          http://127.0.0.1:7061/v1                helixagent-llm, helixagent-debate,
#                                                                helixagent-ensemble, helix-llm, helix-debate
#
#   agent      config file                              override env var(s)
#   opencode   ~/.config/opencode/opencode.json (JSON)   XDG_CONFIG_HOME (then HOME)
#   pi         ~/.pi/agent/models.json (JSON)             PI_CODING_AGENT_DIR
#   crush      ~/.config/crush/crushrc (Bash directives)  XDG_CONFIG_HOME (then HOME)
#
# WHICH ENV VARS THE INSTALLER ACTUALLY HONOURS (measured, not assumed)
# ----------------------------------------------------------------------
# The installer derives opencode's and crush's directories from
# `${XDG_CONFIG_HOME:-$HOME/.config}` and pi's from `PI_CODING_AGENT_DIR`.
# It does NOT read OPENCODE_CONFIG or CRUSH_GLOBAL_CONFIG at all — it only
# EXPORTS those to the child agent process during --verify. This suite still
# sets them (harmless, and correct for anything that spawns a real agent), but
# the vars that actually steer a WRITE are XDG_CONFIG_HOME, HOME and
# PI_CODING_AGENT_DIR; sandboxing the first two was previously missing.
#
# PI_CODING_AGENT_DIR is the AGENT dir, not the ~/.pi root. pi's own
# `--help` on this host: "PI_CODING_AGENT_DIR - Config directory (default:
# ~/.pi/agent)", matching this repo's
# docs/research/cli_agent_config_schemas/EVIDENCE.md quoting pi's
# docs/environment-variables.md verbatim ("Override the config directory;
# default is `~/.pi/agent`"). So the file is $PI_CODING_AGENT_DIR/models.json
# — NOT $PI_CODING_AGENT_DIR/agent/models.json, which is what this suite used
# to seed and assert, making test 2 (its own self-declared most important
# test) fail deterministically for a reason that had nothing to do with the
# installer.
#
# Documented installer CLI surface (the primary contract tests 1-5, 7 and 8
# depend on — HARD CONSTRAINT: another stream owns install_agent_configs.sh's
# internals and setup.sh, so this suite never EDITS either file, and tests
# 1-5/7/8 stay black-box, asserting only against this documented surface plus
# the three documented config-file locations/names):
#   --dry-run   print what would change, write nothing
#   --verify    run each agent's verification command, report PASS/FAIL/SKIP
#   (default)   merge Helix providers into each installed agent's config
#
# CORRECTION (finding N7, §11.4.6): an earlier revision of this header claimed
# the suite "never reads" the installer's source at all. That is false, and
# was contradicted by this file's own tests 6 and 6c, which are deliberately
# WHITE-BOX: they name and reverse-engineer install_agent_configs.sh's
# secret-redaction internals directly (redact()'s rule (1)/(2), the
# _redact_env_values() awk helper, envshaped()/readable()/long_run(), the
# "ROUND-8" labels the installer's own comments use) because no black-box
# probe can prove a redactor resists an adversarial key-spelling forgery — you
# have to read the redactor to know what forgery to try. That is an
# intentional, narrow exception for the two secret-hygiene tests, not license
# for tests 1-5/7/8 to start depending on the installer's other internals.
#
# ANTI-BLUFF ANCHOR (this file was authored BEFORE the installer existed)
# -------------------------------------------------------------------------
# scripts/install_agent_configs.sh did not exist when this suite was written.
# Every test below therefore opens with require_installer(), which SKIPs
# (never fakes a PASS, never hard-FAILs the whole run) when the installer is
# absent or not executable. Once the installer lands, re-running this exact,
# unmodified file is what turns those SKIPs into real PASS/FAIL verdicts —
# nothing about the suite needs to change (§11.4.6, §11.4.98, §11.4.123).
#
# SAFETY (HARD CONSTRAINTS — see task brief)
# -------------------------------------------
#  * Never touches the operator's real agent configs. Every test sandboxes
#    ALL of: the per-agent override env var(s) above AND $HOME itself
#    (belt-and-suspenders — even an undocumented fallback path still lands
#    inside the sandbox, never at the operator's real ~/.config/*).
#  * Never restarts/enables/stops any systemd unit. The three live Helix
#    endpoints above are read via plain GET only (see test 8); test 5's
#    "unreachable endpoint" is simulated with a throwaway local TCP port we
#    bind-then-close ourselves, never by touching real infrastructure.
#  * Every sandbox is a fresh mktemp -d, removed in a trap the instant its
#    owning test function's subshell exits (each test function runs inside
#    a `$(...)` command substitution, which IS its own subshell in bash, so
#    a `trap ... EXIT` set inside the function fires on function return, not
#    merely at the very end of the whole script).
#
# SKIP CONVENTION
# ---------------
# Test functions return 0 (PASS), 1 (FAIL, with the reason on stdout), or
# $SKIP_RC=77 (SKIP, with the reason on stdout) — the long-standing
# autotools convention for "this could not be certified, and that is being
# said out loud rather than silently counted as green" (§11.4.3).
#
# EXIT CODES (whole-suite)
#   0  every test that ran PASSed (SKIPs allowed)
#   1  at least one test FAILed
#   2  every single test SKIPped (nothing at all could be certified this run
#      — expected until scripts/install_agent_configs.sh lands)
# =============================================================================
set -uo pipefail

ROOT="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd)"
SUITE="TEST-AGENT-CONFIG-FANOUT"
INSTALLER="${INSTALLER_OVERRIDE:-$ROOT/scripts/install_agent_configs.sh}"
CA_CERT="$ROOT/submodules/helix_llm/certs/cert.pem"
SKIP_RC=77

echo "$SUITE"

# --- shared fixtures: the closed, contract-given provider/model table -------
# This is the exact table this WRITE task specifies the installer must wire.
# It is data, not a guess (§11.4.6) — but confirmation is NOT uniform across
# the table. The helixllm-coder and helixllm-gateway ids were independently
# confirmed live against the real running endpoints while this suite was
# authored (see the report's evidence section). The HelixAgent (:7061) ids were
# NOT individually enumerated (evidence is one `/v1/models -> 200`, no id list)
# and are UNCONFIRMED per docs/guides/cli_agent_integration/README.md. An
# earlier revision of this header claimed all of them were confirmed.
declare -A PROVIDER_URL=(
  [helixllm-coder]="http://127.0.0.1:18434/v1"
  [helixllm-gateway]="https://127.0.0.1:8443/v1"
  [helixagent]="http://127.0.0.1:7061/v1"
)
declare -A PROVIDER_MODELS=(
  [helixllm-coder]="qwen2.5-coder-3b-instruct-q4_k_m"
  [helixllm-gateway]="helixllm-anton-qwen2-5-coder-3b-instruct-q4_k_m-f6771589d190"
  [helixagent]="helixagent-llm helixagent-debate helixagent-ensemble helix-llm helix-debate"
)

# --- installer presence guard ------------------------------------------------
require_installer() {
  if [[ ! -f "$INSTALLER" ]]; then
    echo "installer not present yet: $INSTALLER (expected from a concurrently-authored work stream; nothing to execute against)"
    return "$SKIP_RC"
  fi
  if [[ ! -x "$INSTALLER" ]]; then
    echo "installer present but not executable: $INSTALLER"
    return "$SKIP_RC"
  fi
  return 0
}

# --- sandbox helpers ---------------------------------------------------------
new_sandbox() { mktemp -d "${TMPDIR:-/tmp}/agent_cfg_test.XXXXXX"; }

sandbox_env_setup() {
  # $1 = sandbox root. Points every documented override env var (AND $HOME
  # itself, defensively) inside the sandbox. HARD CONSTRAINT #4: this MUST
  # make it structurally impossible for a run to reach the operator's real
  # ~/.config/opencode, ~/.pi, or ~/.config/crush, regardless of whether the
  # installer honours every override perfectly.
  local root="$1"
  mkdir -p "$root/home/.config/opencode" "$root/home/.config/crush" \
           "$root/home/.local/share/crush" "$root/pi_agent_dir"
  export HOME="$root/home"
  # XDG_CONFIG_HOME is the var the installer ACTUALLY resolves opencode's and
  # crush's directories from (`${XDG_CONFIG_HOME:-$HOME/.config}`). Leaving it
  # alone meant that on any host where the operator has it set — common on
  # desktops — every write-mode test here would have landed in the operator's
  # REAL config, which is precisely what the paragraph below promises is
  # structurally impossible. It is unset on the host this was measured on, so
  # no real config was ever touched; that was luck, not a guarantee.
  export XDG_CONFIG_HOME="$root/home/.config"
  export OPENCODE_CONFIG="$root/home/.config/opencode/opencode.json"
  export PI_CODING_AGENT_DIR="$root/pi_agent_dir"
  export CRUSH_GLOBAL_CONFIG="$root/home/.config/crush"
  export CRUSH_GLOBAL_DATA="$root/home/.local/share/crush"
  # The one path this suite asserts on for pi. See the header: this is the
  # AGENT dir, so models.json sits directly in it.
  export PI_MODELS_JSON="$root/pi_agent_dir/models.json"
}

locate_cfg() {
  # $1 = sandbox root, $2 = filename (opencode.json | models.json | crushrc).
  # Locating by filename rather than assuming the installer's exact directory
  # join keeps this suite honest about NOT depending on install_agent_configs.sh
  # internals — only the three filenames are part of the documented contract.
  # `find` order is filesystem order, i.e. nondeterministic when more than one
  # file matches. Sort first so a run is reproducible (§11.4.50), and say so
  # out loud when the choice was ambiguous rather than picking silently.
  local matches
  matches="$(find "$1" -type f -name "$2" 2>/dev/null | LC_ALL=C sort)"
  if [[ "$(printf '%s\n' "$matches" | grep -c .)" -gt 1 ]]; then
    echo "NOTE: more than one $2 under $1; taking the first in sorted order:" >&2
    printf '%s\n' "$matches" | sed 's/^/  /' >&2
  fi
  printf '%s\n' "$matches" | head -n1
}

run_installer() {
  # Runs the installer with the given args against whatever env/PATH the
  # caller already set up. RUN_OUT / RUN_RC are globals; safe because every
  # test function's body executes inside its own command-substitution
  # subshell (see header), so nothing leaks between tests.
  RUN_OUT="$(bash "$INSTALLER" "$@" 2>&1)"
  RUN_RC=$?
}

# --- realistic pre-existing config seeds (test 2's non-clobber fixture) -----
# Deliberately carries several UNRELATED entries (MCP servers, a plugin, a
# custom theme, an unrelated provider) so "did the merge preserve everything
# that was already there" has real substance to check, not an empty file.
seed_realistic_opencode() {
  local f="$1"
  mkdir -p "$(dirname "$f")"
  cat > "$f" <<'JSON'
{
  "$schema": "https://opencode.ai/config.json",
  "theme": "custom-midnight",
  "mcp": {
    "playwright": { "type": "local", "command": ["playwright-mcp"] },
    "filesystem": { "type": "local", "command": ["fs-mcp", "--root", "."] }
  },
  "plugin": ["my-team/opencode-plugin-linter"],
  "provider": {
    "unrelated-vendor": {
      "npm": "@ai-sdk/openai-compatible",
      "options": { "baseURL": "https://example.invalid/v1" },
      "models": { "some-model": {} }
    }
  }
}
JSON
}

seed_realistic_pi() {
  local f="$1"
  mkdir -p "$(dirname "$f")"
  cat > "$f" <<'JSON'
{
  "providers": {
    "unrelated-vendor": {
      "baseUrl": "https://example.invalid/v1",
      "models": ["some-model"]
    }
  },
  "defaultModel": "unrelated-vendor/some-model"
}
JSON
}

# UNCONFIRMED (see report): crush's real "Bash directives" schema was not
# reverse-engineered for this suite (no confirmed spec was available at
# authoring time). This fixture is a representative stand-in: ordinary shell
# assignments/exports/function calls, which is what "Bash directives" reads
# as literally. Preservation is checked line-by-line (any exact pre-existing
# line surviving verbatim), which is schema-agnostic by construction.
seed_realistic_crush() {
  local f="$1"
  mkdir -p "$(dirname "$f")"
  cat > "$f" <<'SH'
# user customisations — must survive the installer verbatim
export CRUSH_THEME="solarized"
crush_provider_add "acme-vendor" "https://example.invalid/v1"
SH
}

# --- JSON structural containment (test 2) -----------------------------------
json_containment_ok() {
  # $1 = old json file, $2 = new json file. Exit 0 iff every key/value pair
  # in $1 is present, unchanged, in $2 (recursively for dicts; exact equality
  # for lists and scalars) — i.e. $2 is allowed to ADD keys but never lose or
  # mutate one that was already there.
  python3 - "$1" "$2" <<'PY'
import json, sys
old = json.load(open(sys.argv[1], encoding="utf-8"))
new = json.load(open(sys.argv[2], encoding="utf-8"))
def contains(o, n):
    if isinstance(o, dict):
        return isinstance(n, dict) and all(k in n and contains(v, n[k]) for k, v in o.items())
    return o == n
sys.exit(0 if contains(old, new) else 1)
PY
}

# --- secret-shaped content detector (test 6) --------------------------------
# The SAME detector is run against (a) a synthetic self-test fixture and
# (b) the installer's real output, so a detector weakened to a no-op is
# caught by (a) and never silently trusted for (b) — see test 6's body.
HIGH_CONFIDENCE_SECRET_RE='(sk-[A-Za-z0-9_-]{16,}|AKIA[0-9A-Z]{12,}|xox[baprs]-[A-Za-z0-9-]{10,}|Bearer[[:space:]]+[A-Za-z0-9._-]{20,}|-----BEGIN[[:space:]][A-Z ]*PRIVATE KEY-----)'
GENERIC_KEY_FIELD_RE='api[_-]?key["'"'"' :=]+[A-Za-z0-9_-]{16,}'
# The ONE known, documented, intentionally-non-secret placeholder this
# project's keyless local servers use to satisfy client libraries that
# syntactically require a non-empty apiKey/--api-key field (measured live in
# the real installer's own output: "helix-local-no-auth", plus a couple of
# equally-benign synonyms) — allow-listed so this detector does not cry wolf
# on the exact value CONST-042 does not consider a secret, while a real
# high-entropy key never matches this allow-list and is still flagged below.
KNOWN_SAFE_PLACEHOLDER_RE='helix-local-no-auth|no-auth|not-required|none|n/a|unused'
secret_scanner_flags() {
  local f="$1"
  grep -qE "$HIGH_CONFIDENCE_SECRET_RE" "$f" 2>/dev/null && return 0
  grep -E "$GENERIC_KEY_FIELD_RE" "$f" 2>/dev/null | grep -viE "$KNOWN_SAFE_PLACEHOLDER_RE" | grep -q .
}

snapshot_tree() {
  # $1 = root dir. One "path|mtime|sha256" line per file, sorted — detects
  # added/removed files AND any byte or timestamp change to an existing one.
  local root="$1" f
  while IFS= read -r -d '' f; do
    printf '%s|%s|%s\n' "$f" "$(stat -c '%Y' "$f" 2>/dev/null)" "$(sha256sum "$f" 2>/dev/null | cut -d' ' -f1)"
  done < <(find "$root" -type f -print0 2>/dev/null) | sort
}

fetch_live_models() {
  # $1 = base url ending in /v1. Prints the raw JSON body of GET $1/models,
  # or nothing (with a non-zero curl exit) if the endpoint is unreachable.
  local url="$1/models"
  if [[ "$url" == https://* ]]; then
    if [[ -f "$CA_CERT" ]]; then
      curl -fsS --max-time 5 --cacert "$CA_CERT" "$url" 2>/dev/null
    else
      curl -fsS --max-time 5 -k "$url" 2>/dev/null
    fi
  else
    curl -fsS --max-time 5 "$url" 2>/dev/null
  fi
}

# =============================================================================
# TEST 1 — idempotency
# MUTATION THAT MAKES THIS FAIL: make the installer's writer APPEND instead
# of REPLACE (e.g. keep growing a "providers" array without first removing
# its own prior entries) — the second run's file would then differ from the
# first's (duplicated entries), and this test's byte-identical comparison
# would catch it.
# =============================================================================
test_idempotency() {
  require_installer || return $?
  sb="$(new_sandbox)"; trap 'rm -rf "$sb"' EXIT   # NOT local: EXIT trap fires after this function returns, when a local sb would already be unset (nounset would abort the trap before rm -rf ran)
  sandbox_env_setup "$sb"
  seed_realistic_opencode "$OPENCODE_CONFIG"
  seed_realistic_pi "$PI_MODELS_JSON"
  seed_realistic_crush "$CRUSH_GLOBAL_CONFIG/crushrc"

  run_installer
  [[ $RUN_RC -eq 0 ]] || { echo "run 1 exited $RUN_RC: $RUN_OUT"; return 1; }

  local oc pi cr snap
  oc="$(locate_cfg "$sb" opencode.json)"
  pi="$(locate_cfg "$sb" models.json)"
  cr="$(locate_cfg "$sb" crushrc)"
  if [[ -z "$oc$pi$cr" ]]; then
    echo "run 1 produced no recognised config file (opencode.json/models.json/crushrc) anywhere under $sb"
    return 1
  fi

  snap="$(mktemp -d)"
  local f
  for f in "$oc" "$pi" "$cr"; do
    [[ -n "$f" && -f "$f" ]] && cp -p -- "$f" "$snap/$(basename -- "$f").run1"
  done

  run_installer
  if [[ $RUN_RC -ne 0 ]]; then
    echo "run 2 exited $RUN_RC: $RUN_OUT"
    rm -rf "$snap"
    return 1
  fi

  local mismatches=0
  for f in "$oc" "$pi" "$cr"; do
    [[ -n "$f" && -f "$f" ]] || continue
    if ! cmp -s -- "$f" "$snap/$(basename -- "$f").run1"; then
      echo "NOT byte-identical between run 1 and run 2: $f"
      mismatches=$((mismatches + 1))
    fi
  done
  rm -rf "$snap"
  [[ $mismatches -eq 0 ]] || return 1
  return 0
}

# =============================================================================
# TEST 2 — non-clobber / merge preservation  (the most important test here)
# MUTATION THAT MAKES THIS FAIL: replace the merge with a whole-file
# overwrite (i.e. the installer writes ONLY the Helix providers and drops
# every pre-existing key). json_containment_ok() would then fail for
# opencode.json / models.json, and the crush per-line check would fail for
# crushrc — either one fails this test.
# =============================================================================
test_non_clobber_merge() {
  require_installer || return $?
  sb="$(new_sandbox)"; trap 'rm -rf "$sb"' EXIT   # NOT local: EXIT trap fires after this function returns, when a local sb would already be unset (nounset would abort the trap before rm -rf ran)
  sandbox_env_setup "$sb"

  local oc="$OPENCODE_CONFIG" pi_f="$PI_MODELS_JSON" cr="$CRUSH_GLOBAL_CONFIG/crushrc"
  seed_realistic_opencode "$oc"
  seed_realistic_pi "$pi_f"
  seed_realistic_crush "$cr"
  cp -p -- "$oc" "$sb/oc.before"
  cp -p -- "$pi_f" "$sb/pi.before"
  cp -p -- "$cr" "$sb/cr.before"

  run_installer
  [[ $RUN_RC -eq 0 ]] || { echo "installer exited $RUN_RC: $RUN_OUT"; return 1; }

  local rc=0

  if [[ -f "$oc" ]]; then
    json_containment_ok "$sb/oc.before" "$oc" || { echo "opencode.json: pre-existing content was NOT preserved verbatim (mcp servers / plugin / theme / unrelated provider)"; rc=1; }
    grep -qE 'helixllm-coder|helixagent|helixllm-gateway' "$oc" || { echo "opencode.json: no Helix provider subtree was added"; rc=1; }
  else
    echo "opencode.json is missing after install"; rc=1
  fi

  if [[ -f "$pi_f" ]]; then
    json_containment_ok "$sb/pi.before" "$pi_f" || { echo "pi models.json: pre-existing content was NOT preserved verbatim"; rc=1; }
    grep -qE 'helixllm-coder|helixagent|helixllm-gateway' "$pi_f" || { echo "pi models.json: no Helix provider content was added"; rc=1; }
  else
    echo "pi models.json is missing after install"; rc=1
  fi

  if [[ -f "$cr" ]]; then
    local line lost=0
    while IFS= read -r line; do
      [[ -z "$line" ]] && continue
      grep -qxF -- "$line" "$cr" || { echo "crushrc: lost pre-existing line verbatim: $line"; lost=1; }
    done < "$sb/cr.before"
    [[ $lost -eq 0 ]] || rc=1
    grep -qE 'helixllm-coder|helixagent|helixllm-gateway' "$cr" || { echo "crushrc: no Helix provider content was added"; rc=1; }
  else
    echo "crushrc is missing after install"; rc=1
  fi

  return $rc
}

# =============================================================================
# TEST 3 — absent-agent honesty
# MUTATION THAT MAKES THIS FAIL: make the installer write a config file for
# an agent whose binary is not actually reachable on PATH.
# =============================================================================
test_absent_agent_honesty() {
  require_installer || return $?
  sb="$(new_sandbox)"; trap 'rm -rf "$sb"' EXIT   # NOT local: EXIT trap fires after this function returns, when a local sb would already be unset (nounset would abort the trap before rm -rf ran)
  sandbox_env_setup "$sb"

  # Scrub PATH of opencode's directory ONLY, keeping pi/crush's (a real nvm
  # bin dir) so this exercises "one absent, two present" in a single run.
  # Resolve the binary FIRST, then derive its directory. The previous form fed
  # `dirname` the empty string when `pi` was absent, and `dirname ""` prints
  # ".", which IS a directory — so the guard below could never fire, the test
  # ran with "." appended to PATH, and if `opencode` happened to live in a
  # system dir the test FAILED for the wrong reason (§11.4.201: a gate must
  # assert the real condition, and must not refuse — or pass — on a false one).
  local pi_bin pi_dir
  pi_bin="$(command -v pi 2>/dev/null || true)"
  pi_dir=""
  [[ -n "$pi_bin" ]] && pi_dir="$(dirname -- "$pi_bin")"
  if [[ -z "$pi_dir" || ! -d "$pi_dir" ]]; then
    echo "cannot locate the real 'pi'/'crush' bin dir on this host to build a scrubbed PATH; SKIP"
    return "$SKIP_RC"
  fi
  local scrubbed="/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin:$pi_dir"

  PATH="$scrubbed" run_installer
  [[ $RUN_RC -eq 0 ]] || { echo "installer must degrade non-fatally when one agent is absent, exited $RUN_RC: $RUN_OUT"; return 1; }

  if ! echo "$RUN_OUT" | grep -qi 'opencode' \
     || ! echo "$RUN_OUT" | grep -qi 'skipped' \
     || ! echo "$RUN_OUT" | grep -qi 'not installed'; then
    # Relaxed to two independent substrings ("skipped" + "not installed")
    # rather than one exact literal: measured against the real installer,
    # the wording is "skipped — not installed" (em dash), not literally
    # "skipped: not installed" (colon) as the task brief's prose paraphrased
    # it — the SUBSTANCE (an explicit, honest skip naming the reason) is
    # what this test certifies, not one exact punctuation mark.
    echo "installer did not explicitly report opencode as skipped / not installed. Output:"
    echo "$RUN_OUT"
    return 1
  fi
  if [[ -f "$OPENCODE_CONFIG" ]]; then
    echo "installer wrote a config for an agent (opencode) that is NOT installed: $OPENCODE_CONFIG"
    return 1
  fi
  return 0
}

# =============================================================================
# TEST 4 — shell-builtin trap
# MUTATION THAT MAKES THIS FAIL: detect "is this agent installed" with bare
# `command -v NAME` and nothing else.
#
# None of opencode/pi/crush literally collides with a real bash keyword the
# way the contract's own example ("continue") does, so the closest faithful,
# black-box-testable reproduction of that EXACT hazard class — "command -v
# resolves to something that is not a real, invokable installed program" —
# is a shell FUNCTION of the same name, exported into the installer's own
# bash process via `export -f` (bash propagates exported functions to child
# bash processes via BASH_FUNC_<name>%%). A detector using bare `command -v`
# is fooled by this exactly as it would be fooled by `command -v continue`;
# a detector that also verifies the resolved path is a real, executable,
# regular file is not.
# =============================================================================
test_shell_builtin_trap() {
  require_installer || return $?
  sb="$(new_sandbox)"; trap 'rm -rf "$sb"' EXIT   # NOT local: EXIT trap fires after this function returns, when a local sb would already be unset (nounset would abort the trap before rm -rf ran)
  sandbox_env_setup "$sb"

  # Sanity-check the premise this test operationalises: a real shell builtin
  # IS "resolved" by command -v despite having no installed program behind
  # it at all — this is the exact class the contract names via "continue".
  command -v continue >/dev/null 2>&1 || {
    echo "premise check failed: 'command -v continue' did not resolve on this host/shell — cannot demonstrate the hazard class; SKIP"
    return "$SKIP_RC"
  }

  local sys_path="/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin"
  # shellcheck disable=SC2317  # invoked indirectly via the exported function
  crush() { echo "not a real installed agent"; }
  export -f crush

  PATH="$sys_path" run_installer
  [[ $RUN_RC -eq 0 ]] || { echo "installer must degrade non-fatally, exited $RUN_RC: $RUN_OUT"; return 1; }

  if ! echo "$RUN_OUT" | grep -qi 'crush' \
     || ! echo "$RUN_OUT" | grep -qi 'skipped' \
     || ! echo "$RUN_OUT" | grep -qi 'not installed'; then
    # Same relaxation as test 3 (see its comment) — substance over punctuation.
    echo "installer did not honestly report crush as skipped / not installed when only a shell function (not a real file) answered to its name. Output:"
    echo "$RUN_OUT"
    return 1
  fi
  if [[ -f "$CRUSH_GLOBAL_CONFIG/crushrc" ]]; then
    echo "installer wrote a crushrc for a shell-function-shadowed 'crush' that is not a real installed agent: $CRUSH_GLOBAL_CONFIG/crushrc"
    return 1
  fi
  return 0
}

# =============================================================================
# TEST 5 — unreachable endpoint degrades, never aborts
#
# REVISED (finding N7, §11.4.6 / §11.4.201 / §11.4.123): the previous revision
# of this test probed HELIX_CODER_BASE_URL — an env var install_agent_configs.sh
# never reads. Confirmed by reading the installer's actual source, not by
# guessing: `provider_spec()` HARDCODES all three Helix base URLs
# (helixllm-coder -> 127.0.0.1:18434, helixllm-gateway -> 127.0.0.1:8443,
# helixagent -> 127.0.0.1:7061) and `resolve_models()` curls exactly that
# hardcoded URL — a full grep of the file for every uppercase-shaped ${VAR}/
# $VAR read turns up NO env var and no --flag that changes which URL a
# provider is probed at (--sandbox only redirects where CONFIG FILES are
# written, never which endpoint is curled; REPO_ROOT, which the TLS gateway's
# CA-bundle path is derived from, comes from the script's own on-disk
# location, also with no override). HELIX_CODER_BASE_URL was never a real
# override — probing it could only ever SKIP, on every host, forever, which
# is not coverage (finding N7).
#
# There genuinely is no non-invasive way to FORCE a provider unreachable from
# this suite: this suite's own SAFETY constraints forbid taking any of the
# three real live Helix services down (constraint #2), and install_agent_configs.sh's
# internals are HARD CONSTRAINT out of scope for this suite to read or edit
# (see header). So instead of forcing anything, this test OBSERVES real
# reachability exactly the way the installer itself decides it — a GET to
# <base_url>/models, through the same fetch_live_models() helper test 8 uses,
# with the documented CA bundle for the one TLS surface — and only asserts the
# degrade contract against whichever providers are ACTUALLY unreachable right
# now. This is real, current host state, never a fabricated override; it is
# the same reachable/unreachable duality test 8 already treats as a legitimate
# per-provider SKIP condition, applied here to the whole run instead of one
# model id.
#
# MUTATION THAT MAKES THIS FAIL, on a host where a provider genuinely is down:
# make an unreachable provider endpoint abort the whole run (non-zero exit),
# and/or leave the still-reachable providers unconfigured. Measured live on
# THIS host: all three real Helix providers (helixllm-coder, helixllm-gateway,
# helixagent) answered /v1/models — see the report's curl transcript — so the
# degrade branch is not exercisable here today; SAFETY constraint #2 (never
# take a real live surface down) forbids manufacturing that branch to prove
# the mutation live, so this run reports the honest SKIP below rather than a
# result it did not earn (§11.4.6 no-guessing). SKIP-OK: §11.4.3.
# =============================================================================
test_unreachable_endpoint_degrades() {
  require_installer || return $?
  sb="$(new_sandbox)"; trap 'rm -rf "$sb"' EXIT   # NOT local: EXIT trap fires after this function returns, when a local sb would already be unset (nounset would abort the trap before rm -rf ran)
  sandbox_env_setup "$sb"

  # Observe reachability the same way the installer itself decides it
  # (fetch_live_models() -> plain GET, CA bundle on the TLS surface). Reuses
  # the exact URL table test 8 already uses — no new endpoint knowledge, no
  # override attempted.
  local pid unreachable="" reachable=""
  for pid in "${!PROVIDER_URL[@]}"; do
    if fetch_live_models "${PROVIDER_URL[$pid]}" >/dev/null 2>&1; then
      reachable="$reachable $pid"
    else
      unreachable="$unreachable $pid"
    fi
  done
  unreachable="${unreachable# }"; reachable="${reachable# }"

  if [[ -z "$unreachable" ]]; then
    echo "SKIP: all of ${!PROVIDER_URL[*]} are reachable right now; install_agent_configs.sh hardcodes each provider's base URL with no env-var or CLI override (confirmed by reading provider_spec()/resolve_models() — see the report), and this suite's SAFETY constraints forbid taking a real live Helix service down to observe the degrade branch, so nothing about this run can certify 'unreachable endpoint degrades' — SKIP-OK: §11.4.3"
    return "$SKIP_RC"
  fi

  run_installer
  if [[ $RUN_RC -ne 0 ]]; then
    echo "installer must degrade non-fatally when a provider ($unreachable) is unreachable, exited $RUN_RC: $RUN_OUT"
    return 1
  fi

  local all_cfg="" name f
  for name in opencode.json models.json crushrc; do
    f="$(locate_cfg "$sb" "$name")"
    [[ -n "$f" && -f "$f" ]] && all_cfg+="$(cat -- "$f")"$'\n'
  done

  local p
  for p in $unreachable; do
    if ! echo "$RUN_OUT" | grep -qi -- "$p" || ! echo "$RUN_OUT" | grep -qiE 'unreachable|skip'; then
      echo "installer gave no unreachable/skip/degrade signal for '$p', which is genuinely unreachable right now. Output:"
      echo "$RUN_OUT"
      return 1
    fi
  done
  for p in $reachable; do
    if ! echo "$all_cfg" | grep -qi -- "$p"; then
      echo "provider '$p' is reachable right now but was not configured into any written config, despite only $unreachable being unreachable"
      return 1
    fi
  done
  return 0
}

# =============================================================================
# TEST 6 — secret hygiene (§11.4.10 / CONST-042)
# MUTATION THAT MAKES THIS FAIL: (a) write a literal key into a config file
# — caught by the same detector run against the installer's real output;
# (b) weaken secret_scanner_flags() itself to a no-op — caught by the
# self-test half of THIS test, which runs first and fails on its own
# synthetic fixture before the installer is ever invoked.
# =============================================================================
test_secret_hygiene() {
  require_installer || return $?

  local synth; synth="$(mktemp)"
  printf 'export SOME_KEY="sk-abcdefghijklmnopqrstuvwx1234567890"\n' > "$synth"
  if ! secret_scanner_flags "$synth"; then
    rm -f "$synth"
    echo "self-test FAILED: the secret scanner did not flag its own synthetic literal-key fixture — it is a blind/bluff detector, not trustworthy against real installer output"
    return 1
  fi
  rm -f "$synth"

  sb="$(new_sandbox)"; trap 'rm -rf "$sb"' EXIT   # NOT local: EXIT trap fires after this function returns, when a local sb would already be unset (nounset would abort the trap before rm -rf ran)
  sandbox_env_setup "$sb"
  run_installer
  [[ $RUN_RC -eq 0 ]] || { echo "installer exited $RUN_RC: $RUN_OUT"; return 1; }

  local rc=0 name f mode
  for name in opencode.json models.json crushrc; do
    f="$(locate_cfg "$sb" "$name")"
    [[ -n "$f" && -f "$f" ]] || continue
    if secret_scanner_flags "$f"; then
      echo "possible secret-shaped content found in $f"
      rc=1
    fi
    mode="$(stat -c '%a' -- "$f" 2>/dev/null)"
    if [[ "$mode" != "600" ]]; then
      echo "wrong permissions on $f: got $mode, want 600"
      rc=1
    fi
  done

  # ---- 6b: --dry-run STDOUT, not just written files -------------------------
  # The loop above scans FILES the installer wrote. It cannot see the other,
  # larger exposure: --dry-run prints a unified diff of the operator's REAL
  # config to stdout, setup.sh tells operators to run exactly that, and
  # §11.4.83 asks for such transcripts to be committed. A real config carries
  # env-var-shaped credentials (measured on this host: an opencode.json with
  # /mcp/*/environment/*_API_KEY entries), so the diff is a path to a
  # committed secret unless the installer redacts them on the way out.
  #
  # The seed below is written COMPACT on purpose: the installer re-serialises
  # with indent=2, so the whole file differs and the diff necessarily reaches
  # the credential-bearing region. That the diff DID reach it is then asserted
  # as a PRECONDITION — without it this test could "pass" simply because the
  # secrets never appeared in the output at all, which would be a bluff.
  local sb2 dry
  sb2="$(new_sandbox)"
  sandbox_env_setup "$sb2"
  python3 - "$OPENCODE_CONFIG" <<'PY'
import json, os, sys
os.makedirs(os.path.dirname(sys.argv[1]), exist_ok=True)
cfg = {
    "$schema": "https://opencode.ai/config.json",
    # DIFF-REACHED PROBE (§11.4.201). Carries no credential word and no
    # env-var-shaped key, so nothing in redact() can touch it; if the diff
    # reached this file at all, this marker is in the output. It is what makes
    # "the seeded keys are absent" DECIDABLE: absent WITH the marker present
    # means the redactor ate the KEY NAMES (over-redaction, a real defect);
    # absent WITHOUT it means no diff was printed for this file at all (an
    # honest skip). Before it existed, both collapsed into the same SKIP and
    # an over-redacting installer scored `SKIP secret_hygiene`, exit 2.
    "helix_qa_diff_probe": "diff-reached-marker-9f31",
    "mcp": {
        "notion": {"type": "local", "command": ["notion-mcp"],
                   "environment": {"NOTION_API_KEY": "sk-notionAAAAAAAAAAAAAAAAAAAA"}},
        "tavily": {"type": "local", "command": ["tavily-mcp"],
                   "environment": {"TAVILY_API_KEY": "sk-tavilyBBBBBBBBBBBBBBBBBBBB"}},
        "aws":    {"type": "local", "command": ["aws-mcp"],
                   "environment": {"AWS_ACCESS_KEY_ID": "AKIAIOSFODNN7EXAMPLE"}},
    },
    "headers": {"Authorization": "Bearer liveTokenCCCCCCCCCCCCCCCCCCCCCC"},
    # Shapes measured LEAKING through the pre-2026-09-06 redact(), which was a
    # blocklist of credential prefixes and therefore permanently one vendor
    # behind reality. They are seeded here so this test FAILS if the redactor
    # ever reverts to that stance. Verified: against the pre-fix function 11 of
    # these render verbatim; against the fixed one, none do.
    "db": {"type": "local", "command": ["db-mcp"],
           "environment": {
               "DATABASE_URL": "postgresql://app:Sup3rS3cretPw@db.internal/prod",
               "MONGODB_URI": "mongodb://root:M0ng0P4ss@10.0.0.5:27017",
               "REDIS_URL": "redis://:R3disP4ss@cache.internal:6379",
           }},
    # CARVE-OUT CLASS. Every shape below survived the 2026-09-06 (round 3)
    # redactor verbatim: it masked env-var-shaped values but EXEMPTED any
    # value containing ":" or starting "/" as "probably a URL or a path".
    # Each of these lands squarely in that exemption and carries no vendor
    # prefix and no credential word in its key, so nothing else could catch
    # them. They are seeded here so this test FAILS if the redactor ever
    # reverts to a negative carve-out instead of a positive allowlist.
    "hooks": {"type": "local", "command": ["hooks-mcp"],
              "environment": {
                  "SLACK_WEBHOOK": "https://hooks.slack.com/services/T00000000/B0000000000/XXXXsecretsecret",
                  "HOOK_PATH": "/v1/hooks/abc123secretpathtoken",
                  "GIT_REMOTE": "https://a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2@github.com/org/repo.git",
                  "SVC_ACCOUNT": "svc:R3g1stryPw",
                  "MSSQL_CONN": "Server=tcp:x.database.windows.net,1433;Uid=app;Pwd=Sup3rDbPw;",
                  "PW": "hunter2pw",
              }},
    "misc": {"type": "local", "command": ["misc-mcp"],
             "environment": {
                 "SUPABASE_SERVICE_ROLE_KEY": "eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxIn0.abcdefSUPA",
                 "AWS_SESSION_ID": "ASIAY34FZKBOKMSXQEXAMPLE",
                 "GOOGLE_MAPS_KEY": "AIzaSyD-1234567890abcdefghijklmnopqrstu",
                 "GITLAB_PAT": "glpat-ABCDEFGHIJKLMNOPQRST",
                 "STRIPE_KEY": "rk_live_51ABCDEFGHIJKLMNOP",
                 "WIDGET_CONFIG_VALUE": "ZZtopSecretValue123456",
             }},
    # Lower-case / unprefixed key names: "key", "session" and "passphrase" were
    # all absent from the old key-name word list.
    "legacy": {"key": "bareKeyValue1234567890",
               "session": "sess_abcdef1234567890",
               "passphrase": "correct-horse-battery",
               "private_key": "-----BEGIN PRIVATE KEY-----MIIEvQIBADANsecret",
               # An ESCAPED QUOTE inside the value: the old value class [^"]*
               # stopped at the backslash and printed the tail verbatim.
               "apiKey": 'abc"def-tailLEAK'},
}
# compact, single line -> the installer's indent=2 re-serialisation makes the
# whole file differ, so the printed diff covers the credential region.
open(sys.argv[1], "w", encoding="utf-8").write(json.dumps(cfg))
PY
  run_installer --dry-run
  dry="$(mktemp)"
  printf '%s\n' "$RUN_OUT" > "$dry"

  if [[ $RUN_RC -ne 0 ]]; then
    echo "--dry-run exited $RUN_RC: $RUN_OUT"
    rm -f "$dry"; rm -rf "$sb2"
    return 1
  fi

  if ! grep -q 'NOTION_API_KEY' "$dry" || ! grep -q 'DATABASE_URL' "$dry" \
     || ! grep -q 'SLACK_WEBHOOK' "$dry"; then
    # OVER-REDACTION, not absence: the diff demonstrably reached this file
    # (the probe marker is in the output) yet the seeded KEY NAMES are gone.
    # An installer that masks env-var-shaped key NAMES as well as their values
    # produces exactly this, and it used to score SKIP -- suite exit 2, no
    # FAIL -- which is a §11.4.201 defect reported as an honest gap.
    # DID A DIFF FOR THE SEEDED FILE GET EMITTED AT ALL?
    #
    # The probe marker alone could not answer that, and that was a hole: it
    # only DISCRIMINATES WHEN IT SURVIVES. A BLANKET over-redactor masks the
    # marker and the seeded key names TOGETHER, so the marker test failed and
    # control fell through to the honest-skip branch below even though a diff
    # HAD been printed. Measured against a blanket-over-redactor installer
    # (redact() replaced by `sed s/[A-Za-z0-9_-]\{4,\}/XXXX/g`):
    # `SKIP  secret_hygiene`, suite `run=9 pass=6 fail=0 skip=3`, exit 0,
    # `TEST-AGENT-CONFIG-FANOUT: PASS` -- a vacuous skip on an installer that
    # renders its own diff unreadable.
    #
    # So the precondition is now asserted with two signals REDACTION CANNOT
    # ERASE, both SCOPED to the seeded file:
    #   (1) `WOULD update <path>` -- warn() prints it OUTSIDE the `| redact`
    #       pipe (installer: `warn "...WOULD update..."` then
    #       `diff -u ... | redact`), so no redactor, however blanket, can
    #       remove it; reaching it means cmp -s already said the file differs
    #       and the diff was piped;
    #   (2) `<path> (proposed)` -- the `+++` label of the unified diff.
    # A bare `^@@ ` hunk header was considered and REJECTED: it is structural
    # but NOT scoped to this file, so a diff printed for a DIFFERENT agent
    # config would satisfy it and turn a legitimate "no diff for the seeded
    # file" into a false FAIL (§11.4.201 in the other direction).
    local diff_emitted=0
    if grep -qF -- "WOULD update $OPENCODE_CONFIG" "$dry" \
       || grep -qF -- "$OPENCODE_CONFIG (proposed)" "$dry"; then
      diff_emitted=1
    fi
    if [[ $diff_emitted -eq 1 ]]; then
      if grep -qF -- 'diff-reached-marker-9f31' "$dry"; then
        echo "--dry-run printed a diff of the seeded config (the non-credential probe marker diff-reached-marker-9f31 is present) but NONE of the seeded credential KEY NAMES (NOTION_API_KEY / DATABASE_URL / SLACK_WEBHOOK) survived it. The redactor is masking key NAMES, not just values: the operator can no longer see WHICH settings are being changed, which is what the diff is for. This is over-redaction (§11.4.201), not an unreachable surface, so it is a FAIL and not a skip."
      else
        echo "--dry-run emitted a diff for the seeded config $OPENCODE_CONFIG (the installer printed its WOULD-update line and/or the +++ label, neither of which passes through redact()), yet NEITHER the seeded credential KEY NAMES (NOTION_API_KEY / DATABASE_URL / SLACK_WEBHOOK) NOR the non-credential probe marker diff-reached-marker-9f31 survived it. That is BLANKET over-redaction: the diff was printed and is unreadable, so the operator cannot see what is being changed. It is a FAIL, not a skip -- masking the marker along with everything else is exactly how this shape used to score a vacuous SKIP with the suite still green."
      fi
      rm -f "$dry"; rm -rf "$sb2"
      return 1
    fi
    # The diff never reached the seeded region (e.g. opencode is not installed
    # on this host, so no opencode diff was printed at all), so nothing below
    # was certified. That is an honest SKIP (§11.4.3), never a pass.
    #
    # This branch used to be a bare `echo`: it never touched rc, so the entire
    # `else` arm below -- EVERY leak assertion in this test -- was skipped
    # while the suite still printed `PASS secret_hygiene`. The suite runs with
    # `set -uo pipefail` (no -e) and prints stdout only on FAIL, so the note
    # was invisible on green. Measured against a no-op stub installer:
    # `PASS secret_hygiene` with `SUITE_EXIT=0`.
    #
    # A FAIL already recorded by the file-scan loop above is never downgraded
    # to a SKIP -- a skip of this half must not hide a failure of the other.
    if [[ $rc -eq 0 ]]; then
      rm -f "$dry"; rm -rf "$sb2"
      echo "SKIP: --dry-run stdout never reached the seeded credential region (NOTION_API_KEY, DATABASE_URL and/or SLACK_WEBHOOK absent from the output), so the stdout-redaction half of this test certified nothing this run"
      return "$SKIP_RC"
    fi
    echo "NOTE: --dry-run stdout never reached the seeded credential region, so the stdout-redaction half certified nothing this run; reporting the file-scan FAIL above rather than a skip"
  else
    local leaked
    for leaked in sk-notionAAAAAAAAAAAAAAAAAAAA sk-tavilyBBBBBBBBBBBBBBBBBBBB \
                  AKIAIOSFODNN7EXAMPLE liveTokenCCCCCCCCCCCCCCCCCCCCCC \
                  Sup3rS3cretPw M0ng0P4ss R3disP4ss \
                  eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxIn0.abcdefSUPA \
                  ASIAY34FZKBOKMSXQEXAMPLE \
                  AIzaSyD-1234567890abcdefghijklmnopqrstu \
                  glpat-ABCDEFGHIJKLMNOPQRST rk_live_51ABCDEFGHIJKLMNOP \
                  ZZtopSecretValue123456 bareKeyValue1234567890 \
                  XXXXsecretsecret abc123secretpathtoken \
                  a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2 \
                  R3g1stryPw Sup3rDbPw hunter2pw \
                  sess_abcdef1234567890 correct-horse-battery \
                  MIIEvQIBADANsecret def-tailLEAK; do
      if grep -qF -- "$leaked" "$dry"; then
        echo "--dry-run printed a credential VALUE verbatim to stdout: $leaked (CONST-042 / §11.4.10)"
        rc=1
      fi
    done
    # The reverse assertion: over-redaction has a floor. The installer writes a
    # literal, NON-credential api-key placeholder into every provider block it
    # manages; if the redactor masks that too, the diff no longer tells the
    # operator whether a placeholder or a real key is being written -- which is
    # the single most important thing the diff is for.
    #
    # This asserts on the DRY-RUN STDOUT, the only place the property is
    # observable. It used to be guarded on `grep helix-local-no-auth
    # "$OPENCODE_CONFIG"` -- but in THIS sandbox $OPENCODE_CONFIG holds only
    # the compact python seed above, which contains no placeholder, so the
    # guard was always false and the assertion was dead code. Measured: a
    # temporary installer that deliberately masks the placeholder (the exact
    # over-redaction this claims to catch) still produced PASS. Measured on
    # the real installer in this sandbox: the placeholder appears on 6 added
    # lines of --dry-run stdout, so requiring it is not vacuous.
    if ! grep -qF -- 'helix-local-no-auth' "$dry"; then
      echo "--dry-run stdout never showed the installer's own non-credential placeholder (helix-local-no-auth): either the redactor masked it -- so the diff can no longer show whether a placeholder or a real key is written -- or no provider block was emitted at all"
      rc=1
    fi
    if secret_scanner_flags "$dry"; then
      echo "the shared secret detector flagged --dry-run stdout:"
      grep -nE "$HIGH_CONFIDENCE_SECRET_RE|$GENERIC_KEY_FIELD_RE" "$dry" | head -5 | sed 's/^/        /'
      rc=1
    fi
  fi
  rm -f "$dry"; rm -rf "$sb2"
  return $rc
}

# =============================================================================
# TEST 6c — the redactor's key-name net, against key spellings that are NOT
#           [A-Za-z0-9_.-], on BOTH the JSON surface and the crush surface
# MUTATION THAT MAKES THIS FAIL: narrow redact()'s rule-(1)/(1b) key class back
# to [A-Za-z0-9_.-]*, or replace the leading `tr -d` control-byte strip with a
# single-byte `sed s/<SOH>//g`. Either one is measured to leak below.
#
# WHY THIS EXISTS. redact() exempts its own non-credential placeholder by
# inserting a <SOH> sentinel INTO the key, which works because a <SOH> key does
# not match rule (1)'s key class. That makes the key class itself the security
# boundary, and it was drawn as an ALPHABET ([A-Za-z0-9_.-]), so every key
# spelling outside that alphabet was a working forgery. Measured on the
# pre-fix installer, all of these printed their value verbatim:
#   "<0x02>apiKey"  "<0x1f>apiKey"  "<U+200B>apiKey"  "​apiKey"
#   "my apikey"     "x/api_key"     --api<0x02>-key <value>
# The middle four are VALID JSON — and `\uXXXX` is exactly how the installer's
# own json.dumps renders a non-ASCII key on the `+` side of a diff — so this
# was reachable from a real config, not only an adversarial one.
#
# TWO SURFACES, DELIBERATELY. An earlier report recorded that no in-suite
# fixture was possible on the crush path "because json.dumps escapes \001".
# That is wrong: crushrc is bash directives, not JSON, and a seed is written
# with open(...,'wb') — which is what this test does. The raw-control-byte
# vectors are asserted THERE, where they are reachable; the JSON surface
# carries only spellings that are valid JSON (python's json.loads rejects raw
# control bytes inside strings, so seeding them there would test the parser,
# not the redactor, and would be a fixture that proves nothing).
#
# Each surface has its own non-credential PROBE MARKER, so "no leak found" can
# never mean "no diff was printed". A surface whose marker is absent is an
# honest per-surface SKIP (§11.4.3); if BOTH are absent the whole test SKIPs
# rather than reporting a pass it did not earn.
# =============================================================================
test_redaction_forged_key_spellings() {
  require_installer || return $?
  sb="$(new_sandbox)"; trap 'rm -rf "$sb"' EXIT   # NOT local: the EXIT trap fires after this function returns
  sandbox_env_setup "$sb"

  python3 - "$OPENCODE_CONFIG" "$CRUSH_GLOBAL_CONFIG/crushrc" <<'PY'
import os, sys
oc, cr = sys.argv[1], sys.argv[2]
os.makedirs(os.path.dirname(oc), exist_ok=True)
os.makedirs(os.path.dirname(cr), exist_ok=True)
# JSON surface: VALID JSON only. Compact, so the installer's indent=2
# re-serialisation makes the whole file differ and the diff reaches these keys.
open(oc, "wb").write(
    b'{"$schema":"https://opencode.ai/config.json",'
    b'"helix_qa_json_probe":"json-diff-marker-6c",'
    b'"\xe2\x80\x8bapiKey":"ZWSPKEY_MUSTNOTLEAK",'          # raw U+200B before the key
    b'"\\u200bGITHUB_PAT":"UESCKEY_MUSTNOTLEAK",'           # the same key, JSON-escaped
    b'"my apikey":"SPACEKEY_MUSTNOTLEAK",'                  # a SPACE inside the key
    b'"x/api_key":"SLASHKEY_MUSTNOTLEAK",'                   # a SLASH inside the key
    # ROUND-8. The env-var-key rule inside _redact_env_values was anchored to
    # the OPENING QUOTE ("[A-Z][A-Z0-9_]+"), so ONE byte in front of the key
    # disarmed it and the value went out verbatim. All three spellings below
    # are VALID JSON and were measured leaking on this host.
    b'" MAILGUN_SENDING":"SPACEENVKEY_MUSTNOTLEAK",'         # leading SPACE
    b'"\xef\xbb\xbfMAILGUN_SENDING":"BOMENVKEY_MUSTNOTLEAK",'  # leading UTF-8 BOM
    b'"0MAILGUN_SENDING":"DIGITENVKEY_MUSTNOTLEAK",'         # leading DIGIT
    # ROUND-8. The scheme:// allowlist exempted the WHOLE URL, so everything
    # after the authority was declared readable -- a signed-webhook query
    # parameter is exactly where a credential lives.
    b'"CB_URL":"https://cb.example.invalid/v1?sig=QsigLEAK9X",'
    # ROUND-8. An invisible byte SPLIT the >=16 opaque run long_run() looks
    # for, so the URL branch then declared the whole value readable. U+009D,
    # written as the raw UTF-8 pair C2 9D, is valid JSON and is NOT in the
    # control-byte set the pipeline normalises.
    b'"SPLIT_URL":"https://x.invalid/AAAAAAAA\xc2\x9dSPLITLEAKX"}')
# crush surface: plain bash directives, so raw control bytes ARE reachable
# here. <SOH> is the sentinel redact() uses for its own exemption; <0x02> is
# any other control byte, which the pre-fix single-byte strip left alone.
# ORDER MATTERS. The installer APPENDS its managed block, so the unified diff
# it prints carries only the LAST few lines of the original file as context --
# measured: `@@ -2,3 +2,13 @@`, i.e. line 1 is NOT shown. A probe marker on
# line 1 is therefore invisible in the output, `reached` stays 0 for this
# surface, and the crush assertions below are silently skipped while the test
# still reports PASS. That is the vacuous-fixture failure this whole round is
# about, so the marker and both credential lines sit in the LAST three lines,
# inside the hunk. (Measured before the reorder: crush marker count 0 with the
# crush diff demonstrably present in the same output.)
open(cr, "wb").write(
    b'export CRUSH_THEME="solarized"\n'
    b'crush_provider_add "acme" "https://example.invalid/v1" '
    b'--api\x02-key CRUSHFLAG_MUSTNOTLEAK\n'
    # ROUND-8. The control byte AFTER the flag, not inside it. The pipeline
    # used to DELETE control bytes, which FUSED the flag onto its value and so
    # destroyed the separator the flag rule requires -- the byte-inside-flag
    # form above was masked while this one leaked.
    b'crush_provider_add "beta" "https://example.invalid/v1" '
    b'--api-key\x02CRUSHFUSED_MUSTNOTLEAK\n'
    b'export \x01CRUSH_API_KEY="CRUSHSOH_MUSTNOTLEAK"\n'
    b'# crush probe marker: crush-diff-marker-6c\n')
PY

  run_installer --dry-run
  [[ $RUN_RC -eq 0 ]] || { echo "--dry-run exited $RUN_RC: $RUN_OUT"; return 1; }

  local dry; dry="$(mktemp)"
  printf '%s\n' "$RUN_OUT" > "$dry"

  local rc=0 reached=0 leaked
  if grep -qF -- 'json-diff-marker-6c' "$dry"; then
    reached=$((reached + 1))
    for leaked in ZWSPKEY_MUSTNOTLEAK UESCKEY_MUSTNOTLEAK \
                  SPACEKEY_MUSTNOTLEAK SLASHKEY_MUSTNOTLEAK \
                  SPACEENVKEY_MUSTNOTLEAK BOMENVKEY_MUSTNOTLEAK \
                  DIGITENVKEY_MUSTNOTLEAK QsigLEAK9X \
                  SPLITLEAKX; do
      if grep -qF -- "$leaked" "$dry"; then
        echo "--dry-run printed a credential VALUE verbatim under a key whose spelling is outside [A-Za-z0-9_.-]: $leaked (CONST-042 / §11.4.10). The key-name rule's class is the boundary the placeholder exemption relies on, so a key it cannot match is a forged exemption."
        rc=1
      fi
    done
  fi
  if grep -qF -- 'crush-diff-marker-6c' "$dry"; then
    reached=$((reached + 1))
    for leaked in CRUSHFLAG_MUSTNOTLEAK CRUSHSOH_MUSTNOTLEAK CRUSHFUSED_MUSTNOTLEAK; do
      if grep -qF -- "$leaked" "$dry"; then
        echo "--dry-run printed a credential VALUE verbatim from the crush surface: $leaked (CONST-042 / §11.4.10). A raw control byte in the flag (--api<0x02>-key) or a forged <SOH> sentinel in the key disarmed the redaction rule that would otherwise have masked it."
        rc=1
      fi
    done
  fi

  rm -f "$dry"
  if [[ $reached -eq 0 ]]; then
    [[ $rc -eq 0 ]] || return 1
    echo "SKIP: --dry-run printed no diff for either seeded surface (neither json-diff-marker-6c nor crush-diff-marker-6c appeared), so no forged-key-spelling assertion was exercised this run — most likely neither opencode nor crush is installed on this host"
    return "$SKIP_RC"
  fi
  return $rc
}

# =============================================================================
# TEST 7 — dry-run writes nothing
# MUTATION THAT MAKES THIS FAIL: make --dry-run actually write (even one
# byte, one touched mtime, or one new file) anywhere under the sandbox.
# =============================================================================
test_dry_run_writes_nothing() {
  require_installer || return $?
  sb="$(new_sandbox)"; trap 'rm -rf "$sb"' EXIT   # NOT local: EXIT trap fires after this function returns, when a local sb would already be unset (nounset would abort the trap before rm -rf ran)
  sandbox_env_setup "$sb"
  seed_realistic_opencode "$OPENCODE_CONFIG"
  seed_realistic_pi "$PI_MODELS_JSON"
  seed_realistic_crush "$CRUSH_GLOBAL_CONFIG/crushrc"

  local before after
  before="$(snapshot_tree "$sb")"
  run_installer --dry-run
  [[ $RUN_RC -eq 0 ]] || { echo "--dry-run must exit non-fatally, exited $RUN_RC: $RUN_OUT"; return 1; }
  after="$(snapshot_tree "$sb")"

  if [[ "$before" != "$after" ]]; then
    echo "--dry-run changed the sandbox tree (path|mtime|sha256 diff):"
    diff <(printf '%s\n' "$before") <(printf '%s\n' "$after") || true
    return 1
  fi
  return 0
}

# =============================================================================
# TEST 8 — live model-id agreement (§11.4.111)
# MUTATION THAT MAKES THIS FAIL: have the installer write a model id for a
# reachable provider that is not actually present in that provider's live
# /v1/models listing (a stale/guessed id — exactly what §11.4.111 forbids).
# Honest SKIP (never a fake PASS) when a surface is unreachable.
# =============================================================================
test_live_model_id_agreement() {
  require_installer || return $?
  sb="$(new_sandbox)"; trap 'rm -rf "$sb"' EXIT   # NOT local: EXIT trap fires after this function returns, when a local sb would already be unset (nounset would abort the trap before rm -rf ran)
  sandbox_env_setup "$sb"
  run_installer
  [[ $RUN_RC -eq 0 ]] || { echo "installer exited $RUN_RC: $RUN_OUT"; return 1; }

  local all_cfg="" name f
  for name in opencode.json models.json crushrc; do
    f="$(locate_cfg "$sb" "$name")"
    [[ -n "$f" && -f "$f" ]] && all_cfg+="$(cat -- "$f")"$'\n'
  done
  if [[ -z "$all_cfg" ]]; then
    echo "no config file was produced by the installer to inspect; SKIP"
    return "$SKIP_RC"
  fi

  local rc=0 any_checked=0 provider url body mid
  for provider in "${!PROVIDER_URL[@]}"; do
    url="${PROVIDER_URL[$provider]}"
    body="$(fetch_live_models "$url")"
    if [[ -z "$body" ]]; then
      echo "SKIP: $provider's endpoint ($url) is not reachable right now; honestly skipping its model-id check"
      continue
    fi
    for mid in ${PROVIDER_MODELS[$provider]}; do
      if grep -qF -- "$mid" <<<"$all_cfg"; then
        any_checked=1
        if ! grep -qF -- "$mid" <<<"$body"; then
          echo "model id '$mid' ($provider) is written into a client config but is NOT present in ${url}/models right now"
          rc=1
        fi
      fi
    done
  done

  if [[ "$any_checked" -eq 0 ]]; then
    echo "no known Helix model id appeared in any written config against a reachable provider to compare; SKIP"
    return "$SKIP_RC"
  fi
  return $rc
}

# =============================================================================
# runner
# =============================================================================
declare -a TEST_NAMES=(
  "idempotency:test_idempotency"
  "non_clobber_merge:test_non_clobber_merge"
  "absent_agent_honesty:test_absent_agent_honesty"
  "shell_builtin_trap:test_shell_builtin_trap"
  "unreachable_endpoint_degrades:test_unreachable_endpoint_degrades"
  "secret_hygiene:test_secret_hygiene"
  "redaction_forged_key_spellings:test_redaction_forged_key_spellings"
  "dry_run_writes_nothing:test_dry_run_writes_nothing"
  "live_model_id_agreement:test_live_model_id_agreement"
)

TESTS_RUN=0; TESTS_PASS=0; TESTS_FAIL=0; TESTS_SKIP=0
declare -a FAILED_NAMES=()

run_one() {
  local name="$1" fn="$2" out rc
  TESTS_RUN=$((TESTS_RUN + 1))
  out="$("$fn" 2>&1)"
  rc=$?
  if [[ $rc -eq 0 ]]; then
    TESTS_PASS=$((TESTS_PASS + 1))
    printf 'PASS  %s\n' "$name"
  elif [[ $rc -eq "$SKIP_RC" ]]; then
    TESTS_SKIP=$((TESTS_SKIP + 1))
    printf 'SKIP  %s — %s\n' "$name" "$(printf '%s' "$out" | tail -n1)"
  else
    TESTS_FAIL=$((TESTS_FAIL + 1))
    FAILED_NAMES+=("$name")
    printf 'FAIL  %s\n' "$name"
    printf '%s\n' "$out" | sed 's/^/      /'
  fi
}

for entry in "${TEST_NAMES[@]}"; do
  run_one "${entry%%:*}" "${entry#*:}"
done

echo "-----"
printf '%s: run=%d pass=%d fail=%d skip=%d\n' "$SUITE" "$TESTS_RUN" "$TESTS_PASS" "$TESTS_FAIL" "$TESTS_SKIP"

if [[ $TESTS_FAIL -gt 0 ]]; then
  echo "$SUITE: FAIL — ${FAILED_NAMES[*]}"
  exit 1
fi
if [[ $TESTS_PASS -eq 0 && $TESTS_SKIP -gt 0 ]]; then
  echo "$SUITE: SKIP — every test SKIPped; nothing was certifiable this run (expected until $INSTALLER exists)"
  exit 2
fi
echo "$SUITE: PASS"
exit 0
