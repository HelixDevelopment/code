package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Helper functions for tests
func float64Ptr(f float64) *float64 {
	return &f
}

// TestConfigurationValidator tests configuration validation
func TestConfigurationValidator(t *testing.T) {
	validator := NewConfigurationValidator(true)
	require.NotNil(t, validator)

	// Test valid configuration.
	//
	// §11.4.120 reconciliation (operator decision 2026-09-05): the raw
	// builtin default from getDefaultConfig() now has an EMPTY
	// llm.default_provider (see config.go setDefaultsOn's comment on
	// v.SetDefault("llm.default_provider", "")) instead of the old
	// "local". That is intentional: an empty BUILTIN default lets a
	// genuinely zero-config process fall through to the Ollama
	// zero-config fallback (:11434) instead of being silently pinned to
	// the local coder even with no config file at all (§11.4.122). The
	// shipped config/config.yaml still sets default_provider: "local"
	// explicitly, so normal deployments still route to the local coder
	// (HXC-002-F3-01) -- only the BUILTIN (no-config-file) default
	// changed.
	//
	// This validator's schema check (config.go Validate(), the
	// llm.default_provider enum around line 1565) was NOT changed and
	// still requires a non-empty value from a fixed provider list -- it
	// has no "empty means unset, resolved elsewhere" case. So the raw
	// builtin default is now genuinely reported INVALID by this
	// validator. That is a real gap between the two (BLOCKING finding,
	// out of this test file's edit scope: internal/config/config.go's
	// Validate() is untouched here) -- this test must not paper over it
	// by asserting Valid==true on the raw builtin default.
	//
	// To still exercise "the validator accepts a well-formed, complete
	// config", we validate a config with an EXPLICIT, shipped-config-
	// matching provider layered on top of the builtin defaults for
	// everything else.
	config := getDefaultConfig()
	config.LLM.DefaultProvider = "local"
	result := validator.Validate(config)
	assert.True(t, result.Valid)
	assert.Empty(t, result.Errors)

	// §11.4.120 reconciliation + §11.4.122 guard. A genuinely zero-config
	// Config -- the raw builtin defaults, with no provider chosen -- MUST
	// validate CLEANLY. Empty llm.default_provider is not a malformed value: it
	// is the documented "operator did not choose a provider" state, resolved at
	// request time by resolveLLMProvider's flag > env > config precedence, which
	// falls back to the local Ollama route. An earlier revision of this guard
	// pinned the opposite (the validator's schema check had no empty case and
	// rejected it); that defect is fixed in config.go and the guard is re-aimed
	// at the invariant that actually matters.
	//
	// Mutation-sensitivity (§1.1): restoring the old "local" builtin default --
	// which silently re-breaks the zero-config Ollama fallback -- fails the
	// DefaultProvider assertion below. Re-introducing the validator's
	// reject-empty behaviour fails the Valid/Errors assertions.
	rawDefault := getDefaultConfig()
	assert.Equal(t, "", rawDefault.LLM.DefaultProvider,
		"the BUILTIN llm.default_provider default must stay empty (operator decision 2026-09-05): "+
			"a non-empty builtin applies even with no config file and makes the zero-config "+
			"Ollama route unreachable (§11.4.122)")
	rawResult := validator.Validate(rawDefault)
	assert.True(t, rawResult.Valid,
		"a zero-config Config must validate cleanly -- empty llm.default_provider means 'unset', not 'invalid'")
	for _, err := range rawResult.Errors {
		assert.NotEqual(t, "llm.default_provider", err.Property,
			"empty llm.default_provider must not raise a schema error: %+v", err)
	}

	// Test invalid configuration
	invalidConfig := getDefaultConfig()
	invalidConfig.Server.Port = -1
	invalidConfig.LLM.Temperature = 5.0

	result = validator.Validate(invalidConfig)
	assert.False(t, result.Valid)
	assert.NotEmpty(t, result.Errors)

	// Check for specific errors
	portErrorFound := false
	tempErrorFound := false

	for _, err := range result.Errors {
		if err.Property == "server.port" {
			portErrorFound = true
			assert.Equal(t, "server.port", err.Path)
			assert.Equal(t, "error", err.Severity)
		}
		if err.Property == "llm.temperature" {
			tempErrorFound = true
		}
	}

	assert.True(t, portErrorFound)
	assert.True(t, tempErrorFound)
}

