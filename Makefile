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

# Quality gates below are shipped repo content: a missing service directory is
# a checkout error and must FAIL, not print a green "Skipping". A missing
# toolchain (bun) is a legitimate skip for LOCAL convenience only: in CI (CI
# set) a missing bun is a hard failure unless ALLOW_SKIP=1 is passed
# explicitly, so a broken runner cannot report green with nothing checked
# (clever-cabin-8xv). Each bun decision sits inside a single if/else recipe
# line — an `exit 0` in one recipe line does NOT stop the following lines.
.PHONY: help all services-setup telegram-setup cli-ts-setup fmt fmt-check lint typecheck \
	test test-cli-ts test-frontend build-ts logs-ts autoresearch-test autoresearch-once \
	autoresearch-loop autoresearch-parallel champion-paper champion-demo champion-soak \
	native-check native-box native-ship

all: typecheck

help: ## Show this help message
	@echo 'Usage: make [target]'
	@echo ''
	@echo 'Targets:'
	@awk 'BEGIN {FS = ":.*?## "} /^[a-zA-Z_-]+:.*?## / {printf "  $(BLUE)%-20s$(NC) %s\n", $$1, $$2}' $(MAKEFILE_LIST)

telegram-setup: ## Install Telegram service dependencies
	@echo "$(GREEN)Installing Telegram service dependencies...$(NC)"
	@if [ ! -d "services/telegram-service" ]; then echo "$(RED)missing services/telegram-service$(NC)" >&2; exit 1; fi
	@if ! command -v bun >/dev/null 2>&1; then \
		echo "$(YELLOW)Skipping Telegram setup - bun not found$(NC)"; \
	else \
		cd services/telegram-service && bun install --frozen-lockfile; \
	fi

cli-ts-setup: ## Install TypeScript CLI dependencies
	@echo "$(GREEN)Installing TypeScript CLI dependencies...$(NC)"
	@if [ ! -d "services/neuratrade-cli-ts" ]; then echo "$(RED)missing services/neuratrade-cli-ts$(NC)" >&2; exit 1; fi
	@if ! command -v bun >/dev/null 2>&1; then \
		echo "$(YELLOW)Skipping TypeScript CLI setup - bun not found$(NC)"; \
	else \
		cd services/neuratrade-cli-ts && bun install --frozen-lockfile; \
	fi

services-setup: telegram-setup cli-ts-setup ## Install service dependencies
	@echo "$(GREEN)Service dependencies ready$(NC)"

build-ts: cli-ts-setup ## Type-check and compile the TypeScript CLI
	@echo "$(GREEN)Building $(APP_NAME) TypeScript CLI...$(NC)"
	@mkdir -p bin
	@if [ ! -d "services/neuratrade-cli-ts" ]; then echo "$(RED)missing services/neuratrade-cli-ts$(NC)" >&2; exit 1; fi
	@if ! command -v bun >/dev/null 2>&1; then \
		if [ -n "$$CI" ] && [ "$$ALLOW_SKIP" != "1" ]; then \
			echo "$(RED)bun not found: build-ts cannot run, refusing a false green (ALLOW_SKIP=1 to skip)$(NC)" >&2; exit 1; \
		fi; \
		echo "$(YELLOW)Skipping TypeScript CLI build - bun not found (no build performed)$(NC)"; \
	else \
		cd services/neuratrade-cli-ts && bunx tsc --noEmit && bun build index.ts --compile --outfile ../../bin/neuratrade-ts || exit $$?; \
		if [ -x ../../bin/neuratrade-ts ]; then echo "$(GREEN)Build complete: bin/neuratrade-ts$(NC)"; \
		else echo "$(RED)Build did not produce bin/neuratrade-ts$(NC)" >&2; exit 1; fi; \
	fi

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
	@if [ ! -d "services/telegram-service" ] || [ ! -d "services/neuratrade-cli-ts" ]; then \
		echo "$(RED)missing service directory (fmt-check requires both)$(NC)" >&2; exit 1; \
	fi; \
	if command -v bun >/dev/null 2>&1; then \
		echo "$(GREEN)Checking Telegram formatting...$(NC)"; \
		(cd services/telegram-service && bunx oxfmt --check .) || exit $$?; \
		echo "$(GREEN)Checking TypeScript CLI formatting...$(NC)"; \
		(cd services/neuratrade-cli-ts && bunx oxfmt --check .) || exit $$?; \
		echo "$(GREEN)Format checks passed!$(NC)"; \
	elif [ -n "$$CI" ] && [ "$$ALLOW_SKIP" != "1" ]; then \
		echo "$(RED)bun not found: fmt-check cannot run, refusing a false green (ALLOW_SKIP=1 to skip)$(NC)" >&2; exit 1; \
	else \
		echo "$(YELLOW)Skipping format checks - bun not found (local skip; CI fails)$(NC)"; \
	fi

