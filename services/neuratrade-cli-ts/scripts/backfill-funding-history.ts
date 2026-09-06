#!/usr/bin/env bun
/**
 * Backfill Bybit funding-rate history into funding_rates (public API, no auth).
 *
 * Bybit v5 /v5/market/funding/history: paginated, newest-first, 200 rows/page,
 * 50 pages max per call window. We page backwards from now until START_MS or
 * exhaustion. Rows are INSERT OR IGNORE-deduped on (symbol, timestamp).
 *
 * Usage:
 *   NEURATRADE_HOME=~/.neuratrade bun scripts/backfill-funding-history.ts \
 *     [--symbols BTCUSDT,ETHUSDT] [--top 40] [--months 12]
 */
import { Database } from "bun:sqlite";

const HOME = process.env.NEURATRADE_HOME ?? `${process.env.HOME}/.neuratrade`;

function arg(name: string, fallback: string): string {
  const hit = process.argv.find((a) => a.startsWith(`--${name}=`));
  return hit?.split("=")[1] ?? fallback;
}

const MONTHS = Number(arg("months", "12"));
const TOP = Number(arg("top", "40"));
const symbolOverride = arg("symbols", "");
const BASE = "https://api.bybit.com";
const PAGE = 200;
const MAX_PAGES_PER_SYMBOL = 60; // 60*200 = 12000 rows ≈ 400 days

const db = new Database(`${HOME}/data/neuratrade.db`);
db.exec("PRAGMA busy_timeout = 30000;");
db.exec(
  `CREATE UNIQUE INDEX IF NOT EXISTS idx_funding_rates_unique
   ON funding_rates(exchange, symbol, timestamp)`,
);

const startMs = Date.now() - MONTHS * 30 * 24 * 3600_000;

async function jsonGet(path: string): Promise<{
  retCode: number;
  retMsg?: string;
  result: { list: readonly Record<string, string>[] };
}> {
  for (let attempt = 0; attempt < 6; attempt++) {
    try {
      const res = await fetch(`${BASE}${path}`, {
        headers: { Accept: "application/json" },
        signal: AbortSignal.timeout(20_000),
      });
      if (res.status === 429) {
        const ra = Number(res.headers.get("retry-after") ?? "2");
        await Bun.sleep(Math.max(1, ra) * 1000);
        continue;
      }
      if (!res.ok) throw new Error(`HTTP ${res.status}`);
      return (await res.json()) as Awaited<ReturnType<typeof jsonGet>>;
    } catch (err) {
      if (attempt === 5) throw err;
      await Bun.sleep(1000 * 2 ** attempt);
    }
  }
  throw new Error(`unreachable: ${path}`);
}

interface SymbolRow {
  symbol: string;
  count: number;
}

let targets: string[];
if (symbolOverride) {
  targets = symbolOverride
    .split(",")
    .map((s) => s.trim())
    .filter(Boolean);
} else {
  const rows = db
    .query(
      `SELECT tp.symbol AS symbol, COUNT(*) AS count
       FROM ohlcv_data c JOIN trading_pairs tp ON tp.id = c.trading_pair_id
       WHERE c.timeframe = '5m'
       GROUP BY tp.symbol ORDER BY count DESC LIMIT ?`,
    )
    .all(TOP) as SymbolRow[];
  // DB rows look like "BTC/USDT:USDT"; Bybit wants "BTCUSDT".
  targets = rows.map((r) =>
    r.symbol.replace(/\/USDT.*$/, "USDT").toUpperCase(),
  );
}

console.log(
  `backfill-funding: ${targets.length} symbols, target span ${MONTHS} months (from ${new Date(startMs).toISOString()})`,
);

const ins = db.query(
  `INSERT OR IGNORE INTO funding_rates (exchange, symbol, funding_rate, timestamp)
   VALUES ('bybit-futures', ?, ?, ?)`,
);

let totalRows = 0;
for (const raw of targets) {
  const wire = raw.toUpperCase().includes("/")
    ? raw.toUpperCase()
    : `${raw.toUpperCase()}`;
  let oldest = Date.now();
  let pages = 0;
  let rowsThisSymbol = 0;
  while (oldest > startMs && pages < MAX_PAGES_PER_SYMBOL) {
    const endParam = `&endTime=${oldest}`;
    let list: readonly Record<string, string>[];
    try {
      const resp = await jsonGet(
        `/v5/market/funding/history?category=linear&symbol=${wire}&limit=${PAGE}${endParam}`,
      );
      if (resp.retCode !== 0) {
        console.warn(`  ${wire}: API ${resp.retCode} ${resp.retMsg ?? ""}`);
        break;
      }
      list = resp.result.list;
    } catch (err) {
      console.warn(`  ${wire}: fetch failed (${String(err).slice(0, 80)})`);
      break;
    }
    if (list.length === 0) break;
    let inserted = 0;
    db.transaction(() => {
      for (const r of list) {
        // Bybit v5 names the field fundingRateTimestamp.
        const tsMs = Number(r.fundingRateTimestamp ?? r.fundingTime);
        const rate = Number(r.fundingRate);
        if (!Number.isFinite(tsMs) || !Number.isFinite(rate)) continue;
        ins.run(wire, r.fundingRate, new Date(tsMs).toISOString());
        inserted += 1;
      }
    })();
    rowsThisSymbol += inserted;
    oldest = Math.min(
      ...list.map((r) => Number(r.fundingRateTimestamp ?? r.fundingTime)),
    );
    if (inserted === 0) break;
    pages += 1;
    if (list.length < PAGE) break;
    await Bun.sleep(150);
  }
  totalRows += rowsThisSymbol;
  console.log(`  ${wire}: +${rowsThisSymbol} rows (${pages} pages)`);
}

console.log(
  `backfill-funding: DONE, inserted/attempted ~${totalRows} rows total`,
);
