//go:build nogui

package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"dev.helix.code/applications/desktop/i18n"
	"dev.helix.code/internal/config"
	"dev.helix.code/internal/database"
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

// CLIApp represents the CLI application (nogui mode)
type CLIApp struct {
	config         *config.Config
	db             *database.Database
	taskManager    *CLITaskManager
	workerManager  *CLIWorkerManager
	projectManager *project.Manager
	sessionManager *session.Manager
	llmManager     *llm.ModelManager

	// translator resolves user-facing strings per CONST-046
	// (round-365 §11.4 migration). Defaults to NoopTranslator
	// (loud echo of message IDs) until SetTranslator wires a real
	// *i18nadapter.Translator at boot. Never nil after NewCLIApp
	// returns.
	translator i18n.Translator
}

// NewCLIApp creates a new CLI application
func NewCLIApp() *CLIApp {
	return &CLIApp{
		translator: i18n.NoopTranslator{},
	}
}

// SetTranslator injects the runtime Translator (per CONST-046
// round-365). Passing nil is a no-op — the NoopTranslator default
// installed by NewCLIApp is preserved so the loud-echo safety net
// never disappears silently. helix_code wires
// *i18nadapter.Translator at boot.
func (cliApp *CLIApp) SetTranslator(t i18n.Translator) {
	if t == nil {
		return
	}
	cliApp.translator = t
}

// t is a tiny call-site helper that resolves a message ID through
// the injected Translator and falls back to the literal id on error
// (loud echo — never silently swallow). Centralising the
// boilerplate keeps migrated call sites a single expression long.
func (cliApp *CLIApp) t(id string) string {
	if cliApp.translator == nil {
		cliApp.translator = i18n.NoopTranslator{}
		return id
	}
	got, err := cliApp.translator.T(context.Background(), id, nil)
	if err != nil || got == "" {
		return id
	}
	return got
}

// Initialize sets up the CLI application
func (cliApp *CLIApp) Initialize() error {
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

	return nil
}

// Close cleans up resources
func (cliApp *CLIApp) Close() error {
	if cliApp.db != nil {
		cliApp.db.Close()
	}
	return nil
}

// Run executes the CLI command
func (cliApp *CLIApp) Run(args []string) error {
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
	case "specify":
		return cliApp.cmdSpecify(cmdArgs)
	case "interactive":
		return cliApp.cmdInteractive()
	default:
		fmt.Printf(cliApp.t("desktop_cli_unknown_command")+"\n", command)
		cliApp.printHelp()
		return fmt.Errorf("unknown command: %s", command)
	}

	return nil
}

func (cliApp *CLIApp) printHelp() {
	fmt.Println(cliApp.t("desktop_cli_help_body"))
}

func (cliApp *CLIApp) cmdStatus() error {
	fmt.Println(cliApp.t("desktop_cli_status_header"))
	fmt.Println()

	// Workers
	workers := cliApp.workerManager.GetWorkers()
	activeWorkers := 0
	for _, w := range workers {
		if w.Status == "active" {
			activeWorkers++
		}
	}
	fmt.Printf(cliApp.t("desktop_cli_status_workers")+"\n", len(workers), activeWorkers)

	// Tasks
	totalTasks, completedTasks, runningTasks := cliApp.taskManager.GetStats()
	fmt.Printf(cliApp.t("desktop_cli_status_tasks")+"\n",
		totalTasks, runningTasks, completedTasks)

	// Projects
	ctx := context.Background()
	projects, _ := cliApp.projectManager.ListProjects(ctx, "")
	activeProject, _ := cliApp.projectManager.GetActiveProject(ctx)
	activeProjectName := "none"
	if activeProject != nil {
		activeProjectName = activeProject.Name
	}
	fmt.Printf(cliApp.t("desktop_cli_status_projects")+"\n", len(projects), activeProjectName)

	// Sessions
	sessions := cliApp.sessionManager.GetAll()
	activeSessions := 0
	for _, s := range sessions {
		if s.Status == session.StatusActive {
			activeSessions++
		}
	}
	fmt.Printf(cliApp.t("desktop_cli_status_sessions")+"\n", len(sessions), activeSessions)

	// LLM
	models := cliApp.llmManager.GetAvailableModels()
	fmt.Printf(cliApp.t("desktop_cli_status_llm_models")+"\n", len(models))

	return nil
}

