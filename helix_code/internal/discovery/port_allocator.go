package discovery

import (
	"errors"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Fallback-allocation constants.
//
// The allocator hands out port NUMBERS that are published to other services
// through the registry (see DiscoveryClient.Register); nothing in that path
// holds a listening socket on the number it advertises. A number drawn from
// the OS ephemeral range is therefore unsafe: the kernel may hand that same
// port to any outbound connection at any moment, so an advertised "stable"
// service address can silently belong to something else. The fallback block
// below is deliberately placed OUTSIDE the OS ephemeral range, which is the
// same reservation strategy hashicorp/consul's sdk/freeport uses for exactly
// this reason.
const (
	// procIPLocalPortRange is the Linux sysctl exposing the kernel's ephemeral
	// port range. It is READ AT RUNTIME rather than hardcoded because the range
	// is administrator-configurable; baking in a literal 32768-60999 would be
	// correct only on a default-configured host.
	procIPLocalPortRange = "/proc/sys/net/ipv4/ip_local_port_range"

	// fallbackBlockSize is how many consecutive ports the fallback block spans.
	fallbackBlockSize = 1024

	// lowestFallbackPort keeps the fallback block clear of the privileged
	// (<1024) range, which an unprivileged process cannot bind anyway.
	lowestFallbackPort = 1024

	// maxPort is the highest legal TCP port number.
	maxPort = 65535

	// assumedEphemeralLow / assumedEphemeralHigh are used when the OS range
	// cannot be read (non-Linux hosts, procfs unavailable). They describe the
	// WIDEST plausible ephemeral window — the union of the common defaults
	// (Linux 32768-60999, macOS/BSD/Windows 49152-65535) — so that a block
	// computed against them cannot accidentally land inside the real range on
	// any of those platforms.
	assumedEphemeralLow  = 32768
	assumedEphemeralHigh = maxPort
)

var (
	// ErrNoPortsAvailable is returned when no ports are available in the configured range
	ErrNoPortsAvailable = errors.New("no ports available in configured range")

	// ErrInvalidPortRange is returned when the port range is invalid
	ErrInvalidPortRange = errors.New("invalid port range")

	// ErrPortAlreadyAllocated is returned when trying to allocate an already allocated port
	ErrPortAlreadyAllocated = errors.New("port already allocated")

	// ErrPortNotAllocated is returned when trying to release a port that wasn't allocated
	ErrPortNotAllocated = errors.New("port not allocated")
)

// PortRange defines a range of ports for a service type
type PortRange struct {
	Start int
	End   int
}

// PortAllocation represents an allocated port
type PortAllocation struct {
	Port        int
	ServiceName string
	AllocatedAt time.Time
}

// PortAllocatorConfig configures the port allocator
type PortAllocatorConfig struct {
	// AllowEphemeral enables the last-resort fallback block when every
	// service-type range is exhausted.
	//
	// NOTE ON THE NAME: the flag is historical. It does NOT (any longer) mean
	// "hand out a port from the OS ephemeral range" — doing so was a defect,
	// because the allocated number is advertised through the registry while
	// nothing holds a socket on it, so the kernel could reassign it to an
	// outbound connection at any time. It now means "allocate from the
	// reserved fallback block", which lies outside the OS ephemeral range.
	// See FallbackPortRange and fallbackPortRange().
	AllowEphemeral bool

	// PortRanges defines port ranges for different service types
	PortRanges map[string]PortRange

	// ReservedPorts are ports that should never be allocated
	ReservedPorts []int

	// FallbackPortRange overrides the fallback block used when AllowEphemeral
	// is set and every service-type range is exhausted. When nil, the block is
	// computed at runtime by fallbackPortRange() from the live OS ephemeral
	// range. Callers that set it are responsible for keeping it outside the
	// host's ephemeral range.
	FallbackPortRange *PortRange
}

// DefaultPortAllocatorConfig returns default configuration
func DefaultPortAllocatorConfig() PortAllocatorConfig {
	return PortAllocatorConfig{
		AllowEphemeral: false,
		PortRanges: map[string]PortRange{
			"database":  {Start: 5433, End: 5442},
			"cache":     {Start: 6380, End: 6389},
			"api":       {Start: 8081, End: 8099},
			"grpc":      {Start: 9091, End: 9109},
			"metrics":   {Start: 9100, End: 9199},
			"websocket": {Start: 8001, End: 8020},
		},
		ReservedPorts: []int{22, 80, 443, 3306, 5432, 6379, 8080, 9090},
	}
}

// PortAllocator manages port allocation with fallback mechanisms
type PortAllocator struct {
	config      PortAllocatorConfig
	allocations map[int]*PortAllocation
	serviceMap  map[string]int
	mu          sync.RWMutex
}

// NewPortAllocator creates a new port allocator
func NewPortAllocator(config PortAllocatorConfig) *PortAllocator {
	return &PortAllocator{
		config:      config,
		allocations: make(map[int]*PortAllocation),
		serviceMap:  make(map[string]int),
	}
}

// NewDefaultPortAllocator creates a port allocator with default configuration
func NewDefaultPortAllocator() *PortAllocator {
	return NewPortAllocator(DefaultPortAllocatorConfig())
}

// AllocatePort allocates a port for a service, preferring the specified port
// If the preferred port is unavailable, it falls back to the range for the service type
func (pa *PortAllocator) AllocatePort(serviceName string, preferredPort int) (int, error) {
	pa.mu.Lock()
	defer pa.mu.Unlock()

	// Check if service already has a port
	if existingPort, exists := pa.serviceMap[serviceName]; exists {
		return existingPort, nil
	}

	// Try preferred port first (if valid, not reserved, and available).
	//
	// preferredPort > 0 IS LOAD-BEARING, not defensive noise. Port 0 is the
	// kernel's "choose one for me" sentinel, so isPortAvailableUnsafe(0) binds
	// ":0", ALWAYS succeeds, and returns true — which would make the allocator
	// reserve literal port 0 and publish it into the service registry as a
	// connection target no client can ever reach. Reachable today via
	// DiscoveryClient.getDefaultPort: it returns c.config.DefaultPorts[name]
	// whenever the key EXISTS, so an operator config carrying 0 for a service
	// yields exists=true, port=0. Ports above maxPort are rejected for the
	// mirror-image reason.
	if preferredPort > 0 && preferredPort <= maxPort &&
		!pa.isReserved(preferredPort) && pa.isPortAvailableUnsafe(preferredPort) {
		return pa.reservePortUnsafe(preferredPort, serviceName)
	}

	// Get service type from service name
	serviceType := pa.getServiceType(serviceName)

	// Try fallback range
	portRange, exists := pa.config.PortRanges[serviceType]
	if exists {
		port, err := pa.allocateFromRangeUnsafe(serviceName, portRange)
		if err == nil {
			return port, nil
		}
	}

	// Try the reserved fallback block if allowed
	if pa.config.AllowEphemeral {
		return pa.allocateFallbackPortUnsafe(serviceName)
	}

	return 0, ErrNoPortsAvailable
}

// AllocatePortInRange allocates a port within a specific range
func (pa *PortAllocator) AllocatePortInRange(serviceName string, startPort, endPort int) (int, error) {
	pa.mu.Lock()
	defer pa.mu.Unlock()

	if startPort < 1 || endPort > 65535 || startPort > endPort {
		return 0, ErrInvalidPortRange
	}

	// Check if service already has a port
	if existingPort, exists := pa.serviceMap[serviceName]; exists {
		return existingPort, nil
	}

	return pa.allocateFromRangeUnsafe(serviceName, PortRange{Start: startPort, End: endPort})
}

// ReleasePort releases a previously allocated port
func (pa *PortAllocator) ReleasePort(port int) error {
	pa.mu.Lock()
	defer pa.mu.Unlock()

	allocation, exists := pa.allocations[port]
	if !exists {
		return ErrPortNotAllocated
	}

	delete(pa.allocations, port)
	delete(pa.serviceMap, allocation.ServiceName)

	return nil
}

// ReleaseServicePort releases the port allocated to a service
func (pa *PortAllocator) ReleaseServicePort(serviceName string) error {
	pa.mu.Lock()
	defer pa.mu.Unlock()

	port, exists := pa.serviceMap[serviceName]
	if !exists {
		return ErrPortNotAllocated
	}

	delete(pa.allocations, port)
	delete(pa.serviceMap, serviceName)

	return nil
}

// IsPortAvailable checks if a port is available for binding
func (pa *PortAllocator) IsPortAvailable(port int) bool {
	pa.mu.RLock()
	defer pa.mu.RUnlock()

	return pa.isPortAvailableUnsafe(port)
}

// GetPortForService returns the port allocated to a service
func (pa *PortAllocator) GetPortForService(serviceName string) (int, bool) {
	pa.mu.RLock()
	defer pa.mu.RUnlock()

	port, exists := pa.serviceMap[serviceName]
	return port, exists
}

// GetAllocation returns allocation details for a port
func (pa *PortAllocator) GetAllocation(port int) (*PortAllocation, bool) {
	pa.mu.RLock()
	defer pa.mu.RUnlock()

	allocation, exists := pa.allocations[port]
	if !exists {
		return nil, false
	}

	// Return a copy to prevent modification
	allocationCopy := *allocation
	return &allocationCopy, true
}

// ListAllocations returns all current port allocations
func (pa *PortAllocator) ListAllocations() []*PortAllocation {
	pa.mu.RLock()
	defer pa.mu.RUnlock()

	allocations := make([]*PortAllocation, 0, len(pa.allocations))
	for _, allocation := range pa.allocations {
		allocationCopy := *allocation
		allocations = append(allocations, &allocationCopy)
	}

	return allocations
}

// Internal helper methods (must be called with lock held)

// isPortAvailableUnsafe reports whether a port LOOKS free right now.
//
// This is a HINT, NOT A RESERVATION. The probe binds a listener and immediately
// closes it, so between that close and the caller's real bind any other process
// on the host may take the port — a time-of-check/time-of-use race that no
// probe-then-close design can eliminate. The Go maintainers state it plainly
// (golang/go#24818): "The program is inherently racy. You can't close a listener
// and expect to be able to use that same port later." Callers MUST therefore
// still handle EADDRINUSE at their real bind and treat a positive result here as
// advisory only.
//
// The probe deliberately binds the WILDCARD address (":port"), not loopback.
// Ports allocated here are advertised through the service registry for REMOTE
// consumption, so a loopback-only probe would report a port free while it is in
// fact taken on the LAN interface that consumers actually connect to. Narrowing
// this to 127.0.0.1 would look like a tightening and is in fact a regression for
// this consumer.
func (pa *PortAllocator) isPortAvailableUnsafe(port int) bool {
	// Check if already allocated
	if _, exists := pa.allocations[port]; exists {
		return false
	}

	// Check if reserved
	if pa.isReserved(port) {
		return false
	}

	// Try to bind to the port
	listener, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
	if err != nil {
		return false
	}
	listener.Close()

	return true
}

func (pa *PortAllocator) reservePortUnsafe(port int, serviceName string) (int, error) {
	if _, exists := pa.allocations[port]; exists {
		return 0, ErrPortAlreadyAllocated
	}

	allocation := &PortAllocation{
		Port:        port,
		ServiceName: serviceName,
		AllocatedAt: time.Now(),
	}

	pa.allocations[port] = allocation
	pa.serviceMap[serviceName] = port

	return port, nil
}

func (pa *PortAllocator) allocateFromRangeUnsafe(serviceName string, portRange PortRange) (int, error) {
	for port := portRange.Start; port <= portRange.End; port++ {
		if pa.isPortAvailableUnsafe(port) {
			return pa.reservePortUnsafe(port, serviceName)
		}
	}

	return 0, ErrNoPortsAvailable
}

// allocateFallbackPortUnsafe allocates from the reserved fallback block — the
// last resort once every configured service-type range is exhausted.
//
// It deliberately does NOT ask the kernel for a port via net.Listen("tcp", ":0").
// That draws from ip_local_port_range, and the number produced is then published
// through the service registry as a connection target while nothing holds a
// socket on it, so the kernel is free to reassign it to any outbound connection.
// Instead the block from fallbackPortRange() (or the caller-supplied override)
// is scanned like any other range, so the returned number lives outside the OS
// ephemeral range and cannot be taken by the kernel's ephemeral allocator.
//
// Returns ErrNoPortsAvailable (possibly wrapped) when the fallback block is
// exhausted or cannot be placed outside the ephemeral range at all.
func (pa *PortAllocator) allocateFallbackPortUnsafe(serviceName string) (int, error) {
	fallback := pa.config.FallbackPortRange
	if fallback == nil {
		computed, err := fallbackPortRange()
		if err != nil {
			return 0, err
		}
		fallback = &computed
	}

	return pa.allocateFromRangeUnsafe(serviceName, *fallback)
}

// ephemeralPortRange reports the OS ephemeral port range, read at runtime.
//
// The range is administrator-configurable, so it is read from the kernel rather
// than hardcoded. When it cannot be read (non-Linux host, procfs unavailable,
// unparseable content) the conservative assumed window is returned instead —
// see assumedEphemeralLow / assumedEphemeralHigh.
func ephemeralPortRange() (int, int) {
	data, err := os.ReadFile(procIPLocalPortRange)
	if err != nil {
		return assumedEphemeralLow, assumedEphemeralHigh
	}

	fields := strings.Fields(string(data))
	if len(fields) != 2 {
		return assumedEphemeralLow, assumedEphemeralHigh
	}

	low, errLow := strconv.Atoi(fields[0])
	high, errHigh := strconv.Atoi(fields[1])
	if errLow != nil || errHigh != nil || low < 1 || high < low || high > maxPort {
		return assumedEphemeralLow, assumedEphemeralHigh
	}

	return low, high
}

// fallbackPortRange computes a fallback allocation block guaranteed to lie
// OUTSIDE the OS ephemeral range reported by ephemeralPortRange.
//
// Placement, in order of preference:
//
//   - immediately ABOVE the ephemeral range, when there is room below maxPort.
//     Preferred because the space below the ephemeral range is more crowded on
//     a typical host (for example a Kubernetes node's default NodePort window
//     is 30000-32767, directly beneath the Linux default ephemeral low bound).
//   - immediately BELOW the ephemeral range otherwise — the case when the range
//     runs to 65535, which is the common macOS/BSD/Windows configuration.
//
// If neither placement fits (an ephemeral range so wide that no non-privileged
// block remains outside it), ErrNoPortsAvailable is returned wrapped with the
// observed range, rather than silently falling back inside the ephemeral range.
func fallbackPortRange() (PortRange, error) {
	low, high := ephemeralPortRange()

	if high+fallbackBlockSize <= maxPort {
		return PortRange{Start: high + 1, End: high + fallbackBlockSize}, nil
	}

	if low-fallbackBlockSize >= lowestFallbackPort {
		return PortRange{Start: low - fallbackBlockSize, End: low - 1}, nil
	}

	return PortRange{}, fmt.Errorf(
		"no fallback block of %d ports fits outside the OS ephemeral range %d-%d: %w",
		fallbackBlockSize, low, high, ErrNoPortsAvailable)
}

func (pa *PortAllocator) isReserved(port int) bool {
	for _, reserved := range pa.config.ReservedPorts {
		if port == reserved {
			return true
		}
	}
	return false
}

func (pa *PortAllocator) getServiceType(serviceName string) string {
	// Simple heuristic to determine service type from name
	// This can be enhanced based on actual service naming conventions

	if contains(serviceName, "postgres", "pg", "database", "db") {
		return "database"
	}
	if contains(serviceName, "redis", "cache", "memcache") {
		return "cache"
	}
	if contains(serviceName, "grpc") {
		return "grpc"
	}
	if contains(serviceName, "metrics", "prometheus", "prom") {
		return "metrics"
	}
	if contains(serviceName, "websocket", "ws") {
		return "websocket"
	}

	// Default to api
	return "api"
}

func contains(s string, substrings ...string) bool {
	lower := s
	for _, sub := range substrings {
		if len(lower) >= len(sub) {
			for i := 0; i <= len(lower)-len(sub); i++ {
				match := true
				for j := 0; j < len(sub); j++ {
					if lower[i+j] != sub[j] && lower[i+j] != sub[j]-32 && lower[i+j] != sub[j]+32 {
						match = false
						break
					}
				}
				if match {
					return true
				}
			}
		}
	}
	return false
}
