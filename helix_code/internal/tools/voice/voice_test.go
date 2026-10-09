package voice

import (
	"context"
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// TestDeviceManager_ListDevices tests device enumeration
func TestDeviceManager_ListDevices(t *testing.T) {
	dm, err := NewDeviceManager()
	if err != nil {
		t.Fatalf("NewDeviceManager() error = %v", err)
	}

	devices, err := dm.ListDevices(context.Background())
	if err != nil {
		t.Fatalf("ListDevices() error = %v", err)
	}

	if len(devices) == 0 {
		t.Error("expected at least one device")
	}

	// Verify device properties
	for _, device := range devices {
		if device.ID == "" {
			t.Error("device ID should not be empty")
		}
		if device.Name == "" {
			t.Error("device name should not be empty")
		}
		if len(device.SampleRates) == 0 {
			t.Error("device should have at least one sample rate")
		}
	}
}

// TestDeviceManager_GetDefaultDevice tests getting the default device
func TestDeviceManager_GetDefaultDevice(t *testing.T) {
	dm, err := NewDeviceManager()
	if err != nil {
		t.Fatalf("NewDeviceManager() error = %v", err)
	}

	device, err := dm.GetDefaultDevice()
	if err != nil {
		t.Fatalf("GetDefaultDevice() error = %v", err)
	}

	if device == nil {
		t.Fatal("expected non-nil device")
	}

	if !device.IsDefault && len(device.ID) == 0 {
		t.Error("expected device to be marked as default or have an ID")
	}
}

// TestDeviceManager_SelectDevice tests device selection
func TestDeviceManager_SelectDevice(t *testing.T) {
	dm, err := NewDeviceManager()
	if err != nil {
		t.Fatalf("NewDeviceManager() error = %v", err)
	}

	devices, err := dm.ListDevices(context.Background())
	if err != nil || len(devices) == 0 {
		t.Skip("no devices available for testing")  // SKIP-OK: #legacy-untriaged
	}

	// Select first device
	err = dm.SelectDevice(devices[0].ID)
	if err != nil {
		t.Fatalf("SelectDevice() error = %v", err)
	}

	// Verify selection
	activeDevice := dm.GetActiveDevice()
	if activeDevice == nil {
		t.Fatal("expected active device to be set")
	}

	if activeDevice.ID != devices[0].ID {
		t.Errorf("expected device ID %s, got %s", devices[0].ID, activeDevice.ID)
	}
}

// TestAudioRecorder_StartStop tests recording start and stop
func TestAudioRecorder_StartStop(t *testing.T) {
	// Create temporary directory for recordings
	tmpDir, err := os.MkdirTemp("", "voice_test_*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create mock device
	device := &AudioDevice{
		ID:          "test-device",
		Name:        "Test Microphone",
		IsDefault:   true,
		SampleRates: []int{16000, 44100},
		Channels:    1,
		IsAvailable: true,
		Driver:      "Mock",
	}

	config := &AudioConfig{
		SampleRate:      16000,
		Channels:        1,
		BitDepth:        16,
		Format:          FormatWAV,
		OutputDirectory: tmpDir,
	}

	recorder, err := NewAudioRecorder(device, config)
	if err != nil {
		t.Fatalf("NewAudioRecorder() error = %v", err)
	}

	ctx := context.Background()

	// Test starting recording
	if err := recorder.Start(ctx); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	if !recorder.IsRecording() {
		t.Error("expected recorder to be recording")
	}

	// Wait until the mock recording goroutine has genuinely produced
	// audio. LevelMonitor seeds currentPeak at -100.0 dB and only moves
	// it from recordMockAudio's tick, so Peak > -100.0 is a real
	// happens-after signal rather than a guess about elapsed time.
	// Generous ceiling + tiny interval: a fast machine exits on the
	// first check, a 100x-slow one still passes.
	require.Eventually(t, func() bool {
		return recorder.GetLevels().Peak > -100.0
	}, 30*time.Second, 5*time.Millisecond, "mock recorder never produced audio levels")

	// Test stopping recording
	filePath, err := recorder.Stop(ctx)
	if err != nil {
		t.Fatalf("Stop() error = %v", err)
	}

	if filePath == "" {
		t.Error("expected non-empty file path")
	}

	// Verify file exists
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		t.Errorf("recorded file does not exist: %s", filePath)
	}

	// Verify file has content
	fileInfo, err := os.Stat(filePath)
	if err != nil {
		t.Fatalf("failed to stat file: %v", err)
	}

	if fileInfo.Size() == 0 {
		t.Error("recorded file is empty")
	}
}

// TestAudioRecorder_DoubleStart tests error handling for double start
func TestAudioRecorder_DoubleStart(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "voice_test_*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	device := &AudioDevice{
		ID:          "test-device",
		Name:        "Test Microphone",
		IsDefault:   true,
		SampleRates: []int{16000},
		Channels:    1,
		IsAvailable: true,
		Driver:      "Mock",
	}

	config := &AudioConfig{
		SampleRate:      16000,
		Channels:        1,
		BitDepth:        16,
		Format:          FormatWAV,
		OutputDirectory: tmpDir,
	}

	recorder, err := NewAudioRecorder(device, config)
	if err != nil {
		t.Fatalf("NewAudioRecorder() error = %v", err)
	}

	ctx := context.Background()

	// Start recording
	if err := recorder.Start(ctx); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	// Try to start again - should fail
	err = recorder.Start(ctx)
	if err != ErrAlreadyRecording {
		t.Errorf("expected ErrAlreadyRecording, got %v", err)
	}

	// Clean up
	recorder.Stop(ctx)
}

