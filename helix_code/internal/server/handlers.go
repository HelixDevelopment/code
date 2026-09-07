package server

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"runtime"
	"time"

	"dev.helix.code/internal/auth"
	"dev.helix.code/internal/llm"
	"dev.helix.code/internal/project"
	"dev.helix.code/internal/session"
	"dev.helix.code/internal/task"
	"dev.helix.code/internal/verifier"
	"dev.helix.code/internal/worker"
	"dev.helix.code/internal/workflow"
	"github.com/gin-gonic/gin"
)

// respondInvalidID returns true and writes a 400 Bad Request response
// if err is a "malformed id" sentinel from any manager package. Used
// at every handler that accepts :id parameters — pre-fix these all
// returned 500 for what is plainly a 400 client-input error
// (CONST-035 wrong-HTTP-code).
func respondInvalidID(c *gin.Context, err error, what string) bool {
	switch {
	case errors.Is(err, task.ErrInvalidTaskID):
		c.JSON(http.StatusBadRequest, gin.H{
			"status":  "error",
			"message": tr(reqCtx(c), "internal_server_invalid_task_id", nil),
			"error":   err.Error(),
		})
		return true
	case errors.Is(err, worker.ErrInvalidWorkerID):
		c.JSON(http.StatusBadRequest, gin.H{
			"status":  "error",
			"message": tr(reqCtx(c), "internal_server_invalid_worker_id", nil),
			"error":   err.Error(),
		})
		return true
	case errors.Is(err, project.ErrInvalidProjectID):
		c.JSON(http.StatusBadRequest, gin.H{
			"status":  "error",
			"message": tr(reqCtx(c), "internal_server_invalid_project_id", nil),
			"error":   err.Error(),
		})
		return true
	case errors.Is(err, project.ErrInvalidOwnerID):
		c.JSON(http.StatusBadRequest, gin.H{
			"status":  "error",
			"message": tr(reqCtx(c), "internal_server_invalid_owner_id", nil),
			"error":   err.Error(),
		})
		return true
	}
	_ = what
	return false
}

// requireUserID extracts the authenticated *auth.User stashed by
// authMiddleware under context key "user" and returns its ID as a string.
// On any failure it writes the appropriate error response (401 if the
// middleware didn't run / the value is absent, 500 if the stored value is
// the wrong type) and returns ok=false so the caller returns immediately.
//
// Centralises the user-from-context dance that getProject / updateProject /
// deleteProject / getProjectSessions need for IDOR owner-scoping (and that
// listProjects / createProject already do inline).
func (s *Server) requireUserID(c *gin.Context) (string, bool) {
	userValue, exists := c.Get("user")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{
			"status":  "error",
			"message": tr(reqCtx(c), "internal_server_authentication_required", nil),
			"error":   "user not found in context - please authenticate first",
		})
		return "", false
	}
	user, ok := userValue.(*auth.User)
	if !ok {
		c.JSON(http.StatusInternalServerError, gin.H{
			"status":  "error",
			"message": tr(reqCtx(c), "internal_server_invalid_user_context", nil),
		})
		return "", false
	}
	return user.ID.String(), true
}

// enforceProjectOwner verifies the authenticated requester owns the project
// :id and returns true only if they do. On mismatch (or genuinely-missing
// project) it writes a 404 — owned-by-someone-else and not-found are
// indistinguishable to the caller (GetProjectForUser scopes by owner in the
// query), so project existence is never leaked. Malformed UUIDs surface as
// 400 via respondInvalidID. This is the shared IDOR gate for the mutating /
// sub-resource by-id project handlers (update / delete / sessions); getProject
// uses GetProjectForUser directly as both gate and fetch.
func (s *Server) enforceProjectOwner(c *gin.Context, projectID, userID string) bool {
	if _, err := s.projectManager.GetProjectForUser(c.Request.Context(), projectID, userID); err != nil {
		if respondInvalidID(c, err, "project") {
			return false
		}
		// ErrProjectNotFound and any other lookup error both resolve to a
		// 404 here — we never expose a 500 that would confirm existence.
		c.JSON(http.StatusNotFound, gin.H{
			"status":  "error",
			"message": tr(reqCtx(c), "internal_server_project_not_found", nil),
			"error":   err.Error(),
		})
		return false
	}
	return true
}

// Project Handlers

func (s *Server) listProjects(c *gin.Context) {
	// Pull user from context — authMiddleware stores the full *auth.User
	// under key "user", not a string "user_id". The previous version
	// looked up "user_id" via c.GetString, which is never set, so EVERY
	// authenticated request to GET /api/v1/projects returned 401 even
	// with a valid JWT. Real production bug: the canonical "list my
	// projects" endpoint was unreachable for the entire deployment.
	userValue, exists := c.Get("user")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{
			"status":  "error",
			"message": tr(reqCtx(c), "internal_server_authentication_required", nil),
			"error":   "user not found in context - please authenticate first",
		})
		return
	}
	user, ok := userValue.(*auth.User)
	if !ok {
		c.JSON(http.StatusInternalServerError, gin.H{
			"status":  "error",
			"message": tr(reqCtx(c), "internal_server_invalid_user_context", nil),
		})
		return
	}
	userID := user.ID.String()

	projects, err := s.projectManager.ListProjects(c.Request.Context(), userID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"status":  "error",
			"message": tr(reqCtx(c), "internal_server_failed_list_projects", nil),
			"error":   err.Error(),
		})
		return
	}

	// JSON contract: list endpoint MUST return an array, not null.
	// Same nil-slice→null serialization bluff as listTasks.
	if projects == nil {
		projects = []*project.Project{}
	}

	c.JSON(http.StatusOK, gin.H{
		"status":   "success",
		"projects": projects,
	})
}

// Auth Handlers

func (s *Server) register(c *gin.Context) {
	var req struct {
		Username    string `json:"username" binding:"required"`
		Email       string `json:"email" binding:"required"`
		Password    string `json:"password" binding:"required"`
		DisplayName string `json:"display_name"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"status":  "error",
			"message": tr(reqCtx(c), "internal_server_invalid_request", nil),
			"error":   err.Error(),
		})
		return
	}

	user, err := s.auth.Register(c.Request.Context(), req.Username, req.Email, req.Password, req.DisplayName)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"status":  "error",
			"message": tr(reqCtx(c), "internal_server_registration_failed", nil),
			"error":   err.Error(),
		})
		return
	}

	c.JSON(http.StatusCreated, gin.H{
		"status": "success",
		"user":   user,
	})
}

func (s *Server) login(c *gin.Context) {
	var req struct {
		Username string `json:"username" binding:"required"`
		Password string `json:"password" binding:"required"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"status":  "error",
			"message": tr(reqCtx(c), "internal_server_invalid_request", nil),
			"error":   err.Error(),
		})
		return
	}

	session, user, err := s.auth.Login(c.Request.Context(), req.Username, req.Password, "rest_api", c.ClientIP(), c.GetHeader("User-Agent"))
	if err != nil {
		// HXC-043: when the auth backend is entirely unavailable (server
		// booted with db=nil), Login returns ErrInvalidCredentials so this
		// stays a clean 401 "Login failed" rather than the pre-fix empty
		// HTTP 500 panic. If a future caller surfaces the explicit
		// backend-unavailable sentinel, map it to 503 (service down) — the
		// only deviation from the existing 401 mapping, and only for that
		// distinct condition.
		if errors.Is(err, auth.ErrAuthBackendUnavailable) {
			c.JSON(http.StatusServiceUnavailable, gin.H{
				"status":  "error",
				"message": "Login failed",
				"error":   err.Error(),
			})
			return
		}
		c.JSON(http.StatusUnauthorized, gin.H{
			"status":  "error",
			"message": "Login failed",
			"error":   err.Error(),
		})
		return
	}

	// Generate JWT token
	token, err := s.auth.GenerateJWT(user)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"status":  "error",
			"message": tr(reqCtx(c), "internal_server_failed_generate_token", nil),
			"error":   err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"status":  "success",
		"user":    user,
		"token":   token,
		"session": session,
	})
}

func (s *Server) logout(c *gin.Context) {
	// Get token from Authorization header
	authHeader := c.GetHeader("Authorization")
	if authHeader == "" || len(authHeader) <= 7 || authHeader[:7] != "Bearer " {
		c.JSON(http.StatusBadRequest, gin.H{
			"status":  "error",
			"message": tr(reqCtx(c), "internal_server_invalid_authorization_header", nil),
		})
		return
	}

	token := authHeader[7:]

	// For JWT, we can't invalidate it server-side, but we can log the logout
	// In a production system, you might want to maintain a blacklist
	if err := s.auth.Logout(c.Request.Context(), token); err != nil {
		// Log error but don't fail the request
		c.Error(err)
	}

	c.JSON(http.StatusOK, gin.H{
		"status":  "success",
		"message": tr(reqCtx(c), "internal_server_logged_out_successfully", nil),
	})
}

