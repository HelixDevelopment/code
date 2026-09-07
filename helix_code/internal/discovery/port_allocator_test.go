package discovery

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// testSafeBasePort returns a base port for `count` consecutive test
// allocations that is guaranteed to sit OUTSIDE both (a) the kernel ephemeral
// range and (b) this allocator's own fallback block.
//
// WHY THIS EXISTS (measured, §11.4.50): these tests previously hardcoded
// preferred ports 50000-50049 and 55555, which are INSIDE the Linux default
// ephemeral range 32768-60999. isPortAvailableUnsafe probes each preferred port
// with a real bind, so once the host had accumulated TIME-WAIT pressure in that
// range (a full-suite run was measured driving it to 100% occupancy) those
// probes failed, every goroutine fell through to the 19-port `api` fallback
// 8081-8099, and the tests failed with "no ports available in configured
// range". They passed on an idle host and failed under back-to-back runs — i.e.
// they were asserting on host state, not on the allocator.
//
// The base is DERIVED at runtime rather than hardcoded (§11.4.111): hardcoding
// another literal would just relocate the same brittleness to whatever this
// host's range happens to be today. If no window is available the test SKIPs
// honestly rather than silently reusing a colliding range (§11.4.3).
// safeTestPort returns a port that is outside BOTH the kernel ephemeral range
// and this allocator's fallback block, at a fixed offset from a base computed
// once per test binary.
//
// The hardcoded safeTestPort(0) / safeTestPort(1) these replaced sat inside the Linux default
// ephemeral range 32768-60999. isPortAvailableUnsafe probes a preferred port
// with a real bind, so under accumulated TIME-WAIT pressure those probes failed
// and the tests asserted on host state rather than on allocator behaviour
// (§11.4.50). Derived, not hardcoded, per §11.4.111 — a fresh literal would
// only relocate the same brittleness.
var safeTestPortBase = func() int {
	_, ephHigh := ephemeralPortRange()
	base := ephHigh + 1
	if fb, err := fallbackPortRange(); err == nil && fb.End+1 > base {
		base = fb.End + 1
	}
	// Leave headroom for the largest offset any caller uses.
	if base+128 > maxPort {
		return 0 // signals "no safe window"; callers skip
	}
	return base
}()

func safeTestPort(offset int) int { return safeTestPortBase + offset }

func testSafeBasePort(t *testing.T, count int) int {
	t.Helper()

	_, ephHigh := ephemeralPortRange()
	fb, err := fallbackPortRange()
	if err != nil {
		t.Skipf("SKIP-OK: no fallback block fits outside the ephemeral range on this host: %v", err)
	}

	// Start above BOTH the ephemeral range and the allocator's fallback block,
	// so a test allocation can collide with neither.
	base := fb.End + 1
	if base <= ephHigh {
		base = ephHigh + 1
	}
	if base+count > maxPort {
		t.Skipf("SKIP-OK: no room for %d test ports above ephemeral high %d and fallback block %d-%d",
			count, ephHigh, fb.Start, fb.End)
	}
	return base
}

func TestNewPortAllocator(t *testing.T) {
	config := DefaultPortAllocatorConfig()
	pa := NewPortAllocator(config)

	assert.NotNil(t, pa)
	assert.NotNil(t, pa.allocations)
	assert.NotNil(t, pa.serviceMap)
	assert.Equal(t, config, pa.config)
}

func TestNewDefaultPortAllocator(t *testing.T) {
	pa := NewDefaultPortAllocator()

	assert.NotNil(t, pa)
	assert.False(t, pa.config.AllowEphemeral)
	assert.Len(t, pa.config.PortRanges, 6)
	assert.Contains(t, pa.config.PortRanges, "database")
	assert.Contains(t, pa.config.PortRanges, "cache")
}

