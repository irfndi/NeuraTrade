import { Database } from "bun:sqlite";
import { existsSync, readFileSync } from "node:fs";
import { homedir } from "node:os";
import { join } from "node:path";
import { z } from "zod";
import type { CandleLike } from "../scalping/types.js";
import {
  READINESS_COHORT_CANDIDATES,
  candidateForSymbol,
  type ValidatedGridCandidate,
} from "../scalping/grid-candidate.js";
import {
  bootstrapBlockConfidence,
  validateGridEvidence,
  type GridValidationOk,
} from "../scalping/grid-validation.js";
import {
  DEFAULT_READINESS_THRESHOLDS,
  EXECUTION_PARITY_CHECK_NAMES,
  evaluateRealMoneyReadiness,
  fingerprintStrategyManifest,
  serializeRealMoneyReadiness,
  strategyManifestFor,
  type CohortMemberReport,
  type ExecutionParityEvidence,
  type ProspectiveEvidence,
  type ProvenanceEvidence,
  type ReadinessGate,
  type ReadinessGateId,
  type RealMoneyReadinessInput,
  type RealMoneyReadinessReport,
} from "../scalping/real-money-readiness.js";

export interface RealMoneyReadinessCliOptions {
  readonly home?: string;
  readonly now?: Date;
  readonly testFactory?: boolean;
  readonly parityFixture?: "golden";
}

export interface RealMoneyReadinessResult {
  readonly report: RealMoneyReadinessReport;
  readonly exitCode: number;
}

