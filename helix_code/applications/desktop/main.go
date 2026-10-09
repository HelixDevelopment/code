//go:build !nogui

package main

import (
	"context"
	_ "embed"
	"fmt"
	"log"
	"os"
	"os/signal"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"

	"dev.helix.code/applications/desktop/i18n"
	"dev.helix.code/internal/agent"
	"dev.helix.code/internal/clientcore"
	"dev.helix.code/internal/config"
	"dev.helix.code/internal/database"
	"dev.helix.code/internal/ensembleui"
	"dev.helix.code/internal/fyneui"
	"dev.helix.code/internal/llm"
	"dev.helix.code/internal/notification"
	"dev.helix.code/internal/project"
	"dev.helix.code/internal/redis"
	"dev.helix.code/internal/server"
	"dev.helix.code/internal/session"
	"dev.helix.code/internal/task"
	"dev.helix.code/internal/worker"
)

// hxcLogoPNG is the HelixCode brand logo (the lime-green→teal nautilus spiral,
// copied from the meta-repo's assets/Logo.png into the package so go:embed can
// bundle it into the binary — Fyne needs the bytes available at runtime with no
// external file dependency). It is surfaced as the window icon and in the
// dashboard header.
//
//go:embed Logo.png
var hxcLogoPNG []byte

// hxcLogoResource wraps the embedded brand logo as a fyne.Resource so it can be
// used for the window icon (App.SetIcon / Window.SetIcon) and as the source for
// a canvas.Image in the dashboard header.
func hxcLogoResource() fyne.Resource {
	return fyne.NewStaticResource("HelixCodeLogo.png", hxcLogoPNG)
}

// streamDesktopChat drives one desktop-chat turn over the provider's
// streaming API (P1-T07, speed programme Phase 1).
//
// It emits `prefix` once, then appends each streamed chunk's Content to the
// chat-history Entry the instant the chunk arrives — so the user sees the
// reply grow token-by-token (time-to-first-visible-token) instead of waiting
// for the whole completion.
//
// THREAD-AFFINITY FIX (§11.4.115). An earlier revision of this comment claimed
// "(*widget.Entry).SetText is goroutine-safe in Fyne, so calling it from the
// caller's worker goroutine is correct." That is FALSE, and was the root cause
// of a large data-race population. Fyne states the opposite verbatim since 2.6
// (fyne.io/fyne/v2/thread.go): "This is required when a background process
// wishes to adjust graphical elements of a running app." This function is
// invoked from the Send-Message worker goroutine, so every mutation of
// `history` MUST be dispatched onto the UI goroutine.
//
// Do (async) rather than DoAndWait (blocking) is deliberate: this is the
// streaming hot path, and the whole point of P1-T07 is that tokens appear as
// they arrive. Blocking the producer on the render loop once per token would
// couple stream throughput to frame time. Ordering is still exact — the glfw
// driver's func-queue is an unbounded FIFO drained sequentially by the main
// loop, so appends submitted by this one goroutine are applied in submission
// order.
//
// The read and the write are inside ONE closure on purpose:
// `history.Text + content` READS the widget too, so hoisting the read out
// would leave it racing the renderer even though the write was dispatched.
//
// The channel-consumption loop is delegated to consumeDesktopChatStream (a
// build-tag-free helper) so it stays unit-testable without an X11 display.
//
// No-regression: the text appended is the concatenation of every chunk's
// Content, byte-identical to the buffered Generate result for any conformant
// provider. Only WHEN the bytes appear changes.
func streamDesktopChat(ctx context.Context, provider llm.Provider, request *llm.LLMRequest, prefix string, history *widget.Entry) error {
	fyneui.Do(func() { history.SetText(history.Text + prefix) })
	return consumeDesktopChatStream(ctx, provider, request, func(content string) {
		fyneui.Do(func() { history.SetText(history.Text + content) })
	})
}

// UITask is a simplified task representation for UI display
type UITask struct {
	ID          string
	Type        string
	Description string
	Status      string
	Priority    string
}

// UIWorker is a simplified worker representation for UI display
type UIWorker struct {
	ID      string
	Host    string
	Port    string
	User    string
	Status  string
	Healthy bool
}

// TaskStats provides task statistics for UI
type TaskStats struct {
	TotalTasks     int
	CompletedTasks int
	RunningTasks   int
	PendingTasks   int
}

// DesktopTaskManager wraps task.TaskManager for UI operations
type DesktopTaskManager struct {
	inner *task.TaskManager
	tasks []UITask // In-memory task list for UI
	mu    sync.RWMutex
}

// NewDesktopTaskManager creates a new desktop task manager wrapper
func NewDesktopTaskManager(tm *task.TaskManager) *DesktopTaskManager {
	return &DesktopTaskManager{
		inner: tm,
		tasks: make([]UITask, 0),
	}
}

// GetAllTasks returns all tasks for UI display
func (dtm *DesktopTaskManager) GetAllTasks() []UITask {
	dtm.mu.RLock()
	defer dtm.mu.RUnlock()
	return dtm.tasks
}

// GetStats returns task statistics
func (dtm *DesktopTaskManager) GetStats() TaskStats {
	dtm.mu.RLock()
	defer dtm.mu.RUnlock()

	stats := TaskStats{
		TotalTasks: len(dtm.tasks),
	}

	for _, t := range dtm.tasks {
		switch t.Status {
		case "completed":
			stats.CompletedTasks++
		case "running":
			stats.RunningTasks++
		case "pending":
			stats.PendingTasks++
		}
	}

	return stats
}

// CreateTask creates a new task (simplified for UI)
func (dtm *DesktopTaskManager) CreateTask(ctx context.Context, taskType, description, priority string) (*UITask, error) {
	dtm.mu.Lock()
	defer dtm.mu.Unlock()

	newTask := UITask{
		ID:          fmt.Sprintf("task-%d", time.Now().UnixNano()),
		Type:        taskType,
		Description: description,
		Status:      "pending",
		Priority:    priority,
	}

	dtm.tasks = append(dtm.tasks, newTask)
	return &newTask, nil
}

// CancelTask cancels a task by ID
func (dtm *DesktopTaskManager) CancelTask(ctx context.Context, taskID string) error {
	dtm.mu.Lock()
	defer dtm.mu.Unlock()

	for i, t := range dtm.tasks {
		if t.ID == taskID {
			dtm.tasks = append(dtm.tasks[:i], dtm.tasks[i+1:]...)
			return nil
		}
	}
	return fmt.Errorf("task not found: %s", taskID)
}

// DesktopWorkerManager wraps worker.WorkerManager for UI operations
type DesktopWorkerManager struct {
	inner   *worker.WorkerManager
	workers []UIWorker // In-memory worker list for UI
	mu      sync.RWMutex
}

// NewDesktopWorkerManager creates a new desktop worker manager wrapper
func NewDesktopWorkerManager(wm *worker.WorkerManager) *DesktopWorkerManager {
	return &DesktopWorkerManager{
		inner:   wm,
		workers: make([]UIWorker, 0),
	}
}

// GetWorkers returns all workers for UI display
func (dwm *DesktopWorkerManager) GetWorkers() []UIWorker {
	dwm.mu.RLock()
	defer dwm.mu.RUnlock()
	return dwm.workers
}

// AddWorker adds a new worker (simplified for UI)
func (dwm *DesktopWorkerManager) AddWorker(w *UIWorker) error {
	dwm.mu.Lock()
	defer dwm.mu.Unlock()

	dwm.workers = append(dwm.workers, *w)
	return nil
}

// RemoveWorker removes a worker by ID
func (dwm *DesktopWorkerManager) RemoveWorker(workerID string) error {
	dwm.mu.Lock()
	defer dwm.mu.Unlock()

	for i, w := range dwm.workers {
		if w.ID == workerID {
			dwm.workers = append(dwm.workers[:i], dwm.workers[i+1:]...)
			return nil
		}
	}
	return fmt.Errorf("worker not found: %s", workerID)
}

