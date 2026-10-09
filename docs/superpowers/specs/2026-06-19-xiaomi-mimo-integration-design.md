# Xiaomi MiMo Full Platform Integration Design

**Date**: 2026-06-19
**Status**: Approved
**Scope**: Full Xiaomi MiMo provider integration across all HelixCode layers + nano TUI clone project
**Constitutional anchors**: §11.4.27 (CONST-050), §11.4.36 (CONST-036), §11.4.37 (CONST-037), §11.4.69, §11.4.70, §11.4.126

---

## 1. Executive Summary

Integrate Xiaomi MiMo as a first-class LLM provider in HelixCode, covering text generation, ASR (speech recognition), TTS (text synthesis), multimodal understanding, tool calling, and web search. The integration spans the full stack: provider implementation, factory registration, key recognition, verifier integration, hosted catalogue, documentation, and comprehensive testing.

Additionally, create a nano TUI text editor clone project using HelixCode TUI with HelixAgent AI Ensemble, recorded via HelixQA.

---

## 2. Technical Facts (Live-Verified 2026-06-19)

### API Details

| Property | Value | Verified |
|---|---|---|
| Base URL (pay-as-you-go) | `https://api.xiaomimimo.com/v1` | ✅ HTTP 200 |
| Auth header | `Authorization: Bearer sk-xxx` | ✅ HTTP 200 |
| Auth header (alt) | `api-key: sk-xxx` | ✅ HTTP 200 |
| Model listing | `GET /v1/models` | ✅ 10 models returned |
| Chat completions | `POST /v1/chat/completions` | ✅ Real response with reasoning_content |
| Streaming | `stream: true` | ✅ Documented |
| OpenAI compatible | Yes | ✅ |
| Anthropic compatible | Yes (separate endpoint) | ✅ Documented |

### Models (10 total)

| Model ID | Type | Context | Max Output | Capabilities |
|---|---|---|---|---|
| `mimo-v2.5-pro` | Text gen (flagship) | 1M | 128K | Text, Deep Thinking, Streaming, Function Call, Structured Output, Web Search |
| `mimo-v2.5` | Omni (multimodal) | 1M | 128K | Full-modal (text/image/video/audio), Deep Thinking, Streaming, Function Call, Structured Output, Web Search |
| `mimo-v2-pro` | Text gen (pro) | 1M | 128K | Same as v2.5-pro |
| `mimo-v2-omni` | Omni (multimodal) | 256K | 128K | Same as v2.5 |
| `mimo-v2-flash` | Text gen (fast) | 256K | 64K | Text, Deep Thinking, Streaming, Function Call, Structured Output, Web Search |
| `mimo-v2.5-asr` | Speech recognition | 8K | 2K | ASR (Chinese dialects, English, code-switch, songs, noise, multi-speaker) |
| `mimo-v2.5-tts` | Speech synthesis | 8K | 8K | TTS with natural language style instructions |
| `mimo-v2.5-tts-voiceclone` | Voice cloning | 8K | 8K | TTS + timbre cloning from reference audio |
| `mimo-v2.5-tts-voicedesign` | Voice design | 8K | 8K | TTS + timbre design from text description |
| `mimo-v2-tts` | Speech synthesis (legacy) | 8K | 8K | TTS (deprecated 2026-06-30) |

### Rate Limits

| Metric | Text Gen / TTS | ASR |
|---|---|---|
| RPM | 100 | 100 |
| TPM | 10M | 10K |

### Pricing (Overseas, USD per M tokens)

| Model | Input (Cache Hit) | Input (Cache Miss) | Output |
|---|---|---|---|
| `mimo-v2.5-pro` | $0.0036 | $0.435 | $0.87 |
| `mimo-v2.5` | $0.0028 | $0.14 | $0.28 |
| `mimo-v2-flash` | $0.01 | $0.10 | $0.30 |
| `mimo-v2.5-asr` | $0.074/hour | — | — |
| TTS models | Free (limited time) | — | — |

### Key Technical Details

- **reasoning_content field**: Returned alongside `content` in thinking mode. Must be preserved in multi-turn conversations.
- **Tool calling**: OpenAI-compatible `tools` array + `tool_choice` parameter.
- **Web Search**: Built-in tool, billed independently ($5/1000 calls).
- **System prompt**: "You are MiMo, an AI assistant developed by Xiaomi."
- **Recommended temperature**: 1.0 for v2.5-pro/v2.5, 0.3 for v2-flash (coding), 0.8 for v2-flash (general).
- **License**: Apache-2.0 (open source V2.5 series).
- **V2 deprecation**: V2 models auto-route to V2.5 from June 18, 2026; fully deprecated June 30, 2026.

