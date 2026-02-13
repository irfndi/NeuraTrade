# NeuraTrade Operator Guide

**Version:** 1.0  
**Last Updated:** February 2026  
**Audience:** System operators, DevOps engineers, production support teams

---

## Table of Contents

1. [Overview](#overview)
2. [System Architecture](#system-architecture)
3. [Service Management](#service-management)
4. [Health Monitoring](#health-monitoring)
5. [Telegram Bot Operations](#telegram-bot-operations)
6. [Database Operations](#database-operations)
7. [Redis Operations](#redis-operations)
8. [Exchange Connectivity](#exchange-connectivity)
9. [Trading Operations](#trading-operations)
10. [Alerting & Notifications](#alerting--notifications)
11. [Backup & Recovery](#backup--recovery)
12. [Performance Tuning](#performance-tuning)
13. [Common Operational Tasks](#common-operational-tasks)
14. [Runbooks](#runbooks)

---

## Overview

NeuraTrade is a multi-service cryptocurrency trading platform consisting of:

- **Backend API** (Go) - Core trading logic, signal processing, arbitrage detection
- **CCXT Service** (Bun/TypeScript) - Exchange connectivity via CCXT library
- **Telegram Service** (Bun/TypeScript) - Bot interface for operator control

### Key Responsibilities

As an operator, you are responsible for:
- Monitoring system health and performance
- Managing trading operations via Telegram bot
- Handling exchange connectivity issues
- Database and cache maintenance
- Responding to alerts and incidents

---

## System Architecture

```
┌─────────────────────────────────────────────────────────────────┐
│                        NeuraTrade Platform                       │
├─────────────────────────────────────────────────────────────────┤
│                                                                  │
│  ┌──────────────────┐    ┌──────────────────┐                   │
│  │  Telegram Bot    │    │   Backend API    │                   │
│  │  (grammY/Bun)    │◄──►│   (Go/Gin)       │                   │
│  │  :3003           │    │   :8080          │                   │
│  └──────────────────┘    └────────┬─────────┘                   │
│                                   │                              │
│                          ┌────────▼─────────┐                    │
│                          │  CCXT Service    │                    │
│                          │  (Bun/TypeScript)│                    │
│                          │  :3000           │                    │
│                          └────────┬─────────┘                    │
│                                   │                              │
│  ┌──────────────────┐    ┌────────▼─────────┐                    │
│  │     Redis        │    │   PostgreSQL     │                    │
│  │     :6379        │    │   :5432          │                    │
│  └──────────────────┘    └──────────────────┘                    │
│                                                                  │
└─────────────────────────────────────────────────────────────────┘
                                   │
                                   ▼
                    ┌──────────────────────────┐
                    │   Exchange APIs (CCXT)   │
                    │   Binance, OKX, Bybit... │
                    └──────────────────────────┘
```

### Service Ports

| Service | Default Port | Description |
|---------|-------------|-------------|
| Backend API | 8080 | Main REST API |
| CCXT Service | 3000 | Exchange bridge |
| Telegram Service | 3003 | Bot webhook/polling |
| PostgreSQL | 5432 | Database |
| Redis | 6379 | Cache/Pub-Sub |

---

## Service Management

### Starting Services

#### Development Mode

```bash
# Start all services with hot reload
make dev

# Start only infrastructure (PostgreSQL + Redis)
make dev-setup

# Start individual services
cd services/backend-api && air          # Go backend with hot reload
cd services/ccxt-service && bun run dev # CCXT service
cd services/telegram-service && bun dev # Telegram service
```

#### Production Mode (Docker)

```bash
# Start all services
make docker-run

# Or with Docker Compose directly
docker compose up -d

# Check service status
docker compose ps
```

### Stopping Services

```bash
# Stop development environment
make dev-down

# Stop Docker services
docker compose down

# Emergency stop (all containers)
docker compose kill
```

### Restarting Services

```bash
# Restart specific service
docker compose restart backend-api
docker compose restart ccxt-service
docker compose restart telegram-service

# Rolling restart (zero downtime)
docker compose up -d --no-deps --build backend-api
```

### Viewing Logs

```bash
# All services
make logs

# Specific service
docker compose logs -f backend-api
docker compose logs -f ccxt-service
docker compose logs -f telegram-service

# Last 100 lines
docker compose logs --tail=100 backend-api
```

---

## Health Monitoring

### Health Endpoints

| Endpoint | Purpose | Expected Response |
|----------|---------|-------------------|
| `GET /health` | Basic health check | `{"status": "healthy"}` |
| `GET /ready` | Readiness check (all deps) | `{"status": "ready"}` |
| `GET /api/status` | Detailed system status | JSON with component status |

### Health Check Commands

```bash
# Quick health check
curl http://localhost:8080/health

# Detailed status
curl http://localhost:8080/api/status | jq

# Check all services
curl http://localhost:8080/health && \
curl http://localhost:3000/health && \
curl http://localhost:3003/health
```

### Key Metrics to Monitor

| Metric | Warning Threshold | Critical Threshold |
|--------|------------------|-------------------|
| CPU Usage | > 70% | > 90% |
| Memory Usage | > 75% | > 90% |
| Response Time | > 500ms | > 2s |
| Error Rate | > 1% | > 5% |
| Active Connections | > 80% pool | > 95% pool |
| Queue Depth | > 100 | > 500 |

### Monitoring Commands

```bash
# Docker resource usage
docker stats

# Container health
docker compose ps

# Database connections
docker compose exec postgres psql -U postgres -c "SELECT count(*) FROM pg_stat_activity;"

# Redis info
docker compose exec redis redis-cli INFO
```

---

## Telegram Bot Operations

### Bot Commands Reference

#### Status & Monitoring

| Command | Description |
|---------|-------------|
| `/start` | Initialize bot session |
| `/status` | System status and health |
| `/performance` | Trading performance metrics |
| `/summary` | Portfolio summary |
| `/budget` | Budget status and usage |

#### Trading Control

| Command | Description |
|---------|-------------|
| `/begin` | Start autonomous trading |
| `/pause` | Pause trading operations |
| `/liquidate` | Controlled liquidation |
| `/liquidate_all` | Emergency full liquidation |

#### Wallet Management

| Command | Description |
|---------|-------------|
| `/wallet` | Wallet status and balances |
| `/withdraw` | Initiate withdrawal |

#### Quest & Monitoring

| Command | Description |
|---------|-------------|
| `/quest` | Quest status |
| `/monitor` | Monitoring controls |

#### Diagnostic

| Command | Description |
|---------|-------------|
| `/doctor` | Diagnostic runbooks |
| `/set_ai_key` | Configure AI provider |

### Telegram Troubleshooting

#### Bot Not Responding

1. Check service status:
   ```bash
   docker compose ps telegram-service
   docker compose logs telegram-service
   ```

2. Verify bot token:
   ```bash
   # Check environment
   docker compose exec telegram-service env | grep TELEGRAM
   ```

3. Restart service:
   ```bash
   docker compose restart telegram-service
   ```

#### Webhook vs Polling Mode

```bash
# Check current mode
docker compose logs telegram-service | grep -i "webhook\|polling"

# Switch to polling (for development)
TELEGRAM_WEBHOOK_URL= docker compose up -d telegram-service

# Enable webhook (for production)
TELEGRAM_WEBHOOK_URL=https://your-domain.com/webhook docker compose up -d telegram-service
```

---

## Database Operations

### Connection

```bash
# Connect to PostgreSQL
docker compose exec postgres psql -U postgres -d neuratrade

# Or from host
psql -h localhost -U postgres -d neuratrade
```

### Migration Management

```bash
# Run pending migrations
make migrate

# Check migration status
make migrate-status

# List available migrations
make migrate-list

# Manual migration
cd services/backend-api/database && ./migrate.sh
```

### Common Database Queries

```sql
-- Active connections
SELECT count(*) FROM pg_stat_activity;

-- Long-running queries
SELECT pid, query, state, duration 
FROM pg_stat_activity 
WHERE state = 'active' AND duration > 5000;

-- Table sizes
SELECT relname, pg_size_pretty(pg_total_relation_size(relid)) 
FROM pg_catalog.pg_statio_user_tables 
ORDER BY pg_total_relation_size(relid) DESC;

-- Recent signals
SELECT * FROM signals ORDER BY created_at DESC LIMIT 10;

-- Trading performance
SELECT 
    DATE(created_at) as date,
    COUNT(*) as total_signals,
    AVG(quality_score) as avg_quality
FROM signals 
WHERE created_at > NOW() - INTERVAL '7 days'
GROUP BY DATE(created_at);
```

### Backup

```bash
# Create backup
docker compose exec postgres pg_dump -U postgres neuratrade > backup_$(date +%Y%m%d).sql

# Restore from backup
cat backup_20260213.sql | docker compose exec -T postgres psql -U postgres neuratrade
```

---

## Redis Operations

### Connection

```bash
# Connect to Redis CLI
docker compose exec redis redis-cli

# With authentication
docker compose exec redis redis-cli -a your_password
```

### Key Operations

```bash
# List all keys
KEYS *

# Get key value
GET ticker:BTC/USDT:binance

# Check TTL
TTL ticker:BTC/USDT:binance

# Delete key
DEL ticker:BTC/USDT:binance

# Flush all (DANGER - production use with caution)
FLUSHALL
```

### Monitoring

```bash
# Redis info
docker compose exec redis redis-cli INFO

# Memory usage
docker compose exec redis redis-cli INFO memory

# Connected clients
docker compose exec redis redis-cli INFO clients

# Real-time stats
docker compose exec redis redis-cli --stat
```

### Performance Tuning

```bash
# Set max memory
docker compose exec redis redis-cli CONFIG SET maxmemory 256mb

# Set eviction policy
docker compose exec redis redis-cli CONFIG SET maxmemory-policy allkeys-lru
```

---

## Exchange Connectivity

### Supported Exchanges

NeuraTrade supports 100+ exchanges via CCXT, including:
- Binance (Spot + Futures)
- OKX
- Bybit
- Coinbase
- Kraken
- And many more...

### Exchange Health Check

```bash
# Via API
curl http://localhost:3000/health/exchanges

# Via CCXT service logs
docker compose logs ccxt-service | grep -i "exchange"
```

### Common Exchange Issues

| Issue | Cause | Resolution |
|-------|-------|------------|
| Rate Limited | Too many API calls | Reduce polling frequency |
| Invalid API Key | Key revoked or wrong permissions | Regenerate key, check permissions |
| Connection Timeout | Network issues | Check firewall, retry logic kicks in |
| Market Not Found | Symbol delisted | Update active symbols list |

### Exchange API Key Management

1. Generate new API key on exchange
2. Ensure required permissions (read, trade if needed)
3. Add to environment:
   ```bash
   EXCHANGE_BINANCE_API_KEY=your_key
   EXCHANGE_BINANCE_API_SECRET=your_secret
   ```
4. Restart services to pick up new keys

---

## Trading Operations

### Trading Modes

| Mode | Description | Risk Level |
|------|-------------|------------|
| Paper Trading | Simulated trades, no real execution | Low |
| Live Trading | Real order execution | High |
| Shadow Mode | Track signals without execution | None |

### Starting Trading

Via Telegram:
```
/begin
```

Via API:
```bash
curl -X POST http://localhost:8080/api/trading/start
```

### Pausing Trading

Via Telegram:
```
/pause
```

Via API:
```bash
curl -X POST http://localhost:8080/api/trading/pause
```

### Emergency Liquidation

Via Telegram:
```
/liquidate_all
```

Via API:
```bash
curl -X POST http://localhost:8080/api/liquidate/all
```

### Risk Management Controls

| Control | Default | Description |
|---------|---------|-------------|
| Daily Loss Cap | 5% | Max daily loss before halt |
| Max Drawdown | 15% | Max portfolio drawdown |
| Position Size | Dynamic | Based on portfolio and risk |
| Consecutive Loss Pause | 3 | Pause after N consecutive losses |

### Monitoring Active Positions

```bash
# Via API
curl http://localhost:8080/api/positions | jq

# Via database
docker compose exec postgres psql -U postgres -d neuratrade -c "SELECT * FROM positions WHERE status = 'open';"
```

---

## Alerting & Notifications

### Alert Types

| Alert | Severity | Notification Channel |
|-------|----------|---------------------|
| Service Down | Critical | Telegram + Webhook |
| High Error Rate | High | Telegram |
| Trading Signal | Info | Telegram |
| Budget Warning | Medium | Telegram |
| Exchange Disconnected | High | Telegram + Webhook |
| Risk Event | Critical | Telegram + Webhook |

### Configuring Webhooks

```bash
# Set webhook URL in environment
WEBHOOK_URL=https://your-webhook-endpoint.com/alerts
WEBHOOK_SECRET=your_webhook_secret

# Restart to apply
docker compose restart backend-api
```

### Alert Format

```json
{
  "severity": "critical",
  "type": "service_down",
  "service": "ccxt-service",
  "message": "CCXT service is not responding",
  "timestamp": "2026-02-13T10:30:00Z",
  "metadata": {
    "last_healthy": "2026-02-13T10:25:00Z"
  }
}
```

---

## Backup & Recovery

### Backup Strategy

| Component | Frequency | Retention |
|-----------|-----------|-----------|
| PostgreSQL | Daily | 30 days |
| Redis | On-demand | N/A (ephemeral) |
| Config | On change | Indefinite |

### Automated Backup Script

```bash
#!/bin/bash
# Add to crontab: 0 2 * * * /path/to/backup.sh

DATE=$(date +%Y%m%d_%H%M%S)
BACKUP_DIR="/backups"

# Database backup
docker compose exec -T postgres pg_dump -U postgres neuratrade > "$BACKUP_DIR/db_$DATE.sql"

# Compress
gzip "$BACKUP_DIR/db_$DATE.sql"

# Clean old backups (keep 30 days)
find "$BACKUP_DIR" -name "db_*.sql.gz" -mtime +30 -delete
```

### Recovery Procedure

1. **Stop services**:
   ```bash
   docker compose down
   ```

2. **Restore database**:
   ```bash
   cat backup.sql.gz | gunzip | docker compose exec -T postgres psql -U postgres neuratrade
   ```

3. **Start services**:
   ```bash
   docker compose up -d
   ```

4. **Verify data integrity**:
   ```bash
   curl http://localhost:8080/api/status
   ```

---

## Performance Tuning

### Backend API

```bash
# Environment variables for tuning
GOMAXPROCS=4                    # CPU cores
GOMEMLIMIT=2GiB                 # Memory limit
HTTP_READ_TIMEOUT=30s           # HTTP timeout
HTTP_WRITE_TIMEOUT=30s          # HTTP timeout
DATABASE_MAX_CONNS=25           # DB connection pool
DATABASE_MAX_IDLE_CONNS=10      # Idle connections
REDIS_POOL_SIZE=100             # Redis connections
```

### PostgreSQL

```sql
-- Check current settings
SHOW ALL;

-- Common tuning (in postgresql.conf)
shared_buffers = 256MB
work_mem = 64MB
maintenance_work_mem = 128MB
effective_cache_size = 1GB
max_connections = 100
```

### Redis

```bash
# Memory optimization
CONFIG SET maxmemory 512mb
CONFIG SET maxmemory-policy allkeys-lru

# Persistence (if needed)
CONFIG SET save "900 1 300 10"
```

---

## Common Operational Tasks

### Daily Checklist

- [ ] Check service health (`/status` or `curl /health`)
- [ ] Review error logs
- [ ] Verify trading status
- [ ] Check budget utilization
- [ ] Monitor exchange connectivity

### Weekly Tasks

- [ ] Review performance metrics
- [ ] Check disk space usage
- [ ] Verify backup integrity
- [ ] Update dependencies (if needed)
- [ ] Review security advisories

### Monthly Tasks

- [ ] Rotate API keys
- [ ] Review access permissions
- [ ] Capacity planning
- [ ] Security audit review
- [ ] Update documentation

---

## Runbooks

### Runbook: Service Not Responding

1. **Check container status**:
   ```bash
   docker compose ps
   ```

2. **Check logs**:
   ```bash
   docker compose logs --tail=100 <service>
   ```

3. **Check resource usage**:
   ```bash
   docker stats
   ```

4. **Restart service**:
   ```bash
   docker compose restart <service>
   ```

5. **If restart fails**:
   ```bash
   docker compose down <service>
   docker compose up -d <service>
   ```

6. **Escalate if still failing**

### Runbook: High Error Rate

1. **Identify error source**:
   ```bash
   docker compose logs backend-api | grep -i error | tail -50
   ```

2. **Check upstream services**:
   ```bash
   curl http://localhost:3000/health  # CCXT
   docker compose exec redis redis-cli PING
   docker compose exec postgres pg_isready
   ```

3. **Check rate limits**:
   - Review exchange API usage
   - Reduce polling frequency if needed

4. **Apply fix**:
   - If config issue: update env, restart
   - If code issue: rollback or patch

### Runbook: Exchange Disconnected

1. **Identify affected exchanges**:
   ```bash
   curl http://localhost:3000/health/exchanges
   ```

2. **Check API key validity**:
   - Verify key permissions
   - Check if key is revoked

3. **Check network connectivity**:
   ```bash
   curl -I https://api.binance.com
   ```

4. **Restart CCXT service**:
   ```bash
   docker compose restart ccxt-service
   ```

5. **Monitor recovery**:
   ```bash
   docker compose logs -f ccxt-service
   ```

### Runbook: Emergency Trading Halt

1. **Pause trading**:
   ```
   /pause  (Telegram)
   ```

2. **Verify halt**:
   ```bash
   curl http://localhost:8080/api/trading/status
   ```

3. **Assess situation**:
   - Review open positions
   - Check error logs
   - Verify market conditions

4. **If liquidation needed**:
   ```
   /liquidate_all  (Telegram)
   ```

5. **Post-incident review**

---

## Support

### Getting Help

- **Documentation**: `/docs` directory
- **Logs**: Always include relevant logs when reporting issues
- **GitHub Issues**: For bugs and feature requests
- **Security Issues**: security@neuratrade.com

### Useful Commands Reference

```bash
# Service management
make dev-setup          # Start infrastructure
make dev               # Start with hot reload
make dev-down          # Stop services
make logs              # View all logs

# Health & status
curl localhost:8080/health
curl localhost:8080/api/status
docker compose ps

# Database
make migrate           # Run migrations
make migrate-status    # Check migration status

# Security
make security-check    # Run security scans

# Build
make build             # Build all services
make test              # Run tests
make lint              # Run linters
```

---

*This guide is maintained by the NeuraTrade operations team. For updates, see the GitHub repository.*
