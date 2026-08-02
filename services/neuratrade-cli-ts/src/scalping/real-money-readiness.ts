import { createHash } from "node:crypto";

export const READINESS_SCHEMA_VERSION = "real-money-readiness/v1" as const;

export const READINESS_GATE_IDS = [
  "prospective-evidence",
  "historical-robustness",
  "confidence",
  "execution-parity",
  "stress",
  "provenance",
  "data-quality",
  "freshness",
  "thresholds",
] as const;

export type ReadinessGateId = (typeof READINESS_GATE_IDS)[number];
export type ReadinessStatus = "PASS" | "FAIL" | "ERROR";

export interface ReadinessThresholds {
  readonly minimumDemoTrades: number;
  readonly minimumDemoDurationDays: number;
  readonly minimumDemoExpectancyPct: string;
  readonly minimumDemoConfidenceLowerBoundPct: string;
  readonly maximumDemoDrawdownPct: string;
  readonly minimumHistoricalWindows: number;
  readonly minimumProfitableWindowPct: number;
  readonly minimumHistoricalCompoundedReturnPct: string;
  readonly maximumHistoricalDrawdownPct: string;
  readonly minimumFixedOosTrades: number;
  readonly minimumConfidenceLowerBoundPct: string;
  readonly confidenceResamples: number;
  readonly confidenceBlockLength: number;
  readonly minimumStressReturnPct: string;
  readonly minimumStressLowerBoundPct: string;
  readonly maximumFreshnessHours: number;
}

export const DEFAULT_READINESS_THRESHOLDS: ReadinessThresholds = {
  minimumDemoTrades: 50,
  minimumDemoDurationDays: 7,
  minimumDemoExpectancyPct: "0",
  minimumDemoConfidenceLowerBoundPct: "0",
  maximumDemoDrawdownPct: "15",
  minimumHistoricalWindows: 10,
  minimumProfitableWindowPct: 50,
  minimumHistoricalCompoundedReturnPct: "0",
  maximumHistoricalDrawdownPct: "15",
  minimumFixedOosTrades: 30,
  minimumConfidenceLowerBoundPct: "0",
  confidenceResamples: 5000,
  confidenceBlockLength: 5,
  minimumStressReturnPct: "0",
  minimumStressLowerBoundPct: "0",
  maximumFreshnessHours: 48,
};

export interface StrategyManifest {
  readonly schema: string;
  readonly exchange: string;
  readonly symbol: string;
  readonly timeframe: string;
  readonly gridStepPct: string;
  readonly gridMaxGrids: string;
  readonly gridPauseAfterLossBars: string;
  readonly positionFraction: string;
  readonly feePct: string;
  readonly slippageBps: string;
  readonly trendFilterPeriod: string;
  readonly adxGate: string;
  readonly orderType: string;
  readonly triggerTiming: string;
  readonly engineVersion: string;
  readonly protocolVersion: string;
}

export const DEFAULT_STRATEGY_MANIFEST = {
  schema: READINESS_SCHEMA_VERSION,
  exchange: "bitget-demo",
  symbol: "BTC/USDT:USDT",
  timeframe: "15m",
  gridStepPct: "1",
  gridMaxGrids: "1.5",
  gridPauseAfterLossBars: "12",
  positionFraction: "0.5",
  feePct: "0.06",
  slippageBps: "2",
  trendFilterPeriod: "96",
  adxGate: "30",
  orderType: "market-after-trigger",
  triggerTiming: "next-bar",
  engineVersion: "grid-engine/v1",
  protocolVersion: READINESS_SCHEMA_VERSION,
} as const satisfies StrategyManifest;

export interface ProspectiveEvidence {
  readonly completeTradeCount: number;
  readonly durationDays: number;
  readonly expectancyPct: string;
  readonly confidenceLowerBoundPct: string;
  readonly maximumDrawdownPct: string;
  readonly allTradesHaveLiveFillEvidence: boolean;
}

export interface HistoricalRobustnessEvidence {
  readonly completeWindows: number;
  readonly profitableWindowPct: number;
  readonly compoundedReturnPct: string;
  readonly maximumDrawdownPct: string;
  readonly totalTrades: number;
}

export interface ConfidenceEvidence {
  readonly sampleCount: number;
  readonly lowerBoundPct: string;
  readonly upperBoundPct: string;
  readonly resamples: number;
  readonly blockLength: number;
  readonly seed: number;
}

export interface ExecutionParityEvidence {
  readonly passed: boolean;
  readonly protocolVersion: string;
  readonly checks: readonly string[];
}

