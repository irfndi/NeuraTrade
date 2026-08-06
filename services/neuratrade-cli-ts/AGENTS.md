# NEURATRADE CLI TS — Agent Knowledge Base

## OVERVIEW

`services/neuratrade-cli-ts/` is the TypeScript/Effect-TS port of `cmd/neuratrade-cli`. It is a process manager and control interface for the NeuraTrade platform, and the home of the futures scalping engine (signal composer, backtester, optimizer, paper trading) targeting Bitget USDT-M perpetuals.

**The entire package runs on Effect v4** (`effect@4.0.0-beta.102`, pinned exactly) + `@effect/platform-bun@4.0.0-beta.102`. There is no `@effect/cli` and no `@effect/platform` (v3) — the CLI is a hand-rolled kit and platform services come from core `effect`.

## STRUCTURE

```text
services/neuratrade-cli-ts/
├── index.ts                 # Entrypoint: runEffect(root) + BunRuntime.runMain
├── test-setup.ts            # Bun test preload: temp NEURATRADE_HOME
├── src/
│   ├── cli/                 # Command modules built on the local kit
│   │   └── kit/             # argv parser + dispatcher + help (replaces @effect/cli)
│   ├── services/            # Effect services (Path, Config, PID, ProcessManager, Bitget client, etc.)
│   ├── schemas/             # Schema definitions for config/state JSON
│   ├── market-data/         # Exchange gateways (binance, bitget spot+futures) and SQLite repository
│   ├── scalping/            # Signal composer, backtest engine, grid, walk-forward, readiness gates
│   ├── exchange/            # Exchange adapter port (simulated + live Binance/Bitget futures)
│   ├── paper-trading/       # Real-time paper/live trading engines (spot, futures, grid)
│   ├── risk/                # Pre-trade guards, kill switch, circuit breaker
│   └── utils/               # money.ts (decimal.js), env, signal helpers
├── scripts/                 # Research utilities (grid research, sweeps)
└── tests/
    ├── integration/
    └── e2e/                 # Spawn `bun run index.ts` smoke tests
```

## WHERE TO LOOK

| Task                      | Location                                                     | Notes                                                                            |
| ------------------------- | ------------------------------------------------------------ | -------------------------------------------------------------------------------- |
| CLI kit (parser/dispatch) | `src/cli/kit/kit.ts`                                         | `Command.make`/`Options.*` mirror the old @effect/cli API; help auto-generated   |
| CLI command tree          | `src/cli/index.ts`                                           | Root command + subcommand wiring                                                 |
| Gateway start/stop/status | `src/cli/gateway.ts`, `src/services/gateway-orchestrator.ts` | Process lifecycle                                                                |
| Config loading            | `src/services/config.ts`, `src/schemas/*.ts`                 | env → runtime.json → config.json → defaults                                      |
| Market data commands      | `src/cli/market.ts`                                          | fetch-candles, fetch-funding-rates, fetch-universe                               |
| Bitget futures client     | `src/services/bitget-client.ts`                              | HMAC REST, USDT/COIN futures, PAPTRADING demo mode (`BITGET_USE_SANDBOX`)        |
| Market data persistence   | `src/market-data/repository.ts`                              | Schema-adaptive (slim CLI schema AND shared Go schema with display_name/ccxt_id) |
| Scalping engines          | `src/scalping/{composer,backtest,grid,exit-engine}.ts`       | Pure cores; Effect wrappers in `src/scalping/services.ts`                        |
| Readiness gates           | `src/scalping/readiness.ts`                                  | `evaluateReadiness` G1–G4 (frequency/economics/robustness/hold time)             |
| Strategy profiles         | `src/scalping/strategy-profile.ts`                           | `findSymbolOverride` matches `BTC/USDT` ↔ `BTC/USDT:USDT` keys both ways         |
| Exchange adapter port     | `src/exchange/adapter.ts`, `src/exchange/adapters/`          | Simulated + live adapters                                                        |
| Paper/live trading        | `src/paper-trading/{engine,futures-engine,grid-engine}.ts`   | Iteration loop + persistence                                                     |
| Pre-trade risk guards     | `src/risk/guards.ts`                                         | Drawdown, daily loss, position size limits                                       |

## COMMANDS