func (s *Server) refreshToken(c *gin.Context) {
	// /auth/refresh is registered in the /auth group which has NO
	// authMiddleware (register/login/logout must remain publicly
	// reachable). The previous version called c.Get("user") expecting
	// the middleware to have populated it — so it returned 401 "User
	// not authenticated" for EVERY caller, even those with a perfectly
	// valid Bearer token. Same context-key bug pattern as listProjects
	// (BUG #3) but in the auth group instead of projects.
	//
	// Fix: manually parse + verify the Authorization header (mirrors
	// the logout handler's pattern). Then re-fetch the full user from
	// the database to issue a NEW JWT (the old token's claims are
	// trusted for identification, not for state like is_active —
	// VerifyJWTWithDB handles both).
	authHeader := c.GetHeader("Authorization")
	if authHeader == "" || len(authHeader) <= 7 || authHeader[:7] != "Bearer " {
		c.JSON(http.StatusUnauthorized, gin.H{
			"status":  "error",
			"message": tr(reqCtx(c), "internal_server_authorization_header_required", nil),
		})
		return
	}
	user, err := s.auth.VerifyJWTWithDB(c.Request.Context(), authHeader[7:])
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{
			"status":  "error",
			"message": tr(reqCtx(c), "internal_server_invalid_or_expired_token", nil),
			"error":   err.Error(),
		})
		return
	}

	// Generate new JWT token
	token, err := s.auth.GenerateJWT(user)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"status":  "error",
			"message": tr(reqCtx(c), "internal_server_failed_generate_token", nil),
			"error":   err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"status": "success",
		"token":  token,
	})
}

func (s *Server) getCurrentUser(c *gin.Context) {
	// Get current user from context (set by auth middleware)
	userValue, exists := c.Get("user")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{
			"status":  "error",
			"message": tr(reqCtx(c), "internal_server_user_not_authenticated", nil),
		})
		return
	}

	user, ok := userValue.(*auth.User)
	if !ok {
		c.JSON(http.StatusInternalServerError, gin.H{
			"status":  "error",
			"message": tr(reqCtx(c), "internal_server_invalid_user_context", nil),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"status": "success",
		"user":   user,
	})
}

func (s *Server) createProject(c *gin.Context) {
	var req struct {
		Name        string `json:"name" binding:"required"`
		Description string `json:"description"`
		Path        string `json:"path" binding:"required"`
		Type        string `json:"type"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"status":  "error",
			"message": tr(reqCtx(c), "internal_server_invalid_request", nil),
			"error":   err.Error(),
		})
		return
	}

	// Pull authenticated user — projects are owned, not anonymous.
	// The previous code called s.projectManager.CreateProject(...) which
	// hardcoded "default-user" as ownerID (manager_db.go:135), failing
	// every real request with "invalid UUID length: 12" at the database
	// layer (the string "default-user" is 12 chars). CONST-035 / BLUFF-002
	// territory: the convenience wrapper SHIPPED a fabricated default.
	userValue, exists := c.Get("user")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{
			"status":  "error",
			"message": tr(reqCtx(c), "internal_server_authentication_required", nil),
		})
		return
	}
	user, ok := userValue.(*auth.User)
	if !ok {
		c.JSON(http.StatusInternalServerError, gin.H{
			"status":  "error",
			"message": tr(reqCtx(c), "internal_server_invalid_user_context", nil),
		})
		return
	}

	// Create project directory if it doesn't exist
	if err := os.MkdirAll(req.Path, 0755); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"status":  "error",
			"message": tr(reqCtx(c), "internal_server_failed_create_project_directory", nil),
			"error":   err.Error(),
		})
		return
	}

	proj, err := s.projectManager.CreateProjectWithUser(
		c.Request.Context(), req.Name, req.Description, req.Path, req.Type, user.ID.String())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"status":  "error",
			"message": tr(reqCtx(c), "internal_server_failed_create_project", nil),
			"error":   err.Error(),
		})
		return
	}

	c.JSON(http.StatusCreated, gin.H{
		"status":  "success",
		"project": proj,
	})
}

func (s *Server) getProject(c *gin.Context) {
	id := c.Param("id")

	// IDOR fix: scope the fetch to the authenticated owner. A project owned
	// by someone else returns the SAME 404 as a missing one (no existence
	// leak) — see project.DatabaseManager.GetProjectForUser.
	userID, ok := s.requireUserID(c)
	if !ok {
		return
	}

	proj, err := s.projectManager.GetProjectForUser(c.Request.Context(), id, userID)
	if err != nil {
		// 400 for a malformed UUID; 404 for not-found / not-owned.
		if respondInvalidID(c, err, "project") {
			return
		}
		c.JSON(http.StatusNotFound, gin.H{
			"status":  "error",
			"message": tr(reqCtx(c), "internal_server_project_not_found", nil),
			"error":   err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"status":  "success",
		"project": proj,
	})
}

func (s *Server) updateProject(c *gin.Context) {
	id := c.Param("id")

	var req struct {
		Name        string `json:"name"`
		Description string `json:"description"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"status":  "error",
			"message": tr(reqCtx(c), "internal_server_invalid_request", nil),
			"error":   err.Error(),
		})
		return
	}

	if s.projectManager == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"status":  "error",
			"message": tr(reqCtx(c), "internal_server_project_manager_unavailable", nil),
			"error":   "database connection required for project management",
		})
		return
	}

	// IDOR fix: only the owner may mutate the project. Gate on ownership
	// BEFORE the UPDATE (404 on mismatch — no existence leak). Without this a
	// user could rename another user's project by id.
	userID, ok := s.requireUserID(c)
	if !ok {
		return
	}
	if !s.enforceProjectOwner(c, id, userID) {
		return
	}

	// Update the project using the project manager
	proj, err := s.projectManager.UpdateProject(c.Request.Context(), id, req.Name, req.Description)
	if err != nil {
		// 400 malformed UUID; 404 not-found; 500 only for DB faults.
		if respondInvalidID(c, err, "project") {
			return
		}
		if errors.Is(err, project.ErrProjectNotFound) {
			c.JSON(http.StatusNotFound, gin.H{
				"status":  "error",
				"message": tr(reqCtx(c), "internal_server_project_not_found", nil),
				"error":   err.Error(),
			})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{
			"status":  "error",
			"message": tr(reqCtx(c), "internal_server_failed_update_project", nil),
			"error":   err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"status":  "success",
		"project": proj,
	})
}

func (s *Server) deleteProject(c *gin.Context) {
	id := c.Param("id")

	if s.projectManager == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"status":  "error",
			"message": tr(reqCtx(c), "internal_server_project_manager_unavailable", nil),
			"error":   "database connection required for project management",
		})
		return
	}

	// IDOR fix: only the owner may delete the project. Gate on ownership
	// BEFORE the soft-delete (404 on mismatch — no existence leak).
	userID, ok := s.requireUserID(c)
	if !ok {
		return
	}
	if !s.enforceProjectOwner(c, id, userID) {
		return
	}

	err := s.projectManager.DeleteProject(c.Request.Context(), id)
	if err != nil {
		// 400 malformed UUID; 404 not-found; 500 only for DB faults.
		if respondInvalidID(c, err, "project") {
			return
		}
		if errors.Is(err, project.ErrProjectNotFound) {
			c.JSON(http.StatusNotFound, gin.H{
				"status":  "error",
				"message": tr(reqCtx(c), "internal_server_project_not_found", nil),
				"error":   err.Error(),
			})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{
			"status":  "error",
			"message": tr(reqCtx(c), "internal_server_failed_delete_project", nil),
			"error":   err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"status":  "success",
		"message": tr(reqCtx(c), "internal_server_project_deleted", nil),
	})
}

// Task Handlers

func (s *Server) listTasks(c *gin.Context) {
	if s.taskManager == nil {
		c.JSON(http.StatusOK, gin.H{
			"status": "success",
			"tasks":  []interface{}{},
		})
		return
	}

	tasks, err := s.taskManager.ListTasks(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"status":  "error",
			"message": tr(reqCtx(c), "internal_server_failed_list_tasks", nil),
			"error":   err.Error(),
		})
		return
	}

	// JSON contract: a list endpoint MUST return an array, not null.
	// `s.taskManager.ListTasks` returns a nil slice when the table is
	// empty, which Go's json package serializes as `null`. Callers
	// expecting `tasks: []` will crash on `null`. Normalize here.
	if tasks == nil {
		tasks = []*task.Task{}
	}

	c.JSON(http.StatusOK, gin.H{
		"status": "success",
		"tasks":  tasks,
	})
}

