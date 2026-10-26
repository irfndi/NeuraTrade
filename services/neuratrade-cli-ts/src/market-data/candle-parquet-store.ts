import { parquetReadObjects } from "hyparquet";
import { asyncBufferFromFile } from "hyparquet/src/node.js";
import { parquetWriteBuffer } from "hyparquet-writer";
import type { Candle } from "./types.js";
import {
  CANDLE_PANEL_COLUMNS,
  computePanelHash,
  fromMicros,
  monthKeyFor,
  toMicros,
} from "./candle-parquet.js";
import type {
  CandlePanel,
  CandleRangeQuery,
  CandleRow,
  PanelId,
} from "./candle-parquet.js";

/** Filesystem layout: `<root>/<exchange>/<symbol>/<timeframe>/<month>.parquet`. */
export function panelPathFor(root: string, panel: PanelId): string {
  return `${root}/${panel.exchange}/${panel.symbol}/${panel.timeframe}/${panel.month}.parquet`;
}

/** Directory holding the month partitions for one dataset. */
export function panelDirFor(
  root: string,
  query: Pick<CandleRangeQuery, "exchange" | "symbol" | "timeframe">,
): string {
  return `${root}/${query.exchange}/${query.symbol}/${query.timeframe}`;
}

/** Split rows into month panels, each carrying its own `panel_hash`. */
export function groupRowsIntoPanels(
  id: Pick<PanelId, "exchange" | "symbol" | "timeframe">,
  rows: readonly CandleRow[],
): readonly CandlePanel[] {
  const byMonth = new Map<string, CandleRow[]>();
  for (const row of rows) {
    const month = monthKeyFor(row.openTsMs);
    const bucket = byMonth.get(month);
    if (bucket === undefined) {
      byMonth.set(month, [row]);
    } else {
      bucket.push(row);
    }
  }
  return [...byMonth.entries()]
    .sort(([a], [b]) => (a < b ? -1 : a > b ? 1 : 0))
    .map(([month, monthRows]) => ({
      id: { ...id, month },
      rows: monthRows,
      panelHash: computePanelHash(monthRows),
    }));
}

/** Serialize one month panel to Parquet bytes in the spec's column order. */
function parquetPanelBytes(panel: CandlePanel): Uint8Array {
  // hyparquet returns a raw ArrayBuffer; wrap it so callers get a Uint8Array.
  return new Uint8Array(
    parquetWriteBuffer({
      columnData: [
        {
          name: CANDLE_PANEL_COLUMNS[0],
          type: "INT64",
          data: panel.rows.map((r) => r.openTsMs),
        },
        {
          name: CANDLE_PANEL_COLUMNS[1],
          type: "INT64",
          data: panel.rows.map((r) => r.open),
        },
        {
          name: CANDLE_PANEL_COLUMNS[2],
          type: "INT64",
          data: panel.rows.map((r) => r.high),
        },
        {
          name: CANDLE_PANEL_COLUMNS[3],
          type: "INT64",
          data: panel.rows.map((r) => r.low),
        },
        {
          name: CANDLE_PANEL_COLUMNS[4],
          type: "INT64",
          data: panel.rows.map((r) => r.close),
        },
        {
          name: CANDLE_PANEL_COLUMNS[5],
          type: "INT64",
          data: panel.rows.map((r) => r.volumeBaseMicros),
        },
        {
          name: CANDLE_PANEL_COLUMNS[6],
          type: "STRING",
          data: panel.rows.map(() => panel.panelHash),
        },
      ],
    }),
  );
}

/**
 * Write candles as a month-partitioned Parquet dataset.
 *
 * One file per `(exchange, symbol, timeframe, month)`; the file's rows are the
 * panel for that month and its `panel_hash` column is repeated on every row so
 * a single-row read can verify the panel. SQLite candle writes are untouched:
 * this is the parallel Parquet path for the candle/research role.
 */
export async function writeCandlePanels(
  root: string,
  candles: readonly Candle[],
): Promise<readonly CandlePanel[]> {
  if (candles.length === 0) return [];
  const first = candles[0];
  const mixed = candles.some(
    (c) =>
      c.exchange !== first.exchange ||
      c.symbol !== first.symbol ||
      c.timeframe !== first.timeframe,
  );
  if (mixed) {
    throw new Error(
      "writeCandlePanels: all candles must share the same exchange, symbol, and timeframe",
    );
  }

  const rows: CandleRow[] = candles.map((c) => ({
    openTsMs: BigInt(c.timestamp.getTime()),
    open: toMicros(c.open),
    high: toMicros(c.high),
    low: toMicros(c.low),
    close: toMicros(c.close),
    volumeBaseMicros: toMicros(c.volume),
  }));
  rows.sort((a, b) =>
    a.openTsMs < b.openTsMs ? -1 : a.openTsMs > b.openTsMs ? 1 : 0,
  );

  const panels = groupRowsIntoPanels(
    {
      exchange: first.exchange,
      symbol: first.symbol,
      timeframe: first.timeframe,
    },
    rows,
  );
  for (const panel of panels) {
    const path = panelPathFor(root, panel.id);
    // hyparquet's file writer opens the file directly and creates no parents;
    // Bun.write creates the directory chain and overwrites on re-write.
    await Bun.write(path, parquetPanelBytes(panel));
  }
  return panels;
}

/** A raw panel read back from disk: identity, rows, and its stored hash. */
export interface ReadCandlePanel {
  readonly id: PanelId;
  readonly rows: readonly CandleRow[];
  readonly storedHash: string;
}

