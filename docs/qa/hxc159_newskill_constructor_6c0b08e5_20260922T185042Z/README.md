# HXC-159 T-P5.01 — export a Skill constructor (NewSkill) — QA evidence (§11.4.83)

**Run ID:** `hxc159_newskill_constructor_6c0b08e5_20260922T185042Z`
**Fix commit:** `6c0b08e5cc055f59b14fae2fb873c51acfe0f683`
**Captured (evidence originally produced):** 2026-09-22 18:50
**This docs/qa record authored:** 2026-09-23 (T-P9.03, HXC-159 F-20 remediation)

## What this commit did

`grep "func NewSkill\b"` previously returned zero hits module-wide; the only
constructors were unexported `parseSkillFile` and a documented test-only
helper (`ParseSkillForTest`, which still round-trips through frontmatter
text). A `*commands.Skill` could not be built from outside its package
except by writing a `SKILL.md` to disk and driving it through
`SkillLoader`. Adds `commands.NewSkill(SkillOptions)` as the exported,
in-memory construction seam: no filesystem round-trip, unexported fields
stay unexported, and it constructs THROUGH validation (a non-empty `Name`
is required, matching the invariant every disk-loaded skill already
satisfies unconditionally). Field semantics (Variables defaulting,
TriggerPatterns compiled the same way with an invalid regex silently
skipped, Body trimmed) mirror `parseSkillFile` exactly so a `NewSkill`-built
`Skill` is indistinguishable, at every exported method, from one loaded
from disk.

## How this evidence was produced

Real command captures, RED-first (§11.4.115):

1. `red_compile_fail.txt` — RED: a genuine compile failure
   (`undefined: commands.NewSkill`) from an external test package
   (`commands_test`) before this change.
2. `green_pass.txt` — GREEN: 3 new tests
   (`TestNewSkill_RejectsEmptyName`,
   `TestNewSkill_InvalidTriggerPatternIsSkippedNotFatal`, and a third
   constructing/registering/trigger-matching/rendering a `Skill` built
   entirely in memory) plus the full `internal/commands` package tree,
   all PASS.
3. `full_commands_pkg_test.txt` — the full `internal/commands` +
   `internal/commands/builtin` package suite, green post-fix.
4. `full_build.txt` — full repo build green post-fix.

Copied verbatim from `qa-results/hxc159/skill_ctor/20260922T185042Z/`,
which is this commit's own cited evidence path (`6c0b08e5`'s commit
message: "Evidence: qa-results/hxc159/skill_ctor/20260922T185042Z/").

## Files

- `captures/red_compile_fail.txt`
- `captures/green_pass.txt`
- `captures/full_commands_pkg_test.txt`
- `captures/full_build.txt`

## Result

RED (compile failure on the not-yet-existing constructor) → GREEN (3 new
tests + the full package suite PASS) is captured, and the full repo still
builds clean. The constructor is exercised in memory (no filesystem
round-trip), matching the fix's stated design goal.
