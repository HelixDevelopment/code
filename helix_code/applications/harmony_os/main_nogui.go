//go:build nogui

package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"

	"dev.helix.code/applications/harmony_os/i18n"
	"dev.helix.code/internal/config"
	"dev.helix.code/internal/database"
	"dev.helix.code/internal/hardware"
	"dev.helix.code/internal/llm"
	"dev.helix.code/internal/project"
	"dev.helix.code/internal/redis"
	"dev.helix.code/internal/session"
	"dev.helix.code/internal/task"
	"dev.helix.code/internal/worker"
)

// CLITask is a simplified task representation for CLI
type CLITask struct {
	ID          string
	Type        string
	Description string
	Status      string
	Priority    string
}

// CLIWorker is a simplified worker representation for CLI
type CLIWorker struct {
	ID      string
	Host    string
	Port    string
	User    string
	Status  string
	Healthy bool
}

// CLITaskManager wraps task.TaskManager for CLI operations
type CLITaskManager struct {
	inner *task.TaskManager
	tasks []CLITask
	mu    sync.RWMutex
}

// NewCLITaskManager creates a new CLI task manager wrapper
func NewCLITaskManager(tm *task.TaskManager) *CLITaskManager {
	return &CLITaskManager{
		inner: tm,
		tasks: make([]CLITask, 0),
	}
}

// GetAllTasks returns all tasks
func (ctm *CLITaskManager) GetAllTasks() []CLITask {
	ctm.mu.RLock()
	defer ctm.mu.RUnlock()
	return ctm.tasks
}

// GetStats returns task statistics
func (ctm *CLITaskManager) GetStats() (total, completed, running int) {
	ctm.mu.RLock()
	defer ctm.mu.RUnlock()

	total = len(ctm.tasks)
	for _, t := range ctm.tasks {
		switch t.Status {
		case "completed":
			completed++
		case "running":
			running++
		}
	}
	return
}

// CreateTask creates a new task
func (ctm *CLITaskManager) CreateTask(ctx context.Context, taskType, description, priority string) (*CLITask, error) {
	ctm.mu.Lock()
	defer ctm.mu.Unlock()

	newTask := CLITask{
		ID:          fmt.Sprintf("task-%d", time.Now().UnixNano()),
		Type:        taskType,
		Description: description,
		Status:      "pending",
		Priority:    priority,
	}

	ctm.tasks = append(ctm.tasks, newTask)
	return &newTask, nil
}

// CancelTask cancels a task
func (ctm *CLITaskManager) CancelTask(ctx context.Context, taskID string) error {
	ctm.mu.Lock()
	defer ctm.mu.Unlock()

	for i, t := range ctm.tasks {
		if t.ID == taskID {
			ctm.tasks = append(ctm.tasks[:i], ctm.tasks[i+1:]...)
			return nil
		}
	}
	return fmt.Errorf("task not found: %s", taskID)
}

// CLIWorkerManager wraps worker.WorkerManager for CLI operations
type CLIWorkerManager struct {
	inner   *worker.WorkerManager
	workers []CLIWorker
	mu      sync.RWMutex
}

// NewCLIWorkerManager creates a new CLI worker manager wrapper
func NewCLIWorkerManager(wm *worker.WorkerManager) *CLIWorkerManager {
	return &CLIWorkerManager{
		inner:   wm,
		workers: make([]CLIWorker, 0),
	}
}

// GetWorkers returns all workers
func (cwm *CLIWorkerManager) GetWorkers() []CLIWorker {
	cwm.mu.RLock()
	defer cwm.mu.RUnlock()
	return cwm.workers
}

// AddWorker adds a new worker
func (cwm *CLIWorkerManager) AddWorker(w *CLIWorker) error {
	cwm.mu.Lock()
	defer cwm.mu.Unlock()

	cwm.workers = append(cwm.workers, *w)
	return nil
}

// RemoveWorker removes a worker
func (cwm *CLIWorkerManager) RemoveWorker(workerID string) error {
	cwm.mu.Lock()
	defer cwm.mu.Unlock()

	for i, w := range cwm.workers {
		if w.ID == workerID {
			cwm.workers = append(cwm.workers[:i], cwm.workers[i+1:]...)
			return nil
		}
	}
	return fmt.Errorf("worker not found: %s", workerID)
}

// HarmonyCLIApp represents the CLI application (nogui mode) for Harmony OS
type HarmonyCLIApp struct {
	config           *config.Config
	db               *database.Database
	taskManager      *CLITaskManager
	workerManager    *CLIWorkerManager
	projectManager   *project.Manager
	sessionManager   *session.Manager
	llmManager       *llm.ModelManager
	hardwareDetector *hardware.HardwareDetector

	// translator resolves CONST-046 user-facing message IDs. Defaults
	// to i18n.NoopTranslator{} (loud message-ID echo) when nil — set
	// by helix_code at boot via SetTranslator to a real
	// *i18nadapter.Translator wired to the active.en.yaml bundle.
	translator i18n.Translator
}

