package config

import (
	"context"
	"fmt"
	"reflect"
	"sort"
	"strings"

	"github.com/spf13/viper"
)

// Strict config-key checking.
//
// Why this exists
// ---------------
// Viper is permissive by default: v.Unmarshal() silently discards any key in
// the YAML that has no matching `mapstructure` tag on the target struct. A key
// nobody reads therefore *looks* like configuration — it sits in the file, it
// is documented by its own presence, an operator edits it and reasonably
// believes they changed the program's behaviour. Nothing tells them otherwise.
//
// The concrete defect that motivated this: `llm.timeout` and `llm.max_retries`
// were declared in config/config.yaml but absent from LLMConfig. An operator
// setting `llm.timeout: 30` configured nothing at all — a false belief about
// behaviour under load, held silently. `redis.db` was the same class of
// mistake in miniature: the struct tag is `database`, so the shipped `db: 0`
// was discarded and only the default survived.
//
// checkUnknownKeys turns that silent discard into a startup error naming the
// offending key and the section it sits in. A typo (`llm.temperture`) now
// fails fast instead of silently reverting to a default.
//
// Mechanism choice: explicit key diff, not mapstructure's ErrorUnused
// ------------------------------------------------------------------
// mapstructure's DecoderConfig.ErrorUnused would also reject unknown keys, and
// is a one-line change. It was rejected for three reasons:
//
//  1. WRONG INPUT SET. Viper's Unmarshal decodes v.AllSettings(), which is the
//     merge of the config file, every SetDefault in setDefaultsOn, and every
//     BindEnv key whose variable happens to be set. ErrorUnused cannot tell
//     those apart, so a stray default or an env binding would be reported as
//     if the operator had put it in their file. The thing we actually want to
//     police is the *file*, so this code reads the file — and only the file —
//     through a bare viper instance carrying no defaults and no env bindings.
//
//  2. WORSE MESSAGE. ErrorUnused reports the leaf paths it could not place.
//     For a dead block like `llm.providers` (13 providers × ~6 keys) that is
//     ~60 lines of noise for one mistake. checkUnknownKeys collapses each
//     unknown key to its SHALLOWEST unknown prefix, so that block is reported
//     once, as `llm.providers`, alongside the section it belongs to.
//
//  3. NO MAP ESCAPE HATCH NEEDED. Config legitimately contains open-ended
//     map fields (verifier.providers is a map[string]VerifierProviderConfig),
//     where arbitrary sub-keys are correct. Walking the struct ourselves lets
//     us mark those subtrees as wildcards deliberately, rather than relying on
//     mapstructure's decode-order behaviour to be permissive in the right
//     places.
//
// The check is deliberately confined to unknown *keys*. It says nothing about
// whether a known key's value is sensible; validateConfig still owns that.

// keyKind classifies a dotted config path derived from the Config struct.
type keyKind int

const (
	// keyKnown marks a path that corresponds to a struct field. Sub-paths of
	// a keyKnown path are valid only if they are themselves known.
	keyKnown keyKind = iota
	// keyWildcard marks a path whose Go type accepts arbitrary sub-keys — a
	// map, a slice, or an interface. Everything below it is accepted without
	// further checking.
	keyWildcard
)

// configKeySpec is the set of dotted config paths reachable from a struct,
// derived by reflection over its `mapstructure` tags.
type configKeySpec map[string]keyKind

// knownConfigKeys reflects over the Config struct and returns every dotted
// config path it can accept.
//
// Intermediate struct paths are included (`llm` as well as `llm.max_tokens`)
// so an unknown leaf can be attributed to the shallowest path that actually
// went wrong. Fields whose type accepts arbitrary sub-keys — maps, slices,
// interfaces — are recorded as keyWildcard and not descended into.
func knownConfigKeys() configKeySpec {
	spec := configKeySpec{}
	collectKeys(reflect.TypeOf(Config{}), "", spec, 0)
	return spec
}

// maxKeyDepth bounds the reflection walk. Config is nowhere near this deep;
// the bound exists so a self-referential type (a struct holding a pointer to
// itself) can never send this into an unbounded recursion.
const maxKeyDepth = 12

