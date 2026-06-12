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
  readonly minConfidenceSpread: number;
}

export interface ComposerConfig {
  readonly weights: ComposerWeights;
  readonly thresholds: ComposerThresholds;
}