// NewHarmonyCLIApp creates a new CLI application. The CLI starts with
// i18n.NoopTranslator{} for backward compat — production wiring calls
// SetTranslator with a real Translator (e.g. helix_code's
// *i18nadapter.Translator) before Run.
func NewHarmonyCLIApp() *HarmonyCLIApp {
	return &HarmonyCLIApp{
		translator: i18n.NoopTranslator{},
	}
}

// SetTranslator wires a CONST-046-compliant Translator. Passing nil
// resets to i18n.NoopTranslator{} (loud echo) — never silently
// disables translation lookup (which would be a §11.4 PASS-bluff at
// the i18n injection layer).
func (cliApp *HarmonyCLIApp) SetTranslator(tr i18n.Translator) {
	if tr == nil {
		cliApp.translator = i18n.NoopTranslator{}
		return
	}
	cliApp.translator = tr
}

// tr is the internal CONST-046 resolver used by every user-facing
// string emission in this file. It NEVER returns an error to the
// caller — translation failures degrade to the message ID itself
// (matching NoopTranslator behaviour) so production output remains
// loud + obvious instead of silently empty.
func (cliApp *HarmonyCLIApp) tr(ctx context.Context, msgID string, data map[string]any) string {
	if cliApp.translator == nil {
		cliApp.translator = i18n.NoopTranslator{}
	}
	out, err := cliApp.translator.T(ctx, msgID, data)
	if err != nil || out == "" {
		return msgID
	}
	return out
}

// Initialize sets up the CLI application
func (cliApp *HarmonyCLIApp) Initialize() error {
	// Load configuration
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("failed to load configuration: %v", err)
	}
	cliApp.config = cfg

	// Initialize database (optional)
	db, err := database.New(cfg.Database)
	if err != nil {
		log.Printf("Warning: Database not available: %v (continuing without persistence)", err)
	}
	cliApp.db = db

	// Initialize Redis (optional)
	rds, err := redis.NewClient(&cfg.Redis)
	if err != nil {
		log.Printf("Warning: Redis not available: %v (continuing without caching)", err)
	}

	// Initialize components
	innerTaskManager := task.NewTaskManager(db, rds)
	cliApp.taskManager = NewCLITaskManager(innerTaskManager)

	workerRepo := worker.NewInMemoryWorkerRepository()
	innerWorkerManager := worker.NewWorkerManager(workerRepo, 30*time.Second)
	cliApp.workerManager = NewCLIWorkerManager(innerWorkerManager)

	cliApp.projectManager = project.NewManager()
	cliApp.sessionManager = session.NewManager()

	// W2c-1 cloud gate (operator mandate 2026-09-05, local-only adaptive
	// serving): wire the hosted-provider gate from configuration BEFORE any
	// handler can construct a provider. Mirrors internal/server/server.go.
	// Default false -- llm.cloud.enabled must be explicitly set true to
	// permit cloud construction.
	llm.SetCloudEnabled(cfg.LLM.Cloud.Enabled)
	cliApp.llmManager = llm.NewModelManager()
	cliApp.hardwareDetector = hardware.NewHardwareDetector()

	return nil
}

// Close cleans up resources
func (cliApp *HarmonyCLIApp) Close() error {
	if cliApp.db != nil {
		cliApp.db.Close()
	}
	return nil
}

// Run executes the CLI command
func (cliApp *HarmonyCLIApp) Run(args []string) error {
	if len(args) == 0 {
		cliApp.printHelp()
		return nil
	}

	command := args[0]
	cmdArgs := args[1:]

	switch command {
	case "help", "-h", "--help":
		cliApp.printHelp()
	case "status":
		return cliApp.cmdStatus()
	case "system":
		return cliApp.cmdSystem()
	case "projects":
		return cliApp.cmdProjects(cmdArgs)
	case "sessions":
		return cliApp.cmdSessions(cmdArgs)
	case "tasks":
		return cliApp.cmdTasks(cmdArgs)
	case "workers":
		return cliApp.cmdWorkers(cmdArgs)
	case "llm":
		return cliApp.cmdLLM(cmdArgs)
	case "distributed":
		return cliApp.cmdDistributed(cmdArgs)
	case "interactive":
		return cliApp.cmdInteractive()
	default:
		// CONST-046 (round-330 §11.4): unknown-command error sourced
		// from applications/harmony_os/i18n bundle via injected
		// Translator; NoopTranslator echoes the message ID.
		fmt.Println(cliApp.tr(context.Background(), "harmony_os_cli_unknown_command", map[string]any{"Command": command}))
		cliApp.printHelp()
		return fmt.Errorf("unknown command: %s", command)
	}

	return nil
}

