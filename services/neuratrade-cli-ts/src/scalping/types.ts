/**
 * Deterministic scalping signal types.
 *
 * Mirrors the Go scalping types in services/backend-api/internal/services/scalping/types.go.
 * Signal math uses plain numbers for component classification; position-size and
 * PnL math in the trading layer must use BigDecimal/decimal.js.
 */

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
  readonly regime: number;
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
  readonly rsiOversoldStrong: number;
  readonly rsiOversoldMedium: number;
  readonly rsiOverboughtMedium: number;
  readonly rsiOverboughtStrong: number;
  readonly adxStrongTrend: number;
  readonly adxWeakTrend: number;
  /** Optional override for the minimum ADX required to enter. When 0 the
   *  existing adxWeakTrend default is used. */
  readonly adxMin?: number;
  readonly atrMaxPctOfPrice: number;
  readonly bollingerEntryMaxPct: number;
  readonly bollingerEntryMinPct: number;
  readonly minConfidenceSpread: number;
  readonly regimeMode?: "trend" | "reversion";
  /** Higher-timeframe trend filter periods. Buy entries are allowed only when
   *  EMA(trendFilterFastPeriod) > EMA(trendFilterSlowPeriod); sells when below.
   *  The filter is skipped when insufficient candles are available. */
  readonly trendFilterFastPeriod?: number;
  readonly trendFilterSlowPeriod?: number;
  /** Minimum ratio of current volume to its moving average required to enter.
   *  0 disables the filter. Typical values: 1.0-1.5. */
  readonly volumeMinRatio?: number;
  /** Lookback period for volume moving average. Default 20. */
  readonly volumeLookback?: number;
  /** Minimum number of independent components that must agree with the final
   *  direction. 0 disables the filter. Typical values: 2-3. */
  readonly minConfluence?: number;
  /** If true, require the entry candle's body to align with the signal
   *  direction (green candle for buy, red candle for sell). */
  readonly entryCandleConfirm?: boolean;
  /** Number of recent candles used for short-term momentum confirmation.
   *  A signal is only emitted when the net return over those candles aligns
   *  with the signal direction. 0 disables the filter. */
  readonly momentumConfirmBars?: number;
}

export interface ComposerConfig {
  readonly weights: ComposerWeights;
  readonly thresholds: ComposerThresholds;
}
