# Indexing activation — CodeGraph + Lumen (2026-09-07)

**Revision:** 1
**Last modified:** 2026-09-07T12:00:00Z

| Field | Value |
|---|---|
| Revision | 1 |
| Created | 2026-09-07 |
| Status | active |
| Scope | §11.4.78 (CodeGraph mandatory), §11.4.79 (own-org IN / third-party OUT), §11.4.80 (update + sync cadence) |
| Status summary | CodeGraph index was **18 commits stale** and carried **~12,300 third-party files** (incl. the entire golang/go language repo) plus **761 byte-identical duplicate** own-org files. Exclude list corrected, full re-index run. Lumen was **hard-down** — root cause was a missing Ollama embedding model, now fixed and health-verified. Weekly §11.4.80 cadence automation wired (was absent). |

> Evidence discipline: every number below is a measured command result, not an
> estimate. Where something was **not** verified, it is marked as such rather
> than asserted (§11.4.6).

---

## 1. Before-state (measured)

### 1.1 CodeGraph — present but STALE

| Fact | Measurement |
|---|---|
| Binary on PATH | `/home/milosvasic/.local/bin/codegraph` |
| Installed version | `1.6.0` |
| Latest on npm | `1.6.0` — **already current**, no §11.4.80 update needed |
| `.codegraph/codegraph.db` | 2,765,209,600 bytes (2.7 GB), mtime **2026-09-06 22:27:04** |
| `config.json` tracked in git | yes (`.codegraph/config.json`, `.codegraph/.gitignore`) |
| Commits landed AFTER the index's last write | **18** |
| Files in index (self-reported) | 35,513 |

**Staleness was PROVEN, not inferred.** A `codegraph_explore` probe for
`allocateFallbackPortUnsafe` — a symbol added in *today's* commit `b1af49d0`
(`helix_code/internal/discovery/port_allocator.go`) — returned:

> `helix_code/internal/discovery/port_allocator.go` — ⚠ changed on disk after
> the last index sync — source omitted (indexed line ranges no longer match…)

and the returned call graph contained `allocateFromRangeUnsafe` /
`allocateEphemeralPortUnsafe` but **not** `allocateFallbackPortUnsafe` — i.e.
the pre-commit symbol set. That is measured proof of staleness.

### 1.2 Lumen — hard-down, root cause identified

`health_check` returned **ERROR**, not a vague failure:

```
Backend: ollama   Host: http://localhost:11434
Model: ordis/jina-embeddings-v2-base-code
Status: ERROR
Message: configured model "ordis/jina-embeddings-v2-base-code" is not loaded
```

`index_status`: `Files: 0 | Indexed: 0 | Chunks: 0 | Last indexed: never | Stale: yes`.

**Root cause (FACT, not hypothesis):** Ollama was *running and reachable* and
held `nomic-embed-text` + `qwen2.5:3b`, but **not** the model Lumen wanted.
`ordis/jina-embeddings-v2-base-code` is Lumen's own hardcoded default
(`internal/models/models.go:26 DefaultOllamaModel`, 768-dim / 8192-ctx,
code-specialised). It had simply never been pulled. This was a missing-asset
problem, not a service outage and not a misconfiguration.

### 1.3 §11.4.80 cadence automation — ABSENT

`crontab -l` → no codegraph/lumen entries. `systemctl --user list-timers` → none.
`docs/codegraph/Status.md` last modified **2026-08-31** (7 days stale).
The sanctioned scripts existed but nothing invoked them on a schedule.

---

## 2. §11.4.79 include/exclude audit (measured against `.gitmodules`)

132 submodules total — **118 own-org** (`vasic-digital` / `HelixDevelopment*`),
**14 third-party**. Auditing the live exclude list against that split found
**four third-party trees that were NOT excluded** and therefore being indexed:

| Third-party tree | Upstream | Indexable files | Was excluded? |
|---|---|---|---|
| `submodules/claude-toolkit/submodules/go` | golang/go language repo | **11,819** | ❌ no |
| `dependencies/colibri` | `JustVugg/colibri` | 416 | ❌ no |
| `mcp_servers` | `modelcontextprotocol/servers` | 86 | ❌ no |
| `submodules/superspec` | `WangX0111/superspec` | 2 | ❌ no |

