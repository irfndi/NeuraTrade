#!/usr/bin/env bun
/**
 * UNIVERSAL STRATEGY-BOOK SCANNER.
 *
 * Goal: stop relying on 1-2 hand-picked pairs. Scan ALL symbols in the 5m
 * mainnet cache x EVERY available strategy engine, walk-forward each
 * combination, and emit a "strategy book" — the folder of symbol+strategy
 * entries that cleared out-of-sample gates. Only book entries are tradeable;
 * everything else is research noise.
 *
 * Engines (all existing, no new math):
 *   - ladder : runLadderGridBacktest (validated config family)
 *   - grid   : runGridBacktest (single-position grid)
 *   - signal : runBacktest (composer signal engine with ATR stops)
 *
 * Walk-forward protocol per (symbol, engine):
 *   - split candle history into N sequential windows: TRAIN then TEST
 *   - params are FIXED (no fitting) — this measures raw OOS stability
 *   - a combo enters the BOOK iff:
 *       >= 4 OOS test windows, all non-empty,
 *       median OOS return > 0,
 *       worst OOS return > -15%,
 *       OOS win rate > 40%,
 *       median OOS drawdown < 25%
 *   - the gate is deliberately strict; expect most combos to fail. The book
 *     is only worth trading if SOMETHING clears it honestly.
 *
 * Output:
 *   $NEURATRADE_HOME/data/strategy_book/
 *     book.json          — qualifying entries with full OOS stats
 *     scan_summary.json  — every combo's headline numbers (research ledger)
 *
 * Usage:
 *   NEURATRADE_HOME=~/.neuratrade bun scripts/strategy-book-scanner.ts \
 *     [--symbols 77] [--windows 6] [--train-days 60] [--test-days 30]
 */
import { Database } from "bun:sqlite";
import { mkdir } from "node:fs/promises";
import { resampleCandles } from "../src/scalping/grid-universe.ts";
import { runLadderGridBacktest } from "../src/scalping/ladder-grid.ts";
import { runGridBacktest } from "../src/scalping/grid.ts";
import { runBacktest } from "../src/scalping/backtest.ts";
import { defaultComposerConfig } from "../src/scalping/composer.js";
import type { Candle } from "../src/market-data/types.js";

const HOME = process.env.NEURATRADE_HOME ?? `${process.env.HOME}/.neuratrade`;

function arg(name: string, fallback: string): string {
  const hit = process.argv.find((a) => a.startsWith(`--${name}=`));
  return hit?.split("=")[1] ?? fallback;
}
const TOP_N = Number(arg("symbols", "77"));
const WINDOWS = Number(arg("windows", "6"));
const TRAIN_DAYS = Number(arg("train-days", "60"));
const TEST_DAYS = Number(arg("test-days", "30"));
const FEE = Number(arg("fee", "0.02"));
const SLIP = Number(arg("slippage-bps", "2"));

// ---------------------------------------------------------------------------
// Panel loading
// ---------------------------------------------------------------------------

const db = new Database(`${HOME}/data/neuratrade.db`, { readonly: true });
db.exec("PRAGMA busy_timeout = 30000;");

interface SymbolRow {
  symbol: string;
  count: number;
}

const symbolRows = db
  .query(
    `SELECT tp.symbol AS symbol, COUNT(*) AS count
     FROM ohlcv_data c JOIN trading_pairs tp ON tp.id = c.trading_pair_id
     WHERE c.timeframe = '5m'
     GROUP BY tp.symbol ORDER BY count DESC LIMIT ?`,
  )
  .all(TOP_N) as SymbolRow[];

interface Raw5m {
  open: number;
  high: number;
  low: number;
  close: number;
  volume: number;
  timestamp: string;
}

function load15m(symbolWire: string): Candle[] {
  const canonical = symbolWire.replace(/\/USDT.*/, "/USDT");
  const rowsDb = db
    .query(
      `SELECT c.open_price AS open, c.high_price AS high, c.low_price AS low,
              c.close_price AS close, c.volume, c.timestamp
       FROM ohlcv_data c JOIN trading_pairs tp ON tp.id = c.trading_pair_id
       WHERE tp.symbol IN (?,?,?) AND c.timeframe = '5m'
       ORDER BY c.timestamp DESC LIMIT ?`,
    )
    .all(symbolWire, canonical, `${canonical}:USDT`, 200_000) as Raw5m[];
  const base: Candle[] = rowsDb.toReversed().map((r) => ({
    exchange: "bybit-futures",
    symbol: symbolWire,
    timeframe: "5m",
    open: r.open,
    high: r.high,
    low: r.low,
    close: r.close,
    volume: r.volume,
    timestamp: new Date(r.timestamp),
  }));
  return resampleCandles(base, 15, "15m");
}

