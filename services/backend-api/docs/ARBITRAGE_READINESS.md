# Arbitrage Readiness

Arbitrage is not approved for real-money use.

The `arbitrage_readiness_review` routine records explicit readiness status in
its quest checkpoint so placeholder scans and detected opportunities are not
mistaken for real-money proof. It always writes:

- `arbitrage_live_ready=false`
- `arbitrage_readiness_status=blocked`
- `arbitrage_readiness_blockers`
- `arbitrage_lifecycle_storage_verified`
- `arbitrage_readiness_evidence_metrics`
- `arbitrage_no_trade_safety=false`

The review captures lifecycle context over the previous 7 days, including
closed opportunities, wins, losses, net PnL, average net PnL, fees, open
positions, and inventory/exposure blockers. These metrics are diagnostic only
until the arbitrage execution path has realistic market evidence.

`arbitrage_readiness_evidence_metrics` mirrors the manifest proof fields:

- `closed_trades`
- `winning_trades`
- `losing_trades`
- `open_positions`
- `net_pnl`
- `avg_net_pnl`
- `max_drawdown_pct`
- `drawdown_verified`
- `execution_path_verified`
- `market_data_verified`
- `risk_limits_enforced`
- `backtest_comparison_verified`
- `cost_accounting_verified`
- `exposure_safety_verified`
- `no_trade_safety`
- `no_trade_reason`

`max_drawdown_pct` remains `0.00` and `no_trade_safety` remains false until a
real arbitrage exposure verifier or no-trade safety window supplies proof. The
generated metrics also keep all verifier booleans, including
`drawdown_verified`, false so they cannot satisfy the live-readiness manifest by
accident.

Minimum proof before the live-readiness manifest can mark `arbitrage` ready:

- executable spread or funding signal path using real exchange quotes
- real market data used by the verifier
- risk-limit enforcement verified
- paper/live-market results compared against the relevant backtest
- fee, slippage, funding, and transfer-cost accounting
- inventory and exposure safety for both legs
- paper/live-market lifecycle evidence with at least two closed opportunities,
  including both a win and a loss
- no open positions at proof cutoff
- positive net and average net PnL after all costs
- observed and bounded drawdown or exposure

The manifest may accept `no_trade_safety=true` for arbitrage only when an
observed market window proves no executable spreads/opportunities after costs,
includes a concrete `no_trade_reason`, uses verified real market data, verifies
cost accounting and exposure safety, and has zero open positions.
