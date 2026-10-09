package config

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// shippedConfig is the config file the application actually ships and loads.
const shippedConfig = "../../config/config.yaml"

// configShapedFiles are the files under config/ that are meant to unmarshal
// into the Config struct, and are therefore in scope for the strict key check.
//
// Deliberately NOT listed:
//   - model-aliases.example.yaml — a different schema entirely (top-level
//     `aliases` / `fuzzy_threshold`), loaded by its own reader, not by Load().
//
// production-config.yaml WAS excluded here for not being valid YAML at all: an
// AI-assistant transcript had been pasted into it from line 569 ("Now let me
// create the Phase 3 test implementation:<function_calls>..."), so no parser
// could read it. It is now repaired and in scope. The repair was deliberately
// the part that needed no judgement: the transcript tail was deleted, and six
// duplicate top-level blocks that YAML was ALREADY discarding (only the last
// occurrence of a repeated key survives) were removed, proven by the parsed
// configuration being byte-identical before and after. What the file's 22
// unread keys should BECOME is still an operator call; they are registered in
// inertConfigKeys so startup states plainly that they do nothing.
var configShapedFiles = []string{
	"production-config.yaml",
	"config.yaml",
	"fixed-config.yaml",
	"minimal-config.yaml",
	"minimal-test-config.yaml",
	"replica-8081.yaml",
	"replica-8082.yaml",
	"test-config.yaml",
	"working-config.yaml",
}

// TestShippedConfigPassesStrictKeyCheck is the guard that keeps this feature
// honest: a strict loader that rejects the application's own config file is
// not a fix, it is an outage. Every Config-shaped file under config/ must load
// without a hard error.
func TestShippedConfigPassesStrictKeyCheck(t *testing.T) {
	for _, name := range configShapedFiles {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join("../../config", name)
			_, err := checkConfigKeys(path)
			require.NoError(t, err,
				"%s declares a config key that is neither known to the Config "+
					"struct nor registered in inertConfigKeys. Either add the "+
					"field, fix the key, or — if it is genuinely dead — register "+
					"it in inertConfigKeys with a reason.", name)
		})
	}
}

// TestShippedConfigLoadsEndToEnd exercises the real Load() path against the
// real shipped config, not just the key checker in isolation. This is what
// proves the strict check is wired in without breaking startup.
func TestShippedConfigLoadsEndToEnd(t *testing.T) {
	abs, err := filepath.Abs(shippedConfig)
	require.NoError(t, err)
	t.Setenv("HELIX_CONFIG", abs)

	// The shipped config marks these three "REQUIRED: Set via environment
	// variable" on the line itself, and startup now enforces that: a credential
	// left as an unexpanded ${...} is refused rather than used as the literal
	// value (see secret_placeholder.go).
	//
	// This test previously passed WITHOUT them, which is what the refusal
	// exists to stop -- Load() returned a config whose JWT signing key was the
	// string "${HELIX_AUTH_JWT_SECRET}", a constant committed to this
	// repository. The test was green because of the defect, not despite it.
	//
	// Supplying them keeps this test's actual claim intact -- that strict key
	// checking has not broken startup -- while no longer asserting a startup
	// that was never legitimate.
	t.Setenv("HELIX_DATABASE_PASSWORD", "test-database-password")
	t.Setenv("HELIX_REDIS_PASSWORD", "test-redis-password")
	t.Setenv("HELIX_AUTH_JWT_SECRET", "test-jwt-signing-secret-32-chars+")

	cfg, err := Load()
	require.NoError(t, err, "the application must still load its own config file")
	require.NotNil(t, cfg)
	require.Equal(t, "local", cfg.LLM.DefaultProvider, "config file values must still reach the struct")
}

