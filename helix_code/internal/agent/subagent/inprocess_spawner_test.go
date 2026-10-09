package subagent

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"dev.helix.code/internal/llm"
)

// drainOne receives the single result the spawner is contracted to send,
// asserting that the channel was non-nil and the receive completed within
// `timeout`. It also verifies the channel closes immediately after the result.
func drainOne(t *testing.T, ch <-chan SubagentResult, timeout time.Duration) SubagentResult {
	t.Helper()
	if ch == nil {
		t.Fatalf("drainOne: channel was nil")
	}
	select {
	case res, ok := <-ch:
		if !ok {
			t.Fatalf("drainOne: channel closed before sending a result")
		}
		return res
	case <-time.After(timeout):
		t.Fatalf("drainOne: timed out after %v waiting for result", timeout)
	}
	return SubagentResult{}
}

// gateBudget bounds how long a test waits for the other side of a
// gateProvider barrier. It is NOT a pass/fail threshold: the correct-behaviour
// path crosses each barrier in microseconds, and the budget exists only so a
// genuinely stuck subagent reports a cause instead of hanging the suite. Its
// expiry is always a loud, explanatory FAIL.
const gateBudget = 30 * time.Second

// gateProvider is a TEST-ONLY llm.Provider whose Generate PARKS until the test
// releases it or the call context ends. It is the deterministic replacement
// for FakeLLMProvider.WithDelay in tests whose subject is cancellation or
// timeout rather than latency (§11.4.50).
//
// Why a barrier beats a delay: "block for 2s so the result cannot have
// arrived yet" is only true if the host schedules the test goroutine within
// those 2s. Parking until an explicit release makes the same precondition a
// structural fact — while a subagent sits inside Generate it provably has not
// produced a result, at any host load. And because the park can ONLY end via
// ctx, a timeout/cancellation state in the result is positive evidence that
// the ctx path fired, rather than an inference from elapsed time.
type gateProvider struct {
	entered   chan struct{} // closed on first Generate entry
	release   chan struct{} // closed by Release
	enterOnce sync.Once
	relOnce   sync.Once
}

func newGateProvider() *gateProvider {
	return &gateProvider{entered: make(chan struct{}), release: make(chan struct{})}
}

// AwaitEntered blocks until Generate has been entered — i.e. the subagent is
// provably in flight and cannot yet have published a result.
func (p *gateProvider) AwaitEntered(t *testing.T) {
	t.Helper()
	select {
	case <-p.entered:
	case <-time.After(gateBudget):
		t.Fatalf("gateProvider: Generate was never entered within %s — the subagent never reached the provider (barrier budget expired; this is not a latency assertion)", gateBudget)
	}
}

// Release unparks Generate. Idempotent, so it is safe both as a t.Cleanup and
// as an explicit in-test call.
func (p *gateProvider) Release() { p.relOnce.Do(func() { close(p.release) }) }

