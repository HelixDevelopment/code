# Gap Ledger — helix_code consumption (Spec 002: local LLM execution via llama.cpp + Colibri, dynamic hardware-based model selection)

- **Date:** 2026-09-05
- **Scope:** `helix_code/` Go module (`dev.helix.code`) — `internal/llm/**`, `internal/config/**`, `internal/server/**`, `config/config.yaml`, `cmd/**`
- **Method:** read-only forensic extraction (Steps 1–4 of task-F3 brief). Every entry carries a file:line actually read during this pass. Unknowns marked `UNCONFIRMED:`.
- **Anti-bluff:** no entry asserts runtime behaviour that was not read in source. Live reachability of the HelixLLM gateway (:8443) and coder (:18434) was NOT probed in this pass (read-only mandate) — marked UNCONFIRMED where relevant.

## Step 1 — Default provider resolution (trace)

| Link | Evidence |
|---|---|
| Config default | `internal/config/config.go:676` — `v.SetDefault("llm.default_provider", "local")` |
| Struct field | `internal/config/config.go:199` — `LLMConfig.DefaultProvider` (`mapstructure:"default_provider"`) |
| Server resolver | `internal/server/llm_generate.go:175-267` — `resolveLLMProvider(providerName, model)` |
| Config NOT consulted | `internal/server/llm_generate.go:176-180` — `SelectorInput{Flag, Env, Config: ""}`; `Config` is always empty |
| `"local"`/`"helixllm"` name → coder | `internal/server/llm_generate.go:202-204` → `resolveHelixLLMLocalProvider` (`:553-568`), base URL `http://localhost:18434` (`:530`) via `HELIX_LLM_LOCAL_OPENAI_ENDPOINT` (`:524`) |
| No provider named → Ollama | `internal/server/llm_generate.go:251-266` — `llm.NewOllamaProvider`, `http://localhost:11434` (`:594`) |
| `Select` honours Config when set | `internal/llm/provider_factory.go:57` — `firstNonEmpty(input.Flag, input.Env, input.Config)` |
| Handler entry | `internal/server/llm_generate.go:330` — `llmProviderResolver(req.Provider, req.Model)`; wire_facade `:638`,`:691` pass `""` |
| CLI `helix generate` | `cmd/other_commands.go:116-134` — reads `cfg.LLM.DefaultProvider`, casts to `llm.ProviderType`, feeds `ModelManager` |

**Finding:** config `llm.default_provider: "local"` is never threaded into `SelectorInput.Config` on the server path. A provider-less request therefore falls through to Ollama `:11434`, not the config-declared `"local"` route (`:18434`). On the CLI path, `NewModelManager()` registers zero providers (`internal/llm/model_manager.go:70-76`), so `SelectOptimalModel` always fails with "no models available" (`:109-110`) regardless of config.

## Step 2 — Dead config block (verbatim capture, `config/config.yaml`)

Lines 154–173 (verified by Read at 2026-09-05; block begins at the `# HelixAgent providers` comment on line 154):

```yaml
    # HelixAgent providers
    helix-llm:
      type: helix-llm
      endpoint: "${HELIX_LLM_ENDPOINT:http://localhost:8081}"
      enabled: true
      parameters:
        timeout: 60.0
        max_retries: 3
        streaming_support: true
        api_key: "" # Set via HELIX_LLM_API_KEY
      
    helix-debate:
      type: helix-debate
      endpoint: "${HELIX_DEBATE_ENDPOINT:http://localhost:8082}"
      enabled: true
      parameters:
        timeout: 120.0
        max_retries: 3
        streaming_support: true
        api_key: "" # Set via HELIX_DEBATE_API_KEY
```

Deadness evidence: provider-type switch in `internal/llm/provider_factory.go:83-113` has no `helix-llm`/`helix-debate` case; zero non-comment Go references to either string (grep 2026-09-05); defaults point at the wrong processes (`:8081` = HelixCode server itself, `:8082` = ChromaDB — documented at `config/replica-8081.yaml:187-196`). Worse, the ENTIRE `llm.providers` block is discarded at load: `LLMConfig` declares no `Providers` field (`internal/config/config.go:197-226`), and `internal/config/strict.go:315-317` reports the whole block as discarded. `HELIX_LLM_ENDPOINT` / `HELIX_LLM_API_KEY` are read by no Go code (only `.env.example:44,50` and comments); `HELIX_DEBATE_ENDPOINT` has zero occurrences outside this YAML block.

## Step 3 — Gateway wiring (HelixLLM gateway https://127.0.0.1:8443)

