#!/usr/bin/env python3
"""validate_catalog.py — HXC-159 T-P8.02.4 gate: CM-EXTENSIONS-CATALOG-PRESENT-AND-COMPLETE.

Reads a generated catalog.json (generate_catalog.py's output) AND the source
manifest (scripts/skills_manifest.tsv) and asserts:

  1. catalog.json exists and parses.
  2. EVERY manifest row (by `name`) has a matching row in catalog.json — a
     manifest-registered skill absent from the catalogue FAILs (this is the
     RS-21 runtime signature + the T-P8.02.4 paired-mutation scenario:
     "register a skill with no row -> FAIL").
  3. Every catalogue row carries a non-blank cell for EVERY governed platform
     (§11.4.201(6) — blank is silent absence, never accepted); a cell must be
     a dict with non-empty `status` AND `path` fields.
  4. `status` is drawn from the closed vocabulary
     {SUPPORTED, CONFIG-ONLY, UNTESTED, UNSUPPORTED} — never any other token
     (a typo'd/invented status is as much a bluff as a blank cell).
  5. `path` is never blank/whitespace-only — a genuinely-never-wired platform
     still carries the explicit "N/A" marker, never an empty string
     (RS-21's own text: "never a blank cell").

Exit 0 = PASS, exit 1 = FAIL (prints every violation, never just the first).

Usage:
    python3 validate_catalog.py MANIFEST_TSV CATALOG_JSON
"""
import json
import sys
from pathlib import Path

CLOSED_VOCAB = {"SUPPORTED", "CONFIG-ONLY", "UNTESTED", "UNSUPPORTED"}
PLATFORM_KEYS = {"claude_code", "opencode", "helixcode", "gemini_cli", "qwen_code"}


def parse_manifest(path: Path):
    names = []
    for lineno, raw in enumerate(path.read_text().splitlines(), start=1):
        line = raw.rstrip("\n")
        if not line.strip() or line.lstrip().startswith("#"):
            continue
        parts = line.split("\t")
        if len(parts) != 3:
            continue
        names.append(parts[0].strip())
    return names


def validate(manifest_names, catalog: dict) -> list[str]:
    errs = []
    rows = catalog.get("rows", [])
    by_name = {r.get("name"): r for r in rows}

    for name in manifest_names:
        if name not in by_name:
            errs.append(f"manifest skill '{name}' has NO row in the catalogue")
            continue
        row = by_name[name]
        cells = row.get("cells")
        if not isinstance(cells, dict):
            errs.append(f"skill '{name}': 'cells' is missing/not a dict")
            continue
        missing_platforms = PLATFORM_KEYS - set(cells.keys())
        if missing_platforms:
            errs.append(f"skill '{name}': missing platform columns: {sorted(missing_platforms)}")
        for pkey, cell in cells.items():
            if pkey not in PLATFORM_KEYS:
                continue  # extra platform is not an error; missing ones are checked above
            if not isinstance(cell, dict):
                errs.append(f"skill '{name}' platform '{pkey}': cell is not a dict (blank cell)")
                continue
            status = cell.get("status")
            path = cell.get("path")
            if not status or not str(status).strip():
                errs.append(f"skill '{name}' platform '{pkey}': blank status")
            elif status not in CLOSED_VOCAB:
                errs.append(f"skill '{name}' platform '{pkey}': status '{status}' not in closed vocabulary {sorted(CLOSED_VOCAB)}")
            if not path or not str(path).strip():
                errs.append(f"skill '{name}' platform '{pkey}': blank path (must be a real path or the literal 'N/A')")

    return errs


def main():
    if len(sys.argv) != 3:
        print(f"usage: {sys.argv[0]} MANIFEST_TSV CATALOG_JSON", file=sys.stderr)
        return 2
    manifest_path = Path(sys.argv[1])
    catalog_path = Path(sys.argv[2])

    if not catalog_path.exists():
        print(f"CM-EXTENSIONS-CATALOG-PRESENT-AND-COMPLETE: FAIL — catalog.json absent: {catalog_path}", file=sys.stderr)
        return 1

    manifest_names = parse_manifest(manifest_path)
    try:
        catalog = json.loads(catalog_path.read_text())
    except json.JSONDecodeError as exc:
        print(f"CM-EXTENSIONS-CATALOG-PRESENT-AND-COMPLETE: FAIL — catalog.json does not parse: {exc}", file=sys.stderr)
        return 1

    errs = validate(manifest_names, catalog)
    if errs:
        print(f"CM-EXTENSIONS-CATALOG-PRESENT-AND-COMPLETE: FAIL — {len(errs)} violation(s):", file=sys.stderr)
        for e in errs:
            print(f"  - {e}", file=sys.stderr)
        return 1
    print(
        f"CM-EXTENSIONS-CATALOG-PRESENT-AND-COMPLETE: PASS — "
        f"{len(manifest_names)} manifest skills, all present with complete "
        f"({len(PLATFORM_KEYS)}-platform) non-blank cells"
    )
    return 0


if __name__ == "__main__":
    sys.exit(main())
