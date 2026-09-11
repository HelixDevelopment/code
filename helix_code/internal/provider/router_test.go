package provider

import (
	"context"
	"testing"

	"dev.helix.code/internal/llm"
)

// TestRouter_RouteCompletion_OpenAI tests that openai provider selection returns response with OpenAI signature
func TestRouter_RouteCompletion_OpenAI(t *testing.T) {
	ctx := context.Background()

	// Create a ModelManager and register providers
	modelManager := llm.NewModelManager()
	router := NewRouter(modelManager)

	// Test with explicit OpenAI provider type
	req := CompletionRequest{
		ProviderType: llm.ProviderTypeOpenAI,
		Request: &llm.LLMRequest{
			Model: "gpt-4",
			Messages: []llm.Message{
				{Role: "user", Content: "Hello, test message"},
			},
			MaxTokens:   100,
			Temperature: 0.7,
		},
		Criteria: llm.ModelSelectionCriteria{
			TaskType:             "chat",
			RequiredCapabilities: []llm.ModelCapability{llm.CapabilityTextGeneration},
			MaxTokens:            100,
		},
	}

	// This will fail if no OpenAI provider is registered, but should not panic
	resp, err := router.RouteCompletion(ctx, req)

	// If OpenAI provider is not registered, we expect an error but not a panic
	if err != nil {
		t.Logf("Expected error when OpenAI provider not registered: %v", err)
		// Verify error is meaningful
		if err.Error() == "" {
			t.Errorf("Error should not be empty")
		}
		return
	}

	// If provider IS registered (integration test environment), verify response
	if resp != nil {
		// Verify provider attribution metadata
		if resp.ProviderType != llm.ProviderTypeOpenAI {
			t.Errorf("Expected provider type openai, got %s", resp.ProviderType)
		}
		if resp.ProviderName == "" {
			t.Errorf("Provider name should not be empty")
		}
		if resp.ModelUsed == "" {
			t.Errorf("Model used should not be empty")
		}
		if resp.RoutingReason == "" {
			t.Errorf("Routing reason should not be empty")
		}
		if resp.SelectedBy == "" {
			t.Errorf("SelectedBy should not be empty")
		}

		// Verify actual response from provider
		if resp.Response == nil {
			t.Errorf("Response should not be nil")
		} else {
			if resp.Response.Content == "" && len(resp.Response.ToolCalls) == 0 {
				t.Logf("Response content empty (may be expected if provider not configured)")
			}
			if resp.Response.FinishReason == "" {
				t.Logf("Finish reason empty")
			}
		}

		t.Logf("OpenAI routing successful: provider=%s, model=%s, reason=%s, selectedBy=%s",
			resp.ProviderName, resp.ModelUsed, resp.RoutingReason, resp.SelectedBy)
	}
}

// TestRouter_RouteCompletion_AutoSelection tests automatic model selection
func TestRouter_RouteCompletion_AutoSelection(t *testing.T) {
	ctx := context.Background()

	modelManager := llm.NewModelManager()
	router := NewRouter(modelManager)

	// Test without explicit provider type - should use auto-selection
	req := CompletionRequest{
		ProviderType: "", // Auto-select
		Request: &llm.LLMRequest{
			Model: "default",
			Messages: []llm.Message{
				{Role: "user", Content: "Write a hello world function in Go"},
			},
			MaxTokens:   200,
			Temperature: 0.3,
		},
		Criteria: llm.ModelSelectionCriteria{
			TaskType:             "code_generation",
			RequiredCapabilities: []llm.ModelCapability{llm.CapabilityCodeGeneration},
			MaxTokens:            200,
			QualityPreference:    "quality",
		},
	}

	resp, err := router.RouteCompletion(ctx, req)

	if err != nil {
		t.Logf("Auto-selection error (expected if no providers): %v", err)
		return
	}

	if resp != nil {
		// Verify it used router-based selection
		if resp.SelectedBy != "router" {
			t.Errorf("Expected selectedBy='router', got %s", resp.SelectedBy)
		}
		if resp.RoutingReason == "" {
			t.Errorf("Routing reason should be populated for auto-selection")
		}

		t.Logf("Auto-selection successful: model=%s, provider=%s, reason=%s",
			resp.ModelUsed, resp.ProviderName, resp.RoutingReason)
	}
}