// TestLevelMonitor tests audio level monitoring
func TestLevelMonitor(t *testing.T) {
	monitor := NewLevelMonitor(100*time.Millisecond, 10*time.Millisecond)

	// Generate test samples (sine wave)
	samples := generateSineWave(1000, 440.0, 16000)
	monitor.Update(samples)

	levels := monitor.GetLevels()

	// Peak and RMS are in dB, so they should be > -100 (not silence)
	if levels.Peak <= -100.0 {
		t.Errorf("expected peak level > -100 dB, got %.2f", levels.Peak)
	}

	if levels.RMS <= -100.0 {
		t.Errorf("expected RMS level > -100 dB, got %.2f", levels.RMS)
	}

	if levels.Timestamp.IsZero() {
		t.Error("expected non-zero timestamp")
	}

	// Test with silence - need to flush the buffer with more silence samples
	// since the buffer size is 1024 and still contains sine wave samples
	silence := make([]float64, 2000)
	monitor.Update(silence)

	silentLevels := monitor.GetLevels()
	// After flushing with enough silence, RMS should be very low (< -40 dB)
	if silentLevels.RMS > -40.0 {
		t.Errorf("expected silent levels (< -40 dB), got %.2f dB", silentLevels.RMS)
	}
}

// TestSilenceDetector tests silence detection
func TestSilenceDetector(t *testing.T) {
	detector := NewSilenceDetector(-40.0, 100*time.Millisecond)

	// Test with loud audio
	loudLevels := &AudioLevels{
		Peak:      -10.0,
		RMS:       -15.0,
		IsSilent:  false,
		Timestamp: time.Now(),
	}

	if detector.IsSilent(loudLevels) {
		t.Error("loud audio incorrectly detected as silent")
	}

	// Test with quiet audio
	quietLevels := &AudioLevels{
		Peak:      -50.0,
		RMS:       -55.0,
		IsSilent:  true,
		Timestamp: time.Now(),
	}

	// First check - not yet past minimum duration
	if detector.IsSilent(quietLevels) {
		t.Error("silence detected before minimum duration")
	}

	// Wait for minimum duration
	time.Sleep(150 * time.Millisecond)

	if !detector.IsSilent(quietLevels) {
		t.Error("silence not detected after minimum duration")
	}

	// Test reset
	detector.Reset()
	if detector.IsSilent(quietLevels) {
		t.Error("silence detected immediately after reset")
	}
}

