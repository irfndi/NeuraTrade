/**
 * Deterministic scalping paper-trading engine.
 *
 * Evaluates the latest signal from the backend backtest endpoint over a recent
 * window of real candles, then simulates order execution in SQLite. This version
 * is intentionally closer to real futures trading:
 *   - long and short positions
 *   - configurable leverage
 *   - risk-based position sizing
 *   - stop-loss, take-profit, trailing-stop and time-stop exits
 *   - two-sided fee estimation
 */
import { Context, Data, Effect, Layer } from "effect";
import { ApiClient, type BacktestSignal } from "./api-client.ts";
import { BinanceClient, type RawCandle } from "./binance-client.ts";
import { PaperRepository, type PaperTrade } from "./paper-repository.ts";
import { errorMessage } from "../utils/error-message.ts";
import {
  add,
  compare,
  divide,
  max,
  min,
  multiply,
  subtract,
} from "./decimal.ts";

// ---------------------------------------------------------------------------
// Errors
// ---------------------------------------------------------------------------

export class PaperTradingError extends Data.TaggedError("PaperTradingError")<{
  readonly reason: string;
}> {}

// ---------------------------------------------------------------------------
// Types
// ---------------------------------------------------------------------------

export interface PaperTradingConfig {
  readonly symbol: string;
  readonly exchange: string;
  readonly capital: string;
  readonly windowHours: number;
  readonly timeframe: string;
  readonly feeRate: string;
  readonly mode: string;
  readonly leverage: number;
  readonly riskPct: string;
  readonly stopLossPct: string;
  readonly takeProfitPct: string;
  readonly trailingStopPct: string;
  readonly maxHoldHours: number;
  readonly dryRun?: boolean;
}

export interface PaperTradeResult {
  readonly action:
    | "open_long"
    | "open_short"
    | "close_long"
    | "close_short"
    | "hold"
    | "no_signal";
  readonly tradeId?: number;
  readonly pnl?: string;
  readonly message: string;
}

// ---------------------------------------------------------------------------
// Service interface
// ---------------------------------------------------------------------------

export interface PaperTradingEngineImpl {
  readonly evaluateAndTrade: (
    config: PaperTradingConfig,
  ) => Effect.Effect<
    PaperTradeResult,
    PaperTradingError,
    ApiClient | BinanceClient | PaperRepository
  >;
}

export class PaperTradingEngine extends Context.Tag("PaperTradingEngine")<
  PaperTradingEngine,
  PaperTradingEngineImpl
>() {}

// ---------------------------------------------------------------------------
// Position / exit helpers
// ---------------------------------------------------------------------------

type PositionSide = "long" | "short";

export function positionSide(trade: PaperTrade): PositionSide {
  return trade.side === "buy" ? "long" : "short";
}

export function stopLossPrice(
  entry: string,
  side: PositionSide,
  slPct: string,
): string {
  return side === "long"
    ? multiply(entry, subtract("1", slPct))
    : multiply(entry, add("1", slPct));
}

export function takeProfitPrice(
  entry: string,
  side: PositionSide,
  tpPct: string,
): string {
  return side === "long"
    ? multiply(entry, add("1", tpPct))
    : multiply(entry, subtract("1", tpPct));
}

function candlesAfterEntry(
  candles: ReadonlyArray<RawCandle>,
  entryAt: string,
): ReadonlyArray<RawCandle> {
  const entryTime = new Date(entryAt).getTime();
  return candles.filter((c) => c.timestamp.getTime() > entryTime);
}

interface ExitCheck {
  readonly exitPrice: string;
  readonly exitReason: string;
}

