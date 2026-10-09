package voice

// Standing regression guard (§11.4.135) for HXC-VOICE-START /
// HXC-VOICE-WAIT.
//
// DEFECT (FACT, source-provable): VoiceRecorder.Start() built an
// *exec.Cmd via detectCaptureCmd but never called cmd.Start(), so the
// OS capture process was never launched. The recorder reported
// RecorderRecording while capturing nothing — every downstream
// voice_stop / voice_transcribe then failed because the WAV file was
// never created (ValidateWAV → ErrEmptyRecording / stat error). The
// designed-but-dead VoiceConfig.CaptureCmd was also never wired in.
// Audio path → §11.4.72 top-priority.
//
// §11.4.115 polarity switch (RED_MODE env):
//   RED_MODE=1 → reproduce the defect on a faithful pre-fix stand-in
//               (build the cmd, set status=Recording, but DO NOT Start
//               it) and ASSERT the defect is present (no live process,
//               no file). PASSES on the broken behaviour — proving the
//               guard genuinely catches the bug.
//   RED_MODE=0 (default) → drive the REAL fixed VoiceRecorder against a
//               real capture subprocess and ASSERT the defect is ABSENT
//               (process actually launched, WAV file created + non-empty
//               after Stop()).
//
// Uses a real `sh -c` capture command (no mock — this is anti-bluff
// runtime evidence per §11.4 / CONST-050(A) permits mocks only in unit
// tests, and this drives a REAL subprocess regardless).

import (
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

// redMode reports whether the RED reproduction polarity is active.
func redMode() bool { return os.Getenv("RED_MODE") == "1" }

// writerCaptureCmd writes a small executable capture-stand-in script to
// a temp dir and returns its absolute path (suitable for
// NewVoiceRecorderWithCmd). When launched with the destination WAV path
// as its argument, the script writes a valid >44-byte file then sleeps
// (staying alive until Stop() signals + reaps it) — a faithful REAL
// subprocess stand-in for arecord/sox writing the destination file.
// NewVoiceRecorderWithCmd splits on whitespace, so a single bare path
// (no spaces, no shell quoting) is used to survive that split.
func writerCaptureCmd(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skipf("SKIP-OK: /bin/sh unavailable on this host — §11.4.3: %v", err)
	}
	dir := t.TempDir()
	script := filepath.Join(dir, "capture_writer.sh")
	// $1 = destination path appended by NewVoiceRecorderWithCmd.
	// head -c 64 → 64-byte file (>44 so ValidateWAV passes); sleep keeps
	// the process alive so Stop() must genuinely signal + Wait()-reap it.
	body := "#!/bin/sh\nhead -c 64 /dev/zero > \"$1\"\nsleep 30\n"
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatalf("write capture stand-in script: %v", err)
	}
	return script
}

