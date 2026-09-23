#!/usr/bin/env bash
# scripts/register_skills.sh — manifest-driven, idempotent, prefix-scoped skill
# registrar (HXC-159 T-P1.04; the registrar §11.4.164 calls and which was absent).
#
# ── Purpose ──────────────────────────────────────────────────────────────────
#   The ONE idempotent seam that provisions the constitution's skills into
#   <project>/.claude/skills/ BY REFERENCE (a relative symlink per skill, NEVER
#   a copy, per §11.4.28 / §11.4.80) and REFUSES, fail-closed, any filesystem
#   collision it did not itself create. Re-running is safe: re-run in either
#   install order yields a byte-identical tree (§11.4.50 / §11.4.77 — this script
#   IS the regeneration mechanism for the gitignored .claude/skills/ directory).
#
# ── Usage ────────────────────────────────────────────────────────────────────
#   bash scripts/register_skills.sh [--root DIR] [--manifest FILE] [--check]
#     --root DIR       project root to provision (default: git toplevel, else CWD)
#     --manifest FILE  manifest to register from (default: scripts/skills_manifest.tsv)
#     --check          report state, change nothing (exit 0 = fully wired)
#     -h|--help        print this header and exit
#
# ── Manifest schema ──────────────────────────────────────────────────────────
#   One row per skill, three TAB-separated fields, no header line:
#     name<TAB>source<TAB>owner
#     name    basename under .claude/skills/; ^[a-z0-9][a-z0-9_-]*$ (lowercase)
#     source  project-root-relative directory holding the exact-case SKILL.md
#     owner   registrar allowed to write this name; only `constitution` here.
#   Blank lines and `#`-comment lines are ignored. Rows are applied in
#   byte-sorted name order so output is independent of file order (determinism).
#
# ── Reserved-prefix enforcement ──────────────────────────────────────────────
#   This registrar writes ONLY names listed in the manifest with
#   owner=constitution. The `speckit-*` prefix is owned by SpecKit: any manifest
#   row with that prefix, any other owner, any duplicate name, or any source
#   without an exact-case SKILL.md is REFUSED with a non-zero exit BEFORE any
#   write happens (validated whole-manifest-first, so a refusal changes nothing).
#
# ── Pre-write enumeration ────────────────────────────────────────────────────
#   Before writing, every existing .claude/skills/ entry (excluding the
#   registrar's own .manifest/ dir, lock file and temp files) must carry the
#   registrar's marker (.claude/skills/.manifest/<name>, content = source).
#   A foreign entry aborts the run non-zero with the tree unchanged — NEVER
#   overwrite. Stale own entries (marker present, name no longer in the
#   manifest) are removed so the tree regenerates deterministically.
#
# ── Concurrency & crash recovery ─────────────────────────────────────────────
#   A flock(1) advisory lock serialises concurrent invocations. Every link and
#   marker is written temp-then-rename (trap-cleaned); a SIGKILL mid-run leaves
#   only `.tmp.register.*` residue, which the next run reaps before doing
#   anything else — so the next run always recovers to a consistent tree.
#
# ── §11.4.8 declaration — NO external solution found, original work ──────────
#   Claude Code NEITHER errors NOR warns on a skill-name collision; it resolves
#   silently, and the upstream plugin-skill namespacing request was CLOSED AS
#   NOT PLANNED. The collision faced here happens in the FILESYSTEM, when two
#   independent installers provision into one .claude/skills/ tree — BEFORE
#   Claude Code ever reads the directory. No upstream mechanism observes it, so
#   this install-time collision detector is ORIGINAL WORK; its absence would be
#   a silent-shadowing defect no test elsewhere could catch.
#
# ── Inputs ───────────────────────────────────────────────────────────────────
#   The manifest file (default scripts/skills_manifest.tsv).
#
# ── Outputs ──────────────────────────────────────────────────────────────────
#   <root>/.claude/skills/<name>            relative symlink per manifest row
#   <root>/.claude/skills/.manifest/<name>  marker file (content = source row)
#   stdout: linked/removed/unchanged counts + a WIRED / NOT-WIRED verdict.
#
# ── Side-effects ─────────────────────────────────────────────────────────────
#   Creates <root>/.claude/skills/ + .manifest/. Takes a flock on
#   .claude/skills/.register.lock during registration. Never writes outside
#   the provisioned root. Never touches the live tree when --root points
#   elsewhere (sandbox runs stay sandboxed).
#
# ── Dependencies ─────────────────────────────────────────────────────────────
#   bash, ln, mv, mkdir, rm, sort, flock(1);
#   constitution/scripts/portable_symlink_lib.sh (hc_relpath — sourced BY
#   REFERENCE, never duplicated here).
#
# ── Cross-references ─────────────────────────────────────────────────────────
#   §11.4.164 (auto-propagation calls this registrar), §11.4.28 / §11.4.80 /
#   §11.4.177 (by reference, never copied), §11.4.50 (determinism),
#   §11.4.77 (regeneration mechanism), §11.4.111 (relative, relocatable links),
#   §11.4.6 (refuse rather than guess), §11.4.8 (original-work declaration).
#
# Classification: project-specific (§11.4.17) — this consumer's registrar.
# Last verified: 2026-09-13
set -euo pipefail

SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
REPO_DEFAULT="$(git rev-parse --show-toplevel 2>/dev/null || printf '%s' "$PWD")"

root="$REPO_DEFAULT"
manifest="$SCRIPT_DIR/skills_manifest.tsv"
check_only=0

while [ $# -gt 0 ]; do
  case "$1" in
    --root)     shift; root="${1:?--root requires a directory}" ;;
    --manifest) shift; manifest="${1:?--manifest requires a file}" ;;
    --check)    check_only=1 ;;
    -h|--help)  sed -n '2,60p' "$0"; exit 0 ;;
    *) echo "[register-skills] ERROR: unknown argument: $1" >&2; exit 2 ;;
  esac
  shift || true
done

root="$(cd -- "$root" >/dev/null 2>&1 && pwd)" || { echo "[register-skills] ERROR: root not a directory: $root" >&2; exit 2; }
[ -f "$manifest" ] || { echo "[register-skills] ERROR: manifest missing: $manifest" >&2; exit 2; }

SKILLS_DST="$root/.claude/skills"
MARK_DIR="$SKILLS_DST/.manifest"
LOCK_FILE="$SKILLS_DST/.register.lock"
TMP_PREFIX=".tmp.register.$$"

LIB="$(cd -- "$SCRIPT_DIR/../constitution/scripts" >/dev/null 2>&1 && pwd)/portable_symlink_lib.sh"
[ -f "$LIB" ] || { echo "[register-skills] ERROR: link library missing: $LIB" >&2; exit 1; }
# shellcheck disable=SC1090
. "$LIB"

ok()   { printf '[register-skills] %s\n' "$*"; }
fail() { printf '[register-skills] ERROR: %s\n' "$*" >&2; }

