package shell

import (
	"context"
	"fmt"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSimpleCommandExecution tests basic command execution
func TestSimpleCommandExecution(t *testing.T) {
	executor := NewShellExecutor(DefaultConfig())

	cmd := &Command{
		ID:      "test-simple",
		Command: "echo 'Hello, World!'",
	}

	result, err := executor.Execute(context.Background(), cmd)
	require.NoError(t, err)
	assert.Equal(t, 0, result.ExitCode)
	assert.Contains(t, result.Stdout, "Hello, World!")
	assert.Empty(t, result.Stderr)
}

// TestCommandWithExitCode tests command with non-zero exit code
func TestCommandWithExitCode(t *testing.T) {
	executor := NewShellExecutor(DefaultConfig())

	cmd := &Command{
		ID:      "test-exit",
		Command: "exit 42",
	}

	result, err := executor.Execute(context.Background(), cmd)
	require.NoError(t, err)
	assert.Equal(t, 42, result.ExitCode)
}

// TestCommandTimeout tests command timeout
func TestCommandTimeout(t *testing.T) {
	executor := NewShellExecutor(DefaultConfig())

	cmd := &Command{
		ID:      "test-timeout",
		Command: "sleep 10",
		Timeout: 500 * time.Millisecond,
	}

	result, err := executor.Execute(context.Background(), cmd)
	assert.NoError(t, err)

	// DETERMINISM (§11.4.50): assert the REASON the command ended, not how
	// long it took. `sleep 10` allowed to finish reports TimedOut=false,
	// Killed=false, OutputIncomplete=false and ExitCode 0; all three flags
	// below are set only on the timeout path — the timeout manager fires at
	// cmd.Timeout and cancels the exec context, and that cancellation branch
	// SIGKILLs the child and marks its output truncated. Together they are
	// positive evidence the timeout, and nothing else, ended the command.
	//
	// This replaces `result.Duration < 2*time.Second`, which sampled wall
	// clock: on an oversubscribed host the scheduler, not the executor,
	// decided whether it held. Measured here at load 223/16 CPUs the 500ms
	// timeout produced Durations of 501.0ms–513.1ms — the bound's remaining
	// ~1.5s existed only to absorb scheduling noise.
	assert.True(t, result.TimedOut, "the timeout manager did not fire for a %v timeout on `sleep 10`", cmd.Timeout)
	assert.True(t, result.Killed, "the command was not killed by the timeout's cancellation")
	assert.True(t, result.OutputIncomplete, "a command killed mid-flight must be reported as truncated")
}

// TestCommandWithEnvironment tests command with environment variables
func TestCommandWithEnvironment(t *testing.T) {
	executor := NewShellExecutor(DefaultConfig())

	cmd := &Command{
		ID:      "test-env",
		Command: "echo $TEST_VAR",
		Env: map[string]string{
			"TEST_VAR": "test_value",
		},
	}

	result, err := executor.Execute(context.Background(), cmd)
	require.NoError(t, err)
	assert.Equal(t, 0, result.ExitCode)
	assert.Contains(t, result.Stdout, "test_value")
}

// TestCommandWithWorkDir tests command with working directory
func TestCommandWithWorkDir(t *testing.T) {
	executor := NewShellExecutor(DefaultConfig())

	// Create a temporary directory
	tmpDir := t.TempDir()

	cmd := &Command{
		ID:      "test-workdir",
		Command: "pwd",
		WorkDir: tmpDir,
	}

	result, err := executor.Execute(context.Background(), cmd)
	require.NoError(t, err)
	assert.Equal(t, 0, result.ExitCode)
	assert.Contains(t, result.Stdout, tmpDir)
}

// TestBlockedCommand tests that blocked commands are rejected
func TestBlockedCommand(t *testing.T) {
	config := DefaultConfig()
	executor := NewShellExecutor(config)

	blockedCommands := []string{
		"rm -rf /",
		"dd if=/dev/zero of=/dev/sda",
		"mkfs.ext4 /dev/sda1",
	}

	for _, cmdStr := range blockedCommands {
		t.Run(cmdStr, func(t *testing.T) {
			cmd := &Command{
				ID:      "test-blocked",
				Command: cmdStr,
			}

			_, err := executor.Execute(context.Background(), cmd)
			assert.Error(t, err)

			var secErr *SecurityError
			assert.ErrorAs(t, err, &secErr)
		})
	}
}

// TestAllowlistEnforcement tests allowlist enforcement
func TestAllowlistEnforcement(t *testing.T) {
	config := StrictConfig()
	executor := NewShellExecutor(config)

	// Allowed command should work
	t.Run("allowed command", func(t *testing.T) {
		cmd := &Command{
			ID:      "test-allowed",
			Command: "echo 'test'",
		}

		result, err := executor.Execute(context.Background(), cmd)
		require.NoError(t, err)
		assert.Equal(t, 0, result.ExitCode)
	})

	// Non-allowed command should fail
	t.Run("not allowed command", func(t *testing.T) {
		cmd := &Command{
			ID:      "test-not-allowed",
			Command: "python script.py",
		}

		_, err := executor.Execute(context.Background(), cmd)
		assert.Error(t, err)

		var secErr *SecurityError
		require.ErrorAs(t, err, &secErr)
		assert.Equal(t, "not_allowed", secErr.Type)
	})
}

// TestDangerousPatternDetection tests dangerous pattern detection
func TestDangerousPatternDetection(t *testing.T) {
	executor := NewShellExecutor(DefaultConfig())

	dangerousPatterns := []string{
		"rm -rf /",
		":(){ :|:& };:",
		"> /dev/sda",
		"chmod -R 777 /",
	}

	for _, pattern := range dangerousPatterns {
		t.Run(pattern, func(t *testing.T) {
			cmd := &Command{
				ID:      "test-dangerous",
				Command: pattern,
			}

			_, err := executor.Execute(context.Background(), cmd)
			assert.Error(t, err)

			var secErr *SecurityError
			assert.ErrorAs(t, err, &secErr)
		})
	}
}

// TestStreamingExecution tests real-time output streaming
func TestStreamingExecution(t *testing.T) {
	executor := NewShellExecutor(DefaultConfig())

	cmd := &Command{
		ID:      "test-stream",
		Command: "for i in 1 2 3 4 5; do echo $i; done",
	}

	exec, err := executor.ExecuteStream(context.Background(), cmd)
	require.NoError(t, err)

	var lines []string
	for line := range exec.Stdout {
		lines = append(lines, line)
	}

	result := <-exec.Done
	assert.Equal(t, 0, result.ExitCode)
	assert.Equal(t, 5, len(lines))
}

// TestConcurrentExecution tests concurrent command execution
func TestConcurrentExecution(t *testing.T) {
	executor := NewShellExecutor(DefaultConfig())

	var wg sync.WaitGroup
	numCommands := 10
	results := make([]*ExecutionResult, numCommands)

	for i := 0; i < numCommands; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()

			cmd := &Command{
				ID:      fmt.Sprintf("concurrent-%d", idx),
				Command: fmt.Sprintf("echo 'Task %d'", idx),
			}

			result, err := executor.Execute(context.Background(), cmd)
			require.NoError(t, err)
			results[idx] = result
		}(i)
	}

	wg.Wait()

	for i, result := range results {
		assert.Equal(t, 0, result.ExitCode)
		assert.Contains(t, result.Stdout, fmt.Sprintf("Task %d", i))
	}
}