func (cliApp *HarmonyCLIApp) printHelp() {
	// CONST-046 (round-330 §11.4): help body sourced from
	// applications/harmony_os/i18n bundle via injected Translator.
	fmt.Println(cliApp.tr(context.Background(), "harmony_os_cli_help_body", nil))
}

func (cliApp *HarmonyCLIApp) cmdStatus() error {
	// CONST-046 (round-96 §11.4): section header sourced from
	// applications/harmony_os/i18n bundle via injected Translator;
	// NoopTranslator echoes the message ID.
	fmt.Println(cliApp.tr(context.Background(), "harmony_os_cli_status_header", nil))
	fmt.Println()

	// System info — CONST-046 (round-330 §11.4): info lines sourced
	// from i18n bundle with named placeholders via injected Translator.
	ctxStatus := context.Background()
	profile := cliApp.hardwareDetector.GetProfile()
	fmt.Println(cliApp.tr(ctxStatus, "harmony_os_cli_status_platform", map[string]any{"OSName": profile.OS.Name, "OSArch": profile.OS.Arch}))
	fmt.Println(cliApp.tr(ctxStatus, "harmony_os_cli_status_cpu_cores", map[string]any{"Cores": profile.CPU.Cores}))
	fmt.Println(cliApp.tr(ctxStatus, "harmony_os_cli_status_go_version", map[string]any{"Version": runtime.Version()}))
	fmt.Println()

	// Workers
	workers := cliApp.workerManager.GetWorkers()
	activeWorkers := 0
	for _, w := range workers {
		if w.Status == "active" {
			activeWorkers++
		}
	}
	fmt.Println(cliApp.tr(ctxStatus, "harmony_os_cli_status_workers", map[string]any{"Total": len(workers), "Active": activeWorkers}))

	// Tasks
	totalTasks, completedTasks, runningTasks := cliApp.taskManager.GetStats()
	fmt.Println(cliApp.tr(ctxStatus, "harmony_os_cli_status_tasks", map[string]any{"Total": totalTasks, "Running": runningTasks, "Completed": completedTasks}))

	// Projects
	ctx := ctxStatus
	projects, _ := cliApp.projectManager.ListProjects(ctx, "")
	activeProject, _ := cliApp.projectManager.GetActiveProject(ctx)
	activeProjectName := "none"
	if activeProject != nil {
		activeProjectName = activeProject.Name
	}
	fmt.Println(cliApp.tr(ctxStatus, "harmony_os_cli_status_projects", map[string]any{"Total": len(projects), "Active": activeProjectName}))

	// Sessions
	sessions := cliApp.sessionManager.GetAll()
	activeSessions := 0
	for _, s := range sessions {
		if s.Status == session.StatusActive {
			activeSessions++
		}
	}
	fmt.Println(cliApp.tr(ctxStatus, "harmony_os_cli_status_sessions", map[string]any{"Total": len(sessions), "Active": activeSessions}))

	// LLM
	models := cliApp.llmManager.GetAvailableModels()
	fmt.Println(cliApp.tr(ctxStatus, "harmony_os_cli_status_llm_models", map[string]any{"Count": len(models)}))

	return nil
}

