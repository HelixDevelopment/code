#!/bin/bash
# HelixCode Full Test Environment Setup Script
# This script sets up all infrastructure needed to run tests with ZERO skipped tests.
#
# Usage:
#   ./scripts/setup-full-test-env.sh
#   ./scripts/setup-full-test-env.sh --gpu  # Enable GPU support for Ollama
#   ./scripts/setup-full-test-env.sh --clean  # Clean up and restart

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_DIR="$(dirname "$SCRIPT_DIR")"
COMPOSE_FILE="$PROJECT_DIR/docker-compose.full-test.yml"
SSH_KEYS_DIR="$PROJECT_DIR/tests/infrastructure/ssh_keys"
ENV_FILE="$PROJECT_DIR/.env.full-test"

# HXC-168: docker-compose.full-test.yml credential values are env-sourced
# (`${HELIX_DATABASE_PASSWORD:?...}`) rather than hardcoded literals, so every
# `docker compose` / `podman-compose` invocation below needs HELIX_DATABASE_PASSWORD
# (and friends) exported into THIS shell first, or compose refuses with a
# "variable is not set" error. Export once, up front, before any compose call.
if [ ! -f "$ENV_FILE" ]; then
    echo "Error: $ENV_FILE not found (required — provides HELIX_DATABASE_PASSWORD/etc. for docker-compose.full-test.yml)" >&2
    exit 1
fi
set -a
# shellcheck disable=SC1090
. "$ENV_FILE"
set +a

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Parse arguments
ENABLE_GPU=false
CLEAN_START=false
PULL_MODEL=true

while [[ $# -gt 0 ]]; do
    case $1 in
        --gpu)
            ENABLE_GPU=true
            shift
            ;;
        --clean)
            CLEAN_START=true
            shift
            ;;
        --no-model)
            PULL_MODEL=false
            shift
            ;;
        --help)
            echo "Usage: $0 [options]"
            echo "Options:"
            echo "  --gpu       Enable GPU support for Ollama"
            echo "  --clean     Clean up existing containers and start fresh"
            echo "  --no-model  Skip pulling Ollama model"
            echo "  --help      Show this help message"
            exit 0
            ;;
        *)
            echo "Unknown option: $1"
            exit 1
            ;;
    esac
done

echo -e "${BLUE}=== HelixCode Full Test Environment Setup ===${NC}"
echo ""

# Check for Docker/Podman
if command -v docker &> /dev/null; then
    CONTAINER_CMD="docker"
    COMPOSE_CMD="docker compose"
elif command -v podman &> /dev/null; then
    CONTAINER_CMD="podman"
    COMPOSE_CMD="podman-compose"
else
    echo -e "${RED}Error: Neither Docker nor Podman is installed${NC}"
    exit 1
fi

echo -e "${GREEN}Using container runtime: $CONTAINER_CMD${NC}"

# Clean up if requested
if [ "$CLEAN_START" = true ]; then
    echo -e "${YELLOW}Cleaning up existing containers...${NC}"
    $COMPOSE_CMD -f "$COMPOSE_FILE" down -v --remove-orphans 2>/dev/null || true
    echo -e "${GREEN}Cleanup complete${NC}"
fi

# Step 1: Generate SSH keys if not exist
echo -e "${BLUE}Step 1: Setting up SSH keys...${NC}"
mkdir -p "$SSH_KEYS_DIR"

if [ ! -f "$SSH_KEYS_DIR/id_rsa" ]; then
    echo "Generating SSH test keys..."
    ssh-keygen -t rsa -b 4096 -f "$SSH_KEYS_DIR/id_rsa" -N "" -C "helixcode-test"
    echo -e "${GREEN}SSH keys generated${NC}"
else
    echo -e "${GREEN}SSH keys already exist${NC}"
fi

# Ensure proper permissions
chmod 600 "$SSH_KEYS_DIR/id_rsa" 2>/dev/null || true
chmod 644 "$SSH_KEYS_DIR/id_rsa.pub" 2>/dev/null || true

# Step 2: Check GPU availability
if [ "$ENABLE_GPU" = true ]; then
    echo -e "${BLUE}Step 2: Checking GPU availability...${NC}"
    if command -v nvidia-smi &> /dev/null; then
        echo -e "${GREEN}NVIDIA GPU detected${NC}"
        nvidia-smi --query-gpu=name --format=csv,noheader 2>/dev/null || true
    else
        echo -e "${YELLOW}Warning: GPU requested but nvidia-smi not found. GPU may not work.${NC}"
    fi
else
    echo -e "${BLUE}Step 2: GPU support disabled (use --gpu to enable)${NC}"
    # Modify compose file to remove GPU requirements
    export COMPOSE_PROFILES=""
fi

# Step 3: Start infrastructure
echo -e "${BLUE}Step 3: Starting test infrastructure...${NC}"
$COMPOSE_CMD -f "$COMPOSE_FILE" up -d

# Step 4: Wait for services to be healthy
echo -e "${BLUE}Step 4: Waiting for services to be healthy...${NC}"

