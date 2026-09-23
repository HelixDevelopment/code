#!/usr/bin/env bash
# scripts/security/scan_skills_corpus_credentials.sh — HXC-159 T-P7.05.3
# (FR-027): scan a skills corpus for leaked credentials using the EXISTING
# constitution credential scanner, UNCHANGED — never a new/duplicate
# detector. The skills corpus genuinely handles GitHub tokens, LLM
# provider keys, a Postgres DSN, and API-key middleware code (spec.md
# FR-027), so this scan is expected to find PATTERN-SHAPED text; a hit in
# a *_test.go golden fixture, doc example, or middleware/type declaration
# is reviewed and either an intentional fixture (kept, with a note) or a
# genuine leak (a release blocker, rotated + post-mortemed per CONST-042).
#
# Usage:
#   scripts/security/scan_skills_corpus_credentials.sh DIR [DIR...]
#
# Sources constitution/scripts/hooks/credential_scan_lib.sh BY REFERENCE
# (never copied — §11.4.28/§11.4.177) and reuses its exported
# helix_cred_scan_file function VERBATIM.
#
# Output: one line per file scanned with a hit, `<verdict> <path>`, plus a
# summary line. Exit 0 always (this is a REPORTING scan for T-P7.05,
# never a blocking gate by itself — the security battery's C5-blocker
# gate is separate, scripts/security/gate_third_party_activation_blocked.sh).
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
MODULE_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
REPO_ROOT="$(cd "$MODULE_ROOT/.." && pwd)"
CRED_LIB="$REPO_ROOT/constitution/scripts/hooks/credential_scan_lib.sh"

if [ ! -f "$CRED_LIB" ]; then
	echo "scan_skills_corpus_credentials: FATAL — constitution credential scanner not found at $CRED_LIB" >&2
	exit 2
fi
# shellcheck disable=SC1090
. "$CRED_LIB"

if [ "$#" -eq 0 ]; then
	echo "usage: $0 DIR [DIR...]" >&2
	exit 2
fi

total=0
hits=0
hit_paths=()

for dir in "$@"; do
	if [ ! -d "$dir" ]; then
		echo "scan_skills_corpus_credentials: WARN — not a directory, skipping: $dir" >&2
		continue
	fi
	while IFS= read -r -d '' f; do
		total=$((total + 1))
		if helix_cred_scan_file "$f"; then
			hits=$((hits + 1))
			hit_paths+=("$f")
			echo "HIT $f"
		fi
	done < <(find "$dir" -type f \
		\( -name '*.go' -o -name '*.md' -o -name '*.toml' -o -name '*.yaml' -o -name '*.yml' -o -name '*.json' -o -name '*.env*' -o -name '*.sh' \) \
		-not -path '*/.git/*' -print0)
done

echo "scan_skills_corpus_credentials: scanned=$total hits=$hits"
if [ "$hits" -gt 0 ]; then
	printf 'scan_skills_corpus_credentials: reviewed hit: %s\n' "${hit_paths[@]}"
fi
exit 0
