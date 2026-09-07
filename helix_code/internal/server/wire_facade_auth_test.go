package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"dev.helix.code/internal/config"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// wire_facade_auth_test.go — §11.4.115 RED-baseline + standing GREEN guard for
// the unauthenticated wire-facade endpoints security finding.
//
// THE BUG (RED, captured on the pre-fix artifact, commit 51c058b1): POST
// /v1/chat/completions and POST /v1/messages (wire_facade.go, added in
// 51c058b1) were registered on the bare router with NO auth middleware of any
// kind, while every shipped config profile (including
// config/production-config.yaml) binds server.address to "0.0.0.0" — an
// unauthenticated caller on any reachable interface could drive real, paid
// LLM-provider generation (CONST-035/BLUFF-001). This was flagged by an
// independent security review AND the dual-wire (OpenAI+Anthropic facade)
// review as a release/production blocker.
//
// THE GUARD (GREEN, post-fix): both routes are gated by
// server.go's s.wireFacadeAuthMiddleware(), which is DISTINCT from the
// internal-user JWT authMiddleware() (see wire_facade.go's file-level
// doc-comment: genuine OpenAI clients send `Authorization: Bearer sk-...` and
// genuine Anthropic clients send `x-api-key: ...`, neither of which is this
// server's session JWT). A request with NO key (and/or no key configured at
// all — the middleware fails CLOSED) MUST be rejected 401 before either
// handler runs; a request carrying a key that matches the operator-configured
// cfg.Auth.WireFacadeAPIKeys list MUST pass the middleware.
//
// Polarity switch RED_WIRE_FACADE_AUTH: default ("0") = standing GREEN guard;
// "1" = reproduce-and-assert-the-defect (this assertion mode only reproduces
// the historical defect against the ACTUAL pre-fix source — i.e. it was
// exercised once, by hand, against server.go before
// s.wireFacadeAuthMiddleware() was wired into the two route registrations,
// to capture the RED evidence cited in the fix commit; run against the
// CURRENT, fixed HEAD it necessarily fails, which is expected and documents
// the fix took effect — see §11.4.115).
//
// Anti-bluff: real gin router + the real wireFacadeAuthMiddleware, exercised
// end-to-end through srv.router.ServeHTTP — not a unit call of the middleware
// function in isolation. Mirrors llm_auth_guard_test.go's established
// pattern in this same package (NotEqual(401) as the RED-reproduction
// assertion, Equal(401) as the GREEN-guard assertion) rather than asserting a
// literal 200, since a literal end-to-end 200 depends on a live, reachable
// LLM provider backend that is not guaranteed present in this test
// environment; what both the security finding and this guard care about is
// whether an unauthenticated request is gated BEFORE it can reach — and
// potentially bill — a real provider, which NotEqual/Equal(401) proves
// precisely.

func wireFacadeAuthRedMode(t *testing.T) bool {
	t.Helper()
	return redModeFor(t, "RED_WIRE_FACADE_AUTH")
}

const wireFacadeTestAPIKey = "test-only-wire-facade-key-not-a-real-secret"

// wireFacadeAuthFixture builds a real Server with the wire-facade routes
// registered via the actual setupRoutes(), and an operator-configured
// WireFacadeAPIKeys list so the "valid key passes" half of the guard is
// meaningful.
func wireFacadeAuthFixture(t *testing.T) *Server {
	t.Helper()
	gin.SetMode(gin.TestMode)

	cfg := &config.Config{
		Auth: config.AuthConfig{
			JWTSecret:         "test-secret-key-for-testing-only",
			TokenExpiry:       3600,
			BcryptCost:        4,
			WireFacadeAPIKeys: wireFacadeTestAPIKey,
		},
		Logging: config.LoggingConfig{Level: "error"},
	}

	srv := &Server{
		config: cfg,
		router: gin.New(),
	}
	srv.setupRoutes()
	return srv
}

// wireFacadeEndpoints are the two wire-standard, provider-consuming surfaces
// that MUST require an API key.
var wireFacadeEndpoints = []struct {
	name string
	path string
	body string
}{
	{"chat_completions", "/v1/chat/completions", `{"model":"llama3.2","messages":[{"role":"user","content":"hi"}]}`},
	{"anthropic_messages", "/v1/messages", `{"model":"llama3.2","max_tokens":16,"messages":[{"role":"user","content":"hi"}]}`},
}

