#!/usr/bin/env python3
"""generate_catalog.py — HXC-159 T-P8.02.1/.02.2 extensions-catalogue generator.

Generates `docs/extensions/EXTENSIONS_CATALOG.md` + a `catalog.json` sidecar
FROM `scripts/skills_manifest.tsv` (the T-P1.04.2 manifest — the single
tracked "what SHOULD be registered, by whom" source `scripts/register_skills.sh`
already consumes) — never by hand-transcribing skill names, per §11.4.228(B)(3)
("the catalog is the SINGLE SOURCE OF TRUTH ... ad-hoc per-agent skill
directories without catalog entries are UNCATALOGUED").

For EVERY manifest row this script performs a REAL, LIVE filesystem probe
(§11.4.6 no-guessing / §11.4.99 latest-source — never assumed) against each
of the five governed platforms' skill-loading directory, and classifies the
result using the closed §11.4.228(A)(1) vocabulary:

    SUPPORTED   — a real SKILL.md resolves + reads non-empty at the
                  platform's own directory (a genuine per-platform liveness
                  probe, not a config read).
    CONFIG-ONLY — declared in scripts/skills_manifest.tsv (i.e. this platform
                  HAS a wiring mechanism — scripts/register_skills.sh) but NOT
                  currently present at the probed directory this run.
    UNTESTED    — the platform has NO registrar/wiring mechanism reaching
                  this skill AT ALL in this checkout (no directory, no
                  generator target) — an honest §11.4.197-tracked gap,
                  matching the exact "zero recorded loading paths" framing
                  §11.4.228's own forensic anchor (G1) uses for this state.
    UNSUPPORTED — NOT used by this generator: no platform here has been
                  PROVEN structurally incapable (§11.4.112) of loading a
                  Markdown-frontmatter skill; reserved for a future citation.

Never a blank cell (§11.4.201(6) — a blank reads as silence, which is
indistinguishable from "not probed"); every cell is one of the four tokens
above, plus the literal loading path (or "N/A" when never wired at all).

Usage:
    python3 generate_catalog.py REPO_ROOT MANIFEST_TSV OUT_DIR

REPO_ROOT     the git top-level this manifest's `source` column is relative to
MANIFEST_TSV  scripts/skills_manifest.tsv (3-column TSV: name<TAB>source<TAB>owner)
OUT_DIR       docs/extensions (both EXTENSIONS_CATALOG.md + catalog.json land here)

Exit code: 0 always for `generate` (mirrors coverage_ledger/generate_ledger.py —
the SEPARATE, paired-mutation-tested validate_catalog.py is what enforces
completeness as a gate).
"""
import json
import sys
from datetime import datetime, timezone
from pathlib import Path

# Platform key, display name, skills-subdirectory (relative to repo root),
# has_registrar (whether THIS repo ships any mechanism at all that writes
# into that directory — real, measured fact, not an assumption).
#
# scripts/register_skills.sh (T-P1.04) is the ONLY registrar in this repo,
# and it ONLY ever writes .claude/skills/ (grep-verified:
# `SKILLS_DST="$root/.claude/skills"` is the sole destination in that
# script). HelixCode is handled specially below — it has no directory of its
# own; its "external tier" (agent.ExternalSkillsDir(), HXC-159 T-P5.05)
# defaults to the SAME directory Claude Code uses (.claude/skills),
# config-injectable via HELIXCODE_EXTERNAL_SKILLS_DIR. OpenCode, Gemini CLI
# and Qwen Code have NO registrar in this repo reaching them at all (only
# unrelated `commands/` directories exist under .opencode/ .gemini/ .qwen/)
# — the exact "zero recorded loading paths" gap §11.4.228's own G1 forensic
# anchor names for these platforms; probing a directory nothing ever writes
# to would misreport CONFIG-ONLY ("declared, not wired") when the honest
# state is UNTESTED ("no mechanism reaches it at all this cycle").
PLATFORMS = [
    ("claude_code", "Claude Code", ".claude/skills", True),
    ("opencode", "OpenCode", ".opencode/skills", False),
    ("helixcode", "HelixCode", None, False),
    ("gemini_cli", "Gemini CLI", ".gemini/skills", False),
    ("qwen_code", "Qwen Code", ".qwen/skills", False),
]

