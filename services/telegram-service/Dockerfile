# syntax=docker/dockerfile:1

FROM oven/bun:1.2-alpine AS builder

WORKDIR /app

# Copy package files
COPY package.json bun.lock ./
RUN bun install --frozen-lockfile

# Copy source and build
COPY . .
RUN bun run build:bun

# Final stage
FROM oven/bun:1.2-alpine

# Run as non-root bun user
USER bun

WORKDIR /app

# Copy built artifacts and dependencies
COPY --chown=bun:bun --from=builder /app/dist ./dist
COPY --chown=bun:bun --from=builder /app/node_modules ./node_modules
COPY --chown=bun:bun package.json ./

EXPOSE 3002

CMD ["bun", "run", "./dist/index.js"]
