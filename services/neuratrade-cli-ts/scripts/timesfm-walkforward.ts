#!/usr/bin/env bun

/**
 * Local-only TimesFM-3 walk-forward evaluation.
 *
 * Every request uses candles through one origin and scores only the future
 * close at the requested horizon. Forecasts are advisory diagnostics; this
 * script never creates orders or changes paper/live state.
 */
import { Database } from "bun:sqlite";
import { join } from "node:path";
import type { Candle } from "../src/market-data/types.js";
import { buildTimesFmRequest } from "../src/cli/timesfm.js";
import {
  TimesFmWorker,
  type TimesFmForecastRecord,
  type TimesFmForecastRequest,
} from "../src/services/timesfm-client.js";
import {
  assertRegularCandleSeries,
  buildTimesFmEvaluationOrigins,
  evaluateTimesFmForecasts,
  type TimesFmEvaluationForecast,
} from "../src/scalping/timesfm-evaluation.js";

interface RawCandle {
  readonly open_price: number;
  readonly high_price: number;
  readonly low_price: number;
  readonly close_price: number;
  readonly volume: number;
  readonly timestamp: string;
}

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

function stringArg(name: string, fallback: string): string {
  const index = process.argv.indexOf(name);
  return index >= 0 && index + 1 < process.argv.length
    ? (process.argv[index + 1] ?? fallback)
    : fallback;
}

function numberArg(name: string, fallback: number): number {
  const value = Number(stringArg(name, String(fallback)));
  if (!Number.isFinite(value)) throw new Error(`${name} must be finite`);
  return value;
}

function integerArg(name: string, fallback: number): number {
  const value = numberArg(name, fallback);
  if (!Number.isInteger(value)) throw new Error(`${name} must be an integer`);
  return value;
}

function parseArgs() {
  const horizon = integerArg("--horizon", 12);
  return {
    exchange: stringArg("--exchange", "bybit-futures"),
    symbol: stringArg("--symbol", "BTC/USDT:USDT"),
    timeframe: stringArg("--timeframe", "1m"),
    contextBars: integerArg("--context-bars", 256),
    horizon,
    stepBars: integerArg("--step-bars", horizon),
    maxOrigins: integerArg("--max-origins", 96),
    bars: integerArg("--bars", 0),
    feePct: numberArg("--fee-pct", 0.06),
    slippageBps: numberArg("--slippage-bps", 2),
    checkpoint: stringArg("--checkpoint", "google/timesfm-3.0-pytorch"),
    device: stringArg("--device", "cpu"),
    batchSize: integerArg("--batch-size", 4),
    cacheDir: stringArg("--cache-dir", ""),
    localFilesOnly: process.argv.includes("--local-files-only"),
    torchThreads: integerArg("--torch-threads", 0),
    useZnorm: process.argv.includes("--use-znorm"),
    symmetricAveraging: !process.argv.includes("--no-symmetric-averaging"),
    volumeAsPastCovariate: process.argv.includes("--volume-as-past-covariate"),
    json: process.argv.includes("--json"),
  };
}

function parseTimestamp(value: string): Date {
  const normalized =
    value.endsWith("Z") || value.includes("+")
      ? value
      : `${value.replace(" ", "T")}Z`;
  const timestamp = new Date(normalized);
  if (!Number.isFinite(timestamp.getTime())) {
    throw new Error(`invalid candle timestamp: ${value}`);
  }
  return timestamp;
}

function loadCandles(
  homeDir: string,
  exchange: string,
  symbol: string,
  timeframe: string,
  bars: number,
): Candle[] {
  if (bars < 0) throw new Error("--bars cannot be negative");
  const db = new Database(join(homeDir, "data", "neuratrade.db"), {
    readonly: true,
  });
  try {
    const limit = bars > 0 ? " LIMIT ?" : "";
    const params: (string | number)[] = [exchange, symbol, timeframe];
    if (bars > 0) params.push(bars);
    const rows = db
      .query(
        `SELECT o.open_price, o.high_price, o.low_price, o.close_price, o.volume, o.timestamp
         FROM ohlcv_data o
         JOIN exchanges e ON e.id = o.exchange_id
         JOIN trading_pairs tp ON tp.id = o.trading_pair_id
         WHERE e.name = ? AND tp.symbol = ? AND o.timeframe = ?
         ORDER BY julianday(o.timestamp) DESC${limit}`,
      )
      .all(...params) as RawCandle[];
    return rows
      .slice()
      .reverse()
      .map((row) => ({
        exchange,
        symbol,
        timeframe,
        open: row.open_price,
        high: row.high_price,
        low: row.low_price,
        close: row.close_price,
        volume: row.volume,
        timestamp: parseTimestamp(row.timestamp),
      }));
  } finally {
    db.close();
  }
}

