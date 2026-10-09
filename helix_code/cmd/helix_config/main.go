package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"github.com/spf13/viper"
	"gopkg.in/yaml.v3"

	"dev.helix.code/cmd/helix_config/i18n"
	"dev.helix.code/internal/config"
	"dev.helix.code/internal/llm"
	"dev.helix.code/internal/provider"
)

// translator resolves CONST-046 message IDs for every user-facing
// string emitted by this CLI. Defaults to i18n.NoopTranslator{} (loud
// message-ID echo) so unit tests + ad-hoc invocations remain obvious.
// helix_code wires a real *i18nadapter.Translator at boot via
// SetTranslator (round-108 §11.4 anti-bluff sweep, 2026-05-18).
//
// A package-level variable is the chosen DI seam because cobra's
// RunE signature (func(*cobra.Command, []string) error) does not
// support extra parameters without restructuring the command tree —
// global injection matches cobra's own use of package-level state
// (viper, the *cobra.Command tree itself) and keeps the migration
// minimally invasive.
var translator i18n.Translator = i18n.NoopTranslator{}

// SetTranslator wires a CONST-046-compliant Translator. Passing nil
// resets to i18n.NoopTranslator{} (loud echo) — never silently
// disables translation lookup (which would be a §11.4 PASS-bluff at
// the i18n injection layer).
func SetTranslator(tr i18n.Translator) {
	if tr == nil {
		translator = i18n.NoopTranslator{}
		return
	}
	translator = tr
}

// tr is the internal CONST-046 resolver used by every user-facing
// string emission in this file. It NEVER returns an error to the
// caller — translation failures degrade to the message ID itself
// (matching NoopTranslator behaviour) so production output remains
// loud + obvious instead of silently empty.
func tr(ctx context.Context, msgID string, data map[string]any) string {
	if translator == nil {
		translator = i18n.NoopTranslator{}
	}
	out, err := translator.T(ctx, msgID, data)
	if err != nil || out == "" {
		return msgID
	}
	return out
}

var (
	configFile   string
	format       string
	outputFormat string
	sessionID    string
	user         string
	verbose      bool
	dryRun       bool
	quiet        bool
	noColor      bool
	interactive  bool
	force        bool
	backup       bool
	timeout      time.Duration
	maxRetries   int
	showSecrets  bool
	noValidate   bool
	strictMode   bool
	prettyPrint  bool
	sortKeys     bool
)

// CLI version information
var (
	version   = "1.0.0"
	buildTime = "unknown"
	gitCommit = "unknown"
)