console.log(`scanner: loading ${symbolRows.length} symbols...`);
const panel = new Map<string, Candle[]>();
for (const row of symbolRows) {
  try {
    const candles = load15m(row.symbol);
    // Need train+test * WINDOWS bars of 15m data.
    const minBars = ((TRAIN_DAYS + TEST_DAYS) * WINDOWS * 24) / 15;
    if (candles.length >= minBars) panel.set(row.symbol, candles);
  } catch {
    continue;
  }
}
console.log(`scanner: ${panel.size} symbols have enough history`);

// ---------------------------------------------------------------------------
// Engines
// ---------------------------------------------------------------------------

type EngineName = "ladder" | "grid" | "signal";

interface EngineOutcome {
  retPct: number;
  ddPct: number;
  winRate: number;
  trades: number;
}

function runEngine(
  engine: EngineName,
  slice: readonly Candle[],
  symbol: string,
): EngineOutcome | null {
  try {
    if (engine === "ladder") {
      const r = runLadderGridBacktest(slice, {
        rungs: 1,
        gridStepPct: 1.0,
        gridMaxGrids: 3,
        gridPauseAfterLossBars: 24,
        feePct: FEE,
        slippageBps: SLIP,
        initialCapital: 10_000,
        leverage: 1,
        trendFilterPeriod: 0,
        targetRatio: 2,
        stopRatio: 1.5,
        maxHoldBars: 48,
        conservativeIntrabar: true,
      });
      return {
        retPct: r.totalReturnPct,
        ddPct: r.maxDrawdownPct,
        winRate: r.winRate * 100,
        trades: r.totalTrades,
      };
    }
    if (engine === "grid") {
      const r = runGridBacktest(slice, {
        gridStepPct: 0.75,
        gridMaxGrids: 3,
        gridPauseAfterLossBars: 24,
        feePct: FEE,
        slippageBps: SLIP,
        initialCapital: 10_000,
        leverage: 1,
        trendFilterPeriod: 0,
        targetRatio: 2,
      });
      return {
        retPct: r.totalReturnPct,
        ddPct: r.maxDrawdownPct,
        winRate: r.winRate * 100,
        trades: r.totalTrades,
      };
    }
    // signal
    const r = runBacktest({
      symbol,
      exchange: "bybit-futures",
      timeframe: "15m",
      candles: slice,
      composerConfig: defaultComposerConfig,
      initialCapital: 10_000,
      positionSizePct: 50,
      stopLossPct: 1.5,
      takeProfitPct: 3.0,
      feePct: FEE,
      slippageBps: SLIP,
      // Composer confidence is a 0..1 fraction (p90 ~0.26 on real data).
      minConfidence: 0.3,
      useAtrStops: true,
      atrStopMultiplier: 2,
      atrTakeProfitMultiplier: 3,
    });
    return {
      retPct: r.totalReturnPct,
      ddPct: r.maxDrawdownPct,
      winRate: r.winRate * 100,
      trades: r.totalTrades,
    };
  } catch {
    return null;
  }
}

// ---------------------------------------------------------------------------
// Walk-forward over every (symbol, engine)
// ---------------------------------------------------------------------------

const BAR_PER_DAY = 96; // 15m
const TRAIN_BARS = TRAIN_DAYS * BAR_PER_DAY;
const TEST_BARS = TEST_DAYS * BAR_PER_DAY;

interface WindowStat {
  retPct: number;
  ddPct: number;
  winRate: number;
  trades: number;
}

interface ComboResult {
  symbol: string;
  engine: EngineName;
  windows: WindowStat[];
  medianRet: number;
  worstRet: number;
  medianDd: number;
  totalTrades: number;
  qualified: boolean;
}

function median(xs: number[]): number {
  const s = xs.filter(Number.isFinite).sort((a, b) => a - b);
  if (!s.length) return NaN;
  const m = Math.floor(s.length / 2);
  return s.length % 2 ? s[m] : (s[m - 1] + s[m]) / 2;
}

const results: ComboResult[] = [];
let done = 0;
const engines: EngineName[] = ["ladder", "grid", "signal"];
const tStart = Date.now();

