import { Command, Options } from "./kit/kit.ts";
import { Console, Effect, Layer } from "effect";
import { SqliteClient, type SqliteError } from "../services/sqlite.js";
import {
  MarketDataRepositoryError,
  MarketDataRepositorySQLite,
} from "../market-data/repository.js";
import type { Candle } from "../market-data/types.js";
import {
  TimesFmWorker,
  type TimesFmForecastRecord,
  type TimesFmForecastRequest,
  type TimesFmWorkerOptions,
} from "../services/timesfm-client.js";

const exchangeOption = Options.text("exchange").pipe(
  Options.withDefault("bybit-futures"),
  Options.withDescription("Exchange key used for the stored candle window"),
);

const symbolOption = Options.text("symbol").pipe(
  Options.withDefault("BTC/USDT:USDT"),
  Options.withDescription("Symbol key used for the stored candle window"),
);

const timeframeOption = Options.text("timeframe").pipe(
  Options.withDefault("1m"),
  Options.withDescription("Regular candle timeframe (1m, 5m, 15m, 1h, ...)"),
);

const contextBarsOption = Options.integer("context-bars").pipe(
  Options.withDefault(256),
  Options.withDescription("Number of trailing candles sent to TimesFM"),
);

const horizonOption = Options.integer("horizon").pipe(
  Options.withDefault(12),
  Options.withDescription("Forecast horizon in candles"),
);

const checkpointOption = Options.text("checkpoint").pipe(
  Options.withDefault("google/timesfm-3.0-pytorch"),
  Options.withDescription("Hugging Face repo or local TimesFM checkpoint"),
);

const deviceOption = Options.text("device").pipe(
  Options.withDefault("auto"),
  Options.withDescription("PyTorch device (auto, cpu, cuda, ...)"),
);

const batchSizeOption = Options.integer("batch-size").pipe(
  Options.withDefault(4),
  Options.withDescription("TimesFM worker batch size"),
);

const cacheDirOption = Options.text("cache-dir").pipe(
  Options.withDefault(""),
  Options.withDescription("Optional Hugging Face model cache directory"),
);

const localFilesOnlyOption = Options.boolean("local-files-only").pipe(
  Options.withDefault(false),
  Options.withDescription("Do not download a missing checkpoint"),
);

const torchThreadsOption = Options.integer("torch-threads").pipe(
  Options.withDefault(0),
  Options.withDescription("Torch CPU threads (0 leaves the runtime default)"),
);

const useZnormOption = Options.boolean("use-znorm").pipe(
  Options.withDefault(false),
  Options.withDescription("Z-normalize each target context before inference"),
);

const symmetricAveragingOption = Options.boolean("symmetric-averaging").pipe(
  Options.withDefault(true),
  Options.withDescription("Use TimesFM sign-symmetric ensemble averaging"),
);

const volumeAsPastCovariateOption = Options.boolean(
  "volume-as-past-covariate",
).pipe(
  Options.withDefault(false),
  Options.withDescription(
    "Send log-volume as a past-only covariate instead of a forecast target",
  ),
);

const dryRunOption = Options.boolean("dry-run").pipe(
  Options.withDefault(false),
  Options.withDescription(
    "Validate the sidecar protocol without model weights",
  ),
);

const jsonOption = Options.boolean("json").pipe(
  Options.withDefault(false),
  Options.withDescription("Print the result as one JSON document"),
);

const MIN_CONTEXT_BARS = 32;
const MAX_CONTEXT_BARS = 15_360;
const MAX_HORIZON = 1_024;

const TIMEFRAME_MS = new Map<string, number>([
  ["1m", 60_000],
  ["3m", 180_000],
  ["5m", 300_000],
  ["15m", 900_000],
  ["30m", 1_800_000],
  ["1h", 3_600_000],
  ["2h", 7_200_000],
  ["4h", 14_400_000],
  ["6h", 21_600_000],
  ["12h", 43_200_000],
  ["1d", 86_400_000],
]);

export interface TimesFmForecastArgs {
  readonly exchange: string;
  readonly symbol: string;
  readonly timeframe: string;
  readonly contextBars: number;
  readonly horizon: number;
  readonly checkpoint: string;
  readonly device: string;
  readonly batchSize: number;
  readonly cacheDir: string;
  readonly localFilesOnly: boolean;
  readonly torchThreads: number;
  readonly useZnorm: boolean;
  readonly symmetricAveraging: boolean;
  readonly volumeAsPastCovariate: boolean;
  readonly dryRun: boolean;
  readonly json: boolean;
}