export interface ParsedRealMoneyReadinessArgs {
  readonly exchange: string;
  /** Empty = full readiness cohort (READINESS_COHORT_CANDIDATES). */
  readonly symbols: readonly string[];
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

/** Mutable accumulator for the `--exchange`/`--symbol`/`--timeframe` CLI tokens. */
type ParsedValues = {
  exchange: string;
  symbols: string[];
  timeframe: string;
};

export function parseRealMoneyReadinessArgs(
  argv: readonly string[],
): ParseResult {
  const values: ParsedValues = {
    exchange: "bybit-futures",
    symbols: [],
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
    if (name === "exchange" || name === "timeframe") {
      values[name] = next;
      index += 1;
      continue;
    }
    if (name === "symbol") {
      values.symbols = [...values.symbols, next];
      index += 1;
      continue;
    }
    return { kind: "error", message: `unknown option: --${name}` };
  }
  return { kind: "ok", args: values };
}

function errorReport(message: string): RealMoneyReadinessReport {
  return {
    schemaVersion: "real-money-readiness/v2",
    status: "ERROR",
    exitCode: 2,
    candidateFingerprint: "",
    thresholds: DEFAULT_READINESS_THRESHOLDS,
    gates: [],
    failedGateIds: [],
    cohort: [],
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

const executionParityCheckSchema = z.object({
  name: z.string(),
  passed: z.boolean(),
  detail: z.string(),
});

const executionParityArtifactSchema = z.object({
  protocolVersion: z.string().optional(),
  checks: z.array(z.unknown()).optional(),
});

function absentExecutionParity(): ExecutionParityEvidence {
  return { passed: false, protocolVersion: "execution-parity/v1", checks: [] };
}

const goldenExecutionParity: ExecutionParityEvidence = {
  passed: true,
  protocolVersion: "execution-parity/v1",
  checks: EXECUTION_PARITY_CHECK_NAMES.map((name) => ({
    name,
    passed: true,
    detail: "test-factory golden fixture (not a measured run)",
  })),
};

/**
 * The execution-parity artifact (written by `scalp parity-replay`) is the
 * single source of truth for this gate in production. A CLI flag could be
 * grafted onto a passing report; a filesystem artifact produced by the replay
 * producer cannot pass unless all eight checks carry measured, non-failing
 * detail. Absent or malformed evidence fails closed with no checks.
 */
function readExecutionParityFile(home: string): ExecutionParityEvidence {
  const filePath = join(home, "data", "execution-parity.json");
  try {
    if (!existsSync(filePath)) return absentExecutionParity();
    const raw = JSON.parse(readFileSync(filePath, "utf8"));
    const parsed = executionParityArtifactSchema.safeParse(raw);
    if (!parsed.success) return absentExecutionParity();
    const checks = (parsed.data.checks ?? []).flatMap((check) => {
      const checkResult = executionParityCheckSchema.safeParse(check);
      return checkResult.success ? [checkResult.data] : [];
    });
    return {
      passed: checks.length > 0 && checks.every((check) => check.passed),
      protocolVersion:
        parsed.data.protocolVersion !== undefined &&
        parsed.data.protocolVersion.length > 0
          ? parsed.data.protocolVersion
          : "execution-parity/v1",
      checks,
    };
  } catch {
    return absentExecutionParity();
  }
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
  exchange: string,
  symbol: string,
  timeframe: string,
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
    .all(exchange, symbol, timeframe);
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
  exchange: string,
  symbol: string,
  timeframe: string,
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
       WHERE exchange = ? AND symbol = ? AND timeframe = ? AND cohort_id IS NOT NULL
       ORDER BY closed_at ASC, id ASC`,
    )
    .all(exchange, symbol, timeframe);
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

/** Cohort-union prospective evidence (≥50 fills across the cohort symbols). */
function computeProspective(trades: readonly RawTrade[]): ProspectiveEvidence {
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
  return {
    completeTradeCount: complete.length,
    durationDays:
      (Date.parse(latest) - Date.parse(earliest)) / (24 * 60 * 60 * 1000),
    expectancyPct:
      pValues.length > 0 ? (totalPnl / pValues.length).toString() : "0",
    confidenceLowerBoundPct: confidence.lowerBoundPct.toString(),
    maximumDrawdownPct: demoDrawdown(complete),
    allTradesHaveLiveFillEvidence:
      complete.length === trades.length && trades.length > 0,
  };
}

function timeframeMinutes(timeframe: string): number {
  return timeframe.endsWith("m") ? Number(timeframe.slice(0, -1)) : 15;
}

/** Backtest evidence summary used by `buildInput` (fail-closed defaults). */
interface HistoricalSummary {
  readonly completeWindows: number;
  readonly profitableWindowPct: number;
  readonly compoundedReturnPct: number;
  readonly maximumDrawdownPct: number;
  readonly totalTrades: number;
}

interface ConfidenceSummary {
  readonly sampleCount: number;
  readonly lowerBoundPct: number;
  readonly upperBoundPct: number;
  readonly resamples: number;
  readonly blockLength: number;
  readonly seed: number;
}

interface StressSummary {
  readonly worstReturnPct: number;
  readonly pooledLowerBoundPct: number;
  readonly seeds: readonly number[];
}

function historicalEvidence(
  gridOk: GridValidationOk | null,
  completeWindows: number,
): HistoricalSummary {
  if (gridOk === null) {
    return {
      completeWindows,
      profitableWindowPct: 0,
      compoundedReturnPct: 0,
      maximumDrawdownPct: 0,
      totalTrades: 0,
    };
  }
  return {
    completeWindows,
    profitableWindowPct: gridOk.historical.profitableWindowPct,
    compoundedReturnPct: gridOk.historical.compoundedReturnPct,
    maximumDrawdownPct: gridOk.historical.maximumDrawdownPct,
    totalTrades: gridOk.historical.totalTrades,
  };
}

function confidenceEvidence(
  gridOk: GridValidationOk | null,
): ConfidenceSummary {
  if (gridOk === null) {
    return {
      sampleCount: 0,
      lowerBoundPct: 0,
      upperBoundPct: 0,
      resamples: 0,
      blockLength: 5,
      seed: 0,
    };
  }
  return gridOk.confidence;
}

function stressEvidence(gridOk: GridValidationOk | null): StressSummary {
  if (gridOk === null) {
    // Fail closed: no backtest stress evidence → empty seed set trips the
    // "adverse stress seed set is incomplete" check instead of passing on
    // zero/zero defaults (regression fix 2026-08-07).
    return { worstReturnPct: 0, pooledLowerBoundPct: 0, seeds: [] };
  }
  return gridOk.stress;
}

/** Every trade must carry the expected fingerprint + full demo provenance. */
function provenanceValid(
  trades: readonly RawTrade[],
  expectedFingerprint: string,
): boolean {
  return (
    trades.length > 0 &&
    trades.every(
      (trade) =>
        trade.strategy_config_fingerprint === expectedFingerprint &&
        trade.cohort_id !== null &&
        trade.candidate_lock_at !== null &&
        trade.dataset_cutoff_at !== null &&
        trade.entry_opened_at !== null &&
        trade.execution_environment === "bybit-demo",
    )
  );
}

function provenanceEvidence(
  trades: readonly RawTrade[],
  firstProvenance: RawTrade | undefined,
  expectedFingerprint: string,
): ProvenanceEvidence {
  const earliest =
    firstProvenance?.entry_opened_at ?? "1970-01-01T00:00:00.000Z";
  const latest = trades.at(-1)?.closed_at ?? "1970-01-01T00:00:00.000Z";
  return {
    valid: provenanceValid(trades, expectedFingerprint),
    fingerprint: firstProvenance?.strategy_config_fingerprint ?? "",
    expectedFingerprint,
    cohortId: firstProvenance?.cohort_id ?? "",
    candidateLock: firstProvenance?.candidate_lock_at ?? "",
    datasetCutoff: firstProvenance?.dataset_cutoff_at ?? "",
    earliestEntry: earliest,
    latestClose: latest,
    queriedRows: trades.length,
    expectedRows: trades.length,
  };
}

function buildInput(
  candles: readonly CandleLike[],
  trades: readonly RawTrade[],
  now: Date,
  executionParity: ExecutionParityEvidence,
  timeframe: string,
  candidate: ValidatedGridCandidate,
  prospective: ProspectiveEvidence,
): RealMoneyReadinessInput {
  const manifest = strategyManifestFor(candidate);
  const expectedFingerprint = fingerprintStrategyManifest(manifest);
  const grid = validateGridEvidence(candles, {
    now,
    timeframeMinutes: timeframeMinutes(timeframe),
    grid: {
      gridStepPct: candidate.gridStepPct,
      gridMaxGrids: candidate.gridMaxGrids,
      gridPauseAfterLossBars: candidate.gridPauseAfterLossBars,
      feePct: candidate.feePct,
      slippageBps: candidate.slippageBps,
      initialCapital: 100,
      trendFilterPeriod: candidate.trendFilterPeriod,
      leverage: candidate.leverage,
      positionFraction: candidate.maxPositionSizePct / 100,
      chopGateAdxThreshold: candidate.chopGateAdx,
      targetRatio: candidate.targetRatio,
      onlyWithTrend: candidate.onlyWithTrend,
    },
    executionParityPassed: executionParity.passed,
  });
  const dataQuality = grid.dataQuality;
  const gridOk: GridValidationOk | null = grid.kind === "ok" ? grid : null;
  const historical = historicalEvidence(gridOk, dataQuality.completeWindows);
  const fixedConfidence = confidenceEvidence(gridOk);
  const stress = stressEvidence(gridOk);
  const firstProvenance = trades.at(0);
  return {
    prospectiveEvidence: prospective,
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
    executionParity,
    stress: {
      returnPct: stress.worstReturnPct.toString(),
      // Amendment 2026-08-07 (B): gate LB = pooled 5-seed bootstrap.
      lowerBoundPct: stress.pooledLowerBoundPct.toString(),
      seeds: stress.seeds,
    },
    provenance: provenanceEvidence(
      trades,
      firstProvenance,
      expectedFingerprint,
    ),
    dataQuality: {
      valid: dataQuality.valid && gridOk !== null,
      candleCount: dataQuality.candleCount,
      completeWindows: dataQuality.completeWindows,
      latestCandle: dataQuality.latestCandle?.toISOString() ?? "",
    },
    evaluatedAt: now.toISOString(),
    manifest,
  };
}

function resolveCandidates(
  symbols: readonly string[],
): readonly ValidatedGridCandidate[] {
  if (symbols.length === 0) return [...READINESS_COHORT_CANDIDATES];
  return symbols.map((symbol) => {
    const candidate = candidateForSymbol(symbol);
    if (!candidate) {
      throw new ReadinessInfrastructureError(
        `not a validated cohort symbol: ${symbol}`,
      );
    }
    return candidate;
  });
}

/** Per-gate merge: the gate passes iff it passes for EVERY cohort symbol. */
function mergedGateFor(
  gate: ReadinessGate,
  cohort: readonly CohortMemberReport[],
): ReadinessGate {
  const failed = cohort.filter(
    (member) => !member.gates.find((g) => g.id === gate.id)?.passed,
  );
  return {
    id: gate.id,
    passed: failed.length === 0,
    reasons: failed.flatMap((member) =>
      (member.gates.find((g) => g.id === gate.id)?.reasons ?? []).map(
        (reason) =>
          cohort.length > 1 ? `${member.symbol}: ${reason}` : reason,
      ),
    ),
  };
}

function mergeGates(cohort: readonly CohortMemberReport[]): ReadinessGate[] {
  const first = cohort[0];
  if (first === undefined) return [];
  return first.gates.map((gate) => mergedGateFor(gate, cohort));
}

function mergedProvenance(
  cohort: readonly CohortMemberReport[],
  first: CohortMemberReport["metrics"] | undefined,
): ProvenanceEvidence {
  const merged = cohort.reduce<{
    valid: boolean;
    queriedRows: number;
    expectedRows: number;
  }>(
    (acc, member) => ({
      valid: acc.valid && member.metrics.provenance.valid,
      queriedRows: acc.queriedRows + member.metrics.provenance.queriedRows,
      expectedRows: acc.expectedRows + member.metrics.provenance.expectedRows,
    }),
    { valid: true, queriedRows: 0, expectedRows: 0 },
  );
  return {
    valid: merged.valid,
    fingerprint: first?.provenance.fingerprint ?? "",
    expectedFingerprint: first?.provenance.expectedFingerprint ?? "",
    cohortId: first?.provenance.cohortId ?? "",
    candidateLock: first?.provenance.candidateLock ?? "",
    datasetCutoff: first?.provenance.datasetCutoff ?? "",
    earliestEntry: first?.provenance.earliestEntry ?? "",
    latestClose: first?.provenance.latestClose ?? "",
    queriedRows: merged.queriedRows,
    expectedRows: merged.expectedRows,
  };
}

/** Cohort-wide metrics: first member's measurements, unioned provenance. */
function mergedMetrics(
  cohort: readonly CohortMemberReport[],
  prospective: ProspectiveEvidence,
  evaluatedAt: string,
): RealMoneyReadinessReport["metrics"] {
  const first = cohort[0]?.metrics;
  return {
    prospective,
    historical: first?.historical ?? {
      completeWindows: 0,
      profitableWindowPct: 0,
      compoundedReturnPct: "0",
      maximumDrawdownPct: "0",
      totalTrades: 0,
    },
    confidence: first?.confidence ?? {
      sampleCount: 0,
      lowerBoundPct: "0",
      upperBoundPct: "0",
      resamples: 0,
      blockLength: 5,
      seed: 0,
    },
    stress: first?.stress ?? {
      returnPct: "0",
      lowerBoundPct: "0",
      seeds: [],
    },
    provenance: mergedProvenance(cohort, first),
    dataQuality: first?.dataQuality ?? {
      valid: false,
      candleCount: 0,
      completeWindows: 0,
      latestCandle: "",
    },
    evaluatedAt,
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
  const executionParity =
    options.parityFixture === "golden"
      ? goldenExecutionParity
      : readExecutionParityFile(home);
  const candidates = resolveCandidates(args.symbols);
  const db = new Database(databasePath(home), {
    readonly: true,
    create: false,
  });
  try {
    requireSchema(db);
    const now = options.now ?? new Date();
    const perSymbol = candidates.map((candidate) => ({
      candidate,
      candles: readCandles(db, args.exchange, candidate.symbol, args.timeframe),
      trades: readTrades(db, args.exchange, candidate.symbol, args.timeframe),
    }));
    // Cohort-union prospective evidence: ≥50 fills across ALL cohort symbols.
    const prospective = computeProspective(
      perSymbol.flatMap((entry) => entry.trades),
    );
    const cohort = perSymbol.map(({ candidate, candles, trades }) => {
      const report = evaluateRealMoneyReadiness(
        buildInput(
          candles,
          trades,
          now,
          executionParity,
          args.timeframe,
          candidate,
          prospective,
        ),
      );
      return {
        symbol: candidate.symbol,
        status: report.status,
        gates: report.gates,
        failedGateIds: report.failedGateIds,
        errors: report.errors,
        metrics: report.metrics,
        fingerprint: report.candidateFingerprint,
        expectedFingerprint: fingerprintStrategyManifest(
          strategyManifestFor(candidate),
        ),
      };
    });
    const mergedGates: ReadinessGate[] = mergeGates(cohort);
    const failedGateIds: readonly ReadinessGateId[] = mergedGates
      .filter((gate) => !gate.passed)
      .map((gate) => gate.id);
    const allPassed = cohort.every((member) => member.status === "PASS");
    const first = cohort[0]?.metrics;
    return {
      schemaVersion: "real-money-readiness/v2",
      status: allPassed ? "PASS" : "FAIL",
      exitCode: allPassed ? 0 : 1,
      candidateFingerprint: first?.provenance.fingerprint ?? "",
      thresholds: DEFAULT_READINESS_THRESHOLDS,
      gates: mergedGates,
      failedGateIds,
      errors: [],
      cohort,
      metrics: mergedMetrics(cohort, prospective, now.toISOString()),
    };
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
    "  --exchange <text>   Candle exchange (default: bybit-futures)",
    "  --symbol <text>     Cohort symbol (repeatable; default: BTC+SOL cohort)",
    "  --timeframe <text>  Candidate timeframe (default: 15m)",
  ].join("\n");
}

export function versionText(): string {
  return "real-money-readiness/v2";
}

export function runRealMoneyReadiness(
  argv: readonly string[],
  options: RealMoneyReadinessCliOptions = {},
): RealMoneyReadinessResult {
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
