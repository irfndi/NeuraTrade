#!/usr/bin/env bash
# Weekly ledger archive: full copy -> R2 (gz), then prune local to
# KEEP/SEED/CLAIMED decisions + last 30k rows. Never prunes without
# a successful off-site upload. No-ops until /root/.neuratrade-r2.env
# exists (0600, R2_ACCOUNT_ID + R2_ACCESS_KEY_ID + R2_SECRET_ACCESS_KEY
# scoped to backtest-data read/write in Cloudflare dashboard).
set -euo pipefail
ENV=/root/.neuratrade-r2.env
[ -f "$ENV" ] || {
  echo "r2 env missing, skip"
  exit 0
}
set -a
# shellcheck disable=SC1090
. "$ENV"
set +a
CLI=/opt/neuratrade/services/neuratrade-cli-ts
LEDGER=$CLI/autoresearch/results/ledger.jsonl
[ -f "$LEDGER" ] || exit 0
# ponytail: workers append constantly; stop briefly so cp+replace loses nothing.
export PATH=$PATH:/usr/local/bin:/root/.bun/bin
pm2 stop neuratrade-autoresearch-w0 neuratrade-autoresearch-w1 neuratrade-autoresearch-w2 neuratrade-autoresearch-w3
restart() { pm2 restart neuratrade-autoresearch-w0 neuratrade-autoresearch-w1 neuratrade-autoresearch-w2 neuratrade-autoresearch-w3; }
trap restart EXIT
TS=$(date -u +%Y%m%dT%H%M%SZ)
TMP=/tmp/ledger-$TS.jsonl
cp "$LEDGER" "$TMP"
python3 "$CLI/scripts/r2-put.py" backtest-data "neuratrade/ledger/ledger-$TS.jsonl" "$TMP" --gzip
{
  grep -E '"decision":"(KEEP|SEED|CLAIMED)"' "$TMP" || true
  tail -n 30000 "$TMP"
} | awk '!seen[$0]++' >"$LEDGER.new"
mv "$LEDGER.new" "$LEDGER"
rm -f "$TMP"
echo "archived neuratrade/ledger/ledger-$TS.jsonl, pruned local ledger"
