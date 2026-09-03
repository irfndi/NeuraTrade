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
   *  this symbol, populated by the CLI from the selected venue on the live
   *  path and by tests directly. When set, entry sizing rounds the order
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

function gridProvenanceMismatch(
  state: GridPaperState,
  options: GridPaperTradingOptions,
): string | null {
  if (!options.isLive || state.side === null) return null;
  if (state.entryFillSource !== "live" && state.entryFillSource !== "adopted") {
    return "READINESS PROVENANCE MISMATCH: open state has no live entry source";
  }
  if (state.strategyConfigFingerprint === undefined) {
    return "READINESS PROVENANCE MISMATCH: open state has no strategy fingerprint";
  }
  if (
    state.initialCapital !== undefined &&
    state.initialCapital !== options.initialCapital
  ) {
    return `READINESS PROVENANCE MISMATCH: open state capital ${state.initialCapital} differs from configured ${options.initialCapital}`;
  }
  return null;
}

type GridFillEvidence = Pick<
  FuturesOrderFill,
  "orderId" | "clientOid" | "filledQty" | "fee"
>;

type GridExecutionEnvironment = NonNullable<
  GridPaperTradingOptions["executionEnvironment"]
>;

type GridPaperIterationError =
  | MarketDataError
  | PaperTradingRepositoryError
  | ExchangeError
  | RiskError
  | KillSwitchError
  | CircuitBreakerError;

type GridServiceContext =
  | MarketDataGatewayService
  | PaperTradingRepositoryService
  | FuturesExchangeAdapterService
  | RiskGuardService
  | KillSwitchService
  | CircuitBreakerService;

interface GridIterationServices {
  readonly repo: PaperTradingRepositoryService;
  readonly gateway: MarketDataGatewayService;
  readonly adapter: FuturesExchangeAdapterService;
  readonly riskGuard: RiskGuardService;
  readonly killSwitch: KillSwitchService;
  readonly circuitBreaker: CircuitBreakerService;
}

interface GridMarketData {
  readonly candles: readonly Candle[];
  readonly index: number;
  readonly current: Candle;
  readonly trend: number | null;
}

interface GridMarketResolution {
  readonly market?: GridMarketData;
  readonly earlyResult?: GridPaperTradingIterationResult;
}

interface GridStateResolution {
  readonly state: GridPaperState;
  readonly earlyResult?: GridPaperTradingIterationResult;
  readonly reseedNote?: string;
}

interface GridBarInput {
  readonly options: GridPaperTradingOptions;
  readonly state: GridPaperState;
  readonly strategyFingerprint: string;
  readonly executionEnvironment: GridExecutionEnvironment;
  readonly services: GridIterationServices;
  readonly market: GridMarketData;
  readonly note?: string;
}

interface GridStateOperationResult {
  readonly state: GridPaperState;
  readonly note?: string;
  readonly earlyResult?: GridPaperTradingIterationResult;
}

interface GridLiveStateInput {
  readonly state: GridPaperState;
  readonly options: GridPaperTradingOptions;
  readonly current: Candle;
  readonly strategyFingerprint: string;
  readonly executionEnvironment: GridExecutionEnvironment;
  readonly services: GridIterationServices;
}

function gridExecutionEnvironment(
  options: GridPaperTradingOptions,
): GridExecutionEnvironment {
  return (
    options.executionEnvironment ??
    (options.isLive
      ? options.exchange === "bybit-futures"
        ? "bybit-live"
        : "bitget-live"
      : options.exchange === "bybit-futures"
        ? "bybit-demo"
        : "bitget-demo")
  );
}

function gridHoldResult(
  state: GridPaperState,
  note: string,
): GridPaperTradingIterationResult {
  return {
    action: "hold",
    side: state.side,
    capital: toNumber(state.capital),
    peakCapital: toNumber(state.peakCapital),
    note,
  };
}

function loadGridServices(): Effect.Effect<
  GridIterationServices,
  never,
  GridServiceContext
> {
  return Effect.gen(function* () {
    return {
      repo: yield* PaperTradingRepository,
      gateway: yield* MarketDataGateway,
      adapter: yield* FuturesExchangeAdapter,
      riskGuard: yield* RiskGuard,
      killSwitch: yield* KillSwitch,
      circuitBreaker: yield* CircuitBreaker,
    };
  });
}

