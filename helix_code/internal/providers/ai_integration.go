package providers

import (
	"context"
	"fmt"
	"sync"
	"time"

	"dev.helix.code/internal/llm"
	"dev.helix.code/internal/llm/compressioniface"
	"dev.helix.code/internal/logging"
	"dev.helix.code/internal/memory/providers"
	"github.com/google/uuid"
)

// AIIntegration provides unified interface for AI systems integration
type AIIntegration struct {
	mu              sync.RWMutex
	registry        *providers.ProviderRegistry
	manager         *providers.ProviderManager
	vector          *VectorIntegration
	memory          *MemoryIntegration
	logger          *logging.Logger
	config          *AIConfig
	providers       map[string]AIProvider
	conversationMgr *ConversationManager
	personalityMgr  *PersonalityManager
}

// AIConfig contains AI integration configuration
type AIConfig struct {
	DefaultLLM       string                       `json:"default_llm"`
	DefaultMemory    string                       `json:"default_memory"`
	Providers        map[string]*AIProviderConfig `json:"providers"`
	VectorConfig     *VectorConfig                `json:"vector_config"`
	MemoryConfig     *MemoryConfig                `json:"memory_config"`
	CacheEnabled     bool                         `json:"cache_enabled"`
	CacheSize        int                          `json:"cache_size"`
	CacheTTL         int64                        `json:"cache_ttl"`
	ProfilingEnabled bool                         `json:"profiling_enabled"`
}

// AIProviderConfig contains configuration for AI provider
type AIProviderConfig struct {
	Type             providers.ProviderType `json:"type"`
	Enabled          bool                   `json:"enabled"`
	Config           map[string]interface{} `json:"config"`
	Model            string                 `json:"model"`
	MaxTokens        int                    `json:"max_tokens"`
	Temperature      float64                `json:"temperature"`
	TopP             float64                `json:"top_p"`
	FrequencyPenalty float64                `json:"frequency_penalty"`
	PresencePenalty  float64                `json:"presence_penalty"`
}

// AIProvider defines interface for AI providers
type AIProvider interface {
	GenerateText(ctx context.Context, prompt string, options *GenerationOptions) (*GenerationResult, error)
	GenerateEmbedding(ctx context.Context, text string) ([]float64, error)
	GenerateChat(ctx context.Context, messages []*ChatMessage, options *ChatOptions) (*ChatResult, error)
	ClassifyText(ctx context.Context, text string, categories []string) (*ClassificationResult, error)
	ExtractEntities(ctx context.Context, text string) ([]*Entity, error)
	GetCapabilities() []string
	GetCostInfo() *CostInfo
}

// GenerationOptions contains options for text generation
type GenerationOptions struct {
	MaxTokens        int                `json:"max_tokens"`
	Temperature      float64            `json:"temperature"`
	TopP             float64            `json:"top_p"`
	FrequencyPenalty float64            `json:"frequency_penalty"`
	PresencePenalty  float64            `json:"presence_penalty"`
	Stop             []string           `json:"stop"`
	Stream           bool               `json:"stream"`
	Callback         func(string) error `json:"callback"`
}

// GenerationResult contains result of text generation
type GenerationResult struct {
	Text         string                 `json:"text"`
	Tokens       int                    `json:"tokens"`
	FinishReason string                 `json:"finish_reason"`
	Metadata     map[string]interface{} `json:"metadata"`
	Cost         float64                `json:"cost"`
	Duration     time.Duration          `json:"duration"`
}

// ChatMessage represents a chat message
type ChatMessage struct {
	Role     string                 `json:"role"`
	Content  string                 `json:"content"`
	Name     string                 `json:"name,omitempty"`
	Tokens   int                    `json:"tokens"`
	Metadata map[string]interface{} `json:"metadata,omitempty"`
}

// ChatOptions contains options for chat generation
type ChatOptions struct {
	Model            string   `json:"model"`
	MaxTokens        int      `json:"max_tokens"`
	Temperature      float64  `json:"temperature"`
	TopP             float64  `json:"top_p"`
	FrequencyPenalty float64  `json:"frequency_penalty"`
	PresencePenalty  float64  `json:"presence_penalty"`
	Stop             []string `json:"stop"`
	Stream           bool     `json:"stream"`
	SystemPrompt     string   `json:"system_prompt"`
	Tools            []string `json:"tools"`
}

// ChatResult contains result of chat generation
type ChatResult struct {
	Message      *ChatMessage           `json:"message"`
	Messages     []*ChatMessage         `json:"messages"`
	Tokens       int                    `json:"tokens"`
	FinishReason string                 `json:"finish_reason"`
	Metadata     map[string]interface{} `json:"metadata"`
	Cost         float64                `json:"cost"`
	Duration     time.Duration          `json:"duration"`
}

// ClassificationResult contains result of text classification
type ClassificationResult struct {
	Category      string           `json:"category"`
	Confidence    float64          `json:"confidence"`
	AllCategories []*CategoryScore `json:"all_categories"`
	Tokens        int              `json:"tokens"`
	Cost          float64          `json:"cost"`
	Duration      time.Duration    `json:"duration"`
}

// CategoryScore contains category with score
type CategoryScore struct {
	Category   string  `json:"category"`
	Confidence float64 `json:"confidence"`
}

// Entity represents an extracted entity
type Entity struct {
	Type       string                 `json:"type"`
	Text       string                 `json:"text"`
	Confidence float64                `json:"confidence"`
	Start      int                    `json:"start"`
	End        int                    `json:"end"`
	Metadata   map[string]interface{} `json:"metadata"`
}

// CostInfo contains cost information
type CostInfo struct {
	InputTokens  int     `json:"input_tokens"`
	OutputTokens int     `json:"output_tokens"`
	TotalTokens  int     `json:"total_tokens"`
	Cost         float64 `json:"cost"`
	Currency     string  `json:"currency"`
}

// NewAIIntegration creates a new AI integration
func NewAIIntegration(config *AIConfig) *AIIntegration {
	if config == nil {
		config = &AIConfig{
			DefaultLLM:    "openai",
			DefaultMemory: "memgpt",
			Providers: map[string]*AIProviderConfig{
				"openai": {
					Type:             providers.ProviderTypeOpenAI,
					Enabled:          true,
					Model:            "gpt-4",
					MaxTokens:        4096,
					Temperature:      0.7,
					TopP:             1.0,
					FrequencyPenalty: 0.0,
					PresencePenalty:  0.0,
					Config: map[string]interface{}{
						"api_key": "",
					},
				},
				"anthropic": {
					Type:             providers.ProviderTypeAnthropic,
					Enabled:          true,
					Model:            "claude-3-haiku-20240307",
					MaxTokens:        4096,
					Temperature:      0.7,
					TopP:             1.0,
					FrequencyPenalty: 0.0,
					PresencePenalty:  0.0,
					Config: map[string]interface{}{
						"api_key": "",
					},
				},
				"memgpt": {
					Type:        providers.ProviderTypeMemGPT,
					Enabled:     true,
					Model:       "memgpt-1.0",
					MaxTokens:   4096,
					Temperature: 0.7,
					Config: map[string]interface{}{
						"base_url": "https://api.memgpt.ai",
					},
				},
			},
			CacheEnabled:     true,
			CacheSize:        1000,
			CacheTTL:         300000, // 5 minutes
			ProfilingEnabled: false,
		}
	}

	ai := &AIIntegration{
		registry:  providers.GetRegistry(),
		logger:    logging.NewLogger(logging.INFO),
		config:    config,
		providers: make(map[string]AIProvider),
	}

	// Initialize vector integration
	ai.vector = NewVectorIntegration(config.VectorConfig)

	// Initialize memory integration
	ai.memory = NewMemoryIntegration(config.MemoryConfig)

	return ai
}