```bash
bun install
bun run typecheck
bun test
bun test --coverage

bun run index.ts --help
bun run index.ts gateway start|stop|status
bun run index.ts market fetch-candles --exchange bitget-futures --symbol BTC/USDT:USDT --timeframe 5m --days 365
bun run index.ts market fetch-funding-rates --exchange bitget-futures --symbol BTC/USDT:USDT --days 365
bun run index.ts market fetch-universe --quote USDT --top 20 --timeframe 1h --days 365

bun run index.ts scalp backtest --exchange bitget-futures --symbol BTC/USDT:USDT --timeframe 5m --futures --fee 0.06 --slippage-bps 2 --oos-pct 20 --mc-iterations 200
bun run index.ts scalp readiness --exchange bitget-futures --symbol BTC/USDT:USDT --timeframe 5m --futures --fee 0.06 --slippage-bps 2 [--profile <name>]
bun run index.ts scalp optimize --symbol BTC/USDT --timeframe 1h --regime-mode reversion
bun run index.ts scalp scan --timeframe 1h --regime-mode reversion
bun run index.ts scalp walk-forward --symbol BTC/USDT --timeframe 4h --realistic
bun run index.ts scalp library --list
bun run index.ts scalp paper-trade --symbol BTC/USDT --timeframe 1h --iterations 10
bun run index.ts bitget futures balance|positions|contracts|ticker
bun run index.ts exchange test --api-key $BINANCE_API_KEY --api-secret $BINANCE_API_SECRET --symbol BTC/USDT --quantity 0.001

# Research sweeps (scripts/, not part of the CLI):
bun run scripts/scalp-readiness-scan.ts --exchange bitget-futures --symbol BTC/USDT:USDT --timeframe 5m
bun run scripts/sweep-to-profile.ts --sweep /tmp/sweep-btc-5m.json
```

**Wired `scalp` subcommands**: backtest, optimize, scan, paper-trade, soak, profile, library, walk-forward, readiness, demo-readiness, grid-universe-scan, watchlist. Anything else you may remember (`select`, `validate`, `preset`, `run`) is NOT wired — `selectBestForSymbol`/`validateWatchlist`/`applyPreset` exist only as exported functions used by walk-forward and tests.

## READINESS GATES (`scalp readiness`)

The single source of truth for "ready to scalp futures". Runs a backtest with forced OOS (last 20%) + Monte Carlo (200 iterations) and prints a per-gate PASS/FAIL table; exits non-zero when any gate fails.

- **G1 frequency**: ≥ 20 trades/month in-sample on 5m (≥ 10 on other timeframes; override with `--min-trades-per-month`), ≥ 10 OOS trades.
- **G2 economics**: win rate ≥ 50%, profit factor ≥ 1.3, expectancy > 0 (net of fees).
- **G3 robustness**: OOS return ≥ 0, OOS max drawdown ≤ 15%, MC p95 drawdown ≤ 20%, MC ruin probability ≤ 5%.
- **G4 hold time**: average trade duration ≤ 4h.

Thresholds live in `src/scalping/readiness.ts` (`defaultReadinessThresholds`). The older `isLiveReady` row check in `src/cli/scalp.ts` (used by watchlist validation) shares the G3 values.

## REAL-MONEY / RISK READINESS

- Bitget live-order commands require `--force` to bypass the safety gate (`src/services/bitget-futures-safety.ts`); demo/paper mode uses `BITGET_USE_SANDBOX=true` (PAPTRADING=1 header).
- Live risk defaults: `maxPositionSizePct=10`, `maxDailyLossPct=2`, `maxDrawdownPct=5`, `maxTradesPerDay=10`, `minCapital=100`; circuit breaker default 2% daily loss. Paper mode defaults are permissive.
- Position-size and PnL math in `src/paper-trading/` uses `decimal.js` via `src/utils/money.ts`.
- `scalp --live` is futures-only. Futures orders go through the Go backend risk and execution actors; the CLI rejects spot live mode rather than using the direct Binance adapter.
- Live futures signal mode fails closed when a close has no exchange fill, checks `minAtrPct` before placing an entry, and rejects scale-out until exchange-fill reconciliation is implemented. The current real-money candidate is the separately fill-aware grid path with `scaleOutPct=0`.

## CONVENTIONS

- **Effect v4 everywhere**: `Effect.gen`, `Layer`, `Context.Service` (NOT the v3 `Context.Tag`), `Config`, `Schedule`, `Stream`, `Result` (NOT `Either`).
- `FileSystem`/`Path`/`Terminal` come from core `effect`; Bun layers from `@effect/platform-bun` (`BunServices.layer`, `BunFileSystem.layer`).
- Schema defaults: `X.pipe(S.withDecodingDefault(Effect.succeed(v)))` — no `S.optional` wrapper.
- All IO wrapped in `Effect.try*`/services; no bare `new Database`, `fs.*`, `process.env`, `console.*` inside Effect code (tests excepted).
- Tests co-located as `*.test.ts` plus `tests/integration/` and `tests/e2e/`; `fast-check` for property tests.
- Coverage threshold: 80% lines/functions/statements, 70% branches.