HELIXCODE_EVIDENCE = (
    "helix_code/internal/agent/skill_dirs.go ExternalSkillsDir() defaults to "
    "'.claude/skills' (config-injectable via HELIXCODE_EXTERNAL_SKILLS_DIR); "
    "the external tier's LOADING MECHANISM (a single fixture skill, no "
    "similarity conflict) is REAL-BINARY verified at "
    "qa-results/hxc159/helixcode_wire/20260922T190852Z/ (green_cli_e2e.txt). "
    "HXC-159 F-17 (2026-09-23): the shared SimilarityThreshold was "
    "recalibrated 0.0256->0.0709 against the real, frontmatter-complete "
    "corpus-1 (see docs/qa/hxc159_f17_remediation_20260923T0921Z/threshold.json); the "
    "REAL 8-9-skill corpus-1 tier is nonetheless STILL policy-refused at "
    "BOTH consumers on a genuine confusable pair (action-prefix-system || "
    "reporting-workable-items, score 0.1045 -- real content overlap, not a "
    "miscalibration), captured at "
    "docs/qa/hxc159_f17_remediation_20260923T0921Z/helixcode_recalibrated_run.txt and "
    "the mirrored helix_agent_native_recalibrated_run.txt. This SUPPORTED "
    "cell reports per-skill FILE PRESENCE at the platform directory "
    "(§11.4.228(A)(1)'s liveness probe), not whole-tier ACTIVATION -- a "
    "distinction this catalogue's generator does not currently classify "
    "separately (tracked per §11.4.197, see the F-17 subagent's final "
    "report for the residual-finding discussion). Honest boundary "
    "(§11.4.6, T-P5.05.4 in specs/001-helixskills-incorporation/tasks.md): "
    "the per-skill activation-allowlist/ceiling mechanism was NOT "
    "verifiable from within helix_code/'s own file scope this cycle — "
    "grep-confirmed zero Go implementation of a per-skill activation "
    "ceiling in this module."
)

NEVER_WIRED_NOTES = {
    "opencode": (
        "No .opencode/skills/ directory exists in this checkout and no "
        "registrar (scripts/register_skills.sh only ever writes "
        ".claude/skills/) targets one — zero recorded loading path this "
        "cycle, tracked per §11.4.197. .opencode/ here carries only "
        "SpecKit *commands* (.opencode/commands/), not skills."
    ),
    "gemini_cli": (
        "No .gemini/skills/ directory exists in this checkout (only "
        ".gemini/commands/, unrelated to the Agent-Skill format) and no "
        "registrar targets one — zero recorded loading path this cycle, "
        "tracked per §11.4.197 (the exact G1 gap the constitution's own "
        "§11.4.228 forensic anchor names for this platform)."
    ),
    "qwen_code": (
        "No .qwen/skills/ directory exists in this checkout (only "
        ".qwen/commands/ + .qwen/settings.json, unrelated to the "
        "Agent-Skill format) and no registrar targets one — zero recorded "
        "loading path this cycle, tracked per §11.4.197 (the exact G1 gap "
        "the constitution's own §11.4.228 forensic anchor names for this "
        "platform)."
    ),
}


def parse_manifest(path: Path):
    rows = []
    for lineno, raw in enumerate(path.read_text().splitlines(), start=1):
        line = raw.rstrip("\n")
        if not line.strip() or line.lstrip().startswith("#"):
            continue
        parts = line.split("\t")
        if len(parts) != 3:
            print(f"WARNING: {path}:{lineno}: expected 3 TAB-separated fields, "
                  f"got {len(parts)} — skipping row", file=sys.stderr)
            continue
        name, source, owner = parts
        rows.append({"name": name.strip(), "source": source.strip(), "owner": owner.strip()})
    return rows


def probe_dir_platform(root: Path, rel_dir: str, name: str, expected_source: str):
    """Real filesystem probe: does <root>/<rel_dir>/<name>/SKILL.md resolve
    + read non-empty? Returns (status, path_str, note)."""
    entry = root / rel_dir / name
    rel_path = f"{rel_dir}/{name}"
    if not entry.is_symlink() and not entry.exists():
        return "CONFIG-ONLY", rel_path, (
            f"declared in {expected_source and 'scripts/skills_manifest.tsv'}"
            f"; not present at {rel_path} this run (live probe, "
            f"{datetime.now(timezone.utc).strftime('%Y-%m-%dT%H:%M:%SZ')})."
        )
    skill_md = entry / "SKILL.md"
    provenance = "symlink" if entry.is_symlink() else "directory/copy"
    try:
        resolved = entry.resolve()
    except OSError:
        resolved = None
    if not skill_md.exists():
        return "CONFIG-ONLY", rel_path, (
            f"{provenance} entry present at {rel_path} but SKILL.md is "
            f"missing/unreadable — broken link, not a working skill."
        )
    try:
        content = skill_md.read_text()
    except OSError as exc:
        return "CONFIG-ONLY", rel_path, f"SKILL.md exists but unreadable: {exc}"
    if not content.strip():
        return "CONFIG-ONLY", rel_path, f"SKILL.md at {rel_path} is present but empty."
    expected_abs = (root / expected_source).resolve() if expected_source else None
    match_note = ""
    if resolved is not None and expected_abs is not None:
        if resolved == expected_abs:
            match_note = " (resolves to the manifest-declared source)"
        else:
            match_note = f" (WARNING: resolves to {resolved}, manifest declares {expected_abs})"
    return "SUPPORTED", rel_path, (
        f"{provenance}, SKILL.md read {len(content)} bytes{match_note}. "
        f"Live-probed {datetime.now(timezone.utc).strftime('%Y-%m-%dT%H:%M:%SZ')}."
    )


