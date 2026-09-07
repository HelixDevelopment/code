package cmd

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"syscall"
	"time"

	"dev.helix.code/internal/config"
	"dev.helix.code/internal/database"
	"dev.helix.code/internal/llm"
	"dev.helix.code/internal/notification"
	"dev.helix.code/internal/redis"
	"dev.helix.code/internal/server"
	"github.com/spf13/cobra"
)

var serverCmd = &cobra.Command{
	Use:   "server",
	Short: trc("cmd_server_short", nil),
	Long:  trc("cmd_server_long", nil),
	Run: func(cmd *cobra.Command, args []string) {
		ctx := context.Background()
		// Speed programme P2-T07: config.Get() caches the process config.
		cfg, err := config.Get()
		if err != nil {
			fmt.Fprintln(os.Stderr, tr(ctx, "cmd_err_config", map[string]any{"Error": err.Error()}))
			return
		}

		var db *database.Database
		if cfg.Database.Host != "" {
			db, err = database.New(cfg.Database)
			if err != nil {
				fmt.Fprintln(os.Stderr, tr(ctx, "cmd_err_database_unavailable", map[string]any{"Error": err.Error()}))
			} else {
				defer db.Close()
			}
		}

		var rds *redis.Client
		if cfg.Redis.Enabled && cfg.Redis.Host != "" {
			rds, err = redis.NewClient(&cfg.Redis)
			if err != nil {
				fmt.Fprintln(os.Stderr, tr(ctx, "cmd_err_redis_unavailable", map[string]any{"Error": err.Error()}))
			} else {
				defer rds.Close()
			}
		}

		srv := server.New(cfg, db, rds)

		errChan := make(chan error, 1)
		go func() {
			if err := srv.Start(); err != nil {
				errChan <- err
			}
		}()

		quit := make(chan os.Signal, 1)
		signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

		select {
		case err := <-errChan:
			fmt.Fprintln(os.Stderr, tr(ctx, "cmd_err_server", map[string]any{"Error": err.Error()}))
		case sig := <-quit:
			fmt.Println("\n" + tr(ctx, "cmd_server_received_signal", map[string]any{"Signal": sig.String()}))
		}

		shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			fmt.Fprintln(os.Stderr, tr(ctx, "cmd_err_shutdown", map[string]any{"Error": err.Error()}))
		}
		fmt.Println(tr(ctx, "cmd_server_stopped", nil))
	},
}

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: trc("cmd_version_short", nil),
	Long:  trc("cmd_version_long", nil),
	Run: func(cmd *cobra.Command, args []string) {
		ctx := context.Background()
		fmt.Println(tr(ctx, "cmd_version_platform_name", nil))
		fmt.Println(tr(ctx, "cmd_version_version", map[string]any{"Version": "1.0.0"}))
		fmt.Println(tr(ctx, "cmd_version_build", map[string]any{"Build": "2025.01.20"}))
		fmt.Println(tr(ctx, "cmd_version_providers", map[string]any{"Total": 29, "Cloud": 18, "Local": 11}))
		fmt.Println(tr(ctx, "cmd_version_token_context", map[string]any{"Context": "2M"}))
		fmt.Println(tr(ctx, "cmd_version_license", map[string]any{"License": "MIT"}))
	},
}