func (cliApp *HarmonyCLIApp) cmdSystem() error {
	// CONST-046 (round-96 §11.4): section header sourced from
	// applications/harmony_os/i18n bundle via injected Translator.
	fmt.Println(cliApp.tr(context.Background(), "harmony_os_cli_system_header", nil))
	fmt.Println()

	profile := cliApp.hardwareDetector.GetProfile()

	// CONST-046 (round-369 §11.4): system-report labels sourced from
	// applications/harmony_os/i18n bundle via injected Translator.
	ctx := context.Background()
	fmt.Println(cliApp.tr(ctx, "harmony_os_cli_system_hw_profile", nil))
	fmt.Println(cliApp.tr(ctx, "harmony_os_cli_system_cpu_arch", map[string]any{"Arch": profile.CPU.Arch}))
	fmt.Println(cliApp.tr(ctx, "harmony_os_cli_system_cpu_cores", map[string]any{"Cores": profile.CPU.Cores}))
	fmt.Println(cliApp.tr(ctx, "harmony_os_cli_system_cpu_threads", map[string]any{"Threads": profile.CPU.Threads}))
	fmt.Println(cliApp.tr(ctx, "harmony_os_cli_system_memory_total", map[string]any{"Total": profile.Memory.Total}))
	fmt.Println()

	fmt.Println(cliApp.tr(ctx, "harmony_os_cli_system_os_info", nil))
	fmt.Println(cliApp.tr(ctx, "harmony_os_cli_system_os_platform", nil))
	fmt.Println(cliApp.tr(ctx, "harmony_os_cli_system_os_version", nil))
	fmt.Println(cliApp.tr(ctx, "harmony_os_cli_system_os_kernel", nil))
	fmt.Println(cliApp.tr(ctx, "harmony_os_cli_system_go_runtime", map[string]any{"OS": runtime.GOOS, "Arch": runtime.GOARCH}))
	fmt.Println()

	fmt.Println(cliApp.tr(ctx, "harmony_os_cli_system_capabilities", nil))
	fmt.Println(cliApp.tr(ctx, "harmony_os_cli_system_cap_distributed", nil))
	fmt.Println(cliApp.tr(ctx, "harmony_os_cli_system_cap_cross_device_sync", nil))
	fmt.Println(cliApp.tr(ctx, "harmony_os_cli_system_cap_ai_acceleration", nil))
	fmt.Println(cliApp.tr(ctx, "harmony_os_cli_system_cap_multi_screen", nil))
	fmt.Println(cliApp.tr(ctx, "harmony_os_cli_system_cap_super_device", nil))
	fmt.Println()

	var memStats runtime.MemStats
	runtime.ReadMemStats(&memStats)
	fmt.Println(cliApp.tr(ctx, "harmony_os_cli_system_runtime_stats", nil))
	fmt.Println(cliApp.tr(ctx, "harmony_os_cli_system_goroutines", map[string]any{"Count": runtime.NumGoroutine()}))
	fmt.Println(cliApp.tr(ctx, "harmony_os_cli_system_mem_allocated", map[string]any{"MB": float64(memStats.Alloc) / (1024 * 1024)}))
	fmt.Println(cliApp.tr(ctx, "harmony_os_cli_system_total_allocated", map[string]any{"MB": float64(memStats.TotalAlloc) / (1024 * 1024)}))
	fmt.Println(cliApp.tr(ctx, "harmony_os_cli_system_gc_cycles", map[string]any{"Count": memStats.NumGC}))

	return nil
}

func (cliApp *HarmonyCLIApp) cmdProjects(args []string) error {
	if len(args) == 0 {
		args = []string{"list"}
	}

	ctx := context.Background()

	switch args[0] {
	case "list":
		projects, err := cliApp.projectManager.ListProjects(ctx, "")
		if err != nil {
			return err
		}
		// CONST-046 (round-96 §11.4): section header sourced from
		// applications/harmony_os/i18n bundle via injected Translator.
		fmt.Println(cliApp.tr(ctx, "harmony_os_cli_projects_header", nil))
		if len(projects) == 0 {
			// CONST-046 (round-330 §11.4): empty-list notice via Translator.
			fmt.Println(cliApp.tr(ctx, "harmony_os_cli_no_projects", nil))
			return nil
		}
		for _, p := range projects {
			activeMarker := ""
			if p.Active {
				activeMarker = " [ACTIVE]"
			}
			fmt.Printf("- %s (%s): %s%s\n", p.Name, p.Type, p.Path, activeMarker)
		}

	case "create":
		fs := flag.NewFlagSet("projects create", flag.ExitOnError)
		name := fs.String("name", "", "Project name")
		path := fs.String("path", "", "Project path")
		desc := fs.String("desc", "", cliApp.tr(context.Background(), "harmony_os_cli_flag_project_desc", nil))
		ptype := fs.String("type", "generic", cliApp.tr(context.Background(), "harmony_os_cli_flag_project_type", nil))
		fs.Parse(args[1:])

		if *name == "" || *path == "" {
			fmt.Println(cliApp.tr(ctx, "harmony_os_cli_err_name_path_required", nil))
			return fmt.Errorf("missing required arguments")
		}

		proj, err := cliApp.projectManager.CreateProject(ctx, *name, *desc, *path, *ptype)
		if err != nil {
			return err
		}
		fmt.Println(cliApp.tr(ctx, "harmony_os_cli_project_created", map[string]any{"Name": proj.Name, "ID": proj.ID}))

	case "set-active":
		if len(args) < 2 {
			fmt.Println(cliApp.tr(ctx, "harmony_os_cli_err_project_id_required", nil))
			return fmt.Errorf("missing project ID")
		}
		err := cliApp.projectManager.SetActiveProject(ctx, args[1])
		if err != nil {
			return err
		}
		fmt.Println(cliApp.tr(ctx, "harmony_os_cli_project_set_active", map[string]any{"ID": args[1]}))

	case "delete":
		if len(args) < 2 {
			fmt.Println(cliApp.tr(ctx, "harmony_os_cli_err_project_id_required", nil))
			return fmt.Errorf("missing project ID")
		}
		err := cliApp.projectManager.DeleteProject(ctx, args[1])
		if err != nil {
			return err
		}
		fmt.Println(cliApp.tr(ctx, "harmony_os_cli_project_deleted", map[string]any{"ID": args[1]}))

	default:
		// CONST-046 (round-330 §11.4): unknown-subcommand notice via Translator.
		fmt.Println(cliApp.tr(ctx, "harmony_os_cli_unknown_subcommand", map[string]any{"Subcommand": args[0]}))
	}

	return nil
}

