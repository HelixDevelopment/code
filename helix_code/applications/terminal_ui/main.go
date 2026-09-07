package main

import (
	"context"
	_ "embed"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"

	"dev.helix.code/applications/terminal_ui/i18n"
	"dev.helix.code/internal/agent"
	"dev.helix.code/internal/commands"
	"dev.helix.code/internal/config"
	"dev.helix.code/internal/database"
	"dev.helix.code/internal/helixqa"
	"dev.helix.code/internal/llm"
	"dev.helix.code/internal/mcp"
	"dev.helix.code/internal/memory"
	"dev.helix.code/internal/notification"
	"dev.helix.code/internal/plugins"
	"dev.helix.code/internal/project"
	"dev.helix.code/internal/redis"
	"dev.helix.code/internal/server"
	"dev.helix.code/internal/session"
	"dev.helix.code/internal/task"
	"dev.helix.code/internal/tools"
	"dev.helix.code/internal/tools/git"
	"dev.helix.code/internal/worker"

	"github.com/gdamore/tcell/v2"
	"github.com/google/uuid"
	"github.com/rivo/tview"
	"go.uber.org/zap"
)

// TerminalUI represents the main terminal user interface
type TerminalUI struct {
	app                *tview.Application
	config             *config.Config
	helixConfig        *config.HelixConfig
	db                 *database.Database
	taskManager        *task.TaskManager
	workerManager      *worker.WorkerManager
	workerRepo         *worker.InMemoryWorkerRepository
	llmProvider        llm.Provider
	llmManager         *llm.ModelManager
	notificationEngine *notification.NotificationEngine
	server             *server.Server
	themeManager       *ThemeManager
	projectManager     *project.Manager
	sessionManager     *session.Manager
	qaEngine           *helixqa.Engine

	// Durable cross-session memory (HelixMemory, §11.4.74 reuse). Wired
	// default-on with a zero-config local SQLite backend so a fact stated in
	// one session is recalled in a fresh session OUT-OF-THE-BOX. memoryProvider
	// is nil only when the local store could not be opened (honest degrade to
	// the legacy in-process behaviour, never a fake "remembered").
	memoryManager  *memory.Manager
	memoryProvider *memory.HelixMemoryProvider
	memoryConvID   string

	// UI Components
	pages     *tview.Pages
	mainFlex  *tview.Flex
	sidebar   *tview.List
	content   *tview.Pages
	statusBar *tview.TextView

	// LLM Chat state
	chatHistory []llm.Message
	chatInput   *tview.InputField
	chatOutput  *tview.TextView
	// toolRegistry holds the read-only agentic tool set (git_status, fs_read,
	// glob, grep) the chat tool loop uses. Built once in Initialize; nil when
	// registry construction failed (the TUI still runs, falling back to the
	// plain streaming chat path). Only LevelReadOnly tools are registered, so
	// no approval manager is wired — nothing destructive is reachable.
	toolRegistry *tools.ToolRegistry

	// mcpManager / lspManager are the capability building blocks wired onto
	// toolRegistry in Initialize. They are retained on the struct so Close can
	// release their resources (MCP child processes / LSP server processes) on
	// shutdown. Either may be nil when its wiring failed or no servers are
	// configured/installed — the TUI degrades gracefully in that case.
	mcpManager *mcp.Manager
	lspManager *tools.LSPManager

	// skillDispatcher matches a user prompt against loaded `.md` skills and
	// renders the matched skill body (which then replaces the outgoing prompt).
	// skillRegistry is retained for /skills introspection. pluginLoader runs
	// `@plugin:<name> <action>` invocations. All three are graceful-nil when
	// their load failed or nothing is installed; sendChatMessage skips the
	// corresponding step when nil.
	skillDispatcher *agent.SkillDispatcher
	skillRegistry   *commands.SkillRegistry
	pluginLoader    *plugins.Loader
	// selectedModel is the model id chosen via the model picker. It is sent
	// as LLMRequest.Model on every chat turn — without it the provider call
	// goes out with an empty model id and the API rejects it (e.g. groq 404
	// "The model `` does not exist").
	selectedModel string

	// sessionUsedTokens accumulates the REAL per-session token total reported
	// by the provider's streaming telemetry (prompt+completion as the provider
	// reports it via Usage.TotalTokens on each turn). It backs the honest
	// context-window USED-% indicator rendered on the status bar — mirroring the
	// proven CLI implementation (cmd/cli formatContextUsage / contextWindowForModel).
	// CONST-035: never a fabricated or hardcoded number; only grows from values
	// consumeChatStream actually returns.
	sessionUsedTokens int

	// Current state
	currentUser    string
	currentSession string

	// translator resolves user-facing strings per CONST-046
	// (round-137 §11.4 migration). Defaults to NoopTranslator (loud
	// echo of message IDs) until SetTranslator wires a real
	// *i18nadapter.Translator at boot. Never nil after
	// NewTerminalUI returns.
	translator i18n.Translator
}

// NewTerminalUI creates a new Terminal UI instance
func NewTerminalUI() *TerminalUI {
	return &TerminalUI{
		app:        tview.NewApplication(),
		translator: i18n.NoopTranslator{},
	}
}

// SetTranslator injects the runtime Translator (per CONST-046
// round-137). Passing nil is a no-op — the NoopTranslator default
// installed by NewTerminalUI is preserved so the loud-echo safety
// net never disappears silently. helix_code wires
// *i18nadapter.Translator at boot.
func (tui *TerminalUI) SetTranslator(t i18n.Translator) {
	if t == nil {
		return
	}
	tui.translator = t
}

// t is a tiny call-site helper that resolves a message ID through
// the injected Translator and falls back to the literal id on error
// (loud echo — never silently swallow). Centralising the
// boilerplate keeps migrated call sites a single expression long.
func (tui *TerminalUI) t(id string) string {
	if tui.translator == nil {
		return id
	}
	got, err := tui.translator.T(context.Background(), id, nil)
	if err != nil || got == "" {
		return id
	}
	return got
}

// td resolves a message ID with go-i18n style template placeholders.
// It mirrors tui.t but threads templateData through Translator.T so
// CONST-046-migrated strings that embed runtime values (error text,
// counts, provider names) stay i18n-aware. Loud-echo fallback on a
// nil/erroring Translator — same anti-bluff posture as tui.t.
func (tui *TerminalUI) td(id string, data map[string]any) string {
	if tui.translator == nil {
		return id
	}
	got, err := tui.translator.T(context.Background(), id, data)
	if err != nil || got == "" {
		return id
	}
	return got
}

// Initialize sets up the Terminal UI with all dependencies
func (tui *TerminalUI) Initialize() error {
	// Load configuration
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("failed to load configuration: %v", err)
	}
	tui.config = cfg

	// Load Helix configuration
	helixCfg, err := config.LoadHelixConfig()
	if err != nil {
		// If helix config doesn't exist, use default
		helixCfg = &config.Config{} // Use zero value as default
	}
	tui.helixConfig = helixCfg

	// Initialize database. The TUI's primary surface (chat / LLM /
	// model picker) does NOT depend on PostgreSQL — only DB-backed
	// features (task & session persistence, the API server's auth) do.
	// So a failed DB connection at startup is NON-FATAL: log a clear,
	// i18n-aware warning (CONST-046) and continue in DEGRADED mode with
	// db == nil. task.NewTaskManager and server.New both tolerate a nil
	// db, and the System status view honestly reports DB as offline
	// rather than crashing or faking availability (anti-bluff §11.4).
	db, err := database.New(cfg.Database)
	if err != nil {
		log.Printf("%s", tui.td("terminal_ui_db_offline_warning", map[string]any{"Err": err.Error()}))
		db = nil
	}
	tui.db = db

	// Initialize Redis. Like the database, Redis is only needed by
	// DB-backed task/cache/queue features — not by chat/LLM. A failed
	// Redis connection is therefore NON-FATAL too: warn (i18n-aware)
	// and continue with rds == nil. Downstream consumers
	// (task.NewTaskManager, server.New) accept a nil Redis client.
	rds, err := redis.NewClient(&cfg.Redis)
	if err != nil {
		log.Printf("%s", tui.td("terminal_ui_redis_offline_warning", map[string]any{"Err": err.Error()}))
		rds = nil
	}

	// Initialize components. db has static type *database.Database; assigning a
	// typed-nil pointer directly into the database.DatabaseInterface parameter
	// would yield a NON-nil interface wrapping a typed nil, defeating the
	// `tm.db == nil` guard in storeTaskInDB and panicking on the first DB call
	// in degraded mode. Pass a TRUE nil interface so the guard fires and DB
	// persistence is cleanly disabled (anti-bluff §11.4: honest unavailable,
	// never a crash).
	var dbIface database.DatabaseInterface
	if db != nil {
		dbIface = db
	}
	tui.taskManager = task.NewTaskManager(dbIface, rds)

	// Initialize worker manager with in-memory repository for standalone UI
	workerRepo := worker.NewInMemoryWorkerRepository()
	tui.workerRepo = workerRepo
	tui.workerManager = worker.NewWorkerManager(workerRepo, 30*time.Second)

	tui.notificationEngine = notification.NewNotificationEngine()

	// Initialize project manager
	tui.projectManager = project.NewManager()

	// Initialize session manager
	tui.sessionManager = session.NewManager()

	// Initialize durable cross-session memory (HelixMemory zero-config local
	// store). Default-on: persists OUT-OF-THE-BOX with no API keys at
	// <user-config>/helix_memory/memory.db (override via HELIX_MEMORY_DB). On
	// open failure the TUI still runs with the legacy in-process behaviour —
	// honest degrade, never a fake recall.
	if memProvider, memErr := memory.NewHelixMemoryProviderDefault(); memErr != nil {
		log.Printf("⚠️  TUI: durable memory unavailable (%v); chat history will not persist across sessions", memErr)
	} else {
		tui.memoryProvider = memProvider
		tui.memoryManager = memory.NewManagerWithProvider(memProvider)
		// Hydrate prior memories so recall works on launch.
		hydrateCtx, hydrateCancel := context.WithTimeout(context.Background(), 10*time.Second)
		if hErr := tui.memoryManager.HydrateFromProvider(hydrateCtx); hErr != nil {
			log.Printf("⚠️  TUI: memory hydrate failed (%v); starting with empty recall", hErr)
		}
		hydrateCancel()
		// A stable active conversation for this TUI's persisted turns.
		if conv, cErr := tui.memoryManager.CreateConversation("tui-chat"); cErr == nil {
			tui.memoryConvID = conv.ID
			_ = tui.memoryManager.SetActive(conv.ID)
		}
		log.Printf("✅ TUI: durable memory active (HelixMemory local store at %s)", memProvider.DBPath())
	}

	// Initialize QA engine
	qaEngine, err := helixqa.NewEngine(cfg)
	if err == nil {
		tui.qaEngine = qaEngine
	}

	// W2c-1 cloud gate (operator mandate 2026-09-05, local-only adaptive
	// serving): wire the hosted-provider gate from configuration BEFORE any
	// handler can construct a provider. Mirrors internal/server/server.go.
	// Default false -- llm.cloud.enabled must be explicitly set true to
	// permit cloud construction.
	llm.SetCloudEnabled(cfg.LLM.Cloud.Enabled)

	// Initialize LLM manager and register cloud providers discovered from
	// environment API keys (CONST-036 / BLUFF-002). Without this, the chat
	// showed "Available Models Total: 0" because no provider was ever
	// registered. registerEnvProviders only registers a provider when its key
	// env var is present and non-placeholder, so a no-key environment still
	// honestly reports zero models rather than a fabricated list.
	tui.llmManager = llm.NewModelManager()
	// Wire LLMsVerifier (CONST-036/040) BEFORE registering providers so the
	// Helix Agent ensemble resolves each member's model from verified,
	// chat-capable catalogue entries — fully dynamic, no hardcoded model names.
	if wireVerifierAdapter(tui.llmManager, cfg) {
		log.Printf("✅ TUI: LLMsVerifier wired (ensemble model resolution is verifier-driven)")
	}
	if n := registerEnvProviders(tui.llmManager, cfg); n > 0 {
		log.Printf("✅ TUI: registered %d cloud LLM provider(s) from environment keys", n)
	}
	tui.chatHistory = make([]llm.Message, 0)

	// Build the read-only agentic tool registry ONCE (§11.4.133 safety: only
	// LevelReadOnly tools are registered, so the approval gate is bypassed and
	// the recording stays unattended-safe — nothing destructive is reachable).
	// On any construction failure the TUI still runs: toolRegistry stays nil
	// and sendChatMessage falls back to the plain streaming chat path.
	if reg, regErr := tools.NewToolRegistry(nil); regErr != nil {
		log.Printf("⚠️  TUI: tool registry unavailable (%v); chat falls back to plain streaming", regErr)
	} else {
		repoDir, wdErr := os.Getwd()
		if wdErr != nil {
			repoDir = "."
		}
		// Pin git_status to the enclosing git repository root (walk up for .git)
		// so it inspects the real repo even when the TUI is launched from a
		// subdirectory; falls back to the working directory when no .git is found.
		repoDir = resolveRepoRoot(repoDir)
		reg.Register(git.NewGitStatusTool(repoDir))
		// fs_read / glob / grep are auto-registered by NewToolRegistry
		// (all three are LevelReadOnly). They are present already; the
		// explicit git_status registration above adds the read-only git
		// inspection capability. No write/exec tool is added.

		// Wire LSP — registers the two read-only diagnostics tools
		// (lsp_get_diagnostics, lsp_analyze_diagnostic). WireLSP always
		// returns a non-nil manager even when no LSP server is on PATH (its
		// spec set is then empty and every call is a no-op), so the wiring is
		// branch-free and degrades gracefully. The diagnostics tools are
		// LevelReadOnly, so they pass the ReadOnlyOnly agent loop unchanged.
		tui.lspManager = tools.WireLSP(reg, repoDir, zap.NewNop())
		log.Printf("✅ TUI: LSP diagnostics tools wired (lsp_get_diagnostics, lsp_analyze_diagnostic)")

		// Wire MCP — merge the user + project (.helixcode/mcp.yml) MCP server
		// configs, start the alwaysLoad servers, and register their tools onto
		// the registry. Servers marked readOnly:true in config register their
		// tools at LevelReadOnly so they pass the ReadOnlyOnly agent loop.
		// Graceful: a missing config, a Start failure (e.g. npx not installed),
		// or zero tools all log + continue — the TUI still runs without MCP.
		mcpUserPath := filepath.Join(os.Getenv("HOME"), ".helixcode", "mcp.yml")
		mcpCfg, mcpErr := mcp.LoadMerged(mcpUserPath, ".helixcode/mcp.yml")
		if mcpErr != nil {
			log.Printf("⚠️  TUI: MCP config load failed (%v); continuing without MCP tools", mcpErr)
		} else if mcpCfg == nil || len(mcpCfg.Servers) == 0 {
			log.Printf("ℹ️  TUI: no MCP servers configured; continuing without MCP tools")
		} else {
			mgr := mcp.NewManager()
			mgr.SetConfig(mcpCfg)
			// Start dials alwaysLoad servers; a per-server connect failure does
			// not error the call (only a build/transport failure does). Start
			// BEFORE RegisterMCPManager so the registry sees ready clients' tools.
			if startErr := mgr.Start(context.Background()); startErr != nil {
				log.Printf("⚠️  TUI: MCP start failed (%v); continuing without MCP tools", startErr)
			} else {
				reg.RegisterMCPManager(mgr)
				tui.mcpManager = mgr
				log.Printf("✅ TUI: MCP wired — %d tool(s) registered from %d server(s)", len(mgr.Tools()), len(mcpCfg.Servers))
			}
		}

		tui.toolRegistry = reg
		log.Printf("✅ TUI: agentic tool registry ready (read-only: git_status, fs_read, glob, grep + LSP/MCP tools)")
	}

	// Load skills + plugins ONCE at startup. Both degrade gracefully: a load
	// error or a missing directory yields a nil dispatcher / loader and
	// sendChatMessage simply skips the corresponding step. Skills are scanned
	// from the user skills dir (~/.helix/skills) then the project dir
	// (.helix/skills); the project dir comes last so it overrides on collision.
	userSkillsDir := filepath.Join(os.Getenv("HOME"), ".helix", "skills")
	if skReg, skDisp, skErr := agent.LoadSkillsAndDispatcher([]string{userSkillsDir, ".helix/skills"}); skErr != nil {
		log.Printf("⚠️  TUI: skills unavailable (%v); chat continues without skill triggers", skErr)
	} else {
		tui.skillRegistry = skReg
		tui.skillDispatcher = skDisp
		log.Printf("✅ TUI: skills loaded (%d skill(s) registered)", len(skReg.List()))
	}
	if loader, plErr := plugins.LoadPlugins(context.Background(), "plugins"); plErr != nil {
		log.Printf("⚠️  TUI: plugins unavailable (%v); chat continues without @plugin: invocations", plErr)
	} else {
		tui.pluginLoader = loader
		log.Printf("✅ TUI: plugins loaded (%d plugin(s) available)", len(loader.List()))
	}

	// Initialize server for API calls
	tui.server = server.New(cfg, db, rds)

	// Initialize theme manager
	tui.themeManager = NewThemeManager()

	// Setup UI components
	tui.setupUI()

	return nil
}