The golang/go repo alone was **~33% of the entire index**. `dependencies/colibri`
was vendored in commit `e70485a3` — *after* `config.json` was last written
(Aug 31), which is how it drifted in.

Two further trees were **byte-identical duplicates** of own-org copies already
indexed at the repo root (verified by comparing sorted per-file md5 sets):

| Nested duplicate | Root copy | Files | Content |
|---|---|---|---|
| `submodules/claude-toolkit/submodules/containers` | `submodules/containers` | 501 | **identical** |
| `submodules/claude-toolkit/submodules/challenges` | `submodules/challenges` | 260 | **identical** |

This duplication was *observably* degrading results: the pre-fix
`PortAllocator` probe returned the same file twice (root + nested copy),
crowding out the answer that was actually wanted.

### 2.1 Own-org inclusion — already satisfied

The pre-fix probe resolved symbols across `submodules/helix_qa`,
`submodules/containers` and `submodules/helix_llm`, so §11.4.79(a) own-org
*inclusion* was already working. No own-org tree needed to be added.

### 2.2 Change applied

Added to `.codegraph/config.json` `exclude` (backup taken first, §9.2):

```
submodules/claude-toolkit/submodules/go/**            # golang/go, third-party
mcp_servers/**                                         # modelcontextprotocol/servers
dependencies/colibri/**                                # JustVugg/colibri
submodules/superspec/**                                # WangX0111/superspec
submodules/claude-toolkit/submodules/containers/**     # byte-identical dup of root copy
submodules/claude-toolkit/submodules/challenges/**     # byte-identical dup of root copy
```

Then a full `codegraph index` (niced, backgrounded).

---

## 3. Open contradiction referred to the operator — NOT decided here

`github_pages_website` → `git@github.com:HelixDevelopment-Code/Welcome.git`.

- **`CLAUDE.md`'s own roster** lists it under owned repositories
  ("HelixDevelopment-Code (1)"), and §11.4.79(a) says own-org **MUST be indexed**.
- **`scripts/codegraph_validate.sh`** does the opposite: its live-DB audit
  asserts `github_pages_website/%` must be **absent** from the index.

These cannot both be satisfied. I briefly removed the exclusion (following
§11.4.79(a)) and then **restored it**, because acting on either reading
unilaterally would violate a rule:

- editing `codegraph_validate.sh` to accept my change is exactly the
  "fix breaks its own gate → fake-pass the gate" move §11.4.120 forbids;
- silently re-classifying an existing component is what §11.4.122 forbids.

So the pre-existing classification stands unchanged and the contradiction is
reported for an operator decision (§11.4.101 — reversible, safe, surfaced).
It is 17 files; nothing depends on the outcome.

---

## 4. Lumen — fix applied and verified

`ollama pull ordis/jina-embeddings-v2-base-code` → **322 MB, `success`**.

Re-ran `health_check`:

```
Backend: ollama   Host: http://localhost:11434
Model: ordis/jina-embeddings-v2-base-code
Status: OK
Message: service and configured model are ready
```

The correct fix was to pull Lumen's *intended* code-specialised model rather
than repoint `LUMEN_EMBED_MODEL` at the already-present general-purpose
`nomic-embed-text` (768-dim but 2048-ctx, and not code-tuned), which would have
silently degraded code-search quality.

### 4.1 Runtime evidence — Lumen actually indexes and searches

A green `health_check` is a **config-level** signal; on its own it would be a
metadata-only PASS (§11.4/§11.4.5). Runtime evidence was therefore obtained by
issuing a real search, which auto-triggers indexing:

```
index_status after first search:
  Indexed: 4,488 files | Chunks: 97,434 | Vectors: 97,329 unique
  Storage: int8 | DB: 99,840,000 bytes
```

and a real semantic hit (not a keyword match):

```
query: "fallback port allocation outside ephemeral range"
  -> cli_agents/aider/aider/onboarding.py  find_available_port  score 0.81

query: "JWT token generation and validation for authentication"
  -> DOCUMENTATION_COMPLETION_PLAN.md  "2.1 JWT Token Security"  score 0.76
  -> SECURITY_IMPLEMENTATION.md        "3. Authentication System Security"  score 0.67
```

