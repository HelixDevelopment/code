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

	"dev.helix.code/applications/aurora_os/i18n"
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

// AuroraSecurityManager handles Aurora OS security features for CLI
type AuroraSecurityManager struct {
	encryptionEnabled bool
	accessControl     map[string][]string
	auditLog          []AuditLogEntry
	mu                sync.RWMutex
}

// AuditLogEntry represents a security audit log entry
type AuditLogEntry struct {
	Timestamp time.Time
	Action    string
	User      string
	Details   string
	Severity  string
}

// NewAuroraSecurityManager creates a new security manager
func NewAuroraSecurityManager() *AuroraSecurityManager {
	return &AuroraSecurityManager{
		encryptionEnabled: true,
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

// CLIApp represents the CLI application (nogui mode) for Aurora OS
type CLIApp struct {
	config          *config.Config
	db              *database.Database
	taskManager     *CLITaskManager
	workerManager   *CLIWorkerManager
	projectManager  *project.Manager
	sessionManager  *session.Manager
	llmManager      *llm.ModelManager
	securityManager *AuroraSecurityManager

	// Aurora OS specific
	performanceMode bool
	diagnosticsLog  []string

	// translator resolves user-facing strings per CONST-046
	// (round-327 §11.4 migration). Defaults to NoopTranslator
	// (loud echo of message IDs) until SetTranslator wires a real
	// *i18nadapter.Translator at boot. Never nil after NewCLIApp
	// returns.
	translator i18n.Translator
}

// NewCLIApp creates a new CLI application
func NewCLIApp() *CLIApp {
	return &CLIApp{
		securityManager: NewAuroraSecurityManager(),
		diagnosticsLog:  make([]string, 0),
		translator:      i18n.NoopTranslator{},
	}
}

// SetTranslator injects the runtime Translator (per CONST-046
// round-327). Passing nil is a no-op — the NoopTranslator default
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

	// Log initialization
	cliApp.securityManager.AddAuditEntry("system_init", "system", cliApp.t("aurora_os_cli_audit_app_initialized"), "info")

	return nil
}

// Close cleans up resources
func (cliApp *CLIApp) Close() error {
	cliApp.securityManager.AddAuditEntry("system_shutdown", "system", cliApp.t("aurora_os_cli_audit_app_shutting_down"), "info")
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
	case "aurora":
		return cliApp.cmdAurora(cmdArgs)
	case "security":
		return cliApp.cmdSecurity(cmdArgs)
	case "interactive":
		return cliApp.cmdInteractive()
	default:
		fmt.Printf(cliApp.t("aurora_os_cli_unknown_command")+"\n", command)
		cliApp.printHelp()
		return fmt.Errorf("unknown command: %s", command)
	}

	return nil
}

func (cliApp *CLIApp) printHelp() {
	fmt.Println(cliApp.t("aurora_os_cli_help_body"))
}

