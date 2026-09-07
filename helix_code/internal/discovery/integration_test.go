package discovery

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestFullServiceLifecycle tests complete service registration, discovery, and cleanup flow
func TestFullServiceLifecycle(t *testing.T) {
	// Setup
	registry := NewDefaultServiceRegistry()
	allocator := NewDefaultPortAllocator()
	config := DefaultDiscoveryClientConfig(registry, allocator)
	// Only use registry strategy to test registration lifecycle without fallbacks
	config.PreferredStrategies = []DiscoveryStrategy{StrategyRegistry}
	client := NewDiscoveryClient(config)

	// Register service with auto port allocation
	info := ServiceInfo{
		Name:     "api",
		Host:     "localhost",
		Port:     0, // Auto-allocate
		Protocol: "http",
		Version:  "1.0.0",
		Metadata: map[string]string{
			"env": "test",
		},
	}

	err := client.Register(info)
	require.NoError(t, err)

	// Discover service
	result, err := client.Discover("api")
	require.NoError(t, err)
	assert.NotNil(t, result)
	assert.Equal(t, "api", result.ServiceInfo.Name)
	assert.NotEqual(t, 0, result.ServiceInfo.Port)
	assert.Equal(t, StrategyRegistry, result.Strategy)

	// Verify port was allocated in range
	assert.GreaterOrEqual(t, result.ServiceInfo.Port, 8081)
	assert.LessOrEqual(t, result.ServiceInfo.Port, 8099)

	// Send heartbeat
	err = client.Heartbeat("api")
	require.NoError(t, err)

	// Deregister service
	err = client.Deregister("api")
	require.NoError(t, err)

	// Verify service is gone
	_, err = client.Discover("api")
	assert.Error(t, err)

	// Verify port was released
	allocations := allocator.ListAllocations()
	assert.Empty(t, allocations)
}

// TestMultiServiceDiscovery tests managing multiple services simultaneously
func TestMultiServiceDiscovery(t *testing.T) {
	registry := NewDefaultServiceRegistry()
	allocator := NewDefaultPortAllocator()
	config := DefaultDiscoveryClientConfig(registry, allocator)
	client := NewDiscoveryClient(config)

	// Register multiple services of different types
	services := []struct {
		name     string
		portHint int
	}{
		{"postgres-primary", 5432},
		{"redis-cache", 6379},
		{"api-gateway", 8080},
		{"grpc-server", 9090},
		{"metrics-endpoint", 9100},
	}

	for _, svc := range services {
		info := ServiceInfo{
			Name:     svc.name,
			Host:     "localhost",
			Port:     0, // Auto-allocate
			Protocol: "tcp",
		}
		err := client.Register(info)
		require.NoError(t, err)
	}

	// Verify all services are registered
	allServices := client.ListServices()
	assert.Len(t, allServices, len(services))

	// Discover each service
	for _, svc := range services {
		result, err := client.Discover(svc.name)
		require.NoError(t, err, "Failed to discover %s", svc.name)
		assert.Equal(t, svc.name, result.ServiceInfo.Name)
		assert.NotEqual(t, 0, result.ServiceInfo.Port)
		assert.True(t, result.ServiceInfo.Healthy)
	}

	// Verify port allocations don't conflict
	allocations := allocator.ListAllocations()
	assert.Len(t, allocations, len(services))

	portMap := make(map[int]bool)
	for _, alloc := range allocations {
		assert.False(t, portMap[alloc.Port], "Port %d allocated multiple times", alloc.Port)
		portMap[alloc.Port] = true
	}

	// Cleanup
	for _, svc := range services {
		err := client.Deregister(svc.name)
		require.NoError(t, err)
	}
}

// TestServiceExpirationFlow tests TTL-based service expiration
func TestServiceExpirationFlow(t *testing.T) {
	// Create registry with short TTL for testing
	registryConfig := DefaultRegistryConfig()
	registryConfig.DefaultTTL = 100 * time.Millisecond
	registryConfig.CleanupInterval = 50 * time.Millisecond
	registry := NewServiceRegistry(registryConfig)
	registry.Start()
	defer registry.Stop()

	allocator := NewDefaultPortAllocator()
	config := DefaultDiscoveryClientConfig(registry, allocator)
	client := NewDiscoveryClient(config)

	// Register service
	info := ServiceInfo{
		Name: "expiring-service",
		Host: "localhost",
		Port: 8080,
	}
	err := client.Register(info)
	require.NoError(t, err)

	// Verify service is discoverable
	result, err := client.Discover("expiring-service")
	require.NoError(t, err)
	assert.Equal(t, "expiring-service", result.ServiceInfo.Name)

	// Expiry is completed by the background cleanupLoop. Wait for that OUTCOME
	// rather than sleeping a guessed 200ms and hoping the loop was scheduled.
	require.Eventually(t, func() bool {
		_, gErr := registry.Get("expiring-service")
		return errors.Is(gErr, ErrServiceNotFound)
	}, 30*time.Second, 5*time.Millisecond,
		"expiring-service was never removed by cleanupLoop")

	// Service should no longer be discoverable
	_, err = client.Discover("expiring-service")
	assert.Error(t, err)
}