lint: ## Run lints
	@if [ ! -d "services/telegram-service" ] || [ ! -d "services/neuratrade-cli-ts" ]; then \
		echo "$(RED)missing service directory (lint requires both)$(NC)" >&2; exit 1; \
	fi; \
	if command -v bun >/dev/null 2>&1; then \
		echo "$(GREEN)Running Telegram lint...$(NC)"; \
		(cd services/telegram-service && bunx oxlint .) || exit $$?; \
		echo "$(GREEN)Running TypeScript CLI lint...$(NC)"; \
		(cd services/neuratrade-cli-ts && bunx oxlint .) || exit $$?; \
	elif [ -n "$$CI" ] && [ "$$ALLOW_SKIP" != "1" ]; then \
		echo "$(RED)bun not found: lint cannot run, refusing a false green (ALLOW_SKIP=1 to skip)$(NC)" >&2; exit 1; \
	else \
		echo "$(YELLOW)Skipping lint - bun not found (local skip; CI fails)$(NC)"; \
	fi

typecheck: ## Run TypeScript type checks
	@if [ ! -d "services/telegram-service" ] || [ ! -d "services/neuratrade-cli-ts" ]; then \
		echo "$(RED)missing service directory (typecheck requires both)$(NC)" >&2; exit 1; \
	fi; \
	if command -v bun >/dev/null 2>&1; then \
		echo "$(GREEN)Running Telegram typecheck...$(NC)"; \
		(cd services/telegram-service && bunx tsc --noEmit) || exit $$?; \
		echo "$(GREEN)Running TypeScript CLI typecheck...$(NC)"; \
		(cd services/neuratrade-cli-ts && bunx tsc --noEmit) || exit $$?; \
	elif [ -n "$$CI" ] && [ "$$ALLOW_SKIP" != "1" ]; then \
		echo "$(RED)bun not found: typecheck cannot run, refusing a false green (ALLOW_SKIP=1 to skip)$(NC)" >&2; exit 1; \
	else \
		echo "$(YELLOW)Skipping typecheck - bun not found (local skip; CI fails)$(NC)"; \
	fi

test: test-cli-ts test-frontend ## Run default tests

test-cli-ts: ## Run TypeScript CLI tests
	@if [ ! -d "services/neuratrade-cli-ts" ]; then echo "$(RED)missing services/neuratrade-cli-ts$(NC)" >&2; exit 1; fi; \
	if command -v bun >/dev/null 2>&1; then \
		echo "$(GREEN)Running TypeScript CLI tests...$(NC)"; \
		(cd services/neuratrade-cli-ts && bun test) || exit $$?; \
	elif [ -n "$$CI" ] && [ "$$ALLOW_SKIP" != "1" ]; then \
		echo "$(RED)bun not found: test-cli-ts cannot run, refusing a false green (ALLOW_SKIP=1 to skip)$(NC)" >&2; exit 1; \
	else \
		echo "$(YELLOW)Skipping TypeScript CLI tests - bun not found (local skip; CI fails)$(NC)"; \
	fi

