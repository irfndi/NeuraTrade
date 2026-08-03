import { Database } from "bun:sqlite";
import { homedir } from "node:os";
import { join } from "node:path";
import type { CandleLike } from "../scalping/types.js";
import { VALIDATED_BTC_GRID_CANDIDATE } from "../scalping/grid-candidate.js";
import {
  bootstrapBlockConfidence,
  READINESS_STRESS_SEEDS,
  validateGridEvidence,
  type GridValidationOk,
} from "../scalping/grid-validation.js";
import {
  DEFAULT_READINESS_THRESHOLDS,
  DEFAULT_STRATEGY_MANIFEST,
  evaluateRealMoneyReadiness,
  fingerprintStrategyManifest,
  serializeRealMoneyReadiness,
  type RealMoneyReadinessInput,
  type RealMoneyReadinessReport,
} from "../scalping/real-money-readiness.js";

export interface RealMoneyReadinessCliOptions {
  readonly home?: string;
  readonly now?: Date;
  readonly testFactory?: boolean;
  readonly parityFixture?: "golden";
}

export interface ParsedRealMoneyReadinessArgs {
  readonly exchange: string;
  readonly symbol: string;
  readonly timeframe: string;
}

interface RawCandle {
  readonly open_price: number;
  readonly high_price: number;
  readonly low_price: number;
  readonly close_price: number;
  readonly volume: number;
  readonly timestamp: string;
}

interface RawTrade {
  readonly fill_source: string | null;
  readonly entry_order_id: string | null;
  readonly exit_order_id: string | null;
  readonly entry_filled_qty_decimal: string | null;
  readonly exit_filled_qty_decimal: string | null;
  readonly entry_fee_decimal: string | null;
  readonly exit_fee_decimal: string | null;
  readonly realized_pnl_pct_decimal: string | null;
  readonly opened_at: string;
  readonly closed_at: string;
  readonly strategy_config_fingerprint: string | null;
  readonly cohort_id: string | null;
  readonly candidate_lock_at: string | null;
  readonly dataset_cutoff_at: string | null;
  readonly entry_opened_at: string | null;
  readonly execution_environment: string | null;
}

class ReadinessInfrastructureError extends Error {
  readonly _tag = "ReadinessInfrastructureError" as const;
}

type ParseResult =
  | { readonly kind: "ok"; readonly args: ParsedRealMoneyReadinessArgs }
  | { readonly kind: "error"; readonly message: string };

export function parseRealMoneyReadinessArgs(
  argv: readonly string[],
): ParseResult {
  const values = {
    exchange: "bitget-futures",
    symbol: "BTC/USDT:USDT",
    timeframe: "15m",
  };
  for (let index = 0; index < argv.length; index += 1) {
    const token = argv[index];
    if (token === undefined) continue;
    if (!token.startsWith("--")) {
      return { kind: "error", message: `unexpected argument: ${token}` };
    }
    const name = token.slice(2);
    if (name === "parity-fixture") {
      return { kind: "error", message: "--parity-fixture is test-runner-only" };
    }
    const next = argv[index + 1];
    if (next === undefined || next.startsWith("--")) {
      return { kind: "error", message: `missing value for --${name}` };
    }
    if (name === "exchange" || name === "symbol" || name === "timeframe") {
      values[name] = next;
      index += 1;
      continue;
    }
    return { kind: "error", message: `unknown option: --${name}` };
  }
  return { kind: "ok", args: values };
}

function errorReport(message: string): RealMoneyReadinessReport {
  return {
    schemaVersion: "real-money-readiness/v1",
    status: "ERROR",
    exitCode: 2,
    candidateFingerprint: "",
    thresholds: DEFAULT_READINESS_THRESHOLDS,
    gates: [],
    failedGateIds: [],
    errors: [message],
    metrics: {
      prospective: {
        completeTradeCount: 0,
        durationDays: 0,
        expectancyPct: "0",
        confidenceLowerBoundPct: "0",
        maximumDrawdownPct: "0",
        allTradesHaveLiveFillEvidence: false,
      },
      historical: {
        completeWindows: 0,
        profitableWindowPct: 0,
        compoundedReturnPct: "0",
        maximumDrawdownPct: "0",
        totalTrades: 0,
      },
      confidence: {
        sampleCount: 0,
        lowerBoundPct: "0",
        upperBoundPct: "0",
        resamples: 0,
        blockLength: 5,
        seed: 0,
      },
      stress: { returnPct: "0", lowerBoundPct: "0", seeds: [] },
      provenance: {
        valid: false,
        fingerprint: "",
        expectedFingerprint: "",
        cohortId: "",
        candidateLock: "",
        datasetCutoff: "",
        earliestEntry: "",
        latestClose: "",
        queriedRows: 0,
        expectedRows: 0,
      },
      dataQuality: {
        valid: false,
        candleCount: 0,
        completeWindows: 0,
        latestCandle: "",
      },
      evaluatedAt: new Date(0).toISOString(),
    },
  };
}

