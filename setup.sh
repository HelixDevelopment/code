#!/usr/bin/env bash
#
# HelixCode Setup — installs the ENTIRE Helix platform.
#
# This is the single entry point. It initialises submodules, installs system
# dependencies, builds every sub-system (HelixCode, HelixAgent, HelixLLM), and
# installs the systemd *user* units so the whole platform boots with the host
# and survives restarts.
#
# Usage:
#   ./setup.sh                 # install + build + install systemd units (no start)
#   ./setup.sh --start         # ... and start the platform immediately
#   ./setup.sh --no-systemd    # build only; skip systemd installation
#   ./setup.sh --skip-build    # wire systemd only; assume binaries already built
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "${REPO_ROOT}"

DO_SYSTEMD=1
DO_BUILD=1
START_FLAG=""
for arg in "$@"; do
  case "$arg" in
    --start)      START_FLAG="--start" ;;
    --no-systemd) DO_SYSTEMD=0 ;;
    --skip-build) DO_BUILD=0 ;;
    -h|--help)    sed -n '2,16p' "$0"; exit 0 ;;
    *) echo "unknown option: $arg (try --help)" >&2; exit 2 ;;
  esac
done

section() { printf '\n\033[1m==> %s\033[0m\n' "$*"; }
ok()      { printf '  \033[32m✓\033[0m %s\n' "$*"; }
warn()    { printf '  \033[33m!\033[0m %s\n' "$*"; }
die()     { printf '  \033[31m✗\033[0m %s\n' "$*" >&2; exit 1; }

echo "=================================="
echo "  Helix Platform Setup"
echo "=================================="
echo "  root: ${REPO_ROOT}"

command -v git >/dev/null 2>&1 || die "git is not installed."
command -v go  >/dev/null 2>&1 || die "go is not installed (required to build all sub-systems)."

# --- 1. submodules -----------------------------------------------------------
section "Initialising git submodules"
./scripts/init-submodules.sh
ok "submodules ready"

# --- 2. git hooks ------------------------------------------------------------
section "Installing local git hooks (CONST-042 enforcement)"
./scripts/install-git-hooks.sh
ok "git hooks installed"

# --- 3. system dependencies --------------------------------------------------
section "Installing system dependencies"
case "$OSTYPE" in
  linux-gnu*)
    if [ -x ./install_missing_libs.sh ]; then
      ./install_missing_libs.sh
      ok "system libraries installed"
    else
      warn "install_missing_libs.sh not found or not executable — skipping"
    fi
    ;;
  darwin*)
    warn "macOS: ensure Xcode Command Line Tools are present (xcode-select --install)"
    ;;
  *)
    warn "unrecognised OS '$OSTYPE' — install dependencies manually"
    ;;
esac

# --- 4. build every sub-system ----------------------------------------------
# Each sub-system owns its own Makefile; setup.sh calls into them rather than
# duplicating build logic (§11.4.74 extend-don't-reimplement).
if [ "${DO_BUILD}" -eq 1 ]; then
  section "Building HelixCode (main application)"
  make -C helix_code build
  ok "helix_code/bin/helixcode"

  section "Building HelixAgent"
  if [ -f submodules/helix_agent/Makefile ]; then
    make -C submodules/helix_agent build
    ok "submodules/helix_agent/bin/helixagent"
  else
    warn "submodules/helix_agent not initialised — skipping (run scripts/init-submodules.sh)"
  fi

  section "Building LLMsVerifier"
  # The real Go module lives one level down at llms_verifier/llm-verifier/
  # (module digital.vasic.llmsverifier); the submodule root is a thin wrapper.
  if [ -f submodules/llms_verifier/llm-verifier/go.mod ]; then
    ( cd submodules/llms_verifier/llm-verifier && go build -o bin/llm-verifier ./cmd )
    ok "submodules/llms_verifier/llm-verifier/bin/llm-verifier"
  else
    warn "submodules/llms_verifier not initialised — skipping"
  fi

  section "Building HelixLLM"
  if [ -f submodules/helix_llm/Makefile ]; then
    make -C submodules/helix_llm build
    # The gateway unit resolves `helixllm` from PATH, so publish it to
    # ~/.local/bin rather than leaving it only in the submodule's bin/.
    mkdir -p "${HOME}/.local/bin"
    install -m 0755 submodules/helix_llm/bin/helixllm "${HOME}/.local/bin/helixllm"
    ok "~/.local/bin/helixllm"
    case ":${PATH}:" in
      *":${HOME}/.local/bin:"*) : ;;
      *) warn "~/.local/bin is not on PATH — add it to your shell profile" ;;
    esac
  else
    warn "submodules/helix_llm not initialised — skipping"
  fi

  # Colibri (optional GLM-class local engine, W2a-1): pure-C build, no Go
  # toolchain needed beyond gcc with OpenMP. The launcher contract is
  # HELIX_COLIBRI_BIN -> the repo's `coli` launcher (see task W2a-2); the
  # engine binary this step produces lives at c/colibri per R1 research.
  section "Building Colibri engine (optional)"
  if [ ! -d dependencies/colibri/c ]; then
    warn "dependencies/colibri not initialised — skipping (run scripts/init-submodules.sh)"
  elif [ -x dependencies/colibri/c/colibri ]; then
    ok "dependencies/colibri/c/colibri already built"
  elif ! command -v gcc >/dev/null 2>&1; then
    warn "SKIP-OK: #W2a-1 gcc not available — colibri engine not built (optional component)"
  else
    make -C dependencies/colibri glm
    ok "dependencies/colibri/c/colibri"
  fi
