/**
 * Paper-trading domain types for deterministic scalping.
 *
 * All monetary values are plain numbers for speed; real-money trading must
 * switch to BigDecimal/decimal.js before going live.
 */

export type PaperPositionSide = "long" | "short";

export interface PaperPosition {
  readonly id: string;
  readonly exchange: string;
  readonly symbol: string;
  readonly timeframe: string;
  readonly side: PaperPositionSide;
  readonly entryPrice: number;
  readonly size: number;
  readonly stopLoss: number;
  readonly takeProfit: number;
  readonly openedAt: Date;
  readonly signalId: string;
  readonly capitalAtEntry?: number;
  readonly scaledOut: boolean;
  readonly scaleOutPrice: number;
}

export interface PaperTrade {
  readonly id: string;
  readonly exchange: string;
  readonly symbol: string;
  readonly timeframe: string;
  readonly side: PaperPositionSide;
  readonly entryPrice: number;
  readonly exitPrice: number;
  readonly size: number;
  readonly pnl: number;
  readonly pnlPct: number;
  readonly exitReason: "signal" | "stop_loss" | "take_profit" | "scale_out";
  readonly openedAt: Date;
  readonly closedAt: Date;
}

export interface PaperPortfolio {
  readonly capital: number;
  readonly peakCapital: number;
  readonly position: PaperPosition | null;
}

export type GridPaperPositionSide = "long" | "short";

export interface GridPaperState {
  readonly exchange: string;
  readonly symbol: string;
  readonly timeframe: string;
  readonly capital: number;
  readonly peakCapital: number;
  readonly paused: number;
  readonly side: GridPaperPositionSide | null;
  readonly entryPrice: number;
  readonly gridStepPct: number;
  readonly gridMaxGrids: number;
  readonly gridPauseAfterLossBars: number;
  readonly feePct: number;
  readonly slippageBps: number;
  readonly trendFilterPeriod: number;
  readonly maxPositionPct: number;
  readonly maxDrawdownPct: number;
  readonly leverage: number;
  readonly killed: boolean;
  /** Timestamp of the last processed candle; used in replay mode to avoid reprocessing bars. */
  readonly lastTimestamp: Date | null;
  readonly updatedAt: Date;
}

export interface GridPaperTrade {
  readonly id: string;
  readonly exchange: string;
  readonly symbol: string;
  readonly timeframe: string;
  readonly side: GridPaperPositionSide;
  readonly entryPrice: number;
  readonly exitPrice: number;
  readonly capitalBefore: number;
  readonly capitalAfter: number;
  readonly pnlPct: number;
  readonly exitReason: "target" | "stop" | "liquidation";
  readonly openedAt: Date;
  readonly closedAt: Date;
}
