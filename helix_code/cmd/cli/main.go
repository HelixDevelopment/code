package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"dev.helix.code/cmd/cli/i18n"
	"dev.helix.code/internal/adapters/speckit_debate_adapter"
	"dev.helix.code/internal/agent"
	"dev.helix.code/internal/agent/subagent"
	"dev.helix.code/internal/approval"
	"dev.helix.code/internal/approvalwire"
	"dev.helix.code/internal/autocommit"
	"dev.helix.code/internal/checkpoint"
	"dev.helix.code/internal/commands"
	"dev.helix.code/internal/commands/builtin"
	"dev.helix.code/internal/config"
	"dev.helix.code/internal/continua"
	"dev.helix.code/internal/hooks"
	"dev.helix.code/internal/i18nwiring"
	"dev.helix.code/internal/kilocode"
	"dev.helix.code/internal/llm"
	"dev.helix.code/internal/mcp"
	"dev.helix.code/internal/notification"
	taskplanner "dev.helix.code/internal/planner"
	"dev.helix.code/internal/plantree"
	"dev.helix.code/internal/pprofutil"
	"dev.helix.code/internal/projectmemory"
	"dev.helix.code/internal/rag"
	"dev.helix.code/internal/render"
	"dev.helix.code/internal/roocode"
	"dev.helix.code/internal/secrets"
	"dev.helix.code/internal/server"
	"dev.helix.code/internal/session"
	"dev.helix.code/internal/telemetry"
	"dev.helix.code/internal/theme"
	"dev.helix.code/internal/tools"
	"dev.helix.code/internal/tools/askuser"
	"dev.helix.code/internal/tools/browser"
	"dev.helix.code/internal/tools/confirmation"
	"dev.helix.code/internal/tools/permissions"
	"dev.helix.code/internal/tools/permissions/sessionrules"
	"dev.helix.code/internal/tools/persistence"
	"dev.helix.code/internal/tools/sandbox"
	"dev.helix.code/internal/tools/smartedit"
	"dev.helix.code/internal/tools/task"
	"dev.helix.code/internal/tools/worktree"
	"dev.helix.code/internal/verifier"
	"dev.helix.code/internal/version"
	"dev.helix.code/internal/voice"
	"dev.helix.code/internal/worker"
	"dev.helix.code/internal/workflow"
	"dev.helix.code/internal/workflow/planmode"
	"dev.helix.code/internal/workspace"
	speckitconfig "digital.vasic.helixspecifier/pkg/config"
	"digital.vasic.helixspecifier/pkg/speckit"
	speckittypes "digital.vasic.helixspecifier/pkg/types"
	"github.com/sirupsen/logrus"
	"go.uber.org/zap"
)

// translator resolves CONST-046 message IDs for every user-facing
// string emitted by this CLI. Defaults to i18n.NoopTranslator{} (loud
// message-ID echo) so unit tests + ad-hoc invocations remain obvious.
// helix_code wires a real *i18nadapter.Translator at boot via
// SetTranslator (round-131 §11.4 anti-bluff sweep, 2026-05-18).
//
// A package-level variable is the chosen DI seam because the legacy
// CLI handler signatures (func(*CLI, context.Context) error) do not
// support extra parameters without restructuring the handler tree —
// global injection matches the cli's existing use of package-level
// state (the flag.CommandLine, the *CLI receiver tree) and keeps the
// migration minimally invasive.
var translator i18n.Translator = i18n.NoopTranslator{}

// SetTranslator wires a CONST-046-compliant Translator. Passing nil
// resets to i18n.NoopTranslator{} (loud echo) — never silently
// disables translation lookup (which would be a §11.4 PASS-bluff at
// the i18n injection layer).
func SetTranslator(t i18n.Translator) {
	if t == nil {
		translator = i18n.NoopTranslator{}
		return
	}
	translator = t
}

// tr is the internal CONST-046 resolver used by every migrated
// user-facing string emission in this file. It NEVER returns an error
// to the caller — translation failures degrade to the message ID
// itself (matching NoopTranslator behaviour) so production output
// remains loud + obvious instead of silently empty.
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

// trc is the CONST-046 resolver for strings that are needed at cobra
// command-construction time (Short / Long descriptions, flag-help text)
// — points where no request-scoped context.Context is available. It
// resolves against context.Background() through the same package-level
// translator the runtime tr() helper uses, so cobra metadata is just as
// locale-aware as runtime output. The package-level translator is wired
// via SetTranslator before main() builds the cobra tree, so trc() sees a
// real Translator in production and i18n.NoopTranslator{} (loud message-
// ID echo) in unit tests that build commands without wiring one.
func trc(msgID string, data map[string]any) string {
	return tr(context.Background(), msgID, data)
}

// defaultConfigPathFromEnv resolves the on-disk wizard config path using the
// supplied env-lookup. Mirrors internal/llm.defaultWizardConfigPath but lives
// here so cmd/cli does not need to import an unexported helper. Honours
// XDG_CONFIG_HOME, falls back to $HOME/.config/helixcode/llm.yaml.
func defaultConfigPathFromEnv(env func(string) string) string {
	if xdg := strings.TrimSpace(env("XDG_CONFIG_HOME")); xdg != "" {
		return filepath.Join(xdg, "helixcode", "llm.yaml")
	}
	home := strings.TrimSpace(env("HOME"))
	if home == "" {
		return filepath.Join(".", ".config", "helixcode", "llm.yaml")
	}
	return filepath.Join(home, ".config", "helixcode", "llm.yaml")
}

// loadProviderConfigFromDisk reads the wizard-written YAML at
// $XDG_CONFIG_HOME/helixcode/llm.yaml (or $HOME/.config fallback) and returns
// the persisted ProviderType + ProviderConfigEntry. Missing-file is reported
// as an os.ErrNotExist-wrapped error so callers can fall through to other
// selection sources (env / wizard prompt). Unmarshal failures are surfaced as
// non-sentinel errors.
//
// Anti-bluff anchor: this is a real-disk reader. There is no stubbed config
// path or fake "no config means everything is fine" mode — callers handle
// the missing file with errors.Is(err, os.ErrNotExist).
func loadProviderConfigFromDisk(envLookup func(string) string) (string, llm.ProviderConfigEntry, error) {
	path := defaultConfigPathFromEnv(envLookup)
	res, err := llm.LoadWizardConfig(path)
	if err != nil {
		return "", llm.ProviderConfigEntry{}, err
	}
	if res == nil {
		return "", llm.ProviderConfigEntry{}, fmt.Errorf("loadProviderConfigFromDisk: nil result from %s", path)
	}
	return string(res.ProviderType), res.ConfigEntry, nil
}

// buildSubagentLLMProvider constructs the LLM provider used by a subagent
// helper child (P1-F15-T08). Wired into main() via subagent.RunAsSubagent.
//
// The child does NOT replay the parent's full F12 bootstrap (no flag parsing,
// no friendly Stderr hints): it reads the same config-file + HELIX_LLM_PROVIDER
// env that the parent uses, resolves the type via llm.Select, and constructs
// the cloud provider. On any failure (no config, env-only with missing creds,
// construction error) it falls back to the local Ollama default — matching the
// parent's "keep working for non-LLM paths" stance.
//
// Pragmatic v1: we read env+config-file only; the child does not see the
// parent's --provider flag because the spawner re-exec's the helper without
// argv. This is documented in the spec § 4.2; if a future task plumbs
// --provider through the env protocol, this function should consult it.
//
// Anti-bluff: this function MUST construct a real provider — never a stub or
// the FakeLLMProvider (which lives in the subagent package as a test-only
// type with the "fake-test-only" sentinel ProviderType).
func buildSubagentLLMProvider(ctx context.Context) (llm.Provider, error) {
	// Wire the process-wide cloud gate here too: this function runs inside a
	// re-exec'd subagent HELPER process (see main()'s
	// subagent.IsSubagentInvocation() dispatch above) — a genuinely separate
	// OS process from the parent CLI, so the parent's SetCloudEnabled call
	// never reaches it (cloudGate is process-global, not inherited across
	// exec boundaries). Without this, cloudGate would stay at its
	// fail-closed zero value (CLOSED) for every subagent child forever,
	// silently refusing hosted providers regardless of the operator's
	// llm.cloud.enabled setting. A config load error leaves the gate at
	// its fail-closed zero value (CLOSED) here too.
	if subCfg, err := config.Get(); err == nil {
		llm.SetCloudEnabled(subCfg.LLM.Cloud.Enabled)
	}

	configProviderName, configEntry, configErr := loadProviderConfigFromDisk(os.Getenv)
	if configErr != nil && !errors.Is(configErr, os.ErrNotExist) {
		// Config read failed for a real reason; surface it but keep going so
		// we still try env / default.
		log.Printf("subagent: config load failed (continuing): %v", configErr)
	}
	selectorInput := llm.SelectorInput{
		Flag:   "",
		Env:    os.Getenv("HELIX_LLM_PROVIDER"),
		Config: configProviderName,
	}
	ptype, selErr := llm.Select(selectorInput)
	switch {
	case errors.Is(selErr, llm.ErrNoProviderConfigured):
		// Fall through to default Ollama.
	case selErr != nil:
		// Unknown provider name — surface and fall back to default rather
		// than aborting the subagent run.
		log.Printf("subagent: provider selector error: %v (falling back to default)", selErr)
	default:
		entry := configEntry
		entry.Type = ptype
		cloud, cErr := llm.NewCloudProvider(ptype, entry)
		if cErr == nil && cloud != nil {
			return cloud, nil
		}
		log.Printf("subagent: failed to construct cloud provider %q (%v); falling back to local default", ptype, cErr)
	}

	// Default: local Ollama on the standard port. Mirrors NewCLI()'s default.
	provider, err := llm.NewOllamaProvider(llm.OllamaConfig{
		DefaultModel: "llama3.2",
		BaseURL:      "http://localhost:11434",
	})
	if err != nil {
		return nil, fmt.Errorf("subagent: default Ollama provider construction failed: %w", err)
	}
	return provider, nil
}

// CLI represents the command-line interface
type CLI struct {
	workerPool         *worker.SSHWorkerPool
	llmProvider        llm.Provider
	notificationEngine *notification.NotificationEngine
	verifierAdapter    *verifier.Adapter
	permissionMode     string
	permissionsEngine  *permissions.Engine
	// sessionRuleStore is the single, shared, session-scoped permission-rule
	// store. It is the SAME instance consulted by the live permissions.Engine
	// at decision time AND mutated by the `/permissions add|remove` slash
	// command, so a rule added in-session takes effect on the live gate
	// immediately (no restart). See initPermissions + the slash-command wiring.
	sessionRuleStore   *sessionrules.Store
	persistenceManager *persistence.Manager
	worktreeManager    *worktree.Manager
	sessionMgr         *session.Manager
	toolRegistry       *tools.ToolRegistry
	commandRegistry    *commands.Registry
	skillDispatcher    *agent.SkillDispatcher // F10: routes plain-language input to a matching skill
	mcpManager         *mcp.Manager
	browserManager     *browser.BrowserManager       // F23: cline-style single-session browser façade
	memoryRegistry     *projectmemory.MemoryRegistry // F24: codex-style project memory + hot-reload
	hooksLoaded        int                           // count of hooks loaded at startup (for diagnostics)

	// --- Speed programme P1-T03: lazy CLI startup (R1 B01/B13/B14/B18) ---
	//
	// The eager monolith in an earlier revision of Run() built ~25 subsystems
	// (telemetry, permissions, worktree git shell-out, hooks YAML, tool
	// registry, LSP exec.LookPath sweep, MCP server spawn, sandbox detection,
	// the F22-F30 tool/slash-command suite, the F15 subagent manager, …)
	// sequentially BEFORE the first user action — so even `--help`,
	// `--list-models` and `--command` paid the full bootstrap cost.
	//
	// The bootstrap is now split into sync.Once-guarded getters. Each getter
	// constructs its subsystem exactly once, lazily, on first access; commands
	// that do not need a subsystem never trigger its construction. Construction
	// ORDER is preserved inside each getter (getter-calls-getter where there is
	// a genuine dependency — e.g. the heavy-subsystem getter calls telemetry()
	// first because the tool-registry instrumentation needs the provider).
	//
	// cleanups holds the deferred teardown closures that the eager monolith
	// registered with `defer` inside Run(). They are drained by runCleanups()
	// via a single `defer` in Run() so a lazily-constructed subsystem is still
	// torn down at process exit, regardless of WHICH command triggered it.
	telemetryOnce  sync.Once
	telemetryProv  telemetry.TelemetryProvider
	telemetryCfg   telemetry.TelemetryConfig
	llmOnce        sync.Once
	subsystemsOnce sync.Once
	subsystemsErr  error
	cleanups       []func()
	cleanupsMu     sync.Mutex

	// constructionCount records how many times each lazy getter actually ran
	// its sync.Once body. It exists so integration tests can prove a command
	// that needs subsystem X triggers exactly one construction of X, and a
	// command that does NOT need X triggers zero. Key: subsystem name.
	constructionCount map[string]int
	constructionMu    sync.Mutex

	// flagState carries the parsed command-line flag values that the lazy
	// subsystem getters consume. Run() populates it immediately after
	// flag.Parse(); ensureSubsystems / ensureLLMProvider read it. This keeps
	// the getters free of a *flag dependency so they can be invoked from any
	// command handler (e.g. handleInteractive) without a flag receiver.
	flagState cliFlagState
}

// cliFlagState is the parsed-flag snapshot the lazy getters need. Populated
// once by Run() after flag.Parse().
type cliFlagState struct {
	approvalFlag      string
	providerFlag      string
	resumeFlag        bool
	continueFlag      bool
	resumeSessionFlag string
}

// recordConstruction increments the lazy-construction probe counter for name.
// It is the anti-bluff seam P1-T03's integration tests assert against: a
// getter that constructed must show count==1; a subsystem never touched must
// show count==0.
func (c *CLI) recordConstruction(name string) {
	c.constructionMu.Lock()
	defer c.constructionMu.Unlock()
	if c.constructionCount == nil {
		c.constructionCount = make(map[string]int)
	}
	c.constructionCount[name]++
}

// ConstructionCount returns how many times the named lazy getter ran its
// sync.Once body. Used by P1-T03 tests (same package) to prove laziness.
func (c *CLI) ConstructionCount(name string) int {
	c.constructionMu.Lock()
	defer c.constructionMu.Unlock()
	return c.constructionCount[name]
}

// addCleanup registers a teardown closure to run at Run() exit. Safe for
// concurrent use — the F09/F10 watcher goroutines never call it, but the
// background subagent / workspace wiring may, so the mutex is cheap insurance.
func (c *CLI) addCleanup(fn func()) {
	if fn == nil {
		return
	}
	c.cleanupsMu.Lock()
	defer c.cleanupsMu.Unlock()
	c.cleanups = append(c.cleanups, fn)
}

// runCleanups drains the registered teardown closures in LIFO order — the
// same order Go's `defer` stack would have unwound them in the old eager
// monolith. Called exactly once via a single `defer` in Run().
func (c *CLI) runCleanups() {
	c.cleanupsMu.Lock()
	fns := c.cleanups
	c.cleanups = nil
	c.cleanupsMu.Unlock()
	for i := len(fns) - 1; i >= 0; i-- {
		fns[i]()
	}
}

// NewCLI creates a new CLI instance
func NewCLI() *CLI {
	// Initialize LLM provider from config - use Ollama on port 11434.
	// Speed programme P1-T02: NewOllamaProvider no longer performs a
	// blocking /api/tags discovery round-trip here — model discovery is
	// deferred to first real use (GetModels/GetHealth), so every CLI
	// start (including `--help`) is freed of one synchronous Ollama call.
	llmProvider, _ := llm.NewOllamaProvider(llm.OllamaConfig{
		DefaultModel: "llama3.2",               // Use Ollama's model name
		BaseURL:      "http://localhost:11434", // Ollama default port
	})

	// Initialize LLMsVerifier subsystem if config is available.
	// Speed programme P2-T07: config.Get() loads + caches the config once
	// per process (sync.Once), so repeated NewCLI() / subagent construction
	// reuses the same *Config instead of re-reading YAML and re-churning
	// viper — and is race-free (closes the P0-T02 concurrent-map-write bug).
	var verifierAdapter *verifier.Adapter
	cfg, err := config.Get()
	if err == nil && cfg.Verifier != nil && cfg.Verifier.Enabled {
		vResult, vErr := verifier.Bootstrap(cfg.Verifier)
		if vErr == nil && vResult != nil {
			verifierAdapter = vResult.Adapter
		}
	}

	return &CLI{
		workerPool:         worker.NewSSHWorkerPool(true),
		llmProvider:        llmProvider,
		notificationEngine: notification.NewNotificationEngine(),
		verifierAdapter:    verifierAdapter,
	}
}

// initPermissions bootstraps the permissions.Engine with rules loaded from
// ~/.helixcode/permissions.yaml (user scope) and <cwd>/.helixcode/permissions.yaml
// (project scope), then registers a default confirmation.Policy with pe.
//
// CONCERN (T10 / Phase 3): pe is a locally constructed PolicyEngine that is not
// yet wired into the tool execution path. The --permission-mode flag is parsed
// and validated here, and the Engine is ready, but actual tool calls are not yet
// gated by it. T10 will introduce the `permissions` subcommand; Phase 3 will
// thread pe through the session/tool dispatcher so denies block execution.
func (c *CLI) initPermissions(ctx context.Context, pe *confirmation.PolicyEngine) error {
	if c.permissionMode != "" && !permissions.IsValidMode(c.permissionMode) {
		return fmt.Errorf("invalid --permission-mode %q (valid: %v)", c.permissionMode, permissions.ValidModes)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("resolving user home dir: %w", err)
	}
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("resolving cwd: %w", err)
	}
	loader := &permissions.FileLoader{
		UserPath:    filepath.Join(home, ".helixcode", "permissions.yaml"),
		ProjectPath: filepath.Join(cwd, ".helixcode", "permissions.yaml"),
		Mode:        c.permissionMode,
	}
	// Construct the single shared session-rule store and wire it into the live
	// engine as the session-rule overlay (consulted FIRST at decision time). The
	// SAME store instance is handed to the `/permissions add|remove` slash
	// command (see the command-registry wiring), so a rule added in-session
	// gates live tool execution immediately. sessionrules.Store.Decide is
	// fail-closed on a corrupt session rule set.
	if c.sessionRuleStore == nil {
		c.sessionRuleStore = sessionrules.New()
	}
	eng, err := permissions.NewEngine(ctx, loader, pe,
		permissions.WithSessionDecider(c.sessionRuleStore.Decide))
	if err != nil {
		return fmt.Errorf("initialising permissions engine: %w", err)
	}
	c.permissionsEngine = eng
	return nil
}

