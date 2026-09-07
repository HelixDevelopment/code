package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestNewHelixConfigManager tests configuration manager creation
func TestNewHelixConfigManager(t *testing.T) {
	// Create temporary directory
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "test_config.json")

	// Test creating new manager with existing config
	defaultConfig := getDefaultConfig()
	data, err := json.MarshalIndent(defaultConfig, "", "  ")
	require.NoError(t, err)
	err = os.WriteFile(configPath, data, 0644)
	require.NoError(t, err)

	manager, err := NewHelixConfigManager(configPath)
	require.NoError(t, err)
	assert.NotNil(t, manager)
}

// TestConfigurationManager tests configuration manager operations
func TestConfigurationManager(t *testing.T) {
	// Create temporary directory
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "test_config.json")

	// Test creating new manager without existing config
	manager, err := NewHelixConfigManager(configPath)
	require.NoError(t, err)
	assert.NotNil(t, manager)

	// Test configuration is present
	assert.True(t, manager.IsConfigPresent())
	assert.Equal(t, configPath, manager.GetConfigPath())
}

// TestConfigurationUpdates tests configuration update operations
func TestConfigurationUpdates(t *testing.T) {
	// Create temporary directory
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "test_config.json")

	// Create manager
	manager, err := NewHelixConfigManager(configPath)
	require.NoError(t, err)

	// Test update config
	err = manager.UpdateConfig(func(config *Config) {
		config.Server.Port = 9090
	})
	require.NoError(t, err)

	// Test update from map
	updates := map[string]interface{}{
		"server.port": 8080,
	}
	err = manager.UpdateConfigFromMap(updates)
	require.NoError(t, err)
}

// TestDefaultConfig tests default configuration values
func TestDefaultConfig(t *testing.T) {
	config := getDefaultConfig()

	// Application section
	assert.Equal(t, "HelixCode", config.Application.Name)
	assert.Equal(t, "1.0.0", config.Application.Version)
	assert.Equal(t, "Enterprise AI Development Platform", config.Application.Description)
	assert.Equal(t, "development", config.Application.Environment)
	assert.False(t, config.Application.Telemetry.Enabled)
	assert.Equal(t, "info", config.Application.Telemetry.Level)
	assert.Equal(t, 30, config.Application.Telemetry.DataRetention)

	// Database section
	assert.Equal(t, "localhost", config.Database.Host)
	assert.Equal(t, 5432, config.Database.Port)
	assert.Equal(t, "helixcode", config.Database.DBName)
	assert.Equal(t, "helixcode", config.Database.User)
	assert.Equal(t, "disable", config.Database.SSLMode)

	// Redis section
	assert.False(t, config.Redis.Enabled)
	assert.Equal(t, "localhost", config.Redis.Host)
	assert.Equal(t, 6379, config.Redis.Port)
	assert.Equal(t, 0, config.Redis.Database)

	// Auth section
	assert.NotEmpty(t, config.Auth.JWTSecret)
	assert.NotEmpty(t, config.Auth.TokenExpiry)
	assert.NotEmpty(t, config.Auth.SessionExpiry)

	// Server section
	assert.Equal(t, "0.0.0.0", config.Server.Address)
	assert.Equal(t, 8080, config.Server.Port)
	assert.Equal(t, 30, config.Server.ReadTimeout)
	assert.Equal(t, 30, config.Server.WriteTimeout)
	assert.Equal(t, 60, config.Server.IdleTimeout)
	assert.Equal(t, 30, config.Server.ShutdownTimeout)

	// Workers section
	assert.Equal(t, 30, config.Workers.HealthCheckInterval)
	assert.Equal(t, 120, config.Workers.HealthTTL)
	assert.Equal(t, 10, config.Workers.MaxConcurrentTasks)

	// Tasks section
	assert.Equal(t, 3, config.Tasks.MaxRetries)
	assert.Equal(t, 300, config.Tasks.CheckpointInterval)
	assert.Equal(t, 600, config.Tasks.CleanupInterval)

	// LLM section.
	//
	// §11.4.120 reconciliation (operator decision 2026-09-05): the
	// BUILTIN default for llm.default_provider is now intentionally
	// EMPTY, not "local" (see config.go setDefaultsOn's comment on
	// v.SetDefault("llm.default_provider", "")). A non-empty builtin
	// default applied even with NO config file at all, silently making
	// the zero-config Ollama fallback (:11434) unreachable in every
	// process where config loads -- retiring a shipped capability
	// (§11.4.122). The shipped config/config.yaml still sets
	// default_provider: "local" explicitly, so a normal deployment is
	// unaffected and still routes to the local coder (:18434,
	// HXC-002-F3-01). Do NOT "helpfully" restore "local" here --
	// that would silently re-break the zero-config Ollama route this
	// assertion exists to guard.
	assert.Equal(t, "", config.LLM.DefaultProvider)
	assert.Equal(t, "llama-3.2-3b", config.LLM.DefaultModel)
	assert.Equal(t, 4096, config.LLM.MaxTokens)
	assert.Equal(t, 0.7, config.LLM.Temperature)
}

// TestConfigurationManagerGetConfig tests getting configuration from manager
func TestConfigurationManagerGetConfig(t *testing.T) {
	// Create temporary directory
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "test_config.json")

	// Create and initialize manager
	manager, err := NewHelixConfigManager(configPath)
	require.NoError(t, err)

	// Get config
	config := manager.GetConfig()
	require.NotNil(t, config)
	assert.Equal(t, "HelixCode", config.Application.Name)
}

// TestConfigurationManagerSaveLoad tests saving and loading configuration
func TestConfigurationManagerSaveLoad(t *testing.T) {
	// Create temporary directory
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "test_config.json")

	// Create manager
	manager, err := NewHelixConfigManager(configPath)
	require.NoError(t, err)

	// Modify config
	err = manager.UpdateConfig(func(config *Config) {
		config.Server.Port = 9999
		config.Application.Name = "TestApp"
	})
	require.NoError(t, err)

	// Verify changes
	config := manager.GetConfig()
	assert.Equal(t, 9999, config.Server.Port)
	assert.Equal(t, "TestApp", config.Application.Name)
}