// TestOutputTruncation tests output size limits
func TestOutputTruncation(t *testing.T) {
	config := DefaultConfig()
	config.MaxOutputSize = 100 // Very small limit
	executor := NewShellExecutor(config)

	// Generate long output that will exceed the limit
	cmd := &Command{
		ID:      "test-truncation",
		Command: "i=1; while [ $i -le 1000 ]; do echo 'This is a long line of text'; i=$((i+1)); done",
		Shell:   "/bin/sh",
	}

	result, err := executor.Execute(context.Background(), cmd)
	require.NoError(t, err)
	// The output should have content
	assert.True(t, len(result.Stdout) > 0, "Stdout should have content")
	// Check if truncation happened
	assert.Contains(t, result.Stdout, "[output truncated]")
}

// TestSignalHandling tests signal handling
func TestSignalHandling(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Skipping signal test on Windows") // SKIP-OK: #legacy-untriaged
	}

	executor := NewShellExecutor(DefaultConfig())

	cmd := &Command{
		ID:      "test-signal",
		Command: "sleep 60",
	}

	exec, err := executor.ExecuteAsync(context.Background(), cmd)
	require.NoError(t, err)

	// Wait a bit for command to start
	time.Sleep(200 * time.Millisecond)

	// Send SIGTERM
	err = executor.Kill(cmd.ID, syscall.SIGTERM)
	// Error is ok if process already exited
	if err != nil {
		t.Logf("Kill returned error (may be expected): %v", err)
	}

	// Wait for completion
	result := <-exec.Done
	// The process should be killed or have exited
	assert.True(t, result.Killed || result.ExitCode != 0, "Process should be killed or have non-zero exit code")
}