// initPersistence bootstraps the persistence.Manager rooted at the current
// working directory. Large tool outputs (>50 KB) will be written under
// <cwd>/.helix/tool-results/ so the context window is not flooded.
// A background goroutine prunes files older than DefaultMaxAge (7 days).
func (c *CLI) initPersistence() error {
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("resolving cwd for persistence: %w", err)
	}
	c.persistenceManager = persistence.NewManager(cwd)
	go func() {
		if err := c.persistenceManager.CleanupOld(persistence.DefaultMaxAge); err != nil {
			log.Printf("WARN persistence cleanup: %v", err)
		}
	}()
	return nil
}

// initWorktree bootstraps the worktree.Manager with repoRoot resolved via
// `git rev-parse --show-toplevel`, falling back to os.Getwd() if the cwd
// is not a git repo.
func (c *CLI) initWorktree(ctx context.Context) error {
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("resolving cwd for worktree: %w", err)
	}
	repoRoot := cwd
	if root, err := worktreeRevParseToplevel(ctx, cwd); err == nil {
		repoRoot = root
	}
	c.worktreeManager = worktree.NewManager(repoRoot)
	return nil
}

// initHooks loads ~/.helixcode/hooks.yaml + <cwd>/.helixcode/hooks.yaml,
// wraps each enabled entry in a shell-runner HookFunc, and registers it
// with the session.Manager.hooksManager. Errors fail-fast.
func (c *CLI) initHooks(ctx context.Context, sessionMgr *session.Manager) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("resolving home dir for hooks: %w", err)
	}
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("resolving cwd for hooks: %w", err)
	}
	loader := &hooks.FileLoader{
		UserPath:    filepath.Join(home, ".helixcode", "hooks.yaml"),
		ProjectPath: filepath.Join(cwd, ".helixcode", "hooks.yaml"),
	}
	hs, sources, err := loader.Load(ctx)
	if err != nil {
		return fmt.Errorf("loading hooks: %w", err)
	}
	hm := sessionMgr.GetHooksManager()
	for _, h := range hs {
		scriptPath := h.Metadata["script"]
		h.Handler = hooks.NewShellRunner(scriptPath, h.Timeout)
		if err := hm.Register(h); err != nil {
			return fmt.Errorf("registering hook %q: %w", h.ID, err)
		}
	}
	c.hooksLoaded = len(hs)
	if len(sources) > 0 {
		log.Printf("hooks: loaded %d hook(s) from %v", len(hs), sources)
	}
	return nil
}

// sessionStoreBaseDir resolves the on-disk root for F11 session transcripts.
// Resolution order (XDG Base Directory Specification):
//  1. $XDG_DATA_HOME/helixcode/sessions/   (when $XDG_DATA_HOME is set and absolute)
//  2. $HOME/.local/share/helixcode/sessions/  (XDG default)
//  3. ./.helixcode-sessions/  (last-resort fallback when $HOME is also unset)
//
// The directory is not created here; TranscriptStore creates per-session
// subdirectories on first append.
func sessionStoreBaseDir() string {
	if xdg := os.Getenv("XDG_DATA_HOME"); xdg != "" && filepath.IsAbs(xdg) {
		return filepath.Join(xdg, "helixcode", "sessions")
	}
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		return filepath.Join(home, ".local", "share", "helixcode", "sessions")
	}
	return filepath.Join(".", ".helixcode-sessions")
}

