import type {
  CandleLike,
  ComposerConfig,
  OHLCVInput,
  OrderBookMetricsInput,
  ScalpingSignal,
} from "./types.js";
import { composeSignal } from "./composer.js";
import { calculateATR } from "./indicators.js";

export interface BacktestPosition {
  readonly entrySignal: ScalpingSignal;
  readonly entryPrice: number;
  readonly entryTime: Date;
  readonly side: "long" | "short";
  readonly size: number;
  readonly stopLoss: number;
  readonly takeProfit: number;
}

export interface BacktestTrade {
  readonly id: string;
  readonly symbol: string;
  readonly side: "long" | "short";
  readonly entryTime: Date;
  readonly exitTime: Date;
  readonly entryPrice: number;
  readonly exitPrice: number;
  readonly pnl: number;
  readonly pnlPct: number;
  readonly exitReason: "signal" | "stop_loss" | "take_profit";
}

export interface BacktestResult {
  readonly symbol: string;
  readonly totalTrades: number;
  readonly winningTrades: number;
  readonly losingTrades: number;
  readonly winRate: number;
  readonly totalReturnPct: number;
  readonly maxDrawdownPct: number;
  readonly sharpeRatio: number;
  readonly trades: readonly BacktestTrade[];
  readonly totalFeesPaid: number;
  readonly totalFundingCost: number;
  readonly benchmarkReturnPct: number;
}

export interface BacktestOptions {
  readonly symbol: string;
  readonly exchange: string;
  readonly timeframe: string;
  readonly candles: readonly CandleLike[];
  readonly composerConfig: ComposerConfig;
  readonly initialCapital: number;
  readonly positionSizePct: number;
  readonly stopLossPct: number;
  readonly takeProfitPct: number;
  readonly feePct: number;
  readonly minConfidence: number;
  /** When true, use ATR(14) * multiplier for dynamic stops instead of fixed pct. */
  readonly useAtrStops?: boolean;
  readonly atrStopMultiplier?: number;
  readonly atrTakeProfitMultiplier?: number;
  /** When true, ignore opposite-direction signal exits and only exit on stop/take-profit. */
  readonly holdUntilStop?: boolean;
  /** Slippage in basis points applied to entry and exit fills. Default 0. */
  readonly slippageBps?: number;
  /** Per-interval funding rate as whole-number percent (e.g. 0.01 = 0.01%). Default 0. */
  readonly fundingRatePct?: number;
  /** Hours between funding settlements. Default 8. */
  readonly fundingIntervalHours?: number;
  /** When true, accumulate funding costs on positions. Default false. */
  readonly isFutures?: boolean;
}

/**
 * Walk through historical candles and simulate scalping trades based on
 * deterministic signal composition.
 */
