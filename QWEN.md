# NeuraTrade - Project Context

**Generated:** 2026-02-17  
**Project Type:** Cryptocurrency Arbitrage & Technical Analysis Platform  
**Primary Language:** Go 1.25+ | **Secondary:** TypeScript (Bun runtime)

---

## Project Overview

NeuraTrade is a high-performance, multi-service platform for real-time cryptocurrency arbitrage detection and advanced technical analysis. The system processes market data from 100+ exchanges to identify profitable trading opportunities.

### Core Capabilities
- **Spot & Futures Arbitrage:** Detects price discrepancies and funding rate differentials
- **Technical Analysis:** Real-time indicators (RSI, MACD, Bollinger Bands)
- **Signal Aggregation:** Combines arbitrage + technical signals into trade recommendations
- **Risk Management:** Exchange reliability, liquidity, and volatility assessment
- **Multi-Channel Notifications:** Telegram bot and Webhook alerts

---

## Architecture

### Service Structure (Monorepo)
```
NeuraTrade/
├── services/
│   ├── backend-api/          # Go backend - main business logic
│   │   ├── cmd/server/       # Application entry point
│   │   ├── internal/         # Core modules (api, services, models, etc.)
│   │   ├── pkg/              # Shared libraries
│   │   ├── database/         # PostgreSQL migrations
│   │   └── scripts/          # Operational scripts
│   ├── ccxt-service/         # TypeScript/Bun - CCXT exchange bridge
│   └── telegram-service/     # TypeScript/Bun - Telegram bot (grammY)
├── protos/                   # Protocol Buffer definitions (gRPC)
├── cmd/
│   └── neuratrade-cli/       # CLI tool for service management
├── dev/                      # Local development Docker Compose
└── docs/                     # Documentation
```

### Runtime Architecture
1. **Go Backend:** Central orchestrator handling business logic, API, and service coordination
2. **CCXT Service:** Sidecar providing unified HTTP/gRPC API for 100+ cryptocurrency exchanges
3. **Telegram Service:** Notification delivery via grammY (polling or webhook mode)
4. **Data Layer:** PostgreSQL 15+ (persistence) + Redis 7+ (caching, pub/sub)

---

## Building and Running

### Prerequisites
- **Go:** 1.25+
- **Bun:** 1.0+ (TypeScript services)
- **Docker & Docker Compose**
- **PostgreSQL:** 15+
- **Redis:** 7+

### Quick Start

#### Option 1: NeuraTrade CLI (Recommended)
```bash
# Install CLI
make install-cli

# Start all services
neuratrade gateway start          # Docker mode
neuratrade gateway start --native # Native mode

# Manage services
neuratrade gateway status
neuratrade gateway logs -f
neuratrade gateway stop
```

#### Option 2: Make Commands
```bash
# Setup environment (PostgreSQL, Redis via Docker)
make dev-setup

# Run Go backend locally
DATABASE_HOST=localhost REDIS_HOST=localhost make run

# Or run full Docker stack
make dev-up-orchestrated
```

### Key Make Targets
| Command | Description |
|---------|-------------|
| `make build` | Build Go app + TypeScript services |
| `make run` | Run compiled binary |
| `make dev` | Hot reload with air |
| `make test` | Run all tests (Go + TypeScript) |
| `make lint` | Run golangci-lint + oxlint |
| `make typecheck` | Run `go vet` + `tsc --noEmit` |
| `make fmt` | Format all code |
| `make coverage-check` | Run coverage gate (default 50%) |
| `make security-check` | gosec + gitleaks + govulncheck |
| `make dev-setup` | Start local PostgreSQL/Redis |
| `make dev-down` | Stop local services |
| `make migrate` | Run database migrations |

### Environment Configuration
```bash
# Copy and configure
cp .env.example .env

# Key variables to set:
# - DATABASE_PASSWORD (or DATABASE_URL for managed DB)
# - TELEGRAM_BOT_TOKEN (from @BotFather)
# - JWT_SECRET (min 32 chars)
# - ADMIN_API_KEY (for securing admin endpoints)
# - CCXT_SERVICE_URL (default: http://localhost:3001)
```

