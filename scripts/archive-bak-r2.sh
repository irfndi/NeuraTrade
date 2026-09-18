#!/usr/bin/env bash
# P1: off-box copy of the same-host .bak to R2, then delete. Fail-closed.
# Usage: ./scripts/archive-bak-r2.sh [--delete-after-verify]
# Default is upload + report only; deletion needs the explicit flag AND a
# verified R2-side listing (manual gate — never auto-delete 6.9GB).
set -euo pipefail
ENV=/root/.neuratrade-r2.env
[ -f "$ENV" ] || { echo "r2 env missing ($ENV), abort"; exit 1; }
set -a
# shellcheck disable=SC1090
. "$ENV"
set +a
BAK=/root/.neuratrade/data/neuratrade.db.bak-20260905
[ -f "$BAK" ] || { echo "bak already gone, nothing to do"; exit 0; }
LOCAL_SUM=$(sha256sum "$BAK" | cut -d" " -f1)
echo "local sha256: $LOCAL_SUM ($(du -h "$BAK" | cut -f1))"
TS=$(date -u +%Y%m%dT%H%M%SZ)
python3 /opt/neuratrade/services/neuratrade-cli-ts/scripts/r2-put.py backtest-data "neuratrade/db-backup/neuratrade.db.bak-20260905-$TS" "$BAK" --gzip --verify
echo "uploaded + HEAD-verified neuratrade/db-backup/neuratrade.db.bak-20260905-$TS"
echo "NEXT: re-run with --delete-after-verify (asks YES, then rm + df)"
if [ "${1:-}" = "--delete-after-verify" ]; then
  echo "manual gate: confirm the R2 object exists + size matches, then type YES"
  read -r CONFIRM
  [ "$CONFIRM" = "YES" ] || { echo "aborted (confirmation != YES)"; exit 1; }
  rm -v "$BAK"
  df -h / | tail -1
fi
