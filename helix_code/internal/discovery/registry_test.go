package discovery

import (
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewServiceRegistry(t *testing.T) {
	config := DefaultRegistryConfig()
	registry := NewServiceRegistry(config)

	assert.NotNil(t, registry)
	assert.NotNil(t, registry.services)
	assert.Equal(t, config, registry.config)
}

func TestNewDefaultServiceRegistry(t *testing.T) {
	registry := NewDefaultServiceRegistry()

	assert.NotNil(t, registry)
	assert.Equal(t, 30*time.Second, registry.config.DefaultTTL)
	assert.Equal(t, 10*time.Second, registry.config.CleanupInterval)
	assert.True(t, registry.config.EnableHealthChecks)
}

func TestServiceInfo_Address(t *testing.T) {
	info := ServiceInfo{
		Host: "localhost",
		Port: 8080,
	}

	assert.Equal(t, "localhost:8080", info.Address())
}

func TestServiceInfo_IsExpired(t *testing.T) {
	tests := []struct {
		name     string
		service  ServiceInfo
		expected bool
	}{
		{
			name: "no TTL never expires",
			service: ServiceInfo{
				TTL:           0,
				LastHeartbeat: time.Now().Add(-1 * time.Hour),
			},
			expected: false,
		},
		{
			name: "within TTL not expired",
			service: ServiceInfo{
				TTL:           30 * time.Second,
				LastHeartbeat: time.Now().Add(-10 * time.Second),
			},
			expected: false,
		},
		{
			name: "beyond TTL expired",
			service: ServiceInfo{
				TTL:           30 * time.Second,
				LastHeartbeat: time.Now().Add(-60 * time.Second),
			},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, tt.service.IsExpired())
		})
	}
}

func TestRegister_Success(t *testing.T) {
	registry := NewDefaultServiceRegistry()

	info := ServiceInfo{
		Name:     "test-service",
		Host:     "localhost",
		Port:     8080,
		Protocol: "http",
		Version:  "1.0.0",
		Metadata: map[string]string{
			"env": "test",
		},
	}

	err := registry.Register(info)
	require.NoError(t, err)

	// Verify service is registered
	retrieved, err := registry.Get("test-service")
	require.NoError(t, err)
	assert.Equal(t, "test-service", retrieved.Name)
	assert.Equal(t, "localhost", retrieved.Host)
	assert.Equal(t, 8080, retrieved.Port)
	assert.Equal(t, "http", retrieved.Protocol)
	assert.True(t, retrieved.Healthy)
}

func TestRegister_AlreadyRegistered(t *testing.T) {
	registry := NewDefaultServiceRegistry()

	info := ServiceInfo{
		Name: "test-service",
		Host: "localhost",
		Port: 8080,
	}

	// Register first time
	err := registry.Register(info)
	require.NoError(t, err)

	// Try to register again
	err = registry.Register(info)
	assert.ErrorIs(t, err, ErrServiceAlreadyRegistered)
}

func TestRegister_InvalidServiceInfo(t *testing.T) {
	registry := NewDefaultServiceRegistry()

	tests := []struct {
		name    string
		service ServiceInfo
	}{
		{
			name: "empty name",
			service: ServiceInfo{
				Host: "localhost",
				Port: 8080,
			},
		},
		{
			name: "empty host",
			service: ServiceInfo{
				Name: "test",
				Port: 8080,
			},
		},
		{
			name: "invalid port too low",
			service: ServiceInfo{
				Name: "test",
				Host: "localhost",
				Port: 0,
			},
		},
		{
			name: "invalid port too high",
			service: ServiceInfo{
				Name: "test",
				Host: "localhost",
				Port: 70000,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := registry.Register(tt.service)
			assert.ErrorIs(t, err, ErrInvalidServiceInfo)
		})
	}
}

func TestRegister_DefaultProtocol(t *testing.T) {
	registry := NewDefaultServiceRegistry()

	info := ServiceInfo{
		Name: "test-service",
		Host: "localhost",
		Port: 8080,
		// Protocol not specified
	}

	err := registry.Register(info)
	require.NoError(t, err)

	retrieved, err := registry.Get("test-service")
	require.NoError(t, err)
	assert.Equal(t, "tcp", retrieved.Protocol)
}

func TestDeregister_Success(t *testing.T) {
	registry := NewDefaultServiceRegistry()

	info := ServiceInfo{
		Name: "test-service",
		Host: "localhost",
		Port: 8080,
	}

	// Register service
	err := registry.Register(info)
	require.NoError(t, err)

	// Deregister service
	err = registry.Deregister("test-service")
	assert.NoError(t, err)

	// Verify service is gone
	_, err = registry.Get("test-service")
	assert.ErrorIs(t, err, ErrServiceNotFound)
}