## BITGET FUTURES DATA NOTES

- Futures candles MUST come from `/api/v2/mix/market/history-candles` (the plain `candles` endpoint silently returns `[]` for old windows). Implemented in `src/market-data/gateways/bitget.ts`.
- `history-candles` rejects `limit > 200` (error 40053); the gateway clamps and paginates.
- Futures funding history (`history-fund-rate`) serves ~90 days max. Fetch via `market fetch-funding-rates --exchange bitget-futures`.
- Backtests can consume the stored funding series via `BacktestOptions.fundingRates` (the CLI currently uses the flat `--funding-rate-pct`; research scripts pass the real series).

## REALISTIC BACKTESTING

- `--realistic` applies 5 bps slippage and 0.1% fee per side with the normal intrabar model. For Bitget USDT-M taker use `--fee 0.06 --slippage-bps 2 --futures`.
- `--observed-price` is a worst-case close-only stress test, deliberately pessimistic for scalping.
- Only configs that pass `scalp readiness` AND a walk-forward with >50% profitable windows should be promoted to paper/live trading.
- The baseline (2026-07-17, engine defaults, 1y bitget-futures): every config fails readiness — see `docs/superpowers/specs/2026-07-17-baseline-backtest-results.md`.

## STRATEGY LIBRARY

- `scalp library --list` shows templates; `scalp library <name>` prints the merged backtest args JSON (it does NOT run a backtest).
- Built-in templates: `meanReversion`, `trendFollowing`, `breakout`, `emaPullback`, `momentum`, `rangeExpansion`, `fundingCarry`, `dualEmaCross`, `ensemble`, `microScalp`, `connorsRsi2`, `gridScalp`.
- `scalp paper-trade --strategy <name>` applies a template's composer config and execution overrides to the paper loop.
- `--max-bars-in-trade` forces a time-stop exit after N candles — essential for scalping hold-time discipline.
- New strategy templates go in `src/scalping/strategy-library.ts` with tests in `src/scalping/strategy-library.test.ts`.

## FUNDING-RATE BIAS SIGNAL

The composer supports an optional contrarian funding-rate component for perpetual-futures pairs (`--use-funding` / `useFunding: true`). Threshold default `0.0001` (0.01% per 8h); positive funding above threshold emits `sell`, negative emits `buy`. Zero weight by default. `scalp library fundingCarry` is the pre-weighted template.

## ANTI-PATTERNS

- Do not import from `cmd/neuratrade-cli` (Go module).
- Do not use `float64` for money (use `decimal` equivalents in TS).
- Do not suppress type errors with `as any` or `@ts-ignore`.
- Do not hold locks across IO.
- Do not leave stale PID files after process death.
- Do not reintroduce v3 idioms: `Context.Tag`, `Either`, `@effect/cli`, `@effect/platform`, `Layer.scoped` (use `Context.Service`, `Result`, the CLI kit, core `effect` services, `Layer.effect`).
- Do not edit profiles keyed `BTC/USDT` expecting them to apply to `BTC/USDT:USDT` runs — `findSymbolOverride` now matches both, but exact keys win.

## CLOUDFLARE (alchemy) SCAFFOLD

`alchemy.run.ts` + `src/cloudflare/` deploy part of the TS port to Cloudflare
Workers via [alchemy](https://github.com/alchemy-run/alchemy) (Infrastructure-
as-Effects, requires `effect >= 4.0.0` — this package is on 4.0.0-beta.102).

- `bun run deploy:cf` / `dev:cf` / `destroy:cf` / `logs:cf` / `tail:cf`.
- First deploy: interactive Cloudflare OAuth (no tokens needed); saved to
  `~/.alchemy/profiles.json`.
- Worker `neuratrade-universe-watch` (async form): cron `0 */6 * * *` scans a
  seeded symbol universe via live Bitget public candles and persists survivors
  to KV as a `WatchlistEntry`-compatible whitelist. HTTP: `GET /health`,
  `GET /watchlist`, `POST /scan`, `PUT /seed` (admin-gated via
  `CF_ADMIN_API_KEY`, bound as a Cloudflare secret from the deployer env).
- The porting seam: `src/cloudflare/market-data-repository.ts` implements only
  the two `runGridUniverseScan` methods (`listSymbolsByCandleCount`,
  `getCandles`) over the fetch-based `MarketDataGatewayLive`; the rest are
  loud stubs so nothing silently pretends to persist on the edge. A D1-backed
  implementation replaces it later without touching the scanner.
- Does NOT migrate: local SQLite storage (D1 pending), the Bitget leverage
  probe (needs creds; skip or bind as secrets), telegram-service (still on
  effect 3 — upgrade to effect 4 before it can join an alchemy stack).
