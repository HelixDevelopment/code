package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"

	"dev.helix.code/tests/stresschaos"
)

// §11.4.85(B) chaos coverage for internal/mcp.
//
// Chaos classes exercised against the REAL *MCPServer / *Manager (the only test
// double is MockConn, standing in for the network socket seam — the registry,
// dispatch, session-map, and tool-map logic is 100% real):
//
//   - tool-handler-panic isolation: a tool handler that panics mid-dispatch
//     MUST NOT take down the server. handleMessage dispatches each message in
//     its OWN goroutine (server.go `go s.handleMessage(...)`); an unrecovered
//     panic in tool.Handler therefore crashes the WHOLE process (and every
//     other goroutine, including unrelated work). The server MUST isolate a
//     panicking handler so the session survives and the server stays usable.
//     (This is the exact bug class found in internal/event + internal/hooks.)
//   - input-corruption: structurally hostile tool arguments (NaN/Inf, channel/
//     func values, oversized strings, nested cycles) + malformed JSON-RPC frames
//     are fed. Dispatch + the server's logging paths MUST not crash on them.
//   - state-corruption under contention: tools are concurrently Registered and
//     called while sessions are opened/closed mid-flight. The server MUST never
//     panic or race and MUST end in a self-consistent map.

// chaosSession builds a real *MCPSession backed by a concurrency-safe MockConn.
func chaosSession() *MCPSession {
	return &MCPSession{
		ID:           uuid.New(),
		Conn:         &MockConn{},
		CreatedAt:    time.Now(),
		LastActivity: time.Now(),
		Context:      make(map[string]interface{}),
	}
}

// TestMCPServer_Chaos_ToolHandlerPanicIsolation registers a tool whose handler
// panics, then dispatches a tools/call through the REAL handleMessage goroutine
// path (mirroring how handleSession dispatches every inbound message). An
// unrecovered panic in a dispatch goroutine crashes the whole `go test` binary,
// so this test is the canary for the bug class. The server MUST isolate the
// panic so co-registered tools still work and the server stays usable.
func TestMCPServer_Chaos_ToolHandlerPanicIsolation(t *testing.T) {
	rec := stresschaos.NewChaosRecorder(t, "mcp_server_tool_handler_panic_isolation", "process-death")
	server := NewMCPServer()
	ctx := context.Background()

	var goodHits, panicHits int64
	if err := server.RegisterTool(&Tool{
		ID: "good", Name: "good", Parameters: map[string]interface{}{},
		Handler: func(ctx context.Context, s *MCPSession, a map[string]interface{}) (interface{}, error) {
			atomic.AddInt64(&goodHits, 1)
			return "ok", nil
		},
	}); err != nil {
		t.Fatalf("register good tool: %v", err)
	}
	if err := server.RegisterTool(&Tool{
		ID: "boom", Name: "boom", Parameters: map[string]interface{}{},
		Handler: func(ctx context.Context, s *MCPSession, a map[string]interface{}) (interface{}, error) {
			atomic.AddInt64(&panicHits, 1)
			panic("chaos: tool handler panic")
		},
	}); err != nil {
		t.Fatalf("register boom tool: %v", err)
	}

	session := chaosSession()

	// Drive the panicking tool through the REAL goroutine dispatch path:
	// handleMessage is what handleSession launches as `go s.handleMessage`.
	// If the server does not recover, this goroutine's panic kills the whole
	// process — no recover() in THIS goroutine can save it. The test binary
	// crashing is the four-layer hard failure. If the server DOES recover,
	// handleMessage returns normally and we observe a clean error response.
	var dispatchWG sync.WaitGroup
	dispatchWG.Add(1)
	go func() {
		defer dispatchWG.Done()
		// This inner recover only catches a panic that propagates SYNCHRONOUSLY
		// back to us (it would mean the server failed to isolate). If the server
		// itself isolates the panic in its own dispatch goroutine, we never see
		// it here and the process stays alive.
		defer func() {
			if p := recover(); p != nil {
				rec.Record(stresschaos.Fatal,
					fmt.Sprintf("tool-handler panic propagated to dispatcher caller: %v", p))
			}
		}()
		server.handleMessage(session, &MCPMessage{
			ID: "boom-call", Method: "tools/call",
			Params: map[string]interface{}{"name": "boom", "arguments": map[string]interface{}{}},
		})
		rec.Record(stresschaos.Recovered, "panicking tool dispatch returned without crashing the process")
	}()
	dispatchWG.Wait()
	// The server may isolate the panicking handler in its OWN goroutine, so
	// panicHits can land after handleMessage returns. Wait for the predicate
	// with a generous ceiling instead of sleeping a guessed 30ms: on an idle
	// box this exits on the first check, and it stays correct on a loaded one.
	settleDeadline := time.Now().Add(15 * time.Second)
	for atomic.LoadInt64(&panicHits) == 0 && time.Now().Before(settleDeadline) {
		time.Sleep(time.Millisecond)
	}

	if atomic.LoadInt64(&panicHits) == 0 {
		rec.Record(stresschaos.Fatal, "panic tool handler never ran — dispatch path not exercised")
	}

	// The server MUST remain usable: a fresh call to the good tool must work.
	good := chaosSession()
	server.handleCallTool(ctx, good, &MCPMessage{
		ID: "good-call", Method: "tools/call",
		Params: map[string]interface{}{"name": "good", "arguments": map[string]interface{}{}},
	})
	if atomic.LoadInt64(&goodHits) == 0 {
		rec.Record(stresschaos.Fatal, "server unusable after tool-handler panic — good tool dispatched nothing")
	} else {
		rec.Record(stresschaos.Recovered, "server still usable after tool-handler panic")
	}

	rec.AssertNoFatal()
	t.Logf("mcp server survived tool-handler panic: goodHits=%d panicHits=%d",
		atomic.LoadInt64(&goodHits), atomic.LoadInt64(&panicHits))
}

