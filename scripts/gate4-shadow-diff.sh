#!/usr/bin/env bash
# Gate 4 ladder-half driver (forward).
#
# Walks TS vs Rust over the SAME bars, daily-cadence safe to re-run:
#   1. append bars newer than the local CSV cursor (box read-only over ssh)
#   2. refresh the TS closes window (overwrite; idempotent, no cursor drift)
#   3. nt-cli ladder-shadow over the new bars — cursor + ladder v3 state live
#      entirely in --resume, so nothing else can drift
#   4. print per-symbol reason counts + ending capital for both sides
#
# Diff rule (plan): every line attributed — rounding 1e-6, LADDER-STEP-ANCHOR,
# fee split, cross 15bp, taker rate, window origin (TS state predates 09-18),
# and the TS live target-gate starvation (2-candle window: entryBar ==
# barIndex forever -> target exits unreachable; Rust finding targets is the
# fix inheriting with the cutover, NOT divergence to chase).
#
# Box writes: NONE (only reads). Everything lands in $GATE (default /tmp/gate4).
set -euo pipefail

HOST=${HOST:-root@217.216.35.77}
DB=${DB:-/root/.neuratrade-champion-paper/data/neuratrade.db}
GATE=${GATE:-/tmp/gate4}
NT=${NT:-./crates/target/release/nt-cli}
SINCE=${SINCE:-2026-09-18} # cohort attribution window, both sides
SYMS="BTC ETH SOL LINK"

mkdir -p "$GATE"

# --- 1. panel append (local cursor = last ts_ms) -----------------------------
for S in $SYMS; do
  CSV="$GATE/$S-15m.csv"
  if [ ! -s "$CSV" ]; then
    echo "missing seed panel $CSV (seed from box ohlcv_data first)" >&2
    exit 1
  fi
  LAST_MS=$(tail -1 "$CSV" | cut -d, -f1)
  ssh "$HOST" "sqlite3 -csv '$DB' <<SQL
SELECT strftime('%s',timestamp)*1000,
 CAST(ROUND(open_price*1000000) AS INTEGER),
 CAST(ROUND(high_price*1000000) AS INTEGER),
 CAST(ROUND(low_price*1000000) AS INTEGER),
 CAST(ROUND(close_price*1000000) AS INTEGER)
FROM ohlcv_data o
JOIN exchanges e ON e.id=o.exchange_id
JOIN trading_pairs tp ON tp.id=o.trading_pair_id
WHERE e.name='bybit-futures' AND tp.symbol='$S/USDT:USDT'
  AND o.timeframe='15m'
  AND strftime('%s',o.timestamp)*1000 > $LAST_MS
ORDER BY o.timestamp;
SQL" >>"$CSV"
done

# --- 2. TS closes window (overwrite -> idempotent) ---------------------------
ssh "$HOST" "sqlite3 -csv '$DB' \"SELECT symbol, side, rung_index, exit_reason, entry_price, exit_price, capital_before, capital_after, pnl, opened_at, closed_at FROM ladder_paper_trades WHERE closed_at >= '$SINCE' ORDER BY closed_at;\"" \
  >"$GATE/ts-closes.csv"

# --- 3. shadow walk ----------------------------------------------------------
for S in $SYMS; do
  "$NT" ladder-shadow \
    --bars "$GATE/$S-15m.csv" \
    --resume "$GATE/$S-resume.txt" \
    --ledger "$GATE/$S-rust.csv"
done

# --- 4. diff -----------------------------------------------------------------
echo "== Gate 4 ladder diff | window >= $SINCE | TS (DB) vs Rust (ledger) =="
printf '%-5s | %-46s | %s\n' "SYM" "TS closes by reason / cap_after" "Rust closes by reason / cap_after"
for S in $SYMS; do
  TSYM="$S/USDT:USDT"
  TS_R=$(awk -F, -v s="$TSYM" '$1==s {print $4}' "$GATE/ts-closes.csv" | sort | uniq -c | xargs echo || true)
  RS_R=$(awk -F, 'NR>1 {print $4}' "$GATE/$S-rust.csv" | sort | uniq -c | xargs echo || true)
  T_CAP=$(awk -F, -v s="$TSYM" '$1==s {c=$8} END{print (c==""?"-":c)}' "$GATE/ts-closes.csv")
  R_CAP=$(awk -F, 'NR>1 {c=$9} END{if (c=="") print "-"; else printf "%.6f", c/1000000}' "$GATE/$S-rust.csv")
  printf '%-5s | %-22s cap=%-21s | %-22s cap=%s\n' \
    "$S" "${TS_R:-0}" "$T_CAP" "${RS_R:-0}" "$R_CAP"
done