// worktreeRevParseToplevel is a tiny shim to avoid leaking the worktree
// package's internal helpers; it shells out to git directly.
func worktreeRevParseToplevel(ctx context.Context, cwd string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", "rev-parse", "--show-toplevel")
	cmd.Dir = cwd
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// telemetry lazily constructs the OTel telemetry provider exactly once.
//
// Speed programme P1-T03: in the old eager monolith this ran unconditionally
// on every CLI start. It is cheap when no OTEL_* env vars are set (the SDK
// builds a noop provider) but it is still pure overhead for `--help` /
// `--command` / `--list-models` which never emit a span. It is now lazy:
// constructed on first access by ensureLLMProvider (the traced-LLM decorator
// needs it) and ensureSubsystems (the tool-registry instrumentation needs it).
//
// Construction never returns an error to the caller — failure degrades to a
// noop provider, matching the old monolith's behaviour. The telemetry-shutdown
// teardown is registered via addCleanup so it still runs at Run() exit.
func (c *CLI) telemetry() telemetry.TelemetryProvider {
	c.telemetryOnce.Do(func() {
		c.recordConstruction("telemetry")
		cfg, cfgErr := telemetry.LoadConfigFromEnv(os.Getenv)
		if cfgErr != nil {
			log.Printf("telemetry: config invalid (continuing with noop): %v", cfgErr)
			cfg = telemetry.TelemetryConfig{Enabled: false, Exporter: telemetry.ExporterNoop}
		}
		c.telemetryCfg = cfg
		prov, provErr := telemetry.NewTelemetryProvider(cfg, zap.NewNop())
		if provErr != nil {
			log.Printf("telemetry: provider construction failed (continuing with noop): %v", provErr)
		}
		if prov == nil {
			// Defence in depth — NewTelemetryProvider should never return nil,
			// but a nil here would nil-panic the decorators below.
			prov, _ = telemetry.NewTelemetryProvider(
				telemetry.TelemetryConfig{Enabled: false, Exporter: telemetry.ExporterNoop},
				zap.NewNop(),
			)
		}
		c.telemetryProv = prov
		c.addCleanup(func() {
			shutdownTimeout := c.telemetryCfg.ShutdownTimeout
			if shutdownTimeout <= 0 {
				shutdownTimeout = telemetry.DefaultShutdownTimeout
			}
			shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
			defer cancel()
			if err := c.telemetryProv.Shutdown(shutdownCtx); err != nil {
				log.Printf("telemetry: shutdown error: %v", err)
			}
		})
		log.Printf("telemetry: initialised (exporter=%s)", string(prov.Exporter()))
	})
	return c.telemetryProv
}

// ensureLLMProvider lazily resolves the cloud LLM provider override (F12) and
// wraps the active provider with the telemetry decorator (P1-F16-T10), exactly
// once.
//
// Speed programme P1-T03: this is needed only by commands that actually talk
// to an LLM — `--prompt` (handleGenerate), `--list-models` (handleListModels)
// and the interactive REPL. `--command`, `--health`, `--list-workers`,
// `--notify`, `--qa-*` and `--help` never call it, so they skip the F12
// config-file read + any cloud-provider construction.
//
// providerFlag is the parsed value of the --provider flag. Errors are
// non-fatal except an explicitly unknown --provider value (a fixable user
// error), which is returned so Run() can surface a non-zero exit.
func (c *CLI) ensureLLMProvider(ctx context.Context, providerFlag string) error {
	var fatal error
	c.llmOnce.Do(func() {
		c.recordConstruction("llmProvider")
		// F12: resolve cloud LLM provider via flag > env > config-file.
		configProviderName, configEntry, configErr := loadProviderConfigFromDisk(os.Getenv)
		if configErr != nil && !errors.Is(configErr, os.ErrNotExist) {
			log.Printf("F12 provider: config load failed (continuing without): %v", configErr)
		}
		selectorInput := llm.SelectorInput{
			Flag:   providerFlag,
			Env:    os.Getenv("HELIX_LLM_PROVIDER"),
			Config: configProviderName,
		}
		ptype, selErr := llm.Select(selectorInput)
		switch {
		case errors.Is(selErr, llm.ErrNoProviderConfigured):
			// Friendly hint, then keep the default provider.
			fmt.Fprintln(os.Stderr, tr(ctx, "cli_f12_no_cloud_provider", nil))
		case selErr != nil:
			// User typed an unknown value -> fail loudly with a non-zero exit.
			fatal = fmt.Errorf("F12 provider: %w", selErr)
			return
		default:
			entry := configEntry
			entry.Type = ptype
			cloud, cErr := llm.NewCloudProvider(ptype, entry)
			if cErr != nil {
				fmt.Fprintln(os.Stderr, tr(ctx, "cli_f12_construct_failed", map[string]any{
					"Provider": fmt.Sprintf("%q", ptype),
					"Error":    fmt.Sprintf("%v", cErr),
				}))
			} else if cloud != nil {
				c.llmProvider = cloud
				fmt.Fprintf(os.Stderr, "F12 provider: using %q\n", ptype)
			}
		}

		// P1-F16-T10: wrap the resolved provider with the telemetry decorator.
		// Order anchor preserved from the old monolith — this runs AFTER F12
		// settles c.llmProvider and BEFORE any caller of c.llmProvider observes
		// it. When telemetry is in noop mode the decorator is pass-through.
		if c.llmProvider != nil {
			tracedLLM, traceErr := telemetry.NewTracedLLMProvider(c.llmProvider, c.telemetry())
			if traceErr != nil {
				log.Printf("telemetry: LLM decorator construction failed (using undecorated provider): %v", traceErr)
			} else {
				c.llmProvider = tracedLLM
			}
		}
	})
	return fatal
}

// ensureSubsystems lazily constructs the heavy interactive-session subsystem
// cluster exactly once: permissions, persistence, worktree, hooks, the tool
// registry + the full F07-F30 tool/slash-command suite, LSP, MCP, sandbox,
// approval, the F15 subagent manager and the F09/F10 markdown-command + skill
// loaders/watchers.
//
// Speed programme P1-T03 (R1 B01/B13/B14/B18): this is the hundreds-of-
// milliseconds cost the old eager monolith paid on EVERY CLI start. It is now
// gated behind a sync.Once and is invoked ONLY by the interactive REPL path
// (handleInteractive) — the only command that genuinely needs the slash-
// command registry + agent tool surface. Short commands (`--list-models`,
// `--command`, `--health`, `--qa-*`, `--help`, …) skip the entire cluster:
// no worktree git shell-out, no hooks YAML read, no LSP exec.LookPath sweep,
// no MCP server spawn, no sandbox detection.
//
// Construction ORDER inside this getter is identical to the old monolith's
// statement order — every genuine dependency (sessionMgr before hooks,
// toolReg before the F-feature tool registrations, cmdRegistry before the
// slash-command registrations, telemetry before tool instrumentation) is
// preserved. Every `defer` the monolith used is converted to addCleanup so
// teardown still happens at Run() exit.
func (c *CLI) ensureSubsystems(ctx context.Context) error {
	c.subsystemsOnce.Do(func() {
		c.recordConstruction("subsystems")
		// The F22 auto-committer and the F15 subagent manager both capture
		// c.llmProvider by value at construction time, so the F12 cloud
		// override + telemetry wrapper MUST settle before this getter runs —
		// getter-calls-getter preserves the old monolith's ordering anchor.
		if err := c.ensureLLMProvider(ctx, c.flagState.providerFlag); err != nil {
			c.subsystemsErr = err
			return
		}
		c.subsystemsErr = c.buildSubsystems(ctx)
	})
	return c.subsystemsErr
}

// buildSubsystems constructs the heavy interactive-session subsystem cluster.
// It is the body of ensureSubsystems' sync.Once — never call it directly; it
// is unguarded and would double-register tools/slash-commands. See
// ensureSubsystems for the laziness + ordering contract.
func (c *CLI) buildSubsystems(ctx context.Context) error {
	// HXC-036: wire the CONST-046 boot-time translators BEFORE any subsystem
	// that emits user-facing prompts is constructed (the ask_user prompter and
	// the approval gate below both render localized text). Without this, those
	// packages run on their NoopTranslator{} default and users see raw
	// message-ID keys (e.g. "askuser_prompt_invalid_choice_hint") instead of
	// resolved + interpolated text — a CONST-046 / §11.4 regression. A failed
	// translator build is logged but non-fatal: the loud message-ID echo is a
	// degraded-but-honest fallback, never a silent swallow.
	if err := i18nwiring.WireAll(); err != nil {
		log.Printf("i18n: boot-time translator wiring failed (prompts degrade to message-ID echo): %v", err)
	}

	// Bootstrap permissions engine. A locally constructed PolicyEngine is used
	// here; T10/Phase 3 will thread it into the tool dispatcher so deny rules
	// actually block execution.
	policyEngine := confirmation.NewPolicyEngine()
	if err := c.initPermissions(ctx, policyEngine); err != nil {
		return fmt.Errorf("permissions bootstrap: %w", err)
	}
	if err := c.initPersistence(); err != nil {
		return fmt.Errorf("persistence init: %w", err)
	}
	if err := c.initWorktree(ctx); err != nil {
		return fmt.Errorf("worktree init: %w", err)
	}

	// Construct the session manager (carries the hooks.Manager inside it).
	sessionMgr := session.NewManager()
	c.sessionMgr = sessionMgr

	// Load hooks from ~/.helixcode/hooks.yaml + <cwd>/.helixcode/hooks.yaml
	// and register them with the session's hooks manager.
	if err := c.initHooks(ctx, sessionMgr); err != nil {
		return fmt.Errorf("hooks init: %w", err)
	}

	// Construct the tool registry and wire the hooks manager so that
	// BeforeToolCall / AfterToolCall / BeforeBash / AfterBash actually fire.
	toolReg, err := tools.NewToolRegistry(tools.DefaultRegistryConfig())
	if err != nil {
		return fmt.Errorf("tool registry init: %w", err)
	}
	toolReg.SetHooksManager(sessionMgr.GetHooksManager())
	// Wire F02 permissions engine into the confirmation pipeline.
	toolReg.GetConfirmation().SetPolicyEngine(policyEngine)
	c.toolRegistry = toolReg

	// P1-F19-T05: ask_user tool registration.
	askUserPrompter, askUserErr := askuser.NewStdinPrompter(askuser.StdinPrompterOptions{})
	if askUserErr != nil {
		log.Printf("ask_user: stdinPrompter construction failed; tool unavailable: %v", askUserErr)
	} else {
		toolReg.Register(askuser.NewAskUserTool(askUserPrompter))
		log.Printf("ask_user: wired (interactive=auto-detect, max-retries=%d, timeout=%s)",
			askuser.DefaultMaxRetries, askuser.DefaultTimeout)
	}

	// F13: LSP manager — curated 5-server allowlist filtered by exec.LookPath
	// at startup.
	curatedLSPSpecs := tools.CuratedServerSpecs()
	detectedLSPSpecs := tools.DetectAvailableServers(curatedLSPSpecs)
	lspWorkingDir, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("lsp manager: resolving cwd: %w", err)
	}
	lspManager := tools.NewLSPManager(lspWorkingDir, detectedLSPSpecs, zap.NewNop())
	c.addCleanup(func() {
		shutCtx, shutCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer shutCancel()
		_ = lspManager.Shutdown(shutCtx)
	})
	toolReg.SetLSPManager(lspManager)

	// Construct the MCP Manager, load merged config, start alwaysLoad servers.
	mcpMgr := mcp.NewManager()
	{
		var userMCPPath string
		if configHome, err := os.UserConfigDir(); err == nil {
			userMCPPath = filepath.Join(configHome, "helixcode", "mcp.yml")
		}
		projMCPPath := ".helixcode/mcp.yml"
		cfg, cfgErr := mcp.LoadMerged(userMCPPath, projMCPPath)
		if cfgErr != nil {
			log.Printf("mcp: config load failed: %v (continuing without MCP)", cfgErr)
		} else {
			mcpMgr.SetConfig(cfg)
			if startErr := mcpMgr.Start(ctx); startErr != nil {
				log.Printf("mcp: start failed: %v", startErr)
			}
		}
	}
	c.mcpManager = mcpMgr
	c.addCleanup(func() { _ = mcpMgr.Close() })

	// Wire MCP-discovered tools into the tool registry as "<server>:<tool>".
	toolReg.RegisterMCPManager(mcpMgr)

	// Build the commands registry and register all builtin slash commands.
	cmdRegistry := commands.NewRegistry()
	if regErr := builtin.RegisterBuiltinCommandsWithMCP(cmdRegistry, mcpMgr); regErr != nil {
		log.Printf("mcp: register slash command failed: %v", regErr)
	}
	c.commandRegistry = cmdRegistry

	// Share the live session-rule store with the /permissions command so rules
	// added via `/permissions add|remove` reach the SAME store the permissions
	// Engine consults at decision time (initPermissions, run earlier, wired the
	// engine via WithSessionDecider(c.sessionRuleStore.Decide)). Without this the
	// command and the engine hold two unconnected stores and a session rule never
	// gates live execution. Replaces the builtin-registered default (own store).
	if c.sessionRuleStore != nil {
		cmdRegistry.Unregister("permissions")
		if regErr := cmdRegistry.Register(commands.NewPermissionsCommandWithStore(c.sessionRuleStore)); regErr != nil {
			log.Printf("permissions: register store-shared slash command failed: %v", regErr)
		}
	}

	// F07: background task manager.
	bgMgr := workflow.NewBackgroundManager(zap.NewNop(), workflow.ManagerConfig{})
	c.addCleanup(func() { bgMgr.Close() })
	toolReg.SetBackgroundManager(bgMgr)
	toolReg.RegisterTaskTools(bgMgr)
	if regErr := cmdRegistry.Register(commands.NewTasksCommand(bgMgr)); regErr != nil {
		log.Printf("tasks: register slash command failed: %v", regErr)
	}

	// F08: plan-mode gate.
	modeCtrl := planmode.NewModeController()
	planner := planmode.NewDefaultPlanner()
	gate := planmode.NewToolGate(modeCtrl, planner)
	toolReg.SetPlanModeGate(gate)
	toolReg.Register(tools.NewEnterPlanModeTool(modeCtrl))
	toolReg.Register(tools.NewExitPlanModeTool(modeCtrl))
	if regErr := cmdRegistry.Register(commands.NewPlanCommand(planner, modeCtrl)); regErr != nil {
		log.Printf("plan: register slash command failed: %v", regErr)
	}

	// F13: register /lsp slash command.
	if regErr := cmdRegistry.Register(commands.NewLSPCommand(lspManager, curatedLSPSpecs)); regErr != nil {
		log.Printf("lsp: register slash command failed: %v", regErr)
	}

	// F14: sandbox manager + shell_sandboxed tool + /sandbox slash command.
	sandboxConfigPath := sandbox.DefaultConfigPath(os.Getenv)
	sandboxConfig, sandboxCfgErr := sandbox.LoadSandboxConfig(sandboxConfigPath)
	if sandboxCfgErr != nil {
		log.Printf("sandbox: config load failed (using defaults): %v", sandboxCfgErr)
		sandboxConfig = sandbox.DefaultSandboxConfig()
	}
	sandboxWorkDir, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("sandbox manager: resolving cwd: %w", err)
	}
	sandboxMgr, sandboxCaps, sbErr := sandbox.NewSandboxManagerFromDetector(sandboxWorkDir, sandboxConfig, zap.NewNop())
	if sbErr != nil {
		log.Printf("sandbox: manager init failed: %v", sbErr)
	} else {
		log.Printf("sandbox: backend=%s reason=%q",
			sandboxCaps.SelectedBackend.String(), sandboxCaps.UnavailableReason)
	}
	if sandboxMgr != nil {
		toolReg.Register(sandbox.NewSandboxedShellTool(sandboxMgr))
		if regErr := cmdRegistry.Register(commands.NewSandboxCommand(sandboxMgr)); regErr != nil {
			log.Printf("sandbox: register slash command failed: %v", regErr)
		}
	}

	// F21: approval gate.
	sandboxAvailable := sandboxMgr != nil && sandboxMgr.SelectedBackend() != sandbox.BackendNone
	approvalSelectorInput := approval.SelectorInput{
		Flag:       c.flagState.approvalFlag,
		Env:        os.Getenv(approval.EnvVarName),
		ConfigPath: approval.DefaultConfigPath(os.Getenv),
	}
	approvalMode, approvalSource, approvalSelErr := approval.Select(approvalSelectorInput)
	if approvalSelErr != nil {
		log.Printf("approval: selector reported parse errors (using mode=%s source=%s): %v",
			approvalMode, approvalSource, approvalSelErr)
	}
	approvalMgrOpts := approval.ApprovalManagerOptions{
		InitialMode:      approvalMode,
		Source:           approvalSource,
		SandboxAvailable: sandboxAvailable,
		PauseDangerous:   2 * time.Second,
	}
	if askUserPrompter != nil {
		approvalMgrOpts.Responder = &approvalwire.AskUserYesNoPrompter{Inner: askUserPrompter}
	}
	approvalMgr, approvalMgrErr := approval.NewApprovalManager(approvalMgrOpts)
	if approvalMgrErr != nil {
		log.Printf("approval: manager init failed for mode=%s (falling back to suggest): %v",
			approvalMode, approvalMgrErr)
		approvalMgrOpts.InitialMode = approval.ModeSuggest
		approvalMgrOpts.Source = approval.SourceDefault
		approvalMgr, approvalMgrErr = approval.NewApprovalManager(approvalMgrOpts)
		if approvalMgrErr != nil {
			return fmt.Errorf("approval manager init (fallback): %w", approvalMgrErr)
		}
	}
	toolReg.SetApprovalManager(approvalMgr)
	log.Printf("approval: mode=%s source=%s sandbox_available=%t",
		approvalMgr.Mode(), approvalMgr.Source(), sandboxAvailable)
	if regErr := cmdRegistry.Register(commands.NewApprovalCommand(approvalMgr)); regErr != nil {
		log.Printf("approval: register slash command failed: %v", regErr)
	}

	// F22: per-edit git auto-commit (Aider-style).
	acEnabled := os.Getenv(autocommit.EnvVarName) != "off"
	cwd, _ := os.Getwd()
	autoCommitter := autocommit.NewAutoCommitter(autocommit.Options{
		Enabled:    acEnabled,
		Provider:   c.llmProvider,
		WorkingDir: cwd,
		Logger:     zap.NewNop(),
	})
	toolReg.SetAutoCommitter(autoCommitter)
	log.Printf("git_auto_commit: enabled=%t cwd=%s git_repo=%t",
		autoCommitter.Enabled(), cwd, autoCommitter.IsGitRepo())
	if regErr := cmdRegistry.Register(commands.NewGitAutoCommitCommand(autoCommitter)); regErr != nil {
		log.Printf("git_auto_commit: register slash command failed: %v", regErr)
	}

	// F23: cline-style browser tool suite.
	browserMgr := browser.NewBrowserManager(browser.NewDefaultChromeDiscovery(), zap.NewNop())
	if err := tools.RegisterBrowserToolsV2(toolReg, browserMgr); err != nil {
		log.Printf("browser: register tools failed: %v", err)
	}
	c.browserManager = browserMgr
	if regErr := cmdRegistry.Register(commands.NewBrowserCommand(browserMgr)); regErr != nil {
		log.Printf("browser: register slash command failed: %v", regErr)
	}
	c.addCleanup(func() { _ = browserMgr.CloseSession() })

	// F24: codex-style project memory subsystem.
	memCwd, _ := os.Getwd()
	memLoader := projectmemory.NewMemoryLoader(zap.NewNop())
	memRegistry := projectmemory.NewMemoryRegistry(memLoader, memCwd)
	if _, err := memRegistry.Reload(ctx); err != nil {
		log.Printf("projectmemory: initial reload failed: %v", err)
	}
	memWatcher := projectmemory.NewMemoryWatcher(memRegistry, zap.NewNop())
	if err := memWatcher.Start(ctx); err != nil {
		log.Printf("projectmemory: watcher start failed (degrading to slash-only reload): %v", err)
	}
	c.addCleanup(func() { _ = memWatcher.Close() })
	if regErr := cmdRegistry.Register(commands.NewMemoryCommand(memRegistry)); regErr != nil {
		log.Printf("projectmemory: register slash command failed: %v", regErr)
	}
	c.memoryRegistry = memRegistry

	// F25: plandex-style plan tree system.
	planStore := plantree.NewFileStore(cwd)
	planSummariser := plantree.DeterministicSummariser{}
	if err := plantree.RegisterPlanTools(toolReg, planStore); err != nil {
		log.Printf("plantree: register tools failed: %v", err)
	}
	if regErr := cmdRegistry.Register(commands.NewPlanTreeCommand(planStore, planSummariser)); regErr != nil {
		log.Printf("plantree: register slash command failed: %v", regErr)
	}

	// F26: Openhands-style workspace + planner system.
	wsMgr, wsErr := workspace.NewWorkspaceManager()
	if wsErr != nil {
		log.Printf("workspace: manager init failed (container runtime not available): %v", wsErr)
	} else {
		toolReg.Register(workspace.NewWorkspaceCreateTool(wsMgr))
		toolReg.Register(workspace.NewWorkspaceListTool(wsMgr))
		toolReg.Register(workspace.NewWorkspaceCleanupTool(wsMgr))
		if regErr := cmdRegistry.Register(commands.NewOpenhandsCommand(wsMgr)); regErr != nil {
			log.Printf("openhands: register slash command failed: %v", regErr)
		}
	}
	plannerExec := taskplanner.NewSequentialExecutor(nil)
	toolReg.Register(taskplanner.NewTaskPlanTool(plannerExec))
	toolReg.Register(taskplanner.NewTaskStepTool(plannerExec))

	// F27: aider-style voice input + repo-map.
	// Wire the operator-configured capture backend (HELIX_VOICE_CAPTURE_CMD)
	// so NewVoiceRecorderWithCmd / VoiceConfig.CaptureCmd are actually
	// exercised in production instead of being dead. When unset, fall back
	// to the auto-detected arecord/sox/parec via NewVoiceRecorder(). The
	// value is a command NAME (whitespace-split, first field LookPath-
	// validated inside detectCaptureCmd — no shell injection).
	voiceCaptureCmd := os.Getenv("HELIX_VOICE_CAPTURE_CMD")
	var voiceRec *voice.VoiceRecorder
	if voiceCaptureCmd != "" {
		voiceRec = voice.NewVoiceRecorderWithCmd(voiceCaptureCmd)
	} else {
		voiceRec = voice.NewVoiceRecorder()
	}
	voiceTrans := voice.NewVoiceTranscriber(voice.VoiceConfig{
		WhisperAPIKey: os.Getenv("OPENAI_API_KEY"),
		CaptureCmd:    voiceCaptureCmd,
	})
	toolReg.Register(voice.NewVoiceStartTool(voiceRec))
	toolReg.Register(voice.NewVoiceStopTool(voiceRec))
	toolReg.Register(voice.NewVoiceTranscribeTool(voiceRec, voiceTrans))
	if regErr := cmdRegistry.Register(commands.NewAiderCommand(voiceRec, voiceTrans)); regErr != nil {
		log.Printf("aider: register slash failed: %v", regErr)
	}

	// F28: kilo-code AST-aware refactoring.
	kcEngine := kilocode.NewRenameEngine(cwd)
	toolReg.Register(kilocode.NewKiloRenameTool(kcEngine))
	kcAnalyzer, kcErr := kilocode.NewImpactAnalyzer(cwd)
	if kcErr == nil {
		toolReg.Register(kilocode.NewKiloImpactTool(kcAnalyzer))
	}
	kcRefactorer := kilocode.NewRefactorer(cwd)
	toolReg.Register(kilocode.NewKiloMultiEditTool(kcRefactorer))
	if regErr := cmdRegistry.Register(commands.NewKilocodeCommand(kcEngine, kcAnalyzer, kcRefactorer)); regErr != nil {
		log.Printf("kilocode: register slash failed: %v", regErr)
	}

	// F29: Roo-code full port.
	rooDelegator := roocode.NewTaskDelegator()
	toolReg.Register(roocode.NewRooDelegateTool(rooDelegator))
	rooGen := roocode.NewCodeGenerator(cwd)
	toolReg.Register(roocode.NewRooGenerateTool(rooGen))
	toolReg.Register(roocode.NewRooBootstrapTool(rooGen))
	rooReviewer := roocode.NewCodeReviewer()
	rooConvStore := roocode.NewConversationStore()
	if regErr := cmdRegistry.Register(commands.NewRooCodeCommand(rooDelegator, rooGen, rooReviewer, rooConvStore)); regErr != nil {
		log.Printf("roocode: register slash failed: %v", regErr)
	}

	// F30: Continue.dev IDE integration.
	contEditor := continua.NewWorkspaceEditor()
	toolReg.Register(continua.NewContinueEditTool(contEditor))
	contCompletion := continua.NewCompletionEngine()
	toolReg.Register(continua.NewContinueCompleteTool(contCompletion))
	contChat := continua.NewChatManager()
	if regErr := cmdRegistry.Register(commands.NewContinueCommand(contEditor, contCompletion, contChat)); regErr != nil {
		log.Printf("continue: register slash failed: %v", regErr)
	}

	// F09: user-defined Markdown slash commands.
	projectCmds := filepath.Join(".", ".helix", "commands")
	var userCmds string
	if userCfg, err := os.UserConfigDir(); err == nil {
		userCmds = filepath.Join(userCfg, "helixcode", "commands")
	}
	mdLoader := commands.NewMarkdownLoader(cmdRegistry, projectCmds, userCmds)
	if loadErr := mdLoader.Load(); loadErr != nil {
		log.Printf("markdown commands: load failed: %v", loadErr)
	}
	mdWatcher, mdwErr := commands.NewMarkdownWatcher(mdLoader, []string{projectCmds, userCmds})
	if mdwErr != nil {
		log.Printf("markdown commands: watcher init failed: %v", mdwErr)
	} else {
		go mdWatcher.Run(ctx)
		c.addCleanup(func() { _ = mdWatcher.Close() })
	}
	if regErr := cmdRegistry.Register(commands.NewCommandsCommand(mdLoader, cmdRegistry)); regErr != nil {
		log.Printf("commands: register slash failed: %v", regErr)
	}

	// F10: agent-invoked Skills.
	skillProjectDir := filepath.Join(".", ".helix", "skills")
	var skillUserDir string
	if userCfg, err := os.UserConfigDir(); err == nil {
		skillUserDir = filepath.Join(userCfg, "helixcode", "skills")
	}
	skillReg := commands.NewSkillRegistry()
	skillLoader := commands.NewSkillLoader(skillReg, skillProjectDir, skillUserDir)
	if loadErr := skillLoader.Load(); loadErr != nil {
		log.Printf("skills: load failed: %v", loadErr)
	}
	skillWatcher, swErr := commands.NewSkillsWatcher(skillLoader, []string{skillProjectDir, skillUserDir})
	if swErr != nil {
		log.Printf("skills: watcher init failed: %v", swErr)
	} else {
		go skillWatcher.Run(ctx)
		c.addCleanup(func() { _ = skillWatcher.Close() })
	}
	// HXC-162: the dispatcher is stored on the CLI so the interactive REPL can
	// consult it for every plain-language line. It was previously constructed
	// and immediately discarded into `_`, which made skill auto-trigger dead in
	// the CLI even though the registry, loader and watcher above all worked.
	// The wiring mirrors the terminal-UI front-end, which has routed input
	// through a dispatcher this way since F10 landed.
	c.skillDispatcher = agent.NewSkillDispatcher(skillReg, nil)
	if regErr := cmdRegistry.Register(commands.NewSkillsCommand(skillLoader, skillReg)); regErr != nil {
		log.Printf("skills: register slash failed: %v", regErr)
	}

	// F11: session transcript persistence + resume.
	transcriptStore := session.NewTranscriptStore(sessionStoreBaseDir())
	resumeFinder := session.NewResumeFinder(transcriptStore)
	resumeMgr := session.NewSessionManager()
	resumeMgr.SetStore(transcriptStore)

	currentProject, projErr := session.ComputeProjectIdentity()
	if projErr != nil {
		log.Printf("session: project identity unresolved: %v (continuing with empty scope)", projErr)
		currentProject = ""
	}

	// Process F11 resume flags BEFORE the interactive loop runs.
	if c.flagState.resumeSessionFlag != "" {
		if err := resumeMgr.Resume(ctx, c.flagState.resumeSessionFlag); err != nil {
			return fmt.Errorf("resume session %s: %w", c.flagState.resumeSessionFlag, err)
		}
		fmt.Fprintln(os.Stderr, tr(ctx, "cli_session_resumed", map[string]any{
			"ID":    resumeMgr.CurrentID(),
			"Count": resumeMgr.LoadedMessageCountForTestF11(),
		}))
	} else if c.flagState.resumeFlag || c.flagState.continueFlag {
		mode := session.ResumeProject
		scope := currentProject
		if c.flagState.continueFlag {
			mode = session.ResumeGlobal
			scope = ""
		}
		target, ferr := resumeFinder.FindResumeTarget(ctx, mode, scope)
		if ferr != nil {
			fmt.Fprintln(os.Stderr, tr(ctx, "cli_session_no_resumable",
				map[string]any{"Error": fmt.Sprintf("%v", ferr)}))
		} else {
			if err := resumeMgr.Resume(ctx, target.SessionID); err != nil {
				return fmt.Errorf("resume session %s: %w", target.SessionID, err)
			}
			fmt.Fprintln(os.Stderr, tr(ctx, "cli_session_resumed_active", map[string]any{
				"ID":         resumeMgr.CurrentID(),
				"Count":      resumeMgr.LoadedMessageCountForTestF11(),
				"LastActive": target.LastActivity.Format("2006-01-02 15:04:05"),
			}))
		}
	}
	if regErr := cmdRegistry.Register(commands.NewSessionsCommand(transcriptStore, currentProject)); regErr != nil {
		log.Printf("sessions: register slash failed: %v", regErr)
	}

	// P1-F16-T10: Wire telemetry into the tool registry. The provider is the
	// shared lazy telemetry() instance — also used by the F12 LLM decorator.
	telemetryProv := c.telemetry()
	if toolInstr, tiErr := telemetry.NewToolInstrumentation(telemetryProv); tiErr != nil {
		log.Printf("telemetry: tool instrumentation construction failed: %v", tiErr)
	} else {
		toolReg.SetTelemetryInstrumentation(toolInstr)
	}
	if regErr := cmdRegistry.Register(commands.NewTelemetryCommand(telemetryProv)); regErr != nil {
		log.Printf("telemetry: register slash command failed: %v", regErr)
	}

	// P1-F20-T07: Register the /theme slash command.
	{
		slashThemeRegistry := theme.NewThemeRegistry()
		if path := theme.DefaultThemePath(os.Getenv); path != "" {
			if err := slashThemeRegistry.LoadFromFile(path); err != nil {
				log.Printf("theme(slash): yaml load failed (continuing with built-ins): %v", err)
			}
		}
		slashThemeName := theme.DetectThemeName(os.Getenv)
		slashSelectedTheme, slashErr := slashThemeRegistry.Get(slashThemeName)
		if slashErr != nil {
			log.Printf("theme(slash): get %q failed (%v), falling back to dark", slashThemeName, slashErr)
			slashSelectedTheme, _ = slashThemeRegistry.Get(theme.ThemeDark)
			slashThemeName = theme.ThemeDark
		}
		slashColorDepth := theme.DetectColorDepth(os.Getenv)
		slashSource := commands.ResolveThemeSource(os.Getenv)
		slashStyler := theme.NewStyler(slashSelectedTheme, slashColorDepth)
		if regErr := cmdRegistry.Register(commands.NewThemeCommand(slashThemeRegistry, slashThemeName, slashColorDepth, slashSource, slashStyler)); regErr != nil {
			log.Printf("theme: register slash command failed: %v", regErr)
		}
	}

	// P1-F15-T10: Subagent system wiring. c.llmProvider has already settled
	// (ensureLLMProvider ran before this getter via ensureSubsystems).
	{
		subagentLogger := zap.NewNop()
		subagentWorkDir, swdErr := os.Getwd()
		if swdErr != nil {
			log.Printf("subagent: resolving cwd failed (skipping wire-in): %v", swdErr)
		} else if c.llmProvider == nil {
			log.Printf("subagent: no LLM provider available (F12 default failed); skipping wire-in")
		} else {
			inProcessSpawner := subagent.NewInProcessSpawner()
			subprocessSpawner, spErr := subagent.NewSubprocessSpawner(subagentWorkDir)
			if spErr != nil {
				log.Printf("subagent: subprocess spawner unavailable (continuing in-process only): %v", spErr)
			}
			subagentMgr, smErr := subagent.NewSubagentManager(subagent.SubagentManagerOptions{
				InProcessSpawner:  inProcessSpawner,
				SubprocessSpawner: subprocessSpawner,
				LLMProvider:       c.llmProvider,
				Logger:            subagentLogger,
				WorkDir:           subagentWorkDir,
			})
			if smErr != nil {
				log.Printf("subagent: manager construction failed (skipping wire-in): %v", smErr)
			} else {
				c.addCleanup(func() {
					shutCtx, shutCancel := context.WithTimeout(context.Background(), 5*time.Second)
					defer shutCancel()
					_ = subagentMgr.Shutdown(shutCtx)
				})
				toolReg.Register(task.NewTaskTool(subagentMgr))
				if regErr := cmdRegistry.Register(commands.NewSubagentsCommand(subagentMgr)); regErr != nil {
					log.Printf("subagent: register slash command failed: %v", regErr)
				}
				log.Printf("subagent: manager initialised (max_concurrency=%d)",
					subagent.DefaultMaxConcurrency)
			}
		}
	}

	// P1-F17-T08: Smart File Editing wiring.
	{
		mfe := toolReg.GetMultiEdit()
		if mfe == nil {
			log.Printf("smart-edit: multiedit unavailable; smart_edit tool + /edit slash skipped")
		} else {
			smartWorkDir, swdErr := os.Getwd()
			if swdErr != nil {
				log.Printf("smart-edit: resolving cwd failed (skipping wire-in): %v", swdErr)
			} else {
				committer := smartedit.NewMultieditCommitter(mfe)
				smartTool := smartedit.NewSmartEditTool(committer, smartWorkDir)
				toolReg.Register(smartTool)
				if regErr := cmdRegistry.Register(commands.NewEditCommand(smartTool)); regErr != nil {
					log.Printf("smart-edit: register slash command failed: %v", regErr)
				}
				log.Printf("smart-edit: wired (workdir=%s)", smartWorkDir)
			}
		}
	}

	return nil
}