// DesktopApp represents the desktop application
type DesktopApp struct {
	fyneApp            fyne.App
	mainWindow         fyne.Window
	config             *config.Config
	db                 *database.Database
	taskManager        *DesktopTaskManager
	workerManager      *DesktopWorkerManager
	projectManager     *project.Manager
	sessionManager     *session.Manager
	llmManager         *llm.ModelManager
	notificationEngine *notification.NotificationEngine
	server             *server.Server
	themeManager       *ThemeManager

	// Agentic capability core (wired at parity with the TUI via the shared
	// internal/clientcore package — §11.4.74 reuse-over-reimplement).
	//
	// agenticTools holds the read-only tool registry (git_status + fs_read/
	// glob/grep) with LSP diagnostics + MCP tools merged in. Its Registry
	// drives the chat tool loop via agent.RunToolLoopStream(ReadOnlyOnly);
	// nil when registry construction failed (the chat then falls back to the
	// plain streaming path). skills holds the loaded skill dispatcher + plugin
	// loader (graceful-nil when absent). Both are released in Close.
	agenticTools *clientcore.AgenticTools
	skills       *clientcore.Skills
	// selectedModel is the model id chosen in the LLM tab's chat controls. It
	// is sent as the tool-loop opts.Model on every chat turn; empty falls back
	// to the provider's first model.
	selectedModel string

	// UI Components
	tabs           *container.AppTabs
	statusBar      *widget.Label
	projectList    *widget.List
	sessionList    *widget.List
	llmProviderSel *widget.Select
	chatHistory    *widget.Entry
	chatInput      *widget.Entry

	// Data cache for UI updates
	dataMu       sync.RWMutex
	projects     []*project.Project
	sessions     []*session.Session
	llmProviders []string

	// Update ticker for real-time data
	updateTicker *time.Ticker
	stopUpdate   chan struct{}
	stopOnce     sync.Once

	// updateDone is closed by the startDataUpdates goroutine immediately
	// before it returns (whether returning via the priority stop pre-check or
	// the ticker case's inner re-check). Close() waits on it (bounded) BEFORE
	// tearing down da.db / da.agenticTools, so teardown never races a
	// still-running refreshData() call. nil until startDataUpdates runs (a
	// Close() called without a prior startDataUpdates is therefore a no-op
	// wait, never a hang on a channel nothing will ever close). See
	// updateLoopTick's doc comment for the teardown-race this closes.
	updateDone chan struct{}

	// updateLoopTickRaceHook is a test seam (nil in production, never set
	// outside _test.go files): when non-nil, updateLoopTick invokes it
	// immediately after the non-blocking priority pre-check observes
	// stopUpdate still open, but BEFORE the blocking select that follows is
	// evaluated. This is precisely the residual race window described on
	// updateLoopTick's doc comment -- a close(stopUpdate) landing here,
	// concurrently with an already-buffered tick, leaves Go's uniform-random
	// select pick free to choose either case. Production code never sets
	// this hook; it exists so a §11.4.115 test can land a close(stopUpdate)
	// deterministically inside that exact window instead of depending on
	// scheduler luck to hit a nanosecond-wide gap.
	updateLoopTickRaceHook func()

	// translator resolves CONST-046 user-facing message IDs. Defaults
	// to i18n.NoopTranslator{} (loud message-ID echo) when nil — set
	// by helix_code at boot via SetTranslator to a real
	// *i18nadapter.Translator wired to the active.en.yaml bundle.
	translator i18n.Translator
}

// NewDesktopApp creates a new desktop application. The app starts
// with i18n.NoopTranslator{} for backward compat — production
// wiring calls SetTranslator with a real Translator (e.g.
// helix_code's *i18nadapter.Translator) before Run.
func NewDesktopApp() *DesktopApp {
	fyneApp := app.New()
	// HXC: use the constructor — a zero-value &CustomTheme{} has a nil
	// currentTheme and crashes Fyne's canvas color lookups (SIGSEGV on launch).
	fyneApp.Settings().SetTheme(NewCustomTheme())
	// HelixCode brand identity: set the embedded brand logo as the application
	// icon (taskbar / dock / about). The per-window icon is set in setupUI.
	fyneApp.SetIcon(hxcLogoResource())

	return &DesktopApp{
		fyneApp:      fyneApp,
		projects:     make([]*project.Project, 0),
		sessions:     make([]*session.Session, 0),
		llmProviders: make([]string, 0),
		stopUpdate:   make(chan struct{}),
		translator:   i18n.NoopTranslator{},
	}
}

// SetTranslator wires a CONST-046-compliant Translator. Passing nil
// resets to i18n.NoopTranslator{} (loud echo) — never silently
// disables translation lookup (which would be a §11.4 PASS-bluff at
// the i18n injection layer).
func (da *DesktopApp) SetTranslator(tr i18n.Translator) {
	if tr == nil {
		da.translator = i18n.NoopTranslator{}
		return
	}
	da.translator = tr
}

// tr is the internal CONST-046 resolver used by every user-facing
// string emission in this file. It NEVER returns an error to the
// caller — translation failures degrade to the message ID itself
// (matching NoopTranslator behaviour) so production output remains
// loud + obvious instead of silently empty.
func (da *DesktopApp) tr(ctx context.Context, msgID string, data map[string]any) string {
	if da.translator == nil {
		da.translator = i18n.NoopTranslator{}
	}
	out, err := da.translator.T(ctx, msgID, data)
	if err != nil || out == "" {
		return msgID
	}
	return out
}

// Initialize sets up the desktop application
func (da *DesktopApp) Initialize() error {
	// Load configuration
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("failed to load configuration: %v", err)
	}
	da.config = cfg

	// Initialize database (optional - continue without it if not available)
	db, err := database.New(cfg.Database)
	if err != nil {
		log.Printf("Warning: Database not available: %v (continuing without persistence)", err)
	}
	da.db = db

	// Initialize Redis (optional - continue without it if not available)
	rds, err := redis.NewClient(&cfg.Redis)
	if err != nil {
		log.Printf("Warning: Redis not available: %v (continuing without caching)", err)
	}

	// Initialize components
	innerTaskManager := task.NewTaskManager(db, rds)
	da.taskManager = NewDesktopTaskManager(innerTaskManager)

	// Initialize worker manager with in-memory repository for standalone UI
	workerRepo := worker.NewInMemoryWorkerRepository()
	innerWorkerManager := worker.NewWorkerManager(workerRepo, 30*time.Second)
	da.workerManager = NewDesktopWorkerManager(innerWorkerManager)

	// Initialize project manager
	da.projectManager = project.NewManager()

	// Initialize session manager
	da.sessionManager = session.NewManager()

	// W2c-1 cloud gate (operator mandate 2026-09-05, local-only adaptive
	// serving): wire the hosted-provider gate from configuration BEFORE any
	// handler can construct a provider. Mirrors internal/server/server.go.
	// Default false -- llm.cloud.enabled must be explicitly set true to
	// permit cloud construction.
	llm.SetCloudEnabled(cfg.LLM.Cloud.Enabled)

	// Initialize LLM manager and register VERIFIER-DRIVEN providers at parity
	// with the TUI (CONST-036/037/040, BLUFF-002 — NO hardcoded provider list).
	// Wire LLMsVerifier FIRST so the Helix Agent ensemble resolves each member's
	// model from verified, chat-capable catalogue entries, then register every
	// cloud provider whose API key is present in the environment + the dynamic
	// OpenAI-compatible providers + the ensemble + the reachable HelixAgent
	// adapter. A no-key environment honestly registers zero providers rather
	// than a fabricated list (anti-bluff §11.4). This shared call REPLACES the
	// old hardcoded widget.NewSelect([]string{"ollama","openai",...}) source.
	da.llmManager = llm.NewModelManager()
	if clientcore.WireVerifierAdapter(da.llmManager, cfg) {
		log.Printf("✅ Desktop: LLMsVerifier wired (ensemble model resolution is verifier-driven)")
	}
	if n := clientcore.RegisterEnvProviders(da.llmManager, cfg); n > 0 {
		log.Printf("✅ Desktop: registered %d LLM provider(s) from the verifier-driven path", n)
	}

	// Build the read-only agentic tool registry + LSP + MCP at parity with the
	// TUI (§11.4.74). Default-on; degrades gracefully when gopls/npx absent or
	// registry construction fails (chat falls back to plain streaming).
	if at, atErr := clientcore.WireAgenticTools(".helixcode/mcp.yml"); atErr != nil {
		log.Printf("⚠️  Desktop: agentic tool registry unavailable (%v); chat falls back to plain streaming", atErr)
	} else {
		da.agenticTools = at
	}

	// Load skills + plugins at parity with the TUI (graceful-nil when absent).
	da.skills = clientcore.LoadSkillsAndPlugins("plugins")

	// Initialize notification engine
	da.notificationEngine = notification.NewNotificationEngine()

	// Initialize server for API calls
	da.server = server.New(cfg, db, rds)

	// Initialize theme manager
	da.themeManager = NewThemeManager()

	// Setup UI
	da.setupUI()

	// Start background data updates
	da.startDataUpdates()

	return nil
}

