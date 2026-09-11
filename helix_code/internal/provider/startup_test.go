package provider

import (
	"context"
	"testing"
	"time"
)

// MockProviderBridge implements ProviderBridgeInterface for testing.
type MockProviderBridge struct {
	providers []ProviderEntry
	health    map[string]bool
	err       error
}

func (m *MockProviderBridge) ListProviders() ([]ProviderEntry, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.providers, nil
}

func (m *MockProviderBridge) GetProviderByType(providerType string) (*ProviderEntry, error) {
	for _, p := range m.providers {
		if p.Type == providerType {
			return &p, nil
		}
	}
	return nil, ErrProviderNotFound
}

func (m *MockProviderBridge) GetAllModels() ([]ModelSummary, error) {
	return nil, nil
}

func (m *MockProviderBridge) HealthCheck() (map[string]bool, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.health, nil
}

func TestProviderRegistration_RegisterAllProviders(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name            string
		providers       []ProviderEntry
		health          map[string]bool
		expectedCount   int
		expectError     bool
	}{
		{
			name: "registers all healthy providers",
			providers: []ProviderEntry{
				{Name: "openai", Type: "openai", Endpoint: "https://api.openai.com", Enabled: true},
				{Name: "anthropic", Type: "anthropic", Endpoint: "https://api.anthropic.com", Enabled: true},
			},
			health: map[string]bool{
				"openai":    true,
				"anthropic": true,
			},
			expectedCount: 2,
			expectError:   false,
		},
		{
			name: "registers providers with mixed health",
			providers: []ProviderEntry{
				{Name: "openai", Type: "openai", Endpoint: "https://api.openai.com", Enabled: true},
				{Name: "ollama", Type: "ollama", Endpoint: "http://localhost:11434", Enabled: true},
			},
			health: map[string]bool{
				"openai": true,
				"ollama": false,
			},
			expectedCount: 2,
			expectError:   false,
		},
		{
			name: "handles empty provider list",
			providers: []ProviderEntry{},
			health:    map[string]bool{},
			expectedCount: 0,
			expectError:   false,
		},
		{
			name: "returns error when bridge fails",
			providers: nil,
			health:    nil,
			expectedCount: 0,
			expectError:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bridge := &MockProviderBridge{
				providers: tt.providers,
				health:    tt.health,
				err:       nil,
			}

			if tt.name == "returns error when bridge fails" {
				bridge.err = errors.New("bridge error")
			}

			reg := NewProviderRegistration(bridge)
			count, err := reg.RegisterAllProviders(ctx)

			if tt.expectError {
				if err == nil {
					t.Errorf("Expected error but got none")
				}
				return
			}

			if err != nil {
				t.Errorf("Unexpected error: %v", err)
			}

			if count != tt.expectedCount {
				t.Errorf("Expected %d providers registered, got %d", tt.expectedCount, count)
			}
		})
	}
}

func TestProviderRegistration_Timeout(t *testing.T) {
	ctx := context.Background()
	bridge := &MockProviderBridge{
		providers: []ProviderEntry{
			{Name: "openai", Type: "openai", Endpoint: "https://api.openai.com", Enabled: true},
		},
		health: map[string]bool{"openai": true},
	}

	reg := NewProviderRegistration(bridge)
	reg.timeout = 1 * time.Millisecond // Very short timeout

	count, err := reg.RegisterAllProviders(ctx)
	if err != nil {
		t.Logf("Expected timeout error: %v", err)
	} else {
		t.Logf("Registered %d providers before timeout", count)
	}
}

func TestStartupHook_NilModelManager(t *testing.T) {
	_, err := StartupHook(context.Background(), nil)
	if err == nil {
		t.Error("Expected error for nil model manager")
	}
}

func TestGetProviderBaseDir(t *testing.T) {
	// Test default behavior
	os.Unsetenv("HELIX_PROVIDER_DIR")
	dir := getProviderBaseDir()
	if dir == ".helix" {
		t.Logf("Default dir: %s (expected .helix when no HOME)", dir)
	}

	// Test with env var
	os.Setenv("HELIX_PROVIDER_DIR", "/custom/path")
	dir = getProviderBaseDir()
	if dir != "/custom/path" {
		t.Errorf("Expected /custom/path, got %s", dir)
	}
	os.Unsetenv("HELIX_PROVIDER_DIR")
}

func TestGetProvidersConfigPath(t *testing.T) {
	os.Setenv("HELIX_PROVIDER_DIR", "/test/path")
	path := GetProvidersConfigPath()
	expected := "/test/path/model-aliases.yaml"
	if path != expected {
		t.Errorf("Expected %s, got %s", expected, path)
	}
	os.Unsetenv("HELIX_PROVIDER_DIR")
}

func TestEnsureProvidersConfig(t *testing.T) {
	tmpDir := t.TempDir()
	os.Setenv("HELIX_PROVIDER_DIR", tmpDir)

	err := EnsureProvidersConfig()
	if err != nil {
		t.Errorf("EnsureProvidersConfig failed: %v", err)
	}

	// Verify directory exists
	if _, err := os.Stat(tmpDir); os.IsNotExist(err) {
		t.Error("Config directory was not created")
	}

	os.Unsetenv("HELIX_PROVIDER_DIR")
}