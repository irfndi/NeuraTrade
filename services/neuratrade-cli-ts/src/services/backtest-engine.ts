/**
 * Local deterministic scalping backtest engine.
 *
 * Reads OHLCV candles from SQLite, runs a pure-TS strategy (EMA + RSI + volume
 * filter with SL/TP/trailing/time-stop exits), and returns trade-level metrics.
 * No backend or LLM is involved.
 */
import { Effect } from "effect";
import type { Candle } from "./market-repository.ts";
import { MarketRepository } from "./market-repository.ts";
import { SqliteError } from "./sqlite.ts";
import {
  abs,
  add,
  compare,
  divide,
  max,
  min,
  multiply,
  subtract,
} from "./decimal.ts";

// ---------------------------------------------------------------------------
// Types
// ---------------------------------------------------------------------------

export interface LocalBacktestConfig {
  readonly exchange: string;
  readonly timeframe: string;
  readonly symbols: ReadonlyArray<string>;
  readonly start: Date;
  readonly end: Date;
  readonly initialCapital: string;
  readonly feeRate: string;
  readonly leverage: number;
  readonly riskPct: string;
  readonly stopLossPct: string;
  readonly takeProfitPct: string;
  readonly trailingStopPct: string;
  readonly maxHoldHours: number;
  readonly maxOpenPositions: number;
  readonly fastEmaPeriod: number;
  readonly slowEmaPeriod: number;
  readonly rsiPeriod: number;
  readonly rsiLongMax: number;
  readonly rsiShortMin: number;
  readonly volumeLookback: number;
  readonly atrPeriod: number;
  readonly atrMaxPct: number;
  readonly minVolumeRatio: number;
  readonly adxPeriod: number;
  readonly adxMin: number;
  readonly cooldownCandles: number;
  readonly minTrendDistancePct: number;
  readonly rsiExitLevel: number;
  readonly trendEmaPeriod: number;
  readonly breakoutPeriod: number;
  readonly slippagePct: string;
}

export interface BacktestTrade {
  readonly symbol: string;
  readonly side: "long" | "short";
  readonly entryAt: string;
  readonly exitAt: string;
  readonly entryPrice: string;
  readonly exitPrice: string;
  readonly size: string;
  readonly notional: string;
  readonly pnl: string;
  readonly pnlPct: string;
  readonly fees: string;
  readonly exitReason: string;
}

export interface LocalBacktestResult {
  readonly initialCapital: string;
  readonly finalCapital: string;
  readonly totalPnl: string;
  readonly totalPnlPct: string;
  readonly totalTrades: number;
  readonly winningTrades: number;
  readonly losingTrades: number;
  readonly winRate: string;
  readonly profitFactor: string;
  readonly maxDrawdownPct: string;
  readonly sharpeRatio: string;
  readonly trades: ReadonlyArray<BacktestTrade>;
}

interface OpenTrade {
  readonly symbol: string;
  readonly side: "long" | "short";
  readonly entryPrice: string;
  readonly entryAt: string;
  readonly size: string;
  readonly notional: string;
  readonly stopLoss: string;
  readonly takeProfit: string;
  readonly trailingStop: string;
}

// ---------------------------------------------------------------------------
// Decimal helpers
// ---------------------------------------------------------------------------

const zero = "0";
const hundred = "100";

// ---------------------------------------------------------------------------
// Technical indicators
// ---------------------------------------------------------------------------

function ema(
  values: ReadonlyArray<number>,
  period: number,
): Array<number | null> {
  const out: Array<number | null> = [];
  if (period <= 0 || values.length < period) {
    for (let i = 0; i < values.length; i++) out.push(null);
    return out;
  }
  const k = 2 / (period + 1);
  let prev: number | null = null;
  for (let i = 0; i < values.length; i++) {
    if (i < period - 1) {
      out.push(null);
      continue;
    }
    if (prev === null) {
      let sum = 0;
      for (let j = i - period + 1; j <= i; j++) sum += values[j];
      prev = sum / period;
    } else {
      prev = values[i] * k + prev * (1 - k);
    }
    out.push(prev);
  }
  return out;
}

function sma(
  values: ReadonlyArray<number>,
  period: number,
): Array<number | null> {
  const out: Array<number | null> = [];
  let sum = 0;
  for (let i = 0; i < values.length; i++) {
    sum += values[i];
    if (i >= period) sum -= values[i - period];
    if (i >= period - 1) out.push(sum / period);
    else out.push(null);
  }
  return out;
}

