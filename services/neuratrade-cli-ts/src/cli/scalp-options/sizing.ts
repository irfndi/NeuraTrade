/**
 * scalp CLI options — sizing
 *
 * Capital, leverage, account limits, fees, live/demo routing and credentials (how much and where).
 */
import { Options } from "../kit/kit.ts";
import { SCALP_OPTION_DEFAULTS } from "./defaults.js";

export const capitalOption = Options.integer("capital").pipe(
  Options.withDefault(SCALP_OPTION_DEFAULTS.capital),
  Options.withDescription("Initial capital in quote currency"),
);

export const positionSizeOption = Options.integer("position-size").pipe(
  Options.withDefault(SCALP_OPTION_DEFAULTS.positionSize),
  Options.withDescription("Position size as percent of capital"),
);

export const riskPerTradeOption = Options.float("risk-per-trade").pipe(
  Options.withDefault(SCALP_OPTION_DEFAULTS.riskPerTrade),
  Options.withDescription(
    "Risk per trade as percent of capital (overrides --position-size; 0 = disabled)",
  ),
);

export const riskBasedMaxPositionSizeOption = Options.float(
  "max-position-size",
).pipe(
  Options.withDefault(SCALP_OPTION_DEFAULTS.maxPositionSize),
  Options.withDescription(
    "Maximum position size as percent of capital when using --risk-per-trade",
  ),
);

export const feeOption = Options.float("fee").pipe(
  Options.withDefault(0.02),
  Options.withDescription(
    "Maker fee percent per side (validated research default 0.02)",
  ),
);

export const futuresOption = Options.boolean("futures").pipe(
  Options.withDefault(false),
  Options.withDescription("Trade perpetual futures instead of spot"),
);

export const leverageOption = Options.integer("leverage").pipe(
  Options.withDefault(SCALP_OPTION_DEFAULTS.leverage),
  Options.withDescription("Futures leverage (default 3x)"),
);

export const fundingRateOption = Options.float("funding-rate-pct").pipe(
  Options.withDefault(0.01),
  Options.withDescription(
    "Per-interval funding cost in percent (default 0.01% every 8h)",
  ),
);

export const slippageBpsOption = Options.float("slippage-bps").pipe(
  Options.withDefault(2),
  Options.withDescription(
    "Slippage in basis points applied to fills (validated research default 2)",
  ),
);

export const liveEntryCrossBpsOption = Options.float(
  "live-entry-cross-bps",
).pipe(
  Options.withDefault(0),
  Options.withDescription(
    "Live-entry limit cross in bps (default 0 = opt-in; soak sets 5): venue order crosses the rung touch so thin testnet books fill; paper fee line carries the same cross cost.",
  ),
);

export const demoLiveMarketEntriesOption = Options.boolean(
  "demo-live-market-entries",
).pipe(
  Options.withDefault(false),
  Options.withDescription(
    "Demo live entries as market orders (default false = limit; testnet-only opt-in for thin books, clever-cabin-85m).",
  ),
);

export const allowMainnetLiveOption = Options.boolean(
  "allow-mainnet-live",
).pipe(
  Options.withDefault(false),
  Options.withDescription(
    "Allow live execution on a NON-sandbox account (mainnet). Requires BOTH this flag and NEURATRADE_MAINNET_CONFIRM=I-CONFIRM-MAINNET exported in the launching environment; missing either fails closed before any exchange call.",
  ),
);

export const takerExitFeePctOption = Options.float("taker-exit-fee-pct").pipe(
  Options.withDefault(0.06),
  Options.withDescription(
    "Per-side TAKER fee percent for stop/liquidation/max-hold market exits (default 0.06 = Bybit taker)",
  ),
);

export const fundingRatePct8hOption = Options.float("funding-rate-pct-8h").pipe(
  Options.withDefault(0.01),
  Options.withDescription(
    "Funding cost percent of notional per 8h held on open positions (signed: longs pay positive rates; 0 = disabled)",
  ),
);

export const maintenanceMarginRatePctOption = Options.float(
  "maintenance-margin-rate-pct",
).pipe(
  Options.withDefault(0.5),
  Options.withDescription(
    "Maintenance margin rate percent of notional used by the liquidation price model (0.5 = 0.5%, Bybit tier-1 default)",
  ),
);

export const makerFeeOption = Options.float("maker-fee-pct").pipe(
  Options.withDefault(0),
  Options.withDescription("Maker fee percent per side (for limit entries)"),
);

