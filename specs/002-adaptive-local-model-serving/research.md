# Phase 0 Research — Adaptive Local Model Serving

**Date**: 2026-09-02 | **Plan**: [plan.md](./plan.md)

Consolidated decisions. Raw evidence with per-claim source URLs lives in `research/01..05`; this file
records what was **decided** and why. Every claim below was verified against the tree or the
consumer source before being adopted — the verification is noted where it changed the outcome.

---

## D1 — Streaming-runtime eligibility is a named roster, not an architecture test

**Decision**: A model qualifies for the disk-streaming path only by membership in the streaming
runtime's declared supported set. Architecture is not the predicate.

**Rationale**: The runtime supports a closed, named list of model families and states so explicitly
("Not supported: arbitrary GGUF models, dense-only architectures"). Several widely-known
mixture-of-experts models — DeepSeek-V3.2, Llama 4 Scout/Maverick, gpt-oss-120b, Qwen3-30B-A3B — are
architecturally MoE with **no** support path. An `architecture == MoE` test would offer options that
cannot run: a selection bug presenting as a runtime failure.

**Alternatives considered**: (a) architecture predicate — rejected, provably over-offers;
(b) attempt-and-fall-back — rejected, converts a cheap catalogue lookup into a user-visible failure.

**Consequence**: roster membership is *catalogue data*, so a runtime release that adds a family is a
data change, not a code change.

## D2 — Storage headroom is an axis independent of memory headroom

**Decision**: Fit is checked on memory **and** free storage, separately.

**Rationale**: The streaming path's whole premise is weights that exceed memory living on disk — one
supported family carries roughly a 372 GB on-disk footprint that its memory figure does not imply. A
single "headroom" number silently conflates them and offers models that cannot be stored.

## D3 — Selection is measured; configuration may never name the model

**Decision**: The model to run is derived from measurement. Configuration states where files are,
how to reach an instance, and what the user prefers or forbids — never which model runs. No fixed
default on measurement failure.

**Rationale**: The existing implementation selects via `VISIONGEN_MODEL_GGUF` / `VISIONGEN_MODEL_DIR`
— exactly the static behaviour this feature exists to replace. Because the spec independently
requires environment-variable configuration, the two requirements read as compatible and an
implementer could keep static picking while claiming compliance. FR-056 draws the boundary.

**Alternatives considered**: config-as-default-with-measured-override — rejected: the fallback is
the failure mode, since a stale default is exactly what gets shipped when measurement is skipped.

**Consequence**: a deliberate user pin is a *constraint on* selection, not a bypass — it is refused,
naming the insufficient resource, if the host cannot run it.

## D4 — Usage terms are a first-class selection constraint

**Decision**: Every catalogue entry carries its usage terms. A model whose terms exclude the declared
usage is withheld, and if displayed at all is shown unavailable with the restricting term named.

**Rationale**: Several of the strongest candidates in speech, audio and image carry non-commercial or
revenue-capped terms (CPML, CC-BY-NC, revenue-capped variants). Capability and fit alone are
therefore insufficient grounds to offer a model. Verified: the pre-clarify spec mentioned licensing
zero times — a complete gap, not an under-specification.

**Alternatives considered**: surface terms as advisory metadata — rejected: it puts a legal
determination on the user at the moment they are least equipped to make it.

## D5 — The non-empty-options guarantee is per family

**Decision**: Each capability family that can be served acceptably on a given host offers a non-empty
set. A family that cannot states why and names what the host lacks.

**Rationale**: Three independent research passes hit the same wall from different directions: audio
generation has no processor-viable option, and image generation has none at interactive speed on
either no-GPU tier. The original universal guarantee was therefore unsatisfiable, and the only ways
to "satisfy" it were to offer something unusably slow or return an unexplained empty list — both
worse than an honest refusal. Treated as one defect in the guarantee rather than three exceptions.

## D6 — Runtime choice: in-memory first, streaming only when it is the only path

**Decision**: Try the general in-memory runtime first; fall through to streaming only when the model
does not fit **and** is roster-supported **and** meets that runtime's own minimums. Otherwise state
plainly that nothing can serve it.

**Rationale**: The general runtime is architecture-agnostic but hard-capped by quantised weights plus
KV cache fitting in combined accelerator + system memory; it has no weight-streaming mode. Streaming
trades throughput for feasibility by orders of magnitude, so it is a fallback, never a preference.

**Consequence**: "unsupported configuration" and "insufficient resources" are **different** failure
reasons with different remedies and must not be reported as one generic unavailability (FR-055).

## D7 — Naming is a value; consumer identifiers are derived

**Decision**: `helixllm/<host>/<model>[:<variant>]` is a human-readable identity carried as a value.
Consumers needing an identifier get a separately derived, charset-safe one, with the mapping recorded.

**Rationale**: Verified directly against the consumer: the mandated string is rejected by **both** its
validators — the alias rule (`^[a-zA-Z][a-zA-Z0-9_-]*$`) and the provider-id rule
(`[A-Za-z0-9._-]` only). The second is a security control: the id "is interpolated into the alias
body and re-parsed when the alias is invoked", and the charset exists so "a hostile catalog/--id
value can't inject shell commands". Implementing the original wording literally meant either
widening an injection guard or failing to build.

**Alternatives considered**: widen the validator — **rejected outright**; trades a naming convenience
for a shell-injection defect (FR-014a now forbids it explicitly).

**Consequence**: the correct emission point is `Brain.Models()`, which today publishes a raw model id
with no host prefix. Fixing it there reaches every consumer at once.

## D8 — Consumer integration is extension at two seams, net-new at one

**Decision**: Extend the toolkit's existing HelixAgent/HelixLLM detector from one-model-per-host to
one-entry-per-model-per-host. Correct naming at `Brain.Models()`. Design OpenCode provider export
from scratch.

**Rationale**: The toolkit detector already works and is reusable — replacing it would discard
working code (§11.4.74). OpenCode, by contrast, has *no* provider-config integration today (only
Skills/MCP/instructions sync), so that piece is genuinely net-new rather than an extension.

## Verified corrections to prior assumptions

Recorded because each changed the plan:

- **Speech-to-text is not greenfield.** `container/whisper_stt_server.py` and
  `Containerfile.whisper` exist, unwired from the catalogue. Text-to-speech and audio generation
  genuinely are zero. Effort planning must not treat the three as equivalent.
- **Vision and image generation are further along than assumed.** A measured on-GPU vision boot path
  exists with captured evidence; an image-generation service exists whose generation has never been
  run on hardware. What is missing for both is catalogue diversity, measured selection, and
  integrity verification — not the serving primitive.
- **A declared provider config is dead.** `config.yaml:155-166` declares `helix-llm`/`helix-debate`
  provider types while `LLMConfig` carries no `providers` field at all. Recorded, not touched.

## Open — requires operator decision

- **Video generation.** `cmd/videogen-boot/` and `services/videogen/` exist; the spec never mentions
  video. In scope, or tracked follow-up? Flagged rather than assumed in either direction.
