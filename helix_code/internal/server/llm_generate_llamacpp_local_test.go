package server

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"dev.helix.code/internal/llm"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// llm_generate_llamacpp_local_test.go — RED-first coverage (§11.4.115 /
// §11.4.146) for serving completions from a LOCAL llama.cpp endpoint through
// POST /api/v1/llm/generate, instead of only the hardcoded Ollama default.
//
// THE DEFECT THIS GUARDS (measured, not assumed — see the captured RED
// transcript in the closure record):
//
//  1. A request that names NO provider falls through resolveLLMProvider to a
//     hardcoded `http://localhost:11434` Ollama provider. On a host with no
//     Ollama installed that is a 502:
//     `generation failed: ... dial tcp 127.0.0.1:11434: connect: connection
//     refused` with `"provider":"ollama"`.
//
//  2. Naming `"provider":"llamacpp"` DID resolve (parseCloudProviderType maps
//     llamacpp/llama-cpp/llama.cpp → ProviderTypeLlamaCpp, and
//     NewCloudProvider constructs it via newLlamaCPPFromEntry) — but the
//     resulting *llm.LlamaCPPProvider is unusable for a chat-shaped request
//     from this handler, for two independent reasons:
//
//     (a) WRONG ENDPOINT SHAPE. LlamaCPPProvider.Generate ALWAYS POSTs to
//     `/v1/completions`, and when request.Messages is non-empty it sends a
//     `messages` key to it. A real llama-server rejects that with
//     `400 key 'prompt' not found` — the same failure already recorded
//     verbatim in resolveHelixLLMLocalProvider's doc-comment ("verified
//     live against the coder during this change"). Every request from
//     these handlers carries Messages (buildLLMRequest builds a message
//     list unconditionally), so this path could never succeed.
//
//     (b) UNCONFIGURABLE, SELF-COLLIDING HOST. resolveLLMProvider builds
//     `llm.ProviderConfigEntry{Type: ptype, Enabled: true}` and never sets
//     Endpoint, so newLlamaCPPFromEntry receives ServerHost == "" and
//     LlamaCPPProvider.Generate falls back to its hardcoded
//     `http://localhost:8080` — which is the port HelixCode's OWN API
//     server listens on. There was NO way to point it anywhere else over
//     the HTTP API (§11.4.111 resolve-by-configuration, CONST-046).
//
//  3. `HELIX_LLAMA_CPP_HOST` was a DEAD config key: root `.env.example:89`
//     documents it (now `http://localhost:18434`, fixed from the historical
//     `:8080` self-POST hazard by commit a74ae7cb) and the LLMsVerifier
//     integration plan tabulates it, but a repo-wide grep found ZERO Go
//     readers — an operator setting it got silence, not a redirected
//     endpoint.
//
// THE FIX these tests pin: `llamacpp` / `llama-cpp` / `llama.cpp` resolve, like
// the sibling `helixllm` / `local` selectors already do, to a REAL
// *llm.OpenAICompatibleProvider — the generic OpenAI-compatible HTTP client
// HelixCode already ships and already uses for VLLM / LMStudio / LocalAI
// (internal/llm/openai_compatible_provider.go, reused per CONST-036 /
// §11.4.74) — pointed at a CONFIGURED base URL, and it POSTs messages to
// `/v1/chat/completions`, which is what llama.cpp's OpenAI-compatible server
// actually serves.
//
// NOTHING IS REMOVED (§11.4.122): llm.LlamaCPPProvider, its constructor, its
// factory registration and the Ollama default route all remain exactly as
// they were. This change is strictly additive on the previously-unusable
// server-side llamacpp route.
//
// EVIDENCE LEVEL (§11.4.6 honest boundary): the end-to-end test below drives
// the REAL handler against an httptest stub that mimics llama.cpp's
// OpenAI-compatible surface — including REJECTING `/v1/completions` with
// llama-server's real `key 'prompt' not found` error, so the pre-fix
// endpoint-shape defect is genuinely reproduced rather than assumed. It
// proves the wire contract, not that any particular GGUF loads.