// Initialize initializes AI integration
func (ai *AIIntegration) Initialize(ctx context.Context) error {
	ai.mu.Lock()

	ai.logger.Info("Initializing AI integration: default_llm=%s, default_memory=%s, providers_count=%d", ai.config.DefaultLLM, ai.config.DefaultMemory, len(ai.config.Providers))

	// Initialize vector integration
	if err := ai.vector.Initialize(ctx); err != nil {
		ai.mu.Unlock()
		return fmt.Errorf("failed to initialize vector integration: %w", err)
	}

	// Initialize memory integration
	if err := ai.memory.Initialize(ctx); err != nil {
		ai.mu.Unlock()
		return fmt.Errorf("failed to initialize memory integration: %w", err)
	}

	// Initialize AI providers
	for name, providerConfig := range ai.config.Providers {
		if !providerConfig.Enabled {
			ai.logger.Info("Skipping disabled AI provider: name=%s", name)
			continue
		}

		provider, err := ai.createAIProvider(providerConfig)
		if err != nil {
			ai.logger.Error("Failed to create AI provider: name=%s, error=%v", name, err)
			continue
		}

		ai.providers[name] = provider
		ai.logger.Info("AI provider created: name=%s, type=%s, model=%s", name, providerConfig.Type, providerConfig.Model)
	}

	// The provider map is now fully populated. Release the write lock BEFORE
	// constructing the managers: NewConversationManager calls ai.GetProvider,
	// which takes ai.mu.RLock(). sync.RWMutex is NOT reentrant, so holding the
	// write lock across that call self-deadlocks the goroutine (the RLock blocks
	// forever on the Lock the same goroutine holds). The managers only need the
	// already-written map, so it is safe to build them outside the critical
	// section and then store the results under a re-acquired lock.
	ai.mu.Unlock()

	// Initialize conversation manager
	conversationMgr := NewConversationManager(ai, ai.config)

	// Initialize personality manager
	personalityMgr := NewPersonalityManager(ai, ai.config)

	ai.mu.Lock()
	ai.conversationMgr = conversationMgr
	ai.personalityMgr = personalityMgr
	ai.mu.Unlock()

	ai.logger.Info("AI integration initialized successfully")
	return nil
}

// isCloudGatedAIProviderType reports whether a dispatch arm of
// createAIProvider below delegates to a CONCRETE HOSTED constructor in
// internal/llm — i.e. whether constructing it would dial a vendor endpoint
// with an API key, and must therefore respect the W2c-1 cloud gate.
//
// The set is exactly the three arms whose provider bodies call an
// llm.New<X>Provider (NewOpenAIProvider -> llm.NewOpenAIProvider,
// NewAnthropicProvider -> llm.NewAnthropicProvider, NewGeminiProvider ->
// llm.NewGeminiProvider). It was established by reading every arm's body, not
// by classifying type names as "sounding hosted": the other ten arms —
// cohere, huggingface, mistral, gemma, llamaindex, memgpt, crewai,
// characterai, replika, anima — all return newNotImplementedProvider, so they
// construct nothing, dial nothing, and must keep working while the gate is
// closed. Gating them would break local routes for no safety gain, which is
// the failure the gate exists to avoid.
//
// KEEPING THIS HONEST: if an arm below is ever changed to delegate to a real
// hosted constructor, it must be added here in the same change. The
// TestCreateAIProvider_* guards assert both directions — hosted refused, and
// non-hosted still constructed — so a mis-set predicate fails a test rather
// than silently opening or closing a route.
func isCloudGatedAIProviderType(t providers.ProviderType) bool {
	switch t {
	case providers.ProviderTypeOpenAI,
		providers.ProviderTypeAnthropic,
		providers.ProviderTypeGemini:
		return true
	default:
		return false
	}
}

// createAIProvider creates an AI provider instance
func (ai *AIIntegration) createAIProvider(config *AIProviderConfig) (AIProvider, error) {
	// W2c-1 cloud gate (operator mandate 2026-09-05, local-only-by-default
	// serving). This dispatch reaches the concrete hosted constructors
	// DIRECTLY, so it bypasses both places the gate is otherwise enforced —
	// llm.NewCloudProvider and llm.NewProvider — and must apply it itself.
	// Checked BEFORE the switch so no hosted arm can construct by a back door,
	// and it refuses EVEN WITH credentials present: an API key in config is
	// exactly the situation the gate exists to override. Same error identity
	// as the llm-side gates so every caller can branch on one
	// errors.Is(err, llm.ErrCloudDisabled).
	if config != nil && !llm.CloudEnabled() && isCloudGatedAIProviderType(config.Type) {
		return nil, fmt.Errorf("%w: llm.cloud.enabled is false (default); hosted "+
			"AI provider %q will not be constructed. Set llm.cloud.enabled: true "+
			"to permit cloud providers, or use the local routes "+
			"(local/helixllm coder, llamacpp, ollama)",
			llm.ErrCloudDisabled, config.Type)
	}

	switch config.Type {
	case providers.ProviderTypeOpenAI:
		return NewOpenAIProvider(config), nil
	case providers.ProviderTypeAnthropic:
		return NewAnthropicProvider(config), nil
	case providers.ProviderTypeCohere:
		return NewCohereProvider(config), nil
	case providers.ProviderTypeHuggingFace:
		return NewHuggingFaceProvider(config), nil
	case providers.ProviderTypeMistral:
		return NewMistralProvider(config), nil
	case providers.ProviderTypeGemini:
		return NewGeminiProvider(config), nil
	case providers.ProviderTypeGemma:
		return NewGemmaProvider(config), nil
	case providers.ProviderTypeLlamaIndex:
		return NewLlamaIndexProvider(config), nil
	case providers.ProviderTypeMemGPT:
		return NewMemGPTAIProvider(config), nil
	case providers.ProviderTypeCrewAI:
		return NewCrewAIProvider(config), nil
	case providers.ProviderTypeCharacterAI:
		return NewCharacterAIProvider(config), nil
	case providers.ProviderTypeReplika:
		return NewReplikaAIProvider(config), nil
	case providers.ProviderTypeAnima:
		return NewAnimaAIProvider(config), nil
	default:
		return nil, fmt.Errorf("%s", tr(context.Background(), "internal_providers_unsupported_ai_provider_type", map[string]any{"Type": string(config.Type)}))
	}
}

// GenerateText generates text using default LLM
func (ai *AIIntegration) GenerateText(ctx context.Context, prompt string, options *GenerationOptions) (*GenerationResult, error) {
	return ai.GenerateTextWithProvider(ctx, ai.config.DefaultLLM, prompt, options)
}

// GenerateTextWithProvider generates text using specific provider
func (ai *AIIntegration) GenerateTextWithProvider(ctx context.Context, providerName string, prompt string, options *GenerationOptions) (*GenerationResult, error) {
	ai.mu.RLock()
	provider, exists := ai.providers[providerName]
	ai.mu.RUnlock()

	if !exists {
		return nil, fmt.Errorf("%s", tr(ctx, "internal_providers_ai_provider_not_found", map[string]any{"Name": providerName}))
	}

	start := time.Now()
	defer func() {
		ai.logger.Debug("Text generation completed: provider=%s, duration=%v", providerName, time.Since(start))
	}()

	result, err := provider.GenerateText(ctx, prompt, options)
	if err != nil {
		ai.logger.Error("Text generation failed: provider=%s, error=%v", providerName, err)
		return nil, err
	}

	// Store generation in memory
	if ai.config.DefaultMemory != "" {
		ai.memory.StoreGeneration(ctx, providerName, prompt, result)
	}

	return result, nil
}

// GenerateChat generates chat response using default LLM
func (ai *AIIntegration) GenerateChat(ctx context.Context, messages []*ChatMessage, options *ChatOptions) (*ChatResult, error) {
	return ai.GenerateChatWithProvider(ctx, ai.config.DefaultLLM, messages, options)
}

// GenerateChatWithProvider generates chat response using specific provider
func (ai *AIIntegration) GenerateChatWithProvider(ctx context.Context, providerName string, messages []*ChatMessage, options *ChatOptions) (*ChatResult, error) {
	ai.mu.RLock()
	provider, exists := ai.providers[providerName]
	ai.mu.RUnlock()

	if !exists {
		return nil, fmt.Errorf("%s", tr(ctx, "internal_providers_ai_provider_not_found", map[string]any{"Name": providerName}))
	}

	start := time.Now()
	defer func() {
		ai.logger.Debug("Chat generation completed: provider=%s, duration=%v", providerName, time.Since(start))
	}()

	result, err := provider.GenerateChat(ctx, messages, options)
	if err != nil {
		ai.logger.Error("Chat generation failed: provider=%s, error=%v", providerName, err)
		return nil, err
	}

	// Store conversation in memory
	if ai.config.DefaultMemory != "" {
		ai.memory.StoreConversation(ctx, providerName, messages, result)
	}

	return result, nil
}

