#!/usr/bin/env bash
#
# exec-with-api-keys.sh — source the operator's provider-key file, then exec the
# given command with those keys exported.
#
#   Usage: exec-with-api-keys.sh <command> [args...]
#
# WHY THIS EXISTS
# ---------------
# Services that talk to LLM providers discover them by scanning env-var names
# (HelixAgent scans ~137 candidates). With none set it discovers 0 providers and
# every completion returns 503 no_provider_available — a service that is up,
# healthy, and useless.
#
# The keys live in $HOME/api_keys.sh (override: CMA_KEYS_FILE), and that file
# CANNOT be consumed by systemd's `EnvironmentFile=`. Measured 2026-09-03:
#
#   * 88 of its lines are `export KEY=value`; 0 are bare `KEY=value`. systemd's
#     EnvironmentFile parser does not execute shell, so it reads the variable
#     name as "export KEY" — not a valid name — and DROPS the line.
#   * 41 lines interpolate another variable (`KEY=$ApiKey_Foo`). systemd
#     performs no shell expansion there, so those values would arrive as the
#     literal string "$ApiKey_Foo" even if the names had parsed.
#
# Pointing `EnvironmentFile=` at it therefore fails SILENTLY: the unit starts
# clean and the service still sees zero keys. A shell must source it.
#
# WHY A SCRIPT RATHER THAN AN INLINE `bash -c` IN THE UNIT
# --------------------------------------------------------
# The inline form needs systemd's `$$` escaping for every literal dollar sign
# ("$${CMA_KEYS_FILE:-$$HOME/...}"), which is easy to get wrong, invisible in
# `systemctl show` output (it prints the pre-expansion form, so a broken escape
# looks identical to a correct one), and untestable outside systemd. As a
# tracked script it is readable, runnable by hand, and testable in isolation.
#
# DESIGN NOTES
# ------------
# * `set -a` exports every assignment the sourced file makes; `set +a` restores.
# * `exec` REPLACES this shell with the target process, so systemd supervises
#   the real service directly — no stray wrapper PID, MAINPID stays correct,
#   and signals/Restart= behave exactly as without the wrapper.
# * A missing keys file is NOT fatal: the service degrades to zero providers,
#   which its own startup log states plainly. Failing here instead would take a
#   whole service down over an optional file.
# * `bash` (not `bash -l`): a login shell would pull in interactive profile
#   side effects that have no business in a service's environment.
# * NO VALUE FROM THE KEYS FILE IS EVER PRINTED. The diagnostics below report
#   only a COUNT and the file path — never a name-to-value pair, never a value.
#   The file itself is only read; it is never written, copied or modified.
#
set -uo pipefail

readonly KEYS_FILE="${CMA_KEYS_FILE:-${HOME}/api_keys.sh}"

log() { printf '[exec-with-api-keys] %s\n' "$*"; }

if [ "$#" -eq 0 ]; then
    log "FATAL: no command given"
    log "usage: exec-with-api-keys.sh <command> [args...]"
    exit 64   # EX_USAGE
fi

if [ -r "${KEYS_FILE}" ]; then
    before="$(env | wc -l)"
    set -a
    # shellcheck disable=SC1090  # path is operator config, not resolvable statically
    . "${KEYS_FILE}"
    set +a
    after="$(env | wc -l)"
    # Count only. Deliberately never the names' values (§12.1 / CONST-042).
    log "sourced ${KEYS_FILE} (+$(( after - before )) exported vars)"
else
    log "WARNING: ${KEYS_FILE} not readable — continuing WITHOUT provider keys."
    log "         Provider discovery will find 0 providers and completions will"
    log "         return 503 no_provider_available until it is present."
fi

log "exec: $1"
exec "$@"
