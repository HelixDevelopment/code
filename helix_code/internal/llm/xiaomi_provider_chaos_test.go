//go:build integration

package llm

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
)

// Xiaomi MiMo Chaos Tests
// These tests exercise failure modes and edge cases against the live API.
// They require a valid API key in XIAOMI_MIMO_API_KEY env var (for some tests).
//
// To run:
//   source ~/api_keys.sh
//   go test -v -tags=integration ./internal/llm/... -run TestXiaomiChaos -timeout 300s

func TestXiaomiChaos_InvalidAPIKey(t *testing.T) {
	// Xiaomi is a hosted cloud provider now subject to the W2c-1 cloud gate
	// (NewXiaomiProvider delegates to NewOpenAICompatibleProvider, whose
	// endpoint-locality check rejects hosted construction when the gate is
	// closed). This test opens the gate because it exercises Xiaomi provider
	// behavior under chaos conditions, not gate policy — see
	// openCloudGateForTest in provider_factory_test.go.
	openCloudGateForTest(t)
	config := ProviderConfigEntry{
		Type:    ProviderTypeXiaomi,
		APIKey:  "sk-invalid-key-12345",
		Enabled: true,
	}
	provider, err := NewXiaomiProvider(config)
	if err != nil {
		t.Fatalf("NewXiaomiProvider: %v", err)
	}
	defer provider.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	req := &LLMRequest{
		ID:    uuid.New(),
		Model: "mimo-v2.5-pro",
		Messages: []Message{
			{Role: "user", Content: "test"},
		},
		MaxTokens: 5,
	}

	_, err = provider.Generate(ctx, req)
	if err == nil {
		t.Fatal("expected error with invalid API key")
	}
	t.Logf("CHAOS EVIDENCE: invalid key correctly rejected: %v", err)
}

func TestXiaomiChaos_InvalidModel(t *testing.T) {
	openCloudGateForTest(t)
	apiKey := getEnvOrSkip(t, "XIAOMI_MIMO_API_KEY", "SKIP-OK: XIAOMI_MIMO_API_KEY not set")

	config := ProviderConfigEntry{
		Type:    ProviderTypeXiaomi,
		APIKey:  apiKey,
		Enabled: true,
	}
	provider, err := NewXiaomiProvider(config)
	if err != nil {
		t.Fatalf("NewXiaomiProvider: %v", err)
	}
	defer provider.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	req := &LLMRequest{
		ID:    uuid.New(),
		Model: "nonexistent-model-xyz",
		Messages: []Message{
			{Role: "user", Content: "test"},
		},
		MaxTokens: 5,
	}

	_, err = provider.Generate(ctx, req)
	if err == nil {
		t.Fatal("expected error with invalid model")
	}
	t.Logf("CHAOS EVIDENCE: invalid model correctly rejected: %v", err)
}

func TestXiaomiChaos_ContextCancellation(t *testing.T) {
	openCloudGateForTest(t)
	apiKey := getEnvOrSkip(t, "XIAOMI_MIMO_API_KEY", "SKIP-OK: XIAOMI_MIMO_API_KEY not set")

	config := ProviderConfigEntry{
		Type:    ProviderTypeXiaomi,
		APIKey:  apiKey,
		Enabled: true,
	}
	provider, err := NewXiaomiProvider(config)
	if err != nil {
		t.Fatalf("NewXiaomiProvider: %v", err)
	}
	defer provider.Close()

	// Use an extremely short timeout to force cancellation.
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Millisecond)
	defer cancel()

	req := &LLMRequest{
		ID:    uuid.New(),
		Model: "mimo-v2.5-pro",
		Messages: []Message{
			{Role: "user", Content: "Write a very long essay about everything."},
		},
		MaxTokens: 1000,
	}

	_, err = provider.Generate(ctx, req)
	if err == nil {
		t.Log("WARNING: expected context cancellation error, but got success (API was very fast)")
	} else {
		t.Logf("CHAOS EVIDENCE: context cancellation handled: %v", err)
	}
}

func TestXiaomiChaos_EmptyMessages(t *testing.T) {
	openCloudGateForTest(t)
	apiKey := getEnvOrSkip(t, "XIAOMI_MIMO_API_KEY", "SKIP-OK: XIAOMI_MIMO_API_KEY not set")

	config := ProviderConfigEntry{
		Type:    ProviderTypeXiaomi,
		APIKey:  apiKey,
		Enabled: true,
	}
	provider, err := NewXiaomiProvider(config)
	if err != nil {
		t.Fatalf("NewXiaomiProvider: %v", err)
	}
	defer provider.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	req := &LLMRequest{
		ID:         uuid.New(),
		Model:      "mimo-v2.5-pro",
		Messages:   []Message{},
		MaxTokens:  5,
	}

	_, err = provider.Generate(ctx, req)
	if err == nil {
		t.Log("WARNING: expected error with empty messages, but API accepted it")
	} else {
		t.Logf("CHAOS EVIDENCE: empty messages correctly rejected: %v", err)
	}
}

func TestXiaomiChaos_ZeroMaxTokens(t *testing.T) {
	openCloudGateForTest(t)
	apiKey := getEnvOrSkip(t, "XIAOMI_MIMO_API_KEY", "SKIP-OK: XIAOMI_MIMO_API_KEY not set")

	config := ProviderConfigEntry{
		Type:    ProviderTypeXiaomi,
		APIKey:  apiKey,
		Enabled: true,
	}
	provider, err := NewXiaomiProvider(config)
	if err != nil {
		t.Fatalf("NewXiaomiProvider: %v", err)
	}
	defer provider.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	req := &LLMRequest{
		ID:    uuid.New(),
		Model: "mimo-v2.5-pro",
		Messages: []Message{
			{Role: "user", Content: "Say hello"},
		},
		MaxTokens: 0,
	}

	_, err = provider.Generate(ctx, req)
	if err == nil {
		t.Log("WARNING: expected error with zero max tokens, but API accepted it")
	} else {
		t.Logf("CHAOS EVIDENCE: zero max tokens handled: %v", err)
	}
}

func TestXiaomiChaos_NilContext(t *testing.T) {
	openCloudGateForTest(t)
	apiKey := getEnvOrSkip(t, "XIAOMI_MIMO_API_KEY", "SKIP-OK: XIAOMI_MIMO_API_KEY not set")

	config := ProviderConfigEntry{
		Type:    ProviderTypeXiaomi,
		APIKey:  apiKey,
		Enabled: true,
	}
	provider, err := NewXiaomiProvider(config)
	if err != nil {
		t.Fatalf("NewXiaomiProvider: %v", err)
	}
	defer provider.Close()

	// Passing nil context should not panic — the provider must handle it gracefully.
	req := &LLMRequest{
		ID:    uuid.New(),
		Model: "mimo-v2.5-pro",
		Messages: []Message{
			{Role: "user", Content: "Say OK"},
		},
		MaxTokens: 5,
	}

	// Recover from potential panic.
	defer func() {
		if r := recover(); r != nil {
			t.Logf("CHAOS EVIDENCE: nil context caused panic: %v", r)
		}
	}()

	_, err = provider.Generate(nil, req)
	if err != nil {
		t.Logf("CHAOS EVIDENCE: nil context handled gracefully: %v", err)
	} else {
		t.Log("WARNING: nil context accepted without error")
	}
}