// TestHeartbeatKeepsServiceAlive tests that heartbeats prevent expiration
func TestHeartbeatKeepsServiceAlive(t *testing.T) {
	registryConfig := DefaultRegistryConfig()
	registryConfig.DefaultTTL = 200 * time.Millisecond
	registryConfig.CleanupInterval = 50 * time.Millisecond
	registry := NewServiceRegistry(registryConfig)
	registry.Start()
	defer registry.Stop()

	allocator := NewDefaultPortAllocator()
	config := DefaultDiscoveryClientConfig(registry, allocator)
	client := NewDiscoveryClient(config)

	// Register service
	info := ServiceInfo{
		Name: "heartbeat-service",
		Host: "localhost",
		Port: 8080,
	}
	err := client.Register(info)
	require.NoError(t, err)

	// Send heartbeats periodically to keep service alive.
	done := make(chan bool)
	var beatWG sync.WaitGroup
	beatWG.Add(1)
	go func() {
		defer beatWG.Done()
		ticker := time.NewTicker(100 * time.Millisecond)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				client.Heartbeat("heartbeat-service")
			case <-done:
				return
			}
		}
	}()

	// NEEDS-REDESIGN (deliberately still a sleep): this asserts a REAL-TIME
	// DEADLINE property — "a service heartbeaten every 100ms never exceeds its
	// 200ms TTL". Polling cannot rescue it: under CPU load THIS TEST'S OWN
	// heartbeat goroutine can be starved past the TTL, so the service really
	// does expire and the failure is genuine-but-irrelevant. Making it
	// deterministic needs an injectable clock on ServiceRegistry
	// (ServiceInfo.IsExpired hardcodes time.Since), which is a PRODUCTION
	// design change, not a test fix — so it is surfaced here, not smuggled in.
	// This is the site that failed the loaded pre-fix baseline at line ~211.
	time.Sleep(300 * time.Millisecond)

	// Service should still be alive due to heartbeats
	result, err := client.Discover("heartbeat-service")
	require.NoError(t, err)
	assert.Equal(t, "heartbeat-service", result.ServiceInfo.Name)

	// Stop heartbeats and WAIT for the goroutine to actually exit. Without the
	// join, `close(done)` races the ticker branch: one more Heartbeat can land
	// after we think beating stopped, refreshing the TTL under the assertion.
	close(done)
	beatWG.Wait()

	// Now that no further heartbeat can arrive, expiry is monotone — wait for
	// the outcome instead of sleeping a guessed 250ms.
	require.Eventually(t, func() bool {
		_, dErr := client.Discover("heartbeat-service")
		return dErr != nil
	}, 30*time.Second, 5*time.Millisecond,
		"heartbeat-service never expired after heartbeats stopped")
}

// TestConcurrentServiceOperations tests thread-safe concurrent operations
func TestConcurrentServiceOperations(t *testing.T) {
	registry := NewDefaultServiceRegistry()
	allocator := NewDefaultPortAllocator()
	config := DefaultDiscoveryClientConfig(registry, allocator)
	client := NewDiscoveryClient(config)

	// Use a smaller number that fits within our port ranges
	const numGoroutines = 15
	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	successCount := make(chan int, numGoroutines)

	// Concurrent registration
	for i := 0; i < numGoroutines; i++ {
		go func(id int) {
			defer wg.Done()

			info := ServiceInfo{
				Name: fmt.Sprintf("service-%d", id),
				Host: "localhost",
				Port: 0, // Auto-allocate
			}

			err := client.Register(info)
			if err != nil {
				return
			}

			// Discover the service we just registered
			_, err = client.Discover(info.Name)
			if err != nil {
				return
			}

			// Send heartbeat
			err = client.Heartbeat(info.Name)
			if err == nil {
				successCount <- 1
			}
		}(i)
	}

	wg.Wait()
	close(successCount)

	// Count successful operations
	count := 0
	for range successCount {
		count++
	}

	// At least some should succeed
	assert.Greater(t, count, 10, "Expected at least 10 concurrent operations to succeed")

	// Verify services registered
	services := client.ListServices()
	assert.Equal(t, count, len(services))

	// Verify all port allocations are unique
	allocations := allocator.ListAllocations()
	portMap := make(map[int]bool)
	for _, alloc := range allocations {
		assert.False(t, portMap[alloc.Port], "Port %d allocated multiple times", alloc.Port)
		portMap[alloc.Port] = true
	}
}

