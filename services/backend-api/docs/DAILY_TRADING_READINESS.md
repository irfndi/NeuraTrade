# Daily Trading Readiness

Daily trading is not real-money ready.

The backend registers a `daily_report` quest, but this is a reporting quest, not an executable daily trading strategy. It now writes explicit checkpoint fields so operators and tests can distinguish daily performance reporting from live-money trading readiness:

- `daily_trading_live_ready=false`
- `daily_trading_readiness_status=blocked`
- `daily_trading_readiness_blockers`

Current hard blockers:

- No dedicated `daily_trading` strategy implementation exists.
- No daily paper order lifecycle has been proven with representative closed wins and losses.
- No positive daily net/average PnL evidence exists after fees.
- No readiness evidence should mark daily trading ready until the above proof exists.

The `daily_report` quest may summarize the last 24 hours of realized lifecycle performance when a lifecycle store is configured. That report is observability only and must not be used as live-money authorization.