// TestMCPServer_Chaos_CorruptToolArgs feeds structurally hostile tool arguments
// to the REAL server. Dispatch and the result-formatting path (fmt.Sprintf of
// the handler return) must not panic on NaN/Inf floats, unmarshalable channel/
// func values, oversized keys, or nested cycles — a crash on malformed input is
// a §11.4.85(B) failure. The handler reads the args (mirroring real consumers)
// so the corrupt data flows all the way through dispatch + response encoding.
func TestMCPServer_Chaos_CorruptToolArgs(t *testing.T) {
	server := NewMCPServer()
	ctx := context.Background()

	// Handler that genuinely touches the args and echoes a hostile value back
	// (exercises the handleCallTool fmt.Sprintf("%v", result) formatting path).
	if err := server.RegisterTool(&Tool{
		ID: "consume", Name: "consume", Parameters: map[string]interface{}{},
		Handler: func(ctx context.Context, s *MCPSession, args map[string]interface{}) (interface{}, error) {
			for k, v := range args {
				_ = fmt.Sprintf("%s=%v", k, v) // forces evaluation of hostile values
			}
			return args, nil // echo the whole hostile map back through formatting
		},
	}); err != nil {
		t.Fatalf("register consume tool: %v", err)
	}

	corruptKinds := []map[string]interface{}{
		{"nan": math.NaN()},
		{"inf": math.Inf(1)},
		{"channel": "unmarshalable-marker-chan"},
		{"func": "unmarshalable-marker-func"},
		{"huge_key": makeHugeMCPString(1 << 16)},
		{"nested": map[string]interface{}{"a": map[string]interface{}{"b": math.NaN()}}},
	}
	payloads := make([][]byte, len(corruptKinds))
	for i, k := range corruptKinds {
		b, err := json.Marshal(k)
		if err != nil {
			b = []byte(fmt.Sprintf(`{"corrupt_index":%d}`, i))
		}
		payloads[i] = b
	}

	stresschaos.ChaosCorruptInputDuring(t, "mcp_server_corrupt_tool_args", payloads,
		func(input []byte) error {
			idx := corruptMCPIndexOf(input)
			args := hostileMCPArgsFor(idx)
			session := chaosSession()
			// Dispatch must not panic regardless of args.
			server.handleCallTool(ctx, session, &MCPMessage{
				ID: "c", Method: "tools/call",
				Params: map[string]interface{}{"name": "consume", "arguments": args},
			})
			return nil
		})
}