// startDataUpdates starts periodic background data refresh.
//
// JOIN: updateDone is created fresh here and closed by the goroutine
// immediately before it returns (either exit path below). Close() waits on
// it (bounded) before tearing down da.db / da.agenticTools -- see the
// updateDone field doc comment and Close's doc comment for the
// use-after-close race this closes.
func (da *DesktopApp) startDataUpdates() {
	da.updateTicker = time.NewTicker(5 * time.Second)
	da.updateDone = make(chan struct{})
	go func() {
		defer close(da.updateDone)

		// Initial data load
		da.refreshData()

		for {
			if !da.updateLoopTick(da.updateTicker.C, da.stopUpdate) {
				da.updateTicker.Stop()
				return
			}
		}
	}()
}

// updateLoopTick runs exactly one iteration of the background data-update
// loop's select logic. Extracted from startDataUpdates (identical
// behaviour, same select statement) so the §11.4.115 regression guard
// (main_racefix_test.go) can force the "unprioritized select" race
// deterministically -- pre-buffering a tick AND pre-closing stop before the
// select ever runs -- instead of depending on real wall-clock ticker timing
// or scheduler luck.
//
// ANTI-BLUFF FIX (§11.4.115 unprioritized-select race -- mirrors
// internal/persistence/store.go autoSaveTick, one of four confirmed
// instances of this defect class audited this session): Go's select
// chooses UNIFORMLY AT RANDOM among ALL cases ready at the instant it is
// evaluated. A naked `select { case <-ticker.C: refreshData(); case
// <-stopUpdate: return }` can therefore still pick the ticker.C branch and
// run one more refreshData() even when stopUpdate was ALREADY closed before
// the select was reached -- if this goroutine was scheduler-delayed long
// enough for a tick to also land in ticker.C by the time it finally runs.
// The non-blocking priority pre-check below closes that WIDE window: it
// runs FIRST, on every loop iteration, and returns immediately whenever
// stopUpdate is already closed BEFORE this call is even invoked.
//
// RESIDUAL WINDOW (closed by the inner re-check below): the pre-check only
// proves stopUpdate was open at the instant it ran. If close(stopUpdate)
// lands in the narrow gap BETWEEN the pre-check returning and the blocking
// select below being evaluated, and a tick is already buffered on tickerC,
// both cases are ready when the blocking select fires and Go's
// uniform-random pick can still choose the tickerC branch -- one stray
// refreshData() after Close() has already returned to its caller. The inner
// non-blocking re-check inside the tickerC case closes this too.
//
// HONESTY (§11.4.6): this is the strongest ordering guarantee a plain
// channel select can offer. It is NOT a claim the race is closed to zero
// width -- under Go's async goroutine preemption (>=1.14) this goroutine can
// still be preempted between the final re-check and the refreshData() call
// that follows it while a close(stopUpdate) lands concurrently, an
// unavoidable, unobservable race no select-based implementation can
// provably eliminate. What IS delivered: cancellation observed at the final
// decision point always wins, AND (via the updateDone join in Close, a
// SEPARATE mechanism) the caller never tears down da.db / da.agenticTools
// while this goroutine could still be mid-refreshData() -- the join
// guarantees the loop has RETURNED before teardown begins, which the
// select-priority fix alone does not.
//
// Returns false when the loop must stop (stopUpdate was signalled --
// checked FIRST, non-blocking, and again in the blocking select below),
// true when a tick was processed and the loop should continue.
func (da *DesktopApp) updateLoopTick(tickerC <-chan time.Time, stop <-chan struct{}) bool {
	// Priority pre-check: if stop is already closed, return immediately
	// without ever entering the blocking select below, where an
	// also-ready ticker.C could otherwise be picked instead.
	select {
	case <-stop:
		return false
	default:
	}

	// Test seam (§11.4.115 residual-window guard): fires only when a test
	// has set it, giving the test a deterministic hook to land
	// close(stopUpdate) exactly here -- after the pre-check above, before
	// the blocking select below. Always nil in production.
	if da.updateLoopTickRaceHook != nil {
		da.updateLoopTickRaceHook()
	}

	select {
	case <-tickerC:
		// Re-check: a stop-close landing in the window between the
		// pre-check above and this blocking select being evaluated can
		// still leave the tickerC branch "randomly" chosen even though
		// stop is now closed. Catch it here, non-blocking, before running
		// refreshData.
		select {
		case <-stop:
			return false
		default:
		}
		da.refreshData()
		return true
	case <-stop:
		return false
	}
}

// refreshData updates cached data from managers
func (da *DesktopApp) refreshData() {
	da.dataMu.Lock()
	defer da.dataMu.Unlock()

	ctx := context.Background()

	// Refresh projects
	if da.projectManager != nil {
		projects, err := da.projectManager.ListProjects(ctx, "")
		if err == nil {
			da.projects = projects
		}
	}

	// Refresh sessions
	if da.sessionManager != nil {
		da.sessions = da.sessionManager.GetAll()
	}

	// Refresh LLM providers
	if da.llmManager != nil {
		models := da.llmManager.GetAvailableModels()
		providers := make(map[string]bool)
		for _, model := range models {
			providers[string(model.Provider)] = true
		}
		da.llmProviders = make([]string, 0, len(providers))
		for provider := range providers {
			da.llmProviders = append(da.llmProviders, provider)
		}
	}
}

// setupUI initializes the user interface
func (da *DesktopApp) setupUI() {
	// Create main window
	// CONST-046: window title resolved via applications/desktop/i18n
	// bundle through injected Translator; NoopTranslator echoes ID.
	ctx := context.Background()
	da.mainWindow = da.fyneApp.NewWindow(da.tr(ctx, "desktop_window_title", nil))
	da.mainWindow.SetMaster()
	da.mainWindow.SetIcon(hxcLogoResource()) // HelixCode brand window icon
	da.mainWindow.Resize(fyne.NewSize(1200, 800))

	// Create tabs
	da.tabs = container.NewAppTabs(
		container.NewTabItem("Dashboard", da.createDashboardTab()),
		container.NewTabItem("Tasks", da.createTasksTab()),
		container.NewTabItem("Workers", da.createWorkersTab()),
		container.NewTabItem("Projects", da.createProjectsTab()),
		container.NewTabItem("Sessions", da.createSessionsTab()),
		container.NewTabItem("LLM", da.createLLMTab()),
		container.NewTabItem("Settings", da.createSettingsTab()),
	)

	// Create status bar
	// CONST-046: status bar default text resolved via i18n bundle.
	da.statusBar = widget.NewLabel(da.tr(ctx, "desktop_status_bar_default", nil))
	da.statusBar.Alignment = fyne.TextAlignCenter

	// Create main layout
	mainContent := container.NewBorder(nil, da.statusBar, nil, nil, da.tabs)

	da.mainWindow.SetContent(mainContent)
}