func TestAllocatePort_PreferredAvailable(t *testing.T) {
	pa := NewDefaultPortAllocator()

	// Try to allocate with preferred port
	// If safeTestPort(0) isn't available, it will fall back to api range (8081-8099)
	preferredPort := testSafeBasePort(t, 1)
	port, err := pa.AllocatePort("test-service", preferredPort)

	require.NoError(t, err)
	assert.NotEqual(t, 0, port, "Should allocate a valid port")

	// Port should be either the preferred or in the api range
	if port != preferredPort {
		// Fell back to api range
		assert.GreaterOrEqual(t, port, 8081)
		assert.LessOrEqual(t, port, 8099)
	}

	// Verify allocation exists for whichever port was allocated
	allocation, exists := pa.GetAllocation(port)
	assert.True(t, exists)
	assert.Equal(t, "test-service", allocation.ServiceName)
	assert.Equal(t, port, allocation.Port)
}

func TestAllocatePort_PreferredOccupied_FallbackToRange(t *testing.T) {
	pa := NewDefaultPortAllocator()

	// Allocate first PostgreSQL port
	port1, err := pa.AllocatePort("postgres-1", 5432)
	require.NoError(t, err)

	// Since 5432 is reserved, it should fallback to range 5433-5442
	assert.GreaterOrEqual(t, port1, 5433)
	assert.LessOrEqual(t, port1, 5442)

	// Allocate second PostgreSQL port (should get next in range)
	port2, err := pa.AllocatePort("postgres-2", 5432)
	require.NoError(t, err)

	assert.GreaterOrEqual(t, port2, 5433)
	assert.LessOrEqual(t, port2, 5442)
	assert.NotEqual(t, port1, port2)
}

func TestAllocatePort_ServiceAlreadyHasPort(t *testing.T) {
	pa := NewDefaultPortAllocator()

	// Allocate port (may get preferred or fallback)
	port1, err := pa.AllocatePort("test-service", safeTestPort(0))
	require.NoError(t, err)
	assert.NotEqual(t, 0, port1, "Should allocate a valid port")

	// Try to allocate again for same service with different preferred port
	port2, err := pa.AllocatePort("test-service", safeTestPort(1))
	require.NoError(t, err)
	assert.Equal(t, port1, port2, "Should return existing port for same service")
}

func TestAllocatePort_ReservedPort(t *testing.T) {
	pa := NewDefaultPortAllocator()

	// Try to allocate a reserved port (5432 is in reserved list)
	port, err := pa.AllocatePort("postgres", 5432)

	// Should fall back to range since 5432 is reserved
	require.NoError(t, err)
	assert.NotEqual(t, 5432, port)
	assert.GreaterOrEqual(t, port, 5433)
	assert.LessOrEqual(t, port, 5442)
}

func TestAllocatePort_RangeExhausted_NoEphemeral(t *testing.T) {
	// Create a custom allocator with a small port range for this test
	config := PortAllocatorConfig{
		PortRanges: map[string]PortRange{
			"test": {Start: 15000, End: 15002}, // Only 3 ports
		},
		ReservedPorts:  []int{},
		AllowEphemeral: false,
	}
	pa := NewPortAllocator(config)

	// Exhaust the test port range (3 ports)
	for i := 0; i < 3; i++ {
		_, err := pa.AllocatePortInRange(fmt.Sprintf("test-%d", i), 15000, 15002)
		require.NoError(t, err)
	}

	// Try to allocate one more
	_, err := pa.AllocatePortInRange("test-4", 15000, 15002)
	assert.ErrorIs(t, err, ErrNoPortsAvailable)
}

func TestAllocatePort_RangeExhausted_WithEphemeral(t *testing.T) {
	config := DefaultPortAllocatorConfig()
	config.AllowEphemeral = true
	pa := NewPortAllocator(config)

	// Exhaust the database port range
	for i := 0; i < 10; i++ {
		_, err := pa.AllocatePort(fmt.Sprintf("db-%d", i), 5432)
		require.NoError(t, err)
	}

	// Try to allocate one more - should get ephemeral
	port, err := pa.AllocatePort("db-11", 5432)
	require.NoError(t, err)
	assert.NotEqual(t, 0, port)
	assert.True(t, port > 1024, "Ephemeral port should be > 1024")
}

func TestAllocatePortInRange(t *testing.T) {
	pa := NewDefaultPortAllocator()

	port, err := pa.AllocatePortInRange("test-service", 7000, 7010)

	require.NoError(t, err)
	assert.GreaterOrEqual(t, port, 7000)
	assert.LessOrEqual(t, port, 7010)
}