export interface StressEvidence {
  readonly returnPct: string;
  readonly lowerBoundPct: string;
  readonly seeds: readonly number[];
}

export interface ProvenanceEvidence {
  readonly valid: boolean;
  readonly fingerprint: string;
  readonly expectedFingerprint: string;
  readonly cohortId: string;
  readonly candidateLock: string;
  readonly datasetCutoff: string;
  readonly earliestEntry: string;
  readonly latestClose: string;
  readonly queriedRows: number;
  readonly expectedRows: number;
}

export interface DataQualityEvidence {
  readonly valid: boolean;
  readonly candleCount: number;
  readonly completeWindows: number;
  readonly latestCandle: string;
}

export interface RealMoneyReadinessInput {
  readonly prospectiveEvidence: ProspectiveEvidence;
  readonly historicalRobustness: HistoricalRobustnessEvidence;
  readonly confidence: ConfidenceEvidence;
  readonly executionParity: ExecutionParityEvidence;
  readonly stress: StressEvidence;
  readonly provenance: ProvenanceEvidence;
  readonly dataQuality: DataQualityEvidence;
  readonly evaluatedAt: string;
  readonly manifest: StrategyManifest;
  readonly thresholdOverrides?: Partial<ReadinessThresholds>;
}

export interface ReadinessGate {
  readonly id: ReadinessGateId;
  readonly passed: boolean;
  readonly reasons: readonly string[];
}

export interface RealMoneyReadinessReport {
  readonly schemaVersion: typeof READINESS_SCHEMA_VERSION;
  readonly status: ReadinessStatus;
  readonly exitCode: 0 | 1 | 2;
  readonly candidateFingerprint: string;
  readonly thresholds: ReadinessThresholds;
  readonly gates: readonly ReadinessGate[];
  readonly failedGateIds: readonly ReadinessGateId[];
  readonly errors: readonly string[];
  readonly metrics: {
    readonly prospective: ProspectiveEvidence;
    readonly historical: HistoricalRobustnessEvidence;
    readonly confidence: ConfidenceEvidence;
    readonly stress: StressEvidence;
    readonly provenance: ProvenanceEvidence;
    readonly dataQuality: DataQualityEvidence;
    readonly evaluatedAt: string;
  };
}

type DecimalLike = string;

const decimalSyntax = /^[+-]?(0|[1-9][0-9]*)(\.[0-9]+)?$/;

function normalizeDecimal(value: DecimalLike): string {
  if (!decimalSyntax.test(value)) {
    throw new Error(`invalid decimal: ${value}`);
  }
  const negative = value.startsWith("-");
  const unsigned = value.replace(/^[+-]/, "");
  const [integer, fraction = ""] = unsigned.split(".");
  const normalizedFraction = fraction.replace(/0+$/, "");
  const magnitude =
    normalizedFraction.length > 0
      ? `${integer}.${normalizedFraction}`
      : integer;
  return magnitude === "0" || /^0\.0*$/.test(magnitude)
    ? "0"
    : negative
      ? `-${magnitude}`
      : magnitude;
}

function decimalValue(value: string): number | null {
  try {
    const normalized = normalizeDecimal(value);
    const parsed = Number(normalized);
    return Number.isFinite(parsed) ? parsed : null;
  } catch (error) {
    if (error instanceof Error) return null;
    return null;
  }
}

function stableValue(value: unknown): string {
  if (typeof value === "string") {
    return JSON.stringify(
      decimalSyntax.test(value) ? normalizeDecimal(value) : value,
    );
  }
  if (typeof value === "number") {
    if (!Number.isFinite(value)) throw new Error("non-finite manifest value");
    return JSON.stringify(value);
  }
  if (typeof value === "boolean" || value === null)
    return JSON.stringify(value);
  if (Array.isArray(value)) {
    return `[${value.map((item) => stableValue(item)).join(",")}]`;
  }
  if (typeof value === "object") {
    const entries = Object.entries(value).sort(([left], [right]) =>
      left < right ? -1 : left > right ? 1 : 0,
    );
    return `{${entries.map(([key, item]) => `${JSON.stringify(key)}:${stableValue(item)}`).join(",")}}`;
  }
  throw new Error("unsupported manifest value");
}

export function canonicalizeStrategyManifest(
  manifest: StrategyManifest,
): string {
  const canonical: Record<string, string> = {};
  for (const [key, value] of Object.entries(manifest)) {
    canonical[key] =
      key === "schema" ||
      key.endsWith("Version") ||
      key === "exchange" ||
      key === "symbol" ||
      key === "timeframe" ||
      key === "orderType" ||
      key === "triggerTiming"
        ? value
        : normalizeDecimal(value);
  }
  return stableValue(canonical);
}