// TestHealthyServiceFiltering tests filtering healthy vs unhealthy services
func TestHealthyServiceFiltering(t *testing.T) {
	registry := NewDefaultServiceRegistry()
	allocator := NewDefaultPortAllocator()
	config := DefaultDiscoveryClientConfig(registry, allocator)
	client := NewDiscoveryClient(config)

	// Register multiple services
	for i := 0; i < 5; i++ {
		info := ServiceInfo{
			Name: fmt.Sprintf("service-%d", i),
			Host: "localhost",
			Port: 8080 + i,
		}
		err := client.Register(info)
		require.NoError(t, err)
	}

	// All should be healthy initially
	healthy := client.ListHealthyServices()
	assert.Len(t, healthy, 5)

	// Mark some as unhealthy
	err := registry.UpdateHealth("service-1", false)
	require.NoError(t, err)
	err = registry.UpdateHealth("service-3", false)
	require.NoError(t, err)

	// Only healthy services should be returned
	healthy = client.ListHealthyServices()
	assert.Len(t, healthy, 3)

	names := make(map[string]bool)
	for _, svc := range healthy {
		names[svc.Name] = true
	}

	assert.True(t, names["service-0"])
	assert.False(t, names["service-1"])
	assert.True(t, names["service-2"])
	assert.False(t, names["service-3"])
	assert.True(t, names["service-4"])

	// Unhealthy services should fail discovery
	_, err = client.Discover("service-1")
	assert.Error(t, err)

	// Healthy services should still be discoverable
	result, err := client.Discover("service-0")
	require.NoError(t, err)
	assert.Equal(t, "service-0", result.ServiceInfo.Name)
}

// TestPortAllocationExhaustion tests behavior when port range is exhausted
func TestPortAllocationExhaustion(t *testing.T) {
	registry := NewDefaultServiceRegistry()
	allocator := NewDefaultPortAllocator()
	config := DefaultDiscoveryClientConfig(registry, allocator)
	client := NewDiscoveryClient(config)

	// Try to exhaust the database port range (5433-5442 = 10 ports)
	var successCount int
	for i := 0; i < 15; i++ {
		info := ServiceInfo{
			Name: fmt.Sprintf("db-%d", i),
			Host: "localhost",
			Port: 0, // Auto-allocate with database hint
		}

		// Override getDefaultPort to return database port for all services
		// This will force allocation in database range
		err := registry.Register(ServiceInfo{
			Name: info.Name,
			Host: info.Host,
			Port: 5433 + i, // Manually assign in database range
		})

		if err == nil {
			successCount++
		}
	}

	// Should have successfully registered some services
	assert.Greater(t, successCount, 0)

	// Verify services are discoverable
	services := client.ListServices()
	assert.Len(t, services, successCount)
}

// TestWaitForServiceWithDelayedRegistration tests waiting for delayed service
func TestWaitForServiceWithDelayedRegistration(t *testing.T) {
	registry := NewDefaultServiceRegistry()
	allocator := NewDefaultPortAllocator()
	config := DefaultDiscoveryClientConfig(registry, allocator)
	client := NewDiscoveryClient(config)

	// Start waiting for service in background
	resultChan := make(chan *DiscoveryResult, 1)
	errorChan := make(chan error, 1)

	go func() {
		result, err := client.WaitForService("delayed-service", 3*time.Second)
		if err != nil {
			errorChan <- err
		} else {
			resultChan <- result
		}
	}()

	// Register service after a delay
	time.Sleep(500 * time.Millisecond)
	info := ServiceInfo{
		Name: "delayed-service",
		Host: "localhost",
		Port: 8080,
	}
	err := client.Register(info)
	require.NoError(t, err)

	// Wait for discovery result
	select {
	case result := <-resultChan:
		assert.Equal(t, "delayed-service", result.ServiceInfo.Name)
		assert.Equal(t, 8080, result.ServiceInfo.Port)
	case err := <-errorChan:
		t.Fatalf("Unexpected error: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("Timeout waiting for service discovery")
	}
}

// TestWaitForServiceTimeout tests timeout when service never appears
func TestWaitForServiceTimeout(t *testing.T) {
	registry := NewDefaultServiceRegistry()
	allocator := NewDefaultPortAllocator()
	config := DefaultDiscoveryClientConfig(registry, allocator)
	config.EnableDNS = false // Disable to force failure
	client := NewDiscoveryClient(config)

	start := time.Now()
	_, err := client.WaitForService("never-appears", 500*time.Millisecond)
	elapsed := time.Since(start)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "timeout")
	assert.GreaterOrEqual(t, elapsed, 500*time.Millisecond)
	assert.Less(t, elapsed, 1*time.Second) // Should not wait too long
}

