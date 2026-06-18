import type {
  CandleLike,
  ComposerConfig,
  Direction,
  OHLCVInput,
  OrderBookMetricsInput,
  ScalpingSignal,
} from "./types.js";
import { composeSignal } from "./composer.js";
import {
  calculateATR,
  calculateBollingerBands,
  calculateEMA,
  calculateRSI,
} from "./indicators.js";
import { computeExitLevels } from "./exit-engine.js";
import {
  computePerformanceMetrics,
  type BacktestMetrics,
} from "./performance-metrics.js";

export interface BacktestPosition {
  readonly entrySignal: ScalpingSignal;
  readonly entryPrice: number;
  readonly entryTime: Date;
  readonly side: "long" | "short";
  size: number;
  stopLoss: number;
  readonly takeProfit: number;
  readonly trailingStopAtr?: number;
  highestPrice: number;
  lowestPrice: number;
  scaledOut: boolean;
  readonly scaleOutPrice: number;
  readonly initialRiskPct: number;
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
  readonly exitReason: "signal" | "stop_loss" | "take_profit" | "scale_out";
  readonly initialRiskPct: number;
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
  readonly metrics: BacktestMetrics;
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
  /** Trail the stop-loss this percentage behind the most favorable price once in profit. */
  readonly trailingStopPct?: number;
  /** Trail the stop-loss at this ATR multiplier behind the most favorable price. */
  readonly trailingStopAtrMultiplier?: number;
  /** Minimum ATR% (ATR / price) required to enter a trade. Filters dead markets. */
  readonly minAtrPct?: number;
  /** When using ATR stops, set take-profit distance = stop distance * atrRiskReward.
   *  Overrides the legacy atrTakeProfitMultiplier behavior when > 0. */
  readonly atrRiskReward?: number;
  /** Partial scale-out trigger in R multiples (e.g. 1.0 = +1R). 0 disables. */
  readonly scaleOutAtR?: number;
  /** Percentage of the position to close at the scale-out trigger. Default 50. */
  readonly scaleOutPct?: number;
  /** Lookback window length for ATR% volatility percentile calibration. 0 disables. */
  readonly volatilityLookback?: number;
  /** Low percentile threshold for volatility calibration. */
  readonly volatilityLowPct?: number;
  /** High percentile threshold for volatility calibration. */
  readonly volatilityHighPct?: number;
  /** Multiplier applied to atrStopMultiplier in low-volatility regimes. */
  readonly volatilityLowFactor?: number;
  /** Multiplier applied to atrStopMultiplier in high-volatility regimes. */
  readonly volatilityHighFactor?: number;
  /** Risk a fixed percentage of current capital per trade instead of using a fixed position size.
   *  Position size = (capital * riskPerTradePct) / stopDistancePct. Overrides positionSizePct. */
  readonly riskPerTradePct?: number;
  /** Maximum position size as a percentage of capital when riskPerTradePct is used. */
  readonly maxPositionSizePct?: number;
  /** Require the same directional signal for this many consecutive candles before entering.
   *  0/1 disables the filter. Helps filter whipsaws. */
  readonly signalPersistence?: number;
  /** Additional min-confidence penalty applied after a losing trade.
   *  Decays by decay per candle. Helps avoid revenge trades in choppy regimes. */
  readonly lossConfidencePenalty?: number;
  readonly lossConfidenceDecay?: number;
  /** Optional higher-timeframe candles for a trend filter. When provided, entries
   *  are only allowed when the trade direction aligns with the HTF trend. */
  readonly htfCandles?: readonly CandleLike[];
  /** HTF EMA fast period. Default 50. */
  readonly htfTrendFastPeriod?: number;
  /** HTF EMA slow period. Default 100. */
  readonly htfTrendSlowPeriod?: number;
  /** When > 0, only enter when price is within this percentage of the EMA of
   *  the given period (pullback / value-entry filter). 0 disables. */
  readonly entryPullbackEmaPeriod?: number;
  readonly entryPullbackMarginPct?: number;
  /** Kaufman Efficiency Ratio filter. When > 0, only enter when the ER over the
   *  configured lookback is >= this value (0.0-1.0). 0 disables.
   *  High ER = strong trend, low ER = chop. */
  readonly minEfficiencyRatio?: number;
  readonly efficiencyRatioPeriod?: number;
  /** Leading RSI neutral-zone filter. For longs, require RSI <= rsiLongMax.
   *  For shorts, require RSI >= rsiShortMin. 0 disables. */
  readonly rsiLongMax?: number;
  readonly rsiShortMin?: number;
  /** Leading Bollinger %B pullback filter. For longs, require %B <= bollingerLongMaxPctB.
   *  For shorts, require %B >= bollingerShortMinPctB. Values outside [0,1] disable. */
  readonly bollingerLongMaxPctB?: number;
  readonly bollingerShortMinPctB?: number;
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
  let priorSignalDirection: Direction = "hold";
  let signalStreak = 0;
  const signalPersistence = Math.max(
    0,
    Math.floor(options.signalPersistence ?? 0),
  );
  let lossPenalty = 0;
  const lossConfidencePenalty = options.lossConfidencePenalty ?? 0;
  const lossConfidenceDecay = options.lossConfidenceDecay ?? 0;

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
    const currentDirection = signal?.direction ?? "hold";
    if (
      currentDirection !== "hold" &&
      currentDirection === priorSignalDirection
    ) {
      signalStreak += 1;
    } else {
      signalStreak = currentDirection === "hold" ? 0 : 1;
    }
    priorSignalDirection = currentDirection;
    if (lossPenalty > 0 && lossConfidenceDecay > 0) {
      lossPenalty = Math.max(0, lossPenalty - lossConfidenceDecay);
    }