// Run executes the CLI
func (c *CLI) Run() error {
	// Parse command-line flags
	var (
		command        = flag.String("command", "", "Command to execute")
		workerHost     = flag.String("worker", "", "Worker host to add")
		workerUser     = flag.String("user", "", "Worker SSH username")
		workerKey      = flag.String("key", "", "Worker SSH key path")
		model          = flag.String("model", "llama-3-8b", "LLM model to use")
		prompt         = flag.String("prompt", "", "Prompt for LLM generation")
		maxTokens      = flag.Int("max-tokens", 1000, "Maximum tokens to generate")
		temperature    = flag.Float64("temperature", 0.7, "Generation temperature")
		stream         = flag.Bool("stream", false, "Stream the response")
		listWorkers    = flag.Bool("list-workers", false, "List all workers")
		listModels     = flag.Bool("list-models", false, "List available models")
		healthCheck    = flag.Bool("health", false, "Perform health check")
		notify         = flag.String("notify", "", "Send notification with message")
		notifyType     = flag.String("notify-type", "info", "Notification type")
		notifyPriority = flag.String("notify-priority", "medium", "Notification priority")
		nonInteractive = flag.Bool("non-interactive", false, "Run in non-interactive mode")
		permissionMode = flag.String("permission-mode", "", "permission preset: default|auto|acceptEdits|dontAsk|bypassPermissions")

		// QA flags
		qaRun        = flag.Bool("qa-run", false, "Start a QA session")
		qaList       = flag.Bool("qa-list", false, "List QA sessions")
		qaReport     = flag.String("qa-report", "", "Get QA report for session ID")
		qaScreenshot = flag.String("qa-screenshot", "", "Capture screenshot for session ID")
		qaCancel     = flag.String("qa-cancel", "", "Cancel QA session by ID")
		qaPlatforms  = flag.String("qa-platforms", "web", "Comma-separated platforms for QA")
		qaBanks      = flag.String("qa-banks", "", "Comma-separated bank paths for QA")
		qaFormat     = flag.String("qa-format", "markdown", "Report format: markdown|html|json")
		qaWait       = flag.Bool("qa-wait", false, "Wait for QA session to complete")
		qaServerURL  = flag.String("qa-server", "http://localhost:8080", "HelixCode server URL for QA")

		// F11: session transcript resume.
		// `--resume` resumes the most recently active session for the current project.
		// `--continue` resumes the most recently active session globally (any project).
		// `--resume-session <id>` resumes a specific session by ID (overrides the bool flags).
		// We use three separate flags rather than a NoOptDefVal sentinel because this
		// command-line surface is built on stdlib `flag`, which does not support
		// optional-value flags. Three flags also disambiguate "no id supplied" from
		// "id is the empty string".
		resumeFlag        = flag.Bool("resume", false, "Resume most recent session for current project (F11)")
		continueFlag      = flag.Bool("continue", false, "Resume most recent session globally across projects (F11)")
		resumeSessionFlag = flag.String("resume-session", "", "Resume a specific session by ID (F11)")

		// F12: cloud LLM provider override.
		// Precedence (handled by llm.Select): --provider > HELIX_LLM_PROVIDER > config-file > wizard.
		// On ErrNoProviderConfigured we print a friendly message and continue with
		// the existing default Ollama provider — we never auto-launch the TUI here
		// because that would hang non-TTY runs. Users start the wizard explicitly
		// via `helixcode wizard`.
		providerFlag = flag.String("provider", "", "F12 cloud LLM provider override (anthropic|bedrock|vertexai|azure)")

		// F21: approval mode override.
		// Precedence (handled by approval.Select): --approval > HELIXCODE_APPROVAL >
		// $XDG_CONFIG_HOME/helixcode/approval.yaml > built-in default (suggest).
		// Garbage values fall through to the next source; the selector aggregates
		// parse errors so we can warn the user without losing the runtime mode.
		approvalFlag = flag.String("approval", "", "F21 approval mode override (suggest|auto-edit|full-auto|dangerously-bypass)")

		// P0-T01 (speed programme): opt-in pprof capture. When --pprof <dir> is
		// supplied (or the HELIX_PPROF env var is set), the run writes a CPU
		// profile (<dir>/cpu.pprof) and a heap profile (<dir>/heap.pprof). It is
		// OFF by default — when neither the flag nor the env var is set there is
		// zero behaviour change to the CLI's normal path. The captured profiles
		// are the anti-bluff baseline evidence for the Phase 0 measurement gate.
		pprofDir = flag.String("pprof", "", "P0-T01 speed programme: write CPU+heap pprof profiles to this directory (off by default; also via HELIX_PPROF env)")

		versionFlag = flag.Bool("version", false, "Print the HelixCode brand banner + version, then exit")
	)
	flag.Parse()

	// --version: print the HelixCode brand banner (lime/teal wordmark,
	// NO_COLOR / non-TTY aware) followed by the resolved version string, then
	// exit. Placed immediately after flag.Parse() so it short-circuits before
	// any subsystem or LLM-provider construction.
	if *versionFlag {
		fmt.Print(brandBanner())
		fmt.Println("  " + version.GetFullVersion())
		return nil
	}

	// P0-T01: start opt-in pprof capture immediately after flag parsing so the
	// profile covers as much of the run as possible. pprofutil.Start returns a
	// nil *Capture when profiling was not requested — the deferred Stop is then
	// a safe no-op, so this adds nothing to the unprofiled hot path.
	if profDir := pprofutil.ResolveDir(*pprofDir, os.Getenv); profDir != "" {
		pc, perr := pprofutil.Start(profDir, "")
		if perr != nil {
			log.Printf("pprof: capture disabled — %v", perr)
		} else {
			fmt.Fprintf(os.Stderr, "pprof: capturing CPU profile to %s\n", pc.CPUPath())
			defer func() {
				elapsed, heapPath, stopErr := pc.Stop("")
				if stopErr != nil {
					log.Printf("pprof: stop failed: %v", stopErr)
					return
				}
				fmt.Fprintf(os.Stderr, "pprof: wrote profiles after %s (heap: %s)\n", elapsed, heapPath)
			}()
		}
	}

	// Debug: print flag values
	fmt.Fprintln(os.Stderr, trc("cli_debug_flags_parsed", map[string]any{
		"ListWorkers":    fmt.Sprintf("%v", *listWorkers),
		"NonInteractive": fmt.Sprintf("%v", *nonInteractive),
	}))

	ctx := context.Background()

	// Speed programme P1-T03: lazy CLI startup.
	//
	// The heavy interactive-session subsystem cluster (telemetry, permissions,
	// persistence, worktree git shell-out, hooks YAML, the tool registry + the
	// full F07-F30 tool/slash-command suite, LSP exec.LookPath sweep, MCP
	// server spawn, sandbox detection, the F15 subagent manager, the F09/F10
	// markdown-command + skill loaders/watchers) is NO LONGER built eagerly
	// here. It is constructed on demand by c.ensureSubsystems(), gated by a
	// sync.Once. Likewise the F12 cloud-provider override + telemetry-wrapped
	// LLM provider is built on demand by c.ensureLLMProvider().
	//
	// Snapshot the parsed flag values the lazy getters consume so they need no
	// *flag receiver (the getters are invoked from command handlers).
	c.permissionMode = *permissionMode
	c.flagState = cliFlagState{
		approvalFlag:      *approvalFlag,
		providerFlag:      *providerFlag,
		resumeFlag:        *resumeFlag,
		continueFlag:      *continueFlag,
		resumeSessionFlag: *resumeSessionFlag,
	}

	// Single teardown seam: whatever a lazily-constructed subsystem registered
	// via addCleanup is drained here at Run() exit (LIFO — the order Go's
	// `defer` stack used in the old eager monolith).
	defer c.runCleanups()

	// Handle different commands
	switch {
	case *listWorkers:
		return c.handleListWorkers(ctx)
	case *listModels:
		// --list-models needs the resolved LLM provider (F12 + telemetry
		// wrapper) but NOT the heavy subsystem cluster.
		if err := c.ensureLLMProvider(ctx, c.flagState.providerFlag); err != nil {
			return err
		}
		return c.handleListModels(ctx)
	case *healthCheck:
		return c.handleHealthCheck(ctx)
	case *workerHost != "":
		return c.handleAddWorker(ctx, *workerHost, *workerUser, *workerKey)
	case *prompt != "":
		// --prompt (generate) needs the resolved LLM provider but NOT the
		// heavy subsystem cluster.
		if err := c.ensureLLMProvider(ctx, c.flagState.providerFlag); err != nil {
			return err
		}
		return c.handleGenerate(ctx, *prompt, *model, *maxTokens, *temperature, *stream)
	case *notify != "":
		return c.handleNotification(ctx, *notify, *notifyType, *notifyPriority)
	case *qaRun:
		return c.handleQARun(ctx, *qaServerURL, *qaPlatforms, *qaBanks, *qaWait)
	case *qaList:
		return c.handleQAList(ctx, *qaServerURL)
	case *qaReport != "":
		return c.handleQAReport(ctx, *qaServerURL, *qaReport, *qaFormat)
	case *qaScreenshot != "":
		return c.handleQAScreenshot(ctx, *qaServerURL, *qaScreenshot)
	case *qaCancel != "":
		return c.handleQACancel(ctx, *qaServerURL, *qaCancel)
	case *command != "":
		return c.handleCommand(ctx, *command)
	case *nonInteractive:
		// In non-interactive mode, exit gracefully if no command specified
		return nil
	default:
		// The interactive REPL is the one path that genuinely needs the full
		// slash-command registry + agent tool surface, so it (and only it)
		// triggers the heavy subsystem cluster construction.
		if err := c.ensureSubsystems(ctx); err != nil {
			return err
		}
		return c.handleInteractive(ctx)
	}
}

// handleListWorkers lists all workers
func (c *CLI) handleListWorkers(ctx context.Context) error {
	stats := c.workerPool.GetWorkerStats(ctx)

	fmt.Println()
	fmt.Println(tr(ctx, "cli_workers_header", nil))
	fmt.Println(tr(ctx, "cli_workers_total", map[string]any{"Count": stats.TotalWorkers}))
	fmt.Println(tr(ctx, "cli_workers_active", map[string]any{"Count": stats.ActiveWorkers}))
	fmt.Println(tr(ctx, "cli_workers_healthy", map[string]any{"Count": stats.HealthyWorkers}))
	fmt.Println(tr(ctx, "cli_workers_total_cpu", map[string]any{"Count": stats.TotalCPU}))
	fmt.Println(tr(ctx, "cli_workers_total_memory_gb", map[string]any{"GB": fmt.Sprintf("%.2f", float64(stats.TotalMemory)/(1024*1024*1024))}))
	fmt.Println(tr(ctx, "cli_workers_total_gpu", map[string]any{"Count": stats.TotalGPU}))

	return nil
}

// handleListModels lists available models.
// BLUFF-002 FIX: Uses LLMsVerifier as the single source of truth when enabled.
// Falls back to provider discovery and then to the constitutional fallback list.
func (c *CLI) handleListModels(ctx context.Context) error {
	fmt.Println()
	fmt.Println(tr(ctx, "cli_models_header", nil))

	// Priority 1: LLMsVerifier adapter (CONST-036 single source of truth).
	//
	// D-2 (SP1): list only WORKING models — key-present provider ∧ Verified ∧
	// status=="verified" ∧ score>=min. Presenting failed/pending/rate-limited or
	// no-key models as available is a §11.4 / CONST-035 PASS-bluff (the user
	// cannot actually use them).
	if c.verifierAdapter != nil && c.verifierAdapter.IsEnabled() {
		// Key-presence gate sourced from the committed, multi-alias
		// llm.PresentProviderNames() — the SAME funnel input the server's
		// model-listing path consumes (handlers.go listLLMModels). This
		// retires the CLI-local single-alias presentProviders table at the
		// production call site so both surfaces recognize keys identically
		// (e.g. CLAUDE_API_KEY / DASHSCOPE_API_KEY secondary aliases).
		present := llm.PresentProviderNames()
		models, err := c.verifierAdapter.GetWorkingModels(ctx, present)
		if err == nil && len(models) > 0 {
			c.printVerifiedModels(models)

			// Supplement: for any key-present provider with zero verified
			// models (verifier DB has no data for that provider yet), append
			// seed models from FallbackModels so the user sees ALL providers
			// they configured — not just those the verifier has scored.
			covered := make(map[string]bool, len(models))
			for _, m := range models {
				covered[m.Provider] = true
			}
			c.printSupplementalModels(present, covered)
			return nil
		}
		// Log warning but continue to fallback
		fmt.Fprintf(os.Stderr, "⚠️  Verifier unavailable (%v), using fallback...\n", err)
	}

	// Priority 2: Provider's own model list (e.g., Ollama /api/tags)
	if c.llmProvider != nil {
		providerModels := c.llmProvider.GetModels()
		if len(providerModels) > 0 {
			for _, m := range providerModels {
				fmt.Printf("%s\n\n", tr(ctx, "cli_model_info_provider", map[string]any{
					"ID":          m.ID,
					"Name":        m.Name,
					"Provider":    m.Provider,
					"ContextSize": m.ContextSize,
				}))
			}
			// Supplement: show seed models for other key-present providers
			// that are not the active LLM provider.
			present := llm.PresentProviderNames()
			covered := make(map[string]bool)
			covered[string(c.llmProvider.GetType())] = true
			c.printSupplementalModels(present, covered)
			return nil
		}
	}

	// Priority 3: Constitutional fallback list (CONST-035 compliance).
	//
	// D-2: fallback models are NOT verifier-confirmed "working" — they are an
	// offline baseline. They MUST NOT be rendered through printVerifiedModels
	// (which now asserts Verified+verified status), and MUST carry an explicit
	// "unverified fallback" label so the user is never told they are verified.
	c.printFallbackModels(verifier.FallbackModels)
	fmt.Printf("ℹ️  %s\n", tr(ctx, "cli_models_fallback_notice", nil))
	return nil
}

// printFallbackModels renders the offline fallback list with an explicit
// unverified label (D-2 anti-bluff: never claim fallback == verified working).
func (c *CLI) printFallbackModels(models []*verifier.VerifiedModel) {
	fmt.Print(renderFallbackModels(models))
}

// renderFallbackModels builds the offline-fallback --list-models output for
// a slice of fallback models and returns it as a string (HXC-117 follow-up
// fix: testable seam mirroring renderVerifiedModels below).
//
// Fallback rows share the cli_model_info_verified template with
// renderVerifiedModels -- that template gained a "Capabilities:" line when
// HXC-117 first landed, but printFallbackModels (this function's
// predecessor) supplied no "Capabilities" key. Go's text/template renders a
// missing map key as the literal string "<no value>", so every offline
// fallback row leaked "Capabilities: <no value>" to end users -- a §11.4.1
// fix-A-breaks-B regression against this file's OTHER caller of the shared
// template key. Fallback models are reached precisely when the verifier is
// unavailable (Priority 3 in handleListModels), so no per-flag CONST-040
// capability data exists for them; fabricating ✓/✗ values here would itself
// be a CONST-040 / BLUFF-002 bluff. The honest fix is an explicit,
// i18n-sourced "capabilities unknown" indicator -- never the raw
// text/template zero-value string, never a fabricated capability.
func renderFallbackModels(models []*verifier.VerifiedModel) string {
	var sb strings.Builder
	for _, m := range models {
		scoreStr := fmt.Sprintf("SC:%.1f", m.OverallScore)
		sb.WriteString(fmt.Sprintf("%s\n\n", trc("cli_model_info_verified", map[string]any{
			"ID":           m.ID,
			"Name":         m.DisplayName,
			"Provider":     m.Provider,
			"Score":        scoreStr,
			"ContextSize":  m.ContextSize,
			"Status":       "⚠ unverified fallback",
			"Capabilities": trc("cli_capability_flags_unknown", nil),
		})))
	}
	return sb.String()
}

// printSupplementalModels appends seed models from FallbackModels for any
// key-present provider whose models were NOT already covered by a prior
// display pass (verified or provider-owned). This ensures that providers
// with a configured API key always appear in --list-models output, even
// when the verifier DB has no data for them yet.
func (c *CLI) printSupplementalModels(present map[string]bool, covered map[string]bool) {
	var supplemental []*verifier.VerifiedModel
	for _, fm := range verifier.FallbackModels {
		if present[fm.Provider] && !covered[fm.Provider] {
			supplemental = append(supplemental, fm)
		}
	}
	if len(supplemental) > 0 {
		c.printFallbackModels(supplemental)
	}
}

// providerEnvAliases maps a verifier provider type to the environment-variable
// aliases that supply that provider's API key. A non-empty, non-placeholder
// value in ANY alias marks the provider key-present. Decoupled, data-only table
// (lifted from helix_agent SupportedProviders.EnvVars per §11.4.74); converges
// on the shared substrate when the replace dev.helix.agent wiring lands.
var providerEnvAliases = map[string][]string{
	"openai":     {"OPENAI_API_KEY"},
	"anthropic":  {"ANTHROPIC_API_KEY", "CLAUDE_API_KEY"},
	"gemini":     {"GEMINI_API_KEY", "GOOGLE_API_KEY"},
	"deepseek":   {"DEEPSEEK_API_KEY"},
	"groq":       {"GROQ_API_KEY"},
	"mistral":    {"MISTRAL_API_KEY"},
	"xai":        {"XAI_API_KEY", "GROK_API_KEY"},
	"openrouter": {"OPENROUTER_API_KEY"},
	"xiaomi":     {"XIAOMI_MIMO_API_KEY", "ApiKey_Xiaomi_MiMo"},
}

// isPlaceholderKey reports whether a key value is an obvious placeholder that
// must NOT count as a recognized key (e.g. ".env.example" stubs).
func isPlaceholderKey(v string) bool {
	v = strings.TrimSpace(v)
	if v == "" {
		return true
	}
	lower := strings.ToLower(v)
	for _, p := range []string{"your-", "your_", "changeme", "placeholder", "xxx", "sk-...", "<", "example"} {
		if strings.Contains(lower, p) {
			return true
		}
	}
	return false
}

// presentProviders returns the set of provider types for which a usable API key
// was recognized in the environment (after D-3 secrets.LoadAPIKeys ran at
// startup). getenv is injected for testability. This is the key-presence input
// to the working-model funnel (D-2): only key-present providers' models display.
func presentProviders(getenv func(string) string) map[string]bool {
	present := make(map[string]bool, len(providerEnvAliases))
	for provider, aliases := range providerEnvAliases {
		for _, alias := range aliases {
			if v := getenv(alias); !isPlaceholderKey(v) {
				present[provider] = true
				break
			}
		}
	}
	return present
}

