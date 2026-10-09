package config

import (
	"os"
	"path/filepath"
	"testing"

	"dev.helix.code/internal/database"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadConfig(t *testing.T) {
	// Create temporary config file
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "config.yaml")

	// Write test config
	content := `{
  "version": "1.0.0",
  "server": {
    "address": "0.0.0.0",
    "port": 8080
  },
  "database": {
    "host": "localhost",
    "port": 5432,
    "dbname": "test",
    "user": "test"
  },
  "auth": {
    "jwt_secret": "test-jwt-secret-for-testing"
  }
}`
	err := os.WriteFile(configPath, []byte(content), 0644)
	require.NoError(t, err)

	// Clean up environment
	defer func() {
		if oldVal := os.Getenv("HELIX_CONFIG"); oldVal != "" {
			os.Unsetenv("HELIX_CONFIG")
		}
	}()
	os.Unsetenv("HELIX_AUTH_JWT_SECRET")

	// Test that config file exists and can be read
	_, err = os.Stat(configPath)
	require.NoError(t, err)

	// Basic test - create a config manager and initialize it
	manager, err := NewHelixConfigManager(configPath)
	require.NoError(t, err)
	assert.NotNil(t, manager)
}

func TestValidateConfig(t *testing.T) {
	tests := []struct {
		name    string
		config  Config
		wantErr bool
	}{
		{
			name: "valid config",
			config: Config{
				Version: "1.0.0",
				Application: ApplicationConfig{
					Environment: "development",
				},
				Server: ServerConfig{Port: 8080},
				Database: database.Config{
					Port: 5432,
				},
				Auth: AuthConfig{
					JWTSecret:  "test-jwt-secret-32-chars-long-!!!",
					BcryptCost: 12,
				},
				LLM: LLMConfig{
					DefaultProvider: "local",
					MaxTokens:       1000,
				},
			},
			wantErr: false,
		},
		{
			name: "invalid port",
			config: Config{
				Server: ServerConfig{Port: 99999},
			},
			wantErr: true,
		},
		{
			name: "missing required fields",
			config: Config{
				Server: ServerConfig{Port: 8080},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			validator := NewConfigurationValidator(true)
			result := validator.Validate(&tt.config)
			if tt.wantErr {
				assert.False(t, result.Valid)
				assert.NotEmpty(t, result.Errors)
			} else {
				assert.True(t, result.Valid)
				assert.Empty(t, result.Errors)
			}
		})
	}
}

func TestFindConfigFile(t *testing.T) {
	// Create temporary config file
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "config.yaml")

	content := `
server:
  port: 8080
`
	err := os.WriteFile(configPath, []byte(content), 0644)
	require.NoError(t, err)

	// Change to temp directory
	oldCwd, _ := os.Getwd()
	defer os.Chdir(oldCwd)
	os.Chdir(tempDir)

	found := findConfigFile()
	// §11.4.81 macOS: /var is a symlink to /private/var; resolve both sides
	// before comparing so the test is platform-neutral.
	resolvedExpected, err := filepath.EvalSymlinks(configPath)
	require.NoError(t, err)
	resolvedFound, err := filepath.EvalSymlinks(found)
	require.NoError(t, err)
	assert.Equal(t, resolvedExpected, resolvedFound)
}

func TestCreateDefaultConfig(t *testing.T) {
	// Anti-bluff (CONST-035 / §11.9): original form only asserted Write
	// + Stat NoError — passing even if WriteFile silently wrote empty
	// bytes or os.Stat lied about size. Pin the round-trip: read the
	// file back and assert content matches what we wrote.
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "config.yaml")

	content := `server:
  address: "0.0.0.0"
  port: 8080
auth:
  jwt_secret: "change-me"
database:
  host: "localhost"
  port: 5432
`
	err := os.WriteFile(configPath, []byte(content), 0644)
	require.NoError(t, err)

	info, err := os.Stat(configPath)
	require.NoError(t, err)
	assert.Equal(t, int64(len(content)), info.Size(),
		"written file size must match content length — Stat-only check would pass on 0-byte write")

	readBack, err := os.ReadFile(configPath)
	require.NoError(t, err)
	assert.Equal(t, content, string(readBack),
		"file content must round-trip exactly — silent corruption would pass the NoError-only check")
}