export function runBacktest(options: BacktestOptions): BacktestResult {
  const candles = options.candles;
  if (candles.length < 20) {
    return emptyResult(options.symbol);
  }

  const trades: BacktestTrade[] = [];
  let position: BacktestPosition | null = null;
  let capital = options.initialCapital;
  let peakCapital = capital;
  let maxDrawdown = 0;
  let tradeId = 0;
  let totalFeesPaid = 0;
  let totalFundingCost = 0;
  let lastFundingTime: Date | null = null;

  const slippageBps = options.slippageBps ?? 0;
  const fundingRatePct = options.fundingRatePct ?? 0;
  const fundingIntervalMs = (options.fundingIntervalHours ?? 8) * 3600_000;
  const isFutures = options.isFutures ?? false;

  // We need a rolling window of candles plus current OB metrics.
  // For backtesting, derive synthetic order-book metrics from the candle.
  for (let i = 20; i < candles.length - 1; i++) {
    const window = candles.slice(Math.max(0, i + 1 - 200), i + 1);
    const current = candles[i];
    const next = candles[i + 1];

    const obMetrics = syntheticOrderBook(current);
    const ohlcv: OHLCVInput = {
      exchange: options.exchange,
      symbol: options.symbol,
      timeframe: options.timeframe,
      candles: window,
    };

    const signal = composeSignal(ohlcv, obMetrics, options.composerConfig);

    // Update running capital/drawdown if position open
    if (position) {
      const mtmPrice = current.close;
      const mtmPnl = calculatePnl(position, mtmPrice);
      const mtmCapital = capital + mtmPnl;
      if (mtmCapital > peakCapital) peakCapital = mtmCapital;
      const drawdown = (peakCapital - mtmCapital) / peakCapital;
      if (drawdown > maxDrawdown) maxDrawdown = drawdown;

      // Check stop-loss / take-profit on current candle
      const { stopLoss, takeProfit } = checkExitLevels(position, current);
      if (stopLoss || takeProfit) {
        const rawExit = stopLoss ? position.stopLoss : position.takeProfit;
        const exitSide = position.side === "long" ? "short" : "long";
        const exitPrice = applySlippage(
          rawExit,
          exitSide,
          slippageBps,
          current.high,
          current.low,
        );
        const reason = stopLoss ? "stop_loss" : "take_profit";
        const pnl = calculatePnl(position, exitPrice);
        const exitFee = exitPrice * position.size * (options.feePct / 100);
        const funding = chargeFunding(
          position,
          lastFundingTime,
          current.timestamp,
          fundingRatePct,
          fundingIntervalMs,
          isFutures,
        );
        const pnlPct =
          ((pnl - exitFee) / (position.entryPrice * position.size)) * 100;
        capital += pnl - exitFee - funding;
        totalFeesPaid += exitFee;
        totalFundingCost += funding;
        trades.push({
          id: `trade-${tradeId++}`,
          symbol: options.symbol,
          side: position.side,
          entryTime: position.entryTime,
          exitTime: current.timestamp,
          entryPrice: position.entryPrice,
          exitPrice,
          pnl,
          pnlPct,
          exitReason: reason,
        });
        position = null;
        lastFundingTime = null;
        continue;
      }

      // Exit on signal reversal unless holding until stop/take-profit.
      if (
        !options.holdUntilStop &&
        signal &&
        shouldExitPosition(position, signal)
      ) {
        const exitSide = position.side === "long" ? "short" : "long";
        const exitPrice = applySlippage(
          next.open,
          exitSide,
          slippageBps,
          next.high,
          next.low,
        );
        const pnl = calculatePnl(position, exitPrice);
        const exitFee = exitPrice * position.size * (options.feePct / 100);
        const funding = chargeFunding(
          position,
          lastFundingTime,
          next.timestamp,
          fundingRatePct,
          fundingIntervalMs,
          isFutures,
        );
        const pnlPct =
          ((pnl - exitFee) / (position.entryPrice * position.size)) * 100;
        capital += pnl - exitFee - funding;
        totalFeesPaid += exitFee;
        totalFundingCost += funding;
        trades.push({
          id: `trade-${tradeId++}`,
          symbol: options.symbol,
          side: position.side,
          entryTime: position.entryTime,
          exitTime: next.timestamp,
          entryPrice: position.entryPrice,
          exitPrice,
          pnl,
          pnlPct,
          exitReason: "signal",
        });
        position = null;
        lastFundingTime = null;
      }
    }

    // Open new position if signal is strong enough and no position
    if (!position && signal && isEntrySignal(signal, options.minConfidence)) {
      const side = signal.direction === "buy" ? "long" : "short";
      const rawEntry = next.open;
      const entryPrice = applySlippage(
        rawEntry,
        side,
        slippageBps,
        next.high,
        next.low,
      );
      const positionValue = capital * (options.positionSizePct / 100);
      const size = positionValue / entryPrice;

      const atr = options.useAtrStops ? calculateATR(window, 14) : null;
      const useAtr = options.useAtrStops && atr !== null && atr > 0;
      const stopMult = options.atrStopMultiplier ?? 1.5;
      const tpMult = options.atrTakeProfitMultiplier ?? 2.5;

      let stopLoss: number;
      let takeProfit: number;
      if (useAtr) {
        stopLoss =
          side === "long"
            ? entryPrice - atr * stopMult
            : entryPrice + atr * stopMult;
        takeProfit =
          side === "long"
            ? entryPrice + atr * tpMult
            : entryPrice - atr * tpMult;
      } else {
        stopLoss =
          side === "long"
            ? entryPrice * (1 - options.stopLossPct / 100)
            : entryPrice * (1 + options.stopLossPct / 100);
        takeProfit =
          side === "long"
            ? entryPrice * (1 + options.takeProfitPct / 100)
            : entryPrice * (1 - options.takeProfitPct / 100);
      }

      position = {
        entrySignal: signal,
        entryPrice,
        entryTime: next.timestamp,
        side,
        size,
        stopLoss,
        takeProfit,
      };

      const entryFee = positionValue * (options.feePct / 100);
      capital -= entryFee;
      totalFeesPaid += entryFee;
      lastFundingTime = next.timestamp;
    }
  }

  // Close any open position at the last candle
  if (position && candles.length > 0) {
    const last = candles[candles.length - 1];
    const exitSide = position.side === "long" ? "short" : "long";
    const exitPrice = applySlippage(
      last.close,
      exitSide,
      slippageBps,
      last.high,
      last.low,
    );
    const pnl = calculatePnl(position, exitPrice);
    const exitFee = exitPrice * position.size * (options.feePct / 100);
    const funding = chargeFunding(
      position,
      lastFundingTime,
      last.timestamp,
      fundingRatePct,
      fundingIntervalMs,
      isFutures,
    );
    const pnlPct =
      ((pnl - exitFee) / (position.entryPrice * position.size)) * 100;
    capital += pnl - exitFee - funding;
    totalFeesPaid += exitFee;
    totalFundingCost += funding;
    trades.push({
      id: `trade-${tradeId++}`,
      symbol: options.symbol,
      side: position.side,
      entryTime: position.entryTime,
      exitTime: last.timestamp,
      entryPrice: position.entryPrice,
      exitPrice,
      pnl,
      pnlPct,
      exitReason: "signal",
    });
  }

  const winningTrades = trades.filter((t) => t.pnl > 0).length;
  const losingTrades = trades.filter((t) => t.pnl < 0).length;
  const totalReturnPct =
    ((capital - options.initialCapital) / options.initialCapital) * 100;
  const returns = trades.map((t) => t.pnlPct);
  const sharpe = calculateSharpe(returns);

  return {
    symbol: options.symbol,
    totalTrades: trades.length,
    winningTrades,
    losingTrades,
    winRate: trades.length > 0 ? winningTrades / trades.length : 0,
    totalReturnPct,
    maxDrawdownPct: maxDrawdown * 100,
    sharpeRatio: sharpe,
    trades,
    totalFeesPaid,
    totalFundingCost,
    benchmarkReturnPct: computeBenchmark(candles),
  };
}

