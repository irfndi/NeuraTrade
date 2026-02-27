# Operator Guide (Native Runtime)

This guide covers day-to-day operation using the native gateway flow.

## Start and Stop

```bash
# Start services
./bin/neuratrade gateway start

# Check status
./bin/neuratrade gateway status
curl -s http://localhost:8080/health

# Stop services
./bin/neuratrade gateway stop
```

## Logs

```bash
# Gateway lifecycle logs
tail -f ~/.neuratrade/logs/gateway.log

# Backend service logs
tail -f ~/.neuratrade/logs/backend.log

# CCXT service logs
tail -f ~/.neuratrade/logs/ccxt.log

# Telegram service logs
tail -f ~/.neuratrade/logs/telegram.log
```

## Telegram Operations

```bash
# Webhook / external-connection status
bash services/backend-api/scripts/webhook-control.sh status

# Enable webhook mode (uses values from .env)
bash services/backend-api/scripts/webhook-control.sh enable

# Disable webhook mode
bash services/backend-api/scripts/webhook-control.sh disable
```

## Health Monitoring

```bash
# One-time check
bash services/backend-api/scripts/health-monitor-enhanced.sh check

# Continuous monitor (10 minutes)
bash services/backend-api/scripts/health-monitor-enhanced.sh monitor 600

# Generate JSON report
bash services/backend-api/scripts/health-monitor-enhanced.sh report
```

## Testing and Validation

```bash
# Full native test suite
bash services/backend-api/scripts/test.sh test

# Backend-only
bash services/backend-api/scripts/test.sh backend

# Frontend-only (telegram service)
bash services/backend-api/scripts/test.sh frontend
```

## Data and Runtime Paths

- Home: `~/.neuratrade` (or `NEURATRADE_HOME`)
- Logs: `~/.neuratrade/logs`
- PID files: `~/.neuratrade/pids`
- SQLite DB (default): `~/.neuratrade/data/neuratrade.db`
