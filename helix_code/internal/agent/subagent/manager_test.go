// manager_test.go — P1-F15-T05 SubagentManager unit tests.
//
// All tests use REAL InProcessSpawner with REAL FakeLLMProvider for the
// in-process path. The subprocess path is exercised through a small
// recordingSpawner stub (a real SubagentSpawner implementation that records
// the call) — this is a hexagonal seam for routing-only verification, not a
// mock of the spawner->provider chain (which the in-process tests cover via
// the real fake provider).
//
// Anti-bluff hard rules respected here:
//   - In-process path uses real FakeLLMProvider (not a mock)
//   - Kill test verifies the goroutine actually got canceled by reading the
//     aggregator result's State (not by inspecting internal flags)
//   - MaxConcurrency uses FakeLLMProvider.WithDelay (real time-based blocking)
package subagent

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"dev.helix.code/internal/llm"
)

// recordingSpawner is a TEST-ONLY SubagentSpawner that records the most recent
// Spawn call and emits a canned successful result. Used to assert the manager
// routes IsolationWorktree tasks to the subprocess spawner WITHOUT requiring
// a real subprocess (T06 brings real worktree).
type recordingSpawner struct {
	mu     sync.Mutex
	called bool
	task   SubagentTask
	kind   string
	delay  time.Duration
}

func newRecordingSpawner(kind string) *recordingSpawner {
	return &recordingSpawner{kind: kind}
}

func (s *recordingSpawner) Kind() string { return s.kind }

func (s *recordingSpawner) Spawn(ctx context.Context, task SubagentTask, _ llm.Provider) (<-chan SubagentResult, error) {
	s.mu.Lock()
	s.called = true
	s.task = task
	delay := s.delay
	s.mu.Unlock()

	out := make(chan SubagentResult, 1)
	go func() {
		startedAt := time.Now()
		if delay > 0 {
			select {
			case <-time.After(delay):
			case <-ctx.Done():
				out <- SubagentResult{
					TaskID:      task.ID,
					State:       StateCanceled,
					StartedAt:   startedAt,
					CompletedAt: time.Now(),
					Duration:    time.Since(startedAt),
					Isolation:   task.Isolation,
					Error:       ctx.Err().Error(),
				}
				close(out)
				return
			}
		}
		out <- SubagentResult{
			TaskID:      task.ID,
			State:       StateSucceeded,
			Output:      "recording-spawner-output",
			StartedAt:   startedAt,
			CompletedAt: time.Now(),
			Duration:    time.Since(startedAt),
			Isolation:   task.Isolation,
		}
		close(out)
	}()
	return out, nil
}

func (s *recordingSpawner) wasCalled() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.called
}

func (s *recordingSpawner) recordedTask() SubagentTask {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.task
}

// drainAll reads up to n results from the manager's aggregator (or returns
// what it has when ctx fires).
func drainAll(t *testing.T, m *SubagentManager, n int, timeout time.Duration) []SubagentResult {
	t.Helper()
	out := make([]SubagentResult, 0, n)
	deadline := time.After(timeout)
	for len(out) < n {
		select {
		case r, ok := <-m.Results():
			if !ok {
				return out
			}
			out = append(out, r)
		case <-deadline:
			t.Fatalf("drainAll: timed out after %v with %d/%d results", timeout, len(out), n)
		}
	}
	return out
}

func newManagerForTest(t *testing.T, opts SubagentManagerOptions) *SubagentManager {
	t.Helper()
	if opts.LLMProvider == nil {
		opts.LLMProvider = NewFakeLLMProvider(nil)
	}
	if opts.InProcessSpawner == nil {
		opts.InProcessSpawner = NewInProcessSpawner()
	}
	if opts.SubprocessSpawner == nil {
		// Always supply a recording spawner here so tests never accidentally
		// re-exec the test binary as a subprocess child.
		opts.SubprocessSpawner = newRecordingSpawner("subprocess-recording")
	}
	m, err := NewSubagentManager(opts)
	if err != nil {
		t.Fatalf("NewSubagentManager: %v", err)
	}
	t.Cleanup(func() {
		_ = m.Shutdown(context.Background())
	})
	return m
}