function emptyResult(symbol: string): BacktestResult {
  return {
    symbol,
    totalTrades: 0,
    winningTrades: 0,
    losingTrades: 0,
    winRate: 0,
    totalReturnPct: 0,
    maxDrawdownPct: 0,
    sharpeRatio: 0,
    trades: [],
    totalFeesPaid: 0,
    totalFundingCost: 0,
    benchmarkReturnPct: 0,
  };
}

function syntheticOrderBook(candle: CandleLike): OrderBookMetricsInput {
  const spread = candle.high - candle.low;
  const midPrice = candle.close;
  const spreadPercent = midPrice > 0 ? spread / midPrice : 0;
  const range = candle.high - candle.low;
  const bidDepth = range > 0 ? ((candle.close - candle.low) / range) * 100 : 50;
  const askDepth =
    range > 0 ? ((candle.high - candle.close) / range) * 100 : 50;
  const imbalance =
    bidDepth + askDepth > 0 ? (bidDepth - askDepth) / (bidDepth + askDepth) : 0;

  return {
    exchange: "synthetic",
    symbol: "synthetic",
    spread,
    spreadPercent,
    bidDepth,
    askDepth,
    imbalance,
    midPrice,
    timestamp: candle.timestamp,
  };
}

function checkExitLevels(
  position: BacktestPosition,
  candle: CandleLike,
): { stopLoss: boolean; takeProfit: boolean } {
  if (position.side === "long") {
    return {
      stopLoss: candle.low <= position.stopLoss,
      takeProfit: candle.high >= position.takeProfit,
    };
  }
  return {
    stopLoss: candle.high >= position.stopLoss,
    takeProfit: candle.low <= position.takeProfit,
  };
}

function shouldExitPosition(
  position: BacktestPosition,
  signal: ScalpingSignal,
): boolean {
  return (
    (position.side === "long" && signal.direction === "sell") ||
    (position.side === "short" && signal.direction === "buy")
  );
}

function isEntrySignal(signal: ScalpingSignal, minConfidence: number): boolean {
  return signal.direction !== "hold" && signal.confidence >= minConfidence;
}

function calculatePnl(position: BacktestPosition, exitPrice: number): number {
  const priceDiff =
    position.side === "long"
      ? exitPrice - position.entryPrice
      : position.entryPrice - exitPrice;
  return priceDiff * position.size;
}

function calculateSharpe(returns: readonly number[]): number {
  if (returns.length < 2) return 0;
  const mean = returns.reduce((a, b) => a + b, 0) / returns.length;
  const variance =
    returns.reduce((sum, r) => sum + (r - mean) ** 2, 0) / (returns.length - 1);
  const std = Math.sqrt(variance);
  return std === 0 ? 0 : mean / std;
}

function applySlippage(
  price: number,
  side: "long" | "short",
  bps: number,
  candleHigh: number,
  candleLow: number,
): number {
  if (bps === 0) return price;
  const factor = bps / 10000;
  const slipped = side === "long" ? price * (1 + factor) : price * (1 - factor);
  return Math.min(candleHigh, Math.max(candleLow, slipped));
}

function computeBenchmark(candles: readonly CandleLike[]): number {
  if (candles.length < 2) return 0;
  const first = candles[0].close;
  const last = candles[candles.length - 1].close;
  return first === 0 ? 0 : ((last - first) / first) * 100;
}

function chargeFunding(
  position: BacktestPosition,
  lastFundingTime: Date | null,
  now: Date,
  fundingRatePct: number,
  fundingIntervalMs: number,
  isFutures: boolean,
): number {
  if (!isFutures || fundingRatePct === 0 || !lastFundingTime) return 0;
  const elapsed = now.getTime() - lastFundingTime.getTime();
  if (elapsed < fundingIntervalMs) return 0;
  const intervals = Math.floor(elapsed / fundingIntervalMs);
  const notional = position.entryPrice * position.size;
  // Longs pay on positive rate; shorts pay on negative rate (inverse).
  const effectiveRate =
    position.side === "long" ? fundingRatePct : -fundingRatePct;
  return notional * (effectiveRate / 100) * intervals;
}
