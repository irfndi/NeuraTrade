import { Decimal, money } from "../utils/money.js";
import type { Money } from "../utils/money.js";

/**
 * Exchange contract size constraints used to make paper/live order sizes
 * orderable (Bitget: minTradeNum, quantityPrecision, minTradeUSDT). Populated
 * by the CLI from BitgetClient.getContracts on the live path; tests provide
 * them directly. Absent => legacy sizing (no step rounding, flat 5 USDT floor).
 */
export interface ContractSizeSpec {
  /** Minimum order quantity in base units (Bitget minTradeNum). */
  readonly minQty: number;
  /** Quantity step, 10^-quantityPrecision on Bitget (0 = no step rounding). */
  readonly qtyStep: number;
  /** Minimum notional in quote units (Bitget minTradeUSDT). */
  readonly minTradeUSDT: number;
}

/**
 * Round a raw order qty to an exchange-orderable size.
 *
 * Bitget rejects sizes that are not a multiple of the contract's quantity
 * step and below minTradeNum. The qty is rounded UP to the step (a raw qty
 * below one step, e.g. 0.000077 BTC, is unorderable as-is), then raised to
 * minQty. If the up-rounded size would breach the notional `cap` but a
 * down-rounded step multiple >= minQty fits, that is used instead — equally
 * orderable and keeps the allocation budget (rounding 0.000386 BTC up to
 * 0.0004 would push a $25 allocation to $25.92; 0.0003 stays in budget).
 */
export function orderableQty(
  rawQty: Decimal,
  spec: ContractSizeSpec,
  entryPrice: Decimal,
  cap: Decimal,
): Decimal {
  if (rawQty.lessThanOrEqualTo(0)) return rawQty;
  let qty = rawQty;
  if (spec.qtyStep > 0) {
    const step = money(spec.qtyStep);
    const up = qty.div(step).ceil().times(step);
    qty = up.times(entryPrice).lessThanOrEqualTo(cap)
      ? up
      : qty.div(step).floor().times(step);
  }
  if (qty.lessThan(money(spec.minQty))) qty = money(spec.minQty);
  return qty;
}

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
  /** Leverage the position was actually opened at (futures engines; sizing
   *  may lift leverage above the config value). Persisted so reduce-only
   *  closes send the open leverage, not the config value. */
  readonly leverage?: number;
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
  /**
   * Config-level starting capital the state was seeded under (options.initialCapital).
   * Optional for legacy states persisted before this field existed — those match
   * on the remaining config only. New states always set it, so two soaks with
   * different capital (btc-candidate 50 vs challenge-10 10) never share a row.
   */
  readonly initialCapital?: number;
  readonly capital: Money;
  readonly peakCapital: Money;
  readonly paused: number;
  readonly side: GridPaperPositionSide | null;
  readonly entryPrice: Money;
  readonly entryOrderId?: string;
  readonly entryClientOid?: string;
  readonly entryFilledQty?: Money;
  readonly entryFee?: Money;
  readonly entryFillSource?: "simulated" | "live" | "adopted";
  readonly strategyConfigFingerprint?: string;
  readonly cohortId?: string;
  readonly candidateLockAt?: Date;
  readonly datasetCutoffAt?: Date;
  readonly entryOpenedAt?: Date;
  readonly executionEnvironment?: "bitget-demo" | "bitget-live" | "bybit-demo" | "bybit-live";
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
  readonly strategyConfigFingerprint?: string;
  readonly cohortId?: string;
  readonly candidateLockAt?: Date;
  readonly datasetCutoffAt?: Date;
  readonly entryOpenedAt?: Date;
  readonly executionEnvironment?: "bitget-demo" | "bitget-live" | "bybit-demo" | "bybit-live";
}
