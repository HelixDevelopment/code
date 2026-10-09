package config

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// Standing guards for expandShellDefaults.
//
// BEFORE, captured on the pre-fix tree with the variables unset:
//
//	database.user (one of the five expanded fields) = "helix"
//	qa.banks_dir  (declared, NOT expanded)          = "${HELIX_QA_HOME:/opt/helix}/banks"
//	file value of llm.providers.helix-llm.endpoint  = "${HELIX_LLM_ENDPOINT:http://localhost:8081}"
//	Config declares llm.providers: false  -> the endpoint never reaches any Config field at all
//
// The third line is the correction to the premise that a provider endpoint
// "arrives as a literal": it does not arrive at all. The fields that DO both
// use the form and reach a Config field are the notifications channels, and
// those are what this change newly expands.

// TestExpandShellDefaults_ExistingFiveUnchanged pins the pre-change contract of
// the five original fields across every documented placeholder shape. The
// expander was rewritten from os.Expand to a ${…}-only regex; that rewrite must
// be invisible here.
func TestExpandShellDefaults_ExistingFiveUnchanged(t *testing.T) {
	t.Setenv("HELIX_EXPAND_SET", "from-env")

	cases := []struct{ in, want, why string }{
		{"${HELIX_EXPAND_SET}", "from-env", "${VAR} set"},
		{"${HELIX_EXPAND_UNSET}", "", "${VAR} unset yields empty"},
		{"${HELIX_EXPAND_SET:fallback}", "from-env", "${VAR:default} prefers the variable"},
		{"${HELIX_EXPAND_UNSET:fallback}", "fallback", "${VAR:default} falls back"},
		{"${HELIX_EXPAND_SET:-fallback}", "from-env", "${VAR:-default} prefers the variable"},
		{"${HELIX_EXPAND_UNSET:-fallback}", "fallback", "${VAR:-default} falls back"},
		{"${HELIX_EXPAND_UNSET:redis}", "redis", "the shipped redis.host shape"},
		{"${HELIX_EXPAND_UNSET:http://localhost:8081}", "http://localhost:8081", "default containing colons"},
		{"plain-literal", "plain-literal", "a value with no placeholder is untouched"},
		{"prefix-${HELIX_EXPAND_SET}-suffix", "prefix-from-env-suffix", "placeholder embedded in a larger string"},
	}

	for _, tc := range cases {
		t.Run(tc.why, func(t *testing.T) {
			cfg := &Config{}
			cfg.Redis.Host = tc.in
			cfg.Database.Host = tc.in
			cfg.Database.User = tc.in
			cfg.Database.DBName = tc.in
			cfg.Server.Address = tc.in

			expandShellDefaults(cfg)

			require.Equal(t, tc.want, cfg.Redis.Host, "redis.host")
			require.Equal(t, tc.want, cfg.Database.Host, "database.host")
			require.Equal(t, tc.want, cfg.Database.User, "database.user")
			require.Equal(t, tc.want, cfg.Database.DBName, "database.dbname")
			require.Equal(t, tc.want, cfg.Server.Address, "server.address")
		})
	}
}

// TestExpandShellDefaults_LiteralDollarSurvives is why the expander no longer
// uses os.Expand. os.Expand consumes "$$" and expands a bare $VAR, so an SMTP
// password of "pa$$word" came out as "pa" — silent credential corruption. That
// was survivable while only hostnames were expanded; it is not now that
// credentials are in scope. secret_placeholder_test.go already treats
// "pa$$word-with-dollars" as a legitimate secret.
func TestExpandShellDefaults_LiteralDollarSurvives(t *testing.T) {
	for _, literal := range []string{
		"pa$$word-with-dollars",
		"$HELIX_EXPAND_SET",
		"cost$100",
		"brace{not}placeholder",
		"trailing$",
	} {
		t.Run(literal, func(t *testing.T) {
			t.Setenv("HELIX_EXPAND_SET", "from-env")
			cfg := &Config{}
			cfg.Notifications.Channels.Email.SMTP.Password = literal
			cfg.Database.User = literal

			expandShellDefaults(cfg)

			require.Equal(t, literal, cfg.Notifications.Channels.Email.SMTP.Password,
				"a literal credential must survive expansion byte for byte")
			require.Equal(t, literal, cfg.Database.User,
				"the same holds for the pre-existing five")
		})
	}
}

