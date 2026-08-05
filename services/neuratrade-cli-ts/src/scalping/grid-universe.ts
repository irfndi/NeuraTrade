/**
 * Per-symbol grid walk-forward universe scanner.
 *
 * Walks every symbol that has enough stored candles, finds the best grid
 * parameters in-sample, and reports which ones pass a survival gate.
 */

import { Effect } from "effect";
import { writeFileSync } from "node:fs";
import { resolve } from "node:path";
import {
  MarketDataRepository,
  MarketDataRepositoryError,
  type MarketDataRepositoryService,
} from "../market-data/repository.js";
import type { Candle } from "../market-data/types.js";
import { runGridWalkForward, type GridWalkForwardResult } from "./grid.js";

export interface GridUniverseOptions {
  readonly exchange: string;
  readonly timeframe: string;
  readonly initialCapital: number;
  readonly minCandles: number;
  readonly trainWindow: number;
  readonly testWindow: number;
  readonly minProfitableWindowsPct: number;
  readonly minAggregateReturnPct: number;
  readonly feePct: number;
  readonly slippageBps: number;
  readonly trendFilterPeriod: number;
  /**
   * Min % of candles (0-100) reaching a grid step from the candle open in
   * either direction. Default 0 disables; rejects backtest-profitable symbols
   * whose step is too wide to fill live.
   */
  readonly minFillFrequencyPct?: number;
  readonly searchSpace: {
    readonly gridStepPct: readonly number[];
    readonly gridMaxGrids: readonly number[];
    readonly gridPauseAfterLossBars: readonly number[];
  };
  readonly outputPath?: string;
}

export interface GridUniverseEntry {
  readonly symbol: string;
  readonly candles: number;
  readonly bestParams: {
    readonly gridStepPct: number;
    readonly gridMaxGrids: number;
    readonly gridPauseAfterLossBars: number;
  };
  readonly walkForward: GridWalkForwardResult;
  readonly passed: boolean;
}

export interface GridUniverseResult {
  readonly entries: readonly GridUniverseEntry[];
  readonly survivors: readonly GridUniverseEntry[];
}

/**
 * Fraction (as 0-100 %) of candles whose range reaches a grid step away from
 * the candle open in EITHER direction (a buy grid below the open or a sell
 * grid above it). This is the fill-frequency gate: a backtest-profitable grid
 * whose step is too wide to reach in practice will rarely fill live and should
 * be rejected. A `minFillFrequencyPct` of 0 (or an empty candle set) disables
 * the gate and reports 100.
 */
export function computeFillFrequencyPct(
  candles: readonly Pick<Candle, "open" | "high" | "low">[],
  gridStepPct: number,
  minFillFrequencyPct: number,
): number {
  if (minFillFrequencyPct <= 0 || candles.length === 0) return 100;
  const lowFactor = 1 - gridStepPct / 100;
  const highFactor = 1 + gridStepPct / 100;
  const touched = candles.filter(
    (c) => c.low <= c.open * lowFactor || c.high >= c.open * highFactor,
  ).length;
  return (touched / candles.length) * 100;
}

export function runGridUniverseScan(
  options: GridUniverseOptions,
): Effect.Effect<
  GridUniverseResult,
  MarketDataRepositoryError,
  MarketDataRepositoryService
> {
  return Effect.gen(function* () {
    const repo = yield* MarketDataRepository;

    const symbolsWithCount = yield* repo.listSymbolsByCandleCount(
      options.exchange,
      options.timeframe,
      100,
    );

    const symbols = symbolsWithCount
      .filter((s) => s.count >= options.minCandles)
      .map((s) => s.symbol);

    const entries: GridUniverseEntry[] = [];

    for (const symbol of symbols) {
      const candles = yield* repo.getCandles({
        exchange: options.exchange,
        symbol,
        timeframe: options.timeframe,
        limit: options.minCandles,
      });

      if (candles.length < options.minCandles) continue;

      const walkForward = runGridWalkForward(candles, {
        trainWindow: options.trainWindow,
        testWindow: options.testWindow,
        initialCapital: options.initialCapital,
        searchSpace: options.searchSpace,
        baseOptions: {
          feePct: options.feePct,
          slippageBps: options.slippageBps,
          trendFilterPeriod: options.trendFilterPeriod,
          leverage: 1,
        },
      });

      const lastWindow = walkForward.windows[walkForward.windows.length - 1];
      const bestParams = lastWindow?.params ?? {
        gridStepPct: options.searchSpace.gridStepPct[0] ?? 1,
        gridMaxGrids: options.searchSpace.gridMaxGrids[0] ?? 2,
        gridPauseAfterLossBars:
          options.searchSpace.gridPauseAfterLossBars[0] ?? 0,
      };

      const passedBase =
        walkForward.profitableWindowsPct >= options.minProfitableWindowsPct &&
        walkForward.aggregateReturnPct >= options.minAggregateReturnPct;

      const fillGate = options.minFillFrequencyPct ?? 0;
      const fillFrequencyPct = computeFillFrequencyPct(
        candles,
        bestParams.gridStepPct,
        fillGate,
      );

      const passed = passedBase && fillFrequencyPct >= fillGate;

      entries.push({
        symbol,
        candles: candles.length,
        bestParams: {
          gridStepPct: bestParams.gridStepPct,
          gridMaxGrids: bestParams.gridMaxGrids,
          gridPauseAfterLossBars: bestParams.gridPauseAfterLossBars,
        },
        walkForward,
        passed,
      });
    }

    const survivors = entries.filter((e) => e.passed);

    if (options.outputPath) {
      const whitelist = survivors.map((e) => ({
        exchange: options.exchange,
        symbol: e.symbol,
        timeframe: options.timeframe,
        gridStepPct: e.bestParams.gridStepPct,
        gridMaxGrids: e.bestParams.gridMaxGrids,
        gridPauseAfterLossBars: e.bestParams.gridPauseAfterLossBars,
      }));
      writeFileSync(options.outputPath, JSON.stringify(whitelist, null, 2));
    }

    return { entries, survivors };
  });
}
