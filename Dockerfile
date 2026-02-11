# ==========================================
# NeuraTrade - Unified Monorepo Dockerfile
# ==========================================
# This Dockerfile builds all services from the monorepo structure:
# - services/backend-api (Go)
# - services/ccxt-service (Bun/TypeScript)
# - services/telegram-service (Bun/TypeScript)
# ==========================================

# ==========================================
# Stage 1: Go Backend Builder
# ==========================================
FROM golang:1.25.5 AS go-builder

# Install git and ca-certificates
RUN apt-get update && apt-get install -y --no-install-recommends git ca-certificates tzdata && rm -rf /var/lib/apt/lists/*

WORKDIR /app

# Copy go mod files from backend-api service
COPY services/backend-api/go.mod services/backend-api/go.sum ./
RUN go mod download

# Copy backend-api source code
COPY services/backend-api/ .

# Build the application
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -ldflags="-w -s" -o main ./cmd/server && \
    chmod +x main

# ==========================================
# Stage 2: CCXT Service Builder (Bun)
# ==========================================
FROM oven/bun:1 AS ccxt-builder
WORKDIR /app

# Copy ccxt-service files
COPY services/ccxt-service/package.json services/ccxt-service/bun.lock ./

# Install dependencies
RUN bun install --frozen-lockfile

# Copy source code
COPY services/ccxt-service/ .

# Build
RUN bun run build:bun

# Prune dev dependencies
RUN bun install --frozen-lockfile --production

# ==========================================
# Stage 3: Telegram Service Builder (Bun)
# ==========================================
FROM oven/bun:1 AS telegram-builder
WORKDIR /app

COPY services/telegram-service/package.json services/telegram-service/bun.lock ./
RUN bun install --frozen-lockfile

COPY services/telegram-service/ .
RUN bun run build:bun

RUN bun install --frozen-lockfile --production

# ==========================================
# Stage 4: Unified Runtime (Go + Bun)
# ==========================================
FROM oven/bun:1 AS production

RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates tzdata wget curl bash postgresql-client && rm -rf /var/lib/apt/lists/*

# Create non-root user
RUN groupadd -g 1001 appgroup && \
    useradd -u 1001 -g appgroup -m -s /bin/bash appuser

WORKDIR /app

# Copy Go binary and configs from backend-api
COPY --from=go-builder /app/main .
COPY --from=go-builder /app/config.yml .
COPY --from=go-builder /app/scripts ./scripts
COPY --from=go-builder /app/database ./database
COPY --from=go-builder /app/entrypoint.sh .

# Copy CCXT service
WORKDIR /app/ccxt
COPY --from=ccxt-builder /app/node_modules ./node_modules
COPY --from=ccxt-builder /app/dist ./dist
COPY --from=ccxt-builder /app/package.json .

# Copy Telegram service
WORKDIR /app/telegram
COPY --from=telegram-builder /app/node_modules ./node_modules
COPY --from=telegram-builder /app/dist ./dist
COPY --from=telegram-builder /app/package.json .

WORKDIR /app

# Permissions
RUN chown -R appuser:appgroup /app && \
    chmod +x scripts/*.sh 2>/dev/null || true && \
    chmod +x entrypoint.sh

USER appuser

# Expose ports (Go: 8080, CCXT: 3001, Telegram: 3002)
EXPOSE 8080 3001 3002

# Healthcheck (checks Go app)
HEALTHCHECK --interval=30s --timeout=3s --start-period=10s --retries=3 \
    CMD wget --no-verbose --tries=1 --spider http://localhost:8080/health || exit 1

CMD ["./entrypoint.sh"]
