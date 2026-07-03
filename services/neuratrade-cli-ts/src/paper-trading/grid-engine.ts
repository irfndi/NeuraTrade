/**
 * Grid-scalp paper-trading engine.
 *
 * Persists state between iterations so the same market-neutral grid can be run
 * as a live shadow (one closed candle at a time) against stored or real-time
 * market data.
 */

import { Effect } from 'effect';
import type { Candle } from '../market-data/types.js';
import {
  MarketDataError,
  MarketDataGateway,
  type MarketDataGatewayService,
} from '../market-data/gateway.js';
import {
  FuturesExchangeAdapter,
  type FuturesExchangeAdapterService,
  type FuturesMarginMode,
  type FuturesProductType,
} from '../exchange/futures-adapter.js';
import { ExchangeError } from '../exchange/adapter.js';
import { RiskError, RiskGuard, type RiskGuardService } from '../risk/guards.js';
import {
  KillSwitch,
  KillSwitchError,
  type KillSwitchService,
} from '../risk/kill-switch.js';
import {
  CircuitBreaker,
  CircuitBreakerError,
  type CircuitBreakerService,
} from '../risk/circuit-breaker.js';
import {
  PaperTradingRepository,
  PaperTradingRepositoryError,
  type PaperTradingRepositoryService,
} from './repository.js';
import type { GridPaperPositionSide, GridPaperState, GridPaperTrade } from './types.js';

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
  readonly action: 'opened' | 'closed' | 'hold';
  readonly side: GridPaperState['side'];
  readonly capital: number;
  readonly peakCapital: number;
  readonly note: string;
}

function sma(candles: readonly Candle[], i: number, period: number): number | null {
  if (i < period - 1) return null;
  let sum = 0;
  for (let j = i - period + 1; j <= i; j++) sum += candles[j].close;
  return sum / period;
}

function makeId(): string {
  return `grid-paper-${Date.now()}-${Math.random().toString(36).slice(2, 8)}`;
}

function orderSizeContracts(
  capital: number,
  maxPositionPct: number,
  entryPrice: number,
): number {
  if (entryPrice <= 0) return 0;
  const allocation = capital * (maxPositionPct / 100);
  const size = allocation / entryPrice;
  return Math.max(0, Number(size.toFixed(8)));
}

