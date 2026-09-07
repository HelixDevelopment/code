package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"dev.helix.code/internal/llm"
	"dev.helix.code/internal/testutil/wirecorpus"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// llm_generate_toolcall_replay_test.go — the DETERMINISTIC half of the
// tool-calling capability-divergence proof.
//
// ============================================================================
// WHY THIS FILE EXISTS — DO NOT "HELPFULLY" RESTORE A LIVE ASSERTION HERE
// ============================================================================
//
// The capability divergence guarded below used to be asserted against a LIVE
// model. It cannot be. MEASURED 2026-09-07 against the HelixLLM gateway at
// https://127.0.0.1:8443/v1 — twelve byte-identical POSTs to
// /v1/chat/completions, temperature 0, max_tokens 64, one fixed prompt and one
// fixed nonce:
//
//	8/12  finish_reason=tool_calls   tool_calls PRESENT
//	4/12  finish_reason=length       tool_calls ABSENT
//
// VERDICT: NON-DETERMINISTIC — two distinct outcomes for an identical request
// at temperature 0. Independently reproduced by running the live guard five
// times back to back: PASS, FAIL, PASS, PASS, PASS (the FAIL was
// `expected: "tool_calls" / actual: "length"`).
//
// Retries, longer timeouts and larger token budgets CANNOT fix this: the
// non-determinism lives in the system under test, not in the harness. A live
// single-outcome assertion is therefore incompatible with the operator's
// mandate that all proofs be FULLY DETERMINISTIC.
//
// THE DESIGN (record-and-replay + an opt-in live distribution probe):
//
//   - This file replays REAL captured wire bytes — recorded from the real
//     gateway and the real coder by
//     helix_code/testdata/toolcall_wire_corpus/capture.py, committed under
//     helix_code/testdata/toolcall_wire_corpus, sha256-pinned in
//     provenance.json and verified on every load — through the SAME production
//     seam the live guard used: llmProviderResolver -> resolveLLMProvider ->
//     resolveHelixLLMGatewayProvider / resolveHelixLLMLocalProvider ->
//     llm.OpenAICompatibleProvider.Generate -> parseOpenAIWireToolCalls.
//     The bytes are genuine captured evidence (§11.4.5); the verdict is a
//     function of OUR code alone — no network, no model, no clock.
//
//   - The live probe is NOT deleted and NOT turned into a no-op: see
//     TestGatewayLive_ToolCallingCapabilityDivergence in
//     llm_generate_gateway_live_test.go, which now asserts a DISTRIBUTION over
//     N runs (the gateway must still be CAPABLE of structured tool calls) and
//     skips honestly by default (§11.4.3).
//
// HONEST BOUNDARY (§11.4.6): this replay proves our parsing and routing handle
// the shapes the backends really produced at capture time. It cannot prove the
// backend still produces them today — that is exactly what the opt-in live
// probe is for. Neither claim is stronger than its evidence.
// ============================================================================

// toolCallReplayBackend is a recorded backend: a real TCP httptest server that
// answers the two OpenAI-compatible endpoints the production provider dials,
// returning captured bytes verbatim.
//
// It is deliberately NOT a mock of our own code: it stands in for the REMOTE
// process only. Everything on this side of the socket — the provider
// construction, the HTTP client, the JSON decoding, the tool-call
// normalisation, and (in the wire-facade replay) the gin router, the auth
// middleware and both wire facades — is the real, unmodified production path.
type toolCallReplayBackend struct {
	*httptest.Server
	chatRequests chan []byte
}

