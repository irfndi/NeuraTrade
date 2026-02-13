# GitHub Copilot Instructions for NeuraTrade Go

This document provides repository-specific guidelines for GitHub Copilot to follow when assisting with development in this cryptocurrency arbitrage trading system.

## Project Overview

NeuraTrade is a comprehensive cryptocurrency arbitrage detection and technical analysis platform built with Go. It features real-time market data collection, arbitrage opportunity identification, and technical indicator calculations with support for 100+ cryptocurrency exchanges via CCXT.

## Project Structure & Module Organization

- `cmd/server/main.go` - HTTP entrypoint; runtime wiring lives under `internal/`
- `internal/` - Application-specific logic (API handlers, services, config, telemetry, cache, CCXT integration)
- `pkg/` - Shared packages; prefer `internal/` for app-specific logic, keep reusable abstractions in `pkg/`
- `ccxt-service/` - Bun-based exchange service; treat as sibling project with own tests and Docker build
- `configs/` - Configuration templates; copy `.env.template` or `configs/config.template.yml` for environments
- `test/` - Integration suites and mocks; unit tests live beside source as `*_test.go`
- `database/` - Database migrations and seeding scripts (use `database/migrate.sh`)
- `scripts/` - Deployment, migration, and utility scripts
- `.github/workflows/` - CI/CD pipelines (ci-cd.yml, build-and-test.yml, lint.yml)

## Build, Test, and Development Commands

**CRITICAL**: Always run these commands in this exact order for new development:

```bash
# Build and run
make build          # Compile Go API to bin/neuratrade
make run            # Build then run locally
make dev            # Hot reload via air; requires go install github.com/air-verse/air@latest

# 2. Start required services (Postgres + Redis) before running tests
make dev-setup

# 3. Build the application
make build          # Compiles Go API to bin/celebrum-ai

# 4. Run tests (requires services from step 2)
make test           # Runs Go unit tests and Bun tests (if Bun installed)

# 5. Format code before committing
make fmt            # Format with gofmt/goimports

# 6. Run linter (requires golangci-lint)
make lint           # Run golangci-lint

# 7. Stop services when done
make dev-down       # Stop development environment
```

### Alternative: Hot Reload Development

```bash
# Install air if not already installed
make install-tools

# Run with auto-reload (watches for file changes)
make dev            # Hot reload via air
```

### Testing Workflow Details

**IMPORTANT**: Tests require database and cache services running.

**Option 1: Using Docker services (Recommended)**
```bash
make dev-setup      # Starts Postgres (5432) + Redis (6379)
make test           # Runs all tests
make test-coverage  # Run tests with coverage report
make dev-down       # Cleanup
```

**Option 2: Using CI test configuration**
```bash
cp .env.ci .env.test  # Setup CI test environment (uses mocks)
make test             # Tests run with optimized settings
```

**Test Execution Time**: ~2-3 minutes for full suite (includes database integration tests)

**Coverage Requirement**: 80% minimum (non-blocking warning if below via `make coverage-check`)

### Troubleshooting Build/Test Issues

**Issue**: "Bun not installed" warning during build
- **Impact**: CCXT service build skipped (Go build still succeeds)
- **Fix**: Install Bun from https://bun.sh then run `cd ccxt-service && bun install`

**Issue**: golangci-lint not found
- **Fix**: Run `make install-tools` or `go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest`

**Issue**: Port conflicts (8080, 5432, 6379)
- **Fix**: Stop conflicting services or change ports in `.env` file

**Issue**: Database connection timeout during tests
- **Symptom**: Tests hang or timeout after 2 minutes
- **Fix**: Verify Docker services running: `docker compose ps`
- **Fix**: Check database health: `docker compose exec postgres pg_isready -U postgres`

**Issue**: YAML module cache conflict in tests
- **Symptom**: Conflicts with `gopkg.in/yaml.v3@v3.0.1` and `go.yaml.in/yaml/v3@v3.0.4`
- **Fix**: Clear cache: `go clean -cache -modcache -testcache`

