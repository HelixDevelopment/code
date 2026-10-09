package planner

// Standing regression guards (§11.4.135) for two reproduced defects in the
// SequentialExecutor, each with §11.4.115 RED_MODE polarity:
//
//   RED_MODE=1  reproduces the historical defect on a faithful pre-fix
//               stand-in and asserts the WRONG (defective) behaviour — this
//               is the captured proof the guard exercises a real bug.
//   RED_MODE=0  (default) drives the REAL fixed code and asserts the defect
//               is ABSENT.
//
// Defect 1 (DEF-PLANNER-CTXCANCEL): ExecuteStep's retry loop ignored
//   parent-context cancellation — a cancelled/expired parent ctx burned
//   through MaxRetries with exponential time.Sleep backoff (6 calls / ~31 s
//   observed) instead of aborting immediately.
//
// Defect 2 (DEF-PLANNER-UTF8TRUNC): sanitizeOutput truncated step output on
//   a byte index (output[:maxLen]), splitting multi-byte UTF-8 runes and
//   producing invalid UTF-8 that json.Marshal silently rewrites to U+FFFD.

import (
	"context"
	"errors"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"
	"unicode/utf8"
)

func redMode() bool { return os.Getenv("RED_MODE") == "1" }

// preFixSanitizeOutput is the faithful pre-fix algorithm — a plain byte-index
// slice. Inlined so RED_MODE=1 can reproduce the historical corruption.
func preFixSanitizeOutput(output string, maxLen int) string {
	if len(output) > maxLen {
		output = output[:maxLen]
	}
	return output
}

// TestGuard_SanitizeOutput_UTF8Boundary is the standing regression guard for
// DEF-PLANNER-UTF8TRUNC.
func TestGuard_SanitizeOutput_UTF8Boundary(t *testing.T) {
	// "héllo": 'é' = 0xC3 0xA9 (2 bytes). Truncating to 2 bytes lands
	// mid-rune ("h" + 0xC3), the canonical multi-byte split.
	const in = "héllo"
	const maxLen = 2

	if redMode() {
		// Reproduce the defect on the pre-fix stand-in: invalid UTF-8.
		got := preFixSanitizeOutput(in, maxLen)
		if utf8.ValidString(got) {
			t.Fatalf("RED_MODE: expected pre-fix algorithm to produce INVALID UTF-8 for %q[:%d], got valid %q", in, maxLen, got)
		}
		t.Logf("RED_MODE reproduced defect: pre-fix output %q is invalid UTF-8", got)
		return
	}

	// GREEN: the real fixed code must never emit invalid UTF-8.
	got := sanitizeOutput(in, maxLen)
	if !utf8.ValidString(got) {
		t.Fatalf("sanitizeOutput(%q, %d) = %q which is INVALID UTF-8 (defect DEF-PLANNER-UTF8TRUNC reintroduced)", in, maxLen, got)
	}
	// It must drop the partial rune entirely, yielding "h".
	if got != "h" {
		t.Fatalf("sanitizeOutput(%q, %d) = %q, want %q (rune-boundary truncation)", in, maxLen, got, "h")
	}
}

// TestGuard_SanitizeOutput_PureASCII confirms the rune-safe fix did not
// regress the common ASCII path (still truncates to exactly maxLen bytes).
func TestGuard_SanitizeOutput_PureASCII(t *testing.T) {
	if redMode() {
		t.Skip("RED_MODE: ASCII path is not the reproduced defect") // SKIP-OK: polarity guard, ASCII never broke
	}
	got := sanitizeOutput("abcdef", 3)
	if got != "abc" {
		t.Fatalf("sanitizeOutput(\"abcdef\", 3) = %q, want \"abc\"", got)
	}
}

// preFixExecuteStep is the faithful pre-fix retry loop: no parent-ctx guard,
// blocking time.Sleep backoff. Inlined so RED_MODE=1 can reproduce the
// retry-storm against a cancelled context without time-travel.
//
// It uses a 1ms backoff base (instead of the production 1s) so the RED
// reproduction completes quickly while still proving the loop does NOT abort
// on a cancelled parent context.
func preFixExecuteStep(ctx context.Context, runner ShellRunner, step *TaskStep) {
	for attempt := 0; attempt <= step.MaxRetries; attempt++ {
		if attempt > 0 {
			step.RetryCount = attempt
			time.Sleep(time.Duration(1<<uint(attempt-1)) * time.Millisecond)
		}
		stepCtx, cancel := context.WithTimeout(ctx, step.Timeout)
		_, _ = runner(stepCtx, step.Command)
		cancel()
	}
}

