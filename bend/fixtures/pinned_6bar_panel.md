# Pinned 6-bar panel fixture (P4 parity probe)

Single purpose: pin the exact dataset provenance + candle bytes that the
TS-vs-Bend parity probe runs on. Production-faithful provenance, tiny body.
By design it meets NO phase minimums, so every TS eval phase must return
the degenerate `emptyResult` (score `-Infinity`, `guardsOk: false`).

## Provenance (hash inputs — `computePanelHash` in `prepare.ts`)

| field | value |
|---|---|
| exchange | `bybit-futures` |
| timeframe (source) | `5m` |
| panelTimeframe | `15m` |
| symbols | `["BTC/USDT:USDT"]` (exact wire symbol, no alias merging) |
| refLen | `6` |
| t0Ms | `1767225600000` (`2026-01-01T00:00:00.000Z`) |
| t1Ms | `1767230100000` (`2026-01-01T01:35:00.000Z`) |
| holdoutBars | `2880` (`HOLDOUT_BARS`, NOT overridden) |

Hash preimage (pipe-joined, symbols sorted):

```text
bybit-futures|5m|15m|BTC/USDT:USDT|6|1767225600000|1767230100000
```

Expected `panelHash`: `d3023228` (FNV-1a 32-bit, exact string equality).

## Candles (15m, step 900000 ms, per-candle `timeframe: "15m"`)

Per-candle timeframe label does NOT enter the hash; the hash uses the
panel-level source `5m` + `panelTimeframe 15m` above.

| i | timestamp ms | ISO | O | H | L | C | V |
|---|---|---|---|---|---|---|---|
| 0 | 1767225600000 | 2026-01-01T00:00:00.000Z | 67000 | 67100 | 66950 | 67050 | 12.5 |
| 1 | 1767226500000 | 2026-01-01T00:15:00.000Z | 67050 | 67200 | 67000 | 67150 | 13.1 |
| 2 | 1767227400000 | 2026-01-01T00:30:00.000Z | 67150 | 67250 | 67100 | 67200 | 11.8 |
| 3 | 1767228300000 | 2026-01-01T00:45:00.000Z | 67200 | 67300 | 67150 | 67250 | 14.2 |
| 4 | 1767229200000 | 2026-01-01T01:00:00.000Z | 67250 | 67350 | 67200 | 67300 | 12.9 |
| 5 | 1767230100000 | 2026-01-01T01:15:00.000Z | 67300 | 67400 | 67250 | 67350 | 13.7 |

`exchange: "bybit-futures"`, `symbol: "BTC/USDT:USDT"` on every candle.

## TS construction (no DB — `AlignedPanel` literal)

```ts
import type { Candle } from "./src/market-data/types.ts";

const T0 = 1767225600000;
const rows: Array<[number, number, number, number, number]> = [
  // [open, high, low, close, volume]
  [67000, 67100, 66950, 67050, 12.5],
  [67050, 67200, 67000, 67150, 13.1],
  [67150, 67250, 67100, 67200, 11.8],
  [67200, 67300, 67150, 67250, 14.2],
  [67250, 67350, 67200, 67300, 12.9],
  [67300, 67400, 67250, 67350, 13.7],
];
const candles: Candle[] = rows.map(([open, high, low, close, volume], i) => ({
  exchange: "bybit-futures",
  symbol: "BTC/USDT:USDT",
  timeframe: "15m",
  open, high, low, close, volume,
  timestamp: new Date(T0 + i * 900000),
}));
const panel = {
  symbols: ["BTC/USDT:USDT"],
  aligned: new Map([["BTC/USDT:USDT", candles]]),
  refLen: 6,
  loadedMs: 0,
  exchange: "bybit-futures",
  timeframe: "5m",
  panelTimeframe: "15m",
  panelHash: "d3023228",
  holdoutBars: 2880,
} as const;
```

Verify the hash independently with `computePanelHash({exchange, timeframe: "5m",
panelTimeframe: "15m", symbols, refLen: 6, t0Ms: T0, t1Ms: T0 + 5 * 900000})`
— must return `"d3023228"`.

## Why degenerate by design

- `symbols.length (1) < minSymbols (3)` → every phase returns `emptyResult`
  with `reason: "insufficient_symbols"` before touching backtest code.
- Selection path would also fail (`selectionLen = max(0, 6-2880) = 0`).
- So the probe pins provenance + plumbing with zero dependence on knob
  values, backtest nondeterminism, or budget timing (`elapsedMs` excluded
  from all comparisons).
