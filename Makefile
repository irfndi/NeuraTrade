# NeuraTrade - Makefile for development and deployment

# Variables
APP_NAME=neuratrade
GO_VERSION=1.25
DOCKER_REGISTRY=ghcr.io/irfndi
DOCKER_IMAGE_APP=$(DOCKER_REGISTRY)/app:latest
DOCKER_COMPOSE_FILE?=docker-compose.yaml
DOCKER_COMPOSE_ENV_FILE=.env
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

.PHONY: help build test test-coverage coverage-check lint fmt fmt-check run dev dev-setup dev-down install-tools security docker-build docker-run deploy clean dev-up-orchestrated prod-up-orchestrated webhook-enable webhook-disable webhook-status startup-status down-orchestrated go-env-setup # ccxt-setup (removed) telegram-setup services-setup mod-download mod-tidy ci-structure-check ci-naming-check bd-close-qa

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
proto-gen: ## Generate gRPC code
	@echo "$(GREEN)Generating gRPC code...$(NC)"
	@docker build -t proto-builder -f tools/proto-builder/Dockerfile .
	@chmod +x scripts/gen-proto.sh
	@docker run --rm -v $(PWD):/workspace proto-builder ./scripts/gen-proto.sh
	@echo "$(GREEN)gRPC code generated!$(NC)"


mod-download: ## Download Go module dependencies
	@echo "$(GREEN)Downloading Go module dependencies...$(NC)"
	@cd services/backend-api && go mod download

build: services-setup ## Build the application across all languages
	@echo "$(GREEN)Building $(APP_NAME)...$(NC)"
	# CCXT Service removed - migrated to Go
# 	@if [ -d "services/ccxt-service" ] && command -v bun >/dev/null 2>&1; then \
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

services-setup: # ccxt-setup (removed) telegram-setup ## Install all service dependencies
	@echo "$(GREEN)All service dependencies installed!$(NC)"

fmt-check: ## Check if code is formatted (for CI)
	@echo "$(GREEN)Checking code formatting...$(NC)"
	@cd services/backend-api && test -z "$$(gofmt -l .)" || (echo "$(RED)Go code is not formatted. Run 'make fmt'$(NC)" && gofmt -l . && exit 1)
	@echo "$(GREEN)Code formatting check passed!$(NC)"

## Logs
logs: ## Show application logs
	docker compose -f $(DOCKER_COMPOSE_FILE) --env-file .env logs -f

logs-all: ## Show all service logs
	docker compose --env-file .env logs -f

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
