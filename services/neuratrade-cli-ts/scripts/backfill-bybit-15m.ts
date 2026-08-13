#!/usr/bin/env bun
/**
 * Bybit-futures 15m deep-history backfill for the real-money readiness gate.
 *
 * DATA SOURCE IS MAINNET, NOT TESTNET. The testnet kline feed (api-testnet)
 * has garbage spikes + zero-volume flatlines for most non-BTC symbols (verified
 * 2026-08-13: LINKUSDT testnet jumps to ~12795 while mainnet is ~15.5 at
 * 2025-03-25, then flatlines with 0 volume). Mainnet klines are clean and go
 * back to 2020-11-28. Backfill from api.bybit.com so candle history is
 * mainnet-fidelity and safe to backtest.
 *
 * Usage:
 *   NEURATRADE_HOME=~/.neuratrade bun run scripts/backfill-bybit-15m.ts [--months 24] [--symbols BTCUSDT,ETHUSDT]
 *
 * Pacing 250ms/request, bounded retry on 429/5xx/transport, idempotent
 * (INSERT OR IGNORE). 15m at 1000 bars/page: ~71 pages per symbol per 24m.
 */
import { Effect } from "effect";
import { Database } from "bun:sqlite";
import * as Bybit from "../src/market-data/gateways/bybit.ts";

// Mainnet is the clean history source (see head); testnet has glitches.
const BASE = "https://api.bybit.com";
const HOME = process.env.NEURATRADE_HOME ?? `${process.env.HOME}/.neuratrade`;
const MONTHS = Number(
  process.argv.find((a) => a.startsWith("--months="))?.split("=")[1] ?? 24,
);
const symbolOverride = process.argv
  .find((a) => a.startsWith("--symbols="))
  ?.split("=")[1];
const REQUEST_DELAY_MS = 250;
const PAGE = 1000;
const TIMEFRAME = "15m";

// Liquid majors: the realistic real-money cohort universe. BTC/ETH/SOL first
// so partial runs still unblock the gate's core symbols.
const MAJORS = [
  "BTCUSDT",
  "ETHUSDT",
  "SOLUSDT",
  "BNBUSDT",
  "XRPUSDT",
  "ADAUSDT",
  "DOGEUSDT",
  "LINKUSDT",
  "SUIUSDT",
  "AVAXUSDT",
  "TONUSDT",
  "NEARUSDT",
  "LTCUSDT",
  "DOTUSDT",
  "TRXUSDT",
  "ATOMUSDT",
  "ARBUSDT",
  "OPUSDT",
  "INJUSDT",
  "FILUSDT",
  "APTUSDT",
  "ETCUSDT",
  "BCHUSDT",
  "HBARUSDT",
  "WIFUSDT",
  "PEPEUSDT",
  "RENDERUSDT",
  "FETUSDT",
];

const db = new Database(`${HOME}/data/neuratrade.db`);
db.exec("PRAGMA journal_mode = WAL;");
db.exec("PRAGMA busy_timeout = 30000;");
db.exec("PRAGMA synchronous = NORMAL;");

const sleep = (ms: number) => new Promise<void>((r) => setTimeout(r, ms));

async function withRetry<A>(
  label: string,
  run: () => Promise<A>,
): Promise<A | undefined> {
  for (let attempt = 0; ; attempt++) {
    try {
      return await run();
    } catch (err) {
      const msg = err instanceof Error ? err.message : String(err);
      if (
        attempt < 4 &&
        /429|5\d\d|fetch failed|aborted|ETIMEDOUT|ECONN|rate limit/i.test(msg)
      ) {
        await sleep(1000 * 2 ** attempt + Math.random() * 500);
        continue;
      }
      console.warn(`⚠️ ${label}: ${msg.slice(0, 140)}`);
      return undefined;
    }
  }
}

const run = <A>(e: Effect.Effect<A, unknown, never>): Promise<A> =>
  Effect.runPromise(e);