func TestWireFacadeEndpoints_NoKeyRejected(t *testing.T) {
	red := wireFacadeAuthRedMode(t)
	srv := wireFacadeAuthFixture(t)

	for _, ep := range wireFacadeEndpoints {
		t.Run(ep.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			req, _ := http.NewRequest("POST", ep.path, bytes.NewBufferString(ep.body))
			req.Header.Set("Content-Type", "application/json")
			// NO Authorization / x-api-key header — the whole point.
			srv.router.ServeHTTP(w, req)

			if red {
				// PRE-FIX artifact: the bug — no auth gate of any kind, so an
				// unauthenticated request is NOT rejected 401 (it falls through
				// to the handler, which would attempt a real, paid provider
				// call).
				require.NotEqualf(t, http.StatusUnauthorized, w.Code,
					"RED expects the unauth bug PRESENT on %s: no-key request must NOT be 401 (got %d, body=%s)",
					ep.path, w.Code, w.Body.String())
				t.Logf("RED captured: %s served WITHOUT any API key, status=%d body=%s", ep.path, w.Code, w.Body.String())
			} else {
				// POST-FIX guard: wireFacadeAuthMiddleware rejects the no-key
				// request 401 BEFORE either handler runs.
				require.Equalf(t, http.StatusUnauthorized, w.Code,
					"GREEN: %s must reject a no-key request 401 (got %d, body=%s)",
					ep.path, w.Code, w.Body.String())
				var body map[string]interface{}
				require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
				errObj, ok := body["error"].(map[string]interface{})
				require.True(t, ok, "expected an {error:{...}} body, got %s", w.Body.String())
				require.Equal(t, "authentication_error", errObj["type"])
			}
		})
	}
}

// TestWireFacadeEndpoints_NoKeyConfiguredStillRejected proves the fail-closed
// half of the design: even if a caller supplies SOME bearer token, an empty
// cfg.Auth.WireFacadeAPIKeys (the zero-value default in every shipped config)
// means every request is rejected — there is no accidental "empty config
// means open access" fallback, unlike the DZ-05/APIKeyAuth precedent this
// middleware's pattern is otherwise modeled on.
func TestWireFacadeEndpoints_NoKeyConfiguredStillRejected(t *testing.T) {
	if wireFacadeAuthRedMode(t) {
		t.Skip("SKIP-OK: fail-closed-by-default guard only meaningful post-fix (GREEN mode)")
	}
	gin.SetMode(gin.TestMode)
	cfg := &config.Config{
		Auth:    config.AuthConfig{JWTSecret: "test-secret-key-for-testing-only", TokenExpiry: 3600, BcryptCost: 4},
		Logging: config.LoggingConfig{Level: "error"},
	}
	srv := &Server{config: cfg, router: gin.New()}
	srv.setupRoutes()

	for _, ep := range wireFacadeEndpoints {
		t.Run(ep.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			req, _ := http.NewRequest("POST", ep.path, bytes.NewBufferString(ep.body))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Authorization", "Bearer some-caller-supplied-token")
			srv.router.ServeHTTP(w, req)

			require.Equalf(t, http.StatusUnauthorized, w.Code,
				"GREEN: %s must reject EVERY request when no key is configured, even with a bearer token (got %d, body=%s)",
				ep.path, w.Code, w.Body.String())
		})
	}
}

// TestWireFacadeEndpoints_ValidKeyPassesMiddleware proves the GREEN fix
// doesn't over-reach: a request carrying a key matching the configured
// WireFacadeAPIKeys list passes wireFacadeAuthMiddleware and reaches the
// handler (the handler may then fail for its own reasons — no reachable LLM
// provider in this test environment — but crucially NOT with a 401). GREEN
// mode only, mirroring TestLLMCostEndpoints_ValidTokenPassesMiddleware's
// established pattern in this package.
func TestWireFacadeEndpoints_ValidKeyPassesMiddleware(t *testing.T) {
	if wireFacadeAuthRedMode(t) {
		t.Skip("SKIP-OK: positive-half guard only meaningful post-fix (GREEN mode)")
	}
	srv := wireFacadeAuthFixture(t)

	for _, ep := range wireFacadeEndpoints {
		t.Run(ep.name+"_bearer", func(t *testing.T) {
			w := httptest.NewRecorder()
			req, _ := http.NewRequest("POST", ep.path, bytes.NewBufferString(ep.body))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Authorization", "Bearer "+wireFacadeTestAPIKey)
			srv.router.ServeHTTP(w, req)

			require.NotEqualf(t, http.StatusUnauthorized, w.Code,
				"GREEN: valid-Bearer-key request to %s must pass the middleware (got 401, body=%s)",
				ep.path, w.Body.String())
		})

		t.Run(ep.name+"_x_api_key", func(t *testing.T) {
			w := httptest.NewRecorder()
			req, _ := http.NewRequest("POST", ep.path, bytes.NewBufferString(ep.body))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("x-api-key", wireFacadeTestAPIKey)
			srv.router.ServeHTTP(w, req)

			require.NotEqualf(t, http.StatusUnauthorized, w.Code,
				"GREEN: valid-x-api-key request to %s must pass the middleware (got 401, body=%s)",
				ep.path, w.Body.String())
		})
	}
}