// newToolCallReplayBackend serves modelsBody at GET /v1/models and, for POST
// /v1/chat/completions, whatever chatFor returns for that request's body.
func newToolCallReplayBackend(t *testing.T, modelsBody []byte, chatFor func(reqBody []byte) []byte) *toolCallReplayBackend {
	t.Helper()
	b := &toolCallReplayBackend{chatRequests: make(chan []byte, 64)}

	mux := http.NewServeMux()
	mux.HandleFunc("/v1/models", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(modelsBody)
	})
	mux.HandleFunc("/v1/chat/completions", func(w http.ResponseWriter, r *http.Request) {
		buf := make([]byte, 0, 4096)
		tmp := make([]byte, 4096)
		for {
			n, err := r.Body.Read(tmp)
			buf = append(buf, tmp[:n]...)
			if err != nil {
				break
			}
		}
		select {
		case b.chatRequests <- buf:
		default:
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(chatFor(buf))
	})

	b.Server = httptest.NewServer(mux)
	t.Cleanup(b.Server.Close)
	return b
}

// TestToolCallWireCorpus_CapabilityDivergence_Replay is the DETERMINISTIC
// standing guard for the property the live test used to assert
// non-deterministically: for the identical tool-calling prompt, the gateway
// route yields STRUCTURED tool calls while the coder route yields a fenced
// text blob with finish_reason=stop. Both halves are replayed from real
// captured bytes through the real production routing seam.
//
// Same verdict on every run, with the live services up OR down.
func TestToolCallWireCorpus_CapabilityDivergence_Replay(t *testing.T) {
	corpus := wirecorpus.Load(t)
	nonce := corpus.Provenance.FrozenNonce
	require.NotEmptyf(t, nonce, "corpus provenance must record the frozen capture nonce")

	// Sanity floor on the corpus itself (§11.4.107(10)): a replay guard that
	// would pass against a degenerate/empty recording is worthless, so assert
	// the recorded gateway body genuinely carries the shape under test BEFORE
	// asserting our parser extracts it.
	rawGateway := corpus.Body(t, wirecorpus.GatewayWeatherToolCall)
	require.Containsf(t, string(rawGateway), `"tool_calls"`,
		"the recorded gateway body must actually contain a tool_calls array — "+
			"otherwise this guard would be asserting nothing")
	require.Containsf(t, string(rawGateway), nonce,
		"the recorded gateway body must carry the capture nonce")

	gatewayBackend := newToolCallReplayBackend(t,
		corpus.Body(t, wirecorpus.GatewayModels),
		func([]byte) []byte { return rawGateway })
	coderBackend := newToolCallReplayBackend(t,
		corpus.Body(t, wirecorpus.CoderModels),
		func([]byte) []byte { return corpus.Body(t, wirecorpus.CoderWeatherFencedStop) })

	// Point the REAL resolvers at the replayed backends. Nothing else about
	// the production path is substituted.
	t.Setenv(helixLLMGatewayEndpointEnv, gatewayBackend.URL)
	t.Setenv(helixLLMLocalOpenAIEndpointEnv, coderBackend.URL)

	gatewayProvider, err := llmProviderResolver("gateway", "")
	require.NoError(t, err, "gateway provider must construct via the production routing seam")
	defer func() { _ = gatewayProvider.Close() }()

	coderProvider, err := llmProviderResolver("helixllm", "")
	require.NoError(t, err, "coder provider must construct via the production routing seam")
	defer func() { _ = coderProvider.Close() }()

	tools := []llm.Tool{{
		Type: "function",
		Function: llm.ToolFunction{
			Name:        "get_weather",
			Description: "Get the current weather for a city",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"city": map[string]interface{}{"type": "string"},
				},
				"required": []interface{}{"city"},
			},
		},
	}}
	prompt := fmt.Sprintf(
		"What is the weather in the city named exactly %q? You MUST call the get_weather tool with that exact city string.",
		nonce)
	baseReq := llm.LLMRequest{
		Messages:    []llm.Message{{Role: "user", Content: prompt}},
		Tools:       tools,
		MaxTokens:   64,
		Temperature: 0,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	t.Run("gateway_emits_structured_tool_calls", func(t *testing.T) {
		req := baseReq
		req.ID = uuid.New()
		req.Model = resolveDefaultModel(gatewayProvider, "")

		resp, genErr := gatewayProvider.Generate(ctx, &req)
		require.NoError(t, genErr)
		require.Equalf(t, "tool_calls", resp.FinishReason,
			"replayed gateway bytes carry finish_reason=tool_calls; got %q content=%q",
			resp.FinishReason, resp.Content)
		require.Lenf(t, resp.ToolCalls, 1,
			"the production tool-call normaliser must surface exactly one structured call from the "+
				"recorded bytes; got %d (content=%q)", len(resp.ToolCalls), resp.Content)
		tc := resp.ToolCalls[0]
		require.Equal(t, "get_weather", tc.Function.Name)
		require.Equalf(t, "function", tc.Type, "tool call type must be normalised to \"function\"")
		require.Containsf(t, fmt.Sprintf("%v", tc.Function.Arguments["city"]), nonce,
			"the OpenAI wire encodes arguments as a JSON-ENCODED STRING; the production parser must "+
				"decode it into a typed map whose city echoes the recorded nonce, got %v",
			tc.Function.Arguments)

		t.Logf("PASS (gateway, replayed from %s captured %s): finish_reason=%q tool_calls=%+v",
			wirecorpus.GatewayWeatherToolCall, corpus.Provenance.CapturedAtUTC,
			resp.FinishReason, resp.ToolCalls)
	})

	t.Run("coder_emits_fenced_blob_not_structured_tool_calls", func(t *testing.T) {
		req := baseReq
		req.ID = uuid.New()
		req.Model = resolveDefaultModel(coderProvider, "")

		resp, genErr := coderProvider.Generate(ctx, &req)
		require.NoError(t, genErr)
		require.Emptyf(t, resp.ToolCalls,
			"the coder does NOT emit structured tool_calls for this request — that divergence is the "+
				"documented reason the gateway route exists; got %+v", resp.ToolCalls)
		require.Equalf(t, "stop", resp.FinishReason,
			"the coder reports finish_reason=stop (not tool_calls); got %q", resp.FinishReason)
		require.Containsf(t, resp.Content, "get_weather",
			"the coder's fenced tool-call attempt must still be observable in content: got %q", resp.Content)
		require.Containsf(t, resp.Content, nonce,
			"the coder's fenced blob must carry the recorded nonce city: got %q", resp.Content)

		t.Logf("PASS (coder contrast, replayed from %s): finish_reason=%q content=%q",
			wirecorpus.CoderWeatherFencedStop, resp.FinishReason, resp.Content)
	})

	// The OTHER real gateway outcome — the 4/12 finish_reason=length case — is
	// also in the corpus, and is replayed here so the divergence guard above
	// cannot be mistaken for a claim that the gateway is deterministic. This
	// subtest asserts our code handles that branch honestly too: no tool calls
	// are invented out of a truncated completion.
	t.Run("gateway_length_truncation_yields_no_invented_tool_calls", func(t *testing.T) {
		truncBackend := newToolCallReplayBackend(t,
			corpus.Body(t, wirecorpus.GatewayModels),
			func([]byte) []byte { return corpus.Body(t, wirecorpus.GatewayWeatherNoTool) })
		t.Setenv(helixLLMGatewayEndpointEnv, truncBackend.URL)

		provider, resolveErr := llmProviderResolver("gateway", "")
		require.NoError(t, resolveErr)
		defer func() { _ = provider.Close() }()

		req := baseReq
		req.ID = uuid.New()
		req.Model = resolveDefaultModel(provider, "")

		resp, genErr := provider.Generate(ctx, &req)
		require.NoError(t, genErr)
		require.Equalf(t, "length", resp.FinishReason,
			"this recorded branch is the truncated one; got %q", resp.FinishReason)
		require.Emptyf(t, resp.ToolCalls,
			"a truncated completion carries NO tool_calls on the wire, and the parser must not "+
				"synthesise any from the free text; got %+v", resp.ToolCalls)

		t.Logf("PASS (gateway truncation branch, replayed from %s): finish_reason=%q tool_calls=%d — "+
			"this is the 4/12 outcome that made the live assertion non-deterministic",
			wirecorpus.GatewayWeatherNoTool, resp.FinishReason, len(resp.ToolCalls))
	})
}

