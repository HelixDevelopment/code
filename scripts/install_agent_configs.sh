#!/usr/bin/env bash
#
# install_agent_configs.sh — wire every installed CLI coding agent to the
# Helix model surfaces (HelixLLM coder, HelixLLM gateway, HelixAgent).
#
# This is the fan-out step of ./setup.sh: the platform's OpenAI-compatible
# endpoints exist after `setup.sh` installs the systemd units, but nothing
# tells the operator's CLI agents that they exist. This script does that,
# for every agent that is actually installed, and reports an explicit
# skipped-with-reason line (§11.4.3) for every agent that is not.
#
# Agents handled
#   opencode  ~/.config/opencode/opencode.json   (JSON, deep-merged by opencode)
#   pi        ~/.pi/agent/models.json            (JSON, separate from settings.json)
#   crush     ~/.config/crush/crushrc            (Bash-with-builtins directives)
#
#   claude    NOT handled here. Claude Code speaks the Anthropic Messages API
#             only — it cannot consume an OpenAI-compatible provider — so it is
#             wired through the Anthropic-shaped facade, not by this script.
#
# Guarantees
#   * MERGE, never overwrite. Only the Helix provider subtrees are touched;
#     every other key (MCP servers, plugins, themes, agent definitions, an
#     operator-set apiKey) is preserved.
#   * Idempotent. A second run makes NO write at all — when the merged result
#     is semantically identical to what is on disk the file is left untouched,
#     byte for byte.
#   * Non-destructive. A timestamped backup is taken before any modifying
#     write, and a config that does not parse is skipped, never rewritten.
#   * Live model resolution (§11.4.111): model ids come from each endpoint's
#     /v1/models at write time. An unreachable endpoint degrades to the
#     documented fallback ids with a warning; it never aborts the run.
#
# Usage
#   scripts/install_agent_configs.sh                 # install / update
#   scripts/install_agent_configs.sh --dry-run       # show diffs, write nothing
#   scripts/install_agent_configs.sh --verify        # run each agent's own check
#   scripts/install_agent_configs.sh --only crush    # one agent (repeatable)
#   scripts/install_agent_configs.sh --sandbox DIR   # write under DIR, not $HOME
#   scripts/install_agent_configs.sh --summary-file F  # machine-readable summary
#
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

# ---------------------------------------------------------------------------
# Helix surface spec.
#
# Fields: base_url | label | ca_path (empty = plain http) | default_ctx |
#         default_out | fallback model ids (space separated)
# The fallback ids are used ONLY when the endpoint cannot be reached; the
# live /v1/models response is authoritative when it answers.
# ---------------------------------------------------------------------------
HELIX_PROVIDERS="helixllm-coder helixllm-gateway helixagent"

provider_spec() {
  case "$1" in
    helixllm-coder)
      printf '%s\n' \
        'http://127.0.0.1:18434/v1|HelixLLM Coder (local)||32768|4096|qwen2.5-coder-3b-instruct-q4_k_m'
      ;;
    helixllm-gateway)
      printf '%s\n' \
        "https://127.0.0.1:8443/v1|HelixLLM Gateway (TLS)|${REPO_ROOT}/submodules/helix_llm/certs/cert.pem|32768|4096|helixllm-anton-qwen2-5-coder-3b-instruct-q4_k_m-f6771589d190"
      ;;
    helixagent)
      # default_ctx is 131072 here, NOT the 32768 the two llama.cpp surfaces
      # share, because HelixAgent is a different kind of surface and its
      # capacity was MEASURED rather than assumed (2026-09-07):
      #
      #   * /v1/models on :7061 carries no `meta` block, so resolve_models()
      #     cannot read a live n_ctx for it and falls back to THIS value. The
      #     number therefore has to be right on its own.
      #   * A 900,312-byte request was accepted and answered with
      #     usage.prompt_tokens = 200046 -- and the answer correctly recalled a
      #     marker planted at the VERY FRONT of that prompt, so the surface
      #     neither refused it nor silently truncated it. 131072 is a
      #     deliberately conservative claim well inside that proven figure.
      #   * Leaving it at 32768 under-declared the surface by ~4x, which makes
      #     an agent compact or refuse work the endpoint would have served.
      #
      # This matters because 32768 does not fit ANY mainstream CLI agent's
      # baseline prompt on this host -- measured, same day, same tokenizer:
      # pi 86,411 / crush 96,564 / opencode 134,043 tokens before a single user
      # turn is added (~950 host-installed skills inlined into the system
      # prompt). HelixAgent is the only local surface with room for them.
      printf '%s\n' \
        'http://127.0.0.1:7061/v1|HelixAgent||131072|4096|helixagent-llm helixagent-debate helixagent-ensemble helix-llm helix-debate'
      ;;
    *) return 1 ;;
  esac
}

spec_field() { provider_spec "$1" | cut -d'|' -f"$2"; }

# ---------------------------------------------------------------------------
# Output helpers
# ---------------------------------------------------------------------------
if [ -t 1 ]; then C_B=$'\033[1m'; C_G=$'\033[32m'; C_Y=$'\033[33m'; C_R=$'\033[31m'; C_0=$'\033[0m'
else C_B=''; C_G=''; C_Y=''; C_R=''; C_0=''; fi

section() { printf '\n%s==> %s%s\n' "$C_B" "$*" "$C_0"; }
ok()      { printf '  %s✓%s %s\n' "$C_G" "$C_0" "$*"; }
warn()    { printf '  %s!%s %s\n' "$C_Y" "$C_0" "$*"; }
fail()    { printf '  %s✗%s %s\n' "$C_R" "$C_0" "$*"; }
info()    { printf '    %s\n' "$*"; }

# redact masks credential VALUES in anything printed to the terminal or a log.
# --dry-run prints diffs of real configs, and those configs can legitimately
# contain an operator-set key; a secret must never reach stdout or a setup log
# (CONST-042 / §11.4.10). Key NAMES stay visible so the diff is still readable.
#
# STANCE (round 4, and the reason the previous three rounds kept leaking):
# every earlier revision decided what to MASK. R1 listed credential prefixes;
# R2 listed more of them; R3 inverted to a structural env-var-key rule but
# bolted a NEGATIVE carve-out onto it -- "a value containing ':' or starting
# '/' is probably readable". A negative exemption inherits the blocklist's
# defect: it is still a guess about what is not a secret, and 13 real shapes
# landed squarely inside it (Slack/Discord webhook URLs, "user:pass",
# "api:key-...", "/v1/hooks/<token>", "https://<token>@host", a URL password
# containing '/', the JSON-escaped ':\/\/' form, "v1:<hmac>", ";Pwd=...").
# Round 4 therefore inverts the carve-out itself: rule (2) masks EVERY
# env-var-shaped value and exempts ONLY a CLOSED, POSITIVE allowlist of shapes
# that are readable BY CONSTRUCTION. A credential format nobody has seen yet
# fails SAFE -- masked because it is not on an allowlist, rather than leaked
# because it is not on a blocklist.
#
# The key-NAME rule (1) is kept as defence in depth and stays a CONTAINS
# match: measured against real, live agent configs on this host, an exact-name
# rule covering apiKey/api_key/token/secret/password DID cover
# /provider/*/options/apiKey but did NOT cover the env-var-shaped keys real
# configs are full of (NOTION_API_KEY, TAVILY_API_KEY,
# GITHUB_PERSONAL_ACCESS_TOKEN) nor an "Authorization": "Bearer ..." header.
# Over-redaction is harmless here; under-redaction is a CONST-042 leak.

# The one literal the installer itself writes as a NON-credential: the
# api-key placeholder every managed provider block gets. Masking it hides the
# single fact the operator most needs from the diff -- that a placeholder, and
# not a real key, is being written. It is exempted BY EXACT VALUE and it is
# the only exemption of its kind.
REDACT_PLACEHOLDER='helix-local-no-auth'

