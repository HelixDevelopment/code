package llm

import (
	"fmt"
	"time"
)

// isCloudGateExemptProviderType reports whether NewProvider may construct t
// without consulting the W2c-1 cloud gate. Two disjoint reasons for exemption,
// and nothing else:
//
//   - LOCAL BY IDENTITY — Ollama and llama.cpp only. This mirrors
//     isLocalProviderType in provider_factory.go, which NewCloudProvider
//     applies for the same reason. The load-bearing property is NOT that they
//     are "usually run locally" — it is that NEITHER CARRIES A CREDENTIAL PATH:
//     ollama_provider.go and llamacpp_provider.go contain zero occurrences of
//     APIKey, Authorization or Bearer between them, so exempting them by name
//     can leak nothing no matter what endpoint a caller supplies. Identity is
//     safe here precisely because there is no secret for a wrong endpoint to
//     receive. Do not extend this leg to a provider that can send a credential;
//     verify the absence, do not assume it from the provider's reputation.
//
//     KoboldAI USED TO BE ON THIS LEG AND NO LONGER IS. It attaches
//     `Authorization: Bearer <APIKey>` to every outbound request while taking
//     both endpoint and key from caller-supplied config, so identity-exemption
//     let a KoboldAI provider aimed at an arbitrary public host construct with
//     the gate CLOSED and ship the bearer token there. It now self-gates on
//     endpoint locality inside NewKoboldAIProvider and belongs to the leg
//     below.
//
//   - SELF-GATING ONE LEVEL DOWN — every arm below that reaches
//     NewOpenAICompatibleProvider (the vLLM/LocalAI/FastChat/TextGen/LM
//     Studio/Jan/GPT4All/TabbyAPI/MLX/mistral.rs family, and Xiaomi via its
//     embedded OpenAI-compatible provider), plus KoboldAI via the gate inside
//     NewKoboldAIProvider, is already gated THERE on endpoint
//     LOCALITY. Re-gating them by identity at this layer would refuse a local
//     vLLM — the exact local-serving breakage the gate exists to prevent —
//     while gating them not at all is safe, because a HOSTED base URL is still
//     refused by isLocalEndpointURL inside that constructor.
//
// The default is DENY: any type not named here — including a hosted type added
// to the switch below in future — is gated. That direction is deliberate. A new
// hosted arm is protected the moment it is written, whereas a new local arm
// fails loudly in its own tests until it is listed here, which is the cheaper
// of the two mistakes.
func isCloudGateExemptProviderType(t ProviderType) bool {
	switch t {
	case ProviderTypeOllama, ProviderTypeLlamaCpp:
		return true
	case ProviderTypeKoboldAI,
		ProviderTypeVLLM, ProviderTypeLocalAI, ProviderTypeFastChat,
		ProviderTypeTextGen, ProviderTypeLMStudio, ProviderTypeJan,
		ProviderTypeGPT4All, ProviderTypeTabbyAPI, ProviderTypeMLX,
		ProviderTypeMistralRS, ProviderTypeXiaomi:
		return true
	default:
		return false
	}
}

// NewProvider creates a new provider instance based on the configuration
// providerOrNil adapts a CONCRETE constructor's (*T, error) result to the
// (Provider, error) pair an interface-returning factory promises, without the
// typed-nil trap that `return NewXProvider(cfg)` walks straight into.
//
// The trap: every New<X>Provider in this package returns a concrete POINTER,
// and each returns (nil, err) on failure. Returning that pair directly from a
// function declared `(Provider, error)` boxes the nil *X into a Provider
// interface whose TYPE word is set — so the interface value is NOT nil, and a
// caller's `if prov != nil` guard passes and then panics on first method call.
//
// Measured on this package before this helper existed: with the cloud gate
// OPEN and no credentials in the environment, 13 of NewProvider's arms and 15
// of NewCloudProvider's returned (non-nil interface, non-nil error). No caller
// panicked, because every production call site checks err first — but that made
// the invariant a property of ~9 scattered call sites rather than of the two
// factories, and the same defect had already been found and fixed three times
// in this package (openai_compatible_catalogue.go,
// newOpenAICompatibleFromConfig, and the KoboldAI arm below). Routing every arm
// through one helper is what stops a fourth.
//
// An error means NOTHING USABLE comes back, full stop. The (nil, nil) case —
// a constructor that returns neither a provider nor an error — is a different
// defect and is defended separately by the callers that check for it.
func providerOrNil[T Provider](p T, err error) (Provider, error) {
	if err != nil {
		return nil, err
	}
	return p, nil
}