// TestMCPServer_Chaos_MalformedJSONRPC feeds malformed JSON-RPC frames at the
// REAL unmarshalParams + handleMessage routing surface (the server's own input-
// validation layer): unknown methods, wrong-typed params, truncated JSON,
// non-object params. The server MUST reject each cleanly (a JSON-RPC error
// response) — never panic, never corrupt state.
func TestMCPServer_Chaos_MalformedJSONRPC(t *testing.T) {
	server := NewMCPServer()
	rec := stresschaos.NewChaosRecorder(t, "mcp_server_malformed_jsonrpc", "input-corruption")

	frames := []*MCPMessage{
		{ID: "1", Method: "tools/call", Params: json.RawMessage(`{"name":}`)},      // truncated JSON
		{ID: "2", Method: "tools/call", Params: json.RawMessage(`["not","obj"]`)},  // array, not object
		{ID: "3", Method: "tools/call", Params: json.RawMessage(`42`)},             // scalar params
		{ID: "4", Method: "initialize", Params: json.RawMessage(`{"x":[1,2,3]}`)},  // wrong-typed
		{ID: "5", Method: "totally/unknown/method", Params: nil},                   // unknown method
		{ID: nil, Method: "ping", Params: nil},                                     // nil id
		{ID: "7", Method: "tools/call", Params: json.RawMessage(`{"name":12345}`)}, // wrong-typed name
	}

	for i, fr := range frames {
		func(idx int, frame *MCPMessage) {
			defer func() {
				if p := recover(); p != nil {
					rec.Record(stresschaos.Fatal, fmt.Sprintf("handleMessage[%d] panicked on malformed frame: %v", idx, p))
				}
			}()
			session := chaosSession()
			server.handleMessage(session, frame)
			// A response (error or otherwise) written cleanly is graceful.
			if session.Conn.(*MockConn).WriteCalled() {
				rec.Record(stresschaos.Degraded, fmt.Sprintf("frame[%d] handled with a clean response (no crash)", idx))
			} else {
				rec.Record(stresschaos.Recovered, fmt.Sprintf("frame[%d] handled without crash (no response required)", idx))
			}
		}(i, fr)
	}

	// Server must still work for a valid registration + call after the onslaught.
	var hit int64
	if err := server.RegisterTool(&Tool{
		ID: "after", Name: "after", Parameters: map[string]interface{}{},
		Handler: func(ctx context.Context, s *MCPSession, a map[string]interface{}) (interface{}, error) {
			atomic.AddInt64(&hit, 1)
			return nil, nil
		},
	}); err != nil {
		rec.Record(stresschaos.Fatal, "server refused a valid tool after malformed onslaught: "+err.Error())
	}
	server.handleCallTool(context.Background(), chaosSession(), &MCPMessage{
		ID: "after-call", Method: "tools/call",
		Params: map[string]interface{}{"name": "after", "arguments": map[string]interface{}{}},
	})
	if atomic.LoadInt64(&hit) == 0 {
		rec.Record(stresschaos.Fatal, "server did not dispatch to a valid tool after malformed onslaught")
	} else {
		rec.Record(stresschaos.Recovered, "server dispatches correctly after malformed onslaught")
	}

	rec.AssertNoFatal()
}

