# Verifier TLS + cmd_sync failure-attribution — captured evidence

**Run:** `verifier_tls_cmdsync_20260908T204515Z` (UTC)
**Scope:** respawn of a crashed agent per §11.4.147. Two linked defects were
handed over: (a) a verifier TLS issue around `CMA_PROVIDER_CA_CERT`, and (b) a
"closed-stderr loop at `cmd_sync:2403`".
**Status:** (a) verified working end to end. (b) root-caused, fixed, guarded.
**Not committed, not pushed** — the change is pending independent review
(§11.4.142).

---

## The line reference in the handover was wrong, and that mattered

`cmd_sync:2403` and the `claude-providers.sh:2409` cited in
`specs/003-provable-model-alias-availability/research.md` both point past the
end of the file. `scripts/claude-providers.sh` is **1888 lines** and its eight
most recent revisions run 1182 → 1888; it has never been 2409 lines.

Resolved by content anchor rather than offset (§11.4.111): `cmd_sync()` begins
at **1274**, and the site both references describe — the hardcoded
`failing_layer` — is at **1356**. Every artefact here uses the measured
location. An instrument fault was hit while establishing this: a first
`grep -n` for the exact line returned **empty for a line that was on screen**;
a control-needled `grep -nF` found it at 1356 (§11.4.273 — an empty result is
not a finding).

## (b) Root cause: the reason is produced, then thrown away

`scripts/providers-verify.sh:59` defines the verifier's output contract:

```bash
emit() { echo "$1"; [[ -n "${2:-}" ]] && echo "providers-verify[$PROVIDER]: $2" >&2; }
```

**Verdict on stdout, reason on stderr.** `cmd_sync` captured the stdout and
sent the stderr to `/dev/null`, then wrote a literal:

```bash
vstatus="$( ( … bash "$VERIFY" "${vargs[@]}" 2>/dev/null ) )" || true
…
cma_status_write "$pid" failed "$model" existence      # ← a literal, not a measurement
```

`providers-verify.sh` emits **eight distinct `failed` reasons; only one is
about the model existing.** The other seven — ccr self-route, chat-200-with-
error-body, missing `VERIFY_OK` sentinel, context-inadequate, no tool call,
tool-probe rejected, definitive non-200 — were all filed as `existence`.

This is not cosmetic. Measured on this host, `helixagent` at `:7061` answers a
nonce prompt correctly, so the model plainly exists; it fails only for lacking
tool calling. Its status row said the model was missing — an investigation
aimed at a model that is not missing. It is the same defect class
`scripts/tests/test_failure_cause_attribution.sh` already pins at
`providers-semantic.sh` and `verify_providers_live.sh`. **This is the third
site, and it was uncovered.**

### Fix

- `scripts/lib.sh` — new `cma_verify_failing_layer()` maps the verifier's own
  emitted reason to the layer that actually failed. Keyed on the verifier's
  phrases, so a reworded reason returns `unknown` rather than being mis-filed
  under a neighbour. An absent or unrecognised reason is **`unknown`** — a real
  answer, not a fallback to tidy away (§11.4.6, §11.4.201). The definitive
  non-200 case maps to `chat_http`, because the verifier itself says it is
  "auth/billing/model-missing/account-suspended" and does not single one out,
  so neither may we.
- `scripts/claude-providers.sh` — `cmd_sync` captures the verifier's stderr,
  derives the layer from it, and now also surfaces the reason in the operator
  warning. The reason locals are declared outside the verify block because the
  `--no-verify` path reads them under `set -u`.

## (a) TLS — verified working, not merely configured

`10_tls_runtime_proof.txt`. `scripts/export_gateway_keys.sh:134-138` derives
`CMA_PROVIDER_CA_CERT` from the checkout when unset.