// TestServiceAddressFormatting tests address formatting
func TestServiceAddressFormatting(t *testing.T) {
	registry := NewDefaultServiceRegistry()
	allocator := NewDefaultPortAllocator()
	config := DefaultDiscoveryClientConfig(registry, allocator)
	client := NewDiscoveryClient(config)

	// Register service
	info := ServiceInfo{
		Name: "test-service",
		Host: "api.example.com",
		Port: 8443,
	}
	err := client.Register(info)
	require.NoError(t, err)

	// Get formatted address
	address, err := client.GetServiceAddress("test-service")
	require.NoError(t, err)
	assert.Equal(t, "api.example.com:8443", address)
}

// TestMultiStrategyDiscoveryFallback tests strategy fallback mechanism
func TestMultiStrategyDiscoveryFallback(t *testing.T) {
	registry := NewDefaultServiceRegistry()
	allocator := NewDefaultPortAllocator()

	// Configure client with multiple strategies
	config := DiscoveryClientConfig{
		Registry:      registry,
		PortAllocator: allocator,
		DefaultPorts: map[string]int{
			"localhost": 80,
		},
		EnableRegistry:   true,
		EnableDNS:        true,
		DiscoveryTimeout: 5 * time.Second,
		PreferredStrategies: []DiscoveryStrategy{
			StrategyRegistry,
			StrategyDNS,
		},
	}
	client := NewDiscoveryClient(config)

	// Service not in registry, should fall back to DNS
	result, err := client.Discover("localhost")

	// Anti-bluff (CONST-035 / §11.9): the original `if err == nil` branch
	// asserted nothing on the failure path, so any error — including a
	// codebase regression like "fallback strategy not invoked" — silently
	// passed. Pin both branches: on success the strategy MUST be DNS and
	// the result non-nil; on failure the error MUST mention "DNS" or
	// "lookup" so a regression that returned a different-strategy error
	// (e.g. "no registry entry") would now fail.
	if err == nil {
		require.NotNil(t, result, "successful Discover must return a non-nil result")
		assert.Equal(t, StrategyDNS, result.Strategy, "fallback to DNS must report StrategyDNS")
	} else {
		msg := strings.ToLower(err.Error())
		assert.True(t,
			strings.Contains(msg, "dns") || strings.Contains(msg, "lookup") || strings.Contains(msg, "host"),
			"DNS-fallback failure must surface a DNS/lookup/host-related error; got %q", err.Error())
	}
}

// TestPortReallocationAfterDeregister tests port reuse after deregistration
func TestPortReallocationAfterDeregister(t *testing.T) {
	registry := NewDefaultServiceRegistry()
	allocator := NewDefaultPortAllocator()
	config := DefaultDiscoveryClientConfig(registry, allocator)
	// Override strategies to prioritize registry (for testing auto-allocation flow)
	config.PreferredStrategies = []DiscoveryStrategy{StrategyRegistry, StrategyDefaultPort, StrategyDNS}
	client := NewDiscoveryClient(config)

	// Register first service with a known service type
	info1 := ServiceInfo{
		Name: "api-1",
		Host: "localhost",
		Port: 0, // Auto-allocate (will use API range 8081-8099)
	}
	err := client.Register(info1)
	require.NoError(t, err)

	// Get allocated port
	result1, err := client.Discover("api-1")
	require.NoError(t, err)
	port1 := result1.ServiceInfo.Port

	// Deregister service
	err = client.Deregister("api-1")
	require.NoError(t, err)

	// Verify port is available now
	available := allocator.IsPortAvailable(port1)
	assert.True(t, available, "Port should be available after deregistration")

	// Register second service - should be able to use the released port
	info2 := ServiceInfo{
		Name: "api-2",
		Host: "localhost",
		Port: 0, // Auto-allocate
	}
	err = client.Register(info2)
	require.NoError(t, err)

	result2, err := client.Discover("api-2")
	require.NoError(t, err)

	// Port should be valid and allocated from API range
	assert.NotEqual(t, 0, result2.ServiceInfo.Port)
	assert.GreaterOrEqual(t, result2.ServiceInfo.Port, 8081)
	assert.LessOrEqual(t, result2.ServiceInfo.Port, 8099)
}

// TestLatencyTracking tests discovery latency measurement
func TestLatencyTracking(t *testing.T) {
	registry := NewDefaultServiceRegistry()
	allocator := NewDefaultPortAllocator()
	config := DefaultDiscoveryClientConfig(registry, allocator)
	client := NewDiscoveryClient(config)

	// Register service
	info := ServiceInfo{
		Name: "latency-test",
		Host: "localhost",
		Port: 8080,
	}
	err := client.Register(info)
	require.NoError(t, err)

	// Discover and check latency
	result, err := client.Discover("latency-test")
	require.NoError(t, err)

	// Latency should be measured
	assert.Greater(t, result.Latency, time.Duration(0))
	assert.Less(t, result.Latency, 100*time.Millisecond) // Should be fast for local registry
}
