# HelixCode E2E Testing Framework

A comprehensive end-to-end testing framework for HelixCode, featuring a test orchestrator, mock services, and automated testing workflows.

## Overview

The E2E Testing Framework provides:

- **Test Orchestrator MVP**: Priority-based test execution with parallel execution, retries, and comprehensive reporting
- **Mock Services**: HTTP mock services for LLM providers and Slack/notifications
- **Test Bank**: Curated collection of test cases organized by category and priority
- **Automation Scripts**: Quick setup, service management, and test execution
- **Docker Support**: Containerized mock services with Docker Compose orchestration

## Quick Start

### 1. Setup

Run the setup script to build all components:

```bash
cd tests/e2e
./scripts/setup.sh
```

This will:
- Build the test orchestrator
- Build mock LLM provider and Slack services
- Create environment configuration
- Make all scripts executable

### 2. Start Mock Services

Start all mock services required for testing:

```bash
./scripts/start-services.sh
```

Services will start on:
- Mock LLM Provider: http://localhost:8090
- Mock Slack Service: http://localhost:8091

### 3. Run Tests

Execute E2E tests using the orchestrator:

```bash
# Run all tests
./scripts/run-tests.sh

# Run critical priority tests only
./scripts/run-tests.sh --priority critical

# Run specific test categories
./scripts/run-tests.sh --tags "auth,security"

# Run with custom parallelism
./scripts/run-tests.sh --parallel 5
```

### 4. Stop Services

Stop all running services:

```bash
./scripts/stop-services.sh
```

### 5. Cleanup

Clean up build artifacts and stop services:

```bash
./scripts/clean.sh
```

## Architecture

```
tests/e2e/
├── orchestrator/          # Test execution orchestrator
│   ├── pkg/              # Core orchestrator packages
│   ├── cmd/              # CLI application
│   ├── bin/              # Built binaries
│   └── README.md         # Orchestrator documentation
├── test-bank/            # Test case repository
│   ├── core/             # Core test implementations
│   ├── metadata/         # Test metadata (JSON)
│   ├── loader.go         # Test suite loader
│   └── README.md         # Test bank documentation
├── mocks/                # Mock services
│   ├── llm-provider/     # Mock LLM API service
│   └── slack/            # Mock Slack/notifications service
├── scripts/              # Automation scripts
│   ├── setup.sh          # Initial setup
│   ├── start-services.sh # Start mock services
│   ├── stop-services.sh  # Stop services
│   ├── run-tests.sh      # Execute tests
│   └── clean.sh          # Cleanup
├── docker-compose.yml    # Docker orchestration
├── .env.example          # Environment template
└── README.md             # This file
```

## Components

### Test Orchestrator

The orchestrator manages test execution with:

- **Priority-Based Scheduling**: Critical, High, Normal, Low
- **Parallel Execution**: Configurable concurrency with semaphore control
- **Retry Logic**: Automatic retries with configurable delays
- **Multiple Report Formats**: JSON, JUnit XML, console output
- **Timeout Management**: Per-test and global timeouts

**CLI Usage:**

```bash
cd orchestrator

# Run all tests
./bin/orchestrator run

# List available tests
./bin/orchestrator list

# Run with filters
./bin/orchestrator run --priority critical --parallel 5
./bin/orchestrator run --tags "smoke,regression"

# Custom output
./bin/orchestrator run --output ./custom-results --format json

# Show version
./bin/orchestrator version
```

See [orchestrator/README.md](./orchestrator/README.md) for detailed documentation.

### Test Bank

Collection of 10+ core test cases covering:

- **Authentication & Security**: User auth, session management, API tokens
- **LLM Integration**: Chat completions, streaming, model selection
- **Worker Management**: SSH connections, health checks, task distribution
- **Project Lifecycle**: Creation, configuration, builds
- **Notifications**: Slack messages, webhooks, error alerts

Test metadata format:

```json
{
  "id": "TC-001",
  "name": "User Authentication",
  "description": "Validates user authentication flow",
  "priority": "critical",
  "category": "auth",
  "tags": ["auth", "security", "smoke"],
  "timeout": "30s",
  "dependencies": []
}
```

See [test-bank/README.md](./test-bank/README.md) for test details.

### Mock LLM Provider

HTTP service simulating LLM provider APIs:

- **OpenAI-Compatible**: `/v1/chat/completions`, `/v1/embeddings`, `/v1/models`
- **Pattern-Based Responses**: Context-aware mock responses
- **Multiple Models**: GPT-4, Claude, Llama, Mistral
- **Configurable Delays**: Simulate API latency

**Endpoints:**

```bash
# Chat completion
curl -X POST http://localhost:8090/v1/chat/completions \
  -H "Content-Type: application/json" \
  -d '{"model": "mock-gpt-4", "messages": [{"role": "user", "content": "Hello"}]}'

# Embeddings
curl -X POST http://localhost:8090/v1/embeddings \
  -H "Content-Type: application/json" \
  -d '{"model": "mock-text-embedding-ada-002", "input": ["Hello world"]}'

# List models
curl http://localhost:8090/v1/models
```

See [mocks/llm-provider/README.md](./mocks/llm-provider/README.md) for API documentation.

### Mock Slack Service

HTTP service simulating Slack APIs:

- **Message Posting**: `/api/chat.postMessage`
- **Incoming Webhooks**: `/webhook/:id`
- **Testing Endpoints**: Inspect stored messages and webhooks
- **In-Memory Storage**: Configurable capacity (default: 1000)

**Endpoints:**

```bash
# Post message
curl -X POST http://localhost:8091/api/chat.postMessage \
  -H "Content-Type: application/json" \
  -d '{"channel": "#general", "text": "Test message"}'

# Get messages
curl http://localhost:8091/api/messages

# Send webhook
curl -X POST http://localhost:8091/webhook/test-id \
  -H "Content-Type: application/json" \
  -d '{"text": "Webhook test"}'

# Get webhooks
curl http://localhost:8091/api/webhooks
```

See [mocks/slack/README.md](./mocks/slack/README.md) for API documentation.

## Docker Support

### Using Docker Compose

Start all services with Docker Compose:

```bash
# Start all services
docker-compose up -d

# View logs
docker-compose logs -f

# Stop services
docker-compose down

# Rebuild services
docker-compose build
docker-compose up -d
```

Services included:
- Mock LLM Provider (port 8090)
- Mock Slack Service (port 8091)
- PostgreSQL (port 5432)
- Redis (port 6379)

### Building Individual Docker Images

```bash
# Build Mock LLM Provider
cd mocks/llm-provider
docker build -t mock-llm-provider .
docker run -p 8090:8090 mock-llm-provider

# Build Mock Slack
cd mocks/slack
docker build -t mock-slack .
docker run -p 8091:8091 mock-slack
```

## Configuration

### Environment Variables

Copy `.env.example` to `.env` and customize:

```bash
cp .env.example .env
```

**Key Configuration Options:**

```bash
# Test Orchestrator
E2E_CONCURRENT_TESTS=3       # Parallel test execution
E2E_TIMEOUT=300s             # Default test timeout
E2E_RETRY_DELAY=1s           # Retry delay

# Mock LLM Provider
MOCK_LLM_PORT=8090           # Service port
MOCK_LLM_DELAY_MS=100        # Response delay
MOCK_LLM_DEFAULT_MODEL=mock-gpt-4

# Mock Slack
MOCK_SLACK_PORT=8091         # Service port
MOCK_SLACK_DELAY_MS=50       # Response delay
MOCK_SLACK_STORAGE_CAPACITY=1000

# Database
POSTGRES_HOST=localhost
POSTGRES_PORT=5432
POSTGRES_DB=helixcode_test
POSTGRES_USER=helixcode
POSTGRES_PASSWORD=CHANGE_ME_local_test_password  # required — docker-compose.yml has no fallback (HXC-168)

# Redis
REDIS_HOST=localhost
REDIS_PORT=6379
REDIS_PASSWORD=CHANGE_ME_local_test_password  # required — docker-compose.yml has no fallback (HXC-168)
```

## Workflows

### Development Workflow

1. **Make code changes** to HelixCode
2. **Start mock services**: `./scripts/start-services.sh`
3. **Run relevant tests**: `./scripts/run-tests.sh --tags "your-feature"`
4. **Review results** in `test-results/`
5. **Iterate** as needed

### CI/CD Integration

```yaml
# Example GitHub Actions workflow
- name: Setup E2E Tests
  run: |
    cd tests/e2e
    ./scripts/setup.sh

- name: Start Services
  run: ./tests/e2e/scripts/start-services.sh

- name: Run E2E Tests
  run: ./tests/e2e/scripts/run-tests.sh --output ./test-results

- name: Upload Results
  uses: actions/upload-artifact@v3
  with:
    name: e2e-test-results
    path: tests/e2e/test-results/
```