function databasePath(home: string): string {
  return join(home, "data", "neuratrade.db");
}

function tableColumns(db: Database, table: string): ReadonlySet<string> {
  const rows = db
    .query<{ readonly name: string }, []>(`PRAGMA table_info(${table})`)
    .all();
  return new Set(rows.map((row) => row.name));
}

function requireSchema(db: Database): void {
  const requiredTables = [
    "exchanges",
    "trading_pairs",
    "ohlcv_data",
    "grid_paper_trades",
  ] as const;
  for (const table of requiredTables) {
    const columns = tableColumns(db, table);
    if (columns.size === 0) {
      throw new ReadinessInfrastructureError(
        `missing readiness table: ${table}`,
      );
    }
  }
  const tradeColumns = tableColumns(db, "grid_paper_trades");
  const requiredTradeColumns = [
    "strategy_config_fingerprint",
    "cohort_id",
    "candidate_lock_at",
    "dataset_cutoff_at",
    "entry_opened_at",
    "execution_environment",
  ] as const;
  for (const column of requiredTradeColumns) {
    if (!tradeColumns.has(column)) {
      throw new ReadinessInfrastructureError(
        `missing readiness column: ${column}`,
      );
    }
  }
}

function readCandles(
  db: Database,
  args: ParsedRealMoneyReadinessArgs,
): readonly CandleLike[] {
  const rows = db
    .query<RawCandle, string[]>(
      `SELECT o.open_price, o.high_price, o.low_price, o.close_price, o.volume, o.timestamp
       FROM ohlcv_data o
       JOIN exchanges e ON e.id = o.exchange_id
       JOIN trading_pairs p ON p.id = o.trading_pair_id
       WHERE e.name = ? AND p.symbol = ? AND o.timeframe = ?
       ORDER BY o.timestamp ASC`,
    )
    .all(args.exchange, args.symbol, args.timeframe);
  return rows.map((row) => ({
    open: row.open_price,
    high: row.high_price,
    low: row.low_price,
    close: row.close_price,
    volume: row.volume,
    timestamp: new Date(row.timestamp),
  }));
}

function readTrades(
  db: Database,
  args: ParsedRealMoneyReadinessArgs,
): readonly RawTrade[] {
  return db
    .query<RawTrade, string[]>(
      `SELECT fill_source, entry_order_id, exit_order_id,
              entry_filled_qty_decimal, exit_filled_qty_decimal,
              entry_fee_decimal, exit_fee_decimal, realized_pnl_pct_decimal,
              opened_at, closed_at, strategy_config_fingerprint, cohort_id,
              candidate_lock_at, dataset_cutoff_at, entry_opened_at,
              execution_environment
       FROM grid_paper_trades
       WHERE exchange = ? AND symbol = ? AND timeframe = ?
       ORDER BY closed_at ASC, id ASC`,
    )
    .all(args.exchange, args.symbol, args.timeframe);
}

function completeTrade(trade: RawTrade): boolean {
  return (
    trade.fill_source === "live" &&
    trade.entry_order_id !== null &&
    trade.exit_order_id !== null &&
    trade.entry_filled_qty_decimal !== null &&
    trade.exit_filled_qty_decimal !== null &&
    trade.entry_fee_decimal !== null &&
    trade.exit_fee_decimal !== null &&
    trade.realized_pnl_pct_decimal !== null
  );
}

function demoDrawdown(trades: readonly RawTrade[]): string {
  let capital = 100;
  let peak = 100;
  let maximum = 0;
  for (const trade of trades) {
    const pnl = Number(trade.realized_pnl_pct_decimal ?? "0");
    capital *= 1 + pnl / 100;
    peak = Math.max(peak, capital);
    maximum = Math.max(maximum, peak > 0 ? ((peak - capital) / peak) * 100 : 0);
  }
  return maximum.toString();
}