# LOCALE (§11.4.50). Every stage of redact() whose bracket ranges decide
# masking runs under LC_ALL=C, because those ranges are locale-dependent:
# measured on this host, a UTF-8 collation makes [A-Za-z] match `e-acute`, so
# the same input could be redacted under one locale and not another. The drift
# is safe in DIRECTION (a wider class over-masks, and over-masking is harmless
# here -- see above) but a redactor whose output depends on the caller's
# environment is not reproducible, which is a separate defect. _redact_env_values
# below is NOT pinned by this function: it is invoked as a shell FUNCTION, and a
# `VAR=x func` prefix leaks the assignment past the call in bash, so it pins its
# own stages internally instead.
# --- rule (2), the primary net: STRUCTURAL, allowlist-exempted --------------
# Inside the JSON being diffed, an ENV-VAR-SHAPED key ("[A-Z][A-Z0-9_]+", two
# characters or more -- the floor is 2, not 3, so a "PW" key is covered) is
# sensitive REGARDLESS of its name: MCP servers put every credential in
# "environment" blocks with exactly that key shape. Its value is MASKED unless
# readable() says it matches the closed allowlist:
#   * scheme://... with NO '@' anywhere (userinfo is always a credential
#     position) and NO opaque run of >=16 [A-Za-z0-9_-] characters (that is
#     what a webhook path token looks like) -- so "http://127.0.0.1:7061/v1"
#     stays readable while ".../services/T0/B0/XXXXsecretsecret" does not;
#   * host:port -- a hostname or IP, a colon, and 1-5 digits, nothing else;
#   * a clock, HH:MM or HH:MM:SS;
#   * an absolute path, or a colon-separated list of them (PATH), again with
#     no >=16-character opaque segment;
#   * the empty string, and the placeholder above.
# Everything else containing a colon -- "admin:hunter2", "api:key-...",
# "v1:<hmac>", ";Pwd=..." -- is MASKED.
#
# This is awk, not sed, because "mask unless the value matches an allowlist"
# is not expressible as an ERE substitution, and expressing it as a negative
# character class is exactly the round-3 defect. If awk is somehow absent the
# fallback masks EVERY env-var-shaped value: the failure direction is
# over-redaction, never a leak.
_redact_env_values() {
  if command -v awk >/dev/null 2>&1; then
    LC_ALL=C awk -v PH="$REDACT_PLACEHOLDER" '
      function long_run(v,   i, c, run) {
        run = 0
        for (i = 1; i <= length(v); i++) {
          c = substr(v, i, 1)
          # ROUND-8 CONST-042 #5d. A byte outside printable ASCII CONTINUES
          # the run instead of resetting it. Measured: a single C1 byte
          # inserted mid-token split an opaque 16-run into two 8-runs, so
          # long_run said no, the URL branch below then declared the whole
          # value readable, and it went out verbatim -- while the intact
          # 16-run was masked. Invisible bytes are not token boundaries;
          # treating them as such is what made the split work. (LC_ALL=C,
          # so this compares BYTES.)
          if (c ~ /[A-Za-z0-9_-]/ || c < " " || c > "~") { run++; if (run >= 16) return 1 } else { run = 0 }
        }
        return 0
      }
      # ROUND-8 CONST-042 #5. Sensitivity is decided from the key CONTENT,
      # not from a class anchored to the opening quote. The old test was
      # a quote followed by [A-Z][A-Z0-9_]+, so ONE byte in front of the key
      # disarmed it and the value went out verbatim -- measured on this host
      # with a leading SPACE (valid JSON), a UTF-8 BOM, and a leading digit.
      # Round 4 had already widened rule (1) for exactly this forgery class;
      # this rule never got the same treatment.
      # ENV-VAR-SHAPED := after deleting every non-alphanumeric byte, the key
      # carries NO lowercase ASCII letter. That keeps ordinary
      # camelCase/lowercase JSON keys (model, apiKey, baseUrl) OUT of this
      # rule -- they belong to rule (1) downstream, and sending their values
      # through readable() would mask a model id and make the diff
      # unreadable -- while ALL-CAPS keys stay in, however they are spelled.
      # It also closes the fullwidth and Kelvin-sign key spellings for free:
      # those bytes are not ASCII alphanumerics, so nothing lowercase
      # survives the strip and the key is treated as env-var-shaped.
      # (LC_ALL=C, so the ranges are bytes -- 11.4.50.)
      function envshaped(k,   t) {
        sub(/^"/, "", k)
        sub(/"[ \t]*:[ \t]*"$/, "", k)
        t = k
        gsub(/[^A-Za-z0-9]/, "", t)
        if (t ~ /[a-z]/) return 0
        return 1
      }
      function readable(v,   parts, k, j) {
        if (v == "") return 1
        if (v == PH) return 1
        if (index(v, "@") > 0) return 0
        if (long_run(v)) return 0
        # ROUND-8 CONST-042 #4, second half. long_run() above rejects any
        # value carrying a 16-byte opaque run, but a SHORT credential slips
        # under it and is then exempted by the path/URL branches below:
        # /opt/x/sk-abc123/y went through verbatim (the downstream sk- rule
        # needs 8+ trailing bytes; this has 6). No allowlist branch may
        # exempt a value carrying a KNOWN CREDENTIAL PREFIX, whatever its
        # length. The prefixes are boundary-anchored so ordinary words keep
        # their readability: task_ / disk- / work-dir do NOT match, because
        # the byte before sk/rk is alphanumeric there.
        if (v ~ /(^|[^A-Za-z0-9])[sr]k[_-][A-Za-z0-9]/) return 0
        if (v ~ /(^|[^A-Za-z0-9])(gh[pousr]_|xox[baprs]-|glpat-|hf_|npm_|AKIA|ASIA|AIza|eyJ)/) return 0
        if (v ~ /^[0-9][0-9]?:[0-9][0-9](:[0-9][0-9])?$/) return 1
        if (v ~ /^[A-Za-z0-9_.-]+:[0-9][0-9]?[0-9]?[0-9]?[0-9]?$/) return 1
        # ROUND-8 CONST-042 #4. The tail class used to be [^backslash]*, so
        # once a value merely LOOKED like a URL the WHOLE of it was declared
        # readable -- query string included. Measured leaking verbatim:
        #   https://cb.example.com/v1?sig=abc123def456&x=1
        #   https://cb.example.com/v1?key=ShrtKey456
        # A signed-webhook query parameter is exactly where a credential
        # lives, and both are under every downstream length threshold, so
        # nothing later caught them. The exemption now STOPS AT THE
        # AUTHORITY: any ? or # disqualifies the value, which then falls
        # through to masking. A plain base URL (the shape this installer
        # actually emits, e.g. http://127.0.0.1:7061/v1) is unaffected and
        # stays readable -- asserted by a fixture in BOTH directions.
        if (v ~ /^[A-Za-z][A-Za-z0-9+.-]*:\/\/[^\\?#]*$/) return 1
        if (substr(v, 1, 1) == "/") {
          if (index(v, "\\") > 0) return 0
          k = split(v, parts, ":")
          for (j = 1; j <= k; j++) if (substr(parts[j], 1, 1) != "/") return 0
          return 1
        }
        return 0
      }
      {
        line = $0; out = ""; n = length(line); i = 1
        while (i <= n) {
          rest = substr(line, i)
          if (!match(rest, /"[^"]*"[ \t]*:[ \t]*"/)) { out = out rest; break }
          keytok = substr(rest, RSTART, RLENGTH)
          out = out substr(rest, 1, RSTART - 1 + RLENGTH)
          i = i + RSTART - 1 + RLENGTH
          val = ""; closed = 0
          while (i <= n) {
            c = substr(line, i, 1)
            if (c == "\\") { val = val c substr(line, i + 1, 1); i += 2; continue }
            if (c == "\"") { closed = 1; break }
            val = val c; i++
          }
          # A key that is NOT env-var-shaped passes through untouched here and
          # is left to rule (1) downstream; masking it in THIS rule would eat
          # model ids and paths the operator has to be able to read.
          if (!envshaped(keytok)) { out = out val; if (!closed) { i = n + 1 } }
          else if (closed)        { out = out (readable(val) ? val : "***REDACTED***") }
          else                    { out = out "***REDACTED***"; i = n + 1 }
        }
        print out
      }
    '
  else
    # The key class mirrors the awk rule above: no lowercase ASCII letter
    # and no quote, so a leading space / BOM / digit cannot disarm it here
    # either.
    LC_ALL=C sed -E 's/("[^"a-z]*"[[:space:]]*:[[:space:]]*")(([^"\\]|\\.)*)(")/\1***REDACTED***\4/g'
  fi
}

redact() {
  local sent keycls
  sent="$(printf '\001')"

  # KEY-NAME CLASS for rules (1) and (1b): every byte that is neither a JSON
  # string terminator nor the sentinel. It replaced [A-Za-z0-9_.-]* because
  # that narrow class was a LEAK, not a safety property: measured on this
  # host, six key spellings carried a credential straight to stdout --
  #   "<0x02>apiKey"  "<0x1f>apiKey"  "<U+200B>apiKey"  "\u200bapiKey"
  #   "my apikey"     "x/api_key"
  # -- because a control byte, a zero-width space, a JSON \uXXXX escape, a
  # SPACE and a SLASH are all outside [A-Za-z0-9_.-] and so broke the match.
  # The last two are VALID JSON, and \uXXXX is exactly how this installer's
  # own json.dumps renders a non-ASCII key on the `+` side of a diff, so this
  # was reachable from a real config, not only from an adversarial one.
  # The class is still QUOTE-BOUNDED, so it cannot run out of the key and
  # into the value. Over-redaction stays harmless here (see above); a missed
  # key is a CONST-042 leak.
  keycls="[^\"${sent}]*"

  # --- (0) PLACEHOLDER EXEMPTION ------------------------------------------
  # Rules (1) and (5) key off the KEY name and the CLI flag, so a value-side
  # exemption cannot survive them. The pair is disarmed by inserting a control
  # character into the KEY ("apiKey" -> "<SOH>apiKey") and into the FLAG
  # (--api-key -> --api<SOH>-key), which stops those rules matching; the final
  # sed strips every <SOH> again. A control character cannot occur in a JSON
  # config or a shell command line, so nothing else is affected.
  #
  # INPUT SANITISATION (leading strip). The sentinel disarms rules (1)/(5) BY
  # VALUE, so a <SOH> already present in the INPUT forges the exemption: a key
  # spelled "<SOH>apiKey" missed rule (1)'s key class and the trailing strip
  # then printed it as a clean "apiKey" with its value intact.
  #
  # WHAT THIS ACTUALLY GUARANTEES (§11.4.6). An earlier revision of this
  # comment claimed the exemption was "unforgeable by construction". THAT WAS
  # FALSE, and measuring it is what found the leaks: stripping only <SOH> left
  # every OTHER control byte as a working forgery (<0x02> and <0x1f> were both
  # measured leaking, on the JSON path AND on the crush command-line path as
  # `--api<0x02>-key`), and non-ASCII / JSON-escaped key spellings needed no
  # control byte at all. The honest statement of the guarantee is now:
  #   * NO C0 control byte and no DEL survives into the pipeline, so the only
  #     <SOH> downstream is one this function inserted -- that much IS by
  #     construction, and it is why `tr -d` replaced the single-byte `sed`;
  #   * a key that is not spelled with the exempt sentinel is matched by rule
  #     (1) whatever else it contains, because the key class is now "anything
  #     that is not a quote and not the sentinel" rather than an alphabet.
  # Neither clause claims the redactor is complete. It claims these two
  # properties, both of which have a fixture.
  #
  # Tab, LF and CR are deliberately KEPT: they are legal layout in the diff
  # being redacted, and removing them would corrupt the operator's output.
  # LC_ALL=C so the byte ranges mean bytes (§11.4.50).
  # ROUND-8 CONST-042 #3. This used to be `tr -d`, and DELETING the byte
  # was itself the leak: the flag rules below require a SEPARATOR between
  # the flag and its value, and deleting a C0 byte FUSES them, so the
  # separator the rule needs can no longer exist. Measured leaking
  # verbatim (0x01 behaves identically):
  #   in : provider add x --api-key<0x02>sk-live-AAAABBBBCCCCDDDD ...
  #   out: provider add x --api-keysk-live-AAAABBBBCCCCDDDD ...
  # while the plain --api-key<space> form WAS masked -- a regression
  # manufactured by the round-4/6 hardening: moving the byte INSIDE the
  # flag was masked, moving it AFTER the flag leaked. Translating to a
  # SPACE keeps the round-4 guarantee intact (no C0 byte and no DEL
  # survives into the pipeline, so the only sentinel downstream is one
  # this function inserted) while restoring the separator. [ *] pads SET2,
  # so every byte in SET1 maps to a space.
  LC_ALL=C tr '\000-\010\013\014\016-\037\177' '[ *]' \
  | LC_ALL=C sed -E \
    -e "s/\"([A-Za-z0-9_.-]*)\"([[:space:]]*:[[:space:]]*\"${REDACT_PLACEHOLDER}\")/\"${sent}\1\"\2/g" \
    -e "s/--(api)([_-]key)([[:space:]]+\"?${REDACT_PLACEHOLDER})/--\1${sent}\2\3/g" \
  | _redact_env_values \
  | LC_ALL=C sed -E \
    -e 's/("'"$keycls"'(api[_-]?key|token|secret|password|passwd|pwd|credential|creds?|auth|bearer|basic|login|passphrase|cookie|session|private|dsn|key)'"$keycls"'"[[:space:]]*:[[:space:]]*")(([^"\\]|\\.)*)(")/\1***REDACTED***\5/gI' \
    -e 's/("('"$keycls"'_)?pat(_'"$keycls"')?"[[:space:]]*:[[:space:]]*")(([^"\\]|\\.)*)(")/\1***REDACTED***\6/gI' \
    -e 's/(:\\?\/\\?\/[^"@:[:space:]]*:)([^"@[:space:]]+)(@)/\1***REDACTED***\3/g' \
    -e 's/(\bBearer[[:space:]]+)[A-Za-z0-9._~+\/=-]{8,}/\1***REDACTED***/gI' \
    -e 's/\beyJ[A-Za-z0-9_-]{4,}\.[A-Za-z0-9_-]{4,}\.[A-Za-z0-9_-]+/***REDACTED***/g' \
    -e 's/-----BEGIN[^"]*/***REDACTED***/g' \
    -e 's/\bsk-[A-Za-z0-9_-]{8,}/***REDACTED***/g' \
    -e 's/\b[sr]k_(live|test)_[A-Za-z0-9]{8,}/***REDACTED***/g' \
    -e 's/\bgh[pousr]_[A-Za-z0-9]{8,}/***REDACTED***/g' \
    -e 's/\bxox[baprs]-[A-Za-z0-9-]{8,}/***REDACTED***/g' \
    -e 's/\bAKIA[0-9A-Z]{12,}/***REDACTED***/g' \
    -e 's/\bASIA[0-9A-Z]{12,}/***REDACTED***/g' \
    -e 's/\bAIza[0-9A-Za-z_-]{30,}/***REDACTED***/g' \
    -e 's/\bglpat-[A-Za-z0-9_-]{8,}/***REDACTED***/g' \
    -e 's/\bhf_[A-Za-z0-9]{8,}/***REDACTED***/g' \
    -e 's/\bnpm_[A-Za-z0-9]{8,}/***REDACTED***/g' \
    -e 's/(--(api[_-]?key|token|password|passphrase)[^A-Za-z0-9_*-]*")[^"]*(")/\1***REDACTED***\3/gI' \
    -e "s/(--(api[_-]?key|token|password|passphrase)[^A-Za-z0-9_*-]*)[^\"[:space:]]+/\1***REDACTED***/gI" \
    -e 's/((^|[^A-Za-z0-9_])[A-Za-z0-9_]*(api[_-]?key|token|secret|password|passwd|pwd|credential|passphrase|dsn)[A-Za-z0-9_]*[[:space:]]*=[[:space:]]*)("?)[^"[:space:]]+/\1\4***REDACTED***/gI' \
  | LC_ALL=C sed -e "s/${sent}//g"
}
# Rules applied by the second sed above, in order:
#   NOTE ON EDITING THE PIPELINE ABOVE: the whole `tr | sed | ... | sed`
#   chain is ONE backslash-continued logical line, so a `#` comment cannot
#   be placed between its `-e` arguments -- the continuation joins the line
#   first, the comment eats the rest of it, and every following `-e` is
#   then parsed as a COMMAND (`-e: command not found`), silently dropping
#   those redaction rules. `bash -n` does NOT catch it. Round 8 did exactly
#   this while fixing the flag rules and caught it only because a fixture
#   changed verdict. Put prose here instead.
#   (5a) ROUND-8 CONST-042 #3, second half: the flag rules used
#        [[:space:]]+, which made WHITESPACE the only admissible separator
#        between a flag and its value, so any other byte defeated them. The
#        `tr` above now maps C0/DEL to a space, but NBSP, ZWSP and the C1
#        range are multi-byte UTF-8 and are deliberately NOT stripped there
#        (they are legal layout in a diff), so the separator class is
#        widened to [^A-Za-z0-9_-]* as well: anything that is not a value
#        byte counts as a separator, and ZERO separators are admitted too,
#        which additionally covers --api-key=SECRET. Over-redaction is the
#        safe direction and is what this file already chooses everywhere.
#        The class also excludes `*`, which is REQUIRED FOR IDEMPOTENCE, not
#        cosmetic: the `sk-` prefix rule earlier in this same sed block has
#        already turned the value into ***REDACTED***, and a separator class
#        containing `*` re-consumed the leading `***` of that mask and
#        masked the remainder again, emitting `--api-key ******REDACTED***`.
#        The secret stayed masked, but a redactor whose output is not a
#        fixed point of itself will drift; measured and pinned by a fixture.
#   (1)  KEY-NAME, CONTAINS match, deliberately broad. The value class is
#        ([^"\]|\.)* so an escaped quote INSIDE the value cannot terminate the
#        match early and leak the tail -- it did exactly that:
#          "apiKey": "abc\"def-tail"  ->  "apiKey": "***REDACTED***"def-tail"
#        pwd / login / basic / bearer / cred(s) were added in round 4: "PW",
#        "BASIC", "REGISTRY_LOGIN" and a lower-case "bearer" key all reached
#        stdout under the previous word list.
#   (1b) "pat" is BOUNDED, not a CONTAINS match. An unbounded "pat" would also
#        swallow PATH, *_PATH and "patch", which the operator must still read.
#   (3)  URL USERINFO under ANY key name: postgres://, mongodb://, redis://
#        (empty-user form), amqp://. Round 4: the separator also matches the
#        JSON-ESCAPED ":\/\/" form, and the password class now admits "/" --
#        "postgres://app:ab/cd@host" leaked its password because the old class
#        stopped at the slash.
#   (4)  VALUE-SHAPE rules: defence in depth ONLY, never the primary net. A
#        prefix catalogue is permanently one vendor behind reality: measured
#        against 20 realistic shapes, the catalogue-only revision leaked 11.
#        The fix is inverting the stance in rule (2); these remain only to
#        catch a credential sitting under a lower-case key name.
#   (5)  CLI flags and shell assignments (not JSON). "pwd" was added in round
#        4 so an ODBC ";Pwd=..." is masked the way ";Password=..." already was.
#        The flag spelling is `--api[_-]?key`, matching the SAME class rule
#        (1) uses for JSON keys. It read `--api-key|--api_key` through round 6,
#        so `--apikey <secret>` (and `--APIKEY <secret>`, the rules being /I)
#        printed VERBATIM while the identically-named JSON key was masked --
#        two rules that should agree, disagreeing. Measured on the extracted
#        redactor before the fix: `cmd --apikey SUPERSECRETVALUE1234 more`
#        came through unmasked; after it, `cmd --apikey ***REDACTED*** more`,
#        with `--apipath /usr/bin/x` still untouched (no over-redaction).
#        Not reachable from this installer's OWN output -- it emits
#        `--api-key "helix-local-no-auth"`, a placeholder -- so this is an
#        internal-consistency fix, not a live leak that was measured escaping.
#
# Accepted over-redaction (documented, not silent): an env-var-shaped key
# whose value is a bare number, word or comma list -- "PORT": "8080",
# "LOG_LEVEL": "debug", "TZ": "Europe/Belgrade" -- is masked by rule (2),
# because none of those shapes is on the allowlist and the allowlist is what
# makes the rule fail safe. The key NAME and the JSON structure stay visible,
# which is what keeps the diff reviewable.

# Summary lines: "<agent>\t<status>\t<detail>". Rendered at the end and, when
# --summary-file is given, written there for setup.sh's closing banner.
SUMMARY=()
record() { SUMMARY+=("$1"$'\t'"$2"$'\t'"$3"); }

# ---------------------------------------------------------------------------
# CLI
# ---------------------------------------------------------------------------
DRY_RUN=0
DO_VERIFY=0
SANDBOX=""
SUMMARY_FILE=""
ONLY=()

usage() { sed -n '2,41p' "$0" | sed 's/^# \{0,1\}//'; }

while [ $# -gt 0 ]; do
  case "$1" in
    --dry-run)      DRY_RUN=1 ;;
    --verify)       DO_VERIFY=1 ;;
    --only)         [ $# -ge 2 ] || { fail "--only needs an agent name"; exit 2; }; ONLY+=("$2"); shift ;;
    --sandbox)      [ $# -ge 2 ] || { fail "--sandbox needs a directory"; exit 2; }; SANDBOX="$2"; shift ;;
    --summary-file) [ $# -ge 2 ] || { fail "--summary-file needs a path"; exit 2; }; SUMMARY_FILE="$2"; shift ;;
    -h|--help)      usage; exit 0 ;;
    *) fail "unknown option: $1 (try --help)"; exit 2 ;;
  esac
  shift
