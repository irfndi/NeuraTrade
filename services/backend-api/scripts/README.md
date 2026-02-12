# NeuraTrade Deployment Scripts

This directory contains the optimized deployment and operations scripts for NeuraTrade.

## Core Scripts

### 🚀 Service Management
- **`startup-orchestrator.sh`** - Service startup orchestrator
  - Coordinates startup of infrastructure, core services, and proxy services
  - Usage: `./startup-orchestrator.sh [start|stop|restart|status|help]`

### 🔍 Health Monitoring
- **`health-monitor-enhanced.sh`** - Comprehensive health monitoring
  - Real-time service health checks and auto-recovery
  - Usage: `./health-monitor-enhanced.sh [monitor|check|restart|status|report|verify|help]`

### 🧪 Testing
- **`test.sh`** - Unified testing framework
  - Consolidates all testing scenarios (unit, integration, full)
  - Usage: `./test.sh [test|build|start|health|status|cleanup|help]`

### 🗄️ Database Management
- **`simple-migrate.sh`** - Database migration script
  - Handles schema migrations securely
  - Usage: `./simple-migrate.sh` (typically run via Docker)
- **`check-database-status.sh`** - Quick database status check
- **`init.sql`** - Minimal database initialization for Docker volume

### 🛡️ Security & Validation
- **`validate-env.sh`** - Validates required environment variables
- **`validate-redis-security.sh`** - Checks Redis security configuration
- **`setup-firewall.sh`** - Configures UFW firewall rules
- **`setup-github-secrets.sh`** - Helper to set up GitHub Actions secrets
- **`webhook-control.sh`** - Manages Telegram webhooks and external connections

## Quick Start

### Local Development
```bash
# Validate environment
./scripts/validate-env.sh

# Start services
./scripts/startup-orchestrator.sh start

# Run tests
./scripts/test.sh
```

### Production Operations
```bash
# Monitor health
./scripts/health-monitor-enhanced.sh monitor

# Run migrations
make migrate-docker
```
