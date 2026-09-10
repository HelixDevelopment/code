# Feature Specification: HelixLLM Provider Exposure, Claude Toolkit Integration, and Heroku MCP Removal

**Feature Branch**: `004-helixllm-claude-toolkit-integration`

**Created**: 2026-09-10

**Status**: Draft

**Input**: User description: "Remove completely Heroku MCP implementation! Remove MCP Heroku Submodule fully as well so no Heroku implementation exists! Check if anywhere it is wired and unwire it carefully! Another thing - Make sure we expose as provider HelixLLM and all models it is offering and wire it fully same way we did (and we have planned as well) HelixAgent / HelixLLM (with all models) !!! Once all is done, make sure we fully and completely bind support for locally running HelixAgent / HelixLLM and HelixLLM providers with Claude Toolkit and any running remotely in the cloud or in local network! Write fully documentation for this! Claude Toolkit is located under claude_toolkit directory under same parent directory (Projects) we are located in as well! Extend Claude Toolkit fully for all described scenarios and test it fully and completely LIVE after installing the latest codebase after all changes! We MUST do to it another fixing - kimi1 and kimi2 aliases do not work, whatever we assign to them, we are entering endless context compacting loop(s)! Everything MUST BE fully investigated, tested, debugged and finally after changes to both HelixCode (helix_code) with all its Sub-Systems (submodules) and Claude Toolkit (claude_toolkit) (and its Sub-Systems / Submodules too) performing fully LIVE (make sure we do full installation / setup using proper bash scripts) with full validation and verification of machine produced evidence with deterministic confirmations! All documentation in all Submodules and Repos MUST BE fully extended and updated with all relevant materials! Make sure we do commit and push regularly all changes of all projects / repos / submodules to all upstreams with proper merging!"

## User Scenarios & Testing *(mandatory)*

### User Story 1 - HelixLLM as a First-Class Provider in HelixCode (Priority: P1)

As a HelixCode operator, I want HelixLLM registered as a provider with all models it offers so that I can dispatch generation requests to HelixLLM using the same provider abstraction already used for HelixAgent and other LLM providers.

**Why this priority**: Exposing HelixLLM unlocks the project's own LLM subsystem as a runtime dependency, aligning provider wiring with the existing architecture and removing special-case handling.

**Independent Test**: A fresh HelixCode installation lists HelixLLM in its provider registry, and a generation request routed to HelixLLM returns a non-error response.

**Acceptance Scenarios**:

1. **Given** HelixCode is installed and configured, **When** the provider list is queried, **Then** HelixLLM appears with the complete model set exposed by the HelixLLM submodule.
2. **Given** HelixLLM is enabled and reachable, **When** a generation request is dispatched to a HelixLLM model, **Then** the request is correctly formatted, sent, and the response is surfaced to the caller.
3. **Given** HelixLLM configuration is invalid or the service is unreachable, **When** a request is attempted, **Then** the provider returns a clear, actionable error without crashing the orchestration layer.

---

### User Story 2 - Claude Toolkit Connects to HelixAgent, HelixLLM, and HelixLLM Everywhere (Priority: P1)

As a Claude Toolkit user, I want to configure and use HelixAgent, HelixLLM, and HelixLLM providers whether they run on my local machine, another machine on my LAN, or a remote host in the cloud so that my setup adapts to any deployment topology.

**Why this priority**: Provider binding must be environment-agnostic for the integration to be usable across development, home-lab, and production deployments.

**Independent Test**: Claude Toolkit's installation script and configuration surface allow selecting local/LAN/remote endpoints for HelixAgent, HelixLLM, and HelixLLM, and a chat request succeeds against each topology.

**Acceptance Scenarios**:

1. **Given** a local HelixAgent or HelixLLM process is running, **When** Claude Toolkit is configured for local mode, **Then** a request routes through the local endpoint and returns a response.
2. **Given** HelixAgent or HelixLLM is running on a LAN host, **When** Claude Toolkit is configured with the LAN host address, **Then** requests route to that host and return responses.
3. **Given** HelixAgent or HelixLLM is running on a remote/cloud host, **When** Claude Toolkit is configured with the remote endpoint, **Then** requests route to that host and return responses.
4. **Given** a provider configuration is changed, **When** Claude Toolkit reloads or restarts, **Then** the new endpoint is used without requiring source-code edits.

---

### User Story 3 - Fix kimi1 and kimi2 Aliases in Claude Toolkit (Priority: P1)

As a Claude Toolkit user, I want the `kimi1` and `kimi2` model aliases to resolve and complete requests reliably so that I do not get trapped in an endless context-compaction loop.

**Why this priority**: Broken aliases block a primary user path and degrade trust in the toolkit; fixing them is a prerequisite for releasing the integration.

**Independent Test**: A request sent via the `kimi1` or `kimi2` alias completes and returns a response without triggering repeated context compaction.

**Acceptance Scenarios**:

1. **Given** a valid `kimi1` alias configuration, **When** a user sends a request through that alias, **Then** the request completes and returns output.
2. **Given** a valid `kimi2` alias configuration, **When** a user sends a request through that alias, **Then** the request completes and returns output.
3. **Given** either alias is exercised repeatedly, **When** observed over multiple turns, **Then** context size remains bounded and no compaction loop occurs.

---

### User Story 4 - Remove Heroku MCP Implementation and Submodule from HelixCode (Priority: P2)

