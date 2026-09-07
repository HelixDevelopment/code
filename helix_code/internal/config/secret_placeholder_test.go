package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"dev.helix.code/internal/database"
)

// secret_placeholder_test.go — HXC-SECRET-PLACEHOLDER.
//
// The defect this file pins down: a server started without
// HELIX_AUTH_JWT_SECRET signed JWTs with the literal 24-byte string
// "${HELIX_AUTH_JWT_SECRET}", which is committed in config/config.yaml.
// Anyone who could read the repository could forge a token.
//
// Why it slipped through every existing gate:
//   - expandShellDefaults expands exactly five NON-secret fields
//     (Redis.Host, Database.Host/User/DBName, Server.Address). Auth is not
//     among them, so the placeholder survived as a literal.
//   - validateConfig rejected only "" and "default-secret-change-in-production".
//     A 24-character literal is neither, so it PASSED.
//   - the len(JWTSecret) >= 32 checks that WOULD have caught it live in
//     ConfigurationValidator.Validate / ValidateField, which Load() never calls.
//
// Every test here builds a config that is valid in EVERY respect except the
// placeholder, so a pass can never come from an unrelated missing field.

// assertBaselineValid fails the test unless cfg validates cleanly. Every case
// below calls this BEFORE mutating, so an error observed after the mutation is
// attributable to the mutation and to nothing else.
func assertBaselineValid(t *testing.T, cfg *Config) {
	t.Helper()
	if err := validateConfig(cfg); err != nil {
		t.Fatalf("baseline config must be valid before the placeholder is injected, got: %v", err)
	}
}

// validSecretsConfig extends minimallyValidConfig with every optional block
// that carries a secret-bearing field, so each field can be exercised on a
// config that is otherwise completely valid.
func validSecretsConfig() *Config {
	cfg := minimallyValidConfig()
	cfg.Auth.JWTSecret = "a-real-jwt-signing-secret-32-chars+"
	cfg.Auth.WireFacadeAPIKeys = "sk-local-not-a-real-key"
	cfg.Database.Password = "a-real-database-password"
	cfg.Redis = RedisConfig{Enabled: true, Host: "localhost", Port: 6379, Password: "a-real-redis-password"}
	cfg.QA.LLMAPIKey = "a-real-qa-llm-key"
	cfg.Providers = ProvidersConfig{
		Mem0:    Mem0Config{APIKey: "a-real-mem0-key"},
		Zep:     ZepConfig{APIKey: "a-real-zep-key"},
		Memonto: MemontoConfig{APIKey: "a-real-memonto-key"},
		BaseAI:  BaseAIConfig{APIKey: "a-real-baseai-key"},
	}
	cfg.Verifier = &VerifierConfig{
		Enabled:         true,
		Mode:            "remote",
		Endpoint:        "http://localhost:9090",
		APIKey:          "a-real-verifier-key",
		PollingInterval: 30 * 1e9,
		CacheTTL:        60 * 1e9,
		Scoring: VerifierScoringConfig{Weights: ScoringWeights{
			CodeCapability: 0.40, Responsiveness: 0.20, Reliability: 0.20,
			FeatureRichness: 0.15, ValueProposition: 0.05,
		}},
		Providers: map[string]VerifierProviderConfig{
			"openai": {Enabled: true, APIKey: "a-real-openai-key"},
		},
	}
	return cfg
}