var generateCmd = &cobra.Command{
	Use:   "generate [prompt]",
	Short: trc("cmd_generate_short", nil),
	Long:  trc("cmd_generate_long", nil),
	Run: func(cmd *cobra.Command, args []string) {
		ctx0 := context.Background()
		if len(args) == 0 {
			fmt.Fprintln(os.Stderr, tr(ctx0, "cmd_generate_need_prompt", nil))
			return
		}
		prompt := args[0]

		// HXC-002-F3-04: route `generate` through the SERVER's provider
		// resolution semantics (server.ResolveLLMProvider) so the CLI and the
		// HTTP API can never drift on which local route a default request
		// takes — a config `default_provider: "local"` resolves to the
		// helixllm coder route here exactly as it does for
		// POST /api/v1/llm/generate. Pre-fix this path built a ModelManager
		// with ZERO registered providers, so SelectOptimalModel always
		// failed with "no models available".
		cfg, err := config.Get()
		if err != nil {
			fmt.Fprintln(os.Stderr, tr(ctx0, "cmd_err_config", map[string]any{"Error": err.Error()}))
			return
		}
		// Same cloud gate the server applies in server.New — a CLI generate
		// must refuse cloud providers exactly when the server would.
		llm.SetCloudEnabled(cfg.LLM.Cloud.Enabled)

		// Pass the EMPTY flag slot, NOT cfg.LLM.DefaultProvider. The first
		// argument is the FLAG source in the resolver's flag > env > config
		// precedence chain (the Selector in internal/llm/provider_factory.go).
		// Passing the config value here promoted config ABOVE HELIX_LLM_PROVIDER
		// and inverted the very precedence this path exists to share with the
		// server: with HELIX_LLM_PROVIDER=anthropic exported, the server honours
		// it while `generate` silently used the config default -- exactly the
		// CLI/API drift the comment above says is eliminated. resolveLLMProvider
		// consults cfg.LLM.DefaultProvider itself in the CONFIG slot
		// (HXC-002-F3-01), so the configured default still applies -- at the
		// correct precedence, and only when neither flag nor env named a
		// provider. The former empty-default early-exit is gone for the same
		// reason: an empty config default is not an error when HELIX_LLM_PROVIDER
		// names one, and when nothing names one the resolver falls back exactly
		// as the server does.
		mgr, prov, err := newGenerateManager("")
		if err != nil {
			// The shared resolution path classifies an unresolvable — or
			// cloud-gated — provider name that came from HELIX_LLM_PROVIDER /
			// llm.default_provider as a misconfiguration of the RESOLVING
			// PROCESS. Over HTTP that is a 5xx and the operator is someone
			// else; here the resolving process IS this CLI, so the same
			// condition has to be reported as the USER'S OWN environment and
			// config file rather than as a server-side fault they cannot see.
			// CONST-046: rendered through the tr() seam like every other
			// user-facing string in this command, never a hardcoded literal.
			if server.IsProviderMisconfiguration(err) {
				fmt.Fprintln(os.Stderr, tr(ctx0, "cmd_generate_provider_misconfigured", map[string]any{"Error": err.Error()}))
				return
			}
			fmt.Fprintln(os.Stderr, tr(ctx0, "cmd_generate_provider_unavailable", map[string]any{"Error": err.Error()}))
			return
		}
		defer func() { _ = prov.Close() }()

		modelInfo, err := mgr.SelectOptimalModel(llm.ModelSelectionCriteria{
			TaskType:          "text-generation",
			QualityPreference: "balanced",
		})
		if err != nil {
			fmt.Fprintln(os.Stderr, tr(ctx0, "cmd_generate_no_models", map[string]any{"Error": err.Error()}))
			return
		}

		serving, err := mgr.GetProviderForModel(modelInfo.Name, prov.GetType())
		if err != nil {
			fmt.Fprintln(os.Stderr, tr(ctx0, "cmd_generate_provider_unavailable", map[string]any{"Error": err.Error()}))
			return
		}

		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()

		request := &llm.LLMRequest{
			Model: modelInfo.Name,
			Messages: []llm.Message{
				{Role: "user", Content: prompt},
			},
			MaxTokens:   4096,
			Temperature: 0.7,
		}
		response, err := serving.Generate(ctx, request)
		if err != nil {
			fmt.Fprintln(os.Stderr, tr(ctx0, "cmd_generate_failed", map[string]any{"Error": err.Error()}))
			return
		}
		fmt.Println(response.Content)
	},
}

var testCmd = &cobra.Command{
	Use:   "test",
	Short: trc("cmd_test_short", nil),
	Long:  trc("cmd_test_long", nil),
	Run: func(cmd *cobra.Command, args []string) {
		testArgs := []string{"test", "-v"}
		if len(args) > 0 {
			testArgs = append(testArgs, args...)
		} else {
			testArgs = append(testArgs, "./...")
		}
		c := exec.Command("go", testArgs...)
		c.Stdout = os.Stdout
		c.Stderr = os.Stderr
		if err := c.Run(); err != nil {
			fmt.Fprintln(os.Stderr, tr(context.Background(), "cmd_test_failed", map[string]any{"Error": err.Error()}))
			os.Exit(1)
		}
	},
}

