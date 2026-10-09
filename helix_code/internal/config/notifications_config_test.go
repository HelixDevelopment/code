package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// Standing regression guards for the `notifications:` block.
//
// BEFORE: Config had no field for the block, so viper discarded it wholesale.
// Captured pre-fix reproduction, against a file otherwise identical to the one
// below and loading without error:
//
//	Config struct declares a `notifications` field: false
//	WARN: config key notifications has no effect in this build — no field on
//	      Config; the entire block is discarded. […] the rules list is read by
//	      nothing at all. Setting it changes nothing.
//
// AFTER: the block parses into cfg.Notifications and the warning is gone.

// notificationsFixture is a config valid in EVERY respect, so a failure here
// can only be about the thing under test. An earlier probe of this area
// "passed" only because a stripped-down config tripped an unrelated required
// field before reaching the assertion.
const notificationsFixture = `
server:
  address: "0.0.0.0"
  port: 8080
database:
  host: "localhost"
  port: 5432
  user: "test"
  dbname: "test"
  sslmode: "disable"
redis:
  host: "localhost"
  port: 6379
  enabled: true
auth:
  jwt_secret: "test-secret-minimum-32-chars-long-enough"
  token_expiry: 86400
  session_expiry: 604800
  bcrypt_cost: 12
notifications:
  enabled: true
  rules:
    - name: "Critical Task Failures"
      condition: "type==error"
      channels: ["slack", "email", "telegram"]
      priority: urgent
      enabled: true
    - name: "Workflow Completions"
      condition: "type==success"
      channels: ["slack"]
      priority: medium
      enabled: false
  channels:
    slack:
      enabled: true
      webhook_url: "https://hooks.slack.example/T000/B000/XXX"
      channel: "#helix-notifications"
      username: "HelixCode Bot"
      timeout: 10
    telegram:
      enabled: true
      bot_token: "tg-token-from-file"
      chat_id: "-1001234567890"
      timeout: 10
    email:
      enabled: true
      smtp:
        server: "smtp.example.test"
        port: 2525
        username: "helix@example.test"
        password: "smtp-password-from-file"
        from: "noreply@example.test"
        tls: true
      recipients:
        default: ["ops@example.test"]
      timeout: 30
    discord:
      enabled: false
      webhook_url: "https://discord.example/api/webhooks/1/x"
      timeout: 10
`

// loadFixture writes body to a temp config.yaml, points Load() at it and
// returns the loaded config plus the file path.
func loadFixture(t *testing.T, body string) (*Config, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	require.NoError(t, os.WriteFile(path, []byte(body), 0600))

	t.Setenv("HELIX_CONFIG", path)
	resetForTest()
	t.Cleanup(resetForTest)

	cfg, err := Load()
	require.NoError(t, err, "fixture must be a fully valid config")
	return cfg, path
}

// TestNotificationsBlockReachesConfig is the guard for the discard defect: the
// operator's block must arrive on Config instead of being thrown away.
func TestNotificationsBlockReachesConfig(t *testing.T) {
	cfg, _ := loadFixture(t, notificationsFixture)

	n := cfg.Notifications
	require.True(t, n.Enabled, "notifications.enabled must survive the load")

	require.Len(t, n.Rules, 2, "both rules must survive the load")
	require.Equal(t, "Critical Task Failures", n.Rules[0].Name)
	require.Equal(t, "type==error", n.Rules[0].Condition)
	require.Equal(t, []string{"slack", "email", "telegram"}, n.Rules[0].Channels)
	require.Equal(t, "urgent", n.Rules[0].Priority)
	require.True(t, n.Rules[0].Enabled)
	require.False(t, n.Rules[1].Enabled, "a disabled rule must load as disabled")

	ch := n.Channels
	require.True(t, ch.Slack.Enabled)
	require.Equal(t, "https://hooks.slack.example/T000/B000/XXX", ch.Slack.WebhookURL)
	require.Equal(t, "#helix-notifications", ch.Slack.Channel)
	require.Equal(t, "HelixCode Bot", ch.Slack.Username)

	require.Equal(t, "tg-token-from-file", ch.Telegram.BotToken)
	require.Equal(t, "-1001234567890", ch.Telegram.ChatID)

	require.Equal(t, "smtp.example.test", ch.Email.SMTP.Server)
	require.Equal(t, 2525, ch.Email.SMTP.Port)
	require.Equal(t, "helix@example.test", ch.Email.SMTP.Username)
	require.Equal(t, "smtp-password-from-file", ch.Email.SMTP.Password)
	require.Equal(t, "noreply@example.test", ch.Email.SMTP.From)
	require.Equal(t, []string{"ops@example.test"}, ch.Email.Recipients.Default)

	require.False(t, ch.Discord.Enabled)
	require.Equal(t, "https://discord.example/api/webhooks/1/x", ch.Discord.WebhookURL)
}

