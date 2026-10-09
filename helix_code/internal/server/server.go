package server

import (
	"context"
	"log"
	"net/http"
	"strings"
	"time"

	"dev.helix.code/internal/auth"
	"dev.helix.code/internal/config"
	"dev.helix.code/internal/database"
	"dev.helix.code/internal/helixqa"
	"dev.helix.code/internal/llm"
	"dev.helix.code/internal/mcp"
	"dev.helix.code/internal/netutil"
	"dev.helix.code/internal/notification"
	"dev.helix.code/internal/plugins"
	"dev.helix.code/internal/project"
	"dev.helix.code/internal/redis"
	"dev.helix.code/internal/session"
	"dev.helix.code/internal/task"
	"dev.helix.code/internal/verifier"
	"dev.helix.code/internal/worker"
	"github.com/gin-gonic/gin"
)

// Server represents the HTTP server
type Server struct {
	config         *config.Config
	db             *database.Database
	redis          *redis.Client
	auth           *auth.AuthService
	llm            *llm.Provider
	mcp            *mcp.MCPServer
	notification   *notification.NotificationEngine
	taskManager    *task.DatabaseManager
	workerManager  *worker.DatabaseManager
	projectManager *project.DatabaseManager
	sessionManager *session.Manager
	server         *http.Server
	router         *gin.Engine
	startTime      time.Time
	verifierResult *verifier.BootstrapResult
	qaEngine       *helixqa.Engine
	// pluginRegistry is the server's real plugin registry. It is always
	// instantiated (never nil for a server built by New); on boot the server
	// loads any plugins found under the "plugins" directory (a path relative to
	// the server process working directory) into it via plugins.Loader. /server/info advertises
	// plugins_enabled reflecting that this registry is genuinely wired — NOT a
	// cosmetic flag (CONST-035 / §11.4.122: a capability flag is advertised only
	// because the server truly owns the plugin registry it names).
	pluginRegistry *plugins.Registry
}