export function fingerprintStrategyManifest(
  manifest: StrategyManifest,
): string {
  return createHash("sha256")
    .update(canonicalizeStrategyManifest(manifest), "utf8")
    .digest("hex");
}

function gate(id: ReadinessGateId, reasons: readonly string[]): ReadinessGate {
  return { id, passed: reasons.length === 0, reasons };
}

function below(value: string, threshold: string): boolean {
  const actual = decimalValue(value);
  const limit = decimalValue(threshold);
  return actual !== null && limit !== null && actual < limit;
}

function above(value: string, threshold: string): boolean {
  const actual = decimalValue(value);
  const limit = decimalValue(threshold);
  return actual !== null && limit !== null && actual > limit;
}

function resolveThresholds(
  overrides: Partial<ReadinessThresholds> | undefined,
): {
  readonly thresholds: ReadinessThresholds;
  readonly errors: readonly string[];
} {
  const overridesToApply = overrides ?? {};
  const thresholds = {
    ...DEFAULT_READINESS_THRESHOLDS,
    ...overridesToApply,
  };
  const errors: string[] = [];
  for (const key of Object.keys(overridesToApply) as Array<
    keyof ReadinessThresholds
  >) {
    const requested = thresholds[key];
    const baseline = DEFAULT_READINESS_THRESHOLDS[key];
    if (typeof requested === "number" && !Number.isFinite(requested)) {
      errors.push(`threshold override is malformed: ${key}`);
    } else if (
      typeof requested === "number" &&
      typeof baseline === "number" &&
      requested < baseline
    ) {
      errors.push(`threshold override weakens ${key}`);
    }
    if (typeof requested === "string" && typeof baseline === "string") {
      const requestedValue = decimalValue(requested);
      const baselineValue = decimalValue(baseline);
      if (requestedValue === null || baselineValue === null) {
        errors.push(`threshold override is malformed: ${key}`);
      } else if (
        key.startsWith("maximum")
          ? requestedValue > baselineValue
          : requestedValue < baselineValue
      ) {
        errors.push(`threshold override weakens ${key}`);
      }
    }
  }
  return { thresholds, errors };
}

function dateMillis(value: string): number | null {
  const millis = Date.parse(value);
  return Number.isFinite(millis) ? millis : null;
}

function isAfter(left: string, right: string): boolean {
  const leftMillis = dateMillis(left);
  const rightMillis = dateMillis(right);
  return (
    leftMillis !== null && rightMillis !== null && leftMillis > rightMillis
  );
}

function isBefore(left: string, right: string): boolean {
  const leftMillis = dateMillis(left);
  const rightMillis = dateMillis(right);
  return (
    leftMillis !== null && rightMillis !== null && leftMillis < rightMillis
  );
}

