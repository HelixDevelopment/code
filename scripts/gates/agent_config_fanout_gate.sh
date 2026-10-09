#!/usr/bin/env bash
# =============================================================================
# CM-AGENT-CONFIG-FANOUT
#
# Guards the "installer nothing calls" defect class for the CLI-agent config
# fan-out — the historical shape this whole effort exists to fix: HelixAgent
# already ships a 47-agent config generator that is never invoked from
# anywhere. A `scripts/install_agent_configs.sh` that exists, compiles, and
# does everything right is worth nothing to an operator if nothing in the
# setup path ever runs it.
#
# THE FOUR CHECKS
# ----------------
# (a) EXISTS + EXECUTABLE   — the installer file is present and has the
#     execute bit set. A source file nothing can run is the same defect as
#     a source file nothing calls; the fix isn't real until both hold.
# (b) SYNTACTICALLY VALID   — `bash -n` on the installer is clean. A syntax
#     error is a build-time fact this gate can catch for free, before any
#     of the more expensive checks below.
# (c) ACTUALLY WIRED        — the installer is INVOKED, in command position,
#     from setup.sh. This is the named defect: a correct installer that
#     setup.sh never calls is exactly as useless to an operator as if it did
#     not exist. "Invoked" is checked strictly (see WHY COMMAND POSITION
#     below) because a mere MENTION is not a call.
# (d) LIVE MODEL-ID AGREEMENT (§11.4.111) — every one of the seven Helix
#     model ids this WRITE task's contract table specifies MUST be declared
#     (checked via the boundary rule — see WHY A BOUNDARY CHECK below) is,
#     when actually declared, cross-checked against its provider's live
#     `/v1/models` listing. A model id the installer would hand to an agent
#     that the provider does not actually serve is a stale/guessed id — the
#     exact class §11.4.111 forbids ("resolve by stable name, not by an
#     enumeration index/guess"). An id that is NOT declared anywhere in the
#     installer's source is not a finding: it means that provider's models
#     are discovered dynamically at install time rather than hardcoded,
#     which is the CONST-036-compliant alternative, not a defect.
#
# WHY A BOUNDARY CHECK, NOT A BARE SUBSTRING
# --------------------------------------------
# A bare substring test for "helixagent-llm" would also match
# "helixagent-llm-legacy-2019" (a DIFFERENT, wrong id that merely CONTAINS
# the right one as a prefix) and falsely call it "declared and live". The
# real installer embeds model ids as ONE FIELD of a larger pipe-delimited
# record string (e.g. '...|32768|4096|qwen2.5-coder-3b-instruct-q4_k_m'), so
# an EARLIER version of this gate that required the id be wrapped in its own
# matching quote pair under-reported every real, hardcoded id — it never
# found anything to check. What actually distinguishes a real occurrence
# from a longer sibling string is not quoting but BOUNDARIES: the character
# immediately before/after the match must not itself be a valid
# model-id-continuation character ([A-Za-z0-9._-]) — a pipe, quote, space,
# comma, or start/end of file all qualify; a hyphen does not, so
# "helixagent-llm-legacy-2019" is correctly rejected while
# "...|helixagent-llm helixagent-debate..." is correctly accepted.
#
# WHY COMMAND POSITION, AND WHY HEREDOC BODIES ARE STRIPPED
# ----------------------------------------------------------
# Measured on the real setup.sh: its closing banner is a `cat <<EOF ... EOF`
# heredoc whose help text reads
#     Re-run / inspect   : ./scripts/install_agent_configs.sh --dry-run
#     Prove it works     : ./scripts/install_agent_configs.sh --verify
# Those are DOCUMENTATION, not calls. An earlier rule here accepted any
# non-comment line CONTAINING `scripts/<basename>`, so deleting the ONE real
# invocation still left the gate GREEN — reproduced by removing the real call
# and watching this gate print PASS. The gate was blind to the exact mutation
# its own FAIL message names, and RED-A could not catch it because that
# fixture spells the basename WITHOUT the `scripts/` prefix, so it never
# exercised this shape at all (RED-A2 below now does).
# Two independent narrowings:
#   1. heredoc BODIES are stripped before the search — text inside `cat <<EOF`
#      is output, not code, so it cannot invoke anything;
#   2. the match must sit in COMMAND POSITION — line start, optional
#      indentation, optionally preceded by if/then/else/elif/do/&&/|| — which
#      is what every real sibling invocation in setup.sh looks like
#      (init-submodules.sh, install-git-hooks.sh, install_systemd_units.sh)
#      and what a prose line beginning "Re-run / inspect   : ..." never is.
#
# THE SEVEN-ENTRY TABLE IS THIS TASK'S OWN CONTRACT, NOT A GUESS (§11.4.6)
# -------------------------------------------------------------------------
# Every provider id / base URL / model id below is taken verbatim from this
# WRITE task's brief. CONFIRMATION STATUS IS NOT UNIFORM (§11.4.6): the
# helixllm-coder and helixllm-gateway ids were independently confirmed live
# against the real running endpoints while this gate was authored (curl
# transcripts in the accompanying report). The HelixAgent (:7061) ids were NOT
# individually enumerated — the captured evidence is a single
# `curl :7061/v1/models -> 200`, with no id list — so per
# docs/guides/cli_agent_integration/README.md they are UNCONFIRMED and must be
# checked before being relied on. An earlier revision of this header claimed
# all seven were confirmed; that overstated the evidence. Pinning them here
# is not the same mistake helix_endpoint_agreement_gate.sh warns against
# (deriving an EXPECTED port from nothing but a guess) — this table IS the
# spec the installer is contractually required to implement.
#
# NO SYSTEMD, NO WRITES — this gate only ever performs read-only GET requests
# against the three already-live endpoints (never restarts/starts/stops any
# unit) and never writes to any real agent config; RED_MODE below operates
# exclusively on files under a throwaway mktemp dir plus one ephemeral local
# HTTP stub server this gate starts and stops itself.
#
# POLARITY (§11.4.115) — one source, two roles
#   RED_MODE=1  Reproduce ALL THREE historical defect shapes on synthesized
#               copies (never the real tree) and assert this guard FAILs on
#               each: (A) a valid, executable installer that no setup script
#               calls; (A2) a setup.sh that NAMES `scripts/<installer>` — with
#               the exact `scripts/` prefix — but only inside an echo and a
#               heredoc, never invoking it (the mention-is-not-invocation
#               shape that the pre-fix check accepted); (B) a valid, wired
#               installer that declares a real model id for a provider whose
#               live listing (an ephemeral local stub server, not real
#               infrastructure) does not actually serve it.
#   RED_MODE=0  DEFAULT. The standing GREEN regression guard.
#
# EXIT CODES
#   0  GREEN — every check that could run passed (skips allowed for (d))
#   1  FAIL  — a real finding: missing/non-executable/invalid installer, not
#              wired into setup.sh, or a declared model id absent from its
#              provider's live listing
#   2  SKIP  — nothing could be certified at all (e.g. python3 absent)
# =============================================================================
set -uo pipefail

ROOT="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd)"
GATE="CM-AGENT-CONFIG-FANOUT"
RED_MODE="${RED_MODE:-0}"

INSTALLER="${INSTALLER_OVERRIDE:-$ROOT/scripts/install_agent_configs.sh}"
SETUP_SH="${SETUP_SH_OVERRIDE:-$ROOT/setup.sh}"
CA_CERT="${CA_CERT_OVERRIDE:-$ROOT/submodules/helix_llm/certs/cert.pem}"

echo "$GATE  RED_MODE=$RED_MODE"

# =============================================================================
# RED_MODE: §11.4.115 reproduce-the-defect-on-a-broken-artifact
# =============================================================================
if [[ "$RED_MODE" == "1" ]]; then
  TMP="$(mktemp -d)"
  cleanup_red() {
    [[ -n "${STUB_PID:-}" ]] && kill "$STUB_PID" >/dev/null 2>&1
    rm -rf "$TMP"
  }
  trap cleanup_red EXIT

  # --- RED-A: valid + executable installer, never wired into setup.sh ------
  cat > "$TMP/install_agent_configs.sh" <<'EOF'
#!/usr/bin/env bash
echo "synthetic pre-fix installer — deliberately never invoked from setup.sh"
exit 0
EOF
  chmod +x "$TMP/install_agent_configs.sh"
  cat > "$TMP/setup.sh" <<'EOF'
#!/usr/bin/env bash
echo "this synthetic setup.sh deliberately never calls install_agent_configs.sh"
EOF
  OUT_A="$(RED_MODE=0 INSTALLER_OVERRIDE="$TMP/install_agent_configs.sh" SETUP_SH_OVERRIDE="$TMP/setup.sh" "$0" 2>&1)"
  RC_A=$?
  if [[ $RC_A -eq 0 ]]; then
    echo "$GATE: RED-A FAIL — the gate PASSed on a synthetic installer that setup.sh never calls. It is a blind test." >&2
    printf '%s\n' "$OUT_A" | sed 's/^/    /' >&2
    exit 1
  fi
  if ! grep -qi 'not invoked\|nothing invokes\|not referenced' <<<"$OUT_A"; then
    echo "$GATE: RED-A FAIL — the gate FAILed on the synthetic 'never wired' installer, but not for that reason. Output:" >&2
    printf '%s\n' "$OUT_A" | sed 's/^/    /' >&2
    exit 1
  fi
  echo "$GATE: RED-A OK — gate FAILs on a valid installer that setup.sh never calls, citing:"
  grep -i 'not invoked\|nothing invokes\|not referenced' <<<"$OUT_A" | sed 's/^/    /'

  # --- RED-A2: setup.sh NAMES scripts/<installer> — with the exact
  #     `scripts/` prefix RED-A lacks — but only inside an echo and a heredoc
  #     banner, exactly like the real setup.sh closing banner. This is the
  #     shape the pre-fix check accepted, so deleting the real invocation left
  #     the gate GREEN. RED-A cannot cover it (its fixture never spells the
  #     `scripts/` prefix at all), which is precisely why this second fixture
  #     exists. -------------------------------------------------------------
  cat > "$TMP/setup_a2.sh" <<'EOF'
#!/usr/bin/env bash
echo "To wire your CLI agents, run ./scripts/install_agent_configs.sh --dry-run"
cat <<BANNER
  Re-run / inspect   : ./scripts/install_agent_configs.sh --dry-run
  Prove it works     : ./scripts/install_agent_configs.sh --verify