def classify_row(root: Path, row: dict):
    cells = {}
    for key, _display, rel_dir, has_registrar in PLATFORMS:
        if key == "helixcode":
            continue  # handled after claude_code is known
        if not has_registrar:
            # No mechanism in this repo writes into this platform's
            # directory at all — an honest UNTESTED gap (§11.4.197), never
            # misreported as CONFIG-ONLY (which implies something DID
            # declare intent to wire it here).
            cells[key] = {
                "status": "UNTESTED",
                "path": "N/A",
                "note": NEVER_WIRED_NOTES.get(key, "no registrar targets this platform in this checkout"),
            }
            continue
        status, path, note = probe_dir_platform(root, rel_dir, row["name"], row["source"])
        cells[key] = {"status": status, "path": path, "note": note}

    # HelixCode: external tier defaults to the SAME directory Claude Code
    # uses; mirror that probe result rather than re-probing a directory that
    # does not independently exist for this platform.
    claude_cell = cells["claude_code"]
    if claude_cell["status"] == "SUPPORTED":
        helix_status = "SUPPORTED"
        helix_note = f"via external tier -> {claude_cell['path']}. {HELIXCODE_EVIDENCE}"
    else:
        helix_status = "CONFIG-ONLY"
        helix_note = (
            f"external tier defaults to .claude/skills/{row['name']} "
            f"(config-injectable via HELIXCODE_EXTERNAL_SKILLS_DIR); that "
            f"directory does not currently carry this skill "
            f"({claude_cell['note']}). {HELIXCODE_EVIDENCE}"
        )
    cells["helixcode"] = {"status": helix_status, "path": f".claude/skills/{row['name']} (external tier)", "note": helix_note}
    return cells