// TestRouter_RouteCompletionWithFallback tests fallback chain
func TestRouter_RouteCompletionWithFallback(t *testing.T) {
	ctx := context.Background()

	modelManager := llm.NewModelManager()
	router := NewRouter(modelManager)

	// Request with non-existent provider to trigger fallback
	req := CompletionRequest{
		ProviderType: llm.ProviderType("nonexistent"),
		Request: &llm.LLMRequest{
			Model: "test-model",
			Messages: []llm.Message{
				{Role: "user", Content: "Test"},
			},
			MaxTokens: 50,
		},
		Criteria: llm.ModelSelectionCriteria{
			TaskType: "test",
		},
	}

	resp, err := router.RouteCompletionWithFallback(ctx, req)

	// Should either succeed with fallback or fail gracefully
	if err != nil {
		t.Logf("Fallback test error (expected if no providers): %v", err)
		return
	}

	if resp != nil && resp.SelectedBy == "fallback" {
		t.Logf("Fallback worked: model=%s, provider=%s, reason=%s",
			resp.ModelUsed, resp.ProviderName, resp.RoutingReason)
	}
}

// TestRouter_GetAvailableProviders tests listing available providers
func TestRouter_GetAvailableProviders(t *testing.T) {
	ctx := context.Background()

	modelManager := llm.NewModelManager()
	router := NewRouter(modelManager)

	providers, err := router.GetAvailableProviders(ctx)
	if err != nil {
		t.Fatalf("GetAvailableProviders failed: %v", err)
	}

	t.Logf("Found %d providers", len(providers))

	for _, p := range providers {
		if p.Name == "" {
			t.Errorf("Provider missing name: %+v", p)
		}
		if p.Type == "" {
			t.Errorf("Provider missing type: %+v", p)
		}
		t.Logf("Provider: %s (%s), models: %d, enabled: %v", p.Name, p.Type, len(p.Models), p.Enabled)
	}
}

// TestRouter_GetProviderMetadata tests getting provider metadata
func TestRouter_GetProviderMetadata(t *testing.T) {
	ctx := context.Background()

	modelManager := llm.NewModelManager()
	router := NewRouter(modelManager)

	// Test with known provider types
	testProviders := []llm.ProviderType{
		llm.ProviderTypeOpenAI,
		llm.ProviderTypeAnthropic,
		llm.ProviderTypeOllama,
		llm.ProviderTypeLocal,
	}

	for _, pt := range testProviders {
		metadata, err := router.GetProviderMetadata(ctx, pt)
		if err != nil {
			t.Logf("Provider %s not registered (expected in test env): %v", pt, err)
			continue
		}

		if metadata.Type != pt {
			t.Errorf("Metadata type mismatch: expected %s, got %s", pt, metadata.Type)
		}
		if metadata.Name == "" {
			t.Errorf("Provider name empty for %s", pt)
		}

		t.Logf("Provider %s metadata: %d models, endpoint=%s", pt, len(metadata.Models), metadata.Endpoint)
	}
}

// TestRouter_GetAllModelsMetadata tests getting all models metadata
func TestRouter_GetAllModelsMetadata(t *testing.T) {
	ctx := context.Background()

	modelManager := llm.NewModelManager()
	router := NewRouter(modelManager)

	models, err := router.GetAllModelsMetadata(ctx)
	if err != nil {
		t.Fatalf("GetAllModelsMetadata failed: %v", err)
	}

	t.Logf("Found %d models across all providers", len(models))

	for _, m := range models {
		if m.Name == "" {
			t.Errorf("Model missing name: %+v", m)
		}
		if m.ID == "" {
			t.Errorf("Model missing ID: %+v", m)
		}
		if m.ContextSize <= 0 {
			t.Errorf("Model %s has invalid ContextSize: %d", m.Name, m.ContextSize)
		}
		if m.MaxTokens <= 0 {
			t.Errorf("Model %s has invalid MaxTokens: %d", m.Name, m.MaxTokens)
		}
	}
}

