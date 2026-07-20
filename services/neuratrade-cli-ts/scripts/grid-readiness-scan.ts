#!/usr/bin/env bun
/**
 * Grid-scalping readiness sweep (iteration 3 — market-neutral family).
 *
 * The signal-composer sweeps show no directional edge at scalping frequency
 * on bitget-futures data. Grid scalping profits from oscillation WITHOUT
 * directional prediction and assumes maker fills (~0.04% round trip), so it
 * is the most promising remaining family. Same honest protocol as
 * scalp-readiness-scan: every candidate is evaluated IS/OOS (80/20 split).
 *
 * Usage:
 *   bun run scripts/grid-readiness-scan.ts \
 *     --exchange bitget-futures --symbol BTC/USDT:USDT --timeframe 15m \
 *     [--fee 0.02] [--slippage-bps 1] [--leverage 1] [--top 15] [--out path]
 */
import { Database } from "bun:sqlite";
import { mkdirSync } from "node:fs";
import { dirname, join } from "node:path";
import { runGridBacktest } from "../src/scalping/grid.js";
import type { Candle } from "../src/market-data/types.js";

interface Args {
  exchange: string;
  symbol: string;
  timeframe: string;
  capital: number;
  fee: number;
  slippageBps: number;
  leverage: number;
  top: number;
  out: string;
}

function parseArgs(argv: readonly string[]): Args {
  const get = (name: string, dflt: string): string => {
    const i = argv.indexOf(name);
    return i >= 0 && i + 1 < argv.length ? argv[i + 1] : dflt;
  };
  const home =
    process.env.NEURATRADE_HOME ?? join(process.env.HOME!, ".neuratrade");
  return {
    exchange: get("--exchange", "bitget-futures"),
    symbol: get("--symbol", "BTC/USDT:USDT"),
    timeframe: get("--timeframe", "15m"),
    capital: Number(get("--capital", "10000")),
    fee: Number(get("--fee", "0.02")),
    slippageBps: Number(get("--slippage-bps", "1")),
    leverage: Number(get("--leverage", "1")),
    top: Number(get("--top", "15")),
    out: get(
      "--out",
      join(
        home,
        "tuning",
        `grid-sweep-${get("--symbol", "BTC/USDT:USDT").replace(/[/:]/g, "_")}-${get("--timeframe", "15m")}.json`,
      ),
    ),
  };
}

const args = parseArgs(process.argv.slice(2));
const home =
  process.env.NEURATRADE_HOME ?? join(process.env.HOME!, ".neuratrade");
const db = new Database(join(home, "data", "neuratrade.db"), {
  readonly: true,
});

const candleRows = db
  .query(
    `SELECT o.open_price, o.high_price, o.low_price, o.close_price, o.volume, o.timestamp
     FROM ohlcv_data o
     JOIN exchanges e ON e.id = o.exchange_id
     JOIN trading_pairs tp ON tp.id = o.trading_pair_id
     WHERE e.name = ? AND tp.symbol = ? AND o.timeframe = ?
     ORDER BY o.timestamp ASC`,
  )
  .all(args.exchange, args.symbol, args.timeframe) as Array<{
  open_price: number;
  high_price: number;
  low_price: number;
  close_price: number;
  volume: number;
  timestamp: string;
}>;

if (candleRows.length === 0) {
  console.error(
    `No candles for ${args.exchange}:${args.symbol}:${args.timeframe}`,
  );
  process.exit(1);
}
db.close();

const candles: Candle[] = candleRows.map((r) => ({
  exchange: args.exchange,
  symbol: args.symbol,
  timeframe: args.timeframe,
  open: r.open_price,
  high: r.high_price,
  low: r.low_price,
  close: r.close_price,
  volume: r.volume,
  timestamp: new Date(
    r.timestamp.endsWith("Z")
      ? r.timestamp
      : r.timestamp.replace(" ", "T") + "Z",
  ),
}));

