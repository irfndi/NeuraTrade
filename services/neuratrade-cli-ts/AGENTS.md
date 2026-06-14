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
│   └── utils/               # Signal handling, env resolution helpers
└── tests/
    ├── integration/         # Integration tests
    └── e2e/                 # End-to-end smoke tests
```

## WHERE TO LOOK

| Task                      | Location                                                     | Notes                                                             |
| ------------------------- | ------------------------------------------------------------ | ----------------------------------------------------------------- | ------------- | --------- |
| CLI command tree          | `src/cli/index.ts`                                           | Root command + subcommand wiring                                  |
| Gateway start/stop/status | `src/cli/gateway.ts`, `src/services/gateway-orchestrator.ts` | Process lifecycle                                                 |
| Config loading            | `src/services/config.ts`, `src/schemas/*.ts`                 | env → runtime.json → config.json → defaults                       |
| PID files                 | `src/services/pid.ts`                                        | Read/write/liveness/pattern matching                              |
| Process spawning          | `src/services/process-manager.ts`                            | Bun.spawn, signals, cleanup                                       |
| API client                | `src/services/api-client.ts`                                 | HTTP client for backend endpoints                                 |
| SQLite client             | `src/services/sqlite.ts`                                     | `bun:sqlite` client (scoped layer)                                |
| Market repository         | `src/services/market-repository.ts`                          | Exchange, pair, and OHLCV persistence                             |
| Binance client            | `src/services/binance-client.ts`                             | Public REST market data client                                    |
| Bitget futures safety     | `src/services/bitget-futures-safety.ts`                      | Pre-flight live-order guards (reduce-only, margin-mode, leverage) |
| Rate limiter              | `src/services/rate-limiter.ts`                               | Token-bucket limiter for HTTP calls                               |
| Health checks             | `src/services/health-check.ts`                               | HTTP probe + process probe                                        |
| Market data commands      | `src/cli/market.ts`                                          | `market fetch-universe                                            | fetch-candles | coverage` |
| Backtest commands         | `src/cli/backtest.ts`                                        | `backtest scalping run`                                           |
| Paper trading             | `src/cli/paper.ts`, `src/services/paper-trading-engine.ts`   | Deterministic scalping paper trades with SL/TP/leverage           |

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
bun run index.ts market fetch-universe --top 5 --dry-run
bun run index.ts market fetch-candles --symbols BTC/USDT --start 2025-01-01 --end 2025-01-31
bun run index.ts market coverage --symbols BTC/USDT --start 2025-01-01 --end 2025-01-31
bun run index.ts backtest scalping run --start 2025-01-01T00:00:00Z --end 2025-01-31T00:00:00Z
```

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
