#!/bin/sh
#
# champion-soak-monitor.sh — stateful CLAIM/entry/disk watcher for the isolated
# champion paper/demo soak (P2 clever-cabin-8rs).
#
# Why a second monitor: demo-soak-monitor.sh watches the DEFAULT home
# (~/.neuratrade) grid_paper_trades table and requires ALL pm2 apps online.
# The champion soak is incompatible with both assumptions:
#   - isolated homes: ~/.neuratrade-champion-paper + ~/.neuratrade-champion-demo
#   - ladder fills land in ladder_paper_trades (grid_paper_trades stays empty)
#   - ladder closes come from ladder_paper_trades rows (24h window),
#     never ENTER
#   - intentionally stopped jobs (e.g. *-candidate, candle-sync between cron
#     ticks) must not page
#
# Every run (POSIX-sh, idempotent, macOS-safe) it checks and reports:
#   1. Champion pm2 apps online (neuratrade-champion-paper/demo required when
#      present; candle-sync stopped-between-ticks is OK; non-champion apps and
#      missing champion entries are WARN-only).
#   2. CLAIM state: autoresearch/results/claimed.json appeared or changed
#      (GOALS CLAIMED) — stateful, alerts once per new hash.
#   3. Per isolated home (paper + demo):
#        a. ladder_paper_trades fills in the last 24h + cumulative total.
#           The total is stateful vs STATE_DIR (first-fill detection: alert on
#           increase, re-baseline silently on first run).
#        b. 24h ladder close count from ladder_paper_trades (DB rows, not
#           ENTER) — stateful per home, alerts on increase.
#   4. Disk usage of the champion homes' filesystem(s) vs DISK_THRESHOLD_PCT
#      (default 85) — stateful breach transition, fails while breached.
#
# Alert destination (explicit, authorized): stderr + LOG_FILE only. There is
# no webhook/Telegram/pager path here by design — no secrets are touched or
# rotated by this monitor. Surface: launchd err log, `pm2 logs`, or cron mail.
#
# Exit codes:
#   0  healthy, no state changes
#   1  stateful alert (new CLAIM, new fills, new ladder closes)
#   2  health failure (required champion app offline, disk breach, bad usage)
#
# Env overrides: PAPER_HOME DEMO_HOME CLAIMED_FILE STATE_DIR LOG_DIR
#   PAPER_DB DEMO_DB PAPER_LOG DEMO_LOG DISK_THRESHOLD_PCT SKIP_PM2 SKIP_DISK
#   (SKIP_PM2/SKIP_DISK exist for CI/tests on hosts without pm2 or with
#   unrelated disk pressure; never set them in production wiring.)
#
# Wiring examples (pick ONE):
#
#   launchd (macOS, every 5 min) — see com.neuratrade.champion-monitor.plist:
#     cp com.neuratrade.champion-monitor.plist ~/Library/LaunchAgents/
#     launchctl load -w ~/Library/LaunchAgents/com.neuratrade.champion-monitor.plist
#
#   cron (any unix, every 5 min):
#     */5 * * * * /bin/sh /Users/irfandi/Coding/2025/NeuraTrade/services/neuratrade-cli-ts/scripts/champion-soak-monitor.sh
#
#   pm2 (kept in the process list, cron_restart every 5 min):
#     pm2 start /Users/irfandi/Coding/2025/NeuraTrade/services/neuratrade-cli-ts/scripts/champion-soak-monitor.sh \
#       --name neuratrade-champion-monitor --cron "*/5 * * * *" --no-autorestart
#
#   systemd (Linux timer, every 5 min) — /etc/systemd/system/champion-monitor.service:
#     [Service] Type=oneshot ExecStart=/bin/sh /opt/NeuraTrade/services/neuratrade-cli-ts/scripts/champion-soak-monitor.sh
#     ... plus a champion-monitor.timer with OnCalendar=*:0/5
#
set -eu

# launchd/cron run with a minimal PATH; resolve the toolchain explicitly.
export PATH="$HOME/.bun/bin:/opt/homebrew/bin:/usr/local/bin:/usr/bin:/bin:$PATH"