func (cliApp *CLIApp) cmdStatus() error {
	fmt.Println(cliApp.t("aurora_os_cli_status_header"))
	fmt.Println()

	// System info
	fmt.Printf(cliApp.t("aurora_os_cli_status_platform")+"\n", runtime.GOOS, runtime.GOARCH)
	fmt.Printf(cliApp.t("aurora_os_cli_status_performance_mode")+"\n", map[bool]string{true: "Enabled", false: "Disabled"}[cliApp.performanceMode])
	fmt.Println()

	// Workers
	workers := cliApp.workerManager.GetWorkers()
	activeWorkers := 0
	for _, w := range workers {
		if w.Status == "active" {
			activeWorkers++
		}
	}
	fmt.Printf(cliApp.t("aurora_os_cli_status_workers")+"\n", len(workers), activeWorkers)

	// Tasks
	totalTasks, completedTasks, runningTasks := cliApp.taskManager.GetStats()
	fmt.Printf(cliApp.t("aurora_os_cli_status_tasks")+"\n",
		totalTasks, runningTasks, completedTasks)

	// Projects
	ctx := context.Background()
	projects, _ := cliApp.projectManager.ListProjects(ctx, "")
	activeProject, _ := cliApp.projectManager.GetActiveProject(ctx)
	activeProjectName := "none"
	if activeProject != nil {
		activeProjectName = activeProject.Name
	}
	fmt.Printf(cliApp.t("aurora_os_cli_status_projects")+"\n", len(projects), activeProjectName)

	// Sessions
	sessions := cliApp.sessionManager.GetAll()
	activeSessions := 0
	for _, s := range sessions {
		if s.Status == session.StatusActive {
			activeSessions++
		}
	}
	fmt.Printf(cliApp.t("aurora_os_cli_status_sessions")+"\n", len(sessions), activeSessions)

	// LLM
	models := cliApp.llmManager.GetAvailableModels()
	fmt.Printf(cliApp.t("aurora_os_cli_status_llm_models")+"\n", len(models))

	// Security
	fmt.Println()
	fmt.Println(cliApp.t("aurora_os_cli_security_status_header"))
	fmt.Printf(cliApp.t("aurora_os_cli_encryption_status")+"\n", map[bool]string{true: "Enabled", false: "Disabled"}[cliApp.securityManager.encryptionEnabled])
	fmt.Printf(cliApp.t("aurora_os_cli_audit_log_entries")+"\n", len(cliApp.securityManager.GetAuditLog()))

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
		fmt.Println(cliApp.t("aurora_os_cli_projects_header"))
		if len(projects) == 0 {
			fmt.Println(cliApp.t("aurora_os_cli_no_projects"))
			return nil
		}
		for _, p := range projects {
			activeMarker := ""
			if p.Active {
				activeMarker = " [ACTIVE]"
			}
			fmt.Printf(cliApp.t("aurora_os_cli_project_list_item")+"\n", p.Name, p.Type, p.Path, activeMarker)
		}
		cliApp.securityManager.AddAuditEntry("project_list", "user", cliApp.t("aurora_os_cli_audit_projects_listed"), "info")

	case "create":
		fs := flag.NewFlagSet("projects create", flag.ExitOnError)
		name := fs.String("name", "", "Project name")
		path := fs.String("path", "", "Project path")
		desc := fs.String("desc", "", cliApp.t("aurora_os_cli_flag_project_desc"))
		ptype := fs.String("type", "generic", cliApp.t("aurora_os_cli_flag_project_type"))
		fs.Parse(args[1:])

		if *name == "" || *path == "" {
			fmt.Println(cliApp.t("aurora_os_cli_err_name_path_required"))
			return fmt.Errorf("missing required arguments")
		}

		proj, err := cliApp.projectManager.CreateProject(ctx, *name, *desc, *path, *ptype)
		if err != nil {
			return err
		}
		fmt.Printf(cliApp.t("aurora_os_cli_created_project")+"\n", proj.Name, proj.ID)
		cliApp.securityManager.AddAuditEntry("project_create", "user", fmt.Sprintf(cliApp.t("aurora_os_cli_audit_project_created_fmt"), *name), "info")

	case "set-active":
		if len(args) < 2 {
			fmt.Println(cliApp.t("aurora_os_cli_err_project_id_required"))
			return fmt.Errorf("missing project ID")
		}
		err := cliApp.projectManager.SetActiveProject(ctx, args[1])
		if err != nil {
			return err
		}
		fmt.Printf(cliApp.t("aurora_os_cli_set_active_project")+"\n", args[1])
		cliApp.securityManager.AddAuditEntry("project_set_active", "user", fmt.Sprintf(cliApp.t("aurora_os_cli_audit_project_set_active_fmt"), args[1]), "info")

	case "delete":
		if len(args) < 2 {
			fmt.Println(cliApp.t("aurora_os_cli_err_project_id_required"))
			return fmt.Errorf("missing project ID")
		}
		err := cliApp.projectManager.DeleteProject(ctx, args[1])
		if err != nil {
			return err
		}
		fmt.Printf(cliApp.t("aurora_os_cli_deleted_project")+"\n", args[1])
		cliApp.securityManager.AddAuditEntry("project_delete", "user", fmt.Sprintf(cliApp.t("aurora_os_cli_audit_project_deleted_fmt"), args[1]), "warning")

	default:
		fmt.Printf(cliApp.t("aurora_os_cli_unknown_subcommand")+"\n", args[0])
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
		fmt.Println(cliApp.t("aurora_os_cli_sessions_header"))
		if len(sessions) == 0 {
			fmt.Println(cliApp.t("aurora_os_cli_no_sessions"))
			return nil
		}
		for _, s := range sessions {
			fmt.Printf(cliApp.t("aurora_os_cli_session_list_item")+"\n", s.Name, s.Status, s.Mode, s.ProjectID)
		}

	case "create":
		fs := flag.NewFlagSet("sessions create", flag.ExitOnError)
		name := fs.String("name", "", "Session name")
		projectID := fs.String("project", "", "Project ID")
		desc := fs.String("desc", "", cliApp.t("aurora_os_cli_flag_session_desc"))
		mode := fs.String("mode", "building", cliApp.t("aurora_os_cli_flag_session_mode"))
		fs.Parse(args[1:])

		if *name == "" || *projectID == "" {
			fmt.Println(cliApp.t("aurora_os_cli_err_name_project_required"))
			return fmt.Errorf("missing required arguments")
		}

		sess, err := cliApp.sessionManager.Create(*projectID, *name, *desc, session.Mode(*mode))
		if err != nil {
			return err
		}
		fmt.Printf(cliApp.t("aurora_os_cli_created_session")+"\n", sess.Name, sess.ID)
		cliApp.securityManager.AddAuditEntry("session_create", "user", fmt.Sprintf(cliApp.t("aurora_os_cli_audit_session_created_fmt"), *name), "info")

	case "start":
		if len(args) < 2 {
			fmt.Println(cliApp.t("aurora_os_cli_err_session_id_required"))
			return fmt.Errorf("missing session ID")
		}
		err := cliApp.sessionManager.Start(args[1])
		if err != nil {
			return err
		}
		fmt.Printf(cliApp.t("aurora_os_cli_started_session")+"\n", args[1])
		cliApp.securityManager.AddAuditEntry("session_start", "user", fmt.Sprintf(cliApp.t("aurora_os_cli_audit_session_started_fmt"), args[1]), "info")

	case "pause":
		if len(args) < 2 {
			fmt.Println(cliApp.t("aurora_os_cli_err_session_id_required"))
			return fmt.Errorf("missing session ID")
		}
		err := cliApp.sessionManager.Pause(args[1])
		if err != nil {
			return err
		}
		fmt.Printf(cliApp.t("aurora_os_cli_paused_session")+"\n", args[1])
		cliApp.securityManager.AddAuditEntry("session_pause", "user", fmt.Sprintf(cliApp.t("aurora_os_cli_audit_session_paused_fmt"), args[1]), "info")

	case "complete":
		if len(args) < 2 {
			fmt.Println(cliApp.t("aurora_os_cli_err_session_id_required"))
			return fmt.Errorf("missing session ID")
		}
		err := cliApp.sessionManager.Complete(args[1])
		if err != nil {
			return err
		}
		fmt.Printf(cliApp.t("aurora_os_cli_completed_session")+"\n", args[1])
		cliApp.securityManager.AddAuditEntry("session_complete", "user", fmt.Sprintf(cliApp.t("aurora_os_cli_audit_session_completed_fmt"), args[1]), "info")

	default:
		fmt.Printf(cliApp.t("aurora_os_cli_unknown_subcommand")+"\n", args[0])
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
		fmt.Println(cliApp.t("aurora_os_cli_tasks_header"))
		if len(tasks) == 0 {
			fmt.Println(cliApp.t("aurora_os_cli_no_tasks"))
			return nil
		}
		for _, t := range tasks {
			fmt.Printf(cliApp.t("aurora_os_cli_task_list_item")+"\n", t.Status, t.Type, t.Description, t.Priority)
		}

	case "create":
		fs := flag.NewFlagSet("tasks create", flag.ExitOnError)
		taskType := fs.String("type", "building", cliApp.t("aurora_os_cli_flag_task_type"))
		desc := fs.String("desc", "", cliApp.t("aurora_os_cli_flag_task_desc"))
		priority := fs.String("priority", "normal", cliApp.t("aurora_os_cli_flag_task_priority"))
		fs.Parse(args[1:])

		if *desc == "" {
			fmt.Println(cliApp.t("aurora_os_cli_err_desc_required"))
			return fmt.Errorf("missing required arguments")
		}

		t, err := cliApp.taskManager.CreateTask(ctx, *taskType, *desc, *priority)
		if err != nil {
			return err
		}
		fmt.Printf(cliApp.t("aurora_os_cli_created_task")+"\n", t.Description, t.ID)
		cliApp.securityManager.AddAuditEntry("task_create", "user", fmt.Sprintf(cliApp.t("aurora_os_cli_audit_task_created_fmt"), *desc), "info")

	case "cancel":
		if len(args) < 2 {
			fmt.Println(cliApp.t("aurora_os_cli_err_task_id_required"))
			return fmt.Errorf("missing task ID")
		}
		err := cliApp.taskManager.CancelTask(ctx, args[1])
		if err != nil {
			return err
		}
		fmt.Printf(cliApp.t("aurora_os_cli_cancelled_task")+"\n", args[1])
		cliApp.securityManager.AddAuditEntry("task_cancel", "user", fmt.Sprintf(cliApp.t("aurora_os_cli_audit_task_cancelled_fmt"), args[1]), "warning")

	default:
		fmt.Printf(cliApp.t("aurora_os_cli_unknown_subcommand")+"\n", args[0])
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
		fmt.Println(cliApp.t("aurora_os_cli_workers_header"))
		if len(workers) == 0 {
			fmt.Println(cliApp.t("aurora_os_cli_no_workers"))
			return nil
		}
		for _, w := range workers {
			healthStatus := "unhealthy"
			if w.Healthy {
				healthStatus = "healthy"
			}
			fmt.Printf(cliApp.t("aurora_os_cli_worker_list_item")+"\n", w.ID, w.Status, w.Host, w.Port, healthStatus)
		}

	case "add":
		fs := flag.NewFlagSet("workers add", flag.ExitOnError)
		host := fs.String("host", "", "Worker host")
		port := fs.String("port", "22", "Worker port")
		user := fs.String("user", "", "SSH user")
		fs.Parse(args[1:])

		if *host == "" {
			fmt.Println(cliApp.t("aurora_os_cli_err_host_required"))
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
		fmt.Printf(cliApp.t("aurora_os_cli_added_worker")+"\n", w.ID)
		cliApp.securityManager.AddAuditEntry("worker_add", "user", fmt.Sprintf(cliApp.t("aurora_os_cli_audit_worker_added_fmt"), *host), "info")

	case "remove":
		if len(args) < 2 {
			fmt.Println(cliApp.t("aurora_os_cli_err_worker_id_required"))
			return fmt.Errorf("missing worker ID")
		}
		err := cliApp.workerManager.RemoveWorker(args[1])
		if err != nil {
			return err
		}
		fmt.Printf(cliApp.t("aurora_os_cli_removed_worker")+"\n", args[1])
		cliApp.securityManager.AddAuditEntry("worker_remove", "user", fmt.Sprintf(cliApp.t("aurora_os_cli_audit_worker_removed_fmt"), args[1]), "warning")

	default:
		fmt.Printf(cliApp.t("aurora_os_cli_unknown_subcommand")+"\n", args[0])
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
		fmt.Println(cliApp.t("aurora_os_cli_llm_providers_header"))
		if len(health) == 0 {
			fmt.Println(cliApp.t("aurora_os_cli_no_providers"))
			return nil
		}
		for provider, status := range health {
			fmt.Printf(cliApp.t("aurora_os_cli_provider_list_item")+"\n", provider, status.Status)
		}

	case "models":
		models := cliApp.llmManager.GetAvailableModels()
		fmt.Println(cliApp.t("aurora_os_cli_models_header"))
		if len(models) == 0 {
			fmt.Println(cliApp.t("aurora_os_cli_no_models"))
			return nil
		}
		for _, m := range models {
			fmt.Printf(cliApp.t("aurora_os_cli_model_list_item")+"\n", m.Name, m.Provider, m.ContextSize)
		}

	case "chat":
		// Real LLM chat: the prompt is the positional args after "chat".
		// e.g. `llm chat What is 2+2?` -> prompt = "What is 2+2?".
		prompt := strings.TrimSpace(strings.Join(args[1:], " "))
		if prompt == "" {
			// Honest input-validation message, not a bluff: nothing to send.
			fmt.Println(cliApp.t("aurora_os_cli_llm_chat_usage"))
			return nil
		}
		// Anti-bluff (BLUFF-001 / CONST-035 / CONST-036): delegate to the real
		// Generate path which resolves a genuine llm.Provider and makes a real
		// provider.Generate call. No simulation, no canned response.
		out, err := cliApp.Generate(prompt)
		if err != nil {
			// Surface the real provider/transport error verbatim, and keep the
			// configure-hint as the no-provider remediation path.
			fmt.Printf(cliApp.t("aurora_os_cli_llm_chat_error")+"\n", err)
			fmt.Println(cliApp.t("aurora_os_cli_llm_chat_configure_hint"))
			return err
		}
		fmt.Println(out)

	default:
		fmt.Printf(cliApp.t("aurora_os_cli_unknown_subcommand")+"\n", args[0])
	}

	return nil
}