As a HelixCode maintainer, I want the Heroku MCP implementation and its submodule completely removed and unwired so that no unsupported or dead integration remains in the codebase.

**Why this priority**: Dead code and submodules create maintenance burden, confuse new contributors, and can break builds or scans; removal keeps the repository honest to its supported surface.

**Independent Test**: A repository-wide search finds no Heroku MCP source files, no `.gitmodules` entry, no import references, and no build or configuration wiring after the removal.

**Acceptance Scenarios**:

1. **Given** the Heroku MCP submodule is present, **When** the removal task runs, **Then** the submodule directory, `.gitmodules` entry, and any related documentation are deleted.
2. **Given** source files reference Heroku MCP, **When** the removal task runs, **Then** those references are removed or replaced with neutral fallbacks.
3. **Given** build scripts or provider registries include Heroku MCP wiring, **When** the removal task runs, **Then** the wiring is removed and the build still succeeds.

---

### User Story 5 - Complete Documentation Across HelixCode, Claude Toolkit, and Submodules (Priority: P2)

As a maintainer and end user, I want installation, configuration, provider registration, alias setup, and live verification procedures documented across HelixCode, Claude Toolkit, and their submodules so that the integration is discoverable and reproducible.

**Why this priority**: Without documentation, the integration cannot be operated by anyone except the original implementer, violating the usability mandate for shipped features.

**Independent Test**: A new user can follow the documentation to install both projects, configure providers, run a live verification command, and observe a passing result.

**Acceptance Scenarios**:

1. **Given** the documentation is published, **When** a user follows the HelixCode provider setup section, **Then** HelixLLM is registered and reachable.
2. **Given** the documentation is published, **When** a user follows the Claude Toolkit configuration section, **Then** local/LAN/remote providers are selectable and working.
3. **Given** the documentation is published, **When** a user follows the live verification section, **Then** deterministic evidence artifacts are produced and can be inspected.

---

### Edge Cases

- What happens when HelixLLM is configured but the HelixLLM submodule is not initialized or is on an incompatible commit?
- How does the system handle a Claude Toolkit configuration that points to a HelixAgent/HelixLLM endpoint that is temporarily unreachable?
- What is the behavior when both local and remote HelixLLM providers are configured simultaneously?
- How are the `kimi1`/`kimi2` aliases handled when the underlying model is rate-limited or unavailable?
- What cleanup is required if the Heroku MCP submodule has uncommitted local changes at removal time?

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: HelixCode MUST register HelixLLM as a provider with a model list sourced from the HelixLLM submodule.
- **FR-002**: HelixCode MUST route generation requests to HelixLLM using the same provider abstraction as HelixAgent and other LLM providers.
- **FR-003**: Claude Toolkit MUST support HelixAgent, HelixLLM, and HelixLLM providers across local, LAN, and remote/cloud connection modes.
- **FR-004**: Claude Toolkit MUST fix the `kimi1` and `kimi2` aliases so they complete requests without entering endless context-compaction loops.
- **FR-005**: HelixCode MUST remove all Heroku MCP source files, the Heroku MCP submodule, `.gitmodules` entries, imports, build wiring, and documentation references.
- **FR-006**: Documentation across HelixCode, Claude Toolkit, and their submodules MUST cover installation, configuration, provider registration, alias setup, and live verification procedures.
- **FR-007**: Live installation and verification MUST be performed using the projects' bash installation scripts and MUST produce deterministic evidence artifacts.
- **FR-008**: Changes across all touched repositories and submodules MUST be committed, merged with upstream changes, and pushed to all configured upstreams regularly.

### Key Entities *(include if feature involves data)*

- **Provider**: The runtime abstraction in HelixCode that dispatches requests to a model backend. HelixLLM becomes a new Provider alongside HelixAgent and others.
- **Model alias**: A short name in Claude Toolkit (e.g., `kimi1`, `kimi2`) that resolves to a configured model or provider endpoint.
- **Endpoint configuration**: The host, port, protocol, and authentication details that tell Claude Toolkit where a HelixAgent, HelixLLM, or HelixLLM instance lives.
- **Evidence artifact**: A machine-produced file (log, screenshot, JSON verdict, etc.) that proves a verification step succeeded.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: HelixLLM appears in HelixCode provider listings and accepts generation requests end-to-end.
- **SC-002**: Claude Toolkit connects to HelixAgent, HelixLLM, and HelixLLM in local, LAN, and remote modes without requiring source-code changes.
- **SC-003**: The `kimi1` and `kimi2` aliases in Claude Toolkit complete a request without triggering repeated context compaction.
- **SC-004**: No Heroku MCP files, submodule entries, import references, or build wiring remain in HelixCode.
- **SC-005**: A fresh installation via the projects' bash scripts succeeds end-to-end and produces deterministic evidence artifacts for provider connectivity and alias resolution.
- **SC-006**: Documentation is reachable from each repository's README and covers all setup, configuration, and verification steps.

## Assumptions

- The HelixLLM submodule is already incorporated into HelixCode at a known path and exposes a discoverable model list.
- Claude Toolkit is located at `/home/milosvasic/Projects/claude_toolkit` and uses bash-based installation scripts.
- Both HelixCode and Claude Toolkit support configuration via files or environment variables, without requiring recompilation for endpoint changes.
- Network reachability between Claude Toolkit and any local/LAN/remote HelixCode provider is the operator's responsibility.
- The Heroku MCP submodule removal can proceed without preserving Heroku-specific functionality, as it is explicitly requested to be fully removed.