---

## 3. Provider Architecture

### 3.1 File: `helix_code/internal/llm/xiaomi_provider.go` (NEW)

```go
type XiaomiProvider struct {
    oaiProvider *OpenAICompatibleProvider  // embedded for text gen
    apiKey      string
    baseURL     string                     // https://api.xiaomimimo.com/v1
    httpClient  *http.Client
    models      []ModelInfo
}
```

**Text Generation** — delegates to embedded `OpenAICompatibleProvider`:
- Chat completions: `POST /v1/chat/completions`
- Model listing: `GET /v1/models`
- Streaming, tool calling, reasoning_content — all handled by existing OAI compat code

**ASR** — dedicated method:
- Endpoint: `POST /v1/audio/transcriptions` (OpenAI Whisper-compatible)
- Input: audio file (multipart form data)
- Model: `mimo-v2.5-asr`
- Output: text transcription

**TTS** — dedicated method:
- Endpoint: `POST /v1/audio/speech` (OpenAI TTS-compatible)
- Input: text + voice parameters
- Models: `mimo-v2.5-tts`, `mimo-v2.5-tts-voiceclone`, `mimo-v2.5-tts-voicedesign`
- Output: audio bytes

### 3.2 Registration Points

| File | Change |
|---|---|
| `missing_types.go` | Add `ProviderTypeXiaomi ProviderType = "xiaomi"` |
| `factory.go` | Add `case ProviderTypeXiaomi: return NewXiaomiProvider(config)` |
| `keyrecognition.go` | Add `ProviderTypeXiaomi: {"XIAOMI_MIMO_API_KEY", "ApiKey_Xiaomi_MiMo"}` |
| `openai_compatible_catalogue.go` | Add Xiaomi entry as fallback |
| `verifier_dynamic_catalogue.go` | Add `"xiaomi": true` to `dynamicNativelyWiredProviders` |

### 3.3 Hosted Catalogue Entry

```go
{
    Name:          "xiaomi",
    BaseURL:       "https://api.xiaomimimo.com/v1",
    KeyEnvAliases: []string{"XIAOMI_MIMO_API_KEY", "ApiKey_Xiaomi_MiMo"},
    ModelEndpoint: "/models",
    ChatEndpoint:  "/chat/completions",
},
```

---

## 4. Cross-Layer Integration

### 4.1 LLMsVerifier (CONST-036 / CONST-037)

Add Xiaomi provider entry to `submodules/llms_verifier`:
- Provider name: `xiaomi`
- API URL: `https://api.xiaomimimo.com/v1`
- Models: all 10 models with metadata
- Verification status: `verified`

### 4.2 HelixAgent

Add Xiaomi to `submodules/helix_agent/internal/services/provider_discovery.go`:
- `providerMappings` entry with BaseURL, key aliases, model list

### 4.3 API Keys

From `~/api_keys.sh` (already configured):
```bash
export ApiKey_Xiaomi_MiMo=<REDACTED-XIAOMI-MIMO-KEY-CONST-042-ROTATION-PENDING>
export XIAOMI_MIMO_API_KEY=$ApiKey_Xiaomi_MiMo
```

---

## 5. Testing Strategy

### 5.1 Test Types (per §11.4.27 / CONST-050)

| Test Type | File | Coverage |
|---|---|---|
| Unit | `xiaomi_provider_test.go` | Provider construction, key resolution, model parsing, URL composition, auth header |
| Integration | `xiaomi_provider_integration_test.go` | Real API calls: chat completions, model listing, streaming, tool calling |
| Stress | `xiaomi_provider_stress_test.go` | 100+ sequential calls, concurrent contention, latency percentiles |
| Chaos | `xiaomi_provider_chaos_test.go` | Network faults, invalid keys, rate limits (429), malformed responses, server errors |
| E2E | Via `tests/e2e/challenges/` | Full flow: key detection → provider init → model listing → generation → streaming |
| Challenges | Via `challenges/` submodule | Real-world scenarios: code generation, reasoning, tool calling |
| HelixQA | Via `helix_qa/` submodule | Autonomous QA session with test banks |

### 5.2 Anti-Bluff Evidence (§11.4.5 / §11.4.69)