func (cliApp *CLIApp) cmdAurora(args []string) error {
	if len(args) == 0 {
		args = []string{"info"}
	}

	switch args[0] {
	case "info":
		fmt.Println(cliApp.t("aurora_os_cli_info_header"))
		fmt.Printf(cliApp.t("aurora_os_cli_info_platform")+"\n", runtime.GOOS, runtime.GOARCH)
		fmt.Printf(cliApp.t("aurora_os_cli_info_go_version")+"\n", runtime.Version())
		fmt.Printf(cliApp.t("aurora_os_cli_info_cpus")+"\n", runtime.NumCPU())
		fmt.Printf(cliApp.t("aurora_os_cli_info_goroutines")+"\n", runtime.NumGoroutine())
		fmt.Printf(cliApp.t("aurora_os_cli_info_performance_mode")+"\n", map[bool]string{true: "Enabled", false: "Disabled"}[cliApp.performanceMode])

		var m runtime.MemStats
		runtime.ReadMemStats(&m)
		fmt.Printf(cliApp.t("aurora_os_cli_info_memory_alloc")+"\n", float64(m.Alloc)/1024/1024)
		fmt.Printf(cliApp.t("aurora_os_cli_info_memory_sys")+"\n", float64(m.Sys)/1024/1024)
		fmt.Printf(cliApp.t("aurora_os_cli_info_gc_cycles")+"\n", m.NumGC)

	case "diagnostics":
		return cliApp.runDiagnostics()

	case "performance":
		cliApp.performanceMode = !cliApp.performanceMode
		status := "Disabled"
		if cliApp.performanceMode {
			status = "Enabled"
			// Apply performance optimizations
			runtime.GOMAXPROCS(runtime.NumCPU())
		}
		fmt.Printf(cliApp.t("aurora_os_cli_performance_mode_toggle")+"\n", status)
		cliApp.securityManager.AddAuditEntry("performance_toggle", "user", fmt.Sprintf(cliApp.t("aurora_os_cli_audit_perf_mode_fmt"), status), "info")

	case "optimize":
		return cliApp.runOptimization()

	default:
		fmt.Printf(cliApp.t("aurora_os_cli_unknown_subcommand")+"\n", args[0])
	}

	return nil
}

