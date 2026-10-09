#!/usr/bin/env bash
# scripts/testing/sync_all_markdown_exports.sh
# §11.4.65 / CONST-066 — canonical markdown→{.html,.pdf} sibling exporter.
# §11.4.168 — Mermaid-aware: diagrams render as IMAGES, never raw source text.
#
# Every governed/working doc under docs/**/*.md SHALL ship synchronized
# .html + .pdf siblings. This helper renders those siblings from the .md
# source with a single, depth-independent pipeline:
#
#   mmdc (mermaid-cli, OPTIONAL) : any ```mermaid fence is rendered to a PNG
#             and swapped for an image reference BEFORE pandoc ever sees the
#             doc — this is what keeps raw `flowchart TD` / `sequenceDiagram`
#             / `gantt` / ... source text out of the shipped HTML/PDF
#             (§11.4.168). Applied PER-DIAGRAM: one bad fence never blocks
#             the rest of the doc — a per-block mmdc failure falls back to
#             embedding that single block's raw source with an honest
#             §11.4.6 WARN on stderr, never a silent or hook-breaking
#             failure. Docs with no ```mermaid fence, or a host with mmdc
#             absent, take the pre-existing byte-identical pandoc path — this
#             degrades gracefully and NEVER hard-fails a commit.
#   pandoc  : .md → self-contained .html (CSS embedded inline via
#             --embed-resources, so the HTML renders identically no matter
#             how deeply nested the source doc is — no relative-CSS breakage;
#             --embed-resources also base64-inlines any Mermaid-rendered PNG
#             referenced above, so the HTML stays a single self-contained
#             file).
#   weasyprint : that .html → .pdf  (timeout-bounded; warnings are benign).
#
# Idempotent: a sibling is only (re)rendered when missing or stale (source
# .md newer than the sibling), unless --regenerate-all forces a full pass.
#
# Modes:
#   (default)            render missing/stale siblings for the paths given
#                        (or the whole docs/ tree if no paths given).
#   --check-only         report the docs missing a sibling; render nothing;
#                        exit 1 if any are missing, 0 if all present.
#   --regenerate-all     force re-render every sibling (ignore staleness).
#   --file <path.md>     restrict to one or more explicit .md files
#                        (repeatable; may also be passed as bare args).
#
# Exit: 0 = all requested siblings present/rendered; 1 = (check-only) gaps
#       found, or a render failed.
#
# Dependencies: pandoc (mandatory), weasyprint (mandatory for .pdf), find.
#   mmdc / @mermaid-js/mermaid-cli (OPTIONAL — only needed for docs that
#   actually contain ```mermaid fences; its absence is a graceful, warned
#   degrade per §11.4.6, never a fatal error).
# Cross-references: §11.4.65 / CONST-066 / §11.4.75 (the pre-commit gate
#   that consumes these siblings); §11.4.168 (exported-doc content+textual+
#   visual validation — the mandate this Mermaid-awareness satisfies);
#   scripts/git_hooks/pre-commit.

set -uo pipefail

REPO_ROOT="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && cd .. && pwd)"
cd "$REPO_ROOT" || exit 2

DOCS_DIR="$REPO_ROOT/docs"
CSS="$DOCS_DIR/_progress-style.css"
TIMEOUT_SECS=60

CHECK_ONLY=0
REGEN_ALL=0
declare -a EXPLICIT=()

while [ $# -gt 0 ]; do
  case "$1" in
    --check-only)     CHECK_ONLY=1 ;;
    --regenerate-all) REGEN_ALL=1 ;;
    --file)           shift; [ $# -gt 0 ] && EXPLICIT+=("$1") ;;
    --timeout)        shift; [ $# -gt 0 ] && TIMEOUT_SECS="$1" ;;
    -*)               echo "unknown flag: $1" >&2; exit 2 ;;
    *)                EXPLICIT+=("$1") ;;
  esac
  shift
done

command -v pandoc >/dev/null 2>&1 || { echo "FATAL: pandoc not installed" >&2; exit 2; }
HAVE_WEASY=1
command -v weasyprint >/dev/null 2>&1 || HAVE_WEASY=0

