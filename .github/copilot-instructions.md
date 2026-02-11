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
- `database/` - Database migrations and seeding scripts
- `scripts/` - Deployment, migration, and utility scripts

## Build, Test, and Development Commands

```bash
# Build and run
make build          # Compile Go API to bin/neuratrade
make run            # Build then run locally
make dev            # Hot reload via air; requires go install github.com/air-verse/air@latest

# Testing
make test           # Run Go unit tests and Bun tests (if Bun installed)
make test-coverage  # Run tests with coverage report
make coverage-check # Run non-blocking coverage gate (default 80% threshold)

# Code quality
make lint           # Run golangci-lint
make typecheck      # Run go vet
make fmt            # Format code with gofmt/goimports

# Environment setup
make dev-setup      # Start Postgres + Redis via Docker Compose
make dev-down       # Stop development environment
```

## Coding Style & Naming Conventions

### Go Code
- Target Go 1.25; format with `gofmt`/`goimports` (tabs, canonical imports)
- Use idiomatic Go naming:
  - Exported identifiers in `CamelCase`
  - Packages lowercase, short, descriptive
  - Interfaces with `-er` suffix when applicable (e.g., `Trader`, `Exchanger`)
- Keep functions under 50 lines when possible
- Use `decimal.Decimal` for financial calculations; never use `float64` for monetary values

### TypeScript/JavaScript (CCXT Service)
- Rely on `bunx` formatters (`bun run format`)
- Keep strict lint output clean

### General Principles
- Prefer short, descriptive directory and file names
- Keep shared interfaces in `pkg/`
- Separate business logic from infrastructure concerns

## Testing Guidelines

- Use table-driven tests for Go packages
- Place mocks in `internal/testutil` or `test/testmocks`
- Run `make test` before pushing
- For targeted Go packages: `go test ./internal/services`
- Coverage expectation: ≥80% on touched packages
- Name integration tests under `test/integration`
- Skip external dependencies unless orchestrated in CI
- Test financial calculations with precision edge cases

## Commit & Pull Request Guidelines

- Follow Conventional Commits: `feat:`, `fix:`, `refactor:`, etc.
- Keep subjects imperative and ≤72 characters
- Reference issues in commit bodies or PR descriptions
- PRs must include:
  - Summary of changes
  - Rationale
  - Test evidence (`make test` output)
  - Updated docs/config as needed
- Ensure CI passes before requesting review (build, lint, tests, Trivy scan)

## Security Guidelines

### Critical Rules - Never Do These
- Never commit secrets or API keys to the repository
- Never log sensitive data (API keys, secrets, credentials)
- Never use `float64` for monetary values
- Never hardcode configuration values; use environment variables

### Required Practices
- Derive local env files from `.env.template`
- Use `make security` for gosec, gitleaks, and Docker checks prior to releases
- Gate external webhooks via provided scripts
- Use parameterized queries to prevent SQL injection
- Validate all user inputs
- Implement proper rate limiting for API calls
- Use mutex for shared state in concurrent code

## Financial Trading System Specific Guidelines

### Precision and Accuracy
```go
// GOOD: Use decimal.Decimal for financial calculations
import "github.com/shopspring/decimal"

type Price struct {
    Value    decimal.Decimal `json:"value"`
    Currency string          `json:"currency"`
}

// BAD: Never use float64 for money
type BadPrice struct {
    Value    float64 `json:"value"` // Precision loss!
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
```

## Architecture Patterns

- **Clean Architecture**: Separation of concerns with clear layers
- **Microservices**: Go backend + TypeScript CCXT service
- **Caching Strategy**: Redis for performance optimization
- **Observability**: OpenTelemetry integration with SigNoz
- **Circuit Breaker**: Fault tolerance for external services
- **Repository Pattern**: Data access abstraction
- **Dependency Injection**: Flexible component wiring

## Code Review Checklist

When reviewing or writing code, ensure:

1. **Financial Precision**: No float64 for monetary values
2. **Error Handling**: All errors properly wrapped and handled
3. **Security**: No secrets in logs, proper input validation
4. **Concurrency**: Proper synchronization for shared state
5. **Testing**: Adequate test coverage, especially edge cases
6. **Documentation**: Public functions and complex logic documented
7. **Performance**: No obvious performance bottlenecks
8. **Rate Limiting**: API calls properly rate limited
9. **Monitoring**: Important operations logged with context
10. **Configuration**: No hardcoded values

## Technology Stack

- **Backend**: Go 1.25+ with Gin web framework
- **Database**: PostgreSQL 15+ with Redis 7+ for caching
- **Market Data**: CCXT via Bun service for exchange integration
- **Testing**: testify, pgxmock, miniredis
- **Monitoring**: OpenTelemetry with SigNoz observability stack
- **Deployment**: Docker containers on Digital Ocean
- **CI/CD**: GitHub Actions with automated deployment