    // Update running capital/drawdown if position open
    if (position) {
      const mtmPrice = current.close;
      const mtmPnl = calculatePnl(position, mtmPrice);
      const mtmCapital = capital + mtmPnl;
      if (mtmCapital > peakCapital) peakCapital = mtmCapital;
      const drawdown = (peakCapital - mtmCapital) / peakCapital;
      if (drawdown > maxDrawdown) maxDrawdown = drawdown;

      // Update trailing stop based on favorable price movement.
      updateTrailingStop(position, current, options);

      // Check partial scale-out before stop/take-profit.
      if (checkScaleOut(position, current)) {
        const scaleOutPct = Math.max(
          0,
          Math.min(100, options.scaleOutPct ?? 50),
        );
        const partialSize = position.size * (scaleOutPct / 100);
        if (partialSize > 0) {
          const exitSide = position.side === "long" ? "short" : "long";
          const exitPrice = applySlippage(
            position.scaleOutPrice,
            exitSide,
            slippageBps,
            current.high,
            current.low,
          );
          const pnl = calculatePnl(
            { ...position, size: partialSize },
            exitPrice,
          );
          const exitFee = exitPrice * partialSize * (options.feePct / 100);
          const pnlPct =
            ((pnl - exitFee) / (position.entryPrice * partialSize)) * 100;
          capital += pnl - exitFee;
          totalFeesPaid += exitFee;
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
            exitReason: "scale_out",
            initialRiskPct: position.initialRiskPct,
          });
          position.size -= partialSize;
          position.stopLoss = position.entryPrice;
          position.scaledOut = true;
        }
        continue;
      }

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
          lastFundingTime!,
          current.timestamp,
          fundingRatePct,
          fundingIntervalMs,
          isFutures,
        );
        const pnlPct =
          ((pnl - exitFee) / (position.entryPrice * position.size)) * 100;
        capital += pnl - exitFee - funding.funding;
        totalFeesPaid += exitFee;
        totalFundingCost += funding.funding;
        lastFundingTime = funding.newLastFundingTime;
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
          initialRiskPct: position.initialRiskPct,
        });
        if (pnl - exitFee - funding.funding < 0) {
          lossPenalty = lossConfidencePenalty;
        }
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
          lastFundingTime!,
          next.timestamp,
          fundingRatePct,
          fundingIntervalMs,
          isFutures,
        );
        const pnlPct =
          ((pnl - exitFee) / (position.entryPrice * position.size)) * 100;
        capital += pnl - exitFee - funding.funding;
        totalFeesPaid += exitFee;
        totalFundingCost += funding.funding;
        lastFundingTime = funding.newLastFundingTime;
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
          initialRiskPct: position.initialRiskPct,
        });
        if (pnl - exitFee - funding.funding < 0) {
          lossPenalty = lossConfidencePenalty;
        }
        position = null;
        lastFundingTime = null;
      }
    }

    // Open new position if signal is strong enough and no position
    const persistenceMet =
      signalPersistence <= 1 || signalStreak >= signalPersistence;
    const effectiveMinConfidence = options.minConfidence + lossPenalty;
    if (
      !position &&
      signal &&
      isEntrySignal(signal, effectiveMinConfidence) &&
      persistenceMet
    ) {
      const side = signal.direction === "buy" ? "long" : "short";

      if (
        options.htfCandles &&
        options.htfCandles.length > 0 &&
        !alignsWithHigherTimeframeTrend(
          side,
          options.htfCandles,
          options.htfTrendFastPeriod,
          options.htfTrendSlowPeriod,
        )
      ) {
        continue;
      }

      if (
        options.entryPullbackEmaPeriod &&
        options.entryPullbackEmaPeriod > 0 &&
        !priceWithinEmaPullback(
          current.close,
          window,
          options.entryPullbackEmaPeriod,
          options.entryPullbackMarginPct ?? 0.1,
        )
      ) {
        continue;
      }

      if (
        options.minEfficiencyRatio &&
        options.minEfficiencyRatio > 0 &&
        efficiencyRatio(window, options.efficiencyRatioPeriod ?? 20) <
          options.minEfficiencyRatio
      ) {
        continue;
      }

      if (
        (options.rsiLongMax || options.rsiShortMin) &&
        !passesRsiNeutralZone(
          window,
          side,
          options.rsiLongMax ?? 100,
          options.rsiShortMin ?? 0,
        )
      ) {
        continue;
      }

      const useBollingerLong =
        options.bollingerLongMaxPctB !== undefined &&
        options.bollingerLongMaxPctB >= 0 &&
        options.bollingerLongMaxPctB <= 1;
      const useBollingerShort =
        options.bollingerShortMinPctB !== undefined &&
        options.bollingerShortMinPctB >= 0 &&
        options.bollingerShortMinPctB <= 1;
      if (
        (useBollingerLong || useBollingerShort) &&
        !passesBollingerPullback(
          window,
          side,
          useBollingerLong ? options.bollingerLongMaxPctB! : 1,
          useBollingerShort ? options.bollingerShortMinPctB! : 0,
        )
      ) {
        continue;
      }

      const rawEntry = next.open;
      const entryPrice = applySlippage(
        rawEntry,
        side,
        slippageBps,
        next.high,
        next.low,
      );

      const needsAtr =
        options.useAtrStops ||
        options.trailingStopAtrMultiplier ||
        options.minAtrPct;
      const atr = needsAtr ? calculateATR(window, 14) : null;
      const useAtr = options.useAtrStops && atr !== null && atr > 0;
      const stopMult = options.atrStopMultiplier ?? 1.5;
      const tpMult = options.atrTakeProfitMultiplier ?? 2.5;

      if (options.minAtrPct && options.minAtrPct > 0) {
        const atrPct = atr && entryPrice > 0 ? atr / entryPrice : 0;
        if (atrPct < options.minAtrPct / 100) {
          continue;
        }
      }

      const atrRiskReward =
        options.atrRiskReward && options.atrRiskReward > 0
          ? options.atrRiskReward
          : tpMult / stopMult;
      const { stopLoss, takeProfit, scaleOutPrice } = computeExitLevels({
        side,
        entryPrice,
        atr,
        useAtr: !!useAtr,
        atrStopMultiplier: stopMult,
        atrRiskReward,
        stopLossPct: options.stopLossPct,
        takeProfitPct: options.takeProfitPct,
        scaleOutAtR: options.scaleOutAtR ?? 0,
        candles: window,
        volatilityLookback: options.volatilityLookback ?? 0,
        volatilityLowPct: options.volatilityLowPct ?? 20,
        volatilityHighPct: options.volatilityHighPct ?? 80,
        volatilityLowFactor: options.volatilityLowFactor ?? 0.8,
        volatilityHighFactor: options.volatilityHighFactor ?? 1.2,
      });

      const stopDistancePct =
        entryPrice > 0 ? Math.abs(entryPrice - stopLoss) / entryPrice : 0;
      const initialRiskPct = stopDistancePct;
      const positionValue = calculatePositionValue(
        capital,
        entryPrice,
        stopDistancePct,
        options,
      );
      const size = positionValue / entryPrice;

      position = {
        entrySignal: signal,
        entryPrice,
        entryTime: next.timestamp,
        side,
        size,
        stopLoss,
        takeProfit,
        trailingStopAtr: useAtr ? atr : undefined,
        highestPrice: entryPrice,
        lowestPrice: entryPrice,
        scaledOut: false,
        scaleOutPrice: scaleOutPrice ?? 0,
        initialRiskPct,
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
      lastFundingTime!,
      last.timestamp,
      fundingRatePct,
      fundingIntervalMs,
      isFutures,
    );
    const pnlPct =
      ((pnl - exitFee) / (position.entryPrice * position.size)) * 100;
    capital += pnl - exitFee - funding.funding;
    totalFeesPaid += exitFee;
    totalFundingCost += funding.funding;
    lastFundingTime = funding.newLastFundingTime;
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
      initialRiskPct: position.initialRiskPct,
    });
    if (pnl - exitFee - funding.funding < 0) {
      lossPenalty = lossConfidencePenalty;
    }
  }

  const winningTrades = trades.filter((t) => t.pnl > 0).length;
  const losingTrades = trades.filter((t) => t.pnl < 0).length;
  const totalReturnPct =
    ((capital - options.initialCapital) / options.initialCapital) * 100;
  const returns = trades.map((t) => t.pnlPct);
  const sharpe = calculateSharpe(returns);

  const candleSpanMs =
    candles.length > 1
      ? candles[candles.length - 1].timestamp.getTime() -
        candles[0].timestamp.getTime()
      : 0;

  const metrics = computePerformanceMetrics({
    trades,
    initialCapital: options.initialCapital,
    maxDrawdownPct: maxDrawdown * 100,
    totalReturnPct,
    candleSpanMs,
  });

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
    metrics,
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
    metrics: {
      profitFactor: 0,
      expectancy: 0,
      averageRMultiple: 0,
      sortinoRatio: 0,
      calmarRatio: 0,
      maxConsecutiveLosses: 0,
      averageTradeDurationHours: 0,
      timeInMarketPct: 0,
    },
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

