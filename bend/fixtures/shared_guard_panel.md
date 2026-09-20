# Shared-guard pinned panel fixture (P4T true parity)

Single purpose: pin the exact real-size dataset provenance + candle bytes
that the TS-vs-Bend TRUE parity probe
(`bend/fixtures/shared_guard_probe.md`) runs on. Production-faithful
provenance, deterministic arithmetic-progression body. By design it meets
EVERY phase geometry minimum, so no TS eval phase may return a geometry
`emptyResult` (`insufficient_symbols` / `insufficient_bars` /
`insufficient_windows`); any such reason is fixture drift, not a guard
verdict. Guard verdicts come from the shared GuardInput vectors in §4,
fed to TS `checkKeepGuards` AND Bend `check_keep_guards` on the same
inputs.

Read-only sources (never edited by this fixture):
`services/neuratrade-cli-ts/autoresearch/prepare.ts`
(`PHASE_GEOM`, `HOLDOUT_BARS`, `computePanelHash`, `loadAlignedPanel`,
`evaluateKnobsOnPanel`), `services/neuratrade-cli-ts/autoresearch/goals.ts`
(`KEEP_GUARDS` v2), `bend/guards.bend` (`check_keep_guards`).

## 1. Geometry minimums pinned (prepare.ts `PHASE_GEOM`)

| phase | minSymbols | minCandles | forwardBars | firstBar | minWindows |
|---|---|---|---|---|---|
| screen | 3 | 2500 | 672 | 336 | 4 |
| confirm | 3 | 6000 | 2880 | 672 | 8 |
| holdout | 3 | 6000 | 2880 (`HOLDOUT_BARS`) | 0 (n/a) | 3 |

Panel sizing rule: `nSymbols >= max(minSymbols) = 3`,
`refLen >= max(minCandles) = 6000`. This fixture uses exactly
`nSymbols = 3`, `refLen = 6500` (500 bars of headroom over the 6000
confirm/holdout floor; `minCandles` itself is a LOAD-time filter in
`loadPanelCandles`/`clipPanelToRange`, not a selection gate — see §5).

Geometry gate check (`e#valuatePanelSelection`, prepare.ts:689-702;
`holdoutBars * 0.9` slice rule, prepare.ts:808):

- `selectionLen = refLen - holdoutBars = 6500 - 2880 = 3620`.
- screen: `lastStartBar = 3620 - 672 - 1 = 2947 > firstBar 336` → OK.
- confirm: `lastStartBar = 3620 - 2880 - 1 = 739 > firstBar 672` → OK
  (67-bar margin — do NOT shrink `refLen` without recomputing).
- holdout: `6500 >= 2880 * 0.9 = 2592` per symbol → OK.

## 2. Provenance (hash inputs — `computePanelHash` in prepare.ts)

| field | value |
|---|---|
| exchange | `bybit-futures` (`DEFAULT_EXCHANGE`) |
| timeframe (source) | `5m` (`DEFAULT_TIMEFRAME`) |
| panelTimeframe | `15m` (`PANEL_TIMEFRAME`) |
| symbols | `["BTC/USDT:USDT", "ETH/USDT:USDT", "SOL/USDT:USDT"]` (exact wire symbols, sorted) |
| refLen | `6500` |
| t0Ms | `1767225600000` (`2026-01-01T00:00:00.000Z`) |
| t1Ms | `1773074700000` (`2026-03-09T16:45:00.000Z`, `T0 + 6499 * 900000`) |
| holdoutBars | `2880` (`HOLDOUT_BARS`, NOT overridden) |

Hash preimage (pipe-joined, symbols sorted):

```text
bybit-futures|5m|15m|BTC/USDT:USDT,ETH/USDT:USDT,SOL/USDT:USDT|6500|1767225600000|1773074700000
```

Expected `panelHash`: `295c6675` (FNV-1a 32-bit, exact string equality;
recompute with `computePanelHash` — any mismatch = provenance drift).

## 3. Candles (15m, step 900000 ms, `refLen` 6500 per symbol)

Per-candle timeframe label does NOT enter the hash; the hash uses the
panel-level source `5m` + `panelTimeframe 15m` above.

Deterministic generator (no randomness, no DB — closed form, exact):

```text
T(i) = 1767225600000 + i * 900000            (i = 0 .. 6499)
V(i) = 10.0 + (i mod 5) * 0.7                (10.0, 10.7, 11.4, 12.1, 12.8, …)

BTC/USDT:USDT:  base 67000.00, drift +0.05/bar, tick +2.00, halo 50.00
  O(i) = 67000 + 0.05*i        C(i) = O(i) + 2.00
  H(i) = max(O,C) + 50         L(i) = min(O,C) - 50
ETH/USDT:USDT:  base 3500.00,  drift +0.01/bar, tick +0.50, halo 5.00
  O(i) = 3500 + 0.01*i         C(i) = O(i) + 0.50
  H(i) = max(O,C) + 5          L(i) = min(O,C) - 5
SOL/USDT:USDT:  base 170.00,   drift +0.002/bar, tick +0.05, halo 0.50
  O(i) = 170 + 0.002*i         C(i) = O(i) + 0.05
  H(i) = max(O,C) + 0.50       L(i) = min(O,C) - 0.50
```

`exchange: "bybit-futures"` on every candle; per-candle
`timeframe: "15m"`, `symbol` = its wire symbol.

Anchoring rows (verify generator; full 6500 rows are formula-defined):

