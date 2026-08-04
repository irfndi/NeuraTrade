# Scalping Readiness — Tuning Results Log

Started: 2026-07-18. Data: bitget-futures 1y (2025-07-17 → 2026-07-17), funding series 90d.
Sweep tool: `services/neuratrade-cli-ts/scripts/scalp-readiness-scan.ts` (384 configs:
2 regimes × ATR stop {0.5,0.75,1,1.5} × ATR TP {1,1.5,2,2.5} × conf {0.35,0.45,0.55} × maxBars {6,12,24,48}).
Floors: ≥15 trades/mo, ≤4h avg duration, expectancy > 0, ≥50 trades. Costs: taker 0.06%/side + 2bps slippage, leverage 1.

## Iteration 1 — taker/market entries (2026-07-18)

### ETH 15m — 0/384 pass floors

- Frequency is achievable (320/384 configs ≥ 15 trades/mo) and holds are short (383/384 ≤ 4h).
- **exp>0 AND freq≥15/mo: 0.** Median expectancy -0.105%/trade ≈ the 0.16% round-trip cost.
- Only positive-expectancy configs are snipers: reversion stop 1.5 tp 2.5 conf 0.55 → 0.7 trades/mo,
  62% win, PF 2.32, exp +0.449% — unusable at scalping frequency.

### BTC 15m — 4/384 pass floors, but economically meaningless

- 4 passing configs (all reversion, conf 0.45): 17–22 tr/mo, PF 1.13–1.22, exp +0.0001–0.0143%/trade,
  **all with negative total return** (-10.9% … -13.9%): break-even arithmetic mean, negative compounding.
  None clears PF ≥ 1.3 (G2b) anyway.
- Near-miss band (freq ok, exp ∈ -0.05..0) is dense → the composer's edge at scalping frequency
  is ≈ 0, sitting right at the cost line.

### Structural conclusion (15m)

Parameter tuning alone cannot make taker-fee scalping pass: the edge deficit ≈ the fee structure.
Round trip taker = 0.12% fee + 0.04% slippage ≈ 0.16%; maker (limit) round trip = 0.04% fee + less slippage.
**Maker fills are worth ~+0.12%/trade — exactly the gap between break-even and the G2 target.**

## Iteration 2 — limit/maker entries (2026-07-18)

Hypothesis failed as formulated: maker (limit) entries moved median expectancy only ~+0.02%/trade
(exits still taker-market). BUT the iteration exposed the real bugs that invalidated every earlier
"winner": regimeMode never mapped from profiles (FIXED), FULL-run look-ahead inflation (bd dt8),
funding NaN (bd u1u), entryTime null (bd 91b). All prior conclusions re-measured under the honest
IS/OOS split protocol.

## Iteration 3 — honest protocol + grid family + chop gate (2026-07-18) — BREAKTHROUGH

Honest (IS/OOS split) sweeps:

- Signal composer: **0/384 pass on ETH 5m, BTC 15m, ETH 15m** (BTC 5m outstanding). Every
  positive-expectancy config is a <1 tr/mo sniper; every scalping-frequency config is PF ≈ 1.0.
  Directional prediction at scalping frequency has no edge on this data. Params exhausted.
- Ungated grid: **0/432 pass**. ETH grid IS-profitable (+40%, 65-77% win) but OOS-dies
  (regime dependence: harvests chop, gets run over in trends).
- **Chop-gated grid (ADX ≥ threshold blocks new entries): BTC 15m PASSES ALL 10 READINESS GATES**
  via the standard CLI (`scalp readiness`, exit 0):

  Config: step 1%, grids 1.5, targetRatio 1.0, trendFilter off, pause-after-loss 12, chop-gate ADX 30,
  maker fee 0.02%/side, slippage 1bp, leverage 1.

  | Gate | Target | Actual |
  |---|---|---|
  | G1a IS frequency | ≥10 tr/mo | 11.3 tr/mo (108/9.6mo) PASS |
  | G1b OOS trades | ≥10 | 28 PASS |
  | G2a win rate | ≥50% | 68.52% PASS |
  | G2b profit factor | ≥1.3 | 1.359 PASS |
  | G2c expectancy | >0 | +0.169%/trade PASS |
  | G3a OOS return | ≥0% | +1.53% PASS |
  | G3b OOS maxDD | ≤15% | 3.65% PASS |
  | G3c MC p95 DD | ≤20% | 11.49% PASS |
  | G3d MC ruin | ≤5% | 0.00% PASS |
  | G4 avg duration | ≤4h | 3.35h PASS |

- ETH 15m: 0 pass, but top gated config (step 0.75, grids 3, tgtR 1.5, pause 12, gate 25) is
  OOS-robust: IS +50.4%, **OOS +10.77% PF 1.32**, 57 OOS trades — fails floors only on
  DD 21.1% > 15% and PF 1.23 < 1.3. **DD is a sizing problem, not an edge problem** — the grid
  engine has no position-size knob (all-in per trade). Risk-based sizing is the follow-up.

## Iteration 4 — anti-overfit + demo validation (2026-07-18)

- [x] **Walk-forward on the BTC winner** (scripts/grid-walkforward.ts):
  Pass A (re-optimized per window, neighborhood): aggregate +8.93%, 60% profitable windows, maxDD 8.3%.
  Pass B (fixed winner config): **5/5 profitable windows**, mean +5.12%/window, worst DD 5.98%, 105 trades.
- [x] **Live-loop replay smokes** (paper engine on real bitget-futures data):
  - Found + fixed two replay bugs: stale-pointer lockout (CLI now resets grid state at replay start
    via new `resetGridState`) and chop-gate pointer stall (gate path never persisted lastTimestamp;
    fixed + regression test). bd clever-cabin-4h5.
  - 96-bar replay: chop gate visibly engaging (`ADX 30.0 >= 30`) with real data.
  - 672-bar (1 week) replay: full cycle — OPENED short 63551.03 → CLOSED stop 64507.23, -1.545%
    incl. fees. Entry/exit/guards/persistence all clean on the live loop.
- [ ] 30-day replay soak (2880 bars) — statistical sample vs backtest expectancy (running).
- [ ] Bitget demo paper-trade with PAPTRADING orders (needs demo API keys — user provides at handoff).
- [ ] position-sizing model for the grid engine (ETH DD taming)