else
  section "Skipping builds (--skip-build)"
fi

# --- 5. secrets --------------------------------------------------------------
# Never generated with real values, never committed (CONST-042 / §12.1).
section "Checking environment file"
# gen_secret prints a fresh high-entropy value. openssl is preferred; the
# /dev/urandom path is the portable fallback so setup never silently degrades
# to a weak or fixed value (CONST-042: a predictable secret is not a secret).
gen_secret() {
  if command -v openssl >/dev/null 2>&1; then
    openssl rand -hex 24
    return
  fi
  if [ -r /dev/urandom ]; then
    LC_ALL=C tr -dc 'a-f0-9' < /dev/urandom | head -c 48
    echo
    return
  fi
  die "cannot generate a secret: neither openssl nor /dev/urandom is available"
}

# fill_placeholder replaces a CHANGE_ME_* placeholder in .env with a generated
# secret. It ONLY ever rewrites the placeholder, so re-running setup.sh never
# clobbers a real value the operator (or a previous run) already set.
fill_placeholder() {
  key="$1"; placeholder="$2"
  if grep -q "^${key}=${placeholder}\$" .env 2>/dev/null; then
    secret="$(gen_secret)"
    # Generated values are hex only, so they carry no sed metacharacters.
    sed -i.bak "s|^${key}=${placeholder}\$|${key}=${secret}|" .env
    rm -f .env.bak
    unset secret
    ok "${key}: generated a unique local secret"
  fi
}

if [ ! -f .env ] && [ -f .env.example ]; then
  cp .env.example .env
  chmod 0600 .env
  ok "created .env from .env.example (mode 0600)"
elif [ -f .env ]; then
  chmod 0600 .env
  ok ".env present (mode 0600)"
else
  warn "no .env and no .env.example — services needing secrets may fail dependency verification"
fi

# HXC-168 / CONST-042: the container and setup files no longer carry any
# credential literal — they read HELIX_DATABASE_PASSWORD (and friends) from this
# gitignored .env. Generating a unique per-install value here is what keeps the
# platform working out of the box WITHOUT a shared, published default.
if [ -f .env ]; then
  fill_placeholder HELIX_DATABASE_PASSWORD CHANGE_ME_db_password
  fill_placeholder HELIX_REDIS_PASSWORD    CHANGE_ME_redis_password
  fill_placeholder HELIX_AUTH_JWT_SECRET   CHANGE_ME_jwt_secret
  if grep -q '=CHANGE_ME' .env 2>/dev/null; then
    warn ".env still has CHANGE_ME placeholders (provider API keys) — fill them in before using those providers"
  fi
fi

# --- 6. systemd --------------------------------------------------------------
if [ "${DO_SYSTEMD}" -eq 1 ]; then
  section "Installing systemd user units (boot-persistent)"
  ./scripts/install_systemd_units.sh ${START_FLAG}
else
  section "Skipping systemd installation (--no-systemd)"
fi

# --- done --------------------------------------------------------------------
cat <<EOF

==================================
  ✅ Setup complete
==================================

The platform is installed as systemd USER units and will start on boot.

  Start everything   : systemctl --user start helix.target
  Stop everything    : systemctl --user stop helix.target
  Status (one)       : systemctl --user status helixagent
  Status (all)       : systemctl --user list-units 'helix*'
  Logs               : journalctl --user -u helixagent -f

Services and ports:
  helixcode-server    :8080    HelixCode API
  helixllm-gateway    :8443    HelixLLM multi-provider router (TLS)
  helixagent          :7061    HelixAgent runtime
  llmsverifier        :8100    LLMsVerifier model/provider scoring API
  helixllm-coder      :18434   Local Qwen3-Coder model
  helixcode-infra     :5433 postgres  :6380 redis  :8083 weaviate
                      :8082 chromadb  :8000 cognee :6333 qdrant
                      :11434 ollama   :11211 memcached

EOF
