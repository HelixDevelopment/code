package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"dev.helix.code/internal/config"
	"dev.helix.code/internal/database"
	"dev.helix.code/internal/redis"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestServer_UptimeTracking tests that server uptime is tracked correctly
func TestServer_UptimeTracking(t *testing.T) {
	cfg, db, rds := createMockDependencies(t)

	// Record start time
	startTime := time.Now()

	server := newTestServer(t, cfg, db, rds)

	// Server startTime should be set
	assert.NotNil(t, server.startTime)

	// StartTime should be approximately equal to our recorded time (within 1 second)
	diff := server.startTime.Sub(startTime)
	assert.True(t, diff < time.Second && diff > -time.Second,
		"server start time should be set at initialization")

	// Wait a small amount of time
	time.Sleep(100 * time.Millisecond)

	// Calculate uptime
	uptime := time.Since(server.startTime)
	assert.True(t, uptime >= 100*time.Millisecond,
		"uptime should be at least 100ms")
}

// TestGetSystemStats_WithoutManagers tests getSystemStats endpoint with nil managers
func TestGetSystemStats_WithoutManagers(t *testing.T) {
	cfg, db, rds := createMockDependencies(t)
	server := newTestServer(t, cfg, db, rds)

	// Create test router and register the handler
	router := gin.New()
	router.GET("/api/v1/system/stats", server.getSystemStats)

	// Create request
	req, _ := http.NewRequest("GET", "/api/v1/system/stats", nil)
	resp := httptest.NewRecorder()

	// Perform request
	router.ServeHTTP(resp, req)

	// Check status code
	assert.Equal(t, http.StatusOK, resp.Code)

	// Parse response
	var result map[string]interface{}
	err := json.Unmarshal(resp.Body.Bytes(), &result)
	require.NoError(t, err)

	// Verify response structure
	assert.Equal(t, "success", result["status"])
	assert.Contains(t, result, "stats")

	stats := result["stats"].(map[string]interface{})

	// Verify tasks section exists with zero values
	assert.Contains(t, stats, "tasks")
	tasks := stats["tasks"].(map[string]interface{})
	assert.Equal(t, float64(0), tasks["total"])
	assert.Equal(t, float64(0), tasks["pending"])
	assert.Equal(t, float64(0), tasks["running"])
	assert.Equal(t, float64(0), tasks["completed"])
	assert.Equal(t, float64(0), tasks["failed"])

	// Verify workers section exists with zero values
	assert.Contains(t, stats, "workers")
	workers := stats["workers"].(map[string]interface{})
	assert.Equal(t, float64(0), workers["total"])
	assert.Equal(t, float64(0), workers["active"])

	// Verify system section exists with uptime
	assert.Contains(t, stats, "system")
	system := stats["system"].(map[string]interface{})
	assert.Contains(t, system, "uptime")

	// Uptime should be a non-empty string
	uptime := system["uptime"].(string)
	assert.NotEmpty(t, uptime)
	assert.NotEqual(t, "0s", uptime, "uptime should not be exactly zero")
}

// TestGetSystemStats_UptimeFormat tests that uptime is formatted correctly
func TestGetSystemStats_UptimeFormat(t *testing.T) {
	cfg, db, rds := createMockDependencies(t)
	server := newTestServer(t, cfg, db, rds)

	// Wait a known amount of time
	time.Sleep(150 * time.Millisecond)

	// Create test router
	router := gin.New()
	router.GET("/api/v1/system/stats", server.getSystemStats)

	// Make request
	req, _ := http.NewRequest("GET", "/api/v1/system/stats", nil)
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)

	// Parse response
	var result map[string]interface{}
	err := json.Unmarshal(resp.Body.Bytes(), &result)
	require.NoError(t, err)

	stats := result["stats"].(map[string]interface{})
	system := stats["system"].(map[string]interface{})
	uptime := system["uptime"].(string)

	// Uptime should contain time units
	assert.Contains(t, uptime, "ms", "uptime should be formatted with time units")
}

