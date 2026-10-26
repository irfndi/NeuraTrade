/**
 * scalp CLI options — research
 *
 * Grid-universe, flow and watchlist research command flags.
 */
import { Options } from "../kit/kit.ts";

export const gridUniverseExchangeOption = Options.text("exchange").pipe(
  Options.withDefault("bitget-futures"),
  Options.withDescription("Exchange to scan for grid candidates"),
);

export const gridUniverseTimeframeOption = Options.text("timeframe").pipe(
  Options.withDefault("15m"),
  Options.withDescription("Candle timeframe for the grid universe scan"),
);

export const gridUniverseMinCandlesOption = Options.integer("min-candles").pipe(
  Options.withDefault(500),
  Options.withDescription(
    "Minimum candles a symbol must have to be included (ladder requires at least 2000)",
  ),
);

export const gridUniverseTrainWindowOption = Options.integer(
  "train-window",
).pipe(
  Options.withDefault(180),
  Options.withDescription("Walk-forward training window in candles"),
);

export const gridUniverseTestWindowOption = Options.integer("test-window").pipe(
  Options.withDefault(60),
  Options.withDescription("Walk-forward test window in candles"),
);

export const gridUniverseMinProfitableWindowsOption = Options.float(
  "min-profitable-windows-pct",
).pipe(
  Options.withDefault(60),
  Options.withDescription(
    "Minimum % of walk-forward windows a symbol must be profitable in",
  ),
);

export const gridUniverseMinAggregateReturnOption = Options.float(
  "min-aggregate-return-pct",
).pipe(
  Options.withDefault(0),
  Options.withDescription(
    "Minimum aggregate walk-forward return % a symbol must exceed",
  ),
);

export const gridUniverseFeeOption = Options.float("fee").pipe(
  Options.withDefault(0.06),
  Options.withDescription("Per-side fee % applied in the grid backtests"),
);

export const gridUniverseSlippageOption = Options.float("slippage-bps").pipe(
  Options.withDefault(2),
  Options.withDescription("Slippage in basis points applied to grid fills"),
);

export const gridUniverseTrendFilterOption = Options.integer(
  "trend-filter-period",
).pipe(
  Options.withDefault(0),
  Options.withDescription("SMA trend filter period for the grid backtests"),
);

export const gridUniverseMarketOption = Options.boolean("market").pipe(
  Options.withDefault(false),
  Options.withDescription(
    "Discover symbols from the exchange contract list (24h-volume filtered) instead of the stored candle set",
  ),
);

export const gridUniverseOutputOption = Options.text("output").pipe(
  Options.optional,
  Options.withDescription(
    "Write surviving candidates to a JSON whitelist in NEURATRADE_HOME/data",
  ),
);

export const gridUniverseWatchOption = Options.boolean("watch").pipe(
  Options.withDefault(false),
  Options.withDescription(
    "Continuously re-scan the universe and upsert survivors into the DB watchlist",
  ),
);

export const gridUniverseIntervalOption = Options.integer("interval").pipe(
  Options.withDefault(3600),
  Options.withDescription(
    "Seconds between scans in --watch continuous mode (default 3600)",
  ),
);

export const gridUniverseMinFillFrequencyOption = Options.float(
  "min-fill-frequency-pct",
).pipe(
  Options.withDefault(0),
  Options.withDescription(
    "Reject survivors whose grid step touches < this % of candles (0 = disabled)",
  ),
);

export const gridUniverseTargetFillsPerDayOption = Options.float(
  "target-fills-per-day",
).pipe(
  Options.optional,
  Options.withDescription(
    "Portfolio fills/day target for survivor selection (default: account-scaled 5-50)",
  ),
);

export const gridUniverseAccountCapitalOption = Options.float(
  "account-capital",
).pipe(
  Options.withDefault(1000),
  Options.withDescription(
    "Account capital USDT scaling the fills/day target when --target-fills-per-day is unset (default 1000)",
  ),
);

export const gridUniverseTierOption = Options.text("tier").pipe(
  Options.withDefault("readiness"),
  Options.withDescription(
    "Eligibility tier: 'readiness' (full stage-4 gate board) or 'fast' (light: strict time-split + walk-forward profitability + fills/day floor)",
  ),
);

export const gridUniverseDataSourceOption = Options.text("data-source").pipe(
  Options.withDefault("gateway"),
  Options.withDescription(
    "Candle source for the market scan: 'gateway' (default, live exchange candles — testnet-wired for bybit-futures) or 'db-mainnet' (5m mainnet candles from the DB resampled to the scan timeframe; no gateway fetches; fills modeled conservatively). Requires --market",
  ),
);