func collectKeys(t reflect.Type, prefix string, spec configKeySpec, depth int) {
	if depth > maxKeyDepth {
		return
	}
	for t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	if t.Kind() != reflect.Struct {
		return
	}

	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if f.PkgPath != "" {
			continue // unexported; mapstructure cannot populate it
		}

		name := mapstructureName(f)
		if name == "" {
			continue // explicitly ignored via `mapstructure:"-"`
		}

		path := name
		if prefix != "" {
			path = prefix + "." + name
		}

		ft := f.Type
		for ft.Kind() == reflect.Ptr {
			ft = ft.Elem()
		}

		switch ft.Kind() {
		case reflect.Map, reflect.Slice, reflect.Array, reflect.Interface:
			// Open-ended: arbitrary keys below this point are legitimate.
			spec[path] = keyWildcard
		case reflect.Struct:
			// time.Duration and friends are not structs; a genuine struct is
			// a nested config section, so record it and descend.
			spec[path] = keyKnown
			collectKeys(ft, path, spec, depth+1)
		default:
			spec[path] = keyKnown
		}
	}
}

// mapstructureName returns the config key a struct field binds to, applying
// the same rules mapstructure itself uses: an explicit tag wins, `-` means
// ignore, options after a comma (`,omitempty`) are stripped, and an absent or
// empty tag falls back to the lower-cased field name.
func mapstructureName(f reflect.StructField) string {
	tag := f.Tag.Get("mapstructure")
	if tag == "-" {
		return ""
	}
	name := tag
	if comma := strings.Index(tag, ","); comma >= 0 {
		name = tag[:comma]
	}
	if name == "" {
		name = strings.ToLower(f.Name)
	}
	return name
}

// unknownKeys returns the config keys present in fileKeys that the Config
// struct cannot accept, each collapsed to its shallowest unknown prefix and
// reported once.
//
// fileKeys are dotted, lower-cased leaf paths as produced by viper's
// AllKeys() — the caller is responsible for having read ONLY the config file
// into that viper instance.
func (spec configKeySpec) unknownKeys(fileKeys []string) []string {
	seen := map[string]bool{}
	var unknown []string

	for _, key := range fileKeys {
		segments := strings.Split(key, ".")
		for i := range segments {
			path := strings.Join(segments[:i+1], ".")
			kind, ok := spec[path]
			if ok && kind == keyWildcard {
				break // arbitrary sub-keys are legitimate here
			}
			if ok {
				continue // known so far; keep walking deeper
			}
			// First segment that the struct cannot accept. Report this
			// prefix rather than the full leaf, so a whole dead block
			// collapses to one line.
			if !seen[path] {
				seen[path] = true
				unknown = append(unknown, path)
			}
			break
		}
	}

	sort.Strings(unknown)
	return unknown
}

// fileOnlyKeys reads path as YAML through a bare viper instance — no
// defaults, no environment bindings — and returns its dotted leaf keys.
//
// Reading the file separately from the main Load() instance is what keeps the
// strict check honest: a key reported as unknown is one the operator actually
// wrote, never one setDefaultsOn or a BindEnv put there.
func fileOnlyKeys(path string) ([]string, error) {
	fv := viper.New()
	fv.SetConfigFile(path)
	if err := fv.ReadInConfig(); err != nil {
		return nil, err
	}
	return fv.AllKeys(), nil
}

