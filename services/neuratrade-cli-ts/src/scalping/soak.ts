import { Effect } from "effect";
import type {
  FuturesProductType,
  FuturesMarginMode,
} from "../exchange/futures-adapter.js";
import type { ComposerConfig } from "./types.js";

export interface SoakSymbol {
  readonly symbol: string;
  readonly exchange: string;
  readonly productType?: FuturesProductType;
  readonly leverage?: number;
  readonly marginMode?: FuturesMarginMode;
  readonly bestParams?: {
    readonly atrStopMultiplier?: number;
    readonly atrTakeProfitMultiplier?: number;
    readonly minConfidence?: number;
  };
}

export interface SoakOptions {
  readonly watchlist: readonly SoakSymbol[];
  readonly iterationsPerSymbol: number;
  readonly intervalSeconds: number;
  readonly isLive: boolean;
  readonly initialCapital: number;
  readonly positionSizePct: number;
  readonly feePct: number;
  readonly minConfidence: number;
  readonly useAtrStops: boolean;
  readonly atrStopMultiplier: number;
  readonly atrTakeProfitMultiplier: number;
  readonly holdUntilStop: boolean;
  readonly regimeMode: "trend" | "reversion";
  readonly composerConfig: ComposerConfig;
  readonly leverage: number;
  readonly marginMode: FuturesMarginMode;
  readonly productType: FuturesProductType;
}

export interface IterationResult {
  readonly action: "opened" | "closed" | "hold";
  readonly capital: number;
  readonly note: string;
}

export interface PerSymbolResult {
  readonly symbol: string;
  readonly exchange: string;
  readonly trades: number;
  readonly finalCapital: number;
  readonly totalReturnPct: number;
  readonly maxDrawdownPct: number;
  readonly sharpeRatio: number;
  readonly winRate: number;
  readonly note: string;
}

export interface SoakAggregate {
  readonly avgReturnPct: number;
  readonly profitableCount: number;
  readonly maxDrawdownPct: number;
  readonly avgSharpeRatio: number;
  readonly totalTrades: number;
}

export interface SoakResult {
  readonly perSymbolResults: readonly PerSymbolResult[];
  readonly aggregate: SoakAggregate;
}

export type IterationRunner<E = unknown> = (
  symbol: string,
  exchange: string,
  params?: SoakSymbol["bestParams"],
) => Effect.Effect<IterationResult, E, never>;

export function runSoak<E>(
  options: SoakOptions,
  runner: IterationRunner<E>,
): Effect.Effect<SoakResult, E, never> {
  return Effect.gen(function* () {
    const perSymbolResults: PerSymbolResult[] = [];
    let aggregateTrades = 0;

    for (const entry of options.watchlist) {
      const exchange = entry.exchange;
      const capitalSnapshots: number[] = [];
      const actions: Array<"opened" | "closed" | "hold"> = [];
      const notes: string[] = [];

      for (let i = 0; i < options.iterationsPerSymbol; i++) {
        const result = yield* runner(entry.symbol, exchange, entry.bestParams);
        capitalSnapshots.push(result.capital);
        actions.push(result.action);
        if (result.action === "opened" || result.action === "closed") {
          notes.push(result.note);
        }

        if (
          i < options.iterationsPerSymbol - 1 &&
          options.intervalSeconds > 0
        ) {
          yield* Effect.sleep(`${options.intervalSeconds} seconds`);
        }
      }

      const perSymbol = computePerSymbolMetrics(
        entry.symbol,
        exchange,
        options.initialCapital,
        capitalSnapshots,
        actions,
        notes,
      );
      perSymbolResults.push(perSymbol);
      aggregateTrades += perSymbol.trades;
    }

    return {
      perSymbolResults,
      aggregate: computeAggregate(perSymbolResults, aggregateTrades),
    };
  });
}

function computePerSymbolMetrics(
  symbol: string,
  exchange: string,
  initialCapital: number,
  capitalSnapshots: readonly number[],
  actions: readonly ("opened" | "closed" | "hold")[],
  notes: readonly string[],
): PerSymbolResult {
  const startCapital =
    capitalSnapshots.length > 0 ? capitalSnapshots[0] : initialCapital;
  const finalCapital =
    capitalSnapshots.length > 0
      ? capitalSnapshots[capitalSnapshots.length - 1]
      : startCapital;

  const totalReturnPct =
    startCapital > 0 ? ((finalCapital - startCapital) / startCapital) * 100 : 0;

  let peak = startCapital;
  let maxDrawdownPct = 0;
  for (const cap of capitalSnapshots) {
    peak = Math.max(peak, cap);
    const dd = peak > 0 ? ((peak - cap) / peak) * 100 : 0;
    maxDrawdownPct = Math.max(maxDrawdownPct, dd);
  }

  let entryCap = initialCapital;
  const tradeReturns: number[] = [];
  for (let i = 0; i < actions.length; i++) {
    if (actions[i] === "opened") {
      entryCap = capitalSnapshots[i];
    } else if (actions[i] === "closed") {
      tradeReturns.push(capitalSnapshots[i] - entryCap);
    }
  }

  const trades = tradeReturns.length;
  const winningTrades = tradeReturns.filter((r) => r > 0).length;
  const winRate = trades > 0 ? winningTrades / trades : 0;

  const avgReturn =
    trades > 0 ? tradeReturns.reduce((a, b) => a + b, 0) / trades : 0;
  const variance =
    trades > 1
      ? tradeReturns.reduce((sum, r) => sum + (r - avgReturn) ** 2, 0) /
        (trades - 1)
      : 0;
  const stdDev = Math.sqrt(variance);
  const sharpeRatio = stdDev > 0 ? avgReturn / stdDev : 0;

  const note = notes.length > 0 ? notes[notes.length - 1] : "no trades";

  return {
    symbol,
    exchange,
    trades,
    finalCapital,
    totalReturnPct,
    maxDrawdownPct,
    sharpeRatio,
    winRate,
    note,
  };
}

function computeAggregate(
  results: readonly PerSymbolResult[],
  totalTrades: number,
): SoakAggregate {
  if (results.length === 0) {
    return {
      avgReturnPct: 0,
      profitableCount: 0,
      maxDrawdownPct: 0,
      avgSharpeRatio: 0,
      totalTrades: 0,
    };
  }

  const avgReturnPct =
    results.reduce((sum, r) => sum + r.totalReturnPct, 0) / results.length;
  const profitableCount = results.filter((r) => r.totalReturnPct > 0).length;
  const maxDrawdownPct = Math.max(...results.map((r) => r.maxDrawdownPct));
  const avgSharpeRatio =
    results.reduce((sum, r) => sum + r.sharpeRatio, 0) / results.length;

  return {
    avgReturnPct,
    profitableCount,
    maxDrawdownPct,
    avgSharpeRatio,
    totalTrades,
  };
}