// isWorkingForDisplay reports whether a model is usable enough to show in the
// "available models" listing (D-2). A model is working iff it is Verified with
// status "verified". failed / pending / rate-limited rows are NOT working and
// MUST NOT be presented as available (§11.4 / CONST-035 — listing an unusable
// model is a PASS-bluff at the model-listing layer).
func isWorkingForDisplay(m *verifier.VerifiedModel) bool {
	return m.Verified && m.VerificationStatus == "verified"
}

func (c *CLI) printVerifiedModels(models []*verifier.VerifiedModel) {
	fmt.Print(renderVerifiedModels(models))
}

// renderVerifiedModels builds the --list-models output for a slice of
// verified models and returns it as a string (HXC-117: split out of
// printVerifiedModels as a testable seam -- the stdout call site above is
// unchanged, but unit tests can now assert on the rendered text without
// capturing os.Stdout).
func renderVerifiedModels(models []*verifier.VerifiedModel) string {
	var sb strings.Builder
	for _, m := range models {
		// D-2 defensive gate: never render a non-working row, regardless of
		// which caller supplied the list (the verifier funnel SHOULD already
		// have filtered, but the printer must not be the place a bluff leaks).
		if !isWorkingForDisplay(m) {
			continue
		}
		status := "✓ verified"
		scoreStr := fmt.Sprintf("SC:%.1f", m.OverallScore)
		sb.WriteString(fmt.Sprintf("%s\n\n", trc("cli_model_info_verified", map[string]any{
			"ID":           m.ID,
			"Name":         m.DisplayName,
			"Provider":     m.Provider,
			"Score":        scoreStr,
			"ContextSize":  m.ContextSize,
			"Status":       status,
			"Capabilities": formatCapabilityFlags(m),
		})))
	}
	return sb.String()
}

// formatCapabilityFlags renders the 6 CONST-040 capability flags (MCP / LSP
// / ACP / RAG / Skills / Plugins) for CLI display, sourced directly from
// the verifier-decoded VerifiedModel fields (internal/verifier/types.go
// SupportsMCP / SupportsLSP / SupportsACP / SupportsRAG / SupportsSkills /
// SupportsPlugins -- the fields client.go's VerifyModel decodes from the
// LLMsVerifier response). NEVER hardcode a capability value here: CONST-040
// / BLUFF-002 requires every displayed capability to trace back to
// LLMsVerifier via the decoded model, never a constant.
//
// CONST-046: MCP/LSP/ACP/RAG are protocol acronyms (technical terms, not
// translatable prose) so they stay as literals here, matching the JSON key
// convention on the server surface. The Skills/Plugins labels and the
// supported/unsupported indicators ARE user-facing translatable content, so
// they are i18n-sourced from the active bundle rather than hardcoded English
// words / hardcoded glyphs.
func formatCapabilityFlags(m *verifier.VerifiedModel) string {
	supportedIndicator := trc("cli_capability_indicator_supported", nil)
	unsupportedIndicator := trc("cli_capability_indicator_unsupported", nil)
	skillsLabel := trc("cli_capability_label_skills", nil)
	pluginsLabel := trc("cli_capability_label_plugins", nil)

	flag := func(supported bool, label string) string {
		if supported {
			return label + ":" + supportedIndicator
		}
		return label + ":" + unsupportedIndicator
	}
	return strings.Join([]string{
		flag(m.SupportsMCP, "MCP"),
		flag(m.SupportsLSP, "LSP"),
		flag(m.SupportsACP, "ACP"),
		flag(m.SupportsRAG, "RAG"),
		flag(m.SupportsSkills, skillsLabel),
		flag(m.SupportsPlugins, pluginsLabel),
	}, " ")
}

// handleHealthCheck performs system health check
func (c *CLI) handleHealthCheck(ctx context.Context) error {
	fmt.Println()
	fmt.Println(tr(ctx, "cli_health_header", nil))

	// Check worker pool
	stats := c.workerPool.GetWorkerStats(ctx)
	if stats.HealthyWorkers > 0 {
		fmt.Printf("✅ %s\n", tr(ctx, "cli_health_worker_pool_ok",
			map[string]any{"Count": stats.HealthyWorkers}))
	} else {
		fmt.Printf("⚠️ %s\n", tr(ctx, "cli_health_worker_pool_none", nil))
	}

	// Check notification engine
	channelStats := c.notificationEngine.GetChannelStats()
	enabledChannels := 0
	for _, stats := range channelStats {
		if statsMap, ok := stats.(map[string]interface{}); ok {
			if enabled, ok := statsMap["enabled"].(bool); ok && enabled {
				enabledChannels++
			}
		}
	}

	if enabledChannels > 0 {
		fmt.Printf("✅ %s\n", tr(ctx, "cli_health_notification_ok",
			map[string]any{"Count": enabledChannels}))
	} else {
		fmt.Printf("⚠️ %s\n", tr(ctx, "cli_health_notification_none", nil))
	}

	fmt.Printf("✅ %s\n", tr(ctx, "cli_health_operational", nil))
	return nil
}

// handleAddWorker adds a new worker
func (c *CLI) handleAddWorker(ctx context.Context, host, username, keyPath string) error {
	if username == "" {
		return fmt.Errorf("username is required")
	}

	sshConfig := &worker.SSHWorkerConfig{
		Host:     host,
		Port:     22,
		Username: username,
		KeyPath:  keyPath,
	}

	worker := &worker.SSHWorker{
		Hostname:    host,
		DisplayName: fmt.Sprintf("worker-%s", host),
		SSHConfig:   sshConfig,
	}

	if err := c.workerPool.AddWorker(ctx, worker); err != nil {
		return fmt.Errorf("failed to add worker: %v", err)
	}

	fmt.Printf("✅ %s\n", tr(ctx, "cli_worker_added_success", map[string]any{"Host": host}))
	return nil
}

// isKnownProviderPrefix reports whether seg (the part before the first ":" in a
// "provider:model" string) names a recognized LLM provider type. It is sourced
// from the canonical llm.ProviderType* constants so it cannot drift from the
// provider registry (CONST-036/CONST-039). Comparison is case-insensitive.
//
// HXC-096: this gate is what distinguishes a real provider prefix
// ("ollama:qwen2.5:3b" → strip "ollama") from an Ollama native "name:tag"
// model id ("qwen2.5:3b" → keep intact, because "qwen2.5" is NOT a provider).
func isKnownProviderPrefix(seg string) bool {
	switch llm.ProviderType(strings.ToLower(seg)) {
	case llm.ProviderTypeOllama,
		llm.ProviderTypeLocal,
		llm.ProviderTypeLlamaCpp,
		llm.ProviderTypeOpenAI,
		llm.ProviderTypeAnthropic,
		llm.ProviderTypeGemini,
		llm.ProviderTypeGroq,
		llm.ProviderTypeMistral,
		llm.ProviderTypeDeepSeek,
		llm.ProviderTypeOpenRouter,
		llm.ProviderTypeXAI,
		llm.ProviderTypeQwen,
		llm.ProviderTypeCohere,
		llm.ProviderTypeAzure,
		llm.ProviderTypeBedrock,
		llm.ProviderTypeVertexAI:
		return true
	default:
		return false
	}
}

// parseModelSpec splits a CLI model string into an optional provider prefix and
// the model name to send to the provider. It is the pure, testable core of the
// HXC-096 fix (see handleGenerate): the segment before the first ":" is stripped
// ONLY when it names a recognized provider type (isKnownProviderPrefix), so an
// Ollama native "name:tag" model id ("qwen2.5:3b", "llama3.2:1b") is kept intact
// — the prior unconditional split-on-first-colon mangled "qwen2.5:3b" into "3b"
// and Ollama's /api/chat returned a 404. When no recognized provider prefix is
// present, provider is "" and modelName is the input unchanged.
func parseModelSpec(model string) (provider, modelName string) {
	if idx := strings.Index(model, ":"); idx > 0 {
		if isKnownProviderPrefix(model[:idx]) {
			return model[:idx], model[idx+1:]
		}
	}
	return "", model
}

// handleGenerate performs LLM generation
func (c *CLI) handleGenerate(ctx context.Context, prompt, model string, maxTokens int, temperature float64, stream bool) error {
	// ANTI-BLUFF: MUST use real LLM provider, not simulation
	if c.llmProvider == nil {
		return fmt.Errorf("LLM provider not initialized - please check configuration")
	}

	// Parse model from string (format: provider:model or just model).
	//
	// HXC-096 (CONST-035 / BLUFF-001): the prior code stripped everything
	// before the FIRST ":" unconditionally. That is WRONG for Ollama, whose
	// native model tags ARE "name:tag" (e.g. "qwen2.5:3b", "llama3.2:1b").
	// The unconditional split mangled "qwen2.5:3b" into "3b", which Ollama's
	// /api/chat rejects with a 404 ("model '3b' not found") — exactly the CLI
	// 404 the web path never hit because internal/server passes req.Model
	// through verbatim. The "provider:model" form is only meant to strip a
	// LEADING provider name (e.g. "ollama:qwen2.5:3b", "anthropic:claude-...").
	// So strip the prefix ONLY when the segment before the first ":" is a
	// recognized provider type; otherwise keep the model string intact so a
	// real Ollama "name:tag" reaches the provider unmodified.
	_, modelName := parseModelSpec(model)

	// Round-41 readiness fix (CONST-035): the CLI's default model
	// is "llama-3-8b" which is a generic-Ollama name that does NOT
	// exist on Groq, OpenAI, Anthropic, Gemini, OpenRouter, or
	// most actual providers. Sending it as-is to any of those
	// returns a confusing 404. If the user accepted the default
	// (or supplied an empty value), pick the provider's first
	// reported model so the just-plug-in-API-key-and-go workflow
	// works without the user knowing the provider's exact model
	// names.
	if modelName == "" || modelName == "llama-3-8b" {
		if models := c.llmProvider.GetModels(); len(models) > 0 {
			modelName = models[0].Name
		}
	}

	fmt.Println()
	fmt.Println(tr(ctx, "cli_generating_header", map[string]any{"Model": modelName}))
	fmt.Println(tr(ctx, "cli_generating_prompt", map[string]any{"Prompt": prompt}))
	fmt.Println()

	// Get provider
	provider := c.llmProvider

	// CONST-040 §HXC-118 Phase 2/3: optional RAG (Retrieval-Augmented
	// Generation) context injection — default-OFF.
	//
	// rag.NewFromEnv resolves HELIXCODE_RAG_ENABLED (unset/false by
	// default per internal/rag/config.go) and constructs an Adapter that
	// is disabled unless the operator explicitly opts in. When disabled,
	// Adapter.Retrieve (internal/rag/adapter.go, Phase 1) returns
	// (nil, false, nil) WITHOUT calling the underlying retriever — no
	// HTTP call, no allocation beyond the inert Adapter/Embedder/store
	// structs themselves — so effectivePrompt stays byte-identical to
	// prompt and the request built below is IDENTICAL to the pre-HXC-118
	// behavior. ANTI-BLUFF (§11.4.6): when enabled, ragAdapter.Retrieve
	// performs a REAL embedding call + REAL in-memory cosine-similarity
	// search (internal/rag/vectorstore.go); a retrieval failure degrades
	// gracefully (logged, generation proceeds on the original prompt)
	// rather than aborting the user's request.
	effectivePrompt := prompt
	ragAdapter := rag.NewFromEnv(os.Getenv)
	if ragAdapter.Enabled() {
		ragDocs, ragRan, ragErr := ragAdapter.Retrieve(ctx, prompt, rag.RetrieveOptionsFromEnv(os.Getenv))
		if ragErr != nil {
			log.Printf("rag: retrieval failed, continuing without RAG context: %v", ragErr)
		} else if ragRan && len(ragDocs) > 0 {
			effectivePrompt = rag.PrependContext(prompt, ragDocs)
		}
	}

	// Create generation request with the prompt as a user message
	req := &llm.LLMRequest{
		Model:       modelName,
		MaxTokens:   maxTokens,
		Temperature: temperature,
		Stream:      stream,
		Messages: []llm.Message{
			{Role: "user", Content: effectivePrompt},
		},
	}

	// P1-F20-T06: Theme + Styler construction.
	//
	// Building the registry / theme / depth here (rather than once at process
	// start) keeps the wire-in scoped to the only call site that consumes it
	// in F20 (the non-stream LLM body print). The cost is one map allocation
	// + an opportunistic YAML stat per generate; both are negligible relative
	// to the LLM call itself, and centralising the construction would require
	// threading a Styler through CLI's struct and Run() which are out of
	// scope for this task.
	//
	// Failure mode: every fallible step degrades gracefully. YAML missing /
	// malformed -> log + use built-ins. Unknown theme name -> log + dark
	// fallback. The user always gets a working binary; the worst case is
	// that styling falls back to the dark baseline.
	themeRegistry := theme.NewThemeRegistry()
	if path := theme.DefaultThemePath(os.Getenv); path != "" {
		if err := themeRegistry.LoadFromFile(path); err != nil {
			log.Printf("theme: yaml load failed (continuing with built-ins): %v", err)
		}
	}
	themeName := theme.DetectThemeName(os.Getenv)
	selectedTheme, themeErr := themeRegistry.Get(themeName)
	if themeErr != nil {
		// Should not happen for built-in names, but be defensive: fall back
		// to dark explicitly so an exotic HELIXCODE_THEME value can never
		// produce a zero-Theme that styles to no-op silently.
		log.Printf("theme: get %q failed (%v), falling back to dark", themeName, themeErr)
		selectedTheme, _ = themeRegistry.Get(theme.ThemeDark)
		themeName = theme.ThemeDark
	}
	colorDepth := theme.DetectColorDepth(os.Getenv)

	if stream {
		// Real streaming from provider, wired through the P1-F18 Renderer
		// so that token output respects HELIXCODE_RENDER + TTY detection
		// (fancy = in-place line update, plain = line-buffered transcript).
		r, rerr := render.NewRenderer(render.FactoryOptions{})
		if rerr != nil {
			// Constructing a renderer should be infallible for default
			// options; surface the error rather than silently dropping
			// to fmt.Printf — callers need to know if config is broken.
			return fmt.Errorf("renderer init failed: %w", rerr)
		}
		defer func() { _ = r.Close() }()
		// Plain-mode protection (F20 spec §11): even though we don't style
		// the streaming path in v1, log the active theme depth AFTER the
		// adjustment so operators see the same observable depth they'd see
		// for the non-stream branch.
		effectiveDepth := adjustDepthForRenderer(r, colorDepth)
		log.Printf("theme: name=%s depth=%s", themeName, effectiveDepth)

		chunkChan := make(chan llm.LLMResponse, 100)
		errCh := make(chan error, 1)
		go func() {
			errCh <- provider.GenerateStream(ctx, req, chunkChan)
		}()

		blockID := "llm-" + modelName
		if rerr := streamToRenderer(ctx, chunkChan, r, blockID); rerr != nil {
			// Drain provider error before returning to avoid goroutine leak.
			<-errCh
			return fmt.Errorf("renderer stream failed: %w", rerr)
		}
		if perr := <-errCh; perr != nil {
			return fmt.Errorf("streaming generation failed: %w", perr)
		}
	} else {
		// Real non-streaming from provider, wired through the P1-F18
		// Renderer (T08) so the full LLM response respects HELIXCODE_RENDER
		// + TTY detection. Plain mode -> zero-ANSI/zero-CR transcript;
		// fancy mode -> hide-cursor + per-line emit. The blockID is empty
		// (one-shot) because a non-stream completion is not re-rendered.
		//
		// P1-F20-T06: decorate the response body with theme.Styler before
		// it reaches RenderTextBlock. The depth is forced to DepthOff when
		// the renderer is in plain mode so the styler emits zero ANSI bytes
		// that the plain renderer would otherwise pass through verbatim.
		r, rerr := render.NewRenderer(render.FactoryOptions{})
		if rerr != nil {
			return fmt.Errorf("renderer init failed: %w", rerr)
		}
		defer func() { _ = r.Close() }()

		effectiveDepth := adjustDepthForRenderer(r, colorDepth)
		styler := theme.NewStyler(selectedTheme, effectiveDepth)
		log.Printf("theme: name=%s depth=%s", themeName, effectiveDepth)

		resp, err := provider.Generate(ctx, req)
		if err != nil {
			return fmt.Errorf("generation failed: %w", err)
		}
		if perr := printResponseThroughRendererStyled(r, styler, resp.Content); perr != nil {
			return fmt.Errorf("renderer print failed: %w", perr)
		}
		// Round 41 readiness fix (CONST-035): modern CLI agents
		// (Claude Code, Aider, Cline) report token usage + duration
		// after each generation so users can track consumption.
		// Previously the CLI swallowed this telemetry — the user
		// got the response but no insight into cost/usage.
		if resp != nil {
			printGenerationStats(resp, contextWindowForModel(provider, modelName))
		}
	}

	fmt.Printf("\n✅ Generation completed\n")
	return nil
}

// printGenerationStats surfaces token usage + processing time per turn so
// users can see consumption like they'd see in Claude Code / Aider / Cline.
// Anti-bluff: only prints fields that the provider populated; absence-of-data
// is honestly shown rather than fabricated zero counts.
//
// contextWindow is the active model's REAL context-window size (in tokens),
// resolved from the provider's model catalogue (ModelInfo.ContextSize). When
// it is > 0 the render adds a `context: <used>/<window> (NN%)` indicator so
// the user can see how close they are to the model limit. When it is 0 (the
// provider did not report a window for the active model) the indicator is
// OMITTED entirely — never fabricated with a guessed denominator (CONST-035).
func printGenerationStats(resp *llm.LLMResponse, contextWindow int) {
	if resp == nil {
		return
	}
	in, out := resp.Usage.PromptTokens, resp.Usage.CompletionTokens
	if in == 0 && out == 0 && resp.ProcessingTime == 0 {
		// Provider didn't populate any telemetry; don't fabricate it.
		return
	}
	fmt.Printf("\n📊 %s", trc("cli_tokens_summary", map[string]any{
		"In": in, "Out": out, "Total": in + out,
	}))
	// Context-window USED-% indicator. Computed from the REAL total token
	// count (prompt+completion) against the REAL model window. Emitted only
	// when the window is known; formatContextUsage returns "" otherwise.
	if usage := formatContextUsage(in+out, contextWindow); usage != "" {
		fmt.Print(usage)
	}
	if resp.ProcessingTime > 0 {
		fmt.Printf("   time: %s", resp.ProcessingTime.Round(time.Millisecond))
		if out > 0 && resp.ProcessingTime.Seconds() > 0 {
			tps := float64(out) / resp.ProcessingTime.Seconds()
			fmt.Printf("   tps: %.1f", tps)
		}
	}
	if resp.FinishReason != "" {
		fmt.Printf("   finish: %s", resp.FinishReason)
	}
	fmt.Println()
}