// New creates a new HTTP server
func New(cfg *config.Config, db *database.Database, rds *redis.Client) *Server {
	// W2c-1 cloud gate (operator mandate 2026-09-05, local-only adaptive
	// serving): wire the hosted-provider gate from configuration BEFORE any
	// handler can construct a provider. Default false — llm.cloud.enabled
	// must be explicitly set true to permit cloud construction.
	llm.SetCloudEnabled(cfg.LLM.Cloud.Enabled)

	// Set Gin mode
	if cfg.Logging.Level == "debug" {
		gin.SetMode(gin.DebugMode)
	} else {
		gin.SetMode(gin.ReleaseMode)
	}

	router := gin.New()

	// Global middleware
	router.Use(gin.Logger())
	router.Use(gin.Recovery())
	router.Use(CORSMiddleware(strings.Split(cfg.Auth.CORSAllowedOrigins, ",")))
	router.Use(SecurityMiddleware())

	// Initialize auth service
	var authService *auth.AuthService
	if db != nil {
		authConfig := auth.AuthConfig{
			JWTSecret:     cfg.Auth.JWTSecret,
			TokenExpiry:   time.Duration(cfg.Auth.TokenExpiry) * time.Second,
			SessionExpiry: time.Duration(cfg.Auth.SessionExpiry) * time.Second,
			BcryptCost:    cfg.Auth.BcryptCost,
		}
		authDB := auth.NewAuthDB(db.Pool)
		authService = auth.NewAuthService(authConfig, authDB)
	}

	// Initialize MCP server. The upgrader's Origin allowlist defaults to
	// same-origin/localhost/no-Origin-header (see mcp.newOriginChecker);
	// extend it with any operator-configured extra origins
	// (cfg.Auth.WSAllowedOrigins / HELIX_WS_ALLOWED_ORIGINS) — see
	// docs/research/07.2026/05_mcp_acp_protocols/WS_ENDPOINT_AUTH_DESIGN.md
	// §6.1.3/§8 Option B.
	mcpServer := mcp.NewMCPServer()
	mcpServer.SetAllowedOrigins(strings.Split(cfg.Auth.WSAllowedOrigins, ","))

	// Initialize notification engine
	notificationEngine := notification.NewNotificationEngine()

	// Server.llm is intentionally left nil here. All LLM concerns
	// surfaced over HTTP (/api/v1/llm/providers, etc.) are answered by
	// the LLMsVerifier subsystem via verifierResult, NOT by a single
	// provider stored on the Server struct (CONST-036: LLMsVerifier is
	// the sole authoritative source). The llm field is retained on the
	// struct as a reserved seam for a future per-server pinned-provider
	// feature; until that lands handlers.go MUST NOT dereference s.llm
	// (round-33 §11.4 honest-init anchor — previous "skip LLM
	// initialization as it requires more complex setup" comment was a
	// PASS-bluff implying a missing wire-up; in fact the verifier
	// already supplies the data and the field is correctly nil;
	// CONST-035 / Article XI §11.9).

	// Initialize task and worker managers if database is available
	var taskMgr *task.DatabaseManager
	var workerMgr *worker.DatabaseManager
	var projectMgr *project.DatabaseManager
	if db != nil {
		taskMgr = task.NewDatabaseManager(db)
		workerMgr = worker.NewDatabaseManager(db)
		projectMgr = project.NewDatabaseManager(db)
	}

	// Initialize session manager
	sessionMgr := session.NewManager()

	// Initialize the plugin registry. The server genuinely owns a plugin
	// registry; any plugins present under the plugin directory are loaded into
	// it at boot. The registry is created unconditionally so plugins_enabled in
	// /server/info reflects a real, wired capability rather than a cosmetic flag
	// (CONST-035 / Article XI §11.9 / §11.4.122). An absent or empty plugin
	// directory yields an empty-but-valid registry (the feature is enabled; it
	// simply has zero plugins loaded yet).
	pluginRegistry := plugins.NewRegistry()
	pluginDir := "plugins"
	pluginLoader := plugins.NewLoader(pluginDir)
	if loaded, err := pluginLoader.LoadAll(context.Background()); err != nil {
		log.Printf("⚠️  Failed to load plugins from %q: %v (continuing with empty registry)", pluginDir, err)
	} else {
		for _, p := range loaded {
			if regErr := pluginRegistry.Register(p); regErr != nil {
				log.Printf("⚠️  Failed to register plugin %q: %v", p.Name(), regErr)
			}
		}
		if len(loaded) > 0 {
			log.Printf("✅ Loaded %d plugin(s) into the server plugin registry", len(loaded))
		}
	}

	// Initialize LLMsVerifier subsystem if enabled
	var verifierResult *verifier.BootstrapResult
	if cfg.Verifier != nil && cfg.Verifier.Enabled {
		var err error
		verifierResult, err = verifier.Bootstrap(cfg.Verifier)
		if err != nil {
			log.Printf("⚠️  Failed to bootstrap verifier: %v (continuing without)", err)
		}
	}

	// Initialize helix_qa engine if enabled
	var qaEngine *helixqa.Engine
	if cfg.QA.Enabled {
		var err error
		qaEngine, err = helixqa.NewEngine(cfg)
		if err != nil {
			log.Printf("⚠️  Failed to initialize helix_qa engine: %v (continuing without)", err)
		} else {
			log.Printf("✅ helix_qa engine initialized (output: %s)", cfg.QA.OutputDir)
		}
	}

	server := &Server{
		config:         cfg,
		db:             db,
		redis:          rds,
		auth:           authService,
		mcp:            mcpServer,
		notification:   notificationEngine,
		taskManager:    taskMgr,
		workerManager:  workerMgr,
		projectManager: projectMgr,
		sessionManager: sessionMgr,
		router:         router,
		startTime:      time.Now(),
		verifierResult: verifierResult,
		qaEngine:       qaEngine,
		pluginRegistry: pluginRegistry,
	}

	// Setup routes
	server.setupRoutes()

	// Create HTTP server
	server.server = &http.Server{
		// HXC-185: cfg.Server.Address is operator-supplied and may be a bare
		// IPv6 literal. "%s:%d" produced "::1:8080", which net.Listen rejects
		// with "too many colons in address", so ListenAndServe never bound.
		// Guarded by TestServerAddr_IPv6_Listens.
		Addr:         netutil.JoinHostPort(cfg.Server.Address, cfg.Server.Port),
		Handler:      router,
		ReadTimeout:  time.Duration(cfg.Server.ReadTimeout) * time.Second,
		WriteTimeout: time.Duration(cfg.Server.WriteTimeout) * time.Second,
		IdleTimeout:  time.Duration(cfg.Server.IdleTimeout) * time.Second,
	}

	return server
}

