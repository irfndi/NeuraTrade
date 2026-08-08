/**
 * Per-symbol grid walk-forward universe scanner.
 *
 * Walks every symbol that has enough stored candles, finds the best grid
 * parameters in-sample, and reports which ones pass a survival gate.
 */

import { Effect, Ref } from "effect";
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
import { computeSymbolStats, type SymbolStatistics } from "./symbol-stats.js";

/**
 * Minimum 24h quote volume (USDT) for a market-listed symbol to be scanned:
 * the market source filters the full exchange contract list down to
 * tradeable liquidity instead of a static token list.
 */
export const MIN_UNIVERSE_24H_VOLUME_USDT = 1_000_000;

/**
 * Stage-2 cheap-stats screen thresholds (market scan only, applied BEFORE
 * walk-forward): reject chop (ADX < 15), dead (ATR% < 0.02), and moon-shot
 * (ATR% > 10) candidates. Cost-ordered funnel — the expensive walk-forward
 * never runs on candidates these screens already reject.
 */
export const STAGE2_MIN_ADX = 15;
// ATR% floors/caps are FRACTIONS (atr14Pct returns 0.003 for 0.3%): 0.0005
// (0.05%) filters truly dead symbols while keeping BTC/ETH/SOL/XRP-class
// volatility (measured 0.29-1.11%); 0.1 (10%) caps moon-shots.
export const STAGE2_MIN_ATR_PCT = 0.0005;
export const STAGE2_MAX_ATR_PCT = 0.1;

/**
 * Default account capital (USDT) for scaling the fills/day selection target.
 */
export const DEFAULT_ACCOUNT_CAPITAL = 1_000;

/**
 * Per-symbol fills/day cap in portfolio selection — avoids concentration in
 * a single high-edge symbol.
 */
export const DEFAULT_PER_SYMBOL_FILL_CAP = 10;

/**
 * Stage-2 cheap-stats gate: true when the candidate survives the ADX/ATR
 * regime screens (not chop, not dead, not a moon-shot). Extracted so the
 * thresholds are testable and shared with the market scan.
 */
export function passesStage2Screen(
  stats: Pick<SymbolStatistics, "adx14" | "atr14Pct">,
): boolean {
  // NaN/undefined stats (e.g. flat series -> division by zero) must FAIL
  // closed: NaN comparisons are all false and would let chop through.
  if (!Number.isFinite(stats.adx14) || !Number.isFinite(stats.atr14Pct)) {
    return false;
  }
  return (
    stats.adx14 >= STAGE2_MIN_ADX &&
    stats.atr14Pct >= STAGE2_MIN_ATR_PCT &&
    stats.atr14Pct <= STAGE2_MAX_ATR_PCT
  );
}

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
  /**
   * ATR(14) as a % of the latest close — the volatility used for stage-2
   * screening and capital allocation.
   */
  readonly volatility?: number;
  /**
   * Out-of-sample trade count: sum of walk-forward window test trades
   * (`walkForward.totalTrades`).
   */
  readonly oosTrades?: number;
  /**
   * Projected fills/day = fill-frequency fraction × bars per day.
   */
  readonly fillsPerDay?: number;
  /**
   * Edge per trade % = aggregate OOS return / OOS trade count — a rough
   * per-trade expectation, not a per-trade win-rate edge.
   */
  readonly edgePerTradePct?: number;
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

/**
 * Bars per day for a timeframe: "15m" → 96 (1440/15), "5m" → 288, "1h" → 24.
 * Unparseable timeframes default to 15m. Used to project fills/day from the
 * per-candle fill frequency.
 */
export function barsPerDayForTimeframe(timeframe: string): number {
  const match = /^(\d+)(m|h|d)$/.exec(timeframe);
  const value = Number(match?.[1] ?? 15);
  const unit = match?.[2] ?? "m";
  const minutes = value * (unit === "d" ? 1440 : unit === "h" ? 60 : 1);
  return minutes > 0 ? Math.round(1440 / minutes) : 0;
}

