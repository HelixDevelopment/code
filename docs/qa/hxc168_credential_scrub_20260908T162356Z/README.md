# HXC-168 — credential scrub: captured evidence

| Field | Value |
|---|---|
| Revision | 1 |
| Created | 2026-09-08 |
| Last modified | 2026-09-08 |
| Status | active |

## Table of contents

- [What this run proves](#what-this-run-proves)
- [Files](#files)
- [What it does NOT prove](#what-it-does-not-prove)

## What this run proves

The §11.4.115 polarity pair for the HXC-168 exposure class, captured from the
SAME test source in both directions, plus the §1.1 mutation that shows the
guard can still fail.

- **RED on the pre-fix artifact** — the defect genuinely reproduces on the
  broken tree, not on a synthetic fixture. Run from a `--shared` clone checked
  out at `2182d644~1` (`1f67b44f`), i.e. before the scrub landed: **41
  findings, RED PASS**. A RED test that cannot fail on the known-broken
  artifact is a blind test, so this is the leg that earns the GREEN.
- **GREEN on the fixed artifact** — same source, `RED_MODE=0`: **GREEN PASS
  with 1 OPERATOR-BLOCKED**. Not "all clear": the running container still
  publishes `*:55432` because its SOURCE already binds loopback and podman
  cannot rebind a published port in place, so it closes only on an
  operator-gated recreate.
- **§1.1 mutation** — revert the source publish to the wildcard and the same
  runtime turns from OPERATOR-BLOCKED into **2 findings, GREEN FAIL exit 1**.
  That is what proves OPERATOR-BLOCKED is a discriminator and not a carve-out.
- **Gate self-test** — 8 paired mutations, exit 0. Planted literals are
  REDACTED here (`<REDACTED-L1>` / `<REDACTED-L2>`): the self-test deliberately
  plants them to prove detection, and a transcript is not a place to publish a
  credential (§11.4.10). The verdicts are untouched.
- **Sweep** — `G35` and `G36` each `Gates run: 1`, both PASS.

## Files

| File | What it is |
|---|---|
| `01_RED_on_prefix_artifact.log` | Defect reproduced on the pre-scrub tree (41 findings) |
| `02_GREEN_on_fixed_artifact.log` | Same test, fixed tree, 1 operator-blocked |
| `03_MUTATION_wildcard_source.log` | Paired mutation: wildcard source ⇒ 2 findings |
| `04_gate_selftest.log` | Gate's own 8 paired mutations (literals redacted) |
| `05_sweep_G35_G36.log` | Both registered gates via the sweep |

## What it does NOT prove

Stated plainly, because a green transcript invites the opposite reading:

- **The credential is still valid.** Nothing here rotates it. It is in git
  history on four mirrors and remains usable until it is rotated at the
  database — an operator action no gate can perform.
- **`:55432` is still LAN-reachable.** The loopback bind is landed in source
  only. The recreate that would apply it needs an app-layer cycle, because the
  credential is injected by `internal/infraboot` during the application's boot
  rather than by a standalone launcher.
- **This is not a closure.** HXC-168 remains Operator-blocked. Per §11.4.83 the
  transcripts are captured NOW so the RED leg is not reconstructed from memory
  later; the item closes when the operator's two actions are done.