BANNER
EOF
  OUT_A2="$(RED_MODE=0 INSTALLER_OVERRIDE="$TMP/install_agent_configs.sh" SETUP_SH_OVERRIDE="$TMP/setup_a2.sh" "$0" 2>&1)"
  RC_A2=$?
  if [[ $RC_A2 -eq 0 ]]; then
    echo "$GATE: RED-A2 FAIL — the gate PASSed on a setup.sh that only MENTIONS scripts/install_agent_configs.sh (in an echo and a heredoc banner) and never invokes it. A mention is not a call; this is a blind test." >&2
    printf '%s\n' "$OUT_A2" | sed 's/^/    /' >&2
    exit 1
  fi
  if ! grep -qi 'not invoked\|nothing invokes\|not referenced' <<<"$OUT_A2"; then
    echo "$GATE: RED-A2 FAIL — the gate FAILed on the mention-only fixture, but not for that reason. Output:" >&2
    printf '%s\n' "$OUT_A2" | sed 's/^/    /' >&2
    exit 1
  fi
  echo "$GATE: RED-A2 OK — gate FAILs when scripts/<installer> is only MENTIONED (echo + heredoc), never invoked, citing:"
  grep -i 'not invoked\|nothing invokes\|not referenced' <<<"$OUT_A2" | sed 's/^/    /'

  # --- RED-A3 (positive control): the SAME mention-only setup.sh, plus one
  #     real invocation in command position, MUST make the gate stop citing
  #     the wiring defect. Without this, RED-A2 could be satisfied by a check
  #     that simply always FAILs — that would be a different bluff.
  cat > "$TMP/setup_a3.sh" <<'EOF'
#!/usr/bin/env bash
if ./scripts/install_agent_configs.sh --summary-file /dev/null; then :; fi
cat <<BANNER
  Re-run / inspect   : ./scripts/install_agent_configs.sh --dry-run
BANNER
EOF
  OUT_A3="$(RED_MODE=0 INSTALLER_OVERRIDE="$TMP/install_agent_configs.sh" SETUP_SH_OVERRIDE="$TMP/setup_a3.sh" "$0" 2>&1)"
  if grep -qi 'not invoked\|nothing invokes' <<<"$OUT_A3"; then
    echo "$GATE: RED-A3 FAIL — the gate reported the wiring defect against a setup.sh that DOES invoke the installer in command position. The check over-fires; it is not a usable guard." >&2
    printf '%s\n' "$OUT_A3" | sed 's/^/    /' >&2
    exit 1
  fi
  echo "$GATE: RED-A3 OK — the same fixture WITH a real command-position invocation is not flagged as unwired."

  # --- RED-A4: the two heredoc opener forms the stripper FAILED OPEN on ----
  #     `cat <<\EOF` (backslash-quoted delimiter -- the conventional way to
  #     disable expansion in a help banner, i.e. exactly what setup.sh's
  #     closing banner becomes the moment someone puts a `$` in it) and
  #     `cat <<$D` (variable delimiter). The pre-fix stripper only entered
  #     heredoc mode when the delimiter matched ^[A-Za-z_] AFTER quote
  #     stripping, so neither form was recognised, the banner BODY was treated
  #     as CODE, and a body line satisfied the command-position invocation
  #     regex -- the gate PASSed with NO real call. Measured on the pre-fix
  #     gate: rc=0 and "PASS: ... is invoked" for both forms. RED-A2 cannot
  #     cover this (its banner uses a plain `<<BANNER`, which WAS stripped).
  for A4_FORM in backslash variable; do
    if [[ "$A4_FORM" == "backslash" ]]; then
      cat > "$TMP/setup_a4.sh" <<'EOF'
#!/usr/bin/env bash
echo "documentation only"
cat <<\BANNER
  ./scripts/install_agent_configs.sh --dry-run
BANNER
EOF
    else
      cat > "$TMP/setup_a4.sh" <<'EOF'
#!/usr/bin/env bash
echo "documentation only"
D=BANNER
cat <<$D
  ./scripts/install_agent_configs.sh --dry-run
