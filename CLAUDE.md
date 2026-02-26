# CLAUDE.md

Guidance for contributors and coding agents working in this repository.

## Project Overview

NeuraTrade is a multi-service trading platform with:
- Go backend API (`services/backend-api`)
- Bun CCXT bridge (`services/ccxt-service`)
- Bun Telegram service (`services/telegram-service`)

Runtime is native-first (no Docker dependency).

## Core Commands

```bash
make build
make run
make test
make lint
make typecheck
make coverage-check
```

## Service Management

```bash
./bin/neuratrade gateway start
./bin/neuratrade gateway status
./bin/neuratrade gateway stop
```

## Architecture Notes

- Backend entrypoint: `services/backend-api/cmd/server/main.go`
- API routing: `services/backend-api/internal/api/routes.go`
- Core domain logic: `services/backend-api/internal/services/`
- Shared protobufs: `protos/`

## Testing

- Backend unit/integration/e2e: `services/backend-api/test/...`
- Telegram tests: `services/telegram-service`
- Coverage gate uses `services/backend-api/scripts/coverage-check.sh`

## Configuration

- Primary local runtime config: `~/.neuratrade/config.json`
- Env template: `.env.example`
- SQLite default path is under `NEURATRADE_HOME`