**Issue**: Redis warning "overcommit_memory is set to 0"
- **Impact**: Non-critical warning in development
- **Fix**: Run `scripts/fix-redis-sysctl.sh` if needed

## CI/CD Pipeline

### GitHub Actions Workflows

**Location**: `.github/workflows/`

**1. ci-cd.yml** (Main deployment pipeline)
- **Triggers**: push to main/develop/staging, PRs, manual dispatch
- **Jobs**: preflight → test → security → build → deploy → verify → notify
- **Test Requirements**: 
  - Can be skipped ONLY on development branch via `workflow_dispatch` with `skip_tests=true`
  - Security scans are NEVER bypassed (Trivy vulnerability scanner)
- **Services**: PostgreSQL 18, Redis 7 (GitHub Actions services)
- **Test Timing**: ~2-3 minutes with optimized database settings
- **Coverage**: 80% minimum (non-blocking)

**2. build-and-test.yml** (Fast PR validation)
- **Triggers**: All PRs and pushes to main/develop
- **Jobs**: test-go → test-ccxt → build-docker → security-scan
- **Timing**: ~5-8 minutes total
- **Go Linter**: golangci-lint v2.5.0 with 5-minute timeout

**3. lint.yml** (Code quality checks)
- **Triggers**: PRs and pushes
- **Linter**: golangci-lint with extended timeout

### CI Environment Configuration

```bash
# CI uses optimized database settings:
DATABASE_MAX_CONNS: 5
DATABASE_MAX_IDLE_CONNS: 2
DATABASE_CONNECT_TIMEOUT: 5
DATABASE_POOL_TIMEOUT: 5

# Test execution with race detection:
go test -v -race -timeout=2m -coverprofile=coverage.out ./...
```

### Validation Before Committing

**ALWAYS run these commands before creating a PR**:

```bash
make fmt           # Format code (~5 seconds)
make lint          # Check for issues (~30-60 seconds)
make test          # Run tests (~2-3 minutes)
make ci-check      # Run full CI suite locally (lint + test + build)
```

**Expected Timing for `make ci-check`**: ~3-5 minutes total

## Coding Style & Naming Conventions

### Go Code
- Target Go 1.25; format with `gofmt`/`goimports` (tabs, canonical imports)
- Use idiomatic Go naming:
  - Exported identifiers in `CamelCase`
  - Packages lowercase, short, descriptive
  - Interfaces with `-er` suffix when applicable (e.g., `Trader`, `Exchanger`)
- Keep functions under 50 lines when possible
- Use `decimal.Decimal` for financial calculations; **never use `float64` for monetary values**

### TypeScript/JavaScript (CCXT Service)
- **Format**: Run `bun run format` before committing
- **Build**: `bun run build` (creates dist/ directory)
- **Tests**: Co-located `*.test.ts` files, run with `bun test`
- Keep strict lint output clean

### General Principles
- Prefer short, descriptive directory and file names
- Keep shared interfaces in `pkg/`
- Separate business logic from infrastructure concerns

## Testing Guidelines

- Use table-driven tests for Go packages
- Place mocks in `internal/testutil` or `test/testmocks`
- Run `make test` before pushing
- For targeted Go packages: `go test -v ./internal/services/...`
- Coverage expectation: ≥80% on touched packages
- Name integration tests under `test/integration`
- Skip external dependencies unless orchestrated in CI
- Test financial calculations with precision edge cases (rounding, overflow)
- Use `-race` flag to detect race conditions: `go test -race ./...`

## Commit & Pull Request Guidelines

- Follow Conventional Commits: `feat:`, `fix:`, `refactor:`, `docs:`, `test:`, `chore:`
- Keep commit subjects imperative and ≤72 characters
- Reference issues in commit bodies or PR descriptions (e.g., `Fixes #123`, `Closes #456`)
- PRs must include:
  - Summary of changes
  - Rationale and context
  - Test evidence (`make test` output or screenshots)
  - Updated documentation/config as needed
- Ensure CI passes before requesting review (build, lint, tests, Trivy security scan)

## Security Guidelines