// TestGetSystemStats_ResponseStructure tests the complete response structure
func TestGetSystemStats_ResponseStructure(t *testing.T) {
	cfg, db, rds := createMockDependencies(t)
	server := newTestServer(t, cfg, db, rds)

	router := gin.New()
	router.GET("/api/v1/system/stats", server.getSystemStats)

	req, _ := http.NewRequest("GET", "/api/v1/system/stats", nil)
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)

	assert.Equal(t, http.StatusOK, resp.Code)

	var result map[string]interface{}
	err := json.Unmarshal(resp.Body.Bytes(), &result)
	require.NoError(t, err)

	// Test complete structure
	expectedFields := []string{"status", "stats"}
	for _, field := range expectedFields {
		assert.Contains(t, result, field, "response should contain %s", field)
	}

	stats := result["stats"].(map[string]interface{})

	// Verify all stat categories exist
	expectedCategories := []string{"tasks", "workers", "system"}
	for _, category := range expectedCategories {
		assert.Contains(t, stats, category, "stats should contain %s category", category)
	}

	// Verify task fields
	tasks := stats["tasks"].(map[string]interface{})
	taskFields := []string{"total", "pending", "running", "completed", "failed"}
	for _, field := range taskFields {
		assert.Contains(t, tasks, field, "tasks should contain %s", field)
	}

	// Verify worker fields
	workers := stats["workers"].(map[string]interface{})
	workerFields := []string{"total", "active"}
	for _, field := range workerFields {
		assert.Contains(t, workers, field, "workers should contain %s", field)
	}

	// Verify system fields
	system := stats["system"].(map[string]interface{})
	assert.Contains(t, system, "uptime", "system should contain uptime")
}

// TestGetSystemStatus_UptimeInStatus tests that uptime tracking works with system status
func TestGetSystemStatus_UptimeInStats(t *testing.T) {
	cfg := &config.Config{
		Server: config.ServerConfig{
			Address: "localhost",
			Port:    8080,
		},
		Logging: config.LoggingConfig{
			Level: "info",
		},
	}

	db := (*database.Database)(nil)
	rds := &redis.Client{}

	// Create server
	server1 := newTestServer(t, cfg, db, rds)
	time1 := server1.startTime

	// Wait
	time.Sleep(100 * time.Millisecond)

	// Create another server
	server2 := newTestServer(t, cfg, db, rds)
	time2 := server2.startTime

	// Time2 should be after time1
	assert.True(t, time2.After(time1),
		"second server should have later start time")

	// Uptimes should be different
	uptime1 := time.Since(server1.startTime)
	uptime2 := time.Since(server2.startTime)
	assert.True(t, uptime1 > uptime2,
		"first server should have longer uptime")
}

// TestNewServer_ManagerInitialization tests that managers are initialized correctly
func TestNewServer_ManagerInitialization(t *testing.T) {
	tests := []struct {
		name                string
		db                  *database.Database
		expectTaskManager   bool
		expectWorkerManager bool
	}{
		{
			name:                "with nil database",
			db:                  nil,
			expectTaskManager:   false,
			expectWorkerManager: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.Config{
				Server: config.ServerConfig{
					Address: "localhost",
					Port:    8080,
				},
				Logging: config.LoggingConfig{
					Level: "debug",
				},
			}

			rds := &redis.Client{}

			server := newTestServer(t, cfg, tt.db, rds)

			if tt.expectTaskManager {
				assert.NotNil(t, server.taskManager, "task manager should be initialized")
			} else {
				assert.Nil(t, server.taskManager, "task manager should be nil")
			}

			if tt.expectWorkerManager {
				assert.NotNil(t, server.workerManager, "worker manager should be initialized")
			} else {
				assert.Nil(t, server.workerManager, "worker manager should be nil")
			}
		})
	}
}

