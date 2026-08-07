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

## Plan amendment 2026-08-07 (owner decision B): pooled stress LB

**Decision:** amend the stress gate's statistical protocol — the stress LB is the
moving-block bootstrap (block 5, 5,000 resamples, seed `20260802`) over the
**combined 5-seed trade sequence** (n≈145), not the worst of the per-seed LBs.
Threshold unchanged (LB ≥ 0). Seeds, resample count, and block length unchanged.

**Rationale:** the binding failure was estimator-wide, not edge: per-seed trade
counts of 28–30 make the 95% per-seed interval (mean ± 1.96·SE, SE ≈ 0.35–0.7%)
wider than the per-seed mean (+0.003…+0.008/trade, all five seeds positive).
Pooled n=145 → SE ≈ 0.15% → LB +0.0023 on the same candidate, same data. The
plan's letter ("stress LB ≥ 0, seeds `20260802`–`20260806`") lists the seeds as
the evidence set, not as five separate tests; the worst-of-seeds reading was
introduced by the implementation. Option A (keep gate) was rejected as
hope-based; option C (3–5y history) rejected as months of collector work for an
estimator artifact.

**Conditions (unchanged):** per-seed runs remain in the evidence as diagnostics;
`worstReturnPct` remains the gate's stress-return input (conservative, passes at
+2.90…+6.10%). Regime sensitivity is NOT cured by pooling — the OOS window flip
(+0.00037 → −0.0033 worst-of-seeds when fresh candles moved the window) is
monitored by re-running the gate on every fresh backfill. The cohort clock
(`prospective-evidence` ≥50 fills over ≥7 days) and `provenance` remain binding
and unamendable.

**Code:** `grid-validation.ts` emits `stress.pooledLowerBoundPct`;
`cli/real-money-readiness.ts` feeds it to the `stress` gate;
`scripts/gate-scored-grid-search-2026-08-06.ts` scores on it. Regression test
added in `grid-validation.test.ts`.

**Gate board after amendment (fresh 70,583-candle dataset):** `stress` PASS.
Remaining FAIL = `prospective-evidence` (0 demo fills) + `provenance` (no
cohort) — both gated on the demo soak, per protocol.

## Sweep re-run under the amended protocol (2026-08-07)

`gate-scored-grid-search-2026-08-06.ts` (maker: fee 0.02, slip 1) on the fresh
70,583-candle dataset, stress LB = pooled 5-seed bootstrap:

- **Fast space (360 configs): 12 PASS** (was 0 under worst-of-seeds). Locked
  candidate `step 1, grids 1.5, pause 36, target 3, ADX 28` passes:
  windows 53.8%, ret +8.50%, dd 12.06%, OOS 33, confLB +0.00227,
  stressLB +0.00235. Results:
  `~/.neuratrade/tuning/gate-scored-search-2026-08-06-fee0.02-slip1.json`.
- **Lock unchanged** (owner decision B kept the locked candidate as the soak
  target). Note: `pause 24, target 3, grids 1.5` now also passes with the
  fattest margins in the space (confLB +0.00367, stressLB +0.00422, ret
  +10.10%, dd 10.76%) and trades more often — available re-lock candidate if
  the owner wants a faster cohort clock; not applied to avoid churning the
  lock outside the agreed amendment.
- **⚠ Full-space re-sweep stalled** at config ~1050–1100 (step 1, grids 2, pause
  12–24 region) on the fresh dataset. **Diagnosed 2026-08-07: NOT a code bug.**
  A per-config repro driver over the exact region (configs 1030–1120) completed
  clean (91/91, ≤2.5 s each), and the re-run stalled again under system load
  avg ~32 on an 8-core M1 with near-zero CPU delivered to the process —
  scheduler starvation (hundreds of runnable threads from editor/MCP servers).
  The full sweep is CPU-bound (~1.3 s/config ≈ 40 min on an idle machine);
  re-run when the host is idle.

## Additional fix 2026-08-07: stress gate fail-closed on missing evidence

The CLI's missing-evidence default for the stress gate carried the full seed
set with zero/zero metrics, so `stress` PASSED spuriously whenever the backtest
validation failed (e.g., no/stale candles) — inconsistent with `confidence`
(resamples 0 trips its protocol check). Fix: when `validateGridEvidence` yields
no OK result, the stress evidence now emits `seeds: []`, tripping the existing
"adverse stress seed set is incomplete" check. Regression test in
`src/cli/real-money-readiness.test.ts` (no-candles home must fail the stress
gate). Live board with real evidence is unchanged: `stress` still PASS.

## Additional fix 2026-08-07: readiness evidence scoped to the cohort

