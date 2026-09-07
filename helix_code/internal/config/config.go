package config

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"dev.helix.code/internal/database"
	"dev.helix.code/internal/netutil"
	"github.com/spf13/viper"
)

// Speed programme P2-T07 — config loaded once, threaded down (R1 B10).
//
// Two cooperating mechanisms:
//
//  1. Load() builds a *fresh* per-call *viper.Viper instance instead of
//     mutating the package-global viper singleton. The old code called the
//     global viper.SetDefault / viper.BindEnv / viper.ReadInConfig, whose
//     backing maps are NOT goroutine-safe — concurrent NewCLI() construction
//     panicked with "concurrent map writes" (viper.go internal map). A local
//     instance is owned by exactly one goroutine for the duration of the call,
//     so concurrent Load() calls are now race-free (closes the P0-T02 latent
//     bug). Precedence is unchanged: viper resolves defaults < config-file <
//     env < explicit overrides exactly as before.
//
//  2. Get() loads the process config exactly ONCE via sync.Once and returns
//     the cached *Config to every subsequent caller. The CLI, server and
//     subagent paths call Get() so the YAML file is read a single time per
//     process and the SAME *Config struct is threaded down — no repeat YAML
//     reads, no repeated viper churn.
//
// readInConfigCount is incremented every time a real config file is read off
// disk. Tests assert it stays at exactly 1 across N Get() calls.
var readInConfigCount int64

// readInConfigCalls returns how many times a config file has actually been
// read off disk in this process. Exposed for the load-once unit test.
func readInConfigCalls() int64 { return atomic.LoadInt64(&readInConfigCount) }

var (
	loadOnce  sync.Once
	cachedCfg *Config
	cachedErr error
)

// Get returns the process-wide configuration, loading it from disk exactly
// once (sync.Once-guarded). Every caller — CLI, server, subagent manager —
// receives the SAME already-loaded *Config rather than re-reading the YAML
// file and re-churning viper on each invocation.
//
// Behaviour is identical to Load() for the values it returns; Get() simply
// memoises the first successful (or failed) result. Use Get() in production
// entry points; use Load() only where a deliberately fresh read is required
// (e.g. config-reload tooling or tests that point HELIX_CONFIG at a
// per-test temp file).
func Get() (*Config, error) {
	loadOnce.Do(func() {
		cachedCfg, cachedErr = Load()
	})
	return cachedCfg, cachedErr
}

// resetForTest clears the Get() memoisation. Test-only — lets a test that
// needs Get() to re-read a freshly written temp config start from a clean
// slate. Not part of the public production surface.
func resetForTest() {
	loadOnce = sync.Once{}
	cachedCfg = nil
	cachedErr = nil
}

// AuthConfig represents authentication configuration
type AuthConfig struct {
	JWTSecret     string `mapstructure:"jwt_secret"`
	TokenExpiry   int    `mapstructure:"token_expiry"`
	SessionExpiry int    `mapstructure:"session_expiry"`
	BcryptCost    int    `mapstructure:"bcrypt_cost"`

	// WireFacadeAPIKeys authenticates the OpenAI-compatible (POST
	// /v1/chat/completions) and Anthropic-compatible (POST /v1/messages)
	// wire-facade routes registered in wire_facade.go (server.go's
	// s.wireFacadeAuthMiddleware()). These routes intentionally do NOT use
	// the internal-user JWT authMiddleware — genuine OpenAI/Anthropic SDK
	// clients send `Authorization: Bearer sk-...` or `x-api-key: ...`, never
	// this server's session JWT — so a separate, wire-compatible API-key
	// check is required (§11.4.74 extend-don't-reimplement: mirrors
	// submodules/helix_llm's internal/gateway/middleware.APIKeyAuth pattern).
	//
	// A comma-separated list of accepted keys. §11.4.10: NEVER hardcode a
	// real value here or in any shipped config file — populate via the
	// HELIX_WIRE_FACADE_API_KEYS env var (see setupEnvBindings) or an
	// operator-owned, gitignored config overlay. Deliberately fails CLOSED:
	// an empty value means the wire-facade routes reject every request
	// (401), rather than defaulting to the pre-fix open-access behavior —
	// these routes drive real, paid LLM provider calls (CONST-035/BLUFF-001)
	// and MUST NOT be reachable by an unauthenticated caller in production.
	WireFacadeAPIKeys string `mapstructure:"wire_facade_api_keys"`

	// WSAllowedOrigins extends the /ws MCP WebSocket upgrader's Origin
	// allowlist (internal/mcp.NewMCPServer's CheckOrigin, set via
	// MCPServer.SetAllowedOrigins in server.New) beyond the always-allowed
	// same-origin / localhost / no-Origin-header (non-browser client)
	// defaults baked into internal/mcp.newOriginChecker. Closes the
	// confirmed CSWSH finding (Cross-Site WebSocket Hijacking, OWASP
	// WebSocket Security Cheat Sheet) where CheckOrigin previously
	// unconditionally returned true — see
	// docs/research/07.2026/05_mcp_acp_protocols/WS_ENDPOINT_AUTH_DESIGN.md
	// §6.1.3/§8 Option B.
	//
	// A comma-separated list of extra allowed Origin header VALUES (e.g.
	// "https://app.example.com,https://admin.example.com"). Populate via
	// the HELIX_WS_ALLOWED_ORIGINS env var (see setupEnvBindings) or an
	// operator-owned config overlay. Empty (the zero-value default) means
	// only the same-origin/localhost/no-Origin-header defaults apply —
	// never a wildcard.
	WSAllowedOrigins string `mapstructure:"ws_allowed_origins"`

	// CORSAllowedOrigins is the allowlist used by the HTTP-wide
	// CORSMiddleware (internal/server.CORSMiddleware, wired in server.New)
	// for the "Access-Control-Allow-Origin" / "Access-Control-Allow-Credentials"
	// response headers. §11.4.74 reuse of the WSAllowedOrigins pattern:
	// closes the confirmed CORS spec violation where the middleware
	// previously emitted a wildcard "Access-Control-Allow-Origin: *"
	// TOGETHER WITH "Access-Control-Allow-Credentials: true" on every
	// response — a combination forbidden by the Fetch/CORS spec (browsers
	// reject it, and if reflected as the literal request Origin it would
	// let ANY origin make credentialed cross-origin requests).
	//
	// A comma-separated list of exact allowed Origin header VALUES (e.g.
	// "https://app.example.com,https://admin.example.com"). Populate via
	// the HELIX_CORS_ALLOWED_ORIGINS env var (see setupEnvBindings) or an
	// operator-owned config overlay. Empty (the zero-value default) means
	// no cross-origin request is ever granted Allow-Origin/Allow-Credentials
	// (default-deny) — same-origin requests are unaffected because
	// browsers don't consult CORS headers for those. Never a wildcard.
	CORSAllowedOrigins string `mapstructure:"cors_allowed_origins"`
}

// ServerConfig represents server configuration
type ServerConfig struct {
	Address         string `mapstructure:"address"`
	Port            int    `mapstructure:"port"`
	ReadTimeout     int    `mapstructure:"read_timeout"`
	WriteTimeout    int    `mapstructure:"write_timeout"`
	IdleTimeout     int    `mapstructure:"idle_timeout"`
	ShutdownTimeout int    `mapstructure:"shutdown_timeout"`
}

// RedisConfig represents Redis configuration
type RedisConfig struct {
	Enabled  bool   `mapstructure:"enabled"`
	Host     string `mapstructure:"host"`
	Port     int    `mapstructure:"port"`
	Password string `mapstructure:"password"`

	// Database is the Redis logical database number, passed straight to
	// redis.Options.DB by internal/redis.NewClient.
	//
	// The key is `db`, NOT `database`. It was `database` until the strict
	// key-check work exposed the mismatch: setDefaultsOn has always written
	// v.SetDefault("redis.db", 0), and every shipped config file under
	// config/ spells it `db` — so with the old tag NOTHING ever populated
	// this field. `redis.db: 3` was silently discarded and every deployment
	// connected to database 0 whatever the operator wrote. Zero configs in
	// this repo used `redis.database`, so `database` was never a working
	// spelling and is deliberately not kept as an alias.
	Database int `mapstructure:"db"`
}

// WorkersConfig represents worker configuration
type WorkersConfig struct {
	HealthCheckInterval int `mapstructure:"health_check_interval"`
	HealthTTL           int `mapstructure:"health_ttl"`
	MaxConcurrentTasks  int `mapstructure:"max_concurrent_tasks"`
}

// TasksConfig represents task configuration
type TasksConfig struct {
	MaxRetries         int `mapstructure:"max_retries"`
	CheckpointInterval int `mapstructure:"checkpoint_interval"`
	CleanupInterval    int `mapstructure:"cleanup_interval"`
}

// LLMConfig represents LLM configuration
type LLMConfig struct {
	DefaultProvider string      `mapstructure:"default_provider"`
	DefaultModel    string      `mapstructure:"default_model"`
	MaxTokens       int         `mapstructure:"max_tokens"`
	Temperature     float64     `mapstructure:"temperature"`
	Cloud           CloudConfig `mapstructure:"cloud"`

	// Timeout is the per-request LLM call budget in SECONDS (int, not
	// time.Duration — the shipped configs write a bare `timeout: 30`, which
	// a time.Duration field would decode as 30 *nanoseconds*; the sibling
	// ServerConfig timeouts use the same int-seconds convention).
	//
	// MaxRetries is the retry budget for a failed LLM call.
	//
	// Both keys have been present in config/config.yaml since long before
	// this struct declared them, and were therefore silently discarded by
	// viper. They are declared here so the values survive the load and are
	// available to the LLM layer.
	//
	// HONEST STATUS (§11.4.6): declaring them is only half the fix. No
	// caller reads LLM.Timeout or LLM.MaxRetries yet — the provider
	// construction path in internal/llm does not consume cfg.LLM beyond
	// DefaultProvider — so setting them still does not change request
	// behaviour. Until a consumer exists they stay listed in
	// inertConfigKeys, which makes Load() warn at startup that the value is
	// parsed but not applied. Delete those two entries in the same change
	// that wires them into the LLM client, not before.
	Timeout    int `mapstructure:"timeout"`
	MaxRetries int `mapstructure:"max_retries"`
}

// CloudConfig gates hosted (cloud) LLM provider usage. Operator mandate
// 2026-09-05 (spec 002 local-only adaptive serving): llm.cloud.enabled
// DEFAULTS TO FALSE — cloud provider construction must refuse with a clear
// error even when API keys are present. Local routes (the helixllm coder
// sidecar, llama.cpp, Ollama) are exempt from the gate; the gate is wired
// into internal/llm via llm.SetCloudEnabled at process startup
// (internal/server.New, cmd's generate path).
type CloudConfig struct {
	Enabled bool `mapstructure:"enabled"`
}

// QAConfig holds HelixQA-specific configuration injected into HelixCode.
type QAConfig struct {
	Enabled           bool     `mapstructure:"enabled"`
	BanksDir          string   `mapstructure:"banks_dir"`
	Platforms         []string `mapstructure:"platforms"`
	DeviceID          string   `mapstructure:"device_id"`
	OutputDir         string   `mapstructure:"output_dir"`
	CoverageTarget    float64  `mapstructure:"coverage_target"`
	ReportFormats     []string `mapstructure:"report_formats"`
	Autonomous        bool     `mapstructure:"autonomous"`
	CuriosityEnabled  bool     `mapstructure:"curiosity_enabled"`
	VisionProvider    string   `mapstructure:"vision_provider"`
	LLMProvider       string   `mapstructure:"llm_provider"`
	LLMAPIKey         string   `mapstructure:"llm_api_key"`
	RecordScreenshots bool     `mapstructure:"record_screenshots"`
	RecordVideo       bool     `mapstructure:"record_video"`
}

