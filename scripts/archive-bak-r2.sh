#!/usr/bin/env bash
# P1: off-box copy of the same-host .bak to R2, then delete. Fail-closed.
# Usage: ./scripts/archive-bak-r2.sh [--delete-after-verify]
# Chunked gzip (1GB parts) so a 6.9GB single-PUT timeout can't strand the
# upload: each part PUTs + HEAD-verifies independently, and a re-run skips
# parts R2 already holds (resume). Deletion needs the explicit flag AND a
# manifest showing every part VERIFY OK (never auto-delete 6.9GB).
set -euo pipefail
ENV=/root/.neuratrade-r2.env
[ -f "$ENV" ] || { echo "r2 env missing ($ENV), abort"; exit 1; }
set -a
# shellcheck disable=SC1090
. "$ENV"
set +a
BAK=/root/.neuratrade/data/neuratrade.db.bak-20260905
[ -f "$BAK" ] || { echo "bak already gone, nothing to do"; exit 0; }
PUT=/opt/neuratrade/services/neuratrade-cli-ts/scripts/r2-put.py
TS=$(date -u +%Y%m%dT%H%M%SZ)
WORK=/tmp/bak-archive-$TS
mkdir -p "$WORK"
echo "local sha256: $(sha256sum "$BAK" | cut -d" " -f1) ($(du -h "$BAK" | cut -f1))"
# 1GB gzip parts, numbered for resume.
split -b 1G --numeric-suffixes=1 --suffix-length=3 <(gzip -c "$BAK") "$WORK/part-"
MANIFEST="$WORK/MANIFEST"
: > "$MANIFEST"
for part in "$WORK"/part-*; do
  n=$(basename "$part")
  key="neuratrade/db-backup/neuratrade.db.bak-20260905-$TS/$n.gz"
  if grep -q "^$n VERIFY OK" "$MANIFEST" 2>/dev/null; then
    echo "skip $n (already verified)"
    continue
  fi
  python3 "$PUT" backtest-data "$key" "$part" --verify
  echo "$n VERIFY OK $key" >> "$MANIFEST"
done
echo "all parts verified; manifest: $MANIFEST"
cat "$MANIFEST"
echo "NEXT: re-run with --delete-after-verify (checks manifest, asks YES, then rm + df)"
if [ "${1:-}" = "--delete-after-verify" ]; then
  expected=$(find "$WORK" -maxdepth 1 -name 'part-*' | wc -l)
  verified=$(grep -c "VERIFY OK" "$MANIFEST")
  [ "$verified" = "$expected" ] || { echo "manifest incomplete ($verified/$expected), abort"; exit 1; }
  echo "manifest complete ($verified/$expected). Type YES to delete $BAK"
  read -r CONFIRM
  [ "$CONFIRM" = "YES" ] || { echo "aborted (confirmation != YES)"; exit 1; }
  rm -v "$BAK"
  df -h / | tail -1
fi