// TestValidateConfig_UnexpandedSecretPlaceholder_IsRefused is the RED test.
// On the pre-fix code every case FAILS (validateConfig returns nil for a
// placeholder-valued credential); post-fix every case is refused with an error
// naming BOTH the config field and the environment variable to export.
func TestValidateConfig_UnexpandedSecretPlaceholder_IsRefused(t *testing.T) {
	cases := []struct {
		name        string
		field       string // dotted config path expected in the error
		envVar      string // env var name expected in the error
		placeholder string
		inject      func(*Config, string)
	}{
		{
			// The reported defect, verbatim from config/config.yaml:28.
			name:        "auth.jwt_secret",
			field:       "auth.jwt_secret",
			envVar:      "HELIX_AUTH_JWT_SECRET",
			placeholder: "${HELIX_AUTH_JWT_SECRET}",
			inject:      func(c *Config, v string) { c.Auth.JWTSecret = v },
		},
		{
			name:        "auth.wire_facade_api_keys",
			field:       "auth.wire_facade_api_keys",
			envVar:      "HELIX_WIRE_FACADE_API_KEYS",
			placeholder: "${HELIX_WIRE_FACADE_API_KEYS}",
			inject:      func(c *Config, v string) { c.Auth.WireFacadeAPIKeys = v },
		},
		{
			name:        "database.password",
			field:       "database.password",
			envVar:      "HELIX_DATABASE_PASSWORD",
			placeholder: "${HELIX_DATABASE_PASSWORD}",
			inject:      func(c *Config, v string) { c.Database.Password = v },
		},
		{
			name:        "redis.password",
			field:       "redis.password",
			envVar:      "HELIX_REDIS_PASSWORD",
			placeholder: "${HELIX_REDIS_PASSWORD}",
			inject:      func(c *Config, v string) { c.Redis.Password = v },
		},
		{
			name:        "verifier.api_key",
			field:       "verifier.api_key",
			envVar:      "HELIX_VERIFIER_API_KEY",
			placeholder: "${HELIX_VERIFIER_API_KEY}",
			inject:      func(c *Config, v string) { c.Verifier.APIKey = v },
		},
		{
			name:        "verifier.providers.openai.api_key",
			field:       "verifier.providers.openai.api_key",
			envVar:      "OPENAI_API_KEY",
			placeholder: "${OPENAI_API_KEY}",
			inject: func(c *Config, v string) {
				c.Verifier.Providers["openai"] = VerifierProviderConfig{Enabled: true, APIKey: v}
			},
		},
		{
			name:        "qa.llm_api_key",
			field:       "qa.llm_api_key",
			envVar:      "HELIX_QA_LLM_API_KEY",
			placeholder: "${HELIX_QA_LLM_API_KEY}",
			inject:      func(c *Config, v string) { c.QA.LLMAPIKey = v },
		},
		{
			name:        "providers.mem0.api_key",
			field:       "providers.mem0.api_key",
			envVar:      "MEM0_API_KEY",
			placeholder: "${MEM0_API_KEY}",
			inject:      func(c *Config, v string) { c.Providers.Mem0.APIKey = v },
		},
		{
			name:        "providers.zep.api_key",
			field:       "providers.zep.api_key",
			envVar:      "ZEP_API_KEY",
			placeholder: "${ZEP_API_KEY}",
			inject:      func(c *Config, v string) { c.Providers.Zep.APIKey = v },
		},
		{
			name:        "providers.memonto.api_key",
			field:       "providers.memonto.api_key",
			envVar:      "MEMONTO_API_KEY",
			placeholder: "${MEMONTO_API_KEY}",
			inject:      func(c *Config, v string) { c.Providers.Memonto.APIKey = v },
		},
		{
			name:        "providers.baseai.api_key",
			field:       "providers.baseai.api_key",
			envVar:      "BASEAI_API_KEY",
			placeholder: "${BASEAI_API_KEY}",
			inject:      func(c *Config, v string) { c.Providers.BaseAI.APIKey = v },
		},
		{
			// ${VAR:default} — the other shape expandShellDefaults understands.
			// The default token must not smuggle the placeholder past the check.
			name:        "default_value_form_is_still_a_placeholder",
			field:       "auth.jwt_secret",
			envVar:      "HELIX_AUTH_JWT_SECRET",
			placeholder: "${HELIX_AUTH_JWT_SECRET:change-me}",
			inject:      func(c *Config, v string) { c.Auth.JWTSecret = v },
		},
		{
			// ${VAR:-default} — bash's other default form.
			name:        "dash_default_form_is_still_a_placeholder",
			field:       "auth.jwt_secret",
			envVar:      "HELIX_AUTH_JWT_SECRET",
			placeholder: "${HELIX_AUTH_JWT_SECRET:-change-me}",
			inject:      func(c *Config, v string) { c.Auth.JWTSecret = v },
		},
		{
			// A placeholder embedded in a longer value is still unexpanded.
			name:        "embedded_placeholder",
			field:       "database.password",
			envVar:      "HELIX_DATABASE_PASSWORD",
			placeholder: "prefix-${HELIX_DATABASE_PASSWORD}-suffix",
			inject:      func(c *Config, v string) { c.Database.Password = v },
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := validSecretsConfig()
			assertBaselineValid(t, cfg)

			tc.inject(cfg, tc.placeholder)
			err := validateConfig(cfg)
			if err == nil {
				t.Fatalf("validateConfig accepted %s = %q; an unexpanded placeholder "+
					"is used verbatim as the credential and must be refused",
					tc.field, tc.placeholder)
			}
			msg := err.Error()
			if !strings.Contains(msg, tc.field) {
				t.Errorf("error must name the offending field %q so the operator knows what to fix; got: %s", tc.field, msg)
			}
			if !strings.Contains(msg, tc.envVar) {
				t.Errorf("error must name the environment variable %q to export; got: %s", tc.envVar, msg)
			}
		})
	}
}

