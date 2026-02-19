# NeuraTrade CLI

A unified command-line interface for managing NeuraTrade services, inspired by picoclaw and openclaw.

## Installation

### Quick Install

```bash
# From repository root
make install-cli

# Or use the install script
./install.sh
```

### Build from Source

```bash
make build-cli
# CLI will be available at bin/neuratrade
```

## Usage

### Start All Services

```bash
# Start all services using Docker (default)
neuratrade gateway start

# Start all services natively (for development)
neuratrade gateway start --native

# Start only specific services
neuratrade gateway start --no-telegram
neuratrade gateway start --no-ccxt --no-telegram

# Run in detached mode (Docker only)
neuratrade gateway start -d
```

### Stop Services

```bash
neuratrade gateway stop
```

### Check Status

```bash
neuratrade gateway status
```

### View Logs

```bash
# Show all logs
neuratrade gateway logs

# Follow logs
neuratrade gateway logs -f

# Show specific service logs
neuratrade gateway logs --service backend
neuratrade gateway logs --service ccxt
neuratrade gateway logs --service telegram

# Show last N lines
neuratrade gateway logs --tail 50
```

## Commands

| Command | Description |
|---------|-------------|
| `neuratrade gateway start` | Start all services |
| `neuratrade gateway stop` | Stop all services |
| `neuratrade gateway status` | Check service health and status |
| `neuratrade gateway logs` | Show service logs |
| `neuratrade version` | Show CLI version |
| `neuratrade help` | Show help message |

## Options

### Gateway Start Options

- `--native` - Run services as native processes instead of Docker
- `--no-backend` - Skip starting backend-api service
- `--no-ccxt` - Skip starting ccxt-service
- `--no-telegram` - Skip starting telegram-service
- `--detach, -d` - Run in detached mode (Docker only)

### Gateway Logs Options

- `--service, -s` - Show logs for specific service (backend, ccxt, telegram)
- `--follow, -f` - Follow log output
- `--tail, -n` - Number of lines to show (default: 100)

## Environment Variables

- `NEURATRADE_HOME` - Base directory for NeuraTrade (default: ~/.neuratrade)
- `TELEGRAM_BOT_TOKEN` - Telegram bot token
- `ADMIN_API_KEY` - Admin API key for service authentication
- `DATABASE_PASSWORD` - PostgreSQL password (required for local mode)

## Comparison with Manual Commands

### Before (Manual)

```bash
# Terminal 1: Backend API
cd services/backend-api
go run ./cmd/server/main.go

# Terminal 2: CCXT Service
cd services/ccxt-service
bun run index.ts

# Terminal 3: Telegram Service
cd services/telegram-service
export TELEGRAM_USE_POLLING=true
export TELEGRAM_API_BASE_URL=http://localhost:8080
export ADMIN_API_KEY=...
export TELEGRAM_BOT_TOKEN=...
bun run index.ts
```

### After (Using CLI)

```bash
# Single command to start everything
neuratrade gateway start --native
```

## Architecture

The CLI supports two modes:

1. **Docker Mode (Default)** - Uses Docker Compose to orchestrate services
2. **Native Mode (`--native`)** - Runs services as native processes (for development)

## Requirements

### Docker Mode

- Docker
- Docker Compose

### Native Mode

- Go 1.23+
- Bun
- PostgreSQL (if running backend)
- Redis (if running backend)

## Development

```bash
# Build the CLI
cd cmd/neuratrade-cli
go build -o neuratrade .

# Run locally
./neuratrade --help
```
