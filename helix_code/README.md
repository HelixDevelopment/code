![HelixCode - Distributed AI Development Platform](../assets/Wide_Black.png)

# HelixCode

A distributed, AI-powered software development platform with multi-platform support.

## Features

- **Multi-Platform Support**: Desktop, mobile, terminal, and specialized OS clients
- **Distributed Computing**: Worker nodes for parallel task execution
- **AI Integration**: LLM-powered code generation and reasoning with 15+ providers
- **Free AI Models**: Access to XAI (Grok), OpenRouter, GitHub Copilot, Qwen, and more
- **Cognee.ai Memory Integration**: Advanced memory management with knowledge graphs, semantic search, and real-time processing
- **Real-time Collaboration**: MCP protocol for tool execution
- **Authentication & Security**: JWT-based auth with session management
- **Task Management**: Checkpoint-based work preservation
- **Notification System**: Multi-channel notifications (Slack, Email, Discord, Telegram, Yandex Messenger, Max)

## Quick Start

### Development

```bash
# Clone the repository
git clone https://github.com/HelixDevelopment/Helix-CLI.git
cd helixcode

# Install dependencies
go mod download

# Generate assets
make logo-assets

# Build the server
make build

# Run tests
make test

# Start development server
make dev
```

### Production Deployment

1. **Clone and setup:**
   ```bash
   git clone https://github.com/HelixDevelopment/Helix-CLI.git
   cd helixcode
   cp .env.example .env
   ```

2. **Configure environment variables:**
   Edit `.env` file with your production values:
   ```bash
   HELIX_AUTH_JWT_SECRET=your-super-secure-jwt-secret
   HELIX_DATABASE_PASSWORD=your-secure-database-password
   HELIX_REDIS_PASSWORD=your-secure-redis-password
   ```

3. **Deploy with Docker Compose:**
   ```bash
   docker-compose up -d
   ```

4. **Check deployment:**
   ```bash
   docker-compose ps
   curl http://localhost/health
   ```

## Architecture

### Core Components

- **Server**: Main API server with REST and WebSocket endpoints
- **Database**: PostgreSQL for persistent data storage
- **Cache**: Redis for session and task state management
- **Workers**: Distributed worker nodes for task execution
- **MCP Server**: Model Context Protocol for AI tool integration

### AI Providers

HelixCode supports **15+ AI providers** through a unified provider interface with a cloud gate (W2c-1) for local-first adaptive serving. All providers are configured via `config/config.yaml` under `llm.providers` or the CLI wizard.

#### Cloud Providers (Direct API Access)

