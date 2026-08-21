#!/usr/bin/env bun
/**
 * Nightly gate-watch for LIVE ladder whitelists.
 *
 * The research funnel re-scores the whole universe every cycle, but a whitelist
 * already deployed to a paper/live soak is never re-checked — a member whose
 * edge decayed (or whose candidate was curated in by hand) keeps trading with
 * nobody noticing. This script re-runs the SAME stage-4 gates (fill-frequency
 * floor + ladder evidence validator + gate criteria) on fresh DB candles for
 * exactly the symbols in the whitelist and reports who still passes.
 *
 * Exit code 1 when any member fails (cron/pm2 alertable). With --prune, the
 * whitelist file is rewritten WITHOUT failing members — never to empty; if
 * everything fails the file is left alone and the exit code says so loudly.
 *
 * Usage:
 *   NEURATRADE_HOME=~/.neuratrade bun run scripts/ladder-whitelist-gatewatch.ts \
 *     --watchlist grid-whitelist-ladder-portfolio-v1.json [--timeframe 15m] [--prune]
 */
import { Database } from "bun:sqlite";
import {
  computeFillFrequencyPct,
  ladderGateCriteriaFailures,
  passesLadderGateCriteria,
  resampleCandles,
} from "../src/scalping/grid-universe.ts";
import { validateLadderEvidence } from "../src/scalping/ladder-validation.ts";
import type { LadderOptions } from "../src/scalping/ladder-grid.ts";
import type { Candle } from "../src/market-data/types.ts";

const HOME = process.env.NEURATRADE_HOME ?? `${process.env.HOME}/.neuratrade`;

function arg(name: string, fallback: string): string {
  const hit = process.argv.find((a) => a.startsWith(`--${name}=`));
  return hit?.split("=")[1] ?? fallback;
}
const flag = (name: string): boolean => process.argv.includes(`--${name}`);

const watchlistPath = arg(
  "watchlist",
  "grid-whitelist-ladder-portfolio-v1.json",
);
const timeframe = arg("timeframe", "15m");
const trainBars = Number(arg("train-bars", "3600"));
const testBars = Number(arg("test-bars", "1200"));
const minCandles = Number(arg("min-candles", "10000"));
const feePct = Number(arg("fee", "0.02"));
const slippageBps = Number(arg("slippage-bps", "2"));
const stopRatio = Number(arg("stop-ratio", "1.5"));
const maxHoldBars = Number(arg("max-hold-bars", "48"));
const minFillFrequencyPct = Number(arg("min-fill-frequency-pct", "5"));
const prune = flag("prune");

interface WhitelistRow {
  symbol: string;
  exchange: string;
  gridParams?: {
    gridStepPct: number;
    gridMaxGrids: number;
    gridPauseAfterLossBars: number;
    rungs?: number;
    targetRatio?: number;
    chopGateAdx?: number;
  };
}

const path = watchlistPath.startsWith("/")
  ? watchlistPath
  : `${HOME}/data/${watchlistPath}`;
const rows: WhitelistRow[] = await Bun.file(path).json();
if (!Array.isArray(rows) || rows.length === 0) {
  console.error(`gatewatch: empty or malformed whitelist at ${path}`);
  process.exit(2);
}

const db = new Database(`${HOME}/data/neuratrade.db`, { readonly: true });
db.exec("PRAGMA busy_timeout = 30000;");

/**
 * Candles live in the DB as the 5m mainnet cache (the same source the
 * research scan resamples from). Load 5m rows and resample to the scan
 * timeframe so gate verdicts match the funnel's data path exactly.
 */
