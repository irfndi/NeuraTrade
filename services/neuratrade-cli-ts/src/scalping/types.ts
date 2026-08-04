/**
 * Deterministic scalping signal types.
 *
 * Mirrors the Go scalping types in services/backend-api/internal/services/scalping/types.go.
 * Signal math uses plain numbers for component classification; position-size and
 * PnL math in the trading layer must use BigDecimal/decimal.js.
 */

import type { SymbolStatistics } from "./symbol-stats.js";
import type { FundingRate as MarketDataFundingRate } from "../market-data/types.js";

export type FundingRate = MarketDataFundingRate;

export type Direction = "buy" | "sell" | "hold";
export type SignalStrength = "weak" | "medium" | "strong";

export interface SignalComponent {
  readonly name: string;
  readonly description: string;
  readonly value: number;
  readonly signal: Direction;
  readonly strength: SignalStrength;
  readonly weight: number;
}

export interface MicrostructureContext {
  readonly spread: number;
  readonly spreadPercent: number;
  readonly bidDepth: number;
  readonly askDepth: number;
  readonly imbalance: number;
  readonly midPrice: number;
  readonly timestamp: Date;
}

export interface QualityAssessment {
  readonly score: number;
  readonly grade: string;
  readonly concerns: readonly string[];
}

export interface ScalpingSignal {
  readonly id: string;
  readonly exchange: string;
  readonly symbol: string;
  readonly direction: Direction;
  readonly confidence: number;
  readonly components: readonly SignalComponent[];
  readonly microstructure: MicrostructureContext;
  readonly quality?: QualityAssessment;
  readonly stopLoss?: number;
  readonly takeProfit?: number;
  readonly attributionWeights: Record<string, number>;
  readonly metadata: Record<string, unknown>;
  readonly generatedAt: Date;
}

export interface OHLCVInput {
  readonly exchange: string;
  readonly symbol: string;
  readonly timeframe: string;
  readonly candles: readonly CandleLike[];
  /** Optional historical funding rates used by the funding bias component. */
  readonly fundingRates?: readonly import("../market-data/types.js").FundingRate[];
}

export interface CandleLike {
  readonly open: number;
  readonly high: number;
  readonly low: number;
  readonly close: number;
  readonly volume: number;
  readonly timestamp: Date;
}

export interface OrderBookMetricsInput {
  readonly exchange: string;
  readonly symbol: string;
  readonly spread: number;
  readonly spreadPercent: number;
  readonly bidDepth: number;
  readonly askDepth: number;
  readonly imbalance: number;
  readonly midPrice: number;
  readonly timestamp: Date;
}

export interface ComposerWeights {
  readonly spread: number;
  readonly imbalance: number;
  readonly volatility: number;
  readonly trend: number;
  readonly liquidity: number;
  readonly rsi: number;
  readonly rsiPullback: number;
  readonly emaPullback: number;
  readonly regime: number;
  readonly funding: number;
  readonly connorsRsi2: number;
}

export type ComposerIndicatorName = keyof ComposerWeights;

export interface ComposerEnabled {
  readonly spread?: boolean;
  readonly imbalance?: boolean;
  readonly volatility?: boolean;
  readonly trend?: boolean;
  readonly liquidity?: boolean;
  readonly rsi?: boolean;
  readonly rsiPullback?: boolean;
  readonly emaPullback?: boolean;
  readonly regime?: boolean;
  readonly funding?: boolean;
  readonly connorsRsi2?: boolean;
}

export interface ComposerThresholds {
  readonly spreadTightPct: number;
  readonly spreadModeratePct: number;
  readonly spreadWidePct: number;
  readonly imbalanceWeak: number;
  readonly imbalanceStrong: number;
  readonly volatilityLowPct: number;
  readonly volatilityModeratePct: number;
  readonly volatilityHighPct: number;
  readonly trendWeakPct: number;
  readonly trendStrongPct: number;
  readonly liquidityMedium: number;
  readonly liquidityStrong: number;
  readonly rsiPeriod?: number;
  readonly rsiOversoldStrong?: number;
  readonly rsiOversoldMedium: number;
  readonly rsiOverboughtMedium: number;
  readonly rsiOverboughtStrong?: number;
  readonly maxBarsInTrade?: number;
  readonly adxStrongTrend: number;
  readonly adxWeakTrend: number;
  /** Optional override for the minimum ADX required to enter. When 0 the
   *  existing adxWeakTrend default is used. */
  readonly adxMin?: number;
  readonly atrMaxPctOfPrice: number;
  /** Minimum ATR as a percentage of price required to enter. Prevents trading
   *  when the market is too compressed to cover fees. 0 disables the filter. */
  readonly atrMinPctOfPrice?: number;
  readonly bollingerEntryMaxPct: number;
  readonly bollingerEntryMinPct: number;
  readonly minConfidenceSpread: number;
  readonly regimeMode?: "trend" | "reversion" | "breakout";
  /** Breakout regime lookback period for highest high/lowest low. Default 20. */
  readonly breakoutLookback?: number;
  /** Breakout regime minimum volume ratio (current / SMA volume). Default 1.2. */
  readonly breakoutVolumeMinRatio?: number;
  /** Breakout regime minimum ADX required to enter. Default 20. */
  readonly breakoutAdxMin?: number;
  /** Higher-timeframe trend filter periods. Buy entries are allowed only when
   *  EMA(trendFilterFastPeriod) > EMA(trendFilterSlowPeriod); sells when below.
   *  The filter is skipped when insufficient candles are available. */
  readonly trendFilterFastPeriod?: number;
  readonly trendFilterSlowPeriod?: number;

