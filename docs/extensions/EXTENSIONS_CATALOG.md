# HelixCode Extensions Catalog

| | |
|---|---|
| Revision | 1 |
| Created | 2026-09-23 |
| Last modified | 2026-09-23T09:34:18Z |
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
| `media-validator` | SUPPORTED (`.claude/skills/media-validator`) | UNTESTED (`N/A`) | SUPPORTED (`.claude/skills/media-validator (external tier)`) | UNTESTED (`N/A`) | UNTESTED (`N/A`) |
| `multitrack` | SUPPORTED (`.claude/skills/multitrack`) | UNTESTED (`N/A`) | SUPPORTED (`.claude/skills/multitrack (external tier)`) | UNTESTED (`N/A`) | UNTESTED (`N/A`) |
| `reporting-workable-items` | SUPPORTED (`.claude/skills/reporting-workable-items`) | UNTESTED (`N/A`) | SUPPORTED (`.claude/skills/reporting-workable-items (external tier)`) | UNTESTED (`N/A`) | UNTESTED (`N/A`) |
| `scheduled-work-queue` | SUPPORTED (`.claude/skills/scheduled-work-queue`) | UNTESTED (`N/A`) | SUPPORTED (`.claude/skills/scheduled-work-queue (external tier)`) | UNTESTED (`N/A`) | UNTESTED (`N/A`) |
| `session-sync` | SUPPORTED (`.claude/skills/session-sync`) | UNTESTED (`N/A`) | SUPPORTED (`.claude/skills/session-sync (external tier)`) | UNTESTED (`N/A`) | UNTESTED (`N/A`) |
| `skill-catalog` | SUPPORTED (`.claude/skills/skill-catalog`) | UNTESTED (`N/A`) | SUPPORTED (`.claude/skills/skill-catalog (external tier)`) | UNTESTED (`N/A`) | UNTESTED (`N/A`) |
| `workable-item-lifecycle` | SUPPORTED (`.claude/skills/workable-item-lifecycle`) | UNTESTED (`N/A`) | SUPPORTED (`.claude/skills/workable-item-lifecycle (external tier)`) | UNTESTED (`N/A`) | UNTESTED (`N/A`) |

## Per-skill detail

### `action-prefix-system`

- **Owner (registrar):** `constitution`
- **Manifest source:** `constitution/skills/action-prefix-system`