// createDashboardTab creates the dashboard tab
func (da *DesktopApp) createDashboardTab() fyne.CanvasObject {
	// Header with integrated HelixCode brand logo (embedded brand mark beside
	// the i18n-resolved title — CONST-046: header text resolved via i18n bundle).
	ctx := context.Background()
	header := widget.NewLabel(da.tr(ctx, "desktop_dashboard_header", nil))
	header.Alignment = fyne.TextAlignCenter
	header.TextStyle = fyne.TextStyle{Bold: true}

	// Brand logo (embedded). FillContain preserves the spiral's aspect ratio; a
	// fixed min size keeps the header compact.
	logoImage := canvas.NewImageFromResource(hxcLogoResource())
	logoImage.FillMode = canvas.ImageFillContain
	logoImage.SetMinSize(fyne.NewSize(48, 48))
	brandHeader := container.NewHBox(logoImage, header)

	// Stats cards with dynamic data
	workerStatsLabel := widget.NewLabel("Total: 0\nActive: 0\nHealthy: 0")
	taskStatsLabel := widget.NewLabel("Total: 0\nCompleted: 0\nRunning: 0")
	systemStatsLabel := widget.NewLabel("Status: Operational\nUptime: 00:00:00")

	workerCard := widget.NewCard("Workers", "", workerStatsLabel)
	taskCard := widget.NewCard("Tasks", "", taskStatsLabel)
	systemCard := widget.NewCard("System", "", systemStatsLabel)

	// Start a goroutine to update stats.
	//
	// THREAD-AFFINITY FIX (§11.4.115): this is a background goroutine, so every
	// label mutation below MUST be dispatched onto the UI goroutine via
	// fyneui.Do — a bare SetText here raced the renderer and was the single
	// largest contributor to this package's data-race population. The domain
	// data is gathered OUTSIDE the closure and only the formatted string is
	// captured, so the UI goroutine never touches a manager while this one is
	// mid-read.
	go func() {
		startTime := time.Now()
		ticker := time.NewTicker(1 * time.Second)
		defer ticker.Stop()

		// LIFECYCLE FIX (§11.4.115): this loop used to be `for range ticker.C`,
		// which can never terminate — the goroutine outlived the window it
		// painted into, kept mutating labels after teardown, and leaked for the
		// lifetime of the process. It was observed still running after its own
		// test finished, racing a LATER test's app construction. It now honours
		// the app's existing stopUpdate channel (closed by Close()).
		for fyneui.TickOrStop(ticker.C, da.stopUpdate) {
			// Update worker stats
			if da.workerManager != nil {
				workers := da.workerManager.GetWorkers()
				active := 0
				healthy := 0
				for _, w := range workers {
					if w.Status == "active" {
						active++
					}
					if w.Healthy {
						healthy++
					}
				}
				workerText := fmt.Sprintf("Total: %d\nActive: %d\nHealthy: %d", len(workers), active, healthy)
				fyneui.Do(func() { workerStatsLabel.SetText(workerText) })
			}

			// Update task stats
			if da.taskManager != nil {
				stats := da.taskManager.GetStats()
				taskText := fmt.Sprintf("Total: %d\nCompleted: %d\nRunning: %d",
					stats.TotalTasks, stats.CompletedTasks, stats.RunningTasks)
				fyneui.Do(func() { taskStatsLabel.SetText(taskText) })
			}

			// Update system stats
			uptime := time.Since(startTime)
			hours := int(uptime.Hours())
			minutes := int(uptime.Minutes()) % 60
			seconds := int(uptime.Seconds()) % 60
			systemText := fmt.Sprintf("Status: Operational\nUptime: %02d:%02d:%02d", hours, minutes, seconds)
			fyneui.Do(func() { systemStatsLabel.SetText(systemText) })
		}
	}()

	statsContainer := container.NewGridWithColumns(3, workerCard, taskCard, systemCard)

	// Activity log
	// CONST-046: activity-log seed text + card title resolved via i18n bundle.
	activityLog := widget.NewMultiLineEntry()
	activityLog.SetText(da.tr(ctx, "desktop_dashboard_activity_seed", nil))
	activityLog.Disable()

	activityCard := widget.NewCard(da.tr(ctx, "desktop_dashboard_activity_title", nil), "", activityLog)

	// Quick actions
	actionsCard := widget.NewCard("Quick Actions", "",
		container.NewVBox(
			widget.NewButton("New Task", func() {
				da.tabs.SelectIndex(1) // Switch to Tasks tab
			}),
			widget.NewButton("Add Worker", func() {
				da.tabs.SelectIndex(2) // Switch to Workers tab
			}),
			widget.NewButton("LLM Chat", func() {
				da.tabs.SelectIndex(5) // Switch to LLM tab
			}),
			widget.NewButton("New Project", func() {
				da.tabs.SelectIndex(3) // Switch to Projects tab
			}),
		),
	)

	bottomContainer := container.NewGridWithColumns(2, activityCard, actionsCard)

	return container.NewVBox(brandHeader, statsContainer, bottomContainer)
}

// createTasksTab creates the tasks tab
func (da *DesktopApp) createTasksTab() fyne.CanvasObject {
	// Task list with dynamic data
	taskList := widget.NewList(
		func() int {
			if da.taskManager == nil {
				return 0
			}
			return len(da.taskManager.GetAllTasks())
		},
		func() fyne.CanvasObject {
			return widget.NewLabel("Template")
		},
		func(id widget.ListItemID, obj fyne.CanvasObject) {
			if da.taskManager == nil {
				return
			}
			tasks := da.taskManager.GetAllTasks()
			if id < len(tasks) {
				t := tasks[id]
				obj.(*widget.Label).SetText(fmt.Sprintf("[%s] %s - %s", t.Status, t.Type, t.Description))
			}
		},
	)

	taskCard := widget.NewCard("Tasks", "", taskList)

	// Task type selector for new tasks
	taskTypeSelect := widget.NewSelect([]string{"planning", "building", "testing", "refactoring", "debugging"}, nil)
	taskTypeSelect.SetSelected("building")

	// Task description input
	// CONST-046: task-description placeholder resolved via i18n bundle.
	ctx := context.Background()
	taskDescEntry := widget.NewEntry()
	taskDescEntry.SetPlaceHolder(da.tr(ctx, "desktop_tasks_description_placeholder", nil))

	// Action buttons
	actions := container.NewVBox(
		widget.NewLabel(da.tr(ctx, "desktop_tasks_new_label", nil)),
		taskTypeSelect,
		taskDescEntry,
		widget.NewButton(da.tr(ctx, "desktop_tasks_create_button", nil), func() {
			if da.taskManager != nil && taskDescEntry.Text != "" {
				createCtx := context.Background()
				_, err := da.taskManager.CreateTask(createCtx, taskTypeSelect.Selected, taskDescEntry.Text, "normal")
				if err != nil {
					dialog.ShowError(err, da.mainWindow)
				} else {
					taskDescEntry.SetText("")
					taskList.Refresh()
				}
			}
		}),
		widget.NewSeparator(),
		widget.NewButton(da.tr(ctx, "desktop_common_refresh_button", nil), func() {
			taskList.Refresh()
		}),
	)

	return container.NewBorder(nil, nil, nil, actions, taskCard)
}

// createWorkersTab creates the workers tab
func (da *DesktopApp) createWorkersTab() fyne.CanvasObject {
	workerList := widget.NewList(
		func() int {
			if da.workerManager == nil {
				return 0
			}
			return len(da.workerManager.GetWorkers())
		},
		func() fyne.CanvasObject {
			return widget.NewLabel("Template")
		},
		func(id widget.ListItemID, obj fyne.CanvasObject) {
			if da.workerManager == nil {
				return
			}
			workers := da.workerManager.GetWorkers()
			if id < len(workers) {
				w := workers[id]
				healthStatus := "unhealthy"
				if w.Healthy {
					healthStatus = "healthy"
				}
				obj.(*widget.Label).SetText(fmt.Sprintf("[%s] %s - %s (%s)", w.Status, w.ID, w.Host, healthStatus))
			}
		},
	)

	workerCard := widget.NewCard("Workers", "", workerList)

	// Worker configuration inputs
	hostEntry := widget.NewEntry()
	hostEntry.SetPlaceHolder("hostname or IP")
	portEntry := widget.NewEntry()
	portEntry.SetPlaceHolder("22")
	portEntry.SetText("22")
	userEntry := widget.NewEntry()
	userEntry.SetPlaceHolder("username")

	actions := container.NewVBox(
		widget.NewLabel("Add Worker:"),
		widget.NewLabel("Host:"),
		hostEntry,
		widget.NewLabel("Port:"),
		portEntry,
		widget.NewLabel("User:"),
		userEntry,
		widget.NewButton("Add Worker", func() {
			if da.workerManager != nil && hostEntry.Text != "" {
				// Create worker configuration
				workerConfig := &UIWorker{
					ID:      fmt.Sprintf("worker-%s-%d", hostEntry.Text, time.Now().UnixNano()),
					Host:    hostEntry.Text,
					Port:    portEntry.Text,
					User:    userEntry.Text,
					Status:  "pending",
					Healthy: false,
				}
				err := da.workerManager.AddWorker(workerConfig)
				if err != nil {
					dialog.ShowError(err, da.mainWindow)
				} else {
					hostEntry.SetText("")
					userEntry.SetText("")
					workerList.Refresh()
				}
			}
		}),
		widget.NewSeparator(),
		widget.NewButton("Refresh", func() {
			workerList.Refresh()
		}),
	)

	return container.NewBorder(nil, nil, nil, actions, workerCard)
}

