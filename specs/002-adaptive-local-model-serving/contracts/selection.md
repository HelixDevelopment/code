# Contract: Selection

The measurement → offer surface. Pure function of (Host Capability Profile, catalogue,
declared usage) → option set.

## Request

- target host (defaults to this host)
- capability family, or all families
- declared usage — drives the usage-terms filter (FR-054)
- any deliberate pins

## Response

Per family, either offered options or a refusal. **A family is never silently empty** (D5).

Each offered option carries its resource cost and expected capability in comparable terms (FR-005).
Each withheld option carries exactly one reason:

| Reason | Meaning | Remedy it implies |
|---|---|---|
| `insufficient_resources` | Host lacks memory / storage / accelerator | Change the host, or pick smaller |
| `unsupported_configuration` | No option supports this host's configuration at all | Different approach entirely |
| `excluded_by_usage_terms` | Otherwise suitable; terms forbid the declared usage | Different model, or different declared usage |

These are **not** interchangeable and must not collapse into one generic unavailability (FR-055).

## Invariants

1. Every offered option fits the **measured** host on memory **and** storage, checked separately (D2).
2. No configuration value names the model; no fixed default on measurement failure (FR-056).
3. If the host cannot be measured, the response is a refusal stating that — never a guess.
4. A pin that the host cannot run is refused, naming the insufficient resource (FR-056).
5. Streaming eligibility is roster membership, not architecture (D1).
6. Refusal text is composed from measurement + catalogue data, not fixed strings (CONST-046).

## Testability

Because this is a pure function, fixture hosts drive the whole surface — including every refusal
path — without hardware. That is the point of keeping the join one-directional.