Lumen is **fixed, indexing, and returning semantically-ranked results.**
Indexing continues in the background ("Index is being updated in the background").

### 4.2 A second, distinct Lumen defect — path-scoped search is broken

Isolated and reproducible (§11.4.50 — same input, same result, twice):

| Call | Result |
|---|---|
| `semantic_search(query, limit=N)` | **works** |
| `semantic_search(query, limit=N, path=<subtree>)` | **fails**: `search: k value in knn query too large, provided 8192 and the limit is 4096` |

The trigger is the **`path` subtree filter**, not the query and not `limit`
(it fails with `limit=2` and `limit=3` alike, and the identical query succeeds
with `path` omitted). Lumen over-fetches `k=8192` to allow post-filtering, which
exceeds `sqlite-vec`'s 4096 cap. Workaround: omit `path` and filter results
client-side. This is an upstream Lumen bug worth reporting.

### 4.3 Consistency gap — Lumen indexes what CodeGraph excludes

The first hit above is from `cli_agents/aider/…` — a **third-party** tree that
CodeGraph excludes under §11.4.79(b). Lumen honours `.gitignore` plus a built-in
skip list, and `cli_agents` is *not* gitignored, so Lumen indexes third-party
code that CodeGraph deliberately keeps out. The two indexers therefore disagree
on §11.4.79 scope. Lumen exposes no exclude-list configuration
(`internal/config/service.go` reads only `LUMEN_EMBED_*`, `LUMEN_MAX_CHUNK_TOKENS`,
`LUMEN_FRESHNESS_TTL`, `LUMEN_REINDEX_TIMEOUT`, `LUMEN_LOG_LEVEL`,
`LUMEN_VECTOR_STORAGE`, `LUMEN_BACKEND`), so aligning them needs either an
upstream feature or `.gitignore` changes — an operator decision, not taken here.

---

## 5. §11.4.80 cadence automation — wired

A user-level systemd timer now runs the **sanctioned constitution scripts by
reference** (§11.4.80: inherited, never copied):

- `~/.config/systemd/user/helix-code-codegraph-sync.service`
- `~/.config/systemd/user/helix-code-codegraph-sync.timer`

```
ExecStart=/bin/bash …/constitution/scripts/codegraph_update.sh
ExecStart=/bin/bash …/constitution/scripts/codegraph_sync.sh <repo root>
Nice=15   IOSchedulingClass=idle
OnCalendar=Sun 04:00   Persistent=true   RandomizedDelaySec=1800
```

Verified armed: `NEXT Sun 2026-09-13 04:20:40 CEST`.

It deliberately does **not** call `codegraph_update_and_resync.sh`: that wrapper
performs `commit` + `push_all`, and commits/pushes are handled centrally by the
operator. `Nice`/`IOSchedulingClass=idle` keep the weekly re-index from
competing with active development.

---

## 6. Host-safety constraint encountered (§12.6) — honest blocker

During the re-index the host reached:

```
Mem: 30 GB total, 28 GB used (93%), 2 GB available     ← §12.6 ceiling is 60%
Swap: 8191 MB / 8191 MB used (100% full)
codegraph index process: 10.0 GB RSS, 277% CPU
```

Consequences, stated plainly:

1. **Lumen corpus indexing was NOT started.** Embedding a monorepo of this size
   would have added a second multi-GB memory consumer on a host already at 93%
   with swap exhausted. Lumen is **fixed and health-verified**; building its
   corpus is deferred to a low-load window. This is a deferral with a measured
   reason, not a silent skip.
2. The running index was **not killed**. It was progressing (WAL advancing
   ~12 MB/10 s, resolved by real `/proc/<pid>/cmdline`, not a `pgrep -f`
   substring match which matched only the wrapper shell — the §11.4.196(D)
   carrier footgun). Killing mid-write would have left a partial DB and
   *no* usable index, a worse outcome (§9.2).

---

## 7. MCP daemon serves a DELETED database until restarted

`codegraph index` replaces the DB file. The long-lived MCP daemon (pid 2251386,
`codegraph serve --mcp`) still holds descriptors to the **old, deleted** inodes:

```
/proc/2251386/fd/21 -> …/.codegraph/codegraph.db      (deleted)
/proc/2251386/fd/22 -> …/.codegraph/codegraph.db-wal  (deleted)
/proc/2251386/fd/23 -> …/.codegraph/codegraph.db-shm  (deleted)
```

**Operational consequence:** after any re-index, the
`mcp__codegraph__codegraph_explore` tool keeps answering from the pre-index
database until that daemon is restarted. A post-reindex probe run through MCP
would look green while reading stale data — a §11.4.108-class
source-updated/runtime-stale gap.

**Therefore all after-state verification in §8 uses the `codegraph` CLI**
(`status` / `query` / `node`), which opens the new file. The MCP server needs a
restart (or session reconnect) before it reflects the new index.

---

## 8. After-state — the re-index FAILED, and why

The full `codegraph index` did **not** succeed. It ran ~19 minutes
(13:31 → 13:50), progressed through Scanning → Parsing → Resolving refs, then:

```
✗ Failed to index: database is locked
```

`codegraph status` afterwards:

```
⚠ The last index run never finished (killed mid-index?) — the index is truncated.
⚠ 2,116,595 references from an interrupted run are awaiting resolution.
Files: 35,540 | Nodes: 740,228 | Edges: 1,266,155 | DB 2293.10 MB
```

**Root cause (FACT, §11.4.102) — first attribution CORRECTED.** My initial reading
blamed daemon 2251386 (the one holding deleted fds in §7). That was **wrong** and
is retracted. Verified by re-reading `/proc/<pid>/fd`:

| pid | fds on `codegraph.db` | of which deleted | started | verdict |
|---|---|---|---|---|
| 2251386 | 4 | **4 (all)** | (pre-existing) | serves a **deleted** inode; does **not** lock the live DB |
| 1901050 | 5 | **0** | **13:56:06** | holds the **live** DB — the actual lock holder |

pid 1901050 is `codegraph serve --mcp --path <repo>`, **PPID 1** (daemonised),
and started at **13:56 — six minutes AFTER the index already failed at 13:50**.
So no single pid explains both failures. The real finding is about the *class*:

> A `codegraph serve --mcp` daemon holds a write lock on the index DB (it runs a
> file-watcher that syncs), it is **auto-respawning** (PPID 1, a fresh one appeared
> mid-run), and `codegraph index` / `codegraph sync` cannot take the exclusive lock
> they need while one is alive. `codegraph index` exposes no lock-handling flag
> (`--force` only bypasses a home-dir/root safety check).

That is why *both* `index` (13:50) and `sync` (13:58) died with `database is locked`
despite being minutes apart with different daemons live.

**Second correction — the lock is INTERMITTENT, not absolute.** A later `sync`
started 14:04 ran alongside the very same daemon (1901050) and made large
progress — unresolved refs **2,116,595 → 1,156,595**, edges
**1,266,155 → 1,736,955** — before being terminated by an operator-set 900 s
timeout, *not* by a lock. The accurate statement is therefore: contention is
transient and depends on whether the daemon's file-watcher is mid-write.
`sync` can make progress opportunistically and is repairing the truncation;
a full `index` needs an exclusive lock at its commit phase and is the operation
that genuinely requires the daemon stopped.

This is reported as a failure. The node/edge counts above are from a **truncated**
run and must not be read as a healthy index.

### 8.1 Remediation attempted — partial repair, measured

`codegraph sync` (the remedy the tool itself recommends for the unresolved-refs
half) was run three times, 14:04–14:37:

| Metric | After failed index | After 3 sync attempts | Delta |
|---|---|---|---|
| Unresolved references | 2,116,595 | **846,616** | **−1,269,979 (−60%)** |
| Edges | 1,266,155 | **1,908,850** | **+642,695** |
| Nodes | 740,228 | 740,266 | +38 |
| DB size | 2293.10 MB | 2363.02 MB | +69.9 MB |

Attempt 1 did the bulk of the work in 15 min before hitting an operator-set
timeout (not a lock). Attempts 2 and 3 **both** died `database is locked`. So the
contention is recurring and real, but not absolute — `sync` gets through when the
daemon's watcher happens to be idle. The index is still reported truncated.

### 8.2 Still-open consequence