done

selected() {
  [ ${#ONLY[@]} -eq 0 ] && return 0
  local a; for a in "${ONLY[@]}"; do [ "$a" = "$1" ] && return 0; done
  return 1
}

# Config roots. --sandbox redirects every agent into one throwaway tree so the
# script can be exercised without ever touching the operator's real configs.
if [ -n "$SANDBOX" ]; then
  OPENCODE_DIR="${SANDBOX}/opencode"
  PI_DIR="${SANDBOX}/pi/agent"
  CRUSH_DIR="${SANDBOX}/crush"
else
  OPENCODE_DIR="${XDG_CONFIG_HOME:-${HOME}/.config}/opencode"
  PI_DIR="${PI_CODING_AGENT_DIR:-${HOME}/.pi/agent}"
  CRUSH_DIR="${XDG_CONFIG_HOME:-${HOME}/.config}/crush"
fi
OPENCODE_CFG="${OPENCODE_DIR}/opencode.json"
PI_CFG="${PI_DIR}/models.json"
CRUSH_CFG="${CRUSH_DIR}/crushrc"

WORK="$(mktemp -d)"
# apply_file writes "<target>.helix-tmp.$$" NEXT TO the target (a rename must
# stay on the same filesystem), so it lives outside $WORK and the EXIT trap
# has to reap it separately or an interrupted run strands a 0600 file holding
# the fully-rendered config beside the operator's real one.
CURRENT_TMP=""
cleanup() {
  rm -rf "$WORK"
  [ -n "${CURRENT_TMP:-}" ] && rm -f "$CURRENT_TMP"
  :
}
trap cleanup EXIT

# ---------------------------------------------------------------------------
# Agent detection.
#
# TRAP: `command -v continue` (and `command -v test`, `time`, ...) succeeds on
# a SHELL BUILTIN. A builtin is not an installed agent. Require that the name
# resolves to an executable FILE on disk before believing it.
# ---------------------------------------------------------------------------
agent_path() {
  local name="$1" p
  [ "$(type -t "$name" 2>/dev/null || true)" = "file" ] || return 1
  p="$(command -v "$name" 2>/dev/null || true)"
  [ -n "$p" ] && [ -x "$p" ] && [ -f "$p" ] || return 1
  printf '%s\n' "$p"
}

agent_version() {
  local name="$1"
  timeout 30 "$name" --version 2>/dev/null | head -1 | tr -d '\r' || true
}

# ---------------------------------------------------------------------------
# Live model resolution (§11.4.111 — resolve by name, at write time).
#
# Emits TSV: <model-id>\t<context>\t<max-output>
# rc 0 = live answer, rc 1 = endpoint unreachable (caller falls back).
# ---------------------------------------------------------------------------
# STDOUT of this function is the TSV the caller captures, so every diagnostic
# it emits MUST go to stderr — a stray message on stdout would be parsed as a
# model row and corrupt the generated config.
MODEL_CACHE_STATUS=""   # "live" | "fallback" — set by resolve_models
resolve_models() {
  local pid="$1" url ca body
  url="$(spec_field "$pid" 1)"
  ca="$(spec_field "$pid" 3)"

  local -a curl_args=(-sS -m 8 --fail)
  if [ -n "$ca" ]; then
    if [ -r "$ca" ]; then
      curl_args+=(--cacert "$ca")
    else
      warn "${pid}: CA bundle not readable at ${ca} — TLS probe cannot succeed" >&2
      warn "${pid}: run scripts/init-submodules.sh to populate submodules/helix_llm/certs/" >&2
    fi
  fi

  if body="$(curl "${curl_args[@]}" "${url}/models" 2>/dev/null)"; then
    if printf '%s' "$body" | MODEL_DEFAULT_CTX="$(spec_field "$pid" 4)" \
        MODEL_DEFAULT_OUT="$(spec_field "$pid" 5)" python3 -c '
import json, os, sys
try:
    data = json.load(sys.stdin)
except Exception:
    sys.exit(1)
items = data.get("data") or []
if not items:
    sys.exit(1)
dctx = int(os.environ["MODEL_DEFAULT_CTX"]); dout = int(os.environ["MODEL_DEFAULT_OUT"])
for m in items:
    mid = m.get("id")
    if not mid:
        continue
    meta = m.get("meta") or {}
    ctx = meta.get("n_ctx") or meta.get("n_ctx_train") or dctx
    try:
        ctx = int(ctx)
    except (TypeError, ValueError):
        ctx = dctx
    print("%s\t%d\t%d" % (mid, ctx, min(dout, ctx)))
'; then
      MODEL_CACHE_STATUS="live"
      return 0
    fi
  fi

  MODEL_CACHE_STATUS="fallback"
  local dctx dout m
  # stdout below is the TSV; nothing else may be printed here.
  dctx="$(spec_field "$pid" 4)"; dout="$(spec_field "$pid" 5)"
  for m in $(spec_field "$pid" 6); do printf '%s\t%s\t%s\n' "$m" "$dctx" "$dout"; done
  return 1
}

# Resolve every provider once, into $WORK/models.<pid>. Providers whose
# endpoint is down are still configured (from fallback ids) so the agent is
# ready the moment the service comes up.
PROVIDER_STATUS=()
resolve_all() {
  local pid
  for pid in $HELIX_PROVIDERS; do
    if resolve_models "$pid" > "${WORK}/models.${pid}"; then
      ok "$(printf '%-18s %s  (%s live model(s))' "$pid" "$(spec_field "$pid" 1)" "$(wc -l < "${WORK}/models.${pid}" | tr -d ' ')")"
    else
      warn "$(printf '%-18s %s  UNREACHABLE — using documented fallback ids' "$pid" "$(spec_field "$pid" 1)")"
    fi
    PROVIDER_STATUS+=("${pid}=${MODEL_CACHE_STATUS}")
  done
}

# ---------------------------------------------------------------------------
# apply_file <label> <target> <staged>
#
# The single write path for every agent. <staged> is the fully-rendered
# desired content; an EMPTY staged marker file means "no change needed".
#   * identical bytes  -> no write at all (idempotency, hard requirement)
#   * --dry-run        -> print a unified diff, write nothing
#   * otherwise        -> backup, atomic install (temp + mv), chmod 0600
# ---------------------------------------------------------------------------
apply_file() {
  local label="$1" target="$2" staged="$3"

  # A symlinked target (stow / chezmoi / yadm-managed dotfiles) is FOLLOWED,
  # not replaced. Measured before this change: with
  # ~/.config/opencode/opencode.json -> ~/dotfiles/opencode.json, the `mv`
  # below replaced the LINK with a regular file, the dotfiles copy was left
  # untouched, and the operator's next `stow`/`chezmoi apply` conflicted.
  # Following is chosen over refusing because the installer's contract is
  # "merge into the operator's config", and for a dotfiles-managed operator
  # the real config IS the file at the far end of the link -- refusing would
  # leave them permanently SKIPPED with no supported path. The redirection is
  # announced so it is never silent.
  local followed_from=""
  if [ -L "$target" ]; then
    local real
    if ! real="$(readlink -f -- "$target")" || [ -z "$real" ]; then
      fail "${label}: ${target} is a symlink that cannot be resolved — nothing written"
      return 12
    fi
    if [ "$real" != "$target" ]; then
      info "${label}: ${target} is a symlink — following it to ${real}"
      followed_from="$target"
      target="$real"
    fi
  fi

  if [ -f "$target" ] && cmp -s "$target" "$staged"; then
    ok "${label}: already up to date — no write (${target})"
    return 10   # unchanged
  fi

  if [ "$DRY_RUN" -eq 1 ]; then
    warn "${label}: WOULD update ${target}"
    local left="/dev/null" left_label="(absent)"
    if [ -f "$target" ]; then left="$target"; left_label="${target} (current)"; fi
    diff -u --label "$left_label" --label "${target} (proposed)" "$left" "$staged" | redact || true
    return 11   # would change
  fi

  # EVERY step below is checked, and the temp file is verified byte-for-byte
  # against the staged content BEFORE the rename. apply_file is only ever
  # called inside a `&& rc=0 || rc=$?` list, which suspends `set -e` for this
  # entire function body, so an unchecked command here fails SILENTLY.
  #
  # Reproduced against an unwritable target directory: mkdir, cp -p, cat,
  # chmod and mv ALL failed, yet this function printed "ok ...: updated ..."
  # and returned 0 -> `record CONFIGURED` -> setup.sh printed "CLI agent
  # configuration complete" over a run that wrote nothing. That is a CONST-035
  # false success.
  #
  # Reproduced under a write limit (the disk-full shape): `cat` died partway,
  # leaving a TRUNCATED temp file, and `mv -f` — a rename, which needs no free
  # space — then installed that truncated file over the operator's real
  # config. The `cp -p` backup taken two lines earlier was truncated by the
  # same condition, so the only complete copy was already gone. Hence: verify
  # the backup too, and never rename anything that does not compare equal.
  if ! mkdir -p "$(dirname "$target")"; then
    fail "${label}: cannot create $(dirname "$target") — nothing written"
    return 12
  fi

  if [ -f "$target" ]; then
    local bak="${target}.helix-backup.$(date -u +%Y%m%dT%H%M%SZ)"
    if ! cp -p "$target" "$bak"; then
      rm -f "$bak"
      fail "${label}: backup of ${target} failed — refusing to write; the existing config is untouched"
      return 12
    fi
    if ! cmp -s "$target" "$bak"; then
      rm -f "$bak"
      fail "${label}: backup of ${target} is incomplete (out of space?) — refusing to write; the existing config is untouched"
      return 12
    fi
    if ! chmod 0600 "$bak"; then
      rm -f "$bak"
      fail "${label}: chmod 0600 on the backup failed — refusing to write; the existing config is untouched"
      return 12
    fi
    info "backup: ${bak}"
    # The backup is named after the RESOLVED target, so when the config is a
    # dotfiles symlink the backup lands in the DOTFILES directory -- where a
    # later `git add -A` would commit a file holding the operator's pre-merge
    # config. Following the link is still the right contract (see above), but
    # the side effect must not be silent.
    if [ -n "$followed_from" ]; then
      warn "${label}: the backup was written beside the symlink TARGET ($(dirname "$target")), not beside ${followed_from}. Remove it once you have checked the merge."
      if command -v git >/dev/null 2>&1 && \
         git -C "$(dirname "$target")" rev-parse --is-inside-work-tree >/dev/null 2>&1; then
        warn "${label}: that directory IS inside a git work tree — 'git add -A' there would commit ${bak##*/}, which contains your pre-merge config."
      fi
    fi
  fi

  local tmp="${target}.helix-tmp.$$"
  rm -f "$tmp"
  CURRENT_TMP="$tmp"
  # umask 077 in a subshell: the chmod 0600 below is too late on its own --
  # the file is created by the redirection at the ambient umask (0664 on this
  # host) and already holds the rendered apiKey during the window before the
  # chmod lands.
  if ! ( umask 077; cat "$staged" > "$tmp" ); then
    rm -f "$tmp"
    fail "${label}: writing ${tmp} failed — ${target} left untouched"
    return 12
  fi
  if ! cmp -s "$staged" "$tmp"; then
    rm -f "$tmp"
    fail "${label}: staged content did not land intact in ${tmp} (truncated write — out of space?) — ${target} left untouched"
    return 12
  fi
  if ! chmod 0600 "$tmp"; then
    rm -f "$tmp"
    fail "${label}: chmod 0600 ${tmp} failed — ${target} left untouched"
    return 12
  fi
  if ! mv -f "$tmp" "$target"; then
    rm -f "$tmp"
    fail "${label}: installing ${tmp} -> ${target} failed — ${target} left untouched"
    return 12
  fi
  CURRENT_TMP=""   # renamed away; nothing left for the EXIT trap to reap
  ok "${label}: updated ${target}"
  return 0        # changed
}

# ===========================================================================
# opencode
# ===========================================================================
plan_opencode() {
  local staged="${WORK}/opencode.json"

  if [ -f "$OPENCODE_CFG" ] && ! python3 -c 'import json,sys; json.load(open(sys.argv[1], encoding="utf-8"))' "$OPENCODE_CFG" 2>/dev/null; then
    fail "opencode: ${OPENCODE_CFG} is not strict JSON — refusing to touch it"
    record opencode SKIPPED "config is not strict JSON (hand-edit required)"
    return 0
  fi

  local pid
  : > "${WORK}/opencode.providers.jsonl"
  for pid in $HELIX_PROVIDERS; do
    HP_ID="$pid" HP_LABEL="$(spec_field "$pid" 2)" HP_URL="$(spec_field "$pid" 1)" \
      python3 -c '
import json, os, sys
models = {}
for line in sys.stdin:
    line = line.rstrip("\n")
    if not line:
        continue
    parts = line.split("\t")
    if len(parts) != 3:
        sys.stderr.write("skipping malformed model row: %r\n" % line)
        continue
    mid, ctx, out = parts
    models[mid] = {"name": mid, "limit": {"context": int(ctx), "output": int(out)}}
print(json.dumps({
    "id": os.environ["HP_ID"],
    "npm": "@ai-sdk/openai-compatible",
    "name": os.environ["HP_LABEL"],
    "baseURL": os.environ["HP_URL"],
    "models": models,
}))
' < "${WORK}/models.${pid}" >> "${WORK}/opencode.providers.jsonl"
  done

  # Merge. Existing provider entries gain only what they LACK: a missing
  # baseURL, missing models. Every value already present -- an operator-set
  # baseURL or apiKey, extra models, custom names -- is left exactly alone.
  # If the merge changes nothing, emit nothing -> apply_file sees no diff.
  OC_TARGET="$OPENCODE_CFG" python3 -c '
import copy, json, os, sys

path = os.environ["OC_TARGET"]
try:
    with open(path, encoding="utf-8") as fh:
        cfg = json.load(fh)
except OSError as _e:
    if not isinstance(_e, FileNotFoundError):
        sys.stderr.write("cannot read %s: %s\n" % (_e.filename or "<config>", _e.strerror or _e))
        sys.exit(5)
    cfg = {}
original = copy.deepcopy(cfg)

cfg.setdefault("$schema", "https://opencode.ai/config.json")
providers = cfg.setdefault("provider", {})

for line in sys.stdin:
    line = line.strip()
    if not line:
        continue
    spec = json.loads(line)
    entry = providers.setdefault(spec["id"], {})
    entry.setdefault("npm", spec["npm"])
    entry.setdefault("name", spec["name"])
    opts = entry.setdefault("options", {})
    # setdefault, NOT assignment: never rewrite a baseURL the operator already
    # chose. `localhost` and `127.0.0.1` both reach these surfaces (probed:
    # both return HTTP 200), so "canonicalising" an existing entry changes a
    # working value for no benefit and breaks the merge-not-clobber contract.
    # A genuinely wrong existing URL surfaces through --verify, which is the
    # honest place to discover it.
    opts.setdefault("baseURL", spec["baseURL"])
    models = entry.setdefault("models", {})
    for mid, mdef in spec["models"].items():
        models.setdefault(mid, mdef)           # never rewrite an existing model

if cfg == original:
    sys.exit(3)                                # semantically unchanged
json.dump(cfg, sys.stdout, indent=2, ensure_ascii=False)
sys.stdout.write("\n")
' < "${WORK}/opencode.providers.jsonl" > "$staged" && :
  local rc=$?

  if [ "$rc" -eq 3 ]; then
    ok "opencode: already up to date — no write (${OPENCODE_CFG})"
    record opencode UNCHANGED "$OPENCODE_CFG"
    return 0
  elif [ "$rc" -ne 0 ]; then
    fail "opencode: merge failed (rc=${rc}) — nothing written"
    record opencode ERROR "merge failed"
    return 0
  fi

  apply_file opencode "$OPENCODE_CFG" "$staged" && rc=0 || rc=$?
  case "$rc" in
    0)  record opencode CONFIGURED "$OPENCODE_CFG" ;;
    10) record opencode UNCHANGED  "$OPENCODE_CFG" ;;
    11) record opencode DRY-RUN    "would update $OPENCODE_CFG" ;;
    *)  record opencode ERROR      "write to $OPENCODE_CFG failed (see the ✗ line above)" ;;
  esac
  info "opencode reads config.json -> opencode.json -> opencode.jsonc -> config.toml and deep-merges them; later wins."
  info "first use of a new npm: provider downloads @ai-sdk/openai-compatible into ${OPENCODE_DIR}/node_modules (~63 MB, needs network)."
  info "restart opencode to pick this up."
}

