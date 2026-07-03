import type {
  CandleLike,
  ComposerConfig,
  Direction,
  FundingRate,
} from "./types.js";
import { composeSignal } from "./composer.js";
import { calculateATR } from "./indicators.js";
import { computeExitLevels } from "./exit-engine.js";
import type { BacktestMetrics } from "./performance-metrics.js";
import { resolveEntryFill, normalizeOptionalFeePct } from "./backtest.js";
import { computeSymbolStats } from "./symbol-stats.js";
import type { SymbolStatistics } from "./symbol-stats.js";

export interface PortfolioBacktestOptions {
  readonly symbol: string;
  readonly exchange: string;
  readonly timeframe: string;
  readonly candles: readonly CandleLike[];
  readonly composerConfig: ComposerConfig;
  /** Optional historical funding rates for the funding bias component. */
  readonly fundingRates?: readonly FundingRate[];
  readonly initialCapital: number;
  readonly positionSizePct: number;
  readonly maxOpenPositions: number;
  readonly stopLossPct: number;
  readonly takeProfitPct: number;
  readonly feePct: number;
  readonly makerFeePct?: number;
  readonly entryOrderType?: "market" | "limit";
  readonly entryLimitOffsetBps?: number;
  readonly minConfidence: number;
  readonly maxBarsInTrade?: number;
  readonly sessionStart?: string;
  readonly sessionEnd?: string;
  readonly slippageBps?: number;
  readonly useAtrStops?: boolean;
  readonly atrStopMultiplier?: number;
  readonly atrTakeProfitMultiplier?: number;
  readonly atrRiskReward?: number;
  readonly useAdaptiveStops?: boolean;
  readonly adaptiveStopAtrMultiplier?: number;
  readonly adaptiveRiskReward?: number;
  readonly scaleOutAtR?: number;
  readonly scaleOutPct?: number;
  readonly volatilityLookback?: number;
  readonly volatilityLowPct?: number;
  readonly volatilityHighPct?: number;
  readonly volatilityLowFactor?: number;
  readonly volatilityHighFactor?: number;
  readonly riskPerTradePct?: number;
  readonly maxPositionSizePct?: number;
  readonly maxPortfolioHeatPct?: number;
  readonly correlationFilter?: boolean;
  readonly correlationLookback?: number;
  readonly correlationThreshold?: number;
  readonly observedPrice?: boolean;
}

export type PortfolioBacktestEngineOptions = Omit<
  PortfolioBacktestOptions,
  "symbol" | "candles"
>;

export interface PortfolioBacktestTrade {
  readonly id: string;
  readonly symbol: string;
  readonly side: "long" | "short";
  readonly entryTime: Date;
  readonly exitTime: Date;
  readonly entryPrice: number;
  readonly exitPrice: number;
  readonly pnl: number;
  readonly pnlPct: number;
  readonly netPnl: number;
  readonly exitReason: "take_profit" | "stop_loss" | "signal" | "time_stop";
  readonly barsHeld: number;
}

export interface PortfolioBacktestResult {
  readonly symbol: string;
  readonly totalTrades: number;
  readonly winningTrades: number;
  readonly losingTrades: number;
  readonly winRate: number;
  readonly totalReturnPct: number;
  readonly maxDrawdownPct: number;
  readonly sharpeRatio: number;
  readonly profitFactor: number;
  readonly avgTradeDurationHours: number;
  readonly trades: readonly PortfolioBacktestTrade[];
  readonly totalFeesPaid: number;
  readonly metrics: BacktestMetrics;
}

interface PortfolioPosition {
  id: string;
  side: "long" | "short";
  entryPrice: number;
  entryTime: Date;
  entryBarIndex: number;
  size: number;
  stopLoss: number;
  takeProfit: number;
  entryFeePaid: number;
  fillType: "maker" | "taker";
}

interface GlobalPosition extends PortfolioPosition {
  readonly symbolIndex: number;
  readonly symbol: string;
  readonly initialRisk: number;
  pendingExit?: {
    readonly reason: PortfolioBacktestTrade["exitReason"];
    readonly targetBarIndex: number;
  };
}

interface SymbolSeries {
  readonly symbol: string;
  readonly candles: readonly CandleLike[];
  readonly returns: readonly number[];
  readonly timestampToIndex: ReadonlyMap<string, number>;
  readonly stats: SymbolStatistics;
  readonly fundingRates?: readonly FundingRate[];
  currentIndex: number;
  currentPrice: number;
  pendingEntry?: {
    readonly side: "long" | "short";
    readonly signalBarIndex: number;
  };
}

interface GlobalState {
  cash: number;
  positions: GlobalPosition[];
  trades: PortfolioBacktestTrade[];
  tradeId: number;
  peakEquity: number;
  maxDrawdown: number;
  totalFeesPaid: number;
}

function applySlippage(
  price: number,
  side: "long" | "short",
  slippageBps: number,
  high: number,
  low: number,
): number {
  if (slippageBps <= 0) return price;
  const factor = slippageBps / 10000;
  return side === "long"
    ? Math.min(high, price * (1 + factor))
    : Math.max(low, price * (1 - factor));
}

function isWithinSession(timestamp: Date, start: string, end: string): boolean {
  if (!start || !end) return true;
  const mins = timestamp.getUTCHours() * 60 + timestamp.getUTCMinutes();
  const [sh, sm] = start.split(":").map(Number);
  const [eh, em] = end.split(":").map(Number);
  const startMins = sh * 60 + sm;
  const endMins = eh * 60 + em;
  if (startMins <= endMins) return mins >= startMins && mins <= endMins;
  return mins >= startMins || mins <= endMins;
}