// setupUI initializes the user interface components
func (tui *TerminalUI) setupUI() {
	// Force the HelixCode brand dark palette onto tview.Styles BEFORE any
	// widget is constructed below — tview widgets snapshot their default
	// colors from tview.Styles at creation time, so this must run first
	// (and well before app.Run) for the whole TUI to inherit the brand.
	initHelixTheme()

	// Create main pages container
	tui.pages = tview.NewPages()

	// Create main layout
	tui.mainFlex = tview.NewFlex().SetDirection(tview.FlexColumn)

	// Create sidebar navigation. Sidebar descriptions + title are
	// resolved via i18n.Translator per CONST-046 round-137. The
	// primary item labels (Dashboard/Tasks/...) are short proper
	// nouns / mnemonic-letter anchors and are intentionally kept
	// hardcoded in this round; they are tracked for migration in a
	// later round once the i18n surface stabilises.
	tui.sidebar = tview.NewList().
		AddItem("Dashboard", tui.t("terminal_ui_sidebar_dashboard_desc"), 'd', tui.showDashboard).
		AddItem("Tasks", tui.t("terminal_ui_sidebar_tasks_desc"), 't', tui.showTasks).
		AddItem("Workers", tui.t("terminal_ui_sidebar_workers_desc"), 'w', tui.showWorkers).
		AddItem("Projects", tui.t("terminal_ui_sidebar_projects_desc"), 'p', tui.showProjects).
		AddItem("Sessions", tui.t("terminal_ui_sidebar_sessions_desc"), 's', tui.showSessions).
		AddItem("LLM", tui.t("terminal_ui_sidebar_llm_desc"), 'l', tui.showLLM).
		AddItem("QA", tui.t("terminal_ui_sidebar_qa_desc"), 'q', tui.showQA).
		AddItem("Settings", tui.t("terminal_ui_sidebar_settings_desc"), 'c', tui.showSettings).
		ShowSecondaryText(false)

	tui.sidebar.SetBorder(true).SetTitle(tui.t("terminal_ui_sidebar_title"))

	// Create content area
	tui.content = tview.NewPages()

	// Create status bar
	tui.statusBar = tview.NewTextView().
		SetDynamicColors(true).
		SetText(tui.t("terminal_ui_status_bar_default")).
		SetTextAlign(tview.AlignCenter)
	tui.statusBar.SetBorder(true).SetTitle("Status")

	// Setup main layout
	tui.mainFlex.
		AddItem(tui.sidebar, 25, 0, true).
		AddItem(tui.content, 0, 1, false)

	// Create main flex with status bar
	mainContainer := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(tui.mainFlex, 0, 1, false).
		AddItem(tui.statusBar, 3, 0, false)

	// Add main page
	tui.pages.AddPage("main", mainContainer, true, true)

	// Show initial dashboard
	tui.showDashboard()
}

// embeddedLogoASCII is the colored (tview-tagged) nautilus-spiral ASCII art of
// the brand logo (assets/Logo.png), embedded at build time so the dashboard logo
// always renders — matching the logo in shape AND colour (green→teal gradient) —
// regardless of the process working directory. Regenerate via
// scripts/gen_logo_ascii.go (the §11.4.77 mechanism for this derivative).
//
//go:embed logo-ascii.txt
var embeddedLogoASCII string

// loadASCIIArt returns the colored ASCII logo. It prefers a runtime override file
// (assets/images/logo-ascii.txt relative to cwd) so the art can be edited without
// a rebuild, and otherwise returns the build-time embedded copy — so the colored
// logo shows even when the TUI is launched from a different directory.
func (tui *TerminalUI) loadASCIIArt() string {
	if data, err := ioutil.ReadFile("assets/images/logo-ascii.txt"); err == nil && len(data) > 0 {
		return string(data)
	}
	return embeddedLogoASCII
}

// showDashboard displays the main dashboard
func (tui *TerminalUI) showDashboard() {
	dashboard := tview.NewFlex().SetDirection(tview.FlexRow)

	// Header with ASCII logo + brand banner. The banner is a compact
	// lime-on-teal text header referencing the HelixCode brand (terminals
	// can't render the PNG nautilus spiral, so we use a Unicode spiral
	// glyph + colored wordmark). Colors are the brand palette constants
	// from theme.go (PRIMARY lime / SECONDARY teal).
	asciiArt := tui.loadASCIIArt()
	banner := fmt.Sprintf(
		"[%s::b]🐚 HelixCode[-:-:-]\n[%s]Enterprise distributed AI development platform[-]",
		hxcPrimaryHex, hxcSecondaryHex,
	)
	header := tview.NewTextView().
		SetText(banner + "\n\n" + asciiArt).
		SetTextAlign(tview.AlignCenter).
		SetDynamicColors(true)
	header.SetBorder(true).SetTitle("HelixCode")
	header.SetBorderColor(hxcPrimary).SetTitleColor(hxcPrimary)

	// Stats grid
	statsGrid := tview.NewGrid().SetRows(3, 3, 3).SetColumns(30, 30, 30).SetBorders(true)

	// Worker stats
	workerStats := tui.createWorkerStatsView()
	statsGrid.AddItem(workerStats, 0, 0, 1, 1, 0, 0, false)

	// Task stats
	taskStats := tui.createTaskStatsView()
	statsGrid.AddItem(taskStats, 0, 1, 1, 1, 0, 0, false)

	// System stats
	systemStats := tui.createSystemStatsView()
	statsGrid.AddItem(systemStats, 0, 2, 1, 1, 0, 0, false)

	// Recent activity
	activityView := tui.createActivityView()
	statsGrid.AddItem(activityView, 1, 0, 1, 3, 0, 0, false)

	// Quick actions
	quickActions := tui.createQuickActionsView()
	statsGrid.AddItem(quickActions, 2, 0, 1, 3, 0, 0, false)

	dashboard.
		AddItem(header, 15, 0, false).
		AddItem(statsGrid, 0, 1, false)

	tui.content.SwitchToPage("dashboard")
	tui.content.AddPage("dashboard", dashboard, true, true)
}

// createWorkerStatsView creates the worker statistics view
func (tui *TerminalUI) createWorkerStatsView() tview.Primitive {
	view := tview.NewTextView()
	view.SetDynamicColors(true)
	view.SetBorder(true)
	view.SetTitle("Workers")

	// Get real worker stats from the worker repository
	ctx := context.Background()
	workers, err := tui.workerRepo.ListWorkers(ctx, "")

	totalWorkers := 0
	activeWorkers := 0
	healthyWorkers := 0

	if err == nil {
		totalWorkers = len(workers)
		for _, w := range workers {
			if w.Status == worker.WorkerStatusActive {
				activeWorkers++
			}
			if w.HealthStatus == worker.WorkerHealthHealthy {
				healthyWorkers++
			}
		}
	}

	content := fmt.Sprintf("[green]Total: %d\n[white]Active: %d\n[yellow]Healthy: %d\n[red]Failed: %d",
		totalWorkers, activeWorkers, healthyWorkers, totalWorkers-healthyWorkers)

	view.SetText(content)
	return view
}

// createTaskStatsView creates the task statistics view
func (tui *TerminalUI) createTaskStatsView() tview.Primitive {
	view := tview.NewTextView()
	view.SetDynamicColors(true)
	view.SetBorder(true)
	view.SetTitle("Tasks")

	// Get real task stats from session manager (tasks are tracked through sessions)
	sessions := tui.sessionManager.GetAll()
	stats := tui.sessionManager.GetStatistics()

	totalTasks := stats.Total
	completedTasks := stats.ByStatus[session.StatusCompleted]
	runningTasks := stats.ByStatus[session.StatusActive]
	failedTasks := stats.ByStatus[session.StatusFailed]
	pausedTasks := stats.ByStatus[session.StatusPaused]

	// Also count by mode for more detailed stats
	planningCount := 0
	buildingCount := 0
	testingCount := 0
	for _, s := range sessions {
		switch s.Mode {
		case session.ModePlanning:
			planningCount++
		case session.ModeBuilding:
			buildingCount++
		case session.ModeTesting:
			testingCount++
		}
	}

	content := fmt.Sprintf("[blue]Total: %d\n[green]Completed: %d\n[yellow]Running: %d\n[gray]Paused: %d\n[red]Failed: %d",
		totalTasks, completedTasks, runningTasks, pausedTasks, failedTasks)
	view.SetText(content)
	return view
}

// createSystemStatsView creates the system statistics view
func (tui *TerminalUI) createSystemStatsView() tview.Primitive {
	view := tview.NewTextView()
	view.SetDynamicColors(true)
	view.SetBorder(true)
	view.SetTitle("System")

	// Get real system stats from various managers
	ctx := context.Background()

	// Check database status. A nil db means the DB was unreachable at
	// startup and the TUI is running in degraded mode (chat/LLM works,
	// DB-backed task/session persistence is disabled). Report that
	// honestly rather than implying the feature is merely "off"
	// (anti-bluff §11.4 — a disabled feature must say it's unavailable).
	dbStatus := "[green]Connected"
	if tui.db == nil {
		dbStatus = "[red]" + tui.t("terminal_ui_db_status_offline")
	}

	// Check LLM provider status
	llmStatus := "[yellow]Not Configured"
	if tui.llmProvider != nil {
		if tui.llmProvider.IsAvailable(ctx) {
			llmStatus = "[green]Available"
		} else {
			llmStatus = "[red]Unavailable"
		}
	}

	// Get project count
	projects, _ := tui.projectManager.ListProjects(ctx, "")
	projectCount := len(projects)

	// Get active project
	activeProjectName := "None"
	if activeProject, err := tui.projectManager.GetActiveProject(ctx); err == nil && activeProject != nil {
		activeProjectName = activeProject.Name
	}

	content := fmt.Sprintf("[green]Status: Operational\n[white]Database: %s\n[yellow]LLM: %s\n[blue]Projects: %d\n[cyan]Active: %s",
		dbStatus, llmStatus, projectCount, activeProjectName)
	view.SetText(content)
	return view
}

// createActivityView creates the recent activity view
func (tui *TerminalUI) createActivityView() tview.Primitive {
	view := tview.NewTextView()
	view.SetDynamicColors(true)
	view.SetBorder(true)
	view.SetTitle("Recent Activity")

	var activities []string

	// Add system initialization
	activities = append(activities, "[green]+ System initialized")

	// Add recent sessions
	recentSessions := tui.sessionManager.GetRecent(3)
	for _, s := range recentSessions {
		statusIcon := "+"
		color := "[white]"
		switch s.Status {
		case session.StatusActive:
			statusIcon = ">"
			color = "[green]"
		case session.StatusPaused:
			statusIcon = "||"
			color = "[yellow]"
		case session.StatusCompleted:
			statusIcon = "+"
			color = "[blue]"
		case session.StatusFailed:
			statusIcon = "x"
			color = "[red]"
		}
		activities = append(activities, fmt.Sprintf("%s%s Session '%s' (%s)", color, statusIcon, s.Name, s.Status))
	}

	// Add worker info
	ctx := context.Background()
	workers, _ := tui.workerRepo.ListWorkers(ctx, "")
	if len(workers) > 0 {
		activities = append(activities, fmt.Sprintf("[cyan]+ %d workers registered", len(workers)))
	}

	// Add project info
	projects, _ := tui.projectManager.ListProjects(ctx, "")
	if len(projects) > 0 {
		activities = append(activities, fmt.Sprintf("[blue]+ %d projects loaded", len(projects)))
	}

	// Add LLM status
	models := tui.llmManager.GetAvailableModels()
	if len(models) > 0 {
		activities = append(activities, fmt.Sprintf("[magenta]+ %d LLM models available", len(models)))
	} else {
		activities = append(activities, "[yellow]! "+tui.t("terminal_ui_dashboard_no_llm_providers"))
	}

	if len(activities) == 1 {
		activities = append(activities, "[gray]No recent activity")
	}

	view.SetText(strings.Join(activities, "\n"))
	return view
}

// createQuickActionsView creates the quick actions view
func (tui *TerminalUI) createQuickActionsView() *tview.List {
	list := tview.NewList().
		AddItem("New Task", tui.t("terminal_ui_quickaction_new_task_desc"), 'n', func() {
			tui.showNewTaskForm()
		}).
		AddItem("Add Worker", tui.t("terminal_ui_quickaction_add_worker_desc"), 'a', func() {
			tui.showAddWorkerForm()
		}).
		AddItem("New Project", tui.t("terminal_ui_quickaction_new_project_desc"), 'p', func() {
			tui.showNewProjectForm()
		}).
		AddItem("LLM Chat", tui.t("terminal_ui_quickaction_llm_chat_desc"), 'c', func() {
			tui.showLLM()
		})

	list.SetBorder(true).SetTitle("Quick Actions")
	return list
}