function buildInput(
  candles: readonly CandleLike[],
  trades: readonly RawTrade[],
  now: Date,
  parityPassed: boolean,
): RealMoneyReadinessInput {
  const complete = trades.filter(completeTrade);
  const pValues = complete.map(
    (trade) => trade.realized_pnl_pct_decimal ?? "0",
  );
  const confidence =
    pValues.length >= 5
      ? bootstrapBlockConfidence(pValues, 20260802)
      : {
          lowerBoundPct: 0,
          upperBoundPct: 0,
          resamples: 0,
          blockLength: 5,
          seed: 0,
          sampleCount: pValues.length,
        };
  const totalPnl = pValues.reduce((sum, value) => sum + Number(value), 0);
  const earliest = complete.at(0)?.opened_at ?? "1970-01-01T00:00:00.000Z";
  const latest = complete.at(-1)?.closed_at ?? "1970-01-01T00:00:00.000Z";
  const firstProvenance = trades.at(0);
  const expectedFingerprint = fingerprintStrategyManifest(
    DEFAULT_STRATEGY_MANIFEST,
  );
  const grid = validateGridEvidence(candles, {
    now,
    grid: {
      gridStepPct: VALIDATED_BTC_GRID_CANDIDATE.gridStepPct,
      gridMaxGrids: VALIDATED_BTC_GRID_CANDIDATE.gridMaxGrids,
      gridPauseAfterLossBars:
        VALIDATED_BTC_GRID_CANDIDATE.gridPauseAfterLossBars,
      feePct: VALIDATED_BTC_GRID_CANDIDATE.feePct,
      slippageBps: VALIDATED_BTC_GRID_CANDIDATE.slippageBps,
      initialCapital: 100,
      trendFilterPeriod: VALIDATED_BTC_GRID_CANDIDATE.trendFilterPeriod,
      leverage: VALIDATED_BTC_GRID_CANDIDATE.leverage,
      positionFraction: VALIDATED_BTC_GRID_CANDIDATE.maxPositionSizePct / 100,
      chopGateAdxThreshold: VALIDATED_BTC_GRID_CANDIDATE.chopGateAdx,
      targetRatio: VALIDATED_BTC_GRID_CANDIDATE.targetRatio,
      onlyWithTrend: VALIDATED_BTC_GRID_CANDIDATE.onlyWithTrend,
    },
    executionParityPassed: parityPassed,
  });
  const dataQuality = grid.dataQuality;
  const gridOk: GridValidationOk | null = grid.kind === "ok" ? grid : null;
  const historical = gridOk?.historical ?? {
    completeWindows: dataQuality.completeWindows,
    profitableWindowPct: 0,
    compoundedReturnPct: 0,
    maximumDrawdownPct: 0,
    totalTrades: 0,
  };
  const fixedConfidence = gridOk?.confidence ?? {
    sampleCount: 0,
    lowerBoundPct: 0,
    upperBoundPct: 0,
    resamples: 0,
    blockLength: 5,
    seed: 0,
  };
  const stress = gridOk?.stress ?? {
    worstReturnPct: 0,
    worstLowerBoundPct: 0,
    seeds: [...READINESS_STRESS_SEEDS],
  };
  const candidateLock = firstProvenance?.candidate_lock_at ?? "";
  const datasetCutoff = firstProvenance?.dataset_cutoff_at ?? "";
  const provenanceFingerprint =
    firstProvenance?.strategy_config_fingerprint ?? "";
  return {
    prospectiveEvidence: {
      completeTradeCount: complete.length,
      durationDays:
        (Date.parse(latest) - Date.parse(earliest)) / (24 * 60 * 60 * 1000),
      expectancyPct:
        pValues.length > 0 ? (totalPnl / pValues.length).toString() : "0",
      confidenceLowerBoundPct: confidence.lowerBoundPct.toString(),
      maximumDrawdownPct: demoDrawdown(complete),
      allTradesHaveLiveFillEvidence:
        complete.length === trades.length && trades.length > 0,
    },
    historicalRobustness: {
      completeWindows: dataQuality.completeWindows,
      profitableWindowPct: historical.profitableWindowPct,
      compoundedReturnPct: historical.compoundedReturnPct.toString(),
      maximumDrawdownPct: historical.maximumDrawdownPct.toString(),
      totalTrades: historical.totalTrades,
    },
    confidence: {
      sampleCount: fixedConfidence.sampleCount,
      lowerBoundPct: fixedConfidence.lowerBoundPct.toString(),
      upperBoundPct: fixedConfidence.upperBoundPct.toString(),
      resamples: fixedConfidence.resamples,
      blockLength: fixedConfidence.blockLength,
      seed: fixedConfidence.seed,
    },
    executionParity: {
      passed: parityPassed,
      protocolVersion: "execution-parity/v1",
      checks: parityPassed
        ? [
            "trigger-bar",
            "order-type",
            "fill-price",
            "fees",
            "slippage",
            "quantity",
            "exit-reason",
            "pnl",
          ]
        : [],
    },
    stress: {
      returnPct: stress.worstReturnPct?.toString() ?? "0",
      lowerBoundPct: stress.worstLowerBoundPct?.toString() ?? "0",
      seeds: stress.seeds,
    },
    provenance: {
      valid:
        trades.length > 0 &&
        trades.every(
          (trade) =>
            trade.strategy_config_fingerprint === expectedFingerprint &&
            trade.cohort_id !== null &&
            trade.candidate_lock_at !== null &&
            trade.dataset_cutoff_at !== null &&
            trade.entry_opened_at !== null &&
            trade.execution_environment === "bitget-demo",
        ),
      fingerprint: provenanceFingerprint,
      expectedFingerprint,
      cohortId: firstProvenance?.cohort_id ?? "",
      candidateLock,
      datasetCutoff,
      earliestEntry: firstProvenance?.entry_opened_at ?? earliest,
      latestClose: latest,
      queriedRows: trades.length,
      expectedRows: trades.length,
    },
    dataQuality: {
      valid: dataQuality.valid && gridOk !== null,
      candleCount: dataQuality.candleCount,
      completeWindows: dataQuality.completeWindows,
      latestCandle: dataQuality.latestCandle?.toISOString() ?? "",
    },
    evaluatedAt: now.toISOString(),
    manifest: DEFAULT_STRATEGY_MANIFEST,
  };
}

