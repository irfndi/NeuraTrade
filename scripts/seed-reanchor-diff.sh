#!/usr/bin/env bash
# Offline measurement of the stale-ladder re-anchor (Gate 4 finding (e)).
#
# Sweeps `nt-cli ladder-shadow --seed-reanchor-bps` over the same panels and
# prints capital + close counts per arm, with the delta against arm 0 (the
# shipped behaviour: no re-anchor). Every run starts from a fresh resume file,
# so all arms share one seed bar — the difference is the policy and nothing
# else. Per-arm ledgers are kept for drill-down.
#
#   scripts/seed-reanchor-diff.sh
#   ARMS="0 50" PANELS=/tmp/gate4 OUT=/tmp/reanchor scripts/seed-reanchor-diff.sh
#
# Panels are `ts_ms,open,high,low,close` micro-USDT CSVs (gate4-shadow-diff.sh
# seeds them). Read-only: writes only under $OUT.
set -euo pipefail

NT=${NT:-./crates/target/release/nt-cli}
PANELS=${PANELS:-/tmp/gate4}
OUT=${OUT:-/tmp/reanchor}
ARMS=${ARMS:-"0 10 25 50 100 200"}
SYMS=${SYMS:-"BTC ETH SOL LINK"}
CAP=${CAP:-50}

mkdir -p "$OUT"
: >"$OUT/summary.tsv"

field() { sed -E "s/.* $2=([0-9]+).*/\1/" "$1"; }

for bps in $ARMS; do
  for S in $SYMS; do
    rm -f "$OUT/$S-$bps-resume.txt" "$OUT/$S-$bps.csv"
    "$NT" ladder-shadow --bars "$PANELS/$S-15m.csv" --capital-usdt "$CAP" \
      --resume "$OUT/$S-$bps-resume.txt" --ledger "$OUT/$S-$bps.csv" \
      --seed-reanchor-bps "$bps" >"$OUT/$S-$bps.summary"
  done
  cap=0 closes=0 target=0 stop=0 maxhold=0 fills=0
  for S in $SYMS; do
    f="$OUT/$S-$bps.summary"
    cap=$((cap + $(field "$f" capital)))
    closes=$((closes + $(field "$f" closes)))
    fills=$((fills + $(field "$f" fills)))
    target=$((target + $(field "$f" target)))
    stop=$((stop + $(field "$f" stop)))
    maxhold=$((maxhold + $(field "$f" maxhold)))
  done
  printf '%s\t%s\t%s\t%s\t%s\t%s\t%s\n' \
    "$bps" "$cap" "$closes" "$target" "$stop" "$maxhold" "$fills" >>"$OUT/summary.tsv"
done

NSYM=$(printf '%s\n' $SYMS | wc -l | tr -d ' ')
echo "== seed-reanchor sweep | panels $PANELS | $CAP USDT x $NSYM symbols =="
awk -F'\t' '
  { bps[NR] = $1; cap[NR] = $2; closes[NR] = $3; t[NR] = $4; s[NR] = $5; m[NR] = $6; f[NR] = $7
    if ($1 == 0) base = $2 }
  END {
    printf "%-6s %12s %12s %10s %8s %8s %8s %8s\n", "bps", "capital", "delta", "closes", "target", "stop", "maxhold", "fills"
    for (i = 1; i <= NR; i++)
      printf "%-6s %12.4f %+12.4f %10d %8d %8d %8d %8d\n",
        bps[i], cap[i] / 1e6, (cap[i] - base) / 1e6, closes[i], t[i], s[i], m[i], f[i]
  }
' "$OUT/summary.tsv"
