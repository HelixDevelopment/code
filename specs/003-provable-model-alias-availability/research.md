# Phase 0 Research — Provable Model Alias Availability

**Date**: 2026-09-08 | **Plan**: [plan.md](./plan.md) | **Spec**: [spec.md](./spec.md)

Phase 0 exists to resolve the plan's NEEDS ATTENTION items before any repair is designed. The
principal unknown was: **why are 11 providers pinned in `overrides.json` and still reported
`orphaned`?** FR-020 commits to repairing all 20 orphans, and the repair shape depends entirely on
the cause, so planning a repair before knowing it would be the §11.4.102 violation this phase exists
to prevent.

## Decision: the orphan cause is NOT YET KNOWN. Four candidates are eliminated by measurement.

**Rationale.** Each candidate below was tested, not reasoned about. Four are eliminated. Reporting a
repair plan now would require guessing between the survivors, which §11.4.6 forbids.

| # | Candidate cause | Verdict | Evidence |
|---|---|---|---|
| 1 | Credentials missing or rotated | **ELIMINATED** | All 29 provider records name a key variable and all 29 of those variables are SET in the environment. Compared by hash throughout; no value read or printed (§11.4.10). |
| 2 | Model catalogue stale, absent or incomplete | **ELIMINATED** | `models.dev.cache.json` is 4.5 MB, parses, carries 213 providers, was 23.6 h old against a 24 h TTL, and contains 6 of the 7 orphan key variables checked. `HUGGINGFACE_API_KEY` is genuinely absent from it — a separate, single-provider issue, not the class cause. |
| 3 | Missing pins in `overrides.json` | **ELIMINATED as the class cause** | Running the resolver directly: **10 of the 11 pinned-yet-orphaned providers DO resolve** (`deepseek`, `huggingface`, `hyper`, `kc-for-coding`, `kilo`, `nvidia`, `opencode`, `openrouter`, `poe`, `zai-coding-plan`). Only `kc-for-coding2` does not, and it shares `ApiKey_Kimi` with `kc-for-coding` — the same one-record-per-key-variable mechanism as the 9 unpinned records. So pins are necessary for the 9; they are not what is failing for the 11. |
| 4 | Key variables invisible to `present_key_vars` | **ELIMINATED — refuted by experiment** | `present_key_vars` greps `$CMA_KEYS_FILE` for literal `NAME=` assignments only, and cannot see variables supplied by a file that keys file SOURCES. Measured: the operator's `api_keys.sh` carries exactly ONE literal assignment while the records reference FOURTEEN, so 13 of 14 are invisible to that grep. That looked decisive. It is not: re-running `list-all` against a temporary keys file containing all 14 names as literal (obviously fake) assignments produced **byte-identical counts** — 6 failed, 20 orphaned, 1 stale:verified, 2 verified. Visibility does not drive orphan status. |

**Alternatives still open (the honest remainder).** The leading surviving candidate is that
`list-all` computes orphan status from a **cached or stored resolved set** rather than resolving
live — which would explain precisely why changing the resolver's inputs changed nothing. That is
testable and is the first task of the next research pass. It is recorded as a hypothesis, not a
finding.

**Why candidate 4 is written up despite being wrong.** It produced a striking, publishable-looking
measurement — "13 of 14 key variables are invisible" — and the arithmetic that followed from it fit
the observed 20 orphans closely enough to be persuasive. It was still wrong. Recording the
elimination is worth more than deleting it, because the next person to measure key-variable
visibility will otherwise reach the same dead end and believe it (§11.4.7, §11.4.112).

## Instrument note (§11.4.273) — this phase is the anchor's first real use

The anchor requiring a positive and negative control on any decision-bearing census was minted from
this session's earlier errors, and it earned itself twice within this single research pass:

1. The first resolver invocation returned one record and no ids. The **positive control failed**
   (`helixllm-gateway` absent), so the instrument was treated as the suspect rather than the system —
   and it was: `--keys` takes a comma-separated list of key-variable NAMES, and it had been handed a
   file PATH, which it dutifully treated as a variable name. Without the control the reading would
   have been "the resolver resolves nothing", which is alarming, plausible, and false.
2. The corrected run's positive control ALSO failed, for a different reason: `helixllm-gateway` is
   out of the resolver's declared scope by design — Helix providers are added by the shell
   (`claude-providers.sh:248`, "providers_resolve.py stays pure"). **A positive control must lie
   inside the instrument's declared scope**; one chosen outside it produces a false
   instrument-is-broken signal. Re-run with `deepseek`, an in-scope catalogue provider, the control
   passed and the measurement became trustworthy.

A third case is the reason candidate 4 is in the eliminated column rather than the findings column:
`DEEPSEEK_API_KEY` was used as a positive control for key-file visibility when **its presence was
the very question under test**. A control whose truth is what you are measuring is not a control.

## Decision: honest SKIP-with-reason for three mandatory test types (§11.4.169 / §11.4.3)

**Rationale.** §11.4.169 requires the full test-type set where the domain warrants it. Three do not
apply here, and fabricating them would produce green suites asserting nothing — the PASS-bluff the
anchor exists to prevent.

| Test type | Disposition | Reason |
|---|---|---|
| DDoS / load-flood | **SKIP-with-reason** | The subject is a single-model gateway on loopback with one operator. There is no flood surface; a synthetic one would measure the harness. |
| Scaling | **SKIP-with-reason** | No horizontal dimension exists — one model, one host, one process. Re-evaluate if a second serving host is added. |
| Chaos (infrastructure failure injection) | **PARTIAL** | Process-death and network-fault injection against the gateway ARE meaningful and in scope. Disk-full and OOM injection are not: the component holds no significant state and allocates per request. |

Unit, integration, e2e, full-automation, security, stress, concurrency, race, memory and benchmark
all apply and are in scope for `tasks.md`.

## Decision: cross-platform parity is a declared, tracked gap (§11.4.81)

**Rationale.** The `Darwin*` date branches and the `uname -s` cache exist in the code and are
unexercised — this host is Linux only. Deleting them would break macOS outright; claiming them
verified would be a §11.4 bluff. The honest position is a declared gap carried in Complexity
Tracking until a Darwin host is available, stated rather than implied by an all-PASS table.

**Alternatives considered.** Emulation was rejected: a macOS `date` implementation running under
emulation proves the flag parsing, not the platform, and would produce evidence stronger-looking
than it is.
