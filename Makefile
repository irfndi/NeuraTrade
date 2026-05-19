# NeuraTrade - Native-first Makefile (no Docker)

APP_NAME=neuratrade
VERSION=$(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
GO_CACHE_DIR=$(PWD)/.cache/go-build
GO_MOD_CACHE_DIR=$(PWD)/.cache/go-mod
GO_ENV=GOCACHE=$(GO_CACHE_DIR) GOMODCACHE=$(GO_MOD_CACHE_DIR)

RED=\033[0;31m
GREEN=\033[0;32m
YELLOW=\033[1;33m
BLUE=\033[0;34m
NC=\033[0m

.PHONY: help all go-env-setup proto-gen mod-download build services-setup telegram-setup \
	test test-backend test-frontend lint fmt fmt-check typecheck coverage-check \
	test-scripts run logs logs-all scalping-soak ai-scalping-probe bd-close-qa

all: build

help: ## Show this help message
	@echo 'Usage: make [target]'
	@echo ''
	@echo 'Targets:'
	@awk 'BEGIN {FS = ":.*?## "} /^[a-zA-Z_-]+:.*?## / {printf "  $(BLUE)%-20s$(NC) %s\n", $$1, $$2}' $(MAKEFILE_LIST)

go-env-setup: ## Create local Go cache directories
	@mkdir -p $(GO_CACHE_DIR) $(GO_MOD_CACHE_DIR)

proto-gen: ## Generate protobuf/gRPC code (requires protoc + plugins)
	@echo "$(GREEN)Generating gRPC code...$(NC)"
	@chmod +x scripts/gen-proto.sh
	@./scripts/gen-proto.sh
	@echo "$(GREEN)gRPC code generated!$(NC)"

mod-download: go-env-setup ## Download backend dependencies
	@echo "$(GREEN)Downloading Go module dependencies...$(NC)"
	@cd services/backend-api && $(GO_ENV) go mod download

telegram-setup: ## Install Telegram service dependencies
	@echo "$(GREEN)Installing Telegram service dependencies...$(NC)"
	@if [ -d "services/telegram-service" ] && command -v bun >/dev/null 2>&1; then \
		cd services/telegram-service && bun install --frozen-lockfile; \
	else \
		echo "$(YELLOW)Skipping Telegram setup - directory or bun not found$(NC)"; \
	fi

services-setup: telegram-setup ## Install service dependencies
	@echo "$(GREEN)Service dependencies ready$(NC)"

build: mod-download services-setup ## Build backend binaries
	@echo "$(GREEN)Building $(APP_NAME)...$(NC)"
	@mkdir -p bin
	@cd services/backend-api && $(GO_ENV) go build -o ../../bin/neuratrade-server ./cmd/server
	@cd services/backend-api && $(GO_ENV) go build -o ../../bin/neuratrade-scalping-soak ./cmd/scalping-soak
	@cd cmd/neuratrade-cli && $(GO_ENV) go build -o ../../bin/neuratrade .
	@printf '%s\n' '#!/usr/bin/env bash' \
		'# CCXT Service Stub' \
		'echo "[CCXT Service] Native CCXT implementation is running within neuratrade-server"' \
		'trap "exit 0" SIGTERM SIGINT' \
		'while true; do sleep 60; done' > bin/ccxt-service
	@chmod +x bin/ccxt-service
	@printf '%s\n' '#!/usr/bin/env bash' \
		'SCRIPT_DIR="$$(cd "$$(dirname "$${BASH_SOURCE[0]}")" && pwd)"' \
		'cd "$$SCRIPT_DIR/../services/telegram-service"' \
		'exec bun run index.ts "$$@"' > bin/telegram-service
	@chmod +x bin/telegram-service
	@echo "$(GREEN)Build complete: bin/neuratrade-server, bin/neuratrade, bin/neuratrade-scalping-soak$(NC)"

fmt: ## Format backend + frontend code
	@echo "$(GREEN)Formatting Go code...$(NC)"
	@cd services/backend-api && gofmt -w .
	@if [ -d "services/telegram-service" ] && command -v bun >/dev/null 2>&1; then \
		echo "$(GREEN)Formatting Telegram service...$(NC)"; \
		cd services/telegram-service && bunx prettier --write .; \
	fi

fmt-check: ## Check if code is formatted (for CI)
	@echo "$(GREEN)Checking Go formatting...$(NC)"
	@cd services/backend-api && test -z "$$(gofmt -l .)" || (echo "$(RED)Go code is not formatted. Run 'make fmt'$(NC)" && gofmt -l . && exit 1)
	@if [ -d "services/telegram-service" ] && command -v bun >/dev/null 2>&1; then \
		echo "$(GREEN)Checking Telegram formatting...$(NC)"; \
		cd services/telegram-service && bunx prettier --check .; \
	fi
	@echo "$(GREEN)Format checks passed!$(NC)"

lint: ## Run lints
	@echo "$(GREEN)Running Go lint...$(NC)"
	@cd services/backend-api && golangci-lint run
	@if [ -d "services/telegram-service" ] && command -v bun >/dev/null 2>&1; then \
		echo "$(GREEN)Running Telegram lint...$(NC)"; \
		cd services/telegram-service && bunx oxlint .; \
	fi

typecheck: ## Run TypeScript type checks
	@if [ -d "services/telegram-service" ] && command -v bun >/dev/null 2>&1; then \
		echo "$(GREEN)Running Telegram typecheck...$(NC)"; \
		cd services/telegram-service && bunx @typescript/native-preview; \
	else \
		echo "$(YELLOW)Skipping typecheck - telegram-service or bun missing$(NC)"; \
	fi

test: test-backend test-scripts ## Run default tests

test-backend: mod-download ## Run backend tests
	@echo "$(GREEN)Running backend tests...$(NC)"
	@TEST_HOME=$$(mktemp -d /tmp/neuratrade-ci-home.XXXXXX); \
		cd services/backend-api && \
		NEURATRADE_HOME="$$TEST_HOME" \
		ADMIN_API_KEY= \
		DATABASE_DRIVER=$${DATABASE_DRIVER:-sqlite} \
		SQLITE_PATH=$${SQLITE_PATH:-/tmp/neuratrade-ci.db} \
		SQLITE_DB_PATH=$${SQLITE_PATH:-/tmp/neuratrade-ci.db} \
		go test -v -race -timeout=20m ./cmd/... ./internal/... ./pkg/... ./test/integration/... ./test/e2e/; \
		rm -rf "$$TEST_HOME"

test-frontend: ## Run Telegram service tests
	@if [ -d "services/telegram-service" ] && command -v bun >/dev/null 2>&1; then \
		echo "$(GREEN)Running Telegram tests...$(NC)"; \
		cd services/telegram-service && bun test; \
	else \
		echo "$(YELLOW)Skipping frontend tests - telegram-service or bun missing$(NC)"; \
	fi

test-scripts: ## Run operational script tests
	@echo "$(GREEN)Running script tests...$(NC)"
	@bash services/backend-api/scripts/startup-orchestrator_test.sh
	@bash services/backend-api/scripts/scalping-soak_test.sh
	@bash services/backend-api/scripts/verify-scalping-soak-artifact_test.sh
	@bash services/backend-api/scripts/scalping-soak-acceptance_test.sh
	@bash services/backend-api/scripts/validate-scalping-rule-candidate_test.sh

coverage-check: ## Run coverage threshold checks
	@echo "$(GREEN)Running coverage checks...$(NC)"
	@cd services/backend-api && STRICT=$${STRICT:-false} ./scripts/coverage-check.sh

run: build ## Start NeuraTrade gateway in native mode
	@echo "$(GREEN)Starting NeuraTrade gateway...$(NC)"
	@./bin/neuratrade gateway start

logs: ## Show backend logs from NEURATRADE_HOME
	@tail -f $${NEURATRADE_HOME:-$$HOME/.neuratrade}/logs/backend.log

logs-all: ## Show gateway logs from NEURATRADE_HOME
	@tail -f $${NEURATRADE_HOME:-$$HOME/.neuratrade}/logs/gateway.log

scalping-soak: build ## Run no-order public-data scalping paper soak
	@bash services/backend-api/scripts/scalping-soak.sh run

ai-scalping-probe: build ## Run real LLM no-order scalping probe with recovery gates
	@bash services/backend-api/scripts/ai-scalping-probe.sh run

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
