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
  type FuturesPosition,
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
  ContractSizeSpec,
  GridPaperPositionSide,
  GridPaperState,
  GridPaperTrade,
} from "./types.js";
import { orderableQty } from "./types.js";
import { reconcileLivePosition } from "./live-position-reconciliation.js";
import {
  DEFAULT_STRATEGY_MANIFEST,
  fingerprintStrategyManifest,
  type StrategyManifest,
} from "../scalping/real-money-readiness.js";

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
  /** Per-side TAKER fee (percent) for stop / liquidation exits; defaults to feePct. */
  readonly takerExitFeePct?: number;
  /**
   * Funding cost accrued on the open position every 8h held, in percent of
   * notional per interval (signed: positive = longs pay). Default 0 = off.
   */
  readonly fundingRatePct8h?: number;
  /**
   * Maintenance-margin rate (fraction of notional) in the liquidation model.
   * Default 0 keeps the legacy 1/L distance.
   */
  readonly maintenanceMarginRate?: number;
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
  readonly executionEnvironment?:
    | "bitget-demo"
    | "bitget-live"
    | "bybit-demo"
    | "bybit-live";
  /** Exchange contract size constraints (minQty, qtyStep, minTradeUSDT) for
   *  this symbol, populated by the CLI from BitgetClient.getContracts on the
   *  live path and by tests directly. When set, entry sizing rounds the order
   *  qty UP to the size step, raises a sub-minimum qty to minQty, lifts
   *  leverage so the minimum orderable margin fits the allocation cap, and
   *  SKIPS the entry (never placing an unorderable or over-cap order) when
   *  the margin cannot fit even at max leverage. Absent => legacy sizing
   *  (no step rounding). */
  readonly contractSpecs?: ContractSizeSpec;
}

/** Funding interval in ms (perpetual funding settles every 8h). */
const FUNDING_INTERVAL_MS = 8 * 3_600_000;

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
  if (period < 1) return null;
  if (i < period - 1) return null;
  let sum = 0;
  for (let j = i - period + 1; j <= i; j++) sum += candles[j].close;
  return sum / period;
}

function makeId(): string {
  return `grid-paper-${Date.now()}-${Math.random().toString(36).slice(2, 8)}`;
}

/**
 * Build the post-entry GridPaperState for a live position recorded from the
 * exchange rather than from a place-order fill (reconciliation adoption, or
 * adoption after a lost fill confirmation). Carries the same provenance
 * fields as a normal live entry so readiness-fingerprint checks and exit
 * rules keep working. entryFillSource "adopted" is accepted by the
 * reconciliation as a live-bound state.
 */
function adoptedGridState(
  state: GridPaperState,
  position: FuturesPosition,
  strategyFingerprint: string,
  executionEnvironment:
    | "bitget-demo"
    | "bitget-live"
    | "bybit-demo"
    | "bybit-live",
  now: Date,
): GridPaperState {
  return {
    ...state,
    side: position.side,
    entryPrice: position.entryPrice,
    entryOrderId: "adopted",
    entryClientOid: undefined,
    entryFilledQty: position.quantity,
    entryFee: money(0),
    entryFillSource: "adopted",
    leverage: position.leverage,
    strategyConfigFingerprint: strategyFingerprint,
    cohortId: `grid-${strategyFingerprint.slice(0, 16)}`,
    candidateLockAt: now,
    datasetCutoffAt: now,
    entryOpenedAt: now,
    executionEnvironment,
    updatedAt: now,
  };
}

interface GridOrderSizing {
  readonly size: Decimal;
  readonly leverage: number;
  /** Set when the minimum orderable position cannot fit the allocation cap;
   *  the caller must skip the entry (never place an unorderable order). */
  readonly skipReason?: string;
}

