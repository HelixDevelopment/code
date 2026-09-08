# AUDIT — Which constitution mechanisms actually load for each Claude Toolkit alias?

**Date:** 2026-09-07
**Host:** `anton`
**Auditor:** `(T1/main - claude5 - opus - high)`
**Mode:** READ-ONLY. No file outside this directory was modified; no installer, indexer, or
sync command was run.
**Epistemic convention (§11.4.6):** every claim below is tagged **FACT** (measured, with the
command that produced it), **ESTIMATE** (derived, with method stated), or **UNKNOWN** (could
not be measured). Nothing is asserted from inference alone.

---

## 0. Executive summary — the one-paragraph answer

**Almost none of the constitution's mechanisms load per-alias. What governance does reach an
alias reaches it from the *project directory*, not from the alias.** All 34 Claude config
dirs symlink their user-scope `CLAUDE.md` to a single shared file — and **that shared file is
zero bytes** (FACT). 32 of 34 alias config dirs have a `settings.json` containing *only*
`enabledPlugins` — **no hooks, no MCP servers, no permissions** (FACT). The §11.4.109
`guard-forbidden-commands.sh` hook is wired **nowhere on this host** (FACT). The 8 Kimi
aliases have config dirs containing **one file each (`config.toml`)** — no governance surface
of any kind (FACT). The mechanisms that *do* work — the 283-plugin set, the CodeGraph MCP
server, the §11.4.182 track-label guard, and the ~446 KB project `CLAUDE.md` — work because
they are **uniform plugin state or project-scoped files**, and they consequently stop working
the moment an alias is launched outside `helix_code`.

**Two findings deserve separate billing.** First, **CodeGraph is genuinely healthy** — v1.6.0
(already latest), `helix_code` indexed at 35,514 files / 739,702 nodes / 2.38 M edges, with all
68 `submodules/*` own-org paths present. It is the one mechanism in this audit that works as
designed, though it reaches aliases only project-scoped and its §11.4.79 exclude list has
three deviations. Second, **Lumen is not merely broken, it is misleading**: all 7 of its
project indexes hold **0 files and 0 chunks** (FACT, read from every `index.db`), root-caused
to a missing embedding model in Ollama — while a `PreToolUse` hook on this host actively
advises agents to prefer Lumen's semantic search *over* `grep`. An agent following that advice
receives empty results that are indistinguishable from a genuine absence. That is a live
§11.4-class bluff surface, and it has been live for at least a week.

---

## 1. Alias inventory

**Source of truth (FACT):** `~/.bashrc:66` sources
`/home/milosvasic/.local/share/claude-multi-account/aliases.sh` (106,189 bytes, 1,759 lines,
generated/managed — "do not edit inside"). `grep -c '^alias '` → **43 aliases**.

| Class | Count | Dispatch function | Config dir pattern |
|---|---|---|---|
| Native Claude Code | **5** (`claude1`…`claude5`) | `cma_run` | `~/.claude-claude<N>` |
| Claude Code over a provider | **30** alias names → **29** distinct dirs | `cma_run_provider` | `~/.claude-prov-<id>` |
| Kimi Code over a provider | **8** (`kimi-helixagent`, `kimi-helixagent-native`, `kimi-helixllm-gateway`, `kimi-hyper1`…`kimi-hyper5`) | `cma_run_kimi_provider` | `~/.kimi-prov-<id>` |
| **Total launchable aliases** | **43** | | |

Notes:

- **`opencode` and `opencode-zen` both map to provider id `opencode`** (FACT — two alias lines,
  one config dir). Hence 30 alias names but 29 provider dirs.
- **`kc-*` vs `kimi-*` namespace (FACT, `claude-providers.sh:1225-1229`, v1.27.0):** `kc-*` ids
  are **Claude Code over a Kimi backend** and are ordinary `cma_run_provider` provider aliases
  (`kc-for-coding`, `kc-for-coding2`). `kimi-*` ids are the **Kimi Code binary** over a backend
  (`cma_run_kimi_provider`). The rename vacated the `kimi-*` namespace for the second sense.
  These are two different CLI agents, not two spellings of one thing.
- **There are no native Kimi aliases** (FACT). There is no `kimi1`…`kimiN` analogue of
  `claude1`…`claude5`. Every Kimi alias is provider-routed. The Kimi binary exists at
  `/home/milosvasic/.kimi-code/bin/kimi` and is on PATH (FACT).