// TestGuard_ExecuteStep_AbortsOnCancelledParentCtx is the standing regression
// guard for DEF-PLANNER-CTXCANCEL.
func TestGuard_ExecuteStep_AbortsOnCancelledParentCtx(t *testing.T) {
	const maxRetries = 5

	if redMode() {
		// Reproduce on the pre-fix stand-in: the loop keeps invoking the
		// runner despite an already-cancelled parent ctx.
		calls := 0
		runner := func(_ context.Context, _ string) (string, error) {
			calls++
			return "", errors.New("always fail")
		}
		ctx, cancel := context.WithCancel(context.Background())
		cancel() // cancelled before execution
		step := &TaskStep{Type: StepShell, Command: "fail", Status: StepPending, MaxRetries: maxRetries, Timeout: time.Second}
		preFixExecuteStep(ctx, runner, step)
		if calls <= 1 {
			t.Fatalf("RED_MODE: expected pre-fix loop to keep retrying despite cancelled ctx, got calls=%d", calls)
		}
		t.Logf("RED_MODE reproduced defect: pre-fix loop called runner %d times on a cancelled ctx", calls)
		return
	}

	// GREEN: the real fixed ExecuteStep must abort immediately — exactly
	// zero (or at most one) runner invocations and no exponential sleeps.
	calls := 0
	runner := func(_ context.Context, _ string) (string, error) {
		calls++
		return "", errors.New("always fail")
	}
	executor := NewSequentialExecutor(runner)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	step := &TaskStep{Type: StepShell, Command: "fail", Status: StepPending, MaxRetries: maxRetries, Timeout: time.Second}

	err := executor.ExecuteStep(ctx, step)

	if err == nil {
		t.Fatal("ExecuteStep on a cancelled parent ctx must return an error")
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("ExecuteStep error = %v, want context.Canceled", err)
	}
	// A pre-cancelled ctx must abort BEFORE the first runner call: the runtime
	// signature of the fix is calls == 0. `!= 0` (not `> 1`) so a partial revert
	// that drops only the top-of-loop ctx check (leaving calls == 1) is caught.
	if calls != 0 {
		t.Fatalf("ExecuteStep invoked the runner %d times on a cancelled ctx (defect DEF-PLANNER-CTXCANCEL reintroduced); want 0", calls)
	}
	// No exponential-backoff sleep may have been STARTED. Read that off the
	// executor's own state rather than off the clock: step.RetryCount is
	// written at the top of the `attempt > 0` branch, immediately before the
	// backoff timer is armed, so RetryCount == 0 is a positive witness that no
	// retry iteration — and therefore no backoff sleep — was ever entered.
	//
	// DETERMINISM (§11.4.50): this replaces an `elapsed > 500ms` wall-clock
	// bound. That bound asserted the same property through a quantity the code
	// under test does not control: on an oversubscribed host the scheduler,
	// not ExecuteStep, decided whether it held. Measured on this host at load
	// 223/16 CPUs the real abort took 8.8µs–49µs against a 500ms bound — a
	// ~10000x margin whose only job was to absorb scheduling noise.
	if step.RetryCount != 0 {
		t.Fatalf("step.RetryCount = %d — a retry iteration (and with it a backoff sleep) was entered on a pre-cancelled ctx; want 0", step.RetryCount)
	}
	if step.Status != StepFailed {
		t.Fatalf("step.Status = %v, want StepFailed after cancellation", step.Status)
	}
}

// backoffEntryProbe wraps a context and signals the FIRST consultation of
// Done() that happens after the probe is armed. The executor consults Done()
// exactly once when it enters the inter-retry backoff `select` (and once per
// context.WithTimeout it derives per attempt), so arming the probe as the
// first attempt returns makes "the executor is now waiting between retries" an
// observable EVENT. That is what lets the test below cancel at a precise point
// in the executor's control flow with no sleep and no scheduling assumption.
//
// Arming and probing both happen on the executor's own goroutine (the runner
// callback and Done() are called by it), so `armed` needs no synchronisation
// beyond atomicity for the reader.
type backoffEntryProbe struct {
	context.Context
	armed  *atomic.Bool
	signal chan<- struct{}
}

