#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")"/.. && pwd)"
REPO_ROOT="$(git -C "$ROOT_DIR" rev-parse --show-toplevel)"

echo "[naming-check] validating canonical file naming"

violations=0

while IFS= read -r file; do
  if [[ "$file" != services/backend-api/* ]]; then
    continue
  fi
  if [[ "$file" =~ \.pb\.go$ ]]; then
    continue
  fi
  base="$(basename "$file")"
  if [[ ! "$base" =~ ^[a-z0-9_]+\.go$ ]]; then
    echo "[naming-check] ERROR: non-snake_case Go filename: $file"
    violations=1
  fi
done < <(git -C "$REPO_ROOT" ls-files '*.go')

echo "[naming-check] validating banned legacy import paths"

if git -C "$REPO_ROOT" grep -n 'github.com/irfandi/celebrum-ai-go/internal/handlers' -- 'services/backend-api/**/*.go' >/dev/null 2>&1; then
  echo "[naming-check] ERROR: found banned legacy import path in Go sources"
  git -C "$REPO_ROOT" grep -n 'github.com/irfandi/celebrum-ai-go/internal/handlers' -- 'services/backend-api/**/*.go' || true
  violations=1
fi

if [[ "$violations" -ne 0 ]]; then
  exit 1
fi

echo "[naming-check] OK: canonical naming and import guardrails passed"