function orderSizeContracts(
  capital: Decimal,
  maxPositionPct: number,
  entryPrice: Decimal,
  options?: {
    readonly leverage: number;
    readonly contractSpecs?: ContractSizeSpec;
  },
): GridOrderSizing {
  if (entryPrice.lessThanOrEqualTo(0)) {
    return { size: money(0), leverage: options?.leverage ?? 1 };
  }
  const allocation = capital.times(maxPositionPct / 100);
  const leverage = options?.leverage ?? 1;
  const spec = options?.contractSpecs;
  if (spec === undefined) {
    // Legacy sizing: allocation / price, no step rounding.
    return { size: Decimal.max(0, allocation.div(entryPrice)), leverage };
  }

  // Contract-aware orderability (live path): round the qty UP to the exchange
  // size step (0.000077 BTC at $64,795 would be unorderable; 0.0001 is not),
  // raise a sub-minimum qty to minQty, and lift leverage so the minimum
  // orderable margin fits the allocation cap, bounded by max leverage.
  const effectiveFloor = Decimal.max(
    money(spec.minTradeUSDT),
    money(spec.minQty).times(entryPrice),
  );
  const qty = orderableQty(
    allocation.div(entryPrice),
    spec,
    entryPrice,
    allocation,
  );
  const notional = qty.times(entryPrice);

  let raisedLeverage = leverage;
  if (notional.lessThan(effectiveFloor)) {
    const allocNum = toNumber(allocation);
    if (allocNum > 0) {
      raisedLeverage = Math.min(
        leverage,
        Math.max(1, Math.ceil(toNumber(effectiveFloor) / allocNum)),
      );
    }
  }

  // Never attempt an unorderable or over-cap order: if the minimum orderable
  // margin cannot fit the allocation cap even after the leverage raise, skip
  // the entry instead of sending an order the exchange will reject.
  const margin = notional.div(raisedLeverage);
  if (margin.greaterThan(allocation)) {
    return {
      size: money(0),
      leverage: raisedLeverage,
      skipReason: `min orderable notional ${notional.toFixed(2)} USDT requires margin ${margin.toFixed(2)} at ${raisedLeverage}x, exceeding the ${toNumber(allocation).toFixed(2)} USDT position cap`,
    };
  }
  return { size: qty, leverage: raisedLeverage };
}

function liquidationPrice(
  side: GridPaperPositionSide,
  entryPrice: Decimal,
  leverage: number,
  mmRate = 0,
): Decimal {
  const l = Math.max(1, leverage);
  if (l <= 1) return money(0);
  // Adverse move to liquidation = initial leverage distance minus the
  // maintenance-margin buffer (floored at 1% so a huge mmRate can never
  // produce a non-liquidating or inverted price).
  const move = Math.max(0.01, 1 / l - mmRate);
  return side === "long"
    ? entryPrice.times(1 - move)
    : entryPrice.times(1 + move);
}

function strategyManifestFor(
  options: GridPaperTradingOptions,
  executionEnvironment:
    | "bitget-demo"
    | "bitget-live"
    | "bybit-demo"
    | "bybit-live",
): StrategyManifest {
  return {
    ...DEFAULT_STRATEGY_MANIFEST,
    exchange: executionEnvironment,
    symbol: options.symbol,
    timeframe: options.timeframe,
    gridStepPct: options.gridStepPct.toString(),
    gridMaxGrids: options.gridMaxGrids.toString(),
    gridPauseAfterLossBars: options.gridPauseAfterLossBars.toString(),
    positionFraction: (options.maxPositionPct / 100).toString(),
    feePct: options.feePct.toString(),
    slippageBps: options.slippageBps.toString(),
    trendFilterPeriod: options.trendFilterPeriod.toString(),
    adxGate: (options.chopGateAdxThreshold ?? 0).toString(),
    targetRatio: (options.targetRatio ?? 1).toString(),
    onlyWithTrend: (options.onlyWithTrend ?? false).toString(),
    leverage: (options.leverage ?? 1).toString(),
    productType: options.productType ?? "USDT-FUTURES",
    // NOTE: capital is intentionally excluded from the persisted fingerprint.
    // The readiness gate compares the persisted strategy_config_fingerprint
    // against the capital-free candidate manifest (strategyManifestFor);
    // including capital here made every fingerprinted fill fail provenance
    // with "candidate fingerprint mismatch". Distinct-capital resume safety
    // is enforced explicitly below via state.initialCapital.
  };
}