- Two additional Claude config dirs exist but **no alias launches them**: `~/.claude` (the
  default dir, overridden by every alias's explicit `CLAUDE_CONFIG_DIR=`) and `~/.claude-shared`
  (the shared-state template/target). This distinction turns out to matter a great deal — see
  §3.2 and §3.6.

---

## 2. How the toolkit shares state — the mechanism that explains every gap

**FACT** (`~/Projects/claude_toolkit/scripts/lib.sh:3112-3124`):

```
CMA_SHARED_ITEMS=(
  projects todos tasks plans file-history paste-cache shell-snapshots
  session-env telemetry sessions backups cache plugins
  stats-cache.json history.jsonl CLAUDE.md
  daemon jobs
)
# NOTE (§11.4 own-settings): settings.json is DELIBERATELY NOT in the shared set.
# Each config dir gets its OWN settings.json so per-alias permissions/model/hooks
# never leak across aliases/providers, while the plugin CACHE (`plugins`),
# history (`history.jsonl`), memory (`CLAUDE.md`) and sessions stay shared. Each
# dir's own settings.json is seeded from + kept enabledPlugins-synced with the
# shared template $SHARED_DIR/settings.json by cma_own_settings_seed ...
```

Three consequences follow directly, and they account for essentially the whole gap list:

1. **`CLAUDE.md` IS shared** — every dir symlinks to one file. The delivery vehicle for
   user-scope governance **already exists and is already wired to all 34 aliases**. It is
   simply empty.
2. **`settings.json` is deliberately NOT shared**, and only its `enabledPlugins` key is
   synced. Therefore **hooks can never propagate** through the toolkit's own sync mechanism.
   This is a design decision, not an oversight — but it is the reason §11.4.109 and §11.4.182
   guards do not reach any alias.
3. **`skills` is not in the shared set at all** — see §3.6.

---

## 3. The alias × mechanism matrix

Because the 34 Claude config dirs are **provably uniform** on every axis measured (see the
uniformity proofs inline), the matrix collapses to three rows. This is itself a finding: the
audit's working hypothesis was that aliases would differ; **measured, they do not** — they are
uniformly under-provisioned.

Legend: **ACTIVE** (measured present and effective) · **ABSENT** (measured not present) ·
**PARTIAL** · **UNKNOWN**.

| Mechanism | Native `claude1-5` (5) | Claude provider aliases (30) | Kimi aliases (8) |
|---|---|---|---|
| **`CLAUDE_CONFIG_DIR` set** | ACTIVE `~/.claude-claude<N>` | ACTIVE `~/.claude-prov-<id>` | ACTIVE `~/.kimi-prov-<id>` |
| **User-scope `CLAUDE.md` content** | **ABSENT** (0-byte target) | **ABSENT** (0-byte target) | **ABSENT** (no file at all) |
| **User-scope `AGENTS.md`** | **ABSENT** | **ABSENT** | **ABSENT** |
| **§11.4.109 `guard-forbidden-commands.sh`** | **ABSENT** | **ABSENT** | **ABSENT** |
| **§11.4.182 `guard-track-branch-label.sh`** | ACTIVE *(project-scoped only)* | ACTIVE *(project-scoped only)* | **ABSENT** |
| **Any hook in the alias's own `settings.json`** | **ABSENT** | **ABSENT** | **ABSENT** |
| **CodeGraph MCP** | ACTIVE *(project-scoped only)* | ACTIVE *(project-scoped only)* | **ABSENT** |
| **Lumen** | **PARTIAL — loaded but index empty (0 files/0 chunks); returns nothing** | **PARTIAL — same** | **ABSENT** |
| **Plugin set (283 plugins)** | ACTIVE, uniform | ACTIVE, uniform | **ABSENT** |
| **User-scope skills (955)** | **ABSENT** | **ABSENT** | **ABSENT** |
| **§11.4.140 action registry reachable** | PARTIAL *(cwd-dependent)* | PARTIAL *(cwd-dependent)* | **ABSENT** |
| **§11.4.187 multitrack engine** | **ABSENT** *(no config for this host)* | **ABSENT** | **ABSENT** |
| **Project `CLAUDE.md` (446 KB governance)** | ACTIVE *in `helix_code` only* | ACTIVE *in `helix_code` only* | **ABSENT** |

### 3.1 User-scope `CLAUDE.md` — ABSENT for all 43 aliases (FACT)

The single highest-impact finding.

```
$ ls -l ~/.claude-claude5/CLAUDE.md
lrwxrwxrwx 1 ... 41 Sep  5 15:07 .claude-claude5/CLAUDE.md -> /home/milosvasic/.claude-shared/CLAUDE.md

$ stat -c '%n %s bytes' ~/.claude-shared/CLAUDE.md
.claude-shared/CLAUDE.md 0 bytes
```

Uniformity proof — all 35 `CLAUDE.md` entries across `~/.claude` + the 34 alias dirs resolve to
the same target:

```
$ for d in .claude .claude-claude? .claude-prov-*; do readlink "$d/CLAUDE.md"; done | sort | uniq -c
     35 /home/milosvasic/.claude-shared/CLAUDE.md
```

**Interpretation.** Every Claude alias on this host loads **zero bytes** of user-scope memory.
The symlink fan-out is healthy and complete; the content is missing. `claude-unify.sh:334-353`
shows the shared file is seeded by copying the first *non-symlink* `CLAUDE.md` it finds — at
unification time there was none with content, so the shared file was created empty and every
dir was pointed at it.

**This is the cheapest gap on the list to close** — writing content to one file
(`~/.claude-shared/CLAUDE.md`) instantly reaches all 34 Claude aliases with no new wiring.

### 3.2 Hooks — ABSENT for all 43 aliases (FACT)

```
$ for d in .claude .claude-shared .claude-claude? .claude-prov-*; do jq -r 'keys|join(",")' "$d/settings.json"; done | sort | uniq -c
      1 agentPushNotifEnabled,enabledPlugins,inputNeededNotifEnabled
      1 agentPushNotifEnabled,enabledPlugins,inputNeededNotifEnabled,theme,tui
     32 enabledPlugins
      1 enabledPlugins,hooks,permissions,theme
      1 enabledPlugins,modelSettings
```

**The one `settings.json` on this host that contains `hooks` is `~/.claude/settings.json`** —
and `~/.claude` is the default config dir, which **every alias overrides**:

```json
// ~/.claude/settings.json — NOT reachable by any alias
{ "permissions": { "allow": ["mcp__codegraph__*"] },
  "hooks": { "UserPromptSubmit": [{ "hooks": [{ "type": "command",
             "command": "codegraph prompt-hook" }] }] },
  "theme": "dark" }
```

Because all 43 aliases set `CLAUDE_CONFIG_DIR` explicitly (aliases.sh:1691-1733, and
`export CLAUDE_CONFIG_DIR="$CMA_PROVIDER_CONFIG_DIR"` at aliases.sh:289), the `codegraph
prompt-hook` `UserPromptSubmit` hook and the `mcp__codegraph__*` permission grant **never
load for any alias**. They are configured, and they are dead.

The four non-`enabledPlugins`-only dirs are `~/.claude` (hooks/permissions/theme),
`~/.claude-claude1` and `~/.claude-claude4` (notification prefs), and `~/.claude-claude5`
(`modelSettings`) — all cosmetic or dead; **none adds a hook to an alias**.

### 3.3 §11.4.109 guard-forbidden-commands — ABSENT everywhere (FACT)

The script exists and is executable:

```
$ ls -l helix_code/constitution/scripts/hooks/guard-forbidden-commands.sh
-rwxrwxr-x 1 ... 30635 Aug 31 20:33 guard-forbidden-commands.sh
```

It is referenced by **no** settings file on this host:

```
$ grep -rl "guard-forbidden-commands" ~/.claude*/settings.json ; # → no matches
$ grep -rn "guard-forbidden-commands" helix_code/.claude/ ; # → no matches
```

**Interpretation.** §11.4.109 mandates this hook be wired as `PreToolUse` in
`.claude/settings.json` "or equivalent runtime settings". On this host the mandate's
mechanical floor — the layer explicitly designed to hold "regardless of agent memory" — is
**not installed at any layer, for any alias, in any project checked**. Six other guard scripts
sit in the same directory (`guard-branch-consistency.sh`, `guard-evidence-store-write.sh`,
`guard-work-track-binding.sh` per §11.4.191, plus their test suites); **only one of the seven
is wired anywhere** (see §3.4).

### 3.4 §11.4.182 track-branch-label guard — ACTIVE, but project-scoped (FACT)

`helix_code/.claude/settings.json` (300 bytes, the entire file):

```json
{ "hooks": { "PreToolUse": [ { "matcher": "Agent|Task|TaskCreate",
    "hooks": [ { "type": "command",
      "command": "bash \"$CLAUDE_PROJECT_DIR/constitution/scripts/hooks/guard-track-branch-label.sh\"" } ] } ] } }
```

This is the **only** constitution hook wired anywhere on the host. It is **project-scoped**,
so it applies to any Claude alias launched inside `helix_code` — and to **no** alias launched
anywhere else, and to **no** Kimi alias (Kimi Code does not read `.claude/settings.json`).

### 3.5 MCP servers — CodeGraph ACTIVE project-scoped; nothing per-alias (FACT)

No alias config dir declares `mcpServers` (§3.2 key census: the key is absent from all 36
`settings.json` files). MCP reaches aliases entirely through the **project**:

```
$ jq -r '.mcpServers|keys|join(", ")' helix_code/.mcp.json
codegraph, media-validator, open-design

$ cat helix_code/.claude/settings.local.json
{ "enabledMcpjsonServers": [ "codegraph", "open-design" ] }
```

- **`codegraph` — ACTIVE.** Runtime evidence: in *this* session (`claude5`, cwd `helix_code`)
  the `mcp__codegraph__codegraph_explore` tool is present in the live tool list. **FACT for
  `claude5`.** For the other 33 Claude aliases the enabling file is the same project-scoped
  `settings.local.json`, so activation is **INFERRED-ACTIVE** — highly likely, but not
  individually measured (launching each alias was out of scope for a read-only audit).
- **`open-design` — enabled** by the same file (`mcp__open-design__*` tools present in this
  session — FACT).
- **`media-validator` — declared in `.mcp.json` but NOT in `enabledMcpjsonServers` → ABSENT.**
  §11.4.163 mandates a media-validation pipeline; the server is present but not enabled.
- Per-alias `.claude.json` files each carry a `projects["/home/milosvasic/Projects/helix_code"]`
  entry (all 34 — FACT), but **none** of them sets a project-level `enabledMcpjsonServers`
  (all report `none` — FACT). Enablement rests solely on the shared project file.

**Consequence:** CodeGraph coverage is a property of the *project*, not the alias. Any alias
launched outside a project that ships a `.mcp.json` + `enabledMcpjsonServers` gets **no
CodeGraph at all**, in direct tension with §11.4.78's "wire into every CLI agent".

### 3.6 Plugins — ACTIVE and genuinely uniform (FACT). Skills — ABSENT (FACT).

The one mechanism that is fully, uniformly healthy:

```
$ for d in .claude-claude? .claude-prov-*; do jq -S -c '.enabledPlugins' "$d/settings.json" | md5sum | cut -c1-10; done | sort | uniq -c
     34 3bd35b3254
$ ... '.enabledPlugins|length' ...
     34 283
$ ... '.enabledPlugins["lumen@claude-plugins-official"]' ...
     34 true
```

All 34 alias dirs enable a **byte-identical set of 283 plugins**, including `lumen`. The
`plugins` cache directory is symlinked to `~/.claude-shared/plugins` in every dir (FACT), so
they share one on-disk copy. `cma_own_settings_seed` keeps this key in sync — and it is the
*only* key it syncs, which is precisely why plugins are the only uniformly-healthy mechanism.

**Skills, by contrast, are stranded.** `~/.claude/skills` holds **955 skill directories**
(FACT) — but `skills` is **not** in `CMA_SHARED_ITEMS`, `~/.claude-shared` has **no** `skills`
directory, and **no alias config dir has a `skills` directory at all** (FACT). Those 955
user-scope skills are therefore invisible to all 43 aliases. The skills that *are* available in
a session come from (a) the 283 plugins and (b) `helix_code/.claude/skills` (15 `speckit-*`
skills — project-scoped).

### 3.7 §11.4.140 action registry — PARTIAL, cwd-dependent (FACT)

```
$ echo "${HELIX_ACTION_REGISTRY:-<unset>}"     → <unset>
$ ls helix_code/constitution/actions/registry.yaml → present, 40,988 bytes
```

`$HELIX_ACTION_REGISTRY` is unset in the environment, so resolution falls back to the
relative path `constitution/actions/registry.yaml`. That resolves **only when cwd is a
project that vendors the constitution submodule**. Inside `helix_code` the registry is
reachable and the §11.4.140 instruction block is present in the project `CLAUDE.md` →
**PARTIAL/ACTIVE**. Outside such a project, **both** the instruction and the registry are
absent → the action-prefix system silently no-ops. Note also
`constitution/scripts/hooks/action_prefix_expand.sh` exists (8,906 bytes) and is **wired
nowhere** (FACT).

### 3.8 §11.4.187 multitrack engine — ABSENT on this host (FACT)

The engine ships in the constitution submodule (`constitution/scripts/multitrack/`, 20+
scripts including `multitrack_bootstrap.sh`, `multitrack_device_lock.sh`,
`multitrack_fallback_monitor.sh` — FACT). Its activation state:

- No `multitrack` command on `~/.local/bin` (FACT).
- No `~/.multitrack` or `~/.config/multitrack` state directory (FACT).
- `constitution/scripts/post_update_hook.sh` exists but contains **0 references** to
  multitrack (FACT: `grep -c multitrack` → `0`) — so the §11.4.164 auto-propagation hook does
  **not** bootstrap it.
- Per-host config exists for exactly one host: `helix_code/config/multitrack/the-factory.yaml`.
  **This host is `anton`** (FACT: `hostname`). **There is no `anton.yaml`.**

**Conclusion: the multitrack engine is not installed, not bootstrapped, and has no
configuration for this host.** §11.4.187 requires it be "automatic, out-of-the-box, inherited";
it is currently none of those here.

### 3.9 Kimi aliases — a governance void (FACT)

```
$ for d in ~/.kimi-prov-*; do ls -A "$d"; done
config.toml     # ← the complete contents of every one of the 8 dirs
```

Every Kimi config dir contains exactly one file. Its keys (FACT, values redacted — credentials
present in `api_key`, reported by presence only, never printed): `type`, `base_url`, `api_key`,
`provider`, `model`, `max_context_size`, `capabilities`, `default_model`.

There is **no `CLAUDE.md`, no `AGENTS.md`, no `settings.json`, no hooks, no MCP config, no
plugins directory, and no skills directory** for any of the 8 Kimi aliases. Kimi Code is also a
different binary reading a different config format (`config.toml`, not `settings.json`), so
**none of the Claude-side fixes will reach it**; closing the Kimi gap requires establishing
what Kimi Code's own governance/hook/MCP surface even is.

**UNKNOWN:** whether Kimi Code supports a memory file, hooks, or MCP at all. Not established
in this audit — see §6.

---

## 4. Governance token cost

### 4.1 What is actually loaded (FACT)

In this session (`claude5`, cwd `helix_code`) the always-loaded governance is the **project**
`CLAUDE.md`. The user-scope chain contributes **0 bytes** (§3.1). No `@`-import directives were
found in the project `CLAUDE.md` (FACT: `grep -n '^@'` → no matches), so it is loaded as a
single flat file rather than pulling in `constitution/CLAUDE.md` transitively.

| File | Bytes | Non-ws chars | Words | Status |
|---|---|---|---|---|
| `helix_code/CLAUDE.md` | **446,076** | 386,140 | 56,836 | **LOADED every turn** |
| `~/.claude-shared/CLAUDE.md` (user scope) | **0** | 0 | 0 | loaded, empty |
| `helix_code/AGENTS.md` | 454,760 | 394,274 | 57,886 | not loaded by Claude Code |
| `helix_code/constitution/CLAUDE.md` | 788,487 | 685,989 | 101,263 | not loaded (no import) |

### 4.2 Token count — ESTIMATE, not measurement

**No tokenizer is available on this host** (FACT: `import tiktoken` → `No module named
'tiktoken'`), and §11.4.141 in any case forbids `tiktoken` as the basis of a *measurement*
(the authoritative figure must come from the API `usage` object). What follows is therefore an
**ESTIMATE** and must not be quoted as a measured number.

Three char-based methods applied to the 446,076-byte project `CLAUDE.md`:

| Method | Result | Note |
|---|---|---|
| `chars / 4.0` (generic English rule of thumb) | **~111,500 tokens** | likely a floor |
| `chars / 3.2` (dense technical markdown) | **~139,400 tokens** | likely closest |
| `words × 1.35` | **~76,700 tokens** | likely an under-count for this text |

**Stated estimate: ~110,000–140,000 tokens, most plausibly ~130,000–140,000.**

Rationale for favouring the lower chars-per-token ratio (ESTIMATE, method stated): this
document is unusually token-hostile. It is saturated with identifiers that no tokenizer merges
efficiently — `§11.4.234`, `CM-COVENANT-114-126-PROPAGATION`, `--skip-artifact-byte-check`,
`ab_pass_with_evidence` — plus heavy `§`/`—`/`·` punctuation and long runs of `/`-separated
cross-reference lists (`§11.4.5/.6/.20/.40/.50/…`). Text of this shape typically lands nearer
3.0–3.5 chars/token than the 4.0 that ordinary prose achieves.

### 4.3 Relation to the §11.4.141 figure

§11.4.141 cites a measured "~170K tokens of governance re-sent every turn". This audit's
estimate for the project `CLAUDE.md` alone is ~130–140K. **The two are consistent and not in
conflict:** the ~170K figure plausibly covers the full always-on prefix (governance file +
system prompt + tool schemas + skill listings), of which the governance file is the dominant
single component. **UNKNOWN:** the true split — establishing it requires the §11.4.141 harness
reading the API `usage` object (`input_tokens` / `cache_read_input_tokens` /
`cache_creation_input_tokens`), which this read-only audit did not run.

**One structural observation worth flagging (FACT, not estimate):** because user-scope
`CLAUDE.md` is empty and the project file carries everything, the entire governance prefix is
**already byte-stable across turns within a session** — which is the precondition §11.4.141
requires for prompt-cache hits. Whether cache reads are actually being achieved is
**UNKNOWN** (needs the `usage` object).

---

## 5. CodeGraph and Lumen state

Established by a dedicated read-only sub-audit. No index, sync, reindex, or install command was
run; all reads used `sqlite3 'file:…?immutable=1'`, `stat`, `find`, `cat`, `grep`, plus one
read-only `GET http://localhost:11434/api/tags`.

### 5.1 CodeGraph — installed, current, populated, mildly stale (FACT)

- **Install:** `~/.local/bin/codegraph` → `~/.codegraph/versions/v1.6.0/bin/codegraph`;
  `codegraph --version` → **1.6.0**; only `v1.6.0` present. A shadowed second copy exists at
  `~/.nvm/versions/node/v26.8.1/bin/codegraph` (`~/.local/bin` wins on PATH).
- **Currency (§11.4.80):** `npm view @colbymchenry/codegraph version` → **1.6.0**. **Already
  latest** — no update owed. `~/.codegraph/update-check.json` records a successful check.
- **Indexes:** 12 `.codegraph/` directories exist; **only 4 contain a database.** The other 8
  are `.gitignore`-only stubs that were never indexed — including
  `Projects/claude_toolkit/.codegraph` and every vendored `constitution/.codegraph`.

| Index | config.json | db size | db mtime | newest source file | delta | verdict |
|---|---|---|---|---|---|---|
| `helix_code` | yes | 2,765,209,600 B | 2026-09-06 22:27 | 2026-09-07 13:24 | **+0.62 d** | **mildly STALE** |
| `boba` | **no** | 2,214,719,488 B | 2026-09-02 13:17 | 2026-09-05 13:51 | **+3.86 d** | **STALE** |
| `Projects/tmux` | yes | 73,646,080 B | 2026-09-02 08:34 | 2026-09-02 08:36 | +1.66 d | **STALE** |
| `~/tmux` | yes | 68,710,400 B | 2026-09-02 08:44 | 2026-09-02 08:43 | ~0 | fresh (tree idle) |

- **`helix_code` index contents (FACT, read from the db):** **35,514 files · 739,702 nodes ·
  2,376,353 edges**; `project_metadata` reports `index_state=complete`,
  `indexed_with_version=1.6.0`, `files_discovered == files_accounted`. The index is genuinely
  populated — this is a real, working mechanism.
- `Projects/boba/.codegraph` **has no `config.json` at all** and is therefore running on
  defaults (FACT) — an unmanaged index.

### 5.2 §11.4.79 own-org submodule inclusion — PARTIAL COMPLIANCE (FACT)

Measured **from the index itself**, not inferred from exclude patterns — the strong form of the
check.

**Satisfied:** all **68** declared `submodules/*` paths have ≥1 indexed file (a per-path
`SELECT` loop printed zero zero-indexed lines). Largest: `helix_agent` 13,907 files ·
`claude-toolkit` 12,918 · `helix_qa` 1,030 · `llms_verifier` 845 · `helix_llm` 620 ·
`containers` 514 · `challenges` 286. **The Helix family the mandate names by name is genuinely
included.** Third-party trees are correctly excluded (`cli_agents_resources/`,
`dependencies/LLama_CPP/`, `dependencies/Ollama/`, `dependencies/HuggingFace_Hub/`,
`awesome-ai-memory/` — all verified 0 rows).

**Three deviations, both directions:**

1. **Own-org EXCLUDED (should be in):** `"cli_agents/**"` → **0 rows**. That pattern covers
   **~55 `vasic-digital/caf-*` fork submodules** (aider, cline, plandex, crush, codex,
   gemini-cli, …), all own-org per `.gitmodules`. `"github_pages_website/**"` → **0 rows**
   (`HelixDevelopment-Code/Welcome`, own-org).
2. **Own-org PARTIALLY excluded:** `"submodules/helix_agent/cli_agents/**"`,
   `"submodules/helix_agent/external/**"`, `"submodules/**/tools/opensource/**"`,
   `"**/tools/opensource/**"` carve subtrees out of own-org submodules.
3. **Third-party wrongly INCLUDED:** `dependencies/colibri` 461 files (`JustVugg/colibri`),
   `mcp_servers` 92 files (`modelcontextprotocol/servers`), `submodules/superspec` 5 files
   (`WangX0111/superspec`) — none appear on the exclude list.

Also noted without verdict (FACT): `helix_code`'s `include` array lists no `*.sh`/`*.yaml`/`*.md`
patterns, yet `scripts/*.sh` and `.docs_chain/contexts/*.yaml` appear in `files` — so the
`include` list is evidently not the sole gate on what gets indexed.

### 5.3 CodeGraph MCP wiring — fragile, project-scoped only (FACT, exhaustive)

| Config location | codegraph MCP entry? |
|---|---|
| `~/.claude.json` (default config dir) | **YES** — `mcpServers.codegraph = {stdio, "codegraph serve --mcp"}` |
| `~/.claude/settings.json` | partial — no server def; `permissions.allow: ["mcp__codegraph__*"]` + `codegraph prompt-hook` |
| `~/.claude-claude1` … `claude5` (10 files) | **NO** — `grep -ci codegraph` = **0** in all |
| `~/.claude-shared/settings.json` | **NO** |
| all 29 `~/.claude-prov-*/.claude.json` | **NO** — 0 hits in every one |
| `helix_code/.mcp.json` | **YES** (+ `media-validator`, `open-design`) |
| `tmux/.mcp.json`, `boba/.mcp.json` | **YES** |

This independently confirms §3.5 from the CodeGraph side: **no alias config dir defines the
codegraph MCP server.** An alias gets CodeGraph **only** inside one of three project roots
(`helix_code`, `tmux`, `boba`). Everywhere else — nothing.

### 5.4 Lumen — completely non-functional, and actively misdirecting (FACT)

**What it is:** a Claude Code **plugin** (`lumen@claude-plugins-official` **v0.0.42**, Ory Corp,
`github.com/ory/lumen`, Apache-2.0) that ships its own MCP server. **There is no `lumen` on
PATH.** Binary: `~/.claude-shared/plugins/cache/…/lumen/0.0.42/bin/lumen-linux-amd64` (34.7 MB).
It is enabled in **all 34** alias `settings.json` files, and appears in **no** `mcpServers`
block — it is delivered purely as a plugin. `.claude-claude5/.claude.json` records
`pluginUsage.lumen: {usageCount: 383}` — it is being *invoked* heavily.

**The system-reminder claim is CORROBORATED by on-disk evidence, and it is worse than reported —
it is true for every project, not just one.** State lives in `~/.local/share/lumen/` (not
`~/.lumen`, `~/.cache/lumen`, or `~/.config/lumen` — none exist). All **7** per-project
`index.db` files are **exactly 98,304 bytes** (bare schema), and read-only queries over every one
return:

```
file_revisions = 0   project_files = 0   chunk_defs = 0
vector_keys    = 0   vec_vectors_chunks = 0   vec_vectors_rowids = 0
project_meta → last_index_error | embed shared batch: all embedding servers are unhealthy
```

**Root cause (FACT, from `~/.local/share/lumen/debug.log`):**

```
"health probe failed","backend":"ollama","host":"http://localhost:11434",
  "error":"configured model \"ordis/jina-embeddings-v2-base-code\" is not loaded"
"no healthy embedding server found"
"indexing failed","project":"/home/milosvasic/Projects/helix_code",
  "err":"indexing: embed shared batch: all embedding servers are unhealthy"
```

Independently confirmed against Ollama: **Ollama is up and serving**, but holds only
`nomic-embed-text:latest` and `qwen2.5:3b-instruct-q4_K_M` — **no `jina` model of any kind**.
Lumen's single configured embedding model is simply not pulled. Every indexing attempt since at
least 2026-09-01 has failed identically; the most recent `helix_code` attempt ran 09:36 → failed
10:15 on 2026-09-07.

**Projects registered with Lumen (all 0 files / 0 chunks):** `helix_code`,
`helix_code/submodules/helix_agent`, `helix_code/submodules/helix_llm`, `vasic`,
`vasic/workshop`, `tmux`, `boba`. Lumen's own daily cleanup (2026-09-07 10:51) reported
`removed 0 projects and 0 vectors` for all seven — its own accounting confirms emptiness.

**Why this is more than an outage.** A `PreToolUse` hook fires on this host advising *"Load and
call `mcp__plugin_lumen_lumen__semantic_search` instead of Grep/Glob/find/rg for significantly
faster and better search results."* That advice is currently **false**: the tool it recommends
can return nothing, for any project. An agent that follows it in preference to `grep` will
conclude "no matches" from an empty index and may report an absence as a finding. **This is a
live §11.4-class bluff surface** — a mechanism that reports success-shaped emptiness — and it
has been live for at least a week while being invoked 383 times from one alias alone.

---

## 6. What I could not establish

Recorded explicitly rather than guessed (§11.4.6).

1. **Per-alias runtime confirmation.** Every finding is from **static configuration on disk**,
   plus **runtime evidence for `claude5` only** (the session running this audit). I did not
   launch the other 42 aliases — doing so would have been a state change and is outside a
   read-only audit. Where a mechanism is uniform on disk across all dirs I have marked it
   ACTIVE/ABSENT with the uniformity proof; where activation additionally depends on runtime
   behaviour (notably MCP enablement, §3.5) I marked it **INFERRED-ACTIVE** rather than ACTIVE.
2. **Authoritative token measurement.** No tokenizer on host; no API `usage` object captured.
   §4.2 is an ESTIMATE and is labelled as such. The §11.4.141 harness has not been run.
3. **Whether prompt caching is actually landing.** Requires `cache_read_input_tokens > 0` from
   a live `usage` object. Not measured.
4. **Kimi Code's governance surface.** Whether Kimi Code supports a memory file, hooks, MCP
   servers, or plugins **at all** is UNKNOWN. I established only that its config dirs contain
   nothing but `config.toml`. Before anything can be "wired" for the 8 Kimi aliases, someone
   must establish what Kimi Code can even accept. **This is the single largest unknown in the
   programme.**
5. **Other projects.** I audited alias-side config exhaustively and project-side config for
   `helix_code` only. Whether other checkouts under `~/Projects` wire the guards or
   `.mcp.json` differently is UNKNOWN. Given that project-scoping is the mechanism carrying
   *all* working governance, this matters: a per-project survey is a necessary follow-up.
6. **Whether the deliberate no-share of `settings.json` is still wanted.** `lib.sh:3118`
   documents it as an intentional isolation decision. Closing the hook gap by sharing
   `settings.json` would reverse that decision; closing it by seeding hooks into 34 files
   preserves it but adds drift surface. **This is an operator decision, not an audit finding**
   — flagged, not decided.
7. **`~/Projects/claude_toolkit` was read but not analysed exhaustively.** Per the audit brief,
   another agent is actively editing it (Kimi wiring). I read `aliases.sh`, `lib.sh`
   (targeted), `install.sh`, `claude-unify.sh`, and `claude-providers.sh` (targeted). The
   toolkit may have wiring paths I did not exercise; findings about *installed state on disk*
   are unaffected by that.
8. **Whether Lumen would work once an embedding model is available.** The root cause is
   established (no `jina` model in Ollama) and the failure is unambiguous in Lumen's own logs,
   but I did not pull a model or re-index — so "fix the model and it works" is a
   **HYPOTHESIS**, not a verified outcome. It is well-supported (Lumen's error names exactly
   one missing precondition) but unproven until an index actually populates.
9. **Whether `nomic-embed-text` is a valid substitute for `ordis/jina-embeddings-v2-base-code`.**
   Ollama already holds the former. Whether Lumen accepts a different embedding model, and
   whether its stored vector dimensionality (the jina model is 768-dim) would need resetting,
   is UNKNOWN. Do not assume the substitution is free.
10. **Why `cli_agents/**` is on the CodeGraph exclude list.** It may be a deliberate size
    decision (~55 fork submodules is a large tree) rather than an oversight. §11.4.79 requires
    own-org paths be included *or* carry an audit trail in `config.json` comments; I found the
    exclusion but did not find a recorded rationale. Whether one exists elsewhere is UNKNOWN —
    this should be confirmed with the operator before "fixing" it.

---

## 7. Prioritised gap list

Ordered by (aliases affected × severity) ÷ cost to close.

| # | Gap | Aliases affected | Severity | What closing it requires |
|---|---|---|---|---|
| **1** | **User-scope `CLAUDE.md` is 0 bytes** — no governance loads for any alias outside a governed project | **34 / 43** | **Critical** | Write content to **one file**: `~/.claude-shared/CLAUDE.md`. The symlink fan-out to all 34 dirs already exists and is verified healthy. Highest leverage, lowest cost on the list. Decide content: full inherited text vs. a §11.4.141-style thin index + pointer (the index is likely correct — it keeps the anchor literals for propagation gates while avoiding a second ~140K-token block). |
| **2** | **§11.4.109 `guard-forbidden-commands.sh` wired nowhere** — the mandate's mechanical floor is absent | **43 / 43** | **Critical** | Add a `PreToolUse` hook entry. Two routes: (a) project-level `helix_code/.claude/settings.json` — one edit, covers 35 Claude aliases *in this project only*; (b) per-alias `settings.json` × 34 — covers all cwds but fights the deliberate no-share design (§6.6). Recommend (a) immediately, then decide (b). Does **not** reach Kimi. |
| **3** | **Lumen index is empty for all 7 projects (0 files / 0 chunks) while a hook actively steers agents to it** | **34 / 43** | **Critical** | Single root cause: pull the configured embedding model into Ollama (`ordis/jina-embeddings-v2-base-code`), **or** repoint Lumen at the `nomic-embed-text` model Ollama already has, then re-index. **Until then, treat the "use lumen instead of grep" hook advice as unsafe** — an empty index returns "no matches" indistinguishably from a real absence. Consider suppressing the hook until the index is proven non-empty. |
| **4** | **8 Kimi aliases have zero governance surface** | **8 / 43** | **High** | Blocked on unknown #6.4 — establish Kimi Code's memory/hook/MCP capabilities *first*. Do not attempt wiring before that is known. |
| **5** | **§11.4.187 multitrack engine not installed; no `anton.yaml`** | all (orchestration-wide) | **High** | Author `config/multitrack/anton.yaml`; run `multitrack_bootstrap.sh`; wire it into `constitution/scripts/post_update_hook.sh` (currently 0 references) so §11.4.164 auto-propagation actually installs it. |
| **6** | **955 user-scope skills invisible to every alias** | **43 / 43** | **High** | Add `skills` to `CMA_SHARED_ITEMS` in `lib.sh` and re-run unify — or symlink `~/.claude-shared/skills`. Single-line change, very large surface recovered. Verify none of the 955 conflict with the 283 plugins' skills first. |
| **7** | **CodeGraph reaches aliases only via project `.mcp.json`** — no alias dir defines it | **43 / 43** outside 3 project roots | **High** | Either add a `codegraph` entry to the shared `settings.json` template's `mcpServers` (needs the no-share decision), or accept project-scoping and ensure every project ships `.mcp.json` + `enabledMcpjsonServers`. §11.4.78's "wire into every CLI agent" favours the former. |
| **8** | **§11.4.79 partial: own-org `cli_agents/**` (~55 `vasic-digital/caf-*` forks) and `github_pages_website/` excluded from the index; three third-party trees wrongly included** | n/a (index quality) | **High** | Edit `helix_code/.codegraph/config.json`: drop `cli_agents/**` and `github_pages_website/**` from `exclude`, add `dependencies/colibri`, `mcp_servers`, `submodules/superspec`. Re-index. Note §11.4.79 requires a paired mutation proving a cross-submodule probe fails when an own-org path is excluded. |
| **9** | **`~/.claude/settings.json` hooks + permissions are dead config** | 0 (that is the problem) | **Medium** | The `codegraph prompt-hook` `UserPromptSubmit` hook and `mcp__codegraph__*` grant sit in the one dir no alias uses. Either migrate them into the alias dirs, or delete them so they stop implying coverage that does not exist. Dead config that implies coverage is itself a §11.4-class bluff surface. |
| **10** | **5 further constitution guards wired nowhere** (`guard-branch-consistency`, `guard-evidence-store-write`, `guard-work-track-binding` (§11.4.191), `action_prefix_expand`) | **43 / 43** | **Medium** | Same wiring decision as #2; batch with it. |
| **11** | **CodeGraph indexes stale; `boba` has no `config.json`** | n/a (index quality) | **Medium** | `helix_code` is ~15 h behind newest source; `boba` ~3.9 d and unmanaged. §11.4.80 mandates a weekly sync floor — the *binary* is current (1.6.0 = latest), but the *indexes* are drifting. Add `config.json` to `boba`; schedule re-index. |
| **12** | **`media-validator` MCP declared but not enabled** (§11.4.163) | **43 / 43** | **Medium** | Add to `enabledMcpjsonServers` in `helix_code/.claude/settings.local.json` — one-line change — after confirming the server actually starts. |
| **13** | **`$HELIX_ACTION_REGISTRY` unset** — §11.4.140 registry resolves only by relative path | **43 / 43** outside governed projects | **Low–Medium** | Export it from the alias file or `.bashrc` so the action system resolves regardless of cwd. |

**Recommended first moves.** Three items are unusually cheap relative to their reach and carry
no design conflict:

- **#3 (Lumen)** — one embedding-model decision unblocks a subsystem that is currently
  *worse than absent*, because a hook is steering agents into it. Fix or mute; do not leave it
  as-is.
- **#1 (empty `CLAUDE.md`)** — one file write reaches 34 aliases through wiring that already
  exists and is verified healthy.
- **#6 (stranded skills)** — one line in `CMA_SHARED_ITEMS` recovers 955 skills for 34 aliases.

**#2** is the highest-severity item on the list but should not be rushed: doing it properly
requires the §6.6 operator decision on whether `settings.json` may be shared. Doing it at
project level only (route (a)) is a genuine improvement and can proceed immediately; doing it
host-wide (route (b)) reverses a documented design decision and needs a human call.

---

## 8. Evidence index

Every FACT above is reproducible with these read-only commands:

```bash
# alias inventory
grep -c '^alias ' ~/.local/share/claude-multi-account/aliases.sh
grep -n  '^alias ' ~/.local/share/claude-multi-account/aliases.sh

# CLAUDE.md fan-out and emptiness
for d in ~/.claude ~/.claude-claude? ~/.claude-prov-*; do readlink "$d/CLAUDE.md"; done | sort | uniq -c
stat -c '%n %s bytes' ~/.claude-shared/CLAUDE.md

# settings.json key census (hooks / mcp / permissions)
for d in ~/.claude ~/.claude-shared ~/.claude-claude? ~/.claude-prov-*; do \
  jq -r 'keys|join(",")' "$d/settings.json"; done | sort | uniq -c

# plugin uniformity
for d in ~/.claude-claude? ~/.claude-prov-*; do \
  jq -S -c '.enabledPlugins' "$d/settings.json" | md5sum | cut -c1-10; done | sort | uniq -c

# guard wiring
grep -rl 'guard-forbidden-commands' ~/.claude*/settings.json
grep -rn 'guard-forbidden-commands' ~/Projects/helix_code/.claude/

# kimi dirs
for d in ~/.kimi-prov-*; do echo "$d:"; ls -A "$d"; done

# skills
ls ~/.claude/skills | wc -l ; ls -d ~/.claude-shared/skills
grep -n 'CMA_SHARED_ITEMS=' -A12 ~/Projects/claude_toolkit/scripts/lib.sh

# multitrack
hostname ; ls ~/Projects/helix_code/config/multitrack/
grep -c multitrack ~/Projects/helix_code/constitution/scripts/post_update_hook.sh

# governance size
wc -c ~/Projects/helix_code/CLAUDE.md

# codegraph install + currency + index contents
command -v codegraph ; codegraph --version ; ls ~/.codegraph/versions
npm view @colbymchenry/codegraph version
find ~/Projects -maxdepth 3 -type d -name .codegraph
sqlite3 'file:'"$HOME"'/Projects/helix_code/.codegraph/codegraph.db?immutable=1' \
  'SELECT COUNT(*) FROM files; SELECT COUNT(*) FROM nodes; SELECT COUNT(*) FROM edges;'

# lumen: index emptiness + root cause
ls ~/.local/share/lumen/
for db in ~/.local/share/lumen/*/index.db; do \
  sqlite3 "file:$db?immutable=1" 'SELECT COUNT(*) FROM project_files;'; done
grep -o 'last_index_error[^|]*|[^"]*' ~/.local/share/lumen/*/index.db 2>/dev/null
tail -40 ~/.local/share/lumen/debug.log
curl -s http://localhost:11434/api/tags | grep -o '"name":"[^"]*"'
```
