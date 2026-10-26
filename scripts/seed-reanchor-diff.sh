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

# Pre-flight BEFORE any cleanup: a failed rerun must not wipe the previous
# run's ledgers (they are the only evidence of what it measured).
[ -x "$NT" ] || { echo "nt-cli not found/executable: $NT" >&2; exit 1; }
for S in $SYMS; do
  [ -s "$PANELS/$S-15m.csv" ] || { echo "missing panel $PANELS/$S-15m.csv" >&2; exit 1; }
done
mkdir -p "$OUT"
# Refuse to wipe a $OUT we don't own (predictable /tmp default path).
[ -O "$OUT" ] || { echo "$OUT is not owned by $(id -u): refusing to clean it" >&2; exit 1; }
read -ra SYM_ARR <<<"$SYMS"
NSYM=${#SYM_ARR[@]}
[ "$NSYM" -gt 0 ] || { echo "no symbols in SYMS ('$SYMS')" >&2; exit 1; }
# One clean sweep dir: stale per-arm summaries/ledgers left by an earlier run
# (different ARMS or SYMS) would otherwise be summed into this run's totals.
rm -f "$OUT"/*.summary "$OUT"/*.csv "$OUT"/*-resume.txt
: >"$OUT/summary.tsv"

# A missing key used to yield the whole line, which then blew up (or worse,
# silently accumulated) inside $(( )). Fail on the first malformed summary.
field() {
  local v
  v=$(sed -nE "s/.* $2=([0-9]+).*/\1/p" "$1" | tail -1)
  [ -n "$v" ] || { echo "missing '$2=' in $1" >&2; exit 1; }
  printf '%s' "$v"
}

for bps in $ARMS; do
  for S in "${SYM_ARR[@]}"; do
    "$NT" ladder-shadow --bars "$PANELS/$S-15m.csv" --capital-usdt "$CAP" \
      --resume "$OUT/$S-$bps-resume.txt" --ledger "$OUT/$S-$bps.csv" \
      --seed-reanchor-bps "$bps" >"$OUT/$S-$bps.summary"
  done
  cap=0 closes=0 target=0 stop=0 maxhold=0 fills=0
  for S in "${SYM_ARR[@]}"; do
    f="$OUT/$S-$bps.summary"
    # Each read is guarded: `$(( ... $(field) ...))` would swallow a
    # command-substitution exit, turn the operand into an empty string and
    # carry on with a bogus total.
    v=$(field "$f" capital) || exit 1
    cap=$((cap + v))
    v=$(field "$f" closes) || exit 1
    closes=$((closes + v))
    v=$(field "$f" fills) || exit 1
    fills=$((fills + v))
    v=$(field "$f" target) || exit 1
    target=$((target + v))
    v=$(field "$f" stop) || exit 1
    stop=$((stop + v))
    v=$(field "$f" maxhold) || exit 1
    maxhold=$((maxhold + v))
  done
  printf '%s\t%s\t%s\t%s\t%s\t%s\t%s\n' \
    "$bps" "$cap" "$closes" "$target" "$stop" "$maxhold" "$fills" >>"$OUT/summary.tsv"
done

NSYM=${#SYM_ARR[@]}
echo "== seed-reanchor sweep | panels $PANELS | $CAP USDT x $NSYM symbols =="
awk -F'\t' '
  { bps[NR] = $1; cap[NR] = $2; closes[NR] = $3; t[NR] = $4; s[NR] = $5; m[NR] = $6; f[NR] = $7
    if ($1 == 0) { base = $2; have_base = 1 } }
  END {
    if (!have_base) {
      print "no bps=0 arm in " FILENAME ": delta column would be nonsense" > "/dev/stderr"
      exit 1
    }
    printf "%-6s %12s %12s %10s %8s %8s %8s %8s\n", "bps", "capital", "delta", "closes", "target", "stop", "maxhold", "fills"
    for (i = 1; i <= NR; i++)
      printf "%-6s %12.4f %+12.4f %10d %8d %8d %8d %8d\n",
        bps[i], cap[i] / 1e6, (cap[i] - base) / 1e6, closes[i], t[i], s[i], m[i], f[i]
  }
' "$OUT/summary.tsv"
