# NeuraTrade

[![CodSpeed](https://img.shields.io/endpoint?url=https://codspeed.io/badge.json)](https://codspeed.io/irfndi/NeuraTrade?utm_source=badge)

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

## Recovery Tuning (Autonomous)

- Fractional values are ratios (`0.30 = 30%`, `0.50 = 50%`).
- `NEURATRADE_RECOVERY_MICRO_ENTRY_MIN_DRAWDOWN=0.30`: Drawdown threshold where recovery mode can allow micro entries.
- `NEURATRADE_RECOVERY_DERISK_ONLY_DRAWDOWN=0.40`: Drawdown threshold where new entries are blocked and de-risk-only mode is enforced.
- `NEURATRADE_RECOVERY_CLEAN_CYCLES=1`: Required consecutive clean cycles before micro-entry mode can re-open entries.
- `NEURATRADE_RECOVERY_MICRO_ENTRY_CAP_PCT=0.50`: Maximum position-size cap multiplier while micro-entry mode is active.
- `NEURATRADE_LIVENESS_MAX_ATTEMPTS_PER_HOUR=5`: Maximum liveness-forced entry attempts allowed per rolling hour.
- `NEURATRADE_SCALPING_SYMBOL_LOSS_STREAK_BUDGET=2`: Consecutive per-symbol losses allowed before the symbol is temporarily paused.

## License

MIT
