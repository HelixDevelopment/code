package llm

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewProviderFactory(t *testing.T) {
	// §11.4.120 gate reconciliation: NewProvider now enforces the W2c-1 cloud
	// gate (factory.go), closing the ungated back door around NewCloudProvider's
	// guard. This test's subject is provider CONSTRUCTION and behaviour, which
	// predates the gate — so the refusal it now hits is the fix working, not a
	// regression, and the honest response is to open the gate here rather than
	// weaken either side. Gate POLICY itself is asserted by
	// cloud_gate_closed_test.go / cloud_gate_open_test.go.
	openCloudGateForTest(t)

	tests := []struct {
		name     string
		config   ProviderConfigEntry
		hasError bool
	}{
		{
			name: "OpenAI provider",
			config: ProviderConfigEntry{
				Type:    ProviderTypeOpenAI,
				APIKey:  "test-key",
				Enabled: true,
			},
			hasError: false,
		},
		{
			name: "Anthropic provider",
			config: ProviderConfigEntry{
				Type:    ProviderTypeAnthropic,
				APIKey:  "test-key",
				Enabled: true,
			},
			hasError: false,
		},
		{
			name: "Gemini provider",
			config: ProviderConfigEntry{
				Type:    ProviderTypeGemini,
				APIKey:  "test-key",
				Enabled: true,
			},
			hasError: false,
		},
		{
			name: "Ollama provider",
			config: ProviderConfigEntry{
				Type:     ProviderTypeOllama,
				Endpoint: "http://localhost:11434",
				Models:   []string{"llama2"},
				Enabled:  true,
			},
			hasError: false,
		},
		{
			name: "Invalid provider type",
			config: ProviderConfigEntry{
				Type:    ProviderType("invalid"),
				Enabled: true,
			},
			hasError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			provider, err := NewProvider(tt.config)

			if tt.hasError {
				assert.Error(t, err)
				assert.Nil(t, provider)
				return
			}

			assert.NoError(t, err)
			assert.NotNil(t, provider)
		})
	}
}

func TestModelManagerRegisterProvider(t *testing.T) {
	manager := NewModelManager()

	// Create a mock provider
	mockProvider := &MockRegTestProvider{
		name:         "test-provider",
		providerType: ProviderTypeOpenAI,
		models: []ModelInfo{
			{Name: "test-model-1", Provider: ProviderTypeOpenAI},
			{Name: "test-model-2", Provider: ProviderTypeOpenAI},
		},
		available: true,
	}

	// Register provider
	err := manager.RegisterProvider(mockProvider)
	require.NoError(t, err)

	// Verify provider was registered
	provider, err := manager.GetProviderForModel("test-model-1", ProviderTypeOpenAI)
	require.NoError(t, err)
	assert.NotNil(t, provider)
	assert.Equal(t, "test-provider", provider.GetName())

	// Test registering duplicate provider
	err = manager.RegisterProvider(mockProvider)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "already registered")
}

func TestModelManagerGetAvailableModels(t *testing.T) {
	manager := NewModelManager()

	// Initially empty
	models := manager.GetAvailableModels()
	assert.Len(t, models, 0)

	// Register provider with models
	mockProvider := &MockRegTestProvider{
		name:         "test-provider",
		providerType: ProviderTypeOpenAI,
		models: []ModelInfo{
			{Name: "model-1", Provider: ProviderTypeOpenAI},
			{Name: "model-2", Provider: ProviderTypeOpenAI},
		},
		available: true,
	}

	err := manager.RegisterProvider(mockProvider)
	require.NoError(t, err)

	// Should now have models
	models = manager.GetAvailableModels()
	assert.Len(t, models, 2)
}

func TestModelManagerHealthCheck(t *testing.T) {
	manager := NewModelManager()

	// Register healthy provider
	healthyProvider := &MockRegTestProvider{
		name:         "healthy-provider",
		providerType: ProviderTypeOpenAI,
		models: []ModelInfo{
			{Name: "test-model", Provider: ProviderTypeOpenAI},
		},
		available: true,
		healthy:   true,
	}

	err := manager.RegisterProvider(healthyProvider)
	require.NoError(t, err)

	// Perform health check
	ctx := context.Background()
	health := manager.HealthCheck(ctx)

	assert.NotNil(t, health)
	assert.Contains(t, health, ProviderTypeOpenAI)

	providerHealth := health[ProviderTypeOpenAI]
	assert.Equal(t, "healthy", providerHealth.Status)
}

