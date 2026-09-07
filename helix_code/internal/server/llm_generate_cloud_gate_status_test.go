package server

// Regression guard for the cloud-gate refusal status (§11.4.115 RED-first).
//
// THE DEFECT (pre-fix, reproduced by RED_CLOUD_GATE_STATUS=1 / RED_MODE=1):
// llm.ErrCloudDisabled — returned by llm.NewCloudProvider when a hosted
// provider is requested while the W2c-1 cloud gate is closed — was wrapped by
// resolveLLMProvider as a plain "failed to construct provider" error, and
// providerResolveStatus had no case for it, so it fell through to
// 503 Service Unavailable.
//
// That is the SAME defect class the 400→500 provenance fix addressed: 503
// advertises a TEMPORARY condition and is the status that carries Retry-After,
// so a well-behaved client retry-loops forever against a deterministic
// configuration state (llm.cloud.enabled: false) that no amount of waiting
// clears. See providerResolveStatus's own doc-comment, which already stated
// the rule the gate case was not routed through.
//
// THE FIX: the gate refusal is classified by the SAME providerSource
// provenance the unknown-provider path already uses —
//   - the CALLER named a hosted provider    -> 403 Forbidden (policy refusal
//     of the caller's own, well-formed input; not malformed, so not 400)
//   - a SERVER-SIDE source named it (config llm.default_provider or the
//     process's HELIX_LLM_PROVIDER) -> 500, wrapping errServerProviderMisconfigured
//     exactly as the unresolvable-name case does.
//
// This guard's registered polarity switch is RED_CLOUD_GATE_STATUS
// (red_polarity_convention_test.go rules 1 + 5). It is passed to redModeFor as
// a LITERAL, not via a named constant: the convention file's own conformance
// scanner enumerates the package's switches by matching
// `redModeFor(t, "NAME")` textually.
//
// Reproduction (RED, on a PRE-FIX artifact — asserts the defect is present):
//
//	RED_CLOUD_GATE_STATUS=1 <toolchain> test -count=1 -v \
//	  -run TestProviderResolveStatus_CloudGateClosed ./internal/server/...
//
// Standing guard (GREEN, default):
//
//	<toolchain> test -count=1 -v \
//	  -run TestProviderResolveStatus_CloudGateClosed ./internal/server/...

import (
	"errors"
	"net/http"
	"strings"
	"testing"

	"dev.helix.code/internal/llm"
)

// withClosedCloudGate closes the process-wide W2c-1 cloud gate for the
// duration of the test and restores the previous state afterwards. The gate is
// process-global (an atomic in internal/llm), so leaving it flipped would leak
// into sibling tests in this package.
func withClosedCloudGate(t *testing.T) {
	t.Helper()
	prev := llm.CloudEnabled()
	llm.SetCloudEnabled(false)
	t.Cleanup(func() { llm.SetCloudEnabled(prev) })
}

// pinProviderSources pins BOTH server-side provider-name sources so a row
// exercises exactly one provenance. Passing "" for either leaves that source
// empty (not merely "whatever the host happens to export / the config file
// happens to hold"), which is what makes the provenance assertions honest.
func pinProviderSources(t *testing.T, env, cfg string) {
	t.Helper()
	t.Setenv(llmProviderEnv, env)
	prev := configDefaultProviderFunc
	configDefaultProviderFunc = func() string { return cfg }
	t.Cleanup(func() { configDefaultProviderFunc = prev })
}

// pinLocalRouteEndpoints pins ALL THREE local-route endpoint variables so a
// row exercises exactly one endpoint provenance and never inherits whatever
// the host happens to export. Passing "" leaves that route on its compiled-in
// loopback default, which is what every non-local-route row wants.
func pinLocalRouteEndpoints(t *testing.T, local, gateway, llamaCpp string) {
	t.Helper()
	t.Setenv(helixLLMLocalOpenAIEndpointEnv, local)
	t.Setenv(helixLLMGatewayEndpointEnv, gateway)
	t.Setenv(llamaCppHostEnv, llamaCpp)
}