// TestNotificationChannelPlaceholdersAreExpanded is the new capability: the
// notifications block is the one group that both uses the ${…} form in a
// shipped file and now reaches a Config field.
func TestNotificationChannelPlaceholdersAreExpanded(t *testing.T) {
	t.Setenv("HELIX_SLACK_WEBHOOK_URL", "https://hooks.slack.example/from-env")
	t.Setenv("HELIX_EMAIL_SMTP_SERVER", "smtp.from-env.test")

	cfg := &Config{}
	ch := &cfg.Notifications.Channels
	ch.Slack.WebhookURL = "${HELIX_SLACK_WEBHOOK_URL}"
	ch.Slack.Channel = "${HELIX_SLACK_CHANNEL:#helix-alerts}"
	ch.Slack.Username = "${HELIX_SLACK_USERNAME:-HelixCode Bot}"
	ch.Telegram.BotToken = "${HELIX_TELEGRAM_BOT_TOKEN}"
	ch.Telegram.ChatID = "${HELIX_TELEGRAM_CHAT_ID:-12345}"
	ch.Email.SMTP.Server = "${HELIX_EMAIL_SMTP_SERVER}"
	ch.Email.SMTP.Username = "${HELIX_EMAIL_USERNAME:helix@example.test}"
	ch.Email.SMTP.Password = "${HELIX_EMAIL_PASSWORD}"
	ch.Email.SMTP.From = "${HELIX_EMAIL_FROM:noreply@example.test}"
	ch.Email.Recipients.Default = []string{"${HELIX_EMAIL_RECIPIENTS:ops@example.test}"}
	ch.Discord.WebhookURL = "${HELIX_DISCORD_WEBHOOK_URL}"

	expandShellDefaults(cfg)

	require.Equal(t, "https://hooks.slack.example/from-env", ch.Slack.WebhookURL)
	require.Equal(t, "#helix-alerts", ch.Slack.Channel)
	require.Equal(t, "HelixCode Bot", ch.Slack.Username)
	require.Equal(t, "smtp.from-env.test", ch.Email.SMTP.Server)
	require.Equal(t, "helix@example.test", ch.Email.SMTP.Username)
	require.Equal(t, "noreply@example.test", ch.Email.SMTP.From)
	require.Equal(t, "12345", ch.Telegram.ChatID)
	require.Equal(t, []string{"ops@example.test"}, ch.Email.Recipients.Default)

	// A credential whose variable is unset becomes empty, NOT the literal.
	// Empty means "this channel is off" (the constructors disable a channel
	// with an empty credential); the literal would have been handed to the
	// remote service as the actual token or password.
	for name, got := range map[string]string{
		"telegram.bot_token":  ch.Telegram.BotToken,
		"email.smtp.password": ch.Email.SMTP.Password,
		"discord.webhook_url": ch.Discord.WebhookURL,
	} {
		require.Empty(t, got, "%s must resolve to empty, not a literal", name)
		require.False(t, strings.Contains(got, "${"), "%s must not keep a ${…} literal", name)
	}
}

// TestNotificationPlaceholdersExpandedThroughLoad proves the wiring end to end
// rather than by calling the expander directly: a real file, a real Load().
func TestNotificationPlaceholdersExpandedThroughLoad(t *testing.T) {
	t.Setenv("HELIX_SLACK_WEBHOOK_URL", "https://hooks.slack.example/loaded")

	cfg, _ := loadFixture(t, `
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
  channels:
    slack:
      enabled: true
      webhook_url: "${HELIX_SLACK_WEBHOOK_URL}"
      channel: "${HELIX_SLACK_CHANNEL:#helix-notifications}"
`)

	require.Equal(t, "https://hooks.slack.example/loaded",
		cfg.Notifications.Channels.Slack.WebhookURL)
	require.Equal(t, "#helix-notifications",
		cfg.Notifications.Channels.Slack.Channel)
}
