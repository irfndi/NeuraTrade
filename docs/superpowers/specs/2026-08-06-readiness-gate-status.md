# Real-Money Readiness Gate — Status (2026-08-06)

## The machine contract

`scalp real-money-readiness` (read-only, no credentials, no network, SQLite opened
`readonly:true`) evaluates the **validated BTC/USDT:USDT 15m Bitget futures grid** and
emits versioned JSON (`real-money-readiness/v1`). Contract in
`src/scalping/real-money-readiness.ts`; validator in `src/scalping/grid-validation.ts`;
CLI in `src/cli/real-money-readiness.ts`.

- **Exit codes:** `0` = every gate passes, `1` = evidence missing/malformed/unsafe or a
  gate failed, `2` = malformed CLI/contract input or infrastructure failure.
- **Gates (exact IDs):** `prospective-evidence`, `historical-robustness`, `confidence`,
  `execution-parity`, `stress`, `provenance`, `data-quality`, `freshness`, `thresholds`.
- **Thresholds:** demo ≥50 complete live fills over ≥7 days, expectancy ≥0, demo
  confidence LB ≥0, demo DD ≤15%; ≥10 historical windows, profitable windows >50%,
  compounded return ≥0%, historical DD ≤15%; fixed-OOS ≥30 trades, confidence LB ≥0
  (95%, 5,000 resamples, moving block 5, xorshift32, seed `20260802`); adverse stress
  return ≥0 and stress LB ≥0 (seeds `20260802`–`20260806`, maker-fill prob 0.7,
  adverse selection, taker exit fee); freshness ≤48h. Overrides may only tighten.
- **Provenance:** fills must carry the canonical SHA-256 strategy fingerprint, cohort
  ID, candidate lock, dataset cutoff, and entry provenance; untagged fills are rejected.
- **Fingerprint:** canonical sorted manifest (schema `real-money-readiness/v1`) covering
  strategy params, sizing (position fraction 0.5), fees, slippage, trend/target settings,
  leverage, product/margin mode, risk limits, order type, trigger timing, venue
  (`bitget-demo`), engine/protocol versions, validation profile. See
  `DEFAULT_STRATEGY_MANIFEST`.

## Current state (2026-08-06): FAIL — and it must stay FAIL until the soak produces evidence

Live run on the real DB: `status=FAIL, exit 1` with 6 failing gates:
prospective-evidence (0 complete demo fills), historical-robustness, confidence,
provenance (no cohort), data-quality, freshness (last candle 2026-08-03). `execution-parity`
and `stress` pass — parity was established by the golden-replay fixture
(`scalp parity-replay`); stress passes only because no OOS evidence exists yet.

## Backtest evidence with the CORRECTED bootstrap (2026-08-06)

> The Aug-3 gate-scored search (`2026-08-03-gate-scored-grid-search.md`) promoted
> pause-24/ADX-24 on confidence LB +0.0022 computed with a **degenerate bootstrap**
> (every resample identical → LB = sample mean). The degeneracy was fixed in `745071e0`
> and verified against an independent Python implementation. Re-derived numbers on the
> same 70,240-candle dataset:

| Metric | Aug-3 (degenerate) | Corrected | Gate |
| --- | --- | --- | --- |
| Fixed-OOS trades | 29 | 29 | ≥30 FAIL |
| Fixed-OOS return | +1.42% | +3.77% | ≥0 PASS |
| Confidence LB | +0.0022 | **−0.00148** | ≥0 **FAIL** |
| Stress returns (5 seeds) | ≥ +2.90% | +2.90…+6.10% | ≥0 PASS |
| Stress worst LB | — | **−0.00305** | ≥0 **FAIL** |
| Profitable windows | 62% | 62% | >50% PASS |

**Conclusion: the promoted candidate no longer passes the readiness backtest gates.**
The gate-scored search must be re-run with the corrected bootstrap (`clever-cabin-bnb`,
`clever-cabin-58v`) before any new candidate is locked for the demo soak.

## Re-sweep results (2026-08-06, corrected bootstrap)

