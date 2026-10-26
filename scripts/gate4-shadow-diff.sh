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
  case "$S" in
    '' | *[!A-Z0-9]*) echo "bad symbol '$S' (expected A-Z0-9 only)" >&2; exit 1 ;;
  esac
  LAST_MS=$(tail -1 "$CSV" | cut -d, -f1)
  # A corrupt tail (sqlite error text, short row) must never become a SQL
  # comparison that silently returns nothing.
  case "$LAST_MS" in
    '' | *[!0-9]*) echo "corrupt cursor in $CSV ('$LAST_MS'), refusing to append" >&2; exit 1 ;;
  esac
  SQL="$GATE/$S-append.sql"
  TMP="$GATE/$S-append.tmp"
  ERR="$GATE/$S-append.err"
  cat >"$SQL" <<SQL
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
SQL
  # SQL over stdin, remote stderr to its own file, rows to a temp file: the
  # panel only grows after the query succeeded AND the rows validate.
  if ! ssh "$HOST" sqlite3 -csv "$DB" <"$SQL" >"$TMP" 2>"$ERR"; then
    echo "append failed for $S (see $ERR); $CSV untouched" >&2
    rm -f "$TMP"
    exit 1
  fi
  # The appended panel must stay byte-shaped: 5 numeric columns per row.
  if [ -s "$TMP" ] && ! awk -F, 'NF != 5 || $1 !~ /^[0-9]+$/ || $2 !~ /^-?[0-9]+$/ ||
      $3 !~ /^-?[0-9]+$/ || $4 !~ /^-?[0-9]+$/ || $5 !~ /^-?[0-9]+$/ { exit 1 }' "$TMP"; then
    echo "append for $S returned malformed rows (kept in $TMP); $CSV untouched" >&2
    exit 1
  fi
  cat "$TMP" >>"$CSV"
  rm -f "$TMP"
done

# --- 2. TS closes window (overwrite -> idempotent) ---------------------------
ssh "$HOST" "sqlite3 -csv '$DB' \"SELECT symbol, side, rung_index, exit_reason, entry_price, exit_price, capital_before, capital_after, pnl, opened_at, closed_at FROM ladder_paper_trades WHERE closed_at >= '$SINCE' ORDER BY closed_at;\"" \
  >"$GATE/ts-closes.csv"
# The diff reads capital_after from column 8; assert the shape the SELECT
# promises so clock/column drift can't quietly print '-' instead of a number:
# 11 selected columns, and capital_after (8) numeric on every row.
# (sqlite3 -csv emits no header line, so shape is the only column map.)
if [ -s "$GATE/ts-closes.csv" ] && ! awk -F, 'NF != 11 || $8 !~ /^-?[0-9]+$/ { exit 1 }' "$GATE/ts-closes.csv"; then
  echo "ts-closes.csv does not have the 11 selected columns; diff would be wrong" >&2
  exit 1
fi

# --- 3. shadow walk ----------------------------------------------------------
for S in $SYMS; do
  "$NT" ladder-shadow \
    --bars "$GATE/$S-15m.csv" \
    --resume "$GATE/$S-resume.txt" \
    --ledger "$GATE/$S-rust.csv"
done

# --- 4. diff -----------------------------------------------------------------
echo "== Gate 4 ladder diff | window >= $SINCE | TS (DB) vs Rust (ledger) =="
# Producers must both have emitted output: silence here would print '-' and
# read as a clean diff. Empty-but-present CSVs are legitimate (0 closes).
for f in "$GATE/ts-closes.csv"; do
  [ -f "$f" ] || { echo "produced file missing: $f" >&2; exit 1; }
done
printf '%-5s | %-46s | %s\n' "SYM" "TS closes by reason / cap_after" "Rust closes by reason / cap_after"
for S in $SYMS; do
  TSYM="$S/USDT:USDT"
  [ -f "$GATE/$S-rust.csv" ] || { echo "rust ledger missing: $GATE/$S-rust.csv" >&2; exit 1; }
  # Column positions below are contractual, not incidental: column 9 is
  # capital_after_micros per append_ladder_ledger's header.
  if [ "$(head -1 "$GATE/$S-rust.csv")" != "closed_ts_ms,rung_index,side,reason,entry_micros,exit_micros,pnl_micros,qty_base_micros,capital_after_micros" ]; then
    echo "unexpected rust ledger header in $GATE/$S-rust.csv" >&2
    exit 1
  fi
  TS_R=$(awk -F, -v s="$TSYM" '$1==s {print $4}' "$GATE/ts-closes.csv" | sort | uniq -c | xargs echo || true)
  RS_R=$(awk -F, 'NR>1 {print $4}' "$GATE/$S-rust.csv" | sort | uniq -c | xargs echo || true)
  T_CAP=$(awk -F, -v s="$TSYM" '$1==s {c=$8} END{print (c==""?"-":c)}' "$GATE/ts-closes.csv")
  R_CAP=$(awk -F, 'NR>1 {c=$9} END{if (c=="") print "-"; else printf "%.6f", c/1000000}' "$GATE/$S-rust.csv")
  printf '%-5s | %-22s cap=%-21s | %-22s cap=%s\n' \
    "$S" "${TS_R:-0}" "$T_CAP" "${RS_R:-0}" "$R_CAP"
done
