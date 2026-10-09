# Quickstart — Validating Provable Model Alias Availability

**Date**: 2026-09-08 | **Plan**: [plan.md](./plan.md)

Runnable checks that prove the feature works end to end. Each states what it proves and what a
failure means. Run from the repository root.

## Prerequisites

- The HelixLLM gateway reachable on `https://127.0.0.1:8443`.
- A shell whose key variables resolve: `. ./scripts/export_gateway_keys.sh`. A long-running session
  may carry a stale inherited value; a fresh shell picks up the correct one.
- The toolkit checked out as a sibling at `~/Projects/claude_toolkit`.

## 1. Every listed alias is usable (SC-001, FR-001)

```bash
. ./scripts/export_gateway_keys.sh
claude-providers list          # verified only — every row here must be launchable
claude-providers list-all      # every state, so nothing is hidden
```

**Expected**: each `list` row is `verified`; the count matches the round-trip successes in step 3.
**A failure means**: an alias is presented as available and is not — the defect this feature exists
to remove. Measured baseline: 8 of 27 usable.

## 2. A verdict states its age, and an old one says so (SC-003, FR-004/005)

```bash
claude-providers list-all | awk 'NR==1 || $4 ~ /^(stale:|[0-9]+[smhd]|\?)$/'
```

**Expected**: a `CHECKED` column carrying `42s`/`9m`/`5h`/`3d`/`?`, and any verdict past the horizon
prefixed `stale:`. **A failure means**: a verdict written before an outage is reading as
present-tense success — the original defect, where an alias showed `verified` while a live probe of
that same endpoint returned 401.

## 3. Both wires complete a real round trip (SC-004)

```bash
. ./scripts/export_gateway_keys.sh
NONCE="CHK-$(od -An -tx1 -N4 /dev/urandom | tr -d ' \n')"
M=$(curl -sk https://127.0.0.1:8443/v1/models -H "Authorization: Bearer $HELIXLLM_GATEWAY_KEY" | jq -r '.data[0].id')
curl -sk -X POST https://127.0.0.1:8443/v1/chat/completions \
  -H "Authorization: Bearer $HELIXLLM_GATEWAY_KEY" -H 'Content-Type: application/json' \
  -d "{\"model\":\"$M\",\"max_tokens\":48,\"temperature\":0,\"messages\":[{\"role\":\"user\",\"content\":\"Reply with exactly this token and nothing else: $NONCE\"}]}" \
  | jq -r '.choices[0].message.content'
```

**Expected**: HTTP 200 and the nonce echoed back exactly. Repeat against `/v1/messages` for the
Anthropic wire. **Why a nonce**: the model cannot have anticipated a random token, so a canned or
stubbed reply cannot pass. **A failure means**: either the wire is wrong for the transport, or the
key does not match `HELIX_AUTH_API_KEYS`.

## 4. The verdict does not change between runs (SC-002, FR-006)

```bash
for i in 1 2 3; do claude-providers list-all | md5sum; done
```

**Expected**: three identical digests. **A failure means**: the verdict depends on something other
than recorded state — most likely a live sample of a non-deterministic backend, which FR-007 forbids
as the default path.

## 5. Guards fail when their subject is broken (SC-007)

```bash
cd ~/Projects/claude_toolkit/scripts/tests
env -u KIMI_API_KEY -u HELIXLLM_GATEWAY_KEY bash test_kimi_wire_and_status_freshness.sh
```

**Expected**: `58 passed, 0 failed`. Then, on a scratch copy only, apply any mutation from the file's
own header ledger (D1, M1c, D2, D3, D4, D5, D5e, W2c, W2d) and confirm the named section FAILS.
**A failure to fail means** the guard is decoration — this session found four such guards, one of
which passed while a real column-swap was live.

## 6. The census can see, and can be wrong (§11.4.273)

Any inventory command whose answer will drive a decision is run with two controls: a value known
present must be found, and a fabricated value must not be. **A failing positive control means the
INSTRUMENT is suspect, not the system** — and the control must lie inside the instrument's declared
scope, or it produces a false broken signal. Both mistakes were made during Phase 0 research and are
recorded in [research.md](./research.md).
