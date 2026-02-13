#!/usr/bin/env bash

# Coverage check helper
# - Runs Go tests with coverage across selected packages
# - Computes total coverage and optionally enforces a minimum threshold
# - Emits artifacts to ci-artifacts/coverage for CI visibility
# - Supports baseline capture and delta enforcement

set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")"/.. && pwd)"
ARTIFACTS_DIR="$ROOT_DIR/ci-artifacts/coverage"
BASELINE_FILE="$ARTIFACTS_DIR/coverage_baseline.txt"
mkdir -p "$ARTIFACTS_DIR"
cd "$ROOT_DIR"

# Configurable via env
MIN_COVERAGE="${MIN_COVERAGE:-50}"
MAX_DELTA="${MAX_DELTA:-5}"
COVERAGE_PACKAGES=(
  "./internal/api"
  "./internal/api/handlers"
  "./internal/cache"
  "./internal/ccxt"
  "./internal/config"
  "./internal/database"
  "./internal/logging"
  "./internal/metrics"
  "./internal/middleware"
  "./internal/models"
  "./internal/services"
  "./internal/telemetry"
  "./internal/testutil"
  "./internal/utils"
)

# Allow overrides via env var (comma-separated)
if [[ -n "${COVERAGE_PACKAGES_OVERRIDE:-}" ]]; then
  IFS=',' read -r -a COVERAGE_PACKAGES <<<"$COVERAGE_PACKAGES_OVERRIDE"
fi

STRICT="${STRICT:-true}"
CAPTURE_BASELINE="${CAPTURE_BASELINE:-false}"

COVERPROFILE="$ARTIFACTS_DIR/coverage.out"
COVERHTML="$ARTIFACTS_DIR/coverage.html"
SUMMARY="$ARTIFACTS_DIR/summary.txt"

echo "[coverage] Running tests with coverage for selected packages..." \
  | tee "$ARTIFACTS_DIR/coverage.log"

# Build package list and run tests
set +e
go test -v -coverprofile="$COVERPROFILE" "${COVERAGE_PACKAGES[@]}" | tee -a "$ARTIFACTS_DIR/coverage.log"
TEST_RC=$?
set -e

if [[ $TEST_RC -ne 0 ]]; then
  echo "[coverage] Test execution returned non-zero code ($TEST_RC). Continuing to compute coverage from available data (if any)." | tee -a "$ARTIFACTS_DIR/coverage.log"
fi

# Compute total coverage
TOTAL_LINE=$(go tool cover -func="$COVERPROFILE" 2>/dev/null | tail -n 1 || true)
TOTAL_PCT=""
if [[ -n "$TOTAL_LINE" ]]; then
  # Expect format: total: (statements)	XX.X%
  TOTAL_PCT=$(echo "$TOTAL_LINE" | awk '{print $NF}' | tr -d '%')
fi

if [[ -z "$TOTAL_PCT" ]]; then
  echo "[coverage] Unable to compute total coverage. See logs." | tee -a "$ARTIFACTS_DIR/coverage.log"
  echo "total_coverage=0" >"$SUMMARY"
  [[ "$STRICT" == "true" ]] && exit 1 || exit 0
fi

# Emit HTML report
go tool cover -html="$COVERPROFILE" -o "$COVERHTML" 2>/dev/null || true

printf "[coverage] Total coverage: %s%%\n" "$TOTAL_PCT" | tee -a "$ARTIFACTS_DIR/coverage.log"
printf "total_coverage=%s\nthreshold=%s\n" "$TOTAL_PCT" "$MIN_COVERAGE" >"$SUMMARY"

# Handle baseline capture and delta enforcement
if [[ "$CAPTURE_BASELINE" == "true" ]]; then
  echo "$TOTAL_PCT" >"$BASELINE_FILE"
  printf "[coverage] Baseline captured: %s%%\n" "$TOTAL_PCT" | tee -a "$ARTIFACTS_DIR/coverage.log"
fi

if [[ -f "$BASELINE_FILE" ]]; then
  BASELINE_PCT=$(cat "$BASELINE_FILE")
  DELTA=$(awk -v c="$TOTAL_PCT" -v b="$BASELINE_PCT" 'BEGIN {printf "%.1f", c - b}')

  printf "[coverage] Baseline: %s%%, Current: %s%%, Delta: %s%%\n" "$BASELINE_PCT" "$TOTAL_PCT" "$DELTA" | tee -a "$ARTIFACTS_DIR/coverage.log"

  # Check if coverage has dropped more than the allowed delta.
  # A positive delta (increase in coverage) is always accepted.
  IS_DROP_EXCEEDED=$(awk -v d="$DELTA" -v m="$MAX_DELTA" 'BEGIN { if (d < 0 && -d > m) print 1; else print 0 }')

  if [[ "$IS_DROP_EXCEEDED" -eq 1 ]]; then
    DROP_AMOUNT=$(echo "$DELTA" | awk '{printf "%.1f", -$1}')
    echo "[coverage] ERROR: Coverage dropped by ${DROP_AMOUNT}%, which exceeds the maximum allowed delta of ${MAX_DELTA}%" | tee -a "$ARTIFACTS_DIR/coverage.log"
    if [[ "$STRICT" == "true" ]]; then
      echo "[coverage] Failing due to coverage drop threshold breach in STRICT mode" | tee -a "$ARTIFACTS_DIR/coverage.log"
      exit 1
    fi
  fi
fi

# Generate per-file breakdown for quick triage
go tool cover -func="$COVERPROFILE" >"$ARTIFACTS_DIR/func_breakdown.txt" 2>/dev/null || true

# Simple package summary (aggregate by path prefix before last slash)
awk '/\t[0-9]+\.[0-9]+%$/ { \
  # file path in $1, pct in last field
  n=split($1,a,":"); path=a[1]; pct=$NF; sub("%","",pct); \
  # get package dir component (strip filename)
  sub("/[^/]+$","",path); \
  agg[path]+=pct; cnt[path]++ \
} END { \
  for (k in agg) { \
    if (cnt[k]>0) printf "%s\t%.1f\n", k, agg[k]/cnt[k]; \
  } \
}' "$ARTIFACTS_DIR/func_breakdown.txt" | sort >"$ARTIFACTS_DIR/package_summary.tsv" || true

# Final gate
exceed=$(awk -v t="$MIN_COVERAGE" -v c="$TOTAL_PCT" 'BEGIN {print (c+0 >= t+0) ? 1 : 0}')
if [[ "$exceed" -eq 1 ]]; then
  echo "[coverage] OK: ${TOTAL_PCT}% >= ${MIN_COVERAGE}%" | tee -a "$ARTIFACTS_DIR/coverage.log"
  exit 0
else
  echo "[coverage] WARN: ${TOTAL_PCT}% < ${MIN_COVERAGE}%" | tee -a "$ARTIFACTS_DIR/coverage.log"
  if [[ "$STRICT" == "true" ]]; then
    echo "[coverage] Failing due to STRICT mode" | tee -a "$ARTIFACTS_DIR/coverage.log"
    exit 1
  fi
  exit 0
fi
