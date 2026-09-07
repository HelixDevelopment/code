package discovery

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewHealthMonitor(t *testing.T) {
	config := DefaultHealthMonitorConfig()
	registry := NewDefaultServiceRegistry()

	hm := NewHealthMonitor(config, registry)

	assert.NotNil(t, hm)
	assert.False(t, hm.IsRunning())
	assert.Equal(t, config.CheckInterval, hm.config.CheckInterval)
}

func TestDefaultHealthMonitorConfig(t *testing.T) {
	config := DefaultHealthMonitorConfig()

	assert.Equal(t, 5*time.Second, config.CheckInterval)
	assert.Equal(t, 2*time.Second, config.CheckTimeout)
	assert.Equal(t, 3, config.UnhealthyThreshold)
	assert.Equal(t, 2, config.HealthyThreshold)
	assert.Equal(t, HealthCheckTCP, config.DefaultStrategy)
	assert.True(t, config.EnableAutoRemoval)
	assert.Equal(t, 5, config.RemovalThreshold)
}

func TestHealthMonitor_StartStop(t *testing.T) {
	config := DefaultHealthMonitorConfig()
	registry := NewDefaultServiceRegistry()
	hm := NewHealthMonitor(config, registry)

	// Start
	err := hm.Start()
	require.NoError(t, err)
	assert.True(t, hm.IsRunning())

	// No sleep needed: Start() does hm.wg.Add(1) synchronously before
	// `go hm.monitorLoop()`, and Stop() ends in hm.wg.Wait().
	// Stop
	err = hm.Stop()
	require.NoError(t, err)
	assert.False(t, hm.IsRunning())
}

func TestHealthMonitor_StartAlreadyRunning(t *testing.T) {
	config := DefaultHealthMonitorConfig()
	registry := NewDefaultServiceRegistry()
	hm := NewHealthMonitor(config, registry)

	err := hm.Start()
	require.NoError(t, err)
	defer hm.Stop()

	err = hm.Start()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "already running")
}

func TestHealthMonitor_StopNotRunning(t *testing.T) {
	config := DefaultHealthMonitorConfig()
	registry := NewDefaultServiceRegistry()
	hm := NewHealthMonitor(config, registry)

	err := hm.Stop()
	assert.ErrorIs(t, err, ErrHealthMonitorNotRunning)
}

func TestHealthMonitor_CheckServiceHealth_TCP(t *testing.T) {
	config := DefaultHealthMonitorConfig()
	registry := NewDefaultServiceRegistry()
	hm := NewHealthMonitor(config, registry)

	// Start a test server
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer listener.Close()

	port := listener.Addr().(*net.TCPAddr).Port

	// Register service
	info := ServiceInfo{
		Name: "test-service",
		Host: "127.0.0.1",
		Port: port,
	}
	err = registry.Register(info)
	require.NoError(t, err)

	// Warm-up dials: a freshly-compiled unsigned test binary on macOS can pay
	// a one-time first-execution security-scan (Gatekeeper/syspolicyd) penalty
	// on its FIRST loopback connect(s). Under `go test` (cold binary) that
	// penalty was consumed inside the health check's CheckTimeout,
	// deterministically producing Healthy==false even though the listener is
	// live (the same binary PASSes when pre-built/already-scanned). A single
	// warm-up dial is not always enough — the penalty can consume the whole
	// first dial — so we loop until we observe a genuinely fast connect
	// (< 50ms), which confirms the binary is warm before the timed health
	// check below runs against the listener's true (microsecond) latency.
	for i := 0; i < 10; i++ {
		start := time.Now()
		warm, derr := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), 2*time.Second)
		if derr == nil {
			_ = warm.Close()
			if time.Since(start) < 50*time.Millisecond {
				break // binary is warm; the host scan penalty is paid
			}
		}
	}

	// Check health
	result, err := hm.CheckServiceHealth("test-service")
	require.NoError(t, err)
	assert.True(t, result.Healthy)
	assert.Equal(t, "test-service", result.ServiceName)
}

func TestHealthMonitor_CheckServiceHealth_Unhealthy(t *testing.T) {
	config := DefaultHealthMonitorConfig()
	registry := NewDefaultServiceRegistry()
	hm := NewHealthMonitor(config, registry)

	// Register service on unreachable port
	info := ServiceInfo{
		Name: "unreachable-service",
		Host: "127.0.0.1",
		Port: 19999, // Unlikely to be in use
	}
	err := registry.Register(info)
	require.NoError(t, err)

	// Check health
	result, err := hm.CheckServiceHealth("unreachable-service")
	require.NoError(t, err)
	assert.False(t, result.Healthy)
	assert.NotNil(t, result.Error)
}