// inertConfigKeys is the hand-maintained register of config keys that appear
// in shipped config files but that NO code path applies. Each is tolerated by
// the strict check — the loader will not refuse to start over them — but each
// produces a warning on every load saying, in as many words, that setting it
// changes nothing.
//
// Why tolerate rather than delete: these are pre-existing blocks, some of them
// large, and deleting configuration whose history has not been established is
// exactly the move §11.4.124 forbids. Warning is strictly better than today's
// silence and does not destroy anything. Deleting a block, or wiring it up, is
// a separate decision with a separate change.
//
// Why warn rather than ignore: an entry here is a key an operator can still
// write and still believe in. The register makes the lie visible at startup
// instead of hiding it in a struct definition nobody reads.
//
// TWO CATEGORIES live here, and the distinction matters:
//
//   - NOT DECLARED at all — the Config struct has no field for them, so
//     unknownKeys() flags them and this register is what downgrades the error
//     to a warning.
//   - DECLARED BUT UNCONSUMED — the struct has the field and the value loads
//     fine, but nothing reads it. unknownKeys() cannot see this class; it is
//     recorded here by hand.
//
// Removing an entry is the LAST step of fixing it: delete the line in the same
// change that either wires the key to a consumer or removes the key from the
// config files. An entry removed on its own turns the warning back into
// silence, which is the defect this whole file exists to prevent.
var inertConfigKeys = map[string]string{
	// --- declared but unconsumed -------------------------------------------
	"llm.timeout": "declared on LLMConfig but no caller reads it yet; " +
		"per-request LLM timeouts are not governed by this value",
	"llm.max_retries": "declared on LLMConfig but no caller reads it yet; " +
		"LLM retry behaviour is not governed by this value",

	// --- not declared at all -----------------------------------------------

	// The four blocks below are whole top-level sections that config/
	// production-config.yaml configures at length and that Config does not
	// declare at any depth. Verified unconsumed: no field on Config, and zero
	// direct viper reads (`Get*("security.` etc.) anywhere in non-test code.
	//
	// They are registered rather than deleted. Deleting them would throw away
	// a statement of intent nobody authorised discarding, and declaring them
	// would claim a build reads settings it does not. Registering them says
	// the true thing out loud at startup: this is configured, and it does
	// nothing here yet. Each entry comes out in the same change that wires it.
	"environments": "no field on Config; the per-environment override blocks " +
		"(production/staging/development) are discarded whole. Environment is " +
		"selected by which config file is loaded, not by this block",
	"features": "no field on Config; every feature flag in this block is " +
		"discarded. No feature is gated on it",
	"performance": "no field on Config; the connection-pool, cache and " +
		"concurrency tuning in this block is discarded",
	"security": "no field on Config; the TLS, CORS, rate-limit and audit " +
		"settings in this block are discarded. Security posture is not " +
		"governed by this file",

	// Leaves that production-config.yaml sets on sections that DO exist, but
	// which the section's struct does not declare. Same verification, same
	// reasoning as the blocks above.
	"auth.refresh_token_expiry": "no field on AuthConfig; refresh-token " +
		"lifetime is not governed by this value",
	"database.max_connections": "no field on database.Config; the pool " +
		"maximum is not governed by this value",
	"database.min_connections": "no field on database.Config; the pool " +
		"minimum is not governed by this value",
	"redis.pool_size": "no field on RedisConfig; the Redis pool size is not " +
		"governed by this value",
	"redis.max_retries": "no field on RedisConfig; Redis retry behaviour is " +
		"not governed by this value",
	"redis.read_timeout": "no field on RedisConfig; Redis read timeouts are " +
		"not governed by this value",
	"redis.write_timeout": "no field on RedisConfig; Redis write timeouts " +
		"are not governed by this value",
	"llm.batch_size": "no field on LLMConfig; request batching is not " +
		"governed by this value",
	"llm.rate_limit_per_minute": "no field on LLMConfig; LLM rate limiting " +
		"is not governed by this value",
	"logging.file": "no field on LoggingConfig; the log destination is " +
		"governed by logging.output, which IS read. Setting this changes " +
		"nothing and can disagree with the field that works",
	"logging.fields": "no field on LoggingConfig; these static fields are " +
		"not attached to any log line",
	"tasks.timeout": "no field on TasksConfig; task timeouts are not " +
		"governed by this value",
	"tasks.max_concurrent_per_worker": "no field on TasksConfig; per-worker " +
		"concurrency is not governed by this value",
	"tasks.priority_levels": "no field on TasksConfig; the priority " +
		"vocabulary is not read from configuration",
	"tasks.resource_limits": "no field on TasksConfig; the per-task cpu/" +
		"memory limits in this block are discarded",
	"workers.connection_timeout": "no field on WorkersConfig; worker " +
		"connection timeouts are not governed by this value",
	"workers.retry_interval": "no field on WorkersConfig; worker retry " +
		"spacing is not governed by this value",
	"workers.max_retries": "no field on WorkersConfig; worker retry counts " +
		"are not governed by this value",
	"llm.providers": "no field on LLMConfig; the whole provider block " +
		"(endpoints, enabled flags, api keys, parameters) is discarded. " +
		"Providers are configured elsewhere",
	"llm.selection": "no field on LLMConfig; provider selection strategy, " +
		"fallback and health-check interval are discarded",
	"workers.auto_install": "no field on WorkersConfig; set only in " +
		"config/minimal-test-config.yaml, which nothing references. No " +
		"worker auto-install behaviour is driven by it",
	"workers.ssh_timeout": "no field on WorkersConfig; set only in " +
		"config/minimal-test-config.yaml, which nothing references. No SSH " +
		"timeout is driven by it",
	// `notifications` used to sit in the "not declared at all" group: no field
	// on Config, so the whole block was discarded and only HELIX_* environment
	// variables ever produced a channel. It is now declared
	// (config.NotificationsConfig) and consumed
	// (notification.NewEngineFromConfig), so the block-level entry is gone and
	// the startup warning with it. What remains inert is a handful of LEAVES
	// the channel constructors take no argument for — each named individually
	// so the residue stays visible instead of hiding behind a struct field
	// nobody reads.
	"notifications.channels.slack.timeout": "declared on " +
		"SlackNotificationConfig but NewSlackChannel takes no timeout; the " +
		"value parses and is then ignored",
	"notifications.channels.telegram.timeout": "declared on " +
		"TelegramNotificationConfig but NewTelegramChannel takes no timeout; " +
		"the value parses and is then ignored",
	"notifications.channels.email.timeout": "declared on " +
		"EmailNotificationConfig but NewEmailChannel takes no timeout; the " +
		"value parses and is then ignored",
	"notifications.channels.discord.timeout": "declared on " +
		"DiscordNotificationConfig but NewDiscordChannel takes no timeout; " +
		"the value parses and is then ignored",
	"notifications.channels.email.smtp.tls": "declared on " +
		"EmailSMTPNotificationConfig but EmailChannel negotiates its own " +
		"transport security and reads no flag; the value changes nothing",
	"notifications.channels.email.recipients": "declared on " +
		"EmailNotificationConfig but NewEmailChannel takes no recipient " +
		"list — mail is addressed from the SMTP account — so the whole " +
		"recipients subtree parses and is then ignored",
}

