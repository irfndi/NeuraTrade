# Daily Trading Readiness

Daily trading is not approved for real-money use.

The registered `daily_report` routine records an explicit readiness checkpoint
instead of failing as an unknown routine. It always writes:

- `daily_trading_live_ready=false`
- `daily_trading_readiness_status=blocked`
- `daily_trading_lifecycle_storage_verified`
- `daily_trading_drawdown_verified=false`
- `daily_trading_readiness_evidence_metrics_status`
- `daily_trading_readiness_evidence_metrics`
- `daily_trading_readiness_blockers`

The report captures lifecycle context over the previous 24 hours, including
closed trades, wins, losses, net PnL, average net PnL, open positions, and a
manifest-shaped metrics object. These metrics are diagnostic only until an
executable daily strategy path, drawdown proof, and evidence artifact exist.

`daily_trading_readiness_evidence_metrics` mirrors the manifest proof fields:

- `closed_trades`
- `winning_trades`
- `losing_trades`
- `open_positions`
- `net_pnl`
- `avg_net_pnl`
- `max_drawdown_pct`

`max_drawdown_pct` remains `0.00` until a real drawdown verifier supplies proof,
and the metrics include `diagnostic_only=true` and `drawdown_verified=false` so
downstream consumers can distinguish placeholder diagnostics from a
manifest-ready evidence artifact. These fields also keep the generated metrics
from satisfying the live-readiness manifest by accident.

Minimum proof before the live-readiness manifest can mark `daily_trading` ready:

- executable daily signal path with documented entry and exit rules
- paper/live-market lifecycle evidence over representative daily cadence
- at least two closed trades with both a win and a loss
- no open positions at proof cutoff
- positive net and average net PnL after fees
- observed and bounded drawdown
- evidence artifact written and referenced by `NEURATRADE_LIVE_READINESS_MANIFEST`