function loadGridState(
  repo: PaperTradingRepositoryService,
  options: GridPaperTradingOptions,
  strategyFingerprint: string,
): Effect.Effect<GridStateResolution, PaperTradingRepositoryError> {
  return Effect.gen(function* () {
    let state =
      (yield* repo.getGridState(
        options.exchange,
        options.symbol,
        options.timeframe,
      )) ?? freshGridState(options);

    const provenanceReason = gridProvenanceMismatch(state, options);
    if (provenanceReason !== null) {
      return {
        state,
        earlyResult: gridHoldResult(state, provenanceReason),
      };
    }

    // Flat config drift: heal the config fields but PRESERVE realized
    // capital/peak. A full reseed silently vaporized a realized +0.55% BTC
    // win (50.27 -> 50.00, 2026-09-03): limits come from options, but the
    // balance is the account's and must survive config drift. Only a changed
    // initialCapital (a genuinely different account) reseeds the ledger.
    let flatReseedNote: string | null = null;
    if (state.side === null && !stateConfigMatchesOptions(state, options)) {
      const keepLedger =
        state.initialCapital === undefined ||
        state.initialCapital === options.initialCapital;
      const prevCapital = state.capital;
      const fresh = freshGridState(options);
      state =
        keepLedger === false
          ? fresh
          : {
              ...fresh,
              capital: prevCapital,
              peakCapital: state.peakCapital,
            };
      yield* repo.saveGridState(state);
      flatReseedNote =
        keepLedger === false
          ? "flat reseed on initialCapital change"
          : `flat config drift healed (capital ${toNumber(prevCapital).toFixed(2)} preserved)`;
    }

    if (
      state.side !== null &&
      (state.strategyConfigFingerprint !== strategyFingerprint ||
        (state.initialCapital !== undefined &&
          state.initialCapital !== options.initialCapital))
    ) {
      state = {
        ...state,
        strategyConfigFingerprint: strategyFingerprint,
        maxPositionPct: options.maxPositionPct,
        maxDrawdownPct: options.maxDrawdownPct,
        gridStepPct: options.gridStepPct,
        gridMaxGrids: options.gridMaxGrids,
        gridPauseAfterLossBars: options.gridPauseAfterLossBars,
        updatedAt: new Date(),
      };
      yield* repo.saveGridState(state);
    }

    return { state, reseedNote: flatReseedNote ?? undefined };
  });
}

function guardKilledGridState(
  state: GridPaperState,
  repo: PaperTradingRepositoryService,
  killSwitch: KillSwitchService,
): Effect.Effect<
  GridStateResolution,
  PaperTradingRepositoryError | KillSwitchError
> {
  return Effect.gen(function* () {
    if (!state.killed) return { state };
    if (state.side === null && !(yield* killSwitch.isEngaged())) {
      const reset = { ...state, killed: false, updatedAt: new Date() };
      yield* repo.saveGridState(reset);
      return { state: reset };
    }
    return { state, earlyResult: gridHoldResult(state, "kill switch active") };
  });
}

function requiredGridCandles(options: GridPaperTradingOptions): number {
  const replayBars = options.replayBars ?? 0;
  const adxWarmup = (options.chopGateAdxThreshold ?? 0) > 0 ? 14 * 2 + 2 : 0;
  return replayBars > 0
    ? replayBars + options.trendFilterPeriod + 5
    : Math.max(options.trendFilterPeriod + 1, 2, adxWarmup);
}

function resolveGridMarket(
  state: GridPaperState,
  options: GridPaperTradingOptions,
  candles: readonly Candle[],
): GridMarketResolution {
  const replayBars = options.replayBars ?? 0;
  const minCandles =
    replayBars > 0
      ? Math.min(replayBars, candles.length)
      : Math.max(options.trendFilterPeriod + 1, 2);
  if (candles.length < minCandles) {
    return {
      earlyResult: gridHoldResult(
        state,
        `insufficient candles (${candles.length}/${minCandles})`,
      ),
    };
  }

  const index = resolveGridCandleIndex(state, options, candles);
  if (index === null) {
    return {
      earlyResult: gridHoldResult(state, "no new replay candle"),
    };
  }
  const trend =
    options.trendFilterPeriod >= 1
      ? sma(candles, index, options.trendFilterPeriod)
      : null;
  if (options.trendFilterPeriod >= 1 && trend === null) {
    return {
      earlyResult: gridHoldResult(state, "trend filter not ready"),
    };
  }
  return {
    market: { candles, index, current: candles[index], trend },
  };
}

function resolveGridCandleIndex(
  state: GridPaperState,
  options: GridPaperTradingOptions,
  candles: readonly Candle[],
): number | null {
  const replayBars = options.replayBars ?? 0;
  if (replayBars === 0) return candles.length - 1;
  if (state.lastTimestamp === null) {
    return Math.max(options.trendFilterPeriod, candles.length - replayBars);
  }
  const nextIndex = candles.findIndex(
    (candle) => candle.timestamp.getTime() > state.lastTimestamp!.getTime(),
  );
  return nextIndex === -1 ? null : nextIndex;
}

function saveGridStateWithRetry(
  repo: PaperTradingRepositoryService,
  killSwitch: KillSwitchService,
  state: GridPaperState,
  failReason: string,
): Effect.Effect<boolean, KillSwitchError> {
  return Effect.gen(function* () {
    for (let attempt = 0; attempt < 3; attempt++) {
      const saved = yield* repo.saveGridState(state).pipe(Effect.result);
      if (saved._tag === "Success") return true;
      if (attempt < 2) yield* Effect.sleep(200 * (attempt + 1));
    }
    yield* killSwitch.engage(failReason);
    return false;
  });
}

function cancelGridRestingOrders(
  adapter: FuturesExchangeAdapterService,
  symbol: string,
  productType: FuturesProductType,
): Effect.Effect<void> {
  if (adapter.cancelOpenOrders === undefined) return Effect.void;
  return adapter.cancelOpenOrders(symbol, productType).pipe(
    Effect.match({
      onSuccess: () => undefined,
      onFailure: () => undefined,
    }),
  );
}

