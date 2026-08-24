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
  MarketDataRepositoryError,
  type MarketDataRepositoryService,
} from "../market-data/repository.js";
import type { Candle } from "../market-data/types.js";
import {
  runGridBacktest,
  runGridWalkForward,
  type GridOptions,
} from "./grid.js";
import {
  runLadderGridBacktest,
  runLadderGridWalkForward,
  type LadderOptions,
} from "./ladder-grid.js";
import {
  validateGridEvidence,
  type GridValidationOk,
} from "./grid-validation.js";
import {
  validateLadderEvidence,
  type LadderValidationOk,
} from "./ladder-validation.js";
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
 * Stage-4 gate-scored eligibility sweep: target_ratio dials validated per
 * survivor (the walk-forward fixes step/grids/pause; the gate re-checks the
 * target/ADX dials around them).
 */
export const GATE_TARGETS = [1, 3, 4] as const;

/**
 * Stage-4 gate-scored eligibility sweep: chop-gate ADX dials validated per
 * survivor (matches the manifest sweep in
 * scripts/gate-scored-grid-search-2026-08-06.ts). 12 added 2026-08-14: the
 * live bybit book's validated configs use adx=12 (probe:
 * scripts/probe-targetratio-impact.ts — NEAR tr=4 flips -32.8% → +1.5%
 * with it); excluding it made the gate unable to express the geometry that
 * actually trades.
 */
export const GATE_ADX_GATES = [12, 24, 26, 28] as const;

/**
 * Deep-history target for gate-scored candidates: ~55k 15m bars (~2 years)
 * support 10+ rolling validation windows at the readiness default sizes.
 */
export const DEEP_HISTORY_TARGET = 55_000;

/**
 * Bounded recent-history window (bars) for the LADDER gate's backtest +
 * time-split. The ladder gate does not use the stage-4 rolling-window
 * validator (which needs 55k bars), so it evaluates on a bounded tail that
 * matches the ladder validation sweep (scripts/ladder-universe-sweep.ts,
 * --tail 2000): the ladder's edge is recent, and 2-year full-history returns
 * are inflated by walk-forward compounding.
 */
export const LADDER_GATE_TAIL = 2_000;

/**
 * Per-cycle deep-fetch request budget shared across gate candidates: the
 * cache grows backward over cycles until a survivor has enough history to
 * be gate-validated (~55k bars = ~275 requests at the 200-bar cap).
 */
export const DEEP_FETCH_REQUESTS_PER_CYCLE = 900;
// ~900 requests x ~1s (pacing + latency) ≈ 15 min of scan time per cycle;
// at 200 bars/request that deepens ~3 survivors (~55k bars each) per cycle.
// The 300 default starved the gate: the funnel fetched fast but had few
// deep-history candidates per cycle (FunnelAccelerate research 2026-08-09).

/**
 * Universe eligibility tier. 'readiness' (default) applies the FULL stage-4
 * readiness board — profitable-window share > 50%, fixed-OOS >= 30 trades,
 * drawdown/LB/stress gates, AND the strict time-split honesty check. 'fast'
 * is the high-throughput light tier for symbols that pass walk-forward +
 * time-split but fail the readiness board (windows>50%, OOS>=30, stress
 * LBs): it keeps only the strict time-split + walk-forward profitability +
 * a fills/day floor, dropping the readiness window/OOS/stress requirements
 * so high-frequency candidates become selectable immediately instead of
 * waiting on deep-history readiness validation.
 */
export type UniverseTier = "readiness" | "fast";

/**
 * Fast-tier fills/day floor: minimum % of candles (0-100) whose range
 * reaches a grid step from the open (computeFillFrequencyPct) — 5% on 15m
 * ≈ 4.8 fills/day, the high-frequency pool's touch profile. Below this the
 * grid is too wide to fill live regardless of backtest edge.
 */
export const FAST_TIER_MIN_FILL_FREQUENCY_PCT = 5;

/**
 * Max fraction of account capital allocated per grid position — mirrors the
 * sweep manifest's positionFraction 0.5.
 */
export const MAX_POSITION_FRACTION = 0.5;

/**
 * Minimum order notional (USDT) per position; bounds how many symbols an
 * account can hold: floor(A × MAX_POSITION_FRACTION / MIN_ORDER_NOTIONAL_USDT).
 */
export const MIN_ORDER_NOTIONAL_USDT = 10;

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
  // 0.75/1.0/1.25 added 2026-08-10: the validated gate-scored candidates
  // (BTC step 1.0, SOL step 1.25) were outside the original space — the
  // funnel could never surface them, so 'no profitable demo config' was
  // only proven for <=0.5% steps.
  gridStepPct: [0.1, 0.15, 0.2, 0.3, 0.5, 0.75, 1.0, 1.25],
  // 1.5 grids and 36 pause added 2026-08-11: the only mainnet-validated
  // BTC/SOL configs include these values, so the funnel must be able to
  // express them before concluding no survivor exists.
  gridMaxGrids: [1, 1.5, 2, 3],
  gridPauseAfterLossBars: [0, 6, 24, 36],
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
    /**
     * Ladder engine only (engine === "ladder"): number of simultaneous rungs
     * per side swept by the walk-forward. Absent = single-position grid.
     */
    readonly rungs?: readonly number[];
  };
  /**
   * Which grid engine to evaluate: "grid" (single-position, default) or
   * "ladder" (multi-rung, one TP per rung). The ladder engine converts
   * negative-expectancy single-position configs to OOS-passing profiles.
   */
  readonly engine?: "grid" | "ladder";
  /** Ladder stop geometry used by both the walk-forward and gate. */
  readonly ladderStopRatio?: number;
  /** Ladder max-hold exit used by both the walk-forward and gate. */
  readonly ladderMaxHoldBars?: number;
  /** Per-cycle deep-history fetch request budget (shared across gate candidates). */
  readonly deepFetchBudgetPerCycle?: number;
  /**
   * Eligibility tier: 'readiness' (default) applies the full stage-4
   * readiness board; 'fast' uses the light high-throughput criteria (strict
   * time-split + walk-forward profitability + fills/day floor) while still
   * sweeping the target×ADX dials so watchlist rows carry the full config.
   */
  readonly tier?: UniverseTier;
  /**
   * Execution-parity outcome from the live harness (see real-money-readiness):
   * whether sim fills matched live fills. Gate-scored eligibility propagates
   * this into validateGridEvidence instead of hardcoding a pass — readiness
   * acceptance must not claim parity that was never measured.
   */
  readonly executionParityPassed?: boolean;
  /**
   * Candle source for the market scan. 'gateway' (default) fetches live
   * candles through the market gateway (testnet-wired for bybit-futures —
   * testnet wicks are ~3.3x wider than mainnet and contaminate every
   * downstream metric). 'db-mainnet' reads 5m MAINNET candles from the
   * market-data DB (exchange 'bybit-futures', timeframe '5m') and resamples
   * them to the scan's timeframe — no gateway fetches, no testnet wicks.
   */
  readonly dataSource?: "gateway" | "db-mainnet";
  /**
   * Fill model for the fills/day projection and fill-frequency gates.
   * 'wick' (default) counts a grid level touched by a candle range as a
   * fill (optimistic). 'conservative' discounts touch-counted fills by
   * `fillFraction` — a wick does not guarantee a limit fill at that price
   * (queue/partial-fill risk). db-mainnet scans default to 'conservative'.
   */
  readonly fillModel?: "wick" | "conservative";
  /**
   * Fraction (0..1) of touch-counted fills that actually fill under
   * fillModel 'conservative' (default 0.5).
   */
  readonly fillFraction?: number;
  /**
   * Structural-asymmetry gate threshold: the config's breakeven win rate
   * (avgLossPct / (avgWinPct + avgLossPct) over the walk-forward window
   * trades) must be at most this value (default 0.40 = target >= 1.5x stop).
   * Configs whose average loss is too large relative to their average win
   * are rejected even when walk-forward returns are positive — the ETH
   * 1.25/1/0/1/24 profile (BE 56%) loses -10.3% over 12 mainnet months
   * despite +17.85% testnet walk-forward edge.
   */
  readonly maxBreakevenWinRate?: number;
}