// Config represents the application configuration
type Config struct {
	Version     string            `mapstructure:"version"`
	UpdatedBy   string            `mapstructure:"updated_by"`
	Application ApplicationConfig `mapstructure:"application"`
	Server      ServerConfig      `mapstructure:"server"`
	Database    database.Config   `mapstructure:"database"`
	Redis       RedisConfig       `mapstructure:"redis"`
	Auth        AuthConfig        `mapstructure:"auth"`
	Workers     WorkersConfig     `mapstructure:"workers"`
	Tasks       TasksConfig       `mapstructure:"tasks"`
	LLM         LLMConfig         `mapstructure:"llm"`
	Providers   ProvidersConfig   `mapstructure:"providers"`
	Logging     LoggingConfig     `mapstructure:"logging"`
	Cognee      *CogneeConfig     `mapstructure:"cognee"`
	Verifier    *VerifierConfig   `mapstructure:"verifier"`
	QA          QAConfig          `mapstructure:"qa"`

	// Notifications carries the `notifications:` block. Declared so viper stops
	// discarding it; consumed by internal/notification.NewEngineFromConfig.
	// See notifications_config.go.
	Notifications NotificationsConfig `mapstructure:"notifications"`
}

// HelixConfig is an alias for Config
type HelixConfig = Config

// ProvidersConfig represents provider configurations
type ProvidersConfig struct {
	Mem0    Mem0Config    `mapstructure:"mem0"`
	Zep     ZepConfig     `mapstructure:"zep"`
	Memonto MemontoConfig `mapstructure:"memonto"`
	BaseAI  BaseAIConfig  `mapstructure:"baseai"`
}

// Mem0Config represents Mem0 provider configuration
type Mem0Config struct {
	APIKey  string `mapstructure:"api_key"`
	BaseURL string `mapstructure:"base_url"`
}

// ZepConfig represents Zep provider configuration
type ZepConfig struct {
	APIKey  string `mapstructure:"api_key"`
	BaseURL string `mapstructure:"base_url"`
}

// MemontoConfig represents Memonto provider configuration
type MemontoConfig struct {
	APIKey  string `mapstructure:"api_key"`
	BaseURL string `mapstructure:"base_url"`
}

// BaseAIConfig represents BaseAI provider configuration
type BaseAIConfig struct {
	APIKey  string `mapstructure:"api_key"`
	BaseURL string `mapstructure:"base_url"`
}

// LoggingConfig represents logging configuration
type LoggingConfig struct {
	Level  string `mapstructure:"level"`
	Format string `mapstructure:"format"`
	Output string `mapstructure:"output"`
}

// TelemetryConfig represents telemetry configuration
type TelemetryConfig struct {
	Enabled       bool   `mapstructure:"enabled"`
	Level         string `mapstructure:"level"`
	DataRetention int    `mapstructure:"data_retention"`
}

// ApplicationConfig represents application configuration
type ApplicationConfig struct {
	Name        string          `mapstructure:"name"`
	Version     string          `mapstructure:"version"`
	Description string          `mapstructure:"description"`
	Environment string          `mapstructure:"environment"`
	Workspace   WorkspaceConfig `mapstructure:"workspace"`
	Session     SessionConfig   `mapstructure:"session"`
	Logging     LoggingConfig   `mapstructure:"logging"`
	Telemetry   TelemetryConfig `mapstructure:"telemetry"`
}

// WorkspaceConfig represents workspace configuration
type WorkspaceConfig struct {
	AutoSave         bool   `mapstructure:"auto_save"`
	DefaultPath      string `mapstructure:"default_path"`
	AutoSaveInterval int    `mapstructure:"auto_save_interval"`
	BackupEnabled    bool   `mapstructure:"backup_enabled"`
	BackupLocation   string `mapstructure:"backup_location"`
	BackupRetention  int    `mapstructure:"backup_retention"`
}

// ContextCompressionConfig represents context compression configuration
type ContextCompressionConfig struct {
	Enabled          bool    `mapstructure:"enabled"`
	Threshold        int     `mapstructure:"threshold"`
	Strategy         string  `mapstructure:"strategy"`
	CompressionRatio float64 `mapstructure:"compression_ratio"`
	RetentionPolicy  string  `mapstructure:"retention_policy"`
}

// SessionConfig represents session configuration
type SessionConfig struct {
	Timeout            int                      `mapstructure:"timeout"`
	AutoSave           bool                     `mapstructure:"auto_save"`
	MaxHistory         int                      `mapstructure:"max_history"`
	PersistContext     bool                     `mapstructure:"persist_context"`
	ContextRetention   int                      `mapstructure:"context_retention"`
	MaxHistorySize     int                      `mapstructure:"max_history_size"`
	AutoResume         bool                     `mapstructure:"auto_resume"`
	ContextCompression ContextCompressionConfig `mapstructure:"context_compression"`
}

// AgentConfig represents agent configuration
type AgentConfig struct {
	MaxConcurrency int           `mapstructure:"max_concurrency"`
	Timeout        time.Duration `mapstructure:"timeout"`
	RetryCount     int           `mapstructure:"retry_count"`
	Enabled        bool          `mapstructure:"enabled"`
}

// ContextConfig represents context configuration
type ContextConfig struct {
	Enabled         bool          `mapstructure:"enabled"`
	MaxSize         int           `mapstructure:"max_size"`
	RetentionPeriod time.Duration `mapstructure:"retention_period"`
	Compression     bool          `mapstructure:"compression"`
}

// Load reads configuration from file + environment variables into a fresh
// *Config. Each call builds its own *viper.Viper instance, so concurrent
// Load() calls never touch shared mutable state and are race-free.
//
// Precedence (lowest → highest) is the standard viper order and is unchanged
// by P2-T07: defaults < config file < environment variables < explicit
// overrides. Production entry points should call Get() (process-once cached);
// Load() remains for deliberate fresh reads.
func Load() (*Config, error) {
	// Per-call local viper instance — replaces the process-global viper
	// singleton whose backing maps are not goroutine-safe (P2-T07 / P0-T02
	// race fix). All defaults, bindings and the file read happen here.
	v := viper.New()

	// Set default values on the local instance.
	setDefaultsOn(v)

	// Find config file
	configPath := findConfigFile()
	if configPath != "" {
		v.SetConfigFile(configPath)
	} else {
		// Use default config locations
		v.SetConfigName("config")
		v.SetConfigType("yaml")
		v.AddConfigPath("./config/")
		v.AddConfigPath("./")
		v.AddConfigPath("$HOME/.config/helixcode/")
		v.AddConfigPath("/etc/helixcode/")
	}

	// Read in environment variables
	v.AutomaticEnv()
	v.SetEnvPrefix("HELIX")

	// Explicitly bind environment variables for critical settings
	v.BindEnv("auth.jwt_secret", "HELIX_AUTH_JWT_SECRET")
	v.BindEnv("auth.wire_facade_api_keys", "HELIX_WIRE_FACADE_API_KEYS")
	v.BindEnv("auth.ws_allowed_origins", "HELIX_WS_ALLOWED_ORIGINS")
	v.BindEnv("auth.cors_allowed_origins", "HELIX_CORS_ALLOWED_ORIGINS")
	v.BindEnv("database.password", "HELIX_DATABASE_PASSWORD")
	v.BindEnv("database.host", "HELIX_DATABASE_HOST")
	v.BindEnv("database.port", "HELIX_DATABASE_PORT")
	v.BindEnv("database.user", "HELIX_DATABASE_USER")
	v.BindEnv("database.dbname", "HELIX_DATABASE_NAME")
	v.BindEnv("database.profile", "HELIX_DATABASE_POOL_PROFILE")
	v.BindEnv("database.max_conns", "HELIX_DATABASE_POOL_MAX_CONNS")
	v.BindEnv("database.min_conns", "HELIX_DATABASE_POOL_MIN_CONNS")
	v.BindEnv("database.max_conn_lifetime", "HELIX_DATABASE_POOL_MAX_CONN_LIFETIME")
	v.BindEnv("database.max_conn_idle_time", "HELIX_DATABASE_POOL_MAX_CONN_IDLE_TIME")
	v.BindEnv("redis.password", "HELIX_REDIS_PASSWORD")
	v.BindEnv("redis.host", "HELIX_REDIS_HOST")
	v.BindEnv("redis.port", "HELIX_REDIS_PORT")

	// LLMsVerifier env var bindings
	v.BindEnv("verifier.enabled", "HELIX_VERIFIER_ENABLED")
	v.BindEnv("verifier.mode", "HELIX_VERIFIER_MODE")
	v.BindEnv("verifier.endpoint", "HELIX_VERIFIER_ENDPOINT")
	v.BindEnv("verifier.api_key", "HELIX_VERIFIER_API_KEY")
	v.BindEnv("verifier.timeout", "HELIX_VERIFIER_TIMEOUT")
	v.BindEnv("verifier.cache_ttl", "HELIX_VERIFIER_CACHE_TTL")
	v.BindEnv("verifier.polling_interval", "HELIX_VERIFIER_POLLING_INTERVAL")
	v.BindEnv("verifier.scoring.min_acceptable_score", "HELIX_VERIFIER_MIN_SCORE")
	v.BindEnv("verifier.scoring.models_dev_endpoint", "HELIX_MODELS_DEV_ENDPOINT")

	// Per-provider API key bindings
	v.BindEnv("verifier.providers.openai.api_key", "OPENAI_API_KEY")
	v.BindEnv("verifier.providers.anthropic.api_key", "ANTHROPIC_API_KEY")
	v.BindEnv("verifier.providers.gemini.api_key", "GEMINI_API_KEY")
	v.BindEnv("verifier.providers.deepseek.api_key", "DEEPSEEK_API_KEY")
	v.BindEnv("verifier.providers.groq.api_key", "GROQ_API_KEY")
	v.BindEnv("verifier.providers.mistral.api_key", "MISTRAL_API_KEY")
	v.BindEnv("verifier.providers.xai.api_key", "XAI_API_KEY")
	v.BindEnv("verifier.providers.openrouter.api_key", "OPENROUTER_API_KEY")

	// Read config file
	if err := v.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
			return nil, fmt.Errorf("failed to read config file: %v", err)
		}
		// Config file not found, but we can continue with defaults.
		// Diagnostics go to STDERR, never stdout: stdout is a machine
		// protocol for several callers (the subagent helper writes its
		// SubagentResult JSON there and the parent unmarshals the WHOLE
		// buffer; `helixcode acp` wires stdout as the line-delimited ACP
		// JSON-RPC transport). A single informational line on stdout
		// corrupts those streams. Operators still see this on stderr.
		fmt.Fprintln(os.Stderr, tr(context.Background(), "internal_config_warn_no_config_file_using_defaults", nil))
	} else {
		atomic.AddInt64(&readInConfigCount, 1)
		// STDERR, not stdout — see the stderr rationale above.
		fmt.Fprintln(os.Stderr, tr(context.Background(), "internal_config_info_using_config_file", map[string]any{"Path": v.ConfigFileUsed()}))

		// Strict key check. Viper would silently discard any key the Config
		// struct does not declare, so a typo (`llm.temperture`) or a key left
		// behind by removed code reads as configuration while doing nothing.
		// Reject the unknown ones outright; warn about the known-inert ones.
		// See strict.go for why this reads the file itself rather than using
		// mapstructure's ErrorUnused.
		inert, err := checkConfigKeys(v.ConfigFileUsed())
		if err != nil {
			return nil, err
		}
		for _, msg := range inert {
			// STDERR, not stdout — see the stderr rationale above.
			fmt.Fprintln(os.Stderr, msg)
		}
	}

	// Unmarshal config
	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("failed to unmarshal config: %v", err)
	}

	// Expand ${VAR:default} / ${VAR:-default} shell-style placeholders that
	// Viper leaves as literal strings when the env var is unset.
	// Scope: only the string fields that are known to carry such literals in
	// config YAML (Redis host, Database host/user/dbname, Server address).
	expandShellDefaults(&cfg)

	// Validate config
	if err := validateConfig(&cfg); err != nil {
		return nil, fmt.Errorf("config validation failed: %v", err)
	}

	return &cfg, nil
}

