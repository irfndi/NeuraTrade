/**
 * Grid-scalp paper-trading engine.
 *
 * Persists state between iterations so the same market-neutral grid can be run
 * as a live shadow (one closed candle at a time) against stored or real-time
 * market data.
 */

import { Effect } from "effect";
import type { Candle } from "../market-data/types.js";
import { makeCausalSymbolStats } from "../scalping/symbol-stats.js";
import {
  MarketDataError,
  MarketDataGateway,
  type MarketDataGatewayService,
} from "../market-data/gateway.js";
import {
  FuturesExchangeAdapter,
  type FuturesExchangeAdapterService,
  type FuturesOrderFill,
  type FuturesMarginMode,
  type FuturesProductType,
} from "../exchange/futures-adapter.js";
import { ExchangeError } from "../exchange/adapter.js";
import { Decimal, money, toNumber } from "../utils/money.js";
import { RiskError, RiskGuard, type RiskGuardService } from "../risk/guards.js";
import {
  KillSwitch,
  KillSwitchError,
  type KillSwitchService,
} from "../risk/kill-switch.js";
import {
  CircuitBreaker,
  CircuitBreakerError,
  type CircuitBreakerService,
} from "../risk/circuit-breaker.js";
import {
  PaperTradingRepository,
  PaperTradingRepositoryError,
  type PaperTradingRepositoryService,
} from "./repository.js";
import type {
  GridPaperPositionSide,
  GridPaperState,
  GridPaperTrade,
} from "./types.js";

export interface GridPaperTradingOptions {
  readonly exchange: string;
  readonly symbol: string;
  readonly timeframe: string;
  readonly gridStepPct: number;
  readonly gridMaxGrids: number;
  readonly gridPauseAfterLossBars: number;
  readonly feePct: number;
  readonly slippageBps: number;
  readonly trendFilterPeriod: number;
  readonly initialCapital: number;
  /** Max percentage of capital allocated to a single grid order (default 100). */
  readonly maxPositionPct: number;
  /** If set, stop trading when drawdown exceeds this percent (default 100 = disabled). */
  readonly maxDrawdownPct: number;
  /** Leverage multiplier. 1 = spot-style (no liquidation). */
  readonly leverage: number;
  /** When true, only enter long above the trend SMA and short below it. */
  readonly onlyWithTrend?: boolean;
  /** Target distance as a multiple of the grid step (default 1.0). */
  readonly targetRatio?: number;
  /**
   * Chop gate: when > 0, NEW entries are skipped while the causal ADX(14)
   * of the stored candle history is at or above this threshold. Matches the
   * backtest engine's gate so paper/live runs behave like the validated
   * backtests (bd clever-cabin-24h). 0/undefined disables.
   */
  readonly chopGateAdxThreshold?: number;
  /**
   * When > 0, replay the last N stored candles one per iteration instead of
   * always processing the latest candle. This turns the paper loop into a
   * deterministic shadow walk over historical bars.
   */
  readonly replayBars?: number;
  /** When true, place real orders via the FuturesExchangeAdapter. */
  readonly isLive?: boolean;
  /** Futures product type required for live orders. */
  readonly productType?: FuturesProductType;
  /** Futures margin mode required for live orders. */
  readonly marginMode?: FuturesMarginMode;
}

export interface GridPaperTradingIterationResult {
  readonly action: "opened" | "closed" | "hold";
  readonly side: GridPaperState["side"];
  readonly capital: number;
  readonly peakCapital: number;
  readonly note: string;
}

function sma(
  candles: readonly Candle[],
  i: number,
  period: number,
): number | null {
  if (i < period - 1) return null;
  let sum = 0;
  for (let j = i - period + 1; j <= i; j++) sum += candles[j].close;
  return sum / period;
}

function makeId(): string {
  return `grid-paper-${Date.now()}-${Math.random().toString(36).slice(2, 8)}`;
}

function orderSizeContracts(
  capital: Decimal,
  maxPositionPct: number,
  entryPrice: Decimal,
): Decimal {
  if (entryPrice.lessThanOrEqualTo(0)) return money(0);
  const allocation = capital.times(maxPositionPct / 100);
  return Decimal.max(0, allocation.div(entryPrice));
}