func (p *gateProvider) GetType() llm.ProviderType              { return llm.ProviderType("test-gate-only") }
func (p *gateProvider) GetName() string                        { return "Gate Test Provider" }
func (p *gateProvider) GetModels() []llm.ModelInfo             { return nil }
func (p *gateProvider) GetCapabilities() []llm.ModelCapability { return nil }
func (p *gateProvider) Generate(ctx context.Context, req *llm.LLMRequest) (*llm.LLMResponse, error) {
	p.enterOnce.Do(func() { close(p.entered) })
	select {
	case <-p.release:
		return &llm.LLMResponse{Content: "GATE-PROVIDER-RELEASED"}, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}
func (p *gateProvider) GenerateStream(ctx context.Context, req *llm.LLMRequest, ch chan<- llm.LLMResponse) error {
	resp, err := p.Generate(ctx, req)
	if ch != nil {
		defer close(ch)
	}
	if err != nil {
		return err
	}
	if ch != nil {
		select {
		case ch <- *resp:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return nil
}
func (p *gateProvider) IsAvailable(ctx context.Context) bool                       { return true }
func (p *gateProvider) GetHealth(ctx context.Context) (*llm.ProviderHealth, error) { return nil, nil }
func (p *gateProvider) Close() error                                               { return nil }
func (p *gateProvider) GetContextWindow() int                                      { return 1024 }
func (p *gateProvider) CountTokens(text string) (int, error)                       { return len(text) / 4, nil }

// errProvider is a TEST-ONLY llm.Provider that returns a fixed error from
// Generate. It is a hexagonal seam for the spawner test, NOT a production
// stub.
type errProvider struct {
	err error
}

func (p *errProvider) GetType() llm.ProviderType              { return llm.ProviderType("test-err-only") }
func (p *errProvider) GetName() string                        { return "Err Test Provider" }
func (p *errProvider) GetModels() []llm.ModelInfo             { return nil }
func (p *errProvider) GetCapabilities() []llm.ModelCapability { return nil }
func (p *errProvider) Generate(ctx context.Context, req *llm.LLMRequest) (*llm.LLMResponse, error) {
	return nil, p.err
}
func (p *errProvider) GenerateStream(ctx context.Context, req *llm.LLMRequest, ch chan<- llm.LLMResponse) error {
	return p.err
}
func (p *errProvider) IsAvailable(ctx context.Context) bool                       { return true }
func (p *errProvider) GetHealth(ctx context.Context) (*llm.ProviderHealth, error) { return nil, nil }
func (p *errProvider) Close() error                                               { return nil }
func (p *errProvider) GetContextWindow() int                                      { return 1024 }
func (p *errProvider) CountTokens(text string) (int, error)                       { return len(text) / 4, nil }

// panicProvider is a TEST-ONLY llm.Provider that panics from Generate.
type panicProvider struct{}

func (p *panicProvider) GetType() llm.ProviderType              { return llm.ProviderType("test-panic-only") }
func (p *panicProvider) GetName() string                        { return "Panic Test Provider" }
func (p *panicProvider) GetModels() []llm.ModelInfo             { return nil }
func (p *panicProvider) GetCapabilities() []llm.ModelCapability { return nil }
func (p *panicProvider) Generate(ctx context.Context, req *llm.LLMRequest) (*llm.LLMResponse, error) {
	panic("intentional test panic from panicProvider")
}
func (p *panicProvider) GenerateStream(ctx context.Context, req *llm.LLMRequest, ch chan<- llm.LLMResponse) error {
	return nil
}
func (p *panicProvider) IsAvailable(ctx context.Context) bool                       { return true }
func (p *panicProvider) GetHealth(ctx context.Context) (*llm.ProviderHealth, error) { return nil, nil }
func (p *panicProvider) Close() error                                               { return nil }
func (p *panicProvider) GetContextWindow() int                                      { return 1024 }
func (p *panicProvider) CountTokens(text string) (int, error)                       { return len(text) / 4, nil }

func TestInProcessSpawner_Kind(t *testing.T) {
	s := NewInProcessSpawner()
	if s.Kind() != "in-process" {
		t.Fatalf("expected Kind()=in-process, got %q", s.Kind())
	}
}

func TestInProcessSpawner_NilProviderReturnsError(t *testing.T) {
	s := NewInProcessSpawner()
	ch, err := s.Spawn(context.Background(), SubagentTask{Prompt: "x"}, nil)
	if err == nil {
		t.Fatalf("expected error for nil provider, got nil")
	}
	if ch != nil {
		t.Fatalf("expected nil channel for nil provider, got non-nil")
	}
}

func TestInProcessSpawner_RealProviderInvocation(t *testing.T) {
	provider := NewFakeLLMProvider(nil)
	provider.SetCanned("test-prompt", "canned-response-42")

	s := NewInProcessSpawner()
	ch, err := s.Spawn(context.Background(), SubagentTask{
		ID:     "task-real",
		Prompt: "test-prompt",
	}, provider)
	if err != nil {
		t.Fatalf("Spawn returned error: %v", err)
	}

	res := drainOne(t, ch, 2*time.Second)

	if res.State != StateSucceeded {
		t.Fatalf("expected StateSucceeded, got %q (err=%q)", res.State, res.Error)
	}
	if res.Output != "canned-response-42" {
		t.Fatalf("expected canned response, got %q", res.Output)
	}
	if got := provider.GenerateCallCount(); got != 1 {
		t.Fatalf("expected GenerateCallCount=1, got %d (provider was NOT actually invoked — bluff!)", got)
	}
	if got := provider.LastPrompt(); got != "test-prompt" {
		t.Fatalf("expected LastPrompt=test-prompt, got %q", got)
	}
	if res.TaskID != "task-real" {
		t.Fatalf("expected TaskID=task-real, got %q", res.TaskID)
	}
}

func TestInProcessSpawner_FallbackEchoCapturesPrompt(t *testing.T) {
	provider := NewFakeLLMProvider(nil)

	s := NewInProcessSpawner()
	ch, err := s.Spawn(context.Background(), SubagentTask{
		ID:     "task-echo",
		Prompt: "echo-me",
	}, provider)
	if err != nil {
		t.Fatalf("Spawn returned error: %v", err)
	}

	res := drainOne(t, ch, 2*time.Second)

	if res.State != StateSucceeded {
		t.Fatalf("expected StateSucceeded, got %q (err=%q)", res.State, res.Error)
	}
	if !strings.HasPrefix(res.Output, "FAKE-LLM-ECHO: ") {
		t.Fatalf("expected output starting with FAKE-LLM-ECHO:, got %q (output may be fabricated without invoking provider)", res.Output)
	}
	if !strings.Contains(res.Output, "echo-me") {
		t.Fatalf("expected output to contain prompt 'echo-me', got %q", res.Output)
	}
	if got := provider.GenerateCallCount(); got != 1 {
		t.Fatalf("expected GenerateCallCount=1, got %d", got)
	}
}

// TestInProcessSpawner_TimeoutEnforced proves the per-task timeout is
// enforced and is what ended the call.
//
// DETERMINISM (§11.4.50): the gate is never released, so Generate can ONLY
// return through its context. A StateTimedOut result naming DeadlineExceeded
// is therefore positive evidence that the per-task deadline fired — if the
// timeout were not enforced the provider would still be parked and drainOne's
// budget would expire with an explicit failure, rather than the test silently
// passing on a lucky elapsed time. This replaces a `res.Duration > 800ms`
// bound, which sampled host scheduling: measured on this host at load 223/16
// CPUs the 50ms timeout produced Durations of 50.2ms–56.7ms, i.e. the bound's
// entire 750ms of slack existed only to absorb scheduling noise.
func TestInProcessSpawner_TimeoutEnforced(t *testing.T) {
	provider := newGateProvider()
	t.Cleanup(provider.Release)

	s := NewInProcessSpawner()
	ch, err := s.Spawn(context.Background(), SubagentTask{
		ID:      "task-timeout",
		Prompt:  "anything",
		Timeout: 50 * time.Millisecond,
	}, provider)
	if err != nil {
		t.Fatalf("Spawn returned error: %v", err)
	}

	provider.AwaitEntered(t) // the call is provably in flight and parked

	res := drainOne(t, ch, gateBudget)

	if res.State != StateTimedOut {
		t.Fatalf("expected StateTimedOut, got %q (err=%q)", res.State, res.Error)
	}
	if res.Duration <= 0 {
		t.Fatalf("expected positive Duration, got %v", res.Duration)
	}
	// The REASON the parked call ended: the per-task deadline, not a parent
	// cancellation and not a provider error.
	if !strings.Contains(res.Error, context.DeadlineExceeded.Error()) {
		t.Fatalf("res.Error = %q, want it to name %q — the abort was not attributed to the per-task deadline", res.Error, context.DeadlineExceeded)
	}
}

func TestInProcessSpawner_CtxCancelPropagates(t *testing.T) {
	provider := newGateProvider()
	t.Cleanup(provider.Release)

	ctx, cancel := context.WithCancel(context.Background())
	s := NewInProcessSpawner()
	ch, err := s.Spawn(ctx, SubagentTask{
		ID:     "task-cancel",
		Prompt: "anything",
	}, provider)
	if err != nil {
		t.Fatalf("Spawn returned error: %v", err)
	}

	// Barrier, not a sleep: waiting for Generate to be entered makes "the call
	// is in flight when we cancel" a structural fact rather than a 20ms bet on
	// the host scheduler (§11.4.50). No task Timeout is set, so the parked
	// call can only end via this cancellation.
	provider.AwaitEntered(t)
	cancel()

	res := drainOne(t, ch, gateBudget)

	if res.State != StateCanceled {
		t.Fatalf("expected StateCanceled, got %q (err=%q)", res.State, res.Error)
	}
	if !strings.Contains(res.Error, context.Canceled.Error()) {
		t.Fatalf("res.Error = %q, want it to name %q — the abort was not attributed to the parent cancellation", res.Error, context.Canceled)
	}
}

func TestInProcessSpawner_ProviderErrorBecomesFailedState(t *testing.T) {
	provider := &errProvider{err: errors.New("provider-failure-xyz")}

	s := NewInProcessSpawner()
	ch, err := s.Spawn(context.Background(), SubagentTask{
		ID:     "task-err",
		Prompt: "x",
	}, provider)
	if err != nil {
		t.Fatalf("Spawn returned error: %v", err)
	}

	res := drainOne(t, ch, 2*time.Second)

	if res.State != StateFailed {
		t.Fatalf("expected StateFailed, got %q", res.State)
	}
	if !strings.Contains(res.Error, "provider-failure-xyz") {
		t.Fatalf("expected error to contain provider message, got %q", res.Error)
	}
}

func TestInProcessSpawner_ProviderPanicCapturedAsFailed(t *testing.T) {
	provider := &panicProvider{}

	s := NewInProcessSpawner()
	ch, err := s.Spawn(context.Background(), SubagentTask{
		ID:     "task-panic",
		Prompt: "x",
	}, provider)
	if err != nil {
		t.Fatalf("Spawn returned error: %v", err)
	}

	res := drainOne(t, ch, 2*time.Second)

	if res.State != StateFailed {
		t.Fatalf("expected StateFailed, got %q", res.State)
	}
	if !strings.Contains(res.Error, "panic") {
		t.Fatalf("expected error mentioning 'panic', got %q", res.Error)
	}
}

func TestInProcessSpawner_ChannelClosesAfterResult(t *testing.T) {
	provider := NewFakeLLMProvider(nil)
	s := NewInProcessSpawner()
	ch, err := s.Spawn(context.Background(), SubagentTask{
		ID:     "task-close",
		Prompt: "x",
	}, provider)
	if err != nil {
		t.Fatalf("Spawn returned error: %v", err)
	}

	first, ok := <-ch
	if !ok {
		t.Fatalf("expected first receive to succeed")
	}
	if first.State != StateSucceeded {
		t.Fatalf("expected StateSucceeded, got %q", first.State)
	}

	// Second receive should return zero-value with ok=false (channel closed).
	second, ok := <-ch
	if ok {
		t.Fatalf("expected channel to be closed after first result, got value=%+v", second)
	}
	if second.State != "" {
		t.Fatalf("expected zero-value SubagentResult, got %+v", second)
	}
}

func TestInProcessSpawner_DurationPopulated(t *testing.T) {
	provider := NewFakeLLMProvider(nil)
	s := NewInProcessSpawner()
	ch, err := s.Spawn(context.Background(), SubagentTask{
		ID:     "task-dur",
		Prompt: "x",
	}, provider)
	if err != nil {
		t.Fatalf("Spawn returned error: %v", err)
	}
	res := drainOne(t, ch, 2*time.Second)
	if res.Duration <= 0 {
		t.Fatalf("expected positive Duration, got %v", res.Duration)
	}
}

func TestInProcessSpawner_StartedAtAndCompletedAt_Sane(t *testing.T) {
	provider := NewFakeLLMProvider(nil)
	s := NewInProcessSpawner()
	ch, err := s.Spawn(context.Background(), SubagentTask{
		ID:     "task-times",
		Prompt: "x",
	}, provider)
	if err != nil {
		t.Fatalf("Spawn returned error: %v", err)
	}
	res := drainOne(t, ch, 2*time.Second)

	if res.StartedAt.IsZero() {
		t.Fatalf("expected non-zero StartedAt")
	}
	if res.CompletedAt.IsZero() {
		t.Fatalf("expected non-zero CompletedAt")
	}
	if res.CompletedAt.Before(res.StartedAt) {
		t.Fatalf("CompletedAt (%v) is before StartedAt (%v)", res.CompletedAt, res.StartedAt)
	}
	measured := res.CompletedAt.Sub(res.StartedAt)
	// Tolerate a tiny skew between the two clock reads & the recorded duration.
	skew := measured - res.Duration
	if skew < -50*time.Millisecond || skew > 50*time.Millisecond {
		t.Fatalf("Duration (%v) does not roughly match CompletedAt-StartedAt (%v)", res.Duration, measured)
	}
}

func TestInProcessSpawner_ConcurrentSpawnsIndependent(t *testing.T) {
	provider := NewFakeLLMProvider(nil)
	provider.SetCanned("p1", "r1")
	provider.SetCanned("p2", "r2")
	provider.SetCanned("p3", "r3")

	s := NewInProcessSpawner()

	type pair struct {
		id     string
		prompt string
		want   string
	}
	tasks := []pair{
		{id: "t1", prompt: "p1", want: "r1"},
		{id: "t2", prompt: "p2", want: "r2"},
		{id: "t3", prompt: "p3", want: "r3"},
	}

	var wg sync.WaitGroup
	results := make(chan SubagentResult, len(tasks))

	for _, tt := range tasks {
		tt := tt
		wg.Add(1)
		go func() {
			defer wg.Done()
			ch, err := s.Spawn(context.Background(), SubagentTask{
				ID:     tt.id,
				Prompt: tt.prompt,
			}, provider)
			if err != nil {
				t.Errorf("Spawn returned error for %s: %v", tt.id, err)
				return
			}
			res := drainOne(t, ch, 2*time.Second)
			results <- res
		}()
	}

	wg.Wait()
	close(results)

	got := map[string]string{}
	for r := range results {
		if r.State != StateSucceeded {
			t.Fatalf("expected StateSucceeded for %s, got %q (err=%q)", r.TaskID, r.State, r.Error)
		}
		got[r.TaskID] = r.Output
	}

	for _, tt := range tasks {
		if got[tt.id] != tt.want {
			t.Fatalf("task %s: expected output %q, got %q", tt.id, tt.want, got[tt.id])
		}
	}
	if int64(len(tasks)) != provider.GenerateCallCount() {
		t.Fatalf("expected %d Generate calls, got %d", len(tasks), provider.GenerateCallCount())
	}
}