func main() {
	rootCmd := createRootCommand()
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

// createRootCommand creates the root CLI command
func createRootCommand() *cobra.Command {
	ctx := context.Background()
	rootCmd := &cobra.Command{
		Use:     "helix-config",
		Short:   tr(ctx, "helix_config_root_short", nil),
		Long:    tr(ctx, "helix_config_root_long", nil),
		Version: fmt.Sprintf("%s (built: %s, commit: %s)", version, buildTime, gitCommit),
		PersistentPreRun: func(cmd *cobra.Command, args []string) {
			setupViper()
			loadConfig()
		},
	}

	// Global flags
	rootCmd.PersistentFlags().StringVarP(&configFile, "config", "c", "", tr(ctx, "helix_config_flag_config", nil))
	rootCmd.PersistentFlags().StringVarP(&format, "format", "f", "auto", tr(ctx, "helix_config_flag_format", nil))
	rootCmd.PersistentFlags().StringVarP(&outputFormat, "output", "o", "json", tr(ctx, "helix_config_flag_output", nil))
	rootCmd.PersistentFlags().StringVar(&sessionID, "session-id", "", tr(ctx, "helix_config_flag_session_id", nil))
	rootCmd.PersistentFlags().StringVar(&user, "user", "", tr(ctx, "helix_config_flag_user", nil))
	rootCmd.PersistentFlags().BoolVarP(&verbose, "verbose", "v", false, tr(ctx, "helix_config_flag_verbose", nil))
	rootCmd.PersistentFlags().BoolVar(&dryRun, "dry-run", false, tr(ctx, "helix_config_flag_dry_run", nil))
	rootCmd.PersistentFlags().BoolVarP(&quiet, "quiet", "q", false, tr(ctx, "helix_config_flag_quiet", nil))
	rootCmd.PersistentFlags().BoolVar(&noColor, "no-color", false, tr(ctx, "helix_config_flag_no_color", nil))
	rootCmd.PersistentFlags().BoolVarP(&interactive, "interactive", "i", false, tr(ctx, "helix_config_flag_interactive", nil))
	rootCmd.PersistentFlags().BoolVarP(&force, "force", "F", false, tr(ctx, "helix_config_flag_force", nil))
	rootCmd.PersistentFlags().BoolVar(&backup, "backup", true, tr(ctx, "helix_config_flag_backup", nil))
	rootCmd.PersistentFlags().DurationVar(&timeout, "timeout", 30*time.Second, tr(ctx, "helix_config_flag_timeout", nil))
	rootCmd.PersistentFlags().IntVar(&maxRetries, "max-retries", 3, tr(ctx, "helix_config_flag_max_retries", nil))
	rootCmd.PersistentFlags().BoolVar(&showSecrets, "show-secrets", false, tr(ctx, "helix_config_flag_show_secrets", nil))
	rootCmd.PersistentFlags().BoolVar(&noValidate, "no-validate", false, tr(ctx, "helix_config_flag_no_validate", nil))
	rootCmd.PersistentFlags().BoolVar(&strictMode, "strict", false, tr(ctx, "helix_config_flag_strict", nil))
	rootCmd.PersistentFlags().BoolVar(&prettyPrint, "pretty", true, tr(ctx, "helix_config_flag_pretty", nil))
	rootCmd.PersistentFlags().BoolVar(&sortKeys, "sort-keys", true, tr(ctx, "helix_config_flag_sort_keys", nil))

	// Bind flags to viper
	bindFlags(rootCmd.PersistentFlags())

	// Add subcommands
	rootCmd.AddCommand(createShowCommand())
	rootCmd.AddCommand(createGetCommand())
	rootCmd.AddCommand(createSetCommand())
	rootCmd.AddCommand(createDeleteCommand())
	rootCmd.AddCommand(createValidateCommand())
	rootCmd.AddCommand(createExportCommand())
	rootCmd.AddCommand(createImportCommand())
	rootCmd.AddCommand(createBackupCommand())
	rootCmd.AddCommand(createRestoreCommand())
	rootCmd.AddCommand(createResetCommand())
	rootCmd.AddCommand(createReloadCommand())
	rootCmd.AddCommand(createWatchCommand())
	rootCmd.AddCommand(createMigrateCommand())
	rootCmd.AddCommand(createTemplateCommand())
	rootCmd.AddCommand(createHistoryCommand())
	rootCmd.AddCommand(createSchemaCommand())
	rootCmd.AddCommand(createCompletionCommand())
	rootCmd.AddCommand(createVersionCommand())
	rootCmd.AddCommand(createInfoCommand())
	rootCmd.AddCommand(createStatusCommand())
	rootCmd.AddCommand(createDiffCommand())
	rootCmd.AddCommand(createMergeCommand())
	rootCmd.AddCommand(createSearchCommand())
	rootCmd.AddCommand(createBenchmarkCommand())
	rootCmd.AddCommand(createRegenerateAliasesCommand())

	return rootCmd
}

// Subcommands

func createShowCommand() *cobra.Command {
	ctx := context.Background()
	cmd := &cobra.Command{
		Use:   "show",
		Short: tr(ctx, "helix_config_cmd_show_short", nil),
		Long:  tr(ctx, "helix_config_cmd_show_long", nil),
		RunE:  runShowCommand,
	}

	cmd.Flags().StringP("section", "s", "", tr(ctx, "helix_config_flag_section_show", nil))
	cmd.Flags().Bool("masked", true, tr(ctx, "helix_config_flag_masked", nil))
	cmd.Flags().Bool("defaults", false, tr(ctx, "helix_config_flag_defaults_show", nil))
	cmd.Flags().Bool("flattened", false, tr(ctx, "helix_config_flag_flattened", nil))

	return cmd
}

func createGetCommand() *cobra.Command {
	ctx := context.Background()
	cmd := &cobra.Command{
		Use:   "get <key>",
		Short: tr(ctx, "helix_config_cmd_get_short", nil),
		Long:  tr(ctx, "helix_config_cmd_get_long", nil),
		Args:  cobra.ExactArgs(1),
		RunE:  runGetCommand,
	}

	cmd.Flags().Bool("type", false, tr(ctx, "helix_config_flag_type_show", nil))
	cmd.Flags().Bool("source", false, tr(ctx, "helix_config_flag_source_show", nil))
	cmd.Flags().Bool("valid", false, tr(ctx, "helix_config_flag_valid", nil))

	return cmd
}

func createSetCommand() *cobra.Command {
	ctx := context.Background()
	cmd := &cobra.Command{
		Use:   "set <key> <value>",
		Short: tr(ctx, "helix_config_cmd_set_short", nil),
		Long:  tr(ctx, "helix_config_cmd_set_long", nil),
		Args:  cobra.ExactArgs(2),
		RunE:  runSetCommand,
	}

	cmd.Flags().Bool("create", false, tr(ctx, "helix_config_flag_create", nil))
	cmd.Flags().Bool("validate", true, tr(ctx, "helix_config_flag_validate_set", nil))
	cmd.Flags().String("type", "", tr(ctx, "helix_config_flag_type_force", nil))
	cmd.Flags().String("format", "", tr(ctx, "helix_config_flag_format_parse", nil))
	cmd.Flags().Bool("backup", true, tr(ctx, "helix_config_flag_backup_set", nil))
	cmd.Flags().Bool("restart", false, tr(ctx, "helix_config_flag_restart", nil))

	return cmd
}

func createDeleteCommand() *cobra.Command {
	ctx := context.Background()
	cmd := &cobra.Command{
		Use:   "delete <key>",
		Short: tr(ctx, "helix_config_cmd_delete_short", nil),
		Long:  tr(ctx, "helix_config_cmd_delete_long", nil),
		Args:  cobra.ExactArgs(1),
		RunE:  runDeleteCommand,
	}

	cmd.Flags().Bool("reset", true, tr(ctx, "helix_config_flag_reset_delete", nil))
	cmd.Flags().Bool("confirm", false, tr(ctx, "helix_config_flag_confirm_delete", nil))
	cmd.Flags().Bool("backup", true, tr(ctx, "helix_config_flag_backup_delete", nil))

	return cmd
}

func createValidateCommand() *cobra.Command {
	ctx := context.Background()
	cmd := &cobra.Command{
		Use:   "validate [file]",
		Short: tr(ctx, "helix_config_cmd_validate_short", nil),
		Long:  tr(ctx, "helix_config_cmd_validate_long", nil),
		Args:  cobra.MaximumNArgs(1),
		RunE:  runValidateCommand,
	}

	cmd.Flags().Bool("strict", false, tr(ctx, "helix_config_flag_strict_validate", nil))
	cmd.Flags().Bool("warnings", true, tr(ctx, "helix_config_flag_warnings", nil))
	cmd.Flags().Bool("details", false, tr(ctx, "helix_config_flag_details_validate", nil))
	cmd.Flags().String("section", "", tr(ctx, "helix_config_flag_section_validate", nil))
	cmd.Flags().Bool("schema", false, tr(ctx, "helix_config_flag_schema_validate", nil))

	return cmd
}

func createExportCommand() *cobra.Command {
	ctx := context.Background()
	cmd := &cobra.Command{
		Use:   "export [file]",
		Short: tr(ctx, "helix_config_cmd_export_short", nil),
		Long:  tr(ctx, "helix_config_cmd_export_long", nil),
		Args:  cobra.MaximumNArgs(1),
		RunE:  runExportCommand,
	}

	cmd.Flags().StringP("format", "f", "auto", tr(ctx, "helix_config_flag_format_export", nil))
	cmd.Flags().Bool("secrets", false, tr(ctx, "helix_config_flag_secrets_export", nil))
	cmd.Flags().Bool("defaults", false, tr(ctx, "helix_config_flag_defaults_export", nil))
	cmd.Flags().Bool("comments", false, tr(ctx, "helix_config_flag_comments_export", nil))
	cmd.Flags().Bool("compress", false, tr(ctx, "helix_config_flag_compress_export", nil))
	cmd.Flags().Bool("encrypt", false, tr(ctx, "helix_config_flag_encrypt_export", nil))
	cmd.Flags().String("password", "", tr(ctx, "helix_config_flag_password_export", nil))

	return cmd
}

func createImportCommand() *cobra.Command {
	ctx := context.Background()
	cmd := &cobra.Command{
		Use:   "import <file>",
		Short: tr(ctx, "helix_config_cmd_import_short", nil),
		Long:  tr(ctx, "helix_config_cmd_import_long", nil),
		Args:  cobra.ExactArgs(1),
		RunE:  runImportCommand,
	}

	cmd.Flags().Bool("validate", true, tr(ctx, "helix_config_flag_validate_import", nil))
	cmd.Flags().Bool("backup", true, tr(ctx, "helix_config_flag_backup_import", nil))
	cmd.Flags().Bool("merge", false, tr(ctx, "helix_config_flag_merge_import", nil))
	cmd.Flags().Bool("force", false, tr(ctx, "helix_config_flag_force_import", nil))
	cmd.Flags().String("from", "", tr(ctx, "helix_config_flag_from_import", nil))

	return cmd
}

func createBackupCommand() *cobra.Command {
	ctx := context.Background()
	cmd := &cobra.Command{
		Use:   "backup [path]",
		Short: tr(ctx, "helix_config_cmd_backup_short", nil),
		Long:  tr(ctx, "helix_config_cmd_backup_long", nil),
		Args:  cobra.MaximumNArgs(1),
		RunE:  runBackupCommand,
	}

	cmd.Flags().Bool("incremental", false, tr(ctx, "helix_config_flag_incremental_backup", nil))
	cmd.Flags().Bool("compress", false, tr(ctx, "helix_config_flag_compress_backup", nil))
	cmd.Flags().String("description", "", tr(ctx, "helix_config_flag_description_backup", nil))
	cmd.Flags().String("tags", "", tr(ctx, "helix_config_flag_tags_backup", nil))
	cmd.Flags().Bool("encrypt", false, tr(ctx, "helix_config_flag_encrypt_backup", nil))
	cmd.Flags().String("password", "", tr(ctx, "helix_config_flag_password_backup", nil))

	return cmd
}

func createRestoreCommand() *cobra.Command {
	ctx := context.Background()
	cmd := &cobra.Command{
		Use:   "restore <backup>",
		Short: tr(ctx, "helix_config_cmd_restore_short", nil),
		Long:  tr(ctx, "helix_config_cmd_restore_long", nil),
		Args:  cobra.ExactArgs(1),
		RunE:  runRestoreCommand,
	}

	cmd.Flags().Bool("validate", true, tr(ctx, "helix_config_flag_validate_restore", nil))
	cmd.Flags().Bool("backup", true, tr(ctx, "helix_config_flag_backup_restore", nil))
	cmd.Flags().Bool("confirm", false, tr(ctx, "helix_config_flag_confirm_restore", nil))
	cmd.Flags().String("to", "", tr(ctx, "helix_config_flag_to_restore", nil))
	cmd.Flags().Bool("merge", false, tr(ctx, "helix_config_flag_merge_restore", nil))

	return cmd
}

func createResetCommand() *cobra.Command {
	ctx := context.Background()
	cmd := &cobra.Command{
		Use:   "reset [section]",
		Short: tr(ctx, "helix_config_cmd_reset_short", nil),
		Long:  tr(ctx, "helix_config_cmd_reset_long", nil),
		Args:  cobra.MaximumNArgs(1),
		RunE:  runResetCommand,
	}

	cmd.Flags().Bool("confirm", false, tr(ctx, "helix_config_flag_confirm_reset", nil))
	cmd.Flags().Bool("backup", true, tr(ctx, "helix_config_flag_backup_reset", nil))
	cmd.Flags().String("template", "", tr(ctx, "helix_config_flag_template_reset", nil))
	cmd.Flags().Bool("hard", false, tr(ctx, "helix_config_flag_hard_reset", nil))

	return cmd
}

func createReloadCommand() *cobra.Command {
	ctx := context.Background()
	cmd := &cobra.Command{
		Use:   "reload",
		Short: tr(ctx, "helix_config_cmd_reload_short", nil),
		Long:  tr(ctx, "helix_config_cmd_reload_long", nil),
		Args:  cobra.NoArgs,
		RunE:  runReloadCommand,
	}

	cmd.Flags().Bool("cache", true, tr(ctx, "helix_config_flag_cache_reload", nil))
	cmd.Flags().Bool("watchers", true, tr(ctx, "helix_config_flag_watchers_reload", nil))
	cmd.Flags().Bool("services", false, tr(ctx, "helix_config_flag_services_reload", nil))

	return cmd
}

func createWatchCommand() *cobra.Command {
	ctx := context.Background()
	cmd := &cobra.Command{
		Use:   "watch [key...]",
		Short: tr(ctx, "helix_config_cmd_watch_short", nil),
		Long:  tr(ctx, "helix_config_cmd_watch_long", nil),
		Args:  cobra.MinimumNArgs(0),
		RunE:  runWatchCommand,
	}

	cmd.Flags().Bool("changes", true, tr(ctx, "helix_config_flag_changes_watch", nil))
	cmd.Flags().Bool("timestamps", true, tr(ctx, "helix_config_flag_timestamps_watch", nil))
	cmd.Flags().Bool("user", false, tr(ctx, "helix_config_flag_user_watch", nil))
	cmd.Flags().String("format", "table", tr(ctx, "helix_config_flag_format_watch", nil))
	cmd.Flags().Bool("follow", true, tr(ctx, "helix_config_flag_follow_watch", nil))
	cmd.Flags().Bool("summary", false, tr(ctx, "helix_config_flag_summary_watch", nil))

	return cmd
}

func createMigrateCommand() *cobra.Command {
	ctx := context.Background()
	cmd := &cobra.Command{
		Use:   "migrate <to>",
		Short: tr(ctx, "helix_config_cmd_migrate_short", nil),
		Long:  tr(ctx, "helix_config_cmd_migrate_long", nil),
		Args:  cobra.ExactArgs(1),
		RunE:  runMigrateCommand,
	}

	cmd.Flags().String("from", "", tr(ctx, "helix_config_flag_from_migrate", nil))
	cmd.Flags().Bool("backup", true, tr(ctx, "helix_config_flag_backup_migrate", nil))
	cmd.Flags().Bool("dry-run", false, tr(ctx, "helix_config_flag_dry_run_migrate", nil))
	cmd.Flags().Bool("force", false, tr(ctx, "helix_config_flag_force_migrate", nil))
	cmd.Flags().String("path", "", tr(ctx, "helix_config_flag_path_migrate", nil))
	cmd.Flags().Bool("validate", true, tr(ctx, "helix_config_flag_validate_migrate", nil))

	return cmd
}

func createTemplateCommand() *cobra.Command {
	ctx := context.Background()
	cmd := &cobra.Command{
		Use:   "template",
		Short: tr(ctx, "helix_config_cmd_template_short", nil),
		Long:  tr(ctx, "helix_config_cmd_template_long", nil),
	}

	cmd.AddCommand(createTemplateListCommand())
	cmd.AddCommand(createTemplateApplyCommand())
	cmd.AddCommand(createTemplateShowCommand())
	cmd.AddCommand(createTemplateCreateCommand())
	cmd.AddCommand(createTemplateUpdateCommand())
	cmd.AddCommand(createTemplateDeleteCommand())
	cmd.AddCommand(createTemplateSearchCommand())
	cmd.AddCommand(createTemplateValidateCommand())

	return cmd
}

func createHistoryCommand() *cobra.Command {
	ctx := context.Background()
	cmd := &cobra.Command{
		Use:   "history",
		Short: tr(ctx, "helix_config_cmd_history_short", nil),
		Long:  tr(ctx, "helix_config_cmd_history_long", nil),
	}

	cmd.AddCommand(createHistoryListCommand())
	cmd.AddCommand(createHistoryShowCommand())
	cmd.AddCommand(createHistoryRestoreCommand())
	cmd.AddCommand(createHistoryCompareCommand())
	cmd.AddCommand(createHistorySearchCommand())
	cmd.AddCommand(createHistoryCleanCommand())

	return cmd
}

func createSchemaCommand() *cobra.Command {
	ctx := context.Background()
	cmd := &cobra.Command{
		Use:   "schema",
		Short: tr(ctx, "helix_config_cmd_schema_short", nil),
		Long:  tr(ctx, "helix_config_cmd_schema_long", nil),
	}

	cmd.AddCommand(createSchemaShowCommand())
	cmd.AddCommand(createSchemaValidateCommand())
	cmd.AddCommand(createSchemaGenerateCommand())
	cmd.AddCommand(createSchemaExportCommand())
	cmd.AddCommand(createSchemaImportCommand())

	return cmd
}

func createBenchmarkCommand() *cobra.Command {
	ctx := context.Background()
	cmd := &cobra.Command{
		Use:   "benchmark",
		Short: tr(ctx, "helix_config_cmd_benchmark_short", nil),
		Long:  tr(ctx, "helix_config_cmd_benchmark_long", nil),
		Args:  cobra.NoArgs,
		RunE:  runBenchmarkCommand,
	}

	cmd.Flags().String("operation", "all", tr(ctx, "helix_config_flag_operation_benchmark", nil))
	cmd.Flags().Int("iterations", 1000, tr(ctx, "helix_config_flag_iterations_benchmark", nil))
	cmd.Flags().Bool("parallel", false, tr(ctx, "helix_config_flag_parallel_benchmark", nil))
	cmd.Flags().String("profile", "", tr(ctx, "helix_config_flag_profile_benchmark", nil))
	cmd.Flags().String("output", "", tr(ctx, "helix_config_flag_output_benchmark", nil))
	cmd.Flags().Bool("compare", false, tr(ctx, "helix_config_flag_compare_benchmark", nil))
	cmd.Flags().Bool("warmup", true, tr(ctx, "helix_config_flag_warmup_benchmark", nil))

	return cmd
}

func createRegenerateAliasesCommand() *cobra.Command {
	ctx := context.Background()
	cmd := &cobra.Command{
		Use:   "regenerate-aliases",
		Short: tr(ctx, "helix_config_cmd_regenerate_aliases_short", nil),
		Long:  tr(ctx, "helix_config_cmd_regenerate_aliases_long", nil),
		Args:  cobra.NoArgs,
		RunE:  runRegenerateAliasesCommand,
	}

	cmd.Flags().StringP("output", "o", ".helix/model-aliases.yaml", tr(ctx, "helix_config_flag_output_aliases", nil))
	cmd.Flags().Bool("include-disabled", false, tr(ctx, "helix_config_flag_include_disabled", nil))
	cmd.Flags().Bool("dry-run", false, tr(ctx, "helix_config_flag_dry_run", nil))

	return cmd
}

// Template subcommands

func createTemplateListCommand() *cobra.Command {
	ctx := context.Background()
	cmd := &cobra.Command{
		Use:   "list",
		Short: tr(ctx, "helix_config_cmd_template_list_short", nil),
		Long:  tr(ctx, "helix_config_cmd_template_list_long", nil),
		Args:  cobra.NoArgs,
		RunE:  runTemplateListCommand,
	}

	cmd.Flags().String("category", "", tr(ctx, "helix_config_flag_category_template", nil))
	cmd.Flags().String("tag", "", tr(ctx, "helix_config_flag_tag_template", nil))
	cmd.Flags().String("search", "", tr(ctx, "helix_config_flag_search_template", nil))
	cmd.Flags().String("sort", "name", tr(ctx, "helix_config_flag_sort_template", nil))
	cmd.Flags().Bool("details", false, tr(ctx, "helix_config_flag_details_template", nil))

	return cmd
}

func createTemplateApplyCommand() *cobra.Command {
	ctx := context.Background()
	cmd := &cobra.Command{
		Use:   "apply <template>",
		Short: tr(ctx, "helix_config_cmd_template_apply_short", nil),
		Long:  tr(ctx, "helix_config_cmd_template_apply_long", nil),
		Args:  cobra.ExactArgs(1),
		RunE:  runTemplateApplyCommand,
	}

	cmd.Flags().StringSliceP("var", "v", nil, tr(ctx, "helix_config_flag_var_template", nil))
	cmd.Flags().String("vars-file", "", tr(ctx, "helix_config_flag_vars_file_template", nil))
	cmd.Flags().Bool("backup", true, tr(ctx, "helix_config_flag_backup_template", nil))
	cmd.Flags().Bool("preview", false, tr(ctx, "helix_config_flag_preview_template", nil))
	cmd.Flags().Bool("validate", true, tr(ctx, "helix_config_flag_validate_template", nil))
	cmd.Flags().Bool("force", false, tr(ctx, "helix_config_flag_force_template", nil))

	return cmd
}

// History subcommands

func createHistoryListCommand() *cobra.Command {
	ctx := context.Background()
	cmd := &cobra.Command{
		Use:   "list",
		Short: tr(ctx, "helix_config_cmd_history_list_short", nil),
		Long:  tr(ctx, "helix_config_cmd_history_list_long", nil),
		Args:  cobra.NoArgs,
		RunE:  runHistoryListCommand,
	}

	cmd.Flags().Int("limit", 50, tr(ctx, "helix_config_flag_limit_history", nil))
	cmd.Flags().String("since", "", tr(ctx, "helix_config_flag_since_history", nil))
	cmd.Flags().String("until", "", tr(ctx, "helix_config_flag_until_history", nil))
	cmd.Flags().String("user", "", tr(ctx, "helix_config_flag_user_history", nil))
	cmd.Flags().String("section", "", tr(ctx, "helix_config_flag_section_history", nil))
	cmd.Flags().String("sort", "time", tr(ctx, "helix_config_flag_sort_history", nil))
	cmd.Flags().Bool("details", false, tr(ctx, "helix_config_flag_details_history", nil))

	return cmd
}

// Schema subcommands

func createSchemaShowCommand() *cobra.Command {
	ctx := context.Background()
	cmd := &cobra.Command{
		Use:   "show",
		Short: tr(ctx, "helix_config_cmd_schema_show_short", nil),
		Long:  tr(ctx, "helix_config_cmd_schema_show_long", nil),
		Args:  cobra.NoArgs,
		RunE:  runSchemaShowCommand,
	}

	cmd.Flags().String("section", "", tr(ctx, "helix_config_flag_section_schema", nil))
	cmd.Flags().Bool("examples", true, tr(ctx, "helix_config_flag_examples_schema", nil))
	cmd.Flags().String("format", "json", tr(ctx, "helix_config_flag_format_schema", nil))
	cmd.Flags().Bool("validate", false, tr(ctx, "helix_config_flag_validate_schema", nil))

	return cmd
}

// Utility commands

func createCompletionCommand() *cobra.Command {
	ctx := context.Background()
	cmd := &cobra.Command{
		Use:   "completion [shell]",
		Short: tr(ctx, "helix_config_cmd_completion_short", nil),
		Long:  tr(ctx, "helix_config_cmd_completion_long", nil),
		Args:  cobra.MaximumNArgs(1),
		RunE:  runCompletionCommand,
	}

	cmd.Flags().Bool("install", false, tr(ctx, "helix_config_flag_install_completion", nil))
	cmd.Flags().String("shell", "", tr(ctx, "helix_config_flag_shell_completion", nil))

	return cmd
}

func createVersionCommand() *cobra.Command {
	ctx := context.Background()
	cmd := &cobra.Command{
		Use:   "version",
		Short: tr(ctx, "helix_config_cmd_version_short", nil),
		Long:  tr(ctx, "helix_config_cmd_version_long", nil),
		Args:  cobra.NoArgs,
		RunE:  runVersionCommand,
	}

	cmd.Flags().Bool("short", false, tr(ctx, "helix_config_flag_short_version", nil))
	cmd.Flags().Bool("build", false, tr(ctx, "helix_config_flag_build_version", nil))
	cmd.Flags().Bool("deps", false, tr(ctx, "helix_config_flag_deps_version", nil))

	return cmd
}

func createInfoCommand() *cobra.Command {
	ctx := context.Background()
	cmd := &cobra.Command{
		Use:   "info",
		Short: tr(ctx, "helix_config_cmd_info_short", nil),
		Long:  tr(ctx, "helix_config_cmd_info_long", nil),
		Args:  cobra.NoArgs,
		RunE:  runInfoCommand,
	}

	cmd.Flags().Bool("system", false, tr(ctx, "helix_config_flag_system_info", nil))
	cmd.Flags().Bool("files", false, tr(ctx, "helix_config_flag_files_info", nil))
	cmd.Flags().Bool("stats", false, tr(ctx, "helix_config_flag_stats_info", nil))
	cmd.Flags().Bool("environment", false, tr(ctx, "helix_config_flag_environment_info", nil))

	return cmd
}

func createStatusCommand() *cobra.Command {
	ctx := context.Background()
	cmd := &cobra.Command{
		Use:   "status",
		Short: tr(ctx, "helix_config_cmd_status_short", nil),
		Long:  tr(ctx, "helix_config_cmd_status_long", nil),
		Args:  cobra.NoArgs,
		RunE:  runStatusCommand,
	}

	cmd.Flags().Bool("watchers", false, tr(ctx, "helix_config_flag_watchers_status", nil))
	cmd.Flags().Bool("cache", false, tr(ctx, "helix_config_flag_cache_status", nil))
	cmd.Flags().Bool("locks", false, tr(ctx, "helix_config_flag_locks_status", nil))
	cmd.Flags().Bool("performance", false, tr(ctx, "helix_config_flag_performance_status", nil))

	return cmd
}

func createDiffCommand() *cobra.Command {
	ctx := context.Background()
	cmd := &cobra.Command{
		Use:   "diff <file1> <file2>",
		Short: tr(ctx, "helix_config_cmd_diff_short", nil),
		Long:  tr(ctx, "helix_config_cmd_diff_long", nil),
		Args:  cobra.ExactArgs(2),
		RunE:  runDiffCommand,
	}

	cmd.Flags().String("format", "table", tr(ctx, "helix_config_flag_format_diff", nil))
	cmd.Flags().Bool("unified", false, tr(ctx, "helix_config_flag_unified_diff", nil))
	cmd.Flags().Int("context", 3, tr(ctx, "helix_config_flag_context_diff", nil))
	cmd.Flags().Bool("color", true, tr(ctx, "helix_config_flag_color_diff", nil))
	cmd.Flags().Bool("semantic", false, tr(ctx, "helix_config_flag_semantic_diff", nil))

	return cmd
}

func createMergeCommand() *cobra.Command {
	ctx := context.Background()
	cmd := &cobra.Command{
		Use:   "merge <file1> <file2> [output]",
		Short: tr(ctx, "helix_config_cmd_merge_short", nil),
		Long:  tr(ctx, "helix_config_cmd_merge_long", nil),
		Args:  cobra.RangeArgs(2, 3),
		RunE:  runMergeCommand,
	}

	cmd.Flags().String("strategy", "override", tr(ctx, "helix_config_flag_strategy_merge", nil))
	cmd.Flags().String("conflict", "first", tr(ctx, "helix_config_flag_conflict_merge", nil))
	cmd.Flags().Bool("validate", true, tr(ctx, "helix_config_flag_validate_merge_cmd", nil))
	cmd.Flags().String("base", "", tr(ctx, "helix_config_flag_base_merge", nil))
	cmd.Flags().Bool("preview", false, tr(ctx, "helix_config_flag_preview_merge", nil))

	return cmd
}

func createSearchCommand() *cobra.Command {
	ctx := context.Background()
	cmd := &cobra.Command{
		Use:   "search <pattern>",
		Short: tr(ctx, "helix_config_cmd_search_short", nil),
		Long:  tr(ctx, "helix_config_cmd_search_long", nil),
		Args:  cobra.ExactArgs(1),
		RunE:  runSearchCommand,
	}

	cmd.Flags().String("section", "", tr(ctx, "helix_config_flag_section_search", nil))
	cmd.Flags().Bool("regex", false, tr(ctx, "helix_config_flag_regex_search", nil))
	cmd.Flags().Bool("case-sensitive", false, tr(ctx, "helix_config_flag_case_sensitive_search", nil))
	cmd.Flags().Bool("values", true, tr(ctx, "helix_config_flag_values_search", nil))
	cmd.Flags().Bool("keys", true, tr(ctx, "helix_config_flag_keys_search", nil))
	cmd.Flags().Int("limit", 100, tr(ctx, "helix_config_flag_limit_search", nil))
	cmd.Flags().String("sort", "relevance", tr(ctx, "helix_config_flag_sort_search", nil))

	return cmd
}

// Command implementations

func runShowCommand(cmd *cobra.Command, args []string) error {
	cfg, err := getConfig()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	format, _ := cmd.Flags().GetString("format")
	switch format {
	case "json":
		return printJSON(cfg)
	case "yaml":
		data, err := yaml.Marshal(cfg)
		if err != nil {
			return err
		}
		fmt.Println(string(data))
	default:
		ctx := cmd.Context()
		if ctx == nil {
			ctx = context.Background()
		}
		fmt.Println(tr(ctx, "helix_config_show_loaded_from", map[string]any{"Path": viper.ConfigFileUsed()}))
		fmt.Println(tr(ctx, "helix_config_show_server_port", map[string]any{"Port": cfg.Server.Port}))
		fmt.Println(tr(ctx, "helix_config_show_database_host", map[string]any{"Host": cfg.Database.Host}))
		fmt.Println(tr(ctx, "helix_config_show_redis_enabled", map[string]any{"Enabled": cfg.Redis.Enabled}))
	}
	return nil
}

func runGetCommand(cmd *cobra.Command, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("key argument required")
	}
	key := args[0]

	value := viper.Get(key)
	if value == nil {
		return fmt.Errorf("key not found: %s", key)
	}

	format, _ := cmd.Flags().GetString("format")
	if format == "json" {
		return printJSON(map[string]interface{}{key: value})
	}
	fmt.Printf("%s = %v\n", key, value)
	return nil
}

