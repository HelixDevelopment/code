# Research 02 — Vision (VLM) and Image-Generation models for adaptive local serving

Input for `/speckit-plan` on spec 002 (`specs/002-adaptive-local-model-serving/spec.md`). Covers the
two non-text families explicitly named in FR-009/FR-026/User Story 5: vision/image-understanding
(VLM, image-in text-out) and image generation (text-in image-out). Text LLMs, audio, TTS/STT and
design/vector-graphics are out of scope for this file — see the sibling research files.

## 0. What already exists in `submodules/helix_llm` (confirmed by reading the code, not assumed)

The spec's premise — "extend HelixLLM" — is correct for both families. There is a real, wired,
partially-proven serving path already, not a blank slate:

**Vision (VLM) — `cmd/visiongen-boot/` + `internal/vrambroker`:**
- `cmd/visiongen-boot/compose.vision.yml` boots **`Qwen2.5-VL-3B-Instruct-Q4_K_M.gguf`** (default,
  env-overridable) + its `mmproj-Qwen2.5-VL-3B-Instruct-Q8_0.gguf` projector through the already-built
  `localhost/helixllm/llamacpp-router:cuda12.8-sm120` image (llama.cpp `llama-server` + `libmtmd`), on
  its own port `:18439`, via the `containers` submodule compose orchestrator, rootless podman + NVIDIA
  CDI GPU passthrough.
- `internal/vrambroker` already models a `ClassVLM` "warm tier" lease type, separate from `ClassImage`
  (burst) and the resident coder class — this is the admission mechanism the spec's hardware-aware
  selection (FR-001..FR-008) should extend, not replace.
- `docs/qa/phase3_vision_20260707/RESULTS.md` is a **captured, real, on-GPU proof**: the 3B GGUF model
  co-resident with a live 30B coder on one RTX 5090, given a real generated image (red circle, white
  background, "HELIX" text), correctly named the color, shape, and OCR'd the text; a golden-BAD
  assertion was proven to fail (self-validated analyzer). Measured actual VRAM peak for the VLM alone:
  **4138 MiB** (well under the 6 GiB pre-boot estimate). A larger 7B variant is wired as an
  env-override (`VISIONGEN_MODEL_GGUF` / `VISIONGEN_MMPROJ` / `VISIONGEN_NEED_BYTES`, default ~9 GiB
  budget) but its GGUF has not been downloaded/measured yet.
- **What is MISSING for spec 002**: multi-size catalogue entries beyond the 3B/7B pair (no 32B/72B
  tier, no CPU-only tier, no non-Qwen VLM family for diversity/fallback), no host-capability-driven
  *selection* logic (today the model file is chosen by an env var the operator sets, not measured and
  offered automatically per FR-001..FR-008), no integrity/allowlist verification of the downloaded
  GGUF against a recorded hash (FR-010..FR-012), and no naming per the `helixllm/<host>/<model>[:<variant>]`
  scheme (FR-014) — the service is named by its compose project, not exposed as a selectable option yet.

**Image generation — `services/imagegen/` + `cmd/imagegen-boot/`:**
- `services/imagegen/imagegen_server.py` is a real FastAPI shim around a real `diffusers.FluxPipeline`
  + Nunchaku `NunchakuFluxTransformer2dModel` (NVFP4 SVDQuant), defaulting to
  **`black-forest-labs/FLUX.1-schnell`** (Apache-2.0, switched from the non-commercial `FLUX.1-dev`
  specifically because of licensing — `dev` stays available as an explicit `IMAGEGEN_MODEL` opt-in).
  It refuses to fabricate an image: a failed pipeline load returns HTTP 503 with the exact reason.
- `docs/qa/phase4_imagegen_20260707/README.md` documents the VRAM admission math (coder resident
  ≈19.4 GiB of 32 GiB, ≈12.7 GiB free, broker headroom 2 GiB, NVFP4 need placeholder **7 GiB**) and a
  **self-validated (no-GPU) image analyzer** (entropy/unique-colors/dominant-fraction/adjacent-diff/
  compressibility, proven to reject solid/blank/noise fixtures) — but the status is explicitly
  **SCAFFOLD / PENDING**: the actual FLUX generation and the real VRAM peak on hardware have **not**
  been captured yet (it needs an operator-authorized coder-pause window per §11.4.122).
- **What is MISSING for spec 002**: any second image-gen model (only FLUX is wired — no SD-family
  entry, no CPU-viable option, no distinct "fast/cheap" vs "quality" tier), no completed runtime proof
  of the one model that IS wired, no naming/catalogue/host-selection integration, no integrity
  verification of the downloaded weights.

**Cross-cutting infra already present and reusable by both families:** `internal/vrambroker` (admission
+ lease classes + LRU eviction shape matching FR-044..FR-047), the `containers` submodule boot path
(§11.4.76, rootless podman + CDI GPU), the router image with llama.cpp `libmtmd` multimodal support,
and the self-validated-analyzer pattern (§11.4.107(10)) that any new model's regression guard should
reuse rather than reinvent.

---

## 1. Vision / image-understanding (VLM) candidates

All entries below are decoder-based multimodal chat models (image(s) + text in → text out), servable
through llama.cpp's `libmtmd` (GGUF text backbone + separate `mmproj` vision-projector GGUF) unless
noted. Sizes given are for the **Instruct** variant unless stated.