// GenerateEmbedding generates embedding using default LLM
func (ai *AIIntegration) GenerateEmbedding(ctx context.Context, text string) ([]float64, error) {
	return ai.GenerateEmbeddingWithProvider(ctx, ai.config.DefaultLLM, text)
}

// GenerateEmbeddingWithProvider generates embedding using specific provider
func (ai *AIIntegration) GenerateEmbeddingWithProvider(ctx context.Context, providerName string, text string) ([]float64, error) {
	ai.mu.RLock()
	provider, exists := ai.providers[providerName]
	ai.mu.RUnlock()

	if !exists {
		return nil, fmt.Errorf("%s", tr(ctx, "internal_providers_ai_provider_not_found", map[string]any{"Name": providerName}))
	}

	start := time.Now()
	defer func() {
		ai.logger.Debug("Embedding generation completed: provider=%s, duration=%v", providerName, time.Since(start))
	}()

	embedding, err := provider.GenerateEmbedding(ctx, text)
	if err != nil {
		ai.logger.Error("Embedding generation failed: provider=%s, error=%v", providerName, err)
		return nil, err
	}

	// Store embedding in vector database
	if ai.vector != nil {
		vectorData := &VectorData{
			ID:        fmt.Sprintf("embed_%s_%d", providerName, time.Now().UnixNano()),
			Embedding: embedding,
			Metadata: map[string]interface{}{
				"provider":    providerName,
				"text":        text,
				"text_length": len(text),
				"created_at":  time.Now(),
			},
			IndexName: "text_embeddings",
			CreatedAt: time.Now(),
		}

		if err := ai.vector.StoreVector(ctx, vectorData); err != nil {
			ai.logger.Warn("Failed to store embedding in vector database: %v", err)
		}
	}

	return embedding, nil
}

// ClassifyText classifies text using default LLM
func (ai *AIIntegration) ClassifyText(ctx context.Context, text string, categories []string) (*ClassificationResult, error) {
	return ai.ClassifyTextWithProvider(ctx, ai.config.DefaultLLM, text, categories)
}

// ClassifyTextWithProvider classifies text using specific provider
func (ai *AIIntegration) ClassifyTextWithProvider(ctx context.Context, providerName string, text string, categories []string) (*ClassificationResult, error) {
	ai.mu.RLock()
	provider, exists := ai.providers[providerName]
	ai.mu.RUnlock()

	if !exists {
		return nil, fmt.Errorf("%s", tr(ctx, "internal_providers_ai_provider_not_found", map[string]any{"Name": providerName}))
	}

	start := time.Now()
	defer func() {
		ai.logger.Debug("Text classification completed: provider=%s, duration=%v", providerName, time.Since(start))
	}()

	result, err := provider.ClassifyText(ctx, text, categories)
	if err != nil {
		ai.logger.Error("Text classification failed: provider=%s, error=%v", providerName, err)
		return nil, err
	}

	return result, nil
}

// ExtractEntities extracts entities using default LLM
func (ai *AIIntegration) ExtractEntities(ctx context.Context, text string) ([]*Entity, error) {
	return ai.ExtractEntitiesWithProvider(ctx, ai.config.DefaultLLM, text)
}

// ExtractEntitiesWithProvider extracts entities using specific provider
func (ai *AIIntegration) ExtractEntitiesWithProvider(ctx context.Context, providerName string, text string) ([]*Entity, error) {
	ai.mu.RLock()
	provider, exists := ai.providers[providerName]
	ai.mu.RUnlock()

	if !exists {
		return nil, fmt.Errorf("%s", tr(ctx, "internal_providers_ai_provider_not_found", map[string]any{"Name": providerName}))
	}

	start := time.Now()
	defer func() {
		ai.logger.Debug("Entity extraction completed: provider=%s, duration=%v", providerName, time.Since(start))
	}()

	entities, err := provider.ExtractEntities(ctx, text)
	if err != nil {
		ai.logger.Error("Entity extraction failed: provider=%s, error=%v", providerName, err)
		return nil, err
	}

	return entities, nil
}

// GetConversation returns conversation manager
func (ai *AIIntegration) GetConversation() *ConversationManager {
	return ai.conversationMgr
}

// GetPersonality returns personality manager
func (ai *AIIntegration) GetPersonality() *PersonalityManager {
	return ai.personalityMgr
}

// GetVector returns vector integration
func (ai *AIIntegration) GetVector() *VectorIntegration {
	return ai.vector
}

// GetMemory returns memory integration
func (ai *AIIntegration) GetMemory() *MemoryIntegration {
	return ai.memory
}

// GetProvider returns specific AI provider
func (ai *AIIntegration) GetProvider(name string) (AIProvider, error) {
	ai.mu.RLock()
	defer ai.mu.RUnlock()

	provider, exists := ai.providers[name]
	if !exists {
		return nil, fmt.Errorf("%s", tr(context.Background(), "internal_providers_ai_provider_not_found", map[string]any{"Name": name}))
	}

	return provider, nil
}

// ListProviders returns list of available AI providers
func (ai *AIIntegration) ListProviders() []string {
	ai.mu.RLock()
	defer ai.mu.RUnlock()

	var providers []string
	for name := range ai.providers {
		providers = append(providers, name)
	}

	return providers
}

// providerEntry is a name/provider pair used for lock-free iteration of a
// point-in-time snapshot of the provider map.
type providerEntry struct {
	name     string
	provider AIProvider
}

// snapshotProviders returns a point-in-time copy of the registered providers,
// taken under ai.mu.RLock(). Callers iterate the returned slice lock-free; the
// underlying map is only ever read while the lock is held, so a concurrent
// guarded writer (Initialize) cannot race the iteration.
func (ai *AIIntegration) snapshotProviders() []providerEntry {
	ai.mu.RLock()
	defer ai.mu.RUnlock()

	snapshot := make([]providerEntry, 0, len(ai.providers))
	for name, provider := range ai.providers {
		snapshot = append(snapshot, providerEntry{name: name, provider: provider})
	}
	return snapshot
}

// GetStats returns statistics about AI integration
func (ai *AIIntegration) GetStats(ctx context.Context) (*AIStats, error) {
	stats := &AIStats{
		Providers: make(map[string]*AIProviderStats),
	}

	// Snapshot the provider map under the read lock. ai.providers is mutated
	// under ai.mu.Lock() by Initialize; iterating it here without the lock is a
	// data race (concurrent map read while a guarded writer adds entries). Copy
	// the entries into a local slice while holding the lock, then operate on the
	// snapshot lock-free.
	providersSnapshot := ai.snapshotProviders()

	// Get stats from each provider
	for _, entry := range providersSnapshot {
		name, provider := entry.name, entry.provider
		if aiProvider, ok := provider.(AIStatsProvider); ok {
			providerStats, err := aiProvider.GetStats()
			if err == nil {
				stats.Providers[name] = providerStats
			}
		}
	}

	// Get vector stats
	if ai.vector != nil {
		vectorStats, err := ai.vector.GetVectorStats(ctx)
		if err == nil {
			stats.VectorStats = vectorStats
		}
	}

	// Get memory stats
	if ai.memory != nil {
		memoryStats, err := ai.memory.GetMemoryStats(ctx)
		if err == nil {
			stats.MemoryStats = memoryStats
		}
	}

	return stats, nil
}

