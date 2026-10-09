package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// runSSEServer returns (postURL, sseURL, controlCloseStream, waitStream, cleanup).
// controlCloseStream() closes the active SSE stream so the client must reconnect.
// waitStream(n, ceiling) blocks until the server has accepted at least n SSE
// streams, waking on the accept itself rather than on a timer. It reports
// whether the predicate held; callers assert that bool, never elapsed time.
func runSSEServer(t *testing.T) (string, string, func(), func(int64, time.Duration) bool, func()) {
	t.Helper()
	mux := http.NewServeMux()
	var sessionID atomic.Int64
	type session struct {
		flusher http.Flusher
		w       http.ResponseWriter
		done    chan struct{}
	}
	var current atomic.Pointer[session]

	// Generation broadcast so tests can wait for the Nth stream to be
	// established instead of sleeping a guessed interval.
	var genMu sync.Mutex
	genCh := make(chan struct{})
	bumpGen := func() {
		genMu.Lock()
		close(genCh)
		genCh = make(chan struct{})
		genMu.Unlock()
	}
	waitStream := func(min int64, ceiling time.Duration) bool {
		timer := time.NewTimer(ceiling)
		defer timer.Stop()
		for {
			// Capture the generation BEFORE reading the count, so a bump that
			// lands in between wakes us instead of being missed.
			genMu.Lock()
			g := genCh
			genMu.Unlock()
			if sessionID.Load() >= min {
				return true
			}
			select {
			case <-g:
			case <-timer.C:
				return sessionID.Load() >= min
			}
		}
	}

	mux.HandleFunc("/sse", func(w http.ResponseWriter, r *http.Request) {
		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "no flush", 500)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		flusher.Flush()
		s := &session{flusher: flusher, w: w, done: make(chan struct{})}
		current.Store(s)
		sessionID.Add(1)
		bumpGen()
		<-s.done
	})
	mux.HandleFunc("/post", func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req MCPMessage
		require.NoError(t, json.Unmarshal(body, &req))
		s := current.Load()
		if s == nil {
			http.Error(w, "no session", 503)
			return
		}
		resp := MCPMessage{JSONRPC: "2.0", ID: req.ID, Result: map[string]any{"ok": true}}
		b, _ := json.Marshal(&resp)
		fmt.Fprintf(s.w, "data: %s\n\n", string(b))
		s.flusher.Flush()
		w.WriteHeader(204)
	})
	srv := httptest.NewServer(mux)
	closeStream := func() {
		s := current.Load()
		if s != nil {
			close(s.done)
			current.Store(nil)
		}
	}
	cleanup := func() {
		closeStream()
		srv.Close()
	}
	return srv.URL + "/post", srv.URL + "/sse", closeStream, waitStream, cleanup
}

func TestSSETransport_RoundTrip(t *testing.T) {
	postURL, sseURL, _, waitStream, cleanup := runSSEServer(t)
	defer cleanup()
	tr := NewSSETransport(SSEConfig{PostURL: postURL, SSEURL: sseURL, BackoffOverride: 50 * time.Millisecond})
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	require.NoError(t, tr.Open(ctx))
	defer tr.Close()
	// /post 503s when no SSE stream is attached, so wait for the stream to
	// actually exist rather than sleeping 100ms and hoping.
	require.True(t, waitStream(1, 20*time.Second), "initial SSE stream never established")
	require.NoError(t, tr.Send(ctx, &MCPMessage{JSONRPC: "2.0", ID: "1", Method: "ping"}))
	resp, err := tr.Recv(ctx)
	require.NoError(t, err)
	assert.Equal(t, "1", resp.ID)
	assert.Equal(t, TransportSSE, tr.Type())
}

func TestSSETransport_ReconnectAfterStreamClose(t *testing.T) {
	postURL, sseURL, closeStream, waitStream, cleanup := runSSEServer(t)
	defer cleanup()
	tr := NewSSETransport(SSEConfig{PostURL: postURL, SSEURL: sseURL, BackoffOverride: 50 * time.Millisecond})
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	require.NoError(t, tr.Open(ctx))
	defer tr.Close()
	require.True(t, waitStream(1, 20*time.Second), "initial SSE stream never established")
	closeStream()
	// The reconnect IS the behaviour under test. Wait for the server to accept
	// the SECOND stream — the observable proof the transport re-dialled — then
	// assert the counter. Both are predicates; neither is a timer.
	require.True(t, waitStream(2, 30*time.Second), "transport never re-established the SSE stream")
	assert.GreaterOrEqual(t, tr.Reconnects(), int64(1))
	require.NoError(t, tr.Send(ctx, &MCPMessage{JSONRPC: "2.0", ID: "2", Method: "ping"}))
	resp, err := tr.Recv(ctx)
	require.NoError(t, err)
	assert.Equal(t, "2", resp.ID)
}

// REQUIRED regression test (added based on T03/T04 lesson)
func TestSSETransport_CloseUnblocksRecv(t *testing.T) {
	postURL, sseURL, _, _, cleanup := runSSEServer(t)
	defer cleanup()
	tr := NewSSETransport(SSEConfig{PostURL: postURL, SSEURL: sseURL, BackoffOverride: 50 * time.Millisecond})
	require.NoError(t, tr.Open(context.Background()))

	done := make(chan error, 1)
	go func() {
		_, err := tr.Recv(context.Background())
		done <- err
	}()

	require.NoError(t, tr.Close())
	select {
	case err := <-done:
		require.ErrorIs(t, err, ErrTransportClosed)
	case <-time.After(2 * time.Second):
		t.Fatal("Recv did not unblock after Close")
	}
}
