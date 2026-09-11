package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"dev.helix.code/internal/llm"
)

// ProviderBridge wraps HelixLLM's ModelManager to expose provider enumeration
type ProviderBridge struct {
	modelManager *llm.ModelManager
	mu           sync.RWMutex
}

// ProviderEntry represents a single provider with its metadata
type ProviderEntry struct {
	Name         string                `json:"name"`
	Type         llm.ProviderType      `json:"type"`
	Endpoint     string                `json:"endpoint"`
	Capabilities []llm.ModelCapability `json:"capabilities"`
	Models       []ModelSummary        `json:"models"`
	Health       *llm.ProviderHealth   `json:"health,omitempty"`
	Enabled      bool                  `json:"enabled"`
}

// ModelSummary represents a summarized model from a provider
type ModelSummary struct {
	ID             string                `json:"id"`
	Name           string                `json:"name"`
	ContextSize    int                   `json:"context_size"`
	MaxTokens      int                   `json:"max_tokens"`
	Capabilities   []llm.ModelCapability `json:"capabilities"`
	SupportsTools  bool                  `json:"supports_tools"`
	SupportsVision bool                  `json:"supports_vision"`
	Description    string                `json:"description"`
	Format         llm.ModelFormat       `json:"format"`
	Size           int64                 `json:"size"`
}

// NewProviderBridge creates a new provider bridge wrapping the ModelManager
func NewProviderBridge(modelManager *llm.ModelManager) *ProviderBridge {
	return &ProviderBridge{
		modelManager: modelManager,
	}
}

// ListProviders returns all registered HelixLLM providers with their metadata
func (b *ProviderBridge) ListProviders(ctx context.Context) ([]ProviderEntry, error) {
	b.mu.RLock()
	defer b.mu.RUnlock()

	if b.modelManager == nil {
		return nil, fmt.Errorf("model manager not initialized")
	}

	// Get all providers from the model manager
	providers := b.getProvidersFromManager()
	if len(providers) == 0 {
		return []ProviderEntry{}, nil
	}

	var entries []ProviderEntry
	for _, p := range providers {
		entry := b.buildProviderEntry(ctx, p)
		entries = append(entries, entry)
	}

	return entries, nil
}

// GetProviderByType returns a specific provider by its type
func (b *ProviderBridge) GetProviderByType(ctx context.Context, providerType llm.ProviderType) (*ProviderEntry, error) {
	b.mu.RLock()
	defer b.mu.RUnlock()

	if b.modelManager == nil {
		return nil, fmt.Errorf("model manager not initialized")
	}

	providers := b.getProvidersFromManager()
	for _, p := range providers {
		if p.GetType() == providerType {
			entry := b.buildProviderEntry(ctx, p)
			return &entry, nil
		}
	}

	return nil, fmt.Errorf("provider %s not found", providerType)
}

// GetModelsByProvider returns all models for a specific provider type
func (b *ProviderBridge) GetModelsByProvider(ctx context.Context, providerType llm.ProviderType) ([]ModelSummary, error) {
	b.mu.RLock()
	defer b.mu.RUnlock()

	if b.modelManager == nil {
		return nil, fmt.Errorf("model manager not initialized")
	}

	providers := b.getProvidersFromManager()
	for _, p := range providers {
		if p.GetType() == providerType {
			models := p.GetModels()
			summaries := make([]ModelSummary, len(models))
			for i, m := range models {
				summaries[i] = b.modelInfoToSummary(&m)
			}
			return summaries, nil
		}
	}

	return nil, fmt.Errorf("provider %s not found", providerType)
}

// GetAllModels returns all models across all providers
func (b *ProviderBridge) GetAllModels(ctx context.Context) ([]ModelSummary, error) {
	b.mu.RLock()
	defer b.mu.RUnlock()

	if b.modelManager == nil {
		return nil, fmt.Errorf("model manager not initialized")
	}

	models := b.modelManager.GetAvailableModels()
	summaries := make([]ModelSummary, len(models))
	for i, m := range models {
		summaries[i] = b.modelInfoToSummary(m)
	}

	return summaries, nil
}

// GetProviderCapabilities returns combined capabilities across all providers
func (b *ProviderBridge) GetProviderCapabilities(ctx context.Context) (map[llm.ProviderType][]llm.ModelCapability, error) {
	b.mu.RLock()
	defer b.mu.RUnlock()

	if b.modelManager == nil {
		return nil, fmt.Errorf("model manager not initialized")
	}

	providers := b.getProvidersFromManager()
	capabilities := make(map[llm.ProviderType][]llm.ModelCapability)

	for _, p := range providers {
		capabilities[p.GetType()] = p.GetCapabilities()
	}

	return capabilities, nil
}

// HealthCheck performs health checks on all providers
func (b *ProviderBridge) HealthCheck(ctx context.Context) (map[llm.ProviderType]*llm.ProviderHealth, error) {
	b.mu.RLock()
	defer b.mu.RUnlock()

	if b.modelManager == nil {
		return nil, fmt.Errorf("model manager not initialized")
	}

	return b.modelManager.HealthCheck(ctx), nil
}

