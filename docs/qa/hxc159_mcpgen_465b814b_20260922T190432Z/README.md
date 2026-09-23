# HXC-159 T-P5.04 — generate .mcp.json entries from the manifest — QA evidence (§11.4.83)

**Run ID:** `hxc159_mcpgen_465b814b_20260922T190432Z`
**Fix commit:** `465b814bd75996f7a34e8f6c12ba22dc0d909d91`
**Captured (evidence originally produced):** 2026-09-22 19:04
**This docs/qa record authored:** 2026-09-23 (T-P9.03, HXC-159 F-20 remediation)

## What this commit did

`.mcp.json` previously hardcoded constitution skill paths (e.g. the
media-validator entry's args), re-coupling the consumer to one specific
project layout. Adds a generator instead: `internal/mcpgen`
(`ParseManifest` reads the T-P1.04.2 TSV schema; `Generate` derives one MCP
server entry per manifest row whose wrapper script exists on disk;
`WriteMerged` merges generated entries into a target `.mcp.json`, replacing
same-key entries while preserving hand-authored non-manifest entries,
deterministic and byte-identical on re-run; `Drift` reports manifest-derived
entries that no longer match what `Generate` would currently produce;
`Probe` genuinely execs a generated entry's command+args with a bounded
timeout). New `cmd/mcpgen` CLI wrapper and
`scripts/gate_mcp_entries_generated.sh` (`CM-MCP-ENTRIES-GENERATED`,
read-only).

## How this evidence was produced

Real command captures, RED-first (§11.4.115):

1. `red_compile_fail.txt` — RED: `go vet ./internal/mcpgen/...` fails with
   "no non-test Go files" before the package exists.
2. `green_pass.txt` — GREEN: 7 unit tests including a real subprocess probe
   (writes and executes an actual bash fixture script) with a negative
   control (`TestGenerate_NoSecretsInOutput`,
   `TestProbe_RealGeneratedServerStartsAndAnswers`, etc.), all PASS.
3. `mcpgen_cmd_build.txt` — real build of the `cmd/mcpgen` CLI wrapper.
4. `gate_e2e_fixture.txt` — the literal paired-mutation demonstration
   T-P5.04.4 asks for: generate → gate WIRED → hand-edit one path → gate
   NOT-WIRED (naming the drifted entry: `media-validator`) → regenerate →
   gate WIRED again → probe succeeds (`mcpgen probe media-validator
   started=true OK`).
5. `real_files_readonly_check.txt` — a read-only `check` run against the
   REAL repo-root manifest/`.mcp.json`, confirmed via `git status` to have
   written nothing. It honestly reports `media-validator` and `session-sync`
   as drifted (`CM-MCP-ENTRIES-GENERATED: NOT-WIRED`, exit 1) — this is the
   same real drift the current sweep's G46 still reports (pre-existing,
   documented in `specs/001-helixskills-incorporation/tasks.md`'s
   honest-boundary note; applying the generator to the real repo-root
   `.mcp.json` is a follow-up outside this stream's declared
   `helix_code/**`-only file scope).
6. `full_repo_build_after_p5.04.txt` — full repo build green after this
   change.

Copied verbatim from `qa-results/hxc159/mcp_generated/20260922T190432Z/`,
which is this commit's own cited evidence path (`465b814b`'s commit
message: "Evidence: qa-results/hxc159/mcp_generated/20260922T190432Z/").

## Files

- `captures/red_compile_fail.txt`
- `captures/green_pass.txt`
- `captures/mcpgen_cmd_build.txt`
- `captures/gate_e2e_fixture.txt`
- `captures/real_files_readonly_check.txt`
- `captures/full_repo_build_after_p5.04.txt`

## Result

The generator, gate, and probe are all real and working: RED→GREEN, a full
paired-mutation demonstration (drift introduced, gate catches it by name,
regeneration restores WIRED, probe confirms the restored entry actually
starts and answers). The read-only check against the real repo-root
`.mcp.json` honestly reports the pre-existing, out-of-scope drift rather
than silently passing or being suppressed.
