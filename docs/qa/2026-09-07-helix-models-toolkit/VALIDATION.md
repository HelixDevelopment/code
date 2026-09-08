# Deterministic validation harness — HelixAgent / HelixLLM models as Claude Toolkit aliases

**Run id:** `2026-09-07-helix-models-toolkit`
**Harness:** `scripts/validation/validate_helix_models.py` (wrapper: `validate_helix_models.sh`)
**Captured:** 2026-09-07 12:00–12:20 (+0200), live HTTP against the running services
**Author scope:** the harness and this document only. No service was restarted; no
submodule, `helix_code/internal/**`, or toolkit script was modified.

Every number below was **measured**. Cells that could not be measured are marked
**UNTESTED** and are never presented as passing. FACT and HYPOTHESIS are labelled
explicitly wherever the two could be confused.

---

## 1. Headline

| | |
|---|---|
| Model ids discovered across the three endpoints | 7 |
| **READY** (passed every applicable check, N=3 deterministic) | **1** — `coder/qwen2.5-coder-3b-instruct-q4_k_m` |
| **NOT-READY** (measured failures) | **2** — `helixagent-debate`, `helixllm-gateway` |
| **UNTESTED** (endpoint unavailable during probing) | **4** — `helixagent-llm`, `helixagent-ensemble`, `helix-debate`, `helix-llm` |
| Harness self-validation | **PASS** (7/7 golden fixtures behaved exactly as declared) |

Exit code of the live run: **1** (correct — the system is not green).

Two of the four defects the harness was built to catch were **reproduced and
measured**. A third (dead pins) was **not observed in its original form** but a
new, equivalent break appeared in the same place. The fourth (alias durability)
was deliberately **not run**.

---

## 2. Why the checks are shaped the way they are

Each check was calibrated against the live system *before* being trusted. The
calibration is the reason several "obvious" designs were rejected — each of them
would have produced a green result on a provably broken route.

### 2.1 A start-of-prompt needle would have been a bluff gate

The task brief assumed a truncating route drops the **front** of the prompt. That
is **false** on this stack. Measured (`harness-evidence/calibration_needle_*.py`):

| needle position | coder (honest) | gateway (broken) |
|---|---|---|
| start only | found | **found** |
| start **and** end | both found | **both found** |
| **middle (50 %)** | **found** | **NOT found** — replied `juliet`, a filler word |

The gateway keeps the head **and** the tail and discards the **middle**. A
start-only needle — the design the brief implied — passes on the broken route.
The harness therefore buries the token at **25 %, 50 % and 75 %** and requires
**all three**, which is truncation-direction agnostic.

### 2.2 Byte-inequality alone would have missed the worst stub

`helixagent-debate` returns canned **status strings that differ between prompts**
(`"Comprehensive debate completed with 3 rounds"` vs `"All agent tasks failed
during execution."`). Check C (byte-inequality) **passes** on it. Only the
all-zero `usage` block exposes it.

This is why check B is load-bearing and why golden fixture `bad-stub-varying`
exists: it is a standing, executable proof that C can never be trusted as the
stub detector on its own.

### 2.3 The check-D pass band comes from measurement, not from taste

Measured ratio of reported `prompt_tokens` for an **8× input increase**
(500 → 4 000 filler words):

| route | small | large | ratio | reading |
|---|---|---|---|---|
| coder (control) | 802 | 6 052 | **7.55×** | honest |
| helixagent-llm (control) | 776 | 6 026 | **7.77×** | honest |
| **gateway** | **831** | **1 014** | **1.22×** | **~85 % of the prompt discarded, HTTP 200** |

Thresholds: `< 2.0×` → silent truncation (hard fail); `< 4.0×` → degraded (also a
fail). Honest routes on this stack sit above 7.5×, so a 4.0× floor is far clear of
real behaviour while still catching partial truncation. The band is **stricter**
than the brief's `< 2×`, never looser.

### 2.4 Determinism (Constitution §11.4.50)

Model **wording** is not deterministic even at `temperature 0`, so the harness
never asserts on it. It asserts only on: HTTP status, `usage` counters, response
non-emptiness, byte-**inequality** between two different prompts, token-count
**ratios**, and needle substring **presence**.

`temperature=0` and a pinned `seed` are sent. Every verdict-bearing probe runs
**N=3** and the **verdict** must be identical across all three. A verdict that
varies is reported `NON-DETERMINISTIC` — never majority-voted into a pass.

This fired for real: `helixagent-debate` check C returned
`['PASS','FAIL','PASS']` in run 1 and `['PASS','ERROR','ERROR']` in run 3.

---

## 3. Self-validation — proof the harness can fail

`./scripts/validation/validate_helix_models.sh --self-test` stands up an
in-process mock server and asserts, for each fixture, the **exact** set of checks
that must fail. Verbatim output (`harness-evidence/self_validation.log`):