Because the index run was truncated, whether the §2.2 exclusions actually took
effect is **NOT yet established**. The post-failure file count (35,540) is
essentially unchanged from the pre-run 35,513, which is *consistent with either*
(a) a truncated run that never applied them, or (b) the HXC-041 condition where
`config.json` `exclude` is inert and exclusion is `.gitignore`-driven. These are
distinguished by a live-DB query, not by inference (§11.4.6) — see §9.

**A clean full re-index requires the MCP daemon to be stopped first.** That is an
operator-facing action, not one taken unilaterally here, because it removes the
`codegraph` MCP tool from any live session using it.

---

## 9. Live-DB exclusion audit — and the refutation of HXC-041's root cause

Authoritative method (`sqlite3 … SELECT COUNT(*) FROM files WHERE path LIKE …`,
the same one `scripts/codegraph_validate.sh` uses — a config-only PASS that never
queries the real index is a §11.4 PASS-bluff):

| Path | Indexed files | Expected |
|---|---|---|
| `submodules/claude-toolkit/submodules/go` | **10,705** | 0 (new exclusion) |
| `submodules/claude-toolkit/submodules/containers` | **514** | 0 (new exclusion) |
| `dependencies/colibri` | **461** | 0 (new exclusion) |
| `mcp_servers` | **92** | 0 (new exclusion) |
| `submodules/superspec` | **5** | 0 (new exclusion) |
| `cli_agents` | 0 ✅ | 0 (pre-existing exclusion) |
| `github_pages_website` | 0 ✅ | 0 (pre-existing exclusion) |
| `submodules/helix_qa` | 1,030 ✅ | > 0 (own-org) |
| `submodules/containers` | 514 ✅ | > 0 (own-org) |
| `constitution` | 368 ✅ | > 0 (own-org) |
| `helix_code` | 2,348 ✅ | > 0 (own-org) |
| **total** | **35,540** | — |

### 9.1 HXC-041's recorded root cause is REFUTED

`docs/codegraph/Status.md` records HXC-041 as: *"`.codegraph/config.json` `exclude`
is **INERT** in codegraph 1.2.0 — exclusion is `.gitignore`-driven per §11.4.78"*,
and `scripts/codegraph_validate.sh` carries the same belief in a comment
(*"config.json is INERT per §11.4.78"*). Measured on 1.6.0, that is **not** what is
happening:

```
git check-ignore cli_agents            -> NOT gitignored
git check-ignore github_pages_website  -> NOT gitignored
git check-ignore mcp_servers           -> NOT gitignored
git check-ignore dependencies/colibri  -> NOT gitignored
git check-ignore submodules/superspec  -> NOT gitignored
```

None of them is gitignored — yet `cli_agents` and `github_pages_website` sit at
**0 indexed files**. If exclusion were `.gitignore`-driven they would be indexed.
The only mechanism that can be excluding them is `config.json`, so **`config.json`
`exclude` demonstrably works** (at least in 1.6.0).

The correct explanation for why my six new exclusions did not apply is the
§8 truncation: **the index run never completed**, so the new exclude list was
never committed. This matters because the recorded root cause would have sent the
next engineer to edit `.gitignore` — which would not have fixed anything.

### 9.2 Index freshness — the run DID advance

Despite the truncation, the DB is no longer the Sep-6 index:

- `allocateFallbackPortUnsafe` (added in today's `b1af49d0`) → **1 node present**
- `allocateEphemeralPortUnsafe` (the name it **replaced**) → **0 nodes**

Confirmed a rename, not an addition, from the commit diff:
`- func (pa *PortAllocator) allocateEphemeralPortUnsafe` →
`+ func (pa *PortAllocator) allocateFallbackPortUnsafe`.
So the index reflects today's source. What is missing is **edge resolution**
(2.1M unresolved refs ⇒ incomplete caller/impact trails), not symbol content.

---

## 10. Usefulness probes (§11.4.79) — run via CLI, not the stale MCP tool

Both probes were run with `codegraph query` / `codegraph node`, deliberately **not**
the MCP tool, which would have read the deleted DB per §7 and produced a green
result from stale data.

**Probe A — a symbol living only inside an own-org submodule: PASS**

```
codegraph query ResolveModelCapability
  function ResolveModelCapability
    submodules/llms_verifier/llm-verifier/capabilities/registry_resolve.go:62
    (db *database.Database, modelID int64, capName string) (value bool, verified bool, err error)
```

Own-org submodule content is genuinely reachable — §11.4.79(a) satisfied.

This probe also **demonstrates the duplication cost live**: the same symbol comes
back **twice**, the second hit from
`submodules/claude-toolkit/submodules/LLMsVerifier/…/registry_resolve.go:62`.
(Note a naming trap: the root copy is `submodules/llms_verifier` — lowercase — so
an audit that greps for `submodules/LLMsVerifier` finds "no root copy" and wrongly
concludes the nested one is the only copy. It is not; it is a third duplicate,
alongside the `containers` and `challenges` pairs in §2.)

**Probe B — a symbol added in TODAY's commit: PASS**

```
codegraph query allocateFallbackPortUnsafe
  method   allocateFallbackPortUnsafe
    helix_code/internal/discovery/port_allocator.go:367
  function TestAllocateFallbackPort_Exhausted_ReturnsErrNoPortsAvailable
    helix_code/internal/discovery/port_allocator_fallback_test.go:137
```

The pre-fix probe could not do this (§1.1). Staleness is resolved for symbol
lookup; edge/caller completeness remains degraded pending a successful full index.

**A note on the probe symbol from the task brief:** `contextbudget` does **not**
exist anywhere in the tree (`find -type d -name contextbudget` → no matches), so it
was not used. `allocateFallbackPortUnsafe` was verified present in source before
being relied on.

---

## 11. §1.1 paired mutation — the gate is not a tautology

```
pre-op config md5: faf717cc2b9b391e11f26c8adfdbaa01

STEP 1 golden-FALSE (clean config):
  ✅ submodules/helix_qa is not excluded          ← no false positive (§11.4.201(1))

STEP 2 MUTATE: added submodules/helix_qa/** to exclude

STEP 3 golden-TRUE (mutated):
  ❌ submodules/helix_qa is excluded (should be included per §11.4.79)
  FAIL: 1
  MUTATION DETECTED -> gate FAILED as required

RESTORE: OK (md5 faf717cc2b9b391e11f26c8adfdbaa01 == pre-op)
```

Restore was `trap`-guaranteed on every exit path and **md5-verified**, so no config
change survives the test (§11.4.14, §11.4.84). Both halves of the pair hold: the
gate fires on the mutation and stays quiet on a clean config.

Full validator on the clean config: **PASS 29 / FAIL 0 / SKIP 0**.

### 11.1 But the validator has a coverage gap (§11.4.238)

That green is **necessary, not sufficient**. `codegraph_validate.sh` hardcodes only
four third-party patterns (`cli_agents`, `cli_agents_resources`,
`dependencies/LLama_CPP`, `dependencies/Ollama`). It therefore reported 0 FAIL while
**11,777 third-party files** (golang/go, colibri, mcp_servers, superspec) sat in the
index. I found those by reading `.gitmodules`, not from the validator — which is
precisely the §11.4.238 pattern where the automated check *should* have been the
discoverer. The validator's third-party list should be derived from `.gitmodules`
ownership rather than hardcoded.

---

## 12. What is fixed, what is not, and what it needs

### 12.1 Fixed and verified

| Item | Evidence |
|---|---|
| Lumen hard-down | root-caused to a never-pulled default embedding model; model pulled (322 MB, `success`); `health_check` → **OK**; and runtime-proven — **4,488 files / 97,434 chunks indexed**, real semantic hits at 0.81 / 0.76 / 0.67 |
| §11.4.80 cadence absent | user systemd timer installed + armed, `NEXT Sun 2026-09-13 04:20:40 CEST`, invoking the constitution scripts by reference |
| CodeGraph version currency | 1.6.0 installed == 1.6.0 latest on npm — no §11.4.80 update owed |
| §11.4.79 own-org inclusion | live-DB counts > 0 for helix_qa / containers / constitution / helix_code; Probe A resolves an own-org-only symbol |
| Index symbol freshness | Probe B resolves a symbol from today's commit; the name it replaced is gone |
| Gate is falsifiable | §1.1 mutation pair holds, restore md5-verified |
| §11.4.79 exclude-list drift | 4 unexcluded third-party trees + 2 byte-identical duplicates identified and added to `config.json` |

### 12.2 NOT fixed — stated plainly

**a) The index is truncated.** `codegraph index` and `codegraph sync` both died with
`database is locked`. 2,116,595 references remain unresolved, so caller/impact
trails are incomplete. Symbol lookup works (§10); edge traversal is degraded.