function reconcileGridLiveState(
  input: GridLiveStateInput,
): Effect.Effect<GridStateOperationResult, GridPaperIterationError> {
  return Effect.gen(function* () {
    let state = input.state;
    const {
      options,
      current,
      strategyFingerprint,
      executionEnvironment,
      services,
    } = input;
    const { adapter, repo, killSwitch, circuitBreaker } = services;
    const productType = options.productType ?? "USDT-FUTURES";
    const marginMode = options.marginMode ?? "isolated";
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
    let note: string | undefined;

    if (reconciliation.kind === "adopt") {
      state = adoptedGridState(
        state,
        reconciliation.position,
        strategyFingerprint,
        executionEnvironment,
        current.timestamp,
      );
      const persisted = yield* saveGridStateWithRetry(
        repo,
        killSwitch,
        state,
        "state save failed after position adoption",
      );
      if (!persisted) {
        return {
          state,
          earlyResult: gridHoldResult(
            state,
            "KILL SWITCH ENGAGED: state save failed after position adoption",
          ),
        };
      }
      note = `[LIVE] adopted ${reconciliation.position.side} ${reconciliation.position.quantity.toString()} @ ${reconciliation.position.entryPrice.toString()} (untracked exchange position)`;
    } else if (reconciliation.kind === "unadoptable") {
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
        return yield* failGridLiveReconciliation(
          state,
          options,
          current,
          reconciliation.reason,
          adapter,
          repo,
          killSwitch,
        );
      }
    } else if (reconciliation.kind === "mismatch") {
      return yield* failGridLiveReconciliation(
        state,
        options,
        current,
        reconciliation.reason,
        adapter,
        repo,
        killSwitch,
      );
    }

    if (yield* killSwitch.isEngaged()) {
      return {
        state,
        earlyResult: gridHoldResult(
          state,
          `KILL SWITCH ENGAGED: ${yield* killSwitch.getReason()}`,
        ),
      };
    }
    if (yield* circuitBreaker.isOpen()) {
      return {
        state,
        earlyResult: gridHoldResult(
          state,
          `CIRCUIT BREAKER OPEN: ${yield* circuitBreaker.getReason()}`,
        ),
      };
    }
    return { state, note };
  });
}

function failGridLiveReconciliation(
  state: GridPaperState,
  options: GridPaperTradingOptions,
  current: Candle,
  reconciliationReason: string,
  adapter: FuturesExchangeAdapterService,
  repo: PaperTradingRepositoryService,
  killSwitch: KillSwitchService,
): Effect.Effect<GridStateOperationResult, GridPaperIterationError> {
  return Effect.gen(function* () {
    const reason = `LIVE POSITION MISMATCH: ${reconciliationReason}`;
    yield* killSwitch.engage(reason);
    yield* cancelGridRestingOrders(
      adapter,
      options.symbol,
      options.productType ?? "USDT-FUTURES",
    );
    const killedState = {
      ...state,
      killed: true,
      lastTimestamp: current.timestamp,
      updatedAt: new Date(),
    };
    yield* repo.saveGridState(killedState);
    return {
      state: killedState,
      earlyResult: gridHoldResult(killedState, reason),
    };
  });
}

export interface GridCloseTradeResult {
  readonly trade: GridPaperTrade;
  readonly capitalAfter: Decimal;
  readonly peakCapital: Decimal;
}

export interface GridCloseTradeInput {
  readonly options: GridPaperTradingOptions;
  readonly state: GridPaperState;
  readonly side: GridPaperPositionSide;
  readonly entryPrice: Decimal;
  readonly exitPrice: Decimal;
  readonly exitReason: GridPaperTrade["exitReason"];
  readonly stateCapital: Decimal;
  readonly peakCapital: Decimal;
  readonly maxPositionPct: number;
  readonly leverage: number;
  readonly openedAt: Date;
  readonly entryEvidence: GridFillEvidence | null;
  readonly exitFill: FuturesOrderFill | null;
  readonly fee: number;
  readonly stopFee: number;
  readonly fundingCost: Decimal;
}

function gridEntryEvidence(state: GridPaperState): GridFillEvidence | null {
  const liveEntry =
    state.entryFillSource === "live" || state.entryFillSource === "adopted";
  if (
    !liveEntry ||
    !state.entryOrderId ||
    !state.entryFilledQty ||
    !state.entryFee
  ) {
    return null;
  }
  return {
    orderId: state.entryOrderId,
    clientOid: state.entryClientOid,
    filledQty: state.entryFilledQty,
    fee: state.entryFee,
  };
}

function gridCloseSize(state: GridPaperState, fallback: Decimal): Decimal {
  const liveEntry =
    state.entryFillSource === "live" || state.entryFillSource === "adopted";
  return liveEntry && state.entryFilledQty?.greaterThan(0) === true
    ? state.entryFilledQty
    : fallback;
}

interface GridCloseAccounting {
  readonly capitalAfter: Decimal;
  readonly pnlPct: Decimal;
  readonly realizedPnlPct: Decimal | undefined;
}

function gridPriceDelta(
  side: GridPaperPositionSide,
  entryPrice: Decimal,
  exitPrice: Decimal,
): Decimal {
  return side === "long"
    ? exitPrice.minus(entryPrice)
    : entryPrice.minus(exitPrice);
}