/**
 * Account-scaled fills/day target: clamp(5, 50 × A/1000, 50). A $1000
 * account targets the 5/day floor; capital above $1000 scales up to the
 * 50/day ceiling.
 */
export function accountScaledTargetFillsPerDay(
  accountCapital = DEFAULT_ACCOUNT_CAPITAL,
): number {
  return Math.min(50, Math.max(5, (50 * accountCapital) / 1000));
}

/**
 * Frequency-targeted portfolio selection (degenerate knapsack): greedy
 * top-K by edge/trade descending, taking each candidate's fills/day capped
 * at `perSymbolCap`, until the cumulative fills/day reaches the target.
 * Entries without a computed edge or fills/day are never selected.
 */
export function selectUniversePortfolio(
  entries: readonly GridUniverseEntry[],
  targetFillsPerDay: number,
  perSymbolCap = DEFAULT_PER_SYMBOL_FILL_CAP,
): readonly GridUniverseEntry[] {
  const ranked = [...entries].sort(
    (a, b) => (b.edgePerTradePct ?? 0) - (a.edgePerTradePct ?? 0),
  );
  const selected: GridUniverseEntry[] = [];
  let projectedFills = 0;
  for (const entry of ranked) {
    if (projectedFills >= targetFillsPerDay) break;
    const fills = Math.min(entry.fillsPerDay ?? 0, perSymbolCap);
    if (fills <= 0) continue;
    selected.push(entry);
    projectedFills += fills;
  }
  return selected;
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

  // Candidate metrics for the frequency-targeted selection stage. All are
  // display/selection hints, not money — plain numbers are fine.
  const stats = computeSymbolStats(candles, options.timeframe);
  // OOS trade count: GridWalkForwardWindow carries `testTrades` per window;
  // the result's `totalTrades` is exactly their sum.
  const oosTrades = walkForward.totalTrades;
  // ponytail: computeFillFrequencyPct(…, 0) reports 100 (gate disabled), so
  // fillsPerDay degrades to the bars/day upper bound; to measure the real
  // touch rate, pass options.minFillFrequencyPct when it's > 0.
  const fillsPerDay =
    (fillFrequencyPct / 100) * barsPerDayForTimeframe(options.timeframe);
  // Approximation: aggregate OOS return spread evenly over OOS trades —
  // ignores compounding and win/loss asymmetry, but ranks candidates fairly.
  const edgePerTradePct = walkForward.aggregateReturnPct / Math.max(oosTrades, 1);

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
    volatility: stats.atr14Pct,
    oosTrades,
    fillsPerDay,
    edgePerTradePct,
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
  MarketDataGatewayService | MarketDataRepositoryService
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
  // Tail-fetch concurrency: the sequential 250ms pacing is the binding
  // constraint on batch size, not the rate limit — 2 workers keep the batch
  // ~2x faster while staying well under Bitget's public limit.
  const TAIL_CONCURRENCY = 2;

  return Effect.gen(function* () {
    const gateway = yield* MarketDataGateway;
    const repo = yield* MarketDataRepository;

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

    const fetchBatch = (symbol: string, startTime: Date | undefined) =>
      Effect.gen(function* () {
        yield* Effect.sleep(REQUEST_DELAY_MS);
        return yield* gateway.fetchOHLCV(
          options.exchange,
          symbol,
          options.timeframe,
          BATCH,
          startTime,
        );
      });

    // Transient failures (rate limit, transport abort, DNS, timeouts) are
    // retried with backoff then SKIPPED with a warning — a degraded scan
    // persists the healthy majority; non-transient failures propagate so a
    // broken scan never persists a partial watchlist.
    const isTransient = (reason: string) =>
      reason.includes("429") ||
      reason.includes("aborted") ||
      reason.includes("fetch failed") ||
      reason.includes("ENOTFOUND") ||
      reason.includes("ETIMEDOUT") ||
      reason.includes("ECONNRESET") ||
      reason.includes("ECONNREFUSED") ||
      reason.includes("ConnectionRefused") ||
      reason.includes("Unable to connect") ||
      reason.includes("network error") ||
      /HTTP 5\d\d/.test(reason);
    const withRetry = (
      symbol: string,
      run: () => Effect.Effect<readonly Candle[], MarketDataError, never>,
    ) =>
      Effect.gen(function* () {
        let attempt = 0;
        for (;;) {
          const outcome = yield* run().pipe(Effect.result);
          if (outcome._tag === "Success") return outcome.success;
          const reason =
            (outcome.failure as { reason?: string } | undefined)?.reason ??
            (outcome.failure instanceof Error
              ? outcome.failure.message
              : String(outcome.failure));
          if (!isTransient(reason))
            return yield* Effect.fail(outcome.failure);
          attempt += 1;
          if (attempt >= 6) {
            yield* Effect.logWarning(
              `Transient failure past retries; skipping ${symbol} this cycle`,
            );
            return [] as readonly Candle[];
          }
          yield* Effect.sleep(1_000 * 2 ** attempt);
        }
      });

    // Backward pagination to fill the window when the cache is thin.
    const backfill = (symbol: string) =>
      withRetry(symbol, () =>
        Effect.gen(function* () {
          const byTimestamp = new Map<number, Candle>();
          let startTime: Date | undefined;
          while (byTimestamp.size < options.minCandles) {
            const batch = yield* fetchBatch(symbol, startTime);
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
        }),
      );

    // Incremental candle cache: fetch only bars newer than the DB max, save
    // them, backfill only when the cache is too thin. Steady state = 1
    // request/symbol/cycle; the batch no longer refetches history each run.
    const ensureCandles = (symbol: string) =>
      Effect.gen(function* () {
        const range = yield* repo.getCandleRange(
          options.exchange,
          symbol,
          options.timeframe,
        );
        const latest = range.latest;
        if (latest !== null) {
          const tail = yield* withRetry(symbol, () =>
            fetchBatch(
              symbol,
              new Date(latest.getTime() + timeframeMillis),
            ),
          );
          if (tail.length > 0) yield* repo.saveCandles(tail);
        }
        const cached = yield* repo.getCandles({
          exchange: options.exchange,
          symbol,
          timeframe: options.timeframe,
          limit: options.minCandles,
        });
        if (cached.length >= options.minCandles) return cached;
        const filled = yield* backfill(symbol);
        if (filled.length > 0) yield* repo.saveCandles(filled);
        return yield* repo.getCandles({
          exchange: options.exchange,
          symbol,
          timeframe: options.timeframe,
          limit: options.minCandles,
        });
      });

    const entries: GridUniverseEntry[] = [];
    const scanned = yield* Ref.make(0);

    const scanSymbol = (symbol: string) =>
      Effect.gen(function* () {
        const done = yield* Ref.updateAndGet(scanned, (n) => n + 1);
        if (done % 50 === 0) {
          yield* Effect.log(
            `market scan: ${done}/${candidates.length} symbols, ${entries.length} entries`,
          );
        }
        const candles = yield* ensureCandles(symbol);

        if (candles.length < options.minCandles) return;

        // Stage-2 cheap-stats screen (market scan only; the DB-sourced path
        // keeps its historical behavior): reject chop (ADX < 15), dead
        // (ATR% < 0.02), and moon-shot (ATR% > 10) candidates from the
        // cached candles BEFORE the expensive walk-forward runs.
        const stats = computeSymbolStats(candles, options.timeframe);
        if (!passesStage2Screen(stats)) return;

        entries.push(evaluateUniverseSymbol(symbol, candles, options));
      });

    yield* Effect.forEach(candidates, scanSymbol, {
      concurrency: TAIL_CONCURRENCY,
    });

    const survivors = entries.filter((e) => e.passed);

    return { entries, survivors };
  });
}
