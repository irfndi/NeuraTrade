# SCRIPTS KNOWLEDGE BASE

## OVERVIEW
`scripts/` contains native operational tooling for startup orchestration, health checks, environment validation, webhook control, migration helpers, and diagnostics.

## WHERE TO LOOK
| Task | Location | Notes |
|------|----------|-------|
| Sequential service startup | `startup-orchestrator.sh` | Native gateway lifecycle wrapper |
| Health monitoring/recovery | `health-monitor-enhanced.sh` | Continuous checks and restart actions |
| Environment validation | `validate-env.sh`, `verify-env-sync.sh` | Required var checks and drift detection |
| Telegram diagnostics | `diagnose-telegram-bot.sh`, `set-telegram-webhook.sh`, `webhook-control.sh` | Bot/webhook triage and control |
| Test orchestration | `test.sh`, `coverage-check.sh` | Local/CI test workflows and thresholds |

## CONVENTIONS
- Scripts use strict shell mode (`set -euo pipefail`) for safety.
- Scripts are native-first and do not require container runtime dependencies.
- Prefer explicit preflight checks before mutating state.
- Use timestamped logs for long-running tasks.

## GOTCHAS
- Some scripts expect `NEURATRADE_HOME` and service PID/log files under `~/.neuratrade`.
- Runtime service management assumes gateway-managed processes.
- Migration helper scripts differ between host and CI contexts.

## SAFE USAGE PATTERN
```bash
bash scripts/validate-env.sh
bash scripts/verify-env-sync.sh
bash scripts/startup-orchestrator.sh start
bash scripts/health-monitor-enhanced.sh check
```

## BACKLOG (bd CLI)

**Stats:** 312 total | 64 open | 1 in progress | 14 blocked | 247 closed | 50 ready

## ANTI-PATTERNS
- Running operational scripts without validating env inputs.
- Hardcoding secrets in script invocations/history.
- Editing scripts to bypass health gates instead of fixing underlying readiness.
