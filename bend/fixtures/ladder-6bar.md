# Ladder oscillator fixture (6-bar parity panel)

Single purpose: pin the exact synthetic bar series and knob set that the
TS ladder paper engine (`runLadderPaperTradingIteration`,
`advanceLadderBar`) and the ladder backtest (`runLadderGridBacktest`) must
agree on bar-for-bar. After the TS sunset, a Rust port replays these bars
through its own `advance_bar` and must reproduce the pinned outcomes; the
TS-vs-backtest agreement recorded here is the proof the twin state machines
are identical, so the Rust port inherits the same target.

Read-only sources (never edited by this fixture):
`services/neuratrade-cli-ts/src/paper-trading/ladder-engine.test.ts`
(helpers lines 36-97: `candle`, `baseOptions`, `oscillatorSeries`;
`advanceLadderBar` describe lines 99-238),
`services/neuratrade-cli-ts/src/paper-trading/ladder-engine.ts`
(`advanceLadderBar`, `seedLadderSide` ~603-620, `closeRung`, `ladderRungQty`),
`services/neuratrade-cli-ts/src/scalping/ladder-grid.ts`
(`runLadderGridBacktest`). Verified by running
`bun test src/paper-trading/ladder-engine.test.ts` (24 pass).

## Port rules (commit a07b3dd0, verified in source — apply to every replay)

- `seedLadderSide` (ladder-engine.ts ~603-620) must NOT re-seed or wipe
  armed rungs when rungs already exist:
  `if (rungs.some((rung) => rung.filled)) return;` then
  `if (rungs.length > 0) return;` (added to fix demo churn). Chop / trend /
  drawdown gates apply ONLY to the EMPTY seed. A gate-blocked bar calls
  `setSideRungs(w, side, [])` (wipes to empty) ONLY when nothing is armed;
  blocked bars must never wipe armed rungs.
- The backtest twin carries the same rule (ladder-grid.ts ~515-531): two
  early returns — `if (state.rungs.some((r) => r.filled)) return state;`
  and `if (state.rungs.length > 0) return state;` — then the gate check,
  then a third `if (state.rungs.length > 0) return state;` before building
  fresh rungs. Seed only when EMPTY, not merely unfilled.
- Persisted-rung contract: iteration state round-trips through
  `repo.saveLadderState` / `getLadderState` (rung fields incl. `filledQty`,
  `level`, `step`, `entryPrice`, `entryBar`, `entryTimestamp` survive a
  restart); a config drift detected by `configMatchesLadderState`
  (compares exchange/symbol/timeframe/initialCapital + gridStepPct/
  gridMaxGrids/gridPauseAfterLossBars/rungs/targetRatio/onlyWithTrend/
  chopGateAdxThreshold + maxHoldBars + stopRatio + conservativeIntrabar)
  drives the flat re-seed / open-hold paths, and with
  `configMismatchAction: "force-reseed"` the iteration force-closes stale
  open rungs at the current close and re-seeds fresh
  (`forceReseedLadderState`, ladder-engine.ts ~1602-1710).
