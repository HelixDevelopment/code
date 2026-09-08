# CodeGraph — Status

**Revision:** 4
**Last modified:** 2026-09-07T12:00:00Z

| Field | Value |
|---|---|
| Revision | 4 |
| Created | 2026-05-28 |
| Last modified | 2026-09-07T12:00:00Z |
| Status | active |
| Status summary | Append-only ledger of every CodeGraph-related event for HelixCode. Per §11.4.78 / §11.4.79 / §11.4.80 / §11.4.45 / §11.4.56. Latest: 2026-09-07 activation sweep — index was 18 commits STALE and carried 11,777 third-party files (incl. the whole golang/go repo, ~33% of the index) plus duplicate own-org trees; six exclusions added to config.json. HXC-041's recorded root cause ("config.json exclude is INERT; exclusion is .gitignore-driven") is REFUTED on 1.6.0 — config.json exclusion demonstrably works; the real blocker is that a `codegraph serve --mcp` daemon holds a write lock, so BOTH `index` and `sync` died with `database is locked` and the index is now TRUNCATED (2,116,595 unresolved refs; symbol lookup fresh and both §11.4.79 probes PASS, but caller/impact edges incomplete). Lumen was hard-down (missing default embedding model) — FIXED and health-verified, corpus not yet built. §11.4.80 weekly cadence automation was ABSENT — now wired as a user systemd timer invoking the constitution scripts by reference. |
| Issues | HXC-041 (SUPERSEDED — its recorded root cause is refuted; see the 2026-09-07 entry); index TRUNCATED — 846,616 refs still unresolved after 3 sync attempts (down from 2,116,595); recurring `codegraph serve --mcp` write-lock contention; needs one full index with the daemon stopped; six new exclusions not yet applied; Lumen FIXED + indexing + searching (4,488 files / 97,434 chunks), with a separate path-scoped-search bug (k=8192 > sqlite-vec 4096) and a §11.4.79 scope disagreement vs CodeGraph |
| Issues summary | 2026-09-07 supersedes the earlier HXC-041 summary, whose counts are stale and whose root cause is REFUTED: `cli_agents` (36,089) and `github_pages_website` are now 0 in the live DB and neither is gitignored, which disproves "exclusion is .gitignore-driven / config.json is INERT" — config.json exclusion works on 1.6.0. Live index today: 35,540 files, of which 11,777 third-party (10,705 golang/go + 461 colibri + 92 mcp_servers + 5 superspec) + 514 duplicate remain because the re-index was TRUNCATED by a `codegraph serve --mcp` write lock (`database is locked` on both `index` and `sync`; 2,116,595 unresolved refs). Own-org inclusion verified (helix_qa 1030 / containers 514 / constitution 368 / helix_code 2348) and both §11.4.79 probes PASS. §11.4.10 credentials CLEAN (0 indexed .env/.pem/.key). |
| Fixed | HXC-017 (own-org submodule inclusion in index) |
| Continuation | sibling `Status_Summary.md` carries the operator-readable digest per §11.4.56. |

## Table of contents