BANNER
EOF
    fi
    OUT_A4="$(RED_MODE=0 INSTALLER_OVERRIDE="$TMP/install_agent_configs.sh" SETUP_SH_OVERRIDE="$TMP/setup_a4.sh" "$0" 2>&1)"
    RC_A4=$?
    if [[ $RC_A4 -eq 0 ]]; then
      echo "$GATE: RED-A4 FAIL ($A4_FORM delimiter) — the gate PASSed on a setup.sh whose ONLY occurrence of scripts/install_agent_configs.sh is inside a heredoc banner body. The heredoc stripper failed open on this opener form, so the banner text was read as code. This is the blind test the gate exists to prevent." >&2
      printf '%s\n' "$OUT_A4" | sed 's/^/    /' >&2
      exit 1
    fi
    if ! grep -qi 'not invoked\|nothing invokes\|not referenced' <<<"$OUT_A4"; then
      echo "$GATE: RED-A4 FAIL ($A4_FORM delimiter) — the gate FAILed on the heredoc-banner-only fixture, but not for the wiring reason. Output:" >&2
      printf '%s\n' "$OUT_A4" | sed 's/^/    /' >&2
      exit 1
    fi
    echo "$GATE: RED-A4 OK ($A4_FORM delimiter) — a mention inside a <<\\EOF / <<\$D heredoc body is not mistaken for a call."
  done

  # --- RED-A5: the three QUOTED-ARGUMENT / STRING-BODY shapes the round-3
  #     loosening failed open on. Round 3 widened the command-position regex
  #     with an optional leading quote so `"$ROOT/scripts/x.sh"` would stop
  #     false-FAILing (§11.4.201) -- but a bare optional quote also let a
  #     quoted ARGUMENT satisfy "command position". Measured on that gate: all
  #     three printed `PASS: ... is invoked` with ZERO real call anywhere, so
  #     rewriting the closing banner as a continued printf or an array (a
  #     routine edit) and then deleting the real call at setup.sh:234 would
  #     have left the gate GREEN. RED-A2/A4 cannot cover these: A2 is an echo
  #     and a plain heredoc, A4 is a heredoc opener form -- none of them is a
  #     continuation, an array element, or a multi-line string body.
  # Every form is driven across every realistic path PREFIX. Round 4's
  # narrowing of the quote prefix (`"?` -> `"$`) killed only the `"./`-rooted
  # spelling these three fixtures happened to use, so the `"$`-rooted twin of
  # the SAME form -- `HELP=( "$ROOT/scripts/x.sh --dry-run" )` -- remained a
  # fail-open while RED-A5 still printed OK. A self-test that certifies
  # coverage it does not have is itself the bluff (§1.1), so the prefix is now
  # an explicit axis: `./`, `$ROOT/`, `${ROOT}/`, `$PWD/`, and bare. The
  # fourth form is the reproduced defect verbatim: a heredoc banner (the shape
  # RED-A2/A4 cover) followed by the array element that actually slipped
  # through, so the exact measured fail-open is a standing fixture.
  #
  # PER-FIXTURE MECHANISM, DECLARED AND ASSERTED (§1.1, round-5 finding F2).
  # A fixture whose NAME implies one rejecting mechanism while a DIFFERENT one
  # actually does the rejecting is a latent bluff: the mechanism the name
  # advertises can be deleted and the fixture still passes, so it falsifies
  # nothing. MEASURED on this gate: with the continuation JOIN disabled (the
  # `next` in the stripper's line-buffering rule), RED still reported OK on
  # every fixture -- 27/27, rc=0 -- because `continued-printf`'s payload is a
  # QUOTED argument, refused by the quote-closure rule with or without the
  # join. The join was load-bearing and unfalsifiable at the same time, which
  # is round 4's defect one layer deeper.
  #
  # Two things follow, and both are done here. FIRST, `continued-echo-unquoted`
  # exists precisely to make the join falsifiable: `echo \` + newline +
  # `  ./scripts/x.sh --dry-run` is refused WITH the join (the joined logical
  # line begins with `echo`, which is no command position) and ACCEPTED
  # without it (the second physical line is then a bare path in command
  # position). Deleting the join FAILs this fixture -- measured, both ways.
  # SECOND, every form DECLARES the mechanism that refuses it and that
  # declaration is ASSERTED against this gate's OWN stripper, extracted from
  # $0 -- not asserted in a comment, where it can rot:
  #   stripper-dropped -> the basename line never reaches the regex at all
  #   regex-miss       -> the stripper EMITS the line and the regex refuses it
  # Measured after the round-5 array-depth fix: continued-printf and
  # continued-echo-unquoted are regex-miss; array-literal, multiline-string
  # and banner-plus-array are stripper-dropped. `array-literal` was regex-miss
  # BEFORE that fix -- a mechanism change under a fixture whose name did not
  # move, which is exactly the drift this assertion now catches.
  awk '/^SETUP_CODE="\$\(awk /{f=1;next} f&&/^'"'"' "\$SETUP_SH"\)"\)?$/{exit} f{print}' "$0" > "$TMP/stripper.awk"
  # SELF-EXTRACTION INTEGRITY. `[[ ! -s ]]` only catches TOTAL emptiness. A
  # PARTIAL extraction (the head regex drifts, the terminator regex matches
  # early) yields a non-empty but TRUNCATED awk program that runs and drops
  # lines the real stripper keeps -- and the per-fixture assertion below then
  # reports `MECHANISM DRIFT`, naming a FIXTURE's declared mechanism, when the
  # real cause is this extraction. Two sentinels, one at each end of the awk
  # program, make truncation from either side name ITSELF.
  A5_SENT_HEAD='STRIPPER-SELF-EXTRACT-SENTINEL-HEAD-a41c'
  A5_SENT_TAIL='STRIPPER-SELF-EXTRACT-SENTINEL-TAIL-a41c'
  A5_MISSING=""
  grep -qF -- "$A5_SENT_HEAD" "$TMP/stripper.awk" || A5_MISSING="head"
  grep -qF -- "$A5_SENT_TAIL" "$TMP/stripper.awk" || A5_MISSING="${A5_MISSING:+$A5_MISSING and }tail"
  if [[ ! -s "$TMP/stripper.awk" || -n "$A5_MISSING" ]]; then
    if [[ ! -s "$TMP/stripper.awk" ]]; then
      A5_WHY="the extracted file is EMPTY"
    else
      A5_WHY="the extracted program is non-empty ($(wc -l < "$TMP/stripper.awk") lines) but its $A5_MISSING sentinel is missing, so the extraction is TRUNCATED, not the stripper broken"
    fi
    echo "$GATE: RED-A5 FAIL (harness) — could not extract this gate's own stripper from \$0: $A5_WHY. The per-fixture mechanism assertion below would be vacuous, or worse would report MECHANISM DRIFT against a FIXTURE when the real cause is this extraction. A vacuous or misattributed assertion is the bluff this block exists to prevent, so this is a FAIL, not a skip." >&2
    exit 1
  fi
  for A5_FORM in continued-printf continued-echo-unquoted array-literal multiline-string banner-plus-array; do
    for A5_PREFIX in './' '$ROOT/' '${ROOT}/' '$PWD/' ''; do
      A5_ARG="${A5_PREFIX}scripts/install_agent_configs.sh --dry-run"
      A5_LABEL="$A5_FORM, prefix=${A5_PREFIX:-<bare>}"
      case "$A5_FORM" in
        continued-printf)
          A5_LINES=( '#!/usr/bin/env bash' 'printf "%s\n" \' "  \"$A5_ARG\"" )
          A5_MECH=regex-miss ;;
        continued-echo-unquoted)
          A5_LINES=( '#!/usr/bin/env bash' 'echo \' "  $A5_ARG" )
          A5_MECH=regex-miss ;;
        array-literal)
          A5_LINES=( '#!/usr/bin/env bash' 'HELP=(' "  \"$A5_ARG\"" ')' )
          A5_MECH=stripper-dropped ;;
        multiline-string)
          A5_LINES=( '#!/usr/bin/env bash' 'BANNER="' "$A5_ARG" '"' )
          A5_MECH=stripper-dropped ;;
        banner-plus-array)
          A5_LINES=( '#!/usr/bin/env bash' 'cat <<BANNER' "  run $A5_ARG to wire agents" \
                     'BANNER' 'HELP=(' "  \"$A5_ARG\"" ')' )
          A5_MECH=stripper-dropped ;;
      esac
      printf '%s\n' "${A5_LINES[@]}" > "$TMP/setup_a5.sh"
      # Which mechanism ACTUALLY refuses this fixture? If the stripper emits a
      # line carrying the basename, the regex is what refused it; if it emits
      # none, the stripper dropped it before the regex ever saw it.
      if awk -f "$TMP/stripper.awk" "$TMP/setup_a5.sh" 2>/dev/null | grep -qF -- 'install_agent_configs.sh'; then
        A5_SEEN=regex-miss
      else
        A5_SEEN=stripper-dropped
      fi
      if [[ "$A5_SEEN" != "$A5_MECH" ]]; then
        echo "$GATE: RED-A5 FAIL ($A5_LABEL) — MECHANISM DRIFT: this fixture is declared to be refused by '$A5_MECH' but is actually refused by '$A5_SEEN'. A fixture that passes for a different reason than the one it names cannot falsify the mechanism it is named for, so that mechanism is now unguarded (§1.1). Fix the declaration or restore the mechanism — do not delete this assertion." >&2
        exit 1
      fi
      OUT_A5="$(RED_MODE=0 INSTALLER_OVERRIDE="$TMP/install_agent_configs.sh" SETUP_SH_OVERRIDE="$TMP/setup_a5.sh" "$0" 2>&1)"
      RC_A5=$?
      if [[ $RC_A5 -eq 0 ]]; then
        echo "$GATE: RED-A5 FAIL ($A5_LABEL) — the gate PASSed on a setup.sh whose ONLY occurrence of scripts/install_agent_configs.sh is a quoted ARGUMENT or a string-literal body, never a command. This is the fail-open the command-position check exists to prevent." >&2
        printf '%s\n' "$OUT_A5" | sed 's/^/    /' >&2
        printf '%s\n' "--- fixture ---" >&2
        sed 's/^/    /' "$TMP/setup_a5.sh" >&2
        exit 1
      fi
      if ! grep -qi 'not invoked\|nothing invokes\|not referenced' <<<"$OUT_A5"; then
        echo "$GATE: RED-A5 FAIL ($A5_LABEL) — the gate FAILed on the quoted-argument fixture, but not for the wiring reason. Output:" >&2
        printf '%s\n' "$OUT_A5" | sed 's/^/    /' >&2
        exit 1
      fi
      echo "$GATE: RED-A5 OK ($A5_LABEL; refused by $A5_SEEN) — a quoted argument, an array element or a string-literal body is not mistaken for a call."
    done
  done

  # --- RED-A6 (positive control for A5): the invocation forms A5's narrower
  #     rule must still ACCEPT. Without this, RED-A5 could be satisfied by a
  #     check that rejects every quote and every continuation -- which would
  #     be a §11.4.201 false refusal, i.e. a different defect, not a fix.
  for A6_FORM in quoted-var-path continued-interpreter if-bang env-assign subshell; do
    case "$A6_FORM" in
      quoted-var-path)      A6_LINE='"$ROOT/scripts/install_agent_configs.sh" --dry-run' ;;
      continued-interpreter) A6_LINE=$'bash \\\n  ./scripts/install_agent_configs.sh --dry-run' ;;
      if-bang)              A6_LINE='if ! ./scripts/install_agent_configs.sh --dry-run; then :; fi' ;;
      env-assign)           A6_LINE='HELIX_QUIET=1 ./scripts/install_agent_configs.sh --dry-run' ;;
      subshell)             A6_LINE='( ./scripts/install_agent_configs.sh --dry-run )' ;;
    esac
    { echo '#!/usr/bin/env bash'; printf '%s\n' "$A6_LINE"; } > "$TMP/setup_a6.sh"
    OUT_A6="$(RED_MODE=0 INSTALLER_OVERRIDE="$TMP/install_agent_configs.sh" SETUP_SH_OVERRIDE="$TMP/setup_a6.sh" "$0" 2>&1)"
    if grep -qi 'not invoked\|nothing invokes' <<<"$OUT_A6"; then
      echo "$GATE: RED-A6 FAIL ($A6_FORM) — the gate reported the wiring defect against a setup.sh that DOES invoke the installer. The command-position rule over-fires (§11.4.201); it is not a usable guard." >&2
      printf '%s\n' "$OUT_A6" | sed 's/^/    /' >&2
      exit 1
    fi
    echo "$GATE: RED-A6 OK ($A6_FORM) — a real invocation in this form is still accepted."
  done

  # --- RED-A7: BASH EXECUTION ORACLE -------------------------------------
  #     A5/A6 assert the gate's verdict against a mechanism the FIXTURE AUTHOR
  #     DECLARED (`A5_MECH=stripper-dropped` / `regex-miss`). That is a real
  #     assertion, but it can only ever encode what the author BELIEVED bash
  #     does with a shape. Every fail-open this gate has shipped was exactly
  #     that belief being wrong -- the round-6 defect (a heredoc opened on a
  #     line that BEGINS inside a string body or an array literal was never
  #     registered, so its banner was read as CODE) is the twelfth, and the
  #     paren-delimited heredoc caught below is the thirteenth.
  #
  #     A7 removes the author from the loop. Each fixture is RUN UNDER BASH
  #     with an INSTRUMENTED installer stub that appends a marker when it is
  #     really executed. BASH -- not a regex, not a comment, not a declared
  #     mechanism -- decides whether the fixture is a MENTION or a CALL, and
  #     the gate's verdict is asserted against that:
  #       bash did NOT execute it + gate says PASS  -> FAIL-OPEN, always fatal
  #       bash DID execute it     + gate refuses    -> false refusal (§11.4.201),
  #                                                    fatal unless the fixture
  #                                                    is a DECLARED fail-closed
  #                                                    limit (see HONEST LIMITS)
  #       bash DID execute it     + gate PASSes but the fixture is declared a
  #                                 limit            -> DRIFT: the limit is no
  #                                                    longer a limit and the
  #                                                    documented list is stale
  #     The declared class is itself cross-checked against the oracle, so a
  #     fixture mislabelled `accept` that bash never runs is a harness FAIL,
  #     not a silent pass.
  #
  #     ORACLE SEMANTICS (round 7, stated because it is easy to over-read).
  #     The oracle answers "did bash execute the installer during THIS ONE
  #     RUN", NOT "is this call reachable". A fixture whose real call sits
  #     behind a condition that is false at run time is therefore scored
  #     not-executed, and if the gate accepts it -- correctly, since it IS in
  #     command position -- the harness reports FAIL-OPEN. That verdict is
  #     about the FIXTURE, not the gate: such shapes belong under FAIL-OPEN
  #     LIMITS, and must not be added to this corpus as `refuse`. Every
  #     fixture here is unconditional, so the two readings coincide.
  A7_DIR="$TMP/a7"
  mkdir -p "$A7_DIR/scripts"
  cat > "$A7_DIR/scripts/install_agent_configs.sh" <<'A7EOF'