- Tick model: TS processes ONE bar per invocation
  (`advanceLadderBar(w, candles, i, opts, trend)` mutates `w` in place and
  returns that bar's `{fills, closes}`); a full pass starts at bar index 1
  (bar 0 is the seed/prev reference, never processed). A Rust port needs
  per-bar state carry equivalent to `ResumeState` (capital, peak, wins,
  losses, paused, longBase/shortBase) PLUS the N armed rungs per side.
- Sizing/rounding policy: TS uses `Decimal` throughout. Per-rung allocation
  = `capital * min(1, (maxPositionPct ?? 100)/100) / max(1, floor(rungs))`;
  fill qty = allocation / fillPrice (further leverage/notional/contract-spec
  clamps in `ladderRungQty` only bind on the live path and are inactive in
  this fixture: no `contractSpecs`, no `maxNotionalPct`, default
  `maxLeverage 10`). Close accounting (`closeRung`): `fee = makerFee*2`
  on `"target"`, `makerFee + takerFee` on `"stop"` (both `feePct/100` here,
  no cross fee), `net = pricePnl - fee`,
  `capital = capital * (1 + sizePerRung * net * leverage)` with
  `sizePerRung = positionFraction / N`. Rust `scale()` (nt-grid engine.rs)
  truncates toward zero via an `i128` intermediate; TS `Decimal` division is
  exact (see pinned fill qty `0.50505051` = 50/99 below). The Rust port
  needs an explicit micro-tolerance on capital comparisons — exact-decimal
  equality is NOT achievable across the boundary.

## Generator (regenerable — closed form, no DB, no randomness)

```ts
function candle(o: number, h: number, l: number, c: number, i: number) {
  return {
    timestamp: new Date(1000 * 60 * 15 * i),
    open: o, high: h, low: l, close: c, volume: 1000,
  };
}
function oscillatorSeries() {
  return [
    candle(100, 100, 100, 100, 0),
    candle(100, 101, 98.8, 99.0, 1),
    candle(99.0, 101.2, 99.0, 100.8, 2),
    candle(100.8, 100.8, 100.8, 100.8, 3),
  ];
}
```

Design intent (test-file comment): dips fill the long ladder, rallies take
profit back to flat. Bar 1 dips to fill rung 1 on both sides; bar 2 rallies
through both take-profit targets; bar 3 re-seeds flat at a new base.

Literal bars (O/H/L/C, `volume: 1000`, `timestamp = i * 900000` ms):

| i | timestamp ms | O | H | L | C |
|---|---|---|---|---|---|
| 0 | 0 | 100 | 100 | 100 | 100 |
| 1 | 900000 | 100 | 101 | 98.8 | 99.0 |
| 2 | 1800000 | 99.0 | 101.2 | 99.0 | 100.8 |
| 3 | 2700000 | 100.8 | 100.8 | 100.8 | 100.8 |

## Knobs (test-file `baseOptions`)

`exchange "bybit", symbol "TEST", timeframe "15m", rungs 2,
gridStepPct 1.0, gridMaxGrids 5, gridPauseAfterLossBars 0, feePct 0.05,
slippageBps 0, initialCapital 100, trendFilterPeriod 0, leverage 1`
(V1b variant: `leverage 3`, everything else identical).

Derived per-bar constants a replay must reproduce (long side shown; short
is the mirror; both sides trade in this fixture):
`step = open * gridStepPct/100` re-derived per bar; rung levels
`base - k*step`; boundary (stopRatio 0) `base - step*(rungs+gridMaxGrids)`;
target `entryPrice + rung.step * targetRatio`, exited at `target/slippage`;
conservative intrabar: a rung filled on bar `i` may NOT close on bar `i`
(`entryBar < barIndex` required) — so bar-1 fills survive into bar 2.

| bar | step | long base | rung levels | boundary | fills | closes |
|---|---|---|---|---|---|---|
| 1 | 1.0 | 100 | 99, 98 | 93 | long rung1 @99 (`qty 50/99 = 0.50505051…`), short rung1 @101 | 0 (bar-1 high 101 touches long target 100 but same-bar close is barred) |
| 2 | 0.99 | 100 (kept — rung filled, no re-seed) | 99, 98 (kept) | 93.07 | 0 | long rung1 @100 target, short rung1 @100 target |
| 3 | 1.008 | 100.8 (re-seed, flat) | 99.792, 98.784 | 93.744 | 0 | 0 |

Short-side mirror of bar 1: rung1 @101 (`qty 50/101 = 0.4950495…`),
boundary 107, target 100 — also held into bar 2 by the same-bar rule.

## Expected outcome (verified by executing TS, not hand-derived)

- V1 (leverage 1): bar-1 fills 2 (one per side), closes 0; bar-2 fills 0,
  closes 2 (both `"target"` @100); bar-3 idle. Final capital
  `100.902125210021002`, `totalWins 2, totalLosses 0, paused 0`.
  Backtest agrees: `totalReturnPct 0.902125210021012` → capital
  `100.90212521002103` (differs only past the 12th decimal).
- V1b (leverage 3): same event shape, final capital `102.71852683`,
  backtest `totalReturnPct 2.7185268301830092`.
- Test assertions (test file lines 100-118): `incrementalCapital ≈
  backtestCapital` to 6dp on both variants, and capital moved off 100
  (both engines actually traded).

## Why this fixture exists

Proves the paper engine and the backtest are the same state machine on a
series that fills, holds across a bar boundary, takes profit, and re-seeds —
so any capital divergence a Rust port shows on THESE bars is a port bug,
not a model difference. Widen the series only by appending bars; never
edit bars 0-3 (V1/V1b pins above would silently change).
