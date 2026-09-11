// Package provider provides HelixLLM provider management and startup registration.
package provider

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"

	"dev.helix.code/internal/llm"
)

// ProviderBridgeInterface defines the interface for provider enumeration and management.
type ProviderBridgeInterface interface {
	ListProviders() ([]ProviderEntry, error)
	GetProviderByType(providerType string) (*ProviderEntry, error)
	GetAllModels() ([]ModelSummary, error)
	HealthCheck() (map[string]bool, error)
}

// ProviderRegistration handles provider registration on startup.
type ProviderRegistration struct {
	bridge          ProviderBridgeInterface
	timeout         time.Duration
	registeredCount int
	mu              sync.RWMutex
}

// NewProviderRegistration creates a new ProviderRegistration with default 30s timeout.
func NewProviderRegistration(bridge ProviderBridgeInterface) *ProviderRegistration {
	return &ProviderRegistration{
		bridge:  bridge,
		timeout: 30 * time.Second,
	}
}

// RegisterAllProviders registers all providers on startup.
// Returns the number of providers registered.
func (pr *ProviderRegistration) RegisterAllProviders(ctx context.Context) (int, error) {
	pr.mu.Lock()
	defer pr.mu.Unlock()

	ctx, cancel := context.WithTimeout(ctx, pr.timeout)
	defer cancel()

	providers, err := pr.bridge.ListProviders()
	if err != nil {
		return 0, fmt.Errorf("failed to list providers: %w", err)
	}

	if len(providers) == 0 {
		log.Printf("No providers found to register")
		return 0, nil
	}

	// Verify health of each provider
	health, err := pr.bridge.HealthCheck()
	if err != nil {
		log.Printf("Health check failed: %v", err)
	}

	registered := 0
	for _, provider := range providers {
		if healthy, ok := health[provider.Name]; ok && healthy {
			registered++
			log.Printf("Provider registered: %s (%s) - healthy", provider.Name, provider.Type)
		} else {
			log.Printf("Provider registered: %s (%s) - unhealthy or unknown", provider.Name, provider.Type)
			registered++
		}
	}

	pr.registeredCount = registered
	return registered, nil
}

// StartupHook is the entry point called during application startup.
// It initializes the provider bridge, enumerates providers, and starts them.
func StartupHook(ctx context.Context, modelManager *llm.ModelManager) (*ProviderRegistration, error) {
	if modelManager == nil {
		return nil, errors.New("model manager is nil")
	}

	bridge := &ProviderBridge{
		modelManager: modelManager,
	}

	registration := NewProviderRegistration(bridge)

	// Allow extra time for initialization
	ctx, cancel := context.WithTimeout(ctx, 35*time.Second)
	defer cancel()

	count, err := registration.RegisterAllProviders(ctx)
	if err != nil {
		return nil, fmt.Errorf("provider registration failed: %w", err)
	}

	log.Printf("HelixLLM startup complete: %d providers registered within timeout", count)
	return registration, nil
}

// getProviderBaseDir returns the base directory for provider data.
// Uses $HELIX_PROVIDER_DIR or defaults to ~/.helix
func getProviderBaseDir() string {
	if dir := os.Getenv("HELIX_PROVIDER_DIR"); dir != "" {
		return dir
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ".helix"
	}
	return filepath.Join(home, ".helix")
}

// GetProvidersConfigPath returns the path to the providers configuration file.
func GetProvidersConfigPath() string {
	return filepath.Join(getProviderBaseDir(), "model-aliases.yaml")
}

// EnsureProvidersConfig ensures the providers configuration directory exists.
func EnsureProvidersConfig() error {
	dir := getProviderBaseDir()
	return os.MkdirAll(dir, 0755)
}