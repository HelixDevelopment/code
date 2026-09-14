#!/usr/bin/env bash
# CM-CONSTITUTION-PIN-SINGLE (HXC-159 T-P3.05.5): the superproject's DIRECT
# constitution pin must be a single recorded gitlink and must NOT be the
# known-stale 68875c7a (the value a branch incidentally carried; merging it
# would regress the pin). Nested pins (e.g. skills' 68875c7a under
# update=none) are out of scope — governed by CM-NO-NESTED-OWN-ORG-CHAIN.
# RED: reset the direct pin to 68875c7a -> gate MUST FAIL.
set -u
ROOT="$(git rev-parse --show-toplevel 2>/dev/null || pwd)"
STALE="68875c7ace02d83e15f53828a911fb46a97dd9e3"
pin="$(git -C "$ROOT" ls-tree HEAD constitution 2>/dev/null | awk '{print $3}')"
[ -n "$pin" ] || { echo "CM-CONSTITUTION-PIN-SINGLE FAIL: no direct constitution gitlink"; exit 1; }
if [ "$pin" = "$STALE" ]; then
  echo "CM-CONSTITUTION-PIN-SINGLE FAIL: direct pin is stale $STALE"
  exit 1
fi
echo "CM-CONSTITUTION-PIN-SINGLE PASS: direct pin $pin (single, not stale)"