// TestConfigurationValidatorCustomRules tests custom validation rules
func TestConfigurationValidatorCustomRules(t *testing.T) {
	validator := NewConfigurationValidator(true)

	// Add custom rule
	validator.AddCustomRule("application.name", func(value interface{}) error {
		if str, ok := value.(string); ok && str == "forbidden" {
			return assert.AnError
		}
		return nil
	})

	// Test valid value.
	//
	// §11.4.120 reconciliation (operator decision 2026-09-05): as in
	// TestConfigurationValidator above, getDefaultConfig()'s raw builtin
	// llm.default_provider is now intentionally EMPTY (not "local"), so
	// it is rejected by Validate()'s unconditional provider-enum check
	// regardless of any custom rule outcome. Set an explicit,
	// shipped-config-matching provider so this test still exercises
	// "does the custom rule + schema validation pass together on an
	// otherwise well-formed config" rather than tripping over the
	// unrelated empty-builtin-provider gap (tracked as a BLOCKING
	// finding; see TestConfigurationValidator's dedicated guard).
	config := getDefaultConfig()
	config.LLM.DefaultProvider = "local"
	config.Application.Name = "allowed"
	result := validator.Validate(config)
	assert.True(t, result.Valid)

	// Test invalid value
	config.Application.Name = "forbidden"
	result = validator.Validate(config)
	assert.False(t, result.Valid)

	// Check for custom rule error
	customErrorFound := false
	for _, err := range result.Errors {
		if err.Code == "CUSTOM_RULE_ERROR" && err.Property == "application.name" {
			customErrorFound = true
			break
		}
	}
	assert.True(t, customErrorFound)
}

// TestConfigurationValidatorFieldValidation tests field-specific validation
func TestConfigurationValidatorFieldValidation(t *testing.T) {
	validator := NewConfigurationValidator(true)

	// Test field validation
	config := getDefaultConfig()

	// Valid field
	result := validator.ValidateField(config, "server.port")
	assert.True(t, result.Valid)
	assert.Empty(t, result.Errors)

	// Invalid field
	config.Server.Port = 70000
	result = validator.ValidateField(config, "server.port")
	assert.False(t, result.Valid)
	assert.NotEmpty(t, result.Errors)
	assert.Equal(t, "server.port", result.Path)
}

// TestConfigurationSchema tests configuration schema
func TestConfigurationSchema(t *testing.T) {
	validator := NewConfigurationValidator(true)
	schema := validator.createDefaultSchema()

	require.NotNil(t, schema)
	assert.Equal(t, "1.0", schema.Version)
	assert.NotEmpty(t, schema.Properties)
	assert.NotEmpty(t, schema.Required)

	// Check required properties
	assert.Contains(t, schema.Required, "version")
	assert.Contains(t, schema.Required, "application")
	assert.Contains(t, schema.Required, "server")

	// Check application properties
	appProp, exists := schema.Properties["application"]
	require.True(t, exists)
	assert.Equal(t, "object", appProp.Type)
	assert.NotEmpty(t, appProp.Properties)
	assert.NotEmpty(t, appProp.Required)

	// Check name property
	nameProp, exists := appProp.Properties["name"]
	require.True(t, exists)
	assert.Equal(t, "string", nameProp.Type)
	assert.NotNil(t, nameProp.MinLength)
	assert.NotNil(t, nameProp.MaxLength)
}