func runSetCommand(cmd *cobra.Command, args []string) error {
	if len(args) < 2 {
		return fmt.Errorf("key and value arguments required")
	}
	key := args[0]
	value := args[1]

	viper.Set(key, value)
	if err := viper.WriteConfig(); err != nil {
		// If config doesn't exist, create it
		if err := viper.SafeWriteConfig(); err != nil {
			return fmt.Errorf("failed to write config: %w", err)
		}
	}
	ctx := cmd.Context()
	if ctx == nil {
		ctx = context.Background()
	}
	fmt.Println(tr(ctx, "helix_config_set_done", map[string]any{"Key": key, "Value": value}))
	return nil
}

func runDeleteCommand(cmd *cobra.Command, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("key argument required")
	}
	key := args[0]

	// Set to nil effectively removes the key
	viper.Set(key, nil)
	if err := viper.WriteConfig(); err != nil {
		return fmt.Errorf("failed to write config: %w", err)
	}
	ctx := cmd.Context()
	if ctx == nil {
		ctx = context.Background()
	}
	fmt.Println(tr(ctx, "helix_config_delete_done", map[string]any{"Key": key}))
	return nil
}

func runValidateCommand(cmd *cobra.Command, args []string) error {
	cfg, err := getConfig()
	if err != nil {
		ctx := cmd.Context()
		if ctx == nil {
			ctx = context.Background()
		}
		fmt.Println(tr(ctx, "helix_config_validate_failed_line", map[string]any{"Error": err.Error()}))
		return err
	}

	// Basic validation checks
	errors := []string{}
	if cfg.Server.Port <= 0 || cfg.Server.Port > 65535 {
		errors = append(errors, "server.port must be between 1 and 65535")
	}
	if cfg.Auth.JWTSecret != "" && len(cfg.Auth.JWTSecret) < 32 {
		errors = append(errors, "auth.jwt_secret should be at least 32 characters")
	}

	ctx := cmd.Context()
	if ctx == nil {
		ctx = context.Background()
	}
	if len(errors) > 0 {
		fmt.Println(tr(ctx, "helix_config_validate_failed_header", nil))
		for _, e := range errors {
			fmt.Printf("  - %s\n", e)
		}
		return fmt.Errorf("validation failed with %d errors", len(errors))
	}

	fmt.Println(tr(ctx, "helix_config_validate_ok", nil))
	return nil
}

