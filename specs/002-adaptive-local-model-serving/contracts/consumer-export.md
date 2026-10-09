# Contract: Consumer Export

What each consuming tool receives. The three consumers differ in what they accept, and those
differences are respected as they stand (FR-014a).

## Per consumer

| Consumer | Integration status | Identifier rules |
|---|---|---|
| **Claude Toolkit** | Extend existing detector — one entry per model per host (D8) | Two validators: alias name `^[a-zA-Z][a-zA-Z0-9_-]*$`; provider id `[A-Za-z0-9._-]` only. **The provider-id rule is a shell-injection guard** — the id is interpolated into an alias body and re-parsed on invocation. |
| **HelixCode** | Existing path is a hand-written special case; the declared provider config is dead | See plan Complexity Tracking before touching the dead config (§11.4.122) |
| **OpenCode** | **Net-new** — no provider-config integration exists today, only Skills/MCP/instructions sync | To be designed |

## Invariants

1. The exported identifier satisfies the target consumer's existing validation. **No consumer's
   validation is relaxed, widened, or bypassed to fit a richer name** (FR-014a).
2. The human-readable identity travels in a value field, where it is data rather than something
   interpolated and re-evaluated (D7).
3. Export is idempotent: re-running produces the same configuration, and updates rather than
   duplicates an existing entry.
4. An unreachable or unhealthy instance is not exported as available.
5. The discovery secret is never written into an exported configuration (§11.4.10).

## Why the injection guard matters here

The naming scheme was originally specified as the identifier itself. That string is rejected by both
toolkit validators, so implementing it literally meant either widening an injection guard or failing
to build — the defect FR-014a now forbids by name. Any future proposal to "just allow `/` in the id"
is re-opening that hole.
