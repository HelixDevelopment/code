# HXC-159 F-13 — Architectural Options for `builtin_skills` vs `constitution/skills` — Decision Packet

| Field | Value |
|---|---|
| Revision | 1 |
| Created | 2026-09-23 |
| Last modified | 2026-09-23 |
| Status | active — awaiting operator/architectural decision |
| Item | HXC-159 finding F-13 (`specs/001-helixskills-incorporation/tasks.md`, round-3 ledger + round-3 verification note) |
| Scope | `helix_code/internal/commands/markdown_skills.go` (read), `helix_code/internal/commands/builtin_skills/**` (read), `constitution/skills/**` (read), blast-radius call sites (read) — **no code was changed by this packet's investigation; one throwaway experiment was performed in an isolated `git worktree` under `/tmp` and fully reverted (§11.4.6 — verified, not assumed; see §5)** |
| Author | Investigation subagent, dispatched by the HXC-159 T-P9.01 round-3 closure stream |
| Decision owner | **The operator.** This document does not choose Option A/B/C/D — see §6. |

## Table of contents

- [0. Why this document exists](#0-why-this-document-exists)
- [1. What F-13 actually is (recap, with the exact artifacts)](#1-what-f-13-actually-is-recap-with-the-exact-artifacts)
- [2. Option A — redesign the loader to consume Claude-Code-native frontmatter](#2-option-a--redesign-the-loader-to-consume-claude-code-native-frontmatter)
- [3. Option B — retire the `builtin_skills` embed, load exclusively from the external tier](#3-option-b--retire-the-builtin_skills-embed-load-exclusively-from-the-external-tier)
- [4. Option C and Option D — two paths neither prior record considered](#4-option-c-and-option-d--two-paths-neither-prior-record-considered)
- [5. The Option-B removal experiment — exact commands and exact output](#5-the-option-b-removal-experiment--exact-commands-and-exact-output)
- [6. Side-by-side comparison](#6-side-by-side-comparison)
- [7. Risk of doing nothing, concretely](#7-risk-of-doing-nothing-concretely)
- [8. What this document is NOT](#8-what-this-document-is-not)
- [Sources verified](#sources-verified)

## 0. Why this document exists

HXC-159's round-3 independent review closed F-13 as **investigated, not
fixed** (commit `18ea70e5`, `docs/qa/hxc159_f13_builtin_skills_investigation_18ea70e5_20260923T064844Z/`).
That investigation proved the two prior candidate remedies named in the
task ledger were both unsafe or premature, and left the actual
architectural decision — "redesign the loader" or "retire the internal
mechanism" — explicitly deferred as a §11.4.197 follow-up requiring an
operator/architectural call, "not a unilateral pick under this finding."

This document is that follow-up. It makes both named directions concrete
enough to choose between in one read, adds two directions neither prior
record considered, and states plainly: **it does not make the call.**

## 1. What F-13 actually is (recap, with the exact artifacts)

`constitution/skills/<name>/SKILL.md` and
`helix_code/internal/commands/builtin_skills/<name>/SKILL.md` are two
**structurally different artifact types** that happen to share a directory
name and a governance topic, for 7 of `builtin_skills`'s 8 entries
(`conventional-commit` has no constitution counterpart at all — it is
HelixCode's own, always was).

**Constitution side** — a Claude-Code-native Agent Skill:

```yaml
---
name: Action Prefix System
description: 'Use when a prompt begins with an UPPERCASE action token ...'
version: 1.0.0
---
```

Consumed by Claude Code's own semantic-description-driven skill discovery
(and, once HXC-159's external tier is fully admitted — see §3.3 — by
`helix_agent` and `helix_code`'s external-tier loaders too). The
`description` field is deliberately long, written in "use when ..." form
for an LLM to match against user intent.

**`builtin_skills` side** — a HelixCode-internal REPL completion-prompt
template:

```yaml
---
description: Explain and apply the UPPERCASE action-prefix directive system (BACKGROUND, ISSUE, BUG, TASK, REMINDER, FEATURE)
triggers:
  - "(?i)^what does the (?P<action>[A-Za-z][A-Za-z0-9_]*) (?:action )?prefix (?:do|mean)\b.*$"
  - "(?i)^(?:what|how).*\baction[- ]prefix(?:es)?\b.*$"
  - "(?i)^how do i (?:add|register) (?:a )?new (?:action|directive)\b.*$"
requires_isolation: false
---
```

Consumed by `SkillRegistry.FindMatching` — an explicit-regex trigger
matcher inside HelixCode's own REPL/CLI (`helix_code/internal/commands/markdown_skills.go:307-339`),
which fires on a **literal, hand-authored regex match** against the raw
user utterance, not an LLM's semantic judgement.

Measured byte sizes (all 7, current tree):

| Skill | `builtin_skills` bytes | `constitution/skills` bytes |
|---|---:|---:|
| action-prefix-system | 2657 | 6561 |
| media-validator | 2325 | 1554 |
| multitrack | 2464 | 1980 |
| reporting-workable-items | 2398 | 5100 |
| scheduled-work-queue | 1960 | 3727 |
| session-sync | 2028 | 4924 |
| workable-item-lifecycle | 2690 | 4748 |

All 7 `builtin_skills` entries carry `requires_isolation: false` — that
field currently carries no live semantic signal for any of the 7 (see
§2.2), which simplifies (but does not eliminate) the migration story.

**Proof this is a real, non-cosmetic difference, not accidental drift**
(from the prior investigation, re-verified fresh in §5 of this packet):
overwriting `builtin_skills/*/SKILL.md` with the constitution-native
content and running
`go test -tags nogui -run "Skill|Builtin|Governance" ./internal/commands/...`
breaks all 7 subtests of `TestGovernanceSkills_RegisteredAndInvocable`
with `"declares no triggers, so auto-trigger can never fire"` — because
the constitution-native frontmatter has no `triggers:` field at all, and
`FindMatching` cannot select a skill whose compiled trigger list is empty.

## 2. Option A — redesign the loader to consume Claude-Code-native frontmatter

### 2.1 What would actually need to change

The parsing code is `parseSkillFile` in `markdown_skills.go:191-231`. Its
frontmatter struct is:

```go
type skillFrontmatter struct {
    Description       string            `yaml:"description"`
    Triggers          []string          `yaml:"triggers"`
    Variables         map[string]string `yaml:"variables"`
    RequiresIsolation bool              `yaml:"requires_isolation"`
}
```

To parse the native shape without error, this struct needs `Name` and
`Version` fields added (`yaml:"name"`, `yaml:"version"`) — trivial, and
YAML unmarshalling already ignores unknown keys in either direction, so
this part alone is safe and backward-compatible. **That is not the hard
part.** The hard part is `Triggers`: the native format has no such field,
so a loader reading native frontmatter verbatim gets `Triggers: nil` for
every migrated skill, and `FindMatching` (§2.2) silently stops routing
REPL input to that skill — precisely the regression the prior
investigation already reproduced and reverted.

### 2.2 What `triggers` and `requires_isolation` are actually used for — read at the call sites, not assumed

**`triggers` / `FindMatching`.** The consumer is
`SkillRegistry.FindMatching` (`markdown_skills.go:311-339`): it walks
skills in lexicographic name order and returns the first skill whose
*compiled* trigger regex matches the raw input string, plus any named
capture groups. The sole caller is `SkillDispatcher.Match`
(`helix_code/internal/agent/skill_dispatcher.go:35-51`), used by
`DispatchSkill` to decide, on every user prompt in the REPL/CLI front-end,
whether to substitute the skill's rendered body for the prompt before it
reaches the LLM. **This is deterministic pattern-matching, not semantic
matching** — it is what makes typing `"remind me to re-check the nightly
export"` deterministically route to `scheduled-work-queue` without any
LLM call. A skill with zero compiled triggers can never win that match; it
becomes permanently unreachable via this path (still visible in
`/skills list`, since that enumerates the registry directly, but never
auto-triggered).

Can `triggers` be **derived** from the native `description` field
mechanically? No — not safely. The native descriptions are prose written
for an LLM's semantic judgement ("Use when a prompt begins with an
UPPERCASE action token ... or when the operator asks how the
action-directive system works, how to add a new directive, why a bare
`/name` did not resolve, ..."), not enumerable trigger phrases. Compiling
a regex out of that prose mechanically (e.g. naive keyword extraction)
would either (a) match far too broadly, turning REPL routing from a
precise, auditable regex into an unpredictable heuristic, or (b) require
an LLM call on every keystroke to decide routing, which defeats the
entire purpose of having a fast, offline, deterministic trigger layer
distinct from the LLM's own semantic skill discovery. **Genuine finding,
not guessed:** the native format does not carry the information this
mechanism needs, and there is no safe automatic derivation.

**`requires_isolation`.** Read at every call site
(`grep -rn "RequiresIsolation()"`): `skill_dispatcher.go`'s doc comment
(advisory only — the caller inspects it and decides whether to route to a
worktree; `SkillDispatcher` itself never branches on it),
`skills_command.go:82` and `cmd/cli/skills_cmd.go:67` (both purely
cosmetic — included in `/skills show`'s rendered output map). There is no
gating logic anywhere that refuses to run a skill because
`RequiresIsolation()` is true. Since all 7 migration candidates already
carry `requires_isolation: false`, a native-format migration that simply
defaults this field to `false` when the source frontmatter omits it loses
**no live semantic** for these 7 specific skills today — but the field
itself would no longer be *representable* in the native schema for any
future skill that legitimately needs `true`, unless Option C's superset
approach is taken.

### 2.3 Blast radius — every real call site

`grep -rln "builtin_skills\|builtinSkillsFS\|builtinSkillsRoot"` across
`helix_code` (non-test and test) returns exactly four files:
`markdown_skills.go` (the loader itself), `internal/skillconformance/report.go`
(a string-literal tier name `"builtin"` for `DirTierMap.tierFor`'s fallback
— unaffected by a format change, only by whether the tier exists at all),
`internal/commands/governance_skills_registered_test.go` (HXC-163's
runtime-signature guard — see §5.1), and
`internal/commands/skill_manifest_placeholder_guard_test.go` (a
placeholder-token sweep over the embedded tree — format-agnostic, reads
raw file bytes).

`NewSkillLoader` (the constructor a format change does not itself touch,
but any behavioural change to what it *registers* ripples through) has 7
call sites: `skill_external_policy.go:187`, `skill_activation.go:62`,
`cmd/skillconformance/main.go:51,70`, `cmd/cli/main.go:1089,1094,3383,3388`,
`tests/integration/cmd/p1f10_challenge/main.go:42`. None of these need to
change for Option A — they all go through the same `SkillLoader.Reload`
entry point regardless of the frontmatter schema it parses.

### 2.4 What migration would look like for an end user — concretely

If Option A is implemented as "make the loader accept native frontmatter,
and re-point `//go:embed` at (a build-time copy of) `constitution/skills/`
content directly" without also solving the `triggers` problem (§2.2), the
end-user-visible change is: **all 7 skills stop being reachable via
REPL/CLI regex auto-trigger.** A user who currently types "remind me to
re-check the nightly export" and gets `scheduled-work-queue`'s body
substituted automatically would, post-migration, get nothing — the
skill is still listed in `/skills list` and still invocable by explicit
name (`/skills invoke scheduled-work-queue`), but the phrase-triggered
completion feature HXC-163 introduced (see the runtime-signature comment
in `governance_skills_registered_test.go`) is gone for these 7 skills.
This is a real, measured, user-facing regression, not a hypothetical —
it is the exact failure mode already reproduced in §5 and in the prior
investigation's commit `18ea70e5`.

## 3. Option B — retire the `builtin_skills` embed, load exclusively from the external tier

### 3.1 Current state of the external tier (checked fresh this session, not assumed)

`helix_code`'s external tier is wired: `go.mod` line 21 declares
`github.com/HelixDevelopment/skills`, replaced at line 215 by
`../submodules/skills`; `skill_external_policy.go` implements
`PolicyGateExternalTier`, routing every external-tier load through the
shared `pkg/skills` policy engine (allowlist, activation ceiling,
description-similarity, C5 sandbox precondition); `skill_activation.go`'s
`LoadSkillsAndDispatcher` calls it for the one directory matching
`ExternalSkillsDir()`, falling through to the ordinary `SkillLoader` path
for every other directory.

**But as of this exact session** (`specs/001-helixskills-incorporation/tasks.md`,
F-17 row, live-edited by the concurrent sibling remediation stream while
this packet was being written — cited here as a snapshot, not a settled
fact, because it is actively moving): F-17 is **"PARTIALLY FIXED,
remainder OPERATOR-BLOCKED."** The front-matter-less-skill fail-closed
abort is fixed and the similarity threshold has been recalibrated, but the
residual: on the real corpus, `action-prefix-system` and
`reporting-workable-items` are a *genuinely* confusable pair by the
policy engine's own content-based similarity metric (shared tokens
`{action, add, bug, forms, issue, operator, task}`), and the external
tier's admission model is **whole-tier fail-closed by design** — one
confusable pair anywhere in the tier refuses the *entire* tier, not just
that pair. Sub-item resolution needs an explicit architectural or content
decision (three named options in the F-17 row itself), not a mechanical
fix. **This means: right now, at the moment of writing, retiring
`builtin_skills` in favour of the external tier would leave these 7
skills unreachable through EITHER path** — not because of a coding bug,
but because the tier that would replace `builtin_skills` currently
refuses to admit itself on this exact corpus.

### 3.2 What depends specifically on the embed being a compile-time, zero-filesystem embed

This is not merely a convenience question — it is a **named, tested
product requirement.** `governance_skills_registered_test.go`'s own doc
comment states the HXC-163 runtime signature explicitly:

> "a SkillLoader constructed with NO on-disk directories (built-in tier
> only, exactly as a shipped binary sees the world) resolves all seven
> governance skills"

`TestGovernanceSkills_RegisteredAndInvocable` constructs
`NewSkillLoader(reg, "", "")` — literally no project dir, no user dir —
and asserts all 7 skills resolve, match, and render. This is a
deliberate guarantee that a freshly installed `helixcode` binary, with
**zero filesystem provisioning** (no `.claude/skills/` symlinks from
`scripts/register_skills.sh`, no `.helix/skills/` project directory), can
still explain and apply governance mechanisms to a user out of the box.
An external-tier-only design cannot provide this guarantee by
construction: the external tier is, by definition, populated from
on-disk directories that something else (an installer, a symlink
registrar) must provision before the binary can see them. Retiring the
embed without also either (a) relaxing HXC-163's own runtime-signature
requirement (an explicit decision on a *different*, already-closed
finding), or (b) finding some other zero-filesystem-guarantee mechanism,
directly breaks a requirement this same finding's own governing test was
written to protect.

### 3.3 What actually breaks today if the embed is removed — measured, not guessed

See §5 for the full experiment. Summary: with `builtin_skills` disabled
and no on-disk directories present (the exact HXC-163 zero-filesystem
condition), all 7
`TestGovernanceSkills_RegisteredAndInvocable` subtests fail with
`"governance skill ... is NOT registered: it can never be triggered by a
user or an agent"`, and two further pre-existing tests
(`TestSkillLoader_BuiltinTierAvailable`, `TestSkillLoader_LoadedReturnsSnapshot`)
fail because `conventional-commit` — the 8th `builtin_skills` entry with
**no constitution counterpart at all** — disappears too. Option B, if
executed naively, needs an explicit answer for `conventional-commit`:
it cannot come from `constitution/skills/` (it never existed there), so
"load exclusively from the external/canonical source" needs a second,
separate mechanism for HelixCode's own skills that have no constitution
equivalent — or `conventional-commit` needs to move somewhere the
external tier can see it, which reintroduces the zero-filesystem problem
from §3.2 for that skill specifically.

### 3.4 Blast radius

Same four non-test/test files as Option A (§2.3) directly reference the
`builtin_skills` embed; the same 7 `NewSkillLoader` call sites are
unaffected in shape (they would simply stop being fed a functioning
built-in tier). Additionally: `internal/skillconformance/report.go`'s
`trustForTier` classifies `"builtin"` as `"first-party"` and
`"external"` as `"vendored"` — removing the builtin tier changes the
trust classification every migrated skill reports under, a second-order
effect worth naming even though it is cosmetic in the current code (no
enforcement reads `Trust`).

## 4. Option C and Option D — two paths neither prior record considered

### 4.1 Option C — a superset/hybrid frontmatter schema, single canonical source

`constitution/skills/*/SKILL.md` is not consumed by HelixCode's builtin
loader alone — it is the shared source for Claude Code's native skill
discovery *and* (once F-17 lands) the external-tier policy engine used by
both `helix_code` and `helix_agent`. Nothing in the YAML frontmatter
contract forbids adding **additional, HelixCode-specific optional keys**
(`triggers:`, `requires_isolation:`) to the constitution-native schema
alongside `name:`/`description:`/`version:`. YAML parsers — including the
one this loader already uses (`gopkg.in/yaml.v3`) via `skillFrontmatter`
— silently ignore unknown keys by default in *either* direction: Claude
Code's own skill loader would simply not look at `triggers:`, and
`helix_code`'s `skillFrontmatter` struct would simply not look at
`name:`/`version:` (until Option A's struct extension lands too, which
this option would still need). This would make `constitution/skills/`
the single, genuinely-authoritative source for **both** artifact types at
once, closing F-13 the way its original "source is authoritative"
premise actually expected — not by making the two artifact types
identical, but by making one file honestly declare both facets it serves.

This is not free: `constitution/skills/` is a shared, multi-consumer,
governance-owned artifact (§11.4.35's canonical-root layer). Adding
HelixCode-specific fields to it is a change to a canonical, cross-project
source, not a `helix_code`-local decision — it needs its own §11.4.150
deep-research/consensus pass and likely an explicit constitution-submodule
edit under §CONST-049's fetch-first workflow, which is out of scope for
this packet to execute. There is also a structural constraint worth
naming: Go's `//go:embed` directive cannot reach outside its own module's
directory tree (it cannot embed `../../constitution/skills` across the
`constitution` submodule boundary at build time), so even with a unified
schema, `helix_code` would still need *some* build-time copy step to get
the content into its own embeddable tree — which is exactly what
`generate_builtin_skills.sh`'s existing (currently unsafe) regenerate
mode already does; a unified schema is precisely what would make running
that regenerate mode *safe* rather than destructive.

### 4.2 Option D — formally reclassify the divergence as intentional, keep detection-only tooling

The prior investigation already did most of the work for this path
without naming it as a standing option: it added `scripts/generate_builtin_skills.sh --check`
(safe, detection-only, wired via a `//go:generate ... --check` directive)
and documented the two-artifact-type finding directly in
`markdown_skills.go`'s own comment block. Option D would formalize this:
update the *task/spec premise* itself (the "contradicts the spec's
source-authoritative premise" framing in the F-13 ledger row) to state
explicitly that `constitution/skills/<name>/SKILL.md` and
`builtin_skills/<name>/SKILL.md` are **two legitimately distinct,
intentionally-divergent artifacts that happen to share a name**, keep
the `--check` drift-visibility tooling exactly as-is, and close F-13 as
"not a defect, re-scoped" rather than as an open architectural decision.
This is the cheapest path by a wide margin — zero code change beyond
updating the framing in the spec — but it does **not** address the real,
measured content staleness described in §7 (the REPL-taught explanation
of governance mechanisms drifting further from the canonical explanation
over time); it only stops calling that staleness a "sync bug." Whether
that staleness is acceptable is itself a judgement call, not a technical
finding — which is exactly why this document defers it to the operator.

## 5. The Option-B removal experiment — exact commands and exact output

Per the task's ground rules, this experiment was performed in an
isolated `git worktree` under `/tmp` (never the live checkout), with the
submodule/tracked-directory dependencies (`submodules/*`, `constitution`,
`github_pages_website`) symlinked in read-only from the live checkout so
the module graph resolved. All commands below ran inside that isolated
worktree.

**Baseline (fresh worktree, unmodified code, uncached):**

```
$ go test -count=1 -tags nogui -run "Skill|Builtin|Governance" ./internal/commands/...
ok  	dev.helix.code/internal/commands	0.287s
ok  	dev.helix.code/internal/commands/builtin	0.099s
```

**Experiment applied** — `internal/commands/markdown_skills.go`'s
`Reload()`, the single line `l.loadBuiltinSkills(want)` replaced with a
no-op comment, simulating "the builtin tier is never registered" (the
closest one-line approximation of "retire the embed" that still compiles,
since the embedded files themselves remain physically present — this
tests the *runtime registration* removal, which is what Option B's
end-user-visible behaviour actually depends on):

```
$ go build ./internal/commands/...
[exit 0]

$ go test -count=1 -tags nogui -run "TestGovernanceSkills_RegisteredAndInvocable" -v ./internal/commands/...
=== RUN   TestGovernanceSkills_RegisteredAndInvocable
=== RUN   TestGovernanceSkills_RegisteredAndInvocable/action-prefix-system
    governance_skills_registered_test.go:109: Error: Should be true
      Messages: governance skill "action-prefix-system" is NOT registered:
      it can never be triggered by a user or an agent
=== RUN   TestGovernanceSkills_RegisteredAndInvocable/media-validator
    [same failure]
=== RUN   TestGovernanceSkills_RegisteredAndInvocable/multitrack
    [same failure]
=== RUN   TestGovernanceSkills_RegisteredAndInvocable/reporting-workable-items
    [same failure]
=== RUN   TestGovernanceSkills_RegisteredAndInvocable/scheduled-work-queue
    [same failure]
=== RUN   TestGovernanceSkills_RegisteredAndInvocable/session-sync
    [same failure]
=== RUN   TestGovernanceSkills_RegisteredAndInvocable/workable-item-lifecycle
    [same failure]
--- FAIL: TestGovernanceSkills_RegisteredAndInvocable (0.00s)
    (all 7 subtests FAIL)
FAIL	dev.helix.code/internal/commands	0.073s
```

```
$ go test -count=1 -tags nogui -run "TestSkillLoader_BuiltinTierAvailable|TestSkillLoader_LoadedReturnsSnapshot" -v ./internal/commands/...
--- FAIL: TestSkillLoader_BuiltinTierAvailable (0.00s)
    Messages: built-in conventional-commit skill must be available with no user/project dirs
--- FAIL: TestSkillLoader_LoadedReturnsSnapshot (0.00s)
    expected: "builtin:conventional-commit/SKILL.md"
    actual  : ""
FAIL	dev.helix.code/internal/commands	0.048s
```

Every other test in `./internal/commands/...` (`TestGovernanceSkills_BuiltinSetIsCurated`,
`TestParseSkillFile_*`, `TestSkill_Render_*`, `TestSkillRegistry_*`,
`TestSkillLoader_LoadProjectAndUser`, `TestSkillLoader_ReloadDiff_*`,
`TestSkillLoader_ThreeWayPrecedence`, `TestSkillLoader_ProjectOverridesBuiltin`,
`TestSkillManifestPlaceholders_NoUnresolvableTokens` — this one still
passes because it reads the embedded files directly, not through
`Reload()` — `TestSlashSkills_*`, `TestSkillsTree_NoDanglingOrHostAbsoluteSymlinks`,
`TestSkillsWatcher_*`, `TestNewSkill_*`) passed unchanged.

**Revert, verified clean:**

```
$ git checkout -- internal/commands/markdown_skills.go
$ go test -count=1 -tags nogui -run "Skill|Builtin|Governance" ./internal/commands/...
ok  	dev.helix.code/internal/commands	0.315s
ok  	dev.helix.code/internal/commands/builtin	0.133s
```

**Worktree fully removed** (`git worktree remove --force
/tmp/hxc159-f13-optionB-experiment`; `rm -rf` of the directory;
`git worktree prune`). Confirmed the **live checkout** at
`/home/milosvasic/Projects/helix_code` carries none of this experiment's
edits — its `git status` before and after this investigation differs only
by the concurrent sibling F-17 remediation stream's own commits/edits to
`constitution`, `submodules/skills`, `submodules/helix_agent`,
`submodules/helix_qa`, `specs/001-helixskills-incorporation/tasks.md`,
`docs/extensions/*`, `scripts/benchmark_skills/*`, and
`helix_code/scripts/extensions_catalog/generate_catalog.py` — none of
which this investigation touched.

## 6. Side-by-side comparison

| | **Option A** — native-frontmatter loader | **Option B** — retire embed, external-tier only | **Option C** — superset schema, one source | **Option D** — reclassify, keep detection-only |
|---|---|---|---|---|
| Code change scope | `markdown_skills.go` struct + parse logic; a `triggers`-derivation problem with **no safe automatic solution** (§2.2) | `markdown_skills.go` (remove/change embed call); `conventional-commit` needs a separate answer (§3.3) | `constitution/skills/*` schema (cross-project, governance-owned) + `markdown_skills.go` struct + `generate_builtin_skills.sh` becomes safe to run in regenerate mode | Update the F-13 task/spec framing only; zero production code change |
| Breaks REPL auto-trigger for the 7 skills? | **Yes**, unless `triggers` is also hand-authored somewhere (collapses into Option C) | **Yes** — proven in §5, plus violates HXC-163's own zero-filesystem runtime signature (§3.2) unless that requirement is separately revisited | **No** — same content serves both consumers, triggers preserved | **No** — nothing changes at runtime |
| Depends on external tier being stable | No | **Yes — and it is not stable right now** (§3.1, F-17 `OPERATOR-BLOCKED` as of this session) | No (builtin tier still compile-time embedded) | No |
| Needs a decision outside `helix_code`'s own scope | No | No | **Yes** — `constitution/skills/` schema change is governance-owned, cross-consumer | No |
| Solves `conventional-commit` (no constitution counterpart)? | N/A (unaffected — it's not constitution-sourced) | **No — needs its own separate mechanism** (§3.3) | N/A (unaffected) | N/A (unaffected) |
| Addresses the real content-staleness risk (§7)? | Yes, if completed | Yes, if F-17 lands and the trigger problem is solved | **Yes, most directly** — one source, always current | **No** — explicitly accepts it as by-design |
| Estimated relative cost | Medium-high (a genuinely hard sub-problem with no clean answer) | Medium, but currently blocked on a finding (F-17) outside this decision's control | Medium-high, plus a cross-project governance step | Low |

## 7. Risk of doing nothing, concretely

Doing nothing does not mean "no change happens" — the constitution side
keeps evolving while `builtin_skills` stays frozen, and that gap is
measurably widening, not static:

- `action-prefix-system`'s constitution-side description was rewritten to
  cover the full §11.4.140 grammar (all six directive forms, including
  `NAME ---> rest` and the single-colon `NAME: rest` form) and now runs
  6561 bytes versus `builtin_skills`'s 2657 — the REPL-triggered skill a
  user gets when they type "what does the BACKGROUND action prefix do"
  teaches an **incomplete, dated explanation** of a mechanism that has
  since grown two additional invocation forms.
- `media-validator`, `scheduled-work-queue`, and `session-sync` on the
  constitution side were touched **today, in this exact session**, by the
  concurrent F-17 remediation stream (adding real `name:`/`description:`
  frontmatter where none existed before) — `builtin_skills`'s copies of
  these three have not moved since 2026-07-29 (last real content commit;
  the original embed dates to 2026-06-14).
- This is not a hypothetical future risk. It is the literal mechanism by
  which F-13 was discovered in the first place, and by design it will
  keep recurring on every future constitution-side skill edit, forever,
  unless one of §2–§4's options (or an explicit Option D reclassification)
  is adopted.
- Doing nothing also leaves the `//go:generate ... --check` directive as
  the only safeguard — it is detection-only (per the prior investigation),
  reports drift, and changes nothing; a human still has to notice the
  report and act on it.

## 8. What this document is NOT

This document does not recommend Option A, B, C, or D over the others.
The evidence does not overwhelmingly favor one: Option A has a real,
currently-unsolved sub-problem (§2.2); Option B is currently blocked by a
finding outside this decision's control (§3.1) and violates a named
product requirement as naively conceived (§3.2); Option C is the most
architecturally clean but reaches outside `helix_code`'s own scope into
governance-owned, cross-consumer territory; Option D is cheapest but
explicitly accepts the staleness risk in §7 rather than solving it. This
is a genuine four-way trade-off between engineering cost, cross-project
governance scope, a currently-unstable dependency, and an explicitly
accepted ongoing cost — precisely the shape of decision this packet's
task brief asked to be surfaced for the operator to make, not resolved
here.

## Sources verified

All findings in this document were produced by reading the live source at
`/home/milosvasic/Projects/helix_code` on 2026-09-23 and by running the
commands quoted verbatim in §5 inside an isolated, fully-reverted `git
worktree`. No external web sources were consulted (this is an internal
architecture question, not a library/API question governed by §11.4.99).
Primary internal sources cited: `helix_code/internal/commands/markdown_skills.go`,
`helix_code/internal/commands/governance_skills_registered_test.go`,
`helix_code/internal/agent/skill_dispatcher.go`,
`helix_code/internal/agent/skill_activation.go`,
`helix_code/internal/agent/skill_external_policy.go`,
`helix_code/internal/skillconformance/report.go`,
`helix_code/scripts/generate_builtin_skills.sh`,
`helix_code/go.mod`, `scripts/skills_manifest.tsv`,
`specs/001-helixskills-incorporation/tasks.md` (F-13 and F-17 ledger
rows, read 2026-09-23; the F-17 row is a live snapshot of a document
being concurrently edited by a sibling stream and may have changed by the
time this is read),
`docs/qa/hxc159_f13_builtin_skills_investigation_18ea70e5_20260923T064844Z/README.md`.
