# Research 05 — Consumer Integration (FR-013..FR-016, naming, Claude Toolkit)

Scope: what must actually be produced/changed, in the real code, so a HelixLLM-served model
shows up as a selectable provider in Claude Code (via the Claude Toolkit), OpenCode, and
HelixCode itself — satisfying FR-013 (multi-model selection), FR-014/FR-015 (the
`helixllm/<host>/<model>[:<variant>]` naming scheme, stable across releases), FR-016 (active
model config visible to consuming layers), FR-017..FR-020 (per-tool config artefacts,
env/`.env` configurability), and FR-034..FR-037 (live validation + release, and the
GitHub-repo-identity blocker).

All paths below are absolute or relative to the checkout named. Every claim is anchored to
`file:line` as read on disk during this research pass.

---

## 0. Executive summary

- **Claude Toolkit already has a live, working, single-model-per-alias mechanism for
  HelixAgent/HelixLLM.** It detects the local backend by binary-on-PATH OR a tracked pins
  file, enumerates the live `/v1/models` listing, resolves a `strong`/`fast` model pair, and
  runs those two models through the exact same env-file → shell-alias → 4-layer-verify →
  activation-gate pipeline every cloud provider uses
  (`claude-toolkit/scripts/claude-providers.sh:216-473`). This is the pattern spec 002 must
  **extend**, not replace (§11.4.74 catalogue-check: `reuse claude-toolkit@75d25ab3`).
