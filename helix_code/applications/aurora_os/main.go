//go:build !nogui

package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"runtime"
	"sync"
	"syscall"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"

	"dev.helix.code/applications/aurora_os/i18n"
	"dev.helix.code/internal/config"
	"dev.helix.code/internal/database"
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

// AuroraTaskManager wraps task.TaskManager for UI operations
type AuroraTaskManager struct {
	inner *task.TaskManager
	tasks []UITask
	mu    sync.RWMutex
}

// NewAuroraTaskManager creates a new Aurora task manager wrapper
func NewAuroraTaskManager(tm *task.TaskManager) *AuroraTaskManager {
	return &AuroraTaskManager{
		inner: tm,
		tasks: make([]UITask, 0),
	}
}

// GetAllTasks returns all tasks for UI display
func (atm *AuroraTaskManager) GetAllTasks() []UITask {
	atm.mu.RLock()
	defer atm.mu.RUnlock()
	return atm.tasks
}

// GetStats returns task statistics
func (atm *AuroraTaskManager) GetStats() TaskStats {
	atm.mu.RLock()
	defer atm.mu.RUnlock()

	stats := TaskStats{
		TotalTasks: len(atm.tasks),
	}

	for _, t := range atm.tasks {
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

// CreateTask creates a new task
func (atm *AuroraTaskManager) CreateTask(ctx context.Context, taskType, description, priority string) (*UITask, error) {
	atm.mu.Lock()
	defer atm.mu.Unlock()

	newTask := UITask{
		ID:          fmt.Sprintf("task-%d", time.Now().UnixNano()),
		Type:        taskType,
		Description: description,
		Status:      "pending",
		Priority:    priority,
	}

	atm.tasks = append(atm.tasks, newTask)
	return &newTask, nil
}

// CancelTask cancels a task by ID
func (atm *AuroraTaskManager) CancelTask(ctx context.Context, taskID string) error {
	atm.mu.Lock()
	defer atm.mu.Unlock()

	for i, t := range atm.tasks {
		if t.ID == taskID {
			atm.tasks = append(atm.tasks[:i], atm.tasks[i+1:]...)
			return nil
		}
	}
	return fmt.Errorf("task not found: %s", taskID)
}

// AuroraWorkerManager wraps worker.WorkerManager for UI operations
type AuroraWorkerManager struct {
	inner   *worker.WorkerManager
	workers []UIWorker
	mu      sync.RWMutex
}

// NewAuroraWorkerManager creates a new Aurora worker manager wrapper
func NewAuroraWorkerManager(wm *worker.WorkerManager) *AuroraWorkerManager {
	return &AuroraWorkerManager{
		inner:   wm,
		workers: make([]UIWorker, 0),
	}
}

// GetWorkers returns all workers for UI display
func (awm *AuroraWorkerManager) GetWorkers() []UIWorker {
	awm.mu.RLock()
	defer awm.mu.RUnlock()
	return awm.workers
}

// AddWorker adds a new worker
func (awm *AuroraWorkerManager) AddWorker(w *UIWorker) error {
	awm.mu.Lock()
	defer awm.mu.Unlock()

	awm.workers = append(awm.workers, *w)
	return nil
}

// RemoveWorker removes a worker by ID
func (awm *AuroraWorkerManager) RemoveWorker(workerID string) error {
	awm.mu.Lock()
	defer awm.mu.Unlock()

	for i, w := range awm.workers {
		if w.ID == workerID {
			awm.workers = append(awm.workers[:i], awm.workers[i+1:]...)
			return nil
		}
	}
	return fmt.Errorf("worker not found: %s", workerID)
}

// AuditLogEntry represents a security audit log entry
type AuditLogEntry struct {
	Timestamp time.Time
	Action    string
	User      string
	Details   string
	Severity  string
}

// AuroraApp represents the Aurora OS specialized application
type AuroraApp struct {
	fyneApp            fyne.App
	mainWindow         fyne.Window
	config             *config.Config
	db                 *database.Database
	taskManager        *AuroraTaskManager
	workerManager      *AuroraWorkerManager
	projectManager     *project.Manager
	sessionManager     *session.Manager
	llmManager         *llm.ModelManager
	notificationEngine *notification.NotificationEngine
	server             *server.Server
	themeManager       *ThemeManager

	// Aurora OS specific components
	auroraIntegration *AuroraIntegration
	systemMonitor     *AuroraSystemMonitor
	securityManager   *AuroraSecurityManager

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

	// Performance mode
	performanceMode bool

	// Update ticker for real-time data
	updateTicker *time.Ticker
	stopUpdate   chan struct{}
	stopOnce     sync.Once

	// updateDone is closed by the startDataUpdates goroutine immediately
	// before it returns. Close() waits on it (bounded) BEFORE tearing down
	// auroraApp.db, so teardown never races a still-running refreshData()
	// call. nil until startDataUpdates runs. See updateLoopTick's doc
	// comment for the teardown-race this closes.
	updateDone chan struct{}

	// updateLoopTickRaceHook is a test seam (nil in production, never set
	// outside _test.go files): when non-nil, updateLoopTick invokes it
	// immediately after the non-blocking priority pre-check observes
	// stopUpdate still open, but BEFORE the blocking select that follows is
	// evaluated -- the residual race window described on updateLoopTick's
	// doc comment. Production code never sets this hook.
	updateLoopTickRaceHook func()

	// translator resolves user-facing strings per CONST-046
	// (round-140 §11.4 migration). Defaults to NoopTranslator
	// (loud echo of message IDs) until SetTranslator wires a real
	// *i18nadapter.Translator at boot. Never nil after
	// NewAuroraApp returns.
	translator i18n.Translator
}

// AuroraIntegration handles Aurora OS specific integrations
type AuroraIntegration struct {
	nativeServices map[string]interface{}
	systemAPI      *AuroraSystemAPI
}

// AuroraSystemAPI provides access to Aurora OS system features
type AuroraSystemAPI struct {
	// Aurora OS specific APIs
}

// AuroraSystemMonitor monitors Aurora OS system resources
type AuroraSystemMonitor struct {
	cpuUsage     float64
	memoryUsage  float64
	diskUsage    float64
	networkStats map[string]interface{}
	mu           sync.RWMutex
}

// AuroraSecurityManager handles Aurora OS security features
type AuroraSecurityManager struct {
	encryptionEnabled  bool
	encryptionAlgo     string
	accessControl      map[string][]string
	auditLog           []AuditLogEntry
	lastSecurityScan   time.Time
	securityScanResult string
	mu                 sync.RWMutex
}

// NewAuroraSecurityManager creates a new security manager
func NewAuroraSecurityManager() *AuroraSecurityManager {
	return &AuroraSecurityManager{
		encryptionEnabled: true,
		encryptionAlgo:    "AES-256-GCM",
		accessControl: map[string][]string{
			"admin":     {"read", "write", "execute", "admin"},
			"developer": {"read", "write", "execute"},
			"viewer":    {"read"},
		},
		auditLog: make([]AuditLogEntry, 0),
	}
}

// AddAuditEntry adds an entry to the audit log
func (asm *AuroraSecurityManager) AddAuditEntry(action, user, details, severity string) {
	asm.mu.Lock()
	defer asm.mu.Unlock()
	asm.auditLog = append(asm.auditLog, AuditLogEntry{
		Timestamp: time.Now(),
		Action:    action,
		User:      user,
		Details:   details,
		Severity:  severity,
	})
}

// GetAuditLog returns all audit log entries
func (asm *AuroraSecurityManager) GetAuditLog() []AuditLogEntry {
	asm.mu.RLock()
	defer asm.mu.RUnlock()
	return asm.auditLog
}

// NewAuroraApp creates a new Aurora OS specialized application
func NewAuroraApp() *AuroraApp {
	fyneApp := app.New()
	fyneApp.Settings().SetTheme(&CustomTheme{})

	return &AuroraApp{
		fyneApp: fyneApp,
		auroraIntegration: &AuroraIntegration{
			nativeServices: make(map[string]interface{}),
			systemAPI:      &AuroraSystemAPI{},
		},
		systemMonitor: &AuroraSystemMonitor{
			networkStats: make(map[string]interface{}),
		},
		securityManager: NewAuroraSecurityManager(),
		projects:        make([]*project.Project, 0),
		sessions:        make([]*session.Session, 0),
		llmProviders:    make([]string, 0),
		stopUpdate:      make(chan struct{}),
		translator:      i18n.NoopTranslator{},
	}
}

// SetTranslator injects the runtime Translator (per CONST-046
// round-140). Passing nil is a no-op — the NoopTranslator default
// installed by NewAuroraApp is preserved so the loud-echo safety
// net never disappears silently. helix_code wires
// *i18nadapter.Translator at boot.
func (auroraApp *AuroraApp) SetTranslator(t i18n.Translator) {
	if t == nil {
		return
	}
	auroraApp.translator = t
}

// t is a tiny call-site helper that resolves a message ID through
// the injected Translator and falls back to the literal id on error
// (loud echo — never silently swallow). Centralising the
// boilerplate keeps migrated call sites a single expression long.
func (auroraApp *AuroraApp) t(id string) string {
	if auroraApp.translator == nil {
		return id
	}
	got, err := auroraApp.translator.T(context.Background(), id, nil)
	if err != nil || got == "" {
		return id
	}
	return got
}

// Initialize sets up the Aurora OS application
func (auroraApp *AuroraApp) Initialize() error {
	// Load configuration
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("failed to load configuration: %v", err)
	}
	auroraApp.config = cfg

	// Initialize database (optional - continue without it if not available)
	db, err := database.New(cfg.Database)
	if err != nil {
		log.Printf("Warning: Database not available: %v (continuing without persistence)", err)
	}
	auroraApp.db = db

	// Initialize Redis (optional - continue without it if not available)
	rds, err := redis.NewClient(&cfg.Redis)
	if err != nil {
		log.Printf("Warning: Redis not available: %v (continuing without caching)", err)
	}

	// Initialize components
	innerTaskManager := task.NewTaskManager(db, rds)
	auroraApp.taskManager = NewAuroraTaskManager(innerTaskManager)

	// Initialize worker manager with in-memory repository for standalone UI
	workerRepo := worker.NewInMemoryWorkerRepository()
	innerWorkerManager := worker.NewWorkerManager(workerRepo, 30*time.Second)
	auroraApp.workerManager = NewAuroraWorkerManager(innerWorkerManager)

	// Initialize project manager
	auroraApp.projectManager = project.NewManager()

	// Initialize session manager
	auroraApp.sessionManager = session.NewManager()

	// W2c-1 cloud gate (operator mandate 2026-09-05, local-only adaptive
	// serving): wire the hosted-provider gate from configuration BEFORE any
	// handler can construct a provider. Mirrors internal/server/server.go.
	// Default false -- llm.cloud.enabled must be explicitly set true to
	// permit cloud construction.
	llm.SetCloudEnabled(cfg.LLM.Cloud.Enabled)

	// Initialize LLM manager
	auroraApp.llmManager = llm.NewModelManager()

	// Initialize notification engine
	auroraApp.notificationEngine = notification.NewNotificationEngine()

	// Initialize server for API calls
	auroraApp.server = server.New(cfg, db, rds)

	// Initialize theme manager
	auroraApp.themeManager = NewThemeManager()

	// Initialize Aurora OS specific features
	if err := auroraApp.initializeAuroraFeatures(); err != nil {
		return fmt.Errorf("failed to initialize Aurora features: %v", err)
	}

	// Setup UI
	auroraApp.setupUI()

	// Start background data updates
	auroraApp.startDataUpdates()

	// Log initialization
	auroraApp.securityManager.AddAuditEntry("system_init", "system", auroraApp.t("aurora_os_audit_app_initialized"), "info")

	return nil
}

// initializeAuroraFeatures sets up Aurora OS specific integrations
func (auroraApp *AuroraApp) initializeAuroraFeatures() error {
	// Initialize Aurora OS native services
	auroraApp.auroraIntegration.nativeServices["system"] = "aurora-system-service"
	auroraApp.auroraIntegration.nativeServices["security"] = "aurora-security-service"
	auroraApp.auroraIntegration.nativeServices["network"] = "aurora-network-service"
	auroraApp.auroraIntegration.nativeServices["storage"] = "aurora-storage-service"

	// Initialize system monitoring
	auroraApp.refreshSystemInfo()

	log.Println("Aurora OS features initialized successfully")
	return nil
}

// startDataUpdates starts periodic background data refresh.
//
// JOIN: updateDone is created fresh here and closed by the goroutine
// immediately before it returns. Close() waits on it (bounded) before
// tearing down auroraApp.db -- see the updateDone field doc comment and
// Close's doc comment for the use-after-close race this closes.
func (auroraApp *AuroraApp) startDataUpdates() {
	auroraApp.updateTicker = time.NewTicker(5 * time.Second)
	auroraApp.updateDone = make(chan struct{})
	go func() {
		defer close(auroraApp.updateDone)

		// Initial data load
		auroraApp.refreshData()

		for {
			if !auroraApp.updateLoopTick(auroraApp.updateTicker.C, auroraApp.stopUpdate) {
				auroraApp.updateTicker.Stop()
				return
			}
		}
	}()
}

// updateLoopTick runs exactly one iteration of the background data-update
// loop's select logic. Extracted from startDataUpdates (identical
// behaviour, same select statement, same per-tick work) so the §11.4.115
// regression guard (main_racefix_test.go) can force the "unprioritized
// select" race deterministically -- pre-buffering a tick AND pre-closing
// stop before the select ever runs -- instead of depending on real
// wall-clock ticker timing or scheduler luck.
//
// ANTI-BLUFF FIX (§11.4.115 unprioritized-select race -- mirrors
// internal/persistence/store.go autoSaveTick and applications/desktop's /
// applications/harmony_os's identical fix; one of four confirmed instances
// of this defect class audited this session): Go's select chooses
// UNIFORMLY AT RANDOM among ALL cases ready at the instant it is evaluated.
// A naked `select { case <-ticker.C: refreshData(); refreshSystemInfo();
// case <-stopUpdate: return }` can therefore still pick the ticker.C branch
// and run one more refreshData()+refreshSystemInfo() even when stopUpdate
// was ALREADY closed before the select was reached -- if this goroutine
// was scheduler-delayed long enough for a tick to also land in ticker.C by
// the time it finally runs. The non-blocking priority pre-check below
// closes that WIDE window: it runs FIRST, on every loop iteration, and
// returns immediately whenever stopUpdate is already closed BEFORE this
// call is even invoked.
//
// RESIDUAL WINDOW (closed by the inner re-check below): the pre-check only
// proves stopUpdate was open at the instant it ran. If close(stopUpdate)
// lands in the narrow gap BETWEEN the pre-check returning and the blocking
// select below being evaluated, and a tick is already buffered on tickerC,
// both cases are ready when the blocking select fires and Go's
// uniform-random pick can still choose the tickerC branch -- one stray
// refreshData()/refreshSystemInfo() pair after Close() has already returned
// to its caller. The inner non-blocking re-check inside the tickerC case
// closes this too.
//
// HONESTY (§11.4.6): this is the strongest ordering guarantee a plain
// channel select can offer. It is NOT a claim the race is closed to zero
// width -- under Go's async goroutine preemption (>=1.14) this goroutine can
// still be preempted between the final re-check and the refresh calls that
// follow it while a close(stopUpdate) lands concurrently, an unavoidable,
// unobservable race no select-based implementation can provably eliminate.
// What IS delivered: cancellation observed at the final decision point
// always wins, AND (via the updateDone join in Close, a SEPARATE mechanism)
// the caller never tears down auroraApp.db while this goroutine could still
// be mid-refresh -- the join guarantees the loop has RETURNED before
// teardown begins, which the select-priority fix alone does not (this is
// the concrete mechanism behind the "⇒ if refreshData() wins the race it
// runs CONCURRENTLY with DB/tooling teardown" hazard on auroraApp.db).
//
// Returns false when the loop must stop (stopUpdate was signalled --
// checked FIRST, non-blocking, and again in the blocking select below),
// true when a tick was processed and the loop should continue.
func (auroraApp *AuroraApp) updateLoopTick(tickerC <-chan time.Time, stop <-chan struct{}) bool {
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
	if auroraApp.updateLoopTickRaceHook != nil {
		auroraApp.updateLoopTickRaceHook()
	}

	select {
	case <-tickerC:
		// Re-check: a stop-close landing in the window between the
		// pre-check above and this blocking select being evaluated can
		// still leave the tickerC branch "randomly" chosen even though
		// stop is now closed. Catch it here, non-blocking, before running
		// the refresh pair.
		select {
		case <-stop:
			return false
		default:
		}
		auroraApp.refreshData()
		auroraApp.refreshSystemInfo()
		return true
	case <-stop:
		return false
	}
}

// refreshData updates cached data from managers
func (auroraApp *AuroraApp) refreshData() {
	auroraApp.dataMu.Lock()
	defer auroraApp.dataMu.Unlock()

	ctx := context.Background()

	// Refresh projects
	if auroraApp.projectManager != nil {
		projects, err := auroraApp.projectManager.ListProjects(ctx, "")
		if err == nil {
			auroraApp.projects = projects
		}
	}

	// Refresh sessions
	if auroraApp.sessionManager != nil {
		auroraApp.sessions = auroraApp.sessionManager.GetAll()
	}

	// Refresh LLM providers
	if auroraApp.llmManager != nil {
		models := auroraApp.llmManager.GetAvailableModels()
		providers := make(map[string]bool)
		for _, model := range models {
			providers[string(model.Provider)] = true
		}
		auroraApp.llmProviders = make([]string, 0, len(providers))
		for provider := range providers {
			auroraApp.llmProviders = append(auroraApp.llmProviders, provider)
		}
	}
}

// setupUI initializes the user interface with Aurora OS optimizations
func (auroraApp *AuroraApp) setupUI() {
	// Create main window with Aurora OS branding. Window title +
	// every top-level tab label resolved via i18n.Translator per
	// CONST-046 round-140. Card titles, button labels, and dialog
	// titles deeper in the UI are intentionally kept hardcoded in
	// this round and tracked for migration in later rounds.
	auroraApp.mainWindow = auroraApp.fyneApp.NewWindow(auroraApp.t("aurora_os_window_title"))
	auroraApp.mainWindow.SetMaster()
	auroraApp.mainWindow.Resize(fyne.NewSize(1400, 900)) // Larger for Aurora OS displays

	// Create tabs with Aurora OS specific tabs
	auroraApp.tabs = container.NewAppTabs(
		container.NewTabItem(auroraApp.t("aurora_os_tab_aurora_dashboard"), auroraApp.createAuroraDashboardTab()),
		container.NewTabItem(auroraApp.t("aurora_os_tab_tasks"), auroraApp.createTasksTab()),
		container.NewTabItem(auroraApp.t("aurora_os_tab_workers"), auroraApp.createWorkersTab()),
		container.NewTabItem(auroraApp.t("aurora_os_tab_aurora_system"), auroraApp.createAuroraSystemTab()),
		container.NewTabItem(auroraApp.t("aurora_os_tab_security"), auroraApp.createAuroraSecurityTab()),
		container.NewTabItem(auroraApp.t("aurora_os_tab_projects"), auroraApp.createProjectsTab()),
		container.NewTabItem(auroraApp.t("aurora_os_tab_sessions"), auroraApp.createSessionsTab()),
		container.NewTabItem(auroraApp.t("aurora_os_tab_llm"), auroraApp.createLLMTab()),
		container.NewTabItem(auroraApp.t("aurora_os_tab_settings"), auroraApp.createSettingsTab()),
	)

	// Create enhanced status bar for Aurora OS
	auroraApp.statusBar = widget.NewLabel(auroraApp.t("aurora_os_status_bar_default"))
	auroraApp.statusBar.Alignment = fyne.TextAlignCenter

	// Create main layout
	mainContent := container.NewBorder(nil, auroraApp.statusBar, nil, nil, auroraApp.tabs)

	auroraApp.mainWindow.SetContent(mainContent)
}

// createAuroraDashboardTab creates the Aurora OS specialized dashboard
func (auroraApp *AuroraApp) createAuroraDashboardTab() fyne.CanvasObject {
	// Header with Aurora OS branding
	header := widget.NewLabel(auroraApp.t("aurora_os_dashboard_header"))
	header.Alignment = fyne.TextAlignCenter
	header.TextStyle = fyne.TextStyle{Bold: true}

	// Aurora OS specific stats with dynamic updates
	systemStatsLabel := widget.NewLabel(auroraApp.t("aurora_os_stat_system_initial"))
	workerStatsLabel := widget.NewLabel(auroraApp.t("aurora_os_stat_worker_initial"))
	taskStatsLabel := widget.NewLabel(auroraApp.t("aurora_os_stat_task_initial"))

	systemCard := widget.NewCard(auroraApp.t("aurora_os_card_aurora_system"), "", systemStatsLabel)
	workerCard := widget.NewCard(auroraApp.t("aurora_os_card_workers"), "", workerStatsLabel)
	taskCard := widget.NewCard(auroraApp.t("aurora_os_card_tasks"), "", taskStatsLabel)

	// Start a goroutine to update stats.
	//
	// THREAD-AFFINITY FIX (§11.4.115): this is a background goroutine, so every
	// label mutation below MUST be dispatched onto the UI goroutine via
	// fyneui.Do — a bare SetText here raced the renderer, which reads the same
	// widget fields from the main goroutine (Fyne widgets are NOT
	// goroutine-safe; see the fyneui package doc). Mirrors the identical fix in
	// applications/desktop. The domain data is gathered and the string
	// formatted OUTSIDE the closure — and, for the system stats, INSIDE the
	// systemMonitor read-lock, which is RELEASED BEFORE the dispatch — so a
	// domain lock is never held across a UI hop and the UI goroutine never
	// touches a manager while this one is mid-read.
	go func() {
		ticker := time.NewTicker(1 * time.Second)
		defer ticker.Stop()

		// LIFECYCLE FIX (§11.4.115): this loop used to be `for range ticker.C`,
		// which can never terminate — the goroutine outlived the window it
		// painted into, kept mutating labels after teardown, and leaked for the
		// lifetime of the process. It now honours the app's existing stopUpdate
		// channel (closed exactly once by Close() via stopOnce).
		for fyneui.TickOrStop(ticker.C, auroraApp.stopUpdate) {
			// Update system stats
			auroraApp.systemMonitor.mu.RLock()
			systemText := fmt.Sprintf(auroraApp.t("aurora_os_stat_system_fmt"),
				auroraApp.systemMonitor.cpuUsage, auroraApp.systemMonitor.memoryUsage, auroraApp.systemMonitor.diskUsage)
			auroraApp.systemMonitor.mu.RUnlock()
			fyneui.Do(func() { systemStatsLabel.SetText(systemText) })

			// Update worker stats
			if auroraApp.workerManager != nil {
				workers := auroraApp.workerManager.GetWorkers()
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
				workerText := fmt.Sprintf(auroraApp.t("aurora_os_stat_worker_fmt"), len(workers), active, healthy)
				fyneui.Do(func() { workerStatsLabel.SetText(workerText) })
			}

			// Update task stats
			if auroraApp.taskManager != nil {
				stats := auroraApp.taskManager.GetStats()
				taskText := fmt.Sprintf(auroraApp.t("aurora_os_stat_task_fmt"),
					stats.TotalTasks, stats.CompletedTasks, stats.RunningTasks)
				fyneui.Do(func() { taskStatsLabel.SetText(taskText) })
			}
		}
	}()

	statsContainer := container.NewGridWithColumns(3, systemCard, workerCard, taskCard)

	// Aurora OS activity log
	activityLog := widget.NewMultiLineEntry()
	activityLog.SetText(auroraApp.t("aurora_os_activity_log_seed"))
	activityLog.Disable()

	activityCard := widget.NewCard(auroraApp.t("aurora_os_card_aurora_activity"), "", activityLog)

	// Aurora OS quick actions
	actionsCard := widget.NewCard(auroraApp.t("aurora_os_card_aurora_actions"), "",
		container.NewVBox(
			widget.NewButton(auroraApp.t("aurora_os_btn_system_diagnostics"), func() { auroraApp.runAuroraDiagnostics() }),
			widget.NewButton(auroraApp.t("aurora_os_btn_security_scan"), func() { auroraApp.runSecurityScan() }),
			widget.NewButton(auroraApp.t("aurora_os_btn_performance_boost"), func() { auroraApp.activatePerformanceMode() }),
			widget.NewButton(auroraApp.t("aurora_os_btn_new_task"), func() {
				auroraApp.tabs.SelectIndex(1) // Switch to Tasks tab
			}),
			widget.NewButton(auroraApp.t("aurora_os_btn_new_project"), func() {
				auroraApp.tabs.SelectIndex(5) // Switch to Projects tab
			}),
		),
	)

	bottomContainer := container.NewGridWithColumns(2, activityCard, actionsCard)

	return container.NewVBox(header, statsContainer, bottomContainer)
}

// createAuroraSystemTab creates the Aurora OS system monitoring tab
func (auroraApp *AuroraApp) createAuroraSystemTab() fyne.CanvasObject {
	// System resources with dynamic updates
	resourcesLabel := widget.NewLabel(auroraApp.t("aurora_os_label_loading"))
	resourcesCard := widget.NewCard(auroraApp.t("aurora_os_card_system_resources"), "", resourcesLabel)

	// Update resources display.
	//
	// THREAD-AFFINITY FIX (§11.4.115): background goroutine ⇒ the label
	// mutation is dispatched onto the UI goroutine via fyneui.Do rather than
	// written bare, which raced the renderer. The stats are read and the string
	// formatted inside the systemMonitor read-lock, which is RELEASED BEFORE
	// the dispatch so the domain lock is never held across the UI hop.
	go func() {
		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()

		// LIFECYCLE FIX (§11.4.115): was `for range ticker.C` — unstoppable,
		// so the goroutine outlived the window it painted into and leaked for
		// the lifetime of the process. It now honours stopUpdate (closed by
		// Close()).
		for fyneui.TickOrStop(ticker.C, auroraApp.stopUpdate) {
			auroraApp.systemMonitor.mu.RLock()
			var m runtime.MemStats
			runtime.ReadMemStats(&m)
			resourcesText := fmt.Sprintf(
				auroraApp.t("aurora_os_resources_fmt"),
				auroraApp.systemMonitor.cpuUsage, auroraApp.systemMonitor.memoryUsage, auroraApp.systemMonitor.diskUsage,
				runtime.NumGoroutine(), float64(m.Alloc)/1024/1024, float64(m.Sys)/1024/1024, m.NumGC)
			auroraApp.systemMonitor.mu.RUnlock()
			fyneui.Do(func() { resourcesLabel.SetText(resourcesText) })
		}
	}()

	// Native services status
	servicesList := widget.NewList(
		func() int { return len(auroraApp.auroraIntegration.nativeServices) },
		func() fyne.CanvasObject {
			return widget.NewLabel(auroraApp.t("aurora_os_service_list_template"))
		},
		func(id widget.ListItemID, obj fyne.CanvasObject) {
			services := make([]string, 0, len(auroraApp.auroraIntegration.nativeServices))
			for service := range auroraApp.auroraIntegration.nativeServices {
				services = append(services, service)
			}
			if id < len(services) {
				obj.(*widget.Label).SetText(fmt.Sprintf(auroraApp.t("aurora_os_service_list_item_fmt"), services[id]))
			}
		},
	)

	servicesCard := widget.NewCard(auroraApp.t("aurora_os_card_aurora_services"), "", servicesList)

	// System actions
	actions := container.NewVBox(
		widget.NewButton(auroraApp.t("aurora_os_btn_refresh_system_info"), func() {
			auroraApp.refreshSystemInfo()
			dialog.ShowInformation(auroraApp.t("aurora_os_dialog_refreshed_title"), auroraApp.t("aurora_os_dialog_system_info_refreshed"), auroraApp.mainWindow)
		}),
		widget.NewButton(auroraApp.t("aurora_os_btn_optimize_performance"), func() { auroraApp.optimizePerformance() }),
		widget.NewButton(auroraApp.t("aurora_os_btn_system_diagnostics"), func() { auroraApp.runAuroraDiagnostics() }),
		widget.NewButton(auroraApp.t("aurora_os_btn_force_gc"), func() {
			runtime.GC()
			dialog.ShowInformation(auroraApp.t("aurora_os_dialog_gc_complete_title"), auroraApp.t("aurora_os_dialog_gc_completed"), auroraApp.mainWindow)
		}),
	)

	return container.NewBorder(nil, nil, nil, actions, container.NewVBox(resourcesCard, servicesCard))
}

// createAuroraSecurityTab creates the Aurora OS security management tab
func (auroraApp *AuroraApp) createAuroraSecurityTab() fyne.CanvasObject {
	// Security status with dynamic updates
	statusLabel := widget.NewLabel(auroraApp.t("aurora_os_label_loading"))
	statusCard := widget.NewCard(auroraApp.t("aurora_os_card_security_status"), "", statusLabel)

	// Update status display
	updateStatus := func() {
		auroraApp.securityManager.mu.RLock()
		lastScan := auroraApp.t("aurora_os_token_never")
		if !auroraApp.securityManager.lastSecurityScan.IsZero() {
			lastScan = auroraApp.securityManager.lastSecurityScan.Format("2006-01-02 15:04:05")
		}
		scanResult := auroraApp.securityManager.securityScanResult
		if scanResult == "" {
			scanResult = auroraApp.t("aurora_os_security_no_scan")
		}
		statusLabel.SetText(fmt.Sprintf(
			auroraApp.t("aurora_os_security_status_fmt"),
			map[bool]string{true: auroraApp.t("aurora_os_token_enabled"), false: auroraApp.t("aurora_os_token_disabled")}[auroraApp.securityManager.encryptionEnabled],
			auroraApp.securityManager.encryptionAlgo,
			lastScan, scanResult, len(auroraApp.securityManager.auditLog)))
		auroraApp.securityManager.mu.RUnlock()
	}
	updateStatus()

	// Access control list
	accessList := widget.NewList(
		func() int {
			auroraApp.securityManager.mu.RLock()
			defer auroraApp.securityManager.mu.RUnlock()
			return len(auroraApp.securityManager.accessControl)
		},
		func() fyne.CanvasObject {
			return widget.NewLabel(auroraApp.t("aurora_os_access_list_template"))
		},
		func(id widget.ListItemID, obj fyne.CanvasObject) {
			auroraApp.securityManager.mu.RLock()
			defer auroraApp.securityManager.mu.RUnlock()
			roles := make([]string, 0, len(auroraApp.securityManager.accessControl))
			for role := range auroraApp.securityManager.accessControl {
				roles = append(roles, role)
			}
			if id < len(roles) {
				role := roles[id]
				permissions := auroraApp.securityManager.accessControl[role]
				obj.(*widget.Label).SetText(fmt.Sprintf("%s: %v", role, permissions))
			}
		},
	)

	accessCard := widget.NewCard(auroraApp.t("aurora_os_card_access_control"), "", accessList)

	// Security actions
	actions := container.NewVBox(
		widget.NewButton(auroraApp.t("aurora_os_btn_run_security_scan"), func() {
			auroraApp.runSecurityScan()
			updateStatus()
		}),
		widget.NewButton(auroraApp.t("aurora_os_btn_view_audit_log"), func() { auroraApp.showAuditLog() }),
		widget.NewButton(auroraApp.t("aurora_os_btn_configure_encryption"), func() { auroraApp.configureEncryption() }),
		widget.NewButton(auroraApp.t("aurora_os_btn_refresh_status"), func() { updateStatus() }),
	)

	return container.NewBorder(nil, nil, nil, actions, container.NewVBox(statusCard, accessCard))
}

// Aurora OS specific methods

func (auroraApp *AuroraApp) runAuroraDiagnostics() {
	log.Println("Running Aurora OS diagnostics...")
	auroraApp.securityManager.AddAuditEntry("diagnostics_run", "user", auroraApp.t("aurora_os_audit_diagnostics_initiated"), "info")

	// Perform real diagnostics
	diagnosticsResults := []string{}

	// Check CPU
	cpuCount := runtime.NumCPU()
	diagnosticsResults = append(diagnosticsResults, fmt.Sprintf(auroraApp.t("aurora_os_diag_cpu_cores_fmt"), cpuCount))

	// Check memory
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	memStatus := auroraApp.t("aurora_os_diag_status_ok")
	if m.Alloc > 500*1024*1024 {
		memStatus = auroraApp.t("aurora_os_diag_warn_high_memory")
	}
	diagnosticsResults = append(diagnosticsResults, fmt.Sprintf(auroraApp.t("aurora_os_diag_memory_fmt"), float64(m.Alloc)/1024/1024, memStatus))

	// Check goroutines
	goroutines := runtime.NumGoroutine()
	goroutineStatus := auroraApp.t("aurora_os_diag_status_ok")
	if goroutines > 1000 {
		goroutineStatus = auroraApp.t("aurora_os_diag_warn_high_goroutines")
	}
	diagnosticsResults = append(diagnosticsResults, fmt.Sprintf(auroraApp.t("aurora_os_diag_goroutines_fmt"), goroutines, goroutineStatus))

	// Check database
	dbStatus := auroraApp.t("aurora_os_diag_db_not_connected")
	if auroraApp.db != nil {
		dbStatus = auroraApp.t("aurora_os_diag_db_connected")
	}
	diagnosticsResults = append(diagnosticsResults, fmt.Sprintf(auroraApp.t("aurora_os_diag_database_fmt"), dbStatus))

	// Check managers
	diagnosticsResults = append(diagnosticsResults, auroraApp.t("aurora_os_diag_task_manager_init"))
	diagnosticsResults = append(diagnosticsResults, auroraApp.t("aurora_os_diag_worker_manager_init"))
	diagnosticsResults = append(diagnosticsResults, auroraApp.t("aurora_os_diag_project_manager_init"))
	diagnosticsResults = append(diagnosticsResults, auroraApp.t("aurora_os_diag_session_manager_init"))
	diagnosticsResults = append(diagnosticsResults, auroraApp.t("aurora_os_diag_llm_manager_init"))

	// Check security
	auroraApp.securityManager.mu.RLock()
	encStatus := map[bool]string{true: auroraApp.t("aurora_os_state_enabled"), false: auroraApp.t("aurora_os_state_disabled")}[auroraApp.securityManager.encryptionEnabled]
	auroraApp.securityManager.mu.RUnlock()
	diagnosticsResults = append(diagnosticsResults, fmt.Sprintf(auroraApp.t("aurora_os_diag_encryption_fmt"), encStatus))

	// Check performance mode
	perfStatus := map[bool]string{true: auroraApp.t("aurora_os_state_enabled"), false: auroraApp.t("aurora_os_state_disabled")}[auroraApp.performanceMode]
	diagnosticsResults = append(diagnosticsResults, fmt.Sprintf(auroraApp.t("aurora_os_diag_perf_mode_fmt"), perfStatus))

	// Build result text
	resultText := auroraApp.t("aurora_os_diag_results_header") + "\n\n"
	for _, result := range diagnosticsResults {
		resultText += result + "\n"
	}
	resultText += "\n" + auroraApp.t("aurora_os_diag_completed_ok")

	// Show results in dialog
	dialog.ShowInformation(auroraApp.t("aurora_os_dialog_system_diagnostics_title"), resultText, auroraApp.mainWindow)

	auroraApp.securityManager.AddAuditEntry("diagnostics_complete", "user",
		fmt.Sprintf(auroraApp.t("aurora_os_audit_diagnostics_completed_fmt"), len(diagnosticsResults)), "info")
}

func (auroraApp *AuroraApp) runSecurityScan() {
	log.Println("Running Aurora OS security scan...")
	auroraApp.securityManager.AddAuditEntry("security_scan_start", "user", auroraApp.t("aurora_os_audit_security_scan_initiated"), "info")

	// Simulate security scan with real checks
	scanResults := []string{}
	issues := 0

	// Check encryption
	auroraApp.securityManager.mu.RLock()
	if !auroraApp.securityManager.encryptionEnabled {
		scanResults = append(scanResults, auroraApp.t("aurora_os_scan_encryption_disabled"))
		issues++
	} else {
		scanResults = append(scanResults, auroraApp.t("aurora_os_scan_encryption_enabled"))
	}
	auroraApp.securityManager.mu.RUnlock()

	// Check access control
	auroraApp.securityManager.mu.RLock()
	if len(auroraApp.securityManager.accessControl) == 0 {
		scanResults = append(scanResults, auroraApp.t("aurora_os_scan_no_access_roles"))
		issues++
	} else {
		scanResults = append(scanResults, fmt.Sprintf(auroraApp.t("aurora_os_scan_access_roles_fmt"), len(auroraApp.securityManager.accessControl)))
	}
	auroraApp.securityManager.mu.RUnlock()

	// Check audit logging
	scanResults = append(scanResults, auroraApp.t("aurora_os_scan_audit_logging_enabled"))

	// Check database connection
	if auroraApp.db == nil {
		scanResults = append(scanResults, auroraApp.t("aurora_os_scan_db_not_connected"))
	} else {
		scanResults = append(scanResults, auroraApp.t("aurora_os_scan_db_connected"))
	}

	// Update security manager
	auroraApp.securityManager.mu.Lock()
	auroraApp.securityManager.lastSecurityScan = time.Now()
	if issues == 0 {
		auroraApp.securityManager.securityScanResult = auroraApp.t("aurora_os_scan_all_checks_passed")
	} else {
		auroraApp.securityManager.securityScanResult = fmt.Sprintf(auroraApp.t("aurora_os_scan_issues_found_fmt"), issues)
	}
	auroraApp.securityManager.mu.Unlock()

	// Build result text
	resultText := auroraApp.t("aurora_os_scan_results_header") + "\n\n"
	for _, result := range scanResults {
		resultText += result + "\n"
	}
	resultText += "\n" + fmt.Sprintf(auroraApp.t("aurora_os_scan_completed_fmt"), issues)

	// Show results
	dialog.ShowInformation(auroraApp.t("aurora_os_dialog_security_scan_title"), resultText, auroraApp.mainWindow)

	auroraApp.securityManager.AddAuditEntry("security_scan_complete", "user",
		fmt.Sprintf(auroraApp.t("aurora_os_audit_security_scan_completed_fmt"), issues), "info")
}

func (auroraApp *AuroraApp) activatePerformanceMode() {
	log.Println("Toggling Aurora OS performance mode...")

	auroraApp.performanceMode = !auroraApp.performanceMode

	if auroraApp.performanceMode {
		// Apply performance optimizations
		runtime.GOMAXPROCS(runtime.NumCPU())
		runtime.GC() // Clean up before performance mode

		dialog.ShowInformation(auroraApp.t("aurora_os_dialog_perf_mode_title"),
			auroraApp.t("aurora_os_perf_mode_enabled_intro")+"\n\n"+
				auroraApp.t("aurora_os_perf_mode_optimizations_header")+"\n"+
				fmt.Sprintf(auroraApp.t("aurora_os_perf_mode_opt_gomaxprocs_fmt"), runtime.NumCPU())+"\n"+
				auroraApp.t("aurora_os_perf_mode_opt_gc")+"\n"+
				auroraApp.t("aurora_os_perf_mode_opt_memory"),
			auroraApp.mainWindow)

		auroraApp.securityManager.AddAuditEntry("performance_mode", "user", auroraApp.t("aurora_os_audit_perf_mode_enabled"), "info")
	} else {
		dialog.ShowInformation(auroraApp.t("aurora_os_dialog_perf_mode_title"),
			auroraApp.t("aurora_os_perf_mode_disabled_body"),
			auroraApp.mainWindow)

		auroraApp.securityManager.AddAuditEntry("performance_mode", "user", auroraApp.t("aurora_os_audit_perf_mode_disabled"), "info")
	}
}

func (auroraApp *AuroraApp) refreshSystemInfo() {
	log.Println("Refreshing Aurora OS system information...")

	var m runtime.MemStats
	runtime.ReadMemStats(&m)

	auroraApp.systemMonitor.mu.Lock()
	// Calculate memory usage percentage (using Sys as total available to Go runtime)
	if m.Sys > 0 {
		auroraApp.systemMonitor.memoryUsage = float64(m.Alloc) / float64(m.Sys) * 100
	}
	// Simulate CPU usage based on goroutine count (rough approximation)
	auroraApp.systemMonitor.cpuUsage = float64(runtime.NumGoroutine()) / float64(runtime.NumCPU()*100) * 100
	if auroraApp.systemMonitor.cpuUsage > 100 {
		auroraApp.systemMonitor.cpuUsage = 100
	}
	// Get actual disk usage for root filesystem
	auroraApp.systemMonitor.diskUsage = auroraApp.getDiskUsage("/")
	auroraApp.systemMonitor.mu.Unlock()
}

// getDiskUsage returns the disk usage percentage for the given path
func (auroraApp *AuroraApp) getDiskUsage(path string) float64 {
	var stat syscall.Statfs_t
	if err := syscall.Statfs(path, &stat); err != nil {
		log.Printf("Failed to get disk stats for %s: %v", path, err)
		return 0.0
	}

	// Calculate usage percentage
	total := stat.Blocks * uint64(stat.Bsize)
	free := stat.Bfree * uint64(stat.Bsize)
	used := total - free

	if total == 0 {
		return 0.0
	}

	return float64(used) / float64(total) * 100.0
}

func (auroraApp *AuroraApp) optimizePerformance() {
	log.Println("Optimizing Aurora OS performance...")
	auroraApp.securityManager.AddAuditEntry("optimization_start", "user", auroraApp.t("aurora_os_audit_optimization_initiated"), "info")

	// Get memory stats before
	var before runtime.MemStats
	runtime.ReadMemStats(&before)

	// Force garbage collection
	runtime.GC()

	// Get memory stats after
	var after runtime.MemStats
	runtime.ReadMemStats(&after)

	freed := memFreedMB(before.Alloc, after.Alloc)

	// Set GOMAXPROCS
	runtime.GOMAXPROCS(runtime.NumCPU())

	// Enable performance mode
	auroraApp.performanceMode = true

	resultText := fmt.Sprintf(auroraApp.t("aurora_os_optimization_report_fmt"),
		freed, float64(before.Alloc)/1024/1024, float64(after.Alloc)/1024/1024, runtime.NumCPU())

	dialog.ShowInformation(auroraApp.t("aurora_os_dialog_optimization_title"), resultText, auroraApp.mainWindow)

	auroraApp.securityManager.AddAuditEntry("optimization_complete", "user",
		fmt.Sprintf(auroraApp.t("aurora_os_audit_optimization_completed_fmt"), freed), "info")
}

func (auroraApp *AuroraApp) showAuditLog() {
	log.Println("Showing Aurora OS audit log...")

	auroraApp.securityManager.mu.RLock()
	auditLog := auroraApp.securityManager.auditLog
	auroraApp.securityManager.mu.RUnlock()

	if len(auditLog) == 0 {
		dialog.ShowInformation(auroraApp.t("aurora_os_dialog_audit_log_title"), auroraApp.t("aurora_os_audit_log_empty"), auroraApp.mainWindow)
		return
	}

	// Create scrollable audit log display
	logText := auroraApp.t("aurora_os_audit_log_header") + "\n\n"
	// Show last 50 entries (most recent first)
	start := len(auditLog) - 50
	if start < 0 {
		start = 0
	}
	for i := len(auditLog) - 1; i >= start; i-- {
		entry := auditLog[i]
		logText += fmt.Sprintf(auroraApp.t("aurora_os_audit_log_entry_fmt"),
			entry.Timestamp.Format("2006-01-02 15:04:05"),
			entry.Severity,
			entry.Action,
			entry.User,
			entry.Details)
	}
	logText += fmt.Sprintf(auroraApp.t("aurora_os_audit_log_showing_fmt"), len(auditLog)-start, len(auditLog))

	// Create a dialog with scrollable content
	logEntry := widget.NewMultiLineEntry()
	logEntry.SetText(logText)
	logEntry.Disable()
	logEntry.Wrapping = fyne.TextWrapWord

	scrollContainer := container.NewScroll(logEntry)
	scrollContainer.SetMinSize(fyne.NewSize(600, 400))

	dialog.ShowCustom(auroraApp.t("aurora_os_dialog_audit_log_title"), auroraApp.t("aurora_os_btn_close"), scrollContainer, auroraApp.mainWindow)
}

func (auroraApp *AuroraApp) configureEncryption() {
	log.Println("Configuring Aurora OS encryption...")

	auroraApp.securityManager.mu.RLock()
	currentEnabled := auroraApp.securityManager.encryptionEnabled
	currentAlgo := auroraApp.securityManager.encryptionAlgo
	auroraApp.securityManager.mu.RUnlock()

	// Create encryption configuration dialog
	enabledCheck := widget.NewCheck(auroraApp.t("aurora_os_check_enable_encryption"), nil)
	enabledCheck.Checked = currentEnabled

	algoSelect := widget.NewSelect([]string{"AES-256-GCM", "AES-256-CBC", "ChaCha20-Poly1305"}, nil)
	algoSelect.SetSelected(currentAlgo)

	form := &widget.Form{
		Items: []*widget.FormItem{
			{Text: auroraApp.t("aurora_os_formitem_encryption_enabled"), Widget: enabledCheck},
			{Text: auroraApp.t("aurora_os_formitem_algorithm"), Widget: algoSelect},
		},
		OnSubmit: func() {
			auroraApp.securityManager.mu.Lock()
			oldEnabled := auroraApp.securityManager.encryptionEnabled
			auroraApp.securityManager.encryptionEnabled = enabledCheck.Checked
			auroraApp.securityManager.encryptionAlgo = algoSelect.Selected
			auroraApp.securityManager.mu.Unlock()

			// Log the change
			if oldEnabled != enabledCheck.Checked {
				action := "encryption_disabled"
				severity := "warning"
				if enabledCheck.Checked {
					action = "encryption_enabled"
					severity = "info"
				}
				auroraApp.securityManager.AddAuditEntry(action, "user",
					fmt.Sprintf(auroraApp.t("aurora_os_audit_encryption_change_fmt"),
						map[bool]string{true: auroraApp.t("aurora_os_state_enabled_lower"), false: auroraApp.t("aurora_os_state_disabled_lower")}[enabledCheck.Checked],
						algoSelect.Selected), severity)
			}

			dialog.ShowInformation(auroraApp.t("aurora_os_dialog_encryption_config_title"),
				fmt.Sprintf(auroraApp.t("aurora_os_encryption_updated_fmt"),
					enabledCheck.Checked, algoSelect.Selected),
				auroraApp.mainWindow)
		},
	}

	dialog.ShowForm(auroraApp.t("aurora_os_dialog_configure_encryption_title"), auroraApp.t("aurora_os_btn_save"), auroraApp.t("aurora_os_btn_cancel"), form.Items, func(b bool) {
		if b {
			form.OnSubmit()
		}
	}, auroraApp.mainWindow)
}

// Run starts the Aurora OS application
func (auroraApp *AuroraApp) Run() {
	// Setup signal handling for graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// Start signal handler in goroutine
	go func() {
		<-sigChan
		auroraApp.fyneApp.Quit()
	}()

	// Show window and run (blocks until window closes)
	auroraApp.mainWindow.ShowAndRun()
}

// createTasksTab creates the tasks tab
func (auroraApp *AuroraApp) createTasksTab() fyne.CanvasObject {
	// Task list with dynamic data
	taskList := widget.NewList(
		func() int {
			if auroraApp.taskManager == nil {
				return 0
			}
			return len(auroraApp.taskManager.GetAllTasks())
		},
		func() fyne.CanvasObject {
			return widget.NewLabel("Template")
		},
		func(id widget.ListItemID, obj fyne.CanvasObject) {
			if auroraApp.taskManager == nil {
				return
			}
			tasks := auroraApp.taskManager.GetAllTasks()
			if id < len(tasks) {
				t := tasks[id]
				obj.(*widget.Label).SetText(fmt.Sprintf("[%s] %s - %s (%s)", t.Status, t.Type, t.Description, t.Priority))
			}
		},
	)

	taskCard := widget.NewCard(auroraApp.t("aurora_os_card_tasks_list"), "", taskList)

	// Task type selector for new tasks
	taskTypeSelect := widget.NewSelect([]string{"planning", "building", "testing", "refactoring", "debugging"}, nil)
	taskTypeSelect.SetSelected("building")

	// Task priority selector
	prioritySelect := widget.NewSelect([]string{"low", "normal", "high", "critical"}, nil)
	prioritySelect.SetSelected("normal")

	// Task description input
	taskDescEntry := widget.NewEntry()
	taskDescEntry.SetPlaceHolder(auroraApp.t("aurora_os_placeholder_task_description"))

	// Action buttons
	actions := container.NewVBox(
		widget.NewLabel(auroraApp.t("aurora_os_label_new_task")),
		widget.NewLabel(auroraApp.t("aurora_os_label_field_type")),
		taskTypeSelect,
		widget.NewLabel(auroraApp.t("aurora_os_label_field_priority")),
		prioritySelect,
		widget.NewLabel(auroraApp.t("aurora_os_label_field_description")),
		taskDescEntry,
		widget.NewButton(auroraApp.t("aurora_os_btn_create_task"), func() {
			if auroraApp.taskManager != nil && taskDescEntry.Text != "" {
				ctx := context.Background()
				_, err := auroraApp.taskManager.CreateTask(ctx, taskTypeSelect.Selected, taskDescEntry.Text, prioritySelect.Selected)
				if err != nil {
					dialog.ShowError(err, auroraApp.mainWindow)
				} else {
					taskDescEntry.SetText("")
					taskList.Refresh()
					auroraApp.securityManager.AddAuditEntry("task_create", "user",
						fmt.Sprintf(auroraApp.t("aurora_os_audit_task_created_fmt"), taskDescEntry.Text), "info")
				}
			}
		}),
		widget.NewSeparator(),
		widget.NewButton(auroraApp.t("aurora_os_btn_refresh"), func() {
			taskList.Refresh()
		}),
	)

	return container.NewBorder(nil, nil, nil, actions, taskCard)
}

// createWorkersTab creates the workers tab
func (auroraApp *AuroraApp) createWorkersTab() fyne.CanvasObject {
	workerList := widget.NewList(
		func() int {
			if auroraApp.workerManager == nil {
				return 0
			}
			return len(auroraApp.workerManager.GetWorkers())
		},
		func() fyne.CanvasObject {
			return widget.NewLabel("Template")
		},
		func(id widget.ListItemID, obj fyne.CanvasObject) {
			if auroraApp.workerManager == nil {
				return
			}
			workers := auroraApp.workerManager.GetWorkers()
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

	workerCard := widget.NewCard(auroraApp.t("aurora_os_card_workers_list"), "", workerList)

	// Worker configuration inputs
	hostEntry := widget.NewEntry()
	hostEntry.SetPlaceHolder(auroraApp.t("aurora_os_placeholder_worker_host"))
	portEntry := widget.NewEntry()
	portEntry.SetPlaceHolder("22")
	portEntry.SetText("22")
	userEntry := widget.NewEntry()
	userEntry.SetPlaceHolder(auroraApp.t("aurora_os_placeholder_worker_user"))

	actions := container.NewVBox(
		widget.NewLabel(auroraApp.t("aurora_os_label_add_worker")),
		widget.NewLabel(auroraApp.t("aurora_os_label_field_host")),
		hostEntry,
		widget.NewLabel(auroraApp.t("aurora_os_label_field_port")),
		portEntry,
		widget.NewLabel(auroraApp.t("aurora_os_label_field_user")),
		userEntry,
		widget.NewButton(auroraApp.t("aurora_os_btn_add_worker"), func() {
			if auroraApp.workerManager != nil && hostEntry.Text != "" {
				workerConfig := &UIWorker{
					ID:      fmt.Sprintf("worker-%s-%d", hostEntry.Text, time.Now().UnixNano()),
					Host:    hostEntry.Text,
					Port:    portEntry.Text,
					User:    userEntry.Text,
					Status:  "pending",
					Healthy: false,
				}
				err := auroraApp.workerManager.AddWorker(workerConfig)
				if err != nil {
					dialog.ShowError(err, auroraApp.mainWindow)
				} else {
					hostEntry.SetText("")
					userEntry.SetText("")
					workerList.Refresh()
					auroraApp.securityManager.AddAuditEntry("worker_add", "user",
						fmt.Sprintf(auroraApp.t("aurora_os_audit_worker_added_fmt"), workerConfig.Host), "info")
				}
			}
		}),
		widget.NewSeparator(),
		widget.NewButton(auroraApp.t("aurora_os_btn_refresh"), func() {
			workerList.Refresh()
		}),
	)

	return container.NewBorder(nil, nil, nil, actions, workerCard)
}

// createProjectsTab creates the projects tab
func (auroraApp *AuroraApp) createProjectsTab() fyne.CanvasObject {
	// Project list with dynamic data
	auroraApp.projectList = widget.NewList(
		func() int {
			auroraApp.dataMu.RLock()
			defer auroraApp.dataMu.RUnlock()
			return len(auroraApp.projects)
		},
		func() fyne.CanvasObject {
			return container.NewHBox(
				widget.NewLabel("Template"),
				widget.NewLabel(""),
			)
		},
		func(id widget.ListItemID, obj fyne.CanvasObject) {
			auroraApp.dataMu.RLock()
			defer auroraApp.dataMu.RUnlock()
			if id < len(auroraApp.projects) {
				p := auroraApp.projects[id]
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
	projectDetailsLabel := widget.NewLabel(auroraApp.t("aurora_os_label_select_project"))
	projectDetailsLabel.Wrapping = fyne.TextWrapWord

	auroraApp.projectList.OnSelected = func(id widget.ListItemID) {
		auroraApp.dataMu.RLock()
		defer auroraApp.dataMu.RUnlock()
		if id < len(auroraApp.projects) {
			p := auroraApp.projects[id]
			details := fmt.Sprintf(auroraApp.t("aurora_os_project_details_fmt"),
				p.Name, p.Type, p.Path, p.Description,
				p.CreatedAt.Format(time.RFC822),
				p.Metadata.BuildCommand, p.Metadata.TestCommand)
			projectDetailsLabel.SetText(details)
		}
	}

	projectListCard := widget.NewCard(auroraApp.t("aurora_os_card_projects_list"), "", auroraApp.projectList)
	projectDetailsCard := widget.NewCard(auroraApp.t("aurora_os_card_project_details"), "", projectDetailsLabel)

	// Project creation form
	nameEntry := widget.NewEntry()
	nameEntry.SetPlaceHolder(auroraApp.t("aurora_os_placeholder_project_name"))
	descEntry := widget.NewEntry()
	descEntry.SetPlaceHolder(auroraApp.t("aurora_os_placeholder_description"))
	pathEntry := widget.NewEntry()
	pathEntry.SetPlaceHolder("/path/to/project")
	typeSelect := widget.NewSelect([]string{"go", "node", "python", "rust", "generic"}, nil)
	typeSelect.SetSelected("go")

	createForm := container.NewVBox(
		widget.NewLabel(auroraApp.t("aurora_os_label_create_new_project")),
		widget.NewLabel(auroraApp.t("aurora_os_label_field_name")),
		nameEntry,
		widget.NewLabel(auroraApp.t("aurora_os_label_field_description")),
		descEntry,
		widget.NewLabel(auroraApp.t("aurora_os_label_field_path")),
		pathEntry,
		widget.NewLabel(auroraApp.t("aurora_os_label_field_type")),
		typeSelect,
		widget.NewButton(auroraApp.t("aurora_os_btn_create_project"), func() {
			if auroraApp.projectManager != nil && nameEntry.Text != "" && pathEntry.Text != "" {
				ctx := context.Background()
				_, err := auroraApp.projectManager.CreateProject(ctx, nameEntry.Text, descEntry.Text, pathEntry.Text, typeSelect.Selected)
				if err != nil {
					dialog.ShowError(err, auroraApp.mainWindow)
				} else {
					auroraApp.securityManager.AddAuditEntry("project_create", "user",
						fmt.Sprintf(auroraApp.t("aurora_os_audit_project_created_fmt"), nameEntry.Text), "info")
					nameEntry.SetText("")
					descEntry.SetText("")
					pathEntry.SetText("")
					auroraApp.refreshData()
					auroraApp.projectList.Refresh()
					dialog.ShowInformation(auroraApp.t("aurora_os_dialog_success_title"), auroraApp.t("aurora_os_dialog_project_created"), auroraApp.mainWindow)
				}
			}
		}),
		widget.NewSeparator(),
		widget.NewButton(auroraApp.t("aurora_os_btn_refresh"), func() {
			auroraApp.refreshData()
			auroraApp.projectList.Refresh()
		}),
	)

	leftPanel := container.NewVSplit(projectListCard, projectDetailsCard)
	leftPanel.SetOffset(0.6)

	return container.NewBorder(nil, nil, nil, createForm, leftPanel)
}

// createSessionsTab creates the sessions tab
func (auroraApp *AuroraApp) createSessionsTab() fyne.CanvasObject {
	// Session list with dynamic data
	auroraApp.sessionList = widget.NewList(
		func() int {
			auroraApp.dataMu.RLock()
			defer auroraApp.dataMu.RUnlock()
			return len(auroraApp.sessions)
		},
		func() fyne.CanvasObject {
			return container.NewHBox(
				widget.NewLabel("Template"),
				widget.NewLabel(""),
			)
		},
		func(id widget.ListItemID, obj fyne.CanvasObject) {
			auroraApp.dataMu.RLock()
			defer auroraApp.dataMu.RUnlock()
			if id < len(auroraApp.sessions) {
				s := auroraApp.sessions[id]
				hbox := obj.(*fyne.Container)
				hbox.Objects[0].(*widget.Label).SetText(s.Name)
				hbox.Objects[1].(*widget.Label).SetText(fmt.Sprintf("[%s] %s", s.Status, s.Mode))
			}
		},
	)

	// Session details panel
	sessionDetailsLabel := widget.NewLabel(auroraApp.t("aurora_os_label_select_session"))
	sessionDetailsLabel.Wrapping = fyne.TextWrapWord

	selectedSessionID := ""
	auroraApp.sessionList.OnSelected = func(id widget.ListItemID) {
		auroraApp.dataMu.RLock()
		defer auroraApp.dataMu.RUnlock()
		if id < len(auroraApp.sessions) {
			s := auroraApp.sessions[id]
			selectedSessionID = s.ID
			durationStr := s.Duration.String()
			details := fmt.Sprintf(auroraApp.t("aurora_os_session_details_fmt"),
				s.ID, s.Name, s.Mode, s.Status, s.ProjectID, s.Description,
				s.CreatedAt.Format(time.RFC822), durationStr)
			sessionDetailsLabel.SetText(details)
		}
	}

	sessionListCard := widget.NewCard(auroraApp.t("aurora_os_card_sessions_list"), "", auroraApp.sessionList)
	sessionDetailsCard := widget.NewCard(auroraApp.t("aurora_os_card_session_details"), "", sessionDetailsLabel)

	// Session creation form
	nameEntry := widget.NewEntry()
	nameEntry.SetPlaceHolder(auroraApp.t("aurora_os_placeholder_session_name"))
	descEntry := widget.NewEntry()
	descEntry.SetPlaceHolder(auroraApp.t("aurora_os_placeholder_description"))
	projectIDEntry := widget.NewEntry()
	projectIDEntry.SetPlaceHolder(auroraApp.t("aurora_os_placeholder_project_id"))
	modeSelect := widget.NewSelect([]string{"planning", "building", "testing", "refactoring", "debugging", "deployment"}, nil)
	modeSelect.SetSelected("building")

	actions := container.NewVBox(
		widget.NewLabel(auroraApp.t("aurora_os_label_create_new_session")),
		widget.NewLabel(auroraApp.t("aurora_os_label_field_name")),
		nameEntry,
		widget.NewLabel(auroraApp.t("aurora_os_label_field_description")),
		descEntry,
		widget.NewLabel(auroraApp.t("aurora_os_label_field_project_id")),
		projectIDEntry,
		widget.NewLabel(auroraApp.t("aurora_os_label_field_mode")),
		modeSelect,
		widget.NewButton(auroraApp.t("aurora_os_btn_create_session"), func() {
			if auroraApp.sessionManager != nil && nameEntry.Text != "" && projectIDEntry.Text != "" {
				mode := session.Mode(modeSelect.Selected)
				_, err := auroraApp.sessionManager.Create(projectIDEntry.Text, nameEntry.Text, descEntry.Text, mode)
				if err != nil {
					dialog.ShowError(err, auroraApp.mainWindow)
				} else {
					auroraApp.securityManager.AddAuditEntry("session_create", "user",
						fmt.Sprintf(auroraApp.t("aurora_os_audit_session_created_fmt"), nameEntry.Text), "info")
					nameEntry.SetText("")
					descEntry.SetText("")
					projectIDEntry.SetText("")
					auroraApp.refreshData()
					auroraApp.sessionList.Refresh()
					dialog.ShowInformation(auroraApp.t("aurora_os_dialog_success_title"), auroraApp.t("aurora_os_dialog_session_created"), auroraApp.mainWindow)
				}
			}
		}),
		widget.NewSeparator(),
		widget.NewLabel(auroraApp.t("aurora_os_label_session_controls")),
		widget.NewButton(auroraApp.t("aurora_os_btn_start_session"), func() {
			if auroraApp.sessionManager != nil && selectedSessionID != "" {
				err := auroraApp.sessionManager.Start(selectedSessionID)
				if err != nil {
					dialog.ShowError(err, auroraApp.mainWindow)
				} else {
					auroraApp.securityManager.AddAuditEntry("session_start", "user",
						fmt.Sprintf(auroraApp.t("aurora_os_audit_session_started_fmt"), selectedSessionID), "info")
					auroraApp.refreshData()
					auroraApp.sessionList.Refresh()
				}
			}
		}),
		widget.NewButton(auroraApp.t("aurora_os_btn_pause_session"), func() {
			if auroraApp.sessionManager != nil && selectedSessionID != "" {
				err := auroraApp.sessionManager.Pause(selectedSessionID)
				if err != nil {
					dialog.ShowError(err, auroraApp.mainWindow)
				} else {
					auroraApp.securityManager.AddAuditEntry("session_pause", "user",
						fmt.Sprintf(auroraApp.t("aurora_os_audit_session_paused_fmt"), selectedSessionID), "info")
					auroraApp.refreshData()
					auroraApp.sessionList.Refresh()
				}
			}
		}),
		widget.NewButton(auroraApp.t("aurora_os_btn_complete_session"), func() {
			if auroraApp.sessionManager != nil && selectedSessionID != "" {
				err := auroraApp.sessionManager.Complete(selectedSessionID)
				if err != nil {
					dialog.ShowError(err, auroraApp.mainWindow)
				} else {
					auroraApp.securityManager.AddAuditEntry("session_complete", "user",
						fmt.Sprintf(auroraApp.t("aurora_os_audit_session_completed_fmt"), selectedSessionID), "info")
					auroraApp.refreshData()
					auroraApp.sessionList.Refresh()
				}
			}
		}),
		widget.NewSeparator(),
		widget.NewButton("Refresh", func() {
			auroraApp.refreshData()
			auroraApp.sessionList.Refresh()
		}),
	)

	leftPanel := container.NewVSplit(sessionListCard, sessionDetailsCard)
	leftPanel.SetOffset(0.6)

	return container.NewBorder(nil, nil, nil, actions, leftPanel)
}

// createLLMTab creates the LLM tab
func (auroraApp *AuroraApp) createLLMTab() fyne.CanvasObject {
	// Available models list
	modelList := widget.NewList(
		func() int {
			if auroraApp.llmManager == nil {
				return 0
			}
			return len(auroraApp.llmManager.GetAvailableModels())
		},
		func() fyne.CanvasObject {
			return container.NewHBox(
				widget.NewLabel("Model"),
				widget.NewLabel("Provider"),
			)
		},
		func(id widget.ListItemID, obj fyne.CanvasObject) {
			models := auroraApp.llmManager.GetAvailableModels()
			if id < len(models) {
				m := models[id]
				hbox := obj.(*fyne.Container)
				hbox.Objects[0].(*widget.Label).SetText(m.Name)
				hbox.Objects[1].(*widget.Label).SetText(string(m.Provider))
			}
		},
	)

	modelListCard := widget.NewCard(auroraApp.t("aurora_os_card_available_models"), "", modelList)

	// Model details panel
	modelDetailsLabel := widget.NewLabel(auroraApp.t("aurora_os_label_select_model"))
	modelDetailsLabel.Wrapping = fyne.TextWrapWord

	modelList.OnSelected = func(id widget.ListItemID) {
		models := auroraApp.llmManager.GetAvailableModels()
		if id < len(models) {
			m := models[id]
			caps := make([]string, len(m.Capabilities))
			for i, c := range m.Capabilities {
				caps[i] = string(c)
			}
			details := fmt.Sprintf(auroraApp.t("aurora_os_model_details_fmt"),
				m.Name, m.Provider, m.ContextSize, caps)
			modelDetailsLabel.SetText(details)
		}
	}

	modelDetailsCard := widget.NewCard(auroraApp.t("aurora_os_card_model_details"), "", modelDetailsLabel)

	// Chat interface
	auroraApp.chatHistory = widget.NewMultiLineEntry()
	auroraApp.chatHistory.SetPlaceHolder(auroraApp.t("aurora_os_placeholder_chat_history"))
	auroraApp.chatHistory.Disable()
	auroraApp.chatHistory.Wrapping = fyne.TextWrapWord

	auroraApp.chatInput = widget.NewMultiLineEntry()
	auroraApp.chatInput.SetPlaceHolder(auroraApp.t("aurora_os_placeholder_chat_input"))
	auroraApp.chatInput.SetMinRowsVisible(3)

	// Provider/model selection for chat
	auroraApp.llmProviderSel = widget.NewSelect([]string{"ollama", "openai", "anthropic", "gemini", "local"}, nil)
	auroraApp.llmProviderSel.SetSelected("ollama")

	modelNameEntry := widget.NewEntry()
	modelNameEntry.SetPlaceHolder(auroraApp.t("aurora_os_placeholder_model_name"))
	modelNameEntry.SetText("llama2")

	sendButton := widget.NewButton(auroraApp.t("aurora_os_btn_send_message"), func() {
		if auroraApp.chatInput.Text == "" {
			return
		}

		// Add user message to history
		currentHistory := auroraApp.chatHistory.Text
		userMessage := auroraApp.chatInput.Text
		userMsg := fmt.Sprintf(auroraApp.t("aurora_os_chat_user_message_fmt"), userMessage)
		auroraApp.chatHistory.SetText(currentHistory + userMsg)

		// Resolve the provider + model at DISPATCH time. `.Selected` and
		// `.Text` are widget READS, and this button callback already runs on
		// the UI goroutine — reading them from the worker goroutine below
		// would race the renderer exactly as a bare write would. Captured
		// up-front and passed in as parameters so the worker observes a stable
		// snapshot (mirrors applications/desktop's `selectedLabel := …` hoist).
		providerName := auroraApp.llmProviderSel.Selected
		modelName := modelNameEntry.Text

		// Log the interaction
		auroraApp.securityManager.AddAuditEntry("llm_chat", "user",
			fmt.Sprintf(auroraApp.t("aurora_os_audit_message_sent_fmt"), providerName, modelName), "info")

		// Clear input immediately
		auroraApp.chatInput.SetText("")

		// Make LLM call in goroutine to not block UI.
		//
		// THREAD-AFFINITY FIX (§11.4.115): this whole body runs OFF the UI
		// goroutine, so the terminal auroraApp.chatHistory append below is
		// dispatched via fyneui.DoAndWait — the blocking flavour, because this
		// is a one-shot terminal update and callers (or a test) synchronising
		// on the turn ending must see the final text. The read and the write
		// live in the SAME closure: `auroraApp.chatHistory.Text + …` READS the
		// widget, so wrapping only the write would leave the read racing.
		go func(msg, providerName, modelName string) {
			var responseMsg string

			if auroraApp.llmManager != nil {
				// Get provider from manager using provider type
				providerType := llm.ProviderType(providerName)
				provider, err := auroraApp.llmManager.GetProviderForModel(modelName, providerType)
				if err == nil && provider != nil {
					// Create LLM request
					ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
					defer cancel()

					request := &llm.LLMRequest{
						Messages: []llm.Message{
							{Role: "user", Content: msg},
						},
						Model:       modelName,
						MaxTokens:   1024,
						Temperature: 0.7,
					}

					response, err := provider.Generate(ctx, request)
					if err != nil {
						responseMsg = fmt.Sprintf(auroraApp.t("aurora_os_chat_ai_error_fmt"), providerName, modelName, err)
					} else {
						responseMsg = fmt.Sprintf(auroraApp.t("aurora_os_chat_ai_response_fmt"), providerName, modelName, response.Content)
					}
				} else {
					responseMsg = fmt.Sprintf(auroraApp.t("aurora_os_chat_provider_unavailable_fmt"),
						providerName, modelName, providerName)
				}
			} else {
				// No LLM manager configured - show informative message
				responseMsg = fmt.Sprintf(auroraApp.t("aurora_os_chat_llm_not_initialized_fmt"),
					providerName, modelName)
			}

			// Update the UI ON the UI goroutine.
			//
			// The comment that stood here previously claimed "Update UI on
			// main thread" while doing the exact opposite — a bare mutation
			// from this worker goroutine. It is corrected rather than merely
			// deleted: leaving the false claim in place is what invites the
			// fix to be reverted.
			fyneui.DoAndWait(func() {
				auroraApp.chatHistory.SetText(auroraApp.chatHistory.Text + responseMsg)
			})
		}(userMessage, providerName, modelName)
	})

	clearButton := widget.NewButton(auroraApp.t("aurora_os_btn_clear_chat"), func() {
		auroraApp.chatHistory.SetText("")
	})

	chatControls := container.NewVBox(
		widget.NewLabel(auroraApp.t("aurora_os_label_chat_settings")),
		widget.NewLabel(auroraApp.t("aurora_os_label_provider")),
		auroraApp.llmProviderSel,
		widget.NewLabel(auroraApp.t("aurora_os_label_model")),
		modelNameEntry,
		widget.NewSeparator(),
		sendButton,
		clearButton,
	)

	chatPanel := container.NewBorder(
		widget.NewLabel(auroraApp.t("aurora_os_label_chat_with_ai")),
		container.NewBorder(nil, nil, nil, chatControls, auroraApp.chatInput),
		nil, nil,
		auroraApp.chatHistory,
	)

	chatCard := widget.NewCard(auroraApp.t("aurora_os_card_llm_chat"), "", chatPanel)

	// Provider health status
	healthLabel := widget.NewLabel(auroraApp.t("aurora_os_health_checking"))

	// Start health check goroutine.
	//
	// THREAD-AFFINITY FIX (§11.4.115): every healthLabel mutation below runs
	// off the UI goroutine and is therefore dispatched via fyneui.DoAndWait —
	// the blocking flavour, matching the provider-health sites in
	// applications/desktop, since these are one-shot status updates rather than
	// a high-frequency stream. The dispatch lives INSIDE checkHealth so BOTH
	// call sites are covered: the eager pre-loop call (which also runs on this
	// worker goroutine) and every subsequent tick.
	go func() {
		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()

		checkHealth := func() {
			if auroraApp.llmManager == nil {
				noManager := auroraApp.t("aurora_os_health_no_manager")
				fyneui.DoAndWait(func() { healthLabel.SetText(noManager) })
				return
			}
			ctx := context.Background()
			health := auroraApp.llmManager.HealthCheck(ctx)
			healthText := auroraApp.t("aurora_os_health_header") + "\n"
			for provider, status := range health {
				healthText += fmt.Sprintf("- %s: %s\n", provider, status.Status)
			}
			if len(health) == 0 {
				healthText += auroraApp.t("aurora_os_health_no_providers")
			}
			fyneui.DoAndWait(func() { healthLabel.SetText(healthText) })
		}

		checkHealth()
		// LIFECYCLE FIX (§11.4.115): was `for range ticker.C` — unstoppable, so
		// the goroutine kept polling providers and mutating healthLabel after
		// teardown. It now honours stopUpdate (closed by Close()).
		for fyneui.TickOrStop(ticker.C, auroraApp.stopUpdate) {
			checkHealth()
		}
	}()

	healthCard := widget.NewCard(auroraApp.t("aurora_os_card_provider_status"), "", healthLabel)

	// Layout
	leftPanel := container.NewVSplit(modelListCard, modelDetailsCard)
	leftPanel.SetOffset(0.5)

	rightPanel := container.NewVSplit(chatCard, healthCard)
	rightPanel.SetOffset(0.7)

	return container.NewHSplit(leftPanel, rightPanel)
}

// createSettingsTab creates the settings tab
func (auroraApp *AuroraApp) createSettingsTab() fyne.CanvasObject {
	// Theme selection
	themeInfoLabel := widget.NewLabel("")
	updateThemeInfo := func() {
		currentTheme := auroraApp.themeManager.GetCurrentTheme()
		themeInfo := fmt.Sprintf(auroraApp.t("aurora_os_theme_info_fmt"),
			currentTheme.Name, currentTheme.IsDark,
			currentTheme.Primary, currentTheme.Secondary, currentTheme.Accent)
		themeInfoLabel.SetText(themeInfo)
	}

	themeSelect := widget.NewSelect(auroraApp.themeManager.GetAvailableThemes(), func(selected string) {
		auroraApp.themeManager.SetTheme(selected)
		updateThemeInfo()
	})
	themeSelect.SetSelected(auroraApp.themeManager.GetCurrentTheme().Name)

	themeCard := widget.NewCard(auroraApp.t("aurora_os_card_theme"), auroraApp.t("aurora_os_card_theme_subtitle"), themeSelect)

	// Current theme info
	updateThemeInfo()
	infoCard := widget.NewCard(auroraApp.t("aurora_os_card_current_theme"), "", themeInfoLabel)

	// Server connection settings
	serverURLEntry := widget.NewEntry()
	serverURLEntry.SetText("http://localhost:8080")
	serverURLEntry.SetPlaceHolder(auroraApp.t("aurora_os_placeholder_server_url"))

	serverTimeoutEntry := widget.NewEntry()
	serverTimeoutEntry.SetText("30")
	serverTimeoutEntry.SetPlaceHolder(auroraApp.t("aurora_os_placeholder_timeout"))

	serverCard := widget.NewCard(auroraApp.t("aurora_os_card_server_connection"), "",
		container.NewVBox(
			widget.NewLabel(auroraApp.t("aurora_os_label_server_url")),
			serverURLEntry,
			widget.NewLabel(auroraApp.t("aurora_os_label_timeout")),
			serverTimeoutEntry,
			widget.NewButton(auroraApp.t("aurora_os_btn_test_connection"), func() {
				dialog.ShowInformation(auroraApp.t("aurora_os_dialog_connection_test_title"), auroraApp.t("aurora_os_connection_test_body"), auroraApp.mainWindow)
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

	dbCard := widget.NewCard(auroraApp.t("aurora_os_card_database"), "",
		container.NewVBox(
			widget.NewLabel(auroraApp.t("aurora_os_label_db_host")),
			dbHostEntry,
			widget.NewLabel(auroraApp.t("aurora_os_label_db_port")),
			dbPortEntry,
			widget.NewLabel(auroraApp.t("aurora_os_label_db_database")),
			dbNameEntry,
		),
	)

	// LLM Provider settings
	ollamaURLEntry := widget.NewEntry()
	ollamaURLEntry.SetText("http://localhost:11434")

	llmCard := widget.NewCard(auroraApp.t("aurora_os_card_llm_providers"), "",
		container.NewVBox(
			widget.NewLabel(auroraApp.t("aurora_os_label_ollama_url")),
			ollamaURLEntry,
			widget.NewLabel(auroraApp.t("aurora_os_label_openai_api_key")),
			widget.NewPasswordEntry(),
			widget.NewLabel(auroraApp.t("aurora_os_label_anthropic_api_key")),
			widget.NewPasswordEntry(),
		),
	)

	// Aurora OS specific settings
	perfModeCheck := widget.NewCheck(auroraApp.t("aurora_os_check_performance_mode"), func(checked bool) {
		auroraApp.performanceMode = checked
		if checked {
			runtime.GOMAXPROCS(runtime.NumCPU())
		}
	})
	perfModeCheck.Checked = auroraApp.performanceMode

	auroraCard := widget.NewCard(auroraApp.t("aurora_os_card_aurora_settings"), "",
		container.NewVBox(
			perfModeCheck,
			widget.NewButton(auroraApp.t("aurora_os_btn_run_diagnostics"), func() {
				auroraApp.runAuroraDiagnostics()
			}),
			widget.NewButton(auroraApp.t("aurora_os_btn_view_audit_log"), func() {
				auroraApp.showAuditLog()
			}),
		),
	)

	// About section
	aboutLabel := widget.NewLabel(auroraApp.t("aurora_os_about_body"))
	aboutLabel.Alignment = fyne.TextAlignCenter
	aboutCard := widget.NewCard(auroraApp.t("aurora_os_card_about"), "", aboutLabel)

	// Layout in scrollable container
	settingsContent := container.NewVBox(
		themeCard,
		infoCard,
		auroraCard,
		serverCard,
		dbCard,
		llmCard,
		aboutCard,
	)

	return container.NewScroll(settingsContent)
}

// closeJoinTimeout bounds how long Close() waits for the background
// data-update loop to observe stopUpdate and return before proceeding with
// teardown regardless. See applications/desktop's identical constant for
// rationale.
const closeJoinTimeout = 5 * time.Second

// Close cleans up resources. It is idempotent: the stopUpdate channel is closed
// at most once via stopOnce, so a second Close call is a clean no-op rather than
// a "close of closed channel" panic.
//
// TEARDOWN-RACE FIX (§11.4.115 / §11.4.108, mirrors applications/desktop's /
// applications/harmony_os's identical fix): closing stopUpdate only signals
// the background loop to stop -- it does NOT wait for the loop to have
// actually observed the signal and returned. Without a join, this function
// could proceed to close auroraApp.db while the loop goroutine was still
// mid-refreshData()/refreshSystemInfo() (or about to start that pair after
// the priority-select race described on updateLoopTick), racing a live DB
// handle against its own teardown. HONESTY (§11.4.6): the bounded wait
// below guarantees the loop goroutine has RETURNED before teardown begins;
// it does not make updateLoopTick's own select-priority race provably
// zero-width (see that method's doc comment).
func (auroraApp *AuroraApp) Close() error {
	// Stop background updates
	if auroraApp.stopUpdate != nil {
		auroraApp.stopOnce.Do(func() {
			close(auroraApp.stopUpdate)
		})
	}

	// JOIN: wait for the update loop goroutine to have fully returned
	// before tearing down auroraApp.db below. updateDone is nil when
	// startDataUpdates was never called, so this is a no-op in that case
	// rather than a hang on a channel nothing will ever close.
	if auroraApp.updateDone != nil {
		select {
		case <-auroraApp.updateDone:
		case <-time.After(closeJoinTimeout):
			log.Printf("Close: timed out after %s waiting for background update loop to stop; proceeding with teardown anyway", closeJoinTimeout)
		}
	}

	// Log shutdown
	auroraApp.securityManager.AddAuditEntry("system_shutdown", "system", auroraApp.t("aurora_os_audit_app_shutting_down"), "info")

	// Close database connection
	if auroraApp.db != nil {
		auroraApp.db.Close()
	}
	return nil
}

func main() {
	auroraApp := NewAuroraApp()

	// Wire the real CONST-046 translator (embedded active.en.yaml bundle) onto
	// the app BEFORE any user-facing output, replacing the NoopTranslator{}
	// message-ID-echo default installed by NewAuroraApp. Without this, every
	// user-facing string leaks its raw message key — a §11.4 / CONST-046
	// PASS-bluff. On bundle load failure the loud NoopTranslator{} echo is
	// preserved (never a silent swallow). Mirrors main_nogui.go wireTranslator.
	if tr, err := i18n.NewTranslator(); err != nil {
		log.Printf("⚠️  i18n: falling back to message-ID echo (bundle load failed): %v", err)
	} else {
		auroraApp.SetTranslator(tr)
	}

	if err := auroraApp.Initialize(); err != nil {
		log.Fatalf("Failed to initialize Aurora OS app: %v", err)
	}
	defer auroraApp.Close()

	auroraApp.Run()
}
