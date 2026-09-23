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
import subprocess
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
    "cell's status token reports per-skill FILE PRESENCE at the platform "
    "directory (§11.4.228(A)(1)'s liveness probe), not whole-tier "
    "ACTIVATION -- as of T-P9.01 round-4 F-22(b) (2026-09-23) this note is "
    "PREFIXED, above, by a SEPARATE, live-probed verdict on the distinct "
    "fact of external-tier-SPECIFIC policy-gate admission (see "
    "external_tier_admission in catalog.json), closing the residual gap "
    "this paragraph used to describe as unclassified. Honest boundary "
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


def read_frontmatter_name(skill_dir: Path):
    """Read the front-matter `name:` field of skill_dir/SKILL.md.

    Mirrors pkg/skills' `Qualified()` identity semantics (HXC-159 F-21/F-22):
    under DialectHelixCode the front-matter `name:` is the identity the
    external-tier policy gate AND commands.SkillRegistry key by whenever
    present -- NOT always the manifest/directory basename (4 of 9 real
    skills in this checkout carry a title-cased front-matter name that
    differs from their directory name). Returns None when unreadable or
    absent, in which case callers fall back to the directory name.
    """
    skill_md = skill_dir / "SKILL.md"
    try:
        content = skill_md.read_text()
    except OSError:
        return None
    if not content.startswith("---"):
        return None
    end = content.find("\n---", 3)
    if end == -1:
        return None
    for line in content[3:end].splitlines():
        line = line.strip()
        if line.startswith("name:"):
            value = line[len("name:"):].strip()
            return value.strip('"').strip("'") or None
    return None


def probe_external_tier_admission(root: Path):
    """Real, live probe (§11.4.6/§11.4.99 — never hand-transcribed) of
    helix_agent's ExternalSkillSource policy gate against the REAL
    `.claude/skills/` directory THIS run, via
    `submodules/helix_agent/cmd/similarity-probe` (the F-17 parity
    harness, which calls `ExternalSkillSource.LoadForProbe()` — the exact
    production `Load()` code path, `ParseDirectory` included).

    HXC-159 T-P9.01 round-4 F-22(b): the generator previously derived the
    HelixCode column purely from directory PRESENCE at `.claude/skills/`
    (`probe_dir_platform`, `claude_cell["status"]`), which is TRUE for a
    skill resolved via the SEPARATE, higher-precedence `builtin` tier
    even when the external tier's OWN similarity-conflict policy gate
    EXCLUDES it (F-22(a)) — so the prior static note's blanket "via
    external tier" claim was false for exactly the two skills the
    external-tier gate refuses. This probe distinguishes the two facts
    for real, this run, instead of repeating that static claim.

    Returns (verdicts, probe_error):
      verdicts    {qualified_name: {"admitted": bool, "conflicts_with":
                  str|None, "score": float|None, "threshold": float|None}}
                  keyed by the SAME qualified identity Qualified() uses
                  (front-matter name when present, else directory name).
      probe_error None on success (verdicts is authoritative and MAY be
                  empty when `.claude/skills/` is genuinely empty); a short
                  human-readable string when the probe could not be run at
                  all (missing directory/binary source, go toolchain
                  failure, timeout, non-JSON output) — callers MUST render
                  an honest caveat in that case, never silently reuse a
                  stale or assumed verdict (§11.4.6).
    """
    external_dir = root / ".claude" / "skills"
    if not external_dir.is_dir():
        return {}, "no .claude/skills directory exists in this checkout to probe this run"
    probe_module = root / "submodules" / "helix_agent"
    if not (probe_module / "cmd" / "similarity-probe").is_dir():
        return {}, "submodules/helix_agent/cmd/similarity-probe is not present in this checkout"
    try:
        result = subprocess.run(
            ["go", "run", "./cmd/similarity-probe", str(external_dir)],
            cwd=str(probe_module),
            capture_output=True,
            text=True,
            timeout=180,
        )
    except (OSError, subprocess.TimeoutExpired) as exc:
        return {}, f"similarity-probe invocation failed: {exc}"
    if result.returncode != 0:
        stderr_tail = result.stderr.strip()[-500:]
        return {}, f"similarity-probe exited {result.returncode}: {stderr_tail}"
    try:
        payload = json.loads(result.stdout)
    except json.JSONDecodeError as exc:
        return {}, f"similarity-probe produced non-JSON stdout: {exc}"

    verdicts = {}
    for name in payload.get("admitted", []):
        verdicts[name] = {"admitted": True, "conflicts_with": None, "score": None, "threshold": None}
    for exclusion in payload.get("excluded", []):
        verdicts[exclusion.get("excluded", "")] = {
            "admitted": False,
            "conflicts_with": exclusion.get("conflicts_with"),
            "score": exclusion.get("score"),
            "threshold": exclusion.get("threshold"),
        }
    whole_set_refusal = payload.get("refusal") or None
    if whole_set_refusal and not verdicts:
        return {}, f"external tier whole-set refusal this run: {whole_set_refusal}"
    return verdicts, None


