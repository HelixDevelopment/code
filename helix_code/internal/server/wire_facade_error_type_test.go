package server

// wire_facade_error_type_test.go — guard for the `type` field both wire facades
// put in a provider-resolution error body.
//
// THE DEFECT. Each facade emitted ONE constant type for every resolution
// failure regardless of status: "server_error" on the OpenAI shape,
// "api_error" on the Anthropic shape. That was already inconsistent with the
// 400s the same two handlers emit a few lines earlier ("invalid_request_error"),
// and it became actively wrong once providerResolveStatus started answering 403
// for a cloud-gate refusal: a policy refusal arrived carrying a 5xx-flavoured
// type. Both SDKs branch on `type`, so a client reading that body saw a server
// outage and retried something no retry can clear.
//
// SCOPE OF THE CHANGE, stated precisely: the 5xx bodies are byte-identical to
// what shipped before — OpenAI keeps "server_error", Anthropic keeps
// "api_error", because those are the names the two real APIs use for their
// server-side class. Only the 400 and the 403 bodies change.

import (
	"encoding/json"
	"errors"
	"net/http"
	"testing"

	"dev.helix.code/internal/llm"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWireErrorTypes_AreDerivedFromStatus(t *testing.T) {
	cases := []struct {
		status        int
		wantOpenAI    string
		wantAnthropic string
	}{
		{http.StatusBadRequest, "invalid_request_error", "invalid_request_error"},
		{http.StatusUnauthorized, "authentication_error", "authentication_error"},
		{http.StatusForbidden, "permission_error", "permission_error"},
		{http.StatusNotFound, "not_found_error", "not_found_error"},
		{http.StatusTooManyRequests, "rate_limit_error", "rate_limit_error"},
		// The two families diverge ONLY here, and both values are the ones
		// that shipped before this change.
		{http.StatusInternalServerError, "server_error", "api_error"},
		{http.StatusBadGateway, "server_error", "api_error"},
		{http.StatusServiceUnavailable, "server_error", "api_error"},
	}
	for _, tc := range cases {
		assert.Equalf(t, tc.wantOpenAI, openAIErrorType(tc.status),
			"openAIErrorType(%d)", tc.status)
		assert.Equalf(t, tc.wantAnthropic, anthropicErrorType(tc.status),
			"anthropicErrorType(%d)", tc.status)
	}

	// A constant-type implementation would pass a single-row table. Asserting
	// the map is not degenerate is what makes the rows above load-bearing.
	require.NotEqual(t, openAIErrorType(http.StatusForbidden),
		openAIErrorType(http.StatusInternalServerError),
		"a 403 and a 500 must not carry the same type — that identity IS the defect")
	require.NotEqual(t, anthropicErrorType(http.StatusForbidden),
		anthropicErrorType(http.StatusInternalServerError),
		"a 403 and a 500 must not carry the same type — that identity IS the defect")
}

// resolverReturning stubs the shared resolution seam with a fixed error, which
// is the only input either handler needs to reach the body under test.
func resolverReturning(t *testing.T, err error) {
	t.Helper()
	prev := llmProviderResolver
	llmProviderResolver = func(string, string) (llm.Provider, error) { return nil, err }
	t.Cleanup(func() { llmProviderResolver = prev })
}

// TestWireFacades_GateRefusalBodyIsAPermissionError drives BOTH real handlers
// end to end with a REAL cloud-gate refusal error and reads the body a client
// would actually receive.
//
// MUTATION THAT MAKES THIS FAIL (§1.1): restore the constant in either handler
// (`"type": "server_error"` / `"type": "api_error"`) — the status stays 403 and
// the type assertion fails.
func TestWireFacades_GateRefusalBodyIsAPermissionError(t *testing.T) {
	// The caller-named-hosted-provider refusal: llm.ErrCloudDisabled alone,
	// which providerResolveStatus maps to 403.
	gateErr := cloudDisabledError("anthropic", providerSourceRequest, llm.ErrCloudDisabled)
	require.Equal(t, http.StatusForbidden, providerResolveStatus(gateErr),
		"precondition: this error must map to 403, else the guard proves nothing")
	resolverReturning(t, gateErr)

	srv := &Server{}

	t.Run("openai_shape", func(t *testing.T) {
		w, raw := postSSE(t, "/v1/chat/completions", srv.chatCompletions,
			`{"model":"m","messages":[{"role":"user","content":"hi"}]}`)
		require.Equal(t, http.StatusForbidden, w.Code, "body=%s", raw)

		var body struct {
			Error struct {
				Type    string `json:"type"`
				Message string `json:"message"`
			} `json:"error"`
		}
		require.NoError(t, json.Unmarshal([]byte(raw), &body), "body=%s", raw)
		assert.Equal(t, "permission_error", body.Error.Type,
			"a 403 must not be typed as a server fault — an SDK branching on "+
				"`type` would report an outage and retry a deterministic refusal; body=%s", raw)
		assert.NotEmpty(t, body.Error.Message, "the message must still reach the caller")
	})

	t.Run("anthropic_shape", func(t *testing.T) {
		w, raw := postSSE(t, "/v1/messages", srv.anthropicMessages,
			`{"model":"m","max_tokens":16,"messages":[{"role":"user","content":"hi"}]}`)
		require.Equal(t, http.StatusForbidden, w.Code, "body=%s", raw)

		var body struct {
			Type  string `json:"type"`
			Error struct {
				Type    string `json:"type"`
				Message string `json:"message"`
			} `json:"error"`
		}
		require.NoError(t, json.Unmarshal([]byte(raw), &body), "body=%s", raw)
		assert.Equal(t, "error", body.Type, "the envelope shape is unchanged")
		assert.Equal(t, "permission_error", body.Error.Type, "body=%s", raw)
		assert.NotEmpty(t, body.Error.Message)
	})
}

// TestWireFacades_ServerFaultBodyKeepsItsHistoricalType is the non-regression
// half: the 5xx bodies — the only ones any existing consumer has ever seen from
// this path — must be unchanged.
func TestWireFacades_ServerFaultBodyKeepsItsHistoricalType(t *testing.T) {
	// A plain construction failure: neither sentinel, so providerResolveStatus
	// falls through to 503, exactly as it did before this change.
	resolverReturning(t, errors.New("failed to construct provider: dial tcp: connection refused"))
	srv := &Server{}

	w, raw := postSSE(t, "/v1/chat/completions", srv.chatCompletions,
		`{"model":"m","messages":[{"role":"user","content":"hi"}]}`)
	require.Equal(t, http.StatusServiceUnavailable, w.Code, "body=%s", raw)
	assert.Contains(t, raw, `"type":"server_error"`,
		"the OpenAI facade's 5xx type is unchanged by this fix")

	w, raw = postSSE(t, "/v1/messages", srv.anthropicMessages,
		`{"model":"m","max_tokens":16,"messages":[{"role":"user","content":"hi"}]}`)
	require.Equal(t, http.StatusServiceUnavailable, w.Code, "body=%s", raw)
	assert.Contains(t, raw, `"type":"api_error"`,
		"the Anthropic facade's 5xx type is unchanged by this fix")
}