func (s *Server) createTask(c *gin.Context) {
	var req struct {
		Name         string                 `json:"name" binding:"required"`
		Description  string                 `json:"description"`
		Type         string                 `json:"type" binding:"required"`
		Priority     string                 `json:"priority"`
		Parameters   map[string]interface{} `json:"parameters"`
		Dependencies []string               `json:"dependencies"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"status":  "error",
			"message": tr(reqCtx(c), "internal_server_invalid_request", nil),
			"error":   err.Error(),
		})
		return
	}

	// Use database manager if available
	if s.taskManager != nil {
		priority := req.Priority
		if priority == "" {
			priority = "normal"
		}

		task, err := s.taskManager.CreateTask(
			c.Request.Context(),
			req.Name,
			req.Description,
			req.Type,
			priority,
			req.Parameters,
			req.Dependencies,
		)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"status":  "error",
				"message": tr(reqCtx(c), "internal_server_failed_create_task", nil),
				"error":   err.Error(),
			})
			return
		}

		c.JSON(http.StatusCreated, gin.H{
			"status": "success",
			"task":   task,
		})
		return
	}

	// Task manager not available - return service unavailable error
	c.JSON(http.StatusServiceUnavailable, gin.H{
		"status":  "error",
		"message": tr(reqCtx(c), "internal_server_task_manager_unavailable", nil),
		"error":   "database connection required for task management",
	})
}

func (s *Server) getTask(c *gin.Context) {
	id := c.Param("id")

	if s.taskManager != nil {
		task, err := s.taskManager.GetTask(c.Request.Context(), id)
		if err != nil {
			c.JSON(http.StatusNotFound, gin.H{
				"status":  "error",
				"message": tr(reqCtx(c), "internal_server_task_not_found", nil),
				"error":   err.Error(),
			})
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"status": "success",
			"task":   task,
		})
		return
	}

	// Task manager not available - return service unavailable error
	c.JSON(http.StatusServiceUnavailable, gin.H{
		"status":  "error",
		"message": tr(reqCtx(c), "internal_server_task_manager_unavailable", nil),
		"error":   "database connection required for task management",
	})
}

func (s *Server) updateTask(c *gin.Context) {
	id := c.Param("id")

	var req struct {
		Status       string                 `json:"status"`
		Result       map[string]interface{} `json:"result"`
		ErrorMessage string                 `json:"error_message"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"status":  "error",
			"message": tr(reqCtx(c), "internal_server_invalid_request", nil),
			"error":   err.Error(),
		})
		return
	}

	if s.taskManager != nil {
		var err error
		switch req.Status {
		case "running":
			err = s.taskManager.StartTask(c.Request.Context(), id)
		case "completed":
			err = s.taskManager.CompleteTask(c.Request.Context(), id, req.Result)
		case "failed":
			err = s.taskManager.FailTask(c.Request.Context(), id, req.ErrorMessage)
		default:
			c.JSON(http.StatusBadRequest, gin.H{
				"status":  "error",
				"message": tr(reqCtx(c), "internal_server_invalid_task_status", nil),
			})
			return
		}

		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"status":  "error",
				"message": tr(reqCtx(c), "internal_server_failed_update_task", nil),
				"error":   err.Error(),
			})
			return
		}

		// Get the updated task
		task, err := s.taskManager.GetTask(c.Request.Context(), id)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"status":  "error",
				"message": tr(reqCtx(c), "internal_server_task_updated_failed_retrieve", nil),
				"error":   err.Error(),
			})
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"status": "success",
			"task":   task,
		})
		return
	}

	// Task manager not available - return service unavailable error
	c.JSON(http.StatusServiceUnavailable, gin.H{
		"status":  "error",
		"message": tr(reqCtx(c), "internal_server_task_manager_unavailable", nil),
		"error":   "database connection required for task management",
	})
}

func (s *Server) deleteTask(c *gin.Context) {
	id := c.Param("id")

	if s.taskManager == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"status":  "error",
			"message": tr(reqCtx(c), "internal_server_task_manager_unavailable", nil),
			"error":   "database connection required for task management",
		})
		return
	}

	err := s.taskManager.DeleteTask(c.Request.Context(), id)
	if err != nil {
		// 400 for malformed UUID; 404 for missing resource; 500 only
		// for genuine DB faults.
		if respondInvalidID(c, err, "task") {
			return
		}
		if errors.Is(err, task.ErrTaskNotFound) {
			c.JSON(http.StatusNotFound, gin.H{
				"status":  "error",
				"message": tr(reqCtx(c), "internal_server_task_not_found", nil),
				"error":   err.Error(),
			})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{
			"status":  "error",
			"message": tr(reqCtx(c), "internal_server_failed_delete_task", nil),
			"error":   err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"status":  "success",
		"message": tr(reqCtx(c), "internal_server_task_deleted", nil),
	})
}

// Worker Handlers

func (s *Server) listWorkers(c *gin.Context) {
	if s.workerManager == nil {
		c.JSON(http.StatusOK, gin.H{
			"status":  "success",
			"workers": []interface{}{},
		})
		return
	}

	workers, err := s.workerManager.ListWorkers(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"status":  "error",
			"message": tr(reqCtx(c), "internal_server_failed_list_workers", nil),
			"error":   err.Error(),
		})
		return
	}

	// JSON contract: list endpoint MUST return an array, not null.
	// Same nil-slice→null serialization bluff as listTasks / listProjects.
	if workers == nil {
		workers = []*worker.Worker{}
	}

	c.JSON(http.StatusOK, gin.H{
		"status":  "success",
		"workers": workers,
	})
}

func (s *Server) getWorker(c *gin.Context) {
	id := c.Param("id")

	if s.workerManager != nil {
		worker, err := s.workerManager.GetWorker(c.Request.Context(), id)
		if err != nil {
			c.JSON(http.StatusNotFound, gin.H{
				"status":  "error",
				"message": tr(reqCtx(c), "internal_server_worker_not_found", nil),
				"error":   err.Error(),
			})
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"status": "success",
			"worker": worker,
		})
		return
	}

	// Worker manager not available - return service unavailable error
	c.JSON(http.StatusServiceUnavailable, gin.H{
		"status":  "error",
		"message": tr(reqCtx(c), "internal_server_worker_manager_unavailable", nil),
		"error":   "database connection required for worker management",
	})
}

// System Handlers

func (s *Server) getSystemStats(c *gin.Context) {
	// Initialize counters
	var (
		totalTasks     = 0
		pendingTasks   = 0
		runningTasks   = 0
		completedTasks = 0
		failedTasks    = 0
		totalWorkers   = 0
		activeWorkers  = 0
	)

	// Get task statistics if task manager is available
	if s.taskManager != nil {
		tasks, err := s.taskManager.ListTasks(c.Request.Context())
		if err == nil {
			totalTasks = len(tasks)
			// Count tasks by status
			for _, t := range tasks {
				switch string(t.Status) {
				case "pending":
					pendingTasks++
				case "running":
					runningTasks++
				case "completed":
					completedTasks++
				case "failed":
					failedTasks++
				}
			}
		}
	}

	// Get worker statistics if worker manager is available
	if s.workerManager != nil {
		workers, err := s.workerManager.ListWorkers(c.Request.Context())
		if err == nil {
			totalWorkers = len(workers)
			// Count active workers
			for _, w := range workers {
				if string(w.Status) == "active" {
					activeWorkers++
				}
			}
		}
	}

	// Calculate uptime
	uptime := time.Since(s.startTime)

	stats := gin.H{
		"tasks": gin.H{
			"total":     totalTasks,
			"pending":   pendingTasks,
			"running":   runningTasks,
			"completed": completedTasks,
			"failed":    failedTasks,
		},
		"workers": gin.H{
			"total":  totalWorkers,
			"active": activeWorkers,
		},
		"system": gin.H{
			"uptime": uptime.String(),
		},
	}

	c.JSON(http.StatusOK, gin.H{
		"status": "success",
		"stats":  stats,
	})
}

func (s *Server) getSystemStatus(c *gin.Context) {
	// Check database connection.
	//
	// s.db may be nil — the server is constructible without a database
	// (server.New(cfg, nil, rds); auth/managers are all guarded with
	// `if db != nil`). The previous code called s.db.HealthCheck()
	// unconditionally, which is a nil-RECEIVER dereference (panic) when
	// s.db == nil — (*Database).HealthCheck guards a nil *pool* but a
	// method call on a nil *Database receiver still dereferences the
	// receiver to read db.Pool and SIGSEGVs. Every sibling endpoint
	// (healthCheck, getServerInfo) already guards `s.db != nil`; this one
	// did not. Report "disabled" when no database is wired, matching the
	// getServerInfo `"enabled": s.db != nil` contract, rather than crash
	// the handler (CONST-035 / Article XI §11.9 — a status endpoint must
	// report status, not panic).
	dbStatus := "disabled"
	if s.db != nil {
		dbStatus = "healthy"
		if err := s.db.HealthCheck(); err != nil {
			dbStatus = "unhealthy"
		}
	}

	status := gin.H{
		"database": dbStatus,
		"api":      "healthy",
		"version":  "1.0.0",
	}

	c.JSON(http.StatusOK, gin.H{
		"status": "success",
		"system": status,
	})
}