- Zero code references to `:8443` for LLM routing (grep over `*.go/yaml/yml/json/toml/sh/service/md` in the module). Only mentions: a comment at `config/replica-8081.yaml:189` ("The real HelixLLM gateway is https://localhost:8443 (TLS) per setup.sh:159") and unrelated test port lists (`tests/e2e/test_bank/performance/performance_security_tests.go:1313`, `internal/discovery/*_test.go`).
- The only "helixllm" route in the server is hardwired to the llama.cpp coder sidecar at `http://localhost:18434` (`internal/server/llm_generate.go:530,553-568`) — plain HTTP, loopback, no TLS, no gateway endpoint config key, no API-key handling.
- No config key or env var names the gateway (`HELIXLLM_GATEWAY`, `HELIX_LLM_GATEWAY`, etc.: zero matches).

**Gap:** spec 002's "all local LLM execution via llama.cpp + Colibri" has no consumption path of the HelixLLM gateway in this module; dynamic hardware-based model selection has no wiring surface here either (see entry 05).

## Ledger

| id | repo | severity | file | defect | fix_direction | test_plan |
|---|---|---|---|---|---|---|
| HXC-002-F3-01 | helix_code | critical | `internal/server/llm_generate.go:176-180` (+`:330`, `internal/config/config.go:676`) | Config `llm.default_provider: "local"` is never consumed by the server generate path — `SelectorInput.Config` is hardcoded `""`, so provider-less requests fall to Ollama `:11434` (`:251-266`) while the config-declared `"local"` route (`:18434`, `:202-204`) is only reachable by explicitly naming `local`/`helixllm`. Config intent and runtime behaviour diverge silently. | Thread `cfg.LLM.DefaultProvider` into `SelectorInput.Config` (precedence Flag > Env > Config already honoured at `internal/llm/provider_factory.go:57`); ensure `"local"` in config resolves to the helixllm coder route, not Ollama. | Unit (RED→GREEN): `resolveLLMProvider("","")` with config default `local` must construct a provider whose BaseURL is the coder endpoint (`:18434`), not Ollama — polarity-switch `RED_MODE` per §11.4.115; live round-trip test mirroring `llm_generate_helixllm_live_test.go:53` against the coder `/v1/models`. |
| HXC-002-F3-02 | helix_code | critical | `config/config.yaml:154-173` | Dead provider block `helix-llm` / `helix-debate`: unknown types (no factory case at `internal/llm/provider_factory.go:83-113`), zero Go references, wrong default endpoints (`:8081` self, `:8082` ChromaDB per `config/replica-8081.yaml:187-196`). Entire `llm.providers` block is discarded at load — `LLMConfig` has no `Providers` field (`internal/config/config.go:197-226`), `internal/config/strict.go:315-317` reports it discarded. | Remove the verbatim block (captured in Step 2 above) from `config/config.yaml`; drop dead keys `HELIX_LLM_ENDPOINT`/`HELIX_LLM_API_KEY` from `.env.example:44,50`; if a gateway-backed provider is wanted, add it as a real factory type (see F3-03) rather than resurrecting this block. | RED: startup strict-load warning (`strict.go`) must no longer list `llm.providers` as discarded-after-parse for these two entries; grep gate: zero references to `helix-llm`/`helix-debate` outside the removal commit; config load test asserting no `ErrorUnused` for the removed block. |
| HXC-002-F3-03 | helix_code | high | `internal/server/llm_generate.go:530,553-568` | No route to the HelixLLM gateway (`https://127.0.0.1:8443`, user unit `helixllm-gateway.service`): zero code/config references to `:8443` (only comment `config/replica-8081.yaml:189`). The `helixllm`/`local` selector is hardwired to the llama.cpp coder at `http://localhost:18434`, plain HTTP, no TLS config, no gateway env key, no API-key handling. UNCONFIRMED: gateway live reachability not probed (read-only mandate). | Add a gateway endpoint key (env `HELIX_LLM_GATEWAY_ENDPOINT` or config-injected per §11.4.28) and a provider route constructing an HTTPS client against the gateway with TLS trust config; keep the `:18434` coder route as the direct-sidecar fallback. | Unit: resolver mapping `gateway`/`helixllm-gateway` selector → BaseURL `https://127.0.0.1:8443`; live test: `GET /v1/models` on the gateway (TLS) answered → completion round-trip, mirroring `llm_generate_helixllm_live_test.go` pattern with SKIP-when-unreachable per §11.4.3 (no fake PASS). |
| HXC-002-F3-04 | helix_code | high | `cmd/other_commands.go:116-134` (+`internal/llm/model_manager.go:70-76,109-110`) | `helix generate` CLI is non-functional: `NewModelManager()` registers zero providers, so `SelectOptimalModel` always returns "no models available" regardless of `llm.default_provider`; `ProviderType("local")` is not a registered/known type in the empty map. `cfg.LLM.DefaultProvider` is read but cannot resolve to any provider instance. | Route `helix generate` through the same resolver as the server (`resolveLLMProvider` semantics) or register providers from config into `ModelManager` before selection; make `local` map to the coder/gateway route. | RED: `helix generate "prompt"` with default config currently errors "no models available" (capture); GREEN: same command returns a real completion from the local route; regression test pinning the registration path so the empty-map defect cannot recur (§11.4.135 guard). |
| HXC-002-F3-05 | helix_code | medium | module-wide (zero matches) | "Colibri" is entirely absent from the Go module — zero case-insensitive matches for `colibri` in any `.go/.yaml/.md/.sh`. Spec 002's "dynamic hardware-based model selection via Colibri" has no implementation surface in `helix_code`; closest existing machinery is `ModelManager`'s hardware detector hook (`internal/llm/model_manager.go:70-72`, `hardware.NewDetector()`) with no Colibri policy on top. UNCONFIRMED: submodules outside `helix_code/` (e.g. `submodules/helix_llm`) not scanned in this pass. | Locate/confirm the Colibri component (submodule catalogue check per §11.4.74); define the model-selection contract (hardware profile → model tier) as config-injected data; wire it between hardware detection and provider/model selection. | Unit: hardware-profile → model-selection table with boundary cases; integration: selection against the real coder catalog (`/v1/models`) asserting a verifier-sourced model per CONST-036; honest SKIP-with-reason if Colibri service absent. |
| HXC-002-F3-06 | helix_code | medium | `.env.example:50` vs `internal/server/llm_generate.go:619-630` | Env-var contract drift for local endpoints: `.env.example` documents `HELIX_LLAMA_CPP_HOST=http://localhost:8080`, but :8080 is HelixCode's OWN server port — requests would POST completions to itself (measured 404/502 documented at `llm_generate.go:619-630`); code default is `:18434`. Dead keys `HELIX_LLM_ENDPOINT`/`HELIX_DEBATE_API_KEY`/`HELIX_DEBATE_ENDPOINT` read by no Go code. | Align `.env.example` with the real defaults (`:18434` coder, `:11434` ollama), remove dead gateway/debate keys, and point operators at the single override convention (`HELIX_LLM_LOCAL_OPENAI_ENDPOINT`). | RED: load `.env.example` defaults → resolver must not construct a provider pointing at `:8080`; unit asserting `envLlamaCppHost()` precedence (explicit env > `HELIX_LLM_LOCAL_OPENAI_ENDPOINT` > `:18434`) per `llm_generate_llamacpp_local_test.go:327`. |