// =====================================================================
// Construction / defaults
// =====================================================================

func TestSubagentManager_NilProviderErrors(t *testing.T) {
	_, err := NewSubagentManager(SubagentManagerOptions{
		LLMProvider: nil,
	})
	if err == nil {
		t.Fatalf("expected error for nil LLMProvider")
	}
}

func TestSubagentManager_DefaultsApplied(t *testing.T) {
	m, err := NewSubagentManager(SubagentManagerOptions{
		LLMProvider: NewFakeLLMProvider(nil),
	})
	if err != nil {
		t.Fatalf("NewSubagentManager: %v", err)
	}
	defer m.Shutdown(context.Background())

	if m.opts.MaxConcurrency != DefaultMaxConcurrency {
		t.Fatalf("MaxConcurrency: got %d, want %d", m.opts.MaxConcurrency, DefaultMaxConcurrency)
	}
	if m.opts.DefaultTimeout != DefaultTaskTimeout {
		t.Fatalf("DefaultTimeout: got %v, want %v", m.opts.DefaultTimeout, DefaultTaskTimeout)
	}
	if m.opts.InProcessSpawner == nil {
		t.Fatalf("InProcessSpawner: expected default, got nil")
	}
	if m.opts.SubprocessSpawner == nil {
		t.Fatalf("SubprocessSpawner: expected default, got nil")
	}
}

// =====================================================================
// Dispatch — ID assignment + routing
// =====================================================================

func TestSubagentManager_DispatchAssignsID_WhenEmpty(t *testing.T) {
	m := newManagerForTest(t, SubagentManagerOptions{})

	id, err := m.Dispatch(context.Background(), SubagentTask{
		Description: "anonymous-id",
		Prompt:      "hello",
		Isolation:   IsolationNone,
	})
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	if id == "" {
		t.Fatalf("expected non-empty UUID, got empty string")
	}
	if len(id) != 36 {
		t.Fatalf("expected 36-char UUID v4, got %q (len=%d)", id, len(id))
	}

	res := drainAll(t, m, 1, 2*time.Second)
	if res[0].TaskID != id {
		t.Fatalf("aggregator TaskID=%q does not match dispatched id=%q", res[0].TaskID, id)
	}
}

func TestSubagentManager_DispatchUsesProvidedID(t *testing.T) {
	m := newManagerForTest(t, SubagentManagerOptions{})

	id, err := m.Dispatch(context.Background(), SubagentTask{
		ID:          "fixed-id",
		Description: "explicit-id",
		Prompt:      "hello",
		Isolation:   IsolationNone,
	})
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	if id != "fixed-id" {
		t.Fatalf("expected id=fixed-id, got %q", id)
	}

	res := drainAll(t, m, 1, 2*time.Second)
	if res[0].TaskID != "fixed-id" {
		t.Fatalf("expected TaskID=fixed-id, got %q", res[0].TaskID)
	}
}

func TestSubagentManager_DispatchInProcess_ResultOnAggregator(t *testing.T) {
	provider := NewFakeLLMProvider(nil)
	provider.SetCanned("ping", "pong-from-real-provider")

	m := newManagerForTest(t, SubagentManagerOptions{
		LLMProvider: provider,
	})

	id, err := m.Dispatch(context.Background(), SubagentTask{
		Description: "in-process-real",
		Prompt:      "ping",
		Isolation:   IsolationNone,
	})
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}

	results := drainAll(t, m, 1, 2*time.Second)
	r := results[0]
	if r.TaskID != id {
		t.Fatalf("TaskID mismatch: got %q, want %q", r.TaskID, id)
	}
	if r.State != StateSucceeded {
		t.Fatalf("State: got %q, want StateSucceeded (err=%q)", r.State, r.Error)
	}
	if r.Output != "pong-from-real-provider" {
		t.Fatalf("Output: got %q, want pong-from-real-provider", r.Output)
	}
	if provider.GenerateCallCount() != 1 {
		t.Fatalf("GenerateCallCount: got %d, want 1 (provider was NOT actually invoked — bluff!)", provider.GenerateCallCount())
	}
}

