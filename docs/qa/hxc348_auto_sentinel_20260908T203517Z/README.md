# HXC-348 follow-up — delegation-sentinel pass-through (2026-09-08T20:35:17Z)

Closes the regression that `docs/qa/hxc348_status_reconciliation_20260908T203131Z/`
found in the in-tree HXC-348 fix: `{"model":"auto"}` — a documented sentinel —
was being refused with 404 as though it named an unserved model.

Scope: `submodules/helix_llm` only. `certs/`, `cmd_sync` and
`submodules/helix_agent` were not touched (verified, `13_change_diff.txt` and the
`git status --porcelain` in this file).

**Not committed, not rebuilt, not redeployed** — deliberately. Those steps follow
an independent review (§11.4.142) and are the conductor's to sequence.

---

## 1. The sentinel audit (task item 1)

Instrument discipline per §11.4.273: every sweep below was run with a positive
control (a known-present value must be found through the same command and path)
and a negative control (a fabricated needle must not be found). One instrument
fault was caught and corrected mid-audit — a probe for `model="*"` returned 308
hits because `*` was read as a regex quantifier; re-run with `grep -F` it returns
0. That correction is why `*` is **not** in the set below.

Method: extract every `"model": "<value>"` literal across the whole `helix_code`
tree (parent repo + all submodules), then filter to delegation words.

**The complete set actually in documented use is `{"", "auto", "default"}` — three
members, no more.** Probes for `any`, `best`, `none`, `first`, `cheapest`,
`fastest`, `smart`, `router`, `latest` and `*` as model values returned zero hits
across the entire tree.

| Sentinel | Evidence | Verdict |
|---|---|---|
| `""` (absent/empty) | Already exempt pre-fix; `requestvalidate.go` records that an empty model is deliberately not a fault | Baseline — unchanged |
| `auto` | **15** occurrences in `helix_llm` + **8** more in the parent repo. Shipped surfaces: `website/content/_index.md:29` (public homepage), `docs/courses/01-getting-started/lesson-01-introduction.md:131`, `…/lesson-03-first-api-call.md:202`, `challenges/banks/performance/nonblocking.yaml:34,65`, `tests/e2e/e2e_test.go:60`, `tests/e2e/multi_provider_e2e_test.go:24`, `tests/stress/stress_test.go:34`, `tests/integration/auth_test.go:51`, plus `submodules/helix_agent/challenges/scripts/userflow_comprehensive_challenge.sh` (×4). A plan doc states the semantics outright: `"model": "auto", // let the fallback chain decide`. It is also the project-wide convention for every other choose-for-me setting (`HELIX_CONTAINER_RUNTIME`, `HELIX_SCHEDULE_STRATEGY`, `HELIX_LLM_DEFAULT_PROVIDER` all take `auto`). | **Sentinel** |
| `default` | **15** occurrences in the parent repo. Decisive: `CHANGELOG.md:40` documents it by name under *Fixed* — *"A client requesting an alias (e.g. `"model":"default"`) now gets back the model that actually served the request, on both the OpenAI- and Anthropic-compatible wires, streaming and non-streaming — not the alias echoed back."* Three live QA capture scripts post it against a running gateway (`scripts/qa/capture_feat_{helixllm_a21ad7ca,azureguard_eb233785,wirefacade_51c058b1}.sh`); `docs/OPERATOR_GUIDE.md:419` uses it too. | **Sentinel** |

Honest correction recorded (§11.4.6): on first pass I classified `default` as a
*weak-evidence* member, because its only occurrence I had then seen
(`OPERATOR_GUIDE.md:419`) targets the raw llama.cpp port `18434` rather than the
gateway on `:8443`, and so does not traverse this code path. Widening the sweep
to the parent repo overturned that — the CHANGELOG names it as a shipped alias
and specifies exactly the behaviour implemented here. The code comment was
corrected accordingly rather than left understating the evidence.

### Boundary found, and deliberately not changed

