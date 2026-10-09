package llm

import (
	"errors"
	"strings"
	"testing"
	"time"
)

// TestNewProvider_AllProviderTypes tests that NewProvider creates providers for all types
func TestNewProvider_AllProviderTypes(t *testing.T) {
	// §11.4.120 gate reconciliation: NewProvider now enforces the W2c-1 cloud
	// gate (factory.go), closing the ungated back door around NewCloudProvider's
	// guard. This test's subject is provider CONSTRUCTION and behaviour, which
	// predates the gate — so the refusal it now hits is the fix working, not a
	// regression, and the honest response is to open the gate here rather than
	// weaken either side. Gate POLICY itself is asserted by
	// cloud_gate_closed_test.go / cloud_gate_open_test.go.
	openCloudGateForTest(t)

	tests := []struct {
		name         string
		providerType ProviderType
		endpoint     string
		apiKey       string
		wantErr      bool
	}{
		{
			name:         "OpenAI provider",
			providerType: ProviderTypeOpenAI,
			endpoint:     "https://api.openai.com/v1",
			apiKey:       "test-key",
			wantErr:      false,
		},
		{
			name:         "Anthropic provider",
			providerType: ProviderTypeAnthropic,
			endpoint:     "https://api.anthropic.com/v1",
			apiKey:       "test-key",
			wantErr:      false,
		},
		{
			name:         "Gemini provider",
			providerType: ProviderTypeGemini,
			endpoint:     "https://generativelanguage.googleapis.com/v1",
			apiKey:       "test-key",
			wantErr:      false,
		},
		{
			name:         "Ollama provider",
			providerType: ProviderTypeOllama,
			endpoint:     "http://localhost:11434",
			wantErr:      false,
		},
		{
			name:         "LlamaCpp provider",
			providerType: ProviderTypeLlamaCpp,
			endpoint:     "http://localhost:8080",
			wantErr:      false,
		},
		{
			name:         "Qwen provider",
			providerType: ProviderTypeQwen,
			endpoint:     "http://localhost:8000",
			apiKey:       "test-key",
			wantErr:      false,
		},
		{
			name:         "XAI provider",
			providerType: ProviderTypeXAI,
			endpoint:     "https://api.x.ai/v1",
			apiKey:       "test-key",
			wantErr:      false,
		},
		{
			name:         "OpenRouter provider",
			providerType: ProviderTypeOpenRouter,
			endpoint:     "https://openrouter.ai/api/v1",
			apiKey:       "test-key",
			wantErr:      false,
		},
		{
			name:         "Copilot provider",
			providerType: ProviderTypeCopilot,
			endpoint:     "https://api.githubcopilot.com/v1",
			apiKey:       "test-key",
			wantErr:      true, // Requires valid GitHub token exchange
		},
		{
			name:         "Azure provider",
			providerType: ProviderTypeAzure,
			endpoint:     "https://test.openai.azure.com",
			apiKey:       "test-key",
			wantErr:      true, // Requires AZURE_OPENAI_ENDPOINT env var
		},
		{
			name:         "Bedrock provider",
			providerType: ProviderTypeBedrock,
			endpoint:     "https://bedrock.us-east-1.amazonaws.com",
			wantErr:      false,
		},
		{
			name:         "VertexAI provider",
			providerType: ProviderTypeVertexAI,
			endpoint:     "https://us-central1-aiplatform.googleapis.com",
			apiKey:       "test-key",
			wantErr:      true, // Requires Google Cloud credentials
		},
		{
			name:         "Groq provider",
			providerType: ProviderTypeGroq,
			endpoint:     "https://api.groq.com/openai/v1",
			apiKey:       "test-key",
			wantErr:      false,
		},
		{
			name:         "VLLM provider",
			providerType: ProviderTypeVLLM,
			endpoint:     "http://localhost:8000",
			wantErr:      false,
		},
		{
			name:         "LocalAI provider",
			providerType: ProviderTypeLocalAI,
			endpoint:     "http://localhost:8080",
			wantErr:      false,
		},
		{
			name:         "FastChat provider",
			providerType: ProviderTypeFastChat,
			endpoint:     "http://localhost:21002",
			wantErr:      false,
		},
		{
			name:         "TextGen provider",
			providerType: ProviderTypeTextGen,
			endpoint:     "http://localhost:5000",
			wantErr:      false,
		},
		{
			name:         "LMStudio provider",
			providerType: ProviderTypeLMStudio,
			endpoint:     "http://localhost:1234",
			wantErr:      false,
		},
		{
			name:         "Jan provider",
			providerType: ProviderTypeJan,
			endpoint:     "http://localhost:1337",
			wantErr:      false,
		},
		{
			name:         "GPT4All provider",
			providerType: ProviderTypeGPT4All,
			endpoint:     "http://localhost:4891",
			wantErr:      false,
		},
		{
			name:         "TabbyAPI provider",
			providerType: ProviderTypeTabbyAPI,
			endpoint:     "http://localhost:5000",
			wantErr:      false,
		},
		{
			name:         "MLX provider",
			providerType: ProviderTypeMLX,
			endpoint:     "http://localhost:8000",
			wantErr:      false,
		},
		{
			name:         "MistralRS provider",
			providerType: ProviderTypeMistralRS,
			endpoint:     "http://localhost:8000",
			wantErr:      false,
		},
		{
			name:         "KoboldAI provider",
			providerType: ProviderTypeKoboldAI,
			endpoint:     "http://localhost:5001",
			wantErr:      false,
		},
		{
			name:         "unsupported provider type",
			providerType: ProviderType("unsupported"),
			endpoint:     "http://localhost:8080",
			wantErr:      true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Anti-bluff (§11.4.115): make the Azure subtest hermetic against
			// the ambient environment. AZURE_OPENAI_ENDPOINT is exported by
			// .env.full-test for the full-infra test stack (test-infra-up /
			// test-full); without this isolation, NewAzureProvider legitimately
			// picks up that env-var fallback (see
			// TestAzureProvider_EndpointPrecedence_EnvWinsOverError in
			// azure_provider_audit_test.go — that IS intended behavior) and
			// the "requires AZURE_OPENAI_ENDPOINT env var" wantErr:true
			// assumption below breaks. t.Setenv clears the var for this
			// subtest only and auto-restores it afterward, so the result is
			// deterministic regardless of ambient env state (same class of
			// fix as TestAzureProvider_NewWithoutEndpoint).
			if tt.providerType == ProviderTypeAzure {
				t.Setenv("AZURE_OPENAI_ENDPOINT", "")
			}

			config := ProviderConfigEntry{
				Type:     tt.providerType,
				Endpoint: tt.endpoint,
				APIKey:   tt.apiKey,
				Enabled:  true,
				Parameters: map[string]interface{}{
					"timeout": 30.0,
				},
			}

			provider, err := NewProvider(config)
			if (err != nil) != tt.wantErr {
				t.Errorf("NewProvider() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !tt.wantErr && provider == nil {
				t.Error("NewProvider() returned nil provider")
			}

			// Note: Some providers return different types internally (e.g., Ollama returns "local")
			// So we don't check GetType() matches the config type - just verify provider is created
		})
	}
}