// createProjectsTab creates the projects tab
func (da *DesktopApp) createProjectsTab() fyne.CanvasObject {
	// Project list with dynamic data
	da.projectList = widget.NewList(
		func() int {
			da.dataMu.RLock()
			defer da.dataMu.RUnlock()
			return len(da.projects)
		},
		func() fyne.CanvasObject {
			return container.NewHBox(
				widget.NewLabel("Template"),
				widget.NewLabel(""),
			)
		},
		func(id widget.ListItemID, obj fyne.CanvasObject) {
			da.dataMu.RLock()
			defer da.dataMu.RUnlock()
			if id < len(da.projects) {
				p := da.projects[id]
				hbox := obj.(*fyne.Container)
				hbox.Objects[0].(*widget.Label).SetText(p.Name)
				activeStatus := ""
				if p.Active {
					activeStatus = " [ACTIVE]"
				}
				hbox.Objects[1].(*widget.Label).SetText(fmt.Sprintf("(%s)%s", p.Type, activeStatus))
			}
		},
	)

	// Project details panel
	// CONST-046: select-prompt resolved via i18n bundle.
	ctxProjects := context.Background()
	projectDetailsLabel := widget.NewLabel(da.tr(ctxProjects, "desktop_projects_select_prompt", nil))
	projectDetailsLabel.Wrapping = fyne.TextWrapWord

	da.projectList.OnSelected = func(id widget.ListItemID) {
		da.dataMu.RLock()
		defer da.dataMu.RUnlock()
		if id < len(da.projects) {
			p := da.projects[id]
			// CONST-046: project-details template resolved via i18n bundle.
			details := da.tr(ctxProjects, "desktop_projects_details_template", map[string]any{
				"Name":         p.Name,
				"Type":         p.Type,
				"Path":         p.Path,
				"Description":  p.Description,
				"Created":      p.CreatedAt.Format(time.RFC822),
				"BuildCommand": p.Metadata.BuildCommand,
				"TestCommand":  p.Metadata.TestCommand,
			})
			projectDetailsLabel.SetText(details)
		}
	}

	projectListCard := widget.NewCard("Projects", "", da.projectList)
	projectDetailsCard := widget.NewCard("Project Details", "", projectDetailsLabel)

	// Project creation form
	nameEntry := widget.NewEntry()
	nameEntry.SetPlaceHolder("Project name")
	descEntry := widget.NewEntry()
	descEntry.SetPlaceHolder("Description")
	pathEntry := widget.NewEntry()
	pathEntry.SetPlaceHolder("/path/to/project")
	typeSelect := widget.NewSelect([]string{"go", "node", "python", "rust", "generic"}, nil)
	typeSelect.SetSelected("go")

	createForm := container.NewVBox(
		// CONST-046: create-project header resolved via i18n bundle.
		widget.NewLabel(da.tr(ctxProjects, "desktop_projects_create_header", nil)),
		widget.NewLabel("Name:"),
		nameEntry,
		widget.NewLabel("Description:"),
		descEntry,
		widget.NewLabel("Path:"),
		pathEntry,
		widget.NewLabel("Type:"),
		typeSelect,
		widget.NewButton("Create Project", func() {
			if da.projectManager != nil && nameEntry.Text != "" && pathEntry.Text != "" {
				ctx := context.Background()
				_, err := da.projectManager.CreateProject(ctx, nameEntry.Text, descEntry.Text, pathEntry.Text, typeSelect.Selected)
				if err != nil {
					dialog.ShowError(err, da.mainWindow)
				} else {
					nameEntry.SetText("")
					descEntry.SetText("")
					pathEntry.SetText("")
					da.refreshData()
					da.projectList.Refresh()
					// CONST-046: success dialog title + body resolved via i18n bundle.
					dialog.ShowInformation(
						da.tr(ctxProjects, "desktop_common_success_title", nil),
						da.tr(ctxProjects, "desktop_projects_created_success", nil),
						da.mainWindow)
				}
			}
		}),
		widget.NewSeparator(),
		widget.NewButton("Set as Active", func() {
			if da.projectList.Length() > 0 {
				da.dataMu.RLock()
				selectedID := -1
				// Get currently selected
				da.dataMu.RUnlock()

				if selectedID >= 0 && selectedID < len(da.projects) {
					ctx := context.Background()
					p := da.projects[selectedID]
					err := da.projectManager.SetActiveProject(ctx, p.ID)
					if err != nil {
						dialog.ShowError(err, da.mainWindow)
					} else {
						da.refreshData()
						da.projectList.Refresh()
					}
				}
			}
		}),
		widget.NewButton("Delete Project", func() {
			// CONST-046: delete-confirm prompt resolved via i18n bundle.
			dialog.ShowConfirm("Confirm Delete", da.tr(ctxProjects, "desktop_projects_delete_confirm", nil), func(confirmed bool) {
				if confirmed {
					// Delete selected project
					da.dataMu.RLock()
					// Implementation would need to track selected index
					da.dataMu.RUnlock()
				}
			}, da.mainWindow)
		}),
		widget.NewButton("Refresh", func() {
			da.refreshData()
			da.projectList.Refresh()
		}),
	)

	leftPanel := container.NewVSplit(projectListCard, projectDetailsCard)
	leftPanel.SetOffset(0.6)

	return container.NewBorder(nil, nil, nil, createForm, leftPanel)
}

// createSessionsTab creates the sessions tab
func (da *DesktopApp) createSessionsTab() fyne.CanvasObject {
	// Session list with dynamic data
	da.sessionList = widget.NewList(
		func() int {
			da.dataMu.RLock()
			defer da.dataMu.RUnlock()
			return len(da.sessions)
		},
		func() fyne.CanvasObject {
			return container.NewHBox(
				widget.NewLabel("Template"),
				widget.NewLabel(""),
			)
		},
		func(id widget.ListItemID, obj fyne.CanvasObject) {
			da.dataMu.RLock()
			defer da.dataMu.RUnlock()
			if id < len(da.sessions) {
				s := da.sessions[id]
				hbox := obj.(*fyne.Container)
				hbox.Objects[0].(*widget.Label).SetText(s.Name)
				hbox.Objects[1].(*widget.Label).SetText(fmt.Sprintf("[%s] %s", s.Status, s.Mode))
			}
		},
	)

	// Session details panel
	// CONST-046: select-prompt resolved via i18n bundle.
	ctxSessions := context.Background()
	sessionDetailsLabel := widget.NewLabel(da.tr(ctxSessions, "desktop_sessions_select_prompt", nil))
	sessionDetailsLabel.Wrapping = fyne.TextWrapWord

	da.sessionList.OnSelected = func(id widget.ListItemID) {
		da.dataMu.RLock()
		defer da.dataMu.RUnlock()
		if id < len(da.sessions) {
			s := da.sessions[id]
			durationStr := s.Duration.String()
			// CONST-046: session-details template resolved via i18n bundle.
			details := da.tr(ctxSessions, "desktop_sessions_details_template", map[string]any{
				"Name":        s.Name,
				"Mode":        s.Mode,
				"Status":      s.Status,
				"ProjectID":   s.ProjectID,
				"Description": s.Description,
				"Created":     s.CreatedAt.Format(time.RFC822),
				"Duration":    durationStr,
			})
			sessionDetailsLabel.SetText(details)
		}
	}

	sessionListCard := widget.NewCard("Sessions", "", da.sessionList)
	sessionDetailsCard := widget.NewCard("Session Details", "", sessionDetailsLabel)

	// Session creation form
	nameEntry := widget.NewEntry()
	nameEntry.SetPlaceHolder("Session name")
	descEntry := widget.NewEntry()
	descEntry.SetPlaceHolder("Description")
	projectIDEntry := widget.NewEntry()
	projectIDEntry.SetPlaceHolder("Project ID")
	modeSelect := widget.NewSelect([]string{"planning", "building", "testing", "refactoring", "debugging", "deployment"}, nil)
	modeSelect.SetSelected("building")

	// Session control buttons (for selected session)
	selectedSessionID := ""
	da.sessionList.OnSelected = func(id widget.ListItemID) {
		da.dataMu.RLock()
		defer da.dataMu.RUnlock()
		if id < len(da.sessions) {
			s := da.sessions[id]
			selectedSessionID = s.ID
			durationStr := s.Duration.String()
			// CONST-046: session-details template resolved via i18n bundle.
			details := da.tr(ctxSessions, "desktop_sessions_details_template", map[string]any{
				"Name":        s.Name,
				"Mode":        s.Mode,
				"Status":      s.Status,
				"ProjectID":   s.ProjectID,
				"Description": s.Description,
				"Created":     s.CreatedAt.Format(time.RFC822),
				"Duration":    durationStr,
			})
			sessionDetailsLabel.SetText(details)
		}
	}

	actions := container.NewVBox(
		// CONST-046: create-session header resolved via i18n bundle.
		widget.NewLabel(da.tr(ctxSessions, "desktop_sessions_create_header", nil)),
		widget.NewLabel("Name:"),
		nameEntry,
		widget.NewLabel("Description:"),
		descEntry,
		widget.NewLabel("Project ID:"),
		projectIDEntry,
		widget.NewLabel("Mode:"),
		modeSelect,
		widget.NewButton("Create Session", func() {
			if da.sessionManager != nil && nameEntry.Text != "" && projectIDEntry.Text != "" {
				mode := session.Mode(modeSelect.Selected)
				_, err := da.sessionManager.Create(projectIDEntry.Text, nameEntry.Text, descEntry.Text, mode)
				if err != nil {
					dialog.ShowError(err, da.mainWindow)
				} else {
					nameEntry.SetText("")
					descEntry.SetText("")
					projectIDEntry.SetText("")
					da.refreshData()
					da.sessionList.Refresh()
					// CONST-046: success dialog title + body resolved via i18n bundle.
					dialog.ShowInformation(
						da.tr(ctxSessions, "desktop_common_success_title", nil),
						da.tr(ctxSessions, "desktop_sessions_created_success", nil),
						da.mainWindow)
				}
			}
		}),
		widget.NewSeparator(),
		// CONST-046: session-controls label resolved via i18n bundle.
		widget.NewLabel(da.tr(ctxSessions, "desktop_sessions_controls_label", nil)),
		widget.NewButton("Start Session", func() {
			if da.sessionManager != nil && selectedSessionID != "" {
				err := da.sessionManager.Start(selectedSessionID)
				if err != nil {
					dialog.ShowError(err, da.mainWindow)
				} else {
					da.refreshData()
					da.sessionList.Refresh()
				}
			}
		}),
		widget.NewButton("Pause Session", func() {
			if da.sessionManager != nil && selectedSessionID != "" {
				err := da.sessionManager.Pause(selectedSessionID)
				if err != nil {
					dialog.ShowError(err, da.mainWindow)
				} else {
					da.refreshData()
					da.sessionList.Refresh()
				}
			}
		}),
		widget.NewButton("Resume Session", func() {
			if da.sessionManager != nil && selectedSessionID != "" {
				err := da.sessionManager.Resume(selectedSessionID)
				if err != nil {
					dialog.ShowError(err, da.mainWindow)
				} else {
					da.refreshData()
					da.sessionList.Refresh()
				}
			}
		}),
		// CONST-046: complete-session button resolved via i18n bundle.
		widget.NewButton(da.tr(ctxSessions, "desktop_sessions_complete_button", nil), func() {
			if da.sessionManager != nil && selectedSessionID != "" {
				err := da.sessionManager.Complete(selectedSessionID)
				if err != nil {
					dialog.ShowError(err, da.mainWindow)
				} else {
					da.refreshData()
					da.sessionList.Refresh()
				}
			}
		}),
		widget.NewSeparator(),
		widget.NewButton("Refresh", func() {
			da.refreshData()
			da.sessionList.Refresh()
		}),
	)

	leftPanel := container.NewVSplit(sessionListCard, sessionDetailsCard)
	leftPanel.SetOffset(0.6)

	return container.NewBorder(nil, nil, nil, actions, leftPanel)
}

