# CLI-agent custom OpenAI-compatible provider config contracts — captured evidence

Research date: 2026-09-05. Host: linux, `/home/milosvasic`.
Method per §11.4.99: shipped artifacts (binary strings, shipped docs, `--help`,
`crush schema`) + latest official online docs (fetched 2026-09-05) + live runs.

## Observed versions (command output)

```
$ opencode --version -> 1.18.29        (bin: ~/.opencode/bin/opencode, Bun single-file)
$ pi --version       -> 0.84.4         (@earendil-works/pi-coding-agent, node)
$ crush --version    -> crush version v0.91.2  (@charmland/crush -> Go ELF at
                                          .../node_modules/@charmland/crush/bin/crush)
$ claude --version   -> 2.1.261 (Claude Code)  (~/.local/share/claude/versions/2.1.261, ELF)
```

## Local endpoints probed (2026-09-05)

```
$ curl -s http://localhost:18434/v1/models
  -> 200; llama.cpp server ("owned_by":"llamacpp"), one model
     id = qwen2.5-coder-3b-instruct-q4_k_m, meta.n_ctx = 4096, n_ctx_train = 32768
$ curl -o /dev/null -w '%{http_code}' https://localhost:8443/v1/models   (with --cacert)
  -> 200; TLS cert: subject=CN=helixllm issuer=CN=helixllm (SELF-SIGNED),
     notAfter=Apr 9 2036. Without --cacert: curl error 60 "self-signed certificate (18)".
     Gateway model id = helixllm-anton-qwen2-5-coder-3b-instruct-q4_k_m-f6771589d190
     (DIFFERENT from the raw :18434 model id)
$ curl http://localhost:7061/v1/models -> 200   (provider already in the user's opencode config)
```

Raw completion latency on :18434 is ~0.3 s:
```
$ curl -s http://localhost:18434/v1/chat/completions -d '{"model":"qwen2.5-coder-3b-instruct-q4_k_m",...}'
{"choices":[{"finish_reason":"stop","index":0,"message":{"role":"assistant","content":"HELIXOK"}}],...}
real 0m0.299s
```

## opencode 1.18.29

Global config load order, decompiled verbatim from the shipped Bun bundle
(`strings ~/.opencode/bin/opencode`):

```
X=h$(X,yield*R(p.join(e.Path.config,"config.json"),j)),
X=h$(X,yield*R(p.join(e.Path.config,"opencode.json"),j)),
X=h$(X,yield*R(p.join(e.Path.config,"opencode.jsonc"),j));
  then p.join(e.Path.config,"config")   // TOML
```
=> `~/.config/opencode/{config.json, opencode.json, opencode.jsonc, config(.toml)}`
are all read and MERGED in that order; later wins.

Path literals present in the binary:
```
.config/opencode/opencode.json
.config/opencode/opencode.jsonc
.config/opencode/tui.json
.config/opencode/{agent,command,skill}
```
`~/.opencode/` is NOT a config location in the binary — it holds `bin/` and the
plugin `node_modules` only.

Project-config discovery (same bundle):
```
findUp(["opencode.json","opencode.jsonc"], cwd, ..., {rootFirst:!0})
```
and the "which file do I edit" resolver prefers `.jsonc`:
```
["opencode.jsonc","opencode.json","config.json"]
```

TLS-related literals in the binary: `NODE_EXTRA_CA_CERTS`,
`NODE_TLS_REJECT_UNAUTHORIZED`, `SSL_CERT_FILE`, `rejectUnauthorized`.

Registration proof:
```
$ OPENCODE_CONFIG=<fixture>.json opencode models | grep -i helix
helixagent/helixagent-debate
helixagent/helixagent-llm
helixllm-test/qwen2.5-coder-3b-instruct-q4_k_m     <-- newly declared provider
```

## pi 0.84.4

Shipped docs (authoritative for this version) live at
`~/.nvm/versions/node/v26.8.1/lib/node_modules/@earendil-works/pi-coding-agent/docs/`:
`models.md` (declarative `models.json`), `custom-provider.md` (extension API),
`providers.md`, `settings.md`, `environment-variables.md`.

`docs/models.md` opening line, verbatim:
> Add custom providers and models (Ollama, vLLM, LM Studio, proxies) via `~/.pi/agent/models.json`.

