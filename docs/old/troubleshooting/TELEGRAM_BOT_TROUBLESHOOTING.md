# Telegram Bot Integration Troubleshooting Guide

## Overview
This guide documents common issues and solutions for Telegram bot integration in the NeuraTrade platform.

## Issue: "Telegram webhook endpoint - to be implemented"

### Symptoms
- Webhook returns "Telegram webhook endpoint - to be implemented" instead of processing requests
- Bot doesn't respond to user messages
- No error logs in application logs

### Root Cause Analysis
This issue occurs when:
1. **CCXT Service Unavailability**: The application fails to start properly due to CCXT service connection issues
2. **DNS Resolution Failure**: Container networking issues prevent service discovery
3. **Service Dependencies**: The application cannot initialize due to missing service dependencies

### Resolution Steps

#### Step 1: Verify Service Health
```bash
# Check if all services are running
docker-compose -f docker-compose.single-droplet.yml ps

# Check application logs for startup errors
docker logs neuratrade-app 2>&1 | tail -20
```

#### Step 2: Restart All Services
```bash
# Navigate to project directory
cd /root/neuratrade

# Restart all services to resolve connectivity issues
docker-compose -f docker-compose.single-droplet.yml up -d
```

#### Step 3: Verify Webhook Functionality
```bash
# Test webhook endpoint
curl -k -X POST https://YOUR_SERVER_IP/api/v1/telegram/webhook \
  -H 'Content-Type: application/json' \
  -d '{"update_id":123456,"message":{"message_id":1,"from":{"id":123,"first_name":"Test"},"chat":{"id":123,"type":"private"},"date":1609459200,"text":"/start"}}'
```

Expected response: `{"ok":true}`

#### Step 4: Configure Telegram Webhook
```bash
# Set webhook URL with Telegram
source .env
curl -k -X POST "https://api.telegram.org/bot${TELEGRAM_BOT_TOKEN}/setWebhook" \
  -d 'url=https://YOUR_SERVER_IP/api/v1/telegram/webhook'
```

#### Step 5: Verify Webhook Status
```bash
# Check webhook configuration
source .env
curl -k "https://api.telegram.org/bot${TELEGRAM_BOT_TOKEN}/getWebhookInfo"
```

### Prevention Strategies

#### 1. Health Checks Implementation
Add health checks to docker-compose.yml:
```yaml
healthcheck:
  test: ["CMD", "curl", "-f", "http://localhost:8080/health"]
  interval: 30s
  timeout: 10s
  retries: 3
  start_period: 40s
```

#### 2. Service Dependencies
Ensure proper service startup order:
```yaml
depends_on:
  redis:
    condition: service_healthy
  postgres:
    condition: service_healthy
```

#### 3. Monitoring Setup
Implement container monitoring:
```yaml
# Add to docker-compose.yml
watchtower:
  image: containrrr/watchtower
  volumes:
    - /var/run/docker.sock:/var/run/docker.sock
  command: --interval 3600 --cleanup
```

### Common Error Patterns

| Error Message | Cause | Solution |
|---------------|-------|----------|
| "dial tcp: lookup ccxt-service on 127.0.0.11:53: server misbehaving" | DNS resolution failure | Restart all services with `docker-compose up -d` |
| "Telegram bot is not initialized" | Missing TELEGRAM_BOT_TOKEN | Verify environment variables |
| "certificate verify failed" | Self-signed SSL certificate | Use Let's Encrypt or configure Telegram to accept self-signed |

### Debugging Commands

```bash
# Check environment variables
docker exec neuratrade-app env | grep TELEGRAM

# Test service connectivity
docker exec neuratrade-app curl -f http://localhost:3001/health

# Check network connectivity
docker network ls
docker network inspect neuratrade_default
```

### Quick Recovery Script
```bash
#!/bin/bash
# save as recover-telegram.sh

echo "Restarting services..."
cd /root/neuratrade
docker-compose -f docker-compose.single-droplet.yml restart

echo "Waiting for services..."
sleep 10

echo "Testing webhook..."
curl -k -X POST https://localhost/api/v1/telegram/webhook \
  -H 'Content-Type: application/json' \
  -d '{"update_id":123456,"message":{"message_id":1,"from":{"id":123,"first_name":"Test"},"chat":{"id":123,"type":"private"},"date":1609459200,"text":"/start"}}'

echo "Recovery complete!"
```