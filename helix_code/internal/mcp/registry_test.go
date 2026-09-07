package mcp

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestManager_AddClientAndListTools(t *testing.T) {
	m := NewManager()
	c := NewClient("srv-a", newFakeTransport())
	m.addClient(c)
	c.tools = []ExternalTool{{Name: "echo"}, {Name: "time"}}
	c.state.Store(int32(StateReady))

	tools := m.Tools()
	require.Len(t, tools, 2)
	names := []string{tools[0].Server + ":" + tools[0].Name, tools[1].Server + ":" + tools[1].Name}
	assert.Contains(t, names, "srv-a:echo")
	assert.Contains(t, names, "srv-a:time")
}

func TestManager_CallToolRoutes(t *testing.T) {
	m := NewManager()
	ft := newFakeTransport()
	c := NewClient("srv-a", ft)
	c.tools = []ExternalTool{{Name: "echo"}}
	c.state.Store(int32(StateReady))
	m.addClient(c)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	// Answer tools/call the instant it is sent rather than sleeping 20ms and
	// hoping it has been sent by then.
	ft.respondWith(func(m *MCPMessage) *MCPMessage {
		if m.Method == "tools/call" {
			return &MCPMessage{JSONRPC: "2.0", ID: m.ID, Result: map[string]any{
				"content": []map[string]any{{"type": "text", "text": "ok"}},
			}}
		}
		return nil
	})
	go c.recvLoop(ctx)

	res, err := m.CallTool(ctx, "srv-a", "echo", map[string]any{"x": 1})
	require.NoError(t, err)
	assert.NotNil(t, res)
}

func TestManager_ServerNotFound(t *testing.T) {
	m := NewManager()
	_, err := m.CallTool(context.Background(), "missing", "x", nil)
	assert.ErrorIs(t, err, ErrServerNotFound)
}

func TestManager_StatusReportsAllClients(t *testing.T) {
	m := NewManager()
	a := NewClient("a", newFakeTransport())
	a.state.Store(int32(StateReady))
	a.tools = []ExternalTool{{Name: "x"}}
	b := NewClient("b", newFakeTransport())
	b.state.Store(int32(StateDisconnected))
	m.addClient(a)
	m.addClient(b)
	st := m.Status()
	require.Len(t, st, 2)
}

func TestManager_ReloadDetectsEnvChange(t *testing.T) {
	cfgOld := &Config{Servers: []ServerSpec{
		{Name: "a", Transport: TransportStdio, Command: []string{"x"}, Env: map[string]string{"K": "v1"}},
	}}
	cfgNew := &Config{Servers: []ServerSpec{
		{Name: "a", Transport: TransportStdio, Command: []string{"x"}, Env: map[string]string{"K": "v2"}},
	}}
	m := NewManager()
	m.SetConfig(cfgOld)
	// Manually register a client matching old spec (sidestep transport spawn)
	c := NewClient("a", newFakeTransport())
	m.addClient(c)
	// Reload with new env value — old client must be replaced
	require.NoError(t, m.Reload(context.Background(), cfgNew))
	// The map should now contain a new client (different pointer from the old one).
	m.mu.RLock()
	got, ok := m.clients["a"]
	m.mu.RUnlock()
	require.True(t, ok)
	require.NotSame(t, c, got, "Reload must replace the client when Env changes")
}