# ===========================================================================
# pi
# ===========================================================================
plan_pi() {
  local staged="${WORK}/pi-models.json"

  if [ -f "$PI_CFG" ] && ! python3 -c 'import json,sys; json.load(open(sys.argv[1], encoding="utf-8"))' "$PI_CFG" 2>/dev/null; then
    fail "pi: ${PI_CFG} is not valid JSON — refusing to touch it"
    record pi SKIPPED "models.json is not valid JSON (hand-edit required)"
    return 0
  fi

  local pid
  : > "${WORK}/pi.providers.jsonl"
  for pid in $HELIX_PROVIDERS; do
    HP_ID="$pid" HP_URL="$(spec_field "$pid" 1)" python3 -c '
import json, os, sys
models = []
for line in sys.stdin:
    line = line.rstrip("\n")
    if not line:
        continue
    parts = line.split("\t")
    if len(parts) != 3:
        sys.stderr.write("skipping malformed model row: %r\n" % line)
        continue
    mid, ctx, out = parts
    models.append({"id": mid, "name": mid,
                   "contextWindow": int(ctx), "maxTokens": int(out)})
print(json.dumps({
    "id": os.environ["HP_ID"],
    "baseUrl": os.environ["HP_URL"],
    "api": "openai-completions",
    # pi hides models from providers it believes are unauthenticated, so a
    # NON-EMPTY placeholder is required even though these surfaces need no
    # key. It is a literal placeholder, never a credential (CONST-042).
    "apiKey": "helix-local-no-auth",
    "models": models,
}))
' < "${WORK}/models.${pid}" >> "${WORK}/pi.providers.jsonl"
  done

  PI_TARGET="$PI_CFG" python3 -c '
import copy, json, os, sys

path = os.environ["PI_TARGET"]
try:
    with open(path, encoding="utf-8") as fh:
        cfg = json.load(fh)
except OSError as _e:
    if not isinstance(_e, FileNotFoundError):
        sys.stderr.write("cannot read %s: %s\n" % (_e.filename or "<config>", _e.strerror or _e))
        sys.exit(5)
    cfg = {}
original = copy.deepcopy(cfg)
providers = cfg.setdefault("providers", {})

for line in sys.stdin:
    line = line.strip()
    if not line:
        continue
    spec = json.loads(line)
    entry = providers.setdefault(spec["id"], {})
    # setdefault, NOT assignment — matching the baseURL handling in the
    # opencode merge above, and the merge-not-clobber contract stated in the
    # header of this file (an operator-set value is preserved). Assigning here
    # silently replaced a baseUrl the operator had chosen; a genuinely wrong
    # one surfaces through --verify, the honest place to discover it.
    entry.setdefault("baseUrl", spec["baseUrl"])
    entry.setdefault("api", spec["api"])
    entry.setdefault("apiKey", spec["apiKey"])
    have = {m.get("id") for m in entry.get("models", []) if isinstance(m, dict)}
    merged = list(entry.get("models", []))
    for m in spec["models"]:
        if m["id"] not in have:
            merged.append(m)
    entry["models"] = merged

if cfg == original:
    sys.exit(3)
json.dump(cfg, sys.stdout, indent=2, ensure_ascii=False)
sys.stdout.write("\n")
' < "${WORK}/pi.providers.jsonl" > "$staged" && :
  local rc=$?

  if [ "$rc" -eq 3 ]; then
    ok "pi: already up to date — no write (${PI_CFG})"
    record pi UNCHANGED "$PI_CFG"
    return 0
  elif [ "$rc" -ne 0 ]; then
    fail "pi: merge failed (rc=${rc}) — nothing written"
    record pi ERROR "merge failed"
    return 0
  fi

  apply_file pi "$PI_CFG" "$staged" && rc=0 || rc=$?
  case "$rc" in
    0)  record pi CONFIGURED "$PI_CFG" ;;
    10) record pi UNCHANGED  "$PI_CFG" ;;
    11) record pi DRY-RUN    "would update $PI_CFG" ;;
    *)  record pi ERROR            "write to $PI_CFG failed (see the ✗ line above)" ;;
  esac
  info "pi reloads models.json on /model — no restart needed."
}