// Start starts the HTTP server
func (s *Server) Start() error {
	log.Printf("🚀 Starting HelixCode server on %s", s.server.Addr)
	return s.server.ListenAndServe()
}

// Shutdown gracefully shuts down the server
func (s *Server) Shutdown(ctx context.Context) error {
	if s.verifierResult != nil {
		s.verifierResult.Shutdown()
	}
	return s.server.Shutdown(ctx)
}

// setupRoutes sets up all HTTP routes
func (s *Server) setupRoutes() {
	// Health check
	s.router.GET("/health", s.healthCheck)

	// API routes
	api := s.router.Group("/api/v1")
	{
		// API health alias for /health — matches README §API surface and
		// the anti-bluff challenge harness (ddos_health_flood etc.) which
		// probe /api/v1/health as the API-namespaced endpoint per
		// CONST-035 (documented surface must actually exist).
		api.GET("/health", s.healthCheck)

		// Authentication routes
		auth := api.Group("/auth")
		{
			auth.POST("/register", s.register)
			auth.POST("/login", s.login)
			auth.POST("/logout", s.logout)
			auth.POST("/refresh", s.refreshToken)
		}

		// User routes
		users := api.Group("/users")
		users.Use(s.authMiddleware())
		{
			users.GET("/me", s.getCurrentUser)
			users.PUT("/me", s.updateCurrentUser)
			users.DELETE("/me", s.deleteCurrentUser)
		}

		// Worker routes
		workers := api.Group("/workers")
		workers.Use(s.authMiddleware())
		{
			workers.GET("", s.listWorkers)
			workers.POST("", s.createWorker)
			workers.GET("/:id", s.getWorker)
			workers.PUT("/:id", s.updateWorker)
			workers.DELETE("/:id", s.deleteWorker)
			workers.POST("/:id/heartbeat", s.workerHeartbeat)
			workers.GET("/:id/metrics", s.getWorkerMetrics)
		}

		// Task routes
		tasks := api.Group("/tasks")
		tasks.Use(s.authMiddleware())
		{
			tasks.GET("", s.listTasks)
			tasks.POST("", s.createTask)
			tasks.GET("/:id", s.getTask)
			tasks.PUT("/:id", s.updateTask)
			tasks.DELETE("/:id", s.deleteTask)
			tasks.POST("/:id/assign", s.assignTask)
			tasks.POST("/:id/start", s.startTask)
			tasks.POST("/:id/complete", s.completeTask)
			tasks.POST("/:id/fail", s.failTask)
			tasks.POST("/:id/checkpoint", s.createTaskCheckpoint)
			tasks.GET("/:id/checkpoints", s.getTaskCheckpoints)
			tasks.POST("/:id/retry", s.retryTask)
		}

		// Project routes — fully authenticated.
		//
		// Previously POST / + the 4 workflow endpoints were registered
		// under a `publicProjects` group with the comment
		// "no auth for testing" — i.e., anyone could create projects
		// or trigger workflow executions against any projectId without
		// any credential. That was a real production security hole AND
		// inconsistent with createProject's own requirements (the
		// handler now pulls `*auth.User` from context to determine
		// project owner, which only works through authMiddleware).
		// Consolidated into a single authenticated group.
		projects := api.Group("/projects")
		projects.Use(s.authMiddleware())
		{
			projects.GET("", s.listProjects)
			projects.POST("", s.createProject)
			projects.GET("/:id", s.getProject)
			projects.PUT("/:id", s.updateProject)
			projects.DELETE("/:id", s.deleteProject)
			projects.GET("/:id/sessions", s.getProjectSessions)
			projects.POST("/:projectId/workflows/planning", s.executePlanningWorkflow)
			projects.POST("/:projectId/workflows/building", s.executeBuildingWorkflow)
			projects.POST("/:projectId/workflows/testing", s.executeTestingWorkflow)
			projects.POST("/:projectId/workflows/refactoring", s.executeRefactoringWorkflow)
		}

		// Session routes
		sessions := api.Group("/sessions")
		sessions.Use(s.authMiddleware())
		{
			sessions.GET("", s.listSessions)
			sessions.POST("", s.createSession)
			sessions.GET("/:id", s.getSession)
			sessions.PUT("/:id", s.updateSession)
			sessions.DELETE("/:id", s.deleteSession)
		}

		// System routes
		system := api.Group("/system")
		system.Use(s.authMiddleware())
		{
			system.GET("/stats", s.getSystemStats)
			system.GET("/status", s.getSystemStatus)
		}

		// Server info routes (public)
		api.GET("/server/info", s.getServerInfo)
		api.GET("/metrics", s.getMetrics)

		// LLM routes
		//
		// The provider-metadata reads (GET /providers, /providers/:id,
		// /models) stay public — they expose no paid surface and are used by
		// unauthenticated discovery (e.g. the login screen's model picker).
		//
		// The generation surfaces (POST /generate, /stream) DO hit real, paid
		// LLM providers (CONST-035 / BLUFF-001: both build a real llm.Provider
		// per request and call Generate / GenerateStream). They were
		// previously registered with NO auth middleware, so an unauthenticated
		// caller could drive real paid-provider calls — a cost-abuse hole.
		// They now require the SAME JWT auth as the project / task / session
		// groups (VerifyJWTWithDB), via a dedicated authenticated subgroup.
		llmRoutes := api.Group("/llm")
		{
			llmRoutes.GET("/providers", s.listLLMProviders)
			llmRoutes.GET("/providers/:id", s.getLLMProvider)
			llmRoutes.GET("/models", s.listLLMModels)
		}
		llmCost := api.Group("/llm")
		llmCost.Use(s.authMiddleware())
		{
			llmCost.POST("/generate", s.generateLLM)
			llmCost.POST("/stream", s.streamLLM)
		}

		// Real HelixSpecifier "Specify" phase surface (CONST-035): drives the
		// real speckit engine via a real provider-backed debate responder — no
		// simulation. See specify.go. Like the generation surfaces above it
		// consumes a real provider, so it MUST require authentication.
		specify := api.Group("")
		specify.Use(s.authMiddleware())
		specify.POST("/specify", s.specifyHandler)

		// Memory routes
		memory := api.Group("/memory")
		{
			memory.GET("/systems", s.listMemorySystems)
			memory.GET("/stats", s.getMemoryStats)
		}

		// QA routes
		qa := api.Group("/qa")
		qa.Use(s.authMiddleware())
		{
			qa.POST("/session", s.startQASession)
			qa.GET("/sessions", s.listQASessions)
			qa.GET("/session/:id/status", s.getQASessionStatus)
			qa.GET("/session/:id/report", s.getQASessionReport)
			qa.GET("/session/:id/screenshot/:name", s.getQASessionScreenshot)
			qa.DELETE("/session/:id", s.cancelQASession)
		}

		// Screenshot routes (standalone)
		screenshot := api.Group("/screenshot")
		screenshot.Use(s.authMiddleware())
		{
			screenshot.GET("/engines", s.listScreenshotEngines)
			screenshot.GET("/:platform", s.captureScreenshot)
		}
	}

	// P0-T01 (speed programme): opt-in net/http/pprof debug endpoints.
	//
	// The runtime profiling endpoints (/debug/pprof/*) are mounted ONLY when
	// explicitly enabled — either the HELIX_PPROF_HTTP env var is set to a
	// truthy value, or the logging level is "debug". They are OFF by default
	// so a production server never exposes the profiler. When enabled, the
	// endpoints are the standard net/http/pprof surface:
	//   GET /debug/pprof/         — index
	//   GET /debug/pprof/profile  — 30s CPU profile (?seconds= to override)
	//   GET /debug/pprof/heap     — heap profile
	//   GET /debug/pprof/goroutine, /allocs, /block, /mutex, /cmdline, /trace, /symbol
	if pprofHTTPEnabled(s.config) {
		s.mountPprof()
	}

	// Dual OpenAI-style + Anthropic-style wire facade — Provider-Coverage
	// Expansion Plan v2 §3 Phase D
	// (docs/research/07.2026/06_providers_coverage/EXPANSION_PLAN_v2.md).
	// Registered at the WIRE-STANDARD paths (no /api/v1 prefix) so an
	// OpenAI-compatible client (OPENAI_BASE_URL) or an Anthropic-compatible
	// client (ANTHROPIC_BASE_URL, e.g. Claude Code itself) auto-recognizes
	// this server when pointed at it — mirroring HelixLLM's own dual facade.
	// Both handlers translate their wire shape into the EXISTING internal
	// llm.LLMRequest / provider routing (llmProviderResolver +
	// resolveDefaultModel + provider.Generate/GenerateStream) — see
	// wire_facade.go's file-level doc-comment for the reuse contract.
	//
	// SECURITY (independent security review + dual-wire review finding,
	// closed): these two routes drive real, paid-provider LLM generation
	// (CONST-035/BLUFF-001) and were shipped with NO authentication at all —
	// combined with every shipped config profile binding server.address to
	// 0.0.0.0, that made them an unauthenticated, token-consuming surface
	// reachable from any interface. The existing internal-user JWT
	// s.authMiddleware() cannot be reused here (see wire_facade.go doc
	// comment: genuine OpenAI/Anthropic clients send `Authorization: Bearer
	// sk-...` / `x-api-key: ...`, never this server's session JWT), so a
	// dedicated, wire-compatible API-key middleware
	// (s.wireFacadeAuthMiddleware(), defined below) is applied to both
	// routes instead. §11.4.115 regression guard:
	// llm_generate_helixllm_live_test.go / llm_generate_helixllm_test.go's
	// sibling test file wire_facade_auth_test.go RED-reproduces the
	// pre-fix "unauthenticated -> 200" defect and GREEN-confirms the fix.
	s.router.POST("/v1/chat/completions", s.wireFacadeAuthMiddleware(), s.chatCompletions)
	s.router.POST("/v1/messages", s.wireFacadeAuthMiddleware(), s.anthropicMessages)

	// WebSocket routes
	//
	// SECURITY (confirmed unauthenticated-access finding, closed): this
	// route was registered with ZERO middleware — any client, from any
	// origin, with no credential, could complete the MCP WS handshake.
	// Fixed per Option B of
	// docs/research/07.2026/05_mcp_acp_protocols/WS_ENDPOINT_AUTH_DESIGN.md
	// (§8): a pre-upgrade Bearer/x-api-key gate (s.wsAuthMiddleware(),
	// reusing cfg.Auth.WireFacadeAPIKeys — the same credential class as
	// wireFacadeAuthMiddleware) plus the CheckOrigin allowlist fix in
	// internal/mcp/server.go. Option B was chosen over the ticket-pattern
	// Option A because a repo-wide grep confirmed no browser/JS client
	// (web/frontend/**, applications/**, cmd/**) opens `/ws` today — see
	// this fix's commit message for the STEP-0 fact-check evidence — so
	// the non-browser-lane fix alone is a complete closure for the
	// currently-shipped surface. §11.4.115 regression guard:
	// ws_auth_test.go RED-reproduces the pre-fix "unauthenticated -> 101"
	// defect and GREEN-confirms the fix.
	s.router.GET("/ws", s.wsAuthMiddleware(), s.handleWebSocket)

	// Static file serving for web interface
	s.router.Static("/static", "./web/frontend/static")
	s.router.StaticFile("/", "./web/frontend/index.html")
	s.router.StaticFile("/favicon.ico", "./assets/icons/icon-32x32.png")
}

