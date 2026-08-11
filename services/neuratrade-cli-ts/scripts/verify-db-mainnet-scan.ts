#!/usr/bin/env bun
/**
 * Verify the mainnet-fidelity funnel on BTC/SOL/ETH.
 *
 * Usage:
 *   bun scripts/verify-db-mainnet-scan.ts db-mainnet [--tier fast|readiness]
 *   bun scripts/verify-db-mainnet-scan.ts gateway    [--tier fast|readiness]   # testnet-fed BEFORE run (no DB writes)
 *
 * Prints per-symbol: candles, best params, walk-forward edge, fills/day,
 * avg win/loss, breakeven win rate, gate verdict.
 * A second db-mainnet pass with relaxed walk-forward thresholds isolates the
 * structural-asymmetry gate (so a config that fails walk-forward profitability
 * is still shown for its BE profile).
 */
import { Database } from "bun:sqlite";
import { Effect, Layer } from "effect";
import {
  MarketDataRepository,
  MarketDataRepositorySQLite,
  type MarketDataRepositoryService,
} from "../src/market-data/repository.js";
import { MarketDataGatewayLive } from "../src/market-data/gateways/index.js";
import {
  breakevenWinRateFromWalkForward,
  DEFAULT_GRID_UNIVERSE_SEARCH_SPACE,
  resampleCandles,
  runMarketUniverseScan,
  type GridUniverseOptions,
} from "../src/scalping/grid-universe.js";
import { runGridBacktest, type GridOptions } from "../src/scalping/grid.js";

const HOME = process.env.NEURATRADE_HOME ?? `${process.env.HOME}/.neuratrade`;
const mode = process.argv[2] === "gateway" ? "gateway" : "db-mainnet";
const tierArg = process.argv.indexOf("--tier");
const tier = tierArg >= 0 ? (process.argv[tierArg + 1] as "readiness" | "fast") : "readiness";
// --no-asymmetry disables the structural-asymmetry gate (maxBreakevenWinRate
// 1.0) — reproduces the pre-2026-08-11 pipeline for BEFORE numbers.
const noAsymmetry = process.argv.includes("--no-asymmetry");

const ALLOWED: Record<string, true> = {
  "BTC/USDT:USDT": true,
  "SOL/USDT:USDT": true,
  "ETH/USDT:USDT": true,
};

const db = new Database(`${HOME}/data/neuratrade.db`, { readonly: true });
const sqlite = new MarketDataRepositorySQLite(db);

// The universe is restricted to BTC/SOL/ETH; candle reads/writes go to the
// real repository. saveCandles is a no-op counter so the gateway (testnet)
// before-run evaluates exactly like a real scan WITHOUT mutating the DB.
let saveCalls = 0;
const repo: MarketDataRepositoryService = {
  saveTick: (t) => sqlite.saveTick(t),
  saveCandles: (c) =>
    Effect.sync(() => {
      saveCalls += c.length;
      return 0;
    }),
  getCandles: (q) => sqlite.getCandles(q),
  getLatestTick: (symbol: string, timeframe: string) => sqlite.getLatestTick(symbol, timeframe),
  listSymbols: (ex, tf, m) => sqlite.listSymbols(ex, tf, m),
  listSymbolsByCandleCount: (ex, tf, limit) =>
    Effect.gen(function* () {
      const out: { symbol: string; count: number }[] = [];
      for (const symbol of ["BTC/USDT:USDT", "SOL/USDT:USDT", "ETH/USDT:USDT"]) {
        if (!ALLOWED[symbol]) continue;
        const range = yield* sqlite.getCandleRange(ex, symbol, tf);
        if (range.count > 0) out.push({ symbol, count: range.count });
      }
      return out;
    }),
  deleteCandles: (ex, s, tf) => sqlite.deleteCandles(ex, s, tf),
  getCandleRange: (ex, s, tf) => sqlite.getCandleRange(ex, s, tf),
  getCoverageReport: (ex, s, tf, st, en) =>
    sqlite.getCoverageReport(ex, s, tf, st, en),
  ensureTables: () => sqlite.ensureTables(),
  ensureFundingRatesTable: () => sqlite.ensureFundingRatesTable(),
  saveFundingRates: (ex, s, r) => sqlite.saveFundingRates(ex, s, r),
  getFundingRates: (ex, s, st, en) => sqlite.getFundingRates(ex, s, st, en),
  getLatestFundingRateBefore: (ex, s, t) =>
    sqlite.getLatestFundingRateBefore(ex, s, t),
};

function runScan(
  options: GridUniverseOptions,
): Promise<void> {
  return Effect.runPromise(
    Effect.provide(
      Effect.provide(
        runMarketUniverseScan(options),
        Layer.succeed(MarketDataRepository, repo),
      ),
      MarketDataGatewayLive,
    ),
  ).then((result) => {
    console.log(
      "SYMBOL          candles  step  grids pause  profWin%   aggRet%  fills/day  avgWin%  avgLoss%    BE  passed  gated",
    );
    for (const e of result.entries) {
      const be = breakevenWinRateFromWalkForward(e.walkForward);
      const gated = e.gatedDropped ? "drop" : e.validatedTargetRatio !== undefined ? "ok" : "-";
      console.log(
        `${e.symbol.padEnd(15)} ${String(e.candles).padStart(7)}  ` +
          `${e.bestParams.gridStepPct.toFixed(2).padStart(5)}  ` +
          `${String(e.bestParams.gridMaxGrids).padStart(4)}  ` +
          `${String(e.bestParams.gridPauseAfterLossBars).padStart(5)}  ` +
          `${e.walkForward.profitableWindowsPct.toFixed(0).padStart(6)}%  ` +
          `${e.walkForward.aggregateReturnPct.toFixed(2).padStart(8)}%  ` +
          `${(e.fillsPerDay ?? 0).toFixed(2).padStart(8)}  ` +
          `${(e.walkForward.avgWinPct ?? NaN).toFixed(2).padStart(7)}  ` +
          `${(e.walkForward.avgLossPct ?? NaN).toFixed(2).padStart(8)}  ` +
          `${be === undefined ? "  n/a" : be.toFixed(3).padStart(6)}  ` +
          `${e.passed ? "pass" : "FAIL"}  ${gated}`,
      );
    }
    console.log(
      `  -> ${result.entries.length} entries, ${result.survivors.length} survivors, ${result.gateDropped ?? 0} gate-dropped; saveCandles calls: ${saveCalls}`,
    );
  });
}

