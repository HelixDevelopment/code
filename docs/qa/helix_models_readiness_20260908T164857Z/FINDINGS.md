# HelixAgent :7061 model readiness — machine evidence
Session: 2026-09-08T16:48:57Z .. 17:01Z UTC. Endpoint owned solely by this agent (§11.4.119).
Investigation only: no file under claude_toolkit or submodules/helix_agent was modified; the :7061 service was not restarted or reconfigured.

## Wire-evidence files
plainchat_helixagent-llm.json / plainchat_helix-llm.json  -> nonce echo control (PASS)
toolcall_helixagent-llm.json / toolcall_helix-llm.json    -> tools array accepted, no tool_calls
tokens_notools.json / tokens_bigtools.json                -> 8353-byte tool schema == 0 prompt tokens
toolchoice_required.json                                  -> tool_choice:"required" returns prose, HTTP 200 (spec violation)
sentinel_helixagent-debate.json / sentinel_helix-debate.json / sentinel_helixagent-ensemble2.json
tools_ensemble.json / tools_debate.json / tools_llm_stream.txt / tools_debate_stream.txt
anthropic_tools.json                                      -> /v1/messages HTTP 400: tools unsupported

## Control needles (§11.4.273)
POSITIVE: bad model id -> HTTP 404 model_not_found; malformed JSON -> HTTP 400. Probe can detect rejection.
NEGATIVE: unknown field zzz_bogus_field_xyz -> HTTP 200, ignored. Unknown fields are silently dropped.
=> "tools accepted without error" is a real observation, not an instrument artifact.

## Measurements
prompt_tokens, message held constant, model helix-llm:
  no tools        = 43
  8353B of tools  = 43      (delta 0 -> tools never reach the model)
Sentinel wall time: helixagent-debate 54.9s, helix-debate 54.2s, helixagent-ensemble 18.5s (verifier default timeout = 30s).
Debate/ensemble responses ALL carry "model":"helixagent-ensemble" regardless of requested id.
Debate/ensemble "created" = -62135596800 (Go zero time) and completion_tokens=9 for a 9780-char body (usage is wrong).

## Source citations (submodules/helix_agent)
internal/handlers/openai_compatible.go:302        Tools []OpenAITool declared ("CRITICAL for AI coding assistants")
internal/handlers/openai_compatible.go:573-577    helixagent-llm + helix-llm -> processWithProviderChain (same branch)
internal/handlers/openai_compatible.go:2649-2660  handler DOES copy req.Tools -> llmReq.Tools
internal/handlers/openai_compatible.go:2937-2974  response mapper DOES map ToolCalls back to OpenAI
internal/handlers/openai_compatible.go:1471-1530  debate: user message reduced to a "topic", then expanded/sanitized
internal/llm/providers/helixllm/types.go:4-14     ChatCompletionRequest has NO Tools/ToolChoice field  <-- DROP POINT
internal/llm/providers/helixllm/types.go:17-21    Message has no ToolCalls; Choice (34-39) has no tool_calls
internal/llm/providers/helixllm/provider.go:275-282, 363-370  chatReq built without Tools (non-stream + stream)
internal/llm/providers/helixllm/provider.go:560-588 GetCapabilities: SupportedFeatures = ["streaming"] only;
                                                   comment states tools/function-calling reported FALSE by design
internal/handlers/anthropic_compatible.go:127     /v1/messages explicitly refuses tools (Finding #20)