// Handler methods

func (s *Server) healthCheck(c *gin.Context) {
	// Check database connection
	if s.db != nil {
		if err := s.db.HealthCheck(); err != nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{
				"status":  "error",
				"message": "Database connection failed",
				"error":   err.Error(),
			})
			return
		}
	}

	// Check Redis connection if enabled.
	// Guard s.redis != nil (mirrors the s.db != nil guard above): the server
	// may be constructed with a nil *redis.Client (server.New(cfg, db, nil)),
	// in which case /health must report healthy rather than nil-ptr panic.
	// IsEnabled() is itself nil-receiver-safe; this guard is defense-in-depth.
	if s.redis != nil && s.redis.IsEnabled() {
		if _, err := s.redis.Get(c.Request.Context(), "health_check"); err != nil && err.Error() != "redis: nil" {
			// Try to ping Redis
			if s.redis.GetClient().Ping(c.Request.Context()).Err() != nil {
				c.JSON(http.StatusServiceUnavailable, gin.H{
					"status":  "error",
					"message": "Redis connection failed",
					"error":   err.Error(),
				})
				return
			}
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"status":    "healthy",
		"version":   "1.0.0",
		"timestamp": time.Now().UTC(),
	})
}

func (s *Server) notImplemented(c *gin.Context) {
	c.JSON(http.StatusNotImplemented, gin.H{
		"status":  "error",
		"message": "Not implemented yet",
	})
}