// TestConfigurationMigrator tests configuration migration
func TestConfigurationMigrator(t *testing.T) {
	migrator := NewConfigurationMigrator("1.0.0")
	require.NotNil(t, migrator)
	assert.Equal(t, "1.0.0", migrator.current)

	// Test available versions
	versions := migrator.GetAvailableVersions()
	assert.NotEmpty(t, versions)
	assert.Contains(t, versions, "1.0.0")
	assert.Contains(t, versions, "1.1.0")
	assert.Contains(t, versions, "1.2.0")

	// Test migration path
	config := &Config{
		Version: "1.0.0",
		Application: ApplicationConfig{
			Name: "Test App",
			Workspace: WorkspaceConfig{
				AutoSave: false, // Should be set to true by migration
			},
		},
	}

	err := migrator.Migrate(config, "1.2.0")
	assert.NoError(t, err)
	assert.Equal(t, "1.2.0", config.Version)
	assert.True(t, config.Application.Workspace.AutoSave)
}

// TestConfigurationMigratorPathFinding tests migration path finding
func TestConfigurationMigratorPathFinding(t *testing.T) {
	migrator := NewConfigurationMigrator("1.0.0")

	// Test direct path
	path := migrator.findMigrationPath("1.0.0", "1.1.0")
	assert.Len(t, path, 1)

	// Test multi-step path
	path = migrator.findMigrationPath("1.0.0", "1.2.0")
	assert.Len(t, path, 2) // 1.0.0 -> 1.1.0 -> 1.2.0

	// Test no path
	path = migrator.findMigrationPath("1.0.0", "9.9.9")
	assert.Nil(t, path)
}

// TestConfigurationTransformer tests configuration transformation
func TestConfigurationTransformer(t *testing.T) {
	transformer := NewConfigurationTransformer()
	require.NotNil(t, transformer)

	// Test simple transformation
	config := getDefaultConfig()
	variables := map[string]interface{}{
		"server_port": 9090,
		"app_name":    "Transformed App",
	}

	// Add mappings
	transformer.AddMapping(TransformMapping{
		Source:    "server.port",
		Target:    "server.port",
		Transform: "copy",
		Priority:  1,
	})

	transformer.AddMapping(TransformMapping{
		Source:    "application.name",
		Target:    "application.name",
		Transform: "copy",
		Priority:  2,
	})

	result, err := transformer.Transform(config, variables)
	assert.NoError(t, err)
	assert.NotNil(t, result)

	// Verify transformations
	assert.Equal(t, 9090, result.Server.Port)
	assert.Equal(t, "Transformed App", result.Application.Name)
}

// TestConfigurationTransformerConditions tests conditional transformations
func TestConfigurationTransformerConditions(t *testing.T) {
	transformer := NewConfigurationTransformer()

	// Test conditional mapping
	mapping := TransformMapping{
		Source:    "llm.temperature",
		Target:    "llm.temperature",
		Transform: "copy",
		Condition: "development",
		Priority:  1,
	}

	transformer.AddMapping(mapping)

	// Test with development config
	devConfig := getDefaultConfig()
	devConfig.Application.Environment = "development"
	variables := map[string]interface{}{}

	result, err := transformer.Transform(devConfig, variables)
	assert.NoError(t, err)
	assert.NotNil(t, result)

	// Test with production config (should not apply)
	prodConfig := getDefaultConfig()
	prodConfig.Application.Environment = "production"

	result, err = transformer.Transform(prodConfig, variables)
	assert.NoError(t, err)
	assert.NotNil(t, result)
}