export interface TimesFmForecastSummary {
  readonly exchange: string;
  readonly symbol: string;
  readonly timeframe: string;
  readonly contextBars: number;
  readonly horizon: number;
  readonly lastTimestamp: string;
  readonly lastClose: number;
  readonly latencyMs: number;
  readonly direction: "long" | "short" | "flat";
  readonly terminalPointReturnPct: number;
  readonly terminalQ10ReturnPct: number | null;
  readonly terminalQ90ReturnPct: number | null;
  readonly pointReturnsPct: readonly number[];
}

function validateArgs(args: TimesFmForecastArgs): Effect.Effect<void, Error> {
  return Effect.try({
    try: () => {
      if (
        args.contextBars < MIN_CONTEXT_BARS ||
        args.contextBars > MAX_CONTEXT_BARS
      ) {
        throw new Error(
          `--context-bars must be between ${MIN_CONTEXT_BARS} and ${MAX_CONTEXT_BARS}`,
        );
      }
      if (args.horizon < 1 || args.horizon > MAX_HORIZON) {
        throw new Error(`--horizon must be between 1 and ${MAX_HORIZON}`);
      }
      if (args.batchSize < 1) throw new Error("--batch-size must be positive");
      if (args.torchThreads < 0) {
        throw new Error("--torch-threads cannot be negative");
      }
      if (TIMEFRAME_MS.get(args.timeframe) === undefined) {
        throw new Error(
          `unsupported --timeframe ${args.timeframe}; use a regular timeframe such as 1m, 5m, 15m, 1h, or 1d`,
        );
      }
    },
    catch: (error) =>
      error instanceof Error ? error : new Error(String(error)),
  });
}

function assertFinite(value: number, field: string): number {
  if (!Number.isFinite(value)) throw new Error(`${field} must be finite`);
  return value;
}

export function buildTimesFmRequest(
  args: Pick<
    TimesFmForecastArgs,
    | "exchange"
    | "symbol"
    | "timeframe"
    | "horizon"
    | "useZnorm"
    | "symmetricAveraging"
  > & {
    readonly candles: readonly Candle[];
    readonly volumeAsPastCovariate?: boolean;
  },
): TimesFmForecastRequest {
  const intervalMs = TIMEFRAME_MS.get(args.timeframe);
  if (intervalMs === undefined) {
    throw new Error(`unsupported timeframe: ${args.timeframe}`);
  }
  if (args.candles.length < MIN_CONTEXT_BARS) {
    throw new Error(
      `TimesFM needs at least ${MIN_CONTEXT_BARS} candles; got ${args.candles.length}`,
    );
  }
  const timestampsMs = args.candles.map((candle) => {
    const timestamp = candle.timestamp.getTime();
    if (!Number.isSafeInteger(timestamp)) {
      throw new Error("candle timestamp is not a safe integer");
    }
    return timestamp;
  });
  const closes = args.candles.map((candle) => {
    const close = assertFinite(candle.close, "candle close");
    if (close <= 0)
      throw new Error("candle close must be positive for log transform");
    return Math.log(close);
  });
  const volumes = args.candles.map((candle) => {
    const volume = assertFinite(candle.volume, "candle volume");
    if (volume < 0) throw new Error("candle volume cannot be negative");
    return Math.log1p(volume);
  });
  const volumeAsPastCovariate = args.volumeAsPastCovariate ?? false;
  return {
    requestId: `timesfm-${Date.now()}-${crypto.randomUUID()}`,
    horizon: args.horizon,
    intervalMs,
    series: [
      {
        id: `${args.exchange}:${args.symbol}:${args.timeframe}`,
        timestampsMs,
        targets: volumeAsPastCovariate ? [closes] : [closes, volumes],
        targetNames: volumeAsPastCovariate
          ? ["log_close"]
          : ["log_close", "log_volume"],
        pastOnlyCovariates: volumeAsPastCovariate ? [volumes] : undefined,
      },
    ],
    returnQuantiles: true,
    useSymmetricAveraging: args.symmetricAveraging,
    useZnorm: args.useZnorm,
  };
}

