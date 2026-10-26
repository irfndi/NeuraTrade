import { createHash } from "node:crypto";

/**
 * One candle row in the Parquet candle store, in fixed-point micro-USDT.
 *
 * The `Candle` type carries `open/high/low/close/volume` as float64, so a
 * candle is lossy for fixed-point micros: `toMicros(price) * 1e-6` cannot
 * round-trip every float. The row type is therefore the unit of truth for the
 * store, and the row-level round-trip is what the tests assert. `toCandles`
 * documents its own approximation.
 */
export interface CandleRow {
  /** Open time of the candle, in epoch milliseconds (UTC). */
  readonly openTsMs: bigint;
  /** Open price in micro-USDT (1e-6 USDT). */
  readonly open: bigint;
  /** High price in micro-USDT. */
  readonly high: bigint;
  /** Low price in micro-USDT. */
  readonly low: bigint;
  /** Close price in micro-USDT. */
  readonly close: bigint;
  /** Base-asset volume in micro-units (1e-6 of one base unit). */
  readonly volumeBaseMicros: bigint;
}

/** Parquet columns for one month panel, per the storage-cutover spec. */
export const CANDLE_PANEL_COLUMNS = [
  "open_ts_ms",
  "open",
  "high",
  "low",
  "close",
  "volume_base_micros",
  "panel_hash",
] as const;

/** Which panel a row range belongs to, identified by its UTC month. */
export interface PanelId {
  readonly exchange: string;
  readonly symbol: string;
  readonly timeframe: string;
  /** UTC month, `YYYY-MM`. */
  readonly month: string;
}

/** A month panel: its identity plus the rows it holds, in open-time order. */
export interface CandlePanel {
  readonly id: PanelId;
  readonly rows: readonly CandleRow[];
  /** SHA-256 over the ordered rows; see {@link computePanelHash}. */
  readonly panelHash: string;
}

/** Inclusive half-open bounds for a range query, in epoch milliseconds. */
export interface CandleRangeQuery {
  readonly exchange: string;
  readonly symbol: string;
  readonly timeframe: string;
  /** Inclusive lower bound on `open_ts_ms`. */
  readonly fromMs?: bigint;
  /** Exclusive upper bound on `open_ts_ms`. */
  readonly toMs?: bigint;
  /**
   * Keep the newest N rows in the range, matching `repo.getCandles`'s
   * `ORDER BY timestamp DESC LIMIT ?` then reversed — i.e. the N most recent
   * candles, returned oldest → newest. Unbounded when absent.
   */
  readonly limit?: number;
}

/** UTC month key (`YYYY-MM`) for an epoch-milliseconds timestamp. */
export function monthKeyFor(openTsMs: bigint): string {
  return new Date(Number(openTsMs)).toISOString().slice(0, 7);
}

/**
 * SHA-256 over the ordered `(open_ts_ms, o, h, l, c, v)` rows of a panel.
 *
 * The hash is order-sensitive and field-sensitive: any change to any field of
 * any row, or to the row order, changes the digest. It is the agreement gate
 * between writers — two writers that computed the same hash over the same
 * ordered rows agree on the panel byte-for-byte in meaning.
 */
export function computePanelHash(rows: readonly CandleRow[]): string {
  const digest = createHash("sha256");
  for (const row of rows) {
    digest.update(
      `${row.openTsMs}|${row.open}|${row.high}|${row.low}|${row.close}|${row.volumeBaseMicros}\n`,
      "utf8",
    );
  }
  return digest.digest("hex");
}

/** Convert a float64 USDT price to micro-USDT, rounding half away from zero. */
export function toMicros(value: number): bigint {
  if (!Number.isFinite(value)) {
    throw new RangeError(`non-finite price: ${value}`);
  }
  const scaled = value * 1_000_000;
  // Round half away from zero so a value that lands exactly on a micro
  // boundary does not silently truncate toward zero.
  return BigInt(Math.sign(scaled) * Math.round(Math.abs(scaled)));
}

/** Convert micro-USDT back to a float64 USDT price (approximate). */
export function fromMicros(micros: bigint): number {
  return Number(micros) / 1_000_000;
}