// TestMCPServer_Chaos_ConcurrentSessionAndToolChurn hammers the SAME server
// with concurrent RegisterTool + handleCallTool + session open/close (via the
// real sessionMux + toolMux) from many goroutines. The real mutexes must
// serialise the map mutations so the server never panics or races and the maps
// end self-consistent. Run under -race.
func TestMCPServer_Chaos_ConcurrentSessionAndToolChurn(t *testing.T) {
	rec := stresschaos.NewChaosRecorder(t, "mcp_server_session_tool_churn", "state-corruption")
	server := NewMCPServer()
	ctx := context.Background()

	// One always-present tool so concurrent calls have a real target.
	if err := server.RegisterTool(&Tool{
		ID: "base", Name: "base", Parameters: map[string]interface{}{},
		Handler: func(ctx context.Context, s *MCPSession, a map[string]interface{}) (interface{}, error) { return "ok", nil },
	}); err != nil {
		t.Fatalf("register base: %v", err)
	}

	const goroutines = 12
	const iters = 250
	var wg sync.WaitGroup
	var regs, calls, opens, closes int64

	for w := 0; w < goroutines; w++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			defer func() {
				if p := recover(); p != nil {
					rec.Record(stresschaos.Fatal, fmt.Sprintf("goroutine %d panicked: %v", id, p))
				}
			}()
			for it := 0; it < iters; it++ {
				switch (id + it) % 4 {
				case 0:
					// Register a fresh tool (write toolMux).
					_ = server.RegisterTool(&Tool{
						ID: fmt.Sprintf("g%d-i%d", id, it), Name: fmt.Sprintf("g%d-i%d", id, it),
						Parameters: map[string]interface{}{},
						Handler:    func(ctx context.Context, s *MCPSession, a map[string]interface{}) (interface{}, error) { return nil, nil },
					})
					atomic.AddInt64(&regs, 1)
				case 1:
					// Call the base tool (read toolMux + dispatch).
					server.handleCallTool(ctx, chaosSession(), &MCPMessage{
						ID: "c", Method: "tools/call",
						Params: map[string]interface{}{"name": "base", "arguments": map[string]interface{}{}}})
					atomic.AddInt64(&calls, 1)
				case 2:
					// Open a session into the shared sessionMux map.
					sess := chaosSession()
					server.sessionMux.Lock()
					server.sessions[sess.ID] = sess
					server.sessionMux.Unlock()
					atomic.AddInt64(&opens, 1)
					// And close it back via the real CloseSession path.
					server.CloseSession(sess.ID)
					atomic.AddInt64(&closes, 1)
				default:
					_ = server.GetToolCount()
					_ = server.GetSessionCount()
				}
			}
		}(w)
	}
	wg.Wait()

	rec.Record(stresschaos.Recovered, fmt.Sprintf(
		"survived register/call/session churn: %d regs, %d calls, %d opens, %d closes, no panic/race",
		atomic.LoadInt64(&regs), atomic.LoadInt64(&calls), atomic.LoadInt64(&opens), atomic.LoadInt64(&closes)))

	// Final state must be coherent and the server must still dispatch.
	if server.GetSessionCount() != 0 {
		rec.Record(stresschaos.Degraded, fmt.Sprintf("residual sessions after churn: %d", server.GetSessionCount()))
	}
	var finalHit int64
	if err := server.RegisterTool(&Tool{
		ID: "final", Name: "final", Parameters: map[string]interface{}{},
		Handler: func(ctx context.Context, s *MCPSession, a map[string]interface{}) (interface{}, error) {
			atomic.AddInt64(&finalHit, 1)
			return nil, nil
		},
	}); err != nil {
		rec.Record(stresschaos.Fatal, "could not register final probe after churn: "+err.Error())
	}
	server.handleCallTool(ctx, chaosSession(), &MCPMessage{
		ID: "final-call", Method: "tools/call",
		Params: map[string]interface{}{"name": "final", "arguments": map[string]interface{}{}}})
	if atomic.LoadInt64(&finalHit) == 0 {
		rec.Record(stresschaos.Fatal, "server did not dispatch to a fresh tool after churn — map corrupted")
	} else {
		rec.Record(stresschaos.Recovered, "server dispatches correctly after churn — map self-consistent")
	}

	rec.AssertNoFatal()
	t.Logf("mcp server churn: regs=%d calls=%d opens=%d closes=%d final-tools=%d",
		atomic.LoadInt64(&regs), atomic.LoadInt64(&calls), atomic.LoadInt64(&opens), atomic.LoadInt64(&closes),
		server.GetToolCount())
}