// showTasks displays the task management interface
func (tui *TerminalUI) showTasks() {
	tasksView := tview.NewFlex().SetDirection(tview.FlexRow)

	header := tview.NewTextView()
	header.SetText("[::b]Task Management")
	header.SetTextAlign(tview.AlignCenter)
	header.SetDynamicColors(true)
	header.SetBorder(true)

	// Create task list using components
	components := NewUIComponents(tui)

	// Sample task data - will be replaced with real data
	taskItems := []ListItem{
		{MainText: tui.t("terminal_ui_sample_task_codegen_title"), SecondaryText: tui.t("terminal_ui_sample_task_codegen_desc"), Shortcut: '1'},
		{MainText: tui.t("terminal_ui_sample_task_testing_title"), SecondaryText: tui.t("terminal_ui_sample_task_testing_desc"), Shortcut: '2'},
		{MainText: tui.t("terminal_ui_sample_task_build_title"), SecondaryText: tui.t("terminal_ui_sample_task_build_desc"), Shortcut: '3'},
	}

	taskList := components.CreateList("Tasks", taskItems)

	// Action buttons
	actions := tview.NewFlex().SetDirection(tview.FlexColumn)
	newTaskBtn := tview.NewButton("New Task").SetSelectedFunc(func() {
		tui.showNewTaskForm()
	})
	actions.AddItem(newTaskBtn, 0, 1, false)

	tasksView.
		AddItem(header, 3, 0, false).
		AddItem(taskList, 0, 1, false).
		AddItem(actions, 3, 0, false)

	tui.content.SwitchToPage("tasks")
	tui.content.AddPage("tasks", tasksView, true, true)
}

// showWorkers displays the worker management interface
func (tui *TerminalUI) showWorkers() {
	workersView := tview.NewFlex().SetDirection(tview.FlexRow)

	header := tview.NewTextView().
		SetText("[::b]Worker Management").
		SetTextAlign(tview.AlignCenter).
		SetDynamicColors(true)
	header.SetBorder(true)

	// Create worker table
	workerTable := tview.NewTable().
		SetBorders(true).
		SetSelectable(true, false)
	workerTable.SetBorder(true).SetTitle("Workers")

	// Set headers
	headers := []string{"ID", "Hostname", "Status", "Health", "CPU %", "Memory %", "Tasks", "Last Heartbeat"}
	for col, h := range headers {
		workerTable.SetCell(0, col, tview.NewTableCell(h).
			SetTextColor(tcell.ColorYellow).
			SetAlign(tview.AlignCenter).
			SetSelectable(false))
	}

	// Get workers from repository
	ctx := context.Background()
	workers, err := tui.workerRepo.ListWorkers(ctx, "")
	if err != nil {
		log.Printf("Failed to list workers: %v", err)
	}

	if len(workers) == 0 {
		workerTable.SetCell(1, 0, tview.NewTableCell(tui.t("terminal_ui_workers_none_registered")).
			SetAlign(tview.AlignCenter).
			SetSelectable(false))
	} else {
		for row, w := range workers {
			statusColor := "[green]"
			if w.Status == worker.WorkerStatusOffline {
				statusColor = "[red]"
			} else if w.Status == worker.WorkerStatusMaintenance {
				statusColor = "[yellow]"
			}

			healthColor := "[green]"
			if w.HealthStatus == worker.WorkerHealthUnhealthy {
				healthColor = "[red]"
			} else if w.HealthStatus == worker.WorkerHealthDegraded {
				healthColor = "[yellow]"
			}

			workerTable.SetCell(row+1, 0, tview.NewTableCell(w.ID.String()[:8]).SetAlign(tview.AlignLeft))
			workerTable.SetCell(row+1, 1, tview.NewTableCell(w.Hostname).SetAlign(tview.AlignLeft))
			workerTable.SetCell(row+1, 2, tview.NewTableCell(statusColor+string(w.Status)).SetAlign(tview.AlignCenter))
			workerTable.SetCell(row+1, 3, tview.NewTableCell(healthColor+string(w.HealthStatus)).SetAlign(tview.AlignCenter))
			workerTable.SetCell(row+1, 4, tview.NewTableCell(fmt.Sprintf("%.1f%%", w.CPUUsagePercent)).SetAlign(tview.AlignRight))
			workerTable.SetCell(row+1, 5, tview.NewTableCell(fmt.Sprintf("%.1f%%", w.MemoryUsagePercent)).SetAlign(tview.AlignRight))
			workerTable.SetCell(row+1, 6, tview.NewTableCell(fmt.Sprintf("%d/%d", w.CurrentTasksCount, w.MaxConcurrentTasks)).SetAlign(tview.AlignCenter))
			workerTable.SetCell(row+1, 7, tview.NewTableCell(w.LastHeartbeat.Format("15:04:05")).SetAlign(tview.AlignCenter))
		}
	}

	// Worker stats panel
	statsPanel := tui.createWorkerStatsPanel(workers)

	// Action buttons
	actions := tview.NewFlex().SetDirection(tview.FlexColumn)
	addWorkerBtn := tview.NewButton("Add Worker").SetSelectedFunc(func() {
		tui.showAddWorkerForm()
	})
	refreshBtn := tview.NewButton("Refresh").SetSelectedFunc(func() {
		tui.showWorkers()
	})
	actions.AddItem(addWorkerBtn, 0, 1, false)
	actions.AddItem(refreshBtn, 0, 1, false)

	// Main content area
	contentFlex := tview.NewFlex().SetDirection(tview.FlexColumn)
	contentFlex.AddItem(workerTable, 0, 3, true)
	contentFlex.AddItem(statsPanel, 30, 0, false)

	workersView.
		AddItem(header, 3, 0, false).
		AddItem(contentFlex, 0, 1, true).
		AddItem(actions, 3, 0, false)

	tui.content.SwitchToPage("workers")
	tui.content.AddPage("workers", workersView, true, true)
}

// createWorkerStatsPanel creates a statistics panel for workers
func (tui *TerminalUI) createWorkerStatsPanel(workers []*worker.Worker) *tview.TextView {
	panel := tview.NewTextView().SetDynamicColors(true)
	panel.SetBorder(true).SetTitle("Statistics")

	totalWorkers := len(workers)
	activeWorkers := 0
	healthyWorkers := 0
	totalTasks := 0
	var totalCPU, totalMem float64

	for _, w := range workers {
		if w.Status == worker.WorkerStatusActive {
			activeWorkers++
		}
		if w.HealthStatus == worker.WorkerHealthHealthy {
			healthyWorkers++
		}
		totalTasks += w.CurrentTasksCount
		totalCPU += w.CPUUsagePercent
		totalMem += w.MemoryUsagePercent
	}

	avgCPU := 0.0
	avgMem := 0.0
	if totalWorkers > 0 {
		avgCPU = totalCPU / float64(totalWorkers)
		avgMem = totalMem / float64(totalWorkers)
	}

	content := fmt.Sprintf(`[::b]Worker Summary[white]

[green]Total:[white] %d
[green]Active:[white] %d
[green]Healthy:[white] %d
[red]Offline:[white] %d

[::b]Resource Usage[white]

[yellow]Avg CPU:[white] %.1f%%
[yellow]Avg Memory:[white] %.1f%%
[blue]Active Tasks:[white] %d`,
		totalWorkers, activeWorkers, healthyWorkers, totalWorkers-activeWorkers,
		avgCPU, avgMem, totalTasks)

	panel.SetText(content)
	return panel
}

// showAddWorkerForm displays a form for adding a new worker
func (tui *TerminalUI) showAddWorkerForm() {
	form := tview.NewForm()
	form.SetBorder(true).SetTitle("Add New Worker").SetTitleAlign(tview.AlignLeft)

	var hostname, displayName, sshHost, sshUser string
	var sshPort int = 22
	var maxTasks int = 4

	form.AddInputField("Hostname", "", 30, nil, func(text string) {
		hostname = text
	})
	form.AddInputField("Display Name", "", 30, nil, func(text string) {
		displayName = text
	})
	form.AddInputField("SSH Host", "", 30, nil, func(text string) {
		sshHost = text
	})
	form.AddInputField("SSH Port", "22", 10, tview.InputFieldInteger, func(text string) {
		fmt.Sscanf(text, "%d", &sshPort)
	})
	form.AddInputField("SSH User", "", 20, nil, func(text string) {
		sshUser = text
	})
	form.AddInputField(tui.t("terminal_ui_form_max_concurrent_tasks"), "4", 10, tview.InputFieldInteger, func(text string) {
		fmt.Sscanf(text, "%d", &maxTasks)
	})

	form.AddButton("Add", func() {
		ctx := context.Background()
		newWorker := &worker.Worker{
			ID:          uuid.New(),
			Hostname:    hostname,
			DisplayName: displayName,
			SSHConfig: map[string]interface{}{
				"host":     sshHost,
				"port":     sshPort,
				"username": sshUser,
			},
			Status:             worker.WorkerStatusActive,
			HealthStatus:       worker.WorkerHealthHealthy,
			MaxConcurrentTasks: maxTasks,
			LastHeartbeat:      time.Now(),
			CreatedAt:          time.Now(),
			UpdatedAt:          time.Now(),
		}

		if err := tui.workerRepo.CreateWorker(ctx, newWorker); err != nil {
			tui.statusBar.SetText("[red]" + tui.td("terminal_ui_worker_add_failed", map[string]any{"Error": err.Error()}))
		} else {
			tui.statusBar.SetText("[green]" + tui.td("terminal_ui_worker_added", map[string]any{"Hostname": hostname}))
			tui.showWorkers()
		}

		tui.pages.RemovePage("addWorkerForm")
		tui.app.SetFocus(tui.content)
	})

	form.AddButton("Cancel", func() {
		tui.pages.RemovePage("addWorkerForm")
		tui.app.SetFocus(tui.content)
	})

	modal := tview.NewFlex().
		AddItem(nil, 0, 1, false).
		AddItem(tview.NewFlex().SetDirection(tview.FlexRow).
			AddItem(nil, 0, 1, false).
			AddItem(form, 22, 1, true).
			AddItem(nil, 0, 1, false), 60, 1, true).
		AddItem(nil, 0, 1, false)

	tui.pages.AddPage("addWorkerForm", modal, true, true)
	tui.app.SetFocus(form)
}

// showProjects displays the project management interface
func (tui *TerminalUI) showProjects() {
	projectsView := tview.NewFlex().SetDirection(tview.FlexRow)

	header := tview.NewTextView().
		SetText("[::b]Project Management").
		SetTextAlign(tview.AlignCenter).
		SetDynamicColors(true)
	header.SetBorder(true)

	// Create project table
	projectTable := tview.NewTable().
		SetBorders(true).
		SetSelectable(true, false)
	projectTable.SetBorder(true).SetTitle("Projects")

	// Set headers
	headers := []string{"Name", "Type", "Path", "Status", "Created", "Updated"}
	for col, h := range headers {
		projectTable.SetCell(0, col, tview.NewTableCell(h).
			SetTextColor(tcell.ColorYellow).
			SetAlign(tview.AlignCenter).
			SetSelectable(false))
	}

	// Get projects from manager
	ctx := context.Background()
	projects, err := tui.projectManager.ListProjects(ctx, "")
	if err != nil {
		log.Printf("Failed to list projects: %v", err)
	}

	if len(projects) == 0 {
		projectTable.SetCell(1, 0, tview.NewTableCell(tui.t("terminal_ui_projects_none_found")).
			SetAlign(tview.AlignCenter).
			SetSelectable(false))
	} else {
		for row, p := range projects {
			statusColor := "[white]"
			statusText := "Inactive"
			if p.Active {
				statusColor = "[green]"
				statusText = "Active"
			}

			projectTable.SetCell(row+1, 0, tview.NewTableCell(p.Name).SetAlign(tview.AlignLeft))
			projectTable.SetCell(row+1, 1, tview.NewTableCell(p.Type).SetAlign(tview.AlignCenter))
			projectTable.SetCell(row+1, 2, tview.NewTableCell(truncatePath(p.Path, 30)).SetAlign(tview.AlignLeft))
			projectTable.SetCell(row+1, 3, tview.NewTableCell(statusColor+statusText).SetAlign(tview.AlignCenter))
			projectTable.SetCell(row+1, 4, tview.NewTableCell(p.CreatedAt.Format("2006-01-02")).SetAlign(tview.AlignCenter))
			projectTable.SetCell(row+1, 5, tview.NewTableCell(p.UpdatedAt.Format("2006-01-02")).SetAlign(tview.AlignCenter))
		}
	}

	// Handle project selection
	projectTable.SetSelectedFunc(func(row, col int) {
		if row > 0 && row <= len(projects) {
			selectedProject := projects[row-1]
			tui.showProjectDetails(selectedProject)
		}
	})

	// Project details panel
	detailsPanel := tview.NewTextView().SetDynamicColors(true)
	detailsPanel.SetBorder(true).SetTitle("Project Details")

	activeProject, _ := tui.projectManager.GetActiveProject(ctx)
	if activeProject != nil {
		detailsPanel.SetText(tui.formatProjectDetails(activeProject))
	} else {
		detailsPanel.SetText("[gray]Select a project to view details")
	}

	// Action buttons
	actions := tview.NewFlex().SetDirection(tview.FlexColumn)
	newProjectBtn := tview.NewButton("New Project").SetSelectedFunc(func() {
		tui.showNewProjectForm()
	})
	setActiveBtn := tview.NewButton("Set Active").SetSelectedFunc(func() {
		row, _ := projectTable.GetSelection()
		if row > 0 && row <= len(projects) {
			selectedProject := projects[row-1]
			if err := tui.projectManager.SetActiveProject(ctx, selectedProject.ID); err != nil {
				tui.statusBar.SetText("[red]" + tui.td("terminal_ui_project_set_active_failed", map[string]any{"Error": err.Error()}))
			} else {
				tui.statusBar.SetText("[green]" + tui.td("terminal_ui_project_set_active", map[string]any{"Name": selectedProject.Name}))
				tui.showProjects()
			}
		}
	})
	refreshBtn := tview.NewButton("Refresh").SetSelectedFunc(func() {
		tui.showProjects()
	})
	actions.AddItem(newProjectBtn, 0, 1, false)
	actions.AddItem(setActiveBtn, 0, 1, false)
	actions.AddItem(refreshBtn, 0, 1, false)

	// Main content area
	contentFlex := tview.NewFlex().SetDirection(tview.FlexColumn)
	contentFlex.AddItem(projectTable, 0, 2, true)
	contentFlex.AddItem(detailsPanel, 40, 0, false)

	projectsView.
		AddItem(header, 3, 0, false).
		AddItem(contentFlex, 0, 1, true).
		AddItem(actions, 3, 0, false)

	tui.content.SwitchToPage("projects")
	tui.content.AddPage("projects", projectsView, true, true)
}