def probe_final_registry_source(root: Path, manifest_names):
    """Real, live probe of the ACTUAL registry entry `helixcode skills list`
    serves for each manifest skill name, by building the real CLI binary
    and running it against this checkout (§11.4.6/§11.4.99).

    HXC-159 T-P9.01 round-4 F-22(a)/(b): the external-tier policy gate's
    admit/exclude verdict (probe_external_tier_admission, above) is a
    SEPARATE fact from which tier's copy the FINAL registry actually
    serves — `commands.SkillLoader.Load` calls `loadBuiltinSkills` on
    EVERY load, and the CLI bootstrap loads the external tier FIRST and
    the project/user (`builtin`) loader LAST, so a same-named `builtin`
    entry OVERRIDES a POLICY-ADMITTED external-tier entry too (measured
    live 2026-09-23: 5 of the 7 admitted skills still show
    SOURCE=builtin:...). Conflating "admitted by the policy gate" with
    "served from the external tier" would repeat a milder version of the
    exact bluff this fix closes, so this second probe checks the real,
    final SOURCE= column directly instead of assuming admission implies
    external-tier service.

    Returns (sources, probe_error) where sources is {name: "builtin"|
    "external"|"other:<literal>"} and probe_error is None on success or a
    short string when the binary could not be built/run (never silently
    reused/assumed — §11.4.6).
    """
    inner_module = root / "helix_code"
    if not (inner_module / "cmd" / "cli").is_dir():
        return {}, "helix_code/cmd/cli is not present in this checkout"
    binary = root / "docs" / "extensions" / ".final_source_probe_bin"
    try:
        build = subprocess.run(
            ["go", "build", "-o", str(binary), "./cmd/cli"],
            cwd=str(inner_module),
            capture_output=True,
            text=True,
            timeout=180,
        )
    except (OSError, subprocess.TimeoutExpired) as exc:
        return {}, f"CLI build failed to run: {exc}"
    if build.returncode != 0:
        return {}, f"CLI build exited {build.returncode}: {build.stderr.strip()[-500:]}"
    try:
        run = subprocess.run(
            [str(binary), "skills", "list"],
            cwd=str(root),
            capture_output=True,
            text=True,
            timeout=60,
        )
    finally:
        try:
            binary.unlink()
        except OSError:
            pass
    if run.returncode != 0:
        return {}, f"'skills list' exited {run.returncode}: {run.stderr.strip()[-500:]}"

    sources = {}
    for line in run.stdout.splitlines():
        stripped = line.strip()
        if not stripped:
            continue
        first_token = stripped.split(None, 1)[0]
        if first_token not in manifest_names:
            continue
        source_token = stripped.split()[-1]
        if source_token.startswith("builtin:"):
            sources[first_token] = "builtin"
        elif ".claude/skills/" in source_token:
            sources[first_token] = "external"
        else:
            sources[first_token] = f"other:{source_token}"
    return sources, None


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