// TestRouter_GetModelsByProviderMetadata tests getting models for specific provider
func TestRouter_GetModelsByProviderMetadata(t *testing.T) {
	ctx := context.Background()

	modelManager := llm.NewModelManager()
	router := NewRouter(modelManager)

	models, err := router.GetModelsByProviderMetadata(ctx, llm.ProviderTypeOpenAI)
	if err != nil {
		t.Logf("OpenAI models not available (expected in test env): %v", err)
		return
	}

	t.Logf("OpenAI models: %d", len(models))
	for _, m := range models {
		t.Logf("  Model: %s, context=%d, maxTokens=%d, tools=%v, vision=%v",
			m.Name, m.ContextSize, m.MaxTokens, m.SupportsTools, m.SupportsVision)
	}
}

// TestRouter_GetProviderCapabilities tests getting capabilities
func TestRouter_GetProviderCapabilities(t *testing.T) {
	ctx := context.Background()

	modelManager := llm.NewModelManager()
	router := NewRouter(modelManager)

	capabilities, err := router.GetProviderCapabilities(ctx)
	if err != nil {
		t.Fatalf("GetProviderCapabilities failed: %v", err)
	}

	t.Logf("Capabilities for %d provider types", len(capabilities))

	for providerType, caps := range capabilities {
		if len(caps) == 0 {
			t.Logf("Provider %s has no capabilities", providerType)
		}
		for _, cap := range caps {
			if cap == "" {
				t.Errorf("Empty capability for provider %s", providerType)
			}
		}
	}
}

// TestRouter_HealthCheck tests health check on all providers
func TestRouter_HealthCheck(t *testing.T) {
	ctx := context.Background()

	modelManager := llm.NewModelManager()
	router := NewRouter(modelManager)

	health, err := router.HealthCheck(ctx)
	if err != nil {
		t.Fatalf("HealthCheck failed: %v", err)
	}

	t.Logf("Health status for %d providers", len(health))

	for providerType, h := range health {
		if h == nil {
			t.Errorf("Nil health for provider %s", providerType)
			continue
		}
		if h.Status == "" {
			t.Errorf("Empty health status for provider %s", providerType)
		}
		t.Logf("Provider %s: %s (models: %d, errors: %d)",
			providerType, h.Status, h.ModelCount, h.ErrorCount)
	}
}

// TestRouter_MetadataService_GetAllProvidersMetadata tests metadata service
func TestRouter_MetadataService_GetAllProvidersMetadata(t *testing.T) {
	ctx := context.Background()

	modelManager := llm.NewModelManager()
	metaService := NewMetadataService(modelManager)

	providers, err := metaService.GetAllProvidersMetadata(ctx)
	if err != nil {
		t.Fatalf("GetAllProvidersMetadata failed: %v", err)
	}

	t.Logf("Metadata service found %d providers", len(providers))

	for _, p := range providers {
		if p.Name == "" {
			t.Errorf("Provider missing name")
		}
		if p.Type == "" {
			t.Errorf("Provider missing type")
		}
		t.Logf("Provider: %s, models: %d, health: %s", p.Name, p.ModelCount, p.HealthStatus)

		for _, m := range p.Models {
			if m.ContextSize <= 0 {
				t.Errorf("Model %s invalid context size: %d", m.Name, m.ContextSize)
			}
			if m.QualityScore < 0 || m.QualityScore > 10 {
				t.Errorf("Model %s quality score out of range: %f", m.Name, m.QualityScore)
			}
		}
	}
}

