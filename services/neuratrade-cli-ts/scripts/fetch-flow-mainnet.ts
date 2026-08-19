#!/usr/bin/env bun
/**
 * One-time mainnet Bybit flow-research data fetch (Flow Ignition).
 *
 * For the top-100 flow universe (turnover/spread/age ranked, majors always
 * included): 12 months of 5m klines + 5m open-interest history + funding
 * rates, persisted to the market-data SQLite. Plain async + bun:sqlite —
 * no effect layers (one-off research tool).
 *
 * Usage:
 *   NEURATRADE_HOME=~/.neuratrade bun run scripts/fetch-flow-mainnet.ts [--months=12] [--top-n=100] [--symbols=BTCUSDT,ETHUSDT]
 * Pacing 250ms/request, bounded retry on 429/5xx/transport, resumable.
 */
import { Effect } from "effect";
import { Database } from "bun:sqlite";
import * as Bybit from "../src/market-data/gateways/bybit.ts";
import { selectFlowUniverse } from "../src/scalping/flow-universe.ts";

const MAINNET = "https://api.bybit.com";
const HOME = process.env.NEURATRADE_HOME ?? `${process.env.HOME}/.neuratrade`;
const MONTHS = Number(
  process.argv.find((a) => a.startsWith("--months="))?.split("=")[1] ?? 12,
);
const symbolOverride = process.argv
  .find((a) => a.startsWith("--symbols="))
  ?.split("=")[1];
const topN = Number(
  process.argv.find((a) => a.startsWith("--top-n="))?.split("=")[1] ?? 100,
);
const REQUEST_DELAY_MS = 250;
const PAGE = 1000;
// Two workers keep the public API paced while preventing a 100-symbol
// backfill from taking several hours. Inserts remain synchronous transactions
// on this single SQLite connection and are resumable via INSERT OR IGNORE.
const SYMBOL_CONCURRENCY = 2;
const FIVE_MINUTES_MS = 5 * 60 * 1000;

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

/** "BTC/USDT:USDT" | "BTCUSDT" -> "BTCUSDT" */
function toBitgetSymbol(s: string): string {
  const base = s.includes("/")
    ? s.slice(0, s.indexOf("/"))
    : s.replace(/USDT$/, "");
  return `${base}USDT`;
}
function canonicalSymbol(raw: string): string {
  return raw.includes("/") ? raw : `${raw.replace(/USDT$/, "")}/USDT:USDT`;
}

// ---- exchange + pair resolution -------------------------------------------
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
    ? (symbol.slice(symbol.indexOf("/") + 1, symbol.lastIndexOf(":")) ?? "USDT")
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

function hasCandleCoverage(
  symbol: string,
  startMs: number,
  endMs: number,
): boolean {
  const pair = pairId(symbol);
  const row = db
    .query(
      `SELECT COUNT(*) AS count, MIN(timestamp) AS oldest, MAX(timestamp) AS newest
       FROM ohlcv_data
       WHERE exchange_id = ? AND trading_pair_id = ? AND timeframe = '5m'`,
    )
    .get(EX_ID, pair) as {
    count: number;
    oldest: string | null;
    newest: string | null;
  };
  if (row.oldest === null || row.newest === null) return false;
  const expected = Math.floor((endMs - startMs) / FIVE_MINUTES_MS);
  return (
    row.count >= expected * 0.98 &&
    new Date(row.oldest).getTime() <= startMs + 24 * 3600 * 1000 &&
    new Date(row.newest).getTime() >= endMs - 24 * 3600 * 1000
  );
}