func (cliApp *CLIApp) runDiagnostics() error {
	fmt.Println(cliApp.t("aurora_os_cli_diagnostics_header"))
	fmt.Println()

	cliApp.diagnosticsLog = make([]string, 0)
	addDiag := func(check, status, details string) {
		entry := fmt.Sprintf(cliApp.t("aurora_os_cli_diagnostic_entry"), status, check, details)
		cliApp.diagnosticsLog = append(cliApp.diagnosticsLog, entry)
		fmt.Println(entry)
	}

	// System checks
	fmt.Println(cliApp.t("aurora_os_cli_running_system_checks"))
	addDiag(cliApp.t("aurora_os_cli_diag_check_cpu_count"), "OK",
		fmt.Sprintf(cliApp.t("aurora_os_cli_diag_detail_cpus_available"), runtime.NumCPU()))
	addDiag(cliApp.t("aurora_os_cli_diag_check_goroutines"), "OK",
		fmt.Sprintf(cliApp.t("aurora_os_cli_diag_detail_goroutines_active"), runtime.NumGoroutine()))

	// Memory check
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	memStatus := "OK"
	if m.Alloc > 500*1024*1024 { // > 500MB
		memStatus = "WARNING"
	}
	addDiag(cliApp.t("aurora_os_cli_diag_check_memory_usage"), memStatus,
		fmt.Sprintf(cliApp.t("aurora_os_cli_diag_detail_mb_allocated"), float64(m.Alloc)/1024/1024))

	// Database check
	if cliApp.db != nil {
		addDiag(cliApp.t("aurora_os_cli_diag_check_database"), "OK",
			cliApp.t("aurora_os_cli_diag_detail_connected"))
	} else {
		addDiag(cliApp.t("aurora_os_cli_diag_check_database"), "WARNING",
			cliApp.t("aurora_os_cli_diag_detail_not_connected"))
	}

	// Component checks
	initialized := cliApp.t("aurora_os_cli_diag_detail_initialized")
	addDiag(cliApp.t("aurora_os_cli_diag_check_task_manager"), "OK", initialized)
	addDiag(cliApp.t("aurora_os_cli_diag_check_worker_manager"), "OK", initialized)
	addDiag(cliApp.t("aurora_os_cli_diag_check_project_manager"), "OK", initialized)
	addDiag(cliApp.t("aurora_os_cli_diag_check_session_manager"), "OK", initialized)
	addDiag(cliApp.t("aurora_os_cli_diag_check_llm_manager"), "OK", initialized)
	addDiag(cliApp.t("aurora_os_cli_diag_check_security_manager"), "OK",
		fmt.Sprintf(cliApp.t("aurora_os_cli_diag_detail_encryption"), cliApp.securityManager.encryptionEnabled))

	// Performance mode check
	perfStatus := "OK"
	if !cliApp.performanceMode {
		perfStatus = "INFO"
	}
	perfDetail := cliApp.t("aurora_os_cli_diag_detail_perf_enabled")
	if !cliApp.performanceMode {
		perfDetail = cliApp.t("aurora_os_cli_diag_detail_perf_disabled")
	}
	addDiag(cliApp.t("aurora_os_cli_diag_check_performance_mode"), perfStatus, perfDetail)

	fmt.Println()
	fmt.Printf(cliApp.t("aurora_os_cli_diagnostics_complete")+"\n", len(cliApp.diagnosticsLog))

	cliApp.securityManager.AddAuditEntry("diagnostics_run", "user", fmt.Sprintf(cliApp.t("aurora_os_cli_audit_diagnostics_fmt"), len(cliApp.diagnosticsLog)), "info")

	return nil
}