// This guard's registered polarity switch is RED_LLAMACPP_LOCAL
// (red_polarity_convention_test.go rule 1 + 5). It is passed to redModeFor as
// a LITERAL, not via a named constant: the convention file's own conformance
// scanner (TestRedPolarityConvention_RegistryMatchesReality) enumerates the
// package's switches by matching `redModeFor(t, "NAME")` textually, so a
// const-indirected name would register as "no longer read by any guard".

// stubLlamaServer stands in for a real llama.cpp `llama-server` running with
// its OpenAI-compatible HTTP surface. It deliberately models the TWO
// behaviours that make the pre-fix path fail:
//
//   - `POST /v1/chat/completions` succeeds and returns an OpenAI-shaped
//     envelope (what llama-server really serves for chat payloads).
//   - `POST /v1/completions` FAILS with llama-server's actual error for a
//     body carrying `messages` instead of `prompt`:
//     `400 {"error":{"message":"key 'prompt' not found", ...}}`.
//
// It also serves `GET /v1/models` so provider model-discovery and the
// handler's CONST-036/037 resolveDefaultModel path have a real catalogue to
// read, exactly as they would against the live server.
type stubLlamaServer struct {
	*httptest.Server
	// servedModel is the model id the stub reports and answers as.
	servedModel string
	// content is the completion text the stub returns.
	content string

	// chatHits / legacyHits record which endpoint the provider actually hit,
	// so a test can assert the endpoint SHAPE rather than merely that "a
	// request happened".
	chatHits   int
	legacyHits int
}

func newStubLlamaServer(t *testing.T, servedModel, content string) *stubLlamaServer {
	t.Helper()
	s := &stubLlamaServer{servedModel: servedModel, content: content}

	mux := http.NewServeMux()

	mux.HandleFunc("/v1/models", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"object": "list",
			"data": []map[string]any{
				{"id": s.servedModel, "object": "model", "owned_by": "llamacpp"},
			},
		})
	})

	mux.HandleFunc("/v1/chat/completions", func(w http.ResponseWriter, r *http.Request) {
		s.chatHits++
		raw, _ := io.ReadAll(r.Body)
		var body map[string]any
		_ = json.Unmarshal(raw, &body)
		// llama-server requires a chat payload here; assert the client sent one.
		if _, ok := body["messages"]; !ok {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":{"message":"key 'messages' not found","code":400}}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":      "chatcmpl-stub",
			"object":  "chat.completion",
			"model":   s.servedModel,
			"choices": []map[string]any{{"index": 0, "message": map[string]any{"role": "assistant", "content": s.content}, "finish_reason": "stop"}},
			"usage":   map[string]any{"prompt_tokens": 7, "completion_tokens": 3, "total_tokens": 10},
		})
	})

	// The legacy endpoint the PRE-FIX LlamaCPPProvider always used. A real
	// llama-server answers a messages-carrying body here with this exact
	// class of error, which is why the pre-fix route could never work.
	mux.HandleFunc("/v1/completions", func(w http.ResponseWriter, r *http.Request) {
		s.legacyHits++
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":{"message":"key 'prompt' not found","code":400}}`))
	})

	s.Server = httptest.NewServer(mux)
	t.Cleanup(s.Close)
	return s
}

