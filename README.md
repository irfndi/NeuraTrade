# NeuraTrade

[![CodSpeed](https://img.shields.io/endpoint?url=https://codspeed.io/badge.json)](https://codspeed.io/irfndi/NeuraTrade?utm_source=badge)

NeuraTrade is a multi-service crypto trading platform. The active stack is **TypeScript/Effect-TS** (Bun): a trading CLI (`services/neuratrade-cli-ts/`) that runs grid scalping/paper-trading soaks under pm2 and deploys a universe-watch worker to Cloudflare via alchemy. The Go backend (`services/backend-api`) is **legacy/frozen** and slated for deletion — see `AGENTS.md` → MIGRATION STATUS.

## Core Services

- `services/neuratrade-cli-ts` (Bun + Effect-TS): **active** — grid scalping, paper-trading, Bitget demo live, readiness gate, Cloudflare worker
- `services/telegram-service` (Bun): Telegram bot and delivery
- `services/backend-api` (Go): **LEGACY/FROZEN** — API/strategy/risk engine, persistence (deletion tracked in bd)
- `services/ccxt-service` (Bun): exchange bridge (stub; real CCXT logic is embedded in the legacy Go backend)

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
- `NEURATRADE_SCALPING_MAX_CONCURRENT_POSITIONS=3`: Default concurrent managed-position cap for scalping entries.
- `NEURATRADE_SCALPING_MICRO_MAX_CONCURRENT_POSITIONS=1`: Micro-account concurrent managed-position cap. Keep this conservative unless paper/testnet soak and collateral checks support raising it.
- `NEURATRADE_LIVE_MAX_ORDER_NOTIONAL`: Required positive USDT cap for the TS CLI live-order endpoint; the endpoint stays disabled when unset.
- TS live scalping is futures-only and routes through the backend RiskActor/ExecutionActor gate. The backend reads the live USDT balance and reconciles exchange quantity, average price, status, and fee before returning a fill; spot `--live` exits closed.

## License

MIT