// TestContextCancellation tests context cancellation
func TestContextCancellation(t *testing.T) {
	executor := NewShellExecutor(DefaultConfig())

	ctx, cancel := context.WithCancel(context.Background())

	cmd := &Command{
		ID:      "test-cancel",
		Command: "sleep 60",
	}

	// Start execution
	go func() {
		time.Sleep(100 * time.Millisecond)
		cancel()
	}()

	result, err := executor.Execute(ctx, cmd)
	assert.NoError(t, err)
	assert.True(t, result.Killed)
}

// TestStderrCapture tests stderr capture
func TestStderrCapture(t *testing.T) {
	executor := NewShellExecutor(DefaultConfig())

	cmd := &Command{
		ID:      "test-stderr",
		Command: "echo 'error message' >&2",
	}

	result, err := executor.Execute(context.Background(), cmd)
	require.NoError(t, err)
	assert.Equal(t, 0, result.ExitCode)
	assert.Contains(t, result.Stderr, "error message")
}

// TestInvalidWorkDir tests invalid working directory
func TestInvalidWorkDir(t *testing.T) {
	executor := NewShellExecutor(DefaultConfig())

	cmd := &Command{
		ID:      "test-invalid-workdir",
		Command: "pwd",
		WorkDir: "/nonexistent/directory",
	}

	result, _ := executor.Execute(context.Background(), cmd)
	assert.Error(t, result.Error)
}

// TestCommandInjectionPrevention tests command injection prevention
func TestCommandInjectionPrevention(t *testing.T) {
	executor := NewShellExecutor(DefaultConfig())

	// Try various injection attempts
	injectionAttempts := []string{
		"echo 'test' && rm -rf /",
		"echo 'test'; dd if=/dev/zero of=/dev/sda",
		"echo 'test' | rm -rf /",
	}

	for _, attempt := range injectionAttempts {
		t.Run(attempt, func(t *testing.T) {
			cmd := &Command{
				ID:      "test-injection",
				Command: attempt,
			}

			_, err := executor.Execute(context.Background(), cmd)
			assert.Error(t, err)
		})
	}
}

// TestPathSanitization tests path sanitization
func TestPathSanitization(t *testing.T) {
	tests := []struct {
		name     string
		path     string
		expected string
	}{
		{
			name:     "normal path",
			path:     "/tmp/test",
			expected: "/tmp/test",
		},
		{
			name:     "path with double dots",
			path:     "/tmp/../test",
			expected: "/test",
		},
		{
			name:     "path with multiple double dots",
			path:     "/tmp/../../test",
			expected: "/test",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sanitized := SanitizePath(tt.path)
			// Check that .. is removed
			assert.NotContains(t, sanitized, "..")
		})
	}
}