// formatProjectDetails formats project details for display
func (tui *TerminalUI) formatProjectDetails(p *project.Project) string {
	return fmt.Sprintf(`[::b]%s[white]

[yellow]Type:[white] %s
[yellow]Path:[white] %s
[yellow]Description:[white] %s

[::b]Build Commands[white]
[green]Build:[white] %s
[green]Test:[white] %s
[green]Lint:[white] %s

[::b]Metadata[white]
[blue]Framework:[white] %s
[blue]Language:[white] %s
[blue]Created:[white] %s`,
		p.Name, p.Type, p.Path, p.Description,
		p.Metadata.BuildCommand, p.Metadata.TestCommand, p.Metadata.LintCommand,
		p.Metadata.Framework, p.Metadata.LanguageVersion,
		p.CreatedAt.Format("2006-01-02 15:04:05"))
}

// showProjectDetails shows detailed information about a project
func (tui *TerminalUI) showProjectDetails(p *project.Project) {
	modal := tview.NewModal()
	modal.SetText(fmt.Sprintf("Project: %s\nType: %s\nPath: %s\n\nBuild: %s\nTest: %s",
		p.Name, p.Type, p.Path, p.Metadata.BuildCommand, p.Metadata.TestCommand))
	modal.AddButtons([]string{"Close", "Set Active", "Delete"})
	modal.SetDoneFunc(func(buttonIndex int, buttonLabel string) {
		ctx := context.Background()
		switch buttonLabel {
		case "Set Active":
			if err := tui.projectManager.SetActiveProject(ctx, p.ID); err != nil {
				tui.statusBar.SetText(fmt.Sprintf("[red]Error: %v", err))
			} else {
				tui.statusBar.SetText(fmt.Sprintf("[green]Active: %s", p.Name))
			}
		case "Delete":
			if err := tui.projectManager.DeleteProject(ctx, p.ID); err != nil {
				tui.statusBar.SetText(fmt.Sprintf("[red]Error: %v", err))
			} else {
				tui.statusBar.SetText(fmt.Sprintf("[yellow]Deleted: %s", p.Name))
			}
		}
		tui.pages.RemovePage("projectDetails")
		tui.showProjects()
	})

	tui.pages.AddPage("projectDetails", modal, true, true)
}

// showNewProjectForm displays a form for creating a new project
func (tui *TerminalUI) showNewProjectForm() {
	form := tview.NewForm()
	form.SetBorder(true).SetTitle(tui.t("terminal_ui_form_create_project_title")).SetTitleAlign(tview.AlignLeft)

	var name, description, path, projectType string

	form.AddInputField("Name", "", 30, nil, func(text string) {
		name = text
	})
	form.AddInputField("Description", "", 50, nil, func(text string) {
		description = text
	})
	form.AddInputField("Path", "", 50, nil, func(text string) {
		path = text
	})
	form.AddDropDown("Type", []string{"go", "node", "python", "rust", "generic"}, 0, func(option string, index int) {
		projectType = option
	})

	form.AddButton("Create", func() {
		ctx := context.Background()
		if name == "" || path == "" {
			tui.statusBar.SetText("[red]Name and path are required")
			return
		}

		newProject, err := tui.projectManager.CreateProject(ctx, name, description, path, projectType)
		if err != nil {
			tui.statusBar.SetText("[red]" + tui.td("terminal_ui_project_create_failed", map[string]any{"Error": err.Error()}))
		} else {
			tui.statusBar.SetText("[green]" + tui.td("terminal_ui_project_created", map[string]any{"Name": newProject.Name}))
			tui.showProjects()
		}

		tui.pages.RemovePage("newProjectForm")
		tui.app.SetFocus(tui.content)
	})

	form.AddButton("Cancel", func() {
		tui.pages.RemovePage("newProjectForm")
		tui.app.SetFocus(tui.content)
	})

	modal := tview.NewFlex().
		AddItem(nil, 0, 1, false).
		AddItem(tview.NewFlex().SetDirection(tview.FlexRow).
			AddItem(nil, 0, 1, false).
			AddItem(form, 18, 1, true).
			AddItem(nil, 0, 1, false), 70, 1, true).
		AddItem(nil, 0, 1, false)

	tui.pages.AddPage("newProjectForm", modal, true, true)
	tui.app.SetFocus(form)
}

// truncatePath truncates a path for display
func truncatePath(path string, maxLen int) string {
	if len(path) <= maxLen {
		return path
	}
	return "..." + path[len(path)-maxLen+3:]
}

// showSessions displays the session management interface
func (tui *TerminalUI) showSessions() {
	sessionsView := tview.NewFlex().SetDirection(tview.FlexRow)

	header := tview.NewTextView().
		SetText("[::b]Session Management").
		SetTextAlign(tview.AlignCenter).
		SetDynamicColors(true)
	header.SetBorder(true)

	// Create session table
	sessionTable := tview.NewTable().
		SetBorders(true).
		SetSelectable(true, false)
	sessionTable.SetBorder(true).SetTitle(tui.t("terminal_ui_sessions_table_title"))

	// Set headers
	headers := []string{"Name", "Project", "Mode", "Status", "Duration", "Created"}
	for col, h := range headers {
		sessionTable.SetCell(0, col, tview.NewTableCell(h).
			SetTextColor(tcell.ColorYellow).
			SetAlign(tview.AlignCenter).
			SetSelectable(false))
	}

	// Get sessions from manager
	sessions := tui.sessionManager.GetAll()

	if len(sessions) == 0 {
		sessionTable.SetCell(1, 0, tview.NewTableCell(tui.t("terminal_ui_sessions_none_found")).
			SetAlign(tview.AlignCenter).
			SetSelectable(false))
	} else {
		for row, s := range sessions {
			statusColor := "[white]"
			switch s.Status {
			case session.StatusActive:
				statusColor = "[green]"
			case session.StatusPaused:
				statusColor = "[yellow]"
			case session.StatusCompleted:
				statusColor = "[blue]"
			case session.StatusFailed:
				statusColor = "[red]"
			}

			modeColor := "[white]"
			switch s.Mode {
			case session.ModePlanning:
				modeColor = "[cyan]"
			case session.ModeBuilding:
				modeColor = "[green]"
			case session.ModeTesting:
				modeColor = "[yellow]"
			case session.ModeDebugging:
				modeColor = "[red]"
			case session.ModeRefactoring:
				modeColor = "[magenta]"
			}

			duration := s.Duration
			if s.Status == session.StatusActive && !s.StartedAt.IsZero() {
				duration += time.Since(s.StartedAt)
			}

			sessionTable.SetCell(row+1, 0, tview.NewTableCell(s.Name).SetAlign(tview.AlignLeft))
			sessionTable.SetCell(row+1, 1, tview.NewTableCell(s.ProjectID[:8]+"...").SetAlign(tview.AlignLeft))
			sessionTable.SetCell(row+1, 2, tview.NewTableCell(modeColor+string(s.Mode)).SetAlign(tview.AlignCenter))
			sessionTable.SetCell(row+1, 3, tview.NewTableCell(statusColor+string(s.Status)).SetAlign(tview.AlignCenter))
			sessionTable.SetCell(row+1, 4, tview.NewTableCell(formatDuration(duration)).SetAlign(tview.AlignRight))
			sessionTable.SetCell(row+1, 5, tview.NewTableCell(s.CreatedAt.Format("01-02 15:04")).SetAlign(tview.AlignCenter))
		}
	}

	// Session statistics panel
	statsPanel := tui.createSessionStatsPanel()

	// Handle session selection
	sessionTable.SetSelectedFunc(func(row, col int) {
		if row > 0 && row <= len(sessions) {
			selectedSession := sessions[row-1]
			tui.showSessionActions(selectedSession)
		}
	})

	// Action buttons
	actions := tview.NewFlex().SetDirection(tview.FlexColumn)
	newSessionBtn := tview.NewButton("New Session").SetSelectedFunc(func() {
		tui.showNewSessionForm()
	})
	startBtn := tview.NewButton("Start").SetSelectedFunc(func() {
		row, _ := sessionTable.GetSelection()
		if row > 0 && row <= len(sessions) {
			s := sessions[row-1]
			if err := tui.sessionManager.Start(s.ID); err != nil {
				tui.statusBar.SetText("[red]" + tui.td("terminal_ui_session_start_failed", map[string]any{"Error": err.Error()}))
			} else {
				tui.statusBar.SetText(fmt.Sprintf("[green]Started: %s", s.Name))
				tui.showSessions()
			}
		}
	})
	pauseBtn := tview.NewButton("Pause").SetSelectedFunc(func() {
		row, _ := sessionTable.GetSelection()
		if row > 0 && row <= len(sessions) {
			s := sessions[row-1]
			if err := tui.sessionManager.Pause(s.ID); err != nil {
				tui.statusBar.SetText("[red]" + tui.td("terminal_ui_session_pause_failed", map[string]any{"Error": err.Error()}))
			} else {
				tui.statusBar.SetText(fmt.Sprintf("[yellow]Paused: %s", s.Name))
				tui.showSessions()
			}
		}
	})
	completeBtn := tview.NewButton("Complete").SetSelectedFunc(func() {
		row, _ := sessionTable.GetSelection()
		if row > 0 && row <= len(sessions) {
			s := sessions[row-1]
			if err := tui.sessionManager.Complete(s.ID); err != nil {
				tui.statusBar.SetText("[red]" + tui.td("terminal_ui_session_complete_failed", map[string]any{"Error": err.Error()}))
			} else {
				tui.statusBar.SetText(fmt.Sprintf("[blue]Completed: %s", s.Name))
				tui.showSessions()
			}
		}
	})
	refreshBtn := tview.NewButton("Refresh").SetSelectedFunc(func() {
		tui.showSessions()
	})

	actions.AddItem(newSessionBtn, 0, 1, false)
	actions.AddItem(startBtn, 0, 1, false)
	actions.AddItem(pauseBtn, 0, 1, false)
	actions.AddItem(completeBtn, 0, 1, false)
	actions.AddItem(refreshBtn, 0, 1, false)

	// Main content area
	contentFlex := tview.NewFlex().SetDirection(tview.FlexColumn)
	contentFlex.AddItem(sessionTable, 0, 2, true)
	contentFlex.AddItem(statsPanel, 35, 0, false)

	sessionsView.
		AddItem(header, 3, 0, false).
		AddItem(contentFlex, 0, 1, true).
		AddItem(actions, 3, 0, false)

	tui.content.SwitchToPage("sessions")
	tui.content.AddPage("sessions", sessionsView, true, true)
}

// createSessionStatsPanel creates session statistics panel
func (tui *TerminalUI) createSessionStatsPanel() *tview.TextView {
	panel := tview.NewTextView().SetDynamicColors(true)
	panel.SetBorder(true).SetTitle("Statistics")

	stats := tui.sessionManager.GetStatistics()
	activeSession := tui.sessionManager.GetActive()

	activeInfo := "[gray]None"
	if activeSession != nil {
		activeInfo = fmt.Sprintf("[green]%s[white] (%s)", activeSession.Name, activeSession.Mode)
	}

	content := fmt.Sprintf(`[::b]Session Summary[white]

[green]Total:[white] %d
[green]Active:[white] %d
[yellow]Paused:[white] %d
[blue]Completed:[white] %d
[red]Failed:[white] %d

[::b]Current Session[white]
%s

[::b]Average Duration[white]
%s`,
		stats.Total,
		stats.ByStatus[session.StatusActive],
		stats.ByStatus[session.StatusPaused],
		stats.ByStatus[session.StatusCompleted],
		stats.ByStatus[session.StatusFailed],
		activeInfo,
		formatDuration(stats.AverageDuration))

	panel.SetText(content)
	return panel
}

// showSessionActions shows actions for a specific session
func (tui *TerminalUI) showSessionActions(s *session.Session) {
	modal := tview.NewModal()
	modal.SetText(fmt.Sprintf("Session: %s\nMode: %s\nStatus: %s\nDuration: %s",
		s.Name, s.Mode, s.Status, formatDuration(s.Duration)))

	buttons := []string{"Close"}
	switch s.Status {
	case session.StatusPaused:
		buttons = append([]string{"Start", "Resume"}, buttons...)
	case session.StatusActive:
		buttons = append([]string{"Pause", "Complete"}, buttons...)
	}
	if s.Status != session.StatusActive {
		buttons = append(buttons, "Delete")
	}

	modal.AddButtons(buttons)
	modal.SetDoneFunc(func(buttonIndex int, buttonLabel string) {
		switch buttonLabel {
		case "Start":
			tui.sessionManager.Start(s.ID)
		case "Resume":
			tui.sessionManager.Resume(s.ID)
		case "Pause":
			tui.sessionManager.Pause(s.ID)
		case "Complete":
			tui.sessionManager.Complete(s.ID)
		case "Delete":
			tui.sessionManager.Delete(s.ID)
		}
		tui.pages.RemovePage("sessionActions")
		tui.showSessions()
	})

	tui.pages.AddPage("sessionActions", modal, true, true)
}

// showNewSessionForm displays a form for creating a new session
func (tui *TerminalUI) showNewSessionForm() {
	form := tview.NewForm()
	form.SetBorder(true).SetTitle(tui.t("terminal_ui_form_create_session_title")).SetTitleAlign(tview.AlignLeft)

	var name, description string
	var mode session.Mode = session.ModePlanning

	// Get active project for default project ID
	ctx := context.Background()
	activeProject, _ := tui.projectManager.GetActiveProject(ctx)
	projectID := ""
	if activeProject != nil {
		projectID = activeProject.ID
	}

	form.AddInputField("Name", "", 30, nil, func(text string) {
		name = text
	})
	form.AddInputField("Description", "", 50, nil, func(text string) {
		description = text
	})
	form.AddInputField("Project ID", projectID, 40, nil, func(text string) {
		projectID = text
	})
	form.AddDropDown("Mode", []string{
		string(session.ModePlanning),
		string(session.ModeBuilding),
		string(session.ModeTesting),
		string(session.ModeRefactoring),
		string(session.ModeDebugging),
		string(session.ModeDeployment),
	}, 0, func(option string, index int) {
		mode = session.Mode(option)
	})

	form.AddButton("Create", func() {
		if name == "" {
			tui.statusBar.SetText("[red]Session name is required")
			return
		}
		if projectID == "" {
			tui.statusBar.SetText("[red]Project ID is required (create or select a project first)")
			return
		}

		newSession, err := tui.sessionManager.Create(projectID, name, description, mode)
		if err != nil {
			tui.statusBar.SetText("[red]" + tui.td("terminal_ui_session_create_failed", map[string]any{"Error": err.Error()}))
		} else {
			tui.statusBar.SetText("[green]" + tui.td("terminal_ui_session_created", map[string]any{"Name": newSession.Name}))
			tui.showSessions()
		}

		tui.pages.RemovePage("newSessionForm")
		tui.app.SetFocus(tui.content)
	})

	form.AddButton("Cancel", func() {
		tui.pages.RemovePage("newSessionForm")
		tui.app.SetFocus(tui.content)
	})

	modal := tview.NewFlex().
		AddItem(nil, 0, 1, false).
		AddItem(tview.NewFlex().SetDirection(tview.FlexRow).
			AddItem(nil, 0, 1, false).
			AddItem(form, 18, 1, true).
			AddItem(nil, 0, 1, false), 70, 1, true).
		AddItem(nil, 0, 1, false)

	tui.pages.AddPage("newSessionForm", modal, true, true)
	tui.app.SetFocus(form)
}