// TestUnknownKeyIsRejected is the paired mutation for the whole feature: put a
// key nobody reads into the config and startup must fail, naming the key and
// the section it sits in.
func TestUnknownKeyIsRejected(t *testing.T) {
	tests := []struct {
		name string
		// anchor is an existing line in the shipped config that the injected
		// line is inserted directly after. Appending a duplicate `llm:` block
		// to the end of the file instead would NOT work: YAML keeps only one
		// mapping for a duplicated key, so the injected block is dropped
		// before the checker ever sees it and the test passes vacuously.
		// That exact mistake is why every case below asserts the injected key
		// is actually present in the parsed key set.
		anchor      string
		line        string
		wantKey     string
		wantSection string
	}{
		{
			name:        "typo_in_known_section",
			anchor:      "llm:",
			line:        "  temperture: 0.7",
			wantKey:     "llm.temperture",
			wantSection: "llm",
		},
		{
			name:        "unknown_top_level_section",
			anchor:      "logging:",
			line:        "  levl: \"info\"",
			wantKey:     "logging.levl",
			wantSection: "logging",
		},
		{
			name:        "plausible_but_unread_key",
			anchor:      "server:",
			line:        "  max_header_bytes: 8192",
			wantKey:     "server.max_header_bytes",
			wantSection: "server",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			original, err := os.ReadFile(shippedConfig)
			require.NoError(t, err)
			require.Contains(t, string(original), tc.anchor+"\n",
				"anchor %q must exist in the shipped config", tc.anchor)

			mutated := strings.Replace(string(original),
				tc.anchor+"\n", tc.anchor+"\n"+tc.line+"\n", 1)
			require.NotEqual(t, string(original), mutated,
				"mutation must actually change the file content")
			require.Equal(t, len(strings.Split(string(original), "\n"))+1,
				len(strings.Split(mutated, "\n")),
				"mutation must add exactly one line")

			path := filepath.Join(t.TempDir(), "config.yaml")
			require.NoError(t, os.WriteFile(path, []byte(mutated), 0o600))

			// Read it back — assert the file on disk really carries the
			// mutation, so a PASS here can never come from a no-op write.
			onDisk, err := os.ReadFile(path)
			require.NoError(t, err)
			require.Equal(t, mutated, string(onDisk))
			require.Contains(t, string(onDisk), tc.line)

			// Strongest form of "the mutation bit": the injected key must be
			// visible to the YAML parser, not just present as text.
			parsed, err := fileOnlyKeys(path)
			require.NoError(t, err)
			require.Contains(t, parsed, tc.wantKey,
				"injected key must actually reach the parsed key set")

			// The unmutated original must still be accepted, so the failure
			// below is attributable to the injected key and nothing else.
			_, err = checkConfigKeys(shippedConfig)
			require.NoError(t, err, "control: unmutated shipped config must pass")

			_, err = checkConfigKeys(path)
			require.Error(t, err, "unknown key %q must be rejected", tc.wantKey)
			require.Contains(t, err.Error(), tc.wantKey,
				"error must name the offending key")
			if tc.wantSection != "" {
				require.Contains(t, err.Error(), tc.wantSection,
					"error must name the section the key sits in")
			}

			// And the same failure must reach the caller through Load().
			t.Setenv("HELIX_CONFIG", path)
			_, err = Load()
			require.Error(t, err, "Load() must refuse a config with unknown keys")
			require.Contains(t, err.Error(), tc.wantKey)
		})
	}
}

// TestInertKeysWarnRatherThanFail pins the deliberate middle ground: keys
// registered in inertConfigKeys do not stop startup, but they are announced
// every load so nobody can mistake them for working configuration.
func TestInertKeysWarnRatherThanFail(t *testing.T) {
	warnings, err := checkConfigKeys(shippedConfig)
	require.NoError(t, err)

	joined := strings.Join(warnings, "\n")
	for _, key := range []string{
		"llm.providers",
		"llm.selection",
		"llm.timeout",
		"llm.max_retries",
		// `notifications` used to appear here as a whole-block entry: the
		// struct had no field for it, so every key under it was discarded.
		// The block is now declared and consumed, and what remains inert is
		// the handful of leaves no channel constructor takes an argument for.
		// Naming them individually keeps this assertion honest — the bare
		// string "notifications" would still match by substring while
		// asserting something that is no longer true.
		"notifications.channels.slack.timeout",
		"notifications.channels.telegram.timeout",
		"notifications.channels.email.timeout",
		"notifications.channels.discord.timeout",
		"notifications.channels.email.smtp.tls",
		"notifications.channels.email.recipients",
	} {
		require.Contains(t, joined, key,
			"the shipped config sets %q, which has no effect — it must be warned about", key)
	}

	for _, w := range warnings {
		require.Contains(t, w, "no effect",
			"a warning that does not say the key has no effect is not a warning")
	}
}