func TestSubagentManager_DispatchSubprocess_RoutesToSubprocessSpawner(t *testing.T) {
	rec := newRecordingSpawner("subprocess-recording")
	m := newManagerForTest(t, SubagentManagerOptions{
		SubprocessSpawner: rec,
	})

	id, err := m.Dispatch(context.Background(), SubagentTask{
		Description: "worktree-routing",
		Prompt:      "anything",
		Isolation:   IsolationWorktree,
	})
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}

	results := drainAll(t, m, 1, 2*time.Second)
	if !rec.wasCalled() {
		t.Fatalf("expected subprocess spawner to be called, but recordingSpawner.wasCalled()=false")
	}
	if rec.recordedTask().ID != id {
		t.Fatalf("expected recordedTask.ID=%q, got %q", id, rec.recordedTask().ID)
	}
	if results[0].State != StateSucceeded {
		t.Fatalf("expected StateSucceeded, got %q", results[0].State)
	}
}

// =====================================================================
// Concurrency cap + semaphore
// =====================================================================

func TestSubagentManager_MaxConcurrency_RejectsBeyondLimit(t *testing.T) {
	provider := NewFakeLLMProvider(nil)
	provider.WithDelay(500 * time.Millisecond)

	m := newManagerForTest(t, SubagentManagerOptions{
		LLMProvider:    provider,
		MaxConcurrency: 2,
	})

	// First two should succeed (slots 1 and 2).
	for i := 0; i < 2; i++ {
		_, err := m.Dispatch(context.Background(), SubagentTask{
			Description: "slow",
			Prompt:      "x",
			Isolation:   IsolationNone,
		})
		if err != nil {
			t.Fatalf("Dispatch %d: %v", i, err)
		}
	}

	// Give goroutines a moment to register before the third dispatch.
	time.Sleep(50 * time.Millisecond)

	// Third should be rejected with ErrMaxConcurrency.
	_, err := m.Dispatch(context.Background(), SubagentTask{
		Description: "rejected",
		Prompt:      "x",
		Isolation:   IsolationNone,
	})
	if !errors.Is(err, ErrMaxConcurrency) {
		t.Fatalf("expected ErrMaxConcurrency, got %v", err)
	}

	// Drain the two in-flight results so cleanup doesn't deadlock.
	_ = drainAll(t, m, 2, 3*time.Second)
}

func TestSubagentManager_SemaphoreReleasedAfterCompletion(t *testing.T) {
	m := newManagerForTest(t, SubagentManagerOptions{
		MaxConcurrency: 1,
	})

	_, err := m.Dispatch(context.Background(), SubagentTask{
		Description: "first",
		Prompt:      "x",
		Isolation:   IsolationNone,
	})
	if err != nil {
		t.Fatalf("first Dispatch: %v", err)
	}

	// Drain the first result so the slot is released.
	_ = drainAll(t, m, 1, 2*time.Second)

	// Second dispatch should now succeed.
	_, err = m.Dispatch(context.Background(), SubagentTask{
		Description: "second",
		Prompt:      "x",
		Isolation:   IsolationNone,
	})
	if err != nil {
		t.Fatalf("second Dispatch (should succeed after first completed): %v", err)
	}
	_ = drainAll(t, m, 1, 2*time.Second)
}

// =====================================================================
// Kill + Status
// =====================================================================