export const realisticOption = Options.boolean("realistic").pipe(
  Options.withDefault(false),
  Options.withDescription("Apply realistic slippage and fee assumptions"),
);

export const strictRealismOption = Options.boolean("strict-realism").pipe(
  Options.withDefault(false),
  Options.withDescription("Use strict realism (close-only + slippage)"),
);

export const realisticSlippageBpsOption = Options.float(
  "realistic-slippage-bps",
).pipe(
  Options.withDefault(5),
  Options.withDescription("Realistic slippage in basis points"),
);

export const maxPositionDrawdownPctOption = Options.float(
  "max-position-drawdown-pct",
).pipe(
  Options.withDefault(0),
  Options.withDescription(
    "Ladder: force-close an open rung at the current close when its unrealized loss exceeds this percent of entry (0 = disabled). A per-position guard so one leveraged loser cannot sink the book before the ladder-stop boundary triggers",
  ),
);

export const volatilityTargetAnnualPctOption = Options.float(
  "volatility-target-annual-pct",
).pipe(
  Options.withDefault(0),
  Options.withDescription(
    "Annual volatility target percent for position sizing",
  ),
);

export const liveOption = Options.boolean("live").pipe(
  Options.withDefault(false),
  Options.withDescription(
    "Use live exchange adapter (Binance spot or Bitget futures)",
  ),
);

export const shadowOption = Options.boolean("shadow").pipe(
  Options.withDefault(false),
  Options.withDescription(
    "Use live public market data with simulated fills only; never place exchange orders",
  ),
);

export const apiKeyOption = Options.text("api-key").pipe(
  Options.withDefault(""),
  Options.withDescription("Binance API key (or set BINANCE_API_KEY env)"),
);

export const apiSecretOption = Options.text("api-secret").pipe(
  Options.withDefault(""),
  Options.withDescription("Binance API secret (or set BINANCE_API_SECRET env)"),
);

export const marginModeOption = Options.text("margin-mode").pipe(
  Options.withDefault("crossed"),
  Options.withDescription("Futures margin mode: crossed or isolated"),
);

export const productTypeOption = Options.text("product-type").pipe(
  Options.withDefault("USDT-FUTURES"),
  Options.withDescription(
    "Futures product type: USDT-FUTURES, COIN-FUTURES or USDC-FUTURES",
  ),
);

export const maxDrawdownOption = Options.float("max-drawdown-pct").pipe(
  Options.optional,
  Options.withDescription(
    "Max drawdown % before blocking new trades (live default 5%)",
  ),
);

export const maxDailyLossOption = Options.float("max-daily-loss-pct").pipe(
  Options.optional,
  Options.withDescription(
    "Max daily loss % before blocking new trades (live default 2%)",
  ),
);

export const maxPositionSizeOption = Options.float(
  "max-position-size-pct",
).pipe(
  Options.optional,
  Options.withDescription(
    "Max position size % of capital per trade (live default 10%)",
  ),
);

export const maxTradesPerDayOption = Options.integer("max-trades-per-day").pipe(
  Options.optional,
  Options.withDescription("Max trades per day (live default 10)"),
);

export const minCapitalOption = Options.integer("min-capital").pipe(
  Options.optional,
  Options.withDescription(
    "Minimum capital required to trade (live default 100)",
  ),
);

export const watchlistOption = Options.text("watchlist").pipe(
  Options.optional,
  Options.withDescription(
    "Path to a JSON watchlist in NEURATRADE_HOME/data (uses per-symbol best params)",
  ),
);

export const noWatchlistOption = Options.boolean("no-watchlist").pipe(
  Options.withDescription(
    "Run paper-trade with the --symbol only, ignoring the DB watchlist fallback (validated single-candidate soaks)",
  ),
);

export const killSwitchOption = Options.boolean("kill-switch").pipe(
  Options.withDefault(false),
  Options.withDescription(
    "Engage kill switch before starting (blocks all new trades)",
  ),
);

export const disengageOption = Options.boolean("disengage").pipe(
  Options.withDefault(false),
  Options.withDescription("Disengage kill switch before starting"),
);

export const soakWatchlistOption = Options.text("watchlist").pipe(
  Options.withDescription("Path to a JSON watchlist in NEURATRADE_HOME/data"),
);

export const profileNameOption = Options.text("name").pipe(
  Options.withDescription(
    "Profile name (used as filename in ~/.neuratrade/profiles)",
  ),
);