function alignsWithHigherTimeframeTrend(
  side: "long" | "short",
  htfCandles: readonly CandleLike[],
  fastPeriod = 50,
  slowPeriod = 100,
): boolean {
  if (htfCandles.length < slowPeriod + 1) return true;
  const closes = htfCandles.map((c) => c.close);
  const fastEMA = calculateEMA(closes, fastPeriod);
  const slowEMA = calculateEMA(closes, slowPeriod);
  const lastFast = fastEMA[fastEMA.length - 1];
  const lastSlow = slowEMA[slowEMA.length - 1];
  if (Number.isNaN(lastFast) || Number.isNaN(lastSlow) || lastSlow === 0) {
    return true;
  }
  const htfUp = lastFast > lastSlow;
  return side === "long" ? htfUp : !htfUp;
}

function efficiencyRatio(candles: readonly CandleLike[], period = 20): number {
  if (candles.length < period + 1) return 0;
  const start = candles[candles.length - period - 1].close;
  const end = candles[candles.length - 1].close;
  if (start === 0) return 0;
  const netChange = Math.abs(end - start);
  let sumChanges = 0;
  for (let i = candles.length - period; i < candles.length; i++) {
    sumChanges += Math.abs(candles[i].close - candles[i - 1].close);
  }
  return sumChanges === 0 ? 0 : netChange / sumChanges;
}