// TestNotificationsBlockNoLongerWarnsAsInert pins the other half of the fix:
// the startup warning that told the operator the block does nothing must be
// gone, because it is no longer true.
func TestNotificationsBlockNoLongerWarnsAsInert(t *testing.T) {
	_, path := loadFixture(t, notificationsFixture)

	warnings, err := checkConfigKeys(path)
	require.NoError(t, err, "every notifications key must be declared; an "+
		"undeclared subkey is a HARD startup failure, not a warning")

	for _, w := range warnings {
		require.NotContains(t, w, "config key notifications has no effect",
			"the block-level inert warning must be retired along with the defect")
	}
}

// TestNotificationsInertLeavesStillWarn is the honest counterpart: the leaves
// no channel constructor consumes must still announce themselves, so wiring the
// block up does not quietly convert one silent lie into six smaller ones.
func TestNotificationsInertLeavesStillWarn(t *testing.T) {
	_, path := loadFixture(t, notificationsFixture)

	warnings, err := checkConfigKeys(path)
	require.NoError(t, err)

	joined := strings.Join(warnings, "\n")
	for _, key := range []string{
		"notifications.channels.slack.timeout",
		"notifications.channels.telegram.timeout",
		"notifications.channels.email.timeout",
		"notifications.channels.discord.timeout",
		"notifications.channels.email.smtp.tls",
		"notifications.channels.email.recipients",
	} {
		require.Contains(t, joined, key,
			"%s parses but nothing reads it; that must stay visible at startup", key)
	}
}

// TestShippedConfigsWithNotificationsStillLoad is the completeness guard for
// the derived field set. strict.go turns an undeclared key into a REFUSAL, so
// a notifications subkey present in a shipped file but missing from the struct
// would stop the program from starting. Every parseable shipped config is
// checked, not a sample.
//
// SCOPE, measured rather than assumed: this catches the loss of a CONSUMED
// field (deleting SlackNotificationConfig.WebhookURL makes it fail). It does
// NOT catch the loss of one of the six leaves registered in inertConfigKeys —
// deleting SlackNotificationConfig.Timeout leaves this green, because the
// register downgrades that key from an error to a warning whether or not the
// struct declares it. That is the register working as designed, not a hole
// here; TestNotificationsInertLeavesStillWarn is what keeps those six honest.
func TestShippedConfigsWithNotificationsStillLoad(t *testing.T) {
	matches, err := filepath.Glob("../../config/*.yaml")
	require.NoError(t, err)
	require.NotEmpty(t, matches, "shipped configs must be discoverable from this package")

	checked := 0
	for _, path := range matches {
		keys, err := fileOnlyKeys(path)
		if err != nil {
			// config/production-config.yaml does not parse as YAML at all
			// (pre-existing: it ends with a stray markdown fence). viper
			// refuses it long before any struct field matters.
			t.Logf("skipping unparseable %s: %v", filepath.Base(path), err)
			continue
		}
		if !keySetInFile("notifications", keys) {
			continue
		}
		checked++
		t.Run(filepath.Base(path), func(t *testing.T) {
			_, err := checkConfigKeys(path)
			require.NoError(t, err, "%s sets a notifications key the Config "+
				"struct cannot accept, which makes startup fail", path)
		})
	}
	require.NotZero(t, checked, "no shipped config exercised the notifications block")
}