// TestGetSystemStats_ManagerNilSafety tests nil safety for managers
func TestGetSystemStats_ManagerNilSafety(t *testing.T) {
	cfg, db, rds := createMockDependencies(t)
	server := newTestServer(t, cfg, db, rds)

	// Ensure managers are nil
	server.taskManager = nil
	server.workerManager = nil

	router := gin.New()
	router.GET("/api/v1/system/stats", server.getSystemStats)

	req, _ := http.NewRequest("GET", "/api/v1/system/stats", nil)
	resp := httptest.NewRecorder()

	// This should not panic
	router.ServeHTTP(resp, req)

	assert.Equal(t, http.StatusOK, resp.Code, "should handle nil managers gracefully")

	var result map[string]interface{}
	err := json.Unmarshal(resp.Body.Bytes(), &result)
	require.NoError(t, err)

	// Should return success with zero values
	assert.Equal(t, "success", result["status"])
}

// TestGetSystemStats_MultipleRequests tests that stats are consistent across requests
func TestGetSystemStats_MultipleRequests(t *testing.T) {
	cfg, db, rds := createMockDependencies(t)
	server := newTestServer(t, cfg, db, rds)

	router := gin.New()
	router.GET("/api/v1/system/stats", server.getSystemStats)

	// Make first request
	req1, _ := http.NewRequest("GET", "/api/v1/system/stats", nil)
	resp1 := httptest.NewRecorder()
	router.ServeHTTP(resp1, req1)

	var result1 map[string]interface{}
	err := json.Unmarshal(resp1.Body.Bytes(), &result1)
	require.NoError(t, err)

	stats1 := result1["stats"].(map[string]interface{})
	system1 := stats1["system"].(map[string]interface{})
	uptime1 := system1["uptime"].(string)

	// Wait a bit
	time.Sleep(50 * time.Millisecond)

	// Make second request
	req2, _ := http.NewRequest("GET", "/api/v1/system/stats", nil)
	resp2 := httptest.NewRecorder()
	router.ServeHTTP(resp2, req2)

	var result2 map[string]interface{}
	err = json.Unmarshal(resp2.Body.Bytes(), &result2)
	require.NoError(t, err)

	stats2 := result2["stats"].(map[string]interface{})
	system2 := stats2["system"].(map[string]interface{})
	uptime2 := system2["uptime"].(string)

	// Second uptime should be different (longer) than first
	assert.NotEqual(t, uptime1, uptime2, "uptime should increase between requests")
}

// TestServer_StartTimeImmutable tests that start time doesn't change
func TestServer_StartTimeImmutable(t *testing.T) {
	cfg, db, rds := createMockDependencies(t)
	server := newTestServer(t, cfg, db, rds)

	// Record initial start time
	initialStartTime := server.startTime

	// Make multiple stats requests
	router := gin.New()
	router.GET("/api/v1/system/stats", server.getSystemStats)

	for i := 0; i < 5; i++ {
		req, _ := http.NewRequest("GET", "/api/v1/system/stats", nil)
		resp := httptest.NewRecorder()
		router.ServeHTTP(resp, req)
		time.Sleep(10 * time.Millisecond)
	}

	// Start time should not have changed
	assert.Equal(t, initialStartTime, server.startTime,
		"server start time should remain constant")
}

// BenchmarkGetSystemStats benchmarks the system stats endpoint
func BenchmarkGetSystemStats(b *testing.B) {
	cfg := &config.Config{
		Server: config.ServerConfig{
			Address: "localhost",
			Port:    8080,
		},
		Logging: config.LoggingConfig{
			Level: "release",
		},
	}

	db := (*database.Database)(nil)
	rds := &redis.Client{}

	server := newTestServer(b, cfg, db, rds)

	router := gin.New()
	router.GET("/api/v1/system/stats", server.getSystemStats)

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		req, _ := http.NewRequest("GET", "/api/v1/system/stats", nil)
		resp := httptest.NewRecorder()
		router.ServeHTTP(resp, req)
	}
}

// TestGetSystemStats_ContentType tests response content type
func TestGetSystemStats_ContentType(t *testing.T) {
	cfg, db, rds := createMockDependencies(t)
	server := newTestServer(t, cfg, db, rds)

	router := gin.New()
	router.GET("/api/v1/system/stats", server.getSystemStats)

	req, _ := http.NewRequest("GET", "/api/v1/system/stats", nil)
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)

	assert.Equal(t, http.StatusOK, resp.Code)
	assert.Contains(t, resp.Header().Get("Content-Type"), "application/json")
}
