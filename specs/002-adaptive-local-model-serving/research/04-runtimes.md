# Execution-runtime research: llama.cpp vs. Colibri (disk-streaming)

Input for `/speckit-plan` on spec 002 (adaptive local model serving), covering the execution-path
requirements FR-026..FR-028 ("execution paths" in the current spec numbering — FR-021..024 covers
discovery in this revision; this file researches whichever FRs govern the in-memory-vs-streaming
path selection, per the task brief). Confirms and extends the spec's Assumptions section, which
already states the verified fact that Colibri is **not** a general-purpose llama.cpp replacement.

## 0. The one fact this whole file is built on (do not re-litigate)

> The disk-streaming runtime under consideration is **not** a general-purpose replacement for the
> existing in-memory runtime. It is specialised: it streams mixture-of-experts weights from disk to
> run models that would not otherwise fit in memory, trading speed for feasibility, and it does not
> serve arbitrary models. The choice between paths is therefore **not** a symmetric preference
> between interchangeable engines — it is "in-memory when the model fits, streaming when it otherwise
> could not run at all."
> — spec.md, Assumptions, verified externally 2026-09-02

Everything below sharpens that statement into an implementable decision procedure. It does not
contradict it.

---

## 1. Colibri, in depth

**Repository:** <https://github.com/JustVugg/colibri> — "Run frontier MoE models on hardware you
already own — pure C, zero deps, experts streamed from disk. Tiny engine, immense model."
Project site: <https://justvugg.github.io/colibri/>. License: Apache 2.0. Version at research time:
**v1.10.1**.

### 1.1 What it is, mechanically

Colibri is **not** a GGUF-compatible general-purpose loader. It is a specialised inference engine
for a **closed, named list** of Mixture-of-Experts (MoE) model families. Its own framing: "A 744B
Mixture-of-Experts model activates only ~40B parameters per token — and only ~11 GB of those change
from token to token (the routed experts)." It keeps the model's **dense layers resident in RAM**
and **streams the routed-expert weights from disk on demand**, through a multi-tier cache with
prefetch and learning-based hotspot pinning (a per-layer LRU-style cache, per independent
corroboration — see Sources). This is why it can run a 744B-parameter model with as little as
16–25 GB of RAM: it never loads the full weight set into fast memory, only the dense backbone plus
whichever experts are hot.

The project's own explicit non-goal, quoted from the project site: **"Not supported: Arbitrary GGUF
models, dense-only architectures. Scope: Mixture-of-Experts models only."** This is the load-bearing
negative-space fact for this spec: Colibri cannot serve a dense model (a plain Llama/Mistral/Qwen
dense checkpoint) at all, streaming or otherwise, and it cannot serve an MoE model outside its
named list without upstream work to add support.

### 1.2 Model architectures supported TODAY (verbatim list, cross-verified across 3 independent
fetches: the repo README, the project site, and an independent web summary — all agree)

| Family | Total params | Active params/token | Notes |
|---|---|---|---|
| GLM-5.2 | 744B | ~40B | Int4 quantised, "token-exact reference implementation" |
| GLM-5.3-Flash | 321B | ~40B | Includes a vision tower |
| Inkling | 975B | ~41B | Reasoning model; audio tower; mixed-precision staging |
| Kimi K3 | 2.8T | ~104B | Largest supported family; gated recurrence + MLA attention |
| DeepSeek V4 Flash | 284B | ~13B | Sparse attention, multi-token prediction; **no grammar
  support**; tool calling goes through an HTTP gateway using the native prompt format, not
  constrained decoding |
| Qwen3.8-Flash-Next | 125B (+51B n-gram component) | ~6B | Tool calling and vision tower verified;
  **CPU-only — GPU is explicitly "Not supported" for this family**, unlike the others |
| Qwen3.6 | 35B | ~3B | Gated attention + Gated DeltaNet |
| OLMoE | 7B | ~1B | Smallest supported family, research-focused |