function makeOriginRequest(
  args: ReturnType<typeof parseArgs>,
  candles: readonly Candle[],
  originIndex: number,
  intervalMs: number,
): TimesFmForecastRequest {
  const context = candles.slice(
    originIndex - args.contextBars + 1,
    originIndex + 1,
  );
  const single = buildTimesFmRequest({
    exchange: args.exchange,
    symbol: args.symbol,
    timeframe: args.timeframe,
    horizon: args.horizon,
    useZnorm: args.useZnorm,
    symmetricAveraging: args.symmetricAveraging,
    volumeAsPastCovariate: args.volumeAsPastCovariate,
    candles: context,
  });
  const baseSeries = single.series[0];
  if (baseSeries === undefined) {
    throw new Error("failed to build TimesFM series");
  }
  const series = { ...baseSeries, id: `origin-${originIndex}` };
  return {
    requestId: `timesfm-walkforward-${originIndex}-${crypto.randomUUID()}`,
    horizon: args.horizon,
    intervalMs,
    series: [series],
    returnQuantiles: true,
    useSymmetricAveraging: args.symmetricAveraging,
    useZnorm: args.useZnorm,
  };
}

async function evaluate(
  args: ReturnType<typeof parseArgs>,
  candles: readonly Candle[],
  origins: ReturnType<typeof buildTimesFmEvaluationOrigins>,
  intervalMs: number,
): Promise<{
  readonly forecasts: readonly TimesFmEvaluationForecast[];
  readonly requestCount: number;
  readonly latencyMs: number;
}> {
  const worker = new TimesFmWorker({
    checkpoint: args.checkpoint,
    device: args.device,
    batchSize: args.batchSize,
    cacheDir: args.cacheDir.length > 0 ? args.cacheDir : undefined,
    localFilesOnly: args.localFilesOnly,
    torchThreads: args.torchThreads,
  });
  const forecasts: TimesFmEvaluationForecast[] = [];
  let requestCount = 0;
  let latencyMs = 0;
  try {
    for (let start = 0; start < origins.length; start += args.batchSize) {
      const batch = origins.slice(start, start + args.batchSize);
      const requests = batch.map((origin) =>
        makeOriginRequest(args, candles, origin.index, intervalMs),
      );
      const request: TimesFmForecastRequest = {
        requestId: `timesfm-walkforward-batch-${start}-${crypto.randomUUID()}`,
        horizon: args.horizon,
        intervalMs,
        series: requests.flatMap((item) => item.series),
        returnQuantiles: true,
        useSymmetricAveraging: args.symmetricAveraging,
        useZnorm: args.useZnorm,
      };
      const response = await worker.request(request);
      if (!("forecasts" in response)) {
        throw new Error(
          "TimesFM worker returned validation instead of forecasts",
        );
      }
      requestCount += 1;
      latencyMs += response.latencyMs;
      const records = new Map<string, TimesFmForecastRecord>(
        response.forecasts.map((record) => [record.id, record]),
      );
      for (const origin of batch) {
        const record = records.get(origin.id);
        if (record === undefined) {
          throw new Error(`TimesFM returned no record for ${origin.id}`);
        }
        forecasts.push({ origin, record });
      }
    }
    return { forecasts, requestCount, latencyMs };
  } finally {
    await worker.close();
  }
}