export function checkExit(
  trade: PaperTrade,
  candles: ReadonlyArray<RawCandle>,
  config: PaperTradingConfig,
): ExitCheck | null {
  const side = positionSide(trade);
  const sl = stopLossPrice(trade.entry_price, side, config.stopLossPct);
  const tp = takeProfitPrice(trade.entry_price, side, config.takeProfitPct);

  const relevant = candlesAfterEntry(candles, trade.entry_at);
  if (relevant.length === 0) return null;

  let effectiveSl = sl;

  // Trailing stop: keep the stop as close to the favorable extreme as allowed.
  if (compare(config.trailingStopPct, "0") > 0) {
    const extreme =
      side === "long"
        ? relevant.reduce((acc, c) => max(acc, c.high), trade.entry_price)
        : relevant.reduce((acc, c) => min(acc, c.low), trade.entry_price);
    const trail =
      side === "long"
        ? multiply(extreme, subtract("1", config.trailingStopPct))
        : multiply(extreme, add("1", config.trailingStopPct));
    effectiveSl =
      side === "long" ? max(effectiveSl, trail) : min(effectiveSl, trail);
  }

  for (const c of relevant) {
    if (side === "long") {
      if (compare(c.low, effectiveSl) <= 0) {
        return { exitPrice: effectiveSl, exitReason: "stop_loss" };
      }
      if (compare(c.high, tp) >= 0) {
        return { exitPrice: tp, exitReason: "take_profit" };
      }
    } else {
      if (compare(c.high, effectiveSl) >= 0) {
        return { exitPrice: effectiveSl, exitReason: "stop_loss" };
      }
      if (compare(c.low, tp) <= 0) {
        return { exitPrice: tp, exitReason: "take_profit" };
      }
    }
  }

  // Time stop.
  const lastCandle = candles[candles.length - 1];
  const entryTime = new Date(trade.entry_at).getTime();
  const holdHours = (lastCandle.timestamp.getTime() - entryTime) / 3_600_000;
  if (holdHours >= config.maxHoldHours) {
    return { exitPrice: lastCandle.close, exitReason: "time_stop" };
  }

  return null;
}

export function calculatePositionSize(
  capital: string,
  riskPct: string,
  entryPrice: string,
  stopLossPct: string,
  leverage: number,
): {
  readonly size: string;
  readonly notional: string;
  readonly margin: string;
} {
  const riskAmount = multiply(capital, riskPct);
  const slDistance = multiply(entryPrice, stopLossPct);
  const size = divide(riskAmount, slDistance);
  const notional = multiply(size, entryPrice);
  const margin = divide(notional, String(leverage));
  return { size, notional, margin };
}

export function calculateClosePnl(
  trade: PaperTrade,
  exitPrice: string,
  leverage: number,
  feeRate: string,
): { readonly pnl: string; readonly pnlPct: string; readonly fees: string } {
  const side = positionSide(trade);
  const priceDelta =
    side === "long"
      ? subtract(exitPrice, trade.entry_price)
      : subtract(trade.entry_price, exitPrice);
  const gross = multiply(priceDelta, trade.size);
  const leveragedGross = multiply(gross, String(leverage));
  const exitNotional = multiply(trade.size, exitPrice);
  const fees = multiply(add(trade.notional, exitNotional), feeRate);
  const pnl = subtract(leveragedGross, fees);
  const pnlPct =
    compare(trade.notional, "0") === 0
      ? "0"
      : multiply(divide(pnl, trade.notional), "100");
  return { pnl, pnlPct, fees };
}

// ---------------------------------------------------------------------------
// Signal helpers
// ---------------------------------------------------------------------------

function parseAction(signal: BacktestSignal): "buy" | "sell" | null {
  const raw = signal.hints?.suggested_action?.trim().toLowerCase() ?? "";
  if (raw === "buy" || raw === "sell") return raw;
  return null;
}

function latestAcceptedSignal(
  signals: ReadonlyArray<BacktestSignal>,
): BacktestSignal | null {
  for (let i = signals.length - 1; i >= 0; i--) {
    const s = signals[i];
    if (s.funnel_stage === "eligible" || s.funnel_stage === "accepted") {
      return s;
    }
  }
  return null;
}

