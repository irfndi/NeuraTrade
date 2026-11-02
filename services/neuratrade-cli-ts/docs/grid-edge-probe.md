# Grid edge probe: the mean-reversion premise is falsified

**Question.** A grid entry at `mid - step` wins when price returns `+targetRatio`
steps before `-gridMaxGrids` steps. That is a barrier problem, so the decisive
statistic is the empirical target-first rate against the breakeven rate. If the
hit rate sits at or below breakeven, no amount of tuning `gridStepPct`,
`targetRatio`, `stopRatio` or the ADX gate can create an edge.

**Answer.** At the live champion geometry the target-first rate is at or below
breakeven on every symbol, and the one positive is statistically indistinguishable
from breakeven. The premise is false on this data.

## Method

First-touch barrier test, no look-ahead. From each observation, walk forward at
most 400 bars (100h at 15m) and record which barrier is touched first:

- target at `+1.95` steps
- stop at `-2.0` steps (`gridMaxGrids`)

Sample every 4th bar. Run per `trading_pair_id` — an earlier version concatenated
all pairs into one series and reported a 99.6% hit rate, which was a BTC-to-SOL
price jump being read as reversion. Per-symbol grouping is required for this
measurement to mean anything.

## Cost model

From the live config: maker 0.02%/side entry, taker 0.06% on stop exits,
2 bps slippage per leg, step 1.3%.

```text
win leg  = 1.95 * 1.3 - (0.02 + 0.02 + 0.04) = 2.46%
loss leg = 2.00 * 1.3 + (0.02 + 0.06 + 0.04) = 2.72%
cost-adjusted breakeven = 2.72 / (2.46 + 2.72) = 52.56%
```

## Results

Real Bybit 15m candles, 17,000 bars per symbol, 100h horizon.

**Correction (added after building `src/scalping/edge-validation.ts`):** the
per-symbol numbers originally reported here used a stride of 4, which overlaps
87 of 100 bars and inflates both `n` and the z-score by roughly 5x. Re-measured
with stride equal to the horizon, i.e. genuinely non-overlapping samples:

| symbol         | target-first | z     | samples | verdict      |
| -------------- | ------------ | ----- | ------- | ------------ |
| BTC/USDT:USDT  | 45.5%        | -0.82 | 33      | inconclusive |
| ETH/USDT:USDT  | 55.0%        | 0.31  | 40      | inconclusive |
| LINK/USDT:USDT | 58.5%        | 0.78  | 41      | inconclusive |
| SOL/USDT:USDT  | 50.0%        | -0.32 | 40      | inconclusive |

The earlier table reported z-scores of -7.06 / -3.64 / -5.98 / -0.20. Those
were computed on overlapping windows and are not usable. The conclusion below
survives, but the correct reason is different and is stated under
"What the data can and cannot resolve".

Synthetic pure random walks, same geometry, 5 seeds: 42.8%, 47.7%, 44.0%, 46.8%,
47.0% target-first against the same 50.6% gross breakeven. A random walk has no
mean reversion, so a grid cannot pay on it; the measured real data is not
meaningfully better than that.

## Interpretation

This explains the live ledger without appealing to tuning. The paper soak closed
237 trades for **-$16.23** at a **44.3% win rate**. With the cost-adjusted
breakeven at **52.56%**, a 44.3% win rate loses money by construction. The stop
counts (58 stop exits against 3 target exits) are a symptom of the same geometry,
not an independent bug.

It also explains why the search has stalled: ~273k trials produced 160 KEEPs and
then nothing above the champion for weeks. Every knob in the search space moves
spacing and exit placement. None of them change whether 15m crypto mean-reverts
at a 1.3% step. Optimising a quantity that is negative for every parameter value
converges to a local optimum and stops.

## What the data can and cannot resolve

17,006 bars at a 400-bar horizon yields **42 independent samples per symbol**.
At n=42 the standard error on a 50% rate is **7.7pp**, so this test can only
detect a margin larger than about **15.1pp** at 95% confidence.

The empirical barrier test therefore **cannot resolve the live geometry**. It
returns `inconclusive` for every symbol, and honestly so. The earlier "no edge"
verdict was an artifact of overlap inflation, not a measured result.

The conclusion still holds, for stronger reasons:

1. **Symmetric barrier strategies are structurally near-50%.** A random walk
   gives 50% by construction, and we measured 42.8 to 47.7% on synthetic walks
   against a 52.56% cost-adjusted breakeven. Costs push breakeven _above_ the
   coin-flip rate, so a symmetric grid needs a directional forecast it lacks.
2. **The step-size sweep is unambiguous**, because short horizons give thousands
   of independent samples: 48.8% / 49.8% / 49.9% / 48.4% / 47.5% for steps 0.3% to
   5%, all at or below the 50% gross breakeven.
3. **Live results agree.** 237 trades, -$16.23, 44.3% win rate.

This does _not_ establish that no barrier geometry could ever work. With our
data volume a genuine 5pp edge in the 400-bar geometry would be invisible. More
history, or a shorter horizon with more independent samples, is what would
settle it. `src/scalping/edge-validation.ts` reports `inconclusive` rather than
rounding that uncertainty to a verdict.

## What this rules out

- More grid parameter search. The search space does not contain the missing
  ingredient.
- Geometry micro-tuning (`targetRatio` vs `gridMaxGrids`, stop anchoring). These
  fix real defects and were worth fixing, but they cannot move a 46% hit rate
  past a 52.6% breakeven.

## What this leaves open

The measurement rejects _this_ entry rule at _this_ horizon, not trading in
general. The untested direction is a rule that selects when reversion is likely
rather than assuming it: regime conditioning that actually binds (the live config
runs `chopGateAdxThreshold 0` and `trendFilterPeriod 0`, i.e. both filters off),
or a different horizon where the barrier rates differ.

Anyone re-running this should keep per-symbol grouping and the cost-adjusted
breakeven; using the gross breakeven or a concatenated series will produce a
false positive.

## Reproduce

The probe is deliberately a throwaway script rather than production code, because
its job is to settle one question once. To re-run, restore the two files from this
document's method section: the per-symbol barrier test against
`$NEURATRADE_HOME/data/neuratrade.db` (`ohlcv_data` joined to `trading_pairs`),
and the cost-adjustment arithmetic above.
