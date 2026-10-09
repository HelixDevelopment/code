# Contract: Model Listing

**Emission point**: `Brain.Models()` — the single place model identity reaches every consumer.
Today it publishes a raw model id with no host prefix; this contract corrects it there so all
consumers benefit at once (D7).

## Shape

Each listed model carries:

- **`id`** — the derived, charset-safe identifier appropriate to the requesting consumer.
- **`model_identity`** — the human-readable `helixllm/<host>/<model>[:<variant>]` value.
- **`owned_by`** — provenance.
- **availability** — serving, or unavailable with its withheld reason.

## Invariants

1. `model_identity` alone identifies the option as HelixLLM-served, names its serving host, and
   distinguishes it from remote provider models, with nothing else consulted (FR-014).
2. `id` satisfies the requesting consumer's **existing** validation. It is never obtained by
   relaxing that validation (FR-014a).
3. The mapping `id ↔ model_identity` is recorded; the two cannot drift.
4. The scheme is stable across releases — these names live in users' tool configurations (FR-015).
5. A model that is not actually being served is never listed as available.

## Compatibility

Existing clients read `id`. Adding `model_identity` alongside is additive; **changing what `id`
contains is not**, and needs the migration path FR-015 implies.
