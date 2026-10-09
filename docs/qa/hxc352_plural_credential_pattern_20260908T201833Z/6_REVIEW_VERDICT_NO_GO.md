# Independent review verdict — NO-GO (Fable, xhigh, §11.4.209)

2 BLOCKING, 5 IMPORTANT, 4 MINOR. The fix itself is correct and safe; the suite
does not pin the decisions the fix rests on.

## BLOCKING
- **B1** The load-bearing narrowing is unpinned. Reverting `s=` to the REJECTED
  blanket `s?[[:space:]]*[:=]` leaves the suite at 63/0 while the corpus goes
  141 → 152 flagged (+11). No fixture encodes the decision the whole fix rests on.
- **B2** Two of the three widened carrier strips (`ENV_LOOKUP` lib:272,
  `ACCESSOR_CALL` lib:289) have zero falsifying coverage — un-widening either
  leaves the suite green. Only `PLACEHOLDER` is pinned (case 26-neg-2).

## IMPORTANT
- **I1** The recorded safety argument is FALSE as stated. "A strip can only ever
  REMOVE a false positive" is wrong — a strip removes a detector HIT, and if the
  hit was a true positive it manufactures a false NEGATIVE. The change is safe by
  a DIFFERENT argument: symmetry. `s?` can only consume an `s` between keyword and
  separator; the only extracts carrying one come from the new plural alternative;
  so the widened carrier strips `kws=V` iff the old one strips `kw=V` — the plural
  masking surface EQUALS the pre-existing singular one. Verified by probe; no
  counter-example constructible.
- **I2** Two keyword-anchored carriers were NOT widened, creating asymmetry:
  `passwords=passwords` HITs while `password=password` is clean (lib:225, lib:321).
- **I3** Residual understated: space-padded `=` and elvis forms also escape
  (`PASSWORDS = hunter2…`, `passwords = foo ?: "hunter2…"`). A measured alternative
  `s[[:space:]]*=[[:space:]]*` catches them at 2 corpus FPs vs the blanket's 11.
- **I4** The consumer claim is WRONG for this repo. The parent pre-commit hook
  (`scripts/git_hooks/pre-commit:592-635`) is a FILENAME check only, and
  `scripts/secret_scan.sh` has no generic keyword pattern — so neither `API_KEY=`
  nor `API_KEYS=` is caught at the parent's real commit seam, before or after.
  Real consumers: `constitution/scripts/gates/lib/execution_record.sh:87-93` and
  a second checkout `submodules/claude-toolkit/constitution/` needing a lockstep
  bump. Also UNCONFIRMED: the leaking filter is not in the tracked corpus, so
  there is no evidence THIS library was it.
- **I5** Pre-existing same-class gap: `SECRET_KEY=`, `DJANGO_SECRET_KEY=`,
  `SECRET_KEYS=` are clean on old AND new — `secret` must be followed directly by
  a separator. Separate item.

## Author-blind mutations: 4 of 5 SURVIVED
| Mutation | Result |
|---|---|
| un-widen ENV_LOOKUP carrier | SURVIVES |
| un-widen ACCESSOR_CALL carrier | SURVIVES |
| revert to rejected blanket `s?…[:=]` | SURVIVES |
| `s[[:space:]]*=[[:space:]]*` | SURVIVES |
| un-widen PLACEHOLDER carrier | caught (26-neg-2) |

## Confirmed correct
Regex/quoting valid (paren balance 7/7, ERE exit 1 not 2). Case-insensitive
admissions enumerated and consistent with singular behaviour. Corpus delta
independently re-measured: 140 → 141, one flip, the known prose residual.
