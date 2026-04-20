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

RUN --mount=type=cache,target=/root/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=linux go build \
        -trimpath \
        -ldflags="-s -w" \
        -o /partrack \
        ./cmd/partrack


# ── Stage 3: runtime ──────────────────────────────────────────────────────────
FROM alpine:3.21 AS runtime

RUN apk add --no-cache ca-certificates tzdata && \
    addgroup -S partrack && \
    adduser  -S -G partrack partrack

COPY --from=backend --chown=partrack:partrack /partrack /usr/local/bin/partrack

USER partrack
EXPOSE 8080

HEALTHCHECK --interval=15s --timeout=5s --start-period=10s --retries=3 \
    CMD wget -qO- http://localhost:8080/api/v1/health

ENTRYPOINT ["/usr/local/bin/partrack"]