// TestInertKeysNotWarnedWhenAbsent — a registered inert key the operator never
// wrote is not their problem and must not be mentioned.
func TestInertKeysNotWarnedWhenAbsent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	require.NoError(t, os.WriteFile(path, []byte(
		"version: \"1.0.0\"\nserver:\n  port: 8080\n"), 0o600))

	warnings, err := checkConfigKeys(path)
	require.NoError(t, err)
	require.Empty(t, warnings)
}

// TestEveryInertKeyIsGenuinelyInert stops inertConfigKeys from being used as a
// dumping ground. An entry is only legitimate if the key is either (a) unknown
// to the Config struct, or (b) known but explicitly documented as unconsumed.
// Category (b) is a short, closed list that must shrink, never grow silently.
func TestEveryInertKeyIsGenuinelyInert(t *testing.T) {
	declaredButUnconsumed := map[string]bool{
		"llm.timeout":     true,
		"llm.max_retries": true,

		// The notifications leaves below arrived when the block-level
		// `notifications` entry was retired. That entry was category (a) —
		// unknown to the struct, whole block discarded — and wiring the block
		// up moved its residue into category (b). The list is longer by six
		// names but what is inert is strictly smaller: an entire block of
		// channels and rules that did nothing became six settings that do
		// nothing. Each shrinks further only by giving the channel
		// constructors an argument to receive it.
		"notifications.channels.slack.timeout":    true,
		"notifications.channels.telegram.timeout": true,
		"notifications.channels.email.timeout":    true,
		"notifications.channels.discord.timeout":  true,
		"notifications.channels.email.smtp.tls":   true,
		"notifications.channels.email.recipients": true,
	}

	spec := knownConfigKeys()
	for key := range inertConfigKeys {
		_, declared := spec[key]
		if declared && !declaredButUnconsumed[key] {
			t.Errorf("inertConfigKeys[%q]: the Config struct declares this key, "+
				"so it is not unknown. If it is declared but nothing reads it, add "+
				"it to declaredButUnconsumed here as well; if something does read "+
				"it, delete the inertConfigKeys entry.", key)
		}
	}
	for key := range declaredButUnconsumed {
		require.Contains(t, inertConfigKeys, key,
			"%q is listed as declared-but-unconsumed but is not registered in "+
				"inertConfigKeys, so no warning would be emitted", key)
	}
}

// TestRedisDBKeyReachesConfig is the regression guard for the live bug the key
// census exposed: RedisConfig.Database was tagged `database` while every
// default and every shipped config spells the key `db`, so the field was never
// populated and every deployment silently used Redis database 0.
func TestRedisDBKeyReachesConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	require.NoError(t, os.WriteFile(path, []byte(`
version: "1.0.0"
server:
  port: 8080
database:
  host: "localhost"
  dbname: "helix"
redis:
  enabled: true
  host: "localhost"
  port: 6379
  db: 7
auth:
  jwt_secret: "test-secret-not-the-default"
  bcrypt_cost: 10
workers:
  health_check_interval: 30
  max_concurrent_tasks: 4
`), 0o600))

	t.Setenv("HELIX_CONFIG", path)
	cfg, err := Load()
	require.NoError(t, err)
	require.Equal(t, 7, cfg.Redis.Database,
		"redis.db must select the Redis logical database; internal/redis."+
			"NewClient passes this straight to redis.Options.DB")
}