// formatDuration formats a duration for display
func formatDuration(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm %ds", int(d.Minutes()), int(d.Seconds())%60)
	}
	return fmt.Sprintf("%dh %dm", int(d.Hours()), int(d.Minutes())%60)
}

// showLLM displays the LLM interaction interface
func (tui *TerminalUI) showLLM() {
	llmView := tview.NewFlex().SetDirection(tview.FlexRow)

	header := tview.NewTextView().
		SetText("[::b]AI Model Interaction").
		SetTextAlign(tview.AlignCenter).
		SetDynamicColors(true)
	header.SetBorder(true)

	// Main content area - split between chat and info panels
	mainContent := tview.NewFlex().SetDirection(tview.FlexColumn)

	// Chat area
	chatFlex := tview.NewFlex().SetDirection(tview.FlexRow)

	// Chat output/history
	tui.chatOutput = tview.NewTextView().
		SetDynamicColors(true).
		SetScrollable(true).
		SetWrap(true)
	tui.chatOutput.SetBorder(true).SetTitle("Chat")
	tui.chatOutput.SetText(tui.formatChatHistory())

	// Chat input
	tui.chatInput = tview.NewInputField().
		SetLabel("You: ").
		SetFieldWidth(0).
		SetFieldBackgroundColor(tcell.ColorDarkSlateGray)
	tui.chatInput.SetBorder(true).SetTitle("Message")

	// Handle input submission
	tui.chatInput.SetDoneFunc(func(key tcell.Key) {
		if key == tcell.KeyEnter {
			message := tui.chatInput.GetText()
			if message != "" {
				tui.sendChatMessage(message)
				tui.chatInput.SetText("")
			}
		}
	})

	chatFlex.
		AddItem(tui.chatOutput, 0, 1, false).
		AddItem(tui.chatInput, 3, 0, true)

	// Info panel
	infoPanel := tui.createLLMInfoPanel()

	mainContent.
		AddItem(chatFlex, 0, 2, true).
		AddItem(infoPanel, 40, 0, false)

	// Action buttons
	actions := tview.NewFlex().SetDirection(tview.FlexColumn)
	selectModelBtn := tview.NewButton("Select Model").SetSelectedFunc(func() {
		tui.showModelSelector()
	})
	clearChatBtn := tview.NewButton("Clear Chat").SetSelectedFunc(func() {
		tui.chatHistory = make([]llm.Message, 0)
		tui.chatOutput.SetText(tui.formatChatHistory())
		tui.statusBar.SetText("[green]Chat cleared")
	})
	settingsBtn := tview.NewButton("Settings").SetSelectedFunc(func() {
		tui.showLLMSettings()
	})
	actions.AddItem(selectModelBtn, 0, 1, false)
	actions.AddItem(clearChatBtn, 0, 1, false)
	actions.AddItem(settingsBtn, 0, 1, false)

	llmView.
		AddItem(header, 3, 0, false).
		AddItem(mainContent, 0, 1, true).
		AddItem(actions, 3, 0, false)

	tui.content.AddPage("llm", llmView, true, true)
	tui.content.SwitchToPage("llm")
	// Give the chat input app-level focus so typed text (prompts, /model) goes
	// to it — otherwise menu-letters in the text leak to the global hotkey nav.
	tui.app.SetFocus(tui.chatInput)
}

// createLLMInfoPanel creates the LLM information panel
func (tui *TerminalUI) createLLMInfoPanel() *tview.TextView {
	panel := tview.NewTextView().SetDynamicColors(true)
	panel.SetBorder(true).SetTitle("LLM Info")

	// Get available models
	models := tui.llmManager.GetAvailableModels()
	modelCount := len(models)

	// Get provider status
	ctx := context.Background()
	health := tui.llmManager.HealthCheck(ctx)
	healthyProviders := 0
	for _, h := range health {
		if h.Status == "healthy" {
			healthyProviders++
		}
	}

	currentModel := "Not selected"
	if tui.llmProvider != nil {
		currentModel = tui.llmProvider.GetName()
	}

	content := fmt.Sprintf(tui.t("terminal_ui_chat_dashboard_content_fmt"),
		currentModel,
		healthyProviders,
		len(health)-healthyProviders,
		modelCount,
		len(tui.chatHistory))

	panel.SetText(content)
	return panel
}

// formatChatHistory formats the chat history for display
func (tui *TerminalUI) formatChatHistory() string {
	if len(tui.chatHistory) == 0 {
		return tui.t("terminal_ui_chat_welcome_body")
	}

	var sb strings.Builder
	for _, msg := range tui.chatHistory {
		switch msg.Role {
		case "user":
			sb.WriteString(fmt.Sprintf("[green]You:[white] %s\n\n", msg.Content))
		case "assistant":
			sb.WriteString(fmt.Sprintf("[cyan]AI:[white] %s\n\n", msg.Content))
		case "system":
			sb.WriteString(fmt.Sprintf("[yellow]System:[white] %s\n\n", msg.Content))
		}
	}
	return sb.String()
}

// sendChatMessage sends a message to the LLM
func (tui *TerminalUI) sendChatMessage(message string) {
	// Handle commands
	if strings.HasPrefix(message, "/") {
		tui.handleChatCommand(message)
		return
	}

	// Add user message to history
	tui.chatHistory = append(tui.chatHistory, llm.Message{
		Role:    "user",
		Content: message,
	})

	// Durable memory: persist the user turn (write-through to HelixMemory) AND
	// recall any prior remembered context relevant to this message, prepending
	// it as a system turn so a fact stated in a PREVIOUS session surfaces in the
	// model's context now. No-op when durable memory is unavailable.
	if tui.memoryManager != nil {
		tui.persistChatTurn(memory.RoleUser, message)
		recallCtx, recallCancel := context.WithTimeout(context.Background(), 5*time.Second)
		if recall, rErr := tui.memoryManager.RecallContext(recallCtx, message, 5); rErr == nil && strings.TrimSpace(recall) != "" {
			// Insert recalled context as a system turn BEFORE the user message so
			// the model sees the remembered fact without it polluting the visible
			// transcript ordering (it is appended to chatHistory; the model reads
			// the full slice).
			tui.chatHistory = append(tui.chatHistory, llm.Message{
				Role:    "system",
				Content: recall,
			})
		}
		recallCancel()
	}

	// Update display
	tui.chatOutput.SetText(tui.formatChatHistory())
	tui.chatOutput.ScrollToEnd()

	// Capability dispatch BEFORE the LLM call (after the /-command check):
	//
	//   (a) Plugin invocation — `@plugin:<name> <action> <args>`. When the
	//       prompt matches the syntax, MaybeRunPlugin executes the plugin's
	//       real entrypoint (os/exec, no simulation) and we render its output
	//       as the assistant turn and RETURN — the LLM is skipped entirely.
	//       A matched-but-failed invocation (ran=true, err!=nil) surfaces the
	//       error as the assistant turn rather than silently sending it to the
	//       LLM.
	//
	//   (b) Skill trigger — a `.md` skill whose trigger regex matches the
	//       prompt. DispatchSkill renders the skill body; we REPLACE the
	//       outgoing prompt sent to the LLM with that rendered body (note it in
	//       the chat) and continue to the LLM. The user's original message stays
	//       visible in history; only what the model receives changes.
	if tui.pluginLoader != nil {
		if out, ran, plErr := plugins.MaybeRunPlugin(context.Background(), tui.pluginLoader, message); ran {
			content := out
			if plErr != nil {
				content = fmt.Sprintf("[plugin error: %v]", plErr)
			}
			tui.chatHistory = append(tui.chatHistory, llm.Message{
				Role:    "assistant",
				Content: content,
			})
			tui.chatOutput.SetText(tui.formatChatHistory())
			tui.chatOutput.ScrollToEnd()
			tui.statusBar.SetText("[green]" + tui.t("terminal_ui_status_bar_default"))
			return
		}
	}

	// llmPrompt is what the model actually receives — the user's message by
	// default, or the rendered skill body when a skill trigger matches.
	llmPrompt := message
	if tui.skillDispatcher != nil {
		if rendered, matched := agent.DispatchSkill(tui.skillDispatcher, message); matched {
			llmPrompt = rendered
			tui.chatHistory = append(tui.chatHistory, llm.Message{
				Role:    "system",
				Content: tui.t("terminal_ui_chat_skill_applied"),
			})
			tui.chatOutput.SetText(tui.formatChatHistory())
			tui.chatOutput.ScrollToEnd()
		}
	}

	// Check if provider is available
	if tui.llmProvider == nil {
		tui.chatHistory = append(tui.chatHistory, llm.Message{
			Role:    "assistant",
			Content: tui.t("terminal_ui_chat_no_provider_error"),
		})
		tui.chatOutput.SetText(tui.formatChatHistory())
		tui.chatOutput.ScrollToEnd()
		tui.statusBar.SetText("[red]" + tui.t("terminal_ui_chat_no_provider_status"))
		return
	}

	// Send to LLM provider.
	//
	// P1-T07 (speed programme Phase 1): the TUI consumes the streaming
	// provider API (GenerateStream) instead of buffering the whole response
	// via Generate. An empty assistant turn is appended up-front and its
	// Content is grown chunk-by-chunk as tokens arrive — so the user sees
	// token-by-token output (time-to-first-visible-token) rather than a
	// frozen UI until the completion lands.
	//
	// Threading: the provider call runs in its own goroutine because
	// sendChatMessage executes on the tview event loop (SetDoneFunc); a
	// blocking provider call there would freeze the whole UI. Every
	// chatHistory mutation + redraw is funnelled through QueueUpdateDraw so
	// it is applied on the event loop (tview is not goroutine-safe).
	//
	// No-regression: the assistant turn's final Content is the concatenation
	// of every streamed chunk — byte-identical to the buffered Generate
	// result for any conformant provider. Only WHEN the text appears changes.
	ctx := context.Background()
	// Snapshot the conversation the model receives. When a skill matched,
	// llmPrompt holds the rendered skill body — substitute it for the user's
	// last (raw) message in the model-facing snapshot so the model acts on the
	// rendered instruction, while the user's original text stays visible in the
	// on-screen chatHistory. When no skill matched, llmPrompt == message, so the
	// substitution is a no-op.
	modelMessages := append([]llm.Message(nil), tui.chatHistory...)
	if llmPrompt != message {
		for i := len(modelMessages) - 1; i >= 0; i-- {
			if modelMessages[i].Role == "user" {
				modelMessages[i].Content = llmPrompt
				break
			}
		}
	}
	request := &llm.LLMRequest{
		ID:          uuid.New(),
		Model:       tui.selectedModel,
		Messages:    modelMessages,
		MaxTokens:   2048,
		Temperature: 0.7,
		Stream:      true,
	}

	// NOTE: do NOT call tui.app.Draw() here. sendChatMessage runs on the tview
	// event loop (it is invoked from the chatInput SetDoneFunc handler). The
	// synchronous Application.Draw() posts a draw to the event queue and blocks
	// until it is serviced — but the loop cannot service it until this handler
	// returns, so the call DEADLOCKS the handler (silently: tview does not
	// recover a block, so the prompt never submits, the input never clears, and
	// "Messages" never increments — exactly the headless-Enter bug). The
	// just-set status + chat output are painted by the loop's redraw after this
	// handler returns; every subsequent streamed update is funnelled through
	// QueueUpdateDraw below, which schedules its own redraw. No Draw() needed.
	tui.statusBar.SetText("[yellow]" + tui.t("terminal_ui_chat_generating"))

	// Append the placeholder assistant turn the stream will grow in place.
	tui.chatHistory = append(tui.chatHistory, llm.Message{Role: "assistant", Content: ""})
	assistantIdx := len(tui.chatHistory) - 1
	provider := tui.llmProvider

	// history is the conversation up to and including the user turn — i.e.
	// WITHOUT the empty placeholder assistant turn just appended. request.Messages
	// was snapshotted before the placeholder, so it is exactly that prefix.
	history := request.Messages

	// Agentic path: when the read-only tool registry is wired, drive the chat
	// turn through the multi-turn tool loop so prompts like "Check git status"
	// actually execute a read-only git tool and the operator SEES the tool
	// trace + (for ensemble responses) every member's answer and the vote.
	if tui.toolRegistry != nil {
		registry := tui.toolRegistry
		systemPrompt := buildToolLoopSystemPrompt(registry)
		go func() {
			// onFinalChunk streams the FINAL (tool-call-free) answer
			// token-by-token onto the placeholder assistant turn the instant
			// each chunk arrives — mirroring the plain consumeChatStream path
			// below. Every chunk grows assistantIdx's Content and is funnelled
			// through QueueUpdateDraw (tview is not goroutine-safe). After the
			// stream completes, the guarded result.FinalContent (byte-identical
			// to the accumulated chunks for a conformant provider) is set on the
			// turn, and the trace + ensemble panel render after.
			onFinalChunk := func(chunk string) {
				tui.app.QueueUpdateDraw(func() {
					tui.chatHistory[assistantIdx].Content += chunk
					tui.chatOutput.SetText(tui.formatChatHistory())
					tui.chatOutput.ScrollToEnd()
				})
			}
			result, loopErr := agent.RunToolLoopStream(ctx, provider, registry, history, agent.ToolLoopOptions{
				Model:        tui.selectedModel,
				MaxTurns:     6,
				SystemPrompt: systemPrompt,
				// Bound each tool result fed back into the conversation so a deep
				// multi-tool investigation (many fs_read/grep/git_status results
				// across turns) cannot overflow the SMALLEST member's context window
				// — the ensemble fans to free-tier members, one of which may carry
				// only an 8K context. The display trace keeps its own short excerpt.
				MaxToolResultChars: 800,
				// SAFETY (§11.4.133): the TUI registry is built with
				// tools.NewToolRegistry(nil) — no approval manager wired, so the
				// registry's applyApprovalGate would let EVERY tool through
				// (including fs_write/fs_edit/shell). This unattended loop must
				// never reach a write/shell tool, so restrict it to read-only
				// tools at both the offer and execute layers.
				ReadOnlyOnly: true,
			}, onFinalChunk)
			tui.app.QueueUpdateDraw(func() {
				if loopErr != nil {
					tui.chatHistory[assistantIdx].Content = fmt.Sprintf("[Error: %v]", loopErr)
					tui.statusBar.SetText(fmt.Sprintf("[red]Error: %v", loopErr))
					tui.chatOutput.SetText(tui.formatChatHistory())
					tui.chatOutput.ScrollToEnd()
					return
				}

				// Set the final answer on the placeholder assistant turn. Guard
				// against an empty/whitespace FinalContent: an empty assistant
				// turn left in chatHistory breaks the NEXT provider request for
				// strict providers (HelixAgent rejects an assistant message with
				// no content and no tool_calls), 400ing the whole conversation.
				// RunToolLoop already guarantees non-empty FinalContent; this is
				// defense-in-depth so the stored history is always well-formed.
				finalAnswer := result.FinalContent
				if strings.TrimSpace(finalAnswer) == "" {
					finalAnswer = tui.td("terminal_ui_chat_no_answer", nil)
					if strings.TrimSpace(finalAnswer) == "" {
						finalAnswer = "(no answer produced)"
					}
				}
				tui.chatHistory[assistantIdx].Content = finalAnswer

				// Durable memory: persist the assistant answer so it is recalled
				// in future sessions (write-through to HelixMemory).
				tui.persistChatTurn(memory.RoleAssistant, finalAnswer)

				// Surface the agentic tool trace so the operator SEES each
				// tool call ("tool: git_status … <real output>").
				if len(result.Trace) > 0 {
					if traceLines := FormatToolTrace(adaptToolTrace(result.Trace)); len(traceLines) > 0 {
						tui.chatHistory = append(tui.chatHistory, llm.Message{
							Role:    "assistant",
							Content: strings.Join(traceLines, "\n"),
						})
					}
				}

				// Surface the ensemble panel (empty for non-ensemble responses)
				// so the operator SEES every member + the winning vote.
				if panelLines := FormatEnsemblePanel(result.FinalMetadata); len(panelLines) > 0 {
					tui.chatHistory = append(tui.chatHistory, llm.Message{
						Role:    "assistant",
						Content: strings.Join(panelLines, "\n"),
					})
				}

				tui.statusBar.SetText("[green]" + tui.td("terminal_ui_chat_response_received", map[string]any{"Tokens": 0}))
				tui.chatOutput.SetText(tui.formatChatHistory())
				tui.chatOutput.ScrollToEnd()
			})
		}()
		return
	}

	// Plain streaming path (registry nil) — unchanged, no regression.
	go func() {
		streamErr, totalTokens := consumeChatStream(ctx, provider, request, func(content string) {
			// onChunk: applied on the tview event loop (tview is not
			// goroutine-safe) so the assistant turn grows visibly.
			tui.app.QueueUpdateDraw(func() {
				tui.chatHistory[assistantIdx].Content += content
				tui.chatOutput.SetText(tui.formatChatHistory())
				tui.chatOutput.ScrollToEnd()
			})
		})
		tui.app.QueueUpdateDraw(func() {
			if streamErr != nil {
				tui.chatHistory[assistantIdx].Content = fmt.Sprintf("[Error: %v]", streamErr)
				tui.statusBar.SetText(fmt.Sprintf("[red]Error: %v", streamErr))
			} else {
				// Durable memory: persist the streamed assistant answer.
				tui.persistChatTurn(memory.RoleAssistant, tui.chatHistory[assistantIdx].Content)
				// Accumulate the REAL per-session token total from the provider's
				// streaming telemetry (consumeChatStream returns Usage.TotalTokens
				// from the telemetry-carrying chunk; 0 when the provider reported
				// none this turn — then the running total simply does not grow).
				tui.sessionUsedTokens += totalTokens
				status := "[green]" + tui.td("terminal_ui_chat_response_received", map[string]any{"Tokens": totalTokens})
				// Append the honest context-window USED-% indicator when the active
				// model's REAL window is known; omitted entirely otherwise (CONST-035).
				if usage := tui.contextUsageStatus(); usage != "" {
					status += "  " + usage
				}
				tui.statusBar.SetText(status)
			}
			tui.chatOutput.SetText(tui.formatChatHistory())
			tui.chatOutput.ScrollToEnd()
		})
	}()
}

