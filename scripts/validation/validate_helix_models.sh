#!/usr/bin/env bash
# validate_helix_models.sh — thin wrapper around validate_helix_models.py so the
# harness is reachable at the conventional scripts/validation/*.sh path.
#
# WHY THE IMPLEMENTATION IS PYTHON, NOT BASH
#   The harness must (a) parse three mutually incompatible JSON response shapes,
#   (b) do float ratio math on token counters, (c) compare verdicts across N
#   repeats for determinism, and (d) stand up an in-process HTTP mock server to
#   run its own golden-good/golden-bad self-validation. In bash that means
#   jq + bc + a separately-managed background server process and its teardown,
#   which is materially more fragile than ~40 lines of stdlib Python. Python 3
#   is used with the standard library only -- no pip dependencies.
#
# All arguments are forwarded verbatim. See --help.
set -euo pipefail
exec python3 "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/validate_helix_models.py" "$@"
