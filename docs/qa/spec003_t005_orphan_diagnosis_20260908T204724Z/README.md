# SpecKit 003 — T005 diagnosis gate: why 20 provider aliases report `orphaned`

**Run id:** `20260908T204724Z` (UTC) · **Task:** `T005 [REVIEW]` (Phase 2, blocks US1)
**Scope:** diagnosis only. No repair was designed, proposed or implemented — T005 and
`plan.md` both require the cause to reach the operator first.
**Committed:** no. Nothing here is committed and no source file is left modified.

All files are raw captured command output (§11.4.262). Nothing is a narration of a
run that did not happen.

---

## Verdict

**The hypothesis splits, and both halves matter.**

| | Verdict |
|---|---|
| `list-all` renders a **stored** verdict and performs no resolution | **CONFIRMED** — structural, `03`/`11` |
| That staleness is **why** the 20 are orphaned | **REFUTED** — a live resolve reproduces the verdict exactly, `09` |

**FACT — the cause.** `resolve_records()` builds the resolver's `--keys` argument from
`present_key_vars`, which derives variable NAMES **only from literal `NAME=` assignments
in `$CMA_KEYS_FILE`**. The operator's `~/api_keys.sh` currently contains exactly **two**
such assignments — `CMA_PROVIDER_CA_CERT` and `HELIXAGENT_GATEWAY_KEY` — and **zero** of
the 12 key-variable names the 20 orphans' `.env` files reference (`02`, `04b`). The
catalogue-matching path therefore emits no record for any of them, they fall out of the
resolved set, and `cma_find_orphans` reports them. **On the current inputs the orphan
verdict is CORRECT, not a reporting defect.**

Proved causal rather than correlational by a metamorphic probe (`05`): re-running the
identical live path against a keys file that names those 12 variables moves **10 of the
20** straight into the resolved set and out of the orphan list.

---

## What `cma_find_orphans` actually compares

`cma_find_orphans <resolved-ids>` takes the union of `status.json` keys and
`providers/*.env` basenames, drops any id present in the space-padded `resolved`
string, drops any id carrying a `CMA_PROVIDER_SOURCE` export-ownership marker, and
prints the rest.

The `resolved` argument is **not** a cache. It is computed fresh, per invocation, by
`resolve_records()`, and the two call sites compute it with a byte-identical expression
(`claude-providers.sh:2349` in `cmd_sync`, `:2974` in `cmd_prune` — both quoted in `00`).

`cma_find_orphans` is reachable from exactly one place: `cma_demote_orphans`, called
only from `cmd_sync` (`:2494`), which writes `status=orphaned` into `status.json`.
`list-all` (`cmd_list_all` → `_list_rows all`) never calls it — it reads
`cma_status_read` per `.env` file. **So the `orphaned` an operator reads is a verdict
stored by the last sync, 8 h old at measurement time (`11`).** That is the confirmed
half of the hypothesis, and it is a real defect in the *reporting* surface — it just is
not the cause of these 20.

---

## The discriminating observation

| Set | Count |
|---|---|
| A — `orphaned` stored in `status.json` (what `list-all` prints) | 28 |
| B — live `cma_find_orphans`, real keys file | 29 |
| C — live `cma_find_orphans`, probe keys file naming the 12 key variables | 19 |

- **A ⊂ B, exactly.** All 28 stored orphans are still orphans under a live resolve; the
  only difference is `helixcoder`, which the live run adds. The stored verdict is **not
  stale**. This is the observation that could have confirmed the hypothesis and did not.
- **B → C rescues 10:** `deepseek`, `huggingface`, `hyper`, `kilo`, `nvidia`, `opencode`,
  `openrouter`, `poe`, `sarvam`, `zai-coding-plan`.

The live resolved set with the real keys file is 23 ids and contains **no** catalogue-key
provider at all — it is entirely shell-detector output (`chutes1-5`, `helixagent`,
`helixagent-native`, `helixllm-gateway`, `hyper1-5`, `opencode-go1-5`, `opencode-zen1-5`).

---

## Why research.md eliminated candidate 4 on an experiment that could not respond

`research.md` eliminated "key variables invisible to `present_key_vars`" by supplying a
keys file naming all the variables and observing **byte-identical `list-all` counts**.
`list-all` performs no resolution, so it could not have responded to that change whatever
the truth was — the experiment had no discriminating power. Run through the **live** path
instead, the same change moves 10 of 20 (`05`). **Candidate 4 should be reopened**: it is
not eliminated, and it is adjacent to the measured cause.

---

## What remains UNCONFIRMED (§11.4.6)

Ten ids stay orphaned even with their key variable named in the keys file:

- **7 are multi-alias siblings** of an id that *did* resolve — `nvidia2-4`,
  `openrouter2-5`. Consistent with one-record-per-key-variable; **not independently
  verified here.**
- **3 are NOT explained by any measured mechanism** — `kc-for-coding` and
  `kc-for-coding2` (both `ApiKey_Kimi`; *neither* resolved, so sibling-collision does
  not account for it) and `zai` (`ZHIPU_API_KEY`, unique among the orphans, present in
  the probe, still no record). `PENDING_FORENSICS:` for these three.
