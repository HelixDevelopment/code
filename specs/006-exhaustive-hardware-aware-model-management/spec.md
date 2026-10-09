# Feature Specification: Exhaustive Hardware-Aware Model Management for HelixLLM/HelixAgent/Claude Toolkit

**Feature Branch**: `006-exhaustive-hardware-aware-model-management`

**Created**: 2026-09-12

**Status**: Draft

**Input**: Operator mandate (paraphrased — the full verbatim text was truncated in-line at 2000 chars by the tooling; the complete original is held in the originating session/ticket): local models exposed via HelixLLM (`helix_llm`) and HelixAgent (`helix_agent`) were selected without proper dynamic hardware detection — a 3B model (`helixllm-anton-qwen2-5-coder-3b-instruct-q4_k_m-f6771589d190`) was chosen on a 16-core host with a powerful NVIDIA GPU and ample RAM. The mandate requires: (1) detect hardware of the host and of distribution endpoints; (2) determine all runnable model types (LLM, Vision, TTS, STT, and others) and expose each for selection; (3) clean names without the `helixllm-*` prefix or hash/quantization suffixes (e.g. `qwen2-5-coder-3b-instruct`); (4) automatic start/stop/switch lifecycle so models are not all loaded simultaneously; (5) prefer llama.cpp and colibri for efficiency, allowing multiple concurrent models within hardware limits.

## User Scenarios & Testing *(mandatory)*

### User Story 1 - Hardware-Aware Model Discovery and Selection (Priority: P1)

As a developer using HelixLLM/HelixAgent, I want the system to automatically detect my hardware capabilities (CPU cores, GPU VRAM, system RAM) and present me with all compatible models (LLMs, Vision, TTS, STT, etc.) that can run efficiently on my machine, so that I can choose the most powerful models my hardware supports without manual configuration.

**Why this priority**: This is the foundation - without proper hardware detection and model matching, users get suboptimal models (like 3B on hardware that supports 70B+) and cannot leverage their full compute capacity.

**Independent Test**: Can be fully tested by running hardware detection on a known machine configuration and verifying the model catalog matches expected capabilities for that hardware tier.

**Acceptance Scenarios**:
1. **Given** a machine with 16 CPU cores, powerful NVIDIA GPU with 24GB+ VRAM, and 64GB+ system RAM, **When** hardware detection runs, **Then** the system identifies GPU VRAM, CPU cores, system RAM, and recommends models up to 70B+ parameter size
2. **Given** a machine with limited GPU VRAM (8GB) but high system RAM (32GB), **When** hardware detection runs, **Then** the system recommends quantized models that fit in VRAM with CPU offload for larger models
3. **Given** a CPU-only machine with 32 cores and 128GB RAM, **When** hardware detection runs, **Then** the system recommends CPU-optimized models (llama.cpp with threading) and excludes GPU-only models
4. **Given** a distribution endpoint (remote worker), **When** hardware detection queries the endpoint, **Then** the system retrieves and caches the remote hardware profile for model placement decisions

---

### User Story 2 - Clean Model Naming and Multi-Type ModelCatalog (Priority: P1)

As a developer, I want models to have clean, human-readable names (e.g., "qwen2-5-coder-3b-instruct" instead of "helixllm-anton-qwen2-5-coder-3b-instruct-q4_k_m-f6771589d190") and be organized by type (LLM, Vision, TTS, STT, Embedding, Reranker) so that I can easily discover, select, and reference models in my workflows.

**Why this priority**: Current naming is unusable for humans; clean naming is essential for adoption and prevents errors in model selection.

**Independent Test**: Can be fully tested by listing available models and verifying all names follow the clean convention and are properly categorized by type.