// TestNewProvider_OllamaWithModels tests Ollama provider with custom models
func TestNewProvider_OllamaWithModels(t *testing.T) {
	config := ProviderConfigEntry{
		Type:     ProviderTypeOllama,
		Endpoint: "http://localhost:11434",
		Enabled:  true,
		Models:   []string{"llama3", "codellama"},
		Parameters: map[string]interface{}{
			"timeout": 60.0,
		},
	}

	provider, err := NewProvider(config)
	if err != nil {
		t.Fatalf("NewProvider() error = %v", err)
	}

	if provider == nil {
		t.Fatal("NewProvider() returned nil provider")
	}

	// Note: Ollama provider returns ProviderTypeLocal internally
	// Just verify provider was created successfully
}

// TestNewProvider_LlamaCppWithParameters tests LlamaCpp provider with parameters
func TestNewProvider_LlamaCppWithParameters(t *testing.T) {
	config := ProviderConfigEntry{
		Type:     ProviderTypeLlamaCpp,
		Endpoint: "http://localhost:8080",
		Enabled:  true,
		Models:   []string{"/models/llama-3-8b.gguf"},
		Parameters: map[string]interface{}{
			"context_size": 8192.0,
			"gpu_enabled":  true,
		},
	}

	provider, err := NewProvider(config)
	if err != nil {
		t.Fatalf("NewProvider() error = %v", err)
	}

	if provider == nil {
		t.Fatal("NewProvider() returned nil provider")
	}

	// Note: LlamaCpp provider returns ProviderTypeLocal internally
	// Just verify provider was created successfully
}

