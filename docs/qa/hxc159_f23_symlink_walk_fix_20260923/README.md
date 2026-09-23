# HXC-159 F-23 — helix_agent symlink-blindness fix (RED -> GREEN + real-corpus evidence)

Round-4 independent review finding F-23 (`specs/001-helixskills-incorporation/tasks.md`):
`submodules/helix_agent/internal/skills/parser.go`'s `ParseDirectory` used
`filepath.Walk`, which does not follow directory symlinks, so pointed at a
registrar-shaped external tier (`.claude/skills/`, 9 symlinks provisioned by
`scripts/register_skills.sh`) it silently returned zero skills with no error
-- the same class of defect Phase F-03 already fixed for `pkg/skills`
(`submodules/skills/pkg/skills/loader.go:walkForManifests`).

## Evidence sequence

1. `01_RED_parser_test_fails_pre_fix.txt` -- `go test ./internal/skills/...`
   for the new symlink test BEFORE the fix: `TestParser_ParseDirectory_FollowsSymlinkedSkillDirectories`
   FAILS with `"[]" should have 1 item(s), but has 0` -- the bug reproduced
   on a fixture mirroring `.claude/skills/`'s exact shape (a real skill
   directory + a symlink pointing at it, in a separate "external" root).
2. `02_GREEN_parser_tests_post_fix.txt` -- same tests + the full
   `TestParser_*` set AFTER the fix (symlink-following walk mirroring
   `pkg/skills/loader.go`'s approach, with a `visited` real-path set
   guarding symlink cycles): all PASS, including the pre-existing F-16
   (frontmatterless) and malformed-skill surfacing tests -- no regression.
3. `03_GREEN_full_internal_skills_suite.txt` -- `go test ./internal/skills/...`
   (whole package): 0 FAIL / 131 PASS.
4. `04_real_corpus_similarity_probe_output.json` -- `cmd/similarity-probe`
   (`ExternalSkillSource.LoadForProbe`, which calls the fixed
   `ParseDirectory` through the SAME production path `Load()` uses) run
   directly against the REAL `.claude/skills/` directory (9 real symlinks
   provisioned by the registrar, not a synthetic fixture): 7 admitted + 2
   policy-excluded (similarity conflicts, expected/pre-existing per F-17/F-22)
   = 9 of 9 real symlinked skills discovered. Pre-fix this call returned
   `admitted=[]`, `excluded=[]`, `refusal=""` -- a silent-empty false-null.

## Fix

`submodules/helix_agent/internal/skills/parser.go` -- `ParseDirectory`
rewritten from `filepath.Walk` to a `filepath.EvalSymlinks` + `os.ReadDir`
recursive walk with a `visited` (real-path) cycle guard, mirroring the
already-shipped `pkg/skills/loader.go:walkForManifests` approach for
consistency across the two independently-maintained implementations.
All existing ParseDirectory behaviour preserved byte-for-byte (malformed-skill
Warn+surface, F-16 empty-name Warn+surface, `.git` exclusion, exact-case
`SKILL.MD` match).

New tests: `submodules/helix_agent/internal/skills/symlink_walk_test.go`
(`TestParser_ParseDirectory_FollowsSymlinkedSkillDirectories`,
`TestParser_ParseDirectory_SymlinkCycleTerminates`).

Production impact: helix_agent's CURRENT configuration points
`ExternalSkillSource` at `constitution/skills/` directly (not the
registrar's symlink directory), so this defect was latent, not
production-live -- but the fix closes the inconsistency with helix_code's
already-fixed behaviour for the same directory shape, per the round-4
finding.
