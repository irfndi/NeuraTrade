# NeuraTrade - Makefile for development and deployment

# Variables
APP_NAME=neuratrade
GO_VERSION=1.25
VERSION=$(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
GO_CACHE_DIR=$(PWD)/.cache/go-build
GO_MOD_CACHE_DIR=$(PWD)/.cache/go-mod
GO_ENV=GOCACHE=$(GO_CACHE_DIR)

# Colors for output
RED=\033[0;31m
GREEN=\033[0;32m
YELLOW=\033[1;33m
BLUE=\033[0;34m
NC=\033[0m # No Color

.PHONY: help build build-cli build-all test test-coverage coverage-check lint fmt fmt-check run dev install-tools install-cli security clean mod-tidy ci-structure-check ci-naming-check bd-close-qa gateway gateway-start gateway-stop

# Default target
all: build

## Help
help: ## Show this help message
	@echo 'Usage: make [target]'
	@echo ''
	@echo 'Targets:'
	@awk 'BEGIN {FS = ":.*?## "} /^[a-zA-Z_-]+:.*?## / {printf "  $(BLUE)%-20s$(NC) %s\n", $$1, $$2}' $(MAKEFILE_LIST)

go-env-setup:
	@mkdir -p $(GO_CACHE_DIR) $(GO_MOD_CACHE_DIR)

## Development
build: services-setup ## Build the application across all languages
	@echo "$(GREEN)Building $(APP_NAME)...$(NC)"
	@# Build Go application
	cd services/backend-api && go build -o ../../bin/$(APP_NAME) ./cmd/server
	@# Build CLI
	cd cmd/neuratrade-cli && go build -o ../../bin/neuratrade-cli
	@# Build TypeScript/CCXT service
	@if [ -d "services/ccxt-service" ] && command -v bun >/dev/null 2>&1; then \
		echo "$(GREEN)Building CCXT service...$(NC)"; \
		cd services/ccxt-service && bun run build; \
	else \
		echo "$(YELLOW)Skipping CCXT service build - directory or bun not found$(NC)"; \
	fi
	@# Build TypeScript/Telegram service
	@if [ -d "services/telegram-service" ] && command -v bun >/dev/null 2>&1; then \
		echo "$(GREEN)Building Telegram service...$(NC)"; \
		cd services/telegram-service && bun run build; \
	else \
		echo "$(YELLOW)Skipping Telegram service build - directory or bun not found$(NC)"; \
	fi
	@echo "$(GREEN)Build complete!$(NC)"

build-cli: ## Build the NeuraTrade CLI
	@echo "$(GREEN)Building NeuraTrade CLI...$(NC)"
	@mkdir -p bin
	cd cmd/neuratrade-cli && go build -ldflags "-X main.version=$(VERSION) -X main.buildTime=$(shell date -u '+%Y-%m-%d_%H:%M:%S')" -o ../../bin/neuratrade .
	@echo "$(GREEN)CLI built at bin/neuratrade$(NC)"

install-cli: build-cli ## Install the NeuraTrade CLI to /usr/local/bin
	@echo "$(GREEN)Installing NeuraTrade CLI...$(NC)"
	@cp bin/neuratrade /usr/local/bin/neuratrade || (echo "$(RED)Failed to install. Try: sudo make install-cli$(NC)" && exit 1)
	@echo "$(GREEN)CLI installed to /usr/local/bin/neuratrade$(NC)"
	@echo "$(BLUE)Run 'neuratrade gateway start' to start all services$(NC)"

build-all: build build-cli ## Build all components including CLI

test: services-setup ## Run tests across all languages
	@echo "$(GREEN)Running tests across all languages...$(NC)"
	@# Run Go tests
	cd services/backend-api && go test -v ./...
	@# Run TypeScript/JavaScript tests in ccxt-service
	@if [ -d "services/ccxt-service" ] && command -v bun >/dev/null 2>&1; then \
		cd services/ccxt-service && bun test; \
	fi
	@# Run TypeScript/JavaScript tests in telegram-service
	@if [ -d "services/telegram-service" ] && command -v bun >/dev/null 2>&1; then \
		cd services/telegram-service && bun test; \
	fi
	@# Run shell script tests if available
	@if [ -f "services/backend-api/scripts/test.sh" ]; then \
		bash services/backend-api/scripts/test.sh; \
	else \
		true; \
	fi

test-coverage: ## Run tests with coverage report
	@echo "$(GREEN)Running tests with coverage...$(NC)"
	cd services/backend-api && go test -v -coverprofile=../../coverage.out ./cmd/... ./internal/... ./pkg/...
	go tool cover -html=coverage.out -o coverage.html
	@echo "$(GREEN)Coverage report generated: coverage.html$(NC)"

coverage-check: ## Run coverage gate (strict by default)
	@echo "$(GREEN)Running coverage check (threshold $${MIN_COVERAGE:-50}%)...$(NC)"
	MIN_COVERAGE=$${MIN_COVERAGE:-50} \
	STRICT=$${STRICT:-true} \
	bash services/backend-api/scripts/coverage-check.sh

lint: go-env-setup ## Run linter across all languages
	@echo "$(GREEN)Running linter across all languages...$(NC)"
	@# Lint Go code
	cd services/backend-api && $(GO_ENV) golangci-lint run
	@# Lint TypeScript/JavaScript in ccxt-service
	@if [ -d "services/ccxt-service" ] && command -v bun >/dev/null 2>&1; then \
		echo "$(GREEN)Linting TypeScript...$(NC)"; \
		cd services/ccxt-service && bunx oxlint .; \
	else \
		echo "$(YELLOW)Skipping TypeScript linting$(NC)"; \
	fi
	@# Lint TypeScript/JavaScript in telegram-service
	@if [ -d "services/telegram-service" ] && command -v bun >/dev/null 2>&1; then \
		echo "$(GREEN)Linting Telegram service TypeScript...$(NC)"; \
		cd services/telegram-service && bunx oxlint .; \
	else \
		echo "$(YELLOW)Skipping Telegram service linting$(NC)"; \
	fi

typecheck: services-setup ## Run type checking across all languages
	@echo "$(GREEN)Running type checking across all languages...$(NC)"
	@# Type check Go code
	cd services/backend-api && go vet ./...
	@# Type check TypeScript in ccxt-service
	@if [ -d "services/ccxt-service" ] && command -v bun >/dev/null 2>&1; then \
		echo "$(GREEN)Type checking TypeScript...$(NC)"; \
		cd services/ccxt-service && bun tsc --noEmit; \
	else \
		echo "$(YELLOW)Skipping TypeScript type checking$(NC)"; \
	fi
	@# Type check TypeScript in telegram-service
	@if [ -d "services/telegram-service" ] && command -v bun >/dev/null 2>&1; then \
		echo "$(GREEN)Type checking Telegram service TypeScript...$(NC)"; \
		cd services/telegram-service && bun tsc --noEmit; \
	else \
		echo "$(YELLOW)Skipping Telegram service type checking$(NC)"; \
	fi

fmt: ## Format code across all languages
	@echo "$(GREEN)Formatting code across all languages...$(NC)"
	@# Format Go code
	cd services/backend-api && go fmt ./...
	@if command -v goimports >/dev/null 2>&1; then \
		cd services/backend-api && goimports -w .; \
	else \
		echo "$(YELLOW)goimports not found, skipping Go imports formatting$(NC)"; \
	fi
	@# Format TypeScript/JavaScript in ccxt-service
	@if [ -d "services/ccxt-service" ] && command -v bun >/dev/null 2>&1; then \
		echo "$(GREEN)Formatting TypeScript...$(NC)"; \
		cd services/ccxt-service && bunx prettier --write . || bun format --write .; \
	fi
	@# Format TypeScript/JavaScript in telegram-service
	@if [ -d "services/telegram-service" ] && command -v bun >/dev/null 2>&1; then \
		echo "$(GREEN)Formatting Telegram service TypeScript...$(NC)"; \
		cd services/telegram-service && bunx prettier --write . || bun format --write .; \
	fi

run: build ## Run the application (locally, monolithic-style for Go)
	@echo "$(GREEN)Starting $(APP_NAME)...$(NC)"
	./bin/$(APP_NAME)

dev: ## Run with hot reload (requires air)
	@echo "$(GREEN)Starting development server with hot reload...$(NC)"
	cd services/backend-api && air

## Environment Setup
dev-setup: ## Setup development environment (no longer needed - uses launcher.sh)
	@echo "$(GREEN)Use './launcher.sh start' to start all services$(NC)"
	@echo "$(YELLOW)Development environment no longer requires separate setup$(NC)"

dev-down: ## Stop all services
	@echo "$(GREEN)Stopping all services...$(NC)"
	@./launcher.sh stop
	@echo "$(GREEN)All services stopped$(NC)"

install-tools: ## Install development tools
	@echo "$(GREEN)Installing development tools...$(NC)"
	go install github.com/air-verse/air@latest
	go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest
	go install golang.org/x/tools/cmd/goimports@latest
	go install golang.org/x/vuln/cmd/govulncheck@latest
	@echo "$(GREEN)Tools installed!$(NC)"

security-check: ## Run security checks (gosec, gitleaks, govulncheck)
	@echo "$(GREEN)Running comprehensive security checks...$(NC)"
	@# Go security check with gosec
	@echo "$(BLUE)Running gosec security scanner...$(NC)"
	@cd services/backend-api && gosec -fmt sarif -out ../../gosec-report.sarif ./... 2>/dev/null || gosec ./... 2>/dev/null || echo "$(YELLOW)gosec not installed or found issues$(NC)"
	@# Secret scanning with gitleaks
	@echo "$(BLUE)Running gitleaks secret detection...$(NC)"
	@gitleaks detect --source . --verbose --redact 2>/dev/null || echo "$(YELLOW)gitleaks not installed or found potential secrets$(NC)"
	@# Go vulnerability check
	@echo "$(BLUE)Running govulncheck...$(NC)"
	@cd services/backend-api && govulncheck ./... 2>/dev/null || echo "$(YELLOW)govulncheck not installed$(NC)"
	@echo "$(GREEN)Security checks completed!$(NC)"

security-scan: security-check ## Alias for security-check

## Database
db-migrate: ## Run database migrations
	@echo "$(GREEN)Running database migrations...$(NC)"
	@chmod +x services/backend-api/database/migrate.sh
	@./services/backend-api/database/migrate.sh

db-seed: ## Seed database with sample data
	@echo "$(GREEN)Seeding database...$(NC)"
	./bin/$(APP_NAME) seed

## CI/CD
ci-test: services-setup ## Run CI tests with proper environment
	@echo "$(GREEN)Running CI tests...$(NC)"
	cd services/backend-api && go test -v -race -coverprofile=../../coverage.out $$(go list ./... | grep -v -E '(internal/api/handlers/testmocks|internal/observability)')
	@if [ -d "services/ccxt-service" ] && command -v bun >/dev/null 2>&1; then \
		echo "$(GREEN)Running CCXT service tests...$(NC)"; \
		cd services/ccxt-service && bun test; \
	fi
	@if [ -d "services/telegram-service" ] && command -v bun >/dev/null 2>&1; then \
		echo "$(GREEN)Running Telegram service tests...$(NC)"; \
		cd services/telegram-service && bun test; \
	fi

ci-lint: ## Run linter for CI
	@echo "$(GREEN)Running CI linter...$(NC)"
	@# Use ./bin/golangci-lint if available (CI installs there), otherwise use system golangci-lint
	@if [ -f "./bin/golangci-lint" ]; then \
		cd services/backend-api && ../../bin/golangci-lint run --timeout=5m; \
	else \
		cd services/backend-api && golangci-lint run --timeout=5m; \
	fi

ci-structure-check: ## Enforce canonical path guardrails for CI
	@echo "$(GREEN)Running structure/path guardrails...$(NC)"
	bash services/backend-api/scripts/check-legacy-paths.sh

ci-naming-check: ## Enforce canonical naming guardrails for CI
	@echo "$(GREEN)Running naming/import guardrails...$(NC)"
	bash services/backend-api/scripts/check-canonical-naming.sh

## Database Migration Targets
migrate: ## Run all pending database migrations
	@echo "$(GREEN)Running database migrations...$(NC)"
	@cd services/backend-api/database && ./migrate.sh

migrate-status: ## Check database migration status
	@echo "$(GREEN)Checking migration status...$(NC)"
	@cd services/backend-api/database && ./migrate.sh status

migrate-list: ## List available database migrations
	@echo "$(GREEN)Listing available migrations...$(NC)"
	@cd services/backend-api/database && ./migrate.sh list

## Utilities
clean: ## Clean build artifacts
	@echo "$(YELLOW)Cleaning build artifacts...$(NC)"
	rm -rf bin/
	rm -f coverage.out coverage.html
	@echo "$(GREEN)Clean complete!$(NC)"

mod-tidy: ## Tidy Go modules
	@echo "$(GREEN)Tidying Go modules...$(NC)"
	cd services/backend-api && go mod tidy

mod-download: ## Download Go modules
	@echo "$(GREEN)Downloading Go modules...$(NC)"
	cd services/backend-api && go mod download

ccxt-setup: ## Install CCXT service dependencies
	@echo "$(GREEN)Installing CCXT service dependencies...$(NC)"
	@if [ -d "services/ccxt-service" ] && command -v bun >/dev/null 2>&1; then \
		cd services/ccxt-service && bun install; \
	else \
		echo "$(YELLOW)Skipping CCXT setup - directory or bun not found$(NC)"; \
	fi

telegram-setup: ## Install Telegram service dependencies
	@echo "$(GREEN)Installing Telegram service dependencies...$(NC)"
	@if [ -d "services/telegram-service" ] && command -v bun >/dev/null 2>&1; then \
		cd services/telegram-service && bun install; \
	else \
		echo "$(YELLOW)Skipping Telegram setup - directory or bun not found$(NC)"; \
	fi

services-setup: ccxt-setup telegram-setup ## Install all service dependencies
	@echo "$(GREEN)All service dependencies installed!$(NC)"

fmt-check: ## Check if code is formatted (for CI)
	@echo "$(GREEN)Checking code formatting...$(NC)"
	@cd services/backend-api && test -z "$$(gofmt -l .)" || (echo "$(RED)Go code is not formatted. Run 'make fmt'$(NC)" && gofmt -l . && exit 1)
	@echo "$(GREEN)Code formatting check passed!$(NC)"

## Logs
logs: ## Show application logs
	@echo "$(GREEN)Following service logs...$(NC)"
	@./launcher.sh status
	@tail -f ~/.neuratrade/logs/*.log 2>/dev/null || echo "$(YELLOW)No logs found. Services may not be running.$(NC)"

logs-backend: ## Show backend logs
	@tail -f ~/.neuratrade/logs/backend.log 2>/dev/null || echo "$(YELLOW)No backend log found$(NC)"

logs-ccxt: ## Show CCXT service logs
	@tail -f ~/.neuratrade/logs/ccxt.log 2>/dev/null || echo "$(YELLOW)No CCXT log found$(NC)"

logs-telegram: ## Show Telegram service logs
	@tail -f ~/.neuratrade/logs/telegram.log 2>/dev/null || echo "$(YELLOW)No Telegram log found$(NC)"

## Quick Start (One Command)
gateway: gateway-start ## Alias for gateway-start
gateway-start: ## Start NeuraTrade with one command (uses launcher.sh)
	@echo "$(GREEN)Starting NeuraTrade...$(NC)"
	@echo "$(YELLOW)Make sure ~/.neuratrade/.env exists with required configuration$(NC)"
	@if [ ! -f ~/.neuratrade/.env ]; then \
		echo "$(RED)Error: ~/.neuratrade/.env not found!$(NC)"; \
		echo "$(YELLOW)Copy .env.example to ~/.neuratrade/.env and configure it$(NC)"; \
		exit 1; \
	fi
	@./launcher.sh start
	@echo ""
	@echo "$(GREEN)NeuraTrade started!$(NC)"
	@echo "$(YELLOW)Backend API: http://localhost:8080$(NC)"
	@echo "$(YELLOW)CCXT Service: internal only (port 3001)$(NC)"
	@echo "$(YELLOW)Telegram Service: internal only (port 3002)$(NC)"
	@echo ""
	@echo "Health check: curl http://localhost:8080/health"

gateway-stop: ## Stop NeuraTrade
	@echo "$(GREEN)Stopping NeuraTrade...$(NC)"
	@./launcher.sh stop
	@echo "$(GREEN)NeuraTrade stopped!$(NC)"

bd-close-qa: ## Close bd issue with mandatory QA evidence
	@test -n "$${ISSUE_ID:-}" || (echo "ISSUE_ID is required" && exit 1)
	@test -n "$${UNIT_TESTS:-}" || (echo "UNIT_TESTS is required" && exit 1)
	@test -n "$${INTEGRATION_TESTS:-}" || (echo "INTEGRATION_TESTS is required" && exit 1)
	@test -n "$${E2E_TESTS:-}" || (echo "E2E_TESTS is required" && exit 1)
	@test -n "$${COVERAGE_RESULT:-}" || (echo "COVERAGE_RESULT is required" && exit 1)
	@test -n "$${EVIDENCE:-}" || (echo "EVIDENCE is required" && exit 1)
	bash services/backend-api/scripts/bd-close-with-qa.sh "$${ISSUE_ID}" \
		--unit "$${UNIT_TESTS}" \
		--integration "$${INTEGRATION_TESTS}" \
		--e2e "$${E2E_TESTS}" \
		--coverage "$${COVERAGE_RESULT}" \
		--evidence "$${EVIDENCE}"
