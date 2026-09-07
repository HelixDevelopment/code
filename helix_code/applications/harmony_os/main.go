//go:build !nogui

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
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
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/widget"

	"dev.helix.code/applications/harmony_os/i18n"
	"dev.helix.code/internal/config"
	"dev.helix.code/internal/database"
	"dev.helix.code/internal/fyneui"
	"dev.helix.code/internal/hardware"
	"dev.helix.code/internal/llm"
	"dev.helix.code/internal/monitoring"
	"dev.helix.code/internal/notification"
	"dev.helix.code/internal/project"
	"dev.helix.code/internal/redis"
	"dev.helix.code/internal/server"
	"dev.helix.code/internal/session"
	"dev.helix.code/internal/task"
	"dev.helix.code/internal/worker"
)

// APIClient handles communication with the HelixCode backend API
type APIClient struct {
	baseURL    string
	httpClient *http.Client
	token      string
	mu         sync.RWMutex
}

// NewAPIClient creates a new API client
func NewAPIClient(baseURL string) *APIClient {
	return &APIClient{
		baseURL: baseURL,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// SetToken sets the authentication token
func (c *APIClient) SetToken(token string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.token = token
}

// doRequest performs an HTTP request with authentication
func (c *APIClient) doRequest(method, path string, body io.Reader) (*http.Response, error) {
	c.mu.RLock()
	token := c.token
	c.mu.RUnlock()

	req, err := http.NewRequest(method, c.baseURL+path, body)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	return c.httpClient.Do(req)
}

// APITask represents a task from the API
type APITask struct {
	ID          string    `json:"id"`
	Type        string    `json:"type"`
	Description string    `json:"description"`
	Status      string    `json:"status"`
	Priority    string    `json:"priority"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// APIWorker represents a worker from the API
type APIWorker struct {
	ID           string    `json:"id"`
	Host         string    `json:"host"`
	Port         int       `json:"port"`
	User         string    `json:"user"`
	Status       string    `json:"status"`
	Healthy      bool      `json:"healthy"`
	Capabilities []string  `json:"capabilities"`
	LastSeen     time.Time `json:"last_seen"`
}

// APIProject represents a project from the API
type APIProject struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	Path        string    `json:"path"`
	Type        string    `json:"type"`
	Active      bool      `json:"active"`
	CreatedAt   time.Time `json:"created_at"`
}

// APISession represents a session from the API
type APISession struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	ProjectID   string    `json:"project_id"`
	Mode        string    `json:"mode"`
	Status      string    `json:"status"`
	CreatedAt   time.Time `json:"created_at"`
}

// HarmonyApp represents the main Harmony OS application
type HarmonyApp struct {
	fyneApp            fyne.App
	mainWindow         fyne.Window
	config             *config.Config
	db                 *database.Database
	taskManager        *task.TaskManager
	workerManager      *worker.WorkerManager
	projectManager     *project.Manager
	sessionManager     *session.Manager
	llmManager         *llm.ModelManager
	notificationEngine *notification.NotificationEngine
	server             *server.Server
	themeManager       *ThemeManager
	apiClient          *APIClient
	monitor            *monitoring.Monitor
	hardwareDetector   *hardware.HardwareDetector

	// Harmony OS specific components
	harmonyIntegration *HarmonyIntegration
	systemMonitor      *HarmonySystemMonitor
	resourceManager    *HarmonyResourceManager
	serviceCoordinator *HarmonyServiceCoordinator

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
	tasks        []APITask
	workers      []APIWorker
	projects     []APIProject
	sessions     []APISession
	llmProviders []string

	// Update control
	updateTicker *time.Ticker
	stopUpdate   chan struct{}
	stopOnce     sync.Once

	// updateDone is closed by the startDataUpdates goroutine immediately
	// before it returns. Cleanup() waits on it (bounded) BEFORE tearing down
	// app.db, so teardown never races a still-running refreshData() call.
	// nil until startDataUpdates runs. See updateLoopTick's doc comment for
	// the teardown-race this closes.
	updateDone chan struct{}

	// monitorDone is closed by the monitorSystem goroutine immediately
	// before it returns, and Cleanup() waits on it (bounded) the same way
	// it waits on updateDone. Without the join, Cleanup() returning proved
	// nothing about whether the monitor goroutine had actually stopped
	// touching app.systemMonitor. nil until initializeHarmonyComponents
	// spawns the monitor, so Cleanup() on a partially-constructed app
	// (the minimal-construction pattern used by main_doubleclose_test.go
	// and main_racefix_test.go) is a no-op rather than a hang.
	monitorDone chan struct{}

	// updateLoopTickRaceHook is a test seam (nil in production, never set
	// outside _test.go files): when non-nil, updateLoopTick invokes it
	// immediately after the non-blocking priority pre-check observes
	// stopUpdate still open, but BEFORE the blocking select that follows is
	// evaluated -- the residual race window described on updateLoopTick's
	// doc comment. Production code never sets this hook.
	updateLoopTickRaceHook func()

	// translator resolves CONST-046 user-facing message IDs for the
	// GUI (round-438 §11.4 anti-bluff sweep, 2026-05-20). Defaults to
	// i18n.NoopTranslator{} (loud message-ID echo) when nil — set by
	// helix_code at boot via SetTranslator to a real
	// *i18nadapter.Translator wired to the active.en.yaml bundle.
	translator i18n.Translator
}

// SetTranslator wires a CONST-046-compliant Translator into the GUI
// app. Passing nil resets to i18n.NoopTranslator{} (loud echo) —
// never silently swallow translation requests (a §11.4 PASS-bluff at
// the i18n injection layer).
func (app *HarmonyApp) SetTranslator(tr i18n.Translator) {
	if tr == nil {
		app.translator = i18n.NoopTranslator{}
		return
	}
	app.translator = tr
}

// tr is the internal CONST-046 resolver used by every user-facing
// string emission in the GUI. It NEVER returns an error to the
// caller — translation failures degrade to the message ID itself
// (matching NoopTranslator behaviour) so production output remains
// loud + obvious instead of silently empty.
func (app *HarmonyApp) tr(msgID string, data map[string]any) string {
	if app.translator == nil {
		app.translator = i18n.NoopTranslator{}
	}
	out, err := app.translator.T(context.Background(), msgID, data)
	if err != nil || out == "" {
		return msgID
	}
	return out
}

// HarmonyIntegration handles Harmony OS-specific native features
type HarmonyIntegration struct {
	nativeServices    map[string]interface{}
	systemAPI         *HarmonySystemAPI
	distributedEngine *HarmonyDistributedEngine
	harmonyContext    context.Context
}

// HarmonySystemAPI provides access to Harmony OS system services
type HarmonySystemAPI struct {
	deviceInfo    map[string]string
	capabilities  []string
	systemVersion string
	kernelVersion string
}

// HarmonyDistributedEngine, HarmonyDevice, HarmonyResources,
// HarmonyTaskScheduler, ScheduledTask, HarmonyDataSync — and their
// constructors / methods — were relocated to distributed.go (no build
// tag) during the round-67 §11.4 anti-bluff sweep so the round-31
// sentinel tests can be executed on hosts without the Fyne / X11
// toolchain. See distributed.go for the canonical definitions plus
// the round-67 HarmonyDistributedSDK injection point.

// HarmonySystemMonitor monitors Harmony OS system resources and performance.
//
// CONCURRENCY (§11.4.115): every field below except updateInterval is written
// by the monitorSystem goroutine (updateSystemMetrics) and read by the UI
// goroutine (createHarmonySystemTab), so all of them are guarded by mu. This
// mirrors the sibling AuroraSystemMonitor, which already carried exactly such
// a mutex; harmony_os was the odd one out and its fields raced.
//
// updateInterval is deliberately NOT guarded: it is set once during
// initializeHarmonyComponents, before the monitor goroutine is spawned, and is
// never written again — so it is published by the `go` statement's own
// happens-before edge and only ever read afterwards.
type HarmonySystemMonitor struct {
	mu             sync.RWMutex
	cpuUsage       float64
	memoryUsage    float64
	gpuUsage       float64
	networkTraffic int64
	diskIO         int64
	temperature    float64
	powerUsage     float64
	updateInterval time.Duration
	monitoring     bool
}

// setMonitoring records whether the system monitor is running.
func (m *HarmonySystemMonitor) setMonitoring(v bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.monitoring = v
}

// isMonitoring reports whether the system monitor is running.
func (m *HarmonySystemMonitor) isMonitoring() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.monitoring
}

// HarmonyResourceManager manages system resources and optimization
type HarmonyResourceManager struct {
	resourcePolicies map[string]string
	optimization     bool
	autoTuning       bool
}

// HarmonyServiceCoordinator coordinates distributed services
type HarmonyServiceCoordinator struct {
	services        map[string]bool
	serviceRegistry map[string]interface{}
	coordinator     *ServiceCoordinator
}

// ServiceCoordinator manages service lifecycle
type ServiceCoordinator struct {
	activeServices  []string
	failoverEnabled bool
}

// NewHarmonyApp creates a new Harmony OS application
func NewHarmonyApp() *HarmonyApp {
	return &HarmonyApp{
		fyneApp:      app.NewWithID("dev.helix.code.harmonyos"),
		themeManager: NewThemeManager(),
		tasks:        make([]APITask, 0),
		workers:      make([]APIWorker, 0),
		projects:     make([]APIProject, 0),
		sessions:     make([]APISession, 0),
		llmProviders: make([]string, 0),
		stopUpdate:   make(chan struct{}),
		translator:   i18n.NoopTranslator{},
	}
}

// Initialize initializes the Harmony OS application
func (app *HarmonyApp) Initialize() error {
	// Load configuration
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("failed to load configuration: %v", err)
	}
	app.config = cfg

	// Initialize API client with default URL (can be changed in settings).
	// HXC-202: composition lives in apiServerURL (server_url.go) so the IPv6
	// bracketing is shared with internal/server's bind path and is testable on
	// a host without the GUI toolchain.
	app.apiClient = NewAPIClient(apiServerURL(cfg))

	// Initialize database (optional - continue without it if not available)
	db, err := database.New(cfg.Database)
	if err != nil {
		log.Printf("Warning: Database not available: %v (continuing without persistence)", err)
	}
	app.db = db

	// Initialize Redis (optional - continue without it if not available)
	rds, err := redis.NewClient(&cfg.Redis)
	if err != nil {
		log.Printf("Warning: Redis not available: %v (continuing without caching)", err)
	}

	// Initialize components
	app.taskManager = task.NewTaskManager(db, rds)

	// Initialize worker manager with in-memory repository for standalone UI
	workerRepo := worker.NewInMemoryWorkerRepository()
	app.workerManager = worker.NewWorkerManager(workerRepo, 30*time.Second)

	// Initialize project manager
	app.projectManager = project.NewManager()

	// Initialize session manager
	app.sessionManager = session.NewManager()

	// W2c-1 cloud gate (operator mandate 2026-09-05, local-only adaptive
	// serving): wire the hosted-provider gate from configuration BEFORE any
	// handler can construct a provider. Mirrors internal/server/server.go.
	// Default false -- llm.cloud.enabled must be explicitly set true to
	// permit cloud construction.
	llm.SetCloudEnabled(cfg.LLM.Cloud.Enabled)

	// Initialize LLM manager
	app.llmManager = llm.NewModelManager()

	// Initialize notification engine
	app.notificationEngine = notification.NewNotificationEngine()

	// Initialize server for API calls
	app.server = server.New(cfg, db, rds)

	// Initialize monitoring
	app.monitor = monitoring.NewMonitor()

	// Initialize hardware detector
	app.hardwareDetector = hardware.NewHardwareDetector()

	// Initialize theme manager
	app.themeManager = NewThemeManager()

	// Initialize Harmony OS specific features
	if err := app.initializeHarmonyComponents(); err != nil {
		return fmt.Errorf("failed to initialize Harmony features: %v", err)
	}

	// Setup UI
	app.SetupUI()

	// Start background data updates
	app.startDataUpdates()

	return nil
}

// startDataUpdates starts periodic background data refresh.
//
// JOIN: updateDone is created fresh here and closed by the goroutine
// immediately before it returns. Cleanup() waits on it (bounded) before
// tearing down app.db -- see the updateDone field doc comment and Cleanup's
// doc comment for the use-after-close race this closes.
func (app *HarmonyApp) startDataUpdates() {
	app.updateTicker = time.NewTicker(5 * time.Second)
	app.updateDone = make(chan struct{})
	go func() {
		defer close(app.updateDone)

		// Initial data load
		app.refreshData()

		for {
			if !app.updateLoopTick(app.updateTicker.C, app.stopUpdate) {
				app.updateTicker.Stop()
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
// internal/persistence/store.go autoSaveTick and applications/desktop's
// identical fix; one of four confirmed instances of this defect class
// audited this session): Go's select chooses UNIFORMLY AT RANDOM among ALL
// cases ready at the instant it is evaluated. A naked `select { case
// <-ticker.C: refreshData(); case <-stopUpdate: return }` can therefore
// still pick the ticker.C branch and run one more refreshData() even when
// stopUpdate was ALREADY closed before the select was reached -- if this
// goroutine was scheduler-delayed long enough for a tick to also land in
// ticker.C by the time it finally runs. The non-blocking priority pre-check
// below closes that WIDE window: it runs FIRST, on every loop iteration,
// and returns immediately whenever stopUpdate is already closed BEFORE this
// call is even invoked.
//
// RESIDUAL WINDOW (closed by the inner re-check below): the pre-check only
// proves stopUpdate was open at the instant it ran. If close(stopUpdate)
// lands in the narrow gap BETWEEN the pre-check returning and the blocking
// select below being evaluated, and a tick is already buffered on tickerC,
// both cases are ready when the blocking select fires and Go's
// uniform-random pick can still choose the tickerC branch -- one stray
// refreshData() after Cleanup() has already returned to its caller. The
// inner non-blocking re-check inside the tickerC case closes this too.
//
// HONESTY (§11.4.6): this is the strongest ordering guarantee a plain
// channel select can offer. It is NOT a claim the race is closed to zero
// width -- under Go's async goroutine preemption (>=1.14) this goroutine can
// still be preempted between the final re-check and the refreshData() call
// that follows it while a close(stopUpdate) lands concurrently, an
// unavoidable, unobservable race no select-based implementation can
// provably eliminate. What IS delivered: cancellation observed at the final
// decision point always wins, AND (via the updateDone join in Cleanup, a
// SEPARATE mechanism) the caller never tears down app.db while this
// goroutine could still be mid-refreshData() -- the join guarantees the
// loop has RETURNED before teardown begins, which the select-priority fix
// alone does not.
//
// Returns false when the loop must stop (stopUpdate was signalled --
// checked FIRST, non-blocking, and again in the blocking select below),
// true when a tick was processed and the loop should continue.
func (app *HarmonyApp) updateLoopTick(tickerC <-chan time.Time, stop <-chan struct{}) bool {
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
	if app.updateLoopTickRaceHook != nil {
		app.updateLoopTickRaceHook()
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
		app.refreshData()
		return true
	case <-stop:
		return false
	}
}

// refreshData updates cached data from API and local managers
func (app *HarmonyApp) refreshData() {
	app.dataMu.Lock()
	defer app.dataMu.Unlock()

	ctx := context.Background()

	// Guarded by apiClient != nil so a HarmonyApp constructed without a
	// live API client (a partial-construction path -- e.g. a test that
	// drives startDataUpdates()/updateLoopTick in isolation without going
	// through the full Initialize() sequence, or any future error-recovery
	// path that builds a HarmonyApp before the API client is wired)
	// degrades to the same "no tasks/workers from API, fall back to
	// local" behaviour this function already applies to every OTHER
	// optional dependency below (projectManager, sessionManager,
	// llmManager), instead of a nil-pointer panic in this background
	// goroutine. Brings this function into structural parity with the
	// identically-shaped refreshData() in applications/desktop and
	// applications/aurora_os, neither of which has an API client field at
	// all -- apiClient was the sole unguarded optional dependency here.
	if app.apiClient != nil {
		// Try to fetch tasks from API first, fallback to local
		tasks, err := app.fetchTasksFromAPI()
		if err != nil {
			// Fallback to local task manager
			log.Printf("API tasks unavailable, using local: %v", err)
		} else {
			app.tasks = tasks
		}

		// Try to fetch workers from API first, fallback to local
		workers, err := app.fetchWorkersFromAPI()
		if err != nil {
			// Fallback to local worker manager
			log.Printf("API workers unavailable, using local: %v", err)
		} else {
			app.workers = workers
		}
	}

	// Refresh projects from local manager
	if app.projectManager != nil {
		projects, err := app.projectManager.ListProjects(ctx, "")
		if err == nil {
			app.projects = make([]APIProject, len(projects))
			for i, p := range projects {
				app.projects[i] = APIProject{
					ID:          p.ID,
					Name:        p.Name,
					Description: p.Description,
					Path:        p.Path,
					Type:        p.Type,
					Active:      p.Active,
					CreatedAt:   p.CreatedAt,
				}
			}
		}
	}

	// Refresh sessions from local manager
	if app.sessionManager != nil {
		sessions := app.sessionManager.GetAll()
		app.sessions = make([]APISession, len(sessions))
		for i, s := range sessions {
			app.sessions[i] = APISession{
				ID:          s.ID,
				Name:        s.Name,
				Description: s.Description,
				ProjectID:   s.ProjectID,
				Mode:        string(s.Mode),
				Status:      string(s.Status),
				CreatedAt:   s.CreatedAt,
			}
		}
	}

	// Refresh LLM providers
	if app.llmManager != nil {
		models := app.llmManager.GetAvailableModels()
		providers := make(map[string]bool)
		for _, model := range models {
			providers[string(model.Provider)] = true
		}
		app.llmProviders = make([]string, 0, len(providers))
		for provider := range providers {
			app.llmProviders = append(app.llmProviders, provider)
		}
	}
}

// fetchTasksFromAPI fetches tasks from the backend API
func (app *HarmonyApp) fetchTasksFromAPI() ([]APITask, error) {
	resp, err := app.apiClient.doRequest("GET", "/api/v1/tasks", nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API returned status %d", resp.StatusCode)
	}

	var response struct {
		Tasks []APITask `json:"tasks"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		return nil, err
	}

	return response.Tasks, nil
}

// fetchWorkersFromAPI fetches workers from the backend API
func (app *HarmonyApp) fetchWorkersFromAPI() ([]APIWorker, error) {
	resp, err := app.apiClient.doRequest("GET", "/api/v1/workers", nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API returned status %d", resp.StatusCode)
	}

	var response struct {
		Workers []APIWorker `json:"workers"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		return nil, err
	}

	return response.Workers, nil
}

// initializeHarmonyComponents initializes Harmony OS-specific features
func (app *HarmonyApp) initializeHarmonyComponents() error {
	// Initialize distributed engine
	distributedEngine := NewHarmonyDistributedEngine()

	// Initialize Harmony integration
	app.harmonyIntegration = &HarmonyIntegration{
		nativeServices: make(map[string]interface{}),
		systemAPI: &HarmonySystemAPI{
			deviceInfo: map[string]string{
				"platform":  "HarmonyOS",
				"version":   "4.0",
				"device":    "HelixCode Device",
				"ecosystem": "Harmony",
			},
			capabilities: []string{
				"distributed_computing",
				"cross_device_sync",
				"ai_acceleration",
				"multi_screen_collaboration",
				"super_device",
			},
			systemVersion: "HarmonyOS 4.0",
			kernelVersion: "Linux 5.10-Harmony",
		},
		distributedEngine: distributedEngine,
		harmonyContext:    context.Background(),
	}

	// Start data sync
	distributedEngine.dataSync.StartSync()

	// Initialize system monitor
	app.systemMonitor = &HarmonySystemMonitor{
		updateInterval: 5 * time.Second,
		monitoring:     true,
	}

	// Initialize resource manager
	app.resourceManager = &HarmonyResourceManager{
		resourcePolicies: map[string]string{
			"cpu":    "balanced",
			"memory": "optimized",
			"power":  "efficient",
		},
		optimization: true,
		autoTuning:   true,
	}

	// Initialize service coordinator
	app.serviceCoordinator = &HarmonyServiceCoordinator{
		services:        make(map[string]bool),
		serviceRegistry: make(map[string]interface{}),
		coordinator: &ServiceCoordinator{
			activeServices:  []string{},
			failoverEnabled: true,
		},
	}

	// Start system monitoring.
	//
	// JOIN (§11.4.115): monitorDone is created HERE, before the `go`
	// statement, so Cleanup() can never observe a nil monitorDone for a
	// monitor goroutine that is already running.
	app.monitorDone = make(chan struct{})
	go app.monitorSystem()

	log.Println("Harmony OS features initialized successfully")
	return nil
}

// monitorSystem continuously monitors system resources until the app's
// stopUpdate channel is closed by Cleanup().
//
// LIFECYCLE + DATA-RACE FIX (§11.4.115). The loop used to be:
//
//	for app.systemMonitor.monitoring {
//	    select {
//	    case <-ticker.C:
//	        app.updateSystemMetrics()
//	    }
//	}
//
// which carried TWO distinct defects.
//
//  1. DATA RACE (the reproducing failure). `monitoring` is a plain bool used
//     as a cross-goroutine stop flag with NO synchronisation: this goroutine
//     read it on every iteration while Cleanup() wrote `monitoring = false`
//     from the caller's goroutine. `go test -race ./applications/harmony_os/`
//     reported it deterministically as a read in monitorSystem against a
//     write in Cleanup via TestCleanup. This is the SAME CLASS as the
//     goroutine-lifecycle defect fixed in applications/desktop by commit
//     e879702c, but NOT the same instance: desktop's tickers had no stop
//     check at all (`for range ticker.C`), whereas this loop had one and
//     implemented it with an unsynchronised bool. The stop signal is now the
//     app's existing stopUpdate channel, which carries a proper
//     happens-before edge, via the shared fyneui.TickOrStop primitive.
//
//  2. UNSTOPPABLE FOR A FULL TICK. The `select` had a single case, so the
//     flag was only re-read after ticker.C fired — teardown lagged by up to
//     one whole updateInterval (5s in production). TickOrStop selects on the
//     stop channel too, so a close is observed immediately, and its priority
//     pre-check guarantees an already-closed stop never loses to a pending
//     tick (Go's select picks uniformly at random among ready cases).
//
// `monitoring` survives as the monitor's published STATUS (asserted by
// TestCleanup); it is no longer the loop's control variable, and it is now
// accessed only through the mutex-guarded accessors.
func (app *HarmonyApp) monitorSystem() {
	// JOIN: closed immediately before returning so Cleanup() can prove this
	// goroutine has stopped touching app.systemMonitor before it returns.
	defer close(app.monitorDone)

	ticker := time.NewTicker(app.systemMonitor.updateInterval)
	defer ticker.Stop()

	for fyneui.TickOrStop(ticker.C, app.stopUpdate) {
		// Update system metrics
		app.updateSystemMetrics()
	}
}

// updateSystemMetrics updates current system metrics from real system data.
//
// DATA-RACE FIX (§11.4.115): this runs on the monitorSystem goroutine while
// createHarmonySystemTab reads the same fields on the UI goroutine, so every
// store below is made under systemMonitor.mu. The formatted log line at the
// end reads back COPIES captured inside the critical section rather than
// re-reading the fields after unlocking, which would reintroduce the race it
// is meant to close.
func (app *HarmonyApp) updateSystemMetrics() {
	// Get memory statistics from runtime
	var memStats runtime.MemStats
	runtime.ReadMemStats(&memStats)

	// Get hardware profile
	profile := app.hardwareDetector.GetProfile()

	// Calculate CPU usage based on goroutines and CPU count
	numGoroutines := runtime.NumGoroutine()
	numCPU := runtime.NumCPU()
	// Estimate CPU usage based on goroutine count (simplified)
	estimatedCPUUsage := float64(numGoroutines) / float64(numCPU*10) * 100
	if estimatedCPUUsage > 100 {
		estimatedCPUUsage = 100
	}
	memoryUsageMB := float64(memStats.Alloc) / (1024 * 1024)

	app.systemMonitor.mu.Lock()
	app.systemMonitor.cpuUsage = estimatedCPUUsage

	// Memory usage from runtime (convert to MB)
	app.systemMonitor.memoryUsage = memoryUsageMB

	// GPU usage - would need platform-specific implementation
	// For Harmony OS, this would use HarmonyOS NPU/GPU APIs
	app.systemMonitor.gpuUsage = 0 // Set to 0 when not available

	// Network traffic - would need platform-specific implementation
	// Track approximate based on time since last check
	app.systemMonitor.networkTraffic = 0

	// Disk I/O - would need platform-specific implementation
	app.systemMonitor.diskIO = 0

	// Temperature - would need platform-specific implementation
	// For Harmony OS, this would use thermal APIs
	app.systemMonitor.temperature = 0

	// Power usage - would need platform-specific implementation
	// For Harmony OS, this would use power management APIs
	app.systemMonitor.powerUsage = 0
	app.systemMonitor.mu.Unlock()

	// Log metrics for debugging (optional). Uses the locals captured above,
	// NOT a re-read of the (now unlocked) fields.
	log.Printf("System metrics updated - CPU: %.1f%%, Memory: %.1fMB, Goroutines: %d, CPUs: %d, Arch: %s",
		estimatedCPUUsage,
		memoryUsageMB,
		numGoroutines,
		profile.CPU.Cores,
		profile.OS.Arch)
}

// GetSystemStats returns formatted system statistics for display
func (app *HarmonyApp) GetSystemStats() map[string]interface{} {
	var memStats runtime.MemStats
	runtime.ReadMemStats(&memStats)

	profile := app.hardwareDetector.GetProfile()

	return map[string]interface{}{
		"cpu_cores":       runtime.NumCPU(),
		"cpu_arch":        runtime.GOARCH,
		"os":              runtime.GOOS,
		"goroutines":      runtime.NumGoroutine(),
		"memory_alloc":    memStats.Alloc,
		"memory_total":    memStats.TotalAlloc,
		"memory_sys":      memStats.Sys,
		"gc_cycles":       memStats.NumGC,
		"go_version":      runtime.Version(),
		"hardware_cpu":    profile.CPU.Cores,
		"hardware_memory": profile.Memory.Total,
	}
}

// SetupUI creates and configures the user interface
func (app *HarmonyApp) SetupUI() {
	// Apply Harmony theme
	app.fyneApp.Settings().SetTheme(app.themeManager.GetCustomTheme())

	// Create main window
	app.mainWindow = app.fyneApp.NewWindow(app.tr("harmony_os_gui_window_title", nil))
	app.mainWindow.Resize(fyne.NewSize(1200, 800))
	app.mainWindow.CenterOnScreen()

	// Create status bar
	app.statusBar = widget.NewLabel(app.tr("harmony_os_gui_status_ready", nil))
	app.statusBar.Alignment = fyne.TextAlignCenter

	// Create tabs
	app.tabs = container.NewAppTabs(
		container.NewTabItem(app.tr("harmony_os_gui_tab_dashboard", nil), app.createDashboardTab()),
		container.NewTabItem(app.tr("harmony_os_gui_tab_tasks", nil), app.createTasksTab()),
		container.NewTabItem(app.tr("harmony_os_gui_tab_workers", nil), app.createWorkersTab()),
		container.NewTabItem(app.tr("harmony_os_gui_tab_projects", nil), app.createProjectsTab()),
		container.NewTabItem(app.tr("harmony_os_gui_tab_sessions", nil), app.createSessionsTab()),
		container.NewTabItem(app.tr("harmony_os_gui_tab_llm", nil), app.createLLMTab()),
		container.NewTabItem(app.tr("harmony_os_gui_tab_harmony_system", nil), app.createHarmonySystemTab()),
		container.NewTabItem(app.tr("harmony_os_gui_tab_distributed_services", nil), app.createDistributedServicesTab()),
		container.NewTabItem(app.tr("harmony_os_gui_tab_resource_management", nil), app.createResourceManagementTab()),
		container.NewTabItem(app.tr("harmony_os_gui_tab_settings", nil), app.createSettingsTab()),
	)

	// Create main layout
	mainContent := container.NewBorder(
		nil,           // top
		app.statusBar, // bottom
		nil,           // left
		nil,           // right
		app.tabs,      // center
	)

	app.mainWindow.SetContent(mainContent)
}

// createDashboardTab creates the dashboard tab
func (app *HarmonyApp) createDashboardTab() fyne.CanvasObject {
	// Welcome label
	welcomeLabel := widget.NewLabelWithStyle(
		app.tr("harmony_os_gui_window_title", nil),
		fyne.TextAlignCenter,
		fyne.TextStyle{Bold: true},
	)

	// System info
	systemInfo := widget.NewCard(
		app.tr("harmony_os_gui_card_system_info_title", nil),
		app.tr("harmony_os_gui_card_system_info_subtitle", nil),
		widget.NewLabel(fmt.Sprintf(
			"Platform: %s\nVersion: %s\nKernel: %s\nEcosystem: Harmony",
			app.harmonyIntegration.systemAPI.systemVersion,
			app.harmonyIntegration.systemAPI.deviceInfo["version"],
			app.harmonyIntegration.systemAPI.kernelVersion,
		)),
	)

	// Quick stats
	statsCard := widget.NewCard(
		app.tr("harmony_os_gui_card_quick_stats_title", nil),
		app.tr("harmony_os_gui_card_quick_stats_subtitle", nil),
		widget.NewLabel(app.tr("harmony_os_gui_loading_stats", nil)),
	)

	// Harmony features
	featuresCard := widget.NewCard(
		app.tr("harmony_os_gui_card_features_title", nil),
		app.tr("harmony_os_gui_card_features_subtitle", nil),
		widget.NewLabel(fmt.Sprintf(
			"• Distributed Computing\n• Cross-Device Sync\n• AI Acceleration\n• Multi-Screen Collaboration\n• Super Device Integration",
		)),
	)

	return container.NewVBox(
		welcomeLabel,
		layout.NewSpacer(),
		systemInfo,
		statsCard,
		featuresCard,
		layout.NewSpacer(),
	)
}

// createTasksTab creates the tasks management tab
func (app *HarmonyApp) createTasksTab() fyne.CanvasObject {
	// Task list with dynamic data
	taskList := widget.NewList(
		func() int {
			app.dataMu.RLock()
			defer app.dataMu.RUnlock()
			return len(app.tasks)
		},
		func() fyne.CanvasObject {
			return container.NewHBox(
				widget.NewLabel("Status"),
				widget.NewLabel("Type"),
				widget.NewLabel("Description"),
			)
		},
		func(id widget.ListItemID, obj fyne.CanvasObject) {
			app.dataMu.RLock()
			defer app.dataMu.RUnlock()
			if id < len(app.tasks) {
				t := app.tasks[id]
				hbox := obj.(*fyne.Container)
				hbox.Objects[0].(*widget.Label).SetText(fmt.Sprintf("[%s]", t.Status))
				hbox.Objects[1].(*widget.Label).SetText(t.Type)
				hbox.Objects[2].(*widget.Label).SetText(t.Description)
			}
		},
	)

	taskCard := widget.NewCard(app.tr("harmony_os_gui_card_tasks_title", nil), "", taskList)

	// Task type selector for new tasks
	taskTypeSelect := widget.NewSelect([]string{"planning", "building", "testing", "refactoring", "debugging"}, nil)
	taskTypeSelect.SetSelected("building")

	// Priority selector
	prioritySelect := widget.NewSelect([]string{"low", "normal", "high", "critical"}, nil)
	prioritySelect.SetSelected("normal")

	// Task description input
	taskDescEntry := widget.NewEntry()
	taskDescEntry.SetPlaceHolder(app.tr("harmony_os_gui_placeholder_task_description", nil))

	// Action buttons
	actions := container.NewVBox(
		widget.NewLabel(app.tr("harmony_os_gui_form_new_task_header", nil)),
		widget.NewLabel("Type:"),
		taskTypeSelect,
		widget.NewLabel("Priority:"),
		prioritySelect,
		widget.NewLabel("Description:"),
		taskDescEntry,
		widget.NewButton(app.tr("harmony_os_gui_button_create_task", nil), func() {
			if taskDescEntry.Text == "" {
				dialog.ShowError(fmt.Errorf("description is required"), app.mainWindow)
				return
			}

			// Create task via distributed engine for Harmony OS
			priority := app.harmonyIntegration.distributedEngine.taskScheduler.priorityLevels[prioritySelect.Selected]
			task, err := app.harmonyIntegration.distributedEngine.ScheduleTask(
				taskTypeSelect.Selected,
				taskDescEntry.Text,
				priority,
			)
			if err != nil {
				dialog.ShowError(err, app.mainWindow)
			} else {
				taskDescEntry.SetText("")
				taskList.Refresh()
				app.statusBar.SetText(app.tr("harmony_os_gui_status_task_created", map[string]any{"ID": task.ID, "Device": task.DeviceID}))
				dialog.ShowInformation(app.tr("harmony_os_gui_dialog_title_success", nil), app.tr("harmony_os_gui_dialog_task_scheduled", map[string]any{"ID": task.ID}), app.mainWindow)
			}
		}),
		widget.NewSeparator(),
		widget.NewButton(app.tr("harmony_os_gui_button_refresh", nil), func() {
			app.refreshData()
			taskList.Refresh()
			app.statusBar.SetText(app.tr("harmony_os_gui_status_tasks_refreshed", nil))
		}),
	)

	return container.NewBorder(nil, nil, nil, actions, taskCard)
}

// createWorkersTab creates the workers management tab
func (app *HarmonyApp) createWorkersTab() fyne.CanvasObject {
	// Worker list with dynamic data
	workerList := widget.NewList(
		func() int {
			app.dataMu.RLock()
			defer app.dataMu.RUnlock()
			return len(app.workers)
		},
		func() fyne.CanvasObject {
			return container.NewHBox(
				widget.NewLabel("Status"),
				widget.NewLabel("ID"),
				widget.NewLabel("Host"),
				widget.NewLabel("Health"),
			)
		},
		func(id widget.ListItemID, obj fyne.CanvasObject) {
			app.dataMu.RLock()
			defer app.dataMu.RUnlock()
			if id < len(app.workers) {
				w := app.workers[id]
				hbox := obj.(*fyne.Container)
				hbox.Objects[0].(*widget.Label).SetText(fmt.Sprintf("[%s]", w.Status))
				hbox.Objects[1].(*widget.Label).SetText(w.ID)
				hbox.Objects[2].(*widget.Label).SetText(fmt.Sprintf("%s:%d", w.Host, w.Port))
				healthStatus := "unhealthy"
				if w.Healthy {
					healthStatus = "healthy"
				}
				hbox.Objects[3].(*widget.Label).SetText(healthStatus)
			}
		},
	)

	workerCard := widget.NewCard(app.tr("harmony_os_gui_card_workers_title", nil), "", workerList)

	// Worker configuration inputs
	hostEntry := widget.NewEntry()
	hostEntry.SetPlaceHolder("hostname or IP")
	portEntry := widget.NewEntry()
	portEntry.SetPlaceHolder("22")
	portEntry.SetText("22")
	userEntry := widget.NewEntry()
	userEntry.SetPlaceHolder("username")

	actions := container.NewVBox(
		widget.NewLabel(app.tr("harmony_os_gui_form_add_worker_header", nil)),
		widget.NewLabel("Host:"),
		hostEntry,
		widget.NewLabel("Port:"),
		portEntry,
		widget.NewLabel("User:"),
		userEntry,
		widget.NewButton(app.tr("harmony_os_gui_button_add_worker", nil), func() {
			if hostEntry.Text == "" {
				dialog.ShowError(fmt.Errorf("host is required"), app.mainWindow)
				return
			}

			// Add worker to distributed engine as a Harmony device
			device := HarmonyDevice{
				ID:     fmt.Sprintf("worker-%s-%d", hostEntry.Text, time.Now().UnixNano()),
				Name:   fmt.Sprintf("Worker@%s", hostEntry.Text),
				Type:   "remote_worker",
				Status: "pending",
				Capabilities: []string{
					"task_execution",
					"code_analysis",
					"build",
					"test",
				},
				Resources: HarmonyResources{
					CPUUsage:    0,
					MemoryUsage: 0,
					GPUUsage:    0,
					Available:   true,
				},
				LastSeen: time.Now(),
			}

			app.harmonyIntegration.distributedEngine.AddDevice(device)

			// Also add as API worker for UI display
			app.dataMu.Lock()
			app.workers = append(app.workers, APIWorker{
				ID:           device.ID,
				Host:         hostEntry.Text,
				Port:         22,
				User:         userEntry.Text,
				Status:       "pending",
				Healthy:      false,
				Capabilities: device.Capabilities,
				LastSeen:     time.Now(),
			})
			app.dataMu.Unlock()

			hostEntry.SetText("")
			userEntry.SetText("")
			workerList.Refresh()
			app.statusBar.SetText(fmt.Sprintf("Worker %s added", device.ID))
		}),
		widget.NewSeparator(),
		widget.NewButton(app.tr("harmony_os_gui_button_refresh", nil), func() {
			app.refreshData()
			workerList.Refresh()
			app.statusBar.SetText(app.tr("harmony_os_gui_status_workers_refreshed", nil))
		}),
		widget.NewButton(app.tr("harmony_os_gui_button_discover_devices", nil), func() {
			devices, err := app.harmonyIntegration.distributedEngine.DiscoverDevices()
			if err != nil {
				// Surface the sentinel loudly instead of printing the
				// previous "Found 0 Harmony devices" PASS-bluff (round-31
				// §11.4). The dialog text mirrors the sentinel message so
				// the user sees WHY 0 devices were returned.
				app.statusBar.SetText(app.tr("harmony_os_gui_status_discover_failed", map[string]any{"Error": fmt.Sprintf("%v", err)}))
				dialog.ShowError(err, app.mainWindow)
				return
			}
			app.statusBar.SetText(app.tr("harmony_os_gui_status_devices_found", map[string]any{"Count": len(devices)}))
		}),
	)

	return container.NewBorder(nil, nil, nil, actions, workerCard)
}

// createProjectsTab creates the projects tab
func (app *HarmonyApp) createProjectsTab() fyne.CanvasObject {
	// Project list with dynamic data
	app.projectList = widget.NewList(
		func() int {
			app.dataMu.RLock()
			defer app.dataMu.RUnlock()
			return len(app.projects)
		},
		func() fyne.CanvasObject {
			return container.NewHBox(
				widget.NewLabel("Name"),
				widget.NewLabel("Type"),
				widget.NewLabel("Status"),
			)
		},
		func(id widget.ListItemID, obj fyne.CanvasObject) {
			app.dataMu.RLock()
			defer app.dataMu.RUnlock()
			if id < len(app.projects) {
				p := app.projects[id]
				hbox := obj.(*fyne.Container)
				hbox.Objects[0].(*widget.Label).SetText(p.Name)
				hbox.Objects[1].(*widget.Label).SetText(fmt.Sprintf("(%s)", p.Type))
				activeStatus := ""
				if p.Active {
					activeStatus = " [ACTIVE]"
				}
				hbox.Objects[2].(*widget.Label).SetText(activeStatus)
			}
		},
	)

	// Project details panel
	projectDetailsLabel := widget.NewLabel(app.tr("harmony_os_gui_project_select_prompt", nil))
	projectDetailsLabel.Wrapping = fyne.TextWrapWord

	app.projectList.OnSelected = func(id widget.ListItemID) {
		app.dataMu.RLock()
		defer app.dataMu.RUnlock()
		if id < len(app.projects) {
			p := app.projects[id]
			details := fmt.Sprintf("Name: %s\nType: %s\nPath: %s\nDescription: %s\nCreated: %s",
				p.Name, p.Type, p.Path, p.Description,
				p.CreatedAt.Format(time.RFC822))
			projectDetailsLabel.SetText(details)
		}
	}

	projectListCard := widget.NewCard(app.tr("harmony_os_gui_card_projects_title", nil), "", app.projectList)
	projectDetailsCard := widget.NewCard(app.tr("harmony_os_gui_card_project_details_title", nil), "", projectDetailsLabel)

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
		widget.NewLabel(app.tr("harmony_os_gui_project_create_header", nil)),
		widget.NewLabel("Name:"),
		nameEntry,
		widget.NewLabel("Description:"),
		descEntry,
		widget.NewLabel("Path:"),
		pathEntry,
		widget.NewLabel("Type:"),
		typeSelect,
		widget.NewButton(app.tr("harmony_os_gui_button_create_project", nil), func() {
			if app.projectManager != nil && nameEntry.Text != "" && pathEntry.Text != "" {
				ctx := context.Background()
				proj, err := app.projectManager.CreateProject(ctx, nameEntry.Text, descEntry.Text, pathEntry.Text, typeSelect.Selected)
				if err != nil {
					dialog.ShowError(err, app.mainWindow)
				} else {
					nameEntry.SetText("")
					descEntry.SetText("")
					pathEntry.SetText("")
					app.refreshData()
					app.projectList.Refresh()
					app.statusBar.SetText(app.tr("harmony_os_gui_status_project_created", map[string]any{"Name": proj.Name}))
					dialog.ShowInformation(app.tr("harmony_os_gui_dialog_title_success", nil), app.tr("harmony_os_gui_dialog_project_created", nil), app.mainWindow)
				}
			} else {
				dialog.ShowError(fmt.Errorf("name and path are required"), app.mainWindow)
			}
		}),
		widget.NewSeparator(),
		widget.NewButton(app.tr("harmony_os_gui_button_set_active", nil), func() {
			if app.projectList.Length() > 0 {
				// Get selected project
				app.dataMu.RLock()
				selectedIndex := -1
				// Note: In Fyne, we need to track selection separately
				app.dataMu.RUnlock()

				if selectedIndex >= 0 {
					ctx := context.Background()
					p := app.projects[selectedIndex]
					err := app.projectManager.SetActiveProject(ctx, p.ID)
					if err != nil {
						dialog.ShowError(err, app.mainWindow)
					} else {
						app.refreshData()
						app.projectList.Refresh()
						app.statusBar.SetText(app.tr("harmony_os_gui_status_project_active", map[string]any{"Name": p.Name}))
					}
				}
			}
		}),
		widget.NewButton(app.tr("harmony_os_gui_button_refresh", nil), func() {
			app.refreshData()
			app.projectList.Refresh()
			app.statusBar.SetText(app.tr("harmony_os_gui_status_projects_refreshed", nil))
		}),
	)

	leftPanel := container.NewVSplit(projectListCard, projectDetailsCard)
	leftPanel.SetOffset(0.6)

	return container.NewBorder(nil, nil, nil, createForm, leftPanel)
}

// createSessionsTab creates the sessions tab
func (app *HarmonyApp) createSessionsTab() fyne.CanvasObject {
	// Session list with dynamic data
	app.sessionList = widget.NewList(
		func() int {
			app.dataMu.RLock()
			defer app.dataMu.RUnlock()
			return len(app.sessions)
		},
		func() fyne.CanvasObject {
			return container.NewHBox(
				widget.NewLabel("Name"),
				widget.NewLabel("Status"),
				widget.NewLabel("Mode"),
			)
		},
		func(id widget.ListItemID, obj fyne.CanvasObject) {
			app.dataMu.RLock()
			defer app.dataMu.RUnlock()
			if id < len(app.sessions) {
				s := app.sessions[id]
				hbox := obj.(*fyne.Container)
				hbox.Objects[0].(*widget.Label).SetText(s.Name)
				hbox.Objects[1].(*widget.Label).SetText(fmt.Sprintf("[%s]", s.Status))
				hbox.Objects[2].(*widget.Label).SetText(s.Mode)
			}
		},
	)

	// Session details panel
	sessionDetailsLabel := widget.NewLabel(app.tr("harmony_os_gui_session_select_prompt", nil))
	sessionDetailsLabel.Wrapping = fyne.TextWrapWord

	selectedSessionID := ""
	app.sessionList.OnSelected = func(id widget.ListItemID) {
		app.dataMu.RLock()
		defer app.dataMu.RUnlock()
		if id < len(app.sessions) {
			s := app.sessions[id]
			selectedSessionID = s.ID
			details := app.tr("harmony_os_gui_session_details_fmt", map[string]any{
				"Name": s.Name, "Mode": s.Mode, "Status": s.Status,
				"ProjectID": s.ProjectID, "Description": s.Description,
				"Created": s.CreatedAt.Format(time.RFC822),
			})
			sessionDetailsLabel.SetText(details)
		}
	}

	sessionListCard := widget.NewCard(app.tr("harmony_os_gui_card_sessions_title", nil), "", app.sessionList)
	sessionDetailsCard := widget.NewCard(app.tr("harmony_os_gui_card_session_details_title", nil), "", sessionDetailsLabel)

	// Session creation form
	nameEntry := widget.NewEntry()
	nameEntry.SetPlaceHolder("Session name")
	descEntry := widget.NewEntry()
	descEntry.SetPlaceHolder("Description")
	projectIDEntry := widget.NewEntry()
	projectIDEntry.SetPlaceHolder("Project ID")
	modeSelect := widget.NewSelect([]string{"planning", "building", "testing", "refactoring", "debugging", "deployment"}, nil)
	modeSelect.SetSelected("building")

	actions := container.NewVBox(
		widget.NewLabel(app.tr("harmony_os_gui_session_create_header", nil)),
		widget.NewLabel("Name:"),
		nameEntry,
		widget.NewLabel("Description:"),
		descEntry,
		widget.NewLabel("Project ID:"),
		projectIDEntry,
		widget.NewLabel("Mode:"),
		modeSelect,
		widget.NewButton(app.tr("harmony_os_gui_button_create_session", nil), func() {
			if app.sessionManager != nil && nameEntry.Text != "" && projectIDEntry.Text != "" {
				mode := session.Mode(modeSelect.Selected)
				sess, err := app.sessionManager.Create(projectIDEntry.Text, nameEntry.Text, descEntry.Text, mode)
				if err != nil {
					dialog.ShowError(err, app.mainWindow)
				} else {
					nameEntry.SetText("")
					descEntry.SetText("")
					projectIDEntry.SetText("")
					app.refreshData()
					app.sessionList.Refresh()
					app.statusBar.SetText(app.tr("harmony_os_gui_status_session_created", map[string]any{"Name": sess.Name}))
					dialog.ShowInformation(app.tr("harmony_os_gui_dialog_title_success", nil), app.tr("harmony_os_gui_dialog_session_created", nil), app.mainWindow)
				}
			} else {
				dialog.ShowError(fmt.Errorf("name and project ID are required"), app.mainWindow)
			}
		}),
		widget.NewSeparator(),
		widget.NewLabel(app.tr("harmony_os_gui_session_controls_header", nil)),
		widget.NewButton(app.tr("harmony_os_gui_button_start_session", nil), func() {
			if app.sessionManager != nil && selectedSessionID != "" {
				err := app.sessionManager.Start(selectedSessionID)
				if err != nil {
					dialog.ShowError(err, app.mainWindow)
				} else {
					app.refreshData()
					app.sessionList.Refresh()
					app.statusBar.SetText(app.tr("harmony_os_gui_status_session_started", nil))
				}
			}
		}),
		widget.NewButton(app.tr("harmony_os_gui_button_pause_session", nil), func() {
			if app.sessionManager != nil && selectedSessionID != "" {
				err := app.sessionManager.Pause(selectedSessionID)
				if err != nil {
					dialog.ShowError(err, app.mainWindow)
				} else {
					app.refreshData()
					app.sessionList.Refresh()
					app.statusBar.SetText(app.tr("harmony_os_gui_status_session_paused", nil))
				}
			}
		}),
		widget.NewButton(app.tr("harmony_os_gui_button_resume_session", nil), func() {
			if app.sessionManager != nil && selectedSessionID != "" {
				err := app.sessionManager.Resume(selectedSessionID)
				if err != nil {
					dialog.ShowError(err, app.mainWindow)
				} else {
					app.refreshData()
					app.sessionList.Refresh()
					app.statusBar.SetText(app.tr("harmony_os_gui_status_session_resumed", nil))
				}
			}
		}),
		widget.NewButton(app.tr("harmony_os_gui_button_complete_session", nil), func() {
			if app.sessionManager != nil && selectedSessionID != "" {
				err := app.sessionManager.Complete(selectedSessionID)
				if err != nil {
					dialog.ShowError(err, app.mainWindow)
				} else {
					app.refreshData()
					app.sessionList.Refresh()
					app.statusBar.SetText(app.tr("harmony_os_gui_status_session_completed", nil))
				}
			}
		}),
		widget.NewSeparator(),
		widget.NewButton(app.tr("harmony_os_gui_button_refresh", nil), func() {
			app.refreshData()
			app.sessionList.Refresh()
			app.statusBar.SetText(app.tr("harmony_os_gui_status_sessions_refreshed", nil))
		}),
	)

	leftPanel := container.NewVSplit(sessionListCard, sessionDetailsCard)
	leftPanel.SetOffset(0.6)

	return container.NewBorder(nil, nil, nil, actions, leftPanel)
}

// createLLMTab creates the LLM tab
func (app *HarmonyApp) createLLMTab() fyne.CanvasObject {
	// Available models list
	modelList := widget.NewList(
		func() int {
			if app.llmManager == nil {
				return 0
			}
			return len(app.llmManager.GetAvailableModels())
		},
		func() fyne.CanvasObject {
			return container.NewHBox(
				widget.NewLabel("Model"),
				widget.NewLabel("Provider"),
			)
		},
		func(id widget.ListItemID, obj fyne.CanvasObject) {
			models := app.llmManager.GetAvailableModels()
			if id < len(models) {
				m := models[id]
				hbox := obj.(*fyne.Container)
				hbox.Objects[0].(*widget.Label).SetText(m.Name)
				hbox.Objects[1].(*widget.Label).SetText(string(m.Provider))
			}
		},
	)

	modelListCard := widget.NewCard(app.tr("harmony_os_gui_card_available_models", nil), "", modelList)

	// Model details panel
	modelDetailsLabel := widget.NewLabel(app.tr("harmony_os_gui_model_select_prompt", nil))
	modelDetailsLabel.Wrapping = fyne.TextWrapWord

	modelList.OnSelected = func(id widget.ListItemID) {
		models := app.llmManager.GetAvailableModels()
		if id < len(models) {
			m := models[id]
			caps := make([]string, len(m.Capabilities))
			for i, c := range m.Capabilities {
				caps[i] = string(c)
			}
			details := app.tr("harmony_os_gui_model_details_fmt", map[string]any{
				"Name": m.Name, "Provider": m.Provider,
				"ContextSize": m.ContextSize, "Capabilities": fmt.Sprintf("%v", caps),
			})
			modelDetailsLabel.SetText(details)
		}
	}

	modelDetailsCard := widget.NewCard(app.tr("harmony_os_gui_card_model_details_title", nil), "", modelDetailsLabel)

	// Chat interface
	app.chatHistory = widget.NewMultiLineEntry()
	app.chatHistory.SetPlaceHolder(app.tr("harmony_os_gui_placeholder_chat_history", nil))
	app.chatHistory.Disable()
	app.chatHistory.Wrapping = fyne.TextWrapWord

	app.chatInput = widget.NewMultiLineEntry()
	app.chatInput.SetPlaceHolder(app.tr("harmony_os_gui_placeholder_chat_input", nil))
	app.chatInput.SetMinRowsVisible(3)

	// Provider/model selection for chat
	app.llmProviderSel = widget.NewSelect([]string{"ollama", "openai", "anthropic", "gemini", "local"}, nil)
	app.llmProviderSel.SetSelected("ollama")

	modelNameEntry := widget.NewEntry()
	modelNameEntry.SetPlaceHolder(app.tr("harmony_os_gui_placeholder_model_name", nil))
	modelNameEntry.SetText("llama2")

	sendButton := widget.NewButton(app.tr("harmony_os_gui_button_send_message", nil), func() {
		if app.chatInput.Text == "" {
			return
		}

		// Add user message to history
		currentHistory := app.chatHistory.Text
		userMessage := app.chatInput.Text
		userMsg := fmt.Sprintf("\n[User]: %s\n", userMessage)
		app.chatHistory.SetText(currentHistory + userMsg)

		// Clear input immediately
		app.chatInput.SetText("")

		// Widget reads hoisted OUT of the goroutine (§11.4.115): .Selected and
		// .Text were read INSIDE the worker below, racing the renderer exactly
		// like the write did. Read here — this callback runs on the UI
		// goroutine — and pass the values in, which is also what
		// applications/desktop does with its provider selection.
		providerName := app.llmProviderSel.Selected
		modelName := modelNameEntry.Text

		// Make LLM call in goroutine to not block UI
		// In Harmony OS, this could leverage distributed AI capabilities across devices
		go func(msg, providerName, modelName string) {
			var responseMsg string

			if app.llmManager != nil {
				// Get provider from manager using provider type
				providerType := llm.ProviderType(providerName)
				provider, err := app.llmManager.GetProviderForModel(modelName, providerType)
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
						responseMsg = fmt.Sprintf("[AI (%s/%s)]: Error: %v\n", providerName, modelName, err)
					} else {
						responseMsg = fmt.Sprintf("[AI (%s/%s)]: %s\n", providerName, modelName, response.Content)
					}
				} else {
					responseMsg = app.tr("harmony_os_gui_chat_provider_unavailable_fmt", map[string]any{
						"Provider": providerName, "Model": modelName,
					})
				}
			} else {
				// No LLM manager configured - show informative message
				responseMsg = app.tr("harmony_os_gui_chat_llm_not_initialized_fmt", map[string]any{
					"Provider": providerName, "Model": modelName,
				})
			}

			// THREAD-AFFINITY FIX (§11.4.115). The comment that stood here
			// claimed "Update UI on main thread" — it was FALSE: this body
			// runs on the worker goroutine, and the bare SetText raced the
			// renderer. It is the identical falsehood purged from
			// applications/desktop by commit e879702c; it is corrected rather
			// than merely deleted so a future edit is not invited to
			// re-introduce the bare call on its authority.
			//
			// DoAndWait (not Do) because this is the one-shot terminal append
			// that ends the turn: when this goroutine returns, the UI has
			// provably been updated. The read and the write are in the SAME
			// closure — `chatHistory.Text + responseMsg` reads the widget, so
			// wrapping only the write would leave the read racing.
			fyneui.DoAndWait(func() {
				app.chatHistory.SetText(app.chatHistory.Text + responseMsg)
			})
		}(userMessage, providerName, modelName)
	})

	clearButton := widget.NewButton(app.tr("harmony_os_gui_button_clear_chat", nil), func() {
		app.chatHistory.SetText("")
	})

	chatControls := container.NewVBox(
		widget.NewLabel(app.tr("harmony_os_gui_form_chat_settings_header", nil)),
		widget.NewLabel(app.tr("harmony_os_gui_label_provider", nil)),
		app.llmProviderSel,
		widget.NewLabel(app.tr("harmony_os_gui_label_model", nil)),
		modelNameEntry,
		widget.NewSeparator(),
		sendButton,
		clearButton,
	)

	chatPanel := container.NewBorder(
		widget.NewLabel(app.tr("harmony_os_gui_form_chat_with_ai_header", nil)),
		container.NewBorder(nil, nil, nil, chatControls, app.chatInput),
		nil, nil,
		app.chatHistory,
	)

	chatCard := widget.NewCard(app.tr("harmony_os_gui_card_llm_chat_title", nil), "", chatPanel)

	// Provider health status
	healthLabel := widget.NewLabel(app.tr("harmony_os_gui_health_checking", nil))

	// Start health check goroutine.
	//
	// THREAD-AFFINITY + LIFECYCLE FIX (§11.4.115), mirroring the provider-health
	// ticker fixed in applications/desktop by commit e879702c — a site that was
	// likewise absent from the original defect report and found only by auditing
	// every goroutine in the file. Both SetText calls live INSIDE checkHealth so
	// the eager pre-loop invocation is covered too: it also runs on this worker
	// goroutine, so dispatching only inside the loop would have left the very
	// first paint racing. DoAndWait matches desktop's choice for these one-shot
	// status updates.
	go func() {
		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()

		checkHealth := func() {
			if app.llmManager == nil {
				noManager := app.tr("harmony_os_gui_health_no_manager", nil)
				fyneui.DoAndWait(func() { healthLabel.SetText(noManager) })
				return
			}
			ctx := context.Background()
			health := app.llmManager.HealthCheck(ctx)
			healthText := app.tr("harmony_os_gui_health_header", nil) + "\n"
			for provider, status := range health {
				healthText += fmt.Sprintf("- %s: %s\n", provider, status.Status)
			}
			if len(health) == 0 {
				healthText += app.tr("harmony_os_gui_health_no_providers", nil)
			}
			fyneui.DoAndWait(func() { healthLabel.SetText(healthText) })
		}

		// LIFECYCLE: this was `for range ticker.C`, which can never terminate —
		// the goroutine outlived the window it painted into and leaked for the
		// process lifetime, still mutating healthLabel after teardown. It now
		// honours the app's existing stopUpdate channel (closed by Cleanup()).
		checkHealth()
		for fyneui.TickOrStop(ticker.C, app.stopUpdate) {
			checkHealth()
		}
	}()

	healthCard := widget.NewCard(app.tr("harmony_os_gui_card_provider_status_title", nil), "", healthLabel)

	// Layout
	leftPanel := container.NewVSplit(modelListCard, modelDetailsCard)
	leftPanel.SetOffset(0.5)

	rightPanel := container.NewVSplit(chatCard, healthCard)
	rightPanel.SetOffset(0.7)

	return container.NewHSplit(leftPanel, rightPanel)
}

// createHarmonySystemTab creates the Harmony OS system monitoring tab
func (app *HarmonyApp) createHarmonySystemTab() fyne.CanvasObject {
	// System metrics
	metricsLabel := widget.NewLabel(app.tr("harmony_os_gui_label_system_metrics", nil))

	// DATA-RACE FIX (§11.4.115): these five fields are written by the
	// monitorSystem goroutine (updateSystemMetrics), so snapshot them under
	// the read lock rather than dereferencing them inline five times. The
	// snapshot also makes the five labels mutually consistent — inline reads
	// could straddle a metrics update and paint values from two different
	// sampling rounds.
	app.systemMonitor.mu.RLock()
	cpuUsage := app.systemMonitor.cpuUsage
	memoryUsage := app.systemMonitor.memoryUsage
	gpuUsage := app.systemMonitor.gpuUsage
	temperature := app.systemMonitor.temperature
	powerUsage := app.systemMonitor.powerUsage
	app.systemMonitor.mu.RUnlock()

	cpuLabel := widget.NewLabel(app.tr("harmony_os_gui_metric_cpu_usage_fmt", map[string]any{"Value": fmt.Sprintf("%.1f", cpuUsage)}))
	memLabel := widget.NewLabel(app.tr("harmony_os_gui_metric_memory_usage_fmt", map[string]any{"Value": fmt.Sprintf("%.0f", memoryUsage)}))
	gpuLabel := widget.NewLabel(app.tr("harmony_os_gui_metric_gpu_usage_fmt", map[string]any{"Value": fmt.Sprintf("%.1f", gpuUsage)}))
	tempLabel := widget.NewLabel(app.tr("harmony_os_gui_metric_temperature_fmt", map[string]any{"Value": fmt.Sprintf("%.1f", temperature)}))
	powerLabel := widget.NewLabel(app.tr("harmony_os_gui_metric_power_usage_fmt", map[string]any{"Value": fmt.Sprintf("%.1f", powerUsage)}))

	metricsCard := widget.NewCard(
		app.tr("harmony_os_gui_card_monitoring_title", nil),
		app.tr("harmony_os_gui_card_monitoring_subtitle", nil),
		container.NewVBox(
			cpuLabel,
			memLabel,
			gpuLabel,
			tempLabel,
			powerLabel,
		),
	)

	// Harmony capabilities
	capabilitiesCard := widget.NewCard(
		app.tr("harmony_os_gui_card_capabilities_title", nil),
		app.tr("harmony_os_gui_card_capabilities_subtitle", nil),
		widget.NewLabel(fmt.Sprintf("• %s",
			fmt.Sprintf("%v", app.harmonyIntegration.systemAPI.capabilities)),
		),
	)

	return container.NewVBox(
		metricsLabel,
		metricsCard,
		capabilitiesCard,
	)
}

// createDistributedServicesTab creates the distributed services tab
func (app *HarmonyApp) createDistributedServicesTab() fyne.CanvasObject {
	// Connected devices
	devicesLabel := widget.NewLabel(app.tr("harmony_os_gui_connected_devices_fmt", map[string]any{
		"Count": len(app.harmonyIntegration.distributedEngine.connectedDevices),
	}))

	// Task scheduler info
	schedulerCard := widget.NewCard(
		app.tr("harmony_os_gui_card_scheduler_title", nil),
		app.tr("harmony_os_gui_card_scheduler_subtitle", nil),
		widget.NewLabel(app.tr("harmony_os_gui_scheduler_info_fmt", map[string]any{
			"Policy":    app.harmonyIntegration.distributedEngine.taskScheduler.schedulingPolicy,
			"QueueSize": len(app.harmonyIntegration.distributedEngine.taskScheduler.taskQueue),
		})),
	)

	// Data sync info. Reads sync status through GetSyncStatus so the
	// lastSyncErr sentinel surface from round-31 §11.4 is shown to the
	// user instead of the previous "Last Sync: Just now" PASS-bluff.
	enabled, lastSync, syncedCount, lastSyncErr := app.harmonyIntegration.distributedEngine.dataSync.GetSyncStatus()
	syncStatusText := app.tr("harmony_os_gui_sync_status_fmt", map[string]any{
		"Enabled":  enabled,
		"Interval": app.harmonyIntegration.distributedEngine.dataSync.syncInterval,
		"LastSync": lastSync.Format(time.RFC3339),
		"Synced":   syncedCount,
	})
	if lastSyncErr != nil {
		syncStatusText += app.tr("harmony_os_gui_sync_failed_fmt", map[string]any{"Error": fmt.Sprintf("%v", lastSyncErr)})
	}
	syncCard := widget.NewCard(
		app.tr("harmony_os_gui_card_sync_title", nil),
		app.tr("harmony_os_gui_card_sync_subtitle", nil),
		widget.NewLabel(syncStatusText),
	)

	return container.NewVBox(
		devicesLabel,
		schedulerCard,
		syncCard,
	)
}

// createResourceManagementTab creates the resource management tab
func (app *HarmonyApp) createResourceManagementTab() fyne.CanvasObject {
	// Resource policies
	policiesLabel := widget.NewLabel(app.tr("harmony_os_gui_label_resource_policies", nil))

	policiesCard := widget.NewCard(
		app.tr("harmony_os_gui_card_active_policies_title", nil),
		app.tr("harmony_os_gui_card_active_policies_subtitle", nil),
		widget.NewLabel(app.tr("harmony_os_gui_resource_policies_fmt", map[string]any{
			"CPUPolicy":    app.resourceManager.resourcePolicies["cpu"],
			"MemoryPolicy": app.resourceManager.resourcePolicies["memory"],
			"PowerPolicy":  app.resourceManager.resourcePolicies["power"],
			"Optimization": app.resourceManager.optimization,
			"AutoTuning":   app.resourceManager.autoTuning,
		})),
	)

	// Service coordinator
	servicesCard := widget.NewCard(
		app.tr("harmony_os_gui_card_service_coordinator_title", nil),
		app.tr("harmony_os_gui_card_service_coordinator_subtitle", nil),
		widget.NewLabel(app.tr("harmony_os_gui_service_coordinator_fmt", map[string]any{
			"ActiveServices": len(app.serviceCoordinator.coordinator.activeServices),
			"Failover":       app.serviceCoordinator.coordinator.failoverEnabled,
		})),
	)

	return container.NewVBox(
		policiesLabel,
		policiesCard,
		servicesCard,
	)
}

// createSettingsTab creates the settings tab
func (app *HarmonyApp) createSettingsTab() fyne.CanvasObject {
	// Theme selector
	themeLabel := widget.NewLabel(app.tr("harmony_os_gui_label_theme_selection", nil))
	themeSelect := widget.NewSelect(
		[]string{"Dark", "Light", "Helix", "Harmony"},
		func(selected string) {
			app.themeManager.SetTheme(selected)
			app.fyneApp.Settings().SetTheme(app.themeManager.GetCustomTheme())
			app.statusBar.SetText(app.tr("harmony_os_gui_status_theme_changed_fmt", map[string]any{"Theme": selected}))
		},
	)
	themeSelect.SetSelected("Harmony")

	// Server controls
	serverLabel := widget.NewLabel(app.tr("harmony_os_gui_label_server_controls", nil))
	startServerBtn := widget.NewButton(app.tr("harmony_os_gui_button_start_server", nil), func() {
		// THREAD-AFFINITY FIX (§11.4.115): app.server.Start() blocks, so this
		// error path runs on the worker goroutine long after the click that
		// spawned it — the bare statusBar.SetText raced the renderer. Every
		// OTHER statusBar.SetText in this file (including the two flanking this
		// button's handler) sits directly in a widget callback, i.e. already on
		// the UI goroutine, and is correctly left bare; this was the only one
		// inside a goroutine.
		go func() {
			if err := app.server.Start(); err != nil {
				log.Printf("Server error: %v", err)
				serverErr := app.tr("harmony_os_gui_status_server_error_fmt", map[string]any{"Error": fmt.Sprintf("%v", err)})
				fyneui.DoAndWait(func() { app.statusBar.SetText(serverErr) })
			}
		}()
		app.statusBar.SetText(app.tr("harmony_os_gui_status_server_started", nil))
	})

	stopServerBtn := widget.NewButton(app.tr("harmony_os_gui_button_stop_server", nil), func() {
		if err := app.server.Shutdown(context.Background()); err != nil {
			log.Printf("Server shutdown error: %v", err)
		}
		app.statusBar.SetText(app.tr("harmony_os_gui_status_server_stopped", nil))
	})

	serverControls := container.NewHBox(startServerBtn, stopServerBtn)

	return container.NewVBox(
		themeLabel,
		themeSelect,
		layout.NewSpacer(),
		serverLabel,
		serverControls,
		layout.NewSpacer(),
	)
}

// Run starts the Harmony OS application
func (app *HarmonyApp) Run() {
	// Setup signal handling for graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// Start signal handler in goroutine
	go func() {
		<-sigChan
		log.Println("Received shutdown signal")
		app.fyneApp.Quit()
	}()

	// Show window and run (blocks until window closes)
	app.mainWindow.ShowAndRun()
}

// closeJoinTimeout bounds how long Cleanup() waits for the background
// data-update loop to observe stopUpdate and return before proceeding with
// teardown regardless. See DesktopApp's identical constant for rationale.
const closeJoinTimeout = 5 * time.Second

// Cleanup performs cleanup on application shutdown. It is idempotent: the
// stopUpdate channel is closed at most once via stopOnce, so a second Cleanup
// call is a clean no-op rather than a "close of closed channel" panic.
//
// TEARDOWN-RACE FIX (§11.4.115 / §11.4.108, mirrors applications/desktop's
// identical fix): closing stopUpdate only signals the background loop to
// stop -- it does NOT wait for the loop to have actually observed the
// signal and returned. Without a join, this function could proceed to
// close app.db while the loop goroutine was still mid-refreshData() (or
// about to start one after the priority-select race described on
// updateLoopTick), racing a live DB handle against its own teardown.
// HONESTY (§11.4.6): the bounded wait below guarantees the loop goroutine
// has RETURNED before teardown begins; it does not make updateLoopTick's
// own select-priority race provably zero-width (see that method's doc
// comment).
func (app *HarmonyApp) Cleanup() {
	// Stop background updates
	if app.stopUpdate != nil {
		app.stopOnce.Do(func() {
			close(app.stopUpdate)
		})
	}

	// JOIN: wait for the update loop goroutine to have fully returned
	// before tearing down app.db below. updateDone is nil when
	// startDataUpdates was never called, so this is a no-op in that case
	// rather than a hang on a channel nothing will ever close.
	if app.updateDone != nil {
		select {
		case <-app.updateDone:
		case <-time.After(closeJoinTimeout):
			log.Printf("Cleanup: timed out after %s waiting for background update loop to stop; proceeding with teardown anyway", closeJoinTimeout)
		}
	}

	// JOIN: wait for the system-monitor goroutine to have fully returned.
	// It observes the same stopUpdate close above. monitorDone is nil when
	// initializeHarmonyComponents never ran (the minimal-construction
	// pattern used by main_doubleclose_test.go / main_racefix_test.go), so
	// this is a no-op there rather than a hang.
	if app.monitorDone != nil {
		select {
		case <-app.monitorDone:
		case <-time.After(closeJoinTimeout):
			log.Printf("Cleanup: timed out after %s waiting for the system monitor to stop; proceeding with teardown anyway", closeJoinTimeout)
		}
	}

	// Stop system monitoring.
	//
	// DATA-RACE FIX (§11.4.115): this was a bare `monitoring = false` store
	// racing monitorSystem's unsynchronised loop-condition read — the defect
	// the race detector reported. monitorSystem no longer reads the flag at
	// all (it stops on stopUpdate), and the flag itself is now mutex-guarded,
	// so the status stays truthful for TestCleanup without racing anyone.
	app.systemMonitor.setMonitoring(false)

	// Stop distributed engine
	if app.harmonyIntegration != nil && app.harmonyIntegration.distributedEngine != nil {
		app.harmonyIntegration.distributedEngine.Stop()
	}

	// Shutdown server
	if app.server != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := app.server.Shutdown(ctx); err != nil {
			log.Printf("Server shutdown error: %v", err)
		}
	}

	// Close database
	if app.db != nil {
		app.db.Close()
	}

	log.Println("Harmony OS application cleaned up successfully")
}

func main() {
	// Create application
	harmonyApp := NewHarmonyApp()

	// Wire the real CONST-046 translator (embedded active.en.yaml bundle)
	// onto the GUI app BEFORE any user-facing output, replacing the
	// NoopTranslator{} message-ID-echo default installed by NewHarmonyApp.
	// Without this, user-facing strings leak raw message keys — a §11.4 /
	// CONST-046 PASS-bluff (systemic HXC-097). On bundle load failure the
	// loud NoopTranslator{} echo is preserved (never a silent swallow).
	if tr, err := i18n.NewTranslator(); err != nil {
		log.Printf("⚠️  i18n: falling back to message-ID echo (bundle load failed): %v", err)
	} else {
		harmonyApp.SetTranslator(tr)
	}

	// Initialize
	if err := harmonyApp.Initialize(); err != nil {
		log.Fatalf("Failed to initialize Harmony OS application: %v", err)
		os.Exit(1)
	}

	// Run application (SetupUI is already called in Initialize)
	log.Println("Starting HelixCode Harmony OS Edition...")
	harmonyApp.Run()

	// Cleanup on exit
	harmonyApp.Cleanup()
}