function rsi(
  closes: ReadonlyArray<number>,
  period: number,
): Array<number | null> {
  const out: Array<number | null> = [];
  let avgGain = 0;
  let avgLoss = 0;
  for (let i = 0; i < closes.length; i++) {
    if (i === 0) {
      out.push(null);
      continue;
    }
    const delta = closes[i] - closes[i - 1];
    const gain = delta > 0 ? delta : 0;
    const loss = delta < 0 ? -delta : 0;
    if (i < period) {
      avgGain += gain / period;
      avgLoss += loss / period;
      out.push(null);
      continue;
    }
    if (i === period) {
      // seed already accumulated
    } else {
      avgGain = (avgGain * (period - 1) + gain) / period;
      avgLoss = (avgLoss * (period - 1) + loss) / period;
    }
    if (avgLoss === 0) out.push(100);
    else {
      const rs = avgGain / avgLoss;
      out.push(100 - 100 / (1 + rs));
    }
  }
  return out;
}

function atr(
  highs: ReadonlyArray<number>,
  lows: ReadonlyArray<number>,
  closes: ReadonlyArray<number>,
  period: number,
): Array<number | null> {
  const out: Array<number | null> = [];
  let prevAtr: number | null = null;
  for (let i = 0; i < closes.length; i++) {
    const tr1 = highs[i] - lows[i];
    const tr2 = i > 0 ? Math.abs(highs[i] - closes[i - 1]) : tr1;
    const tr3 = i > 0 ? Math.abs(lows[i] - closes[i - 1]) : tr1;
    const tr = Math.max(tr1, tr2, tr3);
    if (i < period - 1) {
      out.push(null);
      continue;
    }
    if (prevAtr === null) {
      let sum = 0;
      for (let j = i - period + 1; j <= i; j++) {
        const t1 = highs[j] - lows[j];
        const t2 = j > 0 ? Math.abs(highs[j] - closes[j - 1]) : t1;
        const t3 = j > 0 ? Math.abs(lows[j] - closes[j - 1]) : t1;
        sum += Math.max(t1, t2, t3);
      }
      prevAtr = sum / period;
    } else {
      prevAtr = (prevAtr * (period - 1) + tr) / period;
    }
    out.push(prevAtr);
  }
  return out;
}

function adx(
  highs: ReadonlyArray<number>,
  lows: ReadonlyArray<number>,
  closes: ReadonlyArray<number>,
  period: number,
): Array<number | null> {
  const out: Array<number | null> = [];
  let prevDmPlus = 0;
  let prevDmMinus = 0;
  let prevTr = 0;
  let prevAdx: number | null = null;

  for (let i = 0; i < closes.length; i++) {
    if (i === 0) {
      out.push(null);
      continue;
    }
    const upMove = highs[i] - highs[i - 1];
    const downMove = lows[i - 1] - lows[i];
    const dmPlus = upMove > downMove && upMove > 0 ? upMove : 0;
    const dmMinus = downMove > upMove && downMove > 0 ? downMove : 0;
    const tr1 = highs[i] - lows[i];
    const tr2 = Math.abs(highs[i] - closes[i - 1]);
    const tr3 = Math.abs(lows[i] - closes[i - 1]);
    const tr = Math.max(tr1, tr2, tr3);

    if (i < period) {
      prevDmPlus += dmPlus / period;
      prevDmMinus += dmMinus / period;
      prevTr += tr / period;
      out.push(null);
      continue;
    }

    if (i === period) {
      // seed already accumulated
    } else {
      prevDmPlus = (prevDmPlus * (period - 1) + dmPlus) / period;
      prevDmMinus = (prevDmMinus * (period - 1) + dmMinus) / period;
      prevTr = (prevTr * (period - 1) + tr) / period;
    }

    const diPlus = prevTr === 0 ? 0 : (100 * prevDmPlus) / prevTr;
    const diMinus = prevTr === 0 ? 0 : (100 * prevDmMinus) / prevTr;
    const dx =
      diPlus + diMinus === 0
        ? 0
        : (100 * Math.abs(diPlus - diMinus)) / (diPlus + diMinus);

    if (prevAdx === null) {
      prevAdx = dx;
      out.push(dx);
    } else {
      prevAdx = (prevAdx * (period - 1) + dx) / period;
      out.push(prevAdx);
    }
  }
  return out;
}

// ---------------------------------------------------------------------------
// Signal generation
// ---------------------------------------------------------------------------

