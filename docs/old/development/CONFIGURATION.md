# NeuraTrade Configuration Guide

This guide explains how to configure NeuraTrade for different environments using our standardized configuration system.

## 🏗️ Configuration Architecture

Our configuration system uses a simplified approach aligned with Coolify and Docker:

1. **Environment Variables** (highest priority) - Managed via Coolify or `.env` file.
2. **Root Configuration File** (`config.yml`) - Base application settings.
3. **Defaults** (lowest priority) - Hardcoded defaults in the application.

## 📁 Configuration Files

### Core Files

- **`.env.example`** - Template for environment variables
- **`.env`** - Your actual environment variables (git-ignored)
- **`config.yml`** - Main application configuration
- **`redis.conf`** - Redis configuration
- **`nginx.conf`** - Nginx configuration
- **`docker-compose.yml`** - Single Docker configuration for all environments

## 🚀 Quick Start

### 1. Configure Environment Variables

```bash
# Copy the template
cp .env.example .env

# Edit with your values
nano .env
```

### 2. Start Services

```bash
# Start all services
make run
# OR
docker-compose up -d
```

## 🔧 Environment Variables

### Required Variables

| Variable | Description | Development | Production |
|----------|-------------|-------------|------------|
| `ENVIRONMENT` | Environment name | `development` | `production` |
| `POSTGRES_USER` | Database user | `postgres` | Set secret |
| `POSTGRES_PASSWORD` | Database password | `postgres` | Set secret |
| `POSTGRES_DB` | Database name | `neuratrade` | Set secret |
| `JWT_SECRET` | JWT signing key | `dev-secret` | Set secret |

### Optional Variables

| Variable | Default | Description |
|----------|---------|-------------|
| `SERVER_PORT` | `8080` | Application port |
| `PORT` | `3001` | CCXT service port |
| `CCXT_SERVICE_URL` | `http://localhost:3001` | CCXT service URL |
| `TELEGRAM_BOT_TOKEN` | - | Telegram bot token |
| `TELEGRAM_WEBHOOK_URL` | - | Telegram webhook URL (if using webhooks) |
| `TELEGRAM_WEBHOOK_SECRET` | - | Telegram webhook secret token |
| `TELEGRAM_USE_POLLING` | `true` | Use polling instead of webhooks |
| `TELEGRAM_PORT` | `3002` | Telegram service port |
| `TELEGRAM_API_BASE_URL` | `http://localhost:8080` | Base URL for the Go API |
| `RUN_TELEGRAM_SERVICE` | `true` | Start Telegram service in unified container |
| `TELEGRAM_EXTERNAL_SERVICE` | `true` | Disable Go bot when Telegram runs separately |
| `REDIS_PASSWORD` | - | Redis password |

## 🐳 Docker Configuration

We use a single `docker-compose.yml` for all environments, with environment-specific behavior controlled by environment variables.

```bash
# Start services
docker-compose up -d

# Check logs
docker-compose logs -f
```

## 🔄 CI/CD Integration

### GitHub Actions

The CI/CD pipeline is simplified to validation only:

1. **Validation**: Linting, Formatting, Testing.
2. **Build Check**: Ensures the application builds correctly.

Deployment is triggered via Coolify webhooks when changes are pushed to the target branch.

## 🛠️ Configuration Validation

### Validate Configuration

```bash
# Check configuration syntax
docker-compose config

# Check service health
docker-compose ps
```

### Test Endpoints

```bash
# Health check
curl http://localhost:8080/health

# Metrics
curl http://localhost:9090/metrics

# CCXT service health
curl http://localhost:3001/health
```

## 🔍 Troubleshooting

### Common Issues

#### Port Conflicts

```bash
# Check port usage
lsof -i :8080
lsof -i :3001
lsof -i :5432

# Change ports in .env
SERVER_PORT=8081
PORT=3002
POSTGRES_PORT=5433
```

#### Database Connection Issues

```bash
# Check database health
docker-compose exec postgres pg_isready -U postgres

# Check connection from app
docker-compose exec app nc -zv postgres 5432
```

#### Redis Connection Issues

```bash
# Check Redis health
docker-compose exec redis redis-cli ping

# Check connection from app
docker-compose exec app nc -zv redis 6379
```