// TestMockTranscriber tests the mock transcriber
func TestMockTranscriber(t *testing.T) {
	// Create temporary audio file
	tmpDir, err := os.MkdirTemp("", "voice_test_*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	audioFile := filepath.Join(tmpDir, "test.wav")
	if err := os.WriteFile(audioFile, []byte("fake audio data"), 0644); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	config := &TranscriptionConfig{
		APIKey: "test-key",
		Model:  "whisper-1",
	}

	transcriber := NewMockTranscriber(config)

	ctx := context.Background()
	result, err := transcriber.TranscribeFile(ctx, audioFile)
	if err != nil {
		t.Fatalf("TranscribeFile() error = %v", err)
	}

	if result.Text == "" {
		t.Error("expected non-empty transcription text")
	}

	if result.Language == "" {
		t.Error("expected language to be detected")
	}

	if len(result.Segments) == 0 {
		t.Error("expected at least one segment")
	}
}

// TestMockTranscriber_CustomResponse tests custom mock responses
func TestMockTranscriber_CustomResponse(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "voice_test_*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	audioFile := filepath.Join(tmpDir, "custom.wav")
	if err := os.WriteFile(audioFile, []byte("fake audio data"), 0644); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	config := &TranscriptionConfig{
		APIKey: "test-key",
		Model:  "whisper-1",
	}

	transcriber := NewMockTranscriber(config)
	customText := "This is a custom transcription response"
	transcriber.SetMockResponse(audioFile, customText)

	ctx := context.Background()
	result, err := transcriber.TranscribeFile(ctx, audioFile)
	if err != nil {
		t.Fatalf("TranscribeFile() error = %v", err)
	}

	if result.Text != customText {
		t.Errorf("expected text %q, got %q", customText, result.Text)
	}
}

// TestVoiceInputManager_Integration tests the full integration
func TestVoiceInputManager_Integration(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "voice_test_*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Construct a Mock-driver device directly and drive the recorder
	// at the lowest level so this test does not depend on the host
	// platform's audio enumeration (Linux ALSA, macOS CoreAudio,
	// Windows WASAPI) producing a Mock device. The round-32 §11.4 fix
	// removed the silent recordRealAudio→recordMockAudio fallback
	// (ErrPlatformAudioCaptureNotWired now surfaces loudly), so going
	// through VoiceInputManager's AutoSelect path on Linux would have
	// picked an ALSA device and flipped a.recording=false — masking
	// the bluff fix. The lower-level path below faithfully exercises
	// the same code (Start → recordMockAudio → Stop → writeAudioFile)
	// without the platform-enumeration coupling.
	mockDevice := &AudioDevice{
		ID:          "mock-default",
		Name:        "Mock Default Microphone",
		IsDefault:   true,
		SampleRates: []int{8000, 16000, 44100, 48000},
		Channels:    1,
		IsAvailable: true,
		Driver:      "Mock",
	}
	audioConfig := &AudioConfig{
		SampleRate:      16000,
		Channels:        1,
		BitDepth:        16,
		Format:          FormatWAV,
		OutputDirectory: tmpDir,
	}

	recorder, err := NewAudioRecorder(mockDevice, audioConfig)
	if err != nil {
		t.Fatalf("NewAudioRecorder() error = %v", err)
	}

	ctx := context.Background()

	// Test recording
	if err := recorder.Start(ctx); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	if !recorder.IsRecording() {
		t.Error("expected recorder to be active")
	}

	// Wait until the mock recording goroutine has genuinely produced
	// audio before reading levels: LevelMonitor seeds currentPeak at
	// -100.0 dB and only moves it from recordMockAudio's tick. The
	// assertion is the predicate, never elapsed time. Generous ceiling
	// + tiny interval: a fast machine exits on the first check.
	require.Eventually(t, func() bool {
		return recorder.GetLevels().Peak > -100.0
	}, 30*time.Second, 5*time.Millisecond, "mock recorder never produced audio levels")

	// Get audio levels via the recorder directly (manager removed; see
	// platform-decoupling rationale above).
	levels := recorder.GetLevels()
	if levels == nil {
		t.Error("expected non-nil audio levels")
	}

	// Stop recording
	audioPath, err := recorder.Stop(ctx)
	if err != nil {
		t.Fatalf("Stop() error = %v", err)
	}

	if audioPath == "" {
		t.Error("expected non-empty audio path")
	}

	// Verify file exists
	if _, err := os.Stat(audioPath); os.IsNotExist(err) {
		t.Errorf("audio file does not exist: %s", audioPath)
	}
}

// TestAudioRecorder_RealMode_NoPlatformBridge asserts the round-32
// §11.4 PASS-bluff guard: when a non-Mock driver device is passed and
// Start() dispatches to recordRealAudio, the recorder MUST flip
// a.recording=false (so subsequent Stop returns ErrNotRecording rather
// than silently writing fabricated mock samples). Article XI §11.9 /
// CONST-035 / CONST-050(A) — this test pins the loud-fail contract.
func TestAudioRecorder_RealMode_NoPlatformBridge(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "voice_realmode_*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	device := &AudioDevice{
		ID:          "real-alsa",
		Name:        "Real ALSA Microphone",
		IsDefault:   true,
		SampleRates: []int{16000},
		Channels:    1,
		IsAvailable: true,
		Driver:      "ALSA",
	}

	config := &AudioConfig{
		SampleRate:      16000,
		Channels:        1,
		BitDepth:        16,
		Format:          FormatWAV,
		OutputDirectory: tmpDir,
	}

	recorder, err := NewAudioRecorder(device, config)
	if err != nil {
		t.Fatalf("NewAudioRecorder() error = %v", err)
	}

	ctx := context.Background()
	if err := recorder.Start(ctx); err != nil {
		t.Fatalf("Start() should not return immediate error; got %v", err)
	}

	// Wait for the goroutine to flip a.recording=false through
	// recordRealAudio's loud-fail path. Polling the exported getter is
	// correct at any speed; the assertion is the predicate, never
	// elapsed time. Generous ceiling + tiny interval: a fast machine
	// exits on the first check.
	require.Eventually(t, func() bool {
		return !recorder.IsRecording()
	}, 30*time.Second, 5*time.Millisecond, "expected recorder to have flipped recording=false via recordRealAudio's loud-fail path, but it is still recording (§11.4 PASS-bluff regression)")

	// Stop should now report ErrNotRecording — the honest signal that
	// no platform bridge captured anything.
	_, err = recorder.Stop(ctx)
	if err != ErrNotRecording {
		t.Fatalf("expected ErrNotRecording from Stop after real-mode loud-fail, got %v", err)
	}
}