SCRIPT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
PAPER_HOME="${PAPER_HOME:-$HOME/.neuratrade-champion-paper}"
DEMO_HOME="${DEMO_HOME:-$HOME/.neuratrade-champion-demo}"
CLAIMED_FILE="${CLAIMED_FILE:-$SCRIPT_DIR/../autoresearch/results/claimed.json}"
STATE_DIR="${STATE_DIR:-$HOME/.neuratrade/state/champion-soak-monitor}"
LOG_DIR="${LOG_DIR:-$HOME/.neuratrade/logs}"
LOG_FILE="${LOG_DIR}/champion-soak-monitor.log"
PAPER_DB="${PAPER_DB:-$PAPER_HOME/data/neuratrade.db}"
DEMO_DB="${DEMO_DB:-$DEMO_HOME/data/neuratrade.db}"
PAPER_LOG="${PAPER_LOG:-$PAPER_HOME/logs/champion-paper.out.log}"
DEMO_LOG="${DEMO_LOG:-$DEMO_HOME/logs/champion-demo.out.log}"
DISK_THRESHOLD_PCT="${DISK_THRESHOLD_PCT:-85}"
SKIP_PM2="${SKIP_PM2:-0}"
SKIP_DISK="${SKIP_DISK:-0}"

mkdir -p "$LOG_DIR" "$STATE_DIR"

FAILURES=0
ALERTS=0

now() { date '+%Y-%m-%d %H:%M:%S'; }

log() { printf '[%s] %s\n' "$(now)" "$1" | tee -a "$LOG_FILE"; }

alert() {
  printf '[%s] ALERT: %s\n' "$(now)" "$1" >&2
  printf '[%s] ALERT: %s\n' "$(now)" "$1" >> "$LOG_FILE"
  ALERTS=$((ALERTS + 1))
}

warn() {
  printf '[%s] WARN: %s\n' "$(now)" "$1" >&2
  printf '[%s] WARN: %s\n' "$(now)" "$1" >> "$LOG_FILE"
}

fail() {
  alert "$1"
  FAILURES=$((FAILURES + 1))
}

# --- 1. Champion pm2 apps (WARN-only for non-champion / absent soak) ---------
if [ "$SKIP_PM2" = "1" ]; then
  log "pm2 check skipped (SKIP_PM2=1)"
elif command -v pm2 >/dev/null 2>&1; then
  JLIST=$(pm2 jlist 2>/dev/null || true)
  if command -v python3 >/dev/null 2>&1; then
    # shellcheck disable=SC2016
    CHAMPION_STATUS=$(printf '%s\n' "$JLIST" | python3 -c 'import json,sys
try:
    ps = json.load(sys.stdin)
    for p in ps:
        n = p.get("name", "")
        if n.startswith("neuratrade-champion-"):
            print(n + ":" + str(p.get("pm2_env", {}).get("status", "?")))
except Exception:
    pass' 2>/dev/null || true)
  else
    CHAMPION_STATUS=""
  fi
  if [ -z "$CHAMPION_STATUS" ]; then
    warn "no neuratrade-champion-* apps in pm2 (soak not running on this host?)"
  else
    log "pm2 champion apps:$(printf ' %s' $CHAMPION_STATUS)"
    for entry in $CHAMPION_STATUS; do
      name=${entry%%:*}
      status=${entry##*:}
      case "$name" in
        neuratrade-champion-paper | neuratrade-champion-demo)
          if [ "$status" != "online" ]; then
            fail "pm2 $name is '$status' (expected online)"
          fi
          ;;
        *)
          # Cron-restart helpers (candle-sync) sit stopped between ticks.
          if [ "$status" != "online" ] && [ "$status" != "stopped" ]; then
            warn "pm2 $name is '$status'"
          fi
          ;;
      esac
    done
  fi
else
  warn "pm2 not found on PATH (skipping process check)"
fi

