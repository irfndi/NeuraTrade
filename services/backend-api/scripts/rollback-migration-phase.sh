#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")"/.. && pwd)"
REPO_ROOT="$(git -C "$ROOT_DIR" rev-parse --show-toplevel)"

if [[ $# -lt 1 ]]; then
  printf "Usage: %s <phase1|phase2|phase3|phase4> [--execute] [--commit <sha>]\n" "$0"
  exit 1
fi

PHASE="$1"
shift

EXECUTE=false
COMMIT_SHA=""

while [[ $# -gt 0 ]]; do
  case "$1" in
    --execute)
      EXECUTE=true
      shift
      ;;
    --commit)
      COMMIT_SHA="${2:-}"
      shift 2
      ;;
    *)
      printf "Unknown option: %s\n" "$1"
      exit 1
      ;;
  esac
done

if [[ -n "$COMMIT_SHA" ]]; then
  if [[ "$EXECUTE" != true ]]; then
    printf "Dry run: git -C %s revert --no-edit %s\n" "$REPO_ROOT" "$COMMIT_SHA"
    exit 0
  fi
  git -C "$REPO_ROOT" revert --no-edit "$COMMIT_SHA"
  printf "Commit revert completed: %s\n" "$COMMIT_SHA"
  exit 0
fi

FILES=()
case "$PHASE" in
  phase1 | 1)
    FILES=(
      "services/backend-api/internal/api/handlers/futures_arbitrage.go"
      "services/backend-api/internal/api/handlers/futures_arbitrage_test.go"
      "services/backend-api/internal/api/routes.go"
      "services/backend-api/scripts/coverage-check.sh"
      "services/backend-api/scripts/check-legacy-paths.sh"
      "services/backend-api/docs/migration/REPOSITORY_STRUCTURE_CONTRACT.md"
      "Makefile"
    )
    ;;
  phase2 | 2)
    FILES=(
      "services/backend-api/internal/api/handlers/trading.go"
      "services/backend-api/internal/api/handlers/trading_test.go"
      "services/backend-api/internal/api/routes.go"
      "services/backend-api/docs/openapi/trading-phase2.yaml"
      "services/backend-api/scripts/rollback-phase2-trading.sh"
    )
    ;;
  phase3 | 3)
    FILES=(
      "services/backend-api/cmd/server/main.go"
      "services/backend-api/internal/api/routes.go"
    )
    ;;
  phase4 | 4)
    FILES=(
      "services/backend-api/docs/migration/PHASE4_VALIDATION_CHECKLIST.md"
      "services/backend-api/docs/migration/PHASE5_VALIDATION_REPORT.md"
    )
    ;;
  *)
    printf "Invalid phase: %s\n" "$PHASE"
    exit 1
    ;;
esac

printf "Rollback targets for %s:\n" "$PHASE"
for file in "${FILES[@]}"; do
  printf -- "- %s\n" "$file"
done

if [[ "$EXECUTE" != true ]]; then
  printf "Dry run only. Re-run with --execute to apply rollback.\n"
  exit 0
fi

for file in "${FILES[@]}"; do
  abs="$REPO_ROOT/$file"
  if git -C "$REPO_ROOT" ls-files --error-unmatch "$file" >/dev/null 2>&1; then
    git -C "$REPO_ROOT" restore --source=HEAD --worktree -- "$file"
  elif [[ -e "$abs" ]]; then
    rm -rf "$abs"
  fi
done

printf "Rollback applied for %s\n" "$PHASE"
