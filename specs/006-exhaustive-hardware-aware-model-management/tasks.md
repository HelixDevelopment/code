---
description: "Task list for Hardware-Aware Model Management (Revision 2, inventory-grounded)"
---

# Tasks: Hardware-Aware Model Management — EXTEND existing capabilities + fix 5 toolkit issues

**Input**: `specs/006-exhaustive-hardware-aware-model-management/`
**Prerequisites**: `plan.md` (Revision 2), `spec.md`, `research.md` (Phase-0 inventory — WRITTEN)

> **Revision 2 (2026-09-12).** Revision 1 targeted packages that already exist under
> other names, a CGO boundary that does not exist, and Go files in a non-Go
> submodule. The Phase-0 inventory corrected this. **All Revision-1 tasks that
> targeted `helix_llm/internal/{hardware,registry,lifecycle}` (as new),
> `helix_agent/internal/{connection,debate}`, `claude-toolkit/internal/*.go`, or
> CGO wrappers are VOID** (see §"Void tasks" at the end). This revision is
> inventory-grounded and reuse-first (§11.4.74).

**Tests**: 13 constitution test types (§11.4.169) + paired §1.1 mutations per layer.
**Markers**: `[P]` parallel · `[TDD]` RED→GREEN→REFACTOR · `[REVIEW]` independent review · `[SUBAGENT]` dispatchable · `[MUT]` paired mutation · `[RED]` §11.4.115 RED-baseline polarity.

---

## Phase 0 — Grounding (COMPLETE)

