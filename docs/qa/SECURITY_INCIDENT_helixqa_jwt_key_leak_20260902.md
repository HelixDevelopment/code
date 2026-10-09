# Security incident — RSA JWT-signing keypair in helix_qa history (CONST-042)

**Status:** COMPROMISED-BUT-INERT · **ROTATION RECOMMENDED (operator action)** ·
recurrence gap CLOSED (helix_qa `8a57e65`) · found by a repo-wide credential sweep,
2026-09-02.

## What happened

`keys/jwt_private.pem` (28-line PKCS#8 RSA) and `keys/jwt_public.pem` were committed to
`submodules/helix_qa` by commit `83bfa02` — an **"Auto-commit"** batch of 358 files that
scaffolded the synthetic "Helix Seller" demo application. No generator script existed;
the key was produced by hand from the `openssl genrsa` recipe documented in
`.env.example`, and the output was committed directly.

## Severity: compromised-but-inert (investigated, not assumed)

**Not test-fixture-only.** `internal/service/jwt_test.go` generates ephemeral keys per
run and never touches the committed pair — but `internal/config/config.go` defaults
`JWTPrivateKeyPath` to the literal `keys/jwt_private.pem`, `cmd/server/main.go` loads it
at real startup, and `tests/challenges/scripts/hxs_setup.sh` started that server and
drove real `/api/v1/auth/register` and `/auth/login` calls. **It signed real RS256
tokens.**

**Confirmed public.** `git ls-remote --tags origin` returns `refs/tags/hxs-v1`, whose
lineage carries the key blob. It is on `HelixDevelopment/HelixQA` and must be treated as
distributed.

**Not live.** The Helix Seller app does not exist on `main` at all. Its 27-commit
`hxs-v1` lineage is **disjoint** from `main`'s 928-commit history — `git merge-base`
finds no common ancestor — was never merged, has no CI (this project runs none per
§11.4.156), and bound only to `127.0.0.1`. No known service accepts tokens signed by it
today. Its absence from `HEAD` is therefore **structural** (an unmerged branch), not a
deliberate secret scrub.

## Remediation

1. **Recurrence gap CLOSED (helix_qa `8a57e65`).** `HEAD`'s `.gitignore` carried only
   `.env*` patterns — no `*.pem`, `*.key` or `keys/`. That is why the key could land at
   all, and nothing prevented a repeat. Those patterns are now present and verified
   matching. 0 key files are tracked at `HEAD`, so this is prevention, not cleanup.
2. **ROTATION RECOMMENDED — operator action.** History rewriting is forbidden
   (§11.4.113) and the key is already distributed, so rotation is the only remedy. If
   any machine or forgotten checkout can still run `cmd/server/main.go` from `hxs-v1`,
   point its default key path at a freshly generated, gitignored key — or retire the
   fixture.
3. No production or CI exposure. That is **not applicable** rather than
   NOT ESTABLISHED: the absence is positively evidenced by there being no CI config and
   by the localhost-only nature of the HXS scripts. No external notification indicated.

## Pattern worth naming

This is the **third** security problem found today whose introducing commit is titled
**"Auto-commit"**:

| Commit | Damage |
|---|---|
| `36baf676` | swept an antivirus false-positive deletion, destroying 485 lines of security tests |
| `ce075aad9` | wrote a compose service pointing at a Dockerfile that never existed (dead 7 months) |
| `83bfa02`  | committed this RSA signing key |

A blind bulk-commit mechanism does not create these defects, but it is the vector that
lands them in published history. The deletion-guard hooks were installed on 2026-09-02;
a secret scan on `docs/**` and on staged key material is still not wired.

## Follow-ups

- [ ] Operator: rotate or retire the `hxs-v1` JWT keypair
- [ ] Wire a secret scan into the pre-commit hook (covers this and the Xiaomi/GEMINI class)
- [ ] Review what else `Auto-commit` has landed unreviewed
