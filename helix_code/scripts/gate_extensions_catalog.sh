#!/usr/bin/env bash
# scripts/gate_extensions_catalog.sh — CM-EXTENSIONS-CATALOG-PRESENT-AND-COMPLETE
# gate (T-P8.02, HXC-159 Phase P8; §11.4.228 Cross-agent extension lifecycle
# mandate).
#
# Purpose: asserts every skill registered in scripts/skills_manifest.tsv
# (the T-P1.04.2 manifest scripts/register_skills.sh already consumes) has a
# row in docs/extensions/EXTENSIONS_CATALOG.md's catalog.json sidecar, and
# that row carries a non-blank per-platform compatibility cell (status +
# path) for every one of the five governed CLI-agent platforms: Claude Code,
# OpenCode, HelixCode, Gemini CLI, Qwen Code (RS-21).
#
# This gate is READ-ONLY against the checked-in catalogue — it does NOT
# regenerate it. Regeneration is
#   python3 helix_code/scripts/extensions_catalog/generate_catalog.py \
#     REPO_ROOT scripts/skills_manifest.tsv docs/extensions
# run separately (never auto-invoked by this gate, mirroring the
# read-only/generate-is-separate split already used by
# helix_code/scripts/coverage_ledger/{generate,validate}_ledger.py).
#
# Usage:
#   scripts/gate_extensions_catalog.sh [MANIFEST] [CATALOG_JSON]
#     MANIFEST      default: <repo-root>/scripts/skills_manifest.tsv
#     CATALOG_JSON  default: <repo-root>/docs/extensions/catalog.json
#
# Paired mutation (§1.1 / T-P8.02.4): register a skill in the manifest with
# no corresponding row in catalog.json -> this gate MUST exit non-zero and
# name the missing skill. See
# helix_code/scripts/extensions_catalog/test_gate_paired_mutation.sh for the
# exercised mutation.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
HELIX_CODE_DIR="$(cd "${SCRIPT_DIR}/.." && pwd)"
REPO_ROOT="$(cd "${HELIX_CODE_DIR}/.." && pwd)"

MANIFEST="${1:-${REPO_ROOT}/scripts/skills_manifest.tsv}"
CATALOG_JSON="${2:-${REPO_ROOT}/docs/extensions/catalog.json}"

exec python3 "${HELIX_CODE_DIR}/scripts/extensions_catalog/validate_catalog.py" \
  "${MANIFEST}" "${CATALOG_JSON}"