function passesRsiNeutralZone(
  candles: readonly CandleLike[],
  side: "long" | "short",
  rsiLongMax: number,
  rsiShortMin: number,
): boolean {
  const rsi = calculateRSI(candles, 14);
  if (rsi === null) return true;
  if (side === "long") return rsi <= rsiLongMax;
  return rsi >= rsiShortMin;
}

function passesBollingerPullback(
  candles: readonly CandleLike[],
  side: "long" | "short",
  longMaxPctB: number,
  shortMinPctB: number,
): boolean {
  const bb = calculateBollingerBands(candles, 20);
  if (bb === null) return true;
  if (side === "long") return bb.percentB <= longMaxPctB;
  return bb.percentB >= shortMinPctB;
}

function priceWithinEmaPullback(
  price: number,
  candles: readonly CandleLike[],
  period: number,
  marginPct: number,
): boolean {
  if (candles.length < period + 1 || price <= 0) return true;
  const closes = candles.map((c) => c.close);
  const emaSeries = calculateEMA(closes, period);
  const ema = emaSeries[emaSeries.length - 1];
  if (Number.isNaN(ema) || ema <= 0) return true;
  const margin = marginPct / 100;
  return price >= ema * (1 - margin) && price <= ema * (1 + margin);
}

function calculatePositionValue(
  capital: number,
  entryPrice: number,
  stopDistancePct: number,
  options: BacktestOptions,
): number {
  const maxPositionValue =
    capital * ((options.maxPositionSizePct ?? 100) / 100);

  if (
    options.riskPerTradePct &&
    options.riskPerTradePct > 0 &&
    stopDistancePct > 0
  ) {
    const riskAmount = capital * (options.riskPerTradePct / 100);
    const riskBasedValue = riskAmount / stopDistancePct;
    return Math.min(riskBasedValue, maxPositionValue);
  }

  const fixedValue = capital * (options.positionSizePct / 100);
  return Math.min(fixedValue, maxPositionValue);
}