const OOS_PCT = 20;
const cut = Math.floor(candles.length * (1 - OOS_PCT / 100));
const isCandles = candles.slice(0, cut);
const oosCandles = candles.slice(cut);

const first = candles[0].timestamp.getTime();
const last = candles[candles.length - 1].timestamp.getTime();
const months = Math.max((last - first) / (30.44 * 24 * 3600 * 1000), 1e-9);
const isMonths = months * (1 - OOS_PCT / 100);

console.log(
  `Grid sweep ${args.exchange}:${args.symbol}:${args.timeframe} — ${candles.length} candles over ${months.toFixed(1)} months (IS ${isCandles.length} / OOS ${oosCandles.length})`,
);

const STEPS = [0.3, 0.5, 0.75, 1.0, 1.5, 2.0];
const MAX_GRIDS = [1.5, 2, 3];
const TARGET_RATIOS = [0.75, 1.0, 1.5];
const TREND_ONLY = [false, true];
const PAUSES = [0, 12];
const CHOP_GATES = [0, 20, 25, 30];

interface Row {
  step: number;
  maxGrids: number;
  targetRatio: number;
  onlyWithTrend: boolean;
  pause: number;
  chopGate: number;
  trades: number;
  tradesPerMonth: number;
  winRate: number;
  profitFactor: number;
  returnPct: number;
  maxDdPct: number;
  expPerTrade: number;
  oosTrades: number;
  oosWinRate: number;
  oosProfitFactor: number;
  oosReturnPct: number;
}

const rows: Row[] = [];
let tested = 0;
const total =
  STEPS.length *
  MAX_GRIDS.length *
  TARGET_RATIOS.length *
  TREND_ONLY.length *
  PAUSES.length *
  CHOP_GATES.length;
const started = Date.now();

for (const step of STEPS) {
  for (const maxGrids of MAX_GRIDS) {
    for (const targetRatio of TARGET_RATIOS) {
      for (const onlyWithTrend of TREND_ONLY) {
        for (const pause of PAUSES) {
          for (const chopGate of CHOP_GATES) {
            const opts = {
              gridStepPct: step,
              gridMaxGrids: maxGrids,
              gridPauseAfterLossBars: pause,
              feePct: args.fee,
              slippageBps: args.slippageBps,
              initialCapital: args.capital,
              trendFilterPeriod: 0,
              leverage: args.leverage,
              onlyWithTrend,
              targetRatio,
              chopGateAdxThreshold: chopGate,
            };
            const is = runGridBacktest(isCandles, opts);
            const oos = runGridBacktest(oosCandles, opts);
            rows.push({
              step,
              maxGrids,
              targetRatio,
              onlyWithTrend,
              pause,
              chopGate,
              trades: is.totalTrades,
              tradesPerMonth: is.totalTrades / isMonths,
              winRate: is.winRate,
              profitFactor: is.profitFactor,
              returnPct: is.totalReturnPct,
              maxDdPct: is.maxDrawdownPct,
              expPerTrade:
                is.totalTrades > 0 ? is.totalReturnPct / is.totalTrades : 0,
              oosTrades: oos.totalTrades,
              oosWinRate: oos.winRate,
              oosProfitFactor: oos.profitFactor,
              oosReturnPct: oos.totalReturnPct,
            });
            tested += 1;
            if (tested % 72 === 0 || tested === total) {
              const elapsed = (Date.now() - started) / 1000;
              const eta = (elapsed / tested) * (total - tested);
              console.log(
                `progress ${tested}/${total} elapsed ${elapsed.toFixed(0)}s eta ${eta.toFixed(0)}s`,
              );
            }
          }
        }
      }
    }
  }
}