**Nothing outside this list of eight names is servable by Colibri**, regardless of hardware. A spec
that offers Colibri as "the streaming option" for an arbitrary large MoE model not on this list would
be offering an option that will fail — the exact failure mode FR-028 forbids.

### 1.3 Build requirements

- Pure C, **zero runtime dependencies**. Build is `git clone` + `make -C colibri/c`. Compiler is GCC
  or Clang with OpenMP.
- **No GPU is required** for any of the eight families except that GPU acceleration is available as
  an *optional* tier for most of them (CUDA primary, Metal for Apple Silicon, Vulkan documented but
  "limited documentation" per independent summary). Qwen3.8-Flash-Next is the one family that is
  **CPU-only by design** — GPU is explicitly stated as not supported for it.
- Runs identically across CPU-only, CPU+GPU-partial, and (for most families) full-GPU-residency
  configurations — the same C binary, differing only in how much of the working set the operator's
  hardware lets it keep resident vs. streamed.

### 1.4 Disk / RAM profile (published, per-model)

| Model | Disk footprint | Minimum RAM | GPU |
|---|---|---|---|
| GLM-5.2 | ~372 GB | 16 GB min / 24 GB comfortable | optional |
| GLM-5.3-Flash | ~195 GB | 25 GB | optional |
| Inkling | ~469 GB | 25 GB (int4 dense) | optional |
| Kimi K3 | ~1.6 TB | 32 GB+ | optional |
| DeepSeek V4 Flash | ~167 GB | 16 GB min / 32 GB comfortable | optional |
| Qwen3.8-Flash-Next | ~185.5 GB | 16 GB min / 24 GB comfortable | **not supported** |
| Qwen3.6 | ~20 GB | 24 GB — **full residency required, no streaming benefit at this size** | optional |
| OLMoE | ~7 GB | 8 GB | optional |

Two things worth flagging for the decision procedure:

- **The disk footprint is the whole quantised model**, not a fraction — Colibri still needs the full
  weight set to exist somewhere it can seek into (local NVMe strongly implied by the throughput
  numbers below; the project does not claim to stream over a network transport for these numbers).
  Free-disk headroom must be checked against this table, not against RAM.
- **Qwen3.6 at ~20 GB / 24 GB RAM is effectively "full residency required"** by Colibri's own
  documentation — i.e. for the smallest two families (Qwen3.6, OLMoE) the disk-streaming machinery
  buys little or nothing over just fitting the model in RAM directly; streaming exists to make the
  744B–2.8T end of the table possible at all, not to shave RAM off small models.

### 1.5 Measured throughput (UNVERIFIED beyond the project's own self-reported numbers — no
independent third-party benchmark reproduction was found)

From the project site's hardware table (13,260 characterised experts across community-submitted
hardware, per the project's own claim):

| Hardware | Model | Throughput |
|---|---|---|
| 6× RTX 5090 + NUMA, full VRAM residency | all families | 9.0–9.2 tok/s decode |
| AWS Graviton4 (64-core), 512 GB RAM, CPU-only | GLM-5.2 | 8.0 tok/s |
| 2× Xeon Gold 6430, 1 TB DDR5, CPU-only | GLM-5.2 | 5.42 tok/s |
| MacBook Pro M5 Max, 128 GB, Metal | GLM-5.2 | 2.0 tok/s |
| Ryzen 9 9950X3D + RTX 5090, Gen5 NVMe streaming | GLM-5.2 | 1.23 tok/s |
| Entry-level 25 GB dev box, cold NVMe streaming | GLM-5.2 | 0.05–0.1 tok/s |
| RTX 5080 + 2× NVMe | DeepSeek V4 Flash | ~1.6 tok/s decode at 3k context |

**UNVERIFIED: these are Colibri's own self-reported numbers; treat as directional, not a guarantee.**
The spread is enormous (0.05 tok/s to 9.2 tok/s on the *same model*) and is dominated by how much of
the working set is resident vs. streamed and by storage speed — this is the direct evidence for
FR-027's "label the speed trade-off" requirement: the same model on the same architecture can be
15–180× slower depending purely on how cold the cache is and how fast the NVMe is. A spec surfacing
"model X is available via streaming" without also surfacing the expected throughput band would be
misleading by omission.

