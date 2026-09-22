#!/usr/bin/env bash
# scripts/gate_mcp_entries_generated.sh — CM-MCP-ENTRIES-GENERATED gate
# (T-P5.04, HXC-159 Phase P5).
#
# Purpose: proves a target .mcp.json's manifest-derived server entries
# (T-P1.04.2 TSV schema: name<TAB>source<TAB>owner) match what
# internal/mcpgen.Generate would currently produce from the manifest — i.e.
# no path in a generated-looking entry has silently drifted from its
# manifest-declared source (a hand-edit, a stale copy, a re-coupled hardcoded
# string). Exits 0 (WIRED) iff every manifest-derived entry matches; exits
# non-zero (NOT-WIRED) and names the drifted entries otherwise.
#
# READ-ONLY: this gate never writes to the manifest or the target file it
# checks (it calls `mcpgen check`, never `mcpgen generate`).
#
# Usage:
#   scripts/gate_mcp_entries_generated.sh [MANIFEST] [TARGET_MCP_JSON] [ROOT]
#     MANIFEST         default: <repo-root>/scripts/skills_manifest.tsv
#     TARGET_MCP_JSON  default: <repo-root>/.mcp.json
#     ROOT             default: <repo-root> (the manifest's `source` column
#                       is relative to this directory)
#
# Paired mutation (§1.1): hand-edit one generated entry's `args` path in
# TARGET_MCP_JSON to a different string → this gate MUST exit non-zero and
# name that entry. internal/mcpgen/mcpgen_test.go's TestDrift_DetectsHandEditedPath
# exercises the exact same mutation against the Go engine this script calls;
# this script is the shell-invocable entry point to that same logic.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
HELIX_CODE_DIR="$(cd "${SCRIPT_DIR}/.." && pwd)"
REPO_ROOT="$(cd "${HELIX_CODE_DIR}/.." && pwd)"

MANIFEST="${1:-${REPO_ROOT}/scripts/skills_manifest.tsv}"
TARGET="${2:-${REPO_ROOT}/.mcp.json}"
ROOT="${3:-${REPO_ROOT}}"

cd "${HELIX_CODE_DIR}"
exec go run ./cmd/mcpgen check --manifest "${MANIFEST}" --root "${ROOT}" --out "${TARGET}"
