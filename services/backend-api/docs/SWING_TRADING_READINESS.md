# Swing Trading Readiness

Swing trading is not approved for real-money use.

The `swing_trading_review` routine records explicit readiness status in its quest
checkpoint so operators do not infer readiness from generic lifecycle activity.
It always writes:

- `swing_trading_live_ready=false`
- `swing_trading_readiness_status=blocked`
- `swing_trading_lifecycle_storage_verified`
- `swing_trading_drawdown_verified=false`
- `swing_trading_readiness_evidence_metrics_status`
- `swing_trading_readiness_evidence_metrics`
- `swing_trading_readiness_blockers`

The review captures lifecycle context over the previous 14 days, including
closed trades, wins, losses, net PnL, average net PnL, open positions, and stale
open positions. These metrics are diagnostic only until an executable swing
signal path and paper/live-market hold-window proof exist.

`swing_trading_readiness_evidence_metrics` mirrors the manifest proof fields:

- `closed_trades`
- `winning_trades`
- `losing_trades`
- `open_positions`
- `net_pnl`
- `avg_net_pnl`
- `max_drawdown_pct`

`max_drawdown_pct` remains `0.00` until a real swing drawdown verifier supplies
proof, and the metrics include `diagnostic_only=true` and
`drawdown_verified=false` so downstream consumers can distinguish placeholder
diagnostics from a manifest-ready evidence artifact. These fields also keep the
generated metrics from satisfying the live-readiness manifest by accident.

Minimum proof before the live-readiness manifest can mark `swing_trading` ready:

- executable swing signal path with documented entry and exit rules
- paper/live-market lifecycle evidence over representative longer holds
- at least two closed trades with both a win and a loss
- no open positions at proof cutoff
- positive net and average net PnL after fees
- observed and bounded drawdown
- stale-position handling demonstrated
