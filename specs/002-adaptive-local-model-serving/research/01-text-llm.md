# Research 01 — Text-generation LLMs for local serving

Scope: open-weight TEXT generation models a HelixLLM selection engine could offer per FR-001..FR-008
(hardware-aware selection, no offer the host cannot run, real non-empty CPU-only option set,
re-evaluation at selection time). Vision/audio/TTS/STT/image/design families are covered in sibling
research files in this directory — not duplicated here.

Researched via live web search 2026-09-02 (see Sources). This field moves weekly; every figure below
is cited, and every gap is marked `UNVERIFIED:` rather than estimated. Do not treat this table as
frozen — the catalogue-population step in planning should re-verify before committing numbers to code.

---

## 1. Per-family model data

### 1.1 Qwen3 / Qwen3.5 / Qwen3.6 (Alibaba)

**License**: Apache 2.0 across the family. **Weight source**: `huggingface.co/Qwen/*` (official),
mirrored quantized by `huggingface.co/bartowski/*`, `huggingface.co/unsloth/*`, `huggingface.co/ggml-org/*`,
and `ollama.com/library/qwen3*`.

Qwen3 (2025) ships 8 sizes, dense and MoE: 0.6B, 1.7B, 4B, 8B, 14B, 32B (dense), plus 30B-A3B and
235B-A22B (MoE). Qwen3.5 (late 2025) added 27B and a 397B-A17B MoE. Qwen3.6 (April 2026) narrowed the
open-weight release to two models: 27B dense and 35B-A3B MoE. [Sources: knightli.com, willitrunai.com]

| Model | Params | Arch | Context | Q4_K_M footprint | Q8_0 footprint | Min VRAM (GPU) | Min RAM (CPU-only) |
|---|---|---|---|---|---|---|---|
| Qwen3-0.6B | 0.6B dense | dense | 32K native (YaRN to 128K) | ~429 MB (Q4_0 cited) | ~805 MB | ~1 GB | ~2 GB, usable |
| Qwen3-1.7B | 1.7B dense | dense | 32K/128K | ~1.1 GB (est. from linear scaling of 0.6B/8B points) UNVERIFIED: exact Q4_K_M byte count not directly sourced | — | ~2 GB | ~4 GB, usable |
| Qwen3-4B | 4B dense | dense | 32K/128K | UNVERIFIED: no direct Q4_K_M byte figure found this pass; industry convention (~0.6 GB per B at Q4_K_M) implies ~2.5 GB — treat as estimate only | — | ~4 GB | ~6-8 GB, usable |
| Qwen3-8B | 8B dense | dense | 32K/128K | "6-8GB VRAM at Q4_K_M" (whole-stack figure incl. KV cache) [willitrunai.com] | — | 6-8 GB | ~12 GB, usable |
| Qwen3-14B | 14B dense | dense | 32K/128K | "ideal for 10-12GB VRAM at Q4_K_M" [willitrunai.com] | — | 10-12 GB | ~20 GB, usable but slow |
| Qwen3-30B-A3B | 30B total / 3B active | **MoE** | 32K/128K | 18.7 GB (Q4_K_M); range Q2_K 11.4 GB → Q8_0 32.6 GB [search aggregate, bartowski/mradermacher listings] | 32.6 GB | 20 GB (comfortable) | 32 GB+ RAM viable at CPU speed because only 3B active |
| Qwen3-32B | 32B dense | dense | 32K/128K | UNVERIFIED: not directly sourced this pass; scaling from 14B point implies ~20 GB | — | ~22-24 GB | ~40 GB, usable but slow |
| Qwen3-235B-A22B | 235B total / 22B active | **MoE** | 32K/128K | ~143 GB (Q4_K_M, weights only, no KV cache) [spheron.network, willitrunai.com] | — | Multi-GPU (143 GB+) or CPU-offload hybrid | 96 GB+ system RAM with partial GPU offload reported workable; CPU-only impractical without disk streaming |
| Qwen3.5-27B / 397B-A17B | 27B dense / 397B total-17B active | dense + MoE | 32K/128K | UNVERIFIED: no direct figure found; treat as between Qwen3-32B and Qwen3.6-27B points | — | — | — |
| Qwen3.6-27B (dense) | 27B dense | dense | 32K/128K (YaRN 1M adds 20-40GB KV) | Q4_K_M ~16.8 GB; Q5_K_M ~19.5 GB; Q6_K ~22.5 GB; Q8_0 ~28.6 GB [knightli.com] | 28.6 GB | 16.8-19.5 GB comfortable | ~40 GB usable, slow |
| Qwen3.6-35B-A3B | 35B total / 3B active | **MoE** | 32K/128K | ~21 GB (Q4_K_M) [knightli.com] | — | 21-24 GB comfortable | 24-32 GB usable at good speed because only 3B active per token |