// formatContextUsage renders the context-window USED-% indicator as a pure
// function so it is unit-testable in isolation.
//
//   - When window <= 0 (the model's context-window size is NOT reliably known
//     for the active model) it returns "" — the caller OMITS the indicator
//     rather than printing a fabricated percentage against a guessed window.
//     This is the honest-conditional mandated by CONST-035: no fake denominator.
//   - When window > 0 it returns "   context: <used>/<window> (NN%)" where NN
//     is the integer percentage of used/window. used is clamped to >= 0; the
//     percentage is allowed to exceed 100 (an honest signal that the reported
//     token total ran past the catalogue window) rather than being silently
//     capped, but never goes negative.
func formatContextUsage(used, window int) string {
	if window <= 0 {
		return ""
	}
	if used < 0 {
		used = 0
	}
	pct := used * 100 / window
	return fmt.Sprintf("   context: %d/%d (%d%%)", used, window, pct)
}

// contextWindowForModel resolves the REAL context-window size (in tokens) for
// the named model. It returns 0 when no real window is reachable — in which
// case the caller honestly omits the indicator (CONST-035: never substitute a
// guessed window).
//
// Resolution order, both REAL sources (no hardcoded fallback):
//  1. The provider's model catalogue (ModelInfo.ContextSize) matched by Name
//     then ID, mirroring how modelName is resolved on the request path — the
//     most precise per-model figure.
//  2. The provider's GetContextWindow() — the active model's window reported
//     by the provider — used when the catalogue has no matching entry or it
//     reports 0 for that model.
func contextWindowForModel(provider llm.Provider, modelName string) int {
	if provider == nil {
		return 0
	}
	if modelName != "" {
		for _, m := range provider.GetModels() {
			if m.Name == modelName || m.ID == modelName {
				if m.ContextSize > 0 {
					return m.ContextSize
				}
				break
			}
		}
	}
	if w := provider.GetContextWindow(); w > 0 {
		return w
	}
	return 0
}

// expandAtMentions scans `*prompt` for `@<path>` tokens and, for each
// token that resolves to a readable file on disk, appends a context
// block at the end of the prompt of the form:
//
//	<attached_files>
//	<file path="path/to/file">
//	... content ...
//	</file>
//	...
//	</attached_files>
//
// Modern-CLI-agent parity feature (Claude Code, Cursor, Aider).
// Returns the list of resolved paths so the REPL can surface them
// to the user. Tokens that don't resolve to a file stay verbatim in
// the prompt — the LLM sees them as-is. Per-file size cap is 256 KiB
// to keep prompts within typical context budgets; oversized files
// are listed with a `(skipped: too large)` note but not embedded.
//
// CONST-035 anti-bluff: this function never silently invents content
// for a missing file. Every attached path corresponds to a real file
// read at runtime.
func expandAtMentions(prompt *string) []string {
	if prompt == nil || *prompt == "" {
		return nil
	}
	const maxFileBytes = 256 * 1024
	tokens := atMentionTokens(*prompt)
	if len(tokens) == 0 {
		return nil
	}
	attached := make([]string, 0, len(tokens))
	var blocks []string
	seen := make(map[string]bool, len(tokens))
	for _, tok := range tokens {
		path := strings.TrimPrefix(tok, "@")
		if seen[path] {
			continue
		}
		seen[path] = true
		info, err := os.Stat(path)
		if err != nil || info.IsDir() {
			continue
		}
		if info.Size() > maxFileBytes {
			blocks = append(blocks, fmt.Sprintf(
				`<file path=%q size_bytes=%d>%s</file>`,
				path, info.Size(),
				trc("cli_file_skipped_too_large", nil),
			))
			attached = append(attached, path+trc("cli_file_skipped_label", nil))
			continue
		}
		body, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		blocks = append(blocks, fmt.Sprintf(
			"<file path=%q>\n%s\n</file>",
			path, string(body),
		))
		attached = append(attached, path)
	}
	if len(blocks) == 0 {
		return nil
	}
	*prompt = *prompt + "\n\n<attached_files>\n" + strings.Join(blocks, "\n") + "\n</attached_files>"
	return attached
}

// atMentionTokens extracts `@<token>` mentions from text. A token is
// a maximal run of non-whitespace characters after `@`, stripped of
// trailing punctuation (comma, period, semicolon, colon, close
// paren/bracket/brace, quote) that's commonly attached to a path in
// prose but isn't part of the path itself.
//
// Tokens like `@README.md` capture the trailing `.md`; tokens like
// `@docs/file.go.` strip the trailing dot. Tokens shorter than 2
// chars after `@` are ignored (avoids matching e.g. `@`-as-pronoun).
func atMentionTokens(text string) []string {
	var tokens []string
	for i := 0; i < len(text); i++ {
		if text[i] != '@' {
			continue
		}
		// `@` must be at start-of-text or NOT preceded by an
		// identifier-character (letter/digit/underscore/dot), so we
		// don't pick up emails like `user@host` or struct-tag forms.
		// Punctuation like `(` or `,` is fine — `(@path)` should match.
		if i > 0 {
			c := text[i-1]
			isIdent := (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') ||
				(c >= '0' && c <= '9') || c == '_' || c == '.'
			if isIdent {
				continue
			}
		}
		end := i + 1
		for end < len(text) {
			c := text[end]
			if c == ' ' || c == '\t' || c == '\n' || c == '\r' {
				break
			}
			end++
		}
		tok := text[i:end]
		// Strip trailing prose punctuation.
		tok = strings.TrimRight(tok, ".,;:)]}\"'!?")
		if len(tok) < 3 { // `@` + ≥2 chars
			continue
		}
		tokens = append(tokens, tok)
		i = end - 1
	}
	return tokens
}

// streamToRenderer pumps LLM streaming chunks from ch through the supplied
// Renderer using the Begin -> WriteToken... -> Commit token-streaming flow
// (P1-F18 spec §4.1, §11.6).
//
// Invariants enforced:
//   - Begin is called exactly once with blockID before the first WriteToken.
//   - Commit runs unconditionally on return so the trailing newline is always
//     emitted, even if the producer goroutine returns an error mid-stream.
//   - ctx cancellation aborts the loop without surfacing an error from the
//     channel-drain phase; the caller is responsible for joining the producer
//     goroutine separately.
//
// The function reads until ch is closed by the producer; it does NOT close the
// channel itself. Empty content chunks are forwarded so the renderer's
// auto-Begin / no-op fast paths get exercised in the same way the production
// path will see them under real provider implementations.
func streamToRenderer(ctx context.Context, ch <-chan llm.LLMResponse, r render.Renderer, blockID string) (retErr error) {
	if err := r.Begin(blockID); err != nil {
		return fmt.Errorf("renderer begin: %w", err)
	}
	defer func() {
		if cerr := r.Commit(); cerr != nil && retErr == nil {
			retErr = fmt.Errorf("renderer commit: %w", cerr)
		}
	}()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case chunk, ok := <-ch:
			if !ok {
				return nil
			}
			if err := r.WriteToken(chunk.Content); err != nil {
				return fmt.Errorf("renderer write: %w", err)
			}
		}
	}
}

// printResponseThroughRenderer prints a single non-stream LLM response
// through the P1-F18 Renderer (T08) so the bytes flow through plain/fancy
// resolution + zero-ANSI/zero-CR enforcement instead of bare fmt.Println.
//
// Construction is local: the non-stream branch in handleGenerate doesn't
// need a longer-lived renderer (the response is one-shot, never re-rendered),
// so we build, render, and Close on each call. blockID is empty so
// RenderTextBlock generates a synthetic one-shot ID under the hood.
//
// Empty content is a no-op (mirrors RenderTextBlock contract); since
// fmt.Println("") would have emitted a stray newline we keep the empty case
// silent here to preserve transcript hygiene.
//
// Retained as a thin wrapper for the F18-T08 smoke tests; new call sites
// should prefer printResponseThroughRendererStyled which composes the
// theme.Styler decorator (P1-F20-T06) on top of the same RenderTextBlock
// path.
func printResponseThroughRenderer(content string) error {
	r, err := render.NewRenderer(render.FactoryOptions{})
	if err != nil {
		return fmt.Errorf("renderer init failed: %w", err)
	}
	defer func() { _ = r.Close() }()
	return printResponseThroughRendererStyled(r, nil, content)
}

// adjustDepthForRenderer collapses requested to theme.DepthOff when r is in
// plain mode; otherwise returns requested unchanged.
//
// Load-bearing per F20 spec §11: "plain mode forces zero color emission
// regardless of theme setting". The plain renderer's pass-through clause
// (plain_renderer.go: tool-output passthrough lets caller-supplied ANSI
// bytes reach the writer verbatim) means we MUST prevent the Styler from
// emitting ANSI in the first place when targeting a plain renderer —
// otherwise pre-styled bytes from Stylize() would leak straight through into
// log files and pipes. Forcing the depth here rather than down-converting
// the bytes keeps the chokepoint at a single, testable function.
//
// The fancy renderer applies its own escape sequences for cursor moves and
// dirty-line redraws, but it does NOT strip caller-supplied ANSI either; for
// fancy mode we WANT the styler to colour the text, so we pass the requested
// depth through untouched.
func adjustDepthForRenderer(r render.Renderer, requested theme.ColorDepth) theme.ColorDepth {
	if r != nil && r.Mode() == render.ModePlain {
		return theme.DepthOff
	}
	return requested
}

// printResponseThroughRendererStyled prints a single non-stream LLM response
// through the supplied Renderer, optionally decorating the text with the
// supplied Styler before passing it to RenderTextBlock.
//
// Why styling lives here and NOT in streamToRenderer: the streaming hot path
// emits one token at a time and relies on a stable per-line dirty-diff in
// fancy mode; injecting a per-token open/close sequence would either
// fragment the role-styled region across ANSI clear-line boundaries or
// require buffering the entire stream before colouring it. Both options
// regress streaming UX. We therefore restrict styling to the non-stream
// branch in v1, which is the plan's documented compromise (cf. plan T06).
//
// Role choice: theme.RoleHighlight. Justification: the LLM's final answer is
// the most important content in the transcript and benefits from a visual
// pop relative to the surrounding `=== Generating … ===` headers and the
// trailing `✅ Generation completed`, both of which are emitted via bare
// fmt.Printf and remain unstyled. RoleInfo would render the LLM body the
// same colour as routine status messages and lose the contrast.
//
// nil styler short-circuits to RenderTextBlock with the raw content — used
// by callers that opt out of theming (e.g., the F18 backward-compat shim
// printResponseThroughRenderer above) and by tests.
//
// Empty content is a no-op (mirrors RenderTextBlock contract).
func printResponseThroughRendererStyled(r render.Renderer, styler *theme.Styler, content string) error {
	if styler != nil {
		content = styler.Stylize(theme.RoleHighlight, content)
	}
	return render.RenderTextBlock(r, "", content)
}

// handleNotification sends a notification
func (c *CLI) handleNotification(ctx context.Context, message, notifyType, priority string) error {
	notificationType := notification.NotificationType(notifyType)
	notificationPriority := notification.NotificationPriority(priority)

	notif := &notification.Notification{
		Title:    "CLI Notification",
		Message:  message,
		Type:     notificationType,
		Priority: notificationPriority,
		Channels: []string{"cli"}, // Default to CLI output
	}

	if err := c.notificationEngine.SendDirect(ctx, notif, []string{"cli"}); err != nil {
		return fmt.Errorf("failed to send notification: %v", err)
	}

	fmt.Printf("✅ %s\n", tr(ctx, "cli_notification_sent", map[string]any{"Message": message}))
	return nil
}

// exitCodeError carries a child process's real exit code up to main() so the
// CLI process can surface that exact code as its own exit status.
//
// ANTI-BLUFF (BLUFF-003 FIX): a `--command` invocation is a transparent proxy
// for the child process. If `helixcode --command 'exit 42'` exited 1 (because
// main() blanket-mapped every Run() error to log.Fatalf → exit 1), the child's
// real exit code would be silently lost — breaking shell scripts and CI gates
// that branch on the exit status. The real code MUST propagate to the process.
type exitCodeError struct {
	code int
	err  error
}

func (e *exitCodeError) Error() string { return e.err.Error() }
func (e *exitCodeError) Unwrap() error { return e.err }
func (e *exitCodeError) ExitCode() int { return e.code }

// handleCommand executes a command locally via os/exec.
// ANTI-BLUFF (BLUFF-003 FIX): This executes REAL commands, not simulations,
// and surfaces the child process's REAL exit code as the CLI's exit code.
func (c *CLI) handleCommand(ctx context.Context, command string) error {
	fmt.Printf("\n=== Executing Command ===\n")
	fmt.Printf("Command: %s\n\n", command)

	cmd := exec.CommandContext(ctx, "sh", "-c", command)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		// If the child ran to completion with a non-zero status, surface that
		// exact code (via *exec.ExitError → ProcessState.ExitCode()). For any
		// other failure (sh not found, killed by signal, context cancelled),
		// fall back to a generic non-zero exit so genuine errors stay non-zero.
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			code := exitErr.ProcessState.ExitCode()
			if code <= 0 {
				// Signalled/terminated abnormally — ExitCode() returns -1.
				// Map to a generic non-zero so the proxy never reports success.
				code = 1
			}
			return &exitCodeError{code: code, err: fmt.Errorf("command failed: %w", err)}
		}
		return fmt.Errorf("command failed: %w", err)
	}

	fmt.Printf("\n✅ %s\n", tr(ctx, "cli_command_completed",
		map[string]any{"ExitCode": cmd.ProcessState.ExitCode()}))
	return nil
}

// handleInteractive starts the conversational REPL.
//
// Round 41 readiness fix (CONST-035): the prior REPL only accepted 6
// hardcoded commands (workers/models/health/help/exit/quit) and used
// fmt.Scanln which reads a single whitespace-delimited token — so a
// multi-word prompt like "What is 2+2?" was silently truncated to
// "What" and dispatched as an unknown command. The REPL did NOT
// accept LLM prompts at all, which is the core feature a user expects
// from a modern CLI agent (Claude Code, Aider, Cline).
//
// Now: bufio.Scanner reads entire lines; lines that start with "/"
// are treated as slash commands (/workers, /models, /health, /help,
// /exit, /quit, /clear); plain text lines are sent to the resolved
// LLM provider and the response is printed. Multi-turn context is
// preserved across turns within a single REPL session.
func (c *CLI) handleInteractive(ctx context.Context) error {
	// Brand identity: print the lime/teal HelixCode wordmark banner at REPL
	// startup. brandBanner() is NO_COLOR / non-TTY aware (no escapes emitted
	// when color is disabled), so piped/redirected sessions stay clean.
	fmt.Print(brandBanner())
	fmt.Println(brandHeading(tr(ctx, "cli_repl_header", nil)))
	fmt.Println(brandInfo(tr(ctx, "cli_repl_intro", nil)))
	if c.llmProvider != nil {
		if models := c.llmProvider.GetModels(); len(models) > 0 {
			fmt.Println(tr(ctx, "cli_provider_default_model", map[string]any{
				"Provider": c.llmProvider.GetName(),
				"Model":    models[0].Name,
			}))
		}
	}

	// Set up signal handling for graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(sigChan)

	scanner := bufio.NewScanner(os.Stdin)
	// Allow long prompts (default 64 KB is too small for big code snippets).
	scanner.Buffer(make([]byte, 0, 1<<20), 16<<20)

	// Multi-turn conversation state (in-session only — F11 session
	// persistence is via the sessions subcommand).
	var conversation []llm.Message

	for {
		select {
		case <-sigChan:
			fmt.Println()
			fmt.Println()
			fmt.Println(tr(ctx, "cli_repl_shutting_down", nil))
			return nil
		default:
		}

		fmt.Print("\n" + brandPrompt("helix>") + " ")
		if !scanner.Scan() {
			if err := scanner.Err(); err != nil {
				return fmt.Errorf("REPL read error: %w", err)
			}
			// EOF — clean exit
			fmt.Println(tr(ctx, "cli_repl_goodbye", nil))
			return nil
		}

		input := strings.TrimSpace(scanner.Text())
		if input == "" {
			continue
		}

		// Slash commands (extended with /clear, /reset). Backward-
		// compat: bare 'exit'/'quit'/'help'/'workers'/'models'/'health'
		// still recognised since the old REPL used those.
		lower := strings.ToLower(input)
		switch lower {
		case "/exit", "/quit", "exit", "quit":
			fmt.Println(tr(ctx, "cli_repl_goodbye", nil))
			return nil
		case "/help", "help":
			c.showHelp(ctx)
			continue
		case "/workers", "workers":
			_ = c.handleListWorkers(ctx)
			continue
		case "/models", "models":
			_ = c.handleListModels(ctx)
			continue
		case "/health", "health":
			_ = c.handleHealthCheck(ctx)
			continue
		case "/clear", "/reset":
			conversation = conversation[:0]
			fmt.Println("(conversation history cleared)")
			continue
		case "/undo", "undo":
			_ = c.handleUndo(ctx)
			continue
		}
		// /diff [ref] — takes an optional ref argument, so it is matched by
		// prefix (the bare-case switch above matches the whole line). Both
		// "/diff" and the legacy bare "diff" are recognised, mirroring the
		// other dual-form commands.
		if lower == "/diff" || lower == "diff" ||
			strings.HasPrefix(lower, "/diff ") || strings.HasPrefix(lower, "diff ") {
			fields := strings.Fields(input)
			ref := ""
			if len(fields) > 1 {
				ref = fields[1]
			}
			_ = c.handleDiff(ctx, ref)
			continue
		}
		// /debate <prompt> — runs the prompt through the DebateOrchestrator
		// 8-phase MASTER protocol wired to the CLI's REAL llm provider via
		// speckit_debate_adapter.NewLLMBackedResponder. Prefix-matched (takes
		// the remainder of the line as the debate topic), mirroring /diff.
		if strings.HasPrefix(lower, "/debate ") || lower == "/debate" {
			topic := strings.TrimSpace(strings.TrimPrefix(input, "/debate"))
			_ = c.handleDebate(ctx, topic)
			continue
		}
		// /specify <request> — runs the HelixSpecifier speckit Specify phase,
		// backed by the REAL debate responder over the REAL llm provider.
		// Prefix-matched (remainder of the line is the spec request), mirroring
		// /debate.
		if strings.HasPrefix(lower, "/specify ") || lower == "/specify" {
			request := strings.TrimSpace(strings.TrimPrefix(input, "/specify"))
			_ = c.handleSpecify(ctx, request)
			continue
		}
		// /checkpoint [create [label] | list | restore <id>] — F12 workspace
		// checkpoints: snapshot the working-tree file bytes now and restore
		// them later. Prefix-matched (takes subcommand + args), mirroring the
		// other multi-arg commands. Bare "/checkpoint" prints usage.
		if lower == "/checkpoint" || strings.HasPrefix(lower, "/checkpoint ") {
			args := strings.TrimSpace(strings.TrimPrefix(input, "/checkpoint"))
			_ = c.handleCheckpoint(ctx, args)
			continue
		}
		// Unknown slash command: surface clearly, don't send to LLM
		if strings.HasPrefix(input, "/") {
			fmt.Println(tr(ctx, "cli_repl_unknown_slash", map[string]any{"Input": input}))
			continue
		}

		// Plain text: send as LLM prompt (the core REPL contract).
		if c.llmProvider == nil {
			fmt.Println(tr(ctx, "cli_repl_no_provider", nil))
			continue
		}

		// @-file mentions (modern-CLI-agent parity with Claude Code,
		// Cursor, Aider). The user types `@path/to/file` anywhere in
		// the prompt; we resolve each token to a file on disk and
		// attach its content as a context block at the end of the
		// prompt. Tokens that don't resolve to a file stay verbatim
		// (no scary error — the LLM sees them as-is).
		promptToSend := input

		// F10 / HXC-162: agent-invoked Skills. A plain-language line that
		// matches a registered skill's trigger is rewritten into that skill's
		// rendered body (with named captures substituted) before it becomes the
		// LLM prompt. No match leaves the input untouched, so this is a no-op
		// for ordinary prompts. Mirrors the terminal-UI front-end.
		if c.skillDispatcher != nil {
			if rendered, matched := agent.DispatchSkill(c.skillDispatcher, promptToSend); matched {
				promptToSend = rendered
				fmt.Println(tr(ctx, "cli_repl_skill_applied", nil))
			}
		}

		if attached := expandAtMentions(&promptToSend); len(attached) > 0 {
			for _, p := range attached {
				fmt.Printf("  📎 attached: %s\n", p)
			}
		}

		conversation = append(conversation, llm.Message{Role: "user", Content: promptToSend})

		modelName := ""
		if models := c.llmProvider.GetModels(); len(models) > 0 {
			modelName = models[0].Name
		}
		req := &llm.LLMRequest{
			Model:       modelName,
			MaxTokens:   1000,
			Temperature: 0.7,
			// P1-T07 (speed programme): the REPL consumes the streaming
			// provider API so tokens reach the terminal as they arrive
			// (time-to-first-visible-token is the major perceived-speed
			// lever). Stream is set true so providers that branch on the
			// flag take their streaming code path.
			Stream:   true,
			Messages: conversation,
		}
		assembled, stats, err := streamREPLTurn(ctx, c.llmProvider, req)
		if err != nil {
			fmt.Printf("Error: %v\n", err)
			// Pop the user turn so a retry doesn't accumulate bad state.
			conversation = conversation[:len(conversation)-1]
			continue
		}
		if assembled != "" {
			conversation = append(conversation, llm.Message{Role: "assistant", Content: assembled})
		}
		if stats != nil {
			printGenerationStats(stats, contextWindowForModel(c.llmProvider, modelName))
		}
	}
}

