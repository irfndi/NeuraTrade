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