Every PASS must cite:
1. **API response capture** — actual JSON from Xiaomi API (not mocked)
2. **reasoning_content verification** — prove the field is populated
3. **Tool calling verification** — prove tools are invoked and results returned
4. **Streaming verification** — prove SSE chunks arrive incrementally
5. **Rate limit handling** — prove 429 is handled gracefully

### 5.3 Regression Guards (§11.4.135)

Each closed defect gets a permanent `RED_MODE=1/0` polarity test.

---

## 6. Documentation

| Document | Location | Content |
|---|---|---|
| Provider README | `helix_code/internal/llm/XIAOMI_PROVIDER.md` | Full provider docs |
| API reference | `docs/providers/xiaomi-mimo.md` | API details, models, pricing |
| Integration guide | `docs/guides/xiaomi-integration.md` | How to configure and use |
| Status doc | `docs/features/xiaomi-status.md` | Feature status + video confirmation |
| Status_Summary | `docs/features/xiaomi-status_summary.md` | At-a-glance summary |

### Export Formats (§11.4.65 / §11.4.153)

All docs exported to: HTML + PDF + DOCX

---

## 7. Nano TUI Clone Project

### 7.1 Setup

- **Location**: `$PROJECTS/nano_clone` (auto-increment if exists)
- **Tool**: HelixCode TUI with HelixAgent AI Ensemble
- **Approach**: Subagent-driven development (§11.4.70)
- **Recording**: `/Volumes/T7/Downloads/Recordings/helixcode-tui-*`

### 7.2 Scope

Nano text editor clone implementing:
1. File operations (open, save, create, close)
2. Text editing (insert, delete, cut/copy/paste, undo/redo)
3. Navigation (cursor, page up/down, home/end, search)
4. Syntax highlighting (basic language support)
5. Status bar (file name, line/column, modified indicator)
6. Menu/help (keyboard shortcuts, command palette)
7. Terminal UI (tview/tcell stack)

### 7.3 Development Flow

1. Create project directory (auto-increment)
2. Initialize Go module
3. Dispatch parallel subagents:
   - Agent 1: Core editor engine (buffer, cursor, undo/redo)
   - Agent 2: TUI rendering (tview layout, status bar, menus)
   - Agent 3: File I/O (open, save, syntax detection)
   - Agent 4: Key bindings & command handling
   - Agent 5: Search & replace
4. Integration + testing
5. Recording + evidence capture

### 7.4 Recording Strategy

- Window-scoped capture (§11.4.154)
- Project-prefixed filenames (§11.4.155): `helixcode-tui-nano-clone-<timestamp>.mp4`
- Fresh-corpus rotation (§11.4.154)
- Content verification (§11.4.158)

### 7.5 Challenge / HelixQA

Full repeatable Challenge script for end-to-end re-runnability.

---

## 8. Commit & Push Strategy

Per §11.4.88 (background push) + §11.4.113 (no force push):
1. Regular commits with descriptive messages
2. Push to ALL upstreams (GitHub + GitLab + GitFlic + GitVerse)
3. Background push (nohup + disown)
4. Merge-onto-latest-main (never force push)

---

## 9. Risk Assessment

| Risk | Mitigation |
|---|---|
| V2 model deprecation (June 30, 2026) | Focus on V2.5 models; V2 entries as deprecated |
| Rate limits (100 RPM) | Implement backoff/retry in provider |
| API key format (sk-xxx vs tp-xxx) | Support both; key recognition handles both |
| reasoning_content field handling | Extend message parsing to capture and preserve |
| ASR/TTS endpoint format uncertainty | Live-verify endpoints before implementation |

---

## 10. Implementation Phases

### Phase 1: Provider Implementation
1. Create `xiaomi_provider.go`
2. Register in factory, key recognition, catalogue
3. Unit tests

### Phase 2: Integration Testing
1. Live API integration tests
2. Stress + chaos tests
3. E2E challenge

### Phase 3: Verifier + Submodule Integration
1. LLMsVerifier registration
2. HelixAgent provider discovery
3. Cross-provider verification

### Phase 4: Documentation
1. Provider README
2. API reference
3. Integration guide
4. Status docs + exports

### Phase 5: Nano TUI Clone
1. Project setup
2. Subagent-driven implementation
3. Recording + evidence
4. Challenge script

### Phase 6: Release
1. Full test suite run
2. Tag + push to all upstreams
3. Evidence compilation