A **space-padded** rendering (`"  auto  "`) never reaches the chain: the
gateway's model-name charset validation rejects it with **400** first. That is
pre-existing behaviour unrelated to HXC-348, and a model name carrying spaces is
malformed. Relaxing the validator to accept it would weaken an unrelated guard to
serve a rendering nothing documents, so it was left alone and pinned instead
(`TestHXC348Sentinel_SpacePaddedSentinelIsRejectedAtValidation`), so the boundary
is a recorded fact rather than an assumption.

---

## 2. What changed (task item 2)

Two refusal sites existed for one condition, and only one of them was on the
serving path the reconciliation identified. Both now share **one** definition, so
they cannot drift into disagreeing about which requests name a model.

- **NEW `internal/brain/delegation_sentinel.go`** — `IsDelegationSentinel`, the
  single keyword set, matched **exact** (not prefix/substring) and case-folded.
- **`internal/fallback/chain.go`** — `refuseUnknownModel` now exempts any
  sentinel, not just `""`. (The `strings` import the HXC-348 fix added became
  unused and was removed.)
- **`internal/brain/router.go`** — `Route` step 4 exempts the same set. Without
  this, `auto` would still 404 via `Brain.Complete`/`CompleteStream`.

The refusal HXC-348 exists to deliver is **not** weakened: `gpt-4o`,
`claude-opus-4` and `definitely-not-a-real-model-zzz` still return 404
`model_not_found`, and so do sentinel *lookalikes* (`auto-gpt`, `automatic`,
`autopilot-7b`, `default-model`, `gpt-auto`, `auto/llama`) — a prefix or
substring match would have silently re-opened the defect for every id with a
sentinel-shaped prefix, so exact matching is asserted as a negative control.

A deployment that genuinely registers a model named `auto` is unaffected: both
sites consult the sentinel set **only after** the pin has already missed.

---

## 3. A fourth serving path was uncovered (task item 4, extended)

The task named three paths. Auditing the CHANGELOG sentence above — *"on both the
OpenAI- and Anthropic-compatible wires"* — surfaced a **fourth**: `/v1/messages`
(`gateway.HandleMessages`), which reaches the same `Completer` and was covered by
neither the original HXC-348 tests nor the first draft of these. That is the same
class of gap that let the `auto` regression ship. Every sentinel is now exercised
on **all four** paths:

| Path | Buffered | Streaming |
|---|---|---|
| `POST /v1/chat/completions` | ✅ | ✅ |
| `POST /v1/completions` | ✅ | n/a |
| `POST /v1/messages` (Anthropic) | ✅ | ✅ |
| `brain.Router.Route` (second refusal site) | ✅ | n/a |

Beyond status codes, the guards also assert the *content* contract the CHANGELOG
specifies: a sentinel response must name **the model that actually served**, never
the sentinel echoed back (`…_SentinelIsAnsweredByAServedModel`), and a streaming
sentinel must produce real SSE chunks rather than an empty 200
(`…_SentinelStreamsRealChunks`).

---

## 4. Evidence files

| File | What it shows |
|---|---|
| `01_RED_gateway_sentinel_prefix.txt` | **RED.** New guards against the in-tree code: 23 `--- FAIL`. `model="auto"` → `404 {"code":"model_not_found"}` where 200 is required. Written and observed failing **before** any implementation. |
| `02_GREEN_hxc348_all_packages.txt` | First GREEN, 64 subtests, exit 0. |
| `03_mutation_control_pre_green.txt` | **Control that caught a workspace defect** — the first scratch copy failed to build because `go.mod` `replace` directives point at sibling submodules. Without this control the later mutation FAIL would have been meaningless. |
| `04_MUTATION_red_mode_0_must_fail.txt` | **§1.1 pair, round 1.** Sentinel pass-through removed on the scratch copy → guards fail across both packages and all paths. exit 1. |
| `05_RED_MODE_1_on_mutated_must_pass.txt` | **§11.4.115 RED baseline.** `RED_MODE=1` against the defect-carrying artifact: gateway **PASSES** (43 subtests) — the reproduction is real, not synthetic. The single FAIL is `TestHXC348Sentinel_IsDelegationSentinel`, the polarity-free unit contract, which the mutation must kill by design. |
| `06_mutation_reverted_green.txt` | Pair closed: mutation reverted, green again, zero residue. |
| `07_auth_integration_regression_fixed.txt` | **The reported regression.** `TestAuth_ChatWithoutAuth` (`auth_test.go`, posts `{"model":"auto"}`) → PASS. |
| `08_full_suite.txt` | Full suite, first run: 52 ok / 3 FAIL. |
| `09_internal_testing_flake_determination.txt` | The third failure investigated, not assumed. |
| `10_GREEN_final_all_four_paths.txt` | **Final GREEN**, 81 subtests, 0 fail, exit 0. |
| `11_MUTATION_round2_incl_messages.txt` | **§1.1 pair, round 2** — re-run after adding the `/v1/messages` guards: 39 subtests die under mutation, including every Anthropic-wire guard. |
| `12_full_suite_final.txt` | **Final full suite**: 53 ok / 2 FAIL / 3 no-test = 58 packages. |
| `13_change_diff.txt` | The complete change. |

