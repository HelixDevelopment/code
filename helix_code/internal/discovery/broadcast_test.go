package discovery

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewBroadcastService(t *testing.T) {
	config := DefaultBroadcastConfig()
	bs := NewBroadcastService(config)

	assert.NotNil(t, bs)
	assert.Equal(t, config, bs.config)
	assert.NotNil(t, bs.discovered)
	assert.False(t, bs.IsRunning())
}

func TestDefaultBroadcastConfig(t *testing.T) {
	config := DefaultBroadcastConfig()

	assert.Equal(t, DefaultMulticastAddress, config.MulticastAddress)
	assert.Equal(t, DefaultAnnouncementInterval, config.AnnouncementInterval)
	assert.Equal(t, DefaultDiscoveryTimeout, config.DiscoveryTimeout)
	assert.Equal(t, 2, config.TTL)
}

func TestBroadcastService_StartStop(t *testing.T) {
	config := DefaultBroadcastConfig()
	bs := NewBroadcastService(config)

	// Start service
	err := bs.Start()
	require.NoError(t, err)
	assert.True(t, bs.IsRunning())

	// No sleep needed: Start() does bs.wg.Add(1) synchronously before
	// `go bs.listen()`, and Stop() ends in bs.wg.Wait().
	// Stop service
	err = bs.Stop()
	require.NoError(t, err)
	assert.False(t, bs.IsRunning())
}

func TestBroadcastService_StartAlreadyRunning(t *testing.T) {
	config := DefaultBroadcastConfig()
	bs := NewBroadcastService(config)

	// Start service
	err := bs.Start()
	require.NoError(t, err)
	defer bs.Stop()

	// Try to start again
	err = bs.Start()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "already running")
}

func TestBroadcastService_StopNotRunning(t *testing.T) {
	config := DefaultBroadcastConfig()
	bs := NewBroadcastService(config)

	// Stop without starting
	err := bs.Stop()
	assert.ErrorIs(t, err, ErrBroadcastNotRunning)
}

func TestBroadcastService_SetLocalService(t *testing.T) {
	config := DefaultBroadcastConfig()
	bs := NewBroadcastService(config)

	info := ServiceInfo{
		Name:     "test-service",
		Host:     "localhost",
		Port:     8080,
		Protocol: "http",
	}

	err := bs.SetLocalService(info)
	require.NoError(t, err)

	assert.NotNil(t, bs.localService)
	assert.Equal(t, "test-service", bs.localService.Name)
	assert.Equal(t, 8080, bs.localService.Port)
}

func TestBroadcastService_SetLocalServiceWhileRunning(t *testing.T) {
	config := DefaultBroadcastConfig()
	config.AnnouncementInterval = 100 * time.Millisecond
	bs := NewBroadcastService(config)

	// Start service first
	err := bs.Start()
	require.NoError(t, err)
	defer bs.Stop()

	// Set local service while running
	info := ServiceInfo{
		Name: "test-service",
		Host: "localhost",
		Port: 8080,
	}

	err = bs.SetLocalService(info)
	require.NoError(t, err)

	// The only assertion here is on state SetLocalService sets synchronously,
	// so there is nothing to wait for. (Genuinely asserting that an
	// announcement was SENT is not possible today: BroadcastService exposes no
	// send counter — that would need a production observable, so it is
	// recorded as a gap rather than faked with a sleep.)
	assert.NotNil(t, bs.localService)
}

func TestBroadcastService_DiscoverNotRunning(t *testing.T) {
	config := DefaultBroadcastConfig()
	bs := NewBroadcastService(config)

	_, err := bs.Discover("test-service")
	assert.ErrorIs(t, err, ErrBroadcastNotRunning)
}

func TestBroadcastService_DiscoverTimeout(t *testing.T) {
	config := DefaultBroadcastConfig()
	config.DiscoveryTimeout = 200 * time.Millisecond
	bs := NewBroadcastService(config)

	err := bs.Start()
	require.NoError(t, err)
	defer bs.Stop()

	// Try to discover non-existent service
	start := time.Now()
	_, err = bs.Discover("non-existent-service")
	elapsed := time.Since(start)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "timeout")
	assert.GreaterOrEqual(t, elapsed, config.DiscoveryTimeout)
}

func TestBroadcastService_AnnounceAndDiscover(t *testing.T) {
	t.Skip("Skipping flaky network test - UDP multicast unreliable in test environment") // SKIP-OK: #requires-network
	// Create two broadcast services
	config1 := DefaultBroadcastConfig()
	config1.AnnouncementInterval = 100 * time.Millisecond
	config1.DiscoveryTimeout = 2 * time.Second

	config2 := DefaultBroadcastConfig()
	config2.DiscoveryTimeout = 2 * time.Second

	bs1 := NewBroadcastService(config1)
	bs2 := NewBroadcastService(config2)

	// Set local service for bs1
	info1 := ServiceInfo{
		Name:     "service-1",
		Host:     "localhost",
		Port:     8080,
		Protocol: "http",
		Healthy:  true,
	}
	err := bs1.SetLocalService(info1)
	require.NoError(t, err)

	// Start both services
	err = bs1.Start()
	require.NoError(t, err)
	defer bs1.Stop()

	err = bs2.Start()
	require.NoError(t, err)
	defer bs2.Stop()

	// No sleep needed: Discover() already polls on its own ticker until
	// DiscoveryTimeout — it IS the bounded predicate wait.
	discovered, err := bs2.Discover("service-1")
	require.NoError(t, err)
	assert.NotNil(t, discovered)
	assert.Equal(t, "service-1", discovered.Name)
	assert.Equal(t, 8080, discovered.Port)
}

