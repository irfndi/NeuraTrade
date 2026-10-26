/**
 * scalp CLI option surface.
 *
 * The flat option plumbing that used to live in src/cli/scalp.ts — and then in
 * one 1.5k-line module — is grouped by concern so a change lands next to the
 * flags it belongs to:
 *
 *   market     what to look at (instrument, timeframe, data source)
 *   sizing     how much and where (capital, leverage, limits, fees, live/demo)
 *   signal     entry signal, regime, session, strategy profile (+ their ranges)
 *   exit       stops, targets, ATR/trailing, scale-out, time stops
 *   grid       grid geometry shared by backtest, live grid and readiness
 *   optimize   sweep / walk-forward / scan parameters
 *   research   grid-universe, flow and watchlist research flags
 *
 * Everything is re-exported here so command wiring imports one specifier. The
 * load-bearing user-facing defaults stay pinned in SCALP_OPTION_DEFAULTS
 * (asserted by src/cli/scalp.test.ts).
 */
export { SCALP_OPTION_DEFAULTS, minMaxStep } from "./defaults.js";
export * from "./market.js";
export * from "./sizing.js";
export * from "./signal.js";
export * from "./exit.js";
export * from "./grid.js";
export * from "./optimize.js";
export * from "./research.js";