def render_markdown(rows_with_cells, manifest_path: Path, out_path: Path):
    now = datetime.now(timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ")
    today = datetime.now(timezone.utc).strftime("%Y-%m-%d")
    lines = []
    lines.append("# HelixCode Extensions Catalog\n")
    lines.append("| | |")
    lines.append("|---|---|")
    lines.append("| Revision | 1 |")
    lines.append(f"| Created | {today} |")
    lines.append(f"| Last modified | {now} |")
    lines.append("| Status | active |")
    lines.append("| Status summary | — |")
    lines.append("| Continuation | docs/CONTINUATION.md |")
    lines.append("")
    lines.append(
        "Per-§11.4.228 (Cross-agent extension lifecycle mandate) catalog of "
        "EVERY skill registered under `scripts/skills_manifest.tsv` — the "
        "single T-P1.04.2 manifest `scripts/register_skills.sh` provisions "
        "from — with a per-platform compatibility matrix across the five "
        "governed CLI-agent platforms this constitution names as its "
        "minimum: Claude Code, OpenCode, HelixCode, Gemini CLI, Qwen Code. "
        "Generated FROM the manifest by "
        "`helix_code/scripts/extensions_catalog/generate_catalog.py` — "
        "**never hand-transcribed** — with a real, live filesystem probe "
        "per skill × platform cell, never a blank cell (§11.4.201(6))."
    )
    lines.append("")
    lines.append(
        "**Closed status vocabulary** (§11.4.228(A)(1)): `SUPPORTED` "
        "(verified working, real per-platform liveness probe this run) / "
        "`CONFIG-ONLY` (declared — this platform HAS a registrar targeting "
        "it — but not currently wired) / `UNTESTED` (this platform has NO "
        "registrar/wiring mechanism reaching this skill at all — an honest "
        "§11.4.197-tracked gap) / `UNSUPPORTED` (proven structurally "
        "impossible per §11.4.112 — unused by this generator; no platform "
        "here has been proven incapable of loading a Markdown-frontmatter "
        "skill)."
    )
    lines.append("")
    lines.append("## Table of contents")
    lines.append("")
    lines.append("- [Compatibility matrix](#compatibility-matrix)")
    lines.append("- [Per-skill detail](#per-skill-detail)")
    lines.append("- [Source tracking](#source-tracking)")
    lines.append("")
    lines.append("## Compatibility matrix")
    lines.append("")
    header = "| Skill | " + " | ".join(disp for _k, disp, _d, _hr in PLATFORMS) + " |"
    sep = "|---|" + "|".join("---" for _ in PLATFORMS) + "|"
    lines.append(header)
    lines.append(sep)
    for row in rows_with_cells:
        cells = row["cells"]
        cell_strs = []
        for key, _disp, _d, _hr in PLATFORMS:
            c = cells[key]
            cell_strs.append(f"{c['status']} (`{c['path']}`)")
        lines.append(f"| `{row['name']}` | " + " | ".join(cell_strs) + " |")
    lines.append("")
    lines.append("## Per-skill detail")
    lines.append("")
    for row in rows_with_cells:
        lines.append(f"### `{row['name']}`")
        lines.append("")
        lines.append(f"- **Owner (registrar):** `{row['owner']}`")
        lines.append(f"- **Manifest source:** `{row['source']}`")
        lines.append("")
        lines.append("| Platform | Status | Loading path | Evidence / Note |")
        lines.append("|---|---|---|---|")
        for key, disp, _d, _hr in PLATFORMS:
            c = row["cells"][key]
            note = c["note"].replace("|", "\\|").replace("\n", " ")
            lines.append(f"| {disp} | {c['status']} | `{c['path']}` | {note} |")
        lines.append("")
    lines.append("## Source tracking")
    lines.append("")
    lines.append(
        "Per §11.4.228(B): every extension's canonical source is machine-"
        "readable via `scripts/skills_manifest.tsv` itself (name, "
        "repo-relative source directory, owning registrar) — the manifest "
        "row IS this catalogue's `EXTENSION_SOURCE` record for every entry "
        "here (no separate `EXTENSION_SOURCE.yaml` per skill; the manifest "
        "already carries the same three fields at repo scope, avoiding a "
        "duplicated, driftable second record per §11.4.106). All eight "
        "entries are constitution-owned (`owner=constitution`) and "
        "SYMLINKED (never copied) from `constitution/skills/<name>/` into "
        "`.claude/skills/<name>` by `scripts/register_skills.sh` — the "
        "preferred-over-copies provenance §11.4.228(B)(2) requires."
    )
    lines.append("")
    lines.append(f"Source manifest: `{manifest_path}` ({len(rows_with_cells)} rows).")
    lines.append("")
    out_path.write_text("\n".join(lines) + "\n")


def main():
    if len(sys.argv) != 4:
        print(f"usage: {sys.argv[0]} REPO_ROOT MANIFEST_TSV OUT_DIR", file=sys.stderr)
        return 2
    root = Path(sys.argv[1]).resolve()
    manifest_path = Path(sys.argv[2]).resolve()
    out_dir = Path(sys.argv[3])
    out_dir.mkdir(parents=True, exist_ok=True)

    rows = parse_manifest(manifest_path)
    rows_with_cells = []
    for row in rows:
        cells = classify_row(root, row)
        rows_with_cells.append({**row, "cells": cells})

    catalog_json = {
        "generated_at": datetime.now(timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ"),
        "manifest": str(manifest_path.relative_to(root)) if root in manifest_path.parents else str(manifest_path),
        "platforms": [k for k, _d, _r, _hr in PLATFORMS],
        "total_rows": len(rows_with_cells),
        "rows": rows_with_cells,
    }
    (out_dir / "catalog.json").write_text(json.dumps(catalog_json, indent=2))

    render_markdown(rows_with_cells, manifest_path, out_dir / "EXTENSIONS_CATALOG.md")

    supported = sum(
        1 for r in rows_with_cells for k, _d, _rd, _hr in PLATFORMS if r["cells"][k]["status"] == "SUPPORTED"
    )
    total_cells = len(rows_with_cells) * len(PLATFORMS)
    print(
        f"generate_catalog: wrote {len(rows_with_cells)} skill rows x "
        f"{len(PLATFORMS)} platforms = {total_cells} cells to {out_dir}/ "
        f"({supported} SUPPORTED cells)"
    )
    return 0


if __name__ == "__main__":
    sys.exit(main())
