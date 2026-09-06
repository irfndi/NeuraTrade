# NeuraTrade - TypeScript-first Makefile
# The Go backend was removed 2026-08-10 (archived at tag backup/go-backend-2026-08-10).
# This file now covers only the TS services: services/neuratrade-cli-ts and
# services/telegram-service.

APP_NAME=neuratrade

RED=\033[0;31m
GREEN=\033[0;32m
YELLOW=\033[1;33m
BLUE=\033[0;34m
NC=\033[0m

.PHONY: help services-setup telegram-setup cli-ts-setup fmt fmt-check lint typecheck \
	test test-cli-ts test-frontend build-ts logs-ts autoresearch-test autoresearch-once \
	autoresearch-loop autoresearch-parallel

all: typecheck

help: ## Show this help message
	@echo 'Usage: make [target]'
	@echo ''
	@echo 'Targets:'
	@awk 'BEGIN {FS = ":.*?## "} /^[a-zA-Z_-]+:.*?## / {printf "  $(BLUE)%-20s$(NC) %s\n", $$1, $$2}' $(MAKEFILE_LIST)

telegram-setup: ## Install Telegram service dependencies
	@echo "$(GREEN)Installing Telegram service dependencies...$(NC)"
	@if [ -d "services/telegram-service" ] && command -v bun >/dev/null 2>&1; then \
		cd services/telegram-service && bun install --frozen-lockfile; \
	else \
		echo "$(YELLOW)Skipping Telegram setup - directory or bun not found$(NC)"; \
	fi

cli-ts-setup: ## Install TypeScript CLI dependencies
	@echo "$(GREEN)Installing TypeScript CLI dependencies...$(NC)"
	@if [ -d "services/neuratrade-cli-ts" ] && command -v bun >/dev/null 2>&1; then \
		cd services/neuratrade-cli-ts && bun install --frozen-lockfile; \
	else \
		echo "$(YELLOW)Skipping TypeScript CLI setup - directory or bun not found$(NC)"; \
	fi

services-setup: telegram-setup cli-ts-setup ## Install service dependencies
	@echo "$(GREEN)Service dependencies ready$(NC)"

build-ts: cli-ts-setup ## Type-check and compile the TypeScript CLI
	@echo "$(GREEN)Building $(APP_NAME) TypeScript CLI...$(NC)"
	@mkdir -p bin
	@if [ -d "services/neuratrade-cli-ts" ] && command -v bun >/dev/null 2>&1; then \
		cd services/neuratrade-cli-ts && bunx tsc --noEmit && bun build index.ts --compile --outfile ../../bin/neuratrade-ts; \
	else \
		echo "$(YELLOW)Skipping TypeScript CLI build - directory or bun not found$(NC)"; \
	fi
	@echo "$(GREEN)Build complete: bin/neuratrade-ts$(NC)"

fmt: ## Format TS code
	@if [ -d "services/telegram-service" ] && command -v bun >/dev/null 2>&1; then \
		echo "$(GREEN)Formatting Telegram service...$(NC)"; \
		cd services/telegram-service && bunx oxfmt; \
	fi
	@if [ -d "services/neuratrade-cli-ts" ] && command -v bun >/dev/null 2>&1; then \
		echo "$(GREEN)Formatting TypeScript CLI...$(NC)"; \
		cd services/neuratrade-cli-ts && bunx oxfmt; \
	fi

fmt-check: ## Check if code is formatted (for CI)
	@if [ -d "services/telegram-service" ] && command -v bun >/dev/null 2>&1; then \
		echo "$(GREEN)Checking Telegram formatting...$(NC)"; \
		cd services/telegram-service && bunx oxfmt --check .; \
	fi
	@if [ -d "services/neuratrade-cli-ts" ] && command -v bun >/dev/null 2>&1; then \
		echo "$(GREEN)Checking TypeScript CLI formatting...$(NC)"; \
		cd services/neuratrade-cli-ts && bunx oxfmt --check .; \
	fi
	@echo "$(GREEN)Format checks passed!$(NC)"

lint: ## Run lints
	@if [ -d "services/telegram-service" ] && command -v bun >/dev/null 2>&1; then \
		echo "$(GREEN)Running Telegram lint...$(NC)"; \
		cd services/telegram-service && bunx oxlint .; \
	fi
	@if [ -d "services/neuratrade-cli-ts" ] && command -v bun >/dev/null 2>&1; then \
		echo "$(GREEN)Running TypeScript CLI lint...$(NC)"; \
		cd services/neuratrade-cli-ts && bunx oxlint .; \
	fi

typecheck: ## Run TypeScript type checks
	@if [ -d "services/telegram-service" ] && command -v bun >/dev/null 2>&1; then \
		echo "$(GREEN)Running Telegram typecheck...$(NC)"; \
		cd services/telegram-service && bunx tsc --noEmit; \
	else \
		echo "$(YELLOW)Skipping Telegram typecheck - telegram-service or bun missing$(NC)"; \
	fi
	@if [ -d "services/neuratrade-cli-ts" ] && command -v bun >/dev/null 2>&1; then \
		echo "$(GREEN)Running TypeScript CLI typecheck...$(NC)"; \
		cd services/neuratrade-cli-ts && bunx tsc --noEmit; \
	else \
		echo "$(YELLOW)Skipping TypeScript CLI typecheck - directory or bun missing$(NC)"; \
	fi

test: test-cli-ts test-frontend ## Run default tests

test-cli-ts: ## Run TypeScript CLI tests
	@if [ -d "services/neuratrade-cli-ts" ] && command -v bun >/dev/null 2>&1; then \
		echo "$(GREEN)Running TypeScript CLI tests...$(NC)"; \
		cd services/neuratrade-cli-ts && bun test; \
	else \
		echo "$(YELLOW)Skipping TypeScript CLI tests - directory or bun missing$(NC)"; \
	fi

test-frontend: ## Run Telegram service tests
	@if [ -d "services/telegram-service" ] && command -v bun >/dev/null 2>&1; then \
		echo "$(GREEN)Running Telegram tests...$(NC)"; \
		cd services/telegram-service && bun test; \
	else \
		echo "$(YELLOW)Skipping frontend tests - telegram-service or bun missing$(NC)"; \
	fi

logs-ts: ## Show TS gateway logs from NEURATRADE_HOME
	@tail -f $${NEURATRADE_HOME:-$$HOME/.neuratrade}/logs/gateway.log

autoresearch-test: ## Unit tests for autoresearch keep/discard + guards
	@cd services/neuratrade-cli-ts && bun test autoresearch/

autoresearch-once: ## Evaluate current autoresearch knobs once (confirm phase)
	@cd services/neuratrade-cli-ts && bun run autoresearch/run-once.ts --budget-sec=$${BUDGET_SEC:-180} --symbols=$${SYMBOLS:-8} --steps=$${STEPS:-40}

autoresearch-loop: ## Single-worker screen→confirm loop until goals claim
	@cd services/neuratrade-cli-ts && bun run autoresearch/loop.ts --trials=$${TRIALS:-500} --symbols=$${SYMBOLS:-8} --screen-steps=$${SCREEN_STEPS:-12} --screen-budget-sec=$${SCREEN_BUDGET:-45} --confirm-steps=$${CONFIRM_STEPS:-40} --confirm-budget-sec=$${CONFIRM_BUDGET:-180}

autoresearch-parallel: ## Start 4 parallel autoresearch workers via pm2
	@cd services/neuratrade-cli-ts && pm2 start ecosystem.autoresearch.config.cjs
