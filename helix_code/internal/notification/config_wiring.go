package notification

import (
	"os"
	"strings"

	"dev.helix.code/internal/config"
)

// Environment variables that configure notification channels.
//
// These are the variables cmd/other_commands.go has always read, named here
// once so the config path and the environment path cannot drift apart.
const (
	envSlackWebhook   = "HELIX_SLACK_WEBHOOK_URL"
	envDiscordWebhook = "HELIX_DISCORD_WEBHOOK_URL"
	envTelegramToken  = "HELIX_TELEGRAM_BOT_TOKEN"
	envTelegramChatID = "HELIX_TELEGRAM_CHAT_ID"
	envEmailServer    = "HELIX_EMAIL_SMTP_SERVER"
	envEmailUsername  = "HELIX_EMAIL_USERNAME"
	envEmailPassword  = "HELIX_EMAIL_PASSWORD"
)

// Fallbacks that reproduce the values cmd/other_commands.go passed positionally
// before the config block existed, so an environment-only deployment keeps
// producing byte-identical channels.
const (
	defaultSlackChannel  = "helixcode"
	defaultSlackUsername = "HelixCode"
	defaultSMTPPort      = 587
)

// NewEngineFromConfig builds a NotificationEngine from the `notifications:`
// config block and the HELIX_* environment variables.
//
// WHAT THIS REPLACES
// ------------------
// Channels used to be assembled inline in cmd/other_commands.go from
// environment variables alone. The `notifications:` block shipped in
// config/config.yaml and its siblings reached nothing: Config had no field for
// it, so viper discarded it on load. An operator who enabled Slack in the file
// and pasted a webhook there got silence, with no error to explain why.
//
// PRECEDENCE: ENVIRONMENT WINS
// ----------------------------
// For every channel, an environment variable that is set decides the outcome,
// exactly as it did before this function existed. The config block is consulted
// only where the environment is silent. Concretely, per channel:
//
//   - env credential present  → register from the environment. Identical to the
//     pre-change behaviour, including the positional defaults above, and
//     unaffected by `enabled:` in the file.
//   - env absent, block enables the channel and supplies a credential →
//     register from the config. This is the new capability.
//   - otherwise → not registered.
//
// That ordering is what keeps environment-only deployments bit-identical: such
// a deployment has no `notifications:` block at all, every config branch is
// therefore empty, and only the environment branch fires. A test pins this.
//
// WHY `enabled:` DOES NOT GATE THE ENVIRONMENT PATH
// -------------------------------------------------
// A deployment configured purely through HELIX_* has no block, so
// Notifications.Enabled is Go's zero value — false — and is indistinguishable
// from an operator writing `enabled: false`. Letting the flag switch off the
// environment path would therefore have silenced every existing environment-only
// deployment the moment the field was introduced. It gates the config-driven
// channels and rules, which is what an operator editing that file is asking
// about.
//
// A nil cfg is accepted and yields the environment-only engine, so a caller
// whose config failed to load still gets the behaviour it had before.
func NewEngineFromConfig(cfg *config.Config) *NotificationEngine {
	engine := NewNotificationEngine()

	var n config.NotificationsConfig
	if cfg != nil {
		n = cfg.Notifications
	}
	ch := n.Channels

	// --- Slack ----------------------------------------------------------
	if webhook := os.Getenv(envSlackWebhook); webhook != "" {
		engine.RegisterChannel(NewSlackChannel(webhook, defaultSlackChannel, defaultSlackUsername))
	} else if n.Enabled && ch.Slack.Enabled && ch.Slack.WebhookURL != "" {
		engine.RegisterChannel(NewSlackChannel(
			ch.Slack.WebhookURL,
			firstNonEmpty(ch.Slack.Channel, defaultSlackChannel),
			firstNonEmpty(ch.Slack.Username, defaultSlackUsername),
		))
	}

	// --- Discord --------------------------------------------------------
	if webhook := os.Getenv(envDiscordWebhook); webhook != "" {
		engine.RegisterChannel(NewDiscordChannel(webhook))
	} else if n.Enabled && ch.Discord.Enabled && ch.Discord.WebhookURL != "" {
		engine.RegisterChannel(NewDiscordChannel(ch.Discord.WebhookURL))
	}

	// --- Telegram -------------------------------------------------------
	// Both halves come from the environment together or not at all: a token
	// from the environment paired with a chat id from the file would be a
	// silent half-merge nobody asked for.
	envToken, envChat := os.Getenv(envTelegramToken), os.Getenv(envTelegramChatID)
	if envToken != "" && envChat != "" {
		engine.RegisterChannel(NewTelegramChannel(envToken, envChat))
	} else if n.Enabled && ch.Telegram.Enabled && ch.Telegram.BotToken != "" && ch.Telegram.ChatID != "" {
		engine.RegisterChannel(NewTelegramChannel(ch.Telegram.BotToken, ch.Telegram.ChatID))
	}

	// --- Email ----------------------------------------------------------
	// Same all-or-nothing rule, for the same reason. The pre-change code used
	// the SMTP username as the From address; the config path honours an
	// explicit `from:` and falls back to that same behaviour.
	envServer := os.Getenv(envEmailServer)
	envUser := os.Getenv(envEmailUsername)
	envPass := os.Getenv(envEmailPassword)
	switch {
	case envServer != "" && envUser != "" && envPass != "":
		engine.RegisterChannel(NewEmailChannel(envServer, defaultSMTPPort, envUser, envPass, envUser))
	case n.Enabled && ch.Email.Enabled &&
		ch.Email.SMTP.Server != "" && ch.Email.SMTP.Username != "" && ch.Email.SMTP.Password != "":
		port := ch.Email.SMTP.Port
		if port == 0 {
			port = defaultSMTPPort
		}
		engine.RegisterChannel(NewEmailChannel(
			ch.Email.SMTP.Server,
			port,
			ch.Email.SMTP.Username,
			ch.Email.SMTP.Password,
			firstNonEmpty(ch.Email.SMTP.From, ch.Email.SMTP.Username),
		))
	}

	// --- Rules ----------------------------------------------------------
	// Rules had no environment equivalent — nothing read them at all — so
	// there is no precedence question here. A rule the operator marked
	// disabled is still registered, exactly as AddRule callers elsewhere do:
	// applyRules skips it at dispatch time, and keeping it visible means
	// GetChannelStats can report total vs active honestly.
	if n.Enabled {
		for _, r := range n.Rules {
			engine.AddRule(NotificationRule{
				Name:      r.Name,
				Condition: r.Condition,
				Channels:  append([]string(nil), r.Channels...),
				Priority:  parsePriority(r.Priority),
				Enabled:   r.Enabled,
				Template:  r.Template,
			})
		}
	}

	return engine
}

// parsePriority maps the config file's priority token onto the engine's
// closed set. An unrecognised or absent token becomes medium — the same
// priority cmd/other_commands.go has always stamped on a notification — rather
// than the empty string, which getPriorityLevel scores as 0 and which would
// therefore make a rule silently unable to raise a notification's priority.
func parsePriority(s string) NotificationPriority {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case string(NotificationPriorityLow):
		return NotificationPriorityLow
	case string(NotificationPriorityHigh):
		return NotificationPriorityHigh
	case string(NotificationPriorityUrgent):
		return NotificationPriorityUrgent
	default:
		return NotificationPriorityMedium
	}
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}