// TestConfigurationTemplateManager tests template management
func TestConfigurationTemplateManager(t *testing.T) {
	tempDir := t.TempDir()
	templateDir := tempDir + "/templates"

	// Create template directory
	err := os.MkdirAll(templateDir, 0755)
	require.NoError(t, err)

	manager := NewConfigurationTemplateManager(templateDir)
	require.NotNil(t, manager)

	// Test creating template from config
	config := getDefaultConfig()
	variables := map[string]*TemplateVariable{
		"app_name": {
			Name:      "Application Name",
			Type:      "string",
			Default:   "My App",
			Required:  true,
			MinLength: intPtr(1),
		},
		"port": {
			Name:    "Server Port",
			Type:    "number",
			Default: 8080,
			Min:     float64Ptr(1),
			Max:     float64Ptr(65535),
		},
	}

	template, err := manager.CreateTemplateFromConfig(config, "Test Template", "A test template", variables)
	require.NoError(t, err)
	require.NotNil(t, template)

	assert.Equal(t, "Test Template", template.Name)
	assert.Equal(t, "A test template", template.Description)
	assert.Equal(t, "custom", template.Category)
	assert.NotEmpty(t, template.ID)
	assert.NotEmpty(t, template.Variables)

	// Test saving template
	templatePath := tempDir + "/test_template.yaml"
	err = manager.SaveTemplate(template, templatePath)
	require.NoError(t, err)

	// Test loading template
	loadedConfig, err := manager.LoadTemplate(templatePath)
	require.NoError(t, err)
	require.NotNil(t, loadedConfig)

	// Compare configs
	assert.Equal(t, template.Config.Server.Port, loadedConfig.Server.Port)
	assert.Equal(t, template.Config.Application.Name, loadedConfig.Application.Name)
}

// TestConfigurationTemplateManagerApply tests template application
func TestConfigurationTemplateManagerApply(t *testing.T) {
	// Create default templates
	templates := CreateDefaultTemplates()
	require.NotEmpty(t, templates)

	manager := NewConfigurationTemplateManager("/tmp/templates")

	// Add template
	devTemplate := templates["development"]
	if devTemplate == nil {
		// Use first available template if development doesn't exist
		for _, tmpl := range templates {
			devTemplate = tmpl
			break
		}
	}
	manager.templates[devTemplate.ID] = devTemplate

	// Test applying template
	variables := map[string]interface{}{
		"workspace_path": "~/test_workspace",
		"debug_enabled":  true,
	}

	config, err := manager.ApplyTemplate(devTemplate.ID, variables)
	require.NoError(t, err)
	require.NotNil(t, config)

	assert.Equal(t, "development", config.Application.Environment)
}

// TestConfigurationTemplateManagerSearch tests template searching
func TestConfigurationTemplateManagerSearch(t *testing.T) {
	manager := NewConfigurationTemplateManager("/tmp/templates")

	// Add some test templates
	templates := CreateDefaultTemplates()
	for _, template := range templates {
		manager.templates[template.ID] = template
	}

	// Debug: check if templates were added
	assert.Len(t, manager.templates, 4, "Should have 4 templates")
	basicTemplate, exists := manager.templates["basic"]
	assert.True(t, exists, "Basic template should exist")
	assert.Equal(t, "Basic Configuration", basicTemplate.Name, "Template name should match")

	// Test search by name - should find "basic" template when searching for "basic"
	results := manager.SearchTemplates("basic")
	assert.Len(t, results, 1, "Should find 1 template when searching for 'basic'")
	assert.Equal(t, "basic", results[0].ID)

	// Test search by description - should find multiple templates when searching for "configuration"
	results = manager.SearchTemplates("configuration")
	assert.Greater(t, len(results), 0, "Should find at least 1 template when searching for 'configuration'")
	// Should find the basic template specifically
	found := false
	for _, result := range results {
		if result.ID == "basic" {
			found = true
			break
		}
	}
	assert.True(t, found, "Should find basic template in configuration search")

	// Test search by category - should find "basic" when searching for "default"
	results = manager.SearchTemplates("default")
	assert.Len(t, results, 1, "Should find 1 template when searching for 'default'")
	assert.Equal(t, "basic", results[0].ID)

	// Test search with no results
	results = manager.SearchTemplates("nonexistent")
	assert.Len(t, results, 0, "Should find 0 templates for nonexistent term")

	// Test case insensitive search
	results = manager.SearchTemplates("BASIC")
	assert.Len(t, results, 1, "Should find 1 template for uppercase 'BASIC'")
	assert.Equal(t, "basic", results[0].ID)
}