### Adding New Tests

1. **Create test implementation** in `test-bank/core/tests.go`:

```go
func TC011_YourNewTest() *pkg.TestCase {
    return &pkg.TestCase{
        ID:       "TC-011",
        Name:     "Your New Test",
        Priority: pkg.PriorityHigh,
        Timeout:  30 * time.Second,
        Tags:     []string{"feature", "smoke"},
        Execute: func(ctx context.Context) error {
            // Your test logic
            return nil
        },
    }
}
```

2. **Add metadata** in `test-bank/metadata/core_tests.json`

3. **Register in loader** (`test-bank/loader.go`)

4. **Run the test**:
```bash
./scripts/run-tests.sh --tags "feature"
```

## Test Reports

Test results are generated in multiple formats:

### JSON Report

```json
{
  "summary": {
    "total": 10,
    "passed": 9,
    "failed": 1,
    "skipped": 0,
    "duration": "2.5s"
  },
  "tests": [...]
}
```

### JUnit XML Report

Compatible with CI/CD systems like Jenkins, GitLab CI, GitHub Actions:

```xml
<testsuite name="E2E Tests" tests="10" failures="1" time="2.5">
  <testcase classname="core" name="TC-001" time="0.5"/>
  ...
</testsuite>
```

### Console Output

```
========================================
E2E Test Execution Report
========================================

Summary:
  Total Tests: 10
  Passed:      9 (90.0%)
  Failed:      1 (10.0%)
  Skipped:     0 (0.0%)
  Duration:    2.5s

Results:
  ✓ TC-001: User Authentication (0.5s)
  ✓ TC-002: Session Management (0.3s)
  ...
```

## Troubleshooting

### Services Not Starting

```bash
# Check if ports are in use
lsof -i :8090
lsof -i :8091

# View service logs
cat .pids/mock-llm.log
cat .pids/mock-slack.log

# Force stop and restart
./scripts/stop-services.sh
./scripts/start-services.sh
```

### Tests Failing

```bash
# Run with verbose output
cd orchestrator
./bin/orchestrator run --tags "failing-test" -v

# Check test results
cat test-results/report.json

# Verify mock services are healthy
curl http://localhost:8090/health
curl http://localhost:8091/health
```

### Build Issues

```bash
# Clean and rebuild
./scripts/clean.sh
./scripts/setup.sh

# Verify Go version
go version  # Should be 1.24.0 or higher

# Check dependencies
cd orchestrator && go mod verify
cd ../mocks/llm-provider && go mod verify
cd ../slack && go mod verify
```

## Performance

### Test Execution Times

- **Smoke Tests** (8 tests): ~2 seconds
- **Core Tests** (10 tests): ~5 seconds
- **Full Suite** (30+ tests): ~15 seconds
- **Integration Tests** (50+ tests): ~30 seconds

### Resource Usage

- **Mock LLM Provider**: ~12MB binary, <50MB RAM
- **Mock Slack Service**: ~12MB binary, <30MB RAM
- **Test Orchestrator**: ~6MB binary, <20MB RAM
- **PostgreSQL**: ~30MB RAM (Docker)
- **Redis**: ~10MB RAM (Docker)

## Best Practices

1. **Always start services** before running tests
2. **Use appropriate priorities** for test criticality
3. **Tag tests properly** for easy filtering
4. **Set realistic timeouts** for long-running tests
5. **Clean up between runs** to avoid state issues
6. **Review test reports** for insights
7. **Keep mock fixtures updated** with real API changes
8. **Use Docker** for consistent environments

## Contributing

### Adding Mock Endpoints

1. Update handler in `mocks/*/handlers/`
2. Add routes in `cmd/main.go`
3. Update fixtures if needed
4. Document in README

### Extending Test Bank

1. Create test implementation
2. Add metadata
3. Register in loader
4. Document test purpose

## License

Part of the HelixCode project. See main repository for license details.

## Support

For issues or questions:

1. Check [Troubleshooting](#troubleshooting) section
2. Review component-specific READMEs
3. Check HelixCode main documentation
4. Open an issue in the main repository

## Version History

- **v1.0.0** (2025-01-07): Initial MVP release
  - Test Orchestrator with parallel execution
  - Mock LLM Provider and Slack services
  - Test Bank with 10 core tests
  - Docker Compose support
  - Automation scripts