---

## 5. Full-suite state (task item 6)

Final run (`12_full_suite_final.txt`): **53 ok, 2 FAIL, 3 no-test files, `EXIT=1`.**

The two failures are the **known pre-existing host-capability skips**, and are
**not** attributable to this change:

- `cmd/agentgen-boot` — `TestEnvironmentDoesNotChangeTheDecision`,
  `TestStaticallyNamedFileIsNeverBooted`
- `cmd/visiongen-boot` — `TestProjectorIsNeverMistakenForWeights`

Their own output states the cause: `CANNOT-CHOOSE: no text model can run on this
measured host … memory short by 1654MiB`. Verified, not assumed: neither package
imports `internal/brain`, `internal/fallback` or `internal/gateway`, so no change
made here can reach them (the same grep does find such an import in
`internal/testing/precondition_test.go:25`, so the instrument is proven able to
see one).

**A third package failed on the FIRST run and was investigated rather than waved
through** (§11.4.6): `internal/testing` /
`TestBaselineDiscardsConnectionWarmup`. It does import `gateway`, so it could not
be dismissed on import grounds. Its assertion is a latency budget —
*"concurrency overhead 17.39x … over the 12.0x budget"* — and it was measured
while the full suite was saturating the host (load average **19.52** on 16 CPUs,
other agents active). Re-run standalone it passed **3/3**
(`09_internal_testing_flake_determination.txt`) and it passed in the final full
run. Verdict: **load-sensitive, not caused by this change.** It is a
§11.4.248-shaped flake candidate, recorded here rather than silently ignored.

---

## 6. Still owed

1. **Independent code review** (§11.4.142 / §11.4.209 — Fable at `xhigh`).
2. **Commit** the HXC-348 fix together with this follow-up. Both are still
   uncommitted, including the never-committed `internal/brain/model_not_found.go`.
3. **Rebuild** `bin/helixllm` — the running binary (built 2026-09-07 12:09:35)
   still predates every one of these sources.
4. **Redeploy** the running gateway process.
5. **Re-probe live `:8443`** (§11.4.108 layer 3 / §11.4.130): confirm
   `definitely-not-a-real-model-zzz` → 404 **and** `auto` → 200. Until then the
   original defect remains live in production, exactly as the reconciliation
   recorded.
6. Consider quarantining `TestBaselineDiscardsConnectionWarmup` or widening its
   budget under load (§11.4.248) — it is a real flake, just not this change's.

### Honest boundaries (§11.4.6)

- Everything above is **source- and test-layer** evidence (§11.4.108 layers 1–2).
  No layer-3 runtime-on-clean-target claim is made, because nothing was rebuilt
  or redeployed.
- The audit proves `{"", "auto", "default"}` is the complete set **in the
  documented corpus searched** (whole `helix_code` tree, source + docs + website
  + tests + challenge banks + QA scripts). A sentinel used only by an external
  client and never written down here would not appear — that residual is stated,
  not hidden (§11.4.118).
- A prior assumption of mine was **wrong and is corrected here**: my stored note
  said Go CLI commands are blocked by a PreToolUse hook. They ran normally in
  this submodule. All results above come from real observed runs.