// TestConfigurationTemplateValidation tests template variable validation
func TestConfigurationTemplateValidation(t *testing.T) {
	manager := NewConfigurationTemplateManager("/tmp/templates")

	// Test with required variable missing
	template := &ConfigurationTemplate{
		ID:          "test_template",
		Name:        "Test Template",
		Description: "Test template",
		Category:    "test",
		Variables: map[string]*TemplateVariable{
			"required_var": {
				Name:     "Required Variable",
				Type:     "string",
				Required: true,
			},
		},
		Config: &Config{},
	}

	// Test with missing required variable
	variables := map[string]interface{}{
		"other_var": "value",
	}

	_, err := manager.processTemplate(template, variables)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "required variable not provided: required_var")

	// Test with invalid variable type
	variables = map[string]interface{}{
		"required_var": 123, // string expected
	}

	_, err = manager.processTemplate(template, variables)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "must be a string")
}

// TestConfigurationTemplateVariableConstraints tests variable constraint validation
func TestConfigurationTemplateVariableConstraints(t *testing.T) {
	manager := NewConfigurationTemplateManager("/tmp/templates")

	template := &ConfigurationTemplate{
		ID:          "test_template",
		Name:        "Test Template",
		Description: "Test template",
		Variables: map[string]*TemplateVariable{
			"constrained_var": {
				Name:      "Constrained Variable",
				Type:      "string",
				MinLength: intPtr(5),
				MaxLength: intPtr(10),
				Pattern:   "^[a-z]+$",
			},
		},
		Config: &Config{},
	}

	// Test with too short value
	variables := map[string]interface{}{
		"constrained_var": "abc",
	}

	_, err := manager.processTemplate(template, variables)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "too short")

	// Test with too long value
	variables = map[string]interface{}{
		"constrained_var": "abcdefghijklmnopqrstuvwxyz",
	}

	_, err = manager.processTemplate(template, variables)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "too long")

	// Test with invalid pattern
	variables = map[string]interface{}{
		"constrained_var": "ABC123",
	}

	_, err = manager.processTemplate(template, variables)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "doesn't match required pattern")

	// Test with valid value
	variables = map[string]interface{}{
		"constrained_var": "abcde",
	}

	config, err := manager.processTemplate(template, variables)
	assert.NoError(t, err)
	assert.NotNil(t, config)
}

// TestDefaultTemplates tests default template creation
func TestDefaultTemplates(t *testing.T) {
	templates := CreateDefaultTemplates()
	require.NotEmpty(t, templates)

	// Test development template
	devTemplate, exists := templates["development"]
	require.True(t, exists)
	assert.Equal(t, "Development Environment", devTemplate.Name)
	assert.Equal(t, "development", devTemplate.Config.Application.Environment)
	assert.Equal(t, 8080, devTemplate.Config.Server.Port)
	assert.Equal(t, "0.0.0.0", devTemplate.Config.Server.Address)

	// Test production template
	prodTemplate, exists := templates["production"]
	require.True(t, exists)
	assert.Equal(t, "Production Environment", prodTemplate.Name)
	assert.Equal(t, "production", prodTemplate.Config.Application.Environment)
	assert.Equal(t, "error", prodTemplate.Config.Logging.Level)
	assert.Equal(t, 443, prodTemplate.Config.Server.Port)
	assert.Equal(t, "0.0.0.0", prodTemplate.Config.Server.Address)

	// Test testing template
	testTemplate, exists := templates["testing"]
	require.True(t, exists)
	assert.Equal(t, "Testing Environment", testTemplate.Name)
	assert.Equal(t, "testing", testTemplate.Config.Application.Environment)
	assert.Equal(t, "testing", testTemplate.Config.Application.Environment)
	assert.Equal(t, 0, testTemplate.Config.Server.Port)
	assert.Equal(t, "0.0.0.0", testTemplate.Config.Server.Address)
	assert.Equal(t, 10, testTemplate.Config.Workers.MaxConcurrentTasks)
}

