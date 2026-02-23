# NeuraTrade - Crypto Arbitrage & Technical Analysis Platform

NeuraTrade is a high-performance, scalable platform designed for real-time cryptocurrency arbitrage detection and advanced technical analysis. Built with a robust Go backend and a specialized TypeScript/Bun service for exchange integration, it processes market data from over 100 exchanges to identify profitable trading opportunities.

## 🚀 Key Features

*   **Real-time Market Data Engine**: High-throughput ingestion of tickers, order books, and trades via CCXT.
*   **Arbitrage Detection**:
    *   **Spot Arbitrage**: Identifies price discrepancies across exchanges.
    *   **Futures/Funding Arbitrage**: Capitalizes on funding rate differentials between perpetual futures contracts.
*   **Advanced Technical Analysis**: Real-time calculation of indicators (RSI, MACD, Bollinger Bands, etc.) to generate trading signals.
*   **Signal Aggregation**: Combines multiple signals (arbitrage + technical) into high-confidence trade recommendations.
*   **Comprehensive Risk Management**: Assessing exchange reliability, liquidity, and volatility before signaling.
*   **Multi-Channel Notifications**: Real-time alerts via Telegram and Webhooks.

## 🛠 Tech Stack

*   **Backend Core**: Go 1.25+ (Gin Framework) - Handles business logic, API, and orchestration.
*   **Market Data Service**: TypeScript (Bun runtime) - Wraps the CCXT library for unified exchange access.
*   **Telegram Bot Service**: TypeScript (Bun runtime) - Powered by grammY for user alerts.
*   **Database**: PostgreSQL 15+ - Persistent storage for users, signals, and historical data.
*   **Caching & Pub/Sub**: Redis 7+ - High-speed caching and inter-service messaging.
*   **Observability**: Sentry for error tracking and performance monitoring.

## 🏗 Architecture

The system follows a microservices-like architecture:

1.  **Server (Go)**: The central brain. It runs multiple internal services:
    *   `CollectorService`: Orchestrates data fetching.
    *   `ArbitrageService` & `FuturesArbitrageService`: Compute profitability.
    *   `TechnicalAnalysisService`: Computes indicators.
    *   `SignalAggregator`: Merges insights into actionable signals.
    *   `SignalProcessor`: Validates and filters signals based on quality scores.
2.  **CCXT Service (Bun)**: A specialized sidecar service that provides a uniform HTTP API for interacting with cryptocurrency exchanges.
3.  **Data Layer**: Postgres for persistence and Redis for hot data (tickers, cache).

## 📊 Advanced Analytics Status

Predictive forecasting (ARIMA/GARCH, regime detection, correlation analysis) is intentionally deferred.
Current analytics are CPU-only and based on descriptive statistics, liquidity/risk scoring, and backtesting.
See `docs/architecture/ADVANCED_ANALYTICS.md` for scope and future guidance.

## 🏁 Quick Start

### Prerequisites

*   **Go**: Version 1.25 or higher
*   **Bun**: Version 1.0+ (for the CCXT service)
*   **Docker & Docker Compose**: For orchestrated local development
*   **PostgreSQL**: Version 15+
*   **Redis**: Version 7+

### Option 1: Using NeuraTrade CLI (Recommended)

The NeuraTrade CLI provides a simple interface to manage all services:

1.  **Clone the Repository**
    ```bash
    git clone https://github.com/irfndi/neuratrade.git
    cd neuratrade
    ```

2.  **Install the CLI**
    ```bash
    make install-cli
    # Or: ./install.sh
    ```

3.  **Start All Services**
    ```bash
    neuratrade gateway start          # Start with Docker (recommended)
    # Or: neuratrade gateway start --native  # Start natively for development
    ```

4.  **Check Status**
    ```bash
    neuratrade gateway status
    ```

5.  **View Logs**
    ```bash
    neuratrade gateway logs -f        # Follow logs
    ```

6.  **Stop Services**
    ```bash
    neuratrade gateway stop
    ```

### Option 2: Using Make Commands

1.  **Clone the Repository**
    ```bash
    git clone https://github.com/irfndi/neuratrade.git
    cd neuratrade
    ```

2.  **Configure Environment**
    Copy the example configuration files:
    ```bash
    cp .env.example .env
    # Update .env with your specific API keys and secrets
    ```

3.  **Run with Docker (Recommended)**
    This starts the Go backend, CCXT service, Postgres, and Redis.
    ```bash
    make dev-up-orchestrated
    ```
    For future split deploys, build individual services using their Dockerfiles:
    - Backend API: `services/backend-api/Dockerfile`
    - CCXT Service: `services/ccxt-service/Dockerfile`
    - Telegram Service: `services/telegram-service/Dockerfile`

4.  **Run Manually (for deeper debugging)**
    *   Start dependencies:
        ```bash
        make dev-setup
        ```
    *   Run the Go server:
        ```bash
        make run
        ```
*   (Optional) The CCXT service is managed automatically or via Docker, but can be run separately in `services/ccxt-service/`.
*   (Optional) The Telegram bot service can be run separately in `services/telegram-service/`.

## 💻 Usage

### API Endpoints

The platform exposes a RESTful API. Key endpoints include:

*   **Market Data**: `/api/market/...` (Tickers, Orderbooks)
*   **Arbitrage**: `/api/arbitrage/opportunities` (Spot), `/api/futures/opportunities` (Funding)
*   **Signals**: `/api/analysis/signals` (Aggregated signals)
*   **Health**: `/health`

### Telegram Bot

Configure your `TELEGRAM_BOT_TOKEN` in `.env` to receive real-time alerts for high-quality arbitrage opportunities.
The bot is implemented in `services/telegram-service/` using grammY and can run in polling or webhook mode.
When running the Telegram service, set `TELEGRAM_EXTERNAL_SERVICE=true` to disable the legacy Go bot.

## 🧪 Development

We use `make` to manage common tasks:

*   `make build`: Compile the application.
*   `make test`: Run unit and integration tests.
*   `make lint`: Run code linters (golangci-lint).
*   `make fmt`: Format code to standard Go conventions.
*   `make clean`: Clean build artifacts.

### Project Structure (Monorepo)

This is a monorepo with all services organized under the `services/` directory:

*   `services/backend-api/`: Go backend service
    *   `cmd/server`: Entry point for the main application.
    *   `internal/api`: REST API handlers and routing.
    *   `internal/services`: Core business logic (Arbitrage, Analysis, etc.).
    *   `internal/models`: Database structs and domain objects.
    *   `pkg`: Shared libraries and utilities.
*   `services/ccxt-service/`: TypeScript/Bun service for exchange integration via CCXT.
*   `services/telegram-service/`: TypeScript/Bun service for the Telegram bot (grammY).
*   `protos/`: Protocol Buffer definitions for gRPC communication.
*   `docs/`: Project documentation.

## 🤝 Contributing

1.  Fork the repository.
2.  Create a feature branch (`git checkout -b feature/amazing-feature`).
3.  Commit your changes (ensure you've run `make fmt` and `make lint`).
4.  Push to the branch.
5.  Open a Pull Request.

**Documentation**: When adding new features, please ensure all public functions and types are documented using standard Go comments.

## 📄 License

This project is licensed under the MIT License - see the LICENSE file for details.
