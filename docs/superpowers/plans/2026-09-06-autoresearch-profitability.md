# Autoresearch Profitability Loop

> **For agentic workers:** execute task-by-task; do not touch live kill-switch or delete position state.

**Goal:** Overnight keep/discard search over ladder knobs that maximizes OOS log-growth under win-rate and drawdown guards (throughput as secondary, never primary).

**Architecture:** Karpathy-style autoresearch — frozen `prepare.ts` harness, single editable `knobs.ts`, fixed wall-clock budget per trial, ledger of keep/discard. Live trading stays halted until recon + creds.

**Tech Stack:** Bun, existing `runLadderGridBacktest`, mainnet SQLite candles.

**Spec:** chat design 2026-09-06 (growth-under-guards; no live).

## Global Constraints

- Never disengage kill-switch from this loop
- Never DELETE grid_paper_state / trading DB rows
- Fee 0.02% RT + 2 bps slip fixed in prepare
- Primary metric: median window log-return; guards: winRate, medDD, min trades/sym-mo, positive expectancy
