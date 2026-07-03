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

| Task                        | Location                                                           | Notes                                                                                  |
| --------------------------- | ------------------------------------------------------------------ | -------------------------------------------------------------------------------------- |
| CLI command tree            | `src/cli/index.ts`                                                 | Root command + subcommand wiring                                                       |
| Gateway start/stop/status   | `src/cli/gateway.ts`, `src/services/gateway-orchestrator.ts`       | Process lifecycle                                                                      |
| Config loading              | `src/services/config.ts`, `src/schemas/*.ts`                       | env → runtime.json → config.json → defaults                                            |
| PID files                   | `src/services/pid.ts`                                              | Read/write/liveness/pattern matching                                                   |
| Process spawning            | `src/services/process-manager.ts`                                  | Bun.spawn, signals, cleanup                                                            |
| API client                  | `src/services/api-client.ts`                                       | HTTP client for backend endpoints                                                      |
| Health checks               | `src/services/health-check.ts`                                     | HTTP probe + process probe                                                             |
| Market data commands        | `src/cli/market.ts`                                                | fetch-candles, fetch-funding-rates, fetch-universe                                     |
| Market data gateway         | `src/market-data/gateways/`                                        | Binance REST adapter (ticks, candles, funding rates, order book, symbols, 24h volumes) |
| Market data persistence     | `src/market-data/repository.ts`                                    | SQLite candles/ticks/funding-rates repository                                          |
| Deterministic scalping      | `src/scalping/composer.ts`, `src/scalping/backtest.ts`             | Signal composition + backtest engine                                                   |
| Portfolio backtest          | `src/scalping/portfolio-backtest.ts`                               | Cross-symbol global risk manager + correlation guard                                   |
| Unified strategy config     | `src/scalping/strategy-config.ts`, `src/scalping/config-runner.ts` | `strategy-config.json` → composer/backtest options                                     |
| Exchange adapter port       | `src/exchange/adapter.ts`, `src/exchange/adapters/`                | Simulated + live Binance adapters                                                      |
| Exchange testnet validation | `src/cli/exchange.ts`                                              | `exchange test` round-trip command                                                     |
| Paper/live trading          | `src/paper-trading/engine.ts`, `src/paper-trading/repository.ts`   | Iteration loop + persistence                                                           |
| Pre-trade risk guards       | `src/risk/guards.ts`                                               | Drawdown, daily loss, position size limits                                             |

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
bun run index.ts market fetch-funding-rates --symbol BTC/USDT --days 365
bun run index.ts market fetch-universe --quote USDT --top 20 --timeframe 1h --days 365
bun run index.ts scalp backtest --symbol BTC/USDT --timeframe 1h --use-atr-stops --regime-mode reversion
bun run index.ts scalp backtest --symbol BTC/USDT --timeframe 1h --use-funding --funding-threshold 0.0001
bun run index.ts scalp library fundingCarry --symbol BTC/USDT --timeframe 1h
bun run index.ts scalp optimize --symbol BTC/USDT --timeframe 1h --regime-mode reversion
bun run index.ts scalp scan --timeframe 1h --regime-mode reversion
bun run index.ts scalp scan --timeframe 1h --price-only --no-trend --regime-mode reversion --use-atr-stops --optimize
bun run index.ts scalp scan --timeframe 1h --price-only --no-trend --regime-mode reversion --use-atr-stops --optimize --min-return-pct 0 --save-watchlist watchlist.json
bun run index.ts scalp select --universe BTC/USDT,ETH/USDT --timeframe 1h --realistic --save-watchlist live-watchlist
bun run index.ts scalp select --top 5 --timeframe 1h --realistic --save-watchlist live-watchlist-top5
bun run index.ts scalp paper-trade --symbol BTC/USDT --timeframe 1h --iterations 10
bun run index.ts scalp paper-trade --watchlist watchlist.json --timeframe 1h --iterations 30 --interval 60
bun run index.ts scalp paper-trade --symbol BTC/USDT --timeframe 1h --live --api-key $BINANCE_API_KEY --api-secret $BINANCE_API_SECRET
bun run index.ts scalp run --mode backtest --symbols BTC/USDT --exchange binance --timeframe 1h
bun run index.ts scalp run --mode portfolio-backtest --symbols BTC/USDT,ETH/USDT --exchange binance --timeframe 1h --max-open-positions 3
bun run index.ts exchange test --api-key $BINANCE_API_KEY --api-secret $BINANCE_API_SECRET --symbol BTC/USDT --quantity 0.001