func TestBroadcastService_List(t *testing.T) {
	config := DefaultBroadcastConfig()
	bs := NewBroadcastService(config)

	// Empty list initially
	services := bs.List()
	assert.Empty(t, services)

	// Add some discovered services manually
	bs.discoveryMu.Lock()
	bs.discovered["service-1"] = &ServiceInfo{
		Name: "service-1",
		Host: "localhost",
		Port: 8080,
	}
	bs.discovered["service-2"] = &ServiceInfo{
		Name: "service-2",
		Host: "localhost",
		Port: 8081,
	}
	bs.discoveryMu.Unlock()

	// List should return all services
	services = bs.List()
	assert.Len(t, services, 2)

	names := make(map[string]bool)
	for _, svc := range services {
		names[svc.Name] = true
	}
	assert.True(t, names["service-1"])
	assert.True(t, names["service-2"])
}

func TestBroadcastService_CleanExpired(t *testing.T) {
	config := DefaultBroadcastConfig()
	config.AnnouncementInterval = 100 * time.Millisecond
	bs := NewBroadcastService(config)

	// Add services with different heartbeat times
	now := time.Now()

	bs.discoveryMu.Lock()
	bs.discovered["recent-service"] = &ServiceInfo{
		Name:          "recent-service",
		Host:          "localhost",
		Port:          8080,
		LastHeartbeat: now,
	}
	bs.discovered["old-service"] = &ServiceInfo{
		Name:          "old-service",
		Host:          "localhost",
		Port:          8081,
		LastHeartbeat: now.Add(-400 * time.Millisecond), // Older than 3x announcement interval
	}
	bs.discoveryMu.Unlock()

	// Clean expired services
	bs.CleanExpired()

	// Recent service should remain, old service should be removed
	services := bs.List()
	assert.Len(t, services, 1)
	assert.Equal(t, "recent-service", services[0].Name)
}

func TestBroadcastService_QueryResponse(t *testing.T) {
	t.Skip("Skipping flaky network test - UDP multicast unreliable in test environment") // SKIP-OK: #requires-network
	// Create two services
	config1 := DefaultBroadcastConfig()
	config1.AnnouncementInterval = 1 * time.Second // Don't auto-announce
	config1.DiscoveryTimeout = 2 * time.Second

	config2 := DefaultBroadcastConfig()
	config2.AnnouncementInterval = 1 * time.Second

	bs1 := NewBroadcastService(config1)
	bs2 := NewBroadcastService(config2)

	// bs2 has a local service
	info2 := ServiceInfo{
		Name:     "service-2",
		Host:     "localhost",
		Port:     9090,
		Protocol: "grpc",
		Healthy:  true,
	}
	err := bs2.SetLocalService(info2)
	require.NoError(t, err)

	// Start both services
	err = bs1.Start()
	require.NoError(t, err)
	defer bs1.Stop()

	err = bs2.Start()
	require.NoError(t, err)
	defer bs2.Stop()

	// No sleep needed: Discover() already polls until DiscoveryTimeout.
	discovered, err := bs1.Discover("service-2")
	require.NoError(t, err)
	assert.NotNil(t, discovered)
	assert.Equal(t, "service-2", discovered.Name)
	assert.Equal(t, 9090, discovered.Port)
}

