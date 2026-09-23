# Implementation Plan: Hello-Bridge Marker

**Branch**: `007-hello-bridge-marker` | **Date**: 2026-09-22 | **Spec**: [spec.md](./spec.md)
**Input**: Feature specification from `specs/007-hello-bridge-marker/spec.md`

## Summary

Write one marker file, `hello_bridge_marker.txt`, into this feature's directory, via a
dispatched Claude Code subagent, proving the T-B3.02 substitution: the execution tier
that replaces the phantom L5 "SuperBridge MCP" layer is Spec Kit's own
specify/plan/tasks/implement lifecycle driving Claude Code's own skill/subagent
machinery — no MCP server of any kind is invoked at any step of this flow.

## Technical Context

**Language/Version**: Bash / plain text file, no compiled language needed.
**Primary Dependencies**: none — Spec Kit's own bash scripts (`.specify/scripts/bash/`)
plus a dispatched Claude Code subagent. No MCP server, no `superbridge-mcp` package.
**Storage**: a single plain-text file in the feature directory. N/A otherwise.
**Testing**: a byte-for-byte content check against the required text (FR-002).
**Target Platform**: Linux dev host, isolated git worktree.
**Project Type**: single throwaway demo feature (HXC-159 T-B3.02 evidence only).
**Performance Goals**: N/A — proof-of-mechanism only.
**Constraints**: MUST NOT invoke any MCP server tool during execution — that is the
exact property under test.

## Constitution Check

This is a throwaway demo feature scoped entirely to `docs/qa/` evidence production
for HXC-159 T-B3.02; it does not touch any tracked project surface and is not itself
subject to the full release-gate constitution discipline. No gate is bypassed for any
change that ships.

## Project Structure

```
specs/007-hello-bridge-marker/
├── spec.md
├── plan.md
├── tasks.md
└── hello_bridge_marker.txt   ← produced by the dispatched subagent in /speckit-implement
```