// Workflow Handlers

// workflowError maps a workflow-executor error to the right HTTP code.
// Pre-fix the 4 workflow handlers all returned 500 even when the
// underlying cause was "project doesn't exist" (404 territory) — same
// CONST-035 misclassification family as the round-20 bugs. Centralized
// helper avoids duplicating the errors.Is branch in 4 places.
func workflowError(c *gin.Context, err error, action string) {
	if errors.Is(err, project.ErrProjectNotFound) {
		c.JSON(http.StatusNotFound, gin.H{
			"status":  "error",
			"message": tr(reqCtx(c), "internal_server_project_not_found", nil),
			"error":   err.Error(),
		})
		return
	}
	c.JSON(http.StatusInternalServerError, gin.H{
		"status":  "error",
		"message": "Failed to execute " + action + " workflow",
		"error":   err.Error(),
	})
}

func (s *Server) executePlanningWorkflow(c *gin.Context) {
	projectID := c.Param("projectId")

	workflowExecutor := workflow.NewExecutor(s.projectManager)

	wf, err := workflowExecutor.ExecutePlanningWorkflow(c.Request.Context(), projectID)
	if err != nil {
		workflowError(c, err, "planning")
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"status":   "success",
		"workflow": wf,
	})
}

func (s *Server) executeBuildingWorkflow(c *gin.Context) {
	projectID := c.Param("projectId")

	workflowExecutor := workflow.NewExecutor(s.projectManager)

	wf, err := workflowExecutor.ExecuteBuildingWorkflow(c.Request.Context(), projectID)
	if err != nil {
		workflowError(c, err, "building")
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"status":   "success",
		"workflow": wf,
	})
}

func (s *Server) executeTestingWorkflow(c *gin.Context) {
	projectID := c.Param("projectId")

	workflowExecutor := workflow.NewExecutor(s.projectManager)

	wf, err := workflowExecutor.ExecuteTestingWorkflow(c.Request.Context(), projectID)
	if err != nil {
		workflowError(c, err, "testing")
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"status":   "success",
		"workflow": wf,
	})
}

func (s *Server) executeRefactoringWorkflow(c *gin.Context) {
	projectID := c.Param("projectId")

	workflowExecutor := workflow.NewExecutor(s.projectManager)

	wf, err := workflowExecutor.ExecuteRefactoringWorkflow(c.Request.Context(), projectID)
	if err != nil {
		workflowError(c, err, "refactoring")
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"status":   "success",
		"workflow": wf,
	})
}

// Server Info and Metrics Handlers

// getServerInfo returns server information
func (s *Server) getServerInfo(c *gin.Context) {
	uptime := time.Since(s.startTime)

	info := gin.H{
		"name":       "HelixCode Server",
		"version":    "1.0.0",
		"go_version": "1.24",
		"uptime":     uptime.String(),
		"start_time": s.startTime.UTC().Format(time.RFC3339),
		"database": gin.H{
			"enabled":   s.db != nil,
			"connected": s.db != nil && s.db.HealthCheck() == nil,
		},
		"redis": gin.H{
			"enabled":   s.redis != nil && s.redis.IsEnabled(),
			"connected": s.redis != nil && s.redis.IsEnabled() && s.redis.GetClient().Ping(c.Request.Context()).Err() == nil,
		},
		"features": gin.H{
			"auth_enabled":          s.auth != nil,
			"mcp_enabled":           s.mcp != nil,
			"notifications_enabled": s.notification != nil,
			// streaming_enabled is true because POST /api/v1/llm/stream is a
			// real, registered route backed by s.streamLLM (server.go) that
			// performs genuine provider streaming (llm_generate.go:streamLLM).
			// The capability is always wired for a server built by New, so the
			// flag is honestly constant true (CONST-035 / Article XI §11.9).
			"streaming_enabled": true,
			// plugins_enabled reflects the server's real plugin registry
			// (server.go pluginRegistry). It is non-nil for any server built by
			// New, so the flag is true — the server genuinely owns a plugin
			// registry into which plugins found under the plugin directory are
			// loaded at boot (§11.4.122: advertised only because the capability
			// is real, never a cosmetic flag).
			"plugins_enabled": s.pluginRegistry != nil,
		},
	}

	c.JSON(http.StatusOK, gin.H{
		"status": "success",
		"info":   info,
	})
}

// getMetrics returns server metrics
func (s *Server) getMetrics(c *gin.Context) {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)

	metrics := gin.H{
		"uptime_seconds": time.Since(s.startTime).Seconds(),
		"requests": gin.H{
			"total":  0, // TODO: add request counter middleware
			"active": 0,
		},
		"resources": gin.H{
			"goroutines":      runtime.NumGoroutine(),
			"memory_mb":       float64(m.Alloc) / (1024 * 1024),
			"memory_total_mb": float64(m.TotalAlloc) / (1024 * 1024),
			"gc_count":        m.NumGC,
		},
		"database": gin.H{
			"connections_active": 0,
			"connections_idle":   0,
		},
	}

	// Database stats if available
	if s.db != nil && s.db.Pool != nil {
		stats := s.db.Pool.Stat()
		dbMetrics := metrics["database"].(gin.H)
		dbMetrics["connections_active"] = stats.TotalConns()
		dbMetrics["connections_idle"] = stats.IdleConns()
		dbMetrics["max_connections"] = stats.MaxConns()
	}

	c.JSON(http.StatusOK, gin.H{
		"status":  "success",
		"metrics": metrics,
	})
}

// LLM Handlers

// listLLMProviders returns available LLM providers
func (s *Server) listLLMProviders(c *gin.Context) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
	defer cancel()

	// Priority 1: LLMsVerifier (CONST-036) — providers backed by WORKING models
	// only (D-2/D-4 funnel). A provider whose key is absent has zero working
	// models and therefore MUST NOT be listed as an available provider; listing
	// it would be a §11.4 / CONST-035 PASS-bluff (the user has no key, so the
	// provider is not usable). llm.PresentProviderNames() is the shared
	// key-presence gate.
	if s.verifierResult != nil && s.verifierResult.Adapter.IsEnabled() {
		present := llm.PresentProviderNames()
		models, err := s.verifierResult.Adapter.GetWorkingModels(ctx, present)
		if err == nil && len(models) > 0 {
			providers := buildProvidersFromVerifiedModels(models)
			c.JSON(http.StatusOK, gin.H{
				"status":        "success",
				"providers":     providers,
				"count":         len(providers),
				"source":        "verifier",
				"cloud_enabled": llm.CloudEnabled(),
				"last_updated":  time.Now().UTC(),
			})
			return
		}
	}

	// Priority 2: Constitutional fallback (CONST-035)
	models := verifier.FallbackModels
	providers := buildProvidersFromVerifiedModels(models)
	c.JSON(http.StatusOK, gin.H{
		"status":        "success",
		"providers":     providers,
		"count":         len(providers),
		"source":        "fallback",
		"cloud_enabled": llm.CloudEnabled(),
		"last_updated":  time.Now().UTC(),
	})
}

// isLocalProviderID reports whether provider (the raw provider-identity
// string carried on a verifier.VerifiedModel — e.g. "ollama", "deepseek",
// "openai") names one of the local-construction provider types.
//
// Anti-bluff (§11.4): this MUST classify identically to internal/llm's
// unexported isLocalProviderType (NewCloudProvider's cloud-gate exemption
// list — currently only ProviderTypeOllama / ProviderTypeLlamaCpp), so the
// status this handler reports can never disagree with what construction
// will actually do. There is no exported helper in internal/llm to call
// directly today (isLocalProviderType is unexported); if/when one lands
// (see the concurrent internal/llm endpoint-locality-predicate work), this
// function should be replaced with a call to it instead of kept as a
// second copy of the list. Locality is deliberately NOT derived from
// VerifiedModel.OpenSource — that field describes the model's weights
// licensing, not where the model is hosted (a hosted API can legally
// serve open-weight models, e.g. Provider: "deepseek", OpenSource: true
// per internal/verifier/fallback_models.go — that is still a cloud call).
func isLocalProviderID(provider string) bool {
	switch llm.ProviderType(provider) {
	case llm.ProviderTypeOllama, llm.ProviderTypeLlamaCpp:
		return true
	default:
		return false
	}
}