func TestSubagentManager_KillCancelsRunning(t *testing.T) {
	provider := NewFakeLLMProvider(nil)
	provider.WithDelay(2 * time.Second)

	m := newManagerForTest(t, SubagentManagerOptions{
		LLMProvider: provider,
	})

	id, err := m.Dispatch(context.Background(), SubagentTask{
		ID:          "kill-target",
		Description: "to-be-killed",
		Prompt:      "slow",
		Isolation:   IsolationNone,
	})
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}

	// Give the goroutine time to start.
	time.Sleep(50 * time.Millisecond)

	if err := m.Kill(id); err != nil {
		t.Fatalf("Kill: %v", err)
	}

	results := drainAll(t, m, 1, 2*time.Second)
	if results[0].State != StateCanceled {
		t.Fatalf("expected StateCanceled, got %q (err=%q)", results[0].State, results[0].Error)
	}
	if results[0].TaskID != id {
		t.Fatalf("expected TaskID=%q, got %q", id, results[0].TaskID)
	}

	// Running map should no longer contain ID.
	for _, s := range m.Status() {
		if s.ID == id {
			t.Fatalf("expected ID %q to be removed from running map after kill, found in Status()", id)
		}
	}
}

func TestSubagentManager_KillUnknownErrors(t *testing.T) {
	m := newManagerForTest(t, SubagentManagerOptions{})

	if err := m.Kill("does-not-exist"); err == nil {
		t.Fatalf("expected error for unknown ID, got nil")
	}
}

func TestSubagentManager_StatusReflectsRunning(t *testing.T) {
	provider := NewFakeLLMProvider(nil)
	provider.WithDelay(500 * time.Millisecond)

	m := newManagerForTest(t, SubagentManagerOptions{
		LLMProvider: provider,
	})

	id, err := m.Dispatch(context.Background(), SubagentTask{
		Description: "status-test",
		Prompt:      "slow",
		Isolation:   IsolationNone,
	})
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}

	// Give the goroutine time to register in the running map.
	time.Sleep(30 * time.Millisecond)

	stat := m.Status()
	found := false
	for _, s := range stat {
		if s.ID == id {
			found = true
			if s.Description != "status-test" {
				t.Fatalf("expected Description=status-test, got %q", s.Description)
			}
			if s.Isolation != IsolationNone {
				t.Fatalf("expected Isolation=none, got %q", s.Isolation)
			}
			if s.StartedAt.IsZero() {
				t.Fatalf("expected non-zero StartedAt")
			}
			if s.Elapsed < 0 {
				t.Fatalf("expected non-negative Elapsed, got %v", s.Elapsed)
			}
			break
		}
	}
	if !found {
		t.Fatalf("expected Status() to contain id=%q, got %+v", id, stat)
	}

	// Drain the result and verify Status now empty for this id.
	_ = drainAll(t, m, 1, 2*time.Second)

	for _, s := range m.Status() {
		if s.ID == id {
			t.Fatalf("expected id=%q to be gone from Status() after completion", id)
		}
	}
}

// =====================================================================
// WaitAll
// =====================================================================