// TestEnvSanitization tests environment variable sanitization
func TestEnvSanitization(t *testing.T) {
	tests := []struct {
		name     string
		env      map[string]string
		expected int // number of expected valid vars
	}{
		{
			name: "valid vars",
			env: map[string]string{
				"TEST_VAR": "value",
				"PATH":     "/usr/bin",
			},
			expected: 2,
		},
		{
			name: "invalid key",
			env: map[string]string{
				"123INVALID": "value",
				"VALID_VAR":  "value",
			},
			expected: 1,
		},
		{
			name: "command substitution in value",
			env: map[string]string{
				"TEST": "$(rm -rf /)",
			},
			expected: 1, // Key is valid, value will be sanitized
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sanitized := SanitizeEnv(tt.env)
			assert.Equal(t, tt.expected, len(sanitized))

			// Check that command substitution is removed
			for _, v := range sanitized {
				assert.NotContains(t, v, "$(")
				assert.NotContains(t, v, "`")
			}
		})
	}
}

// TestQuickExecute tests the convenience function
func TestQuickExecute(t *testing.T) {
	result, err := QuickExecute("echo 'quick test'")
	require.NoError(t, err)
	assert.Equal(t, 0, result.ExitCode)
	assert.Contains(t, result.Stdout, "quick test")
}

// TestQuickExecuteWithTimeout tests the timeout convenience function
func TestQuickExecuteWithTimeout(t *testing.T) {
	result, err := QuickExecuteWithTimeout("sleep 5", 500*time.Millisecond)
	assert.NoError(t, err)
	assert.True(t, result.TimedOut || result.Killed)
}

// TestConfigValidation tests configuration validation
func TestConfigValidation(t *testing.T) {
	t.Run("valid config", func(t *testing.T) {
		config := DefaultConfig()
		err := ValidateConfig(config)
		assert.NoError(t, err)
	})

	t.Run("nil config", func(t *testing.T) {
		err := ValidateConfig(nil)
		assert.Error(t, err)
	})

	t.Run("nil security config", func(t *testing.T) {
		config := DefaultConfig()
		config.Security = nil
		err := ValidateConfig(config)
		assert.Error(t, err)
	})

	t.Run("negative max concurrent", func(t *testing.T) {
		config := DefaultConfig()
		config.MaxConcurrent = -1
		err := ValidateConfig(config)
		assert.Error(t, err)
	})

	t.Run("default timeout exceeds max", func(t *testing.T) {
		config := DefaultConfig()
		config.DefaultTimeout = 10 * time.Minute
		config.MaxTimeout = 1 * time.Minute
		err := ValidateConfig(config)
		assert.Error(t, err)
	})
}