func runExportCommand(cmd *cobra.Command, args []string) error {
	cfg, err := getConfig()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	output, _ := cmd.Flags().GetString("output")
	format, _ := cmd.Flags().GetString("format")

	var data []byte
	switch format {
	case "json":
		data, err = json.MarshalIndent(cfg, "", "  ")
	default:
		data, err = yaml.Marshal(cfg)
	}
	if err != nil {
		return err
	}

	if output == "" || output == "-" {
		fmt.Println(string(data))
	} else {
		if err := os.WriteFile(output, data, 0644); err != nil {
			return fmt.Errorf("failed to write file: %w", err)
		}
		ctx := cmd.Context()
		if ctx == nil {
			ctx = context.Background()
		}
		fmt.Println(tr(ctx, "helix_config_export_written", map[string]any{"Path": output}))
	}
	return nil
}

func runImportCommand(cmd *cobra.Command, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("file argument required")
	}
	file := args[0]

	data, err := os.ReadFile(file)
	if err != nil {
		return fmt.Errorf("failed to read file: %w", err)
	}

	// Parse and merge config
	var imported map[string]interface{}
	if strings.HasSuffix(file, ".json") {
		if err := json.Unmarshal(data, &imported); err != nil {
			return fmt.Errorf("failed to parse JSON: %w", err)
		}
	} else {
		if err := yaml.Unmarshal(data, &imported); err != nil {
			return fmt.Errorf("failed to parse YAML: %w", err)
		}
	}

	merge, _ := cmd.Flags().GetBool("merge")
	if merge {
		for k, v := range imported {
			viper.Set(k, v)
		}
	} else {
		for k, v := range imported {
			viper.Set(k, v)
		}
	}

	if err := viper.WriteConfig(); err != nil {
		return fmt.Errorf("failed to write config: %w", err)
	}
	ctx := cmd.Context()
	if ctx == nil {
		ctx = context.Background()
	}
	fmt.Println(tr(ctx, "helix_config_import_done", map[string]any{"Path": file}))
	return nil
}