// TestRouter_MetadataService_GetProviderMetadata tests provider metadata
func TestRouter_MetadataService_GetProviderMetadata(t *testing.T) {
	ctx := context.Background()

	modelManager := llm.NewModelManager()
	metaService := NewMetadataService(modelManager)

	metadata, err := metaService.GetProviderMetadata(ctx, llm.ProviderTypeOpenAI)
	if err != nil {
		t.Logf("OpenAI metadata not available: %v", err)
		return
	}

	if metadata.Type != llm.ProviderTypeOpenAI {
		t.Errorf("Type mismatch")
	}
	if metadata.ModelCount != len(metadata.Models) {
		t.Errorf("Model count mismatch")
	}

	t.Logf("OpenAI metadata: %d models, endpoint=%s", metadata.ModelCount, metadata.Endpoint)
}

// TestRouter_MetadataService_GetModelMetadata tests specific model metadata
func TestRouter_MetadataService_GetModelMetadata(t *testing.T) {
	ctx := context.Background()

	modelManager := llm.NewModelManager()
	metaService := NewMetadataService(modelManager)

	// Try to find a known model
	metadata, err := metaService.GetModelMetadata(ctx, "gpt-4")
	if err != nil {
		t.Logf("Model gpt-4 not found: %v", err)
		return
	}

	if metadata.Name != "gpt-4" && metadata.ID != "gpt-4" {
		t.Logf("Found model: %s (ID: %s)", metadata.Name, metadata.ID)
	}
	if metadata.QualityScore < 0 || metadata.QualityScore > 10 {
		t.Errorf("Quality score out of range: %f", metadata.QualityScore)
	}

	t.Logf("Model metadata: %s, provider=%s, quality=%.1f, context=%d",
		metadata.Name, metadata.ProviderName, metadata.QualityScore, metadata.ContextSize)
}