| Platform | Status | Loading path | Evidence / Note |
|---|---|---|---|
| Claude Code | SUPPORTED | `.claude/skills/action-prefix-system` | symlink, SKILL.md read 6515 bytes (resolves to the manifest-declared source). Live-probed 2026-09-23T09:34:18Z. |
| OpenCode | UNTESTED | `N/A` | No .opencode/skills/ directory exists in this checkout and no registrar (scripts/register_skills.sh only ever writes .claude/skills/) targets one — zero recorded loading path this cycle, tracked per §11.4.197. .opencode/ here carries only SpecKit *commands* (.opencode/commands/), not skills. |
| HelixCode | SUPPORTED | `.claude/skills/action-prefix-system (external tier)` | via external tier -> .claude/skills/action-prefix-system. helix_code/internal/agent/skill_dirs.go ExternalSkillsDir() defaults to '.claude/skills' (config-injectable via HELIXCODE_EXTERNAL_SKILLS_DIR); the external tier's LOADING MECHANISM (a single fixture skill, no similarity conflict) is REAL-BINARY verified at qa-results/hxc159/helixcode_wire/20260922T190852Z/ (green_cli_e2e.txt). HXC-159 F-17 (2026-09-23): the shared SimilarityThreshold was recalibrated 0.0256->0.0709 against the real, frontmatter-complete corpus-1 (see docs/qa/hxc159_f17_remediation_20260923T0921Z/threshold.json); the REAL 8-9-skill corpus-1 tier is nonetheless STILL policy-refused at BOTH consumers on a genuine confusable pair (action-prefix-system \|\| reporting-workable-items, score 0.1045 -- real content overlap, not a miscalibration), captured at docs/qa/hxc159_f17_remediation_20260923T0921Z/helixcode_recalibrated_run.txt and the mirrored helix_agent_native_recalibrated_run.txt. This SUPPORTED cell reports per-skill FILE PRESENCE at the platform directory (§11.4.228(A)(1)'s liveness probe), not whole-tier ACTIVATION -- a distinction this catalogue's generator does not currently classify separately (tracked per §11.4.197, see the F-17 subagent's final report for the residual-finding discussion). Honest boundary (§11.4.6, T-P5.05.4 in specs/001-helixskills-incorporation/tasks.md): the per-skill activation-allowlist/ceiling mechanism was NOT verifiable from within helix_code/'s own file scope this cycle — grep-confirmed zero Go implementation of a per-skill activation ceiling in this module. |
| Gemini CLI | UNTESTED | `N/A` | No .gemini/skills/ directory exists in this checkout (only .gemini/commands/, unrelated to the Agent-Skill format) and no registrar targets one — zero recorded loading path this cycle, tracked per §11.4.197 (the exact G1 gap the constitution's own §11.4.228 forensic anchor names for this platform). |
| Qwen Code | UNTESTED | `N/A` | No .qwen/skills/ directory exists in this checkout (only .qwen/commands/ + .qwen/settings.json, unrelated to the Agent-Skill format) and no registrar targets one — zero recorded loading path this cycle, tracked per §11.4.197 (the exact G1 gap the constitution's own §11.4.228 forensic anchor names for this platform). |

### `media-validator`

- **Owner (registrar):** `constitution`
- **Manifest source:** `constitution/skills/media-validator`

| Platform | Status | Loading path | Evidence / Note |
|---|---|---|---|
| Claude Code | SUPPORTED | `.claude/skills/media-validator` | symlink, SKILL.md read 1536 bytes (resolves to the manifest-declared source). Live-probed 2026-09-23T09:34:18Z. |
| OpenCode | UNTESTED | `N/A` | No .opencode/skills/ directory exists in this checkout and no registrar (scripts/register_skills.sh only ever writes .claude/skills/) targets one — zero recorded loading path this cycle, tracked per §11.4.197. .opencode/ here carries only SpecKit *commands* (.opencode/commands/), not skills. |
| HelixCode | SUPPORTED | `.claude/skills/media-validator (external tier)` | via external tier -> .claude/skills/media-validator. helix_code/internal/agent/skill_dirs.go ExternalSkillsDir() defaults to '.claude/skills' (config-injectable via HELIXCODE_EXTERNAL_SKILLS_DIR); the external tier's LOADING MECHANISM (a single fixture skill, no similarity conflict) is REAL-BINARY verified at qa-results/hxc159/helixcode_wire/20260922T190852Z/ (green_cli_e2e.txt). HXC-159 F-17 (2026-09-23): the shared SimilarityThreshold was recalibrated 0.0256->0.0709 against the real, frontmatter-complete corpus-1 (see docs/qa/hxc159_f17_remediation_20260923T0921Z/threshold.json); the REAL 8-9-skill corpus-1 tier is nonetheless STILL policy-refused at BOTH consumers on a genuine confusable pair (action-prefix-system \|\| reporting-workable-items, score 0.1045 -- real content overlap, not a miscalibration), captured at docs/qa/hxc159_f17_remediation_20260923T0921Z/helixcode_recalibrated_run.txt and the mirrored helix_agent_native_recalibrated_run.txt. This SUPPORTED cell reports per-skill FILE PRESENCE at the platform directory (§11.4.228(A)(1)'s liveness probe), not whole-tier ACTIVATION -- a distinction this catalogue's generator does not currently classify separately (tracked per §11.4.197, see the F-17 subagent's final report for the residual-finding discussion). Honest boundary (§11.4.6, T-P5.05.4 in specs/001-helixskills-incorporation/tasks.md): the per-skill activation-allowlist/ceiling mechanism was NOT verifiable from within helix_code/'s own file scope this cycle — grep-confirmed zero Go implementation of a per-skill activation ceiling in this module. |
| Gemini CLI | UNTESTED | `N/A` | No .gemini/skills/ directory exists in this checkout (only .gemini/commands/, unrelated to the Agent-Skill format) and no registrar targets one — zero recorded loading path this cycle, tracked per §11.4.197 (the exact G1 gap the constitution's own §11.4.228 forensic anchor names for this platform). |
| Qwen Code | UNTESTED | `N/A` | No .qwen/skills/ directory exists in this checkout (only .qwen/commands/ + .qwen/settings.json, unrelated to the Agent-Skill format) and no registrar targets one — zero recorded loading path this cycle, tracked per §11.4.197 (the exact G1 gap the constitution's own §11.4.228 forensic anchor names for this platform). |

### `multitrack`

- **Owner (registrar):** `constitution`
- **Manifest source:** `constitution/skills/multitrack`

| Platform | Status | Loading path | Evidence / Note |
|---|---|---|---|
| Claude Code | SUPPORTED | `.claude/skills/multitrack` | symlink, SKILL.md read 1965 bytes (resolves to the manifest-declared source). Live-probed 2026-09-23T09:34:18Z. |
| OpenCode | UNTESTED | `N/A` | No .opencode/skills/ directory exists in this checkout and no registrar (scripts/register_skills.sh only ever writes .claude/skills/) targets one — zero recorded loading path this cycle, tracked per §11.4.197. .opencode/ here carries only SpecKit *commands* (.opencode/commands/), not skills. |
| HelixCode | SUPPORTED | `.claude/skills/multitrack (external tier)` | via external tier -> .claude/skills/multitrack. helix_code/internal/agent/skill_dirs.go ExternalSkillsDir() defaults to '.claude/skills' (config-injectable via HELIXCODE_EXTERNAL_SKILLS_DIR); the external tier's LOADING MECHANISM (a single fixture skill, no similarity conflict) is REAL-BINARY verified at qa-results/hxc159/helixcode_wire/20260922T190852Z/ (green_cli_e2e.txt). HXC-159 F-17 (2026-09-23): the shared SimilarityThreshold was recalibrated 0.0256->0.0709 against the real, frontmatter-complete corpus-1 (see docs/qa/hxc159_f17_remediation_20260923T0921Z/threshold.json); the REAL 8-9-skill corpus-1 tier is nonetheless STILL policy-refused at BOTH consumers on a genuine confusable pair (action-prefix-system \|\| reporting-workable-items, score 0.1045 -- real content overlap, not a miscalibration), captured at docs/qa/hxc159_f17_remediation_20260923T0921Z/helixcode_recalibrated_run.txt and the mirrored helix_agent_native_recalibrated_run.txt. This SUPPORTED cell reports per-skill FILE PRESENCE at the platform directory (§11.4.228(A)(1)'s liveness probe), not whole-tier ACTIVATION -- a distinction this catalogue's generator does not currently classify separately (tracked per §11.4.197, see the F-17 subagent's final report for the residual-finding discussion). Honest boundary (§11.4.6, T-P5.05.4 in specs/001-helixskills-incorporation/tasks.md): the per-skill activation-allowlist/ceiling mechanism was NOT verifiable from within helix_code/'s own file scope this cycle — grep-confirmed zero Go implementation of a per-skill activation ceiling in this module. |
| Gemini CLI | UNTESTED | `N/A` | No .gemini/skills/ directory exists in this checkout (only .gemini/commands/, unrelated to the Agent-Skill format) and no registrar targets one — zero recorded loading path this cycle, tracked per §11.4.197 (the exact G1 gap the constitution's own §11.4.228 forensic anchor names for this platform). |
| Qwen Code | UNTESTED | `N/A` | No .qwen/skills/ directory exists in this checkout (only .qwen/commands/ + .qwen/settings.json, unrelated to the Agent-Skill format) and no registrar targets one — zero recorded loading path this cycle, tracked per §11.4.197 (the exact G1 gap the constitution's own §11.4.228 forensic anchor names for this platform). |

### `reporting-workable-items`

- **Owner (registrar):** `constitution`
- **Manifest source:** `constitution/skills/reporting-workable-items`

| Platform | Status | Loading path | Evidence / Note |
|---|---|---|---|
| Claude Code | SUPPORTED | `.claude/skills/reporting-workable-items` | symlink, SKILL.md read 5013 bytes (resolves to the manifest-declared source). Live-probed 2026-09-23T09:34:18Z. |
| OpenCode | UNTESTED | `N/A` | No .opencode/skills/ directory exists in this checkout and no registrar (scripts/register_skills.sh only ever writes .claude/skills/) targets one — zero recorded loading path this cycle, tracked per §11.4.197. .opencode/ here carries only SpecKit *commands* (.opencode/commands/), not skills. |
| HelixCode | SUPPORTED | `.claude/skills/reporting-workable-items (external tier)` | via external tier -> .claude/skills/reporting-workable-items. helix_code/internal/agent/skill_dirs.go ExternalSkillsDir() defaults to '.claude/skills' (config-injectable via HELIXCODE_EXTERNAL_SKILLS_DIR); the external tier's LOADING MECHANISM (a single fixture skill, no similarity conflict) is REAL-BINARY verified at qa-results/hxc159/helixcode_wire/20260922T190852Z/ (green_cli_e2e.txt). HXC-159 F-17 (2026-09-23): the shared SimilarityThreshold was recalibrated 0.0256->0.0709 against the real, frontmatter-complete corpus-1 (see docs/qa/hxc159_f17_remediation_20260923T0921Z/threshold.json); the REAL 8-9-skill corpus-1 tier is nonetheless STILL policy-refused at BOTH consumers on a genuine confusable pair (action-prefix-system \|\| reporting-workable-items, score 0.1045 -- real content overlap, not a miscalibration), captured at docs/qa/hxc159_f17_remediation_20260923T0921Z/helixcode_recalibrated_run.txt and the mirrored helix_agent_native_recalibrated_run.txt. This SUPPORTED cell reports per-skill FILE PRESENCE at the platform directory (§11.4.228(A)(1)'s liveness probe), not whole-tier ACTIVATION -- a distinction this catalogue's generator does not currently classify separately (tracked per §11.4.197, see the F-17 subagent's final report for the residual-finding discussion). Honest boundary (§11.4.6, T-P5.05.4 in specs/001-helixskills-incorporation/tasks.md): the per-skill activation-allowlist/ceiling mechanism was NOT verifiable from within helix_code/'s own file scope this cycle — grep-confirmed zero Go implementation of a per-skill activation ceiling in this module. |
| Gemini CLI | UNTESTED | `N/A` | No .gemini/skills/ directory exists in this checkout (only .gemini/commands/, unrelated to the Agent-Skill format) and no registrar targets one — zero recorded loading path this cycle, tracked per §11.4.197 (the exact G1 gap the constitution's own §11.4.228 forensic anchor names for this platform). |
| Qwen Code | UNTESTED | `N/A` | No .qwen/skills/ directory exists in this checkout (only .qwen/commands/ + .qwen/settings.json, unrelated to the Agent-Skill format) and no registrar targets one — zero recorded loading path this cycle, tracked per §11.4.197 (the exact G1 gap the constitution's own §11.4.228 forensic anchor names for this platform). |

### `scheduled-work-queue`

- **Owner (registrar):** `constitution`
- **Manifest source:** `constitution/skills/scheduled-work-queue`

| Platform | Status | Loading path | Evidence / Note |
|---|---|---|---|
| Claude Code | SUPPORTED | `.claude/skills/scheduled-work-queue` | symlink, SKILL.md read 3681 bytes (resolves to the manifest-declared source). Live-probed 2026-09-23T09:34:18Z. |
| OpenCode | UNTESTED | `N/A` | No .opencode/skills/ directory exists in this checkout and no registrar (scripts/register_skills.sh only ever writes .claude/skills/) targets one — zero recorded loading path this cycle, tracked per §11.4.197. .opencode/ here carries only SpecKit *commands* (.opencode/commands/), not skills. |
| HelixCode | SUPPORTED | `.claude/skills/scheduled-work-queue (external tier)` | via external tier -> .claude/skills/scheduled-work-queue. helix_code/internal/agent/skill_dirs.go ExternalSkillsDir() defaults to '.claude/skills' (config-injectable via HELIXCODE_EXTERNAL_SKILLS_DIR); the external tier's LOADING MECHANISM (a single fixture skill, no similarity conflict) is REAL-BINARY verified at qa-results/hxc159/helixcode_wire/20260922T190852Z/ (green_cli_e2e.txt). HXC-159 F-17 (2026-09-23): the shared SimilarityThreshold was recalibrated 0.0256->0.0709 against the real, frontmatter-complete corpus-1 (see docs/qa/hxc159_f17_remediation_20260923T0921Z/threshold.json); the REAL 8-9-skill corpus-1 tier is nonetheless STILL policy-refused at BOTH consumers on a genuine confusable pair (action-prefix-system \|\| reporting-workable-items, score 0.1045 -- real content overlap, not a miscalibration), captured at docs/qa/hxc159_f17_remediation_20260923T0921Z/helixcode_recalibrated_run.txt and the mirrored helix_agent_native_recalibrated_run.txt. This SUPPORTED cell reports per-skill FILE PRESENCE at the platform directory (§11.4.228(A)(1)'s liveness probe), not whole-tier ACTIVATION -- a distinction this catalogue's generator does not currently classify separately (tracked per §11.4.197, see the F-17 subagent's final report for the residual-finding discussion). Honest boundary (§11.4.6, T-P5.05.4 in specs/001-helixskills-incorporation/tasks.md): the per-skill activation-allowlist/ceiling mechanism was NOT verifiable from within helix_code/'s own file scope this cycle — grep-confirmed zero Go implementation of a per-skill activation ceiling in this module. |
| Gemini CLI | UNTESTED | `N/A` | No .gemini/skills/ directory exists in this checkout (only .gemini/commands/, unrelated to the Agent-Skill format) and no registrar targets one — zero recorded loading path this cycle, tracked per §11.4.197 (the exact G1 gap the constitution's own §11.4.228 forensic anchor names for this platform). |
| Qwen Code | UNTESTED | `N/A` | No .qwen/skills/ directory exists in this checkout (only .qwen/commands/ + .qwen/settings.json, unrelated to the Agent-Skill format) and no registrar targets one — zero recorded loading path this cycle, tracked per §11.4.197 (the exact G1 gap the constitution's own §11.4.228 forensic anchor names for this platform). |

### `session-sync`

- **Owner (registrar):** `constitution`
- **Manifest source:** `constitution/skills/session-sync`

| Platform | Status | Loading path | Evidence / Note |
|---|---|---|---|
| Claude Code | SUPPORTED | `.claude/skills/session-sync` | symlink, SKILL.md read 4860 bytes (resolves to the manifest-declared source). Live-probed 2026-09-23T09:34:18Z. |
| OpenCode | UNTESTED | `N/A` | No .opencode/skills/ directory exists in this checkout and no registrar (scripts/register_skills.sh only ever writes .claude/skills/) targets one — zero recorded loading path this cycle, tracked per §11.4.197. .opencode/ here carries only SpecKit *commands* (.opencode/commands/), not skills. |
| HelixCode | SUPPORTED | `.claude/skills/session-sync (external tier)` | via external tier -> .claude/skills/session-sync. helix_code/internal/agent/skill_dirs.go ExternalSkillsDir() defaults to '.claude/skills' (config-injectable via HELIXCODE_EXTERNAL_SKILLS_DIR); the external tier's LOADING MECHANISM (a single fixture skill, no similarity conflict) is REAL-BINARY verified at qa-results/hxc159/helixcode_wire/20260922T190852Z/ (green_cli_e2e.txt). HXC-159 F-17 (2026-09-23): the shared SimilarityThreshold was recalibrated 0.0256->0.0709 against the real, frontmatter-complete corpus-1 (see docs/qa/hxc159_f17_remediation_20260923T0921Z/threshold.json); the REAL 8-9-skill corpus-1 tier is nonetheless STILL policy-refused at BOTH consumers on a genuine confusable pair (action-prefix-system \|\| reporting-workable-items, score 0.1045 -- real content overlap, not a miscalibration), captured at docs/qa/hxc159_f17_remediation_20260923T0921Z/helixcode_recalibrated_run.txt and the mirrored helix_agent_native_recalibrated_run.txt. This SUPPORTED cell reports per-skill FILE PRESENCE at the platform directory (§11.4.228(A)(1)'s liveness probe), not whole-tier ACTIVATION -- a distinction this catalogue's generator does not currently classify separately (tracked per §11.4.197, see the F-17 subagent's final report for the residual-finding discussion). Honest boundary (§11.4.6, T-P5.05.4 in specs/001-helixskills-incorporation/tasks.md): the per-skill activation-allowlist/ceiling mechanism was NOT verifiable from within helix_code/'s own file scope this cycle — grep-confirmed zero Go implementation of a per-skill activation ceiling in this module. |
| Gemini CLI | UNTESTED | `N/A` | No .gemini/skills/ directory exists in this checkout (only .gemini/commands/, unrelated to the Agent-Skill format) and no registrar targets one — zero recorded loading path this cycle, tracked per §11.4.197 (the exact G1 gap the constitution's own §11.4.228 forensic anchor names for this platform). |
| Qwen Code | UNTESTED | `N/A` | No .qwen/skills/ directory exists in this checkout (only .qwen/commands/ + .qwen/settings.json, unrelated to the Agent-Skill format) and no registrar targets one — zero recorded loading path this cycle, tracked per §11.4.197 (the exact G1 gap the constitution's own §11.4.228 forensic anchor names for this platform). |

### `skill-catalog`

- **Owner (registrar):** `constitution`
- **Manifest source:** `constitution/skills/skill-catalog`

| Platform | Status | Loading path | Evidence / Note |
|---|---|---|---|
| Claude Code | SUPPORTED | `.claude/skills/skill-catalog` | symlink, SKILL.md read 2328 bytes (resolves to the manifest-declared source). Live-probed 2026-09-23T09:34:18Z. |
| OpenCode | UNTESTED | `N/A` | No .opencode/skills/ directory exists in this checkout and no registrar (scripts/register_skills.sh only ever writes .claude/skills/) targets one — zero recorded loading path this cycle, tracked per §11.4.197. .opencode/ here carries only SpecKit *commands* (.opencode/commands/), not skills. |
| HelixCode | SUPPORTED | `.claude/skills/skill-catalog (external tier)` | via external tier -> .claude/skills/skill-catalog. helix_code/internal/agent/skill_dirs.go ExternalSkillsDir() defaults to '.claude/skills' (config-injectable via HELIXCODE_EXTERNAL_SKILLS_DIR); the external tier's LOADING MECHANISM (a single fixture skill, no similarity conflict) is REAL-BINARY verified at qa-results/hxc159/helixcode_wire/20260922T190852Z/ (green_cli_e2e.txt). HXC-159 F-17 (2026-09-23): the shared SimilarityThreshold was recalibrated 0.0256->0.0709 against the real, frontmatter-complete corpus-1 (see docs/qa/hxc159_f17_remediation_20260923T0921Z/threshold.json); the REAL 8-9-skill corpus-1 tier is nonetheless STILL policy-refused at BOTH consumers on a genuine confusable pair (action-prefix-system \|\| reporting-workable-items, score 0.1045 -- real content overlap, not a miscalibration), captured at docs/qa/hxc159_f17_remediation_20260923T0921Z/helixcode_recalibrated_run.txt and the mirrored helix_agent_native_recalibrated_run.txt. This SUPPORTED cell reports per-skill FILE PRESENCE at the platform directory (§11.4.228(A)(1)'s liveness probe), not whole-tier ACTIVATION -- a distinction this catalogue's generator does not currently classify separately (tracked per §11.4.197, see the F-17 subagent's final report for the residual-finding discussion). Honest boundary (§11.4.6, T-P5.05.4 in specs/001-helixskills-incorporation/tasks.md): the per-skill activation-allowlist/ceiling mechanism was NOT verifiable from within helix_code/'s own file scope this cycle — grep-confirmed zero Go implementation of a per-skill activation ceiling in this module. |
| Gemini CLI | UNTESTED | `N/A` | No .gemini/skills/ directory exists in this checkout (only .gemini/commands/, unrelated to the Agent-Skill format) and no registrar targets one — zero recorded loading path this cycle, tracked per §11.4.197 (the exact G1 gap the constitution's own §11.4.228 forensic anchor names for this platform). |
| Qwen Code | UNTESTED | `N/A` | No .qwen/skills/ directory exists in this checkout (only .qwen/commands/ + .qwen/settings.json, unrelated to the Agent-Skill format) and no registrar targets one — zero recorded loading path this cycle, tracked per §11.4.197 (the exact G1 gap the constitution's own §11.4.228 forensic anchor names for this platform). |

### `workable-item-lifecycle`

- **Owner (registrar):** `constitution`
- **Manifest source:** `constitution/skills/workable-item-lifecycle`

| Platform | Status | Loading path | Evidence / Note |
|---|---|---|---|
| Claude Code | SUPPORTED | `.claude/skills/workable-item-lifecycle` | symlink, SKILL.md read 4648 bytes (resolves to the manifest-declared source). Live-probed 2026-09-23T09:34:18Z. |
| OpenCode | UNTESTED | `N/A` | No .opencode/skills/ directory exists in this checkout and no registrar (scripts/register_skills.sh only ever writes .claude/skills/) targets one — zero recorded loading path this cycle, tracked per §11.4.197. .opencode/ here carries only SpecKit *commands* (.opencode/commands/), not skills. |
| HelixCode | SUPPORTED | `.claude/skills/workable-item-lifecycle (external tier)` | via external tier -> .claude/skills/workable-item-lifecycle. helix_code/internal/agent/skill_dirs.go ExternalSkillsDir() defaults to '.claude/skills' (config-injectable via HELIXCODE_EXTERNAL_SKILLS_DIR); the external tier's LOADING MECHANISM (a single fixture skill, no similarity conflict) is REAL-BINARY verified at qa-results/hxc159/helixcode_wire/20260922T190852Z/ (green_cli_e2e.txt). HXC-159 F-17 (2026-09-23): the shared SimilarityThreshold was recalibrated 0.0256->0.0709 against the real, frontmatter-complete corpus-1 (see docs/qa/hxc159_f17_remediation_20260923T0921Z/threshold.json); the REAL 8-9-skill corpus-1 tier is nonetheless STILL policy-refused at BOTH consumers on a genuine confusable pair (action-prefix-system \|\| reporting-workable-items, score 0.1045 -- real content overlap, not a miscalibration), captured at docs/qa/hxc159_f17_remediation_20260923T0921Z/helixcode_recalibrated_run.txt and the mirrored helix_agent_native_recalibrated_run.txt. This SUPPORTED cell reports per-skill FILE PRESENCE at the platform directory (§11.4.228(A)(1)'s liveness probe), not whole-tier ACTIVATION -- a distinction this catalogue's generator does not currently classify separately (tracked per §11.4.197, see the F-17 subagent's final report for the residual-finding discussion). Honest boundary (§11.4.6, T-P5.05.4 in specs/001-helixskills-incorporation/tasks.md): the per-skill activation-allowlist/ceiling mechanism was NOT verifiable from within helix_code/'s own file scope this cycle — grep-confirmed zero Go implementation of a per-skill activation ceiling in this module. |
| Gemini CLI | UNTESTED | `N/A` | No .gemini/skills/ directory exists in this checkout (only .gemini/commands/, unrelated to the Agent-Skill format) and no registrar targets one — zero recorded loading path this cycle, tracked per §11.4.197 (the exact G1 gap the constitution's own §11.4.228 forensic anchor names for this platform). |
| Qwen Code | UNTESTED | `N/A` | No .qwen/skills/ directory exists in this checkout (only .qwen/commands/ + .qwen/settings.json, unrelated to the Agent-Skill format) and no registrar targets one — zero recorded loading path this cycle, tracked per §11.4.197 (the exact G1 gap the constitution's own §11.4.228 forensic anchor names for this platform). |

## Source tracking

Per §11.4.228(B): every extension's canonical source is machine-readable via `scripts/skills_manifest.tsv` itself (name, repo-relative source directory, owning registrar) — the manifest row IS this catalogue's `EXTENSION_SOURCE` record for every entry here (no separate `EXTENSION_SOURCE.yaml` per skill; the manifest already carries the same three fields at repo scope, avoiding a duplicated, driftable second record per §11.4.106). All eight entries are constitution-owned (`owner=constitution`) and SYMLINKED (never copied) from `constitution/skills/<name>/` into `.claude/skills/<name>` by `scripts/register_skills.sh` — the preferred-over-copies provenance §11.4.228(B)(2) requires.

Source manifest: `/home/milosvasic/Projects/helix_code/scripts/skills_manifest.tsv` (8 rows).