func TestModelManagerSelectOptimalModel(t *testing.T) {
	manager := NewModelManager()

	// Register provider with multiple models
	mockProvider := &MockRegTestProvider{
		name:         "test-provider",
		providerType: ProviderTypeOpenAI,
		models: []ModelInfo{
			{
				Name:         "small-model",
				Provider:     ProviderTypeOpenAI,
				ContextSize:  4096,
				Capabilities: []ModelCapability{CapabilityTextGeneration},
			},
			{
				Name:         "large-model",
				Provider:     ProviderTypeOpenAI,
				ContextSize:  32000,
				Capabilities: []ModelCapability{CapabilityTextGeneration, CapabilityCodeGeneration},
			},
		},
		available: true,
	}

	err := manager.RegisterProvider(mockProvider)
	require.NoError(t, err)

	// Select model with criteria
	criteria := ModelSelectionCriteria{
		TaskType:             "code_generation",
		RequiredCapabilities: []ModelCapability{CapabilityCodeGeneration},
		MaxTokens:            8000,
	}

	model, err := manager.SelectOptimalModel(criteria)
	require.NoError(t, err)
	assert.NotNil(t, model)
	assert.Equal(t, "large-model", model.Name)
}

func TestModelManagerGetModelsByCapability(t *testing.T) {
	manager := NewModelManager()

	// Register provider with models having different capabilities
	mockProvider := &MockRegTestProvider{
		name:         "test-provider",
		providerType: ProviderTypeOpenAI,
		models: []ModelInfo{
			{
				Name:         "text-model",
				Provider:     ProviderTypeOpenAI,
				Capabilities: []ModelCapability{CapabilityTextGeneration},
			},
			{
				Name:         "code-model",
				Provider:     ProviderTypeOpenAI,
				Capabilities: []ModelCapability{CapabilityTextGeneration, CapabilityCodeGeneration},
			},
		},
		available: true,
	}

	err := manager.RegisterProvider(mockProvider)
	require.NoError(t, err)

	// Get models with code generation capability
	models := manager.GetModelsByCapability([]ModelCapability{CapabilityCodeGeneration})
	assert.Len(t, models, 1)
	assert.Equal(t, "code-model", models[0].Name)

	// Get models with text generation capability
	models = manager.GetModelsByCapability([]ModelCapability{CapabilityTextGeneration})
	assert.Len(t, models, 2)
}

func TestModelManagerNoModelsAvailable(t *testing.T) {
	manager := NewModelManager()

	// Try to select model when none available
	criteria := ModelSelectionCriteria{
		TaskType: "code_generation",
	}

	model, err := manager.SelectOptimalModel(criteria)
	assert.Error(t, err)
	assert.Nil(t, model)
	assert.Contains(t, err.Error(), "no models available")
}

