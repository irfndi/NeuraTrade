#!/usr/bin/env bun
/**
 * Incremental top-up for the 5m mainnet candle cache (Bybit linear).
 *
 * The readiness funnel's db-mainnet source reads this cache and NEVER
 * refreshes it; `fetch-flow-mainnet.ts` is a one-off full backfill (re-pages
 * a year per symbol). Without a refresher the cache goes stale and every
 * gate-scored scan starts failing the 48h freshness check silently.
 *
 * Paging direction: the Bybit gateway treats its cursor as BACKWARD
 * (`end=` — 1000 bars ending at cursor). So each symbol pages BACKWARD
 * from now down to its newest cached bar, INSERT OR IGNORE. ~1 page per
 * symbol per 2h gap at 1000 bars/page — a full run is seconds, not hours.
 * (Fixed 2026-09-04: the old loop passed newest-cached as a forward
 * cursor, re-fetched cached bars forever, and saved ~11 boundary
 * bars/symbol — cache froze. Never pass newest as a forward cursor here.)
 *
 * Usage:
 *   NEURATRADE_HOME=~/.neuratrade bun run scripts/refresh-flow-mainnet.ts [--timeframe=15m]
 * Schedule: 5m every 2h at :40 — cron "40 0-20/2"; 15m four times daily —
 * cron "25 1,7,13,19 ... --timeframe=15m". Never auto-restart on overlap.
 */
import { Database } from "bun:sqlite";
import * as Bybit from "../src/market-data/gateways/bybit.ts";
import { Effect } from "effect";

const MAINNET = "https://api.bybit.com";
const HOME = process.env.NEURATRADE_HOME ?? `${process.env.HOME}/.neuratrade`;
const REQUEST_DELAY_MS = 250;
const PAGE = 1000;
const MAX_PAGES_PER_SYMBOL = 12; // 12 pages ≈ 41d of 5m / 125d of 15m — plenty for either cadence
const TF_ARG = process.argv
  .find((a) => a.startsWith("--timeframe="))
  ?.split("=")[1];
if (TF_ARG !== undefined && TF_ARG !== "5m" && TF_ARG !== "15m") {
  console.error("--timeframe must be 5m or 15m");
  process.exit(2);
}
const TF = TF_ARG ?? "5m";
const TF_MS = TF === "15m" ? 900_000 : 300_000;

const db = new Database(`${HOME}/data/neuratrade.db`);
db.exec("PRAGMA journal_mode = WAL;");
db.exec("PRAGMA busy_timeout = 30000;");

const sleep = (ms: number) => new Promise<void>((r) => setTimeout(r, ms));

async function withRetry<A>(
  label: string,
  run: () => Promise<A>,
): Promise<A | undefined> {
  for (let attempt = 0; ; attempt++) {
    try {
      return await run();
    } catch (err) {
      const msg =
        err instanceof Error
          ? err.message
          : String((err as { readonly reason?: unknown }).reason ?? err);
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

const exRow = db
  .query("SELECT id FROM exchanges WHERE name = 'bybit-futures'")
  .get() as { id: number } | undefined;
if (!exRow) {
  console.error(
    "refresh: no bybit-futures exchange row — run the backfill first",
  );
  process.exit(2);
}
const EX_ID = exRow.id;

const symbols = db
  .query(
    `SELECT tp.id AS pairId, tp.symbol AS symbol,
            MAX(c.timestamp) AS newest
     FROM ohlcv_data c
     JOIN trading_pairs tp ON tp.id = c.trading_pair_id
     WHERE c.exchange_id = ? AND c.timeframe = ? GROUP BY tp.id`,
  )
  .all(EX_ID, TF) as { pairId: number; symbol: string; newest: string }[];

const insCandle = db.prepare(
  `INSERT OR IGNORE INTO ohlcv_data (exchange_id, trading_pair_id, timeframe, open_price, high_price, low_price, close_price, volume, timestamp)
   VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
);

let totalSaved = 0;
let symbolsTouched = 0;

for (const { pairId, symbol, newest } of symbols) {
  // Backward from now (gateway cursor direction); stop at the cached newest.
  const newestMs = new Date(newest).getTime();
  let cursor: Date | undefined = new Date();
  let saved = 0;
  for (let page = 0; page < MAX_PAGES_PER_SYMBOL; page++) {
    await sleep(REQUEST_DELAY_MS);
    const batch = await withRetry(`kline ${symbol} page ${page}`, () =>
      run(Bybit.fetchOHLCV(symbol, TF, PAGE, cursor, MAINNET)),
    );
    if (batch === undefined || batch.length === 0) break;
    const oldest = batch[0];
    const keep = batch.filter((c) => c.timestamp.getTime() > newestMs);
    db.transaction(() => {
      for (const c of keep) {
        const res = insCandle.run(
          EX_ID,
          pairId,
          TF,
          c.open,
          c.high,
          c.low,
          c.close,
          c.volume,
          c.timestamp.toISOString(),
        );
        saved += Number(res.changes);
      }
    })();
    if (!oldest || oldest.timestamp.getTime() <= newestMs) break;
    cursor = new Date(oldest.timestamp.getTime() - 1);
  }
  if (saved > 0) {
    totalSaved += saved;
    symbolsTouched += 1;
  }
}

console.log(
  `refresh: +${totalSaved} candles across ${symbolsTouched}/${symbols.length} symbols`,
);