`docs/environment-variables.md`: `PI_CODING_AGENT_DIR` — "Override the config
directory; default is `~/.pi/agent`".

Registration proof (isolated config dir, real fixture `models.json`):
```
$ PI_CODING_AGENT_DIR=<fixture-dir> PI_OFFLINE=1 pi --list-models | grep -i helix
helixllm     qwen2.5-coder-3b-instruct-q4_k_m     32.8K   4.1K   no   no
```

Live-call proof (reached the provider; upstream answered):
```
$ PI_CODING_AGENT_DIR=<fixture-dir> pi -p --mode json --provider helixllm \
    --model qwen2.5-coder-3b-instruct-q4_k_m --api-key local "Reply with exactly the word: HELIXOK"
{"type":"message_end","message":{... "provider":"helixllm","stopReason":"error",
 "errorMessage":"400: {\"code\":400,\"message\":\"request (141440 tokens) exceeds the
 available context size (4096 tokens), try increasing it\",\"type\":\"exceed_context_size_error\",
 \"n_prompt_tokens\":141440,\"n_ctx\":4096}"}}
```
This is positive wiring evidence (the request really reached :18434 and llama.cpp
replied) AND a real operational finding: the server's runtime context is 4096
tokens, far below what pi's system prompt needs.

## crush v0.91.2

`crush dirs` output:
```
/home/milosvasic/.config/crush
/home/milosvasic/.local/share/crush
/etc/crush
/home/milosvasic/Projects/helix_code   (project dir)
```

Usage strings extracted verbatim from the shipped Go binary:
```
usage: provider add <id> [--name NAME] [--type TYPE] [--api-key KEY] [--base-url URL]
       [--disable true|false] [--flat-rate true|false] [--discover-models true|false]
       [--system-prompt-prefix TEXT] [--extra-header KEY VALUE] [--extra-body JSON]
       [--provider-options JSON]
usage: model add <provider>/<id> [--name NAME] [--context-window N] [--default-max-tokens N]
       [--can-reason true|false] [--supports-images true|false] [--price-input F]
       [--price-output F] [--price-cache-create F] [--price-cache-hit F]
       [--reasoning-effort low|medium|high]
usage: model add|remove <provider>/<id> | model large|small [<provider>/<id>]
usage: option <key> [value]
provider add <id> [flags]    # define/update; repeated calls merge
model add: provider %q does not exist (declare it with `provider add %s` first)
```

`crush schema` (shipped JSON-schema emitter) `ProviderConfig.type` enum:
```
openai, openai-compat, openrouter, vercel, anthropic, google, azure, bedrock,
google-vertex, hyper, litellm, llamacpp, lmstudio, ollama, omlx
```
and `discover_models`: "Auto-discover models from /v1/models endpoint. When true
with existing models they are merged (yours win)", default `true`.
`Config` and `ProviderConfig` both declare `"additionalProperties": false`.

Registration proof (crushrc fixture, isolated config+data dirs):
```
$ CRUSH_GLOBAL_CONFIG=<fixture> CRUSH_GLOBAL_DATA=<fixture-data> crush models | grep -i helix
helixllm/qwen2.5-coder-3b-instruct-q4_k_m
```

Auto-discovery proof (provider only, NO `model add`):
```
$ (crushrc containing only: provider add helixllm ... --discover-models true)
$ crush models | grep -i helix
helixllm/qwen2.5-coder-3b-instruct-q4_k_m
```

END-TO-END LIVE PROOF:
```
$ CRUSH_GLOBAL_CONFIG=<fixture> CRUSH_GLOBAL_DATA=<fixture-data> crush run -q "Reply with exactly the word: HELIXOK"
HELIXOK
```

TLS PROOF against the self-signed :8443 gateway (isolated cwd so no stale state):
```
# A) no custom CA:      crush models | grep -c helixgw  -> 0     (discovery failed)
# B) SSL_CERT_FILE=pem: crush models | grep helixgw     -> helixgw/helixllm-anton-qwen2-5-coder-3b-instruct-q4_k_m-f6771589d190
```
So `SSL_CERT_FILE` (Go stdlib) is the working TLS-trust knob for crush; there is
NO TLS field anywhere in `crush schema` (grep for tls|insecure|verify|cert|proxy
returns nothing).