// streamREPLTurn drives one interactive-REPL turn over the provider's
// streaming API (P1-T07, speed programme Phase 1).
//
// It prints each chunk's Content to stdout the instant it arrives — so the
// first token is visible long before the completion finishes — while
// accumulating the full text so the caller can append the assistant turn to
// the conversation. The final chunk that carries a populated Usage block is
// returned as `stats` so the caller can print token/usage telemetry exactly
// as the buffered path did.
//
// Channel-close robustness: the llm.Provider streaming contract is NOT
// uniform — Anthropic/OpenAI/Groq/DeepSeek `defer close(ch)` from inside
// GenerateStream, but Ollama and the OpenAI-compatible provider return
// WITHOUT closing the channel. A naive `for range chunkChan` therefore
// deadlocks against the non-closing providers. We instead select on BOTH the
// chunk channel AND the provider's return signal: once GenerateStream has
// returned we drain whatever is still buffered, then stop — correct for both
// the closing and the non-closing provider families.
//
// No-regression guarantee: the assembled string is the concatenation of every
// chunk's Content, which is byte-identical to the buffered `Generate` result
// for any conformant provider (each provider's GenerateStream emits the same
// total content as Generate — verified by the per-provider GenerateStream
// tests). Only WHEN the bytes appear changes, not WHAT appears.
func streamREPLTurn(ctx context.Context, provider llm.Provider, req *llm.LLMRequest) (assembled string, stats *llm.LLMResponse, err error) {
	chunkChan := make(chan llm.LLMResponse, 100)
	errCh := make(chan error, 1)
	go func() {
		errCh <- provider.GenerateStream(ctx, req, chunkChan)
	}()

	var sb strings.Builder
	var last llm.LLMResponse
	haveStats := false
	render := func(chunk llm.LLMResponse) {
		if chunk.Content != "" {
			fmt.Print(chunk.Content)
			sb.WriteString(chunk.Content)
		}
		// Capture the most recent chunk that carried usage/timing telemetry
		// so the post-turn stats line matches the buffered path.
		if chunk.Usage.TotalTokens > 0 || chunk.Usage.PromptTokens > 0 ||
			chunk.Usage.CompletionTokens > 0 || chunk.ProcessingTime > 0 {
			last = chunk
			haveStats = true
		}
	}

	streamErr := drainProviderStream(chunkChan, errCh, render)
	if streamErr != nil {
		return "", nil, streamErr
	}
	// Terminate the streamed line so the stats line / next prompt start clean.
	if sb.Len() > 0 {
		fmt.Println()
	}
	if haveStats {
		stats = &last
	}
	return sb.String(), stats, nil
}

// drainProviderStream consumes every chunk a provider's GenerateStream emits
// onto chunkChan, invoking onChunk for each, and returns the provider's error.
//
// It is the single chokepoint that copes with the non-uniform channel-close
// contract across llm.Provider implementations (see streamREPLTurn doc). The
// algorithm:
//   - Phase 1: select on chunkChan AND errCh. A chunk -> render it. A close of
//     chunkChan (ok == false) -> the channel-closing providers finished;
//     join errCh and return. An errCh send -> the non-closing providers
//     finished; move to phase 2.
//   - Phase 2: non-blocking drain of whatever is still buffered in chunkChan
//     (the producer goroutine has already returned, so no more will arrive),
//     then return the provider error.
//
// This terminates for BOTH provider families and never deadlocks.
func drainProviderStream(chunkChan chan llm.LLMResponse, errCh chan error, onChunk func(llm.LLMResponse)) error {
	for {
		select {
		case chunk, ok := <-chunkChan:
			if !ok {
				// Channel-closing provider: drain done, join the error.
				return <-errCh
			}
			onChunk(chunk)
		case provErr := <-errCh:
			// Non-closing provider (Ollama / OpenAI-compatible): the producer
			// has returned. Drain any chunks it left buffered, then stop.
			for {
				select {
				case chunk, ok := <-chunkChan:
					if !ok {
						return provErr
					}
					onChunk(chunk)
				default:
					return provErr
				}
			}
		}
	}
}

// handleCheckpoint implements the /checkpoint REPL command (F12 workspace
// checkpoints). It snapshots/restores the REAL working-tree file bytes via the
// internal/checkpoint Manager (git plumbing when in a repo, real file-copy
// otherwise) rooted at the CLI's current working directory.
//
// Subcommands:
//
//	/checkpoint create [label]   snapshot the working tree now
//	/checkpoint list             list checkpoints (newest first)
//	/checkpoint restore <id>     restore the working tree to a snapshot
//
// Anti-bluff (§11.4 / CONST-035): every path drives the real Manager against
// the real filesystem — there is no fake/printed-only output. Restore
// writes real bytes back to disk (covered by internal/checkpoint round-trip
// tests).
func (c *CLI) handleCheckpoint(ctx context.Context, args string) error {
	_ = ctx
	cwd, err := os.Getwd()
	if err != nil {
		fmt.Printf("❌ /checkpoint: cannot determine working directory: %v\n", err)
		return err
	}
	mgr, err := checkpoint.NewManager(cwd)
	if err != nil {
		fmt.Printf("❌ /checkpoint: %v\n", err)
		return err
	}

	fields := strings.Fields(args)
	sub := "list"
	if len(fields) > 0 {
		sub = strings.ToLower(fields[0])
	}

	switch sub {
	case "create":
		label := strings.TrimSpace(strings.TrimPrefix(args, fields[0]))
		id, err := mgr.Create(label)
		if err != nil {
			fmt.Printf("❌ /checkpoint create failed: %v\n", err)
			return err
		}
		fmt.Printf("✅ checkpoint created: %s (backend: %s)\n", id, mgr.Backend())
		if label != "" {
			fmt.Printf("   label: %s\n", label)
		}
		return nil

	case "list":
		cps := mgr.List()
		if len(cps) == 0 {
			fmt.Println("(no checkpoints)")
			return nil
		}
		fmt.Printf("Checkpoints (%s backend, newest first):\n", mgr.Backend())
		for _, cp := range cps {
			label := cp.Label
			if label == "" {
				label = "(no label)"
			}
			fmt.Printf("  %s  %s  %s\n", cp.ID, cp.CreatedAt.Format("2006-01-02 15:04:05"), label)
		}
		return nil

	case "restore":
		if len(fields) < 2 {
			fmt.Println("usage: /checkpoint restore <id>")
			return nil
		}
		id := fields[1]
		report, err := mgr.RestoreReported(id)
		if err != nil {
			fmt.Printf("❌ /checkpoint restore failed: %v\n", err)
			return err
		}
		// Anti-bluff (§11.4 / CONST-035): the git backend snapshots ONLY
		// git-tracked content, so untracked working-tree files are NOT restored.
		// Report the truth instead of an unconditional success — never claim the
		// whole working tree was restored when untracked files were left as-is.
		if report.FullyRestored() {
			fmt.Printf("✅ working tree restored to checkpoint %s\n", id)
		} else {
			fmt.Printf("⚠️  tracked files restored to checkpoint %s, but %d untracked file(s) were NOT restored (the %s checkpoint only snapshots git-tracked content):\n",
				id, len(report.UntrackedNotRestored), report.Backend)
			for _, f := range report.UntrackedNotRestored {
				fmt.Printf("     - %s\n", f)
			}
		}
		return nil

	default:
		fmt.Println("usage: /checkpoint create [label] | list | restore <id>")
		return nil
	}
}

// replGit constructs an *autocommit.Git rooted at the process working
// directory — the same construction the F22 auto-committer uses at bootstrap
// (NewAutoCommitter with WorkingDir: os.Getwd()). It returns an error if the
// working directory cannot be determined or is not inside a git work tree, so
// the /diff and /undo REPL handlers refuse cleanly instead of shelling out
// into a non-repo. The Git handle is cheap (two fields) and stateless across
// reads, so building it per-command keeps the CLI struct unchanged.
func (c *CLI) replGit(ctx context.Context) (*autocommit.Git, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return nil, fmt.Errorf("cannot determine working directory: %w", err)
	}
	g := autocommit.NewGit(cwd, zap.NewNop())
	isRepo, err := g.IsRepo(ctx)
	if err != nil {
		return nil, err
	}
	if !isRepo {
		return nil, fmt.Errorf("%s is not a git repository", cwd)
	}
	return g, nil
}

// handleDiff prints the real working-tree diff. With no argument it shows the
// unstaged diff (`git diff` — "what changed in my working tree"); with a ref
// argument it shows `git diff <ref>` (everything since that commit/tag/ref).
// Output is verbatim git output — no placeholder text.
func (c *CLI) handleDiff(ctx context.Context, ref string) error {
	g, err := c.replGit(ctx)
	if err != nil {
		fmt.Printf("❌ /diff: %v\n", err)
		return err
	}
	ref = strings.TrimSpace(ref)
	var out string
	if ref == "" {
		out, err = g.DiffUnstaged(ctx)
	} else {
		out, err = g.DiffSinceRef(ctx, ref)
	}
	if err != nil {
		fmt.Printf("❌ /diff failed: %v\n", err)
		return err
	}
	if strings.TrimSpace(out) == "" {
		fmt.Println("(no changes)")
		return nil
	}
	fmt.Print(out)
	if !strings.HasSuffix(out, "\n") {
		fmt.Println()
	}
	return nil
}

// handleDebate runs the given topic through DebateOrchestrator's 8-phase
// MASTER protocol, wired to the CLI's REAL llm provider. This is the
// production call-site for speckit_debate_adapter.NewLLMBackedResponder:
// the orchestrator's ProviderInvoker is supplied by wrapping the CLI's
// existing *llm.Provider.Generate (the same provider the REPL uses for plain
// prompts) into the (ctx, prompt) (string, error) shape the adapter requires.
//
// Anti-bluff (§11.4 / CONST-035): the invoker makes a REAL provider.Generate
// call — no fabricated/synthesised output. When no provider is configured the
// command refuses cleanly rather than fabricating a debate. The adapter
// itself refuses (ErrSpeckitDebateInvokerNotProvided) if a nil invoker were
// ever passed, and surfaces orchestrator errors verbatim.
func (c *CLI) handleDebate(ctx context.Context, topic string) error {
	topic = strings.TrimSpace(topic)
	if topic == "" {
		fmt.Println("usage: /debate <topic>")
		return nil
	}
	if c.llmProvider == nil {
		fmt.Println(tr(ctx, "cli_repl_no_provider", nil))
		return nil
	}

	// Resolve the provider's first advertised model. The orchestrator's
	// RegisterProvider requires a non-empty model name, and the provider's
	// Generate needs one too, so a provider with zero models cannot drive a
	// debate — refuse cleanly rather than hand the adapter an empty spec
	// that RegisterProvider would reject (§11.4.6 no-guessing).
	provider := c.llmProvider
	modelName := ""
	if models := provider.GetModels(); len(models) > 0 {
		modelName = models[0].Name
	}
	if strings.TrimSpace(modelName) == "" {
		fmt.Println("❌ /debate: active provider advertises no models; cannot run a debate")
		return nil
	}

	// Wrap the REAL CLI provider into the adapter's ProviderInvoker shape.
	// provider.Generate is (ctx, *LLMRequest) (*LLMResponse, error); the
	// orchestrator wants (ctx, prompt) (string, error). This closure is the
	// honest seam: every debate agent turn round-trips through a real LLM
	// call against c.llmProvider.
	invoker := func(ictx context.Context, prompt string) (string, error) {
		resp, err := provider.Generate(ictx, &llm.LLMRequest{
			Model:       modelName,
			MaxTokens:   1000,
			Temperature: 0.7,
			Messages:    []llm.Message{{Role: "user", Content: prompt}},
		})
		if err != nil {
			return "", err
		}
		if resp == nil {
			return "", fmt.Errorf("provider returned nil response")
		}
		return resp.Content, nil
	}

	responder, err := speckit_debate_adapter.NewLLMBackedResponder(
		invoker,
		// Two agents (same provider/model, distinct scores) — the debate orchestrator
		// requires >=2 participants (MinAgentsPerDebate); a single agent fails at
		// runtime with "insufficient agents (have 1, need 2)" (HXC-080).
		[]speckit_debate_adapter.AgentSpec{
			{Provider: provider.GetName(), Model: modelName, Score: 0.9},
			{Provider: provider.GetName(), Model: modelName, Score: 0.85},
		},
	)
	if err != nil {
		fmt.Printf("❌ /debate setup failed: %v\n", err)
		return err
	}

	out, err := responder.Generate(ctx, topic)
	if err != nil {
		fmt.Printf("❌ /debate failed: %v\n", err)
		return err
	}
	fmt.Print(out)
	if !strings.HasSuffix(out, "\n") {
		fmt.Println()
	}
	return nil
}

// handleSpecify runs HelixSpecifier's speckit Specify phase against the CLI's
// REAL llm provider. It mirrors handleDebate's provider→responder wiring, then
// drives the real speckit engine:
//
//	pillar := speckit.NewPillar(config.DefaultConfig(), logger)
//	pillar.SetDebateFunc(speckit.LLMBackedDebateFunc(responder))
//	result, err := pillar.ExecutePhase(ctx, types.PhaseSpecify, &types.PhaseInput{...})
//
// Anti-bluff (§11.4 / CONST-035): the responder round-trips every phase debate
// turn through a REAL provider.Generate call — no fabricated output. The speckit
// engine itself REQUIRES a real DebateFunc: a nil DebateFunc returns
// speckit.ErrDebateFuncNotConfigured (the round-28 §11.4 audit removed the prior
// fabricating branch). When no provider/model is configured, OR the engine/debate
// returns an error, the command surfaces the REAL error rather than fabricating
// any phase output.
func (c *CLI) handleSpecify(ctx context.Context, request string) error {
	request = strings.TrimSpace(request)
	if request == "" {
		fmt.Println("usage: /specify <request>")
		return nil
	}
	if c.llmProvider == nil {
		fmt.Println(tr(ctx, "cli_repl_no_provider", nil))
		return nil
	}

	// Resolve the provider's first advertised model (same guard as /debate):
	// the responder's RegisterProvider + the provider's Generate both require a
	// non-empty model name (§11.4.6 no-guessing).
	provider := c.llmProvider
	modelName := ""
	if models := provider.GetModels(); len(models) > 0 {
		modelName = models[0].Name
	}
	if strings.TrimSpace(modelName) == "" {
		fmt.Println("❌ /specify: active provider advertises no models; cannot run the specify phase")
		return nil
	}

	// Wrap the REAL CLI provider into the adapter's ProviderInvoker shape —
	// identical honest seam to handleDebate.
	invoker := func(ictx context.Context, prompt string) (string, error) {
		resp, err := provider.Generate(ictx, &llm.LLMRequest{
			Model:       modelName,
			MaxTokens:   1000,
			Temperature: 0.7,
			Messages:    []llm.Message{{Role: "user", Content: prompt}},
		})
		if err != nil {
			return "", err
		}
		if resp == nil {
			return "", fmt.Errorf("provider returned nil response")
		}
		return resp.Content, nil
	}

	responder, err := speckit_debate_adapter.NewLLMBackedResponder(
		invoker,
		// Two agents (same provider/model, distinct scores) — the debate orchestrator
		// requires >=2 participants (MinAgentsPerDebate); a single agent fails at
		// runtime with "insufficient agents (have 1, need 2)" (HXC-080).
		[]speckit_debate_adapter.AgentSpec{
			{Provider: provider.GetName(), Model: modelName, Score: 0.9},
			{Provider: provider.GetName(), Model: modelName, Score: 0.85},
		},
	)
	if err != nil {
		fmt.Printf("❌ /specify setup failed: %v\n", err)
		return err
	}

	// Build the real speckit pillar and wire the REAL debate responder into it
	// via the canonical SetDebateFunc(LLMBackedDebateFunc(responder)) path. A nil
	// DebateFunc would make ExecutePhase return ErrDebateFuncNotConfigured.
	pillar := speckit.NewPillar(speckitconfig.DefaultConfig(), logrus.New())
	pillar.SetDebateFunc(speckit.LLMBackedDebateFunc(responder))

	result, err := pillar.ExecutePhase(ctx, speckittypes.PhaseSpecify, &speckittypes.PhaseInput{
		UserRequest: request,
	})
	if err != nil {
		// Surfaces the REAL error verbatim — including
		// speckit.ErrDebateFuncNotConfigured and any debate/provider failure.
		// Never a fabricated phase output (§11.4 / CONST-035).
		fmt.Printf("❌ /specify failed: %v\n", err)
		return err
	}
	if result == nil {
		err := fmt.Errorf("speckit ExecutePhase returned nil result")
		fmt.Printf("❌ /specify failed: %v\n", err)
		return err
	}

	fmt.Printf("# Specify phase (quality score %.3f, debate %s)\n",
		result.QualityScore, result.DebateID)
	fmt.Print(result.Output)
	if !strings.HasSuffix(result.Output, "\n") {
		fmt.Println()
	}
	return nil
}

