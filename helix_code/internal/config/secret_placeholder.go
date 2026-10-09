package config

import (
	"context"
	"fmt"
	"regexp"
	"sort"
)

// unexpandedPlaceholder matches the three shapes expandShellDefaults
// understands -- ${VAR}, ${VAR:default} and ${VAR:-default} -- and captures the
// variable name so the refusal can tell the operator exactly what to export.
//
// It deliberately does NOT match a bare $VAR. That form is not a placeholder
// this loader ever expands, so a secret that happens to start with a dollar
// sign is a real secret, not an unexpanded reference, and refusing it would
// lock a user out of their own configuration.
var unexpandedPlaceholder = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)[^}]*\}`)

// secretFields returns every configuration field whose value is a credential,
// paired with the dotted path an operator would edit.
//
// The list is explicit rather than a reflective walk over every string in the
// Config, and that is a deliberate narrowing. Non-secret fields legitimately
// hold unexpanded placeholders in normal operation — `qa.banks_dir` set to
// `${HELIX_QA_HOME}/banks`, `logging.output` to `${HELIX_LOG_DIR}/helix.log`,
// `verifier.endpoint` to `${HELIX_VERIFIER_ENDPOINT}` — because
// expandShellDefaults substitutes only a named set of fields and none of those
// are in it. A blanket scan would refuse configurations that load fine today.
// The danger being fixed here is specific to credentials, so the check is too.
// TestValidateConfig_NonSecretFieldsAreNotScanned pins that scope.
//
// RE-EXAMINED when expandShellDefaults grew to cover the notifications block.
// That change makes the notification credentials — the Slack and Discord
// webhooks, the Telegram bot token, the SMTP password — expanded fields, so
// they can no longer be holding a `${…}` literal by the time validateConfig
// runs. Adding them here would therefore be a check that cannot fire. They are
// also the opposite failure mode from the fields listed below: an unset
// variable leaves them empty, and every channel constructor treats an empty
// credential as "this channel is off". The fields below fail OPEN — an empty
// JWT secret still signs tokens — which is exactly why they must be refused
// rather than quietly expanded. The narrow scope stands; only its worked
// examples needed correcting. (The previous example,
// `${HELIX_LLM_ENDPOINT:http://localhost:8081}`, was doubly inapt: LLMConfig
// declares no `providers` field, so that value is discarded by viper and never
// reaches a Config string at all.)
//
// The cost of the narrowing is that a newly added credential field must be
// added here as well. That is the maintained surface, and it is why the guard
// enumerates every field rather than sampling one.
func secretFields(cfg *Config) []struct{ Path, Value string } {
	fields := []struct{ Path, Value string }{
		{"auth.jwt_secret", cfg.Auth.JWTSecret},
		{"auth.wire_facade_api_keys", cfg.Auth.WireFacadeAPIKeys},
		{"database.password", cfg.Database.Password},
		{"redis.password", cfg.Redis.Password},
		{"qa.llm_api_key", cfg.QA.LLMAPIKey},
		{"providers.mem0.api_key", cfg.Providers.Mem0.APIKey},
		{"providers.zep.api_key", cfg.Providers.Zep.APIKey},
		{"providers.memonto.api_key", cfg.Providers.Memonto.APIKey},
		{"providers.baseai.api_key", cfg.Providers.BaseAI.APIKey},
	}

	// Verifier is an OPTIONAL section and its field on Config is a POINTER, so
	// it is nil in any config that omits the block -- which the minimal configs
	// built by other validation tests do. Reading through it unconditionally
	// panicked validateConfig, turning a missing optional section into a crash
	// at startup: a far worse failure than the one this file exists to prevent.
	// Caught by a sibling bcrypt guard, which is precisely what that guard is
	// for.
	if cfg.Verifier == nil {
		return fields
	}
	fields = append(fields, struct{ Path, Value string }{"verifier.api_key", cfg.Verifier.APIKey})

	// Per-provider verifier keys are a map, so the paths are data. Sorted, so
	// that a config with two offending providers always names the same one
	// first: an error message that varies run to run is a bug report nobody can
	// reproduce.
	names := make([]string, 0, len(cfg.Verifier.Providers))
	for name := range cfg.Verifier.Providers {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		fields = append(fields, struct{ Path, Value string }{
			Path:  fmt.Sprintf("verifier.providers.%s.api_key", name),
			Value: cfg.Verifier.Providers[name].APIKey,
		})
	}
	return fields
}

// checkSecretPlaceholders refuses a configuration in which a credential is
// still an unexpanded ${...} reference.
//
// This is the failure mode worth being loud about. When the environment
// variable is unset, misspelled, or the expansion step is skipped, the literal
// string "${HELIX_AUTH_JWT_SECRET}" becomes the value -- and for a secret that
// makes it a known, published constant sitting in a git-tracked file. Every JWT
// would then be signed with a value any reader of this repository already
// knows, while health checks, tests and startup logs all report success. A
// missing secret must stop the process, not quietly become a shared password.
//
// The refusal names both the field and the variable so the fix is one export
// away, and it never includes the value: for a field that IS correctly
// expanded, printing it would leak the real credential into logs.
func checkSecretPlaceholders(cfg *Config) error {
	for _, f := range secretFields(cfg) {
		m := unexpandedPlaceholder.FindStringSubmatch(f.Value)
		if m == nil {
			continue
		}
		return fmt.Errorf("%s", tr(context.Background(),
			"internal_config_error_unexpanded_secret_placeholder",
			map[string]any{"Field": f.Path, "Var": m[1]}))
	}
	return nil
}
