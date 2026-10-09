.PHONY: all build test stress-chaos stress-chaos-meta stress-chaos-infra clean logo-assets setup-deps sync-manual manual-html docs release

# Build variables
BINARY_NAME=helixcode
VERSION=3.0.0
BUILD_TIME=$(shell date +%Y-%m-%d_%H:%M:%S)
GIT_COMMIT=$(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")

# Go variables
GO=go
GO_BUILD=$(GO) build
GO_TEST=$(GO) test
GO_MOD=$(GO) mod
GOFMT=$(GO) fmt
GOLINT=golangci-lint run

# ============================================
# BUILD-CACHE & TEST-PARALLELISM TUNING — speed-programme Phase 5 task P5-T01
# (R1 bottleneck-audit B20 — test targets were untuned for -p/-parallel)
# ============================================
#
# Go's build cache (GOCACHE) is leveraged automatically by every `go build`
# and `go test` below — we deliberately do NOT pass `-a` (force-rebuild) or a
# blanket `-count=1`, because either would defeat the cache. `-count=1` IS
# kept on the targets where it is load-bearing for correctness:
#   * test-coverage / coverage-full — -race + -covermode=atomic need a fresh
#     run; a cached coverage profile would be stale.
#   * test-full / test-*-full / test-verifier-* — real-infrastructure
#     (PostgreSQL/Redis/Ollama/verifier) integration runs MUST re-execute
#     against the live stack every time; a cached PASS would be a CONST-035
#     anti-bluff false-success (a green line with no fresh runtime evidence).
# The plain `make test` (unit, hermetic, no external infra) keeps the cache:
# unchanged packages are not re-run, so the developer-experience suite is
# fast on a warm tree and only re-runs what actually changed.
#
# Parallelism knobs:
#   GO_TEST_P        — `-p`: number of test BINARIES (packages) compiled and
#                      run concurrently. Defaults to GOMAXPROCS (host CPU
#                      count). Capping it keeps a large multi-package suite
#                      from oversubscribing CPU + RAM on smaller dev hosts.
#   GO_TEST_PARALLEL — `-parallel`: number of `t.Parallel()` tests allowed to
#                      run simultaneously WITHIN a single package binary.
#                      Defaults to GOMAXPROCS; set explicitly for stable,
#                      reproducible scheduling across machines.
# Both are computed from the CPU count that ACTUALLY BINDS this process, so the
# suite scales with the machine without hardcoding a number. Override on the
# command line:
#   make test GO_TEST_P=4 GO_TEST_PARALLEL=4
#
# NOT `nproc` — that reports the host's CPU count and ignores the cgroup quota.
# Measured on this host 2026-08-05: `nproc` says 64 while the binding quota is
# 8.6 CPUs (`cpu.max = 860000 100000` on a PARENT slice). Go 1.25+ is
# container-aware but reads the process's own LEAF cgroup, which here is a
# per-command `run-<id>.scope` carrying no `cpu.max`; finding none, it falls
# back to NumCPU(). So every build and test ran 64-way against 8.6 CPUs —
# 7.4x oversubscription per process, and roughly 30x with four agents.
# Consequence: 50.3% of scheduling periods throttled, 75.5 hours of wall-clock
# frozen, 0.62s frozen per 1s of CPU obtained. Long commands overran the 600s
# agent watchdog and were reported as "stalled" when they were merely starved.
#
# cgroup_cpu_quota walks every level of the hierarchy and takes the MINIMUM,
# which is the only correct reading of a nested limit. It falls back to nproc
# when genuinely unconstrained, so this is a no-op on an unlimited host.
# Two guards here, BOTH load-bearing — each was added after the other proved
# insufficient against a real deleted/corrupted helper:
#   1. `[ -r ... ] &&`  — a FAILED `.` (source) is fatal in a non-interactive
#      POSIX shell, so the shell exits before any `||` fallback can run.
#   2. the SUBSHELL `( ... )` — guard 1 covers a MISSING file, but a file that
#      exists and is syntactically BROKEN dies at parse time, killing the whole
#      shell anyway. Running it in a subshell contains that death so the `||`
#      still fires in the parent.
# Without both, this expands to the EMPTY string, which silently removes the
# -p/-parallel caps and unsets GOMAXPROCS — strictly worse than the bug being
# fixed. Verified against three cases: helper present (9), helper deleted (64),
# helper corrupted (64). Re-run all three if you touch this line.
NPROC := $(shell ( [ -r "$(CURDIR)/../scripts/lib/cgroup_cpu_quota.sh" ] && . "$(CURDIR)/../scripts/lib/cgroup_cpu_quota.sh" && cgroup_cpu_quota ) 2>/dev/null || nproc 2>/dev/null || echo 4)
GO_TEST_P ?= $(NPROC)
GO_TEST_PARALLEL ?= $(NPROC)
# Bind GOMAXPROCS for every recipe in this Makefile unless the caller set one.
# Without this, `go build`/`go test` each spawn NumCPU()-way parallelism
# regardless of the -p/-parallel caps above, which govern only test scheduling.
export GOMAXPROCS ?= $(NPROC)
# Composite flag block reused by the parallelism-tuned test targets below.
GO_TEST_PARFLAGS := -p $(GO_TEST_P) -parallel $(GO_TEST_PARALLEL)

# Directories
CMD_DIR=cmd
INTERNAL_DIR=internal
PKG_DIR=pkg
ASSETS_DIR=assets
SCRIPTS_DIR=scripts

all: build

# ============================================
# PROFILE-GUIDED OPTIMIZATION (PGO) — speed-programme Phase 4 task P4-T01
# ============================================
#
# Go applies PGO automatically (`-pgo=auto`, the toolchain default since Go
# 1.21) whenever a `default.pgo` file sits next to the package's `main.go`.
# This repo commits a representative production profile at:
#     cmd/cli/default.pgo      — picked up by every `cmd/cli` build
#     cmd/server/default.pgo   — picked up by every `cmd/server` build
# Therefore `make build` below, `go build ./cmd/...`, and the container
# builds are ALL PGO-optimised with no extra flags — the profile is a
# committed build INPUT, not a regenerable artefact.
#
# PGO only changes code generation; it never changes behaviour. The PGO
# binary passes the full existing test suite identically.
#
# Build explicitly without PGO (e.g. for a PGO vs non-PGO comparison):
#     go build -pgo=off ./cmd/cli/ ./cmd/server/
#
# Refresh the profile when the hot paths shift materially (new release,
# major feature, profile gone stale vs current code):
#     make pgo-refresh
# which merges the Phase-0 baseline CPU profiles
# (docs/research/speed/baseline/pprof/S{1..4}-cpu.pprof) with freshly
# captured repo-map + filesystem benchmark profiles into one merged
# default.pgo and copies it to both cmd/ directories.

.PHONY: pgo-refresh
pgo-refresh:
	@echo "🔧 Refreshing PGO profile (cmd/cli/default.pgo + cmd/server/default.pgo)..."
	@bash scripts/pgo-refresh.sh

# Build the application
# (PGO-optimised automatically — cmd/server/default.pgo is picked up by the
#  toolchain; see the PGO section above.)
build: logo-assets
	@echo "🚀 Building $(BINARY_NAME)..."
	$(GO_BUILD) -ldflags="-X main.version=$(VERSION) -X main.buildTime=$(BUILD_TIME) -X main.gitCommit=$(GIT_COMMIT)" -o bin/$(BINARY_NAME) ./$(CMD_DIR)/server
	@echo "✅ Build complete: bin/$(BINARY_NAME) (PGO-optimised via cmd/server/default.pgo)"

# Run tests
# P5-T01: -p/-parallel tuned from host CPU count; NO -count=1 here so the Go
# build cache short-circuits unchanged packages (hermetic unit suite — safe to
# cache). Same tests, same rigor — only scheduling/caching changed.
test:
	@echo "🧪 Running tests (-p $(GO_TEST_P) -parallel $(GO_TEST_PARALLEL), build cache enabled)..."
	$(GO_TEST) -v $(GO_TEST_PARFLAGS) ./...
	@echo "✅ Tests complete"

# §11.4.85 Stress + Chaos suites — real components, -race, captured evidence in
# qa-results/<run-id>/ (latency.json / concurrency_report.json / recovery_trace.*)
stress-chaos: ## Run §11.4.85 stress + chaos suites (real components, -race, captured evidence)
	@echo "🌪️  Running §11.4.85 stress + chaos suites (under -race)..."
	$(GO_TEST) -race -run 'Stress|Chaos' ./internal/worker/... ./internal/llm/... ./internal/task/... ./internal/session/... ./internal/event/... ./internal/memory/... ./internal/context/... ./internal/rules/... ./internal/repomap/... ./internal/discovery/... ./internal/tools/... ./internal/workflow/... ./internal/hooks/... ./internal/agent/... ./internal/monitoring/... ./internal/commands/... ./internal/mcp/... ./internal/notification/... ./internal/performance/... ./internal/security/... ./internal/providers/... ./internal/editor/... ./internal/template/... ./internal/cognee/... ./internal/focus/... ./internal/config/... ./internal/persistence/... ./internal/auth/... ./internal/deployment/... ./internal/project/... ./tests/stresschaos/...
	@echo "✅ Stress + chaos suites complete (evidence under qa-results/)"

# §1.1 paired-mutation meta-tests — prove the stress/chaos harness itself cannot
# bluff (planted deadlock/leak/error-rate/below-floor/chaos-panic must be detected)
stress-chaos-meta: ## Run §1.1 paired-mutation meta-tests for the stress/chaos harness
	@echo "🔬 Running stress/chaos harness paired-mutation meta-tests..."
	$(GO_TEST) -race -run 'TestMeta' -v ./tests/stresschaos/...
	@echo "✅ Harness meta-tests complete"

# §11.4.85 stress + chaos against REAL infrastructure (integration-tagged, no mocks
# per CONST-050). Requires real Postgres + Redis reachable. The new-pool tests read
# HELIX_TEST_DB_* (default localhost:5432 helix/helix/helix_test); the legacy
# pool/CRUD tests target localhost:5433 helix_test/test_password_secure_123. Redis
# defaults to localhost:6379. Bring infra up first (e.g. podman run postgres/redis)
# or `make test-infra-up`. Honest SKIP-OK when infra is unreachable (§11.4.3).
stress-chaos-infra: ## Run §11.4.85 stress+chaos against real PG+Redis (integration-tagged, -race)
	@echo "🌪️  Running §11.4.85 INFRA stress + chaos suites (real PG+Redis, -tags=integration, -race)..."
	$(GO_TEST) -tags=integration -race -run 'Stress|Chaos' ./internal/redis/... ./internal/database/... ./internal/server/... ./internal/llm/...
	@echo "✅ Infra stress + chaos suites complete (evidence under qa-results/)"

# Comprehensive test suite
test-all: test test-coverage test-benchmark test-docs
	@echo "🎉 All tests completed successfully!"

# Run coverage analysis
# P5-T01: -p/-parallel tuned. -race needs a fresh run so the cache cannot
# short-circuit it; -count=1 is intentionally NOT added (-race already forces
# a distinct race-instrumented build, and coverage re-runs regardless), but
# parallelism is capped to keep the race detector's memory overhead bounded.
test-coverage:
	@echo "📊 Running coverage analysis..."
	@if [ -f scripts/coverage.sh ]; then \
		./scripts/coverage.sh; \
	else \
		echo "⚠️ Coverage script not found, running basic coverage..."; \
		$(GO_TEST) -v -race $(GO_TEST_PARFLAGS) -coverprofile=coverage.out -covermode=atomic ./...; \
		go tool cover -func=coverage.out | tail -1; \
	fi

# Run benchmarks
test-benchmark:
	@echo "⚡ Running benchmarks..."
	@if [ -f scripts/benchmark.sh ]; then \
		./scripts/benchmark.sh; \
	else \
		echo "⚠️ Benchmark script not found, running basic benchmarks..."; \
		$(GO_TEST) -bench=. -benchmem ./... || true; \
	fi

# Run documentation tests
test-docs:
	@echo "📚 Testing documentation..."
	@if [ -f scripts/docs-validate.sh ]; then \
		./scripts/docs-validate.sh; \
	else \
		echo "⚠️ Documentation validation script not found, skipping..."; \
	fi

# ============================================
# FULL TEST INFRASTRUCTURE (Zero Skipped Tests)
# ============================================

.PHONY: test-infra-up test-infra-down test-infra-status test-full test-unit-full test-integration-full test-integration-tag test-e2e-full test-security-full test-loadtest

# Compose runner auto-detection (Rule 4 / §11.4.76): prefer `docker compose`
# when docker is on PATH, otherwise fall back to `podman compose` (or the
# standalone `podman-compose` binary if that is the available subcommand).
# Keeps behaviour identical on docker hosts; podman-only hosts (e.g. macOS dev
# boxes per CLAUDE.md Rule 4) no longer fail with `docker: command not found`.
COMPOSE := $(shell \
	if command -v docker >/dev/null 2>&1; then echo "docker compose"; \
	elif podman compose version >/dev/null 2>&1; then echo "podman compose"; \
	elif command -v podman-compose >/dev/null 2>&1; then echo "podman-compose"; \
	elif command -v podman >/dev/null 2>&1; then echo "podman compose"; \
	else echo "docker compose"; fi)

# Start full test infrastructure with all dependencies
# HXC-168: docker-compose.full-test.yml credential values are env-sourced
# (`${HELIX_DATABASE_PASSWORD:?...}`) — .env.full-test MUST be exported into
# this recipe's shell before any $(COMPOSE) invocation, or compose refuses
# with a "variable is not set" error (Make prerequisites run in a SEPARATE
# shell, so sourcing it once in test-full's recipe does not help this target).
test-infra-up:
	@echo "🚀 Starting full test infrastructure (compose runner: $(COMPOSE))..."
	@set -a && . ./.env.full-test && set +a && \
	$(COMPOSE) -f docker-compose.full-test.yml up -d
	@echo "⏳ Waiting for services to be healthy..."
	@sleep 30
	@echo "📦 Pulling Ollama test model..."
	@set -a && . ./.env.full-test && set +a && \
	$(COMPOSE) -f docker-compose.full-test.yml exec -T ollama ollama pull llama2:7b 2>/dev/null || echo "⚠️ Model pull skipped"
	@echo "✅ Test infrastructure ready!"
	@echo ""
	@echo "To run all tests: make test-full"

# Stop full test infrastructure
test-infra-down:
	@echo "🛑 Stopping full test infrastructure (compose runner: $(COMPOSE))..."
	@set -a && . ./.env.full-test && set +a && \
	$(COMPOSE) -f docker-compose.full-test.yml down -v
	@echo "✅ Test infrastructure stopped"

# Check test infrastructure status
test-infra-status:
	@echo "📊 Test infrastructure status (compose runner: $(COMPOSE)):"
	@set -a && . ./.env.full-test && set +a && \
	$(COMPOSE) -f docker-compose.full-test.yml ps

# Run ALL tests with full infrastructure (ZERO skipped tests)
# P5-T01: -p/-parallel tuned for faster wall-time. -count=1 is KEPT — these
# targets exercise real PostgreSQL/Redis/Ollama; a cached PASS would be a
# CONST-035 anti-bluff false-success (green line, no fresh runtime evidence).
test-full: test-infra-up
	@echo "🧪 Running ALL tests (no skips, -p $(GO_TEST_P) -parallel $(GO_TEST_PARALLEL))..."
	@set -a && . ./.env.full-test && set +a && \
	$(GO_TEST) -v -count=1 $(GO_TEST_PARFLAGS) -timeout=30m ./... && \
	$(GO_TEST) -v -count=1 $(GO_TEST_PARFLAGS) -tags=integration -timeout=30m ./... && \
	echo "🎉 All tests passed with ZERO skips!"

# Run unit tests with full infrastructure
test-unit-full: test-infra-up
	@echo "🧪 Running unit tests with full infrastructure (-p $(GO_TEST_P) -parallel $(GO_TEST_PARALLEL))..."
	@set -a && . ./.env.full-test && set +a && \
	$(GO_TEST) -v -count=1 $(GO_TEST_PARFLAGS) -timeout=15m ./internal/...

# Run integration tests with full infrastructure
test-integration-full: test-infra-up
	@echo "🧪 Running integration tests (-p $(GO_TEST_P) -parallel $(GO_TEST_PARALLEL))..."
	@set -a && . ./.env.full-test && set +a && \
	$(GO_TEST) -v -count=1 $(GO_TEST_PARFLAGS) -tags=integration -timeout=20m ./...

# Lightweight standalone run of the `-tags=integration` suite under
# tests/integration/**, WITHOUT bringing up docker-compose.full-test.yml.
# (F-D1-06 / §11.4.169: these tests are gated by `//go:build integration` and
# a bare `go test` / `make test` never compiles or runs them, so their
# pass/fail signal was invisible in routine checks. `-tags=nogui` is required
# so `applications/desktop/` resolves its no-GUI variant instead of pulling
# in Fyne's X11 dependency chain — see verify-compile above for the same
# rationale.) This is intentionally distinct from `test-integration-full`
# (which requires the real Postgres/Redis/Ollama docker-compose stack per
# CONST-050 no-mocks-beyond-unit-tests): individual test files may still
# SKIP-with-reason at runtime when a specific external dependency they need
# is unreachable (§11.4.3), but the suite itself now compiles and runs on a
# bare host with zero infra bring-up, making its PASS/FAIL/SKIP status
# visible in any ordinary dev/CI-less workflow.
test-integration-tag:
	@echo "🧪 Running -tags=integration suite standalone (no infra bring-up, -tags=nogui,integration)..."
	$(GO_TEST) -v -tags=nogui,integration -count=1 ./tests/integration/...
	@echo "✅ Standalone integration-tagged tests complete"

# Run E2E tests with full infrastructure
test-e2e-full: test-infra-up
	@echo "🧪 Running E2E challenge tests..."
	@set -a && . ./.env.full-test && set +a && \
	cd tests/e2e/challenges && $(GO) run cmd/runner/main.go -all

# Run security tests with full infrastructure
test-security-full: test-infra-up
	@echo "🔒 Running security tests (-p $(GO_TEST_P) -parallel $(GO_TEST_PARALLEL))..."
	@set -a && . ./.env.full-test && set +a && \
	$(GO_TEST) -v -count=1 $(GO_TEST_PARFLAGS) -timeout=20m ./tests/security/...

# Run load tests with full infrastructure
test-load-full: test-infra-up
	@echo "⚡ Running load tests (-p $(GO_TEST_P) -parallel $(GO_TEST_PARALLEL))..."
	@set -a && . ./.env.full-test && set +a && \
	$(GO_TEST) -v -count=1 $(GO_TEST_PARFLAGS) -timeout=30m ./test/load/...

# Load-generating suites, tagged OUT of the default `go test ./...` unit pass.
#
# WHY THEY ARE SEPARATED (measured, not assumed): a full-suite run was measured
# driving the host ephemeral port range to 100% occupancy (28,221 of 28,232
# ports) held by ~29,600 TIME-WAIT sockets for 60-90s at a time. Any package
# binding `:0` / `127.0.0.1:0` inside that window fails EADDRINUSE, which is how
# ~7 unrelated unit tests began failing in the suite while passing in isolation.
# Kernel arithmetic: 28,232 ports / 60s TIME_WAIT = ~470 close-cycles/sec before
# saturation, and `go test` defaults -p to NPROC, so the unit pass and these
# suites were competing for one host-wide budget.
#
# -p 1 IS LOAD-BEARING: it runs these packages SERIALLY so they cannot flood
# each other either. Do not "speed this up" by raising it — that reintroduces
# the exact contention this target exists to remove.
#
# Evidence: docs/qa/2026-09-07-ephemeral-port-exhaustion/ANALYSIS.md
test-loadtest:
	@echo "⚡ Load-generating suites (ddos/performance/scaling/stresschaos), SERIAL (-p 1)..."
	$(GO_TEST) -v -count=1 -p 1 -tags=loadtest -timeout=30m \
		./tests/ddos/... ./tests/performance/... ./tests/scaling/... ./tests/stresschaos/...
	@echo "✅ Load-generating suites complete"

# Run all test types in sequence
test-complete: test-unit-full test-integration-full test-e2e-full test-security-full test-load-full test-loadtest test-benchmark test-verifier-challenges
	@echo ""
	@echo "🎉 ============================================="
	@echo "🎉 ALL TESTS COMPLETED SUCCESSFULLY!"
	@echo "🎉 ============================================="
	@echo ""

# ============================================
# LLMsVerifier Test Targets
# ============================================

.PHONY: test-verifier-unit test-verifier-integration test-verifier-challenges test-verifier-hardcode

# Run verifier unit tests (mocks OK for client/cache/health)
# P5-T01: -p/-parallel tuned. -count=1 KEPT — verifier tests assert
# real-time model status (CONST-038) and must not serve a cached PASS.
test-verifier-unit:
	@echo "🔍 Running verifier unit tests (-p $(GO_TEST_P) -parallel $(GO_TEST_PARALLEL))..."
	$(GO_TEST) -v -count=1 $(GO_TEST_PARFLAGS) ./internal/verifier/...
	@echo "✅ Verifier unit tests complete"

# Run verifier integration tests (NO MOCKS — requires verifier service)
test-verifier-integration:
	@echo "🔍 Running verifier integration tests (-p $(GO_TEST_P) -parallel $(GO_TEST_PARALLEL))..."
	$(GO_TEST) -v -count=1 $(GO_TEST_PARFLAGS) -tags=integration ./internal/verifier/... || echo "⚠️  Integration tests require running verifier at http://localhost:8081"
	@echo "✅ Verifier integration tests complete"

# Run verifier challenge scripts
test-verifier-challenges: test-verifier-hardcode test-verifier-capability
	@echo "✅ All verifier challenges passed"

# Challenge: Verify no hardcoded model lists exist in production code
test-verifier-hardcode:
	@echo "🔍 Checking for hardcoded model lists (CONST-037)..."
	@bash scripts/anti_bluff/verifier_hardcode_check.sh
	@echo "✅ No hardcoded model lists found"

# Challenge: Verify model capabilities come from verifier, not hardcoded
test-verifier-capability:
	@echo "🔍 Checking for hardcoded capability flags (CONST-041)..."
	@bash scripts/anti_bluff/verifier_capability_check.sh || echo "⚠️  Capability check requires verifier service"
	@echo "✅ Capability checks complete"

# Generate coverage report with full infrastructure
coverage-full: test-infra-up
	@echo "📊 Generating comprehensive coverage report (-p $(GO_TEST_P) -parallel $(GO_TEST_PARALLEL))..."
	@set -a && . ./.env.full-test && set +a && \
	$(GO_TEST) $(GO_TEST_PARFLAGS) -coverprofile=coverage.out -covermode=atomic -timeout=30m ./... && \
	go tool cover -func=coverage.out && \
	go tool cover -html=coverage.out -o coverage.html
	@echo "✅ Coverage report: coverage.html"

# Quick check that code compiles.
#
# Uses -tags=nogui so applications/desktop/ picks up its main_nogui.go
# instead of the Fyne-GUI main.go. Without that tag, `go build ./...`
# fails in headless / non-X11 environments because of the transitive
# go-gl/glfw → X11/Xcursor C dependency — even though the codebase
# already has a no-GUI variant. CONST-035: the canonical compile-gate
# must not silently depend on an optional system library when a
# no-dep variant exists in the tree.
verify-compile:
	@echo "🔍 Verifying code compilation (nogui — no X11 system libs required)..."
	@$(GO) build -tags=nogui ./... && echo "✅ All packages compile successfully"
	@$(MAKE) --no-print-directory verify-compile-tests

# Companion guard for verify-compile: `go build` NEVER compiles _test.go files,
# so the target above cannot see a test file that fails to build under
# -tags=nogui. That class is real: applications/{desktop,aurora_os,harmony_os}
# carry GUI-only tests exercising symbols declared in !nogui-tagged sources, so
# any such test file missing its `//go:build !nogui` constraint breaks every
# nogui-tagged invocation (verify-compile's own `./...`, test-integration-tag,
# desktop-nogui, …) while `go build` stays green.
#
# `-run='^$$'` compiles every test binary in the module and runs zero tests, so
# this is a compile gate (seconds), not a test run. Build failures go to stderr
# and are surfaced; the per-package "ok … [no tests to run]" chatter is dropped.
.PHONY: verify-compile-tests
verify-compile-tests: ## Compile-check ALL test code under -tags=nogui (go build skips _test.go)
	@echo "🔍 Verifying TEST code compiles under -tags=nogui (go build never sees _test.go)..."
	@$(GO) test -tags=nogui -run='^$$' -count=1 ./... >/dev/null && \
		echo "✅ All test packages compile under -tags=nogui"

# Clean build artifacts
clean:
	@echo "🧹 Cleaning build artifacts..."
	rm -rf bin/
	rm -rf dist/
	rm -rf coverage.out
	rm -rf coverage.html
	rm -rf coverage-reports/
	rm -rf benchmark-reports/
	rm -rf doc-reports/
	@echo "✅ Clean complete"

# Clean all generated files including reports
clean-all: clean
	@echo "🧹 Cleaning all generated files..."
	rm -rf *.out
	rm -rf *.html
	rm -rf -reports/
	@echo "✅ All clean complete"

# Generate logo assets
logo-assets:
	@echo "🎨 Generating logo assets..."
	cd $(SCRIPTS_DIR)/logo && $(GO) run generate_assets.go
	@echo "✅ Logo assets generated"

# Setup dependencies
setup-deps:
	@echo "📦 Setting up dependencies..."
	$(GO_MOD) download
	$(GO_MOD) tidy
	@echo "✅ Dependencies setup complete"

# Format code
fmt:
	@echo "🎨 Formatting code..."
	$(GOFMT) ./...
	@echo "✅ Format complete"

# Lint code
lint:
	@echo "🔍 Linting code..."
	$(GOLINT) ./...
	@echo "✅ Lint complete"

# Run the application
dev: build
	@echo "🚀 Starting development server..."
	./bin/$(BINARY_NAME) --config config/dev/config.yaml

# Build for production
prod: clean build
	@echo "🏗️ Building for production..."
	GOOS=linux GOARCH=amd64 $(GO_BUILD) -ldflags="-s -w" -o bin/$(BINARY_NAME)-linux ./$(CMD_DIR)/server
	GOOS=darwin GOARCH=amd64 $(GO_BUILD) -ldflags="-s -w" -o bin/$(BINARY_NAME)-macos ./$(CMD_DIR)/server
	GOOS=windows GOARCH=amd64 $(GO_BUILD) -ldflags="-s -w" -o bin/$(BINARY_NAME)-windows.exe ./$(CMD_DIR)/server
	@echo "✅ Production builds complete"

# Install dependencies for logo processing
logo-deps:
	@echo "📦 Installing logo processing dependencies..."
	$(GO) get github.com/nfnt/resize
	@echo "✅ Logo dependencies installed"

# Generate all assets (logo + documentation)
assets: logo-assets sync-manual
	@echo "✅ All assets generated"

# Sync user manual to website
sync-manual:
	@echo "📚 Syncing user manual to website..."
	$(SCRIPTS_DIR)/sync-manual.sh
	@echo "✅ Manual synced"

# Convert manual Markdown to HTML
manual-html:
	@echo "📄 Converting manual to HTML..."
	@if command -v pandoc > /dev/null; then \
		mkdir -p website/manual; \
		pandoc docs/USER_MANUAL.md -f markdown -t html5 --standalone --toc --toc-depth=3 \
			--css=style.css --metadata title="HelixCode User Manual" \
			-o website/manual/USER_MANUAL.html; \
		echo "✅ HTML manual generated"; \
	else \
		echo "⚠️  pandoc not installed - skipping HTML conversion"; \
		echo "   Install with: brew install pandoc (macOS) or apt-get install pandoc (Linux)"; \
	fi

# Build documentation
docs: manual-html sync-manual
	@echo "📚 Building documentation..."
	@echo "✅ Documentation built"

# Release build with all assets and documentation
release: clean logo-assets docs build test
	@echo "🚀 Building release..."
	@echo "✅ Release build complete"

# Debian packaging target
.PHONY: deb
deb: build
	@echo "📦 Building Debian package..."
	mkdir -p packaging/debian/helixcode/DEBIAN
	mkdir -p packaging/debian/helixcode/usr/bin
	mkdir -p packaging/debian/helixcode/etc/helixcode
	mkdir -p packaging/debian/helixcode/lib/systemd/system
	mkdir -p packaging/debian/helixcode/usr/share/doc/helixcode
	cp bin/helixcode packaging/debian/helixcode/usr/bin/
	cp packaging/debian/control packaging/debian/helixcode/DEBIAN/
	cp packaging/debian/postinst packaging/debian/helixcode/DEBIAN/
	cp packaging/debian/prerm packaging/debian/helixcode/DEBIAN/
	cp packaging/debian/helixcode.service packaging/debian/helixcode/lib/systemd/system/
	cp config/config.yaml packaging/debian/helixcode/etc/helixcode/config.yaml.example
	cp README.md packaging/debian/helixcode/usr/share/doc/helixcode/
	chmod 755 packaging/debian/helixcode/DEBIAN/postinst
	chmod 755 packaging/debian/helixcode/DEBIAN/prerm
	dpkg-deb --build packaging/debian/helixcode
	mv packaging/debian/helixcode.deb packaging/helixcode_$(VERSION)_amd64.deb
	@echo "✅ Debian package: packaging/helixcode_$(VERSION)_amd64.deb"

# RPM packaging target
.PHONY: rpm
rpm: build
	@echo "📦 Building RPM package..."
	mkdir -p packaging/rpmbuild/{BUILD,RPMS,SOURCES,SPECS,SRPMS}
	cp packaging/rpm/helixcode.spec packaging/rpmbuild/SPECS/
	tar czf packaging/rpmbuild/SOURCES/helixcode-$(VERSION).tar.gz \
		--transform="s/^/helixcode-$(VERSION)\//" \
		bin/helixcode config/config.yaml README.md
	rpmbuild --define "_topdir $(PWD)/packaging/rpmbuild" \
		-bb packaging/rpmbuild/SPECS/helixcode.spec
	cp packaging/rpmbuild/RPMS/x86_64/helixcode-*.rpm packaging/
	@echo "✅ RPM package: packaging/helixcode-$(VERSION)-1.x86_64.rpm"

# Homebrew packaging target
.PHONY: homebrew
homebrew: build
	@echo "📦 Building Homebrew package..."
	mkdir -p packaging/homebrew
	tar czf packaging/helixcode-$(VERSION).darwin.amd64.tar.gz \
		-C bin helixcode \
		-C ../config config.yaml
	shasum -a 256 packaging/helixcode-$(VERSION).darwin.amd64.tar.gz \
		> packaging/helixcode-$(VERSION).darwin.amd64.tar.gz.sha256
	@echo "✅ Homebrew archive: packaging/helixcode-$(VERSION).darwin.amd64.tar.gz"
	@echo "✅ Formula: packaging/homebrew/helixcode.rb"

# Windows installer target
.PHONY: windows
windows: build
	@echo "📦 Building Windows installer..."
	mkdir -p packaging/windows
	GOOS=windows GOARCH=amd64 $(GO_BUILD) -ldflags="-s -w" -o bin/helixcode.exe ./$(CMD_DIR)/server
	zip -j packaging/helixcode-$(VERSION).windows.amd64.zip bin/helixcode.exe config/config.yaml
	@echo "✅ Windows package: packaging/helixcode-$(VERSION).windows.amd64.zip"

.PHONY: installers
installers: deb rpm homebrew windows
	@echo "📦 All installers built in packaging/"

help:
	@echo "HelixCode Build System"
	@echo ""
	@echo "Usage:"
	@echo "  make build           - Build the application"
	@echo "  make test            - Run tests (build cache + -p/-parallel tuned)"
	@echo "  make test-integration-tag - Run -tags=integration suite standalone, no infra bring-up"
	@echo "  make test-loadtest        - Run load suites (ddos/perf/scaling/stresschaos) SERIALLY"
	@echo "  make clean           - Clean build artifacts"
	@echo ""
	@echo "Test parallelism tuning (P5-T01 — override on the command line):"
	@echo "  GO_TEST_P            - -p: test binaries run concurrently (default: nproc=$(NPROC))"
	@echo "  GO_TEST_PARALLEL     - -parallel: t.Parallel() tests per binary (default: nproc=$(NPROC))"
	@echo "  e.g.  make test GO_TEST_P=4 GO_TEST_PARALLEL=4"
	@echo "  make logo-assets     - Generate logo assets and themes"
	@echo "  make setup-deps      - Setup dependencies"
	@echo "  make fmt             - Format code"
	@echo "  make lint            - Lint code"
	@echo "  make dev             - Run development server"
	@echo "  make prod            - Build for production"
	@echo "  make assets          - Generate all assets"
	@echo ""
	@echo "Containerized Builds (NO host deps required):"
	@echo "  make container-builder-image  - Build the builder container"
	@echo "  make container-build          - Build inside container"
	@echo "  make container-test           - Run tests inside container"
	@echo "  make container-lint           - Run lint inside container"
	@echo "  make container-shell          - Interactive builder shell"
	@echo "  make container-dev-up         - Start containerized dev env"
	@echo "  make container-dev-down       - Stop containerized dev env"
	@echo "  make container-release        - Full release in container"
	@echo ""
	@echo "Documentation:"
	@echo "  make sync-manual     - Sync user manual to website"
	@echo "  make manual-html     - Convert manual Markdown to HTML"
	@echo "  make docs            - Build all documentation"
	@echo "  make release         - Full release build (code + docs + tests)"
	@echo ""
	@echo "Desktop:"
	@echo "  make desktop         - Build desktop GUI application"
	@echo "  make desktop-nogui   - Build desktop CLI (no GUI dependencies)"
	@echo "  make desktop-linux   - Build desktop for Linux (GUI + CLI)"
	@echo "  make desktop-macos   - Build desktop for macOS (Intel + ARM)"
	@echo "  make desktop-windows - Build desktop for Windows (GUI + CLI)"
	@echo "  make desktop-all     - Build desktop for all platforms"
	@echo ""
	@echo "Mobile:"
	@echo "  make mobile-init     - Initialize gomobile"
	@echo "  make mobile-ios      - Build iOS framework"
	@echo "  make mobile-android  - Build Android AAR"
	@echo "  make mobile          - Build all mobile bindings"
	@echo ""
	@echo "Platform-Specific:"
	@echo "  make aurora-os       - Build Aurora OS client"
	@echo "  make harmony-os      - Build Harmony OS client"
	@echo "  make aurora-harmony  - Build Aurora and Harmony OS clients"
	@echo ""
	@echo "  make help            - Show this help"

# Desktop build targets
.PHONY: desktop desktop-nogui desktop-all desktop-linux desktop-macos desktop-windows

# Build desktop GUI application (requires GUI dependencies)
desktop:
	@echo "🖥️ Building desktop GUI application..."
	$(GO_BUILD) -ldflags="-X main.version=$(VERSION) -X main.buildTime=$(BUILD_TIME) -X main.gitCommit=$(GIT_COMMIT)" -o bin/helix-desktop ./applications/desktop
	@echo "✅ Desktop GUI built: bin/helix-desktop"

# Build desktop CLI application (no GUI dependencies required)
desktop-nogui:
	@echo "🖥️ Building desktop CLI application (no GUI)..."
	$(GO_BUILD) -tags nogui -ldflags="-X main.version=$(VERSION) -X main.buildTime=$(BUILD_TIME) -X main.gitCommit=$(GIT_COMMIT)" -o bin/helix-desktop-cli ./applications/desktop
	@echo "✅ Desktop CLI built: bin/helix-desktop-cli"

# Build desktop for Linux
desktop-linux:
	@echo "🐧 Building desktop for Linux..."
	GOOS=linux GOARCH=amd64 $(GO_BUILD) -ldflags="-s -w -X main.version=$(VERSION)" -o bin/helix-desktop-linux ./applications/desktop
	GOOS=linux GOARCH=amd64 $(GO_BUILD) -tags nogui -ldflags="-s -w -X main.version=$(VERSION)" -o bin/helix-desktop-cli-linux ./applications/desktop
	@echo "✅ Linux desktop builds complete"

# Build desktop for macOS
desktop-macos:
	@echo "🍎 Building desktop for macOS..."
	GOOS=darwin GOARCH=amd64 $(GO_BUILD) -ldflags="-s -w -X main.version=$(VERSION)" -o bin/helix-desktop-macos ./applications/desktop
	GOOS=darwin GOARCH=arm64 $(GO_BUILD) -ldflags="-s -w -X main.version=$(VERSION)" -o bin/helix-desktop-macos-arm64 ./applications/desktop
	@echo "✅ macOS desktop builds complete"

# Build desktop for Windows
desktop-windows:
	@echo "🪟 Building desktop for Windows..."
	GOOS=windows GOARCH=amd64 $(GO_BUILD) -ldflags="-s -w -X main.version=$(VERSION)" -o bin/helix-desktop.exe ./applications/desktop
	GOOS=windows GOARCH=amd64 $(GO_BUILD) -tags nogui -ldflags="-s -w -X main.version=$(VERSION)" -o bin/helix-desktop-cli.exe ./applications/desktop
	@echo "✅ Windows desktop builds complete"

# Build desktop for all platforms
desktop-all: desktop-linux desktop-macos desktop-windows
	@echo "🖥️ All desktop platform builds complete"

# Mobile build targets
mobile-init:
	@echo "📱 Initializing gomobile..."
	$(GO) run golang.org/x/mobile/cmd/gomobile init

mobile-ios: mobile-init
	@echo "🍎 Building iOS framework..."
	gomobile bind -target=ios -o HelixCore.xcframework ./shared/mobile-core
	@echo "✅ iOS framework built: HelixCore.xcframework"

mobile-android: mobile-init
	@echo "🤖 Building Android AAR..."
	gomobile bind -target=android -o mobile.aar ./shared/mobile-core
	@echo "✅ Android AAR built: mobile.aar"

mobile: mobile-ios mobile-android
	@echo "📱 All mobile bindings built"

# Aurora OS and Harmony OS build targets
aurora-os:
	@echo "🌟 Building Aurora OS client..."
	$(GO_BUILD) -ldflags="-X main.version=$(VERSION) -X main.buildTime=$(BUILD_TIME) -X main.gitCommit=$(GIT_COMMIT)" -o bin/aurora-os ./applications/aurora_os
	@echo "✅ Aurora OS client built: bin/aurora-os"

harmony-os:
	@echo "🔶 Building Harmony OS client..."
	$(GO_BUILD) -ldflags="-X main.version=$(VERSION) -X main.buildTime=$(BUILD_TIME) -X main.gitCommit=$(GIT_COMMIT)" -o bin/harmony-os ./applications/harmony_os
	@echo "✅ Harmony OS client built: bin/harmony-os"

aurora-harmony: aurora-os harmony-os
	@echo "🌟🔶 Aurora OS and Harmony OS clients built"

# ============================================================
# CONTAINERIZED BUILDS — No host dependencies required
# Authority: CONST-035 — All builds MUST be containerized.
# ============================================================

COMPOSE_BUILDER := $(COMPOSE) -f docker-compose.builder.yml
BUILDER_SERVICE := builder

.PHONY: container-builder-image container-build container-test container-lint container-shell container-dev-up container-dev-down container-clean

# Build the builder container image
container-builder-image:
	@echo "🐳 Building HelixCode builder image..."
	$(COMPOSE_BUILDER) build $(BUILDER_SERVICE)
	@echo "✅ Builder image ready: helixcode-builder:latest"

# Build the application inside the builder container
container-build: container-builder-image
	@echo "🔨 Building HelixCode inside container..."
	$(COMPOSE_BUILDER) run --rm $(BUILDER_SERVICE) -c "make build"
	@echo "✅ Container build complete"

# Run tests inside the builder container
container-test: container-builder-image
	@echo "🧪 Running tests inside container..."
	$(COMPOSE_BUILDER) run --rm $(BUILDER_SERVICE) -c "make test"
	@echo "✅ Container tests complete"

# Run lint inside the builder container
container-lint: container-builder-image
	@echo "🔍 Running lint inside container..."
	$(COMPOSE_BUILDER) run --rm $(BUILDER_SERVICE) -c "make lint"
	@echo "✅ Container lint complete"

# Run full test suite inside the builder container
# P5-T01: delegates to `make test` inside the container so the container run
# inherits the same -p/-parallel tuning + build-cache behaviour as the host.
container-test-full: container-builder-image
	@echo "🧪 Running full test suite inside container..."
	$(COMPOSE_BUILDER) run --rm $(BUILDER_SERVICE) -c "make test"
	@echo "✅ Container full tests complete"

# Drop into an interactive shell inside the builder container
container-shell: container-builder-image
	@echo "🐚 Starting interactive builder shell..."
	$(COMPOSE_BUILDER) run --rm $(BUILDER_SERVICE)

# Start builder + dependencies (postgres, redis) for dev
container-dev-up:
	@echo "🚀 Starting containerized dev environment..."
	$(COMPOSE_BUILDER) up -d postgres redis
	@echo "✅ Dev environment ready (postgres + redis)"
	@echo "   Run 'make container-shell' to enter builder"

# Stop builder dev environment
container-dev-down:
	@echo "🛑 Stopping containerized dev environment..."
	$(COMPOSE_BUILDER) down
	@echo "✅ Dev environment stopped"

# Clean builder containers and volumes
container-clean:
	@echo "🧹 Cleaning builder containers and volumes..."
	$(COMPOSE_BUILDER) down -v --remove-orphans
	@echo "✅ Builder environment cleaned"

# One-shot containerized build with all dependencies
container-release: container-clean container-builder-image
	@echo "🚀 Running containerized release build..."
	$(COMPOSE_BUILDER) run --rm $(BUILDER_SERVICE) -c "make clean && make build && make test"
	@echo "✅ Container release build complete"

# ============================================================
# Development tools setup
dev-setup: setup-deps logo-deps
	@echo "🔧 Development environment setup complete"
	@echo ""
	@echo "Next steps:"
	@echo "  1. Run 'make logo-assets' to generate logo assets"
	@echo "  2. Run 'make build' to build the application"
	@echo "  3. Run 'make dev' to start development server"
	@echo "  4. Run 'make mobile-ios' to build iOS framework"
	@echo "  5. Run 'make mobile-android' to build Android AAR"
	@echo "  6. Run 'make aurora-os' to build Aurora OS client"
	@echo "  7. Run 'make harmony-os' to build Harmony OS client"
# ---------------------------------------------------------------------------
# helix_qa integration targets
# ---------------------------------------------------------------------------

.PHONY: helixqa-build helixqa-test helixqa-challenge helixqa-bump-submodules

HELIXQA_DIR := ../HelixQA
HELIXQA_DEPS := ../Dependencies/HelixDevelopment

helixqa-build: ## Build helix_qa and all dependency modules
	@echo "Building HelixQA..."
	cd $(HELIXQA_DIR) && go build ./...
	@echo "Building DocProcessor..."
	cd $(HELIXQA_DEPS)/DocProcessor && go build ./...
	@echo "Building LLMOrchestrator..."
	cd $(HELIXQA_DEPS)/LLMOrchestrator && go build ./...
	@echo "Building LLMProvider..."
	cd $(HELIXQA_DEPS)/LLMProvider && go build ./...
	@echo "Building VisionEngine..."
	cd $(HELIXQA_DEPS)/VisionEngine && go build ./...
	@echo "HelixQA build complete."

helixqa-test: ## Run helix_qa test suite with race detector
	cd $(HELIXQA_DIR) && go test ./... -race -count=1 -p 2

helixqa-challenge: ## Run helix_qa challenge banks against local HelixCode stack
	cd $(HELIXQA_DIR) && go run ./cmd/helixqa run \
		--banks ./banks/full-qa-api,./banks/full-qa-web \
		--platform api,web \
		--browser-url http://localhost:8080 \
		--output ./qa-results/helixcode-$(shell date +%Y%m%d-%H%M%S) \
		--report markdown,html,json \
		--validate \
		--tickets

helixqa-bump-submodules: ## Bump all helix_qa dependency submodules with verification
	@echo "Fetching updates for helix_qa dependencies..."
	@for dep in DocProcessor LLMOrchestrator LLMProvider VisionEngine; do \
		path="$(HELIXQA_DEPS)/$$dep"; \
		echo "  Checking $$dep ($$path)..."; \
		git -C $$path fetch origin; \
		current=$$(git -C $$path rev-parse HEAD); \
		candidate=$$(git -C $$path rev-parse origin/main); \
		if [ "$$current" != "$$candidate" ]; then \
			echo "    Advance $$dep: $$(git -C $$path rev-parse --short $$current) -> $$(git -C $$path rev-parse --short $$candidate)"; \
			git -C $$path checkout $$candidate; \
		else \
			echo "    $$dep is already at latest."; \
		fi; \
	done
	@echo "Building with candidate commits..."
	$(MAKE) helixqa-build
	@echo "Testing with candidate commits..."
	$(MAKE) helixqa-test
	@echo "Bump verification complete. Stage changes with: git add $(HELIXQA_DEPS)"

# ---------------------------------------------------------------------------
# Security scanning targets (P0-T08.7)
# Delegates to scripts/security-scan.sh which loads credentials from .env.
# To run a live scan: set SONAR_TOKEN / SNYK_TOKEN in HelixCode/.env first.
# ---------------------------------------------------------------------------

.PHONY: security-scan security-scan-snyk security-scan-sonarqube security-scan-trivy \
        security-scan-gosec security-scan-grype security-scan-kics security-scan-semgrep \
        security-scan-all deps-scan secrets-scan scan-start-sonar scan-stop

security-scan: ## Run all open-source security scanners (gosec, trivy, grype, go-analysis)
	@./scripts/security-scan.sh all

security-scan-snyk: ## Run Snyk vulnerability scanner (requires SNYK_TOKEN in .env)
	@./scripts/security-scan.sh snyk

security-scan-sonarqube: ## Run SonarQube code analysis (requires SONAR_TOKEN in .env)
	@./scripts/security-scan.sh sonarqube

security-scan-trivy: ## Run Trivy vulnerability and secret scanner
	@./scripts/security-scan.sh trivy

security-scan-gosec: ## Run Gosec Go security checker
	@./scripts/security-scan.sh gosec

security-scan-grype: ## Run Grype container vulnerability scanner
	@./scripts/security-scan.sh grype

security-scan-kics: ## Run KICS IaC security scanner
	@./scripts/security-scan.sh kics

security-scan-semgrep: ## Run Semgrep SAST scanner
	@./scripts/security-scan.sh semgrep

security-scan-all: ## Run ALL scanners including SonarQube and Snyk (requires tokens in .env)
	@./scripts/security-scan.sh gosec
	@./scripts/security-scan.sh trivy
	@./scripts/security-scan.sh grype
	@./scripts/security-scan.sh semgrep
	@./scripts/security-scan.sh snyk
	@./scripts/security-scan.sh sonarqube

deps-scan: ## Scan Go module dependencies for known vulnerabilities (govulncheck / snyk)
	@if command -v govulncheck >/dev/null 2>&1; then \
		echo "Running govulncheck..."; \
		govulncheck ./...; \
	else \
		$(MAKE) security-scan-snyk; \
	fi

secrets-scan: ## Run scan-secrets.sh to verify no credentials are committed
	@if [ -f scripts/scan-secrets.sh ]; then \
		./scripts/scan-secrets.sh; \
	else \
		echo "Error: scripts/scan-secrets.sh not found"; exit 1; \
	fi

scan-start-sonar: ## Start SonarQube server (uses containers BootManager once wired, T08.7/4)
	@./scripts/security-scan.sh start-sonar

scan-stop: ## Stop SonarQube services
	@./scripts/security-scan.sh stop