Side effect noticed and cleaned: running `crush` in the repo created
`helix_code/.crush/` (its `data_directory` default is `.crush` relative to cwd).
It was removed. Use `--cwd` / `--data-dir` when testing.

## Claude Code 2.1.261

`ANTHROPIC_*` env literals present in the shipped binary include:
`ANTHROPIC_BASE_URL`, `ANTHROPIC_AUTH_TOKEN`, `ANTHROPIC_API_KEY`,
`ANTHROPIC_CUSTOM_HEADERS`, `ANTHROPIC_BETAS`, `ANTHROPIC_CONFIG_DIR`,
`ANTHROPIC_MODEL`, `ANTHROPIC_DEFAULT_{OPUS,SONNET,HAIKU,FABLE}_MODEL`,
`ANTHROPIC_CUSTOM_MODEL_OPTION{,_NAME,_DESCRIPTION,_SUPPORTED_CAPABILITIES}`,
`ANTHROPIC_BEDROCK_BASE_URL`, `ANTHROPIC_VERTEX_*`, `ANTHROPIC_AWS_BASE_URL`.
TLS literals: `NODE_EXTRA_CA_CERTS`, `NODE_TLS_REJECT_UNAUTHORIZED`,
`SSL_CERT_FILE`, `HTTPS_PROXY`.

Official docs (fetched 2026-09-05, https://code.claude.com/docs/en/llm-gateway-protocol):
the "API formats" table lists exactly three client-selectable formats —
Anthropic Messages (`ANTHROPIC_BASE_URL`, endpoints `/v1/messages`,
`/v1/messages/count_tokens`), Amazon Bedrock InvokeModel, Google Agent Platform
rawPredict. There is NO OpenAI Chat Completions row.
https://code.claude.com/docs/en/llm-gateway states verbatim: "Anthropic doesn't
endorse, maintain, or audit third-party gateway products, and doesn't support
routing Claude Code to non-Claude models through any gateway."

Model discovery (same page): `GET /v1/models?limit=1000`, 3-second timeout,
redirects treated as failure, enabled by `CLAUDE_CODE_ENABLE_GATEWAY_MODEL_DISCOVERY=1`;
Claude Code "keeps an entry when its `id` contains `claude` or `anthropic`
anywhere in the string, matched case-insensitively, and ignores the rest."

## Sources verified 2026-09-05

- https://opencode.ai/docs/providers/
- https://opencode.ai/docs/config/
- https://github.com/charmbracelet/crush  (README; also read the byte-identical
  shipped copy at .../node_modules/@charmland/crush/README.md, v0.91.2)
- https://code.claude.com/docs/en/settings
- https://code.claude.com/docs/en/llm-gateway
- https://code.claude.com/docs/en/llm-gateway-protocol
- https://code.claude.com/docs/en/llm-gateway-connect
- shipped: `@earendil-works/pi-coding-agent@0.84.4` docs/{models,providers,settings,custom-provider,environment-variables}.md

## Additional observed facts (2026-09-05)

- **opencode installs the provider's `npm` package into the config directory.**
  Running `opencode models` with `OPENCODE_CONFIG_DIR=<fixture>` created a 63 MB
  `node_modules/` there containing `@ai-sdk/*`, `@opencode-ai/*`, `effect`, etc.
  The user's real `~/.config/opencode/node_modules` (with `@opencode-ai/plugin`)
  exists for the same reason. First use of a new `npm:` provider therefore needs
  either network access or the package already present.
- **`OPENCODE_CONFIG_DIR` did not isolate the global config**: with it set to an
  empty fixture dir, `opencode models` still listed `helixagent/*` from the user's
  real `~/.config/opencode/opencode.json`. Treat it as NOT a sandbox switch.
- **Inconclusive live runs (recorded honestly, not as failures of the config):**
  `opencode run --pure --model helixllm-test/...` produced no output before a
  280 s `timeout` (SIGTERM, rc 143). `pi -p` against the HTTPS gateway timed out
  at 90 s both with and without `NODE_EXTRA_CA_CERTS`. Neither run yields a
  usable verdict on generation or TLS trust for those two agents.
