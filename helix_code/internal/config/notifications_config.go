package config

// Notification configuration.
//
// WHAT CHANGED AND WHY
// --------------------
// `notifications:` has shipped in config/config.yaml, config/replica-8081.yaml,
// config/replica-8082.yaml, config/test-config.yaml and friends since long
// before this file existed, and Config had no field for it. Viper discards any
// key with no matching `mapstructure` tag, so the whole block — the enable
// flag, every rule, every channel credential — was thrown away on load. The
// channels an operator actually got were built solely from HELIX_* environment
// variables in cmd/other_commands.go, and the rules list was read by nothing at
// all. Editing the file changed nothing; strict.go's inertConfigKeys register
// said so out loud at startup.
//
// These types give the block somewhere to land. The consuming end is
// internal/notification.NewEngineFromConfig, which registers channels and rules
// from it — with environment variables still winning, so deployments that
// configure notifications purely through HELIX_* keep behaving exactly as they
// did (see that function's contract).
//
// SHAPE IS DERIVED, NOT INVENTED. Every field below corresponds to a key that
// one of the shipped config files actually sets; the set was enumerated by
// reading each file through a bare viper instance and collecting the
// `notifications.*` leaves. That completeness matters: strict.go rejects a key
// the struct cannot accept, so a subkey present in a shipped file but missing
// here would turn startup into a hard failure rather than a warning.
//
// Keys present ONLY in config/production-config.yaml (icon_emoji, parse_mode,
// recipients.critical, templates.*) are deliberately absent. That file does not
// parse as YAML at all — it ends with a stray markdown fence, so viper refuses
// it — and declaring fields for keys no loadable file sets would be inventing
// surface rather than deriving it. Fixing that file is a separate change; when
// it is fixed, its extra keys must be added here in the same commit.

// NotificationsConfig is the `notifications:` block.
type NotificationsConfig struct {
	// Enabled gates the CONFIG-DRIVEN channels and rules only. It deliberately
	// does NOT gate the environment-variable path: a deployment that configures
	// notifications purely through HELIX_* has no `notifications:` block at
	// all, which in Go is indistinguishable from an explicit `enabled: false`.
	// Letting this flag switch off the env path would therefore silence every
	// such deployment the moment this field was introduced.
	Enabled bool `mapstructure:"enabled"`

	// Rules map an event condition onto a set of channels and a priority.
	// Applied by NotificationEngine.applyRules on every SendNotification.
	Rules []NotificationRuleConfig `mapstructure:"rules"`

	// Channels carry the per-channel credentials and presentation settings.
	Channels NotificationChannelsConfig `mapstructure:"channels"`
}

// NotificationRuleConfig is one entry of `notifications.rules`.
type NotificationRuleConfig struct {
	Name      string   `mapstructure:"name"`
	Condition string   `mapstructure:"condition"`
	Channels  []string `mapstructure:"channels"`
	Priority  string   `mapstructure:"priority"`
	Enabled   bool     `mapstructure:"enabled"`
	Template  string   `mapstructure:"template"`
}

// NotificationChannelsConfig is `notifications.channels`.
type NotificationChannelsConfig struct {
	Slack    SlackNotificationConfig    `mapstructure:"slack"`
	Telegram TelegramNotificationConfig `mapstructure:"telegram"`
	Email    EmailNotificationConfig    `mapstructure:"email"`
	Discord  DiscordNotificationConfig  `mapstructure:"discord"`
}

// SlackNotificationConfig is `notifications.channels.slack`.
type SlackNotificationConfig struct {
	Enabled    bool   `mapstructure:"enabled"`
	WebhookURL string `mapstructure:"webhook_url"`
	Channel    string `mapstructure:"channel"`
	Username   string `mapstructure:"username"`

	// Timeout is declared so the shipped key parses, but nothing reads it:
	// NewSlackChannel takes no timeout. Registered in inertConfigKeys so the
	// gap is announced at startup rather than hidden here.
	Timeout int `mapstructure:"timeout"`
}

// TelegramNotificationConfig is `notifications.channels.telegram`.
type TelegramNotificationConfig struct {
	Enabled  bool   `mapstructure:"enabled"`
	BotToken string `mapstructure:"bot_token"`
	ChatID   string `mapstructure:"chat_id"`

	// Timeout: declared-but-unconsumed, see SlackNotificationConfig.Timeout.
	Timeout int `mapstructure:"timeout"`
}

// EmailNotificationConfig is `notifications.channels.email`.
type EmailNotificationConfig struct {
	Enabled bool                        `mapstructure:"enabled"`
	SMTP    EmailSMTPNotificationConfig `mapstructure:"smtp"`

	// Recipients: declared-but-unconsumed. NewEmailChannel derives the
	// destination from the SMTP account and takes no recipient list, so this
	// subtree parses but changes nothing. Registered in inertConfigKeys.
	Recipients EmailRecipientsNotificationConfig `mapstructure:"recipients"`

	// Timeout: declared-but-unconsumed, see SlackNotificationConfig.Timeout.
	Timeout int `mapstructure:"timeout"`
}

// EmailSMTPNotificationConfig is `notifications.channels.email.smtp`.
type EmailSMTPNotificationConfig struct {
	Server   string `mapstructure:"server"`
	Port     int    `mapstructure:"port"`
	Username string `mapstructure:"username"`
	Password string `mapstructure:"password"`
	From     string `mapstructure:"from"`

	// TLS: declared-but-unconsumed. EmailChannel.Send negotiates its own
	// transport security and takes no flag. Registered in inertConfigKeys.
	TLS bool `mapstructure:"tls"`
}

// EmailRecipientsNotificationConfig is `notifications.channels.email.recipients`.
type EmailRecipientsNotificationConfig struct {
	Default []string `mapstructure:"default"`
}

// DiscordNotificationConfig is `notifications.channels.discord`.
type DiscordNotificationConfig struct {
	Enabled    bool   `mapstructure:"enabled"`
	WebhookURL string `mapstructure:"webhook_url"`

	// Timeout: declared-but-unconsumed, see SlackNotificationConfig.Timeout.
	Timeout int `mapstructure:"timeout"`
}