func TestAllocatePortInRange_InvalidRange(t *testing.T) {
	pa := NewDefaultPortAllocator()

	tests := []struct {
		name      string
		startPort int
		endPort   int
	}{
		{"start < 1", 0, 100},
		{"end > 65535", 1000, 70000},
		{"start > end", 200, 100},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := pa.AllocatePortInRange("test", tt.startPort, tt.endPort)
			assert.ErrorIs(t, err, ErrInvalidPortRange)
		})
	}
}

func TestReleasePort(t *testing.T) {
	pa := NewDefaultPortAllocator()

	// Allocate port
	port, err := pa.AllocatePort("test-service", safeTestPort(0))
	require.NoError(t, err)

	// Release it
	err = pa.ReleasePort(port)
	assert.NoError(t, err)

	// Verify it's released
	_, exists := pa.GetAllocation(port)
	assert.False(t, exists)

	_, exists = pa.GetPortForService("test-service")
	assert.False(t, exists)
}

func TestReleasePort_NotAllocated(t *testing.T) {
	pa := NewDefaultPortAllocator()

	err := pa.ReleasePort(safeTestPort(0))
	assert.ErrorIs(t, err, ErrPortNotAllocated)
}

func TestReleaseServicePort(t *testing.T) {
	pa := NewDefaultPortAllocator()

	// Allocate port
	port, err := pa.AllocatePort("test-service", safeTestPort(0))
	require.NoError(t, err)

	// Release by service name
	err = pa.ReleaseServicePort("test-service")
	assert.NoError(t, err)

	// Verify it's released
	_, exists := pa.GetAllocation(port)
	assert.False(t, exists)
}

func TestReleaseServicePort_NotAllocated(t *testing.T) {
	pa := NewDefaultPortAllocator()

	err := pa.ReleaseServicePort("non-existent")
	assert.ErrorIs(t, err, ErrPortNotAllocated)
}

func TestIsPortAvailable(t *testing.T) {
	pa := NewDefaultPortAllocator()

	// Allocate a port (may be safeTestPort(0) or fallback to api range)
	port, err := pa.AllocatePort("test", safeTestPort(0))
	require.NoError(t, err)
	assert.NotEqual(t, 0, port, "Should allocate a valid port")

	// The allocated port shouldn't be available anymore
	available := pa.IsPortAvailable(port)
	assert.False(t, available, "Allocated port should not be available")

	// Release it
	err = pa.ReleasePort(port)
	require.NoError(t, err)

	// Now it should be available again
	available = pa.IsPortAvailable(port)
	assert.True(t, available, "Released port should be available")
}

func TestGetPortForService(t *testing.T) {
	pa := NewDefaultPortAllocator()

	// No port allocated
	_, exists := pa.GetPortForService("test-service")
	assert.False(t, exists)

	// Allocate port
	expectedPort, err := pa.AllocatePort("test-service", safeTestPort(0))
	require.NoError(t, err)

	// Get port
	port, exists := pa.GetPortForService("test-service")
	assert.True(t, exists)
	assert.Equal(t, expectedPort, port)
}

func TestGetAllocation(t *testing.T) {
	pa := NewDefaultPortAllocator()

	// No allocation
	_, exists := pa.GetAllocation(safeTestPort(0))
	assert.False(t, exists)

	// Allocate port
	port, err := pa.AllocatePort("test-service", safeTestPort(0))
	require.NoError(t, err)

	// Get allocation
	allocation, exists := pa.GetAllocation(port)
	require.True(t, exists)
	assert.Equal(t, "test-service", allocation.ServiceName)
	assert.Equal(t, port, allocation.Port)
	assert.WithinDuration(t, time.Now(), allocation.AllocatedAt, 1*time.Second)
}

func TestListAllocations(t *testing.T) {
	pa := NewDefaultPortAllocator()

	// No allocations
	allocations := pa.ListAllocations()
	assert.Empty(t, allocations)

	// Allocate some ports
	services := []string{"service-1", "service-2", "service-3"}
	for _, svc := range services {
		_, err := pa.AllocatePort(svc, safeTestPort(0)+len(allocations))
		require.NoError(t, err)
		allocations = pa.ListAllocations()
	}

	// Check allocations
	allocations = pa.ListAllocations()
	assert.Len(t, allocations, 3)

	// Verify service names are present
	serviceNames := make(map[string]bool)
	for _, alloc := range allocations {
		serviceNames[alloc.ServiceName] = true
	}

	for _, svc := range services {
		assert.True(t, serviceNames[svc])
	}
}

