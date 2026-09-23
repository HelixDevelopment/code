# Tasks: Hello-Bridge Marker

**Input**: Design documents from `specs/007-hello-bridge-marker/`
**Prerequisites**: plan.md, spec.md

## Task Format

```
[ID] [markers] [Story] Description
```

## Tasks

- [x] T001 [SUBAGENT] [US1] Write `specs/007-hello-bridge-marker/hello_bridge_marker.txt`
  containing the exact line: `Written by a dispatched Claude Code subagent under
  /speckit-specify -> /speckit-plan -> /speckit-tasks -> /speckit-implement. No MCP
  server (SuperBridge MCP / L5) was used at any step of this flow.` Dispatch a
  subagent to perform the write (not the orchestrating session directly), per
  spec.md FR-003. Verify the file's content byte-for-byte after the subagent
  reports completion (spec.md SC-001).

## Dependencies

T001 has no dependencies; it is the only task in this demo feature.
