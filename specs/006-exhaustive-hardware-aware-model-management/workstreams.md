# Feature Work-Streams — §11.4.167 Registry

**Feature**: `006-exhaustive-hardware-aware-model-management`
**Created**: 2026-09-12
**Decisions**: CoW feature work-streams first, all three submodules (operator-approved).

## Environment constraints (§11.4.6 — measured, not assumed)

| Fact | Value |
|---|---|
| Filesystem | **ext4** (`stat -f -c '%T'` → ext2/ext3) — **no reflink/CoW support** |
| Disk | 122G free of 1.8T (**94% used**) |
| Trunk submodules | all on `main`; `helix_agent` has **41** uncommitted changes, `claude-toolkit` **1**, `helix_llm` clean |
| Shared object store | submodule `.git` is a gitlink → `helix_code/.git/modules/...` |

## Method chosen

`git worktree` per submodule (the §11.4.167(B) fallback when CoW is unavailable),
with build output externalized. **§11.4.179 tension recorded**: worktrees share the
common `.git` object store, which §11.4.179 discourages when *corruption isolation*
is a requirement. Here it is a **single** feature work-stream (not the
parallel/git-corrupting fleet §11.4.179 targets), CoW is impossible on ext4, and a
full `git clone` per stream would spend scarce disk. This is an explicit,
operator-visible deviation, not a silent one.

## Work-streams

| Submodule | Work-stream path | Branch | HEAD |
|---|---|---|---|
| helix_llm | `/home/milosvasic/Projects/helix_code_ws_006/helix_llm` | `feature/006-hardware-aware-model-management` | `5e096c2` |
| helix_agent | `/home/milosvasic/Projects/helix_code_ws_006/helix_agent` | `feature/006-hardware-aware-model-management` | `6c24b326` |
| claude-toolkit | `/home/milosvasic/Projects/helix_code_ws_006/claude-toolkit` | `feature/006-hardware-aware-model-management` | `46861bb` |

**Disk cost**: ~242M total (helix_llm 56M, helix_agent 174M, claude-toolkit 12M).
**Trunk verified untouched**: `helix_agent` still 41 dirty, `claude-toolkit` 1 dirty.

## Naming (§11.4.181 / §11.4.151)

- Canonical branch: `feature/006-hardware-aware-model-management` — **one name, identical on all three submodules**.
- Release prefix: `HELIX_RELEASE_PREFIX=helix-code` (from `.env`).
- Planned tags: `helix-code-<base>-feat-006-hardware-aware-model-management[-<iter>]` — **not created** until a validated, operator-approved release.

## Rules in force

- **No merge to trunk** until the operator explicitly approves a fully-validated release (§11.4.167(C)/(I), §11.4.195).
- **Trunk merged INTO the stream** regularly (≥ after every trunk tag / daily), `git merge` never rebase (§11.4.188).
- **No force-push, ever** (§11.4.113).
- Streams are **not** a quality carve-out: full review + impact-research + anti-bluff gauntlet applies (§11.4.167(G)).
- Retire only after merge; unmerged work blocks auto-deletion (§9.2).

## Work-stream dependency layout (REQUIRED — discovered 2026-09-12)

`helix_llm/go.mod` carries **36 `replace` directives** of the form
`digital.vasic.X => ../X`. In the original checkout those siblings live at
`helix_code/submodules/X`; a worktree at
`/home/milosvasic/Projects/helix_code_ws_006/helix_llm` resolves `../X` to a
directory that does not exist, so packages such as `internal/brain` **fail to
build** (`replacement directory ../i18n does not exist`).

Remedy applied — symlinks (zero disk cost) in the work-stream base, created once:

```bash
BASE=/home/milosvasic/Projects/helix_code_ws_006
SRC=/home/milosvasic/Projects/helix_code/submodules
cd "$BASE/helix_llm"
for t in $(grep -oE '=> \.\./[A-Za-z0-9_.-]+' go.mod | sed 's|=> \.\./||' | sort -u); do
  [ -e "$BASE/$t" ] || ln -s "$SRC/$t" "$BASE/$t"
done
```

**36/36 resolved, 0 missing.** The same must be done for `helix_agent`
(module `dev.helix.agent`, `replace` targets include `../containers`,
`../debate_orchestrator`) and `claude-toolkit`'s nested Go modules before their
builds are trusted. Symlinks live OUTSIDE the repos and are therefore not
committed — this section is the record.

## Nested submodule initialisation (claude-toolkit) — REQUIRED

A worktree does **not** inherit checked-out nested submodules. Two were needed
and their absence caused **7 test failures** across two suites:

```bash
cd /home/milosvasic/Projects/helix_code_ws_006/claude-toolkit
git submodule update --init --depth 1 submodules/claude-code-router   # 4+1 failures
git submodule update --init --depth 1 submodules/LLMsVerifier         # 3 failures
```

| Submodule | Suites it unblocks | Result after init |
|---|---|---|
| `claude-code-router` | `test_ccr_conformance.sh`, `test_ccr_build.sh` (go-toolchain pin) | 10/0, 53/0 |
| `LLMsVerifier` | `test_providers.sh` (semantic-visibility driver builds from it) | 427/0 |

`submodules/go` (the vendored Go *source* tree) is deliberately **not** initialised
— it is large and the suites that build Go use a fake `go` on PATH, except where
a real toolchain is already reachable via the real HOME.

In `helix_agent`, a third nested submodule was needed:

```bash
cd /home/milosvasic/Projects/helix_code_ws_006/helix_agent
git submodule update --init --depth 1 external/cognee
```

Absent, `TestComposeBuildContextsAreShippable` reported 8 build-context
violations; initialising it dropped that to 7.

## KNOWN WORK-STREAM LIMITATION — directory DEPTH is load-bearing

A work-stream placed at `<parent>/helix_code_ws_006/<repo>` sits **one level
shallower** than the canonical `<parent>/helix_code/submodules/<repo>` layout, and
some paths are written RELATIVE to that canonical depth. Measured:

| From `helix_agent/docker/mcp/` | Canonical layout | Work-stream layout |
|---|---|---|
| `../../../../mcp_servers` | `helix_code/mcp_servers` — **EXISTS** | `/home/milosvasic/Projects/mcp_servers` — **missing** |

This is **not a product defect**: the compose paths are correct for the layout
they were written for. It is the reason the remaining
`TestComposeBuildContextsAreShippable` violations persist in a work-stream and
would not in a normal checkout.

**Consequence for reading results:** a work-stream failure that is a *path
resolution* failure must be re-verified in the canonical layout before being
treated as a real defect — otherwise a work-stream artefact is reported as a
product bug (and, symmetrically, a work-stream PASS on a depth-sensitive path
would prove nothing). Where a work-stream must be trusted for such a path, place
it at the canonical depth instead.

## Honest boundaries (§11.4.6)

- The worktrees are created; **no feature code has been written** yet.
- `claude-toolkit` is a Bash/Python/JSON repo plus nested Go submodules; its stream
  contains the nested submodules (`go`, `LLMsVerifier`, `containers`, …) at their
  committed revisions — the fix targets (`scripts/`, `claude-code-router`) are in scope.
- Tags are **not** created at this stage; creating them would imply a release that has not happened.
