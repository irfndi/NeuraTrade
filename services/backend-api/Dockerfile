# Backend API Dockerfile
FROM golang:1.25.5 AS builder

WORKDIR /app

# Copy go mod files
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Build the application
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -ldflags="-w -s" -o main ./cmd/server && \
    chmod +x main

# Final Stage
FROM oven/bun:1 AS production

RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates tzdata curl bash postgresql-client && rm -rf /var/lib/apt/lists/*

# Create non-root user
RUN groupadd -g 1001 appgroup && \
    useradd -u 1001 -g appgroup -m -s /bin/bash appuser

WORKDIR /app

# Copy Go binary and configs
COPY --from=builder /app/main .
COPY --from=builder /app/config.yml .
COPY --from=builder /app/scripts ./scripts
COPY --from=builder /app/database ./database
COPY --from=builder /app/entrypoint.sh .

# Permissions
RUN chown -R appuser:appgroup /app && \
    chmod +x scripts/*.sh && \
    chmod +x entrypoint.sh

USER appuser

EXPOSE 8080

HEALTHCHECK --interval=30s --timeout=3s --start-period=10s --retries=3 \
    CMD curl -f http://localhost:8080/health || exit 1

CMD ["./entrypoint.sh"]