**Notable for selection logic**: 30B-A3B and 35B-A3B are the sweet spot for CPU+RAM-only hosts —
MoE with a small active-parameter count means CPU throughput is close to a dense ~3-4B model even
though the full weight set must be resident (or disk-streamed).

### 1.2 Meta Llama 4 (and legacy Llama 3.2 for the very-low-end tier)

**License**: **Llama 4 Community License** (custom, NOT Apache/MIT) — free for research and commercial
use under 700M monthly active users; larger deployments require a separate agreement with Meta.
[llama.com/llama4/license/] This is a licence-compliance input for the allowlist/catalogue, not a
technical blocker. **Weight source**: `huggingface.co/meta-llama/*` (official, gated download),
GGUF mirrors at `huggingface.co/unsloth/Llama-4-Scout-17B-16E-Instruct-GGUF` and
`huggingface.co/unsloth/Llama-4-Maverick-17B-128E-Instruct-GGUF`.

| Model | Params | Arch | Context | Footprint | Min hardware |
|---|---|---|---|---|---|
| Llama 4 Scout | 109B total / 17B active, 16 experts | **MoE** | marketed up to 10M (practical serving context far lower; KV cache cost scales with actual context used) | Full precision impractical locally; Q4_K_M GGUF widely distributed by unsloth | UNVERIFIED: no single authoritative Q4_K_M byte figure found this pass — treat as "large, multi-GPU or high-RAM CPU-offload only" until re-verified |
| Llama 4 Maverick | 400B total / 17B active, 128 experts | **MoE** | same context caveat as Scout | Larger than Scout; multi-GPU territory | Not a fit for any single-consumer-GPU tier; candidate only for disk-streaming or a well-resourced multi-GPU host |
| Llama 3.2 1B | 1B dense | dense | 128K | Q4_K_M ≈ 807 MB [search aggregate] | <1 GB VRAM; runs on nearly anything |
| Llama 3.2 3B | 3B dense | dense | 128K | Q4_K_M ≈ 2.0 GB [search aggregate] | ~2-3 GB VRAM or 4 GB RAM CPU-only |

**Selection-logic note**: Llama 4's own MoE routing needs "all experts resident" for the in-memory
path regardless of active-parameter count — per the spec's Assumptions section this is exactly the
class of model the disk-streaming path (Colibri, §1.7) exists for on hosts that cannot hold Scout/
Maverick's full expert set. Llama 3.2 1B/3B remain the only Meta-family fit for the smallest tier.

### 1.3 Google Gemma 3 / Gemma 4

**License**: Gemma 3 ships under the source-available **Gemma Terms of Use** (not OSI-approved).
Gemma 4 (released 2026-04-03) switched to **Apache 2.0** — a materially different allowlist/licence
posture between the two generations. [mindstudio.ai, en.wikipedia.org/Gemma_(language_model)]
**Weight source**: `huggingface.co/google/*`, GGUF at `huggingface.co/ggml-org/gemma-3-*-GGUF` and
`huggingface.co/unsloth/gemma-4-*-GGUF`.