interface SignalSnapshot {
  readonly fastEma: number;
  readonly slowEma: number;
  readonly trendEma: number;
  readonly rsi: number;
  readonly volumeSma: number;
  readonly atr: number;
  readonly adx: number;
  readonly highestHigh: number;
  readonly lowestLow: number;
  readonly close: number;
  readonly volume: number;
}

function generateSignal(
  snapshot: SignalSnapshot,
  prevSnapshot: SignalSnapshot | null,
  config: LocalBacktestConfig,
): "long" | "short" | null {
  const atrPct = snapshot.atr / snapshot.close;
  if (atrPct > config.atrMaxPct / 100) return null;
  if (snapshot.volumeSma <= 0) return null;
  if (snapshot.volume / snapshot.volumeSma < config.minVolumeRatio) return null;
  if (snapshot.adx < config.adxMin) return null;

  const inUptrend =
    config.trendEmaPeriod <= 0 || snapshot.close > snapshot.trendEma;
  const inDowntrend =
    config.trendEmaPeriod <= 0 || snapshot.close < snapshot.trendEma;

  // Volatility breakout: close above/below the recent N-candle range.
  const brokeHigh =
    prevSnapshot !== null && snapshot.close > snapshot.highestHigh;
  const brokeLow = prevSnapshot !== null && snapshot.close < snapshot.lowestLow;

  if (inUptrend && brokeHigh) {
    return "long";
  }
  if (inDowntrend && brokeLow) {
    return "short";
  }

  return null;
}

// ---------------------------------------------------------------------------
// Trade math
// ---------------------------------------------------------------------------

function stopLossPrice(
  entry: string,
  side: "long" | "short",
  slPct: string,
): string {
  return side === "long"
    ? multiply(entry, subtract("1", slPct))
    : multiply(entry, add("1", slPct));
}

function takeProfitPrice(
  entry: string,
  side: "long" | "short",
  tpPct: string,
): string {
  return side === "long"
    ? multiply(entry, add("1", tpPct))
    : multiply(entry, subtract("1", tpPct));
}

function positionSize(
  capital: string,
  riskPct: string,
  entryPrice: string,
  stopLossPct: string,
): { readonly size: string; readonly notional: string } {
  const riskAmount = multiply(capital, riskPct);
  const slDistance = multiply(entryPrice, stopLossPct);
  const size = divide(riskAmount, slDistance);
  const notional = multiply(size, entryPrice);
  return { size, notional };
}

function applyEntrySlippage(
  price: string,
  side: "long" | "short",
  slippagePct: string,
): string {
  if (compare(slippagePct, zero) <= 0) return price;
  return side === "long"
    ? multiply(price, add("1", slippagePct))
    : multiply(price, subtract("1", slippagePct));
}

function applyExitSlippage(
  price: string,
  side: "long" | "short",
  slippagePct: string,
): string {
  if (compare(slippagePct, zero) <= 0) return price;
  return side === "long"
    ? multiply(price, subtract("1", slippagePct))
    : multiply(price, add("1", slippagePct));
}

function closePnl(
  trade: OpenTrade,
  exitPrice: string,
  leverage: number,
  feeRate: string,
  slippagePct: string,
): { readonly pnl: string; readonly pnlPct: string; readonly fees: string } {
  const filledExitPrice = applyExitSlippage(exitPrice, trade.side, slippagePct);
  const priceDelta =
    trade.side === "long"
      ? subtract(filledExitPrice, trade.entryPrice)
      : subtract(trade.entryPrice, filledExitPrice);
  const gross = multiply(priceDelta, trade.size);
  const leveragedGross = multiply(gross, String(leverage));
  const exitNotional = multiply(trade.size, filledExitPrice);
  const fees = multiply(add(trade.notional, exitNotional), feeRate);
  const pnl = subtract(leveragedGross, fees);
  const pnlPct =
    compare(trade.notional, zero) === 0
      ? zero
      : multiply(divide(pnl, trade.notional), hundred);
  return { pnl, pnlPct, fees };
}

function updateTrailingStop(trade: OpenTrade, candle: Candle): OpenTrade {
  if (compare(trade.trailingStop, zero) === 0) return trade;
  if (trade.side === "long") {
    const trail = multiply(candle.high, subtract("1", trade.trailingStop));
    return { ...trade, trailingStop: max(trade.trailingStop, trail) };
  }
  const trail = multiply(candle.low, add("1", trade.trailingStop));
  return { ...trade, trailingStop: min(trade.trailingStop, trail) };
}