# ===========================================================================
# crush
#
# crushrc is Bash-with-builtins, not JSON, so the Helix directives live in a
# sentinel-delimited managed block that is REPLACED (never duplicated) on
# re-run. Everything outside the sentinels is preserved verbatim.
# Models are auto-discovered from /v1/models, so no `model add` is needed.
# ===========================================================================
CRUSH_BEGIN='# >>> helix-managed block — install_agent_configs.sh — do not edit >>>'
CRUSH_END='# <<< helix-managed block — install_agent_configs.sh <<<'

plan_crush() {
  local staged="${WORK}/crushrc" block="${WORK}/crushrc.block"
  local pid url ca rc=0

  {
    printf '%s\n' "$CRUSH_BEGIN"
    for pid in $HELIX_PROVIDERS; do
      url="$(spec_field "$pid" 1)"
      ca="$(spec_field "$pid" 3)"
      if [ -n "$ca" ]; then
        printf '# %s is TLS with a self-signed CA. crushrc cannot export env vars\n' "$pid"
        printf '# (proven: an `export` line here has no effect), so crush must be started with\n'
        printf '#   SSL_CERT_FILE=%s\n' "$ca"
        printf '# in its environment or model discovery for this provider silently returns none.\n'
      fi
      printf 'provider add %s --name "%s" --type openai-compat --base-url "%s" --api-key "helix-local-no-auth" --discover-models true\n' \
        "$pid" "$(spec_field "$pid" 2)" "$url"
    done
    printf '%s\n' "$CRUSH_END"
  } > "$block"

  CR_TARGET="$CRUSH_CFG" CR_BLOCK="$block" \
  CR_BEGIN="$CRUSH_BEGIN" CR_END="$CRUSH_END" python3 -c '
import os, sys

target = os.environ["CR_TARGET"]
begin, end = os.environ["CR_BEGIN"], os.environ["CR_END"]
with open(os.environ["CR_BLOCK"], encoding="utf-8") as fh:
    block = fh.read()

try:
    with open(target, encoding="utf-8") as fh:
        current = fh.read()
except OSError as _e:
    if not isinstance(_e, FileNotFoundError):
        sys.stderr.write("cannot read %s: %s\n" % (_e.filename or "<config>", _e.strerror or _e))
        sys.exit(5)
    current = ""

# A file left with TWO managed blocks by the historical append bug: index()
# returns the FIRST BEGIN and the FIRST END, so the splice rewrites pair #1
# and carries every later block forward verbatim -- forever, invisibly. Refuse
# for the same reason a lone sentinel is refused: the operator hand-edits.
nb, ne = current.count(begin), current.count(end)
if nb > 1 or ne > 1:
    sys.stderr.write("managed-block sentinels are duplicated (%d BEGIN / %d END); only the first pair would ever be updated\n" % (nb, ne))
    sys.exit(4)

has_begin, has_end = begin in current, end in current
if has_begin and has_end:
    b, e = current.index(begin), current.index(end)
    if e < b:
        # END before BEGIN: partition() would drop everything after BEGIN.
        sys.stderr.write("managed-block sentinels are out of order (END appears before BEGIN)\n")
        sys.exit(4)
    out = current[:b] + block
    tail = current[e + len(end):].lstrip("\n")
    if tail:
        out += "\n" + tail
elif has_begin or has_end:
    # One sentinel without its partner: the old code fell into the `else`
    # branch and APPENDED a second block, leaving the file with a duplicate
    # (and, for a lone BEGIN, an unterminated) managed region. Refusing is the
    # only non-corrupting option; the operator hand-edits and re-runs.
    sys.stderr.write("managed-block sentinels are inconsistent (%s marker present without its partner)\n"
                     % ("BEGIN" if has_begin else "END"))
    sys.exit(4)
else:
    out = (current.rstrip("\n") + "\n\n" if current.strip() else "") + block

sys.stdout.write(out)
' > "$staged" && rc=0 || rc=$?

  if [ "$rc" -eq 4 ]; then
    fail "crush: ${CRUSH_CFG} has an inconsistent helix-managed block — refusing to touch it"
    record crush SKIPPED "managed-block sentinels inconsistent (hand-edit required)"
    return 0
  elif [ "$rc" -ne 0 ]; then
    fail "crush: managed-block splice failed (rc=${rc}) — nothing written"
    record crush ERROR "managed-block splice failed"
    return 0
  fi

  apply_file crush "$CRUSH_CFG" "$staged" && rc=0 || rc=$?
  case "$rc" in
    0)  record crush CONFIGURED "$CRUSH_CFG" ;;
    10) record crush UNCHANGED  "$CRUSH_CFG" ;;
    11) record crush DRY-RUN    "would update $CRUSH_CFG" ;;
    *)  record crush ERROR         "write to $CRUSH_CFG failed (see the ✗ line above)" ;;
  esac
  info "crush auto-discovers models from /v1/models; restart crush to pick this up."
  info "for the TLS gateway export SSL_CERT_FILE=$(spec_field helixllm-gateway 3)"
}