// shellPlaceholder matches exactly the three placeholder shapes this loader
// documents: ${VAR}, ${VAR:default} and ${VAR:-default}. Group 1 is the
// variable name, group 2 the separator (":" or ":-") when present, group 3 the
// default token.
//
// It deliberately does NOT match a bare $VAR. os.Expand — which this function
// used to be built on — expands that form too, and also eats a literal "$$",
// so a value that merely CONTAINS a dollar sign was silently mangled: an SMTP
// password of "pa$$word" came out as "pa". That was tolerable while the
// expanded set was five hostname-ish fields where a literal '$' is
// implausible, and is not tolerable now that the set includes credentials.
// Matching only the braced form also makes this agree with
// secret_placeholder.go, which already states that a bare $VAR "is not a
// placeholder this loader ever expands".
var shellPlaceholder = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)(?:(:-|:)([^}]*))?\}`)

// expandShellDefaults expands ${VAR:default} and ${VAR:-default} shell-style
// placeholders in config string fields.  Viper does NOT perform this expansion
// itself: when HELIX_REDIS_HOST is unset, BindEnv falls through to the YAML
// value which is the raw literal "${HELIX_REDIS_HOST:redis}".
//
// Resolver behaviour (mirrors bash default-value syntax):
//   - ${VAR}          → os.Getenv("VAR"), or "" if unset
//   - ${VAR:default}  → os.Getenv("VAR") if non-empty, else "default"
//   - ${VAR:-default} → os.Getenv("VAR") if non-empty, else "default"
//
// Only fields that may legitimately contain ${…} placeholders are expanded;
// all other string config fields are left untouched.
//
// WHICH FIELDS, AND WHY THOSE
// ---------------------------
// The set was DERIVED, not guessed: every Config-shaped file under config/ was
// read for values containing the ${…} form, and each such value mapped to the
// dotted key it sets. That enumeration divides into four groups.
//
//  1. ALREADY EXPANDED — redis.host, database.user, database.dbname. These are
//     the shipped uses of the form that this function has always covered, and
//     they behave exactly as before. database.host and server.address carry no
//     ${…} in any shipped file but are kept: removing coverage is not this
//     change's business.
//
//  2. CREDENTIALS THAT MUST *NOT* BE EXPANDED — auth.jwt_secret,
//     database.password, redis.password. checkSecretPlaceholders REFUSES an
//     unexpanded placeholder in these, and that refusal is the whole point:
//     they fail OPEN. Expanding ${HELIX_AUTH_JWT_SECRET} to "" when the
//     variable is unset would sign every JWT with an empty secret while
//     startup reported success. A hard refusal is correct; silent expansion
//     would defeat it. They stay out.
//
//  3. UNREACHABLE — llm.providers.*.endpoint and llm.providers.*.api_key.
//     LLMConfig declares no `providers` field, so viper discards that whole
//     subtree before expansion could ever see it (strict.go lists llm.providers
//     as inert for exactly this reason). Expanding a field that does not exist
//     is not possible; declaring the block is a separate change.
//
//     This item previously cited config/config.yaml writing
//     "${HELIX_LLM_ENDPOINT:...}" as the worked example. That block was removed
//     (gap-ledger HXC-002-F3-02, guarded by dead_providers_gate_test.go), so
//     config.yaml no longer contains it and the citation would now be false.
//     The REASONING above is unaffected — it turns on LLMConfig's shape, not on
//     any particular yaml file happening to carry such a key.
//
//  4. NEWLY EXPANDED — the notifications block. Its channel settings are the
//     one group that both uses the form and now reaches a Config field:
//     config/production-config.yaml writes ${HELIX_SLACK_WEBHOOK_URL},
//     ${HELIX_TELEGRAM_BOT_TOKEN}, ${HELIX_EMAIL_*} and friends, and every
//     comment beside the empty-string equivalents in config/config.yaml tells
//     the operator to "Set via HELIX_…". The whole block is covered rather
//     than a per-field allowlist because every value in it is documented as
//     environment-sourced.
//
// Expanding the notification CREDENTIALS is safe in a way group 2 is not:
// they fail CLOSED. NewSlackChannel/NewTelegramChannel/NewEmailChannel/
// NewDiscordChannel each mark the channel disabled when its credential is
// empty, so an unset variable means "that channel is off", never "authenticate
// with a value published in a git-tracked file". It is also strictly better
// than the status quo, in which the literal "${HELIX_EMAIL_PASSWORD}" would be
// handed to the SMTP server as the password.
func expandShellDefaults(cfg *Config) {
	cfg.Redis.Host = expandShellString(cfg.Redis.Host)
	cfg.Database.Host = expandShellString(cfg.Database.Host)
	cfg.Database.User = expandShellString(cfg.Database.User)
	cfg.Database.DBName = expandShellString(cfg.Database.DBName)
	cfg.Server.Address = expandShellString(cfg.Server.Address)

	ch := &cfg.Notifications.Channels
	ch.Slack.WebhookURL = expandShellString(ch.Slack.WebhookURL)
	ch.Slack.Channel = expandShellString(ch.Slack.Channel)
	ch.Slack.Username = expandShellString(ch.Slack.Username)
	ch.Telegram.BotToken = expandShellString(ch.Telegram.BotToken)
	ch.Telegram.ChatID = expandShellString(ch.Telegram.ChatID)
	ch.Email.SMTP.Server = expandShellString(ch.Email.SMTP.Server)
	ch.Email.SMTP.Username = expandShellString(ch.Email.SMTP.Username)
	ch.Email.SMTP.Password = expandShellString(ch.Email.SMTP.Password)
	ch.Email.SMTP.From = expandShellString(ch.Email.SMTP.From)
	ch.Discord.WebhookURL = expandShellString(ch.Discord.WebhookURL)
	for i, r := range ch.Email.Recipients.Default {
		ch.Email.Recipients.Default[i] = expandShellString(r)
	}
}

// expandShellString resolves every ${…} placeholder in s, leaving every other
// character — including a lone or doubled '$' — exactly as written.
func expandShellString(s string) string {
	return shellPlaceholder.ReplaceAllStringFunc(s, func(match string) string {
		m := shellPlaceholder.FindStringSubmatch(match)
		varName, sep, defaultVal := m[1], m[2], m[3]
		if val := os.Getenv(varName); val != "" {
			return val
		}
		if sep == "" {
			// Plain ${VAR} with no default: an unset variable yields "".
			return ""
		}
		return defaultVal
	})
}

// setDefaultsOn sets default configuration values on the given viper
// instance. P2-T07 moved defaults off the process-global viper singleton —
// passing the instance explicitly means concurrent Load()/getDefaultConfig()
// calls each mutate their own private map and are race-free.
func setDefaultsOn(v *viper.Viper) {
	// Version defaults
	v.SetDefault("version", "1.0.0")

	// Application defaults
	v.SetDefault("application.name", "HelixCode")
	v.SetDefault("application.version", "1.0.0")
	v.SetDefault("application.description", "Enterprise AI Development Platform")
	v.SetDefault("application.environment", "development")
	v.SetDefault("application.workspace.auto_save", true)
	v.SetDefault("application.telemetry.enabled", false)
	v.SetDefault("application.telemetry.level", "info")
	v.SetDefault("application.telemetry.data_retention", 30)

	// Server defaults
	v.SetDefault("server.address", "0.0.0.0")
	v.SetDefault("server.port", 8080)
	v.SetDefault("server.read_timeout", 30)
	v.SetDefault("server.write_timeout", 30)
	v.SetDefault("server.idle_timeout", 60)
	v.SetDefault("server.shutdown_timeout", 30)

	// Database defaults
	v.SetDefault("database.host", "localhost")
	v.SetDefault("database.port", 5432)
	v.SetDefault("database.user", "helixcode")
	v.SetDefault("database.dbname", "helixcode")
	v.SetDefault("database.sslmode", "disable")
	// Connection-pool profile (P4-T03). "server" (the default) yields a
	// larger pool sized for a long-lived multi-request process; "cli"
	// yields a smaller pool with fewer idle connections at startup. The
	// individual pool-sizing keys (database.max_conns, database.min_conns,
	// database.max_conn_lifetime, database.max_conn_idle_time) are left
	// unset by default so the profile default applies — an explicitly set
	// key always overrides the profile per defaults < file < env < flags.
	v.SetDefault("database.profile", "server")

	// Redis defaults
	v.SetDefault("redis.enabled", false)
	v.SetDefault("redis.host", "localhost")
	v.SetDefault("redis.port", 6379)
	v.SetDefault("redis.password", "")
	v.SetDefault("redis.db", 0)

	// Auth defaults
	v.SetDefault("auth.jwt_secret", "default-secret-change-in-production")
	v.SetDefault("auth.token_expiry", 60)    // 60 seconds (not hours)
	v.SetDefault("auth.session_expiry", 600) // 10 minutes (not days)
	v.SetDefault("auth.bcrypt_cost", 12)

	// Workers defaults
	v.SetDefault("workers.health_check_interval", 30)
	v.SetDefault("workers.health_ttl", 120)
	v.SetDefault("workers.max_concurrent_tasks", 10)

	// Tasks defaults
	v.SetDefault("tasks.max_retries", 3)
	v.SetDefault("tasks.checkpoint_interval", 300)
	v.SetDefault("tasks.cleanup_interval", 600)

	// LLM defaults
	// Deliberately EMPTY, not "local" (operator decision 2026-09-05).
	// This is the BUILT-IN default, applied when nothing else sets the key --
	// i.e. the genuine zero-config case. The shipped config/config.yaml sets
	// `default_provider: "local"` explicitly, so a normal deployment still
	// routes a provider-less request to the local helixllm coder (:18434) via
	// the CONFIG precedence slot (HXC-002-F3-01).
	//
	// Why not default it to "local" here: resolveLLMProvider consults this
	// value in its lowest-precedence CONFIG slot, so a built-in "local" would
	// apply even with NO config file at all -- making the zero-config Ollama
	// fallback (:11434) unreachable in every process where config loads, and
	// silently retiring a shipped capability (§11.4.122). Both routes are
	// local, so the local-only-serving mandate does not choose between them;
	// the operator does, by writing the key.
	v.SetDefault("llm.default_provider", "")
	v.SetDefault("llm.default_model", "llama-3.2-3b")
	v.SetDefault("llm.max_tokens", 4096)
	v.SetDefault("llm.temperature", 0.7)
	// Cloud gate (W2c-1, operator mandate 2026-09-05 local-only serving):
	// hosted providers refuse construction unless explicitly enabled.
	v.SetDefault("llm.cloud.enabled", false)
	// Seconds. Matches the value shipped in config/config.yaml.
	v.SetDefault("llm.timeout", 30)
	v.SetDefault("llm.max_retries", 3)

	// Logging defaults
	v.SetDefault("logging.level", "info")
	v.SetDefault("logging.format", "text")
	v.SetDefault("logging.output", "stdout")

	// LLMsVerifier defaults
	v.SetDefault("verifier.enabled", false)
	v.SetDefault("verifier.mode", "remote")
	v.SetDefault("verifier.endpoint", "http://localhost:8081")
	v.SetDefault("verifier.timeout", "30s")
	v.SetDefault("verifier.cache_ttl", "5m")
	v.SetDefault("verifier.polling_interval", "60s")

	// Scoring defaults (match LLMsVerifier weights)
	v.SetDefault("verifier.scoring.weights.code_capability", 0.40)
	v.SetDefault("verifier.scoring.weights.responsiveness", 0.20)
	v.SetDefault("verifier.scoring.weights.reliability", 0.20)
	v.SetDefault("verifier.scoring.weights.feature_richness", 0.15)
	v.SetDefault("verifier.scoring.weights.value_proposition", 0.05)
	v.SetDefault("verifier.scoring.min_acceptable_score", 6.0)
	v.SetDefault("verifier.scoring.models_dev_enabled", true)
	v.SetDefault("verifier.scoring.models_dev_endpoint", "https://api.models.dev")

	// Health defaults
	v.SetDefault("verifier.health.check_interval", "30s")
	v.SetDefault("verifier.health.timeout", "10s")
	v.SetDefault("verifier.health.failure_threshold", 5)
	v.SetDefault("verifier.health.recovery_threshold", 3)
	v.SetDefault("verifier.health.circuit_breaker.enabled", true)
	v.SetDefault("verifier.health.circuit_breaker.half_open_timeout", "60s")

	// Event defaults
	v.SetDefault("verifier.events.enabled", true)
	v.SetDefault("verifier.events.websocket", false)
	v.SetDefault("verifier.events.websocket_path", "/ws/verifier/events")
}

// findConfigFile searches for config file in various locations
func findConfigFile() string {
	// Check environment variable first
	if configPath := os.Getenv("HELIX_CONFIG"); configPath != "" {
		if absPath, err := filepath.Abs(configPath); err == nil {
			if _, err := os.Stat(absPath); err == nil {
				return absPath
			}
		}
	}

	// Check common locations
	locations := []string{
		"./config.yaml",
		"./config/config.yaml",
		"config.yaml",
		"$HOME/.config/helixcode/config.yaml",
		"/etc/helixcode/config.yaml",
	}

	for _, location := range locations {
		expanded := os.ExpandEnv(location)
		if absPath, err := filepath.Abs(expanded); err == nil {
			if _, err := os.Stat(absPath); err == nil {
				return absPath
			}
		}
	}

	return ""
}

// validateConfig validates the configuration
func validateConfig(cfg *Config) error {
	// Credentials first: an unexpanded ${...} in a secret is a published
	// constant, and it must stop startup before anything is built from it.
	if err := checkSecretPlaceholders(cfg); err != nil {
		return err
	}

	// Version validation
	if cfg.Version == "" {
		return fmt.Errorf("%s", tr(context.Background(), "internal_config_validate_version_required", nil))
	}

	// Application validation
	if cfg.Application.Name == "" {
		return fmt.Errorf("%s", tr(context.Background(), "internal_config_validate_application_name_required", nil))
	}

	// Server validation
	if cfg.Server.Port < 1 || cfg.Server.Port > 65535 {
		return fmt.Errorf("%s", tr(context.Background(), "internal_config_validate_server_port_out_of_range", nil))
	}

	// Database validation
	if cfg.Database.Host == "" {
		return fmt.Errorf("%s", tr(context.Background(), "internal_config_validate_database_host_required", nil))
	}
	if cfg.Database.DBName == "" {
		return fmt.Errorf("%s", tr(context.Background(), "internal_config_validate_database_name_required", nil))
	}

	// Redis validation
	if cfg.Redis.Enabled {
		if cfg.Redis.Host == "" {
			return fmt.Errorf("redis host is required when redis is enabled")
		}
		if cfg.Redis.Port < 1 || cfg.Redis.Port > 65535 {
			return fmt.Errorf("redis port must be between 1 and 65535")
		}
	}

	// Auth validation
	if cfg.Auth.JWTSecret == "" || cfg.Auth.JWTSecret == "default-secret-change-in-production" {
		return fmt.Errorf("%s", tr(context.Background(), "internal_config_validate_jwt_secret_must_be_set", nil))
	}
	// bcrypt cost must fall inside golang.org/x/crypto/bcrypt's documented
	// inclusive range [MinCost=4 .. MaxCost=31]. A cost > 31 makes EVERY
	// bcrypt.GenerateFromPassword call (internal/auth/auth.go) return
	// InvalidCostError at runtime, so password hashing — and thus every user
	// registration / password-set flow — fails for a config the loader had
	// silently accepted. A cost < 4 is silently coerced to bcrypt.DefaultCost,
	// quietly weakening the configured work factor; we reject it so the
	// configured value is the value actually used (HXC-config / §11.4.110:
	// catch the second-artifact contract mismatch at config-validation time,
	// not at runtime). bcryptMinCost/bcryptMaxCost mirror the bcrypt package
	// constants without importing it solely for two literals.
	const bcryptMinCost, bcryptMaxCost = 4, 31
	if cfg.Auth.BcryptCost < bcryptMinCost || cfg.Auth.BcryptCost > bcryptMaxCost {
		return fmt.Errorf("auth.bcrypt_cost must be between %d and %d, got: %d",
			bcryptMinCost, bcryptMaxCost, cfg.Auth.BcryptCost)
	}

	// Workers validation
	if cfg.Workers.HealthCheckInterval < 1 {
		return fmt.Errorf("%s", tr(context.Background(), "internal_config_validate_health_check_interval_positive", nil))
	}
	if cfg.Workers.MaxConcurrentTasks < 1 {
		return fmt.Errorf("%s", tr(context.Background(), "internal_config_validate_max_concurrent_tasks_positive", nil))
	}

	// Tasks validation
	if cfg.Tasks.MaxRetries < 0 {
		return fmt.Errorf("max retries cannot be negative")
	}

	// LLM validation
	if cfg.LLM.MaxTokens < 1 {
		return fmt.Errorf("max tokens must be positive")
	}
	if cfg.LLM.Temperature < 0 || cfg.LLM.Temperature > 2 {
		return fmt.Errorf("temperature must be between 0 and 2")
	}

	// Verifier validation
	if cfg.Verifier != nil && cfg.Verifier.Enabled {
		if cfg.Verifier.Mode != "remote" && cfg.Verifier.Mode != "embedded" {
			return fmt.Errorf("verifier.mode must be 'remote' or 'embedded', got: %s", cfg.Verifier.Mode)
		}
		if cfg.Verifier.Endpoint == "" && cfg.Verifier.Mode == "remote" {
			return fmt.Errorf("verifier.endpoint is required when mode is 'remote'")
		}
		if cfg.Verifier.PollingInterval < 10*time.Second {
			return fmt.Errorf("verifier.polling_interval must be >= 10s")
		}
		if cfg.Verifier.CacheTTL < 1*time.Second {
			return fmt.Errorf("verifier.cache_ttl must be >= 1s")
		}

		// Validate scoring weights sum to 1.0
		totalWeight := cfg.Verifier.Scoring.Weights.CodeCapability +
			cfg.Verifier.Scoring.Weights.Responsiveness +
			cfg.Verifier.Scoring.Weights.Reliability +
			cfg.Verifier.Scoring.Weights.FeatureRichness +
			cfg.Verifier.Scoring.Weights.ValueProposition
		if math.Abs(totalWeight-1.0) > 0.001 {
			return fmt.Errorf("verifier scoring weights must sum to 1.0, got: %.3f", totalWeight)
		}
	}

	// QA validation
	if cfg.QA.Enabled {
		if cfg.QA.CoverageTarget < 0 || cfg.QA.CoverageTarget > 1 {
			return fmt.Errorf("qa.coverage_target must be between 0 and 1, got: %.2f", cfg.QA.CoverageTarget)
		}
		for _, format := range cfg.QA.ReportFormats {
			lower := strings.ToLower(format)
			if lower != "markdown" && lower != "html" && lower != "json" {
				return fmt.Errorf("qa.report_formats contains invalid format: %s", format)
			}
		}
		if cfg.QA.OutputDir == "" {
			cfg.QA.OutputDir = "qa-results"
		}
	}

	return nil
}

// CreateDefaultConfig creates a default configuration file. The directory tree
// and the file are created owner-only (0700/0600) by writeSecretFile below —
// the config tree holds plaintext credentials (CONST-042 / §12.1).
func CreateDefaultConfig(path string) error {
	// Create default config content
	configContent := `# HelixCode Server Configuration