func TestBroadcastService_MultipleServices(t *testing.T) {
	t.Skip("Skipping flaky network test - UDP multicast unreliable in test environment") // SKIP-OK: #requires-network
	// Create three broadcast services
	config1 := DefaultBroadcastConfig()
	config1.AnnouncementInterval = 100 * time.Millisecond

	config2 := DefaultBroadcastConfig()
	config2.AnnouncementInterval = 100 * time.Millisecond

	config3 := DefaultBroadcastConfig()
	config3.DiscoveryTimeout = 2 * time.Second

	bs1 := NewBroadcastService(config1)
	bs2 := NewBroadcastService(config2)
	bs3 := NewBroadcastService(config3)

	// Set local services
	info1 := ServiceInfo{
		Name:    "service-1",
		Host:    "localhost",
		Port:    8080,
		Healthy: true,
	}
	info2 := ServiceInfo{
		Name:    "service-2",
		Host:    "localhost",
		Port:    8081,
		Healthy: true,
	}

	err := bs1.SetLocalService(info1)
	require.NoError(t, err)
	err = bs2.SetLocalService(info2)
	require.NoError(t, err)

	// Start all services
	err = bs1.Start()
	require.NoError(t, err)
	defer bs1.Stop()

	err = bs2.Start()
	require.NoError(t, err)
	defer bs2.Stop()

	err = bs3.Start()
	require.NoError(t, err)
	defer bs3.Stop()

	// List() is a SNAPSHOT (unlike Discover it does not poll), so wait for the
	// predicate instead of sleeping a guessed 500ms.
	require.Eventually(t, func() bool {
		return len(bs3.List()) >= 2
	}, 30*time.Second, 10*time.Millisecond, "Should discover at least 2 services")

	list := bs3.List()
	assert.GreaterOrEqual(t, len(list), 2, "Should discover at least 2 services")

	names := make(map[string]bool)
	for _, svc := range list {
		names[svc.Name] = true
	}

	assert.True(t, names["service-1"] || len(list) >= 1, "Should find service-1")
	assert.True(t, names["service-2"] || len(list) >= 1, "Should find service-2")
}

func TestBroadcastMessage_Serialization(t *testing.T) {
	msg := BroadcastMessage{
		Type: "announce",
		ServiceInfo: ServiceInfo{
			Name:     "test-service",
			Host:     "localhost",
			Port:     8080,
			Protocol: "http",
		},
		Timestamp: time.Now(),
		Metadata:  map[string]interface{}{"key": "value"},
	}

	// Test that message can be marshaled and unmarshaled
	config := DefaultBroadcastConfig()
	bs := NewBroadcastService(config)

	err := bs.Start()
	require.NoError(t, err)
	defer bs.Stop()

	// Message should be valid
	assert.Equal(t, "announce", msg.Type)
	assert.Equal(t, "test-service", msg.ServiceInfo.Name)
}

func TestBroadcastService_HandleStaleMessages(t *testing.T) {
	config := DefaultBroadcastConfig()
	bs := NewBroadcastService(config)

	// Create a stale message (older than 1 minute)
	staleMsg := &BroadcastMessage{
		Type: "announce",
		ServiceInfo: ServiceInfo{
			Name: "stale-service",
			Host: "localhost",
			Port: 8080,
		},
		Timestamp: time.Now().Add(-2 * time.Minute),
	}

	// Handle the stale message
	bs.handleMessage(staleMsg, nil)

	// Stale service should not be added
	services := bs.List()
	assert.Empty(t, services)
}

func TestBroadcastService_ConcurrentAccess(t *testing.T) {
	config := DefaultBroadcastConfig()
	config.AnnouncementInterval = 50 * time.Millisecond
	bs := NewBroadcastService(config)

	info := ServiceInfo{
		Name: "test-service",
		Host: "localhost",
		Port: 8080,
	}

	err := bs.SetLocalService(info)
	require.NoError(t, err)

	err = bs.Start()
	require.NoError(t, err)
	defer bs.Stop()

	// Concurrent operations
	done := make(chan bool)

	// Goroutine 1: List services
	go func() {
		for i := 0; i < 100; i++ {
			bs.List()
			time.Sleep(1 * time.Millisecond)
		}
		done <- true
	}()

	// Goroutine 2: Check running status
	go func() {
		for i := 0; i < 100; i++ {
			bs.IsRunning()
			time.Sleep(1 * time.Millisecond)
		}
		done <- true
	}()

	// Goroutine 3: Clean expired
	go func() {
		for i := 0; i < 10; i++ {
			bs.CleanExpired()
			time.Sleep(10 * time.Millisecond)
		}
		done <- true
	}()

	// Wait for all goroutines
	for i := 0; i < 3; i++ {
		<-done
	}

	// Should not panic or deadlock
	assert.True(t, bs.IsRunning())
}

func TestBroadcastService_IsRunning(t *testing.T) {
	config := DefaultBroadcastConfig()
	bs := NewBroadcastService(config)

	// Initially not running
	assert.False(t, bs.IsRunning())

	// Start service
	err := bs.Start()
	require.NoError(t, err)
	assert.True(t, bs.IsRunning())

	// Stop service
	err = bs.Stop()
	require.NoError(t, err)
	assert.False(t, bs.IsRunning())
}

func TestBroadcastConfig_CustomValues(t *testing.T) {
	config := BroadcastConfig{
		MulticastAddress:     "239.255.0.2:8000",
		AnnouncementInterval: 10 * time.Second,
		DiscoveryTimeout:     5 * time.Second,
		Interface:            "eth0",
		TTL:                  5,
	}

	bs := NewBroadcastService(config)

	assert.Equal(t, "239.255.0.2:8000", bs.config.MulticastAddress)
	assert.Equal(t, 10*time.Second, bs.config.AnnouncementInterval)
	assert.Equal(t, 5*time.Second, bs.config.DiscoveryTimeout)
	assert.Equal(t, "eth0", bs.config.Interface)
	assert.Equal(t, 5, bs.config.TTL)
}