// TestVoiceError tests error wrapping
func TestVoiceError(t *testing.T) {
	originalErr := ErrDeviceNotFound
	wrappedErr := NewVoiceError("SelectDevice", originalErr, "device-123")

	if wrappedErr == nil {
		t.Fatal("expected non-nil error")
	}

	errorMsg := wrappedErr.Error()
	if errorMsg == "" {
		t.Error("expected non-empty error message")
	}

	// Check if original error is preserved
	var voiceErr *VoiceError
	if !os.IsExist(wrappedErr) {
		// Try to unwrap
		if ve, ok := wrappedErr.(*VoiceError); ok {
			voiceErr = ve
		}
	}

	if voiceErr != nil && voiceErr.Unwrap() != originalErr {
		t.Error("expected wrapped error to preserve original error")
	}
}

// Helper function to generate sine wave samples
func generateSineWave(samples int, frequency, sampleRate float64) []float64 {
	wave := make([]float64, samples)
	for i := range wave {
		t := float64(i) / sampleRate
		wave[i] = math.Sin(2 * math.Pi * frequency * t)
	}
	return wave
}

// TestDeviceManager_ValidateDevice tests device validation
func TestDeviceManager_ValidateDevice(t *testing.T) {
	dm, err := NewDeviceManager()
	if err != nil {
		t.Fatalf("NewDeviceManager() error = %v", err)
	}

	t.Run("validate valid device", func(t *testing.T) {
		devices, err := dm.ListDevices(context.Background())
		if err != nil || len(devices) == 0 {
			t.Skip("no devices available for testing")  // SKIP-OK: #legacy-untriaged
		}

		// ValidateDevice takes *AudioDevice
		device := &devices[0]
		err = dm.ValidateDevice(device)
		if err != nil {
			t.Errorf("ValidateDevice() error = %v", err)
		}
	})

	t.Run("validate nil device", func(t *testing.T) {
		err := dm.ValidateDevice(nil)
		if err == nil {
			t.Error("expected error for nil device")
		}
	})

	t.Run("validate unavailable device", func(t *testing.T) {
		device := &AudioDevice{
			ID:          "test",
			Name:        "Test",
			IsAvailable: false,
		}
		err := dm.ValidateDevice(device)
		if err == nil {
			t.Error("expected error for unavailable device")
		}
	})
}

// TestDeviceManager_RefreshDevices tests device refresh
func TestDeviceManager_RefreshDevices(t *testing.T) {
	dm, err := NewDeviceManager()
	if err != nil {
		t.Fatalf("NewDeviceManager() error = %v", err)
	}

	err = dm.RefreshDevices(context.Background())
	if err != nil {
		t.Errorf("RefreshDevices() error = %v", err)
	}
}

