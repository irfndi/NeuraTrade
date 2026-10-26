#!/usr/bin/env bun
/**
 * Runnable invariant check for the Parquet candle store.
 *
 * Fails (non-zero exit) if the round-trip or the `panel_hash` invariant
 * breaks. Writes synthetic candles only — it never opens the real candle DB.
 *
 * Usage: bun run scripts/check-candle-parquet-invariants.ts
 */
import { mkdtempSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import type { Candle } from "../src/market-data/types.js";
import { computePanelHash } from "../src/market-data/candle-parquet.js";
import {
  readCandlePanel,
  readCandleRows,
  writeCandlePanels,
} from "../src/market-data/candle-parquet-store.js";

const failures: string[] = [];

function check(name: string, condition: boolean, detail = ""): void {
  if (condition) {
    console.log(`ok   ${name}`);
  } else {
    console.log(`FAIL ${name}${detail ? `: ${detail}` : ""}`);
    failures.push(name);
  }
}

/** 15m candles spanning a Jan/Feb 2026 month boundary, all synthetic. */
function boundaryCandles(): Candle[] {
  const start = Date.parse("2026-01-31T22:00:00.000Z");
  return Array.from({ length: 12 }, (_, i) => ({
    exchange: "check-futures",
    symbol: "CHECK/USDT:USDT",
    timeframe: "15m",
    open: 100 + i,
    high: 100 + i + 0.5,
    low: 100 + i - 0.5,
    close: 100 + i + 0.25,
    volume: 1 + i * 0.1,
    timestamp: new Date(start + i * 15 * 60_000),
  }));
}

const root = mkdtempSync(join(tmpdir(), "candle-parquet-check-"));
try {
  const panels = await writeCandlePanels(root, boundaryCandles());

  // 1. Month partitioning: the boundary candles split into exactly two panels.
  check(
    "month partitioning splits at the boundary",
    panels.length === 2 &&
      panels[0].id.month === "2026-01" &&
      panels[1].id.month === "2026-02",
    `got ${panels.map((p) => p.id.month).join(",")}`,
  );

  // 2. Round-trip: the INT64 rows read back equal the rows written.
  const written = panels.flatMap((p) => p.rows.map((r) => ({ ...r })));
  const readBack = await readCandleRows(root, {
    exchange: "check-futures",
    symbol: "CHECK/USDT:USDT",
    timeframe: "15m",
  });
  check(
    "round-trip returns identical INT64 rows",
    readBack.length === written.length &&
      readBack.every(
        (r, i) =>
          r.openTsMs === written[i].openTsMs &&
          r.open === written[i].open &&
          r.high === written[i].high &&
          r.low === written[i].low &&
          r.close === written[i].close &&
          r.volumeBaseMicros === written[i].volumeBaseMicros,
      ),
    `${readBack.length} rows read, ${written.length} written`,
  );

  // 3. panel_hash matches the rows it covers, and is stable across a
  //    re-write of identical input (self-consistency alone would not prove
  //    the digest is reproducible).
  check(
    "panel_hash matches the rows it covers",
    panels.every((p) => p.panelHash === computePanelHash(p.rows)),
  );
  const rewritten = await writeCandlePanels(root, boundaryCandles());
  check(
    "panel_hash is stable across a re-write of identical input",
    rewritten.length === panels.length &&
      rewritten.every((p, i) => p.panelHash === panels[i].panelHash),
    `first=${panels.map((p) => p.panelHash.slice(0, 8)).join(",")} second=${rewritten.map((p) => p.panelHash.slice(0, 8)).join(",")}`,
  );
  for (const panel of panels) {
    const stored = await readCandlePanel(root, panel.id);
    check(
      `stored panel_hash matches computed (${panel.id.month})`,
      stored?.storedHash === panel.panelHash,
      `stored=${stored?.storedHash} computed=${panel.panelHash}`,
    );
  }

  // 4. panel_hash changes when any field changes.
  const base = {
    openTsMs: 1n,
    open: 5n,
    high: 7n,
    low: 3n,
    close: 6n,
    volumeBaseMicros: 2n,
  };
  const reference = computePanelHash([base]);
  const drifted = [
    { ...base, openTsMs: 2n },
    { ...base, open: base.open + 1n },
    { ...base, high: base.high + 1n },
    { ...base, low: base.low - 1n },
    { ...base, close: base.close + 1n },
    { ...base, volumeBaseMicros: base.volumeBaseMicros + 1n },
  ];
  check(
    "panel_hash changes when any field changes",
    drifted.every((row) => computePanelHash([row]) !== reference),
  );
  check(
    "panel_hash changes when row order changes",
    computePanelHash([...drifted].reverse()) !== computePanelHash(drifted),
  );

  // 5. Range query honors its bounds.
  const from = BigInt(Date.parse("2026-01-31T23:00:00.000Z"));
  const to = BigInt(Date.parse("2026-02-01T00:30:00.000Z"));
  const ranged = await readCandleRows(root, {
    exchange: "check-futures",
    symbol: "CHECK/USDT:USDT",
    timeframe: "15m",
    fromMs: from,
    toMs: to,
  });
  check(
    "range query honors inclusive-lower/exclusive-upper bounds",
    ranged.length > 0 &&
      ranged.every((r) => r.openTsMs >= from && r.openTsMs < to) &&
      ranged[0].openTsMs === from,
  );
} finally {
  rmSync(root, { recursive: true, force: true });
}

if (failures.length > 0) {
  console.error(
    `\n${failures.length} invariant(s) broken: ${failures.join("; ")}`,
  );
  process.exit(1);
}
console.log("\nall candle-parquet invariants hold");