function syntheticRequest(args: TimesFmForecastArgs): TimesFmForecastRequest {
  const intervalMs = TIMEFRAME_MS.get(args.timeframe);
  if (intervalMs === undefined) throw new Error("invalid timeframe");
  const start = Date.now() - (args.contextBars - 1) * intervalMs;
  const timestampsMs = Array.from(
    { length: args.contextBars },
    (_, index) => start + index * intervalMs,
  );
  const closes = timestampsMs.map((_, index) => Math.log(100 + index / 100));
  const volumes = timestampsMs.map(() => Math.log1p(1));
  return {
    requestId: `timesfm-validation-${Date.now()}`,
    horizon: args.horizon,
    intervalMs,
    series: [
      {
        id: "validation",
        timestampsMs,
        targets: args.volumeAsPastCovariate ? [closes] : [closes, volumes],
        targetNames: args.volumeAsPastCovariate
          ? ["log_close"]
          : ["log_close", "log_volume"],
        pastOnlyCovariates: args.volumeAsPastCovariate ? [volumes] : undefined,
      },
    ],
    returnQuantiles: true,
    useSymmetricAveraging: args.symmetricAveraging,
    useZnorm: args.useZnorm,
  };
}

function terminalQuantileReturn(
  record: TimesFmForecastRecord,
  targetIndex: number,
  horizonIndex: number,
  lastLogClose: number,
  quantileIndex: number,
): number | null {
  const variate = record.quantiles?.[targetIndex];
  const horizon = variate?.[horizonIndex];
  const logValue = horizon?.[quantileIndex];
  if (logValue === undefined) return null;
  return (Math.exp(logValue - lastLogClose) - 1) * 100;
}

function summarizeForecast(
  args: TimesFmForecastArgs & { readonly candles: readonly Candle[] },
  request: TimesFmForecastRequest,
  record: TimesFmForecastRecord,
  latencyMs: number,
): TimesFmForecastSummary {
  const lastCandle = args.candles.at(-1);
  if (lastCandle === undefined)
    throw new Error("forecast has no source candle");
  const targetForecast = record.forecast[0];
  if (targetForecast === undefined || targetForecast.length !== args.horizon) {
    throw new Error("TimesFM returned an unexpected log-close forecast shape");
  }
  const lastLogClose = request.series[0]?.targets[0]?.at(-1);
  if (lastLogClose === undefined)
    throw new Error("forecast request has no close target");
  const pointReturnsPct = targetForecast.map(
    (logValue) => (Math.exp(logValue - lastLogClose) - 1) * 100,
  );
  const terminalPointReturnPct = pointReturnsPct.at(-1);
  if (terminalPointReturnPct === undefined) {
    throw new Error("TimesFM returned an empty forecast");
  }
  const q10 = terminalQuantileReturn(
    record,
    0,
    args.horizon - 1,
    lastLogClose,
    0,
  );
  const quantileCount = record.quantiles?.[0]?.[args.horizon - 1]?.length ?? 0;
  const q90 =
    quantileCount > 0
      ? terminalQuantileReturn(
          record,
          0,
          args.horizon - 1,
          lastLogClose,
          quantileCount - 1,
        )
      : null;
  const direction =
    q10 !== null && q10 > 0
      ? "long"
      : q90 !== null && q90 < 0
        ? "short"
        : "flat";
  return {
    exchange: args.exchange,
    symbol: args.symbol,
    timeframe: args.timeframe,
    contextBars: args.candles.length,
    horizon: args.horizon,
    lastTimestamp: lastCandle.timestamp.toISOString(),
    lastClose: lastCandle.close,
    latencyMs,
    direction,
    terminalPointReturnPct,
    terminalQ10ReturnPct: q10,
    terminalQ90ReturnPct: q90,
    pointReturnsPct,
  };
}

function workerOptions(args: TimesFmForecastArgs): TimesFmWorkerOptions {
  return {
    checkpoint: args.checkpoint,
    device: args.device,
    batchSize: args.batchSize,
    cacheDir: args.cacheDir.length > 0 ? args.cacheDir : undefined,
    localFilesOnly: args.localFilesOnly,
    torchThreads: args.torchThreads,
    validateOnly: args.dryRun,
  };
}