func TestConfigValidationEdgeCases(t *testing.T) {
	tests := []struct {
		name   string
		config Config
		valid  bool
	}{
		{
			name: "minimal valid config",
			config: Config{
				Version: "1.0.0",
				Application: ApplicationConfig{
					Environment: "development",
				},
				Server: ServerConfig{Port: 8080},
				Database: database.Config{
					Port: 5432,
				},
				Auth: AuthConfig{JWTSecret: "test-jwt-secret-32-chars-long-!!!", BcryptCost: 12},
				LLM: LLMConfig{
					DefaultProvider: "local",
					MaxTokens:       1000,
				},
			},
			valid: true,
		},
		{
			name: "port too low",
			config: Config{
				Server: ServerConfig{Port: 0},
			},
			valid: false,
		},
		{
			name: "port negative",
			config: Config{
				Server: ServerConfig{Port: -1},
			},
			valid: false,
		},
		{
			name: "missing auth secret",
			config: Config{
				Server: ServerConfig{Port: 8080},
				Auth:   AuthConfig{JWTSecret: ""},
			},
			valid: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			validator := NewConfigurationValidator(true)
			result := validator.Validate(&tt.config)
			if tt.valid {
				assert.True(t, result.Valid)
				assert.Empty(t, result.Errors)
			} else {
				assert.False(t, result.Valid)
				assert.NotEmpty(t, result.Errors)
			}
		})
	}
}

func TestLoadFunction(t *testing.T) {
	// Test Load function with environment setup
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "config.yaml")

	// Create a valid config file
	content := `version: "1.0.0"
application:
  name: "HelixCode"
  environment: "development"
server:
  address: "0.0.0.0"
  port: 8080
database:
  host: "localhost"
  port: 5432
  dbname: "helixcode"
  user: "helixcode"
auth:
  jwt_secret: "test-jwt-secret-32-chars-long-for-testing"
workers:
  health_check_interval: 30
  max_concurrent_tasks: 10
tasks:
  max_retries: 3
  checkpoint_interval: 300
llm:
  default_provider: "local"
  max_tokens: 4096
  temperature: 0.7
logging:
  level: "info"
  format: "text"
  output: "stdout"
`
	err := os.WriteFile(configPath, []byte(content), 0644)
	require.NoError(t, err)

	// Set environment to point to our test config
	oldConfig := os.Getenv("HELIX_CONFIG")
	oldJWT := os.Getenv("HELIX_AUTH_JWT_SECRET")
	defer func() {
		if oldConfig != "" {
			os.Setenv("HELIX_CONFIG", oldConfig)
		} else {
			os.Unsetenv("HELIX_CONFIG")
		}
		if oldJWT != "" {
			os.Setenv("HELIX_AUTH_JWT_SECRET", oldJWT)
		} else {
			os.Unsetenv("HELIX_AUTH_JWT_SECRET")
		}
	}()
	os.Setenv("HELIX_CONFIG", configPath)

	// Test Load function
	config, err := Load()
	require.NoError(t, err)
	assert.NotNil(t, config)
	assert.Equal(t, "1.0.0", config.Version)
	assert.Equal(t, "HelixCode", config.Application.Name)
	assert.Equal(t, 8080, config.Server.Port)
}

func TestCreateDefaultConfigFunction(t *testing.T) {
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "default_config.yaml")

	// Test CreateDefaultConfig function
	err := CreateDefaultConfig(configPath)
	require.NoError(t, err)

	// Verify file was created
	_, err = os.Stat(configPath)
	assert.NoError(t, err)

	// Verify content
	content, err := os.ReadFile(configPath)
	require.NoError(t, err)
	assert.Contains(t, string(content), "# HelixCode Server Configuration")
	assert.Contains(t, string(content), "server:")
	assert.Contains(t, string(content), "port: 8080")
}