/**
 * Structural walk-forward result shared by the grid and ladder engines: the
 * subset of fields the universe funnel consumes. Both `GridWalkForwardResult`
 * and `LadderWalkForwardResult` satisfy it structurally.
 */
export interface UniverseWalkForwardResult {
  readonly windows: readonly {
    readonly params: {
      readonly gridStepPct: number;
      readonly gridMaxGrids: number;
      readonly gridPauseAfterLossBars: number;
      readonly rungs?: number;
    };
    readonly testReturnPct: number;
    readonly testMaxDrawdownPct: number;
    readonly testTrades: number;
  }[];
  readonly aggregateReturnPct: number;
  readonly profitableWindowsPct: number;
  readonly maxDrawdownPct: number;
  readonly totalTrades: number;
  readonly avgWinPct?: number;
  readonly avgLossPct?: number;
}

export interface GridUniverseEntry {
  readonly symbol: string;
  readonly candles: number;
  readonly bestParams: {
    readonly gridStepPct: number;
    readonly gridMaxGrids: number;
    readonly gridPauseAfterLossBars: number;
    /** Ladder engine only: number of simultaneous rungs per side. */
    readonly rungs?: number;
  };
  readonly walkForward: UniverseWalkForwardResult;
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
  /** Compact fail-closed reason(s) when the entry is not eligible. */
  readonly rejectionReason?: string;
  /** Detailed stage-4 diagnostics retained for research reports. */
  readonly gateFailureReasons?: readonly string[];
  /**
   * Stage-4 gate-scored eligibility: target_ratio of the passing gate combo
   * with the highest compounded return (unset until validated).
   */
  readonly validatedTargetRatio?: number;
  /**
   * Stage-4 gate-scored eligibility: chop-gate ADX threshold of the passing
   * gate combo with the highest compounded return (unset until validated).
   */
  readonly validatedChopGateAdx?: number;
  /**
   * Market scan only: the entry passed walk-forward but NO target×ADX combo
   * cleared the stage-4 gate — kept in `entries` for the report, excluded
   * from `survivors`/selection.
   */
  readonly gatedDropped?: boolean;
}