func TestDeregister_NotFound(t *testing.T) {
	registry := NewDefaultServiceRegistry()

	err := registry.Deregister("non-existent")
	assert.ErrorIs(t, err, ErrServiceNotFound)
}

func TestUpdate_Success(t *testing.T) {
	registry := NewDefaultServiceRegistry()

	// Register initial service
	info := ServiceInfo{
		Name:    "test-service",
		Host:    "localhost",
		Port:    8080,
		Version: "1.0.0",
	}
	err := registry.Register(info)
	require.NoError(t, err)

	// Update service
	updatedInfo := ServiceInfo{
		Name:    "test-service",
		Host:    "localhost",
		Port:    8081, // Changed port
		Version: "2.0.0",
	}
	err = registry.Update("test-service", updatedInfo)
	require.NoError(t, err)

	// Verify update
	retrieved, err := registry.Get("test-service")
	require.NoError(t, err)
	assert.Equal(t, 8081, retrieved.Port)
	assert.Equal(t, "2.0.0", retrieved.Version)
}

func TestUpdate_NotFound(t *testing.T) {
	registry := NewDefaultServiceRegistry()

	info := ServiceInfo{
		Name: "test-service",
		Host: "localhost",
		Port: 8080,
	}

	err := registry.Update("non-existent", info)
	assert.ErrorIs(t, err, ErrServiceNotFound)
}

func TestHeartbeat_Success(t *testing.T) {
	registry := NewDefaultServiceRegistry()

	info := ServiceInfo{
		Name: "test-service",
		Host: "localhost",
		Port: 8080,
	}
	err := registry.Register(info)
	require.NoError(t, err)

	// Get initial heartbeat time
	initial, err := registry.Get("test-service")
	require.NoError(t, err)
	initialTime := initial.LastHeartbeat

	// Wait a bit
	time.Sleep(10 * time.Millisecond)

	// Send heartbeat
	err = registry.Heartbeat("test-service")
	require.NoError(t, err)

	// Verify heartbeat updated
	updated, err := registry.Get("test-service")
	require.NoError(t, err)
	assert.True(t, updated.LastHeartbeat.After(initialTime))
}

func TestHeartbeat_NotFound(t *testing.T) {
	registry := NewDefaultServiceRegistry()

	err := registry.Heartbeat("non-existent")
	assert.ErrorIs(t, err, ErrServiceNotFound)
}

func TestGet_NotFound(t *testing.T) {
	registry := NewDefaultServiceRegistry()

	_, err := registry.Get("non-existent")
	assert.ErrorIs(t, err, ErrServiceNotFound)
}

func TestList_Empty(t *testing.T) {
	registry := NewDefaultServiceRegistry()

	services := registry.List()
	assert.Empty(t, services)
}

func TestList_MultipleServices(t *testing.T) {
	registry := NewDefaultServiceRegistry()

	// Register multiple services
	services := []string{"service-1", "service-2", "service-3"}
	for _, name := range services {
		info := ServiceInfo{
			Name: name,
			Host: "localhost",
			Port: 8080,
		}
		err := registry.Register(info)
		require.NoError(t, err)
	}

	// List all services
	list := registry.List()
	assert.Len(t, list, 3)

	// Verify all service names are present
	names := make(map[string]bool)
	for _, svc := range list {
		names[svc.Name] = true
	}

	for _, name := range services {
		assert.True(t, names[name])
	}
}

func TestListByProtocol(t *testing.T) {
	registry := NewDefaultServiceRegistry()

	// Register services with different protocols
	httpService := ServiceInfo{
		Name:     "http-service",
		Host:     "localhost",
		Port:     8080,
		Protocol: "http",
	}
	grpcService := ServiceInfo{
		Name:     "grpc-service",
		Host:     "localhost",
		Port:     9090,
		Protocol: "grpc",
	}
	anotherHTTP := ServiceInfo{
		Name:     "another-http",
		Host:     "localhost",
		Port:     8081,
		Protocol: "http",
	}

	require.NoError(t, registry.Register(httpService))
	require.NoError(t, registry.Register(grpcService))
	require.NoError(t, registry.Register(anotherHTTP))

	// List HTTP services
	httpServices := registry.ListByProtocol("http")
	assert.Len(t, httpServices, 2)

	// List gRPC services
	grpcServices := registry.ListByProtocol("grpc")
	assert.Len(t, grpcServices, 1)
	assert.Equal(t, "grpc-service", grpcServices[0].Name)
}