// TestExecutionStatus tests execution status tracking.
//
// Determinism note (§11.4.50): this test used to sleep a fixed 100ms and then
// require GetStatus to succeed. That is a race, not a wait. ExecuteAsync only
// spawns a goroutine; the status entry is Stored by Execute AFTER it acquires
// the semaphore, prepares the command, applies the sandbox and completes a real
// fork/exec. On a loaded host that whole chain routinely exceeds 100ms, the
// entry is not there yet, and the require aborts — the observed
// "--- FAIL: TestExecutionStatus (0.10s)", failing at exactly the sleep
// duration, which is the signature of this race and NOT of anything the test
// intends to assert.
//
// The fix waits for the CONDITION (status observable and Running) instead of
// for a duration, and terminates the command deterministically via Cancel
// rather than racing the observation window against the command's own lifetime.
//
// §1.1 paired mutation: replace the require.Eventually below with the original
// `time.Sleep(100 * time.Millisecond)`, then run under host load (or insert a
// sleep before the executions.Store in DefaultExecutor.Execute) — the test
// FAILs again at ~0.10s, proving the guard is what removes the flake.
func TestExecutionStatus(t *testing.T) {
	executor := NewShellExecutor(DefaultConfig())

	// Long enough that the observation window is not a race against the
	// command's own lifetime, and well under the 30s DefaultConfig timeout.
	// The command is not waited out — it is cancelled below.
	cmd := &Command{
		ID:      "test-status",
		Command: "sleep 20",
	}

	exec, err := executor.ExecuteAsync(context.Background(), cmd)
	require.NoError(t, err)
	t.Cleanup(exec.Cancel)

	// Wait for the execution to be REGISTERED AND RUNNING, rather than sleeping
	// a fixed interval and hoping the goroutine + fork/exec beat the clock.
	var status *ExecutionStatus
	require.Eventually(t, func() bool {
		s, err := executor.GetStatus(cmd.ID)
		if err != nil {
			return false
		}
		status = s
		return s.State == StateRunning
	}, 10*time.Second, 5*time.Millisecond,
		"execution should become observable and Running")

	assert.Equal(t, cmd.ID, status.ID)
	assert.Equal(t, StateRunning, status.State)
	assert.True(t, status.Duration > 0)

	// Terminate deterministically instead of waiting out `sleep`: Cancel fires
	// the execution context, Execute SIGKILLs the child, reaps it, and its
	// deferred executions.Delete runs BEFORE the result is sent on Done — so by
	// the time this receive returns, the deregistration has definitely happened.
	exec.Cancel()
	select {
	case <-exec.Done:
	case <-time.After(30 * time.Second):
		t.Fatal("cancelled execution did not finish")
	}

	// Status should no longer be available
	_, err = executor.GetStatus(cmd.ID)
	assert.Error(t, err)
}

// TestListExecutions tests listing running executions.
//
// Determinism note (§11.4.50): this carried the identical latent defect as
// TestExecutionStatus above — start N async commands, sleep a fixed 100ms,
// then assert they are all listed. ExecuteAsync only spawns a goroutine; each
// entry is Stored by Execute AFTER it acquires the semaphore, prepares the
// command, applies the sandbox, and completes a real fork/exec. On a loaded
// host that chain can exceed 100ms for all three goroutines, so the fixed
// sleep is a race against their registration, not a wait for it.
//
// This test also carries a SECOND, independent defect the sibling did not
// have: it assumes all three commands register concurrently. That is only
// true if DefaultConfig().MaxConcurrent >= numCommands, because Execute
// blocks acquiring e.semaphore (buffered to MaxConcurrent) before it Stores
// the status entry — a lower limit would make command N+1 block behind the
// semaphore and never appear in ListExecutions, independent of any timing
// fix. Verified: DefaultConfig().MaxConcurrent is 10 (shell.go), so with
// numCommands = 3 all three can register concurrently and this path is not
// reachable here. The assertion below still keys off the real config value
// instead of the bare literal 3, so this test cannot silently start lying if
// MaxConcurrent is ever lowered under numCommands.
//
// §1.1 paired mutation: replace the require.Eventually below with the
// original `time.Sleep(100 * time.Millisecond)`, then run under host load
// (or insert a sleep before the executions.Store in DefaultExecutor.Execute)
// — the test FAILs again at ~0.10s, proving the guard is what removes the
// flake. Actually run: see report.
func TestListExecutions(t *testing.T) {
	config := DefaultConfig()
	executor := NewShellExecutor(config)

	// Start multiple commands
	numCommands := 3
	require.LessOrEqual(t, numCommands, config.MaxConcurrent,
		"test assumes all commands register concurrently; MaxConcurrent must cover numCommands")

	for i := 0; i < numCommands; i++ {
		cmd := &Command{
			ID:      fmt.Sprintf("list-test-%d", i),
			Command: "sleep 2",
		}
		exec, err := executor.ExecuteAsync(context.Background(), cmd)
		require.NoError(t, err)
		t.Cleanup(exec.Cancel)
	}

	// Wait for the CONDITION (all commands registered), not a duration.
	require.Eventually(t, func() bool {
		return len(executor.ListExecutions()) >= numCommands
	}, 10*time.Second, 5*time.Millisecond,
		"all started executions should become observable via ListExecutions")

	// List executions
	executions := executor.ListExecutions()
	assert.GreaterOrEqual(t, len(executions), numCommands)
}