# §11.4.168 Mermaid-awareness. OPTIONAL dependency — its absence degrades
# gracefully (raw Mermaid source ships, honestly warned) rather than
# blocking the html/pdf pipeline or breaking the §11.4.75 commit hook.
HAVE_MMDC=1
command -v mmdc >/dev/null 2>&1 || HAVE_MMDC=0

# Browser resolution for mmdc (§11.4.6: probe, never assume).
#
# mmdc drives headless Chromium via puppeteer. Two things routinely break that
# on a hardened host, and both were observed here:
#
#   1. puppeteer's bundled browser is often absent, because npm refuses to run
#      its postinstall script without an explicit --allow-scripts.
#   2. Even with a browser present, Chromium REFUSES TO START on distros that
#      disable unprivileged user namespaces via AppArmor (Ubuntu 23.10+):
#        FATAL zygote_host_impl_linux.cc: No usable sandbox!
#
# Either one makes every Mermaid block fall back to RAW SOURCE in the shipped
# HTML/PDF -- the exact §11.4.168 defect this pipeline exists to prevent
# (measured: 19 lines of readable `graph LR` source and 0 embedded images in a
# generated PDF).
#
# We therefore (a) reuse an already-installed Chromium when puppeteer has none,
# preferring an operator override, and (b) pass --no-sandbox.
#
# On --no-sandbox: this Chromium renders ONLY Mermaid source already committed
# to this repository's own Markdown. It opens no remote URL and consumes no
# untrusted input, so the sandbox is not standing between anything here. That is
# what makes the flag acceptable in THIS pipeline and not a general default.
MERMAID_PUPPETEER_CFG=""
if [ "$HAVE_MMDC" -eq 1 ] && [ -z "${PUPPETEER_EXECUTABLE_PATH:-}" ]; then
  for _cand in \
    "$HOME"/.cache/puppeteer/chrome/*/chrome-linux64/chrome \
    "$HOME"/.cache/ms-playwright/chromium-*/chrome-linux64/chrome \
    "$HOME"/.cache/ms-playwright/chromium_headless_shell-*/chrome-headless-shell-linux64/chrome-headless-shell \
    /usr/bin/chromium /usr/bin/chromium-browser /usr/bin/google-chrome; do
    if [ -x "$_cand" ]; then
      export PUPPETEER_EXECUTABLE_PATH="$_cand"
      break
    fi
  done
  unset _cand
fi

MERMAID_TMPROOT=""
if [ "$HAVE_MMDC" -eq 1 ]; then
  MERMAID_TMPROOT="$(mktemp -d "${TMPDIR:-/tmp}/sync_md_mermaid.XXXXXX")"
  trap 'rm -rf "$MERMAID_TMPROOT"' EXIT
  MERMAID_PUPPETEER_CFG="$MERMAID_TMPROOT/puppeteer.json"
  printf '{"args":["--no-sandbox","--disable-dev-shm-usage"]}\n' > "$MERMAID_PUPPETEER_CFG"
fi

# A doc "has Mermaid" if it contains a ```mermaid (or :::mermaid) fence.
has_mermaid() {
  # $1 = source.md
  grep -qE '^[[:space:]]*(```+|:::)[[:space:]]*mermaid[[:space:]]*$' "$1" 2>/dev/null
}