// buildProvidersFromVerifiedModels groups models by provider. Cloud providers
// are reported "disabled" while the W2c-1 cloud gate is closed
// (llm.cloud.enabled, default false) — listing a hosted provider as
// "available" when construction would refuse it is a §11.4 status bluff.
//
// Locality ("local" vs "cloud") is classified purely by provider identity
// via isLocalProviderID, never by the model's OpenSource flag — see that
// function's doc comment. OpenSource is still surfaced, as its own
// "open_source" field, for callers that care about weights licensing.
func buildProvidersFromVerifiedModels(models []*verifier.VerifiedModel) []gin.H {
	cloudEnabled := llm.CloudEnabled()
	providerMap := make(map[string][]string)
	providerInfo := make(map[string]gin.H)

	for _, m := range models {
		providerMap[m.Provider] = append(providerMap[m.Provider], m.ID)
		if _, ok := providerInfo[m.Provider]; !ok {
			providerType := "cloud"
			if isLocalProviderID(m.Provider) {
				providerType = "local"
			}
			status := "available"
			if providerType == "cloud" && !cloudEnabled {
				status = "disabled"
			}
			providerInfo[m.Provider] = gin.H{
				"id":          m.Provider,
				"name":        capitalize(m.Provider),
				"type":        providerType,
				"status":      status,
				"open_source": m.OpenSource,
			}
		}
	}

	var providers []gin.H
	for id, info := range providerInfo {
		providers = append(providers, gin.H{
			"id":          id,
			"name":        info["name"],
			"type":        info["type"],
			"status":      info["status"],
			"open_source": info["open_source"],
			"models":      providerMap[id],
			"model_count": len(providerMap[id]),
		})
	}
	return providers
}

func capitalize(s string) string {
	if len(s) == 0 {
		return s
	}
	if s == "openai" {
		return "OpenAI"
	}
	if s == "llamacpp" {
		return "Llama.cpp"
	}
	return string(s[0]-32) + s[1:]
}

// getLLMProvider returns details for a specific LLM provider.
//
// Anti-bluff (CONST-035 / CONST-037 / BLUFF-002): unknown provider IDs
// MUST return 404, not a fabricated "available" stub. Returning success
// for `/api/v1/llm/providers/typo-xyz` is the same class of bug as
// hardcoded model lists — it lies to the caller about platform state.
// The legitimate provider set is sourced from the verifier (priority 1)
// and the constitutional fallback set (priority 2). Anything outside
// that set is genuinely unknown.
func (s *Server) getLLMProvider(c *gin.Context) {
	providerID := c.Param("id")

	ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
	defer cancel()

	// Priority 1: verifier — return verified models for this provider.
	if s.verifierResult != nil && s.verifierResult.Adapter.IsEnabled() {
		models, err := s.verifierResult.Adapter.GetVerifiedModels(ctx)
		if err == nil {
			var providerModels []gin.H
			for _, m := range models {
				if m.Provider == providerID {
					providerModels = append(providerModels, verifiedModelToJSON(m))
				}
			}
			if len(providerModels) > 0 {
				c.JSON(http.StatusOK, gin.H{
					"status": "success",
					"provider": gin.H{
						"id":          providerID,
						"name":        capitalize(providerID),
						"status":      "available",
						"models":      providerModels,
						"model_count": len(providerModels),
						"source":      "verifier",
					},
				})
				return
			}
		}
	}

	// Priority 2: constitutional fallback set (CONST-035).
	knownProviders := make(map[string]bool)
	for _, m := range verifier.FallbackModels {
		knownProviders[m.Provider] = true
	}
	if !knownProviders[providerID] {
		c.JSON(http.StatusNotFound, gin.H{
			"status": "error",
			"error":  fmt.Sprintf("unknown LLM provider: %s", providerID),
		})
		return
	}

	var fallbackModels []gin.H
	for _, m := range verifier.FallbackModels {
		if m.Provider == providerID {
			fallbackModels = append(fallbackModels, verifiedModelToJSON(m))
		}
	}
	c.JSON(http.StatusOK, gin.H{
		"status": "success",
		"provider": gin.H{
			"id":          providerID,
			"name":        capitalize(providerID),
			"status":      "available",
			"configured":  true,
			"description": "LLM Provider",
			"source":      "fallback",
			"models":      fallbackModels,
			"model_count": len(fallbackModels),
		},
	})
}

// listLLMModels returns available LLM models
func (s *Server) listLLMModels(c *gin.Context) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
	defer cancel()

	// Priority 1: LLMsVerifier (CONST-036) — WORKING models only (D-2/D-4 funnel).
	//
	// The server's model listing MUST show only models the end user can actually
	// use: key-present provider ∧ Verified ∧ status=="verified" ∧ score>=min.
	// Presenting failed / pending / sub-threshold / no-key models as available is
	// a §11.4 / CONST-035 PASS-bluff at the model-listing layer — identical to
	// the CLI handleListModels path. llm.PresentProviderNames() is the shared
	// key-presence gate input (populated by the startup secrets.LoadAPIKeys wire).
	if s.verifierResult != nil && s.verifierResult.Adapter.IsEnabled() {
		present := llm.PresentProviderNames()
		models, err := s.verifierResult.Adapter.GetWorkingModels(ctx, present)
		if err == nil && len(models) > 0 {
			var result []gin.H
			for _, m := range models {
				result = append(result, verifiedModelToJSON(m))
			}
			c.JSON(http.StatusOK, gin.H{
				"status":       "success",
				"models":       result,
				"count":        len(result),
				"source":       "verifier",
				"last_updated": time.Now().UTC(),
			})
			return
		}
	}

	// Priority 2: Constitutional fallback (CONST-035)
	var result []gin.H
	for _, m := range verifier.FallbackModels {
		result = append(result, verifiedModelToJSON(m))
	}
	c.JSON(http.StatusOK, gin.H{
		"status":       "success",
		"models":       result,
		"count":        len(result),
		"source":       "fallback",
		"last_updated": time.Now().UTC(),
	})
}

// verifiedModelToJSON converts a VerifiedModel to a JSON-friendly map.
func verifiedModelToJSON(m *verifier.VerifiedModel) gin.H {
	status := "available"
	if m.VerificationStatus == "failed" {
		status = "failed"
	} else if m.VerificationStatus == "rate_limited" {
		status = "rate_limited"
	} else if m.Deprecated {
		status = "deprecated"
	}

	return gin.H{
		"id":              m.ID,
		"name":            m.DisplayName,
		"provider":        m.Provider,
		"context_length":  m.ContextSize,
		"max_tokens":      m.MaxOutputTokens,
		"score":           m.OverallScore,
		"tier":            m.Tier,
		"verified":        m.Verified,
		"status":          status,
		"supports_vision": m.SupportsVision,
		"supports_tools":  m.SupportsTools,
		// CONST-040 capability flags -- MCP / LSP / ACP / RAG / Skills /
		// Plugins -- sourced directly from the verifier-decoded
		// VerifiedModel fields (internal/verifier/types.go SupportsMCP /
		// SupportsLSP / SupportsACP / SupportsRAG / SupportsSkills /
		// SupportsPlugins). NEVER hardcode a capability value here:
		// CONST-040 / BLUFF-002 requires every value exposed here to trace
		// back to LLMsVerifier via the decoded model (HXC-117).
		"supports_mcp":     m.SupportsMCP,
		"supports_lsp":     m.SupportsLSP,
		"supports_acp":     m.SupportsACP,
		"supports_rag":     m.SupportsRAG,
		"supports_skills":  m.SupportsSkills,
		"supports_plugins": m.SupportsPlugins,
		"open_source":      m.OpenSource,
		"source":           m.Source,
	}
}

// Memory System Handlers

// listMemorySystems returns the catalogue of known memory subsystems
// with their real configuration status.
//
// Anti-bluff (CONST-035): the previous version hardcoded `status:
// "available"` for all 6 entries regardless of whether any provider
// was actually wired up. /memory/stats simultaneously reported
// `systems_connected: 0` — a direct contradiction. Callers seeing
// "6 systems available" but `systems_connected: 0` had no way to know
// which fact to trust; in reality NONE of the providers had a manager
// instance plumbed in this Server struct, so all 6 were stubs.
//
// New contract: the catalogue itself (id/name/type/description/features)
// remains as the documented set of supported memory backends — that
// data IS real (these are known software products HelixCode is
// designed to integrate with). But `status` is derived from real
// wiring state, not hardcoded. Until the corresponding manager is
// instantiated and a reachability probe runs, status is
// "not_configured" — which now agrees with `systems_connected: 0`.
func (s *Server) listMemorySystems(c *gin.Context) {
	// memoryStatus reports "available" only when the subsystem has a
	// real manager instance bound to the Server. Today none are wired
	// (the *cognee.Service field and memoryManager fields are nil in
	// every code path that reaches this handler), so all entries
	// return "not_configured". When real providers are wired they
	// MUST replace this with a real reachability probe — NOT swap
	// the literal back to "available".
	memoryStatus := func(_id string) string {
		return "not_configured"
	}
	systems := []gin.H{
		{
			"id":          "cognee",
			"name":        "Cognee",
			"type":        "knowledge_graph",
			"description": "AI-powered knowledge graph for memory management",
			"status":      memoryStatus("cognee"),
			"features":    []string{"semantic_search", "context_retrieval", "optimization"},
		},
		{
			"id":          "weaviate",
			"name":        "Weaviate",
			"type":        "vector_db",
			"description": "Vector database for embeddings",
			"status":      memoryStatus("weaviate"),
			"features":    []string{"vector_search", "hybrid_search", "filtering"},
		},
		{
			"id":          "chromadb",
			"name":        "ChromaDB",
			"type":        "vector_db",
			"description": "Embedding database",
			"status":      memoryStatus("chromadb"),
			"features":    []string{"vector_search", "metadata_filtering"},
		},
		{
			"id":          "qdrant",
			"name":        "Qdrant",
			"type":        "vector_db",
			"description": "High-performance vector similarity search",
			"status":      memoryStatus("qdrant"),
			"features":    []string{"vector_search", "filtering", "payload_indexing"},
		},
		{
			"id":          "mem0",
			"name":        "Mem0",
			"type":        "memory_layer",
			"description": "Memory layer for AI applications",
			"status":      memoryStatus("mem0"),
			"features":    []string{"user_memory", "session_memory", "agent_memory"},
		},
		{
			"id":          "zep",
			"name":        "Zep",
			"type":        "memory_store",
			"description": "Long-term memory for AI assistants",
			"status":      memoryStatus("zep"),
			"features":    []string{"conversation_history", "entity_extraction", "summarization"},
		},
	}

	c.JSON(http.StatusOK, gin.H{
		"status":  "success",
		"systems": systems,
		"count":   len(systems),
	})
}