// BenchmarkSimpleExecution benchmarks simple command execution
func BenchmarkSimpleExecution(b *testing.B) {
	executor := NewShellExecutor(DefaultConfig())

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		cmd := &Command{
			ID:      fmt.Sprintf("bench-%d", i),
			Command: "echo 'benchmark'",
		}
		_, _ = executor.Execute(context.Background(), cmd)
	}
}

// BenchmarkConcurrentExecution benchmarks concurrent execution
func BenchmarkConcurrentExecution(b *testing.B) {
	executor := NewShellExecutor(DefaultConfig())

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			cmd := &Command{
				ID:      fmt.Sprintf("bench-concurrent-%d", i),
				Command: "echo 'benchmark'",
			}
			_, _ = executor.Execute(context.Background(), cmd)
			i++
		}
	})
}

// TestMultilineCommand tests execution of multiline commands
func TestMultilineCommand(t *testing.T) {
	executor := NewShellExecutor(DefaultConfig())

	script := `
	#!/bin/sh
	echo "Line 1"
	echo "Line 2"
	echo "Line 3"
	`

	cmd := &Command{
		ID:      "test-multiline",
		Command: script,
	}

	result, err := executor.Execute(context.Background(), cmd)
	require.NoError(t, err)
	assert.Equal(t, 0, result.ExitCode)
	assert.Contains(t, result.Stdout, "Line 1")
	assert.Contains(t, result.Stdout, "Line 2")
	assert.Contains(t, result.Stdout, "Line 3")
}

// TestCommandWithArgs tests command execution with arguments
func TestCommandWithArgs(t *testing.T) {
	executor := NewShellExecutor(DefaultConfig())

	cmd := &Command{
		ID:      "test-args",
		Command: "echo",
		Args:    []string{"arg1", "arg2", "arg3"},
	}

	result, err := executor.Execute(context.Background(), cmd)
	require.NoError(t, err)
	assert.Equal(t, 0, result.ExitCode)
	// Note: The way args are passed might not work as expected with shell -c
	// This test documents the current behavior
}

// TestStreamingWithErrors tests streaming execution with errors
func TestStreamingWithErrors(t *testing.T) {
	executor := NewShellExecutor(DefaultConfig())

	cmd := &Command{
		ID:      "test-stream-error",
		Command: "echo 'stdout'; echo 'stderr' >&2; exit 1",
	}

	exec, err := executor.ExecuteStream(context.Background(), cmd)
	require.NoError(t, err)

	var stdoutLines []string
	var stderrLines []string

	stdoutDone := make(chan struct{})
	stderrDone := make(chan struct{})
	go func() {
		for line := range exec.Stdout {
			stdoutLines = append(stdoutLines, line)
		}
		close(stdoutDone)
	}()

	go func() {
		for line := range exec.Stderr {
			stderrLines = append(stderrLines, line)
		}
		close(stderrDone)
	}()

	result := <-exec.Done
	// Wait for BOTH stream collectors to finish before reading the
	// slices they wrote to — otherwise a goroutine append races with
	// the assertions below.
	<-stdoutDone
	<-stderrDone

	assert.Equal(t, 1, result.ExitCode)
	assert.True(t, len(stdoutLines) > 0 || strings.Contains(result.Stdout, "stdout"))
	assert.True(t, len(stderrLines) > 0 || strings.Contains(result.Stderr, "stderr"))
}