// buildVerifierModelChoices enumerates EVERY (provider/model) pair the
// verifier-backed ModelManager actually registered and returns: the display
// labels for the chat model picker ("<provider>/<model>"), a label→model-id
// map, and a label→provider-type map. This is the verifier-driven source that
// REPLACES the desktop's old hardcoded provider list (CONST-036/037, BLUFF-002)
// — a no-key environment yields an empty slice, so the picker is honestly empty
// rather than fake.
func (da *DesktopApp) buildVerifierModelChoices() (labels []string, labelToID map[string]string, labelToProvider map[string]llm.ProviderType) {
	labelToID = make(map[string]string)
	labelToProvider = make(map[string]llm.ProviderType)
	if da.llmManager == nil {
		return labels, labelToID, labelToProvider
	}
	for _, m := range da.llmManager.GetAvailableModels() {
		label := fmt.Sprintf("%s/%s", m.Provider, m.Name)
		if _, exists := labelToID[label]; exists {
			continue
		}
		labels = append(labels, label)
		labelToID[label] = m.ID
		labelToProvider[label] = m.Provider
	}
	sort.Strings(labels)
	return labels, labelToID, labelToProvider
}

// createLLMTab creates the LLM tab
func (da *DesktopApp) createLLMTab() fyne.CanvasObject {
	// Available models list
	modelList := widget.NewList(
		func() int {
			if da.llmManager == nil {
				return 0
			}
			return len(da.llmManager.GetAvailableModels())
		},
		func() fyne.CanvasObject {
			return container.NewHBox(
				widget.NewLabel("Model"),
				widget.NewLabel("Provider"),
			)
		},
		func(id widget.ListItemID, obj fyne.CanvasObject) {
			models := da.llmManager.GetAvailableModels()
			if id < len(models) {
				m := models[id]
				hbox := obj.(*fyne.Container)
				hbox.Objects[0].(*widget.Label).SetText(m.Name)
				hbox.Objects[1].(*widget.Label).SetText(string(m.Provider))
			}
		},
	)

	// CONST-046: "Available Models" card title resolved via i18n bundle.
	ctxLLM := context.Background()
	modelListCard := widget.NewCard(da.tr(ctxLLM, "desktop_models_available_header", nil), "", modelList)

	// Model details panel
	// CONST-046: model-details prompt resolved via i18n bundle.
	modelDetailsLabel := widget.NewLabel(da.tr(ctxLLM, "desktop_models_select_prompt", nil))
	modelDetailsLabel.Wrapping = fyne.TextWrapWord

	modelList.OnSelected = func(id widget.ListItemID) {
		models := da.llmManager.GetAvailableModels()
		if id < len(models) {
			m := models[id]
			caps := make([]string, len(m.Capabilities))
			for i, c := range m.Capabilities {
				caps[i] = string(c)
			}
			// CONST-046: model-details template resolved via i18n bundle.
			details := da.tr(ctxLLM, "desktop_models_details_template", map[string]any{
				"Name":         m.Name,
				"Provider":     m.Provider,
				"ContextSize":  m.ContextSize,
				"Capabilities": fmt.Sprintf("%v", caps),
			})
			modelDetailsLabel.SetText(details)
		}
	}

	modelDetailsCard := widget.NewCard(da.tr(ctxLLM, "desktop_models_details_title", nil), "", modelDetailsLabel)

	// Chat interface
	// CONST-046: chat-history placeholder resolved via i18n bundle.
	da.chatHistory = widget.NewMultiLineEntry()
	da.chatHistory.SetPlaceHolder(da.tr(ctxLLM, "desktop_chat_history_placeholder", nil))
	da.chatHistory.Disable()
	da.chatHistory.Wrapping = fyne.TextWrapWord

	da.chatInput = widget.NewMultiLineEntry()
	// CONST-046: chat input placeholder resolved via i18n bundle.
	da.chatInput.SetPlaceHolder(da.tr(ctxLLM, "desktop_chat_input_placeholder", nil))
	da.chatInput.SetMinRowsVisible(3)

	// Provider/model selection for chat — VERIFIER-DRIVEN (CONST-036/037/040,
	// BLUFF-002). The model picker lists EVERY (provider/model) pair the
	// verifier-backed ModelManager actually registered, so the desktop chat
	// dispatches to a REAL, verified model — NOT the old hardcoded
	// []string{"ollama","openai","anthropic","gemini","local"} list. A no-key
	// environment honestly shows an empty picker rather than fake entries.
	modelChoices, modelLabelToID, modelLabelToProvider := da.buildVerifierModelChoices()
	da.llmProviderSel = widget.NewSelect(modelChoices, nil)
	if len(modelChoices) > 0 {
		da.llmProviderSel.SetSelected(modelChoices[0])
		da.selectedModel = modelLabelToID[modelChoices[0]]
	}
	da.llmProviderSel.OnChanged = func(label string) {
		da.selectedModel = modelLabelToID[label]
	}

	// Manual model-name override (advanced): typed value wins when non-empty,
	// e.g. for a local model not in the verifier catalogue.
	modelNameEntry := widget.NewEntry()
	modelNameEntry.SetPlaceHolder(da.tr(ctxLLM, "desktop_chat_model_name_placeholder", nil))

	sendButton := widget.NewButton("Send Message", func() {
		if da.chatInput.Text == "" {
			return
		}

		// Add user message to history
		currentHistory := da.chatHistory.Text
		userMessage := da.chatInput.Text
		userMsg := fmt.Sprintf("\n[User]: %s\n", userMessage)
		da.chatHistory.SetText(currentHistory + userMsg)

		// Clear input immediately
		da.chatInput.SetText("")

		// Resolve the model id: the manual override wins when non-empty,
		// otherwise the verifier-driven picker selection. Captured up-front so
		// the goroutine reads a stable value.
		modelName := strings.TrimSpace(modelNameEntry.Text)
		if modelName == "" {
			modelName = da.selectedModel
		}
		selectedLabel := da.llmProviderSel.Selected
		providerType := modelLabelToProvider[selectedLabel]

		// Make LLM call in goroutine to not block UI.
		//
		// THREAD-AFFINITY FIX (§11.4.115): this whole body runs OFF the UI
		// goroutine, so every da.chatHistory mutation below is dispatched via
		// the fyneui helpers. Two flavours, deliberately:
		//   - fyneui.Do        — the streaming hot path (per-token appends and
		//                        the prefix that opens the stream). Non-blocking
		//                        so token throughput is never coupled to frame
		//                        time; ordering is preserved by the driver's
		//                        FIFO func-queue.
		//   - fyneui.DoAndWait — one-shot status/terminal appends. Blocking, so
		//                        that when this goroutine returns the UI has
		//                        provably been updated (anything synchronising
		//                        on the turn ending sees the final text).
		// In every case the read and the write live in the SAME closure:
		// `da.chatHistory.Text + …` reads the widget, so splitting them would
		// leave the read racing the renderer.
		go func(msg, model string, ptype llm.ProviderType) {
			if da.llmManager == nil {
				// CONST-046: llm-not-initialized message resolved via i18n bundle.
				notInit := da.tr(ctxLLM, "desktop_chat_llm_not_initialized", map[string]any{
					"Provider": string(ptype),
					"Model":    model,
				}) + "\n"
				fyneui.DoAndWait(func() { da.chatHistory.SetText(da.chatHistory.Text + notInit) })
				return
			}

			provider, err := da.llmManager.GetProviderForModel(model, ptype)
			if err != nil || provider == nil {
				// CONST-046: provider-unavailable message resolved via i18n bundle.
				unavailable := da.tr(ctxLLM, "desktop_chat_provider_unavailable", map[string]any{
					"Provider": string(ptype),
					"Model":    model,
				}) + "\n"
				fyneui.DoAndWait(func() { da.chatHistory.SetText(da.chatHistory.Text + unavailable) })
				return
			}

			ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
			defer cancel()

			prefix := fmt.Sprintf("[AI (%s/%s)]: ", ptype, model)

			// AGENTIC PATH (parity with the TUI, §11.4.74): when the read-only
			// tool registry is wired, drive the chat turn through the shared
			// multi-turn tool loop so prompts like "Check git status" actually
			// execute a read-only git/fs tool and the operator SEES the tool
			// trace + (for ensemble responses) every member's answer and the
			// vote. ReadOnlyOnly=true (§11.4.133): the registry has no approval
			// manager, so the loop is restricted to read-only tools — nothing
			// destructive is reachable.
			if da.agenticTools != nil && da.agenticTools.Registry != nil {
				registry := da.agenticTools.Registry
				systemPrompt := clientcore.BuildToolLoopSystemPrompt(registry)
				history := []llm.Message{{Role: "user", Content: msg}}

				fyneui.Do(func() { da.chatHistory.SetText(da.chatHistory.Text + prefix) })
				onFinalChunk := func(chunk string) {
					// Streaming append: dispatched onto the UI goroutine
					// (widgets are NOT goroutine-safe — see fyneui package doc)
					// and non-blocking so the token stream is not throttled by
					// the render loop.
					fyneui.Do(func() { da.chatHistory.SetText(da.chatHistory.Text + chunk) })
				}
				result, loopErr := agent.RunToolLoopStream(ctx, provider, registry, history, agent.ToolLoopOptions{
					Model:              model,
					MaxTurns:           6,
					SystemPrompt:       systemPrompt,
					MaxToolResultChars: 800,
					ReadOnlyOnly:       true,
				}, onFinalChunk)
				if loopErr != nil {
					errLine := fmt.Sprintf("\n[Error: %v]\n", loopErr)
					fyneui.DoAndWait(func() { da.chatHistory.SetText(da.chatHistory.Text + errLine) })
					return
				}
				fyneui.DoAndWait(func() { da.chatHistory.SetText(da.chatHistory.Text + "\n") })

				// Surface the agentic tool trace so the operator SEES each tool
				// call ("tool: git_status … <real output>").
				if len(result.Trace) > 0 {
					if traceLines := ensembleui.FormatToolTrace(clientcore.AdaptToolTrace(result.Trace)); len(traceLines) > 0 {
						traceText := "\n" + strings.Join(traceLines, "\n") + "\n"
						fyneui.DoAndWait(func() { da.chatHistory.SetText(da.chatHistory.Text + traceText) })
					}
				}

				// Surface the ensemble panel (empty for non-ensemble responses)
				// — the SAME shared renderer the TUI uses — so the operator SEES
				// every member + the winning vote.
				if panelLines := ensembleui.FormatEnsemblePanel(result.FinalMetadata); len(panelLines) > 0 {
					panelText := "\n" + strings.Join(panelLines, "\n") + "\n"
					fyneui.DoAndWait(func() { da.chatHistory.SetText(da.chatHistory.Text + panelText) })
				}
				return
			}

			// PLAIN STREAMING PATH (registry nil) — unchanged, no regression.
			request := &llm.LLMRequest{
				Messages:    []llm.Message{{Role: "user", Content: msg}},
				Model:       model,
				MaxTokens:   1024,
				Temperature: 0.7,
				Stream:      true,
			}
			if streamErr := streamDesktopChat(ctx, provider, request, prefix, da.chatHistory); streamErr != nil {
				errLine := fmt.Sprintf("\n[AI (%s/%s)]: Error: %v\n", ptype, model, streamErr)
				fyneui.DoAndWait(func() { da.chatHistory.SetText(da.chatHistory.Text + errLine) })
			} else {
				fyneui.DoAndWait(func() { da.chatHistory.SetText(da.chatHistory.Text + "\n") })
			}
		}(userMessage, modelName, providerType)
	})

	clearButton := widget.NewButton("Clear Chat", func() {
		da.chatHistory.SetText("")
	})

	chatControls := container.NewVBox(
		widget.NewLabel("Chat Settings:"),
		widget.NewLabel("Provider:"),
		da.llmProviderSel,
		widget.NewLabel("Model:"),
		modelNameEntry,
		widget.NewSeparator(),
		sendButton,
		clearButton,
	)

	chatPanel := container.NewBorder(
		widget.NewLabel("Chat with AI"),
		container.NewBorder(nil, nil, nil, chatControls, da.chatInput),
		nil, nil,
		da.chatHistory,
	)

	chatCard := widget.NewCard("LLM Chat", "", chatPanel)

	// Provider health status
	// CONST-046: provider-health labels resolved via i18n bundle.
	healthLabel := widget.NewLabel(da.tr(ctxLLM, "desktop_health_checking", nil))

	// Start health check goroutine
	go func() {
		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()

		// THREAD-AFFINITY FIX (§11.4.115): background ticker goroutine — the
		// health probe itself stays here, only the resulting label mutation is
		// dispatched onto the UI goroutine.
		checkHealth := func() {
			if da.llmManager == nil {
				noManager := da.tr(ctxLLM, "desktop_health_no_manager", nil)
				fyneui.DoAndWait(func() { healthLabel.SetText(noManager) })
				return
			}
			ctx := context.Background()
			health := da.llmManager.HealthCheck(ctx)
			healthText := da.tr(ctxLLM, "desktop_health_header", nil) + "\n"
			for provider, status := range health {
				healthText += fmt.Sprintf("- %s: %s\n", provider, status.Status)
			}
			if len(health) == 0 {
				healthText += da.tr(ctxLLM, "desktop_health_none_configured", nil)
			}
			fyneui.DoAndWait(func() { healthLabel.SetText(healthText) })
		}

		checkHealth()
		// LIFECYCLE FIX (§11.4.115): same unstoppable-ticker defect as the
		// dashboard stats loop — honour stopUpdate instead of looping forever.
		for fyneui.TickOrStop(ticker.C, da.stopUpdate) {
			checkHealth()
		}
	}()

	healthCard := widget.NewCard("Provider Status", "", healthLabel)

	// Layout
	leftPanel := container.NewVSplit(modelListCard, modelDetailsCard)
	leftPanel.SetOffset(0.5)

	rightPanel := container.NewVSplit(chatCard, healthCard)
	rightPanel.SetOffset(0.7)

	return container.NewHSplit(leftPanel, rightPanel)
}

