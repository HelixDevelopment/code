# HXC-352 — remediation of the Fable-xhigh NO-GO review (2026-09-08)

Evidence for the remediation of `6_REVIEW_VERDICT_NO_GO.md` in the parent
directory. Scope: `constitution/scripts/hooks/credential_scan_lib.sh` and
`constitution/scripts/hooks/test_credential_scan_lib.sh`.

## What is in here

| File | What it proves |
|---|---|
| `7a_suite_after_remediation.log` | Full golden suite on the shipped library: **73 passed, 0 failed**, exit 0. |
| `7b_falsification_matrix.log` | §1.1 / §11.4.224(C): each of 10 named mutations applied to a **scratch copy** of the library (§11.4.84 — never the live tree), with the exact fixtures that FAIL under it. Every new fixture is falsified by its own target mutation; no mutation collapses the suite. |
| `7c_corpus_flagged_<variant>.txt` | Per-variant flagged-file list over the 644-file tracked corpus (paths repo-relative). |
| `7d_corpus_delta_matrix.txt` | The measured trade behind every design choice. |

## The measured trade (`7d`)

| variant | what it is | flagged | verdict |
|---|---|---|---|
| `old` | pre-HXC-352 library, singular keywords only | 140 | leaks the plural class |
| `base` | the shipped narrow `s=` fix the review returned NO-GO on | 141 | correct but under-tight |
| `blanket` | `s?` on the generic `[[:space:]]*[:=]` alternative | 152 | **REFUTED** — +11 false-positive refusals |
| `spaceeq` | assignment-only, space-padding permitted | 143 | +2, both `apiKeys = &APIKeys{}` |
| `final` | `spaceeq` + carrier-strip #21 widened for `&Type{}` | **141** | **adopted** — 0 new FPs, 0 lost catches |

`final` closes three false-negative classes (plural assignment, space-padded
plural, plural elvis-fallback) and three false-positive asymmetries
(`passwords=passwords`, `apiKeys=DefaultAPIKey`, `apiKeys = &APIKeys{}`) at zero
measured corpus cost.

## Instrument discipline (§11.4.273)

Every measurement here was control-needled before it was believed:

* the scan harness was proven on a known-secret file (HIT) and a known-clean file (clean);
* it independently reproduces the reviewer's own count (141) for the shipped library;
* the mutation applier asserts an exact one-occurrence anchor and refuses a
  zero-match sed — it caught two real instrument errors during this remediation
  (a hand-typed `M10` anchor that matched nothing, and a `comm` run over
  non-`LC_ALL=C`-sorted input whose output was discarded and re-measured);
* `comm` was itself needled (`only-in-2` / `only-in-1`) under `LC_ALL=C`;
* one fixture (`26-j`, plural elvis) was found NON-DISCRIMINATING by the matrix
  and rewritten until its target mutation actually failed it.

## Honest boundary (§11.4.6)

The corpus is the 644 candidate text files of this checkout, not "all code".
The change is proven not to regress that corpus and to close the enumerated
classes; it does not prove the detector complete. Standing residuals — colon
config style, the pre-existing `SECRET_KEY=` gap, and one spec-file false
positive — are recorded in the library and suite comments, not hidden.