**Acceptance Scenarios**:
1. **Given** the HelixLLM ModelCatalog, **When** a user lists available models, **Then** all model names are clean (no provider prefix, no hash suffixes, no quantization details in name) and grouped by type
2. **Given** a model "qwen2-5-coder-3b-instruct", **When** the user requests model details, **Then** the system shows the clean name, type (LLM), quantization options available, hardware requirements, and capabilities
3. **Given** Vision/TTS/STT models are registered, **When** filtering by type, **Then** only models of that type are shown with appropriate metadata (e.g., Vision: input resolution, TTS: supported languages/voices)

---

### User Story 3 - Intelligent Model Lifecycle Management (Start/Stop/Switch) (Priority: P1)

As a developer, I want the HelixLLM system to automatically manage model lifecycle - loading a model on first request, keeping it warm for subsequent requests, and gracefully unloading it when switching to another model or when resources are needed - so that I never run out of memory and always get fast responses without manual intervention.

**Why this priority**: Running all models simultaneously strangles the host; automatic lifecycle management is essential for production use on shared hardware.

**Independent Test**: Can be fully tested by sending requests to multiple models in sequence and verifying only the active model(s) consume resources, with clean transitions.

**Acceptance Scenarios**:
1. **Given** no models are loaded, **When** a request goes to Model A, **Then** Model A loads, responds, and stays loaded (warm)
2. **Given** Model A is warm, **When** a request goes to Model B and resources are insufficient for both, **Then** Model A unloads cleanly, Model B loads, responds, and stays warm
3. **Given** Model A is warm and hardware supports concurrent models, **When** a request goes to Model B, **Then** both Model A and Model B remain loaded (up to hardware capacity)
4. **Given** a model has been idle for a configurable timeout (default 5 minutes), **When** the timeout expires, **Then** the model unloads automatically to free resources
5. **Given** a model fails to load, **When** the error occurs, **Then** the system reports a clear error and does not leave partial state
6. **Given** concurrent requests exceed capacity, **When** capacity is exceeded, **Then** system queues requests with fair scheduling (FIFO per model type) and rejects with clear messaging when queue full

---

### User Story 4 - Systematic Debugging and Resolution of 5 Critical Integration Issues (Priority: P1)

As a developer, I want all 5 identified integration issues between HelixLLM/HelixAgent and Claude Toolkit systematically debugged using reproduce→root-cause→hypothesis→fix workflow per §11.4.102, with root causes identified and permanently fixed with regression tests so that the integration works reliably on first try.

**Why this priority**: These 5 issues block all usage of Helix models through Claude Toolkit and Pi CLI - they must be resolved before any other features matter.

**Independent Test**: Each issue can be independently verified by reproducing the original error scenario and confirming it no longer occurs.

**Acceptance Scenarios**:

**Issue 1 - ca-bundle.pem Overwrite Error**:
1. **Given** a fresh Claude Code session with HelixLLM provider alias, **When** the provider initializes, **Then** no "cannot overwrite existing file" error for ca-bundle.pem occurs
2. **Given** multiple concurrent sessions using the same model, **When** they initialize, **Then** no file lock/overwrite conflicts occur

**Issue 2 - Session Resume Failure (ID: ebee2470-ab31-43e9-a4ad-ad876b7dfb2e)**:
1. **Given** a previous session exists, **When** running `claude --resume "helix-code"`, **Then** the session resumes successfully without "No conversation found" error

**Issue 3 - Request Too Large (32MB Limit)**:
1. **Given** a conversation with accumulated images/attachments approaching 32MB, **When** a new request is made, **Then** the system automatically compacts (removes oldest media, summarizes old turns, preserves recent context) first, then streams if still over limit

**Issue 4 - Pi CLI Unverified Alias (pi-helixllm-gateway)**:
1. **Given** the Pi CLI agent with HelixLLM gateway alias, **When** the alias is invoked for the first time, **Then** it auto-verifies (health check + certificate validation), caches verified status, and launches without "unverified" error
2. **Given** the verification workflow, **When** running `claude-providers verify helixllm-gateway` and `claude-providers sync`, **Then** the alias becomes verified and works without --force, persisted across restarts