wait_for_service() {
    local service=$1
    local url=$2
    local max_attempts=${3:-60}
    local attempt=1

    echo -n "  Waiting for $service..."
    while [ $attempt -le $max_attempts ]; do
        if curl -sf "$url" > /dev/null 2>&1; then
            echo -e " ${GREEN}OK${NC}"
            return 0
        fi
        sleep 2
        attempt=$((attempt + 1))
        echo -n "."
    done
    echo -e " ${RED}TIMEOUT${NC}"
    return 1
}

wait_for_port() {
    local service=$1
    local host=$2
    local port=$3
    local max_attempts=${4:-30}
    local attempt=1

    echo -n "  Waiting for $service ($host:$port)..."
    while [ $attempt -le $max_attempts ]; do
        if nc -z "$host" "$port" 2>/dev/null; then
            echo -e " ${GREEN}OK${NC}"
            return 0
        fi
        sleep 2
        attempt=$((attempt + 1))
        echo -n "."
    done
    echo -e " ${RED}TIMEOUT${NC}"
    return 1
}

# Wait for each service
echo ""
wait_for_port "PostgreSQL" "localhost" "5432"
wait_for_port "Redis" "localhost" "6379"
wait_for_service "Ollama" "http://localhost:11434/api/tags" 120
wait_for_service "Mock LLM Server" "http://localhost:8090/health" 60
wait_for_service "Selenium Chrome" "http://localhost:4444/status" 60
wait_for_port "Chromedp" "localhost" "9222"
wait_for_port "SSH Server" "localhost" "2222"
wait_for_port "SSH Worker 1" "localhost" "2223"
wait_for_port "SSH Worker 2" "localhost" "2224"
wait_for_service "Cognee" "http://localhost:8000/health" 120 || echo -e "${YELLOW}Warning: Cognee may still be starting${NC}"
wait_for_service "Weaviate" "http://localhost:8081/v1/.well-known/ready" 60
wait_for_service "ChromaDB" "http://localhost:8082/api/v1/heartbeat" 60
wait_for_service "Qdrant" "http://localhost:6333/healthz" 60
wait_for_service "Mock Slack" "http://localhost:8091/health" 30

echo ""

# Step 5: Pull Ollama model
if [ "$PULL_MODEL" = true ]; then
    echo -e "${BLUE}Step 5: Pulling Ollama test model (llama2:7b)...${NC}"
    echo "This may take several minutes on first run..."

    # Check if model already exists
    if $CONTAINER_CMD exec helixcode-ollama-full ollama list 2>/dev/null | grep -q "llama2"; then
        echo -e "${GREEN}Model llama2 already available${NC}"
    else
        $CONTAINER_CMD exec helixcode-ollama-full ollama pull llama2:7b || {
            echo -e "${YELLOW}Warning: Could not pull llama2:7b, trying llama2...${NC}"
            $CONTAINER_CMD exec helixcode-ollama-full ollama pull llama2 || {
                echo -e "${YELLOW}Warning: Could not pull model. Tests requiring Ollama may fail.${NC}"
            }
        }
    fi
else
    echo -e "${BLUE}Step 5: Skipping model pull (--no-model specified)${NC}"
fi

# Step 6: Verify SSH connectivity
echo -e "${BLUE}Step 6: Verifying SSH connectivity...${NC}"

test_ssh() {
    local host=$1
    local port=$2

    if ssh -o StrictHostKeyChecking=no -o ConnectTimeout=5 \
           -i "$SSH_KEYS_DIR/id_rsa" \
           -p "$port" helixcode@"$host" "echo 'SSH OK'" 2>/dev/null; then
        return 0
    else
        return 1
    fi
}

echo -n "  Testing SSH to main server..."
if test_ssh "localhost" "2222"; then
    echo -e " ${GREEN}OK${NC}"
else
    echo -e " ${YELLOW}SKIPPED (keys may not be mounted yet)${NC}"
fi

# Step 7: Display status
echo ""
echo -e "${BLUE}=== Test Infrastructure Status ===${NC}"
echo ""

$COMPOSE_CMD -f "$COMPOSE_FILE" ps --format "table {{.Name}}\t{{.Status}}\t{{.Ports}}" 2>/dev/null || \
$COMPOSE_CMD -f "$COMPOSE_FILE" ps

echo ""
echo -e "${GREEN}=== Setup Complete ===${NC}"
echo ""
echo "Environment file: $ENV_FILE"
echo ""
echo "To run tests:"
echo "  source $ENV_FILE"
echo "  make test-full"
echo ""
echo "Or run individual test types:"
echo "  make test-unit"
echo "  make test-integration"
echo "  make test-e2e"
echo "  make test-benchmark"
echo ""
echo "To stop the infrastructure:"
echo "  $COMPOSE_CMD -f $COMPOSE_FILE down"
echo ""
echo "To view logs:"
echo "  $COMPOSE_CMD -f $COMPOSE_FILE logs -f [service]"
echo ""
