/**
 * scalp CLI options — market
 *
 * Instrument, timeframe and data-source flags (what to look at).
 */
import { Options } from "../kit/kit.ts";

export const exchangeOption = Options.text("exchange").pipe(
  Options.withDefault("binance"),
  Options.withDescription("Exchange identifier"),
);

export const symbolOption = Options.text("symbol").pipe(
  Options.withDefault("BTC/USDT"),
  Options.withDescription("Trading pair symbol"),
);

export const timeframeOption = Options.text("timeframe").pipe(
  Options.withDefault("1h"),
  Options.withDescription("Candle timeframe"),
);

export const observedPriceOption = Options.boolean("observed-price").pipe(
  Options.withDefault(false),
  Options.withDescription("Use observed price (close-only) for entries"),
);

export const signalFeedOption = Options.choice("signal-feed", [
  "testnet",
  "mainnet",
]).pipe(
  Options.withDefault("testnet" as const),
  Options.withDescription(
    "Bybit signal-data feed (klines driving entries): testnet = api-testnet.bybit.com (default; matches the demo execution venue so a forward paper-vs-demo comparison runs on the SAME feed), mainnet = api.bybit.com (research parity with mainnet price behavior). Independent of the execution venue (--live + BYBIT_USE_TESTNET selects testnet vs mainnet order routing). A demo proves testnet EXECUTION (routing, fills, risk guards) — it does NOT prove a mainnet edge. The resolved feed is logged at startup as signalFeed=... next to executionEnv=.... Also settable via BYBIT_SIGNAL_FEED.",
  ),
);

export const priceOnlyOption = Options.boolean("price-only").pipe(
  Options.withDefault(false),
  Options.withDescription(
    "Ignore synthetic order-book components in backtest (trend/volatility/RSI/regime only)",
  ),
);

export const minCandlesOption = Options.integer("min-candles").pipe(
  Options.withDefault(500),
  Options.withDescription(
    "Minimum candles required for a symbol to be included in scan",
  ),
);

export const topOption = Options.integer("top").pipe(
  Options.withDefault(0),
  Options.withDescription(
    "Limit scan to top N symbols by candle count (0 = all)",
  ),
);

export const intervalOption = Options.integer("interval").pipe(
  Options.withDefault(60),
  Options.withDescription("Seconds between paper-trading iterations"),
);

export const replayBarsOption = Options.integer("replay-bars").pipe(
  Options.withDefault(0),
  Options.withDescription(
    "Grid: replay the last N stored candles one per iteration (0 = live shadow)",
  ),
);

// Defined once: the backtest and optimize command tables need the identical
// pair, and two copies of a date flag drift silently.
export const startOption = Options.text("start").pipe(
  Options.withDefault(""),
  Options.withDescription(
    "Inclusive backtest start date (YYYY-MM-DD). Empty = earliest available candle.",
  ),
);

export const endOption = Options.text("end").pipe(
  Options.withDefault(""),
  Options.withDescription(
    "Inclusive backtest end date (YYYY-MM-DD). Empty = latest available candle.",
  ),
);