func (cliApp *HarmonyCLIApp) cmdSessions(args []string) error {
	if len(args) == 0 {
		args = []string{"list"}
	}

	switch args[0] {
	case "list":
		sessions := cliApp.sessionManager.GetAll()
		// CONST-046 (round-96 §11.4): section header sourced from
		// applications/harmony_os/i18n bundle via injected Translator.
		fmt.Println(cliApp.tr(context.Background(), "harmony_os_cli_sessions_header", nil))
		if len(sessions) == 0 {
			// CONST-046 (round-330 §11.4): empty-list notice via Translator.
			fmt.Println(cliApp.tr(context.Background(), "harmony_os_cli_no_sessions", nil))
			return nil
		}
		for _, s := range sessions {
			fmt.Printf("- %s [%s] %s (Project: %s)\n", s.Name, s.Status, s.Mode, s.ProjectID)
		}

	case "create":
		fs := flag.NewFlagSet("sessions create", flag.ExitOnError)
		name := fs.String("name", "", "Session name")
		projectID := fs.String("project", "", "Project ID")
		desc := fs.String("desc", "", cliApp.tr(context.Background(), "harmony_os_cli_flag_session_desc", nil))
		mode := fs.String("mode", "building", cliApp.tr(context.Background(), "harmony_os_cli_flag_session_mode", nil))
		fs.Parse(args[1:])

		if *name == "" || *projectID == "" {
			fmt.Println(cliApp.tr(context.Background(), "harmony_os_cli_err_name_project_required", nil))
			return fmt.Errorf("missing required arguments")
		}

		sess, err := cliApp.sessionManager.Create(*projectID, *name, *desc, session.Mode(*mode))
		if err != nil {
			return err
		}
		fmt.Println(cliApp.tr(context.Background(), "harmony_os_cli_session_created", map[string]any{"Name": sess.Name, "ID": sess.ID}))

	case "start":
		if len(args) < 2 {
			fmt.Println(cliApp.tr(context.Background(), "harmony_os_cli_err_session_id_required", nil))
			return fmt.Errorf("missing session ID")
		}
		err := cliApp.sessionManager.Start(args[1])
		if err != nil {
			return err
		}
		fmt.Println(cliApp.tr(context.Background(), "harmony_os_cli_session_started", map[string]any{"ID": args[1]}))

	case "pause":
		if len(args) < 2 {
			fmt.Println(cliApp.tr(context.Background(), "harmony_os_cli_err_session_id_required", nil))
			return fmt.Errorf("missing session ID")
		}
		err := cliApp.sessionManager.Pause(args[1])
		if err != nil {
			return err
		}
		fmt.Println(cliApp.tr(context.Background(), "harmony_os_cli_session_paused", map[string]any{"ID": args[1]}))

	case "complete":
		if len(args) < 2 {
			fmt.Println(cliApp.tr(context.Background(), "harmony_os_cli_err_session_id_required", nil))
			return fmt.Errorf("missing session ID")
		}
		err := cliApp.sessionManager.Complete(args[1])
		if err != nil {
			return err
		}
		fmt.Println(cliApp.tr(context.Background(), "harmony_os_cli_session_completed", map[string]any{"ID": args[1]}))

	default:
		fmt.Println(cliApp.tr(context.Background(), "harmony_os_cli_unknown_subcommand", map[string]any{"Subcommand": args[0]}))
	}

	return nil
}

func (cliApp *HarmonyCLIApp) cmdTasks(args []string) error {
	if len(args) == 0 {
		args = []string{"list"}
	}

	ctx := context.Background()

	switch args[0] {
	case "list":
		tasks := cliApp.taskManager.GetAllTasks()
		// CONST-046 (round-96 §11.4): section header sourced from
		// applications/harmony_os/i18n bundle via injected Translator.
		fmt.Println(cliApp.tr(ctx, "harmony_os_cli_tasks_header", nil))
		if len(tasks) == 0 {
			// CONST-046 (round-330 §11.4): empty-list notice via Translator.
			fmt.Println(cliApp.tr(ctx, "harmony_os_cli_no_tasks", nil))
			return nil
		}
		for _, t := range tasks {
			fmt.Printf("- [%s] %s: %s (Priority: %s)\n", t.Status, t.Type, t.Description, t.Priority)
		}

	case "create":
		fs := flag.NewFlagSet("tasks create", flag.ExitOnError)
		taskType := fs.String("type", "building", cliApp.tr(context.Background(), "harmony_os_cli_flag_task_type", nil))
		desc := fs.String("desc", "", cliApp.tr(context.Background(), "harmony_os_cli_flag_task_desc", nil))
		priority := fs.String("priority", "normal", cliApp.tr(context.Background(), "harmony_os_cli_flag_task_priority", nil))
		fs.Parse(args[1:])

		if *desc == "" {
			fmt.Println(cliApp.tr(ctx, "harmony_os_cli_err_desc_required", nil))
			return fmt.Errorf("missing required arguments")
		}

		t, err := cliApp.taskManager.CreateTask(ctx, *taskType, *desc, *priority)
		if err != nil {
			return err
		}
		fmt.Println(cliApp.tr(ctx, "harmony_os_cli_task_created", map[string]any{"Description": t.Description, "ID": t.ID}))

	case "cancel":
		if len(args) < 2 {
			fmt.Println(cliApp.tr(ctx, "harmony_os_cli_err_task_id_required", nil))
			return fmt.Errorf("missing task ID")
		}
		err := cliApp.taskManager.CancelTask(ctx, args[1])
		if err != nil {
			return err
		}
		fmt.Println(cliApp.tr(ctx, "harmony_os_cli_task_cancelled", map[string]any{"ID": args[1]}))

	default:
		fmt.Println(cliApp.tr(ctx, "harmony_os_cli_unknown_subcommand", map[string]any{"Subcommand": args[0]}))
	}

	return nil
}

