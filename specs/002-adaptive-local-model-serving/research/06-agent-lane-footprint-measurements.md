# 06 — Agent-lane footprint measurements (OPEN-24)

**Measurement pass, not a research pass.** Every figure below was produced by
running the weights on this host on 2026-09-03 and reading a number off the
running process. Nothing here is extrapolated from a published figure, and where
a figure could not be produced this file says so instead of supplying one.

This file exists because `internal/catalogue/data/text.yaml`'s invariant 3
(UNVERIFIED FIGURES ARE NEVER RESOLVED INTO A CONFIDENT NUMBER) is what blocked
the agent lane's three configured candidates from ever being catalogued, and
SELF-7 records what happened the last time a footprint was guessed instead.

---

## What OPEN-24 actually is, reproduced

The agent lane (`cmd/agentgen-boot`, `family = catalogue.FamilyText`) selects
*within* the catalogue and never names a model. Its three former configured
candidates — Mistral-Nemo-Instruct-2407, GLM-4.7-Flash, DeepSeek-Coder-V2-Lite —
are absent from the catalogue, so an operator holding working weights is refused.
Reproduced verbatim on the shipped binary, before any change:

```
$ go run ./cmd/agentgen-boot plan --pin mistral-nemo-12b:q4_k_m
DECLARED-USAGE: commercial (default — the narrowest purpose; …)
PIN: mistral-nemo-12b:q4_k_m — a constraint on the choice, not a bypass: …
MEASURED host=anton cpu=16 memory_available=12119MiB storage_available=1119753MiB accelerators=1 (measured)
WITHHELD mistral-nemo-12b:q4_k_m: unsupported_configuration — this host provides no
  catalogue-entry (mistral-nemo-12b:q4_k_m); more memory does not help; remedy=different-approach
CANNOT-CHOOSE: no text model can run on this measured host.
  what the candidates lacked: catalogue-entry
exit status 22
```

`more memory does not help; remedy=different-approach` is the narrowing: the
refusal is not "your host is too small", it is "no such model exists here".

---

## Host the measurements were taken on

```
$ grep -E 'MemTotal|MemAvailable|SwapFree' /proc/meminfo     # at measurement start
MemTotal:       31717560 kB          #  30.2 GiB
MemAvailable:   12570320 kB          #  11.99 GiB
SwapFree:           1476 kB          #  swap is EXHAUSTED (8 GiB total, ~0 free)
```

```
$ /usr/bin/llama-server --version
version: 8681 (Debian)
$ /usr/bin/llama-server --list-devices
Available devices:                    # ← empty: this is the CPU-ONLY Debian build
$ nvidia-smi --query-gpu=name,memory.total,memory.free --format=csv
NVIDIA GeForce RTX 3060, 12288 MiB, 10183 MiB
```

An RTX 3060 is present and idle, and **the installed llama.cpp cannot use it.**
Every figure below is therefore a **CPU / host-RAM** figure. No GPU-path figure
was measured, and none is stated.

Weights live in `/home/milosvasic/models/`, which is outside every git
repository (`git rev-parse` there: `fatal: not a git repository`) — CONST-053.

---

## Feasibility, decided BEFORE downloading anything

Sizes come from the HuggingFace model API (`/api/models/<repo>/tree/main`),
which returns each file's byte size and its LFS `oid` (a sha256). That is the
same authoritative-source method `embedding.yaml` and `vector.yaml` already
record as `measured:huggingface-blob-api`, and it costs no RAM, so it was done
first rather than after a multi-GB download.

| model | Q4_K_M bytes | GiB | vs 11.99 GiB available | verdict |
|---|---:|---:|---:|---|
| Mistral-Nemo-Instruct-2407 | 7 477 208 192 | 6.96 | 58 % | **measurable** |
| DeepSeek-Coder-V2-Lite-Instruct | 10 364 416 768 | 9.65 | 80 % of RAM, before KV or repack | **not measurable here** |
| GLM-4.7-Flash | 18 312 339 808 | 17.06 | 142 % — exceeds total available | **not measurable here** |

The two refusals are not caution, they are arithmetic:

