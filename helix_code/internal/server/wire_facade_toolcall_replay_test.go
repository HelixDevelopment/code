package server

import (
	"encoding/json"
	"fmt"
	"testing"

	"dev.helix.code/internal/testutil/wirecorpus"
	"github.com/stretchr/testify/require"
)

// wire_facade_toolcall_replay_test.go — the DETERMINISTIC full-HTTP proof of
// the tool-call wire-shape divergence between the two facades.
//
// ============================================================================
// WHY THIS FILE EXISTS — DO NOT REPLACE IT WITH A LIVE ASSERTION
// ============================================================================
//
// TestWireFacade_FullHTTP_E2E_LiveRoundTrip/tool_calls_shape_divergence_live
// asserted this same divergence against a LIVE model, and could not be made to
// hold:
//
//   - Against the CODER (localhost:18434, the route that test configures) it
//     FAILED 5/5 in a back-to-back BEFORE baseline, always for the same
//     reason: the coder never emits native tool_calls, it returns the call as
//     a fenced ```json blob with finish_reason "stop" (measured:
//     `expected: "tool_calls" / actual: "stop"` on the OpenAI facade and
//     `expected: "tool_use" / actual: "max_tokens"` on the Anthropic one).
//     That is a STABLE, reproducible property of the coder — so the assertion
//     is simply unsatisfiable there.
//
//   - Against the GATEWAY (https://127.0.0.1:8443/v1) — the redirection that
//     ledger item HXC-002-F3-09 proposed — it becomes NON-DETERMINISTIC.
//     MEASURED 2026-09-07: twelve byte-identical POSTs, temperature 0,
//     max_tokens 64, fixed prompt and fixed nonce, produced 8/12
//     finish_reason=tool_calls (tool_calls PRESENT) and 4/12
//     finish_reason=length (tool_calls ABSENT). Running the sibling live guard
//     five times in a row reproduced the split directly: PASS, FAIL, PASS,
//     PASS, PASS.
//
// So neither backend can carry a deterministic single-outcome live assertion.
// This guard therefore replays REAL captured gateway bytes
// (helix_code/testdata/toolcall_wire_corpus, sha256-pinned in provenance.json,
// re-recordable with helix_code/testdata/toolcall_wire_corpus/capture.py)
// through the ENTIRE production HTTP path — the real `curl` binary over a real
// TCP socket, the real gin router, the real wireFacadeAuthMiddleware, the real
// chatCompletions / anthropicMessages handlers, the real
// resolveLLMProvider("helixllm") routing, the real
// llm.OpenAICompatibleProvider and the real llmResponseToOpenAI /
// llmResponseToAnthropic converters. ONLY the remote model process is replaced
// — by a recording of itself.
//
// HONEST BOUNDARY (§11.4.6): this proves our facades render the divergence
// correctly for wire bytes the backend really produced. It does not prove the
// backend produces them today; that is the opt-in live probe's job (see
// TestGatewayLive_ToolCallingCapabilityDivergence).
// ============================================================================