// Middleware

func (s *Server) authMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		// Get token from Authorization header
		authHeader := c.GetHeader("Authorization")
		if authHeader == "" {
			c.JSON(http.StatusUnauthorized, gin.H{
				"status":  "error",
				"message": "Authorization header required",
			})
			c.Abort()
			return
		}

		// Check for Bearer token
		const bearerPrefix = "Bearer "
		if len(authHeader) <= len(bearerPrefix) || authHeader[:len(bearerPrefix)] != bearerPrefix {
			c.JSON(http.StatusUnauthorized, gin.H{
				"status":  "error",
				"message": "Invalid authorization header format",
			})
			c.Abort()
			return
		}

		token := authHeader[len(bearerPrefix):]

		// Verify JWT token AND fetch the complete user from the database.
		//
		// VerifyJWT (the cheap variant) returns a stub User with ONLY
		// {ID, Username, Email} populated from JWT claims — every other
		// field (IsActive, IsVerified, MFAEnabled, DisplayName, LastLogin,
		// timestamps) is zero-valued. That stub then propagates into
		// /api/v1/users/me responses with `is_active:false` and
		// `created_at:"0001-01-01T00:00:00Z"`, which is a CONST-035 bluff:
		// callers think the user was "created in year 0001" or is "not
		// active" while in reality the DB has the right state.
		//
		// VerifyJWTWithDB performs a single indexed UUID lookup AND
		// rejects deactivated accounts (defense-in-depth — a JWT issued
		// before account deactivation must not continue to authenticate).
		user, err := s.auth.VerifyJWTWithDB(c.Request.Context(), token)
		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{
				"status":  "error",
				"message": "Invalid or expired token",
				"error":   err.Error(),
			})
			c.Abort()
			return
		}

		// Set user in context
		c.Set("user", user)
		c.Next()
	}
}

