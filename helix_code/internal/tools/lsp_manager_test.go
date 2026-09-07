package tools_test

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"

	"go.uber.org/zap"

	"dev.helix.code/internal/tools"
)

// fakeServerBinary is the absolute path to the freshly-built
// internal/tools/lsp_fakeserver binary. Populated by TestMain so every
// LSPManager test in this file can spawn it as a real subprocess.
var fakeServerBinary string

// TestMain compiles the in-tree fake LSP server once before any
// LSPManager tests run. We deliberately use `go build` (real compile,
// real binary on disk, real exec.Command from tests) rather than any
// shortcut: the manager's whole job is to drive a real subprocess, so
// the test scaffold must too.
//
// The binary lives in os.TempDir() under a username-suffixed name so
// parallel test runs from different users don't collide.
func TestMain(m *testing.M) {
	tmpDir, err := os.MkdirTemp("", "helix-lsp-manager-test-")
	if err != nil {
		fmt.Fprintln(os.Stderr, "TestMain: mkdtemp:", err)
		os.Exit(2)
	}
	binName := "helix-lsp-fakeserver"
	if runtime.GOOS == "windows" {
		binName += ".exe"
	}
	binPath := filepath.Join(tmpDir, binName)

	// We assume `go test ./internal/tools/...` is invoked with the
	// HelixCode/ Go module as the cwd or that `go build` can resolve
	// the import path from anywhere inside the module — both are true
	// for this repo because there is exactly one go.mod at HelixCode/.
	cmd := exec.Command("go", "build", "-o", binPath, "dev.helix.code/internal/tools/lsp_fakeserver")
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "TestMain: failed to build fake LSP server:", err)
		_ = os.RemoveAll(tmpDir)
		os.Exit(2)
	}
	fakeServerBinary = binPath

	code := m.Run()

	_ = os.RemoveAll(tmpDir)
	os.Exit(code)
}

// fakeSpec returns a curated-allowlist entry for the in-tree fake
// LSP server. We use the .fake extension so we never collide with
// real-language pickers in other tests.
func fakeSpec() tools.LSPServerSpec {
	return tools.LSPServerSpec{
		Name:           "fake",
		Binary:         fakeServerBinary,
		Args:           nil,
		FileExtensions: []string{".fake"},
		LanguageID:     "fake",
	}
}

// writeTempFile writes content to a fresh file inside t.TempDir() and
// returns its absolute path. Extension determines which spec the
// manager will route the file to.
func writeTempFile(t *testing.T, ext, content string) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "doc"+ext)
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatalf("write temp file: %v", err)
	}
	return p
}

// waitForDiagnostics polls m.GetDiagnostics(filePath) for up to `timeout`,
// returning when the diagnostic count satisfies pred or timing out.
// Returns the last seen diagnostics regardless.
func waitForDiagnostics(m *tools.LSPManager, filePath string, pred func(int) bool, timeout time.Duration) []tools.Diagnostic {
	deadline := time.Now().Add(timeout)
	var last []tools.Diagnostic
	for time.Now().Before(deadline) {
		last = m.GetDiagnostics(filePath)
		if pred(len(last)) {
			return last
		}
		time.Sleep(20 * time.Millisecond)
	}
	return last
}

func waitForServers(m *tools.LSPManager, want int, timeout time.Duration) []tools.ServerInfo {
	deadline := time.Now().Add(timeout)
	var last []tools.ServerInfo
	for time.Now().Before(deadline) {
		last = m.Servers()
		if len(last) == want {
			return last
		}
		time.Sleep(20 * time.Millisecond)
	}
	return last
}

// findServer returns the named server's info or nil.
func findServer(infos []tools.ServerInfo, name string) *tools.ServerInfo {
	for i := range infos {
		if infos[i].Name == name {
			return &infos[i]
		}
	}
	return nil
}

// ---------- Tests ----------

func TestLSPManager_LazySpawnsOnFirstUse(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	m := tools.NewLSPManager(t.TempDir(), []tools.LSPServerSpec{fakeSpec()}, zap.NewNop())
	defer m.Shutdown(ctx)

	// Before any use: no servers running.
	if got := len(m.Servers()); got != 0 {
		t.Fatalf("Servers() before use: got %d want 0", got)
	}

	filePath := writeTempFile(t, ".fake", "// @fake-error: bad code here\nbody\n")
	if err := m.NotifyOpen(ctx, filePath); err != nil {
		t.Fatalf("NotifyOpen: %v", err)
	}

	infos := waitForServers(m, 1, 5*time.Second)
	if len(infos) != 1 {
		t.Fatalf("Servers() after first NotifyOpen: got %d want 1", len(infos))
	}
	if infos[0].Name != "fake" {
		t.Fatalf("server name: got %q want %q", infos[0].Name, "fake")
	}
	if infos[0].PID == 0 {
		t.Fatalf("server PID: got 0 want >0")
	}

	diags := waitForDiagnostics(m, filePath, func(n int) bool { return n >= 1 }, 5*time.Second)
	if len(diags) != 1 {
		t.Fatalf("diagnostics: got %d want 1 (full=%+v)", len(diags), diags)
	}
	if diags[0].Severity != tools.SeverityError {
		t.Fatalf("severity: got %v want error", diags[0].Severity)
	}
	if !strings.Contains(diags[0].Message, "bad code here") {
		t.Fatalf("message: got %q want substring 'bad code here'", diags[0].Message)
	}
	if diags[0].FilePath != filePath {
		t.Fatalf("filePath: got %q want %q", diags[0].FilePath, filePath)
	}
	if diags[0].Source != "fake" {
		t.Fatalf("source: got %q want %q", diags[0].Source, "fake")
	}
}