export const gridUniverseEngineOption = Options.text("engine").pipe(
  Options.withDefault("grid"),
  Options.withDescription(
    "Grid engine to evaluate: 'grid' (single-position, default) or 'ladder' (multi-rung, one TP per rung)",
  ),
);

export const gridUniverseRungsOption = Options.text("rungs").pipe(
  Options.optional,
  Options.withDescription(
    "Comma-separated rungs sweep for --engine ladder (default '1,2,3')",
  ),
);

export const watchlistListExchangeOption = Options.text("exchange").pipe(
  Options.withDefault("bitget-futures"),
  Options.withDescription("Exchange to list watchlist for"),
);

export const watchlistListTimeframeOption = Options.text("timeframe").pipe(
  Options.withDefault("15m"),
  Options.withDescription("Timeframe to list watchlist for"),
);

// ---------------------------------------------------------------------------
// Flow Ignition (flow-v1) options
// ---------------------------------------------------------------------------

export const flowSymbolsOption = Options.text("symbols").pipe(
  Options.withDefault("BTCUSDT"),
  Options.withDescription(
    "Comma-separated Bybit wire symbols to backtest, e.g. BTCUSDT,ETHUSDT",
  ),
);

export const flowStartOption = Options.text("start").pipe(
  Options.withDefault(""),
  Options.withDescription(
    "Inclusive start date (YYYY-MM-DD). Default: 180 days ago",
  ),
);

export const flowEndOption = Options.text("end").pipe(
  Options.withDefault(""),
  Options.withDescription("Inclusive end date (YYYY-MM-DD). Default: now"),
);

export const flowTimeframeOption = Options.choice("timeframe", [
  "5m",
  "1m",
] as const).pipe(
  Options.withDefault("5m"),
  Options.withDescription("Candle timeframe for the flow backtest"),
);

export const flowThresholdOption = Options.float("threshold").pipe(
  Options.withDefault(1.0),
  Options.withDescription("Entry score threshold in z-units (default 1.0)"),
);

export const flowHoldTimesOption = Options.text("hold-times").pipe(
  Options.withDefault("0.5,1,2,4,8"),
  Options.withDescription(
    "Comma-separated hold-time grid in hours; every entry is reported",
  ),
);

export const flowTradeExchangeOption = Options.text("exchange").pipe(
  Options.withDefault("bybit-futures"),
  Options.withDescription(
    "Exchange key for the flow live engine (data reads + adapter orders)",
  ),
);

export const flowTradeSymbolOption = Options.text("symbol").pipe(
  Options.withDefault("BTCUSDT"),
  Options.withDescription(
    "Bybit wire symbol to trade, e.g. BTCUSDT (default: the flow universe's #1 always-include base)",
  ),
);

export const flowHoldMinutesOption = Options.integer("hold-minutes").pipe(
  Options.withDefault(60),
  Options.withDescription(
    "Time exit for flow positions, in minutes (default 60)",
  ),
);

export const flowFeeOption = Options.float("fee").pipe(
  Options.withDefault(0.055),
  Options.withDescription("Taker fee percent per side (default 0.055)"),
);

export const flowSpreadBpsOption = Options.float("spread-bps").pipe(
  Options.withDefault(2),
  Options.withDescription(
    "Spread in basis points charged on BOTH entry and exit legs",
  ),
);

export const flowConservativeFillRateOption = Options.float(
  "conservative-fill-rate",
).pipe(
  Options.withDefault(0.75),
  Options.withDescription(
    "Deterministic fraction of flow signals filled after queue/partial-fill discount (default 0.75)",
  ),
);

export const flowMaxBreakevenWinRateOption = Options.float(
  "max-breakeven-win-rate",
).pipe(
  Options.withDefault(0.4),
  Options.withDescription(
    "Reject selected flow configs requiring a higher breakeven win rate (default 0.40)",
  ),
);

export const flowLimitOption = Options.integer("limit").pipe(
  Options.withDefault(40),
  Options.withDescription("Maximum ranked rows to print"),
);

export const flowMinTurnoverOption = Options.float("min-turnover").pipe(
  Options.withDefault(0),
  Options.withDescription(
    "Minimum 24h quote turnover (USDT) for a symbol to appear (0 = off)",
  ),
);

export const flowUniverseDataSourceOption = Options.choice("data-source", [
  "live-mainnet",
  "db-mainnet",
] as const).pipe(
  Options.withDefault("live-mainnet" as const),
  Options.withDescription(
    "Universe source: live Bybit mainnet tickers or cached mainnet SQLite candles",
  ),
);