// createSettingsTab creates the settings tab
func (da *DesktopApp) createSettingsTab() fyne.CanvasObject {
	// CONST-046: settings-tab user-facing strings resolved via i18n bundle.
	ctxSettings := context.Background()
	// Theme selection
	themeInfoLabel := widget.NewLabel("")
	updateThemeInfo := func() {
		currentTheme := da.themeManager.GetCurrentTheme()
		themeInfo := fmt.Sprintf("Name: %s\nDark: %t\nPrimary: %s\nSecondary: %s\nAccent: %s",
			currentTheme.Name, currentTheme.IsDark,
			currentTheme.Primary, currentTheme.Secondary, currentTheme.Accent)
		themeInfoLabel.SetText(themeInfo)
	}

	themeSelect := widget.NewSelect(da.themeManager.GetAvailableThemes(), func(selected string) {
		da.themeManager.SetTheme(selected)
		updateThemeInfo()
	})
	themeSelect.SetSelected(da.themeManager.GetCurrentTheme().Name)

	// CONST-046: theme-card subtitle resolved via i18n bundle.
	themeCard := widget.NewCard("Theme", da.tr(ctxSettings, "desktop_settings_theme_subtitle", nil), themeSelect)

	// Current theme info
	updateThemeInfo()
	infoCard := widget.NewCard("Current Theme", "", themeInfoLabel)

	// Server connection settings
	serverURLEntry := widget.NewEntry()
	serverURLEntry.SetText("http://localhost:8080")
	serverURLEntry.SetPlaceHolder("Server URL")

	serverTimeoutEntry := widget.NewEntry()
	serverTimeoutEntry.SetText("30")
	serverTimeoutEntry.SetPlaceHolder("Timeout (seconds)")

	// CONST-046: server-connection card title resolved via i18n bundle.
	serverCard := widget.NewCard(da.tr(ctxSettings, "desktop_settings_server_title", nil), "",
		container.NewVBox(
			widget.NewLabel("Server URL:"),
			serverURLEntry,
			widget.NewLabel("Timeout (seconds):"),
			serverTimeoutEntry,
			widget.NewButton("Test Connection", func() {
				// CONST-046: connection-test dialog resolved via i18n bundle.
				dialog.ShowInformation(
					da.tr(ctxSettings, "desktop_settings_conn_test_title", nil),
					da.tr(ctxSettings, "desktop_settings_conn_test_body", nil),
					da.mainWindow)
			}),
		),
	)

	// Database settings
	dbHostEntry := widget.NewEntry()
	dbHostEntry.SetPlaceHolder("localhost")
	dbPortEntry := widget.NewEntry()
	dbPortEntry.SetText("5432")
	dbNameEntry := widget.NewEntry()
	dbNameEntry.SetPlaceHolder("helixcode")

	dbCard := widget.NewCard("Database", "",
		container.NewVBox(
			widget.NewLabel("Host:"),
			dbHostEntry,
			widget.NewLabel("Port:"),
			dbPortEntry,
			widget.NewLabel("Database:"),
			dbNameEntry,
		),
	)

	// LLM Provider settings
	ollamaURLEntry := widget.NewEntry()
	ollamaURLEntry.SetText("http://localhost:11434")

	llmCard := widget.NewCard("LLM Providers", "",
		container.NewVBox(
			widget.NewLabel("Ollama URL:"),
			ollamaURLEntry,
			widget.NewLabel("OpenAI API Key:"),
			widget.NewPasswordEntry(),
			// CONST-046: anthropic-key label resolved via i18n bundle.
			widget.NewLabel(da.tr(ctxSettings, "desktop_settings_anthropic_key_label", nil)),
			widget.NewPasswordEntry(),
		),
	)

	// About section
	// CONST-046: about text + card title resolved via i18n bundle.
	aboutLabel := widget.NewLabel(da.tr(ctxSettings, "desktop_settings_about_text", nil))
	aboutLabel.Alignment = fyne.TextAlignCenter
	aboutCard := widget.NewCard(da.tr(ctxSettings, "desktop_settings_about_title", nil), "", aboutLabel)

	// Layout in scrollable container
	settingsContent := container.NewVBox(
		themeCard,
		infoCard,
		serverCard,
		dbCard,
		llmCard,
		aboutCard,
	)

	return container.NewScroll(settingsContent)
}