#!/usr/bin/env bash
# INSTRUMENTED STUB: appends a marker ONLY when it is really executed.
printf 'A7_REAL_INVOCATION args=%s\n' "$*" >> "${A7_ORACLE_LOG:?}"
exit 0
A7EOF
  chmod +x "$A7_DIR/scripts/install_agent_configs.sh"
  A7_TAB="$(printf '\t')"
  A7_SQ="$(printf '\047')"
  A7_EXEC_SEEN=0; A7_NOEXEC_SEEN=0
  for A7_CASE in \
      "array-close-heredoc|refuse" \
      "string-close-heredoc|refuse" \
      "array-close-heredoc-dash|refuse" \
      "heredoc-paren-delimiter|refuse" \
      "echo-paren-mention|refuse" \
      "echo-amp-mention|refuse" \
      "case-branch|accept" \
      "case-alt-branch|accept" \
      "test-guard-and|accept" \
      "plain-call|accept" \
      "backtick-env-assign-mention|refuse" \
      "backtick-command-substitution|accept" \
      "multi-heredoc-same-command|refuse" \
      "multi-heredoc-two-commands|refuse" \
      "param-default-paren-mention|refuse" \
      "here-string-then-call|accept" \
      "array-command-substitution|limit" \
      "ansi-c-escaped-quote-mention|refuse" \
      "ansi-c-escaped-quote-then-call|accept" \
      "backslash-space-assign-mention|refuse"; do
    A7_NAME="${A7_CASE%%|*}"; A7_CLASS="${A7_CASE##*|}"
    case "$A7_NAME" in
      array-close-heredoc)
        A7_LINES=( 'HELP=(' '  "some help text"' ') ; cat <<BANNER' \
                   '  ./scripts/install_agent_configs.sh --dry-run' 'BANNER' ) ;;
      string-close-heredoc)
        A7_LINES=( 'MSG="line one' 'line two" ; cat <<BANNER' \
                   '  ./scripts/install_agent_configs.sh --verify' 'BANNER' ) ;;
      array-close-heredoc-dash)
        A7_LINES=( 'ROOT=.' 'FLAGS=(' '  --one --two' ') ; cat <<-BANNER' \
                   "${A7_TAB}\"\$ROOT/scripts/install_agent_configs.sh\" --dry-run" \
                   "${A7_TAB}BANNER" ) ;;
      heredoc-paren-delimiter)
        A7_LINES=( "cat <<')'" './scripts/install_agent_configs.sh --dry-run' ')' ) ;;
      echo-paren-mention)
        A7_LINES=( 'echo "a) ./scripts/install_agent_configs.sh --dry-run"' ) ;;
      echo-amp-mention)
        A7_LINES=( 'echo "a && ./scripts/install_agent_configs.sh --dry-run"' ) ;;
      case-branch)
        A7_LINES=( 'case "${1:---agents}" in' \
                   '  --agents) ./scripts/install_agent_configs.sh --dry-run ;;' 'esac' ) ;;
      case-alt-branch)
        A7_LINES=( 'case "${1:---agents}" in' \
                   '  -a|--agents) ./scripts/install_agent_configs.sh --dry-run ;;' 'esac' ) ;;
      test-guard-and)
        A7_LINES=( '[ -x ./scripts/install_agent_configs.sh ] && ./scripts/install_agent_configs.sh --dry-run' ) ;;
      plain-call)
        A7_LINES=( './scripts/install_agent_configs.sh --dry-run' ) ;;
      backtick-env-assign-mention)
        # ROUND-7 B1 (fail-open #14). The env-assignment prefix class admitted
        # a BACKTICK, so `MSG=`+backtick+`echo ...` matched as a real call.
        # bash runs `echo`; the installer is never executed.
        A7_LINES=( 'MSG=`echo ./scripts/install_agent_configs.sh --dry-run`' \
                   'printf "%s\n" "${MSG:-none}"' ) ;;
      backtick-command-substitution)
        # The positive twin of B1: the same backtick, but around a REAL call.
        # Excluding the backtick from the value class alone would leave this a
        # FALSE REFUSAL, so the prefix admits a leading backtick symmetrically
        # to `$(`. Both directions are asserted, in this fixture and the one
        # above.
        A7_LINES=( 'OUT=`./scripts/install_agent_configs.sh --dry-run`' \
                   'printf "%s\n" "${OUT:-none}"' ) ;;
      multi-heredoc-same-command)
        # ROUND-7 B2 (fail-open #15). TWO heredocs on ONE logical line. The
        # scanner registered only the FIRST (`hdpos == 0`), so after body A
        # terminated, body B was read as CODE.
        A7_LINES=( 'cat <<A <<B' 'first body' 'A' \
                   './scripts/install_agent_configs.sh --dry-run' 'B' ) ;;
      multi-heredoc-two-commands)
        # The same defect spelled with two commands separated by `;`, which is
        # the shape a help banner is far more likely to take.
        A7_LINES=( 'cat <<A; cat <<B' 'first body' 'A' \
                   './scripts/install_agent_configs.sh --dry-run' 'B' ) ;;
      param-default-paren-mention)
        # ROUND-7 B3. Valid bash: the case-pattern alternative
        # (`[^[:space:]"'"'"'();&]+\)`) admitted `${X:-a)`, so a parameter
        # expansion default became a "case pattern" prefix. bash expands it and
        # tries to run `a)`, never the installer.
        A7_LINES=( '${X:-a) ./scripts/install_agent_configs.sh --dry-run}' ) ;;
      here-string-then-call)
        # ROUND-7 B4. A `<<<` HERE-STRING was registered as a heredoc from its
        # SECOND `<` (the third-character exclusion only fired at the first
        # position), so the rest of the file was swallowed as a body and the
        # REAL call below vanished -- fail-CLOSED but MUTE, no diagnostic at
        # all, which is the same §11.4.201 defect the arithmetic fix treated.
        A7_LINES=( 'read -r VAR <<< "hello"' \
                   './scripts/install_agent_configs.sh --dry-run' ) ;;
      array-command-substitution)
        A7_LINES=( 'HELP=( $(./scripts/install_agent_configs.sh --dry-run) )' \
                   'printf "%s\n" "${HELP[@]:-none}"' ) ;;
      ansi-c-escaped-quote-mention)
        # ROUND-8 B1 (fail-open #16). ANSI-C quoting: inside $'...' a backslash
        # is an ESCAPE, so backslash-quote does NOT close the string. The
        # scanner had one single-quote state and used the PLAIN rule (where a
        # backslash is literal) for both, so it closed the string early and
        # read the banner line as CODE. bash: ONE assignment, ZERO executions.
        A7_LINES=( "MSG=\$${A7_SQ}help text\\${A7_SQ}" \
                   '  ./scripts/install_agent_configs.sh --dry-run' \
                   "${A7_SQ}" ) ;;
      ansi-c-escaped-quote-then-call)
        # The MIRROR of the above, and the reason a one-state scanner cannot
        # simply be made stricter: with the plain rule, $'a\'b' leaves the
        # scanner INSIDE a phantom string, so the REAL call below is dropped
        # with no diagnostic -- fail-closed but MUTE (11.4.201). Both
        # directions are asserted, here and in the fixture above.
        A7_LINES=( "X=\$${A7_SQ}a\\${A7_SQ}b${A7_SQ}" \
                   './scripts/install_agent_configs.sh --dry-run' ) ;;
      backslash-space-assign-mention)
        # ROUND-8 B2 (fail-open #17), and it was round 7 own fix one character
        # wide: the env-assignment value class excluded quote, backtick,
        # whitespace and open-paren but NOT the BACKSLASH, so a
        # backslash-escaped space kept the VALUE going in bash while the regex
        # read the space as its separator and the path as command position.
        # bash: ONE assignment whose value is the literal
        # "x ./scripts/install_agent_configs.sh", ZERO executions.
        A7_LINES=( 'MSG=x\ ./scripts/install_agent_configs.sh' \
                   'printf "%s\n" "${MSG:-none}"' ) ;;
    esac
    { echo '#!/usr/bin/env bash'; printf '%s\n' "${A7_LINES[@]}"; } > "$A7_DIR/setup_a7.sh"

    if ! bash -n "$A7_DIR/setup_a7.sh" 2>/dev/null; then
      echo "$GATE: RED-A7 FAIL (harness, $A7_NAME) — the fixture is not valid bash, so the execution oracle cannot decide anything about it. A fixture bash refuses to parse certifies nothing." >&2
      exit 1
    fi
    A7_LOG="$A7_DIR/oracle.log"; : > "$A7_LOG"
    # ROUND-7 B7(b): stdin is redirected from /dev/null. Without it the
    # fixture INHERITS this gate's stdin, so a fixture containing a `read`
    # blocks until the 20 s timeout fires and is then scored not-executed for
    # a reason that has nothing to do with the shape under test.
    ( cd "$A7_DIR" && A7_ORACLE_LOG="$A7_LOG" timeout 20 bash ./setup_a7.sh ) >/dev/null 2>&1 </dev/null
    if grep -qF -- 'A7_REAL_INVOCATION' "$A7_LOG"; then
      A7_ORACLE=executed; A7_EXEC_SEEN=$((A7_EXEC_SEEN + 1))
    else
      A7_ORACLE=not-executed; A7_NOEXEC_SEEN=$((A7_NOEXEC_SEEN + 1))
    fi

    OUT_A7="$(RED_MODE=0 INSTALLER_OVERRIDE="$A7_DIR/scripts/install_agent_configs.sh" \
              SETUP_SH_OVERRIDE="$A7_DIR/setup_a7.sh" "$0" 2>&1)"
    if grep -q 'is invoked (in command position' <<<"$OUT_A7"; then A7_GATE=pass; else A7_GATE=refuse; fi

    # the DECLARED class must agree with what bash actually did
    if [[ "$A7_CLASS" == "refuse" && "$A7_ORACLE" == "executed" ]] \
       || [[ "$A7_CLASS" != "refuse" && "$A7_ORACLE" == "not-executed" ]]; then
      echo "$GATE: RED-A7 FAIL (harness, $A7_NAME) — the fixture is declared '$A7_CLASS' but bash reports '$A7_ORACLE'. The fixture, not the gate, is wrong; a mislabelled fixture makes the assertion below meaningless." >&2
      exit 1
    fi

    if [[ "$A7_ORACLE" == "not-executed" && "$A7_GATE" == "pass" ]]; then
      echo "$GATE: RED-A7 FAIL ($A7_NAME) — FAIL-OPEN. bash executed the installer ZERO times for this setup.sh, yet the gate reported it invoked. Deleting the one real call would leave this gate GREEN with no invocation anywhere — the exact defect it exists to catch." >&2
      printf '%s\n' "$OUT_A7" | sed 's/^/    /' >&2
      exit 1
    fi
    if [[ "$A7_ORACLE" == "executed" && "$A7_GATE" == "refuse" && "$A7_CLASS" != "limit" ]]; then
      echo "$GATE: RED-A7 FAIL ($A7_NAME) — FALSE REFUSAL (§11.4.201). bash really executed the installer for this setup.sh, yet the gate refused it. Refusing a real call is a different defect, not a stricter gate." >&2
      printf '%s\n' "$OUT_A7" | sed 's/^/    /' >&2
      exit 1
    fi
    if [[ "$A7_CLASS" == "limit" && "$A7_GATE" == "pass" ]]; then
      echo "$GATE: RED-A7 FAIL ($A7_NAME) — DRIFT. This shape is listed under HONEST LIMITS as a fail-CLOSED false refusal, but the gate now ACCEPTS it. The limits list is stale: remove the entry (and this fixture class) so the documentation matches the measured behaviour." >&2
      exit 1
    fi
    echo "$GATE: RED-A7 OK ($A7_NAME) — bash: $A7_ORACLE; gate: $A7_GATE (declared $A7_CLASS)."
  done
  if [[ $A7_EXEC_SEEN -eq 0 || $A7_NOEXEC_SEEN -eq 0 ]]; then
    echo "$GATE: RED-A7 FAIL (harness) — the oracle produced only one verdict (executed=$A7_EXEC_SEEN, not-executed=$A7_NOEXEC_SEEN). With no example of BOTH, the marker mechanism is unfalsified: a stub that never runs, or a marker that is always present, would look identical to a working oracle." >&2
    exit 1
  fi
  # The claim is scoped to THIS fixture set, deliberately (round 7). The
  # earlier wording read as a property of the gate; it is a property of the
  # corpus. Shapes outside it are measured and listed under FAIL-OPEN LIMITS
  # and HONEST LIMITS -- FAIL-CLOSED above, not covered by this line.
  echo "$GATE: RED-A7 OK — execution-oracle agreement over $((A7_EXEC_SEEN + A7_NOEXEC_SEEN)) fixtures (bash executed $A7_EXEC_SEEN, did not execute $A7_NOEXEC_SEEN); no fail-open and no undeclared false refusal AMONG THESE FIXTURES (this is not a claim about shapes outside the corpus -- see FAIL-OPEN LIMITS)."

  # --- RED-B: wired installer declaring a real id absent from a stub's
  #     live listing (an ephemeral local server, not real infrastructure) ---
  STUB_PORT="$(python3 - <<'PY'