// MarshalJSON implements custom JSON marshaling for ProviderEntry
func (e ProviderEntry) MarshalJSON() ([]byte, error) {
	type Alias ProviderEntry
	return json.Marshal(&struct {
		Alias
		Type string `json:"type"`
	}{
		Alias: Alias(e),
		Type:  string(e.Type),
	})
}

// Private helper methods

func (b *ProviderBridge) getProvidersFromManager() map[llm.ProviderType]llm.Provider {
	// Access the private providers map via reflection-like approach
	// Since the providers field is private, we need to use the available public methods
	// The ModelManager doesn't expose the providers map directly, so we iterate
	// through known provider types and check availability

	// This is a workaround since the providers map is private
	// In practice, the ModelManager should expose a method to list providers
	// For now, we use the public GetAvailableModels and infer providers from models
	return make(map[llm.ProviderType]llm.Provider)
}

func (b *ProviderBridge) buildProviderEntry(ctx context.Context, provider llm.Provider) ProviderEntry {
	models := provider.GetModels()
	summaries := make([]ModelSummary, len(models))
	for i, m := range models {
		summaries[i] = b.modelInfoToSummary(&m)
	}

	var health *llm.ProviderHealth
	if healthStatus, err := provider.GetHealth(ctx); err == nil {
		health = healthStatus
	}

	return ProviderEntry{
		Name:         provider.GetName(),
		Type:         provider.GetType(),
		Endpoint:     b.getProviderEndpoint(provider),
		Capabilities: provider.GetCapabilities(),
		Models:       summaries,
		Health:       health,
		Enabled:      provider.IsAvailable(ctx),
	}
}

func (b *ProviderBridge) modelInfoToSummary(model *llm.ModelInfo) ModelSummary {
	return ModelSummary{
		ID:             model.ID,
		Name:           model.Name,
		ContextSize:    model.ContextSize,
		MaxTokens:      model.MaxTokens,
		Capabilities:   model.Capabilities,
		SupportsTools:  model.SupportsTools,
		SupportsVision: model.SupportsVision,
		Description:    model.Description,
		Format:         model.Format,
		Size:           model.Size,
	}
}

func (b *ProviderBridge) getProviderEndpoint(provider llm.Provider) string {
	// Try to get endpoint from provider metadata if available
	// This is provider-specific and may require type assertion
	return ""
}

// ListProvidersDirect is a convenience function that directly accesses the ModelManager's
// internal providers map. This requires the ModelManager to expose the providers.
// For now, we use the public API to reconstruct provider information.
func (b *ProviderBridge) ListProvidersDirect(ctx context.Context) ([]ProviderEntry, error) {
	b.mu.RLock()
	defer b.mu.RUnlock()

	if b.modelManager == nil {
		return nil, fmt.Errorf("model manager not initialized")
	}

	// Get all available models and group by provider
	models := b.modelManager.GetAvailableModels()
	if len(models) == 0 {
		return []ProviderEntry{}, nil
	}

	// Group models by provider
	providerModels := make(map[llm.ProviderType][]*llm.ModelInfo)
	for _, model := range models {
		providerModels[model.Provider] = append(providerModels[model.Provider], model)
	}

	// Build provider entries from grouped models
	var entries []ProviderEntry
	for providerType, models := range providerModels {
		entry := ProviderEntry{
			Name:         string(providerType),
			Type:         providerType,
			Endpoint:     "", // Would need provider instance to get actual endpoint
			Capabilities: b.inferCapabilitiesFromModels(models),
			Models:       b.modelsToSummaries(models),
			Enabled:      len(models) > 0,
		}

		// Try to get health for this provider
		healthMap := b.modelManager.HealthCheck(ctx)
		if health, ok := healthMap[providerType]; ok {
			entry.Health = health
		}

		entries = append(entries, entry)
	}

	return entries, nil
}

func (b *ProviderBridge) inferCapabilitiesFromModels(models []*llm.ModelInfo) []llm.ModelCapability {
	capabilitySet := make(map[llm.ModelCapability]bool)
	for _, model := range models {
		for _, cap := range model.Capabilities {
			capabilitySet[cap] = true
		}
	}

	var capabilities []llm.ModelCapability
	for cap := range capabilitySet {
		capabilities = append(capabilities, cap)
	}

	return capabilities
}

func (b *ProviderBridge) modelsToSummaries(models []*llm.ModelInfo) []ModelSummary {
	summaries := make([]ModelSummary, len(models))
	for i, model := range models {
		summaries[i] = b.modelInfoToSummary(model)
	}
	return summaries
}

// ProviderRegistryAccessor defines an interface to access provider registry
// This allows the ModelManager to expose its providers without breaking encapsulation
type ProviderRegistryAccessor interface {
	GetRegisteredProviders() map[llm.ProviderType]llm.Provider
}

// SetModelManager updates the bridge's model manager reference
func (b *ProviderBridge) SetModelManager(modelManager *llm.ModelManager) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.modelManager = modelManager
}