// getMemoryStats returns memory system statistics
func (s *Server) getMemoryStats(c *gin.Context) {
	stats := gin.H{
		"total_memories":    0,
		"total_embeddings":  0,
		"storage_used_mb":   0,
		"active_sessions":   0,
		"cache_hit_rate":    0.0,
		"avg_retrieval_ms":  0,
		"systems_connected": 0,
	}

	c.JSON(http.StatusOK, gin.H{
		"status": "success",
		"stats":  stats,
	})
}

// User Handlers

// updateCurrentUser updates the current user's profile
func (s *Server) updateCurrentUser(c *gin.Context) {
	// Get current user from context
	userValue, exists := c.Get("user")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{
			"status":  "error",
			"message": tr(reqCtx(c), "internal_server_user_not_authenticated", nil),
		})
		return
	}

	user, ok := userValue.(*auth.User)
	if !ok {
		c.JSON(http.StatusInternalServerError, gin.H{
			"status":  "error",
			"message": tr(reqCtx(c), "internal_server_invalid_user_context", nil),
		})
		return
	}

	var req struct {
		DisplayName string `json:"display_name"`
		Email       string `json:"email"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"status":  "error",
			"message": tr(reqCtx(c), "internal_server_invalid_request", nil),
			"error":   err.Error(),
		})
		return
	}

	// Use existing email if not provided
	if req.Email == "" {
		req.Email = user.Email
	}

	updatedUser, err := s.auth.UpdateUser(c.Request.Context(), user.ID, req.DisplayName, req.Email)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"status":  "error",
			"message": "Failed to update user",
			"error":   err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"status": "success",
		"user":   updatedUser,
	})
}

// deleteCurrentUser deletes the current user's account
func (s *Server) deleteCurrentUser(c *gin.Context) {
	// Get current user from context
	userValue, exists := c.Get("user")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{
			"status":  "error",
			"message": tr(reqCtx(c), "internal_server_user_not_authenticated", nil),
		})
		return
	}

	user, ok := userValue.(*auth.User)
	if !ok {
		c.JSON(http.StatusInternalServerError, gin.H{
			"status":  "error",
			"message": tr(reqCtx(c), "internal_server_invalid_user_context", nil),
		})
		return
	}

	err := s.auth.DeleteUser(c.Request.Context(), user.ID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"status":  "error",
			"message": "Failed to delete user",
			"error":   err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"status":  "success",
		"message": "User account deleted successfully",
	})
}

// Worker Handlers

// createWorker registers a new worker
func (s *Server) createWorker(c *gin.Context) {
	var req struct {
		Hostname           string                 `json:"hostname" binding:"required"`
		DisplayName        string                 `json:"display_name"`
		SSHConfig          map[string]interface{} `json:"ssh_config"`
		Capabilities       []string               `json:"capabilities"`
		Resources          map[string]interface{} `json:"resources"`
		MaxConcurrentTasks int                    `json:"max_concurrent_tasks"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"status":  "error",
			"message": tr(reqCtx(c), "internal_server_invalid_request", nil),
			"error":   err.Error(),
		})
		return
	}

	if s.workerManager == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"status":  "error",
			"message": tr(reqCtx(c), "internal_server_worker_manager_unavailable", nil),
		})
		return
	}

	w, err := s.workerManager.RegisterWorker(c.Request.Context(), req.Hostname, req.DisplayName, req.SSHConfig, req.Capabilities, req.Resources)
	if err != nil {
		// 400 Bad Request for hostname-length violations (no pg
		// SQLSTATE 22001 leakage); 409 Conflict for duplicate-hostname
		// (no pg 23505 leakage); 500 only for genuine DB faults.
		if errors.Is(err, worker.ErrWorkerHostnameTooLong) {
			c.JSON(http.StatusBadRequest, gin.H{
				"status":  "error",
				"message": tr(reqCtx(c), "internal_server_worker_hostname_too_long", nil),
				"error":   err.Error(),
			})
			return
		}
		if errors.Is(err, worker.ErrWorkerHostnameTaken) {
			c.JSON(http.StatusConflict, gin.H{
				"status":  "error",
				"message": tr(reqCtx(c), "internal_server_worker_hostname_in_use", nil),
				"error":   err.Error(),
			})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{
			"status":  "error",
			"message": tr(reqCtx(c), "internal_server_failed_register_worker", nil),
			"error":   err.Error(),
		})
		return
	}

	c.JSON(http.StatusCreated, gin.H{
		"status": "success",
		"worker": w,
	})
}

// updateWorker updates an existing worker
func (s *Server) updateWorker(c *gin.Context) {
	id := c.Param("id")

	var req struct {
		Hostname           string   `json:"hostname"`
		DisplayName        string   `json:"display_name"`
		Capabilities       []string `json:"capabilities"`
		MaxConcurrentTasks int      `json:"max_concurrent_tasks"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"status":  "error",
			"message": tr(reqCtx(c), "internal_server_invalid_request", nil),
			"error":   err.Error(),
		})
		return
	}

	if s.workerManager == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"status":  "error",
			"message": tr(reqCtx(c), "internal_server_worker_manager_unavailable", nil),
		})
		return
	}

	w, err := s.workerManager.UpdateWorker(c.Request.Context(), id, req.Hostname, req.DisplayName, req.Capabilities, req.MaxConcurrentTasks)
	if err != nil {
		// 400 for malformed UUID; 404 for missing-resource;
		// 500 only for genuine DB faults.
		if respondInvalidID(c, err, "worker") {
			return
		}
		if errors.Is(err, worker.ErrWorkerNotFound) {
			c.JSON(http.StatusNotFound, gin.H{
				"status":  "error",
				"message": tr(reqCtx(c), "internal_server_worker_not_found", nil),
				"error":   err.Error(),
			})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{
			"status":  "error",
			"message": tr(reqCtx(c), "internal_server_failed_update_worker", nil),
			"error":   err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"status": "success",
		"worker": w,
	})
}

// deleteWorker removes a worker
func (s *Server) deleteWorker(c *gin.Context) {
	id := c.Param("id")

	if s.workerManager == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"status":  "error",
			"message": tr(reqCtx(c), "internal_server_worker_manager_unavailable", nil),
		})
		return
	}

	err := s.workerManager.DeleteWorker(c.Request.Context(), id)
	if err != nil {
		// 400 for malformed UUID; 404 for missing-resource;
		// 500 only for genuine DB faults.
		if respondInvalidID(c, err, "worker") {
			return
		}
		if errors.Is(err, worker.ErrWorkerNotFound) {
			c.JSON(http.StatusNotFound, gin.H{
				"status":  "error",
				"message": tr(reqCtx(c), "internal_server_worker_not_found", nil),
				"error":   err.Error(),
			})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{
			"status":  "error",
			"message": tr(reqCtx(c), "internal_server_failed_delete_worker", nil),
			"error":   err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"status":  "success",
		"message": tr(reqCtx(c), "internal_server_worker_deleted_successfully", nil),
	})
}

// workerHeartbeat updates a worker's heartbeat
func (s *Server) workerHeartbeat(c *gin.Context) {
	id := c.Param("id")

	var req struct {
		Metrics map[string]interface{} `json:"metrics"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"status":  "error",
			"message": tr(reqCtx(c), "internal_server_invalid_request", nil),
			"error":   err.Error(),
		})
		return
	}

	if s.workerManager == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"status":  "error",
			"message": tr(reqCtx(c), "internal_server_worker_manager_unavailable", nil),
		})
		return
	}

	err := s.workerManager.UpdateWorkerHeartbeat(c.Request.Context(), id, req.Metrics)
	if err != nil {
		// 400 for malformed UUID; 404 for missing worker; 500 only
		// for genuine DB faults. Pre-fix: a heartbeat on a bogus
		// worker id leaked the raw postgres FK constraint error as
		// HTTP 500 (CONST-042 schema leakage + CONST-035
		// misclassified 404 as 500).
		if respondInvalidID(c, err, "worker") {
			return
		}
		if errors.Is(err, worker.ErrWorkerNotFound) {
			c.JSON(http.StatusNotFound, gin.H{
				"status":  "error",
				"message": tr(reqCtx(c), "internal_server_worker_not_found", nil),
				"error":   err.Error(),
			})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{
			"status":  "error",
			"message": tr(reqCtx(c), "internal_server_failed_update_heartbeat", nil),
			"error":   err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"status":  "success",
		"message": tr(reqCtx(c), "internal_server_heartbeat_received", nil),
	})
}