func TestGetServiceType(t *testing.T) {
	pa := NewDefaultPortAllocator()

	tests := []struct {
		serviceName  string
		expectedType string
	}{
		{"postgres-primary", "database"},
		{"postgresql-replica", "database"},
		{"db-master", "database"},
		{"redis-cache", "cache"},
		{"memcache-server", "cache"},
		{"grpc-server", "grpc"},
		{"metrics-server", "metrics"},
		{"prometheus", "metrics"},
		{"websocket-server", "websocket"},
		{"ws-handler", "websocket"},
		{"api-server", "api"},
		{"unknown-service", "api"},
	}

	for _, tt := range tests {
		t.Run(tt.serviceName, func(t *testing.T) {
			serviceType := pa.getServiceType(tt.serviceName)
			assert.Equal(t, tt.expectedType, serviceType)
		})
	}
}

func TestConcurrentAllocations(t *testing.T) {
	pa := NewDefaultPortAllocator()

	const numGoroutines = 50
	basePort := testSafeBasePort(t, numGoroutines)

	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	errors := make(chan error, numGoroutines)
	ports := make(chan int, numGoroutines)

	// Allocate ports concurrently
	for i := 0; i < numGoroutines; i++ {
		go func(id int) {
			defer wg.Done()

			serviceName := fmt.Sprintf("service-%d", id)
			port, err := pa.AllocatePort(serviceName, basePort+id)

			if err != nil {
				errors <- err
			} else {
				ports <- port
			}
		}(i)
	}

	wg.Wait()
	close(errors)
	close(ports)

	// Check no errors
	for err := range errors {
		t.Errorf("Unexpected error: %v", err)
	}

	// Collect allocated ports
	allocatedPorts := make(map[int]bool)
	for port := range ports {
		assert.False(t, allocatedPorts[port], "Port %d allocated multiple times", port)
		allocatedPorts[port] = true
	}

	// Verify we got numGoroutines unique ports
	assert.Len(t, allocatedPorts, numGoroutines)
}

func TestConcurrentReleases(t *testing.T) {
	pa := NewDefaultPortAllocator()

	const numServices = 20
	releaseBase := testSafeBasePort(t, numServices)

	// Allocate ports first
	servicePorts := make(map[string]int)
	for i := 0; i < numServices; i++ {
		serviceName := fmt.Sprintf("service-%d", i)
		port, err := pa.AllocatePort(serviceName, releaseBase+i)
		require.NoError(t, err)
		servicePorts[serviceName] = port
	}

	// Release them concurrently
	var wg sync.WaitGroup
	wg.Add(numServices)

	for serviceName := range servicePorts {
		go func(svc string) {
			defer wg.Done()
			err := pa.ReleaseServicePort(svc)
			assert.NoError(t, err)
		}(serviceName)
	}

	wg.Wait()

	// Verify all released
	allocations := pa.ListAllocations()
	assert.Empty(t, allocations)
}

func TestPortReallocation(t *testing.T) {
	pa := NewDefaultPortAllocator()

	// Allocate port (may be preferred or fallback)
	preferredPort := testSafeBasePort(t, 1)
	port1, err := pa.AllocatePort("service-1", preferredPort)
	require.NoError(t, err)
	assert.NotEqual(t, 0, port1, "Should allocate a valid port")

	// Release it
	err = pa.ReleasePort(port1)
	require.NoError(t, err)

	// Allocate same preferred port for different service
	// Should get the same port back since it's now available
	port2, err := pa.AllocatePort("service-2", preferredPort)
	require.NoError(t, err)
	assert.Equal(t, port1, port2, "Should get same port after release")

	// Verify new service owns the port
	allocation, exists := pa.GetAllocation(port2)
	require.True(t, exists)
	assert.Equal(t, "service-2", allocation.ServiceName)
}