func TestHealthMonitor_RegisterCustomCheck(t *testing.T) {
	config := DefaultHealthMonitorConfig()
	registry := NewDefaultServiceRegistry()
	hm := NewHealthMonitor(config, registry)

	// Register custom check
	customCheckCalled := false
	hm.RegisterCustomCheck("custom-service", func(info *ServiceInfo) error {
		customCheckCalled = true
		return nil
	})

	// Register service
	info := ServiceInfo{
		Name: "custom-service",
		Host: "localhost",
		Port: 8080,
	}
	err := registry.Register(info)
	require.NoError(t, err)

	// Check health
	result, err := hm.CheckServiceHealth("custom-service")
	require.NoError(t, err)
	assert.True(t, result.Healthy)
	assert.True(t, customCheckCalled)
}

func TestHealthMonitor_CustomCheckFailure(t *testing.T) {
	config := DefaultHealthMonitorConfig()
	registry := NewDefaultServiceRegistry()
	hm := NewHealthMonitor(config, registry)

	// Register custom check that fails
	testErr := errors.New("custom check failed")
	hm.RegisterCustomCheck("failing-service", func(info *ServiceInfo) error {
		return testErr
	})

	// Register service
	info := ServiceInfo{
		Name: "failing-service",
		Host: "localhost",
		Port: 8080,
	}
	err := registry.Register(info)
	require.NoError(t, err)

	// Check health
	result, err := hm.CheckServiceHealth("failing-service")
	require.NoError(t, err)
	assert.False(t, result.Healthy)
	assert.Equal(t, testErr, result.Error)
}

func TestHealthMonitor_SetServiceStrategy(t *testing.T) {
	config := DefaultHealthMonitorConfig()
	registry := NewDefaultServiceRegistry()
	hm := NewHealthMonitor(config, registry)

	hm.SetServiceStrategy("test-service", HealthCheckHTTP)

	strategy := hm.getStrategy("test-service")
	assert.Equal(t, HealthCheckHTTP, strategy)
}

func TestHealthMonitor_GetLastResult(t *testing.T) {
	config := DefaultHealthMonitorConfig()
	registry := NewDefaultServiceRegistry()
	hm := NewHealthMonitor(config, registry)

	// Initially no result
	_, exists := hm.GetLastResult("test-service")
	assert.False(t, exists)

	// Store a result
	result := &HealthCheckResult{
		ServiceName: "test-service",
		Healthy:     true,
		Timestamp:   time.Now(),
		Latency:     10 * time.Millisecond,
	}

	hm.mu.Lock()
	hm.lastResults["test-service"] = result
	hm.mu.Unlock()

	// Retrieve result
	retrieved, exists := hm.GetLastResult("test-service")
	assert.True(t, exists)
	assert.Equal(t, "test-service", retrieved.ServiceName)
	assert.True(t, retrieved.Healthy)
}

func TestHealthMonitor_GetAllResults(t *testing.T) {
	config := DefaultHealthMonitorConfig()
	registry := NewDefaultServiceRegistry()
	hm := NewHealthMonitor(config, registry)

	// Store multiple results
	results := []*HealthCheckResult{
		{ServiceName: "service-1", Healthy: true},
		{ServiceName: "service-2", Healthy: false},
		{ServiceName: "service-3", Healthy: true},
	}

	hm.mu.Lock()
	for _, r := range results {
		hm.lastResults[r.ServiceName] = r
	}
	hm.mu.Unlock()

	// Retrieve all
	allResults := hm.GetAllResults()
	assert.Len(t, allResults, 3)
	assert.Contains(t, allResults, "service-1")
	assert.Contains(t, allResults, "service-2")
	assert.Contains(t, allResults, "service-3")
}

func TestHealthMonitor_FailureAndSuccessCounts(t *testing.T) {
	config := DefaultHealthMonitorConfig()
	registry := NewDefaultServiceRegistry()
	hm := NewHealthMonitor(config, registry)

	serviceName := "test-service"

	// Initially zero
	assert.Equal(t, 0, hm.GetFailureCount(serviceName))
	assert.Equal(t, 0, hm.GetSuccessCount(serviceName))

	// Set counts
	hm.mu.Lock()
	hm.failureCounts[serviceName] = 3
	hm.successCounts[serviceName] = 2
	hm.mu.Unlock()

	assert.Equal(t, 3, hm.GetFailureCount(serviceName))
	assert.Equal(t, 2, hm.GetSuccessCount(serviceName))
}