func TestListHealthy(t *testing.T) {
	registry := NewDefaultServiceRegistry()

	// Register healthy service
	healthyInfo := ServiceInfo{
		Name: "healthy-service",
		Host: "localhost",
		Port: 8080,
	}
	require.NoError(t, registry.Register(healthyInfo))

	// Register unhealthy service
	unhealthyInfo := ServiceInfo{
		Name: "unhealthy-service",
		Host: "localhost",
		Port: 8081,
	}
	require.NoError(t, registry.Register(unhealthyInfo))
	require.NoError(t, registry.UpdateHealth("unhealthy-service", false))

	// List only healthy services
	healthy := registry.ListHealthy()
	assert.Len(t, healthy, 1)
	assert.Equal(t, "healthy-service", healthy[0].Name)
}

func TestUpdateHealth(t *testing.T) {
	registry := NewDefaultServiceRegistry()

	info := ServiceInfo{
		Name: "test-service",
		Host: "localhost",
		Port: 8080,
	}
	require.NoError(t, registry.Register(info))

	// Service should start healthy
	retrieved, err := registry.Get("test-service")
	require.NoError(t, err)
	assert.True(t, retrieved.Healthy)

	// Mark as unhealthy
	err = registry.UpdateHealth("test-service", false)
	require.NoError(t, err)

	retrieved, err = registry.Get("test-service")
	require.NoError(t, err)
	assert.False(t, retrieved.Healthy)

	// Mark as healthy again
	err = registry.UpdateHealth("test-service", true)
	require.NoError(t, err)

	retrieved, err = registry.Get("test-service")
	require.NoError(t, err)
	assert.True(t, retrieved.Healthy)
}

func TestUpdateHealth_NotFound(t *testing.T) {
	registry := NewDefaultServiceRegistry()

	err := registry.UpdateHealth("non-existent", true)
	assert.ErrorIs(t, err, ErrServiceNotFound)
}

func TestCleanupExpired(t *testing.T) {
	config := DefaultRegistryConfig()
	config.CleanupInterval = 50 * time.Millisecond
	registry := NewServiceRegistry(config)

	// Register service with short TTL
	info := ServiceInfo{
		Name: "expiring-service",
		Host: "localhost",
		Port: 8080,
		TTL:  50 * time.Millisecond,
	}
	err := registry.Register(info)
	require.NoError(t, err)

	// Register service with no TTL (should never expire)
	persistentInfo := ServiceInfo{
		Name: "persistent-service",
		Host: "localhost",
		Port: 8081,
		TTL:  0, // No expiration
	}
	err = registry.Register(persistentInfo)
	require.NoError(t, err)

	// Start registry to enable cleanup
	registry.Start()
	defer registry.Stop()

	// Cleanup runs on a BACKGROUND goroutine (cleanupLoop). Sleeping 200ms and
	// hoping it was scheduled is what made this test fail under load; wait for
	// the OUTCOME instead. The assertion is the predicate, never the elapsed
	// time, so this is correct whether the loop runs instantly or is starved.
	require.Eventually(t, func() bool {
		_, gErr := registry.Get("expiring-service")
		return errors.Is(gErr, ErrServiceNotFound)
	}, 30*time.Second, 5*time.Millisecond,
		"expiring-service was never removed by cleanupLoop")

	// Persistent service should still exist
	_, err = registry.Get("persistent-service")
	assert.NoError(t, err)
}

func TestStartStop(t *testing.T) {
	registry := NewDefaultServiceRegistry()

	// Start should not block
	registry.Start()

	// No sleep needed: Start() does cleanupWg.Add(1) synchronously before each
	// `go`, and Stop() ends in cleanupWg.Wait() — the WaitGroup IS the signal.
	registry.Stop()
}

func TestConcurrentAccess(t *testing.T) {
	registry := NewDefaultServiceRegistry()

	const numGoroutines = 50

	// Concurrent registrations. Join them with a WaitGroup rather than sleeping
	// 100ms and hoping 50 goroutines were all scheduled; Register errors are
	// collected so a real failure reports itself instead of surfacing as a
	// confusing count mismatch.
	var wg sync.WaitGroup
	regErrs := make(chan error, numGoroutines)
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			info := ServiceInfo{
				Name: fmt.Sprintf("service-%d", id),
				Host: "localhost",
				Port: 8080 + id,
			}
			if err := registry.Register(info); err != nil {
				regErrs <- err
			}
		}(i)
	}
	wg.Wait()
	close(regErrs)
	for err := range regErrs {
		require.NoError(t, err)
	}

	// Verify all services registered
	services := registry.List()
	assert.Equal(t, numGoroutines, len(services))
}