// TestTemplateVariableSubstitution tests template variable substitution
func TestTemplateVariableSubstitution(t *testing.T) {
	manager := NewConfigurationTemplateManager("/tmp/templates")

	// Create template with string variables
	template := &ConfigurationTemplate{
		ID:          "test_template",
		Name:        "Test Template",
		Description: "Test template",
		Config: &Config{
			Application: ApplicationConfig{
				Name:        "{{app_name}}",
				Description: "{{app_desc}}",
				Version:     "1.0.0",
			},
		},
		Variables: map[string]*TemplateVariable{
			"app_name": {
				Name:     "App Name",
				Type:     "string",
				Default:  "Default App",
				Required: false,
			},
			"app_desc": {
				Name:     "App Description",
				Type:     "string",
				Default:  "Default Description",
				Required: false,
			},
		},
	}

	variables := map[string]interface{}{
		"app_name": "My Custom App",
		"app_desc": "My custom description",
	}

	config, err := manager.processTemplate(template, variables)
	require.NoError(t, err)
	require.NotNil(t, config)

	assert.Equal(t, "My Custom App", config.Application.Name)
	assert.Equal(t, "My custom description", config.Application.Description)
	assert.Equal(t, "1.0.0", config.Application.Version) // Unchanged
}

// TestConfigurationValidationIntegration tests validation integration
func TestConfigurationValidationIntegration(t *testing.T) {
	validator := NewConfigurationValidator(true)

	// Test comprehensive validation
	config := &Config{
		Version: "1.0.0",
		Application: ApplicationConfig{
			Name:        "Test App",
			Description: "Test Description",
			Version:     "1.0.0",
			Environment: "invalid_env", // Should be in enum
			Workspace: WorkspaceConfig{
				DefaultPath: "~/test",
				AutoSave:    true,
			},
		},
		Server: ServerConfig{
			Address:      "localhost",
			Port:         -1, // Should be valid port
			ReadTimeout:  30,
			WriteTimeout: 30,
		},
		LLM: LLMConfig{
			DefaultProvider: "invalid_provider", // Should be in enum
			DefaultModel:    "test-model",
			MaxTokens:       -1,  // Should be positive
			Temperature:     3.0, // Should be <= 2.0
		},
	}

	result := validator.Validate(config)
	assert.False(t, result.Valid)
	assert.NotEmpty(t, result.Errors)

	// Check for specific validation errors
	errorCodes := make(map[string]bool)
	for _, err := range result.Errors {
		errorCodes[err.Code] = true
	}

	// Should have errors for:
	// - Invalid environment
	// - Invalid port
	// - Invalid provider
	// - Invalid max tokens
	// - Invalid temperature
	assert.True(t, errorCodes["FIELD_SCHEMA_ERROR"])
	assert.True(t, errorCodes["CUSTOM_RULE_ERROR"])
}