### Critical Rules - Never Do These
- **Never commit secrets or API keys** to the repository (use `.env` files, which are gitignored)
- **Never log sensitive data** (API keys, secrets, credentials, tokens)
- **Never use `float64` for monetary values** (precision loss - always use `decimal.Decimal`)
- **Never hardcode configuration values** (use environment variables or config files)

### Required Practices
- Derive local env files from `.env.template`
- Use `make security` for gosec, gitleaks, and Docker checks prior to releases
- Use parameterized queries to prevent SQL injection
- Validate all user inputs before processing
- Implement proper rate limiting for API calls
- Use mutex (`sync.RWMutex` or `sync.Mutex`) for shared state in concurrent code
- Test concurrent access with `-race` flag: `go test -race ./...`

### Security Scan Commands
```bash
make security      # Run all security checks

# Individual checks:
gosec ./...                    # Go security scanner
cd ccxt-service && bun audit   # TypeScript dependency audit
gitleaks detect --source .     # Secret detection (requires gitleaks)
```

### Required GitHub Secrets (for CI/CD)
```
# Application secrets:
JWT_SECRET, TELEGRAM_BOT_TOKEN, TELEGRAM_WEBHOOK_URL, TELEGRAM_WEBHOOK_SECRET

# Database & Cache:
POSTGRES_PASSWORD, POSTGRES_USER, POSTGRES_DB, REDIS_URL, DATABASE_URL

# Deployment (SSH):
SSH_PRIVATE_KEY, DEPLOY_HOST, DEPLOY_USER
PRODUCTION_SSH_PRIVATE_KEY, PRODUCTION_SSH_HOST
STAGING_SSH_HOST

# DigitalOcean:
DIGITALOCEAN_ACCESS_TOKEN
```

## Financial Trading System Specific Guidelines

### Precision and Accuracy
```go
// GOOD: Use decimal.Decimal for financial calculations
import "github.com/shopspring/decimal"

type Price struct {
    Value    decimal.Decimal `json:"value"`
    Currency string          `json:"currency"`
}

// Calculate profit
profit := sellPrice.Sub(buyPrice).Mul(quantity)

// BAD: Never use float64 for money (precision loss!)
type BadPrice struct {
    Value    float64 `json:"value"` // ❌ WRONG!
    Currency string  `json:"currency"`
}
```

### Error Handling
```go
// GOOD: Wrap errors with context
func (s *ExchangeService) GetBalance(ctx context.Context, exchange string) (*Balance, error) {
    balance, err := s.client.FetchBalance(ctx)
    if err != nil {
        return nil, fmt.Errorf("failed to fetch balance from %s: %w", exchange, err)
    }
    return balance, nil
}

// Always check errors, never ignore:
if err != nil {
    return fmt.Errorf("operation failed: %w", err)
}
```

### Concurrency Safety
```go
// GOOD: Use mutex for shared state
type OrderBook struct {
    mu    sync.RWMutex
    bids  []Order
    asks  []Order
}

func (ob *OrderBook) GetBestBid() (Order, bool) {
    ob.mu.RLock()
    defer ob.mu.RUnlock()
    
    if len(ob.bids) == 0 {
        return Order{}, false
    }
    return ob.bids[0], true
}

func (ob *OrderBook) UpdateBids(newBids []Order) {
    ob.mu.Lock()
    defer ob.mu.Unlock()
    
    ob.bids = newBids
}
```

## Architecture Patterns

- **Clean Architecture**: Separation of concerns with clear layers (handlers → services → repositories)
- **Microservices**: Go backend + TypeScript CCXT service (communicate via HTTP)
- **Caching Strategy**: Redis for performance optimization (market data, exchange rates)
- **Observability**: OpenTelemetry integration with SigNoz for traces, metrics, logs
- **Circuit Breaker**: Fault tolerance for external service calls (exchanges, APIs)
- **Repository Pattern**: Data access abstraction (database operations in repositories)
- **Dependency Injection**: Flexible component wiring via interfaces

## Code Review Checklist

When reviewing or writing code, ensure:

1. **Financial Precision**: No float64 for monetary values (use `decimal.Decimal`)
2. **Error Handling**: All errors properly wrapped with context using `fmt.Errorf(...: %w, err)`
3. **Security**: No secrets in logs or code, proper input validation
4. **Concurrency**: Proper synchronization for shared state (use mutexes, test with `-race`)
5. **Testing**: Adequate test coverage (≥80%), especially edge cases for financial calculations
6. **Documentation**: Public functions and complex logic documented with godoc comments
7. **Performance**: No obvious performance bottlenecks (N+1 queries, unnecessary allocations)
8. **Rate Limiting**: API calls properly rate limited (exchange APIs have strict limits)
9. **Monitoring**: Important operations logged with context (use structured logging)
10. **Configuration**: No hardcoded values (use environment variables or config files)

## Common Development Workflows

### Adding New Features

```bash
# 1. Create feature branch
git checkout -b feature/my-feature

# 2. Start dev environment
make dev-setup

# 3. Make code changes in internal/ or pkg/

# 4. Format and lint
make fmt
make lint

# 5. Run tests for your package
go test -v ./internal/services/my_new_service_test.go

# 6. Run full test suite
make test

# 7. Build to verify compilation
make build

# 8. Check coverage (optional)
make test-coverage

# 9. Commit changes
git add .
git commit -m "feat: add my new feature"

# 10. Stop dev environment
make dev-down
```

### Database Migrations

```bash
# Check current migration status
make migrate-status

# Run pending migrations
make migrate

# For Docker environment
make migrate-docker

# Create new migration (manual)
# Add file to database/migrations/ with format: YYYYMMDDHHMMSS_description.sql
```

### Adding Go Dependencies

```bash
# Add new dependency
go get github.com/example/package@v1.2.3

# Tidy go.mod and go.sum
go mod tidy

# Verify build and tests still work
make build
make test
```

### Adding TypeScript Dependencies (CCXT service)

```bash
cd ccxt-service

# Add dependency
bun add package-name

# Run tests
bun test

# Build
bun run build
```

## Technology Stack

- **Backend**: Go 1.25+ with Gin web framework
- **Database**: PostgreSQL 15+ with Redis 7+ for caching
- **Market Data**: CCXT via Bun service for exchange integration (100+ exchanges)
- **Testing**: testify (assertions), pgxmock (database mocking), miniredis (Redis mocking)
- **Monitoring**: OpenTelemetry with SigNoz observability stack (traces, metrics, logs)
- **Deployment**: Docker containers on DigitalOcean droplets
- **CI/CD**: GitHub Actions with automated testing, security scans, and deployment

## Quick Reference

**Get Help**: Run `make help` to see all available Makefile targets with descriptions

**Critical File Locations**:
- Main entrypoint: `cmd/server/main.go`
- API handlers: `internal/api/handlers/`
- Business logic (services): `internal/services/`
- Database operations: `internal/database/`
- Configuration: `internal/config/`
- Tests: Co-located with source as `*_test.go`
- Database migrations: `database/migrations/`
- Config templates: `.env.template`, `configs/config.template.yml`
- CI/CD workflows: `.github/workflows/`
- Deployment scripts: `scripts/`

**Essential Documentation**:
- Getting started: `README.md`
- Contributing guide: `CONTRIBUTING.md`
- Configuration: `CONFIGURATION.md`
- CI troubleshooting: `CI_TROUBLESHOOTING.md`
- Manual deployment: `MANUAL_DEPLOYMENT.md`
- Database migrations: `database/README.md`
- Scripts documentation: `scripts/README.md`

**Repository Configuration Files**:
- Go dependencies: `go.mod`, `go.sum`
- CCXT service: `ccxt-service/package.json`, `ccxt-service/bun.lock`
- Docker: `Dockerfile`, `docker-compose.yml`, `docker-compose.*.yml`
- Environment: `.env.template`, `.env.ci`, `.env.example`
- Build automation: `Makefile`

## Important Notes

**TRUST these instructions**: The commands and workflows documented here have been validated and work correctly. Only search for additional information if something is incomplete, incorrect, or appears to have changed. Following these instructions will minimize build failures and ensure consistent development practices across the team.
