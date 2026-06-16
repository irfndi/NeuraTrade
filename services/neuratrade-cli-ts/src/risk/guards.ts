import { Context, Effect, Layer } from "effect";

/**
 * Error raised when a pre-trade risk guard blocks an order.
 */
export class RiskError {
  readonly _tag = "RiskError" as const;
  constructor(
    readonly reason: string,
    readonly violations: readonly string[],
  ) {}
}

/**
 * Hard risk limits for the deterministic scalping engine.
 *
 * All percentages are expressed as whole numbers (e.g. 5 means 5%).
 */
export interface RiskLimits {
  readonly liveTradingEnabled: boolean;
  readonly maxPositionSizePct: number;
  readonly maxDailyLossPct: number;
  readonly maxDrawdownPct: number;
  readonly minCapital: number;
  readonly maxTradesPerDay: number;
  readonly allowedSymbols?: readonly string[];
}

/**
 * Trading context supplied to the pre-trade risk check.
 */
export interface RiskContext {
  readonly isLive: boolean;
  readonly capital: number;
  readonly peakCapital: number;
  readonly startOfDayCapital: number;
  readonly dailyRealizedPnl: number;
  readonly tradesTodayCount: number;
  readonly positionValue: number;
  readonly symbol: string;
  readonly side: "buy" | "sell";
}

/**
 * Port for pre-trade risk checks.
 */
export interface RiskGuardService {
  readonly check: (
    context: RiskContext,
  ) => Effect.Effect<void, RiskError, never>;
}

export const RiskGuard = Context.GenericTag<RiskGuardService>("RiskGuard");

/**
 * Conservative defaults for live trading; permissive defaults for paper trading.
 */
export function defaultRiskLimits(isLive: boolean): RiskLimits {
  if (isLive) {
    return {
      liveTradingEnabled: true,
      maxPositionSizePct: 10,
      maxDailyLossPct: 2,
      maxDrawdownPct: 5,
      minCapital: 100,
      maxTradesPerDay: 10,
    };
  }

  return {
    liveTradingEnabled: false,
    maxPositionSizePct: 100,
    maxDailyLossPct: 100,
    maxDrawdownPct: 100,
    minCapital: 0,
    maxTradesPerDay: Number.MAX_SAFE_INTEGER,
  };
}

/**
 * Build a risk guard from explicit limits.
 */
export function makeRiskGuard(limits: RiskLimits): RiskGuardService {
  return {
    check: (context) =>
      Effect.gen(function* () {
        const violations: string[] = [];

        if (context.isLive && !limits.liveTradingEnabled) {
          violations.push("live trading is disabled");
        }

        if (context.capital < limits.minCapital) {
          violations.push(
            `capital ${context.capital.toFixed(2)} is below minimum ${limits.minCapital}`,
          );
        }

        const drawdownPct =
          context.peakCapital > 0
            ? ((context.peakCapital - context.capital) / context.peakCapital) *
              100
            : 0;
        if (drawdownPct > limits.maxDrawdownPct) {
          violations.push(
            `drawdown ${drawdownPct.toFixed(2)}% exceeds max ${limits.maxDrawdownPct}%`,
          );
        }

        const dailyLossPct =
          context.startOfDayCapital > 0
            ? (-context.dailyRealizedPnl / context.startOfDayCapital) * 100
            : 0;
        if (dailyLossPct > limits.maxDailyLossPct) {
          violations.push(
            `daily loss ${dailyLossPct.toFixed(2)}% exceeds max ${limits.maxDailyLossPct}%`,
          );
        }

        if (context.tradesTodayCount >= limits.maxTradesPerDay) {
          violations.push(
            `trades today ${context.tradesTodayCount} meets or exceeds max ${limits.maxTradesPerDay}`,
          );
        }

        const positionSizePct =
          context.capital > 0
            ? (context.positionValue / context.capital) * 100
            : 0;
        if (positionSizePct > limits.maxPositionSizePct) {
          violations.push(
            `position size ${positionSizePct.toFixed(2)}% exceeds max ${limits.maxPositionSizePct}%`,
          );
        }

        if (
          limits.allowedSymbols &&
          limits.allowedSymbols.length > 0 &&
          !limits.allowedSymbols.includes(context.symbol)
        ) {
          violations.push(
            `symbol ${context.symbol} is not in the allowed list`,
          );
        }

        if (violations.length > 0) {
          return yield* Effect.fail(
            new RiskError("pre-trade risk check failed", violations),
          );
        }
      }),
  };
}

/**
 * Build a RiskGuard layer using mode-aware defaults and optional overrides.
 */
export function RiskGuardLive(
  isLive: boolean,
  overrides?: Partial<RiskLimits>,
): Layer.Layer<RiskGuardService> {
  const limits = { ...defaultRiskLimits(isLive), ...overrides };
  return Layer.succeed(RiskGuard, makeRiskGuard(limits));
}