// wireFacadeAuthMiddleware authenticates the OpenAI-compatible
// (POST /v1/chat/completions) and Anthropic-compatible (POST /v1/messages)
// wire-facade routes registered in wire_facade.go.
//
// This is a DELIBERATELY SEPARATE check from authMiddleware(): genuine
// OpenAI/Anthropic SDK clients pointed at this server via OPENAI_BASE_URL /
// ANTHROPIC_BASE_URL send an API key, not this server's internal-user JWT —
// `Authorization: Bearer sk-...` (OpenAI wire convention) or `x-api-key: ...`
// (Anthropic wire convention, see wire_facade.go's file-level doc-comment).
// Wiring authMiddleware()/VerifyJWTWithDB here would reject every genuine
// client and defeat the wire-compatibility this facade exists to provide.
//
// Pattern reused, not reinvented (§11.4.74 extend-don't-reimplement): this
// mirrors submodules/helix_llm's internal/gateway/middleware.APIKeyAuth,
// which authenticated HelixLLM's own OpenAI-compatible gateway the same way
// (DZ-05 fix, commit 60b8e4a) — Bearer/x-api-key token compared against a
// configured, comma-separated key list.
//
// Fail-closed by design (unlike APIKeyAuth's "empty keys => open access"
// convenience mode): cfg.Auth.WireFacadeAPIKeys defaults to "" in every
// shipped config, and these routes drive real, paid-provider LLM calls
// (CONST-035/BLUFF-001) that were the subject of the security finding this
// middleware closes — an operator MUST explicitly configure at least one key
// (HELIX_WIRE_FACADE_API_KEYS or auth.wire_facade_api_keys) before either
// route accepts any request. No key configured => every request, even one
// carrying a bearer token, is rejected with 401.
func (s *Server) wireFacadeAuthMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		configuredKeys := ""
		if s.config != nil {
			configuredKeys = s.config.Auth.WireFacadeAPIKeys
		}

		// Extract the caller-supplied token from either accepted wire
		// convention. `Authorization: Bearer <key>` first (OpenAI + generic
		// wire clients), falling back to the Anthropic-native `x-api-key`
		// header used by Claude Code and other Anthropic SDK clients.
		token := ""
		if authHeader := c.GetHeader("Authorization"); authHeader != "" {
			const bearerPrefix = "Bearer "
			if len(authHeader) > len(bearerPrefix) && authHeader[:len(bearerPrefix)] == bearerPrefix {
				token = authHeader[len(bearerPrefix):]
			}
		}
		if token == "" {
			token = c.GetHeader("x-api-key")
		}

		if token == "" || configuredKeys == "" {
			c.JSON(http.StatusUnauthorized, gin.H{
				"error": gin.H{
					"message": "missing or invalid API key: provide 'Authorization: Bearer <key>' or 'x-api-key: <key>'",
					"type":    "authentication_error",
				},
			})
			c.Abort()
			return
		}

		for _, configured := range strings.Split(configuredKeys, ",") {
			configured = strings.TrimSpace(configured)
			// CONST-042 defence in depth: never let a shipped placeholder
			// authenticate. .env.example ships this key EMPTY and setup.sh
			// generates a unique per-install secret, but a hand-assembled .env
			// (or one copied from an older example that carried
			// CHANGE_ME_wire_facade_key) would otherwise arm a PUBLISHED
			// credential on routes that drive real LLM calls — and
			// server.address ships as 0.0.0.0. Refusing the placeholder here
			// keeps the documented fail-closed posture true on every path into
			// config, not just the setup.sh one.
			if configured == "" || strings.HasPrefix(configured, "CHANGE_ME") {
				continue
			}
			if configured == token {
				c.Set("wire_facade_api_key", token)
				c.Next()
				return
			}
		}

		c.JSON(http.StatusUnauthorized, gin.H{
			"error": gin.H{
				"message": "invalid API key",
				"type":    "authentication_error",
			},
		})
		c.Abort()
	}
}