# ===========================================================================
# Verification — each agent's own listing command, real output only.
# ===========================================================================
verify_agent() {
  local name="$1" out rc=0
  case "$name" in
    opencode)
      # OPENCODE_CONFIG points at a config FILE. NOTE: opencode still merges
      # the operator's global config on top, so a hit here proves the provider
      # is visible to opencode, not that it came only from this file.
      # In --sandbox mode also redirect OPENCODE_CONFIG_DIR, so the ~63 MB
      # @ai-sdk/openai-compatible install that a first listing triggers lands
      # in the sandbox instead of the operator's config directory.
      if [ -n "$SANDBOX" ]; then
        out="$(OPENCODE_CONFIG="$OPENCODE_CFG" OPENCODE_CONFIG_DIR="$OPENCODE_DIR" \
               timeout 300 opencode models 2>&1 || true)"
      else
        out="$(OPENCODE_CONFIG="$OPENCODE_CFG" timeout 300 opencode models 2>&1 || true)"
      fi
      ;;
    pi)
      out="$(PI_CODING_AGENT_DIR="$PI_DIR" PI_OFFLINE=1 timeout 120 pi --list-models 2>&1 || true)"
      ;;
    crush)
      # CRUSH_GLOBAL_CONFIG is a DIRECTORY containing crushrc (probed, not
      # assumed: passing the crushrc file path itself loads nothing).
      # --cwd keeps crush from writing a .crush/ data dir into the caller's cwd.
      out="$(SSL_CERT_FILE="$(spec_field helixllm-gateway 3)" \
             CRUSH_GLOBAL_CONFIG="$CRUSH_DIR" \
             CRUSH_GLOBAL_DATA="${WORK}/crush-data" \
             timeout 180 crush models --cwd "${WORK}" 2>&1 || true)"
      ;;
    *) return 1 ;;
  esac

  local pid hits total=0 found=0
  for pid in $HELIX_PROVIDERS; do
    total=$((total + 1))
    hits="$(printf '%s\n' "$out" | grep -c -- "$pid" || true)"
    if [ "$hits" -gt 0 ]; then
      ok "${name}: ${pid} visible (${hits} line(s))"
      printf '%s\n' "$out" | grep -- "$pid" | head -3 | redact | sed 's/^/      /'
      found=$((found + 1))
    else
      fail "${name}: ${pid} NOT listed"
    fi
  done

  if [ "$found" -eq "$total" ]; then
    record "$name" VERIFY-PASS "${found}/${total} providers listed"
  elif [ "$found" -gt 0 ]; then
    record "$name" VERIFY-PARTIAL "${found}/${total} providers listed"
    rc=1
  else
    record "$name" VERIFY-FAIL "0/${total} providers listed"
    printf '%s\n' "$out" | head -5 | redact | sed 's/^/      /'
    rc=1
  fi
  return $rc
}

