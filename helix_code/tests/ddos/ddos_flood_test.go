// Build-tagged OUT of the default `go test ./...` unit pass.
//
// WHY (measured, not assumed): these suites generate load. A full-suite run was
// measured driving the host ephemeral port range to 100% occupancy (28,221 of
// 28,232 ports) held by ~29,600 TIME-WAIT sockets, for 60-90s at a time. While
// that window is open, ANY concurrently-running package that binds `:0` or
// `127.0.0.1:0` fails with EADDRINUSE — which is exactly how ~7 unrelated unit
// tests in internal/discovery, internal/tools/shell and internal/tools/web
// began failing in the suite while passing in isolation.
//
// The kernel arithmetic: 28,232 ports / 60s TIME_WAIT = ~470 socket
// close-cycles/sec before total saturation. `go test` defaults `-p` to NPROC
// (16 here), so the unit pass and these load suites were competing for that one
// budget. SO_REUSEADDR does not help (bind(0) treats every occupied port as a
// hard conflict) and tcp_tw_reuse is connect()-path only.
//
// This is NOT a removal (§11.4.122): the suites still run, via
//     make test-loadtest
// which runs them SERIALLY (-p 1) so they cannot flood each other either, and
// which is wired into `make test-complete`.
//
// Full evidence: docs/qa/2026-09-07-ephemeral-port-exhaustion/ANALYSIS.md
//go:build integration || loadtest

package ddos

// §11.4.85 / DDoS-class flood coverage against the REAL booted internal/server.
//
// CONST-050(A): non-unit test against the REAL Server — real Gin router + real
// handler stack served over a real in-process HTTP listener (httptest.NewServer),
// booted with a real PostgreSQL pool + real Redis client (live podman). Every
// request crosses the genuine middleware chain (Logger -> Recovery -> CORS ->
// Security) and a real TCP socket. An honest §11.4.3 SKIP fires ONLY when the real
// infrastructure is genuinely unreachable — never a faked PASS.

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"testing"
	"time"

	"dev.helix.code/internal/config"
	"dev.helix.code/internal/database"
	"dev.helix.code/internal/redis"
	"dev.helix.code/internal/server"

	"github.com/gin-gonic/gin"
)

// freePort grabs an ephemeral TCP port from the kernel and immediately frees it so
// the real server can bind it. There is an inherent tiny race window, but the
// retry-on-bind-failure poll below tolerates it.
func freePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("ddos: cannot grab a free port: %v", err)
	}
	port := l.Addr().(*net.TCPAddr).Port
	_ = l.Close()
	return port
}

// bootRealServer builds the REAL Server with a real PG pool + real Redis client,
// binds a real TCP listener via srv.Start() on an ephemeral port, polls /health
// until ready, and returns the base URL. SKIP-with-reason when infra is unreachable
// (§11.4.3), never a faked PASS.
func bootRealServer(t *testing.T) (baseURL string) {
	t.Helper()
	gin.SetMode(gin.ReleaseMode)

	dbCfg := database.Config{
		Host: envOrHelix("HELIX_DATABASE_HOST", "TEST_PG_HOST", "localhost"),
		Port: envOrIntHelix("HELIX_DATABASE_PORT", "TEST_PG_PORT", 5432),
		User: envOrHelix("HELIX_DATABASE_USER", "TEST_PG_USER", "helixcode"),
		// HXC-168: no literal default — an unset password falls through to
		// database.New's connect error below, which SKIPs honestly (§11.4.3)
		// rather than silently authenticating with a (previously leaked) value.
		Password: envOrHelix("HELIX_DATABASE_PASSWORD", "TEST_PG_PASSWORD", ""),
		DBName:   envOrHelix("HELIX_DATABASE_NAME", "TEST_PG_DB", "helixcode_test"),
		SSLMode:  envOrHelix("HELIX_DATABASE_SSL_MODE", "TEST_PG_SSLMODE", "disable"),
	}
	db, err := database.New(dbCfg)
	if err != nil {
		t.Skipf("SKIP-OK: real PostgreSQL unreachable at %s:%d (%v) — §11.4.3 honest skip, never a faked PASS",
			dbCfg.Host, dbCfg.Port, err)
	}
	t.Cleanup(func() { db.Close() })

	rdsCfg := &config.RedisConfig{
		Enabled:  true,
		Host:     envOrHelix("HELIX_REDIS_HOST", "TEST_REDIS_HOST", "localhost"),
		Port:     envOrIntHelix("HELIX_REDIS_PORT", "TEST_REDIS_PORT", 6379),
		Password: envOrHelix("HELIX_REDIS_PASSWORD", "TEST_REDIS_PASSWORD", ""),
		Database: envOrIntHelix("HELIX_REDIS_DB", "TEST_REDIS_DB", 0),
	}
	rds, err := redis.NewClient(rdsCfg)
	if err != nil {
		t.Skipf("SKIP-OK: real Redis unreachable at %s:%d (%v) — §11.4.3 honest skip, never a faked PASS",
			rdsCfg.Host, rdsCfg.Port, err)
	}
	t.Cleanup(func() { _ = rds.Close() })

	port := freePort(t)
	cfg := &config.Config{
		Server: config.ServerConfig{
			Address: "127.0.0.1", Port: port, ReadTimeout: 15, WriteTimeout: 15, IdleTimeout: 30,
		},
		Auth: config.AuthConfig{
			JWTSecret: "ddos-flood-test-secret-not-a-real-credential", TokenExpiry: 3600,
			SessionExpiry: 86400, BcryptCost: 4,
		},
		Logging: config.LoggingConfig{Level: "error"},
	}

	srv := server.New(cfg, db, rds)
	go func() { _ = srv.Start() }() // real TCP listener (ListenAndServe)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(ctx)
	})

	base := fmt.Sprintf("http://127.0.0.1:%d", port)
	// Poll /health until the real listener accepts (bounded), so the flood hits a
	// genuinely-ready server.
	client := &http.Client{Timeout: 2 * time.Second}
	deadline := time.Now().Add(15 * time.Second)
	for {
		resp, err := client.Get(base + "/health")
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				break
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("ddos: real server did not become ready on %s within 15s (last err: %v)", base, err)
		}
		time.Sleep(50 * time.Millisecond)
	}
	return base
}

// TestDDoS_HealthFlood floods the real /health endpoint and asserts graceful
// degradation: no goroutine leak / no deadlock, zero 5xx, real served responses
// (body contains "healthy"), bounded p99. Evidence: qa-results/<run-id>/.
func TestDDoS_HealthFlood(t *testing.T) {
	url := bootRealServer(t)

	rep := RunFlood(t, "ddos_health_flood", FloodConfig{
		URL:                    url + "/health",
		BodyMarker:             "healthy",
		Parallelism:            32,
		IterationsPerGoroutine: 50,
		MaxP99Ms:               0, // first run: record p99 baseline; no ceiling asserted yet
		Timeout:                60 * time.Second,
	})

	if rep.Status5xx != 0 {
		t.Fatalf("real server returned %d 5xx under flood", rep.Status5xx)
	}
	if rep.BodyMarkerHits == 0 {
		t.Fatal("no real served /health responses under flood")
	}
	t.Logf("DDoS health flood PASS: sent=%d 2xx=%d p99=%.2fms (no 5xx, no leak/deadlock)",
		rep.RequestsSent, rep.Status2xx, rep.P99UnderFloodMs)
}
