package tools_test

import (
	"context"
	"encoding/json"
	"io"
	"sync"
	"testing"
	"time"

	"go.lsp.dev/jsonrpc2"
	"go.lsp.dev/protocol"
	"go.lsp.dev/uri"

	"dev.helix.code/internal/tools"
)

// duplexPipe wires two halves of a connection over a pair of os.Pipe-equivalents.
//
// We use io.Pipe (in-memory, synchronous) on each direction, then wrap each end
// in an io.ReadWriteCloser that reads from one pipe and writes to the other.
// This lets us run two real jsonrpc2 connections back-to-back without sockets,
// subprocesses, or mocks: every byte traverses the LSP framing layer.
type duplexEnd struct {
	r *io.PipeReader
	w *io.PipeWriter
}

func (d *duplexEnd) Read(p []byte) (int, error)  { return d.r.Read(p) }
func (d *duplexEnd) Write(p []byte) (int, error) { return d.w.Write(p) }
func (d *duplexEnd) Close() error {
	_ = d.r.Close()
	return d.w.Close()
}

// newDuplexPipes returns a pair of io.ReadWriteClosers that talk to each other.
//
// Anything client writes is read by server, and vice versa.
func newDuplexPipes() (clientEnd, serverEnd io.ReadWriteCloser) {
	c2sR, c2sW := io.Pipe() // client -> server
	s2cR, s2cW := io.Pipe() // server -> client
	clientEnd = &duplexEnd{r: s2cR, w: c2sW}
	serverEnd = &duplexEnd{r: c2sR, w: s2cW}
	return clientEnd, serverEnd
}

// testLSPServer is a tiny real LSP server: it speaks the actual jsonrpc2
// protocol via go.lsp.dev/jsonrpc2 (not a mock — the client cannot tell it
// from a real subprocess) and records every inbound method.
type testLSPServer struct {
	t    *testing.T
	conn jsonrpc2.Conn

	mu       sync.Mutex
	received []recordedReq

	// publishOnInitialized, if non-empty, is sent as a publishDiagnostics
	// notification once the client signals "initialized".
	publishOnInitialized []protocol.PublishDiagnosticsParams
}

type recordedReq struct {
	Method string
	Params json.RawMessage
	IsCall bool
}

func newTestLSPServer(t *testing.T, ctx context.Context, transport io.ReadWriteCloser) *testLSPServer {
	t.Helper()
	stream := jsonrpc2.NewStream(transport)
	conn := jsonrpc2.NewConn(stream)
	s := &testLSPServer{t: t, conn: conn}
	conn.Go(ctx, s.handle)
	return s
}

func (s *testLSPServer) record(req jsonrpc2.Request, isCall bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.received = append(s.received, recordedReq{
		Method: req.Method(),
		Params: append(json.RawMessage(nil), req.Params()...),
		IsCall: isCall,
	})
}

func (s *testLSPServer) recvCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.received)
}

func (s *testLSPServer) waitFor(method string, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		s.mu.Lock()
		for _, r := range s.received {
			if r.Method == method {
				s.mu.Unlock()
				return true
			}
		}
		s.mu.Unlock()
		time.Sleep(5 * time.Millisecond)
	}
	return false
}

func (s *testLSPServer) findReq(method string) *recordedReq {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.received {
		if s.received[i].Method == method {
			return &s.received[i]
		}
	}
	return nil
}

func (s *testLSPServer) handle(ctx context.Context, reply jsonrpc2.Replier, req jsonrpc2.Request) error {
	switch r := req.(type) {
	case *jsonrpc2.Call:
		s.record(req, true)
		switch r.Method() {
		case protocol.MethodInitialize:
			return reply(ctx, &protocol.InitializeResult{
				Capabilities: protocol.ServerCapabilities{
					TextDocumentSync: protocol.TextDocumentSyncKindFull,
				},
			}, nil)
		case protocol.MethodShutdown:
			return reply(ctx, nil, nil)
		default:
			return reply(ctx, nil, jsonrpc2.NewError(jsonrpc2.MethodNotFound, "method not found"))
		}
	case *jsonrpc2.Notification:
		s.record(req, false)
		if r.Method() == protocol.MethodInitialized {
			for _, p := range s.publishOnInitialized {
				if err := s.conn.Notify(ctx, protocol.MethodTextDocumentPublishDiagnostics, p); err != nil {
					s.t.Logf("server publish failed: %v", err)
				}
			}
		}
		return nil
	}
	return nil
}