| Provider | Type | Key Env Var | Models | Notable Features |
|----------|------|-------------|--------|------------------|
| **Anthropic** | Cloud | `ANTHROPIC_API_KEY` | Claude 4 Sonnet/Opus, 3.7 Sonnet, 3.5 Sonnet/Haiku, 3 Opus/Sonnet/Haiku | Extended thinking, prompt caching (90% cost reduction), tool caching, vision, streaming |
| **Google Gemini** | Cloud | `GEMINI_API_KEY` / `GOOGLE_API_KEY` | Gemini 2.5 Pro/Flash, 2.0 Flash, 1.5 Pro/Flash | 2M token context (2.5 Pro/1.5 Pro), multimodal, function calling, flash models |
| **OpenAI** | Cloud | `OPENAI_API_KEY` | GPT-4.1, GPT-4.5 Preview, GPT-4o, o1/o3 (reasoning), o4-mini | 1M+ context (GPT-4.1), function calling, vision, reasoning models |
| **XAI (Grok)** | Cloud | `XAI_API_KEY` | Grok 3 Fast/Mini/Beta | Fast reasoning, free tier available |
| **Groq** | Cloud | `GROQ_API_KEY` | Llama 3.1/3.3, Mixtral, Gemma | Ultra-fast inference, free tier |
| **Mistral** | Cloud | `MISTRAL_API_KEY` | Mistral Large/Small, Codestral | Code-optimized models, function calling |
| **DeepSeek** | Cloud | `DEEPSEEK_API_KEY` | DeepSeek-V3, DeepSeek-R1 (reasoning) | Strong reasoning, code generation |
| **OpenRouter** | Cloud | `OPENROUTER_API_KEY` | 300+ models from all providers | Unified access, free models available |
| **Cohere** | Cloud | `COHERE_API_KEY` | Command R/R+, Aya | Multilingual, RAG-optimized |
| **GitHub Copilot** | Cloud | `GITHUB_TOKEN` | GPT-4o, Claude 3.5/3.7 Sonnet, o1, Gemini 2.0 Flash | Free with GitHub subscription |
| **Azure OpenAI** | Cloud | `AZURE_OPENAI_API_KEY` | GPT-4o, GPT-4, o1 | Enterprise Azure integration |
| **AWS Bedrock** | Cloud | `AWS_ACCESS_KEY_ID` / `AWS_SECRET_ACCESS_KEY` | Claude, Llama, Titan, Jurassic | AWS-native, multiple model families |
| **GCP Vertex AI** | Cloud | `GOOGLE_APPLICATION_CREDENTIALS` | Gemini, PaLM, Codey | GCP-native, enterprise features |
| **Qwen** | Cloud | `QWEN_API_KEY` / OAuth2 | Qwen 2.5/3/Max/Plus/Turbo | Chinese-optimized, 2K free req/day |
| **Replicate** | Cloud | `REPLICATE_API_TOKEN` | 1000+ open models | Pay-per-second, model hosting |

#### Local Providers (No Cloud Gate, Run Locally)

| Provider | Type | Requirements | Models | Notable Features |
|----------|------|--------------|--------|------------------|
| **Ollama** | Local | Ollama service | Any GGUF model | Easy model management, local-only |
| **Llama.cpp** | Local | llama.cpp binary | GGUF models | Direct llama.cpp, hardware acceleration |
| **vLLM** | Local | vLLM server | Any HF model | High-throughput serving, PagedAttention |
| **LocalAI** | Local | LocalAI server | OpenAI-compatible | Drop-in OpenAI replacement |
| **FastChat** | Local | FastChat server | Vicuna, LLaMA | Conversation templates |
| **LM Studio** | Local | LM Studio app | GGUF models | GUI + local server |
| **Jan** | Local | Jan app | GGUF models | Desktop app with API |
| **GPT4All** | Local | GPT4All app | Quantized models | Consumer-friendly |
| **TabbyAPI** | Local | TabbyAPI server | exllama models | ExLLaMAv2 wrapper |
| **MLX** | Local | Apple Silicon | MLX-format models | Apple Silicon native |
| **Mistral.rs** | Local | mistral.rs binary | Any HF model | Rust implementation, fast |
| **KoboldAI** | Local | KoboldAI server | Story/writing models | Storytelling-focused |
| **Xiaomi MiMo** | Local/Cloud | MiMo API key | MiMo v2.5 Pro/Omni/Flash | 1M context, multimodal, tool calling |

#### Special Providers
- **HelixAgent** — Embedded agent provider for multi-agent workflows
- **Cerebras** — Wafer-scale inference for Llama models
- **Together AI** — Optimized open-model serving
- **HuggingFace** — Inference endpoints for HF models

### Provider Selection Strategy

HelixCode uses an intelligent model selection system (`internal/llm/model_manager.go`):
- **Performance-based**: Selects fastest/lowest-latency model
- **Capability-aware**: Matches model capabilities to task requirements
- **Fallback chains**: Automatic failover on errors
- **Health monitoring**: Real-time provider health checks

### Applications

- **Desktop**: Full-featured desktop application (Fyne)
- **Terminal UI**: Terminal-based interface (tview)
- **Aurora OS**: Specialized Aurora OS client
- **Harmony OS**: Specialized Harmony OS client
- **Mobile**: Cross-platform mobile applications (gomobile)

## Configuration