func TestHealthMonitor_ResetCounts(t *testing.T) {
	config := DefaultHealthMonitorConfig()
	registry := NewDefaultServiceRegistry()
	hm := NewHealthMonitor(config, registry)

	serviceName := "test-service"

	// Set counts
	hm.mu.Lock()
	hm.failureCounts[serviceName] = 5
	hm.successCounts[serviceName] = 3
	hm.mu.Unlock()

	// Reset
	hm.ResetCounts(serviceName)

	assert.Equal(t, 0, hm.GetFailureCount(serviceName))
	assert.Equal(t, 0, hm.GetSuccessCount(serviceName))
}

func TestHealthMonitor_ProcessResult_Success(t *testing.T) {
	config := DefaultHealthMonitorConfig()
	config.HealthyThreshold = 2
	registry := NewDefaultServiceRegistry()
	hm := NewHealthMonitor(config, registry)

	// Register service
	info := ServiceInfo{
		Name: "test-service",
		Host: "localhost",
		Port: 8080,
	}
	err := registry.Register(info)
	require.NoError(t, err)

	// Process successful results
	for i := 0; i < 2; i++ {
		result := &HealthCheckResult{
			ServiceName: "test-service",
			Healthy:     true,
		}
		hm.processResult(result)
	}

	// Service should be marked healthy
	service, err := registry.Get("test-service")
	require.NoError(t, err)
	assert.True(t, service.Healthy)
}

func TestHealthMonitor_ProcessResult_Failure(t *testing.T) {
	config := DefaultHealthMonitorConfig()
	config.UnhealthyThreshold = 2
	registry := NewDefaultServiceRegistry()
	hm := NewHealthMonitor(config, registry)

	// Register service
	info := ServiceInfo{
		Name: "test-service",
		Host: "localhost",
		Port: 8080,
	}
	err := registry.Register(info)
	require.NoError(t, err)

	// Process failed results
	for i := 0; i < 2; i++ {
		result := &HealthCheckResult{
			ServiceName: "test-service",
			Healthy:     false,
			Error:       errors.New("check failed"),
		}
		hm.processResult(result)
	}

	// Service should be marked unhealthy
	service, err := registry.Get("test-service")
	require.NoError(t, err)
	assert.False(t, service.Healthy)
}

func TestHealthMonitor_AutoRemoval(t *testing.T) {
	config := DefaultHealthMonitorConfig()
	config.EnableAutoRemoval = true
	config.RemovalThreshold = 3
	registry := NewDefaultServiceRegistry()
	hm := NewHealthMonitor(config, registry)

	// Register service
	info := ServiceInfo{
		Name: "failing-service",
		Host: "localhost",
		Port: 8080,
	}
	err := registry.Register(info)
	require.NoError(t, err)

	// Process enough failures to trigger removal
	for i := 0; i < 3; i++ {
		result := &HealthCheckResult{
			ServiceName: "failing-service",
			Healthy:     false,
			Error:       errors.New("check failed"),
		}
		hm.processResult(result)
	}

	// Service should be removed
	_, err = registry.Get("failing-service")
	assert.ErrorIs(t, err, ErrServiceNotFound)
}

func TestHealthMonitor_GetHealthyServices(t *testing.T) {
	config := DefaultHealthMonitorConfig()
	registry := NewDefaultServiceRegistry()
	hm := NewHealthMonitor(config, registry)

	// Register services
	healthyInfo := ServiceInfo{
		Name: "healthy-service",
		Host: "localhost",
		Port: 8080,
	}
	unhealthyInfo := ServiceInfo{
		Name: "unhealthy-service",
		Host: "localhost",
		Port: 8081,
	}

	require.NoError(t, registry.Register(healthyInfo))
	require.NoError(t, registry.Register(unhealthyInfo))
	require.NoError(t, registry.UpdateHealth("unhealthy-service", false))

	// Get healthy services
	healthy := hm.GetHealthyServices()
	assert.Len(t, healthy, 1)
	assert.Equal(t, "healthy-service", healthy[0].Name)
}