func (cliApp *CLIApp) cmdProjects(args []string) error {
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
		fmt.Println(cliApp.t("desktop_cli_projects_header"))
		if len(projects) == 0 {
			fmt.Println(cliApp.t("desktop_cli_no_projects"))
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
		desc := fs.String("desc", "", cliApp.t("desktop_cli_flag_project_desc"))
		ptype := fs.String("type", "generic", cliApp.t("desktop_cli_flag_project_type"))
		fs.Parse(args[1:])

		if *name == "" || *path == "" {
			fmt.Println(cliApp.t("desktop_cli_err_name_path_required"))
			return fmt.Errorf("missing required arguments")
		}

		project, err := cliApp.projectManager.CreateProject(ctx, *name, *desc, *path, *ptype)
		if err != nil {
			return err
		}
		fmt.Printf(cliApp.t("desktop_cli_created_project")+"\n", project.Name, project.ID)

	case "set-active":
		if len(args) < 2 {
			fmt.Println(cliApp.t("desktop_cli_err_project_id_required"))
			return fmt.Errorf("missing project ID")
		}
		err := cliApp.projectManager.SetActiveProject(ctx, args[1])
		if err != nil {
			return err
		}
		fmt.Printf(cliApp.t("desktop_cli_set_active_project")+"\n", args[1])

	case "delete":
		if len(args) < 2 {
			fmt.Println(cliApp.t("desktop_cli_err_project_id_required"))
			return fmt.Errorf("missing project ID")
		}
		err := cliApp.projectManager.DeleteProject(ctx, args[1])
		if err != nil {
			return err
		}
		fmt.Printf(cliApp.t("desktop_cli_deleted_project")+"\n", args[1])

	default:
		fmt.Printf(cliApp.t("desktop_cli_unknown_subcommand")+"\n", args[0])
	}

	return nil
}

func (cliApp *CLIApp) cmdSessions(args []string) error {
	if len(args) == 0 {
		args = []string{"list"}
	}

	switch args[0] {
	case "list":
		sessions := cliApp.sessionManager.GetAll()
		fmt.Println(cliApp.t("desktop_cli_sessions_header"))
		if len(sessions) == 0 {
			fmt.Println(cliApp.t("desktop_cli_no_sessions"))
			return nil
		}
		for _, s := range sessions {
			fmt.Printf("- %s [%s] %s (Project: %s)\n", s.Name, s.Status, s.Mode, s.ProjectID)
		}

	case "create":
		fs := flag.NewFlagSet("sessions create", flag.ExitOnError)
		name := fs.String("name", "", "Session name")
		projectID := fs.String("project", "", "Project ID")
		desc := fs.String("desc", "", cliApp.t("desktop_cli_flag_session_desc"))
		mode := fs.String("mode", "building", cliApp.t("desktop_cli_flag_session_mode"))
		fs.Parse(args[1:])

		if *name == "" || *projectID == "" {
			fmt.Println(cliApp.t("desktop_cli_err_name_project_required"))
			return fmt.Errorf("missing required arguments")
		}

		sess, err := cliApp.sessionManager.Create(*projectID, *name, *desc, session.Mode(*mode))
		if err != nil {
			return err
		}
		fmt.Printf(cliApp.t("desktop_cli_created_session")+"\n", sess.Name, sess.ID)

	case "start":
		if len(args) < 2 {
			fmt.Println(cliApp.t("desktop_cli_err_session_id_required"))
			return fmt.Errorf("missing session ID")
		}
		err := cliApp.sessionManager.Start(args[1])
		if err != nil {
			return err
		}
		fmt.Printf(cliApp.t("desktop_cli_started_session")+"\n", args[1])

	case "pause":
		if len(args) < 2 {
			fmt.Println(cliApp.t("desktop_cli_err_session_id_required"))
			return fmt.Errorf("missing session ID")
		}
		err := cliApp.sessionManager.Pause(args[1])
		if err != nil {
			return err
		}
		fmt.Printf(cliApp.t("desktop_cli_paused_session")+"\n", args[1])

	case "complete":
		if len(args) < 2 {
			fmt.Println(cliApp.t("desktop_cli_err_session_id_required"))
			return fmt.Errorf("missing session ID")
		}
		err := cliApp.sessionManager.Complete(args[1])
		if err != nil {
			return err
		}
		fmt.Printf(cliApp.t("desktop_cli_completed_session")+"\n", args[1])

	default:
		fmt.Printf(cliApp.t("desktop_cli_unknown_subcommand")+"\n", args[0])
	}

	return nil
}

