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
make -C ../.. autoresearch-once
make -C ../.. autoresearch-loop          # 1 worker, panel cached, screen→confirm
make -C ../.. autoresearch-parallel      # 4 pm2 workers, shared champion lock
```

Each trial: cheap **screen** (~7d windows) → only promising knobs pay full **confirm** (~30d).
Candle panel loads once per process.

## Mutation hints (for agents)

Prefer one-knob changes. Useful axes: `gridStepPct`, `stopRatio`, `maxHoldBars`,
`targetRatio`, `rungs`, `gridMaxGrids`, `gridPauseAfterLossBars`, `chopGateAdxThreshold`.
Avoid leverage > 1 in this loop. Avoid fee/slippage edits (frozen in prepare).
