#!/usr/bin/env bash
# test_gate_paired_mutation.sh — T-P8.02.4 paired mutation for
# CM-EXTENSIONS-CATALOG-PRESENT-AND-COMPLETE (§1.1).
#
# RED-then-mutation harness: proves the gate genuinely enforces "every
# manifest skill has a catalogue row with complete per-platform cells" by
# (1) confirming the CURRENT checked-in catalogue passes (sanity: the gate
#     is not permanently broken/always-failing), then
# (2) planting a skill in a TEMP COPY of the manifest with NO corresponding
#     row in a TEMP COPY of catalog.json, and asserting the gate FAILS,
#     naming the planted skill, then
# (3) planting a skill that DOES have a row but with a BLANK platform cell,
#     and asserting the gate FAILS that mutation too.
#
# Never touches the real manifest or the real docs/extensions/ tree — all
# mutation happens in a scratch directory (mktemp -d), removed on exit.
#
# Usage: helix_code/scripts/extensions_catalog/test_gate_paired_mutation.sh
# Exit 0 = every case behaved as expected (gate PASS on real state, FAIL on
#          both planted mutations). Exit 1 = a case misbehaved.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
HELIX_CODE_DIR="$(cd "${SCRIPT_DIR}/../.." && pwd)"
REPO_ROOT="$(cd "${HELIX_CODE_DIR}/.." && pwd)"
VALIDATE="${SCRIPT_DIR}/validate_catalog.py"

TMPDIR="$(mktemp -d "${TMPDIR:-/tmp}/ext_catalog_mutation.XXXXXX")"
trap 'rm -rf "${TMPDIR}"' EXIT

pass=0
fail=0

step() {
  echo "--- $1 ---"
}

# --- Case 0: golden-good — the CURRENT real manifest + catalogue MUST pass
# (§11.4.201(1) false-positive guard: a gate that refuses a genuinely clean
# state is as much a bluff as one that accepts a broken one).
step "Case 0 (golden-good): real manifest + real catalog.json"
if python3 "${VALIDATE}" "${REPO_ROOT}/scripts/skills_manifest.tsv" "${REPO_ROOT}/docs/extensions/catalog.json"; then
  echo "PASS: gate accepts the real, complete catalogue"
  pass=$((pass + 1))
else
  echo "FAIL (unexpected): gate rejected the real, complete catalogue"
  fail=$((fail + 1))
fi

# --- Case 1 (golden-bad #1): a manifest row with NO corresponding catalogue
# row -> MUST FAIL.
step "Case 1 (golden-bad, missing row): plant a skill in the manifest absent from catalog.json"
cp "${REPO_ROOT}/scripts/skills_manifest.tsv" "${TMPDIR}/manifest_missing_row.tsv"
printf 'planted-phantom-skill\tconstitution/skills/planted-phantom-skill\tconstitution\n' >> "${TMPDIR}/manifest_missing_row.tsv"
# catalog.json is the REAL one — it has no row for planted-phantom-skill.
if python3 "${VALIDATE}" "${TMPDIR}/manifest_missing_row.tsv" "${REPO_ROOT}/docs/extensions/catalog.json" 2>"${TMPDIR}/case1.err"; then
  echo "FAIL (unexpected): gate PASSED with a manifest skill missing from the catalogue"
  fail=$((fail + 1))
else
  if grep -q "planted-phantom-skill" "${TMPDIR}/case1.err" && grep -q "NO row in the catalogue" "${TMPDIR}/case1.err"; then
    echo "PASS: gate FAILed and named the planted skill"
    pass=$((pass + 1))
  else
    echo "FAIL (unexpected): gate FAILed but did not name the planted skill:"
    cat "${TMPDIR}/case1.err"
    fail=$((fail + 1))
  fi
fi

# --- Case 2 (golden-bad #2): a catalogue row present but with a BLANK
# platform cell -> MUST FAIL.
step "Case 2 (golden-bad, blank cell): row present but one platform cell is blanked"
python3 - "${REPO_ROOT}/docs/extensions/catalog.json" "${TMPDIR}/catalog_blank_cell.json" <<'PYEOF'
import json, sys
src, dst = sys.argv[1], sys.argv[2]
with open(src) as f:
    catalog = json.load(f)
if not catalog["rows"]:
    raise SystemExit("catalog.json has zero rows — cannot plant a blank-cell mutation")
# Blank the first row's first platform cell's status.
row = catalog["rows"][0]
first_key = next(iter(row["cells"]))
row["cells"][first_key]["status"] = ""
with open(dst, "w") as f:
    json.dump(catalog, f, indent=2)
print(f"planted blank status on skill={row['name']!r} platform={first_key!r}", file=sys.stderr)
PYEOF
if python3 "${VALIDATE}" "${REPO_ROOT}/scripts/skills_manifest.tsv" "${TMPDIR}/catalog_blank_cell.json" 2>"${TMPDIR}/case2.err"; then
  echo "FAIL (unexpected): gate PASSED with a blanked platform cell"
  fail=$((fail + 1))
else
  if grep -q "blank status" "${TMPDIR}/case2.err"; then
    echo "PASS: gate FAILed on the blanked cell"
    pass=$((pass + 1))
  else
    echo "FAIL (unexpected): gate FAILed but not for the blank-status reason:"
    cat "${TMPDIR}/case2.err"
    fail=$((fail + 1))
  fi
fi

echo "=== summary: ${pass} passed, ${fail} failed ==="
if [ "${fail}" -ne 0 ]; then
  exit 1
fi
exit 0