// HealthCheck performs health check on all AI providers
func (ai *AIIntegration) HealthCheck(ctx context.Context) (*AIHealthStatus, error) {
	status := &AIHealthStatus{
		ProviderStatuses: make(map[string]string),
	}

	healthyCount := 0
	totalCount := 0

	// Snapshot the provider map under the read lock (see GetStats) so the
	// iteration below does not race Initialize's guarded writes to ai.providers.
	providersSnapshot := ai.snapshotProviders()

	for _, entry := range providersSnapshot {
		name, provider := entry.name, entry.provider
		totalCount++
		// Simple health check - try to generate a small text
		result, err := provider.GenerateText(ctx, "test", &GenerationOptions{MaxTokens: 10})
		if err == nil && result.Text != "" {
			status.ProviderStatuses[name] = "healthy"
			healthyCount++
		} else {
			status.ProviderStatuses[name] = "unhealthy"
		}
	}

	if healthyCount == totalCount {
		status.Status = "healthy"
	} else if healthyCount > 0 {
		status.Status = "degraded"
	} else {
		status.Status = "unhealthy"
	}

	status.TotalProviders = totalCount
	status.HealthyProviders = healthyCount
	status.LastCheck = time.Now()

	return status, nil
}

// Stop stops AI integration
func (ai *AIIntegration) Stop(ctx context.Context) error {
	ai.mu.Lock()
	defer ai.mu.Unlock()

	ai.logger.Info("Stopping AI integration")

	// Stop vector integration
	if ai.vector != nil {
		if err := ai.vector.Stop(ctx); err != nil {
			ai.logger.Warn("Failed to stop vector integration: %v", err)
		}
	}

	// Stop memory integration
	if ai.memory != nil {
		if err := ai.memory.Stop(ctx); err != nil {
			ai.logger.Warn("Failed to stop memory integration: %v", err)
		}
	}

	// Stop conversation manager
	if ai.conversationMgr != nil {
		if err := ai.conversationMgr.Stop(ctx); err != nil {
			ai.logger.Warn("Failed to stop conversation manager: %v", err)
		}
	}

	// Stop personality manager
	if ai.personalityMgr != nil {
		if err := ai.personalityMgr.Stop(ctx); err != nil {
			ai.logger.Warn("Failed to stop personality manager: %v", err)
		}
	}

	ai.logger.Info("AI integration stopped")
	return nil
}

// AIStats contains statistics about AI integration
type AIStats struct {
	Providers   map[string]*AIProviderStats `json:"providers"`
	VectorStats *VectorStats                `json:"vector_stats"`
	MemoryStats *MemoryStats                `json:"memory_stats"`
	TotalCost   float64                     `json:"total_cost"`
	TotalTokens int                         `json:"total_tokens"`
	LastUpdate  time.Time                   `json:"last_update"`
}

// AIProviderStats contains statistics for AI provider
type AIProviderStats struct {
	Name           string        `json:"name"`
	Type           string        `json:"type"`
	Requests       int64         `json:"requests"`
	Successes      int64         `json:"successes"`
	Failures       int64         `json:"failures"`
	AverageLatency time.Duration `json:"average_latency"`
	TotalCost      float64       `json:"total_cost"`
	TotalTokens    int           `json:"total_tokens"`
	LastRequest    time.Time     `json:"last_request"`
}

// AIHealthStatus contains health status of AI integration
type AIHealthStatus struct {
	Status           string            `json:"status"`
	TotalProviders   int               `json:"total_providers"`
	HealthyProviders int               `json:"healthy_providers"`
	ProviderStatuses map[string]string `json:"provider_statuses"`
	LastCheck        time.Time         `json:"last_check"`
}

// AIStatsProvider interface for providers that can provide statistics
type AIStatsProvider interface {
	GetStats() (*AIProviderStats, error)
}

// Placeholder implementations for missing types and functions
type MemoryIntegration struct {
	mu                 sync.RWMutex
	logger             *logging.Logger
	config             *MemoryConfig
	provider           providers.VectorProvider
	generations        map[string]*GenerationResult
	conversations      map[string][]*ChatMessage
	totalGenerations   int64
	totalConversations int64
	totalTokens        int64
	totalCost          float64
	lastUpdate         time.Time
}

func NewMemoryIntegration(config *MemoryConfig) *MemoryIntegration {
	if config == nil {
		config = &MemoryConfig{
			Enabled:          true,
			MaxGenerations:   10000,
			MaxConversations: 1000,
			TTL:              24 * time.Hour,
		}
	}

	return &MemoryIntegration{
		logger:        logging.NewLogger(logging.INFO),
		config:        config,
		generations:   make(map[string]*GenerationResult),
		conversations: make(map[string][]*ChatMessage),
		lastUpdate:    time.Now(),
	}
}

func (mi *MemoryIntegration) Initialize(ctx context.Context) error {
	mi.mu.Lock()
	defer mi.mu.Unlock()

	mi.logger.Info("Initializing memory integration: max_generations=%d, max_conversations=%d",
		mi.config.MaxGenerations, mi.config.MaxConversations)

	// Initialize memory provider if configured
	if mi.config.Provider != "" {
		registry := providers.GetRegistry()
		providerType := providers.ProviderType(mi.config.Provider)
		provider, err := registry.CreateProvider(providerType, make(map[string]interface{}))
		if err != nil {
			mi.logger.Warn("Failed to create memory provider: %v", err)
		} else {
			mi.provider = provider
			mi.logger.Info("Memory provider initialized: %s", mi.config.Provider)
		}
	}

	mi.logger.Info("Memory integration initialized successfully")
	return nil
}

func (mi *MemoryIntegration) StoreGeneration(ctx context.Context, providerName, prompt string, generation *GenerationResult) error {
	mi.mu.Lock()
	defer mi.mu.Unlock()

	// Store generation with timestamp-based key
	key := fmt.Sprintf("%s_%d", providerName, time.Now().UnixNano())
	mi.generations[key] = generation

	// Update statistics
	mi.totalGenerations++
	mi.totalTokens += int64(generation.Tokens)
	mi.totalCost += generation.Cost
	mi.lastUpdate = time.Now()

	// Clean up old generations if limit exceeded
	if len(mi.generations) > mi.config.MaxGenerations {
		mi.cleanupGenerations()
	}

	mi.logger.Debug("Stored generation: provider=%s, tokens=%d, cost=%.4f",
		providerName, generation.Tokens, generation.Cost)

	return nil
}

func (mi *MemoryIntegration) StoreConversation(ctx context.Context, providerName string, messages []*ChatMessage, result *ChatResult) error {
	mi.mu.Lock()
	defer mi.mu.Unlock()

	// Store conversation with timestamp-based key
	key := fmt.Sprintf("%s_%d", providerName, time.Now().UnixNano())
	mi.conversations[key] = messages

	// Update statistics
	mi.totalConversations++
	mi.totalTokens += int64(result.Tokens)
	mi.totalCost += result.Cost
	mi.lastUpdate = time.Now()

	// Clean up old conversations if limit exceeded
	if len(mi.conversations) > mi.config.MaxConversations {
		mi.cleanupConversations()
	}

	mi.logger.Debug("Stored conversation: provider=%s, messages=%d, tokens=%d, cost=%.4f",
		providerName, len(messages), result.Tokens, result.Cost)

	return nil
}

func (mi *MemoryIntegration) cleanupGenerations() {
	// Remove oldest generations (simple FIFO cleanup)
	toRemove := len(mi.generations) - mi.config.MaxGenerations
	if toRemove <= 0 {
		return
	}

	// Remove first N entries
	count := 0
	for key := range mi.generations {
		delete(mi.generations, key)
		count++
		if count >= toRemove {
			break
		}
	}

	mi.logger.Debug("Cleaned up %d old generations", count)
}

func (mi *MemoryIntegration) cleanupConversations() {
	// Remove oldest conversations (simple FIFO cleanup)
	toRemove := len(mi.conversations) - mi.config.MaxConversations
	if toRemove <= 0 {
		return
	}

	// Remove first N entries
	count := 0
	for key := range mi.conversations {
		delete(mi.conversations, key)
		count++
		if count >= toRemove {
			break
		}
	}

	mi.logger.Debug("Cleaned up %d old conversations", count)
}

func (mi *MemoryIntegration) GetMemoryStats(ctx context.Context) (*MemoryStats, error) {
	mi.mu.RLock()
	defer mi.mu.RUnlock()

	return &MemoryStats{
		TotalGenerations:    mi.totalGenerations,
		TotalConversations:  mi.totalConversations,
		TotalTokens:         mi.totalTokens,
		TotalCost:           mi.totalCost,
		StoredGenerations:   len(mi.generations),
		StoredConversations: len(mi.conversations),
		LastUpdate:          mi.lastUpdate,
	}, nil
}