# ===========================================================================
# main
# ===========================================================================
printf '%sHelix CLI-agent configuration fan-out%s\n' "$C_B" "$C_0"
[ -n "$SANDBOX" ] && warn "SANDBOX MODE — writing under ${SANDBOX}, the operator's real configs are untouched"
[ "$DRY_RUN" -eq 1 ] && warn "DRY RUN — nothing will be written"

section "Resolving Helix model surfaces"
resolve_all

section "Detecting installed CLI agents"
AGENTS_PRESENT=()
for a in opencode pi crush claude; do
  if p="$(agent_path "$a")"; then
    ok "$(printf '%-9s %s  %s' "$a" "$p" "$(agent_version "$a")")"
    AGENTS_PRESENT+=("$a")
  else
    if [ "$(type -t "$a" 2>/dev/null || true)" = "builtin" ]; then
      warn "${a}: resolves to a SHELL BUILTIN, not an installed agent — treated as absent"
    else
      warn "${a}: not installed"
    fi
    [ "$a" = "claude" ] || record "$a" SKIPPED "not installed"
  fi
done

present() { local a; for a in "${AGENTS_PRESENT[@]:-}"; do [ "$a" = "$1" ] && return 0; done; return 1; }

if present claude; then
  record claude SKIPPED "Anthropic Messages API only — cannot consume an OpenAI-compatible provider; wired via the Anthropic facade, not this script"
  warn "claude: SKIPPED — speaks the Anthropic Messages API only; the OpenAI-compatible surfaces are wired for it elsewhere"
