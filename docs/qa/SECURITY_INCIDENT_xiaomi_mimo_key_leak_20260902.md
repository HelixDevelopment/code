# Security incident — leaked Xiaomi MiMo API key (CONST-042)

**Status:** Redacted at HEAD (this commit) · **ROTATION PENDING (operator action required)** · found by a repo-wide credential sweep, 2026-09-02.

## What happened

A real Xiaomi MiMo provider API key (`ApiKey_Xiaomi_MiMo=sk-…`, value **redacted, not
reproduced here** per §11.4.10) was committed in plaintext to a tracked design
document:

    docs/superpowers/specs/2026-06-19-xiaomi-mimo-integration-design.md:151

The document itself states the value came "From `~/api_keys.sh` (already configured)" —
i.e. a real, working local credential was copied verbatim into a committed spec.

Unlike the 2026-07-11 GEMINI_API_KEY incident (redacted by `41372967`), this key was
**never redacted** and remained live in HEAD, published to all four mirrors, until this
commit.

## Remediation

1. **Redacted at HEAD (this commit):** the value replaced with
   `<REDACTED-XIAOMI-MIMO-KEY-CONST-042-ROTATION-PENDING>`. Verified `git grep` → 0
   remaining occurrences anywhere in the working tree.
2. **ROTATION PENDING — operator action.** Redaction does **not** withdraw the key. The
   plaintext remains permanently readable in git history on every mirror, and §11.4.113
   forbids history rewriting. Revoking and reissuing at the provider is the only remedy.
   The operator has accepted this and scheduled rotation (decision recorded 2026-09-02).

## How it was found, and the honest scope

Found by a repo-wide credential sweep run 2026-09-02, not by any gate. That sweep
identified **12 LIKELY-LIVE credentials beyond the 3 already tracked** — the largest
cluster being 8-9 genuine provider keys in `submodules/llms_verifier` history
(committed 2025-12-23, stripped from the working tree later but still readable in
history). All are pending operator rotation.

Coverage was **7 of 131 submodule histories**. A widened sweep across all owned
submodules is in progress. Until it completes, the count above is a **floor, not a
total**.

## Why the existing controls did not catch it

Same root cause as the GEMINI incident, still unclosed at the time: committed
`docs/**` content is version-controlled by design and was **not secret-scanned before
commit**. The pre-commit hooks that would have caught it existed at
`scripts/git_hooks/` but were **not installed** — `core.hooksPath` was empty and
`.git/hooks/` held no non-sample hooks. They were installed earlier on 2026-09-02.

Note the asymmetry this exposes: the GEMINI post-mortem (2026-07-11) already identified
"no pre-commit / pre-push / release-gate secret scan existed to block it" as the root
cause, and the scan still did not exist ~7 weeks later when this key was found. A
post-mortem that names a control gap but does not close it does not prevent recurrence.

## Follow-ups

- [ ] Operator: revoke + reissue the Xiaomi MiMo key
- [ ] Operator: rotate the remaining 11 LIKELY-LIVE credentials
- [ ] Complete the widened sweep across all owned submodules
- [ ] Wire a secret scan into the now-installed pre-commit hook so `docs/**` is covered
