# Ladder boundary-stop fixture (whole-ladder stop-out + loss pause)

Single purpose: pin the exact bars, knobs, and asserted outcome for the
boundary stop-out path — one rung fills, price craters through the ladder
boundary, the WHOLE ladder force-closes at the boundary, losses are counted,
and the post-loss pause engages. A Rust port replays these 3 bars and must
reproduce every pinned number.

Read-only sources (never edited by this fixture):
`services/neuratrade-cli-ts/src/paper-trading/ladder-engine.test.ts`
(test `"stops out the whole ladder at the boundary and pauses after a loss"`,
lines 120-144), `services/neuratrade-cli-ts/src/paper-trading/ladder-engine.ts`
(`advanceLadderBar`, `seedLadderSide` ~603-620, `sideStopBoundary`,
`applyLadderRiskExit`, `closeRung`). Verified by running
`bun test src/paper-trading/ladder-engine.test.ts` (24 pass).

## Port rules (commit a07b3dd0, verified in source — apply to this replay)

- `seedLadderSide` (ladder-engine.ts ~603-620) must NOT re-seed or wipe
  armed rungs when rungs already exist:
  `if (rungs.some((rung) => rung.filled)) return;` then
  `if (rungs.length > 0) return;` (added to fix demo churn). Chop / trend /
  drawdown gates apply ONLY to the EMPTY seed. A gate-blocked bar wipes to
  empty ONLY when nothing is armed; blocked bars must never wipe armed rungs.
- The backtest twin carries the same rule (ladder-grid.ts ~515-531): two
  early returns on filled / non-empty rungs, then the gate check, then a
  third non-empty guard before building fresh rungs. Seed only when EMPTY.
- Persisted-rung contract: iteration state round-trips through
  `repo.saveLadderState` / `getLadderState` (rung fields incl. `filledQty`,
  `level`, `step`, `entryPrice`, `entryBar`, `entryTimestamp` survive a
  restart); config drift detected by `configMatchesLadderState` drives the
  flat re-seed / open-hold paths, with `configMismatchAction:
  "force-reseed"` force-closing stale open rungs and re-seeding fresh.
- Tick model: TS processes ONE bar per invocation
  (`advanceLadderBar(w, candles, i, opts, trend)` mutates `w` in place,
  returns that bar's `{fills, closes}`); a full pass starts at bar index 1
  (bar 0 is the seed/prev reference, never processed). A Rust port needs
  per-bar state carry equivalent to `ResumeState` (capital, peak, wins,
  losses, paused, longBase/shortBase) PLUS the N armed rungs per side.
- Sizing/rounding policy: TS uses `Decimal`. Per-rung allocation =
  `capital * min(1, (maxPositionPct ?? 100)/100) / max(1, floor(rungs))`;
  fill qty = allocation / fillPrice (leverage/notional/contract-spec clamps
  in `ladderRungQty` are inactive here: no `contractSpecs`, no
  `maxNotionalPct`, default `maxLeverage 10`). Close accounting
  (`closeRung`): `"stop"` fee = `makerFee + takerFee` (`feePct/100` each
  here, no cross fee), `net = pricePnl - fee`,
  `capital = capital * (1 + sizePerRung * net * leverage)`. Rust `scale()`
  (nt-grid engine.rs) truncates toward zero via `i128`; TS `Decimal` is
  exact — the Rust port needs an explicit micro-tolerance on capital.

## Bars (generator + literal values)

```ts
function candle(o, h, l, c, i) {
  return { timestamp: new Date(1000 * 60 * 15 * i),
           open: o, high: h, low: l, close: c, volume: 1000 };
}
const candles = [
  candle(100, 100, 100, 100, 0),
  candle(100, 99.5, 98.9, 99.3, 1), // rung1 (level 99) fills; boundary is 93
  candle(99.3, 99.3, 90, 90, 2),    // craters below boundary -> stop-out both
];
```

| i | timestamp ms | O | H | L | C |
|---|---|---|---|---|---|
| 0 | 0 | 100 | 100 | 100 | 100 |
| 1 | 900000 | 100 | 99.5 | 98.9 | 99.3 |
| 2 | 1800000 | 99.3 | 99.3 | 90 | 90 |

## Knobs (test-file `baseOptions` + override)

`exchange "bybit", symbol "TEST", timeframe "15m", rungs 2,
gridStepPct 1.0, gridMaxGrids 5, gridPauseAfterLossBars 3, feePct 0.05,
slippageBps 0, initialCapital 100, trendFilterPeriod 0, leverage 1`.
Working state starts `freshWorkingState(100)` (capital = peak = 100,
wins = losses = 0, no rungs, bases 0, paused 0).

## Derivation walkthrough (what a replay must compute)

- Bar 1 (`step = 100 * 1/100 = 1.0`): no fills yet so `seedLadderSide`
  seeds long base 100, rungs at levels 99 and 98 (short side seeds at
  101/102 and never interacts — low never reaches it). Fill pass: long
  rung1 touched (`low 98.9 <= 99`), fills @99 with
  `qty = 50/99 = 0.50505051…`; rung2 untouched (`98.9 > 98`). Boundary
  check: `100 - 1.0*(2+5) = 93`, `low 98.9 > 93` → no exit. Target 100
  (`entry 99 + step 1.0 * ratio 1`) above bar high 99.5 → rung holds.
- Bar 2 (`step = 99.3 * 1/100 = 0.993`): seed skipped (rung filled —
  the a07b3dd0 early return; levels stay 99/98). Fill pass: rung2 touched
  (`low 90 <= 98`), fills @98 with `qty = 50/98 = 0.51020408…`. Boundary
  recomputed with the new step: `100 - 0.993*7 = 93.049`;
  `low 90 <= 93.049` → touched → WHOLE ladder exits at `93.049`
  (`resetAll: true`), reason `"stop"`, both closes recorded, `paused = 3`.
- Pause tick: replaying bar 0 while `paused = 3` consumes one pause tick
  with zero events (`paused → 2`).

## Expected outcome (verified by executing TS, not hand-derived)

- After bar 1: `closes.length 0`; exactly 1 long rung filled
  (`entryPrice 99, entryBar 1, entryTimestamp 900000, filledQty 0.50505051`).
- After bar 2: `closes.length 2` (both `reason "stop"`, `exitPrice 93.049`);
  capital `94.447135770975057503` (rung1 leg `100 → 96.94444444444444`,
  rung2 leg `→ 94.447135770975057503`); `totalLosses 2, totalWins 0`;
  no filled rungs remain (`longRungs` emptied, `longBase` reset to 0);
  `paused 3`. Short rungs stay armed-but-empty throughout.
- Pause replay: `closes.length 0`, `paused 2`.
- Full-slice parity: fresh `incrementalCapital` over bars 1-2 =
  `94.44713577…`, backtest `totalReturnPct`-implied capital
  `94.44713577097507` — agreement to 6dp is asserted in the test.
