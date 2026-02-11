# Security Credentials Management

## Overview

This document outlines the security fixes implemented to address hardcoded credentials detected by GitGuardian and provides guidance for secure credential management.

## Fixed Security Vulnerabilities

### 1. CI/CD Workflow Hardcoded Passwords

**Files Fixed:**
- `.github/workflows/ci-cd.yml`

**Changes Made:**
- Replaced hardcoded `POSTGRES_PASSWORD: postgres` with `POSTGRES_PASSWORD: ${{ secrets.POSTGRES_TEST_PASSWORD }}`
- Updated `DATABASE_URL` to use GitHub secrets for consistency

**Required Action:**
Add `POSTGRES_TEST_PASSWORD` to your GitHub repository secrets:
1. Go to Settings > Secrets and variables > Actions
2. Add new repository secret: `POSTGRES_TEST_PASSWORD`
3. Set value to a secure password for testing

### 2. Database Status Script Default Password

**Files Fixed:**
- `scripts/check-database-status.sh`

**Changes Made:**
- Removed hardcoded default password `DB_PASSWORD=${DB_PASSWORD:-postgres}`
- Added validation to require `DB_PASSWORD` environment variable
- Script now exits with error if password not provided

**Usage:**
```bash
export DB_PASSWORD=your_secure_password
./scripts/check-database-status.sh
```

### 3. Configuration Files

**Files Fixed:**
- `config.yaml`
- `docker-compose.ci.yml`
- `docker-compose.single-droplet.yml`
- `ccxt-service/.env.example`

**Changes Made:**
- Removed hardcoded passwords from configuration files
- Added environment variable references with security comments
- Updated Docker Compose files to use `${POSTGRES_PASSWORD:-postgres}` pattern

## Environment Variables Required

### Production Deployment

Ensure these environment variables are set:

```bash
# Database
POSTGRES_PASSWORD=your-secure-postgres-password
DATABASE_PASSWORD=your-secure-database-password

# Security
JWT_SECRET=your-super-secret-jwt-key-minimum-32-chars

# CCXT Service
ADMIN_API_KEY=your-secure-admin-api-key

# Telegram (if using)
TELEGRAM_BOT_TOKEN=your-telegram-bot-token
TELEGRAM_WEBHOOK_SECRET=your-webhook-secret
```

### Development Environment

For local development, create a `.env` file:

```bash
# Copy from .env.example and set secure values
cp .env.example .env

# Edit .env with your secure credentials
# NEVER commit .env to version control
```

## Security Best Practices

### 1. Never Hardcode Credentials
- Use environment variables for all sensitive data
- Use configuration management tools for production
- Implement proper secret rotation policies

### 2. GitHub Secrets Management
- Store all CI/CD credentials in GitHub Secrets
- Use different secrets for different environments
- Regularly rotate secrets

### 3. Docker Security
- Use environment variables in Docker Compose
- Avoid hardcoded values in Dockerfiles
- Use Docker secrets for production deployments

### 4. Database Security
- Use strong, unique passwords
- Enable SSL/TLS connections in production
- Implement proper access controls

## Validation

To validate the security fixes:

1. **Check for hardcoded credentials (safe scanning):**
   ```bash
   # Use specialized tools for secret scanning
   # Install gitleaks for comprehensive secret detection
   gitleaks detect --source . --verbose
   
   # Alternative: Use truffleHog for historical scanning
   truffleHog filesystem . --only-verified
   
   # Manual check for common patterns (without exposing values)
   grep -r "password\s*=\s*['\"][^'\"]*['\"]" --exclude-dir=.git --exclude-dir=node_modules . | grep -v "your-" | grep -v "example" | grep -v "placeholder"
   ```

2. **Verify environment variable usage:**
   ```bash
   grep -r "\${.*PASSWORD" docker-compose*.yml
   grep -r "\$[A-Z_]*PASSWORD" --include="*.yml" --include="*.yaml" .
   ```

3. **Test application startup:**
   ```bash
   # Set required environment variables
   export POSTGRES_PASSWORD=test_password
   export JWT_SECRET=test-jwt-secret-minimum-32-chars
   
   # Test Docker Compose
   docker-compose -f docker-compose.ci.yml up --build
   ```

## Monitoring

- Use GitGuardian or similar tools for continuous secret scanning
- Implement alerts for credential exposure
- Regular security audits of configuration files

## Emergency Response

If credentials are accidentally committed:

1. **Immediate Actions:**
   - Rotate all exposed credentials immediately
   - Remove credentials from Git history
   - Update all deployment environments

2. **Git History Cleanup:**
   ```bash
   # Use git-filter-repo (recommended)
   pip install git-filter-repo
   git filter-repo --strip-blobs-bigger-than 10M --force
   
   # Alternative: Use BFG Repo-Cleaner
   # Download BFG from https://rtyley.github.io/bfg-repo-cleaner/
   # java -jar bfg.jar --delete-files credentials.txt
   
   # IMPORTANT: Contact security team before running these commands
   # These commands rewrite Git history and require force-push
   ```

3. **Notification:**
   - Inform security team
   - Update incident response documentation
   - Review and improve security practices

## Contact

For security-related questions or incidents:
- Security Team: [security@company.com]
- Emergency: [emergency-security@company.com]

---

**Last Updated:** January 2025  
**Next Review:** Quarterly security review