func TestSubagentManager_WaitAll_BlocksUntilAllComplete(t *testing.T) {
	m := newManagerForTest(t, SubagentManagerOptions{})

	ids := make([]string, 3)
	for i := 0; i < 3; i++ {
		id, err := m.Dispatch(context.Background(), SubagentTask{
			Description: "fast",
			Prompt:      "x",
			Isolation:   IsolationNone,
		})
		if err != nil {
			t.Fatalf("Dispatch %d: %v", i, err)
		}
		ids[i] = id
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	results, err := m.WaitAll(ctx, ids)
	if err != nil {
		t.Fatalf("WaitAll: %v", err)
	}
	if len(results) != 3 {
		t.Fatalf("expected 3 results, got %d", len(results))
	}

	gotIDs := make(map[string]bool)
	for _, r := range results {
		gotIDs[r.TaskID] = true
	}
	for _, id := range ids {
		if !gotIDs[id] {
			t.Fatalf("expected results to include id=%q", id)
		}
	}
}

// TestSubagentManager_WaitAll_RespectsCtxCancel proves WaitAll returns through
// its ctx.Done() branch when the caller's context is already cancelled.
//
// DETERMINISM (§11.4.50): the subagent is held inside the provider by a
// barrier, so the aggregator is PROVABLY empty and ctx.Done() is the only
// ready case in WaitAll's select — where a 2s provider delay merely made that
// likely, and an `elapsed > 500ms` bound then sampled the host scheduler to
// check it. The reason is now read off the return value: context.Canceled with
// an EMPTY result set can only come from the cancellation branch, because the
// aggregator branch always appends a result before looping. (Measured on this
// host at load 223/16 CPUs the old call took 1.6µs–17.6µs against its 500ms
// bound — a five-order-of-magnitude margin that existed purely to absorb
// scheduling noise.)
func TestSubagentManager_WaitAll_RespectsCtxCancel(t *testing.T) {
	provider := newGateProvider()
	t.Cleanup(provider.Release)

	m := newManagerForTest(t, SubagentManagerOptions{
		LLMProvider: provider,
	})

	id, err := m.Dispatch(context.Background(), SubagentTask{
		Description: "slow",
		Prompt:      "x",
		Isolation:   IsolationNone,
	})
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}

	// Barrier: while the subagent is parked in Generate it has published
	// nothing, so no result can be waiting in the aggregator.
	provider.AwaitEntered(t)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // already canceled

	results, err := m.WaitAll(ctx, []string{id})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
	if len(results) != 0 {
		t.Fatalf("WaitAll returned %d result(s) alongside context.Canceled — it did not return through the cancellation branch", len(results))
	}

	// Release the in-flight subagent and drain its result so shutdown doesn't
	// deadlock. The budget is a stuck-detector, not a latency assertion.
	provider.Release()
	_ = drainAll(t, m, 1, gateBudget)
}

// =====================================================================
// Shutdown
// =====================================================================

func TestSubagentManager_Shutdown_CancelsRunning(t *testing.T) {
	provider := NewFakeLLMProvider(nil)
	provider.WithDelay(2 * time.Second)

	m, err := NewSubagentManager(SubagentManagerOptions{
		LLMProvider:       provider,
		InProcessSpawner:  NewInProcessSpawner(),
		SubprocessSpawner: newRecordingSpawner("subprocess-recording"),
	})
	if err != nil {
		t.Fatalf("NewSubagentManager: %v", err)
	}

	id, err := m.Dispatch(context.Background(), SubagentTask{
		ID:          "shutdown-target",
		Description: "slow",
		Prompt:      "x",
		Isolation:   IsolationNone,
	})
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}

	time.Sleep(50 * time.Millisecond)

	// Capture aggregator BEFORE shutdown closes it.
	results := m.Results()

	if err := m.Shutdown(context.Background()); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}

	// Drain remaining results.
	got := []SubagentResult{}
	deadline := time.After(2 * time.Second)
	for {
		select {
		case r, ok := <-results:
			if !ok {
				goto done
			}
			got = append(got, r)
		case <-deadline:
			t.Fatalf("Shutdown drain timed out, got=%v", got)
		}
	}
done:
	found := false
	for _, r := range got {
		if r.TaskID == id && r.State == StateCanceled {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected id=%q with StateCanceled, got %+v", id, got)
	}
}

func TestSubagentManager_Shutdown_ClosesAggregator(t *testing.T) {
	m, err := NewSubagentManager(SubagentManagerOptions{
		LLMProvider:       NewFakeLLMProvider(nil),
		InProcessSpawner:  NewInProcessSpawner(),
		SubprocessSpawner: newRecordingSpawner("subprocess-recording"),
	})
	if err != nil {
		t.Fatalf("NewSubagentManager: %v", err)
	}

	results := m.Results()

	if err := m.Shutdown(context.Background()); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}

	select {
	case _, ok := <-results:
		if ok {
			// Drain any remaining values, then expect close.
			for range results {
			}
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("expected Results channel to close after Shutdown")
	}
}

