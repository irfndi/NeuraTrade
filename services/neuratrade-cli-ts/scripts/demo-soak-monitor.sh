#!/bin/sh
#
# demo-soak-monitor.sh — ops health + first-fill alert monitor for the
# Bitget PAPTRADING demo soak (pm2 apps from ecosystem.demo-soak.config.cjs).
#
# Every run (POSIX-sh, idempotent, macOS-safe) it checks and reports:
#   1. ALL pm2 apps are online (not just neuratrade-demo-soak).
#   2. Risk kill switch state (risk_kill_switch.engaged in neuratrade.db).
#   3. The funnel's last gate-eligible count (parsed from
#      ~/.neuratrade/logs/universe-watch.out.log).
#   4. Fill count in the last 24h (grid_paper_trades.closed_at).
#   5. Cumulative fill total vs ~/.neuratrade/state/fills-since.txt — when
#      the total INCREASES between runs it alerts on stderr and exits 1
#      (first-fill detection for a launchd schedule), then writes the new
#      total back.
#
# Exit codes:
#   0  healthy, no new fills
#   1  fill total increased since last run (NEW FILLS — the alert trigger)
#   2  one or more health checks failed (stderr explains which)
#
# Env overrides: NEURATRADE_HOME DB_PATH UNIVERSE_LOG STATE_DIR MIN_FILLS_24H
# (MIN_FILLS_24H default 0 = report-only; set to 1+ to fail on a quiet soak —
#   the funnel currently selects 0 symbols, so zero 24h fills is expected).
#
# launchd (every 5 min): see com.neuratrade.monitor.plist in this directory.
#   launchctl load -w ~/Library/LaunchAgents/com.neuratrade.monitor.plist
# Alert visibility: ~/.neuratrade/logs/neuratrade-monitor-launchd.err.log

set -eu

# launchd runs with a minimal PATH; resolve the toolchain explicitly.
export PATH="$HOME/.bun/bin:/opt/homebrew/bin:/usr/local/bin:/usr/bin:/bin:$PATH"

NEURATRADE_HOME="${NEURATRADE_HOME:-$HOME/.neuratrade}"
LOG_DIR="${NEURATRADE_HOME}/logs"
LOG_FILE="${LOG_DIR}/demo-soak-monitor.log"
DB_PATH="${DB_PATH:-$NEURATRADE_HOME/data/neuratrade.db}"
UNIVERSE_LOG="${UNIVERSE_LOG:-$LOG_DIR/universe-watch.out.log}"
STATE_DIR="${STATE_DIR:-$NEURATRADE_HOME/state}"
STATE_FILE="${STATE_DIR}/fills-since.txt"
MIN_FILLS_24H="${MIN_FILLS_24H:-0}"

mkdir -p "$LOG_DIR" "$STATE_DIR"

FAILURES=0
NEWFILLS=0

now() { date '+%Y-%m-%d %H:%M:%S'; }

log() { printf '[%s] %s\n' "$(now)" "$1" | tee -a "$LOG_FILE"; }

alert() {
  printf '[%s] ALERT: %s\n' "$(now)" "$1" >&2
  printf '[%s] ALERT: %s\n' "$(now)" "$1" >> "$LOG_FILE"
}

warn() {
  printf '[%s] WARN: %s\n' "$(now)" "$1" >&2
  printf '[%s] WARN: %s\n' "$(now)" "$1" >> "$LOG_FILE"
}

fail() {
  alert "$1"
  FAILURES=$((FAILURES + 1))
}