// wsAuthMiddleware gates the /ws MCP WebSocket route (non-browser lane —
// Option B of
// docs/research/07.2026/05_mcp_acp_protocols/WS_ENDPOINT_AUTH_DESIGN.md §6/§8)
// with the SAME Bearer/x-api-key check as wireFacadeAuthMiddleware, reusing
// cfg.Auth.WireFacadeAPIKeys as the credential source (§11.4.74
// extend-don't-reimplement: extends the existing wire-facade credential
// class rather than inventing a new standing secret). Runs BEFORE
// websocket.Upgrader.Upgrade(), so an unauthenticated caller never
// completes the WS handshake, gets a plain HTTP 401, and never spawns an
// MCP session goroutine — closing the design doc's §3.1
// resource-exhaustion/DoS finding as a byproduct (no auth, no upgrade, no
// goroutine, no session-map entry).
//
// Fail-closed, identical semantics to wireFacadeAuthMiddleware: an empty
// cfg.Auth.WireFacadeAPIKeys (the zero-value default in every shipped
// config) rejects EVERY request, even one carrying a bearer token — an
// operator must explicitly configure at least one key
// (HELIX_WIRE_FACADE_API_KEYS or auth.wire_facade_api_keys) before /ws
// accepts any connection.
//
// Non-browser MCP SDK / CLI clients (Go/Python/Node MCP client libraries,
// curl-style test harnesses, gorilla/websocket dialers) CAN set arbitrary
// headers on the pre-upgrade HTTP request, so this check works for them
// unmodified — unlike a genuine browser `WebSocket()` client, which cannot
// set Authorization/x-api-key on the handshake at all (see the design doc
// §4). STEP-0 fact-check (repo-wide grep of web/frontend/**,
// applications/**, cmd/** for `new WebSocket`/`ws://`/`wss://`/`/ws`)
// confirmed no browser/JS client opens `/ws` in this codebase today, so
// this non-browser-lane fix alone is a complete closure of the confirmed
// finding for the currently-shipped surface.
func (s *Server) wsAuthMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		configuredKeys := ""
		if s.config != nil {
			configuredKeys = s.config.Auth.WireFacadeAPIKeys
		}

		token := ""
		if authHeader := c.GetHeader("Authorization"); authHeader != "" {
			const bearerPrefix = "Bearer "
			if len(authHeader) > len(bearerPrefix) && authHeader[:len(bearerPrefix)] == bearerPrefix {
				token = authHeader[len(bearerPrefix):]
			}
		}
		if token == "" {
			token = c.GetHeader("x-api-key")
		}

		if token == "" || configuredKeys == "" {
			c.JSON(http.StatusUnauthorized, gin.H{
				"error": gin.H{
					"message": "missing or invalid API key: provide 'Authorization: Bearer <key>' or 'x-api-key: <key>' before opening the /ws connection",
					"type":    "authentication_error",
				},
			})
			c.Abort()
			return
		}

		for _, configured := range strings.Split(configuredKeys, ",") {
			if strings.TrimSpace(configured) == token {
				c.Set("ws_api_key", token)
				c.Next()
				return
			}
		}

		c.JSON(http.StatusUnauthorized, gin.H{
			"error": gin.H{
				"message": "invalid API key",
				"type":    "authentication_error",
			},
		})
		c.Abort()
	}
}