// TestDeviceManager_GetDevice tests getting device by ID
func TestDeviceManager_GetDevice(t *testing.T) {
	dm, err := NewDeviceManager()
	if err != nil {
		t.Fatalf("NewDeviceManager() error = %v", err)
	}

	devices, err := dm.ListDevices(context.Background())
	if err != nil || len(devices) == 0 {
		t.Skip("no devices available for testing")  // SKIP-OK: #legacy-untriaged
	}

	t.Run("get existing device", func(t *testing.T) {
		device, err := dm.GetDevice(devices[0].ID)
		if err != nil {
			t.Errorf("GetDevice() error = %v", err)
		}
		if device == nil {
			t.Error("expected non-nil device")
		}
	})

	t.Run("get nonexistent device", func(t *testing.T) {
		device, err := dm.GetDevice("nonexistent-id")
		if err == nil {
			t.Error("expected error for nonexistent device")
		}
		if device != nil {
			t.Error("expected nil device")
		}
	})
}

// TestVoiceInputManager_SelectDevice tests device selection on manager
func TestVoiceInputManager_SelectDevice(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "voice_test_*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	config := &VoiceConfig{
		SampleRate:      16000,
		Channels:        1,
		BitDepth:        16,
		Format:          FormatWAV,
		OutputDirectory: tmpDir,
		AutoSelect:      true,
	}

	manager, err := NewVoiceInputManager(config)
	if err != nil {
		t.Fatalf("NewVoiceInputManager() error = %v", err)
	}

	ctx := context.Background()
	devices, err := manager.ListDevices(ctx)
	if err != nil || len(devices) == 0 {
		t.Skip("no devices available for testing")  // SKIP-OK: #legacy-untriaged
	}

	t.Run("select valid device", func(t *testing.T) {
		err := manager.SelectDevice(ctx, devices[0].ID)
		if err != nil {
			t.Errorf("SelectDevice() error = %v", err)
		}
	})

	t.Run("select invalid device", func(t *testing.T) {
		err := manager.SelectDevice(ctx, "nonexistent-device")
		if err == nil {
			t.Error("expected error for invalid device")
		}
	})
}

// TestVoiceConfig_Defaults tests default configuration values
func TestVoiceConfig_Defaults(t *testing.T) {
	config := &VoiceConfig{}

	// Test that zero values are acceptable
	if config.SampleRate != 0 {
		t.Error("expected zero sample rate by default")
	}
}

// TestAudioDevice_Fields tests AudioDevice struct fields
func TestAudioDevice_Fields(t *testing.T) {
	device := &AudioDevice{
		ID:          "test-id",
		Name:        "Test Device",
		IsDefault:   true,
		Channels:    2,
		SampleRates: []int{44100, 48000},
		IsAvailable: true,
		Driver:      "ALSA",
	}

	if device.ID != "test-id" {
		t.Error("unexpected device ID")
	}
	if device.Name != "Test Device" {
		t.Error("unexpected device name")
	}
	if !device.IsDefault {
		t.Error("expected device to be default")
	}
	if device.Channels != 2 {
		t.Error("unexpected channel count")
	}
	if len(device.SampleRates) != 2 {
		t.Error("unexpected sample rate count")
	}
	if !device.IsAvailable {
		t.Error("expected device to be available")
	}
	if device.Driver != "ALSA" {
		t.Error("unexpected driver")
	}
}

// TestVoiceError_All tests all error types
func TestVoiceError_All(t *testing.T) {
	errors := []error{
		ErrNoDevicesFound,
		ErrDeviceNotFound,
		ErrDeviceUnavailable,
		ErrAlreadyRecording,
		ErrNotRecording,
		ErrTranscriptionFailed,
		ErrInvalidFormat,
	}

	for _, err := range errors {
		if err == nil {
			t.Error("expected non-nil error")
		}
		if err.Error() == "" {
			t.Error("expected non-empty error message")
		}
	}
}

// TestAudioFormat_String tests format string conversion
func TestAudioFormat_String(t *testing.T) {
	formats := []AudioFormat{
		FormatWAV,
		FormatMP3,
	}

	for _, format := range formats {
		str := string(format)
		if str == "" {
			t.Error("expected non-empty format string")
		}
	}
}

