# Research 03 — Speech & Audio Model Serving (STT, TTS, General Audio)

Input for `/speckit-plan` on `specs/002-adaptive-local-model-serving/spec.md` — covers the
"Audio generating or recognition, Text to speech, Speech to text" portion of the modality
families named in the spec (Clarification Q1: **all families in v1**, FR-051..053: no
partial/advertised-but-unserved delivery).

## 0. Starting-point confirmation (load-bearing for effort estimate)

Measured directly against `submodules/helix_llm` on 2026-09-02, per the instruction to confirm
this myself rather than take it on faith:

- **Text-to-speech (TTS): zero implementation.** No file under `internal/`, `cmd/`, `docs/`, or
  `container/` references any TTS engine, model, or library (`grep -ril -E
  "\btts\b|text-to-speech|piper|coqui|xtts"` over `internal/ cmd/ docs/` returns nothing, and no
  non-test hit anywhere in the tree). **This family is entirely new work.**
- **General audio generation / recognition (music, sound effects, non-speech audio
  classification): zero implementation.** No music-generation, sound-effect, or audio-tagging
  library reference anywhere in the tree. **This family is entirely new work.**
- **Speech-to-text (STT): not zero, but not integrated either — a narrower gap than "new work"
  but still load-bearing.** There is exactly one artifact: `container/whisper_stt_server.py` (131
  lines) + `container/Containerfile.whisper`, a self-contained CPU-only `faster-whisper`
  OpenAI-compatible transcription microservice (model/device/compute-type env-configurable,
  VAD + no-speech-probability hallucination guards, proven working per the citation in its own
  header at `docs/qa/phase3_whisper_stt_20260707/RESULTS.md` — that specific QA directory was not
  found in the current tree at research time, so treat the "proven" claim as the file's own
  historical record, not something this research re-verified). `cmd/laneconfig-validate/validate.go`
  reserves port `18437` for `helixllm-whisper` alongside the other lane services. **What is
  missing**: it is not wired into `internal/brain/models` (no catalog/registry entry, no modality
  flag, no hardware-aware selection path, no naming per the `helixllm/<host>/<model>[:<variant>]`
  scheme this spec requires under FR-014). So STT has a reusable, already-proven CPU inference
  artifact to extend (§11.4.74 extend-don't-reimplement) but the catalog/selection/naming/discovery
  integration this spec demands (FR-001..050) is unbuilt for all three families alike. Effort
  planning should **not** assume STT is "already done" — the serving primitive exists for one
  engine on one execution path; the hardware-aware multi-engine, multi-family selection and
  exposure machinery does not exist for any of the three families in this document.

Net: for planning purposes, treat speech + audio as **three new families of roughly comparable
integration effort**, with STT carrying a modest head start (one already-proven CPU container to
wrap rather than build from scratch) and TTS / general-audio carrying none.

---

## A. Speech-to-text (STT / transcription)

### A.1 Candidate models

| Model | Params | Licence | Memory footprint (fp16/native) | Min usable GPU VRAM | CPU+RAM viability | Streaming vs batch | Language coverage |
|---|---|---|---|---|---|---|---|
| **Whisper tiny** (OpenAI) | 39M | MIT | ~1 GB VRAM fp16 | ~1 GB | Runs on almost anything incl. Raspberry Pi / old laptops | Batch natively; streaming via wrapper (below) | 99 languages (multilingual) |
| **Whisper base** | 74M | MIT | ~1 GB | ~1 GB | CPU-viable everywhere | Batch + wrapper streaming | 99 languages |
| **Whisper small** | 244M | MIT | ~2 GB | ~2 GB | **CPU-viable, near real-time**: ~6–8x real-time on a modern (12th/13th-gen i7-class) CPU; ~0.35 RTF (i.e. ~2.9x real-time) CPU-only on Apple M2 | Batch + wrapper streaming | 99 languages |
| **Whisper medium** | 769M | MIT | ~5 GB VRAM; ~1.5 GB disk (GGML) / ~2.1 GB RAM at runtime (int8/ggml) | ~5 GB | CPU-viable but slower than real-time on mid-range CPUs | Batch + wrapper streaming | 99 languages |
| **Whisper large-v3** | 1550M (1.5B) | MIT | ~10 GB VRAM fp16 (some sources cite ~3.4 GB fp16 including KV cache for the CTranslate2 path — figures diverge by framework; treat ~10 GB as the conservative HF/native figure) | ~10 GB (native); ~6 GB for the **large-v3-turbo** distilled variant | CPU-viable but slow: RTF ~2.5–3.0 on i9-12900K / mid-range Ryzen (i.e. slower than real-time — usable for batch, not live) | Batch natively | 99 languages |
| **distil-whisper (large-v3 distilled)** | ~50% smaller than large-v3 | MIT (distillation of MIT-licensed Whisper) | Roughly half of large-v3's footprint | ~5 GB (approx., not separately re-verified) | Better CPU fit than large-v3: ~6x faster than large-v3, within ~1% WER on long-form | Batch + wrapper streaming | Inherits Whisper's multilingual training but distillation typically narrows robustness on the tail languages — **UNVERIFIED**: exact per-language WER delta vs large-v3 not sourced here |
| **NVIDIA Parakeet-TDT-0.6B-v3** | 627M (~0.6B) | **CC-BY-4.0** (permits commercial use) | FP16 ~1–1.4 GB VRAM; int4 ~0.3 GB; CPU int8 ~670 MB disk / ~2 GB RAM | ~1 GB (tiny GPU footprint — leaves headroom for concurrent models) | **CPU-viable, batch-oriented**: RTF ~0.3–0.5 on a recent x86 CPU (e.g. Ryzen 9) with int8 quantization and 4-thread inference — i.e. 2–3x real-time, "usable for offline batch, not real-time interactive" per source | Primarily batch/high-throughput; a `sherpa-onnx` streaming int8 build exists (see A.2) | **25 European languages** (English + 24 others) — **not** Whisper's 99-language breadth |
| **NVIDIA Canary-Qwen-2.5B** | 2.5B | Check NVIDIA's model card at acquisition time — **UNVERIFIED** exact licence terms not independently re-confirmed here beyond source claims | ~8 GB VRAM | ~8 GB (tight fit on an 8 GB card once other models are loaded) | **UNVERIFIED**: no CPU RTF figure was found in this research pass — treat CPU viability as unconfirmed, plan for GPU-only until verified | Batch | English-only (leaderboard-topping English accuracy: 5.63% avg WER) |

### A.2 Streaming architecture notes

Whisper and Parakeet are natively **batch/chunk** models (they transcribe a fixed audio buffer);
genuine low-latency streaming is bolted on via a wrapper, not a property of the base weights:

- **RealtimeSTT** (open source) recommends pairing a fast partial-result model
  (`sherpa-onnx-nemotron-3.5-asr-streaming-0.6b-560ms-int8`) for live partial text with
  `sherpa-onnx-nemo-parakeet-tdt-0.6b-v3-int8` for the authoritative final transcript — a
  two-model streaming pattern, not single-model streaming.
- **Whisper-Flow** (open source) reports ~275ms average latency / 7% WER on consumer hardware
  (M1 MacBook Air) using a chunked-Whisper streaming wrapper.
- Mistral's **Voxtral Transcribe** (Feb 2026, Apache-2.0, 4B params) is a genuinely
  streaming-native architecture rather than a chunking wrapper over a batch model — worth a
  follow-up research pass if HelixLLM wants a single-model streaming path instead of the
  two-model RealtimeSTT pattern. **UNVERIFIED**: VRAM/CPU figures for Voxtral Transcribe were not
  gathered in this pass.

### A.3 STT tiered picks by host class

| Host class | Recommended pick(s) | Notes |
|---|---|---|
| **No-GPU, 8 GB RAM** | `faster-whisper small` (int8, CPU) — ~2 GB RAM, ~6x real-time on a decent CPU | Parakeet CPU int8 also fits (~2 GB RAM) but is batch-oriented (RTF 0.3–0.5) and English/European-only; prefer Whisper-small for broader language coverage on this tier, Parakeet if the workload is English/European-only batch transcription |
| **No-GPU, 32 GB RAM** | `distil-whisper large-v3` (CPU) or `faster-whisper medium` | Full `large-v3` CPU-only is present but slow (RTF ~2.5–3, i.e. not real-time) — reserve for overnight/batch jobs on this tier, not interactive use |
| **8 GB VRAM** | `Parakeet-TDT-0.6B-v3` (GPU, ~1 GB VRAM — huge headroom left for a concurrent TTS model) for English/European; `Whisper large-v3-turbo` (~6 GB VRAM) for full 99-language coverage | This tier can comfortably run STT + a TTS model concurrently because Parakeet's footprint is so small |
| **16 GB VRAM** | `Whisper large-v3` (~10 GB) or `Canary-Qwen-2.5B` (~8 GB, English-only, best-in-class accuracy) alongside a TTS model | Room for STT + TTS + a small vision or LLM model concurrently |
| **24 GB+ VRAM** | Any combination, including running Whisper large-v3 + Parakeet + Canary-Qwen simultaneously for ensemble/fallback serving | No meaningful ceiling at this tier for STT alone |

**What does not fit anywhere in this range**: full-multilingual (99-language) **real-time
interactive** transcription on CPU-only hardware at any RAM tier tested — the only CPU path that
reaches real-time-or-better is Whisper `small` (99 languages, ~6–8x RT) or Parakeet int8 (25
languages, batch-oriented, 2–3x RT); anything larger than `small`/Parakeet-0.6B on CPU alone drops
below real-time and is batch-only. Canary-Qwen's CPU viability is unconfirmed (UNVERIFIED) —
plan it as GPU-only (≥8 GB VRAM) until independently verified.

---

## B. Text-to-speech (TTS)

### B.1 Candidate models

| Model | Params | Licence | Memory footprint | GPU/CPU viability | Voice cloning | Streaming | Quality tier | Latency to first audio |
|---|---|---|---|---|---|---|---|---|
| **Piper** | Small, per-voice ONNX models (tens of MB) | Was MIT under `rhasspy/piper` (now read-only, Oct 2025); active fork `OHF-Voice/piper1-gpl` is **GPL-3.0** as of v1.6.0 (2026-07-23) — copyleft, check before bundling into closed-source distribution | Minimal — runs on a Raspberry Pi 5 | **CPU-first by design** — RTF ~0.192 on Raspberry Pi 5 (≈5x real-time), CPU-only, no GPU needed | **No zero-shot cloning** — cloning requires 20–60 min of target-voice recordings + a GPU fine-tune job | Real-time streaming supported; ~30–50ms first-audio latency reported | Good/solid for embedded and assistant-style use, not SOTA naturalness | ~30–50ms |
| **Kokoro-82M** | 82M | **Apache-2.0** (fully commercial) | <2 GB VRAM; CPU-viable | **CPU-viable at usable speed**: ~5x real-time on a 32-core CPU, 36x real-time on a free Colab T4 GPU | **No voice cloning** — fixed catalogue of 54 baked-in voices only | Yes — lowest first-audio latency of any model surveyed (28ms on RTX 5090; ~46ms server-side on its own tier) | High naturalness for its size (leads UTMOS among surveyed models per one source) despite zero cloning | ~28–46ms (GPU); CPU figure not separately sourced — **UNVERIFIED** exact CPU first-chunk latency |
| **Chatterbox** (Resemble AI) | ~500M | **MIT** (fully commercial) | ~8 GB VRAM | GPU-oriented; no CPU RTF figure sourced (**UNVERIFIED**) | **Yes** — zero-shot cloning from ~5 seconds of audio, plus emotion control; 23 languages | Not explicitly confirmed streaming-capable in sources gathered — **UNVERIFIED** | Competitive with commercial APIs per one source (e.g. cited as beating ElevenLabs on a stated metric) | Not sourced — **UNVERIFIED** |
| **XTTS-v2** (Coqui) | — (1.88 GB weights at fp32) | **CPML** (Coqui Public Model License) — **non-commercial in practice**: Coqui Inc shut down Jan 2024, so no one can currently sell a commercial CPML licence. Treat as non-commercial-only for a shipping product. | ~3–4 GB VRAM at fp16 per worker; 6+ GB recommended, 8 GB for stable faster-than-real-time | GPU-oriented; CPU inference is reported to use 2–3x the RAM of the GPU/VRAM figure and is not the intended path | **Yes** — clones from a ~6-second sample, 17 languages, cross-language cloning | Full streaming support, ~200ms time-to-first-chunk | Best cloning quality among CPU/consumer-GPU-tier models per multiple sources | ~200ms |
| **F5-TTS** | — | **CC-BY-NC 4.0** (non-commercial) | ~3 GB VRAM (2,994 MB reported) | GPU-oriented, most resource-efficient of the cloning-capable models surveyed | Yes (cloning-capable; exact zero-shot sample length not sourced here — **UNVERIFIED**) | Not confirmed in sources gathered — **UNVERIFIED** | Competitive quality per source, resource-efficient | Not sourced — **UNVERIFIED** |
| **Fish Speech (v1.5)** | — | Cited elsewhere as Apache-2.0/fully-commercial in one comparison table, but not independently re-confirmed against Fish Speech's own repo in this pass — **UNVERIFIED**, re-check before relying on it for a commercial default | Tested on RTX 3060-class hardware per source (implies ~12 GB-class card, not a precise VRAM figure) | GPU-oriented | Yes — cited as "industry-leading multilingual performance" | Not confirmed — **UNVERIFIED** | High (multilingual-focused) | Not sourced — **UNVERIFIED** |
| **CosyVoice2-0.5B** | 0.5B | Not independently re-confirmed in this pass — **UNVERIFIED**, check Alibaba's CosyVoice2 repo licence before use | Tested on RTX 3060-class hardware per source | GPU-oriented | Voice conversion / cloning-adjacent capability cited, exact zero-shot cloning claim not separately confirmed — **UNVERIFIED** | **Unified streaming + non-streaming framework** — explicitly designed for both | High, with emotional-control cited | Not sourced — **UNVERIFIED** |
| **OpenVoice V2** | — | **MIT** (research + commercial) | Lightweight per source, no specific VRAM figure sourced — **UNVERIFIED** | Cited as suited to real-time, zero-shot use; CPU viability not confirmed — **UNVERIFIED** | **Yes** — real-time zero-shot voice cloning is its headline feature | Cited as low-latency-oriented | Good for prototyping/live-demo use per source | Not sourced — **UNVERIFIED** |

### B.2 Licensing is the load-bearing constraint for TTS, not just VRAM

Unlike STT (where every strong candidate — Whisper, Parakeet — is permissively licensed), the
TTS field splits sharply:

- **Fully commercial-safe with cloning**: Chatterbox (MIT), OpenVoice V2 (MIT).
- **Fully commercial-safe, no cloning**: Kokoro (Apache-2.0, fixed voices only), Piper (GPL-3.0 —
  usable commercially but copyleft terms must be honoured if bundled into closed-source
  distribution — worth flagging explicitly for HelixCode's distribution model).
- **Cloning-capable but non-commercial or licence-orphaned**: XTTS-v2 (CPML, effectively
  non-commercial since the licensor shut down) and F5-TTS (CC-BY-NC 4.0). These should **not** be
  the default cloning option in a product HelixCode ships to users, even though they are the most
  commonly cited "best cloning quality" open weights — they are research/self-hosting-only choices
  unless HelixCode is comfortable operating under a non-commercial licence for that specific
  serving path.

**Recommendation for planning**: default cloning-capable TTS to **Chatterbox (MIT)**, default
non-cloning fast/embedded TTS to **Kokoro (Apache-2.0)** for CPU/low-VRAM tiers and **Piper
(GPL-3.0)** only where the lower dependency weight and Raspberry-Pi-class footprint matters more
than avoiding copyleft.

### B.3 TTS tiered picks by host class

| Host class | Recommended pick(s) | Notes |
|---|---|---|
| **No-GPU, 8 GB RAM** | **Piper** (fixed voices, GPL-3.0, ~30–50ms latency, minimal RAM) | No cloning available at this tier under any surveyed model |
| **No-GPU, 32 GB RAM** | **Kokoro-82M** (CPU, Apache-2.0, ~5x real-time on a 32-core CPU) as primary; Piper as a lighter-weight fallback | Still no cloning — every cloning-capable model surveyed is GPU-oriented |
| **8 GB VRAM** | **Chatterbox** (MIT, ~8 GB VRAM, cloning) if cloning is required; **Kokoro** (GPU) for lowest latency, no cloning | XTTS-v2 also fits VRAM-wise (~4–6 GB) but its licence makes it a non-commercial-only choice |
| **16 GB VRAM** | Chatterbox or CosyVoice2 alongside a concurrent STT model | Comfortable headroom for STT + TTS running together |
| **24 GB+ VRAM** | Any combination — multiple concurrent TTS workers, or Chatterbox + Fish Speech/CosyVoice2 ensemble | No meaningful ceiling |

**What does not fit anywhere in this range**: commercially-licensed, high-fidelity voice cloning
on CPU-only hardware at any RAM tier — every cloning-capable model surveyed (Chatterbox, XTTS-v2,
F5-TTS, Fish Speech, CosyVoice2, OpenVoice) is GPU-oriented, and no CPU RTF figure for any of them
was found in this research pass. A host with no accelerator can get fast, natural, **non-cloning**
speech (Kokoro/Piper) but not commercial-grade cloning at any RAM size tested.

---

## C. General audio generation / recognition

This family genuinely splits into two sub-families with very different maturity, resource floors,
and licensing risk — treating them as one family (as the spec's taxonomy currently does) risks an
empty-or-broken option set on low-end hosts. Stating this plainly per the task's instruction: **do
not pad this section to look uniformly complete.**

### C.1 Audio classification / recognition (non-speech sound events) — MATURE, cheap, CPU-viable

| Model | Params | Licence | Memory footprint | GPU/CPU viability | Notes |
|---|---|---|---|---|---|
| **PANNs (CNN14)** | ~81M | Open (research-released; original repo does not impose a restrictive commercial licence beyond attribution — **not independently re-verified against the exact licence file in this pass**, flag as UNVERIFIED-licence-text-not-read though the model is widely self-hosted) | ~21G MACs per 10s clip — small, no VRAM requirement of note | **CPU-viable on essentially any host**, including the 8 GB RAM / no-GPU tier | Trained on AudioSet (527 sound-event classes: doorbell, glass breaking, engine, speech-vs-music, etc.) — this is "what sound is this," not speech transcription |
| **CLAP (CNN14 audio encoder variant)** | ~80.8M audio encoder | Open-research release (LAION/Microsoft variants exist; exact licence not independently re-verified in this pass — UNVERIFIED) | Comparable to PANNs — small | CPU-viable everywhere | Adds zero-shot audio-to-text-concept matching (natural-language audio search/classification) on top of PANNs-class tagging |
| **YAMNet** | Small (MobileNet-based) | Apache-2.0 (TensorFlow Model Garden) | Tiny — mobile-targeted | CPU-viable everywhere, including edge/mobile | Same AudioSet-class-tagging use case as PANNs, TensorFlow-native |

This sub-family is **complete and cheap enough to offer on every host class**, including the
smallest (no-GPU, 8 GB RAM) — it should not be gated behind a GPU tier in the catalogue.

### C.2 Music / sound-effect generation — real, but narrow, GPU-gated, and licence-fraught

| Model | Params (usable variant) | Licence | Memory footprint | GPU/CPU viability | Notes |
|---|---|---|---|---|---|
| **MusicGen** (Meta) | Small/Large variants | **CC-BY-NC 4.0** — non-commercial; using its output in a commercial product is out of licence regardless of self-hosting | Small ~8 GB VRAM; Large/Stereo ~12 GB VRAM at fp16 | GPU-oriented; no CPU RTF found (**UNVERIFIED**, and diffusion/transformer audio-gen models are not typically CPU-real-time-viable) | Do not default to this for a shipping product because of the output-licence restriction, even though weights are freely downloadable |
| **Stable Audio Open 1.5** | — | Stability AI Community Licence — **commercial use permitted under a revenue threshold** (not unconditionally free) | ~12 GB VRAM | GPU-oriented | Trained on Freesound (CC0/CC-BY/CC-BY-NC mixed sources) — check the Community Licence's revenue cap before defaulting to this for a commercial HelixCode deployment |
| **ACE-Step (2B variant)** | 2B | **Apache-2.0** (fully commercial, no output restriction) | **<4 GB VRAM** | GPU-oriented; smallest VRAM floor of the generation-capable models surveyed | The cleanest licence + lowest entry cost of the three — recommend this as the sole default if music/SFX generation ships at all |
| **ACE-Step (XL / 4B DiT variant)** | 4B | Apache-2.0 | ~12 GB VRAM minimum, 20 GB+ recommended | GPU-oriented, high tier only | Higher-quality variant, gated to the 16 GB+/24 GB+ host classes |

**Finding, stated plainly**: there is **no mature, CPU-viable, low-VRAM open-weight option** for
music/sound-effect generation. Every candidate surveyed needs a GPU with at least ~4 GB VRAM
(ACE-Step 2B, the cheapest), and two of the three strongest candidates (MusicGen, Stable Audio
Open) carry licence terms that make them a poor default for a product distributing generated
output commercially. If this sub-family ships in v1 at all, **ACE-Step 2B is the only candidate
that is simultaneously open, commercially clean, and fits below the 8 GB VRAM host tier** — every
other candidate either needs more VRAM, carries a non-commercial/revenue-capped licence, or both.

### C.3 General-audio tiered picks by host class

| Host class | Classification (C.1) | Generation (C.2) |
|---|---|---|
| **No-GPU, 8 GB RAM** | PANNs / CLAP / YAMNet — all fit comfortably | **Nothing fits** — no candidate surveyed runs acceptably on CPU |
| **No-GPU, 32 GB RAM** | Same as above, more headroom for concurrent classification workloads | **Nothing fits** — same gap; more RAM does not substitute for a GPU here |
| **8 GB VRAM** | Trivial (classification models are tiny relative to this tier) | **ACE-Step 2B** (<4 GB VRAM, Apache-2.0) — the one candidate that fits and is licence-clean |
| **16 GB VRAM** | Trivial | ACE-Step 2B comfortably, or Stable Audio Open 1.5 (~12 GB, check revenue-threshold licence terms) |
| **24 GB+ VRAM** | Trivial | ACE-Step XL/4B (~12–20 GB+), Stable Audio Open, or MusicGen (self-hosting/research use only given its licence) |

### C.4 Recommendation for the spec's family taxonomy

Given C.1 and C.2 have essentially opposite resource profiles (classification: trivial on every
tier; generation: unavailable below 8 GB VRAM and licence-sensitive even above it), the planning
phase should consider **splitting "audio generating or recognition" into two catalogue-visible
sub-families** rather than one. Under the spec's own FR-003 ("a host with no accelerator... MUST
receive real options, never an empty set") and the Edge Cases/SC-010 requirement that a no-GPU
host still get a real, non-empty option set per family, treating generation and recognition as one
family would force a choice between (a) violating FR-003 for that family on no-GPU hosts, since
generation genuinely has nothing to offer there, or (b) papering over the split by only ever
surfacing classification and never being honest that "generation" is GPU-gated. Splitting them
lets classification honestly satisfy FR-003 on every tier while generation is honestly and
explicitly labelled as GPU-only per FR-028 ("state plainly when a requested model cannot be served
by any available path on that host").

---

## Sources

All accessed 2026-09-02.

- [Best open source speech-to-text (STT) model in 2026 (with benchmarks) — Northflank](https://northflank.com/blog/best-open-source-speech-to-text-stt-model-in-2026-benchmarks)
- [Parakeet vs Whisper vs Nemotron: Best Local STT 2026 — OpenWhispr](https://openwhispr.com/blog/parakeet-vs-whisper-vs-nemotron)
- [Parakeet.cpp vs Whisper: Best Self-Hosted ASR in 2026 — ModelsLab](https://modelslab.com/blog/audio-generation/parakeet-cpp-vs-whisper-self-hosted-asr-comparison-2026)
- [Self-Hosted Whisper in 2026: Local AI Transcription Guide — Digital Applied](https://www.digitalapplied.com/blog/local-speech-to-text-whisper-self-hosted-transcription-2026)
- [Offline Speech-to-Text 2026: Best Local and Self-Hosted Options Tested — DIYAI](https://diyai.io/ai-tools/speech-to-text/offline-speech-to-text/)
- [Whisper.cpp vs faster-whisper 2026: STT Speed Test — PromptQuorum](https://www.promptquorum.com/power-local-llm/local-whisper-stt-comparison-2026)
- [Best Local Speech-to-Text Models in 2026: Moonshine vs Parakeet vs Whisper — OnResonant](https://www.onresonant.com/resources/local-stt-models-2026)
- [Parakeet vs Whisper 2026: Faster Local Speech-to-Text? — LocalAIMaster](https://localaimaster.com/blog/parakeet-vs-whisper)
- [Whisper vs Parakeet TDT (2026): ~4x faster on CPU — SnailText](https://snailtext.app/blog/whisper-vs-parakeet-tdt/)
- [Best Open Source Speech-to-Text Models in 2026 — AssemblyAI](https://www.assemblyai.com/blog/top-open-source-stt-options-for-voice-applications)
- [Best open-source speech-to-text models in 2026 — Gladia](https://www.gladia.io/blog/best-open-source-speech-to-text-models)
- [Top Open-Source AI Speech-to-Text Models in 2026 — Resemble AI](https://www.resemble.ai/resources/open-source-ai-speech-to-text-models)
- [Memory requirements? · openai/whisper Discussion #5 — GitHub](https://github.com/openai/whisper/discussions/5)
- [openai/whisper-large-v3 — Automated Model Memory Requirements — Hugging Face](https://huggingface.co/openai/whisper-large-v3/discussions/83)
- [whisper-large-v3 VRAM Requirements — Spheron](https://www.spheron.network/tools/gpu-recommender/openai/whisper-large-v3/)
- [Whisper Model Sizes: Complete Guide — OpenWhispr](https://openwhispr.com/blog/whisper-model-sizes-explained)
- [Whisper Model Sizes 2026: Tiny to Large V3 Turbo — Spokenly](https://spokenly.app/blog/whisper-model-sizes)
- [Whisper VRAM Requirements (Tiny to Large-v3) — GIGAGPU](https://gigagpu.com/whisper-vram-requirements/)
- [parakeet-tdt-0.6b-v3 VRAM Requirements — Spheron](https://www.spheron.network/tools/gpu-recommender/mlx-community/parakeet-tdt-0.6b-v3/)
- [achetronic/parakeet — Whisper-compatible ASR server using NVIDIA Parakeet TDT 0.6B (ONNX) — GitHub](https://github.com/achetronic/parakeet)
- [nvidia/parakeet-tdt-0.6b-v3 — Hugging Face](https://huggingface.co/nvidia/parakeet-tdt-0.6b-v3)
- [parakeet-tdt-0.6b-v3 - 627M | GPU Requirements — vram.run](https://vram.run/model/nvidia/parakeet-tdt-0.6b-v3/)
- [What Hardware Runs NVIDIA Parakeet TDT 0.6B v3 — MadeByAgents](https://www.madebyagents.com/models/nvidia-parakeet-tdt-0-6b-v3)
- [Whisper.cpp Benchmark Report: Intel Core i5-460M — ggml-org/whisper.cpp Discussion #3752](https://github.com/ggml-org/whisper.cpp/discussions/3752)
- [Choosing a Real-Time Whisper Engine — Allen Kuo, Medium](https://allenkuo.medium.com/choosing-a-real-time-whisper-engine-c4eeb5885e22)
- [faster-whisper vs whisper.cpp vs OpenAI Whisper (2026) — CodersEra](https://codersera.com/blog/faster-whisper-vs-whisper-cpp-speech-to-text-2026/)
- [whisper.cpp Benchmark: Speed & Accuracy on Apple Silicon — GetSpeakUp](https://getspeakup.app/blog/whisper-cpp-benchmark-mac/)
- [Do you need a GPU for voice-to-text? Benchmarks say no (2026) — SnailText](https://snailtext.app/blog/do-you-need-a-gpu-for-voice-to-text/)
- [OpenAI Realtime Whisper Streaming API Explained (May 2026) — BibiGPT](https://bibigpt.co/en/features/openai-realtime-whisper-streaming-explained)
- [KoljaB/RealtimeSTT — GitHub](https://github.com/KoljaB/RealtimeSTT)
- [Voxtral vs Whisper 2026: WER Benchmarks, Streaming & Hardware — Weesper Neon Flow](https://weesperneonflow.ai/en/blog/2026-03-31-voxtral-whisper-open-source-speech-models-comparison-2026/)
- [Whisper-Flow: Real-Time Transcription Made Simple — BrightCoding](https://converter.brightcoding.dev/blog/whisper-flow-real-time-transcription-made-simple)
- [Best Open Source Self-Hosted Text-to-Speech Models in 2026 — Pinggy](https://pinggy.io/blog/best_open_source_self_hosted_text_to_speech_models/)
- [The Best Open-Source Text-to-Speech Models in 2026 — BentoML](https://www.bentoml.com/blog/exploring-the-world-of-open-source-text-to-speech-models)
- [Best Self-Hosted TTS: Open Source vs. On-Premise Voice AI (2026) — Inworld AI](https://inworld.ai/resources/best-self-hosted-tts)
- [Top 5 Open-Source AI Voice & Text-to-Speech LLMs in 2026 — Second Talent](https://www.secondtalent.com/resources/open-source-ai-voice-generators/)
- [Best TTS Models 2026: Open-Source Voice AI Compared — CodeSOTA](https://www.codesota.com/guides/tts-models)
- [Best Local TTS Models 2026: 8 Open-Source Voices Tested — LocalAIMaster](https://localaimaster.com/blog/best-local-tts-models)
- [Deploy Open-Source TTS on GPU Cloud: Kokoro, Fish Speech, and Hume TADA Guide (2026) — Spheron](https://www.spheron.network/blog/deploy-open-source-tts-gpu-cloud-2026/)
- [Which Open Source Text-to-Speech Model Should You Use? — DataRoot Labs](https://datarootlabs.com/blog/text-to-speech-models)
- [Kokoro vs Piper vs XTTS v2: Local Text to Speech on M5 Max (2026) — Contra Collective](https://contracollective.com/blog/kokoro-vs-piper-vs-xtts-local-text-to-speech-m5-max-2026)
- [Kokoro TTS Local Setup (2026): Tiny 82M Open Voice Model — LocalAIMaster](https://localaimaster.com/blog/kokoro-tts-local-setup)
- [Kokoro TTS — Clore.ai docs](https://docs.clore.ai/guides/audio-and-voice/kokoro-tts)
- [Kokoro 82M — PolarGrid](https://polargrid.mintlify.app/models/kokoro-82m)
- [TTS Latency Benchmarks — GIGAGPU](https://gigagpu.com/tts-latency-benchmarks/)
- [Best Local TTS Models in 2026: 63 Open Voice Models — LocalClaw](https://localclaw.io/blog/local-tts-guide-2026)
- [Coqui XTTS v2 License (CPML): Commercial Use Guide 2026 — PromptQuorum](https://www.promptquorum.com/power-local-llm/local-tts-voice-cloning-piper-coqui-xtts)
- [Piper TTS: Fast Offline Voice Synthesis on a Raspberry Pi — AI Video Sensei](https://aivideosensei.com/guides/piper-tts-offline-voice-guide)
- [Coqui TTS - XTTS v2 — Open-Source Voice Cloning & TTS — LocalAIMaster](https://localaimaster.com/models/coqui-tts)
- [Install Piper TTS v1.4.2: Open Source TTS Guide — QWE AI Academy](https://www.qwe.edu.pl/ai-tools/install-piper-tts-open-source/)
- [Self-Host AI Voice Cloning on GPU Cloud: XTTS-2, F5-TTS, and OpenVoice V2 (2026) — Spheron](https://www.spheron.network/blog/self-host-voice-cloning-gpu-cloud-xtts-f5-tts-openvoice-v2/)
- [why xtts v2 inference time used RAM double(or more 3x) than GPU/VRAM — coqui-ai/TTS Issue #3976](https://github.com/coqui-ai/TTS/issues/3976)
- [XTTS-v2 VRAM Requirements — GIGAGPU](https://gigagpu.com/xtts-v2-vram-requirements/)
- [XTTS v2 — voice samples, specs & how to run it — OpenSpeech](https://www.openspeech.dev/models/xtts-v2)
- [Ultimate Guide - The Best Open Source Models for Voice Cloning in 2026 — SiliconFlow](https://www.siliconflow.com/articles/best-open-source-models-for-voice-cloning)
- [Best Open Source AI Voice Cloning Tools in 2026 — Resemble AI](https://www.resemble.ai/resources/best-open-source-ai-voice-cloning-tools)
- [myshell-ai/OpenVoice — GitHub](https://github.com/myshell-ai/OpenVoice)
- [Best Open-Source Voice Cloning Models 2026 — 6 Tested — RareBuildSoftware](https://rarebuildsoftware.com/blog/best-open-source-voice-cloning-2026)
- [Best Open-Source TTS 2026: Chatterbox 65.3% Beats ElevenLabs — FindSkill.ai](https://findskill.ai/blog/best-open-source-tts-2026/)
- [Ultimate Guide - The Best Open Source Music Generation Models in 2026 — SiliconFlow](https://www.siliconflow.com/articles/en/best-open-source-music-generation-models)
- [Deploy Open-Source AI Music Generation on GPU Cloud: YuE, ACE-Step, MusicGen, and Stable Audio Open Guide (2026) — Spheron](https://www.spheron.network/blog/deploy-open-source-ai-music-generation-gpu-cloud-2026/)
- [Best Open-Source AI Music Generators 2026 — IT-JIM](https://www.it-jim.com/blog/best-open-source-ai-music-generator/)
- [ace-step/ACE-Step-1.5 — GitHub](https://github.com/ace-step/ACE-Step-1.5)
- [ace-step/ACE-Step — GitHub](https://github.com/ace-step/ACE-Step)
- [ACE-Step 1.5 - Music generation for low VRAM - SFT 1.7B AIO — Civitai](https://civitai.com/models/2425543/ace-step-15-music-generation-for-low-vram)
- [Best Open-Source AI Music Models (2026) — Boppy](https://boppy.me/blog/best-open-source-ai-music-models)
- [E-PANNs: Sound Recognition Using Efficient Pre-trained Audio Neural Networks — University of Surrey / arXiv](https://openresearch.surrey.ac.uk/view/pdfCoverPage?instCode=44SUR_INST&filePid=13186519080002346&download=true)
- [StefanoGiacomelli/panns_AT_inference — GitHub](https://github.com/StefanoGiacomelli/panns_AT_inference)
- [CLAP: Learning Audio Concepts From Natural Language Supervision — arXiv](https://arxiv.org/pdf/2206.04769)
- [qiuqiangkong/audioset_tagging_cnn — GitHub](https://github.com/qiuqiangkong/audioset_tagging_cnn)
