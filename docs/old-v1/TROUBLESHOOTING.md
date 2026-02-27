# Troubleshooting (Native Runtime)

## Quick Triage

```bash
./bin/neuratrade gateway status
curl -sS http://localhost:8080/health | jq .
```

If health fails, check logs:

```bash
tail -n 200 ~/.neuratrade/logs/backend.log
tail -n 200 ~/.neuratrade/logs/ccxt.log
tail -n 200 ~/.neuratrade/logs/telegram.log
```

## Common Issues

### 1) Backend not reachable

Checks:

```bash
bash services/backend-api/scripts/startup-orchestrator.sh status
lsof -i :8080
```

Recovery:

```bash
bash services/backend-api/scripts/startup-orchestrator.sh restart
```

### 2) Telegram bot not responding

Checks:

```bash
tail -n 200 ~/.neuratrade/logs/telegram.log
bash services/backend-api/scripts/webhook-control.sh status
```

Recovery:

```bash
bash services/backend-api/scripts/webhook-control.sh disable
bash services/backend-api/scripts/webhook-control.sh enable
```

### 3) Exchange calls timing out

Checks:

```bash
tail -n 200 ~/.neuratrade/logs/ccxt.log
curl -sS http://localhost:3001/health || true
```

Recovery:

```bash
./bin/neuratrade gateway stop
./bin/neuratrade gateway start
```

### 4) SQLite lock/contention

Checks:

```bash
ls -la ~/.neuratrade/data
lsof ~/.neuratrade/data/neuratrade.db
```

Recovery:

```bash
./bin/neuratrade gateway stop
# ensure no stale backend process
pkill -f neuratrade-server || true
./bin/neuratrade gateway start
```

### 5) Test failures in CI/local

Use native test runner and SQLite defaults:

```bash
DATABASE_DRIVER=sqlite SQLITE_PATH=/tmp/neuratrade-ci.db make test
STRICT=true make coverage-check
```

## Monitoring Commands

```bash
bash services/backend-api/scripts/health-monitor-enhanced.sh check
bash services/backend-api/scripts/health-monitor-enhanced.sh monitor 300
```

## Incident Bundle

Collect diagnostics quickly:

```bash
mkdir -p /tmp/neuratrade-debug
cp -r ~/.neuratrade/logs /tmp/neuratrade-debug/
cp -r ~/.neuratrade/pids /tmp/neuratrade-debug/
curl -sS http://localhost:8080/health > /tmp/neuratrade-debug/health.json || true
```