function freshGridState(options: GridPaperTradingOptions): GridPaperState {
  return {
    exchange: options.exchange,
    symbol: options.symbol,
    timeframe: options.timeframe,
    initialCapital: options.initialCapital,
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
}

function stateConfigMatchesOptions(
  state: GridPaperState,
  options: GridPaperTradingOptions,
): boolean {
  return (
    (state.initialCapital === undefined ||
      state.initialCapital === options.initialCapital) &&
    state.gridStepPct === options.gridStepPct &&
    state.gridMaxGrids === options.gridMaxGrids &&
    state.gridPauseAfterLossBars === options.gridPauseAfterLossBars &&
    state.feePct === options.feePct &&
    state.slippageBps === options.slippageBps &&
    state.trendFilterPeriod === options.trendFilterPeriod &&
    state.maxPositionPct === options.maxPositionPct &&
    state.maxDrawdownPct === options.maxDrawdownPct &&
    state.leverage === (options.leverage ?? 1)
  );
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

    const executionEnvironment =
      options.executionEnvironment ??
      (options.isLive
        ? options.exchange === "bybit-futures"
          ? "bybit-live"
          : "bitget-live"
        : options.exchange === "bybit-futures"
          ? "bybit-demo"
          : "bitget-demo");
    const strategyFingerprint = fingerprintStrategyManifest(
      strategyManifestFor(options, executionEnvironment),
    );
    let state: GridPaperState =
      (yield* repo.getGridState(
        options.exchange,
        options.symbol,
        options.timeframe,
      )) ?? freshGridState(options);

    // A persisted state may belong to a previous run with different
    // parameters (e.g. the demo soak was reconfigured after a candidate
    // promotion). When the position is flat, the persisted config fields are
    // still used for sizing/risk — silently reusing a stale config trades the
    // wrong capital/position/drawdown limits. Re-seed the state from the
    // current options instead of resuming with the stale parameters.
    if (state.side === null && !stateConfigMatchesOptions(state, options)) {
      state = freshGridState(options);
      yield* repo.saveGridState(state);
    }

    if (
      state.side !== null &&
      (state.strategyConfigFingerprint !== strategyFingerprint ||
        (state.initialCapital !== undefined &&
          state.initialCapital !== options.initialCapital))
    ) {
      // Size-only change (allocatedWeight/maxPosition) while position open:
      // heal fingerprint and sizing fields instead of killing the position.
      // The open position stays live with new sizing for next exits/entries.
      state = {
        ...state,
        strategyConfigFingerprint: strategyFingerprint,
        maxPositionPct: options.maxPositionPct,
        maxDrawdownPct: options.maxDrawdownPct,
        maxDailyLossPct: options.maxDailyLossPct,
        gridStepPct: options.gridStepPct,
        gridMaxGrids: options.gridMaxGrids,
        gridPauseAfterLossBars: options.gridPauseAfterLossBars,
        updatedAt: new Date(),
      };
      yield* repo.saveGridState(state);
      // fall through to normal trading (exits still work)
    }

    if (state.killed) {
      // A stale kill flag on a FLAT state (no open position) must not hold
      // the soak forever after a transient mismatch (e.g. a phantom
      // exchange position that has since closed). The account-level kill
      // switch still gates regardless. Regression 2026-08-09: the SOL
      // candidate was stranded by a sticky flag with a clean state.
      if (state.side === null && !(yield* killSwitch.isEngaged())) {
        state = { ...state, killed: false, updatedAt: new Date() };
        yield* repo.saveGridState(state);
      } else {
        return {
          action: "hold" as const,
          side: state.side,
          capital: toNumber(state.capital),
          peakCapital: toNumber(state.peakCapital),
          note: "kill switch active",
        };
      }
    }

    const replayBars = options.replayBars ?? 0;
    // The live chop gate computes causal ADX(14) from the fetched candles;
    // with trendFilterPeriod 0 the engine used to fetch just 2 candles ->
    // ADX was always 0 and the validated ADX gate never fired live. Fetch
    // an ADX warmup (14*2+2 bars) whenever the chop gate is enabled.
    const adxWarmup = (options.chopGateAdxThreshold ?? 0) > 0 ? 14 * 2 + 2 : 0;
    const requiredCandles =
      replayBars > 0
        ? replayBars + options.trendFilterPeriod + 5
        : Math.max(options.trendFilterPeriod + 1, 2, adxWarmup);
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
    // trendFilterPeriod 0 disables the trend filter (CLI sets 0 when
    // onlyWithTrend is off): skip the sma call entirely — a 0-period sma
    // would be NaN, defeating the null guard and propagating NaN into grid
    // decisions. Only an enabled-but-not-ready filter holds.
    const trendFilterEnabled = options.trendFilterPeriod >= 1;
    const trend = trendFilterEnabled
      ? sma(candles, i, options.trendFilterPeriod)
      : null;
    if (trendFilterEnabled && trend === null) {
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

    let note = "no action";

    // Persist grid state with up to 3 attempts (200ms/400ms backoff). On
    // final failure, engage the account kill switch fail-closed: a live
    // position that is not recorded locally must halt trading, never
    // silently continue. Returns true when the state was persisted.
    const saveStateWithRetry = (
      s: GridPaperState,
      failReason: string,
    ): Effect.Effect<boolean, KillSwitchError, never> =>
      Effect.gen(function* () {
        for (let attempt = 0; attempt < 3; attempt++) {
          const saved = yield* repo.saveGridState(s).pipe(Effect.result);
          if (saved._tag === "Success") return true;
          if (attempt < 2) yield* Effect.sleep(200 * (attempt + 1));
        }
        yield* killSwitch.engage(failReason);
        return false;
      });

    // Best-effort cancellation of any resting open orders for the symbol.
    // Adapters without cancellation support omit cancelOpenOrders; the kill
    // switch remains the fail-closed gate regardless.
    const cancelRestingOrders = (
      symbol: string,
      productType: FuturesProductType,
    ): Effect.Effect<void, never, never> =>
      adapter.cancelOpenOrders === undefined
        ? Effect.void
        : adapter.cancelOpenOrders(symbol, productType).pipe(
            Effect.match({
              onSuccess: () => undefined,
              onFailure: () => undefined,
            }),
          );

    if (isLive) {
      const reconciliation = reconcileLivePosition(
        state,
        yield* adapter.getPosition(options.symbol, productType),
        {
          productType,
          marginMode,
          leverage: state.leverage,
          entryPrice: state.entryPrice,
        },
      );
      if (reconciliation.kind === "adopt") {
        // The exchange holds a position the local state never recorded
        // (e.g. the fill confirmation was lost between the adapter and the
        // state save). Adopt it into local state and CONTINUE managing it —
        // exit rules apply below — instead of holding the kill switch
        // forever on a position we can see and control.
        state = adoptedGridState(
          state,
          reconciliation.position,
          strategyFingerprint,
          executionEnvironment,
          current.timestamp,
        );
        const persisted = yield* saveStateWithRetry(
          state,
          "state save failed after position adoption",
        );
        if (!persisted) {
          return {
            action: "hold" as const,
            side: state.side,
            capital: toNumber(state.capital),
            peakCapital: toNumber(state.peakCapital),
            note: "KILL SWITCH ENGAGED: state save failed after position adoption",
          };
        }
        note = `[LIVE] adopted ${reconciliation.position.side} ${reconciliation.position.quantity.toString()} @ ${reconciliation.position.entryPrice.toString()} (untracked exchange position)`;
      } else if (reconciliation.kind === "unadoptable") {
        // The exchange position cannot be represented locally (invalid
        // quantity/price). Close it out instead of adopting garbage.
        const close = yield* adapter
          .closePosition({
            symbol: options.symbol,
            side: reconciliation.position.side === "long" ? "sell" : "buy",
            productType,
            marginMode,
            leverage: state.leverage,
            size: reconciliation.position.quantity,
          })
          .pipe(Effect.result);
        if (close._tag === "Success" && close.success !== null) {
          note = `[LIVE] closed untracked invalid exchange position (${reconciliation.reason})`;
        } else {
          const reason = `LIVE POSITION MISMATCH: ${reconciliation.reason}`;
          yield* killSwitch.engage(reason);
          yield* cancelRestingOrders(options.symbol, productType);
          state = {
            ...state,
            killed: true,
            lastTimestamp: current.timestamp,
            updatedAt: new Date(),
          };
          yield* repo.saveGridState(state);
          return {
            action: "hold" as const,
            side: state.side,
            capital: toNumber(state.capital),
            peakCapital: toNumber(state.peakCapital),
            note: reason,
          };
        }
      } else if (reconciliation.kind === "mismatch") {
        const reason = `LIVE POSITION MISMATCH: ${reconciliation.reason}`;
        yield* killSwitch.engage(reason);
        // Cancel any resting open orders for the symbol so pending orders
        // cannot pile up while the kill switch holds (best-effort).
        yield* cancelRestingOrders(options.symbol, productType);
        state = {
          ...state,
          killed: true,
          lastTimestamp: current.timestamp,
          updatedAt: new Date(),
        };
        yield* repo.saveGridState(state);
        return {
          action: "hold" as const,
          side: state.side,
          capital: toNumber(state.capital),
          peakCapital: toNumber(state.peakCapital),
          note: reason,
        };
      }

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
    // Round-trip fee: maker both legs for target exits; the exit leg of a
    // stop/liquidation is a TAKER market order, so charge maker+taker there.
    const makerFee = options.feePct / 100;
    const takerFee = (options.takerExitFeePct ?? options.feePct) / 100;
    const fee = makerFee * 2;
    const stopFee = makerFee + takerFee;

    // Funding accrued while the position was open (whole 8h intervals,
    // charged at close; longs pay a positive rate, shorts receive it).
    const fundingCost = (() => {
      const ratePct = options.fundingRatePct8h ?? 0;
      if (
        ratePct === 0 ||
        state.side === null ||
        state.entryOpenedAt === undefined
      ) {
        return money(0);
      }
      const intervals = Math.floor(
        (current.timestamp.getTime() - state.entryOpenedAt.getTime()) /
          FUNDING_INTERVAL_MS,
      );
      if (intervals <= 0) return money(0);
      const notional = state.capital
        .times(state.maxPositionPct / 100)
        .times(state.leverage);
      return notional
        .times((ratePct / 100) * intervals)
        .times(state.side === "long" ? -1 : 1);
    })();

    const todayPnl = yield* repo.getTodayRealizedPnl();
    const startOfDayCapital = yield* repo.getStartOfDayCapital(
      new Date(),
      state.capital,
    );

    /** Result of closing a grid position leg. */
    interface GridCloseTradeResult {
      readonly trade: GridPaperTrade;
      readonly capitalAfter: Decimal;
      readonly peakCapital: Decimal;
    }

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
    ): GridCloseTradeResult => {
      const pricePnl =
        side === "long"
          ? exitPrice.minus(entryPrice).div(entryPrice)
          : entryPrice.minus(exitPrice).div(entryPrice);
      const roundTripFee = exitReason === "target" ? fee : stopFee;
      const net = pricePnl.minus(roundTripFee);
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
            ).plus(fundingCost);
      const pnlPct =
        exitReason === "liquidation" ? money(-100) : leveragedReturn.times(100);
      const realizedPnlPct =
        entryEvidence !== null && exitFill !== null
          ? (side === "long"
              ? exitPrice.minus(entryPrice)
              : entryPrice.minus(exitPrice)
            )
              .times(Decimal.min(entryEvidence.filledQty, exitFill.filledQty))
              .minus(entryEvidence.fee.plus(exitFill.fee))
              .div(stateCapital)
              .times(100)
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
        strategyConfigFingerprint: state.strategyConfigFingerprint,
        cohortId: state.cohortId,
        candidateLockAt: state.candidateLockAt,
        datasetCutoffAt: state.datasetCutoffAt,
        entryOpenedAt: state.entryOpenedAt,
        executionEnvironment: state.executionEnvironment,
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
        if (isLive) {
          // Close sizing keeps the legacy allocation/price size: no contract
          // specs are passed, so closing the position never applies the entry
          // orderability floor/step (the position already exists on the book).
          const size = orderSizeContracts(
            s.capital,
            s.maxPositionPct,
            s.entryPrice,
          ).size;
          const closeSize =
            (s.entryFillSource === "live" || s.entryFillSource === "adopted") &&
            s.entryFilledQty?.greaterThan(0) === true
              ? s.entryFilledQty
              : size;
          if (size.greaterThan(0)) {
            const fill = yield* adapter.closePosition({
              symbol: options.symbol,
              side: side === "long" ? "sell" : "buy",
              productType,
              marginMode,
              leverage: s.leverage,
              size: closeSize,
              price: theoreticalExitPrice,
            });
            if (!fill) {
              return yield* Effect.fail(
                new ExchangeError(`live ${exitReason} close returned no fill`),
              );
            }
            if (fill.filledQty.lessThan(closeSize)) {
              return yield* Effect.fail(
                new ExchangeError(
                  `live ${exitReason} close was partially filled (${fill.filledQty.toString()}/${closeSize.toString()})`,
                ),
              );
            }
            exitPrice = money(fill.filledPrice);
            exitFill = fill;
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
          (s.entryFillSource === "live" || s.entryFillSource === "adopted") &&
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
        yield* circuitBreaker.recordTradeResult(
          toNumber(close.capitalAfter.minus(s.capital)),
          toNumber(startOfDayCapital),
        );
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
      // A disabled trend filter (trendFilterPeriod 0 -> trend null) imposes
      // no direction constraint, mirroring onlyWithTrend === false.
      const allowLong =
        !onlyWithTrend || trend === null || current.close > trend;
      const allowShort =
        !onlyWithTrend || trend === null || current.close < trend;

      let entrySide: GridPaperPositionSide | null = null;
      let theoreticalEntryPrice = money(0);
      // Maker parity with the validated backtest model: entries are LIMIT
      // orders resting at the raw grid level (the bar has already touched the
      // level, so the order fills immediately at that price). The slippage
      // factor only feeds conservative sizing.
      let entryLevelPrice = money(0);
      if (allowLong && money(current.low).lessThanOrEqualTo(buyLevel)) {
        entrySide = "long";
        entryLevelPrice = buyLevel;
        theoreticalEntryPrice = buyLevel.times(slippageFactor);
      } else if (
        allowShort &&
        money(current.high).greaterThanOrEqualTo(sellLevel)
      ) {
        entrySide = "short";
        entryLevelPrice = sellLevel;
        theoreticalEntryPrice = sellLevel.div(slippageFactor);
      }

      if (entrySide !== null) {
        if (isLive) {
          const tradesTodayCount = yield* repo.countTradesForDate(new Date());
          const riskGuard = yield* RiskGuard;
          const riskCheck = yield* riskGuard
            .check({
              isLive: true,
              capital: toNumber(state.capital),
              peakCapital: toNumber(state.peakCapital),
              startOfDayCapital: toNumber(startOfDayCapital),
              dailyRealizedPnl: toNumber(todayPnl),
              tradesTodayCount,
              positionValue: toNumber(
                state.capital.times(state.maxPositionPct / 100),
              ),
              symbol: options.symbol,
              side: entrySide === "long" ? "buy" : "sell",
              leverage: state.leverage,
              productType,
              // Fail closed locally for the minimum orderable position: the
              // guard blocks (RISK BLOCKED) instead of the exchange rejecting
              // an unorderable qty, when the floor cannot fit the cap even at
              // the configured leverage.
              minOrderableNotional:
                options.contractSpecs === undefined
                  ? undefined
                  : toNumber(
                      Decimal.max(
                        money(options.contractSpecs.minTradeUSDT),
                        money(options.contractSpecs.minQty).times(
                          theoreticalEntryPrice,
                        ),
                      ),
                    ),
            })
            .pipe(Effect.result);
          if (riskCheck._tag === "Failure") {
            note = `RISK BLOCKED ${entrySide}: ${riskCheck.failure.violations.join("; ")}`;
          } else {
            const sized = orderSizeContracts(
              state.capital,
              state.maxPositionPct,
              theoreticalEntryPrice,
              {
                leverage: state.leverage,
                contractSpecs: options.contractSpecs,
              },
            );
            const size = sized.size;
            const orderLeverage = sized.leverage;
            if (sized.skipReason !== undefined) {
              // Never attempt an unorderable or over-cap order: the minimum
              // orderable position cannot fit the allocation cap, so skip
              // instead of sending an order the exchange would reject.
              note = `RISK BLOCKED ${entrySide} (orderability): ${sized.skipReason}`;
            } else if (size.lessThanOrEqualTo(0)) {
              note = `RISK BLOCKED ${entrySide}: computed size zero`;
            } else {
              yield* adapter.setLeverage(
                options.symbol,
                productType,
                marginMode,
                orderLeverage,
              );
              yield* adapter.setMarginMode(
                options.symbol,
                productType,
                marginMode,
              );
              // Grids trade both directions on one symbol: force one-way
              // position mode so Bitget doesn't reject orders with 40774
              // (order type must match the account's position type).
              yield* adapter.setPositionMode(productType, "one_way");
              const placed = yield* adapter
                .placeOrder({
                  symbol: options.symbol,
                  side: entrySide === "long" ? "buy" : "sell",
                  type: "limit",
                  size,
                  productType,
                  marginMode,
                  leverage: orderLeverage,
                  price: entryLevelPrice,
                })
                .pipe(Effect.result);
              if (placed._tag === "Failure") {
                // The adapter may have acked the order and then lost the
                // fill confirmation (e.g. its fill poll failed after the
                // order was accepted). Check the exchange directly and
                // adopt the position if one now exists, so the order cannot
                // orphan on the book; otherwise surface the original error.
                const currentPosition = yield* adapter
                  .getPosition(options.symbol, productType)
                  .pipe(Effect.result);
                if (
                  currentPosition._tag === "Success" &&
                  currentPosition.success !== null
                ) {
                  state = adoptedGridState(
                    state,
                    currentPosition.success,
                    strategyFingerprint,
                    executionEnvironment,
                    current.timestamp,
                  );
                  const persisted = yield* saveStateWithRetry(
                    state,
                    "state save failed after order placement",
                  );
                  note = `[LIVE] placed ${entrySide} ${size.toFixed(6)} then adopted ${currentPosition.success.side} ${currentPosition.success.quantity.toString()} @ ${currentPosition.success.entryPrice.toString()} (fill confirmation lost)`;
                  if (!persisted) {
                    return {
                      action: "hold" as const,
                      side: state.side,
                      capital: toNumber(state.capital),
                      peakCapital: toNumber(state.peakCapital),
                      note: "KILL SWITCH ENGAGED: state save failed after order placement",
                    };
                  }
                } else {
                  return yield* Effect.fail(
                    new ExchangeError(
                      `live ${entrySide} entry failed: ${placed.failure.reason}`,
                      placed.failure,
                    ),
                  );
                }
              } else {
                const fill = placed.success;
                state = {
                  ...state,
                  side: entrySide,
                  entryPrice: money(fill.filledPrice),
                  entryOrderId: fill.orderId,
                  entryClientOid: fill.clientOid,
                  entryFilledQty: fill.filledQty,
                  entryFee: fill.fee,
                  entryFillSource: "live",
                  leverage: orderLeverage,
                  strategyConfigFingerprint: strategyFingerprint,
                  cohortId: `grid-${strategyFingerprint.slice(0, 16)}`,
                  candidateLockAt: current.timestamp,
                  datasetCutoffAt: current.timestamp,
                  entryOpenedAt: new Date(),
                  executionEnvironment,
                  updatedAt: new Date(),
                  lastTimestamp: current.timestamp,
                };
                // Persist the opened position IMMEDIATELY after the ack —
                // never defer to the end-of-iteration save. A crash or a
                // contended DB between the ack and that save orphans the
                // live position (regression 2026-08-10: ADA short 126 filled
                // on the Bitget demo with no grid_paper_state row).
                const persisted = yield* saveStateWithRetry(
                  state,
                  "state save failed after order placement",
                );
                note = `[LIVE] opened ${entrySide} @ ${state.entryPrice.toFixed(2)} size=${size.toFixed(6)} (leverage=${orderLeverage}x)`;
                if (!persisted) {
                  return {
                    action: "hold" as const,
                    side: state.side,
                    capital: toNumber(state.capital),
                    peakCapital: toNumber(state.peakCapital),
                    note: "KILL SWITCH ENGAGED: state save failed after order placement",
                  };
                }
              }
            }
          }
        } else {
          const sized = orderSizeContracts(
            state.capital,
            state.maxPositionPct,
            theoreticalEntryPrice,
            {
              leverage: state.leverage,
              contractSpecs: options.contractSpecs,
            },
          );
          if (sized.skipReason !== undefined) {
            note = `RISK BLOCKED ${entrySide} (orderability): ${sized.skipReason}`;
          } else {
            state = {
              ...state,
              side: entrySide,
              entryPrice: theoreticalEntryPrice,
              entryFilledQty: sized.size,
              entryFee: money(0),
              entryFillSource: "simulated",
              leverage: sized.leverage,
              strategyConfigFingerprint: strategyFingerprint,
              cohortId: `grid-${strategyFingerprint.slice(0, 16)}`,
              candidateLockAt: current.timestamp,
              datasetCutoffAt: current.timestamp,
              entryOpenedAt: current.timestamp,
              executionEnvironment,
              updatedAt: new Date(),
              lastTimestamp: current.timestamp,
            };
            note = `opened ${entrySide} @ ${state.entryPrice.toFixed(2)} size=${sized.size.toFixed(6)} (leverage=${sized.leverage}x)`;
          }
        }
      }
    } else if (state.side === "long") {
      const target = state.entryPrice.plus(step.times(targetRatio));
      const stop = state.entryPrice.minus(step.times(options.gridMaxGrids));
      const liq = liquidationPrice(
        "long",
        state.entryPrice,
        state.leverage,
        options.maintenanceMarginRate ?? 0,
      );

      if (liq.greaterThan(0) && money(current.low).lessThanOrEqualTo(liq)) {
        // Market exits fill ADVERSE to the trigger: a long stop sells below
        // the level by the slippage amount.
        const close = yield* closeGridPosition(
          "long",
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
        note = `liquidated long @ ${close.trade.exitPrice.toFixed(2)} pnl=-100.000% (leverage=${state.leverage}x)`;
      } else if (money(current.high).greaterThanOrEqualTo(target)) {
        const close = yield* closeGridPosition("long", "target", target);
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
        note = `${isLive ? "[LIVE] " : ""}closed long stop @ ${close.trade.exitPrice.toFixed(2)} pnl=${close.trade.pnlPct.toFixed(3)}%`;
      }
    } else if (state.side === "short") {
      const target = state.entryPrice.minus(step.times(targetRatio));
      const stop = state.entryPrice.plus(step.times(options.gridMaxGrids));
      const liq = liquidationPrice(
        "short",
        state.entryPrice,
        state.leverage,
        options.maintenanceMarginRate ?? 0,
      );

      if (liq.greaterThan(0) && money(current.high).greaterThanOrEqualTo(liq)) {
        // Market exits fill ADVERSE to the trigger: a short stop buys above
        // the level by the slippage amount.
        const close = yield* closeGridPosition(
          "short",
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
        note = `liquidated short @ ${close.trade.exitPrice.toFixed(2)} pnl=-100.000% (leverage=${state.leverage}x)`;
      } else if (money(current.low).lessThanOrEqualTo(target)) {
        const close = yield* closeGridPosition("short", "target", target);
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
