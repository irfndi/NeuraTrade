# Arbitrage Readiness

Arbitrage is not approved for real-money use.

The `arbitrage_readiness_review` routine records explicit readiness status in
its quest checkpoint so placeholder scans and detected opportunities are not
mistaken for real-money proof. It always writes:

- `arbitrage_live_ready=false`
- `arbitrage_readiness_status=blocked`
- `arbitrage_readiness_blockers`
- `arbitrage_no_trade_safety=false`

The review captures lifecycle context over the previous 7 days, including
closed opportunities, wins, losses, net PnL, average net PnL, fees, open
positions, and inventory/exposure blockers. These metrics are diagnostic only
until the arbitrage execution path has realistic market evidence.

Minimum proof before the live-readiness manifest can mark `arbitrage` ready:

- executable spread or funding signal path using real exchange quotes
- fee, slippage, funding, and transfer-cost accounting
- inventory and exposure safety for both legs
- paper/live-market lifecycle evidence with at least two closed opportunities,
  including both a win and a loss
- no open positions at proof cutoff
- positive net and average net PnL after all costs
- observed and bounded drawdown or exposure

The manifest may accept `no_trade_safety=true` for arbitrage only when an
observed market window proves no executable spreads/opportunities after costs,
includes a concrete `no_trade_reason`, and has zero open positions.