Configuration is managed through YAML files and environment variables. See `config/config.yaml` for default settings.

Key configuration areas:
- Server settings (ports, timeouts)
- Database connection
- Redis configuration
- Authentication settings
- Worker management
- LLM provider settings (all 15+ providers)

### LLM Provider Configuration Example

```yaml
llm:
  default_provider: "local"
  max_tokens: 4096
  temperature: 0.7
  timeout: 30
  max_retries: 3
  providers:
    anthropic:
      type: "anthropic"
      endpoint: "https://api.anthropic.com"
      enabled: true
      parameters:
        api_key: "${ANTHROPIC_API_KEY}"
        streaming_support: true
    gemini:
      type: "gemini"
      endpoint: "https://generativelanguage.googleapis.com"
      enabled: true
      parameters:
        api_key: "${GEMINI_API_KEY}"
    ollama:
      type: "ollama"
      endpoint: "http://localhost:11434"
      enabled: true
    # ... more providers
  selection:
    strategy: "performance"
    fallback_enabled: true
    health_check_interval: 30
```

### Getting Started with Free AI

HelixCode comes with multiple free AI providers pre-configured:

#### Quick AI Setup
```bash
# Use XAI (Grok) - no setup required
helixcode llm provider set xai

# Use OpenRouter free models
helixcode llm provider set openrouter

# Use GitHub Copilot (requires GitHub token)
export GITHUB_TOKEN="your_github_token"
helixcode llm provider set copilot

# Use Qwen with OAuth2 (interactive setup)
helixcode llm auth qwen

# Use local Ollama
helixcode llm provider set ollama
```

#### Environment Variables for All Providers

**Free Providers:**
```bash
# GitHub Copilot
export GITHUB_TOKEN="ghp_your_github_token"

# OpenRouter (optional, for higher rate limits)
export OPENROUTER_API_KEY="sk-or-your-key"

# XAI (optional, for higher rate limits)
export XAI_API_KEY="xai-your-key"

# Groq (free tier)
export GROQ_API_KEY="gsk_your-key"

# Mistral (free tier)
export MISTRAL_API_KEY="your-mistral-key"
```

**Premium Providers:**
```bash
# Anthropic Claude
export ANTHROPIC_API_KEY="sk-ant-your-key"

# Google Gemini
export GEMINI_API_KEY="your-gemini-key"
# or
export GOOGLE_API_KEY="your-google-key"

# OpenAI
export OPENAI_API_KEY="sk-your-openai-key"

# DeepSeek
export DEEPSEEK_API_KEY="your-deepseek-key"

# Cohere
export COHERE_API_KEY="your-cohere-key"

# Azure OpenAI
export AZURE_OPENAI_API_KEY="your-azure-key"

# AWS Bedrock
export AWS_ACCESS_KEY_ID="your-access-key"
export AWS_SECRET_ACCESS_KEY="your-secret-key"

# Qwen
export QWEN_API_KEY="your-qwen-key"
```

## API Documentation

### Authentication Endpoints

- `POST /api/auth/register` - User registration
- `POST /api/auth/login` - User login
- `POST /api/auth/logout` - User logout
- `POST /api/auth/refresh` - Token refresh
- `GET /api/auth/me` - Current user info

### Task Management

- `GET /api/tasks` - List tasks
- `POST /api/tasks` - Create task
- `GET /api/tasks/{id}` - Get task details
- `PUT /api/tasks/{id}` - Update task
- `DELETE /api/tasks/{id}` - Delete task

### Worker Management

- `GET /api/workers` - List workers
- `POST /api/workers` - Register worker
- `GET /api/workers/{id}` - Get worker details
- `DELETE /api/workers/{id}` - Remove worker

### LLM Provider Management

- `GET /api/v1/llm/providers` - List all configured providers with status
- `POST /api/v1/llm/providers` - Add/configure provider
- `GET /api/v1/llm/models` - List available models across providers
- `POST /api/v1/llm/generate` - Generate completion
- `POST /api/v1/llm/chat` - Chat completion with history

