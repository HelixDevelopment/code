# Quickstart — Validating Adaptive Local Model Serving

**Date**: 2026-09-02 | **Plan**: [plan.md](./plan.md) | Entities: [data-model.md](./data-model.md)

Runnable scenarios that prove the feature works end-to-end. Commands cite targets that exist in
`submodules/helix_llm/Makefile` today. Implementation detail belongs in `tasks.md`, not here.

## Prerequisites

- Go 1.26.1 toolchain (`helix_llm` module).
- **No accelerator required for scenarios 1–4** — that is deliberate: selection is a pure function
  of (measured host, catalogue, declared usage), so its whole surface, refusals included, is
  reachable from fixture hosts.
- Scenarios 5–7 need real hardware and are honest SKIPs without it (never silent passes).
- Discovery secret in the environment or `.env` for scenario 6 — never committed, never logged.

```bash
cd submodules/helix_llm
make test-unit          # baseline must be green before starting
```

---

## Scenario 1 — A no-accelerator host gets real options, per family

**Proves**: FR-002 scenario 2, D5 — the guarantee holds per family, and a family that cannot be
served says so instead of returning an empty list.

```bash
make test-unit
```

**Expected**: with a fixture host having ample memory and zero accelerators —

- every processor-servable family returns a **non-empty** option set;
- families that cannot be served acceptably (generative image, generative audio) return a **stated
  reason naming what the host lacks** — not an empty list, and not an option below the family's
  usable threshold;
- no offered option requires an accelerator.

**Fails if**: any family returns an unexplained empty set, or an option is offered that would start
but perform below its family's usable threshold.

## Scenario 2 — Memory and storage are checked separately

**Proves**: D2, FR-004.

**Expected**: a fixture host with abundant memory but insufficient free storage does **not** receive
options whose weights cannot be stored, and the withheld reason is `insufficient_resources` citing
**storage**, not memory.

**Fails if**: a single conflated headroom number lets a storage-infeasible option through.

## Scenario 3 — Configuration cannot name the model

**Proves**: FR-056, D3 — the loophole the implementation used to fall through.

> Fixed 2026-09-02: `cmd/visiongen-boot` and `cmd/imagegen-boot` now treat their model
> variables as OUTPUTS and print `IGNORED-CONFIG:` when a value is present;
> `*_NEED_BYTES` is no longer honoured. This scenario is now a regression guard
> rather than a reproduction.

**Expected**:

- with `VISIONGEN_MODEL_GGUF` (or any equivalent) set to a specific model, selection **still**
  measures the host and does not treat that value as the choice;
- with measurement made to fail, selection **refuses and says why** — it does not start a default;
- a deliberate pin the host cannot run is **refused with the insufficient resource named**, not
  silently downgraded and not honoured.

**Fails if**: any configuration value determines which model runs, or a fixed default appears when
measurement is unavailable.

## Scenario 4 — Usage terms withhold models

**Proves**: FR-054, SC-012, D4.

**Expected**: declaring commercial use withholds every non-commercial model from selection, each
with its restricting term named. Declaring non-commercial use makes them available again. The
withheld reason is `excluded_by_usage_terms`, distinct from `insufficient_resources`.

**Fails if**: a non-commercially-licensed model is offered for declared commercial use, or the
exclusion is reported as a generic unavailability.

## Scenario 5 — Real measurement on real hardware

**Proves**: FR-001, FR-002, and §11.4.111 — the binding is by stable device identity.

```bash
make test-integration
```

**Expected**: measured accelerator memory reflects the actual device; the binding records a **stable
device identity, not an enumeration index**. With two accelerators present, reversing their
enumeration order leaves the binding pointing at the **same physical device**.

**Fails if**: the binding follows the index. **SKIPs (with reason)** if no accelerator is present —
never passes silently.

## Scenario 6 — Serving and propagation, end to end

**Proves**: FR-014, D7, D8 — real output, not a config read.

```bash
make test-e2e
```

**Expected**: a selected option actually serves a request and returns real model output; its
identity appears as `helixllm/<host>/<model>[:<variant>]` in the model-listing value field; the
consumer identifier is the derived charset-safe form; an instance that cannot present the
pre-shared secret is not trusted as a model source and receives no request content.

**Fails if**: the PASS rests on configuration state rather than a served response, or the exported
identifier was made to work by relaxing a consumer's validation (FR-014a).

## Scenario 7 — Behaviour under pressure

**Proves**: §11.4.169 coverage — stress, chaos, concurrency, memory, benchmark.

```bash
make test-stress test-chaos test-race test-benchmark
```

**Expected**: concurrent selections do not corrupt lease state or double-allocate accelerator
memory; a model process killed mid-serve is detected and reported, not reported as still serving;
repeated select/release cycles do not leak; measured throughput is recorded against a baseline.

**Fails if**: a killed instance is still exported as available, or concurrent selection
double-allocates.

---

## Full suite

```bash
make test-all
```

## Reading a result honestly

- A **PASS** must cite a real measurement and, above unit scope, a real served response. A PASS
  resting on configuration or absence-of-error is not a pass.
- A **SKIP** must name what was missing (no accelerator, no second device, no credential). A silent
  skip reported as a pass is the failure mode this whole feature exists to avoid reproducing.
- A **refusal is a valid, testable outcome** — most of scenarios 1–4 assert refusals. Selection that
  can never say no is not selection.