export interface GridUniverseResult {
  readonly entries: readonly GridUniverseEntry[];
  readonly survivors: readonly GridUniverseEntry[];
  /**
   * Stage-4 gate-scored eligibility drop count (market scan only; the
   * DB-sourced path predates the gate and omits this).
   */
  readonly gateDropped?: number;
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
 * 50/day ceiling. The floor is a ceiling, not a promise: a $10 account
 * cannot hit 5 fills/day pool-wise — the target just stops scaling down.
 */
export function accountScaledTargetFillsPerDay(
  accountCapital = DEFAULT_ACCOUNT_CAPITAL,
): number {
  return Math.min(50, Math.max(5, (50 * accountCapital) / 1000));
}

/**
 * Capital-aware symbol cap: max(1, floor(A × MAX_POSITION_FRACTION /
 * MIN_ORDER_NOTIONAL_USDT)). The max(1, …) keeps tiny accounts (A < 20,
 * where the raw floor is 0) in concentrated mode — at least 1 symbol is
 * always selectable.
 */
export function accountSymbolCap(
  accountCapital = DEFAULT_ACCOUNT_CAPITAL,
): number {
  return Math.max(
    1,
    Math.floor(
      (accountCapital * MAX_POSITION_FRACTION) / MIN_ORDER_NOTIONAL_USDT,
    ),
  );
}

/**
 * Frequency-targeted portfolio selection (degenerate knapsack): greedy
 * top-K by edge/trade descending, taking each candidate's fills/day capped
 * at `perSymbolCap`, until the cumulative fills/day reaches the target.
 * A capital-aware bound (accountSymbolCap: max(1, floor(A ×
 * MAX_POSITION_FRACTION / MIN_ORDER_NOTIONAL_USDT)) symbols) is applied as
 * the final cap; tiny accounts (A < 20) still select 1 symbol
 * (concentrated mode). Entries without a computed edge or fills/day are
 * never selected.
 */
export function selectUniversePortfolio(
  entries: readonly GridUniverseEntry[],
  targetFillsPerDay: number,
  perSymbolCap = DEFAULT_PER_SYMBOL_FILL_CAP,
  accountCapital = DEFAULT_ACCOUNT_CAPITAL,
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
  // Capital-aware bound (final): the account can hold at most
  // max(1, floor(A × fraction / notional)) concurrent grid positions;
  // greedy order already ranks by edge, so slicing keeps the best.
  // $1000 -> 50 symbols; $10 -> 1 (concentrated mode, never 0).
  return selected.slice(0, accountSymbolCap(accountCapital));
}

/**
 * Minutes per bar for a timeframe ("15m" → 15, "5m" → 5) — the candle
 * spacing the stage-4 gate validates against.
 */
function timeframeMinutesFor(timeframe: string): number {
  return 1440 / barsPerDayForTimeframe(timeframe);
}

/**
 * Resample ascending candles (e.g. 5m mainnet rows) into a coarser
 * timeframe by grouping on aligned window boundaries
 * (timestamp floored to targetMinutes). Per group: open = first open,
 * high = max, low = min, close = last close, volume = sum, timestamp =
 * window start, exchange/symbol preserved, timeframe = targetTimeframe.
 * Partial edge groups (1-2 bars at the series ends) are kept — backtest
 * warmup absorbs them and dropping them would silently shorten history.
 * Candles MUST be sorted ascending by timestamp; targetMinutes must be a
 * multiple of the input spacing (5m base for db-mainnet).
 */
export function resampleCandles(
  candles: readonly Candle[],
  targetMinutes: number,
  targetTimeframe: string,
): Candle[] {
  if (candles.length === 0) return [];
  const windowMs = targetMinutes * 60_000;
  const groups = new Map<number, Candle>();
  for (const c of candles) {
    const windowStart = Math.floor(c.timestamp.getTime() / windowMs) * windowMs;
    const existing = groups.get(windowStart);
    if (existing === undefined) {
      groups.set(windowStart, {
        ...c,
        timeframe: targetTimeframe,
        timestamp: new Date(windowStart),
      });
    } else {
      groups.set(windowStart, {
        ...existing,
        high: Math.max(existing.high, c.high),
        low: Math.min(existing.low, c.low),
        close: c.close,
        volume: existing.volume + c.volume,
      });
    }
  }
  return [...groups.entries()]
    .sort((a, b) => a[0] - b[0])
    .map(([, bar]) => bar);
}

/**
 * Structural win/loss asymmetry of a walk-forward run:
 * breakevenWinRate = avgLossPct / (avgWinPct + avgLossPct) — the win rate
 * at which the config breaks even given its average win and average loss
 * sizes. A config with avgWin 1.10% / avgLoss 1.40% (ETH-1.25/1/0/1/24)
 * breaks even at 56%; the BTC/SOL gate-validated profiles (target 3-4)
 * break even at <= 40% (target >= 1.5x stop). Returns undefined when the
 * walk-forward produced no measurable win/loss data (no trades, or a
 * degenerate all-flat series) — callers pass that through (no rejection).
 */
export function breakevenWinRateFromWalkForward(
  walkForward: Pick<UniverseWalkForwardResult, "avgWinPct" | "avgLossPct">,
): number | undefined {
  const { avgWinPct, avgLossPct } = walkForward;
  if (avgWinPct === undefined && avgLossPct === undefined) return undefined;
  const win = avgWinPct ?? 0;
  const loss = avgLossPct ?? 0;
  const denom = win + loss;
  if (!(denom > 0) || !Number.isFinite(denom)) return undefined;
  return loss / denom;
}

/**
 * Effective fill-model multiplier: 1 for 'wick' (touch = fill), fillFraction
 * for 'conservative' (touch x fraction). Defaults: 'wick' for the gateway
 * path, 'conservative' for db-mainnet (mainnet wicks are real, but a touch
 * still does not guarantee a limit fill).
 */
export function fillModelMultiplier(
  options: Pick<
    GridUniverseOptions,
    "fillModel" | "fillFraction" | "dataSource"
  >,
): number {
  if (
    (options.fillModel ??
      (options.dataSource === "db-mainnet" ? "conservative" : "wick")) ===
    "conservative"
  ) {
    return Math.max(0, Math.min(1, options.fillFraction ?? 0.5));
  }
  return 1;
}

/**
 * Stage-4 gate criteria — EXACTLY the manifest sweep gates from
 * scripts/gate-scored-grid-search-2026-08-06.ts: every gate must clear.
 */
function passesGateCriteria(result: GridValidationOk): boolean {
  return (
    result.historical.profitableWindowPct > 50 &&
    result.historical.compoundedReturnPct >= 0 &&
    result.historical.maximumDrawdownPct <= 15 &&
    result.fixedOos.totalTrades >= 30 &&
    result.confidence.lowerBoundPct >= 0 &&
    result.stress.worstReturnPct >= 0 &&
    result.stress.pooledLowerBoundPct >= 0
  );
}

/**
 * Strict time-split honesty check: the readiness gates are trade/window
 * based and cannot see regime concentration (validated 2026-08-09: BTC 15m
 * passed every gate while its edge lived entirely in the last 20% of data,
 * IS -20.99% / OOS +34.06%). A config must also be profitable on a held-out
 * last-20% slice it never trained on.
 */
export function passesTimeSplitGate(
  candles: readonly Candle[],
  grid: GridOptions,
  splitPct = 0.2,
): boolean {
  const split = Math.floor(candles.length * (1 - splitPct));
  const isSlice = runGridBacktest(candles.slice(0, split), {
    ...grid,
    initialCapital: grid.initialCapital,
  });
  const oosSlice = runGridBacktest(candles.slice(split), {
    ...grid,
    initialCapital: grid.initialCapital,
  });
  // BOTH halves must be profitable AND actually trade: the BTC-15m family
  // passed every window gate with IS -21% / OOS +34% (edge entirely recent);
  // a half with zero trades is regime-dead and must fail closed.
  return (
    isSlice.totalTrades >= 1 &&
    oosSlice.totalTrades >= 1 &&
    isSlice.totalReturnPct >= 0 &&
    oosSlice.totalReturnPct >= 0
  );
}

/**
 * Ladder variant of the strict time-split honesty check: both the in-sample
 * and held-out last-20% slices must be profitable AND trade, otherwise the
 * regime-concentration guard fails closed. Identical semantics to
 * `passesTimeSplitGate` but evaluates through the ladder engine.
 */
export function passesLadderTimeSplitGate(
  candles: readonly Candle[],
  ladder: LadderOptions,
  splitPct = 0.2,
): boolean {
  const split = Math.floor(candles.length * (1 - splitPct));
  const isSlice = runLadderGridBacktest(candles.slice(0, split), {
    ...ladder,
    initialCapital: ladder.initialCapital,
  });
  const oosSlice = runLadderGridBacktest(candles.slice(split), {
    ...ladder,
    initialCapital: ladder.initialCapital,
  });
  return (
    isSlice.totalTrades >= 1 &&
    oosSlice.totalTrades >= 1 &&
    isSlice.totalReturnPct >= 0 &&
    oosSlice.totalReturnPct >= 0
  );
}

/**
 * Ladder readiness gate criteria, mirroring the grid board's manifest:
 * majority profitable historical windows, non-negative compounded return,
 * drawdown cap, fixed-OOS trade floor, and non-negative block-bootstrap
 * confidence + pooled adverse-stress lower bounds.
 */
export function passesLadderGateCriteria(result: LadderValidationOk): boolean {
  return (
    result.historical.profitableWindowPct > 50 &&
    result.historical.compoundedReturnPct >= 0 &&
    result.historical.maximumDrawdownPct <= 15 &&
    result.fixedOos.totalTrades >= 30 &&
    result.confidence.lowerBoundPct >= 0 &&
    result.stress.worstReturnPct >= 0 &&
    result.stress.pooledLowerBoundPct >= 0
  );
}

export function ladderGateCriteriaFailures(
  result: LadderValidationOk,
): string[] {
  const failures: string[] = [];
  if (result.historical.profitableWindowPct <= 50) {
    failures.push("historical_profitable_windows");
  }
  if (result.historical.compoundedReturnPct < 0) {
    failures.push("historical_return");
  }
  if (result.historical.maximumDrawdownPct > 15) {
    failures.push("historical_drawdown");
  }
  if (result.fixedOos.totalTrades < 30) {
    failures.push("fixed_oos_trades");
  }
  if (result.confidence.lowerBoundPct < 0) {
    failures.push("confidence_lower_bound");
  }
  if (result.stress.worstReturnPct < 0) {
    failures.push("stress_worst_return");
  }
  if (result.stress.pooledLowerBoundPct < 0) {
    failures.push("stress_pooled_lower_bound");
  }
  return failures;
}

export interface LadderGateScoreResult {
  readonly entry: GridUniverseEntry | null;
  readonly failureReasons: readonly string[];
}

/**
 * Ladder-engine gate-scored eligibility: sweeps the target_ratio × chop-gate
 * ADX dials around the ladder walk-forward bestParams (including rungs)
 * through the ladder backtest + ladder time-split.
 *
 * Fast tier: walk-forward profitability + fills/day floor + strict time-split
 * honesty. Readiness tier: the same sweep must ALSO clear the full ladder
 * evidence validator (data quality, historical windows, fixed-OOS >= 30
 * trades, block-bootstrap confidence LB >= 0, pooled 5-seed stress LB >= 0).
 *
 * The ladder deliberately OMITS the single-position structural-asymmetry
 * gate (BE <= 0.40): the ladder's edge is frequency-based — a tight TP yields
 * a high win rate whose many small wins outweigh fewer, larger stops
 * (BE ~0.7), which the win/loss-ratio gate misclassifies as doomed.
 */
export function ladderGateScoredEligibilityDetailed(
  entry: GridUniverseEntry,
  candles: readonly Candle[],
  options: GridUniverseOptions,
): LadderGateScoreResult {
  const tier = options.tier ?? "readiness";
  if (!entry.passed) {
    return { entry: null, failureReasons: ["walk_forward"] };
  }
  const fillFloorOk =
    computeFillFrequencyPct(
      candles,
      entry.bestParams.gridStepPct,
      FAST_TIER_MIN_FILL_FREQUENCY_PCT,
    ) *
      fillModelMultiplier(options) >=
    FAST_TIER_MIN_FILL_FREQUENCY_PCT;
  if (!fillFloorOk) {
    return { entry: null, failureReasons: ["fill_frequency_floor"] };
  }

  // Noise gate: the grid step must be a meaningful multiple of the symbol's
  // per-bar range (ATR as % of price). A step inside the noise band fills
  // constantly on tick-level churn — the ladder wins a hair under one step
  // and loses the full N+grids stop, an inverted R:R that bleeds the account
  // even at a high win rate (regression 2026-08-19: PUMPFUN at 0.75% step
  // churned 190 round-trips for -$12.89 on a $50 account). ATR14 is the
  // typical bar's range, so step >= 3x ATR means the target/stop sits outside
  // the noise band and trades capture real directional moves. A zero ATR
  // (null volatility on degenerate candles with no sort/sort) skips the gate.
  const atrPct = entry.volatility ?? 0;
  if (atrPct > 0 && entry.bestParams.gridStepPct < atrPct * 1.5) {
    return { entry: null, failureReasons: ["step_inside_atr_noise"] };
  }

  const failureReasons = new Set<string>();

  let best: {
    targetRatio: number;
    chopGateAdx: number;
    totalReturnPct: number;
  } | null = null;
  let timeSplitPasses = 0;
  let evidenceChecks = 0;
  let evidencePasses = 0;
  for (const targetRatio of GATE_TARGETS) {
    for (const chopGateAdx of GATE_ADX_GATES) {
      const ladder: LadderOptions = {
        rungs: entry.bestParams.rungs ?? 1,
        gridStepPct: entry.bestParams.gridStepPct,
        gridMaxGrids: entry.bestParams.gridMaxGrids,
        gridPauseAfterLossBars: entry.bestParams.gridPauseAfterLossBars,
        feePct: options.feePct,
        slippageBps: options.slippageBps,
        initialCapital: options.initialCapital,
        trendFilterPeriod: options.trendFilterPeriod,
        leverage: 1,
        positionFraction: MAX_POSITION_FRACTION,
        chopGateAdxThreshold: chopGateAdx,
        targetRatio,
        onlyWithTrend: false,
        stopRatio: options.ladderStopRatio ?? 0,
        maxHoldBars: options.ladderMaxHoldBars ?? 0,
        conservativeIntrabar: true,
      };
      if (!passesLadderTimeSplitGate(candles, ladder)) continue;
      timeSplitPasses += 1;
      if (tier === "readiness") {
        // Readiness tier: the combo must ALSO clear the full ladder evidence
        // validator (windows scaled to the available history, same protocol
        // as the grid board).
        const n = candles.length;
        const trainBars = Math.min(11520, Math.max(200, Math.floor(n * 0.6)));
        const testBars = Math.min(4320, Math.max(50, Math.floor(n * 0.2)));
        const minimumWindows = Math.max(
          1,
          Math.floor((n - trainBars - testBars) / testBars),
        );
        const evidence = validateLadderEvidence(candles, {
          now: new Date(),
          timeframeMinutes: timeframeMinutesFor(options.timeframe),
          trainBars,
          testBars,
          minimumWindows,
          ladder,
        });
        evidenceChecks += 1;
        if (evidence.kind !== "ok") {
          failureReasons.add(
            `evidence_invalid:${evidence.failures[0] ?? "unknown"}`,
          );
          continue;
        }
        const criteriaFailures = ladderGateCriteriaFailures(evidence);
        if (!passesLadderGateCriteria(evidence)) {
          for (const failure of criteriaFailures) failureReasons.add(failure);
          continue;
        }
        evidencePasses += 1;
      }
      const totalReturnPct = runLadderGridBacktest(
        candles,
        ladder,
      ).totalReturnPct;
      // The time-split already requires each half >= 0; this strict-positive
      // check drops the degenerate all-flat case (both halves exactly 0).
      if (totalReturnPct <= 0) continue;
      if (best === null || totalReturnPct > best.totalReturnPct) {
        best = { targetRatio, chopGateAdx, totalReturnPct };
      }
    }
  }
  if (best === null) {
    if (timeSplitPasses === 0) failureReasons.add("time_split");
    if (tier === "readiness" && evidenceChecks > 0 && evidencePasses === 0) {
      failureReasons.add("readiness_evidence");
    }
    if (failureReasons.size === 0) failureReasons.add("tail_return");
    return {
      entry: null,
      failureReasons: [...failureReasons].slice(0, 8),
    };
  }
  return {
    entry: {
      ...entry,
      validatedTargetRatio: best.targetRatio,
      validatedChopGateAdx: best.chopGateAdx,
    },
    failureReasons: [],
  };
}

export function ladderGateScoredEligibility(
  entry: GridUniverseEntry,
  candles: readonly Candle[],
  options: GridUniverseOptions,
): GridUniverseEntry | null {
  return ladderGateScoredEligibilityDetailed(entry, candles, options).entry;
}

/**
 * Stage-4 gate-scored eligibility: sweep the target_ratio × chop-gate-ADX
 * dials (GATE_TARGETS × GATE_ADX_GATES) around the entry's walk-forward
 * bestParams through validateGridEvidence, applying the manifest gate
 * criteria. Returns the entry annotated with the passing combo with the
 * highest compounded return, or null when no combo clears every gate.
 * Per-entry (not batch) because the scan holds candles only transiently per
 * symbol.
 */
export function gateScoredEligibility(
  entry: GridUniverseEntry,
  candles: readonly Candle[],
  options: GridUniverseOptions,
): GridUniverseEntry | null {
  const tier = options.tier ?? "readiness";
  if (options.engine === "ladder") {
    return ladderGateScoredEligibilityDetailed(entry, candles, options).entry;
  }
  // Structural-asymmetry gate (both tiers): the config's breakeven win rate
  // over the walk-forward window trades must be <= maxBreakevenWinRate
  // (default 0.40 = target >= 1.5x stop). Rejects profiles like ETH
  // 1.25/1/0/1/24 (BE 56%) whose average loss overwhelms the average win
  // despite positive walk-forward returns.
  const maxBreakevenWinRate = options.maxBreakevenWinRate ?? 0.4;
  const breakevenWinRate = breakevenWinRateFromWalkForward(entry.walkForward);
  const asymmetryOk =
    breakevenWinRate === undefined || breakevenWinRate <= maxBreakevenWinRate;
  // Fast-tier acceptance (light): walk-forward profitability (the entry's
  // `passed` flag) + a fills/day floor from computeFillFrequencyPct (5%
  // touch on the sweep's grid step) + the strict time-split honesty check.
  // The readiness window/OOS>=30/stress-LB requirements are dropped. The
  // floor is checked here (not just via options.minFillFrequencyPct) so the
  // touch rate is measured honestly even when that option is disabled. The
  // conservative fill model discounts the floor (modeled fills = touch x
  // fillFraction) — a testnet-grade wick touch must not clear the floor at
  // face value.
  const fastEligible =
    tier === "fast" &&
    entry.passed &&
    asymmetryOk &&
    computeFillFrequencyPct(
      candles,
      entry.bestParams.gridStepPct,
      FAST_TIER_MIN_FILL_FREQUENCY_PCT,
    ) *
      fillModelMultiplier(options) >=
      FAST_TIER_MIN_FILL_FREQUENCY_PCT;

  let best: {
    targetRatio: number;
    chopGateAdx: number;
    compoundedReturnPct: number;
  } | null = null;
  for (const targetRatio of GATE_TARGETS) {
    for (const chopGateAdx of GATE_ADX_GATES) {
      const grid: GridOptions = {
        gridStepPct: entry.bestParams.gridStepPct,
        gridMaxGrids: entry.bestParams.gridMaxGrids,
        gridPauseAfterLossBars: entry.bestParams.gridPauseAfterLossBars,
        feePct: options.feePct,
        slippageBps: options.slippageBps,
        initialCapital: options.initialCapital,
        trendFilterPeriod: options.trendFilterPeriod,
        leverage: 1,
        positionFraction: MAX_POSITION_FRACTION,
        chopGateAdxThreshold: chopGateAdx,
        targetRatio,
        onlyWithTrend: false,
      };
      // Scale the validator's rolling windows to the available history: the
      // readiness defaults (11520/4320/10 windows) need ~55k candles, which
      // young symbols lack. Windows shrink with the data; the fixed-OOS
      // >=30 trades and LB gates still bind regardless of history depth.
      const n = candles.length;
      const trainBars = Math.min(11520, Math.max(200, Math.floor(n * 0.6)));
      const testBars = Math.min(4320, Math.max(50, Math.floor(n * 0.2)));
      const minimumWindows = Math.max(
        1,
        Math.floor((n - trainBars - testBars) / testBars),
      );
      const result = validateGridEvidence(candles, {
        now: new Date(),
        timeframeMinutes: timeframeMinutesFor(options.timeframe),
        trainBars,
        testBars,
        minimumWindows,
        grid,
        executionParityPassed: options.executionParityPassed ?? false,
      });
      if (tier === "readiness") {
        // Full readiness board: valid evidence must clear EVERY gate AND
        // survive the strict time-split (regime-concentration guard) AND
        // the structural-asymmetry requirement (BE win rate <= 0.40);
        // invalid evidence fails closed.
        if (result.kind !== "ok" || !passesGateCriteria(result)) continue;
        if (!asymmetryOk) continue;
        if (!passesTimeSplitGate(candles, grid)) continue;
      } else {
        // Fast tier: the light criteria are the ONLY acceptance — evidence
        // is used just to rank the sweep combos (young symbols may lack the
        // deep history valid evidence needs; that alone must not drop them).
        if (!fastEligible) continue;
        if (!passesTimeSplitGate(candles, grid)) continue;
        if (result.kind !== "ok") {
          // Fast tier tolerates ONLY history-depth failures (complete-window
          // count below minimum, fixed-OOS trade count below minimum): a
          // symbol with shallow history may still be admitted on the light
          // criteria. ANY data-quality failure (stale/gapped/negative-volume
          // candles, invalid timestamps, historical return at/below -100%)
          // is a hard drop even in the light tier — fast admission must not
          // paper over bad data.
          const depthOnlyFailures =
            result.dataQuality.failures.every((failure) =>
              failure.startsWith("complete window count"),
            ) &&
            result.failures.every((failure) =>
              failure.startsWith("fixed OOS trade count"),
            );
          if (!depthOnlyFailures) continue;
          // Invalid evidence cannot rank; keep the first passing combo.
          if (best === null) {
            best = {
              targetRatio,
              chopGateAdx,
              compoundedReturnPct: -Infinity,
            };
          }
          continue;
        }
      }
      if (
        best === null ||
        result.historical.compoundedReturnPct > best.compoundedReturnPct
      ) {
        best = {
          targetRatio,
          chopGateAdx,
          compoundedReturnPct: result.historical.compoundedReturnPct,
        };
      }
    }
  }
  if (best === null) return null;
  return {
    ...entry,
    validatedTargetRatio: best.targetRatio,
    validatedChopGateAdx: best.chopGateAdx,
  };
}

function evaluateUniverseSymbol(
  symbol: string,
  candles: readonly Candle[],
  options: GridUniverseOptions,
): GridUniverseEntry {
  const engine = options.engine ?? "grid";
  const walkForward: UniverseWalkForwardResult =
    engine === "ladder"
      ? runLadderGridWalkForward(candles, {
          trainWindow: options.trainWindow,
          testWindow: options.testWindow,
          initialCapital: options.initialCapital,
          searchSpace: {
            rungs: options.searchSpace.rungs ?? [1],
            gridStepPct: options.searchSpace.gridStepPct,
            gridMaxGrids: options.searchSpace.gridMaxGrids,
            gridPauseAfterLossBars: options.searchSpace.gridPauseAfterLossBars,
            // Sweep the R:R and chop-gate dials INSIDE walk-forward
            // selection. Fixed at [1]/[0] the wf objective is inverted-R:R
            // churn (win one step, lose gridMaxGrids steps) — it selects
            // configs that bleed OOS on every symbol while the profitable
            // gated geometry never reaches the gate stage (2026-08-14:
            // 0/25 survivors on a full-year db-mainnet scan; same configs
            // flip positive under tr=3-4/adx=12, see
            // scripts/probe-targetratio-impact.ts).
            // ponytail: two-stage cost control — wf sweeps only the
            // live-validated geometry (adx=12; tr 3|4). adx=0 combos always
            // lose and each ADX-enabled backtest pays per-bar ADX compute,
            // so sweeping all 4×3 dials here made a full scan >60min. The
            // gate stage below still sweeps the full GATE_TARGETS ×
            // GATE_ADX_GATES board for final validation.
            targetRatio: [3, 4],
            chopGateAdxThreshold: [12],
          },
          baseOptions: {
            feePct: options.feePct,
            slippageBps: options.slippageBps,
            trendFilterPeriod: options.trendFilterPeriod,
            leverage: 1,
            stopRatio: options.ladderStopRatio ?? 0,
            maxHoldBars: options.ladderMaxHoldBars ?? 0,
            conservativeIntrabar: true,
          },
          // Select among training-window configurations that are executable
          // under the same conservative fill model used by the downstream
          // gate. Without this, the selector can choose a high-return,
          // wide-step config and reject it later for having too few fills.
          candidateFilter:
            (options.minFillFrequencyPct ?? 0) > 0
              ? (trainCandles, candidate) => {
                  const minimum = options.minFillFrequencyPct ?? 0;
                  return (
                    computeFillFrequencyPct(
                      trainCandles,
                      candidate.gridStepPct,
                      minimum,
                    ) *
                      fillModelMultiplier(options) >=
                    minimum
                  );
                }
              : undefined,
        })
      : runGridWalkForward(candles, {
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
  const bestParams = {
    gridStepPct:
      lastWindow?.params.gridStepPct ?? options.searchSpace.gridStepPct[0] ?? 1,
    gridMaxGrids:
      lastWindow?.params.gridMaxGrids ??
      options.searchSpace.gridMaxGrids[0] ??
      2,
    gridPauseAfterLossBars:
      lastWindow?.params.gridPauseAfterLossBars ??
      options.searchSpace.gridPauseAfterLossBars[0] ??
      0,
    rungs: lastWindow?.params.rungs,
  };

  // Zero windows (candles shorter than train+test) yields profitableWindowsPct
  // and aggregateReturnPct of 0, which a permissive minimum could "pass" with
  // no walk-forward evidence and no trades — fail closed instead.
  const passedBase =
    walkForward.windows.length >= 1 &&
    walkForward.profitableWindowsPct >= options.minProfitableWindowsPct &&
    walkForward.aggregateReturnPct >= options.minAggregateReturnPct;

  // Structural-asymmetry gate: a config whose average loss is too large
  // relative to its average win must be rejected even when walk-forward
  // returns are positive (the ETH-1.25/1/0/1/24 profile has BE 56% and
  // loses -10.3% over 12 mainnet months despite +17.85% testnet edge).
  const maxBreakevenWinRate = options.maxBreakevenWinRate ?? 0.4;
  const breakevenWinRate = breakevenWinRateFromWalkForward(walkForward);
  // Ladder: the walk-forward now sweeps targetRatio/ADX itself (same dial
  // sets as the gate), so avgWin/avgLoss reflects real geometry. The ladder
  // gate re-checks asymmetry on its own swept winning combo anyway.
  // (Historical note: when wf ran at fixed tr=1/adx=0 this check compared
  // apples to oranges — kept engine === "ladder" bypass for continuity.)
  const asymmetryOk =
    engine === "ladder" ||
    breakevenWinRate === undefined ||
    breakevenWinRate <= maxBreakevenWinRate;

  // Fill model: 'wick' counts every touch as a fill; 'conservative' (the
  // db-mainnet default) discounts touches by fillFraction — a wick does not
  // guarantee a limit fill at that price. The gate and fills/day projection
  // both use the modeled fill rate.
  const fillMultiplier = fillModelMultiplier(options);
  const fillGate = options.minFillFrequencyPct ?? 0;
  const fillFrequencyPct = computeFillFrequencyPct(
    candles,
    bestParams.gridStepPct,
    fillGate,
  );
  const modeledFillPct = fillFrequencyPct * fillMultiplier;

  const passed = passedBase && asymmetryOk && modeledFillPct >= fillGate;

  const rejectionReasons: string[] = [];
  if (walkForward.windows.length < 1) rejectionReasons.push("no_wf_windows");
  if (walkForward.profitableWindowsPct < options.minProfitableWindowsPct) {
    rejectionReasons.push("wf_profit_windows");
  }
  if (walkForward.aggregateReturnPct < options.minAggregateReturnPct) {
    rejectionReasons.push("wf_return");
  }
  if (!asymmetryOk) rejectionReasons.push("asymmetry");
  if (modeledFillPct < fillGate) rejectionReasons.push("fill_frequency");

  // Candidate metrics for the frequency-targeted selection stage. All are
  // display/selection hints, not money — plain numbers are fine.
  const stats = computeSymbolStats(candles, options.timeframe);
  // OOS trade count: GridWalkForwardWindow carries `testTrades` per window;
  // the result's `totalTrades` is exactly their sum.
  const oosTrades = walkForward.totalTrades;
  // ponytail: computeFillFrequencyPct(…, 0) reports 100 (gate disabled), so
  // fillsPerDay degrades to the bars/day upper bound; to measure the real
  // touch rate, pass options.minFillFrequencyPct when it's > 0. The
  // conservative fill model discounts the projection (fills/day = modeled
  // fill rate x bars/day).
  const fillsPerDay =
    (modeledFillPct / 100) * barsPerDayForTimeframe(options.timeframe);
  // Approximation: aggregate OOS return spread evenly over OOS trades —
  // ignores compounding and win/loss asymmetry, but ranks candidates fairly.
  const edgePerTradePct =
    walkForward.aggregateReturnPct / Math.max(oosTrades, 1);

  return {
    symbol,
    candles: candles.length,
    bestParams: {
      gridStepPct: bestParams.gridStepPct,
      gridMaxGrids: bestParams.gridMaxGrids,
      gridPauseAfterLossBars: bestParams.gridPauseAfterLossBars,
      rungs: bestParams.rungs,
    },
    walkForward,
    passed,
    volatility: stats.atr14Pct,
    oosTrades,
    fillsPerDay,
    edgePerTradePct,
    rejectionReason:
      rejectionReasons.length > 0 ? rejectionReasons.join(",") : undefined,
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

    const tier = options.tier ?? "readiness";
    yield* Effect.log(
      tier === "fast"
        ? "universe scan: tier=fast (light: time-split + walk-forward + fills/day floor)"
        : "universe scan: tier=readiness (full gate board)",
    );

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
  // delay between every request (observed HTTP 429 mid-scan). The delay is
  // CONSTANT — shrinking it when the per-cycle budget is small (tests,
  // constrained runs) increases burstiness exactly when quota headroom is
  // lowest; the budget Ref alone bounds volume.
  const REQUEST_DELAY_MS = 250;
  const requestDelayMs = REQUEST_DELAY_MS;
  // Tail-fetch concurrency: the sequential pacing is the binding
  // constraint on batch size, not the rate limit — 2 workers keep the batch
  // ~2x faster while staying well under Bitget's public limit.
  const TAIL_CONCURRENCY = 2;

  return Effect.gen(function* () {
    const gateway = yield* MarketDataGateway;
    const repo = yield* MarketDataRepository;

    const tier = options.tier ?? "readiness";
    const dataSource = options.dataSource ?? "gateway";
    yield* Effect.log(
      tier === "fast"
        ? `market scan: tier=fast (light: time-split + walk-forward + fills/day floor) dataSource=${dataSource}`
        : `market scan: tier=readiness (full gate board) dataSource=${dataSource}`,
    );

    // db-mainnet candles come from the 5m mainnet cache; the scan
    // timeframe must be a resample of 5m (5m itself = identity).
    const targetMinutes = timeframeMinutesFor(options.timeframe);
    if (
      dataSource === "db-mainnet" &&
      (targetMinutes % 5 !== 0 || targetMinutes < 5)
    ) {
      return yield* Effect.fail(
        new MarketDataRepositoryError(
          `db-mainnet data source cannot resample 5m candles to timeframe '${options.timeframe}' (${targetMinutes} min bars) — use a multiple of 5m`,
        ),
      );
    }

    let candidates: string[];
    if (dataSource === "db-mainnet") {
      // Mainnet-fidelity universe: symbol discovery comes from the 5m
      // mainnet cache (fetch-flow-mainnet top-100 liquid symbols), NOT
      // from the testnet gateway's contract list / 24h volumes / demo
      // subset. Zero gateway calls on this path — the testnet demo list
      // would re-contaminate the universe.
      candidates = (yield* repo.listSymbolsByCandleCount(
        options.exchange,
        "5m",
        Math.ceil((options.minCandles * targetMinutes) / 5),
      ))
        .filter((s) => s.count >= options.minCandles)
        .map((s) => s.symbol);
    } else {
      // fetchDemoSymbols IS fetchSymbols for bybit-futures (same paginated
      // instruments-info endpoint) — call it once and reuse, or every scan
      // cycle pages through the full instrument list twice.
      const [marketSymbols, volumes] = yield* Effect.all([
        gateway.fetchSymbols(options.exchange),
        gateway.fetch24hrVolumes(options.exchange),
      ]);
      const demoSymbols = yield* gateway
        .fetchDemoSymbols(options.exchange)
        .pipe(Effect.orElseSucceed(() => marketSymbols));
      // Tickers key volumes by "BTCUSDT" while fetchSymbols returns
      // "BTC/USDT"; normalize so the liquidity filter sees the same keys.
      const normalizedVolumes = new Map<string, number>(
        Object.entries(volumes).map(([symbol, volume]) => [
          symbol.includes("/") ? symbol : symbol.replace(/USDT$/, "/USDT"),
          volume,
        ]),
      );
      // Hard universe bound: the demo/tradeable instrument subset. The live
      // list (~741 contracts) includes contracts the simulated engine cannot
      // trade — scanning them wastes the whole cycle on symbols the
      // tradeability probe then drops (verified 2026-08-10: TIA/IOTX/WLFI/
      // GRVT/CYS 40034 on the demo account; the PAPTRADING-scoped list is
      // ~25 majors). Gateways without a demo concept return the full list,
      // making this filter a no-op.
      const demoSet = new Set(
        demoSymbols.map((symbol) =>
          symbol.includes("/") ? symbol : symbol.replace(/USDT$/, "/USDT"),
        ),
      );

      candidates = marketSymbols
        .filter(
          (symbol) =>
            (normalizedVolumes.get(symbol) ?? 0) >=
            MIN_UNIVERSE_24H_VOLUME_USDT,
        )
        .filter((symbol) => demoSet.has(symbol))
        // Canonical futures form ("BTC/USDT:USDT") — the convention the
        // watchlist, soak, and grid engine all expect.
        .map((symbol) => (symbol.includes(":") ? symbol : `${symbol}:USDT`));
    }

    const fetchBatch = (symbol: string, startTime: Date | undefined) =>
      Effect.gen(function* () {
        yield* Effect.sleep(requestDelayMs);
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
          const failure = outcome.failure as {
            reason?: string;
            retryAfterMs?: number;
          };
          const reason =
            failure?.reason ??
            (outcome.failure instanceof Error
              ? outcome.failure.message
              : String(outcome.failure));
          if (!isTransient(reason)) return yield* Effect.fail(outcome.failure);
          attempt += 1;
          if (attempt >= 6) {
            yield* Effect.logWarning(
              `Transient failure past retries; skipping ${symbol} this cycle`,
            );
            return [] as readonly Candle[];
          }
          // Honor the server's Retry-After hint when present (429s), else
          // exponential backoff.
          const backoffMs = failure?.retryAfterMs ?? 1_000 * 2 ** attempt;
          yield* Effect.sleep(backoffMs);
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
    // db-mainnet: candles come from the 5m mainnet cache, resampled to the
    // scan timeframe in memory — NO gateway fetch, NO testnet wicks, and
    // the resampled bars are NEVER written back (writing them under the
    // scan timeframe key would mix mainnet-resampled and testnet-native
    // rows in ohlcv_data).
    const dbMainnetCandles = (symbol: string, targetBars: number) =>
      Effect.gen(function* () {
        const fiveMin = yield* repo.getCandles({
          exchange: options.exchange,
          symbol,
          timeframe: "5m",
          // Fetch 5m rows covering targetBars at the scan timeframe plus a
          // small slack (a partial edge group would otherwise yield one bar
          // short of the requested depth).
          limit: Math.ceil((targetBars * targetMinutes) / 5) + 4,
        });
        if (fiveMin.length === 0) return [] as readonly Candle[];
        const resampled = resampleCandles(
          fiveMin,
          targetMinutes,
          options.timeframe,
        );
        return resampled.slice(-targetBars);
      });

    const ensureCandles = (symbol: string) =>
      Effect.gen(function* () {
        if (dataSource === "db-mainnet") {
          return yield* dbMainnetCandles(symbol, options.minCandles);
        }
        const range = yield* repo.getCandleRange(
          options.exchange,
          symbol,
          options.timeframe,
        );
        const latest = range.latest;
        if (latest !== null) {
          const tail = yield* withRetry(symbol, () =>
            fetchBatch(symbol, new Date(latest.getTime() + timeframeMillis)),
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

    const deepBudget = yield* Ref.make(
      options.deepFetchBudgetPerCycle ?? DEEP_FETCH_REQUESTS_PER_CYCLE,
    );

    // Top up a gate candidate's cache backward toward DEEP_HISTORY_TARGET,
    // bounded by the shared per-cycle request budget. Returns the deep
    // history for gate validation (persisted so later cycles resume).
    // db-mainnet: the 5m mainnet cache IS the history ceiling (fetch-flow-
    // mainnet backfilled ~12 months); read + resample whatever exists, no
    // gateway fetches, no budget spend, no writes.
    const deepFetch = (symbol: string, existingCount: number) =>
      Effect.gen(function* () {
        if (dataSource === "db-mainnet") {
          return yield* dbMainnetCandles(symbol, DEEP_HISTORY_TARGET);
        }
        if (existingCount >= DEEP_HISTORY_TARGET) {
          return yield* repo.getCandles({
            exchange: options.exchange,
            symbol,
            timeframe: options.timeframe,
            limit: DEEP_HISTORY_TARGET,
          });
        }
        const byTimestamp = new Map<number, Candle>();
        const existing = yield* repo.getCandles({
          exchange: options.exchange,
          symbol,
          timeframe: options.timeframe,
          limit: DEEP_HISTORY_TARGET,
        });
        for (const candle of existing) {
          byTimestamp.set(candle.timestamp.getTime(), candle);
        }
        const fetched: Candle[] = [];
        let startTime: Date | undefined;
        let oldest = [...byTimestamp.keys()].sort((a, b) => a - b)[0];
        if (oldest !== undefined)
          startTime = new Date(oldest - timeframeMillis);
        while (byTimestamp.size < DEEP_HISTORY_TARGET) {
          const budget = yield* Ref.get(deepBudget);
          if (budget <= 0) break;
          yield* Ref.update(deepBudget, (b) => b - 1);
          const batch = yield* withRetry(symbol, () =>
            fetchBatch(symbol, startTime),
          );
          if (batch.length === 0) break;
          for (const candle of batch) {
            byTimestamp.set(candle.timestamp.getTime(), candle);
            fetched.push(candle);
          }
          oldest = [...byTimestamp.keys()].sort((a, b) => a - b)[0];
          startTime = new Date(oldest - timeframeMillis);
        }
        if (fetched.length > 0) yield* repo.saveCandles(fetched);
        return [...byTimestamp.values()]
          .sort((a, b) => a.timestamp.getTime() - b.timestamp.getTime())
          .slice(-DEEP_HISTORY_TARGET);
      });

    const entries: GridUniverseEntry[] = [];
    const scanned = yield* Ref.make(0);
    // Stage-4 gate drop counter (walk-forward survivors that cleared no
    // target×ADX combo) — surfaced in GridUniverseResult.gateDropped.
    const gateDropped = yield* Ref.make(0);

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

        // Stage-2 cheap-stats screen: reject chop (ADX < 15), dead
        // (ATR% < 0.02), and moon-shot (ATR% > 10) candidates from the
        // cached candles BEFORE the expensive walk-forward runs. Apply this
        // to db-mainnet too: the expanded cache is a market universe, and
        // letting dead/choppy symbols reach the ladder sweep made a 100-name
        // readiness run needlessly expensive.
        const stats = computeSymbolStats(candles, options.timeframe);
        if (!passesStage2Screen(stats)) return;

        const entry = evaluateUniverseSymbol(symbol, candles, options);
        // Stage-4 gate-scored eligibility (walk-forward survivors only):
        // sweep target_ratio × chop-gate-ADX dials around the walk-forward's
        // bestParams through validateGridEvidence. A survivor clearing no
        // combo is dropped from selection (counted for the funnel summary,
        // flagged in the report table, kept out of `survivors`).
        if (entry.passed) {
          // Gate candidates need deep history (~55k bars for 10+ windows):
          // top up the cache backward under a shared per-cycle request
          // budget; the cache grows over cycles until eligible. The ladder
          // gate evaluates on a bounded recent tail instead (its backtest +
          // time-split do not use the rolling-window validator).
          let deep: readonly Candle[];
          if (options.engine === "ladder") {
            deep =
              dataSource === "db-mainnet"
                ? yield* dbMainnetCandles(symbol, LADDER_GATE_TAIL)
                : yield* repo.getCandles({
                    exchange: options.exchange,
                    symbol,
                    timeframe: options.timeframe,
                    limit: LADDER_GATE_TAIL,
                  });
          } else {
            deep = yield* deepFetch(symbol, candles.length);
          }
          const gateResult =
            options.engine === "ladder"
              ? ladderGateScoredEligibilityDetailed(entry, deep, options)
              : {
                  entry: gateScoredEligibility(entry, deep, options),
                  failureReasons: [] as readonly string[],
                };
          if (gateResult.entry === null) {
            yield* Ref.update(gateDropped, (n) => n + 1);
            entries.push({
              ...entry,
              gatedDropped: true,
              gateFailureReasons: gateResult.failureReasons,
              rejectionReason: entry.rejectionReason
                ? `${entry.rejectionReason},stage4_gate`
                : "stage4_gate",
            });
            return;
          }
          entries.push(gateResult.entry);
          return;
        }
        entries.push(entry);
      });

    yield* Effect.forEach(candidates, scanSymbol, {
      concurrency: TAIL_CONCURRENCY,
    });

    const dropped = yield* Ref.get(gateDropped);
    const survivors = entries.filter((e) => e.passed && !e.gatedDropped);

    return { entries, survivors, gateDropped: dropped };
  });
}