// --- CONST-042: a shipped placeholder must never authenticate ---------------
//
// THE DEFECT (found by independent review before it was ever pushed):
// `.env.example` shipped `HELIX_WIRE_FACADE_API_KEYS=CHANGE_ME_wire_facade_key`
// as a live value. setup.sh copies .env.example -> .env verbatim and regenerates
// only HELIX_DATABASE_PASSWORD / HELIX_REDIS_PASSWORD / HELIX_AUTH_JWT_SECRET,
// so that literal survived into every fresh install. The middleware compared it
// verbatim, and server.address ships as 0.0.0.0 — so a PUBLISHED credential
// authenticated routes that drive real LLM calls on a stock deployment. The
// file's own comment claimed "an unconfigured deployment cannot be driven by
// anyone who can reach the port", which the shipped value made false.
//
// Fixed in three independent layers: .env.example ships the key EMPTY (empty ==
// no key configured == fail closed), setup.sh generates a unique per-install
// secret into that empty slot, and the middleware refuses any CHANGE_ME-prefixed
// value outright. This guard covers the third — the one that still holds for a
// hand-assembled .env that never runs setup.sh.
//
// FALSIFYING MUTATION (§1.1): delete the `strings.HasPrefix(configured,
// "CHANGE_ME")` guard in wireFacadeAuthMiddleware. The placeholder then matches
// verbatim, the request is served, and this test FAILS.
func TestWireFacadeEndpoints_PlaceholderKeyNeverAuthenticates(t *testing.T) {
	gin.SetMode(gin.TestMode)

	// The exact literal that shipped in .env.example, as an operator with an
	// older or hand-copied .env would still have it configured.
	const shippedPlaceholder = "CHANGE_ME_wire_facade_key"

	cfg := &config.Config{
		Auth: config.AuthConfig{
			JWTSecret:         "test-secret-key-for-testing-only",
			TokenExpiry:       3600,
			BcryptCost:        4,
			WireFacadeAPIKeys: shippedPlaceholder,
		},
		Logging: config.LoggingConfig{Level: "error"},
	}
	srv := &Server{config: cfg, router: gin.New()}
	srv.setupRoutes()

	for _, ep := range wireFacadeEndpoints {
		for _, hdr := range []struct{ name, key, val string }{
			{"authorization_bearer", "Authorization", "Bearer " + shippedPlaceholder},
			{"x_api_key", "x-api-key", shippedPlaceholder},
		} {
			t.Run(ep.name+"/"+hdr.name, func(t *testing.T) {
				w := httptest.NewRecorder()
				req, _ := http.NewRequest("POST", ep.path, bytes.NewBufferString(ep.body))
				req.Header.Set("Content-Type", "application/json")
				req.Header.Set(hdr.key, hdr.val)
				srv.router.ServeHTTP(w, req)

				require.Equalf(t, http.StatusUnauthorized, w.Code,
					"the shipped placeholder %q MUST NOT authenticate %s via %s — a published "+
						"credential driving real LLM calls (got %d, body=%s)",
					shippedPlaceholder, ep.path, hdr.key, w.Code, w.Body.String())
			})
		}
	}
}

// A real, operator-configured key must still work — otherwise the guard above
// could "pass" by rejecting everything, which would be a different bug.
func TestWireFacadeEndpoints_RealKeyStillAuthenticates(t *testing.T) {
	srv := wireFacadeAuthFixture(t)

	for _, ep := range wireFacadeEndpoints {
		t.Run(ep.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			req, _ := http.NewRequest("POST", ep.path, bytes.NewBufferString(ep.body))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Authorization", "Bearer "+wireFacadeTestAPIKey)
			srv.router.ServeHTTP(w, req)

			// NotEqual(401) rather than Equal(200): passing the auth gate is the
			// property under test; what happens downstream depends on a live
			// provider backend this test does not require.
			require.NotEqualf(t, http.StatusUnauthorized, w.Code,
				"a genuine configured key must pass the auth gate on %s (got 401, body=%s)",
				ep.path, w.Body.String())
		})
	}
}