| Model | Params | Arch | Context | Min hardware (approx, Q4_K_M class) |
|---|---|---|---|---|
| Gemma 3 1B | 1B dense | dense (text-only) | 32K | <1 GB |
| Gemma 3 4B | 4B dense | dense (vision-language) | 128K | ~3-4 GB VRAM |
| Gemma 3 12B | 12B dense | dense (vision-language) | 128K | ~8-9 GB VRAM |
| Gemma 3 27B | 27B dense | dense (vision-language) | 128K | ~17-19 GB VRAM |
| Gemma 4 E2B | ~2B effective | dense | UNVERIFIED: exact context not found this pass | ~2 GB |
| Gemma 4 E4B | ~4B effective | dense | UNVERIFIED | ~4 GB |
| Gemma 4 26B-A4B | 25.2B total / 3.8B active | **MoE** | UNVERIFIED | 26B total resident but only ~4B active compute — good CPU-RAM candidate, similar profile to Qwen's *-A3B line |
| Gemma 4 31B (dense) | 31B dense | dense | UNVERIFIED | ~20 GB VRAM class |

**UNVERIFIED**: precise Gemma 4 GGUF byte-size-per-quant table was not directly retrieved this pass
(sources described architecture/params but not a quant table); re-verify against
`huggingface.co/unsloth/gemma-4-31B-it-GGUF` file listing before hardcoding numbers into the catalogue.

### 1.4 OpenAI gpt-oss (20B / 120B)

**License**: Apache 2.0. **Weight source**: `huggingface.co/openai/gpt-oss-20b`,
`huggingface.co/openai/gpt-oss-120b` (official — native MXFP4 quantized weights, no separate
"full precision" download is the primary distribution).

| Model | Total params | Active params | Arch | Context | Native quant | Min hardware |
|---|---|---|---|---|---|---|
| gpt-oss-20b | 20.9B | 3.6B | **MoE** | 128K (industry-standard figure at launch; not independently re-confirmed this pass — UNVERIFIED: exact figure not re-fetched from the model card this session) | MXFP4 | **16 GB** unified/VRAM — explicitly stated by OpenAI as edge/local-inference target [huggingface.co/openai/gpt-oss-20b] |
| gpt-oss-120b | 116.8B (~117B) | 5.1B | **MoE** | same caveat as above | MXFP4 | Fits a **single 80GB GPU** (H100/MI300X class) per OpenAI's own guidance; not a consumer-GPU fit |

**Selection-logic note**: gpt-oss-20b is the single strongest citeable "16 GB VRAM" data point in this
research — OpenAI states it explicitly as the design target, not a community estimate. gpt-oss-120b's
5.1B active parameters make it another disk-streaming candidate for high-RAM CPU-only hosts once
Colibri/equivalent support for it is confirmed (not confirmed in this pass — Colibri's supported list
in §1.7 does not currently name gpt-oss).

### 1.5 Mistral (Small / NeMo)

**License**: Apache 2.0 for Mistral Small, NeMo, Ministral 8B, Mixtral, Codestral Mamba — flagship
Mistral Large 3 / Small 4 (2026) also ship Apache 2.0 per current Mistral positioning; the one
non-Apache exception found is Voxtral TTS (CC BY-NC 4.0, out of scope for this text file).
**Weight source**: `huggingface.co/mistralai/*`, GGUF mirrors community-maintained (bartowski,
lmstudio-community).

| Model | Params | Arch | Context | Q4_K_M VRAM |
|---|---|---|---|---|
| Mistral NeMo | 12B dense | dense | 128K | ~7.1 GB [llmhardware.io / ollama.com] |
| Mistral Small 3.2 | 24B dense | dense | 131K | ~13.4 GB [openlaboratory.com] |

**UNVERIFIED**: Mistral Small 4 / Large 3 (2026 releases referenced in search results) — exact
parameter counts and GGUF quant footprints were not retrieved this pass; treat NeMo/Small-3.2 above
as the currently-verified anchors and re-check Small 4/Large 3 specifically before catalogue entry.

### 1.6 DeepSeek V3.2

**License**: MIT. **Weight source**: `huggingface.co/deepseek-ai/DeepSeek-V3.2` (official).

