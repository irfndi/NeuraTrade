#!/usr/bin/env bash

# One-command acceptance wrapper for the final scalping soak. This runs runtime
# health preflight, captures timestamped soak evidence, verifies the artifact,
# and writes a compact manifest for the tracking issue.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
BACKEND_ROOT="$(dirname "$SCRIPT_DIR")"
REPO_ROOT="$(dirname "$(dirname "$BACKEND_ROOT")")"
NEURATRADE_HOME="${NEURATRADE_HOME:-$HOME/.neuratrade}"
DATA_DIR="${DATA_DIR:-${NEURATRADE_HOME}/data}"
LOG_DIR="${LOG_DIR:-${NEURATRADE_HOME}/logs}"
STAMP="${STAMP:-$(date +%Y%m%d%H%M%S)}"
SOAK_DB_PATH="${SOAK_DB_PATH:-${DATA_DIR}/scalping-soak-acceptance-${STAMP}.db}"
SOAK_OUTPUT_FILE="${SOAK_OUTPUT_FILE:-${DATA_DIR}/scalping-soak-acceptance-${STAMP}.json}"
ACCEPTANCE_MANIFEST_FILE="${ACCEPTANCE_MANIFEST_FILE:-${SOAK_OUTPUT_FILE%.json}.acceptance.json}"
LOG_FILE="${LOG_FILE:-${LOG_DIR}/scalping-soak-acceptance-${STAMP}.log}"

SCALPING_SOAK_SCRIPT="${SCALPING_SOAK_SCRIPT:-${SCRIPT_DIR}/scalping-soak.sh}"
SCALPING_SOAK_VERIFIER="${SCALPING_SOAK_VERIFIER:-${SCRIPT_DIR}/verify-scalping-soak-artifact.sh}"
GATEWAY_BIN="${GATEWAY_BIN:-${REPO_ROOT}/bin/neuratrade}"
BACKEND_URL="${BACKEND_URL:-http://127.0.0.1:8080}"
HEALTH_TIMEOUT_SECONDS="${HEALTH_TIMEOUT_SECONDS:-5}"
RUN_HEALTH_PREFLIGHT="${RUN_HEALTH_PREFLIGHT:-true}"
CHECK_GATEWAY_STATUS="${CHECK_GATEWAY_STATUS:-true}"

export BACKEND_URL
export RUN_HEALTH_PREFLIGHT
export CHECK_GATEWAY_STATUS
export SOAK_DB_PATH
export SOAK_OUTPUT_FILE
export CYCLES="${CYCLES:-60}"
export INTERVAL_MS="${INTERVAL_MS:-15000}"
export TIMEOUT_SECONDS="${TIMEOUT_SECONDS:-1800}"
export HOLD_PERIOD_SECONDS="${HOLD_PERIOD_SECONDS:-300}"
export CAPITAL="${CAPITAL:-48}"
export MIN_TRADES="${MIN_TRADES-20}"
export MIN_WIN_RATE="${MIN_WIN_RATE-0.123}"
export MIN_NET_PNL="${MIN_NET_PNL-0}"
export MIN_AVG_NET_PNL="${MIN_AVG_NET_PNL-0}"
export MIN_SIGNAL_QUALITY_COVERAGE="${MIN_SIGNAL_QUALITY_COVERAGE-1}"
export MAX_HOLD_RATIO="${MAX_HOLD_RATIO-0.745}"
export MAX_DRAWDOWN_PCT="${MAX_DRAWDOWN_PCT-0.01}"
export MAX_AI_PROVIDER_DEGRADED_CYCLES="${MAX_AI_PROVIDER_DEGRADED_CYCLES-0}"
export MAX_PERFECT_WIN_TRADES="${MAX_PERFECT_WIN_TRADES-20}"
export MIN_BASELINE_WIN_RATE_DELTA="${MIN_BASELINE_WIN_RATE_DELTA-0}"
export MIN_BASELINE_NET_PNL_DELTA="${MIN_BASELINE_NET_PNL_DELTA-0}"
export MIN_BASELINE_AVG_PNL_DELTA="${MIN_BASELINE_AVG_PNL_DELTA-0}"
export REQUIRE_LIVE_TRIAL_READY="${REQUIRE_LIVE_TRIAL_READY:-true}"

