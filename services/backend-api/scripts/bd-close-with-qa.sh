#!/usr/bin/env bash

set -euo pipefail

if [[ $# -lt 1 ]]; then
  printf "Usage: %s <issue-id> --unit \"...\" --integration \"...\" --e2e \"...\" --coverage \"...\" --evidence \"...\"\n" "$0" >&2
  exit 1
fi

ISSUE_ID="$1"
shift

UNIT=""
INTEGRATION=""
E2E=""
COVERAGE=""
EVIDENCE=""

while [[ $# -gt 0 ]]; do
  case "$1" in
    --unit)
      UNIT="${2:-}"
      shift 2
      ;;
    --integration)
      INTEGRATION="${2:-}"
      shift 2
      ;;
    --e2e)
      E2E="${2:-}"
      shift 2
      ;;
    --coverage)
      COVERAGE="${2:-}"
      shift 2
      ;;
    --evidence)
      EVIDENCE="${2:-}"
      shift 2
      ;;
    *)
      printf "Unknown argument: %s\n" "$1" >&2
      exit 1
      ;;
  esac
done

if [[ -z "$UNIT" || -z "$INTEGRATION" || -z "$E2E" || -z "$COVERAGE" || -z "$EVIDENCE" ]]; then
  printf "All QA fields are required: --unit, --integration, --e2e, --coverage, --evidence\n" >&2
  exit 1
fi

if [[ "$E2E" == "N/A" || "$E2E" == "na" || "$E2E" == "n/a" ]]; then
  printf "E2E cannot be bare N/A. Provide rationale in --e2e.\n" >&2
  exit 1
fi

if ! bd show "$ISSUE_ID" >/dev/null 2>&1; then
  printf "Issue not found: %s\n" "$ISSUE_ID" >&2
  exit 1
fi

STAMP="$(date -u +"%Y-%m-%dT%H:%M:%SZ")"
NOTE=$'QA_GATE\n'
NOTE+=$'timestamp: '
NOTE+="$STAMP"
NOTE+=$'\nunit: '
NOTE+="$UNIT"
NOTE+=$'\nintegration: '
NOTE+="$INTEGRATION"
NOTE+=$'\ne2e_or_smoke: '
NOTE+="$E2E"
NOTE+=$'\ncoverage: '
NOTE+="$COVERAGE"
NOTE+=$'\nevidence: '
NOTE+="$EVIDENCE"

bd update "$ISSUE_ID" --notes "$NOTE" >/dev/null
bd close "$ISSUE_ID" --reason "QA gate passed: test evidence recorded" >/dev/null

if ! bd show "$ISSUE_ID" --json | grep -q '"status": "closed"'; then
	printf "Failed to close %s. Check blockers/dependencies and close manually after resolving them.\n" "$ISSUE_ID" >&2
	exit 1
fi

printf "Closed %s with QA gate evidence.\n" "$ISSUE_ID"