# ── Manifest validation (whole-manifest-first: any refusal precedes any write) ──
ROWS="$(mktemp)"
trap 'rm -f "$ROWS"' EXIT
lineno=0
while IFS= read -r line || [ -n "$line" ]; do
  lineno=$((lineno + 1))
  case "$line" in ''|\#*) continue ;; esac
  name="$(printf '%s' "$line" | cut -f1)"
  source="$(printf '%s' "$line" | cut -f2)"
  owner="$(printf '%s' "$line" | cut -f3)"
  extra="$(printf '%s' "$line" | cut -f4)"
  [ -n "$name" ] && [ -n "$source" ] && [ -n "$owner" ] && [ -z "$extra" ] || {
    fail "manifest line $lineno: expected exactly 3 TAB fields (name/source/owner)"; exit 1; }
  case "$name" in
    speckit-*) fail "manifest line $lineno: '$name' uses the SpecKit-reserved prefix (refusing)"; exit 1 ;;
  esac
  case "$name" in
    *[!a-z0-9_-]*|_*|-*|"") fail "manifest line $lineno: bad name '$name'"; exit 1 ;;
  esac
  [ "$owner" = "constitution" ] || {
    fail "manifest line $lineno: owner '$owner' is not this registrar's (refusing)"; exit 1; }
  case "$source" in /*|*../*) fail "manifest line $lineno: bad source '$source'"; exit 1 ;; esac
  [ -d "$root/$source" ] || {
    fail "manifest line $lineno: source dir missing: $root/$source"; exit 1; }
  if ! ls -a "$root/$source" 2>/dev/null | grep -x -q 'SKILL.md'; then
    fail "manifest line $lineno: no exact-case SKILL.md in $root/$source"; exit 1
  fi
  printf '%s\t%s\n' "$name" "$source" >> "$ROWS"
done < "$manifest"
trap - EXIT

dupes="$(cut -f1 "$ROWS" | sort | uniq -d)"
[ -z "$dupes" ] || { fail "duplicate manifest name(s): $dupes"; rm -f "$ROWS"; exit 1; }
SORTED="$(mktemp)"; sort -t "$(printf '\t')" -k1,1 "$ROWS" > "$SORTED"; rm -f "$ROWS"
[ -s "$SORTED" ] || { fail "manifest declares zero skills"; rm -f "$SORTED"; exit 1; }

is_managed() { grep -q "$(printf '^%s\t' "$1")" "$SORTED"; }

# True iff symlink $2 (basename $1, a manifest-owned name) already resolves
# to exactly the target this registrar would itself write for that name.
# Used to silently adopt a pre-existing correct link left by another
# mechanism (e.g. the §11.4.272 activation engine) instead of treating it
# as a foreign writer — it IS what we would write, marker or not.
target_matches() {
  name="$1"; entry="$2"
  src="$(grep "$(printf '^%s\t' "$name")" "$SORTED" | cut -f2)"
  [ -n "$src" ] || return 1
  resolved="$(cd -- "$SKILLS_DST" >/dev/null 2>&1 && cd -- "$(dirname "$(readlink "$entry")")" >/dev/null 2>&1 && pwd)/$(basename "$(readlink "$entry")")" 2>/dev/null || resolved="UNRESOLVABLE"
  [ "$resolved" = "$root/$src" ]
}

# ── Tree enumeration (read-only; shared by --check and register) ─────────────
# Prints FOREIGN entries: names this registrar itself is trying to manage
# that are occupied by something other than what it would write itself.
# A name NEVER in our manifest is another tool's/the operator's — per
# T-P1.04.3 (other tools/prefixes own their own reserved names) it is never
# our concern: we coexist peacefully, we never touch it and never complain
# about it (F-04 fix — T-P1.04.4 as originally implemented contradicted
# T-P1.04.3 by aborting on ANY unmanaged name merely being present).
find_foreign() {
  [ -d "$SKILLS_DST" ] || return 0
  for entry in "$SKILLS_DST"/* "$SKILLS_DST"/.*; do
    base="$(basename "$entry")"
    case "$base" in .|..|.manifest|.register.lock|.tmp.register.*) continue ;; esac
    [ -e "$entry" ] || [ -L "$entry" ] || continue
    # Not one of our own manifest names — not our concern, never foreign.
    is_managed "$base" || continue
    # `base` IS a name we ourselves intend to write. It is a genuine
    # collision UNLESS it is already exactly what we would write ourselves:
    #   (a) already carries our marker and is a symlink (steady state), or
    #   (b) is an unmarked symlink that already resolves to our intended
    #       target (a pre-existing correct link from elsewhere, silently
    #       adopted rather than treated as a foreign writer).
    # A non-symlink, or a symlink pointing anywhere else, is a real
    # collision and MUST still abort — NEVER overwritten.
    if [ -f "$MARK_DIR/$base" ]; then
      [ -L "$entry" ] || printf '%s\n' "$base"
      continue
    fi
    if [ -L "$entry" ] && target_matches "$base" "$entry"; then
      continue
    fi
    printf '%s\n' "$base"
  done
}

tmp_residue() {
  [ -d "$SKILLS_DST" ] || return 0
  for t in "$SKILLS_DST"/.tmp.register.*; do
    [ -e "$t" ] || [ -L "$t" ] || continue
    printf '%s\n' "$t"
  done
}

# ── --check: report the wiring state, change nothing ─────────────────────────
if [ "$check_only" -eq 1 ]; then
  rc=0
  foreign="$(find_foreign)"
  if [ -n "$foreign" ]; then
    fail "foreign entr(ies) this registrar did not write: $foreign"; rc=1
  fi
  while IFS="$(printf '\t')" read -r name source; do
    dst="$SKILLS_DST/$name"
    if [ ! -L "$dst" ]; then ok "missing/non-link: $name"; rc=1; continue; fi
    resolved="$(cd -- "$SKILLS_DST" >/dev/null 2>&1 && cd -- "$(dirname "$(readlink "$dst")")" >/dev/null 2>&1 && pwd)/$(basename "$(readlink "$dst")")" || resolved="UNRESOLVABLE"
    if [ "$resolved" != "$root/$source" ]; then ok "mispointed: $name"; rc=1; continue; fi
    [ -f "$MARK_DIR/$name" ] && [ "$(cat "$MARK_DIR/$name")" = "$source" ] || { ok "unmarked: $name"; rc=1; }
  done < "$SORTED"
  # WARN-1 fix: --check must also see stale own pairs (marker present, name
  # absent from the manifest) — mirrors the :271 removal loop read-only.
  for mark in "$MARK_DIR"/*; do
    [ -f "$mark" ] || continue
    base="$(basename "$mark")"
    case "$base" in .tmp.register.*) continue ;; esac
    is_managed "$base" || { ok "stale: $base (marker present, name not in manifest — re-run to reap)"; rc=1; }
  done
  residue="$(tmp_residue)"
  [ -z "$residue" ] || { fail "stale temp residue: $residue"; rc=1; }
  rm -f "$SORTED"
  [ "$rc" -eq 0 ] && ok "VERDICT: WIRED" || fail "VERDICT: NOT-WIRED"
  exit "$rc"
fi

# ── Register: lock, re-validate tree state, then write ───────────────────────
command -v flock >/dev/null 2>&1 || { fail "flock(1) missing — refusing concurrent-unsafe run"; rm -f "$SORTED"; exit 1; }
dst_existed=0; [ -d "$SKILLS_DST" ] && dst_existed=1
mark_existed=0; [ -d "$MARK_DIR" ] && mark_existed=1
lock_existed=0; [ -e "$LOCK_FILE" ] && lock_existed=1
mkdir -p "$SKILLS_DST" "$MARK_DIR"
exec 9>"$LOCK_FILE"
flock 9 || { fail "cannot acquire lock $LOCK_FILE"; rm -f "$SORTED"; exit 1; }

cleanup_tmp() { rm -f "$SKILLS_DST"/.tmp.register.$$.* "$MARK_DIR"/.tmp.register.$$.*; }
trap cleanup_tmp EXIT

# Reap temp residue from killed runs BEFORE doing anything else (chaos recovery).
for stale in "$SKILLS_DST"/.tmp.register.* "$MARK_DIR"/.tmp.register.*; do
  [ -e "$stale" ] || [ -L "$stale" ] || continue
  case "$stale" in *".$TMP_PREFIX."*) continue ;; esac
  rm -f "$stale"
done

foreign="$(find_foreign)"
if [ -n "$foreign" ]; then
  fail "foreign entr(ies) present — aborting with tree unchanged: $foreign"
  # Roll back scaffolding WE created so the abort is byte-identical no-op.
  [ "$lock_existed" -eq 0 ] && rm -f "$LOCK_FILE"
  [ "$mark_existed" -eq 0 ] && rmdir "$MARK_DIR" 2>/dev/null
  [ "$dst_existed" -eq 0 ] && rmdir "$SKILLS_DST" 2>/dev/null
  rm -f "$SORTED"; exit 1
fi

linked=0; removed=0; unchanged=0
while IFS="$(printf '\t')" read -r name source; do
  dst="$SKILLS_DST/$name"
  # Relocation-proof target (reuse hc_relpath; refuse host-specific absolutes).
  rel="$(hc_relpath "$root/$source" "$SKILLS_DST")"
  top_dst="$(printf '%s' "$SKILLS_DST" | cut -d/ -f2)"
  top_src="$(printf '%s' "$root/$source" | cut -d/ -f2)"
  if [ "$top_dst" != "$top_src" ]; then
    fail "'$name': no shared root — refusing non-relocatable link"; rm -f "$SORTED"; exit 1
  fi
  case "$rel" in /*) fail "'$name': absolute target refused"; rm -f "$SORTED"; exit 1 ;; esac
  tmp="$SKILLS_DST/$TMP_PREFIX.$name"
  mark_tmp="$MARK_DIR/$TMP_PREFIX.$name"
  if [ -L "$dst" ] && [ -f "$MARK_DIR/$name" ] && [ "$(cat "$MARK_DIR/$name")" = "$source" ]; then
    resolved="$(cd -- "$SKILLS_DST" >/dev/null 2>&1 && cd -- "$(dirname "$(readlink "$dst")")" >/dev/null 2>&1 && pwd)/$(basename "$(readlink "$dst")")" || resolved="UNRESOLVABLE"
    if [ "$resolved" = "$root/$source" ] && [ -e "$dst/SKILL.md" ]; then
      unchanged=$((unchanged + 1)); continue
    fi
  fi
  ln -s "$rel" "$tmp"
  # Atomic link replace WITHOUT descending into a symlink-to-dir (managed
  # links ARE symlinks-to-dirs, so plain `mv tmp dst` would move tmp INTO the
  # target on relink). GNU `mv -T` treats dst as a plain file; elsewhere fall
  # back to rm+mv (brief non-atomic window, still flock-serialised).
  if mv -T "$tmp" "$dst" 2>/dev/null; then :; else rm -f "$dst"; mv "$tmp" "$dst"; fi
  printf '%s\n' "$source" > "$mark_tmp"
  mv "$mark_tmp" "$MARK_DIR/$name"
  linked=$((linked + 1))
done < "$SORTED"

# Remove stale own entries (marker present, name left the manifest).
for mark in "$MARK_DIR"/*; do
  [ -f "$mark" ] || continue
  base="$(basename "$mark")"
  case "$base" in .tmp.register.*) continue ;; esac
  if ! is_managed "$base"; then
    rm -f "$SKILLS_DST/$base" "$mark"
    removed=$((removed + 1))
  fi
done

rm -f "$SORTED"
ok "done — linked=$linked removed=$removed unchanged=$unchanged (idempotent; re-run is byte-identical)"
