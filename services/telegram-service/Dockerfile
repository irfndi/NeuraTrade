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

WORKDIR /app

# Copy built artifacts and dependencies
COPY --from=builder /app/dist ./dist
COPY --from=builder /app/node_modules ./node_modules
COPY package.json ./

EXPOSE 3002

CMD ["bun", "run", "./dist/index.js"]