func (cliApp *CLIApp) runOptimization() error {
	fmt.Println(cliApp.t("aurora_os_cli_optimization_header"))
	fmt.Println()

	// Force garbage collection
	fmt.Println(cliApp.t("aurora_os_cli_running_gc"))
	var before runtime.MemStats
	runtime.ReadMemStats(&before)
	runtime.GC()
	var after runtime.MemStats
	runtime.ReadMemStats(&after)
	freed := memFreedMB(before.Alloc, after.Alloc)
	fmt.Printf(cliApp.t("aurora_os_cli_memory_freed")+"\n", freed)

	// Optimize GOMAXPROCS
	fmt.Printf(cliApp.t("aurora_os_cli_setting_gomaxprocs")+"\n", runtime.NumCPU())
	runtime.GOMAXPROCS(runtime.NumCPU())

	// Enable performance mode if not already enabled
	if !cliApp.performanceMode {
		cliApp.performanceMode = true
		fmt.Println(cliApp.t("aurora_os_cli_performance_mode_enabled"))
	}

	fmt.Println()
	fmt.Println(cliApp.t("aurora_os_cli_optimization_complete"))

	cliApp.securityManager.AddAuditEntry("optimization_run", "user", fmt.Sprintf(cliApp.t("aurora_os_cli_audit_optimization_fmt"), freed), "info")

	return nil
}

