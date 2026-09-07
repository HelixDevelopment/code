package server

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"dev.helix.code/internal/config"
	"dev.helix.code/internal/database"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// §11.4.115 RED-on-broken-artifact + §11.4.135 standing regression guard.
//
// Defect: GET /health panics (nil-ptr deref) when the server is constructed
// with a nil *redis.Client. server.go:374 called `s.redis.IsEnabled()` with no
// `s.redis != nil` guard (unlike the `s.db != nil` guard above it), and
// redis.(*Client).IsEnabled() dereferenced its receiver's .config field without
// a nil-receiver guard, so `s.redis.IsEnabled()` on a nil *Client paniced.
// gin.Recovery() caught the panic and returned HTTP 500 on the most basic
// endpoint.
//
// Polarity switch (RED_MODE, default "0" = standing GREEN regression guard):
//
//	RED_MODE=1 reproduces the defect on the pre-fix artifact and asserts the
//	          nil-redis /health currently PANICS / returns 500 (captures the
//	          defect is genuinely present on the broken build).
//	RED_MODE=0 (default) is the standing GREEN guard asserting the defect is
//	          ABSENT: nil-redis /health returns 200 healthy, no panic.
func newNilRedisServer(t *testing.T) *Server {
	t.Helper()
	cfg := &config.Config{
		Server: config.ServerConfig{
			Address: "localhost",
			Port:    8080,
		},
		Logging: config.LoggingConfig{
			Level: "debug",
		},
	}
	db := (*database.Database)(nil)
	// The reproduced defect: a NIL *redis.Client argument.
	server := newTestServer(t, cfg, db, nil)
	require.NotNil(t, server)
	return server
}

func TestHealthCheck_NilRedis_NoPanic(t *testing.T) {
	server := newNilRedisServer(t)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	server.router.ServeHTTP(w, req)

	if redMode(t) {
		// RED: prove the defect is present on the pre-fix artifact.
		// gin.Recovery() turns the nil-ptr panic into a 500 on /health.
		assert.Equal(t, http.StatusInternalServerError, w.Code,
			"RED_MODE: expected nil-redis /health to 500 on the broken artifact")
		return
	}

	// GREEN standing guard: nil redis must not panic; /health is healthy 200.
	assert.Equal(t, http.StatusOK, w.Code,
		"nil-redis /health must return 200, not panic/500")
	assert.Contains(t, w.Body.String(), "healthy",
		"nil-redis /health body must report healthy status")
}

// Directly invoke the healthCheck handler (bypassing route registration) with a
// nil redis client, so the nil-redis guard is covered even if the /health route
// is ever re-registered or renamed. This exercises the s.redis.IsEnabled() call
// site through a different entry point than the route-dispatched test above, so
// the two are NOT redundant: this one would still catch a regression if the route
// wiring changed, and it asserts the handler itself (not gin.Recovery) is panic-free.
func TestHealthCheck_NilRedis_HandlerDirect(t *testing.T) {
	server := newNilRedisServer(t)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/health", nil)

	if redMode(t) {
		// RED: the pre-fix handler nil-derefs; assert it panics when called directly
		// (no gin.Recovery wrapping at this layer — the panic surfaces).
		assert.Panics(t, func() { server.healthCheck(c) },
			"RED_MODE: pre-fix healthCheck must panic on a nil redis client")
		return
	}

	// GREEN standing guard: the handler itself must not panic on a nil redis
	// client and must write a 200 healthy response.
	require.NotPanics(t, func() { server.healthCheck(c) },
		"healthCheck must not panic on a nil redis client")
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), "healthy")
}