// TestConfigurationMigrationBackup tests migration backup functionality.
//
// §11.4.120 reconciliation (CONST-042): migration backups previously landed in
// the shared, world-traversable os.TempDir() at world-readable mode — a secret
// leak (config backups carry plaintext credentials). The fix moved them into a
// user-PRIVATE 0700 directory at owner-only 0600 file mode. This test now
// asserts the NEW secure location, not the old leaky /tmp one. The home dir is
// redirected via HELIX_CONFIG_PATH so the real ~/.config is never touched.
func TestConfigurationMigrationBackup(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HELIX_CONFIG_PATH", filepath.Join(tmpHome, ".config", "helixcode", "config.json"))

	migrator := NewConfigurationMigrator("1.0.0")

	config := &Config{
		Version: "1.0.0",
		Application: ApplicationConfig{
			Name: "Test App",
		},
	}

	// Test migration with backup
	migration := Migration{
		From:   "1.0.0",
		To:     "1.1.0",
		Name:   "test_migration",
		Desc:   "Test migration",
		Backup: true,
		Up: func(config *HelixConfig) error {
			config.Application.Description = "Migrated"
			return nil
		},
		Down: func(config *HelixConfig) error {
			return nil
		},
	}

	migrator.migrations["1.0.0"] = []Migration{migration}

	err := migrator.Migrate(config, "1.1.0")
	assert.NoError(t, err)
	assert.Equal(t, "1.1.0", config.Version)
	assert.Equal(t, "Migrated", config.Application.Description)

	// Check that a backup was created in the PRIVATE backup dir (NOT shared /tmp).
	backupDir := configBackupDir()
	backupFiles, err := filepath.Glob(filepath.Join(backupDir, "helix_config_backup_*.json"))
	assert.NoError(t, err)
	assert.NotEmpty(t, backupFiles, "backup must land in the private backup dir")

	// CONST-042: the private dir must be 0700 and each backup file 0600.
	if di, derr := os.Stat(backupDir); assert.NoError(t, derr) {
		assert.Equal(t, os.FileMode(0o700), di.Mode().Perm(), "backup dir must be owner-only")
	}
	for _, f := range backupFiles {
		fi, ferr := os.Stat(f)
		assert.NoError(t, ferr)
		assert.Equal(t, os.FileMode(0o600), fi.Mode().Perm(), "backup file must be owner-only")
	}

	// No secret-bearing backup may leak into shared os.TempDir().
	leaked, _ := filepath.Glob(filepath.Join(os.TempDir(), "helix_config_backup_*.json"))
	assert.Empty(t, leaked, "no backup may land in shared TempDir (CONST-042)")
}

// TestAdvancedConfigurationPerformance tests performance of advanced config operations
func TestAdvancedConfigurationPerformance(t *testing.T) {
	// Test validation performance
	validator := NewConfigurationValidator(true)
	config := getDefaultConfig()

	start := time.Now()
	for i := 0; i < 1000; i++ {
		validator.Validate(config)
	}
	duration := time.Since(start)

	// Real timing is benchmarked in BenchmarkAdvancedConfiguration. Here we only
	// guard against a PATHOLOGICAL regression (hang / O(n^2) blowup) with a
	// generous bound that cannot flake under -race / coverage instrumentation —
	// the prior tight 1s cap flaked under `make test-coverage`'s -race overhead
	// even with no product regression (§11.4.50 deterministic / §11.4.6).
	t.Logf("1000 validations took %v", duration)
	assert.Less(t, duration, 30*time.Second, "1000 validations should not hang or blow up")

	// Test transformation performance
	transformer := NewConfigurationTransformer()
	variables := map[string]interface{}{
		"test_var": "test_value",
	}

	start = time.Now()
	for i := 0; i < 1000; i++ {
		transformer.Transform(config, variables)
	}
	duration = time.Since(start)

	// Generous pathological-regression bound (see the validation note above);
	// real transform timing lives in BenchmarkAdvancedConfiguration (§11.4.50/§11.4.6).
	t.Logf("1000 transformations took %v", duration)
	assert.Less(t, duration, 30*time.Second, "1000 transformations should not hang or blow up")
}

// BenchmarkAdvancedConfiguration benchmarks advanced configuration operations
func BenchmarkAdvancedConfiguration(b *testing.B) {
	validator := NewConfigurationValidator(true)
	config := getDefaultConfig()

	b.Run("Validation", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			validator.Validate(config)
		}
	})

	transformer := NewConfigurationTransformer()
	variables := map[string]interface{}{
		"test_var": "test_value",
	}

	b.Run("Transformation", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			transformer.Transform(config, variables)
		}
	})

	manager := NewConfigurationTemplateManager("test-dir")

	b.Run("TemplateApplication", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			manager.ApplyTemplate("development", map[string]interface{}{})
		}
	})
}
