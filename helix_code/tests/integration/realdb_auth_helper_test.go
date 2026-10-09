//go:build integration

package integration

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"dev.helix.code/internal/auth"
	"dev.helix.code/internal/config"
	"dev.helix.code/internal/database"
	"github.com/google/uuid"
)

// realdb_auth_helper_test.go — shared real-infrastructure auth helper for the
// integration E2E tests that exercise the now-auth-gated paid LLM surfaces
// (POST /api/v1/llm/generate, /api/v1/llm/stream, /api/v1/specify).
//
// WHY THIS EXISTS (§11.4.102 root cause): a prior stream landed
// `authMiddleware()` on the `/api/v1/llm/*` and `/api/v1/specify` route groups
// (internal/server/server.go:308-320) so an UNAUTHENTICATED caller is now
// rejected 401 BEFORE the handler runs — the standing GREEN guard is
// internal/server/llm_auth_guard_test.go. The integration E2E tests in this
// package predate that fix and POSTed with NO Authorization header, so they
// began failing 401. That is the CORRECT new product behaviour; the stale
// tests must authenticate.
//
// ANTI-BLUFF (CONST-050(A) / §11.4): this helper makes NO mock, NO stub, NO
// fake. The auth middleware's VerifyJWTWithDB requires a REAL active user row
// in a REAL database — a nil-DB server (the old test pattern) can NEVER
// authenticate (auth.go:388 returns ErrAuthBackendUnavailable). So the helper
// connects to the REAL PostgreSQL the full-test stack provides, registers a
// REAL user, and mints a REAL JWT the real middleware accepts. This is the
// real-Postgres validation of the IDOR/auth follow-up: the auth fix is proven
// to hold against a live database, not a fixture.
//
// HONEST SKIP (§11.4.3): if no real database is reachable (DB_* / HELIX_DATABASE_*
// env unset or the server down), the dependent E2E test SKIPs with a documented
// reason — it never bluffs a PASS and never silently bypasses auth.

// realDBConfigFromEnv builds a database.Config from the full-test env vars.
// It honours both the HELIX_DATABASE_* names (set by .env.full-test) and the
// shorter DB_* overrides used when pointing the suite at a remote host (e.g.
// the nezha distribution: DB_HOST=nezha.local). Returns ok=false when no host
// is configured at all.
func realDBConfigFromEnv() (database.Config, bool) {
	host := firstNonEmpty(os.Getenv("DB_HOST"), os.Getenv("HELIX_DATABASE_HOST"))
	if host == "" {
		return database.Config{}, false
	}
	portStr := firstNonEmpty(os.Getenv("DB_PORT"), os.Getenv("HELIX_DATABASE_PORT"), "5432")
	port, err := strconv.Atoi(strings.TrimSpace(portStr))
	if err != nil {
		port = 5432
	}
	return database.Config{
		Host:     host,
		Port:     port,
		User:     firstNonEmpty(os.Getenv("DB_USER"), os.Getenv("HELIX_DATABASE_USER"), "helixcode"),
		// HXC-168: no literal default — an unset/wrong password surfaces as a
		// ping/connect failure below, which SKIPs honestly (§11.4.3) rather
		// than silently authenticating with a (previously leaked) value.
		Password: firstNonEmpty(os.Getenv("DB_PASSWORD"), os.Getenv("HELIX_DATABASE_PASSWORD")),
		DBName:   firstNonEmpty(os.Getenv("DB_NAME"), os.Getenv("HELIX_DATABASE_NAME"), "helixcode_test"),
		SSLMode:  firstNonEmpty(os.Getenv("DB_SSLMODE"), os.Getenv("HELIX_DATABASE_SSL_MODE"), "disable"),
	}, true
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

// realAuthedServer connects to the real database, registers a unique active
// test user, mints a real JWT the real authMiddleware accepts, and returns the
// database handle, the JWT, and the auth service. The test that calls this is
// responsible for booting server.New(cfg, db, nil) with the SAME db so the
// middleware's VerifyJWTWithDB finds the registered user. On any real-infra
// gap it calls t.Skip with a documented reason (never a fake PASS).
//
// authJWTSecret MUST match the secret the booted server uses (see
// realServerConfig) so the token the middleware verifies validates against the
// same HMAC key.
const authJWTSecret = "test-jwt-secret-for-testing-only-minimum-32-characters-long"

func realAuthedServer(t *testing.T) (db *database.Database, bearerToken string) {
	t.Helper()

	dbCfg, ok := realDBConfigFromEnv()
	if !ok {
		t.Skip("SKIP-OK: no real database configured (set DB_HOST/HELIX_DATABASE_HOST); cannot exercise the auth-gated endpoint against real infra") //nolint
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	realDB, err := database.New(dbCfg)
	if err != nil {
		t.Skipf("SKIP-OK: real database at %s:%d unreachable: %v; cannot exercise the auth-gated endpoint", dbCfg.Host, dbCfg.Port, err) //nolint
	}
	// Prove the connection is genuinely live before relying on it (§11.4.5).
	if pingErr := realDB.Pool.Ping(ctx); pingErr != nil {
		realDB.Pool.Close()
		t.Skipf("SKIP-OK: real database at %s:%d did not answer ping: %v", dbCfg.Host, dbCfg.Port, pingErr) //nolint
	}
	t.Cleanup(func() { realDB.Pool.Close() })

	authConfig := auth.AuthConfig{
		JWTSecret:   authJWTSecret,
		TokenExpiry: time.Hour,
		BcryptCost:  4,
	}
	authService := auth.NewAuthService(authConfig, auth.NewAuthDB(realDB.Pool))

	// Register a unique active user so VerifyJWTWithDB finds an active row.
	// Uniqueness avoids ErrUserExists collisions across repeated -count runs.
	suffix := strings.ReplaceAll(uuid.NewString(), "-", "")[:12]
	username := "e2e_auth_" + suffix
	email := fmt.Sprintf("%s@e2e.test", username)
	user, err := authService.Register(ctx, username, email, "e2e-password-123", "E2E Auth User")
	if err != nil {
		t.Skipf("SKIP-OK: could not register a real test user against %s:%d (schema migrated?): %v", dbCfg.Host, dbCfg.Port, err) //nolint
	}

	token, err := authService.GenerateJWT(user)
	if err != nil {
		t.Fatalf("minting a JWT for the registered real user must succeed: %v", err)
	}

	return realDB, token
}

// realServerConfig is minimalServerConfig but with the auth JWT secret wired so
// the booted server's authMiddleware verifies the helper-minted token against
// the same HMAC key the helper signed with.
func realServerConfig(port int) *config.Config {
	cfg := minimalServerConfig(port)
	cfg.Auth.JWTSecret = authJWTSecret
	cfg.Auth.TokenExpiry = 3600
	cfg.Auth.BcryptCost = 4
	// The /api/v1/specify SpecKit debate caps a phase at 180s internally and a
	// real small local model can run right up to that cap. The HTTP server's
	// WriteTimeout MUST exceed the slowest handler or the response is severed
	// mid-write (client sees EOF) even though the work succeeded — a
	// harness-shaped false-FAIL, not a product defect. Give generous headroom
	// over the 180s phase cap; ReadTimeout left short (request bodies are tiny).
	cfg.Server.WriteTimeout = 240
	cfg.Server.IdleTimeout = 240
	return cfg
}