# 1. pm2 — every app online, not just demo-soak.
TOTAL=0
ONLINE=0
OFFLINE=""
if command -v pm2 >/dev/null 2>&1; then
  JLIST=$(pm2 jlist 2>/dev/null || true)
  TOTAL=$(printf '%s\n' "$JLIST" | grep -o '"pm2_env":{' | wc -l | tr -d ' ')
  ONLINE=$(printf '%s\n' "$JLIST" | grep -o '"status":"online"' | wc -l | tr -d ' ')
  if command -v python3 >/dev/null 2>&1; then
    OFFLINE=$(printf '%s\n' "$JLIST" | python3 -c 'import json,sys
try:
    ps = json.load(sys.stdin)
    print(" ".join(p.get("name", "?") for p in ps if p.get("pm2_env", {}).get("status") != "online"))
except Exception:
    pass' 2>/dev/null || true)
  fi
  if [ "$TOTAL" -eq 0 ]; then
    fail "pm2 daemon unreachable (jlist empty)"
  elif [ "$ONLINE" -lt "$TOTAL" ]; then
    fail "pm2 apps offline: ${OFFLINE:-unknown} ($ONLINE/$TOTAL online)"
  else
    log "pm2: all $TOTAL apps online"
  fi
else
  fail "pm2 not found on PATH"
fi

# 2. Risk kill switch.
KS=$(sqlite3 "$DB_PATH" "SELECT engaged FROM risk_kill_switch WHERE id=1;" 2>/dev/null || echo unknown)
case "$KS" in
  1) fail "RISK KILL SWITCH ENGAGED";;
  unknown) fail "kill switch state unknown (query failed)";;
  0) log "kill switch: disengaged";;
  *) fail "kill switch state unexpected: $KS";;
esac

# 3. Funnel gate-eligible count — last matching line in the universe-watch log.
ELIGIBLE=$(tail -n 500 "$UNIVERSE_LOG" 2>/dev/null | sed -n 's/.*survivors.*\([0-9][0-9]*\) gate-eligible.*/\1/p' | tail -1 || true)
if [ -z "$ELIGIBLE" ]; then
  fail "no 'gate-eligible' line in last 500 lines of $UNIVERSE_LOG"
else
  log "funnel gate-eligible: $ELIGIBLE"
fi

# 4. Fills in the last 24h.
F24=$(sqlite3 "$DB_PATH" "SELECT COUNT(*) FROM grid_paper_trades WHERE closed_at >= datetime('now','-1 day');" 2>/dev/null || echo unknown)
case "$F24" in
  unknown) fail "24h fill count query failed";;
  *)
    if [ "$F24" -eq 0 ] && [ "$MIN_FILLS_24H" -gt 0 ]; then
      fail "0 fills in last 24h (MIN_FILLS_24H=$MIN_FILLS_24H)"
    elif [ "$F24" -eq 0 ]; then
      warn "no fills in last 24h (report-only; set MIN_FILLS_24H=1 to fail)"
    else
      log "fills (24h): $F24"
    fi
    ;;
esac

# 5. First-fill detection: cumulative total vs fills-since.txt.
TOTAL_FILLS=$(sqlite3 "$DB_PATH" "SELECT COUNT(*) FROM grid_paper_trades;" 2>/dev/null || echo unknown)
case "$TOTAL_FILLS" in
  unknown) fail "fill total query failed";;
  *)
    if [ -f "$STATE_FILE" ]; then
      PREV=$(cat "$STATE_FILE" 2>/dev/null | tr -d '[:space:]' || true)
      case "$PREV" in
        *[!0-9]*|'') PREV="";;
      esac
    else
      PREV=""
    fi
    if [ -z "$PREV" ]; then
      printf '%s\n' "$TOTAL_FILLS" > "$STATE_FILE"
      log "fills-since baseline: $TOTAL_FILLS"
    elif [ "$TOTAL_FILLS" -gt "$PREV" ]; then
      NEWFILLS=1
      printf '%s\n' "$TOTAL_FILLS" > "$STATE_FILE"
      alert "NEW FILLS detected: total $TOTAL_FILLS (was $PREV) — first-fill alert"
    elif [ "$TOTAL_FILLS" -lt "$PREV" ]; then
      printf '%s\n' "$TOTAL_FILLS" > "$STATE_FILE"
      warn "fill total DECREASED $PREV -> $TOTAL_FILLS (DB reset?); baseline reset"
    else
      log "fills unchanged: $TOTAL_FILLS"
    fi
    ;;
esac

log "summary: apps=${ONLINE}/${TOTAL} kill_switch=${KS} eligible=${ELIGIBLE:-?} fills_24h=${F24:-?} fills_total=${TOTAL_FILLS:-?}"

if [ "$FAILURES" -gt 0 ]; then
  exit 2
fi
if [ "$NEWFILLS" -eq 1 ]; then
  exit 1
fi
exit 0
