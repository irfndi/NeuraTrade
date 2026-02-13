# NeuraTrade Troubleshooting Guide

**Version:** 1.0  
**Last Updated:** February 2026  
**Audience:** Developers, operators, support teams

---

## Table of Contents

1. [Quick Diagnostics](#quick-diagnostics)
2. [Service Issues](#service-issues)
3. [Database Issues](#database-issues)
4. [Redis Issues](#redis-issues)
5. [Exchange Connectivity Issues](#exchange-connectivity-issues)
6. [Telegram Bot Issues](#telegram-bot-issues)
7. [Trading Issues](#trading-issues)
8. [CI/CD Issues](#cicd-issues)
9. [Performance Issues](#performance-issues)
10. [Error Recovery](#error-recovery)

---

## Quick Diagnostics

### System Health Check

Run this first when diagnosing issues:

```bash
#!/bin/bash
# Quick diagnostic script

echo "=== NeuraTrade Diagnostics ==="
echo ""

echo "1. Docker Services Status:"
docker compose ps
echo ""

echo "2. Health Endpoints:"
curl -s http://localhost:8080/health | jq . 2>/dev/null || echo "Backend API: DOWN"
curl -s http://localhost:3000/health | jq . 2>/dev/null || echo "CCXT Service: DOWN"
curl -s http://localhost:3003/health | jq . 2>/dev/null || echo "Telegram Service: DOWN"
echo ""

echo "3. Database Connection:"
docker compose exec -T postgres pg_isready -U postgres || echo "PostgreSQL: UNREACHABLE"
echo ""

echo "4. Redis Connection:"
docker compose exec -T redis redis-cli PING || echo "Redis: UNREACHABLE"
echo ""

echo "5. Recent Errors:"
docker compose logs --tail=20 backend-api 2>&1 | grep -i error | tail -5
echo ""

echo "6. Resource Usage:"
docker stats --no-stream --format "table {{.Name}}\t{{.CPUPerc}}\t{{.MemUsage}}"
```

### Log Analysis Commands

```bash
# Find recent errors
docker compose logs backend-api 2>&1 | grep -i "error\|fatal\|panic" | tail -50

# Find warnings
docker compose logs backend-api 2>&1 | grep -i "warn" | tail -30

# Search for specific error
docker compose logs backend-api 2>&1 | grep -i "connection refused"

# Follow logs in real-time
docker compose logs -f backend-api

# Logs with timestamp filtering
docker compose logs --since=1h backend-api
docker compose logs --since="2026-02-13T10:00:00" backend-api
```

---

## Service Issues

### Backend API Won't Start

#### Symptoms
- Container exits immediately
- Health check fails
- Connection refused errors

#### Diagnosis

```bash
# Check exit code
docker compose ps backend-api

# View startup logs
docker compose logs backend-api 2>&1 | head -100

# Check for port conflicts
lsof -i :8080
netstat -tulpn | grep 8080
```

#### Common Causes & Solutions

| Cause | Solution |
|-------|----------|
| Missing environment variables | Copy `.env.example` to `.env` and configure |
| Port already in use | Kill process or change port: `PORT=8081 make run` |
| Database not ready | Wait for PostgreSQL: `docker compose up -d postgres && sleep 5` |
| Redis not ready | Wait for Redis: `docker compose up -d redis && sleep 3` |
| Invalid config | Validate config file syntax |

#### Solution Steps

```bash
# 1. Verify environment
cat .env | grep -E "^(DATABASE|REDIS|JWT)"

# 2. Check dependencies
docker compose up -d postgres redis
sleep 5

# 3. Restart with fresh state
docker compose down backend-api
docker compose up -d backend-api

# 4. Check logs
docker compose logs -f backend-api
```

### CCXT Service Issues

#### Symptoms
- Exchange API calls failing
- Timeouts on market data requests
- "CCXT service unavailable" errors

#### Diagnosis

```bash
# Check service status
curl http://localhost:3000/health

# View recent logs
docker compose logs --tail=50 ccxt-service

# Test specific exchange
curl "http://localhost:3000/api/tickers?exchanges=binance"
```

#### Common Causes & Solutions

| Cause | Solution |
|-------|----------|
| Bun not installed | Install Bun: `curl -fsSL https://bun.sh/install | bash` |
| Node modules missing | Run: `cd services/ccxt-service && bun install` |
| Exchange rate limit | Reduce request frequency |
| Invalid API credentials | Verify API key and secret |

#### Solution Steps

```bash
# 1. Rebuild service
cd services/ccxt-service
bun install
bun run build

# 2. Restart container
docker compose restart ccxt-service

# 3. Verify health
curl http://localhost:3000/health
```

### Telegram Service Issues

#### Symptoms
- Bot not responding to commands
- Webhook errors
- No notifications received

#### Diagnosis

```bash
# Check service status
docker compose ps telegram-service
docker compose logs --tail=50 telegram-service

# Verify bot token
docker compose exec telegram-service env | grep TELEGRAM_BOT_TOKEN
```

#### Common Causes & Solutions

| Cause | Solution |
|-------|----------|
| Invalid bot token | Get new token from @BotFather |
| Webhook URL invalid | Ensure HTTPS URL is accessible |
| Wrong chat ID | Verify operator chat binding |
| Service not running | Restart service |

#### Solution Steps

```bash
# 1. Verify token
curl "https://api.telegram.org/bot<YOUR_TOKEN>/getMe"

# 2. Check webhook status
curl "https://api.telegram.org/bot<YOUR_TOKEN>/getWebhookInfo"

# 3. Switch to polling mode (debug)
TELEGRAM_WEBHOOK_URL= docker compose up -d telegram-service

# 4. Restart service
docker compose restart telegram-service
```

---

## Database Issues

### Connection Refused

#### Symptoms
```
dial tcp 127.0.0.1:5432: connect: connection refused
pq: could not connect to server
```

#### Solution

```bash
# Check if PostgreSQL is running
docker compose ps postgres

# Start PostgreSQL
docker compose up -d postgres

# Verify connection
docker compose exec postgres pg_isready -U postgres

# Check connection string
echo $DATABASE_URL
```

### Too Many Connections

#### Symptoms
```
pq: sorry, too many clients already
FATAL: remaining connection slots are reserved
```

#### Diagnosis

```bash
# Check active connections
docker compose exec postgres psql -U postgres -c "SELECT count(*) FROM pg_stat_activity;"

# See connection sources
docker compose exec postgres psql -U postgres -c "SELECT usename, application_name, count(*) FROM pg_stat_activity GROUP BY usename, application_name;"
```

#### Solution

```sql
-- Kill idle connections
SELECT pg_terminate_backend(pid) 
FROM pg_stat_activity 
WHERE state = 'idle' 
AND query_start < NOW() - INTERVAL '10 minutes';

-- Increase max connections (requires restart)
ALTER SYSTEM SET max_connections = 200;
```

```bash
# Restart PostgreSQL
docker compose restart postgres
```

### Migration Failures

#### Symptoms
```
migration failed: dirty database version
ERROR: relation already exists
```

#### Diagnosis

```bash
# Check migration status
make migrate-status

# View migration table
docker compose exec postgres psql -U postgres -d neuratrade -c "SELECT * FROM schema_migrations ORDER BY version;"
```

#### Solution

```bash
# Force version (only if you know what you're doing)
cd services/backend-api/database
./migrate.sh force <version>

# Rollback last migration
./migrate.sh down 1

# Reset and re-run (CAUTION: destroys data)
./migrate.sh drop
./migrate.sh up
```

### Slow Queries

#### Symptoms
- API timeouts
- High database CPU

#### Diagnosis

```sql
-- Find slow queries
SELECT query, calls, total_time, mean_time 
FROM pg_stat_statements 
ORDER BY mean_time DESC 
LIMIT 10;

-- Find missing indexes
SELECT relname, seq_scan, idx_scan 
FROM pg_stat_user_tables 
WHERE seq_scan > idx_scan 
ORDER BY seq_scan DESC;

-- Check table sizes
SELECT relname, pg_size_pretty(pg_total_relation_size(relid)) 
FROM pg_catalog.pg_statio_user_tables 
ORDER BY pg_total_relation_size(relid) DESC 
LIMIT 10;
```

#### Solution

```sql
-- Add index for common queries
CREATE INDEX idx_signals_created_at ON signals(created_at DESC);
CREATE INDEX idx_positions_status ON positions(status);

-- Analyze tables
ANALYZE signals;
ANALYZE positions;
```

---

## Redis Issues

### Connection Refused

#### Symptoms
```
dial tcp 127.0.0.1:6379: connect: connection refused
redis: connection refused
```

#### Solution

```bash
# Check if Redis is running
docker compose ps redis

# Start Redis
docker compose up -d redis

# Test connection
docker compose exec redis redis-cli PING
```

### Memory Issues

#### Symptoms
```
OOM command not allowed when used_memory > 'maxmemory'
```

#### Diagnosis

```bash
# Check memory usage
docker compose exec redis redis-cli INFO memory

# Check max memory setting
docker compose exec redis redis-cli CONFIG GET maxmemory
```

#### Solution

```bash
# Set max memory
docker compose exec redis redis-cli CONFIG SET maxmemory 256mb

# Set eviction policy
docker compose exec redis redis-cli CONFIG SET maxmemory-policy allkeys-lru
```

### High Latency

#### Symptoms
- Slow cache hits
- Timeout errors

#### Diagnosis

```bash
# Check latency
docker compose exec redis redis-cli --latency

# Check slow log
docker compose exec redis redis-cli SLOWLOG GET 10

# Check commands
docker compose exec redis redis-cli INFO commandstats
```

#### Solution

```bash
# Reset slow log
docker compose exec redis redis-cli SLOWLOG RESET

# Check for blocking commands
docker compose exec redis redis-cli CLIENT LIST
```

---

## Exchange Connectivity Issues

### Rate Limiting

#### Symptoms
```
rate limit exceeded
too many requests
429 Too Many Requests
```

#### Diagnosis

```bash
# Check CCXT service logs
docker compose logs ccxt-service | grep -i "rate limit"

# Check request frequency
docker compose logs ccxt-service | grep -c "fetchTicker"
```

#### Solution

```bash
# Reduce polling interval (in .env)
COLLECTOR_POLL_INTERVAL=10s  # Increase from default

# Restart service
docker compose restart backend-api ccxt-service
```

### API Key Issues

#### Symptoms
```
invalid api key
permission denied
unauthorized
```

#### Diagnosis

```bash
# Check API key configuration
docker compose exec backend-api env | grep EXCHANGE | head -5

# Test API key manually
curl -H "X-MBX-APIKEY: your_key" https://api.binance.com/api/v3/account
```

#### Solution

1. Verify API key on exchange website
2. Check key permissions (need read + trade)
3. Regenerate key if compromised
4. Update `.env` file
5. Restart services

### Network Issues

#### Symptoms
```
connection timeout
no route to host
network unreachable
```

#### Diagnosis

```bash
# Test connectivity
curl -I https://api.binance.com
curl -I https://api.okx.com

# Check DNS
nslookup api.binance.com

# Check firewall
telnet api.binance.com 443
```

#### Solution

1. Check firewall rules
2. Verify network connectivity
3. Check if IP is blocked by exchange
4. Try alternative endpoints

### Invalid Symbol

#### Symptoms
```
invalid symbol
market not found
```

#### Diagnosis

```bash
# Check available symbols
curl "http://localhost:3000/api/symbols?exchange=binance"

# Verify symbol format
docker compose logs ccxt-service | grep -i "symbol"
```

#### Solution

1. Check symbol format (e.g., `BTC/USDT` not `BTCUSDT`)
2. Verify symbol is still active on exchange
3. Update symbol list in configuration

---

## Telegram Bot Issues

### Bot Not Responding

#### Symptoms
- No response to commands
- Timeouts on interactions

#### Diagnosis

```bash
# Check service status
docker compose ps telegram-service

# Check logs
docker compose logs --tail=50 telegram-service

# Verify bot is reachable
curl "https://api.telegram.org/bot<TOKEN>/getMe"
```

#### Solution

```bash
# Restart service
docker compose restart telegram-service

# Check token validity
docker compose exec telegram-service env | grep TELEGRAM_BOT_TOKEN
```

### Webhook Errors

#### Symptoms
```
webhook failed
bad webhook: bad request
url host is invalid
```

#### Diagnosis

```bash
# Check webhook status
curl "https://api.telegram.org/bot<TOKEN>/getWebhookInfo"

# Test webhook endpoint
curl -X POST https://your-domain.com/webhook -d '{"update_id": 1}'
```

#### Solution

```bash
# Delete existing webhook
curl "https://api.telegram.org/bot<TOKEN>/deleteWebhook"

# Set new webhook
curl "https://api.telegram.org/bot<TOKEN>/setWebhook?url=https://your-domain.com/webhook"

# Or switch to polling
TELEGRAM_WEBHOOK_URL= docker compose up -d telegram-service
```

### Permission Denied

#### Symptoms
- "You are not authorized" messages
- Commands ignored

#### Diagnosis

```bash
# Check operator binding
docker compose logs telegram-service | grep -i "chat\|operator"

# Verify chat ID
docker compose exec postgres psql -U postgres -d neuratrade -c "SELECT * FROM operators;"
```

#### Solution

1. Ensure user is registered as operator
2. Check `TELEGRAM_OPERATOR_CHAT_ID` environment variable
3. Run `/start` command to bind chat

---

## Trading Issues

### Orders Not Executing

#### Symptoms
- Signals generated but no trades
- Order stuck in pending

#### Diagnosis

```bash
# Check trading status
curl http://localhost:8080/api/trading/status

# Check for risk blocks
docker compose logs backend-api | grep -i "risk\|block"

# Check position status
docker compose exec postgres psql -U postgres -d neuratrade -c "SELECT * FROM positions WHERE status = 'pending';"
```

#### Solution

1. Verify trading is enabled (`/begin`)
2. Check budget limits
3. Verify risk controls not blocking
4. Check exchange connectivity

### Risk Controls Triggered

#### Symptoms
```
daily loss cap reached
max drawdown exceeded
consecutive loss limit
trading paused
```

#### Diagnosis

```bash
# Check risk status
curl http://localhost:8080/api/risk/status

# Check daily P&L
docker compose logs backend-api | grep -i "daily loss\|pnl"
```

#### Solution

1. Review trading performance
2. Adjust risk parameters if appropriate
3. Wait for reset period
4. Manual override (with caution)

### Position Stuck

#### Symptoms
- Position shows open but exchange has none
- Unable to close position

#### Diagnosis

```bash
# Check database vs exchange
docker compose exec postgres psql -U postgres -d neuratrade -c "SELECT * FROM positions WHERE status = 'open';"

# Compare with exchange
curl "http://localhost:3000/api/positions?exchange=binance"
```

#### Solution

```bash
# Force position sync
curl -X POST http://localhost:8080/api/positions/sync

# Manual position update (with caution)
docker compose exec postgres psql -U postgres -d neuratrade -c "UPDATE positions SET status = 'closed' WHERE id = 'xxx';"
```

---

## CI/CD Issues

### Build Failures

#### Symptoms
```
build failed
undefined reference
compilation error
```

#### Diagnosis

```bash
# Local build test
make build

# Check Go version
go version

# Check dependencies
cd services/backend-api && go mod verify
```

#### Solution

```bash
# Clean and rebuild
make clean
go clean -cache
make build

# Update dependencies
cd services/backend-api && go mod tidy && go mod download
```

### Test Failures

#### Symptoms
```
test failed
assertion error
race condition detected
```

#### Diagnosis

```bash
# Run tests locally
make test

# Run with verbose output
cd services/backend-api && go test -v ./...

# Run specific test
cd services/backend-api && go test -v -run TestName ./internal/services
```

#### Solution

1. Check if services are running (tests need DB/Redis)
2. Run `make dev-setup` before tests
3. Check for race conditions: `go test -race ./...`

### Lint Errors

#### Symptoms
```
golangci-lint failed
undefined variable
unused import
```

#### Diagnosis

```bash
# Run linter locally
make lint

# Check specific file
cd services/backend-api && golangci-lint run ./internal/services/specific.go
```

#### Solution

```bash
# Auto-fix formatting
make fmt

# Fix imports
cd services/backend-api && goimports -w .

# Address specific lint errors
cd services/backend-api && golangci-lint run --fix
```

### Docker Build Failures

#### Symptoms
```
docker build failed
no space left on device
permission denied
```

#### Diagnosis

```bash
# Check disk space
df -h

# Check Docker disk usage
docker system df

# Check Docker logs
journalctl -u docker
```

#### Solution

```bash
# Clean Docker resources
docker system prune -a

# Remove old images
docker image prune -a

# Remove build cache
docker builder prune
```

---

## Performance Issues

### High CPU Usage

#### Diagnosis

```bash
# Container resource usage
docker stats

# Process list inside container
docker compose exec backend-api top

# Go profiling
curl http://localhost:8080/debug/pprof/profile?seconds=30 > cpu.prof
go tool pprof cpu.prof
```

#### Solution

1. Reduce polling frequency
2. Optimize queries (add indexes)
3. Scale horizontally
4. Profile and fix hot paths

### High Memory Usage

#### Diagnosis

```bash
# Memory usage
docker stats --no-stream

# Go memory profile
curl http://localhost:8080/debug/pprof/heap > mem.prof
go tool pprof mem.prof

# Check for leaks
docker compose logs backend-api | grep -i "memory\|oom"
```

#### Solution

1. Set memory limits in Docker
2. Adjust Go GC: `GOMEMLIMIT=2GiB`
3. Check for goroutine leaks
4. Review caching strategy

### Slow API Response

#### Diagnosis

```bash
# Response time
curl -w "%{time_total}s\n" http://localhost:8080/health

# Check downstream services
curl -w "%{time_total}s\n" http://localhost:3000/health

# Database query time
docker compose exec postgres psql -U postgres -c "SELECT NOW();" 2>&1 | grep -i time
```

#### Solution

1. Add caching for frequent queries
2. Optimize database queries
3. Check for N+1 query patterns
4. Consider pagination for large datasets

---

## Error Recovery

### Circuit Breaker Stuck Open

#### Symptoms
```
circuit breaker open
request rejected
```

#### Diagnosis

```bash
# Check circuit breaker status
docker compose logs backend-api | grep -i "circuit breaker"

# Check downstream health
curl http://localhost:3000/health
```

#### Solution

```bash
# Wait for half-open state (usually 60 seconds)
# Or restart service to reset circuit breaker
docker compose restart backend-api
```

### Goroutine Leak

#### Symptoms
- Memory steadily increasing
- Many goroutines in `top`

#### Diagnosis

```bash
# Check goroutine count
curl http://localhost:8080/debug/pprof/goroutine?debug=1

# Count goroutines
curl -s http://localhost:8080/debug/pprof/goroutine?debug=1 | grep -c "goroutine"
```

#### Solution

1. Restart service (temporary)
2. Report bug with goroutine dump
3. Fix leak in code

### Deadlock

#### Symptoms
- Service completely frozen
- No response to any request

#### Diagnosis

```bash
# Get goroutine stack
curl http://localhost:8080/debug/pprof/goroutine?debug=2 > deadlock.txt

# Check last logs
docker compose logs --tail=100 backend-api
```

#### Solution

1. Restart service
2. Analyze goroutine dump for circular wait
3. Report with full stack trace

---

## Emergency Contacts

| Issue Type | Contact | Response Time |
|------------|---------|---------------|
| Security Incident | security@neuratrade.com | < 1 hour |
| Production Down | On-call rotation | < 15 minutes |
| Exchange Issues | Exchange support | Varies |
| Bug Report | GitHub Issues | < 24 hours |

---

## Diagnostic Data Collection

When reporting issues, collect:

```bash
#!/bin/bash
# Collect diagnostic data

mkdir -p diagnostics_$(date +%Y%m%d_%H%M%S)
cd diagnostics_*

# Service status
docker compose ps > services.txt

# Logs (last 1000 lines)
docker compose logs --tail=1000 > logs.txt

# Health status
curl -s http://localhost:8080/health > health_api.json
curl -s http://localhost:3000/health > health_ccxt.json
curl -s http://localhost:3003/health > health_telegram.json

# System resources
docker stats --no-stream > resources.txt
free -m >> resources.txt
df -h >> resources.txt

# Database status
docker compose exec -T postgres psql -U postgres -c "SELECT version();" > db_version.txt
docker compose exec -T postgres psql -U postgres -c "SELECT count(*) FROM pg_stat_activity;" >> db_version.txt

# Create archive
cd ..
tar -czvf diagnostics.tar.gz diagnostics_*
```

---

*This guide is maintained by the NeuraTrade team. For the latest version, see the GitHub repository.*
