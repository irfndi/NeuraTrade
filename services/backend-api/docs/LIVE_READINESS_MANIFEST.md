# Live Readiness Manifest

`/mode live` is blocked by default unless the backend can read a live-readiness
manifest from `NEURATRADE_LIVE_READINESS_MANIFEST`.

The manifest is intentionally a final gate, not proof by itself. Each entry must
point to evidence produced by the relevant paper/live-market verifier for that
strategy.

```json
{
  "updated_at": "2026-05-21T00:00:00Z",
  "strategies": {
    "paper_trading": {
      "ready": true,
      "evidence": "/path/to/paper-trading-verification.json",
      "verified_at": "2026-05-21T00:00:00Z"
    },
    "scalping": {
      "ready": true,
      "evidence": "/path/to/scalping-live-trial-ready.json",
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

Default required strategies:

- `paper_trading`
- `scalping`
- `daily_trading`
- `swing_trading`
- `arbitrage`

Override the checked list with `NEURATRADE_LIVE_READINESS_STRATEGIES`, using a
comma-separated list. Disable the guard only for local diagnostics with
`NEURATRADE_REQUIRE_LIVE_READINESS_PROOF=false`.
