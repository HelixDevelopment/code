# HelixCode Extensions Catalog

| | |
|---|---|
| Revision | 1 |
| Created | 2026-09-23 |
| Last modified | 2026-09-23T00:35:58Z |
| Status | active |
| Status summary | — |
| Continuation | docs/CONTINUATION.md |

Per-§11.4.228 (Cross-agent extension lifecycle mandate) catalog of EVERY skill registered under `scripts/skills_manifest.tsv` — the single T-P1.04.2 manifest `scripts/register_skills.sh` provisions from — with a per-platform compatibility matrix across the five governed CLI-agent platforms this constitution names as its minimum: Claude Code, OpenCode, HelixCode, Gemini CLI, Qwen Code. Generated FROM the manifest by `helix_code/scripts/extensions_catalog/generate_catalog.py` — **never hand-transcribed** — with a real, live filesystem probe per skill × platform cell, never a blank cell (§11.4.201(6)).

**Closed status vocabulary** (§11.4.228(A)(1)): `SUPPORTED` (verified working, real per-platform liveness probe this run) / `CONFIG-ONLY` (declared — this platform HAS a registrar targeting it — but not currently wired) / `UNTESTED` (this platform has NO registrar/wiring mechanism reaching this skill at all — an honest §11.4.197-tracked gap) / `UNSUPPORTED` (proven structurally impossible per §11.4.112 — unused by this generator; no platform here has been proven incapable of loading a Markdown-frontmatter skill).

## Table of contents