func (mi *MemoryIntegration) Stop(ctx context.Context) error {
	mi.mu.Lock()
	defer mi.mu.Unlock()

	mi.logger.Info("Stopping memory integration")

	// Clear in-memory storage
	mi.generations = make(map[string]*GenerationResult)
	mi.conversations = make(map[string][]*ChatMessage)

	mi.logger.Info("Memory integration stopped")
	return nil
}

type MemoryConfig struct {
	Enabled          bool          `json:"enabled"`
	Provider         string        `json:"provider"`
	MaxGenerations   int           `json:"max_generations"`
	MaxConversations int           `json:"max_conversations"`
	TTL              time.Duration `json:"ttl"`
}
type ConversationManager struct {
	mu                     sync.RWMutex
	logger                 *logging.Logger
	ai                     *AIIntegration
	config                 *AIConfig
	compressionCoordinator compressioniface.CompressionCoordinator
	conversations          map[string]*Conversation
	activeConversations    map[string]*Conversation
	totalMessages          int64
	totalTokens            int64
	lastUpdate             time.Time
}

type Conversation struct {
	ID               string
	Messages         []*ChatMessage
	Context          map[string]interface{}
	CreatedAt        time.Time
	UpdatedAt        time.Time
	TotalTokens      int
	TotalCost        float64
	Compressed       bool
	CompressionRatio float64
}

func NewConversationManager(ai *AIIntegration, config *AIConfig) *ConversationManager {
	// Initialize compression coordinator with actual LLM provider
	var llmProvider AIProvider
	if config.DefaultLLM != "" {
		if provider, err := ai.GetProvider(config.DefaultLLM); err == nil {
			llmProvider = provider
		}
	}

	compressionConfig := &compressioniface.Config{
		Enabled:              true,
		DefaultStrategy:      compressioniface.StrategyHybrid,
		TokenBudget:          200000,
		WarningThreshold:     150000,
		CompressionThreshold: 180000,
		AutoCompressEnabled:  true,
		AutoCompressInterval: 5 * time.Minute,
	}

	// Convert AIProvider to LLMProvider if possible
	var compressionCoordinator compressioniface.CompressionCoordinator
	if llmProvider != nil {
		// Create wrapper that implements LLMProvider interface
		llmWrapper := &LLMProviderWrapper{provider: llmProvider}
		coordinator, err := compressioniface.NewCoordinatorFactory(llmWrapper, compressionConfig)
		if err != nil {
			ai.logger.Warn("Failed to initialize compression coordinator: %v", err)
			compressionCoordinator = nil
		} else {
			compressionCoordinator = coordinator
		}
	}

	return &ConversationManager{
		logger:                 logging.NewLogger(logging.INFO),
		ai:                     ai,
		config:                 config,
		compressionCoordinator: compressionCoordinator,
		conversations:          make(map[string]*Conversation),
		activeConversations:    make(map[string]*Conversation),
		lastUpdate:             time.Now(),
	}
}

func (cm *ConversationManager) CreateConversation(ctx context.Context, id string) (*Conversation, error) {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	conv := &Conversation{
		ID:        id,
		Messages:  make([]*ChatMessage, 0),
		Context:   make(map[string]interface{}),
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	cm.conversations[id] = conv
	cm.activeConversations[id] = conv

	cm.logger.Info("Created conversation: id=%s", id)
	return conv, nil
}

func (cm *ConversationManager) AddMessage(ctx context.Context, conversationID string, message *ChatMessage) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	conv, exists := cm.conversations[conversationID]
	if !exists {
		return fmt.Errorf("%s", tr(ctx, "internal_providers_conversation_not_found", map[string]any{"ID": conversationID}))
	}

	conv.Messages = append(conv.Messages, message)
	conv.UpdatedAt = time.Now()
	conv.TotalTokens += message.Tokens

	cm.totalMessages++
	cm.totalTokens += int64(message.Tokens)
	cm.lastUpdate = time.Now()

	cm.logger.Debug("Added message to conversation: id=%s, role=%s, tokens=%d",
		conversationID, message.Role, message.Tokens)

	return nil
}

func (cm *ConversationManager) GetConversation(ctx context.Context, id string) (*Conversation, error) {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	conv, exists := cm.conversations[id]
	if !exists {
		return nil, fmt.Errorf("%s", tr(ctx, "internal_providers_conversation_not_found", map[string]any{"ID": id}))
	}

	return conv, nil
}

func (cm *ConversationManager) Stop(ctx context.Context) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	cm.logger.Info("Stopping conversation manager")

	// Clear conversations
	cm.conversations = make(map[string]*Conversation)
	cm.activeConversations = make(map[string]*Conversation)

	cm.logger.Info("Conversation manager stopped")
	return nil
}

// LLMProviderWrapper wraps AIProvider to implement compressioniface.LLMProvider
type LLMProviderWrapper struct {
	provider AIProvider
}

func (w *LLMProviderWrapper) Generate(ctx context.Context, prompt string) (string, error) {
	result, err := w.provider.GenerateText(ctx, prompt, &GenerationOptions{
		MaxTokens:   4096,
		Temperature: 0.7,
	})
	if err != nil {
		return "", err
	}
	return result.Text, nil
}

type PersonalityManager struct {
	mu                 sync.RWMutex
	logger             *logging.Logger
	ai                 *AIIntegration
	config             *AIConfig
	personalities      map[string]*Personality
	activePersonality  *Personality
	defaultPersonality string
	lastUpdate         time.Time
}

type Personality struct {
	ID           string                 `json:"id"`
	Name         string                 `json:"name"`
	Description  string                 `json:"description"`
	Traits       map[string]interface{} `json:"traits"`
	SystemPrompt string                 `json:"system_prompt"`
	Temperature  float64                `json:"temperature"`
	TopP         float64                `json:"top_p"`
	Enabled      bool                   `json:"enabled"`
	CreatedAt    time.Time              `json:"created_at"`
	UpdatedAt    time.Time              `json:"updated_at"`
	UsageCount   int64                  `json:"usage_count"`
}

func NewPersonalityManager(ai *AIIntegration, config *AIConfig) *PersonalityManager {
	pm := &PersonalityManager{
		logger:             logging.NewLogger(logging.INFO),
		ai:                 ai,
		config:             config,
		personalities:      make(map[string]*Personality),
		defaultPersonality: "default",
		lastUpdate:         time.Now(),
	}

	// Create default personality
	defaultPersonality := &Personality{
		ID:          "default",
		Name:        "Default Assistant",
		Description: "A helpful and professional AI assistant",
		Traits: map[string]interface{}{
			"helpfulness":     0.9,
			"professionalism": 0.8,
			"creativity":      0.7,
			"conciseness":     0.7,
		},
		SystemPrompt: "You are a helpful, professional AI assistant. Provide clear and accurate responses.",
		Temperature:  0.7,
		TopP:         1.0,
		Enabled:      true,
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
	}

	pm.personalities["default"] = defaultPersonality
	pm.activePersonality = defaultPersonality

	// Create additional default personalities
	pm.personalities["technical"] = &Personality{
		ID:          "technical",
		Name:        "Technical Expert",
		Description: "A highly technical AI assistant for development tasks",
		Traits: map[string]interface{}{
			"technical_depth": 0.95,
			"precision":       0.9,
			"verbosity":       0.6,
		},
		SystemPrompt: "You are a technical expert specializing in software development. Provide detailed, accurate technical guidance.",
		Temperature:  0.5,
		TopP:         0.95,
		Enabled:      true,
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
	}

	pm.personalities["creative"] = &Personality{
		ID:          "creative",
		Name:        "Creative Assistant",
		Description: "A creative and imaginative AI assistant",
		Traits: map[string]interface{}{
			"creativity":  0.95,
			"imagination": 0.9,
			"flexibility": 0.85,
		},
		SystemPrompt: "You are a creative AI assistant. Think outside the box and provide innovative solutions.",
		Temperature:  0.9,
		TopP:         0.95,
		Enabled:      true,
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
	}

	pm.logger.Info("Personality manager initialized with %d personalities", len(pm.personalities))

	return pm
}

