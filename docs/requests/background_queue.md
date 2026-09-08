# Background Work Queue (§11.4.140 `BACKGROUND` action, v2)

**Revision:** 1
**Created:** 2026-07-29
**Last modified:** 2026-07-29T00:30Z
**Status:** active
**Maintainer:** CLI-agent main work stream

Durable, append-only, NEVER-dropped queue of `BACKGROUND ::` requests.

The `BACKGROUND` action's expansion is explicit: **the moment** such a request
is received it MUST be recorded here — with an id, a received-timestamp, a
status and any blocker — so it can never be lost, dropped, or forgotten. It
MUST then be executed: subagent-driven in parallel with all main work if
capacity exists, otherwise left `Queued` with its blocker recorded, retried at
the next opportunity **including the next fresh session**, and re-surfaced to
the operator every session until it reaches a terminal state (`Done` or
`Operator-cancelled`).

Two rules that shape this file:

- **Non-interrupting (v2).** A `BACKGROUND` request runs in PARALLEL with the
  main stream and must NOT interrupt, block, preempt, or divert it. If no
  genuine background capacity exists, the request stays `Queued` here rather
  than being run inline — running it inline would violate the action's purpose.
- **Silently dropping, or deferring-and-forgetting, is FORBIDDEN.**

Composes §11.4.87 / §11.4.94 / §11.4.97 / §11.4.103 / §11.4.126.

## Table of contents

- [Queue](#queue)
- [Status vocabulary](#status-vocabulary)

## Queue

| # | Received (UTC) | Request | Tracked as | Status | Blocker |
|---|---|---|---|---|---|
| BG-001 | 2026-07-29T00:26Z | Fully incorporate the HelixSkills System (`git@github.com:HelixDevelopment/skills.git`) into HelixCode **and** HelixAgent — all power-features, nothing left out; exhaustive phase/task/subtask plan; full §11.4.169 test-type + Challenges + HelixQA coverage; all docs/manuals/guides/FAQs extended with illustrations, graphs and diagrams; every gap/weak-spot/danger-zone detected **in advance** during planning and paired with a rock-solid risk-free solution; progress tracked through continuation + memory/knowledge mechanisms; **GitHub SpecKit** for all phases, bridged to Superpowers, subagent-driven. | **HXC-159** (Task, Queued, High) — SSoT `docs/workable_items.db`; research tree `docs/research/fully_incorporate_the_helixskills_system_into_helixcode_heli_20260728T192622Z_2557683/`; scheduling evidence `qa-results/feature/HXC-159_20260728T192622Z/result.json` | In-progress | none — background capacity available; dispatched subagent-driven in parallel with the main stream |

## Status vocabulary

| Status | Meaning |
|---|---|
| `Queued` | Recorded, not yet started. A `Blocker` MUST be stated. |
| `In-progress` | Actively running in background, parallel to the main stream. |
| `Blocked` | Started or startable but externally blocked; `Blocker` states what and the retry condition. Re-attempted next opportunity, including next fresh session. |
| `Done` | Terminal. Reached a genuinely completed, evidence-backed state (§11.4.197 — never merely acknowledged). |
| `Operator-cancelled` | Terminal. Explicitly cancelled by the operator; the reason is recorded. |

**Honest boundary (§11.4.6):** this queue guarantees a `BACKGROUND` request is
never lost or forgotten. It does NOT by itself guarantee the work is correct —
each request's output still crosses the same gates as any other change:
independent review (§11.4.142 / §11.4.134), four-layer runtime-signature
verification (§11.4.108), and the full-suite retest (§11.4.40).
| BGQ-0001 | 2026-09-07T13:47+02:00 | in-progress | Dynamic on-demand Skill/extension activation so only skills needed at a given moment are active; token use always minimal; implemented in the constitution submodule so every project inherits it | none — subagent dispatched |
| BGQ-0002 | 2026-09-07T15:51+02:00 | in-progress | Enumerate every operator-blocked item, blocker and show-stopper across the session and put them to the operator as interactive decisions so all can be unblocked | none — compiled and surfaced |
| BGQ-0003 | 2026-09-08T14:12+02:00 | done | Delegate long mechanical work to properly written bash scripts / small Go utilities instead of burning tokens on it; scan every project regularly and refactor to extract such work; encode as a mandatory constitutional rule so all projects do it; full documentation, user guides, manuals, and tests producing deterministic machine evidence | DONE — §11.4.274 minted and published to all 6 constitution upstreams; `constitution/scripts/mechanical/` landed at 1f67272 (15 files, 181 assertions, 6 suites). Acceptance was reproducing a MEASURED session outcome, not a synthetic one: `MUTATION D5e: EXPECTED-FAIL got 23 failed / 35 passed [OK]`. Five instrument defects were found and fixed while building it, two of them only on first real use against a 1.8 GB repository. |