function loadCandles(symbolWire: string, targetMinutes: number): Candle[] {
  const canonical = symbolWire.replace(/\/USDT.*/, "/USDT");
  // Wire form, canonical pair, and perp suffix — always three bound params,
  // so the statement text is fully static.
  const variants = [symbolWire, canonical, `${canonical}:USDT`];
  // 5m base bars needed to cover minCandles at the target timeframe.
  const baseLimit = minCandles * Math.max(1, Math.round(targetMinutes / 5));
  const rowsDb = db
    .query(
      `SELECT c.open_price AS open, c.high_price AS high, c.low_price AS low,
              c.close_price AS close, c.volume, c.timestamp
       FROM ohlcv_data c
       JOIN trading_pairs tp ON tp.id = c.trading_pair_id
       WHERE tp.symbol IN (?,?,?) AND c.timeframe = '5m'
       ORDER BY c.timestamp DESC LIMIT ?`,
    )
    .all(variants[0], variants[1], variants[2], baseLimit) as {
    open: number;
    high: number;
    low: number;
    close: number;
    volume: number;
    timestamp: string;
  }[];
  // Loaded DESC for the LIMIT; resampler wants chronological order.
  const chronological = rowsDb.toReversed();
  const base: Candle[] = chronological.map((r) => ({
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
  if (targetMinutes === 5) return base;
  return resampleCandles(base, targetMinutes, timeframe);
}

const survivors: WhitelistRow[] = [];
let failures = 0;

for (const row of rows) {
  const targetMinutes = timeframe === "5m" ? 5 : 15;
  const candles = loadCandles(row.symbol, targetMinutes);
  const reasons: string[] = [];

  if (candles.length < trainBars + testBars + testBars) {
    failures += 1;
    console.log(
      `✘ ${row.symbol} insufficient_candles (${candles.length} < ${trainBars + testBars * 2})`,
    );
    continue;
  }

  const params = row.gridParams;
  const step = params?.gridStepPct ?? 1;

  const fillPct = computeFillFrequencyPct(candles, step, minFillFrequencyPct);
  if (fillPct < minFillFrequencyPct) {
    reasons.push("fill_frequency_floor");
  }

  const n = candles.length;
  const minimumWindows = Math.max(
    1,
    Math.floor((n - trainBars - testBars) / testBars),
  );
  const ladder: LadderOptions = {
    rungs: params?.rungs ?? 1,
    gridStepPct: step,
    gridMaxGrids: params?.gridMaxGrids ?? 1,
    gridPauseAfterLossBars: params?.gridPauseAfterLossBars ?? 24,
    feePct,
    slippageBps,
    initialCapital: 10000,
    leverage: 1,
    trendFilterPeriod: 0,
    targetRatio: params?.targetRatio ?? 1,
    chopGateAdxThreshold: params?.chopGateAdx ?? 0,
    stopRatio,
    maxHoldBars,
    conservativeIntrabar: true,
  };

  const evidence = validateLadderEvidence(candles, {
    now: new Date(),
    timeframeMinutes: timeframe === "5m" ? 5 : 15,
    trainBars,
    testBars,
    minimumWindows,
    ladder,
  });

  if (evidence.kind !== "ok") {
    reasons.push(`evidence_invalid:${evidence.failures[0] ?? "unknown"}`);
  } else if (!passesLadderGateCriteria(evidence)) {
    reasons.push(...ladderGateCriteriaFailures(evidence));
  }

  if (reasons.length > 0) {
    failures += 1;
    console.log(`✘ ${row.symbol} ${reasons.join("|")}`);
  } else {
    survivors.push(row);
    console.log(`✔ ${row.symbol} pass`);
  }
}

console.log(
  `gatewatch: ${rows.length - failures}/${rows.length} members pass (${path})`,
);

if (failures > 0 && prune && survivors.length > 0) {
  await Bun.write(path, JSON.stringify(survivors, null, 2));
  console.log(`gatewatch: pruned ${path} to ${survivors.length} member(s)`);
} else if (failures > 0 && prune) {
  console.log(
    "gatewatch: --prune skipped, every member failed (refusing to empty the whitelist)",
  );
}

process.exit(failures > 0 ? 1 : 0);
