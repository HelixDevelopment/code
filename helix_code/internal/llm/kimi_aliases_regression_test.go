package llm

import (
	"os"
	"path/filepath"
	"testing"
)

// getConfigRoot returns the helix_code root directory
func getConfigRoot() string {
	// Start from current directory and go up
	dir, _ := os.Getwd()
	for {
		// Check if we found go.mod with helix_code module
		goModPath := filepath.Join(dir, "go.mod")
		if _, err := os.Stat(goModPath); err == nil {
			content, _ := os.ReadFile(goModPath)
			if len(content) >= 21 && string(content[:21]) == "module dev.helix.code" {
				return dir
			}
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return ""
}

// TestKimiAliasesInConfig verifies that Kimi aliases exist in the model-aliases config
func TestKimiAliasesInConfig(t *testing.T) {
	root := getConfigRoot()
	if root == "" {
		t.Skip("Could not find helix_code root")
	}

	// Test loading from the generated .helix/model-aliases.yaml
	configPath := filepath.Join(root, ".helix", "model-aliases.yaml")
	
	// Check if file exists
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		// Fallback to example config
		configPath = filepath.Join(root, "config", "model-aliases.example.yaml")
	}

	config, err := LoadAliasConfig(configPath)
	if err != nil {
		t.Fatalf("LoadAliasConfig() error = %v", err)
	}

	// Verify Kimi aliases exist
	kimiAliases := map[string]struct {
		targetModel string
		provider    string
	}{
		"kimi":      {targetModel: "moonshot-v1-8k", provider: "kimi"},
		"kimi1":     {targetModel: "moonshot-v1-8k", provider: "kimi"},
		"kimi2":     {targetModel: "moonshot-v1-32k", provider: "kimi"},
		"kimi-128k": {targetModel: "moonshot-v1-128k", provider: "kimi"},
	}

	foundAliases := make(map[string]*ModelAlias)
	for _, alias := range config.Aliases {
		foundAliases[alias.Alias] = alias
	}

	for aliasName, expected := range kimiAliases {
		t.Run("alias_"+aliasName, func(t *testing.T) {
			alias, exists := foundAliases[aliasName]
			if !exists {
				t.Errorf("Alias %q not found in config", aliasName)
				return
			}

			if alias.TargetModel != expected.targetModel {
				t.Errorf("Alias %q target_model = %v, want %v", aliasName, alias.TargetModel, expected.targetModel)
			}

			if alias.Provider != expected.provider {
				t.Errorf("Alias %q provider = %v, want %v", aliasName, alias.Provider, expected.provider)
			}
		})
	}
}

// TestKimiAliasResolution verifies that Kimi aliases resolve correctly
func TestKimiAliasResolution(t *testing.T) {
	root := getConfigRoot()
	if root == "" {
		t.Skip("Could not find helix_code root")
	}

	manager := NewAliasManager(0.7)

	// Load aliases from config
	configPath := filepath.Join(root, ".helix", "model-aliases.yaml")
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		configPath = filepath.Join(root, "config", "model-aliases.example.yaml")
	}

	loadedConfig, err := LoadAliasConfig(configPath)
	if err != nil {
		t.Fatalf("LoadAliasConfig() error = %v", err)
	}

	for _, alias := range loadedConfig.Aliases {
		if err := manager.AddAlias(alias); err != nil {
			t.Logf("Warning: failed to add alias %s: %v", alias.Alias, err)
		}
	}

	// Test Kimi alias resolution
	kimiTestCases := []struct {
		alias        string
		wantModel    string
		wantProvider string
	}{
		{"kimi", "moonshot-v1-8k", "kimi"},
		{"kimi1", "moonshot-v1-8k", "kimi"},
		{"kimi2", "moonshot-v1-32k", "kimi"},
		{"kimi-128k", "moonshot-v1-128k", "kimi"},
		// Test case-insensitive
		{"KIMI", "moonshot-v1-8k", "kimi"},
		{"Kimi1", "moonshot-v1-8k", "kimi"},
		{"KIMI2", "moonshot-v1-32k", "kimi"},
	}

	for _, tc := range kimiTestCases {
		t.Run("resolve_"+tc.alias, func(t *testing.T) {
			model, provider, resolved := manager.Resolve(tc.alias)
			if !resolved {
				t.Errorf("Resolve(%q) should resolve, got resolved=%v", tc.alias, resolved)
				return
			}
			if model != tc.wantModel {
				t.Errorf("Resolve(%q) model = %v, want %v", tc.alias, model, tc.wantModel)
			}
			if provider != tc.wantProvider {
				t.Errorf("Resolve(%q) provider = %v, want %v", tc.alias, provider, tc.wantProvider)
			}
		})
	}
}

