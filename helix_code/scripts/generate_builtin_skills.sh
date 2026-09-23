#!/usr/bin/env bash
# helix_code/scripts/generate_builtin_skills.sh — regenerates the
# constitution-sourced entries of internal/commands/builtin_skills/ from the
# canonical constitution/skills/ source (HXC-159 T-P9.01 finding F-13).
#
# ── Purpose ──────────────────────────────────────────────────────────────────
#   internal/commands/markdown_skills.go embeds internal/commands/builtin_skills/
#   into the HelixCode binary via `//go:embed builtin_skills` so a set of
#   governance skills works out of the box with no on-disk files. T-P9.01
#   finding F-13 reported these 7 entries as byte-for-byte "drifted" from
#   constitution/skills/, contradicting "source is authoritative".
#
# ── HONEST BOUNDARY (§11.4.6) — investigated 2026-09-23, F-13 root cause ────
#   Running this script's --check confirms the 7 entries genuinely differ
#   from constitution/skills/ — but the CONTENT-LEVEL investigation (diffing
#   frontmatter AND body, not just file bytes) shows this is NOT accidental
#   copy-paste drift of the SAME artifact. The two sides are two
#   STRUCTURALLY DIFFERENT artifact types that happen to share a directory
#   name and a governance theme:
#     - constitution/skills/<name>/SKILL.md — the canonical Claude-Code-
#       native Agent Skill: `name`/`description`/`version` frontmatter, full
#       reference-documentation body, consumed by Claude Code's own
#       semantic-description-driven skill discovery.
#     - internal/commands/builtin_skills/<name>/SKILL.md — a HelixCode-
#       INTERNAL prompt TEMPLATE for its own regex-triggered in-app REPL
#       completion feature: `description`/`triggers` (explicit regexes)/
#       `requires_isolation` frontmatter, a short `{{ARG.x}}`-templated
#       instruction body, consumed by markdown_skills.go's OWN loader and
#       auto-trigger matcher — a different HelixCode product surface
#       entirely, not a copy of the constitution doc.
#   PROVEN (not guessed): running this script's real (non --check) mode once
#   and re-running `go test ./internal/commands/...` broke
#   TestGovernanceSkills_RegisteredAndInvocable for all 7 skills ("declares
#   no triggers, so auto-trigger can never fire" / "must carry a
#   description") — overwriting builtin_skills/*/SKILL.md with the
#   constitution-native format silently DESTROYS the regex-trigger
#   completion feature these tests exist to protect. That run was reverted
#   (`git checkout --`); it was never committed.
#
#   CONCLUSION: full byte-identical generation, as this script's earlier
#   header claimed, is NOT SAFE to apply as-is — it is a real capability
#   loss (§11.4.122), not a sync fix. This script remains as REAL, TESTED
#   drift-DETECTION tooling (`--check`, wired per §11.4.77) so future
#   divergence is at minimum VISIBLE and tracked rather than silently
#   invisible; its regenerate mode is preserved for the day the two formats
#   are reconciled (e.g. if markdown_skills.go's loader is redesigned to
#   consume Claude-Code-native frontmatter, or if this internal mechanism is
#   retired in favour of the pkg/skills external tier once that work lands),
#   but it MUST NOT be run in regenerate mode against the live tree without
#   that reconciliation — doing so reintroduces the exact capability
#   regression proven above. Tracked as a §11.4.197 follow-up requiring an
#   explicit architectural decision, not executed unilaterally here.
#
# ── Usage ────────────────────────────────────────────────────────────────────
#   bash helix_code/scripts/generate_builtin_skills.sh [--check]
#     --check   report drift, change nothing (exit 0 = in sync, 1 = drift)
#     (no args) copy every constitution-sourced skill's SKILL.md (and any
#               sibling files present) into builtin_skills/, exit 0 always
#               unless a copy fails
#
# ── Which skills this covers ─────────────────────────────────────────────────
#   Every subdirectory already present under internal/commands/builtin_skills/
#   THAT ALSO has a same-named directory under constitution/skills/ is
#   treated as constitution-sourced and is kept byte-identical to it.
#   `conventional-commit` has no constitution/skills/ counterpart — it is
#   HelixCode's OWN built-in skill, not constitution-sourced, and this script
#   never touches it. Adding a NEW constitution-sourced builtin skill is done
#   by creating internal/commands/builtin_skills/<name>/ once (any content —
#   this script will overwrite it on the next run) and adding the row to
#   scripts/skills_manifest.tsv per the normal registrar flow (§11.4.164);
#   this script only keeps ALREADY-linked names in sync, it never invents a
#   new binding on its own (§11.4.6 — never guess which names should link).
#
# ── Side-effects ─────────────────────────────────────────────────────────────
#   Overwrites internal/commands/builtin_skills/<name>/* for every
#   constitution-sourced <name> with the current constitution/skills/<name>/*
#   content. Never touches conventional-commit/. Never writes outside
#   internal/commands/builtin_skills/.
#
# ── Dependencies ─────────────────────────────────────────────────────────────
#   bash, cp, diff, find.
#
# ── Cross-references ─────────────────────────────────────────────────────────
#   §11.4.77 (regeneration mechanism), §11.4.6 (no-guessing — never invents a
#   binding), §11.4.108 (source-of-truth / artifact-drift discipline),
#   internal/commands/markdown_skills.go (the //go:embed consumer),
#   scripts/skills_manifest.tsv (constitution/CLAUDE.md's registrar source).
#
# Classification: project-specific (§11.4.17) — this consumer's generator.
# Last verified: 2026-09-23
set -euo pipefail

SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
HELIX_CODE_ROOT="$(cd -- "$SCRIPT_DIR/.." && pwd)"
REPO_ROOT="$(cd -- "$HELIX_CODE_ROOT/.." && pwd)"

BUILTIN_DIR="$HELIX_CODE_ROOT/internal/commands/builtin_skills"
SOURCE_DIR="$REPO_ROOT/constitution/skills"

CHECK_ONLY=0
[ "${1:-}" = "--check" ] && CHECK_ONLY=1

[ -d "$BUILTIN_DIR" ] || { echo "[generate-builtin-skills] ERROR: missing $BUILTIN_DIR" >&2; exit 2; }
[ -d "$SOURCE_DIR" ] || { echo "[generate-builtin-skills] ERROR: missing $SOURCE_DIR" >&2; exit 2; }

drift=0
synced=0
skipped=0

for dir in "$BUILTIN_DIR"/*/; do
  [ -d "$dir" ] || continue
  name="$(basename "$dir")"
  src="$SOURCE_DIR/$name"
  if [ ! -d "$src" ]; then
    skipped=$((skipped + 1))
    echo "[generate-builtin-skills] SKIP $name (no constitution/skills/$name — not constitution-sourced)"
    continue
  fi

  # Sync ONLY the files already present under builtin_skills/<name>/ —
  # NEVER the whole constitution/skills/<name>/ directory. The constitution
  # source dir also carries its own register.sh helper and §11.4.65 export
  # siblings (SKILL.html/.pdf/.docx) that are neither meant to be embedded
  # into the Go binary nor consumed by markdown_skills.go's SKILL.md-only
  # loader — pulling those in would silently bloat/change the embed's
  # contents beyond what this generator is chartered to keep in sync
  # (§11.4.6: sync the declared surface, never invent a wider one).
  any_drift=0
  for f in "$dir"*; do
    [ -f "$f" ] || continue
    base="$(basename "$f")"
    src_f="$src/$base"
    if [ ! -f "$src_f" ]; then
      echo "[generate-builtin-skills] WARN $name/$base has no constitution/skills/$name/$base counterpart — left untouched"
      continue
    fi
    if [ "$CHECK_ONLY" -eq 1 ]; then
      if ! diff -q "$src_f" "$f" >/dev/null 2>&1; then
        any_drift=1
      fi
      continue
    fi
    cp -f "$src_f" "$f"
  done

  if [ "$CHECK_ONLY" -eq 1 ]; then
    if [ "$any_drift" -eq 1 ]; then
      drift=$((drift + 1))
      echo "[generate-builtin-skills] DRIFT $name — differs from constitution/skills/$name (embedded-file set only):"
      for f in "$dir"*; do
        [ -f "$f" ] || continue
        base="$(basename "$f")"
        [ -f "$src/$base" ] || continue
        diff -q "$src/$base" "$f" >/dev/null 2>&1 || echo "    differs: $base"
      done
    fi
    continue
  fi

  synced=$((synced + 1))
  echo "[generate-builtin-skills] synced $name <- constitution/skills/$name"
done

if [ "$CHECK_ONLY" -eq 1 ]; then
  if [ "$drift" -eq 0 ]; then
    echo "[generate-builtin-skills] PASS — 0 drifted, $skipped skipped (non-constitution-sourced)"
    exit 0
  fi
  echo "[generate-builtin-skills] FAIL — $drift drifted (run without --check to regenerate)"
  exit 1
fi

echo "[generate-builtin-skills] done — synced=$synced skipped=$skipped"