// pushDiagnostics asks the server to send a publishDiagnostics notification
// to the client right now (used after handshake is already complete).
func (s *testLSPServer) pushDiagnostics(ctx context.Context, p protocol.PublishDiagnosticsParams) error {
	return s.conn.Notify(ctx, protocol.MethodTextDocumentPublishDiagnostics, p)
}

// ---------- Tests ----------

func TestLSPClient_InitializeHandshake(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	clientEnd, serverEnd := newDuplexPipes()
	srv := newTestLSPServer(t, ctx, serverEnd)

	c := tools.NewLSPClient(clientEnd, "gopls")
	defer c.Close()

	if err := c.Initialize(ctx, "/tmp/root", nil); err != nil {
		t.Fatalf("Initialize: %v", err)
	}

	if !srv.waitFor(protocol.MethodInitialize, time.Second) {
		t.Fatal("server never received initialize")
	}
	if !srv.waitFor(protocol.MethodInitialized, time.Second) {
		t.Fatal("server never received initialized notification")
	}

	initReq := srv.findReq(protocol.MethodInitialize)
	if initReq == nil || !initReq.IsCall {
		t.Fatal("initialize must be a call (request), not a notification")
	}
	var params protocol.InitializeParams
	if err := json.Unmarshal(initReq.Params, &params); err != nil {
		t.Fatalf("decode initialize params: %v", err)
	}
	if got, want := string(params.RootURI), string(uri.File("/tmp/root")); got != want {
		t.Fatalf("rootURI: got %q want %q", got, want)
	}
}