func NewProvider(config ProviderConfigEntry) (Provider, error) {
	// TYPE VALIDITY IS DECIDED FIRST, GATE POLICY SECOND. Both checks precede
	// any construction, but their ORDER is load-bearing for the diagnosis a
	// caller gets: an unrecognised type is an unrecognised type whether the
	// gate is open or shut, so it must never be reported as a cloud refusal.
	//
	// It used to be. The gate ran ahead of the switch, and since an unknown
	// type is (correctly) not on the exemption list, a typo'd
	// `ProviderType("openaii")` came back as
	//   `ErrCloudDisabled: ... hosted provider "openaii" will not be constructed`
	// — naming a "hosted provider" that does not exist, and sending the reader
	// to look for a config flag instead of at their spelling. The symptom that
	// exposed it: TestInitializeModelManager_UnsupportedProvider_Factory had to
	// OPEN the gate just to observe its own subject.
	//
	// providerConstructorFor is the SINGLE source of truth for "is this type
	// known" — there is no second list of type names here to drift out of sync
	// with the switch. A nil result means no arm exists.
	construct := providerConstructorFor(config)
	if construct == nil {
		return nil, fmt.Errorf("unsupported provider type: %s", config.Type)
	}

	// W2c-1 cloud gate (operator mandate 2026-09-05, local-only adaptive
	// serving) — the SAME guard NewCloudProvider applies in provider_factory.go,
	// applied here because this switch constructs twelve hosted backends
	// (OpenAI, Anthropic, Gemini, Qwen, XAI, OpenRouter, Copilot, Azure,
	// Bedrock, VertexAI, Groq, Replicate) and, until this check existed, did so
	// with no gate at all. That made NewProvider an ungated back door around
	// NewCloudProvider's guard — and not an obscure one: provider_factory.go's
	// own doc comments point callers here and doc.go advertises it as the
	// canonical constructor, while NewCloudProvider's comment claimed "no hosted
	// arm can construct by a back door". Closing it makes that claim true.
	//
	// SECURITY POSTURE IS UNCHANGED by the reordering above. Selecting the
	// constructor does NOT invoke it: `construct` is a closure that is called
	// only on the last line, after this check passes. No credential is
	// resolved, no endpoint is dialled, and no hosted provider is built before
	// the gate has had its say — a refusal is still about POLICY and still
	// never leaks whether a key happens to be present. Default-deny for
	// UNKNOWN types is likewise unchanged: an unknown type has no arm, so it
	// is refused outright above and can never reach a constructor whether the
	// gate is open or closed. The sentinel is shared with NewCloudProvider so
	// every caller branches on one error identity
	// (errors.Is(err, ErrCloudDisabled)).
	if !cloudGate.Load() && !isCloudGateExemptProviderType(config.Type) {
		return nil, fmt.Errorf("%w: llm.cloud.enabled is false (default); "+
			"hosted provider %q will not be constructed. Set "+
			"llm.cloud.enabled: true to permit cloud providers, or use the "+
			"local routes (local/helixllm coder, llamacpp, ollama)",
			ErrCloudDisabled, config.Type)
	}

	return construct()
}

