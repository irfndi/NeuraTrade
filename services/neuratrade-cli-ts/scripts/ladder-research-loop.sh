#!/usr/bin/env bash
set -euo pipefail

script_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
cd "$script_dir/.."

bun_bin="${BUN_BIN:-}"
if [[ -z "$bun_bin" ]]; then
  bun_bin="$(command -v bun 2>/dev/null || true)"
fi
if [[ -z "$bun_bin" && -x /root/.bun/bin/bun ]]; then
  bun_bin="/root/.bun/bin/bun"
fi
if [[ -z "$bun_bin" ]]; then
  echo "ladder research: Bun executable not found" >&2
  exit 127
fi

exec "$bun_bin" run index.ts scalp grid-universe-scan \
  --exchange bybit-futures \
  --timeframe 15m \
  --data-source db-mainnet \
  --market \
  --min-candles 10000 \
  --train-window 3600 \
  --test-window 1200 \
  --account-capital 50 \
  --tier readiness \
  --engine ladder \
  --rungs 1,2,3 \
  --stop-ratio 1.5 \
  --max-hold-bars 48 \
  --fee 0.02 \
  --slippage-bps 2 \
  --min-fill-frequency-pct 5 \
  --output grid-whitelist-ladder.json \
  --watch \
  --interval 21600
