/**
 * Per-symbol grid walk-forward universe scanner.
 *
 * Walks every symbol that has enough stored candles, finds the best grid
 * parameters in-sample, and reports which ones pass a survival gate.
 */

import { Effect } from "effect";
import {
  MarketDataGateway,
  type MarketDataError,
  type MarketDataGatewayService,
} from "../market-data/gateway.js";
import {
  MarketDataRepository,
  type MarketDataRepositoryError,
  type MarketDataRepositoryService,
} from "../market-data/repository.js";
import type { Candle } from "../market-data/types.js";
import { runGridWalkForward, type GridWalkForwardResult } from "./grid.js";

/**
 * Minimum 24h quote volume (USDT) for a market-listed symbol to be scanned:
 * the market source filters the full exchange contract list down to
 * tradeable liquidity instead of a static token list.
 */
export const MIN_UNIVERSE_24H_VOLUME_USDT = 1_000_000;

/**
 * Default grid parameter search space used by the universe scanner. Fixed so
 * survivors are comparable across symbols and runs.
 */
export const DEFAULT_GRID_UNIVERSE_SEARCH_SPACE = {
  gridStepPct: [0.1, 0.15, 0.2, 0.3, 0.5],
  gridMaxGrids: [1, 2, 3],
  gridPauseAfterLossBars: [0, 6, 24],
} as const;

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

function evaluateUniverseSymbol(
  symbol: string,
  candles: readonly Candle[],
  options: GridUniverseOptions,
): GridUniverseEntry {
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
    gridPauseAfterLossBars: options.searchSpace.gridPauseAfterLossBars[0] ?? 0,
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

  return {
    symbol,
    candles: candles.length,
    bestParams: {
      gridStepPct: bestParams.gridStepPct,
      gridMaxGrids: bestParams.gridMaxGrids,
      gridPauseAfterLossBars: bestParams.gridPauseAfterLossBars,
    },
    walkForward,
    passed,
  };
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
      options.minCandles,
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

      entries.push(evaluateUniverseSymbol(symbol, candles, options));
    }

    const survivors = entries.filter((e) => e.passed);

    return { entries, survivors };
  });
}

/**
 * Market-sourced universe scan: symbol discovery comes from the exchange's
 * full contract list (filtered by 24h liquidity), never from a static
 * watchlist or previously collected DB set. Candles are fetched live from
 * the market for every candidate.
 */
export function runMarketUniverseScan(
  options: GridUniverseOptions,
): Effect.Effect<
  GridUniverseResult,
  MarketDataRepositoryError | MarketDataError,
  MarketDataGatewayService
> {
  const timeframeMillis = (() => {
    const match = /^(\d+)(m|h|d)$/.exec(options.timeframe);
    const value = Number(match?.[1] ?? 15);
    const unit = match?.[2] ?? "m";
    return (
      value * (unit === "d" ? 86_400_000 : unit === "h" ? 3_600_000 : 60_000)
    );
  })();
  const BATCH = 200; // Bitget futures /history-candles rejects limit > 200.
  // Public API pacing: sequential scans burst past the rate limit without a
  // delay between every request (observed HTTP 429 mid-scan).
  const REQUEST_DELAY_MS = 250;

  return Effect.gen(function* () {
    const gateway = yield* MarketDataGateway;

    const [marketSymbols, volumes] = yield* Effect.all([
      gateway.fetchSymbols(options.exchange),
      gateway.fetch24hrVolumes(options.exchange),
    ]);
    // Tickers key volumes by "BTCUSDT" while fetchSymbols returns
    // "BTC/USDT"; normalize so the liquidity filter sees the same keys.
    const normalizedVolumes = new Map<string, number>(
      Object.entries(volumes).map(([symbol, volume]) => [
        symbol.includes("/") ? symbol : symbol.replace(/USDT$/, "/USDT"),
        volume,
      ]),
    );

    const candidates = marketSymbols
      .filter(
        (symbol) =>
          (normalizedVolumes.get(symbol) ?? 0) >= MIN_UNIVERSE_24H_VOLUME_USDT,
      )
      // Canonical futures form ("BTC/USDT:USDT") — the convention the
      // watchlist, soak, and grid engine all expect.
      .map((symbol) => (symbol.includes(":") ? symbol : `${symbol}:USDT`));

    const fetchCandles = (symbol: string) =>
      Effect.gen(function* () {
        const byTimestamp = new Map<number, Candle>();
        let startTime: Date | undefined;
        while (byTimestamp.size < options.minCandles) {
          yield* Effect.sleep(REQUEST_DELAY_MS);
          const batch = yield* gateway.fetchOHLCV(
            options.exchange,
            symbol,
            options.timeframe,
            BATCH,
            startTime,
          );
          if (batch.length === 0) break;
          for (const candle of batch) {
            byTimestamp.set(candle.timestamp.getTime(), candle);
          }
          const oldest = [...byTimestamp.keys()].sort((a, b) => a - b)[0];
          startTime = new Date(oldest - timeframeMillis);
        }
        return [...byTimestamp.values()].sort(
          (a, b) => a.timestamp.getTime() - b.timestamp.getTime(),
        );
      });

    // Retry rate-limited batches with backoff; non-429 failures propagate so
    // a failed scan never persists a partial watchlist.
    const fetchCandlesRetry = (symbol: string) =>
      Effect.gen(function* () {
        let attempt = 0;
        for (;;) {
          const outcome = yield* fetchCandles(symbol).pipe(Effect.result);
          if (outcome._tag === "Success") return outcome.success;
          const reason =
            (outcome.failure as { reason?: string } | undefined)?.reason ??
            (outcome.failure instanceof Error
              ? outcome.failure.message
              : String(outcome.failure));
          if (!reason.includes("429"))
            return yield* Effect.fail(outcome.failure);
          attempt += 1;
          if (attempt >= 4) return yield* Effect.fail(outcome.failure);
          yield* Effect.sleep(1_000 * 2 ** attempt);
        }
      });

    const entries: GridUniverseEntry[] = [];

    let scanned = 0;
    for (const symbol of candidates) {
      scanned += 1;
      if (scanned % 50 === 0) {
        yield* Effect.log(
          `market scan: ${scanned}/${candidates.length} symbols, ${entries.length} entries`,
        );
      }
      const candles = yield* fetchCandlesRetry(symbol);

      if (candles.length < options.minCandles) continue;

      entries.push(evaluateUniverseSymbol(symbol, candles, options));
    }

    const survivors = entries.filter((e) => e.passed);

    return { entries, survivors };
  });
}