- **The naming scheme `helixllm/<host>/<model>[:<variant>]` cannot be a shell-alias name.**
  Toolkit alias names are validated `^[a-zA-Z][a-zA-Z0-9_-]*$`
  (`claude-toolkit/scripts/lib.sh:334`) and provider ids `[A-Za-z0-9._-]`
  (`claude-toolkit/scripts/lib.sh:3305-3309`). Neither accepts `/` or `:`. The FR-014 string
  must be the **model identifier** (the value written to `CMA_PROVIDER_MODEL` / sent as the
  wire `model` field and shown in `claude-providers list`'s STRONG_MODEL column), while the
  shell alias / provider id stays a separate, slugified, POSIX-safe token.
- **The FR-014 naming should be emitted once, at the source — HelixLLM's own
  `GET /v1/models`** (`submodules/helix_llm/internal/brain/brain.go:319-333`) — not
  reconstructed independently by every consumer. Today `Brain.Models()` emits the model's raw
  provider-reported id verbatim (`ID: m` at brain.go:327) with **no host/provenance
  prefix**, which is exactly the "llama.cpp reports the loaded `.gguf` path as its model id"
  problem the toolkit's own code comments already warn about
  (`claude-toolkit/scripts/claude-providers.sh:237-247`). Every downstream reader — the
  toolkit's live-enumeration detector, HelixCode's `OpenAICompatibleProvider.discoverModels()`
  — inherits whatever id HelixLLM returns, so rewriting it once at the source is both the
  FR-015 stability point and the cheapest implementation.
- **`Brain.Models()` already excludes unavailable providers** (`if !p.Available() { continue }`
  at brain.go:322) — this is the existing root of FR-019 ("a stopped model is not presented as
  usable") for the local-serving side, and it's why the toolkit's live-listing detector already
  behaves correctly when the backend is down (empty/absent id ⇒ honest fallback to the pinned
  default, never a fabricated list).
- **OpenCode has zero provider-config integration in the toolkit today.** The toolkit's
  `OpenCode_Integration.md` / `claude-opencode-sync.sh` sync Skills, MCP servers, and
  `CLAUDE.md` instructions into `opencode.json` — it explicitly documents that "your providers
  ... are never clobbered" (`claude-toolkit/OpenCode_Integration.md:60`). This is a genuine gap
  (**MISSING**) requiring new code, not an extension of an existing detector.
- **HelixCode's own config.yaml declares `helix-llm`/`helix-debate` provider types that the
  Go factory does not implement** — `config/config.yaml:154-172` sets `type: helix-llm` /
  `type: helix-debate`, but no `ProviderType` constant or `factory.go` switch-case exists for
  either string anywhere in non-test code (**MISSING**, confirmed by an empty grep). The config
  section is also not wired into `internal/config.Config` at all (`internal/config/config.go`'s
  `LLMConfig` has no `providers` field, and `Config.Providers` is `ProvidersConfig` — a
  completely different, memory-backend struct at `internal/config/config.go:234-239`). The
  **actual live** HelixCode↔HelixLLM path bypasses config.yaml entirely: it's the
  `resolveHelixLLMLocalProvider` special-case in `internal/server/llm_generate.go:538-552`.
- **A cross-repo release blocker (FR-037) is resolved by evidence, not by choice**: GitHub and
  GitLab already treat `vasic-digital/claude-toolkit` (hyphen) as a rename-redirect alias of
  the single canonical repo `vasic-digital/claude_toolkit` (underscore) — same repo id
  `R_kgDOSn8-DQ` on both name forms via `gh repo view`. GitVerse has **only** the underscore
  name; GitFlic has **only** the hyphen name. The toolkit's own git config already pushes to
  exactly this set (github underscore, gitlab underscore, gitverse underscore, gitflic hyphen)
  from **both** checkouts. See §4 for the full evidence trail — this is a documentation/
  submodule-URL inconsistency inside HelixCode, not a live publishing risk today, but it does
  need fixing per CONST-052.

---

## 1. Claude Toolkit — the real provider-alias mechanism

Checkout used: `/home/milosvasic/Projects/claude_toolkit`, HEAD `75d25ab394c5f5cf624d096da6a314ee130ce154`.
(Same commit as `/home/milosvasic/Projects/helix_code/submodules/claude-toolkit` — see §4.)

### 1.1 The provider-entry artefact — concrete shape

A *pins file* at `scripts/providers/<id>.json` is the tracked, human-edited source of truth
for a locally-detected (non-catalogue) provider. Three already exist for HelixAgent/HelixLLM:

```jsonc
// scripts/providers/helixagent.json — verbatim, claude-toolkit repo
{
  "bin": "helixagent",
  "id": "helixagent",
  "base_url": "http://127.0.0.1:18434/v1",
  "transport": "router",
  "strong_model": "HelixAgent/HelixLLM",
  "fast_model": "HelixAgent/HelixLLM",
  "key_var": "HELIXAGENT_GATEWAY_KEY",
  "context_limit": 229376,
  "max_output": 8192
}
```

```jsonc
// scripts/providers/helixllm-gateway.json — router transport, /v1 (OpenAI-compatible)
{
  "bin": "helixllm",
  "id": "helixllm-gateway",
  "base_url": "http://127.0.0.1:18435/v1",
  "transport": "router",
  "strong_model": "helixllm-multi",
  "fast_model": "helixllm-multi",
  "key_var": "HELIXLLM_GATEWAY_KEY",
  "context_limit": 229376,
  "max_output": 8192
}
```

```jsonc
// scripts/providers/helixagent-native.json — native transport, bare host:port (Anthropic-compatible /v1/messages)
{
  "bin": "helixllm",
  "id": "helixagent-native",
  "base_url": "http://127.0.0.1:18435",
  "transport": "native",
  "strong_model": "helixllm-multi",
  "fast_model": "helixllm-multi",
  "key_var": "HELIXLLM_GATEWAY_KEY",
  "context_limit": 229376,
  "max_output": 8192
}
```

**Field contract** (inferred from the loader — §1.2): `bin` (string, the CLI binary that gates
"is this backend installed"), `id` (must equal the filename stem), `base_url`, `transport`
(`"router"` — goes through `claude-code-router`/`ccr` and its OpenAI↔Anthropic translation — or
`"native"` — direct Anthropic-compatible `/v1/messages`, bypasses `ccr`), `strong_model` /
`fast_model` (pinned model ids — see §1.4 for why these are currently static), `key_var` (env
var name holding the API key; local backends still declare one for symmetry, may be unset),
`context_limit` / `max_output` (integers, token budgets exported to Claude Code as
`CLAUDE_CODE_AUTO_COMPACT_WINDOW` / `CLAUDE_CODE_MAX_OUTPUT_TOKENS`).

There is **no schema file or JSON-Schema validator** for this shape — it is read field-by-field
with `jq -r 'to_entries[] | [.key, (.value|tostring)] | @tsv'` and a bash `case` statement
(`scripts/claude-providers.sh:227-240`), so an unrecognized key is silently ignored and a
missing key falls back to a built-in default (`scripts/claude-providers.sh:256-265`).

### 1.2 How a pins file becomes a live alias — the detector functions

`scripts/claude-providers.sh` defines one detector function per local backend family:

- `detect_helixagent_record()` — `scripts/claude-providers.sh:216-364`. Loads
  `scripts/providers/helixagent.json` (path overridable via `CMA_HELIXAGENT_PINS_FILE`),
  applies `CMA_HELIXAGENT_*` env overrides on top (precedence: process-env > pins-file >
  built-in default, line 231-239), gates on `command -v helixagent` on PATH **OR** the pins
  file existing (line 273-274 — "opt-in on tracked config, no stub binary needed"), then
  enumerates the live model ids from `GET {base_url%/}/models` (line 285-299), and picks
  `strong`/`fast` either from the **explicit pin** (authoritative, never overwritten by live
  data — this exact bug, a pinned facade name getting silently replaced by a raw `.gguf` path,
  is the forensic anchor documented at line 237-247) or, when unpinned, from whichever of the
  live ids the first/second position happens to be (line 313-334). Emits **exactly one**
  `resolved`-shaped JSON record (line 341-359).
- `detect_helixllm_records()` — `scripts/claude-providers.sh:375-473`. Same shape, but loads
  **two** pins files (`helixllm-gateway.json` and `helixagent-native.json`) and emits **two**
  records — one `router`-transport, one `native`-transport — so both show up as independent
  aliases.

Both detectors are merged into the resolver's output inside `resolve_records()`
(`scripts/claude-providers.sh:1070-1131`), which is called once per `claude-providers sync`
invocation (`scripts/claude-providers.sh:1274` → `cmd_sync`).

### 1.3 The sync pipeline — exact artefacts written, and how to confirm it worked

Entry point: `claude-providers sync` (dispatch table `scripts/claude-providers.sh:1809`+, not
read in full but the subcommand name is confirmed live at `cmd_sync()` line 1274).

`cmd_sync()` (`scripts/claude-providers.sh:1274-1394`), per resolved record:

1. **4-layer verification** (`scripts/claude-providers.sh:1319-1382`):
   - Layer "existence" — `bash "$VERIFY" --provider <id> --model <model> --key-var <keyvar>
     [--base-url <url>]` where `VERIFY="$LIB_DIR/providers-verify.sh"`
     (`scripts/claude-providers.sh:43`). Failure ⇒ status `failed`, **env/alias files are NOT
     written** (`continue` at line 1352 after `cma_status_write "$pid" failed ...`).
   - Layer "semantic" (code-visibility) — `bash "$SEMANTIC" --provider <id> --model <model>
     --key-var <keyvar>` where `SEMANTIC="$LIB_DIR/providers-semantic.sh"`
     (`scripts/claude-providers.sh:44`). A `sstatus=="unverified"` downgrades the whole record
     to `unverified` (line 1373-1375).
   - (Layer "superpowers_tui" / deep-verify — `cmd_verify --deep`, `scripts/claude-providers.sh:1440-1453`
     — is opt-in per provider, not run during a plain `sync`.)
2. **Write the provider env file** — `cma_provider_write_env` (`scripts/lib.sh:3221-3297`) to
   `$HOME/.local/share/claude-multi-account/providers/<id>.env`
   (`cma_providers_dir()` at `scripts/lib.sh:2995`). Exact keys written:
   `CMA_PROVIDER_ID`, `CMA_PROVIDER_KEYVAR`, `CMA_PROVIDER_TRANSPORT`, `CMA_PROVIDER_BASE_URL`,
   `CMA_PROVIDER_MODEL`, `CMA_PROVIDER_FAST_MODEL`, `CMA_PROVIDER_CONFIG_DIR`,
   `CMA_PROVIDER_CONTEXT_LIMIT`, `CMA_PROVIDER_MAX_OUTPUT`, `CMA_PROVIDER_ALIAS` (single-quoted,
   POSIX-escaped, shell-sourceable — `scripts/lib.sh:3260-3282`).
3. **Write the shell alias** — `cma_provider_write_alias` (`scripts/lib.sh:3301-3318`) appends
   `alias <name>="cma_run_provider <id>"` into `$ALIAS_FILE`
   (`$HOME/.local/share/claude-multi-account/aliases.sh`, default at `scripts/lib.sh:21`),
   which is sourced from the user's rc files (`cma_ensure_alias_file`, `scripts/lib.sh:2904-2925`).
4. **Write status** — `cma_status_write "$pid" "$vstatus" "$model" "$flayer"`
   (`scripts/claude-providers.sh:1382`) into `status.json` next to the env files
   (`cma_status_cache()` at `scripts/lib.sh:3003`). Status is one of
   `verified | unverified | failed | pending`.
5. **Orphan demotion** — any status/env entry no longer in the current resolved set is demoted
   (`cma_demote_orphans`, called at `scripts/claude-providers.sh:1391`).
6. Logs a one-line summary: `sync done: N active, N disabled (failed verify), N not-resolved`
   and tells the operator `reload your shell or: source $ALIAS_FILE`
   (`scripts/claude-providers.sh:1393-1394`).

**Confirming it worked** (this is the FR-034/FR-035 evidence path):

- `claude-providers list` — verified aliases only (`_list_rows verified`,
  `scripts/claude-providers.sh:1500`, row-emitter `scripts/claude-providers.sh:1462-1499`).
- `claude-providers list-all` / `list-faulty` — everything / only broken
  (`scripts/claude-providers.sh:1501-1502`).
- `claude-providers verify <id> [--deep]` (`scripts/claude-providers.sh:1406-1453`) — re-runs
  the layer pipeline for one alias and prints the terminal verdict.
- `scripts/alias_e2e_test.py --alias <name> [--all] [--verbose]` — the genuinely **live** proof:
  reads the alias's env file, resolves the real chat endpoint through `ccr`'s config
  (`get_ccr_config()` / `chat_endpoint_for()`, `scripts/alias_e2e_test.py:94-121`), sends the
  literal prompt `"Do you see our codebase? Reply with YES or NO and explain briefly."` with
  `cache_control` attached (to also prove `ccr`'s cache_control-stripping works), and classifies
  the result as **pass**, **genuine fail**, **quota-skip**, or **transient-skip**
  (`scripts/alias_e2e_test.py:36-70`, `test_provider_direct` at line 122-190). Exit codes: `0`
  pass/quota-skip, `1` genuine fail, `3` nothing-installed skip
  (`scripts/alias_e2e_test.py:9-14`).
- `claude-release-gate` (documented `docs/Provider_Aliases_User_Guide.md:1088-1130`) is the
  **mandatory pre-release** live check: sandbox suite + a real launch through
  `PATH → ccr → route-apply → proxy → provider backend` asserting the reply body and, for
  router transport, that `ccr`'s own applied route (`~/.claude-code-router/<alias-id>/config.json`
  → `.Router.default`) actually names the provider under test
  (`docs/Provider_Aliases_User_Guide.md:1100-1109`). This is the literal mechanism FR-034/FR-036
  are asking to be exercised before any toolkit release.

### 1.4 What is genuinely MISSING for FR-013/FR-014/FR-016 (multi-model, provenance naming)

Today each detector emits **at most two** models per backend (`strong`, `fast`) as the fixed
positions 1 and 2 of the live `/v1/models` listing, or the pinned default if unpinned selection
fails (`scripts/claude-providers.sh:308-339`). There is **no** mechanism that turns "N models
served by this host" into N distinct aliases the way `providers_generate.py` does for verified
cloud-catalogue models (`pair_models()`, `scripts/providers_generate.py:39-79` — 2-per-alias
pairing with `alias`, `alias2`, `alias3`... naming). That pairing generator is **cloud-only**
today (driven by `model_verify.py` catalogue scores); it is not wired to the local detectors.

**MISSING, concretely:**
1. A way to enumerate *every* served model on a HelixLLM host as its own alias/provider
   record (not just position 1/2) — the closed analogue of `providers_generate.py`'s pairing
   loop applied to `detect_helixllm_records()`'s live `ids` list instead of
   `verified_models.json`.
2. A way to enumerate *multiple hosts* (FR-021 discovery: current host, LAN, cloud-configured)
   — the detectors are single-host (`CMA_HELIXAGENT_HOST`/`CMA_HELIXLLM_*` are one value each,
   not a list; `scripts/claude-providers.sh:258-259`).
3. Emitting `CMA_PROVIDER_MODEL` (and therefore what `claude-providers list` shows and what the
   wire `model` field carries) in the `helixllm/<host>/<model>[:<variant>]` form. Today it is
   the pinned facade string `"HelixAgent/HelixLLM"` / `"helixllm-multi"` or the raw live id —
   neither matches FR-014.
4. Because `cma_validate_alias` forbids `/` and `:` (`scripts/lib.sh:334`), the **shell alias
   name / provider id** for a per-model entry needs a *separate*, slugified identifier (e.g.
   `helixllm-<host-slug>-<model-slug>`), with the full `helixllm/<host>/<model>[:<variant>]`
   string living only in `CMA_PROVIDER_MODEL` / the `.env` file / `claude-providers list`
   output — never as the alias/provider id itself.

**Already reusable (do not reimplement — §11.4.74):**
- The live-`/v1/models`-enumeration + pin-precedence + honest-fallback-on-unreachable pattern
  in `detect_helixllm_records()` (`scripts/claude-providers.sh:375-473`) is the correct shape
  to iterate over — extend it to loop over every id returned, not just position 1/2.
- The whole env-write → alias-write → verify → status → activation-gate → orphan-demote
  pipeline in `cmd_sync()` (`scripts/claude-providers.sh:1274-1394`) is generic over
  `provider_id`/`alias` and needs **no change** to accept many more records per sync pass —
  it already dedupes by `provider_id` (`scripts/claude-providers.sh:1311-1314`), so each
  per-model record just needs a **distinct** `provider_id`.
- The activation gate at launch (`cma_run_provider`, `scripts/lib.sh:1212` onward, gate logic
  at `scripts/lib.sh:1279-1290`) already refuses to launch anything whose `status.json` entry
  isn't `verified` — this is the existing, reusable answer to FR-019 ("a stopped model is not
  presented as usable") at the Claude Code launch layer, symmetric with HelixLLM's own
  `Brain.Models()` availability filter (§3).

### 1.5 Availability semantics already in place (FR-019)

- **At sync time**: a model/backend that fails the existence probe writes status `failed` and
  its env/alias files are never (re)written — it simply is not launchable
  (`scripts/claude-providers.sh:1348-1353`).
- **At launch time**: `cma_run_provider` reads `status.json` and refuses anything not
  `verified` (unless `--force`), printing an actionable message
  (`scripts/lib.sh:1279-1298` — full text not re-quoted here, confirmed present).
- **At list time**: `claude-providers list` shows only `verified`; `list-faulty` shows
  everything else with its failing layer (`scripts/claude-providers.sh:1462-1499`).

This three-tier mechanism (sync-time gate → launch-time gate → list-time filter) is the direct,
reusable analogue of FR-019 and needs no new primitive — only feeding it many more per-model
records.

### 1.6 Existing docs already covering the single-model case

`docs/Provider_Aliases_User_Guide.md:980-1130` — a full section, "HelixAgent (helixagent) —
local single-GPU model", documents: the shared-GPU "claude mode" vs "coder mode" mutual
exclusion with `helix_code/scripts/helixllm-mode.sh` (an **external** script the toolkit calls
by convention, not one it ships), the opt-in `CMA_HELIX_AUTOSTART` auto-start flag (off by
default because starting HelixLLM in claude mode evicts HelixCode's coder-mode slot — a
machine-wide side effect), and the `CMA_PROVIDER_TRIM='bare'` minimal-launch mode needed because
a 229,376-token local window is dwarfed by a normal Claude Code session's auto-resumed history +
tool schemas. This whole section describes the **current single-GPU, single-model** constraint
that spec 002's host-capability-measurement and multi-model/multi-host placement work is meant
to generalize — it is prior art to extend, not to ignore.

---

## 2. `claude_toolkit` vs `submodules/claude-toolkit` — which is authoritative (blocks FR-037)

**Finding: there is exactly one canonical GitHub/GitLab repository. Two names both resolve to
it. The two local checkouts are at the identical commit and push to the identical remote set.**

Evidence, gathered live:

```
$ gh repo view vasic-digital/claude_toolkit  --json name,url,id,createdAt
{"name":"claude_toolkit","id":"R_kgDOSn8-DQ","createdAt":"2026-05-26T05:03:01Z", ...}
$ gh repo view vasic-digital/claude-toolkit  --json name,url,id,createdAt
{"name":"claude_toolkit","id":"R_kgDOSn8-DQ","createdAt":"2026-05-26T05:03:01Z", ...}
```

Identical repo id and creation timestamp for both name forms — GitHub's rename-redirect makes
the hyphen name resolve to the same physical repository as the canonical underscore name.

```
$ git ls-remote git@github.com:vasic-digital/claude_toolkit.git HEAD   → 75d25ab3...
$ git ls-remote git@github.com:vasic-digital/claude-toolkit.git HEAD   → 75d25ab3...
$ git ls-remote git@gitlab.com:vasic-digital/claude_toolkit.git HEAD   → 75d25ab3...
$ git ls-remote git@gitlab.com:vasic-digital/claude-toolkit.git HEAD   → 75d25ab3...  (redirects too)
$ git ls-remote git@gitflic.ru:vasic-digital/claude-toolkit.git HEAD   → 75d25ab3...
$ git ls-remote git@gitverse.ru:vasic-digital/claude_toolkit.git HEAD  → 75d25ab3...
$ git ls-remote git@gitverse.ru:vasic-digital/claude-toolkit.git HEAD  → error: Cannot find repository (does not exist)
$ git ls-remote git@gitflic.ru:vasic-digital/claude_toolkit.git HEAD   → fatal: Could not read from remote repository (does not exist)
```

So: **GitHub and GitLab** both serve the underscore name canonically and redirect the hyphen
alias to it. **GitVerse** has only the underscore name (no hyphen repo exists there).
**GitFlic** has only the hyphen name (no underscore repo exists there — likely because GitFlic
never got a rename, or never allowed the underscore form).

This exactly matches what both local checkouts already push to. `git config --local -l` for
BOTH `/home/milosvasic/Projects/claude_toolkit/.git/config` and
`/home/milosvasic/Projects/helix_code/.git/modules/submodules/claude-toolkit/config` show the
**identical** `origin` push-url set:

```
[remote "origin"]
  pushurl = git@gitflic.ru:vasic-digital/claude-toolkit.git   # hyphen — GitFlic's only name
  pushurl = git@github.com:vasic-digital/claude_toolkit.git   # underscore — GitHub canonical
  pushurl = git@gitlab.com:vasic-digital/claude_toolkit.git   # underscore — GitLab canonical
  pushurl = git@gitverse.ru:vasic-digital/claude_toolkit.git  # underscore — GitVerse's only name
```

The only difference between the two checkouts is the **fetch** URL of `origin` — the standalone
project fetches from `claude_toolkit.git` (underscore), the submodule fetches from
`claude-toolkit.git` (hyphen) — and since GitHub redirects the hyphen fetch to the same repo,
both checkouts land on the identical commit. This is why `git log -1` agrees at `75d25ab3` in
both places.

**Conclusion for FR-037**: publishing a release today is **not** actually at risk of going to
"the wrong repo invisible to the other" — there is no other GitHub or GitLab repo to be
invisible from; both names are the one repo. The genuine defect is internal, not external:

- `/home/milosvasic/Projects/helix_code/.gitmodules:517-519` declares
  `submodules/claude-toolkit` with `url = git@github.com:vasic-digital/claude-toolkit.git`
  (the **legacy hyphen alias**), which is CONST-052-non-compliant (submodule directory + URL
  should be lowercase snake_case, i.e. `claude_toolkit`) even though it still resolves
  correctly today via GitHub's redirect.
- The submodule's own `origin` fetch URL should be pointed at the canonical
  `git@github.com:vasic-digital/claude_toolkit.git` to remove the dependency on the redirect
  (a redirect can be a fragile assumption to build a release process on, and CONST-052 already
  mandates the rename).

This is a rename/URL-hygiene fix (submodule path, `.gitmodules` URL, and directory rename to
`submodules/claude_toolkit`), **not** a release-target ambiguity. The spec's clarification
("resolve this before release") is satisfiable by this rename alone; no data or history is at
risk since both names already point at the same object graph.

---

## 3. OpenCode — provider config shape needed (currently `MISSING` in the toolkit)

The toolkit's only OpenCode integration is Skills/MCP/instructions sync
(`OpenCode_Integration.md`, driven by `scripts/opencode_sync.py` and
`scripts/claude-opencode-sync.sh`). Grepping `scripts/opencode_sync.py` for any `"provider"`
handling returns **zero matches** — confirmed empty. The doc states outright: "your providers
and any pre-existing MCP keys are never clobbered" (`OpenCode_Integration.md:60`).

`docs/research/innovations/04-new-features-and-technologies.md:35` (an internal design-research
note, not implemented code) observes: "the toolkit already has the *scaffolding* for ... a
local provider (the `detect_helixagent_record` pattern generalizes trivially) ... None of these
are wired up." This confirms the gap is understood but genuinely unimplemented.

**MISSING**: there is no code in this toolkit that reads/writes OpenCode's `provider` config
key at all. What's confirmed from the existing sync script's own env-var contract
(`OPENCODE_CONFIG` defaulting to `~/.config/opencode/opencode.json`,
`OpenCode_Integration.md:100-105`) is only the **file path** consuming-tool config would need to
merge into — the shape of the `provider.<id>` object itself (OpenCode's real schema —
`options.baseURL`, per-model `models.<id>`, an SDK adapter id like
`@ai-sdk/openai-compatible`) is **not present anywhere in this codebase** and must be sourced
from OpenCode's own upstream documentation during planning (external research, out of scope for
this code-reading pass — flagged here so the plan phase does not assume it exists in-repo).

What IS reusable from the existing sync machinery, by direct analogy to the MCP-merge pattern
already implemented (`scripts/opencode_sync.py`, described step-by-step in
`OpenCode_Integration.md:41-63`):
- **Additive-only merge** into the existing `opencode.json`, never clobbering
  pre-existing provider entries (same rule the MCP merge already follows,
  `OpenCode_Integration.md:60`).
- **Backup-before-write**: `opencode.json.bak.<timestamp>` + atomic write
  (`OpenCode_Integration.md:63`).
- **Dry-run + stats preview** (`claude-opencode-sync --dry-run --stats`,
  `OpenCode_Integration.md:97`) — the same flag shape a new `--sync-providers` (or similar) mode
  should offer.

---

## 4. HelixCode's own inner Go app — the real provider mechanism

Checkout: `/home/milosvasic/Projects/helix_code/helix_code` (the inner Go module, `go 1.26`,
per root `CLAUDE.md` §3.2.1).

### 4.1 What's declared but dead: `config/config.yaml`

`config/config.yaml:44-172` declares an `llm.providers` map with per-provider `type` /
`endpoint` / `enabled` / `parameters` blocks, including:

```yaml
    # HelixAgent providers
    helix-llm:
      type: helix-llm
      endpoint: "${HELIX_LLM_ENDPOINT:http://localhost:8081}"
      enabled: true
      parameters:
        timeout: 60.0
        max_retries: 3
        streaming_support: true
        api_key: "" # Set via HELIX_LLM_API_KEY

    helix-debate:
      type: helix-debate
      endpoint: "${HELIX_DEBATE_ENDPOINT:http://localhost:8082}"
      enabled: true
      ...
```

**MISSING**: neither `"helix-llm"` nor `"helix-debate"` exists as a `ProviderType` constant, nor
as a `case` in the factory switch, anywhere in non-test `.go` source (`grep -rn '"helix-llm"\|
"helix-debate"\|ProviderTypeHelixLLM\|ProviderTypeHelixDebate' internal/llm/*.go` returns
nothing). Worse: `internal/config/config.go`'s `LLMConfig` struct
(`internal/config/config.go:186-191`) has only `DefaultProvider`, `DefaultModel`, `MaxTokens`,
`Temperature` — **no `providers` field at all** — and `Config.Providers`
(`internal/config/config.go:223`, type `ProvidersConfig` at line 234-239) is a **completely
different, unrelated struct** holding memory-backend credentials (`Mem0Config`, `ZepConfig`,
`MemontoConfig`, `BaseAIConfig`) — not LLM inference providers. A repo-wide grep for
`map[string]ProviderConfigEntry` or any `viper.UnmarshalKey("llm...")` call returns zero
matches. **`config/config.yaml`'s entire `llm.providers` block is orphaned YAML that no Go code
loads.** Any plan that assumes editing this YAML wires up a new provider is wrong; it does not.

### 4.2 The real, live mechanism

`NewProvider(config llm.ProviderConfigEntry)` (`internal/llm/factory.go:9-104`) is the
catch-all constructor switch, keyed on `config.Type` (a `ProviderType` string const, full list
`internal/llm/missing_types.go:39-91`). Its OpenAI-compatible-local branch
(`newOpenAICompatibleFromConfig`, `internal/llm/factory.go:107-124`) is reused by
`NewOpenAICompatibleProvider(name, cfg)` for named local backends.

The **actual** HelixCode→HelixLLM wiring is a hand-written special case, not config-driven, in
the live HTTP handler `resolveLLMProvider` (`internal/server/llm_generate.go:174-247`), which
services `POST /api/v1/llm/generate` (the exact endpoint CLAUDE.md §9's smoke test curls):

```go
// internal/server/llm_generate.go:198-201
if strings.EqualFold(requested, "helixllm") || strings.EqualFold(requested, "local") {
    return resolveHelixLLMLocalProvider(model)
}
```

```go
// internal/server/llm_generate.go:538-552
func resolveHelixLLMLocalProvider(model string) (llm.Provider, error) {
    cfg := llm.OpenAICompatibleConfig{
        BaseURL:          envHelixLLMLocalEndpoint(),
        DefaultModel:     strings.TrimSpace(model),
        Timeout:          120 * time.Second,
        StreamingSupport: true,
    }
    provider, err := llm.NewOpenAICompatibleProvider("helixllm", cfg)
    ...
}
```

```go
// internal/server/llm_generate.go:509-522
const helixLLMLocalOpenAIEndpointEnv = "HELIX_LLM_LOCAL_OPENAI_ENDPOINT"
...
const helixLLMLocalDefaultEndpoint = "http://localhost:18434"
```

Provider name `"helixllm"` OR `"local"` (case-insensitive), resolved **before** the four-cloud-
provider `llm.Select`/`llm.NewCloudProvider` path (`internal/server/llm_generate.go:203-231`,
whose own doc-comment explicitly scopes it to Anthropic/Bedrock/VertexAI/Azure/Groq/etc. and
says it deliberately does not recognize `"helixllm"`/`"local"` — hence the early special case).
Default endpoint `http://localhost:18434` matches the toolkit's `helixagent.json` `base_url`
(`http://127.0.0.1:18434/v1`, §1.1) — same port, confirming this is the same live HelixLLM
instance both tools already agree on. Configurable via env var `HELIX_LLM_LOCAL_OPENAI_ENDPOINT`
(this satisfies FR-020's "environment variables ... for endpoint selection").

### 4.3 The model-list mechanism — FR-016's real hook

`OpenAICompatibleProvider.GetModels()` (`internal/llm/openai_compatible_provider.go:214-216`)
returns `p.models`, populated once at construction by `discoverModels()`
(`internal/llm/openai_compatible_provider.go:150-151` calls it; body around
`internal/llm/openai_compatible_provider.go:395-432`): a `GET {BaseURL}{ModelEndpoint}` (default
`/v1/models`) request, decoded as `{"data": [{"id": ...}]}`, mapped 1:1 to
`ModelInfo{Name: model.ID, Provider: p.GetType(), ...}` (line 423-430). For a provider
constructed with `name="helixllm"`, `p.GetType()` falls through to the `default:` branch
(`internal/llm/openai_compatible_provider.go:196-198`) and returns `ProviderType("helixllm")`
verbatim — so every model this provider reports is correctly attributed to a `"helixllm"`
provider type.

`resolveDefaultModel` (`internal/server/llm_generate.go:277-291`) is the consumer of this
catalogue: when a request omits `model`, it takes the **first** entry from
`provider.GetModels()` rather than inventing a literal — explicitly citing CONST-036/CONST-037
("LLMsVerifier is the single source of truth ... never a hardcoded literal",
`internal/server/llm_generate.go:258-261`) and a real historical defect this fixed (empty model
→ upstream 400 → API 502, `internal/server/llm_generate.go:265-270`).

**This `GetModels()` → `ModelInfo.Name` chain is exactly FR-016's "make the active model
configuration available to consuming layers" hook for HelixCode.** It already works end-to-end
for a single HelixLLM host at a fixed endpoint. What it inherits unmodified from wherever
`model.ID` comes from is the naming problem in §0/§4.4 below.

### 4.4 Where the FR-014 naming scheme should actually be implemented

Traced to the source: `submodules/helix_llm/internal/gateway/openai.go:526-536` (`HandleListModels`,
serving `GET /v1/models`) returns `b.Models()` when a `*brain.Brain` is wired, else a
hardcoded fallback list. `Brain.Models()` (`submodules/helix_llm/internal/brain/brain.go:319-333`):

```go
func (b *Brain) Models() []api.Model {
    var models []api.Model
    for _, p := range b.providers {
        if !p.Available() {
            continue                      // <- FR-019 filtering, already correct
        }
        for _, m := range p.Models() {
            models = append(models, api.Model{
                ID:      m,                // <- raw provider-reported id, NOT prefixed
                Object:  "model",
                Created: 1700000000,
                OwnedBy: p.Name(),
            })
        }
    }
    return models
}
```

Two load-bearing findings:
1. **Availability is already correctly modeled at the HelixLLM source** — `p.Available()` gates
   inclusion, so a stopped/unavailable provider's models are already absent from `/v1/models`
   before either the toolkit or HelixCode ever sees them. FR-019 is therefore already satisfied
   at the *serving* layer; both consumers (toolkit's live enumeration, HelixCode's
   `discoverModels()`) inherit this for free.
2. **The `ID` field is the raw per-provider model string (`m`), with no host or provenance
   prefix.** This is the single place FR-014's `helixllm/<host>/<model>[:<variant>]` scheme
   should be constructed — rewriting `ID` here (or in the gateway handler wrapping
   `b.Models()`) means every consumer — the toolkit's `detect_helixllm_records()` live listing,
   HelixCode's `OpenAICompatibleProvider.discoverModels()`, and any future OpenCode sync that
   also does live discovery against the same `/v1/models` endpoint — automatically gets the
   correctly-named model with **zero** per-consumer rewriting logic, which is also the cheapest
   way to satisfy FR-015 ("keep this naming scheme stable across releases": one emission point,
   one place to version).

---

## 5. Per-tool table — what must be produced, where, and how availability is represented

| Consumer | Config artefact to generate | Where it lives | `model` field content | Availability representation |
|---|---|---|---|---|
| **Claude Code (Claude Toolkit)** | One `.env` file per selected model/host combination, `cma_provider_write_env` shape (`CMA_PROVIDER_ID`, `CMA_PROVIDER_MODEL`, `CMA_PROVIDER_BASE_URL`, `CMA_PROVIDER_TRANSPORT=router`, `CMA_PROVIDER_CONTEXT_LIMIT`, `CMA_PROVIDER_MAX_OUTPUT`, ...) + one shell `alias <name>="cma_run_provider <id>"` line | `$HOME/.local/share/claude-multi-account/providers/<id>.env` + `$HOME/.local/share/claude-multi-account/aliases.sh` (sourced from the user's rc file) | `CMA_PROVIDER_MODEL` = the FR-014 string `helixllm/<host>/<model>[:<variant>]`; `<id>`/`<name>` = a separate, slugified, POSIX-safe token (alias/id regex forbids `/` and `:` — `scripts/lib.sh:334`, `scripts/lib.sh:3305-3309`) | `status.json` entry `verified\|unverified\|failed\|pending`; `cma_run_provider`'s launch-time activation gate refuses anything not `verified` (`scripts/lib.sh:1279-1298`); `claude-providers list` shows only `verified` |
| **OpenCode** | A `provider.<id>` entry in `opencode.json` (schema **not present in this codebase — MISSING**, must be sourced from OpenCode's own docs during planning; the merge mechanics — additive-only, backup-before-write, dry-run preview — are established by the existing MCP-sync code path, `OpenCode_Integration.md:41-63,97`) | `$OPENCODE_CONFIG` (default `~/.config/opencode/opencode.json`, `OpenCode_Integration.md:100-105`) | Whatever field OpenCode's schema uses for the served model id — must carry the same `helixllm/<host>/<model>[:<variant>]` string sourced from `GET /v1/models` for cross-tool consistency (FR-014's "the name alone MUST ... identify the option ... without the user consulting anything else") | No existing mechanism in this codebase — must be designed: likely mirrors the toolkit's per-model `resolved`/`unresolved` gate, since OpenCode has no native "is this local model up" concept documented in-repo |
| **HelixCode (this repo's inner app)** | No new on-disk config file is required for the *existing* single-endpoint case — `resolveHelixLLMLocalProvider` already resolves live via `HELIX_LLM_LOCAL_OPENAI_ENDPOINT` (`internal/server/llm_generate.go:519-522`). For the spec's multi-model/multi-host extension: a new `ProviderType` (e.g. add a `helix-llm`/`helixllm` const to `internal/llm/missing_types.go` and a case to `internal/llm/factory.go`'s `NewProvider` switch), OR extend `resolveLLMProvider`'s existing "helixllm"/"local" special case to accept a `helixllm/<host>/<model>[:<variant>]`-shaped `model` string and route to the named host | Env var `HELIX_LLM_LOCAL_OPENAI_ENDPOINT` (already exists, `internal/server/llm_generate.go:509`); `config/config.yaml`'s `llm.providers.helix-llm`/`helix-debate` block exists on disk but is **not loaded by any Go code today** — do not assume editing it wires anything up (§4.1) | `ModelInfo.Name`/`ModelInfo.ID`, sourced from `provider.GetModels()` → `discoverModels()`'s `GET /v1/models` decode (`internal/llm/openai_compatible_provider.go:423-430`) — inherits whatever HelixLLM's `Brain.Models()` emits, so fixing the name at the HelixLLM source (§4.4) fixes this for free | `Brain.Models()` already excludes unavailable providers at the source (`p.Available()` check, `submodules/helix_llm/internal/brain/brain.go:322`) — HelixCode's `discoverModels()` therefore only ever sees currently-available models; `resolveDefaultModel` falls back to `""` (never a fabricated literal) when the catalogue is empty (`internal/server/llm_generate.go:290-292`) |

---

## 6. Open questions / MISSING items for the plan phase (explicit, not inferred)

- **MISSING**: multi-model-per-host enumeration in the Claude Toolkit detectors (§1.4) —
  `detect_helixllm_records()` picks exactly two of N live models; there is no per-model
  alias-fan-out analogous to `providers_generate.py`'s cloud-catalogue pairing.
- **MISSING**: multi-host enumeration/discovery in the Claude Toolkit (§1.4) — one
  `CMA_HELIXAGENT_HOST`/`CMA_HELIXLLM_*` value, not a list; FR-021's "current host, LAN,
  explicitly configured remote endpoints" has no existing scaffolding here beyond
  single-endpoint env-var overrides.
- **MISSING**: OpenCode provider-config generation entirely (§3) — no code, no documented
  schema in this repo; requires external research into OpenCode's real `provider.<id>` shape
  during planning (out of scope for this code-reading pass).
- **MISSING**: a `ProviderType`/factory case for HelixLLM as a config-driven (not
  hand-wired-special-case) provider in HelixCode (§4.1) — the declared `helix-llm`/`helix-debate`
  YAML entries are dead config today.
- **Naming-scheme emission point identified but not implemented**: `Brain.Models()`
  (`submodules/helix_llm/internal/brain/brain.go:319-333`) is where `ID` should become
  `helixllm/<host>/<model>[:<variant>]`; this is a `helix_llm` submodule change, not a
  `claude_toolkit` or `helix_code` change, and every consumer inherits it once done.
- **Rename needed, not a release blocker**: `submodules/claude-toolkit` (path and `.gitmodules`
  URL) should be renamed to `claude_toolkit` per CONST-052 and to stop relying on GitHub's
  rename-redirect (§2) — evidence shows this is safe (same repo id, same commit, same push-url
  set already) but should be done as its own tracked change before/alongside the FR-037
  release, per the spec's explicit call-out.