function liquidationPrice(
  side: GridPaperPositionSide,
  entryPrice: Decimal,
  leverage: number,
): Decimal {
  const l = Math.max(1, leverage);
  if (l <= 1) return money(0);
  return side === "long"
    ? entryPrice.times(1 - 1 / l)
    : entryPrice.times(1 + 1 / l);
}

type GridFillEvidence = Pick<
  FuturesOrderFill,
  "orderId" | "clientOid" | "filledQty" | "fee"
>;

export function runGridPaperTradingIteration(
  options: GridPaperTradingOptions,
): Effect.Effect<
  GridPaperTradingIterationResult,
  | MarketDataError
  | PaperTradingRepositoryError
  | ExchangeError
  | RiskError
  | KillSwitchError
  | CircuitBreakerError,
  | MarketDataGatewayService
  | PaperTradingRepositoryService
  | FuturesExchangeAdapterService
  | RiskGuardService
  | KillSwitchService
  | CircuitBreakerService
> {
  return Effect.gen(function* () {
    const repo = yield* PaperTradingRepository;
    const gateway = yield* MarketDataGateway;
    const adapter = yield* FuturesExchangeAdapter;
    const killSwitch = yield* KillSwitch;
    const circuitBreaker = yield* CircuitBreaker;

    yield* repo.ensureTables();

    let state: GridPaperState = (yield* repo.getGridState(
      options.exchange,
      options.symbol,
      options.timeframe,
    )) ?? {
      exchange: options.exchange,
      symbol: options.symbol,
      timeframe: options.timeframe,
      capital: money(options.initialCapital),
      peakCapital: money(options.initialCapital),
      paused: 0,
      side: null,
      entryPrice: money(0),
      gridStepPct: options.gridStepPct,
      gridMaxGrids: options.gridMaxGrids,
      gridPauseAfterLossBars: options.gridPauseAfterLossBars,
      feePct: options.feePct,
      slippageBps: options.slippageBps,
      trendFilterPeriod: options.trendFilterPeriod,
      maxPositionPct: options.maxPositionPct,
      maxDrawdownPct: options.maxDrawdownPct,
      leverage: options.leverage ?? 1,
      killed: false,
      lastTimestamp: null,
      updatedAt: new Date(),
    };

    if (state.killed) {
      return {
        action: "hold" as const,
        side: state.side,
        capital: toNumber(state.capital),
        peakCapital: toNumber(state.peakCapital),
        note: "kill switch active",
      };
    }

    const replayBars = options.replayBars ?? 0;
    const requiredCandles =
      replayBars > 0
        ? replayBars + options.trendFilterPeriod + 5
        : Math.max(options.trendFilterPeriod + 1, 2);
    const candles = yield* gateway.fetchOHLCV(
      options.exchange,
      options.symbol,
      options.timeframe,
      requiredCandles,
    );

    const minCandles =
      replayBars > 0
        ? Math.min(replayBars, candles.length)
        : Math.max(options.trendFilterPeriod + 1, 2);
    if (candles.length < minCandles) {
      return {
        action: "hold" as const,
        side: state.side,
        capital: toNumber(state.capital),
        peakCapital: toNumber(state.peakCapital),
        note: `insufficient candles (${candles.length}/${minCandles})`,
      };
    }

    let i: number;
    if (replayBars > 0) {
      if (state.lastTimestamp === null) {
        i = Math.max(options.trendFilterPeriod, candles.length - replayBars);
      } else {
        const nextIndex = candles.findIndex(
          (c) => c.timestamp.getTime() > state!.lastTimestamp!.getTime(),
        );
        if (nextIndex === -1) {
          return {
            action: "hold" as const,
            side: state.side,
            capital: toNumber(state.capital),
            peakCapital: toNumber(state.peakCapital),
            note: "no new replay candle",
          };
        }
        i = nextIndex;
      }
    } else {
      i = candles.length - 1;
    }
    const current = candles[i];
    const trend = sma(candles, i, options.trendFilterPeriod);
    if (trend === null) {
      return {
        action: "hold" as const,
        side: state.side,
        capital: toNumber(state.capital),
        peakCapital: toNumber(state.peakCapital),
        note: "trend filter not ready",
      };
    }

    const isLive = options.isLive ?? false;
    const productType = options.productType ?? "USDT-FUTURES";
    const marginMode = options.marginMode ?? "isolated";

    if (isLive) {
      if (yield* killSwitch.isEngaged()) {
        const reason = yield* killSwitch.getReason();
        return {
          action: "hold" as const,
          side: state.side,
          capital: toNumber(state.capital),
          peakCapital: toNumber(state.peakCapital),
          note: `KILL SWITCH ENGAGED: ${reason}`,
        };
      }

      if (yield* circuitBreaker.isOpen()) {
        const reason = yield* circuitBreaker.getReason();
        return {
          action: "hold" as const,
          side: state.side,
          capital: toNumber(state.capital),
          peakCapital: toNumber(state.peakCapital),
          note: `CIRCUIT BREAKER OPEN: ${reason}`,
        };
      }
    }

    // Decrement pause at the start of a new bar.
    if (state.paused > 0) {
      state = {
        ...state,
        paused: state.paused - 1,
        lastTimestamp: current.timestamp,
      };
      yield* repo.saveGridState(state);
      return {
        action: "hold" as const,
        side: state.side,
        capital: toNumber(state.capital),
        peakCapital: toNumber(state.peakCapital),
        note: `paused (${state.paused} bars remaining)`,
      };
    }

    const mid = money(current.open);
    const step = mid.times(options.gridStepPct / 100);
    const slippageFactor = 1 + options.slippageBps / 10000;
    const fee = (options.feePct / 100) * 2;

    const todayPnl = yield* repo.getTodayRealizedPnl();
    const startOfDayCapital = yield* repo.getStartOfDayCapital(
      new Date(),
      state.capital,
    );

    let note = "no action";

    const closeTrade = (
      side: GridPaperPositionSide,
      entryPrice: Decimal,
      exitPrice: Decimal,
      exitReason: GridPaperTrade["exitReason"],
      stateCapital: Decimal,
      peakCapital: Decimal,
      maxPositionPct: number,
      leverage: number,
      openedAt: Date,
      entryEvidence: GridFillEvidence | null,
      exitFill: FuturesOrderFill | null,
    ): {
      readonly trade: GridPaperTrade;
      readonly capitalAfter: Decimal;
      readonly peakCapital: Decimal;
    } => {
      const pricePnl =
        side === "long"
          ? exitPrice.minus(entryPrice).div(entryPrice)
          : entryPrice.minus(exitPrice).div(entryPrice);
      const net = pricePnl.minus(fee);
      const allocationFactor = maxPositionPct / 100;
      const leveragedReturn =
        exitReason === "liquidation" ? money(-1) : net.times(leverage);
      const rawCapitalAfter = stateCapital.times(
        money(1).plus(leveragedReturn.times(allocationFactor)),
      );
      const capitalAfter =
        exitReason === "liquidation"
          ? stateCapital.times(1 - allocationFactor)
          : Decimal.max(
              stateCapital.times(1 - allocationFactor),
              rawCapitalAfter,
            );
      const pnlPct =
        exitReason === "liquidation" ? money(-100) : leveragedReturn.times(100);
      const realizedPnlPct =
        entryEvidence !== null && exitFill !== null
          ? leveragedReturn
              .times(100)
              .minus(
                entryEvidence.fee
                  .plus(exitFill.fee)
                  .div(stateCapital)
                  .times(100),
              )
          : undefined;
      const trade: GridPaperTrade = {
        id: makeId(),
        exchange: options.exchange,
        symbol: options.symbol,
        timeframe: options.timeframe,
        side,
        entryPrice,
        exitPrice,
        capitalBefore: stateCapital,
        capitalAfter,
        pnlPct,
        exitReason,
        openedAt,
        closedAt: new Date(),
        fillSource:
          entryEvidence !== null && exitFill !== null ? "live" : "simulated",
        entryOrderId: entryEvidence?.orderId,
        entryClientOid: entryEvidence?.clientOid,
        exitOrderId: exitFill?.orderId,
        exitClientOid: exitFill?.clientOid,
        entryFilledQty: entryEvidence?.filledQty,
        exitFilledQty: exitFill?.filledQty,
        entryFee: entryEvidence?.fee,
        exitFee: exitFill?.fee,
        realizedPnlPct,
      };
      return {
        trade,
        capitalAfter,
        peakCapital: Decimal.max(peakCapital, capitalAfter),
      };
    };

    const closeGridPosition = (
      side: GridPaperPositionSide,
      exitReason: GridPaperTrade["exitReason"],
      theoreticalExitPrice: Decimal,
    ) => {
      const s = state;
      return Effect.gen(function* () {
        let exitPrice = theoreticalExitPrice;
        let exitFill: FuturesOrderFill | null = null;
        if (isLive && exitReason !== "liquidation") {
          const size = orderSizeContracts(
            s.capital,
            s.maxPositionPct,
            s.entryPrice,
          );
          if (size.greaterThan(0)) {
            const fill = yield* adapter.closePosition({
              symbol: options.symbol,
              side: side === "long" ? "sell" : "buy",
              productType,
              marginMode,
              leverage: s.leverage,
              size,
              price: theoreticalExitPrice,
            });
            if (fill) {
              exitPrice = money(fill.filledPrice);
              exitFill = fill;
            }
          }
        }
        const close = closeTrade(
          side,
          s.entryPrice,
          exitPrice,
          exitReason,
          s.capital,
          s.peakCapital,
          s.maxPositionPct,
          s.leverage,
          s.updatedAt,
          s.entryFillSource === "live" &&
            s.entryOrderId &&
            s.entryFilledQty &&
            s.entryFee
            ? {
                orderId: s.entryOrderId,
                clientOid: s.entryClientOid,
                filledQty: s.entryFilledQty,
                fee: s.entryFee,
              }
            : null,
          exitFill,
        );
        yield* repo.recordGridTrade(close.trade);
        return { ...close, exitPrice };
      });
    };

    const targetRatio = options.targetRatio ?? 1;
    if (state.side === null) {
      // Chop gate parity with the backtest engine: trending markets are
      // where grid inventory gets run over; sit out until ADX says ranging.
      const chopGateAdxThreshold = Math.max(
        0,
        options.chopGateAdxThreshold ?? 0,
      );
      if (chopGateAdxThreshold > 0) {
        const stats = makeCausalSymbolStats(candles, options.timeframe)(i);
        if (stats.adx14 >= chopGateAdxThreshold) {
          // Persist the advanced pointer — otherwise gate-blocked bars
          // re-evaluate the same candle forever (replay stalls at the
          // first blocked bar).
          state = {
            ...state,
            lastTimestamp: current.timestamp,
            updatedAt: new Date(),
          };
          yield* repo.saveGridState(state);
          return {
            action: "hold" as const,
            side: state.side,
            capital: toNumber(state.capital),
            peakCapital: toNumber(state.peakCapital),
            note: `chop gate active (ADX ${stats.adx14.toFixed(1)} >= ${chopGateAdxThreshold})`,
          };
        }
      }
      const buyLevel = mid.minus(step);
      const sellLevel = mid.plus(step);
      const onlyWithTrend = options.onlyWithTrend ?? false;
      const allowLong = !onlyWithTrend || current.close > trend;
      const allowShort = !onlyWithTrend || current.close < trend;

      let entrySide: GridPaperPositionSide | null = null;
      let theoreticalEntryPrice = money(0);
      if (allowLong && money(current.low).lessThanOrEqualTo(buyLevel)) {
        entrySide = "long";
        theoreticalEntryPrice = buyLevel.times(slippageFactor);
      } else if (
        allowShort &&
        money(current.high).greaterThanOrEqualTo(sellLevel)
      ) {
        entrySide = "short";
        theoreticalEntryPrice = sellLevel.div(slippageFactor);
      }

      if (entrySide !== null) {
        if (isLive) {
          const riskGuard = yield* RiskGuard;
          const riskCheck = yield* riskGuard
            .check({
              isLive: true,
              capital: toNumber(state.capital),
              peakCapital: toNumber(state.peakCapital),
              startOfDayCapital: toNumber(startOfDayCapital),
              dailyRealizedPnl: toNumber(todayPnl),
              tradesTodayCount: 0,
              positionValue: toNumber(
                state.capital.times(state.maxPositionPct / 100),
              ),
              symbol: options.symbol,
              side: entrySide === "long" ? "buy" : "sell",
              leverage: state.leverage,
              productType,
            })
            .pipe(Effect.result);
          if (riskCheck._tag === "Failure") {
            note = `RISK BLOCKED ${entrySide}: ${riskCheck.failure.violations.join("; ")}`;
          } else {
            const size = orderSizeContracts(
              state.capital,
              state.maxPositionPct,
              theoreticalEntryPrice,
            );
            if (size.lessThanOrEqualTo(0)) {
              note = `RISK BLOCKED ${entrySide}: computed size zero`;
            } else {
              yield* adapter.setLeverage(
                options.symbol,
                productType,
                marginMode,
                state.leverage,
              );
              yield* adapter.setMarginMode(
                options.symbol,
                productType,
                marginMode,
              );
              const fill = yield* adapter.placeOrder({
                symbol: options.symbol,
                side: entrySide === "long" ? "buy" : "sell",
                type: "market",
                size,
                productType,
                marginMode,
                leverage: state.leverage,
                price: theoreticalEntryPrice,
              });
              state = {
                ...state,
                side: entrySide,
                entryPrice: money(fill.filledPrice),
                entryOrderId: fill.orderId,
                entryClientOid: fill.clientOid,
                entryFilledQty: fill.filledQty,
                entryFee: fill.fee,
                entryFillSource: "live",
                updatedAt: new Date(),
                lastTimestamp: current.timestamp,
              };
              note = `[LIVE] opened ${entrySide} @ ${state.entryPrice.toFixed(2)} size=${size.toFixed(6)} (leverage=${state.leverage}x)`;
            }
          }
        } else {
          const size = orderSizeContracts(
            state.capital,
            state.maxPositionPct,
            theoreticalEntryPrice,
          );
          state = {
            ...state,
            side: entrySide,
            entryPrice: theoreticalEntryPrice,
            entryFilledQty: size,
            entryFee: money(0),
            entryFillSource: "simulated",
            updatedAt: new Date(),
            lastTimestamp: current.timestamp,
          };
          note = `opened ${entrySide} @ ${state.entryPrice.toFixed(2)} (leverage=${state.leverage}x)`;
        }
      }
    } else if (state.side === "long") {
      const target = state.entryPrice.plus(step.times(targetRatio));
      const stop = state.entryPrice.minus(step.times(options.gridMaxGrids));
      const liq = liquidationPrice("long", state.entryPrice, state.leverage);

      if (liq.greaterThan(0) && money(current.low).lessThanOrEqualTo(liq)) {
        const close = yield* closeGridPosition(
          "long",
          "liquidation",
          liq.times(slippageFactor),
        );
        state = {
          ...state,
          side: null,
          entryPrice: money(0),
          capital: close.capitalAfter,
          peakCapital: close.peakCapital,
          paused: 0,
          killed: true,
          updatedAt: new Date(),
          lastTimestamp: current.timestamp,
        };
        note = `liquidated long @ ${close.trade.exitPrice.toFixed(2)} pnl=-100.000% (leverage=${state.leverage}x)`;
      } else if (money(current.high).greaterThanOrEqualTo(target)) {
        const close = yield* closeGridPosition(
          "long",
          "target",
          target.div(slippageFactor),
        );
        state = {
          ...state,
          side: null,
          entryPrice: money(0),
          capital: close.capitalAfter,
          peakCapital: close.peakCapital,
          paused: 0,
          updatedAt: new Date(),
          lastTimestamp: current.timestamp,
        };
        note = `${isLive ? "[LIVE] " : ""}closed long target @ ${close.trade.exitPrice.toFixed(2)} pnl=${close.trade.pnlPct.toFixed(3)}%`;
      } else if (money(current.low).lessThanOrEqualTo(stop)) {
        const close = yield* closeGridPosition(
          "long",
          "stop",
          stop.times(slippageFactor),
        );
        state = {
          ...state,
          side: null,
          entryPrice: money(0),
          capital: close.capitalAfter,
          peakCapital: close.peakCapital,
          paused: options.gridPauseAfterLossBars,
          updatedAt: new Date(),
          lastTimestamp: current.timestamp,
        };
        note = `${isLive ? "[LIVE] " : ""}closed long stop @ ${close.trade.exitPrice.toFixed(2)} pnl=${close.trade.pnlPct.toFixed(3)}%`;
      }
    } else if (state.side === "short") {
      const target = state.entryPrice.minus(step.times(targetRatio));
      const stop = state.entryPrice.plus(step.times(options.gridMaxGrids));
      const liq = liquidationPrice("short", state.entryPrice, state.leverage);

      if (liq.greaterThan(0) && money(current.high).greaterThanOrEqualTo(liq)) {
        const close = yield* closeGridPosition(
          "short",
          "liquidation",
          liq.div(slippageFactor),
        );
        state = {
          ...state,
          side: null,
          entryPrice: money(0),
          capital: close.capitalAfter,
          peakCapital: close.peakCapital,
          paused: 0,
          killed: true,
          updatedAt: new Date(),
          lastTimestamp: current.timestamp,
        };
        note = `liquidated short @ ${close.trade.exitPrice.toFixed(2)} pnl=-100.000% (leverage=${state.leverage}x)`;
      } else if (money(current.low).lessThanOrEqualTo(target)) {
        const close = yield* closeGridPosition(
          "short",
          "target",
          target.times(slippageFactor),
        );
        state = {
          ...state,
          side: null,
          entryPrice: money(0),
          capital: close.capitalAfter,
          peakCapital: close.peakCapital,
          paused: 0,
          updatedAt: new Date(),
          lastTimestamp: current.timestamp,
        };
        note = `${isLive ? "[LIVE] " : ""}closed short target @ ${close.trade.exitPrice.toFixed(2)} pnl=${close.trade.pnlPct.toFixed(3)}%`;
      } else if (money(current.high).greaterThanOrEqualTo(stop)) {
        const close = yield* closeGridPosition(
          "short",
          "stop",
          stop.div(slippageFactor),
        );
        state = {
          ...state,
          side: null,
          entryPrice: money(0),
          capital: close.capitalAfter,
          peakCapital: close.peakCapital,
          paused: options.gridPauseAfterLossBars,
          updatedAt: new Date(),
          lastTimestamp: current.timestamp,
        };
        note = `${isLive ? "[LIVE] " : ""}closed short stop @ ${close.trade.exitPrice.toFixed(2)} pnl=${close.trade.pnlPct.toFixed(3)}%`;
      }
    }

    state = {
      ...state,
      lastTimestamp: current.timestamp,
      updatedAt: new Date(),
    };

    // Realized-drawdown kill switch.
    const drawdownPct = state.peakCapital.greaterThan(0)
      ? state.peakCapital.minus(state.capital).div(state.peakCapital).times(100)
      : money(0);
    if (
      drawdownPct.greaterThanOrEqualTo(state.maxDrawdownPct) &&
      state.maxDrawdownPct < 100
    ) {
      state = { ...state, killed: true };
      note =
        note === "no action"
          ? "kill switch triggered"
          : `${note}; kill switch triggered`;
    }

    yield* repo.saveGridState(state);

    const closedLike = note.includes("closed") || note.startsWith("liquidated");

    return {
      action:
        state.side === null && closedLike
          ? "closed"
          : state.side !== null && note.includes("opened")
            ? "opened"
            : "hold",
      side: state.side,
      capital: toNumber(state.capital),
      peakCapital: toNumber(state.peakCapital),
      note,
    };
  });
}