// TestNewProvider_KoboldAIWithParameters tests KoboldAI provider with parameters
func TestNewProvider_KoboldAIWithParameters(t *testing.T) {
	config := ProviderConfigEntry{
		Type:     ProviderTypeKoboldAI,
		Endpoint: "http://localhost:5001",
		Enabled:  true,
		APIKey:   "test-api-key",
		Models:   []string{"default-model"},
		Parameters: map[string]interface{}{
			"timeout": 90.0,
		},
	}

	provider, err := NewProvider(config)
	if err != nil {
		t.Fatalf("NewProvider() error = %v", err)
	}

	if provider == nil {
		t.Fatal("NewProvider() returned nil provider")
	}

	if provider.GetType() != ProviderTypeKoboldAI {
		t.Errorf("Provider type = %v, want %v", provider.GetType(), ProviderTypeKoboldAI)
	}
}

// TestInitializeModelManager_Factory tests model manager initialization from factory
func TestInitializeModelManager_Factory(t *testing.T) {
	// §11.4.120 gate reconciliation: NewProvider now enforces the W2c-1 cloud
	// gate (factory.go), closing the ungated back door around NewCloudProvider's
	// guard. This test's subject is provider CONSTRUCTION and behaviour, which
	// predates the gate — so the refusal it now hits is the fix working, not a
	// regression, and the honest response is to open the gate here rather than
	// weaken either side. Gate POLICY itself is asserted by
	// cloud_gate_closed_test.go / cloud_gate_open_test.go.
	openCloudGateForTest(t)

	configs := []ProviderConfigEntry{
		{
			Type:     ProviderTypeOllama,
			Endpoint: "http://localhost:11434",
			Enabled:  true,
		},
		{
			Type:     ProviderTypeOpenAI,
			Endpoint: "https://api.openai.com/v1",
			APIKey:   "test-key", // Provide test key to avoid error
			Enabled:  true,
		},
		{
			Type:     ProviderTypeAnthropic,
			Endpoint: "https://api.anthropic.com/v1",
			Enabled:  false, // Disabled, should be skipped
		},
	}

	manager, err := InitializeModelManager(configs)
	if err != nil {
		t.Fatalf("InitializeModelManager() error = %v", err)
	}

	if manager == nil {
		t.Fatal("InitializeModelManager() returned nil manager")
	}

	// Manager should be initialized (we can't check provider count directly)
	// but we verify it doesn't panic and returns valid manager
}

// TestInitializeModelManager_UnsupportedProvider_Factory tests error handling for unsupported provider
func TestInitializeModelManager_UnsupportedProvider_Factory(t *testing.T) {
	// §11.4.120 gate reconciliation, second round. This test used to call
	// openCloudGateForTest(t) with the note "with the gate CLOSED this test
	// would still PASS — but for the wrong reason": the gate ran ahead of the
	// type switch, so an unknown type came back as ErrCloudDisabled and the
	// bare `err != nil` assertion was satisfied without the unsupported-type
	// path ever being reached. Opening the gate was a workaround for that
	// ordering; a test that has to disable a policy to observe its own subject
	// is reporting on the workaround, not on the code.
	//
	// NewProvider now decides type validity BEFORE gate policy, so the gate is
	// no longer in the way and the workaround is removed rather than kept:
	// this runs with the gate at whatever the suite's ambient state is, and
	// the two subtests below pin it EXPLICITLY in each direction to prove the
	// verdict does not depend on it.
	configs := []ProviderConfigEntry{
		{
			Type:     ProviderType("unsupported"),
			Endpoint: "http://localhost:8080",
			Enabled:  true,
		},
	}

	manager, err := InitializeModelManager(configs)
	if err == nil {
		t.Error("InitializeModelManager() should return error for unsupported provider")
	}

	if manager != nil {
		t.Error("InitializeModelManager() should return nil manager on error")
	}
}