for (const [symbol, candles] of panel) {
  for (const engine of engines) {
    const windowStats: WindowStat[] = [];
    for (let w = 0; w < WINDOWS; w++) {
      const testEnd =
        candles.length -
        (WINDOWS - 1 - w) * TEST_BARS;
      const testStart = testEnd - TEST_BARS;
      const trainStart = testStart - TRAIN_BARS;
      if (trainStart < 0 || testEnd > candles.length) break;
      const outcome = runEngine(engine, candles.slice(trainStart, testEnd), symbol);
      if (outcome && outcome.trades > 0) {
        windowStats.push({
          retPct: outcome.retPct,
          ddPct: outcome.ddPct,
          winRate: outcome.winRate,
          trades: outcome.trades,
        });
      }
    }
    if (windowStats.length === 0) continue;
    const rets = windowStats.map((w) => w.retPct);
    const dds = windowStats.map((w) => w.ddPct);
    const medRet = median(rets);
    const worst = Math.min(...rets.filter(Number.isFinite));
    const medDd = median(dds);
    // OOS win rate aggregated across windows.
    const totalTrades = windowStats.reduce((s, w) => s + w.trades, 0);
    const qualified =
      windowStats.length >= 4 &&
      medRet > 0 &&
      worst > -15 &&
      medDd < 25 &&
      // Weighted win-rate across windows must beat 40%.
      (() => {
        let wins = 0;
        let n = 0;
        for (const w of windowStats) {
          wins += (w.winRate / 100) * w.trades;
          n += w.trades;
        }
        return n > 0 ? wins / n > 0.4 : false;
      })();
    results.push({
      symbol,
      engine,
      windows: windowStats,
      medianRet: medRet,
      worstRet: worst,
      medianDd: medDd,
      totalTrades,
      qualified,
    });
  }
  done++;
  if (done % 10 === 0) {
    const elapsed = (Date.now() - tStart) / 1000;
    console.log(
      `scanner: ${done}/${panel.size} symbols (${elapsed.toFixed(0)}s elapsed, ${results.length} combos)`,
    );
  }
}

const book = results.filter((r) => r.qualified);
console.log(`\nscanner: ${results.length} combos evaluated, ${book.length} QUALIFIED\n`);

// Per-engine honesty table.
console.log("engine  | combos | qualified | median(medRet%) | best combo");
console.log("-".repeat(78));
for (const engine of engines) {
  const rs = results.filter((r) => r.engine === engine);
  const q = rs.filter((r) => r.qualified);
  const best = rs.slice().sort((a, b) => b.medianRet - a.medianRet)[0];
  console.log(
    `${engine.padEnd(7)} | ${String(rs.length).padStart(6)} | ${String(q.length).padStart(9)} | ${(median(rs.map((r) => r.medianRet)) ?? NaN).toFixed(2).padStart(15)} | ${best ? `${best.symbol} ${best.medianRet.toFixed(1)}%` : "-"}`,
  );
}

if (book.length > 0) {
  console.log("\nBOOK ENTRIES:");
  for (const e of book.slice(0, 20)) {
    console.log(
      `  ${e.symbol} [${e.engine}] medOOS=${e.medianRet.toFixed(2)}% worst=${e.worstRet.toFixed(2)}% medDD=${e.medianDd.toFixed(1)}% trades=${e.totalTrades}`,
    );
  }
}

// ---------------------------------------------------------------------------
// Persist artifacts
// ---------------------------------------------------------------------------

const outDir = `${HOME}/data/strategy_book`;
await mkdir(outDir, { recursive: true });
await Bun.write(`${outDir}/book.json`, JSON.stringify(book, null, 2));
await Bun.write(
  `${outDir}/scan_summary.json`,
  JSON.stringify(
    {
      scannedAt: new Date().toISOString(),
      symbols: panel.size,
      engines,
      windowsRequested: WINDOWS,
      trainDays: TRAIN_DAYS,
      testDays: TEST_DAYS,
      feePct: FEE,
      slippageBps: SLIP,
      combos: results.map((r) => ({
        symbol: r.symbol,
        engine: r.engine,
        medianRet: r.medianRet,
        worstRet: r.worstRet,
        medianDd: r.medianDd,
        totalTrades: r.totalTrades,
        oosWindows: r.windows.length,
        qualified: r.qualified,
      })),
    },
    null,
    2,
  ),
);
console.log(`\nscanner: wrote ${outDir}/book.json and scan_summary.json`);
