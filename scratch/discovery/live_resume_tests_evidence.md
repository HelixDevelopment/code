# Live resume-test evidence — helix_code session under provider aliases

Date: 2026-09-04. Session under test: `helix_code` (Claude Code alias `claude1`,
CLAUDE_CONFIG_DIR=`~/.claude-claude1`), transcript
`9bf7353c-587d-44d6-9612-7d643b56c74d.jsonl` (26.8 MB, ~178K tokens at test start).
Toolkit under test: `/home/milosvasic/Projects/claude_toolkit`, branch
`fix/helixllm-export-review-findings` (uncommitted batch at test time).

Method: `cd ~/Projects/helix_code && timeout 870 bash -c 'source
~/.local/share/claude-multi-account/aliases.sh; cma_run_provider <alias> -p
"Reply with exactly: <SENTINEL>"'`. Transcript forensics: `compact_boundary`
events counted before/after; serving model read from the last assistant event.

## Results

| Alias | Transport | Window (tokens) | Session ctx | RC | Sentinel | Compactions during run | Verdict |
|---|---|---|---|---|---|---|---|
| deepseek | native | 476000 | ~178K | 0 | `RESUME_OK` exact | 0 | PASS |
| helixagent | native (cma-proxy transform) | 120000 | ~178K | 0 | weak-model output (Agent tool echo from compacted context) | exactly 1, announced by the new resume-fit warning | PASS (machinery); model fidelity noted |
| helixllm-anton-qwen2-5-coder-3b-instruct-q4_k_m | router (ccr) | 117184 | unknown (gateway reports zero usage) | 0 | `RESUME_OK_HLLM` echoed first | 0 | PASS |

Evidence files: `/tmp/deepseek_resume_test.{out,err}`,
`/tmp/helixagent_resume_test.{out,err}`, `/tmp/helixllm_resume_test{,2}.{out,err}`.
Route attribution: last assistant event model =
`helixllm-anton-qwen2-5-coder-3b-instruct-q4_k_m-f6771589d190` (2026-09-04T06:53:52Z).

## Defects found by the live tests and fixed in the same batch

1. **Endless compaction loop (the operator-reported bug).** Root causes and
   fixes: see `compaction_loop_forensics.md` + `toolkit_compaction_rootcause.md`.
   Fixes: context-proportional compact cap `max(200000, context/2)` in
   `scripts/lib.sh`; degenerate-transcript skip (`_cma_session_resumable`,
   `CMA_SESSION_DEAD_BYTES`) + `context-size` subcommand in
   `scripts/claude-session.sh`; resume-fit warning on both launch paths.
2. **ccr upstream TLS CA gap.** `helixllm-anton` verified green (the verify
   probe honors `CMA_PROVIDER_CA_CERT`) but died at launch:
   `502 upstream request failed … x509: certificate signed by unknown authority`.
   Fix: router aliases build a combined CA bundle (system roots + the CA) under
   the per-alias `CCR_HOME` and pass it as `SSL_CERT_FILE` to every
   gateway-(re)spawning `ccr` call; native aliases export
   `NODE_EXTRA_CA_CERTS`; `helixllm-export --apply` persists
   `CMA_PROVIDER_CA_CERT` into the env record (and a re-apply without the
   anchor converges it away). Tests: `scripts/tests/test_ccr_upstream_ca.sh`
   (11 assertions, RED 6-fail → GREEN 11-pass) + CASE 9 in
   `test_helixllm_model_export.sh`.

## Honest limitations

- helixagent's facade model (`helixagent-llm`) is 3B-class: it did not follow
  the exact-reply instruction and emitted a replayed Agent tool call from the
  compacted context instead. Machinery (window guard, warning, single
  compaction, RC=0) is proven; model fidelity is a HelixLLM model-choice
  matter, not a toolkit defect.
- helixagent's gateway reports all-zero usage fields on assistant turns, so
  `claude-session context-size` honestly prints empty for sessions it last
  served (no fake numbers).
- HelixLLM claude mode (Qwen3-Coder-30B, 229376 ctx) is impossible on this
  host (RTX 3060 12GB, not the 5090/32GB the toolkit docs assume):
  `helixllm-gateway` / `helixagent-native` stay `unverified` — hardware-gated.