export function evaluateRealMoneyReadiness(
  input: RealMoneyReadinessInput,
): RealMoneyReadinessReport {
  const { thresholds, errors: resolvedErrors } = resolveThresholds(
    input.thresholdOverrides,
  );
  const errors = [...resolvedErrors];
  let candidateFingerprint = "";
  try {
    candidateFingerprint = fingerprintStrategyManifest(input.manifest);
  } catch (error) {
    errors.push(
      `manifest is invalid: ${error instanceof Error ? error.message : String(error)}`,
    );
  }
  if (errors.length > 0) {
    return {
      schemaVersion: READINESS_SCHEMA_VERSION,
      status: "ERROR",
      exitCode: 2,
      candidateFingerprint,
      thresholds,
      gates: READINESS_GATE_IDS.map((id) =>
        gate(id, ["not evaluated because contract input is invalid"]),
      ),
      failedGateIds: [],
      errors,
      metrics: {
        prospective: input.prospectiveEvidence,
        historical: input.historicalRobustness,
        confidence: input.confidence,
        stress: input.stress,
        provenance: input.provenance,
        dataQuality: input.dataQuality,
        evaluatedAt: input.evaluatedAt,
      },
    };
  }

  const p = input.prospectiveEvidence;
  const h = input.historicalRobustness;
  const c = input.confidence;
  const s = input.stress;
  const provenance = input.provenance;
  const quality = input.dataQuality;
  const evaluatedAt = dateMillis(input.evaluatedAt);
  const latestCandle = dateMillis(quality.latestCandle);
  const ageHours =
    evaluatedAt !== null && latestCandle !== null
      ? (evaluatedAt - latestCandle) / (60 * 60 * 1000)
      : Number.POSITIVE_INFINITY;

  const prospectiveReasons = [
    ...(!Number.isFinite(p.completeTradeCount)
      ? ["demo trade count is malformed"]
      : []),
    ...(!Number.isFinite(p.durationDays) ? ["demo duration is malformed"] : []),
    ...(p.completeTradeCount < thresholds.minimumDemoTrades
      ? ["demo trade count is below the minimum"]
      : []),
    ...(p.durationDays < thresholds.minimumDemoDurationDays
      ? ["demo duration is below the minimum"]
      : []),
    ...(decimalValue(p.expectancyPct) === null
      ? ["demo expectancy is malformed"]
      : []),
    ...(below(p.expectancyPct, thresholds.minimumDemoExpectancyPct)
      ? ["demo expectancy is below the minimum"]
      : []),
    ...(decimalValue(p.confidenceLowerBoundPct) === null
      ? ["demo confidence lower bound is malformed"]
      : []),
    ...(below(
      p.confidenceLowerBoundPct,
      thresholds.minimumDemoConfidenceLowerBoundPct,
    )
      ? ["demo confidence lower bound is below the minimum"]
      : []),
    ...(decimalValue(p.maximumDrawdownPct) === null
      ? ["demo drawdown is malformed"]
      : []),
    ...(above(p.maximumDrawdownPct, thresholds.maximumDemoDrawdownPct)
      ? ["demo drawdown is above the maximum"]
      : []),
    ...(!p.allTradesHaveLiveFillEvidence
      ? ["one or more demo trades lack complete live fill evidence"]
      : []),
  ];
  const historicalReasons = [
    ...(!Number.isFinite(h.completeWindows)
      ? ["historical window count is malformed"]
      : []),
    ...(!Number.isFinite(h.profitableWindowPct)
      ? ["profitable historical window percentage is malformed"]
      : []),
    ...(!Number.isFinite(h.totalTrades)
      ? ["historical trade count is malformed"]
      : []),
    ...(h.completeWindows < thresholds.minimumHistoricalWindows
      ? ["historical window count is below the minimum"]
      : []),
    ...(h.profitableWindowPct <= thresholds.minimumProfitableWindowPct
      ? ["profitable historical windows do not exceed the minimum"]
      : []),
    ...(decimalValue(h.compoundedReturnPct) === null
      ? ["historical compounded return is malformed"]
      : []),
    ...(below(
      h.compoundedReturnPct,
      thresholds.minimumHistoricalCompoundedReturnPct,
    )
      ? ["historical compounded return is negative"]
      : []),
    ...(decimalValue(h.maximumDrawdownPct) === null
      ? ["historical drawdown is malformed"]
      : []),
    ...(above(h.maximumDrawdownPct, thresholds.maximumHistoricalDrawdownPct)
      ? ["historical drawdown is above the maximum"]
      : []),
    ...(h.totalTrades < thresholds.minimumFixedOosTrades
      ? ["historical trade count is below the minimum"]
      : []),
  ];
  const confidenceReasons = [
    ...(c.sampleCount < thresholds.minimumFixedOosTrades
      ? ["confidence sample count is below the minimum"]
      : []),
    ...(c.resamples !== thresholds.confidenceResamples
      ? ["confidence resample count does not match the protocol"]
      : []),
    ...(c.blockLength !== thresholds.confidenceBlockLength
      ? ["confidence block length does not match the protocol"]
      : []),
    ...(c.seed === 0 ? ["confidence seed must be non-zero"] : []),
    ...(decimalValue(c.lowerBoundPct) === null
      ? ["confidence lower bound is malformed"]
      : []),
    ...(below(c.lowerBoundPct, thresholds.minimumConfidenceLowerBoundPct)
      ? ["confidence lower bound is below the minimum"]
      : []),
    ...(decimalValue(c.upperBoundPct) === null
      ? ["confidence upper bound is malformed"]
      : []),
    ...(above(c.lowerBoundPct, c.upperBoundPct)
      ? ["confidence interval is inverted"]
      : []),
  ];
  const stressReasons = [
    ...(![20260802, 20260803, 20260804, 20260805, 20260806].every((seed) =>
      s.seeds.includes(seed),
    )
      ? ["adverse stress seed set is incomplete"]
      : []),
    ...(decimalValue(s.returnPct) === null
      ? ["adverse stress return is malformed"]
      : []),
    ...(below(s.returnPct, thresholds.minimumStressReturnPct)
      ? ["adverse stress return is negative"]
      : []),
    ...(decimalValue(s.lowerBoundPct) === null
      ? ["adverse stress confidence lower bound is malformed"]
      : []),
    ...(below(s.lowerBoundPct, thresholds.minimumStressLowerBoundPct)
      ? ["adverse stress confidence lower bound is below the minimum"]
      : []),
  ];
  const provenanceReasons = [
    ...(!provenance.valid ? ["provenance validation failed"] : []),
    ...(provenance.fingerprint !== provenance.expectedFingerprint
      ? ["candidate fingerprint mismatch"]
      : []),
    ...(provenance.cohortId.length === 0 ? ["cohort ID is missing"] : []),
    ...(provenance.queriedRows !== provenance.expectedRows
      ? ["cohort query was truncated"]
      : []),
    ...([
      provenance.candidateLock,
      provenance.datasetCutoff,
      provenance.earliestEntry,
      provenance.latestClose,
    ].some((value) => dateMillis(value) === null)
      ? ["provenance timestamp is malformed"]
      : []),
    ...(isAfter(provenance.candidateLock, provenance.datasetCutoff)
      ? ["candidate lock is after dataset cutoff"]
      : []),
    ...(isBefore(provenance.earliestEntry, provenance.candidateLock)
      ? ["entry predates candidate lock"]
      : []),
    ...(isAfter(provenance.datasetCutoff, provenance.earliestEntry)
      ? ["entry predates dataset cutoff"]
      : []),
    ...(isAfter(provenance.earliestEntry, provenance.latestClose)
      ? ["entry is after close"]
      : []),
    ...(evaluatedAt !== null &&
    isAfter(provenance.latestClose, input.evaluatedAt)
      ? ["close is after evaluation time"]
      : []),
  ];
  const dataQualityReasons = [
    ...(!Number.isFinite(quality.candleCount)
      ? ["candle count is malformed"]
      : []),
    ...(!Number.isFinite(quality.completeWindows)
      ? ["complete window count is malformed"]
      : []),
    ...(!quality.valid ? ["candle data-quality validation failed"] : []),
    ...(quality.candleCount === 0 ? ["candle evidence is empty"] : []),
    ...(quality.completeWindows < thresholds.minimumHistoricalWindows
      ? ["candle evidence has insufficient complete windows"]
      : []),
  ];
  const freshnessReasons = [
    ...(evaluatedAt === null || latestCandle === null
      ? ["freshness timestamp is malformed"]
      : []),
    ...(ageHours < 0 ? ["latest candle is in the future"] : []),
    ...(ageHours > thresholds.maximumFreshnessHours
      ? ["latest candle is stale"]
      : []),
  ];
  const requiredParityChecks = [
    "trigger-bar",
    "order-type",
    "fill-price",
    "fees",
    "slippage",
    "quantity",
    "exit-reason",
    "pnl",
  ] as const;
  const parityReasons = [
    ...(!input.executionParity.passed
      ? ["deployed execution semantics do not match the validated replay"]
      : []),
    ...(input.executionParity.protocolVersion !== "execution-parity/v1"
      ? ["execution parity protocol version is unsupported"]
      : []),
    ...requiredParityChecks
      .filter((check) => !input.executionParity.checks.includes(check))
      .map((check) => `execution parity check is missing: ${check}`),
  ];

  const gates = [
    gate("prospective-evidence", prospectiveReasons),
    gate("historical-robustness", historicalReasons),
    gate("confidence", confidenceReasons),
    gate("execution-parity", parityReasons),
    gate("stress", stressReasons),
    gate("provenance", provenanceReasons),
    gate("data-quality", dataQualityReasons),
    gate("freshness", freshnessReasons),
    gate("thresholds", []),
  ] satisfies readonly ReadinessGate[];
  const failedGateIds = gates
    .filter((item) => !item.passed)
    .map((item) => item.id);

  return {
    schemaVersion: READINESS_SCHEMA_VERSION,
    status: failedGateIds.length === 0 ? "PASS" : "FAIL",
    exitCode: failedGateIds.length === 0 ? 0 : 1,
    candidateFingerprint,
    thresholds,
    gates,
    failedGateIds,
    errors: [],
    metrics: {
      prospective: p,
      historical: h,
      confidence: c,
      stress: s,
      provenance,
      dataQuality: quality,
      evaluatedAt: input.evaluatedAt,
    },
  };
}

export function serializeRealMoneyReadiness(
  report: RealMoneyReadinessReport,
): string {
  return JSON.stringify(report);
}