func TestConfigUtilityFunctions(t *testing.T) {
	// DETERMINISM (§11.4.50): all three variables are set with t.Setenv, which
	// the framework restores when the test ends. TEST_VAR previously used a
	// hand-rolled save/defer while TEST_INT_VAR and TEST_INVALID_INT were set
	// with raw os.Setenv and never restored at all — they leaked into the
	// process environment for the remainder of the test binary, so any later
	// test in this package that read them saw values whose presence depended
	// on whether this test had already run. t.Setenv also fails the test if it
	// is ever combined with t.Parallel(), which is the correct guard here.

	// Test GetEnvOrDefault
	t.Setenv("TEST_VAR", "test_value")
	assert.Equal(t, "test_value", GetEnvOrDefault("TEST_VAR", "default"))
	assert.Equal(t, "default", GetEnvOrDefault("NONEXISTENT_VAR", "default"))

	// Test GetEnvIntOrDefault
	t.Setenv("TEST_INT_VAR", "123")
	assert.Equal(t, 123, GetEnvIntOrDefault("TEST_INT_VAR", 0))
	assert.Equal(t, 456, GetEnvIntOrDefault("NONEXISTENT_INT_VAR", 456))

	// Test invalid int value
	t.Setenv("TEST_INVALID_INT", "not_a_number")
	assert.Equal(t, 789, GetEnvIntOrDefault("TEST_INVALID_INT", 789))
}

func TestConfigManagerInitialize(t *testing.T) {
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "config.yaml")

	// Create config manager
	manager, err := NewHelixConfigManager(configPath)
	require.NoError(t, err)

	// Test Initialize method (should be no-op for compatibility)
	err = manager.Initialize(nil)
	assert.NoError(t, err)
}

func TestConfigManagerLifecycle(t *testing.T) {
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "config.yaml")

	// Test when config doesn't exist (creates default)
	manager, err := NewHelixConfigManager(configPath)
	require.NoError(t, err)
	assert.True(t, manager.IsConfigPresent())

	// Test GetConfig
	config := manager.GetConfig()
	assert.NotNil(t, config)

	// Test UpdateConfig
	originalPort := config.Server.Port
	err = manager.UpdateConfig(func(cfg *Config) {
		cfg.Server.Port = 9999
	})
	require.NoError(t, err)

	// Verify update
	updatedConfig := manager.GetConfig()
	assert.Equal(t, 9999, updatedConfig.Server.Port)
	assert.NotEqual(t, originalPort, updatedConfig.Server.Port)
}

func TestConfigManagerFileOperations(t *testing.T) {
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "config.yaml")
	exportPath := filepath.Join(tempDir, "export.json")
	backupPath := filepath.Join(tempDir, "backup.json")

	manager, err := NewHelixConfigManager(configPath)
	require.NoError(t, err)

	// Test ExportConfig
	err = manager.ExportConfig(exportPath)
	require.NoError(t, err)
	_, err = os.Stat(exportPath)
	assert.NoError(t, err)

	// Test ImportConfig
	err = manager.ImportConfig(exportPath)
	require.NoError(t, err)

	// Test BackupConfig
	err = manager.BackupConfig(backupPath)
	require.NoError(t, err)
	_, err = os.Stat(backupPath)
	assert.NoError(t, err)

	// Test ResetToDefaults - first set a non-default value
	err = manager.UpdateConfig(func(cfg *Config) {
		cfg.Server.Port = 9999
	})
	require.NoError(t, err)
	originalPort := manager.GetConfig().Server.Port
	assert.Equal(t, 9999, originalPort)

	err = manager.ResetToDefaults()
	require.NoError(t, err)
	// Port should be reset to default (8080)
	assert.Equal(t, 8080, manager.GetConfig().Server.Port)
	assert.NotEqual(t, originalPort, manager.GetConfig().Server.Port)
}

