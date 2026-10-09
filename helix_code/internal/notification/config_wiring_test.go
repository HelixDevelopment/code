package notification

import (
	"sort"
	"testing"

	"dev.helix.code/internal/config"
	"github.com/stretchr/testify/require"
)

// Guards for NewEngineFromConfig.
//
// BEFORE: channels came from HELIX_* environment variables assembled inline in
// cmd/other_commands.go, and the `notifications:` config block reached nothing
// — viper discarded it because Config had no field for it. An operator who
// enabled Slack in the file and pasted a webhook there got silence.
//
// AFTER: the block registers channels and rules, and the environment still
// wins wherever it is set. The env-only path is pinned byte-for-byte below,
// because "existing environment-only deployments keep working exactly as they
// do today" is the hard constraint of this change.

// clearNotificationEnv unsets every variable the wiring reads, so a test that
// means "the environment is silent" really has a silent environment rather than
// whatever the developer's shell happened to export.
func clearNotificationEnv(t *testing.T) {
	t.Helper()
	for _, k := range []string{
		envSlackWebhook, envDiscordWebhook,
		envTelegramToken, envTelegramChatID,
		envEmailServer, envEmailUsername, envEmailPassword,
	} {
		t.Setenv(k, "")
	}
}

func channelNames(e *NotificationEngine) []string {
	e.mutex.RLock()
	defer e.mutex.RUnlock()
	names := make([]string, 0, len(e.channels))
	for n := range e.channels {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

// fullyConfigured returns a config whose block enables all four channels.
func fullyConfigured() *config.Config {
	cfg := &config.Config{}
	n := &cfg.Notifications
	n.Enabled = true
	n.Channels.Slack = config.SlackNotificationConfig{
		Enabled: true, WebhookURL: "https://hooks.slack.example/from-file",
		Channel: "#from-file", Username: "FileBot",
	}
	n.Channels.Discord = config.DiscordNotificationConfig{
		Enabled: true, WebhookURL: "https://discord.example/from-file",
	}
	n.Channels.Telegram = config.TelegramNotificationConfig{
		Enabled: true, BotToken: "tg-token-from-file", ChatID: "chat-from-file",
	}
	n.Channels.Email = config.EmailNotificationConfig{
		Enabled: true,
		SMTP: config.EmailSMTPNotificationConfig{
			Server: "smtp.from-file.test", Port: 2525,
			Username: "helix@from-file.test", Password: "pw-from-file",
			From: "noreply@from-file.test",
		},
	}
	return cfg
}

// TestEngineFromConfig_ConfigBlockRegistersChannels is the guard for the
// discard defect at the behavioural level: editing the file must now produce
// channels.
func TestEngineFromConfig_ConfigBlockRegistersChannels(t *testing.T) {
	clearNotificationEnv(t)

	engine := NewEngineFromConfig(fullyConfigured())

	require.Equal(t, []string{"discord", "email", "slack", "telegram"}, channelNames(engine),
		"every channel the file enables must be registered")

	engine.mutex.RLock()
	slack := engine.channels["slack"].(*SlackChannel)
	email := engine.channels["email"].(*EmailChannel)
	engine.mutex.RUnlock()

	require.Equal(t, "https://hooks.slack.example/from-file", slack.webhook)
	require.Equal(t, "#from-file", slack.channel, "the file's channel must be used")
	require.Equal(t, "FileBot", slack.username, "the file's username must be used")
	require.Equal(t, 2525, email.port, "the file's SMTP port must be used")
	require.Equal(t, "noreply@from-file.test", email.from, "an explicit from: must be honoured")
}

// TestEngineFromConfig_EnvironmentOnlyIsUnchanged is the hard constraint. With
// no config block at all — the shape of every deployment that predates this
// change — the engine must be exactly what the old inline code produced:
// the same four channels, the same positional defaults, no rules.
func TestEngineFromConfig_EnvironmentOnlyIsUnchanged(t *testing.T) {
	clearNotificationEnv(t)
	t.Setenv(envSlackWebhook, "https://hooks.slack.example/from-env")
	t.Setenv(envDiscordWebhook, "https://discord.example/from-env")
	t.Setenv(envTelegramToken, "tg-token-from-env")
	t.Setenv(envTelegramChatID, "chat-from-env")
	t.Setenv(envEmailServer, "smtp.from-env.test")
	t.Setenv(envEmailUsername, "helix@from-env.test")
	t.Setenv(envEmailPassword, "pw-from-env")

	for name, cfg := range map[string]*config.Config{
		"no config at all":     nil,
		"config with no block": {},
		"block present, absent enabled flag": func() *config.Config {
			// The exact shape of an old deployment whose file was never
			// touched: zero-valued block, i.e. Enabled == false.
			return &config.Config{}
		}(),
	} {
		t.Run(name, func(t *testing.T) {
			engine := NewEngineFromConfig(cfg)

			require.Equal(t, []string{"discord", "email", "slack", "telegram"}, channelNames(engine))

			engine.mutex.RLock()
			slack := engine.channels["slack"].(*SlackChannel)
			discord := engine.channels["discord"].(*DiscordChannel)
			telegram := engine.channels["telegram"].(*TelegramChannel)
			email := engine.channels["email"].(*EmailChannel)
			rules := len(engine.rules)
			engine.mutex.RUnlock()

			require.Equal(t, "https://hooks.slack.example/from-env", slack.webhook)
			require.Equal(t, "helixcode", slack.channel, "the pre-change positional default")
			require.Equal(t, "HelixCode", slack.username, "the pre-change positional default")
			require.Equal(t, "https://discord.example/from-env", discord.webhook)
			require.Equal(t, "tg-token-from-env", telegram.botToken)
			require.Equal(t, "chat-from-env", telegram.chatID)
			require.Equal(t, "smtp.from-env.test", email.smtpServer)
			require.Equal(t, 587, email.port, "the pre-change positional default")
			require.Equal(t, "helix@from-env.test", email.username)
			require.Equal(t, "pw-from-env", email.password)
			require.Equal(t, "helix@from-env.test", email.from,
				"the pre-change code addressed mail from the SMTP username")
			require.Zero(t, rules, "no rules existed before the block was wired up")
		})
	}
}

// TestEngineFromConfig_EnvironmentWinsOverConfig pins the precedence direction.
func TestEngineFromConfig_EnvironmentWinsOverConfig(t *testing.T) {
	clearNotificationEnv(t)
	t.Setenv(envSlackWebhook, "https://hooks.slack.example/from-env")
	t.Setenv(envTelegramToken, "tg-token-from-env")
	t.Setenv(envTelegramChatID, "chat-from-env")
	t.Setenv(envEmailServer, "smtp.from-env.test")
	t.Setenv(envEmailUsername, "helix@from-env.test")
	t.Setenv(envEmailPassword, "pw-from-env")
	t.Setenv(envDiscordWebhook, "https://discord.example/from-env")

	engine := NewEngineFromConfig(fullyConfigured())

	engine.mutex.RLock()
	defer engine.mutex.RUnlock()
	require.Equal(t, "https://hooks.slack.example/from-env", engine.channels["slack"].(*SlackChannel).webhook)
	require.Equal(t, "https://discord.example/from-env", engine.channels["discord"].(*DiscordChannel).webhook)
	require.Equal(t, "tg-token-from-env", engine.channels["telegram"].(*TelegramChannel).botToken)
	require.Equal(t, "smtp.from-env.test", engine.channels["email"].(*EmailChannel).smtpServer)
}

// TestEngineFromConfig_EnvAndConfigMixPerChannel: precedence is per channel, so
// a deployment can move one channel into the file without losing the others.
func TestEngineFromConfig_EnvAndConfigMixPerChannel(t *testing.T) {
	clearNotificationEnv(t)
	t.Setenv(envSlackWebhook, "https://hooks.slack.example/from-env")

	engine := NewEngineFromConfig(fullyConfigured())

	engine.mutex.RLock()
	defer engine.mutex.RUnlock()
	require.Equal(t, "https://hooks.slack.example/from-env",
		engine.channels["slack"].(*SlackChannel).webhook, "env keeps slack")
	require.Equal(t, "https://discord.example/from-file",
		engine.channels["discord"].(*DiscordChannel).webhook, "file supplies discord")
}

// TestEngineFromConfig_DisabledBlockSuppressesConfigChannelsOnly documents the
// deliberate asymmetry of the enabled flag, and why it is not a regression.
func TestEngineFromConfig_DisabledBlockSuppressesConfigChannelsOnly(t *testing.T) {
	clearNotificationEnv(t)
	cfg := fullyConfigured()
	cfg.Notifications.Enabled = false

	require.Empty(t, channelNames(NewEngineFromConfig(cfg)),
		"enabled: false must suppress every config-driven channel")

	// ...but must not silence a deployment that never had a block, because
	// "absent" and "explicitly false" are the same value in Go.
	t.Setenv(envSlackWebhook, "https://hooks.slack.example/from-env")
	require.Equal(t, []string{"slack"}, channelNames(NewEngineFromConfig(cfg)),
		"an environment-configured channel must survive enabled: false")
}

// TestEngineFromConfig_PartialCredentialsRegisterNothing: a half-filled channel
// must not be registered from the file, matching the all-or-nothing rule the
// environment path has always used.
func TestEngineFromConfig_PartialCredentialsRegisterNothing(t *testing.T) {
	clearNotificationEnv(t)

	cfg := fullyConfigured()
	cfg.Notifications.Channels.Slack.WebhookURL = ""    // enabled but no webhook
	cfg.Notifications.Channels.Telegram.ChatID = ""     // token without chat id
	cfg.Notifications.Channels.Email.SMTP.Password = "" // server+user without password
	cfg.Notifications.Channels.Discord.Enabled = false  // credential present, switched off

	require.Empty(t, channelNames(NewEngineFromConfig(cfg)))
}

// TestEngineFromConfig_RulesAreRegistered is the other half of the discarded
// block: the rules list was "read by nothing at all".
func TestEngineFromConfig_RulesAreRegistered(t *testing.T) {
	clearNotificationEnv(t)

	cfg := fullyConfigured()
	cfg.Notifications.Rules = []config.NotificationRuleConfig{
		{Name: "Critical Task Failures", Condition: "type==error",
			Channels: []string{"slack", "email"}, Priority: "urgent", Enabled: true},
		{Name: "Workflow Completions", Condition: "type==success",
			Channels: []string{"slack"}, Priority: "medium", Enabled: false},
	}

	engine := NewEngineFromConfig(cfg)

	engine.mutex.RLock()
	rules := append([]NotificationRule(nil), engine.rules...)
	engine.mutex.RUnlock()

	require.Len(t, rules, 2)
	require.Equal(t, "Critical Task Failures", rules[0].Name)
	require.Equal(t, "type==error", rules[0].Condition)
	require.Equal(t, []string{"slack", "email"}, rules[0].Channels)
	require.Equal(t, NotificationPriorityUrgent, rules[0].Priority)
	require.True(t, rules[0].Enabled)
	require.False(t, rules[1].Enabled)
	require.Equal(t, 1, engine.countActiveRules())
}

// TestEngineFromConfig_RulesDriveDispatch is the end-to-end proof that the
// rules do something: a notification naming no channel must reach the channels
// the matching rule names.
func TestEngineFromConfig_RulesDriveDispatch(t *testing.T) {
	clearNotificationEnv(t)

	cfg := fullyConfigured()
	cfg.Notifications.Rules = []config.NotificationRuleConfig{
		{Name: "Critical Task Failures", Condition: "type==error",
			Channels: []string{"slack"}, Priority: "urgent", Enabled: true},
	}
	engine := NewEngineFromConfig(cfg)

	notif := &Notification{
		Title: "t", Message: "m",
		Type:     NotificationTypeError,
		Priority: NotificationPriorityLow,
	}
	sent := *notif
	sent.Channels = nil
	engine.applyRules(&sent)

	require.Equal(t, []string{"slack"}, sent.Channels,
		"the rule must select the channel the operator configured")
	require.Equal(t, NotificationPriorityUrgent, sent.Priority,
		"the rule must raise the priority the operator configured")
}

// TestParsePriority pins the token mapping, including the fallback. An
// unrecognised token must not become "", which getPriorityLevel scores as 0 and
// which would leave the rule unable to raise any priority.
func TestParsePriority(t *testing.T) {
	for in, want := range map[string]NotificationPriority{
		"low": NotificationPriorityLow, "medium": NotificationPriorityMedium,
		"high": NotificationPriorityHigh, "urgent": NotificationPriorityUrgent,
		"URGENT": NotificationPriorityUrgent, " high ": NotificationPriorityHigh,
		"": NotificationPriorityMedium, "nonsense": NotificationPriorityMedium,
	} {
		require.Equal(t, want, parsePriority(in), "parsePriority(%q)", in)
	}
}