func runBackupCommand(cmd *cobra.Command, args []string) error {
	configPath := viper.ConfigFileUsed()
	if configPath == "" {
		return fmt.Errorf("no config file in use")
	}

	backupPath := configPath + ".backup." + time.Now().Format("20060102-150405")
	data, err := os.ReadFile(configPath)
	if err != nil {
		return fmt.Errorf("failed to read config: %w", err)
	}

	if err := os.WriteFile(backupPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write backup: %w", err)
	}
	ctx := cmd.Context()
	if ctx == nil {
		ctx = context.Background()
	}
	fmt.Println(tr(ctx, "helix_config_backup_written", map[string]any{"Path": backupPath}))
	return nil
}

func runRestoreCommand(cmd *cobra.Command, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("backup file argument required")
	}
	backupPath := args[0]

	data, err := os.ReadFile(backupPath)
	if err != nil {
		return fmt.Errorf("failed to read backup: %w", err)
	}

	configPath := viper.ConfigFileUsed()
	if configPath == "" {
		configPath = "config.yaml"
	}

	if err := os.WriteFile(configPath, data, 0644); err != nil {
		return fmt.Errorf("failed to restore config: %w", err)
	}
	ctx := cmd.Context()
	if ctx == nil {
		ctx = context.Background()
	}
	fmt.Println(tr(ctx, "helix_config_restore_done", map[string]any{"Path": backupPath}))
	return nil
}

func runResetCommand(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	if ctx == nil {
		ctx = context.Background()
	}
	force, _ := cmd.Flags().GetBool("force")
	if !force {
		fmt.Println(tr(ctx, "helix_config_reset_confirm_required", nil))
		return nil
	}

	// Create default config
	defaultCfg := &config.HelixConfig{}
	if err := config.SaveHelixConfig(defaultCfg); err != nil {
		return fmt.Errorf("failed to save default config: %w", err)
	}
	fmt.Println(tr(ctx, "helix_config_reset_done", nil))
	return nil
}

func runReloadCommand(cmd *cobra.Command, args []string) error {
	if err := viper.ReadInConfig(); err != nil {
		return fmt.Errorf("failed to reload config: %w", err)
	}
	ctx := cmd.Context()
	if ctx == nil {
		ctx = context.Background()
	}
	fmt.Println(tr(ctx, "helix_config_reload_done", nil))
	return nil
}

func runWatchCommand(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	if ctx == nil {
		ctx = context.Background()
	}
	fmt.Println(tr(ctx, "helix_config_watch_start", nil))
	viper.WatchConfig()
	viper.OnConfigChange(func(e fsnotify.Event) {
		fmt.Println(tr(ctx, "helix_config_watch_config_changed", map[string]any{"Name": e.Name}))
	})
	// Block until interrupted
	select {}
}

func runMigrateCommand(cmd *cobra.Command, args []string) error {
	from, _ := cmd.Flags().GetString("from")
	to, _ := cmd.Flags().GetString("to")

	if from == "" {
		from = viper.ConfigFileUsed()
	}

	data, err := os.ReadFile(from)
	if err != nil {
		return fmt.Errorf("failed to read source: %w", err)
	}

	srcVer, dstVer, err := configVersions(to, data)
	if err != nil {
		return fmt.Errorf("inspect config versions: %w", err)
	}
	if srcVer != dstVer {
		return fmt.Errorf(
			"helix-config migrate: source version %q ≠ target version %q — "+
				"version-aware schema migration is not yet implemented. "+
				"Previous behavior silently copied source bytes verbatim, "+
				"which masked schema upgrades and was a §11.4 PASS-bluff. "+
				"To proceed: either edit %s to set version=%q before retry, "+
				"or implement a per-version migrator under internal/config/migrations/",
			srcVer, dstVer, from, dstVer,
		)
	}
	if err := os.WriteFile(to, data, 0644); err != nil {
		return fmt.Errorf("failed to write target: %w", err)
	}
	ctx := cmd.Context()
	if ctx == nil {
		ctx = context.Background()
	}
	fmt.Println(tr(ctx, "helix_config_migrate_copied", map[string]any{
		"Version": fmt.Sprintf("%q", srcVer),
		"From":    from,
		"To":      to,
	}))
	return nil
}

// configVersions extracts the `version:` field from source bytes and
// the target file (if present). Missing version defaults to "" /
// matches source (no migration needed).
func configVersions(dstPath string, srcData []byte) (string, string, error) {
	srcVer := extractConfigVersion(srcData)
	dstVer := srcVer
	if dstData, err := os.ReadFile(dstPath); err == nil {
		dstVer = extractConfigVersion(dstData)
	} else if !os.IsNotExist(err) {
		return "", "", fmt.Errorf("read target %s: %w", dstPath, err)
	}
	return srcVer, dstVer, nil
}

// extractConfigVersion scans for `version:` (YAML) or `"version":` (JSON).
// Returns "" when absent.
func extractConfigVersion(data []byte) string {
	for _, line := range strings.Split(string(data), "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "version:") {
			return strings.TrimSpace(strings.TrimPrefix(trimmed, "version:"))
		}
		if strings.HasPrefix(trimmed, `"version":`) {
			rest := strings.TrimSpace(strings.TrimPrefix(trimmed, `"version":`))
			rest = strings.TrimSuffix(rest, ",")
			return strings.Trim(rest, `"`)
		}
	}
	return ""
}

func runBenchmarkCommand(cmd *cobra.Command, args []string) error {
	iterations, _ := cmd.Flags().GetInt("iterations")
	if iterations <= 0 {
		iterations = 1000
	}

	start := time.Now()
	for i := 0; i < iterations; i++ {
		viper.Get("server.port")
	}
	readDuration := time.Since(start)

	start = time.Now()
	for i := 0; i < iterations; i++ {
		viper.Set("benchmark.test", i)
	}
	writeDuration := time.Since(start)

	ctx := cmd.Context()
	if ctx == nil {
		ctx = context.Background()
	}
	fmt.Println(tr(ctx, "helix_config_benchmark_header", map[string]any{"Iterations": iterations}))
	fmt.Println(tr(ctx, "helix_config_benchmark_read", map[string]any{
		"Duration": readDuration.String(),
		"Rate":     fmt.Sprintf("%.2f", float64(iterations)/readDuration.Seconds()),
	}))
	fmt.Println(tr(ctx, "helix_config_benchmark_write", map[string]any{
		"Duration": writeDuration.String(),
		"Rate":     fmt.Sprintf("%.2f", float64(iterations)/writeDuration.Seconds()),
	}))
	return nil
}