function makeResult(r: PaperTradeResult): PaperTradeResult {
  return r;
}

// ---------------------------------------------------------------------------
// Engine implementation
// ---------------------------------------------------------------------------

export const PaperTradingEngineLiveImpl: PaperTradingEngineImpl = {
  evaluateAndTrade: (config) =>
    Effect.gen(function* () {
      const api = yield* ApiClient;
      const binance = yield* BinanceClient;
      const repo = yield* PaperRepository;

      // 1. Resolve recent candle window from Binance.
      const now = new Date();
      const start = new Date(now.getTime() - config.windowHours * 3_600_000);
      const candles = yield* binance.getKlines({
        symbol: config.symbol,
        interval: config.timeframe,
        startTime: start.getTime(),
        endTime: now.getTime(),
      });
      if (candles.length === 0) {
        return makeResult({
          action: "no_signal",
          message: "no recent candles fetched",
        });
      }
      const lastCandle = candles[candles.length - 1];

      // 2. Ask the backend for the deterministic signal over the same window.
      const backtest = yield* api.runScalpingBacktest(
        {
          start_time: start.toISOString(),
          end_time: now.toISOString(),
          symbols: [config.symbol],
          exchange: config.exchange,
          initial_capital: config.capital,
          mode: config.mode,
          summary_only: false,
        },
        120_000,
      );

      // 3. Manage any open position first (SL/TP/trailing/time stop).
      const openTrade = yield* repo.getOpenTrade(
        config.symbol,
        config.exchange,
      );
      if (openTrade !== null) {
        const exit = checkExit(openTrade, candles, config);
        if (exit !== null) {
          const { pnl, pnlPct, fees } = calculateClosePnl(
            openTrade,
            exit.exitPrice,
            config.leverage,
            config.feeRate,
          );
          const closeAction =
            positionSide(openTrade) === "long" ? "close_long" : "close_short";
          if (!config.dryRun) {
            yield* repo.closeTrade({
              id: openTrade.id,
              exit_price: exit.exitPrice,
              exit_at: lastCandle.timestamp.toISOString(),
              pnl,
              pnl_pct: pnlPct,
              fees,
              exit_reason: exit.exitReason,
            });
          }
          return makeResult({
            action: closeAction,
            tradeId: openTrade.id,
            pnl,
            message: `${closeAction} ${openTrade.size} ${config.symbol} @ ${exit.exitPrice} (${exit.exitReason}) pnl=${pnl}`,
          });
        }
      }

      // 4. Evaluate new signal.
      const signal = latestAcceptedSignal(backtest.signals ?? []);
      if (signal === null) {
        return makeResult({
          action: "no_signal",
          message: "no accepted signal in window",
        });
      }

      const action = parseAction(signal);
      if (action === null) {
        return makeResult({
          action: "no_signal",
          message: `accepted signal has no actionable side: ${signal.hints?.suggested_action}`,
        });
      }

      const entryPrice = lastCandle.close;

      // Open long.
      if (action === "buy" && openTrade === null) {
        const { size, notional, margin } = calculatePositionSize(
          config.capital,
          config.riskPct,
          entryPrice,
          config.stopLossPct,
          config.leverage,
        );
        const sl = stopLossPrice(entryPrice, "long", config.stopLossPct);
        const tp = takeProfitPrice(entryPrice, "long", config.takeProfitPct);
        if (!config.dryRun) {
          const tradeId = yield* repo.openTrade({
            symbol: config.symbol,
            exchange: config.exchange,
            side: "buy",
            size,
            notional,
            entry_price: entryPrice,
            entry_at: lastCandle.timestamp.toISOString(),
            signal_id: signal.signal_id,
            mode: config.mode,
          });
          return makeResult({
            action: "open_long",
            tradeId: Number(tradeId),
            message: `opened long ${size} ${config.symbol} @ ${entryPrice} (lev=${config.leverage}x, margin=${margin}, SL=${sl}, TP=${tp})`,
          });
        }
        return makeResult({
          action: "open_long",
          message: `[DRY RUN] would open long ${size} ${config.symbol} @ ${entryPrice} (lev=${config.leverage}x, margin=${margin}, SL=${sl}, TP=${tp})`,
        });
      }

      // Open short.
      if (action === "sell" && openTrade === null) {
        const { size, notional, margin } = calculatePositionSize(
          config.capital,
          config.riskPct,
          entryPrice,
          config.stopLossPct,
          config.leverage,
        );
        const sl = stopLossPrice(entryPrice, "short", config.stopLossPct);
        const tp = takeProfitPrice(entryPrice, "short", config.takeProfitPct);
        if (!config.dryRun) {
          const tradeId = yield* repo.openTrade({
            symbol: config.symbol,
            exchange: config.exchange,
            side: "sell",
            size,
            notional,
            entry_price: entryPrice,
            entry_at: lastCandle.timestamp.toISOString(),
            signal_id: signal.signal_id,
            mode: config.mode,
          });
          return makeResult({
            action: "open_short",
            tradeId: Number(tradeId),
            message: `opened short ${size} ${config.symbol} @ ${entryPrice} (lev=${config.leverage}x, margin=${margin}, SL=${sl}, TP=${tp})`,
          });
        }
        return makeResult({
          action: "open_short",
          message: `[DRY RUN] would open short ${size} ${config.symbol} @ ${entryPrice} (lev=${config.leverage}x, margin=${margin}, SL=${sl}, TP=${tp})`,
        });
      }

      // Reverse long -> short.
      if (
        action === "sell" &&
        openTrade !== null &&
        positionSide(openTrade) === "long"
      ) {
        const { pnl, pnlPct, fees } = calculateClosePnl(
          openTrade,
          entryPrice,
          config.leverage,
          config.feeRate,
        );
        if (!config.dryRun) {
          yield* repo.closeTrade({
            id: openTrade.id,
            exit_price: entryPrice,
            exit_at: lastCandle.timestamp.toISOString(),
            pnl,
            pnl_pct: pnlPct,
            fees,
            exit_reason: "signal_reverse",
          });
        }
        return makeResult({
          action: "close_long",
          tradeId: openTrade.id,
          pnl,
          message: `[${config.dryRun ? "DRY RUN" : "live"}] reversed long to flat @ ${entryPrice} pnl=${pnl}`,
        });
      }

      // Reverse short -> long.
      if (
        action === "buy" &&
        openTrade !== null &&
        positionSide(openTrade) === "short"
      ) {
        const { pnl, pnlPct, fees } = calculateClosePnl(
          openTrade,
          entryPrice,
          config.leverage,
          config.feeRate,
        );
        if (!config.dryRun) {
          yield* repo.closeTrade({
            id: openTrade.id,
            exit_price: entryPrice,
            exit_at: lastCandle.timestamp.toISOString(),
            pnl,
            pnl_pct: pnlPct,
            fees,
            exit_reason: "signal_reverse",
          });
        }
        return makeResult({
          action: "close_short",
          tradeId: openTrade.id,
          pnl,
          message: `[${config.dryRun ? "DRY RUN" : "live"}] reversed short to flat @ ${entryPrice} pnl=${pnl}`,
        });
      }

      return makeResult({
        action: "hold",
        message: `signal=${action}; no position change (open=${openTrade?.status ?? "none"})`,
      });
    }).pipe(
      Effect.catchAll((err) =>
        Effect.fail(
          new PaperTradingError({
            reason: errorMessage(err),
          }),
        ),
      ),
    ),
};

export const PaperTradingEngineLive = Layer.succeed(
  PaperTradingEngine,
  PaperTradingEngineLiveImpl,
);
