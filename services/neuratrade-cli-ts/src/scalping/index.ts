/**
 * Scalping module public API surface.
 */

// Ladder Grid Engine
export {
  runLadderGridBacktest,
  liquidationPrice,
  findBestLadderGridParams,
  runLadderGridWalkForward,
  type LadderOptions,
  type LadderTrade,
  type LadderResult,
  type LadderSearchSpace,
  type LadderWalkForwardWindow,
  type LadderWalkForwardResult,
} from "./ladder-grid.js";

// Single-Position Grid Engine
export {
  runGridBacktest,
  findBestGridParams,
  runGridWalkForward,
  type GridOptions,
  type GridTrade,
  type GridResult,
  type GridSearchSpace,
  type GridWalkForwardWindow,
  type GridWalkForwardResult,
} from "./grid.js";

// Core Types
export type {
  CandleLike,
  Direction,
  SignalStrength,
  SignalComponent,
  MicrostructureContext,
  QualityAssessment,
  ScalpingSignalMetadataValue,
  ScalpingSignal,
  OHLCVInput,
  OrderBookMetricsInput,
  ComposerWeights,
  ComposerIndicatorName,
  ComposerEnabled,
  ComposerThresholds,
  ComposerConfig,
} from "./types.js";

// Signal Backtesting & Composer
export {
  runBacktest,
  type BacktestOptions,
  type BacktestResult,
  type BacktestTrade,
} from "./backtest.js";
export { composeSignal, defaultComposerConfig } from "./composer.js";

// Exit Engine
export {
  computeExitLevels,
  checkRsiExit,
  type ExitEngineOptions,
  type ExitLevels,
  type RsiExitOptions,
} from "./exit-engine.js";

// Indicators
export {
  calculateSMA,
  calculateEMA,
  calculateRSI,
  calculateRSISeries,
  calculateATR,
  calculateBollingerBands,
  calculateADX,
} from "./indicators.js";

// Strategy Library & Presets
export {
  listStrategies,
  buildBacktestArgsFromTemplate,
  buildComposerConfigFromTemplate,
  type StrategyTemplate,
  type StrategyTemplateName,
} from "./strategy-library.js";
export { applyPreset, listPresets, type PresetName } from "./presets.js";

// Effect Services
export {
  BacktestEngine,
  BacktestEngineLive,
  SignalComposer,
  SignalComposerLive,
  ExitEngine,
  ExitEngineLive,
  StrategyLibrary,
  StrategyLibraryLive,
} from "./services.js";

// Grid Validation & Universe
export {
  validateGridEvidence,
  type GridValidationOk,
  type GridValidationResult,
} from "./grid-validation.js";
export {
  runGridUniverseScan,
  type GridUniverseOptions,
  type GridUniverseResult,
} from "./grid-universe.js";
export {
  VALIDATED_BTC_GRID_CANDIDATE,
  VALIDATED_SOL_GRID_CANDIDATE,
  VALIDATED_ETH_GRID_CANDIDATE,
  READINESS_COHORT_CANDIDATES,
  candidateForSymbol,
} from "./grid-candidate.js";
export { computePerformanceMetrics } from "./performance-metrics.js";
