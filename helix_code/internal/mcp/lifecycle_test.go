package mcp

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClient_HandshakeSuccess(t *testing.T) {
	ft := newFakeTransport()
	c := NewClient("srv-a", ft)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	// Answer each handshake message as it is actually sent, instead of
	// sleeping 50ms and hoping it has been sent by then. This also removes the
	// require.NotNil calls that ran on a non-test goroutine (calling t.FailNow
	// off the test goroutine is undefined); a message that never arrives now
	// surfaces as a Connect error on the assertion below.
	ft.respondWith(handshakeResponder([]map[string]any{{"name": "echo"}}))

	require.NoError(t, c.Connect(ctx))
	assert.Equal(t, StateReady, c.State())
	tools := c.Tools()
	require.Len(t, tools, 1)
	assert.Equal(t, "echo", tools[0].Name)
}

func TestClient_CallToolReturnsResult(t *testing.T) {
	ft := newFakeTransport()
	c := NewClient("srv-a", ft)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	// Handshake and tools/call are both answered inline as each request is
	// sent, so the test is correct at any speed.
	ft.respondWith(respondAlso(
		handshakeResponder([]map[string]any{}),
		func(m *MCPMessage) *MCPMessage {
			if m.Method == "tools/call" {
				return &MCPMessage{JSONRPC: "2.0", ID: m.ID, Result: map[string]any{
					"content": []map[string]any{{"type": "text", "text": "hello"}},
				}}
			}
			return nil
		},
	))
	require.NoError(t, c.Connect(ctx))

	res, err := c.CallTool(ctx, "echo", map[string]any{"x": 1})
	require.NoError(t, err)
	assert.NotNil(t, res)
}

func TestClient_StateTransitions(t *testing.T) {
	ft := newFakeTransport()
	c := NewClient("srv-a", ft)
	assert.Equal(t, StateDisconnected, c.State())
	c.setState(StateConnecting)
	assert.Equal(t, StateConnecting, c.State())
	c.setState(StateReady)
	assert.Equal(t, StateReady, c.State())
}

// contextWithSeededHandshake seeds the fake transport with replies and
// returns a short-lived context for Connect. Helper to dedupe boilerplate.
func contextWithSeededHandshake(t *testing.T, ft *fakeTransport) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	t.Cleanup(cancel)
	ft.respondWith(handshakeResponder([]map[string]any{}))
	return ctx
}

func TestClient_CloseIdempotentUnderRace(t *testing.T) {
	ft := newFakeTransport()
	c := NewClient("srv-a", ft)
	require.NoError(t, c.Connect(contextWithSeededHandshake(t, ft)))

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = c.Close()
		}()
	}
	wg.Wait()
}

func TestClient_HandshakeFailureStopsRecvLoop(t *testing.T) {
	ft := newFakeTransport()
	c := NewClient("srv-a", ft)
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	// Don't push any reply — handshake will time out.
	err := c.Connect(ctx)
	require.Error(t, err)
	assert.Equal(t, StateDisconnected, c.State())
	// NEEDS-REDESIGN (deliberately left as a sleep): this waits for recvLoop
	// to observe its cancelled context and return. Client exposes no
	// observable "recvLoop exited" signal, and adding one is a change to
	// PRODUCTION code (a done-channel on Client), not a test fix — so it is
	// surfaced here rather than smuggled in. The assertions below do not
	// depend on this sleep for correctness; a zombie recvLoop would steal a
	// handshake reply and fail the Connect below, which is the actual guard.
	time.Sleep(50 * time.Millisecond)
	// A retry must work cleanly (no zombie recvLoop racing on the same transport).
	ctx2, cancel2 := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel2()
	ft.respondWith(handshakeResponder([]map[string]any{}))
	require.NoError(t, c.Connect(ctx2))
	assert.Equal(t, StateReady, c.State())
	require.NoError(t, c.Close())
}