func TestHealthMonitor_GetUnhealthyServices(t *testing.T) {
	config := DefaultHealthMonitorConfig()
	registry := NewDefaultServiceRegistry()
	hm := NewHealthMonitor(config, registry)

	// Register services
	healthyInfo := ServiceInfo{
		Name: "healthy-service",
		Host: "localhost",
		Port: 8080,
	}
	unhealthyInfo := ServiceInfo{
		Name: "unhealthy-service",
		Host: "localhost",
		Port: 8081,
	}

	require.NoError(t, registry.Register(healthyInfo))
	require.NoError(t, registry.Register(unhealthyInfo))
	require.NoError(t, registry.UpdateHealth("unhealthy-service", false))

	// Get unhealthy services
	unhealthy := hm.GetUnhealthyServices()
	assert.Len(t, unhealthy, 1)
	assert.Equal(t, "unhealthy-service", unhealthy[0].Name)
}

func TestHealthMonitor_ConcurrentAccess(t *testing.T) {
	config := DefaultHealthMonitorConfig()
	config.CheckInterval = 50 * time.Millisecond
	registry := NewDefaultServiceRegistry()
	hm := NewHealthMonitor(config, registry)

	// Register service with listener
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer listener.Close()

	port := listener.Addr().(*net.TCPAddr).Port

	info := ServiceInfo{
		Name: "concurrent-service",
		Host: "127.0.0.1",
		Port: port,
	}
	err = registry.Register(info)
	require.NoError(t, err)

	// Start monitor
	err = hm.Start()
	require.NoError(t, err)
	defer hm.Stop()

	done := make(chan bool)

	// Concurrent readers
	for i := 0; i < 10; i++ {
		go func() {
			for j := 0; j < 50; j++ {
				hm.GetLastResult("concurrent-service")
				hm.GetAllResults()
				hm.GetFailureCount("concurrent-service")
				hm.GetSuccessCount("concurrent-service")
				hm.GetHealthyServices()
				time.Sleep(1 * time.Millisecond)
			}
			done <- true
		}()
	}

	// Wait for goroutines
	for i := 0; i < 10; i++ {
		<-done
	}

	// Should not panic or deadlock
	assert.True(t, hm.IsRunning())
}

func TestHealthMonitor_MonitorLoop(t *testing.T) {
	config := DefaultHealthMonitorConfig()
	config.CheckInterval = 100 * time.Millisecond
	config.HealthyThreshold = 2
	registry := NewDefaultServiceRegistry()
	hm := NewHealthMonitor(config, registry)

	// Start test server
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer listener.Close()

	port := listener.Addr().(*net.TCPAddr).Port

	// Register service
	info := ServiceInfo{
		Name: "monitored-service",
		Host: "127.0.0.1",
		Port: port,
	}
	err = registry.Register(info)
	require.NoError(t, err)

	// Start monitoring
	err = hm.Start()
	require.NoError(t, err)

	// Wait for the monitor to actually COMPLETE its checks rather than sleeping
	// 300ms and hoping. GetSuccessCount is the observable the monitorLoop
	// increments, so this asserts the predicate the rest of the test depends
	// on. (Without it, `assert.True(t, service.Healthy)` below is a bluff:
	// Register sets Healthy=true, so it passes even if zero checks ran.)
	require.Eventually(t, func() bool {
		return hm.GetSuccessCount("monitored-service") >= config.HealthyThreshold
	}, 30*time.Second, 5*time.Millisecond,
		"health monitor never completed HealthyThreshold successful checks")

	// Stop monitoring
	err = hm.Stop()
	require.NoError(t, err)

	// Check that results were recorded
	result, exists := hm.GetLastResult("monitored-service")
	assert.True(t, exists)
	assert.NotNil(t, result)

	// Service should be healthy
	service, err := registry.Get("monitored-service")
	require.NoError(t, err)
	assert.True(t, service.Healthy)
}

func TestHealthMonitor_MultipleServices(t *testing.T) {
	config := DefaultHealthMonitorConfig()
	registry := NewDefaultServiceRegistry()
	hm := NewHealthMonitor(config, registry)

	// Start multiple test servers
	listeners := make([]net.Listener, 3)
	for i := 0; i < 3; i++ {
		listener, err := net.Listen("tcp", "127.0.0.1:0")
		require.NoError(t, err)
		defer listener.Close()
		listeners[i] = listener

		port := listener.Addr().(*net.TCPAddr).Port
		info := ServiceInfo{
			Name: fmt.Sprintf("service-%d", i),
			Host: "127.0.0.1",
			Port: port,
		}
		err = registry.Register(info)
		require.NoError(t, err)
	}

	// Check all services and manually store results
	for i := 0; i < 3; i++ {
		result, err := hm.CheckServiceHealth(fmt.Sprintf("service-%d", i))
		require.NoError(t, err)
		assert.True(t, result.Healthy)

		// Manually store result (since CheckServiceHealth doesn't auto-store)
		hm.mu.Lock()
		hm.lastResults[result.ServiceName] = result
		hm.mu.Unlock()
	}

	// All results should be stored
	allResults := hm.GetAllResults()
	assert.Len(t, allResults, 3)
}

