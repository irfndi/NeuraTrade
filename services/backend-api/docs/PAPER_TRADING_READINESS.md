# Paper Trading Readiness

Paper trading is the simulation layer used before any strategy can be considered
for real-money execution.

The `paper_trading_review` routine records an explicit readiness checkpoint. It
runs a deterministic simulator probe covering market entry, limit take-profit,
stop-loss, and position close PnL. The routine writes:

- `paper_trading_ready=false`
- `paper_trading_readiness_status=blocked`
- `paper_trading_runtime_probe_passed`
- `paper_trading_lifecycle_storage_verified`
- `paper_trading_readiness_evidence_metrics_status`
- `paper_trading_readiness_evidence_metrics`
- `paper_trading_readiness_blockers`

The runtime probe is diagnostic only. It proves the local simulator path can
create and fill representative paper orders, but it is not enough by itself to
mark `paper_trading` ready in the live-readiness manifest.

`paper_trading_readiness_evidence_metrics` mirrors the manifest proof fields:

- `paper_runtime_probe_passed`
- `lifecycle_storage_verified`
- `diagnostic_only`
- `closed_trades`
- `open_positions`
- `net_pnl`
- `avg_net_pnl`

Generated review metrics always include `diagnostic_only=true`. The
live-readiness guard rejects diagnostic evidence, so operators must publish a
separate concrete paper-trading evidence artifact before the manifest can mark
`paper_trading` ready.

Minimum proof before the live-readiness manifest can mark `paper_trading` ready:

- deterministic simulator probe passes
- lifecycle storage is available
- paper order open, close, cancellation, take-profit, and stop-loss flows are
  persisted and queryable
- fees, cost basis, realized PnL, and open-position state are recorded
- the evidence artifact is written and referenced by
  `NEURATRADE_LIVE_READINESS_MANIFEST`