func TestModelManagerProviderNotFound(t *testing.T) {
	manager := NewModelManager()

	// Try to get provider for non-existent model
	_, err := manager.GetProviderForModel("non-existent-model", ProviderTypeOpenAI)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestInitializeModelManager(t *testing.T) {
	// Test with empty configs
	configs := []ProviderConfigEntry{}
	manager, err := InitializeModelManager(configs)
	require.NoError(t, err)
	assert.NotNil(t, manager)

	// Test with disabled provider
	configs = []ProviderConfigEntry{
		{
			Type:    ProviderTypeOpenAI,
			APIKey:  "test-key",
			Enabled: false, // Disabled
		},
	}
	manager, err = InitializeModelManager(configs)
	require.NoError(t, err)
	assert.NotNil(t, manager)
	// Provider shouldn't be registered since it's disabled
	models := manager.GetAvailableModels()
	assert.Len(t, models, 0)
}

func TestCrossProviderRegistryBasic(t *testing.T) {
	// Create temporary directory for registry
	tmpDir := t.TempDir()

	registry := NewCrossProviderRegistry(tmpDir)
	require.NotNil(t, registry)

	// List providers - should have defaults
	providers := registry.ListProviders()
	assert.True(t, len(providers) > 0)

	// Get compatible formats for a known provider
	formats, err := registry.GetCompatibleFormats("llamacpp")
	require.NoError(t, err)
	assert.Contains(t, formats, FormatGGUF)
}

func TestCrossProviderRegistryCheckCompatibility(t *testing.T) {
	tmpDir := t.TempDir()
	registry := NewCrossProviderRegistry(tmpDir)

	query := ModelCompatibilityQuery{
		ModelID:        "test-model",
		SourceFormat:   FormatGGUF,
		TargetProvider: "llamacpp",
	}

	result, err := registry.CheckCompatibility(query)
	require.NoError(t, err)
	assert.True(t, result.IsCompatible)
	assert.Equal(t, 1.0, result.Confidence)
}

func TestCrossProviderRegistryDownloadedModels(t *testing.T) {
	tmpDir := t.TempDir()
	registry := NewCrossProviderRegistry(tmpDir)

	// Initially no downloaded models
	models := registry.GetDownloadedModels()
	assert.Len(t, models, 0)

	// Register a downloaded model
	model := &DownloadedModel{
		ModelID:      "test-model",
		Provider:     "ollama",
		Format:       FormatGGUF,
		Path:         "/path/to/model",
		Size:         1024 * 1024 * 100, // 100MB
		DownloadTime: time.Now(),
		LastUsed:     time.Now(),
	}

	err := registry.RegisterDownloadedModel(model)
	require.NoError(t, err)

	// Should now have one model
	models = registry.GetDownloadedModels()
	assert.Len(t, models, 1)
	assert.Equal(t, "test-model", models[0].ModelID)
}

func TestCrossProviderRegistryFindOptimalProvider(t *testing.T) {
	tmpDir := t.TempDir()
	registry := NewCrossProviderRegistry(tmpDir)

	// Find optimal provider for GGUF format
	provider, err := registry.FindOptimalProvider("test-model", FormatGGUF, nil)
	require.NoError(t, err)
	assert.NotNil(t, provider)
	// Should be one of the providers that supports GGUF
	assert.True(t, provider.Name == "Ollama" || provider.Name == "Llama.cpp" || provider.Name == "VLLM")
}

func TestCrossProviderRegistryProviderNotFound(t *testing.T) {
	tmpDir := t.TempDir()
	registry := NewCrossProviderRegistry(tmpDir)

	// Try to get formats for non-existent provider
	_, err := registry.GetCompatibleFormats("non-existent")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

// MockRegTestProvider is a mock provider for testing (unique name to avoid conflicts)
type MockRegTestProvider struct {
	name         string
	providerType ProviderType
	models       []ModelInfo
	available    bool
	healthy      bool
}

func (m *MockRegTestProvider) Generate(ctx context.Context, request *LLMRequest) (*LLMResponse, error) {
	return &LLMResponse{
		Content: "Test response",
	}, nil
}

func (m *MockRegTestProvider) GenerateStream(ctx context.Context, request *LLMRequest, stream chan<- LLMResponse) error {
	stream <- LLMResponse{Content: "Test response"}
	close(stream)
	return nil
}

func (m *MockRegTestProvider) GetModels() []ModelInfo {
	return m.models
}

func (m *MockRegTestProvider) IsAvailable(ctx context.Context) bool {
	return m.available
}

func (m *MockRegTestProvider) GetName() string {
	return m.name
}

func (m *MockRegTestProvider) GetType() ProviderType {
	return m.providerType
}

func (m *MockRegTestProvider) GetCapabilities() []ModelCapability {
	return []ModelCapability{CapabilityTextGeneration, CapabilityCodeGeneration}
}

func (m *MockRegTestProvider) GetHealth(ctx context.Context) (*ProviderHealth, error) {
	status := "healthy"
	if !m.healthy {
		status = "unhealthy"
	}
	return &ProviderHealth{
		Status:    status,
		LastCheck: time.Now(),
	}, nil
}

func (m *MockRegTestProvider) Close() error {
	return nil
}

func (m *MockRegTestProvider) GetContextWindow() int {
	return 200_000
}

func (m *MockRegTestProvider) CountTokens(text string) (int, error) {
	return CharBasedTokenCount(text)
}