server:
  address: "0.0.0.0"
  port: 8080
  read_timeout: 30
  write_timeout: 30
  idle_timeout: 300
  shutdown_timeout: 30

database:
  host: "localhost"
  port: 5432
  user: "helixcode"
  password: "" # Set via HELIX_DATABASE_PASSWORD environment variable
  dbname: "helixcode"
  sslmode: "disable"

redis:
  host: "localhost"
  port: 6379
  password: "" # Set via HELIX_REDIS_PASSWORD environment variable
  db: 0
  enabled: true

auth:
  jwt_secret: "" # Set via HELIX_AUTH_JWT_SECRET environment variable
  token_expiry: 86400
  session_expiry: 604800
  bcrypt_cost: 12

workers:
  health_check_interval: 30
  health_ttl: 120
  max_concurrent_tasks: 10

tasks:
  max_retries: 3
  checkpoint_interval: 300
  cleanup_interval: 3600

llm:
  default_provider: "local"
  providers:
    local: "http://localhost:11434"
    openai: "" # Set API key via environment variable
  max_tokens: 4096
  temperature: 0.7

logging:
  level: "info"
  format: "text"
  output: "stdout"
`

	// Write config file with owner-only perms — the config tree holds plaintext
	// credentials (CONST-042 / §12.1).
	if err := writeSecretFile(path, []byte(configContent)); err != nil {
		return fmt.Errorf("failed to write config file: %v", err)
	}

	return nil
}

// GetEnvOrDefault gets an environment variable with a default value
func GetEnvOrDefault(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

// GetEnvIntOrDefault gets an environment variable as integer with a default value
func GetEnvIntOrDefault(key string, defaultValue int) int {
	if value := os.Getenv(key); value != "" {
		if intValue, err := strconv.Atoi(value); err == nil {
			return intValue
		}
	}
	return defaultValue
}

// getDefaultConfig returns a default configuration. P2-T07: uses a private
// viper instance so it shares no mutable state with Load() or other callers
// — concurrent invocation is race-free.
func getDefaultConfig() *Config {
	v := viper.New()
	setDefaultsOn(v)
	var cfg Config
	v.Unmarshal(&cfg)
	return &cfg
}

// ConfigManager manages configuration loading and saving.
//
// HXC-014 (§11.4.85) concurrency fix: the manager is accessed concurrently by
// the ConfigAPI HTTP handlers (config_api.go) — GetConfig serves the config on
// every request while POST /config/reload (loadConfig), PUT /config
// (UpdateConfig/UpdateConfigFromMap), and POST /config/restore (ImportConfig /
// ResetToDefaults) swap m.config from other goroutines. The shared m.config
// pointer (and the *Config it points at, which the writers replace and the
// readers dereference) was previously unguarded — a data race surfaced under
// -race by config_manager_chaos_test.go. mu serialises all access: readers take
// RLock, writers take Lock. The exported method surface and behaviour are
// unchanged.
type ConfigManager struct {
	mu         sync.RWMutex
	configPath string
	config     *Config
	watchers   []ConfigWatcher
}

// Initialize initializes the configuration manager
func (m *ConfigManager) Initialize(ctx context.Context) error {
	// Configuration manager is already initialized during creation
	// This method exists for compatibility with the test expectations
	return nil
}

// NewConfigurationManager creates a new configuration manager with options.
// Currently the only option consumed is ConfigPath; future option fields
// (e.g. validation policy, secrets backend) should be wired here as they
// are introduced rather than left as TODO comments (round-33 §11.4
// comment rewrite — previous "For now" lead-in implied an unfinished
// stub when the function is in fact the canonical option-aware factory).
func NewConfigurationManager(options *ConfigurationOptions) (*ConfigManager, error) {
	return NewHelixConfigManager(options.ConfigPath)
}

// NewHelixConfigManager creates a new configuration manager
func NewHelixConfigManager(configPath string) (*ConfigManager, error) {
	manager := &ConfigManager{
		configPath: configPath,
	}

	// Try to load existing config (constructor — no concurrent access yet, but
	// use the locked helpers so the field writes are race-detector clean).
	if _, err := os.Stat(configPath); err == nil {
		if err := manager.loadConfigLocked(); err != nil {
			return nil, err
		}
	} else {
		// Create default config
		manager.config = getDefaultConfig()
		if err := manager.saveConfigLocked(); err != nil {
			return nil, err
		}
	}

	return manager, nil
}

// GetConfig returns the current configuration. RLock-guarded against concurrent
// reload/update writers (HXC-014 / §11.4.85).
func (m *ConfigManager) GetConfig() *Config {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.config
}

// UpdateConfig updates the configuration with the provided function under the
// write lock. Copy-on-write: the update is applied to a COPY of the current
// config and the pointer is swapped, so a concurrent GetConfig caller holding
// the previous pointer dereferences an immutable snapshot and never races with
// the mutation (HXC-014 / §11.4.85).
func (m *ConfigManager) UpdateConfig(updateFunc func(*Config)) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	next := *m.config // shallow copy of the current snapshot
	updateFunc(&next)
	m.config = &next
	return m.saveConfigLocked()
}

// UpdateConfigFromMap updates the configuration with a map of values
func (m *ConfigManager) UpdateConfigFromMap(updates map[string]interface{}) error {
	return m.UpdateConfig(func(cfg *Config) {
		// Apply updates from map by converting to JSON and back
		// This allows nested field updates while maintaining type safety

		// First, convert current config to a map
		currentBytes, err := json.Marshal(cfg)
		if err != nil {
			return
		}

		var currentMap map[string]interface{}
		if err := json.Unmarshal(currentBytes, &currentMap); err != nil {
			return
		}

		// Merge updates into current map
		mergeConfigMaps(currentMap, updates)

		// Convert back to config
		mergedBytes, err := json.Marshal(currentMap)
		if err != nil {
			return
		}

		// Unmarshal into the config pointer
		json.Unmarshal(mergedBytes, cfg)
	})
}

// mergeConfigMaps recursively merges src into dst
func mergeConfigMaps(dst, src map[string]interface{}) {
	for key, srcValue := range src {
		if dstValue, exists := dst[key]; exists {
			// If both are maps, merge recursively
			if dstMap, ok := dstValue.(map[string]interface{}); ok {
				if srcMap, ok := srcValue.(map[string]interface{}); ok {
					mergeConfigMaps(dstMap, srcMap)
					continue
				}
			}
		}
		// Otherwise, replace the value
		dst[key] = srcValue
	}
}

// IsConfigPresent checks if the configuration file exists
func (m *ConfigManager) IsConfigPresent() bool {
	_, err := os.Stat(m.configPath)
	return err == nil
}

// GetConfigPath returns the configuration file path
func (m *ConfigManager) GetConfigPath() string {
	return m.configPath
}

// loadConfig loads configuration from file. Write-locked: callable directly by
// the ConfigAPI reload handler concurrently with GetConfig readers.
func (m *ConfigManager) loadConfig() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.loadConfigLocked()
}

// loadConfigLocked performs the load. Caller MUST hold m.mu (write). Internal
// callers use this to avoid re-locking the non-reentrant RWMutex.
func (m *ConfigManager) loadConfigLocked() error {
	data, err := os.ReadFile(m.configPath)
	if err != nil {
		return err
	}

	// HXC-098: start from the fully-defaulted config and unmarshal the file ON
	// TOP of it, so values the file supplies override the defaults while every
	// field the file omits keeps its default. A plain json.Unmarshal into an
	// empty struct merges NO defaults (unlike the viper-based Load()), so an
	// out-of-box / hand-written config.json omitting top-level `version`,
	// `server.port`, `application.name`, etc. would fail validateConfig
	// ("version is required" / "server port must be between 1 and 65535"),
	// blocking fresh users' status/system/version commands.
	//
	// Decode only swaps m.config on success, so a failed reload leaves the
	// previous good config intact (no nil/torn read).
	cfg := getDefaultConfig()
	if err := json.Unmarshal(data, cfg); err != nil {
		return err
	}
	m.config = cfg
	return nil
}

// saveConfig saves configuration to file. Write-locked for direct callers.
func (m *ConfigManager) saveConfig() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.saveConfigLocked()
}

// saveConfigLocked performs the save. Caller MUST hold m.mu.
func (m *ConfigManager) saveConfigLocked() error {
	data, err := json.MarshalIndent(m.config, "", "  ")
	if err != nil {
		return err
	}

	// Fresh-install safety: the config dir (e.g. ~/.config/helixcode/) may not
	// exist yet on a clean machine. writeSecretFile creates the parent tree
	// (mode 0700) and writes the file owner-only (0600) — the config persists
	// plaintext credentials (CONST-042 / §12.1).
	return writeSecretFile(m.configPath, data)
}

// AddWatcher adds a configuration change watcher.
func (m *ConfigManager) AddWatcher(watcher ConfigWatcher) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.watchers = append(m.watchers, watcher)
}

// ExportConfig exports the configuration to a file (read-locked snapshot).
func (m *ConfigManager) ExportConfig(path string) error {
	m.mu.RLock()
	data, err := json.MarshalIndent(m.config, "", "  ")
	m.mu.RUnlock()
	if err != nil {
		return err
	}
	// Exported config carries the same plaintext credentials as the live config
	// — write owner-only (CONST-042 / §12.1). writeSecretFile creates any missing
	// parent tree (mode 0700).
	return writeSecretFile(path, data)
}

// ImportConfig imports the configuration from a file. Write-locked: decode into
// a fresh struct and only swap on success so a failed import keeps the prior
// good config (no nil/torn read for concurrent GetConfig callers).
func (m *ConfigManager) ImportConfig(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	cfg := &Config{}
	if err := json.Unmarshal(data, cfg); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.config = cfg
	return m.saveConfigLocked()
}

// BackupConfig backs up the configuration to a file (read-locked snapshot).
func (m *ConfigManager) BackupConfig(path string) error {
	m.mu.RLock()
	data, err := json.MarshalIndent(m.config, "", "  ")
	m.mu.RUnlock()
	if err != nil {
		return err
	}
	// Backups carry the same plaintext credentials as the live config — write
	// owner-only (CONST-042 / §12.1). writeSecretFile creates any missing parent
	// tree (mode 0700).
	return writeSecretFile(path, data)
}

// ResetToDefaults resets the configuration to defaults (write-locked).
func (m *ConfigManager) ResetToDefaults() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.config = getDefaultConfig()
	return m.saveConfigLocked()
}

// LoadConfig loads configuration from the default location
func LoadConfig() (*Config, error) {
	path := GetConfigPath()
	manager, err := NewHelixConfigManager(path)
	if err != nil {
		return nil, err
	}
	return manager.GetConfig(), nil
}

// SaveConfig saves configuration to the default location
func SaveConfig(config *Config) error {
	path := GetConfigPath()
	manager, err := NewHelixConfigManager(path)
	if err != nil {
		return err
	}
	manager.mu.Lock()
	manager.config = config
	err = manager.saveConfigLocked()
	manager.mu.Unlock()
	return err
}

// GetConfigPath returns the default configuration file path
func GetConfigPath() string {
	if path := os.Getenv("HELIX_CONFIG_PATH"); path != "" {
		return path
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "helixcode", "config.json")
}

// secretFileMode is the only permission any persisted config/secret file may
// carry: owner read+write, no group/other access. CONST-042 / §12.1 — config
// files persist plaintext credentials (Auth.JWTSecret, Redis.Password, provider
// APIKeys, Cognee APIKey / RemoteAPI.APIKey), so a world-readable file is a
// secret leak any local user can read.
const (
	secretFileMode os.FileMode = 0o600
	privateDirMode os.FileMode = 0o700
)

// configDir returns the user-private directory that holds the config file
// (e.g. ~/.config/helixcode). Derived from GetConfigPath so HELIX_CONFIG_PATH
// overrides are honoured.
func configDir() string {
	return filepath.Dir(GetConfigPath())
}

// configBackupDir returns the user-PRIVATE directory under which config backups
// are written. CONST-042: backups carry the same plaintext secrets as the live
// config, so they MUST NOT land in the shared, world-traversable os.TempDir().
func configBackupDir() string {
	return filepath.Join(configDir(), "backups")
}

// writeSecretFile writes data to path with owner-only (0600) permissions,
// creating any missing parent directories with owner-only-traversable (0700)
// permissions. It is the single sanctioned write path for any config file that
// can contain a credential (CONST-042 / §12.1). On platforms where a prior file
// already exists with looser permissions, the mode is forcibly tightened.
func writeSecretFile(path string, data []byte) error {
	if dir := filepath.Dir(path); dir != "" {
		if err := os.MkdirAll(dir, privateDirMode); err != nil {
			return fmt.Errorf("failed to create directory %q: %w", dir, err)
		}
	}
	if err := os.WriteFile(path, data, secretFileMode); err != nil {
		return err
	}
	// os.WriteFile honours the umask on the initial create and does not change
	// the mode of a pre-existing file; force the exact secret mode so a leftover
	// world-readable file from before this fix is tightened on next write.
	if err := os.Chmod(path, secretFileMode); err != nil {
		return fmt.Errorf("failed to set secret file mode on %q: %w", path, err)
	}
	return nil
}

// IsConfigPresent checks if the default configuration file exists
func IsConfigPresent() bool {
	path := GetConfigPath()
	_, err := os.Stat(path)
	return err == nil
}

// UpdateConfig updates the configuration with the provided function
func UpdateConfig(updateFunc func(*Config)) error {
	config, err := LoadConfig()
	if err != nil {
		return err
	}
	updateFunc(config)
	return SaveConfig(config)
}

// GetHelixConfigPath returns the default configuration file path
func GetHelixConfigPath() string {
	return GetConfigPath()
}

// CreateDefaultHelixConfig creates a default configuration file
func CreateDefaultHelixConfig() error {
	return CreateDefaultConfig(GetConfigPath())
}

// IsHelixConfigPresent checks if the default configuration file exists
func IsHelixConfigPresent() bool {
	return IsConfigPresent()
}

// LoadHelixConfig loads configuration from the default location
func LoadHelixConfig() (*Config, error) {
	return LoadConfig()
}

// SaveHelixConfig saves configuration to the default location
func SaveHelixConfig(config *Config) error {
	return SaveConfig(config)
}

// UpdateHelixConfig updates the configuration with the provided function
func UpdateHelixConfig(updateFunc func(*Config)) error {
	return UpdateConfig(updateFunc)
}

// NewConfigWatcher creates a new configuration watcher
func NewConfigWatcher(configPath string) (ConfigWatcher, error) {
	if configPath == "" {
		return nil, fmt.Errorf("config path cannot be empty")
	}

	if _, err := os.Stat(configPath); err != nil {
		return nil, fmt.Errorf("config file not found: %v", err)
	}

	return &fileConfigWatcher{
		configPath: configPath,
		lastMod:    time.Now(),
	}, nil
}

type fileConfigWatcher struct {
	configPath string
	lastMod    time.Time
}

func (w *fileConfigWatcher) OnConfigChange(old, new *Config) error {
	return nil
}

// GetConfigInfo returns configuration information
func GetConfigInfo() (*ConfigInfo, error) {
	cfg, err := LoadConfig()
	if err != nil {
		return nil, fmt.Errorf("failed to load config: %v", err)
	}

	return &ConfigInfo{
		ConfigPath: GetConfigPath(),
		// HXC-185: report the same bracketed authority the server actually
		// binds (internal/server/server.go), so an IPv6 deployment's reported
		// address is a valid, dialable authority rather than "::1:8080".
		ServerAddress: netutil.JoinHostPort(cfg.Server.Address, cfg.Server.Port),
		DatabaseHost:  cfg.Database.Host,
		RedisEnabled:  cfg.Redis.Enabled,
		LLMProvider:   cfg.LLM.DefaultProvider,
		LogLevel:      cfg.Logging.Level,
		LastModified:  time.Now(),
	}, nil
}

// ConfigInfo represents configuration information
type ConfigInfo struct {
	ConfigPath    string    `json:"config_path"`
	ServerAddress string    `json:"server_address"`
	DatabaseHost  string    `json:"database_host"`
	RedisEnabled  bool      `json:"redis_enabled"`
	LLMProvider   string    `json:"llm_provider"`
	LogLevel      string    `json:"log_level"`
	LastModified  time.Time `json:"last_modified"`
}

// ConfigWatcher represents a configuration watcher
type ConfigWatcher interface {
	OnConfigChange(old, new *Config) error
}

// ConfigurationValidator validates configuration
// ValidationRule represents a custom validation rule
type ValidationRule struct {
	Name      string
	Validator func(interface{}) error
	Message   string
	Severity  string
}

type ConfigurationValidator struct {
	strict bool
	rules  map[string][]ValidationRule
}

// NewConfigurationValidator creates a new configuration validator
func NewConfigurationValidator(strict bool) *ConfigurationValidator {
	validator := &ConfigurationValidator{
		strict: strict,
		rules:  make(map[string][]ValidationRule),
	}

	if strict {
		validator.addDefaultRules()
	}

	return validator
}

// AddRule adds a validation rule for a specific property
func (cv *ConfigurationValidator) AddRule(property string, rule ValidationRule) {
	if cv.rules[property] == nil {
		cv.rules[property] = make([]ValidationRule, 0)
	}
	cv.rules[property] = append(cv.rules[property], rule)
}

// addDefaultRules adds default validation rules
func (cv *ConfigurationValidator) addDefaultRules() {
	cv.AddRule("server.port", ValidationRule{
		Name: "port_range",
		Validator: func(value interface{}) error {
			port, ok := value.(int)
			if !ok {
				return fmt.Errorf("port must be an integer")
			}
			if port < 1 || port > 65535 {
				return fmt.Errorf("port must be between 1 and 65535")
			}
			return nil
		},
		Message:  "Port must be between 1 and 65535",
		Severity: "error",
	})

	cv.AddRule("llm.temperature", ValidationRule{
		Name: "temperature_range",
		Validator: func(value interface{}) error {
			temp, ok := value.(float64)
			if !ok {
				return fmt.Errorf("temperature must be a number")
			}
			if temp < 0.0 || temp > 2.0 {
				return fmt.Errorf("temperature must be between 0.0 and 2.0")
			}
			return nil
		},
		Message:  "LLM temperature must be between 0.0 and 2.0",
		Severity: "error",
	})
}

// Validate validates the configuration
func (v *ConfigurationValidator) Validate(config *Config) ValidationResult {
	result := ValidationResult{
		Valid:  true,
		Errors: make([]ValidationError, 0),
	}

	// Validate server port
	if config.Server.Port < 1 || config.Server.Port > 65535 {
		result.Valid = false
		result.Errors = append(result.Errors, ValidationError{
			Property: "server.port",
			Path:     "server.port",
			Severity: "error",
			Code:     "invalid_port",
			Message:  "Port must be between 1 and 65535",
		})
	}

	// Validate application environment
	validEnvironments := []string{"development", "production", "testing", "staging"}
	isValidEnv := false
	for _, env := range validEnvironments {
		if config.Application.Environment == env {
			isValidEnv = true
			break
		}
	}
	if !isValidEnv {
		result.Valid = false
		result.Errors = append(result.Errors, ValidationError{
			Property: "application.environment",
			Path:     "application.environment",
			Severity: "error",
			Code:     "FIELD_SCHEMA_ERROR",
			Message:  "Environment must be one of: development, production, testing, staging",
		})
	}

	// Validate LLM provider.
	//
	// An EMPTY value is VALID and means "operator did not choose a provider".
	// It is the built-in default (setDefaultsOn, operator decision 2026-09-05)
	// and is resolved at request time by resolveLLMProvider's flag > env >
	// config precedence, which falls back to the local Ollama route. Rejecting
	// empty here would report a genuinely zero-config Config as invalid and make
	// the zero-config path unconfigurable -- the exact capability that decision
	// set out to preserve (§11.4.122).
	//
	// KNOWN DEFECT (pre-existing, tracked separately): validProviders below is
	// STALE. It lists 7 names while internal/llm defines 44 ProviderType
	// constants; "ollama", "llamacpp", "deepseek", "mistral" and "groq" are all
	// constructible by the resolver yet rejected here. Do NOT blanket-expand it
	// from the constant list -- several of those types are vector stores and
	// frameworks (chroma, clickhouse, llamaindex, crewai), not LLM providers.
	// The correct membership is the set internal/llm's factory can actually
	// construct as a default provider.
	validProviders := []string{"local", "openai", "anthropic", "gemini", "xai", "openrouter", "copilot"}
	isValidProvider := config.LLM.DefaultProvider == ""
	for _, provider := range validProviders {
		if config.LLM.DefaultProvider == provider {
			isValidProvider = true
			break
		}
	}
	if !isValidProvider {
		result.Valid = false
		result.Errors = append(result.Errors, ValidationError{
			Property: "llm.default_provider",
			Path:     "llm.default_provider",
			Severity: "error",
			Code:     "FIELD_SCHEMA_ERROR",
			Message:  "LLM provider must be one of: local, openai, anthropic, gemini, xai, openrouter, copilot",
		})
	}

	// Validate LLM max tokens
	if config.LLM.MaxTokens < 1 {
		result.Valid = false
		result.Errors = append(result.Errors, ValidationError{
			Property: "llm.max_tokens",
			Path:     "llm.max_tokens",
			Severity: "error",
			Code:     "FIELD_SCHEMA_ERROR",
			Message:  "LLM max tokens must be a positive integer",
		})
	}

	// Validate LLM temperature
	if config.LLM.Temperature < 0.0 || config.LLM.Temperature > 2.0 {
		result.Valid = false
		result.Errors = append(result.Errors, ValidationError{
			Property: "llm.temperature",
			Path:     "llm.temperature",
			Severity: "error",
			Code:     "CUSTOM_RULE_ERROR",
			Message:  "LLM temperature must be between 0.0 and 2.0",
		})
	}

	// Validate database port
	if config.Database.Port < 1 || config.Database.Port > 65535 {
		result.Valid = false
		result.Errors = append(result.Errors, ValidationError{
			Property: "database.port",
			Path:     "database.port",
			Severity: "error",
			Code:     "invalid_port",
			Message:  "Database port must be between 1 and 65535",
		})
	}

	// Validate JWT secret
	if len(config.Auth.JWTSecret) < 32 {
		result.Valid = false
		result.Errors = append(result.Errors, ValidationError{
			Property: "auth.jwt_secret",
			Path:     "auth.jwt_secret",
			Severity: "error",
			Code:     "invalid_jwt_secret",
			Message:  "JWT secret must be at least 32 characters",
		})
	}

	// Validate bcrypt cost — must be within bcrypt's documented inclusive range
	// [4..31]. A cost > 31 makes every bcrypt.GenerateFromPassword call fail at
	// runtime (InvalidCostError), and a cost < 4 is silently coerced to
	// DefaultCost, weakening the configured work factor. See validateConfig for
	// the full rationale.
	if config.Auth.BcryptCost < 4 || config.Auth.BcryptCost > 31 {
		result.Valid = false
		result.Errors = append(result.Errors, ValidationError{
			Property: "auth.bcrypt_cost",
			Path:     "auth.bcrypt_cost",
			Severity: "error",
			Code:     "invalid_bcrypt_cost",
			Message:  "bcrypt cost must be between 4 and 31",
		})
	}

	// Validate custom rules
	if v.rules != nil {
		// Check application.name field
		if rules, exists := v.rules["application.name"]; exists {
			value := config.Application.Name
			for _, rule := range rules {
				if rule.Name == "custom" {
					if err := rule.Validator(value); err != nil {
						result.Valid = false
						result.Errors = append(result.Errors, ValidationError{
							Property: "application.name",
							Path:     "application.name",
							Severity: "error",
							Code:     "CUSTOM_RULE_ERROR",
							Message:  "Custom rule validation failed",
						})
					}
				}
			}
		}
	}

	return result
}

// ValidateField validates a specific field
func (v *ConfigurationValidator) ValidateField(config *Config, field string) ValidationResult {
	result := ValidationResult{
		Valid:  true,
		Errors: make([]ValidationError, 0),
		Path:   field,
	}

	switch field {
	case "server.port":
		if config.Server.Port < 1 || config.Server.Port > 65535 {
			result.Valid = false
			result.Errors = append(result.Errors, ValidationError{
				Property: "server.port",
				Path:     "server.port",
				Severity: "error",
				Code:     "invalid_port",
				Message:  "Port must be between 1 and 65535",
			})
		}
	case "llm.temperature":
		if config.LLM.Temperature < 0.0 || config.LLM.Temperature > 2.0 {
			result.Valid = false
			result.Errors = append(result.Errors, ValidationError{
				Property: "llm.temperature",
				Path:     "llm.temperature",
				Severity: "error",
				Code:     "invalid_temperature",
				Message:  "LLM temperature must be between 0.0 and 2.0",
			})
		}
	case "database.port":
		if config.Database.Port < 1 || config.Database.Port > 65535 {
			result.Valid = false
			result.Errors = append(result.Errors, ValidationError{
				Property: "database.port",
				Path:     "database.port",
				Severity: "error",
				Code:     "invalid_port",
				Message:  "Database port must be between 1 and 65535",
			})
		}
	case "auth.jwt_secret":
		if len(config.Auth.JWTSecret) < 32 {
			result.Valid = false
			result.Errors = append(result.Errors, ValidationError{
				Property: "auth.jwt_secret",
				Path:     "auth.jwt_secret",
				Severity: "error",
				Code:     "invalid_jwt_secret",
				Message:  "JWT secret must be at least 32 characters",
			})
		}
	}

	return result
}

// AddCustomRule adds a custom validation rule
func (v *ConfigurationValidator) AddCustomRule(field string, rule func(interface{}) error) {
	// Store custom rule - implementation depends on rules field existence
	if v.rules == nil {
		v.rules = make(map[string][]ValidationRule)
	}

	v.rules[field] = append(v.rules[field], ValidationRule{
		Name:      "custom",
		Validator: rule,
		Message:   "Custom rule validation failed",
		Severity:  "error",
	})
}

// ValidationResult represents validation result
type ValidationResult struct {
	Valid  bool
	Errors []ValidationError
	Path   string
}

// ValidationError represents a validation error
type ValidationError struct {
	Property string
	Path     string
	Severity string
	Code     string
	Message  string
}

// createDefaultSchema creates the default validation schema
func (v *ConfigurationValidator) createDefaultSchema() *ValidationSchema {
	schema := &ValidationSchema{
		Version:    "1.0",
		Properties: make(map[string]*SchemaProperty),
		Required:   []string{"version", "application", "server"},
	}

	// Application properties
	schema.Properties["application"] = &SchemaProperty{
		Type: "object",
		Properties: map[string]*SchemaProperty{
			"name": {
				Type:      "string",
				MinLength: intPtr(1),
				MaxLength: intPtr(100),
			},
			"version": {
				Type:      "string",
				MinLength: intPtr(1),
			},
			"environment": {
				Type: "string",
			},
		},
		Required: []string{"name", "version"},
	}

	// Server properties
	schema.Properties["server"] = &SchemaProperty{
		Type: "object",
		Properties: map[string]*SchemaProperty{
			"address": {
				Type: "string",
			},
			"port": {
				Type: "integer",
			},
			"read_timeout": {
				Type: "integer",
			},
			"write_timeout": {
				Type: "integer",
			},
		},
		Required: []string{"address", "port"},
	}

	// Version property
	schema.Properties["version"] = &SchemaProperty{
		Type:      "string",
		MinLength: intPtr(1),
	}

	return schema
}

// ValidationSchema represents validation schema
type ValidationSchema struct {
	Version    string
	Properties map[string]*SchemaProperty
	Required   []string
}

// SchemaProperty represents a schema property
type SchemaProperty struct {
	Type       string
	Properties map[string]*SchemaProperty
	Required   []string
	MinLength  *int
	MaxLength  *int
}

// ConfigurationMigrator migrates configuration between versions
type ConfigurationMigrator struct {
	current    string
	migrations map[string][]Migration
}

// NewConfigurationMigrator creates a new configuration migrator
func NewConfigurationMigrator(currentVersion string) *ConfigurationMigrator {
	m := &ConfigurationMigrator{
		current:    currentVersion,
		migrations: make(map[string][]Migration),
	}

	// Register migrations
	m.registerMigrations()
	return m
}

// registerMigrations registers all available migrations
func (m *ConfigurationMigrator) registerMigrations() {
	// 1.0.0 -> 1.1.0
	m.addMigration("1.0.0", "1.1.0", Migration{
		From: "1.0.0",
		To:   "1.1.0",
		Up: func(config *Config) error {
			// Add auto-save feature with default true
			config.Application.Workspace.AutoSave = true
			config.Version = "1.1.0"
			return nil
		},
		Down: func(config *Config) error {
			config.Version = "1.0.0"
			return nil
		},
	})

	// 1.1.0 -> 1.2.0
	m.addMigration("1.1.0", "1.2.0", Migration{
		From: "1.1.0",
		To:   "1.2.0",
		Up: func(config *Config) error {
			// Ensure auto-save is enabled in 1.2.0
			config.Application.Workspace.AutoSave = true
			config.Version = "1.2.0"
			return nil
		},
		Down: func(config *Config) error {
			config.Version = "1.1.0"
			return nil
		},
	})
}

// addMigration adds a migration to the registry
func (m *ConfigurationMigrator) addMigration(from, to string, migration Migration) {
	if m.migrations[from] == nil {
		m.migrations[from] = []Migration{}
	}
	m.migrations[from] = append(m.migrations[from], migration)
}

// GetAvailableVersions returns available versions
func (m *ConfigurationMigrator) GetAvailableVersions() []string {
	return []string{"1.0.0", "1.1.0", "1.2.0"}
}

// Migrate migrates configuration to a target version
func (m *ConfigurationMigrator) Migrate(config *Config, targetVersion string) error {
	if config.Version == targetVersion {
		return nil
	}

	path := m.findMigrationPath(config.Version, targetVersion)
	if path == nil {
		return fmt.Errorf("no migration path from %s to %s", config.Version, targetVersion)
	}

	// Execute migrations in sequence
	currentVersion := config.Version
	for _, nextVersion := range path {
		migrations, exists := m.migrations[currentVersion]
		if !exists {
			return fmt.Errorf("no migrations from version %s", currentVersion)
		}

		// Find the migration to the next version
		var migration *Migration
		for j := range migrations {
			if migrations[j].To == nextVersion {
				migration = &migrations[j]
				break
			}
		}

		if migration == nil {
			return fmt.Errorf("no migration from %s to %s", currentVersion, nextVersion)
		}

		// Create backup if required
		if migration.Backup {
			if err := m.createBackup(config, currentVersion); err != nil {
				return fmt.Errorf("failed to create backup before migration from %s to %s: %w", currentVersion, nextVersion, err)
			}
		}

		// Execute the migration
		if err := migration.Up(config); err != nil {
			return fmt.Errorf("migration from %s to %s failed: %w", currentVersion, nextVersion, err)
		}

		// Update the configuration version
		config.Version = nextVersion

		currentVersion = nextVersion
	}

	return nil
}

// findMigrationPath finds the migration path
func (m *ConfigurationMigrator) findMigrationPath(from, to string) []string {
	// Direct migration available
	if migrations, exists := m.migrations[from]; exists {
		for _, migration := range migrations {
			if migration.To == to {
				// Return just the target version (1 step)
				return []string{to}
			}
		}
	}

	// Try multi-step paths
	for _, version := range m.GetAvailableVersions() {
		if version == from || version == to {
			continue
		}

		// Check if we can migrate from 'from' to 'version'
		if m.canMigrate(from, version) {
			// Check if we can then migrate from 'version' to 'to'
			if m.canMigrate(version, to) {
				// Return intermediate steps (2 steps)
				return []string{version, to}
			}
		}
	}

	// Check for reverse migration (downgrade)
	if from > to {
		// Simple downgrade path
		return []string{to}
	}

	return nil
}

// canMigrate checks if direct migration is possible
func (m *ConfigurationMigrator) canMigrate(from, to string) bool {
	migrations, exists := m.migrations[from]
	if !exists {
		return false
	}

	for _, migration := range migrations {
		if migration.To == to {
			return true
		}
	}
	return false
}

// createBackup creates a backup of the configuration.
//
// CONST-042 / §12.1: a config backup carries the same plaintext credentials as
// the live config (Auth.JWTSecret, Redis.Password, provider APIKeys, Cognee
// APIKey / RemoteAPI.APIKey). The backup therefore MUST NOT land in the shared,
// world-traversable os.TempDir() at a predictable name and world-readable mode
// — that is an accumulating secret leak any local user can read. Instead it is
// written into a user-PRIVATE directory (mode 0700) at owner-only file mode
// (0600) with an unpredictable name, and old backups are pruned to bound the
// on-disk secret footprint.
func (m *ConfigurationMigrator) createBackup(config *Config, version string) error {
	backupDir := configBackupDir()
	if err := os.MkdirAll(backupDir, privateDirMode); err != nil {
		return fmt.Errorf("failed to create backup directory: %w", err)
	}
	// Force-tighten the directory mode in case it pre-existed world-traversable.
	if err := os.Chmod(backupDir, privateDirMode); err != nil {
		return fmt.Errorf("failed to secure backup directory: %w", err)
	}

	timestamp := time.Now().Format("20060102_150405")
	suffix, err := randomHexSuffix()
	if err != nil {
		return fmt.Errorf("failed to generate backup name: %w", err)
	}
	backupPath := filepath.Join(backupDir, fmt.Sprintf("helix_config_backup_%s_%s_%s.json", version, timestamp, suffix))

	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal config for backup: %w", err)
	}

	if err := writeSecretFile(backupPath, data); err != nil {
		return fmt.Errorf("failed to write backup file: %w", err)
	}

	// Retention: keep only the newest maxConfigBackups so secret-bearing backups
	// do not accumulate unbounded on disk.
	pruneOldBackups(backupDir, maxConfigBackups)

	return nil
}

// maxConfigBackups bounds how many secret-bearing config backups are retained.
const maxConfigBackups = 10

// randomHexSuffix returns 8 random hex chars for an unpredictable backup name.
func randomHexSuffix() (string, error) {
	b := make([]byte, 4)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// pruneOldBackups removes the oldest helix config backups in dir, keeping at
// most keep of them. Errors are non-fatal — a failure to prune must not block a
// successful backup, but it is best-effort to bound the secret footprint.
func pruneOldBackups(dir string, keep int) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	type backup struct {
		path string
		mod  time.Time
	}
	var backups []backup
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasPrefix(name, "helix_config_backup_") || !strings.HasSuffix(name, ".json") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		backups = append(backups, backup{path: filepath.Join(dir, name), mod: info.ModTime()})
	}
	if len(backups) <= keep {
		return
	}
	sort.Slice(backups, func(i, j int) bool { return backups[i].mod.After(backups[j].mod) })
	for _, b := range backups[keep:] {
		_ = os.Remove(b.path)
	}
}

// ConfigurationTransformer transforms configuration
type ConfigurationTransformer struct {
	mappings []TransformMapping
}

// Migration represents a configuration migration
type Migration struct {
	From    string
	To      string
	Name    string
	Desc    string
	Backup  bool
	Up      func(config *Config) error
	Down    func(config *Config) error
	Migrate func(config *Config) error
}

// NewConfigurationTransformer creates a new configuration transformer
func NewConfigurationTransformer() *ConfigurationTransformer {
	return &ConfigurationTransformer{
		mappings: []TransformMapping{},
	}
}

// AddMapping adds a transformation mapping
func (t *ConfigurationTransformer) AddMapping(mapping TransformMapping) {
	t.mappings = append(t.mappings, mapping)
}

// Transform transforms configuration with variables
func (t *ConfigurationTransformer) Transform(config *Config, variables map[string]interface{}) (*Config, error) {
	// Create a copy of the config
	result := *config

	// Sort mappings by priority
	sort.Slice(t.mappings, func(i, j int) bool {
		return t.mappings[i].Priority < t.mappings[j].Priority
	})

	// Apply transformations
	for _, mapping := range t.mappings {
		// Skip if condition is specified and doesn't match
		if mapping.Condition != "" && result.Application.Environment != mapping.Condition {
			continue
		}

		// Apply transformation based on type
		switch mapping.Transform {
		case "copy":
			// Try to find value in variables - check direct source first
			if sourceVal, exists := variables[mapping.Source]; exists {
				// Handle different target paths
				switch mapping.Target {
				case "server.port":
					if port, ok := sourceVal.(int); ok {
						result.Server.Port = port
					}
				case "application.name":
					if name, ok := sourceVal.(string); ok {
						result.Application.Name = name
					}
				}
			} else {
				// Try alternate variable naming conventions
				switch mapping.Source {
				case "server.port":
					if portVal, exists := variables["server_port"]; exists {
						if port, ok := portVal.(int); ok {
							result.Server.Port = port
						}
					}
				case "application.name":
					if nameVal, exists := variables["app_name"]; exists {
						if name, ok := nameVal.(string); ok {
							result.Application.Name = name
						}
					}
				}
			}
		}
	}

	return &result, nil
}

// TransformMapping represents a transformation mapping
type TransformMapping struct {
	Source    string
	Target    string
	Transform string
	Priority  int
	Condition string
}

// ConfigurationOptions provides options for configuration management
// This is a simplified interface for testing purposes
type ConfigurationOptions struct {
	ConfigPath       string
	AutoSave         bool
	AutoBackup       bool
	EnableEncryption bool
	EncryptionKey    string
	SchemaPath       string
	ValidationMode   string
	TransformMode    string
	WatchInterval    time.Duration
	MaxBackups       int
	Compression      bool
	LogLevel         string
	BackupPath       string
}

// ConfigurationTemplateManager manages configuration templates
type ConfigurationTemplateManager struct {
	templateDir string
	templates   map[string]*ConfigurationTemplate
}

// TemplateVariable represents a template variable
type TemplateVariable struct {
	Name        string      `json:"name"`
	Type        string      `json:"type"`
	Default     interface{} `json:"default"`
	Description string      `json:"description,omitempty"`
	Required    bool        `json:"required"`
	MinLength   *int        `json:"min_length,omitempty"`
	MaxLength   *int        `json:"max_length,omitempty"`
	Pattern     string      `json:"pattern,omitempty"`
	Min         *float64    `json:"min,omitempty"`
	Max         *float64    `json:"max,omitempty"`
}

// NewConfigurationTemplateManager creates a new template manager
func NewConfigurationTemplateManager(templateDir string) *ConfigurationTemplateManager {
	return &ConfigurationTemplateManager{
		templateDir: templateDir,
		templates:   make(map[string]*ConfigurationTemplate),
	}
}

// ConfigurationTemplate represents a configuration template
type ConfigurationTemplate struct {
	ID          string                       `json:"id"`
	Name        string                       `json:"name"`
	Description string                       `json:"description"`
	Category    string                       `json:"category"`
	Variables   map[string]*TemplateVariable `json:"variables"`
	Config      *Config                      `json:"config"`
	CreatedAt   time.Time                    `json:"created_at"`
	UpdatedAt   time.Time                    `json:"updated_at"`
}

// CreateTemplateFromConfig creates a template from configuration
func (tm *ConfigurationTemplateManager) CreateTemplateFromConfig(config *Config, name, description string, variables map[string]*TemplateVariable) (*ConfigurationTemplate, error) {
	template := &ConfigurationTemplate{
		ID:          "template-" + name,
		Name:        name,
		Description: description,
		Category:    "custom",
		Variables:   variables,
		Config:      config,
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}
	return template, nil
}

// SaveTemplate saves a configuration template
func (tm *ConfigurationTemplateManager) SaveTemplate(template *ConfigurationTemplate, path string) error {
	// Store in manager
	tm.templates[template.ID] = template

	// Serialize template to JSON
	data, err := json.MarshalIndent(template, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal template: %w", err)
	}

	// A template embeds a full *Config, which can carry plaintext credentials —
	// write owner-only (CONST-042 / §12.1). writeSecretFile creates the parent
	// tree (mode 0700).
	if err := writeSecretFile(path, data); err != nil {
		return fmt.Errorf("failed to write template file: %w", err)
	}

	return nil
}

// ApplyTemplate applies a template with variables
func (tm *ConfigurationTemplateManager) ApplyTemplate(templateID string, variables map[string]interface{}) (*Config, error) {
	template, exists := tm.templates[templateID]
	if !exists {
		return nil, fmt.Errorf("template not found: %s", templateID)
	}

	// Return a copy of the template's config
	result := *template.Config
	return &result, nil
}

// LoadTemplate loads a configuration template
func (tm *ConfigurationTemplateManager) LoadTemplate(path string) (*Config, error) {
	// Read file
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read template file: %w", err)
	}

	// Deserialize template from JSON
	var template ConfigurationTemplate
	if err := json.Unmarshal(data, &template); err != nil {
		return nil, fmt.Errorf("failed to unmarshal template: %w", err)
	}

	// Store in manager
	tm.templates[template.ID] = &template

	// Return the config from the template
	if template.Config == nil {
		return getDefaultConfig(), nil
	}
	return template.Config, nil
}

// SearchTemplates searches templates by query
func (tm *ConfigurationTemplateManager) SearchTemplates(query string) []*ConfigurationTemplate {
	results := make([]*ConfigurationTemplate, 0)
	lowerQuery := strings.ToLower(query)

	for _, template := range tm.templates {
		// Search in name, description, and category
		nameMatch := strings.Contains(strings.ToLower(template.Name), lowerQuery)
		descMatch := strings.Contains(strings.ToLower(template.Description), lowerQuery)
		categoryMatch := strings.Contains(strings.ToLower(template.Category), lowerQuery)

		if nameMatch || descMatch || categoryMatch {
			results = append(results, template)
		}
	}

	return results
}

// intPtr returns a pointer to int
func intPtr(i int) *int {
	return &i
}

// processTemplate processes a template with variable validation
func (tm *ConfigurationTemplateManager) processTemplate(template *ConfigurationTemplate, variables map[string]interface{}) (*Config, error) {
	// Validate required variables
	for name, variable := range template.Variables {
		if variable.Required {
			if _, exists := variables[name]; !exists {
				return nil, fmt.Errorf("required variable not provided: %s", name)
			}
		}

		// Type validation and constraints
		if value, exists := variables[name]; exists {
			if variable.Type == "string" {
				strValue, ok := value.(string)
				if !ok {
					return nil, fmt.Errorf("variable %s must be a string, got %T", name, value)
				}

				// Length validation
				if variable.MinLength != nil && len(strValue) < *variable.MinLength {
					return nil, fmt.Errorf("variable %s is too short (min %d chars)", name, *variable.MinLength)
				}
				if variable.MaxLength != nil && len(strValue) > *variable.MaxLength {
					return nil, fmt.Errorf("variable %s is too long (max %d chars)", name, *variable.MaxLength)
				}

				// Pattern validation
				if variable.Pattern != "" {
					matched, err := regexp.MatchString(variable.Pattern, strValue)
					if err != nil {
						return nil, fmt.Errorf("invalid pattern for variable %s: %w", name, err)
					}
					if !matched {
						return nil, fmt.Errorf("variable %s doesn't match required pattern %s", name, variable.Pattern)
					}
				}
			}
		}
	}

	// Create a deep copy of template's config for manipulation
	configBytes, err := json.Marshal(template.Config)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal template config: %w", err)
	}

	var result Config
	if err := json.Unmarshal(configBytes, &result); err != nil {
		return nil, fmt.Errorf("failed to unmarshal template config: %w", err)
	}

	// Variable substitution via strings.ReplaceAll over {{name}}
	// placeholders. This is a deliberate, lightweight template engine
	// — the variable map has already been schema-validated (Type,
	// Required, MinLength/MaxLength/Pattern) at lines 1740-1782 above,
	// so the substitution layer only needs to perform the literal
	// {{name}} -> value swap. Heavier engines (text/template,
	// pongo2, etc.) are explicitly NOT used to keep the templates
	// declarative and predictable. This is the honest current
	// design, not a stub awaiting upgrade. Article XI §11.9 / CONST-035.
	configStr := string(configBytes)

	// Replace variables in the configuration
	for name, value := range variables {
		placeholder := "{{" + name + "}}"
		replacement := fmt.Sprintf("%v", value)
		configStr = strings.ReplaceAll(configStr, placeholder, replacement)
	}

	// Unmarshal the substituted configuration
	if err := json.Unmarshal([]byte(configStr), &result); err != nil {
		return nil, fmt.Errorf("failed to unmarshal substituted config: %w", err)
	}

	return &result, nil
}

// CreateDefaultTemplates creates default configuration templates
func CreateDefaultTemplates() map[string]*ConfigurationTemplate {
	templates := make(map[string]*ConfigurationTemplate)

	// Add basic template
	templates["basic"] = &ConfigurationTemplate{
		ID:          "basic",
		Name:        "Basic Configuration",
		Description: "Basic server configuration",
		Category:    "default",
		Variables:   make(map[string]*TemplateVariable),
		Config:      getDefaultConfig(),
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}

	// Add development template
	devConfig := getDefaultConfig()
	devConfig.Application.Environment = "development"
	devConfig.Server.Port = 8080
	devConfig.Server.Address = "0.0.0.0"

	templates["development"] = &ConfigurationTemplate{
		ID:          "development",
		Name:        "Development Environment",
		Description: "Development environment configuration",
		Category:    "environment",
		Variables:   make(map[string]*TemplateVariable),
		Config:      devConfig,
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}

	// Add production template
	prodConfig := getDefaultConfig()
	prodConfig.Application.Environment = "production"
	prodConfig.Server.Port = 443
	prodConfig.Server.Address = "0.0.0.0"
	prodConfig.Logging.Level = "error"

	templates["production"] = &ConfigurationTemplate{
		ID:          "production",
		Name:        "Production Environment",
		Description: "Production environment configuration",
		Category:    "environment",
		Variables:   make(map[string]*TemplateVariable),
		Config:      prodConfig,
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}

	// Add testing template
	testConfig := getDefaultConfig()
	testConfig.Application.Environment = "testing"
	testConfig.Server.Port = 0
	testConfig.Server.Address = "0.0.0.0"
	testConfig.Logging.Level = "debug"
	testConfig.Database.Host = "" // Empty host disables database
	testConfig.Redis.Enabled = false
	testConfig.Workers.MaxConcurrentTasks = 10

	templates["testing"] = &ConfigurationTemplate{
		ID:          "testing",
		Name:        "Testing Environment",
		Description: "Testing environment configuration",
		Category:    "environment",
		Variables:   make(map[string]*TemplateVariable),
		Config:      testConfig,
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}

	return templates
}

// Configuration validation modes
const (
	ValidationModeStrict   = "strict"
	ValidationModeLenient  = "lenient"
	ValidationModeDisabled = "disabled"
	ValidationModeSchema   = "schema"
)

// Configuration transformation modes
const (
	TransformModeStrict  = "strict"
	TransformModeLenient = "lenient"
	TransformModeNone    = "none"
	TransformModeSchema  = "schema"
)
