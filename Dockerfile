FROM golang:1.26-alpine AS builder

# Install build dependencies
RUN apk add --no-cache git ca-certificates tzdata docker-cli openssh-client

# Set working directory
WORKDIR /app

# Copy go mod files
COPY helix_code/go.mod helix_code/go.sum ./

# Download dependencies
RUN go mod download

# Copy source code
COPY helix_code/ .

# Build all components
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-X main.version=1.0.0 -X main.buildTime=$(date +%Y-%m-%d_%H:%M:%S) -X main.gitCommit=$(git rev-parse --short HEAD 2>/dev/null || echo 'unknown')" -o bin/server ./cmd/server
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-X main.version=1.0.0 -X main.buildTime=$(date +%Y-%m-%d_%H:%M:%S) -X main.gitCommit=$(git rev-parse --short HEAD 2>/dev/null || echo 'unknown')" -o bin/cli ./cmd/cli
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-X main.version=1.0.0 -X main.buildTime=$(date +%Y-%m-%d_%H:%M:%S) -X main.gitCommit=$(git rev-parse --short HEAD 2>/dev/null || echo 'unknown')" -o bin/terminal-ui ./applications/terminal_ui

FROM alpine:latest

# Install runtime dependencies
RUN apk add --no-cache docker-cli docker-compose postgresql-client redis bash openssh-client curl ca-certificates tzdata

# Create app directory
WORKDIR /app

# Copy built binaries
COPY --from=builder /app/bin/ .
COPY --from=builder /app/assets/ ./assets/
COPY --from=builder /app/config/ ./config/

# Create directories for project mounting
RUN mkdir -p /workspace /projects /shared

# Copy entrypoint script
COPY docker-entrypoint.sh /usr/local/bin/
RUN chmod +x /usr/local/bin/docker-entrypoint.sh

# Expose ports
EXPOSE 8080 2222 3000

# Set environment variables
ENV HELIX_ENV=production
# CONST-042 / §12.1: no credential is baked into the image. This URL is a
# readiness-probe target only (docker-entrypoint.sh feeds it to `pg_isready`,
# which does not authenticate); the real password is supplied at RUNTIME from
# the private configuration source (.env -> HELIX_DATABASE_PASSWORD).
ENV HELIX_DATABASE_URL=postgres://helix@postgres:5432/helixcode_prod?sslmode=disable
ENV HELIX_REDIS_URL=redis://redis:6379
ENV HELIX_WORKSPACE=/workspace
ENV HELIX_PROJECTS=/projects
ENV HELIX_SHARED=/shared

# Set entrypoint
ENTRYPOINT ["/usr/local/bin/docker-entrypoint.sh"]