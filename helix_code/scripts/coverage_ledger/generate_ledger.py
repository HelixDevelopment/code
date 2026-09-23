#!/usr/bin/env python3
"""generate_ledger.py — HXC-159 T-P7.04 coverage-ledger generator.

Parses specs/001-helixskills-incorporation/spec.md §5 (the 57-row
power-feature inventory table set, sections 5.A-5.F) and §6 (the
23-member constitution-extension-disposition table) DIRECTLY from the
tracked spec document — never by hand-transcribing the rows — and emits
a feature x test-type x evidence-state coverage ledger with NO BLANK
CELLS: every row resolves to either `covered` (with a real evidence
path) or an honest `skip` (with a §11.4.3-style reason and a tracked
migration-item pointer).

Usage:
    python3 generate_ledger.py SPEC_MD EVIDENCE_MAP_JSON OUT_DIR

SPEC_MD           path to specs/001-helixskills-incorporation/spec.md
EVIDENCE_MAP_JSON path to a small curated JSON file mapping a row id
                  (e.g. "A9", "5.6-REUSE-credential_scan_lib") to either
                  {"covered": true, "evidence": "<path or description>"}
                  or {"covered": false, "reason": "<why not this cycle>"}.
                  A row id absent from this map gets the generator's
                  OWN default: honest skip, generic reason, pointing at
                  the shared T-P7.04 follow-up tracked item. This keeps
                  "never by hand" true for the LEDGER (script-generated)
                  while the evidence map is legitimate curated INPUT
                  data (what evidence exists is a fact the generator
                  cannot discover by re-reading spec.md alone).

Exit code: 0 always for `generate`; the SEPARATE validate_ledger.py
(paired-mutation-tested) is what enforces the no-blank-cell invariant as
a gate.
"""
import json
import re
import sys
from pathlib import Path

DISPOSITION_RE = re.compile(r"\*\*([A-Z/ ]+?)\*\*")
TRACKED_FOLLOWUP_ITEM = (
    "HXC-159-P7-FOLLOWUP: DEP/UPS/N/S/DEF-disposition rows are tracked "
    "as a standing coverage gap in specs/001-helixskills-incorporation/"
    "tasks.md Phase P8 (documentation) / P9 and beyond — see spec.md §5/§6 "
    "for the per-row disposition reasoning, which IS the tracked record "
    "for scope-decision rows (N/S), and tasks.md's existing DEF-row "
    "citations (A10/D8 scheduling-duplication decision, C6 tree-sitter "
    "partial, E5 Watch unimplemented) for deferred-row follow-ups."
)


def parse_section_5(spec_text: str):
    """Extract the 57 §5.A-5.F rows: (id, feature, disposition, reason)."""
    rows = []
    # Each table row: | A1 | Skill CRUD store | Complete | **DEP** | reason... |
    row_re = re.compile(
        r"^\|\s*([A-F]\d{1,2})\s*\|\s*(.+?)\s*\|\s*(.+?)\s*\|\s*(.+?)\s*\|\s*(.+?)\s*\|\s*$",
        re.MULTILINE,
    )
    section5 = spec_text.split("## 5. Complete power-feature inventory")[1]
    section5 = section5.split("## 6. Constitution-derived extension dispositions")[0]
    for m in row_re.finditer(section5):
        row_id, feature, upstream_state, disposition_cell, reason = m.groups()
        if row_id.strip() in ("#",) or not re.match(r"^[A-F]\d+$", row_id.strip()):
            continue
        disp_match = DISPOSITION_RE.search(disposition_cell)
        disposition = disp_match.group(1).strip() if disp_match else disposition_cell.strip()
        rows.append(
            {
                "id": row_id.strip(),
                "feature": feature.strip(),
                "disposition": disposition,
                "reason": reason.strip(),
                "kind": "power-feature",
            }
        )
    return rows


def parse_section_6(spec_text: str):
    """Extract the 23 §6 extension-disposition members: (id, member, verdict)."""
    section6 = spec_text.split("## 6. Constitution-derived extension dispositions")[1]
    section6 = section6.split("## 7. Bridge layer dispositions")[0]
    row_re = re.compile(
        r"^\|\s*\*\*([A-Z]+)\*\*\s*\|\s*(\d+)\s*\|\s*(.+?)\s*\|\s*(.+?)\s*\|\s*$",
        re.MULTILINE,
    )
    rows = []
    for m in row_re.finditer(section6):
        verdict, count, members_cell, where = m.groups()
        # Members are comma-separated at the top level; MOST are fully
        # backtick-wrapped (`a`, `b`), but a few genuinely are not
        # (bare "SpecKit", "Superpowers", "Bridge L3/L4/L5") and one is
        # PARTIALLY wrapped ("`helix` plugin" — only the code-styled
        # word is backticked, the member name is the whole two-word
        # phrase). Split on top-level ", " first, THEN strip backticks
        # only when a whole segment is fully wrapped — never regex the
        # backtick pairs across the whole cell, which silently drops
        # every unwrapped member (the exact defect measured 2026-09-22:
        # a whole-cell `findall(r"`([^`]+)`")` parsed 20 of 23 members).
        members = []
        for segment in re.split(r",\s*", members_cell.strip()):
            segment = segment.strip()
            if not segment:
                continue
            full_wrap = re.fullmatch(r"`([^`]+)`", segment)
            members.append(full_wrap.group(1) if full_wrap else segment)
        for idx, member in enumerate(members, start=1):
            rows.append(
                {
                    "id": f"6-{verdict}-{idx:02d}",
                    "feature": member,
                    "disposition": verdict,
                    "reason": where.strip(),
                    "kind": "extension",
                }
            )
    return rows


