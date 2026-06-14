/**
 * Market data repository — SQLite-backed persistence for exchanges, trading
 * pairs, and OHLCV candles.
 *
 * All monetary values are stored as strings to avoid floating-point money bugs.
 */
import { Context, Effect, Layer } from "effect";
import { SqliteClient, SqliteError } from "./sqlite.ts";

// ---------------------------------------------------------------------------
// Types
// ---------------------------------------------------------------------------

export interface Candle {
  readonly exchangeId: number;
  readonly pairId: number;
  readonly timeframe: string;
  readonly timestamp: Date;
  readonly open: string;
  readonly high: string;
  readonly low: string;
  readonly close: string;
  readonly volume: string;
}

export interface CandleRange {
  readonly earliest: Date | null;
  readonly latest: Date | null;
  readonly count: number;
}

export interface CoverageGap {
  readonly from: Date;
  readonly to: Date;
}

// ---------------------------------------------------------------------------
// Service interface
// ---------------------------------------------------------------------------

export interface MarketRepositoryImpl {
  readonly ensureExchange: (name: string) => Effect.Effect<number, SqliteError>;
  readonly ensureTradingPair: (
    symbol: string,
    exchangeId: number,
  ) => Effect.Effect<number, SqliteError>;
  readonly getCandleRange: (args: {
    readonly exchangeId: number;
    readonly pairId: number;
    readonly timeframe: string;
    readonly start: Date;
    readonly end: Date;
  }) => Effect.Effect<CandleRange, SqliteError>;
  readonly findCoverageGaps: (args: {
    readonly exchangeId: number;
    readonly pairId: number;
    readonly timeframe: string;
    readonly start: Date;
    readonly end: Date;
    readonly intervalMs: number;
  }) => Effect.Effect<ReadonlyArray<CoverageGap>, SqliteError>;
  readonly getCandles: (args: {
    readonly exchangeId: number;
    readonly pairId: number;
    readonly timeframe: string;
    readonly start: Date;
    readonly end: Date;
  }) => Effect.Effect<ReadonlyArray<Candle>, SqliteError>;
  readonly insertCandles: (
    candles: ReadonlyArray<Candle>,
  ) => Effect.Effect<number, SqliteError>;
  readonly listKnownSymbols: () => Effect.Effect<
    ReadonlyArray<string>,
    SqliteError
  >;
}

export class MarketRepository extends Context.Tag("MarketRepository")<
  MarketRepository,
  MarketRepositoryImpl
>() {}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

function normalizeSymbol(symbol: string): string {
  const trimmed = symbol.trim().toUpperCase();
  if (trimmed.includes("/")) return trimmed;
  const quoteAssets = [
    "USDT",
    "USDC",
    "BTC",
    "ETH",
    "BNB",
    "FDUSD",
    "TUSD",
    "BUSD",
    "PAX",
    "TRY",
    "EUR",
    "GBP",
    "JPY",
    "AUD",
  ];
  for (const quote of quoteAssets) {
    if (trimmed.endsWith(quote) && trimmed.length > quote.length) {
      return `${trimmed.slice(0, trimmed.length - quote.length)}/${quote}`;
    }
  }
  return trimmed;
}

function parseTimestamp(value: unknown): Date {
  if (value instanceof Date) return value;
  const str = String(value);
  const parsed = new Date(str);
  if (Number.isNaN(parsed.getTime())) {
    return new Date(0);
  }
  return parsed;
}

// ---------------------------------------------------------------------------
// Implementation
// ---------------------------------------------------------------------------

export const MarketRepositoryLive: Layer.Layer<
  MarketRepository,
  never,
  SqliteClient
