#!/bin/sh
#
# backup-db.sh — WAL-safe SQLite backup of ~/.neuratrade/data/neuratrade.db.
#
# Uses the sqlite3 ".backup" online backup API, which is safe to run against
# a LIVE WAL-mode database (no downtime, consistent snapshot). Keeps the
# KEEP newest backups and prunes the rest. Idempotent: re-running just
# produces another dated snapshot and re-prunes.
#
# Env overrides (defaults shown):
#   DB_PATH     ~/.neuratrade/data/neuratrade.db
#   BACKUP_DIR  ~/.neuratrade/data/backups
#   KEEP        7
#
# launchd registration (daily 03:17):
#   cp scripts/com.neuratrade.backup.plist ~/Library/LaunchAgents/
#   launchctl load -w ~/Library/LaunchAgents/com.neuratrade.backup.plist
# Uninstall:
#   launchctl unload -w ~/Library/LaunchAgents/com.neuratrade.backup.plist
#
# Exit codes: 0 = backup written, 1 = failure (message on stderr).

set -eu

NEURATRADE_HOME="${NEURATRADE_HOME:-$HOME/.neuratrade}"
DB_PATH="${DB_PATH:-$NEURATRADE_HOME/data/neuratrade.db}"
BACKUP_DIR="${BACKUP_DIR:-$NEURATRADE_HOME/data/backups}"
KEEP="${KEEP:-7}"

command -v sqlite3 >/dev/null 2>&1 || { echo "backup-db: sqlite3 not found" >&2; exit 1; }
[ -f "$DB_PATH" ] || { echo "backup-db: database not found: $DB_PATH" >&2; exit 1; }
mkdir -p "$BACKUP_DIR"

TS=$(date +%Y%m%d-%H%M%S)
DEST="$BACKUP_DIR/neuratrade.backup-$TS.db"
TMP="$DEST.tmp"

# .backup is the online backup API: consistent even while writers are active.
sqlite3 "$DB_PATH" ".backup '$TMP'"
mv -f "$TMP" "$DEST"
SIZE=$(wc -c < "$DEST" | tr -d ' ')
echo "backup-db: wrote $DEST ($SIZE bytes)"

# Retention: keep the KEEP newest by mtime, prune the rest.
OLD=$(ls -1t "$BACKUP_DIR"/neuratrade.backup-*.db 2>/dev/null || true)
COUNT=0
for backup in $OLD; do
  COUNT=$((COUNT + 1))
  if [ "$COUNT" -gt "$KEEP" ]; then
    rm -f "$backup"
    echo "backup-db: pruned $backup"
  fi
done