## JSON (same schema, exact keys)

```json
[
  {
    "id": "HXC-002-F3-01",
    "repo": "helix_code",
    "severity": "critical",
    "file": "internal/server/llm_generate.go:176-180",
    "defect": "Config llm.default_provider (\"local\", internal/config/config.go:676) is never consumed by the server generate path: SelectorInput.Config is hardcoded \"\" (llm_generate.go:179), so provider-less requests fall to Ollama :11434 (llm_generate.go:251-266) instead of the config-declared \"local\" route to the coder at :18434 (llm_generate.go:202-204,530).",
    "fix_direction": "Thread cfg.LLM.DefaultProvider into SelectorInput.Config (precedence Flag > Env > Config already honoured at internal/llm/provider_factory.go:57); \"local\" in config must resolve to the helixllm coder route, not Ollama.",
    "test_plan": "Unit RED->GREEN with RED_MODE polarity per §11.4.115: resolveLLMProvider(\"\",\"\") under config default \"local\" constructs a provider with BaseURL http://localhost:18434 (not :11434); live round-trip mirroring internal/server/llm_generate_helixllm_live_test.go:53 against coder /v1/models, honest SKIP-when-unreachable."
  },
  {
    "id": "HXC-002-F3-02",
    "repo": "helix_code",
    "severity": "critical",
    "file": "config/config.yaml:154-173",
    "defect": "Dead provider block helix-llm/helix-debate: unknown types (no case in internal/llm/provider_factory.go:83-113), zero Go references, wrong default endpoints (:8081 = HelixCode server itself, :8082 = ChromaDB per config/replica-8081.yaml:187-196). Whole llm.providers block discarded at load: LLMConfig lacks a Providers field (internal/config/config.go:197-226) and strict.go:315-317 reports it discarded.",
    "fix_direction": "Remove the verbatim block (Step 2 capture) from config/config.yaml; drop dead keys HELIX_LLM_ENDPOINT/HELIX_LLM_API_KEY from .env.example:44,50; if a gateway provider is wanted, add it as a real factory type (F3-03) instead of resurrecting this block.",
    "test_plan": "RED: startup strict-load no longer reports llm.providers discarded for these entries; grep gate: zero references to helix-llm/helix-debate outside the removal commit; config load test asserting no ErrorUnused for the removed block."
  },
  {
    "id": "HXC-002-F3-03",
    "repo": "helix_code",
    "severity": "high",
    "file": "internal/server/llm_generate.go:530",
    "defect": "No route to the HelixLLM gateway (https://127.0.0.1:8443, unit helixllm-gateway.service): zero code/config references to :8443 (only comment config/replica-8081.yaml:189). The helixllm/local selector is hardwired to the llama.cpp coder at http://localhost:18434, plain HTTP, no TLS config, no gateway env key, no API-key handling. Gateway live reachability UNCONFIRMED (read-only pass).",
    "fix_direction": "Add a gateway endpoint key (env HELIX_LLM_GATEWAY_ENDPOINT or config-injected per §11.4.28) plus a provider route building an HTTPS client against the gateway with TLS trust config; keep the :18434 coder route as direct-sidecar fallback.",
    "test_plan": "Unit: selector gateway/helixllm-gateway resolves BaseURL https://127.0.0.1:8443; live test GET /v1/models over TLS then completion round-trip, mirroring llm_generate_helixllm_live_test.go with §11.4.3 SKIP-when-unreachable (no fake PASS)."
  },
  {
    "id": "HXC-002-F3-04",
    "repo": "helix_code",
    "severity": "high",
    "file": "cmd/other_commands.go:116-134",
    "defect": "helix generate CLI is non-functional: NewModelManager() registers zero providers (internal/llm/model_manager.go:70-76) so SelectOptimalModel always errors \"no models available\" (model_manager.go:109-110) regardless of llm.default_provider; ProviderType(\"local\") resolves against an empty provider map. cfg.LLM.DefaultProvider is read but can never resolve to a provider instance.",
    "fix_direction": "Route helix generate through the same resolver semantics as the server (resolveLLMProvider) or register config providers into ModelManager before selection; map \"local\" to the coder/gateway route.",
    "test_plan": "RED: helix generate \"prompt\" with default config errors \"no models available\" (capture as defect-present evidence); GREEN: same command returns a real completion from the local route; §11.4.135 regression guard pinning provider registration so the empty-map defect cannot recur."
  },
  {
    "id": "HXC-002-F3-05",
    "repo": "helix_code",
    "severity": "medium",
    "file": "module-wide (zero matches for 'colibri')",
    "defect": "Colibri is entirely absent from the Go module (zero case-insensitive matches in .go/.yaml/.md/.sh); spec 002's dynamic hardware-based model selection has no implementation surface in helix_code. Closest existing machinery: ModelManager hardware detector hook (internal/llm/model_manager.go:70-72) with no Colibri policy on top. Presence in submodules outside helix_code/: UNCONFIRMED.",
    "fix_direction": "Locate/confirm the Colibri component (§11.4.74 catalogue check); define the hardware-profile -> model-tier selection contract as config-injected data; wire it between hardware detection and provider/model selection.",
    "test_plan": "Unit: hardware-profile -> model-selection table with boundary cases; integration: selection against the real coder catalog (/v1/models) asserting a verifier-sourced model per CONST-036; honest SKIP-with-reason when the Colibri service is absent."
  },
  {
    "id": "HXC-002-F3-06",
    "repo": "helix_code",
    "severity": "medium",
    "file": ".env.example:50",
    "defect": "Env-var contract drift: .env.example documents HELIX_LLAMA_CPP_HOST=http://localhost:8080, but :8080 is HelixCode's own server port (self-POST 404/502 measured and documented at internal/server/llm_generate.go:619-630); code default is :18434. Dead keys HELIX_LLM_ENDPOINT/HELIX_DEBATE_ENDPOINT/HELIX_DEBATE_API_KEY are read by no Go code.",
    "fix_direction": "Align .env.example with real defaults (:18434 coder, :11434 ollama), remove dead gateway/debate keys, and document the single override convention (HELIX_LLM_LOCAL_OPENAI_ENDPOINT).",
    "test_plan": "RED: loading .env.example defaults must never construct a provider pointing at :8080; unit asserting envLlamaCppHost() precedence chain (explicit env > HELIX_LLM_LOCAL_OPENAI_ENDPOINT > :18434) per internal/server/llm_generate_llamacpp_local_test.go:327."
  }
]
```
