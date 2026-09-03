#!/usr/bin/env bun

/**
 * Research-only TimesFM overlay scan for the validated grid candidate.
 *
 * TimesFM is scored at causal origins, then its forecast is held until the
 * next origin. The resulting overlay only controls new grid entries; open
 * positions still use the ordinary grid exits. This script never writes the
 * database, creates orders, or changes risk state.
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
  type TimesFmEvaluationOrigin,
} from "../src/scalping/timesfm-evaluation.js";
import { runGridBacktest, type GridOptions } from "../src/scalping/grid.js";
import {
  buildTimesFmEntryOverlay,
  type TimesFmGridForecast,
  type TimesFmGridPolicy,
} from "../src/scalping/timesfm-grid-filter.js";
import {
  candidateForSymbol,
  VALIDATED_BTC_GRID_CANDIDATE,
} from "../src/scalping/grid-candidate.js";

interface RawCandle {
  readonly open_price: number;
  readonly high_price: number;
  readonly low_price: number;
  readonly close_price: number;
  readonly volume: number;
  readonly timestamp: string;
}

interface ScoredForecast {
  readonly origin: TimesFmEvaluationOrigin;
  readonly forecast: TimesFmGridForecast;
}

interface PolicyResult {
  readonly policy: string;
  readonly totalReturnPct: number;
  readonly maxDrawdownPct: number;
  readonly totalTrades: number;
  readonly winRatePct: number;
  readonly profitFactor: number;
}

interface WindowSeriesResult {
  readonly policy: string;
  readonly aggregateReturnPct: number;
  readonly maxDrawdownPct: number;
  readonly totalTrades: number;
  readonly winRatePct: number;
  readonly profitFactor: number;
  readonly profitableWindowsPct: number;
  readonly windows: readonly PolicyResult[];
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

function listArg(name: string, fallback: readonly number[]): readonly number[] {
  const raw = stringArg(name, fallback.join(","));
  const values = raw
    .split(",")
    .map((value) => value.trim())
    .filter((value) => value.length > 0)
    .map(Number);
  if (values.length === 0 || !values.every(Number.isFinite)) {
    throw new Error(`${name} must be a comma-separated list of finite numbers`);
  }
  if (values.some((value) => value < 0)) {
    throw new Error(`${name} cannot contain negative values`);
  }
  return values;
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
  db.exec("PRAGMA busy_timeout = 30000;");
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

function returnFromLogForecast(
  forecastLogClose: number,
  originClose: number,
): number {
  const result = (Math.exp(forecastLogClose) / originClose - 1) * 100;
  if (!Number.isFinite(result))
    throw new Error("forecast return is not finite");
  return result;
}

function terminalForecast(
  record: TimesFmForecastRecord,
  originClose: number,
): TimesFmGridForecast {
  const forecast = record.forecast[0];
  const terminal = forecast?.at(-1);
  if (terminal === undefined)
    throw new Error(`empty forecast for ${record.id}`);
  const quantileRow = record.quantiles?.[0]?.at(-1);
  const q10Log = quantileRow?.[0];
  const q90Log = quantileRow?.at(-1);
  return {
    originIndex: Number(record.id.slice(record.id.lastIndexOf("-") + 1)),
    pointReturnPct: returnFromLogForecast(terminal, originClose),
    q10ReturnPct:
      q10Log === undefined ? null : returnFromLogForecast(q10Log, originClose),
    q90ReturnPct:
      q90Log === undefined ? null : returnFromLogForecast(q90Log, originClose),
  };
}

function makeOriginRequest(
  args: ReturnType<typeof parseArgs>,
  candles: readonly Candle[],
  originIndex: number,
): TimesFmForecastRequest {
  const context = candles.slice(
    originIndex - args.contextBars + 1,
    originIndex + 1,
  );
  const request = buildTimesFmRequest({
    exchange: args.exchange,
    symbol: args.symbol,
    timeframe: args.timeframe,
    horizon: args.horizon,
    useZnorm: args.useZnorm,
    symmetricAveraging: args.symmetricAveraging,
    volumeAsPastCovariate: args.volumeAsPastCovariate,
    candles: context,
  });
  const series = request.series[0];
  if (series === undefined) throw new Error("TimesFM request has no series");
  return {
    ...request,
    requestId: `timesfm-grid-filter-${originIndex}-${crypto.randomUUID()}`,
    series: [{ ...series, id: `origin-${originIndex}` }],
  };
}

async function scoreForecasts(
  args: ReturnType<typeof parseArgs>,
  candles: readonly Candle[],
  origins: readonly TimesFmEvaluationOrigin[],
  intervalMs: number,
): Promise<{
  readonly forecasts: readonly ScoredForecast[];
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
  const scored: ScoredForecast[] = [];
  let latencyMs = 0;
  try {
    for (let start = 0; start < origins.length; start += args.batchSize) {
      const batch = origins.slice(start, start + args.batchSize);
      const requests = batch.map((origin) =>
        makeOriginRequest(args, candles, origin.index),
      );
      const response = await worker.request({
        requestId: `timesfm-grid-filter-batch-${start}-${crypto.randomUUID()}`,
        horizon: args.horizon,
        intervalMs,
        series: requests.flatMap((request) => request.series),
        returnQuantiles: true,
        useSymmetricAveraging: args.symmetricAveraging,
        useZnorm: args.useZnorm,
      });
      if (!("forecasts" in response)) {
        throw new Error("TimesFM returned validation instead of forecasts");
      }
      latencyMs += response.latencyMs;
      const records = new Map(
        response.forecasts.map((record) => [record.id, record]),
      );
      for (const origin of batch) {
        const record = records.get(`origin-${origin.index}`);
        const source = candles[origin.index];
        if (record === undefined || source === undefined) {
          throw new Error(`missing forecast for origin ${origin.index}`);
        }
        scored.push({
          origin,
          forecast: {
            ...terminalForecast(record, source.close),
            originIndex: origin.index,
          },
        });
      }
    }
    return { forecasts: scored, latencyMs };
  } finally {
    await worker.close();
  }
}

function median(values: readonly number[]): number {
  const sorted = values.slice().sort((left, right) => left - right);
  if (sorted.length === 0) return 0;
  const middle = Math.floor(sorted.length / 2);
  return sorted.length % 2 === 0
    ? ((sorted[middle - 1] ?? 0) + (sorted[middle] ?? 0)) / 2
    : (sorted[middle] ?? 0);
}

function percentile(values: readonly number[], p: number): number {
  const sorted = values.slice().sort((left, right) => left - right);
  if (sorted.length === 0) return 0;
  const position = (sorted.length - 1) * p;
  const lower = Math.floor(position);
  const upper = Math.ceil(position);
  const lowerValue = sorted[lower] ?? 0;
  const upperValue = sorted[upper] ?? lowerValue;
  return lowerValue + (upperValue - lowerValue) * (position - lower);
}

function parseArgs() {
  const localFilesOnly = process.argv.includes("--local-files-only");
  return {
    exchange: stringArg("--exchange", "bybit-futures"),
    symbol: stringArg("--symbol", VALIDATED_BTC_GRID_CANDIDATE.symbol),
    timeframe: stringArg("--timeframe", VALIDATED_BTC_GRID_CANDIDATE.timeframe),
    bars: integerArg("--bars", 0),
    windowBars: integerArg("--window-bars", 0),
    contextBars: integerArg("--context-bars", 256),
    horizon: integerArg("--horizon", 12),
    stepBars: integerArg("--step-bars", 96),
    maxOrigins: integerArg("--max-origins", 0),
    feePct: numberArg("--fee-pct", VALIDATED_BTC_GRID_CANDIDATE.feePct),
    slippageBps: numberArg(
      "--slippage-bps",
      VALIDATED_BTC_GRID_CANDIDATE.slippageBps,
    ),
    checkpoint: stringArg("--checkpoint", "google/timesfm-3.0-pytorch"),
    device: stringArg("--device", "cpu"),
    batchSize: integerArg("--batch-size", 8),
    cacheDir: stringArg("--cache-dir", ""),
    localFilesOnly,
    torchThreads: integerArg("--torch-threads", 4),
    useZnorm: process.argv.includes("--use-znorm"),
    symmetricAveraging: !process.argv.includes("--no-symmetric-averaging"),
    volumeAsPastCovariate: process.argv.includes("--volume-as-past-covariate"),
    includeStress: !process.argv.includes("--no-stress"),
    json: process.argv.includes("--json"),
    pointThresholds: listArg("--point-thresholds", [0.25, 0.5, 0.75, 1]),
    bandThresholds: listArg("--band-thresholds", [0.5, 1, 1.5, 2, 3]),
  };
}

function baseGridOptions(args: ReturnType<typeof parseArgs>): GridOptions {
  const candidate =
    candidateForSymbol(args.symbol) ?? VALIDATED_BTC_GRID_CANDIDATE;
  return {
    gridStepPct: candidate.gridStepPct,
    gridMaxGrids: candidate.gridMaxGrids,
    gridPauseAfterLossBars: candidate.gridPauseAfterLossBars,
    feePct: args.feePct,
    slippageBps: args.slippageBps,
    initialCapital: 10_000,
    trendFilterPeriod: candidate.trendFilterPeriod,
    leverage: candidate.leverage,
    onlyWithTrend: candidate.onlyWithTrend,
    targetRatio: candidate.targetRatio,
    chopGateAdxThreshold: candidate.chopGateAdx,
  };
}

function policyName(policy: TimesFmGridPolicy): string {
  const threshold = policy.thresholdPct.toFixed(2);
  if (policy.kind === "standAsidePoint")
    return `stand-aside-point>=${threshold}%`;
  if (policy.kind === "standAsideBand")
    return `stand-aside-band>=${threshold}%`;
  return `${policy.contrarian ? "contrarian" : "directional"}-point>=${threshold}%`;
}

function runPolicy(
  candles: readonly Candle[],
  forecasts: readonly ScoredForecast[],
  policyNameValue: string,
  policy: TimesFmGridPolicy | null,
  options: GridOptions,
): PolicyResult {
  const overlay =
    policy === null
      ? undefined
      : buildTimesFmEntryOverlay(
          candles.length,
          forecasts.map(({ forecast }) => forecast),
          policy,
        );
  const optionsWithOverlay: GridOptions =
    overlay === undefined
      ? options
      : { ...options, entryDirectionByBar: overlay };
  const result = runGridBacktest(candles, optionsWithOverlay);
  return {
    policy: policyNameValue,
    totalReturnPct: result.totalReturnPct,
    maxDrawdownPct: result.maxDrawdownPct,
    totalTrades: result.totalTrades,
    winRatePct: result.winRate,
    profitFactor: result.profitFactor,
  };
}

function runWindowSeries(
  candles: readonly Candle[],
  fullOverlay: readonly ("long" | "short" | "flat" | undefined)[] | undefined,
  policyNameValue: string,
  options: GridOptions,
  windowBars: number,
): WindowSeriesResult {
  let capital = options.initialCapital;
  let maxDrawdownPct = 0;
  let totalTrades = 0;
  let winningTrades = 0;
  let grossProfit = 0;
  let grossLoss = 0;
  const windows: PolicyResult[] = [];
  for (
    let start = 0;
    start + windowBars <= candles.length;
    start += windowBars
  ) {
    const end = start + windowBars;
    const slice = candles.slice(start, end);
    const overlay = fullOverlay?.slice(start, end);
    const windowOptions: GridOptions =
      overlay === undefined
        ? { ...options, initialCapital: capital }
        : {
            ...options,
            initialCapital: capital,
            entryDirectionByBar: overlay,
          };
    const result = runGridBacktest(slice, windowOptions);
    capital *= 1 + result.totalReturnPct / 100;
    maxDrawdownPct = Math.max(maxDrawdownPct, result.maxDrawdownPct);
    totalTrades += result.totalTrades;
    winningTrades += result.trades.filter((trade) => trade.win).length;
    grossProfit += result.trades
      .filter((trade) => trade.pnlPct > 0)
      .reduce((sum, trade) => sum + trade.pnlPct, 0);
    grossLoss += result.trades
      .filter((trade) => trade.pnlPct < 0)
      .reduce((sum, trade) => sum + Math.abs(trade.pnlPct), 0);
    windows.push({
      policy: policyNameValue,
      totalReturnPct: result.totalReturnPct,
      maxDrawdownPct: result.maxDrawdownPct,
      totalTrades: result.totalTrades,
      winRatePct: result.winRate,
      profitFactor: result.profitFactor,
    });
  }
  const profitableWindows = windows.filter(
    (window) => window.totalReturnPct > 0,
  ).length;
  return {
    policy: policyNameValue,
    aggregateReturnPct:
      options.initialCapital > 0
        ? ((capital - options.initialCapital) / options.initialCapital) * 100
        : 0,
    maxDrawdownPct,
    totalTrades,
    winRatePct: totalTrades > 0 ? (winningTrades / totalTrades) * 100 : 0,
    profitFactor:
      grossLoss > 0 ? grossProfit / grossLoss : grossProfit > 0 ? Infinity : 0,
    profitableWindowsPct:
      windows.length > 0 ? (profitableWindows / windows.length) * 100 : 0,
    windows,
  };
}

function policies(args: ReturnType<typeof parseArgs>): readonly {
  readonly name: string;
  readonly policy: TimesFmGridPolicy;
}[] {
  return [
    ...args.pointThresholds.map((thresholdPct) => ({
      name: policyName({ kind: "standAsidePoint", thresholdPct }),
      policy: { kind: "standAsidePoint", thresholdPct } as const,
    })),
    ...args.bandThresholds.map((thresholdPct) => ({
      name: policyName({ kind: "standAsideBand", thresholdPct }),
      policy: { kind: "standAsideBand", thresholdPct } as const,
    })),
    ...args.pointThresholds.flatMap((thresholdPct) => [
      {
        name: policyName({ kind: "directionalPoint", thresholdPct }),
        policy: { kind: "directionalPoint", thresholdPct } as const,
      },
      {
        name: policyName({
          kind: "directionalPoint",
          thresholdPct,
          contrarian: true,
        }),
        policy: {
          kind: "directionalPoint",
          thresholdPct,
          contrarian: true,
        } as const,
      },
    ]),
  ];
}

function printResults(label: string, results: readonly PolicyResult[]): void {
  console.log(`\n=== ${label} ===`);
  for (const result of results) {
    console.log(
      `${result.policy.padEnd(32)} ret=${result.totalReturnPct.toFixed(2).padStart(8)}% dd=${result.maxDrawdownPct.toFixed(2).padStart(7)}% trades=${String(result.totalTrades).padStart(4)} win=${result.winRatePct.toFixed(1).padStart(5)}% pf=${result.profitFactor.toFixed(2).padStart(5)}`,
    );
  }
}

try {
  const args = parseArgs();
  const intervalMs = TIMEFRAME_MS.get(args.timeframe);
  if (intervalMs === undefined)
    throw new Error(`unsupported timeframe: ${args.timeframe}`);
  if (args.contextBars < 32)
    throw new Error("--context-bars must be at least 32");
  if (args.horizon < 1 || args.stepBars < 1 || args.maxOrigins < 0) {
    throw new Error(
      "--horizon and --step-bars must be positive; --max-origins cannot be negative",
    );
  }
  if (args.windowBars < 0) throw new Error("--window-bars cannot be negative");
  if (args.batchSize < 1 || args.torchThreads < 0) {
    throw new Error(
      "--batch-size must be positive and --torch-threads non-negative",
    );
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
  if (candles.length === 0)
    throw new Error(
      `no candles for ${args.exchange}:${args.symbol}:${args.timeframe}`,
    );
  assertRegularCandleSeries(candles, intervalMs);
  const origins = buildTimesFmEvaluationOrigins(
    candles,
    args.contextBars,
    args.horizon,
    args.stepBars,
    args.maxOrigins,
  );
  if (origins.length === 0)
    throw new Error("not enough candles for the requested evaluation");
  const scored = await scoreForecasts(args, candles, origins, intervalMs);
  const pointMagnitudes = scored.forecasts.map(({ forecast }) =>
    Math.abs(forecast.pointReturnPct),
  );
  const bandWidths = scored.forecasts
    .map(({ forecast }) =>
      forecast.q10ReturnPct === null || forecast.q90ReturnPct === null
        ? null
        : forecast.q90ReturnPct - forecast.q10ReturnPct,
    )
    .filter((value): value is number => value !== null);
  const featureSummary = {
    medianAbsPointReturnPct: median(pointMagnitudes),
    p90AbsPointReturnPct: percentile(pointMagnitudes, 0.9),
    medianBandWidthPct: median(bandWidths),
    p90BandWidthPct: percentile(bandWidths, 0.9),
  };
  const gridOptions = baseGridOptions(args);
  const stressOptions: GridOptions = {
    ...gridOptions,
    makerFillProb: 0.7,
    adverseSelection: true,
    takerExitFeePct: 0.06,
    fillSeed: 20260802,
  };
  const policySet = policies(args);
  const baseResults = [
    runPolicy(candles, scored.forecasts, "baseline", null, gridOptions),
    ...policySet.map(({ name, policy }) =>
      runPolicy(candles, scored.forecasts, name, policy, gridOptions),
    ),
  ];
  const stressResults = args.includeStress
    ? [
        runPolicy(candles, scored.forecasts, "baseline", null, stressOptions),
        ...policySet.map(({ name, policy }) =>
          runPolicy(candles, scored.forecasts, name, policy, stressOptions),
        ),
      ]
    : [];
  const forecasts = scored.forecasts.map(({ forecast }) => forecast);
  const walkForward =
    args.windowBars > 0
      ? {
          windowBars: args.windowBars,
          baseResults: [
            runWindowSeries(
              candles,
              undefined,
              "baseline",
              gridOptions,
              args.windowBars,
            ),
            ...policySet.map(({ name, policy }) =>
              runWindowSeries(
                candles,
                buildTimesFmEntryOverlay(candles.length, forecasts, policy),
                name,
                gridOptions,
                args.windowBars,
              ),
            ),
          ],
          stressResults: args.includeStress
            ? [
                runWindowSeries(
                  candles,
                  undefined,
                  "baseline",
                  stressOptions,
                  args.windowBars,
                ),
                ...policySet.map(({ name, policy }) =>
                  runWindowSeries(
                    candles,
                    buildTimesFmEntryOverlay(candles.length, forecasts, policy),
                    name,
                    stressOptions,
                    args.windowBars,
                  ),
                ),
              ]
            : [],
        }
      : null;
  const output = {
    ok: true,
    researchOnly: true,
    exchange: args.exchange,
    symbol: args.symbol,
    timeframe: args.timeframe,
    candleCount: candles.length,
    dataStart: candles[0]?.timestamp.toISOString(),
    dataEnd: candles.at(-1)?.timestamp.toISOString(),
    originCount: scored.forecasts.length,
    contextBars: args.contextBars,
    horizon: args.horizon,
    stepBars: args.stepBars,
    inferenceLatencyMs: scored.latencyMs,
    averageInferenceLatencyMs: scored.latencyMs / scored.forecasts.length,
    featureSummary,
    baseResults,
    stressResults,
    walkForward,
  };
  if (args.json) console.log(JSON.stringify(output));
  else {
    console.log(
      `TimesFM grid-filter research ${args.exchange}:${args.symbol}:${args.timeframe}`,
    );
    console.log(
      `candles=${candles.length} origins=${scored.forecasts.length} context=${args.contextBars} horizon=${args.horizon} step=${args.stepBars} inference=${scored.latencyMs.toFixed(1)}ms`,
    );
    console.log(
      `features: median|point|=${featureSummary.medianAbsPointReturnPct.toFixed(3)}% p90|point|=${featureSummary.p90AbsPointReturnPct.toFixed(3)}% median band=${featureSummary.medianBandWidthPct.toFixed(3)}% p90 band=${featureSummary.p90BandWidthPct.toFixed(3)}%`,
    );
    printResults("candidate costs", baseResults);
    if (args.includeStress)
      printResults("maker stress + adverse selection", stressResults);
    if (walkForward !== null) {
      console.log(`\n=== ${walkForward.windowBars}-bar disjoint windows ===`);
      for (const result of walkForward.baseResults) {
        console.log(
          `${result.policy.padEnd(32)} ret=${result.aggregateReturnPct.toFixed(2).padStart(8)}% dd=${result.maxDrawdownPct.toFixed(2).padStart(7)}% windows=${result.profitableWindowsPct.toFixed(1).padStart(5)}% trades=${String(result.totalTrades).padStart(4)} win=${result.winRatePct.toFixed(1).padStart(5)}% pf=${result.profitFactor.toFixed(2).padStart(5)}`,
        );
      }
      if (args.includeStress) {
        console.log("\n=== stress disjoint windows ===");
        for (const result of walkForward.stressResults) {
          console.log(
            `${result.policy.padEnd(32)} ret=${result.aggregateReturnPct.toFixed(2).padStart(8)}% dd=${result.maxDrawdownPct.toFixed(2).padStart(7)}% windows=${result.profitableWindowsPct.toFixed(1).padStart(5)}% trades=${String(result.totalTrades).padStart(4)} win=${result.winRatePct.toFixed(1).padStart(5)}% pf=${result.profitFactor.toFixed(2).padStart(5)}`,
          );
        }
      }
    }
    console.log(
      "research-only: TimesFM never places orders or changes risk state",
    );
  }
} catch (error) {
  console.error(error instanceof Error ? error.message : String(error));
  process.exitCode = 1;
}