### 1.6 Maturity and limitations

- v1.10.1, described by its own author as built "in six weeks, in the open"; project claims
  25,157 stars and 100+ contributors, 510 merged PRs (per project site — **UNVERIFIED**, not
  independently confirmed against GitHub's live counters at research time).
- **No SLA on speed, hard guarantee on semantics** — the project's explicit policy is that
  insufficient fast memory degrades throughput but never silently changes model precision or router
  behaviour ("speed never buys drift" — outputs are validated token-exact against a reference
  implementation).
- Speculative decoding (MTP drafting) exists for at least one family but **defaults to disabled**
  unless acceptance rate repays the verification cost — do not assume it is active.
- Grammar-constrained generation is **not supported** for DeepSeek V4 Flash specifically; tool
  calling for that family is bridged through an HTTP gateway using the native prompt format rather
  than constrained decoding.
- The project is young (single-author-led, six-week build, v1.x) — this is a maturity/support-risk
  factor for a production spec, independent of the technical capability claims above.

---

## 2. llama.cpp, current state

**Repository:** <https://github.com/ggml-org/llama.cpp>. General-purpose, actively maintained,
multi-year project — the dominant local-inference runtime and the format-definer for GGUF.

### 2.1 Hardware backends (verified against the live README, 2026-09-02)

GPU/accelerator backends: **CUDA** (NVIDIA), **Metal** (Apple Silicon), **HIP/ROCm** (AMD), **Vulkan**
(cross-platform, including on GPUs without vendor-specific backends), **SYCL** (Intel GPUs), **MUSA**
(Moore Threads), **WebGPU**, **OpenCL** (Adreno). Specialised-processor backends: **CANN** (Ascend
NPU), **IBM zDNN** (Z/LinuxONE) — plus Hexagon (Snapdragon) and OpenVINO listed as "in progress."
CPU-level acceleration: BLAS/BLIS, ARM NEON + Accelerate (Apple CPU path), AVX/AVX2/AVX512/AMX (x86),
RVV/ZVFH/ZFH (RISC-V). One codebase builds for all of these; CPU-only is always a fallback path with
no hard dependency on any accelerator.

### 2.2 Quantisation formats

GGUF (llama.cpp's own container format, now the de facto standard the rest of the local-inference
ecosystem also targets) supports quantisation from **~1.5-bit through 8-bit integer**, exposed as the
named K-quant family (Q2_K, Q3_K, Q4_K_M, Q5_K_M, Q6_K, Q8_0, etc.) plus the newer importance-matrix
IQ-series (IQ1_*, IQ2_*, IQ3_*, IQ4_*) for more aggressive compression with better quality retention
than naive low-bit quantisation. The quantisation level chosen for a given model is the single
biggest lever on whether that model's on-disk/in-memory footprint fits a given host — the same model
can range roughly 4× in size between Q8_0 and the smallest IQ1 variant, at a real, non-linear quality
cost as bit-width drops.

### 2.3 What determines whether a model fits a host, in llama.cpp

llama.cpp does **not** require the whole model to fit in VRAM. It supports:

1. **Full GPU residency** — all layers + KV cache on the accelerator. Fastest, requires
   VRAM ≥ quantised-weights-size + KV-cache-size + activation overhead.
2. **Partial offload** (`--n-gpu-layers`) — some transformer layers on the accelerator, the rest on
   CPU/RAM. Works whenever VRAM + system RAM together exceed the model's footprint; throughput
   degrades roughly in proportion to the fraction pushed to CPU.
3. **CPU-only** — the whole model in system RAM, no accelerator at all. Slowest of the three tiers,
   but a real, supported, first-class mode — not a degraded fallback the project discourages.

The hard failure boundary is: **quantised model size + KV cache for the requested context length
must fit within (VRAM + system RAM) at some accelerator/CPU split, or llama.cpp cannot serve it at
all on that host** (short of relying on OS-level swap/mmap thrashing, which is not a supported
operating mode and is exactly the kind of "will fail" outcome FR-028 says must not be offered).
llama.cpp performs no disk-streaming of weights during inference; the model file is either mapped
into RAM/VRAM in full (mmap can lazily page it in, but there is no working-set-aware eviction and
re-streaming design analogous to Colibri's — this is a genuine capability gap, not an
implementation detail).

### 2.4 Model architectures supported

Because GGUF conversion scripts exist for essentially every openly-released dense and MoE
architecture the community maintains converters for (Llama family, Mistral/Mixtral, Qwen dense and
MoE variants, Gemma, Phi, DeepSeek dense and MoE, GLM, and hundreds more), llama.cpp is, for
practical purposes, **general-purpose across architectures** in a way Colibri explicitly is not —
its constraint is *memory footprint*, not *which named model family this is*. This is the structural
opposite of Colibri's constraint shape (fixed architecture list, flexible footprint via streaming).

---

## 3. The decision procedure

### 3.1 Inputs

**Host, measured (not asked of the user — this spec's other requirements already establish hardware
profiling exists and must be reused, per the Assumptions section):**

- `accelerator_present: bool`, `accelerator_type: {cuda|metal|rocm|vulkan|none}`
- `usable_vram_gb: float` (VRAM minus the accelerator's own reserved/driver overhead)
- `system_ram_gb: float` (physical RAM minus OS + already-running-process floor)
- `free_disk_gb: float`, and ideally `disk_class: {nvme|sata_ssd|hdd|network}` — Colibri's own
  numbers show over an order-of-magnitude throughput difference driven by storage class, so this
  must be tracked even though it is not a hard go/no-go gate on its own

**Candidate model:**

- `architecture_family: string` (e.g. "GLM-5.2", "Llama-3.x-70B", "DeepSeek V4 Flash")
- `total_params`, `active_params_per_token` (active == total for dense models)
- `is_moe: bool`
- `quant_level` (the specific GGUF quant, or Colibri's fixed int4 for its own catalogue)
- `weights_size_on_disk_gb` at the chosen quant level
- `kv_cache_gb_estimate` for the requested context length (model- and quant-dependent; computed
  separately, not derived here)

### 3.2 Procedure

```
1. LLAMA_CPP_FIT?
   fast_memory_gb = usable_vram_gb + system_ram_gb
   required_gb    = weights_size_on_disk_gb + kv_cache_gb_estimate
                     + overhead_margin (recommend 10-15% of weights_size_on_disk_gb)
   if required_gb <= fast_memory_gb:
       -> SERVE via llama.cpp, in-memory path (FR-026: prefer this whenever it fits)
       -> placement: full GPU residency if weights_size_on_disk_gb + kv_cache_gb_estimate
          <= usable_vram_gb, else partial offload split across VRAM/RAM, else CPU-only
       -> STOP (do not even consider Colibri: it is not a symmetric alternative, and using it
          when the model already fits in memory only adds streaming latency for no benefit —
          Colibri's own numbers show its "full residency" configurations already just collapse
          to the same regime as llama.cpp's in-memory path, at greater engineering/runtime cost)

2. COLIBRI_ELIGIBLE?  (only reached if step 1 is false)
   if architecture_family not in COLIBRI_SUPPORTED_FAMILIES:      # the 8-name closed list, §1.2
       -> NOT SERVABLE VIA COLIBRI (wrong reason: architecture, not resources)
       -> go to step 3
   if not is_moe:
       -> NOT SERVABLE VIA COLIBRI (Colibri is MoE-only by design, §1.1)
       -> go to step 3
   if system_ram_gb < COLIBRI_MIN_RAM_GB[architecture_family]:     # per §1.4 table
       -> NOT SERVABLE VIA COLIBRI (insufficient RAM even for the dense/resident portion)
       -> go to step 3
   if free_disk_gb < COLIBRI_DISK_FOOTPRINT_GB[architecture_family]:  # per §1.4 table
       -> NOT SERVABLE VIA COLIBRI (insufficient disk for the full quantised weight set)
       -> go to step 3
   if architecture_family == "Qwen3.8-Flash-Next" and accelerator_present and
      <request assumes GPU accel>:
       -> NOTE: this family is explicitly CPU-only in Colibri; GPU presence does not help it
   -> SERVE via Colibri, disk-streaming path (FR-027)
   -> MUST label with a throughput expectation drawn from §1.5's measured range for comparable
      hardware/storage class, not a single point number, and MUST NOT imply parity with the
      in-memory path
   -> STOP

3. NEITHER CAN SERVE
   -> state plainly that this model cannot be served by any available path on this host (FR-028)
   -> the reason MUST be surfaced (architecture unsupported by streaming vs. genuinely insufficient
      resources on every path) rather than a bare "unavailable", because the two failure reasons
      have different remedies (waiting for upstream Colibri support vs. adding RAM/disk/GPU)
```

### 3.3 Worked examples

| Model | Host | Step-1 fit? | Step-2 result | Outcome |
|---|---|---|---|---|
| Llama-3.x-70B, Q4_K_M (~40 GB) | 24 GB VRAM + 64 GB RAM | 40 GB ≤ 88 GB → yes | n/a | llama.cpp, partial GPU offload |
| GLM-5.2 (Colibri catalogue, 744B/~40B active, 372 GB disk) | 24 GB VRAM + 32 GB RAM (56 GB fast mem) | 372 GB ≤ 56 GB → **no** | family ✓, MoE ✓, RAM 32≥16 ✓, disk ≥372GB? if yes → yes | Colibri, streaming, labelled ~1–2 tok/s class per §1.5 |
| GLM-5.2 same host, but only 200 GB free disk | as above | no | disk 200 < 372 → **fails disk check** | neither path — state plainly, reason: insufficient disk |
| A 900B MoE model not in Colibri's 8 families, no host can fit it in RAM+VRAM | any | no | architecture not in list → **fails architecture check** | neither path — state plainly, reason: no streaming support for this architecture (not a resource problem) |
| Qwen3.6 (Colibri catalogue, 35B/~3B active, ~20 GB disk) | 8 GB VRAM + 16 GB RAM (24 GB fast mem) | weights ~20GB + kv fits in 24GB fast mem → likely **yes** | n/a | llama.cpp in-memory — this is the case from §1.4 where Colibri's own docs say "full residency required" anyway, so the in-memory path was the right call even though the model is technically in Colibri's catalogue |

That last row is important: **catalogue membership alone never routes to Colibri.** The procedure
always tries the in-memory path first regardless of whether the model happens to be one Colibri also
supports, because FR-026 mandates preferring in-memory whenever it fits, and Colibri's own numbers
show no benefit (only added latency and engineering surface) when it does.

---

## 4. What each runtime cannot do (negative space)

**Colibri cannot:**
- Serve any model outside its 8 named families, at any hardware tier, streaming or not.
- Serve a dense (non-MoE) model at all — by explicit statement of scope.
- Give GPU acceleration to Qwen3.8-Flash-Next specifically (CPU-only for that one family).
- Support grammar-constrained decoding for DeepSeek V4 Flash.
- Guarantee any particular throughput — its own docs disclaim an SLA on speed; the measured range
  spans ~180× depending on cache warmth and storage class.
- Be assumed production-hardened at the level of a multi-year project — it is six weeks old at
  v1.10.1, single-author-led per its own framing.

**llama.cpp cannot:**
- Serve a model whose quantised size plus KV cache exceeds VRAM+RAM combined on the host — there is
  no supported disk-streaming-of-weights mode; mmap-based lazy paging is not a substitute (no
  working-set-aware eviction/prefetch, and relying on it risks OS thrashing rather than a bounded,
  labelled speed trade-off).
- Turn a 744B–2.8T-class model into something a 16–32 GB-RAM host can run at all — that is precisely
  the gap Colibri exists to fill for its eight families.

This is the concrete meaning of "not a symmetric preference between interchangeable engines" from
the spec's Assumptions: llama.cpp's constraint is footprint-vs-fast-memory across (almost) any
architecture; Colibri's constraint is a fixed architecture list, in exchange for a footprint ceiling
that is instead bounded by disk, not fast memory, for MoE models specifically inside that list.

---

## 5. Are there other serious contenders?

Short answer: **for this spec's two-tier problem (fits in fast memory / doesn't but is a supported
large MoE), llama.cpp and Colibri genuinely do cover the space** — but two adjacent projects are
worth recording so a future planning pass does not have to re-discover them:

- **KTransformers** (`kvcache-ai/ktransformers`) — a heterogeneous CPU+GPU inference framework that
  keeps attention/KV-cache on a single consumer GPU (as little as 24 GB) and offloads MoE experts to
  system RAM, demonstrated running DeepSeek-R1-class 671B models. This is **RAM-offload, not
  disk-streaming** — its ceiling is system RAM size, not disk size, so it does not extend reach the
  way Colibri's disk-tier does; it optimizes a different point (GPU-scarce, RAM-rich hosts). Not
  currently proposed for wiring into this spec; recorded as adjacent prior art.
- **PowerInfer** (research project, `arxiv 2312.12456`) — hot/cold neuron-locality-based CPU/GPU
  hybrid inference; predates the current generation of frontier MoE models used by Colibri's
  catalogue, and is best characterised as the research lineage Colibri's approach descends from
  rather than a deployable competitor today.

Neither offers a materially different answer to FR-026..028 than the llama.cpp/Colibri pair already
does for the models named in Colibri's catalogue, and neither claims to be a general-purpose
GGUF-server replacement either. **UNVERIFIED:** whether either project has matured into a
production-ready option since the sources below were captured; treat both as "known, not adopted"
rather than "ruled out."

---

## Sources

- Colibri repository (README, fetched live) — <https://github.com/JustVugg/colibri> — accessed 2026-09-02
- Colibri raw README — <https://raw.githubusercontent.com/JustVugg/colibri/main/README.md> — accessed 2026-09-02
- Colibri project site (technical specs page) — <https://justvugg.github.io/colibri/> — accessed 2026-09-02
- Web search corroboration of Colibri's 8 supported families and architecture (GitHub listing,
  daily.dev repost, gitgem.org mirror, project site) — accessed 2026-09-02
- Colibri capability analysis (cited in spec.md Sources) — <https://wavect.io/blog/colibri-glm-5-2-consumer-hardware/>
- Colibri model/memory characteristics (cited in spec.md Sources) — <https://pasqualepillitteri.it/en/news/7923/colibri-glm-5-2-744b-25gb-ram-en>
- llama.cpp repository (README, fetched live) — <https://github.com/ggml-org/llama.cpp> — accessed 2026-09-02
- Web search corroboration of llama.cpp backend/quantisation state, 2026 — accessed 2026-09-02
- Web search on adjacent MoE-offload projects (KTransformers, PowerInfer) — accessed 2026-09-02,
  including <https://github.com/kvcache-ai/ktransformers> and <https://arxiv.org/pdf/2312.12456>

**Note on verification depth:** this file relies on WebFetch's page-summarisation of live GitHub/
project-site content rather than a byte-for-byte manual read of the raw README, because the tooling
available in this session summarises rather than returns raw bytes. Three independent fetches
(repo README, project site, and an independent web-search synthesis) agree on the 8-family list,
the MoE-only/no-arbitrary-GGUF scope statement, and the per-model RAM/disk table, which is the load-
bearing content for this spec's decision procedure — treat that convergence as the verification.
Anything marked **UNVERIFIED** above (throughput numbers, star/contributor counts, maturity claims)
rests on Colibri's own self-reported figures with no independent third-party reproduction found.