func TestVoiceRecorder_StartLaunchesProcess_Guard(t *testing.T) {
	outDir := t.TempDir()
	outPath := filepath.Join(outDir, "guard.wav")

	if redMode() {
		// RED: faithful pre-fix stand-in. Reproduce the exact broken
		// behaviour: build the command but DO NOT Start() it.
		rec := &VoiceRecorder{status: RecorderIdle, captureCmd: writerCaptureCmd(t)}
		cmd, err := rec.detectCaptureCmd(outPath)
		if err != nil {
			t.Skipf("SKIP-OK: no capture backend on this host — §11.4.3: %v", err)
		}
		// Mirror the PRE-FIX Start(): assign cmd + flip status, but never
		// call cmd.Start().
		rec.mu.Lock()
		rec.cmd = cmd
		rec.filePath = outPath
		rec.status = RecorderRecording
		rec.mu.Unlock()

		// DEFECT ASSERTION 1: no process was ever launched.
		rec.mu.Lock()
		proc := rec.cmd.Process
		rec.mu.Unlock()
		if proc != nil {
			t.Fatalf("RED expected cmd.Process==nil (process never launched), got pid=%d", proc.Pid)
		}

		// DEFECT ASSERTION 2: the capture file was never created, so the
		// downstream ValidateWAV the voice_stop tool performs fails.
		time.Sleep(200 * time.Millisecond)
		if _, statErr := os.Stat(outPath); statErr == nil {
			t.Fatalf("RED expected NO capture file (process never ran), but %s exists", outPath)
		}
		if vErr := ValidateWAV(outPath); vErr == nil {
			t.Fatalf("RED expected ValidateWAV to fail on absent file, got nil")
		}
		t.Logf("RED reproduced: Start() stand-in set status=Recording but launched no process and wrote no file")
		return
	}

	// GREEN: drive the REAL fixed recorder end-to-end.
	rec := NewVoiceRecorderWithCmd(writerCaptureCmd(t))
	if err := rec.Start(outPath); err != nil {
		t.Fatalf("Start() failed on real fixed recorder: %v", err)
	}

	// The capture process MUST have actually been launched.
	rec.mu.Lock()
	proc := rec.cmd.Process
	rec.mu.Unlock()
	if proc == nil {
		t.Fatalf("GREEN: Start() reported Recording but cmd.Process is nil — defect NOT fixed")
	}

	// Give the real subprocess a moment to write the destination file.
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if info, err := os.Stat(outPath); err == nil && info.Size() > 44 {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	if err := rec.Stop(); err != nil {
		t.Fatalf("Stop() failed: %v", err)
	}

	// ABSENCE-OF-DEFECT: a real, non-empty capture file exists — the
	// exact thing the broken code never produced.
	info, err := os.Stat(outPath)
	if err != nil {
		t.Fatalf("GREEN: capture file missing after Start/Stop — defect present: %v", err)
	}
	if info.Size() <= 44 {
		t.Fatalf("GREEN: capture file too small (%d bytes) — process did not write — defect present", info.Size())
	}
	if err := ValidateWAV(outPath); err != nil {
		t.Fatalf("GREEN: ValidateWAV failed on real captured file — defect present: %v", err)
	}
	if rec.Status() != RecorderStopped {
		t.Fatalf("GREEN: expected RecorderStopped after Stop(), got %v", rec.Status())
	}
}

// TestVoiceRecorder_StopReapsProcess_Guard proves HXC-VOICE-WAIT: after
// Stop(), the launched capture process is reaped (no zombie / no leaked
// wait). GREEN-only (the pre-fix code never launched a process, so there
// was nothing to reap — the RED reproduction lives in the test above).
func TestVoiceRecorder_StopReapsProcess_Guard(t *testing.T) {
	if redMode() {
		t.Skip("SKIP-OK: reap behaviour only meaningful against the fixed code (pre-fix launched no process)")
	}
	outPath := filepath.Join(t.TempDir(), "reap.wav")
	rec := NewVoiceRecorderWithCmd(writerCaptureCmd(t))
	if err := rec.Start(outPath); err != nil {
		t.Fatalf("Start() failed: %v", err)
	}
	rec.mu.Lock()
	cmd := rec.cmd
	rec.mu.Unlock()
	if cmd == nil || cmd.Process == nil {
		t.Fatalf("Start() reported Recording but no process was launched — defect present (HXC-VOICE-START regressed)")
	}
	pid := cmd.Process.Pid

	if err := rec.Stop(); err != nil {
		t.Fatalf("Stop() failed: %v", err)
	}
	// After Wait(), ProcessState is populated — proof the child was reaped
	// (not left a zombie). A nil ProcessState means Wait() never ran.
	if cmd.ProcessState == nil {
		t.Fatalf("ProcessState nil after Stop() — process pid=%d was NOT reaped (zombie/leak)", pid)
	}
	// DETERMINISM (§11.4.50): the reap REASON, read off the exit status
	// instead of the clock. Stop() sends SIGINT and only escalates to SIGKILL
	// if the child is still there after 2s, so "was the child SIGKILLed?" is a
	// direct, scheduler-free witness of whether that escalation path ran —
	// exactly what the previous `elapsed > 2500ms` bound stood in for.
	// Measured on this host at load 223/16 CPUs, Stop() took 240µs–3.2ms
	// against that 2500ms bound: the bound was ~780x the observed cost and its
	// truth was decided by host scheduling, not by Stop().
	type signalStatus interface {
		Signaled() bool
		Signal() syscall.Signal
	}
	if ws, ok := cmd.ProcessState.Sys().(signalStatus); ok {
		if ws.Signaled() && ws.Signal() == syscall.SIGKILL {
			t.Fatalf("child pid=%d was SIGKILLed — Stop() fell through to its 2s Kill escalation instead of reaping the SIGINT-terminated child", pid)
		}
	} else {
		// SKIP-OK (§11.4.3): this platform's wait status does not expose
		// signal disposition. The reap itself is still asserted above; only
		// the which-path refinement is unavailable here.
		t.Logf("SKIP-OK: %T exposes no signal disposition on this platform; escalation-path check not performed", cmd.ProcessState.Sys())
	}
}

// awaitFile blocks until path exists, or fails the test when the budget
// expires. The budget is NOT a pass/fail threshold — it is only an upper
// bound on "the other side of the barrier never arrived", and its expiry
// is always a loud, explanatory FAIL. The correct-behaviour path crosses
// each barrier in microseconds; the budget exists so a genuinely stuck
// subprocess reports a cause instead of hanging the suite.
func awaitFile(t *testing.T, path string, budget time.Duration, whatFailed string) {
	t.Helper()
	deadline := time.Now().Add(budget)
	for {
		if _, err := os.Stat(path); err == nil {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("%s (waited %s for barrier file %s)", whatFailed, budget, path)
		}
		time.Sleep(200 * time.Microsecond)
	}
}

// TestVoiceRecorder_StatusNonBlockingDuringStop_Guard proves MUST-FIX 1
// (§11.4.72 audio path): Stop() must NOT hold r.mu across its blocking
// SIGINT→reap, or a concurrent Status()/IsRecording()/FilePath()/Duration()
// would stall for up to ~2s.
//
// DETERMINISM (§11.4.50) — this guard was rebuilt to remove every timing,
// scheduling and ambient-load dependence. What it used to do, and why that
// was non-deterministic (measured, not assumed):
//
//	The old shape slept a fixed 50ms after Start() and then ASSUMED the perl
//	capture stand-in had already installed $SIG{INT}='IGNORE', so that Stop()
//	would be forced down its full 2s escalation; it then asserted a wall-clock
//	observer latency < 250ms against that ~2s window. Both halves raced the
//	host. Measured on this tree: perl reaches handler-installed in 16-20ms on a
//	quiet host but 21-311ms under load — 9 of 10 loaded runs blew the 50ms
//	budget. When that happens SIGINT arrives BEFORE the handler exists, perl
//	dies on the default disposition, cmd.Wait() returns in ~4ms, and the
//	test failed its own sanity check with
//	  "Stop() returned in 3.802285ms — expected ~2s escalation".
//	The 250ms latency bound was the second race: it compared two wall-clock
//	numbers whose ratio shrinks as the host gets busier.
//
// The rebuilt guard replaces BOTH races with synchronisation points and a
// threshold-free observation of the property itself:
//
//	BARRIER 1 (ready)     — the stand-in writes "ready" only AFTER installing
//	                        its signal handlers, so its appearance PROVES the
//	                        subprocess is armed. Replaces the 50ms sleep.
//	BARRIER 2 (signalled) — the handlers are non-fatal but OBSERVABLE: they
//	                        record receipt instead of exiting. "signalled"
//	                        appearing PROVES SIGINT was delivered, i.e. Stop()
//	                        is between Signal() and return — inside the reap.
//	ORACLE 1 (TryLock)    — at that provably-in-window moment, ask the mutex
//	                        directly whether it is held. Instantaneous, no
//	                        threshold, no latency comparison, load-invariant.
//	ORACLE 2 (hold+drain) — still HOLDING r.mu, release the subprocess so the
//	                        reap can finish, and require Stop() to return. If
//	                        Stop() needed r.mu it can NEVER return while we
//	                        hold it; if it does not, it returns in µs. The
//	                        discrimination is ∞-vs-µs, so no bound the host
//	                        can perturb.
//
// Deliberately dropped: the old "Stop() took ~2s" duration assertion. The
// reap window is now ended by the test (ORACLE 2), so Stop()'s duration is
// no longer evidence of anything and asserting on it would re-introduce a
// wall-clock dependence. The window is instead proven by BARRIER 2 plus an
// explicit not-yet-returned check, and a missed window is a loud FAIL, never
// a silent pass.
//
// Paired §1.1 mutation: re-acquiring r.mu around the reap block in Stop()
// (the pre-fix lock-held-across-wait shape) makes ORACLE 1's TryLock fail →
// this guard FAILs. Restoring the snapshot-and-release shape → guard PASSes.
func TestVoiceRecorder_StatusNonBlockingDuringStop_Guard(t *testing.T) {
	if redMode() {
		t.Skip("SKIP-OK: lock-contention behaviour only meaningful against the fixed Stop() (pre-fix launched no process to reap)")
	}
	perlPath, err := exec.LookPath("perl")
	if err != nil {
		// §11.4.3: this guard needs a capture stand-in whose signal
		// handlers are non-fatal AND observable. Plain /bin/sh `trap ''
		// INT` is neither uniformly honoured across platforms nor able to
		// report receipt, so we require perl.
		t.Skipf("SKIP-OK: perl unavailable — cannot build a deterministic signal-observing capture stand-in on this host — §11.4.3: %v", err)
	}

	// Control directory carrying the three barrier files. Kept separate from
	// the script dir so the script can derive every path from one argument.
	ctrl := t.TempDir()
	readyPath := filepath.Join(ctrl, "ready")
	signalledPath := filepath.Join(ctrl, "signalled")
	releasePath := filepath.Join(ctrl, "release")

	// Capture stand-in. Its INT/TERM handlers RECORD receipt and return —
	// they neither exit (so Stop() stays in its reap) nor are invisible (so
	// the test gets a hard synchronisation point). It exits only when the
	// test creates the release file, so the reap window is closed by the
	// test, not by a clock.
	scriptDir := t.TempDir()
	script := filepath.Join(scriptDir, "barrier_capture.pl")
	body := "#!/usr/bin/env perl\n" +
		"my ($dir, $out) = @ARGV;\n" +
		"my $note = sub { open(my $m, '>', \"$dir/signalled\") and close($m); };\n" +
		"$SIG{INT} = $note; $SIG{TERM} = $note;\n" +
		"open(my $fh, '>', $out) or die \"open out: $!\";\n" +
		"print $fh ('\\0' x 64);\n" +
		"close($fh);\n" +
		"open(my $r, '>', \"$dir/ready\") or die \"open ready: $!\";\n" +
		"close($r);\n" +
		"until (-e \"$dir/release\") { select(undef, undef, undef, 0.005); }\n" +
		"exit 0;\n"
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatalf("write barrier capture stand-in: %v", err)
	}
	// NewVoiceRecorderWithCmd whitespace-splits the capture command,
	// LookPath-validates field[0], and appends the destination path last →
	// argv = [perl, script, ctrl, outPath] → $ARGV[0]=ctrl, $ARGV[1]=outPath.
	captureCmd := perlPath + " " + script + " " + ctrl

	outPath := filepath.Join(t.TempDir(), "nonblock.wav")
	rec := NewVoiceRecorderWithCmd(captureCmd)
	if err := rec.Start(outPath); err != nil {
		t.Fatalf("Start() failed: %v", err)
	}
	// Unconditional release so no failure path can leave the stand-in alive
	// (§11.4.14). Runs before t.TempDir cleanup.
	defer func() { _ = os.WriteFile(releasePath, []byte("release"), 0o644) }()

	// BARRIER 1: the stand-in is armed. This is a synchronisation point, not
	// a sleep — it is exactly the fact the old 50ms sleep merely hoped for.
	awaitFile(t, readyPath, 60*time.Second,
		"capture stand-in never reported ready — it did not install its signal handlers, so this guard cannot probe the reap window")

	stopErr := make(chan error, 1)
	stopReturned := make(chan struct{})
	go func() {
		stopErr <- rec.Stop()
		close(stopReturned)
	}()

	// BARRIER 2: SIGINT was delivered and the handler ran. Stop() is now
	// provably between Signal() and its return — inside the reap.
	awaitFile(t, signalledPath, 60*time.Second,
		"capture stand-in never observed SIGINT — Stop() did not reach its reap (if it is blocked acquiring r.mu before signalling, that IS the defect this guard exists for)")

	// ORACLE 1 — threshold-free property observation. Is r.mu held right now,
	// at a moment provably inside the reap? No other goroutine in this test
	// holds it, so with the correct snapshot-and-release Stop() this ALWAYS
	// succeeds, on any host, at any load.
	acquired := rec.mu.TryLock()

	// Prove the sample was taken INSIDE the window and not after Stop()
	// already finished (which would make ORACLE 1 vacuously true). Stop()'s
	// internal 2s kill-timeout is the only thing that can close the window
	// early; missing it is reported loudly rather than passed silently.
	windowMissed := false
	select {
	case <-stopReturned:
		windowMissed = true
	default:
	}

	if !acquired {
		t.Fatalf("r.mu was HELD at a moment provably inside Stop()'s reap window (SIGINT delivered, Stop() not yet returned) — Stop() is holding the lock across the blocking reap, so concurrent Status()/IsRecording()/FilePath()/Duration() would stall (MUST-FIX 1 regressed)")
	}
	if windowMissed {
		rec.mu.Unlock()
		t.Fatalf("Stop() returned before the lock could be sampled — its internal kill-timeout closed the reap window first, so this run proved nothing; re-run (this is an inconclusive run reported honestly, never a pass)")
	}

	// ORACLE 2 — ∞-vs-µs discrimination. We hold r.mu. Let the stand-in exit
	// so cmd.Wait() returns and Stop() can complete. A Stop() that needs r.mu
	// after/across the reap can NEVER return while we hold it; the correct one
	// returns immediately because it snapshotted cmd and released the lock.
	if err := os.WriteFile(releasePath, []byte("release"), 0o644); err != nil {
		rec.mu.Unlock()
		t.Fatalf("write release barrier: %v", err)
	}
	select {
	case err := <-stopErr:
		rec.mu.Unlock()
		if err != nil {
			t.Fatalf("Stop() failed: %v", err)
		}
	case <-time.After(30 * time.Second):
		rec.mu.Unlock()
		t.Fatalf("Stop() did not return within 30s while the test held r.mu, although its capture process had been released — Stop() requires r.mu across/after its reap (MUST-FIX 1 regressed)")
	}

	// Post-conditions: the terminal state was published and the lock is free.
	if got := rec.Status(); got != RecorderStopped {
		t.Fatalf("expected RecorderStopped after Stop(), got %v", got)
	}
	t.Logf("r.mu was free at a barrier-proven point inside Stop()'s reap, and Stop() completed while the test held r.mu — lock provably not held across the reap (no timing threshold involved)")
}
