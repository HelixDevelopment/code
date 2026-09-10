# HXC-350 I1 — response `model` reports the answering provider/model

**Run id:** `hxc350_label_decision_20260909T065114Z`
**Date (UTC):** 2026-09-09
**Repo:** `submodules/helix_agent` @ `b4033a6966be6ac29a0b0322d203a08cc1ca3f71` (working tree, uncommitted)
**Operator decision applied:** on the provider-chain routes the response `model`
field reports **the provider/model that actually answered**, never the requested
alias.

---

## 1. Single-funnel claim — VERIFIED, WITH ONE GAP

The previous agent claimed all six label-emission sites in
`processWithProviderChain` and `streamWithProviderChain` were routed through the
single `return` in `chainResponseModelLabel`.

**Measured (control-needled, §11.4.273), file `00b_control_needles.txt` and the
scan below:**

- `awk 'NR>=3232 && NR<=3413 && /req\.Model/'` over the two named functions →
  **0 matches**. Positive control (`respModelLabel`, a value known present in the
  same range, same command) → 5 matches. Negative control
  (`zzzNotARealSymbolXyz`) → 0.
- The six sites are: 3 × `convertSingleResponseToOpenAI(response, respModelLabel)`
  in `processWithProviderChain`, 3 × `tryStreamWithContentCheck(..., respModelLabel, ...)`
  in `streamWithProviderChain`.

**So the claim holds for the two functions it names — and is incomplete for the
streaming half as a whole.** `streamWithProviderChain` delegates to
`streamToolCallViaNonStreaming` whenever `len(req.Tools) > 0`, and that function
built BOTH of its SSE envelopes from `req.Model` directly:

```
3468:  chunk := h.convertChunkToSSE(resp, streamID, req.Model)
3492:          Model:   req.Model,
```

The path is genuinely reachable under pass-through:
`handleStreamingChatCompletions` routes `passthrough + stream:true` into
`streamWithProviderChain`, which hands any tools-bearing request straight to it.
Tools-bearing streaming requests are exactly what CLI-agent clients send. Left
alone, the decision would have held for two of the three streaming shapes and
silently no-opped for the third.

Both sites are now funnelled. Post-fix the funnel has 5 call sites (3 + 1 in
`tryStreamWithContentCheck` covering all three streaming sites + 1 in
`streamToolCallViaNonStreaming` covering both of its envelopes), all through one
`return`.

---

## 2. What is reported, and where it comes from

`chainResponseModelLabel(providerName string, resp *models.LLMResponse) string`
returns `"<provider>/<model>"`.

- **model half** — the provider's OWN self-report, read from
  `resp.Metadata["model"]`. This is the convention every provider under
  `internal/llm/providers` already follows (`helixllm/provider.go` sets it on
  both the non-streaming and the streaming path; so do claude, gemini, qwen,
  deepseek, zai, openrouter, githubmodels, ollama, venice, junie, zen). Preferred
  over any registry-held name, per the decision: report what ANSWERED.
  `models.LLMResponse` has **no** `Model` field — `Metadata["model"]` is the only
  place a provider states its own model id.
- **provider half** — the provider the chain actually invoked, known at every
  call site (`PrimaryProviderName`, the score-ordered loop variable, or
  `req.ForceProvider`); falls back to `resp.ProviderName`.

**When a provider reports no identity** the label is
`"<provider>/<unreported>"`. Nothing is fabricated and there is NO silent
fallback to the requested alias — a silent fallback would reinstate exactly the
defect this decision fixes, and would do it precisely in the cases nobody
inspects. The angle brackets are not legal in any provider's model identifier,
so the placeholder cannot be mistaken for a model name. A non-string
`Metadata["model"]` is treated as not-reported rather than coerced.

Both halves are reported on the non-streaming body AND on every SSE chunk,
including the tools/SSE path.

---

## 3. Evidence files

| File | What it is |
|---|---|
| `00_baseline_guard_preexisting.txt` | Pre-change baseline of the existing HXC-350 guard — **14/14 PASS**, `go test` exit 0. |
| `00b_control_needles.txt` | §11.4.273 control needles for the PASS-counting instrument (positive 1, negative 0, totals 14/0). |
| `01_RED_green_mode_prefix_MUST_FAIL.txt` | **RED**: new + reconciled assertions run against the UNFIXED code, exit 1, 11 PASS / 7 FAIL. Observed value in all four new tests: `actual: "helixagent-debate"` — the alias. |
| `02_RED_MODE1_prefix_defect_reproduced.txt` | `RED_MODE=1` on the unfixed code: the four new tests PASS (defect reproduced). See §5 for the three pre-existing RED-branch failures. |
| `03_GREEN_postfix.txt` | **GREEN** after the fix: **18/18 PASS**, `go test` exit 0. Positive control 1, negative control 0. |