// Template command implementations

func runTemplateListCommand(cmd *cobra.Command, args []string) error {
	templates := []string{
		"minimal    - Minimal configuration for testing",
		"production - Production-ready configuration",
		"development - Development configuration with debug enabled",
		"enterprise - Enterprise configuration with all features",
	}
	ctx := cmd.Context()
	if ctx == nil {
		ctx = context.Background()
	}
	fmt.Println(tr(ctx, "helix_config_template_list_header", nil))
	for _, t := range templates {
		fmt.Printf("  %s\n", t)
	}
	return nil
}

func runTemplateApplyCommand(cmd *cobra.Command, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("template name argument required")
	}
	templateName := args[0]

	// Apply template based on name
	switch templateName {
	case "minimal":
		viper.Set("server.port", 8080)
		viper.Set("database.enabled", false)
		viper.Set("redis.enabled", false)
	case "production":
		viper.Set("server.port", 8080)
		viper.Set("database.enabled", true)
		viper.Set("redis.enabled", true)
		viper.Set("logging.level", "info")
	case "development":
		viper.Set("server.port", 8080)
		viper.Set("database.enabled", false)
		viper.Set("logging.level", "debug")
	case "enterprise":
		viper.Set("server.port", 8080)
		viper.Set("database.enabled", true)
		viper.Set("redis.enabled", true)
		viper.Set("logging.level", "info")
		viper.Set("monitoring.enabled", true)
	default:
		return fmt.Errorf("unknown template: %s", templateName)
	}

	if err := viper.WriteConfig(); err != nil {
		if err := viper.SafeWriteConfig(); err != nil {
			return fmt.Errorf("failed to save config: %w", err)
		}
	}
	ctx := cmd.Context()
	if ctx == nil {
		ctx = context.Background()
	}
	fmt.Println(tr(ctx, "helix_config_template_applied", map[string]any{"Name": templateName}))
	return nil
}

// History command implementations

func runHistoryListCommand(cmd *cobra.Command, args []string) error {
	configPath := viper.ConfigFileUsed()
	if configPath == "" {
		return fmt.Errorf("no config file in use")
	}

	// Look for backup files
	dir := filepath.Dir(configPath)
	base := filepath.Base(configPath)
	pattern := base + ".backup.*"

	matches, err := filepath.Glob(filepath.Join(dir, pattern))
	if err != nil {
		return fmt.Errorf("failed to list backups: %w", err)
	}

	ctx := cmd.Context()
	if ctx == nil {
		ctx = context.Background()
	}
	if len(matches) == 0 {
		fmt.Println(tr(ctx, "helix_config_history_none", nil))
		return nil
	}

	fmt.Println(tr(ctx, "helix_config_history_header", nil))
	for _, m := range matches {
		info, _ := os.Stat(m)
		if info != nil {
			fmt.Printf("  %s  %s\n", info.ModTime().Format("2006-01-02 15:04:05"), filepath.Base(m))
		}
	}
	return nil
}

// Schema command implementations

func runSchemaShowCommand(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	if ctx == nil {
		ctx = context.Background()
	}
	schema := map[string]interface{}{
		"server": map[string]string{
			"port":         tr(ctx, "helix_config_schema_server_port", nil),
			"host":         tr(ctx, "helix_config_schema_server_host", nil),
			"read_timeout": tr(ctx, "helix_config_schema_server_read_timeout", nil),
		},
		"database": map[string]string{
			"enabled":  tr(ctx, "helix_config_schema_database_enabled", nil),
			"host":     tr(ctx, "helix_config_schema_database_host", nil),
			"port":     tr(ctx, "helix_config_schema_database_port", nil),
			"user":     tr(ctx, "helix_config_schema_database_user", nil),
			"password": tr(ctx, "helix_config_schema_database_password", nil),
			"dbname":   tr(ctx, "helix_config_schema_database_dbname", nil),
		},
		"redis": map[string]string{
			"enabled":  tr(ctx, "helix_config_schema_redis_enabled", nil),
			"host":     tr(ctx, "helix_config_schema_redis_host", nil),
			"port":     tr(ctx, "helix_config_schema_redis_port", nil),
			"password": tr(ctx, "helix_config_schema_redis_password", nil),
		},
		"auth": map[string]string{
			"jwt_secret":   tr(ctx, "helix_config_schema_auth_jwt_secret", nil),
			"token_expiry": tr(ctx, "helix_config_schema_auth_token_expiry", nil),
		},
	}
	return printJSON(schema)
}

// Utility command implementations

func runCompletionCommand(cmd *cobra.Command, args []string) error {
	shell := "bash"
	if len(args) > 0 {
		shell = args[0]
	}

	switch shell {
	case "bash":
		return cmd.Root().GenBashCompletion(os.Stdout)
	case "zsh":
		return cmd.Root().GenZshCompletion(os.Stdout)
	case "fish":
		return cmd.Root().GenFishCompletion(os.Stdout, true)
	case "powershell":
		return cmd.Root().GenPowerShellCompletion(os.Stdout)
	default:
		return fmt.Errorf("unsupported shell: %s", shell)
	}
}

func runVersionCommand(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	if ctx == nil {
		ctx = context.Background()
	}
	fmt.Println(tr(ctx, "helix_config_version_line", map[string]any{"Version": version}))
	fmt.Println(tr(ctx, "helix_config_version_build_time", map[string]any{"BuildTime": buildTime}))
	fmt.Println(tr(ctx, "helix_config_version_git_commit", map[string]any{"GitCommit": gitCommit}))
	return nil
}

func runInfoCommand(cmd *cobra.Command, args []string) error {
	info := map[string]interface{}{
		"config_file":  viper.ConfigFileUsed(),
		"config_type":  viper.GetString("config_type"),
		"keys_count":   len(viper.AllKeys()),
		"env_prefix":   "HELIX",
		"search_paths": []string{".", "$HOME/.helixcode", "/etc/helixcode"},
	}
	return printJSON(info)
}

func runStatusCommand(cmd *cobra.Command, args []string) error {
	cfg, err := getConfig()
	status := "healthy"
	issues := []string{}

	if err != nil {
		status = "error"
		issues = append(issues, err.Error())
	} else {
		// Check database configuration - Host being empty means database is not configured
		// but if other DB settings are present without Host, that's a misconfiguration
		if cfg.Database.Host == "" && (cfg.Database.User != "" || cfg.Database.DBName != "") {
			issues = append(issues, "database user/name set but host not configured")
		}
		// Check Redis configuration - has explicit Enabled flag
		if cfg.Redis.Enabled && cfg.Redis.Host == "" {
			issues = append(issues, "redis enabled but host not configured")
		}
		if len(issues) > 0 {
			status = "warning"
		}
	}

	result := map[string]interface{}{
		"status":      status,
		"issues":      issues,
		"config_file": viper.ConfigFileUsed(),
	}
	return printJSON(result)
}

func runDiffCommand(cmd *cobra.Command, args []string) error {
	if len(args) < 2 {
		return fmt.Errorf("two config files required for diff")
	}

	data1, err := os.ReadFile(args[0])
	if err != nil {
		return fmt.Errorf("failed to read first file: %w", err)
	}
	data2, err := os.ReadFile(args[1])
	if err != nil {
		return fmt.Errorf("failed to read second file: %w", err)
	}

	var cfg1, cfg2 map[string]interface{}
	yaml.Unmarshal(data1, &cfg1)
	yaml.Unmarshal(data2, &cfg2)

	// Simple diff - just compare keys
	fmt.Printf("--- %s\n", args[0])
	fmt.Printf("+++ %s\n", args[1])

	allKeys := make(map[string]bool)
	for k := range cfg1 {
		allKeys[k] = true
	}
	for k := range cfg2 {
		allKeys[k] = true
	}

	for k := range allKeys {
		v1, ok1 := cfg1[k]
		v2, ok2 := cfg2[k]
		if !ok1 {
			fmt.Printf("+ %s: %v\n", k, v2)
		} else if !ok2 {
			fmt.Printf("- %s: %v\n", k, v1)
		} else if fmt.Sprintf("%v", v1) != fmt.Sprintf("%v", v2) {
			fmt.Printf("- %s: %v\n", k, v1)
			fmt.Printf("+ %s: %v\n", k, v2)
		}
	}
	return nil
}