func (pm *PersonalityManager) GetPersonality(id string) (*Personality, error) {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	personality, exists := pm.personalities[id]
	if !exists {
		return nil, fmt.Errorf("personality not found: %s", id)
	}

	return personality, nil
}

func (pm *PersonalityManager) SetActivePersonality(id string) error {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	personality, exists := pm.personalities[id]
	if !exists {
		return fmt.Errorf("personality not found: %s", id)
	}

	if !personality.Enabled {
		return fmt.Errorf("personality is disabled: %s", id)
	}

	pm.activePersonality = personality
	pm.lastUpdate = time.Now()

	pm.logger.Info("Set active personality: id=%s, name=%s", id, personality.Name)
	return nil
}

func (pm *PersonalityManager) GetActivePersonality() *Personality {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	return pm.activePersonality
}

func (pm *PersonalityManager) AddPersonality(personality *Personality) error {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	if personality.ID == "" {
		return fmt.Errorf("personality ID cannot be empty")
	}

	if _, exists := pm.personalities[personality.ID]; exists {
		return fmt.Errorf("personality already exists: %s", personality.ID)
	}

	personality.CreatedAt = time.Now()
	personality.UpdatedAt = time.Now()
	pm.personalities[personality.ID] = personality
	pm.lastUpdate = time.Now()

	pm.logger.Info("Added personality: id=%s, name=%s", personality.ID, personality.Name)
	return nil
}

func (pm *PersonalityManager) UpdatePersonality(id string, updates map[string]interface{}) error {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	personality, exists := pm.personalities[id]
	if !exists {
		return fmt.Errorf("personality not found: %s", id)
	}

	// Apply updates
	if name, ok := updates["name"].(string); ok {
		personality.Name = name
	}
	if description, ok := updates["description"].(string); ok {
		personality.Description = description
	}
	if systemPrompt, ok := updates["system_prompt"].(string); ok {
		personality.SystemPrompt = systemPrompt
	}
	if temperature, ok := updates["temperature"].(float64); ok {
		personality.Temperature = temperature
	}
	if topP, ok := updates["top_p"].(float64); ok {
		personality.TopP = topP
	}
	if enabled, ok := updates["enabled"].(bool); ok {
		personality.Enabled = enabled
	}
	if traits, ok := updates["traits"].(map[string]interface{}); ok {
		personality.Traits = traits
	}

	personality.UpdatedAt = time.Now()
	pm.lastUpdate = time.Now()

	pm.logger.Info("Updated personality: id=%s", id)
	return nil
}

func (pm *PersonalityManager) RemovePersonality(id string) error {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	if id == pm.defaultPersonality {
		return fmt.Errorf("cannot remove default personality")
	}

	if _, exists := pm.personalities[id]; !exists {
		return fmt.Errorf("personality not found: %s", id)
	}

	// If this is the active personality, switch to default
	if pm.activePersonality != nil && pm.activePersonality.ID == id {
		pm.activePersonality = pm.personalities[pm.defaultPersonality]
	}

	delete(pm.personalities, id)
	pm.lastUpdate = time.Now()

	pm.logger.Info("Removed personality: id=%s", id)
	return nil
}

func (pm *PersonalityManager) ListPersonalities() []*Personality {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	personalities := make([]*Personality, 0, len(pm.personalities))
	for _, personality := range pm.personalities {
		personalities = append(personalities, personality)
	}

	return personalities
}

func (pm *PersonalityManager) IncrementUsage(id string) {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	if personality, exists := pm.personalities[id]; exists {
		personality.UsageCount++
		personality.UpdatedAt = time.Now()
	}
}

func (pm *PersonalityManager) Stop(ctx context.Context) error {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	pm.logger.Info("Stopping personality manager")

	// Clear non-default personalities
	for id := range pm.personalities {
		if id != pm.defaultPersonality {
			delete(pm.personalities, id)
		}
	}

	// Reset to default personality
	pm.activePersonality = pm.personalities[pm.defaultPersonality]

	pm.logger.Info("Personality manager stopped")
	return nil
}

type MemoryStats struct {
	TotalGenerations    int64     `json:"total_generations"`
	TotalConversations  int64     `json:"total_conversations"`
	TotalTokens         int64     `json:"total_tokens"`
	TotalCost           float64   `json:"total_cost"`
	StoredGenerations   int       `json:"stored_generations"`
	StoredConversations int       `json:"stored_conversations"`
	LastUpdate          time.Time `json:"last_update"`
}

// NotImplementedProvider returns errors for providers not yet integrated with AIIntegration.
// Use the internal/llm package providers directly for full functionality.
type NotImplementedProvider struct {
	providerName string
}

// newNotImplementedProvider creates a provider that returns clear errors
func newNotImplementedProvider(name string) *NotImplementedProvider {
	return &NotImplementedProvider{providerName: name}
}

func (p *NotImplementedProvider) GenerateText(ctx context.Context, prompt string, options *GenerationOptions) (*GenerationResult, error) {
	return nil, fmt.Errorf("provider %s is not yet integrated with AIIntegration - use internal/llm package directly", p.providerName)
}

func (p *NotImplementedProvider) GenerateEmbedding(ctx context.Context, text string) ([]float64, error) {
	return nil, fmt.Errorf("provider %s is not yet integrated with AIIntegration - use internal/llm package directly", p.providerName)
}

func (p *NotImplementedProvider) GenerateChat(ctx context.Context, messages []*ChatMessage, options *ChatOptions) (*ChatResult, error) {
	return nil, fmt.Errorf("provider %s is not yet integrated with AIIntegration - use internal/llm package directly", p.providerName)
}

func (p *NotImplementedProvider) ClassifyText(ctx context.Context, text string, categories []string) (*ClassificationResult, error) {
	return nil, fmt.Errorf("provider %s is not yet integrated with AIIntegration - use internal/llm package directly", p.providerName)
}

func (p *NotImplementedProvider) ExtractEntities(ctx context.Context, text string) ([]*Entity, error) {
	return nil, fmt.Errorf("provider %s is not yet integrated with AIIntegration - use internal/llm package directly", p.providerName)
}

func (p *NotImplementedProvider) GetCapabilities() []string {
	return []string{} // No capabilities - provider not implemented
}

func (p *NotImplementedProvider) GetCostInfo() *CostInfo {
	return &CostInfo{
		InputTokens:  0,
		OutputTokens: 0,
		TotalTokens:  0,
		Cost:         0,
		Currency:     "USD",
	}
}

// LLMProviderAdapter wraps an internal/llm.Provider to implement the AIProvider interface.
// This adapter enables using the low-level LLM providers through the higher-level AIIntegration API.
type LLMProviderAdapter struct {
	provider     llm.Provider
	providerName string

	// costMu guards lastCostInfo. GenerateText / GenerateChat update the cost
	// info after each call while GetCostInfo (and concurrent generations) read
	// it; without this lock those accesses are a data race.
	costMu       sync.Mutex
	lastCostInfo *CostInfo
}

// NewLLMProviderAdapter creates a new adapter wrapping an llm.Provider
func NewLLMProviderAdapter(provider llm.Provider, name string) *LLMProviderAdapter {
	return &LLMProviderAdapter{
		provider:     provider,
		providerName: name,
		lastCostInfo: &CostInfo{Currency: "USD"},
	}
}

func (a *LLMProviderAdapter) GenerateText(ctx context.Context, prompt string, options *GenerationOptions) (*GenerationResult, error) {
	if a.provider == nil {
		return nil, fmt.Errorf("provider not initialized")
	}

	// Build LLM request
	request := &llm.LLMRequest{
		ID:       uuid.New(),
		Messages: []llm.Message{{Role: "user", Content: prompt}},
	}

	if options != nil {
		request.MaxTokens = options.MaxTokens
		request.Temperature = options.Temperature
		request.TopP = options.TopP
		request.Stop = options.Stop
		request.Stream = options.Stream
	}

	// Generate response
	startTime := time.Now()
	resp, err := a.provider.Generate(ctx, request)
	if err != nil {
		return nil, fmt.Errorf("generation failed: %w", err)
	}

	// Update cost tracking
	a.updateCostInfo(resp.Usage.PromptTokens, resp.Usage.CompletionTokens, resp.Usage.TotalTokens)

	return &GenerationResult{
		Text:         resp.Content,
		Tokens:       resp.Usage.TotalTokens,
		FinishReason: resp.FinishReason,
		Duration:     time.Since(startTime),
		Metadata:     resp.ProviderMetadata,
	}, nil
}