# Render every ```mermaid fence in $1 to a standalone PNG and rewrite $2 with
# each fence replaced by an image reference to that PNG (absolute path, so
# pandoc's --embed-resources can base64-inline it regardless of cwd). A
# per-block mmdc failure is NOT fatal: that one block's raw source is kept
# (with an honest §11.4.6 WARN to stderr) while every other block in the
# same doc still renders — one bad diagram never blocks the rest of the doc,
# and never blocks the doc from getting SOME sibling at all.
preprocess_mermaid() {
  # $1 = source.md  $2 = output effective .md  $3 = per-doc image workdir
  local src="$1" out="$2" workdir="$3"
  mkdir -p "$workdir"
  : > "$out"
  local in_block=0 idx=0 block_file="" line
  while IFS= read -r line || [ -n "$line" ]; do
    if [ "$in_block" -eq 0 ] \
       && printf '%s\n' "$line" | grep -qE '^[[:space:]]*(```+|:::)[[:space:]]*mermaid[[:space:]]*$'; then
      in_block=1
      idx=$((idx + 1))
      block_file="$workdir/block_${idx}.mmd"
      : > "$block_file"
      continue
    fi
    if [ "$in_block" -eq 1 ] \
       && printf '%s\n' "$line" | grep -qE '^[[:space:]]*(```+|:::)[[:space:]]*$'; then
      in_block=0
      local png="$workdir/block_${idx}.png"
      local mlog="$workdir/block_${idx}.mmdc.log"
      if mmdc -i "$block_file" -o "$png" -b white -p "$MERMAID_PUPPETEER_CFG" >"$mlog" 2>&1 && [ -f "$png" ]; then
        printf '\n![Mermaid diagram %d](%s)\n\n' "$idx" "$png" >> "$out"
      else
        echo "WARN §11.4.6/§11.4.168: mmdc failed to render Mermaid block #$idx in $src" \
             "— embedding RAW SOURCE for this block only (log: $mlog)." >&2
        printf '\n```mermaid\n' >> "$out"
        cat "$block_file" >> "$out"
        printf '```\n\n' >> "$out"
      fi
      continue
    fi
    if [ "$in_block" -eq 1 ]; then
      printf '%s\n' "$line" >> "$block_file"
      continue
    fi
    printf '%s\n' "$line" >> "$out"
  done < "$src"
}

# Return (on stdout) the markdown path pandoc should actually read for $1:
# the original file untouched (no Mermaid, or mmdc unavailable — the latter
# case prints an honest §11.4.6 warning), or a Mermaid-preprocessed copy.
effective_source() {
  # $1 = source.md
  local src="$1"
  if ! has_mermaid "$src"; then
    printf '%s' "$src"
    return 0
  fi
  if [ "$HAVE_MMDC" -eq 0 ]; then
    echo "WARN §11.4.6/§11.4.168: mmdc (mermaid-cli) not found on PATH — $src contains" \
         "Mermaid diagram(s) that will export as RAW SOURCE TEXT (violates §11.4.168)." \
         "Install: npm i -g @mermaid-js/mermaid-cli" >&2
    printf '%s' "$src"
    return 0
  fi
  local workdir="$MERMAID_TMPROOT/$(printf '%s' "$src" | tr '/' '_')"
  local out="$workdir/effective.md"
  if [ ! -f "$out" ]; then
    preprocess_mermaid "$src" "$out" "$workdir"
  fi
  printf '%s' "$out"
}

# --- §11.4.186 doc-integrity gate (anti-divergence enforcement) ---
# Runs BEFORE any export render — a FAIL here refuses the export.
CHECKSET="$REPO_ROOT/.helix_code/doc_integrity/checkset.yaml"
if [ -f "$CHECKSET" ] && command -v bash >/dev/null 2>&1; then
  if ! bash constitution/scripts/doc_integrity/wire/doc_integrity_gate.sh \
       "$CHECKSET" "$REPO_ROOT" --divergence-class-only; then
    # exit 1 means hard FAIL; exit 3 means SKIP (source unavailable — proceed
    # with warning). Only exit 1 aborts the render.
    rc=$?
    if [ "$rc" -eq 1 ]; then
      echo "FATAL §11.4.186: doc-integrity FAIL — export REFUSED. Fix cross-doc" \
           "divergences before exporting." >&2
      exit 1
    fi
  fi
else
  echo "WARN §11.4.186/§11.4.3: checkset not found at $CHECKSET — gate skipped" >&2
fi
# --- end doc-integrity gate ---

# Collect the .md source set.
declare -a SOURCES=()
if [ "${#EXPLICIT[@]}" -gt 0 ]; then
  for p in "${EXPLICIT[@]}"; do
    case "$p" in *.md) [ -f "$p" ] && SOURCES+=("$p") ;; esac
  done
else
  while IFS= read -r f; do SOURCES+=("$f"); done \
    < <(find "$DOCS_DIR" -name '*.md' -type f | sort)
fi

# A sibling is "stale" if missing OR older than the source .md.
stale() {
  # $1 = source.md  $2 = sibling
  [ ! -f "$2" ] && return 0
  [ "$1" -nt "$2" ] && return 0
  return 1
}

missing=0
rendered=0
failed=0
declare -a MISSING_LIST=()

