# HXC-348 — independent status reconciliation (2026-09-08T20:31:31Z)

An agent reported this fixed. Verified independently against the live system.
**Verdict: PARTIALLY fixed, NOT deployed, plus an unaddressed regression. Item stays open.**

## The original defect is STILL LIVE
Probed the running gateway (pid 1143132, `bin/helixllm`, `:8443`) directly:

| Request model | Result |
|---|---|
| `definitely-not-a-real-model-zzz` | HTTP 200, answered by `qwen2.5-coder-3b-instruct-q4_k_m` |
| `gpt-4o` | HTTP 200, answered by `qwen2.5-coder-3b-instruct-q4_k_m` |
| `claude-opus-4` | HTTP 200, answered by `qwen2.5-coder-3b-instruct-q4_k_m` |
| `../../etc/passwd` | HTTP 400 |

## What IS true
- Root cause correctly diagnosed: `Router.Route` is off the production path;
  `main.go:414/490` wires `Brain: fallbackChain` (`*fallback.Chain`) into
  `gateway.RegisterRoutes`. `brain.Router.Route` is reachable only via
  `Brain.Complete`/`CompleteStream`, which the serving path never calls.
- The fix exists in the working tree: 7 modified + 3 new files in
  `submodules/helix_llm`, including a never-committed `internal/brain/model_not_found.go`.
- Its tests are genuine and pass — buffered, streaming and `/v1/completions`,
  asserting 404 `model_not_found`; `model:""` still falls back, and a served
  model on a down backend still gets 503 not 404.

## What is NOT true
- **Not committed.** `git log -- internal/brain/model_not_found.go` is empty.
- **Not deployed.** `bin/helixllm` built 2026-09-07 12:09:35; the fix sources are
  2026-09-08 19:13-19:24. The running binary predates the fix — §11.4.108 layer 3
  fails, which is why the probe above still shows the defect.
- The "42 → 43 packages" claim is unsubstantiated: both new test files land in
  existing packages; `go list ./...` reports 58. Treat that number as UNCONFIRMED.

## NEW REGRESSION the fix introduces (not previously surfaced)
`tests/integration/auth_test.go:59` — `TestAuth_ChatWithoutAuth` posts
`{"model":"auto"}` and now gets **404**, want 200 or 503.

`"auto"` is a documented sentinel meaning "let the gateway pick" — it appears in
`docs/courses/01-getting-started/lesson-03-first-api-call.md`, `website/content/_index.md`,
`tests/e2e/e2e_test.go`, `tests/e2e/multi_provider_e2e_test.go`, `tests/stress/stress_test.go`
and several plan docs — yet no production code special-cases the literal `"auto"`.
Pre-fix it fell through to the any-available-provider branch; post-fix it is refused
as an unknown model, breaking a documented client flow. Neither new `hxc348_*_test.go`
exercises `"auto"`, so only the pre-existing auth test caught it.

Two other failing packages (`cmd/agentgen-boot`, `cmd/visiongen-boot`) are
pre-existing host-capability skips, unrelated.

## Remaining work before this may close
1. Handle `"auto"` (and audit for other documented sentinels) under the refusal logic.
2. Commit. 3. Rebuild `bin/helixllm`. 4. Redeploy the running process.
5. Re-probe live `:8443` and confirm 404 — §11.4.108 / §11.4.130.