function liquidationPrice(
  side: GridPaperPositionSide,
  entryPrice: number,
  leverage: number,
): number {
  const l = Math.max(1, leverage);
  if (l <= 1) return 0;
  return side === 'long'
    ? entryPrice * (1 - 1 / l)
    : entryPrice * (1 + 1 / l);
}

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

    let state = yield* repo.getGridState(
      options.exchange,
      options.symbol,
      options.timeframe,
    );

    if (!state) {
      state = {
        exchange: options.exchange,
        symbol: options.symbol,
        timeframe: options.timeframe,
        capital: options.initialCapital,
        peakCapital: options.initialCapital,
        paused: 0,
        side: null,
        entryPrice: 0,
        gridStepPct: options.gridStepPct,
        gridMaxGrids: options.gridMaxGrids,
        gridPauseAfterLossBars: options.gridPauseAfterLossBars,
        feePct: options.feePct,
        slippageBps: options.slippageBps,
        trendFilterPeriod: options.trendFilterPeriod,
        maxPositionPct: options.maxPositionPct,
        maxDrawdownPct: options.maxDrawdownPct,
        leverage: options.leverage,
        killed: false,
        lastTimestamp: null,
        updatedAt: new Date(),
      };
    }

    if (state.killed) {
      return {
        action: 'hold' as const,
        side: state.side,
        capital: state.capital,
        peakCapital: state.peakCapital,
        note: 'kill switch active',
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
        action: 'hold' as const,
        side: state.side,
        capital: state.capital,
        peakCapital: state.peakCapital,
        note: `insufficient candles (${candles.length}/${minCandles})`,
      };
    }

    let i: number;
    if (replayBars > 0) {
      if (state.lastTimestamp === null) {
        i = Math.max(
          options.trendFilterPeriod,
          candles.length - replayBars,
        );
      } else {
        const nextIndex = candles.findIndex(
          (c) => c.timestamp.getTime() > state!.lastTimestamp!.getTime(),
        );
        if (nextIndex === -1) {
          return {
            action: 'hold' as const,
            side: state.side,
            capital: state.capital,
            peakCapital: state.peakCapital,
            note: 'no new replay candle',
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
        action: 'hold' as const,
        side: state.side,
        capital: state.capital,
        peakCapital: state.peakCapital,
        note: 'trend filter not ready',
      };
    }

    const isLive = options.isLive ?? false;
    const productType = options.productType ?? 'USDT-FUTURES';
    const marginMode = options.marginMode ?? 'isolated';

    if (isLive) {
      if (yield* killSwitch.isEngaged()) {
        const reason = yield* killSwitch.getReason();
        return {
          action: 'hold' as const,
          side: state.side,
          capital: state.capital,
          peakCapital: state.peakCapital,
          note: `KILL SWITCH ENGAGED: ${reason}`,
        };
      }

      if (yield* circuitBreaker.isOpen()) {
        const reason = yield* circuitBreaker.getReason();
        return {
          action: 'hold' as const,
          side: state.side,
          capital: state.capital,
          peakCapital: state.peakCapital,
          note: `CIRCUIT BREAKER OPEN: ${reason}`,
        };
      }
    }

    // Decrement pause at the start of a new bar.
    if (state.paused > 0) {
      state = { ...state, paused: state.paused - 1, lastTimestamp: current.timestamp };
      yield* repo.saveGridState(state);
      return {
        action: 'hold' as const,
        side: state.side,
        capital: state.capital,
        peakCapital: state.peakCapital,
        note: `paused (${state.paused} bars remaining)`,
      };
    }

    const mid = current.open;
    const step = mid * (options.gridStepPct / 100);
    const slippageFactor = 1 + options.slippageBps / 10000;
    const fee = (options.feePct / 100) * 2;

    const todayPnl = yield* repo.getTodayRealizedPnl();
    const startOfDayCapital = yield* repo.getStartOfDayCapital(
      new Date(),
      state.capital,
    );

    let note = 'no action';

    const closeTrade = (
      side: GridPaperPositionSide,
      entryPrice: number,
      exitPrice: number,
      exitReason: GridPaperTrade['exitReason'],
      stateCapital: number,
      peakCapital: number,
      maxPositionPct: number,
      leverage: number,
      openedAt: Date,
    ): { readonly trade: GridPaperTrade; readonly capitalAfter: number; readonly peakCapital: number } => {
      const pricePnl =
        side === 'long'
          ? (exitPrice - entryPrice) / entryPrice
          : (entryPrice - exitPrice) / entryPrice;
      const net = pricePnl - fee;
      const allocationFactor = maxPositionPct / 100;
      const leveragedReturn = exitReason === 'liquidation' ? -1 : net * leverage;
      const rawCapitalAfter = stateCapital * (1 + leveragedReturn * allocationFactor);
      const capitalAfter =
        exitReason === 'liquidation'
          ? stateCapital * (1 - allocationFactor)
          : Math.max(stateCapital * (1 - allocationFactor), rawCapitalAfter);
      const pnlPct = exitReason === 'liquidation' ? -100 : leveragedReturn * 100;
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
      };
      return { trade, capitalAfter, peakCapital: Math.max(peakCapital, capitalAfter) };
    };

    const closeGridPosition = (
      side: GridPaperPositionSide,
      exitReason: GridPaperTrade['exitReason'],
      theoreticalExitPrice: number,
    ) => {
      if (state === null) {
        return Effect.fail(
          new PaperTradingRepositoryError('closeGridPosition called with null state'),
        );
      }
      const s = state!;
      return Effect.gen(function* () {
        let exitPrice = theoreticalExitPrice;
        if (isLive && exitReason !== 'liquidation') {
          const size = orderSizeContracts(
            s.capital,
            s.maxPositionPct,
            s.entryPrice,
          );
          if (size > 0) {
            const fill = yield* adapter.closePosition({
              symbol: options.symbol,
              side: side === 'long' ? 'sell' : 'buy',
              productType,
              marginMode,
              leverage: s.leverage,
              size,
            });
            if (fill) {
              exitPrice = fill.filledPrice;
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
        );
        yield* repo.recordGridTrade(close.trade);
        return { ...close, exitPrice };
      });
    };

    const targetRatio = options.targetRatio ?? 1;
    if (state.side === null) {
      const buyLevel = mid - step;
      const sellLevel = mid + step;
      const onlyWithTrend = options.onlyWithTrend ?? false;
      const allowLong = !onlyWithTrend || current.close > trend;
      const allowShort = !onlyWithTrend || current.close < trend;

      let entrySide: GridPaperPositionSide | null = null;
      let theoreticalEntryPrice = 0;
      if (allowLong && current.low <= buyLevel) {
        entrySide = 'long';
        theoreticalEntryPrice = buyLevel * slippageFactor;
      } else if (allowShort && current.high >= sellLevel) {
        entrySide = 'short';
        theoreticalEntryPrice = sellLevel / slippageFactor;
      }

      if (entrySide !== null) {
        if (isLive) {
          const riskGuard = yield* RiskGuard;
          const riskCheck = yield* riskGuard
            .check({
              isLive: true,
              capital: state.capital,
              peakCapital: state.peakCapital,
              startOfDayCapital,
              dailyRealizedPnl: todayPnl,
              tradesTodayCount: 0,
              positionValue: state.capital * (state.maxPositionPct / 100),
              symbol: options.symbol,
              side: entrySide === 'long' ? 'buy' : 'sell',
              leverage: state.leverage,
              productType,
            })
            .pipe(Effect.either);
          if (riskCheck._tag === 'Left') {
            note = `RISK BLOCKED ${entrySide}: ${riskCheck.left.violations.join('; ')}`;
          } else {
            const size = orderSizeContracts(
              state.capital,
              state.maxPositionPct,
              theoreticalEntryPrice,
            );
            if (size <= 0) {
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
                side: entrySide === 'long' ? 'buy' : 'sell',
                type: 'market',
                size,
                productType,
                marginMode,
                leverage: state.leverage,
              });
              state = {
                ...state,
                side: entrySide,
                entryPrice: fill.filledPrice,
                updatedAt: new Date(),
                lastTimestamp: current.timestamp,
              };
              note = `[LIVE] opened ${entrySide} @ ${state.entryPrice.toFixed(2)} size=${size.toFixed(6)} (leverage=${state.leverage}x)`;
            }
          }
        } else {
          state = {
            ...state,
            side: entrySide,
            entryPrice: theoreticalEntryPrice,
            updatedAt: new Date(),
            lastTimestamp: current.timestamp,
          };
          note = `opened ${entrySide} @ ${state.entryPrice.toFixed(2)} (leverage=${state.leverage}x)`;
        }
      }
    } else if (state.side === 'long') {
      const target = state.entryPrice + step * targetRatio;
      const stop = state.entryPrice - step * options.gridMaxGrids;
      const liq = liquidationPrice('long', state.entryPrice, state.leverage);

      if (liq > 0 && current.low <= liq) {
        const close = yield* closeGridPosition(
          'long',
          'liquidation',
          liq * slippageFactor,
        );
        state = {
          ...state,
          side: null,
          entryPrice: 0,
          capital: close.capitalAfter,
          peakCapital: close.peakCapital,
          paused: 0,
          killed: true,
          updatedAt: new Date(),
          lastTimestamp: current.timestamp,
        };
        note = `liquidated long @ ${close.trade.exitPrice.toFixed(2)} pnl=-100.000% (leverage=${state.leverage}x)`;
      } else if (current.high >= target) {
        const close = yield* closeGridPosition(
          'long',
          'target',
          target / slippageFactor,
        );
        state = {
          ...state,
          side: null,
          entryPrice: 0,
          capital: close.capitalAfter,
          peakCapital: close.peakCapital,
          paused: 0,
          updatedAt: new Date(),
          lastTimestamp: current.timestamp,
        };
        note = `${isLive ? '[LIVE] ' : ''}closed long target @ ${close.trade.exitPrice.toFixed(2)} pnl=${close.trade.pnlPct.toFixed(3)}%`;
      } else if (current.low <= stop) {
        const close = yield* closeGridPosition(
          'long',
          'stop',
          stop * slippageFactor,
        );
        state = {
          ...state,
          side: null,
          entryPrice: 0,
          capital: close.capitalAfter,
          peakCapital: close.peakCapital,
          paused: options.gridPauseAfterLossBars,
          updatedAt: new Date(),
          lastTimestamp: current.timestamp,
        };
        note = `${isLive ? '[LIVE] ' : ''}closed long stop @ ${close.trade.exitPrice.toFixed(2)} pnl=${close.trade.pnlPct.toFixed(3)}%`;
      }
    } else if (state.side === 'short') {
      const target = state.entryPrice - step * targetRatio;
      const stop = state.entryPrice + step * options.gridMaxGrids;
      const liq = liquidationPrice('short', state.entryPrice, state.leverage);

      if (liq > 0 && current.high >= liq) {
        const close = yield* closeGridPosition(
          'short',
          'liquidation',
          liq / slippageFactor,
        );
        state = {
          ...state,
          side: null,
          entryPrice: 0,
          capital: close.capitalAfter,
          peakCapital: close.peakCapital,
          paused: 0,
          killed: true,
          updatedAt: new Date(),
          lastTimestamp: current.timestamp,
        };
        note = `liquidated short @ ${close.trade.exitPrice.toFixed(2)} pnl=-100.000% (leverage=${state.leverage}x)`;
      } else if (current.low <= target) {
        const close = yield* closeGridPosition(
          'short',
          'target',
          target * slippageFactor,
        );
        state = {
          ...state,
          side: null,
          entryPrice: 0,
          capital: close.capitalAfter,
          peakCapital: close.peakCapital,
          paused: 0,
          updatedAt: new Date(),
          lastTimestamp: current.timestamp,
        };
        note = `${isLive ? '[LIVE] ' : ''}closed short target @ ${close.trade.exitPrice.toFixed(2)} pnl=${close.trade.pnlPct.toFixed(3)}%`;
      } else if (current.high >= stop) {
        const close = yield* closeGridPosition(
          'short',
          'stop',
          stop / slippageFactor,
        );
        state = {
          ...state,
          side: null,
          entryPrice: 0,
          capital: close.capitalAfter,
          peakCapital: close.peakCapital,
          paused: options.gridPauseAfterLossBars,
          updatedAt: new Date(),
          lastTimestamp: current.timestamp,
        };
        note = `${isLive ? '[LIVE] ' : ''}closed short stop @ ${close.trade.exitPrice.toFixed(2)} pnl=${close.trade.pnlPct.toFixed(3)}%`;
      }
    }

    state = { ...state, lastTimestamp: current.timestamp, updatedAt: new Date() };

    // Realized-drawdown kill switch.
    const drawdownPct =
      state.peakCapital > 0
        ? ((state.peakCapital - state.capital) / state.peakCapital) * 100
        : 0;
    if (drawdownPct >= state.maxDrawdownPct && state.maxDrawdownPct < 100) {
      state = { ...state, killed: true };
      note = note === 'no action' ? 'kill switch triggered' : `${note}; kill switch triggered`;
    }

    yield* repo.saveGridState(state);

    const closedLike =
      note.includes('closed') || note.startsWith('liquidated');

    return {
      action: state.side === null && closedLike
        ? 'closed'
        : state.side !== null && note.includes('opened')
          ? 'opened'
          : 'hold',
      side: state.side,
      capital: state.capital,
      peakCapital: state.peakCapital,
      note,
    };
  });
}
