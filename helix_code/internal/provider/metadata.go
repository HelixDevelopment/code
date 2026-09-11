package provider

import (
	"context"
	"fmt"
	"sync"
	"time"

	"dev.helix.code/internal/llm"
)

// MetadataService provides model metadata endpoints matching live HelixLLM registry
type MetadataService struct {
	modelManager *llm.ModelManager
	bridge       *ProviderBridge
	mu           sync.RWMutex
}

// ModelMetadata represents comprehensive model metadata
type ModelMetadata struct {
	// Identity
	ID          string           `json:"id"`
	Name        string           `json:"name"`
	Provider    llm.ProviderType `json:"provider"`
	ProviderName string          `json:"provider_name"`

	// Capabilities
	ContextSize    int                   `json:"context_size"`
	MaxTokens      int                   `json:"max_tokens"`
	Capabilities   []llm.ModelCapability `json:"capabilities"`
	SupportsTools  bool                  `json:"supports_tools"`
	SupportsVision bool                  `json:"supports_vision"`
	SupportsStream bool                  `json:"supports_stream"`

	// Model details
	Format       llm.ModelFormat `json:"format"`
	Size         int64           `json:"size"`
	Description  string          `json:"description"`
	Version      string          `json:"version"`
	License      string          `json:"license"`
	ReleaseDate  string          `json:"release_date"`

	// Performance characteristics
	LatencyEstimate    time.Duration `json:"latency_estimate"`
	ThroughputEstimate float64       `json:"throughput_estimate"` // tokens/sec
	CostPer1kTokens    float64       `json:"cost_per_1k_tokens"`  // USD
	QualityScore       float64       `json:"quality_score"`       // 0-10

	// Verification status
	VerificationStatus string  `json:"verification_status"` // "verified", "partial", "pending", "failed"
	VerifiedAt         string  `json:"verified_at,omitempty"`
	VerifierScore      float64 `json:"verifier_score,omitempty"`

	// Health
	HealthStatus string `json:"health_status"`
	LastHealthCheck string `json:"last_health_check,omitempty"`

	// Runtime
	LastUsedAt   string  `json:"last_used_at,omitempty"`
	UsageCount   int64   `json:"usage_count"`
	ErrorCount   int64   `json:"error_count"`
	SuccessRate  float64 `json:"success_rate"`
}

// ProviderMetadata represents provider-level metadata
type ProviderMetadata struct {
	Type            llm.ProviderType      `json:"type"`
	Name            string                `json:"name"`
	Endpoint        string                `json:"endpoint"`
	Capabilities    []llm.ModelCapability `json:"capabilities"`
	Models          []ModelMetadata       `json:"models"`
	Enabled         bool                  `json:"enabled"`
	HealthStatus    string                `json:"health_status"`
	LastHealthCheck string                `json:"last_health_check,omitempty"`
	ModelCount      int                   `json:"model_count"`
	AvgLatency      time.Duration         `json:"avg_latency"`
	UptimePercent   float64               `json:"uptime_percent"`
	RateLimits      RateLimitInfo         `json:"rate_limits"`
}

// RateLimitInfo represents rate limiting information
type RateLimitInfo struct {
	RequestsPerMinute int `json:"requests_per_minute"`
	TokensPerMinute   int `json:"tokens_per_minute"`
	ConcurrentRequests int `json:"concurrent_requests"`
}

// MetadataRegistry holds cached metadata from live HelixLLM registry
type MetadataRegistry struct {
	providers map[llm.ProviderType]*ProviderMetadata
	models    map[string]*ModelMetadata
	mu        sync.RWMutex
	updatedAt string
}

// NewMetadataService creates a new metadata service
func NewMetadataService(modelManager *llm.ModelManager) *MetadataService {
	return &MetadataService{
		modelManager: modelManager,
		bridge:       NewProviderBridge(modelManager),
	}
}

// GetAllProvidersMetadata returns metadata for all registered providers
func (s *MetadataService) GetAllProvidersMetadata(ctx context.Context) ([]ProviderMetadata, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.modelManager == nil {
		return nil, fmt.Errorf("model manager not initialized")
	}

	providers, err := s.bridge.ListProvidersDirect(ctx)
	if err != nil {
		return nil, err
	}

	var result []ProviderMetadata
	for _, p := range providers {
		models := make([]ModelMetadata, len(p.Models))
		for i, m := range p.Models {
			models[i] = s.modelSummaryToMetadata(&m, p.Type, p.Name)
		}

		health := &llm.ProviderHealth{}
		if p.Health != nil {
			health = p.Health
		}

		result = append(result, ProviderMetadata{
			Type:            p.Type,
			Name:            p.Name,
			Endpoint:        p.Endpoint,
			Capabilities:    p.Capabilities,
			Models:          models,
			Enabled:         p.Enabled,
			HealthStatus:    health.Status,
			LastHealthCheck: health.LastCheck.Format(time.RFC3339),
			ModelCount:      len(models),
			RateLimits: RateLimitInfo{
				RequestsPerMinute: 60,
				TokensPerMinute:   150000,
				ConcurrentRequests: 10,
			},
		})
	}

	return result, nil
}