usage() {
  cat <<USAGE
Usage: $(basename "$0") [run|help]

Runs the final scalping soak acceptance workflow:
  1. Verify runtime health (unless RUN_HEALTH_PREFLIGHT=false).
  2. Run scalping-soak.sh with timestamped SOAK_OUTPUT_FILE and SOAK_DB_PATH.
  3. Verify the artifact and persisted SQLite evidence.
  4. Write an acceptance manifest next to the artifact.

Environment:
  DATA_DIR                  Evidence directory (default: ${DATA_DIR})
  SOAK_OUTPUT_FILE          Clean JSON artifact path (default: ${SOAK_OUTPUT_FILE})
  SOAK_DB_PATH              SQLite evidence DB path (default: ${SOAK_DB_PATH})
  ACCEPTANCE_MANIFEST_FILE  Acceptance manifest path (default: ${ACCEPTANCE_MANIFEST_FILE})
  RUN_HEALTH_PREFLIGHT      true/false health preflight (default: ${RUN_HEALTH_PREFLIGHT})
  CHECK_GATEWAY_STATUS      true/false gateway status preflight (default: ${CHECK_GATEWAY_STATUS})
  BACKEND_URL               Backend base URL for /health and /ready (default: ${BACKEND_URL})

Gate defaults match SCALPING_SOAK_ACCEPTANCE.md and can be overridden with the
same environment variables accepted by scalping-soak.sh and verify-scalping-soak-artifact.sh.
USAGE
}

log() {
  mkdir -p "$LOG_DIR"
  printf '[%s] [SCALPING-SOAK-ACCEPTANCE] %s\n' "$(date '+%Y-%m-%d %H:%M:%S')" "$*" | tee -a "$LOG_FILE"
}

fail() {
  log "[FAIL] $*"
  exit 1
}

require_file() {
  [ -f "$1" ] || fail "required file not found: $1"
}

validate_boolean() {
  local label="$1"
  local value="$2"
  case "$value" in
    true | false)
      ;;
    *)
      fail "${label} must be true or false, got ${value}"
      ;;
  esac
}

git_value() {
  local fallback="$1"
  shift
  git -C "$REPO_ROOT" "$@" 2>/dev/null || printf '%s\n' "$fallback"
}

run_health_preflight() {
  [ "$RUN_HEALTH_PREFLIGHT" = "true" ] || {
    log "health preflight skipped by RUN_HEALTH_PREFLIGHT=${RUN_HEALTH_PREFLIGHT}"
    return
  }

  if [ "$CHECK_GATEWAY_STATUS" = "true" ]; then
    [ -x "$GATEWAY_BIN" ] || fail "gateway binary not executable: ${GATEWAY_BIN}"
    log "checking gateway status with ${GATEWAY_BIN}"
    "$GATEWAY_BIN" gateway status | tee -a "$LOG_FILE"
  fi

  command -v curl >/dev/null 2>&1 || fail "curl is required for health preflight"
  log "checking backend health at ${BACKEND_URL}/health"
  curl -fsS --max-time "$HEALTH_TIMEOUT_SECONDS" "${BACKEND_URL}/health" >/dev/null \
    || fail "backend /health failed at ${BACKEND_URL}/health"
  log "checking backend readiness at ${BACKEND_URL}/ready"
  curl -fsS --max-time "$HEALTH_TIMEOUT_SECONDS" "${BACKEND_URL}/ready" >/dev/null \
    || fail "backend /ready failed at ${BACKEND_URL}/ready"
}