function canonicalSymbol(raw: string): string {
  return raw.includes("/") ? raw : `${raw.replace(/USDT$/, "")}/USDT:USDT`;
}

function ensureExchange(): number {
  const row = db
    .query("SELECT id FROM exchanges WHERE name = ?")
    .get("bybit-futures") as { id: number } | undefined;
  if (row) return row.id;
  const info = db
    .query(
      "INSERT INTO exchanges (name, display_name, ccxt_id, status) VALUES ('bybit-futures', 'Bybit Futures', 'bybit', 'active')",
    )
    .run();
  return Number(info.lastInsertRowid);
}
const EX_ID = ensureExchange();

function pairId(symbol: string): number {
  const base = symbol.split("/")[0];
  const quote = symbol.includes("/")
    ? symbol.slice(symbol.indexOf("/") + 1, symbol.lastIndexOf(":"))
    : "USDT";
  const q = db.query(
    "SELECT id FROM trading_pairs WHERE exchange_id = ? AND symbol = ?",
  );
  const row = q.get(EX_ID, symbol) as { id: number } | undefined;
  if (row) return row.id;
  const info = db
    .query(
      "INSERT INTO trading_pairs (exchange_id, symbol, base_currency, quote_currency) VALUES (?, ?, ?, ?)",
    )
    .run(EX_ID, symbol, base, quote);
  return Number(info.lastInsertRowid);
}

const insCandle = db.prepare(
  `INSERT OR IGNORE INTO ohlcv_data (exchange_id, trading_pair_id, timeframe, open_price, high_price, low_price, close_price, volume, timestamp)
   VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
);

async function fetchCandles(symbol: string, startMs: number): Promise<number> {
  const pair = pairId(symbol);
  let startTime: Date | undefined;
  let saved = 0;
  const maxPages = Math.ceil((MONTHS * 30 * 24 * 60) / 15 / PAGE) + 10;
  for (let page = 0; page < maxPages; page++) {
    await sleep(REQUEST_DELAY_MS);
    const batch = await withRetry(`kline ${symbol} page ${page}`, () =>
      run(Bybit.fetchOHLCV(symbol, TIMEFRAME, PAGE, startTime, BASE)),
    );
    if (batch === undefined || batch.length === 0) break;
    const oldestTs = batch.at(-1)!.timestamp.getTime();
    const keep = batch.filter((c) => c.timestamp.getTime() >= startMs);
    let inserted = 0;
    db.transaction(() => {
      for (const c of keep) {
        const res = insCandle.run(
          EX_ID,
          pair,
          TIMEFRAME,
          c.open,
          c.high,
          c.low,
          c.close,
          c.volume,
          c.timestamp.toISOString(),
        );
        inserted += Number(res.changes);
      }
    })();
    saved += inserted;
    if (oldestTs < startMs) break;
    startTime = new Date(oldestTs - 1);
    if (page % 10 === 0) {
      console.log(
        `  ${symbol}: page ${page} saved ${saved} (oldest ${new Date(oldestTs).toISOString().slice(0, 10)})`,
      );
    }
  }
  return saved;
}

async function main() {
  const symbols: string[] = symbolOverride
    ? symbolOverride.split(",")
    : MAJORS;
  const now = Date.now();
  const startMs = now - MONTHS * 30 * 24 * 3600 * 1000;
  console.log(
    `backfill bybit-futures ${TIMEFRAME} for ${symbols.length} symbols, ${MONTHS} months (since ${new Date(startMs).toISOString().slice(0, 10)})`,
  );

  let total = 0;
  for (const raw of symbols) {
    const symbol = canonicalSymbol(raw);
    const n = await fetchCandles(symbol, startMs);
    total += n;
    console.log(`== ${symbol}: +${n} candles (running total ${total})`);
  }
  console.log(`done: ${total} candles`);
  db.close();
}

main().catch((e) => {
  console.error(e);
  process.exit(1);
});
