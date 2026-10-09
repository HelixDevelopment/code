#!/usr/bin/env bash
#
# wait-http-ready.sh — block until an HTTP(S) endpoint actually SERVES.
#
# WHY THIS EXISTS
# ---------------
# Every Helix service unit is Type=simple, which means systemd reports the unit
# "active (running)" the instant the process is forked — before it has bound its
# port, and regardless of whether it ever will. That makes "active" a claim about
# the PROCESS, not about the SERVICE.
#
# That gap is not hypothetical here. Recorded in helixagent.service (2026-07-27):
# the unit reported "active (running)" while hung inside `podman compose`, never
# binding :7061 — active-but-not-listening. A dependent unit ordered
# After=helixagent.service was therefore ordered after a lie.
#
# Running this as ExecStartPost makes the unit's "started" signal mean "the
# endpoint answered", so:
#   * `systemctl --user start` fails LOUDLY on a service that came up dead,
#     instead of succeeding and leaving a broken platform that looks green;
#   * After= ordering between Helix units becomes truthful (§11.4.108: source
#     present != artifact deployed != actually serving).
#
# It is the HTTP sibling of wait-infra-ready.sh, which does the same job for the
# container stack.
#
# SAFETY
# ------
# * Read-only. A single HTTP GET. Starts nothing, stops nothing, mutates nothing.
# * Bounded — an unbounded wait would convert a slow boot into a hang. The
#   deadline is always well inside the caller's TimeoutStartSec.
# * -k on HTTPS is deliberate: the gateway serves a self-signed certificate on
#   :8443. This probe asks "are you serving?", not "is your chain trusted".
# * Touches only the URL it is given, never another project's port (§11.4.174).
#
# EXIT CONTRACT
#   0  the endpoint returned an HTTP status (any status — see below)
#   1  the deadline passed with no HTTP response at all, or curl is missing
#
# Any HTTP status counts as ready. A 401/404/503 proves the process is bound and
# serving, which is exactly what this probe exists to establish; judging the
# BODY is the caller's job, not the ordering primitive's.
#
# Usage: wait-http-ready.sh <url> [timeout_seconds] [poll_interval_seconds]
#
set -uo pipefail

readonly URL="${1:?usage: wait-http-ready.sh <url> [timeout] [poll]}"
readonly TIMEOUT="${2:-90}"
readonly POLL="${3:-2}"

log() { printf '[wait-http-ready] %s\n' "$*"; }

if ! command -v curl >/dev/null 2>&1; then
    log "FATAL: curl not found on PATH"
    exit 1
fi

log "waiting up to ${TIMEOUT}s for ${URL}"
deadline=$(( $(date +%s) + TIMEOUT ))

while :; do
    # --max-time keeps a single hung connect from eating the whole budget.
    code="$(curl -sk -o /dev/null -w '%{http_code}' --max-time 5 "${URL}" 2>/dev/null || true)"

    # curl writes 000 when it never got an HTTP response (refused, timeout, DNS).
    if [ -n "${code}" ] && [ "${code}" != "000" ]; then
        log "READY: ${URL} answered HTTP ${code}"
        exit 0
    fi

    now=$(date +%s)
    if [ "${now}" -ge "${deadline}" ]; then
        log "FAILED after ${TIMEOUT}s — ${URL} never answered (last curl code: ${code:-none})"
        exit 1
    fi

    log "not serving yet — $(( deadline - now ))s left"
    sleep "${POLL}"
done
