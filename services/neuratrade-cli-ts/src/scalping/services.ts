/**
 * Effect service façades over the pure scalping engines.
 *
 * The underlying modules (`backtest.ts`, `grid.ts`, `composer.ts`,
 * `exit-engine.ts`, `strategy-library.ts`) stay pure and are exercised
 * directly by their unit tests. These services wrap them so Effect-based
 * callers (CLI commands, paper-trading engines) consume them via `yield*`
 * instead of calling the pure functions directly. Delegation is
 * behavior-identical: methods return `Effect.sync` around the pure call,
 * or `Effect.try` where the pure function can throw.
 */
import { Context, Effect, Layer } from "effect";
import {
  feeFractionWarning,
  runBacktest,
  type BacktestOptions,
  type BacktestResult,
} from "./backtest.js";
import { runGridBacktest, type GridOptions, type GridResult } from "./grid.js";
import {
  runLadderGridBacktest,
  type LadderOptions,
  type LadderResult,
} from "./ladder-grid.js";
import { composeSignal } from "./composer.js";
import {
  checkRsiExit,
  computeExitLevels,
  type ExitEngineOptions,
  type ExitLevels,
  type RsiExitOptions,
} from "./exit-engine.js";
import {
  buildBacktestArgsFromTemplate,
  buildComposerConfigFromTemplate,
  listStrategies,
  type StrategyTemplate,
  type StrategyTemplateName,
} from "./strategy-library.js";
import type { ResolvedBacktestArgs } from "./strategy-profile.js";
import type {
  CandleLike,
  ComposerConfig,
  OHLCVInput,
  OrderBookMetricsInput,
  ScalpingSignal,
} from "./types.js";

// ---------------------------------------------------------------------------
// BacktestEngine
// ---------------------------------------------------------------------------

export interface BacktestEngineImpl {
  /** Run the signal-based backtest engine over historical candles. */
  readonly runBacktest: (
    options: BacktestOptions,
  ) => Effect.Effect<BacktestResult>;
  /** Run the market-neutral grid backtest engine over historical candles. */
  readonly runGridBacktest: (
    candles: readonly CandleLike[],
    options: GridOptions,
  ) => Effect.Effect<GridResult>;
  /** Run the multi-level ladder grid backtest engine over historical candles. */
  readonly runLadderGridBacktest: (
    candles: readonly CandleLike[],
    options: LadderOptions,
  ) => Effect.Effect<LadderResult>;
}

export class BacktestEngine extends Context.Service<
  BacktestEngine,
  BacktestEngineImpl
>()("BacktestEngine") {}

export const BacktestEngineLive: Layer.Layer<BacktestEngine> = Layer.succeed(
  BacktestEngine,
  {
    runBacktest: (options) =>
      Effect.gen(function* () {
        // Fee-convention warnings used to be emitted by the pure engine via
        // console.warn; they are routed through Effect.logWarning here at the
        // Effect boundary so the pure module stays side-effect free.
        const feeWarning = feeFractionWarning(options.feePct, "fee");
        if (feeWarning !== null) {
          yield* Effect.logWarning(feeWarning);
        }
        if (options.makerFeePct !== undefined) {
          const makerWarning = feeFractionWarning(
            options.makerFeePct,
            "maker-fee",
          );
          if (makerWarning !== null) {
            yield* Effect.logWarning(makerWarning);
          }
        }
        return yield* Effect.sync(() => runBacktest(options));
      }),
    runGridBacktest: (candles, options) =>
      Effect.sync(() => runGridBacktest(candles, options)),
    runLadderGridBacktest: (candles, options) =>
      Effect.sync(() => runLadderGridBacktest(candles, options)),
  } satisfies BacktestEngineImpl,
);

// ---------------------------------------------------------------------------
// SignalComposer
// ---------------------------------------------------------------------------

export interface SignalComposerImpl {
  /** Compose a deterministic scalping signal from OHLCV + order-book inputs. */
  readonly composeSignal: (
    ohlcv: OHLCVInput,
    obMetrics: OrderBookMetricsInput,
    config?: ComposerConfig,
  ) => Effect.Effect<ScalpingSignal | null>;
}

export class SignalComposer extends Context.Service<
  SignalComposer,
  SignalComposerImpl
>()("SignalComposer") {}

export const SignalComposerLive: Layer.Layer<SignalComposer> = Layer.succeed(
  SignalComposer,
  {
    composeSignal: (ohlcv, obMetrics, config) =>
      Effect.sync(() => composeSignal(ohlcv, obMetrics, config)),
  } satisfies SignalComposerImpl,
);

// ---------------------------------------------------------------------------
// ExitEngine
// ---------------------------------------------------------------------------

export interface ExitEngineImpl {
  /** Compute stop-loss / take-profit / scale-out levels for a position. */
  readonly computeExitLevels: (
    options: ExitEngineOptions,
  ) => Effect.Effect<ExitLevels>;
  /** Check whether an open position should exit because RSI normalized. */
  readonly checkRsiExit: (options: RsiExitOptions) => Effect.Effect<boolean>;
}

export class ExitEngine extends Context.Service<ExitEngine, ExitEngineImpl>()(
  "ExitEngine",
) {}

export const ExitEngineLive: Layer.Layer<ExitEngine> = Layer.succeed(
  ExitEngine,
  {
    computeExitLevels: (options) =>
      Effect.sync(() => computeExitLevels(options)),
    checkRsiExit: (options) => Effect.sync(() => checkRsiExit(options)),
  } satisfies ExitEngineImpl,
);

// ---------------------------------------------------------------------------
// StrategyLibrary
// ---------------------------------------------------------------------------

export interface StrategyLibraryImpl {
  /** List all available strategy templates. */
  readonly listStrategies: () => Effect.Effect<readonly StrategyTemplate[]>;
  /** Merge a template's execution overrides into base backtest args. */
  readonly buildBacktestArgsFromTemplate: (
    templateName: StrategyTemplateName,
    baseArgs: Partial<ResolvedBacktestArgs>,
  ) => Effect.Effect<ResolvedBacktestArgs, Error>;
  /** Merge a template's composer-config overrides into a base config. */
  readonly buildComposerConfigFromTemplate: (
    templateName: StrategyTemplateName,
    baseConfig?: ComposerConfig,
  ) => Effect.Effect<ComposerConfig, Error>;
}

export class StrategyLibrary extends Context.Service<
  StrategyLibrary,
  StrategyLibraryImpl
>()("StrategyLibrary") {}

const toError = (cause: unknown): Error =>
  cause instanceof Error ? cause : new Error(String(cause));

export const StrategyLibraryLive: Layer.Layer<StrategyLibrary> = Layer.succeed(
  StrategyLibrary,
  {
    listStrategies: () => Effect.sync(() => listStrategies()),
    buildBacktestArgsFromTemplate: (templateName, baseArgs) =>
      Effect.try({
        try: () => buildBacktestArgsFromTemplate(templateName, baseArgs),
        catch: toError,
      }),
    buildComposerConfigFromTemplate: (templateName, baseConfig) =>
      Effect.try({
        try: () => buildComposerConfigFromTemplate(templateName, baseConfig),
        catch: toError,
      }),
  } satisfies StrategyLibraryImpl,
);