# --- 2. CLAIM state (autoresearch claimed.json) -------------------------------
if [ -f "$CLAIMED_FILE" ]; then
  if grep -q "CLAIMED" "$CLAIMED_FILE" 2>/dev/null; then
    HASH=$(md5 -q "$CLAIMED_FILE" 2>/dev/null || md5sum "$CLAIMED_FILE" 2>/dev/null | awk '{print $1}' || cksum "$CLAIMED_FILE" | awk '{print $1}')
    SEEN_FILE="$STATE_DIR/claimed-hash.txt"
    PREV=""
    [ -f "$SEEN_FILE" ] && PREV=$(cat "$SEEN_FILE" 2>/dev/null | tr -d '[:space:]' || true)
    if [ "$HASH" != "$PREV" ]; then
      printf '%s\n' "$HASH" > "$SEEN_FILE"
      alert "GOALS CLAIMED: $CLAIMED_FILE is new/changed (hash $HASH)"
    else
      log "claim: already seen (hash $HASH)"
    fi
  else
    log "claim: $CLAIMED_FILE present but not CLAIMED yet"
  fi
else
  log "claim: no claimed.json yet"
fi

# --- 3. Per-home ladder fills + 24h closes ---------------------------------
check_home() {
  home_name=$1
  db_path=$2
  out_log=$3
  if [ ! -f "$db_path" ]; then
    warn "$home_name: DB missing at $db_path (home not initialized?)"
    return
  fi
  F24=$(sqlite3 "$db_path" "SELECT COUNT(*) FROM ladder_paper_trades WHERE closed_at >= datetime('now','-1 day');" 2>/dev/null || echo unknown)
  # Monotonic total. The 24h window AGES CLOSES OUT, so a correctly working
  # engine that closes a rung every few hours reads 0 most of the time and a
  # "sustained > 0" gate becomes structurally unsatisfiable. The total only
  # ever grows, which is what "sustained" actually means.
  FTOT=$(sqlite3 "$db_path" "SELECT COUNT(*) FROM ladder_paper_trades;" 2>/dev/null || echo unknown)
  case "$F24" in
    unknown) warn "$home_name: ladder 24h fill query failed (no ladder_paper_trades?)" ;;
    *) log "$home_name: ladder fills (24h): $F24" ;;
  esac
  TOTAL=$(sqlite3 "$db_path" "SELECT COUNT(*) FROM ladder_paper_trades;" 2>/dev/null || echo unknown)
  case "$TOTAL" in
    unknown) warn "$home_name: ladder fill total query failed" ;;
    *)
      TOTAL_FILE="$STATE_DIR/ladder-total-$home_name.txt"
      if [ -f "$TOTAL_FILE" ]; then
        PREV=$(cat "$TOTAL_FILE" 2>/dev/null | tr -d '[:space:]' || true)
        case "$PREV" in
          *[!0-9]* | '') PREV="" ;;
        esac
      else
        PREV=""
      fi
      if [ -z "$PREV" ]; then
        printf '%s\n' "$TOTAL" > "$TOTAL_FILE"
        log "$home_name: ladder fills baseline: $TOTAL"
      elif [ "$TOTAL" -gt "$PREV" ]; then
        printf '%s\n' "$TOTAL" > "$TOTAL_FILE"
        alert "$home_name: NEW LADDER FILLS: total $TOTAL (was $PREV)"
      elif [ "$TOTAL" -lt "$PREV" ]; then
        printf '%s\n' "$TOTAL" > "$TOTAL_FILE"
        warn "$home_name: ladder total DECREASED $PREV -> $TOTAL (DB reset?); baseline reset"
      else
        log "$home_name: ladder fills unchanged: $TOTAL"
      fi
      ;;
  esac
  if [ ! -f "$out_log" ]; then
    warn "$home_name: log missing at $out_log"
    return
  fi
  # Gate 3's real signal is CLOSED round-trips: one row per trade in
  # ladder_paper_trades. Counting log lines was the original bug (the engine
  # never printed "OPENED", so the old grep read 0 forever); counting
  # "open=N" lines instead would count ONE open position once per interval
  # it survives, which is not a trade count either. DB rows cannot
  # double-count: a rung that opens and holds is not a fill.
  #
  # A failed query yields the literal "unknown" — that must NOT be coerced
  # to 0, because a broken DB would then read as "zero closes", the same
  # silent-zero failure the log counter had.
  case "$F24" in
    unknown)
      warn "$home_name: 24h close count unavailable (query failed); not reporting a gate number"
      return
      ;;
  esac
  # Gate on the 24h WINDOW. The lifetime total cannot be the criterion: the
  # demo's total is 28 and all of it predates the poll fix, so a total-based
  # gate passes on history it was created to supersede. The window reads 0
  # both while healthy-with-open-positions and while dead, so it is also not
  # sufficient alone — the pair (window, total) is the honest statement and
  # the total is logged as context below.
  case "$F24" in
    unknown)
      warn "$home_name: 24h close count unavailable (query failed); not reporting a gate number"
      return
      ;;
  esac
  CLOSES=$F24
  case "$CLOSES" in
    '' | *[!0-9]*)
      warn "$home_name: 24h close count not numeric ($CLOSES); not reporting a gate number"
      return
      ;;
  esac
  CLOSES_FILE="$STATE_DIR/closed-24h-$home_name.txt"
  if [ -f "$CLOSES_FILE" ]; then
    PREV_C=$(cat "$CLOSES_FILE" 2>/dev/null | tr -d '[:space:]' || true)
    case "$PREV_C" in
      *[!0-9]* | '') PREV_C="" ;;
    esac
  else
    PREV_C=""
  fi
  if [ -z "$PREV_C" ]; then
    printf '%s\n' "$CLOSES" > "$CLOSES_FILE"
    log "$home_name: 24h ladder closes baseline: $CLOSES (lifetime total: $FTOT)"
  elif [ "$CLOSES" -gt "$PREV_C" ]; then
    printf '%s\n' "$CLOSES" > "$CLOSES_FILE"
    alert "$home_name: NEW LADDER CLOSES: 24h count $CLOSES (was $PREV_C; lifetime total $FTOT)"
  elif [ "$CLOSES" -lt "$PREV_C" ]; then
    printf '%s\n' "$CLOSES" > "$CLOSES_FILE"
    warn "$home_name: 24h close count DECREASED $PREV_C -> $CLOSES (window aged out, or DB reset); baseline reset"
  else
    log "$home_name: 24h ladder closes unchanged: $CLOSES (lifetime total: $FTOT)"
  fi
}