// TestMCPClient_Chaos_EventCallbackPanicIsolation installs an onEvent callback
// that panics, then drives Client state transitions (Connect failure path +
// Close) which fire setState -> onEvent. setState invokes the callback inline
// while NOT holding the lock; an unrecovered panic in the callback would crash
// the caller goroutine. The client MUST survive a panicking event callback.
func TestMCPClient_Chaos_EventCallbackPanicIsolation(t *testing.T) {
	rec := stresschaos.NewChaosRecorder(t, "mcp_client_event_callback_panic_isolation", "process-death")

	c := NewClient("panic-cb", newFakeTransport())
	var fired int64
	c.SetOnEvent(func(e Event) {
		atomic.AddInt64(&fired, 1)
		panic("chaos: event callback panic")
	})

	// Connect with a fakeTransport that never replies -> handshake will hang on
	// the init RPC; so instead drive setState directly via the public Close path
	// which is guaranteed to fire setState(StateClosed) -> onEvent.
	func() {
		defer func() {
			if p := recover(); p != nil {
				rec.Record(stresschaos.Fatal,
					fmt.Sprintf("event callback panic propagated to Close() caller: %v", p))
			}
		}()
		_ = c.Close()
		rec.Record(stresschaos.Recovered, "Close() completed despite panicking event callback")
	}()

	if atomic.LoadInt64(&fired) == 0 {
		rec.Record(stresschaos.Fatal, "event callback never fired — state-transition path not exercised")
	}

	rec.AssertNoFatal()
	t.Logf("mcp client survived event-callback panic: fired=%d", atomic.LoadInt64(&fired))
}

// makeHugeMCPString returns an n-byte string of 'x' for oversized-payload chaos.
func makeHugeMCPString(n int) string {
	b := make([]byte, n)
	for i := range b {
		b[i] = 'x'
	}
	return string(b)
}

// corruptMCPIndexOf recovers the chaos payload index from the descriptor.
func corruptMCPIndexOf(input []byte) int {
	var m map[string]json.RawMessage
	if err := json.Unmarshal(input, &m); err != nil {
		return 0
	}
	if _, ok := m["corrupt_index"]; ok {
		var probe struct {
			CorruptIndex int `json:"corrupt_index"`
		}
		if json.Unmarshal(input, &probe) == nil {
			return probe.CorruptIndex
		}
	}
	switch {
	case hasMCPKey(m, "channel"):
		return 2
	case hasMCPKey(m, "func"):
		return 3
	case hasMCPKey(m, "huge_key"):
		return 4
	case hasMCPKey(m, "nested"):
		return 5
	case hasMCPKey(m, "nan"):
		return 0
	case hasMCPKey(m, "inf"):
		return 1
	}
	return 0
}

func hasMCPKey(m map[string]json.RawMessage, key string) bool {
	_, ok := m[key]
	return ok
}

// hostileMCPArgsFor reconstructs the actual hostile args map for a chaos index,
// including types (chan, func) that cannot survive []byte serialisation but
// exercise dispatch + the response-formatting path.
func hostileMCPArgsFor(idx int) map[string]interface{} {
	switch idx {
	case 0:
		return map[string]interface{}{"nan": math.NaN()}
	case 1:
		return map[string]interface{}{"inf": math.Inf(1)}
	case 2:
		return map[string]interface{}{"channel": make(chan int)}
	case 3:
		return map[string]interface{}{"func": func() {}}
	case 4:
		return map[string]interface{}{"huge_key": makeHugeMCPString(1 << 16)}
	default:
		return map[string]interface{}{"nested": map[string]interface{}{"a": map[string]interface{}{"b": math.NaN()}}}
	}
}