function liveGridCloseAccounting(
  input: GridCloseTradeInput,
): GridCloseAccounting | null {
  const { side, entryPrice, exitPrice, stateCapital, maxPositionPct } = input;
  const { entryEvidence, exitFill, fundingCost } = input;
  if (
    entryEvidence === null ||
    exitFill === null ||
    !entryEvidence.filledQty.greaterThan(0) ||
    !exitFill.filledQty.greaterThan(0)
  ) {
    return null;
  }
  const matchedQty = Decimal.min(entryEvidence.filledQty, exitFill.filledQty);
  const fillPnl = gridPriceDelta(side, entryPrice, exitPrice)
    .times(matchedQty)
    .minus(entryEvidence.fee.plus(exitFill.fee));
  const allocationFloor = stateCapital.times(1 - maxPositionPct / 100);
  const capitalAfter = Decimal.max(
    allocationFloor,
    stateCapital.plus(fillPnl).plus(fundingCost),
  );
  const pnlPct = stateCapital.greaterThan(0)
    ? capitalAfter.minus(stateCapital).div(stateCapital).times(100)
    : money(0);
  const realizedPnlPct = stateCapital.greaterThan(0)
    ? fillPnl.div(stateCapital).times(100)
    : money(0);
  return { capitalAfter, pnlPct, realizedPnlPct };
}

function simulatedGridCloseAccounting(
  input: GridCloseTradeInput,
): GridCloseAccounting {
  const {
    side,
    entryPrice,
    exitPrice,
    exitReason,
    stateCapital,
    maxPositionPct,
    leverage,
    fee,
    stopFee,
    fundingCost,
  } = input;
  const pricePnl = gridPriceDelta(side, entryPrice, exitPrice).div(entryPrice);
  const roundTripFee = exitReason === "target" ? fee : stopFee;
  const leveragedReturn =
    exitReason === "liquidation"
      ? money(-1)
      : pricePnl.minus(roundTripFee).times(leverage);
  const allocationFloor = stateCapital.times(1 - maxPositionPct / 100);
  const rawCapitalAfter = stateCapital.times(
    money(1).plus(leveragedReturn.times(maxPositionPct / 100)),
  );
  const capitalAfter =
    exitReason === "liquidation"
      ? allocationFloor
      : Decimal.max(allocationFloor, rawCapitalAfter).plus(fundingCost);
  const pnlPct =
    exitReason === "liquidation" ? money(-100) : leveragedReturn.times(100);
  return { capitalAfter, pnlPct, realizedPnlPct: undefined };
}

export function buildGridCloseTrade(
  input: GridCloseTradeInput,
): GridCloseTradeResult {
  const { options, state, side, entryPrice, exitPrice, openedAt } = input;
  const liveAccounting = liveGridCloseAccounting(input);
  const accounting = liveAccounting ?? simulatedGridCloseAccounting(input);
  const trade: GridPaperTrade = {
    id: makeId(),
    exchange: options.exchange,
    symbol: options.symbol,
    timeframe: options.timeframe,
    side,
    entryPrice,
    exitPrice,
    capitalBefore: input.stateCapital,
    capitalAfter: accounting.capitalAfter,
    pnlPct: accounting.pnlPct,
    exitReason: input.exitReason,
    openedAt,
    closedAt: new Date(),
    fillSource: liveAccounting !== null ? "live" : "simulated",
    entryOrderId: input.entryEvidence?.orderId,
    entryClientOid: input.entryEvidence?.clientOid,
    exitOrderId: input.exitFill?.orderId,
    exitClientOid: input.exitFill?.clientOid,
    entryFilledQty: input.entryEvidence?.filledQty,
    exitFilledQty: input.exitFill?.filledQty,
    entryFee: input.entryEvidence?.fee,
    exitFee: input.exitFill?.fee,
    realizedPnlPct: accounting.realizedPnlPct,
    strategyConfigFingerprint: state.strategyConfigFingerprint,
    cohortId: state.cohortId,
    candidateLockAt: state.candidateLockAt,
    datasetCutoffAt: state.datasetCutoffAt,
    entryOpenedAt: state.entryOpenedAt,
    executionEnvironment: state.executionEnvironment,
  };
  return {
    trade,
    capitalAfter: accounting.capitalAfter,
    peakCapital: Decimal.max(input.peakCapital, accounting.capitalAfter),
  };
}

interface GridClosePositionInput {
  readonly state: GridPaperState;
  readonly options: GridPaperTradingOptions;
  readonly side: GridPaperPositionSide;
  readonly exitReason: GridPaperTrade["exitReason"];
  readonly theoreticalExitPrice: Decimal;
  readonly isLive: boolean;
  readonly productType: FuturesProductType;
  readonly marginMode: FuturesMarginMode;
  readonly adapter: FuturesExchangeAdapterService;
  readonly repo: PaperTradingRepositoryService;
  readonly circuitBreaker: CircuitBreakerService;
  readonly startOfDayCapital: Decimal;
  readonly fee: number;
  readonly stopFee: number;
  readonly fundingCost: Decimal;
}

interface GridEntrySetup {
  readonly side: GridPaperPositionSide;
  readonly entryLevelPrice: Decimal;
  readonly theoreticalEntryPrice: Decimal;
}

function gridFundingCost(
  state: GridPaperState,
  options: GridPaperTradingOptions,
  current: Candle,
): Decimal {
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
  const notional =
    state.entryFilledQty?.greaterThan(0) === true &&
    state.entryPrice.greaterThan(0)
      ? state.entryFilledQty.times(state.entryPrice)
      : state.capital.times(state.maxPositionPct / 100).times(state.leverage);
  return notional
    .times((ratePct / 100) * intervals)
    .times(state.side === "long" ? -1 : 1);
}