```
  [OK]   golden-GOOD  golden-good          failing checks == expected {} (none)
  [OK]   golden-BAD   bad-dead-pin         failing checks == expected ['A']
  [OK]   golden-BAD   bad-stub-identical   failing checks == expected ['B', 'C', 'D', 'E']
  [OK]   golden-BAD   bad-stub-varying     failing checks == expected ['B', 'D', 'E']
  [OK]   golden-BAD   bad-zero-usage       failing checks == expected ['B', 'D']
  [OK]   golden-BAD   bad-truncating       failing checks == expected ['D', 'E']
  [OK]   golden-BAD   bad-needle-dropped   failing checks == expected ['E']

Self-validation: PASS -- harness provably fails broken fixtures and passes the honest one
```

The assertion is **set equality**, not "at least one failure": a fixture failing
*more* checks than declared is treated as a bug too, because it means a check
fired for a reason the fixture did not model.

The self-test earned its keep — on first execution it rejected three of my own
fixture expectations. Two were genuinely wrong (a zero-usage stub really cannot
account context or retrieve a needle, so B/C/D/E all fail correctly); the third
exposed an unfaithful mock (`bad-truncating` returned a constant, tripping C,
unlike the real gateway which passes C). Both were corrected.

Self-validation runs automatically before every live run and **aborts the run**
if it fails: results from an unvalidated harness are worthless.

---

## 4. Live result matrix

Run 3, `2026-09-07T12:15:53+0200`, N=3, self-validation PASS.

| Model | A reach | B real | C prompt | D ctx | E needle | F sync | Verdict |
|---|---|---|---|---|---|---|---|
| `coder/qwen2.5-coder-3b-instruct-q4_k_m` | PASS | PASS | PASS | PASS | PASS | not run | **READY** |
| `helixagent/helixagent-debate` | PASS | **FAIL** | **NONDET** | **ERROR** | **ERROR** | not run | **NOT-READY** |
| `helixllm-gateway/*` (all models) | **FAIL** | UNTESTED | UNTESTED | UNTESTED | UNTESTED | not run | **NOT-READY** |
| `helixagent/helixagent-llm` | UNTESTED | UNTESTED | UNTESTED | UNTESTED | UNTESTED | UNTESTED | **UNTESTED** |
| `helixagent/helixagent-ensemble` | UNTESTED | UNTESTED | UNTESTED | UNTESTED | UNTESTED | UNTESTED | **UNTESTED** |
| `helixagent/helix-debate` | UNTESTED | UNTESTED | UNTESTED | UNTESTED | UNTESTED | UNTESTED | **UNTESTED** |
| `helixagent/helix-llm` | UNTESTED | UNTESTED | UNTESTED | UNTESTED | UNTESTED | UNTESTED | **UNTESTED** |

### 4.1 `coder/qwen2.5-coder-3b-instruct-q4_k_m` — READY (FACT)

The only route that passes everything, identically across all three runs.

- **B** `prompt_tokens=43 completion_tokens=2`, content 5 chars
- **C** `"Paris"` vs a 216-char Python function — distinct
- **D** `prompt_tokens` **786 → 6036 = 7.68×** for an 8.0× input increase
- **E** token retrieved at **all three depths** (`MIDKEY-QW3RM8-D25/D50/D75`, 6 079 prompt tokens)

### 4.2 `helixagent/helixagent-debate` — NOT-READY (FACT, defect 2 confirmed)

Reproduced in **all three runs**:

- **B FAIL** — `prompt_tokens=0, completion_tokens=0` beside HTTP 200. No LLM ran.
- **C NONDET** — canned status strings; verdict unstable across repeats.
- **D/E** — cannot be accounted (zero usage) and the buried token is never
  returned at any depth; replies observed: `"Comprehensive debate completed with
  3 rounds"`, `"All agent tasks failed during execution."`, `"No responses to
  analyze"`.

The content is a **pipeline status message, not an answer**.

### 4.3 `helixllm-gateway` — NOT-READY (FACT, and a *new* break)

The gateway now returns **HTTP 401** to model discovery and to completions:

```
GET /v1/models -> 401 {"error":{"message":"invalid credentials", ...}}
```

Reproducing exactly what the registered toolkit alias sends — the `api_key`
`helixllm-local-loopback` recorded in
`~/.claude-code-router/helixllm-anton-…/config.json`, as a `Bearer` token:

```
POST /v1/chat/completions  Authorization: Bearer <alias api_key>   ->  HTTP 401
```

**FACT:** the gateway alias is currently **unusable through Claude Toolkit**.

**Cause (FACT from source + process state):** `helixllm` was restarted at 12:09
with `HELIX_AUTH_JWT_SECRET` set. Per
`submodules/helix_llm/internal/gateway/middleware/auth.go:110-145`, a set JWT
secret with an empty API-key list means **only a JWT is accepted** — by design,
so that enabling JWT cannot silently leave the surface open. The static API key
the alias holds can no longer authenticate.

