#!/usr/bin/env python3
"""validate_ledger.py — HXC-159 T-P7.04.3 gate: no blank cells.

Reads a generated ledger.json (generate_ledger.py's output) and asserts:
  1. total_rows == 80 (57 power-feature + 23 extension), unless the
     generator itself already reported a parse-count discrepancy — in
     which case this validator surfaces that as a FAIL too (a silent
     row-count drift is exactly the defect this gate exists to catch).
  2. every row's evidence_state is one of {"covered", "skip"} — never
     blank/null/missing.
  3. a "covered" row has a non-empty evidence string.
  4. a "skip" row has a non-empty reason AND a non-empty tracked_item
     pointer.

Exit 0 = PASS, exit 1 = FAIL (prints every violation, never just the
first).

Usage:
    python3 validate_ledger.py LEDGER_JSON
"""
import json
import sys


def validate(ledger: dict) -> list[str]:
    errs = []
    if ledger.get("parsed_57_count") != 57:
        errs.append(f"parsed_57_count = {ledger.get('parsed_57_count')}, expected 57")
    if ledger.get("parsed_23_count") != 23:
        errs.append(f"parsed_23_count = {ledger.get('parsed_23_count')}, expected 23")
    rows = ledger.get("rows", [])
    if len(rows) != 80:
        errs.append(f"total rows = {len(rows)}, expected 80 (57 + 23)")
    for i, r in enumerate(rows):
        rid = r.get("id", f"<row {i} missing id>")
        state = r.get("evidence_state")
        if state not in ("covered", "skip"):
            errs.append(f"row {rid}: evidence_state is blank/invalid: {state!r}")
            continue
        if state == "covered":
            if not r.get("evidence"):
                errs.append(f"row {rid}: evidence_state=covered but evidence is blank")
        else:
            if not r.get("reason"):
                errs.append(f"row {rid}: evidence_state=skip but reason is blank")
            if not r.get("tracked_item"):
                errs.append(f"row {rid}: evidence_state=skip but tracked_item is blank")
    return errs


def main():
    if len(sys.argv) != 2:
        print(f"usage: {sys.argv[0]} LEDGER_JSON", file=sys.stderr)
        return 2
    with open(sys.argv[1]) as f:
        ledger = json.load(f)
    errs = validate(ledger)
    if errs:
        print(f"CM-COVERAGE-LEDGER-NO-BLANK: FAIL — {len(errs)} violation(s):", file=sys.stderr)
        for e in errs:
            print(f"  - {e}", file=sys.stderr)
        return 1
    print(f"CM-COVERAGE-LEDGER-NO-BLANK: PASS — {len(ledger.get('rows', []))} rows, zero blank cells")
    return 0


if __name__ == "__main__":
    sys.exit(main())