func (cliApp *HarmonyCLIApp) cmdWorkers(args []string) error {
	if len(args) == 0 {
		args = []string{"list"}
	}

	switch args[0] {
	case "list":
		workers := cliApp.workerManager.GetWorkers()
		// CONST-046 (round-330 §11.4): section header + empty notice via Translator.
		fmt.Println(cliApp.tr(context.Background(), "harmony_os_cli_workers_header", nil))
		if len(workers) == 0 {
			fmt.Println(cliApp.tr(context.Background(), "harmony_os_cli_no_workers", nil))
			return nil
		}
		for _, w := range workers {
			healthStatus := "unhealthy"
			if w.Healthy {
				healthStatus = "healthy"
			}
			fmt.Printf("- %s [%s] %s:%s (%s)\n", w.ID, w.Status, w.Host, w.Port, healthStatus)
		}

	case "add":
		fs := flag.NewFlagSet("workers add", flag.ExitOnError)
		host := fs.String("host", "", "Worker host")
		port := fs.String("port", "22", "Worker port")
		user := fs.String("user", "", "SSH user")
		fs.Parse(args[1:])

		if *host == "" {
			fmt.Println(cliApp.tr(context.Background(), "harmony_os_cli_err_host_required", nil))
			return fmt.Errorf("missing required arguments")
		}

		w := &CLIWorker{
			ID:      fmt.Sprintf("worker-%s-%d", *host, time.Now().UnixNano()),
			Host:    *host,
			Port:    *port,
			User:    *user,
			Status:  "pending",
			Healthy: false,
		}
		err := cliApp.workerManager.AddWorker(w)
		if err != nil {
			return err
		}
		fmt.Println(cliApp.tr(context.Background(), "harmony_os_cli_worker_added_fmt", map[string]any{"ID": w.ID}))

	case "remove":
		if len(args) < 2 {
			fmt.Println(cliApp.tr(context.Background(), "harmony_os_cli_err_worker_id_required", nil))
			return fmt.Errorf("missing worker ID")
		}
		err := cliApp.workerManager.RemoveWorker(args[1])
		if err != nil {
			return err
		}
		fmt.Println(cliApp.tr(context.Background(), "harmony_os_cli_worker_removed_fmt", map[string]any{"ID": args[1]}))

	default:
		// CONST-046 (round-369 §11.4): unknown-subcommand notice via Translator.
		fmt.Println(cliApp.tr(context.Background(), "harmony_os_cli_unknown_subcommand", map[string]any{"Subcommand": args[0]}))
	}

	return nil
}