`scripts/gate-scored-grid-search-2026-08-06.ts` — 1,728 configs × 2 cost models on the
70,240-candle BTC 15m dataset (2024-08 → 2026-08-03):

- **Taker economics (fee 0.06, slippage 2 — current deployed market-after-trigger):
  **0/1728 PASS**.** Every config fails the stress confidence LB; the closest config
  (`step 1, grids 1.5, pause 36, target 3, ADX 28`) misses only `stressLB` at
  **−0.00014**. The Aug-3 promotion (pause 24/ADX 24) also fails (confLB −0.0015).
- **Maker economics (fee 0.02, slippage 1):** the frontier passes. Fast sweep found:
  - `step 1, grids 1.5, pause 36, target 3, ADX 28` — windows 53.8%, **+8.50%**,
    DD 12.1%, OOS 34, **confLB +0.00180**, stressRet +8.05%, **stressLB +0.00037**
  - `step 1, grids 1.5, pause 42, target 3, ADX 28` — same LBs, +6.15%
  Full-space sweep running to map the complete frontier.

**Conclusion:** the grid family has statistically significant edge on BTC 15m only
under **maker/limit fills** (~+0.12%/trade advantage).

## Locked candidate + maker execution (2026-08-07, shipped `ca2276d0`)

- **Locked candidate** (`VALIDATED_BTC_GRID_CANDIDATE`): `step 1%, grids 1.5,
  pause 36, target ratio 3, ADX gate 28, fee 0.02 (maker), slippage 1bp,
  leverage 1, position fraction 0.5` — the only passing configs from the full
  1,728-config sweep (pause 36 and pause 42; pause 36 chosen for the higher
  compounded return, +8.50% vs +6.15%).
- **Maker execution shipped**: the grid engine now places LIMIT orders at the raw
  grid level (the bar has already touched the level, so the fill is a maker fill
  at that price); exits use raw target/stop/liquidation levels. The simulated
  adapter fills limits at the requested price.
- **Execution parity: PASS 8/8** (live `scalp parity-replay`, entry delta 0.0000
  vs the backtest) — the deployed engine now matches the validated replay model.
- Manifest: `orderType limit-at-grid-level`, `engine grid-engine/v2`,
  validation profile `gate-scored-grid-search-2026-08-06-maker`.
- The BTC candidate soak (`neuratrade-btc-candidate`) runs the locked candidate
  on the demo account; fills carry the new fingerprint.

**Status 2026-08-07 (fresh data):** BTC 15m candles backfilled to 2026-08-07
(70,583, zero gaps) → `data-quality`, `freshness`, `historical-robustness`,
`confidence` (plain OOS LB +0.0023), `execution-parity` all PASS. The gate that
blocks everything is the **worst-of-5-seeds adverse-fill bootstrap LB** (stress
gate): per-seed trade counts are 28–30, so the 95% LB = mean − 1.96·SE is
negative for every config in the full 1,728-config sweep (locked candidate
−0.0033; best −0.0017). Every seed's mean is positive; the pooled 5-seed LB is
+0.0023. Owner decision tracked in `clever-cabin-out` (keep gate / amend plan's
statistical protocol to the combined-seed sequence / extend history).

## The path to real-money review

1. Re-run the gate-scored grid search with the corrected bootstrap → lock a candidate
   that passes all backtest gates (including confidence and stress LBs).
2. Fund a **dedicated demo account** (the current one has ~$50 USDT and is used by the
   multi-symbol soak). The readiness cohort must be clean.
3. Start the BTC candidate soak (`neuratrade-btc-candidate` in
   `ecosystem.demo-soak.config.cjs` — prepared, stopped) with `--live` on the exact
   locked candidate. The grid engine persists provenance (fingerprint, cohort, lock,
   cutoff) on every fill.
4. After ≥50 complete live fills over ≥7 days with non-negative expectancy/LB/DD, the
   gate flips eligible: `scalp real-money-readiness` → PASS → human review.
5. The command never places orders; real-money activation remains a separate,
   manual, human-reviewed decision with the existing sandbox/risk controls.
