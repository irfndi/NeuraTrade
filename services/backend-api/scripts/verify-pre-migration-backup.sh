#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")"/.. && pwd)"
REPO_ROOT="$(git -C "$ROOT_DIR" rev-parse --show-toplevel)"

TAG_REF=""
BRANCH_REF=""
PATCH_FILE=""

while [[ $# -gt 0 ]]; do
  case "$1" in
    --tag)
      TAG_REF="${2:-}"
      shift 2
      ;;
    --branch)
      BRANCH_REF="${2:-}"
      shift 2
      ;;
    --patch)
      PATCH_FILE="${2:-}"
      shift 2
      ;;
    *)
      printf "Unknown option: %s\n" "$1"
      exit 1
      ;;
  esac
done

if [[ -z "$TAG_REF" && -z "$BRANCH_REF" && -z "$PATCH_FILE" ]]; then
  printf "Usage: %s [--tag <tag>] [--branch <branch>] [--patch <path>]\n" "$0"
  exit 1
fi

ok=true

if [[ -n "$TAG_REF" ]]; then
  if git -C "$REPO_ROOT" rev-parse --verify "refs/tags/$TAG_REF" >/dev/null 2>&1; then
    printf "[ok] tag exists: %s\n" "$TAG_REF"
  else
    printf "[missing] tag not found: %s\n" "$TAG_REF"
    ok=false
  fi
fi

if [[ -n "$BRANCH_REF" ]]; then
  if git -C "$REPO_ROOT" rev-parse --verify "$BRANCH_REF" >/dev/null 2>&1; then
    printf "[ok] branch exists: %s\n" "$BRANCH_REF"
  else
    printf "[missing] branch not found: %s\n" "$BRANCH_REF"
    ok=false
  fi
fi

if [[ -n "$PATCH_FILE" ]]; then
  if [[ -f "$PATCH_FILE" ]]; then
    printf "[ok] patch file exists: %s\n" "$PATCH_FILE"
  else
    printf "[missing] patch file not found: %s\n" "$PATCH_FILE"
    ok=false
  fi
fi

if [[ "$ok" != true ]]; then
  exit 1
fi

printf "Backup verification completed successfully.\n"