function checkScaleOut(
  position: BacktestPosition,
  candle: CandleLike,
): boolean {
  if (position.scaledOut || position.scaleOutPrice <= 0) return false;
  if (position.side === "long") {
    return candle.high >= position.scaleOutPrice;
  }
  return candle.low <= position.scaleOutPrice;
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

function updateTrailingStop(
  position: BacktestPosition,
  candle: CandleLike,
  options: BacktestOptions,
): void {
  if (!options.trailingStopPct && !options.trailingStopAtrMultiplier) return;

  if (position.side === "long") {
    if (candle.high > position.highestPrice) {
      position.highestPrice = candle.high;
    }
  } else {
    if (candle.low < position.lowestPrice) {
      position.lowestPrice = candle.low;
    }
  }

  let trailDistance: number | null = null;
  if (options.trailingStopPct && options.trailingStopPct > 0) {
    const referencePrice =
      position.side === "long" ? position.highestPrice : position.lowestPrice;
    trailDistance = referencePrice * (options.trailingStopPct / 100);
  } else if (
    options.trailingStopAtrMultiplier &&
    options.trailingStopAtrMultiplier > 0 &&
    position.trailingStopAtr
  ) {
    trailDistance =
      position.trailingStopAtr * options.trailingStopAtrMultiplier;
  }

  if (trailDistance === null || trailDistance <= 0) return;

  if (position.side === "long") {
    const candidateStop = position.highestPrice - trailDistance;
    if (candidateStop > position.stopLoss) {
      position.stopLoss = candidateStop;
    }
  } else {
    const candidateStop = position.lowestPrice + trailDistance;
    if (candidateStop < position.stopLoss) {
      position.stopLoss = candidateStop;
    }
  }
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
  lastFundingTime: Date,
  now: Date,
  fundingRatePct: number,
  fundingIntervalMs: number,
  isFutures: boolean,
): { funding: number; newLastFundingTime: Date } {
  if (!isFutures || fundingRatePct === 0) {
    return { funding: 0, newLastFundingTime: lastFundingTime };
  }
  const elapsed = now.getTime() - lastFundingTime.getTime();
  if (elapsed < fundingIntervalMs) {
    return { funding: 0, newLastFundingTime: lastFundingTime };
  }
  const intervals = Math.floor(elapsed / fundingIntervalMs);
  const notional = position.entryPrice * position.size;
  const effectiveRate =
    position.side === "long" ? fundingRatePct : -fundingRatePct;
  const funding = notional * (effectiveRate / 100) * intervals;
  const newLastFundingTime = new Date(
    lastFundingTime.getTime() + intervals * fundingIntervalMs,
  );
  return { funding, newLastFundingTime };
}