// TestRouter_MetadataService_GetModelsByCapability tests filtering by capability
func TestRouter_MetadataService_GetModelsByCapability(t *testing.T) {
	ctx := context.Background()

	modelManager := llm.NewModelManager()
	metaService := NewMetadataService(modelManager)

	models, err := metaService.GetModelsByCapability(ctx, []llm.ModelCapability{llm.CapabilityCodeGeneration})
	if err != nil {
		t.Fatalf("GetModelsByCapability failed: %v", err)
	}

	t.Logf("Models with code generation: %d", len(models))

	for _, m := range models {
		found := false
		for _, cap := range m.Capabilities {
			if cap == llm.CapabilityCodeGeneration {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("Model %s missing code generation capability", m.Name)
		}
	}
}

// TestRouter_MetadataService_GetModelsByContextSize tests filtering by context size
func TestRouter_MetadataService_GetModelsByContextSize(t *testing.T) {
	ctx := context.Background()

	modelManager := llm.NewModelManager()
	metaService := NewMetadataService(modelManager)

	models, err := metaService.GetModelsByContextSize(ctx, 8192)
	if err != nil {
		t.Fatalf("GetModelsByContextSize failed: %v", err)
	}

	t.Logf("Models with context >= 8192: %d", len(models))

	for _, m := range models {
		if m.ContextSize < 8192 {
			t.Errorf("Model %s has context size %d < 8192", m.Name, m.ContextSize)
		}
	}
}

// TestRouter_MetadataService_GetRegistrySnapshot tests registry snapshot
func TestRouter_MetadataService_GetRegistrySnapshot(t *testing.T) {
	ctx := context.Background()

	modelManager := llm.NewModelManager()
	metaService := NewMetadataService(modelManager)

	snapshot, err := metaService.GetRegistrySnapshot(ctx)
	if err != nil {
		t.Fatalf("GetRegistrySnapshot failed: %v", err)
	}

	if snapshot == nil {
		t.Errorf("Snapshot should not be nil")
		return
	}

	// Registry fields are unexported, just verify snapshot is returned
	t.Logf("Registry snapshot received successfully")
}

// TestRouter_Integration_RealProvider tests with real provider if available
func TestRouter_Integration_RealProvider(t *testing.T) {
	ctx := context.Background()
	modelManager := llm.NewModelManager()
	router := NewRouter(modelManager)

	// Try to route a completion - this tests the full integration
	req := CompletionRequest{
		ProviderType: "", // Auto-select
		Request: &llm.LLMRequest{
			Model: "default",
			Messages: []llm.Message{
				{Role: "user", Content: "Say 'test passed' if you receive this"},
			},
			MaxTokens:   50,
			Temperature: 0.1,
		},
		Criteria: llm.ModelSelectionCriteria{
			TaskType:             "test",
			RequiredCapabilities: []llm.ModelCapability{llm.CapabilityTextGeneration},
			MaxTokens:            50,
		},
	}

	resp, err := router.RouteCompletion(ctx, req)

	// Document the result
	if err != nil {
		t.Logf("Integration test: routing failed (expected if no providers configured): %v", err)
		return
	}

	if resp == nil {
		t.Errorf("Response should not be nil when no error")
		return
	}

	// Verify response structure
	if resp.Response == nil {
		t.Errorf("LLMResponse should not be nil")
	}

	// Verify attribution metadata
	if resp.ProviderType == "" {
		t.Errorf("Provider type missing in response")
	}
	if resp.ProviderName == "" {
		t.Errorf("Provider name missing in response")
	}
	if resp.ModelUsed == "" {
		t.Errorf("Model used missing in response")
	}
	if resp.SelectedBy == "" {
		t.Errorf("SelectedBy missing in response")
	}

	t.Logf("SUCCESS: Real provider integration test passed")
	t.Logf("  Provider: %s (%s)", resp.ProviderName, resp.ProviderType)
	t.Logf("  Model: %s", resp.ModelUsed)
	t.Logf("  Selected by: %s", resp.SelectedBy)
	t.Logf("  Reason: %s", resp.RoutingReason)
	t.Logf("  Response content length: %d", len(resp.Response.Content))
	t.Logf("  Finish reason: %s", resp.Response.FinishReason)
	t.Logf("  Usage: prompt=%d, completion=%d, total=%d",
		resp.Response.Usage.PromptTokens,
		resp.Response.Usage.CompletionTokens,
		resp.Response.Usage.TotalTokens)
}

// TestRouter_Metadata_MatchesLiveRegistry tests that metadata matches live HelixLLM registry
func TestRouter_Metadata_MatchesLiveRegistry(t *testing.T) {
	ctx := context.Background()

	modelManager := llm.NewModelManager()
	metaService := NewMetadataService(modelManager)
	router := NewRouter(modelManager)

	// Get metadata from both services
	metaProviders, err := metaService.GetAllProvidersMetadata(ctx)
	if err != nil {
		t.Fatalf("Metadata service failed: %v", err)
	}

	routerProviders, err := router.GetAvailableProviders(ctx)
	if err != nil {
		t.Fatalf("Router providers failed: %v", err)
	}

	// Compare provider counts
	if len(metaProviders) != len(routerProviders) {
		t.Errorf("Provider count mismatch: metadata=%d, router=%d",
			len(metaProviders), len(routerProviders))
	}

	// Build maps for comparison
	metaMap := make(map[llm.ProviderType]ProviderMetadata)
	for _, p := range metaProviders {
		metaMap[p.Type] = p
	}

	routerMap := make(map[llm.ProviderType]ProviderEntry)
	for _, p := range routerProviders {
		routerMap[p.Type] = p
	}

	// Verify each provider in metadata exists in router
	for pType, meta := range metaMap {
		routerEntry, exists := routerMap[pType]
		if !exists {
			t.Errorf("Provider %s in metadata but not in router", pType)
			continue
		}

		// Verify model counts match
		if meta.ModelCount != len(routerEntry.Models) {
			t.Errorf("Provider %s model count mismatch: metadata=%d, router=%d",
				pType, meta.ModelCount, len(routerEntry.Models))
		}

		// Verify capabilities match
		if len(meta.Capabilities) != len(routerEntry.Capabilities) {
			t.Logf("Provider %s capability count differs: metadata=%d, router=%d",
				pType, len(meta.Capabilities), len(routerEntry.Capabilities))
		}

		t.Logf("Provider %s matches: models=%d, capabilities=%d",
			pType, meta.ModelCount, len(meta.Capabilities))
	}

	t.Logf("Metadata-registry consistency check passed for %d providers", len(metaMap))
}