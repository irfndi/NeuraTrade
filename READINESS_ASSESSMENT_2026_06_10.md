# NeuraTrade Readiness Assessment (post-fix, 2026-06-10)

**Date**: 2026-06-10
**Verdict**: **NOT READY for real money** — but with the unprofitable-strategy blocker now honest
**vs. previous report (2026-06-08)**: Major corrections to backtest numbers; production-bug fixes landed

---

## What changed since the 2026-06-08 report

1. **Backtest SQL bug fixed** (`4507e1ce`): the scalping backtest engine was
   emitting PostgreSQL-flavored SQL against SQLite, returning 500 on every
   run. The bug masked the true state — only the 30d run was producing
   results. With the fix, the engine runs end-to-end and exposes the real
   numbers in the table below.
2. **CLI backtest auth fixed** (`4507e1ce`): the CLI was sending
   `X-API-Key` against a route that only accepted JWTs, so
   `neuratrade backtest run` returned 401. The backtest route now accepts
   either auth (intentionally limited to research routes; do not apply to
   order/balance endpoints).
3. **Orphan paper_trades reconciler wired** (`c9d7acc2` + `201ccfb8`):
   the live paper-trading path opened rows in `paper_trades` but only the
   backfill path closed them, so positions accumulated unboundedly. On
   2026-06-08 there were 120 open rows with $701.98 cost basis. The
   reconciler now runs at startup and every 10 minutes, closing any
   `paper_trades` row older than 4h. Verified live: 120 → 29 open rows
   on first sweep, no false-positive closes on fresh positions.
4. **Risk-actor daily loss accumulation fix** (`201ccfb8`): the previous
   implementation overwrote `dailyLoss` with the latest delta instead
   of accumulating across multiple `UpdateDailyLoss` messages. Fixed
   in `risk_actor.go:handleUpdateDailyLoss`.

## Updated backtest results (post-fix, deterministic mode)

Run via `RUN_READINESS=1 go test -v -count=1 -run TestRealMoney_ReadinessBacktests ./internal/services/`.
Command: `BTC/USDT, ETH/USDT, SOL/USDT, BNB/USDT, XRP/USDT`, `binance`,
mode=`deterministic`, initial capital=$10,000.

| Window | Signals | Trades | Win% | PnL (USDT) | PnL% | Max DD% | vs. old report |
|---|---|---|---|---|---|---|---|
| 30d (Apr 15 → May 15 2026) | 9,404 | 41 | 48.78% | **-2.30** | -0.023% | 0.13% | Old: 808 / 46% / +138.34 (cherry-picked window) |
| 90d (Mar 1 → Jun 1 2026) | 30,122 | 102 | 45.10% | **-32.89** | -0.33% | 0.34% | Old: 8 / 0% / -8.73 (sparse signals) |
| 180d (Dec 1 2025 → Jun 1 2026) | 74,667 | 120 | 40.00% | **-41.34** | -0.41% | 0.83% | Old: 9 / 33% / -1.03 (sparse signals) |
| 1yr (Jun 10 2025 → Jun 10 2026) | 178,186 | 283 | 38.16% | **-8.77** | -0.09% | 0.72% | Old: not run |

**Honest verdict:** deterministic mode is unprofitable at every window.
The 30d window that looked great in the old report (+$138) was the only
signal-rich slice. The "expectancy" gate rejects 99.9% of signals, but
the trades that do pass have `avg_loss > avg_win` (e.g. 30d: avg win
$1.94 vs avg loss $1.96) — a 1% edge that disappears once fees are paid.

This is a **strategy robustness** problem, not an infrastructure problem.
The next phase of work is research, not wiring.

## Live trading state (2026-06-10)