// TestGenerateLLM_LlamaCppLocal_ServesRealCompletion is the load-bearing
// end-to-end guard: the REAL generateLLM handler, the REAL provider-resolution
// path (no fake resolver), driven against a stub that behaves like
// llama-server. It proves a caller naming `"provider":"llamacpp"` gets a
// genuine completion served from the CONFIGURED local endpoint.
//
// Both polarities drive the SAME handler against the SAME stub — only the
// final assertion flips (§11.4.115 one-source-two-roles):
//
//   - RED (RED_LLAMACPP_LOCAL=1 or the RED_MODE umbrella): asserts the request
//     does NOT succeed — the pre-fix behaviour, where the llamacpp route
//     ignored HELIX_LLAMA_CPP_HOST entirely and POSTed a messages body to
//     `/v1/completions`. That holds on a pre-fix artifact and FAILS on the
//     fixed one, and that failure is the proof the fix reached the binary.
//   - GREEN (default): asserts HTTP 200, the stub's actual completion text,
//     and that the provider hit the CHAT endpoint, not the legacy one.
func TestGenerateLLM_LlamaCppLocal_ServesRealCompletion(t *testing.T) {
	const servedModel = "local-llama-3.2-3b"
	const wantContent = "ready"

	stub := newStubLlamaServer(t, servedModel, wantContent)
	t.Setenv(llamaCppHostEnv, stub.URL)

	srv := &Server{}
	w, body := postJSON(t, "/api/v1/llm/generate", srv.generateLLM,
		`{"provider":"llamacpp","prompt":"Reply with exactly one word: ready","max_tokens":16}`)

	if redModeFor(t, "RED_LLAMACPP_LOCAL") {
		assert.NotEqual(t, http.StatusOK, w.Code,
			"RED expectation: the PRE-FIX llamacpp route could not serve a chat request from a local "+
				"llama.cpp endpoint — it ignored %s and POSTed a messages body to /v1/completions "+
				"(llama-server: key 'prompt' not found). On the FIXED artifact this assertion FAILS "+
				"(the call now returns 200), and that failure is the proof the fix is present. body=%v",
			llamaCppHostEnv, body)
		return
	}

	require.Equal(t, http.StatusOK, w.Code,
		"GREEN: a llamacpp-named request must be served by the configured local endpoint; body=%v", body)
	assert.Equal(t, "success", body["status"])
	assert.Equal(t, wantContent, body["content"],
		"GREEN: the response content must be the bytes the local endpoint actually returned")
	assert.Equal(t, servedModel, body["model"],
		"GREEN: the reported model must be the one the local endpoint actually served (CONST-036/037)")

	assert.Positive(t, stub.chatHits,
		"GREEN: the provider must POST to /v1/chat/completions — the endpoint llama.cpp's "+
			"OpenAI-compatible server actually serves for a messages payload")
	assert.Zero(t, stub.legacyHits,
		"GREEN: the provider must NOT POST a messages body to the legacy /v1/completions endpoint "+
			"(a real llama-server answers that with `key 'prompt' not found`)")
}

// TestGenerateLLM_LlamaCppLocal_ViaEnvProviderSelection proves the headline
// ask — "serve completions from a LOCAL llama.cpp endpoint INSTEAD of only
// hardcoded Ollama" — is reachable by CONFIGURATION ALONE, with an unchanged
// request body: setting HELIX_LLM_PROVIDER=llamacpp makes a request that names
// no provider go to the local llama.cpp endpoint rather than falling through
// to the hardcoded localhost:11434 Ollama default.
func TestGenerateLLM_LlamaCppLocal_ViaEnvProviderSelection(t *testing.T) {
	const servedModel = "local-llama-3.2-3b"
	const wantContent = "ready"

	stub := newStubLlamaServer(t, servedModel, wantContent)
	t.Setenv(llamaCppHostEnv, stub.URL)
	t.Setenv("HELIX_LLM_PROVIDER", "llamacpp")

	srv := &Server{}
	// NOTE: no "provider" field in the body — selection comes from the env.
	w, body := postJSON(t, "/api/v1/llm/generate", srv.generateLLM,
		`{"prompt":"Reply with exactly one word: ready","max_tokens":16}`)

	require.Equal(t, http.StatusOK, w.Code,
		"HELIX_LLM_PROVIDER=llamacpp must route a provider-less request to the local llama.cpp endpoint "+
			"instead of the hardcoded Ollama default; body=%v", body)
	assert.Equal(t, wantContent, body["content"])
	assert.Positive(t, stub.chatHits)
}