func runMergeCommand(cmd *cobra.Command, args []string) error {
	if len(args) < 2 {
		return fmt.Errorf("at least two config files required for merge")
	}

	merged := make(map[string]interface{})

	for _, file := range args {
		data, err := os.ReadFile(file)
		if err != nil {
			return fmt.Errorf("failed to read %s: %w", file, err)
		}

		var cfg map[string]interface{}
		if err := yaml.Unmarshal(data, &cfg); err != nil {
			return fmt.Errorf("failed to parse %s: %w", file, err)
		}

		// Merge configs (later files override earlier)
		for k, v := range cfg {
			merged[k] = v
		}
	}

	output, _ := cmd.Flags().GetString("output")
	data, _ := yaml.Marshal(merged)

	if output == "" || output == "-" {
		fmt.Println(string(data))
	} else {
		if err := os.WriteFile(output, data, 0644); err != nil {
			return fmt.Errorf("failed to write merged config: %w", err)
		}
		ctx := cmd.Context()
		if ctx == nil {
			ctx = context.Background()
		}
		fmt.Println(tr(ctx, "helix_config_merge_written", map[string]any{"Path": output}))
	}
	return nil
}

func runSearchCommand(cmd *cobra.Command, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("search query required")
	}
	query := strings.ToLower(args[0])

	keys, _ := cmd.Flags().GetBool("keys")
	values, _ := cmd.Flags().GetBool("values")
	limit, _ := cmd.Flags().GetInt("limit")

	results := []map[string]interface{}{}
	count := 0

	for _, key := range viper.AllKeys() {
		if count >= limit {
			break
		}

		value := viper.Get(key)
		valueStr := fmt.Sprintf("%v", value)

		match := false
		if keys && strings.Contains(strings.ToLower(key), query) {
			match = true
		}
		if values && strings.Contains(strings.ToLower(valueStr), query) {
			match = true
		}

		if match {
			results = append(results, map[string]interface{}{
				"key":   key,
				"value": value,
			})
			count++
		}
	}

	if len(results) == 0 {
		ctx := cmd.Context()
		if ctx == nil {
			ctx = context.Background()
		}
		fmt.Println(tr(ctx, "helix_config_search_no_results", nil))
		return nil
	}

	return printJSON(results)
}

// Utility functions

func setupViper() {
	viper.SetConfigName("helix")
	viper.SetConfigType("yaml")
	viper.AddConfigPath(".")
	viper.AddConfigPath("$HOME/.helixcode")
	viper.AddConfigPath("/etc/helixcode")
	viper.AddConfigPath("/usr/local/etc/helixcode")
}

func loadConfig() {
	if configFile != "" {
		viper.SetConfigFile(configFile)
	}

	if err := viper.ReadInConfig(); err != nil {
		if !strings.Contains(err.Error(), "Not Found") {
			fmt.Println(tr(context.Background(), "helix_config_load_error", map[string]any{"Error": err.Error()}))
		}
	}
}

func bindFlags(flags *pflag.FlagSet) {
	flags.VisitAll(func(f *pflag.Flag) {
		viper.BindPFlag(f.Name, f)
	})
}

func getConfig() (*config.HelixConfig, error) {
	return config.LoadHelixConfig()
}

func saveConfig(cfg *config.HelixConfig) error {
	if configFile == "" {
		configFile = "$HOME/.helixcode/helix.yaml"
	}

	return config.SaveHelixConfig(cfg)
}

func printJSON(data interface{}) error {
	var output []byte
	var err error

	if prettyPrint {
		output, err = json.MarshalIndent(data, "", "  ")
	} else {
		output, err = json.Marshal(data)
	}

	if err != nil {
		return err
	}

	fmt.Println(string(output))
	return nil
}

func printYAML(data interface{}) error {
	output, err := yaml.Marshal(data)
	if err != nil {
		return err
	}

	fmt.Println(string(output))
	return nil
}

func printTable(headers []string, rows [][]string) {
	if len(rows) == 0 {
		return
	}

	// Calculate column widths
	widths := make([]int, len(headers))
	for i, header := range headers {
		widths[i] = len(header)
	}

	for _, row := range rows {
		for i, cell := range row {
			if len(cell) > widths[i] {
				widths[i] = len(cell)
			}
		}
	}

	// Print header
	for i, header := range headers {
		fmt.Printf("%-*s", widths[i]+2, header)
	}
	fmt.Println()

	// Print separator
	for _, width := range widths {
		fmt.Printf("%-*s", width+2, strings.Repeat("-", width))
	}
	fmt.Println()

	// Print rows
	for _, row := range rows {
		for i, cell := range row {
			fmt.Printf("%-*s", widths[i]+2, cell)
		}
		fmt.Println()
	}
}

func parseTime(timeStr string) (time.Time, error) {
	// Try parsing as duration first
	if duration, err := time.ParseDuration(timeStr); err == nil {
		return time.Now().Add(-duration), nil
	}

	// Try parsing as timestamp
	formats := []string{
		time.RFC3339,
		"2006-01-02 15:04:05",
		"2006-01-02",
		"01/02/2006 15:04:05",
		"01/02/2006",
	}

	for _, format := range formats {
		if parsed, err := time.Parse(format, timeStr); err == nil {
			return parsed, nil
		}
	}

	return time.Time{}, fmt.Errorf("unable to parse time: %s", timeStr)
}

func formatValue(value interface{}) string {
	switch v := value.(type) {
	case string:
		return fmt.Sprintf("\"%s\"", v)
	case nil:
		return "null"
	case bool:
		return strconv.FormatBool(v)
	default:
		return fmt.Sprintf("%v", v)
	}
}

func maskSecret(value string) string {
	if len(value) <= 4 {
		return strings.Repeat("*", len(value))
	}
	return value[:2] + strings.Repeat("*", len(value)-4) + value[len(value)-2:]
}

func confirm(prompt string) bool {
	if force || interactive {
		return true
	}

	fmt.Printf("%s [y/N]: ", prompt)
	var response string
	fmt.Scanln(&response)

	return strings.ToLower(response) == "y" || strings.ToLower(response) == "yes"
}

func errorf(format string, args ...interface{}) {
	if !quiet {
		prefix := tr(context.Background(), "helix_config_msg_prefix_error", nil)
		fmt.Fprintf(os.Stderr, prefix+" "+format+"\n", args...)
	}
}

func warnf(format string, args ...interface{}) {
	if !quiet {
		prefix := tr(context.Background(), "helix_config_msg_prefix_warning", nil)
		fmt.Fprintf(os.Stderr, prefix+" "+format+"\n", args...)
	}
}

func infof(format string, args ...interface{}) {
	if !quiet {
		fmt.Printf(format+"\n", args...)
	}
}

func debugf(format string, args ...interface{}) {
	if verbose && !quiet {
		prefix := tr(context.Background(), "helix_config_msg_prefix_debug", nil)
		fmt.Printf(prefix+" "+format+"\n", args...)
	}
}

func successf(format string, args ...interface{}) {
	if !quiet {
		fmt.Printf("✅ "+format+"\n", args...)
	}
}

// Missing template command implementations
func createTemplateShowCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "show <template>",
		Short: tr(context.Background(), "helix_config_cmd_template_show_short", nil),
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			ctx := cmd.Context()
			if ctx == nil {
				ctx = context.Background()
			}
			fmt.Println(tr(ctx, "helix_config_template_show_action", map[string]any{"Name": args[0]}))
		},
	}
}

func createTemplateCreateCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "create <name>",
		Short: tr(context.Background(), "helix_config_cmd_template_create_short", nil),
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			ctx := cmd.Context()
			if ctx == nil {
				ctx = context.Background()
			}
			fmt.Println(tr(ctx, "helix_config_template_create_action", map[string]any{"Name": args[0]}))
		},
	}
}

func createTemplateUpdateCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "update <template>",
		Short: tr(context.Background(), "helix_config_cmd_template_update_short", nil),
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			ctx := cmd.Context()
			if ctx == nil {
				ctx = context.Background()
			}
			fmt.Println(tr(ctx, "helix_config_template_update_action", map[string]any{"Name": args[0]}))
		},
	}
}

func createTemplateDeleteCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "delete <template>",
		Short: tr(context.Background(), "helix_config_cmd_template_delete_short", nil),
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			ctx := cmd.Context()
			if ctx == nil {
				ctx = context.Background()
			}
			fmt.Println(tr(ctx, "helix_config_template_delete_action", map[string]any{"Name": args[0]}))
		},
	}
}

func createTemplateSearchCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "search <query>",
		Short: tr(context.Background(), "helix_config_cmd_template_search_short", nil),
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			ctx := cmd.Context()
			if ctx == nil {
				ctx = context.Background()
			}
			fmt.Println(tr(ctx, "helix_config_template_search_action", map[string]any{"Query": args[0]}))
		},
	}
}

func createTemplateValidateCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "validate <template>",
		Short: tr(context.Background(), "helix_config_cmd_template_validate_short", nil),
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			ctx := cmd.Context()
			if ctx == nil {
				ctx = context.Background()
			}
			fmt.Println(tr(ctx, "helix_config_template_validate_action", map[string]any{"Name": args[0]}))
		},
	}
}