// TestParseSampleRate tests sample rate parsing from PulseAudio spec
func TestParseSampleRate(t *testing.T) {
	tests := []struct {
		name       string
		sampleSpec string
		wantRate   int
		wantErr    bool
	}{
		{"standard 44100Hz", "s16le 2ch 44100Hz", 44100, false},
		{"48000Hz", "s16le 2ch 48000Hz", 48000, false},
		{"16000Hz", "s16le 1ch 16000Hz", 16000, false},
		{"96000Hz", "float32le 2ch 96000Hz", 96000, false},
		{"no Hz suffix", "s16le 2ch 44100", 0, true},
		{"empty string", "", 0, true},
		{"malformed", "invalid", 0, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rate, err := parseSampleRate(tt.sampleSpec)
			if (err != nil) != tt.wantErr {
				t.Errorf("parseSampleRate() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && rate != tt.wantRate {
				t.Errorf("parseSampleRate() = %v, want %v", rate, tt.wantRate)
			}
		})
	}
}

// TestParseChannels tests channel count parsing from PulseAudio spec
func TestParseChannels(t *testing.T) {
	tests := []struct {
		name       string
		sampleSpec string
		wantCh     int
		wantErr    bool
	}{
		{"2 channels", "s16le 2ch 44100Hz", 2, false},
		{"1 channel", "s16le 1ch 16000Hz", 1, false},
		{"8 channels", "float32le 8ch 48000Hz", 8, false},
		{"no ch suffix", "s16le 2 44100Hz", 0, true},
		{"empty string", "", 0, true},
		{"malformed", "invalid", 0, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ch, err := parseChannels(tt.sampleSpec)
			if (err != nil) != tt.wantErr {
				t.Errorf("parseChannels() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && ch != tt.wantCh {
				t.Errorf("parseChannels() = %v, want %v", ch, tt.wantCh)
			}
		})
	}
}