function gridChopGateNote(
  candles: readonly Candle[],
  index: number,
  options: GridPaperTradingOptions,
): string | null {
  const threshold = Math.max(0, options.chopGateAdxThreshold ?? 0);
  if (threshold === 0) return null;
  const adx = makeCausalSymbolStats(candles, options.timeframe)(index).adx14;
  return adx >= threshold
    ? `chop gate active (ADX ${adx.toFixed(1)} >= ${threshold})`
    : null;
}

function resolveGridEntry(
  current: Candle,
  trend: number | null,
  mid: Decimal,
  step: Decimal,
  slippageFactor: number,
  options: GridPaperTradingOptions,
): GridEntrySetup | null {
  const buyLevel = mid.minus(step);
  const sellLevel = mid.plus(step);
  const onlyWithTrend = options.onlyWithTrend ?? false;
  const allowLong = !onlyWithTrend || trend === null || current.close > trend;
  const allowShort = !onlyWithTrend || trend === null || current.close < trend;
  if (allowLong && money(current.low).lessThanOrEqualTo(buyLevel)) {
    return {
      side: "long",
      entryLevelPrice: buyLevel,
      theoreticalEntryPrice: buyLevel.times(slippageFactor),
    };
  }
  if (allowShort && money(current.high).greaterThanOrEqualTo(sellLevel)) {
    return {
      side: "short",
      entryLevelPrice: sellLevel,
      theoreticalEntryPrice: sellLevel.div(slippageFactor),
    };
  }
  return null;
}

function closeGridPosition(
  input: GridClosePositionInput,
): Effect.Effect<
  GridCloseTradeResult & { readonly exitPrice: Decimal },
  ExchangeError | PaperTradingRepositoryError | CircuitBreakerError
