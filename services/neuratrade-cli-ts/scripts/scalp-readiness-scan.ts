#!/usr/bin/env bun
/**
 * Scalp readiness sweep (bd clever-cabin-3gr).
 *
 * Sweeps the scalping parameter space on stored candles and ranks configs by
 * in-sample expectancy with scalping floors (frequency, hold time, positive
 * economics). OOS + Monte Carlo validation happens afterwards via
 * `scalp readiness` on the winning profile — this script is the fast filter.
 *
 * Usage:
 *   bun run scripts/scalp-readiness-scan.ts \
 *     --exchange bitget-futures --symbol BTC/USDT:USDT --timeframe 5m \
 *     [--capital 10000] [--fee 0.06] [--slippage-bps 2] [--leverage 1] \
 *     [--top 15] [--out ~/.neuratrade/tuning/BTC-5m-sweep.json]
 */
import { Database } from "bun:sqlite";
import { mkdirSync } from "node:fs";
import { dirname, join } from "node:path";
import { runBacktest } from "../src/scalping/backtest.js";
import { defaultComposerConfig } from "../src/scalping/composer.js";
import type { Candle, FundingRate } from "../src/market-data/types.js";

interface Args {
  exchange: string;
  symbol: string;
  timeframe: string;
  capital: number;
  fee: number;
  makerFee: number;
  slippageBps: number;
  leverage: number;
  entryOrderType: "market" | "limit";
  limitOffsetBps: number;
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
    timeframe: get("--timeframe", "5m"),
    capital: Number(get("--capital", "10000")),
    fee: Number(get("--fee", "0.06")),
    makerFee: Number(get("--maker-fee", "0.02")),
    slippageBps: Number(get("--slippage-bps", "2")),
    leverage: Number(get("--leverage", "1")),
    entryOrderType: get("--entry-order-type", "market") as "market" | "limit",
    limitOffsetBps: Number(get("--limit-offset-bps", "0")),
    top: Number(get("--top", "15")),
    out: get(
      "--out",
      join(
        home,
        "tuning",
        `sweep-${get("--symbol", "BTC/USDT:USDT").replace(/[/:]/g, "_")}-${get("--timeframe", "5m")}.json`,
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

const fundingRows = db
  .query(
    `SELECT funding_rate, timestamp FROM funding_rates
     WHERE exchange = ? AND symbol = ? ORDER BY timestamp ASC`,
  )
  .all(args.exchange, args.symbol) as Array<{
  funding_rate: number;
  timestamp: string;
}>;

const fundingRates: FundingRate[] = fundingRows.map((r) => ({
  exchange: args.exchange,
  symbol: args.symbol,
  fundingRate: r.funding_rate,
  timestamp: new Date(
    r.timestamp.endsWith("Z")
      ? r.timestamp
      : r.timestamp.replace(" ", "T") + "Z",
  ),
}));

db.close();

const first = candles[0].timestamp.getTime();
const last = candles[candles.length - 1].timestamp.getTime();
const months = Math.max((last - first) / (30.44 * 24 * 3600 * 1000), 1e-9);

console.log(
  `Sweeping ${args.exchange}:${args.symbol}:${args.timeframe} — ${candles.length} candles over ${months.toFixed(1)} months, ${fundingRates.length} funding rows`,
);

// Scalping search space (design doc D4): tight ATR stops, fee-aware TP,
// relaxed confidence, short time-stops, both regimes.
const REGIMES = ["trend", "reversion"] as const;
const STOP_MULTS = [0.5, 0.75, 1.0, 1.5];
const TP_MULTS = [1.0, 1.5, 2.0, 2.5];
const CONFIDENCES = [0.35, 0.45, 0.55];
const MAX_BARS = [6, 12, 24, 48];

interface Row {
  regime: string;
  stopMult: number;
  tpMult: number;
  conf: number;
  maxBars: number;
  trades: number;
  tradesPerMonth: number;
  winRate: number;
  profitFactor: number;
  expectancy: number;
  returnPct: number;
  maxDdPct: number;
  avgDurationH: number;
  sharpe: number;
  oosTrades: number;
  oosWinRate: number;
  oosProfitFactor: number;
  oosReturnPct: number;
}

const rows: Row[] = [];
let tested = 0;
const total =
  REGIMES.length *
  STOP_MULTS.length *
  TP_MULTS.length *
  CONFIDENCES.length *
  MAX_BARS.length;
const started = Date.now();

// Honest protocol (bd clever-cabin-dt8): full-period runs are look-ahead
// inflated, so every candidate is evaluated with an OOS split and ranked on
// in-sample metrics, with OOS columns for the robustness check.
const OOS_PCT = 20;
const isMonths = months * (1 - OOS_PCT / 100);

for (const regime of REGIMES) {
  for (const stopMult of STOP_MULTS) {
    for (const tpMult of TP_MULTS) {
      for (const conf of CONFIDENCES) {
        for (const maxBars of MAX_BARS) {
          const composerConfig = {
            weights: defaultComposerConfig.weights,
            thresholds: {
              ...defaultComposerConfig.thresholds,
              regimeMode: regime,
            },
          };
          const result = runBacktest({
            symbol: args.symbol,
            exchange: args.exchange,
            timeframe: args.timeframe,
            candles,
            composerConfig,
            initialCapital: args.capital,
            positionSizePct: 100,
            stopLossPct: 0,
            takeProfitPct: 0,
            feePct: args.fee,
            makerFeePct:
              args.entryOrderType === "limit" ? args.makerFee : undefined,
            entryOrderType: args.entryOrderType,
            entryLimitOffsetBps: args.limitOffsetBps,
            minConfidence: conf,
            useAtrStops: true,
            atrStopMultiplier: stopMult,
            atrTakeProfitMultiplier: tpMult,
            isFutures: true,
            slippageBps: args.slippageBps,
            leverage: args.leverage,
            maxBarsInTrade: maxBars,
            fundingRates,
            recordEquityCurve: false,
            htfCandles: [],
            oosPct: OOS_PCT,
          });
          const oos = result.oosResult;
          rows.push({
            regime,
            stopMult,
            tpMult,
            conf,
            maxBars,
            trades: result.totalTrades,
            tradesPerMonth: result.totalTrades / isMonths,
            winRate: result.winRate,
            profitFactor: result.metrics.profitFactor,
            expectancy: result.metrics.expectancy,
            returnPct: result.totalReturnPct,
            maxDdPct: result.maxDrawdownPct,
            avgDurationH: result.metrics.averageTradeDurationHours,
            sharpe: result.sharpeRatio,
            oosTrades: oos?.totalTrades ?? 0,
            oosWinRate: oos?.winRate ?? 0,
            oosProfitFactor: oos?.metrics.profitFactor ?? 0,
            oosReturnPct: oos?.totalReturnPct ?? -999,
          });
          tested += 1;
          if (tested % 32 === 0 || tested === total) {
            const elapsed = (Date.now() - started) / 1000;
            const eta = (elapsed / tested) * (total - tested);
            console.log(
              `progress ${tested}/${total} (${((tested / total) * 100).toFixed(0)}%) elapsed ${elapsed.toFixed(0)}s eta ${eta.toFixed(0)}s`,
            );
          }
        }
      }
    }
  }
}

// Scalping floors before ranking (design doc G1/G4 + positive economics + OOS).
const FLOOR_TRADES_PER_MONTH = 15;
const FLOOR_MAX_DURATION_H = 4;
const FLOOR_OOS_TRADES = 10;

const passing = rows.filter(
  (r) =>
    r.tradesPerMonth >= FLOOR_TRADES_PER_MONTH &&
    r.avgDurationH <= FLOOR_MAX_DURATION_H &&
    r.expectancy > 0 &&
    r.trades >= 50 &&
    r.oosTrades >= FLOOR_OOS_TRADES &&
    r.oosReturnPct >= 0,
);

const ranked = [...passing].sort(
  (a, b) => b.expectancy - a.expectancy || b.sharpe - a.sharpe,
);

console.log(
  `\n${rows.length} configs tested, ${passing.length} pass floors (IS >=${FLOOR_TRADES_PER_MONTH} tr/mo & <=${FLOOR_MAX_DURATION_H}h & exp>0 & >=50 trades; OOS >=${FLOOR_OOS_TRADES} trades & return>=0)`,
);
console.log(
  `${"regime".padEnd(10)} ${"stop".padStart(5)} ${"tp".padStart(5)} ${"conf".padStart(5)} ${"bars".padStart(5)} ${"tr/mo".padStart(7)} ${"win%".padStart(6)} ${"PF".padStart(6)} ${"exp%".padStart(7)} ${"ret%".padStart(8)} ${"dd%".padStart(6)} ${"durH".padStart(6)} ${"oosTr".padStart(6)} ${"oosRet%".padStart(8)} ${"oosPF".padStart(6)}`,
);
for (const r of ranked.slice(0, args.top)) {
  console.log(
    `${r.regime.padEnd(10)} ${r.stopMult.toFixed(2).padStart(5)} ${r.tpMult.toFixed(2).padStart(5)} ${r.conf.toFixed(2).padStart(5)} ${String(r.maxBars).padStart(5)} ${r.tradesPerMonth.toFixed(1).padStart(7)} ${(r.winRate * 100).toFixed(1).padStart(6)} ${r.profitFactor.toFixed(2).padStart(6)} ${r.expectancy.toFixed(3).padStart(7)} ${r.returnPct.toFixed(2).padStart(8)} ${r.maxDdPct.toFixed(1).padStart(6)} ${r.avgDurationH.toFixed(2).padStart(6)} ${String(r.oosTrades).padStart(6)} ${r.oosReturnPct.toFixed(2).padStart(8)} ${r.oosProfitFactor.toFixed(2).padStart(6)}`,
  );
}

mkdirSync(dirname(args.out), { recursive: true });
const payload = {
  meta: {
    exchange: args.exchange,
    symbol: args.symbol,
    timeframe: args.timeframe,
    candles: candles.length,
    months,
    fee: args.fee,
    makerFee: args.entryOrderType === "limit" ? args.makerFee : null,
    entryOrderType: args.entryOrderType,
    limitOffsetBps: args.limitOffsetBps,
    slippageBps: args.slippageBps,
    leverage: args.leverage,
    fundingRows: fundingRates.length,
    tested: rows.length,
    passing: passing.length,
    floors: {
      tradesPerMonth: FLOOR_TRADES_PER_MONTH,
      maxDurationH: FLOOR_MAX_DURATION_H,
    },
    ranAt: new Date().toISOString(),
  },
  top: ranked.slice(0, args.top),
  all: rows,
};
await Bun.write(args.out, JSON.stringify(payload, null, 2));
console.log(`\nWrote ${args.out}`);