write_manifest() {
  command -v jq >/dev/null 2>&1 || fail "jq is required to write acceptance manifest"
  require_file "$SOAK_OUTPUT_FILE"

  local git_branch
  local git_commit
  local git_status
  git_branch="$(git_value unknown rev-parse --abbrev-ref HEAD)"
  git_commit="$(git_value unknown rev-parse HEAD)"
  git_status="$(git_value unknown status --short --branch)"

  mkdir -p "$(dirname "$ACCEPTANCE_MANIFEST_FILE")"
  jq -n \
    --arg created_at "$(date -u '+%Y-%m-%dT%H:%M:%SZ')" \
    --arg git_branch "$git_branch" \
    --arg git_commit "$git_commit" \
    --arg git_status "$git_status" \
    --arg backend_url "$BACKEND_URL" \
    --arg artifact "$SOAK_OUTPUT_FILE" \
    --arg db_path "$SOAK_DB_PATH" \
    --arg log_file "$LOG_FILE" \
    --argjson report "$(jq '.result.report' "$SOAK_OUTPUT_FILE")" \
    '{
      created_at: $created_at,
      git: {
        branch: $git_branch,
        commit: $git_commit,
        status: $git_status
      },
      runtime: {
        backend_url: $backend_url,
        health_preflight: env.RUN_HEALTH_PREFLIGHT,
        gateway_status_check: env.CHECK_GATEWAY_STATUS
      },
      evidence: {
        artifact: $artifact,
        db_path: $db_path,
        log_file: $log_file
      },
      gates: {
        min_trades: env.MIN_TRADES,
        hold_period_seconds: env.HOLD_PERIOD_SECONDS,
        min_win_rate: env.MIN_WIN_RATE,
        min_net_pnl: env.MIN_NET_PNL,
        min_avg_net_pnl: env.MIN_AVG_NET_PNL,
        min_signal_quality_coverage: env.MIN_SIGNAL_QUALITY_COVERAGE,
        max_hold_ratio: env.MAX_HOLD_RATIO,
        max_drawdown_pct: env.MAX_DRAWDOWN_PCT,
        max_ai_provider_degraded_cycles: env.MAX_AI_PROVIDER_DEGRADED_CYCLES,
        max_perfect_win_trades: env.MAX_PERFECT_WIN_TRADES,
        min_baseline_win_rate_delta: env.MIN_BASELINE_WIN_RATE_DELTA,
        min_baseline_net_pnl_delta: env.MIN_BASELINE_NET_PNL_DELTA,
        min_baseline_avg_pnl_delta: env.MIN_BASELINE_AVG_PNL_DELTA,
        require_live_trial_ready: env.REQUIRE_LIVE_TRIAL_READY
      },
      report: {
        total_cycles: $report.total_cycles,
        action_split: $report.action_split,
        regime_split: $report.regime_split,
        rejection_by_reason: $report.rejection_by_reason,
        gate_block_by_code: $report.gate_block_by_code,
        signal_quality: $report.signal_quality,
        trade_summary: $report.trade_summary,
        ai_provider_degradation: $report.ai_provider_degradation,
        baseline_comparison: $report.baseline_comparison,
        insufficient_trade_proof: $report.insufficient_trade_proof,
        live_trial_readiness: $report.live_trial_readiness
      }
    }' >"$ACCEPTANCE_MANIFEST_FILE"
  log "wrote acceptance manifest to ${ACCEPTANCE_MANIFEST_FILE}"
}

run_acceptance() {
  require_file "$SCALPING_SOAK_SCRIPT"
  require_file "$SCALPING_SOAK_VERIFIER"
  validate_boolean "RUN_HEALTH_PREFLIGHT" "$RUN_HEALTH_PREFLIGHT"
  validate_boolean "CHECK_GATEWAY_STATUS" "$CHECK_GATEWAY_STATUS"
  mkdir -p "$DATA_DIR" "$LOG_DIR"

  log "starting scalping soak acceptance branch=$(git_value unknown rev-parse --abbrev-ref HEAD) commit=$(git_value unknown rev-parse --short HEAD)"
  log "evidence artifact=${SOAK_OUTPUT_FILE} db=${SOAK_DB_PATH} manifest=${ACCEPTANCE_MANIFEST_FILE}"
  run_health_preflight

  log "running scalping soak"
  bash "$SCALPING_SOAK_SCRIPT" run | tee -a "$LOG_FILE"

  log "verifying scalping soak artifact"
  bash "$SCALPING_SOAK_VERIFIER" "$SOAK_OUTPUT_FILE" "$SOAK_DB_PATH" | tee -a "$LOG_FILE"
  write_manifest
  log "[OK] scalping soak acceptance complete"
}

main() {
  case "${1:-run}" in
    run)
      run_acceptance
      ;;
    help | -h | --help)
      usage
      ;;
    *)
      usage
      fail "unknown command: $1"
      ;;
  esac
}

main "$@"