const base: GridUniverseOptions = {
  exchange: "bybit-futures",
  timeframe: "15m",
  initialCapital: 10000,
  minCandles: 500,
  trainWindow: 180,
  testWindow: 60,
  minProfitableWindowsPct: 60,
  minAggregateReturnPct: 0,
  minFillFrequencyPct: 10,
  feePct: 0.06,
  slippageBps: 2,
  trendFilterPeriod: 0,
  searchSpace: DEFAULT_GRID_UNIVERSE_SEARCH_SPACE,
  tier,
  dataSource: mode,
  fillModel: mode === "db-mainnet" ? "conservative" : "wick",
  maxBreakevenWinRate: noAsymmetry ? 1 : 0.4,
  deepFetchBudgetPerCycle: 15,
};

async function main(): Promise<void> {
  const gateLabel = noAsymmetry ? "asymmetry gate OFF" : "asymmetry gate ON (BE<=0.40)";
  console.log(
    `\n=== ${mode === "db-mainnet" ? "AFTER (db-mainnet, conservative fills)" : "BEFORE (gateway/testnet-fed, wick fills)"} — tier=${tier}, ${gateLabel} ===`,
  );
  await runScan({ ...base });

  if (mode === "db-mainnet") {
    // Isolate the structural-asymmetry gate: relax the walk-forward
    // profitability bars so any surviving asymmetry rejection is visible.
    console.log(
      "\n=== db-mainnet with relaxed walk-forward thresholds (minAggregateReturnPct=-100, minProfitableWindowsPct=0) — isolates the asymmetry gate ===",
    );
    await runScan({
      ...base,
      minAggregateReturnPct: -100,
      minProfitableWindowsPct: 0,
    });

    // Config-level check on the FULL 12-month mainnet series: the funnel's
    // gate dials (target/ADX) are what made the old BTC/SOL configs
    // profitable (+9.1%/+25.3% per the 2026-08-11 scout) — a 500-bar
    // walk-forward with targetRatio 1 cannot see that edge. Verify the
    // manifest configs honestly on mainnet data.
    console.log(
      "\n=== config-level 12-month MAINNET backtests (full resampled 15m series, ~34.5k bars) ===",
    );
    console.log(
      "config                       return%   maxDD%  winRate  trades   PF",
    );
    const configs: { label: string; symbol: string; grid: GridOptions }[] = [
      {
        label: "ETH 1.25/1/0 t1 a24 (old funnel pick)",
        symbol: "ETH/USDT:USDT",
        grid: {
          gridStepPct: 1.25,
          gridMaxGrids: 1,
          gridPauseAfterLossBars: 0,
          targetRatio: 1,
          chopGateAdxThreshold: 24,
          feePct: 0.06,
          slippageBps: 2,
          initialCapital: 10000,
          trendFilterPeriod: 0,
          leverage: 1,
          positionFraction: 0.5,
        },
      },
      {
        label: "BTC 1.0/1.5/24 t3 a28 (old Bitget-era)",
        symbol: "BTC/USDT:USDT",
        grid: {
          gridStepPct: 1.0,
          gridMaxGrids: 1.5,
          gridPauseAfterLossBars: 24,
          targetRatio: 3,
          chopGateAdxThreshold: 28,
          feePct: 0.06,
          slippageBps: 2,
          initialCapital: 10000,
          trendFilterPeriod: 0,
          leverage: 1,
          positionFraction: 0.5,
        },
      },
      {
        label: "SOL 1.25/2/36 t4 a26 (old Bitget-era)",
        symbol: "SOL/USDT:USDT",
        grid: {
          gridStepPct: 1.25,
          gridMaxGrids: 2,
          gridPauseAfterLossBars: 36,
          targetRatio: 4,
          chopGateAdxThreshold: 26,
          feePct: 0.06,
          slippageBps: 2,
          initialCapital: 10000,
          trendFilterPeriod: 0,
          leverage: 1,
          positionFraction: 0.5,
        },
      },
    ];
    for (const { label, symbol, grid } of configs) {
      const fiveMin = await Effect.runPromise(
        sqlite.getCandles({
          exchange: "bybit-futures",
          symbol,
          timeframe: "5m",
          limit: 103_680,
        }),
      );
      const series = resampleCandles(fiveMin, 15, "15m");
      const result = runGridBacktest(series, grid);
      console.log(
        `${label.padEnd(42)} ${result.totalReturnPct.toFixed(2).padStart(8)}%  ` +
          `${result.maxDrawdownPct.toFixed(1).padStart(6)}%  ` +
          `${result.winRate.toFixed(1).padStart(7)}%  ` +
          `${String(result.totalTrades).padStart(6)}  ` +
          `${(result.profitFactor === Infinity ? "inf" : result.profitFactor.toFixed(2)).padStart(5)}`,
      );
    }
  }
}

await main().catch((err) => {
  console.error(err);
  process.exit(1);
});