// Run starts the desktop application
func (da *DesktopApp) Run() {
	// Setup signal handling for graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// Start signal handler in goroutine
	go func() {
		<-sigChan
		da.fyneApp.Quit()
	}()

	// Show window and run (blocks until window closes)
	da.mainWindow.ShowAndRun()
}

// closeJoinTimeout bounds how long Close() waits for the background
// data-update loop to observe stopUpdate and return before proceeding with
// teardown regardless. Chosen well above the loop's own decision latency
// (a non-blocking select-pair, effectively instant) so it only ever fires on
// a genuinely wedged goroutine, never on ordinary shutdown.
const closeJoinTimeout = 5 * time.Second

// Close cleans up resources. It is idempotent: the stopUpdate channel is closed
// at most once via stopOnce, so a second Close call is a clean no-op rather than
// a "close of closed channel" panic.
//
// TEARDOWN-RACE FIX (§11.4.115 / §11.4.108, mirrors the priority-select fix
// on updateLoopTick): closing stopUpdate only signals the background loop to
// stop -- it does NOT wait for the loop to have actually observed the signal
// and returned. Without a join, this function could proceed to close da.db /
// da.agenticTools while the loop goroutine was still mid-refreshData() (or
// about to start one after the priority-select race described on
// updateLoopTick), racing a live DB handle / MCP-LSP child process against
// its own teardown -- a genuine use-after-close hazard, not merely a stray
// extra refresh. The wait below blocks until updateDone closes (the loop's
// own confirmation that it has returned) or closeJoinTimeout elapses,
// whichever comes first.
//
// HONESTY (§11.4.6): this guarantees the loop goroutine has RETURNED before
// teardown begins -- it does not and cannot prove no other goroutine ever
// touches da.db/da.agenticTools after Close (out of scope for this fix), nor
// does it make updateLoopTick's own select-priority race provably zero-width
// (see that method's doc comment). The bounded timeout intentionally trades
// an unbounded hang (if the loop is genuinely wedged) for proceeding with
// teardown anyway, logged, rather than making shutdown hang forever.
func (da *DesktopApp) Close() error {
	// Stop background updates
	if da.stopUpdate != nil {
		da.stopOnce.Do(func() {
			close(da.stopUpdate)
		})
	}

	// JOIN: wait for the update loop goroutine to have fully returned
	// before tearing down da.db / da.agenticTools below. updateDone is nil
	// when startDataUpdates was never called (e.g. Close invoked on a
	// partially-constructed app in a test), so this is a no-op in that case
	// rather than a hang on a channel nothing will ever close.
	if da.updateDone != nil {
		select {
		case <-da.updateDone:
		case <-time.After(closeJoinTimeout):
			log.Printf("Close: timed out after %s waiting for background update loop to stop; proceeding with teardown anyway", closeJoinTimeout)
		}
	}

	// Release the agentic capability sub-managers (MCP child processes, LSP
	// server processes) at parity with the TUI's shutdown. Nil-safe: agenticTools
	// is only assigned when clientcore.WireAgenticTools succeeds (Initialize),
	// so guard before Close to avoid a shutdown-time nil dereference when agentic
	// wiring was unavailable (no gopls/npx, missing MCP config).
	if da.agenticTools != nil {
		da.agenticTools.Close()
	}

	// Close database connection
	if da.db != nil {
		da.db.Close()
	}
	return nil
}

func main() {
	desktopApp := NewDesktopApp()

	// Wire the real CONST-046 translator (embedded active.en.yaml bundle)
	// onto the GUI app BEFORE any window/widget is built, replacing the
	// NoopTranslator{} message-ID-echo default installed by NewDesktopApp.
	// Without this, every dashboard/tab label leaks raw message keys
	// (`desktop_dashboard_header`, `desktop_dashboard_activity_title`, ...)
	// — a §11.4 / CONST-046 PASS-bluff visible on the rendered GUI (HXC-111).
	// The `nogui` main() in main_nogui.go already did this; the GUI main()
	// never did, so only the GUI surface regressed. We call NewTranslator
	// with NO langs so it deterministically defaults to the shipped English
	// bundle and never feeds a malformed host locale (e.g. "en-AT@..." whose
	// "at" subtag Fyne rejects) into go-i18n's accept-language parser. On
	// bundle-load failure the loud NoopTranslator{} echo is preserved (never
	// a silent swallow). Mirrors main_nogui.go.
	if tr, err := i18n.NewTranslator(); err != nil {
		log.Printf("⚠️  i18n: falling back to message-ID echo (bundle load failed): %v", err)
	} else {
		desktopApp.SetTranslator(tr)
	}

	if err := desktopApp.Initialize(); err != nil {
		log.Fatalf("Failed to initialize desktop app: %v", err)
	}
	defer desktopApp.Close()

	desktopApp.Run()
}