test-frontend: ## Run Telegram service tests
	@if [ ! -d "services/telegram-service" ]; then echo "$(RED)missing services/telegram-service$(NC)" >&2; exit 1; fi; \
	if command -v bun >/dev/null 2>&1; then \
		echo "$(GREEN)Running Telegram tests...$(NC)"; \
		(cd services/telegram-service && bun test) || exit $$?; \
	elif [ -n "$$CI" ] && [ "$$ALLOW_SKIP" != "1" ]; then \
		echo "$(RED)bun not found: test-frontend cannot run, refusing a false green (ALLOW_SKIP=1 to skip)$(NC)" >&2; exit 1; \
	else \
		echo "$(YELLOW)Skipping frontend tests - bun not found (local skip; CI fails)$(NC)"; \
	fi

logs-ts: ## Show TS gateway logs from NEURATRADE_HOME
	@log="$${NEURATRADE_HOME:-$$HOME/.neuratrade}/logs/gateway.log"; \
		[ -f "$$log" ] || { echo "no gateway log at $$log" >&2; exit 1; }; \
		tail -f "$$log"

autoresearch-test: ## Unit tests for autoresearch keep/discard + guards
	@cd services/neuratrade-cli-ts && bun test autoresearch/

autoresearch-once: ## Evaluate current autoresearch knobs once (confirm phase)
	@cd services/neuratrade-cli-ts && bun run autoresearch/run-once.ts --budget-sec=$${BUDGET_SEC:-180} --symbols=$${SYMBOLS:-8} --steps=$${STEPS:-40}

autoresearch-loop: ## Single-worker screen→confirm loop until goals claim
	@cd services/neuratrade-cli-ts && bun run autoresearch/loop.ts --trials=$${TRIALS:-500} --symbols=$${SYMBOLS:-8} --screen-steps=$${SCREEN_STEPS:-12} --screen-budget-sec=$${SCREEN_BUDGET:-45} --confirm-steps=$${CONFIRM_STEPS:-40} --confirm-budget-sec=$${CONFIRM_BUDGET:-180}

autoresearch-parallel: ## Start 4 parallel autoresearch workers via pm2
	@cd services/neuratrade-cli-ts && pm2 start ecosystem.autoresearch.config.cjs

champion-paper: ## Paper-soak claimed champion knobs (simulated, isolated HOME)
	@cd services/neuratrade-cli-ts && pm2 start ecosystem.champion-soak.config.cjs --only neuratrade-champion-paper

champion-demo: ## Bybit testnet soak of claimed champion knobs
	@cd services/neuratrade-cli-ts && pm2 start ecosystem.champion-soak.config.cjs --only neuratrade-champion-demo

champion-soak: ## Paper + Bybit testnet champion soaks
	@cd services/neuratrade-cli-ts && pm2 start ecosystem.champion-soak.config.cjs

native-check: ## Rust fmt + clippy (zero warnings) + all 8 example checks
	@cd crates && cargo fmt --check
	@cd crates && cargo fetch
	@cd crates && cargo clippy --all-targets -- -D warnings
	@cd crates && cargo build
	@cd crates && for p in nt-risk nt-execution nt-ledger nt-market nt-grid; do cargo run -p $$p --example check || exit 1; done
	@cd crates && cargo run -p nt-grid --example paper_engine_check
	@cd crates && cargo run -p nt-grid --example sleeves_check
	@cd crates && cargo run -p nt-grid --example pump_catch

native-box: ## Cross-build the box binary via zigbuild (release)
	@./scripts/zig/zig-build.sh release box

native-ship: ## Print the box deploy recipe (set BOX_HOST; prints only)
	@echo "scp crates/target/x86_64-unknown-linux-gnu/release/nt-cli $${BOX_HOST:?set BOX_HOST}:/opt/neuratrade/bin/nt-cli"
	@echo "ssh $${BOX_HOST:?set BOX_HOST} 'pm2 restart nt-cli'"
