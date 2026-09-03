# NeuraTrade TimesFM-3 sidecar

This is a research-only, advisory forecasting sidecar. It is intentionally
isolated from order placement and risk decisions. The NeuraTrade CLI can use it
to produce a shadow forecast for evaluation, but no TimesFM output is permitted
to place, resize, or close a position.

The project is pinned to CPython 3.12 and managed with [uv](https://docs.astral.sh/uv/):

```bash
uv python install 3.12
uv sync --dev
uv run pytest
uv run ruff check .
```

The default TimesFM 3.0 checkpoint is governed by Google's separate
non-commercial, non-production license. Verify the license and obtain any
required approval before using a different checkpoint or deploying this
sidecar for commercial or production workloads.

The worker accepts one JSON object per line on stdin and returns one JSON object
per line on stdout. Use `--validate-only` to exercise the protocol without
importing PyTorch or loading model weights. The Bun command wraps this worker:

```bash
bun run index.ts scalp timesfm-forecast --dry-run
bun run index.ts scalp timesfm-forecast --exchange bybit-futures \
  --symbol BTC/USDT:USDT --timeframe 1m --context-bars 256 --horizon 12
```

Run a bounded, fee/slippage-aware walk-forward diagnostic from stored candles:

```bash
bun run scripts/timesfm-walkforward.ts \
  --exchange bybit-futures --symbol BTC/USDT:USDT --timeframe 5m \
  --context-bars 256 --horizon 12 --max-origins 96 --device cpu
```

Forecasts must be evaluated with walk-forward, fee/slippage-aware data and
compared with a no-model baseline before they can influence any strategy.