func (cliApp *CLIApp) cmdTasks(args []string) error {
	if len(args) == 0 {
		args = []string{"list"}
	}

	ctx := context.Background()

	switch args[0] {
	case "list":
		tasks := cliApp.taskManager.GetAllTasks()
		fmt.Println(cliApp.t("desktop_cli_tasks_header"))
		if len(tasks) == 0 {
			fmt.Println(cliApp.t("desktop_cli_no_tasks"))
			return nil
		}
		for _, t := range tasks {
			fmt.Printf("- [%s] %s: %s\n", t.Status, t.Type, t.Description)
		}

	case "create":
		fs := flag.NewFlagSet("tasks create", flag.ExitOnError)
		taskType := fs.String("type", "building", cliApp.t("desktop_cli_flag_task_type"))
		desc := fs.String("desc", "", cliApp.t("desktop_cli_flag_task_desc"))
		priority := fs.String("priority", "normal", cliApp.t("desktop_cli_flag_task_priority"))
		fs.Parse(args[1:])

		if *desc == "" {
			fmt.Println(cliApp.t("desktop_cli_err_desc_required"))
			return fmt.Errorf("missing required arguments")
		}

		t, err := cliApp.taskManager.CreateTask(ctx, *taskType, *desc, *priority)
		if err != nil {
			return err
		}
		fmt.Printf(cliApp.t("desktop_cli_created_task")+"\n", t.Description, t.ID)

	case "cancel":
		if len(args) < 2 {
			fmt.Println(cliApp.t("desktop_cli_err_task_id_required"))
			return fmt.Errorf("missing task ID")
		}
		err := cliApp.taskManager.CancelTask(ctx, args[1])
		if err != nil {
			return err
		}
		fmt.Printf(cliApp.t("desktop_cli_cancelled_task")+"\n", args[1])

	default:
		fmt.Printf(cliApp.t("desktop_cli_unknown_subcommand")+"\n", args[0])
	}

	return nil
}