# Built-in scalper presets (realistic costs by default)
bun run index.ts scalp preset --list
bun run index.ts scalp preset conservative --symbol BTC/USDT --timeframe 1h
bun run index.ts scalp preset --compare --symbol BTC/USDT --timeframe 1h
bun run index.ts scalp preset aggressive --symbol BTC/USDT --timeframe 1h --realistic=false
```

## SCALPER PRESETS

The `scalp preset` subcommand provides three agent-harness-friendly presets:
`conservative`, `balanced` (default), and `aggressive`. Every preset defaults to
`observedPrice: true`, so exits use only observed close prices rather than
optimistic intrabar extremes. Users can override any preset value with the same
options accepted by `scalp backtest` (e.g., `--symbol`, `--timeframe`,
`--observed-price=false`).

- **conservative**: wide ATR stops, high confidence/confluence filters, small
  risk per trade, limit entries, auto-regime filter enabled.
- **balanced**: moderate ATR distances and thresholds; good default for
  exploration.
- **aggressive**: tight stops, lower confidence, larger size, mean-reversion
  regime mode, no auto-regime filter.

`--compare` runs all three presets on the same symbol/timeframe and prints a
ranked table by `robustnessScore`. Results also include the normal backtest
output, the composite robustness score, and any realism warnings.

## REAL-MONEY / RISK READINESS

- `--live` routes orders to the live Binance adapter (testnet by default; add the
  testnet key/secret, or pass `--live` only after confirming production readiness).
- Live mode applies conservative pre-trade risk defaults:
  `maxPositionSizePct=10`, `maxDailyLossPct=2`, `maxDrawdownPct=5`,
  `maxTradesPerDay=10`, `minCapital=100`. Paper mode defaults are permissive.
- Override any limit with `--max-position-size-pct`, `--max-daily-loss-pct`,
  `--max-drawdown-pct`, `--max-trades-per-day`, `--min-capital`.
- Position-size and PnL math in `src/paper-trading/` uses `decimal.js` via
  `src/utils/money.ts`. Values are converted to `number` only at persistence and
  exchange boundaries.

## CONVENTIONS

- Effect-TS everywhere: `Effect.gen`, `Layer`, `Context.Tag`, `Config`, `Schedule`, `Stream`.
- Bun 1.4 canary runtime.
- Tests co-located as `*.test.ts` plus `tests/integration/` and `tests/e2e/`.
- File system operations via `@effect/platform` `FileSystem`/`Path`.
- Process operations via `Bun.spawn` wrapped in Effect.
- Coverage threshold: 80% lines/functions/statements, 70% branches.

## REALISTIC BACKTESTING

- `--realistic` is the recommended default for real-money readiness. It applies
  5 bps slippage and the standard 0.1% fee per side while keeping the normal
  intrabar execution model.
- `--observed-price` is a worst-case close-only stress test, not the primary
  realism mode. It exits only on observed closes and is useful for sanity
  checking, but it is deliberately pessimistic for scalping.
- `scalp select` is the recommended way to build a symbol+parameter watchlist.
  It runs a small parameter grid per symbol under realistic costs and keeps the
  most robust config that passes the configured filters.
- Use `scalp validate --watchlist <name>` to run out-of-sample (OOS) backtests
  and Monte-Carlo drawdown simulation on a watchlist produced by `scalp select`.
  Validation reserves the last 20% of candles for OOS and runs 200 MC iterations.
- Only configs marked "live-ready" should be promoted to paper/live trading.
  A config is live-ready when OOS return >= 0, OOS max drawdown <= 15%,
  MC p95 drawdown <= 20%, MC probability of ruin <= 5%, and both IS and OOS
  trade counts are at least 10.
- Use `--save-validated <name>` to persist only the live-ready entries to a new
  watchlist in `~/.neuratrade/watchlists/<name>.json`.

## STRATEGY LIBRARY

- `scalp library --list` shows the available strategy templates.
- `scalp library <name>` runs a backtest using the named template; it accepts the
  same options as `scalp backtest` (e.g., `--symbol`, `--timeframe`, `--realistic`).
- Built-in templates: `meanReversion`, `trendFollowing`, `breakout`, `emaPullback`,
  `momentum`, `rangeExpansion`, `fundingCarry`, `dualEmaCross`, `microScalp`,
  `connorsRsi2`.
- `scalp paper-trade --strategy <name>` applies a template's composer config and
  execution overrides to the paper-trading loop. Explicit CLI options always
  override the template. The recommended shadow command for the current live-ready
  walk-forward configs is:
  ```bash
  bun run index.ts scalp paper-trade --strategy dualEmaCross --symbol AVAX/USDT --timeframe 1d --risk-per-trade 1 --realistic
  ```
- `scalp select --strategy <name>` applies a single template's composer/execution
  overrides to the selection grid, restricting the search to the template's regime
  mode. Use `--strategy all` to run the grid for every template and keep the most
  robust result per symbol.
- `scalp walk-forward` runs rolling train/test windows across the full candle
  history. For each window it picks the most robust parameter combo on the
  training slice and evaluates it on the following unseen test slice. Use it to
  get a more realistic OOS estimate than a single split:
  ```bash
  bun run index.ts scalp walk-forward --symbol BTC/USDT --timeframe 4h --realistic \
    --train-window 180 --test-window 60 --save btc-wf-4h
  ```
  The aggregate summary reports combined return, combined max drawdown, Sharpe on
  per-window returns, percentage of profitable windows, and average trades per
  window. Results are saved to `~/.neuratrade/walkforwards/<name>.json` when
  `--save <name>` is provided.
- A walk-forward result with >50% profitable windows and a positive aggregate
  return is a stronger real-money readiness gate than a single-split OOS result,
  because it shows the strategy can adapt as market regimes change.
- New strategy templates must be added to `src/scalping/strategy-library.ts` and
  covered by tests in `src/scalping/strategy-library.test.ts` and/or
  `src/cli/scalp.test.ts`.
- `gridScalp` is a market-neutral grid scalping template for choppy markets.
  It places symmetric buy/sell grids around the open price, captures
  oscillations, and pauses after stop-out losses.  The grid engine assumes
  limit/maker fills; realistic mode uses 0.02% per side + 1 bps slippage.
  Example $20 challenge commands:
  ```bash
  bun run index.ts scalp library gridScalp --symbol ETH/USDT --timeframe 15m --capital 20 --risk-per-trade 1 --realistic
  bun run index.ts scalp walk-forward --symbol ETH/USDT --timeframe 15m --strategy gridScalp --capital 20 --risk-per-trade 1 --realistic
  ```
- `microScalp` is the short-term RSI(2) mean-reversion template for 5m/15m
  timeframes. Example $20 challenge command:
  ```bash
  bun run index.ts scalp library microScalp --symbol BTC/USDT --timeframe 5m --realistic --capital 20 --risk-per-trade 1
  bun run index.ts scalp walk-forward --symbol BTC/USDT --timeframe 15m --realistic --strategy microScalp --train-window 1000 --test-window 500 --capital 20 --risk-per-trade 1
  ```
- `connorsRsi2` is a Larry Connors-style RSI(2) mean-reversion template with a
  trend filter and RSI-normalization exits instead of fixed ATR stops. It is
  aimed at the $20 scalping challenge on 15m timeframes:
  ```bash
  bun run index.ts scalp library connorsRsi2 --symbol BTC/USDT --timeframe 15m --realistic --capital 20 --risk-per-trade 1
  bun run index.ts scalp walk-forward --symbol BTC/USDT --timeframe 15m --realistic --strategy connorsRsi2 --train-window 1000 --test-window 500 --capital 20 --risk-per-trade 1
  ```
- `--max-bars-in-trade` forces a time-stop exit after N candles. Useful for
  scalping so trades do not sit open.

## FUNDING-RATE BIAS SIGNAL

The composer supports an optional contrarian funding-rate component for
perpetual-futures pairs:

- Fetch historical Binance USD-M funding rates with
  `market fetch-funding-rates` and store them in SQLite.
- Enable the component with `--use-funding` (CLI) or `useFunding: true` in a
  strategy profile / `strategy-config.json`.
- The threshold defaults to `0.0001` (0.01% per 8h) and is configurable via
  `--funding-threshold` or `fundingBiasThreshold`.
- Positive funding above the threshold emits a `sell` signal (shorts are
  expensive → fade longs); negative funding emits a `buy`.
- Strength is `strong` when `|rate| > 3 × threshold`, otherwise `medium`.
- The component is classified as **lagging** and has zero weight by default,
  so existing behavior is unchanged unless explicitly enabled.
- Use `scalp library fundingCarry` for a pre-weighted template that combines
  the funding bias with trend/volatility/regime signals.

## ANTI-PATTERNS

- Do not import from `cmd/neuratrade-cli` (Go module).
- Do not use `float64` for money (use `decimal` equivalents in TS).
- Do not suppress type errors with `as any` or `@ts-ignore`.
- Do not hold locks across IO.
- Do not leave stale PID files after process death.
