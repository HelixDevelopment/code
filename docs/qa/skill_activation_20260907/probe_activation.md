# Hot-load / hot-unload probe — first-hand evidence
Host: anton  UTC: 2026-09-07T11:51:00Z
claude: 2.1.263 (Claude Code)
CLAUDE_CONFIG_DIR=/home/milosvasic/.claude-claude5

## T0 create -> observed in-session WITHOUT restart
created 2026-09-07T11:50:07Z at .claude/skills/zz-hotload-probe-20260907
session available-skills list gained, verbatim:
  - zz-hotload-probe-20260907: Temporary probe skill created to empirically test whether Claude Code hot-loads skills mid-session. Safe to delete.
=> ACTIVATION hot-load CONFIRMED. Delta rendered as NAME + DESCRIPTION.

## T1 delete -> still invocable in the SAME session
`rm -rf .claude/skills/zz-hotload-probe-20260907` at 2026-09-07T11:51:00Z
`ls` confirms: "No such file or directory" (dir AND SKILL.md gone).
Yet `Skill(zz-hotload-probe-20260907)` in the SAME session STILL launched and
returned the skill body from cache.

=> DEACTIVATION does NOT hot-unload. Asymmetric:
     ACTIVATION   = hot, immediate, mid-session.      PROVEN
     DEACTIVATION = deferred to next session start.   PROVEN (negative finding)

DESIGN CONSEQUENCE: token savings come from a MINIMAL SESSION-START BASELINE
plus on-demand growth — NOT from mid-session shrinking. Any claim that
deactivation frees tokens in the live session would be a bluff (§11.4.6).

## T2 engine-driven activation (end-to-end, not manual mkdir)
`skill_activate.sh session-init` linked constitution/skills/skill-catalog into
.claude/skills/ as a RELATIVE symlink (../../constitution/skills/skill-catalog).
The running session's available-skills list then gained, verbatim:
  - skill-catalog: Lists every skill available to this project — including ones
    not currently active — and activates any of them on demand. [...]
=> ENGINE-DRIVEN activation hot-loads. PROVEN.

## T3 first-party permission mechanism does NOT save tokens
`.claude/settings.local.json` permissions.deny = ["Skill(speckit-analyze)"].
A FRESH headless session (`claude -p`) still listed speckit-analyze among its
available skills. => Skill(name) deny blocks invocation but does NOT remove the
entry from the rendered list; it saves ZERO tokens. Setting reverted; git clean.

## T4 measured surface (exact bytes; tokens are labelled bytes/4 ESTIMATES)
  personal pool ~/.claude/skills  n=955  name-only  29,153 B (~7,288 tok/turn)
                                         name+desc 101,279 B (~25,319 tok/turn)
  project active .claude/skills   n= 19  name-only     422 B (~105 tok/turn)
  manifest CORE only              n=  3  name-only      65 B (~16 tok/turn)
  => description-eager cliff measured at 3.5x, NOT the 14x stated in the brief.
     Neither the description-field sum (101,279 B) nor the full-body sum
     (5,301,328 B) reproduces the brief's 422,777 B figure. Reported, not repeated.

## T5 automated suite
19/19 pass including 5 paired §1.1 mutations (broken-target activation must
refuse + leave no dangling link; dead-claim must be reaped; budget gate must
FAIL in BOTH render modes when squeezed; mode-C must never exit 0).

## Defects this work FOUND (each fixed or reported)
 1. FIXED  engine: session ids from config-dir basenames start with "." so the
           claims glob `*` never matched them — reference counting was written
           but never read, i.e. silently a no-op while reporting itself
           concurrency-safe. Caught by its own paired mutation.
 2. FIXED  engine: relative symlink target resolved against .claude/skills/ and
           dangled; fail-safe caught it and rolled back.
 3. FIXED  engine: wrong helper name (psl_* vs hc_ln_relative).
 4. FIXED  test:  `cmd | grep -q` under `set -o pipefail` gave a false FAIL.
 5. REPORTED (not changed — §11.4.124): 4 of 8 constitution "skills" are not
           loadable as Agent Skills: media-validator, scheduled-work-queue,
           session-sync use lowercase skill.md; multitrack has neither.
