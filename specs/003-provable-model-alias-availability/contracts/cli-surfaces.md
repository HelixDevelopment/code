# Contracts — CLI Surfaces

**Date**: 2026-09-08 | **Plan**: [plan.md](./plan.md)

These are the surfaces this feature promises to operators and to other scripts. A change to any
column order, exit code or state vocabulary below is a breaking change and needs its own review.

## `claude-providers list` / `list-all`

Seven columns, in this order. **The order is the contract** — `kimi-providers` parses it
positionally, and this session shipped a defect by inserting a column without updating that parser.

```
ALIAS  PROVIDER  STATUS  CHECKED  LAYER  STRONG_MODEL
```

- `list` shows `verified` only. `list-all` shows every state.
- `STATUS` carries the `stale:` prefix when the verdict is past the horizon.
- `CHECKED` is the verdict's age, or `?` when unknown, or `-` for a row that has no verdict of its
  own (a `no-twin` row states missing wiring, not a judgement).
- **Neither command performs any network I/O.** Re-verification is `verify`, and only `verify`.

## `kimi-providers list`

The same table with an `AGENT` column prepended (`claude` | `kimi`), one row per agent per provider.

```
AGENT  ALIAS  PROVIDER  STATUS  CHECKED  LAYER  STRONG_MODEL
```

A twin lacking its `config.toml` renders `STATUS=no-twin`, `CHECKED=-`, `LAYER=run-sync`.

## Exit codes (FR-009)

Three states, never conflated:

| Code | Meaning |
|---|---|
| `0` | The operation ran and succeeded — including "ran and found nothing". |
| `1` | The operation ran and FAILED, or found a violation it exists to detect. |
| `3` | The engine could not be reached — the operation did NOT run. |

"Found nothing" and "could not look" are different answers and MUST be distinguishable by exit code
alone, without parsing output.

## Degradation without the governance corpus (FR-010)

Any entry point invoked where the corpus is absent either completes normally or refuses LOUDLY,
naming: what is unavailable, what still works, and how to supply it. A silent no-op is forbidden —
the operator would believe a protection is active when it is not.

## Credential handling (FR-019)

No credential value appears in any listing, verdict, log, evidence file, proof artifact or
diagnostic. Comparisons are by hash. The single secret-bearing artifact is the per-twin
`config.toml`, written `0600` inside a `0700` directory via umask, `mktemp`, and atomic rename —
never a post-hoc `chmod`, which would leave a world-readable instant.