func (a *LLMProviderAdapter) GenerateEmbedding(ctx context.Context, text string) ([]float64, error) {
	// Embeddings require a specialized endpoint that most providers don't expose through the standard Generate method
	// Return error indicating this needs a dedicated embedding model
	return nil, fmt.Errorf("embedding generation requires dedicated embedding model; use a specialized embedding provider")
}

func (a *LLMProviderAdapter) GenerateChat(ctx context.Context, messages []*ChatMessage, options *ChatOptions) (*ChatResult, error) {
	if a.provider == nil {
		return nil, fmt.Errorf("provider not initialized")
	}

	// Convert ChatMessages to LLM Messages
	llmMessages := make([]llm.Message, 0, len(messages)+1)

	// Add system prompt if provided
	if options != nil && options.SystemPrompt != "" {
		llmMessages = append(llmMessages, llm.Message{
			Role:    "system",
			Content: options.SystemPrompt,
		})
	}

	// Convert user messages
	for _, msg := range messages {
		llmMessages = append(llmMessages, llm.Message{
			Role:    msg.Role,
			Content: msg.Content,
			Name:    msg.Name,
		})
	}

	// Build request
	request := &llm.LLMRequest{
		ID:       uuid.New(),
		Messages: llmMessages,
	}

	if options != nil {
		if options.Model != "" {
			request.Model = options.Model
		}
		request.MaxTokens = options.MaxTokens
		request.Temperature = options.Temperature
		request.TopP = options.TopP
		request.Stop = options.Stop
		request.Stream = options.Stream
	}

	// Generate response
	startTime := time.Now()
	resp, err := a.provider.Generate(ctx, request)
	if err != nil {
		return nil, fmt.Errorf("chat generation failed: %w", err)
	}

	// Update cost tracking
	a.updateCostInfo(resp.Usage.PromptTokens, resp.Usage.CompletionTokens, resp.Usage.TotalTokens)

	// Build response message
	responseMessage := &ChatMessage{
		Role:    "assistant",
		Content: resp.Content,
		Tokens:  resp.Usage.CompletionTokens,
	}

	return &ChatResult{
		Message:      responseMessage,
		Tokens:       resp.Usage.TotalTokens,
		FinishReason: resp.FinishReason,
		Duration:     time.Since(startTime),
		Metadata:     resp.ProviderMetadata,
	}, nil
}

func (a *LLMProviderAdapter) ClassifyText(ctx context.Context, text string, categories []string) (*ClassificationResult, error) {
	if a.provider == nil {
		return nil, fmt.Errorf("provider not initialized")
	}

	// Build a classification prompt
	prompt := fmt.Sprintf(
		"Classify the following text into exactly one of these categories: %v\n\nText: %s\n\nRespond with only the category name.",
		categories, text,
	)

	result, err := a.GenerateText(ctx, prompt, &GenerationOptions{
		MaxTokens:   50,
		Temperature: 0.0, // Deterministic for classification
	})
	if err != nil {
		return nil, fmt.Errorf("classification failed: %w", err)
	}

	// Parse the response to find the matching category
	responseText := result.Text
	selectedCategory := ""
	maxConfidence := 0.0

	// Check which category the response matches
	for _, cat := range categories {
		if responseText == cat || containsIgnoreCase(responseText, cat) {
			selectedCategory = cat
			maxConfidence = 0.9 // High confidence if exact match
			break
		}
	}

	if selectedCategory == "" && len(categories) > 0 {
		selectedCategory = categories[0]
		maxConfidence = 0.1 // Low confidence fallback
	}

	// Build category scores
	allCategories := make([]*CategoryScore, len(categories))
	for i, cat := range categories {
		score := 0.1
		if cat == selectedCategory {
			score = maxConfidence
		}
		allCategories[i] = &CategoryScore{
			Category:   cat,
			Confidence: score,
		}
	}

	return &ClassificationResult{
		Category:      selectedCategory,
		Confidence:    maxConfidence,
		AllCategories: allCategories,
		Tokens:        result.Tokens,
		Duration:      result.Duration,
	}, nil
}

func (a *LLMProviderAdapter) ExtractEntities(ctx context.Context, text string) ([]*Entity, error) {
	if a.provider == nil {
		return nil, fmt.Errorf("provider not initialized")
	}

	// Build entity extraction prompt
	prompt := fmt.Sprintf(
		`Extract named entities from the following text. For each entity, identify:
- Type (PERSON, ORGANIZATION, LOCATION, DATE, NUMBER, etc.)
- The exact text of the entity

Format each entity as: TYPE: "text"

Text: %s

Entities:`, text,
	)

	result, err := a.GenerateText(ctx, prompt, &GenerationOptions{
		MaxTokens:   500,
		Temperature: 0.0,
	})
	if err != nil {
		return nil, fmt.Errorf("entity extraction failed: %w", err)
	}

	// Parse entities from response (simplified parsing)
	entities := parseEntities(result.Text, text)

	return entities, nil
}

func (a *LLMProviderAdapter) GetCapabilities() []string {
	if a.provider == nil {
		return []string{}
	}

	llmCaps := a.provider.GetCapabilities()
	caps := make([]string, len(llmCaps))
	for i, c := range llmCaps {
		caps[i] = string(c)
	}
	return caps
}

func (a *LLMProviderAdapter) GetCostInfo() *CostInfo {
	a.costMu.Lock()
	defer a.costMu.Unlock()

	// Return a copy, not the live stored pointer: handing the caller the same
	// *CostInfo the next Generate call mutates is a snapshot-getter race even
	// with the lock (the lock would not protect the caller's later reads).
	costCopy := *a.lastCostInfo
	return &costCopy
}

// updateCostInfo records the token usage of the most recent generation under
// the cost lock.
func (a *LLMProviderAdapter) updateCostInfo(promptTokens, completionTokens, totalTokens int) {
	a.costMu.Lock()
	defer a.costMu.Unlock()

	a.lastCostInfo.InputTokens = promptTokens
	a.lastCostInfo.OutputTokens = completionTokens
	a.lastCostInfo.TotalTokens = totalTokens
}

// Helper functions for LLMProviderAdapter

func containsIgnoreCase(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || contains(toLower(s), toLower(substr)))
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || findSubstring(s, substr))
}

func findSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

func toLower(s string) string {
	result := make([]byte, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 'A' && c <= 'Z' {
			result[i] = c + 32
		} else {
			result[i] = c
		}
	}
	return string(result)
}

func parseEntities(response, originalText string) []*Entity {
	// Simplified entity parsing - in production, use more robust NER
	entities := []*Entity{}

	// Look for patterns like "TYPE: \"text\""
	lines := splitLines(response)
	for _, line := range lines {
		line = trimSpace(line)
		if line == "" {
			continue
		}

		// Try to parse TYPE: "text" format
		colonIdx := findChar(line, ':')
		if colonIdx > 0 && colonIdx < len(line)-1 {
			entityType := trimSpace(line[:colonIdx])
			entityText := trimSpace(line[colonIdx+1:])

			// Remove quotes if present
			if len(entityText) >= 2 && entityText[0] == '"' && entityText[len(entityText)-1] == '"' {
				entityText = entityText[1 : len(entityText)-1]
			}

			if entityType != "" && entityText != "" {
				// Find position in original text
				start := findSubstringIndex(originalText, entityText)
				end := start + len(entityText)
				if start < 0 {
					start = 0
					end = 0
				}

				entities = append(entities, &Entity{
					Type:       entityType,
					Text:       entityText,
					Confidence: 0.8,
					Start:      start,
					End:        end,
				})
			}
		}
	}

	return entities
}

func splitLines(s string) []string {
	var lines []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			lines = append(lines, s[start:i])
			start = i + 1
		}
	}
	if start < len(s) {
		lines = append(lines, s[start:])
	}
	return lines
}

