# NeuraTrade Security Documentation

**Version:** 1.0  
**Last Updated:** February 2026  
**Audience:** Security teams, developers, operators

---

## Table of Contents

1. [Security Overview](#security-overview)
2. [Security Architecture](#security-architecture)
3. [Authentication & Authorization](#authentication--authorization)
4. [Data Protection](#data-protection)
5. [Secrets Management](#secrets-management)
6. [Network Security](#network-security)
7. [Exchange API Security](#exchange-api-security)
8. [Trading Security](#trading-security)
9. [Security Monitoring](#security-monitoring)
10. [Incident Response](#incident-response)
11. [Security Best Practices](#security-best-practices)
12. [Compliance](#compliance)

---

## Security Overview

NeuraTrade handles sensitive financial data and executes real trades on cryptocurrency exchanges. Security is a top priority and is implemented at every layer of the application.

### Security Principles

1. **Defense in Depth** - Multiple layers of security controls
2. **Least Privilege** - Minimum required permissions
3. **Zero Trust** - Verify everything, trust nothing
4. **Secure by Default** - Security enabled out of the box
5. **Audit Everything** - Comprehensive logging for forensics

### Security Scope

| Component | Security Concerns |
|-----------|------------------|
| Exchange APIs | API key protection, rate limiting, permissions |
| Telegram Bot | Chat binding, command authorization, webhook security |
| Database | Encryption at rest, access control, query safety |
| Redis | Network isolation, access control |
| Trading | Risk limits, position controls, audit trails |
| Infrastructure | Container security, network segmentation |

---

## Security Architecture

```
┌────────────────────────────────────────────────────────────────────┐
│                        Security Layers                              │
├────────────────────────────────────────────────────────────────────┤
│                                                                     │
│  ┌──────────────────────────────────────────────────────────────┐  │
│  │  Layer 1: Perimeter Security                                  │  │
│  │  - TLS 1.3 encryption                                         │  │
│  │  - Rate limiting                                              │  │
│  │  - IP allowlisting (optional)                                 │  │
│  └──────────────────────────────────────────────────────────────┘  │
│                                                                     │
│  ┌──────────────────────────────────────────────────────────────┐  │
│  │  Layer 2: Application Security                                │  │
│  │  - JWT authentication                                         │  │
│  │  - RBAC authorization                                         │  │
│  │  - Input validation                                           │  │
│  │  - Request signing                                            │  │
│  └──────────────────────────────────────────────────────────────┘  │
│                                                                     │
│  ┌──────────────────────────────────────────────────────────────┐  │
│  │  Layer 3: Data Security                                       │  │
│  │  - AES-256-GCM encryption for API keys                        │  │
│  │  - Argon2 password hashing                                    │  │
│  │  - Key masking in logs                                        │  │
│  └──────────────────────────────────────────────────────────────┘  │
│                                                                     │
│  ┌──────────────────────────────────────────────────────────────┐  │
│  │  Layer 4: Infrastructure Security                             │  │
│  │  - Container isolation                                        │  │
│  │  - Network segmentation                                       │  │
│  │  - Secret management                                          │  │
│  └──────────────────────────────────────────────────────────────┘  │
│                                                                     │
└────────────────────────────────────────────────────────────────────┘
```

---

## Authentication & Authorization

### Authentication Methods

| Method | Use Case | Implementation |
|--------|----------|----------------|
| JWT Tokens | API access | HS256/RS256 signed tokens |
| OTP Codes | Initial setup | One-time passcodes |
| Telegram Binding | Bot access | Chat ID verification |
| API Keys | Exchange access | Encrypted storage |

### JWT Token Security

```yaml
# Token configuration
JWT_ALGORITHM: HS256
JWT_EXPIRY: 24h
JWT_REFRESH_EXPIRY: 168h
JWT_ISSUER: neuratrade
```

**Best Practices:**
- Short expiry times for access tokens
- Refresh tokens with longer expiry
- Token revocation on logout/suspicious activity
- Secure token storage (httpOnly cookies preferred)

### Role-Based Access Control (RBAC)

| Role | Permissions |
|------|-------------|
| `admin` | Full system access |
| `operator` | Trading control, monitoring |
| `viewer` | Read-only access |

### Telegram Authorization

```bash
# Operator binding flow
1. User sends /start to bot
2. System generates OTP
3. Operator enters OTP in CLI
4. Chat ID bound to operator profile
5. Subsequent commands require bound chat
```

---

## Data Protection

### Encryption Standards

| Data Type | Encryption | Algorithm |
|-----------|------------|-----------|
| API Keys (at rest) | Symmetric | AES-256-GCM |
| Passwords | Hashing | Argon2id |
| Transit | TLS | TLS 1.3 |
| Secrets | Environment | Base64 encoded |

### API Key Encryption

```go
// API keys are encrypted with AES-256-GCM
type EncryptedKey struct {
    Ciphertext  []byte // Encrypted data
    Nonce       []byte // GCM nonce (12 bytes)
    KeyID       string // Reference to master key
}
```

**Key Management:**
- Master key stored in `ENCRYPTION_KEY` environment variable
- Key rotation requires re-encrypting all stored keys
- Keys never logged or exposed in API responses

### Sensitive Data Masking

```go
// Automatic masking in logs
func MaskAPIKey(key string) string {
    if len(key) <= 8 {
        return "****"
    }
    return key[:4] + "..." + key[len(key)-4:]
}

// Example: sk_live_abc123xyz789 -> sk_l...89789
```

### Database Security

```sql
-- Connection security
SSL_MODE: require
SSL_CERT: /path/to/cert
SSL_KEY: /path/to/key

-- Access control
GRANT SELECT, INSERT, UPDATE, DELETE ON ALL TABLES IN SCHEMA public TO neuratrade_app;
REVOKE ALL ON ALL TABLES IN SCHEMA public FROM PUBLIC;
```

**Query Safety:**
- All queries use parameterized statements
- No string concatenation for SQL
- ORM-level input validation

---

## Secrets Management

### Environment Variables

```bash
# Required secrets (NEVER commit these)
JWT_SECRET=<random-64-char-string>
ENCRYPTION_KEY=<32-byte-key>
TELEGRAM_BOT_TOKEN=<bot-token>

# Exchange credentials
EXCHANGE_BINANCE_API_KEY=<key>
EXCHANGE_BINANCE_API_SECRET=<secret>

# Database credentials
DATABASE_URL=<connection-string>
POSTGRES_PASSWORD=<password>

# Redis
REDIS_URL=<connection-string>
```

### Secret Generation

```bash
# Generate JWT secret (64 characters)
openssl rand -base64 48 | tr -d '\r\n'

# Generate encryption key (32 bytes)
openssl rand -bytes 32 | base64

# Generate webhook secret
openssl rand -hex 32
```

### GitHub Secrets Management

For CI/CD, secrets are stored in GitHub:

```bash
# Set secret via gh CLI
gh secret set JWT_SECRET --body "$(openssl rand -base64 48)"

# List secrets
gh secret list

# Delete secret
gh secret delete OLD_SECRET
```

### Secret Rotation Schedule

| Secret Type | Rotation Frequency | Rotation Procedure |
|-------------|-------------------|-------------------|
| JWT Secret | 90 days | Update env, restart services |
| Encryption Key | 180 days | Re-encrypt all data |
| Database Password | 90 days | Update database, env, restart |
| Exchange API Keys | On compromise | Revoke old, generate new |
| Telegram Bot Token | On compromise | Revoke via @BotFather |

---

## Network Security

### TLS Configuration

```yaml
# Minimum TLS version
TLS_MIN_VERSION: "1.3"

# Cipher suites
TLS_CIPHER_SUITES:
  - TLS_AES_256_GCM_SHA384
  - TLS_CHACHA20_POLY1305_SHA256
  - TLS_AES_128_GCM_SHA256
```

### Network Segmentation

```yaml
# Docker network configuration
networks:
  frontend:
    driver: bridge
  backend:
    driver: bridge
    internal: true  # No external access
  database:
    driver: bridge
    internal: true
```

### Firewall Rules

```bash
# Example iptables rules
# Allow only necessary ports
iptables -A INPUT -p tcp --dport 8080 -j ACCEPT  # API
iptables -A INPUT -p tcp --dport 443 -j ACCEPT   # HTTPS
iptables -A INPUT -p tcp --dport 22 -j ACCEPT    # SSH
iptables -A INPUT -j DROP                        # Deny rest
```

### Rate Limiting

```yaml
# Rate limit configuration
RATE_LIMIT_ENABLED: true
RATE_LIMIT_REQUESTS: 100
RATE_LIMIT_WINDOW: 1m

# Per-endpoint limits
RATE_LIMIT_API: 100/min
RATE_LIMIT_TELEGRAM: 30/min
RATE_LIMIT_EXCHANGE: 10/sec
```

---

## Exchange API Security

### API Key Permissions

Required permissions per exchange:

| Exchange | Read | Trade | Withdraw |
|----------|------|-------|----------|
| Binance | ✅ | ✅ (optional) | ❌ |
| OKX | ✅ | ✅ (optional) | ❌ |
| Bybit | ✅ | ✅ (optional) | ❌ |

**Best Practice:** 
- Only enable `trade` permission for live trading
- **NEVER** enable `withdraw` permission
- Use IP allowlisting on exchange if available

### API Key Storage

```
┌─────────────────────────────────────────────────────────────┐
│                    API Key Storage Flow                      │
├─────────────────────────────────────────────────────────────┤
│                                                              │
│  1. Operator provides API key via Telegram                   │
│              │                                               │
│              ▼                                               │
│  2. Key encrypted with AES-256-GCM                          │
│              │                                               │
│              ▼                                               │
│  3. Encrypted key stored in database                        │
│              │                                               │
│              ▼                                               │
│  4. Original key wiped from memory                          │
│              │                                               │
│              ▼                                               │
│  5. Key decrypted only when needed for API call             │
│                                                              │
└─────────────────────────────────────────────────────────────┘
```

### Request Signing

For exchanges requiring signed requests:

```go
// Binance signature example
func SignRequest(params string, secret string) string {
    mac := hmac.New(sha256.New, []byte(secret))
    mac.Write([]byte(params))
    return hex.EncodeToString(mac.Sum(nil))
}
```

---

## Trading Security

### Risk Controls

| Control | Description | Default |
|---------|-------------|---------|
| Daily Loss Cap | Max daily loss before halt | 5% |
| Max Drawdown | Portfolio drawdown limit | 15% |
| Position Size | Max position as % of portfolio | Dynamic |
| Consecutive Loss Pause | Pause after N losses | 3 |
| Kill Switch | Emergency trading halt | Available |

### Trade Authorization

```yaml
# Trade authorization flow
1. Signal generated by strategy
2. Risk manager validates
3. Position size calculated
4. Trade execution authorized
5. Order sent to exchange
6. Execution confirmed
7. Position recorded
```

### Audit Trail

All trading actions are logged:

```sql
-- Trading audit log structure
CREATE TABLE trading_audit (
    id UUID PRIMARY KEY,
    action VARCHAR(50) NOT NULL,
    symbol VARCHAR(20) NOT NULL,
    side VARCHAR(10) NOT NULL,
    quantity DECIMAL(20, 8),
    price DECIMAL(20, 8),
    operator_id UUID,
    exchange VARCHAR(50),
    status VARCHAR(20),
    error_message TEXT,
    created_at TIMESTAMP NOT NULL
);
```

### Kill Switch

Emergency trading halt:

```bash
# Via Telegram
/liquidate_all

# Via API
curl -X POST http://localhost:8080/api/trading/halt

# Via database (emergency)
UPDATE system_config SET trading_enabled = false;
```

---

## Security Monitoring

### Security Events

| Event | Severity | Alert Channel |
|-------|----------|---------------|
| Failed login attempts (>5) | High | Telegram + Webhook |
| API key access from new IP | Medium | Telegram |
| Permission denied errors | Medium | Logs |
| Unusual trading volume | High | Telegram + Webhook |
| Exchange API errors | Medium | Logs |
| Circuit breaker triggered | High | Telegram |

### Logging

```yaml
# Security log format
{
    "timestamp": "2026-02-13T10:30:00Z",
    "level": "warn",
    "event": "auth_failure",
    "source_ip": "192.168.1.100",
    "user_id": "user_123",
    "details": {
        "reason": "invalid_token",
        "attempts": 3
    }
}
```

### Intrusion Detection

The system monitors for:
- Multiple failed authentication attempts
- Requests from unusual locations
- Abnormal trading patterns
- Unexpected API access

### Security Scanning

```bash
# Run security scans
make security-check

# Individual tools
gosec ./...                    # Go security scanner
govulncheck ./...              # Vulnerability check
gitleaks detect --source .     # Secret detection
trivy image neuratrade:latest  # Container scanning
```

---

## Incident Response

### Security Incident Levels

| Level | Description | Response Time |
|-------|-------------|---------------|
| P0 - Critical | Active breach, data exfiltration | < 15 minutes |
| P1 - High | Vulnerability exploitation attempt | < 1 hour |
| P2 - Medium | Security policy violation | < 4 hours |
| P3 - Low | Minor security event | < 24 hours |

### Incident Response Procedure

```
┌─────────────────────────────────────────────────────────────┐
│                  Incident Response Flow                      │
├─────────────────────────────────────────────────────────────┤
│                                                              │
│  1. DETECT                                                   │
│     - Automated monitoring alerts                            │
│     - User reports                                           │
│     - External notification                                  │
│              │                                               │
│              ▼                                               │
│  2. CONTAIN                                                  │
│     - Halt trading operations                                │
│     - Revoke compromised keys                                │
│     - Isolate affected systems                               │
│              │                                               │
│              ▼                                               │
│  3. ERADICATE                                                │
│     - Remove threat                                          │
│     - Patch vulnerabilities                                  │
│     - Rotate all secrets                                     │
│              │                                               │
│              ▼                                               │
│  4. RECOVER                                                  │
│     - Restore from clean backup                              │
│     - Resume operations                                      │
│     - Verify system integrity                                │
│              │                                               │
│              ▼                                               │
│  5. POST-MORTEM                                              │
│     - Document incident                                      │
│     - Update security measures                               │
│     - Share learnings                                        │
│                                                              │
└─────────────────────────────────────────────────────────────┘
```

### Emergency Contacts

```yaml
# Security incident contacts
Security Team: security@neuratrade.com
On-Call: +1-xxx-xxx-xxxx (emergency only)
Exchange Support:
  Binance: https://www.binance.com/en/support
  OKX: https://www.okx.com/support
```

### Recovery Checklist

- [ ] Halt all trading operations
- [ ] Revoke compromised credentials
- [ ] Rotate all secrets (JWT, encryption keys, API keys)
- [ ] Review audit logs for unauthorized access
- [ ] Patch exploited vulnerability
- [ ] Verify data integrity
- [ ] Resume operations with new credentials
- [ ] Document incident and lessons learned

---

## Security Best Practices

### For Developers

1. **Never commit secrets** - Use `.env` files (gitignored)
2. **Validate all input** - Never trust user data
3. **Use parameterized queries** - Prevent SQL injection
4. **Encrypt sensitive data** - Use AES-256-GCM for API keys
5. **Log security events** - But never log secrets
6. **Use `decimal.Decimal`** - Never `float64` for money
7. **Review dependencies** - Regular security updates

### For Operators

1. **Strong passwords** - 16+ characters, mixed
2. **Enable MFA** - Where available
3. **Monitor logs** - Daily review
4. **Limit API permissions** - Only what's needed
5. **Regular key rotation** - Follow schedule
6. **Secure Telegram access** - Verify chat binding
7. **Report suspicious activity** - Immediately

### For Infrastructure

1. **Keep updated** - Security patches promptly
2. **Minimal permissions** - Least privilege principle
3. **Network isolation** - Separate sensitive services
4. **Regular backups** - Test restoration
5. **Monitoring** - Real-time alerting
6. **Incident plan** - Documented and tested

---

## Compliance

### Data Handling

| Data Type | Retention | Protection |
|-----------|-----------|------------|
| Trading records | 7 years | Encrypted at rest |
| API keys | While active | AES-256-GCM |
| Access logs | 1 year | Anonymized |
| Personal data | As required | GDPR compliant |

### Security Standards

- **OWASP Top 10** - Web application security
- **CIS Benchmarks** - Container and OS hardening
- **NIST Cybersecurity Framework** - Overall security posture

### Audit Requirements

- All trades logged with timestamps
- User actions traceable
- Regular access reviews
- Penetration testing (annual)

---

## Security Checklist

### Pre-Deployment

- [ ] All secrets in environment variables
- [ ] TLS enabled for all endpoints
- [ ] Rate limiting configured
- [ ] API keys encrypted
- [ ] Database access restricted
- [ ] Network segmentation in place
- [ ] Security scans passed
- [ ] Audit logging enabled

### Regular Reviews

- [ ] Access review (quarterly)
- [ ] Secret rotation (per schedule)
- [ ] Dependency updates (monthly)
- [ ] Security scan (weekly)
- [ ] Log review (daily)
- [ ] Incident response drill (quarterly)

---

## Reporting Security Issues

If you discover a security vulnerability:

1. **DO NOT** open a public GitHub issue
2. **Email**: security@neuratrade.com
3. **Include**:
   - Description of vulnerability
   - Steps to reproduce
   - Potential impact
   - Suggested fix (if available)

**Response Time:**
- Initial response: 48 hours
- Critical issues: 24-hour fix
- Other issues: Per severity

**Safe Harbor:** We support responsible disclosure and will not pursue legal action against researchers who follow this policy.

---

*This document is maintained by the NeuraTrade security team. For questions, contact security@neuratrade.com*
