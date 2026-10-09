# Ephemeral-port churn audit — which unit packages generate the socket flood

| Field | Value |
|---|---|
| Revision | 1 |
| Created | 2026-09-07 |
| Last modified | 2026-09-07 |
| Status | active |
| Scope | `helix_code/` (Go module `dev.helix.code`) — `internal/**`, `tests/**`, `cmd/**`, `applications/**` |
| Method | **STATIC ONLY.** No test run, no build, no port bound. `grep`, file reads, `git`, and Python source scanners under the session scratchpad. |
| Evidence-class labelling | Every number below is tagged **FACT** (counted from source) or **ESTIMATE** (extrapolated). Per §11.4.6, an estimate is never presented as a measurement. |

---

## Table of contents

- [0. Executive finding (read this first)](#0-executive-finding-read-this-first)
- [1. Method and cost model](#1-method-and-cost-model)
- [2. Ranked table — top 20 packages by estimated port cost](#2-ranked-table--top-20-packages-by-estimated-port-cost)
- [3. Multiplier sites — servers constructed inside loops](#3-multiplier-sites--servers-constructed-inside-loops)
- [4. Multiplier sites — helper factories (the larger multiplier)](#4-multiplier-sites--helper-factories-the-larger-multiplier)
- [5. Request-side churn outside httptest](#5-request-side-churn-outside-httptest)
- [6. Undrained / unclosed response bodies](#6-undrained--unclosed-response-bodies)
- [7. Servers never Close()d](#7-servers-never-closed)
- [8. Per-offender recommended fix](#8-per-offender-recommended-fix)
- [9. Total-port estimate, arithmetic shown](#9-total-port-estimate-arithmetic-shown)
- [10. Honest boundary — what this audit does NOT explain](#10-honest-boundary--what-this-audit-does-not-explain)
  - [10.1 The ESTABLISHED peak constrains the hypothesis space](#101-the-established-peak-constrains-the-hypothesis-space)
  - [10.2 The decisive measurement](#102-the-decisive-measurement)
- [11. Prior art already in this codebase](#11-prior-art-already-in-this-codebase)

---

## 0. Executive finding (read this first)

**FACT:** the module contains **452 `httptest.New{,TLS,Unstarted}Server` construction
sites** (comment lines excluded). Resolving helper factories and loop bodies to their
runtime instantiation counts gives **≈673 server instances per full suite run
(ESTIMATE)**.

**FACT:** only **16 of the 452 sites sit inside a `for` loop**. Loops are *not* the
dominant multiplier in this codebase. The dominant multiplier is **helper factories** —
a single `func …() *httptest.Server` called from 10–34 separate test functions
(§4). Five such factories alone account for **102 server instances from 5 source
lines**.

**HONEST BOUNDARY (§11.4.6):** the counted `httptest` inventory, costed generously,
accounts for roughly **2,000–3,700 ephemeral ports per suite run (ESTIMATE)** — about
**7–13 % of the 28,232-port range**. It **cannot by itself produce the measured 100.0 %
occupancy**. Something outside this static inventory contributes the remaining ~90 %.
§10 enumerates the candidates and names the one measurement that would settle it.
Presenting this inventory as "the cause" would be a §11.4.6 violation, so I do not.

---

## 1. Method and cost model

### 1.1 What was counted

Four scanners were written to the session scratchpad and run over the tree:

| Scanner | What it does |
|---|---|
| `churn_audit.py` | Brace-depth scan with string/comment stripping; attributes every `httptest.New*Server` site to its enclosing `for` / `t.Run` / `go func` blocks. |
| `req_audit.py` | Finds `http.Get/Post/Head/PostForm` and `*.Do(` sites; checks the following 10 lines for a drain of the named response variable. |
| `close_audit.py` | For each `x := httptest.New…`, searches the following 80 lines for `defer x.Close()` / `t.Cleanup(x.Close)` / `x.Close()`. |
| `factory_audit.py` | Attributes each server site to its enclosing `func`; if that func is **not** `Test*`/`Benchmark*`, multiplies by its call-site count inside the same package. |

### 1.2 Port-cost model (stated so the arithmetic is auditable)

Per the brief's established mechanics, one server + one exchange pins **two** ports
drawn from `32768-60999`:

```
cost(one server instance) = 1   listen port  (httptest binds 127.0.0.1:0 — an
                                              ephemeral-range port; Server.Close()
                                              makes the server the active closer, so
                                              that port is held 60 s in TIME_WAIT)
                          + R   client-connect ports (one per round trip that could
                                              NOT reuse a pooled connection)
```

`R` is the only free parameter. Baseline used below: **R = 2** (a typical provider test
issues a models/health call plus one generate call). Sensitivity band: **R ∈ [1, 4]**.
`R` is an ESTIMATE — it is not statically countable, because virtually every test issues
its HTTP through a provider method (`provider.Generate(...)`), not through a
grep-visible `client.Do(`.

### 1.3 Scope correction — which suites are actually in the unit pass

**FACT (verified 2026-09-07 against the working tree):** the four load-generating
suites *are* build-tagged out. The tag sits at **line 23**, below a licence header, so a
shallow `head -6` check misses it:

```
tests/ddos/*_test.go          //go:build integration || loadtest
tests/performance/*_test.go   //go:build loadtest
tests/scaling/*_test.go       //go:build loadtest
tests/stresschaos/*_test.go   //go:build loadtest
```

`tests/integration/**` carries `//go:build integration`. All of these are therefore
**excluded** from a plain `./...` unit pass — consistent with the brief's established
fact that the flood persisted after they were tagged out.

**FACT — but these are NOT tagged and DO compile into the unit pass:**
`tests/memory`, `tests/security`, `tests/e2e/**`, `tests/automation`, `tests/testinfra`,
`tests/regression`, `tests/unit`, `tests/ui`, `tests/ux`, `tests/qa`.

**FACT — however, they self-skip:** `tests/memory/memory_test.go` and
`tests/security/owasp_test.go` both gate on `HELIXCODE_TEST_URL` reachability and call
`t.Skip("Server not available…")` (7 and 3 skip sites respectively). With no server
running they contribute ~0 ports. `tests/automation/hardware_test.go` gates on
`RUN_REAL_EXECUTION` / `testing.Short()`. This is a **negative finding**: these untagged
suites are *not* a hidden flood source in the default configuration.

---

## 2. Ranked table — top 20 packages by estimated port cost

Columns:
- **sites** — FACT, static `httptest.New*Server` construction lines (comments excluded).
- **in loop** — FACT, subset of `sites` whose enclosing block is a real `for` loop.
- **instances** — ESTIMATE, runtime server constructions after helper-factory
  multiplication (§4) and loop multiplication (§3).
- **est. ports** — ESTIMATE, `instances × (1 + R)` at R = 2.
- **tag** — FACT, whether the package is in a plain `./...` unit pass.

| # | Package | sites | in loop | instances | est. ports (R=2) | in unit pass |
|---:|---|---:|---:|---:|---:|:--:|
| 1 | `internal/llm` | 218 | 7 | **298** | **894** | yes |
| 2 | `internal/memory/providers` | 47 | 0 | **103** | **309** | yes |
| 3 | `internal/cognee` | 33 | 1 | 37 | 111 | yes |
| 4 | `internal/verifier` | 29 | 0 | 30 | 90 | yes |
| 5 | `internal/server` | 13 | 0 | 26 | 78 | yes |
| 6 | `internal/notification` | 17 | 4 | 33 | 99 | yes |
| 7 | `internal/notification/testutil` | 3 | 0 | 18 | **61** ¹ | yes |
| 8 | `internal/llm/providers/helixagent` | 13 | 0 | 17 | 51 | yes |
| 9 | `internal/mcp` | 9 | 0 | 13 | 39 | yes |
| 10 | `internal/discovery` | 10 | 1 | 18 | 54 ² | yes |
| 11 | `internal/tools/web` | 9 | 1 | 11 | 33 | yes |
| 12 | `tests/integration` | 1 | 0 | 6 | 18 | **no** (`integration`) |
| 13 | `internal/providers/httpclient` | 1 | 0 | 5 | 15 | yes |
| 14 | `internal/context/mentions` | 5 | 0 | 5 | 15 | yes |
| 15 | `tests/ddos` | 5 | 0 | 5 | 15 | **no** (`loadtest`) |
| 16 | `internal/llm/routing` | 1 | 0 | 4 | 12 | yes |
| 17 | `internal/llm/providers/cerebras` | 4 | 0 | 4 | 12 | yes |
| 18 | `internal/llm/providers/huggingface` | 4 | 1 | 6 | 18 | yes |
| 19 | `internal/llm/providers/together` | 4 | 1 | 5 | 15 | yes |
| 20 | `internal/rag` / `…/replicate` / `…/cohere` | 3 ea | 0 | 3 ea | 9 ea | yes |
| — | all remaining packages (14 pkgs, ≤2 sites each) | 18 | 0 | 20 | 60 | mixed |
| | **TOTAL** | **452** | **16** | **≈673** | **≈2,020** | |

¹ `internal/notification/testutil` is costed differently: 18 server instances **plus 43
undrained-body requests** (§6), each of which forfeits connection reuse and therefore
burns its own port → `18 + 43 = 61`.

² `internal/discovery` carries an *additional*, time-dependent cost not in this column:
health monitors with `CheckInterval` of 50–100 ms (`health_monitor_test.go:461,513`;
`registry_test.go:446`; `broadcast_test.go:92,149,223,298,301,411`). Connection count
there is a function of wall-clock, not of source, so it is **not statically countable**.

**`internal/llm` and `internal/memory/providers` together are 59 % of the estimated
cost.** Any remediation that does not start there is misdirected.

---

## 3. Multiplier sites — servers constructed inside loops

**FACT.** All 16 sites, with the literal iteration count read from the source table.
`servers/iter` is the constructions the loop performs.

| file:line | loop at | iterations | servers/iter | total |
|---|---|---:|---:|---:|
| `internal/llm/tool_protocol_test.go:373` **and** `:390` | `:369 for _, tc := range cases` | 5 | 2 | **10** |
| `internal/discovery/registry_health_test.go:136` | `:133 for _, tt := range tests` | 8 | 1 | **8** |
| `internal/notification/discord_test.go:145` | `:140 for _, tt := range tests` | 7 | 1 | **7** |
| `internal/llm/vertexai_provider_test.go:605` | `:603 for _, tt := range tests` | 6 | 1 | **6** |
| `internal/llm/groq_provider_test.go:493` | `:491 for _, tt := range tests` | 6 | 1 | **6** |
| `internal/notification/webhook_test.go:239` | `:237 for _, tt := range tests` | 5 | 1 | **5** |
| `internal/cognee/client_stresschaos_test.go:267` | `:265 for _, f := range faults` | 5 | 1 | **5** |
| `internal/llm/anthropic_provider_test.go:532` | `:530 for _, tt := range tests` | 4 | 1 | **4** |
| `internal/llm/gemini_provider_test.go:543` | `:541 for _, tt := range tests` | 4 | 1 | **4** |
| `internal/notification/telegram_test.go:143` | `:140 for _, tt := range tests` | 4 | 1 | **4** |
| `internal/notification/slack_test.go:123` | `:120 for _, tt := range tests` | 4 | 1 | **4** |
| `internal/llm/qwen_provider_test.go:225` | `:223 for _, tt := range tests` | 3 | 1 | **3** |
| `internal/llm/providers/huggingface/client_test.go:74` | `:62 for _, tt := range tests` | 3 | 1 | **3** |
| `internal/tools/web/web_test.go:424` | `:422 for i := range servers` (`make([]*httptest.Server, 3)`) | 3 | 1 | **3** |
| `internal/llm/providers/together/client_test.go:77` | `:66 for _, tt := range tests` | 2 | 1 | **2** |
| | | | **sum** | **74** |

Iteration counts were derived two ways and cross-checked: counting gofmt element
openers (`^\s+\{$`) and counting `^\s+name:` fields between the slice declaration and
the `for`. Both agreed on all 12 sites where both applied; the remaining 3
(`webhook_test.go` positional literal, `client_stresschaos_test.go` `faults`,
`web_test.go` `make(…, 3)`) were read directly from source.

**Net effect: 16 source lines produce 74 server instances — 58 more than a naive
site count.** Real, but modest: **9 % of the estimated total**.

---

## 4. Multiplier sites — helper factories (the larger multiplier)

**FACT.** A `func` that is *not* `Test*`/`Benchmark*` and constructs a server runs once
per call site. These are the true multipliers in this codebase, and a per-line grep
completely misses them.

| calls | definition site | function | package |
|---:|---|---|---|
| **34** | `internal/memory/providers/chromadb_provider_test.go:931` | `createMockChromaServer()` | `internal/memory/providers` |
| **24** | `internal/memory/providers/baseai_provider_test.go:978` | `createMockBaseAIServer()` | `internal/memory/providers` |
| **18** | `internal/llm/koboldai_provider_test.go:16` | `setupKoboldAITestServer()` | `internal/llm` |
| **16** | `internal/llm/openai_compatible_provider_test.go:17` | `setupOpenAICompatibleTestServer()` | `internal/llm` |
| **10** | `internal/llm/copilot_provider_test.go:18` | `setupCopilotTestServer()` | `internal/llm` |
| 8 | `internal/server/server_chaos_test.go:149` | `newRealServerHarness()` | `internal/server` |
| 6 | `internal/notification/testutil/mock_servers.go:30` | `NewMockSlackServer()` | `internal/notification/testutil` |
| 6 | `internal/notification/testutil/mock_servers.go:91` | `NewMockTelegramServer()` | `internal/notification/testutil` |
| 6 | `internal/notification/testutil/mock_servers.go:162` | `NewMockDiscordServer()` | `internal/notification/testutil` |
| 6 | `tests/integration/browser_test.go:67` | `newBrowserFixtureServer()` | `tests/integration` (tagged out) |
| 5 | `internal/llm/bench_test.go:37` | `newBenchOpenAIProvider()` | `internal/llm` |
| 5 | `internal/llm/providers/helixagent/helixagent_test.go:114` | `newFakeHelixAgent()` | `…/helixagent` |
| 5 | `internal/providers/httpclient/httpclient_test.go:99` | `newConnCountingServer()` | `internal/providers/httpclient` |
| 4 | `internal/llm/provider_tools_test.go:98` | `toolTestServer()` | `internal/llm` |
| 4 | `internal/llm/routing/integration_test.go:56` | `newProviderShim()` | `internal/llm/routing` |
| 4 | `internal/server/llm_generate_toolcall_replay_test.go:113` | `newToolCallReplayBackend()` | `internal/server` |
| 3 | `applications/terminal_ui/env_providers_dynamic_test.go:30` | `newFakeVerifier()` | `applications/terminal_ui` |
| 3 | `internal/verifier/working_models_test.go:33` | `newWorkingModelsServer()` | `internal/verifier` |
| 3 | `internal/server/llm_working_funnel_test.go:40` | `newFunnelCatalogServer()` | `internal/server` |

**The top five lines in this table produce 102 server instances.** That is 1.4× the
entire loop-multiplier contribution (§3) from one thirtieth of the source lines.

---

## 5. Request-side churn outside `httptest`

**FACT.** 396 sites match `http.Get/Post/Head/PostForm` or `*.Do(` across the module;
32 are inside a `for` loop. After discarding non-HTTP `.Do(` false positives
(`fyneui.Do(`, `sync.Once.Do(`), the in-loop HTTP set that runs in the unit pass is
small and enumerable:

| file:line | loop bound | iterations | drains body? |
|---|---|---:|:--:|
| `internal/notification/testutil/mock_servers_test.go:92` | `:86 for i := 0; i < 5` | 5 | **no** |
| `internal/notification/testutil/mock_servers_test.go:181` | `:178 for i := 1; i <= 3` | 3 | partial |
| `internal/notification/testutil/mock_servers_test.go:262` | `:259 for i := 0; i < 5` | 5 | **no** |
| `internal/notification/testutil/mock_servers_test.go:282` | `:278 for i := 0; i < 10` | 10 | **no** |
| `internal/notification/testutil/mock_servers_test.go:304` | `:300 for i := 0; i < 10` | 10 | **no** |
| `internal/notification/testutil/mock_servers_test.go:325` | `:321 for i := 0; i < 10` | 10 | **no** |
| `internal/server/server_chaos_test.go:397` | `:394 for i := 0; i < 50` | **50** | yes |
| `internal/server/server_chaos_test.go:227` | `:205 for _, hc := range cases` | table | yes |
| `internal/verifier/embedded_server.go:85` / `server_test.go:99` | `for {}` readiness poll | until ready | yes |
| `tests/e2e/phase3/production_validation_test.go:216` | `:211 concurrentRequests` | var | — |
| `tests/memory/*`, `tests/security/*` | large | large | — (self-skips, §1.3) |

### 5.1 `net.Listen` / `net.Dial` sites

**FACT.** 33 `net.Listen(` sites in `_test.go`. None is a loop-driven free-port
scanner. Most are single `net.Listen("tcp6", "[::1]:0")` probes in the HXC-185
IPv6-address regression guards (one per package, 9 packages). `internal/discovery`
holds 5 (`health_monitor_test.go:88,466,519,571`, `client_test.go:380`,
`registry_health_test.go:251,324,454`). **These are not a flood source.**

### 5.2 Live-service targets — the one high-volume non-`httptest` source found

**FACT.** `internal/llm/ollama_provider_stress_test.go` targets a **live Ollama at
`http://localhost:11434`** (`:39 defaultOllamaTestURL`) and runs:

- `:190 ConcurrencyConfig{Parallelism: 16, IterationsPerGoroutine: 25}` → **400 real HTTP calls**
- `:231 ConcurrencyConfig{Parallelism: 10, IterationsPerGoroutine: 1}` → 10 real generations

**FACT.** `internal/llm/ollama_provider.go:180` sets `MaxIdleConnsPerHost: 2`. At
`Parallelism: 16`, at most 2 of 16 concurrent connections can be pooled; the other ~14
are discarded after each round. **ESTIMATE:** ~350 of the 400 calls open a fresh TCP
connection, each burning a client-side ephemeral port, delivered as a burst.

The test honestly skips when Ollama is unreachable (`:70`, `:76` — `SKIP-OK … §11.4.3`).
**But `make test-infra-up` starts Ollama**, so under `test-unit-full` / `test-full` this
path is live. This is the single largest *non-`httptest`* churn source the static audit
found, and it is invisible to any `httptest` grep.

---

## 6. Undrained / unclosed response bodies

A response body left undrained forfeits connection reuse: Go's transport will not
return a connection to the pool with unread bytes on it, so each such request burns a
fresh port instead of reusing one. **This converts what should be 1 connection into N.**

**FACT — 27 sites in test code that imports `net/http`.** Ranked by package:

### 6.1 `internal/notification/testutil` — 16 sites, the worst offender

`internal/notification/testutil/mock_servers_test.go`:

| line | in loop | iterations | shape |
|---|:--:|---:|---|
| `:26` | — | 1 | `resp, err := http.Post(...)` — status asserted, body never read/closed |
| `:46` | — | 1 | `http.Post(...)` — **return value discarded entirely** |
| `:60` | — | 1 | `resp, err := http.Get(...)` |
| `:73` | — | 1 | `resp, err := http.Post(...)` |
| `:92` | **yes** `:86` | **5** | `http.Post(...)` — return discarded |
| `:138` | — | 1 | `http.Post(...)` — return discarded |
| `:152` | — | 1 | `resp, err := http.Get(...)` |
| `:165` | — | 1 | `resp, err := http.Post(...)` |
| `:202` | — | 1 | `resp, err := http.Post(...)` |
| `:219` | — | 1 | `http.Post(...)` — return discarded |
| `:233` | — | 1 | `resp, err := http.Get(...)` |
| `:246` | — | 1 | `resp, err := http.Post(...)` |
| `:262` | **yes** `:259` | **5** | `http.Post(...)` — return discarded |
| `:282` | **yes** `:278` | **10** | `http.Post(...)` — return discarded |
| `:304` | **yes** `:300` | **10** | `http.Post(...)` — return discarded |
| `:325` | **yes** `:321` | **10** | `http.Post(...)` — return discarded |

**Total requests: 43 (FACT).** All go through `http.DefaultClient` →
`http.DefaultTransport` (a shared pool that *would* reuse), but every one forfeits reuse
by not draining. **43 requests → ~43 connections instead of ~3.**

### 6.2 `internal/llm` — 5 confirmed sites

`internal/llm/transport_redaction_scan_test.go:387, 413, 422, 435, 447` —
`resp, err := c.Do(req)` / `_, err := c.Do(req)` with no drain in the following 10
lines. Low volume (5 requests) but the same class.

`internal/llm/copilot_provider_test.go:485` — `_, err := provider.httpClient.Do(req)`,
response discarded.

*(Excluded as a false positive: `internal/llm/provider_live_proof_test.go:211`
`providerLiveRunIDOnce.Do(...)` is a `sync.Once`, not an HTTP call.)*

### 6.3 Pass-through helpers — verify the caller, not the helper

`tests/testinfra/testinfra.go:549, 562, 574` and
`tests/integration/integration_test.go:184` all end `return c.client.Do(req)` — the
response is handed to the caller. These are only defects if a caller drops it. Both
packages are outside the default unit pass (`testinfra` is a library; `tests/integration`
is `integration`-tagged). **Flagged for follow-up, not counted as offenders.**

---

## 7. Servers never `Close()`d

**FACT — 8 sites with no `Close()` within 80 lines of construction.** A server never
closed holds its listen port for the lifetime of the test binary rather than releasing
it after 60 s.

| file:line | variable | assessment |
|---|---|---|
| `internal/notification/testutil/mock_servers.go:30` | `mock.Server` | **benign** — constructor; the type exposes `Close()` and callers `defer server.Close()` (verified in `mock_servers_test.go:70,83,102`). |
| `internal/notification/testutil/mock_servers.go:91` | `mock.Server` | benign, same pattern |
| `internal/notification/testutil/mock_servers.go:162` | `mock.Server` | benign, same pattern |
| `internal/llm/tool_calling_concurrent_replay_test.go:134` | `b.Server` | **needs review** — harness field; confirm the harness's cleanup closes it |
| `internal/server/llm_generate_toolcall_replay_test.go:113` | `b.Server` | **needs review** — ×4 call sites (§4), so 4 leaked listeners if uncleaned |
| `internal/server/llm_generate_llamacpp_local_test.go:160` | `s.Server` | **needs review** |
| `internal/providers/httpclient/httpclient_test.go:99` | `srv` | `NewUnstartedServer` helper, ×5 call sites — **needs review** |
| `tests/e2e/e2e_test_framework.go:43` | `server` | framework helper — **needs review** |

**This is a small, well-contained list.** Unclosed servers are *not* a material driver
here (≤ ~15 ports), which is consistent with the brief's measured finding that LISTEN
count stayed flat at 118–122 while TIME-WAIT peaked at 29,625 — the ports are being
*cycled*, not *leaked*.

---

## 8. Per-offender recommended fix

Fix classes, per the brief: **(a)** share one server across a subtest table;
**(b)** drain+close bodies; **(c)** tune `MaxIdleConnsPerHost` on a shared client;
**(d)** replace the round trip with `httptest.NewRecorder` + `handler.ServeHTTP`.

| Rank | Offender | Est. ports saved | Fix |
|---:|---|---:|---|
| 1 | `internal/memory/providers/chromadb_provider_test.go:931` `createMockChromaServer()` — **34 call sites** | ~99 | **(a)** The handler is stateless per call site. Hoist to one package-level server started in `TestMain` (or a `sync.Once` + `t.Cleanup` on the last user) and hand every test the shared `srv.URL`. 34 instances → 1. Where a test needs a *distinct* canned response, key the shared handler on a request path or header instead of standing up a new listener. |
| 2 | `internal/memory/providers/baseai_provider_test.go:978` `createMockBaseAIServer()` — **24 call sites** | ~69 | **(a)** Identical shape to #1, same fix. Do both in one change — same package, same idiom. |
| 3 | `internal/llm/koboldai_provider_test.go:16` `setupKoboldAITestServer()` — **18 call sites** | ~51 | **(a)** One shared server per package, path-multiplexed. Additionally **(c)**: `internal/llm/koboldai_provider.go:357` calls `httpClient.CloseIdleConnections()` on provider close — correct hygiene, but with a new provider per test the pool never warms, so every test pays a fresh connect. Building the provider once against the shared server removes both costs. |
| 4 | `internal/llm/openai_compatible_provider_test.go:17` `setupOpenAICompatibleTestServer()` — **16 call sites** | ~45 | **(a)** Same. Note `internal/llm/openai_compatible_provider.go` constructs its own `&http.Transport{}` (private pool) — a per-test provider means a per-test pool, so the shared-server fix must be paired with a shared provider to actually collapse connections. |
| 5 | `internal/notification/testutil/mock_servers_test.go` — 43 undrained requests, 16 sites (§6.1) | ~40 | **(b)** — highest value-per-line in the audit. Every site becomes:<br>`resp, err := http.Post(...)`<br>`require.NoError(t, err)`<br>`io.Copy(io.Discard, resp.Body); resp.Body.Close()`<br>The six discard-the-return sites (`:46, :92, :138, :219, :262, :282, :304, :325`) must capture and drain rather than ignore. With draining, `http.DefaultTransport` reuses the connection and 43 requests collapse to ~3. |
| 6 | `internal/llm/copilot_provider_test.go:18` `setupCopilotTestServer()` — **10 call sites** | ~27 | **(a)** Same as #3. |
| 7 | `internal/llm/tool_protocol_test.go:373` **and** `:390` — 2 servers inside a 5-case loop | ~24 | **(a)** Hoist **both** servers above `:369 for _, tc := range cases`. `twoTurnHandler(t, &turn2Body)` already takes a per-case pointer, so parameterise the handler by a case field read at request time instead of rebuilding the listener. 10 instances → 2. |
| 8 | `internal/server/server_chaos_test.go:149` `newRealServerHarness()` — 8 call sites, wraps the **real** `srv.router` | ~24 | **(d)** where the assertion is about handler behaviour rather than transport: `httptest.NewRecorder()` + `srv.router.ServeHTTP(rec, req)` needs **zero** ports. Keep the real listener only for the tests that genuinely assert transport-level behaviour (timeouts, connection reuse, TLS). The file's own doc comment at `:8` claims "real in-process HTTP listener … No mocked" — so this one needs an explicit §11.4.120 reconciliation of that claim before conversion, not a silent swap. |
| 9 | `internal/discovery/registry_health_test.go:136` — 8-case loop | ~24 | **(a)** Hoist the server above `:133`; select the canned response from `tt` inside the handler closure. |
| 10 | `internal/notification/discord_test.go:145` (7), `webhook_test.go:239` (5), `telegram_test.go:143` (4), `slack_test.go:123` (4) | ~60 | **(a)** All four are the identical shape: `for _, tt := range tests { t.Run(..., func(t *testing.T) { server := httptest.NewServer(...) ... }) }`. One shared server per test function, handler reading the current case from a captured pointer or a request header. 20 instances → 4. |
| 11 | `internal/llm/{vertexai:605, groq:493, anthropic:532, gemini:543, qwen:225}` — table loops | ~48 | **(a)** Same pattern; `anthropic_provider_test.go:532` is the canonical example (4 error-status cases, each standing up a listener only to return `tt.statusCode` + `tt.responseBody`). Hoist and switch on a header. |
| 12 | `internal/llm/ollama_provider_stress_test.go:190` — 400 live calls at `Parallelism: 16` against `MaxIdleConnsPerHost: 2` (§5.2) | ~350 | **(c)** — but note the production default is deliberate. `ollama_provider.go:174-184` documents the choice. Do **not** change production tuning to make a test cheaper (that would invert §11.4.1 fix-at-source). Instead, either (i) set a test-local transport with `MaxIdleConnsPerHost: 16` matching the test's own parallelism, exactly as `internal/cognee/client_stresschaos_test.go:89` already does, or (ii) gate this test behind the `loadtest` tag alongside its siblings — it drives a live external service and is a load test in everything but its tag. |
| 13 | `internal/llm/transport_redaction_scan_test.go:387,413,422,435,447` | ~5 | **(b)** Add `defer resp.Body.Close()` + `io.Copy(io.Discard, resp.Body)`; for the `_, err :=` forms, capture the response first. |
| 14 | `internal/tools/web/web_test.go:422-424` — `make([]*httptest.Server, 3)` | ~9 | Keep. The test is *about* multiple distinct endpoints; 3 servers is the minimum that expresses it. **(c)** applies instead: `internal/tools/web/web.go:101-102` already sets `MaxIdleConns: 100, MaxIdleConnsPerHost: 10`, so reuse is available if bodies are drained. |
| 15 | The 5 `needs review` unclosed servers (§7) | ~15 | Add `t.Cleanup(func(){ b.Server.Close() })` at each harness constructor. Cheap, and it converts a binary-lifetime hold into a 60-second one. |

**If items 1–11 alone are done, the estimated `httptest` server-instance count drops
from ≈673 to ≈380 (ESTIMATE) — a ~44 % reduction in the `httptest` share of the churn.**

---

## 9. Total-port estimate, arithmetic shown

### 9.1 Server instances (ESTIMATE, derived from FACTs)

```
static construction sites (comments excluded)                       452   FACT
  minus sites that live inside a for loop                           -16   FACT
  plus  loop-multiplied instances (§3 sum)                          +74   FACT (literal table sizes)
  = sites-once + loop instances                                     510
  plus helper-factory multiplication (§4; e.g. 1 line x 34 calls)   +163   ESTIMATE
                                                                   -----
  total runtime server instances per full-tree run                  673   ESTIMATE
```

*(The factory scanner independently reported 615 instances counting factories but
treating loop bodies as single; 615 + 58 loop extras = 673. The two paths agree.)*

### 9.2 Excluding suites not in the unit pass

```
  total instances                                                   673
  minus tests/ddos            (loadtest tag)                         -5   FACT
  minus tests/integration     (integration tag)                      -6   FACT
  minus tests/stresschaos     (loadtest tag)                         -1   FACT
  minus tests/performance     (loadtest tag)                         -1   FACT
                                                                   -----
  server instances in a plain ./... unit pass                       660   ESTIMATE
```

### 9.3 Ports

```
  ports = instances x (1 listen port + R client ports)

  R = 1  (single round trip)      660 x 2 = 1,320    ESTIMATE — lower bound
  R = 2  (baseline)               660 x 3 = 1,980    ESTIMATE — central
  R = 4  (models + generate + 2)  660 x 5 = 3,300    ESTIMATE — upper bound

  plus §6.1 undrained-body excess  43 requests - ~3 reused        +40   FACT-derived
  plus §5.2 live-Ollama burst      ~350 non-pooled connections   +350   ESTIMATE, only
                                                                        when Ollama is up
                                                                 -----
  central estimate, Ollama down                            ~2,020 ports
  central estimate, Ollama up                              ~2,370 ports
  upper bound,      Ollama up                              ~3,690 ports
```

### 9.4 Against the 28,232-port range

```
  2,020 / 28,232 =  7.2 %
  3,690 / 28,232 = 13.1 %
```

### 9.5 Uncertainty statement (§11.4.6)

The **452 site count, the 16 loop sites, the 74 loop instances, the 43 undrained
requests, the 8 unclosed servers, and every build tag** are FACTs read from source and
independently cross-checked.

**`R` is the dominant uncertainty and it is unbounded from source.** Almost every test
issues HTTP through a provider method, so round trips are invisible to a grep. If the
true average `R` were ~40 rather than ~2, the httptest inventory alone would reach
28,232. I have no static evidence for or against that, and I will not assert either.

The factory multiplier is also approximate: `factory_audit.py` counts textual
`funcName(` occurrences within the package, which over-counts if a factory is called
inside a skipped test and under-counts cross-package helpers. Spot-checks on the five
largest (34/24/18/16/10) matched hand-verified `grep -c` counts.

---

## 10. Honest boundary — what this audit does NOT explain

**The counted inventory reaches ~7–13 % of the range. The measurement showed 100.0 %.
This audit does not close that gap, and I am not going to dress up an estimate to make
it look like it does.**

The brief's own arithmetic is the constraint: sustaining 28,232 simultaneous TIME_WAIT
sockets requires **~470 socket close-cycles per second held for a full 60 s**. A
660-server unit pass at R≈2, spread across a multi-minute run, averages **far** below
that. Four candidate explanations remain, in descending order of my confidence:

1. **`R` is much larger than 2 for the provider tests.** `internal/llm` alone holds 218
   sites; if provider tests average tens of round trips per server (retry paths —
   `factory.go:280 MaxRetries: 3`, `xiaomi_provider.go:167 MaxRetries: 3`,
   `replicate_provider.go:310` polling `replicateMaxPollCycles`,
   `providers/replicate/client.go:126 for i := 0; i < 30`, streaming responses,
   `response_err_round*_test.go` error-status suites that trigger retry), the
   arithmetic closes. **This is the first thing to measure.**
2. **The measured run was not a plain unit pass.** `make test-unit-full` /
   `test-full` bring up infra and add `-tags=integration`, which re-admits
   `tests/integration` (6 instances + `newBrowserFixtureServer` ×6 + chromedp) and the
   `-tags=integration` stress targets at `Makefile:166`
   (`./internal/redis/... ./internal/database/... ./internal/server/... ./internal/llm/...`).
3. **Parallel package execution compresses the whole run into one 60 s window.**
   Go runs packages concurrently up to `GOMAXPROCS`. 660 servers spread over 5 minutes
   is ~2 closes/sec; the same 660 compressed into overlapping 60 s windows across ~30
   parallel packages is a different curve entirely — and TIME_WAIT is a *stock*, not a
   flow, so occupancy is `rate × 60 s`.
4. **A source outside this static scope** — the live-Ollama path (§5.2), the
   `internal/discovery` millisecond-interval health monitors whose connection count is
   wall-clock-dependent, or infra client churn.

### 10.1 The ESTABLISHED peak constrains the hypothesis space

The sibling measurement record (`ANALYSIS.md`, sample `09:43:15`) shows **11,010
ESTABLISHED** sockets at peak alongside 20,317 TIME-WAIT. On loopback both endpoints are
local sockets, so that is **~5,500 genuinely concurrent connections**.

This is load-bearing, and it **argues against** the shape this audit inventoried. 660
short-lived `httptest` servers, each serving a couple of sequential round trips, would
produce a *low* ESTAB count and a *high* TIME-WAIT count — connections open and close
one after another. Five thousand simultaneously-open connections is a different
signature: something is holding thousands of sockets **at once**.

Static search found **no single test that opens ≥500 concurrent connections** (checked:
`wg.Add(1)` inside loops with bounds ≥500, literals ≥1000 near conn/http/dial, and the
`port_allocator` tests named in the failure list — `TestAllocatePort_RangeExhausted_WithEphemeral`
loops only 10 times at `port_allocator_test.go:198`). So the concurrency is almost
certainly **aggregate**, not from one test:

- Go runs packages in parallel up to `GOMAXPROCS` (**16 on this host**, FACT via `nproc`).
  ~30 packages with servers, overlapping, multiplies whatever each holds.
- **FACT:** no `PoolSize` is configured on any `redis.NewClient` call site, so go-redis
  uses its default `10 × GOMAXPROCS` = **160 connections per client** on this host.
  Seven construction sites exist (`internal/cache`, `internal/infraboot`,
  `internal/server` ×2, `tests/ddos`). Under `test-unit-full` / `test-full` (infra up),
  these pools are live and long-held — exactly the ESTAB-heavy shape observed.
  Under a plain unit pass with no Redis they fail fast and cost nothing.

**HYPOTHESIS (not FACT — I have no measurement of my own):** the ESTAB mass is
infra-client pools plus cross-package parallelism, and the `httptest` inventory in this
document is the TIME-WAIT *tail*, not the ESTAB *peak*. That would make this audit's
targets real but secondary. Per §11.4.6 I am labelling this a hypothesis, and the
measurement in §10.2 is what would confirm or kill it.

### 10.2 The decisive measurement

**The one measurement that would settle it** (and it belongs to whoever owns the
port-pressure resource, not to me): run **one package at a time**, sampling
`ss -tan state time-wait | wc -l` at ~1 s, and record peak TIME_WAIT per package. That
turns `R` from an assumption into a measurement and produces a per-package attribution
that this static audit cannot. Ranked by this document's estimates, the packages worth
measuring first are `internal/llm`, `internal/memory/providers`, `internal/notification`,
and `internal/discovery`.

---

## 11. Prior art already in this codebase

This exact failure mode has already been diagnosed and fixed once here — the fix is
documented in-source and is the model for §8.

`internal/cognee/client_stresschaos_test.go:58-89` (HXC-064). Verbatim:

> `• DisableKeepAlives (the previous setting): each of the 800 calls opens + tears
> down its OWN TCP connection. … (b) the rapid connect/close storm exhausted
> ephemeral sockets on macOS → connection errors ("reported N errors").`

The landed fix, at `:89`:

```go
c.httpClient.Transport = &http.Transport{MaxConnsPerHost: 1, MaxIdleConnsPerHost: 1}
```

— a single reused keep-alive connection for a 16 × 50 = 800-call concurrent test, with
`t.Cleanup(func() { _ = c.Close() })` draining it at test end. **800 calls, 1 port.**

A second precedent: `internal/providers/httpclient/httpclient_reuse_test.go:29` records
that `MaxIdleConnsPerHost >= 32` "is what keeps the pool warm."

Both confirm that fix classes **(b)** and **(c)** are established, accepted practice in
this repository — the remediation in §8 is applying an existing local pattern, not
importing a new one.

---

## Sources verified

All numbers in this document derive from the working tree at
`/home/milosvasic/Projects/helix_code/helix_code` as of 2026-09-07, read statically.
No test was executed, no build was run, and no network port was bound during this audit.
Scanner sources are retained in the session scratchpad at
`churn_audit.py`, `req_audit.py`, `close_audit.py`, `factory_audit.py`,
`tablesize2.py` and their JSON outputs (`servers.json`, `reqs.json`, `factories.json`).
