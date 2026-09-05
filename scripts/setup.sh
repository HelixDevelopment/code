#!/usr/bin/env bash
# scripts/setup.sh — RETIRED SHIM (W2e-2).
#
# The canonical installer is the repository-root ./setup.sh. This legacy
# script previously implemented a narrower, partially stale setup flow
# (wrong replica config, port drift :8081 vs canonical :8080, no HelixAgent /
# HelixLLM / infra / colibri wiring). It now delegates to the root script so
# both documented entry points converge on one path.
#
# Usage: bash scripts/setup.sh [any root setup.sh flags]

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

echo "[INFO] scripts/setup.sh is retired; delegating to $REPO_ROOT/setup.sh" >&2
exec "$REPO_ROOT/setup.sh" "$@"
