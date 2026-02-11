# Secrets Management Guide

This guide explains how to properly manage environment variables and secrets for the NeuraTrade project without committing sensitive data to GitHub.

## Overview

The project uses GitHub Secrets for secure management of environment variables in CI/CD pipelines. The `.env` file is **never committed** to the repository and is instead created dynamically during deployment from GitHub Secrets.

## GitHub Secrets Setup

### Required Secrets

Add these secrets to your GitHub repository under Settings > Secrets and variables > Actions:

#### Required Secrets
- `JWT_SECRET`: A strong JWT secret key (minimum 32 characters)
- `TELEGRAM_BOT_TOKEN`: Your Telegram bot token from @BotFather
- `TELEGRAM_WEBHOOK_URL`: Your webhook URL (e.g., `https://yourdomain.com/webhook`)
- `TELEGRAM_WEBHOOK_SECRET`: A secret for webhook verification

#### Optional Secrets
- `FEATURE_TELEGRAM_BOT`: Set to `true` or `false` to enable/disable Telegram bot features
- `DIGITALOCEAN_ACCESS_TOKEN`: DigitalOcean API token for deployment
- `DEPLOY_SSH_KEY`: Private SSH key for server access
- `DEPLOY_HOST`: Server hostname or IP address
- `DEPLOY_USER`: SSH username for deployment

### Setting Up Secrets

1. Go to your GitHub repository
2. Navigate to Settings > Secrets and variables > Actions
3. Click "New repository secret"
4. Add each secret with its corresponding value

## Local Development

### Creating .env for Local Development

Create a `.env` file in the project root for local development:

```bash
# Copy the example file
cp .env.example .env

# Edit with your values
nano .env
```

### .env.example Template

```bash
# JWT Configuration
JWT_SECRET=your-super-secret-jwt-key-change-this-for-production

# Telegram Bot Configuration
TELEGRAM_BOT_TOKEN=your-telegram-bot-token-here
TELEGRAM_WEBHOOK_URL=https://yourdomain.com/webhook
TELEGRAM_WEBHOOK_SECRET=your-webhook-secret-here

# Feature Flags
FEATURE_TELEGRAM_BOT=true

# Database Configuration (for local development)
DATABASE_HOST=localhost
DATABASE_PORT=5432
DATABASE_USER=postgres
DATABASE_PASSWORD=postgres
DATABASE_DBNAME=neuratrade
DATABASE_SSLMODE=disable

# Redis Configuration
REDIS_HOST=localhost
REDIS_PORT=6379

# Environment
ENVIRONMENT=development
```

## Production Deployment

### How It Works

1. **No .env files in Git**: The `.env` file is listed in `.gitignore`
2. **Secrets in GitHub**: All sensitive values are stored as GitHub Secrets
3. **Dynamic .env creation**: During CI/CD deployment, the `.env` file is created from GitHub Secrets
4. **Secure defaults**: If secrets are missing, secure defaults are used

### Deployment Process

The CI/CD pipeline automatically:
1. Creates the `.env` file on the server from GitHub Secrets
2. Uses the values in Docker containers via environment variables
3. Never exposes secrets in logs or build artifacts

## Security Best Practices

### Never Commit These Files
- `.env` files (already in .gitignore)
- `*.key` files
- `*.pem` files
- Any file containing API keys or secrets

### Generate Strong Secrets

#### JWT Secret
```bash
# Generate a strong JWT secret
openssl rand -base64 64
```

#### Webhook Secret
```bash
# Generate a webhook secret
openssl rand -hex 32
```

### Rotate Secrets Regularly

1. Update secrets in GitHub Secrets
2. Redeploy the application
3. Update any local development environments

## Troubleshooting

### Missing Secrets in CI/CD

If deployment fails due to missing secrets:

1. Check GitHub Secrets are properly configured
2. Verify secret names match exactly (case-sensitive)
3. Check deployment logs for any environment variable errors

### Local Development Issues

If environment variables aren't loading locally:

1. Ensure `.env` file exists in project root
2. Check Docker Compose is loading the .env file
3. Verify variable names match those expected by the application

## Alternative: Using Docker Secrets (Advanced)

For even more secure deployments, consider using Docker Secrets:

1. Store secrets in Docker Swarm or Kubernetes
2. Use secret management services like HashiCorp Vault
3. Implement runtime secret injection

## Verification

### Check Environment Variables

After deployment, verify environment variables are properly set:

```bash
# On the server
ssh user@your-server
sudo cat /opt/neuratrade/.env

# Check container environment
docker exec neuratrade-app env | grep -v "PASSWORD"
```

### Test Application

Verify the application is working with the configured secrets:

```bash
# Test JWT authentication
curl -H "Authorization: Bearer your-jwt-token" https://yourdomain.com/api/v1/health

# Test Telegram webhook
curl -X POST https://yourdomain.com/webhook -d '{"test": true}'
```

## Emergency Rollback

If secrets are compromised:

1. Immediately rotate all affected secrets in GitHub
2. Redeploy the application
3. Monitor for any suspicious activity
4. Update any external services using the compromised secrets