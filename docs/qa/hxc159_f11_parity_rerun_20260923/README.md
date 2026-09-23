# HXC-159 F-11 real parity re-run — 2026-09-23

Evidence for tasks.md F-11 / RS-12 closure.

## Commands run (from meta-repo root unless noted)

```bash
EXTERNAL="$(pwd)/constitution/skills"
cd helix_code
go run ./cmd/skillconformance -project /tmp/.../empty_project -user /tmp/.../empty_local \
  -external "$EXTERNAL" -out helix_code_report.json
cd ../submodules/helix_agent
go run ./cmd/skillconformance -local /tmp/.../empty_local -external "$EXTERNAL" \
  -out helix_agent_report.json
cd ../../helix_code
bash scripts/gate_cross_consumer_parity.sh helix_code_report.json helix_agent_report.json
```

Both consumers were pointed at the SAME stable, repo-tracked `constitution/skills/`
directory as their external tier, with EMPTY project/user/local directories — this
deliberately excludes the host-state-mutable `.claude/skills/` pool (which on this
host contains a `speckit-analyze` entry symlinked from the non-repo-tracked
`.skill-activation/pool/`) from the comparison entirely, per F-11's "drop host-state
rows" requirement — achieved by never feeding the mutable directory to either report
generator, rather than by post-hoc filtering.

## Files

- `helix_code_report.json` — helix_code's `skillconformance.CapabilityManifest`, 9 skills
  (8 external + 1 builtin `conventional-commit`).
- `helix_agent_report.json` — helix_agent's `skillconformance.CapabilityManifest`, 5 skills
  (all external; 3 of the 8 external directories are silently absent — see F-16).
- `gate_stdout.txt` — real stdout + exit code of
  `scripts/gate_cross_consumer_parity.sh` (`go run ./cmd/skillparity`) diffing the two
  reports above.

## Result (honest, not editorialised)

`CM-CROSS-CONSUMER-PARITY: FAIL` — 3 surface asymmetries + 4 skill asymmetries, exit 1.

Zero of the remaining asymmetries are a `local.Title Case` vs `local.directory-name`
identity-scheme mismatch (F-11's actual defect, now provably eliminated — all 8
external skills present in BOTH reports carry byte-identical `external.<dirname>`
qualified names). The 4 skill asymmetries and 3 surface asymmetries are real,
separately-rooted, NOT fixed by this pass:

- `builtin.conventional-commit` — architectural (helix_code's compiled-in builtin
  tier has no helix_agent equivalent).
- `external.media-validator` / `external.scheduled-work-queue` / `external.session-sync`
  — root-caused live (isolated `Parser.ParseDirectory` probe against each of the 8
  `constitution/skills/*` directories individually) to
  `submodules/helix_agent/internal/skills/registry.go:145`
  (`Registry.registerSkillLocked`): `if skill.Name == "" { return }` silently drops
  any parsed skill whose YAML front-matter `name:` field is empty. These 3
  directories use a bare `# Title` H1 heading with NO front-matter block at all;
  helix_agent's `Parser.splitFrontmatter` treats a missing `---` delimiter as valid
  ("no frontmatter, whole file is body"), so the 3 parse cleanly with `Name==""` and
  vanish at registration, never reaching `GetAllSkills()`. Tracked as new finding
  **F-16** in tasks.md — pre-existing, upstream of and independent from the F-11
  identity fix, out of F-01/F-11's authorized fix set.
- `audit_log` / `capability_declaration` / `capability_enforcement` surface flags —
  helix_agent genuinely has pre-existing `AllowedTools` + `SkillAuthorizer`
  infrastructure helix_code's report currently declares `false` for; a real
  architecture difference, not a bug. Per tasks.md T-P9.03.6(f), marking these
  optional-on-both-sides requires an explicit operator §11.4.66 decision, not made
  here.