const commandOptions = {
  exchange: exchangeOption,
  symbol: symbolOption,
  timeframe: timeframeOption,
  contextBars: contextBarsOption,
  horizon: horizonOption,
  checkpoint: checkpointOption,
  device: deviceOption,
  batchSize: batchSizeOption,
  cacheDir: cacheDirOption,
  localFilesOnly: localFilesOnlyOption,
  torchThreads: torchThreadsOption,
  useZnorm: useZnormOption,
  symmetricAveraging: symmetricAveragingOption,
  volumeAsPastCovariate: volumeAsPastCovariateOption,
  dryRun: dryRunOption,
  json: jsonOption,
} as const;

export function makeTimesFmForecastCommand(
  dbLayer: Layer.Layer<SqliteClient, SqliteError, never>,
) {
  return Command.make("timesfm-forecast", commandOptions, (args) =>
    Effect.gen(function* () {
      yield* validateArgs(args);
      const requestAndCandles = args.dryRun
        ? { request: syntheticRequest(args), candles: undefined }
        : yield* Effect.gen(function* () {
            const sqlite = yield* SqliteClient;
            const repository = new MarketDataRepositorySQLite(sqlite.database);
            const candles = yield* repository.getCandles({
              exchange: args.exchange,
              symbol: args.symbol,
              timeframe: args.timeframe,
              limit: args.contextBars,
            });
            if (candles.length < MIN_CONTEXT_BARS) {
              return yield* Effect.fail(
                new MarketDataRepositoryError(
                  `only ${candles.length} candles found for ${args.exchange}:${args.symbol}:${args.timeframe}; need at least ${MIN_CONTEXT_BARS}`,
                ),
              );
            }
            return {
              request: buildTimesFmRequest({ ...args, candles }),
              candles,
            };
          });
      const request = requestAndCandles.request;
      const worker = new TimesFmWorker(workerOptions(args));
      const response = yield* Effect.tryPromise({
        try: () => worker.request(request),
        catch: (error) =>
          error instanceof Error ? error : new Error(String(error)),
      }).pipe(Effect.ensuring(Effect.promise(() => worker.close())));

      if (args.dryRun) {
        if (!("validated" in response)) {
          return yield* Effect.fail(
            new Error(
              "TimesFM validation worker returned a forecast unexpectedly",
            ),
          );
        }
        const result = {
          ok: true,
          validated: true,
          seriesCount: response.seriesCount,
          horizon: response.horizon,
          project: "services/timesfm-forecast",
          python: "CPython 3.12 via uv",
        };
        yield* Console.log(
          args.json ? JSON.stringify(result) : JSON.stringify(result, null, 2),
        );
        return result;
      }

      if (!("forecasts" in response)) {
        return yield* Effect.fail(
          new Error("TimesFM worker did not return forecasts"),
        );
      }
      const candles = requestAndCandles.candles;
      if (candles === undefined) {
        return yield* Effect.fail(
          new Error("forecast candles were not loaded"),
        );
      }
      const record = response.forecasts[0];
      if (record === undefined) {
        return yield* Effect.fail(
          new Error("TimesFM returned no forecast record"),
        );
      }
      const summary = summarizeForecast(
        { ...args, candles },
        request,
        record,
        response.latencyMs,
      );
      if (args.json) {
        yield* Console.log(JSON.stringify(summary));
      } else {
        yield* Console.log(
          `TimesFM-3 shadow forecast ${summary.exchange}:${summary.symbol}:${summary.timeframe}`,
        );
        yield* Console.log(
          `context=${summary.contextBars} horizon=${summary.horizon} last=${summary.lastClose} at=${summary.lastTimestamp} inference=${summary.latencyMs.toFixed(1)}ms`,
        );
        yield* Console.log(
          `advisory=${summary.direction} terminal point=${summary.terminalPointReturnPct.toFixed(3)}% q10=${summary.terminalQ10ReturnPct?.toFixed(3) ?? "n/a"}% q90=${summary.terminalQ90ReturnPct?.toFixed(3) ?? "n/a"}%`,
        );
        yield* Console.log(
          "shadow-only: this forecast is not connected to order placement or risk decisions",
        );
      }
      return summary;
    }).pipe(Effect.provide(dbLayer)),
  ).pipe(
    Command.withDescription(
      "Run a research-only TimesFM-3 shadow forecast from stored candles",
    ),
  );
}
