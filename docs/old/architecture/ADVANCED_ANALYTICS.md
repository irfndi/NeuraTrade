# Advanced Analytics and Forecasting (Status)

This project uses descriptive statistics and deterministic scoring for funding rate analytics and risk metrics.
Predictive ML/time-series forecasting is intentionally deferred while running on CPU-only VPS infrastructure.

## Implemented (CPU-only)

- Funding rate statistics: rolling mean, standard deviation, min/max, trend via linear regression.
- Volatility scoring for funding rates and price data.
- Exchange reliability tracking (uptime/latency-based risk score).
- Order-book based liquidity and slippage estimation.
- Backtesting engine with Sharpe/Sortino, max drawdown, profit factor.

## Deferred (Not Implemented)

- Funding rate prediction (ARIMA or similar time-series models).
- GARCH volatility modeling.
- Correlation matrix analysis across exchanges or symbols.
- Market regime detection.
- Multi-leg arbitrage (non-ML but advanced).
- Exchange-specific fee tables (partial; defaults still used).

## Infrastructure Notes

- ARIMA/GARCH and regime detection do not require GPUs, but they are CPU-bound.
- For large symbol sets, compute these features in batch jobs (cron/worker), not on request.
- Keep history windows capped (90 days) and persist derived metrics to avoid repeated heavy computation.

## Future Implementation Guidance

- Prefer CPU-friendly Go libraries (e.g., Gonum or lightweight statistical routines).
- Gate forecasting features behind config flags so they can be enabled per environment.
- Emit runtime metrics (duration, errors) to monitor model cost and stability.