func (cliApp *HarmonyCLIApp) cmdLLM(args []string) error {
	if len(args) == 0 {
		args = []string{"providers"}
	}

	// CONST-046 (round-369 §11.4): LLM subcommand output sourced from
	// applications/harmony_os/i18n bundle via injected Translator.
	ctx := context.Background()

	switch args[0] {
	case "chat", "generate":
		// Round-XXX §11.4 / BLUFF-001 fix: the previous `chat` case was
		// a placeholder — it printed two i18n hints
		// (harmony_os_cli_llm_chat_needs_provider +
		// harmony_os_cli_llm_chat_configure_hint) and never performed a
		// real generation, telling the user to "use the GUI version".
		// It now drives the REAL llm.Provider pipeline via
		// HarmonyLLMCore.Generate (defined in distributed.go, no build
		// tag) — the SAME canonical path cmd/cli + the mobile bindings
		// use. The prompt comes from the remaining args; the provider's
		// genuine output is printed; a real transport/provider error is
		// surfaced verbatim (never swallowed into a fake success).
		prompt := strings.TrimSpace(strings.Join(args[1:], " "))
		if prompt == "" {
			// CONST-046: usage notice sourced from the harmony_os i18n
			// bundle via injected Translator; NoopTranslator echoes the ID.
			fmt.Println(cliApp.tr(ctx, "harmony_os_cli_llm_chat_usage", nil))
			return fmt.Errorf("llm chat: prompt required")
		}
		core := NewHarmonyLLMCore()
		// CONST-046: "generating" status line via Translator.
		fmt.Println(cliApp.tr(ctx, "harmony_os_cli_llm_chat_generating", nil))
		out, err := core.Generate(prompt)
		if err != nil {
			// Surface the real provider/transport error verbatim (the
			// underlying error already carries the honest "no LLM
			// provider available" / "provider call failed" context).
			return fmt.Errorf("llm chat: %w", err)
		}
		fmt.Println(out)

	case "providers":
		health := cliApp.llmManager.HealthCheck(ctx)
		fmt.Println(cliApp.tr(ctx, "harmony_os_cli_llm_providers_header", nil))
		if len(health) == 0 {
			fmt.Println(cliApp.tr(ctx, "harmony_os_cli_llm_no_providers", nil))
			return nil
		}
		for provider, status := range health {
			fmt.Printf("- %s: %s\n", provider, status.Status)
		}

	case "models":
		models := cliApp.llmManager.GetAvailableModels()
		fmt.Println(cliApp.tr(ctx, "harmony_os_cli_llm_models_header", nil))
		if len(models) == 0 {
			fmt.Println(cliApp.tr(ctx, "harmony_os_cli_llm_no_models", nil))
			return nil
		}
		for _, m := range models {
			fmt.Printf("- %s (%s) - Context: %d\n", m.Name, m.Provider, m.ContextSize)
		}

	default:
		fmt.Println(cliApp.tr(ctx, "harmony_os_cli_unknown_subcommand", map[string]any{"Subcommand": args[0]}))
	}

	return nil
}

func (cliApp *HarmonyCLIApp) cmdDistributed(args []string) error {
	if len(args) == 0 {
		args = []string{"status"}
	}

	switch args[0] {
	case "status":
		// Round-31 §11.4 fix: previous output advertised
		// "Data synchronization: Enabled" and "Device discovery:
		// Available" — both are PASS-bluffs in this build because
		// neither the Harmony OS distributed-data SDK nor the
		// device-manager SDK is wired. New output reports the real
		// implementation state honestly so the user (and any wrapper
		// script) knows the gaps before invoking `discover` or `sync`.
		fmt.Println("=== Harmony OS Distributed Status ===")
		fmt.Println()
		fmt.Println("Distributed Features:")
		fmt.Println("  - Cross-device task scheduling: stub (local-only scheduler; no remote dispatch)")
		fmt.Println("  - Data synchronization: NOT WIRED (Harmony distributed-data SDK absent — `sync` returns error)")
		fmt.Println("  - Device discovery: NOT WIRED (Harmony device-manager SDK absent — `discover` returns error)")
		fmt.Println()
		fmt.Println("Implementation gap tracked in applications/harmony_os/distributed.go via")
		fmt.Println("ErrHarmonyDistributedSyncNotImplemented and ErrHarmonyDiscoveryNotImplemented.")
		fmt.Println("Resolution path (round-67 §11.4): consumers with a real Harmony OS Go")
		fmt.Println("binding (cgo shim around OHOS::DistributedKv / JS bridge to ArkTS /")
		fmt.Println("OEM SDK) call (*HarmonyDistributedEngine).SetDistributedSDK(impl) at boot.")

	case "discover":
		// Round-31 §11.4 fix: previous output was a PASS-bluff —
		// it printed "Scanning..." then "No devices found (running
		// in standalone mode)" plus a self-admission that on
		// non-Harmony platforms the command would show fabricated
		// results, giving the user a fake "successful but empty"
		// outcome from a feature that never ran. New output mirrors
		// the ErrHarmonyDiscoveryNotImplemented sentinel surfaced by
		// (*HarmonyDistributedEngine).DiscoverDevices in the GUI
		// build: distributed discovery is NOT WIRED in this build,
		// report that loudly with a non-zero exit so scripts can
		// detect the gap.
		fmt.Fprintln(os.Stderr, "ERROR: Harmony OS distributed device discovery is not wired in this build.")
		fmt.Fprintln(os.Stderr, "The Harmony OS device-manager SDK is required and not present. This")
		fmt.Fprintln(os.Stderr, "command previously printed a fake 'No devices found' success message;")
		fmt.Fprintln(os.Stderr, "it now refuses to fabricate a result. See applications/harmony_os/distributed.go")
		fmt.Fprintln(os.Stderr, "(ErrHarmonyDiscoveryNotImplemented) for the full implementation gap.")
		fmt.Fprintln(os.Stderr, "Round-67 §11.4 resolution path: inject a HarmonyDistributedSDK via")
		fmt.Fprintln(os.Stderr, "(*HarmonyDistributedEngine).SetDistributedSDK(impl) at boot.")
		return fmt.Errorf("harmony distributed discover: not wired in this build")

	case "sync":
		// Round-31 §11.4 fix: previous output was a PASS-bluff —
		// it printed "Sync Status: Enabled / Last Sync: Just now /
		// Synced Devices: 0" without ever calling the underlying
		// sync engine, so callers got fake "successful" sync data
		// on every invocation. New output mirrors the
		// ErrHarmonyDistributedSyncNotImplemented sentinel surfaced
		// by (*HarmonyDataSync).performSync in the GUI build:
		// distributed sync is NOT WIRED in this build, report that
		// loudly with a non-zero exit so scripts can detect the gap.
		fmt.Fprintln(os.Stderr, "ERROR: Harmony OS distributed data synchronization is not wired in this build.")
		fmt.Fprintln(os.Stderr, "The Harmony OS distributed-data SDK (KVManager / SingleKVStore /")
		fmt.Fprintln(os.Stderr, "DeviceKVStore) is required and not present. This command previously")
		fmt.Fprintln(os.Stderr, "printed 'Last Sync: Just now / Synced Devices: 0' without exchanging")
		fmt.Fprintln(os.Stderr, "any state with any device; it now refuses to fabricate a result.")
		fmt.Fprintln(os.Stderr, "See applications/harmony_os/distributed.go (ErrHarmonyDistributedSyncNotImplemented)")
		fmt.Fprintln(os.Stderr, "for the full implementation gap.")
		fmt.Fprintln(os.Stderr, "Round-67 §11.4 resolution path: inject a HarmonyDistributedSDK via")
		fmt.Fprintln(os.Stderr, "(*HarmonyDataSync).SetDistributedSDK(impl) at boot.")
		return fmt.Errorf("harmony distributed sync: not wired in this build")

	default:
		// CONST-046 (round-369 §11.4): unknown-subcommand notices via Translator.
		fmt.Println(cliApp.tr(context.Background(), "harmony_os_cli_unknown_subcommand", map[string]any{"Subcommand": args[0]}))
		fmt.Println(cliApp.tr(context.Background(), "harmony_os_cli_distributed_subcommands", nil))
	}

	return nil
}