check_home "paper" "$PAPER_DB" "$PAPER_LOG"
check_home "demo" "$DEMO_DB" "$DEMO_LOG"

# --- 4. Disk pressure on the champion filesystems -----------------------------
if [ "$SKIP_DISK" = "1" ]; then
  log "disk check skipped (SKIP_DISK=1)"
else
  case "$DISK_THRESHOLD_PCT" in
    '' | *[!0-9]*) fail "DISK_THRESHOLD_PCT is not numeric: $DISK_THRESHOLD_PCT" ;;
    *)
      BREACH_FILE="$STATE_DIR/disk-breached.txt"
      BREACHED=0
      for target in "$PAPER_HOME" "$DEMO_HOME"; do
        [ -d "$target" ] || continue
        USE=$(df -P "$target" 2>/dev/null | awk 'NR==2 {print $5}' | tr -d '% ' || true)
        case "$USE" in
          '' | *[!0-9]*) warn "disk: df parse failed for $target" && continue ;;
        esac
        log "disk: $target at ${USE}% (threshold ${DISK_THRESHOLD_PCT}%)"
        if [ "$USE" -ge "$DISK_THRESHOLD_PCT" ]; then
          BREACHED=1
          fail "disk breach: $target at ${USE}% >= ${DISK_THRESHOLD_PCT}%"
        fi
      done
      if [ -f "$BREACH_FILE" ]; then
        PREV_B=$(cat "$BREACH_FILE" 2>/dev/null | tr -d '[:space:]' || echo 0)
      else
        PREV_B=0
      fi
      printf '%s\n' "$BREACHED" > "$BREACH_FILE"
      if [ "$BREACHED" = "1" ] && [ "$PREV_B" != "1" ]; then
        alert "disk: newly breached threshold (state transition)"
      elif [ "$BREACHED" = "0" ] && [ "$PREV_B" = "1" ]; then
        log "disk: breach cleared"
      fi
      ;;
  esac
fi

log "summary: failures=$FAILURES alerts=$ALERTS"

if [ "$FAILURES" -gt 0 ]; then
  exit 2
fi
if [ "$ALERTS" -gt 0 ]; then
  exit 1
fi
exit 0