  /** Larry Connors RSI(2) mean-reversion trend filter period. Trades are only
   *  taken when price is on the right side of the SMA(trendFilterPeriod). */
  readonly trendFilterPeriod?: number;
  /** RSI(2) long entry threshold. Long when RSI(2) is below this level and
   *  price is above the trend filter. */
  readonly entryRsiLongThreshold?: number;
  /** RSI(2) short entry threshold. Short when RSI(2) is above this level and
   *  price is below the trend filter. */
  readonly entryRsiShortThreshold?: number;
  /** RSI exit threshold for longs. Close long when RSI rises above this level. */
  readonly exitRsiLongThreshold?: number;
  /** RSI exit threshold for shorts. Close short when RSI falls below this level. */
  readonly exitRsiShortThreshold?: number;

  /** Perpetual futures funding-rate threshold. When |fundingRate| exceeds this
   *  value the composer generates a contrarian signal. Default 0.0001 (0.01%). */
  readonly fundingBiasThreshold?: number;
  /** Enable the funding-rate bias component. Default false. */
  readonly useFunding?: boolean;
  /** Minimum ratio of current volume to its moving average required to enter.
   *  0 disables the filter. Typical values: 1.0-1.5. */
  readonly volumeMinRatio?: number;
  /** Lookback period for volume moving average. Default 20. */
  readonly volumeLookback?: number;
  /** Minimum number of independent components that must agree with the final
   *  direction. 0 disables the filter. Typical values: 2-3. */
  readonly minConfluence?: number;
  /** Minimum number of indicator categories (leading, current, lagging) that
   *  must contribute a directional signal. 0 disables the filter. Typical
   *  values: 2-3. */
  readonly minCategoryConfluence?: number;
  /** If true, require the entry candle's body to align with the signal
   *  direction (green candle for buy, red candle for sell). */
  readonly entryCandleConfirm?: boolean;
  /** Number of recent candles used for short-term momentum confirmation.
   *  A signal is only emitted when the net return over those candles aligns
   *  with the signal direction. 0 disables the filter. */
  readonly momentumConfirmBars?: number;
  /** When true, only trend/RSI/regime are used as directional signals.
   *  Spread, imbalance, liquidity and volatility become pure filters. */
  readonly directionalOnly?: boolean;
  /** When true and regimeMode is "trend", RSI confirms the trend direction
   *  (buy when RSI > 50, sell when RSI < 50) instead of fading extremes. */
  readonly rsiFollowTrend?: boolean;
  /** When true, all directional components must agree for a signal to fire. */
  readonly strictAgreement?: boolean;
  /** Maximum spread percent allowed. When exceeded the signal is suppressed.
   *  0 disables the filter. */
  readonly maxSpreadPct?: number;
  /** Minimum liquidity (bid+ask depth) required. 0 disables the filter. */
  readonly minLiquidity?: number;
  /** UTC trading session start (HH:MM). Empty string disables the filter. */
  readonly sessionStart?: string;
  /** UTC trading session end (HH:MM). Empty string disables the filter. */
  readonly sessionEnd?: string;
  /** Minimum efficiency ratio over the lookback window required to enter.
   *  ER = |price change| / sum(|bar moves|). 0 disables the filter.
   *  Higher values mean a cleaner trend. Typical: 0.3-0.5. */
  readonly minEfficiencyRatio?: number;
  /** When true, ATR/volatility bands are derived from per-symbol statistics
   *  instead of fixed thresholds. */
  readonly useAdaptiveMarketFilters?: boolean;
  /** Lookback window used for adaptive statistics. Default 100. */
  readonly adaptiveLookback?: number;
  /** Per-symbol statistics used by adaptive filters. */
  readonly symbolStats?: SymbolStatistics;
  /** How the trend component generates signals.
   *  "slope" = EMA fast vs slow slope (default).
   *  "cross" = only fire on the bar where fast EMA crosses slow EMA. */
  readonly trendSignalStyle?: "slope" | "cross";
  /** Fast EMA period used by the trend component. Default 9. */
  readonly trendFastPeriod?: number;
  /** Slow EMA period used by the trend component. Default 21. */
  readonly trendSlowPeriod?: number;
}

export interface ComposerConfig {
  readonly weights: ComposerWeights;
  readonly thresholds: ComposerThresholds;
  readonly enabled?: ComposerEnabled;
}