**Issue 5 - HelixAgent Connection Errors (helixagent-debate, helixagent-llm)**:
1. **Given** systematic debugging identifies root cause (protocol mismatch, auth failure, or serialization error), **When** fix is applied, **Then** connection succeeds on first attempt with no "Connection error" or retry failures
2. **Given** multiple sequential requests (200+), **Then** all succeed without connection drops, with persistent connections, health checks, exponential backoff retry (max 3 attempts, jitter), and circuit breaker

---

### User Story 5 - Exhaustive Testing and Validation on Live CLI Instances (Priority: P1)

As a developer, I want all changes validated by running exhaustive tests on live CLI agent instances (Claude Code, Pi CLI, Helix TUI) using the actual models being tested, so that I have confidence the system works end-to-end in real usage scenarios.

**Why this priority**: Unit tests alone cannot catch integration issues; only live CLI testing validates the full stack.

**Independent Test**: Can be fully tested by running the full test suite against live CLI instances and verifying all pass with real model responses.

**Acceptance Scenarios**:
1. **Given** the complete implementation, **When** the test suite runs against live Claude Code with HelixLLM models, **Then** all tests pass with real model responses (not mocked)
2. **Given** the complete implementation, **When** the test suite runs against live Pi CLI with HelixAgent models, **Then** all tests pass with real model responses
3. **Given** the complete implementation, **When** the test suite runs against live Helix TUI with all model types, **Then** all tests pass with real model responses
4. **Given** test results, **When** reviewed, **Then** all evidence is machine-captured and verifiable (no manual verification needed)
5. **Given** "live" is defined as: real CLI process + real model binary + real hardware (no mocks for hardware, models, or CLI processes), test isolation via per-test containers booted rootless through the `vasic-digital/containers` submodule (§11.4.161 / §11.4.76) with model provisioning

---

### User Story 6 - Multi-Submodule Release with Git Tags and Documentation (Priority: P2)

As a maintainer, I want all changes committed and pushed to every affected submodule (helix_llm, helix_agent, claude-toolkit, and any others), with proper git tags created via GitHub/GitLab CLIs, comprehensive changelogs, and updated documentation so that the release is complete and traceable.

**Why this priority**: Ensures all changes are properly versioned, distributed, and documented across the Helix family.

**Independent Test**: Can be fully tested by verifying all submodules have the changes, tags exist on all remotes, and documentation is updated.

**Acceptance Scenarios**:
1. **Given** all changes are implemented and tested, **When** the release process runs, **Then** all submodules receive commits with the changes (transactional with rollback on failure)
2. **Given** commits are pushed, **When** git tags are created, **Then** tags exist on both GitHub and GitLab remotes with proper version format (project-prefixed per §11.4.151)
3. **Given** tags are created, **When** changelogs are generated, **Then** changelogs accurately describe all changes, fixes, and improvements (conventional commits grouped by type)
4. **Given** documentation exists, **When** reviewed, **Then** all relevant docs reflect the new hardware-aware model management, naming conventions, lifecycle management, and issue fixes with 100% API/workflow coverage validated

---

### Edge Cases

- What happens when hardware detection fails or returns incomplete data? → System falls back to safe defaults (CPU-only, conservative model selection) and logs a warning
- What happens when a model fails to load due to OOM? → System catches OOM, unloads other models if needed, retries with lower quantization, or reports clear error to user
- What happens when two concurrent requests target different models exceeding capacity? → System queues requests with fair scheduling (FIFO per model type), rejects with clear messaging when queue full
- What happens when a remote distribution endpoint becomes unavailable? → System marks endpoint unhealthy, redistributes model placement, caches last known state; automatic deregistration after configurable timeout
- What happens when model quantization requirements change? → System re-evaluates hardware fit on registry change events and updates recommendations dynamically
- How does system handle model updates/upgrades? → Version-aware registry; clean migration path with backward-compatible aliases; migration task runs on version change
- What happens when backend compatibility changes? → System maintains backend compatibility matrix; re-evaluates model-backend assignments on registry/backend updates