func (cliApp *HarmonyCLIApp) cmdInteractive() error {
	// CONST-046 (round-369 §11.4): interactive-mode banner via Translator.
	fmt.Println(cliApp.tr(context.Background(), "harmony_os_cli_interactive_header", nil))
	fmt.Println(cliApp.tr(context.Background(), "harmony_os_cli_interactive_hint", nil))
	fmt.Println()

	// Setup signal handling
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-sigChan
		fmt.Println("\nExiting...")
		os.Exit(0)
	}()

	var input string
	for {
		fmt.Print("helix-harmony> ")
		_, err := fmt.Scanln(&input)
		if err != nil {
			continue
		}

		input = strings.TrimSpace(input)
		if input == "" {
			continue
		}

		if input == "quit" || input == "exit" {
			fmt.Println(cliApp.tr(context.Background(), "harmony_os_cli_interactive_goodbye", nil))
			break
		}

		args := strings.Fields(input)
		if err := cliApp.Run(args); err != nil {
			// HXC-102: route through the translator (CONST-046); bind the Error
			// param so the {{.Error}} placeholder resolves (no '<no value>' per
			// the HXC-099 lesson).
			fmt.Println(cliApp.tr(context.Background(), "harmony_os_cli_interactive_error_fmt", map[string]any{"Error": err.Error()}))
		}
		fmt.Println()
	}

	return nil
}

func main() {
	app := NewHarmonyCLIApp()

	// Wire the real CONST-046 translator (embedded active.en.yaml bundle)
	// onto the app BEFORE any user-facing output, replacing the
	// NoopTranslator{} message-ID-echo default installed by NewHarmonyCLIApp.
	// Without this, every command (help/status/version/...) leaks raw message
	// keys (`harmony_os_cli_status_header`, ...) — a §11.4 / CONST-046
	// PASS-bluff (systemic HXC-097). On bundle load failure the loud
	// NoopTranslator{} echo is preserved (never a silent swallow). Mirrors
	// applications/desktop/main_nogui.go.
	if tr, err := i18n.NewTranslator(); err != nil {
		log.Printf("⚠️  i18n: falling back to message-ID echo (bundle load failed): %v", err)
	} else {
		app.SetTranslator(tr)
	}

	if err := app.Initialize(); err != nil {
		log.Fatalf("Failed to initialize: %v", err)
	}
	defer app.Close()

	args := os.Args[1:]
	if err := app.Run(args); err != nil {
		os.Exit(1)
	}
}