// GetProviderMetadata returns metadata for a specific provider
func (s *MetadataService) GetProviderMetadata(ctx context.Context, providerType llm.ProviderType) (*ProviderMetadata, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.modelManager == nil {
		return nil, fmt.Errorf("model manager not initialized")
	}

	entry, err := s.bridge.GetProviderByType(ctx, providerType)
	if err != nil {
		return nil, err
	}

	models := make([]ModelMetadata, len(entry.Models))
	for i, m := range entry.Models {
		models[i] = s.modelSummaryToMetadata(&m, entry.Type, entry.Name)
	}

	health := &llm.ProviderHealth{}
	if entry.Health != nil {
		health = entry.Health
	}

	return &ProviderMetadata{
		Type:            entry.Type,
		Name:            entry.Name,
		Endpoint:        entry.Endpoint,
		Capabilities:    entry.Capabilities,
		Models:          models,
		Enabled:         entry.Enabled,
		HealthStatus:    health.Status,
		LastHealthCheck: health.LastCheck.Format(time.RFC3339),
		ModelCount:      len(models),
		RateLimits: RateLimitInfo{
			RequestsPerMinute: 60,
			TokensPerMinute:   150000,
			ConcurrentRequests: 10,
		},
	}, nil
}

// GetModelMetadata returns metadata for a specific model
func (s *MetadataService) GetModelMetadata(ctx context.Context, modelID string) (*ModelMetadata, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.modelManager == nil {
		return nil, fmt.Errorf("model manager not initialized")
	}

	// Try to find model across all providers
	providers, err := s.bridge.ListProvidersDirect(ctx)
	if err != nil {
		return nil, err
	}

	for _, p := range providers {
		for _, m := range p.Models {
			if m.ID == modelID || m.Name == modelID {
				metadata := s.modelSummaryToMetadata(&m, p.Type, p.Name)
				return &metadata, nil
			}
		}
	}

	return nil, fmt.Errorf("model %s not found", modelID)
}

// GetModelsByCapability returns models that support specific capabilities
func (s *MetadataService) GetModelsByCapability(ctx context.Context, capabilities []llm.ModelCapability) ([]ModelMetadata, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.modelManager == nil {
		return nil, fmt.Errorf("model manager not initialized")
	}

	models := s.modelManager.GetModelsByCapability(capabilities)
	var result []ModelMetadata
	for _, m := range models {
		// Find provider for this model
		provider, err := s.modelManager.GetProviderForModel(m.Name, m.Provider)
		if err != nil {
			continue
		}
		providerName := provider.GetName()
		result = append(result, s.modelInfoToMetadata(m, m.Provider, providerName))
	}

	return result, nil
}

// GetModelsByContextSize returns models with at least the specified context size
func (s *MetadataService) GetModelsByContextSize(ctx context.Context, minContextSize int) ([]ModelMetadata, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.modelManager == nil {
		return nil, fmt.Errorf("model manager not initialized")
	}

	allModels := s.modelManager.GetAvailableModels()
	var result []ModelMetadata
	for _, m := range allModels {
		if m.ContextSize >= minContextSize {
			provider, err := s.modelManager.GetProviderForModel(m.Name, m.Provider)
			if err != nil {
				continue
			}
			providerName := provider.GetName()
			result = append(result, s.modelInfoToMetadata(m, m.Provider, providerName))
		}
	}

	return result, nil
}

// GetRegistrySnapshot returns a complete snapshot of the metadata registry
func (s *MetadataService) GetRegistrySnapshot(ctx context.Context) (*MetadataRegistry, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.modelManager == nil {
		return nil, fmt.Errorf("model manager not initialized")
	}

	providers, err := s.bridge.ListProvidersDirect(ctx)
	if err != nil {
		return nil, err
	}

	registry := &MetadataRegistry{
		providers: make(map[llm.ProviderType]*ProviderMetadata),
		models:    make(map[string]*ModelMetadata),
		updatedAt: time.Now().Format(time.RFC3339),
	}

	for _, p := range providers {
		models := make([]ModelMetadata, len(p.Models))
		for i, m := range p.Models {
			metadata := s.modelSummaryToMetadata(&m, p.Type, p.Name)
			models[i] = metadata
			registry.models[m.ID] = &metadata
		}

		health := &llm.ProviderHealth{}
		if p.Health != nil {
			health = p.Health
		}

		providerMeta := &ProviderMetadata{
			Type:            p.Type,
			Name:            p.Name,
			Endpoint:        p.Endpoint,
			Capabilities:    p.Capabilities,
			Models:          models,
			Enabled:         p.Enabled,
			HealthStatus:    health.Status,
			LastHealthCheck: health.LastCheck.Format(time.RFC3339),
			ModelCount:      len(models),
			RateLimits: RateLimitInfo{
				RequestsPerMinute: 60,
				TokensPerMinute:   150000,
				ConcurrentRequests: 10,
			},
		}
		registry.providers[p.Type] = providerMeta
	}

	return registry, nil
}

