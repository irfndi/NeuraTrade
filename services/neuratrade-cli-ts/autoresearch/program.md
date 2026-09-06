# NeuraTrade Autoresearch — program.md

You are running an overnight research loop to improve **ladder grid** expectancy.
Live money is frozen. Do not touch kill-switch, credentials, or SQLite position rows.

## Goal (claim when ALL true on a kept champion)

1. **Profitability:** median OOS window log-return > 0 (and beats the running baseline).
2. **Win rate:** overall win rate ≥ 48%.
3. **Throughput:** ≥ 4 trades per symbol-month (secondary — never optimize this alone).
4. **Risk:** median window max drawdown ≤ 15%.

Until claimed: keep looping. Prefer falsification over storytelling.

## Rules

- Edit **only** `knobs.ts` (or accept the mutation loop writing it).
- Never edit `prepare.ts`, risk/, paper-trading/, or live ecosystem configs from this loop.
- Each trial has a **fixed wall-clock budget** (`--budget-sec`, default 180).
- Metric is computed by `prepare.ts` — trust the printed `score` / `guardsOk`.
- **KEEP** only if `guardsOk` and `score` strictly beats the current champion.
- **DISCARD** otherwise; revert knobs to champion.
- Log every trial to `results/ledger.jsonl` (the runner does this).
- Do not promote anything to live. Paper soak is a separate human decision after claim.

## How to run

```bash
cd services/neuratrade-cli-ts
bun run autoresearch:once          # single evaluate of current knobs
bun run autoresearch:loop         # mutate → evaluate → keep/discard overnight
bun run autoresearch:loop -- --trials=50 --budget-sec=180
```

## Mutation hints (for agents)

Prefer one-knob changes. Useful axes: `gridStepPct`, `stopRatio`, `maxHoldBars`,
`targetRatio`, `rungs`, `gridMaxGrids`, `gridPauseAfterLossBars`, `chopGateAdxThreshold`.
Avoid leverage > 1 in this loop. Avoid fee/slippage edits (frozen in prepare).