func TestSubagentManager_Shutdown_Idempotent(t *testing.T) {
	m, err := NewSubagentManager(SubagentManagerOptions{
		LLMProvider:       NewFakeLLMProvider(nil),
		InProcessSpawner:  NewInProcessSpawner(),
		SubprocessSpawner: newRecordingSpawner("subprocess-recording"),
	})
	if err != nil {
		t.Fatalf("NewSubagentManager: %v", err)
	}

	if err := m.Shutdown(context.Background()); err != nil {
		t.Fatalf("first Shutdown: %v", err)
	}
	// Second call must not panic and must return nil.
	if err := m.Shutdown(context.Background()); err != nil {
		t.Fatalf("second Shutdown: %v", err)
	}
}

// =====================================================================
// Validation
// =====================================================================

func TestSubagentManager_TimeoutClampedToCeiling(t *testing.T) {
	rec := newRecordingSpawner("inproc-recording")
	m := newManagerForTest(t, SubagentManagerOptions{
		InProcessSpawner: rec,
	})

	_, err := m.Dispatch(context.Background(), SubagentTask{
		Description: "long-timeout",
		Prompt:      "x",
		Isolation:   IsolationNone,
		Timeout:     1 * time.Hour,
	})
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}

	_ = drainAll(t, m, 1, 2*time.Second)

	got := rec.recordedTask().Timeout
	if got != HardTaskTimeoutCeiling {
		t.Fatalf("expected Timeout clamped to %v, got %v", HardTaskTimeoutCeiling, got)
	}
}

func TestSubagentManager_DefaultTimeoutAppliedWhenZero(t *testing.T) {
	rec := newRecordingSpawner("inproc-recording")
	m := newManagerForTest(t, SubagentManagerOptions{
		InProcessSpawner: rec,
		DefaultTimeout:   123 * time.Second,
	})

	_, err := m.Dispatch(context.Background(), SubagentTask{
		Description: "zero-timeout",
		Prompt:      "x",
		Isolation:   IsolationNone,
	})
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}

	_ = drainAll(t, m, 1, 2*time.Second)

	got := rec.recordedTask().Timeout
	if got != 123*time.Second {
		t.Fatalf("expected Timeout=123s (default), got %v", got)
	}
}

func TestSubagentManager_ValidatesIsolation(t *testing.T) {
	m := newManagerForTest(t, SubagentManagerOptions{})

	_, err := m.Dispatch(context.Background(), SubagentTask{
		Description: "bogus-isolation",
		Prompt:      "x",
		Isolation:   Isolation("bogus"),
	})
	if !errors.Is(err, ErrUnknownIsolation) {
		t.Fatalf("expected ErrUnknownIsolation, got %v", err)
	}
}

func TestSubagentManager_ValidatesEmptyPrompt(t *testing.T) {
	m := newManagerForTest(t, SubagentManagerOptions{})

	_, err := m.Dispatch(context.Background(), SubagentTask{
		Description: "no-prompt",
		Prompt:      "",
		Isolation:   IsolationNone,
	})
	if err == nil {
		t.Fatalf("expected error for empty prompt, got nil")
	}
}

func TestSubagentManager_ValidatesEmptyDescription(t *testing.T) {
	m := newManagerForTest(t, SubagentManagerOptions{})

	_, err := m.Dispatch(context.Background(), SubagentTask{
		Description: "",
		Prompt:      "x",
		Isolation:   IsolationNone,
	})
	if err == nil {
		t.Fatalf("expected error for empty description, got nil")
	}
}

// =====================================================================
// Results channel idempotency
// =====================================================================

func TestSubagentManager_Results_Idempotent(t *testing.T) {
	m := newManagerForTest(t, SubagentManagerOptions{})
	a := m.Results()
	b := m.Results()
	if a != b {
		t.Fatalf("expected Results() to return the same channel on repeated calls")
	}
}
