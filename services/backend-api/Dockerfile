# Backend API Dockerfile
FROM golang:1.25.5 AS builder

# Install git, ca-certificates, and C compiler for SQLite
RUN apt-get update && apt-get install -y --no-install-recommends git ca-certificates gcc musl-dev && rm -rf /var/lib/apt/lists/*

WORKDIR /app

# Copy go mod files
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Build the application (enable CGO for SQLite)
RUN CGO_ENABLED=1 GOOS=linux go build -a -installsuffix cgo -ldflags="-w -s" -o main ./cmd/server && \
    chmod +x main

# Final Stage
FROM oven/bun:1 AS production

RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates tzdata curl bash postgresql-client && rm -rf /var/lib/apt/lists/*

# Create non-root user
RUN groupadd -g 1001 appgroup && \
    useradd -u 1001 -g appgroup -m -s /bin/bash appuser

WORKDIR /app

# Copy Go binary and configs (config.yml is optional - will use env vars)
COPY --from=builder /app/main .
RUN test -f /app/config.yml && COPY --from=builder /app/config.yml . || true
COPY --from=builder /app/scripts ./scripts
COPY --from=builder /app/database ./database
RUN test -f /app/scripts/entrypoint.sh && COPY --from=builder /app/scripts/entrypoint.sh . || true

# Permissions
RUN chown -R appuser:appgroup /app && \
    chmod +x scripts/*.sh 2>/dev/null || true

USER appuser

EXPOSE 8080

HEALTHCHECK --interval=30s --timeout=3s --start-period=10s --retries=3 \
    CMD curl -f http://localhost:8080/health || exit 1

CMD ["./entrypoint.sh"]