**HYPOTHESIS (not verified):** this is an in-flight change by the agent repairing
the gateway, and the alias credential simply has not been updated to match yet.
I did not attempt to mint a JWT from the process secret — forging a credential is
out of scope, and the point stands regardless: *what the alias actually sends is
rejected.*

### 4.4 Four HelixAgent models — UNTESTED, not passing and not condemned

`helixagent-llm`, `helixagent-ensemble`, `helix-debate`, `helix-llm` returned
`Connection refused` during probing.

**Cause (FACT):** a concurrent agent owns HelixAgent and is cycling it. Observed
in the process table:

```
bash build.sh … && systemctl --user restart helixagent.service && sleep 20 …
  && echo "--- MUTATED SERVICE PROBE ---" && curl … helixagent-debate
```

Availability sampled every 4 s for 48 s: `000` twelve times consecutively.
Three full harness runs were attempted (12:04, 12:14, 12:15); the service went
down mid-probe in two of them and was down for the whole of another.

These cells are **UNTESTED**. The service being stopped is an environment fact,
not evidence about the model: calling it FAIL would blame the model for another
agent's stopped process, and calling it PASS would be a bluff.

**Pre-restart measurements (FACT, superseded).** Before the 12:09 rebuild I
measured `helixagent-llm` directly: scaling **776 → 6 026 = 7.77×** (honest) and
the mid-depth needle retrieved. That was a **different build** than the one now
being deployed and is recorded as history, **not** as a current verdict.

### 4.5 Check F — NOT RUN, by deliberate design

`claude-providers sync` **mutates shared provider configuration** that other
agents are actively repairing; running it unbidden could clobber in-flight work.
F is implemented and gated behind `--with-sync`. It is reported as **SKIPPED**,
never as passing. Run it when you own that config:

```bash
./scripts/validation/validate_helix_models.sh --with-sync
```

---

## 5. Status of the four target defects

| # | Defect | Status this run |
|---|---|---|
| 1 | Gateway silent prompt truncation | **Reproduced (1.22× vs 7.55× control) at 12:02.** Not re-measurable at 12:15 — the endpoint now 401s before a prompt can be sent. Truncation is **unconfirmed as fixed**; it is merely unreachable. |
| 2 | Stub models with zero usage | **Reproduced and still present** on `helixagent-debate` in all three runs. |
| 3 | Dead pins (`helixllm-multi`) | **Not observed in its original form** — no alias now points at `helixllm-multi`. Replaced by a *new* break at the same place: the gateway alias 401s (§4.3). |
| 4 | Non-durable config (`sync` vs `export --apply`) | **NOT TESTED** — gated (§4.5). |

---

## 6. Reproducing

```bash
# prove the harness can fail before trusting any result
./scripts/validation/validate_helix_models.sh --self-test

# validate the live system (self-test runs first and aborts on failure)
./scripts/validation/validate_helix_models.sh --json matrix.json
```

Exit code is **0 only if** every discovered model passed every applicable check
**and** self-validation passed. Any FAIL / ERROR / NON-DETERMINISTIC / UNTESTED
verdict, or any endpoint discovery failure, yields **1**.

Environment knobs: `HELIX_AGENT_BASE`, `HELIX_LLM_BASE`, `HELIX_CODER_BASE`,
`HELIX_API_KEY` / `HELIX_<ENDPOINT>_KEY`, `HELIX_SYNC_CMD`.
Model ids are **never** hardcoded — they are discovered from `/v1/models` at run
time (CONST-036). No credential is committed; endpoint tokens are resolved at run
time from the environment or the local provider records (CONST-042).

---

## 7. Honest boundaries

- **1 of 7 models is validated as working.** 4 are UNTESTED because their
  endpoint was unavailable — that is *absence of evidence*, not evidence of
  health, and must not be read as a partial pass.
- **A green run of this harness does not certify the whole stack.** It certifies
  the six properties in §2 for the models it could reach.
- **Check F has never been executed.** Alias durability across `sync` remains
  entirely unverified.
- **The gateway's truncation defect is not proven fixed.** It is currently
  *masked* by a 401. When auth is resolved, re-run before drawing any conclusion.
- **The results are a snapshot of a moving system.** Services were restarted by
  other agents *during* measurement; three runs were needed and none captured a
  fully stable system. Re-run once the concurrent repairs settle.

## Sources verified 2026-09-07

- `submodules/helix_llm/internal/gateway/middleware/auth.go` (read-only) — JWT/API-key mode matrix
- `~/.claude-code-router/*/config.json` — registered alias base URLs and keys
- `/proc/<pid>/environ`, `/proc/<pid>/cmdline` — running service configuration
- Live HTTP probes to `127.0.0.1:7061`, `127.0.0.1:8443`, `127.0.0.1:18434`