// handleWebSocket handles WebSocket connections for MCP
func (s *Server) handleWebSocket(c *gin.Context) {
	s.mcp.HandleWebSocket(c.Writer, c.Request)
}

// CORSMiddleware provides CORS headers for cross-origin requests.
//
// SECURITY (fixed — was CVE-class CORS misconfiguration): this middleware
// MUST NEVER combine a wildcard "Access-Control-Allow-Origin: *" with
// "Access-Control-Allow-Credentials: true" — that combination is forbidden
// by the Fetch/CORS spec. Real browsers reject a wildcard Allow-Origin
// whenever credentials mode is "include", and an implementation that
// instead reflects the wildcard as the literal request Origin would let
// ANY origin make credentialed cross-origin requests (a genuine
// cross-origin credential-theft vector).
//
// Only an origin present in allowedOrigins is ever echoed back — as that
// SPECIFIC origin, never "*" — together with "Vary: Origin" (so shared
// caches don't leak one origin's response to another) and only then is
// Allow-Credentials set. Every other request (no Origin header, or an
// Origin not on the allowlist) gets no CORS-allow headers at all
// (default-deny); same-origin requests are unaffected since browsers don't
// consult CORS headers for those.
//
// allowedOrigins is populated from cfg.Auth.CORSAllowedOrigins /
// HELIX_CORS_ALLOWED_ORIGINS (§11.4.74 reuse of the WSAllowedOrigins
// allowlist pattern used by the /ws MCP WebSocket upgrader).
func CORSMiddleware(allowedOrigins []string) gin.HandlerFunc {
	allowed := make(map[string]struct{}, len(allowedOrigins))
	for _, o := range allowedOrigins {
		o = strings.TrimSpace(o)
		if o != "" {
			allowed[o] = struct{}{}
		}
	}

	return func(c *gin.Context) {
		if origin := c.Request.Header.Get("Origin"); origin != "" {
			if _, ok := allowed[origin]; ok {
				c.Writer.Header().Set("Access-Control-Allow-Origin", origin)
				c.Writer.Header().Add("Vary", "Origin")
				c.Writer.Header().Set("Access-Control-Allow-Credentials", "true")
			}
		}
		c.Writer.Header().Set("Access-Control-Allow-Headers", "Content-Type, Content-Length, Accept-Encoding, X-CSRF-Token, Authorization, accept, origin, Cache-Control, X-Requested-With")
		c.Writer.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS, GET, PUT, DELETE")

		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(204)
			return
		}

		c.Next()
	}
}

// SecurityMiddleware provides security headers
func SecurityMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Writer.Header().Set("X-Content-Type-Options", "nosniff")
		c.Writer.Header().Set("X-Frame-Options", "DENY")
		c.Writer.Header().Set("X-XSS-Protection", "1; mode=block")
		c.Writer.Header().Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
		c.Next()
	}
}
