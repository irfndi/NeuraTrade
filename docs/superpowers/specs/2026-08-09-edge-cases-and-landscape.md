# Honest Edge Landscape + Edge-Case Fix Batch — 2026-08-09

Three research agents + two fixer agents + a validation loop. The "many
cases not think of" pass: the audit found real, live-verified defects, all
fixed; the validation loop produced the definitive edge landscape.

## The definitive landscape (validation loop, AgentValidate, honest fees)

**Nothing survives BOTH honesty mechanisms** (strict time-split AND the
execution-stress gates at maker 0.02%/side):

| config | full-period | time-split IS / OOS | funnel gates | verdict |
|---|---|---|---|---|
| BTC 15m grid (step1/p24/t3/adx28) | +4.27% | **−20.99% / +34.06%** | PASS (confLB +0.0033) | regime-concentrated — gate-passing but the edge is recent-only |
| SOL 5m grid (pause-24 variant) | +25.26% | +15.65% / +8.31% (all splits) | FAIL (windows 47.6%, confLB −0.0025, stressLB −0.0017) | time-split-positive but execution-fragile |
| ETH 5m grid | +10.69% | +24.80% / −13.37% | — | in-sample artifact |
| fundingCarry BTC 15m | +6.41% | +15.33% / **−7.73%** | — | edge faded; OOS-negative in every variant; pure-funding edge lives only inside the funding-coverage window |

The funnel gates (trade/window-based) cannot see regime splits — the new
strict time-split gate fixes that at the source.

## Edge cases found (AgentEdgeHunt) and fixed (FixerOrderability + FixerHonesty, commit 31536902)

1. **CRITICAL orderability** (live-verified): the 5 USDT floor is unorderable
   for BTC/ETH/SOL (BTC 0.000077 < minTradeNum 0.0001; exchange rejects; the
   simulated adapter masked it). Fixed: contract-spec-aware sizing
   (effectiveFloor = max(minTradeUSDT, minQty×price), qty rounds UP to the
   step with a down-round fallback, leverage raises to afford the floor, the
   cap binds on MARGIN, unorderable orders SKIP with "RISK BLOCKED
   (orderability)" on the simulated path too) + guard fail-closed
   (minOrderableNotional; adapter rejects below-min qty / off-step).
   Exit-price finding: already fixed in the checkout (closePosition
   normalizes); regression test locked.
2. **Funding PnL was a flat constant** — now accrues the real historical
   rates (mean over the hold window, fallback preserved).
3. **bootstrapBlockConfidence threw on <5 samples** — crashed the universe
   scan; now returns a degenerate interval.
4. **--funding-bias-threshold / --use-funding were inert** — now wired into
   the composer.
5. **Capital excluded from the grid state match + fingerprint** — the
   challenge-10 and btc-candidate soaks shared a state row; now distinct
   (idempotent initial_capital migration).
6. **$10 challenge app**: --leverage 2 (BTC min-qty margin 32% of $10 ≤ 50%
   cap) + the sizing fixes make it actually orderable (BTC/SOL; ETH needs
   ~$39 at 50%).

## Funnel improvement (commit 652467b7)

Strict time-split IS/OOS gate on gate-scored eligibility: both the
first-80% and last-20% slices must be profitable AND trade. Would have
rejected 3 of the 4 positive-looking cells in the sweep.

## State

981/981 tests, typecheck clean, all committed (`31536902`, `652467b7`).
Live: 7 pm2 apps, soaks clean, funnel running with the new gate.