const insCandle = db.prepare(
  `INSERT OR IGNORE INTO ohlcv_data (exchange_id, trading_pair_id, timeframe, open_price, high_price, low_price, close_price, volume, timestamp)
   VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
);
const insOi = db.prepare(
  `INSERT OR IGNORE INTO open_interest_history (exchange, symbol, timeframe, ts, oi, oi_value) VALUES (?, ?, ?, ?, ?, ?)`,
);
const insFunding = db.prepare(
  `INSERT OR IGNORE INTO funding_rates (exchange, symbol, funding_rate, timestamp) VALUES (?, ?, ?, ?)`,
);

async function fetchCandles(symbol: string, startMs: number): Promise<number> {
  const pair = pairId(symbol);
  const bSym = toBitgetSymbol(symbol);
  let startTime: Date | undefined;
  let saved = 0;
  let fetched = 0;
  for (let page = 0; page < 600; page++) {
    await sleep(REQUEST_DELAY_MS);
    const batch = await withRetry(`kline ${bSym} page ${page}`, () =>
      run(Bybit.fetchOHLCV(symbol, "5m", PAGE, startTime, MAINNET)),
    );
    if (batch === undefined || batch.length === 0) break;
    fetched += batch.length;
    // The gateway normalizes Bybit's newest-first wire response to
    // oldest-first. The cursor must advance from batch[0], not batch.at(-1)
    // (the newest), or every page returns the same window forever.
    const oldestTs = batch[0]!.timestamp.getTime();
    const keep = batch.filter((c) => c.timestamp.getTime() >= startMs);
    let inserted = 0;
    db.transaction(() => {
      for (const c of keep) {
        const res = insCandle.run(
          EX_ID,
          pair,
          "5m",
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
    if (page === 0 || page % 20 === 0 || oldestTs < startMs) {
      console.log(
        `  klines fetched=${fetched} saved=${saved} (${new Date(oldestTs).toISOString().slice(0, 10)})`,
      );
    }
  }
  return saved;
}

async function fetchOi(
  symbol: string,
  startMs: number,
  endMs: number,
): Promise<number> {
  const bSym = toBitgetSymbol(symbol);
  const windowMs = 30 * 24 * 3600 * 1000;
  let got = 0;
  for (let wStart = startMs; wStart < endMs; wStart += windowMs) {
    await sleep(REQUEST_DELAY_MS);
    const rows = await withRetry(
      `oi ${bSym} ${new Date(wStart).toISOString().slice(0, 10)}`,
      () =>
        run(
          Bybit.fetchOpenInterest(
            symbol,
            "5m",
            MAINNET,
            wStart,
            Math.min(wStart + windowMs, endMs),
          ),
        ),
    );
    if (rows === undefined || rows.length === 0) continue;
    for (let i = 0; i < rows.length; i += 500) {
      const chunk = rows.slice(i, i + 500);
      db.transaction(() => {
        for (const r of chunk) {
          insOi.run(
            "bybit-futures",
            symbol,
            "5m",
            r.timestamp,
            r.oi,
            r.oiValue,
          );
        }
      })();
    }
    got += rows.length;
  }
  return got;
}

async function fetchFunding(symbol: string, startMs: number): Promise<number> {
  const bSym = toBitgetSymbol(symbol);
  const rows = await withRetry(`funding ${bSym}`, () =>
    run(
      Bybit.fetchFundingRates(
        symbol,
        new Date(startMs),
        new Date(),
        200,
        MAINNET,
      ),
    ),
  );
  if (rows === undefined || rows.length === 0) return 0;
  db.transaction(() => {
    for (const r of rows) {
      if (!(r.timestamp instanceof Date) || Number.isNaN(r.timestamp.getTime()))
        continue;
      insFunding.run(
        "bybit-futures",
        symbol,
        r.fundingRate,
        r.timestamp.toISOString(),
      );
    }
  })();
  return rows.length;
}

async function main() {
  db.run(`CREATE TABLE IF NOT EXISTS open_interest_history (
    exchange TEXT NOT NULL, symbol TEXT NOT NULL, timeframe TEXT NOT NULL,
    ts INTEGER NOT NULL, oi REAL NOT NULL, oi_value REAL NOT NULL,
    UNIQUE(exchange, symbol, timeframe, ts))`);

  const symbols: string[] = symbolOverride
    ? symbolOverride.split(",")
    : await (async () => {
        const [tickers, rawInstruments] = await Promise.all([
          run(Bybit.fetchTickers(MAINNET)),
          run(Bybit.fetchInstruments(MAINNET)),
        ]);
        const tickerBySymbol = new Map(
          tickers.map((ticker) => [ticker.symbol, ticker]),
        );
        const volumes = Object.fromEntries(
          tickers.map((ticker) => [ticker.symbol, ticker.turnover24h]),
        );
        const instruments = rawInstruments.map((instrument) => {
          const ticker = tickerBySymbol.get(instrument.symbol);
          return ticker === undefined
            ? instrument
            : {
                ...instrument,
                bid1Price: ticker.bid1Price,
                ask1Price: ticker.ask1Price,
              };
        });
        const ranked = selectFlowUniverse(volumes, instruments, undefined, {
          topN,
        });
        console.log(`universe: ${ranked.length} selected`);
        return ranked.map((e) => e.symbol);
      })();

  const now = Date.now();
  const startMs = now - MONTHS * 30 * 24 * 3600 * 1000;
  let totalCandles = 0;
  let totalOi = 0;
  let totalFunding = 0;

  let nextIndex = 0;
  const processSymbol = async () => {
    for (;;) {
      const index = nextIndex++;
      if (index >= symbols.length) return;
      const rawSymbol = symbols[index];
      const symbol = canonicalSymbol(rawSymbol);
      const bSym = toBitgetSymbol(rawSymbol);
      console.log(`\n== ${bSym} ==`);
      try {
        const c = hasCandleCoverage(symbol, startMs, now)
          ? 0
          : await fetchCandles(symbol, startMs);
        if (c === 0) console.log("  klines skipped: cached range complete");
        totalCandles += c;
        console.log(`  klines saved: ${c}`);
        const oi = await fetchOi(symbol, startMs, now);
        totalOi += oi;
        console.log(`  OI saved: ${oi}`);
        const f = await fetchFunding(symbol, startMs);
        totalFunding += f;
        console.log(`  funding saved: ${f}`);
      } catch (err) {
      console.warn(
        `⚠️ ${bSym} failed: ${(err instanceof Error ? err.message : String((err as { readonly reason?: unknown }).reason ?? err)).slice(0, 140)}`,
      );
      }
    }
  };

  await Promise.all(
    Array.from(
      { length: Math.min(SYMBOL_CONCURRENCY, symbols.length) },
      processSymbol,
    ),
  );

  console.log(
    `\nDONE: ${symbols.length} symbols, ${totalCandles} candles, ${totalOi} OI rows, ${totalFunding} funding rows`,
  );
  db.close();
}

await main().catch((err) => {
  console.error(
    "fetch failed:",
    err instanceof Error ? err.message : String(err),
  );
  process.exit(1);
});