// ---------------------------------------------------------------------------
// Per-symbol simulation
// ---------------------------------------------------------------------------

interface SymbolResult {
  readonly trades: BacktestTrade[];
  readonly finalCapital: string;
}

function runSymbolBacktest(
  symbol: string,
  candles: ReadonlyArray<Candle>,
  startCapital: string,
  config: LocalBacktestConfig,
): SymbolResult {
  if (candles.length === 0) {
    return { trades: [], finalCapital: startCapital };
  }

  const closes = candles.map((c) => Number(c.close));
  const highs = candles.map((c) => Number(c.high));
  const lows = candles.map((c) => Number(c.low));
  const volumes = candles.map((c) => Number(c.volume));

  const fastEma = ema(closes, config.fastEmaPeriod);
  const slowEma = ema(closes, config.slowEmaPeriod);
  const trendEma =
    config.trendEmaPeriod > 0
      ? ema(closes, config.trendEmaPeriod)
      : closes.map(() => 0);
  const rsiValues = rsi(closes, config.rsiPeriod);
  const volumeSma = sma(volumes, config.volumeLookback);
  const atrValues = atr(highs, lows, closes, config.atrPeriod);
  const adxValues = adx(highs, lows, closes, config.adxPeriod);

  const highestHighs: Array<number | null> = [];
  const lowestLows: Array<number | null> = [];
  for (let i = 0; i < highs.length; i++) {
    if (i < config.breakoutPeriod) {
      highestHighs.push(null);
      lowestLows.push(null);
    } else {
      const windowHighs = highs.slice(i - config.breakoutPeriod, i);
      const windowLows = lows.slice(i - config.breakoutPeriod, i);
      highestHighs.push(Math.max(...windowHighs));
      lowestLows.push(Math.min(...windowLows));
    }
  }

  const lookback = Math.max(
    config.fastEmaPeriod,
    config.slowEmaPeriod,
    config.trendEmaPeriod,
    config.rsiPeriod,
    config.volumeLookback,
    config.atrPeriod,
    config.adxPeriod * 2,
    config.breakoutPeriod,
  );

  let capital = startCapital;
  const riskCapital = startCapital;
  let openTrade: OpenTrade | null = null;
  const trades: BacktestTrade[] = [];
  let lastExitIndex = -Infinity;

  for (let i = lookback; i < candles.length; i++) {
    const candle = candles[i];
    const fast = fastEma[i];
    const slow = slowEma[i];
    const trend = trendEma[i];
    const rsiVal = rsiValues[i];
    const volSma = volumeSma[i];
    const atrVal = atrValues[i];
    const adxVal = adxValues[i];
    const highestHigh = highestHighs[i];
    const lowestLow = lowestLows[i];
    if (
      fast == null ||
      slow == null ||
      trend == null ||
      rsiVal == null ||
      volSma == null ||
      atrVal == null ||
      adxVal == null ||
      highestHigh == null ||
      lowestLow == null
    ) {
      continue;
    }

    // First handle exit of an open position using this candle.
    if (openTrade !== null) {
      openTrade = updateTrailingStop(openTrade, candle);
      const sl = openTrade.stopLoss;
      const tp = openTrade.takeProfit;
      let exitPrice: string | null = null;
      let exitReason = "";

      // RSI mean-reversion exit. Use rsiExitLevel as the long overbought
      // threshold and (100 - rsiExitLevel) as the short oversold threshold.
      // When rsiExitLevel >= 100 the exit is effectively disabled.
      const shortRsiExitLevel = 100 - config.rsiExitLevel;
      if (openTrade.side === "long" && rsiVal >= config.rsiExitLevel) {
        exitPrice = candle.close;
        exitReason = "rsi_exit";
      } else if (openTrade.side === "short" && rsiVal <= shortRsiExitLevel) {
        exitPrice = candle.close;
        exitReason = "rsi_exit";
      }

      if (exitPrice === null) {
        if (openTrade.side === "long") {
          if (compare(candle.low, sl) <= 0) {
            exitPrice = sl;
            exitReason = "stop_loss";
          } else if (compare(candle.high, tp) >= 0) {
            exitPrice = tp;
            exitReason = "take_profit";
          } else if (compare(candle.low, openTrade.trailingStop) <= 0) {
            exitPrice = openTrade.trailingStop;
            exitReason = "trailing_stop";
          }
        } else {
          if (compare(candle.high, sl) >= 0) {
            exitPrice = sl;
            exitReason = "stop_loss";
          } else if (compare(candle.low, tp) <= 0) {
            exitPrice = tp;
            exitReason = "take_profit";
          } else if (compare(candle.high, openTrade.trailingStop) >= 0) {
            exitPrice = openTrade.trailingStop;
            exitReason = "trailing_stop";
          }
        }
      }

      // Time stop.
      if (exitPrice === null) {
        const entryTime = new Date(openTrade.entryAt).getTime();
        const holdHours = (candle.timestamp.getTime() - entryTime) / 3_600_000;
        if (holdHours >= config.maxHoldHours) {
          exitPrice = candle.close;
          exitReason = "time_stop";
        }
      }

      if (exitPrice !== null) {
        const { pnl, pnlPct, fees } = closePnl(
          openTrade,
          exitPrice,
          config.leverage,
          config.feeRate,
          config.slippagePct,
        );
        capital = add(capital, pnl);
        trades.push({
          symbol,
          side: openTrade.side,
          entryAt: openTrade.entryAt,
          exitAt: candle.timestamp.toISOString(),
          entryPrice: openTrade.entryPrice,
          exitPrice,
          size: openTrade.size,
          notional: openTrade.notional,
          pnl,
          pnlPct,
          fees,
          exitReason,
        });
        openTrade = null;
        lastExitIndex = i;
      }
    }

    // Cooldown after a closed trade.
    if (i - lastExitIndex <= config.cooldownCandles) continue;

    // No new entry if already in a trade for this symbol.
    if (openTrade !== null) continue;

    const snapshot: SignalSnapshot = {
      fastEma: fast,
      slowEma: slow,
      trendEma: trend,
      rsi: rsiVal,
      volumeSma: volSma,
      atr: atrVal,
      adx: adxVal,
      highestHigh,
      lowestLow,
      close: Number(candle.close),
      volume: Number(candle.volume),
    };
    const prevSnapshot: SignalSnapshot | null =
      fastEma[i - 1] != null &&
      slowEma[i - 1] != null &&
      trendEma[i - 1] != null &&
      rsiValues[i - 1] != null &&
      adxValues[i - 1] != null &&
      highestHighs[i - 1] != null &&
      lowestLows[i - 1] != null
        ? {
            fastEma: fastEma[i - 1]!,
            slowEma: slowEma[i - 1]!,
            trendEma: trendEma[i - 1]!,
            rsi: rsiValues[i - 1]!,
            volumeSma: volumeSma[i - 1] ?? 0,
            atr: atrValues[i - 1] ?? 0,
            adx: adxValues[i - 1] ?? 0,
            highestHigh: highestHighs[i - 1]!,
            lowestLow: lowestLows[i - 1]!,
            close: Number(candles[i - 1].close),
            volume: Number(candles[i - 1].volume),
          }
        : null;

    const signal = generateSignal(snapshot, prevSnapshot, config);
    if (signal === null) continue;

    // Enter at the breakout level rather than the candle close for a tighter,
    // more realistic fill price. Adverse slippage is applied to the fill price.
    const rawEntryPrice =
      signal === "long"
        ? String(snapshot.highestHigh)
        : String(snapshot.lowestLow);
    const entryPrice = applyEntrySlippage(
      rawEntryPrice,
      signal,
      config.slippagePct,
    );
    const { size, notional } = positionSize(
      riskCapital,
      config.riskPct,
      entryPrice,
      config.stopLossPct,
    );
    if (compare(size, zero) <= 0 || compare(notional, zero) <= 0) continue;

    const side = signal;
    const sl = stopLossPrice(entryPrice, side, config.stopLossPct);
    const tp = takeProfitPrice(entryPrice, side, config.takeProfitPct);
    const trailingPct =
      compare(config.trailingStopPct, zero) > 0 ? config.trailingStopPct : zero;
    const initialTrailing =
      side === "long"
        ? multiply(entryPrice, subtract("1", trailingPct))
        : multiply(entryPrice, add("1", trailingPct));

    openTrade = {
      symbol,
      side,
      entryPrice,
      entryAt: candle.timestamp.toISOString(),
      size,
      notional,
      stopLoss: sl,
      takeProfit: tp,
      trailingStop: initialTrailing,
    };
  }

  // Close any remaining open position at the last close.
  if (openTrade !== null) {
    const last = candles[candles.length - 1];
    const { pnl, pnlPct, fees } = closePnl(
      openTrade,
      last.close,
      config.leverage,
      config.feeRate,
      config.slippagePct,
    );
    capital = add(capital, pnl);
    trades.push({
      symbol,
      side: openTrade.side,
      entryAt: openTrade.entryAt,
      exitAt: last.timestamp.toISOString(),
      entryPrice: openTrade.entryPrice,
      exitPrice: last.close,
      size: openTrade.size,
      notional: openTrade.notional,
      pnl,
      pnlPct,
      fees,
      exitReason: "end_of_data",
    });
  }

  return { trades, finalCapital: capital };
}

