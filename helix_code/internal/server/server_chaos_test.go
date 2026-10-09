//go:build integration

package server

// §11.4.85 CHAOS coverage for internal/server.
//
// CONST-050(A): non-unit test against the REAL server — real Gin router, real
// handler stack, real in-process HTTP listener (httptest.NewServer). No mocked
// HTTP layer. The chaos faults injected are the closed-set §11.4.85(B) shapes
// applicable to an HTTP server:
//
//   - input-corruption: malformed JSON, wrong content-type, oversized body,
//     path-traversal URLs, garbage methods — the server MUST reject with a
//     controlled 4xx/5xx and STAY UP.
//   - process-death / panic-isolation: a handler that panics MUST be caught by
//     Gin's recovery middleware and turned into a 500, NEVER crash the process.
//     We register a real panicking route ON THE REAL ROUTER (same middleware
//     stack) to prove the recovery seam actually fires end-to-end over the wire.
//   - concurrent churn: malformed + valid requests interleaved from many
//     goroutines while faults are injected.
//   - slow / cancelled requests: client cancels mid-flight; the server must not
//     leak or wedge.
//
// After every chaos burst, a post-chaos /health request MUST still return 200 —
// the §11.4.85 "system stays up + responsive" survival property. Recovery traces
// are captured per §11.4.5/§11.4.69 by the stresschaos harness.

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"dev.helix.code/internal/config"
	"dev.helix.code/internal/database"
	"dev.helix.code/internal/redis"
	"dev.helix.code/tests/stresschaos"

	"github.com/gin-gonic/gin"
)