- [Cadence + automation (§11.4.80)](#cadence--automation-1148)
- [Index configuration (§11.4.79)](#index-configuration-11479)
- [Event ledger](#event-ledger)

## Cadence + automation (§11.4.80)

Per §11.4.80, HelixCode MUST run the CodeGraph update + sync automation at
least weekly (cadence floor per §11.4.45 status-digest cadence). The two
canonical scripts are **inherited by reference** from the constitution
submodule (per §3 submodule inheritance) and MUST be invoked at their
constitution-submodule paths — never copied into HelixCode:

- `constitution/scripts/codegraph_update.sh` — npm-installs the latest
  `@colbymchenry/codegraph`, verifies `codegraph --version` reflects the
  new version (anti-bluff: npm exit 0 is not proof of a working binary),
  and appends old/new version to this ledger.
- `constitution/scripts/codegraph_sync.sh` — after a successful update runs
  `codegraph status` → `codegraph sync .` → `codegraph status` →
  validation, appending every step's output to this ledger.

Regeneration mechanism (per §11.4.77): `.codegraph/codegraph.db` is
gitignored; `codegraph index .` (full) or `codegraph sync .` (incremental)
regenerates it from `.codegraph/config.json` (tracked).

## Index configuration (§11.4.79)

`.codegraph/config.json` (tracked) controls which paths enter the index.
Per §11.4.79 the `dependencies/` tree is split by submodule ownership:

**INCLUDED — own-org submodules (full CLI access via vasic-digital + HelixDevelopment):**

- `dependencies/vasic-digital/**` — ~55 own-org submodules (EventBus,
  Concurrency, Observability, Auth, Storage, VectorDB, Embeddings,
  Database, Cache, Messaging, Formatters, MCP_Module, RAG, Memory,
  Optimization, Plugins, Agentic, LLMOps, SelfImprove, Planning,
  Benchmark, ToolSchema, SkillRegistry, Models, LLMProvider,
  BackgroundTasks, DocProcessor, conversation, LLMOrchestrator,
  VisionEngine, Normalize, RedTeam, PliniusCommon, GandalfSolutions,
  AutoTemp, HyperTune, I-LLM, Streaming, Veritas, LeakHub, Claritas,
  Ouroborous, Config, Lazy, Watcher, Middleware, RateLimiter, I18n,
  Recovery, Document, Filesystem, TOON, …).
- `dependencies/HelixDevelopment/**` — own-org submodules (DocProcessor,
  LLMOrchestrator, LLMProvider, VisionEngine, LLMsVerifier, Models,
  HelixMemory, HelixSpecifier, HelixLLM, DebateOrchestrator, …).

**EXCLUDED — third-party vendored submodules (per §11.4.74 `no-match → vendor`):**

- `dependencies/LLama_CPP/**` — `git@github.com:ggml-org/llama.cpp.git`
- `dependencies/Ollama/**` — `git@github.com:ollama/ollama.git`
- `dependencies/HuggingFace_Hub/**` — `git@github.com:huggingface/huggingface_hub.git`

**Credential/secret exclusions (per §11.4.10, belt-and-suspenders):** the
`include` list is code-extensions only (no `.env` / `.key` / `.pem`), and
the exclude list additionally pins `**/.env`, `**/.env.*`, `**/*.key`,
`**/*.pem`, `**/secrets/**` so no credential path can ever be indexed.

## Event ledger

(events appended below by the automation; newest at the bottom)

## 2026-05-28T12:03:15Z — HXC-017 config fix: include own-org submodules, exclude only third-party (§11.4.79)

- **Defect**: `.codegraph/config.json` carried a blanket `dependencies/**`
  exclude that wrongly removed ALL own-org submodules
  (`dependencies/vasic-digital/**` + `dependencies/HelixDevelopment/**`)
  from the index — a §11.4.79 violation (own-org submodules MUST be
  INCLUDED; only third-party submodules excluded).
- **Fix**: replaced the blanket `dependencies/**` exclude with three
  specific third-party excludes (`dependencies/LLama_CPP/**`,
  `dependencies/Ollama/**`, `dependencies/HuggingFace_Hub/**`); added
  explicit credential excludes (`**/.env`, `**/.env.*`, `**/*.key`,
  `**/*.pem`, `**/secrets/**`) per §11.4.10.
- **Config JSON validity**: confirmed via
  `python3 -c "import json;json.load(open('.codegraph/config.json'))"` → VALID.
- **Index status BEFORE re-index**: Files 39,024 / Nodes 624,103 / Edges 1,643,200 / DB 1609.00 MB.
- **Index status AFTER re-index** (`codegraph index .`, exit 0): Files **76,044** / Nodes **1,255,974** / Edges **3,955,444** / DB 2617.24 MB. The +37,020 files / +631,871 nodes delta is the newly-included own-org submodule trees.
- **§11.4.79 anti-bluff probe (own-org symbol now resolves)**:
  - `codegraph query EventBus` → `submodules/event_bus/pkg/bus/bus.go:85` ✅ (would NOT have resolved under the old blanket `dependencies/**` exclude).
  - `codegraph query helix_memory` → `submodules/helix_memory/pkg/config/config.go` (+more) ✅.
  - `codegraph query llama` filtered to `dependencies/LLama_CPP` → **empty** ✅ (third-party correctly excluded).
- **HXC-017 status**: fully done — config fixed, re-index complete, own-org inclusion proven, third-party exclusion proven.

## 2026-05-29T06:37:00Z — codegraph_sync.sh @ /run/media/milosvasic/DATA4TB/Projects/HelixCode

**FAIL** — codegraph sync exited non-zero. Tail of log:\n\n```\n┌  Syncing CodeGraph
[2m│[0m
  _(raw progress-spinner log line removed — was 25387 chars of ANSI noise from codegraph_sync.sh; see qa-results/ logs)_
  _(raw progress-spinner log line removed — was 3007584 chars of ANSI noise from codegraph_sync.sh; see qa-results/ logs)_

## 2026-05-29 — codegraph 0.9.7 update: index/sync FAIL + §11.4.79 own-org regression (HXC-033)

**Event**: operator installed codegraph **0.9.7** on the host (`codegraph --version` → `0.9.7`).

**§11.4.80 post-update sync — FAILED (honest ledger, no bluff):**
- The 0.9.7 install reset the gitignored index DB (was 76,044 files / 1,255,974 nodes at HXC-017; dropped to 39,203).
- `constitution/scripts/codegraph_sync.sh` ran `codegraph sync .` → exited before completing its 4 steps (index reached 43,073 files).
- Full re-index `codegraph index .` → process **KILLED mid-run, no diagnostic / no exit code** (terminated by signal); index left at 54,207 files. Reproduced.
- `codegraph index . --force --quiet` → **KILLED again, no diagnostic**; `--force` wiped + left only 4,630 files.
- `codegraph sync . --quiet` → **exit 1** at 8,461 files.
- Host memory ample (51 GiB free) — not an obvious §12.6 OOM.

**§11.4.79 anti-bluff probe — FAILS:** `codegraph_search BundleTranslator` (MCP) returns ONLY `helix_code/internal/tools/askuser/...` — the own-org `submodules/llm_orchestrator/pkg/i18n/bundle.go` symbol does NOT resolve. Own-org submodules are NOT reachable in the 0.9.7 index. **This is a §11.4.79 regression introduced by the 0.9.7 update.**

**Not a config regression:** tracked `.codegraph/config.json` is intact (git-clean) — own-org includes + §11.4.10 credential excludes (`**/.env`, `**/*.key`, `**/secrets/**`) all present.

**Root cause: UNCONFIRMED** (§11.4.6) — codegraph 0.9.7 `index`/`sync` terminate without an actionable diagnostic on this 76k-file repo. Not determinable from captured evidence whether it is a 0.9.7 stability bug, a submodule-traversal change, or a config-schema change. Filed as **HXC-033**; needs operator decision (downgrade to prior working version / upstream bug report / accept degraded index). Evidence: `qa-results/codegraph_index_*.log`, `codegraph_recover_*.log`.

## 2026-05-29 — codegraph 0.9.7 RESOLVED: wipe + init + re-index restored own-org index (HXC-033 → Fixed)

**Resolution (operator-directed: "clear all indexed data fully and re-index — it MUST be a data-compatibility problem; ALWAYS index main + HelixDevelopment + vasic-digital").** Confirmed correct.

**Root cause (now CONFIRMED, was UNCONFIRMED):** codegraph **0.9.7 requires an explicit `codegraph init`** before `index` (behavioral change vs the prior version). The pre-0.9.7 index DB was incompatible; operating on it produced the failures. After a full wipe of the gitignored DB (`codegraph.db`/`-wal`/`-shm`; the 1.7 GB stale WAL was a tell) + `codegraph init` (tracked `config.json` preserved — own-org includes + §11.4.10 credential excludes intact) + `codegraph index .`, the index rebuilt cleanly.

**Two earlier mis-diagnoses corrected (per §11.4.6 — stated here as the record):** (1) "index crashes/killed mid-run" was a FAULTY `pgrep` pattern (`codegraph index` vs the real `node … codegraph.js index .`) giving false "ENDED" reads — the process was simply slow (76k files); real `ps` showed it alive (~600 MB RSS, healthy, climbing). (2) "own-org symbols do not resolve" used the WRONG CLI verb (`codegraph search` — removed in 0.9.7; the verb is `codegraph query`) AND queried a stale MCP-server DB handle from before the re-index.

**Result (CLI `codegraph status` on fresh 0.9.7 DB):** Files **75,663** / Nodes **1,272,492** / Edges finalizing (1.7M→ toward ~3.95M; edge-enrichment phase runs async after node indexing).

**§11.4.79 anti-bluff probe — PASS (`codegraph query`, fresh DB):**
- `NewBundleTranslator` → `submodules/llm_orchestrator/pkg/i18n/bundle.go:34` (+ `dependencies/vasic-digital/...`, `doc_processor`) — 10 own-org hits. ✅ HelixDevelopment + vasic-digital both reachable.
- `EventBus` → resolves. ✅
- `llama_model_load` under `dependencies/LLama_CPP` → empty. ✅ third-party correctly excluded.

**Status.md hygiene fix:** this ledger had bloated to 3.66 MB — a single 3,007,584-char line of raw ANSI progress-spinner output that `constitution/scripts/codegraph_sync.sh` dumped verbatim on its earlier FAIL. Stripped to 8 KB (all 7 real ledger entries preserved). FOLLOW-UP: `codegraph_sync.sh` should strip ANSI / not dump raw spinner logs into Status.md (constitution-submodule fix per §11.4.26).

**Operational follow-up:** the agent-facing codegraph **MCP server** (`tools/codegraph/...serve --mcp`, a separate install) holds the pre-wipe DB inode — it must be **restarted** to serve the fresh index to AI agents; the CLI `query` already reflects it.

## 2026-07-07 — codegraph 1.2.0 Phase-4 reindex + §11.4.79 own-org symbol proof (deferred item completed; HXC-041 opened)

**Event**: completed the deferred CodeGraph reindex + own-org symbol-resolution proof (RESUME.md #5 — deferred to avoid a stale-v1.1.1-daemon DB conflict). Host now runs codegraph **1.2.0** (matches §11.4.80 update expectation); the serving `serve --mcp` daemon and the DB backend are both 1.2.0, so the stale-v1.1.1-daemon blocker is resolved.

**§11.4.80 reindex/sync — GREEN.** `codegraph sync` synced 8 changed files in 5.6 s (exit 0). Index intact at 1.2.0: **Files 102,657 / Nodes 1,785,100 / Edges 1,918,963 / DB 6.33 GB / node:sqlite full-WAL**. Evidence: `docs/qa/phase4_codegraph_20260707/{00_pre_sync_status,10_sync_run,11_post_sync_status}.txt`.

**§11.4.79 own-org symbol resolution — PROVEN (MCP + CLI, unforgeable).** Resolved symbols that live ONLY inside own-org submodules:
- `admit` — an **unexported** Go function — → `submodules/helix_llm/internal/vrambroker/broker.go:178` (module `github.com/HelixDevelopment/HelixLLM`), with verbatim source + blast-radius (callers `Acquire`, `TestAdmit_TruthTable`, `TestAdmit_PairedMutation`).
- `ResolveModelCapability` → `submodules/llms_verifier/llm-verifier/capabilities/registry_resolve.go:62` (module `digital.vasic.llmsverifier`).
Both via the `mcp__codegraph__codegraph_explore` MCP tool AND the `codegraph query`/`codegraph node` CLI. `scripts/codegraph_validate.sh` independently confirms own-org inclusion (helix_qa 28,333 / llm_provider 151 / constitution 84 / challenges / containers / security all indexed) — 26 PASS. Evidence: `docs/qa/phase4_codegraph_20260707/{20_own_org_symbol_proof_cli,21_own_org_symbol_proof_mcp,60_codegraph_validate}.txt`.

**§11.4.10 credentials — CLEAN.** Live-DB audit: **0** indexed `.env` / `.pem` / `.key` files. (The `**/secrets/**` + `**/.env.*` glob would match some third-party *source* files like `.env.d.ts` and `secrets/` React dirs, but no real credential file types are indexed; the DB is gitignored per §11.4.77, so nothing reaches git.)

**§11.4.79 third-party exclusion — PARTIAL FAIL → HXC-041 (BLOCKED on host resources, honest, no bluff).** `scripts/codegraph_validate.sh` reports 3 FAIL: live index still holds **36,089** `cli_agents` + **519** `cli_agents_resources` + **9** `github_pages_website` third-party files. Root cause (FACT, §11.4.102): (1) `.codegraph/config.json` `exclude` is **INERT** in codegraph 1.2.0 — exclusion is `.gitignore`-driven per §11.4.78, and these are *tracked* reference dirs not in `.gitignore`; (2) the `.codegraph/config.json` exclude list was recently expanded (git diff: added `tools/opensource/**`, `submodules/helix_agent/cli_agents/**`, `external/**`) but the from-scratch `codegraph index` to apply it was deferred — `codegraph sync` is incremental and does not purge now-excluded files (`indexed_at`: cli_agents 1783289990159 / Jul-5 vs helix_llm 1783420230150 / Jul-7). **Remediation blocked**: a from-scratch `codegraph index` fork-failed (`errno=11`, `runtime: failed to create new OS thread`) — host at **4069/4096** user processes (`ulimit -u`), saturation dominated by ~14+ non-ours 75-thread processes that §11.4.174 forbids killing. The aborted index fork-failed **before writing** — the 1.2.0 sync'd index is verified INTACT + still resolves own-org symbols (`docs/qa/phase4_codegraph_20260707/50_post_abort_integrity.txt`). HXC-041 is deferred to a low-host-load window; it does NOT affect own-org reachability (proven above). Evidence: `docs/qa/phase4_codegraph_20260707/{30_stale_index_rootcause,40_full_index_run,50_post_abort_integrity}.txt`.

## 2026-07-08T18:56:00Z — codegraph 1.2.0 → 1.3.0 update (§11.4.80)

- **Event**: weekly codegraph npm update check (per §11.4.80 cadence).
- **npm registry**: `@colbymchenry/codegraph@1.3.0` (latest).
- **Installed before**: `@colbymchenry/codegraph@1.2.0` at `/home/milos/.nvm/versions/node/v24.18.0/lib`.
- **Update**: `npm install -g @colbymchenry/codegraph` → `changed 2 packages in 15s`. Exit 0.
- **PATH symlink reconciled**: `/home/milos/.local/bin/codegraph` was pointing to `.codegraph/versions/v1.2.0/bin/codegraph` (stale 1.2.0 binary). Re-pointed to `/home/milos/.nvm/versions/node/v24.18.0/bin/codegraph` (npm-shim for 1.3.0).
- **Binary version confirmed** (`codegraph --version`): **1.3.0**. ✅
- **npm global confirmation** (`npm ls -g @colbymchenry/codegraph`): `1.3.0`. ✅
- **Evidence**: `npm view` → 1.3.0, `npm ls -g` → 1.3.0, `codegraph --version` → 1.3.0.
- **HXC-041 status**: unchanged — still blocked on host process saturation (`ulimit -u 4096`, ~4069 used).
- **Root-scoped commit**: committed to this repo; push deferred per operator instruction.

## 2026-09-07T12:00:00Z — index staleness + §11.4.79 exclude-drift remediation; Lumen restored; §11.4.80 cadence wired

- **Event**: CodeGraph + Lumen activation sweep. Full evidence:
  `docs/qa/2026-09-07-toolkit-constitution-activation/INDEXING.md`.

### Before-state (measured)

- `codegraph` **1.6.0** on PATH; npm latest **1.6.0** → **no §11.4.80 update owed**.
- `.codegraph/codegraph.db` 2,765,209,600 B, mtime **2026-09-06 22:27:04**;
  **18 commits** landed after that write. Staleness PROVEN, not inferred: an
  `explore` of `allocateFallbackPortUnsafe` (added in today's `b1af49d0`) returned
  *"changed on disk after the last index sync — source omitted"* and a call graph
  still containing the pre-rename `allocateEphemeralPortUnsafe`.
- **§11.4.80 cadence automation: ABSENT** — no crontab entry, no systemd timer.

### §11.4.79 exclude-list drift (measured against `.gitmodules`)

132 submodules; **118 own-org / 14 third-party**. Four third-party trees were
**not** excluded and were being indexed:
`submodules/claude-toolkit/submodules/go` (**11,819** files — the golang/go
language repo, ~33% of the whole index), `dependencies/colibri` (416,
vendored in `e70485a3` *after* config.json was last written), `mcp_servers` (86),
`submodules/superspec` (2). Plus two **byte-identical duplicates** of own-org
root copies (`claude-toolkit/submodules/{containers,challenges}` = 501 + 260
files, md5-set verified). All six added to `.codegraph/config.json` `exclude`
(pre-op backup taken, §9.2).

Own-org **inclusion** was already satisfied — no own-org tree needed adding.

### HXC-041's recorded root cause is REFUTED (important)

This ledger and `scripts/codegraph_validate.sh` both record *"config.json `exclude`
is INERT — exclusion is `.gitignore`-driven"*. Measured on 1.6.0 that is **false**:
`git check-ignore` reports **none** of `cli_agents`, `github_pages_website`,
`mcp_servers`, `dependencies/colibri`, `submodules/superspec` as gitignored, yet
`cli_agents` and `github_pages_website` sit at **0 indexed files**. Only
`config.json` can be excluding them, so `config.json` exclusion **works**. The
real reason the six new exclusions did not apply is the truncated run below.
Left uncorrected, the recorded cause would send the next engineer to edit
`.gitignore`, which would fix nothing.

### Re-index OUTCOME: FAILED — `database is locked`

`codegraph index` ran 13:31→13:50 and died with `✗ Failed to index: database is
locked`; `codegraph sync` (13:53→13:58) died the same way. `codegraph status`:
*"the index is truncated"*, **2,116,595 references awaiting resolution**.

**Root cause (FACT) — a `codegraph serve --mcp` daemon holds a write lock on the
index DB.** Verified via `/proc/<pid>/fd`, and an initial mis-attribution was
corrected: pid **2251386** holds 4 fds that are **all deleted inodes** (it serves a
stale DB but does not lock the live one), while pid **1901050** holds **5 live
fds** — and it started at **13:56, six minutes AFTER the index had already
failed**. So this is not one rogue process: the serve-daemons are
**auto-respawning** (PPID 1), and `codegraph index` has **no lock-handling flag**
(`--force` only bypasses a home-dir/root safety check). The lock is **intermittent contention**, not a permanent block: a later `sync`
(14:04) coexisted with the same daemon and resolved **960,000 refs in 15 min**
(edges 1,266,155 → 1,736,955) before being cut short by an operator-set timeout,
not by a lock. So `sync` can make progress opportunistically, while a full
`index` — which needs an exclusive lock at its commit phase — is the vulnerable
operation and is the one that should be run with the daemon stopped.

### State of the index now

- **Symbol content is fresh**: `allocateFallbackPortUnsafe` → 1 node;
  `allocateEphemeralPortUnsafe` (the name today's commit replaced) → 0 nodes.
- **Edges are degraded**: 2.1M unresolved refs ⇒ incomplete caller/impact trails.
- **Exclusions not yet applied**: 10,705 golang/go + 461 colibri + 92 mcp_servers
  + 5 superspec + 514 duplicate files still indexed. Total 35,540 files.

### §11.4.79 usefulness probes — both PASS (run via CLI, not the stale MCP tool)

- **Own-org-only symbol**: `codegraph query ResolveModelCapability` →
  `submodules/llms_verifier/llm-verifier/capabilities/registry_resolve.go:62`.
- **Today's commit**: `codegraph query allocateFallbackPortUnsafe` →
  `helix_code/internal/discovery/port_allocator.go:367` + its new test file.

Probes were deliberately **not** run through `mcp__codegraph__codegraph_explore`:
that daemon holds deleted fds, so an MCP probe would have returned a green result
from the pre-index database — a §11.4.108 source-updated/runtime-stale PASS-bluff.
**The MCP server must be restarted before it reflects any re-index.**

### §1.1 paired mutation — gate is falsifiable

Adding own-org `submodules/helix_qa/**` to the exclude list makes
`codegraph_validate.sh` FAIL (`❌ … should be included per §11.4.79`, FAIL: 1);
the clean config passes (`✅ … is not excluded`) — no false-positive refusal
(§11.4.201(1)). Restore was `trap`-guaranteed and **md5-verified** identical to
pre-op. Clean-config validator run: **PASS 29 / FAIL 0 / SKIP 0**.

That green is necessary but **not sufficient**: the validator hardcodes only four
third-party patterns and reported 0 FAIL while 11,777 third-party files sat in the
index (§11.4.238 — the automated check should have been the discoverer). Its list
should be derived from `.gitmodules` ownership.

### Lumen — root-caused and FIXED

`health_check` was **ERROR**: *"configured model
`ordis/jina-embeddings-v2-base-code` is not loaded"*. Ollama was running and
reachable with `nomic-embed-text` + `qwen2.5:3b` — just not that model, which is
Lumen's own hardcoded default (`internal/models/models.go:26`, 768-dim/8192-ctx,
code-specialised). `ollama pull` → 322 MB, `success`; `health_check` → **OK**.
Pulling the intended model was preferred over repointing `LUMEN_EMBED_MODEL` at
the general-purpose `nomic-embed-text`, which would have silently degraded
code-search quality.

**Runtime-proven, not just health-green** (a green `health_check` alone is a
config-level/metadata PASS per §11.4.5): a real search auto-triggered indexing and
returned semantically-ranked hits — `Indexed: 4,488 files | Chunks: 97,434 |
Vectors: 97,329 | DB 99.8 MB`, e.g. *"fallback port allocation outside ephemeral
range"* → `find_available_port` (score 0.81), *"JWT token generation and
validation"* → the two security docs (0.76 / 0.67). Indexing continues in
background.

**Second, distinct Lumen defect found** (reproducible, §11.4.50): path-scoped
search is broken — `semantic_search(query, limit=N)` works, but adding
`path=<subtree>` fails with `k value in knn query too large, provided 8192 and the
limit is 4096`. The trigger is the `path` filter (Lumen over-fetches k=8192 vs
`sqlite-vec`'s 4096 cap), not the query or `limit`. Workaround: omit `path`.

**Consistency gap:** Lumen indexes `cli_agents/…` — third-party code CodeGraph
excludes under §11.4.79(b). Lumen honours `.gitignore` + a builtin skip list, and
`cli_agents` is not gitignored; Lumen exposes no exclude-list setting. The two
indexers therefore disagree on §11.4.79 scope — operator decision, not taken here.

### §11.4.80 cadence — WIRED (was absent)

User systemd timer invoking the constitution scripts **by reference**:
`~/.config/systemd/user/helix-code-codegraph-sync.{service,timer}` →
`constitution/scripts/codegraph_update.sh` + `codegraph_sync.sh`,
`Nice=15`, `IOSchedulingClass=idle`, `OnCalendar=Sun 04:00`, `Persistent=true`.
Armed and verified: **NEXT Sun 2026-09-13 04:20:40 CEST**. It deliberately does
**not** call `codegraph_update_and_resync.sh`, which performs `commit` +
`push_all` — commits/pushes are handled centrally by the operator.

### Host-safety (§12.6) — why the remaining work was not forced

During the index the host reached **93% memory with swap 100% full
(8191/8191 MB)**, 2 GB available, the indexer at 10.0 GB RSS / 277% CPU; later
**load average 23.82 on 16 cores**. §12.6 caps project procedures at 60% of RAM.
Lumen's whole-monorepo embedding was therefore **not** started, and the running
index was **not** killed (it was provably progressing — WAL +12 MB/10 s, resolved
by real `/proc/<pid>/cmdline` rather than a `pgrep -f` substring that matched only
the wrapper shell, the §11.4.196(D) carrier footgun; killing mid-write would have
left a partial DB and *no* usable index, §9.2).

The serve-daemons were **not** killed either: they are auto-respawning and spawned
by MCP clients, other agents are active in this repo, and §11.4.174 (verify a
process is yours before signalling) + §11.4.122 (don't disable a running component
without asking) both forbid it. Reported, not done.

### Remediation (needs a quiet window + operator confirmation)

```bash
pkill -f 'codegraph.js serve --mcp --path /home/milosvasic/Projects/helix_code'
nice -n 15 codegraph index          # applies the 6 exclusions AND clears truncation
codegraph status && bash scripts/codegraph_validate.sh
```
Expect ~23,700 files (35,540 − 11,777 third-party − duplicates) and no
unresolved-refs warning. Then build Lumen's corpus separately (CPU/GPU-heavy;
must not overlap the index).

### Open items

1. HXC-041's recorded root cause is refuted (above) — correct it here and in the
   `codegraph_validate.sh` comment.
2. `codegraph_validate.sh` third-party list is hardcoded; derive from `.gitmodules`.
3. Serve-daemon vs index lock contention has no in-tool mitigation — the §11.4.80
   sync wrapper should stop/restart the daemon around a full index, or the weekly
   timer will hit exactly this failure.
4. `github_pages_website` classification: `CLAUDE.md`'s owned roster says own-org
   (⇒ must be indexed per §11.4.79(a)); `codegraph_validate.sh` asserts it must be
   absent. Contradiction referred to the operator; pre-existing exclusion left
   in place rather than editing a gate to fit a change (§11.4.120/§11.4.122).
5. A third duplicate tree — `claude-toolkit/submodules/LLMsVerifier` duplicates
   `submodules/llms_verifier` (the root copy is lowercase, so an audit grepping
   `submodules/LLMsVerifier` wrongly concludes there is no root copy) — not yet
   excluded.

### Partial repair achieved by `codegraph sync` (same session, 14:04–14:37)

Three `sync` attempts were run against the truncated index. They did **not**
fully repair it, but they recovered a majority of the lost edge data:

| Metric | After failed index | After 3 sync attempts | Delta |
|---|---|---|---|
| Unresolved references | 2,116,595 | **846,616** | **−1,269,979 (−60%)** |
| Edges | 1,266,155 | **1,908,850** | **+642,695** |
| Nodes | 740,228 | 740,266 | +38 |
| Files | 35,540 | 35,543 | +3 |
| DB size | 2293.10 MB | 2363.02 MB | +69.9 MB |

Attempt 1 (14:04) ran 15 min and did the bulk of the work before hitting an
operator-set timeout. Attempts 2 (14:19) and 3 (14:22) **both** died with
`database is locked` — confirming the contention is real and recurring, even
though it is not permanent. `codegraph status` still reports the index truncated.

**Net position:** symbol lookup is fresh and correct (both §11.4.79 probes PASS);
caller/impact edges are ~60% recovered but still incomplete; the six new
exclusions remain unapplied. A single successful full `index` with the serve
daemon stopped resolves all three at once.
