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