func (cliApp *CLIApp) cmdSecurity(args []string) error {
	if len(args) == 0 {
		args = []string{"status"}
	}

	switch args[0] {
	case "status":
		fmt.Println(cliApp.t("aurora_os_cli_security_status_header"))
		fmt.Printf(cliApp.t("aurora_os_cli_encryption_status")+"\n", map[bool]string{true: "Enabled", false: "Disabled"}[cliApp.securityManager.encryptionEnabled])
		fmt.Printf(cliApp.t("aurora_os_cli_audit_log_entries")+"\n", len(cliApp.securityManager.GetAuditLog()))
		fmt.Println("\n" + cliApp.t("aurora_os_cli_access_control_roles_label"))
		for role, perms := range cliApp.securityManager.accessControl {
			fmt.Printf(cliApp.t("aurora_os_cli_role_perms_item")+"\n", role, perms)
		}

	case "audit":
		fmt.Println(cliApp.t("aurora_os_cli_audit_log_header"))
		auditLog := cliApp.securityManager.GetAuditLog()
		if len(auditLog) == 0 {
			fmt.Println(cliApp.t("aurora_os_cli_no_audit_entries"))
			return nil
		}
		for _, entry := range auditLog {
			fmt.Printf(cliApp.t("aurora_os_cli_audit_entry_line")+"\n",
				entry.Timestamp.Format("2006-01-02 15:04:05"),
				entry.Severity,
				entry.Action,
				entry.Details,
				entry.User)
		}

	case "encryption":
		if len(args) > 1 {
			switch args[1] {
			case "enable":
				cliApp.securityManager.encryptionEnabled = true
				fmt.Println(cliApp.t("aurora_os_cli_encryption_enabled"))
				cliApp.securityManager.AddAuditEntry("encryption_enable", "user", cliApp.t("aurora_os_cli_audit_encryption_enabled"), "info")
			case "disable":
				cliApp.securityManager.encryptionEnabled = false
				fmt.Println(cliApp.t("aurora_os_cli_encryption_disabled"))
				cliApp.securityManager.AddAuditEntry("encryption_disable", "user", cliApp.t("aurora_os_cli_audit_encryption_disabled"), "warning")
			default:
				fmt.Printf(cliApp.t("aurora_os_cli_unknown_encryption_command")+"\n", args[1])
			}
		} else {
			fmt.Printf(cliApp.t("aurora_os_cli_encryption_status")+"\n", map[bool]string{true: "Enabled", false: "Disabled"}[cliApp.securityManager.encryptionEnabled])
		}

	case "access":
		if len(args) > 1 {
			switch args[1] {
			case "list":
				fmt.Println(cliApp.t("aurora_os_cli_access_roles_header"))
				for role, perms := range cliApp.securityManager.accessControl {
					fmt.Printf(cliApp.t("aurora_os_cli_role_perms_item")+"\n", role, perms)
				}
			case "add":
				if len(args) < 4 {
					fmt.Println(cliApp.t("aurora_os_cli_access_add_usage"))
					return nil
				}
				role := args[2]
				perm := args[3]
				if _, exists := cliApp.securityManager.accessControl[role]; !exists {
					cliApp.securityManager.accessControl[role] = []string{}
				}
				cliApp.securityManager.accessControl[role] = append(cliApp.securityManager.accessControl[role], perm)
				fmt.Printf(cliApp.t("aurora_os_cli_permission_added")+"\n", perm, role)
				cliApp.securityManager.AddAuditEntry("access_add", "user", fmt.Sprintf(cliApp.t("aurora_os_cli_audit_permission_added_fmt"), perm, role), "info")
			default:
				fmt.Printf(cliApp.t("aurora_os_cli_unknown_access_command")+"\n", args[1])
			}
		} else {
			fmt.Println(cliApp.t("aurora_os_cli_access_usage"))
		}

	default:
		fmt.Printf(cliApp.t("aurora_os_cli_unknown_subcommand")+"\n", args[0])
	}

	return nil
}