function executeReadiness(
  args: ParsedRealMoneyReadinessArgs,
  options: RealMoneyReadinessCliOptions,
): RealMoneyReadinessReport {
  const home =
    options.home ??
    process.env.NEURATRADE_HOME ??
    join(homedir(), ".neuratrade");
  const db = new Database(databasePath(home), {
    readonly: true,
    create: false,
  });
  try {
    requireSchema(db);
    const now = options.now ?? new Date();
    const candles = readCandles(db, args);
    const trades = readTrades(db, args);
    return evaluateRealMoneyReadiness(
      buildInput(candles, trades, now, options.parityFixture === "golden"),
    );
  } finally {
    db.close();
  }
}

export function helpText(): string {
  return [
    "Read-only real-money readiness evidence gate",
    "",
    "Usage: scalp real-money-readiness [options]",
    "",
    "Reads persisted SQLite candles and fingerprinted Bitget demo fills only.",
    "It never places orders, reads exchange credentials, or enables live mode.",
    "Current public-data evidence is expected to FAIL until demo and parity gates qualify.",
    "",
    "Options:",
    "  --exchange <text>   Candle exchange (default: bitget-futures)",
    "  --symbol <text>     Candidate symbol (default: BTC/USDT:USDT)",
    "  --timeframe <text>  Candidate timeframe (default: 15m)",
  ].join("\n");
}

export function versionText(): string {
  return "real-money-readiness/v1";
}

export function runRealMoneyReadiness(
  argv: readonly string[],
  options: RealMoneyReadinessCliOptions = {},
): { readonly report: RealMoneyReadinessReport; readonly exitCode: number } {
  if (
    !options.testFactory &&
    (options.parityFixture !== undefined ||
      process.env.NEURATRADE_READINESS_PARITY_FIXTURE !== undefined)
  ) {
    return {
      report: errorReport(
        "test-only parity fixture is unavailable in production",
      ),
      exitCode: 2,
    };
  }
  const commandArgs = options.testFactory
    ? argv.filter((token) => token !== "--parity-fixture" && token !== "golden")
    : argv;
  const parsed = parseRealMoneyReadinessArgs(commandArgs);
  if (parsed.kind === "error") {
    return { report: errorReport(parsed.message), exitCode: 2 };
  }
  try {
    const report = executeReadiness(parsed.args, options);
    return { report, exitCode: report.exitCode };
  } catch (error) {
    const message = error instanceof Error ? error.message : String(error);
    return { report: errorReport(message), exitCode: 2 };
  }
}

export function serializeReadinessResult(
  result: ReturnType<typeof runRealMoneyReadiness>,
): string {
  return serializeRealMoneyReadiness(result.report);
}
