# F-13 builtin_skills investigation — evidence summary

## Root cause (investigated, not assumed — §11.4.102/§11.4.6)

The finding's premise ("7 constitution skills embedded and diverged
byte-for-byte") is only half true. Investigation of BOTH frontmatter AND
body content (not just `diff -q` on file bytes) shows the two sides are
NOT the same artifact:

- `constitution/skills/<name>/SKILL.md` — Claude-Code-native Agent Skill:
  `name`/`description`/`version` frontmatter, full reference-doc body.
- `helix_code/internal/commands/builtin_skills/<name>/SKILL.md` —
  HelixCode-internal prompt TEMPLATE for its own regex-triggered REPL
  completion feature: `description`/`triggers`(regex list)/
  `requires_isolation` frontmatter, `{{ARG.x}}`-templated short body,
  consumed by `markdown_skills.go`'s own loader/matcher.

See `02_check_before_scoped.log` for the drift report and the body-diff
capture below.

## Proof that byte-identical generation is UNSAFE (not merely "different")

01_check_before.log / 02_check_before_scoped.log — RED: `--check` confirms
7/8 drifted (`conventional-commit` has no constitution counterpart at all).

03_regenerate.log — regenerate mode applied for real (synced 7).

04_check_after.log — `--check` after regenerate: 0 drifted (proves the
generator mechanically WORKS as a sync tool).

**Then**, `go test ./internal/commands/...` was re-run against the
regenerated content:

```
--- FAIL: TestGovernanceSkills_RegisteredAndInvocable/action-prefix-system
    governance skill "action-prefix-system" declares no triggers, so
    auto-trigger can never fire
--- FAIL: TestGovernanceSkills_RegisteredAndInvocable/media-validator
    governance skill "media-validator" must carry a description
... (all 7 fail, same two failure classes)
```

This is real, captured proof (§11.4.5/§11.4.69) that overwriting the
builtin format with the constitution-native format destroys the regex
auto-trigger completion feature — a genuine capability regression
(§11.4.122), not a sync fix.

## Decision (§11.4.6 honest boundary — not executed blindly)

- Reverted the regenerated content (`git checkout --
  helix_code/internal/commands/builtin_skills/`); re-ran the same test
  suite: GREEN (`go test ./internal/commands/... ` all pass, see
  `06_tests_after_revert.log`).
- Kept `helix_code/scripts/generate_builtin_skills.sh` as REAL,
  WORKING drift-DETECTION tooling (`--check` mode), wired via a
  `//go:generate` directive on `--check` only (never regenerate) so
  future divergence is at minimum VISIBLE and CI-checkable, never
  silently invisible again.
- Did NOT run the destructive regenerate mode against the live tree.
- Did NOT retire the embedded copy (F-01's `pkg/skills` external-tier
  wiring, the natural retirement candidate per this finding's own text,
  is the sibling's exclusive in-flight scope this round — retiring a
  shipped, tested capability ahead of that landing would itself be a
  §11.4.122 silent-removal violation).
- Filed this as requiring an explicit architectural decision (reconcile
  the two SKILL.md formats, or retire the internal regex-trigger
  mechanism once pkg/skills is confirmed to supersede it) — tracked, not
  executed unilaterally.

## Build/test evidence (final state — content reverted, tooling kept)

```
$ go build -tags nogui ./internal/commands/...
(exit 0)
$ go test -tags nogui -run "Skill|Builtin|Governance" ./internal/commands/...
ok  	dev.helix.code/internal/commands	0.283s
ok  	dev.helix.code/internal/commands/builtin	(cached)
```