function normalizeFeePct(feePct: number): number {
  if (feePct > 0 && feePct < 0.01) return feePct * 100;
  return feePct;
}

function candleDurationMinutes(timeframe: string): number {
  const unit = timeframe.slice(-1);
  const value = Number.parseInt(timeframe.slice(0, -1), 10);
  if (Number.isNaN(value)) return 5;
  if (unit === "m") return value;
  if (unit === "h") return value * 60;
  if (unit === "d") return value * 1440;
  return 5;
}

function computePortfolioMetrics(
  trades: PortfolioBacktestTrade[],
): BacktestMetrics {
  if (trades.length === 0) {
    return {
      profitFactor: 0,
      expectancy: 0,
      averageRMultiple: 0,
      sortinoRatio: 0,
      calmarRatio: 0,
      maxConsecutiveLosses: 0,
      averageTradeDurationHours: 0,
      timeInMarketPct: 0,
    };
  }
  const wins = trades.filter((t) => t.netPnl > 0);
  const losses = trades.filter((t) => t.netPnl < 0);
  const grossProfit = wins.reduce((sum, t) => sum + t.netPnl, 0);
  const grossLoss = Math.abs(losses.reduce((sum, t) => sum + t.netPnl, 0));
  let maxConsecLosses = 0;
  let current = 0;
  for (const t of trades) {
    if (t.netPnl < 0) {
      current += 1;
      maxConsecLosses = Math.max(maxConsecLosses, current);
    } else {
      current = 0;
    }
  }
  return {
    profitFactor:
      grossLoss === 0
        ? grossProfit > 0
          ? Infinity
          : 0
        : grossProfit / grossLoss,
    expectancy: trades.reduce((sum, t) => sum + t.netPnl, 0) / trades.length,
    averageRMultiple: 0,
    sortinoRatio: 0,
    calmarRatio: 0,
    maxConsecutiveLosses: maxConsecLosses,
    averageTradeDurationHours:
      trades.reduce((sum, t) => sum + t.barsHeld, 0) / trades.length,
    timeInMarketPct: 0,
  };
}