// persistChatTurn write-throughs a single chat turn to durable memory so it
// survives a process restart. It is a no-op when durable memory is unavailable
// (honest degrade — never a fake "remembered"). Empty content is skipped.
func (tui *TerminalUI) persistChatTurn(role memory.Role, content string) {
	if tui.memoryManager == nil || tui.memoryConvID == "" || strings.TrimSpace(content) == "" {
		return
	}
	_ = tui.memoryManager.AddMessage(tui.memoryConvID, memory.NewMessage(role, content))
}

// closeMemory checkpoints + releases the durable memory store. Called on TUI
// shutdown so the on-disk DB is folded back from its WAL sidecars.
func (tui *TerminalUI) closeMemory() {
	if tui.memoryProvider != nil {
		_ = tui.memoryProvider.Close()
	}
}

// adaptToolTrace converts the agent loop's []agent.ToolTraceEntry into the
// terminal_ui-local []ToolTraceLine that FormatToolTrace consumes, so this
// package never has to expose internal/agent's type to the render helper.
func adaptToolTrace(entries []agent.ToolTraceEntry) []ToolTraceLine {
	out := make([]ToolTraceLine, len(entries))
	for i, e := range entries {
		out[i] = ToolTraceLine{
			ToolName:  e.ToolName,
			Output:    e.Output,
			Err:       e.Err,
			Arguments: e.Arguments,
		}
	}
	return out
}

// resolveRepoRoot walks up from start looking for a directory containing a
// .git entry and returns it (the enclosing git repository root). When no .git
// is found on the way to the filesystem root, it returns start unchanged — so
// git_status still operates on the working directory as a sensible fallback.
func resolveRepoRoot(start string) string {
	dir := start
	for {
		if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return start
		}
		dir = parent
	}
}

// buildToolLoopSystemPrompt composes a SHORT structural system prompt from the
// registry's live tool names (CONST-046: metadata-composed, not a hardcoded
// catalogue). The tool-name list adapts to whatever is actually registered.
func buildToolLoopSystemPrompt(registry *tools.ToolRegistry) string {
	names := make([]string, 0)
	for _, t := range registry.List() {
		names = append(names, t.Name())
	}
	sort.Strings(names)
	// The prompt is structural agent-steering composed from the live tool names
	// (CONST-046 — not hardcoded user-facing content). It asserts the agent is
	// operating INSIDE the user's real codebase and REQUIRES a tool call before
	// any claim about the codebase, so a model can never answer "I cannot see
	// your files" from memory — it has genuine read access via these tools.
	return "You are the Helix coding agent, operating INSIDE the user's real codebase at the current working directory. " +
		"You have these tools available: " + strings.Join(names, ", ") + ". " +
		"These tools give you genuine read access to the user's files and git state — you CAN see the codebase. " +
		"When the user asks whether you can see or access their codebase (or anything about its files, structure, or git state), " +
		"you MUST call a tool FIRST (e.g. glob to list files, git_status to inspect the repo, fs_read to read a file) and then " +
		"answer from what the tool actually returned — concretely (how many files, which languages, the repository's state). " +
		"NEVER claim you cannot see or access the codebase without first calling a tool: you have both the tools and the access. " +
		"Prefer calling a tool over guessing."
}

// consumeChatStream drives one TUI chat turn over the provider's streaming
// API (P1-T07, speed programme Phase 1).
//
// It calls provider.GenerateStream and invokes onChunk for every non-empty
// chunk the instant it arrives — so the caller can render token-by-token. The
// total token count from the telemetry-carrying chunk and any provider error
// are returned for the post-turn status line.
//
// Extracted from sendChatMessage so the chunk-consumption loop is unit-testable
// without standing up a live tview event loop: a test supplies a fake provider
// + a recording onChunk and asserts N chunks produce N incremental callbacks.
//
// Channel-close robustness: the llm.Provider streaming contract is not uniform
// (Anthropic/OpenAI/Groq/DeepSeek close the channel from GenerateStream;
// Ollama and the OpenAI-compatible provider do not). The consumer therefore
// selects on BOTH the chunk channel AND the provider's return signal so it
// terminates for either provider family — a naive `for range` would deadlock
// against the non-closing providers.
//
// No-regression: the sum of every onChunk argument is the byte-exact
// concatenation of the streamed chunks — identical to the buffered Generate
// result for any conformant provider. Only WHEN the text appears changes.
func consumeChatStream(ctx context.Context, provider llm.Provider, request *llm.LLMRequest, onChunk func(content string)) (streamErr error, totalTokens int) {
	chunkChan := make(chan llm.LLMResponse, 100)
	errCh := make(chan error, 1)
	go func() { errCh <- provider.GenerateStream(ctx, request, chunkChan) }()

	render := func(chunk llm.LLMResponse) {
		if chunk.Usage.TotalTokens > 0 {
			totalTokens = chunk.Usage.TotalTokens
		}
		if chunk.Content != "" {
			onChunk(chunk.Content)
		}
	}
	streamErr = drainProviderStream(chunkChan, errCh, render)
	return streamErr, totalTokens
}