func TestLSPManager_NoSpawnForUnknownExtension(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	m := tools.NewLSPManager(t.TempDir(), []tools.LSPServerSpec{fakeSpec()}, zap.NewNop())
	defer m.Shutdown(ctx)

	filePath := writeTempFile(t, ".unknown", "irrelevant\n")
	if err := m.NotifyOpen(ctx, filePath); err != nil {
		t.Fatalf("NotifyOpen for unknown ext should be a no-op error-free, got: %v", err)
	}

	if got := len(m.Servers()); got != 0 {
		t.Fatalf("Servers() after unknown ext: got %d want 0", got)
	}
	if got := len(m.GetDiagnostics(filePath)); got != 0 {
		t.Fatalf("GetDiagnostics for unrouted file: got %d want 0", got)
	}
}

func TestLSPManager_DidChangePublishesNewDiagnostics(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	m := tools.NewLSPManager(t.TempDir(), []tools.LSPServerSpec{fakeSpec()}, zap.NewNop())
	defer m.Shutdown(ctx)

	filePath := writeTempFile(t, ".fake", "// @fake-error: original\n")
	if err := m.NotifyOpen(ctx, filePath); err != nil {
		t.Fatalf("NotifyOpen: %v", err)
	}
	first := waitForDiagnostics(m, filePath, func(n int) bool { return n >= 1 }, 5*time.Second)
	if len(first) != 1 || !strings.Contains(first[0].Message, "original") {
		t.Fatalf("first diagnostics wrong: %+v", first)
	}

	// Send didChange with a different fake-error pragma.
	updated := "// @fake-error: changed-message\n// @fake-error: second one\n"
	if err := m.NotifyChange(ctx, filePath, updated); err != nil {
		t.Fatalf("NotifyChange: %v", err)
	}

	second := waitForDiagnostics(m, filePath, func(n int) bool { return n >= 2 }, 5*time.Second)
	if len(second) != 2 {
		t.Fatalf("after didChange diagnostics: got %d want 2 (%+v)", len(second), second)
	}
	var sawChanged, sawSecond bool
	for _, d := range second {
		if strings.Contains(d.Message, "changed-message") {
			sawChanged = true
		}
		if strings.Contains(d.Message, "second one") {
			sawSecond = true
		}
	}
	if !sawChanged || !sawSecond {
		t.Fatalf("expected both new messages, got %+v", second)
	}
}