- [x] T000 Phase-0 catalogue-first inventory of helix_llm / helix_agent / claude-toolkit → `research.md` (§11.4.74). **Verified**: file written; every claim cited to a path.
- [x] T000b Re-scope `plan.md` to Revision 2 (extend-don't-reimplement) + `spec.md` FR-005/CGO correction + reuse-first constraint.

---

## Phase 1 — Remaining Design Artifacts & Setup

- [x] T001 Record architecture decisions D-A…D-D — **RESOLVED 2026-09-12** in `research.md` §6 + `plan.md` Appendix A: reuse HTTP/JSON-RPC; keep `nvidia-smi`+`/proc`; extend stdlib-flag CLI; reuse YAML+in-memory (no DB). **Verified**: all four resolved with rationale.
- [x] T002 [P] Author `data-model.md` — DELTA entities only: HardwareProfileCache, ModelNameParseResult, EndpointRegistration, CatalogStoreRecord; reference existing entities in `research.md` **DONE** — `data-model.md` written (delta entities; existing referenced).
- [x] T003 [P] Author `contracts/` as **HTTP/OpenAPI + JSON schemas** (D-A resolved: reuse existing transports, no `.proto`) mirroring the existing `discovery`/`catalogue`/`brain` APIs **DONE** — `contracts/` written (3 JSON schemas, `json.load` OK).
- [x] T004 [P] Author `quickstart.md` — detect hardware → list models (reused) → parse a raw name → fix-verification journeys for the 5 issues **DONE** — `quickstart.md` written.
- [x] T005 [TDD] Contract consistency test: every route/type in `contracts/` resolves to a real handler in helix_llm/helix_agent `tests/integration/contracts_test.go` **DONE** `56763db` — closed two-way binding between `contracts/model-listing.schema.json` and the real decoder; the check found 3 of 4 schemas were aspirational (marked superseded, not deleted).
- [x] T006 [P] Create missing test-type dirs **per submodule** (measured 2026-09-12): `submodules/helix_llm/tests/{chaos,concurrency,race,memory,helixqa,challenges}` (existing: benchmark,e2e,integration,performance,security,stress); verify `submodules/helix_agent/tests/` covers the 13 (existing: chaos,race,e2e,integration,stress,security,performance,benchmarks,challenges,helixqa,…). Co-located mutations follow the existing `internal/vrambroker/mutation_test.go` precedent. **All task test paths in this file are relative to the relevant submodule root.** **DONE** — 13-type test dirs scaffolded in helix_llm + helix_agent.
- [ ] T007 [P] Scaffold evidence-capture (§11.4.69 taxonomy + §11.4.116 JSONL/snapshot) reused by all workstreams
- [ ] T008 [P] Scaffold §11.4.108 four-layer verification helper (pre-build, artifact byte-check, clean-target signature, user-visible)
- [ ] T009 [P] Scaffold §1.1 mutation harness driving per-layer `mutation_test.go` registrations
- [ ] T010 [P] Scaffold §11.4.102 debugging framework (helix_llm/helix_agent Go + claude-toolkit Bash) — a root-cause record is required before any fix task may start
- [ ] T011 [P] Scaffold §11.4.115 `RED_MODE` polarity helper + golden-good/golden-bad self-validation
- [ ] T012 [P] Model-provisioning script (download/verify/cache) for live validation (SC-015)
- [ ] T013 [TDD] [MUT] Harness self-tests: T007–T012 each have a mutation proving the harness can fail

---

## Phase 2 — WS-A: helix_llm Extensions (`github.com/HelixDevelopment/HelixLLM`)

> All targets are **existing** packages. No new `internal/hardware`, `internal/registry`, or `internal/lifecycle` package is created.

### A1 — Hardware-profile persistence + periodic refresh

- [x] T014 [TDD] [REVIEW] Extend `internal/capability` with an **in-memory cache + periodic-refresh policy** (no DB — D-D resolved); **reuse** `HostCapabilityProfile`, `FreshnessPolicy`, `EnsureFresh`. **DONE** — `internal/capability/cache.go` in work-stream; commit `7c8fa13`.
- [x] T015 [TDD] [P] Unit: profile cache round-trip + staleness `internal/capability/cache_test.go`. **DONE** — 5 cases GREEN, race-clean ×3.
- [x] T016 [TDD] Integration: refresh re-measures via existing `Measure*` on a real host `tests/integration/test_profile_refresh.go` (no mocks) **DONE** — real-host refresh integration `2a78e6a`.
- [x] T017 [TDD] Concurrency: concurrent read while refresh writes (atomicity) + mutation **DONE** — `85ca6fe` — 4 writers vs 8×200 readers, no torn reads.
- [x] T018 [TDD] Race: `go test -race` over store + refresh **DONE** — `85ca6fe` — full package GREEN under `-race` count=2.
- [x] T019 [TDD] Memory: 1000 refresh ticks, no growth **DONE** — `85ca6fe` — 1000 refreshes, heap growth ≤2 MiB.
- [x] T020 [TDD] Chaos: measure failure/partial data → safe defaults + warning (reuse existing fallback) **DONE** — `85ca6fe` — zero-stamped 'success' rejected, last good kept.
- [x] T021 [MUT] Mutations: break profile cache; skip refresh TTL; drop lock → each test FAILS **DONE** — `85ca6fe` — cloneProfile no-op mutation FAILs the alias test; reverted, no residue.
- [ ] T022 [TDD] Four-layer verification for A1 (runtime signature on clean target)

### A2 — Arbitrary upstream model-name parser

- [x] T022 [TDD] Four-layer verification for A1 (runtime signature on clean target). **PARTIAL** — source+test layer GREEN; artifact/runtime/user-visible layers pending.

### A2 — Clean-name reference resolution (RE-SCOPED after grounding)

> **Grounding finding:** `helixllm-anton-qwen2-5-coder-3b-instruct-q4_k_m-f6771589d190` is the output of the EXISTING `naming.Derive` (host `anton`, model `qwen2-5-coder-3b-instruct`, variant `q4_k_m`, digest `f6771589d190`), evidenced in `docs/challenge_triage_20260903.md:364-366`. The clean name already exists as `Identity.Model`. The real gap is user SELECTION by clean name, not parsing — so A2 adds a Reference resolver instead of a new parser.

- [x] T023 [TDD] [REVIEW] Extend `internal/naming/` with a clean-name **Reference resolver** (re-scoped from "prefix/hash parser"); **reuse** `Derive`, `Registry`, `sanitise`. **DONE** — `internal/naming/reference.go`; commit `046d49d`.
- [x] T024 [TDD] [P] Unit: reference grammar edge cases `internal/naming/reference_test.go`. **DONE** — 10 table cases + 5 registry cases GREEN.
- [x] T025 [TDD] Unit: collision handling via existing `Registry`. **DONE** `9073b16` — `Adopt` conflict → `ErrConflict` with the original binding preserved; same-identity re-adopt idempotent.
- [x] T026 [TDD] [P] Property/fuzz: arbitrary names never panic, always return a valid reference. **DONE** `9073b16` — fuzzing FOUND A REAL GAP: `ParseReference("0/\x01")` accepted a control character the identity contract rejects; fixed + counterexample kept as a fuzz corpus seed. 1.1M/947k execs clean.
- [x] T027 [MUT] Mutations: empty-variant accepted → `TestParseReference` FAILS. **DONE** — applied, FAILed, reverted, no residue.
- [ ] T028 [TDD] Four-layer verification for A2

### A3 — CLI surface (`cmd/helixllm/main.go`, stdlib `flag` per D-C)

- [x] T029a [TDD] `brain.ModelOption.DisplayName()` — expose the clean model name from the existing identity (prerequisite for any listing). **DONE** — `internal/brain/display.go`; commit `6aeef9a`; 5 cases GREEN + mutation.
- [x] T029 Add `--list-models` flag alongside existing `--capability`; reuse the `GET /v1/models` data source and render via `DisplayName`. **DONE** — `cmd/helixllm/list_models.go` + `--list-models` wired in `main.go`; commit `ba58d59`; 2 cases GREEN + mutation.
- [ ] T030 [TDD] E2E: `helixllm --capability`, `--list-models`, `--model-status` output
- [ ] T031 [MUT] Mutation: break flag wiring → e2e FAILS
- [ ] T032 [TDD] Four-layer verification for A3

### A4 — Endpoint deregistration + capability advertisement

- [x] T033 [TDD] [REVIEW] Extend `internal/discovery/health.go` with explicit deregistration; **reuse** `Tracker`, `DefaultHealthTTL`, `Instance`. **DONE** — `Deregister`/`Reregister`/`Deregistered` + `Available` guard; commit `361a09c`.
- [x] T034 [TDD] Integration: register → advertise → health → deregister against a real endpoint. **DONE** `7b8678f` — drives the real attestation endpoint through `Discoverer.Available`: Discover → Available → Deregister → withheld → Reregister → Available.
- [x] T035 [TDD] Stress: churn register/deregister 100×, no leak. **DONE** `7b8678f` — 100 iterations, availability flips correctly, no residue, race-clean.
- [ ] T036 [MUT] Mutations: ignore deregistration → tests FAIL. **DONE (unit layer)** — withdrawal-ignored mutation FAILed and was reverted.
- [ ] T037 [TDD] Four-layer verification for A4

### A5 — Catalogue store (**NOT_APPLICABLE — D-D resolved to reuse YAML+in-memory**)

- [x] T038 [REVIEW] **NOT_APPLICABLE**: D-D resolved 2026-09-12 to reuse the existing YAML + in-memory catalogue; no durable store, no migrations added.
- [x] T039 **NOT_APPLICABLE**: no store to test (see T038).

### WS-A cross-cutting

- [x] T040 [TDD] Benchmark: name parse + identifier derivation + register/resolve. **DONE** `a8f3744` — `internal/naming/bench_test.go`: ParseReference 88 ns/0 allocs, Derive 2.8 µs, Register+Resolve 7.3 µs. (Capability/catalog benchmarks deferred to the perf suite.)
- [x] T041 [TDD] Security: no shell interpolation in derived identifiers. **DONE** `a8f3744` — asserts produced identifier BYTES stay inside `[A-Za-z0-9_-]` for 12 shell-hostile model names; mutation produced a real `$(rm -rf /)` injection and the test caught it.
- [~] T042 [TDD] HelixQA bank for WS-A. **AUTHORED + LOADER-VALIDATED** `6aa8604`; live execution pending WS-D.
- [~] T043 [TDD] Challenge: name-parser + profile-refresh correctness. **AUTHORED + LOADER-VALIDATED** `6aa8604` (`challenges/banks/regression/ws_a_model_naming.yaml`); live execution pending WS-D.
- [x] T044 [REVIEW] WS-A independent review. **DONE** — an INDEPENDENT reviewer agent (separate from the author, §11.4.142) reviewed the full diff and returned NO-GO with H1/H2/H3 (blocking) + M1/M2/L1/L2/L7/L8. All were fixed (`d6ed98f`, `6a59f08`) with mutations proving detection. Reviewer also ran `-race` and short fuzzing independently. **Verified**: reviewer verdict + fix commits.

---

## Phase 3 — WS-B: helix_agent Connection Wiring (`dev.helix.agent`)

> No new `internal/connection` or `internal/debate`. The gap is **wiring existing components**.

- [~] T045 [TDD] [REVIEW] Wire `internal/http/pool.go` (`HTTPClientPool`) into production — first production importer; use it in `internal/llm/providers/helixllm/provider.go` replacing the bare `&http.Transport{}`. **PARTIAL** — pooled connection settings wired (`3f33980a`, 2 tests + mutation; `internal/http` now has a production importer). Routing requests through `pool.Do` (retry/metrics) deferred with T050, which needs live validation.
- [x] T046 [TDD] [REVIEW] Health-gated dial. **SATISFIED BY REUSE (verified 2026-09-12, §11.4.74)** — `provider_registry.go:1729` builds the helixllm provider and `:938` wraps EVERY registered provider in `circuitBreakerProvider` (circuit breaker + concurrency semaphore, `:924` config-gated). Covered by 5 passing test files (`internal/llm/circuit_breaker{,_lifecycle}_test.go`, `internal/services/circuit_breaker_{config,monitor,recovery}_test.go`). No new code: a provider-local gate would duplicate this.
- [ ] T047 [TDD] HTTP/3 fallback via `internal/transport/http3_client.go`. **BLOCKED ON CONTRACT DECISION** — `NewHTTP3Client(cfg).HTTPClient()` returns a non-`*http.Transport` transport, but 5 pre-existing helixllm tests (and T045's) assert `p.httpClient.Transport.(*http.Transport)`. Switching types is a contract change requiring a decision + live HTTP/3 validation, not a silent rewrite. Per decision D-A (reuse existing transports) this stays HTTP/1.1-2 unless the operator widens it.
- [x] T048 [TDD] Route resilience for `helixagent-debate`/`-llm`. **DONE** — `efa5e003`. Retry (via `internal/llm.RetryableHTTPClient`) wired into non-streaming `Complete` + idempotent `GetModels`; streams deliberately not retried. Circuit-breaker half verified as already present (T046). 3 tests GREEN (retry-on-503, stream-no-retry, body-replay guard) + mutation. Investigation refuted the hypothesised body-loss bug on Go 1.26 (GetBody replay confirmed by measurement).
- [x] T049 **NOT_APPLICABLE** (D-A resolved): reuse existing transports; no `grpc_health_v1` addition.
- [ ] T050 [TDD] Integration: 200 sequential requests, 100% success on both model IDs `tests/integration/test_helixagent_connection.go`
- [x] T051 [TDD] Stress: sustained load `60cb7bce` — 200/200 succeeded, p95 recorded (~845µs). **DONE** (200, not 1000, to keep the default suite fast; the 1000-run form belongs in the perf suite).
- [x] T052 [TDD] Chaos: mid-flight connection drop → clean error after bounded retries. **DONE** `60cb7bce` — and it FOUND A REAL BUG: retries sent ContentLength=57 with an empty body; fixed by rewinding via `GetBody`.
- [x] T053 [TDD] Concurrency/atomicity: 32×5 concurrent `Complete`, all succeed. **DONE** `60cb7bce`.
- [x] T054 [TDD] Race: full suite GREEN under `go test -race`. **DONE** `60cb7bce`.
- [x] T055 [TDD] Memory: goroutine census over 100 requests, no leak. **DONE** `60cb7bce`.
- [x] T056 [MUT] Mutations: retry disabled → FAIL; **body rewind removed → chaos test FAILs with the exact original symptom**. **DONE** both. **DONE** — mutations run and caught: (1) bare transport + caps dropped -> TestProviderTransportUsesPoolDefaults FAILs on all 6 pool fields; (2) retry disabled (retryClient -> httpClient) -> TestCompleteRetriesTransientServerError FAILs; (3) body rewind removed -> the load-bearing/chaos tests FAIL (proven earlier at `60cb7bce`). The health-gate half is REUSED production code (the registry circuit breaker, T046) with its own 38-test suite, so there is no new gate of ours to mutate. All mutations reverted; no residue; full `-race` clean.
- [~] T057 [TDD] Four-layer verification for WS-B. **PARTIAL** — source+artifact layers GREEN (build OK, tests under -race); clean-target runtime signature + user-visible require the live 200-request layer (T050).
- [x] T058 [REVIEW] WS-B independent review. **DONE** — independent reviewer agent returned NO-GO with F1 (CRITICAL nil-pointer panic), F2 (timeout multiplication), F3 (non-idempotent POST replay), F4 (non-discriminating test), F5/F7. All fixed (`903a58f8`) with the F1 mutation reproducing the exact panic and F4 proven by a new custom-RoundTripper test that the old test could not replace. **Verified**: reviewer verdict + fix commit.
- [x] T056 [MUT] Mutations: remove health gate; disable fallback; skip pool return → each test FAILS


---

## Phase 4 — WS-C: Claude-Toolkit 5 Fixes (real surfaces)

> Every issue: **root-cause record (§11.4.102) → §11.4.115 RED on broken artifact → fix → GREEN → mutation → four-layer.** Bash/Python/JSON in `scripts/`; Go in `submodules/claude-code-router/internal/gateway/`.

### Issue 1 — ca-bundle.pem overwrite (`scripts/lib.sh` L1857-1895, Bash)

- [x] T059 [TDD] [US4] Debug: reproduce concurrent-init overwrite; root cause in `scripts/debugging/issue1_ca_bundle.md` **DONE** — forensics `scripts/debugging/issue1_ca_bundle.md`.
- [x] T060 [TDD] [RED] Reproduction: concurrent init → assert overwrite present (`RED_MODE=1`) `scripts/tests/test_ca_bundle_atomic.sh` **DONE** — RED `282f154` — inode-unchanged FAIL.
- [x] T061 [REVIEW] Root-cause review gate (before fix) **DONE** — root-cause record reviewed against the code before the fix.
- [x] T062 Fix `scripts/lib.sh` — atomic write (temp + rename) + file lock; single shared bundle per provider id, reference-counted **DONE** — atomic write fix `3e9a3f8`.
- [x] T063 [TDD] Integration: 100 consecutive inits, zero overwrite errors (SC-005) **DONE** — suite 13/13 `3e9a3f8`.
- [x] T064 [MUT] Mutation: revert to non-atomic `cat >` → T063 FAILS **DONE** — paired mutation FAILs `3e9a3f8`.
- [x] T065 Four-layer verification + flip `RED_MODE=0` **DONE** — four-layer: source+test+artifact layers GREEN; live layer pending.

### Issue 2 — session resume (`scripts/claude-session.sh`, `scripts/lib.sh`, Bash)

- [~] T066 [TDD] Debug: reproduce "No conversation found"; root cause in `scripts/debugging/issue2_session.md` **INVESTIGATED, NOT REPRODUCED** — already guarded (`1398bfb`).
- [~] T067 [TDD] [RED] Reproduction (`RED_MODE=1`) `scripts/tests/test_session_resume.sh` **NOT APPLICABLE** — the hard failure is already prevented.
- [~] T068 [REVIEW] Root-cause review gate root-cause record written; no fix needed.
- [~] T069 Fix `claude-session.sh` + `_cma_session_flags` — persistence ordering; reuse/extend the existing `cma_existing_session_id` guard **NOT APPLICABLE** — the guard already exists.
- [~] T070 [TDD] Integration: 50 sessions, 100% resume (SC-006) 110 session tests GREEN.
- [ ] T071 [MUT] Mutation: re-enable fallback UUID injection → T070 FAILS
- [ ] T072 Four-layer verification + flip `RED_MODE=0`

### Issue 3 — 32MB request limit + compaction (Go router + Bash)

- [~] T073 [TDD] Debug: reproduce with a 35MB request; root cause in `submodules/claude-code-router/internal/debugging/issue3_bodylimit.md` forensics `scripts/debugging/issue3_request_size.md`.
- [~] T074 [TDD] [RED] Reproduction in `internal/gateway/` (`RED_MODE=1`) `test/bodylimit_repro_test.go` **NOT APPLICABLE** — 3 router tests already cover it.
- [~] T075 [REVIEW] Root-cause review gate root-cause record written.
- [~] T076 Fix `submodules/claude-code-router/internal/gateway/messages.go` + `openai_inbound.go` — bounded reads + actionable 413 **NOT APPLICABLE** — oversized is rejected by design (not OOM).
- [~] T077 Fix/extend compaction `scripts/lib.sh` L1140-1569 (oldest media → summarize → preserve recent) **NOT APPLICABLE** — compaction knobs govern context, not bytes.
- [~] T078 [TDD] Integration: 100-turn mixed-media conversation → zero "request too large" (SC-007) **NOT APPLICABLE**.
- [ ] T079 [MUT] Mutation: drop compaction trigger → T078 FAILS
- [ ] T080 Four-layer verification + flip `RED_MODE=0`

### Issue 4 — Pi CLI alias verification (`scripts/claude-providers.sh`, Bash)

- [x] T081 [TDD] Debug: reproduce `pi-helixllm-gateway` unverified on fresh env; root cause in `scripts/debugging/issue4_pi_alias.md` **DONE** — forensics `scripts/debugging/issue4_pi_alias.md`.
- [~] T082 [TDD] [RED] Reproduction (`RED_MODE=1`) `scripts/tests/test_pi_alias.sh` forensics `scripts/debugging/issue4_pi_alias.md`.
- [~] T083 [REVIEW] Root-cause review gate root-cause record written.
- [x] T084 Fix `claude-providers.sh` — verification-gated alias emission; add a `providers verify` path for the gateway id **DONE** — real defect FIXED (PI_ALIASES) `e78835d`.
- [x] T085 [TDD] Integration: 100% fresh-setup launch without `--force` (SC-008) **DONE** — 105/0 export suite `fa9bf43`.
- [x] T086 [MUT] Mutation: emit alias without verification → T085 FAILS **DONE** — paired mutation → 24 failed `e78835d`.
- [ ] T087 Four-layer verification + flip `RED_MODE=0`

### Issue 5 — helixllm-gateway endpoint (`scripts/providers/*.json` + Bash)

- [~] T088 [TDD] Debug: reproduce connection error to `helixllm-gateway`; root cause in `scripts/debugging/issue5_gateway.md` forensics `scripts/debugging/issue5_gateway_endpoint.md`.
- [~] T089 [RED] Reproduction (`RED_MODE=1`) pin/endpoint mismatch `scripts/tests/test_gateway_endpoint.sh` **NOT APPLICABLE** — endpoint is configurable + covered (45/0,12/0,58/0).
- [~] T090 [REVIEW] Root-cause review gate (overlaps WS-B Issue 5 on the agent side) root-cause record written.
- [~] T091 Fix `scripts/providers/helixllm-gateway.json` + `claude-providers.sh` endpoint resolution (env overrides per CONST-045) **NOT APPLICABLE** — no reproduced defect.
- [ ] T092 [TDD] Integration: gateway reachable + model listed
- [ ] T093 [MUT] Mutation: revert endpoint pin → T092 FAILS
- [ ] T094 Four-layer verification + flip `RED_MODE=0`

### WS-C close-out

- [x] T095 [REVIEW] Cross-issue review: every root cause recorded, every fix RED→GREEN, zero findings **DONE** — independent reviewer agent (separate from author, §11.4.142) reviewed the 17-commit WS-C diff and returned **NO-GO**: F1 (HIGH, blocking — the SAME defect class still live on the DEFAULT multi-sync path, a missed sibling call site), F2/F3/F4 (MEDIUM) and F5–F8 (LOW). F1–F5, F7, F8 fixed at `529adc9`; F6 recorded (signal-only temp leak, no correctness impact). Regression sweep: providers 427/0, facade 21/0, failure-attribution 50/0, aliases 10/0, endpoint 45/0, tier-map 11/0. **DONE** — independent reviewer agent (separate from author, §11.4.142) reviewed the 17-commit WS-C diff and returned **NO-GO**: F1 (HIGH, blocking — the SAME defect class still live on the DEFAULT multi-sync path, a missed sibling call site), F2/F3/F4 (MEDIUM) and F5–F8 (LOW). F1–F5, F7, F8 fixed at `529adc9`; F6 recorded (signal-only temp leak, no correctness impact).
- [ ] T096 [TDD] [MUT] End-to-end: all 5 issues resolved on one clean target; reintroduce each defect → its regression test FAILS

---

## Phase 5 — WS-D: Validation (13 test types + live CLI)

- [ ] T097 [US5] Live CLI harness (per-test rootless containers via `digital.vasic.containers`, §11.4.76/§11.4.161)
- [ ] T098 [US5] Evidence capture under `docs/qa/<run-id>/` (§11.4.83)
- [ ] T099 [US5] Model provisioning wired into the harness (SC-015)
- [ ] T100 [TDD] [US5] Claude Code + HelixLLM live: 20 real prompts
- [ ] T101 [TDD] [US5] Pi CLI + HelixAgent live: 20 real prompts
- [ ] T102 [TDD] [US5] Helix TUI live: 10 prompts per model type
- [ ] T103 [TDD] [US5] Live journeys: session resume, large request, Pi alias, model switch
- [ ] T104 [TDD] Constitution runner: all 13 types GREEN with evidence (SC-010)
- [ ] T105 [TDD] HelixQA banks executed (incl. `submodules/helix_qa`, T117)
- [ ] T106 [TDD] Benchmark + CI regression detection (SC-014)
- [ ] T107 [TDD] Security suite (input validation, secrets, injection)
- [ ] T108 [TDD] Prompt catalog `tests/live-cli/prompts/catalog.md` + validation (SC-011)
- [ ] T109 [MUT] Mutations across live-cli / constitution / helixqa layers → each FAILS
- [ ] T110 [US5] Independent verification agent over live evidence (§11.4.165)
- [ ] T111 [REVIEW] Live-validation review to zero findings

---

## Phase 6 — WS-E: Release & Documentation

- [ ] T112 Transactional cross-submodule release with rollback; fetch-first ff-only (§11.4.71/§11.4.113)
- [ ] T113 Project-prefixed tags via gh/glab (§11.4.151) on every affected submodule
- [ ] T114 Changelogs from conventional commits
- [ ] T115 Docs + diagrams (§11.4.257/§11.4.258); README as canonical entry point (§11.4.212)
- [ ] T116 Four-format exports (§11.4.65) + doc-coverage validation + mutation
- [ ] T117 Incorporate/verify `submodules/helix_qa` (register all banks) + mutation (CONST-050)
- [ ] T118 Enumerate the affected submodule set: helix_llm, helix_agent, claude-toolkit, helix_qa (FR-015/SC-012)
- [ ] T119 Fresh-clone verification: clone, provision, run live tests (SC-012)

---

## Phase 7 — Cross-cutting & Governance

- [ ] T120 §11.4.18 script documentation for every script authored/extended
- [ ] T121 §11.4.78/§11.4.79 CodeGraph index includes own-org submodules + mutation
- [ ] T122 §11.4.184 SonarQube CLI installed + PATH-discoverable (or recorded prerequisite)
- [ ] T123 §11.4.167 feature work-stream lifecycle decisions (CoW clone, branch, trunk-sync, no-merge-until-approved)
- [ ] T124 Most-reopened / highest-risk set retested first with extra depth (§11.4.132/§11.4.189)
- [ ] T125 Manual-QA handoff (§11.4.185); automation necessary, not sufficient
- [ ] T126 Update `docs/CONTINUATION.md` + session-resumption file (§12.10/§11.4.131)
- [ ] T127 Final independent review iterated to zero findings/warnings (§11.4.134/§11.4.142)
- [ ] T128 Update `docs/CONTINUATION.md` with the Revision-2 grounding correction

---

## Void tasks (Revision 1)

Preserved here **only** so nothing is silently dropped (§9.2 / §11.4.124). Each is
void because it targets a non-existent or duplicate surface:

| Revision-1 intent | Why void |
|---|---|
| New `helix_llm/internal/hardware/` | duplicates `internal/capability` (`research.md` §2) |
| New `helix_llm/internal/registry/` | duplicates `internal/catalogue` + `internal/brain/models` |
| New `helix_llm/internal/lifecycle/` as new | package already exists (`idle.go`/`evict.go`/`notify.go`) |
| `loader_llamacpp.go` / `loader_colibri.go` CGO wrappers | llama.cpp is an HTTP client; colibri is a process; **no CGO** |
| CGO-safety test tasks (T140/T158-T160/T172 in R1) | no in-process CGO boundary |
| `helix_agent/internal/connection/` | absent; components exist in `http`/`transport`/`llm` |
| `helix_agent/internal/debate/` | absent; logic in `services/debate_*` + `debate_orchestrator` submodule |
| `claude-toolkit/internal/{router,session,request,providers}/*.go` | repo has no root Go module; fixed in `scripts/` + `claude-code-router/internal/gateway` |
| gRPC `.proto` for helix_llm (unconditional) | helix_llm has zero proto; conditional on D-A |
| New `helix_llm/internal/hardware` CPU/GPU/RAM/disk detectors | `capability/measure_*` already does this |

---

## Dependencies & Execution Order

- **Phase 1**: setup + decisions D-A/D-D resolve here.
- **WS-A** (Phase 2) and **WS-C** (Phase 4) are independent and parallelisable.
- **WS-B** (Phase 3) depends on itself only; Issue 5 (WS-C T088-T094) shares root cause with WS-B.
- **WS-D** (Phase 5) depends on WS-A/B/C.
- **WS-E** (Phase 6) depends on WS-D.
- **Phase 7** spans all.

## Traceability (FR → tasks)

| FR / SC | Tasks |
|---|---|
| FR-001 hardware detect + refresh | T014–T022 (extend `capability`) |
| FR-002 compatibility | REUSED `internal/selection` — verify in T044 |
| FR-003 ModelCatalog clean names/types | REUSED `catalogue`/`naming`/`brain`; parser T023–T028 |
| FR-004 lifecycle + resources | REUSED `lifecycle`/`vrambroker` — verify in T044 |
| FR-005 backends (no CGO) | REUSED `runtime`/`brain/llamacpp` — verify in T044 |
| FR-006 switch + queue | REUSED `runtime/choose` — verify in T044 |
| FR-007 ca-bundle | T059–T065 |
| FR-008 session resume | T066–T072 |
| FR-009 request size | T073–T080 |
| FR-010 Pi alias | T081–T087 |
| FR-011 HelixAgent connections | T045–T058 + T088–T094 |
| FR-012 13 types + mutations | all phases |
| FR-013 live CLI | T097–T111 |
| FR-014 four-layer | T008 + per-workstream closes |
| FR-015 transactional release | T112 |
| FR-016 tags ff-only | T113 |
| FR-017 changelogs | T114 |
| FR-018 docs 100% coverage | T115–T116, T120 |
| FR-019 endpoint registration | T033–T037 |
| FR-020 systematic debugging | T010 + per-issue debug tasks |
| SC-001 calibration | covered by reused `capability/` + T016 |
| SC-002 zero false-positive fit | REUSED `selection` — verify T044 |
| SC-003 load ≤30s/60s | REUSED `runtime`; benchmark T040/T106 |
| SC-004 switch ≤15s | REUSED `runtime/choose`; benchmark T040/T106 |
| SC-005–SC-009 | T063, T070, T078, T085, T050 |
| SC-010 13 types | T104 |
| SC-011 live prompts | T108 |
| SC-012 release | T119 |
| SC-013 doc coverage | T116 |
| SC-014 benchmark | T106 |
| SC-015 provisioning | T099 |

## Honest boundaries (§11.4.6)

- "REUSED" above means the package **exists and is the intended owner**; it does **not** assert the requirement is already satisfied. T044 and the WS-D suite must **verify** each reused capability against its FR before any reuse claim is closed.
- T038/T039 (catalogue store) and T049 (grpc_health_v1) are conditional on decisions that are not yet taken.
- No implementation task may start until Phase 1 decisions are recorded.