export function runPortfolioBacktest(
  options: PortfolioBacktestOptions,
): PortfolioBacktestResult {
  const candles = options.candles;
  if (candles.length < 20) {
    return emptyPortfolioResult(options.symbol);
  }

  const feePct = normalizeFeePct(options.feePct);
  const makerFeePct = normalizeOptionalFeePct(options.makerFeePct, "maker-fee");
  const entryOrderType = options.entryOrderType ?? "market";
  const entryLimitOffsetBps = options.entryLimitOffsetBps ?? 0;
  const slippageBps = options.slippageBps ?? 0;
  const maxBarsInTrade = Math.max(0, Math.floor(options.maxBarsInTrade ?? 0));
  const sessionStart = options.sessionStart ?? "";
  const sessionEnd = options.sessionEnd ?? "";
  const useAtr = options.useAtrStops ?? false;
  const atrStopMultiplier = options.atrStopMultiplier ?? 1.5;
  const atrTakeProfitMultiplier = options.atrTakeProfitMultiplier ?? 2.5;
  let atrRiskReward = options.atrRiskReward ?? 0;
  if (useAtr && atrRiskReward <= 0 && atrTakeProfitMultiplier > 0) {
    atrRiskReward = atrTakeProfitMultiplier / Math.max(1e-9, atrStopMultiplier);
  }
  const scaleOutAtR = options.scaleOutAtR ?? 0;
  const volatilityLookback = options.volatilityLookback ?? 0;
  const volatilityLowPct = options.volatilityLowPct ?? 20;
  const volatilityHighPct = options.volatilityHighPct ?? 80;
  const volatilityLowFactor = options.volatilityLowFactor ?? 0.8;
  const volatilityHighFactor = options.volatilityHighFactor ?? 1.2;
  const symbolStats = computeSymbolStats(candles, options.timeframe);

  let capital = options.initialCapital;
  let peakCapital = capital;
  let maxDrawdown = 0;
  let tradeId = 0;
  let totalFeesPaid = 0;

  const positions: PortfolioPosition[] = [];
  const trades: PortfolioBacktestTrade[] = [];

  const recordTrade = (
    position: PortfolioPosition,
    exitPrice: number,
    exitTime: Date,
    exitReason: PortfolioBacktestTrade["exitReason"],
    barIndex: number,
  ) => {
    const notional = position.entryPrice * position.size;
    const rawPnl =
      position.side === "long"
        ? (exitPrice - position.entryPrice) * position.size
        : (position.entryPrice - exitPrice) * position.size;
    const exitFee = exitPrice * position.size * (feePct / 100);
    const netPnl = rawPnl - exitFee - position.entryFeePaid;
    capital += netPnl;
    totalFeesPaid += exitFee + position.entryFeePaid;

    trades.push({
      id: position.id,
      symbol: options.symbol,
      side: position.side,
      entryTime: position.entryTime,
      exitTime,
      entryPrice: position.entryPrice,
      exitPrice,
      pnl: rawPnl,
      pnlPct: (rawPnl / notional) * 100,
      netPnl,
      exitReason,
      barsHeld: barIndex - position.entryBarIndex,
    });

    if (capital > peakCapital) peakCapital = capital;
    const drawdown = (peakCapital - capital) / peakCapital;
    if (drawdown > maxDrawdown) maxDrawdown = drawdown;
  };

  for (let i = 20; i < candles.length - 1; i++) {
    const window = candles.slice(Math.max(0, i + 1 - 200), i + 1);
    const current = candles[i];
    const next = candles[i + 1];

    if (!isWithinSession(current.timestamp, sessionStart, sessionEnd)) {
      continue;
    }

    const signal = composeSignal(
      {
        exchange: options.exchange,
        symbol: options.symbol,
        timeframe: options.timeframe,
        candles: window,
        fundingRates: options.fundingRates,
      },
      {
        exchange: "synthetic",
        symbol: "synthetic",
        spread: current.high - current.low,
        spreadPercent: (current.high - current.low) / current.close,
        bidDepth:
          ((current.close - current.low) / (current.high - current.low || 1)) *
          100,
        askDepth:
          ((current.high - current.close) / (current.high - current.low || 1)) *
          100,
        imbalance: 0,
        midPrice: current.close,
        timestamp: current.timestamp,
      },
      options.composerConfig,
    );

    // Manage existing positions
    const remaining: PortfolioPosition[] = [];
    for (const position of positions) {
      let closed = false;
      const exitSide = position.side === "long" ? "short" : "long";

      // SL/TP checks on current bar
      if (position.side === "long") {
        if (current.low <= position.stopLoss) {
          recordTrade(
            position,
            applySlippage(
              position.stopLoss,
              exitSide,
              slippageBps,
              current.high,
              current.low,
            ),
            current.timestamp,
            "stop_loss",
            i,
          );
          closed = true;
        } else if (current.high >= position.takeProfit) {
          recordTrade(
            position,
            applySlippage(
              position.takeProfit,
              exitSide,
              slippageBps,
              current.high,
              current.low,
            ),
            current.timestamp,
            "take_profit",
            i,
          );
          closed = true;
        }
      } else {
        if (current.high >= position.stopLoss) {
          recordTrade(
            position,
            applySlippage(
              position.stopLoss,
              exitSide,
              slippageBps,
              current.high,
              current.low,
            ),
            current.timestamp,
            "stop_loss",
            i,
          );
          closed = true;
        } else if (current.low <= position.takeProfit) {
          recordTrade(
            position,
            applySlippage(
              position.takeProfit,
              exitSide,
              slippageBps,
              current.high,
              current.low,
            ),
            current.timestamp,
            "take_profit",
            i,
          );
          closed = true;
        }
      }

      if (
        !closed &&
        maxBarsInTrade > 0 &&
        i - position.entryBarIndex >= maxBarsInTrade
      ) {
        recordTrade(
          position,
          applySlippage(
            current.close,
            exitSide,
            slippageBps,
            current.high,
            current.low,
          ),
          current.timestamp,
          "time_stop",
          i,
        );
        closed = true;
      }

      if (!closed && signal) {
        const signalSide = signal.direction === "buy" ? "long" : "short";
        if (signalSide !== position.side) {
          recordTrade(
            position,
            applySlippage(
              next.open,
              exitSide,
              slippageBps,
              next.high,
              next.low,
            ),
            next.timestamp,
            "signal",
            i + 1,
          );
          closed = true;
        }
      }

      if (!closed) remaining.push(position);
    }
    positions.length = 0;
    positions.push(...remaining);

    // Open new position if signal and capacity
    if (
      signal &&
      signal.direction !== "hold" &&
      signal.confidence >= options.minConfidence &&
      positions.length < options.maxOpenPositions
    ) {
      const side = signal.direction === "buy" ? "long" : "short";
      const rawEntry = next.open;
      const fillResult = resolveEntryFill(
        rawEntry,
        next,
        side,
        feePct,
        makerFeePct,
        entryOrderType,
        entryLimitOffsetBps,
        slippageBps,
      );
      if (!fillResult.filled) {
        continue;
      }
      const entryPrice = fillResult.entryPrice;
      const appliedFeePct = fillResult.appliedFeePct;
      const fillType = fillResult.fillType;
      const atr = calculateATR(window, 14);
      const { stopLoss, takeProfit } = computeExitLevels({
        side,
        entryPrice,
        atr,
        useAtr,
        atrStopMultiplier,
        atrRiskReward,
        stopLossPct: options.stopLossPct,
        takeProfitPct: options.takeProfitPct,
        scaleOutAtR,
        candles: window,
        volatilityLookback,
        volatilityLowPct,
        volatilityHighPct,
        volatilityLowFactor,
        volatilityHighFactor,
        symbolStats,
        useAdaptiveStops: options.useAdaptiveStops,
        adaptiveStopAtrMultiplier: options.adaptiveStopAtrMultiplier,
        adaptiveRiskReward: options.adaptiveRiskReward,
      });

      // Size each slot as a fixed fraction of initial capital so multiple
      // concurrent positions do not silently over-leverage the account.
      const positionValue =
        (options.initialCapital * options.positionSizePct) / 100;
      const size = positionValue / entryPrice;
      const entryFee = positionValue * (appliedFeePct / 100);

      positions.push({
        id: `trade-${tradeId++}`,
        side,
        entryPrice,
        entryTime: next.timestamp,
        entryBarIndex: i + 1,
        size,
        stopLoss,
        takeProfit,
        entryFeePaid: entryFee,
        fillType,
      });

      totalFeesPaid += entryFee;
    }

    const mtmCapital =
      capital +
      positions.reduce((sum, p) => {
        const price = current.close;
        return (
          sum +
          (p.side === "long"
            ? (price - p.entryPrice) * p.size
            : (p.entryPrice - price) * p.size)
        );
      }, 0);
    if (mtmCapital > peakCapital) peakCapital = mtmCapital;
    const drawdown = (peakCapital - mtmCapital) / peakCapital;
    if (drawdown > maxDrawdown) maxDrawdown = drawdown;
  }

  // Close all remaining positions at last close
  const last = candles[candles.length - 1];
  for (const position of positions) {
    const exitSide = position.side === "long" ? "short" : "long";
    recordTrade(
      position,
      applySlippage(last.close, exitSide, slippageBps, last.high, last.low),
      last.timestamp,
      "signal",
      candles.length - 1,
    );
  }

  const winningTrades = trades.filter((t) => t.netPnl > 0).length;
  const losingTrades = trades.filter((t) => t.netPnl < 0).length;
  const totalReturnPct =
    ((capital - options.initialCapital) / options.initialCapital) * 100;

  const avgDurationHours =
    trades.length > 0
      ? (trades.reduce((sum, t) => sum + t.barsHeld, 0) / trades.length) *
        (candleDurationMinutes(options.timeframe) / 60)
      : 0;

  return {
    symbol: options.symbol,
    totalTrades: trades.length,
    winningTrades,
    losingTrades,
    winRate: trades.length > 0 ? winningTrades / trades.length : 0,
    totalReturnPct,
    maxDrawdownPct: maxDrawdown * 100,
    sharpeRatio: 0,
    profitFactor: computePortfolioMetrics(trades).profitFactor,
    avgTradeDurationHours: avgDurationHours,
    trades,
    totalFeesPaid,
    metrics: computePortfolioMetrics(trades),
  };
}

