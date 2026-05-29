# Live Readiness Manifest

`/mode live` is blocked by default unless the backend can read a live-readiness
manifest from `NEURATRADE_LIVE_READINESS_MANIFEST`.

The manifest is intentionally a final gate, not proof by itself. Each entry must
point to evidence produced by the relevant paper/live-market verifier for that
strategy. Required entries also require structured `evidence_metrics`; a
non-empty evidence path alone is not enough to permit live mode.

```json
{
  "updated_at": "2026-05-21T00:00:00Z",
  "strategies": {
    "paper_trading": {
      "ready": true,
      "evidence": "/path/to/paper-trading-verification.json",
      "evidence_metrics": {
        "paper_runtime_probe_passed": true,
        "lifecycle_storage_verified": true,
        "continuous_validation_hours": 168,
        "strategy_count": 2,
        "risk_limits_enforced": true,
        "backtest_comparison_verified": true,
        "closed_trades": 1,
        "open_positions": 0,
        "net_pnl": "1.25",
        "avg_net_pnl": "1.25"
      },
      "verified_at": "2026-05-21T00:00:00Z"
    },
    "scalping": {
      "ready": true,
      "evidence": "/path/to/scalping-live-trial-ready.json",
      "evidence_metrics": {
        "closed_trades": 20,
        "winning_trades": 9,
        "losing_trades": 11,
        "open_positions": 0,
        "net_pnl": "1.25",
        "avg_net_pnl": "0.0625",
        "max_drawdown_pct": "0.42",
        "drawdown_verified": true,
        "execution_path_verified": true,
        "market_data_verified": true,
        "risk_limits_enforced": true,
        "backtest_comparison_verified": true
      },
      "verified_at": "2026-05-21T00:00:00Z"
    },
    "daily_trading": {
      "ready": false,
      "reason": "paper/live-market proof missing"
    },
    "swing_trading": {
      "ready": false,
      "reason": "paper/live-market proof missing"
    },
    "arbitrage": {
      "ready": false,
      "reason": "paper/live-market proof missing"
    }
  }
}
```

Required paper-trading metrics:

- `paper_runtime_probe_passed`: must be true.
- `lifecycle_storage_verified`: must be true.
- `diagnostic_only`: must be absent or false.
- `continuous_validation_hours`: at least 168 hours of continuous paper
  validation.
- `strategy_count`: at least 2 strategy types covered by the paper evidence.
- `risk_limits_enforced`: must be true.
- `backtest_comparison_verified`: must be true.
- `closed_trades`: at least 1 persisted closed paper trade.
- `open_positions`: must be 0.
- `net_pnl` and `avg_net_pnl`: decimal strings greater than 0.

Required trading-strategy metrics:

- `closed_trades`: at least 20 for `scalping`; at least 2 for `daily_trading`, `swing_trading`, and `arbitrage` unless arbitrage uses documented no-trade safety.
- `winning_trades` and `losing_trades`: both must be positive.
- `open_positions`: must be 0.
- `net_pnl`, `avg_net_pnl`, and `max_drawdown_pct`: decimal strings greater than 0.
- `drawdown_verified`: must be true.
- `execution_path_verified`: must be true.
- `market_data_verified`: must be true.
- `risk_limits_enforced`: must be true.
- `backtest_comparison_verified`: must be true.
- `hold_window_verified`: must be true for `swing_trading`.
- `cost_accounting_verified`: must be true for `arbitrage`.
- `exposure_safety_verified`: must be true for `arbitrage`.
- `diagnostic_only`: must be absent or false.

Arbitrage may use no-trade safety evidence instead of closed-trade metrics only
when an observed window proves no executable spreads/opportunities after costs:

```json
{
  "ready": true,
  "evidence": "/path/to/arbitrage-no-trade-safety.json",
  "evidence_metrics": {
    "no_trade_safety": true,
    "no_trade_reason": "no executable spreads after fees across observed window",
    "market_data_verified": true,
    "cost_accounting_verified": true,
    "exposure_safety_verified": true,
    "open_positions": 0
  }
}
```

Default required strategies:

- `paper_trading`
- `scalping`
- `daily_trading`
- `swing_trading`
- `arbitrage`

Override the checked list with `NEURATRADE_LIVE_READINESS_STRATEGIES`, using a
comma-separated list. Disable the guard only for local diagnostics with
`NEURATRADE_REQUIRE_LIVE_READINESS_PROOF=false`.