* **GLM-4.7-Flash** needs 17.06 GiB of weights against 11.99 GiB available with
  no swap. It cannot be resident. It also does not fit the 10 183 MiB idle
  RTX 3060, so a CUDA build landing would not change this verdict.
* **DeepSeek-Coder-V2-Lite** at 9.65 GiB leaves 2.3 GiB for a repack buffer that
  Mistral-Nemo measured at 4.89 GiB (below), plus KV and compute. Under mmap
  llama.cpp *would* still emit a token by demand-faulting pages — but the
  footprint you would then measure is however much page cache the kernel
  happened to allow, i.e. a property of this host's memory pressure and not of
  the model. Recording that as `memory_required_bytes` would be SELF-7 in a new
  costume: a number that looks measured and describes the wrong thing.

Disk was never the constraint — 1.1 TB free.

---

## Mistral-Nemo-Instruct-2407 Q4_K_M — MEASURED

### Provenance of the file

| property | value |
|---|---|
| local path | `/home/milosvasic/models/Mistral-Nemo-Instruct-2407-Q4_K_M.gguf` |
| size (`stat -c %s`) | `7477208192` |
| size declared by HF API | `7477208192` — **agrees** |
| sha256 computed on this host | `7c1a10d202d8788dbe5628dc962254d10654c853cae6aaeca0618f05490d4a46` |
| sha256 published as HF LFS oid | `7c1a10d202d8788dbe5628dc962254d10654c853cae6aaeca0618f05490d4a46` — **agrees** |
| weights repo | `bartowski/Mistral-Nemo-Instruct-2407-GGUF` |
| repo revision | `a2dd64a0a76ea1bdb2bb6ab6fa5496b003c7c908` |
| gated | `false` |
| licence (HF cardData, and upstream `mistralai/Mistral-Nemo-Instruct-2407`) | `apache-2.0` |

```
$ sha256sum Mistral-Nemo-Instruct-2407-Q4_K_M.gguf
7c1a10d202d8788dbe5628dc962254d10654c853cae6aaeca0618f05490d4a46  Mistral-Nemo-Instruct-2407-Q4_K_M.gguf
```

This is the first digest in this catalogue verified against bytes on disk rather
than left absent. `embedding.yaml` anticipated exactly this: *"Populate the
digest … at first authorized download."*

### It runs — real output, not a load test

```
$ llama-completion -m …Q4_K_M.gguf -c 4096 -n 16 -t 4 --no-warmup --temp 0 \
    -p "The capital of France is" < /dev/null
The capital of France is Paris. Paris is known for its iconic landmarks such as
exit_code=0
```

### Allocation accounting — the figures that are pressure-independent

`llama_memory_breakdown_print`, verbatim, at two contexts:

```
n_ctx=512
| memory breakdown [MiB] | total   free    self   model   context   compute    unaccounted |
|   - Host               |                 7479 =  7083 +      80 +     315                |
|   - CPU_REPACK         |                 5006 =  5006 +       0 +       0                |

n_ctx=4096
| memory breakdown [MiB] | total   free    self   model   context   compute    unaccounted |
|   - Host               |                 8037 =  7083 +     640 +     314                |
|   - CPU_REPACK         |                 5006 =  5006 +       0 +       0                |
```

Two runs, two contexts, and the components behave exactly as they should:

| component | n_ctx=512 | n_ctx=4096 | reading |
|---|---:|---:|---|
| `model` | 7083 MiB | 7083 MiB | identical — independent of context and of host pressure |
| `compute` | 315 MiB | 314 MiB | identical within rounding |
| `CPU_REPACK` | 5006 MiB | 5006 MiB | identical — a haswell-backend repack of the weights |
| `context` (KV) | 80 MiB | 640 MiB | **exactly 8× for 8× context** |

The KV axis is therefore **measured-linear**: `640 MiB / 4096 tokens =
0.15625 MiB/token = 163840 bytes per token`, confirmed across an 8× range
rather than assumed from one point.

### Why peak RSS is NOT used as the figure

Peak resident set was captured independently, by polling `VmHWM` in
`/proc/<pid>/status` every 0.5 s:

| run | n_ctx | MemAvailable at start | peak `VmHWM` |
|---|---:|---:|---:|
| generation | 512 | 9 849 416 kB | 9 722 760 kB = **9496 MiB** |
| generation | 4096 | 12 570 320 kB | 8 101 892 kB = **7912 MiB** |

The run with **more** context and **more** free memory peaked **1584 MiB
lower**. That is not noise to be averaged away — it is the signature of mmap:
weight pages are file-backed and clean, so how many stay resident is decided by
the kernel under prevailing pressure, not by the model. **Peak RSS is a property
of the host at that moment and cannot be an admission figure.** The allocation
accounting above is the quantity that reproduced identically, and it is the one
this measurement rests on.

### The figure recorded, and its honest boundary

* `storage_required_bytes` = **7 477 208 192** — MEASURED, byte-exact, twice
  (local `stat`, HF API), digest-verified.
* `memory_required_bytes` = **9 956 106 240** (9496 MiB) — the highest resident
  peak actually observed. Chosen over the 8037 MiB steady-state accounting
  because the load-time transient — when the mmap'd source tensors and the
  4.89 GiB repack destination are both live — genuinely reached it, and an
  admission figure has to survive the transient, not just the steady state.
* Chosen *under* the fully-accounted ceiling of 8037 + 5006 = **13 043 MiB**,
  which nothing observed ever reached, because `CPU_REPACK` substantially
  overlaps the mmap'd weights it replaces. Recording 13 043 MiB would withhold
  the model from hosts that demonstrably run it.
* **UNVERIFIED:** the true requirement lies somewhere in
  `[8037, 13043] MiB`; 9496 MiB is the measured evidence within that interval,
  not a proof of the endpoint. A host near the figure may thrash rather than
  fail — both runs above were slow for exactly that reason.

### The context claim, and why it is 4096 and not 131072

The model's architectural maximum is 131 072 tokens. Pairing that number with
this memory figure would be false: at the measured 163 840 B/token, a 131 072
context is **20 480 MiB of KV alone**, so the honest total there is ~29 GiB —
roughly three times the recorded figure, and beyond this host entirely.
The entry therefore records the context it was **measured at**, 4096, and states
the per-token KV cost so any other context is derivable by a stated rule instead
of implied by an unstated one.

### Throughput

Measured, and deliberately **not** promoted to
`expected_capability.throughput_tokens_per_second`:

```
n_ctx=512 : eval time = 14030.28 ms / 23 runs ( 610.01 ms per token, 1.64 tokens per second)
n_ctx=4096: eval time = 17242.55 ms / 15 runs (1149.50 ms per token, 0.87 tokens per second)
```

These are CPU-only, 4 threads, on a host whose swap is exhausted — a property of
this measurement rig, not a capability of the model. It is carried in
`annotations` with its conditions attached; the entry keeps the family's standing
`UNVERIFIED: no measured throughput figure` note, because no figure was measured
under conditions that would generalise.

---

## The two that could not be measured, and what each needs

Neither is deleted, guessed, nor derived. Both remain deferred, each with the
measurement it is waiting on — the discipline `text.yaml` already applies to 24
of its 30 researched models.

### GLM-4.7-Flash

Never researched: it appears in **no** `model_id` in `text.yaml`, deferred block
included. Established here, all from authoritative sources:

| property | value |
|---|---|
| upstream | `zai-org/GLM-4.7-Flash`, licence `mit`, gated `false` |
| architecture (`config.json`) | `Glm4MoeLiteForCausalLM`, `model_type: glm4_moe_lite` — mixture-of-experts |
| | 47 layers, hidden 2048, 64 routed experts + 1 shared, 4 experts/token |
| `max_position_embeddings` | 202 752 |
| GGUF Q4_K_M | `unsloth/GLM-4.7-Flash-GGUF` @ `0d32489ecb9db6d2a4fc93bd27ef01519f95474d`, **18 312 339 808 B**, sha256 `29837ed2c0fc5f51981adf8ac8083fcf80743c598381f13e9f06cbad0498b174` |
| official ggml build | `ggml-org/GLM-4.7-Flash-GGUF` @ `7559e96b7e324ab405897dc2b91492b0f376ad4a`, `Q4_K` **18 244 193 920 B** |