// ---------------------------------------------------------------------------
// Public API
// ---------------------------------------------------------------------------

export function runLocalBacktest(
  config: LocalBacktestConfig,
): Effect.Effect<LocalBacktestResult, SqliteError, MarketRepository> {
  return Effect.gen(function* () {
    const repo = yield* MarketRepository;
    const exchangeId = yield* repo.ensureExchange(config.exchange);

    const symbolCapital =
      config.symbols.length === 0
        ? config.initialCapital
        : divide(config.initialCapital, String(config.symbols.length));
    const allTrades: BacktestTrade[] = [];

    for (const symbol of config.symbols) {
      const pairId = yield* repo.ensureTradingPair(symbol, exchangeId);
      const candles = yield* repo.getCandles({
        exchangeId,
        pairId,
        timeframe: config.timeframe,
        start: config.start,
        end: config.end,
      });
      if (candles.length === 0) continue;

      const result = runSymbolBacktest(symbol, candles, symbolCapital, config);
      allTrades.push(...result.trades);
    }

    // Sort all trades chronologically.
    allTrades.sort(
      (a, b) => new Date(a.entryAt).getTime() - new Date(b.entryAt).getTime(),
    );

    const totalPnl = allTrades.reduce((sum, t) => add(sum, t.pnl), zero);
    const finalCapital = add(config.initialCapital, totalPnl);
    const totalPnlPct =
      compare(config.initialCapital, zero) === 0
        ? zero
        : multiply(divide(totalPnl, config.initialCapital), hundred);

    const winningTrades = allTrades.filter(
      (t) => compare(t.pnl, zero) > 0,
    ).length;
    const losingTrades = allTrades.filter(
      (t) => compare(t.pnl, zero) < 0,
    ).length;
    const winRate =
      allTrades.length === 0
        ? zero
        : multiply(
            divide(String(winningTrades), String(allTrades.length)),
            hundred,
          );

    const grossProfit = allTrades
      .filter((t) => compare(t.pnl, zero) > 0)
      .reduce((sum, t) => add(sum, t.pnl), zero);
    const grossLoss = abs(
      allTrades
        .filter((t) => compare(t.pnl, zero) < 0)
        .reduce((sum, t) => add(sum, t.pnl), zero),
    );
    const profitFactor =
      compare(grossLoss, zero) === 0
        ? grossProfit
        : divide(grossProfit, grossLoss);

    // Max drawdown from trade-by-trade equity curve.
    let peak = config.initialCapital;
    let maxDrawdown = zero;
    let runningCapital = config.initialCapital;
    for (const t of allTrades) {
      runningCapital = add(runningCapital, t.pnl);
      if (compare(runningCapital, peak) > 0) peak = runningCapital;
      const drawdown = subtract(peak, runningCapital);
      const drawdownPct =
        compare(peak, zero) === 0
          ? zero
          : multiply(divide(drawdown, peak), hundred);
      if (compare(drawdownPct, maxDrawdown) > 0) maxDrawdown = drawdownPct;
    }

    // Sharpe-ish ratio: mean trade return / stddev of returns (annualised not needed).
    const returns = allTrades.map((t) => t.pnlPct);
    let sharpe = zero;
    if (returns.length >= 2) {
      const mean = divide(
        returns.reduce((sum, r) => add(sum, r), zero),
        String(returns.length),
      );
      const variance = divide(
        returns.reduce((sum, r) => {
          const diff = subtract(r, mean);
          return add(sum, multiply(diff, diff));
        }, zero),
        String(returns.length),
      );
      const std = String(Math.sqrt(Number(variance)));
      sharpe = compare(std, zero) === 0 ? zero : divide(mean, std);
    }

    return {
      initialCapital: config.initialCapital,
      finalCapital,
      totalPnl,
      totalPnlPct,
      totalTrades: allTrades.length,
      winningTrades,
      losingTrades,
      winRate,
      profitFactor,
      maxDrawdownPct: maxDrawdown,
      sharpeRatio: sharpe,
      trades: allTrades,
    };
  });
}