// checkConfigKeys inspects a config file for keys that do not reach the
// program.
//
// It returns (warnings, error): error is non-nil when the file declares a key
// that is neither known to the Config struct nor registered in
// inertConfigKeys — that is a typo or a stale key and startup should fail.
// warnings are the ready-to-print notices for keys in inertConfigKeys that the
// file actually sets.
//
// A path of "" (no config file found — Load() runs on defaults alone) yields
// nothing to check.
func checkConfigKeys(path string) ([]string, error) {
	if path == "" {
		return nil, nil
	}

	fileKeys, err := fileOnlyKeys(path)
	if err != nil {
		// Load() reads the same file and will surface a better parse error
		// than we can from here; don't mask it with our own.
		return nil, nil
	}

	var hardUnknown []string
	for _, k := range knownConfigKeys().unknownKeys(fileKeys) {
		if _, tolerated := inertConfigKeys[k]; !tolerated {
			hardUnknown = append(hardUnknown, k)
		}
	}

	if len(hardUnknown) > 0 {
		return nil, fmt.Errorf("%s", tr(context.Background(),
			"internal_config_error_unknown_config_keys",
			map[string]any{
				"Path": path,
				"Keys": strings.Join(describeKeys(hardUnknown), "; "),
			}))
	}

	return inertWarnings(fileKeys), nil
}

// inertWarnings returns one warning per inertConfigKeys entry that the loaded
// file actually sets. A registered key the operator did not write is not worth
// mentioning.
func inertWarnings(fileKeys []string) []string {
	var present []string
	for inert := range inertConfigKeys {
		if keySetInFile(inert, fileKeys) {
			present = append(present, inert)
		}
	}
	sort.Strings(present)

	warnings := make([]string, 0, len(present))
	for _, k := range present {
		warnings = append(warnings, tr(context.Background(),
			"internal_config_warn_inert_config_key",
			map[string]any{"Key": k, "Reason": inertConfigKeys[k]}))
	}
	return warnings
}

// keySetInFile reports whether want is set in the file, either as a leaf key
// in its own right or as an ancestor of one (a block like `notifications`
// never appears as a leaf — only `notifications.enabled` and friends do).
func keySetInFile(want string, fileKeys []string) bool {
	for _, k := range fileKeys {
		if k == want || strings.HasPrefix(k, want+".") {
			return true
		}
	}
	return false
}

// describeKeys renders each unknown key alongside the top-level section it
// sits in, so an operator scanning a long YAML file knows where to look.
func describeKeys(keys []string) []string {
	out := make([]string, 0, len(keys))
	for _, k := range keys {
		if section, _, nested := strings.Cut(k, "."); nested {
			out = append(out, fmt.Sprintf("%q (in section %q)", k, section))
			continue
		}
		out = append(out, fmt.Sprintf("%q (top-level section)", k))
	}
	return out
}