---

## 4. RED → GREEN (§11.4.115)

Assertions were written and observed failing BEFORE the implementation.

RED (pre-fix, GREEN-mode) — 7 failures, exit 1:

```
--- FAIL: TestPassthroughFlag_SingleUserMessage
--- FAIL: TestPassthroughFlag_MultiTurnBypassPreserved
--- FAIL: TestPassthroughFlag_StreamingHonoursFlag
--- FAIL: TestPassthrough_ModelReportsAnsweringProviderIdentity
--- FAIL: TestPassthrough_StreamingModelReportsAnsweringProviderIdentity
--- FAIL: TestPassthrough_StreamingWithToolsReportsAnsweringProviderIdentity
--- FAIL: TestPassthrough_ProviderWithoutModelIdentityIsHonest
```

with, e.g.:

```
expected: "helixllm/helixllm-stub-v9"
actual  : "helixagent-debate"
```

GREEN (post-fix): `PASS=18 FAIL=0`, exit 0.

Four new tests were added, covering both halves plus the previously-unfunnelled
third path:

- `TestPassthrough_ModelReportsAnsweringProviderIdentity` (non-streaming)
- `TestPassthrough_StreamingModelReportsAnsweringProviderIdentity` (SSE)
- `TestPassthrough_StreamingWithToolsReportsAnsweringProviderIdentity` (tools/SSE)
- `TestPassthrough_ProviderWithoutModelIdentityIsHonest` (no provider identity)

---

## 5. §11.4.120 reconciliation — three pre-existing assertions

The decision deliberately changes a client-visible field, so three assertions
that pinned the OLD behaviour were **reconciled, not weakened or deleted**:

- `TestPassthroughFlag_SingleUserMessage` leg 3 — was
  `assert.Equal(hxc350RequestModel, model)`; now asserts the exact answering
  identity AND adds a negative that the alias must NOT come back.
- `TestPassthroughFlag_MultiTurnBypassPreserved` — same change; the answering
  identity is a sharper route oracle than the alias, which more than one route
  emitted.
- `TestPassthroughFlag_StreamingHonoursFlag` leg 3 — same, plus a second negative.

Each replacement is strictly stronger than what it replaced.

**Honest note on `RED_MODE=1` (§11.4.6).** In `02_...`, three tests FAIL. Two
(`SingleUserMessage`, `StreamingHonoursFlag`) fail on their OWN RED-branch
assertion — lines this change did not touch — because those branches reproduce
the ORIGINAL HXC-350 defect (flag ignored → ensemble label) and the passthrough
fix is already applied in this working tree, so `RED_MODE=1` can no longer
reproduce it here. The third (`MultiTurnBypassPreserved`) has no RED branch: it
is polarity-independent, and its reconciled assertion correctly failed pre-fix,
which is itself RED evidence. All four NEW tests pass under `RED_MODE=1` on the
pre-fix artifact.

---

## 6. Host condition encountered (§12.9) — and what it blocked

`go build ./...` (a whole-module build, run once early) exhausted a per-user
quota on the `/tmp` tmpfs. The failures it produced were all in unrelated
packages (`plugins/example`, `scripts/cli-agents/plugins/generated/**`) and were
linker/disk errors, not compile errors from this change. The exhaustion
repeatedly wedged the shell tool itself (every command, including `echo`,
returning exit 1 with no output).

Worked around for the test runs by redirecting Go's temp:
`TMPDIR=/home/milosvasic/.cache/gotmp` — the `03_GREEN_postfix.txt` run above
succeeded that way.

**NOT COMPLETED, and not claimed (§11.4.6 / §11.4.1):**

- The mandatory §1.1 paired mutation (revert the helper to the pre-fix label on a
  SCRATCH COPY and confirm the new tests FAIL) — the scratch copy was being
  written under the scratchpad, which lives on the same exhausted `/tmp` tmpfs;
  the copy was killed mid-way and the shell has not recovered since.
- The full `./internal/handlers/` package run (beyond the `TestPassthrough`
  subset).

Operator action to unblock: free the `/tmp` tmpfs (a partial copy at
`/tmp/claude-1000/-home-milosvasic-Projects-helix-code/d16d2eb1-de59-4512-9cfb-fe148422826c/scratchpad/mut`
is this session's own leftover and is safe to delete), then re-run the mutation
on a scratch copy placed on the large filesystem rather than on `/tmp`.

Until that mutation is run, the new tests are **not yet proven non-decorative**.
The RED evidence in `01_...` and `02_...` is the closest available substitute —
it shows the assertions genuinely failing against the unfixed code with the
alias as the observed value — but it is not a substitute for the mutation, and
this document does not treat it as one.
