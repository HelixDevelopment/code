# HXC-159 N1/N2 fix (`97e8c852`) — independent review evidence

| Field | Value |
|---|---|
| Reviewed commit | `97e8c852` (parent `19033dc3` used as the RED baseline) |
| Reviewer | independent agent, zero authorship stake (§11.4.240) |
| Date | 2026-09-23 |
| Verdict | **NOT-GO** — N1 closed, N2 closed, builtin-shadow ordering closed at startup; three regressions introduced (NEW-1, NEW-4, NEW-5) plus one precedence divergence (NEW-2) |

## Method

Both trees were exported with `git archive` (parent `97e8c852^` and `97e8c852`)
into a scratch directory outside the checkout, with `submodules/` and
`constitution/` symlinked back to the real checkout (pkg/skills is untouched by
the commit: pointer diff empty, submodule working tree clean). The same probe
test source was injected into BOTH trees with `go test -overlay`, so one piece
of code measures both. Two real `helixcode` CLI binaries were built, one per
tree, and driven against fixtures the reviewer wrote (not the implementer's).
The real checkout was never modified except for adding this directory.

Probe sources are in `probes/`. Captures are in `captures/`.

## Results

| Item | Parent | Fixed | Capture |
|---|---|---|---|
| N1: 3 packaged + 31 flat `.md` (control needle: plain `commands.SkillLoader` recognises all 31) | 31/31 flat registered, flat trigger dispatched | 0 registered, 31 WARN lines | `02_*`, `07_real_*_n1_*` |
| N2: `mmm-top` vs `lib/x/aaa-deep`, same description | both absent | `aaa-deep` registered + invocable, `mmm-top` absent | `01_*`, `08_*` |
| Builtin override in dir1 survives unrelated later dir | reverted to builtin | override survives | `01_*` (RV-F) |
| Builtins when the external tier is the only dir | 8 | 8 | `01_*` (RV-E) |
| Real `.claude/skills`, CLI `skills list` | 7 builtin + 2 external | same set; SOURCE prefix now `external:` | `07_real_*_corpus_*` |
| Suites: agent, commands, clientcore, skillconformance, cmd/cli; pkg/skills; vet | — | all `ok`; vet clean | `06_*` |
| Paired mutations | — | m1/m2/m3 caught; **m4 (drop same-Dir fail-closed) SURVIVES** | `05_*` |

## New findings

- **NEW-1 (regression, user-visible).** Triggers are now taken from pkg/skills'
  stdlib YAML-subset parser, which does not unquote list items or process
  escapes. `"(?i)…\\b…"` becomes a regex with literal quote characters and a
  literal backslash, so it compiles but never matches. On the real corpus 5 of
  7 admitted skills are affected. In `LoadSkillsAndDispatcher` (clientcore and
  terminal_ui) the external copies now override the builtins, so
  "how do i add a new directive" and "what does the BACKGROUND prefix do" go
  from matched=true (parent) to matched=false (fixed). Real binary: see
  `08_*`, `skills show zapper`.
- **NEW-2 (precedence divergence).** `LoadSkillsAndDispatcher` now resolves
  external over builtin. The CLI still resolves builtin over external, because
  its legacy loader, which loads builtins, runs after the external tier. The
  same skill name therefore resolves to different sources in different
  front-ends.
- **NEW-4 (regression).** The CLI's watched project/user loader and
  `skills reload` loader now carry `SkipBuiltins(true)`. If a project override
  of a builtin is deleted and the loader reloads, the builtin is removed from
  the running registry and not restored. The parent restored it (`03_*`).
- **NEW-5 (safety-flag fidelity).** `requires_isolation: True`, `TRUE`, or
  `true # comment` is dropped to `false`. yaml.v3 honoured these values in the
  parent (`04_*`). No production caller routes on the flag today, so this is a
  latent fail-open.
- **Minor.** A same-leaf-Dir collision drops the whole external tier, including
  unrelated skills (`08_*`, dup fixture). The path is untested (m4 survives).
  A root-level `SKILL.md` in the tier now registers under the tier directory's
  basename. In the CLI path, builtins are loaded only as a side effect of the
  legacy loader. That path is not reachable today, because the CLI refuses to
  run without `$HOME`. The cmd/cli hand-rolled call sites have no test.
- **Pre-existing, not introduced here.** The external tier is identified by
  string equality, so `dir + "/"` bypasses the gate entirely (`02_*`).
  `cmd/skillconformance` loads the external tier ungated, so its capability
  manifest over-reports. It has no activation consequence.