| step | result |
|---|---|
| on-disk cert fingerprint | `C4:7B:D9:…:C3:CF` |
| live `:8443` endpoint fingerprint | **identical** |
| curl **without** `--cacert` | `http=000`, **exit 60** (cert not verified) |
| curl **with** `--cacert` | exit 0, `http=401` — handshake succeeds, auth layer reached |
| curl with `--cacert` + gateway key | **`http=200`** |
| model served | `helixllm-anton-qwen2-5-coder-3b-instruct-q4_k_m-f6771589d190` |

Config presence was not treated as proof (§11.4.108): the 000→401→200 ladder is
what establishes that the trust anchor is doing work.

Note against the research doc: it records "no default for that variable
(CONST-045)", but `export_gateway_keys.sh` **does** derive one, guarded on
readability and never overriding an operator value. The code is the current
state; that sentence is stale.

Also confirmed independently: the key length that returns 200 is **48**, matching
the research doc's note that a stale 23-char environment value returns 401. No
credential value appears in any artefact here — only name, length, and a
truncated SHA-256 (§11.4.10).

## Files

| file | what it proves |
|---|---|
| `00_host_state.txt` | load/threads at capture — tests were not run under unsafe pressure |
| `10_tls_runtime_proof.txt` | (a) the 000 → 401 → 200 TLS ladder |
| `20_red_prefix_body.txt` | RED: **13/13**, all six evidence classes collapse to `existence` on the pre-fix body (`sha256 f2d9871df424…`, 1888 lines) |
| `21_green_fixed_tree.txt` | GREEN: **14/14** on the fixed tree — same test source, polarity flipped |
| `30_mutation1_…txt` | §1.1 — classifier forced to always answer `existence` → **5 failed** |
| `31_mutation2_…txt` | §1.1 — verifier stderr sent back to `/dev/null` → **5 failed** |
| `40_regression_providers_lib.txt` | `test_providers.sh` **421/0**, `test_lib.sh` **50/0** with the change applied |

The two mutations are complementary: M1 spares case E (which expects
`existence`), M2 spares case F (which expects `unknown`). Together they cover
all six cases, and both files were restored **byte-identical** (SHA-256 verified)
before any further work (§11.4.84).

`EXIT=1` in `40_regression_providers_lib.txt` is **my own argument error**, not a
regression: `run-all.sh` was given suffixes that match no filename, so it
reported two files "missing". The two that ran are green, and the other two were
re-run separately to `ALL GREEN`, 2/2.

## Honest boundary (§11.4.6)

- The guard proves the layer written to `status.json` tracks the verifier's
  emitted reason. It does **not** prove the verifier's own verdict is right.
- The classifier is keyed on the current reason phrasing. That is deliberate —
  a reworded reason degrades to `unknown` rather than to a wrong noun — but it
  does mean the mapping needs updating alongside `providers-verify.sh`.
- Existing rows in `status.json` are **not** rewritten. Every currently-recorded
  `failing_layer=existence` remains unproven until its provider is re-synced.
- The same hardcoded `existence` literal survives at three further sites, cited
  by enclosing function rather than line number because this very run proved
  offsets drift (§11.4.111) — my own edits moved them by 12 lines while this
  document was being written:
  - **`cmd_verify()`** — two `cma_status_write … existence` calls, one on the
    `failed` branch and one on the `unverified` branch.
  - **`cmd_sync_multi()`** — one `cma_status_write … unverified … existence`.

  Same defect class, **NOT fixed here** — untouched, unguarded, and owed
  (§11.4.146 STEP 3 extend-to-all-cases). `cma_verify_failing_layer()` is
  already in `lib.sh` and applies unchanged, so the remaining work is wiring
  plus its own RED/GREEN pair, not new design. Locate them with
  `grep -nF '"$model" existence' scripts/claude-providers.sh`.
- The gateway's `/v1/models` returned the full alias id, not the bare
  `qwen2.5-coder-3b-instruct-q4_k_m` the research doc flagged as a wrong-service
  warning. Whether that anomaly is fixed or merely differs on this endpoint was
  **not** established here.
