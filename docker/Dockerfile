# syntax=docker/dockerfile:1.7

# ── Stage 1: frontend ─────────────────────────────────────────────────────────
FROM node:22-alpine AS frontend
WORKDIR /app/web

COPY web/package.json web/package-lock.json ./
RUN --mount=type=cache,target=/root/.npm \
    npm ci --prefer-offline

COPY web/ ./
RUN npm run build


# ── Stage 2: Go binary ────────────────────────────────────────────────────────
FROM golang:1.25-alpine AS backend
WORKDIR /app

# git is needed by some go modules at download time
RUN apk add --no-cache git

COPY go.mod go.sum ./
RUN --mount=type=cache,target=/root/go/pkg/mod \
    go mod download

COPY . .
COPY --from=frontend /app/web/dist ./web/dist

# Version metadata and the embedded user-facing changelog are derived from git
# inside this stage (the .git dir is in the build context). The runtime stage
# only copies the binary, so .git never reaches the final image. VERSION/COMMIT/
# DATE may be overridden via build args (e.g. a CI build from a tree with no
# .git); otherwise they fall back to git, then to sane defaults.
ARG VERSION=
ARG COMMIT=
ARG DATE=

RUN --mount=type=cache,target=/root/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build <<'SH'
set -eu
git config --global --add safe.directory /app
# Read refs/objects only — never `--dirty`: the build context omits some tracked
# files via .dockerignore, which would otherwise look like a dirty worktree.
VERSION="${VERSION:-$(git describe --tags --always 2>/dev/null || echo dev)}"
COMMIT="${COMMIT:-$(git rev-parse --short HEAD 2>/dev/null || echo unknown)}"
DATE="${DATE:-$(date -u +%Y-%m-%dT%H:%M:%SZ)}"
# Generate the embedded changelog from history before building.
go run ./tools/genchangelog -o internal/changelog/changelog.json
CGO_ENABLED=0 GOOS=linux go build \
    -trimpath \
    -ldflags="-s -w \
        -X github.com/Yamakaja/partrack/internal/version.Version=${VERSION} \
        -X github.com/Yamakaja/partrack/internal/version.Commit=${COMMIT} \
        -X github.com/Yamakaja/partrack/internal/version.Date=${DATE}" \
    -o /partrack \
    ./cmd/partrack
SH


# ── Stage 3: runtime ──────────────────────────────────────────────────────────
FROM alpine:3.21 AS runtime

RUN apk add --no-cache ca-certificates tzdata && \
    addgroup -S partrack && \
    adduser  -S -G partrack partrack && \
    mkdir -p /data && \
    chown partrack:partrack /data

COPY --from=backend --chown=partrack:partrack /partrack /usr/local/bin/partrack

USER partrack
EXPOSE 8080

HEALTHCHECK --interval=15s --timeout=5s --start-period=10s --retries=3 \
    CMD wget -qO- http://localhost:8080/api/v1/health

ENTRYPOINT ["/usr/local/bin/partrack"]
