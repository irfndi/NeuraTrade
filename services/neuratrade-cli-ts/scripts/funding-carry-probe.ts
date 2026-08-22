#!/usr/bin/env bun
/**
 * FUNDING-CARRY FEASIBILITY PROBE — pre-registered.
 *
 * Question: is delta-neutral funding capture (long spot / short perp, or the
 * reverse) worth building as the next strategy class, on REAL Bybit funding
 * history?
 *
 * Method (per symbol, over the backfilled window):
 *   - avg funding per 8h, annualized carry = avg * 3 * 365
 *   - positivity: fraction of intervals with funding > 0
 *   - persistence: P(rate_t same sign as rate_{t-1}) — sign autocorrelation;
 *     a carry book only works if positive-rate regimes PERSIST (you enter
 *     after observing positive funding)
 *   - conditional carry: avg funding on intervals FOLLOWING a positive
 *     interval (what you'd actually harvest by entering on signal)
 *   - net estimate: conditional carry minus round-trip cost amortized over
 *     an assumed 30d holding period (taker 0.055% x 4 legs = 0.22% entry+
 *     exit on perp; spot side ~0.1% x2; slippage ~0.1% total => ~0.5%
 *     round trip => 0.5%/30d ≈ 0.0167%/day ≈ 0.0056%/8h drag)
 *
 * FEASIBILITY BAR (decided before running):
 *   BUILD only if >= 8 of the top-20 liquid symbols show net conditional
 *   carry >= +3%/yr with sign-persistence >= 55%. Otherwise: closed.
 */
import { Database } from "bun:sqlite";

const HOME = process.env.NEURATRADE_HOME ?? `${process.env.HOME}/.neuratrade`;
const db = new Database(`${HOME}/data/neuratrade.db`, { readonly: true });
db.exec("PRAGMA busy_timeout = 30000;");

const rows = db
  .query(
    `SELECT symbol, funding_rate AS rate, timestamp FROM funding_rates
     WHERE symbol NOT LIKE '%/%'
     ORDER BY symbol, timestamp`,
  )
  .all() as { symbol: string; rate: string; timestamp: string }[];

if (rows.length < 1000) {
  console.error("carry-probe: not enough funding rows");
  process.exit(2);
}

interface PerInterval {
  rate: number; // decimal per 8h
  ts: number;
}

const bySymbol = new Map<string, PerInterval[]>();
for (const r of rows) {
  const rate = Number(r.rate);
  const ts = Date.parse(r.timestamp);
  if (!Number.isFinite(rate) || !Number.isFinite(ts)) continue;
  if (!bySymbol.has(r.symbol)) bySymbol.set(r.symbol, []);
  bySymbol.get(r.symbol)!.push({ rate, ts });
}

interface SymbolStats {
  symbol: string;
  intervals: number;
  avg8h: number;
  annualized: number;
  positiveFrac: number;
  signPersistence: number;
  conditionalCarry: number;
  netAnnualized: number;
}

const stats: SymbolStats[] = [];
for (const [symbol, intervals] of bySymbol) {
  if (intervals.length < 200) continue;
  const rates = intervals.map((i) => i.rate);
  const avg = rates.reduce((s, v) => s + v, 0) / rates.length;
  const positive = rates.filter((r) => r > 0).length / rates.length;
  let sameSign = 0;
  let signPairs = 0;
  for (let i = 1; i < rates.length; i++) {
    const a = rates[i - 1] > 0;
    const b = rates[i] > 0;
    if (rates[i - 1] === 0 || rates[i] === 0) continue;
    sameSign += a === b ? 1 : 0;
    signPairs += 1;
  }
  const persistence = signPairs > 0 ? sameSign / signPairs : 0;
  // Conditional carry: mean rate at t+1 given rate_t > 0 (you observed
  // positive funding, entered, and harvest the NEXT interval).
  let condSum = 0;
  let condN = 0;
  for (let i = 1; i < rates.length; i++) {
    if (rates[i - 1] > 0) {
      condSum += rates[i];
      condN += 1;
    }
  }
  const condCarry = condN > 0 ? condSum / condN : 0;
  // Net drag: 0.5% round trip over 30d holding => per-8h drag.
  const dragPer8h = 0.005 / 90;
  const net = condCarry - dragPer8h;
  stats.push({
    symbol,
    intervals: intervals.length,
    avg8h: avg,
    annualized: avg * 3 * 365 * 100,
    positiveFrac: positive * 100,
    signPersistence: persistence * 100,
    conditionalCarry: condCarry * 3 * 365 * 100,
    netAnnualized: net * 3 * 365 * 100,
  });
}

stats.sort((a, b) => b.netAnnualized - a.netAnnualized);

console.log(
  "symbol          | n     | ann.avg% | pos%  | persist% | cond.ann% | NET.ann%",
);
console.log("-".repeat(84));
for (const s of stats.slice(0, 20)) {
  console.log(
    `${s.symbol.padEnd(15)} | ${String(s.intervals).padStart(5)} | ${s.annualized.toFixed(1).padStart(8)} | ${s.positiveFrac.toFixed(0).padStart(4)} | ${s.signPersistence.toFixed(0).padStart(7)} | ${s.conditionalCarry.toFixed(1).padStart(9)} | ${s.netAnnualized.toFixed(1).padStart(8)}`,
  );
}

const qualifying = stats.filter(
  (s) => s.netAnnualized >= 3 && s.signPersistence >= 55,
).length;

console.log("\n=== KILL CRITERIA (pre-registered) ===");
console.log(
  `symbols with net carry >= 3%/yr AND persistence >= 55%: ${qualifying} (need >= 8 of ${stats.length})`,
);
const medNet = medianOf(stats.map((s) => s.netAnnualized));
console.log(`median net annualized across symbols: ${medNet.toFixed(2)}%`);
if (qualifying >= 8) {
  console.log(
    "\nVERDICT: SURVIVED — funding carry is feasible; proceed to design (spot+perp basis execution, rebalance cadence).",
  );
} else {
  console.log(
    "\nVERDICT: NULL — funding carry does not clear the bar net of costs on this window. Strategy class closed.",
  );
}

function medianOf(xs: number[]): number {
  const s = xs.filter(Number.isFinite).sort((a, b) => a - b);
  if (s.length === 0) return Number.NaN;
  const m = Math.floor(s.length / 2);
  return s.length % 2 ? s[m] : (s[m - 1] + s[m]) / 2;
}