import socket
s = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
s.bind(("127.0.0.1", 0))
print(s.getsockname()[1])
s.close()
PY
)"
  cat > "$TMP/stub_server.py" <<PYEOF
import http.server, json
class H(http.server.BaseHTTPRequestHandler):
    def do_GET(self):
        self.send_response(200)
        self.send_header("Content-Type", "application/json")
        self.end_headers()
        # Deliberately serves a DIFFERENT model than the one the fixture
        # installer below declares for helixagent.
        self.wfile.write(json.dumps({"object": "list", "data": [{"id": "some-other-model"}]}).encode())
    def log_message(self, *a): pass
http.server.HTTPServer(("127.0.0.1", $STUB_PORT), H).serve_forever()
PYEOF
  python3 "$TMP/stub_server.py" &
  STUB_PID=$!
  sleep 0.3

  cat > "$TMP/install_agent_configs_b.sh" <<EOF
#!/usr/bin/env bash
echo "synthetic wired installer — declares a model id its own provider does not actually serve"
HELIXAGENT_MODEL_ID="helixagent-llm"
exit 0
EOF
  chmod +x "$TMP/install_agent_configs_b.sh"
  cat > "$TMP/setup_b.sh" <<'EOF'
#!/usr/bin/env bash
./scripts/install_agent_configs_b.sh
EOF
  OUT_B="$(RED_MODE=0 INSTALLER_OVERRIDE="$TMP/install_agent_configs_b.sh" \
           SETUP_SH_OVERRIDE="$TMP/setup_b.sh" \
           AGENT_URL_OVERRIDE="helixagent=http://127.0.0.1:$STUB_PORT/v1" \
           "$0" 2>&1)"
  RC_B=$?
  kill "$STUB_PID" >/dev/null 2>&1; STUB_PID=""
  if [[ $RC_B -eq 0 ]]; then
    echo "$GATE: RED-B FAIL — the gate PASSed on a synthetic installer declaring 'helixagent-llm' against a stub that does not serve it. It is a blind test." >&2
    printf '%s\n' "$OUT_B" | sed 's/^/    /' >&2
    exit 1
  fi
  if ! grep -qi "helixagent-llm.*not present\|not present in.*helixagent-llm" <<<"$OUT_B"; then
    echo "$GATE: RED-B FAIL — the gate FAILed on the synthetic stale-model-id installer, but not for that reason. Output:" >&2
    printf '%s\n' "$OUT_B" | sed 's/^/    /' >&2
    exit 1
  fi
  echo "$GATE: RED-B OK — gate FAILs on a declared model id absent from its provider's live listing, citing:"
  grep -i "helixagent-llm" <<<"$OUT_B" | sed 's/^/    /'

  echo "$GATE: RED OK — all synthetic pre-fix defects are caught (A: unwired; A2: mentioned-but-not-invoked, with A3 as its positive control; A4: mention inside a backslash- or variable-delimited heredoc body, the two forms the stripper failed OPEN on; A5: a quoted ARGUMENT, an ARRAY ELEMENT or a string-literal body -- continued printf, continued UNQUOTED echo (the form that makes the continuation join falsifiable), array literal, multi-line BANNER=\", and a heredoc banner followed by an array -- each across five path prefixes, each ASSERTING which mechanism refuses it, with A6 as its positive control over five real invocation forms; A7: a BASH EXECUTION ORACLE -- every fixture is run under bash with an instrumented installer stub, so bash and not a declared mechanism decides mention-vs-call, and the gate verdict is asserted against it (no fail-open, no undeclared false refusal); B: stale model id)."
  exit 0
fi

# =============================================================================
# THE REAL CHECKS
# =============================================================================

# --- (a) exists + executable -------------------------------------------------
if [[ ! -f "$INSTALLER" ]]; then
  echo "$GATE: FAIL — installer script missing: $INSTALLER (a correctly-designed installer that does not exist is exactly as useless to an operator as one nothing calls)" >&2
  exit 1
fi
if [[ ! -x "$INSTALLER" ]]; then
  echo "$GATE: FAIL — installer exists but is not executable: $INSTALLER" >&2
  exit 1
fi
echo "  PASS: $INSTALLER exists and is executable"

# --- (b) syntactically valid -------------------------------------------------
SYNTAX_ERR="$(bash -n "$INSTALLER" 2>&1)"
if [[ $? -ne 0 ]]; then
  echo "$GATE: FAIL — installer has a bash syntax error:" >&2
  printf '%s\n' "$SYNTAX_ERR" | sed 's/^/    /' >&2
  exit 1
fi
echo "  PASS: bash -n clean"

# --- (c) actually wired into setup.sh ----------------------------------------
if [[ ! -f "$SETUP_SH" ]]; then
  echo "$GATE: FAIL — cannot check wiring, setup script missing: $SETUP_SH" >&2
  exit 1
fi
INSTALLER_BASENAME="$(basename -- "$INSTALLER")"
ESCAPED_BASENAME="$(printf '%s' "$INSTALLER_BASENAME" | sed 's/[.[\*^$]/\\&/g')"

