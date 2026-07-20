#!/usr/bin/env bash
# Baseline backtest matrix for bitget-futures scalping readiness (bd clever-cabin-3sf).
# Records "before" numbers used as the Phase 1 (Effect v4) regression reference
# and the Phase 2 tuning starting point.
#
# Usage: cd services/neuratrade-cli-ts && bash scripts/baseline-matrix.sh [outdir]
set -uo pipefail

OUTDIR="${1:-$HOME/.neuratrade/baseline-$(date +%Y%m%d-%H%M%S)}"
mkdir -p "$OUTDIR"
echo "Writing baseline results to $OUTDIR"

SYMBOLS=("BTC/USDT:USDT" "ETH/USDT:USDT")
TIMEFRAMES=("5m" "15m")

run_case() {
  local name="$1"; shift
  local symbol="$1"; shift
  local timeframe="$1"; shift
  echo "=== ${name} ${symbol} ${timeframe} ==="
  bun run index.ts scalp backtest \
    --exchange bitget-futures \
    --symbol "$symbol" \
    --timeframe "$timeframe" \
    --capital 10000 \
    --futures \
    --leverage 1 \
    --fee 0.06 \
    --slippage-bps 2 \
    --oos-pct 20 \
    --mc-iterations 200 \
    "$@" 2>&1 | tee "$OUTDIR/${name}-${symbol//\//_}-${timeframe}.log" | tail -30
}

for symbol in "${SYMBOLS[@]}"; do
  for timeframe in "${TIMEFRAMES[@]}"; do
    # A) engine defaults (current out-of-the-box behavior)
    run_case "defaults" "$symbol" "$timeframe"
    # NOTE: --profile legs intentionally removed: profile symbol keys are
    # spot-style ("BTC/USDT") and silently don't match futures symbols
    # ("BTC/USDT:USDT") — see bd clever-cabin-3px. Run profiles only after
    # that bug is fixed.
  done
done

echo "=== MATRIX DONE: $OUTDIR ==="
