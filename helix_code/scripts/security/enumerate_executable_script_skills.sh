#!/usr/bin/env bash
# scripts/security/enumerate_executable_script_skills.sh — HXC-159
# T-P7.05.4: enumerate every skill in a catalogue that ships an
# executable script, as a single reviewable SET rather than scattered
# per-file discovery. A skill "ships executable scripts" when its
# directory contains a file that is either (a) marked executable
# (`-perm -u+x`) or (b) has a common script extension
# (.sh/.py/.rb/.js/.ts/.pl) — covering scripts a source repository may
# have stripped the executable bit from on checkout/export.
#
# Usage:
#   scripts/security/enumerate_executable_script_skills.sh DIR [DIR...]
#
# Output: one CSV-ish line per (skill_dir, script_path) pair, plus a
# summary count, to stdout.
set -euo pipefail

if [ "$#" -eq 0 ]; then
	echo "usage: $0 DIR [DIR...]" >&2
	exit 2
fi

total_skills=0
skills_with_scripts=0
total_scripts=0

for root in "$@"; do
	if [ ! -d "$root" ]; then
		echo "enumerate_executable_script_skills: WARN — not a directory, skipping: $root" >&2
		continue
	fi
	while IFS= read -r -d '' skill_md; do
		skill_dir="$(dirname "$skill_md")"
		total_skills=$((total_skills + 1))
		found_any=0
		while IFS= read -r -d '' script; do
			if [ "$found_any" -eq 0 ]; then
				skills_with_scripts=$((skills_with_scripts + 1))
				found_any=1
			fi
			total_scripts=$((total_scripts + 1))
			echo "SCRIPT_SKILL	$skill_dir	$script"
		done < <(find "$skill_dir" -type f \
			\( -perm -u+x -o -name '*.sh' -o -name '*.py' -o -name '*.rb' -o -name '*.js' -o -name '*.ts' -o -name '*.pl' \) \
			-print0)
	done < <(find "$root" -type f -iname 'SKILL.md' -print0)
done

echo "enumerate_executable_script_skills: total_skills=$total_skills skills_with_scripts=$skills_with_scripts total_scripts=$total_scripts"