// getWorkerMetrics retrieves metrics for a worker
func (s *Server) getWorkerMetrics(c *gin.Context) {
	id := c.Param("id")

	if s.workerManager == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"status":  "error",
			"message": tr(reqCtx(c), "internal_server_worker_manager_unavailable", nil),
		})
		return
	}

	// Get metrics from the last hour
	since := time.Now().Add(-1 * time.Hour)
	metrics, err := s.workerManager.GetWorkerMetrics(c.Request.Context(), id, since)
	if err != nil {
		// 400 for malformed UUID; 500 only for genuine DB faults.
		if respondInvalidID(c, err, "worker") {
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{
			"status":  "error",
			"message": "Failed to get worker metrics",
			"error":   err.Error(),
		})
		return
	}

	// JSON contract: list endpoint MUST return an array, not null.
	// 5th instance of the nil-slice→null bluff (tasks, projects,
	// workers, checkpoints, now worker-metrics).
	if metrics == nil {
		metrics = []*worker.WorkerMetrics{}
	}

	c.JSON(http.StatusOK, gin.H{
		"status":  "success",
		"metrics": metrics,
	})
}

// Task Handlers

// assignTask assigns a task to a worker
func (s *Server) assignTask(c *gin.Context) {
	id := c.Param("id")

	var req struct {
		WorkerID string `json:"worker_id" binding:"required"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"status":  "error",
			"message": tr(reqCtx(c), "internal_server_invalid_request", nil),
			"error":   err.Error(),
		})
		return
	}

	if s.taskManager == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"status":  "error",
			"message": tr(reqCtx(c), "internal_server_task_manager_unavailable", nil),
		})
		return
	}

	err := s.taskManager.AssignTask(c.Request.Context(), id, req.WorkerID)
	if err != nil {
		// 400 for malformed UUID (either task id OR worker_id body);
		// 422 for client-state errors; 500 for DB faults.
		if respondInvalidID(c, err, "task") {
			return
		}
		if errors.Is(err, task.ErrTaskInvalidStateTransition) {
			c.JSON(http.StatusUnprocessableEntity, gin.H{
				"status":  "error",
				"message": "Task is not in the prerequisite state to assign (must be pending)",
				"error":   err.Error(),
			})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{
			"status":  "error",
			"message": "Failed to assign task",
			"error":   err.Error(),
		})
		return
	}

	// Get the updated task
	t, _ := s.taskManager.GetTask(c.Request.Context(), id)

	c.JSON(http.StatusOK, gin.H{
		"status":  "success",
		"message": "Task assigned successfully",
		"task":    t,
	})
}

// startTask starts a task
func (s *Server) startTask(c *gin.Context) {
	id := c.Param("id")

	if s.taskManager == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"status":  "error",
			"message": tr(reqCtx(c), "internal_server_task_manager_unavailable", nil),
		})
		return
	}

	err := s.taskManager.StartTask(c.Request.Context(), id)
	if err != nil {
		// 400 for malformed UUID; 422 for client-state; 500 for DB faults.
		if respondInvalidID(c, err, "task") {
			return
		}
		if errors.Is(err, task.ErrTaskInvalidStateTransition) {
			c.JSON(http.StatusUnprocessableEntity, gin.H{
				"status":  "error",
				"message": "Task is not in the prerequisite state to start (must be pending)",
				"error":   err.Error(),
			})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{
			"status":  "error",
			"message": "Failed to start task",
			"error":   err.Error(),
		})
		return
	}

	// Get the updated task
	t, _ := s.taskManager.GetTask(c.Request.Context(), id)

	c.JSON(http.StatusOK, gin.H{
		"status":  "success",
		"message": "Task started successfully",
		"task":    t,
	})
}

// completeTask marks a task as completed
func (s *Server) completeTask(c *gin.Context) {
	id := c.Param("id")

	var req struct {
		Result map[string]interface{} `json:"result"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"status":  "error",
			"message": tr(reqCtx(c), "internal_server_invalid_request", nil),
			"error":   err.Error(),
		})
		return
	}

	if s.taskManager == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"status":  "error",
			"message": tr(reqCtx(c), "internal_server_task_manager_unavailable", nil),
		})
		return
	}

	err := s.taskManager.CompleteTask(c.Request.Context(), id, req.Result)
	if err != nil {
		// 400 for malformed UUID; 422 for client-state; 500 for DB faults.
		if respondInvalidID(c, err, "task") {
			return
		}
		if errors.Is(err, task.ErrTaskInvalidStateTransition) {
			c.JSON(http.StatusUnprocessableEntity, gin.H{
				"status":  "error",
				"message": "Task is not in the prerequisite state to complete (must be running)",
				"error":   err.Error(),
			})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{
			"status":  "error",
			"message": "Failed to complete task",
			"error":   err.Error(),
		})
		return
	}

	// Get the updated task
	t, _ := s.taskManager.GetTask(c.Request.Context(), id)

	c.JSON(http.StatusOK, gin.H{
		"status":  "success",
		"message": "Task completed successfully",
		"task":    t,
	})
}

// failTask marks a task as failed
func (s *Server) failTask(c *gin.Context) {
	id := c.Param("id")

	var req struct {
		ErrorMessage string `json:"error_message" binding:"required"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"status":  "error",
			"message": tr(reqCtx(c), "internal_server_invalid_request", nil),
			"error":   err.Error(),
		})
		return
	}

	if s.taskManager == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"status":  "error",
			"message": tr(reqCtx(c), "internal_server_task_manager_unavailable", nil),
		})
		return
	}

	err := s.taskManager.FailTask(c.Request.Context(), id, req.ErrorMessage)
	if err != nil {
		// 400 for malformed UUID; 404 for missing-resource;
		// 500 only for genuine DB faults.
		if respondInvalidID(c, err, "task") {
			return
		}
		if errors.Is(err, task.ErrTaskNotFound) {
			c.JSON(http.StatusNotFound, gin.H{
				"status":  "error",
				"message": tr(reqCtx(c), "internal_server_task_not_found", nil),
				"error":   err.Error(),
			})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{
			"status":  "error",
			"message": "Failed to mark task as failed",
			"error":   err.Error(),
		})
		return
	}

	// Get the updated task
	t, _ := s.taskManager.GetTask(c.Request.Context(), id)

	c.JSON(http.StatusOK, gin.H{
		"status":  "success",
		"message": "Task marked as failed",
		"task":    t,
	})
}

// createTaskCheckpoint creates a checkpoint for a task
func (s *Server) createTaskCheckpoint(c *gin.Context) {
	id := c.Param("id")

	var req struct {
		CheckpointName string                 `json:"checkpoint_name" binding:"required"`
		CheckpointData map[string]interface{} `json:"checkpoint_data"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"status":  "error",
			"message": tr(reqCtx(c), "internal_server_invalid_request", nil),
			"error":   err.Error(),
		})
		return
	}

	if s.taskManager == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"status":  "error",
			"message": tr(reqCtx(c), "internal_server_task_manager_unavailable", nil),
		})
		return
	}

	err := s.taskManager.CreateCheckpoint(c.Request.Context(), id, req.CheckpointName, req.CheckpointData)
	if err != nil {
		// 400 for malformed UUID; 422 for client-state errors
		// (task not assigned to a worker — schema requires worker_id
		// on every checkpoint row). 500 only for genuine DB faults.
		if respondInvalidID(c, err, "task") {
			return
		}
		if errors.Is(err, task.ErrCheckpointRequiresAssignment) {
			c.JSON(http.StatusUnprocessableEntity, gin.H{
				"status":  "error",
				"message": "Task must be assigned to a worker before creating a checkpoint",
				"error":   err.Error(),
			})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{
			"status":  "error",
			"message": "Failed to create checkpoint",
			"error":   err.Error(),
		})
		return
	}

	c.JSON(http.StatusCreated, gin.H{
		"status":  "success",
		"message": "Checkpoint created successfully",
	})
}

