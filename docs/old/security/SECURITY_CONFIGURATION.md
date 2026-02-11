# Security Configuration Guide

This document outlines the security improvements made to address GitGuardian alerts and provides guidance for secure deployment.

## Security Improvements

### 1. Removed Hardcoded Secrets

#### Test Files
- **Fixed**: `internal/middleware/auth_test.go` now uses dynamically generated secrets instead of hardcoded values
- **Implementation**: Added `generateTestSecret()` function that creates random 32-byte secrets for each test
- **Benefit**: Eliminates static secrets in test code that could be accidentally used in production

#### Docker Configuration
- **Fixed**: `docker-compose.yml` no longer contains default passwords
- **Implementation**: Uses `${VAR:?Error message}` syntax to require environment variables
- **Benefit**: Forces explicit password configuration, preventing accidental use of default credentials

#### Migration Scripts
- **Fixed**: `scripts/robust-migrate.sh` requires `DB_PASSWORD` environment variable
- **Implementation**: Removed default password fallback, script fails if password not provided
- **Benefit**: Ensures database credentials are always explicitly configured

### 2. Environment Variable Requirements

#### Required Variables for Production
```bash
# Database (REQUIRED)
POSTGRES_PASSWORD=your-secure-password-here

# Security (REQUIRED)
JWT_SECRET=your-jwt-secret-here
ADMIN_API_KEY=your-admin-api-key-here
```

#### Generating Secure Values
```bash
# Generate a secure JWT secret (32+ characters)
openssl rand -base64 32

# Generate a secure admin API key
openssl rand -base64 32

# Generate a secure database password
openssl rand -base64 24
```

## Setup Instructions

### Development Environment

1. **Copy the development environment file**:
   ```bash
   cp .env.development .env
   ```

2. **Update credentials** (optional for local development):
   ```bash
   # Edit .env and change default development passwords if desired
   nano .env
   ```

3. **Start services**:
   ```bash
   docker-compose up -d
   ```

### Production Environment

1. **Create production environment file**:
   ```bash
   cp .env.example .env
   ```

2. **Generate secure credentials**:
   ```bash
   # Generate JWT secret
   echo "JWT_SECRET=$(openssl rand -base64 32)" >> .env
   
   # Generate admin API key
   echo "ADMIN_API_KEY=$(openssl rand -base64 32)" >> .env
   
   # Generate database password
   echo "POSTGRES_PASSWORD=$(openssl rand -base64 24)" >> .env
   ```

3. **Configure remaining variables**:
   ```bash
   nano .env  # Set remaining required variables
   ```

4. **Verify configuration**:
   ```bash
   # Check that all required variables are set
   docker-compose config
   ```

## Security Best Practices

### 1. Environment Variables
- ✅ **DO**: Use environment variables for all secrets
- ✅ **DO**: Generate random, strong passwords (24+ characters)
- ✅ **DO**: Use different credentials for each environment
- ❌ **DON'T**: Commit `.env` files to version control
- ❌ **DON'T**: Use default or example passwords in production

### 2. Secret Management
- ✅ **DO**: Rotate secrets regularly
- ✅ **DO**: Use a secret management service in production (AWS Secrets Manager, HashiCorp Vault, etc.)
- ✅ **DO**: Limit access to production secrets
- ❌ **DON'T**: Share secrets via email, chat, or other insecure channels

### 3. Testing
- ✅ **DO**: Use dynamically generated secrets in tests
- ✅ **DO**: Use separate test databases
- ✅ **DO**: Clean up test data after tests complete
- ❌ **DON'T**: Use production credentials in tests

## Verification

### Check for Hardcoded Secrets
```bash
# Scan for potential hardcoded secrets
grep -r "password.*=.*['\"]" --exclude-dir=.git --exclude="*.md" .
grep -r "secret.*=.*['\"]" --exclude-dir=.git --exclude="*.md" .
grep -r "key.*=.*['\"]" --exclude-dir=.git --exclude="*.md" .
```

### Validate Environment Configuration
```bash
# Check that docker-compose requires environment variables
docker-compose config 2>&1 | grep -i "must be set"
```

### Test Security
```bash
# Run tests to ensure dynamic secret generation works
go test ./internal/middleware/...
```

## Compliance

These changes address the following security concerns:

1. **GitGuardian Alerts**: All hardcoded secrets have been removed
2. **OWASP Guidelines**: Secrets are externalized from code
3. **12-Factor App**: Configuration is stored in environment variables
4. **Security Scanning**: Code passes static analysis security scans

## Monitoring

### Security Events to Monitor
- Failed authentication attempts
- Admin API access
- Database connection failures
- Unusual API usage patterns

### Recommended Tools
- Application logs (structured JSON)
- Security scanning (GitGuardian, Snyk)
- Runtime monitoring (SIEM integration)
- Access logging (nginx/reverse proxy)

## Support

For security-related questions or concerns:
1. Review this documentation
2. Check the `.env.example` file for configuration options
3. Consult the main README.md for general setup instructions
4. Follow the principle of least privilege for all access controls