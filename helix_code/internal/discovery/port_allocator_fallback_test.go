package discovery

import (
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// readEphemeralRangeForTest is an INDEPENDENT oracle for the kernel's ephemeral
// port range. It deliberately does not call the production ephemeralPortRange()
// helper: if the test read the range through the same code the fix uses, a bug
// in that helper would make the assertion agree with itself instead of catching
// it. Returns ok=false when the sysctl is unreadable or unparseable, so callers
// can SKIP-with-reason rather than assert against a guessed range.
func readEphemeralRangeForTest(t *testing.T) (low, high int, ok bool) {
	t.Helper()

	data, err := os.ReadFile("/proc/sys/net/ipv4/ip_local_port_range")
	if err != nil {
		return 0, 0, false
	}

	fields := strings.Fields(string(data))
	if len(fields) != 2 {
		return 0, 0, false
	}

	low, errLow := strconv.Atoi(fields[0])
	high, errHigh := strconv.Atoi(fields[1])
	if errLow != nil || errHigh != nil || low < 1 || high < low {
		return 0, 0, false
	}

	return low, high, true
}

// TestAllocateFallbackPort_OutsideEphemeralRange proves the fallback allocation
// path hands out a port NUMBER that the kernel's ephemeral allocator can never
// hand to an unrelated outbound connection.
//
// Why this matters: the allocated number is published through the service
// registry (DiscoveryClient.Register) as a connection target for other services,
// and nothing in that path holds a listening socket on it. A number drawn from
// ip_local_port_range would therefore be reassignable by the kernel at any
// moment, making the advertised address unstable.
//
// FALSIFIABILITY (§11.4.115 / §1.1) — the exact mutation that breaks this test:
// in port_allocator.go, replace the body of allocateFallbackPortUnsafe with the
// old ephemeral implementation
//
//	listener, _ := net.Listen("tcp", ":0")
//	port := listener.Addr().(*net.TCPAddr).Port
//	listener.Close()
//	return pa.reservePortUnsafe(port, serviceName)
//
// The kernel then draws the port from ip_local_port_range and the
// outside-the-range assertion below FAILS. (Executed and confirmed FAILing
// before this test was accepted.)
func TestAllocateFallbackPort_OutsideEphemeralRange(t *testing.T) {
	low, high, ok := readEphemeralRangeForTest(t)
	if !ok {
		// Honest SKIP-with-reason (§11.4.3): without the kernel's own range
		// there is no oracle to assert against. Never a silent pass.
		t.Skip("SKIP-OK: /proc/sys/net/ipv4/ip_local_port_range unreadable on this host; " +
			"no independent oracle for the ephemeral range")
	}

	const preferredPort = 80 // reserved below, so allocation must fall through

	// No service-type ranges at all, so AllocatePort is forced past the
	// preferred port and past the (absent) type range straight to the fallback.
	pa := NewPortAllocator(PortAllocatorConfig{
		AllowEphemeral: true,
		PortRanges:     map[string]PortRange{},
		ReservedPorts:  []int{preferredPort},
	})

	port, err := pa.AllocatePort("fallback-service", preferredPort)
	require.NoError(t, err, "fallback allocation must succeed on a host with free ports")

	assert.True(t, port < low || port > high,
		"allocated port %d is INSIDE the kernel ephemeral range %d-%d; the kernel may "+
			"reassign it to any outbound connection while it is advertised in the registry",
		port, low, high)
	assert.Greater(t, port, 0, "allocated port must be a real port number")
	assert.LessOrEqual(t, port, 65535, "allocated port must be a legal TCP port")

	// The allocation is real, not just a returned number: it is recorded and
	// resolvable through the allocator's own bookkeeping.
	recorded, exists := pa.GetPortForService("fallback-service")
	require.True(t, exists, "allocation must be recorded for the service")
	assert.Equal(t, port, recorded)
}

// TestFallbackPortRange_DoesNotOverlapEphemeralRange checks the block-placement
// helper directly against the kernel's own reported range.
//
// FALSIFIABILITY: in fallbackPortRange(), change the ABOVE-placement return to
// `PortRange{Start: high - fallbackBlockSize, End: high}` (i.e. land inside the
// ephemeral range) and the overlap assertions below FAIL.
func TestFallbackPortRange_DoesNotOverlapEphemeralRange(t *testing.T) {
	low, high, ok := readEphemeralRangeForTest(t)
	if !ok {
		t.Skip("SKIP-OK: /proc/sys/net/ipv4/ip_local_port_range unreadable on this host; " +
			"no independent oracle for the ephemeral range")
	}

	block, err := fallbackPortRange()
	require.NoError(t, err, "a fallback block must be placeable against range %d-%d", low, high)

	assert.LessOrEqual(t, block.Start, block.End, "fallback block must be a valid range")
	assert.GreaterOrEqual(t, block.Start, lowestFallbackPort,
		"fallback block must stay clear of privileged ports")
	assert.LessOrEqual(t, block.End, maxPort, "fallback block must stay within legal ports")

	// Two half-open intervals overlap iff each starts before the other ends.
	overlaps := block.Start <= high && low <= block.End
	assert.False(t, overlaps,
		"fallback block %d-%d overlaps the kernel ephemeral range %d-%d",
		block.Start, block.End, low, high)
}

// TestAllocateFallbackPort_Exhausted_ReturnsErrNoPortsAvailable proves the
// documented exhaustion behaviour survived the fix: once the fallback block has
// no free port left, allocation reports ErrNoPortsAvailable rather than falling
// back into the ephemeral range or inventing an unrelated error.
//
// FALSIFIABILITY: in allocateFallbackPortUnsafe, drop the
// `pa.config.FallbackPortRange` override and always use the computed block. The
// computed block is fallbackBlockSize (1024) ports wide, so the small loop below
// can never exhaust it, no ErrNoPortsAvailable is ever produced, and this test
// FAILS. (Executed and confirmed FAILing before this test was accepted.)
func TestAllocateFallbackPort_Exhausted_ReturnsErrNoPortsAvailable(t *testing.T) {
	// Take a deliberately tiny slice of the real fallback block so exhaustion is
	// reachable in a handful of allocations instead of 1024.
	computed, err := fallbackPortRange()
	require.NoError(t, err, "a fallback block must be placeable on this host")

	const smallBlock = 3
	tiny := PortRange{Start: computed.Start, End: computed.Start + smallBlock - 1}

	const preferredPort = 80 // reserved below, so allocation must fall through

	pa := NewPortAllocator(PortAllocatorConfig{
		AllowEphemeral:    true,
		PortRanges:        map[string]PortRange{},
		ReservedPorts:     []int{preferredPort},
		FallbackPortRange: &tiny,
	})

	// Allocate until the block is empty. A port in the block may already be held
	// by an unrelated process on this host, so the number of successes is not
	// asserted exactly — only that at least one succeeded and that the run ends
	// in ErrNoPortsAvailable.
	successes := 0
	var lastErr error
	for i := 0; i < smallBlock+1; i++ {
		port, allocErr := pa.AllocatePort("svc-"+strconv.Itoa(i), preferredPort)
		if allocErr != nil {
			lastErr = allocErr
			break
		}
		assert.GreaterOrEqual(t, port, tiny.Start, "allocation must come from the fallback block")
		assert.LessOrEqual(t, port, tiny.End, "allocation must come from the fallback block")
		successes++
	}

	require.Greater(t, successes, 0,
		"at least one allocation must succeed from the fallback block %d-%d", tiny.Start, tiny.End)
	require.Error(t, lastErr, "exhausting the fallback block must produce an error")
	assert.ErrorIs(t, lastErr, ErrNoPortsAvailable,
		"exhaustion must report ErrNoPortsAvailable, got %v", lastErr)
}

// TestAllocatePort_ZeroPreferredIsNotReserved guards the port-0 sentinel.
//
// THE DEFECT: isPortAvailableUnsafe(0) binds ":0", which is the kernel's
// "pick one for me" form and therefore ALWAYS succeeds. Without a validity
// check the allocator concluded port 0 was "available" and reserved it, then
// DiscoveryClient.Register published 0 into the service registry as the
// address other services should dial. Reachable via getDefaultPort, which
// returns c.config.DefaultPorts[name] whenever the KEY EXISTS — so an operator
// config with 0 for a service produces exists=true, port=0.
//
// FALSIFYING MUTATION (§1.1): delete `preferredPort > 0 &&` from AllocatePort.
// The allocator then reserves 0 and this test FAILS.
func TestAllocatePort_ZeroPreferredIsNotReserved(t *testing.T) {
	pa := NewDefaultPortAllocator()

	port, err := pa.AllocatePort("svc-with-zero-default", 0)
	require.NoError(t, err, "a 0 preferred port must fall through to a real range, not error")
	require.NotZero(t, port, "port 0 must never be reserved — it is unreachable for any client")
	require.Greater(t, port, 0, "allocated port must be a dialable port number")
}

// Ports above the protocol maximum are rejected for the mirror-image reason.
func TestAllocatePort_OutOfRangePreferredIsNotReserved(t *testing.T) {
	pa := NewDefaultPortAllocator()

	port, err := pa.AllocatePort("svc-with-absurd-default", 70000)
	require.NoError(t, err)
	require.LessOrEqual(t, port, maxPort, "allocated port must be within the protocol range")
	require.Greater(t, port, 0)
}