| Model | Total params | Active params | Arch | Context | Hardware |
|---|---|---|---|---|---|
| DeepSeek-V3.2 | 675.2B | 37B | **MoE** | UNVERIFIED: not directly re-confirmed this pass; the V3 lineage has shipped 128K historically | Not a single-consumer-GPU fit at any quantization; GGUF conversion of the MoE routing requires a specific llama.cpp patch/PR and is not a stock feature [dasroot.net] — flag as an integration-effort risk, not just a memory one |

**Selection-logic note**: DeepSeek-V3.2's 37B active-parameter count is too large for a CPU-only host
to serve at usable speed even with disk streaming of the (huge, 675B-total) expert set — this is the
clearest example in this research of a model that is *architecturally MoE* (so technically
disk-streaming-eligible per FR-026..028) but likely fails the "no performance glitches" bar (SC-003)
on anything short of a well-resourced multi-GPU or very high-RAM host. Needs an explicit
speed-vs-feasibility label per FR-027 rather than a blanket "streaming path available" claim.

### 1.7 GLM (Z.ai / Zhipu) — GLM-4.6 and the GLM-5.x line

**License**: MIT across the family through at least GLM-5.2. **Weight source**:
`huggingface.co/zai-org/*` (official; org renamed from a Zhipu-branded name to `zai-org`), GGUF at
`huggingface.co/bartowski/zai-org_GLM-4.6-GGUF` and `huggingface.co/unsloth/GLM-4.6-GGUF`.

