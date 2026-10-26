/**
 * scalp CLI options — optimize
 *
 * Sweep/walk-forward/scan flags (how the parameters are searched and reported).
 */
import { Options } from "../kit/kit.ts";

export const scanEntryOrdersOption = Options.boolean("scan-entry-orders").pipe(
  Options.withDefault(false),
  Options.withDescription("Scan market/limit entry order types in optimize"),
);

export const randomSearchOption = Options.integer("random-search").pipe(
  Options.withDefault(0),
  Options.withDescription("Random search sample count (0 = full grid)"),
);

export const walkForwardOption = Options.boolean("walk-forward").pipe(
  Options.withDefault(false),
  Options.withDescription("Run walk-forward optimization"),
);

export const wfTrainDaysOption = Options.integer("wf-train-days").pipe(
  Options.withDefault(180),
  Options.withDescription("Walk-forward training window in days"),
);

export const wfTestDaysOption = Options.integer("wf-test-days").pipe(
  Options.withDefault(60),
  Options.withDescription("Walk-forward testing window in days"),
);

export const wfStepDaysOption = Options.integer("wf-step-days").pipe(
  Options.withDefault(60),
  Options.withDescription("Walk-forward step size in days"),
);

export const minTradesOption = Options.integer("min-trades").pipe(
  Options.withDefault(0),
  Options.withDescription("Minimum trades filter for optimization/selection"),
);

export const minOosTradesOption = Options.integer("min-oos-trades").pipe(
  Options.withDefault(0),
  Options.withDescription("Minimum out-of-sample trades filter"),
);

export const selectByOption = Options.choice("select-by", [
  "return",
  "sharpe",
  "calmar",
] as const).pipe(
  Options.withDefault("return" as const),
  Options.withDescription("Objective for selecting best candidate"),
);

export const recordEquityCurveOption = Options.boolean(
  "record-equity-curve",
).pipe(
  Options.withDefault(false),
  Options.withDescription("Record equity curve at each trade close"),
);

export const exportTradesOption = Options.text("export-trades").pipe(
  Options.withDefault(""),
  Options.withDescription(
    "Write trades.csv (+ equity.csv if --record-equity-curve) to this path",
  ),
);

export const oosPctOption = Options.integer("oos-pct").pipe(
  Options.withDefault(0),
  Options.withDescription(
    "Percentage of recent candles to hold out for out-of-sample validation (0 disables)",
  ),
);

export const mcIterationsOption = Options.integer("mc-iterations").pipe(
  Options.withDefault(0),
  Options.withDescription(
    "Number of Monte Carlo permutations for drawdown simulation (0 disables)",
  ),
);

export const optimizeScanOption = Options.boolean("optimize").pipe(
  Options.withDefault(false),
  Options.withDescription(
    "Run a coarse per-symbol parameter grid search and report best params",
  ),
);

export const minReturnOption = Options.float("min-return-pct").pipe(
  Options.optional,
  Options.withDescription(
    "Skip symbols with total return below this threshold",
  ),
);

export const minSharpeOption = Options.float("min-sharpe").pipe(
  Options.optional,
  Options.withDescription(
    "Skip symbols with Sharpe ratio below this threshold",
  ),
);

export const scanMaxDrawdownOption = Options.float("max-drawdown-pct").pipe(
  Options.optional,
  Options.withDescription(
    "Skip symbols with max drawdown above this threshold",
  ),
);

export const saveWatchlistOption = Options.text("save-watchlist").pipe(
  Options.optional,
  Options.withDescription(
    "Write passing symbols to a JSON watchlist file in NEURATRADE_HOME/data",
  ),
);

export const iterationsOption = Options.integer("iterations").pipe(
  Options.withDefault(1),
  Options.withDescription("Number of iterations to run (0 = infinite)"),
);

export const trainWindowOption = Options.integer("train-window").pipe(
  Options.withDefault(180),
  Options.withDescription("Walk-forward training window in days"),
);

export const testWindowOption = Options.integer("test-window").pipe(
  Options.withDefault(60),
  Options.withDescription("Walk-forward testing window in days"),
);

export const wfMinTradesOption = Options.integer("min-trades").pipe(
  Options.withDefault(0),
  Options.withDescription("Minimum trades per window"),
);

export const minTradesPerMonthOption = Options.integer(
  "min-trades-per-month",
).pipe(
  Options.optional,
  Options.withDescription(
    "G1 override: minimum in-sample trades per month (default 20 for 5m, 10 otherwise)",
  ),
);