// TestProviderResolveStatus_CloudGateClosed_IsNotRetryable drives the REAL
// shipped resolveLLMProvider with the REAL cloud gate closed (no stub
// resolver, no hand-rolled error) and asserts the status providerResolveStatus
// derives from the resulting error.
//
// No network is involved: llm.NewCloudProvider refuses at the gate check,
// which is placed BEFORE any per-type constructor.
func TestProviderResolveStatus_CloudGateClosed_IsNotRetryable(t *testing.T) {
	red := redModeFor(t, "RED_CLOUD_GATE_STATUS")

	cases := []struct {
		name string
		// requested is the providerName argument — the CALLER's own input
		// (request body `provider` field / the CLI's --provider flag).
		requested string
		env       string
		cfg       string
		// wantGateErr: the resolution error must wrap llm.ErrCloudDisabled.
		wantGateErr bool
		// wantServerFault: the error must wrap errServerProviderMisconfigured
		// (asserted GREEN-only — it is the fix's own mechanism).
		wantServerFault bool
		// wantStatus is the GREEN (post-fix) expectation.
		wantStatus int
		// redStatus is the PRE-FIX expectation. Zero means the row is a
		// non-regression row whose behaviour the fix must not change, so it
		// asserts wantStatus in both polarities.
		redStatus int
		// localEndpoint / gatewayEndpoint / llamaCppHost pin the endpoint
		// environment variables of the three LOCAL routes. They are what makes
		// the local-route rows below possible: the CALLER names a local route
		// and supplies no endpoint at all, and the remoteness is introduced
		// entirely by one of these SERVER-SIDE variables.
		localEndpoint   string
		gatewayEndpoint string
		llamaCppHost    string
		// wantNoError marks a POSITIVE-CONTROL row: with the gate closed and a
		// genuinely local endpoint the route must still construct. Without it
		// the new wrapping could be "satisfied" by failing every local route.
		wantNoError bool
		// wantMsgContains lists substrings the error MUST name. For a
		// local-route row this is the offending environment variable — a 500
		// that does not say WHICH setting is wrong leaves the operator no
		// better off than the 403 did.
		wantMsgContains []string
	}{
		{
			name:        "caller_named_hosted_provider_while_gate_closed",
			requested:   "anthropic",
			wantGateErr: true,
			wantStatus:  http.StatusForbidden,
			redStatus:   http.StatusServiceUnavailable,
		},
		{
			name:            "config_named_hosted_provider_while_gate_closed",
			cfg:             "anthropic",
			wantGateErr:     true,
			wantServerFault: true,
			wantStatus:      http.StatusInternalServerError,
			redStatus:       http.StatusServiceUnavailable,
		},
		{
			name:            "env_named_hosted_provider_while_gate_closed",
			env:             "anthropic",
			wantGateErr:     true,
			wantServerFault: true,
			wantStatus:      http.StatusInternalServerError,
			redStatus:       http.StatusServiceUnavailable,
		},
		// LOCAL-ROUTE rows (the defect this row set adds). The caller names a
		// LOCAL route — "local"/"helixllm", "gateway", "llamacpp" — and controls
		// NONE of the inputs that make the refusal happen: the remoteness comes
		// entirely from the deployment's OWN endpoint variable. Answering 403
		// there tells the caller "you are not permitted", which an OpenAI SDK
		// surfaces as PermissionDeniedError and blames the user for a fault only
		// the deployment can fix. That is the SAME misattribution already fixed
		// for llm.default_provider / HELIX_LLM_PROVIDER, recreated on a
		// different key — so it takes the same 500 with the offending key named.
		{
			name:            "local_helixllm_route_pointed_at_remote_endpoint",
			requested:       "local",
			localEndpoint:   "https://api.together.xyz",
			wantGateErr:     true,
			wantServerFault: true,
			wantStatus:      http.StatusInternalServerError,
			redStatus:       http.StatusForbidden,
			wantMsgContains: []string{helixLLMLocalOpenAIEndpointEnv, "api.together.xyz"},
		},
		{
			name:            "gateway_route_pointed_at_remote_endpoint",
			requested:       "gateway",
			gatewayEndpoint: "https://api.together.xyz/v1",
			wantGateErr:     true,
			wantServerFault: true,
			wantStatus:      http.StatusInternalServerError,
			redStatus:       http.StatusForbidden,
			wantMsgContains: []string{helixLLMGatewayEndpointEnv, "api.together.xyz"},
		},
		{
			name:            "llamacpp_route_pointed_at_remote_endpoint",
			requested:       "llamacpp",
			llamaCppHost:    "https://api.together.xyz",
			wantGateErr:     true,
			wantServerFault: true,
			wantStatus:      http.StatusInternalServerError,
			redStatus:       http.StatusForbidden,
			wantMsgContains: []string{llamaCppHostEnv, "api.together.xyz"},
		},
		{
			// llamacpp falls through to the project-wide local-endpoint key
			// when its own key is unset, so the message must name the key that
			// ACTUALLY supplied the value — naming the wrong one would send an
			// operator to edit a variable that is not set.
			name:            "llamacpp_route_inheriting_remote_endpoint_from_shared_key",
			requested:       "llamacpp",
			localEndpoint:   "https://api.together.xyz",
			wantGateErr:     true,
			wantServerFault: true,
			wantStatus:      http.StatusInternalServerError,
			redStatus:       http.StatusForbidden,
			wantMsgContains: []string{helixLLMLocalOpenAIEndpointEnv, "api.together.xyz"},
		},
		{
			// POSITIVE CONTROL. Nothing listens on 127.0.0.1:1, so the
			// constructor's best-effort model probe fails and is only logged;
			// what this row asserts is that the GATE does not refuse and the
			// new wrapping does not turn a working local route into a 500.
			name:          "local_route_with_local_endpoint_still_constructs",
			requested:     "local",
			localEndpoint: "http://127.0.0.1:1",
			wantNoError:   true,
		},

		// Non-regression rows: the pre-existing unknown-name provenance split
		// (400 for the caller, 500 for a server-side source) must survive the
		// new gate branch untouched — including its check ORDER, which is what
		// stops a gate case from re-demoting a server fault.
		{
			name:       "caller_named_unknown_provider_still_400",
			requested:  "definitely-not-a-real-provider",
			wantStatus: http.StatusBadRequest,
		},
		{
			name:            "config_named_unknown_provider_still_500",
			cfg:             "definitely-not-a-real-provider",
			wantServerFault: true,
			wantStatus:      http.StatusInternalServerError,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			withClosedCloudGate(t)
			pinProviderSources(t, tc.env, tc.cfg)
			pinLocalRouteEndpoints(t, tc.localEndpoint, tc.gatewayEndpoint, tc.llamaCppHost)

			provider, err := resolveLLMProvider(tc.requested, "")
			if provider != nil {
				defer func() { _ = provider.Close() }()
			}
			if tc.wantNoError {
				if err != nil {
					t.Fatalf("resolveLLMProvider(%q, \"\") = %v, want no error: a LOCAL "+
						"route pointed at a LOCAL endpoint must construct with the "+
						"cloud gate closed — that is the whole point of the gate "+
						"keying on endpoint locality rather than on provider name",
						tc.requested, err)
				}
				if provider == nil {
					t.Fatalf("resolveLLMProvider(%q, \"\") returned (nil, nil)", tc.requested)
				}
				t.Logf("PASS (positive control): %s constructed with the gate closed", tc.name)
				return
			}
			if err == nil {
				t.Fatalf("resolveLLMProvider(%q, \"\") returned no error with the cloud "+
					"gate CLOSED and provider sources env=%q config=%q — a hosted "+
					"provider must never be constructed while llm.cloud.enabled is false",
					tc.requested, tc.env, tc.cfg)
			}

			if tc.wantGateErr && !errors.Is(err, llm.ErrCloudDisabled) {
				t.Fatalf("error %v does not wrap llm.ErrCloudDisabled — the refusal "+
					"identity every caller branches on was lost in wrapping", err)
			}

			status := providerResolveStatus(err)

			if red && tc.redStatus != 0 {
				if status != tc.redStatus {
					t.Fatalf("RED expectation: on the PRE-FIX artifact this refusal was "+
						"reported as %d — got %d. On the FIXED artifact this "+
						"assertion FAILS, and that failure is the proof the fix is "+
						"present. err=%v", tc.redStatus, status, err)
				}
				t.Logf("RED reproduced: closed-gate refusal reported as %d (retryable) — err=%v",
					status, err)
				return
			}

			if status != tc.wantStatus {
				t.Fatalf("providerResolveStatus = %d, want %d. A closed cloud gate is a "+
					"DETERMINISTIC configuration state: 503 advertises a temporary "+
					"condition and carries Retry-After, so a well-behaved client "+
					"retry-loops forever against something no wait can clear. err=%v",
					status, tc.wantStatus, err)
			}
			if status == http.StatusServiceUnavailable {
				t.Fatalf("providerResolveStatus = 503 for a deterministic refusal — "+
					"err=%v", err)
			}

			if tc.wantServerFault && !errors.Is(err, errServerProviderMisconfigured) {
				t.Fatalf("error %v is not errServerProviderMisconfigured — the caller "+
					"named no provider, so the fault is the deployment's own and must "+
					"carry the sentinel that maps it to 5xx", err)
			}
			if !tc.wantServerFault && errors.Is(err, errServerProviderMisconfigured) {
				t.Fatalf("error %v wraps errServerProviderMisconfigured although the "+
					"CALLER named the provider — that sentinel is checked first and "+
					"would mask the caller-facing status", err)
			}

			// An operator (or a caller reading a 403) needs the key that
			// governs the refusal named in the body, not just a status.
			if tc.wantGateErr && !strings.Contains(err.Error(), cloudEnabledConfigKey) {
				t.Fatalf("error message %q does not name %q — nothing points at the "+
					"key that has to change", err.Error(), cloudEnabledConfigKey)
			}

			for _, want := range tc.wantMsgContains {
				if !strings.Contains(err.Error(), want) {
					t.Fatalf("error message %q does not name %q — a 500 that does not "+
						"say WHICH setting is wrong leaves the operator no better off "+
						"than the misattributed 403 did", err.Error(), want)
				}
			}

			t.Logf("PASS: %s -> status %d, err=%v", tc.name, status, err)
		})
	}
}
