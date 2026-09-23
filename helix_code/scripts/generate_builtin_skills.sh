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
# ── HONEST BOUNDARY (§11.4.6) — resolved 2026-09-23, F-13 Option C ─────────
#   The 2026-09-23 investigation (docs/qa/hxc159_f13_builtin_skills_investigation_18ea70e5_20260923T064844Z/)
#   proved the two sides were STRUCTURALLY DIFFERENT artifact types: the
#   constitution side carried only `name`/`description`/`version`
#   (Claude-Code-native Agent Skill), the builtin side additionally needed
#   `triggers` (explicit regexes) + `requires_isolation` for
#   markdown_skills.go's own regex-triggered REPL auto-trigger matcher —
#   running this script's regenerate mode against that source PROVABLY
#   broke TestGovernanceSkills_RegisteredAndInvocable (all 7 skills lost
#   their triggers).
#
#   RESOLVED same day via the follow-up architectural-options packet
#   (docs/research/hxc159_f13_architectural_options_20260923.md, Option C —
#   superset schema): constitution/skills/<name>/SKILL.md's frontmatter now
#   OPTIONALLY carries `triggers:` and `requires_isolation:` alongside the
#   native `name`/`description`/`version` fields. YAML ignores unknown keys
#   in either direction, so ONE file now genuinely serves BOTH consumers —
#   Claude Code's own semantic-description-driven discovery (reads
#   name/description/version, ignores triggers/requires_isolation) AND this
#   loader's regex-trigger REPL auto-completion (reads description/
#   triggers/requires_isolation, ignores name/version). constitution/skills/
#   is therefore the SOLE source of truth; builtin_skills/ is a pure,
#   verified-safe GENERATED copy. Regenerating (no flags) is now safe and
#   GREEN-tested (TestGovernanceSkills_RegisteredAndInvocable AND the
#   dedicated drift-regression guard TestBuiltinSkills_NoDrift both pass
#   post-regeneration). The `triggers`/`requires_isolation` fields for the
#   7 migrated skills were carried forward VERBATIM from their pre-superset
#   builtin_skills copies — never invented (§11.4.6) — so REPL trigger-
#   phrase matching behaviour is byte-identical to before; only the
#   `description` + body content changed (now sourced from the richer,
#   kept-current constitution documentation instead of a separately-
#   maintained short REPL template), which is the intended fix for the
#   staleness risk the original F-13 investigation flagged.
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