| Model | Params | Licence | Quantisations & footprint | Min GPU VRAM | CPU+RAM-only viable? | Weights source |
|---|---|---|---|---|---|---|
| **SmolVLM2-256M-Video-Instruct** | 256 M | Apache-2.0 | FP16 full model well under 1 GB; GGUF quants exist for llama.cpp | none required | **Yes** — designed as the smallest video/image LM shipped; runs on CPU laptops/edge devices | `HuggingFaceTB/SmolVLM2-256M-Video-Instruct` (HF) |
| **SmolVLM2-500M-Video-Instruct** | 500 M | Apache-2.0 | FP16 ~1 GB | none required | **Yes** | `HuggingFaceTB/SmolVLM2-500M-Video-Instruct` (HF) |
| **Moondream2** | ~1.9 B (reported "2B" by the vendor) | Apache-2.0 | BF16 ≈6.4 GB VRAM to load; int8 ≈4.4 GB; int4 ≈3.4 GB; further-quantised builds reported down to ≈1.2 GB | none required at int4/int8 | **Yes** — vendor states it is "highly optimized for CPU inference" and runs on a Raspberry Pi–class device | `vikhyatk/moondream2` (HF) |
| **Qwen2.5-VL-3B-Instruct** *(already wired in helix_llm)* | 3.09 B | Apache-2.0 | GGUF Q4_K_M weights **1.79 GiB** + `mmproj` Q8_0 **806 MiB** = ~2.6 GiB on disk; measured actual GPU peak (this repo's own proof) **4.14 GiB** | ~5 GB (safety-gated estimate used internally) | **Yes** — small enough for CPU+`libmtmd`, though prompt/gen speed will be low tokens/s on CPU (UNVERIFIED exact CPU tok/s for this repo's hardware) | `ggml-org/Qwen2.5-VL-3B-Instruct-GGUF` (HF; already the source in `compose.vision.yml`) |
| **Qwen3-VL-4B-Instruct** | 4 B | Apache-2.0 | GGUF: language model FP16 / Q8_0 / **Q4_K_M ≈2.5 GB**; `mmproj` FP16 or Q8_0 (mixable independently of LM precision) | ~5–6 GB at Q4_K_M + mmproj + KV cache | **Yes**, comparable to the 3B Qwen2.5-VL entry | `Qwen/Qwen3-VL-4B-Instruct-GGUF` (HF) |
| **Qwen3-VL-8B-Instruct** | 8 B | Apache-2.0 | GGUF Q4_K_M **5.03 GB**, Q8_0 **8.71 GB**, F16 **16.4 GB**; separate `mmproj-Qwen3VL-8B-Instruct` (F16 or Q8_0) | Q4_K_M + mmproj + KV cache fits in **8 GB** with limited headroom; comfortable at 12–16 GB | Borderline — runnable on CPU at Q4_K_M but noticeably slower than the 3B/4B tier (UNVERIFIED exact tok/s) | `Qwen/Qwen3-VL-8B-Instruct-GGUF` (HF) |
| **MiniCPM-V-4.5** | 8 B (Qwen3-8B LLM + SigLIP2-400M vision tower) | **Custom "MiniCPM Model License"** — free for research/academic use; commercial use requires registration with the vendor (not a plain permissive OSS licence like Apache/MIT) | GGUF: Q4_0 4.77 GB … Q4_K_M **5.03 GB** … Q6_K 6.72 GB … Q8_0 8.71 GB … F16 16.4 GB | ~6–8 GB at Q4_K_M | **Yes**, vendor explicitly documents llama.cpp/Ollama CPU inference and int4 GGUF for 4–6 GB VRAM class hardware | `openbmb/MiniCPM-V-4_5-gguf` (HF) |
| **InternVL3-8B** | 8 B (InternViT-300M-448px-V2.5 vision tower + Qwen2.5-7B LLM) | Project licence **MIT**; note the model composes a Qwen2.5 component (Apache-2.0 per that component's own card — verify the exact sub-licence text before shipping since Qwen licensing has varied by size/series) | Native BF16 card gives multi-80GB-GPU guidance for the *unquantised* reference deployment; **GGUF quantisations exist** ("5 quantized model variants" via community requantisers) — exact GGUF sizes UNVERIFIED, treat as comparable to other 8B VLMs (~5 GB at Q4_K_M) pending a direct measurement | UNVERIFIED at GGUF Q4 (estimate ~6–8 GB); the vendor's own BF16 reference numbers (80 GB-class GPUs) do **not** apply to the quantised local-serving path | Likely, via GGUF, comparable to the 3B/8B Qwen tier — UNVERIFIED, no CPU benchmark found | `OpenGVLab/InternVL3-8B` (HF); GGUF requants under community namespaces |
| **Phi-4-multimodal-instruct** | 5.6 B | **MIT** | Repo footprint ≈12.9 GB (mixed vision+speech+text adapters, not a pure GGUF quant ladder); no GGUF quant table found in this pass — UNVERIFIED | UNVERIFIED (no GGUF/llama.cpp quant sizes located); treat as an **8 GB+** candidate pending quantisation data | UNVERIFIED — architecture (adapters over Phi-4-Mini) suggests CPU viability similar to other ~5–8B models but no confirming source found | `microsoft/Phi-4-multimodal-instruct` (HF) |
| **PaliGemma2-3B** (224/448/896px variants) | 3 B (Gemma-2 2B LM + SigLIP vision tower) | **Gemma licence** (permissive-with-terms: redistribution, commercial use, fine-tuning and derivatives all allowed under Google's Gemma terms — not a plain Apache/MIT grant) | UNVERIFIED exact VRAM/quant table in this pass — Gemma-2 2B-class LM footprint suggests a similar order to the Qwen 3–4B VLM tier | UNVERIFIED, estimate ~5–6 GB unquantised bf16 | UNVERIFIED, plausible given the small LM backbone | `google/paligemma2-3b-pt-224` (HF) |
| **PaliGemma2-10B / -28B** | 10 B / 28 B (Gemma-2 9B / 27B LM) | Gemma licence | UNVERIFIED exact figures; scale suggests 10B ≈ the Qwen3-VL-8B tier, 28B ≈ the Qwen2.5-VL-32B tier below | UNVERIFIED — 10B likely fits 12–16 GB unquantised bf16, quantised less; 28B needs 24 GB+ class or heavy quantisation | Not practical | `google/paligemma2-10b-pt-448`, `google/paligemma2-28b-pt-224` (HF) |
| **Qwen2.5-VL-32B-Instruct** | 33 B (vendor-stated "33B params") | Apache-2.0 | GGUF quantisations exist via community requantisers; no exact size table found in this pass — extrapolating linearly from the 7B Q4_K_M figure (5.03 GB → ≈33/7× ≈ **24 GB** at Q4_K_M) is a rough estimate, not a measured figure (UNVERIFIED) | Estimated **≥24 GB** at Q4_K_M with little to no headroom — **does not fit the 16 GB tier**, only comfortable at 24 GB+ | No — too large for practical CPU-only serving | `Qwen/Qwen2.5-VL-32B-Instruct` (HF) |
| **Qwen3-VL-32B(-Instruct/-Thinking)** | 32 B (dense) | Apache-2.0 | Same order-of-magnitude as the 2.5-VL-32B entry above; UNVERIFIED exact GGUF sizes | Estimated ≥24 GB at Q4-class quant (UNVERIFIED, extrapolated) | No | `Qwen/Qwen3-VL-32B-*` (HF) |
| **Qwen2.5-VL-72B-Instruct / Qwen3-VL-235B-A22B** | 72 B dense / 235 B total (22 B active, MoE) | Apache-2.0 | 72B dense at Q4_K_M would extrapolate to roughly **43 GB** — exceeds every host class in this table's ceiling (24 GB+). The 235B-A22B MoE's **total weight** (not just active params) must still be resident for GGUF-style serving; at Q4-class quantisation that is on the order of **100+ GB** — UNVERIFIED exact figure, but clearly beyond a single consumer/workstation GPU regardless of active-parameter count | **Does not fit any tier below** — this is squarely User Story 4 (disk-streaming) territory, not in-memory selection | N/A at this scale without heavy offload/streaming | `Qwen/Qwen2.5-VL-72B-Instruct`, `Qwen/Qwen3-VL-235B-A22B-Instruct` (HF) |

**llama.cpp multimodal support (the serving engine this repo already uses).** Confirmed current:
`libmtmd` (the shared multimodal library behind `llama-server` / `llama-mtmd-cli`) supports image
(and audio/video) input for **Qwen2-VL / Qwen3-VL, LLaVA, Gemma-3 (vision), InternVL, MiniCPM-V,
Pixtral, CogVLM, and DeepSeek-OCR** — every GGUF-based candidate above other than PaliGemma2 and
Phi-4-multimodal is on that supported list; PaliGemma2 and Phi-4-multimodal support in llama.cpp specifically
is **UNVERIFIED** in this pass and should be re-checked against `docs/multimodal.md` in the pinned
llama.cpp release before those two are added to the catalogue.

---

## 2. Image generation (text-in, image-out) candidates

| Model | Params | Licence | Quantisations & footprint | Min GPU VRAM | Typical generation time | CPU-only viable / degrades usefully? | Weights source |
|---|---|---|---|---|---|---|---|
| **Stable Diffusion 1.5** | 0.98 B UNet | **CreativeML OpenRAIL-M** (permissive but carries RAIL use-based restrictions — commercial use and redistribution allowed, harmful-use clauses apply) | FP16 full ≈5.9 GB VRAM at 1024×1024 (model card era default was 512×512, where footprint is smaller); via `stable-diffusion.cpp` GGUF quant, 512×512 fp16 needs only **≈2.3 GB** | ~4 GB minimum for basic low-res generation | GPU (RTX-class): **~1–2 s/image** at 512×512 on high-end cards. CPU-only: **~5–20 minutes per 512×512 image** on a modern CPU (unoptimised); ONNX/OpenVINO backends cut this materially but still land in the minutes range | **Yes, CPU is viable but slow** — this is the practical "works everywhere, badly" fallback | `stable-diffusion-v1-5/stable-diffusion-v1-5` (HF) |
| **PixArt-Σ (Sigma) XL-2** | 0.6 B (diffusion transformer) | Weights: **CreativeML Open RAIL++-M**; code: Apache-2.0 | Runs under 8 GB VRAM with the T5 text encoder loaded in 8-bit; CPU offload available via `enable_model_cpu_offload()` | <8 GB achievable with 8-bit text-encoder + offload | UNVERIFIED exact seconds/image on this repo's target hardware classes; smallest-parameter model in this table so plausibly the fastest per-step, but no benchmark source located | UNVERIFIED — no CPU-only benchmark found; small param count suggests it degrades better than SD/SDXL on CPU but this is not confirmed | `PixArt-alpha/PixArt-Sigma-XL-2-1024-MS` (HF) |
| **SDXL 1.0 (base)** | ~3.5 B UNet + 2 fixed CLIP/OpenCLIP text encoders | **CreativeML OpenRAIL++-M** | FP16 weights **6.94 GB** on disk; documented as workable on **8 GB VRAM** consumer GPUs, with CPU-offload available for less | 8 GB (vendor-stated consumer-GPU target) | UNVERIFIED precise seconds/image for this repo's hardware; broadly "tens of seconds" class at 1024×1024 on 8 GB cards per general community reporting, not independently confirmed here | CPU-only degrades to the same many-minutes-per-image regime as SD1.5, worse due to larger UNet — not independently benchmarked in this pass | `stabilityai/stable-diffusion-xl-base-1.0` (HF) |
| **SDXL-Turbo** | Same ~3.5 B UNet as SDXL (distilled for 1–4-step generation) | **Stability AI Non-Commercial Research Community Licence** — research/non-commercial only; NOT cleared for HelixCode's commercial-capable default catalogue without a separate commercial licence (one search result claimed an Apache-2.0 HF listing exists somewhere — treat as UNCONFIRMED and verify the exact repo's `LICENSE` file before offering it, since the vendor's own licence page is authoritative) | ~4.5 GB VRAM at 512×512/FP16 single-step; ~7 GB at 1024×1024/FP16 | ~7 GB at 1024×1024 (12 GB recommended "comfortable") | **Sub-second to a few seconds** per image (1–4 inference steps, no classifier-free guidance) — the fastest GPU option in this table | Not really — the speed advantage is step-count-based, not compute-cheaper per step; CPU-only still lands in the minutes range | `stabilityai/sdxl-turbo` (HF) |
| **Stable Diffusion 3.5 Medium** | 2.5 B (MMDiT) | **Stability AI Community Licence** — free for research and for commercial use under $1M annual revenue (not unconditionally permissive like Apache/MIT) | FP16 ≈5 GB VRAM (weights only, excluding text encoders which add further overhead); full pipeline documented at up to ≈9.9 GB for full quality; **INT4-quantised ≈1.3 GB** | ~5–10 GB depending on which text encoders are kept resident | UNVERIFIED exact seconds/image on target hardware | INT4 quantised path exists; CPU-only viability not independently benchmarked here — treat as UNVERIFIED, likely similar many-minutes-class to SD1.5/SDXL given comparable/larger UNet-equivalent size | `stabilityai/stable-diffusion-3.5-medium` (HF) |
| **Stable Diffusion 3.5 Large** | 8.1 B (MMDiT) | Stability AI Community Licence (same terms as Medium) | FP16 ≈18 GB VRAM; **FP8 build ≈11 GB** (NVIDIA co-engineered quantisation, ~40% VRAM reduction) | ~11 GB (FP8) to ~18 GB (FP16) | UNVERIFIED exact seconds/image | No — 8.1B-class diffusion transformer on CPU is not a practical path; no source found even attempting it | `stabilityai/stable-diffusion-3.5-large` (HF) |
| **FLUX.1-schnell** *(already wired in helix_llm, default)* | 12 B transformer | **Apache-2.0** | Full BF16 pipeline: **not confirmed to fit 24 GB** per multiple independent sources (community figures range ~22–33 GB depending on what is counted as "in VRAM"); **this repo's own NVFP4 (Nunchaku SVDQuant) path is the intended local-serving configuration**: transformer ≈6.1 GiB + quantised T5 + CPU-offloaded idle stages, estimated co-resident peak ≈6 GB, current placeholder budget **7 GiB** (PENDING first real-hardware calibration per the repo's own `IMAGEGEN_NEED_BYTES` comment) | **~7–8 GB at NVFP4** (this repo's own quantised path); BF16 unquantised needs 24 GB+ and per several sources may not comfortably fit even a 24 GB card | UNVERIFIED exact seconds/image on this repo's hardware (harness exists, run is PENDING per `docs/qa/phase4_imagegen_20260707/README.md`) | No practical CPU path found for the diffusers/Nunchaku pipeline used here; `stable-diffusion.cpp` upstream claims general Flux support in pure C/C++, which would be the CPU-fallback route if pursued, but this repo does not currently wire that path | `black-forest-labs/FLUX.1-schnell` + `nunchaku-ai/nunchaku-flux.1-schnell` (HF; already the source in `imagegen_server.py`) |
| **FLUX.1-dev** *(already wired, opt-in)* | 12 B transformer | **Non-commercial** (`flux-1-dev-non-commercial-license`) — HF API confirms `"gated": "auto"`, an HF account must click "Agree" | Same architecture/footprint class as schnell (same transformer size); this repo's NVFP4 path applies identically via the `IMAGEGEN_NVFP4_TRANSFORMER` auto-selection already in `imagegen_server.py` | Same as schnell (~7–8 GB NVFP4; 24 GB+ unquantised, per some sources even 24 GB is tight) | UNVERIFIED, same PENDING status as schnell | Same as schnell — no wired CPU path | `black-forest-labs/FLUX.1-dev` + `nunchaku-ai/nunchaku-flux.1-dev` (HF) |
| **FLUX.2 [klein] 4B** *(new, not yet in helix_llm)* | 4 B | **Apache-2.0** — the only fully-permissive weight in the FLUX.2 family | FP16 fits **~8–13 GB** VRAM (sources vary: "~8.4 GB" vs "fits in ~13 GB, suits RTX 3090/4070"); FP8/NVFP4 builds cut this further (BFL + NVIDIA co-released FP8/NVFP4 for the whole klein family) | **~8 GB** at FP16 (the smallest-footprint fully-permissive image-gen model found in this research) | Vendor-stated **sub-10-second** generation on consumer hardware; klein family is explicitly built for "real-time"/interactive use | UNVERIFIED CPU path; the architecture is optimised for fast few-step GPU inference, not documented for CPU | `black-forest-labs/FLUX.2-klein-4B` (HF; released ~mid-Jan 2026 per BFL's own release notes) |
| **FLUX.2 [klein] 9B** | 9 B | **FLUX.2-dev Non-Commercial Licence** (not Apache — only the 4B klein variant is permissive) | UNVERIFIED exact figure in this pass; scaling from the 4B (~8–13 GB) suggests roughly **16–18 GB** at FP16 — treat as an estimate, not measured | Estimated ~16 GB+ at FP16; smaller with FP8/NVFP4 (UNVERIFIED exact numbers) | Vendor states the klein family targets sub-second-to-few-second generation; exact figure for 9B UNVERIFIED | UNVERIFIED | `black-forest-labs/FLUX.2-klein-9B` (HF) |
| **FLUX.2 [dev]** | 32 B | **Non-commercial** licence (same restriction class as FLUX.1-dev) | FP8 checkpoint ≈32–35 GB (fits a 24 GB card only with CPU offload); **GGUF Q4_K quantisation ≈19 GB fits a 24 GB card directly**; a 16 GB card (e.g. Intel Arc A770) has been reported as the cheapest GPU that fits it fully in VRAM at some quant level (source did not specify which) | **~19 GB at Q4 GGUF** (fits 24 GB with headroom); FP8 effectively needs 32 GB+ or offload | UNVERIFIED exact seconds/image | No practical CPU path found; this is the largest image-gen model in this table | `black-forest-labs/FLUX.2-dev` (HF) |
| **Qwen-Image** | 20 B (MMDiT) — note: search results indicate a 7 B "Qwen-Image 2.0" was announced separately (UNVERIFIED release status/date — treat the 20B figure as the confirmed current entry and re-verify the 7B successor before relying on it) | **Apache-2.0** | Full BF16 ≈**40 GB**; FP8 ≈**16 GB**; via GGUF or Nunchaku 4-bit **+ a 4-step "Lightning" LoRA** distillation, down to **~8 GB VRAM with 16 GB system RAM**; layer-by-layer CPU offload (DiffSynth-Studio) claims inference within **4 GB VRAM** at a steep speed cost | **~8 GB** at the aggressive 4-bit + Lightning-LoRA + 16 GB system-RAM configuration; **~16 GB** at FP8; **40 GB+** for full BF16 — three very different tiers depending on quantisation choice, so the catalogue entry for this model MUST record which variant it is offering, not just "Qwen-Image" | UNVERIFIED exact seconds/image per tier | UNVERIFIED direct CPU-only benchmark; the 4 GB-VRAM layer-offload path implies the model is designed to degrade rather than hard-fail on constrained hardware, which is the useful property FR-027 wants, but no number source confirms CPU-only feasibility | `Qwen/Qwen-Image` (HF); released 2025-08-04 per vendor |

**CPU-serving engine note.** `stable-diffusion.cpp` (pure C/C++, ggml backend) is the concrete
CPU-viable serving path for the SD-family models above: 2/3/4/5/8-bit GGUF quantisation, ~2.3 GB RAM
for a 512×512 SD1.5-class generation, single native binary (no Python runtime), CUDA/Vulkan/Metal/
OpenCL/SYCL backends for the GPU tiers too. This repo does not currently wire `stable-diffusion.cpp`
anywhere (`imagegen_server.py` uses `diffusers` + Nunchaku, GPU-only) — adding a CPU-tier image-gen
option to the catalogue means either wiring this alternate engine or accepting the "usable but very
slow" GPU-engine-on-CPU path, which is unverified for the diffusers pipeline already in the repo.

---

## 3. Tiered fit — 5 host classes

Legend: ✅ fits comfortably · ⚠️ fits tightly / with caveats (quantisation required, little headroom,
or figure UNVERIFIED) · ❌ does not fit.

### 3a. Vision (VLM)

| Model | No-GPU / 8 GB RAM | No-GPU / 32 GB RAM | 8 GB VRAM | 16 GB VRAM | 24 GB+ VRAM |
|---|---|---|---|---|---|
| SmolVLM2-256M/500M | ✅ | ✅ | ✅ | ✅ | ✅ |
| Moondream2 (~1.9B) | ✅ (int4 ≈1.2 GB) | ✅ | ✅ | ✅ | ✅ |
| Qwen2.5-VL-3B (already wired) | ✅ (~2.6 GB, slow) | ✅ | ✅ | ✅ | ✅ |
| Qwen3-VL-4B | ⚠️ (~2.5 GB, CPU slow) | ✅ | ✅ | ✅ | ✅ |
| Qwen3-VL-8B / MiniCPM-V-4.5 (8B) | ❌ too slow to be "usable" (RAM fits, latency does not) | ⚠️ (fits, slow) | ✅ (Q4_K_M) | ✅ | ✅ |
| InternVL3-8B | ❌ | ⚠️ (UNVERIFIED size, likely fits) | ⚠️ (UNVERIFIED GGUF size) | ✅ | ✅ |
| Phi-4-multimodal (5.6B) | ❌ | ⚠️ (UNVERIFIED) | ⚠️ (UNVERIFIED, no GGUF ladder found) | ✅ (est.) | ✅ |
| PaliGemma2-3B | ❌ (CPU, slow) | ⚠️ (UNVERIFIED) | ✅ (est.) | ✅ | ✅ |
| PaliGemma2-10B | ❌ | ❌ | ⚠️ (UNVERIFIED, likely tight) | ✅ (est.) | ✅ |
| PaliGemma2-28B | ❌ | ❌ | ❌ | ⚠️ (UNVERIFIED, likely too big) | ✅ (est.) |
| Qwen2.5-VL-32B / Qwen3-VL-32B | ❌ | ❌ (fits RAM at Q4 but impractically slow) | ❌ | ❌ (~24 GB estimated need, no headroom) | ✅ |
| Qwen2.5-VL-72B / Qwen3-VL-235B-A22B | ❌ | ❌ | ❌ | ❌ | ❌ — needs the disk-streaming path (US4), not any tier here |

**What does NOT fit each tier, explicitly:**
- **No-GPU/8GB RAM**: nothing above ~4B params at CPU speeds anyone would call responsive; 8B-class
  models technically fit in RAM at Q4 but latency makes them not "usable" per SC-001's intent.
- **No-GPU/32GB RAM**: extra RAM lets bigger *files* load, but CPU compute — not RAM — is the
  bottleneck for anything above ~8B; 32B-class models are RAM-feasible at Q4 (~20-24 GB) but CPU
  generation speed at that size was not found benchmarked anywhere and should be assumed impractically
  slow until measured.
- **8 GB VRAM**: 32B+ dense models do not fit even at Q4 (~24 GB needed); this tier should be the
  ceiling for the "small/medium" VLM catalogue band.
- **16 GB VRAM**: 32B-class models are a tight/no-headroom fit at best (extrapolated ~24 GB need) —
  recommend NOT offering them until measured; PaliGemma2-28B similarly borderline/UNVERIFIED.
- **24 GB+ VRAM**: still cannot fit 72B dense or 235B-MoE-total-weight models — those remain out of
  reach for in-memory serving on any single-GPU host class in this table.

### 3b. Image generation

| Model | No-GPU / 8 GB RAM | No-GPU / 32 GB RAM | 8 GB VRAM | 16 GB VRAM | 24 GB+ VRAM |
|---|---|---|---|---|---|
| SD 1.5 (`stable-diffusion.cpp` GGUF) | ⚠️ fits (~2.3 GB RAM), **minutes/image** | ⚠️ same, no faster (compute-bound not RAM-bound) | ✅ fast (~1-2s/image) | ✅ | ✅ |
| PixArt-Σ (0.6B) | ⚠️ UNVERIFIED CPU speed, small enough to plausibly work | ⚠️ same | ✅ (<8 GB w/ 8-bit text encoder) | ✅ | ✅ |
| SDXL 1.0 base | ❌ impractical (bigger UNet than SD1.5, no CPU quant path confirmed here) | ⚠️ fits, very slow (UNVERIFIED, worse than SD1.5) | ✅ (vendor-stated 8 GB target) | ✅ | ✅ |
| SDXL-Turbo | ❌ | ⚠️ same caveat as SDXL | ✅ (~7 GB @1024px, fastest GPU option) | ✅ | ✅ — licence is non-commercial-research, verify before offering as a default |
| SD3.5 Medium | ❌ | ⚠️ UNVERIFIED, likely impractical | ✅ (~5-10 GB) | ✅ | ✅ |
| SD3.5 Large | ❌ | ❌ (8.1B, no source even attempts CPU) | ❌ (needs ≥11 GB FP8) | ✅ (FP8 ≈11 GB) | ✅ (FP16 ≈18 GB) |
| FLUX.1-schnell (already wired) | ❌ | ❌ | ⚠️ NVFP4 estimated ~7-8 GB — PENDING real measurement | ✅ | ✅ |
| FLUX.1-dev (already wired, opt-in) | ❌ | ❌ | ⚠️ same as schnell, PENDING | ✅ | ✅ |
| FLUX.2 klein 4B (Apache-2.0) | ❌ | ❌ | ✅ (~8 GB FP16, fastest-fitting Apache option) | ✅ | ✅ |
| FLUX.2 klein 9B | ❌ | ❌ | ❌ (UNVERIFIED, est. needs >8 GB) | ⚠️ UNVERIFIED, est. ~16-18 GB, tight | ✅ |
| FLUX.2 dev (32B, non-commercial) | ❌ | ❌ | ❌ | ❌ (needs ≥19 GB even at Q4 GGUF) | ✅ (Q4 GGUF ≈19 GB; FP8 needs 32 GB+) |
| Qwen-Image (20B, Apache-2.0) | ❌ | ❌ | ⚠️ only at aggressive 4-bit + Lightning-LoRA + 16 GB **system RAM** — record which variant is offered | ✅ (FP8 ≈16 GB) | ✅ (BF16 ≈40 GB does NOT fit 24 GB — still needs FP8/GGUF even here) |

**What does NOT fit each tier, explicitly (image generation):**
- **No-GPU/8GB RAM and No-GPU/32GB RAM**: **no image-generation model in this research is confirmed
  to produce output in anything resembling interactive time on CPU alone.** SD1.5 via
  `stable-diffusion.cpp` is the one model with a sourced CPU figure, and that figure is **5–20 minutes
  per single 512×512 image** — technically it runs, but per the spec's own SC-001 framing ("without
  performance glitches") this should be offered, if at all, labelled explicitly as a slow/batch-only
  option, never presented alongside the GPU tiers as comparable. Every other candidate in this table
  either has no sourced CPU figure (treat as unproven, do not offer) or is architecturally GPU-only in
  how this repo currently wires it (FLUX via Nunchaku/diffusers).
- **8 GB VRAM**: the full-precision SD3.5-Large, and every FLUX.2 variant except klein-4B, do not fit;
  FLUX.1-schnell/dev fit ONLY via the NVFP4 path this repo has already built but not yet proven on
  hardware — treat as ⚠️ PENDING, not ✅, until `docs/qa/phase4_imagegen_20260707` produces a real
  measured peak.
- **16 GB VRAM**: FLUX.2-dev (32B) does not fit even at Q4 GGUF (needs ≈19 GB, no headroom at 16 GB);
  Qwen-Image needs the FP8 tier (~16 GB) as its floor here, not the smaller quantised variants alone —
  those are for the 8 GB tier and slower/lower-quality.
- **24 GB+ VRAM**: even here, Qwen-Image's own **full BF16 weight (~40 GB)** does not fit — the
  catalogue must still select the FP8 or GGUF variant on a 24 GB host, never the raw BF16 checkpoint.

---

## 4. Direct implications for `/speckit-plan`

1. **Extend, don't replace, `internal/vrambroker` + `cmd/visiongen-boot` / `cmd/imagegen-boot`.** The
   `ClassVLM` (warm) and `ClassImage` (burst) lease shapes already match the spec's lifecycle
   requirements (FR-044..FR-047 idle-unload / LRU-eviction / no-evict-while-serving) — the plan should
   treat this as the mechanism to generalise across the whole catalogue, not a thing to redesign.
2. **The catalogue needs a `verified_variant` field, not just a model name**, especially for image
   generation: several candidates (FLUX.1, FLUX.2-dev, Qwen-Image) span a 5×–8× VRAM range purely by
   quantisation choice. FR-014's naming scheme (`helixllm/<host>/<model>[:<variant>]`) already has a
   variant slot — this research confirms it is load-bearing, not decorative.
3. **Two PENDING real-hardware proofs block "done" for either family**: the visiongen 7B override is
   unmeasured, and the imagegen FLUX path has never generated a real image on hardware yet (per the
   repo's own `docs/qa/phase4_imagegen_20260707/README.md` "PENDING" markers). Per FR-051..053
   (no partial delivery) and this project's §11.4.108 runtime-signature discipline, the plan must
   schedule these two captures explicitly, not assume the SCAFFOLD status resolves itself.
4. **Licence heterogeneity is a selection input, not a footnote.** SDXL-Turbo, MiniCPM-V, FLUX.1-dev,
   FLUX.2-dev/klein-9B, and PaliGemma2 all carry non-Apache/MIT terms (research-only, revenue caps,
   registration requirements, or RAIL use restrictions). FR-010's "allowlisted sources" plus this
   file's per-model licence column give the plan what it needs to keep a permissive default catalogue
   (Qwen-family VLMs, FLUX.1-schnell, FLUX.2-klein-4B, Qwen-Image, SD1.5/SDXL-under-RAIL) separate from
   opt-in restricted entries, mirroring the pattern `imagegen_server.py` already uses for FLUX.1-dev.
5. **A genuine CPU-only image-generation tier does not exist yet in this repo** and, per the research
   above, may not be worth adding beyond SD1.5-via-`stable-diffusion.cpp` labelled honestly as
   minutes-per-image. The plan should decide explicitly whether User Story 5's "every family complete"
   requirement is satisfied by that slow-but-real option on the two no-GPU host classes, or whether
   image generation is the one family that is allowed to state "no usable option on this host" per
   FR-028's "state plainly when a requested model cannot be served" — this is a decision for the plan,
   not something this research file should resolve unilaterally.

---

## Sources

Accessed 2026-09-02 (web search results reflect the live state of each source at query time; per
§11.4.99 these are messenger/vendor/AI-stack-adjacent sources and carry a 90-day staleness bound —
re-verify before citing as authority beyond that window).

**Already in this repo (read directly, not searched):**
- `submodules/helix_llm/cmd/visiongen-boot/compose.vision.yml`, `vision_boot_test.sh`
- `submodules/helix_llm/docs/qa/phase3_vision_20260707/RESULTS.md`
- `submodules/helix_llm/services/imagegen/imagegen_server.py`
- `submodules/helix_llm/docs/qa/phase4_imagegen_20260707/README.md`
- `submodules/helix_llm/cmd/imagegen-boot/main.go`
- `submodules/helix_llm/docs/VRAM_BROKER.md`

**Vision (VLM):**
- Qwen2.5-VL GGUF family — https://huggingface.co/DhruvalLabs/Qwen2.5-VL-7B-Instruct-GGUF ,
  https://huggingface.co/bartowski/Qwen_Qwen2.5-VL-7B-Instruct-GGUF
- Qwen2.5-VL-32B-Instruct — https://huggingface.co/Qwen/Qwen2.5-VL-32B-Instruct
- Qwen3-VL release/licence — https://github.com/qwenlm/qwen3-vl ,
  https://docs.kanaries.net/articles/qwen3-vl
- Qwen3-VL-8B-Instruct-GGUF — https://huggingface.co/Qwen/Qwen3-VL-8B-Instruct-GGUF
- Qwen3-VL-4B-Instruct-GGUF — https://huggingface.co/Qwen/Qwen3-VL-4B-Instruct-GGUF
- MiniCPM-V-4.5 GGUF — https://huggingface.co/openbmb/MiniCPM-V-4_5-gguf
- InternVL3-8B — https://huggingface.co/OpenGVLab/InternVL3-8B
- Moondream2 — https://huggingface.co/vikhyatk/moondream2 ,
  https://roboflow.com/model/moondream-2
- SmolVLM2 — https://huggingface.co/HuggingFaceTB/SmolVLM2-2.2B-Instruct ,
  https://huggingface.co/blog/smolvlm2
- Phi-4-multimodal-instruct — https://huggingface.co/microsoft/Phi-4-multimodal-instruct
- PaliGemma 2 — https://huggingface.co/blog/paligemma2 ,
  https://huggingface.co/google/paligemma2-3b-pt-224
- llama.cpp multimodal (libmtmd) support list — https://raw.githubusercontent.com/ggml-org/llama.cpp/master/docs/multimodal.md ,
  https://deepwiki.com/ggml-org/llama.cpp/6.5-multimodal-support-(libmtmd) ,
  https://github.com/ggml-org/llama.cpp/discussions/19516

**Image generation:**
- Stable Diffusion 1.5 licence/card — https://huggingface.co/stable-diffusion-v1-5/stable-diffusion-v1-5
- SD1.5 VRAM/CPU timing — https://willitrunai.com/image-models/sd-1-5 ,
  https://www.thundercompute.com/blog/how-to-run-stable-diffusion
- `stable-diffusion.cpp` — https://github.com/leejet/stable-diffusion.cpp ,
  https://www.emergentmind.com/topics/stable-diffusion-cpp
- PixArt-Σ — https://huggingface.co/PixArt-alpha/PixArt-Sigma-XL-2-1024-MS ,
  https://huggingface.co/docs/diffusers/main/en/api/pipelines/pixart_sigma ,
  https://github.com/PixArt-alpha/PixArt-sigma/blob/master/LICENSE
- SDXL 1.0 — https://huggingface.co/stabilityai/stable-diffusion-xl-base-1.0 ,
  https://stability.ai/news/stable-diffusion-sdxl-1-announcement
- SDXL-Turbo — https://willitrunai.com/image-models/sdxl-turbo ,
  https://gigagpu.com/sdxl-turbo-vram-requirements/
- Stable Diffusion 3.5 — https://stability.ai/news-updates/introducing-stable-diffusion-3-5 ,
  https://www.spheron.network/tools/gpu-recommender/stabilityai/stable-diffusion-3.5-medium/
- FLUX.1 VRAM (bf16) — https://huggingface.co/black-forest-labs/FLUX.1-dev/discussions/52 ,
  https://localaimaster.com/blog/flux-vram-requirements-by-gpu
- FLUX.2 (klein + dev) — https://github.com/black-forest-labs/flux2 ,
  https://bfl.ai/blog/flux-2 , https://bfl.ai/licensing ,
  https://www.marktechpost.com/2026/01/16/black-forest-labs-releases-flux-2-klein-compact-flow-models-for-interactive-visual-intelligence/ ,
  https://www.spheron.network/tools/gpu-recommender/black-forest-labs/FLUX.2-klein-4B/ ,
  https://willitrunai.com/blog/flux-2-klein-9b-vram-requirements
- Qwen-Image — https://github.com/QwenLM/Qwen-Image ,
  https://huggingface.co/Qwen/Qwen-Image ,
  https://localaimaster.com/models/qwen-image-local-guide