func (cliApp *CLIApp) cmdInteractive() error {
	fmt.Println(cliApp.t("aurora_os_cli_interactive_header"))
	fmt.Println(cliApp.t("aurora_os_cli_interactive_hint"))
	fmt.Println()

	// Setup signal handling
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-sigChan
		fmt.Println("\n" + cliApp.t("aurora_os_cli_exiting"))
		os.Exit(0)
	}()

	var input string
	for {
		fmt.Print(cliApp.t("aurora_os_cli_interactive_prompt"))
		_, err := fmt.Scanln(&input)
		if err != nil {
			continue
		}

		input = strings.TrimSpace(input)
		if input == "" {
			continue
		}

		if input == "quit" || input == "exit" {
			fmt.Println(cliApp.t("aurora_os_cli_goodbye"))
			break
		}

		args := strings.Fields(input)
		if err := cliApp.Run(args); err != nil {
			fmt.Printf(cliApp.t("aurora_os_cli_runtime_error")+"\n", err)
		}
		fmt.Println()
	}

	return nil
}

// wireTranslator injects the real CONST-046 translator (embedded
// active.en.yaml bundle) onto the app BEFORE any user-facing output,
// replacing the NoopTranslator{} message-ID-echo default installed by
// NewCLIApp. Without this, every command (version/help/status/...) leaks raw
// message keys (`aurora_os_cli_version_banner`, ...) and any Printf call site
// passing args to an unresolved bare-key "format" emits Go's `%!(EXTRA ...)`
// noise — a §11.4 / CONST-046 PASS-bluff. On bundle load failure the loud
// NoopTranslator{} echo is preserved (never a silent swallow). Mirrors
// applications/desktop/main_nogui.go main().
func wireTranslator(app *CLIApp) {
	if tr, err := i18n.NewTranslator(); err != nil {
		log.Printf("⚠️  i18n: falling back to message-ID echo (bundle load failed): %v", err)
	} else {
		app.SetTranslator(tr)
	}
}

func main() {
	args := os.Args[1:]

	// Handle help commands without requiring full initialization
	if len(args) == 0 || args[0] == "help" || args[0] == "-h" || args[0] == "--help" {
		app := NewCLIApp()
		wireTranslator(app)
		app.printHelp()
		return
	}

	// Handle version command without initialization
	if args[0] == "version" || args[0] == "-v" || args[0] == "--version" {
		app := NewCLIApp()
		wireTranslator(app)
		fmt.Println(app.t("aurora_os_cli_version_banner"))
		fmt.Printf(app.t("aurora_os_cli_version_go")+"\n", runtime.Version())
		fmt.Printf(app.t("aurora_os_cli_version_platform")+"\n", runtime.GOOS, runtime.GOARCH)
		return
	}

	app := NewCLIApp()
	wireTranslator(app)

	if err := app.Initialize(); err != nil {
		log.Fatalf("Failed to initialize: %v", err)
	}
	defer app.Close()

	if err := app.Run(args); err != nil {
		os.Exit(1)
	}
}