// TestNewProvider_UnknownTypeReportsUnknownRegardlessOfGate is the guard for
// the ordering fix in NewProvider: an unrecognised provider type must be
// diagnosed as UNRECOGNISED whether the cloud gate is open or closed.
//
// The defect it pins: the gate check used to run before the type switch, and
// an unknown type is (correctly) absent from the exemption list, so with the
// gate closed `ProviderType("definitely-not-a-provider")` was reported as
//
//	ErrCloudDisabled: ... hosted provider "definitely-not-a-provider" will not
//	be constructed. Set llm.cloud.enabled: true ...
//
// — inventing a "hosted provider" that does not exist and directing the reader
// at a config flag instead of at their typo. Toggling llm.cloud.enabled would
// not have helped: the type is unknown in both states.
//
// THE CLOSED-GATE SUBTEST IS THE LOAD-BEARING ONE — it fails on the pre-fix
// ordering. The open-gate subtest is the positive control that the fix did not
// simply move the problem: unknown must still be unknown there too.
//
// SECURITY POSTURE IS ASSERTED, NOT ASSUMED. A cheap way to "fix" the message
// would be to let unknown types fall through the gate entirely — so the third
// subtest re-checks that a genuinely HOSTED type is still refused with
// ErrCloudDisabled while the gate is closed. Without it this guard would pass
// on a build that had quietly opened the gate for everything.
//
// FALSIFYING MUTATION (§1.1): move the `if !cloudGate.Load() && ...` block in
// NewProvider back above the providerConstructorFor lookup. The closed-gate
// subtest then observes ErrCloudDisabled instead of "unsupported provider
// type" and FAILS.
func TestNewProvider_UnknownTypeReportsUnknownRegardlessOfGate(t *testing.T) {
	const unknown = ProviderType("definitely-not-a-provider")

	assertUnknown := func(t *testing.T) {
		t.Helper()
		prov, err := NewProvider(ProviderConfigEntry{
			Type:     unknown,
			Endpoint: "http://127.0.0.1:1", // never dialled; construction is refused first
			Enabled:  true,
		})
		if err == nil {
			t.Fatalf("NewProvider(%q) returned no error — an unknown type must "+
				"never construct", unknown)
		}
		if prov != nil {
			t.Fatalf("NewProvider(%q) returned a non-nil Provider (%T) alongside "+
				"the error — a refused construction must yield nothing usable",
				unknown, prov)
		}
		if errors.Is(err, ErrCloudDisabled) {
			t.Fatalf("NewProvider(%q) reported the cloud gate (%v) for a type that "+
				"does not exist. An unknown type is unknown in both gate states; "+
				"reporting it as a hosted-provider refusal names a provider that "+
				"was never defined and sends the reader after a config flag "+
				"instead of a typo", unknown, err)
		}
		if !strings.Contains(err.Error(), "unsupported provider type") {
			t.Fatalf("NewProvider(%q) error = %q, want it to say \"unsupported "+
				"provider type\"", unknown, err)
		}
	}

	t.Run("gate closed", func(t *testing.T) {
		closeCloudGateForTest(t) // cloud_gate_endpoint_locality_test.go
		assertUnknown(t)
	})

	t.Run("gate open", func(t *testing.T) {
		openCloudGateForTest(t)
		assertUnknown(t)
	})

	t.Run("known hosted type is still gated when closed", func(t *testing.T) {
		closeCloudGateForTest(t)
		prov, err := NewProvider(ProviderConfigEntry{
			Type:    ProviderTypeOpenAI,
			Enabled: true,
		})
		if err == nil {
			t.Fatal("NewProvider(openai) constructed with the cloud gate closed — " +
				"reordering the type check ahead of the gate must not have " +
				"weakened the gate itself")
		}
		if prov != nil {
			t.Fatalf("NewProvider(openai) returned a non-nil Provider (%T) with the "+
				"gate closed", prov)
		}
		if !errors.Is(err, ErrCloudDisabled) {
			t.Fatalf("NewProvider(openai) error = %v, want it to wrap ErrCloudDisabled", err)
		}
	})
}

// TestInitializeModelManager_EmptyConfigs_Factory tests with empty configuration
func TestInitializeModelManager_EmptyConfigs_Factory(t *testing.T) {
	manager, err := InitializeModelManager([]ProviderConfigEntry{})
	if err != nil {
		t.Fatalf("InitializeModelManager() error = %v", err)
	}

	if manager == nil {
		t.Fatal("InitializeModelManager() returned nil manager")
	}
}

// TestNewProvider_DefaultTimeout tests default timeout when not specified
func TestNewProvider_DefaultTimeout(t *testing.T) {
	config := ProviderConfigEntry{
		Type:       ProviderTypeOllama,
		Endpoint:   "http://localhost:11434",
		Enabled:    true,
		Parameters: map[string]interface{}{}, // No timeout specified
	}

	provider, err := NewProvider(config)
	if err != nil {
		t.Fatalf("NewProvider() error = %v", err)
	}

	if provider == nil {
		t.Fatal("NewProvider() returned nil provider")
	}

	// Verify provider was created (default timeout is used internally)
	// Note: Ollama returns ProviderTypeLocal
}

