# NeuraTrade

NeuraTrade is a multi-service crypto trading platform with a Go backend and Bun sidecar services for exchange and Telegram integrations.

## Core Services

- `services/backend-api` (Go): API, strategy/risk engine, persistence, autonomous orchestration
- `services/ccxt-service` (Bun): exchange bridge
- `services/telegram-service` (Bun): Telegram bot and delivery

## Prerequisites

- Go 1.26+
- Bun 1.3+
- SQLite (default runtime database)
- Redis (recommended for runtime caches/queues)

## Quick Start (Native)

1. Clone and configure:
```bash
git clone https://github.com/irfndi/neuratrade.git
cd neuratrade
cp .env.example .env
```

2. Build binaries:
```bash
make build
```

3. Start all services natively:
```bash
./bin/neuratrade gateway start
```

4. Check health:
```bash
./bin/neuratrade gateway status
curl -s http://localhost:8080/health
```

5. Stop services:
```bash
./bin/neuratrade gateway stop
```

## Common Commands

```bash
make build
make test
make lint
make typecheck
make coverage-check
```

## API Surface

- Health: `/health`
- Market data: `/api/market/...`
- Arbitrage: `/api/arbitrage/opportunities`, `/api/futures/opportunities`
- Signals: `/api/analysis/signals`

## Notes

- Runtime/log/pid state is stored under `NEURATRADE_HOME` (default `~/.neuratrade`).
- Telegram notifications are managed by `services/telegram-service`; configure bot settings in `.env` or `config.json`.

## License

MIT