func (c backoffEntryProbe) Done() <-chan struct{} {
	if c.armed.Load() {
		select {
		case c.signal <- struct{}{}:
		default: // already signalled; later consultations are not interesting
		}
	}
	// MUST return the underlying channel unchanged: the caller's select
	// captures this exact channel, and cancel() is what closes it.
	return c.Context.Done()
}

// TestGuard_ExecuteStep_AbortsOnCancelDuringBackoff proves the inter-retry
// wait is context-aware: a parent ctx cancelled WHILE the executor is waiting
// between retries must abandon the retry cycle rather than run the next
// attempt once the interval elapses.
//
// DETERMINISM (§11.4.50) — this guard was rebuilt to remove its wall-clock
// dependence. What it used to do: sleep 100ms, cancel, and assert the call
// returned within 2s. Both halves were scheduler-decided — the 100ms sleep
// only *hoped* to land inside the backoff window, and the 2s bound sampled how
// promptly a loaded host rescheduled the goroutine (measured on this host at
// load 223/16 CPUs: 100.3ms–120.7ms against the 2s bound, of which 100ms was
// the test's own sleep).
//
// What it does now — the discriminator is the RUNNER CALL COUNT, not the clock.
// Each iteration of the retry loop runs: top-of-loop ctx check → backoff wait →
// runner. For a cancellation delivered while the executor is in the backoff
// wait:
//
//	context-aware wait      → the ctx.Done() branch returns immediately  → 1 call
//	plain time.Sleep(back)  → the wait finishes, attempt 1's runner RUNS,
//	                          and only the NEXT top-of-loop check aborts  → 2 calls
//
// The cancel is ordered into that window by backoffEntryProbe rather than by a
// sleep, so the observation is exact: exactly one runner call is positive
// evidence the retry cycle was abandoned mid-wait.
func TestGuard_ExecuteStep_AbortsOnCancelDuringBackoff(t *testing.T) {
	if redMode() {
		t.Skip("RED_MODE: covered by the cancelled-before-exec reproduction above") // SKIP-OK: same defect class
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var armed atomic.Bool
	enteredBackoff := make(chan struct{}, 1)

	attempts := 0
	runner := func(_ context.Context, _ string) (string, error) {
		attempts++
		// Arm as the first attempt returns, so the next Done() consultation —
		// the backoff wait the executor is about to enter — is the one
		// reported. Same goroutine as the Done() call below it.
		armed.Store(true)
		return "", errors.New("transient")
	}
	executor := NewSequentialExecutor(runner)

	// The canceller fires the moment the executor enters the backoff wait.
	// stop + WaitGroup guarantee it is reaped even if that never happens (a
	// regression that skips the wait entirely), so no goroutine outlives the
	// test.
	stop := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		select {
		case <-enteredBackoff:
			cancel()
		case <-stop:
		}
	}()
	t.Cleanup(func() { close(stop); wg.Wait() })

	step := &TaskStep{Type: StepShell, Command: "fail", Status: StepPending, MaxRetries: 5, Timeout: time.Second}
	err := executor.ExecuteStep(backoffEntryProbe{Context: ctx, armed: &armed, signal: enteredBackoff}, step)

	if !errors.Is(err, context.Canceled) {
		t.Fatalf("ExecuteStep error = %v, want context.Canceled", err)
	}
	// THE discriminator. Two calls means the executor completed its wait and
	// ran the next attempt before noticing the cancellation — i.e. the wait
	// was not context-aware (defect reintroduced).
	if attempts != 1 {
		t.Fatalf("runner was invoked %d times — the executor finished its inter-retry wait and ran another attempt instead of aborting when the ctx was cancelled during the wait (defect reintroduced); want 1", attempts)
	}
	if step.Status != StepFailed {
		t.Fatalf("step.Status = %v, want StepFailed after cancellation", step.Status)
	}
	// The abort must be ATTRIBUTED to the cancellation, not to retry
	// exhaustion that happened to coincide with it.
	if step.Error != context.Canceled.Error() {
		t.Fatalf("step.Error = %q, want %q — the abort was not attributed to the cancellation", step.Error, context.Canceled.Error())
	}
}