func trimSpace(s string) string {
	start := 0
	end := len(s)
	for start < end && (s[start] == ' ' || s[start] == '\t' || s[start] == '\r' || s[start] == '\n') {
		start++
	}
	for end > start && (s[end-1] == ' ' || s[end-1] == '\t' || s[end-1] == '\r' || s[end-1] == '\n') {
		end--
	}
	return s[start:end]
}

func findChar(s string, c byte) int {
	for i := 0; i < len(s); i++ {
		if s[i] == c {
			return i
		}
	}
	return -1
}

func findSubstringIndex(s, substr string) int {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}

// Provider factory functions - create adapters wrapping internal/llm providers.
//
// THE THREE HOSTED ONES GO THROUGH llm.NewProvider, NOT llm.New<X>Provider.
// NewOpenAIProvider / NewAnthropicProvider / NewGeminiProvider below each call
// llm.NewProvider(llmConfig) — the canonical, GATED factory
// (internal/llm/factory.go) — rather than the concrete constructor for their
// type. llm.NewProvider dispatches ProviderTypeOpenAI/Anthropic/Gemini to
// exactly those same concrete constructors with exactly the same
// ProviderConfigEntry, so behaviour is unchanged in every respect except one:
// it applies the W2c-1 cloud gate first, and returns an ErrCloudDisabled-
// wrapping error while llm.cloud.enabled is false. It also routes through
// providerOrNil, so a failed construction can never hand back a non-nil
// llm.Provider wrapping a nil pointer.
//
// WHY, given createAIProvider already gates. These three are EXPORTED. The
// gate in createAIProvider protects the dispatch path; it does not protect a
// caller outside this package that calls providers.NewOpenAIProvider directly.
// Before this change such a caller reached llm.NewOpenAIProvider with a
// credential in hand and no gate anywhere on the path. The two checks are not
// redundant — they cover different entry points — and double-gating is free:
// same sentinel, and the outer check short-circuits before this function runs.
//
// §11.4.124 INVESTIGATION (why these are callerless, established from git
// history, not assumed). These were never wired to an external caller and then
// orphaned; they were never external API at all. At their introduction
// (015e2696 "Cognee integration") all thirteen were one-line stubs —
// `func NewOpenAIProvider(config *AIProviderConfig) AIProvider { return
// &MockAIProvider{} }` — created solely as the arms of createAIProvider's
// dispatch switch in this same file. a4bb2d37 replaced the mock with
// newNotImplementedProvider (anti-bluff sweep); 8dd4204c gave these three real
// bodies delegating to internal/llm. Their export is incidental to this file's
// house style (nearly every helper here is exported), not a published
// contract. So: NOT dead code — they are live, called by createAIProvider on
// every hosted arm — and NOT a regression from a deleted call site. Nothing is
// removed here; the reachable-but-ungated seam is closed instead. Deleting
// them would be a separate decision requiring operator sign-off (§11.4.122).
func NewOpenAIProvider(config *AIProviderConfig) AIProvider {
	if config == nil || config.Config == nil {
		return newNotImplementedProvider("OpenAI (missing config)")
	}

	// Build llm.ProviderConfigEntry from AIProviderConfig
	llmConfig := llm.ProviderConfigEntry{
		Type:       llm.ProviderTypeOpenAI,
		Enabled:    config.Enabled,
		Parameters: config.Config,
	}

	// Extract API key from config
	if apiKey, ok := config.Config["api_key"].(string); ok {
		llmConfig.APIKey = apiKey
	}
	if endpoint, ok := config.Config["endpoint"].(string); ok {
		llmConfig.Endpoint = endpoint
	}
	if model := config.Model; model != "" {
		llmConfig.Models = []string{model}
	}

	provider, err := llm.NewProvider(llmConfig)
	if err != nil {
		return newNotImplementedProvider(fmt.Sprintf("OpenAI (init failed: %v)", err))
	}

	return NewLLMProviderAdapter(provider, "OpenAI")
}
func NewAnthropicProvider(config *AIProviderConfig) AIProvider {
	if config == nil || config.Config == nil {
		return newNotImplementedProvider("Anthropic (missing config)")
	}

	// Build llm.ProviderConfigEntry from AIProviderConfig
	llmConfig := llm.ProviderConfigEntry{
		Type:       llm.ProviderTypeAnthropic,
		Enabled:    config.Enabled,
		Parameters: config.Config,
	}

	// Extract API key from config
	if apiKey, ok := config.Config["api_key"].(string); ok {
		llmConfig.APIKey = apiKey
	}
	if endpoint, ok := config.Config["endpoint"].(string); ok {
		llmConfig.Endpoint = endpoint
	}
	if model := config.Model; model != "" {
		llmConfig.Models = []string{model}
	}

	provider, err := llm.NewProvider(llmConfig)
	if err != nil {
		return newNotImplementedProvider(fmt.Sprintf("Anthropic (init failed: %v)", err))
	}

	return NewLLMProviderAdapter(provider, "Anthropic")
}

// The following providers are not yet implemented in internal/llm.
// They return clear errors indicating the functionality is not available.
// To add support: implement the provider in internal/llm package, then update the factory here.

func NewCohereProvider(config *AIProviderConfig) AIProvider {
	return newNotImplementedProvider("Cohere (not implemented in internal/llm)")
}
func NewHuggingFaceProvider(config *AIProviderConfig) AIProvider {
	return newNotImplementedProvider("HuggingFace (not implemented in internal/llm)")
}
func NewMistralProvider(config *AIProviderConfig) AIProvider {
	// Note: MistralRS is available via NewMistralRSProvider in internal/llm for local Mistral models
	return newNotImplementedProvider("Mistral (cloud API not implemented - use MistralRS for local)")
}
func NewGeminiProvider(config *AIProviderConfig) AIProvider {
	if config == nil || config.Config == nil {
		return newNotImplementedProvider("Gemini (missing config)")
	}

	// Build llm.ProviderConfigEntry from AIProviderConfig
	llmConfig := llm.ProviderConfigEntry{
		Type:       llm.ProviderTypeGemini,
		Enabled:    config.Enabled,
		Parameters: config.Config,
	}

	// Extract API key from config
	if apiKey, ok := config.Config["api_key"].(string); ok {
		llmConfig.APIKey = apiKey
	}
	if endpoint, ok := config.Config["endpoint"].(string); ok {
		llmConfig.Endpoint = endpoint
	}
	if model := config.Model; model != "" {
		llmConfig.Models = []string{model}
	}

	provider, err := llm.NewProvider(llmConfig)
	if err != nil {
		return newNotImplementedProvider(fmt.Sprintf("Gemini (init failed: %v)", err))
	}

	return NewLLMProviderAdapter(provider, "Gemini")
}
func NewGemmaProvider(config *AIProviderConfig) AIProvider {
	// Note: Gemma models can be run via Ollama or other local providers
	return newNotImplementedProvider("Gemma (use Ollama or local providers for Gemma models)")
}
func NewLlamaIndexProvider(config *AIProviderConfig) AIProvider {
	// LlamaIndex is a framework, not an LLM provider
	return newNotImplementedProvider("LlamaIndex (framework - use underlying LLM provider directly)")
}
func NewMemGPTAIProvider(config *AIProviderConfig) AIProvider {
	// MemGPT uses other LLM providers; see internal/memory for memory integration
	return newNotImplementedProvider("MemGPT (use memory integration with underlying LLM provider)")
}
func NewCrewAIProvider(config *AIProviderConfig) AIProvider {
	// CrewAI is an orchestration framework, not an LLM provider
	return newNotImplementedProvider("CrewAI (framework - use underlying LLM provider directly)")
}
func NewCharacterAIProvider(config *AIProviderConfig) AIProvider {
	return newNotImplementedProvider("CharacterAI (third-party API not implemented)")
}
func NewReplikaAIProvider(config *AIProviderConfig) AIProvider {
	return newNotImplementedProvider("Replika (third-party API not implemented)")
}
func NewAnimaAIProvider(config *AIProviderConfig) AIProvider {
	return newNotImplementedProvider("Anima (third-party API not implemented)")
}