> {
  return Effect.gen(function* () {
    const {
      state,
      options,
      side,
      exitReason,
      theoreticalExitPrice,
      isLive,
      productType,
      marginMode,
      adapter,
      repo,
      circuitBreaker,
      startOfDayCapital,
      fee,
      stopFee,
      fundingCost,
    } = input;
    let exitPrice = theoreticalExitPrice;
    let exitFill: FuturesOrderFill | null = null;
    if (isLive) {
      // Existing positions close with their persisted fill size. Contract
      // orderability rules apply only when opening a new position.
      const size = orderSizeContracts(
        state.capital,
        state.maxPositionPct,
        state.entryPrice,
      ).size;
      const closeSize = gridCloseSize(state, size);
      if (size.greaterThan(0)) {
        // Stop and liquidation exits are protected-risk closes. Leave the
        // price unset so futures adapters submit a MARKET reduce-only order;
        // passing the theoretical trigger here turns the close into a
        // resting LIMIT order that can partially fill while the position is
        // supposed to be leaving immediately.
        const closePrice =
          exitReason === "target" ? theoreticalExitPrice : undefined;
        const fill = yield* adapter.closePosition({
          symbol: options.symbol,
          side: side === "long" ? "sell" : "buy",
          productType,
          marginMode,
          leverage: state.leverage,
          size: closeSize,
          price: closePrice,
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
    const close = buildGridCloseTrade({
      options,
      state,
      side,
      entryPrice: state.entryPrice,
      exitPrice,
      exitReason,
      stateCapital: state.capital,
      peakCapital: state.peakCapital,
      maxPositionPct: state.maxPositionPct,
      leverage: state.leverage,
      openedAt: state.updatedAt,
      entryEvidence: gridEntryEvidence(state),
      exitFill,
      fee,
      stopFee,
      fundingCost,
    });
    yield* repo.recordGridTrade(close.trade);
    yield* circuitBreaker.recordTradeResult(
      toNumber(close.capitalAfter.minus(state.capital)),
      toNumber(startOfDayCapital),
    );
    return { ...close, exitPrice };
  });
}

interface GridEntryInput {
  readonly state: GridPaperState;
  readonly setup: GridEntrySetup;
  readonly options: GridPaperTradingOptions;
  readonly current: Candle;
  readonly isLive: boolean;
  readonly productType: FuturesProductType;
  readonly marginMode: FuturesMarginMode;
  readonly strategyFingerprint: string;
  readonly executionEnvironment: GridExecutionEnvironment;
  readonly todayPnl: Decimal;
  readonly startOfDayCapital: Decimal;
  readonly services: GridIterationServices;
}

function gridMinimumOrderableNotional(
  options: GridPaperTradingOptions,
  entryPrice: Decimal,
): number | undefined {
  if (options.contractSpecs === undefined) return undefined;
  return toNumber(
    Decimal.max(
      money(options.contractSpecs.minTradeUSDT),
      money(options.contractSpecs.minQty).times(entryPrice),
    ),
  );
}

function gridOpenedState(
  input: GridEntryInput,
  entryPrice: Decimal,
  size: Decimal,
  leverage: number,
  fill: FuturesOrderFill | null,
): GridPaperState {
  const { state, current, strategyFingerprint, executionEnvironment } = input;
  const base = {
    ...state,
    side: input.setup.side,
    entryPrice,
    leverage,
    strategyConfigFingerprint: strategyFingerprint,
    cohortId: `grid-${strategyFingerprint.slice(0, 16)}`,
    candidateLockAt: current.timestamp,
    datasetCutoffAt: current.timestamp,
    entryOpenedAt: fill !== null ? new Date() : current.timestamp,
    executionEnvironment,
    updatedAt: new Date(),
    lastTimestamp: current.timestamp,
  };
  if (fill !== null) {
    return {
      ...base,
      entryOrderId: fill.orderId,
      entryClientOid: fill.clientOid,
      entryFilledQty: fill.filledQty,
      entryFee: fill.fee,
      entryFillSource: "live" as const,
    };
  }
  return {
    ...base,
    entryFilledQty: size,
    entryFee: money(0),
    entryFillSource: "simulated" as const,
  };
}

function gridEntryHold(
  state: GridPaperState,
  note: string,
): GridStateOperationResult {
  return { state, earlyResult: gridHoldResult(state, note) };
}

function executeLiveGridEntry(
  input: GridEntryInput,
  size: Decimal,
  orderLeverage: number,
): Effect.Effect<GridStateOperationResult, GridPaperIterationError> {
  return Effect.gen(function* () {
    const {
      state: originalState,
      setup,
      options,
      current,
      productType,
      marginMode,
      strategyFingerprint,
      executionEnvironment,
      services,
    } = input;
    const { adapter, repo, killSwitch } = services;
    yield* adapter.setLeverage(
      options.symbol,
      productType,
      marginMode,
      orderLeverage,
    );
    yield* adapter.setMarginMode(options.symbol, productType, marginMode);
    yield* adapter.setPositionMode(productType, "one_way");
    const placed = yield* adapter
      .placeOrder({
        symbol: options.symbol,
        side: setup.side === "long" ? "buy" : "sell",
        type: "limit",
        size,
        productType,
        marginMode,
        leverage: orderLeverage,
        price: setup.entryLevelPrice,
      })
      .pipe(Effect.result);
    if (placed._tag === "Failure") {
      const currentPosition = yield* adapter
        .getPosition(options.symbol, productType)
        .pipe(Effect.result);
      if (
        currentPosition._tag === "Success" &&
        currentPosition.success !== null
      ) {
        const state = adoptedGridState(
          originalState,
          currentPosition.success,
          strategyFingerprint,
          executionEnvironment,
          current.timestamp,
        );
        const persisted = yield* saveGridStateWithRetry(
          repo,
          killSwitch,
          state,
          "state save failed after order placement",
        );
        const note = `[LIVE] placed ${setup.side} ${size.toFixed(6)} then adopted ${currentPosition.success.side} ${currentPosition.success.quantity.toString()} @ ${currentPosition.success.entryPrice.toString()} (fill confirmation lost)`;
        return persisted
          ? { state, note }
          : gridEntryHold(
              state,
              "KILL SWITCH ENGAGED: state save failed after order placement",
            );
      }
      return yield* Effect.fail(
        new ExchangeError(
          `live ${setup.side} entry failed: ${placed.failure.reason}`,
          placed.failure,
        ),
      );
    }

    const fill = placed.success;
    // Record the EFFECTIVE leverage the adapter set on the exchange, not
    // the requested one: floor-sizing can raise it (yolo-btc 2026-09-03:
    // exchange lev 2 vs recorded 10 froze the account on reconciliation).
    const state = gridOpenedState(
      input,
      money(fill.filledPrice),
      size,
      fill.leverage ?? orderLeverage,
      fill,
    );
    const persisted = yield* saveGridStateWithRetry(
      repo,
      killSwitch,
      state,
      "state save failed after order placement",
    );
    const note = `[LIVE] opened ${setup.side} @ ${state.entryPrice.toFixed(2)} size=${size.toFixed(6)} (leverage=${orderLeverage}x)`;
    return persisted
      ? { state, note }
      : gridEntryHold(
          state,
          "KILL SWITCH ENGAGED: state save failed after order placement",
        );
  });
}

function openGridEntry(
  input: GridEntryInput,
  entryPrice: Decimal,
): Effect.Effect<GridStateOperationResult, GridPaperIterationError> {
  return Effect.gen(function* () {
    const {
      state,
      setup,
      options,
      isLive,
      productType,
      todayPnl,
      startOfDayCapital,
      services,
    } = input;
    const { repo, riskGuard } = services;
    if (isLive) {
      const tradesTodayCount = yield* repo.countTradesForDate(new Date());
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
          side: setup.side === "long" ? "buy" : "sell",
          leverage: state.leverage,
          productType,
          minOrderableNotional: gridMinimumOrderableNotional(
            options,
            entryPrice,
          ),
        })
        .pipe(Effect.result);
      if (riskCheck._tag === "Failure") {
        return {
          state,
          note: `RISK BLOCKED ${setup.side}: ${riskCheck.failure.violations.join("; ")}`,
        };
      }
    }

    const sized = orderSizeContracts(
      state.capital,
      state.maxPositionPct,
      entryPrice,
      { leverage: state.leverage, contractSpecs: options.contractSpecs },
    );
    if (sized.skipReason !== undefined) {
      return {
        state,
        note: `RISK BLOCKED ${setup.side} (orderability): ${sized.skipReason}`,
      };
    }
    if (sized.size.lessThanOrEqualTo(0)) {
      return { state, note: `RISK BLOCKED ${setup.side}: computed size zero` };
    }
    if (isLive) {
      return yield* executeLiveGridEntry(input, sized.size, sized.leverage);
    }
    const nextState = gridOpenedState(
      input,
      entryPrice,
      sized.size,
      sized.leverage,
      null,
    );
    return {
      state: nextState,
      note: `opened ${setup.side} @ ${nextState.entryPrice.toFixed(2)} size=${sized.size.toFixed(6)} (leverage=${sized.leverage}x)`,
    };
  });
}

interface GridFlatBarInput {
  readonly state: GridPaperState;
  readonly options: GridPaperTradingOptions;
  readonly candles: readonly Candle[];
  readonly index: number;
  readonly current: Candle;
  readonly trend: number | null;
  readonly mid: Decimal;
  readonly step: Decimal;
  readonly slippageFactor: number;
  readonly isLive: boolean;
  readonly productType: FuturesProductType;
  readonly marginMode: FuturesMarginMode;
  readonly strategyFingerprint: string;
  readonly executionEnvironment: GridExecutionEnvironment;
  readonly todayPnl: Decimal;
  readonly startOfDayCapital: Decimal;
  readonly services: GridIterationServices;
}

function processFlatGridBar(
  input: GridFlatBarInput,
): Effect.Effect<GridStateOperationResult, GridPaperIterationError> {
  return Effect.gen(function* () {
    const {
      state,
      options,
      candles,
      index,
      current,
      trend,
      mid,
      step,
      slippageFactor,
      isLive,
      productType,
      marginMode,
      strategyFingerprint,
      executionEnvironment,
      todayPnl,
      startOfDayCapital,
      services,
    } = input;
    const chopNote = gridChopGateNote(candles, index, options);
    if (chopNote !== null) {
      const advancedState = {
        ...state,
        lastTimestamp: current.timestamp,
        updatedAt: new Date(),
      };
      yield* services.repo.saveGridState(advancedState);
      return {
        state: advancedState,
        earlyResult: gridHoldResult(advancedState, chopNote),
      };
    }

    const setup = resolveGridEntry(
      current,
      trend,
      mid,
      step,
      slippageFactor,
      options,
    );
    if (setup === null) return { state };
    return yield* openGridEntry(
      {
        state,
        setup,
        options,
        current,
        isLive,
        productType,
        marginMode,
        strategyFingerprint,
        executionEnvironment,
        todayPnl,
        startOfDayCapital,
        services,
      },
      setup.theoreticalEntryPrice,
    );
  });
}

interface GridPositionExit {
  readonly reason: GridPaperTrade["exitReason"];
  readonly theoreticalExitPrice: Decimal;
  readonly killed: boolean;
  readonly paused: number;
}

type GridClosePosition = (
  side: GridPaperPositionSide,
  exitReason: GridPaperTrade["exitReason"],
  theoreticalExitPrice: Decimal,
) => Effect.Effect<
  GridCloseTradeResult & { readonly exitPrice: Decimal },
  ExchangeError | PaperTradingRepositoryError | CircuitBreakerError
>;

function resolveGridPositionExit(
  state: GridPaperState,
  options: GridPaperTradingOptions,
  current: Candle,
  step: Decimal,
  targetRatio: number,
  slippageFactor: number,
): GridPositionExit | null {
  const { side, entryPrice } = state;
  if (side === null) return null;
  const target =
    side === "long"
      ? entryPrice.plus(step.times(targetRatio))
      : entryPrice.minus(step.times(targetRatio));
  const stop =
    side === "long"
      ? entryPrice.minus(step.times(options.gridMaxGrids))
      : entryPrice.plus(step.times(options.gridMaxGrids));
  const liquidation = liquidationPrice(
    side,
    entryPrice,
    state.leverage,
    options.maintenanceMarginRate ?? 0,
  );
  if (
    side === "long" &&
    liquidation.greaterThan(0) &&
    money(current.low).lessThanOrEqualTo(liquidation)
  ) {
    return {
      reason: "liquidation",
      theoreticalExitPrice: liquidation.div(slippageFactor),
      killed: true,
      paused: 0,
    };
  }
  if (
    side === "short" &&
    liquidation.greaterThan(0) &&
    money(current.high).greaterThanOrEqualTo(liquidation)
  ) {
    return {
      reason: "liquidation",
      theoreticalExitPrice: liquidation.times(slippageFactor),
      killed: true,
      paused: 0,
    };
  }
  if (
    (side === "long" && money(current.high).greaterThanOrEqualTo(target)) ||
    (side === "short" && money(current.low).lessThanOrEqualTo(target))
  ) {
    return {
      reason: "target",
      theoreticalExitPrice: target,
      killed: false,
      paused: 0,
    };
  }
  if (
    (side === "long" && money(current.low).lessThanOrEqualTo(stop)) ||
    (side === "short" && money(current.high).greaterThanOrEqualTo(stop))
  ) {
    return {
      reason: "stop",
      theoreticalExitPrice:
        side === "long" ? stop.div(slippageFactor) : stop.times(slippageFactor),
      killed: false,
      paused: options.gridPauseAfterLossBars,
    };
  }
  return null;
}

function gridStateAfterPositionExit(
  state: GridPaperState,
  current: Candle,
  exit: GridPositionExit,
  close: GridCloseTradeResult & { readonly exitPrice: Decimal },
): GridPaperState {
  const base = {
    ...state,
    side: null,
    entryPrice: money(0),
    capital: close.capitalAfter,
    peakCapital: close.peakCapital,
    paused: exit.paused,
    updatedAt: new Date(),
    lastTimestamp: current.timestamp,
  };
  return exit.killed ? { ...base, killed: true } : base;
}

function gridPositionExitNote(
  state: GridPaperState,
  isLive: boolean,
  exit: GridPositionExit,
  close: GridCloseTradeResult & { readonly exitPrice: Decimal },
): string {
  if (exit.reason === "liquidation") {
    return `liquidated ${state.side} @ ${close.exitPrice.toFixed(2)} pnl=-100.000% (leverage=${state.leverage}x)`;
  }
  return `${isLive ? "[LIVE] " : ""}closed ${state.side} ${exit.reason} @ ${close.exitPrice.toFixed(2)} pnl=${close.trade.pnlPct.toFixed(3)}%`;
}

function processGridPosition(
  state: GridPaperState,
  options: GridPaperTradingOptions,
  current: Candle,
  step: Decimal,
  slippageFactor: number,
  isLive: boolean,
  closePosition: GridClosePosition,
): Effect.Effect<GridStateOperationResult, GridPaperIterationError> {
  return Effect.gen(function* () {
    if (state.side === null) return { state };
    const exit = resolveGridPositionExit(
      state,
      options,
      current,
      step,
      options.targetRatio ?? 1,
      slippageFactor,
    );
    if (exit === null) return { state };
    const close = yield* closePosition(
      state.side,
      exit.reason,
      exit.theoreticalExitPrice,
    );
    const nextState = gridStateAfterPositionExit(state, current, exit, close);
    return {
      state: nextState,
      note: gridPositionExitNote(state, isLive, exit, close),
    };
  });
}

interface GridFinalizeInput {
  readonly state: GridPaperState;
  readonly options: GridPaperTradingOptions;
  readonly current: Candle;
  readonly note: string;
  readonly repo: PaperTradingRepositoryService;
}

function finalizeGridBar(
  input: GridFinalizeInput,
): Effect.Effect<GridPaperTradingIterationResult, PaperTradingRepositoryError> {
  return Effect.gen(function* () {
    let { state, note } = input;
    const { current, repo } = input;
    state = {
      ...state,
      lastTimestamp: current.timestamp,
      updatedAt: new Date(),
    };
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
    const services = yield* loadGridServices();
    const { repo, gateway, killSwitch } = services;
    yield* repo.ensureTables();
    const executionEnvironment = gridExecutionEnvironment(options);
    const strategyFingerprint = fingerprintStrategyManifest(
      strategyManifestFor(options, executionEnvironment),
    );
    const loadedState = yield* loadGridState(
      repo,
      options,
      strategyFingerprint,
    );
    if (loadedState.earlyResult) return loadedState.earlyResult;
    const guardedState = yield* guardKilledGridState(
      loadedState.state,
      repo,
      killSwitch,
    );
    if (guardedState.earlyResult) return guardedState.earlyResult;
    const state = guardedState.state;
    const candles = yield* gateway.fetchOHLCV(
      options.exchange,
      options.symbol,
      options.timeframe,
      requiredGridCandles(options),
    );
    const market = resolveGridMarket(state, options, candles);
    if (market.earlyResult) return market.earlyResult;
    return yield* processGridBar({
      options,
      state,
      strategyFingerprint,
      executionEnvironment,
      services,
      market: market.market!,
      note: loadedState.reseedNote,
    });
  });
}

function processGridBar(
  input: GridBarInput,
): Effect.Effect<GridPaperTradingIterationResult, GridPaperIterationError> {
  return Effect.gen(function* () {
    const {
      options,
      strategyFingerprint,
      executionEnvironment,
      services,
      market,
    } = input;
    const { repo, adapter, circuitBreaker } = services;
    let state = input.state;
    const { candles, index: i, current, trend } = market;
    const isLive = options.isLive ?? false;
    const productType = options.productType ?? "USDT-FUTURES";
    const marginMode = options.marginMode ?? "isolated";

    let note = input.note ?? "no action";

    if (isLive) {
      const liveState = yield* reconcileGridLiveState({
        state,
        options,
        current,
        strategyFingerprint,
        executionEnvironment,
        services,
      });
      state = liveState.state;
      if (liveState.note !== undefined) note = liveState.note;
      if (liveState.earlyResult) return liveState.earlyResult;
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

    const fundingCost = gridFundingCost(state, options, current);

    const todayPnl = yield* repo.getTodayRealizedPnl();
    const startOfDayCapital = yield* repo.getStartOfDayCapital(
      new Date(),
      state.capital,
    );

    const closePosition = (
      side: GridPaperPositionSide,
      exitReason: GridPaperTrade["exitReason"],
      theoreticalExitPrice: Decimal,
    ) =>
      closeGridPosition({
        state,
        options,
        side,
        exitReason,
        theoreticalExitPrice,
        isLive,
        productType,
        marginMode,
        adapter,
        repo,
        circuitBreaker,
        startOfDayCapital,
        fee,
        stopFee,
        fundingCost,
      });

    if (state.side === null) {
      const flatResult = yield* processFlatGridBar({
        state,
        options,
        candles,
        index: i,
        current,
        trend,
        mid,
        step,
        slippageFactor,
        isLive,
        productType,
        marginMode,
        strategyFingerprint,
        executionEnvironment,
        todayPnl,
        startOfDayCapital,
        services,
      });
      state = flatResult.state;
      if (flatResult.note !== undefined) note = flatResult.note;
      if (flatResult.earlyResult) return flatResult.earlyResult;
    } else {
      const positionResult = yield* processGridPosition(
        state,
        options,
        current,
        step,
        slippageFactor,
        isLive,
        closePosition,
      );
      state = positionResult.state;
      if (positionResult.note !== undefined) note = positionResult.note;
    }

    return yield* finalizeGridBar({
      state,
      options,
      current,
      note,
      repo,
    });
  });
}