// TestToolCallWireCorpus_Provenance_IsRealCapturedEvidence asserts the corpus
// is what it claims to be: recorded from live endpoints, sha256-pinned, and
// carrying BOTH observed gateway outcomes. Without this, "record-and-replay"
// would be indistinguishable from "someone typed a fixture" — the precise
// failure this whole design is meant to avoid (§11.4.107(10)).
func TestToolCallWireCorpus_Provenance_IsRealCapturedEvidence(t *testing.T) {
	corpus := wirecorpus.Load(t) // sha256-verifies every recorded body

	p := corpus.Provenance
	require.NotEmpty(t, p.CapturedAtUTC, "provenance must record WHEN the capture happened")
	require.NotEmpty(t, p.GatewayEndpoint, "provenance must record WHICH gateway was recorded")
	require.NotEmpty(t, p.CoderEndpoint, "provenance must record WHICH coder was recorded")
	require.NotEmpty(t, p.FrozenNonce, "provenance must record the frozen capture nonce")

	// The measurement that forced this design must travel with the corpus, so
	// a future reader can see WHY a live assertion was removed.
	m := p.NondeterminismMeasurement
	require.Equal(t, 12, m.Requests)
	require.Equal(t, 8, m.ToolCallsPresent)
	require.Equal(t, 4, m.FinishReasonLengthToolCallsAbsent)
	require.Containsf(t, m.Verdict, "NON-DETERMINISTIC",
		"the recorded verdict for 12 identical requests must be the measured one")

	// Both branches present — see the subtest above for why.
	for _, name := range []string{
		wirecorpus.GatewayWeatherToolCall,
		wirecorpus.GatewayWeatherNoTool,
		wirecorpus.CoderWeatherFencedStop,
	} {
		require.NotEmpty(t, corpus.Body(t, name), name)
	}

	// The two gateway branches must genuinely differ, or the corpus is not
	// recording a non-deterministic backend at all.
	withTool := string(corpus.Body(t, wirecorpus.GatewayWeatherToolCall))
	withoutTool := string(corpus.Body(t, wirecorpus.GatewayWeatherNoTool))
	require.NotEqual(t, withTool, withoutTool)
	require.Contains(t, withTool, `"finish_reason":"tool_calls"`)
	require.NotContains(t, withoutTool, `"tool_calls"`)

	// And the coder recording must be the fenced-text contrast, not a second
	// copy of a structured response.
	var coderParsed struct {
		Choices []struct {
			FinishReason string `json:"finish_reason"`
			Message      struct {
				Content   string            `json:"content"`
				ToolCalls []json.RawMessage `json:"tool_calls"`
			} `json:"message"`
		} `json:"choices"`
	}
	require.NoError(t, json.Unmarshal(corpus.Body(t, wirecorpus.CoderWeatherFencedStop), &coderParsed))
	require.Len(t, coderParsed.Choices, 1)
	require.Equal(t, "stop", coderParsed.Choices[0].FinishReason)
	require.Empty(t, coderParsed.Choices[0].Message.ToolCalls)
	require.True(t, strings.Contains(coderParsed.Choices[0].Message.Content, "get_weather"),
		"the coder recording must show the tool call arriving as text")

	t.Logf("PASS: corpus at %s captured %s from gateway=%s coder=%s, %d bodies sha256-verified; "+
		"recorded non-determinism = %d/%d tool_calls present, %d/%d truncated",
		corpus.Dir, p.CapturedAtUTC, p.GatewayEndpoint, p.CoderEndpoint, len(p.Files),
		m.ToolCallsPresent, m.Requests, m.FinishReasonLengthToolCallsAbsent, m.Requests)
}