# Reduce setup.sh to the lines that are CODE, then look for a command-position
# invocation in them. Four classes of line are NOT code and are dropped:
#   * heredoc BODIES -- text emitted by `cat <<EOF ... EOF` is output, and
#     setup.sh documents this very installer inside its closing banner heredoc;
#   * comment lines;
#   * CONTINUATION lines -- a line after one ending in `\` is the tail of the
#     previous command, so it is JOINED to it rather than treated as a fresh
#     command position. Joining (not dropping) is what keeps `bash \` + newline
#     + `./scripts/x.sh` a real invocation while making `printf '%s\n' \` +
#     newline + `"./scripts/x.sh"` an argument, which is what it is;
#   * lines INSIDE an unclosed quote -- the body of `BANNER="` ... `"` is a
#     string literal, not code, exactly like a heredoc body.
# The last two were fail-OPEN: measured on the pre-fix gate, a continued
# `printf`, an array literal `HELP=( "./scripts/..." )` and a multi-line
# `BANNER="..."` each satisfied the invocation check with ZERO real call, so
# deleting the real call at setup.sh:234 would have left the gate GREEN.
#
# awk, not python3, deliberately: checks (a)-(c) must stay decidable on a host
# where python3 is absent (that only downgrades check (d) to SKIP).
#
# A quote state machine runs the whole file, which also buys two §11.4.201
# false-refusal fixes: a `<<WORD` inside a string (`echo "cat <<EOF"`) or after
# a `#` comment marker no longer opens a phantom heredoc that swallows the rest
# of the file. Comments are now recognised INSIDE awk rather than stripped by a
# prior grep, so the NR in the fail-closed diagnostic is the real line number.
#
# inhd == 1  -> a parsed heredoc; ends at its terminator line (compared
#               LITERALLY, so a delimiter containing regex metacharacters
#               still terminates).
# inhd == 2  -> FAIL CLOSED. The opener is a real heredoc but its delimiter is
#               not a literal word (`<<$D`, `<<${D}`), so we cannot know where
#               the body ends. Treating the body as CODE is the fail-OPEN that
#               let a mention-only banner satisfy the invocation check, so the
#               body is swallowed to end-of-file instead and a diagnostic is
#               emitted -- an illegible FAIL beats a silent PASS.
SETUP_CODE="$(awk '
  # STRIPPER-SELF-EXTRACT-SENTINEL-HEAD-a41c (see RED-A5 harness): asserted by
  # the self-extraction guard so a TRUNCATED extraction is reported as an
  # extraction failure instead of being misattributed to a fixture.
  function ltrim(s) { sub(/^[ \t]+/, "", s); return s }
  function trim(s)  { sub(/^[ \t]+/, "", s); sub(/[ \t]+$/, "", s); return s }

  # emit(): one LOGICAL line (continuations already joined).
  function emit(line,   startst, startarr, i, c, rest, t, tl, arith, hdscan, k) {
    if (inhd) {
      # bash closes a heredoc on a line that equals the delimiter EXACTLY.
      # trim() also stripped LEADING SPACES and TRAILING WHITESPACE, so awk
      # closed the body on `  BANNER` and on `BANNER ` where bash does not --
      # and every line after that was then read as CODE. Measured on the
      # pre-fix gate: a banner containing `  BANNER` followed by
      # `  ./scripts/x.sh --dry-run` printed `PASS: ... is invoked` with no
      # call anywhere. Only `<<-` strips leading TABS (never spaces), and it
      # never strips trailing whitespace, which is all hddash does here.
      tl = line
      # ROUND-7 B2: the pending heredocs are a QUEUE, closed IN ORDER, because
      # bash accepts more than one opener on a single logical line.
      if (hddashq[hdq_i]) { sub(/^\t+/, "", tl) }
      if (inhd == 1 && tl == hdterm[hdq_i]) {
        hdq_i++
        if (hdq_i > hdq_n) { inhd = 0; hdq_n = 0; hdq_i = 0 }
      }
      return
    }
    startst = st
    startarr = arr
    arith = 0
    hdscan = 0
    i = 1
    while (i <= length(line)) {
      c = substr(line, i, 1)
      if (st == 0) {
        if (c == bs) { i += 2; continue }
        if (c == dq) { st = 1; i++; continue }
        # ROUND-8 B1 (fail-open #16). DOLLAR-SINGLE-QUOTE is ANSI-C quoting,
        # where a BACKSLASH IS AN ESCAPE -- unlike a plain single-quoted
        # string, where a backslash is LITERAL. The scanner had ONE
        # single-quote state and applied the plain rule to both, so a
        # backslash-quote pair was read as CLOSING the string and every line
        # after it was emitted as CODE. Measured with the bash execution
        # oracle: an ANSI-C assignment whose text ends in backslash-quote,
        # followed by a command-position mention and the real closing quote,
        # printed PASS-is-invoked, rc=0, while bash executed the installer
        # ZERO times -- one assignment, no call. The MIRROR defect is a false
        # refusal (11.4.201): an inline ANSI-C value containing
        # backslash-quote left the scanner inside a phantom string, so a REAL
        # call below it was dropped SILENTLY, with no diagnostic -- the same
        # fail-closed-but-MUTE class the arithmetic and here-string fixes
        # above already treated. Testing for the dollar here is safe because
        # an escaped dollar was already consumed by the bs branch above.
        # DOLLAR-DOUBLE-QUOTE needs nothing: the dollar falls through and the
        # double quote lands in st==1, which is already correct.
        if (c == "$" && substr(line, i + 1, 1) == q) { st = 3; i += 2; continue }
        if (c == q)  { st = 2; i++; continue }
        # a `#` at line start or after whitespace begins a comment: the rest of
        # the line is neither code nor quote-state input.
        if (c == "#" && (i == 1 || substr(line, i - 1, 1) ~ /[ \t]/)) { break }
        # ARITHMETIC DEPTH. The `<<` in `$((1<<2))` / `((a<<b))` is a SHIFT,
        # not a heredoc opener. The pre-fix scanner opened a phantom heredoc
        # with delimiter `2))` and silently swallowed the REST OF THE FILE, so
        # a real call below it was reported "not invoked" with no diagnostic
        # at all -- fail-CLOSED but misleading, which is the §11.4.201 defect
        # in its own right. Depth is tracked rather than testing "does this
        # line contain `((` anywhere", so a genuine heredoc opened AFTER the
        # arithmetic closes on the same line is still seen.
        if (c == "(" && substr(line, i + 1, 1) == "(") { arith++; i += 2; continue }
        if (c == ")" && substr(line, i + 1, 1) == ")" && arith > 0) { arith--; i += 2; continue }
        # ARRAY DEPTH. `NAME=(` opens an ARRAY LITERAL, and every line that
        # BEGINS inside one is an ELEMENT LIST, never a command position:
        # `HELP=(` newline `  ./scripts/x.sh --dry-run` newline `)` runs
        # nothing. Measured on the pre-fix gate, SEVEN array shapes (quoted
        # and unquoted, single-line and multi-line, `./`/`$ROOT`/`${ROOT}`/
        # `$PWD`-rooted) each printed `PASS: ... is invoked` with zero real
        # call. `$( ... )` is deliberately NOT counted here: a command
        # substitution DOES run its contents, so `OUT=$( ./scripts/x.sh )` is
        # a real call and must still be emitted.
        if (c == "(" && i > 1 && substr(line, i - 1, 1) == "=" && arith == 0) { arr++; i++; continue }
        if (c == "(" && arr > 0) { arr++; i++; continue }
        if (c == ")" && arr > 0) { arr--; i++; continue }
        # ROUND-7 B4. `<<<` is a HERE-STRING, not a heredoc. The pre-fix
        # guard excluded it only at the FIRST `<`: the scan then advanced one
        # character, re-tested at the SECOND `<`, saw `<` + `<` + <not-`<`>,
        # and opened a PHANTOM heredoc whose delimiter was the word the
        # here-string supplies -- swallowing the rest of the file, with NO
        # diagnostic at all. Measured: `read -r VAR <<< "hello"` followed by a
        # real call refused that call SILENTLY. Fail-closed but MUTE is the
        # same §11.4.201 defect the arithmetic fix above already treated, so
        # the whole three-character operator is skipped.
        if (c == "<" && substr(line, i + 1, 1) == "<" && substr(line, i + 2, 1) == "<") { i += 3; continue }
        # ROUND-7 B2. EVERY `<<` on the logical line is registered, not just
        # the first. bash accepts several openers per line and reads their
        # bodies IN ORDER (`cat <<A <<B`, `cat <<A; cat <<B`); the pre-fix
        # `hdpos == 0` guard registered only A, so once A terminated, body B
        # was read as CODE. Measured with the bash execution oracle: both
        # spellings printed `PASS: ... is invoked`, rc=0, while bash executed
        # the installer ZERO times.
        if (c == "<" && substr(line, i + 1, 1) == "<" && arith == 0 && arr == 0) { hdp[++hdscan] = i; i += 2; continue }
        i++; continue
      }
      if (st == 1) {
        if (c == bs) { i += 2; continue }
        if (c == dq) { st = 0; i++; continue }
        i++; continue
      }
      # st == 3 -> inside ANSI-C quoting: a backslash ESCAPES the next
      # character, so a backslash-quote pair does NOT close the string
      # (ROUND-8 B1, above). st == 2 (plain single quote) keeps the literal
      # -backslash rule below, which is what bash does there.
      if (st == 3) {
        if (c == bs) { i += 2; continue }
        if (c == q)  { st = 0; i++; continue }
        i++; continue
      }
      if (c == q) { st = 0 }
      i++
    }
    # HEREDOC REGISTRATION RUNS BEFORE THE EARLY RETURNS BELOW, DELIBERATELY.
    # A `<<` scanned at st==0 on a line that BEGINS inside a string body or an
    # array literal still OPENS A REAL HEREDOC in bash:
    #   `HELP=(` / `  "text"` / `) ; cat <<BANNER` -- the opener sits on the
    #   array-CLOSING line, which begins with startarr>0.
    # When registration sat AFTER `if (startst != 0) return` / `if (startarr >
    # 0) return`, hdpos was computed and then thrown away: inhd was never set,
    # so every banner line below was emitted as CODE and satisfied the
    # command-position regex. Measured on the pre-fix gate with a bash
    # EXECUTION ORACLE (an instrumented installer stub that appends a marker
    # when it really runs): three shapes -- array-close + `cat <<BANNER`,
    # string-close + `cat <<BANNER`, and array-close + `cat <<-BANNER` -- each
    # printed `PASS: ... is invoked`, rc=0, while bash executed the installer
    # ZERO times. Deleting the real call at setup.sh:234 would have left the
    # gate GREEN with no invocation anywhere.
    # The `arr == 0` half of the detector guard above is what makes this
    # reordering safe: without it, a `<<` INSIDE an array element would now
    # register a phantom heredoc and swallow a real call below it. (Honest
    # scope, §11.4.6: bash REJECTS `FLAGS=( a<<b )` outright -- measured, it is
    # a syntax error -- so the guard protects the scanner against MALFORMED
    # input rather than a reachable valid-bash false refusal. It costs nothing
    # and keeps the scanner from desyncing on a file bash would not run.)
    if (hdscan > 0) {
      hdq_n = 0
      for (k = 1; k <= hdscan; k++) {
      rest = substr(line, hdp[k] + 2)
      hddashq[k] = (rest ~ /^-/)
      sub(/^-/, "", rest)
      sub(/^[ \t]+/, "", rest)
      if (rest ~ /^\$/ && line !~ /\$\(\(/) {
        inhd = 2
        printf "%s: cannot parse heredoc delimiter at line %d (%s); failing closed and treating the rest of the file as heredoc body\n", "CM-AGENT-CONFIG-FANOUT", NR, line > "/dev/stderr"
        break
      } else {
        gsub(qcls, "", rest)
        # `cat <<\EOF` -- the conventional way to disable expansion in a help
        # banner. The backslash is not part of the delimiter; stripping it is
        # what makes the body get treated as a body and not as code.
        sub(/^\\/, "", rest)
        # The delimiter class admits every character a word delimiter may
        # carry: `<<END-OF-HELP` never matched the old [A-Za-z_][A-Za-z0-9_]*
        # class, so its body was read as code.
        if (match(rest, /^[^ \t;|&<>()]+/)) {
          hdterm[k] = substr(rest, 1, RLENGTH)
          hdq_n = k
        } else {
          # THE DELIMITER DID NOT PARSE, so we do not know where the body ends.
          # Falling through to the print below (the pre-fix behaviour) treated
          # the BODY as CODE -- the same fail-OPEN the dollar-delimiter branch
          # above already refuses. Measured with a bash EXECUTION ORACLE: a
          # heredoc whose delimiter is a single close-paren, quoted, followed
          # by a command-position mention of the installer and its terminator
          # line, printed PASS-is-invoked, rc=0, while bash executed the
          # installer ZERO times -- bash takes the paren as a literal
          # delimiter, the class here cannot match it, and the banner became
          # code. The paren is deliberately still EXCLUDED from the class
          # rather than admitted: NAME=$(cat <<EOF) is a real idiom whose
          # delimiter is EOF, and admitting the paren would make termlit
          # EOF-plus-paren -- a body that never terminates. So the unparseable
          # case fails CLOSED instead.
          inhd = 2
          printf "%s: cannot parse heredoc delimiter at line %d (%s); failing closed and treating the rest of the file as heredoc body\n", "CM-AGENT-CONFIG-FANOUT", NR, line > "/dev/stderr"
          break
        }
      }
      }
      # An UNPARSEABLE opener anywhere in the queue already set inhd=2 (fail
      # closed, with its diagnostic); only a fully parsed queue becomes a
      # normal body.
      if (inhd != 2 && hdq_n > 0) { inhd = 1; hdq_i = 1 }
    }

    # began inside a quoted string -> string body, not a command position.
    if (startst != 0) { return }
    # began inside an unclosed array literal -> element list, not a command.
    if (startarr > 0) { return }
    # a line that is nothing but a comment is not code either.
    if (ltrim(line) ~ /^#/) { return }

    print line
  }

  BEGIN { q = sprintf("%c", 39); dq = sprintf("%c", 34); bs = sprintf("%c", 92)
          qcls = "[" q dq "]"; st = 0; inhd = 0; arr = 0; hdq_n = 0; hdq_i = 0; buf = "" }
  {
    buf = buf $0
    nb = 0
    while (nb < length(buf) && substr(buf, length(buf) - nb, 1) == bs) { nb++ }
    if (nb % 2 == 1) { buf = substr(buf, 1, length(buf) - 1); next }
    emit(buf); buf = ""
  }
  END { if (buf != "") { emit(buf) }
        # An unbalanced `NAME=(` swallows the rest of the file the same way an
        # unparseable heredoc delimiter does. That is fail-CLOSED, but a
        # silent refusal is exactly the §11.4.201 defect the arithmetic fix
        # above removes, so say it out loud rather than refuse mutely.
        if (arr > 0) {
          printf "%s: an array literal opened with NAME=( is still unclosed at end of file; every line from there on was read as an element list, not as code. A real invocation inside that region will be refused (fail-closed, but say so).\n", "CM-AGENT-CONFIG-FANOUT" > "/dev/stderr"
        } }
  # STRIPPER-SELF-EXTRACT-SENTINEL-TAIL-a41c
' "$SETUP_SH")"

# COMMAND POSITION: line start, optional indentation, optionally preceded by
# one or more shell keywords/operators/env-assignments that can precede a
# command. `bash ./scripts/x.sh`, `"$ROOT/scripts/x.sh"`, `if ! ./scripts/x`,
# `VAR=1 ./scripts/x` and `( ./scripts/x )` are REAL invocations; rejecting
# them is a false refusal (§11.4.201), not a stricter gate.
#
# THE DISCRIMINATOR IS WHERE THE QUOTE CLOSES, not whether one is present.
# Round 3 allowed a bare optional leading quote, which let a quoted ARGUMENT
# satisfy "command position". Round 4 narrowed that prefix to `"$` -- which
# closed the `"./scripts/x.sh --dry-run"` shape but NOT its variable-rooted
# twin: `HELP=( "$ROOT/scripts/x.sh --dry-run" )` still matched, because the
# terminator class admitted a SPACE after the basename, so the quote was
# allowed to close after the ARGUMENTS instead of after the path. Measured on
# the round-4 gate: `PASS: ... is invoked`, rc=0, with no call anywhere.
# (Removing the env-assignment alternative does NOT close this -- measured:
# the array-element line matches without it, and dropping it breaks the
# RED-A6 `VAR=1 ./scripts/x.sh` positive control.)
#
# So the path is matched by two alternatives, and a quoted one MUST be closed
# by its own quote immediately after the basename:
#   (1) UNQUOTED  ([A-Za-z0-9_.{}$-]+/)*scripts/BN  terminated by
#       whitespace / `;` / `&` / `|` / end-of-line   -- `"` is NOT a terminator
#       here, which is what stops an argument from closing its quote later.
#   (2) QUOTED    "$([A-Za-z0-9_.{}$-]+/)*scripts/BN"   -- the closing quote is
#       REQUIRED and must sit directly after the basename, so
#       `"$ROOT/scripts/x.sh" --dry-run` (a real call) matches while
#       `"$ROOT/scripts/x.sh --dry-run"` (an argument) cannot.
# `"${ROOT}/...` and `"$PWD/...` are covered by (2) via the {} and $ in the
# path class.
#
# COMMAND-SUBSTITUTION CAPTURE. `OUT=$(./scripts/x.sh ...)` and
# `OUT="$(./scripts/x.sh ...)"` ARE real calls -- a command substitution runs
# what is inside it -- and were false refusals through round 4. They are
# admitted by ONE optional leading group, `NAME="?$(` + optional space, and
# nothing else: a MENTION inside a capture (`X=$(echo ./scripts/x.sh)`,
# `X=$(basename ./scripts/x.sh)`, `X=$(cat ./scripts/x.sh)`) still cannot
# match, because after `$(` the next token must be an interpreter or the path
# itself, and `echo `/`basename `/`cat ` is neither.
#
# WRAPPER COMMANDS. `env`, `nohup`, `nice [-n N]` and `timeout <N>[smhd]`
# EXECUTE their argument, so a line beginning with one is a real invocation
# and refusing it is a false refusal. They cannot create a fail-open: a
# mention line must begin with a command that takes the path as DATA
# (`echo`/`printf`/`cat`/`grep`/`ls`/`[`), and no such command is on this
# list -- deliberately, because that is the whole discriminator.
#
# CASE-BRANCH AND TEST-GUARD PREFIXES (added round 7, both MEASURED).
# `--agents) ./scripts/x.sh ;;` and `[ -x ... ] && ./scripts/x.sh` are REAL
# invocations that rounds 1-6 refused. The real setup.sh already uses `case`
# in three places, so moving the call into a `--agents) ...;;` branch would
# have FAILED a correctly-wired setup -- a false refusal on the most likely
# next edit. Two narrow alternatives admit them:
#   * a BRACKET TEST followed by `&&`, bounded: the class cannot cross a `]`,
#     so this is NOT the `.*&&` that was measured unsafe (with `.*&&`,
#     `echo "a && ./scripts/x.sh --dry-run"` MATCHES -- a fail-OPEN);
#   * ONE unquoted word ending in `)`, i.e. a case pattern. It cannot start
#     after `echo `/`printf `/`cat `, because those are not prefix tokens and
#     the class cannot span the space that follows them.
# Measured over a 20-fixture corpus with a BASH EXECUTION ORACLE (an
# instrumented installer stub that appends a marker when it really runs, so
# bash -- not the author -- decides mention-vs-call): 12 mention shapes
# (`echo "a) ..."`, `echo "a && ..."`, `MSG="foo) ..."`, `printf ... "*) ..."`,
# a case branch whose body is `echo`, one whose body is `printf`,
# `[ -n x ] && echo ...`, `X=$(basename ...)`, `grep -q ... file`,
# `echo "a|b) ..."`, a heredoc banner, an array element) ALL still refused,
# and 8 real-call shapes (case, alternation-pattern case, bracket-test `&&`,
# plain, `bash `, `if !`, `VAR=1 `, quoted-var-path) ALL accepted:
# 20/20 agreement with bash.
#
# FAIL-OPEN LIMITS (round 7, ALL MEASURED with the bash execution oracle).
# The heading below used to read "ALL fail-CLOSED -- they refuse a real call,
# they never pass a mention". That claim was FALSE, and round 7 falsified it
# twice over: a backtick inside an env-assignment prefix and a second heredoc
# on one logical line each produced gate PASS, rc=0, with bash executing the
# installer ZERO times. Both are now fixed and are standing A7 fixtures --
# but the claim itself was the defect, so what remains is stated as a
# FAIL-OPEN list rather than folded back into a fail-closed one.
#
# What remains fail-OPEN is STRUCTURAL, not lexical: the gate decides whether
# a line is a COMMAND POSITION, and it does not evaluate REACHABILITY. A real
# invocation that is present in command position but never reached still
# satisfies the check. Measured, each gate=PASS with bash executing zero
# times:
#   * a call inside a function that nothing invokes (`helper() { ./scripts/x.sh; }`);
#   * a call under a guard that is never true (`if false; then ./scripts/x.sh; fi`,
#     `while false; do ./scripts/x.sh; done`, `[ -n "" ] && ./scripts/x.sh`);
#   * a call after an unconditional `exit`.
# These are NOT closable by this gate's mechanism: distinguishing them needs
# reachability analysis, not a command-position scanner, and a scanner that
# tried would start refusing real calls behind ordinary runtime guards. They
# are recorded here so the next reader does not mistake the check for
# something stronger than it is. The check answers "is it called in the setup
# path", not "is it always called".
#
# HONEST LIMITS -- FAIL-CLOSED (§11.4.201, these refuse a REAL call; none of
# them passes a mention). This list is NOT claimed to be exhaustive, and that
# word is deliberately absent: rounds 1-6 each asserted a completeness
# property over this file and each was falsified by the next round -- most
# recently by two shapes ABSENT from the list rather than wrong in it (a
# case-branch invocation, and a heredoc opened on a line that begins inside a
# string body or an array literal). What follows is what has been MEASURED;
# nothing is implied about what has not:
#   * a quoted path that is NOT variable-rooted: `"scripts/x.sh"`,
#     `"./scripts/x.sh"`  (unchanged from round 4);
#   * a no-argument call whose path is closed by `)` rather than whitespace:
#     `OUT=$(./scripts/x.sh)`, `(./scripts/x.sh)`. `)` is deliberately NOT a
#     terminator: admitting it re-widens the class that produced the round-4
#     quoted-argument fail-open, and the with-arguments spelling (which ends
#     in whitespace) is the one that occurs;
#   * a call after a `;` on the same line: `if true; then ./scripts/x; fi`,
#     `echo hi; ./scripts/x`. Measured: allowing `.*;` as a prefix lets
#     `echo "a; ./scripts/x.sh --dry-run"` match, so `;` stays out;
#   * `sh -c "./scripts/x.sh"` -- the interpreter alternative takes no options;
#   * a call inside a COMMAND SUBSTITUTION INSIDE AN ARRAY LITERAL:
#     `HELP=( $(./scripts/x.sh) )`. The substitution DOES run (bash oracle:
#     executed), but the array-literal drop in the stripper is unconditional,
#     so the line never reaches the regex. Fail-closed, and asserted as a
#     DECLARED limit by RED-A7, which FAILS if it ever starts passing -- so
#     this entry cannot go stale silently;
#   * a heredoc whose delimiter the delimiter class cannot parse -- `<<$D`,
#     `<<${D}`, and (round 7) a quoted delimiter containing a parenthesis.
#     These swallow the rest of the file WITH a diagnostic. Before round 7 the
#     parenthesis case fell through to "treat the body as code", which the
#     bash oracle showed was not a limit at all but a FAIL-OPEN: gate PASS,
#     rc=0, bash executions ZERO;
#   * (round 7, all MEASURED with the bash execution oracle -- bash really
#     executes each of these and the gate refuses it) EIGHT further real-call
#     spellings the prefix classes do not admit:
#       `[[ -x ... ]] && ./scripts/x.sh`   -- only the single-bracket test is a
#                                             prefix; `[[` is not, because the
#                                             `\[[^]]*\]` class stops at the
#                                             first `]`;
#       `eval ./scripts/x.sh`              -- `eval` is deliberately not a
#                                             wrapper alternative: it takes its
#                                             argument as DATA, so admitting it
#                                             would admit `eval "echo ./scripts/x.sh"`;
#       `coproc ./scripts/x.sh`            -- (round 8) bash DOES execute this,
#                                             the gate refuses it, and until now
#                                             it was not on this list at all: an
#                                             UNDECLARED false refusal, which is
#                                             the one thing this section exists
#                                             to prevent. Unlike `eval` the
#                                             omission is not deliberate -- it is
#                                             simply not in the wrapper
#                                             alternation. Fail-CLOSED, so it
#                                             cannot pass a mention;
#       `command ./scripts/x.sh`, `time ./scripts/x.sh`;
#       a case pattern with a space before `)` (`--agents )`) or a QUOTED
#         pattern (`"--agents")`) -- the pattern class is one unquoted word
#         ending in `)`;
#       a `#` comment whose last character is a backslash, followed by the
#         call -- the continuation join swallows the call into the comment;
#       a heredoc BODY line whose last character is a backslash, followed by
#         the terminator and then the call -- same join, one layer down.
#     None of the eight can pass a mention: each fails by NOT MATCHING, which
#     is the safe direction.
# RED-A5 covers both path alternatives; RED-A6 is its positive control; RED-A7
# asserts every shape above against what bash actually executes.
#
# The env-assignment alternative excludes values that OPEN A QUOTE and
# values that ESCAPE THE FOLLOWING CHARACTER, so `VAR=1 ./scripts/x` is a
# call while `MSG="see ./scripts/x.sh"` is not.
#
# ROUND-8 B2 (fail-open #17) -- and it was round 7's own fix, one character
# wide. The class excluded " ' backtick whitespace and `(` but NOT the
# BACKSLASH, so a backslash-escaped space kept the VALUE going in bash while
# the regex read the literal space as the required separator and the path as
# command position. Measured with the bash execution oracle:
#     MSG=x\ ./scripts/install_agent_configs.sh
# is ONE assignment with ZERO executions (bash takes the value to be the
# literal `x ./scripts/install_agent_configs.sh`), yet the gate reported it
# invoked; it fires behind a case pattern too.
# NOTE FOR ANY FUTURE EDIT OF THIS CLASS: a backslash inside an ERE bracket
# expression is LITERAL, and the shell's own double-quote processing runs
# FIRST -- re-expand the final ERE exactly as the shell passes it and check
# it byte by byte. Round 7's first attempt at this same line escaped the
# backtick as backslash-backtick, which GNU grep reads as BUFFER-START, and
# it silently never matched. MEASURED THIS ROUND, same trap, other
# direction: a SINGLE backslash written before the quote inside the
# bracket is EATEN by GNU grep as an escape, so the backslash never
# enters the set at all and the fixture kept passing. The class carries
# a DOUBLED backslash for that reason -- GNU reads it as one literal
# backslash, a strict-POSIX engine reads it as two literal backslashes,
# and a set is idempotent, so BOTH readings contain the backslash.
#
# A MENTION is still rejected: `echo "... ./scripts/x.sh"` cannot match,
# because `echo ` satisfies neither the keyword list, the interpreter list,
# nor the path-prefix class (which must end in `/`).
INVOKE_RE="^[[:space:]]*([A-Za-z_][A-Za-z0-9_]*=\"?(\\\$\\(|\`)[[:space:]]*)?((if|then|else|elif|do|!|\{|\(|&&|\|\||env|nohup|nice([[:space:]]+-n[[:space:]]+-?[0-9]+)?|timeout[[:space:]]+[0-9]+[smhd]?|\[[^]]*\][[:space:]]*&&|[^[:space:]\"'();&\$\{\}]+\)|[A-Za-z_][A-Za-z0-9_]*=[^\\\\\"'\`[:space:](]*)[[:space:]]+)*((bash|sh|source|exec|\.)[[:space:]]+)?((([A-Za-z0-9_.{}\$-]+/)*scripts/${ESCAPED_BASENAME}([[:space:]]|;|\&|\||$))|(\"\\\$([A-Za-z0-9_.{}\$-]+/)*scripts/${ESCAPED_BASENAME}\"))"

# BRACKET-CLASS NOTE (round 8, correction). In a POSIX ERE a backslash inside
# a bracket expression is LITERAL, so [^[:space:]"'();&$\{\}] does not contain
# two ESCAPES -- under a strict reading it excludes the BACKSLASH as well as
# the braces, and GNU grep additionally reads \{ as {. The set is the same
# either way, which is why it works, but the spelling should not be read as
# escaping. See the assignment-value class above for the case where the
# difference between the two readings is load-bearing.
#
# LC_ALL=C (§11.4.50): the bracket RANGES above ([A-Za-z], [A-Za-z0-9_.{}$-])
# are locale-dependent. Measured on this host, a UTF-8 collation makes
# [A-Za-z] match `e-acute`, so the same file could match under one locale and
# not another. Every range here only WIDENS a PREFIX class (the literal
# `scripts/<basename>` is still required), so the drift is safe in direction --
# but "safe in direction" is not "deterministic", and a gate whose verdict
# depends on the caller's locale is not reproducible. Pinned, not assumed.
if ! printf '%s\n' "$SETUP_CODE" | LC_ALL=C grep -qE -- "$INVOKE_RE"; then
  MENTIONS="$(grep -cE -- "scripts/${ESCAPED_BASENAME}" "$SETUP_SH" 2>/dev/null || true)"
  echo "$GATE: FAIL — $INSTALLER_BASENAME is not invoked (nothing invokes it) from $SETUP_SH: no line outside a heredoc body puts scripts/$INSTALLER_BASENAME in command position. The file mentions it on ${MENTIONS:-0} line(s), but a MENTION (help text, an echo, a heredoc banner) is not a call — an installer nothing calls is not referenced by the setup path and is the exact defect this gate exists to catch." >&2
  exit 1
fi
echo "  PASS: $INSTALLER_BASENAME is invoked (in command position, outside any heredoc body) from $SETUP_SH"

# --- (d) live model-id agreement (§11.4.111) ---------------------------------
command -v python3 >/dev/null 2>&1 || {
  echo "$GATE: SKIP — python3 not on PATH; cannot run the live model-id check (checks (a)-(c) above already passed)" >&2
  exit 2
}

VERDICT="$(INSTALLER="$INSTALLER" CA_CERT="$CA_CERT" AGENT_URL_OVERRIDE="${AGENT_URL_OVERRIDE:-}" python3 - <<'PY' 2>&1
import os, re, sys, json, ssl, urllib.request, urllib.error

INSTALLER = os.environ["INSTALLER"]
CA = os.environ.get("CA_CERT", "")
OVERRIDE = os.environ.get("AGENT_URL_OVERRIDE", "")

# The closed, contract-given table (see this gate's header). Overridable
# PER PROVIDER only for this gate's own RED_MODE self-test (an ephemeral
# local stub server) — never part of install_agent_configs.sh's own CLI.
PROVIDERS = {
    "helixllm-coder":   ["http://127.0.0.1:18434/v1", ["qwen2.5-coder-3b-instruct-q4_k_m"]],
    "helixllm-gateway": ["https://127.0.0.1:8443/v1", ["helixllm-anton-qwen2-5-coder-3b-instruct-q4_k_m-f6771589d190"]],
    "helixagent":       ["http://127.0.0.1:7061/v1", ["helixagent-llm", "helixagent-debate", "helixagent-ensemble",
                                                        "helix-llm", "helix-debate"]],
}
if OVERRIDE and "=" in OVERRIDE:
    name, url = OVERRIDE.split("=", 1)
    if name in PROVIDERS:
        PROVIDERS[name][0] = url

try:
    src = open(INSTALLER, encoding="utf-8", errors="replace").read()
except OSError as e:
    print("SKIP|cannot read installer source: %s" % e); sys.exit(4)

def declared(src, model_id):
    # Boundary check, not bare substring (see gate header). Measured live
    # against the real installer: model ids are embedded as one field of a
    # larger pipe-delimited record string (e.g.
    # '...|32768|4096|qwen2.5-coder-3b-instruct-q4_k_m'), so requiring the
    # id be wrapped in its OWN matching quote pair (the original design)
    # under-reported real, hardcoded ids. What actually distinguishes a real
    # occurrence from a longer sibling string like
    # "helixagent-llm-legacy-2019" is not quoting but BOUNDARIES: the
    # character immediately before/after the match must not be a valid
    # model-id-continuation character ([A-Za-z0-9._-]) — a pipe, quote,
    # space, comma, or start/end of file all qualify; a hyphen does not.
    pattern = r'(?<![A-Za-z0-9._-])' + re.escape(model_id) + r'(?![A-Za-z0-9._-])'
    return re.search(pattern, src) is not None

CTX = ssl.create_default_context(cafile=CA) if CA and os.path.isfile(CA) else None
if CTX is None:
    CTX = ssl.create_default_context()
    CTX.check_hostname = False
    CTX.verify_mode = ssl.CERT_NONE
OPENER = urllib.request.build_opener(
    urllib.request.ProxyHandler({}),
    urllib.request.HTTPSHandler(context=CTX))

def live_models(url):
    try:
        with OPENER.open(url + "/models", timeout=5) as resp:
            body = resp.read(65536).decode("utf-8", "replace")
    except Exception as e:
        return None, "%s: %s" % (type(e).__name__, e)
    try:
        doc = json.loads(body)
    except Exception as e:
        return None, "non-JSON response (%s)" % e
    if not (isinstance(doc, dict) and doc.get("object") == "list" and isinstance(doc.get("data"), list)):
        return None, "not an OpenAI model list"
    return {m.get("id") for m in doc["data"] if isinstance(m, dict)}, None

green, skips, findings = [], [], []
for provider, (url, model_ids) in sorted(PROVIDERS.items()):
    decl = [m for m in model_ids if declared(src, m)]
    if not decl:
        continue
    live, err = live_models(url)
    if live is None:
        skips.append("%s: endpoint %s unreachable (%s); cannot verify %d declared model id(s)"
                     % (provider, url, err, len(decl)))
        continue
    for m in decl:
        if m in live:
            green.append("%s: '%s' declared and confirmed live at %s" % (provider, m, url))
        else:
            findings.append("%s: model id '%s' is declared in the installer but is NOT present in %s "
                            "(live ids: %s)" % (provider, m, url, ", ".join(sorted(live)) or "<none>"))

for g in green:
    print("GREEN|%s" % g)
for s in skips:
    print("SKIPPED|%s" % s)
for f in findings:
    print("FINDING|%s" % f)
print("TALLY|green=%d skipped=%d findings=%d" % (len(green), len(skips), len(findings)))
sys.exit(3 if findings else 0)
PY
)"
ANALYZER_RC=$?

if [[ "$ANALYZER_RC" -ne 0 && "$ANALYZER_RC" -ne 3 && "$ANALYZER_RC" -ne 4 ]]; then
  echo "$GATE: FAIL — ANALYZER CRASHED (exit $ANALYZER_RC). This is a defect in the guard, not a verdict about the installer." >&2
  printf '%s\n' "$VERDICT" | sed 's/^/    /' >&2
  exit 1
fi

if [[ "$ANALYZER_RC" -eq 4 ]]; then
  echo "  SKIP: $(printf '%s' "$VERDICT" | grep -m1 '^SKIP|' | cut -d'|' -f2-)"
elif [[ -n "$VERDICT" ]]; then
  printf '%s\n' "$VERDICT" | grep -E '^(GREEN|SKIPPED)\|' | sed 's/|/: /' | sed 's/^/  /'
fi

if [[ "$ANALYZER_RC" -eq 3 ]]; then
  printf '%s\n' "$VERDICT" | grep '^FINDING|' | cut -d'|' -f2- | sed 's/^/  FAIL: /' >&2
  echo "$GATE: FAIL — $(printf '%s' "$VERDICT" | grep -c '^FINDING|') stale model-id finding(s); $(printf '%s' "$VERDICT" | grep -m1 '^TALLY|' | cut -d'|' -f2-)" >&2
  exit 1
fi

TALLY="$(printf '%s' "$VERDICT" | grep -m1 '^TALLY|' | cut -d'|' -f2-)"
echo "$GATE: PASS — installer exists, is executable, is syntactically valid, and is wired into the setup path ($SETUP_SH)${TALLY:+; $TALLY}"
exit 0
