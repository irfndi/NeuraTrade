# NEURATRADE CLI TS — Agent Knowledge Base

## OVERVIEW

`services/neuratrade-cli-ts/` is the TypeScript/Effect-TS port of `cmd/neuratrade-cli`. It is a process manager and control interface for the NeuraTrade platform. The original Go CLI remains operational until this port is fully stable.

## STRUCTURE

```text
services/neuratrade-cli-ts/
├── index.ts                 # Entrypoint: BunRuntime.runMain(Command.run(...))
├── test-setup.ts            # Bun test preload: temp NEURATRADE_HOME
├── src/
│   ├── cli/                 # @effect/cli command definitions
│   ├── services/            # Effect services (Path, Config, PID, ProcessManager, etc.)
│   ├── schemas/             # Schema.Struct definitions for config/state JSON
│   ├── market-data/         # Exchange gateways and SQLite repository
│   ├── scalping/            # Deterministic signal composer + backtest engine
│   ├── exchange/            # Exchange adapter port (simulated + live Binance)
│   ├── paper-trading/       # Real-time paper/live trading engine
│   ├── risk/                # Pre-trade risk guards for real-money readiness
│   └── utils/               # Signal handling, env resolution helpers
└── tests/
    ├── integration/         # Integration tests
    └── e2e/                 # End-to-end smoke tests
```

## WHERE TO LOOK

| Task | Location | Notes |
|------|----------|-------|
| CLI command tree | `src/cli/index.ts` | Root command + subcommand wiring |
| Gateway start/stop/status | `src/cli/gateway.ts`, `src/services/gateway-orchestrator.ts` | Process lifecycle |
| Config loading | `src/services/config.ts`, `src/schemas/*.ts` | env → runtime.json → config.json → defaults |
| PID files | `src/services/pid.ts` | Read/write/liveness/pattern matching |
| Process spawning | `src/services/process-manager.ts` | Bun.spawn, signals, cleanup |
| API client | `src/services/api-client.ts` | HTTP client for backend endpoints |
| Health checks | `src/services/health-check.ts` | HTTP probe + process probe |
| Market data commands | `src/cli/market.ts` | fetch-candles, fetch-universe |
| Market data gateway | `src/market-data/gateways/` | Binance REST adapter (ticks, candles, order book, symbols, 24h volumes) |
| Market data persistence | `src/market-data/repository.ts` | SQLite candles/ticks repository |
| Deterministic scalping | `src/scalping/composer.ts`, `src/scalping/backtest.ts` | Signal composition + backtest engine |
| Exchange adapter port | `src/exchange/adapter.ts`, `src/exchange/adapters/` | Simulated + live Binance adapters |
| Paper/live trading | `src/paper-trading/engine.ts`, `src/paper-trading/repository.ts` | Iteration loop + persistence |
| Pre-trade risk guards | `src/risk/guards.ts` | Drawdown, daily loss, position size limits |

## COMMANDS

```bash
bun install
bun run typecheck
bun test
bun test --coverage
bun run index.ts --help
bun run index.ts gateway start
bun run index.ts gateway stop
bun run index.ts gateway status
bun run index.ts market fetch-candles --symbol BTC/USDT --timeframe 1h --start "2025-06-01"
bun run index.ts market fetch-universe --quote USDT --top 20 --timeframe 1h --days 365
bun run index.ts scalp backtest --symbol BTC/USDT --timeframe 1h --use-atr-stops --regime-mode reversion
bun run index.ts scalp optimize --symbol BTC/USDT --timeframe 1h --regime-mode reversion
bun run index.ts scalp scan --timeframe 1h --regime-mode reversion
bun run index.ts scalp scan --timeframe 1h --price-only --no-trend --regime-mode reversion --use-atr-stops --optimize
bun run index.ts scalp scan --timeframe 1h --price-only --no-trend --regime-mode reversion --use-atr-stops --optimize --min-return-pct 0 --save-watchlist watchlist.json
bun run index.ts scalp paper-trade --symbol BTC/USDT --timeframe 1h --iterations 10
bun run index.ts scalp paper-trade --symbol BTC/USDT --timeframe 1h --live --api-key $BINANCE_API_KEY --api-secret $BINANCE_API_SECRET
```

## REAL-MONEY / RISK READINESS

- `--live` routes orders to the live Binance adapter (testnet by default; add the
  testnet key/secret, or pass `--live` only after confirming production readiness).
- Live mode applies conservative pre-trade risk defaults:
  `maxPositionSizePct=10`, `maxDailyLossPct=2`, `maxDrawdownPct=5`,
  `maxTradesPerDay=10`, `minCapital=100`. Paper mode defaults are permissive.
- Override any limit with `--max-position-size-pct`, `--max-daily-loss-pct`,
  `--max-drawdown-pct`, `--max-trades-per-day`, `--min-capital`.
- Position-size and PnL math currently uses `number`; migrate to `decimal.js`
  before production real-money sizing.

## CONVENTIONS

- Effect-TS everywhere: `Effect.gen`, `Layer`, `Context.Tag`, `Config`, `Schedule`, `Stream`.
- Bun 1.4 canary runtime.
- Tests co-located as `*.test.ts` plus `tests/integration/` and `tests/e2e/`.
- File system operations via `@effect/platform` `FileSystem`/`Path`.
- Process operations via `Bun.spawn` wrapped in Effect.
- Coverage threshold: 80% lines/functions/statements, 70% branches.

## ANTI-PATTERNS

- Do not import from `cmd/neuratrade-cli` (Go module).
- Do not use `float64` for money (use `decimal` equivalents in TS).
- Do not suppress type errors with `as any` or `@ts-ignore`.
- Do not hold locks across IO.
- Do not leave stale PID files after process death.