var workerCmd = &cobra.Command{
	Use:   "worker",
	Short: trc("cmd_worker_short", nil),
	Long:  trc("cmd_worker_long", nil),
	Run: func(cmd *cobra.Command, args []string) {
		ctx := context.Background()
		// Speed programme P2-T07: config.Get() caches the process config.
		cfg, err := config.Get()
		if err != nil {
			fmt.Fprintln(os.Stderr, tr(ctx, "cmd_err_config", map[string]any{"Error": err.Error()}))
			return
		}

		if cfg.Database.Host == "" {
			fmt.Println(tr(ctx, "cmd_worker_needs_database", nil))
			fmt.Println(tr(ctx, "cmd_worker_set_database", nil))
			return
		}

		fmt.Println(tr(ctx, "cmd_worker_config_summary", map[string]any{
			"HealthTTL":     cfg.Workers.HealthTTL,
			"MaxConcurrent": cfg.Workers.MaxConcurrentTasks,
		}))
		fmt.Println(tr(ctx, "cmd_worker_use_subcommands", nil))
	},
}

var notifyCmd = &cobra.Command{
	Use:   "notify [message]",
	Short: trc("cmd_notify_short", nil),
	Long:  trc("cmd_notify_long", nil),
	Run: func(cmd *cobra.Command, args []string) {
		if len(args) == 0 {
			fmt.Fprintln(os.Stderr, tr(context.Background(), "cmd_notify_need_message", nil))
			return
		}
		message := args[0]

		// Channels and rules come from the `notifications:` config block AND
		// the HELIX_* environment variables, with the environment winning —
		// see notification.NewEngineFromConfig for the precedence contract.
		// Before this, the block was discarded by viper and only the
		// environment produced a channel.
		//
		// A config that fails to load is not fatal here: notify's job is to
		// deliver a message, and the environment-only path it used before
		// still works without any config at all, so fall back to it rather
		// than refusing to send.
		cfg, cfgErr := config.Get()
		if cfgErr != nil {
			fmt.Fprintln(os.Stderr, tr(context.Background(), "cmd_notify_config_unavailable",
				map[string]any{"Error": cfgErr.Error()}))
			cfg = nil
		}
		engine := notification.NewEngineFromConfig(cfg)

		notif := &notification.Notification{
			Title:    tr(context.Background(), "cmd_notify_title", nil),
			Message:  message,
			Type:     notification.NotificationTypeInfo,
			Priority: notification.NotificationPriorityMedium,
		}

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		if err := engine.SendNotification(ctx, notif); err != nil {
			fmt.Fprintln(os.Stderr, tr(ctx, "cmd_notify_failed", map[string]any{"Error": err.Error()}))
			return
		}
		fmt.Println(tr(ctx, "cmd_notify_dispatched", nil))
	},
}

// newGenerateManager resolves defaultProvider through the SERVER's provider
// resolution (server.ResolveLLMProvider — local default routes to the
// helixllm coder sidecar, cloud names are subject to the cloud gate) and
// registers the constructed provider into a fresh ModelManager so model
// selection runs over a REAL provider catalog. The returned provider is
// owned by the caller (Close it); on a registration failure it is closed
// here so the caller never leaks it.
func newGenerateManager(defaultProvider string) (*llm.ModelManager, llm.Provider, error) {
	prov, err := server.ResolveLLMProvider(defaultProvider, "")
	if err != nil {
		return nil, nil, err
	}
	mgr := llm.NewModelManager()
	if err := mgr.RegisterProvider(prov); err != nil {
		_ = prov.Close()
		return nil, nil, err
	}
	return mgr, prov, nil
}

// HXC-002-F3-04: generateCmd was defined but NEVER registered on rootCmd, so
// `helix generate` answered `unknown command "generate" for "helix"`. The
// other commands in this file (server/version/test/worker/notify) have their
// own registration sites or are tracked separately; only generate is wired
// here.
func init() {
	rootCmd.AddCommand(generateCmd)
}
