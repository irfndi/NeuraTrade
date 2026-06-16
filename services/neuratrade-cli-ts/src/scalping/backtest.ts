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
        const exitPrice = stopLoss ? position.stopLoss : position.takeProfit;
        const reason = stopLoss ? "stop_loss" : "take_profit";
        const pnl = calculatePnl(position, exitPrice);
        const pnlPct = (pnl / (position.entryPrice * position.size)) * 100;
        capital += pnl;
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
        continue;
      }

      // Exit on signal reversal unless holding until stop/take-profit.
      if (
        !options.holdUntilStop &&
        signal &&
        shouldExitPosition(position, signal)
      ) {
        const exitPrice = next.open;
        const pnl = calculatePnl(position, exitPrice);
        const pnlPct = (pnl / (position.entryPrice * position.size)) * 100;
        capital += pnl;
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
      }
    }

    // Open new position if signal is strong enough and no position
    if (!position && signal && isEntrySignal(signal, options.minConfidence)) {
      const side = signal.direction === "buy" ? "long" : "short";
      const entryPrice = next.open;
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

      // Deduct fee on entry (simplified)
      capital -= positionValue * (options.feePct / 100);
    }
  }

  // Close any open position at the last candle
  if (position && candles.length > 0) {
    const last = candles[candles.length - 1];
    const exitPrice = last.close;
    const pnl = calculatePnl(position, exitPrice);
    const pnlPct = (pnl / (position.entryPrice * position.size)) * 100;
    capital += pnl;
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