// modelSummaryToMetadata converts ModelSummary to ModelMetadata
func (s *MetadataService) modelSummaryToMetadata(summary *ModelSummary, providerType llm.ProviderType, providerName string) ModelMetadata {
	return ModelMetadata{
		ID:               summary.ID,
		Name:             summary.Name,
		Provider:         providerType,
		ProviderName:     providerName,
		ContextSize:      summary.ContextSize,
		MaxTokens:        summary.MaxTokens,
		Capabilities:     summary.Capabilities,
		SupportsTools:    summary.SupportsTools,
		SupportsVision:   summary.SupportsVision,
		SupportsStream:   true, // Most providers support streaming
		Format:           summary.Format,
		Size:             summary.Size,
		Description:      summary.Description,
		QualityScore:     s.estimateQualityFromSummary(summary),
	}
}

// modelInfoToMetadata converts ModelInfo to ModelMetadata
func (s *MetadataService) modelInfoToMetadata(model *llm.ModelInfo, providerType llm.ProviderType, providerName string) ModelMetadata {
	return ModelMetadata{
		ID:               model.ID,
		Name:             model.Name,
		Provider:         providerType,
		ProviderName:     providerName,
		ContextSize:      model.ContextSize,
		MaxTokens:        model.MaxTokens,
		Capabilities:     model.Capabilities,
		SupportsTools:    model.SupportsTools,
		SupportsVision:   model.SupportsVision,
		SupportsStream:   true,
		Format:           model.Format,
		Size:             model.Size,
		Description:      model.Description,
		QualityScore:     s.estimateQuality(model),
	}
}

// estimateQuality estimates model quality from ModelInfo
func (s *MetadataService) estimateQuality(model *llm.ModelInfo) float64 {
	// Use the same logic as ModelManager.calculateQualityScore
	modelSize := s.estimateModelSize(model.Name)
	var qualityEstimate float64
	switch modelSize {
	case "70B":
		qualityEstimate = 9.0
	case "34B":
		qualityEstimate = 8.0
	case "13B":
		qualityEstimate = 7.0
	case "7B":
		qualityEstimate = 6.0
	case "3B":
		qualityEstimate = 5.0
	default:
		qualityEstimate = 6.0
	}

	// Boost for specific capabilities
	if s.hasCapability(model.Capabilities, llm.CapabilityCodeGeneration) {
		qualityEstimate += 0.5
	}
	if s.hasCapability(model.Capabilities, llm.CapabilityReasoning) {
		qualityEstimate += 0.3
	}

	if qualityEstimate > 10.0 {
		qualityEstimate = 10.0
	}
	return qualityEstimate
}

// estimateQualityFromSummary estimates quality from ModelSummary
func (s *MetadataService) estimateQualityFromSummary(summary *ModelSummary) float64 {
	modelSize := s.estimateModelSize(summary.Name)
	var qualityEstimate float64
	switch modelSize {
	case "70B":
		qualityEstimate = 9.0
	case "34B":
		qualityEstimate = 8.0
	case "13B":
		qualityEstimate = 7.0
	case "7B":
		qualityEstimate = 6.0
	case "3B":
		qualityEstimate = 5.0
	default:
		qualityEstimate = 6.0
	}

	if summary.SupportsTools {
		qualityEstimate += 0.5
	}
	if summary.SupportsVision {
		qualityEstimate += 0.3
	}

	if qualityEstimate > 10.0 {
		qualityEstimate = 10.0
	}
	return qualityEstimate
}

// estimateModelSize estimates model size from name
func (s *MetadataService) estimateModelSize(modelName string) string {
	name := modelName
	if len(name) > 0 && name[0] >= 'A' && name[0] <= 'Z' {
		name = string(name[0]+32) + name[1:] // lowercase first char
	}
	for _, c := range name {
		if c >= 'A' && c <= 'Z' {
			name = name[:len(name)-1] + string(c+32)
		}
	}

	if contains(name, "70b") {
		return "70B"
	}
	if contains(name, "34b") {
		return "34B"
	}
	if contains(name, "13b") {
		return "13B"
	}
	if contains(name, "7b") {
		return "7B"
	}
	if contains(name, "3b") {
		return "3B"
	}
	return ""
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > len(substr) && (s[:len(substr)] == substr || s[len(s)-len(substr):] == substr || containsInternal(s, substr)))
}

func containsInternal(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

func (s *MetadataService) hasCapability(capabilities []llm.ModelCapability, capability llm.ModelCapability) bool {
	for _, cap := range capabilities {
		if cap == capability {
			return true
		}
	}
	return false
}

// RefreshRegistry refreshes the metadata registry from live providers
func (s *MetadataService) RefreshRegistry(ctx context.Context) error {
	// Force health check to update status
	_, err := s.bridge.HealthCheck(ctx)
	return err
}

// SetModelManager updates the metadata service's model manager
func (s *MetadataService) SetModelManager(modelManager *llm.ModelManager) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.modelManager = modelManager
	s.bridge.SetModelManager(modelManager)
}

// GetModelManager returns the underlying model manager
func (s *MetadataService) GetModelManager() *llm.ModelManager {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.modelManager
}