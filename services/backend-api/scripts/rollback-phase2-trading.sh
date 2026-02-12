#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")"/.. && pwd)"

TARGET_A="$ROOT_DIR/internal/api/handlers/trading.go"
TARGET_B="$ROOT_DIR/internal/api/handlers/trading_test.go"
TARGET_C="$ROOT_DIR/internal/api/routes.go"

if [[ "${1:-}" != "--execute" ]]; then
  printf "Dry run only. Re-run with --execute to apply rollback.\n"
  printf "Rollback targets:\n"
  printf -- "- %s\n" "$TARGET_A"
  printf -- "- %s\n" "$TARGET_B"
  printf -- "- %s\n" "$TARGET_C"
  exit 0
fi

rm -f "$TARGET_A" "$TARGET_B"
git -C "$ROOT_DIR" restore --source=HEAD --worktree -- "$TARGET_C"

printf "Rollback applied for phase-2 trading changes.\n"