func TestLSPClient_InitializeIsIdempotent(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	clientEnd, serverEnd := newDuplexPipes()
	srv := newTestLSPServer(t, ctx, serverEnd)

	c := tools.NewLSPClient(clientEnd, "gopls")
	defer c.Close()

	if err := c.Initialize(ctx, "/tmp/root", nil); err != nil {
		t.Fatalf("Initialize 1: %v", err)
	}
	if err := c.Initialize(ctx, "/tmp/root", nil); err != nil {
		t.Fatalf("Initialize 2: %v", err)
	}

	// NEEDS-REDESIGN (deliberately still a sleep): this asserts a NEGATIVE —
	// that NO duplicate handshake was sent. A predicate wait cannot prove a
	// non-event; the correct fix is a flush BARRIER (send a later request,
	// wait for the server to record it, then count — ordering on one
	// connection then guarantees any duplicate would already have arrived).
	// That needs a barrier request this client API does not currently expose,
	// so it is surfaced here rather than smuggled in as a production hook.
	time.Sleep(50 * time.Millisecond)

	srv.mu.Lock()
	defer srv.mu.Unlock()
	count := 0
	for _, r := range srv.received {
		if r.Method == protocol.MethodInitialize {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("initialize should be sent exactly once, got %d", count)
	}
}

func TestLSPClient_DidOpenSendsCorrectPayload(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	clientEnd, serverEnd := newDuplexPipes()
	srv := newTestLSPServer(t, ctx, serverEnd)

	c := tools.NewLSPClient(clientEnd, "gopls")
	defer c.Close()

	if err := c.Initialize(ctx, "/tmp/root", nil); err != nil {
		t.Fatalf("Initialize: %v", err)
	}

	if err := c.DidOpen(ctx, "/tmp/root/main.go", "go", "package main\n"); err != nil {
		t.Fatalf("DidOpen: %v", err)
	}

	if !srv.waitFor(protocol.MethodTextDocumentDidOpen, time.Second) {
		t.Fatal("server never received didOpen")
	}
	openReq := srv.findReq(protocol.MethodTextDocumentDidOpen)
	if openReq == nil || openReq.IsCall {
		t.Fatal("didOpen must be a notification, not a call")
	}
	var p protocol.DidOpenTextDocumentParams
	if err := json.Unmarshal(openReq.Params, &p); err != nil {
		t.Fatalf("decode didOpen params: %v", err)
	}
	if string(p.TextDocument.LanguageID) != "go" {
		t.Fatalf("languageId: got %q want %q", p.TextDocument.LanguageID, "go")
	}
	if string(p.TextDocument.URI) != string(uri.File("/tmp/root/main.go")) {
		t.Fatalf("uri: got %q", p.TextDocument.URI)
	}
	if p.TextDocument.Text != "package main\n" {
		t.Fatalf("text: got %q", p.TextDocument.Text)
	}
}

func TestLSPClient_DidChangeFullSync(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	clientEnd, serverEnd := newDuplexPipes()
	srv := newTestLSPServer(t, ctx, serverEnd)

	c := tools.NewLSPClient(clientEnd, "gopls")
	defer c.Close()

	if err := c.Initialize(ctx, "/tmp/root", nil); err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	if err := c.DidOpen(ctx, "/tmp/root/main.go", "go", "v1"); err != nil {
		t.Fatalf("DidOpen: %v", err)
	}
	if err := c.DidChange(ctx, "/tmp/root/main.go", "v2"); err != nil {
		t.Fatalf("DidChange: %v", err)
	}

	if !srv.waitFor(protocol.MethodTextDocumentDidChange, time.Second) {
		t.Fatal("server never received didChange")
	}
	chReq := srv.findReq(protocol.MethodTextDocumentDidChange)
	var p protocol.DidChangeTextDocumentParams
	if err := json.Unmarshal(chReq.Params, &p); err != nil {
		t.Fatalf("decode didChange: %v", err)
	}
	if len(p.ContentChanges) != 1 || p.ContentChanges[0].Text != "v2" {
		t.Fatalf("expected single full-sync change with text 'v2', got %+v", p.ContentChanges)
	}
	if p.TextDocument.Version <= 1 {
		t.Fatalf("version should advance after didChange, got %d", p.TextDocument.Version)
	}
}

func TestLSPClient_PublishDiagnosticsConvertsToWrappedDiagnostic(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	clientEnd, serverEnd := newDuplexPipes()
	srv := newTestLSPServer(t, ctx, serverEnd)

	filePath := "/tmp/root/main.go"
	srv.publishOnInitialized = []protocol.PublishDiagnosticsParams{{
		URI: uri.File(filePath),
		Diagnostics: []protocol.Diagnostic{
			{
				Severity: protocol.DiagnosticSeverityError,
				Message:  "undefined: foo",
				Source:   "compiler",
				Code:     "E001",
				Range: protocol.Range{
					Start: protocol.Position{Line: 1, Character: 2},
					End:   protocol.Position{Line: 1, Character: 5},
				},
			},
			{
				Severity: protocol.DiagnosticSeverityWarning,
				Message:  "unused var",
				Range: protocol.Range{
					Start: protocol.Position{Line: 3, Character: 0},
					End:   protocol.Position{Line: 3, Character: 4},
				},
			},
		},
	}}

	c := tools.NewLSPClient(clientEnd, "gopls")
	defer c.Close()

	if err := c.Initialize(ctx, "/tmp/root", nil); err != nil {
		t.Fatalf("Initialize: %v", err)
	}

	// Wait for diagnostics to arrive.
	deadline := time.Now().Add(30 * time.Second)
	var diags []tools.Diagnostic
	for time.Now().Before(deadline) {
		diags = c.GetDiagnostics(filePath)
		if len(diags) == 2 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if len(diags) != 2 {
		t.Fatalf("expected 2 diagnostics for %s, got %d", filePath, len(diags))
	}

	var errDiag, warnDiag tools.Diagnostic
	for _, d := range diags {
		switch d.Severity {
		case tools.SeverityError:
			errDiag = d
		case tools.SeverityWarning:
			warnDiag = d
		}
	}
	if errDiag.Message != "undefined: foo" {
		t.Fatalf("error message: %q", errDiag.Message)
	}
	if errDiag.FilePath != filePath {
		t.Fatalf("error filePath: %q", errDiag.FilePath)
	}
	if errDiag.Source != "gopls" {
		t.Fatalf("error source: got %q want %q (server name)", errDiag.Source, "gopls")
	}
	if errDiag.Code != "E001" {
		t.Fatalf("error code: %q", errDiag.Code)
	}
	if errDiag.Range.Start.Line != 1 || errDiag.Range.End.Character != 5 {
		t.Fatalf("error range: %+v", errDiag.Range)
	}
	if errDiag.ID == "" || warnDiag.ID == "" {
		t.Fatalf("ids must be assigned: err=%q warn=%q", errDiag.ID, warnDiag.ID)
	}
	if errDiag.ID == warnDiag.ID {
		t.Fatalf("ids must be unique: %q == %q", errDiag.ID, warnDiag.ID)
	}

	all := c.AllDiagnostics()
	if len(all) != 2 {
		t.Fatalf("AllDiagnostics: got %d want 2", len(all))
	}
}

func TestLSPClient_GetDiagnosticsForUnknownFileReturnsEmpty(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	clientEnd, serverEnd := newDuplexPipes()
	_ = newTestLSPServer(t, ctx, serverEnd)

	c := tools.NewLSPClient(clientEnd, "gopls")
	defer c.Close()

	if err := c.Initialize(ctx, "/tmp/root", nil); err != nil {
		t.Fatalf("Initialize: %v", err)
	}

	got := c.GetDiagnostics("/never/touched.go")
	if len(got) != 0 {
		t.Fatalf("expected empty slice, got %d items", len(got))
	}
}

func TestLSPClient_DiagnosticIDsAreReplacedOnRepublish(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	clientEnd, serverEnd := newDuplexPipes()
	srv := newTestLSPServer(t, ctx, serverEnd)

	c := tools.NewLSPClient(clientEnd, "gopls")
	defer c.Close()

	if err := c.Initialize(ctx, "/tmp/root", nil); err != nil {
		t.Fatalf("Initialize: %v", err)
	}

	filePath := "/tmp/root/main.go"
	first := protocol.PublishDiagnosticsParams{
		URI: uri.File(filePath),
		Diagnostics: []protocol.Diagnostic{
			{Severity: protocol.DiagnosticSeverityError, Message: "first"},
		},
	}
	second := protocol.PublishDiagnosticsParams{
		URI: uri.File(filePath),
		Diagnostics: []protocol.Diagnostic{
			{Severity: protocol.DiagnosticSeverityError, Message: "second-a"},
			{Severity: protocol.DiagnosticSeverityWarning, Message: "second-b"},
		},
	}

	if err := srv.pushDiagnostics(ctx, first); err != nil {
		t.Fatalf("push 1: %v", err)
	}

	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) && len(c.GetDiagnostics(filePath)) == 0 {
		time.Sleep(5 * time.Millisecond)
	}
	firstSeen := c.GetDiagnostics(filePath)
	if len(firstSeen) != 1 || firstSeen[0].Message != "first" {
		t.Fatalf("first seen: %+v", firstSeen)
	}

	if err := srv.pushDiagnostics(ctx, second); err != nil {
		t.Fatalf("push 2: %v", err)
	}
	deadline = time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		got := c.GetDiagnostics(filePath)
		if len(got) == 2 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	secondSeen := c.GetDiagnostics(filePath)
	if len(secondSeen) != 2 {
		t.Fatalf("second seen len: %d", len(secondSeen))
	}
	// Old IDs from first publish must NOT survive — second-publish IDs are unique
	// per item but unrelated to first.
	for _, d := range secondSeen {
		if d.ID == firstSeen[0].ID {
			t.Fatalf("republish should mint fresh IDs; saw old ID %q", d.ID)
		}
	}
	if secondSeen[0].ID == secondSeen[1].ID {
		t.Fatalf("republished IDs must remain unique within a publish")
	}
}

func TestLSPClient_ShutdownClosesConn(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	clientEnd, serverEnd := newDuplexPipes()
	srv := newTestLSPServer(t, ctx, serverEnd)

	c := tools.NewLSPClient(clientEnd, "gopls")

	if err := c.Initialize(ctx, "/tmp/root", nil); err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	if err := c.Shutdown(ctx); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}

	if !srv.waitFor(protocol.MethodShutdown, time.Second) {
		t.Fatal("server never received shutdown")
	}
	if !srv.waitFor(protocol.MethodExit, time.Second) {
		t.Fatal("server never received exit notification")
	}
	if err := c.Close(); err != nil {
		t.Fatalf("Close after Shutdown returned error: %v", err)
	}
}