func TestHealthMonitor_CheckHTTP_Success(t *testing.T) {
	// Create a test HTTP server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" {
			w.WriteHeader(http.StatusOK)
			w.Write([]byte("OK"))
		} else {
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	// Parse server address to get host and port
	serverURL, err := url.Parse(server.URL)
	require.NoError(t, err)
	host := serverURL.Hostname()
	port, err := strconv.Atoi(serverURL.Port())
	require.NoError(t, err)

	// Create health monitor
	config := DefaultHealthMonitorConfig()
	config.CheckTimeout = 2 * time.Second
	hm := NewHealthMonitor(config, nil)

	// Create service info
	serviceInfo := &ServiceInfo{
		Name:     "http-service",
		Host:     host,
		Port:     port,
		Protocol: "http",
	}

	// Test checkHTTP directly
	err = hm.checkHTTP(serviceInfo)
	assert.NoError(t, err)
}

func TestHealthMonitor_CheckHTTP_Failure(t *testing.T) {
	// Create health monitor
	config := DefaultHealthMonitorConfig()
	config.CheckTimeout = 1 * time.Second
	hm := NewHealthMonitor(config, nil)

	t.Run("HTTP_StatusNotOK", func(t *testing.T) {
		// Create a server that returns non-200 status
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		}))
		defer server.Close()

		serverURL, err := url.Parse(server.URL)
		require.NoError(t, err)
		host := serverURL.Hostname()
		port, err := strconv.Atoi(serverURL.Port())
		require.NoError(t, err)

		serviceInfo := &ServiceInfo{
			Name: "failing-service",
			Host: host,
			Port: port,
		}

		err = hm.checkHTTP(serviceInfo)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "health check failed")
	})

	t.Run("HTTP_ConnectionRefused", func(t *testing.T) {
		// Use a port that's not listening
		serviceInfo := &ServiceInfo{
			Name: "unreachable-service",
			Host: "127.0.0.1",
			Port: 59999, // Unlikely to be in use
		}

		err := hm.checkHTTP(serviceInfo)
		assert.Error(t, err)
	})

	t.Run("HTTP_Timeout", func(t *testing.T) {
		// Create a server that delays response
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			time.Sleep(3 * time.Second) // Longer than health check timeout
			w.WriteHeader(http.StatusOK)
		}))
		defer server.Close()

		serverURL, err := url.Parse(server.URL)
		require.NoError(t, err)
		host := serverURL.Hostname()
		port, err := strconv.Atoi(serverURL.Port())
		require.NoError(t, err)

		serviceInfo := &ServiceInfo{
			Name: "slow-service",
			Host: host,
			Port: port,
		}

		err = hm.checkHTTP(serviceInfo)
		assert.Error(t, err)
	})
}

func TestHealthMonitor_CheckService_HTTPStrategy(t *testing.T) {
	// Create a test HTTP server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" {
			w.WriteHeader(http.StatusOK)
		}
	}))
	defer server.Close()

	serverURL, err := url.Parse(server.URL)
	require.NoError(t, err)
	host := serverURL.Hostname()
	port, err := strconv.Atoi(serverURL.Port())
	require.NoError(t, err)

	// Create registry and register service
	registry := NewServiceRegistry(DefaultRegistryConfig())
	serviceInfo := ServiceInfo{
		Name:     "http-service",
		Host:     host,
		Port:     port,
		Protocol: "http",
		Healthy:  true,
	}
	err = registry.Register(serviceInfo)
	require.NoError(t, err)

	// Create health monitor
	config := DefaultHealthMonitorConfig()
	hm := NewHealthMonitor(config, registry)

	// Set HTTP strategy for this service
	hm.SetServiceStrategy("http-service", "http")

	// Check service health (will use HTTP strategy)
	result, err := hm.CheckServiceHealth("http-service")
	require.NoError(t, err)
	assert.True(t, result.Healthy)
	assert.Equal(t, "http-service", result.ServiceName)
}
