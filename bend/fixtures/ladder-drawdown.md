# Ladder drawdown re-anchor fixture (peak re-anchor + paused-then-retry)

Single purpose: pin the exact bars, knobs, and asserted outcome for the
account-drawdown path — a flat account sitting below its peak must
re-anchor peak to capital and take the post-loss pause (paused-then-retry),
NOT latch dead forever, and must seed + fill again once the pause expires.
A Rust port replays these 6 bars and must reproduce every pinned number.

Read-only sources (never edited by this fixture):
`services/neuratrade-cli-ts/src/paper-trading/ladder-engine.test.ts`
(test `"re-anchors peak and pauses instead of latching dead on flat
drawdown breach"`, lines 162-192),
`services/neuratrade-cli-ts/src/paper-trading/ladder-engine.ts`
(`advanceLadderBar` re-anchor block ~957-987, `accountDrawdownBreached`,
`seedLadderSide` ~603-620). Verified by running
`bun test src/paper-trading/ladder-engine.test.ts` (24 pass).

Motivation (test-file comment, 2026-09-03): the ENA shadow (+2.64 book)
went permanently silent after an 8% peak-to-capital slide — flat capital
can never trade its way back, so without the re-anchor the kill latches
with no operator reset path.

## Port rules (commit a07b3dd0, verified in source — apply to this replay)

- `seedLadderSide` (ladder-engine.ts ~603-620) must NOT re-seed or wipe
  armed rungs when rungs already exist:
  `if (rungs.some((rung) => rung.filled)) return;` then
  `if (rungs.length > 0) return;` (added to fix demo churn). Chop / trend /
  drawdown gates apply ONLY to the EMPTY seed. Bar 5 below is the live
  proof: rungs armed on bar 1 survive bars 2-4 (pause ticks) and are
  STILL armed on bar 5 — a port that re-seeds or wipes on a blocked bar
  fails this fixture.
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
  returns that bar's `{fills, closes}`); while `paused > 0` the call ONLY
  decrements the counter and returns empty events (no seeding, no fills —
  bars 2-4 below). A Rust port needs per-bar state carry equivalent to
  `ResumeState` (capital, peak, wins, losses, paused, longBase/shortBase)
  PLUS the N armed rungs per side.
- Sizing/rounding policy: TS uses `Decimal`. Per-rung allocation =
  `capital * min(1, (maxPositionPct ?? 100)/100) / max(1, floor(rungs))`;
  fill qty = allocation / fillPrice (leverage/notional/contract-spec clamps
  in `ladderRungQty` are inactive here: no `contractSpecs`, no
  `maxNotionalPct`, default `maxLeverage 10`). Rust `scale()` (nt-grid
  engine.rs) truncates toward zero via `i128`; TS `Decimal` is exact (see
  pinned fill qty `0.45959596` = 41.6/99… precisely `45.5/99` below). The
  Rust port needs an explicit micro-tolerance on capital/qty comparisons.

## Bars (generator + literal values)

```ts
function candle(o, h, l, c, i) {
  return { timestamp: new Date(1000 * 60 * 15 * i),
           open: o, high: h, low: l, close: c, volume: 1000 };
}
const candles = [
  candle(100, 100, 100, 100, 0),
  candle(100, 100.1, 99.9, 100, 1),
  candle(100, 100.1, 99.9, 100, 2),
  candle(100, 100.1, 99.9, 100, 3),
  candle(100, 100.1, 99.9, 100, 4),
  candle(100, 99.0, 98.9, 99.3, 5), // 1% dip fills the reseeded rung
];
```

| i | timestamp ms | O | H | L | C |
|---|---|---|---|---|---|
| 0 | 0 | 100 | 100 | 100 | 100 |
| 1 | 900000 | 100 | 100.1 | 99.9 | 100 |
| 2 | 1800000 | 100 | 100.1 | 99.9 | 100 |
| 3 | 2700000 | 100 | 100.1 | 99.9 | 100 |
| 4 | 3600000 | 100 | 100.1 | 99.9 | 100 |
| 5 | 4500000 | 100 | 99.0 | 98.9 | 99.3 |

## Knobs (test-file `baseOptions` + overrides)

`exchange "bybit", symbol "TEST", timeframe "15m", rungs 2,
gridStepPct 1.0, gridMaxGrids 5, gridPauseAfterLossBars 3, feePct 0.05,
slippageBps 0, initialCapital 100, trendFilterPeriod 0, leverage 1,
maxDrawdownPct 8, chopGateAdxThreshold 0`. Working state starts
`freshWorkingState(100)` with `capital` then forced to `91`
(`money(91)`), so the account opens 9% under its peak (100) while flat —
past the 8% kill line. Breach predicate: `(peak - capital)/peak*100 >=
maxDrawdownPct` (0/undefined disables; `>= 100` also disables).

## Derivation walkthrough (what a replay must compute)

- Bar 1 (`step = 100 * 1/100 = 1.0`): flat + breached (9% >= 8%) →
  re-anchor: `peak = capital = 91`,
  `paused = max(0, floor(gridPauseAfterLossBars)) = 3`. Gates now see a
  0% drawdown → seed proceeds: long base 100, rungs at 99/98 (short at
  101/102). Fill pass: long rung1 untouched (`low 99.9 > 99`) — no fills.
- Bars 2-4: `paused > 0` → each call ONLY decrements (`3 → 2 → 1 → 0`)
  and returns empty events. Rungs stay armed (2 long, 2 short) — nothing
  is seeded, filled, or wiped while paused.
- Bar 5 (`step = 1.0`, `paused = 0`): flat + not breached (0%) → no
  re-anchor. `seedLadderSide`: long rungs non-empty (2 armed) → EARLY
  RETURN, levels stay 99/98 (this is the a07b3dd0 line — the port must
  keep, not rebuild, them). Fill pass: rung1 touched (`low 98.9 <= 99`),
  fills @99 with `qty = perRungAllocation/fillPrice = (91*1/2)/99 =
  45.5/99 = 0.45959596…`; rung2 untouched (`98.9 > 98`). Boundary
  `100 - 1.0*7 = 93` untouched; target 100 above bar high 99.0 → hold.

## Expected outcome (verified by executing TS, not hand-derived)

- After bar 1: `peak 91` (exact), `paused 3`, `longRungs.length 2`
  (armed, unfilled), capital still 91.
- After bars 2-4: `paused 0`, rungs still armed (2 long / 2 short).
- After bar 5: `longRungs.length 2`, exactly 1 filled
  (`rungIndex 1, entryPrice 99, entryBar 5, entryTimestamp 4500000,
  filledQty 0.45959596`); `closes.length 0`; `wins 0, losses 0`;
  `longBase 100`, capital still 91.