- **Live orders** (last 3 days): 117 closed, $179.47 total notional
  (all on bitget). All under $0.10 PnL per trade — the test balance of
  $17.30 futures / $0.21 spot produces micro-positions by design
  (issue #455).
- **MaxLossMonitor**: running with 1.5% threshold, 1s poll interval,
  all 4 configured exchanges. No fire events in the live window
  (expected — no position has dropped 1.5% from entry).
- **Open paper_trades**: 29 rows, $169.99 cost basis (all <4h old, in
  active live trading).
- **Open live positions**: 0 (every position closes within the same
  scalping cycle).
- **Orphan paper_trades**: 0 (reconciler closed 91 stale rows on
  startup; 0 accumulated since).

## What the post-fix engine fixed

| Issue | Symptom | Fix |
|---|---|---|
| `scalping_backtest` returns 500 | "near 'FROM': syntax error" | `sqlitePoolProbe` marker interface — `c9d7acc2`, `4507e1ce` |
| `neuratrade backtest run` returns 401 | CLI sends `X-API-Key` against JWT-only route | `RequireAuthOrAdmin` middleware on backtest routes — `4507e1ce` |
| `paper_trades` rows accumulate forever | 120 open rows, $701.98 cost basis | Periodic reconciler on the shadow coordinator — `c9d7acc2` + `201ccfb8` |
| `MaxDailyLossRule` resets on each `UpdateDailyLoss` | Daily loss reporting was inaccurate | `handleUpdateDailyLoss` accumulates via `Add()` — `201ccfb8` |

## Open real-money blockers (post-fix)

### P0 — strategy not profitable

The deterministic scalping strategy is unprofitable on all long-term
windows. The 30d window that looked like a green light was a fit, not
a generalization. Strategy robustness is the **first** gate for
real money and it is not met.

Recommended next steps:

1. Re-tune the expectancy gate: the `expectancy_below_min_edge` rejection
   count is the single biggest filter (1601/120 = 13x the trade count
   in 180d, 3740/283 in 1yr). Lowering the threshold should produce
   more trades but may also amplify the negative edge.
2. Investigate the regime breakdown: `neutral` and `trend` regimes
   each have ~40% win rate. A regime-specific strategy split may
   improve aggregate performance.
3. Consider whether the deterministic fallback is even the right
   baseline — the AI mode is blocked on DeepSeek credits
   (`402 Insufficient Balance`), so all live scalping is already
   running deterministic.

### P1 — paper trading lacks diversification

- All 76 paper-trade closes on the active strategies are BNB/USDT
  (issue #451). The 1yr backtest only covers BTC/ETH/SOL/BNB/XRP
  on binance. There is no overlap between the paper-traded
  universe and the backtested universe, so paper results prove
  nothing about the backtested symbols.
- The baseline strategy is still opening positions without exits;
  even with the reconciler, it should be capped at fewer symbols
  or disabled entirely.

### P1 — no live-trading safety guard on real capital

- `MaxLossMonitor` is running but no fire event has been observed
  in the live window. The first real-money deployment should run
  with a hard daily-loss cap that pauses trading entirely
  (issue #453: $205 drawdown on $323 profit = 63.5% ratio).
  The `RiskActor.UpdateDailyLoss` is exposed but not wired into
  the live scalping path; only the simpler `KillSwitch` and
  `SafeMode` are active.

### P2 — testnet evidence

- Bitget has $17.30 futures + $0.21 spot. This is too small to
  produce meaningful live data (issue #455). A real-money readiness
  review should require 30+ days of testnet data with non-trivial
  position sizes.

## Minimum viable criteria for "ready for real money"

Same as the 2026-06-08 report — no changes:

- [ ] Profitable 90-day backtest (currently -$32.89)
- [ ] Profitable 180-day backtest (currently -$41.34)
- [ ] Full 5-year data coverage for all symbols (SOL/BNB still sparse)
- [ ] 30-day testnet validation (Bitget balance too small)
- [ ] Daily-loss-cap pause on the live scalping path
  (issue #453, not wired)
- [ ] Multi-symbol paper trading with overlap to backtested universe
  (issue #451, still BNB-only)

**My call:** do not deploy real capital until the strategy is
profitable on a 90-day window and a daily-loss pause is in place.
The infrastructure (reconciler, dual-auth, risk-actor) is in
much better shape than 48h ago, but **the engine is the wrong
place to fix an unprofitable strategy**.

---

*Assessment generated 2026-06-10 by post-fix verification run*
*Oracle verification: pending*
