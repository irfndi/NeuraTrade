# NeuraTrade Full Project Analysis — 2026-08-09

Six-agent analysis (architecture, data, goal-gap, ops, record + pipeline from
session history). Purpose: guide the journey toward the $10 -> $1M goal.

## 1. Architecture (Agent: ArchitectureMap)

- **The live system is `services/neuratrade-cli-ts/`** (Bun + effect@4): 171
  `.ts` files, ~29k lines src; `scalp.ts` is a 6,266-line god-file with 13
  subcommands; `backtest.ts` runBacktestCore spans 832 lines (single largest
  function), `grid-engine.ts` runGridPaperTradingIteration 732 (the LIVE hot
  path). 22-26 effect DI tags wired via `index.ts buildRootLayer`.
- **Audit memo dead-code claims: mostly confirmed.** Dead: portfolio-backtest
  (1,425), strategy-config, config-runner, backend-risk-gated-futures,
  utils/env, market-data/collector, grid-sweep, 6/7 ApiClient methods,
  SqliteClientLive initSchema. **Two claims WRONG**: logger.ts and
  scalping/services.ts are LIVE (the DI assembly). Net CLI dead ≈ 3,300
  lines (the ~17k total incl. backend-api/telegram stands).
- `services/backend-api` (Go :8080) is the only cross-service dependency
  (CLI spawns it, polls /health). Telegram (:3002) + ccxt are started by the
  CLI but never called directly.

## 2. Pipeline (session history + agents)

data (ohlcv + funding) -> backtest/gate-scored search -> universe funnel
(market list -> volume -> stage2 -> walk-forward -> deep-fetch -> gate +
time-split -> probe -> selection) -> watchlist -> soaks (demo/universe +
candidates + challenge-10) -> grid_paper_trades -> readiness gates.

**Live gaps**: the tradeability probe drops gate-eligible symbols (CYS
"not demo-tradeable"); the selection target is mis-set (account-capital
1000 -> target 50/day vs the $10-50 accounts); the watchlist is frozen on a
stale ADA row (0 selected -> never replaced); 5m data exists but nothing
trades it; the readiness cohort clock hasn't started (0 fills).

## 3. Data edges (Agent: DataEdges + session)

- Funding: binance BTC/ETH 5.5y (mean +0.0100%/8h, 85.5% positive, autocorr
  0.84 — persistent); bitget 270 rows/symbol mostly negative (short-biased
  carry on the live venue). Funding PnL now honest (historical rates wired).
- Price regime: BTC/SOL 15m 2y + 5m 1y; **the 1% level has NOT been touched
  in 7 days** (current low-vol regime — the 0.22-0.6 fills/day model is
  current-reality; P(0 fills in 8h) ~93%).
- Universe cache: 222 symbols at ~8.6k bars (1.5 months); survivors at
  0.1-0.5% steps (~5 entries/day/symbol — the high-frequency path, just
  unblocked from the sizing-collapse bug).
- Missing data for better edge-finding: order-book history, per-symbol
  funding beyond 3 majors, 1m data, historical fills with real PnL.

## 4. Goal gap (Agent: GoalGap + challenge assessment)

- Goal: $10->$1M needs 3.2-13.65%/day (365/180/90d). Edge needed: 0.8-1.4%
  /trade sustained = 70-80x the validated 0.02-0.06%.
- Current capability: gate-validated cohort 0.22-0.6 fills/day x eligible
  0-1; universe pool ~5 entries/day/symbol x survivors (just unblocked);
  funding capture marginal (OOS-negative); scalps rejected. **Real
  achievable now: ~0-3 fills/day, ~0.02-0.06% edge/trade -> 0.01-0.06%/day
  — 100-1,000x short of the target.**
- The gap is fundamentally EDGE, not fills. Pool size alone cannot close it.

## 5. Ops reliability (Agent: OpsReliability + verification)

- **Fixed this session**: pm2 dump re-saved (the deleted 5m crash-loopers
  would have resurrected on reboot — now clean), sizing collapse, cohort
  exclusion, killed-flag auto-clear, orderability.
- **Open fragility**: NO alerting on anything (fills, kill switch, app
  death, the eligible count); NO DB backup — the 3.95 GB single-file SQLite
  holds the readiness evidence at total-loss risk; NO log rotation (~400KB/hr
  during the 5m crash era shows the blast); the account-wide kill switch is
  manual-recovery-only and non-alerting (a phantom once held the cohort for
  ~13h of disagreement); the monitor script only checks demo-soak liveness +
  BTC fills (misleading — the soak trades altcoins now).
- At-scale: 55+ entries/day will stress Bitget rate limits, the single-file
  DB, and the account-wide kill switch (one phantom halts everything).

## 6. The record (Agent: RecordState)

Docs are decision-dense and mostly accurate, but: the audit memo's dead-code
list needs correction (logger/services.ts live); the paper-trading RESULTS
are unrecorded (0 fills era + the sizing-collapse root cause deserve a
postmortem); no roadmap doc exists; the incident postmortems are partial.

## Recommendations — the ranked roadmap

1. **Ops hardening (this week, cheap, prerequisite):** DB backup job
   (sqlite3 .backup via launchd/cron), pm2 log rotation (max_size), a real
   monitor covering ALL apps + kill-switch state + the funnel eligible count
   + alerts on first-fill (replace the misleading demo-soak monitor). Cost:
   hours. Expected: no silent data-loss/incident risk.
2. **Measure the unblocked universe pool (this week):** let the demo-soak
   trade ADA at $25; measure its REAL fills/day vs the ~5/day model. This is
   the first real throughput data and validates whether the high-frequency
   path works at all.
3. **Funnel acceleration (next):** the high-throughput tier (light
   time-split + fills/day gates on the gate-dropped survivors — 6-8
   selectable immediately vs 0-1 today) + raise the deep-fetch budget
   (300 -> 900). Expected: pool to 6-8, universe fills toward 30-40/day.
4. **Scale capital WITH the pool (later):** $50 is min-notional-bound;
   more symbols need more capital — the pool must grow first.
5. **The edge wall (the honest blocker):** funding-capture rework
   (payment-time alignment, per-venue signs) is marginal; a genuinely new
   edge needs data the system lacks (order-book history, more funding
   symbols). The 20-400x edge gap to the compounding target is NOT closable
   by the current pools — the roadmap gets the system to real trading
   (steps 1-4) and the honest long game is edge discovery, not throughput.

**Verdict**: the project is a well-instrumented, honest trading system with
real (small) edges and a healthy pipeline — but it is not, and with current
data cannot become, a $10->$1M vehicle. The roadmap maximizes real fills/day
(steps 1-3), which is the correct and only available path to a bigger edge
harvest.