- **9 status-only leftovers** (`chutes`, `fireworks-ai`, `helixcoder`, `inference`,
  `novita-ai`, `siliconflow`, `tencent-tokenhub`, `upstage`, `xiaomi`) have no `.env`,
  are invisible to `list-all`, and are a different class with a different remedy.

**Honest boundary.** Everything measured here is a property of resolution and of the
records. Nothing here establishes whether any alias would complete a round trip if
invoked — that is the end-to-end question T005 gates, and it remains open.

---

## Files

| File | What it proves |
|---|---|
| `00_pre_state.txt` | Pre-change fingerprint: toolkit HEAD, sha256 of the file about to be instrumented, the pre-existing uncommitted changes that are **not mine**, and the two byte-identical `resolved_ids` computations |
| `01_cached_side.txt` | The stored side: 53 status entries, 28 `orphaned`, 20 with an `.env` and 8 status-only; catalogue cache 8.4 h old against a 24 h TTL (fresh) |
| `02_present_key_vars.txt` | The resolver's actual input: `present_key_vars` returns **2** names, neither a provider key |
| `02b_keys_file_shape.txt` | The keys file with every value redacted — 2 literal assignments and one `source` line; variables supplied by the sourced file are invisible to the name grep |
| `03_live_resolve_no_trace.txt` | Live resolve, uninstrumented: 23 resolved ids, 29 orphans |
| `04a_orphan_key_vars.txt` | **A recorded instrument failure**, kept: searched `CMA_PROVIDER_KEY_VAR` and returned a confident `<none>` for all 20 |
| `04b_orphan_key_vars_corrected.txt` | Same census with the real field `CMA_PROVIDER_KEYVAR`, positive and negative controls both shown: 12 distinct key variables, **0** returned by `present_key_vars` |
| `05_metamorphic_probe.txt` | The discriminating run: identical live path, keys file naming the 12 variables → 33 resolved, 19 orphans |
| `06_instrumented_trace.txt`, `06_trace_raw.txt` | The T005 instrumentation firing: the resolved set recorded **from inside** `cma_find_orphans`, plus a per-candidate `IN_RESOLVED`/`HAS_ENV` verdict; instrumented stdout is byte-identical to uninstrumented |
| `07_trace_controls.txt` | **A second recorded instrument failure**: the trace's own `wc -l` count said 22 for 23 ids (no trailing newline). Corrected, plus a positive control proving the trace sees a candidate the output filters (`helixllm-anton-…`, export-owned) |
| `08_instrumentation.patch`, `08_restoration.txt` | The exact instrumentation, and proof the file was restored **byte-exact** (sha256 match) with zero residue and an unchanged `git status` |
| `09_three_orphan_sets.txt` | The A/B/C comparison with `comm` sort-order and needle controls |
| `10_credential_audit.txt` | §11.4.10: two independent instruments, positive and negative controls held; the only hits are variable **names**, no values |
| `11_list_all.txt` | The operator-visible surface: 20 rows reading `orphaned`, `CHECKED 8h`, `LAYER orphan` |

## Instrument errors this run made, and did not hide (§11.4.273)

1. **Field-name typo returning a confident zero.** `04a` searched
   `CMA_PROVIDER_KEY_VAR`; the field is `CMA_PROVIDER_KEYVAR`. Every row read `<none>`
   — plausible, actionable, wrong. Kept in place rather than deleted, with the corrected
   census in `04b` carrying both controls.
2. **`wc -l` on output with no trailing newline.** The trace reported
   `T005_RESOLVED_COUNT=22` for 23 ids and concatenated the last id with the next line.
   The raw argument line is authoritative; corrected in `07`.
3. **`grep -c '^T005_CANDIDATE='` undercounted by one** for the same missing-newline
   reason (52 for 53). Reconciled in `07` against the emitted orphan count.
5. **A blind `git status` grep.** A final check ran
   `git status --porcelain | grep -c 'spec003_t005'` and returned **0** — which would
   have read as "the evidence directory was never created". It is an artifact: porcelain
   collapses a wholly-new directory to a single `?? docs/qa/<dir>/` line. Re-run with
   `--untracked-files=all` the same grep finds 17 files, and a fabricated path still
   finds none. The zero described the instrument, not the tree.

4. **A meaningless `$?`.** `01` prints `RC_HISTOGRAM=$?` captured after a pipeline, so it
   is `sort`'s exit code, not `jq`'s. It is not cited anywhere as evidence.

## Method notes

- Every live run used `OFFLINE=1`, so `ensure_catalog` used the on-disk catalogue and
  never rewrote it. `resolve_records` writes nothing. **No operator state was mutated.**
- `claude-providers.sh` is source-guarded (`:3263`), so the real `cma_find_orphans` was
  driven directly with a live resolved set — no `sync`, no status writes, no `.env`
  creation.
- The instrumentation was opt-in (`CMA_T005_TRACE`), append-only, and read by no branch;
  with it enabled the function's stdout was byte-identical to the uninstrumented run.
- The probe keys file held the literal string `t005-fake-not-a-credential`, was mode
  0600, was never used for a network call, and was deleted.
- Host was loaded throughout (load average 11–28, memory ~78–83%) from other projects'
  work. No process was signalled. Nothing measured here is timing-sensitive, so the load
  does not put any reading in doubt.
- `tasks.md` is deliberately **not** marked: T005 is `[REVIEW]` and its result goes to
  the operator before US1 proceeds.