**NEEDS: a measured in-use footprint on a host with ≥ 24 GiB of usable RAM, or
a GPU with ≥ 20 GiB.** 17.06 GiB of weights does not fit 11.99 GiB of RAM or a
12 GiB card.

Also note for whoever catalogues it: it is mixture-of-experts, and per
`text.yaml` invariant 1 that says **nothing** about streaming eligibility —
eligibility is roster membership by name and nothing else. `glm-4.6` is already
in the deferred block as an MoE resolving to not-eligible.

### DeepSeek-Coder-V2-Lite-Instruct

Also never researched — absent from every `model_id` in `text.yaml`.

| property | value |
|---|---|
| upstream | `deepseek-ai/DeepSeek-Coder-V2-Lite-Instruct`, licence `other`, gated `false` |
| GGUF Q4_K_M | `bartowski/DeepSeek-Coder-V2-Lite-Instruct-GGUF` @ `8f248fa2072348f77a8bc37754e470de1f61866e` |
| size | **10 364 416 768 B** |
| sha256 (HF LFS oid) | `603bd3f8a0281d16571da7c08bd661ee17ff0d1be6fcbd1b42242da257ef0bb8` — identical in `lmstudio-community/DeepSeek-Coder-V2-Lite-Instruct-GGUF` |

**NEEDS TWO THINGS, and the second is not a measurement:**