- [Compatibility matrix](#compatibility-matrix)
- [Per-skill detail](#per-skill-detail)
- [Source tracking](#source-tracking)

## Compatibility matrix

| Skill | Claude Code | OpenCode | HelixCode | Gemini CLI | Qwen Code |
|---|---|---|---|---|---|
| `action-prefix-system` | SUPPORTED (`.claude/skills/action-prefix-system`) | UNTESTED (`N/A`) | SUPPORTED (`.claude/skills/action-prefix-system (external tier)`) | UNTESTED (`N/A`) | UNTESTED (`N/A`) |
| `media-validator` | CONFIG-ONLY (`.claude/skills/media-validator`) | UNTESTED (`N/A`) | CONFIG-ONLY (`.claude/skills/media-validator (external tier)`) | UNTESTED (`N/A`) | UNTESTED (`N/A`) |
| `multitrack` | CONFIG-ONLY (`.claude/skills/multitrack`) | UNTESTED (`N/A`) | CONFIG-ONLY (`.claude/skills/multitrack (external tier)`) | UNTESTED (`N/A`) | UNTESTED (`N/A`) |
| `reporting-workable-items` | CONFIG-ONLY (`.claude/skills/reporting-workable-items`) | UNTESTED (`N/A`) | CONFIG-ONLY (`.claude/skills/reporting-workable-items (external tier)`) | UNTESTED (`N/A`) | UNTESTED (`N/A`) |
| `scheduled-work-queue` | CONFIG-ONLY (`.claude/skills/scheduled-work-queue`) | UNTESTED (`N/A`) | CONFIG-ONLY (`.claude/skills/scheduled-work-queue (external tier)`) | UNTESTED (`N/A`) | UNTESTED (`N/A`) |
| `session-sync` | CONFIG-ONLY (`.claude/skills/session-sync`) | UNTESTED (`N/A`) | CONFIG-ONLY (`.claude/skills/session-sync (external tier)`) | UNTESTED (`N/A`) | UNTESTED (`N/A`) |
| `skill-catalog` | SUPPORTED (`.claude/skills/skill-catalog`) | UNTESTED (`N/A`) | SUPPORTED (`.claude/skills/skill-catalog (external tier)`) | UNTESTED (`N/A`) | UNTESTED (`N/A`) |
| `workable-item-lifecycle` | SUPPORTED (`.claude/skills/workable-item-lifecycle`) | UNTESTED (`N/A`) | SUPPORTED (`.claude/skills/workable-item-lifecycle (external tier)`) | UNTESTED (`N/A`) | UNTESTED (`N/A`) |

## Per-skill detail

### `action-prefix-system`

- **Owner (registrar):** `constitution`
- **Manifest source:** `constitution/skills/action-prefix-system`

| Platform | Status | Loading path | Evidence / Note |
|---|---|---|---|
| Claude Code | SUPPORTED | `.claude/skills/action-prefix-system` | symlink, SKILL.md read 6515 bytes (resolves to the manifest-declared source). Live-probed 2026-09-23T00:35:58Z. |
| OpenCode | UNTESTED | `N/A` | No .opencode/skills/ directory exists in this checkout and no registrar (scripts/register_skills.sh only ever writes .claude/skills/) targets one — zero recorded loading path this cycle, tracked per §11.4.197. .opencode/ here carries only SpecKit *commands* (.opencode/commands/), not skills. |
| HelixCode | SUPPORTED | `.claude/skills/action-prefix-system (external tier)` | via external tier -> .claude/skills/action-prefix-system. helix_code/internal/agent/skill_dirs.go ExternalSkillsDir() defaults to '.claude/skills' (config-injectable via HELIXCODE_EXTERNAL_SKILLS_DIR); the external tier's end-to-end load+list+invoke path is REAL-BINARY verified at qa-results/hxc159/helixcode_wire/20260922T190852Z/ (green_cli_e2e.txt). Honest boundary (§11.4.6, T-P5.05.4 in specs/001-helixskills-incorporation/tasks.md): the per-skill activation-allowlist/ceiling mechanism was NOT verifiable from within helix_code/'s own file scope this cycle — grep-confirmed zero Go implementation of a per-skill activation ceiling in this module. |
| Gemini CLI | UNTESTED | `N/A` | No .gemini/skills/ directory exists in this checkout (only .gemini/commands/, unrelated to the Agent-Skill format) and no registrar targets one — zero recorded loading path this cycle, tracked per §11.4.197 (the exact G1 gap the constitution's own §11.4.228 forensic anchor names for this platform). |
| Qwen Code | UNTESTED | `N/A` | No .qwen/skills/ directory exists in this checkout (only .qwen/commands/ + .qwen/settings.json, unrelated to the Agent-Skill format) and no registrar targets one — zero recorded loading path this cycle, tracked per §11.4.197 (the exact G1 gap the constitution's own §11.4.228 forensic anchor names for this platform). |

### `media-validator`

- **Owner (registrar):** `constitution`
- **Manifest source:** `constitution/skills/media-validator`

| Platform | Status | Loading path | Evidence / Note |
|---|---|---|---|
| Claude Code | CONFIG-ONLY | `.claude/skills/media-validator` | declared in scripts/skills_manifest.tsv; not present at .claude/skills/media-validator this run (live probe, 2026-09-23T00:35:58Z). |
| OpenCode | UNTESTED | `N/A` | No .opencode/skills/ directory exists in this checkout and no registrar (scripts/register_skills.sh only ever writes .claude/skills/) targets one — zero recorded loading path this cycle, tracked per §11.4.197. .opencode/ here carries only SpecKit *commands* (.opencode/commands/), not skills. |
| HelixCode | CONFIG-ONLY | `.claude/skills/media-validator (external tier)` | external tier defaults to .claude/skills/media-validator (config-injectable via HELIXCODE_EXTERNAL_SKILLS_DIR); that directory does not currently carry this skill (declared in scripts/skills_manifest.tsv; not present at .claude/skills/media-validator this run (live probe, 2026-09-23T00:35:58Z).). helix_code/internal/agent/skill_dirs.go ExternalSkillsDir() defaults to '.claude/skills' (config-injectable via HELIXCODE_EXTERNAL_SKILLS_DIR); the external tier's end-to-end load+list+invoke path is REAL-BINARY verified at qa-results/hxc159/helixcode_wire/20260922T190852Z/ (green_cli_e2e.txt). Honest boundary (§11.4.6, T-P5.05.4 in specs/001-helixskills-incorporation/tasks.md): the per-skill activation-allowlist/ceiling mechanism was NOT verifiable from within helix_code/'s own file scope this cycle — grep-confirmed zero Go implementation of a per-skill activation ceiling in this module. |
| Gemini CLI | UNTESTED | `N/A` | No .gemini/skills/ directory exists in this checkout (only .gemini/commands/, unrelated to the Agent-Skill format) and no registrar targets one — zero recorded loading path this cycle, tracked per §11.4.197 (the exact G1 gap the constitution's own §11.4.228 forensic anchor names for this platform). |
| Qwen Code | UNTESTED | `N/A` | No .qwen/skills/ directory exists in this checkout (only .qwen/commands/ + .qwen/settings.json, unrelated to the Agent-Skill format) and no registrar targets one — zero recorded loading path this cycle, tracked per §11.4.197 (the exact G1 gap the constitution's own §11.4.228 forensic anchor names for this platform). |

### `multitrack`

- **Owner (registrar):** `constitution`
- **Manifest source:** `constitution/skills/multitrack`

| Platform | Status | Loading path | Evidence / Note |
|---|---|---|---|
| Claude Code | CONFIG-ONLY | `.claude/skills/multitrack` | declared in scripts/skills_manifest.tsv; not present at .claude/skills/multitrack this run (live probe, 2026-09-23T00:35:58Z). |
| OpenCode | UNTESTED | `N/A` | No .opencode/skills/ directory exists in this checkout and no registrar (scripts/register_skills.sh only ever writes .claude/skills/) targets one — zero recorded loading path this cycle, tracked per §11.4.197. .opencode/ here carries only SpecKit *commands* (.opencode/commands/), not skills. |
| HelixCode | CONFIG-ONLY | `.claude/skills/multitrack (external tier)` | external tier defaults to .claude/skills/multitrack (config-injectable via HELIXCODE_EXTERNAL_SKILLS_DIR); that directory does not currently carry this skill (declared in scripts/skills_manifest.tsv; not present at .claude/skills/multitrack this run (live probe, 2026-09-23T00:35:58Z).). helix_code/internal/agent/skill_dirs.go ExternalSkillsDir() defaults to '.claude/skills' (config-injectable via HELIXCODE_EXTERNAL_SKILLS_DIR); the external tier's end-to-end load+list+invoke path is REAL-BINARY verified at qa-results/hxc159/helixcode_wire/20260922T190852Z/ (green_cli_e2e.txt). Honest boundary (§11.4.6, T-P5.05.4 in specs/001-helixskills-incorporation/tasks.md): the per-skill activation-allowlist/ceiling mechanism was NOT verifiable from within helix_code/'s own file scope this cycle — grep-confirmed zero Go implementation of a per-skill activation ceiling in this module. |
| Gemini CLI | UNTESTED | `N/A` | No .gemini/skills/ directory exists in this checkout (only .gemini/commands/, unrelated to the Agent-Skill format) and no registrar targets one — zero recorded loading path this cycle, tracked per §11.4.197 (the exact G1 gap the constitution's own §11.4.228 forensic anchor names for this platform). |
| Qwen Code | UNTESTED | `N/A` | No .qwen/skills/ directory exists in this checkout (only .qwen/commands/ + .qwen/settings.json, unrelated to the Agent-Skill format) and no registrar targets one — zero recorded loading path this cycle, tracked per §11.4.197 (the exact G1 gap the constitution's own §11.4.228 forensic anchor names for this platform). |

### `reporting-workable-items`

- **Owner (registrar):** `constitution`
- **Manifest source:** `constitution/skills/reporting-workable-items`

| Platform | Status | Loading path | Evidence / Note |
|---|---|---|---|
| Claude Code | CONFIG-ONLY | `.claude/skills/reporting-workable-items` | declared in scripts/skills_manifest.tsv; not present at .claude/skills/reporting-workable-items this run (live probe, 2026-09-23T00:35:58Z). |
| OpenCode | UNTESTED | `N/A` | No .opencode/skills/ directory exists in this checkout and no registrar (scripts/register_skills.sh only ever writes .claude/skills/) targets one — zero recorded loading path this cycle, tracked per §11.4.197. .opencode/ here carries only SpecKit *commands* (.opencode/commands/), not skills. |
| HelixCode | CONFIG-ONLY | `.claude/skills/reporting-workable-items (external tier)` | external tier defaults to .claude/skills/reporting-workable-items (config-injectable via HELIXCODE_EXTERNAL_SKILLS_DIR); that directory does not currently carry this skill (declared in scripts/skills_manifest.tsv; not present at .claude/skills/reporting-workable-items this run (live probe, 2026-09-23T00:35:58Z).). helix_code/internal/agent/skill_dirs.go ExternalSkillsDir() defaults to '.claude/skills' (config-injectable via HELIXCODE_EXTERNAL_SKILLS_DIR); the external tier's end-to-end load+list+invoke path is REAL-BINARY verified at qa-results/hxc159/helixcode_wire/20260922T190852Z/ (green_cli_e2e.txt). Honest boundary (§11.4.6, T-P5.05.4 in specs/001-helixskills-incorporation/tasks.md): the per-skill activation-allowlist/ceiling mechanism was NOT verifiable from within helix_code/'s own file scope this cycle — grep-confirmed zero Go implementation of a per-skill activation ceiling in this module. |
| Gemini CLI | UNTESTED | `N/A` | No .gemini/skills/ directory exists in this checkout (only .gemini/commands/, unrelated to the Agent-Skill format) and no registrar targets one — zero recorded loading path this cycle, tracked per §11.4.197 (the exact G1 gap the constitution's own §11.4.228 forensic anchor names for this platform). |
| Qwen Code | UNTESTED | `N/A` | No .qwen/skills/ directory exists in this checkout (only .qwen/commands/ + .qwen/settings.json, unrelated to the Agent-Skill format) and no registrar targets one — zero recorded loading path this cycle, tracked per §11.4.197 (the exact G1 gap the constitution's own §11.4.228 forensic anchor names for this platform). |

### `scheduled-work-queue`

- **Owner (registrar):** `constitution`
- **Manifest source:** `constitution/skills/scheduled-work-queue`

| Platform | Status | Loading path | Evidence / Note |
|---|---|---|---|
| Claude Code | CONFIG-ONLY | `.claude/skills/scheduled-work-queue` | declared in scripts/skills_manifest.tsv; not present at .claude/skills/scheduled-work-queue this run (live probe, 2026-09-23T00:35:58Z). |
| OpenCode | UNTESTED | `N/A` | No .opencode/skills/ directory exists in this checkout and no registrar (scripts/register_skills.sh only ever writes .claude/skills/) targets one — zero recorded loading path this cycle, tracked per §11.4.197. .opencode/ here carries only SpecKit *commands* (.opencode/commands/), not skills. |
| HelixCode | CONFIG-ONLY | `.claude/skills/scheduled-work-queue (external tier)` | external tier defaults to .claude/skills/scheduled-work-queue (config-injectable via HELIXCODE_EXTERNAL_SKILLS_DIR); that directory does not currently carry this skill (declared in scripts/skills_manifest.tsv; not present at .claude/skills/scheduled-work-queue this run (live probe, 2026-09-23T00:35:58Z).). helix_code/internal/agent/skill_dirs.go ExternalSkillsDir() defaults to '.claude/skills' (config-injectable via HELIXCODE_EXTERNAL_SKILLS_DIR); the external tier's end-to-end load+list+invoke path is REAL-BINARY verified at qa-results/hxc159/helixcode_wire/20260922T190852Z/ (green_cli_e2e.txt). Honest boundary (§11.4.6, T-P5.05.4 in specs/001-helixskills-incorporation/tasks.md): the per-skill activation-allowlist/ceiling mechanism was NOT verifiable from within helix_code/'s own file scope this cycle — grep-confirmed zero Go implementation of a per-skill activation ceiling in this module. |
| Gemini CLI | UNTESTED | `N/A` | No .gemini/skills/ directory exists in this checkout (only .gemini/commands/, unrelated to the Agent-Skill format) and no registrar targets one — zero recorded loading path this cycle, tracked per §11.4.197 (the exact G1 gap the constitution's own §11.4.228 forensic anchor names for this platform). |
| Qwen Code | UNTESTED | `N/A` | No .qwen/skills/ directory exists in this checkout (only .qwen/commands/ + .qwen/settings.json, unrelated to the Agent-Skill format) and no registrar targets one — zero recorded loading path this cycle, tracked per §11.4.197 (the exact G1 gap the constitution's own §11.4.228 forensic anchor names for this platform). |

### `session-sync`

- **Owner (registrar):** `constitution`
- **Manifest source:** `constitution/skills/session-sync`

| Platform | Status | Loading path | Evidence / Note |
|---|---|---|---|
| Claude Code | CONFIG-ONLY | `.claude/skills/session-sync` | declared in scripts/skills_manifest.tsv; not present at .claude/skills/session-sync this run (live probe, 2026-09-23T00:35:58Z). |
| OpenCode | UNTESTED | `N/A` | No .opencode/skills/ directory exists in this checkout and no registrar (scripts/register_skills.sh only ever writes .claude/skills/) targets one — zero recorded loading path this cycle, tracked per §11.4.197. .opencode/ here carries only SpecKit *commands* (.opencode/commands/), not skills. |
| HelixCode | CONFIG-ONLY | `.claude/skills/session-sync (external tier)` | external tier defaults to .claude/skills/session-sync (config-injectable via HELIXCODE_EXTERNAL_SKILLS_DIR); that directory does not currently carry this skill (declared in scripts/skills_manifest.tsv; not present at .claude/skills/session-sync this run (live probe, 2026-09-23T00:35:58Z).). helix_code/internal/agent/skill_dirs.go ExternalSkillsDir() defaults to '.claude/skills' (config-injectable via HELIXCODE_EXTERNAL_SKILLS_DIR); the external tier's end-to-end load+list+invoke path is REAL-BINARY verified at qa-results/hxc159/helixcode_wire/20260922T190852Z/ (green_cli_e2e.txt). Honest boundary (§11.4.6, T-P5.05.4 in specs/001-helixskills-incorporation/tasks.md): the per-skill activation-allowlist/ceiling mechanism was NOT verifiable from within helix_code/'s own file scope this cycle — grep-confirmed zero Go implementation of a per-skill activation ceiling in this module. |
| Gemini CLI | UNTESTED | `N/A` | No .gemini/skills/ directory exists in this checkout (only .gemini/commands/, unrelated to the Agent-Skill format) and no registrar targets one — zero recorded loading path this cycle, tracked per §11.4.197 (the exact G1 gap the constitution's own §11.4.228 forensic anchor names for this platform). |
| Qwen Code | UNTESTED | `N/A` | No .qwen/skills/ directory exists in this checkout (only .qwen/commands/ + .qwen/settings.json, unrelated to the Agent-Skill format) and no registrar targets one — zero recorded loading path this cycle, tracked per §11.4.197 (the exact G1 gap the constitution's own §11.4.228 forensic anchor names for this platform). |

### `skill-catalog`

- **Owner (registrar):** `constitution`
- **Manifest source:** `constitution/skills/skill-catalog`

| Platform | Status | Loading path | Evidence / Note |
|---|---|---|---|
| Claude Code | SUPPORTED | `.claude/skills/skill-catalog` | symlink, SKILL.md read 2328 bytes (resolves to the manifest-declared source). Live-probed 2026-09-23T00:35:58Z. |
| OpenCode | UNTESTED | `N/A` | No .opencode/skills/ directory exists in this checkout and no registrar (scripts/register_skills.sh only ever writes .claude/skills/) targets one — zero recorded loading path this cycle, tracked per §11.4.197. .opencode/ here carries only SpecKit *commands* (.opencode/commands/), not skills. |
| HelixCode | SUPPORTED | `.claude/skills/skill-catalog (external tier)` | via external tier -> .claude/skills/skill-catalog. helix_code/internal/agent/skill_dirs.go ExternalSkillsDir() defaults to '.claude/skills' (config-injectable via HELIXCODE_EXTERNAL_SKILLS_DIR); the external tier's end-to-end load+list+invoke path is REAL-BINARY verified at qa-results/hxc159/helixcode_wire/20260922T190852Z/ (green_cli_e2e.txt). Honest boundary (§11.4.6, T-P5.05.4 in specs/001-helixskills-incorporation/tasks.md): the per-skill activation-allowlist/ceiling mechanism was NOT verifiable from within helix_code/'s own file scope this cycle — grep-confirmed zero Go implementation of a per-skill activation ceiling in this module. |
| Gemini CLI | UNTESTED | `N/A` | No .gemini/skills/ directory exists in this checkout (only .gemini/commands/, unrelated to the Agent-Skill format) and no registrar targets one — zero recorded loading path this cycle, tracked per §11.4.197 (the exact G1 gap the constitution's own §11.4.228 forensic anchor names for this platform). |
| Qwen Code | UNTESTED | `N/A` | No .qwen/skills/ directory exists in this checkout (only .qwen/commands/ + .qwen/settings.json, unrelated to the Agent-Skill format) and no registrar targets one — zero recorded loading path this cycle, tracked per §11.4.197 (the exact G1 gap the constitution's own §11.4.228 forensic anchor names for this platform). |

### `workable-item-lifecycle`

- **Owner (registrar):** `constitution`
- **Manifest source:** `constitution/skills/workable-item-lifecycle`

| Platform | Status | Loading path | Evidence / Note |
|---|---|---|---|
| Claude Code | SUPPORTED | `.claude/skills/workable-item-lifecycle` | symlink, SKILL.md read 4648 bytes (resolves to the manifest-declared source). Live-probed 2026-09-23T00:35:58Z. |
| OpenCode | UNTESTED | `N/A` | No .opencode/skills/ directory exists in this checkout and no registrar (scripts/register_skills.sh only ever writes .claude/skills/) targets one — zero recorded loading path this cycle, tracked per §11.4.197. .opencode/ here carries only SpecKit *commands* (.opencode/commands/), not skills. |
| HelixCode | SUPPORTED | `.claude/skills/workable-item-lifecycle (external tier)` | via external tier -> .claude/skills/workable-item-lifecycle. helix_code/internal/agent/skill_dirs.go ExternalSkillsDir() defaults to '.claude/skills' (config-injectable via HELIXCODE_EXTERNAL_SKILLS_DIR); the external tier's end-to-end load+list+invoke path is REAL-BINARY verified at qa-results/hxc159/helixcode_wire/20260922T190852Z/ (green_cli_e2e.txt). Honest boundary (§11.4.6, T-P5.05.4 in specs/001-helixskills-incorporation/tasks.md): the per-skill activation-allowlist/ceiling mechanism was NOT verifiable from within helix_code/'s own file scope this cycle — grep-confirmed zero Go implementation of a per-skill activation ceiling in this module. |
| Gemini CLI | UNTESTED | `N/A` | No .gemini/skills/ directory exists in this checkout (only .gemini/commands/, unrelated to the Agent-Skill format) and no registrar targets one — zero recorded loading path this cycle, tracked per §11.4.197 (the exact G1 gap the constitution's own §11.4.228 forensic anchor names for this platform). |
| Qwen Code | UNTESTED | `N/A` | No .qwen/skills/ directory exists in this checkout (only .qwen/commands/ + .qwen/settings.json, unrelated to the Agent-Skill format) and no registrar targets one — zero recorded loading path this cycle, tracked per §11.4.197 (the exact G1 gap the constitution's own §11.4.228 forensic anchor names for this platform). |

## Source tracking

Per §11.4.228(B): every extension's canonical source is machine-readable via `scripts/skills_manifest.tsv` itself (name, repo-relative source directory, owning registrar) — the manifest row IS this catalogue's `EXTENSION_SOURCE` record for every entry here (no separate `EXTENSION_SOURCE.yaml` per skill; the manifest already carries the same three fields at repo scope, avoiding a duplicated, driftable second record per §11.4.106). All eight entries are constitution-owned (`owner=constitution`) and SYMLINKED (never copied) from `constitution/skills/<name>/` into `.claude/skills/<name>` by `scripts/register_skills.sh` — the preferred-over-copies provenance §11.4.228(B)(2) requires.

Source manifest: `/home/milosvasic/Projects/helix_code/scripts/skills_manifest.tsv` (8 rows).

