#!/usr/bin/env bash

set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")"/.. && pwd)"

echo "[structure-check] validating canonical handler paths"

if [[ -d "$ROOT_DIR/internal/handlers" ]]; then
  echo "[structure-check] ERROR: legacy directory exists: internal/handlers"
  exit 1
fi

LEGACY_SEGMENT='internal/handlers'
PATTERN="github.com/irfandi/celebrum-ai-go/${LEGACY_SEGMENT}|\\./${LEGACY_SEGMENT}|/${LEGACY_SEGMENT}/"

if rg -n "$PATTERN" "$ROOT_DIR" \
  --glob '*.go' \
  --glob '*.sh' \
  --glob '*.yml' \
  --glob '*.yaml' \
  --glob '!**/scripts/check-legacy-paths.sh' \
  --glob '!**/scripts/check-canonical-naming.sh' \
  --glob '!**/.golangci.yml' \
  --glob '!docs/**' \
  --glob '!**/*.md'; then
  echo "[structure-check] ERROR: found legacy handler path references"
  exit 1
fi

echo "[structure-check] OK: no legacy handler paths found"