// TestValidateConfig_NonSecretFieldsAreNotScanned pins the SCOPE of the check.
// Placeholders survive today in plenty of non-credential string fields; that is
// a separate (non-security) defect and this change must not start refusing them,
// or it would break configs that load fine now.
func TestValidateConfig_NonSecretFieldsAreNotScanned(t *testing.T) {
	cases := []struct {
		name   string
		inject func(*Config)
	}{
		{"verifier.endpoint", func(c *Config) { c.Verifier.Endpoint = "${HELIX_VERIFIER_ENDPOINT}" }},
		{"logging.output", func(c *Config) { c.Logging.Output = "${HELIX_LOG_DIR}/helix.log" }},
		{"qa.banks_dir", func(c *Config) { c.QA.BanksDir = "${HELIX_QA_HOME}/banks" }},
		{"application.name", func(c *Config) { c.Application.Name = "helix-${ENVIRONMENT}" }},
		{"llm.default_model", func(c *Config) { c.LLM.DefaultModel = "${MODEL_NAME}" }},
		{"verifier.providers.openai.base_url", func(c *Config) {
			c.Verifier.Providers["openai"] = VerifierProviderConfig{Enabled: true, BaseURL: "${OPENAI_BASE_URL}", APIKey: "real"}
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := validSecretsConfig()
			assertBaselineValid(t, cfg)

			tc.inject(cfg)
			if err := validateConfig(cfg); err != nil {
				t.Fatalf("%s is not a credential; a ${...} value there must not be "+
					"refused (it loads today), got: %v", tc.name, err)
			}
		})
	}
}

// TestValidateConfig_RealSecretsStillAccepted is the negative control: the
// check must not reject ordinary credentials, including ones containing a
// lone '$' or '{'.
func TestValidateConfig_RealSecretsStillAccepted(t *testing.T) {
	for _, secret := range []string{
		"a-real-jwt-signing-secret-32-chars+",
		"pa$$word-with-dollars",
		"brace{not}placeholder",
		"$HELIX_AUTH_JWT_SECRET", // bare $VAR: not the ${...} form; see fix rationale
	} {
		cfg := validSecretsConfig()
		cfg.Auth.JWTSecret = secret
		if err := validateConfig(cfg); err != nil {
			t.Errorf("validateConfig rejected legitimate secret %q: %v", secret, err)
		}
	}
}

// helixConfigYAML is a complete, loadable config whose ONLY problem is the
// placeholder in auth.jwt_secret — mirroring config/config.yaml:28 exactly.
const helixConfigYAML = `version: "1.0.0"
application:
  name: "HelixCode"
server:
  address: "127.0.0.1"
  port: 8080
database:
  host: "localhost"
  port: 5432
  user: "helix"
  password: "a-real-database-password"
  dbname: "helixcode"
redis:
  enabled: false
auth:
  jwt_secret: "${HELIX_AUTH_JWT_SECRET}"
  bcrypt_cost: 12
workers:
  health_check_interval: 30
  max_concurrent_tasks: 10
tasks:
  max_retries: 3
llm:
  max_tokens: 4096
  temperature: 0.7
`

// TestLoad_RefusesShippedPlaceholderJWTSecret drives the REAL startup path
// (Load, not validateConfig directly) against a file that reproduces the
// shipped config byte-for-byte at the offending line.
func TestLoad_RefusesShippedPlaceholderJWTSecret(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte(helixConfigYAML), 0o600); err != nil {
		t.Fatalf("write temp config: %v", err)
	}
	t.Setenv("HELIX_CONFIG", path)
	// Save/restore: a bare os.Unsetenv leaks to every later test in the
	// binary and makes verdicts depend on -shuffle ordering.
	if prev, had := os.LookupEnv("HELIX_AUTH_JWT_SECRET"); had {
		t.Cleanup(func() { _ = os.Setenv("HELIX_AUTH_JWT_SECRET", prev) })
	}
	_ = os.Unsetenv("HELIX_AUTH_JWT_SECRET")

	cfg, err := Load()
	if err == nil {
		t.Fatalf("Load() accepted a config whose jwt_secret is the literal "+
			"%q — every JWT would be signed with a value committed to this "+
			"repository (loaded secret: %q)",
			"${HELIX_AUTH_JWT_SECRET}", cfg.Auth.JWTSecret)
	}
	if !strings.Contains(err.Error(), "HELIX_AUTH_JWT_SECRET") {
		t.Errorf("startup refusal must tell the operator which variable to export; got: %v", err)
	}
	if !strings.Contains(err.Error(), "auth.jwt_secret") {
		t.Errorf("startup refusal must name the offending config field; got: %v", err)
	}
}

// TestLoad_AcceptsSameConfigWhenEnvIsSet is the positive control for the test
// above: the identical file loads when the environment supplies the secret, so
// the refusal is caused by the placeholder and not by the file being unloadable.
func TestLoad_AcceptsSameConfigWhenEnvIsSet(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte(helixConfigYAML), 0o600); err != nil {
		t.Fatalf("write temp config: %v", err)
	}
	t.Setenv("HELIX_CONFIG", path)
	t.Setenv("HELIX_AUTH_JWT_SECRET", "a-real-jwt-signing-secret-32-chars+")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() must succeed when HELIX_AUTH_JWT_SECRET is exported: %v", err)
	}
	if cfg.Auth.JWTSecret != "a-real-jwt-signing-secret-32-chars+" {
		t.Fatalf("env var must win over the file placeholder, got %q", cfg.Auth.JWTSecret)
	}
}

// keep the database import used even if the struct literal above changes shape.
var _ = database.Config{}