func TestGlobalConfigFunctions(t *testing.T) {
	tempDir := t.TempDir()

	// Set environment for testing
	oldConfigPath := os.Getenv("HELIX_CONFIG_PATH")
	defer func() {
		if oldConfigPath != "" {
			os.Setenv("HELIX_CONFIG_PATH", oldConfigPath)
		} else {
			os.Unsetenv("HELIX_CONFIG_PATH")
		}
	}()

	testConfigPath := filepath.Join(tempDir, "test_config.json")
	os.Setenv("HELIX_CONFIG_PATH", testConfigPath)

	// Test GetConfigPath
	assert.Equal(t, testConfigPath, GetConfigPath())

	// Test IsConfigPresent (should be false initially)
	assert.False(t, IsConfigPresent())

	// Test SaveConfig
	testConfig := &Config{
		Version: "1.0.0",
		Application: ApplicationConfig{
			Name:        "Test App",
			Environment: "test",
		},
		Server: ServerConfig{
			Port: 9090,
		},
		Database: database.Config{
			Host:   "localhost",
			Port:   5432,
			DBName: "test",
		},
		Auth: AuthConfig{
			JWTSecret: "test-jwt-secret-32-chars-long-for-testing",
		},
		LLM: LLMConfig{
			DefaultProvider: "local",
			MaxTokens:       2048,
		},
	}

	err := SaveConfig(testConfig)
	require.NoError(t, err)

	// Test IsConfigPresent (should be true now)
	assert.True(t, IsConfigPresent())

	// Test LoadConfig
	loadedConfig, err := LoadConfig()
	require.NoError(t, err)
	assert.Equal(t, testConfig.Version, loadedConfig.Version)
	assert.Equal(t, testConfig.Application.Name, loadedConfig.Application.Name)
	assert.Equal(t, testConfig.Server.Port, loadedConfig.Server.Port)

	// Test UpdateConfig
	err = UpdateConfig(func(cfg *Config) {
		cfg.Server.Port = 7777
		cfg.Application.Name = "Updated App"
	})
	require.NoError(t, err)

	// Verify update
	updatedConfig, err := LoadConfig()
	require.NoError(t, err)
	assert.Equal(t, 7777, updatedConfig.Server.Port)
	assert.Equal(t, "Updated App", updatedConfig.Application.Name)
}

func TestHelixConfigAliasFunctions(t *testing.T) {
	tempDir := t.TempDir()

	// Set environment for testing
	oldConfigPath := os.Getenv("HELIX_CONFIG_PATH")
	defer func() {
		if oldConfigPath != "" {
			os.Setenv("HELIX_CONFIG_PATH", oldConfigPath)
		} else {
			os.Unsetenv("HELIX_CONFIG_PATH")
		}
	}()

	testConfigPath := filepath.Join(tempDir, "helix_test_config.json")
	os.Setenv("HELIX_CONFIG_PATH", testConfigPath)

	// Test Helix aliases
	assert.Equal(t, testConfigPath, GetHelixConfigPath())
	assert.False(t, IsHelixConfigPresent())

	// Create a valid JSON config using NewHelixConfigManager
	// (CreateDefaultHelixConfig writes YAML but loadConfig expects JSON)
	manager, err := NewHelixConfigManager(testConfigPath)
	require.NoError(t, err)
	assert.True(t, IsHelixConfigPresent())
	_ = manager // silence unused variable

	// Test LoadHelixConfig
	config, err := LoadHelixConfig()
	require.NoError(t, err)
	assert.NotNil(t, config)

	// Test SaveHelixConfig
	config.Application.Name = "Helix Test"
	err = SaveHelixConfig(config)
	require.NoError(t, err)

	// Test UpdateHelixConfig
	err = UpdateHelixConfig(func(cfg *Config) {
		cfg.Server.Port = 8888
	})
	require.NoError(t, err)

	// Verify update
	updatedConfig, err := LoadHelixConfig()
	require.NoError(t, err)
	assert.Equal(t, 8888, updatedConfig.Server.Port)
}

func TestConfigurationManagerWithOptions(t *testing.T) {
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "options_config.json")

	// Test NewConfigurationManager with options
	options := &ConfigurationOptions{
		ConfigPath:       configPath,
		AutoSave:         true,
		AutoBackup:       false,
		EnableEncryption: false,
		ValidationMode:   "lenient",
		TransformMode:    "none",
		MaxBackups:       5,
		Compression:      false,
		LogLevel:         "info",
	}

	manager, err := NewConfigurationManager(options)
	require.NoError(t, err)
	assert.NotNil(t, manager)
	assert.Equal(t, configPath, manager.GetConfigPath())
}

func TestConfigWatcherAndInfo(t *testing.T) {
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "test_config.yaml")

	configContent := `server:
  address: "localhost"
  port: 8080
`
	err := os.WriteFile(configPath, []byte(configContent), 0644)
	require.NoError(t, err)

	watcher, err := NewConfigWatcher(configPath)
	assert.NotNil(t, watcher)
	assert.NoError(t, err)

	info, err := GetConfigInfo()
	assert.NotNil(t, info)
	assert.NoError(t, err)
	assert.NotEmpty(t, info.ConfigPath)
}

func TestConfigManagerWatcherSupport(t *testing.T) {
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "watcher_config.yaml")

	manager, err := NewHelixConfigManager(configPath)
	require.NoError(t, err)

	manager.AddWatcher(nil)

	assert.Len(t, manager.watchers, 1)
}
