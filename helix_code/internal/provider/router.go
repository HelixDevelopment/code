package provider

import (
	"context"
	"fmt"
	"sync"

	"dev.helix.code/internal/llm"
)

// Router routes Toolkit completion requests to HelixLLM ModelManager
type Router struct {
	modelManager *llm.ModelManager
	bridge       *ProviderBridge
	mu           sync.RWMutex
}

// CompletionRequest represents a completion request from Toolkit
type CompletionRequest struct {
	ProviderType llm.ProviderType
	Request      *llm.LLMRequest
	Criteria     llm.ModelSelectionCriteria
	SelectedBy   string // "router" | "explicit" | "fallback"
}

// CompletionResponse represents a completion response with provider attribution
type CompletionResponse struct {
	Response       *llm.LLMResponse
	ProviderType   llm.ProviderType
	ModelUsed      string
	ProviderName   string
	RoutingReason  string
	SelectedBy     string // "router" | "explicit" | "fallback"
}

// NewRouter creates a new router instance
func NewRouter(modelManager *llm.ModelManager) *Router {
	return &Router{
		modelManager: modelManager,
		bridge:       NewProviderBridge(modelManager),
	}
}

// RouteCompletion routes a completion request to the appropriate provider
func (r *Router) RouteCompletion(ctx context.Context, req CompletionRequest) (*CompletionResponse, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if r.modelManager == nil {
		return nil, fmt.Errorf("model manager not initialized")
	}

	var selectedModel *llm.ModelInfo
	var selectedProvider llm.Provider
	var routingReason string
	var selectedBy string

	// If provider type is explicitly specified, try to use it
	if req.ProviderType != "" {
		provider, err := r.modelManager.GetProviderForModel(req.Request.Model, req.ProviderType)
		if err != nil {
			// Explicit provider not available, fall back to selection
			routingReason = fmt.Sprintf("explicit provider %s unavailable: %v", req.ProviderType, err)
			selectedBy = "fallback"
		} else {
			selectedProvider = provider
			selectedBy = "explicit"
			routingReason = fmt.Sprintf("explicit provider %s selected", req.ProviderType)
		}
	}

	// If no provider selected yet, use ModelManager to select optimal model
	if selectedProvider == nil {
		model, err := r.modelManager.SelectOptimalModel(req.Criteria)
		if err != nil {
			return nil, fmt.Errorf("model selection failed: %w", err)
		}
		selectedModel = model

		provider, err := r.modelManager.GetProviderForModel(model.Name, model.Provider)
		if err != nil {
			return nil, fmt.Errorf("provider for selected model not found: %w", err)
		}
		selectedProvider = provider
		selectedBy = "router"
		routingReason = fmt.Sprintf("auto-selected by ModelManager (score-based): %s", model.Name)
	}

	// If we have a model but no provider yet, get the provider
	if selectedProvider == nil && selectedModel != nil {
		provider, err := r.modelManager.GetProviderForModel(selectedModel.Name, selectedModel.Provider)
		if err != nil {
			return nil, fmt.Errorf("provider for selected model not found: %w", err)
		}
		selectedProvider = provider
	}

	// Execute the completion request
	response, err := selectedProvider.Generate(ctx, req.Request)
	if err != nil {
		return nil, fmt.Errorf("provider generation failed: %w", err)
	}

	// Determine model used - prefer provider's reported model, fall back to request model
	modelUsed := response.Model
	if modelUsed == "" {
		modelUsed = req.Request.Model
	}
	if selectedModel != nil && modelUsed == "" {
		modelUsed = selectedModel.Name
	}

	return &CompletionResponse{
		Response:       response,
		ProviderType:   selectedProvider.GetType(),
		ModelUsed:      modelUsed,
		ProviderName:   selectedProvider.GetName(),
		RoutingReason:  routingReason,
		SelectedBy:     selectedBy,
	}, nil
}

// RouteCompletionWithFallback attempts routing with automatic fallback chain
func (r *Router) RouteCompletionWithFallback(ctx context.Context, req CompletionRequest) (*CompletionResponse, error) {
	// First attempt with primary routing
	resp, err := r.RouteCompletion(ctx, req)
	if err == nil {
		return resp, nil
	}

	// If explicit provider was requested and failed, try auto-selection
	if req.ProviderType != "" && req.SelectedBy != "fallback" {
		// Modify request to use auto-selection
		fallbackReq := req
		fallbackReq.ProviderType = ""
		fallbackReq.Criteria = req.Criteria

		fallbackResp, fallbackErr := r.RouteCompletion(ctx, fallbackReq)
		if fallbackErr == nil {
			fallbackResp.RoutingReason = fmt.Sprintf("fallback from %s: %v; %s", req.ProviderType, err, fallbackResp.RoutingReason)
			fallbackResp.SelectedBy = "fallback"
			return fallbackResp, nil
		}
	}

	return nil, err
}

// GetAvailableProviders returns all available providers with their metadata
func (r *Router) GetAvailableProviders(ctx context.Context) ([]ProviderEntry, error) {
	return r.bridge.ListProvidersDirect(ctx)
}

// GetProviderMetadata returns metadata for a specific provider
func (r *Router) GetProviderMetadata(ctx context.Context, providerType llm.ProviderType) (*ProviderEntry, error) {
	return r.bridge.GetProviderByType(ctx, string(providerType))
}

// GetAllModelsMetadata returns metadata for all models across all providers
func (r *Router) GetAllModelsMetadata(ctx context.Context) ([]ModelSummary, error) {
	return r.bridge.GetAllModels(ctx)
}

// GetModelsByProviderMetadata returns models for a specific provider
func (r *Router) GetModelsByProviderMetadata(ctx context.Context, providerType llm.ProviderType) ([]ModelSummary, error) {
	return r.bridge.GetModelsByProvider(ctx, providerType)
}

// GetProviderCapabilities returns capabilities for all providers
func (r *Router) GetProviderCapabilities(ctx context.Context) (map[llm.ProviderType][]llm.ModelCapability, error) {
	return r.bridge.GetProviderCapabilities(ctx)
}

// HealthCheck performs health checks on all providers
func (r *Router) HealthCheck(ctx context.Context) (map[llm.ProviderType]*llm.ProviderHealth, error) {
	healthMap, err := r.bridge.HealthCheck(ctx)
	if err != nil {
		return nil, err
	}
	if err != nil {
		return nil, err
	}
	
	// Convert map[string]bool to map[llm.ProviderType]*llm.ProviderHealth
	result := make(map[llm.ProviderType]*llm.ProviderHealth)
	for providerTypeStr, healthy := range healthMap {
		providerType := llm.ProviderType(providerTypeStr)
		health := &llm.ProviderHealth{}
		if healthy {
			health.Status = "healthy"
		} else {
			health.Status = "unhealthy"
		}
		result[providerType] = health
	}
	return result, nil
}

// SetModelManager updates the router's model manager
func (r *Router) SetModelManager(modelManager *llm.ModelManager) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.modelManager = modelManager
	r.bridge.SetModelManager(modelManager)
}

// GetModelManager returns the underlying model manager
func (r *Router) GetModelManager() *llm.ModelManager {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.modelManager
}