export interface MultiSymbolPortfolioInput {
  readonly symbol: string;
  readonly candles: readonly CandleLike[];
  readonly fundingRates?: readonly FundingRate[];
}

export interface MultiSymbolPortfolioResult {
  readonly symbolCount: number;
  readonly totalTrades: number;
  readonly winningTrades: number;
  readonly losingTrades: number;
  readonly winRate: number;
  readonly totalReturnPct: number;
  readonly maxDrawdownPct: number;
  readonly profitFactor: number;
  readonly avgTradeDurationHours: number;
  readonly maxConsecutiveLosses: number;
  readonly symbolResults: readonly PortfolioBacktestResult[];
}

function emptyPortfolioResult(symbol: string): PortfolioBacktestResult {
  return {
    symbol,
    totalTrades: 0,
    winningTrades: 0,
    losingTrades: 0,
    winRate: 0,
    totalReturnPct: 0,
    maxDrawdownPct: 0,
    sharpeRatio: 0,
    profitFactor: 0,
    avgTradeDurationHours: 0,
    trades: [],
    totalFeesPaid: 0,
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

function emptyMultiSymbolResult(): MultiSymbolPortfolioResult {
  return {
    symbolCount: 0,
    totalTrades: 0,
    winningTrades: 0,
    losingTrades: 0,
    winRate: 0,
    totalReturnPct: 0,
    maxDrawdownPct: 0,
    profitFactor: 0,
    avgTradeDurationHours: 0,
    maxConsecutiveLosses: 0,
    symbolResults: [],
  };
}

function buildSymbolSeries(
  input: MultiSymbolPortfolioInput,
  symbolIndex: number,
  timeframe: string,
): SymbolSeries {
  const sorted = [...input.candles].sort(
    (a, b) => a.timestamp.getTime() - b.timestamp.getTime(),
  );
  const timestampToIndex = new Map<string, number>();
  sorted.forEach((c, i) => {
    timestampToIndex.set(c.timestamp.toISOString(), i);
  });
  const returns = sorted.map((c, i) => {
    if (i === 0) return 0;
    const prev = sorted[i - 1];
    return prev.close !== 0 ? (c.close - prev.close) / prev.close : 0;
  });
  return {
    symbol: input.symbol,
    candles: sorted,
    returns,
    timestampToIndex,
    stats: computeSymbolStats(sorted, timeframe),
    fundingRates: input.fundingRates,
    currentIndex: 0,
    currentPrice: sorted.length > 0 ? sorted[sorted.length - 1].close : 0,
  };
}

function computeSignal(
  series: SymbolSeries,
  barIndex: number,
  composerConfig: ComposerConfig,
  timeframe: string,
): ReturnType<typeof composeSignal> {
  const window = series.candles.slice(
    Math.max(0, barIndex + 1 - 200),
    barIndex + 1,
  );
  const current = series.candles[barIndex];
  const configWithStats: ComposerConfig = {
    ...composerConfig,
    thresholds: {
      ...composerConfig.thresholds,
      symbolStats: series.stats,
    },
  };
  return composeSignal(
    {
      exchange: "synthetic",
      symbol: series.symbol,
      timeframe,
      candles: window,
      fundingRates: series.fundingRates,
    },
    {
      exchange: "synthetic",
      symbol: "synthetic",
      spread: current.high - current.low,
      spreadPercent: (current.high - current.low) / current.close,
      bidDepth:
        ((current.close - current.low) / (current.high - current.low || 1)) *
        100,
      askDepth:
        ((current.high - current.close) / (current.high - current.low || 1)) *
        100,
      imbalance: 0,
      midPrice: current.close,
      timestamp: current.timestamp,
    },
    configWithStats,
  );
}

function computeEquity(
  state: GlobalState,
  symbols: readonly SymbolSeries[],
): number {
  let equity = state.cash;
  for (const position of state.positions) {
    const series = symbols[position.symbolIndex];
    const price = series ? series.currentPrice : position.entryPrice;
    equity +=
      position.side === "long"
        ? (price - position.entryPrice) * position.size
        : (position.entryPrice - price) * position.size;
  }
  return equity;
}

function updateDrawdown(state: GlobalState, equity: number): void {
  if (equity > state.peakEquity) state.peakEquity = equity;
  const drawdown = (state.peakEquity - equity) / state.peakEquity;
  if (drawdown > state.maxDrawdown) state.maxDrawdown = drawdown;
}

function pearsonCorrelation(
  xs: readonly number[],
  ys: readonly number[],
): number {
  const n = Math.min(xs.length, ys.length);
  if (n < 2) return 0;
  let sumX = 0;
  let sumY = 0;
  for (let i = 0; i < n; i++) {
    sumX += xs[i];
    sumY += ys[i];
  }
  const meanX = sumX / n;
  const meanY = sumY / n;
  let num = 0;
  let denX = 0;
  let denY = 0;
  for (let i = 0; i < n; i++) {
    const dx = xs[i] - meanX;
    const dy = ys[i] - meanY;
    num += dx * dy;
    denX += dx * dx;
    denY += dy * dy;
  }
  if (denX <= 0 || denY <= 0) return 0;
  return num / Math.sqrt(denX * denY);
}

function correlationBlocks(
  candidate: SymbolSeries,
  openPositions: readonly GlobalPosition[],
  symbols: readonly SymbolSeries[],
  lookback: number,
  threshold: number,
): boolean {
  if (openPositions.length === 0) return false;
  const end = candidate.currentIndex;
  const start = Math.max(1, end - lookback + 1);
  const timestamps: { readonly ts: string; readonly idx: number }[] = [];
  for (let i = start; i <= end; i++) {
    timestamps.push({
      ts: candidate.candles[i].timestamp.toISOString(),
      idx: i,
    });
  }
  for (const position of openPositions) {
    if (position.symbol === candidate.symbol) continue;
    const other = symbols[position.symbolIndex];
    if (!other) continue;
    const candidateAligned: number[] = [];
    const otherReturns: number[] = [];
    for (const { ts, idx } of timestamps) {
      const otherIdx = other.timestampToIndex.get(ts);
      if (otherIdx !== undefined && otherIdx > 0) {
        candidateAligned.push(candidate.returns[idx]);
        otherReturns.push(other.returns[otherIdx]);
      }
    }
    if (candidateAligned.length < Math.max(5, Math.floor(lookback / 2)))
      continue;
    const correlation = Math.abs(
      pearsonCorrelation(candidateAligned, otherReturns),
    );
    if (correlation >= threshold) return true;
  }
  return false;
}

function computeCandidateExitLevels(
  series: SymbolSeries,
  barIndex: number,
  side: "long" | "short",
  entryPrice: number,
  options: PortfolioBacktestEngineOptions,
): { readonly stopLoss: number; readonly takeProfit: number } {
  // Use the bar before the entry bar so the current candle's close/high/low
  // are not leaked into the stop calculation.
  const signalBarIndex = Math.max(0, barIndex - 1);
  const window = series.candles.slice(
    Math.max(0, signalBarIndex + 1 - 200),
    signalBarIndex + 1,
  );
  const atr = calculateATR(window, 14);
  const useAtr = options.useAtrStops ?? false;
  const atrStopMultiplier = options.atrStopMultiplier ?? 1.5;
  const atrTakeProfitMultiplier = options.atrTakeProfitMultiplier ?? 2.5;
  let atrRiskReward = options.atrRiskReward ?? 0;
  if (useAtr && atrRiskReward <= 0 && atrTakeProfitMultiplier > 0) {
    atrRiskReward = atrTakeProfitMultiplier / Math.max(1e-9, atrStopMultiplier);
  }
  return computeExitLevels({
    side,
    entryPrice,
    atr,
    useAtr,
    atrStopMultiplier,
    atrRiskReward,
    stopLossPct: options.stopLossPct,
    takeProfitPct: options.takeProfitPct,
    scaleOutAtR: 0,
    candles: window,
    volatilityLookback: options.volatilityLookback ?? 0,
    volatilityLowPct: options.volatilityLowPct ?? 20,
    volatilityHighPct: options.volatilityHighPct ?? 80,
    volatilityLowFactor: options.volatilityLowFactor ?? 0.8,
    volatilityHighFactor: options.volatilityHighFactor ?? 1.2,
    symbolStats: series.stats,
    useAdaptiveStops: options.useAdaptiveStops,
    adaptiveStopAtrMultiplier: options.adaptiveStopAtrMultiplier,
    adaptiveRiskReward: options.adaptiveRiskReward,
  });
}

function canEnter(
  state: GlobalState,
  series: SymbolSeries,
  side: "long" | "short",
  barIndex: number,
  options: PortfolioBacktestEngineOptions,
  symbols: readonly SymbolSeries[],
  entryPrice: number,
): boolean {
  const maxOpenPositions = options.maxOpenPositions ?? 1;
  if (state.positions.length >= maxOpenPositions) return false;

  const maxPortfolioHeatPct = options.maxPortfolioHeatPct ?? 100;
  if (maxPortfolioHeatPct < 100) {
    const equity = computeEquity(state, symbols);
    if (equity <= 0) return false;
    const existingRisk = state.positions.reduce(
      (sum, p) => sum + p.initialRisk,
      0,
    );
    const { stopLoss } = computeCandidateExitLevels(
      series,
      barIndex,
      side,
      entryPrice,
      options,
    );
    const stopDistancePct =
      entryPrice > 0 ? Math.abs(entryPrice - stopLoss) / entryPrice : 0;
    const riskPerTradePct = options.riskPerTradePct ?? 0;
    let newRisk: number;
    if (riskPerTradePct > 0) {
      newRisk = (equity * riskPerTradePct) / 100;
    } else {
      const positionValue = (equity * options.positionSizePct) / 100;
      newRisk = positionValue * stopDistancePct;
    }
    const heat = ((existingRisk + newRisk) / equity) * 100;
    if (heat > maxPortfolioHeatPct) return false;
  }

  if (options.correlationFilter) {
    const lookback = options.correlationLookback ?? 50;
    const threshold = options.correlationThreshold ?? 0.8;
    if (
      correlationBlocks(series, state.positions, symbols, lookback, threshold)
    ) {
      return false;
    }
  }

  return true;
}

function openPosition(
  state: GlobalState,
  series: SymbolSeries,
  barIndex: number,
  candle: CandleLike,
  side: "long" | "short",
  options: PortfolioBacktestEngineOptions,
  symbols: readonly SymbolSeries[],
): void {
  const feePct = normalizeFeePct(options.feePct);
  const makerFeePct = normalizeOptionalFeePct(options.makerFeePct, "maker-fee");
  const entryOrderType = options.entryOrderType ?? "market";
  const entryLimitOffsetBps = options.entryLimitOffsetBps ?? 0;
  const slippageBps = options.slippageBps ?? 0;

  const fillResult = resolveEntryFill(
    candle.open,
    candle,
    side,
    feePct,
    makerFeePct,
    entryOrderType,
    entryLimitOffsetBps,
    slippageBps,
  );
  if (!fillResult.filled) return;

  const entryPrice = fillResult.entryPrice;
  const appliedFeePct = fillResult.appliedFeePct;
  const fillType = fillResult.fillType;

  const { stopLoss, takeProfit } = computeCandidateExitLevels(
    series,
    barIndex,
    side,
    entryPrice,
    options,
  );
  const stopDistancePct =
    entryPrice > 0 ? Math.abs(entryPrice - stopLoss) / entryPrice : 0;

  const equity = computeEquity(state, symbols);
  if (equity <= 0 || entryPrice <= 0) return;

  const riskPerTradePct = options.riskPerTradePct ?? 0;
  let size: number;
  if (riskPerTradePct > 0) {
    const riskAmount = (equity * riskPerTradePct) / 100;
    const stopDistance = Math.abs(entryPrice - stopLoss);
    if (stopDistance <= 0) return;
    size = riskAmount / stopDistance;
    const maxSize =
      (equity * (options.maxPositionSizePct ?? 100)) / 100 / entryPrice;
    size = Math.min(size, maxSize);
  } else {
    const positionValue = (equity * options.positionSizePct) / 100;
    size = positionValue / entryPrice;
  }
  if (!Number.isFinite(size) || size <= 0) return;

  const notional = entryPrice * size;
  const entryFee = notional * (appliedFeePct / 100);
  const initialRisk = notional * stopDistancePct;

  state.cash -= entryFee;
  state.totalFeesPaid += entryFee;
  state.positions.push({
    id: `trade-${state.tradeId++}`,
    symbolIndex: symbols.indexOf(series),
    symbol: series.symbol,
    side,
    entryPrice,
    entryTime: candle.timestamp,
    entryBarIndex: barIndex,
    size,
    stopLoss,
    takeProfit,
    entryFeePaid: entryFee,
    fillType,
    initialRisk,
  });
}

function closePosition(
  state: GlobalState,
  position: GlobalPosition,
  exitPrice: number,
  exitTime: Date,
  exitReason: PortfolioBacktestTrade["exitReason"],
  barIndex: number,
  feePct: number,
): PortfolioBacktestTrade {
  const notional = position.entryPrice * position.size;
  const rawPnl =
    position.side === "long"
      ? (exitPrice - position.entryPrice) * position.size
      : (position.entryPrice - exitPrice) * position.size;
  const exitFee = exitPrice * position.size * (feePct / 100);
  const netPnl = rawPnl - exitFee - position.entryFeePaid;
  state.cash += netPnl;
  state.totalFeesPaid += exitFee + position.entryFeePaid;

  const trade: PortfolioBacktestTrade = {
    id: position.id,
    symbol: position.symbol,
    side: position.side,
    entryTime: position.entryTime,
    exitTime,
    entryPrice: position.entryPrice,
    exitPrice,
    pnl: rawPnl,
    pnlPct: notional > 0 ? (rawPnl / notional) * 100 : 0,
    netPnl,
    exitReason,
    barsHeld: barIndex - position.entryBarIndex,
  };
  state.trades.push(trade);
  return trade;
}

export function runMultiSymbolPortfolioBacktest(
  baseOptions: PortfolioBacktestEngineOptions & {
    readonly initialCapital: number;
    readonly symbols: readonly MultiSymbolPortfolioInput[];
  },
): MultiSymbolPortfolioResult {
  const inputs = baseOptions.symbols;
  if (inputs.length === 0) {
    return emptyMultiSymbolResult();
  }

  const symbols = inputs.map((input, idx) =>
    buildSymbolSeries(input, idx, baseOptions.timeframe),
  );

  const globalBars: {
    readonly symbolIndex: number;
    readonly barIndex: number;
    readonly timestamp: number;
  }[] = [];
  for (let s = 0; s < symbols.length; s++) {
    const series = symbols[s];
    for (let i = 20; i < series.candles.length; i++) {
      globalBars.push({
        symbolIndex: s,
        barIndex: i,
        timestamp: series.candles[i].timestamp.getTime(),
      });
    }
  }
  globalBars.sort((a, b) => a.timestamp - b.timestamp);

  const state: GlobalState = {
    cash: baseOptions.initialCapital,
    positions: [],
    trades: [],
    tradeId: 0,
    peakEquity: baseOptions.initialCapital,
    maxDrawdown: 0,
    totalFeesPaid: 0,
  };

  const feePct = normalizeFeePct(baseOptions.feePct);
  const slippageBps = baseOptions.slippageBps ?? 0;
  const maxBarsInTrade = Math.max(
    0,
    Math.floor(baseOptions.maxBarsInTrade ?? 0),
  );
  const sessionStart = baseOptions.sessionStart ?? "";
  const sessionEnd = baseOptions.sessionEnd ?? "";
  const minConfidence = baseOptions.minConfidence;

  for (const bar of globalBars) {
    const series = symbols[bar.symbolIndex];
    const candle = series.candles[bar.barIndex];
    series.currentIndex = bar.barIndex;
    series.currentPrice = candle.close;

    if (!isWithinSession(candle.timestamp, sessionStart, sessionEnd)) {
      updateDrawdown(state, computeEquity(state, symbols));
      continue;
    }

    // Close any signal-triggered exits scheduled for this bar's open.
    const afterPendingCloses: GlobalPosition[] = [];
    for (const position of state.positions) {
      if (
        position.symbol === series.symbol &&
        position.pendingExit &&
        position.pendingExit.targetBarIndex === bar.barIndex
      ) {
        const exitSide = position.side === "long" ? "short" : "long";
        closePosition(
          state,
          position,
          applySlippage(
            candle.open,
            exitSide,
            slippageBps,
            candle.high,
            candle.low,
          ),
          candle.timestamp,
          position.pendingExit.reason,
          bar.barIndex,
          feePct,
        );
        continue;
      }
      afterPendingCloses.push(position);
    }
    state.positions = afterPendingCloses;

    // Open any pending entry scheduled for this bar's open.
    if (series.pendingEntry) {
      if (
        canEnter(
          state,
          series,
          series.pendingEntry.side,
          bar.barIndex,
          baseOptions,
          symbols,
          candle.open,
        )
      ) {
        openPosition(
          state,
          series,
          bar.barIndex,
          candle,
          series.pendingEntry.side,
          baseOptions,
          symbols,
        );
      }
      series.pendingEntry = undefined;
    }

    const signal = computeSignal(
      series,
      bar.barIndex,
      baseOptions.composerConfig,
      baseOptions.timeframe,
    );

    // Manage existing positions for this symbol on the current candle.
    const remaining: GlobalPosition[] = [];
    for (const position of state.positions) {
      if (position.symbol !== series.symbol) {
        remaining.push(position);
        continue;
      }
      let closed = false;
      const exitSide = position.side === "long" ? "short" : "long";

      if (position.side === "long") {
        if (candle.low <= position.stopLoss) {
          closePosition(
            state,
            position,
            applySlippage(
              position.stopLoss,
              exitSide,
              slippageBps,
              candle.high,
              candle.low,
            ),
            candle.timestamp,
            "stop_loss",
            bar.barIndex,
            feePct,
          );
          closed = true;
        } else if (candle.high >= position.takeProfit) {
          closePosition(
            state,
            position,
            applySlippage(
              position.takeProfit,
              exitSide,
              slippageBps,
              candle.high,
              candle.low,
            ),
            candle.timestamp,
            "take_profit",
            bar.barIndex,
            feePct,
          );
          closed = true;
        }
      } else {
        if (candle.high >= position.stopLoss) {
          closePosition(
            state,
            position,
            applySlippage(
              position.stopLoss,
              exitSide,
              slippageBps,
              candle.high,
              candle.low,
            ),
            candle.timestamp,
            "stop_loss",
            bar.barIndex,
            feePct,
          );
          closed = true;
        } else if (candle.low <= position.takeProfit) {
          closePosition(
            state,
            position,
            applySlippage(
              position.takeProfit,
              exitSide,
              slippageBps,
              candle.high,
              candle.low,
            ),
            candle.timestamp,
            "take_profit",
            bar.barIndex,
            feePct,
          );
          closed = true;
        }
      }

      if (
        !closed &&
        maxBarsInTrade > 0 &&
        bar.barIndex - position.entryBarIndex >= maxBarsInTrade
      ) {
        closePosition(
          state,
          position,
          applySlippage(
            candle.close,
            exitSide,
            slippageBps,
            candle.high,
            candle.low,
          ),
          candle.timestamp,
          "time_stop",
          bar.barIndex,
          feePct,
        );
        closed = true;
      }

      if (!closed && signal) {
        const signalSide = signal.direction === "buy" ? "long" : "short";
        if (signalSide !== position.side) {
          if (bar.barIndex + 1 < series.candles.length) {
            position.pendingExit = {
              reason: "signal",
              targetBarIndex: bar.barIndex + 1,
            };
            remaining.push(position);
          } else {
            closePosition(
              state,
              position,
              applySlippage(
                candle.close,
                exitSide,
                slippageBps,
                candle.high,
                candle.low,
              ),
              candle.timestamp,
              "signal",
              bar.barIndex,
              feePct,
            );
          }
          continue;
        }
      }

      if (!closed) remaining.push(position);
    }
    state.positions = remaining;

    // Schedule a new entry for the next bar if we have a fresh signal.
    if (
      signal &&
      signal.direction !== "hold" &&
      signal.confidence >= minConfidence &&
      bar.barIndex + 1 < series.candles.length &&
      !state.positions.some((p) => p.symbol === series.symbol)
    ) {
      const signalSide = signal.direction === "buy" ? "long" : "short";
      series.pendingEntry = {
        side: signalSide,
        signalBarIndex: bar.barIndex,
      };
    }

    updateDrawdown(state, computeEquity(state, symbols));
  }

  // Close any remaining positions at each symbol's last known price.
  for (const position of state.positions) {
    const series = symbols[position.symbolIndex];
    const lastCandle = series.candles[series.candles.length - 1];
    const exitSide = position.side === "long" ? "short" : "long";
    closePosition(
      state,
      position,
      applySlippage(
        series.currentPrice,
        exitSide,
        slippageBps,
        lastCandle ? lastCandle.high : series.currentPrice,
        lastCandle ? lastCandle.low : series.currentPrice,
      ),
      lastCandle ? lastCandle.timestamp : new Date(0),
      "signal",
      series.candles.length - 1,
      feePct,
    );
  }
  state.positions = [];

  const symbolResults = symbols.map((series) =>
    computeSymbolResult(series, state.trades, baseOptions),
  );
  const allTrades = state.trades
    .slice()
    .sort((a, b) => a.exitTime.getTime() - b.exitTime.getTime());

  const winningTrades = allTrades.filter((t) => t.netPnl > 0).length;
  const losingTrades = allTrades.filter((t) => t.netPnl < 0).length;
  const grossProfit = allTrades
    .filter((t) => t.netPnl > 0)
    .reduce((sum, t) => sum + t.netPnl, 0);
  const grossLoss = Math.abs(
    allTrades.filter((t) => t.netPnl < 0).reduce((sum, t) => sum + t.netPnl, 0),
  );

  let maxConsecLosses = 0;
  let currentStreak = 0;
  for (const trade of allTrades) {
    if (trade.netPnl < 0) {
      currentStreak += 1;
      maxConsecLosses = Math.max(maxConsecLosses, currentStreak);
    } else {
      currentStreak = 0;
    }
  }

  const avgDurationHours =
    allTrades.length > 0
      ? (allTrades.reduce((sum, t) => sum + t.barsHeld, 0) / allTrades.length) *
        (candleDurationMinutes(baseOptions.timeframe) / 60)
      : 0;

  return {
    symbolCount: symbols.length,
    totalTrades: allTrades.length,
    winningTrades,
    losingTrades,
    winRate: allTrades.length > 0 ? winningTrades / allTrades.length : 0,
    totalReturnPct:
      ((state.cash - baseOptions.initialCapital) / baseOptions.initialCapital) *
      100,
    maxDrawdownPct: state.maxDrawdown * 100,
    profitFactor: grossLoss === 0 ? 0 : grossProfit / grossLoss,
    avgTradeDurationHours: avgDurationHours,
    maxConsecutiveLosses: maxConsecLosses,
    symbolResults,
  };
}

function computeSymbolResult(
  series: SymbolSeries,
  allTrades: readonly PortfolioBacktestTrade[],
  options: { readonly initialCapital: number; readonly timeframe: string },
): PortfolioBacktestResult {
  const trades = allTrades.filter((t) => t.symbol === series.symbol);
  const winningTrades = trades.filter((t) => t.netPnl > 0).length;
  const losingTrades = trades.filter((t) => t.netPnl < 0).length;
  const netPnl = trades.reduce((sum, t) => sum + t.netPnl, 0);
  const avgDurationHours =
    trades.length > 0
      ? (trades.reduce((sum, t) => sum + t.barsHeld, 0) / trades.length) *
        (candleDurationMinutes(options.timeframe) / 60)
      : 0;

  return {
    symbol: series.symbol,
    totalTrades: trades.length,
    winningTrades,
    losingTrades,
    winRate: trades.length > 0 ? winningTrades / trades.length : 0,
    totalReturnPct: (netPnl / options.initialCapital) * 100,
    maxDrawdownPct: 0,
    sharpeRatio: 0,
    profitFactor: computePortfolioMetrics(trades).profitFactor,
    avgTradeDurationHours: avgDurationHours,
    trades,
    totalFeesPaid: trades.reduce((sum, t) => sum + (t.pnl - t.netPnl), 0),
    metrics: computePortfolioMetrics(trades),
  };
}
