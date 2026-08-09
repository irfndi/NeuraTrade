# Throughput Research — 2026-08-09 (faster/bigger while profitable?)

**Question:** can the system produce more fills/day while every symbol keeps
passing the profitability gates?

**Verdict (3 parallel agents, live data): throughput is POOL-BOUND. Every
other lever is closed by data.**

## 1. The throughput ceiling is the gate-eligible pool size

Funnel pass-through (real): 801 contracts → 134 (volume ≥1M) → ~85 (stage-2
stats) → 10–20 (walk-forward) → **2–5 gate-eligible**. Per-symbol throughput
is fixed at 0.22–0.60 fills/day by the gates (BTC passers 0.218–0.252, SOL
0.267–0.596; gate pass rate 0.9–1.4% by design).

| account | symbol cap | target/day | achievable now | pool needed for target |
|---|---|---|---|---|
| $50 | 2 | 5 | 0.6–1.2 | 9–17 eligible |
| $1000 | 50 | 50 | 0.6–3.0 | 84–167 eligible |
| $10000 | 500 | 50 | 0.6–3.0 | 84–167 eligible |

**Capital is never binding** ($1000 vs $10000 = zero marginal throughput
today). Cadence has 160× headroom (15m soak vs 0.2–0.6 touch rate) — not
binding. The metric that matters: **gate-eligible symbol count** (2–5 → 9–17
for 5/day; 84–167 for 50/day).

## 2. Levers closed by data (all researched, all fail)

- **Frequency (scalp family)**: 2.4–4.7 fills/day/symbol at maker fees —
  10–20× the grid family — but negative expectancy on every config even at
  maker 0.02%/rebate 0.0% (gross edge ~+0.02%/trade fully consumed by the
  0.04% round trip; connorsRsi2 5m "passes" at +0.002% printed but is
  -0.018% counting the entry fee). More throughput would compound losses.
- **Smaller steps**: 0/576 configs pass at 0.1–0.5% steps (fee + adverse
  selection bleed).
- **Taker fees**: 0/1728 passers at 0.06%.
- **Faster cadence**: no gain (touch-rate gated); 5m negative-edge.
- **Multi-product**: real Bitget ceiling is **801 contracts** (741 USDT +
  49 USDC + 11 COIN), 134 volume-passing — NOT ~2000; USDC/COIN adds ~5
  symbols. Also found: USDC futures native symbols are `BTCPERP` (current
  `toBitgetFuturesSymbol` would emit `BTCUSDC` → 40034; latent bug, fix is
  a 1-line transform + optional productType param).

## 3. The only lever that moves the ceiling: pool graduation

The funnel's deep-fetch machinery (300 req/cycle ≈ 60k bars/survivor/cycle)
grows each walk-forward survivor toward gate eligibility. That pipeline is
the throughput engine; nothing else matters until the pool saturates.

## Recommendations

1. Track **gate-eligible symbol count** as the headline metric (the funnel
   logs `Gate-scored funnel: survivors → eligible → selected` already).
2. Let the deep-fetch cycles graduate the 10–20 survivors — days–weeks to
   ~9–17 eligible (5/day at $50) — the only real build is patience + the
   existing machinery.
3. Defer: capital scaling (until pool >50), cadence/frequency research
   (empirically closed), multi-product (gains ~5 symbols; the PERP symbol
   fix is worth doing independently as a latent-bug fix).
4. If 5–50/day is a hard requirement with the gates intact, the honest
   options are: (a) gate thresholds must loosen (admits the losers the data
   shows), or (b) a different fee/venue model (maker rebates at scale) —
   both are owner decisions, not engineering.