---

## Development Conventions

### Code Style
- **Go:** Standard `gofmt` formatting + `goimports` for imports
- **TypeScript:** Prettier formatting in Bun services
- **Linting:** Strict golangci-lint config with test exclusions

### Testing Practices
- **Go Tests:** `go test -v ./...` in `services/backend-api/`
- **TypeScript Tests:** `bun test` in each service
- **Coverage:** Minimum 50% threshold (configurable via `MIN_COVERAGE`)
- **CI Pipeline:** Format → Lint → Test → Build → Security

### Project Structure Conventions
```
services/backend-api/
├── cmd/           # Application entry points
├── internal/      # Private application code
│   ├── api/       # HTTP handlers, routing
│   ├── services/  # Business logic (Arbitrage, Analysis, etc.)
│   ├── models/    # Database structs, domain objects
│   ├── database/  # SQL connection, queries
│   └── ...        # Other domain modules
└── pkg/           # Public libraries (importable)
```

### Anti-Patterns to Avoid
- ❌ Editing generated protobuf artifacts (`*.pb.go`, `*.ts`) - regenerate instead
- ❌ Adding handlers to legacy `internal/handlers/` - use `internal/api/handlers/`
- ❌ Using float primitives for money math - use `decimal.Decimal`
- ❌ Committing `.env` files or secrets

### Git Workflow
```bash
# Before committing
make fmt
make lint
make test

# Commit style: clear, concise, focused on "why"
git commit -m "Fix race condition in arbitrage detection"
```

---

## API Endpoints

| Endpoint | Description |
|----------|-------------|
| `/api/market/...` | Market data (tickers, orderbooks) |
| `/api/arbitrage/opportunities` | Spot arbitrage opportunities |
| `/api/futures/opportunities` | Funding rate arbitrage |
| `/api/analysis/signals` | Aggregated trading signals |
| `/health` | Health check |

---

## Key Technologies

### Backend (Go)
- **Framework:** Gin
- **Database:** pgx (PostgreSQL driver)
- **Cache:** go-redis
- **Logging:** zap
- **Monitoring:** Sentry
- **Technical Analysis:** goflux (TA-Lib wrapper)

### Sidecar Services (TypeScript/Bun)
- **Runtime:** Bun
- **Exchange Integration:** CCXT
- **Telegram Bot:** grammY
- **RPC:** gRPC + protobuf

### Infrastructure
- **Container Orchestration:** Docker Compose
- **Database:** PostgreSQL 15+
- **Cache/PubSub:** Redis 7+
- **CI/CD:** GitHub Actions

---

## Troubleshooting

### Common Issues

**CCXT Service Connection Failed**
```bash
# Ensure service is running
docker compose ps ccxt-service

# Check logs
docker compose logs ccxt-service
```

**Database Migration Errors**
```bash
# Check migration status
make migrate-status

# Run migrations manually
make migrate
```

**Telegram Bot Not Responding**
```bash
# Verify TELEGRAM_BOT_TOKEN is set
# Ensure TELEGRAM_EXTERNAL_SERVICE=true
# Check service logs
docker compose logs telegram-service
```

**Coverage Check Failing**
```bash
# View coverage report
make test-coverage

# Adjust threshold (temporary)
MIN_COVERAGE=40 make coverage-check
```

---

## Documentation References

- `README.md` - Full project documentation
- `AGENTS.md` - AI agent knowledge base
- `TEST_PLAN.md` - Testing procedures
- `docs/` - Architecture and operational guides
- `services/*/AGENTS.md` - Service-specific context

---

## Commands Quick Reference

```bash
# Development
make dev-setup          # Start PostgreSQL + Redis
make run                # Run backend locally
make dev                # Hot reload mode

# Testing
make test               # Run all tests
make test-coverage      # Generate coverage report
make coverage-check     # Enforce coverage threshold

# Code Quality
make fmt                # Format code
make lint               # Run linters
make typecheck          # Type checking
make security-check     # Security scanning

# Deployment
make build              # Build all services
make docker-build       # Build Docker images
make migrate            # Run DB migrations
```
