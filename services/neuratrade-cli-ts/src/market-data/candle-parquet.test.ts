import { describe, expect, it, beforeEach, afterEach } from "bun:test";
import { mkdtempSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import type { Candle } from "./types.js";
import { computePanelHash, monthKeyFor, toMicros } from "./candle-parquet.js";
import type { CandleRow } from "./candle-parquet.js";
import {
  groupRowsIntoPanels,
  listPanelMonths,
  panelPathFor,
  readCandlePanel,
  readCandleRows,
  readCandles,
  writeCandlePanels,
} from "./candle-parquet-store.js";

/** Synthetic candles: no exchange data is read, everything is in-test. */
function candleAt(ms: number, price: number, volume = 1): Candle {
  return {
    exchange: "synthetic-futures",
    symbol: "TEST/USDT:USDT",
    timeframe: "15m",
    open: price,
    high: price + 0.5,
    low: price - 0.5,
    close: price + 0.25,
    volume,
    timestamp: new Date(ms),
  };
}

/** 15m candles spanning a Jan/Feb 2026 month boundary. */
function boundaryCandles(): readonly Candle[] {
  const start = Date.parse("2026-01-31T22:00:00.000Z");
  return Array.from({ length: 12 }, (_, i) =>
    candleAt(start + i * 15 * 60_000, 100 + i),
  );
}

describe("candle parquet store", () => {
  let root: string;

  beforeEach(() => {
    root = mkdtempSync(join(tmpdir(), "neuratrade-candle-parquet-"));
  });

  afterEach(() => {
    rmSync(root, { recursive: true, force: true });
  });

  describe("panel_hash", () => {
    const row = (
      openTsMs: bigint,
      close: bigint,
      volume: bigint = 10n,
    ): CandleRow => ({
      openTsMs,
      open: close - 1n,
      high: close + 2n,
      low: close - 3n,
      close,
      volumeBaseMicros: volume,
    });

    it("is stable for identical input", () => {
      const rows = [row(1n, 5n), row(2n, 6n)];
      expect(computePanelHash(rows)).toBe(computePanelHash(rows));
      expect(computePanelHash(rows)).toMatch(/^[0-9a-f]{64}$/);
    });

    it("is stable across a fresh array with identical rows", () => {
      const a = [row(1n, 5n), row(2n, 6n)];
      const b = [row(1n, 5n), row(2n, 6n)];
      expect(computePanelHash(a)).toBe(computePanelHash(b));
    });

    it("changes when any single field changes", () => {
      const base = row(1n, 5n);
      const variants: readonly CandleRow[] = [
        { ...base, openTsMs: 2n },
        { ...base, open: base.open + 1n },
        { ...base, high: base.high + 1n },
        { ...base, low: base.low - 1n },
        { ...base, close: base.close + 1n },
        { ...base, volumeBaseMicros: base.volumeBaseMicros + 1n },
      ];
      const reference = computePanelHash([base]);
      for (const variant of variants) {
        expect(computePanelHash([variant])).not.toBe(reference);
      }
    });

    it("is stable across a re-write of identical input", async () => {
      const first = await writeCandlePanels(root, boundaryCandles());
      const second = await writeCandlePanels(root, boundaryCandles());
      expect(second.map((p) => p.panelHash)).toEqual(
        first.map((p) => p.panelHash),
      );
    });

    it("changes when row order changes", () => {
      const rows = [row(1n, 5n), row(2n, 6n)];
      expect(computePanelHash([...rows].reverse())).not.toBe(
        computePanelHash(rows),
      );
    });

    it("changes when a row is added or dropped", () => {
      const rows = [row(1n, 5n), row(2n, 6n)];
      expect(computePanelHash(rows)).not.toBe(computePanelHash([rows[0]]));
      expect(computePanelHash(rows)).not.toBe(
        computePanelHash([...rows, row(3n, 7n)]),
      );
    });
  });

  describe("month partitioning", () => {
    it("maps a timestamp to its UTC month", () => {
      expect(monthKeyFor(BigInt(Date.parse("2026-01-01T00:00:00.000Z")))).toBe(
        "2026-01",
      );
      expect(monthKeyFor(BigInt(Date.parse("2026-01-31T23:59:59.999Z")))).toBe(
        "2026-01",
      );
      expect(monthKeyFor(BigInt(Date.parse("2026-02-01T00:00:00.000Z")))).toBe(
        "2026-02",
      );
      expect(monthKeyFor(BigInt(Date.parse("2026-12-31T23:59:59.999Z")))).toBe(
        "2026-12",
      );
    });

    it("splits rows at a month boundary into one panel per month", () => {
      const start = Date.parse("2026-01-31T22:00:00.000Z");
      const rows: CandleRow[] = Array.from({ length: 12 }, (_, i) => ({
        openTsMs: BigInt(start + i * 15 * 60_000),
        open: BigInt(100 + i),
        high: BigInt(100 + i),
        low: BigInt(100 + i),
        close: BigInt(100 + i),
        volumeBaseMicros: 1n,
      }));
      const panels = groupRowsIntoPanels(
        { exchange: "e", symbol: "s", timeframe: "15m" },
        rows,
      );
      expect(panels).toHaveLength(2);
      expect(panels.map((p) => p.id.month)).toEqual(["2026-01", "2026-02"]);
      // 8 x 15m from 22:00 stays inside January; the last 4 cross into Feb.
      expect(panels[0].rows).toHaveLength(8);
      expect(panels[1].rows).toHaveLength(4);
      expect(panels[0].rows[0].openTsMs).toBe(rows[0].openTsMs);
      expect(panels[1].rows[3].openTsMs).toBe(rows[11].openTsMs);
    });

    it("writes one file per month, and the file path carries the month", async () => {
      const panels = await writeCandlePanels(root, boundaryCandles());
      expect(panels.map((p) => p.id.month)).toEqual(["2026-01", "2026-02"]);
      expect(await listPanelMonths(root, panels[0].id)).toEqual([
        "2026-01",
        "2026-02",
      ]);
      const janPath = panelPathFor(root, panels[0].id);
      expect(
        janPath.endsWith(
          "/synthetic-futures/TEST/USDT:USDT/15m/2026-01.parquet",
        ),
      ).toBe(true);
      expect(Bun.file(janPath).size).toBeGreaterThan(0);
    });

    it("rejects a mixed exchange/symbol/timeframe batch", async () => {
      const candles = boundaryCandles();
      const mixed: Candle[] = [
        candles[0],
        { ...candles[1], symbol: "OTHER/USDT:USDT" },
      ];
      await expect(writeCandlePanels(root, mixed)).rejects.toThrow(
        /must share the same exchange, symbol, and timeframe/,
      );
    });
  });

  describe("round-trip", () => {
    it("returns the identical INT64 rows that were written", async () => {
      const written = await writeCandlePanels(root, boundaryCandles());
      const expected: CandleRow[] = written.flatMap((p) =>
        p.rows.map((r) => ({ ...r })),
      );
      const readBack = await readCandleRows(root, {
        exchange: "synthetic-futures",
        symbol: "TEST/USDT:USDT",
        timeframe: "15m",
      });
      expect(readBack).toEqual(expected);
    });

    it("preserves the stored panel_hash byte-for-byte", async () => {
      const panels = await writeCandlePanels(root, boundaryCandles());
      for (const panel of panels) {
        const readBack = await readCandlePanel(root, panel.id);
        expect(readBack?.storedHash).toBe(panel.panelHash);
        expect(computePanelHash(readBack?.rows ?? [])).toBe(panel.panelHash);
      }
    });

    it("returns candles whose values match within micro tolerance", async () => {
      const candles = boundaryCandles();
      await writeCandlePanels(root, candles);
      const readBack = await readCandles(root, {
        exchange: "synthetic-futures",
        symbol: "TEST/USDT:USDT",
        timeframe: "15m",
      });
      expect(readBack).toHaveLength(candles.length);
      for (const [i, original] of candles.entries()) {
        const round = readBack[i];
        expect(round.timestamp.getTime()).toBe(original.timestamp.getTime());
        expect(round.exchange).toBe(original.exchange);
        expect(round.symbol).toBe(original.symbol);
        expect(round.timeframe).toBe(original.timeframe);
        // ponytail: float64 -> micros -> float64 is lossy; 1 micro of USDT is
        // far below any price signal the scan path uses.
        expect(Math.abs(round.open - original.open)).toBeLessThanOrEqual(1e-6);
        expect(Math.abs(round.high - original.high)).toBeLessThanOrEqual(1e-6);
        expect(Math.abs(round.low - original.low)).toBeLessThanOrEqual(1e-6);
        expect(Math.abs(round.close - original.close)).toBeLessThanOrEqual(
          1e-6,
        );
        expect(Math.abs(round.volume - original.volume)).toBeLessThanOrEqual(
          1e-6,
        );
      }
    });

    it("round-trips micro values that float64 cannot represent exactly", async () => {
      // 0.1 * 1e6 = 100000.00000000001 in float64; the micros round-trip must
      // still land on exactly 100000n.
      const candles: Candle[] = [
        {
          exchange: "synthetic-futures",
          symbol: "TEST/USDT:USDT",
          timeframe: "15m",
          open: 0.1,
          high: 0.2,
          low: 0.25,
          close: 0.3,
          volume: 0.125,
          timestamp: new Date("2026-03-01T00:00:00.000Z"),
        },
      ];
      await writeCandlePanels(root, candles);
      const rows = await readCandleRows(root, {
        exchange: "synthetic-futures",
        symbol: "TEST/USDT:USDT",
        timeframe: "15m",
      });
      expect(rows).toEqual([
        {
          openTsMs: BigInt(Date.parse("2026-03-01T00:00:00.000Z")),
          open: 100_000n,
          high: 200_000n,
          low: 250_000n,
          close: 300_000n,
          volumeBaseMicros: 125_000n,
        },
      ]);
      expect(rows[0].openTsMs).toBeTypeOf("bigint");
    });
  });

  describe("range query", () => {
    it("honors an inclusive lower bound", async () => {
      await writeCandlePanels(root, boundaryCandles());
      const rows = await readCandleRows(root, {
        exchange: "synthetic-futures",
        symbol: "TEST/USDT:USDT",
        timeframe: "15m",
        fromMs: BigInt(Date.parse("2026-01-31T23:30:00.000Z")),
      });
      expect(rows.length).toBeGreaterThan(0);
      expect(
        rows.every(
          (r) => r.openTsMs >= BigInt(Date.parse("2026-01-31T23:30:00.000Z")),
        ),
      ).toBe(true);
      // The boundary candle itself is included, not skipped.
      expect(rows[0].openTsMs).toBe(
        BigInt(Date.parse("2026-01-31T23:30:00.000Z")),
      );
    });

    it("honors an exclusive upper bound", async () => {
      await writeCandlePanels(root, boundaryCandles());
      const rows = await readCandleRows(root, {
        exchange: "synthetic-futures",
        symbol: "TEST/USDT:USDT",
        timeframe: "15m",
        toMs: BigInt(Date.parse("2026-01-31T23:30:00.000Z")),
      });
      expect(
        rows.every(
          (r) => r.openTsMs < BigInt(Date.parse("2026-01-31T23:30:00.000Z")),
        ),
      ).toBe(true);
      expect(rows[rows.length - 1].openTsMs).toBe(
        BigInt(Date.parse("2026-01-31T23:15:00.000Z")),
      );
    });

    it("honors both bounds together", async () => {
      await writeCandlePanels(root, boundaryCandles());
      const from = BigInt(Date.parse("2026-01-31T23:00:00.000Z"));
      const to = BigInt(Date.parse("2026-02-01T00:30:00.000Z"));
      const rows = await readCandleRows(root, {
        exchange: "synthetic-futures",
        symbol: "TEST/USDT:USDT",
        timeframe: "15m",
        fromMs: from,
        toMs: to,
      });
      expect(rows.every((r) => r.openTsMs >= from && r.openTsMs < to)).toBe(
        true,
      );
      expect(rows[0].openTsMs).toBe(from);
      expect(rows[rows.length - 1].openTsMs).toBe(
        BigInt(Date.parse("2026-02-01T00:15:00.000Z")),
      );
    });

    it("returns ascending open time across a month boundary", async () => {
      await writeCandlePanels(root, boundaryCandles());
      const rows = await readCandleRows(root, {
        exchange: "synthetic-futures",
        symbol: "TEST/USDT:USDT",
        timeframe: "15m",
      });
      expect(rows).toHaveLength(12);
      for (let i = 1; i < rows.length; i++) {
        expect(rows[i].openTsMs > rows[i - 1].openTsMs).toBe(true);
      }
    });

    it("returns nothing for a range outside every panel", async () => {
      await writeCandlePanels(root, boundaryCandles());
      const rows = await readCandleRows(root, {
        exchange: "synthetic-futures",
        symbol: "TEST/USDT:USDT",
        timeframe: "15m",
        fromMs: BigInt(Date.parse("2025-01-01T00:00:00.000Z")),
        toMs: BigInt(Date.parse("2025-02-01T00:00:00.000Z")),
      });
      expect(rows).toEqual([]);
    });

    it("returns the newest N rows, oldest -> newest, like repo.getCandles", async () => {
      await writeCandlePanels(root, boundaryCandles());
      const all = await readCandleRows(root, {
        exchange: "synthetic-futures",
        symbol: "TEST/USDT:USDT",
        timeframe: "15m",
      });
      expect(all).toHaveLength(12);
      const newestThree = await readCandleRows(root, {
        exchange: "synthetic-futures",
        symbol: "TEST/USDT:USDT",
        timeframe: "15m",
        limit: 3,
      });
      // The oldest three and the newest three differ, so this cannot pass by
      // accident: taking the first N instead of the last N fails here.
      expect(newestThree).toHaveLength(3);
      expect(newestThree).toEqual(all.slice(-3));
      expect(newestThree.map((r) => r.openTsMs)).not.toEqual(
        all.slice(0, 3).map((r) => r.openTsMs),
      );
    });

    it("returns nothing for an unknown dataset", async () => {
      const rows = await readCandleRows(root, {
        exchange: "synthetic-futures",
        symbol: "MISSING/USDT:USDT",
        timeframe: "15m",
      });
      expect(rows).toEqual([]);
    });
  });

  describe("toMicros", () => {
    it("scales to micro-USDT", () => {
      expect(toMicros(1)).toBe(1_000_000n);
      expect(toMicros(0.5)).toBe(500_000n);
      expect(toMicros(42_000.125)).toBe(42_000_125_000n);
    });

    it("rejects non-finite input", () => {
      expect(() => toMicros(Number.NaN)).toThrow(/non-finite/);
      expect(() => toMicros(Number.POSITIVE_INFINITY)).toThrow(/non-finite/);
    });
  });
});