1. A measured in-use footprint on a host with ≥ 16 GiB usable RAM (9.65 GiB of
   weights plus a repack buffer of Mistral-Nemo's order plus KV). It is the one
   of the three that would fit the idle RTX 3060's 10 183 MiB if the CUDA build
   lands — as a GPU-path figure, which is a different axis from everything
   measured here.
2. **An enumerated licence.** HF reports `license: other` — the DeepSeek Model
   Licence, not an SPDX identifier. `Entry.Validate` refuses an entry with no
   `license_id` *and* refuses one with no `permitted` purpose, and the four
   Gemma models in the deferred block are blocked on precisely this: terms
   established but purposes not enumerated. Someone must read the licence and
   record which purposes it grants. **No figure can substitute for that**, so
   this model would stay deferred even with a footprint in hand.

---

## Cross-lane note (not fixed here — OPEN-25)

The agent lane sets `AcceleratorBound: true`, so `memory_required_bytes` is
checked against a *device* budget through `vrambroker` as well as against host
RAM. Every figure in this file is a **host-RAM, CPU-backend** measurement, and
`CPU_REPACK` — 4.89 GiB of it — is a CPU-backend artefact that has no meaning on
a GPU. Handing this figure to a VRAM broker is exactly the shape OPEN-25
describes. This measurement pass does not change that, and does not pretend the
figure is device-neutral: it is labelled CPU throughout.

---

## Proof the lane admits it

Same binary, same real host, catalogue swapped via `HELIXLLM_CATALOGUE_DIR`.
The refusal changes kind — which is precisely what OPEN-24 is:

```
# BEFORE — shipped catalogue
WITHHELD mistral-nemo-12b:q4_k_m: unsupported_configuration — this host provides no
  catalogue-entry (mistral-nemo-12b:q4_k_m); more memory does not help; remedy=different-approach
  what the candidates lacked: catalogue-entry

# AFTER — catalogue carrying the measured entry
WITHHELD mistral-nemo-12b:q4_k_m: insufficient_resources — memory short by 1822MiB
  (needs 9494MiB, 7671MiB available after 4646MiB held back to keep the host responsive);
  remedy=change-host-or-pick-smaller
  what the candidates lacked: memory
```

The operator is no longer told "no such model, try a different approach". They are
told their host is 1822 MiB short — an honest, actionable refusal against the
MEASURED figure (`needs 9494MiB` is this file's 9 956 106 240 B). This host cannot
run it; that was always true and is now said correctly.

That the entry reached the resource comparison at all is itself the proof that it
passed every non-resource gate — `supports()` and `excludedBy()` — since a failure
in either would have produced `unsupported_configuration` or
`excluded_by_usage_terms` instead.

### Offered, on a host that can run it

A fixture host (48 GiB RAM, 24 GiB card — the agent lane is `AcceleratorBound`, so
the card is checked too) against the real catalogue, with the §11.4.115 polarity
switch the repo already uses:

```
$ go test ./internal/selection/ -run TestAgentLaneAdmitsTheMeasuredMistralNemo -v
GREEN: OFFERED helixllm/fixture-open24-can-run-nemo/mistral-nemo-12b:q4_k_m
       memory=9494MiB storage=7130MiB source=https://huggingface.co/bartowski/Mistral-Nemo-Instruct-2407-GGUF
--- PASS

$ RED_MODE=1 go test ./internal/selection/ -run TestAgentLaneAdmitsTheMeasuredMistralNemo -v
RED: mistral-nemo-12b is absent from the catalogue entirely; 4 text options offered
--- PASS
```

The GREEN assertion includes `Entry.ValidateForAcquisition()`, which every other
entry in this catalogue fails: it requires a complete integrity block, and this is
the first entry whose digest was verified against bytes on disk.

Regression check, same catalogue: `go test ./internal/catalogue/...` ok;
`go test ./internal/selection/` ok; `TestShippedCatalogueLoads` reports
`35 entries across 8 families, 30 carrying a source` (34/29 before).

---

## Appendix A — the entry, as applied

**APPLIED** in helix_llm `b83babf`, on top of `7dd5835` (which includes
CRITICAL-2's `ce116c5`). It was fenced to a concurrent agent while this
measurement was taken; the fence was released once that agent landed, and the
insertion point was re-read against current HEAD before pasting.

It sits at the end of the `entries:` list, immediately before the `# DEFERRED`
header. Deferred record 18 was NOT deleted — it was replaced by a pointer saying
the entry was promoted and why the researched figure was not carried forward,
so the trail survives. Three header claims the entry invalidated were corrected
in the same commit: the file no longer says every figure comes from the research
pass, no longer says no digest exists anywhere in it, and its deferred count
goes 24 → 23.

```yaml

  # =========================================================================
  # Mistral. Apache 2.0. The one agent-lane candidate of the three that this
  # host could actually run — see
  # specs/002-adaptive-local-model-serving/research/06-agent-lane-footprint-measurements.md
  # =========================================================================

  - model_id: mistral-nemo-12b
    variant: q4_k_m
    family: text
    architecture: dense
    descriptor:
      parameter_count: 12000000000
      quantisation: q4_k_m
      specialisations: [general, instruction-following]
    memory_required_bytes: 9956106240
    storage_required_bytes: 7477208192
    requires_accelerator: false
    source: https://huggingface.co/bartowski/Mistral-Nemo-Instruct-2407-GGUF
    usage_terms:
      license_id: apache-2.0
      permitted: [commercial, personal, research, evaluation]
    runtime: in-memory
    expected_capability:
      context_tokens: 4096
      modalities: [text]
    integrity:
      algorithm: sha256
      digest: 7c1a10d202d8788dbe5628dc962254d10654c853cae6aaeca0618f05490d4a46
      size_bytes: 7477208192
    notes:
      - "MEMORY IS MEASURED-IN-USE, NOT RESEARCHED: 9956106240 B (9496 MiB) is the highest resident peak (VmHWM, polled from /proc) actually observed while this build generated text on this host, 2026-09-03. It REPLACES the research pass's 7623566950 B, which was a published *VRAM* figure and understates the measured CPU footprint by 2.3 GiB — admitting this model to a host that then cannot run it is the exact failure this catalogue exists to prevent."
      - "MEASUREMENT IS CPU-PATH: the installed llama.cpp (Debian b8681) is a CPU-only build — `--list-devices` lists none — so no GPU-path figure was measured and none is stated. 4886 MiB of the figure is a `CPU_REPACK` buffer that is a CPU-backend artefact with no meaning on a device."
      - "UNVERIFIED: the true requirement lies in [8037, 13043] MiB — llama.cpp's steady-state Host accounting at this context vs. its fully-accounted ceiling including CPU_REPACK. 9496 MiB is the measured evidence inside that interval, not a proof of its endpoint. Peak RSS under mmap is host-pressure dependent (a 4096-context run on a less-loaded host peaked 1584 MiB LOWER than a 512-context run), which is why the recorded figure is an observed peak and not an average."
      - "CONTEXT IS THE MEASURED ONE, 4096 — NOT the model's 131072 architectural maximum. KV cost is measured-linear at 163840 B/token (80 MiB at n_ctx=512, 640 MiB at n_ctx=4096 — exactly 8x for 8x context, confirmed across the range). A 131072 context is therefore 20480 MiB of KV alone, ~29 GiB in total; pairing that context with this memory figure would be false."
      - "UNVERIFIED: no generalisable throughput figure. 1.64 tok/s (n_ctx=512) and 0.87 tok/s (n_ctx=4096) were measured, but CPU-only on 4 threads with host swap exhausted — a property of the measurement rig, not of the model. Conditions are carried in annotations rather than promoted to expected_capability."
      - "DIGEST IS VERIFIED AGAINST BYTES ON DISK: sha256 computed locally on the downloaded file equals the HuggingFace LFS oid, and the local size equals the API-declared size. This is the first entry in this catalogue whose integrity block is complete, so it is also the first that Entry.ValidateForAcquisition does not refuse."
    annotations:
      summary: >-
        Dense 12B Mistral with a long architectural context, quantised to Q4_K_M.
        Runs on a no-GPU host: measured generating real text on a CPU-only
        llama.cpp build. The agent lane's former configured candidate, catalogued
        here from a measurement rather than a published figure.
      memory_sourced: "MEASURED-IN-USE on host anton 2026-09-03: peak VmHWM 9722760 kB during generation"
      memory_provenance: "measured-in-use:llama.cpp-b8681-cpu:vmhwm-peak:2026-09-03"
      memory_accounting_steady_state_mib: 8037
      memory_accounting_ceiling_with_repack_mib: 13043
      storage_sourced: "MEASURED: stat(1) on the downloaded GGUF, equal to the HF API declared size"
      storage_provenance: "measured:local-file-and-huggingface-blob-api:2026-09-03"
      kv_bytes_per_token: 163840
      context_architectural_max: 131072
      measured_throughput_note: "1.64 tok/s @ n_ctx=512, 0.87 tok/s @ n_ctx=4096; CPU-only, 4 threads, swap exhausted"
      weights_repo: bartowski/Mistral-Nemo-Instruct-2407-GGUF
      weights_file: Mistral-Nemo-Instruct-2407-Q4_K_M.gguf
      hf_revision: a2dd64a0a76ea1bdb2bb6ab6fa5496b003c7c908
      hf_gated: false
      upstream_model: mistralai/Mistral-Nemo-Instruct-2407
      digest_key: mistral-nemo-12b.q4_k_m.gguf.sha256
      measurement_record: specs/002-adaptive-local-model-serving/research/06-agent-lane-footprint-measurements.md
      catalogue_status: verified
```

### Why the deferred block cannot simply be uncommented

The deferred records are in an older shape than the live entries, and the loader is
strict (`KnownFields(true)`), so ONE unknown key refuses the WHOLE catalogue —
all 34 entries across all six files. Uncommenting record 18 as written would fail on
every one of: `description:`, `weight_source:` (the live key is `source:`),
a top-level `catalogue_status:` (lives under `annotations`), a per-entry
`streaming_roster:` map (entries may carry only a `streaming_family:` string —
an entry may never assert its own listedness), and `integrity.digest_key`
(lives under `annotations`). It also carries no `family:` key and a
`storage_required_bytes: null`, which decodes silently to 0 and is then refused
by `Entry.Validate`. The entry above is written in the live shape.

## Appendix B — the admission test, as applied

**APPLIED** in the same commit as
`internal/selection/open24_agent_lane_admission_test.go`.

One change from the version used during measurement: RED_MODE no longer reads a
pre-change copy of the catalogue from a scratch directory. It removes the entry
from the loaded set instead — reproducing at the DATA seam, since the defect WAS
the absence of that data. The guard now needs no path outside the repository and
keeps working wherever the suite runs.

```go
package selection_test

// OPEN-24: the agent lane's configured candidates were absent from the
// catalogue, so an operator holding working weights was REFUSED rather than
// served. That is a narrowing of what the product can do, not a tidy-up, and
// the refusal it produced said so in the worst possible way: "this host
// provides no catalogue-entry … more memory does not help;
// remedy=different-approach". The operator's host was fine. The catalogue was
// empty.
//
// mistral-nemo-12b is now a live entry, carrying a footprint that was MEASURED
// by running the weights rather than read off a vendor page — see
// specs/002-adaptive-local-model-serving/research/06-agent-lane-footprint-measurements.md.
// This is the guard that it stays offerable.
//
// POLARITY SWITCH (§11.4.115) — one source, two roles:
//
//	RED_MODE unset / RED_MODE=0 (DEFAULT, post-change) — standing regression
//	                 guard: the entry is OFFERED to an accelerator-bound agent
//	                 lane on a host that can run it.
//	RED_MODE=1     — reproduction of the pre-change catalogue, by removing the
//	                 entry from the loaded set: the model is absent, and the
//	                 lane can only refuse it for the reason OPEN-24 recorded.
//
// Honest boundary (§11.4.6). RED_MODE reproduces at the DATA seam — it drops
// the entry from a loaded catalogue rather than checking out the pre-change
// file — because the defect WAS the absence of that data. It needs no external
// fixture and no path outside the repository, so it keeps working wherever the
// suite runs.
//
// The host is a fixture, and only the host is. The figures the selection rules
// are fed are the measured ones, read from the shipped catalogue.

import (
	"os"
	"testing"
	"time"

	"github.com/HelixDevelopment/HelixLLM/internal/capability"
	"github.com/HelixDevelopment/HelixLLM/internal/capability/testdata/fixtures"
	"github.com/HelixDevelopment/HelixLLM/internal/catalogue"
	"github.com/HelixDevelopment/HelixLLM/internal/runtime"
	"github.com/HelixDevelopment/HelixLLM/internal/selection"
	"github.com/stretchr/testify/require"
)

// open24Model is the entry this guard is about.
const open24Model = "mistral-nemo-12b"

// These are the MEASURED figures. They are asserted, not merely read, because
// the whole point of OPEN-24 is that the number an operator is admitted on has
// to be one somebody produced by running the thing. A later edit that quietly
// restores the researched 7623566950 B — a published VRAM figure, 2.3 GiB
// BELOW the measured CPU footprint — would offer this model to hosts that then
// cannot run it, and would fail here.
const (
	open24MeasuredMemoryBytes  = uint64(9956106240)
	open24MeasuredStorageBytes = uint64(7477208192)
)

func open24RedMode() bool { return os.Getenv("RED_MODE") == "1" }

// open24Host has RAM and a card both comfortably above the measured figure.
// The card matters: every *-boot lane is AcceleratorBound, so the memory
// figure is checked against the device as well as against host RAM.
func open24Host() capability.HostCapabilityProfile {
	p := fixtures.SingleAccelerator()
	p.HostIdentity = "fixture-open24-can-run-nemo"
	p.MemoryTotal = 48 * capability.GiB
	p.MemoryAvailable = 40 * capability.GiB
	p.StorageAvailable = 500 * capability.GiB
	p.Accelerators = []capability.Accelerator{{
		Identity:        capability.DeviceIdentity("GPU-fixture-24gib-0000"),
		Model:           "fixture 24 GiB accelerator",
		API:             capability.APICUDA,
		MemoryTotal:     24 * capability.GiB,
		MemoryAvailable: 22 * capability.GiB,
	}}
	return p
}

func TestAgentLaneAdmitsTheMeasuredMistralNemo(t *testing.T) {
	loaded, err := catalogue.Load(shippedCatalogueDir)
	require.NoError(t, err)

	entries := loaded.Entries()
	var entry *catalogue.Entry
	for i := range entries {
		if entries[i].ModelID == open24Model {
			entry = &entries[i]
		}
	}

	if open24RedMode() {
		// The pre-change catalogue exactly: the entry is not in it.
		kept := entries[:0:0]
		for _, e := range entries {
			if e.ModelID != open24Model {
				kept = append(kept, e)
			}
		}
		entries, entry = kept, nil
	} else {
		require.NotNil(t, entry,
			"the measured entry has been removed from the shipped catalogue; "+
				"OPEN-24's narrowing is back and operators holding this GGUF are refused again")
	}

	res, err := selection.Select(selection.Request{
		Profile:          open24Host(),
		Entries:          entries,
		Families:         []catalogue.CapabilityFamily{catalogue.FamilyText},
		DeclaredUsage:    catalogue.UsageCommercial,
		Now:              time.Now().UTC(),
		MaxProfileAge:    time.Minute,
		Reserve:          runtime.SelectionReserve(),
		AcceleratorBound: true,
	})
	require.NoError(t, err)
	fr, ok := res.Family(catalogue.FamilyText)
	require.True(t, ok)

	var offered *selection.Option
	for i := range fr.Offered {
		if fr.Offered[i].ModelID == open24Model {
			offered = &fr.Offered[i]
		}
	}

	if open24RedMode() {
		require.Nil(t, offered,
			"RED: with the entry removed the lane must not be able to offer it")
		t.Logf("RED reproduced: %s is absent from the catalogue; the lane can only "+
			"refuse it as an unknown model, which is OPEN-24. %d other text options offered.",
			open24Model, len(fr.Offered))
		return
	}

	require.NotNil(t, offered,
		"GREEN: a host with 40 GiB free and a 22 GiB card can run a 9496 MiB model; "+
			"if it is not offered, admission has regressed")
	require.Equal(t, "q4_k_m", offered.Variant)
	require.Equal(t, open24MeasuredMemoryBytes, offered.Cost.MemoryRequiredBytes,
		"the figure the lane admits on must be the MEASURED one")
	require.Equal(t, open24MeasuredStorageBytes, offered.Cost.StorageRequiredBytes)

	require.NoError(t, entry.Validate())
	require.NoError(t, entry.ValidateForAcquisition(),
		"this entry's digest was verified against the bytes on disk, so unlike every "+
			"other entry here it must also pass the gate that precedes touching a weight file")

	t.Logf("GREEN: OFFERED %s memory=%dMiB storage=%dMiB source=%s",
		offered.Identity,
		offered.Cost.MemoryRequiredBytes/(1024*1024),
		offered.Cost.StorageRequiredBytes/(1024*1024),
		entry.Source)
}
```

---

## Follow-up, flagged and NOT started

The GPU situation changed while this measurement was running. `/usr/bin/llama-server`
is still the CPU-only Debian build, so **every figure above stands exactly as
taken** — but the RTX 3060 is now usable through a Vulkan llama.cpp build at
`~/opt/llamacpp_gpu/current`, reported at 37/37 layers offloaded and ~67-108 tok/s
against the 0.87-1.64 tok/s measured here on CPU.

Two questions that opens, neither of which is in scope for OPEN-24:

1. **Do GLM-4.7-Flash and DeepSeek-Coder-V2-Lite become measurable on 12 GiB of
   VRAM?** DeepSeek at 9.65 GiB plausibly fits; GLM at 17.06 GiB plausibly does
   not. Neither is answerable without trying, and DeepSeek would still be blocked
   on its unenumerated `license: other` regardless of the outcome.
2. **Does mistral-nemo-12b need a second, device-path figure?** The 9956106240 B
   recorded here is a host-RAM, CPU-backend measurement, and 4886 MiB of it is a
   `CPU_REPACK` buffer that has no meaning on a GPU. The agent lane is
   `AcceleratorBound` and spends this figure on the card, so a GPU-path
   measurement would very likely be materially different — and lower. That is the
   OPEN-25 shape, and it wants fixing as its own change rather than as a second
   number bolted onto this entry.