func (cliApp *CLIApp) cmdWorkers(args []string) error {
	if len(args) == 0 {
		args = []string{"list"}
	}

	switch args[0] {
	case "list":
		workers := cliApp.workerManager.GetWorkers()
		fmt.Println(cliApp.t("desktop_cli_workers_header"))
		if len(workers) == 0 {
			fmt.Println(cliApp.t("desktop_cli_no_workers"))
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
			fmt.Println(cliApp.t("desktop_cli_err_host_required"))
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
		fmt.Printf(cliApp.t("desktop_cli_added_worker")+"\n", w.ID)

	case "remove":
		if len(args) < 2 {
			fmt.Println(cliApp.t("desktop_cli_err_worker_id_required"))
			return fmt.Errorf("missing worker ID")
		}
		err := cliApp.workerManager.RemoveWorker(args[1])
		if err != nil {
			return err
		}
		fmt.Printf(cliApp.t("desktop_cli_removed_worker")+"\n", args[1])

	default:
		fmt.Printf(cliApp.t("desktop_cli_unknown_subcommand")+"\n", args[0])
	}

	return nil
}

func (cliApp *CLIApp) cmdLLM(args []string) error {
	if len(args) == 0 {
		args = []string{"providers"}
	}

	switch args[0] {
	case "providers":
		ctx := context.Background()
		health := cliApp.llmManager.HealthCheck(ctx)
		fmt.Println(cliApp.t("desktop_cli_llm_providers_header"))
		if len(health) == 0 {
			fmt.Println(cliApp.t("desktop_cli_no_providers"))
			return nil
		}
		for provider, status := range health {
			fmt.Printf("- %s: %s\n", provider, status.Status)
		}

	case "models":
		models := cliApp.llmManager.GetAvailableModels()
		fmt.Println(cliApp.t("desktop_cli_available_models_header"))
		if len(models) == 0 {
			fmt.Println(cliApp.t("desktop_cli_no_models"))
			return nil
		}
		for _, m := range models {
			fmt.Printf("- %s (%s) - Context: %d\n", m.Name, m.Provider, m.ContextSize)
		}

	case "chat":
		fmt.Println(cliApp.t("desktop_cli_chat_requires_provider"))
		fmt.Println(cliApp.t("desktop_cli_chat_configure_hint"))

	case "specify":
		return cliApp.cmdSpecify(args[1:])

	default:
		fmt.Printf(cliApp.t("desktop_cli_unknown_subcommand")+"\n", args[0])
	}

	return nil
}

// cmdSpecify drives the headless desktop client's REAL speckit Specify phase
// (specify_nogui.go → (*CLIApp).Specify). It surfaces the real phase output or
// the real error verbatim — never a fabricated success (§11.4 / CONST-035).
// Wired both as `helix specify <request>` and `helix llm specify <request>`.
func (cliApp *CLIApp) cmdSpecify(args []string) error {
	request := strings.TrimSpace(strings.Join(args, " "))
	if request == "" {
		fmt.Println("usage: specify <request>")
		return fmt.Errorf("specify: missing request")
	}

	out, err := cliApp.Specify(request)
	if err != nil {
		// Real error, surfaced verbatim (anti-bluff).
		fmt.Printf("❌ specify failed: %v\n", err)
		return err
	}
	fmt.Print(out)
	return nil
}

func (cliApp *CLIApp) cmdInteractive() error {
	fmt.Println(cliApp.t("desktop_cli_interactive_header"))
	fmt.Println(cliApp.t("desktop_cli_interactive_hint"))
	fmt.Println()

	// Setup signal handling
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-sigChan
		fmt.Println("\n" + cliApp.t("desktop_cli_exiting"))
		os.Exit(0)
	}()

	var input string
	for {
		fmt.Print("helix> ")
		_, err := fmt.Scanln(&input)
		if err != nil {
			continue
		}

		input = strings.TrimSpace(input)
		if input == "" {
			continue
		}

		if input == "quit" || input == "exit" {
			fmt.Println(cliApp.t("desktop_cli_goodbye"))
			break
		}

		args := strings.Fields(input)
		if err := cliApp.Run(args); err != nil {
			fmt.Printf(cliApp.t("desktop_cli_error_prefix")+"\n", err)
		}
		fmt.Println()
	}

	return nil
}

func main() {
	app := NewCLIApp()

	// Wire the real CONST-046 translator (embedded active.en.yaml
	// bundle) onto the app BEFORE any user-facing output, replacing the
	// NoopTranslator{} message-ID-echo default installed by NewCLIApp.
	// Without this, every command (help/status/...) leaks raw message
	// keys (`desktop_cli_status_header`, ...) and any Printf call site
	// passing args to an unresolved bare-key "format" emits Go's
	// `%!(EXTRA ...)` noise — a §11.4 / CONST-046 PASS-bluff. On bundle
	// load failure the loud NoopTranslator{} echo is preserved (never a
	// silent swallow). Mirrors applications/terminal_ui/i18n_boot_wire.go.
	if tr, err := i18n.NewTranslator(); err != nil {
		log.Printf("⚠️  i18n: falling back to message-ID echo (bundle load failed): %v", err)
	} else {
		app.SetTranslator(tr)
	}

	args := os.Args[1:]

	// help/version-style requests must resolve and print without
	// requiring full initialization (config/db/redis). This also keeps
	// `help` working on hosts without a complete config — matching the
	// aurora_os/harmony_os nogui boot ordering.
	if len(args) == 0 || args[0] == "help" || args[0] == "-h" || args[0] == "--help" {
		app.printHelp()
		return
	}

	if err := app.Initialize(); err != nil {
		log.Fatalf("Failed to initialize: %v", err)
	}
	defer app.Close()

	if err := app.Run(args); err != nil {
		os.Exit(1)
	}
}