> = Layer.effect(
  MarketRepository,
  Effect.gen(function* () {
    const db = yield* SqliteClient;

    const ensureExchange = (name: string): Effect.Effect<number, SqliteError> =>
      Effect.gen(function* () {
        const normalized = name.trim().toLowerCase();
        const existing = yield* db.queryOne<{ id: number }>(
          "SELECT id FROM exchanges WHERE name = ?",
          [normalized],
        );
        if (existing) return existing.id;
        yield* db.execute(
          `INSERT OR IGNORE INTO exchanges(
            name, display_name, ccxt_id, api_url, status,
            has_spot, has_futures, is_active, priority
          ) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
          [
            normalized,
            normalized,
            normalized,
            `https://${normalized}.com`,
            "active",
            1,
            0,
            1,
            0,
          ],
        );
        const inserted = yield* db.queryOne<{ id: number }>(
          "SELECT id FROM exchanges WHERE name = ?",
          [normalized],
        );
        if (!inserted) {
          return yield* Effect.fail(
            new SqliteError({
              message: `Could not resolve exchange id for ${normalized}`,
              cause: null,
            }),
          );
        }
        return inserted.id;
      });

    const ensureTradingPair = (
      symbol: string,
      exchangeId: number,
    ): Effect.Effect<number, SqliteError> =>
      Effect.gen(function* () {
        const normalized = normalizeSymbol(symbol);
        const [base, quote] = normalized.split("/");
        const baseCurrency = base ?? normalized;
        const quoteCurrency = quote ?? "USDT";
        yield* db.execute(
          `INSERT OR IGNORE INTO trading_pairs(
            exchange_id, symbol, base_currency, quote_currency
          ) VALUES (?, ?, ?, ?)`,
          [exchangeId, normalized, baseCurrency, quoteCurrency],
        );
        const row = yield* db.queryOne<{ id: number }>(
          "SELECT id FROM trading_pairs WHERE exchange_id = ? AND symbol = ?",
          [exchangeId, normalized],
        );
        if (!row) {
          return yield* Effect.fail(
            new SqliteError({
              message: `Could not resolve trading pair id for ${normalized}`,
              cause: null,
            }),
          );
        }
        return row.id;
      });

    const getCandleRange = (args: {
      readonly exchangeId: number;
      readonly pairId: number;
      readonly timeframe: string;
      readonly start: Date;
      readonly end: Date;
    }): Effect.Effect<CandleRange, SqliteError> =>
      Effect.gen(function* () {
        const row = yield* db.queryOne<{
          earliest: string | null;
          latest: string | null;
          count: number;
        }>(
          `SELECT MIN(timestamp) AS earliest, MAX(timestamp) AS latest, COUNT(*) AS count
           FROM ohlcv_data
           WHERE exchange_id = ? AND trading_pair_id = ? AND timeframe = ?
             AND timestamp >= ? AND timestamp <= ?`,
          [
            args.exchangeId,
            args.pairId,
            args.timeframe,
            args.start.toISOString(),
            args.end.toISOString(),
          ],
        );
        return {
          earliest: row?.earliest ? parseTimestamp(row.earliest) : null,
          latest: row?.latest ? parseTimestamp(row.latest) : null,
          count: row ? Number(row.count) : 0,
        };
      });

    const findCoverageGaps = (args: {
      readonly exchangeId: number;
      readonly pairId: number;
      readonly timeframe: string;
      readonly start: Date;
      readonly end: Date;
      readonly intervalMs: number;
    }): Effect.Effect<ReadonlyArray<CoverageGap>, SqliteError> =>
      Effect.gen(function* () {
        const rows = yield* db.queryAll<{ timestamp: string }>(
          `SELECT timestamp FROM ohlcv_data
           WHERE exchange_id = ? AND trading_pair_id = ? AND timeframe = ?
             AND timestamp >= ? AND timestamp <= ?
           ORDER BY timestamp ASC`,
          [
            args.exchangeId,
            args.pairId,
            args.timeframe,
            args.start.toISOString(),
            args.end.toISOString(),
          ],
        );

        const timestamps = rows.map((r) =>
          parseTimestamp(r.timestamp).getTime(),
        );
        const gaps: Array<CoverageGap> = [];
        let expected = args.start.getTime();
        const endTime = args.end.getTime();

        for (const ts of timestamps) {
          if (ts > expected) {
            gaps.push({
              from: new Date(expected),
              to: new Date(Math.min(ts, endTime)),
            });
          }
          if (ts >= expected) {
            expected = ts + args.intervalMs;
          }
          if (expected >= endTime) break;
        }

        if (expected < endTime) {
          gaps.push({ from: new Date(expected), to: new Date(endTime) });
        }

        return gaps;
      });

    const getCandles = (args: {
      readonly exchangeId: number;
      readonly pairId: number;
      readonly timeframe: string;
      readonly start: Date;
      readonly end: Date;
    }): Effect.Effect<ReadonlyArray<Candle>, SqliteError> =>
      Effect.gen(function* () {
        const rows = yield* db.queryAll<{
          timestamp: string;
          open_price: string;
          high_price: string;
          low_price: string;
          close_price: string;
          volume: string;
        }>(
          `SELECT timestamp, open_price, high_price, low_price, close_price, volume
           FROM ohlcv_data
           WHERE exchange_id = ? AND trading_pair_id = ? AND timeframe = ?
             AND timestamp >= ? AND timestamp <= ?
           ORDER BY timestamp ASC`,
          [
            args.exchangeId,
            args.pairId,
            args.timeframe,
            args.start.toISOString(),
            args.end.toISOString(),
          ],
        );
        return rows.map((r) => ({
          exchangeId: args.exchangeId,
          pairId: args.pairId,
          timeframe: args.timeframe,
          timestamp: parseTimestamp(r.timestamp),
          open: String(r.open_price),
          high: String(r.high_price),
          low: String(r.low_price),
          close: String(r.close_price),
          volume: String(r.volume),
        }));
      });

    const insertCandles = (
      candles: ReadonlyArray<Candle>,
    ): Effect.Effect<number, SqliteError> =>
      Effect.gen(function* () {
        if (candles.length === 0) return 0;

        const BATCH = 500;
        let inserted = 0;

        for (let offset = 0; offset < candles.length; offset += BATCH) {
          const batch = candles.slice(offset, offset + BATCH);
          const placeholders = batch
            .map(() => "(?, ?, ?, ?, ?, ?, ?, ?, ?)")
            .join(",");
          const params = batch.flatMap((c) => [
            c.exchangeId,
            c.pairId,
            c.timeframe,
            c.open,
            c.high,
            c.low,
            c.close,
            c.volume,
            c.timestamp.toISOString(),
          ]);

          const result = yield* db.execute(
            `INSERT OR IGNORE INTO ohlcv_data(
              exchange_id, trading_pair_id, timeframe,
              open_price, high_price, low_price, close_price, volume, timestamp
            ) VALUES ${placeholders}`,
            params,
          );
          inserted += result.changes;
        }

        return inserted;
      });

    const listKnownSymbols = (): Effect.Effect<
      ReadonlyArray<string>,
      SqliteError
    > =>
      Effect.gen(function* () {
        const rows = yield* db.queryAll<{ symbol: string }>(
          "SELECT DISTINCT symbol FROM trading_pairs ORDER BY symbol",
        );
        return rows.map((r) => r.symbol);
      });

    return {
      ensureExchange,
      ensureTradingPair,
      getCandleRange,
      findCoverageGaps,
      getCandles,
      insertCandles,
      listKnownSymbols,
    };
  }),
);