| sym | i | timestamp ms | O | H | L | C | V |
|---|---|---|---|---|---|---|---|
| BTC | 0 | 1767225600000 | 67000.00 | 67052.00 | 66950.00 | 67002.00 | 10.0 |
| BTC | 1 | 1767226500000 | 67000.05 | 67052.05 | 66950.05 | 67002.05 | 10.7 |
| BTC | 2 | 1767227400000 | 67000.10 | 67052.10 | 66950.10 | 67002.10 | 11.4 |
| BTC | 6499 | 1773074700000 | 67324.95 | 67376.95 | 67274.95 | 67326.95 | 12.8 |
| ETH | 0 | 1767225600000 | 3500.00 | 3505.50 | 3495.00 | 3500.50 | 10.0 |
| ETH | 1 | 1767226500000 | 3500.01 | 3505.51 | 3495.01 | 3500.51 | 10.7 |
| ETH | 2 | 1767227400000 | 3500.02 | 3505.52 | 3495.02 | 3500.52 | 11.4 |
| ETH | 6499 | 1773074700000 | 3564.99 | 3570.49 | 3559.99 | 3565.49 | 12.8 |
| SOL | 0 | 1767225600000 | 170.000 | 170.550 | 169.500 | 170.050 | 10.0 |
| SOL | 1 | 1767226500000 | 170.002 | 170.552 | 169.502 | 170.052 | 10.7 |
| SOL | 2 | 1767227400000 | 170.004 | 170.554 | 169.504 | 170.054 | 11.4 |
| SOL | 6499 | 1773074700000 | 182.998 | 183.548 | 182.498 | 183.048 | 12.8 |

Monotone-up drift is intentional: the panel is a provenance + geometry
anchor, NOT a strategy edge fixture. Backtest scores on it are asserted
on nothing (timing/`elapsedMs` excluded); only geometry-passage and hash
equality matter.

## 4. Shared GuardInput vectors (KEEP_GUARDS v2)

KEEP_GUARDS v2 (goals.ts:12-18): `medianLogReturn > 0`,
`winRatePct >= 52`, `medianDrawdownPct <= 12`,
`tradesPerSymMonth >= 4`, `expectancyPct > 0`. Bit order (guards.bend:24-28):
`0=log_return_nonpositive`, `1=winrate_below_52`,
`2=drawdown_above_12`, `3=throughput_below_4`,
`4=expectancy_nonpositive`.

V1/V2 reuse the exact `mutate.test.ts` "autoresearch guards" fixtures;
V3 is the new exact-threshold borderline vector:

| id | medianLogReturn | winRatePct | medianDrawdownPct | tradesPerSymMonth | expectancyPct | expected ok | expected mask |
|---|---|---|---|---|---|---|---|
| V1 fail | -0.01 | 55 | 8 | 40 | -0.1 | false | 17 (`1+16`: bits 0,4) |
| V2 pass | 0.01 | 52 | 10 | 5 | 0.2 | true | 0 |
| V3 borderline | 0.001 | 52 | 12 | 4 | 0.001 | true | 0 (every guard on its exact pass edge: strict `>` clears by ε; `>=`/`<=` clear exactly) |

V3 is the F32-risk vector: TS f64 vs Bend F32 rounding could flip a
strict-`>` comparison at the edge — the probe (§3 of the probe file)
declares TS ground truth and requires the COMPILED Bend binary to agree.
V3 passing on both sides proves the port holds on the threshold, not just
in the interior.

## 5. TS construction (no DB — `AlignedPanel` literal)

```ts
import type { Candle } from "./src/market-data/types.ts";

const T0 = 1767225600000;
const REF = 6500;
const SYMS = ["BTC/USDT:USDT", "ETH/USDT:USDT", "SOL/USDT:USDT"] as const;
const PARAMS = {
  "BTC/USDT:USDT": { base: 67000, drift: 0.05, tick: 2.0, halo: 50 },
  "ETH/USDT:USDT": { base: 3500, drift: 0.01, tick: 0.5, halo: 5 },
  "SOL/USDT:USDT": { base: 170, drift: 0.002, tick: 0.05, halo: 0.5 },
} as const;

function gen(s: keyof typeof PARAMS): Candle[] {
  const p = PARAMS[s];
  return Array.from({ length: REF }, (_, i) => {
    const open = p.base + p.drift * i;
    const close = open + p.tick;
    return {
      exchange: "bybit-futures", symbol: s, timeframe: "15m",
      open, high: Math.max(open, close) + p.halo,
      low: Math.min(open, close) - p.halo, close,
      volume: 10.0 + (i % 5) * 0.7,
      timestamp: new Date(T0 + i * 900000),
    };
  });
}
const aligned = new Map(SYMS.map((s) => [s, gen(s)] as const));
const panel = {
  symbols: [...SYMS],
  aligned, refLen: REF, loadedMs: 0,
  exchange: "bybit-futures", timeframe: "5m", panelTimeframe: "15m",
  panelHash: "295c6675", holdoutBars: 2880,
} as const;
```

Verify independently:

```ts
import { computePanelHash } from "./autoresearch/prepare.ts";
computePanelHash({ exchange: "bybit-futures", timeframe: "5m",
  panelTimeframe: "15m", symbols: [...SYMS], refLen: 6500,
  t0Ms: T0, t1Ms: T0 + 6499 * 900000 });  // REQUIRE "295c6675"
```

## 6. Why real-size by design (vs `pinned_6bar_panel.md`)

- 1-symbol/6-bar panel: forces TS `emptyResult`
  (`insufficient_symbols`) BEFORE the backtest — pins provenance +
  plumbing only, no shared GuardInput compared (see `parity_probe.md` §2).
- THIS panel: `3 symbols >= minSymbols 3` on every phase,
  `6500 >= max minCandles 6000`, selection windows exist on screen +
  confirm (§1) → TS reaches the backtest + `checkGuards`, so the probe
  can feed the SAME GuardInput (§4) to TS `checkKeepGuards` + Bend
  `check_keep_guards` for TRUE parity (bit-for-bit `ok` + 5-bit mask).