// TestWireFacade_FullHTTP_E2E_ReplayToolCallShapes drives real HTTP through
// the real server and asserts the documented fork point: the OpenAI wire
// encodes tool_calls[].function.arguments as a JSON-ENCODED STRING, while the
// Anthropic wire encodes content[].{type:"tool_use"}.input as a JSON OBJECT —
// from ONE upstream response, so the divergence is genuinely produced by our
// converters rather than by two independently-shaped upstream replies.
//
// Same verdict on every run, with the live services up OR down.
func TestWireFacade_FullHTTP_E2E_ReplayToolCallShapes(t *testing.T) {
	corpus := wirecorpus.Load(t)
	nonce := corpus.Provenance.FrozenNonce
	require.NotEmpty(t, nonce, "corpus provenance must record the frozen capture nonce")

	recorded := corpus.Body(t, wirecorpus.GatewayWeatherToolCall)
	require.Containsf(t, string(recorded), `"tool_calls"`,
		"the recorded upstream body must genuinely carry tool_calls, or this guard asserts nothing")

	backend := newToolCallReplayBackend(t,
		corpus.Body(t, wirecorpus.GatewayModels),
		func([]byte) []byte { return recorded })

	// Same routing the live E2E test configures: the wire-facade handlers call
	// llmProviderResolver("", model), so resolution falls through to
	// HELIX_LLM_PROVIDER.
	t.Setenv("HELIX_LLM_PROVIDER", "helixllm")
	t.Setenv(helixLLMLocalOpenAIEndpointEnv, backend.URL)

	ts := wireFacadeE2EFixture(t)

	toolsJSON := `[{"type":"function","function":{"name":"get_weather","description":"Get the current weather for a city","parameters":{"type":"object","properties":{"city":{"type":"string"}},"required":["city"]}}}]`
	prompt := fmt.Sprintf(
		"What is the weather in the city named exactly %q? You MUST call the get_weather tool with that exact city string.",
		nonce)

	t.Run("openai_tool_calls_arguments_is_json_string", func(t *testing.T) {
		reqBody := fmt.Sprintf(
			`{"messages":[{"role":"user","content":%q}],"tools":%s,"max_tokens":64,"temperature":0}`,
			prompt, toolsJSON)
		headers := []string{"Authorization: Bearer " + wireFacadeE2ETestAPIKey}
		status, respBody, _ := curlCapture(t, ts.URL+"/v1/chat/completions", headers, reqBody)
		require.Equalf(t, 200, status,
			"authenticated tool-calling POST /v1/chat/completions must succeed — got %d body=%s", status, respBody)

		var parsed struct {
			Choices []struct {
				Message struct {
					ToolCalls []struct {
						Type     string `json:"type"`
						Function struct {
							Name string `json:"name"`
							// MUST decode into a Go string straight off the wire,
							// which is only possible if the wire bytes were
							// `"arguments":"{...}"` — a quoted, JSON-encoded string.
							Arguments string `json:"arguments"`
						} `json:"function"`
					} `json:"tool_calls"`
				} `json:"message"`
				FinishReason string `json:"finish_reason"`
			} `json:"choices"`
		}
		require.NoErrorf(t, json.Unmarshal([]byte(respBody), &parsed),
			"response must be valid OpenAI-shaped JSON: %s", respBody)
		require.Len(t, parsed.Choices, 1)
		require.Equal(t, "tool_calls", parsed.Choices[0].FinishReason)
		require.Len(t, parsed.Choices[0].Message.ToolCalls, 1)
		tc := parsed.Choices[0].Message.ToolCalls[0]
		require.Equal(t, "get_weather", tc.Function.Name)
		require.NotEmpty(t, tc.Function.Arguments,
			"OpenAI wire tool_calls[].function.arguments must be a non-empty JSON-encoded string")

		var argsObj map[string]interface{}
		require.NoErrorf(t, json.Unmarshal([]byte(tc.Function.Arguments), &argsObj),
			"function.arguments must itself parse as a JSON object once string-decoded "+
				"(the JSON-STRING-of-JSON wire shape): %q", tc.Function.Arguments)
		require.Containsf(t, fmt.Sprintf("%v", argsObj["city"]), nonce,
			"tool arguments must echo the recorded nonce city: %v", argsObj)

		t.Logf("PASS (OpenAI wire, replayed): arguments is a JSON-ENCODED STRING = %q (decodes to %v)",
			tc.Function.Arguments, argsObj)
	})

	t.Run("anthropic_tool_use_input_is_json_object", func(t *testing.T) {
		reqBody := fmt.Sprintf(
			`{"messages":[{"role":"user","content":%q}],"tools":[{"name":"get_weather","description":"Get the current weather for a city","input_schema":{"type":"object","properties":{"city":{"type":"string"}},"required":["city"]}}],"max_tokens":64,"temperature":0}`,
			prompt)
		headers := []string{"x-api-key: " + wireFacadeE2ETestAPIKey}
		status, respBody, _ := curlCapture(t, ts.URL+"/v1/messages", headers, reqBody)
		require.Equalf(t, 200, status,
			"authenticated tool-calling POST /v1/messages must succeed — got %d body=%s", status, respBody)

		var parsed struct {
			StopReason string `json:"stop_reason"`
			Content    []struct {
				Type string `json:"type"`
				Name string `json:"name"`
				// MUST decode into a Go map straight off the wire, which is only
				// possible if the wire bytes were `"input":{...}` — a real object.
				Input map[string]interface{} `json:"input"`
			} `json:"content"`
		}
		require.NoErrorf(t, json.Unmarshal([]byte(respBody), &parsed),
			"response must be valid Anthropic-shaped JSON: %s", respBody)
		require.Equal(t, "tool_use", parsed.StopReason)

		var found bool
		for _, block := range parsed.Content {
			if block.Type != "tool_use" {
				continue
			}
			found = true
			require.Equal(t, "get_weather", block.Name)
			require.NotEmpty(t, block.Input, "Anthropic wire tool_use.input must be a non-empty JSON OBJECT")
			require.Containsf(t, fmt.Sprintf("%v", block.Input["city"]), nonce,
				"tool input must echo the recorded nonce city: %v", block.Input)
			t.Logf("PASS (Anthropic wire, replayed): content[].input is a JSON OBJECT = %v", block.Input)
		}
		require.Truef(t, found, "response content must include a tool_use block: %s", respBody)
		require.NotContainsf(t, respBody, `"input":"{`,
			"tool_use.input must be a raw JSON object on the wire, never a JSON-encoded string")
	})
}