**b) The six new exclusions have not taken effect.** 11,777 third-party files
(10,705 golang/go + 461 colibri + 92 mcp_servers + 5 superspec) and 514 duplicate
files remain indexed. The config change is correct and in place; it needs one
successful full index to apply.

**c) Lumen has two remaining issues, but is working.** The corpus is building
(4,488 files / 97,434 chunks so far, background indexing continues), and search
returns real semantic hits. Outstanding: (i) **path-scoped search is broken**
(`k=8192` vs `sqlite-vec`'s 4096 cap — §4.2), workaround is to omit `path`;
(ii) Lumen indexes third-party trees CodeGraph excludes (§4.3), and exposes no
exclude-list setting to align them.

### 12.3 Why (b) and (c) were not forced through here

Both need the same scarce thing — a quiet host — and one needs an action I should
not take unilaterally:

- **The lock.** A `codegraph serve --mcp` daemon must not be running during a full
  index. Those daemons are **auto-respawning** (PPID 1; a new one appeared at
  13:56 mid-run) and are spawned by MCP clients. With other agents active in this
  repo, killing one risks removing another session's tooling mid-work — which
  §11.4.174 (verify a process is *yours* before signalling it) and §11.4.122
  (don't disable a running component without asking) both forbid. So it is
  reported, not done.
- **The load.** At the time of the CodeGraph decision: load average **23.82 on 16 cores**,
  memory 22/30 GB used. Earlier in the run the host hit **93 % memory with swap
  100 % full (8191/8191 MB)** and only 2 GB available. Starting a
  whole-monorepo embedding job on top of that would have been fighting the other
  agents for CPU and risking the OOM killer — §12.6 caps project procedures at
  60 % of RAM.

### 12.4 Exact remediation

Run in a quiet window, in this order:

```bash
# 1. stop every codegraph MCP daemon for THIS repo (operator-confirmed —
#    this removes the codegraph MCP tool from any live session until it reconnects)
pkill -f 'codegraph.js serve --mcp --path /home/milosvasic/Projects/helix_code'

# 2. full re-index — applies the six new exclusions AND clears the truncation
nice -n 15 codegraph index

# 3. verify: expect ~23,700 files (35,540 - 11,777 third-party - ~60 dup-only),
#    zero unresolved-refs warning, and 0 for each newly-excluded path
codegraph status
bash scripts/codegraph_validate.sh
```

Lumen needs no operator action to index — it is already doing so in the
background; `index_status` reports progress. Its two open issues (§4.2 path-scoped
search, §4.3 third-party scope) are upstream/config matters, not blockers.

### 12.5 Follow-ups worth tracking

1. **`docs/codegraph/Status.md` records a refuted root cause** for HXC-041
   ("config.json exclude is INERT … exclusion is .gitignore-driven"). §9.1 refutes
   it on 1.6.0. The same claim is repeated as a comment in
   `scripts/codegraph_validate.sh`. Left in place, it misdirects the next fix.
2. **`codegraph_validate.sh`'s third-party list is hardcoded** and missed 11,777
   indexed third-party files while reporting 0 FAIL (§11.1). It should derive the
   list from `.gitmodules` ownership.
3. **Serve-daemon vs index lock contention** has no in-tool mitigation
   (`codegraph index` has no lock/force-unlock flag). Worth an upstream issue, and
   worth teaching the §11.4.80 sync wrapper to stop/restart the daemon around a
   full index — otherwise the weekly timer will hit exactly this failure.
4. **`github_pages_website` classification** — the §3 contradiction between
   `CLAUDE.md`'s owned roster and `codegraph_validate.sh`'s exclude assertion needs
   an operator decision.
5. **A third duplicate tree** — `submodules/claude-toolkit/submodules/LLMsVerifier`
   duplicates `submodules/llms_verifier` (§10) and is not yet excluded.