// TestLLMTimeoutAndRetriesReachConfig proves the two keys now survive the load
// instead of being discarded. It deliberately does NOT claim they take effect —
// nothing reads them yet, which is why they are still in inertConfigKeys.
func TestLLMTimeoutAndRetriesReachConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	require.NoError(t, os.WriteFile(path, []byte(`
version: "1.0.0"
server:
  port: 8080
database:
  host: "localhost"
  dbname: "helix"
auth:
  jwt_secret: "test-secret-not-the-default"
  bcrypt_cost: 10
workers:
  health_check_interval: 30
  max_concurrent_tasks: 4
llm:
  timeout: 45
  max_retries: 9
`), 0o600))

	t.Setenv("HELIX_CONFIG", path)
	cfg, err := Load()
	require.NoError(t, err)
	require.Equal(t, 45, cfg.LLM.Timeout)
	require.Equal(t, 9, cfg.LLM.MaxRetries)
}

// TestKnownConfigKeysWalksNestedStructs checks the reflection walk itself —
// if it silently stopped descending, every unknown key would be reported and
// the shipped-config test above would be the only thing standing between us
// and a loader that rejects everything.
func TestKnownConfigKeysWalksNestedStructs(t *testing.T) {
	spec := knownConfigKeys()

	for _, want := range []string{
		"llm",
		"llm.max_tokens",
		"redis.db",
		"server.read_timeout",
		"database.max_conn_lifetime",           // field on the external database.Config
		"application.telemetry.data_retention", // three levels deep
		"verifier.scoring.weights.reliability", // through a pointer field
	} {
		require.Contains(t, spec, want, "reflection walk missed %q", want)
	}

	// Open-ended fields must be wildcards, or arbitrary-but-legitimate
	// sub-keys would be reported as unknown.
	for _, want := range []string{
		"verifier.providers", // map[string]VerifierProviderConfig
		"qa.platforms",       // []string
	} {
		require.Equal(t, keyWildcard, spec[want], "%q must accept arbitrary sub-keys", want)
	}
}

// TestWildcardSubtreeAccepted — anything below a map-typed field is legitimate
// and must not be reported.
func TestWildcardSubtreeAccepted(t *testing.T) {
	spec := knownConfigKeys()
	unknown := spec.unknownKeys([]string{
		"verifier.providers.some-vendor.api_key",
		"verifier.providers.some-vendor.models",
	})
	require.Empty(t, unknown)
}

// TestUnknownKeysCollapseToShallowestPrefix — a dead block must be reported
// once by its root, not once per leaf. `llm.providers` in the shipped config
// expands to ~60 leaves; reporting all of them would bury the real message.
func TestUnknownKeysCollapseToShallowestPrefix(t *testing.T) {
	spec := knownConfigKeys()
	unknown := spec.unknownKeys([]string{
		"llm.providers.openai.endpoint",
		"llm.providers.openai.enabled",
		"llm.providers.ollama.parameters.timeout",
		"llm.max_tokens",
	})
	require.Equal(t, []string{"llm.providers"}, unknown)
}

// TestUnknownTopLevelSectionIsDescribedAsSuch — a key with no dot has no
// parent section to point at, and must say so rather than printing an empty
// section name.
func TestUnknownTopLevelSectionIsDescribedAsSuch(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	require.NoError(t, os.WriteFile(path, []byte(
		"version: \"1.0.0\"\nteremetry: true\n"), 0o600))

	_, err := checkConfigKeys(path)
	require.Error(t, err)
	require.Contains(t, err.Error(), "teremetry")
	require.Contains(t, err.Error(), "top-level section")
}

// TestMapstructureNameHonoursTagOptions — the walk must read tags the way
// mapstructure does, or it will invent keys that do not exist.
func TestMapstructureNameHonoursTagOptions(t *testing.T) {
	type sample struct {
		Tagged   string `mapstructure:"explicit_name"`
		Optioned string `mapstructure:"with_options,omitempty"`
		Ignored  string `mapstructure:"-"`
		Untagged string
	}

	spec := configKeySpec{}
	collectKeys(reflect.TypeOf(sample{}), "", spec, 0)

	require.Contains(t, spec, "explicit_name")
	require.Contains(t, spec, "with_options")
	require.Contains(t, spec, "untagged", "an untagged field falls back to its lower-cased name")
	require.NotContains(t, spec, "ignored", `mapstructure:"-"`+" must be skipped")
}