/**
 * Read one month panel from disk.
 *
 * `undefined` when the file does not exist — a missing partition is not an
 * error, it is simply no data for that month.
 */
export async function readCandlePanel(
  root: string,
  id: PanelId,
): Promise<ReadCandlePanel | undefined> {
  const path = panelPathFor(root, id);
  if (!(await Bun.file(path).exists())) return undefined;
  const rows = await parquetReadObjects({
    file: await asyncBufferFromFile(path),
  });
  if (rows.length === 0) return { id, rows: [], storedHash: "" };
  return {
    id,
    rows: rows.map((r) => ({
      openTsMs: r.open_ts_ms,
      open: r.open,
      high: r.high,
      low: r.low,
      close: r.close,
      volumeBaseMicros: r.volume_base_micros,
    })),
    storedHash: rows[0].panel_hash,
  };
}

/** List the months present for one dataset, sorted ascending. */
export async function listPanelMonths(
  root: string,
  query: Pick<CandleRangeQuery, "exchange" | "symbol" | "timeframe">,
): Promise<readonly string[]> {
  const dir = panelDirFor(root, query);
  const months: string[] = [];
  // Glob.scan throws ENOENT on a missing directory; an absent dataset is a
  // valid "no data", not an error.
  let entries: string[] = [];
  try {
    const glob = new Bun.Glob("*.parquet");
    for await (const match of glob.scan({ cwd: dir, onlyFiles: true })) {
      entries.push(match);
    }
  } catch {
    return [];
  }
  for (const match of entries) {
    months.push(match.slice(0, -".parquet".length));
  }
  return months.sort((a, b) => (a < b ? -1 : a > b ? 1 : 0));
}

/** Start of the UTC month after `month` (`YYYY-MM`), as epoch milliseconds. */
function nextMonthStartMs(month: string): bigint {
  const year = Number(month.slice(0, 4));
  const monthNumber = Number(month.slice(5, 7));
  const [nextYear, nextMonth] =
    monthNumber === 12 ? [year + 1, 1] : [year, monthNumber + 1];
  return BigInt(Date.UTC(nextYear, nextMonth - 1, 1, 0, 0, 0, 0));
}

/** Start of `month` (`YYYY-MM`), as epoch milliseconds. */
function monthStartMs(month: string): bigint {
  return BigInt(
    Date.UTC(Number(month.slice(0, 4)), Number(month.slice(5, 7)) - 1, 1),
  );
}

/**
 * Read candles for `(exchange, symbol, timeframe)` within an optional
 * `[fromMs, toMs)` range, ordered ascending by open time.
 *
 * Only the month panels the range can touch are opened: a one-day range inside
 * a two-year store reads one file, not 24. Bounds are inclusive on `fromMs`
 * and exclusive on `toMs`.
 */
export async function readCandles(
  root: string,
  query: CandleRangeQuery,
): Promise<readonly Candle[]> {
  const months = await listPanelMonths(root, query);
  const wanted = months.filter((month) => {
    if (query.fromMs === undefined && query.toMs === undefined) return true;
    const start = monthStartMs(month);
    const end = nextMonthStartMs(month);
    return (
      (query.toMs === undefined || start < query.toMs) &&
      (query.fromMs === undefined || end > query.fromMs)
    );
  });

  const out: Candle[] = [];
  for (const month of wanted) {
    const panel = await readCandlePanel(root, { ...query, month });
    if (panel === undefined) continue;
    for (const row of panel.rows) {
      if (query.fromMs !== undefined && row.openTsMs < query.fromMs) continue;
      if (query.toMs !== undefined && row.openTsMs >= query.toMs) continue;
      out.push({
        exchange: query.exchange,
        symbol: query.symbol,
        timeframe: query.timeframe,
        open: fromMicros(row.open),
        high: fromMicros(row.high),
        low: fromMicros(row.low),
        close: fromMicros(row.close),
        volume: fromMicros(row.volumeBaseMicros),
        timestamp: new Date(Number(row.openTsMs)),
      });
    }
  }
  out.sort((a, b) => a.timestamp.getTime() - b.timestamp.getTime());
  return query.limit === undefined ? out : out.slice(-query.limit);
}

/**
 * Read the raw INT64 rows for a range, bypassing the float64 `Candle` view.
 *
 * This is the lossless path: the row-level round-trip test asserts against it,
 * because the float conversion in {@link readCandles} cannot be exact for
 * every price.
 */
export async function readCandleRows(
  root: string,
  query: CandleRangeQuery,
): Promise<readonly CandleRow[]> {
  const months = await listPanelMonths(root, query);
  const wanted = months.filter((month) => {
    if (query.fromMs === undefined && query.toMs === undefined) return true;
    const start = monthStartMs(month);
    const end = nextMonthStartMs(month);
    return (
      (query.toMs === undefined || start < query.toMs) &&
      (query.fromMs === undefined || end > query.fromMs)
    );
  });

  const out: CandleRow[] = [];
  for (const month of wanted) {
    const panel = await readCandlePanel(root, { ...query, month });
    if (panel === undefined) continue;
    for (const row of panel.rows) {
      if (query.fromMs !== undefined && row.openTsMs < query.fromMs) continue;
      if (query.toMs !== undefined && row.openTsMs >= query.toMs) continue;
      out.push({ ...row });
    }
  }
  out.sort((a, b) =>
    a.openTsMs < b.openTsMs ? -1 : a.openTsMs > b.openTsMs ? 1 : 0,
  );
  return query.limit === undefined ? out : out.slice(-query.limit);
}