def load_evidence_map(path: Path):
    if not path.exists():
        return {}
    with path.open() as f:
        return json.load(f)


def classify(row, evidence_map):
    """Resolve one row to a non-blank evidence-state cell."""
    lookup_keys = [row["id"], row["feature"]]
    entry = None
    for k in lookup_keys:
        if k in evidence_map:
            entry = evidence_map[k]
            break
    if entry and entry.get("covered"):
        return {
            "evidence_state": "covered",
            "evidence": entry.get("evidence", "(evidence path not recorded — generator defect)"),
        }
    reason = None
    if entry and entry.get("reason"):
        reason = entry["reason"]
    else:
        disp = row["disposition"]
        if disp == "DEP":
            reason = (
                "out-of-process dependency (D-3): tested within "
                "HelixDevelopment/skills' own upstream test suite, not "
                "retested at the consumer seam this cycle."
            )
        elif disp == "UPS":
            reason = "upstream-only per spec.md §5 disposition; not surfaced to consumers this feature."
        elif disp == "N/S":
            reason = "explicitly out of scope per spec.md §5/§6 disposition (a scope decision, not a gap)."
        elif disp == "DEF":
            reason = "deferred per spec.md's own named reason; see tasks.md/spec.md for the tracked follow-on."
        elif disp in ("BLOCKED",):
            reason = "blocked per spec.md §7 Bridge-layer disposition."
        else:
            reason = f"not exercised by HXC-159 Phase P7 (T-P7.01/.02/.04/.05) this cycle; disposition={disp}."
    return {
        "evidence_state": "skip",
        "reason": reason,
        "tracked_item": TRACKED_FOLLOWUP_ITEM,
    }


def main():
    if len(sys.argv) != 4:
        print(f"usage: {sys.argv[0]} SPEC_MD EVIDENCE_MAP_JSON OUT_DIR", file=sys.stderr)
        return 2
    spec_path, evidence_path, out_dir = Path(sys.argv[1]), Path(sys.argv[2]), Path(sys.argv[3])
    out_dir.mkdir(parents=True, exist_ok=True)

    spec_text = spec_path.read_text()
    evidence_map = load_evidence_map(evidence_path)

    rows_57 = parse_section_5(spec_text)
    rows_23 = parse_section_6(spec_text)

    if len(rows_57) != 57:
        print(f"WARNING: parsed {len(rows_57)} §5 rows, expected 57 — reporting the discrepancy honestly, not forcing a match", file=sys.stderr)
    if len(rows_23) != 23:
        print(f"WARNING: parsed {len(rows_23)} §6 rows, expected 23 — reporting the discrepancy honestly, not forcing a match", file=sys.stderr)

    ledger = []
    for row in rows_57 + rows_23:
        cell = classify(row, evidence_map)
        ledger.append({**row, **cell})

    (out_dir / "ledger.json").write_text(json.dumps({
        "parsed_57_count": len(rows_57),
        "parsed_23_count": len(rows_23),
        "total_rows": len(ledger),
        "rows": ledger,
    }, indent=2))

    with (out_dir / "ledger.md").open("w") as f:
        f.write("# HXC-159 T-P7.04 Coverage Ledger\n\n")
        f.write(f"Parsed {len(rows_57)} power-feature rows (§5, expected 57) + "
                f"{len(rows_23)} extension rows (§6, expected 23) = {len(ledger)} total.\n\n")
        f.write("| ID | Kind | Feature | Disposition | Evidence-state | Evidence / Reason |\n")
        f.write("|---|---|---|---|---|---|\n")
        for r in ledger:
            if r["evidence_state"] == "covered":
                detail = r["evidence"]
            else:
                detail = f"SKIP: {r['reason']}"
            feature_cell = r["feature"].replace("|", "\\|")
            detail_cell = detail.replace("|", "\\|").replace("\n", " ")
            f.write(f"| {r['id']} | {r['kind']} | {feature_cell} | {r['disposition']} | {r['evidence_state']} | {detail_cell} |\n")

    print(f"generate_ledger: wrote {len(ledger)} rows to {out_dir}/ledger.md + ledger.json "
          f"({sum(1 for r in ledger if r['evidence_state']=='covered')} covered, "
          f"{sum(1 for r in ledger if r['evidence_state']=='skip')} skipped)")
    return 0


if __name__ == "__main__":
    sys.exit(main())
