# Pass-through feasibility investigation — HelixAgent debate/ensemble routes

Session: 2026-09-08. Investigation only — no file in `submodules/helix_agent` was modified.
Submodule HEAD at investigation time: `b4033a6966be6ac29a0b0322d203a08cc1ca3f71`.
Service under test: http://127.0.0.1:7061 (owned for this task per §11.4.119).

## ANSWER
Pass-through is **ALREADY POSSIBLE**, via two undocumented routing side-effects.
But it works by **skipping the debate**, not by making the debate honour an instruction.

## Where the literal text is lost (non-streaming, single-turn)
- `internal/services/agentic_ensemble.go:172` — `topic := e.extractUserMessage(req)`
- `internal/services/agentic_ensemble.go:569-572` — returns ONLY last `role=="user"` content
- `internal/services/agentic_ensemble.go:193` — `Topic: topic` into `DebateConfig`
- `internal/services/debate_support_types.go:81-100` — `DebateConfig` has NO messages field
- `internal/services/debate_service.go:1706-1708` — hardcoded debate preamble wraps the text
- `internal/handlers/openai_compatible.go:3959` — `Model: "helixagent-ensemble"` hardcoded

## Existing bypasses (both CONFIRMED empirically)
1. `openai_compatible.go:671` — `multiTurnUserCount > 1` -> `processWithProviderChain`
2. `openai_compatible.go:2679` — `len(req.Tools) > 0` -> `processWithDirectProvider`

## Probe evidence
- `pt_probes_raw.txt` / `pt_probes_summary.txt` — round 1 (P1-P7)
- `pt_probes2_raw.txt` / `pt_probes2_summary.txt` — round 2 (R1-R8)
- `pt_probes3_raw.txt` / `pt_probes3_summary.txt` — round 3 (S1-S4)
- `pt_all_probes_summary.txt` — combined one-line summaries