The readiness queries returned **every** demo fill for the symbol/timeframe and
required **all** of them to carry the canonical fingerprint + cohort fields
(`trades.every`). The DB holds 17 legacy BTC/USDT:USDT 15m fills (pre-provenance
engine) with NULL provenance fields — so the provenance gate could NEVER pass,
even after the new soak produced a clean tagged cohort. Fix: the evidence query
now scopes to `cohort_id IS NOT NULL` ("untagged fills are rejected" per the
protocol). Legacy rows are excluded from prospective evidence and provenance;
the gate fails only on a genuinely missing cohort. Regression test: an untagged
legacy row must not count toward the cohort nor veto provenance. Also corrected
the stale candidate description in `ecosystem.demo-soak.config.cjs` (comment
still described the Aug-3 pause-24/ADX-24 promotion; the app args were already
the locked pause-36/ADX-28 candidate). Live: `provenance rows 0/0`, gate FAIL =
no cohort yet (correct).

## Candidate re-lock 2026-08-07: pause 24 (owner decision, frequency review)

Fill-frequency review of the 12 passing configs (fresh data, OOS 147 days):
the whole family is pinned at 6.6–7.5 fills/month (~7 months to the 50-fill
cohort gate); ADX-28 is the dominant throttle and every passer shares it, so
no passing config materially accelerates the clock. On evidence quality,
`pause 24, target 3, grids 1.5` strictly dominates the prior lock (pause 36):

| | pause 36 (old lock) | **pause 24 (new lock)** |
| --- | --- | --- |
| confLB | +0.00227 | **+0.00367** |
| stressLB | +0.00235 | **+0.00422** |
| ret / dd | +8.50% / 12.06% | **+10.10% / 10.76%** |
| OOS trades | 33 | 33 |
| cohort clock | ~7.3 mo | ~7.3 mo |

Re-locked: `VALIDATED_BTC_GRID_CANDIDATE.gridPauseAfterLossBars 36 → 24`;
soak args + comments aligned (`--grid-pause-after-loss-bars 24`). Fingerprint
derives from the candidate, so the provenance manifest updates automatically.
Verified live: confLB +0.003667, stressLB +0.004222, all backtest gates PASS,
remaining FAIL = prospective-evidence + provenance (no cohort yet).

## Full-space sweep completed 2026-08-07 (1,728 configs, pooled stress LB)

The previously stalled full sweep completed on an eased host: **16 PASS** (12
fast-space + 3 new: `grids 1 pause 48 t3`, `grids 1.5 pause 12 t2`, `grids 1.5
pause 48 t1` — all adx 28). **Lock confirmed optimal on evidence margins:**
pause-24/t3/grids-1.5 holds the fattest LBs in the entire space (confLB
+0.00367, stressLB +0.00422; nearest rival +0.00131/+0.00128, and the
higher-return configs run 3–19× thinner margins). No config outside the fast
space dominates the lock; the ~7.3-month cohort clock stands (fastest
alternative, pause-12 t2 at ~6.5 mo, has the thinnest margins in the set).
Results: `~/.neuratrade/tuning/gate-scored-search-2026-08-06-fee0.02-slip1.json`
(now the full-space pooled run).

## Soak re-verified 2026-08-07

`neuratrade-btc-candidate` was found ONLINE (pm2, ~12h uptime) running the
pre-re-lock pause-36 args from pm2's config snapshot. No fills had been placed
(DB shows only the 17 Jul-18 legacy rows — cohort still clean). Restarted via
`pm2 startOrRestart ecosystem.demo-soak.config.cjs --only neuratrade-btc-candidate`;
verified via ps: `--grid-pause-after-loss-bars 24 --target-ratio 3
--chop-gate-adx 28`. Cycling every 15m: `HOLD | capital=50.00 | no action`.
Fills, when they come, carry the pause-24 fingerprint matching the readiness
manifest.

## BTC+SOL cohort (schema v2) 2026-08-07 — clock ~7.3 → ~3.3 months

Owner decision: multi-symbol cohort to cut the 50-fill clock without weakening
any gate threshold. `real-money-readiness/v2`: each cohort symbol is evaluated
against its own backtest-validated candidate; the gate board merges (every
gate must pass for every symbol); prospective evidence is the cohort UNION of
complete fills; provenance is merged across symbols (per-symbol manifest
fingerprints). ETH rejected (0 PASS in the fast sweep — no edge in this
family). SOL validated: `step 1.25, grids 2, pause 36, target 4, ADX 26`
(confLB +0.00133, stressLB +0.00325, OOS 39 ≈ 8 fills/mo). BTC+SOL ≈ 15
fills/mo → 50 fills ≈ 3.3 months. Soaks: `neuratrade-btc-candidate` +
`neuratrade-sol-candidate` (both on the ~$50 demo, `--capital 50`).

### Operational incident 2026-08-07 (resolved)

The universe soak opened a SOL position (0.3 @ 73.235, universe params) whose
exchange record showed available 0 vs total 0.3 → account-wide persisted kill
switch engaged and held BOTH candidate soaks even after the exchange-side
record vanished. Resolved: deleted the phantom `grid_paper_state`/watchlist
rows, disengaged the kill switch after verifying zero exchange positions and
clean local cohort state, restarted soaks (clean `HOLD | no action` cycles).
Root-cause fix: `persistSurvivors` excludes `READINESS_COHORT_CANDIDATES`
symbols from the universe watchlist/whitelist. Future resets:
`scalp paper-trade --disengage`.