// handleUndo reverts the last commit via autocommit.Git.RevertLastCommit
// (`git revert --no-edit HEAD` — a NEW commit that inverts HEAD, never a
// history rewrite, so it is force-push-free per §11.4.113) and prints the
// resulting HEAD SHA. Real git result only.
func (c *CLI) handleUndo(ctx context.Context) error {
	g, err := c.replGit(ctx)
	if err != nil {
		fmt.Printf("❌ /undo: %v\n", err)
		return err
	}
	head, err := g.RevertLastCommit(ctx)
	if err != nil {
		fmt.Printf("❌ /undo failed: %v\n", err)
		return err
	}
	fmt.Printf("✅ reverted last commit; HEAD is now %s\n", head)
	return nil
}

// showHelp displays available commands.
//
// Round-202 §11.4 (CONST-046 Phase 4 round 85, 2026-05-19): the seven
// highest-impact help-screen lines (section headers + the five `slash`
// command entries) are routed through tr() so non-English users see a
// locale-appropriate help screen. The eleven Command-Line-Options lines
// remain literal in this round because they refer to verbatim flag
// names (--list-workers, --user, etc.) which MUST stay machine-stable
// across locales — they are command-line tokens, not human-readable
// content, and translating them would break shell scripts that parse
// the help screen. A future round MAY migrate the trailing description
// half of each option line while keeping the flag-name half literal.
func (c *CLI) showHelp(ctx context.Context) {
	fmt.Println()
	fmt.Println(tr(ctx, "cli_help_commands_header", nil))
	fmt.Println(tr(ctx, "cli_help_cmd_workers", nil))
	fmt.Println(tr(ctx, "cli_help_cmd_models", nil))
	fmt.Println(tr(ctx, "cli_help_cmd_health", nil))
	// /diff and /undo are git command tokens (machine-stable, like the
	// flag-name help lines below): plain literals rather than tr() keys so
	// they stay consistent across locales and don't require new i18n keys.
	fmt.Println("/diff [ref]      - Show working-tree git diff (optionally since <ref>)")
	fmt.Println("/undo            - Revert the last commit (git revert HEAD)")
	fmt.Println("/debate <topic>  - Run a real LLM-backed debate on <topic>")
	fmt.Println("/specify <req>   - Run the speckit Specify phase (real LLM-backed) for <req>")
	fmt.Println("/checkpoint ...  - Snapshot/restore working-tree files (create [label] | list | restore <id>)")
	fmt.Println(tr(ctx, "cli_help_cmd_help", nil))
	fmt.Println(tr(ctx, "cli_help_cmd_exit", nil))
	fmt.Println("")
	fmt.Println(tr(ctx, "cli_help_options_header", nil))
	fmt.Println("--list-workers   - List all workers")
	fmt.Println("--list-models    - List available models")
	fmt.Println("--health         - Perform health check")
	fmt.Println("--worker         - Add a worker (requires --user)")
	fmt.Println("--user           - Worker SSH username")
	fmt.Println("--key            - Worker SSH key path")
	fmt.Println("--prompt         - Generate with LLM")
	fmt.Println("--model          - LLM model to use")
	fmt.Println("--stream         - Stream the response")
	fmt.Println("--notify         - Send notification")
	fmt.Println("--notify-type    - Notification type (info/warning/error/success/alert)")
	fmt.Println("--notify-priority - Notification priority (low/medium/high/urgent)")
}

// loadAPIKeysAtStartup wires the previously-dead secrets.LoadAPIKeys into the
// CLI bootstrap (D-3 / SP1). It recognizes provider API keys from
// $HOME/api_keys.sh or a walked-up .env and applies them to the process env via
// gap-fill precedence (already-exported vars win — DECISION-1). A missing source
// is non-fatal: returning false means no recognition source was found, which is
// a legitimate state (the operator may have exported keys directly). The error
// is intentionally swallowed (never logged — CONST-042) but the boolean is
// returned so tests can assert the wiring is live (anti-bluff: this is the seam
// that proves the loader is no longer dead code).
func loadAPIKeysAtStartup() bool {
	return secrets.LoadAPIKeys() == nil
}

func main() {
	// P1-F15-T08: subagent helper-mode dispatch — MUST run BEFORE the F14
	// sandbox helper-mode dispatch (per spec §3 anchor). A subagent helper
	// child does NOT initialise the sandbox; the parent re-exec'd us with
	// HELIXCODE_SUBAGENT_HELPER=1 and a JSON-encoded SubagentTask in
	// HELIXCODE_SUBAGENT_HELPER_PAYLOAD. We must decode, run an
	// InProcessSpawner with the child's own LLM provider, write a
	// SubagentResult JSON to stdout, and exit — without touching the sandbox
	// or the rest of the CLI bootstrap.
	//
	// Anti-bluff anchor: this MUST be the very first statement in main().
	// Reordering it after the sandbox dispatch (or any flag parsing) would
	// break the protocol round-trip the parent depends on.
	if subagent.IsSubagentInvocation() {
		os.Exit(subagent.RunAsSubagent(buildSubagentLLMProvider))
	}

	// F14: native sandbox helper re-exec dispatch.
	//
	// The native backend launches the host binary with HELIX_SANDBOX_NATIVE_HELPER
	// set; that invocation must run the helper (mount /proc, apply rlimits,
	// chdir, exec /bin/sh -c <command>) and exit. It must NOT continue into
	// normal CLI logic — the helper child is inside fresh PID/MNT/USER/UTS/IPC
	// namespaces and re-entering the CLI bootstrap would hang or crash.
	//
	// Anti-bluff anchor: this dispatch MUST be the SECOND statement in
	// main() (the first is subagent.IsSubagentInvocation above) so no flag
	// parsing, cobra subcommand interception, or env-driven init runs before
	// the helper takes over. On non-Linux this is a no-op (IsHelperInvocation
	// returns false there — see native_backend_other.go).
	if sandbox.IsHelperInvocation() {
		os.Exit(sandbox.RunAsHelper())
	}

	// D-3 (SP1): recognize provider API keys from $HOME/api_keys.sh or a
	// walked-up .env BEFORE any config / env read, so a key supplied only via
	// those files becomes visible to config.Load() (viper AutomaticEnv) and the
	// working-model funnel. Gap-fill precedence (DECISION-1): an already-exported
	// shell var is NEVER overwritten — the file only fills gaps. A missing source
	// is non-fatal (the operator may export keys directly). Values are never
	// logged (CONST-042). This runs after the helper-mode early-exit dispatches
	// (which never reach normal CLI logic) and before every config-reading path.
	loadAPIKeysAtStartup()

	// Wire the process-wide cloud gate (internal/llm.SetCloudEnabled) from
	// config.LLM.Cloud.Enabled now that loadAPIKeysAtStartup has populated the
	// process environment — config.Get() is memoized (sync.Once) so every
	// later config.Get() call in this process observes the same value the
	// key funnel just set. A config load error leaves the gate at its
	// fail-closed zero value (CLOSED): hosted providers stay refused rather
	// than silently permitted.
	if cliCfg, err := config.Get(); err == nil {
		llm.SetCloudEnabled(cliCfg.LLM.Cloud.Enabled)
	}

	// Minimal dispatcher: intercept the "permissions" subcommand group before
	// flag.Parse() so that Cobra handles its own flag parsing.
	if len(os.Args) > 1 && os.Args[1] == "permissions" {
		cmd := newPermissionsCommand()
		cmd.SetArgs(os.Args[2:])
		if err := cmd.Execute(); err != nil {
			log.Fatalf("Error: %v", err)
		}
		return
	}

	// Dispatcher: intercept the "worktree" subcommand group before flag.Parse()
	// so that Cobra handles its own flag parsing (same pattern as "permissions").
	if len(os.Args) >= 2 && os.Args[1] == "worktree" {
		cwd, _ := os.Getwd()
		m := worktree.NewManager(cwd)
		cmd := newWorktreeCommand(m)
		cmd.SetArgs(os.Args[2:])
		if err := cmd.Execute(); err != nil {
			os.Exit(1)
		}
		return
	}

	// Dispatcher: intercept the "hooks" subcommand group before flag.Parse()
	// so that Cobra handles its own flag parsing (same pattern as "permissions" / "worktree").
	if len(os.Args) >= 2 && os.Args[1] == "hooks" {
		cmd := newHooksCommand()
		cmd.SetArgs(os.Args[2:])
		if err := cmd.Execute(); err != nil {
			os.Exit(1)
		}
		return
	}

	// Dispatcher: intercept the "mcp" subcommand group before flag.Parse()
	// so that Cobra handles its own flag parsing (same pattern as "hooks").
	if len(os.Args) >= 2 && os.Args[1] == "mcp" {
		cmd := newMCPCommand(MCPCommandDeps{ConfigPath: ".helixcode/mcp.yml"})
		cmd.SetArgs(os.Args[2:])
		if err := cmd.Execute(); err != nil {
			os.Exit(1)
		}
		return
	}

	// Dispatcher: intercept the "acp" subcommand before flag.Parse() so that
	// Cobra handles its own flag parsing (same pattern as "mcp"). This is an
	// opt-in surface (HXC-119 Phase 1-3 scaffold, see internal/acp/doc.go):
	// it only runs when explicitly invoked as `helixcode acp` and does not
	// alter any other CLI invocation's behavior. Wires real os.Stdin/
	// os.Stdout — the stdio subprocess transport an ACP-aware editor
	// (Zed, JetBrains) launches HelixCode with.
	if len(os.Args) >= 2 && os.Args[1] == "acp" {
		cmd := newACPCommand(ACPCommandDeps{In: os.Stdin, Out: os.Stdout})
		cmd.SetArgs(os.Args[2:])
		if err := cmd.Execute(); err != nil {
			os.Exit(1)
		}
		return
	}

	// Dispatcher: intercept the "commands" subcommand group before flag.Parse()
	// so that Cobra handles its own flag parsing (same pattern as "mcp").
	// The loader and registry are constructed here with zero startup cost
	// (Load() is a no-op when dirs are absent); the full wiring runs later
	// inside cli.Run() for other code paths.
	if len(os.Args) >= 2 && os.Args[1] == "commands" {
		projectCmds := filepath.Join(".", ".helix", "commands")
		var userCmds string
		if userCfg, err := os.UserConfigDir(); err == nil {
			userCmds = filepath.Join(userCfg, "helixcode", "commands")
		}
		cmdReg := commands.NewRegistry()
		mdLdr := commands.NewMarkdownLoader(cmdReg, projectCmds, userCmds)
		if err := mdLdr.Load(); err != nil {
			log.Printf("commands dispatcher: load failed: %v", err)
		}
		cmd := newCommandsCmd(commandsCmdDeps{Loader: mdLdr, Registry: cmdReg})
		cmd.SetArgs(os.Args[2:])
		if err := cmd.Execute(); err != nil {
			os.Exit(1)
		}
		return
	}

	// Dispatcher: intercept the "sessions" subcommand group before flag.Parse()
	// so that Cobra handles its own flag parsing (same pattern as "commands").
	// The TranscriptStore is constructed here directly off the F11 base dir so
	// `helixcode sessions list` works without a full CLI bootstrap.
	if len(os.Args) >= 2 && os.Args[1] == "sessions" {
		store := session.NewTranscriptStore(sessionStoreBaseDir())
		currentProject, _ := session.ComputeProjectIdentity()
		cmd := newSessionsCmd(sessionsCmdDeps{Store: store, CurrentProject: currentProject})
		cmd.SetArgs(os.Args[2:])
		if err := cmd.Execute(); err != nil {
			os.Exit(1)
		}
		return
	}

	// Dispatcher: intercept the "wizard" subcommand group before flag.Parse()
	// so Cobra handles its own flag parsing (same pattern as "sessions").
	// The wizard runs RunWizard (interactive tview) or, when --provider is
	// supplied, builds a NonInteractiveResult and writes the YAML directly.
	if len(os.Args) >= 2 && os.Args[1] == "wizard" {
		cmd := newWizardCmd(wizardCmdDeps{})
		cmd.SetArgs(os.Args[2:])
		if err := cmd.Execute(); err != nil {
			os.Exit(1)
		}
		return
	}

	// Dispatcher: intercept the "lsp" subcommand group before flag.Parse() so
	// Cobra handles its own flag parsing (same pattern as "sessions"/"wizard").
	// The LSPManager is constructed here directly off the curated allowlist
	// filtered by exec.LookPath, so `helixcode lsp list-servers` works without
	// the full CLI bootstrap. status/restart/stop also work but only over the
	// servers visible to this short-lived manager — for a long-running session
	// the in-process /lsp slash command is the right surface.
	if len(os.Args) >= 2 && os.Args[1] == "lsp" {
		curated := tools.CuratedServerSpecs()
		detected := tools.DetectAvailableServers(curated)
		cwd, _ := os.Getwd()
		mgr := tools.NewLSPManager(cwd, detected, zap.NewNop())
		defer func() {
			shutCtx, shutCancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer shutCancel()
			_ = mgr.Shutdown(shutCtx)
		}()
		cmd := newLSPCmd(lspCmdDeps{Manager: mgr, CuratedSpecs: curated})
		cmd.SetArgs(os.Args[2:])
		if err := cmd.Execute(); err != nil {
			os.Exit(1)
		}
		return
	}

	// Dispatcher: intercept the "skills" subcommand group before flag.Parse()
	// so that Cobra handles its own flag parsing (same pattern as "commands").
	if len(os.Args) >= 2 && os.Args[1] == "skills" {
		projDir := filepath.Join(".", ".helix", "skills")
		var userDir string
		if userCfg, err := os.UserConfigDir(); err == nil {
			userDir = filepath.Join(userCfg, "helixcode", "skills")
		}
		skillReg := commands.NewSkillRegistry()
		loader := commands.NewSkillLoader(skillReg, projDir, userDir)
		if err := loader.Load(); err != nil {
			log.Printf("skills dispatcher: load failed: %v", err)
		}
		cmd := newSkillsCmd(skillsCmdDeps{Loader: loader, Registry: skillReg})
		cmd.SetArgs(os.Args[2:])
		if err := cmd.Execute(); err != nil {
			os.Exit(1)
		}
		return
	}

	cli := NewCLI()

	if err := cli.Run(); err != nil {
		// BLUFF-003: when a `--command` child exited with a real non-zero
		// status, propagate that exact code instead of the blanket exit 1.
		var ece *exitCodeError
		if errors.As(err, &ece) {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(ece.ExitCode())
		}
		log.Fatalf("Error: %v", err)
	}
}

// QA command handlers

func (c *CLI) handleQARun(ctx context.Context, serverURL, platforms, banks string, wait bool) error {
	if banks == "" {
		return fmt.Errorf("--qa-banks is required for --qa-run")
	}
	client := server.NewClient(serverURL)
	req := server.StartSessionRequest{
		Platforms: strings.Split(platforms, ","),
		Banks:     strings.Split(banks, ","),
	}
	state, err := client.StartQASession(req)
	if err != nil {
		return fmt.Errorf("failed to start QA session: %w", err)
	}
	fmt.Println(tr(ctx, "cli_qa_session_started", map[string]any{"ID": state.ID}))
	fmt.Printf("Platforms: %s\n", strings.Join(state.Platforms, ", "))
	fmt.Printf("Status: %s\n", state.Status)

	if wait {
		fmt.Println(tr(ctx, "cli_qa_waiting", nil))
		if err := client.WaitForSession(state.ID, os.Stdout); err != nil {
			return err
		}
		fmt.Println(tr(ctx, "cli_qa_session_completed", nil))
	}
	return nil
}

func (c *CLI) handleQAList(ctx context.Context, serverURL string) error {
	client := server.NewClient(serverURL)
	sessions, err := client.ListQASessions()
	if err != nil {
		return fmt.Errorf("failed to list sessions: %w", err)
	}
	if len(sessions) == 0 {
		fmt.Println(tr(ctx, "cli_qa_no_sessions", nil))
		return nil
	}
	fmt.Println("\n=== QA Sessions ===")
	fmt.Printf("%-12s %-12s %-20s %-30s %-20s\n", "ID", "Status", "Phase", "Platforms", "Started")
	for _, s := range sessions {
		id := s.ID
		if len(id) > 10 {
			id = id[:10]
		}
		fmt.Printf("%-12s %-12s %-20s %-30s %-20s\n",
			id, s.Status, s.Phase, strings.Join(s.Platforms, ","), s.StartTime.Format("2006-01-02 15:04:05"))
	}
	return nil
}

func (c *CLI) handleQAReport(ctx context.Context, serverURL, sessionID, format string) error {
	client := server.NewClient(serverURL)
	data, err := client.GetReport(sessionID, format)
	if err != nil {
		return fmt.Errorf("failed to get report: %w", err)
	}
	fmt.Println(string(data))
	return nil
}

func (c *CLI) handleQAScreenshot(ctx context.Context, serverURL, sessionID string) error {
	client := server.NewClient(serverURL)
	data, meta, err := client.CaptureScreenshot(sessionID, "", false)
	if err != nil {
		return fmt.Errorf("failed to capture screenshot: %w", err)
	}
	filename := fmt.Sprintf("screenshot-%s.png", sessionID)
	if err := os.WriteFile(filename, data, 0644); err != nil {
		return err
	}
	fmt.Println(tr(ctx, "cli_screenshot_saved", map[string]any{
		"Filename": filename,
		"Bytes":    len(data),
		"Platform": fmt.Sprintf("%v", meta["platform"]),
	}))
	return nil
}

func (c *CLI) handleQACancel(ctx context.Context, serverURL, sessionID string) error {
	client := server.NewClient(serverURL)
	if err := client.CancelQASession(sessionID); err != nil {
		return fmt.Errorf("failed to cancel session: %w", err)
	}
	fmt.Println(tr(ctx, "cli_session_cancelled", map[string]any{"ID": sessionID}))
	return nil
}