// TestNewProvider_CloseProvider tests that providers can be closed
func TestNewProvider_CloseProvider(t *testing.T) {
	// §11.4.120 gate reconciliation: NewProvider now enforces the W2c-1 cloud
	// gate (factory.go), closing the ungated back door around NewCloudProvider's
	// guard. This test's subject is provider CONSTRUCTION and behaviour, which
	// predates the gate — so the refusal it now hits is the fix working, not a
	// regression, and the honest response is to open the gate here rather than
	// weaken either side. Gate POLICY itself is asserted by
	// cloud_gate_closed_test.go / cloud_gate_open_test.go.
	openCloudGateForTest(t)

	config := ProviderConfigEntry{
		Type:     ProviderTypeOpenAI,
		Endpoint: "https://api.openai.com/v1",
		APIKey:   "test-key", // Provide test key
		Enabled:  true,
	}

	provider, err := NewProvider(config)
	if err != nil {
		t.Fatalf("NewProvider() error = %v", err)
	}

	// Close should not panic
	err = provider.Close()
	if err != nil {
		t.Errorf("Provider.Close() error = %v", err)
	}
}

// TestNewProvider_ProviderCapabilities tests that providers return capabilities
func TestNewProvider_ProviderCapabilities(t *testing.T) {
	// §11.4.120 gate reconciliation: NewProvider now enforces the W2c-1 cloud
	// gate (factory.go), closing the ungated back door around NewCloudProvider's
	// guard. This test's subject is provider CONSTRUCTION and behaviour, which
	// predates the gate — so the refusal it now hits is the fix working, not a
	// regression, and the honest response is to open the gate here rather than
	// weaken either side. Gate POLICY itself is asserted by
	// cloud_gate_closed_test.go / cloud_gate_open_test.go.
	openCloudGateForTest(t)

	tests := []struct {
		name         string
		providerType ProviderType
		endpoint     string
		apiKey       string
	}{
		{"OpenAI", ProviderTypeOpenAI, "https://api.openai.com/v1", "test-key"},
		{"Anthropic", ProviderTypeAnthropic, "https://api.anthropic.com/v1", "test-key"},
		{"Ollama", ProviderTypeOllama, "http://localhost:11434", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := ProviderConfigEntry{
				Type:     tt.providerType,
				Endpoint: tt.endpoint,
				APIKey:   tt.apiKey,
				Enabled:  true,
			}

			provider, err := NewProvider(config)
			if err != nil {
				t.Fatalf("NewProvider() error = %v", err)
			}

			caps := provider.GetCapabilities()
			if caps == nil {
				t.Error("GetCapabilities() returned nil")
			}

			// Each provider should report at least one capability
			if len(caps) == 0 {
				t.Error("GetCapabilities() returned empty capabilities")
			}
		})
	}
}

// TestNewProvider_ProviderName tests that providers return names
func TestNewProvider_ProviderName(t *testing.T) {
	// §11.4.120 gate reconciliation: NewProvider now enforces the W2c-1 cloud
	// gate (factory.go), closing the ungated back door around NewCloudProvider's
	// guard. This test's subject is provider CONSTRUCTION and behaviour, which
	// predates the gate — so the refusal it now hits is the fix working, not a
	// regression, and the honest response is to open the gate here rather than
	// weaken either side. Gate POLICY itself is asserted by
	// cloud_gate_closed_test.go / cloud_gate_open_test.go.
	openCloudGateForTest(t)

	config := ProviderConfigEntry{
		Type:     ProviderTypeOpenAI,
		Endpoint: "https://api.openai.com/v1",
		APIKey:   "test-key",
		Enabled:  true,
	}

	provider, err := NewProvider(config)
	if err != nil {
		t.Fatalf("NewProvider() error = %v", err)
	}

	name := provider.GetName()
	if name == "" {
		t.Error("GetName() returned empty string")
	}
}

// TestNewProvider_ProviderModels tests that providers return models
func TestNewProvider_ProviderModels(t *testing.T) {
	// §11.4.120 gate reconciliation: NewProvider now enforces the W2c-1 cloud
	// gate (factory.go), closing the ungated back door around NewCloudProvider's
	// guard. This test's subject is provider CONSTRUCTION and behaviour, which
	// predates the gate — so the refusal it now hits is the fix working, not a
	// regression, and the honest response is to open the gate here rather than
	// weaken either side. Gate POLICY itself is asserted by
	// cloud_gate_closed_test.go / cloud_gate_open_test.go.
	openCloudGateForTest(t)

	config := ProviderConfigEntry{
		Type:     ProviderTypeOpenAI,
		Endpoint: "https://api.openai.com/v1",
		APIKey:   "test-key",
		Enabled:  true,
	}

	provider, err := NewProvider(config)
	if err != nil {
		t.Fatalf("NewProvider() error = %v", err)
	}

	models := provider.GetModels()
	if models == nil {
		t.Error("GetModels() returned nil")
	}

	// OpenAI should have predefined models
	if len(models) == 0 {
		t.Error("GetModels() returned empty models list")
	}
}

// Ensure time package is used
var _ = time.Second
