# Feature Specification: Hello-Bridge Marker

**Feature Branch**: `007-hello-bridge-marker`
**Created**: 2026-09-22
**Status**: Draft
**Input**: User description: "Add a hello-bridge marker file that proves the L5 execution tier is Spec Kit plus Superpowers plus Claude Code's own skill and subagent machinery, with no MCP server layer"

## User Scenarios & Testing *(mandatory)*

### User Story 1 - Real subagent implements a marker file (Priority: P1)

As the HXC-159 T-B3.02 investigator, I need a real, independently-verifiable artefact
proving that a `/speckit-specify → /speckit-plan → /speckit-tasks → /speckit-implement`
flow drives genuine subagent execution without any "SuperBridge MCP" (L5) server in the
loop, so the Bridge design corpus's claimed substitution can be recorded as PROVEN
rather than asserted.

**Why this priority**: This is the only story in this demo feature; it exists solely to
produce runtime-signature evidence for T-B3.02.

**Independent Test**: After `/speckit-implement` completes, `hello_bridge_marker.txt`
exists in the feature directory, contains the required proof sentence, and was written
by a dispatched Claude Code subagent (not the orchestrating session directly).

**Acceptance Scenarios**:

1. **Given** a fresh spec-kit feature directory with `spec.md`, `plan.md`, and `tasks.md`,
   **When** `/speckit-implement`-equivalent execution dispatches a subagent against the
   single task in `tasks.md`, **Then** `hello_bridge_marker.txt` exists with the exact
   required content and the subagent's own transcript is captured.

## Requirements *(mandatory)*

- **FR-001**: The feature MUST produce a file named `hello_bridge_marker.txt` inside this
  feature's directory.
- **FR-002**: The file's content MUST state that no MCP server layer was used to
  produce it.
- **FR-003**: The file MUST be written by a dispatched subagent, not the orchestrating
  session's own direct file write.

## Success Criteria *(mandatory)*

- **SC-001**: `hello_bridge_marker.txt` exists and its content is verified byte-for-byte
  against the required text.
- **SC-002**: The subagent's dispatch prompt and final report are both captured in the
  bidirectional transcript per §11.4.83.