// providerConstructorFor maps a provider type to the closure that builds it,
// or nil when no arm exists for the type.
//
// Deferring construction into a closure is what lets NewProvider report an
// unknown type as unknown while still applying the cloud gate before anything
// is actually built: the lookup is pure — it allocates a closure and touches
// no network, no credential, and no filesystem — and the closure is invoked
// only after the gate has passed.
//
// This switch is the ONE place the set of supported provider types is written
// down. Do not add a parallel "known types" list anywhere: a second list is a
// second thing to forget, and the drift would be silent.
func providerConstructorFor(config ProviderConfigEntry) func() (Provider, error) {
	switch config.Type {
	case ProviderTypeOpenAI:
		return func() (Provider, error) { return providerOrNil(NewOpenAIProvider(config)) }
	case ProviderTypeAnthropic:
		return func() (Provider, error) { return providerOrNil(NewAnthropicProvider(config)) }
	case ProviderTypeGemini:
		return func() (Provider, error) { return providerOrNil(NewGeminiProvider(config)) }
	case ProviderTypeOllama:
		return func() (Provider, error) {
			ollamaConfig := OllamaConfig{
				BaseURL:       config.Endpoint,
				DefaultModel:  "llama2", // Default
				Timeout:       120 * time.Second,
				StreamEnabled: true,
			}
			if len(config.Models) > 0 {
				ollamaConfig.DefaultModel = config.Models[0]
			}
			// Map parameters if available
			if val, ok := config.Parameters["timeout"].(float64); ok {
				ollamaConfig.Timeout = time.Duration(val) * time.Second
			}
			return providerOrNil(NewOllamaProvider(ollamaConfig))
		}
	case ProviderTypeLlamaCpp:
		return func() (Provider, error) {
			llamaConfig := LlamaConfig{
				Model:         "", // Needs to be set from config
				ContextSize:   4096,
				ServerTimeout: 120 * time.Second,
			}
			if len(config.Models) > 0 {
				llamaConfig.Model = config.Models[0]
			}
			// Map parameters
			if val, ok := config.Parameters["context_size"].(float64); ok {
				llamaConfig.ContextSize = int(val)
			}
			if val, ok := config.Parameters["gpu_enabled"].(bool); ok {
				llamaConfig.GPUEnabled = val
			}
			return providerOrNil(NewLlamaCPPProvider(llamaConfig))
		}
	case ProviderTypeQwen:
		return func() (Provider, error) { return providerOrNil(NewQwenProvider(config)) }
	case ProviderTypeXAI:
		return func() (Provider, error) { return providerOrNil(NewXAIProvider(config)) }
	case ProviderTypeOpenRouter:
		return func() (Provider, error) { return providerOrNil(NewOpenRouterProvider(config)) }
	case ProviderTypeCopilot:
		return func() (Provider, error) { return providerOrNil(NewCopilotProvider(config)) }
	case ProviderTypeAzure:
		return func() (Provider, error) { return providerOrNil(NewAzureProvider(config)) }
	case ProviderTypeBedrock:
		return func() (Provider, error) { return providerOrNil(NewBedrockProvider(config)) }
	case ProviderTypeVertexAI:
		return func() (Provider, error) { return providerOrNil(NewVertexAIProvider(config)) }
	case ProviderTypeGroq:
		return func() (Provider, error) { return providerOrNil(NewGroqProvider(config)) }
	case ProviderTypeVLLM:
		return func() (Provider, error) { return newOpenAICompatibleFromConfig("vllm", config) }
	case ProviderTypeLocalAI:
		return func() (Provider, error) { return newOpenAICompatibleFromConfig("localai", config) }
	case ProviderTypeFastChat:
		return func() (Provider, error) { return newOpenAICompatibleFromConfig("fastchat", config) }
	case ProviderTypeTextGen:
		return func() (Provider, error) { return newOpenAICompatibleFromConfig("textgen", config) }
	case ProviderTypeLMStudio:
		return func() (Provider, error) { return newOpenAICompatibleFromConfig("lmstudio", config) }
	case ProviderTypeJan:
		return func() (Provider, error) { return newOpenAICompatibleFromConfig("jan", config) }
	case ProviderTypeGPT4All:
		return func() (Provider, error) { return newOpenAICompatibleFromConfig("gpt4all", config) }
	case ProviderTypeTabbyAPI:
		return func() (Provider, error) { return newOpenAICompatibleFromConfig("tabbyapi", config) }
	case ProviderTypeMLX:
		return func() (Provider, error) { return newOpenAICompatibleFromConfig("mlx", config) }
	case ProviderTypeMistralRS:
		return func() (Provider, error) { return newOpenAICompatibleFromConfig("mistralrs", config) }
	case ProviderTypeKoboldAI:
		return func() (Provider, error) {
			koboldConfig := KoboldAIConfig{
				BaseURL: config.Endpoint,
				APIKey:  config.APIKey,
				Timeout: 120 * time.Second,
			}
			if len(config.Models) > 0 {
				koboldConfig.DefaultModel = config.Models[0]
			}
			if val, ok := config.Parameters["timeout"].(float64); ok {
				koboldConfig.Timeout = time.Duration(val) * time.Second
			}
			// NOT `return NewKoboldAIProvider(koboldConfig)`: that returns a
			// CONCRETE *KoboldAIProvider, and now that the constructor can fail
			// (cloud gate) a nil pointer would be boxed into a NON-nil Provider
			// interface. A caller's `if prov != nil` would then pass and panic on
			// first use. A refused construction must yield nothing usable. This
			// arm was the hand-rolled original of that guard; it now uses the
			// shared providerOrNil so there is ONE mechanism rather than one
			// protected arm beside two dozen unprotected ones.
			return providerOrNil(NewKoboldAIProvider(koboldConfig))
		}
	case ProviderTypeXiaomi:
		return func() (Provider, error) { return providerOrNil(NewXiaomiProvider(config)) }
	case ProviderTypeReplicate:
		return func() (Provider, error) { return providerOrNil(NewReplicateProvider(config)) }
	default:
		return nil
	}
}