func TestLSPManager_RestartCleansAndRespawns(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	m := tools.NewLSPManager(t.TempDir(), []tools.LSPServerSpec{fakeSpec()}, zap.NewNop())
	defer m.Shutdown(ctx)

	filePath := writeTempFile(t, ".fake", "// @fake-error: a\n")
	if err := m.NotifyOpen(ctx, filePath); err != nil {
		t.Fatalf("NotifyOpen: %v", err)
	}
	infos := waitForServers(m, 1, 5*time.Second)
	if len(infos) != 1 {
		t.Fatalf("expected 1 server, got %d", len(infos))
	}
	oldPID := infos[0].PID
	if oldPID == 0 {
		t.Fatalf("oldPID is 0")
	}

	if err := m.Restart(ctx, "fake"); err != nil {
		t.Fatalf("Restart: %v", err)
	}

	// After restart, we expect a (possibly empty for a moment then) running
	// server. Re-open the file to drive a respawn if Restart left the
	// pool empty (manager may either keep an entry or re-spawn lazily).
	if err := m.NotifyOpen(ctx, filePath); err != nil {
		t.Fatalf("NotifyOpen after Restart: %v", err)
	}

	// Wait for a server with a different PID.
	deadline := time.Now().Add(5 * time.Second)
	var newPID int
	for time.Now().Before(deadline) {
		infos = m.Servers()
		s := findServer(infos, "fake")
		if s != nil && s.PID != 0 && s.PID != oldPID {
			newPID = s.PID
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if newPID == 0 {
		t.Fatalf("expected respawned server with different PID; oldPID=%d, infos=%+v", oldPID, infos)
	}
}

func TestLSPManager_StopKillsServer(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	m := tools.NewLSPManager(t.TempDir(), []tools.LSPServerSpec{fakeSpec()}, zap.NewNop())
	defer m.Shutdown(ctx)

	filePath := writeTempFile(t, ".fake", "// @fake-error: x\n")
	if err := m.NotifyOpen(ctx, filePath); err != nil {
		t.Fatalf("NotifyOpen: %v", err)
	}
	if infos := waitForServers(m, 1, 5*time.Second); len(infos) != 1 {
		t.Fatalf("expected 1 server, got %d", len(infos))
	}

	if err := m.Stop(ctx, "fake"); err != nil {
		t.Fatalf("Stop: %v", err)
	}

	// After Stop: server entry may or may not remain in the snapshot,
	// but if it remains its status must be Stopped.
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		infos := m.Servers()
		if len(infos) == 0 {
			return
		}
		if s := findServer(infos, "fake"); s != nil && s.Status == tools.ServerStatusStopped {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("Stop did not surface as empty pool or Stopped status within timeout: %+v", m.Servers())
}

func TestLSPManager_IdleTimeoutShutsDown(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	m := tools.NewLSPManager(t.TempDir(), []tools.LSPServerSpec{fakeSpec()}, zap.NewNop())
	// 200ms idle window; the timer thread polls/checks a bit slower so
	// we need a generous wait below.
	m.SetIdleTimeout(200 * time.Millisecond)
	defer m.Shutdown(ctx)

	filePath := writeTempFile(t, ".fake", "// @fake-error: x\n")
	if err := m.NotifyOpen(ctx, filePath); err != nil {
		t.Fatalf("NotifyOpen: %v", err)
	}
	if infos := waitForServers(m, 1, 5*time.Second); len(infos) != 1 {
		t.Fatalf("expected 1 server, got %d", len(infos))
	}

	// Wait well past the idle timeout. We give a comfortable margin
	// (~10x) because the watcher goroutine + LSP shutdown handshake
	// + process exit can take a few hundred ms in CI.
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		infos := m.Servers()
		if len(infos) == 0 {
			return
		}
		if s := findServer(infos, "fake"); s != nil && s.Status == tools.ServerStatusStopped {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("idle-timeout did not stop server within margin: %+v", m.Servers())
}

func TestLSPManager_CrashRecoveryRespawnsOnNextUse(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	m := tools.NewLSPManager(t.TempDir(), []tools.LSPServerSpec{fakeSpec()}, zap.NewNop())
	defer m.Shutdown(ctx)

	filePath := writeTempFile(t, ".fake", "// @fake-error: a\n")
	if err := m.NotifyOpen(ctx, filePath); err != nil {
		t.Fatalf("NotifyOpen: %v", err)
	}
	infos := waitForServers(m, 1, 5*time.Second)
	if len(infos) != 1 {
		t.Fatalf("expected 1 server, got %d", len(infos))
	}
	oldPID := infos[0].PID
	if oldPID == 0 {
		t.Fatalf("oldPID is 0")
	}

	// Send SIGKILL to the process out-of-band to simulate a crash.
	proc, err := os.FindProcess(oldPID)
	if err != nil {
		t.Fatalf("FindProcess: %v", err)
	}
	if err := proc.Signal(syscall.SIGKILL); err != nil {
		t.Fatalf("SIGKILL: %v", err)
	}

	// Wait for the manager's wait-goroutine to actually OBSERVE the crash
	// rather than sleeping 300ms and hoping it was scheduled. The status is an
	// observable, so assert the predicate; the ceiling only bounds a wedge.
	crashDeadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(crashDeadline) {
		s := findServer(m.Servers(), "fake")
		if s == nil || s.Status == tools.ServerStatusCrashed || s.Status == tools.ServerStatusStopped {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}

	// Now drive another open: should respawn lazily.
	if err := m.NotifyOpen(ctx, filePath); err != nil {
		t.Fatalf("NotifyOpen after crash: %v", err)
	}

	deadline := time.Now().Add(5 * time.Second)
	var newPID int
	for time.Now().Before(deadline) {
		s := findServer(m.Servers(), "fake")
		if s != nil && s.PID != 0 && s.PID != oldPID {
			newPID = s.PID
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if newPID == 0 {
		t.Fatalf("expected respawned server with different PID after crash; oldPID=%d", oldPID)
	}
}

func TestLSPManager_ShutdownStopsAllServers(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	m := tools.NewLSPManager(t.TempDir(), []tools.LSPServerSpec{fakeSpec()}, zap.NewNop())

	filePath := writeTempFile(t, ".fake", "// @fake-error: x\n")
	if err := m.NotifyOpen(ctx, filePath); err != nil {
		t.Fatalf("NotifyOpen: %v", err)
	}
	infos := waitForServers(m, 1, 5*time.Second)
	if len(infos) != 1 {
		t.Fatalf("expected 1 server, got %d", len(infos))
	}
	pid := infos[0].PID

	if err := m.Shutdown(ctx); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}

	// After Shutdown: all server entries gone or Stopped.
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		infos := m.Servers()
		stoppedAll := true
		for _, s := range infos {
			if s.Status != tools.ServerStatusStopped {
				stoppedAll = false
				break
			}
		}
		if stoppedAll {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	// Sanity-check the OS view: the original PID must no longer be
	// alive (signal 0 should fail).
	if pid > 0 {
		if err := syscall.Kill(pid, 0); err == nil {
			t.Fatalf("PID %d still alive after Shutdown", pid)
		}
	}
}