## Development

### Building Applications

```bash
# Build all applications
make prod

# Build specific applications
make aurora-os
make harmony-os

# Build mobile bindings
make mobile-ios
make mobile-android
```

### Testing

#### Test Infrastructure (MANDATORY)

Before running integration tests, you **MUST** start the test infrastructure containers. These provide PostgreSQL, Redis, ChromaDB, Qdrant, Cognee, and Ollama services needed for full test coverage.

**Start Test Infrastructure:**
```bash
# Using Docker Compose
docker compose -f docker-compose.test.yml up -d

# OR using Podman
podman-compose -f docker-compose.test.yml up -d

# Wait for all services to be healthy (check status)
docker compose -f docker-compose.test.yml ps
# OR
podman-compose -f docker-compose.test.yml ps
```

**Test Infrastructure Services:**
| Service | Port | Purpose |
|---------|------|---------|
| postgres-test | 5433 | PostgreSQL database for persistence tests |
| redis-test | 6380 | Redis for caching and session tests |
| chromadb-test | 8002 | ChromaDB vector storage tests |
| qdrant-test | 6333/6334 | Qdrant vector database tests |
| cognee-test | 8001 | Cognee.ai memory integration tests |
| ollama-test | 11434 | Local LLM integration tests |

**Stop Test Infrastructure:**
```bash
docker compose -f docker-compose.test.yml down -v
# OR
podman-compose -f docker-compose.test.yml down -v
```

#### Running Tests

```bash
# Run all tests (requires test infrastructure)
make test

# Run unit tests only (no infrastructure needed)
./run_tests.sh

# Run all tests including integration and e2e
./run_all_tests.sh

# Run specific test suites
go test ./internal/auth/...
go test ./internal/worker/...

# Run with coverage
go test -cover ./...

# Run with coverage report
make test-coverage
```

### Code Quality

```bash
# Format code
make fmt

# Lint code
make lint
```

## Deployment Options

### Docker Compose (Recommended)

The production `docker-compose.yml` includes:
- HelixCode server
- PostgreSQL database
- Redis cache
- Nginx reverse proxy
- Prometheus monitoring
- Grafana dashboards

### Manual Deployment

1. Build the binary: `make prod`
2. Setup PostgreSQL and Redis
3. Configure environment variables
4. Run the server: `./bin/helixcode-server`

### Kubernetes

For large-scale deployments, use the provided Kubernetes manifests in the `k8s/` directory.

## Monitoring

The deployment includes Prometheus and Grafana for monitoring:
- Application metrics
- Database performance
- Worker health
- Task execution stats

Access Grafana at `http://localhost:3000` (default credentials: admin/admin)

## Security

- JWT-based authentication
- Password hashing with bcrypt + argon2
- SSH key-based worker authentication
- Environment variable configuration
- No secrets in code or config files
- Cloud gate (W2c-1) prevents accidental cloud provider usage

## Contributing

1. Fork the repository
2. Create a feature branch
3. Make your changes
4. Add tests
5. Submit a pull request

## License

This project is licensed under the terms specified in the LICENSE file.

## Documentation

- [TUI Capabilities Guide](docs/CAPABILITIES.md) — streaming, MCP tools, LSP diagnostics, skills, plugins, the agentic read-only tool loop, the Helix Agent ensemble (visible members + per-member model via LLMsVerifier), environment providers, and the HelixAgent full-capacity provider, each with a real trigger and example prompt.
- [Zero-Bluff User Manual](docs/user_manual/ZERO_BLUFF_USER_MANUAL.md) — Complete user guide
- [Provider Aliases Guide](submodules/claude-toolkit/docs/Provider_Aliases_User_Guide.md) — claude-providers tool documentation

## Support

For support and questions:
- GitHub Issues: https://github.com/HelixDevelopment/Helix-CLI/issues
- Documentation: https://docs.helixcode.dev
- Community: https://community.helixcode.dev