// drainProviderStream consumes every chunk a provider's GenerateStream emits
// onto chunkChan, invoking onChunk for each, and returns the provider's error.
//
// It copes with the non-uniform channel-close contract across llm.Provider
// implementations: some close chunkChan from inside GenerateStream, some return
// without closing. It selects on both chunkChan and errCh — on channel close it
// joins errCh; on an errCh send it drains the remaining buffered chunks
// non-blockingly and returns. Terminates for both provider families.
func drainProviderStream(chunkChan chan llm.LLMResponse, errCh chan error, onChunk func(llm.LLMResponse)) error {
	for {
		select {
		case chunk, ok := <-chunkChan:
			if !ok {
				return <-errCh
			}
			onChunk(chunk)
		case provErr := <-errCh:
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

// formatContextUsage renders the context-window USED-% indicator as a pure
// function so it is unit-testable in isolation. It mirrors the proven CLI
// implementation (cmd/cli/main.go formatContextUsage) byte-for-byte in policy:
//
//   - When window <= 0 (the model's context-window size is NOT reliably known
//     for the active model) it returns "" — the caller OMITS the indicator
//     rather than rendering a fabricated percentage against a guessed window.
//     This is the honest-conditional mandated by CONST-035: no fake denominator.
//   - When window > 0 it returns "context: <used>/<window> (NN%)" where NN is the
//     integer percentage of used/window. used is clamped to >= 0; the percentage
//     is allowed to exceed 100 (an honest signal that the reported token total
//     ran past the catalogue window) rather than being silently capped, but never
//     goes negative.
//
// The TUI prefixes/joins this fragment onto the status line; the leading-spaces
// padding the CLI uses for terminal alignment is intentionally omitted here so
// the indicator composes cleanly with the status-bar i18n string.
func formatContextUsage(used, window int) string {
	if window <= 0 {
		return ""
	}
	if used < 0 {
		used = 0
	}
	pct := used * 100 / window
	// Pure language-neutral numeric body (CONST-046 metadata-composed); the
	// localisable "Context:" label is supplied by the i18n bundle key via
	// contextUsageStatus, not hardcoded here.
	return fmt.Sprintf("%d/%d (%d%%)", used, window, pct)
}

// contextWindowForModel resolves the REAL context-window size (in tokens) for
// the named model, mirroring cmd/cli/main.go contextWindowForModel. It returns
// 0 when no real window is reachable — in which case the caller honestly omits
// the indicator (CONST-035: never substitute a guessed window).
//
// Resolution order, both REAL sources (no hardcoded fallback):
//  1. The provider's model catalogue (ModelInfo.ContextSize) matched by Name
//     then ID, mirroring how the model id is resolved on the request path — the
//     most precise per-model figure.
//  2. The provider's GetContextWindow() — the active model's window reported by
//     the provider — used when the catalogue has no matching entry or it reports
//     0 for that model.
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

// contextUsageStatus composes the honest context-window USED-% status fragment
// for the active provider/model from the REAL accumulated session token total.
// It returns "" when the window is unknown (formatContextUsage's honest-omit) so
// the caller can append it conditionally without ever showing a fabricated
// denominator. The string is i18n-routed (CONST-046) via tui.td so non-English
// operators see a localised label; the numeric body is composed from runtime
// data per CONST-046's metadata-composed allowance.
func (tui *TerminalUI) contextUsageStatus() string {
	window := contextWindowForModel(tui.llmProvider, tui.selectedModel)
	body := formatContextUsage(tui.sessionUsedTokens, window)
	if body == "" {
		return ""
	}
	return tui.td("terminal_ui_chat_context_usage", map[string]any{"Usage": body})
}

// handleChatCommand handles chat commands
func (tui *TerminalUI) handleChatCommand(cmd string) {
	switch {
	case cmd == "/help":
		tui.chatHistory = append(tui.chatHistory, llm.Message{
			Role:    "system",
			Content: tui.t("terminal_ui_chat_help_commands_body"),
		})
	case cmd == "/clear":
		tui.chatHistory = make([]llm.Message, 0)
		tui.statusBar.SetText("[green]Chat cleared")
	case cmd == "/model":
		tui.showModelSelector()
		return
	case strings.HasPrefix(cmd, "/specify"):
		// Real speckit Specify phase against tui.llmProvider (mirrors
		// cmd/cli handleSpecify). Argument = everything after the verb.
		tui.handleSpecifyCommand(strings.TrimSpace(strings.TrimPrefix(cmd, "/specify")))
		return
	case strings.HasPrefix(cmd, "/debate"):
		// Real debate orchestrator against tui.llmProvider (mirrors
		// cmd/cli handleDebate).
		tui.handleDebateCommand(strings.TrimSpace(strings.TrimPrefix(cmd, "/debate")))
		return
	case cmd == "/info":
		info := tui.t("terminal_ui_chat_info_no_model")
		if tui.llmProvider != nil {
			info = tui.td("terminal_ui_chat_info_provider", map[string]any{
				"Provider":   tui.llmProvider.GetName(),
				"ModelCount": len(tui.llmProvider.GetModels()),
			})
		}
		tui.chatHistory = append(tui.chatHistory, llm.Message{
			Role:    "system",
			Content: info,
		})
	case strings.HasPrefix(cmd, "/system "):
		systemPrompt := strings.TrimPrefix(cmd, "/system ")
		// Prepend system message to chat history
		tui.chatHistory = append([]llm.Message{{Role: "system", Content: systemPrompt}}, tui.chatHistory...)
		tui.statusBar.SetText("[green]System prompt set")
	default:
		tui.chatHistory = append(tui.chatHistory, llm.Message{
			Role:    "system",
			Content: tui.td("terminal_ui_chat_unknown_command", map[string]any{"Command": cmd}),
		})
	}

	tui.chatOutput.SetText(tui.formatChatHistory())
	tui.chatOutput.ScrollToEnd()
}

// showModelSelector displays a modal for selecting LLM models
func (tui *TerminalUI) showModelSelector() {
	list := tview.NewList()
	list.SetBorder(true).SetTitle("Select Model")

	// Get available models. Sort deterministically (provider, then name) so the
	// picker order — and its 1-9 digit shortcuts — are stable across runs
	// (GetAvailableModels iterates a map, so order would otherwise vary).
	models := tui.llmManager.GetAvailableModels()
	sort.Slice(models, func(i, j int) bool {
		// The Helix Agent ensemble is the flagship "model" (it fans every prompt
		// across all configured providers), so it always sorts FIRST — it keeps
		// the digit-1 shortcut even as many providers from ~/api_keys.sh push the
		// alphabetical list past the 9 digit-selectable slots. The HelixAgent
		// provider (HelixAgent's own full-capacity 25-provider engine + ensemble,
		// reached over its REST server) sorts SECOND — right after the local
		// ensemble — so its logical models ("helixagent-llm" / "helixagent-
		// ensemble") land on low digits (2,3) and stay reliably digit-selectable.
		// Tiers are compared by provider TYPE (not hardcoded model names) so this
		// stays CONST-046-safe. Then deterministic (provider, name) order for
		// everything else (stable picker shortcuts).
		const helixAgentProviderType llm.ProviderType = "helixagent"
		rank := func(m *llm.ModelInfo) int {
			switch m.Provider {
			case llm.ProviderTypeEnsemble:
				return 0
			case helixAgentProviderType:
				return 1
			default:
				return 2
			}
		}
		if ri, rj := rank(models[i]), rank(models[j]); ri != rj {
			return ri < rj
		}
		if models[i].Provider != models[j].Provider {
			return models[i].Provider < models[j].Provider
		}
		return models[i].Name < models[j].Name
	})

	if len(models) == 0 {
		list.AddItem(tui.t("terminal_ui_models_none_available"), tui.t("terminal_ui_models_configure_hint"), 0, nil)
		list.AddItem(tui.t("terminal_ui_models_configure_ollama"), tui.t("terminal_ui_models_ollama_desc"), 'o', func() {
			tui.statusBar.SetText(tui.t("terminal_ui_status_configure_ollama_hint"))
			tui.pages.RemovePage("modelSelector")
		})
	} else {
		for i, model := range models {
			shortcut := rune('1' + i)
			if i >= 9 {
				shortcut = 0
			}
			modelInfo := model
			list.AddItem(model.Name, fmt.Sprintf("Provider: %s, Context: %d", model.Provider, model.ContextSize), shortcut, func() {
				tui.selectModel(modelInfo)
				tui.pages.RemovePage("modelSelector")
				if tui.chatInput != nil {
					tui.app.SetFocus(tui.chatInput)
				}
			})
		}
	}

	list.AddItem("Cancel", "Return to chat", 'c', func() {
		tui.pages.RemovePage("modelSelector")
	})

	modal := tview.NewFlex().
		AddItem(nil, 0, 1, false).
		AddItem(tview.NewFlex().SetDirection(tview.FlexRow).
			AddItem(nil, 0, 1, false).
			AddItem(list, 20, 1, true).
			AddItem(nil, 0, 1, false), 60, 1, true).
		AddItem(nil, 0, 1, false)

	tui.pages.AddPage("modelSelector", modal, true, true)
	tui.app.SetFocus(list)
}

// selectModel selects an LLM model
func (tui *TerminalUI) selectModel(model *llm.ModelInfo) {
	ctx := context.Background()
	provider, err := tui.llmManager.GetProviderForModel(model.Name, model.Provider)
	if err != nil {
		tui.statusBar.SetText("[red]" + tui.td("terminal_ui_provider_get_failed", map[string]any{"Error": err.Error()}))
		return
	}

	tui.llmProvider = provider
	tui.selectedModel = model.Name
	tui.statusBar.SetText("[green]" + tui.td("terminal_ui_model_selected", map[string]any{"Name": model.Name, "Provider": model.Provider}))

	// Warm the ensemble's per-member working-model cache the moment it is selected
	// (a few seconds before the user types + submits), so the FIRST real prompt
	// hits the cached working model (1 call/member) instead of triggering the
	// cold-start discovery storm that made prompt-1 slow and caused concurrent
	// "all N member(s) failed" in the TUI. Non-blocking (its own goroutine) and a
	// no-op when the selected provider is not the ensemble. WarmCache is idempotent
	// and panic-free, so a failed type assertion is simply skipped.
	if provider.GetType() == llm.ProviderTypeEnsemble {
		if ew, ok := provider.(*llm.EnsembleProvider); ok {
			go ew.WarmCache(context.Background())
		}
	}

	// Add system message about model selection
	tui.chatHistory = append(tui.chatHistory, llm.Message{
		Role:    "system",
		Content: tui.td("terminal_ui_chat_model_changed", map[string]any{"Name": model.Name, "Provider": model.Provider}),
	})
	tui.chatOutput.SetText(tui.formatChatHistory())

	// Refresh LLM view if currently showing
	if provider.IsAvailable(ctx) {
		tui.showLLM()
	}
}

// showLLMSettings displays LLM configuration settings
func (tui *TerminalUI) showLLMSettings() {
	form := tview.NewForm()
	form.SetBorder(true).SetTitle("LLM Settings").SetTitleAlign(tview.AlignLeft)

	var temperature float64 = 0.7
	var maxTokens int = 2048
	var systemPrompt string

	form.AddInputField("Temperature", "0.7", 10, nil, func(text string) {
		fmt.Sscanf(text, "%f", &temperature)
	})
	form.AddInputField("Max Tokens", "2048", 10, tview.InputFieldInteger, func(text string) {
		fmt.Sscanf(text, "%d", &maxTokens)
	})
	form.AddInputField("System Prompt", "", 50, nil, func(text string) {
		systemPrompt = text
	})

	form.AddButton("Apply", func() {
		if systemPrompt != "" {
			// Add system prompt to beginning of chat
			tui.chatHistory = append([]llm.Message{{Role: "system", Content: systemPrompt}}, tui.chatHistory...)
		}
		tui.statusBar.SetText("[green]" + tui.td("terminal_ui_settings_applied", map[string]any{
			"Temperature": fmt.Sprintf("%.2f", temperature),
			"MaxTokens":   maxTokens,
		}))
		tui.pages.RemovePage("llmSettings")
		tui.showLLM()
	})

	form.AddButton("Cancel", func() {
		tui.pages.RemovePage("llmSettings")
	})

	modal := tview.NewFlex().
		AddItem(nil, 0, 1, false).
		AddItem(tview.NewFlex().SetDirection(tview.FlexRow).
			AddItem(nil, 0, 1, false).
			AddItem(form, 16, 1, true).
			AddItem(nil, 0, 1, false), 60, 1, true).
		AddItem(nil, 0, 1, false)

	tui.pages.AddPage("llmSettings", modal, true, true)
	tui.app.SetFocus(form)
}

// showSettings displays the settings interface
func (tui *TerminalUI) showSettings() {
	settingsView := tview.NewFlex().SetDirection(tview.FlexRow)

	header := tview.NewTextView()
	header.SetText("[::b]Settings & Configuration")
	header.SetTextAlign(tview.AlignCenter)
	header.SetDynamicColors(true)
	header.SetBorder(true)

	// Create tabs for different settings categories
	tabs := tview.NewTextView()
	tabs.SetText("[::b]1. Theme  2. Cognee  3. System")
	tabs.SetTextAlign(tview.AlignCenter)
	tabs.SetDynamicColors(true)
	tabs.SetBorder(true)
	tabs.SetTitle(tui.t("terminal_ui_settings_categories_title"))

	// Theme settings
	themeView := tui.createThemeSettingsView()

	// Cognee settings
	cogneeView := tui.createCogneeSettingsView()

	// System settings
	systemView := tui.createSystemSettingsView()

	// Start with theme view
	contentArea := tview.NewPages()
	contentArea.AddPage("theme", themeView, true, true)
	contentArea.AddPage("cognee", cogneeView, true, true)
	contentArea.AddPage("system", systemView, true, true)
	contentArea.SwitchToPage("theme")

	// Handle tab navigation
	tabs.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		switch event.Key() {
		case tcell.KeyF1, tcell.KeyCtrlT:
			contentArea.SwitchToPage("theme")
			return nil
		case tcell.KeyF2, tcell.KeyCtrlC:
			contentArea.SwitchToPage("cognee")
			return nil
		case tcell.KeyF3, tcell.KeyCtrlS:
			contentArea.SwitchToPage("system")
			return nil
		}
		return event
	})

	settingsView.
		AddItem(header, 3, 0, false).
		AddItem(tabs, 3, 0, false).
		AddItem(contentArea, 0, 1, false)

	tui.content.SwitchToPage("settings")
	tui.content.AddPage("settings", settingsView, true, true)
}

// createThemeSettingsView creates the theme settings view
func (tui *TerminalUI) createThemeSettingsView() tview.Primitive {
	view := tview.NewFlex().SetDirection(tview.FlexRow)

	// Theme selection
	themeList := tview.NewList()
	themeList.SetBorder(true)
	themeList.SetTitle("Theme Selection")
	themeList.SetTitleAlign(tview.AlignLeft)

	themes := tui.themeManager.GetAvailableThemes()
	for _, themeName := range themes {
		// Capture themeName by value — Go's loop variable was the
		// previous gotcha that hid the bluff (every callback would
		// have closed over the same final value). Round-33 §11.4
		// anti-bluff fix: previous closure body was empty with the
		// comment "// For now, just show a message" — the
		// "Theme Selection" UI element rendered every theme but
		// selecting one was a no-op, fabricating UX completion.
		// CONST-035 / Article XI §11.9 / CONST-050(A).
		name := themeName
		themeList.AddItem(name, "", 0, func() {
			tui.themeManager.SetTheme(name)
		})
	}

	// Current theme info
	currentTheme := tui.themeManager.GetCurrentTheme()
	themeInfo := tview.NewTextView()
	themeInfo.SetBorder(true)
	themeInfo.SetTitle("Current Theme")
	themeInfo.SetTitleAlign(tview.AlignLeft)
	themeInfo.SetText(fmt.Sprintf("Name: %s\nDark: %t\nPrimary: %s\nSecondary: %s\nAccent: %s",
		currentTheme.Name, currentTheme.IsDark,
		currentTheme.Primary, currentTheme.Secondary, currentTheme.Accent))

	view.
		AddItem(themeList, 0, 1, false).
		AddItem(themeInfo, 0, 1, false)

	return view
}

// createCogneeSettingsView creates the Cognee settings view
func (tui *TerminalUI) createCogneeSettingsView() tview.Primitive {
	view := tview.NewFlex().SetDirection(tview.FlexRow)

	// Cognee status and controls
	statusView := tview.NewTextView()
	statusView.SetBorder(true)
	statusView.SetTitle("Cognee Status")
	statusView.SetTitleAlign(tview.AlignLeft)
	statusView.SetDynamicColors(true)

	// Get Cognee config from helix config
	cogneeEnabled := "Disabled"
	cogneeMode := "N/A"
	if tui.helixConfig != nil && tui.helixConfig.Cognee.Enabled {
		cogneeEnabled = "[green]Enabled"
		cogneeMode = tui.helixConfig.Cognee.Mode
	}

	statusView.SetText(fmt.Sprintf("Status: %s\nMode: %s\nHost: %s\nPort: %d",
		cogneeEnabled, cogneeMode,
		tui.helixConfig.Cognee.Host, tui.helixConfig.Cognee.Port))

	// Control buttons
	controls := tview.NewFlex().SetDirection(tview.FlexColumn)

	enableBtn := tview.NewButton("Enable Cognee").SetSelectedFunc(func() {
		// Enable Cognee in configuration
		tui.helixConfig.Cognee.Enabled = true
		if tui.helixConfig.Cognee.Host == "" {
			tui.helixConfig.Cognee.Host = "localhost"
		}
		if tui.helixConfig.Cognee.Port == 0 {
			tui.helixConfig.Cognee.Port = 8000
		}

		// Save configuration
		if err := config.SaveHelixConfig(tui.helixConfig); err != nil {
			log.Printf("Failed to save Cognee config: %v", err)
		}

		// Update status display
		statusView.SetText(fmt.Sprintf("Status: [green]Enabled\nMode: %s\nHost: %s\nPort: %d",
			tui.helixConfig.Cognee.Mode,
			tui.helixConfig.Cognee.Host,
			tui.helixConfig.Cognee.Port))

		tui.statusBar.SetText(tui.t("terminal_ui_cognee_enabled_status"))
	})
	controls.AddItem(enableBtn, 0, 1, false)

	disableBtn := tview.NewButton("Disable Cognee").SetSelectedFunc(func() {
		// Disable Cognee in configuration
		tui.helixConfig.Cognee.Enabled = false

		// Save configuration
		if err := config.SaveHelixConfig(tui.helixConfig); err != nil {
			log.Printf("Failed to save Cognee config: %v", err)
		}

		// Update status display
		statusView.SetText("Status: [red]Disabled\nMode: N/A\nHost: N/A\nPort: N/A")

		tui.statusBar.SetText(tui.t("terminal_ui_cognee_disabled_status"))
	})
	controls.AddItem(disableBtn, 0, 1, false)

	// Configuration options
	configView := tview.NewTextView()
	configView.SetBorder(true)
	configView.SetTitle(tui.t("terminal_ui_config_options_title"))
	configView.SetTitleAlign(tview.AlignLeft)
	configView.SetText(tui.t("terminal_ui_config_basic_settings_body"))

	view.
		AddItem(statusView, 6, 0, false).
		AddItem(controls, 3, 0, false).
		AddItem(configView, 0, 1, false)

	return view
}

// showNewTaskForm displays a modal form for creating a new task
func (tui *TerminalUI) showNewTaskForm() {
	form := tview.NewForm()
	form.SetBorder(true).SetTitle("Create New Task").SetTitleAlign(tview.AlignLeft)

	// Form fields
	var taskType, taskData, taskPriority, taskCriticality string

	form.AddDropDown("Task Type", []string{
		"Planning",
		"Building",
		"Testing",
		"Refactoring",
		"Debugging",
		"Deployment",
	}, 0, func(option string, index int) {
		taskType = option
	})

	form.AddInputField(tui.t("terminal_ui_form_task_data_json"), tui.t("terminal_ui_form_task_data_default"), 50, nil, func(text string) {
		taskData = text
	})

	form.AddDropDown("Priority", []string{"Low", "Normal", "High", "Critical"}, 1, func(option string, index int) {
		taskPriority = option
	})

	form.AddDropDown("Criticality", []string{"Low", "Normal", "High", "Critical"}, 1, func(option string, index int) {
		taskCriticality = option
	})

	// Buttons
	form.AddButton("Create", func() {
		// Parse task data
		var data map[string]interface{}
		if taskData != "" {
			// In production, would properly parse JSON
			data = map[string]interface{}{
				"description": taskData,
			}
		}

		// Map string values to task constants
		typeMap := map[string]task.TaskType{
			"Planning":    task.TaskTypePlanning,
			"Building":    task.TaskTypeBuilding,
			"Testing":     task.TaskTypeTesting,
			"Refactoring": task.TaskTypeRefactoring,
			"Debugging":   task.TaskTypeDebugging,
			"Deployment":  task.TaskTypeDeployment,
		}

		priorityMap := map[string]task.TaskPriority{
			"Low":      task.PriorityLow,
			"Normal":   task.PriorityNormal,
			"High":     task.PriorityHigh,
			"Critical": task.PriorityCritical,
		}

		criticalityMap := map[string]task.TaskCriticality{
			"Low":      task.CriticalityLow,
			"Normal":   task.CriticalityNormal,
			"High":     task.CriticalityHigh,
			"Critical": task.CriticalityCritical,
		}

		// Create task
		newTask, err := tui.taskManager.CreateTask(
			typeMap[taskType],
			data,
			priorityMap[taskPriority],
			criticalityMap[taskCriticality],
			[]uuid.UUID{}, // UI-created tasks start with no dependencies
		)

		if err != nil {
			tui.statusBar.SetText(tui.td("terminal_ui_task_create_failed", map[string]any{"Error": err.Error()}))
		} else {
			tui.statusBar.SetText(tui.td("terminal_ui_task_created", map[string]any{"TaskID": newTask.ID}))
		}

		// Close the modal
		tui.pages.RemovePage("newTaskForm")
		tui.app.SetFocus(tui.content)
	})

	form.AddButton("Cancel", func() {
		tui.pages.RemovePage("newTaskForm")
		tui.app.SetFocus(tui.content)
	})

	// Create a modal
	modal := tview.NewFlex().
		AddItem(nil, 0, 1, false).
		AddItem(tview.NewFlex().SetDirection(tview.FlexRow).
			AddItem(nil, 0, 1, false).
			AddItem(form, 20, 1, true).
			AddItem(nil, 0, 1, false), 60, 1, true).
		AddItem(nil, 0, 1, false)

	tui.pages.AddPage("newTaskForm", modal, true, true)
	tui.app.SetFocus(form)
}

// createSystemSettingsView creates the system settings view
func (tui *TerminalUI) createSystemSettingsView() tview.Primitive {
	view := tview.NewTextView()
	view.SetBorder(true)
	view.SetTitle("System Settings")
	view.SetTitleAlign(tview.AlignLeft)
	view.SetText(tui.t("terminal_ui_system_config_body"))

	return view
}

// showQA displays the QA dashboard with session list and controls.
func (tui *TerminalUI) showQA() {
	qaView := tview.NewFlex().SetDirection(tview.FlexRow)

	header := tview.NewTextView().
		SetText("[::b]QA Dashboard").
		SetTextAlign(tview.AlignCenter).
		SetDynamicColors(true)
	header.SetBorder(true)

	// Engine status line
	statusText := "[red]" + tui.t("terminal_ui_qa_engine_disabled")
	if tui.qaEngine != nil && tui.qaEngine.Enabled() {
		statusText = "[green]" + tui.t("terminal_ui_qa_engine_enabled")
	}
	statusView := tview.NewTextView().
		SetText(statusText).
		SetTextAlign(tview.AlignLeft).
		SetDynamicColors(true)
	statusView.SetBorder(true).SetTitle("Status")

	// Session table
	sessionTable := tview.NewTable().
		SetBorders(true).
		SetSelectable(true, false)
	sessionTable.SetBorder(true).SetTitle("Sessions")

	headers := []string{"ID", "Status", "Phase", "Progress", "Platforms", "Banks", "Duration"}
	for col, h := range headers {
		sessionTable.SetCell(0, col, tview.NewTableCell(h).
			SetTextColor(tcell.ColorYellow).
			SetAlign(tview.AlignCenter).
			SetSelectable(false))
	}

	if tui.qaEngine == nil || !tui.qaEngine.Enabled() {
		sessionTable.SetCell(1, 0, tview.NewTableCell(tui.t("terminal_ui_qa_engine_disabled_hint")).
			SetAlign(tview.AlignCenter).
			SetSelectable(false))
	} else {
		// HXC-174: snapshots, NOT the live pointers. Every field read below
		// (Status / Phase / PhaseProgress / StartTime / EndTime / Platforms /
		// Banks) is concurrently written by the orchestrator goroutine under
		// state.Mu; reading them off ListSessions() here was a data race, with
		// the EndTime check-then-use able to observe a non-nil pointer to a
		// not-yet-published value. ListSessionSnapshots detaches each session
		// under its own read lock, so the rows below are formatted from a
		// stable copy nothing else can touch.
		sessions := tui.qaEngine.ListSessionSnapshots()
		if len(sessions) == 0 {
			sessionTable.SetCell(1, 0, tview.NewTableCell(tui.t("terminal_ui_qa_no_sessions")).
				SetAlign(tview.AlignCenter).
				SetSelectable(false))
		} else {
			for row, s := range sessions {
				statusColor := "[yellow]"
				switch s.Status {
				case "completed":
					statusColor = "[green]"
				case "failed", "cancelled":
					statusColor = "[red]"
				case "running":
					statusColor = "[blue]"
				}

				duration := "-"
				if !s.StartTime.IsZero() {
					if s.EndTime != nil {
						duration = s.EndTime.Sub(s.StartTime).Round(time.Second).String()
					} else {
						duration = time.Since(s.StartTime).Round(time.Second).String()
					}
				}

				progressStr := fmt.Sprintf("%.0f%%", s.PhaseProgress*100)

				sessionTable.SetCell(row+1, 0, tview.NewTableCell(s.ID[:min(8, len(s.ID))]).SetAlign(tview.AlignLeft))
				sessionTable.SetCell(row+1, 1, tview.NewTableCell(statusColor+s.Status).SetAlign(tview.AlignCenter))
				sessionTable.SetCell(row+1, 2, tview.NewTableCell(s.Phase).SetAlign(tview.AlignLeft))
				sessionTable.SetCell(row+1, 3, tview.NewTableCell(progressStr).SetAlign(tview.AlignCenter))
				sessionTable.SetCell(row+1, 4, tview.NewTableCell(strings.Join(s.Platforms, ", ")).SetAlign(tview.AlignLeft))
				sessionTable.SetCell(row+1, 5, tview.NewTableCell(strings.Join(s.Banks, ", ")).SetAlign(tview.AlignLeft))
				sessionTable.SetCell(row+1, 6, tview.NewTableCell(duration).SetAlign(tview.AlignCenter))
			}
		}
	}

	// Stats panel
	statsPanel := tview.NewTextView().
		SetDynamicColors(true).
		SetWrap(true)
	statsPanel.SetBorder(true).SetTitle("QA Stats")

	var statsBuilder strings.Builder
	if tui.qaEngine != nil && tui.qaEngine.Enabled() {
		// HXC-174: snapshots for the same reason as the session table above —
		// this tally reads s.Status while the orchestrator goroutine writes it.
		sessions := tui.qaEngine.ListSessionSnapshots()
		var running, completed, failed int
		for _, s := range sessions {
			switch s.Status {
			case "running":
				running++
			case "completed":
				completed++
			case "failed":
				failed++
			}
		}
		statsBuilder.WriteString("[white]" + tui.td("terminal_ui_qa_stats_total_sessions", map[string]any{"Count": len(sessions)}) + "\n")
		statsBuilder.WriteString(fmt.Sprintf("[white]Running: [blue]%d\n", running))
		statsBuilder.WriteString(fmt.Sprintf("[white]Completed: [green]%d\n", completed))
		statsBuilder.WriteString(fmt.Sprintf("[white]Failed: [red]%d\n", failed))
		statsBuilder.WriteString("[white]" + tui.td("terminal_ui_qa_stats_coverage_target", map[string]any{"Percent": fmt.Sprintf("%.0f", tui.config.QA.CoverageTarget*100)}))
	} else {
		statsBuilder.WriteString("[gray]QA not configured.\n")
		statsBuilder.WriteString("[gray]" + tui.t("terminal_ui_qa_enable_hint"))
	}
	statsPanel.SetText(statsBuilder.String())

	// Action buttons
	actions := tview.NewFlex().SetDirection(tview.FlexColumn)
	refreshBtn := tview.NewButton("Refresh").SetSelectedFunc(func() {
		tui.showQA()
	})
	actions.AddItem(refreshBtn, 0, 1, false)
	actions.AddItem(tview.NewBox(), 1, 0, false)

	if tui.qaEngine != nil && tui.qaEngine.Enabled() {
		startBtn := tview.NewButton("Start Session").SetSelectedFunc(func() {
			tui.showStartQAForm()
		})
		actions.AddItem(startBtn, 0, 1, false)

		cancelBtn := tview.NewButton("Cancel Selected").SetSelectedFunc(func() {
			row, _ := sessionTable.GetSelection()
			if row > 0 {
				cell := sessionTable.GetCell(row, 0)
				if cell != nil {
					sessionID := cell.Text
					if err := tui.qaEngine.CancelSession(sessionID); err != nil {
						tui.statusBar.SetText("[red]" + tui.td("terminal_ui_qa_cancel_failed", map[string]any{"Error": err.Error()}))
					} else {
						tui.statusBar.SetText(fmt.Sprintf("[green]Session %s cancelled", sessionID))
						tui.showQA()
					}
				}
			}
		})
		actions.AddItem(cancelBtn, 0, 1, false)
	}

	// Main content area
	contentFlex := tview.NewFlex().SetDirection(tview.FlexColumn)
	contentFlex.AddItem(sessionTable, 0, 3, true)
	contentFlex.AddItem(statsPanel, 30, 0, false)

	qaView.
		AddItem(header, 3, 0, false).
		AddItem(statusView, 3, 0, false).
		AddItem(contentFlex, 0, 1, true).
		AddItem(actions, 3, 0, false)

	tui.content.SwitchToPage("qa")
	tui.content.AddPage("qa", qaView, true, true)
}

// showStartQAForm displays a form for starting a new QA session.
func (tui *TerminalUI) showStartQAForm() {
	form := tview.NewForm()
	form.SetBorder(true).SetTitle(tui.t("terminal_ui_form_start_qa_session_title")).SetTitleAlign(tview.AlignLeft)

	var platformsStr, banksStr string
	var autonomous bool

	form.AddInputField("Platforms", "web", 30, nil, func(text string) {
		platformsStr = text
	})
	form.AddInputField("Banks", "default", 30, nil, func(text string) {
		banksStr = text
	})
	form.AddCheckbox("Autonomous", false, func(checked bool) {
		autonomous = checked
	})

	form.AddButton("Start", func() {
		if platformsStr == "" || banksStr == "" {
			tui.statusBar.SetText("[red]Platforms and banks are required")
			tui.pages.RemovePage("startQAForm")
			return
		}
		platforms := strings.Split(platformsStr, ",")
		for i := range platforms {
			platforms[i] = strings.TrimSpace(platforms[i])
		}
		banks := strings.Split(banksStr, ",")
		for i := range banks {
			banks[i] = strings.TrimSpace(banks[i])
		}

		sessionID := uuid.New().String()
		_, err := tui.qaEngine.StartSession(context.Background(), sessionID, platforms, banks, autonomous)
		if err != nil {
			tui.statusBar.SetText("[red]" + tui.td("terminal_ui_qa_start_session_failed", map[string]any{"Error": err.Error()}))
		} else {
			tui.statusBar.SetText(fmt.Sprintf("[green]Session %s started", sessionID[:8]))
		}
		tui.pages.RemovePage("startQAForm")
		tui.showQA()
	})

	form.AddButton("Cancel", func() {
		tui.pages.RemovePage("startQAForm")
	})

	modal := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(tview.NewBox(), 0, 1, false).
		AddItem(form, 15, 0, true).
		AddItem(tview.NewBox(), 0, 1, false)

	tui.pages.AddPage("startQAForm", modal, true, true)
}

// menuHotkeyTarget maps a key event to the sidebar page it should navigate to,
// honouring focus: when a text field / form / list (chat input, model picker,
// new-task form, the sidebar list itself) holds focus, the event is passed
// through (returns "", false) so typing, picker selection, and the sidebar's
// own shortcuts keep working. Otherwise a menu-hotkey rune resolves to its page.
func menuHotkeyTarget(focus tview.Primitive, ev *tcell.EventKey) (string, bool) {
	switch focus.(type) {
	case *tview.InputField, *tview.TextArea, *tview.Form, *tview.List:
		return "", false
	}
	if ev == nil || ev.Key() != tcell.KeyRune {
		return "", false
	}
	switch ev.Rune() {
	case 'd':
		return "dashboard", true
	case 't':
		return "tasks", true
	case 'w':
		return "workers", true
	case 'p':
		return "projects", true
	case 's':
		return "sessions", true
	case 'l':
		return "llm", true
	case 'q':
		return "qa", true
	case 'c':
		return "settings", true
	}
	return "", false
}

// navigateTo dispatches to the show* function for a sidebar page name.
func (tui *TerminalUI) navigateTo(page string) {
	switch page {
	case "dashboard":
		tui.showDashboard()
	case "tasks":
		tui.showTasks()
	case "workers":
		tui.showWorkers()
	case "projects":
		tui.showProjects()
	case "sessions":
		tui.showSessions()
	case "llm":
		tui.showLLM()
	case "qa":
		tui.showQA()
	case "settings":
		tui.showSettings()
	}
}

// Run starts the Terminal UI application
func (tui *TerminalUI) Run() error {
	// Setup signal handling for graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// Global menu-hotkey navigation: route the sidebar shortcuts
	// (d/t/w/p/s/l/q/c) from anywhere, EXCEPT when a text field / form / list
	// (chat input, model picker, forms) holds focus, so typing and pickers
	// still work. Fixes the keyboard-navigation dead-end where focus left the
	// sidebar and no key could return to it (the LLM chat was unreachable).
	tui.app.SetInputCapture(func(ev *tcell.EventKey) *tcell.EventKey {
		if page, ok := menuHotkeyTarget(tui.app.GetFocus(), ev); ok {
			tui.navigateTo(page)
			return nil
		}
		return ev
	})

	// Run the application in a goroutine
	go func() {
		if err := tui.app.SetRoot(tui.pages, true).Run(); err != nil {
			log.Printf("TUI application error: %v", err)
		}
	}()

	// Wait for shutdown signal
	<-sigChan
	tui.app.Stop()

	return nil
}

// Close cleans up resources
func (tui *TerminalUI) Close() error {
	if tui.db != nil {
		tui.db.Close()
	}
	// Release the capability building blocks wired in Initialize so their
	// child processes (MCP servers, LSP servers) are torn down on shutdown.
	if tui.mcpManager != nil {
		if err := tui.mcpManager.Close(); err != nil {
			log.Printf("⚠️  TUI: MCP manager close failed: %v", err)
		}
	}
	if tui.lspManager != nil {
		if err := tui.lspManager.Shutdown(context.Background()); err != nil {
			log.Printf("⚠️  TUI: LSP manager shutdown failed: %v", err)
		}
	}
	// Checkpoint + release the durable memory store so the on-disk DB is folded
	// back from its WAL sidecars before exit.
	tui.closeMemory()
	return nil
}

func main() {
	tui := NewTerminalUI()

	// Install the real CONST-046 translator BEFORE Initialize() (setupUI
	// resolves the sidebar title + status bar via tui.t(...)). Without this
	// the standalone binary ran on NoopTranslator{} and leaked raw message-ID
	// keys on the landing screen.
	wireTranslator(tui)

	if err := tui.Initialize(); err != nil {
		log.Fatalf("Failed to initialize Terminal UI: %v", err)
	}
	defer tui.Close()

	if err := tui.Run(); err != nil {
		log.Fatalf("Terminal UI error: %v", err)
	}
}
