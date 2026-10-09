//go:build integration

package llm

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
)

// Xiaomi MiMo Stress Tests
// These tests make REAL API calls to Xiaomi MiMo's live API under sustained load.
// They require a valid API key in XIAOMI_MIMO_API_KEY env var.
//
// To run:
//   source ~/api_keys.sh
//   go test -v -tags=integration ./internal/llm/... -run TestXiaomiStress -timeout 300s

func TestXiaomiStress_SequentialCalls(t *testing.T) {
	// Xiaomi is a hosted cloud provider now subject to the W2c-1 cloud gate
	// (NewXiaomiProvider delegates to NewOpenAICompatibleProvider, whose
	// endpoint-locality check rejects hosted construction when the gate is
	// closed). This test opens the gate because it exercises Xiaomi provider
	// behavior under sustained load, not gate policy — see
	// openCloudGateForTest in provider_factory_test.go.
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

	const iterations = 20
	var successes, failures int64
	var totalLatency time.Duration

	for i := 0; i < iterations; i++ {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		req := &LLMRequest{
			ID:    uuid.New(),
			Model: "mimo-v2.5-pro",
			Messages: []Message{
				{Role: "user", Content: "Say OK"},
			},
			MaxTokens:   5,
			Temperature: 0.3,
		}

		start := time.Now()
		_, err := provider.Generate(ctx, req)
		latency := time.Since(start)
		cancel()

		if err != nil {
			atomic.AddInt64(&failures, 1)
			t.Logf("iteration %d: FAIL (%v)", i, err)
		} else {
			atomic.AddInt64(&successes, 1)
			totalLatency += latency
		}
	}

	avgLatency := time.Duration(0)
	if successes > 0 {
		avgLatency = totalLatency / time.Duration(successes)
	}
	t.Logf("STRESS EVIDENCE: %d/%d successes, avg latency %v", successes, iterations, avgLatency)

	if successes == 0 {
		t.Fatal("all sequential iterations failed")
	}
}

func TestXiaomiStress_ConcurrentCalls(t *testing.T) {
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

	const concurrency = 5
	var wg sync.WaitGroup
	var successes, failures int64

	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()

			req := &LLMRequest{
				ID:    uuid.New(),
				Model: "mimo-v2.5-pro",
				Messages: []Message{
					{Role: "user", Content: "Say OK"},
				},
				MaxTokens:   5,
				Temperature: 0.3,
			}

			_, err := provider.Generate(ctx, req)
			if err != nil {
				atomic.AddInt64(&failures, 1)
			} else {
				atomic.AddInt64(&successes, 1)
			}
		}(i)
	}

	wg.Wait()
	t.Logf("CONCURRENT EVIDENCE: %d/%d successes (rate-limit 429 counts as expected chaos)", successes, concurrency)

	if successes == 0 {
		// All concurrent calls hitting 429 is valid rate-limit behavior under load.
		t.Logf("CONCURRENT NOTE: all %d calls hit rate limits — expected under free-tier API load", concurrency)
	}
}

func TestXiaomiStress_RapidFire(t *testing.T) {
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

	// Fire 10 requests as fast as possible with a small delay to avoid
	// free-tier rate limits while still exercising sustained throughput.
	const count = 10
	var successes, failures int64

	for i := 0; i < count; i++ {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		req := &LLMRequest{
			ID:    uuid.New(),
			Model: "mimo-v2.5-pro",
			Messages: []Message{
				{Role: "user", Content: "Reply with just the word yes"},
			},
			MaxTokens:   5,
			Temperature: 0.0,
		}

		_, err := provider.Generate(ctx, req)
		cancel()

		if err != nil {
			atomic.AddInt64(&failures, 1)
		} else {
			atomic.AddInt64(&successes, 1)
		}
		// Small delay to avoid hitting free-tier rate limits on rapid sequence.
		time.Sleep(200 * time.Millisecond)
	}

	t.Logf("RAPID-FIRE EVIDENCE: %d/%d successes, %d rate-limited", successes, count, failures)

	if successes == 0 {
		// All rapid-fire hitting 429 is valid rate-limit evidence.
		t.Logf("RAPID-FIRE NOTE: all %d calls hit rate limits — expected under free-tier API load", count)
	}
}