// TestResolveLLMProvider_LlamaCppLocal_Aliases proves every documented
// spelling parseCloudProviderType already accepted ("llamacpp", "llama-cpp",
// "llama.cpp") resolves through the local OpenAI-compatible route, in any
// case — so the fix does not narrow the set of names that used to resolve.
func TestResolveLLMProvider_LlamaCppLocal_Aliases(t *testing.T) {
	for _, name := range []string{"llamacpp", "LlamaCPP", "LLAMACPP", "llama-cpp", "Llama-Cpp", "llama.cpp"} {
		name := name
		t.Run(name, func(t *testing.T) {
			provider, err := resolveLLMProvider(name, "")
			require.NoError(t, err, "resolveLLMProvider(%q) must resolve the local llama.cpp route", name)
			require.NotNil(t, provider)
			defer func() { _ = provider.Close() }()

			_, ok := provider.(*llm.OpenAICompatibleProvider)
			require.True(t, ok,
				"expected the reused *llm.OpenAICompatibleProvider (which POSTs to /v1/chat/completions) "+
					"for provider name %q, got %T", name, provider)
		})
	}
}

// TestResolveLLMProvider_LlamaCppLocal_EndpointIsConfigured proves the base
// URL comes from configuration, never a literal baked into the route
// (§11.4.111 / CONST-046). HELIX_LLAMA_CPP_HOST — the key `.env.example`
// already documents — is honoured; this is the assertion that would have
// failed while that key was dead.
func TestResolveLLMProvider_LlamaCppLocal_EndpointIsConfigured(t *testing.T) {
	t.Setenv(llamaCppHostEnv, "http://127.0.0.1:19991")

	provider, err := resolveLLMProvider("llamacpp", "")
	require.NoError(t, err)
	require.NotNil(t, provider)
	defer func() { _ = provider.Close() }()

	oc, ok := provider.(*llm.OpenAICompatibleProvider)
	require.True(t, ok)
	assert.Equal(t, "http://127.0.0.1:19991", oc.BaseURL(),
		"%s must select the llama.cpp base URL — it was a dead config key before this fix", llamaCppHostEnv)
}

// TestResolveLLMProvider_LlamaCppLocal_FallsBackToSharedLocalEndpoint proves
// the documented precedence when HELIX_LLAMA_CPP_HOST is unset: the route
// reuses the project's already-established local-OpenAI endpoint convention
// (HELIX_LLM_LOCAL_OPENAI_ENDPOINT, the var the sibling helixllm route and
// submodules/helix_agent already read) rather than inventing a second
// unrelated default.
func TestResolveLLMProvider_LlamaCppLocal_FallsBackToSharedLocalEndpoint(t *testing.T) {
	t.Setenv(llamaCppHostEnv, "")
	t.Setenv(helixLLMLocalOpenAIEndpointEnv, "http://127.0.0.1:19992")

	provider, err := resolveLLMProvider("llamacpp", "")
	require.NoError(t, err)
	require.NotNil(t, provider)
	defer func() { _ = provider.Close() }()

	oc, ok := provider.(*llm.OpenAICompatibleProvider)
	require.True(t, ok)
	assert.Equal(t, "http://127.0.0.1:19992", oc.BaseURL(),
		"with %s unset the route must fall back to the shared %s convention",
		llamaCppHostEnv, helixLLMLocalOpenAIEndpointEnv)
}

// TestResolveLLMProvider_LlamaCppLocal_DefaultEndpointDoesNotCollideWithOurOwnServer
// pins the zero-config default AND the reason it is not the generic
// `http://localhost:8080` llama-server upstream default (still shown in
// `configs/verifier.yaml`; root `.env.example` itself now documents the
// fixed `:18434` value, per commit a74ae7cb).
//
// 8080 is llama-server's UPSTREAM default, but it is also the port HelixCode's
// OWN API server listens on — the pre-fix hardcoded fallback in
// LlamaCPPProvider.Generate would have made the server POST completions to
// ITSELF. The project's live local llama.cpp port is 18434
// (config/llmsverifier/config.yaml's `llamacpp` row: `http://localhost:18434/v1`,
// served by helixllm-coder.service), which is also what the sibling helixllm
// route already defaults to — so the two local routes agree and neither
// collides with our own listener.
func TestResolveLLMProvider_LlamaCppLocal_DefaultEndpointDoesNotCollideWithOurOwnServer(t *testing.T) {
	t.Setenv(llamaCppHostEnv, "")
	t.Setenv(helixLLMLocalOpenAIEndpointEnv, "")

	provider, err := resolveLLMProvider("llamacpp", "")
	require.NoError(t, err)
	require.NotNil(t, provider)
	defer func() { _ = provider.Close() }()

	oc, ok := provider.(*llm.OpenAICompatibleProvider)
	require.True(t, ok)
	assert.Equal(t, helixLLMLocalDefaultEndpoint, oc.BaseURL(),
		"the zero-config llama.cpp default must be the project's live local endpoint")
	assert.NotContains(t, oc.BaseURL(), ":8080",
		"the default must NOT be :8080 — that is HelixCode's own API port, so it would make the "+
			"server POST completions to itself")
}