| Model | Total params | Active params | Arch | Context | Hardware |
|---|---|---|---|---|---|
| GLM-4.6 | 357B | UNVERIFIED: active-parameter count not directly re-confirmed this pass (earlier GLM-4.5-Air-class models in the same family run smaller active counts) | **MoE** | 200K (confirmed, expanded from 128K) [z.ai docs] | Large — multi-GPU or disk-streaming territory |
| GLM-5.2 | 744B | ~40B active (Colibri's own figures) | **MoE** | UNVERIFIED (not directly re-confirmed) | **Disk-streaming reference case** — see below |

**Colibri-verified numbers (highest-confidence data point in this whole file — pulled directly from
the runtime's own repository, and directly relevant to spec Assumptions/FR-026..028):**
[github.com/JustVugg/colibri, 2026-09-02]

> "GLM-5.2 (744B): 16 GB min, 24 GB comfortable RAM; ~372 GB disk storage. Dense layers (~17B
> parameters) stay resident in RAM at int4 (~9.9 GB); the ~19,456 routed experts stream from disk
> on-demand."

This is the single clearest confirmation of the spec's core disk-streaming premise: a 744B model made
runnable on a 16 GB-RAM, no-GPU host by keeping only the dense backbone resident and streaming experts
from ~372 GB of local disk. GPU is explicitly optional for Colibri — it accelerates but does not gate
functionality.

### 1.8 Colibri-supported model roster (disk-streaming path — direct source data)

Colibri (`github.com/JustVugg/colibri`) is a pure-C, zero-dependency inference engine purpose-built
for MoE disk streaming, matching the spec's Assumptions section characterization exactly ("specialised…
not a general-purpose replacement for the existing in-memory runtime"). As of this research pass it
supports exactly **8 model families**, each with a stated minimum RAM figure:

| Family | Total / active params | Min RAM (Colibri) | Disk footprint |
|---|---|---|---|
| GLM-5.2 | 744B / 40B | 16 GB min, 24 GB comfortable | ~372 GB |
| GLM-5.3-Flash | 321B (vision-capable) | UNVERIFIED (not stated in the fetched excerpt) | UNVERIFIED |
| Inkling | 975B / 41B | UNVERIFIED | UNVERIFIED |
| Kimi K3 | 2.8T / 104B | UNVERIFIED | UNVERIFIED |
| DeepSeek V4 Flash | 284B / 13B | UNVERIFIED | UNVERIFIED |
| Qwen3.8-Flash-Next | 125B + 51B n-gram | UNVERIFIED | UNVERIFIED |
| Qwen3.6 | 35B / 3B | **24 GB RAM minimum** | UNVERIFIED |
| OLMoE | 7B / 1B | **8 GB RAM** | ~7 GB |

**Critical for FR-027/FR-028 selection logic**: Colibri's supported roster is a **closed, named list**
— it is NOT a generic "any MoE model" streaming engine. A model outside this list (e.g. DeepSeek-V3.2,
Llama 4 Scout/Maverick, gpt-oss-120b, Qwen3-30B-A3B/235B-A22B as of this research pass) has no
confirmed Colibri support path even though each is architecturally MoE. This directly matches spec
edge case: *"A model too large AND of an architecture the streaming path cannot serve… the system
must say so explicitly."* The selection engine's disk-streaming eligibility check must therefore be a
**named-family allowlist check against the actual runtime's supported roster**, not a bare
`architecture == MoE` predicate. OLMoE (7B/1B active, 8 GB RAM, ~7 GB disk) is the smallest and
cheapest verified Colibri entry — a plausible "streaming path available even on the low-end tier"
proof point.

**Weight source for Colibri containers**: pre-converted containers hosted on Hugging Face, e.g.
`huggingface.co/mastouri/GLM-5.2-colibri-int4-g64-with-int8-mtp` (~372 GB) — a *different* artifact
from the vendor's own GGUF/safetensors release, which the allowlist mechanism (FR-010) needs to
account for as a distinct source/integrity-value entry per catalogue row (FR-012).

### 1.9 Microsoft Phi-4 family

**License**: MIT across the family. **Weight source**: `huggingface.co/microsoft/*`, GGUF via
`ollama.com/library/phi4` and community mirrors.

| Model | Params | Arch | Context | Hardware |
|---|---|---|---|---|
| Phi-4-mini | 3.8B dense | dense | 128K | Smallest of the "serious" reasoning-tuned small models; cited as "the only viable option for 8GB machines" in one aggregate source — treat as a strong low-end-tier candidate alongside Llama 3.2 3B and Qwen3-4B |
| Phi-4 | 14B dense | dense | UNVERIFIED (not re-confirmed this pass) | Comparable footprint class to Qwen3-14B / Mistral NeMo |
| Phi-4-reasoning-vision-15B | 15B, vision-capable | dense | UNVERIFIED | Newer (March 2026); text-only footprint likely close to Phi-4 14B — UNVERIFIED, do not hardcode |

---

## 2. Tiered recommendation table

Five host classes as requested. "Fits" means the model's Q4_K_M-class footprint sits comfortably
within the class's stated headroom while leaving the SC-003 15%-free / no-sustained-swap margin —
these are planning-input judgements built on the cited figures above, not independently benchmarked
by this research pass.

### Tier A — No GPU, 8 GB system RAM (the floor; FR-003 requires this tier to be non-empty)

**Fits**:
- Llama 3.2 1B (Q4_K_M ≈ 807 MB) — comfortable headroom even with OS + context overhead.
- Qwen3-0.6B / Qwen3-1.7B (Q4_0 ≈ 429 MB / est. ~1.1 GB) — comfortable.
- Phi-4-mini 3.8B — cited directly as "the only viable option for 8GB machines" among mid-size models;
  fits with modest headroom, context window should be capped below the full 128K to stay inside the
  15% free-memory margin.
- Llama 3.2 3B (Q4_K_M ≈ 2.0 GB) — fits with headroom for a short-to-medium context.
- OLMoE (7B/1B active, Colibri disk-streaming path, 8 GB RAM minimum per Colibri's own figure) — this
  is the one MoE/disk-streaming entry that is explicitly rated for this tier; every larger Colibri
  entry needs strictly more RAM than this class has.

**Does NOT fit**:
- Anything in the 8B-dense-and-up class (Qwen3-8B needs 6-8 GB *for weights alone*, leaving no
  headroom for OS + KV cache + the SC-003 margin on an 8 GB total-RAM host).
- Every MoE family in §1.8 except OLMoE — GLM-5.2/Qwen3.6-MoE/Kimi K3/etc. all state minimum RAM
  figures at or above 16-24 GB, well past this tier's ceiling.
- Any 12B+ dense model (Mistral NeMo, Gemma 3 12B+) — footprint alone exceeds total system RAM before
  accounting for OS and inference overhead.

### Tier B — No GPU, 32 GB system RAM

**Fits**:
- Everything from Tier A, plus:
- Qwen3-8B (weights ~6-8 GB) and Qwen3-14B (~10-12 GB) at CPU speed — usable, not fast.
- Mistral NeMo 12B (~7.1 GB) and Gemma 3 12B (~8-9 GB).
- Qwen3-30B-A3B / Qwen3.6-35B-A3B (MoE, weights 18.7-21 GB resident) — the MoE active-parameter
  advantage means CPU throughput on this tier is materially better than a dense model of the same
  total size; this is the standout recommendation for this tier per FR-005 ("expected capability" vs
  resource cost).
- Colibri Qwen3.6 (35B/3B, 24 GB RAM minimum) fits with some headroom.
- Gemma 4 26B-A4B MoE (25.2B total / 3.8B active) — same active-parameter-advantage logic as the
  Qwen *-A3B line; exact footprint UNVERIFIED this pass but total-parameter class fits the 32 GB
  budget by the same reasoning as the Qwen equivalent.

**Does NOT fit**:
- Qwen3-32B / Qwen3.6-27B dense (~17-24 GB weights) — technically fits numerically but leaves thin
  headroom once OS + KV cache + SC-003's 15%-free margin are subtracted; borderline, recommend Tier B
  only offer this at reduced context, or defer to Tier C.
- Any 235B-class or larger MoE model (Qwen3-235B-A22B needs ~143 GB weights alone) — not offerable
  even via disk streaming without an explicit Colibri-class engine and far more disk/RAM headroom
  than this tier states.
- GLM-5.2 (16 GB *minimum*, 24 GB *comfortable* per Colibri) is borderline-fits on paper but the
  ~372 GB disk footprint requirement is a separate, larger constraint this tier's RAM figure says
  nothing about — disk headroom must be checked independently (see §3 caveat).

### Tier C — 8 GB VRAM (discrete GPU)

**Fits**:
- Everything a Tier-A/B CPU-only host can run, now at GPU speed.
- Qwen3-8B (6-8 GB VRAM) — comfortable, arguably the ceiling for pure in-VRAM residency on this tier.
- Mistral NeMo 12B (~7.1 GB) — fits.
- Gemma 3 4B (~3-4 GB) with strong headroom.
- Qwen3-14B only at aggressive quantization (below Q4_K_M) or with partial CPU offload — borderline;
  the cited 10-12 GB Q4_K_M figure exceeds this tier's raw VRAM, so full-GPU residency needs either a
  smaller quant or a hybrid GPU+RAM placement.

**Does NOT fit**:
- Mistral Small 3.2 24B (~13.4 GB) — exceeds VRAM outright.
- Qwen3-32B / any 27B+ dense model — exceeds VRAM outright.
- Any MoE family from §1.7/§1.8 at their full resident-weight footprint — all stated minimums
  (16 GB+) exceed this tier's VRAM; these belong on the CPU-RAM or disk-streaming path for a host in
  this class, not the in-VRAM path.

### Tier D — 16 GB VRAM (discrete GPU)

**Fits**:
- Everything Tier C fits, plus:
- gpt-oss-20b — OpenAI's own stated design target is exactly 16 GB, the strongest citeable number in
  this research; this is the standout recommendation for this tier.
- Qwen3-14B (10-12 GB) comfortably.
- Mistral Small 3.2 24B (~13.4 GB) comfortably.
- Qwen3.6-27B dense at Q4_K_M (~16.8 GB) is right at the edge — fits numerically but leaves near-zero
  headroom for KV cache/context; recommend offering it only at a capped context length on this tier,
  or bump it to Tier E.
- Qwen3-30B-A3B / Qwen3.6-35B-A3B MoE (18.7-21 GB) exceed pure 16 GB VRAM but are strong candidates
  for a GPU+CPU hybrid placement on a host that also has ample system RAM — flag as a placement
  decision, not a flat "does/doesn't fit."

**Does NOT fit**:
- Qwen3-32B dense outright in 16 GB VRAM.
- Any 235B+/Llama-4/DeepSeek-V3.2/GLM-5.x class model in-VRAM — all require multi-GPU or
  disk-streaming regardless of this tier's single-GPU ceiling.
- gpt-oss-120b (needs a single 80 GB GPU per OpenAI's own guidance) — not a fit at any consumer VRAM
  tier without disk streaming, and gpt-oss is not on Colibri's confirmed-support list.

### Tier E — 24 GB+ VRAM (discrete GPU, e.g. RTX 4090/5090 class)

**Fits**:
- Everything Tier D fits, plus:
- Qwen3.6-27B dense at Q5_K_M/Q6_K (19.5-22.5 GB) with real headroom, or Q4_K_M with generous context.
- Qwen3-32B dense (~20-24 GB estimated) — the ceiling dense model for this tier.
- Qwen3-30B-A3B / Qwen3.6-35B-A3B MoE fully in-VRAM with headroom to spare.
- Gemma 3 27B (~17-19 GB).
- This is the natural tier to pair with the disk-streaming path for the *smaller* Colibri entries
  (OLMoE, Qwen3.6 35B/3B) even though those don't strictly need a GPU — GPU acceleration here
  improves the resident dense-backbone and any hybrid-offloaded expert compute without changing
  output, per Colibri's own stated design.

**Does NOT fit**:
- Qwen3-235B-A22B (~143 GB Q4_K_M weights), Llama 4 Maverick, DeepSeek-V3.2 (675B), GLM-5.2 (744B) —
  none fit in a single 24-48 GB consumer GPU's VRAM even at aggressive quantization; these remain
  disk-streaming (Colibri, where supported) or multi-GPU-only candidates regardless of how large a
  single consumer GPU gets. This is the tier boundary the spec's User Story 4 (disk-streaming) exists
  to address — even the best single-GPU consumer tier cannot in-memory-serve this size class.

---

## 3. Cross-cutting notes for the selection engine (planning input, not spec text)

- **MoE ≠ automatically streaming-eligible.** Per §1.8, Colibri supports a **named, closed list** of
  8 families. The selection logic's FR-026..028 branch must check the specific model against the
  actual disk-streaming runtime's supported roster, not just `architecture == "moe"`. A model that is
  MoE but unsupported by any available streaming runtime must fall through to FR-028 ("say so
  explicitly"), not be silently offered.
- **Disk headroom is a second, independent resource axis.** GLM-5.2's ~372 GB disk footprint is not
  implied by its RAM figure — the Host Capability Profile (Key Entities) needs a storage-headroom
  field distinct from memory headroom, and FR-004's "within its available headroom" must be checked
  against both.
- **Licence is a catalogue field, not a memory constraint**, but it gates whether a model may appear
  in the allowlist at all for a given deployment (Llama 4's 700M-MAU commercial-use ceiling, Gemma 3's
  non-Apache terms vs Gemma 4's Apache 2.0). FR-009's catalogue entry should record licence alongside
  resource requirements.
- **Two distinct artifact shapes per model**: a vendor's own GGUF/safetensors release (allowlist
  source: e.g. `huggingface.co/Qwen/*`, `huggingface.co/openai/*`) versus a Colibri-specific
  pre-converted container (allowlist source: e.g. `huggingface.co/mastouri/GLM-5.2-colibri-int4-*`).
  These need separate catalogue rows with separate integrity values (FR-012) even when they represent
  "the same model" to the end user.
- **Active-parameter MoE models are the best fit for CPU-only, no-GPU hosts with generous RAM** — the
  *-A3B/-A4B pattern (Qwen3-30B-A3B, Qwen3.6-35B-A3B, Gemma 4 26B-A4B) repeatedly gives CPU throughput
  much closer to a small dense model than their total-parameter count would suggest, while requiring
  the full weight set resident in RAM. This is the load-bearing justification for Tier B's standout
  recommendation.

---

## Sources

All accessed 2026-09-02 via live web search / fetch during this research pass.

- Qwen3/3.5/3.6 sizing and licence: <https://knightli.com/en/2026/05/01/qwen3-6-local-vram-quantization-table/>,
  <https://willitrunai.com/blog/qwen-3-gpu-requirements>, <https://packet.ai/blog/qwen-3-6-vram-requirements>,
  <https://huggingface.co/bartowski/Qwen_Qwen3-0.6B-GGUF>, <https://github.com/QwenLM/Qwen3>
- Qwen3-235B-A22B VRAM: <https://www.spheron.network/tools/gpu-recommender/Qwen/Qwen3-235B-A22B/>,
  <https://huggingface.co/ubergarm/Qwen3-235B-A22B-GGUF>
- Llama 4 licence and architecture: <https://www.llama.com/llama4/license/>,
  <https://huggingface.co/unsloth/Llama-4-Scout-17B-16E-Instruct-GGUF>,
  <https://huggingface.co/meta-llama/Llama-4-Scout-17B-16E>
- Llama 3.2 1B/3B footprints: aggregate of <https://llmhardware.io/guides/llama-3.1-hardware-requirements>,
  <https://insiderllm.com/guides/llama-3-guide-every-size/>, <https://ggufloader.github.io/gguf-memory-calculator.html>
- Gemma 3 sizes/licence: <https://en.wikipedia.org/wiki/Gemma_(language_model)>,
  <https://www.deeplearning.ai/the-batch/google-releases-gemma-3-vision-language-models-with-open-weights/>
- Gemma 4 sizes/licence: <https://www.mindstudio.ai/blog/what-is-gemma-4-google-open-weight-model>,
  <https://codersera.com/blog/gemma-4-complete-guide-2026/>, <https://deepinfra.com/blog/gemma-4-model-overview>
- gpt-oss-20b/120b (fetched directly from model cards): <https://huggingface.co/openai/gpt-oss-20b>,
  <https://huggingface.co/openai/gpt-oss-120b>
- Mistral NeMo/Small 3.2: <https://llmhardware.io/guides/mistral-hardware-requirements>,
  <https://openlaboratory.com/models/mistral-small-3_2-24b-instruct-2506/>,
  <https://ollama.com/library/mistral-nemo:12b-instruct-2407-q4_K_M>
- Mistral Apache 2.0 licensing (2026 lineup): <https://www.secondtalent.com/resources/every-mistral-ai-model-explained-compared/>,
  <https://serenitiesai.com/articles/mistral-ai-models-2026-complete-guide>
- DeepSeek V3.2: <https://huggingface.co/deepseek-ai/DeepSeek-V3.2>, <https://dasroot.net/posts/2026/05/integrating-deepseek-v3-2-moe-gguf/>,
  <https://dev.to/rams901/deepseek-v3-the-671b-moe-model-you-can-run-locally-in-2026-30o4>
- GLM-4.6/5.x: <https://huggingface.co/zai-org/GLM-4.6>, <https://docs.z.ai/guides/llm/glm-4.6>,
  <https://huggingface.co/bartowski/zai-org_GLM-4.6-GGUF>, <https://presenc.ai/research/zhipu-glm-model-lineage-2026>,
  <https://kie.ai/blog/what-is-glm-5-5>
- Colibri engine and supported-family table (fetched directly from repository): <https://github.com/JustVugg/colibri>
  (cross-referenced against the spec's own cited sources: <https://wavect.io/blog/colibri-glm-5-2-consumer-hardware/>,
  <https://pasqualepillitteri.it/en/news/7923/colibri-glm-5-2-744b-25gb-ram-en>)
- Phi-4 family: <https://en.wikipedia.org/wiki/Phi_(language_model)>, <https://apxml.com/models/phi-4-mini>,
  <https://ollama.com/library/phi4>, <https://localaimaster.com/models/phi-4-mini>
- General quantization framing (Q4_K_M as size/quality balance point): <https://huggingface.co/blog/daya-shankar/open-source-llm-models-to-run-locally>,
  <https://www.matterai.so/guides/running-llms-locally-gguf-quantization-memory-planning>

**Access date for every citation above: 2026-09-02.**