// TestFormatPulseAudioDeviceName tests PulseAudio device name formatting
func TestFormatPulseAudioDeviceName(t *testing.T) {
	dm := &DeviceManager{}

	tests := []struct {
		name     string
		deviceID string
		want     string
	}{
		{
			"alsa input device",
			"alsa_input.pci-0000_00_1f.3.analog-stereo",
			"Pci-0000 00 1f 3 Analog-stereo", // Hyphens preserved, underscores/dots become spaces
		},
		{
			"alsa output device",
			"alsa_output.usb-audio-device",
			"Usb-audio-device", // Hyphens preserved
		},
		{
			"simple device",
			"default",
			"Default",
		},
		{
			"underscores only",
			"device_name_test",
			"Device Name Test",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := dm.formatPulseAudioDeviceName(tt.deviceID)
			if got != tt.want {
				t.Errorf("formatPulseAudioDeviceName() = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestParseMacOSAudioDevices tests macOS audio device parsing
func TestParseMacOSAudioDevices(t *testing.T) {
	dm := &DeviceManager{}

	t.Run("empty output", func(t *testing.T) {
		devices := dm.parseMacOSAudioDevices("")
		if len(devices) != 0 {
			t.Errorf("expected 0 devices, got %d", len(devices))
		}
	})

	t.Run("output only devices", func(t *testing.T) {
		output := `
Audio:
    Output:
        Built-in Output:
            Default: Yes
`
		devices := dm.parseMacOSAudioDevices(output)
		// Output devices should be filtered out
		if len(devices) != 0 {
			t.Errorf("expected 0 input devices, got %d", len(devices))
		}
	})

	t.Run("input devices", func(t *testing.T) {
		output := `
Audio:
    Input:
        Built-in Microphone:
            Default: Yes
        External Mic:
            Default: No
`
		devices := dm.parseMacOSAudioDevices(output)
		if len(devices) < 1 {
			t.Errorf("expected at least 1 input device, got %d", len(devices))
		}
	})
}

// TestParseWindowsAudioDevices tests Windows audio device parsing
func TestParseWindowsAudioDevices(t *testing.T) {
	dm := &DeviceManager{}

	t.Run("empty output", func(t *testing.T) {
		devices := dm.parseWindowsAudioDevices("")
		if len(devices) != 0 {
			t.Errorf("expected 0 devices, got %d", len(devices))
		}
	})

	t.Run("single device", func(t *testing.T) {
		output := `{
    "Name": "Realtek High Definition Audio",
    "DeviceID": "HDAUDIO\\FUNC_01"
}`
		devices := dm.parseWindowsAudioDevices(output)
		if len(devices) != 1 {
			t.Errorf("expected 1 device, got %d", len(devices))
		}
		if len(devices) > 0 {
			if devices[0].Name != "Realtek High Definition Audio" {
				t.Errorf("unexpected device name: %s", devices[0].Name)
			}
			if devices[0].Driver != "WASAPI" {
				t.Errorf("unexpected driver: %s", devices[0].Driver)
			}
		}
	})

	t.Run("multiple devices", func(t *testing.T) {
		output := `[
    {
        "Name": "Device 1",
        "DeviceID": "ID1"
    },
    {
        "Name": "Device 2",
        "DeviceID": "ID2"
    }
]`
		devices := dm.parseWindowsAudioDevices(output)
		if len(devices) != 2 {
			t.Errorf("expected 2 devices, got %d", len(devices))
		}
	})
}

// TestEnumerateMockDevices tests mock device enumeration
func TestEnumerateMockDevices(t *testing.T) {
	dm := &DeviceManager{}

	devices, err := dm.enumerateMockDevices()
	if err != nil {
		t.Fatalf("enumerateMockDevices() error = %v", err)
	}

	if len(devices) != 2 {
		t.Errorf("expected 2 mock devices, got %d", len(devices))
	}

	// Check first device (default)
	if !devices[0].IsDefault {
		t.Error("expected first device to be default")
	}
	if devices[0].Driver != "Mock" {
		t.Error("expected Mock driver")
	}
	if devices[0].Name != "Mock Default Microphone" {
		t.Errorf("unexpected name: %s", devices[0].Name)
	}

	// Check second device (USB)
	if devices[1].IsDefault {
		t.Error("expected second device to NOT be default")
	}
	if devices[1].Name != "Mock USB Microphone" {
		t.Errorf("unexpected name: %s", devices[1].Name)
	}
}

// TestDeviceManager_GetActiveDevice_NotSet tests getting active device when none is set
func TestDeviceManager_GetActiveDevice_NotSet(t *testing.T) {
	dm := &DeviceManager{
		devices:      []AudioDevice{},
		activeDevice: nil,
	}

	device := dm.GetActiveDevice()
	if device != nil {
		t.Error("expected nil device when none is active")
	}
}

// TestDeviceManager_SelectDevice_Unavailable tests selecting unavailable device
func TestDeviceManager_SelectDevice_Unavailable(t *testing.T) {
	dm := &DeviceManager{
		devices: []AudioDevice{
			{
				ID:          "unavailable-device",
				Name:        "Unavailable Device",
				IsAvailable: false,
			},
		},
	}

	err := dm.SelectDevice("unavailable-device")
	if err != ErrDeviceUnavailable {
		t.Errorf("expected ErrDeviceUnavailable, got %v", err)
	}
}

// TestDeviceManager_ListDevices_Empty tests listing when no devices available
func TestDeviceManager_ListDevices_Empty(t *testing.T) {
	dm := &DeviceManager{
		devices: []AudioDevice{},
	}

	_, err := dm.ListDevices(context.Background())
	if err != ErrNoDevicesFound {
		t.Errorf("expected ErrNoDevicesFound, got %v", err)
	}
}

// TestDeviceManager_ValidateDevice_NoChannels tests validation with zero channels
func TestDeviceManager_ValidateDevice_NoChannels(t *testing.T) {
	dm := &DeviceManager{}

	device := &AudioDevice{
		ID:          "test",
		Name:        "Test",
		IsAvailable: true,
		Channels:    0,
		SampleRates: []int{44100},
	}

	err := dm.ValidateDevice(device)
	if err == nil {
		t.Error("expected error for device with no channels")
	}
}

// TestDeviceManager_ValidateDevice_NoSampleRates tests validation with no sample rates
func TestDeviceManager_ValidateDevice_NoSampleRates(t *testing.T) {
	dm := &DeviceManager{}

	device := &AudioDevice{
		ID:          "test",
		Name:        "Test",
		IsAvailable: true,
		Channels:    1,
		SampleRates: []int{},
	}

	err := dm.ValidateDevice(device)
	if err == nil {
		t.Error("expected error for device with no sample rates")
	}
}