// Readiness-aligned floors for grid (G1 freq, G2 economics, G3 robustness).
const minPerMonth = args.timeframe === "5m" ? 15 : 10;
const passing = rows.filter(
  (r) =>
    r.tradesPerMonth >= minPerMonth &&
    r.profitFactor >= 1.3 &&
    r.winRate >= 0.5 &&
    r.oosTrades >= 10 &&
    r.oosReturnPct >= 0 &&
    r.maxDdPct <= 15,
);

const ranked = [...passing].sort(
  (a, b) => b.returnPct - a.returnPct || b.profitFactor - a.profitFactor,
);

console.log(
  `\n${rows.length} configs tested, ${passing.length} pass floors (>=${minPerMonth} tr/mo, PF>=1.3, win>=50%, OOS>=10 trades & return>=0, DD<=15%)`,
);
console.log(
  `${"step".padStart(5)} ${"grids".padStart(6)} ${"tgtR".padStart(5)} ${"trend".padStart(6)} ${"pause".padStart(6)} ${"gate".padStart(5)} ${"tr/mo".padStart(7)} ${"win%".padStart(6)} ${"PF".padStart(6)} ${"ret%".padStart(8)} ${"dd%".padStart(6)} ${"exp/tr".padStart(7)} ${"oosTr".padStart(6)} ${"oosRet%".padStart(8)} ${"oosPF".padStart(6)}`,
);
for (const r of ranked.slice(0, args.top)) {
  console.log(
    `${r.step.toFixed(2).padStart(5)} ${r.maxGrids.toFixed(1).padStart(6)} ${r.targetRatio.toFixed(2).padStart(5)} ${String(r.onlyWithTrend).padStart(6)} ${String(r.pause).padStart(6)} ${String(r.chopGate).padStart(5)} ${r.tradesPerMonth.toFixed(1).padStart(7)} ${(r.winRate * 100).toFixed(1).padStart(6)} ${r.profitFactor.toFixed(2).padStart(6)} ${r.returnPct.toFixed(2).padStart(8)} ${r.maxDdPct.toFixed(1).padStart(6)} ${r.expPerTrade.toFixed(3).padStart(7)} ${String(r.oosTrades).padStart(6)} ${r.oosReturnPct.toFixed(2).padStart(8)} ${r.oosProfitFactor.toFixed(2).padStart(6)}`,
  );
}

// Also print the best-by-IS-return regardless of floors for diagnostics.
const byIs = [...rows].sort((a, b) => b.returnPct - a.returnPct).slice(0, 5);
console.log("\nBest by IS return (any floor):");
for (const r of byIs) {
  console.log(
    `  step ${r.step} grids ${r.maxGrids} tgtR ${r.targetRatio} trend ${r.onlyWithTrend} pause ${r.pause} gate ${r.chopGate} | tr/mo ${r.tradesPerMonth.toFixed(1)} win ${(r.winRate * 100).toFixed(1)}% PF ${r.profitFactor.toFixed(2)} ret ${r.returnPct.toFixed(2)}% dd ${r.maxDdPct.toFixed(1)}% | oos ${r.oosTrades} ${r.oosReturnPct.toFixed(2)}% PF ${r.oosProfitFactor.toFixed(2)}`,
  );
}

mkdirSync(dirname(args.out), { recursive: true });
await Bun.write(
  args.out,
  JSON.stringify(
    {
      meta: {
        exchange: args.exchange,
        symbol: args.symbol,
        timeframe: args.timeframe,
        candles: candles.length,
        months,
        fee: args.fee,
        slippageBps: args.slippageBps,
        leverage: args.leverage,
        tested: rows.length,
        passing: passing.length,
        floors: {
          tradesPerMonth: minPerMonth,
          profitFactor: 1.3,
          winRate: 0.5,
          oosTrades: 10,
          oosReturnPct: 0,
          maxDdPct: 15,
        },
        ranAt: new Date().toISOString(),
      },
      top: ranked.slice(0, args.top),
      all: rows,
    },
    null,
    2,
  ),
);
console.log(`\nWrote ${args.out}`);