// ---------------------------------------------------------------------------
// Non-regression guards — this change must not alter any OTHER route.
// ---------------------------------------------------------------------------

// TestResolveLLMProvider_LlamaCppLocal_OllamaDefaultUnchanged proves the
// pre-existing hardcoded-Ollama default is INTACT (§11.4.122 — the ollama path
// is not removed or disabled, the local path is added ALONGSIDE it): a request
// naming no provider, with no HELIX_LLM_PROVIDER, still resolves to a real
// Ollama provider on the standard port.
func TestResolveLLMProvider_LlamaCppLocal_OllamaDefaultUnchanged(t *testing.T) {
	t.Setenv("HELIX_LLM_PROVIDER", "")
	t.Setenv(ollamaHostEnv, "")

	provider, err := resolveLLMProvider("", "")
	require.NoError(t, err, "the zero-config Ollama default must still construct")
	require.NotNil(t, provider)
	defer func() { _ = provider.Close() }()

	assert.Equal(t, "ollama", strings.ToLower(provider.GetName()),
		"the default route must still be Ollama — this change adds the local llama.cpp route "+
			"alongside it, it does not replace it")
}

// TestResolveLLMProvider_OllamaHostIsConfigured proves the second dead config
// key from the same `.env.example` block, HELIX_OLLAMA_HOST, is now honoured
// too — the same defect class as HELIX_LLAMA_CPP_HOST (documented, tabulated,
// zero Go readers). Default behaviour is unchanged when it is unset, which the
// guard above pins.
func TestResolveLLMProvider_OllamaHostIsConfigured(t *testing.T) {
	t.Setenv("HELIX_LLM_PROVIDER", "")
	t.Setenv(ollamaHostEnv, "http://127.0.0.1:19993")

	provider, err := resolveLLMProvider("", "")
	require.NoError(t, err)
	require.NotNil(t, provider)
	defer func() { _ = provider.Close() }()

	op, ok := provider.(*llm.OllamaProvider)
	require.True(t, ok, "the default route must still be *llm.OllamaProvider, got %T", provider)
	assert.Equal(t, "http://127.0.0.1:19993", op.BaseURL(),
		"%s must select the Ollama base URL — it was a dead config key before this fix", ollamaHostEnv)
}

// TestResolveLLMProvider_LlamaCppLocal_UnknownProviderStillRejected is the
// non-regression guard for server defect #4: adding the llamacpp route must
// not weaken the "explicitly-named-but-unknown provider is a 400, never a
// silent fallback" behaviour for any other name.
func TestResolveLLMProvider_LlamaCppLocal_UnknownProviderStillRejected(t *testing.T) {
	_, err := resolveLLMProvider("llamacpp-typo-not-real", "")
	require.Error(t, err)
	require.ErrorIs(t, err, errUnknownProvider,
		"an unknown provider name must still surface errUnknownProvider (400), never fall back silently")
}

// TestResolveLLMProvider_LlamaCppLocal_HelixLLMRouteUnchanged proves the
// sibling helixllm/local route this fix is modelled on is untouched.
func TestResolveLLMProvider_LlamaCppLocal_HelixLLMRouteUnchanged(t *testing.T) {
	provider, err := resolveLLMProvider("helixllm", "")
	require.NoError(t, err)
	require.NotNil(t, provider)
	defer func() { _ = provider.Close() }()
	assert.Equal(t, "helixllm", provider.GetName())
}