---

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: System MUST automatically detect hardware capabilities (CPU cores, GPU model/VRAM, system RAM, disk space) on the current host and all registered distribution endpoints, and maintain hardware capability profiles with caching and periodic refresh (configurable interval, default 5 minutes)
- **FR-002**: System MUST match available local models to hardware profiles using a compatibility algorithm: compatibility_score = f(VRAM_required, RAM_required, quantization, context_len, backend) — the aggregation function `f(...)` is an `UNKNOWN:` resolved by the T010 research task, which records the exact weights/order with cited evidence (§11.4.6); hard thresholds (VRAM_fit ≥ 90%, RAM_fit ≥ 80%, quantization_supported, context_len ≤ max_context, backend_available) apply regardless; exposes ModelCatalog.FilterByHardware returning only compatible models
- **FR-003**: System MUST expose a ModelCatalog API with clean human-readable names (e.g., "qwen2-5-coder-3b-instruct") organized by type (LLM, Vision, TTS, STT, Embedding, Reranker) with complete metadata: clean name, type, parameter count, quantization options, minimum VRAM/RAM, recommended hardware, capabilities (tools, vision, streaming), runtime backend, version
- **FR-004**: System MUST implement a Lifecycle Manager with resource-aware load/switch/unload: load on demand, keep warm with configurable TTL (default 5 minutes, configurable via config/env), unload when switching or under memory pressure, support concurrent models within hardware limits; ResourceTracker enforces atomic VRAM/RAM counters, global max concurrent models (default 2, configurable via config/env), per-model max instances (default 1, configurable via config/env)
- **FR-005**: System MUST prioritize the existing llama.cpp and colibri backends for maximum efficiency with automatic backend selection (colibri where the model's architecture supports it, per the supported-architecture set resolved by the T010 research task; llama.cpp otherwise; fallback chain colibri → llama.cpp → clear error). Backends MUST be consumed through their EXISTING interfaces — the llama.cpp provider is an HTTP client and colibri is a launched process — and this feature MUST NOT introduce a new in-process CGO boundary. Where the existing runtime-choice path already satisfies this, it MUST be reused rather than reimplemented (§11.4.74)
- **FR-006**: System MUST enable on-the-fly model switching with protocol: max switch time 15s; request queueing during switch (per-model FIFO); rollback on load failure (restore Model A if Model B fails); when both cannot fit, queue the lower-priority request behind the active model (per-model FIFO) and return a clear "capacity exceeded" error only when that queue is full (observable, measurable degradation — no silent drop)
- **FR-007**: System MUST fix ca-bundle.pem overwrite conflict in Claude Code Router integration using atomic write (temp file + rename), file locking, shared ca-bundle per model (not per session) with reference counting, cleaned on model unload
- **FR-008**: System MUST fix session resume failure for session ID ebee2470-ab31-43e9-a4ad-ad876b7dfb2e by ensuring session ID written to storage before response, handling concurrent access, with TTL-based expiration and orphaned session cleanup
- **FR-009**: System MUST handle request size limits (32MB) gracefully: estimate request size before send, trigger compaction if >28MB (headroom); compaction priority: remove oldest images/attachments → summarize old turns → preserve recent context; if still >32MB, stream response in chunks
- **FR-010**: System MUST auto-verify Pi CLI alias "pi-helixllm-gateway" on first invocation (health check endpoint + certificate validation), cache verified status, persist across restarts; provide manual `claude-providers verify helixllm-gateway` + `claude-providers sync` commands
- **FR-011**: System MUST fix HelixAgent connection errors by performing systematic debugging per §11.4.102 (reproduce → root cause → hypothesis → fix verification) for each error; implement persistent connections with health checks, connection pool (configurable size, idle timeout, max lifetime, health check interval), exponential backoff retry (max 3 attempts, jitter), circuit breaker, gRPC health checking endpoints
- **FR-012**: System MUST provide comprehensive test coverage per constitution §11.4.169: all 13 mandated test types (unit, integration, e2e, challenges, HelixQA, stress, chaos, security, performance, benchmarking, concurrency/atomicity, race-condition/deadlock, memory) with paired mutations at every layer per §1.1
- **FR-013**: System MUST validate all functionality on LIVE running CLI instances (Claude Code, Pi CLI, Helix TUI) with real model responses; "live" = real CLI process + real model binary + real hardware (no mocks for hardware, models, or CLI processes); test isolation via per-test containers booted rootless through the `vasic-digital/containers` submodule (§11.4.161 / §11.4.76) with model provisioning (download, verify, cache)
- **FR-014**: System MUST implement four-layer verification per §11.4.108 for every change: (1) pre-build (static analysis, lint, type-check), (2) post-build (artifact verification, byte-check), (3) on-device/clean-target (runtime signature on fresh deployment), (4) user-visible (captured runtime evidence per §11.4.5/§11.4.69)
- **FR-015**: System MUST commit and push changes to all affected submodules (helix_llm, helix_agent, claude-toolkit, etc.) transactionally with rollback on failure; per-submodule success tracking; retry logic
- **FR-016**: System MUST create release git tags on all submodules using GitHub CLI (gh) and GitLab CLI (glab) with version format per §11.4.151 (project-prefixed), ff-only push to latest main per §11.4.113
- **FR-017**: System MUST generate comprehensive changelogs for each submodule from conventional commits, grouped by type (feat/fix/docs/chore)
- **FR-018**: System MUST update all relevant documentation (README, user guides, API docs, architecture docs) with 100% API/workflow coverage validated (OpenAPI for APIs, example count for workflows)
- **FR-019**: System MUST register and manage distribution endpoints: registration API, health monitoring, automatic deregistration after configurable timeout, endpoint capability advertisement
- **FR-020**: System MUST perform systematic debugging per §11.4.102 for each integration issue: reproduce on broken artifact → root cause analysis → hypothesis → implementation → RED→GREEN verification on clean target per §11.4.115

### Key Entities

- **Hardware Profile**: Represents compute capabilities of an environment (host or remote) - CPU cores, GPU(s) with VRAM, system RAM, disk, OS, runtime backends available, last_refreshed timestamp
- **Model Specification**: Defines a model's identity (clean name), type, parameter count, quantization variants, hardware requirements (min VRAM/RAM, recommended), capabilities (tools, vision, streaming), runtime backend, version, backend_compatibility (colibri/llama.cpp)
- **Model Instance**: A running/loaded model with state (loaded/warm/cold), resource allocation (VRAM/RAM), assigned backend, request queue, loaded_at, last_accessed, TTL
- **ModelCatalog**: Central catalog of all available Model Specifications, queryable by type, hardware compatibility, and capabilities; dynamic discovery from HelixLLM/HelixAgent/LLMsVerifier (no hardcoded lists per CONST-036)
- **Lifecycle Manager**: Component responsible for loading, warming, switching, and unloading Model Instances based on demand and resource constraints; ResourceTracker with atomic VRAM/RAM counters
- **Distribution Endpoint**: Remote worker/container with its own Hardware Profile, registered for distributed model placement; registration API, health monitoring, automatic deregistration
- **Provider Alias**: Named entry point in Claude Toolkit (or Pi CLI) that maps to a HelixLLM/HelixAgent model endpoint; verification status, sync state
- **Model Version Migration**: Tracks model version changes, provides backward-compatible aliases, runs migration on registry update

---

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: Hardware detection completes in under 5 seconds and accurately identifies GPU VRAM within 5% of actual on 100% of the enumerated calibration tiers: (a) 16-core CPU + NVIDIA GPU with 24GB+ VRAM + 64GB+ RAM, (b) 8GB-VRAM GPU + 32GB RAM, (c) CPU-only 32-core + 128GB RAM; calibration test validates accuracy against known hardware specs for each tier
- **SC-002**: ModelCatalog.FilterByHardware returns only compatible models (zero false positives: incompatible models never returned as available)
- **SC-003**: Model load time for first request is under 30 seconds for 7B models, under 60 seconds for 70B models on target hardware; benchmark harness measures and enforces with CI regression detection
- **SC-004**: Model switch time (unload A + load B) is under 15 seconds for models within same quantization tier; benchmark harness with CI regression detection
- **SC-005**: Zero ca-bundle.pem overwrite errors across 100 consecutive Claude Code session initializations
- **SC-006**: Session resume succeeds for 100% of valid session IDs across 50 test sessions
- **SC-007**: Zero "Request too large" errors for conversations under 100 turns with mixed media; auto-compaction triggers before limit (compaction first, then streaming)
- **SC-008**: Pi CLI alias "pi-helixllm-gateway" launches successfully without --force in 100% of fresh environment setups (auto-verify on first use)
- **SC-009**: HelixAgent models (helixagent-debate, helixagent-llm) achieve 100% connection success rate over 200 sequential requests (persistent connections, health checks, retry with backoff)
- **SC-010**: All 13 constitution-mandated test types pass: unit, integration, e2e, challenges, HelixQA, stress, chaos, security, performance, benchmarking, concurrency/atomicity, race-condition/deadlock, memory; each with paired mutations at every layer per §1.1
- **SC-011**: Live CLI testing achieves 100% pass rate on 50+ real prompts across Claude Code, Pi CLI, and Helix TUI; prompt catalog defines expected behavior patterns per model type
- **SC-012**: All submodules (helix_llm, helix_agent, claude-toolkit, etc.) receive commits and tags on both GitHub and GitLab remotes; transactional release with rollback
- **SC-013**: Documentation completeness: 100% of new APIs, configurations, and workflows documented with examples; validated via OpenAPI spec coverage for APIs, example count for workflows
- **SC-014**: Benchmark infrastructure measures load/switch times with CI regression detection
- **SC-015**: Model provisioning for test environments (download, verify, cache) enables live CLI testing without manual setup

---

## Assumptions

- Target hardware: 16+ CPU cores, NVIDIA GPU with 12GB+ VRAM, 32GB+ system RAM (primary); CPU-only fallbacks supported
- llama.cpp and colibri are available as primary inference backends; others (llamafile, ollama, vLLM) as optional; specific versions pinned in build config
- HelixLLM, HelixAgent, and claude-toolkit are git submodules within helix_code with independent release cycles
- Claude Code Router is the integration point for Claude Code; Pi CLI uses its own provider system
- Constitution-mandated test infrastructure (Docker Compose with PostgreSQL, Redis, Ollama, etc.) is available
- GitHub CLI (gh) and GitLab CLI (glab) are configured with appropriate permissions for tagging
- Model files are sourced from Hugging Face or local cache via dynamic discovery (no hardcoded lists per CONST-036); quantization done via llama.cpp tools
- Session persistence for Claude Code uses standard conversation storage mechanism
- Network connectivity exists between helix_code host and distribution endpoints
- **Reuse-first constraint**: the catalogue-first inventory in `research.md` is authoritative for what already exists in helix_llm, helix_agent, and claude-toolkit. Every requirement below MUST be satisfied by reusing or extending an existing component before any new component is created; a new package that duplicates an existing capability is a §11.4.74 violation.
- CGO thread safety, memory management, and panic recovery are handled for llama.cpp/colibri wrappers

---

## Clarifications

### Session 2026-09-12

- Q: Should the HelixLLM ModelCatalog include only locally-run models (llama.cpp/colibri), or also cloud/remote models accessed via API (OpenAI, Anthropic, Gemini, etc.)? → A: Local models only (llama.cpp/colibri backends) - hardware-aware, lifecycle managed