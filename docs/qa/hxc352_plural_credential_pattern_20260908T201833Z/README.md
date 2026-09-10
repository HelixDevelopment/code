# HXC-352 — plural credential-name blind spot: RED → GREEN → mutation → measured FP delta

| Field | Value |
|---|---|
| Item | HXC-352 (Bug) + HXC-353 (Task, residuals) |
| Captured | 2026-09-08T20:20:03Z |
| Subject | `constitution/scripts/hooks/credential_scan_lib.sh` detector-1 |

## What was wrong
An agent inspecting a live server's environment printed two credential VALUES in
full: its redaction filter recognised `KEY=` but not `KEYS=`. Measured on the
affected host: exactly two variables end in the plural form, nine end in the
singular form and were correctly redacted. The singular/plural split was the
whole defect, and it applied to all seven credential keywords, not just API keys.

## Evidence

| File | Run | Result |
|---|---|---|
| `1_RED_prefix_library.log` | pre-fix library recovered from git HEAD, current test source | **56 passed, 7 failed** — the 7 plural fixtures reproduce the defect |
| `2_GREEN_shipped_library.log` | shipped library, same test source | **63 passed, 0 failed** |
| `3_MUTATION_alternative_removed.log` | §1.1 paired mutation: plural alternative stripped on a scratch copy | **56 passed, 7 failed** — the fixtures are load-bearing |
| `4a/4b/4c_flagged_*.txt` | scanner verdicts over every tracked file containing the plural shape | 10 before → **22** with the refuted blanket fix → **11** with the shipped narrow fix |

## Why the obvious fix was refuted (§11.4.201 both-directions)
Appending `s?` to the keyword group closed the 7 false negatives and opened
**12 new false positives** — `APIKeys: map[string]string{`, `apiKeys: apiKeys`,
`Secrets: HashiCorp` — because a plural keyword before a colon is how code names
a *collection* of keys, not a secret. Trading 7 false negatives for 12
false-positive refusals is the FAIL-bluff §11.4.201(1) forbids, not a fix.

What shipped is one additional detector alternative matching only the
**env-assignment shape** the real leak had (plural keyword immediately followed
by `=`, no surrounding whitespace). 11 of the 12 false positives disappear; no
true positive is lost. The carrier-strips keep the plural `s?` — a strip can
only ever remove a false positive.

## Honest residuals (§11.4.6) — tracked as HXC-353
1. A plural credential in colon/config style (`api_keys: <secret>`) is still not
   caught. Catching it is exactly what produced the 12 false positives.
2. One residual false positive: a spec file whose prose quotes `APIKeys="real-key"`
   as a fixture. It is genuinely credential-shaped; adding that literal to the
   placeholder vocabulary would be over-fitting.

## Operator decision recorded
The transcript file holding the two exposed values is left in place as an
**accepted local risk** (operator decision, 2026-09-08). This item covers the
pattern repair only.

## Repo-wide control (completed 2026-09-08T20:35:45Z)
Every tracked file scanned with the pre-fix library and again with the shipped one:

| | flagged |
|---|---|
| before | 962 |
| after | **963** |
| newly flagged | `specs/002-adaptive-local-model-serving/progress.yml` (the one known residual) |
| newly missed | **none** |

Artifacts: `5a_repowide_flagged_before.txt`, `5b_repowide_flagged_after.txt`.
This confirms the targeted 22-file measurement across the whole corpus: the change
adds exactly one flag and loses no true positive.