// newOpenAICompatibleFromConfig creates an OpenAI-compatible provider from a generic config entry.
// This is used for local providers (VLLM, LocalAI, LMStudio, etc.) that implement the OpenAI API spec.
func newOpenAICompatibleFromConfig(name string, config ProviderConfigEntry) (Provider, error) {
	cfg := OpenAICompatibleConfig{
		BaseURL:          config.Endpoint,
		APIKey:           config.APIKey,
		DefaultModel:     "",
		Timeout:          120 * time.Second,
		MaxRetries:       3,
		StreamingSupport: true,
		ModelEndpoint:    "/v1/models",
		ChatEndpoint:     "/v1/chat/completions",
	}
	if len(config.Models) > 0 {
		cfg.DefaultModel = config.Models[0]
	}
	if val, ok := config.Parameters["timeout"].(float64); ok {
		cfg.Timeout = time.Duration(val) * time.Second
	}
	if val, ok := config.Parameters["streaming_support"].(bool); ok {
		cfg.StreamingSupport = val
	}
	if val, ok := config.Parameters["model_endpoint"].(string); ok {
		cfg.ModelEndpoint = val
	}
	if val, ok := config.Parameters["chat_endpoint"].(string); ok {
		cfg.ChatEndpoint = val
	}
	// Unboxed deliberately — see the KoboldAI arm above. This constructor
	// already fails on a hosted endpoint with the gate closed, so returning
	// its concrete *OpenAICompatibleProvider directly handed callers a
	// non-nil Provider interface wrapping a nil pointer on every gate
	// refusal. Observed, not theorised: the guard
	// TestNewProvider_ExemptOpenAICompatibleRefusedAtHostedEndpoint failed
	// on exactly this assertion for both vllm and lmstudio before this fix.
	prov, err := NewOpenAICompatibleProvider(name, cfg)
	if err != nil {
		return nil, err
	}
	return prov, nil
}

// InitializeModelManager initializes a ModelManager with providers from configuration
func InitializeModelManager(configs []ProviderConfigEntry) (*ModelManager, error) {
	manager := NewModelManager()

	for _, config := range configs {
		if !config.Enabled {
			continue
		}

		provider, err := NewProvider(config)
		if err != nil {
			return nil, fmt.Errorf("failed to create provider %s: %w", config.Type, err)
		}

		if err := manager.RegisterProvider(provider); err != nil {
			return nil, fmt.Errorf("failed to register provider %s: %w", config.Type, err)
		}
	}

	return manager, nil
}
