# Local Development Debugging Guide

## Quick Start Commands

### Starting the Development Environment
```bash
# Start all services
make dev-up

# If ccxt-service fails to start automatically:
docker-compose up -d ccxt-service

# Restart the main application if needed:
docker-compose up -d app
```

### Checking Service Status
```bash
# View all running containers
docker ps

# Check specific container logs
docker logs neuratrade-app --tail 20
docker logs neuratrade-ccxt --tail 20
docker logs neuratrade-postgres --tail 20
docker logs neuratrade-redis --tail 20
```

### Health Checks
```bash
# Main application health
curl -I http://localhost:8080/health

# CCXT service health
curl -I http://localhost:3000/health

# PostgreSQL connectivity
docker exec neuratrade-postgres pg_isready -U neuratrade_user -d neuratrade_db

# Redis connectivity
docker exec neuratrade-redis redis-cli ping
```

## Common Issues and Solutions

### Issue: Main app fails with "no such host" error for ccxt-service
**Symptoms:**
- App logs show: `dial tcp: lookup ccxt-service on x.x.x.x:53: no such host`
- neuratrade-app container exits or fails to start

**Solution:**
1. Check if ccxt-service is running: `docker ps | grep ccxt`
2. If not running, start it: `docker-compose up -d ccxt-service`
3. Wait for ccxt-service to be healthy, then restart app: `docker-compose up -d app`

### Issue: Database connection failures
**Symptoms:**
- App logs show database connection errors
- Migration container fails

**Solution:**
1. Check PostgreSQL status: `docker ps | grep postgres`
2. Verify database connectivity: `docker exec neuratrade-postgres pg_isready -U neuratrade_user -d neuratrade_db`
3. Check database logs: `docker logs neuratrade-postgres`
4. Ensure .env file has correct database credentials

### Issue: Redis connection failures
**Symptoms:**
- App logs show Redis connection errors
- Cache-related functionality fails

**Solution:**
1. Check Redis status: `docker ps | grep redis`
2. Test Redis connectivity: `docker exec neuratrade-redis redis-cli ping`
3. Check