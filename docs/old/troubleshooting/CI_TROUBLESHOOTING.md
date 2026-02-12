# CI/CD Troubleshooting Guide

## Quick Fix: CI Failed Due to Missing Environment Variables

### ✅ Problem Solved
CI failures caused by missing `.env` files have been resolved by implementing **GitHub Secrets-based environment management**.

### 🔄 How We Fixed It

1. **Removed dependency on .env files in repository**
2. **Added automatic .env file creation during deployment**
3. **Centralized secret management with GitHub Secrets**
4. **Created automated setup script**

### 🚀 Quick Setup (5 minutes)

```bash
# 1. Run the automated setup
./scripts/setup-github-secrets.sh

# 2. Verify secrets are set
gh secret list

# 3. Push to main branch to trigger deployment
```

### 📋 Required GitHub Secrets

**Application Secrets (Required):**
- `JWT_SECRET` - JWT signing key (auto-generated)
- `TELEGRAM_BOT_TOKEN` - Your bot token from @BotFather
- `TELEGRAM_WEBHOOK_URL` - HTTPS webhook URL
- `TELEGRAM_WEBHOOK_SECRET` - Webhook verification (auto-generated)
- `FEATURE_TELEGRAM_BOT` - Set to `true`

**Deployment Secrets (Required):**
- `DIGITALOCEAN_ACCESS_TOKEN` - DigitalOcean API token
- `DEPLOY_SSH_KEY` - Private SSH key for server access
- `DEPLOY_USER` - SSH username (e.g., `root`)
- `DEPLOY_HOST` - Server IP or hostname

### 🔧 Manual Setup (if needed)

```bash
# Generate secure secrets
JWT_SECRET=$(openssl rand -base64 64 | tr -d '\r\n')
TELEGRAM_WEBHOOK_SECRET=$(openssl rand -hex 32 | tr -d '\r\n')

# Set secrets manually
gh secret set JWT_SECRET --body "$JWT_SECRET"
gh secret set TELEGRAM_BOT_TOKEN --body "your_bot_token"
gh secret set TELEGRAM_WEBHOOK_URL --body "https://yourdomain.com/webhook"
gh secret set TELEGRAM_WEBHOOK_SECRET --body "$TELEGRAM_WEBHOOK_SECRET"
gh secret set FEATURE_TELEGRAM_BOT --body "true"
```

### 🏠 Local Development

```bash
# For local development, use .env.local
cp .env.example .env.local
# Edit .env.local with your local values
# Never commit .env files to git
```

### 🐛 Common Issues & Solutions

| Issue | Solution |
|-------|----------|
| CI fails with "environment variable not found" | Run `./scripts/setup-github-secrets.sh` |
| Deployment fails with Telegram errors | Verify `TELEGRAM_BOT_TOKEN` is correct |
| Webhook setup fails | Ensure `TELEGRAM_WEBHOOK_URL` uses HTTPS |
| SSH connection fails | Check `DEPLOY_HOST`, `DEPLOY_USER`, and `DEPLOY_SSH_KEY` |

### 📊 Verification Commands

```bash
# Check all required secrets
gh secret list | grep -E "(JWT_SECRET|TELEGRAM_BOT_TOKEN|TELEGRAM_WEBHOOK_URL|TELEGRAM_WEBHOOK_SECRET|FEATURE_TELEGRAM_BOT)"

# Check deployment secrets
gh secret list | grep -E "(DEPLOY_HOST|DEPLOY_USER|DEPLOY_SSH_KEY|DIGITALOCEAN_ACCESS_TOKEN)"

# Test deployment
make deploy
```

### 📚 Documentation

- **Full Deployment Guide**: See `.github/DEPLOYMENT.md`
- **Secrets Management**: See `SECRETS_MANAGEMENT.md`
- **Environment Variables**: See `.env.example`

### 🎯 Next Steps

1. ✅ Run the setup script
2. ✅ Verify secrets are configured
3. ✅ Test deployment by pushing to main branch
4. ✅ Monitor deployment logs in GitHub Actions

**No more CI failures due to missing .env files!** 🎉