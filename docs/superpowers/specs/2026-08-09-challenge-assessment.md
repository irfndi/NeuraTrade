# $10 → $1M Challenge — Honest Assessment (2026-08-09)

Six-agent research + refactor batch. The challenge goal, the math, the one
real edge lead, and what shipped.

## 1. The math (agent: ChallengeMath)

| target | horizon | required daily return |
|---|---|---|
| $10 → $1M | 90d | 13.65% |
| $10 → $1M | 180d | 6.61% |
| $10 → $1M | 365d | 3.20% |
| $100 → $2M | 112d | 9.25% |

- Feasible (winrate, R:R, risk) triples EXIST for every target (e.g. 60%
  winrate, 2R, 2% risk, 10 trades/day → 16%/day) — feasibility is never the
  constraint. **The per-trade EDGE is**: 0.8–1.4%/trade sustained = 70–80×
  the validated 0.02–0.06%.
- Ruin: any positive-edge profile has P(50% DD) ≤ 2e−11 — ruin is a binary
  function of edge SIGN, not variance. All real risk lives in un-modeled
  reality (edge decay, tiny-account fees, execution, the 5 USDT floor).
- The 2% daily loss cap vs 7–14% target: even at 90% win-days, 112d needs
  +10.5% average win-days — an option-like payoff shape retail access does
  not offer. The cap is CORRECT; the target is not.
- Reality benchmark: 0.5%/trade retail edge @10/day = Sharpe ~17 (best
  professionals: 1–3) — unattainable. Validated system: 0.01–0.06%/day.
  Elite trend: 0.18–0.6%/day (2× per 112 days, not 20,000×). Maker-rebate
  at scale: ~1%/day, unproven.
- **Verdict: the "100 → 2M in 112 days" stories are survivorship/selection
  artifacts. The median of a genuinely-capable profile is $3.3B/112d —
  nobody publishes that because nobody can hold the edge.**

## 2. The one real edge lead (agent: StrategySpace)

**Funding-rate capture.** BTC perpetual funding in the data: mean
+0.0100%/8h ≈ **+10.95%/yr at 1x**, median +0.0079%, **85.5% positive**,
lag-1 autocorrelation 0.838 (strongly persistent). High winrate by
construction (hold through payment times). Both-direction capable.

**Critical wiring gap found**: the funding bias component is DEAD in every
CLI backtest — `backtest.ts` accepts `options.fundingRates` and
`composer.ts` consumes `ohlcv.fundingRates`, but ZERO call sites supply
them. Every fundingCarry/backtest so far ran with the flat
`--funding-rate-pct` assumption; the fundingCarry backtest (PF 1.083,
+15.24%) never fired its funding component. Bitget's live funding is
negative most of the time (2–6% of rates over 1bp, all negative) — the
strategy must adapt per venue (long-biased carry on Bitget, short-biased
on Binance history).

Also found: the breakout dial is inert in the backtest command
(buildBacktestComposerConfig never receives breakoutLookback).

## 3. What shipped (refactor batch, all committed `aab8a796`)

- **Account-scaled sizing** (futures-engine): per-trade risk capped by
  min(riskPerTrade, maxDailyLossPct/maxConcurrentTrades); sub-floor notional
  (5 USDT) raised with leverage ceil(floor/A) ≤ maxLeverage; computed
  leverage threaded through guard/adapter/order placement. 3 new tests.
- **Tiny-account funnel**: `accountSymbolCap = max(1, floor(A×0.5/10))` —
  $10 accounts now select 1 symbol (concentrated mode) instead of 0.
- **5m cadence apps** (btc/sol-candidate-5m, --interval 300) — stopped by
  default, for exploratory runs.
- Wire: paper-trade futures options activate the sizing bounds.

## 4. Recommendations

1. **Wire funding rates into the backtest engine** — the highest-value build
   in the repo: it unlocks honest fundingCarry validation (the one strategy
   family with a real, persistent, high-winrate edge in the data).
2. **Reframe the goal**: prove sustained positive edge on paper 6–12 months;
   then compound 0.1–1%/day at scale. The challenge's 3–14%/day is an edge
   test, not a plan.
3. **Keep the 2% daily cap** — it is correct; the target is not.
4. Floor-capital reality: at $10 the 5 USDT floor forces stops ≤1.34%
   (15m noise territory) or ~35% cap-breach days at 2% risk — a $10 account
   is a stress test, not a compounding vehicle; $100+ is the realistic start.