else
  record claude SKIPPED "not installed"
fi

if [ "$DO_VERIFY" -eq 1 ]; then
  section "Verifying agent configuration (real command output)"
  VERIFY_RC=0
  for a in opencode pi crush; do
    selected "$a" || { warn "${a}: not selected (--only)"; continue; }
    if present "$a"; then
      printf '  %s%s%s\n' "$C_B" "$a" "$C_0"
      verify_agent "$a" || VERIFY_RC=1
    else
      warn "${a}: skipped — not installed"
    fi
  done
else
  section "Writing agent configurations"
  for a in opencode pi crush; do
    selected "$a" || { warn "${a}: not selected (--only)"; continue; }
    if present "$a"; then
      case "$a" in
        opencode) plan_opencode ;;
        pi)       plan_pi ;;
        crush)    plan_crush ;;
      esac
    else
      warn "${a}: skipped — not installed (no config written)"
    fi
  done
fi

section "Summary"
for line in "${SUMMARY[@]:-}"; do
  IFS=$'\t' read -r s_agent s_status s_detail <<< "$line"
  case "$s_status" in
    CONFIGURED|UNCHANGED|VERIFY-PASS) ok  "$(printf '%-9s %-14s %s' "$s_agent" "$s_status" "$s_detail")" ;;
    ERROR|VERIFY-FAIL)                fail "$(printf '%-9s %-14s %s' "$s_agent" "$s_status" "$s_detail")" ;;
    *)                                warn "$(printf '%-9s %-14s %s' "$s_agent" "$s_status" "$s_detail")" ;;
  esac
done

if [ -n "$SUMMARY_FILE" ]; then
  : > "$SUMMARY_FILE"
  for line in "${SUMMARY[@]:-}"; do
    IFS=$'\t' read -r s_agent s_status s_detail <<< "$line"
    printf '  %-9s %-12s %s\n' "$s_agent" "$s_status" "$s_detail" >> "$SUMMARY_FILE"
  done
fi

if [ "$DO_VERIFY" -eq 1 ]; then
  exit "${VERIFY_RC:-0}"
fi

# A run that recorded an ERROR did NOT do what it was asked to do. The former
# unconditional `exit 0` here is what let setup.sh print "CLI agent
# configuration complete" over a run whose every write had failed; exiting
# non-zero is what routes the operator to setup.sh's "reported problems"
# branch instead (CONST-035). SKIPPED is NOT an error — an absent agent, or a
# config we deliberately refuse to touch, is an honest §11.4.3 outcome.
ERROR_COUNT=0
for line in "${SUMMARY[@]:-}"; do
  IFS=$'\t' read -r s_agent s_status s_detail <<< "$line"
  case "$s_status" in
    ERROR|VERIFY-FAIL) ERROR_COUNT=$((ERROR_COUNT + 1)) ;;
  esac
done
if [ "$ERROR_COUNT" -gt 0 ]; then
  fail "${ERROR_COUNT} agent(s) reported ERROR — exiting non-zero so the caller sees the failure"
  exit 1
fi
exit 0
