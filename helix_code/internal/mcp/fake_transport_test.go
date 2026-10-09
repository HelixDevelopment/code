package mcp

import (
	"context"
	"sync"
)

// fakeTransport is a programmable transport for unit-testing Client.
//
// Synchronisation contract: tests MUST NOT sleep waiting for the client to
// send something. A sleep that waits for an async event is not a
// synchronisation primitive — under CPU load it expires before the event
// happens and the test fails for a reason unrelated to the code under test.
// Two event-driven facilities replace it:
//
//   - respondWith(fn) installs an auto-responder. Every message the client
//     Sends is handed to fn inline, and any non-nil reply fn returns is
//     queued for Recv immediately. A handshake therefore completes at
//     whatever speed the machine runs at — instantly on an idle box,
//     correctly on a loaded one, with no timer involved either way.
type fakeTransport struct {
	mu         sync.Mutex
	sent       []*MCPMessage
	recvCh     chan *recvItem
	openErr    error
	t          TransportType
	closed     bool
	autoReply func(*MCPMessage) *MCPMessage
}

func newFakeTransport() *fakeTransport {
	return &fakeTransport{
		recvCh: make(chan *recvItem, 32),
		t:      TransportType("fake"),
	}
}

func (f *fakeTransport) Type() TransportType { return f.t }
func (f *fakeTransport) Open(ctx context.Context) error {
	if f.openErr != nil {
		return f.openErr
	}
	return nil
}

func (f *fakeTransport) Send(ctx context.Context, m *MCPMessage) error {
	f.mu.Lock()
	if f.closed {
		f.mu.Unlock()
		return ErrTransportClosed
	}
	f.sent = append(f.sent, m)
	fn := f.autoReply
	f.mu.Unlock()

	// Reply OUTSIDE the lock: pushReply writes to a buffered channel and must
	// never be able to block a Send while f.mu is held.
	if fn != nil {
		if reply := fn(m); reply != nil {
			f.pushReply(reply)
		}
	}
	return nil
}

func (f *fakeTransport) Recv(ctx context.Context) (*MCPMessage, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case item := <-f.recvCh:
		if item.err != nil {
			return nil, item.err
		}
		return item.msg, nil
	}
}
func (f *fakeTransport) Close() error {
	f.mu.Lock()
	f.closed = true
	f.mu.Unlock()
	return nil
}

// pushReply queues a synthetic server response.
func (f *fakeTransport) pushReply(m *MCPMessage) {
	f.recvCh <- &recvItem{msg: m}
}

// pushError queues a synthetic transport error.
func (f *fakeTransport) pushError(err error) {
	f.recvCh <- &recvItem{err: err}
}

// sentMessages returns a snapshot of all messages Send received.
func (f *fakeTransport) sentMessages() []*MCPMessage {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]*MCPMessage, len(f.sent))
	copy(out, f.sent)
	return out
}

// respondWith installs an auto-responder consulted on every Send. Passing nil
// removes it. See the type comment for why this exists.
func (f *fakeTransport) respondWith(fn func(*MCPMessage) *MCPMessage) {
	f.mu.Lock()
	f.autoReply = fn
	f.mu.Unlock()
}

// handshakeResponder auto-answers the initialize / tools-list handshake with
// the supplied tool list. notifications/initialized carries no ID and needs no
// reply, so it (and anything else) falls through to nil.
func handshakeResponder(tools []map[string]any) func(*MCPMessage) *MCPMessage {
	return func(m *MCPMessage) *MCPMessage {
		switch m.Method {
		case "initialize":
			return &MCPMessage{JSONRPC: "2.0", ID: m.ID, Result: map[string]any{
				"capabilities": map[string]any{"tools": map[string]any{}},
			}}
		case "tools/list":
			return &MCPMessage{JSONRPC: "2.0", ID: m.ID, Result: map[string]any{"tools": tools}}
		}
		return nil
	}
}

// respondAlso chains an extra responder after base: base wins when it answers,
// otherwise extra is consulted. Used where a test needs the standard handshake
// plus one more method (e.g. tools/call).
func respondAlso(base, extra func(*MCPMessage) *MCPMessage) func(*MCPMessage) *MCPMessage {
	return func(m *MCPMessage) *MCPMessage {
		if base != nil {
			if r := base(m); r != nil {
				return r
			}
		}
		if extra != nil {
			return extra(m)
		}
		return nil
	}
}