function printReport(output: {
  readonly exchange: string;
  readonly symbol: string;
  readonly timeframe: string;
  readonly candleCount: number;
  readonly originCount: number;
  readonly contextBars: number;
  readonly horizon: number;
  readonly stepBars: number;
  readonly requestCount: number;
  readonly totalInferenceLatencyMs: number;
  readonly report: ReturnType<typeof evaluateTimesFmForecasts>;
}): void {
  const { model, baseline } = output.report;
  console.log(
    `TimesFM-3 walk-forward ${output.exchange}:${output.symbol}:${output.timeframe}`,
  );
  console.log(
    `candles=${output.candleCount} origins=${output.originCount} context=${output.contextBars} horizon=${output.horizon} step=${output.stepBars} requests=${output.requestCount} inference=${output.totalInferenceLatencyMs.toFixed(1)}ms`,
  );
  console.log(
    `model: trades=${model.trades} coverage=${model.coveragePct.toFixed(1)}% win=${model.winRatePct.toFixed(1)}% PF=${model.profitFactor?.toFixed(2) ?? "n/a"} net=${model.netReturnPct.toFixed(3)}% MAE=${model.meanAbsoluteErrorPct.toFixed(3)}% direction=${model.directionAccuracyPct.toFixed(1)}%`,
  );
  console.log(
    `baseline: trades=${baseline.trades} coverage=${baseline.coveragePct.toFixed(1)}% win=${baseline.winRatePct.toFixed(1)}% PF=${baseline.profitFactor?.toFixed(2) ?? "n/a"} net=${baseline.netReturnPct.toFixed(3)}%`,
  );
  console.log(
    "research-only: directional close-to-close diagnostic; no orders, risk state, or live positions are changed",
  );
}

try {
  const args = parseArgs();
  const intervalMs = TIMEFRAME_MS.get(args.timeframe);
  if (intervalMs === undefined) {
    throw new Error(`unsupported timeframe: ${args.timeframe}`);
  }
  if (args.contextBars < 32) {
    throw new Error("--context-bars must be at least 32");
  }
  if (args.batchSize < 1 || args.torchThreads < 0) {
    throw new Error(
      "--batch-size must be positive and --torch-threads non-negative",
    );
  }
  if (args.horizon < 1 || args.stepBars < 1 || args.maxOrigins < 0) {
    throw new Error(
      "--horizon and --step-bars must be positive; --max-origins cannot be negative",
    );
  }
  if (args.feePct < 0 || args.slippageBps < 0) {
    throw new Error("--fee-pct and --slippage-bps cannot be negative");
  }
  const homeDir =
    process.env.NEURATRADE_HOME ?? join(process.env.HOME ?? ".", ".neuratrade");
  const candles = loadCandles(
    homeDir,
    args.exchange,
    args.symbol,
    args.timeframe,
    args.bars,
  );
  if (candles.length === 0) {
    throw new Error(
      `no candles for ${args.exchange}:${args.symbol}:${args.timeframe}`,
    );
  }
  assertRegularCandleSeries(candles, intervalMs);
  const origins = buildTimesFmEvaluationOrigins(
    candles,
    args.contextBars,
    args.horizon,
    args.stepBars,
    args.maxOrigins,
  );
  if (origins.length === 0) {
    throw new Error(
      `not enough candles for context=${args.contextBars}, horizon=${args.horizon}`,
    );
  }
  const scored = await evaluate(args, candles, origins, intervalMs);
  const frictionCostPct = args.feePct * 2 + (args.slippageBps * 2) / 100;
  const report = evaluateTimesFmForecasts(
    candles,
    scored.forecasts,
    frictionCostPct,
  );
  const output = {
    ok: true,
    researchOnly: true,
    exchange: args.exchange,
    symbol: args.symbol,
    timeframe: args.timeframe,
    candleCount: candles.length,
    dataStart: candles[0]?.timestamp.toISOString(),
    dataEnd: candles.at(-1)?.timestamp.toISOString(),
    originCount: origins.length,
    contextBars: args.contextBars,
    horizon: args.horizon,
    stepBars: args.stepBars,
    requestCount: scored.requestCount,
    totalInferenceLatencyMs: scored.latencyMs,
    averageInferenceLatencyMs: scored.latencyMs / scored.requestCount,
    checkpoint: args.checkpoint,
    device: args.device,
    report,
  };
  if (args.json) console.log(JSON.stringify(output));
  else printReport(output);
} catch (error) {
  console.error(error instanceof Error ? error.message : String(error));
  process.exitCode = 1;
}
