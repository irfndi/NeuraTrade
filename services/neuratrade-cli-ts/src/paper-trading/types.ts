import type { Money } from "../utils/money.js";

export type PaperPositionSide = "long" | "short";

export interface PaperPosition {
  readonly id: string;
  readonly exchange: string;
  readonly symbol: string;
  readonly timeframe: string;
  readonly side: PaperPositionSide;
  readonly entryPrice: Money;
  readonly size: Money;
  readonly stopLoss: Money;
  readonly takeProfit: Money;
  readonly openedAt: Date;
  readonly signalId: string;
  readonly capitalAtEntry?: Money;
  readonly scaledOut: boolean;
  readonly scaleOutPrice: Money;
}

export interface PaperTrade {
  readonly id: string;
  readonly exchange: string;
  readonly symbol: string;
  readonly timeframe: string;
  readonly side: PaperPositionSide;
  readonly entryPrice: Money;
  readonly exitPrice: Money;
  readonly size: Money;
  readonly pnl: Money;
  readonly pnlPct: Money;
  readonly exitReason: "signal" | "stop_loss" | "take_profit" | "scale_out";
  readonly openedAt: Date;
  readonly closedAt: Date;
}

export interface PaperPortfolio {
  readonly capital: Money;
  readonly peakCapital: Money;
  readonly position: PaperPosition | null;
}

export type GridPaperPositionSide = "long" | "short";

export interface GridPaperState {
  readonly exchange: string;
  readonly symbol: string;
  readonly timeframe: string;
  readonly capital: Money;
  readonly peakCapital: Money;
  readonly paused: number;
  readonly side: GridPaperPositionSide | null;
  readonly entryPrice: Money;
  readonly entryOrderId?: string;
  readonly entryClientOid?: string;
  readonly entryFilledQty?: Money;
  readonly entryFee?: Money;
  readonly entryFillSource?: "simulated" | "live";
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
  readonly entryPrice: Money;
  readonly exitPrice: Money;
  readonly capitalBefore: Money;
  readonly capitalAfter: Money;
  readonly pnlPct: Money;
  readonly exitReason: "target" | "stop" | "liquidation";
  readonly openedAt: Date;
  readonly closedAt: Date;
  readonly fillSource?: "simulated" | "live";
  readonly entryOrderId?: string;
  readonly entryClientOid?: string;
  readonly exitOrderId?: string;
  readonly exitClientOid?: string;
  readonly entryFilledQty?: Money;
  readonly exitFilledQty?: Money;
  readonly entryFee?: Money;
  readonly exitFee?: Money;
  readonly realizedPnlPct?: Money;
}