for src in "${SOURCES[@]}"; do
  base="${src%.md}"
  html="${base}.html"
  pdf="${base}.pdf"

  if [ "$CHECK_ONLY" -eq 1 ]; then
    if [ ! -f "$html" ] || [ ! -f "$pdf" ]; then
      missing=$((missing+1)); MISSING_LIST+=("$src")
    fi
    continue
  fi

  need_html=0; need_pdf=0
  if [ "$REGEN_ALL" -eq 1 ]; then need_html=1; need_pdf=1
  else
    stale "$src" "$html" && need_html=1
    { [ "$HAVE_WEASY" -eq 1 ] && stale "$src" "$pdf"; } && need_pdf=1
  fi

  title="$(basename "$base")"

  # §11.4.168: Mermaid-preprocessed only when needed and only computed once
  # per doc, even though it may be consulted from both the html and the pdf
  # (html-fallback) branches below.
  effsrc="$src"
  if [ "$need_html" -eq 1 ] || [ "$need_pdf" -eq 1 ]; then
    effsrc="$(effective_source "$src")"
  fi

  if [ "$need_html" -eq 1 ]; then
    # Primary render. If pandoc trips on a stray `---` it mistakes for a
    # YAML metadata block, retry with yaml_metadata_block disabled so the
    # `---` is treated as a literal horizontal rule (the doc is content,
    # not front-matter).
    if pandoc "$effsrc" -s --embed-resources --standalone \
         --metadata title="$title" --css "$CSS" -o "$html" 2>/dev/null \
       || pandoc "$effsrc" -f markdown-yaml_metadata_block -s --embed-resources \
         --standalone --metadata title="$title" --css "$CSS" -o "$html" 2>/dev/null; then
      rendered=$((rendered+1))
    else
      echo "FAIL html: $src" >&2; failed=$((failed+1)); continue
    fi
  fi

  if [ "$need_pdf" -eq 1 ] && [ "$HAVE_WEASY" -eq 1 ]; then
    # HTML must exist (render it if it was up-to-date but we still need pdf).
    [ -f "$html" ] || pandoc "$effsrc" -s --embed-resources --standalone \
         --metadata title="$title" --css "$CSS" -o "$html" 2>/dev/null \
      || pandoc "$effsrc" -f markdown-yaml_metadata_block -s --embed-resources \
         --standalone --metadata title="$title" --css "$CSS" -o "$html" 2>/dev/null
    if timeout "$TIMEOUT_SECS" weasyprint "$html" "$pdf" >/dev/null 2>&1; then
      rendered=$((rendered+1))
    else
      _wp_rc=$?
      # FALSE-SUCCESS FIX (2026-09-08, §11.4.201).
      #
      # This fallback used to ask `[ -f "$pdf" ]`. Existence is NOT freshness:
      # when weasyprint TIMED OUT, the STALE pdf from a previous run was still
      # on disk, so the run counted it as `rendered`. Measured: a batch of 8
      # documents reported `rendered=16 failed=0` while 2 pdfs were never
      # written and kept an eight-day-old mtime — and because those two were
      # regenerations of a CREDENTIAL SCRUB, the stale copies went on handing
      # the credential over under a green summary line. Absence of a fresh
      # render was indistinguishable from success.
      #
      # A timeout is now ALWAYS a failure (124 is timeout(1)'s own code), and
      # the fallback requires the pdf to be NEWER than the html it renders,
      # never merely present.
      if [ "$_wp_rc" -eq 124 ]; then
        echo "FAIL pdf (timeout after ${TIMEOUT_SECS}s): $src" >&2; failed=$((failed+1))
      elif [ -f "$pdf" ] && [ ! "$pdf" -ot "$html" ]; then
        # weasyprint emits benign warnings and can exit non-zero while still
        # having written a CURRENT pdf; that case is a genuine pass.
        rendered=$((rendered+1))
      else
        echo "FAIL pdf: $src" >&2; failed=$((failed+1))
      fi
    fi
  fi
done

if [ "$CHECK_ONLY" -eq 1 ]; then
  if [ "$missing" -gt 0 ]; then
    echo "MISSING SIBLINGS ($missing):" >&2
    printf '  %s\n' "${MISSING_LIST[@]}" >&2
    exit 1
  fi
  echo "all docs/**/*.md have .html + .pdf siblings"
  exit 0
fi

echo "rendered=$rendered failed=$failed"
[ "$failed" -gt 0 ] && exit 1
exit 0