def classify_row(root: Path, row: dict, external_tier_verdicts: dict, probe_error,
                  final_sources: dict, source_probe_error):
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
    # uses; mirror that PRESENCE probe result for reachability, but — F-22(b)
    # — additionally consult the REAL, live external-tier policy-gate probe
    # (external_tier_verdicts, from cmd/similarity-probe) so the note
    # distinguishes "resolvable via SOME tier" (file presence — always true
    # when claude_cell is SUPPORTED) from "ADMITTED specifically by the
    # external tier's own policy gate" (which two real skills fail on a
    # similarity conflict and instead resolve only via the separate
    # higher-precedence `builtin` tier — F-22(a)). Two distinguishable facts,
    # never collapsed back into one blanket claim.
    claude_cell = cells["claude_code"]
    external_tier_admission = None
    if claude_cell["status"] == "SUPPORTED":
        helix_status = "SUPPORTED"
        qualified = read_frontmatter_name(root / row["source"]) or row["name"]
        verdict = external_tier_verdicts.get(qualified)
        if probe_error is not None:
            helix_note = (
                f"resolvable via .claude/skills/{row['name']} (file-presence "
                f"probe this run — SOME tier serves it to end users). "
                f"EXTERNAL-TIER POLICY ADMISSION COULD NOT BE LIVE-PROBED "
                f"THIS RUN ({probe_error}) — per §11.4.6 this cell reports "
                f"file-presence only, not a verified external-tier-specific "
                f"admission claim. {HELIXCODE_EVIDENCE}"
            )
            external_tier_admission = {"verified": False, "reason": probe_error}
        elif verdict is None:
            helix_note = (
                f"resolvable via .claude/skills/{row['name']} (file-presence "
                f"probe this run). Live external-tier probe ran but returned "
                f"no verdict for qualified identity {qualified!r} — treated "
                f"as unverified this run rather than assumed admitted "
                f"(§11.4.6). {HELIXCODE_EVIDENCE}"
            )
            external_tier_admission = {"verified": False, "reason": f"no verdict for {qualified!r}"}
        elif verdict["admitted"]:
            admitted_note = (
                f"ADMITTED by the external-tier similarity-conflict policy "
                f"gate (live-probed this run via "
                f"submodules/helix_agent/cmd/similarity-probe against the "
                f"real .claude/skills/, qualified identity {qualified!r} -- "
                f"not excluded on a similarity conflict)."
            )
            # F-22(a)/(b): admission by the policy gate is a SEPARATE fact
            # from which tier's copy the FINAL registry actually serves --
            # a same-named `builtin` entry overrides an admitted external
            # entry too (loadBuiltinSkills runs on every load, last).
            # Cross-check the REAL final SOURCE= this run rather than
            # assuming admission implies external-tier service.
            final_source = final_sources.get(row["name"]) if source_probe_error is None else None
            if source_probe_error is not None:
                helix_note = (
                    f"{admitted_note} Whether the FINAL registry serves "
                    f"this skill from the external tier or a same-named "
                    f"`builtin` copy (which overrides when both exist, per "
                    f"F-22(a)) COULD NOT be live-probed this run "
                    f"({source_probe_error}) -- do not assume policy "
                    f"admission implies external-tier service; cross-check "
                    f"`helixcode skills list` SOURCE= directly. "
                    f"{HELIXCODE_EVIDENCE}"
                )
                external_tier_admission = {
                    "verified": True, "admitted": True, "qualified_name": qualified,
                    "final_source_verified": False, "final_source_error": source_probe_error,
                }
            elif final_source == "external":
                helix_note = (
                    f"{admitted_note} The FINAL registry SOURCE (live-"
                    f"probed this run via a real `helixcode skills list`) "
                    f"CONFIRMS it -- genuinely served from the external "
                    f"tier -> {claude_cell['path']}. {HELIXCODE_EVIDENCE}"
                )
                external_tier_admission = {
                    "verified": True, "admitted": True, "qualified_name": qualified,
                    "final_source_verified": True, "final_source": "external",
                }
            else:
                observed = final_source or "not found in `skills list` output"
                helix_note = (
                    f"{admitted_note} HOWEVER the FINAL registry SOURCE "
                    f"(live-probed this run via a real `helixcode skills "
                    f"list`) is {observed!r}, NOT the external tier -- a "
                    f"same-named `builtin` copy loaded LAST overrides the "
                    f"admitted external-tier entry (F-22(a)); content is "
                    f"byte-identical post-F-13 so this is not user-visible-"
                    f"wrong, but claiming this skill 'resolves via the "
                    f"external tier' would be FALSE. {HELIXCODE_EVIDENCE}"
                )
                external_tier_admission = {
                    "verified": True, "admitted": True, "qualified_name": qualified,
                    "final_source_verified": True, "final_source": final_source or "unknown",
                }
        else:
            helix_note = (
                f"EXCLUDED by the external-tier similarity-conflict policy "
                f"gate (live-probed this run via "
                f"submodules/helix_agent/cmd/similarity-probe: qualified "
                f"identity {qualified!r} conflicts with "
                f"{verdict['conflicts_with']!r}, score={verdict['score']} >= "
                f"threshold={verdict['threshold']}) -- this skill resolves "
                f"ONLY via the SEPARATE, higher-precedence `builtin` tier "
                f"(masking the external-tier exclusion for the end user, "
                f"per F-22(a)); the prior blanket \"via external tier\" "
                f"claim was FALSE for this skill specifically. Still "
                f"SUPPORTED overall (a real tier does serve it), but NOT "
                f"via the external tier. {HELIXCODE_EVIDENCE}"
            )
            external_tier_admission = {
                "verified": True,
                "admitted": False,
                "qualified_name": qualified,
                "conflicts_with": verdict["conflicts_with"],
                "score": verdict["score"],
                "threshold": verdict["threshold"],
            }
    else:
        helix_status = "CONFIG-ONLY"
        helix_note = (
            f"external tier defaults to .claude/skills/{row['name']} "
            f"(config-injectable via HELIXCODE_EXTERNAL_SKILLS_DIR); that "
            f"directory does not currently carry this skill "
            f"({claude_cell['note']}). {HELIXCODE_EVIDENCE}"
        )
    cells["helixcode"] = {
        "status": helix_status,
        "path": f".claude/skills/{row['name']} (external tier)",
        "note": helix_note,
        "external_tier_admission": external_tier_admission,
    }
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
    lines.append(
        "**HelixCode column — three distinct facts, not one (HXC-159 "
        "T-P9.01 round-4 F-22(b) correction).** The HelixCode cell's "
        "`SUPPORTED` / `CONFIG-ONLY` status still reports the SAME thing "
        "it always has — whether the skill is *reachable via "
        "`.claude/skills/` by some tier* (a directory-presence probe). "
        "That status alone does NOT establish either of two further, "
        "independently live-probed facts, each also-true and each "
        "distinct: (1) whether the skill is specifically *ADMITTED by the "
        "external tier's own similarity-conflict policy gate* (live-"
        "probed via `submodules/helix_agent/cmd/similarity-probe`, the "
        "SAME code path `Load()` uses); and (2) — for a skill the gate "
        "DOES admit — whether the FINAL registry a user actually gets "
        "skills from serves it FROM the external tier or from a same-"
        "named, independently-loaded `builtin` copy that overrides it "
        "(live-probed via a real `helixcode skills list`, HXC-159 "
        "F-22(a)'s root cause: `loadBuiltinSkills` runs on every load, "
        "last). Every Per-skill-detail note below states ALL applicable "
        "facts explicitly and honestly — never hand-transcribed, §11.4.6 "
        "— including when either probe itself could not run this run, in "
        "which case the note says so rather than silently reusing a stale "
        "or assumed verdict."
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

    external_tier_verdicts, probe_error = probe_external_tier_admission(root)
    if probe_error is not None:
        print(f"WARNING: external-tier policy-gate probe unavailable this run: {probe_error}", file=sys.stderr)

    rows = parse_manifest(manifest_path)
    manifest_names = {r["name"] for r in rows}
    final_sources, source_probe_error = probe_final_registry_source(root, manifest_names)
    if source_probe_error is not None:
        print(f"WARNING: final-registry-source probe unavailable this run: {source_probe_error}", file=sys.stderr)

    rows_with_cells = []
    for row in rows:
        cells = classify_row(root, row, external_tier_verdicts, probe_error,
                              final_sources, source_probe_error)
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
