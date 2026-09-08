#!/usr/bin/env sh
# scripts/export_gateway_keys.sh
#
# Exports the INBOUND gateway credentials that local CLI clients (Claude
# Toolkit provider aliases, claude-code-router, the validation harness) need in
# order to authenticate AGAINST our own gateways.
#
# THIS FILE IS SOURCED, NOT EXECUTED.
#
#   . /path/to/helix_code/scripts/export_gateway_keys.sh
#
# It is what `~/api_keys.sh` (the toolkit's $CMA_KEYS_FILE) sources so that the
# toolkit reads the key from the SAME file the gateway itself reads, at the
# moment it needs it.
#
#
# WHY DERIVE INSTEAD OF COPY
# ==========================
#
# The gateway's systemd unit gets its configuration from ONE place:
#
#     EnvironmentFile=-<repo>/.env        (helixllm-gateway.service)
#
# so `<repo>/.env` is, by construction, the single source of truth for what the
# RUNNING gateway will accept. Any second file holding a literal copy of that
# credential is a cache that goes stale the instant the first one is rotated,
# and the failure mode is silent: the gateway keeps serving, the client keeps
# sending, and every request 401s with a credential that "looks right" in both
# files. That is exactly the class of drift this project has already been bitten
# by (a generated config and a sync step each undoing the other), so the key is
# READ from the authoritative file on every source, never mirrored into a
# second one.
#
# The consequence worth stating plainly: rotating the credential means editing
# `.env` and restarting the unit. Nothing else has to be touched, because
# nothing else holds a copy.
#
#
# WHAT IS EXPORTED, AND FROM WHICH VARIABLE
# =========================================
#
#   HELIXLLM_GATEWAY_KEY   <- first entry of HELIX_AUTH_API_KEYS
#
# HELIX_AUTH_API_KEYS is HelixLLM's INBOUND credential list — the keys the
# gateway accepts on `Authorization: Bearer`. It is a COMMA-SEPARATED list
# (internal/gateway/middleware/auth.go splits on "," and compares each entry
# with crypto/subtle) specifically so a rotation can run both the old and the
# new key for a window. A client can only present one credential, so the FIRST
# entry is the current one by convention; put a new key first and keep the old
# one second while callers roll over.
#
# NOT the JWT signing secret. HELIX_AUTH_JWT_SECRET stays server-side and is
# deliberately never read here: it mints a valid token for ANY subject, so
# handing it to a client would give every toolkit-launched process the ability
# to forge credentials for the whole gateway. A scoped, revocable API key is
# the right credential to put in a client's hands; the signing secret is not.
#
#
# SHELL CONTRACT
# ==============
#
# POSIX sh only, and deliberately so. The toolkit's alias bodies are sourced
# into the operator's INTERACTIVE shell, which is bash on this host and zsh on
# macOS, and `scripts/lib.sh` already documents having been bitten by a
# bash-only construct (`${!var}`) being a fatal error under zsh. Nothing here
# uses arrays, `local`, `[[ ]]`, or indirect expansion.
#
# It is also sourced by `~/api_keys.sh` under the caller's `set -a` (auto-export)
# and with `set -u` disabled — so it must not depend on either being set, and it
# must not leave the caller's shell options changed. It does neither: every
# expansion carries a default, and the only state it leaves behind is the one
# exported variable.
#
# Silence is intentional. This runs on every provider-alias launch, so a
# missing .env or an unconfigured key prints NOTHING here — the toolkit's own
# launch path already reports an empty key with the variable name and the file
# to set it in, and duplicating that on every launch would train the operator
# to ignore it. The value itself is of course never printed.

# Repo root: overridable for a non-standard checkout, otherwise derived from
# this script's own location (scripts/ -> repo root). $0 is unreliable when a
# file is sourced, so the override is the documented path for anything exotic.
if [ -z "${HELIX_CODE_ROOT:-}" ]; then
    HELIX_CODE_ROOT=/home/milosvasic/Projects/helix_code
fi

# Read exactly the one assignment we need. The whole .env is NEVER sourced: it
# carries dozens of unrelated settings (ports, hosts, provider keys) and
# dumping all of them into an interactive shell on every alias launch would
# silently override whatever the operator had set for their own session.
_helix_gw_read_env_var() {
    # $1 = variable name, $2 = env file. Prints the value, or nothing.
    [ -f "$2" ] || return 0
    # Last assignment wins, matching how a shell would evaluate the file.
    sed -n "s/^[[:space:]]*$1=//p" "$2" 2>/dev/null | tail -n 1
}

_helix_gw_strip_quotes() {
    # Accepts a value on stdin; removes one matched pair of surrounding quotes.
    sed -e 's/^"\(.*\)"$/\1/' -e "s/^'\(.*\)'\$/\1/"
}

_helix_gw_env_file="${HELIX_CODE_ROOT}/.env"

# HELIXLLM_GATEWAY_KEY — first entry of the accepted-key list.
_helix_gw_keys="$(_helix_gw_read_env_var HELIX_AUTH_API_KEYS "$_helix_gw_env_file" | _helix_gw_strip_quotes)"
_helix_gw_first_key="${_helix_gw_keys%%,*}"
if [ -n "$_helix_gw_first_key" ]; then
    HELIXLLM_GATEWAY_KEY="$_helix_gw_first_key"
    export HELIXLLM_GATEWAY_KEY
fi

# CMA_PROVIDER_CA_CERT — trust anchor for the gateway's TLS certificate.
#
# Not a credential, but it belongs here for the same reason the key does: it is
# derived from this checkout and it must be present in exactly the same two
# places. The gateway serves HTTPS with a self-signed certificate, so a client
# that does not trust it cannot complete a handshake at all — and the failure is
# indistinguishable from the port being dead unless you read the TLS error. The
# toolkit's probe reports it precisely ("reachable, but the connection was
# refused at the TLS layer ... Point CMA_PROVIDER_CA_CERT at the CA/cert PEM"),
# and the toolkit honours this variable in all three places that talk to the
# endpoint: the verification probe, the generated router config, and the launch
# path.
#
# Setting it here rather than expecting an operator export is what makes the
# alias work on a fresh shell: ~/.bashrc sources the keys file, so this reaches
# both an interactive `claude-providers sync` and the alias launch that follows.
#
# Guarded on existence: an unreadable path is worse than an unset one — the
# toolkit warns and probes WITHOUT it, so a stale path buys a confusing warning
# instead of a clean "no CA configured". Never overrides an operator's own
# value.
if [ -z "${CMA_PROVIDER_CA_CERT:-}" ]; then
    _helix_gw_cert="${HELIX_CODE_ROOT}/submodules/helix_llm/certs/cert.pem"
    if [ -r "$_helix_gw_cert" ]; then
        CMA_PROVIDER_CA_CERT="$_helix_gw_cert"
        export CMA_PROVIDER_CA_CERT
    fi
    unset _helix_gw_cert
fi

# Leave no scratch state in the caller's shell. Under the caller's `set -a`
# every assignment above was auto-exported, so unset (not just blank) is the
# only thing that actually removes them from the environment of every process
# the alias goes on to launch — including the value itself.
unset _helix_gw_keys _helix_gw_first_key _helix_gw_env_file
unset -f _helix_gw_read_env_var _helix_gw_strip_quotes 2>/dev/null || true