// getTaskCheckpoints retrieves all checkpoints for a task
func (s *Server) getTaskCheckpoints(c *gin.Context) {
	id := c.Param("id")

	if s.taskManager == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"status":  "error",
			"message": tr(reqCtx(c), "internal_server_task_manager_unavailable", nil),
		})
		return
	}

	checkpoints, err := s.taskManager.GetCheckpoints(c.Request.Context(), id)
	if err != nil {
		// 400 for malformed UUID; 500 only for genuine DB faults.
		if respondInvalidID(c, err, "task") {
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{
			"status":  "error",
			"message": "Failed to get checkpoints",
			"error":   err.Error(),
		})
		return
	}

	// JSON contract: list endpoint MUST return an array, not null.
	// Same nil-slice→null serialization bluff as listTasks/Projects/Workers
	// — 4th instance of this pattern. Default to empty slice.
	if checkpoints == nil {
		checkpoints = []map[string]interface{}{}
	}

	c.JSON(http.StatusOK, gin.H{
		"status":      "success",
		"checkpoints": checkpoints,
	})
}

// retryTask retries a failed task
func (s *Server) retryTask(c *gin.Context) {
	id := c.Param("id")

	if s.taskManager == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"status":  "error",
			"message": tr(reqCtx(c), "internal_server_task_manager_unavailable", nil),
		})
		return
	}

	err := s.taskManager.RetryTask(c.Request.Context(), id)
	if err != nil {
		// 400 for malformed UUID; 422 for client-state errors (task
		// not found, not in failed state, or max-retries exceeded);
		// 500 only for genuine server faults.
		if respondInvalidID(c, err, "task") {
			return
		}
		if errors.Is(err, task.ErrTaskNotRetryable) {
			c.JSON(http.StatusUnprocessableEntity, gin.H{
				"status":  "error",
				"message": "Task is not in a retryable state",
				"error":   err.Error(),
			})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{
			"status":  "error",
			"message": "Failed to retry task",
			"error":   err.Error(),
		})
		return
	}

	// Get the updated task
	task, _ := s.taskManager.GetTask(c.Request.Context(), id)

	c.JSON(http.StatusOK, gin.H{
		"status":  "success",
		"message": "Task queued for retry",
		"task":    task,
	})
}

// Project Handlers

// getProjectSessions retrieves all sessions for a project
func (s *Server) getProjectSessions(c *gin.Context) {
	projectID := c.Param("id")

	if s.sessionManager == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"status":  "error",
			"message": tr(reqCtx(c), "internal_server_session_manager_unavailable", nil),
		})
		return
	}

	// IDOR fix: only the project's owner may list its sessions. Without this
	// gate any authenticated user could enumerate another user's project
	// sessions by id. Requires projectManager to verify ownership; if it's
	// unavailable, fail closed (503) rather than serve unscoped data.
	if s.projectManager == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"status":  "error",
			"message": tr(reqCtx(c), "internal_server_project_manager_unavailable", nil),
			"error":   "database connection required to authorize project access",
		})
		return
	}
	userID, ok := s.requireUserID(c)
	if !ok {
		return
	}
	if !s.enforceProjectOwner(c, projectID, userID) {
		return
	}

	sessions := s.sessionManager.GetByProject(projectID)

	c.JSON(http.StatusOK, gin.H{
		"status":   "success",
		"sessions": sessions,
	})
}

// Session Handlers

// listSessions returns all sessions
func (s *Server) listSessions(c *gin.Context) {
	if s.sessionManager == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"status":  "error",
			"message": tr(reqCtx(c), "internal_server_session_manager_unavailable", nil),
		})
		return
	}

	sessions := s.sessionManager.GetAll()

	c.JSON(http.StatusOK, gin.H{
		"status":   "success",
		"sessions": sessions,
	})
}

// createSession creates a new session
func (s *Server) createSession(c *gin.Context) {
	var req struct {
		ProjectID   string `json:"project_id" binding:"required"`
		Name        string `json:"name" binding:"required"`
		Description string `json:"description"`
		Mode        string `json:"mode" binding:"required"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"status":  "error",
			"message": tr(reqCtx(c), "internal_server_invalid_request", nil),
			"error":   err.Error(),
		})
		return
	}

	if s.sessionManager == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"status":  "error",
			"message": tr(reqCtx(c), "internal_server_session_manager_unavailable", nil),
		})
		return
	}

	// Validate project_id BEFORE creating the session. The in-memory
	// session manager doesn't check the project FK, so without this
	// gate POST /sessions silently accepts bogus or malformed
	// project_id (BUG #29/#30: the session would be created with a
	// dangling reference). This pre-check catches both cases:
	//   - malformed UUID → 400 via ErrInvalidProjectID
	//   - well-formed UUID but no such project → 404 via ErrProjectNotFound
	if s.projectManager != nil {
		if _, err := s.projectManager.GetProject(c.Request.Context(), req.ProjectID); err != nil {
			if respondInvalidID(c, err, "project") {
				return
			}
			if errors.Is(err, project.ErrProjectNotFound) {
				c.JSON(http.StatusNotFound, gin.H{
					"status":  "error",
					"message": "Project not found — cannot create session without a real project",
					"error":   err.Error(),
				})
				return
			}
			c.JSON(http.StatusInternalServerError, gin.H{
				"status":  "error",
				"message": "Failed to validate project for session create",
				"error":   err.Error(),
			})
			return
		}
	}

	sess, err := s.sessionManager.Create(req.ProjectID, req.Name, req.Description, session.Mode(req.Mode))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"status":  "error",
			"message": "Failed to create session",
			"error":   err.Error(),
		})
		return
	}

	c.JSON(http.StatusCreated, gin.H{
		"status":  "success",
		"session": sess,
	})
}

// getSession retrieves a session by ID
func (s *Server) getSession(c *gin.Context) {
	id := c.Param("id")

	if s.sessionManager == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"status":  "error",
			"message": tr(reqCtx(c), "internal_server_session_manager_unavailable", nil),
		})
		return
	}

	sess, err := s.sessionManager.Get(id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{
			"status":  "error",
			"message": tr(reqCtx(c), "internal_server_session_not_found", nil),
			"error":   err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"status":  "success",
		"session": sess,
	})
}

// updateSession updates a session
func (s *Server) updateSession(c *gin.Context) {
	id := c.Param("id")

	var req struct {
		Action string `json:"action" binding:"required"` // start, pause, resume, complete, fail
		Reason string `json:"reason"`                    // For fail action
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"status":  "error",
			"message": tr(reqCtx(c), "internal_server_invalid_request", nil),
			"error":   err.Error(),
		})
		return
	}

	if s.sessionManager == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"status":  "error",
			"message": tr(reqCtx(c), "internal_server_session_manager_unavailable", nil),
		})
		return
	}

	var err error
	switch req.Action {
	case "start":
		err = s.sessionManager.Start(id)
	case "pause":
		err = s.sessionManager.Pause(id)
	case "resume":
		err = s.sessionManager.Resume(id)
	case "complete":
		err = s.sessionManager.Complete(id)
	case "fail":
		err = s.sessionManager.Fail(id, req.Reason)
	default:
		c.JSON(http.StatusBadRequest, gin.H{
			"status":  "error",
			"message": "Invalid action. Use: start, pause, resume, complete, fail",
		})
		return
	}

	if err != nil {
		// 404 for missing session; 500 only for genuine faults.
		if errors.Is(err, session.ErrSessionNotFound) {
			c.JSON(http.StatusNotFound, gin.H{
				"status":  "error",
				"message": tr(reqCtx(c), "internal_server_session_not_found", nil),
				"error":   err.Error(),
			})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{
			"status":  "error",
			"message": "Failed to update session",
			"error":   err.Error(),
		})
		return
	}

	// Get the updated session
	sess, _ := s.sessionManager.Get(id)

	c.JSON(http.StatusOK, gin.H{
		"status":  "success",
		"message": "Session updated successfully",
		"session": sess,
	})
}

// deleteSession deletes a session
func (s *Server) deleteSession(c *gin.Context) {
	id := c.Param("id")

	if s.sessionManager == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"status":  "error",
			"message": tr(reqCtx(c), "internal_server_session_manager_unavailable", nil),
		})
		return
	}

	err := s.sessionManager.Delete(id)
	if err != nil {
		// 404 for missing-resource errors; 500 only for genuine faults.
		if errors.Is(err, session.ErrSessionNotFound) {
			c.JSON(http.StatusNotFound, gin.H{
				"status":  "error",
				"message": tr(reqCtx(c), "internal_server_session_not_found", nil),
				"error":   err.Error(),
			})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{
			"status":  "error",
			"message": "Failed to delete session",
			"error":   err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"status":  "success",
		"message": "Session deleted successfully",
	})
}