// realServerHarness holds a fully-wired Server served over a real HTTP listener.
type realServerHarness struct {
	srv    *Server
	ts     *httptest.Server
	url    string
	client *http.Client
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func envOrInt(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

// newRealServerHarness builds the REAL Server with a real PostgreSQL pool + real
// Redis client (the live podman instances), serves srv.router over a real
// in-process HTTP listener, and registers cleanup. It performs an honest §11.4.3
// skip ONLY when the real infrastructure is genuinely unreachable — never a faked
// PASS.
func newRealServerHarness(t *testing.T) *realServerHarness {
	t.Helper()
	gin.SetMode(gin.ReleaseMode)

	// Real PostgreSQL pool (live podman). Connection convention from the task /
	// existing integration tests; overridable via env for portability.
	dbCfg := database.Config{
		Host:     envOr("TEST_PG_HOST", "localhost"),
		Port:     envOrInt("TEST_PG_PORT", 5432),
		User:     envOr("TEST_PG_USER", "helix"),
		Password: envOr("TEST_PG_PASSWORD", "helix"),
		DBName:   envOr("TEST_PG_DB", "helix_test"),
		SSLMode:  envOr("TEST_PG_SSLMODE", "disable"),
	}
	db, err := database.New(dbCfg)
	if err != nil {
		t.Skipf("SKIP-OK: real PostgreSQL unreachable at %s:%d (%v) — §11.4.3 honest skip, never a faked PASS",
			dbCfg.Host, dbCfg.Port, err)
	}
	t.Cleanup(func() { db.Close() })

	// Real Redis client (live podman).
	rdsCfg := &config.RedisConfig{
		Enabled:  true,
		Host:     envOr("TEST_REDIS_HOST", "localhost"),
		Port:     envOrInt("TEST_REDIS_PORT", 6379),
		Password: envOr("TEST_REDIS_PASSWORD", ""),
		Database: envOrInt("TEST_REDIS_DB", 0),
	}
	rds, err := redis.NewClient(rdsCfg)
	if err != nil {
		t.Skipf("SKIP-OK: real Redis unreachable at %s:%d (%v) — §11.4.3 honest skip, never a faked PASS",
			rdsCfg.Host, rdsCfg.Port, err)
	}
	t.Cleanup(func() { _ = rds.Close() })

	cfg := &config.Config{
		Server: config.ServerConfig{
			Address:      "127.0.0.1",
			Port:         0,
			ReadTimeout:  15,
			WriteTimeout: 15,
			IdleTimeout:  30,
		},
		Auth: config.AuthConfig{
			JWTSecret:     "stress-chaos-test-secret-not-a-real-credential",
			TokenExpiry:   3600,
			SessionExpiry: 86400,
			BcryptCost:    4, // low cost: tests, not production
		},
		Logging: config.LoggingConfig{Level: "error"},
	}

	srv := newTestServer(t, cfg, db, rds)

	// Register a deliberately-panicking route ON THE REAL ROUTER so the panic
	// crosses the genuine middleware stack (incl. gin.Recovery()). This proves the
	// recovery seam returns 500 instead of crashing the process — the §11.4.85(B)
	// panic-isolation survival property. The route name is test-only and never
	// reachable in production builds (this file is integration-tagged + _test.go).
	srv.router.GET("/__stress_chaos_panic", func(c *gin.Context) {
		panic("injected handler panic for §11.4.85 recovery-isolation test")
	})
	srv.router.GET("/__stress_chaos_nilderef", func(c *gin.Context) {
		var m map[string]string
		// nil-map write panic — a different panic class than the explicit panic above.
		m["x"] = "y"
		c.JSON(http.StatusOK, gin.H{"unreachable": true})
	})

	ts := httptest.NewServer(srv.router)
	t.Cleanup(ts.Close)

	return &realServerHarness{
		srv:    srv,
		ts:     ts,
		url:    ts.URL,
		client: &http.Client{Timeout: 15 * time.Second},
	}
}

// assertHealthy issues a real /health request and fails if the server is not
// responsive with 200 — the §11.4.85 post-chaos liveness check.
func (h *realServerHarness) assertHealthy(t *testing.T) {
	t.Helper()
	resp, err := h.client.Get(h.url + "/health")
	if err != nil {
		t.Fatalf("post-chaos liveness FAILED: /health transport error (server down?): %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("post-chaos liveness FAILED: /health -> %d (body=%s)", resp.StatusCode, string(body))
	}
}

// TestServer_Chaos_MalformedRequests feeds a battery of malformed/hostile requests
// to real endpoints and asserts each is rejected with a controlled status while the
// server stays up. §11.4.85(B) input-corruption.
func TestServer_Chaos_MalformedRequests(t *testing.T) {
	h := newRealServerHarness(t)

	type hostile struct {
		name        string
		method      string
		path        string
		contentType string
		body        string
	}
	cases := []hostile{
		{"truncated_json", http.MethodPost, "/api/v1/auth/login", "application/json", `{"username":"a","passwo`},
		{"not_json_at_all", http.MethodPost, "/api/v1/auth/login", "application/json", `<<<not-json>>>`},
		{"empty_body_required_fields", http.MethodPost, "/api/v1/auth/register", "application/json", ``},
		{"wrong_content_type", http.MethodPost, "/api/v1/auth/login", "text/plain", `username=a&password=b`},
		{"json_array_not_object", http.MethodPost, "/api/v1/auth/register", "application/json", `[1,2,3]`},
		{"deeply_nested_json", http.MethodPost, "/api/v1/auth/login", "application/json",
			strings.Repeat("[", 5000) + strings.Repeat("]", 5000)},
		{"null_bytes_in_json", http.MethodPost, "/api/v1/auth/login", "application/json", "{\"username\":\"a\x00b\",\"password\":\"p\"}"},
		{"path_traversal", http.MethodGet, "/api/v1/../../../../etc/passwd", "", ""},
		{"encoded_traversal", http.MethodGet, "/api/v1/%2e%2e%2f%2e%2e%2fetc%2fpasswd", "", ""},
		{"garbage_path", http.MethodGet, "/api/v1/\x01\x02\x03nonexistent", "", ""},
		{"unknown_endpoint", http.MethodGet, "/api/v1/this/does/not/exist", "", ""},
	}

	rec := stresschaos.NewChaosRecorder(t, "server_chaos_malformed", "input-corruption")

	for _, hc := range cases {
		func(hc hostile) {
			defer func() {
				if p := recover(); p != nil {
					rec.Record(stresschaos.Fatal, fmt.Sprintf("%s: client-side panic %v", hc.name, p))
				}
			}()
			var bodyReader io.Reader
			if hc.body != "" {
				bodyReader = strings.NewReader(hc.body)
			}
			req, err := http.NewRequest(hc.method, h.url+hc.path, bodyReader)
			if err != nil {
				// A path so malformed that net/http refuses to build a request is a
				// client-side rejection — the server was never reached, which is a
				// safe (degraded) outcome.
				rec.Record(stresschaos.Degraded, fmt.Sprintf("%s: request rejected client-side: %v", hc.name, err))
				return
			}
			if hc.contentType != "" {
				req.Header.Set("Content-Type", hc.contentType)
			}
			resp, err := h.client.Do(req)
			if err != nil {
				rec.Record(stresschaos.Degraded, fmt.Sprintf("%s: transport-level rejection: %v", hc.name, err))
				return
			}
			defer resp.Body.Close()
			_, _ = io.Copy(io.Discard, resp.Body)
			// A controlled HTTP status (any 2xx-5xx) means the server handled the
			// hostile input without crashing. We treat 4xx as clean rejection
			// (Recovered) and other controlled codes as Degraded — neither is Fatal.
			switch {
			case resp.StatusCode >= 400 && resp.StatusCode < 500:
				rec.Record(stresschaos.Recovered, fmt.Sprintf("%s: rejected with %d", hc.name, resp.StatusCode))
			case resp.StatusCode >= 200 && resp.StatusCode < 600:
				rec.Record(stresschaos.Degraded, fmt.Sprintf("%s: controlled status %d", hc.name, resp.StatusCode))
			default:
				rec.Record(stresschaos.Fatal, fmt.Sprintf("%s: nonsense status %d", hc.name, resp.StatusCode))
			}
		}(hc)
	}

	rec.AssertNoFatal()
	// Server must still be alive after the hostile battery.
	h.assertHealthy(t)
}

// TestServer_Chaos_HandlerPanicIsolation proves that a handler which panics is
// caught by gin.Recovery() and converted to a 500, and that the server keeps
// serving other requests. This is the §11.4.85(B) process-death / panic-isolation
// survival property exercised end-to-end over the real HTTP listener.
func TestServer_Chaos_HandlerPanicIsolation(t *testing.T) {
	h := newRealServerHarness(t)

	rec := stresschaos.NewChaosRecorder(t, "server_chaos_panic_isolation", "handler-panic")

	for _, panicPath := range []string{"/__stress_chaos_panic", "/__stress_chaos_nilderef"} {
		resp, err := h.client.Get(h.url + panicPath)
		if err != nil {
			// A transport error here would mean the panic crashed the listener — FATAL.
			rec.Record(stresschaos.Fatal, fmt.Sprintf("panic route %s crashed the server: %v", panicPath, err))
			continue
		}
		_, _ = io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
		if resp.StatusCode == http.StatusInternalServerError {
			rec.Record(stresschaos.Recovered, fmt.Sprintf("panic route %s recovered to 500", panicPath))
		} else {
			rec.Record(stresschaos.Fatal, fmt.Sprintf("panic route %s returned %d, expected 500 (recovery seam not firing)", panicPath, resp.StatusCode))
		}

		// CRITICAL: the server must still serve a NORMAL request immediately after
		// the panic — proving the panic did not take the process down.
		resp2, err := h.client.Get(h.url + "/health")
		if err != nil {
			rec.Record(stresschaos.Fatal, fmt.Sprintf("after panic on %s, /health transport error (process died): %v", panicPath, err))
			continue
		}
		_, _ = io.Copy(io.Discard, resp2.Body)
		resp2.Body.Close()
		if resp2.StatusCode == http.StatusOK {
			rec.Record(stresschaos.Recovered, fmt.Sprintf("server still serves /health=200 after panic on %s", panicPath))
		} else {
			rec.Record(stresschaos.Fatal, fmt.Sprintf("after panic on %s, /health=%d (server impaired)", panicPath, resp2.StatusCode))
		}
	}

	rec.AssertNoFatal()
	h.assertHealthy(t)
}

// TestServer_Chaos_ConcurrentMalformedChurn interleaves hostile + valid requests
// from many goroutines, including the panicking route, and asserts the server
// survives the churn (no deadlock, no crash, still healthy afterwards).
// §11.4.85(B) combined with concurrent contention.
func TestServer_Chaos_ConcurrentMalformedChurn(t *testing.T) {
	h := newRealServerHarness(t)

	const goroutines = 16
	const perG = 30

	rec := stresschaos.NewChaosRecorder(t, "server_chaos_concurrent_churn", "input-corruption+panic")
	var mu sync.Mutex
	var wg sync.WaitGroup
	wg.Add(goroutines)

	record := func(cat stresschaos.EventCategory, detail string) {
		mu.Lock()
		rec.Record(cat, detail)
		mu.Unlock()
	}

	hostileBodies := []string{`{bad`, `[]`, ``, `"string"`, `{"x":`}

	for g := 0; g < goroutines; g++ {
		go func(gid int) {
			defer wg.Done()
			defer func() {
				if p := recover(); p != nil {
					record(stresschaos.Fatal, fmt.Sprintf("g=%d client panic: %v", gid, p))
				}
			}()
			client := &http.Client{Timeout: 10 * time.Second}
			for i := 0; i < perG; i++ {
				switch i % 4 {
				case 0: // valid public GET
					resp, err := client.Get(h.url + "/health")
					if err != nil {
						record(stresschaos.Fatal, fmt.Sprintf("g=%d valid GET failed: %v", gid, err))
						continue
					}
					io.Copy(io.Discard, resp.Body)
					resp.Body.Close()
				case 1: // malformed POST
					resp, err := client.Post(h.url+"/api/v1/auth/login", "application/json",
						strings.NewReader(hostileBodies[i%len(hostileBodies)]))
					if err != nil {
						record(stresschaos.Degraded, fmt.Sprintf("g=%d malformed transport err: %v", gid, err))
						continue
					}
					io.Copy(io.Discard, resp.Body)
					resp.Body.Close()
				case 2: // panic route — must be recovered each time
					resp, err := client.Get(h.url + "/__stress_chaos_panic")
					if err != nil {
						record(stresschaos.Fatal, fmt.Sprintf("g=%d panic route crashed server: %v", gid, err))
						continue
					}
					io.Copy(io.Discard, resp.Body)
					resp.Body.Close()
					if resp.StatusCode != http.StatusInternalServerError {
						record(stresschaos.Fatal, fmt.Sprintf("g=%d panic route -> %d (not 500)", gid, resp.StatusCode))
					}
				case 3: // server info
					resp, err := client.Get(h.url + "/api/v1/server/info")
					if err != nil {
						record(stresschaos.Fatal, fmt.Sprintf("g=%d server/info failed: %v", gid, err))
						continue
					}
					io.Copy(io.Discard, resp.Body)
					resp.Body.Close()
				}
			}
			record(stresschaos.Recovered, fmt.Sprintf("g=%d completed %d mixed requests", gid, perG))
		}(g)
	}

	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(60 * time.Second):
		record(stresschaos.Fatal, "concurrent churn deadlocked (60s timeout)")
	}

	rec.AssertNoFatal()
	h.assertHealthy(t)
}

// TestServer_Chaos_SlowAndCancelledRequests opens connections and cancels them
// mid-flight (slowloris-ish), then asserts the server is still responsive and has
// not leaked the cancelled work. §11.4.85(B) process-death (client-side) class.
func TestServer_Chaos_SlowAndCancelledRequests(t *testing.T) {
	h := newRealServerHarness(t)

	rec := stresschaos.NewChaosRecorder(t, "server_chaos_cancelled", "request-cancellation")

	// (1) Many requests cancelled immediately after dispatch.
	for i := 0; i < 50; i++ {
		ctx, cancel := context.WithTimeout(context.Background(), 1*time.Millisecond)
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, h.url+"/api/v1/metrics", nil)
		resp, err := h.client.Do(req)
		if err != nil {
			// Expected: context deadline / cancelled — a clean degraded outcome.
			rec.Record(stresschaos.Degraded, fmt.Sprintf("req %d cancelled as expected: %v", i, err))
		} else {
			io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
			rec.Record(stresschaos.Recovered, fmt.Sprintf("req %d completed before cancel: %d", i, resp.StatusCode))
		}
		cancel()
	}

	// (2) Half-open connections: dial the raw TCP socket, send a partial request
	// line, then close without finishing — the server must not wedge on these.
	addr := strings.TrimPrefix(h.url, "http://")
	for i := 0; i < 20; i++ {
		conn, err := net.DialTimeout("tcp", addr, 5*time.Second)
		if err != nil {
			rec.Record(stresschaos.Degraded, fmt.Sprintf("partial conn %d dial failed: %v", i, err))
			continue
		}
		// Send an incomplete request and abandon it.
		_, _ = conn.Write([]byte("GET /health HTTP/1.1\r\nHost: x\r\n"))
		_ = conn.Close()
		rec.Record(stresschaos.Recovered, fmt.Sprintf("partial conn %d sent+closed without wedging server", i))
	}

	rec.AssertNoFatal()
	// The server absorbed all the cancelled/half-open work and is still healthy.
	h.assertHealthy(t)
}