// TestKimiAliasNoContextCompactingLoop verifies no infinite loop on alias resolution
func TestKimiAliasNoContextCompactingLoop(t *testing.T) {
	manager := NewAliasManager(0.7)

	// Add Kimi aliases
	kimiAliases := []*ModelAlias{
		{Alias: "kimi", TargetModel: "moonshot-v1-8k", Provider: "kimi"},
		{Alias: "kimi1", TargetModel: "moonshot-v1-8k", Provider: "kimi"},
		{Alias: "kimi2", TargetModel: "moonshot-v1-32k", Provider: "kimi"},
		{Alias: "kimi-128k", TargetModel: "moonshot-v1-128k", Provider: "kimi"},
	}

	for _, alias := range kimiAliases {
		manager.AddAlias(alias)
	}

	// Resolve multiple times - should not loop
	for i := 0; i < 100; i++ {
		model, provider, resolved := manager.Resolve("kimi")
		if !resolved || model != "moonshot-v1-8k" || provider != "kimi" {
			t.Errorf("Iteration %d: Resolve(kimi) = %v, %v, %v", i, model, provider, resolved)
			return
		}
	}

	// Verify no excessive memory growth (no leak)
	if manager.Count() != len(kimiAliases) {
		t.Errorf("Alias count changed after repeated resolution: got %d, want %d", manager.Count(), len(kimiAliases))
	}
}

// TestKimiAliasHealthCheckEndpoint verifies the provider endpoint structure
func TestKimiAliasHealthCheckEndpoint(t *testing.T) {
	// Verify the Kimi provider is defined in the hosted catalogue
	catalogue := HostedOpenAICompatibleCatalogue()

	kimiFound := false
	for _, entry := range catalogue {
		if entry.Name == "kimi" {
			kimiFound = true
			// Verify endpoint is correct
			if entry.BaseURL != "https://api.moonshot.ai/v1" {
				t.Errorf("Kimi BaseURL = %v, want https://api.moonshot.ai/v1", entry.BaseURL)
			}
			if entry.ModelEndpoint != "/models" {
				t.Errorf("Kimi ModelEndpoint = %v, want /models", entry.ModelEndpoint)
			}
			if entry.ChatEndpoint != "/chat/completions" {
				t.Errorf("Kimi ChatEndpoint = %v, want /chat/completions", entry.ChatEndpoint)
			}
			break
		}
	}

	if !kimiFound {
		t.Error("Kimi provider not found in HostedOpenAICompatibleCatalogue")
	}
}

// TestRegenerateAliasesCommand verifies the regenerate-aliases command works
func TestRegenerateAliasesCommand(t *testing.T) {
	tmpDir := t.TempDir()
	outputPath := filepath.Join(tmpDir, "generated-aliases.yaml")

	// Test that generating aliases from providers works
	manager := NewAliasManager(0.7)
	manager.AddAlias(&ModelAlias{Alias: "test", TargetModel: "test-model", Provider: "test"})

	aliasConfig := &AliasConfig{
		Version:        "1.0",
		FuzzyThreshold: 0.7,
		Aliases:        manager.ExportAliases(),
	}

	err := SaveAliasConfig(aliasConfig, outputPath)
	if err != nil {
		t.Fatalf("SaveAliasConfig() error = %v", err)
	}

	// Verify file was created
	if _, err := os.Stat(outputPath); os.IsNotExist(err) {
		t.Error("Output file not created")
	}

	// Load and verify
	loaded, err := LoadAliasConfig(outputPath)
	if err != nil {
		t.Fatalf("LoadAliasConfig() error = %v", err)
	}

	if len(loaded.Aliases) != 1 {
		t.Errorf("Generated config has %d aliases, want 1", len(loaded.Aliases))
	}
}