// Missing history command implementations
func createHistoryShowCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "show <id>",
		Short: tr(context.Background(), "helix_config_cmd_history_show_short", nil),
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			ctx := cmd.Context()
			if ctx == nil {
				ctx = context.Background()
			}
			fmt.Println(tr(ctx, "helix_config_history_show_action", map[string]any{"ID": args[0]}))
		},
	}
}

func createHistoryRestoreCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "restore <id>",
		Short: tr(context.Background(), "helix_config_cmd_history_restore_short", nil),
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			ctx := cmd.Context()
			if ctx == nil {
				ctx = context.Background()
			}
			fmt.Println(tr(ctx, "helix_config_history_restore_action", map[string]any{"ID": args[0]}))
		},
	}
}

func createHistoryCompareCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "compare <id1> <id2>",
		Short: tr(context.Background(), "helix_config_cmd_history_compare_short", nil),
		Args:  cobra.ExactArgs(2),
		Run: func(cmd *cobra.Command, args []string) {
			ctx := cmd.Context()
			if ctx == nil {
				ctx = context.Background()
			}
			fmt.Println(tr(ctx, "helix_config_history_compare_action", map[string]any{"First": args[0], "Second": args[1]}))
		},
	}
}

func createHistorySearchCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "search <query>",
		Short: tr(context.Background(), "helix_config_cmd_history_search_short", nil),
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			ctx := cmd.Context()
			if ctx == nil {
				ctx = context.Background()
			}
			fmt.Println(tr(ctx, "helix_config_history_search_action", map[string]any{"Query": args[0]}))
		},
	}
}

func createHistoryCleanCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "clean",
		Short: tr(context.Background(), "helix_config_cmd_history_clean_short", nil),
		Run: func(cmd *cobra.Command, args []string) {
			ctx := cmd.Context()
			if ctx == nil {
				ctx = context.Background()
			}
			fmt.Println(tr(ctx, "helix_config_history_clean_start", nil))
		},
	}
}

// Missing schema command implementations
func createSchemaValidateCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "validate <file>",
		Short: tr(context.Background(), "helix_config_cmd_schema_validate_short", nil),
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			ctx := cmd.Context()
			if ctx == nil {
				ctx = context.Background()
			}
			fmt.Println(tr(ctx, "helix_config_schema_validate_action", map[string]any{"File": args[0]}))
		},
	}
}

func createSchemaGenerateCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "generate",
		Short: tr(context.Background(), "helix_config_cmd_schema_generate_short", nil),
		Run: func(cmd *cobra.Command, args []string) {
			ctx := cmd.Context()
			if ctx == nil {
				ctx = context.Background()
			}
			fmt.Println(tr(ctx, "helix_config_schema_generate_action", nil))
		},
	}
}

func createSchemaExportCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "export <file>",
		Short: tr(context.Background(), "helix_config_cmd_schema_export_short", nil),
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			ctx := cmd.Context()
			if ctx == nil {
				ctx = context.Background()
			}
			fmt.Println(tr(ctx, "helix_config_schema_export_action", map[string]any{"File": args[0]}))
		},
	}
}

func createSchemaImportCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "import <file>",
		Short: tr(context.Background(), "helix_config_cmd_schema_import_short", nil),
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			ctx := cmd.Context()
			if ctx == nil {
				ctx = context.Background()
			}
			fmt.Println(tr(ctx, "helix_config_schema_import_action", map[string]any{"File": args[0]}))
		},
	}
}

// Regenerate aliases command implementation

func runRegenerateAliasesCommand(cmd *cobra.Command, args []string) error {
	outputPath, _ := cmd.Flags().GetString("output")
	includeDisabled, _ := cmd.Flags().GetBool("include-disabled")
	dryRun, _ := cmd.Flags().GetBool("dry-run")

	ctx := cmd.Context()
	if ctx == nil {
		ctx = context.Background()
	}

	fmt.Println(tr(ctx, "helix_config_regenerate_aliases_start", map[string]any{"Output": outputPath}))

	// Load config to get LLM settings
	if _, err := config.LoadHelixConfig(); err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	// Load providers from viper (config.yaml providers section)
	var providerConfigs []llm.ProviderConfigEntry
	if err := viper.UnmarshalKey("llm.providers", &providerConfigs); err != nil {
		fmt.Println(tr(ctx, "helix_config_regenerate_aliases_warn_providers", map[string]any{"Error": err.Error()}))
		providerConfigs = []llm.ProviderConfigEntry{}
	}

	// Create ModelManager and register providers
	modelManager := llm.NewModelManager()

	// Enable cloud gate for local development (allow cloud providers)
	llm.SetCloudEnabled(true)

	for _, pc := range providerConfigs {
		if !includeDisabled && !pc.Enabled {
			continue
		}

		provider, err := llm.NewProvider(pc)
		if err != nil {
			fmt.Println(tr(ctx, "helix_config_regenerate_aliases_warn_provider", map[string]any{
				"Type":  pc.Type,
				"Error": err.Error(),
			}))
			continue
		}

		if err := modelManager.RegisterProvider(provider); err != nil {
			fmt.Println(tr(ctx, "helix_config_regenerate_aliases_warn_register", map[string]any{
				"Type":  pc.Type,
				"Error": err.Error(),
			}))
			continue
		}
	}

	// Create ProviderBridge to access live registry
	bridge := provider.NewProviderBridge(modelManager)

	// Get providers from live registry
	providers, err := bridge.ListProvidersDirect(ctx)
	if err != nil {
		fmt.Println(tr(ctx, "helix_config_regenerate_aliases_warn_list", map[string]any{"Error": err.Error()}))
		providers = []provider.ProviderEntry{}
	}

	// Generate aliases from live providers
	aliases := generateAliasesFromProviders(providers, includeDisabled)

	// Add static aliases from example config
	staticAliases := loadStaticAliases()
	aliases = mergeAliases(staticAliases, aliases)

	// Create alias config
	aliasConfig := &llm.AliasConfig{
		Version:        "1.0",
		FuzzyThreshold: 0.7,
		Aliases:        aliases,
	}

	if dryRun {
		fmt.Println(tr(ctx, "helix_config_regenerate_aliases_dry_run", nil))
		data, _ := yaml.Marshal(aliasConfig)
		fmt.Println(string(data))
		return nil
	}

	// Ensure output directory exists
	dir := filepath.Dir(outputPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create output directory: %w", err)
	}

	// Save alias config
	if err := llm.SaveAliasConfig(aliasConfig, outputPath); err != nil {
		return fmt.Errorf("failed to save aliases: %w", err)
	}

	fmt.Println(tr(ctx, "helix_config_regenerate_aliases_done", map[string]any{"Count": len(aliases), "Path": outputPath}))
	return nil
}

func generateAliasesFromProviders(providers []provider.ProviderEntry, includeDisabled bool) []*llm.ModelAlias {
	var aliases []*llm.ModelAlias

	for _, p := range providers {
		if !includeDisabled && !p.Enabled {
			continue
		}

		for _, model := range p.Models {
			// Create alias from model name
			aliasName := strings.ToLower(strings.ReplaceAll(model.Name, " ", "-"))
			aliasName = strings.ReplaceAll(aliasName, ".", "-")
			aliasName = strings.ReplaceAll(aliasName, "_", "-")

			// Skip if alias already exists (prefer first provider)
			exists := false
			for _, a := range aliases {
				if a.Alias == aliasName {
					exists = true
					break
				}
			}
			if exists {
				continue
			}

			aliases = append(aliases, &llm.ModelAlias{
				Alias:       aliasName,
				TargetModel: model.ID,
				Provider:    string(p.Type),
				Description: fmt.Sprintf("%s (%s)", model.Name, p.Name),
				Tags:        []string{string(p.Type), strings.ToLower(model.Name)},
			})
		}
	}

	return aliases
}

func loadStaticAliases() []*llm.ModelAlias {
	// Load from example config as fallback
	configPaths := llm.GetConfigPaths()
	var allAliases []*llm.ModelAlias

	for _, path := range configPaths {
		if _, err := os.Stat(path); err == nil {
			config, err := llm.LoadAliasConfig(path)
			if err == nil && config != nil {
				allAliases = append(allAliases, config.Aliases...)
			}
		}
	}

	// If no configs found, use defaults
	if len(allAliases) == 0 {
		defaultConfig := llm.DefaultAliasConfig()
		allAliases = defaultConfig.Aliases
	}

	return allAliases
}

func mergeAliases(static, live []*llm.ModelAlias) []*llm.ModelAlias {
	aliasMap := make(map[string]*llm.ModelAlias)

	// Add static aliases first (lower priority)
	for _, a := range static {
		key := strings.ToLower(strings.TrimSpace(a.Alias))
		aliasMap[key] = a
	}

	// Add live aliases (higher priority - they override)
	for _, a := range live {
		key := strings.ToLower(strings.TrimSpace(a.Alias))
		aliasMap[key] = a
	}

	// Convert back to slice
	var merged []*llm.ModelAlias
	for _, a := range aliasMap {
		merged = append(merged, a)
	}

	return merged
}
