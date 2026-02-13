package api

import (
	"context"
	"log"
	"os"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/irfndi/neuratrade/internal/api/handlers"
	"github.com/irfndi/neuratrade/internal/ccxt"
	"github.com/irfndi/neuratrade/internal/config"
	"github.com/irfndi/neuratrade/internal/database"
	"github.com/irfndi/neuratrade/internal/middleware"
	"github.com/irfndi/neuratrade/internal/services"
	"github.com/shopspring/decimal"
)

// HealthResponse represents the response structure for health check endpoints.
// It provides the overall system status and the status of individual components.
type HealthResponse struct {
	// Status indicates the overall health of the service (e.g., "ok", "error").
	Status string `json:"status"`
	// Timestamp is the server time when the response was generated.
	Timestamp time.Time `json:"timestamp"`
	// Version is the current version of the application.
	Version string `json:"version"`
	// Services contains the health status of dependent services like Database and Redis.
	Services Services `json:"services"`
}

// Services contains the health status of individual dependencies.
type Services struct {
	// Database indicates the status of the primary database connection (e.g., "up", "down").
	Database string `json:"database"`
	// Redis indicates the status of the Redis cache connection (e.g., "up", "down").
	Redis string `json:"redis"`
}

type routeDB interface {
	services.DBPool
	HealthCheck(ctx context.Context) error
}

func getEnvOrDefault(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

// SetupRoutes configures all the HTTP routes for the application.
// It sets up middleware, health checks, and API endpoints (v1), and injects necessary dependencies into handlers.
//
// Parameters:
//
//	router: The Gin engine instance to register routes on.
//	db: The PostgreSQL database connection wrapper.
//	redis: The Redis client wrapper.
//	ccxtService: Service for interacting with crypto exchanges via CCXT.
//	collectorService: Service for collecting market data.
//	cleanupService: Service for data cleanup tasks.
//	cacheAnalyticsService: Service for cache metrics and analytics.
//	signalAggregator: Service for aggregating trading signals.
//	telegramConfig: Configuration for Telegram notifications.
//	authMiddleware: Middleware for handling authentication.
func SetupRoutes(router *gin.Engine, db routeDB, redis *database.RedisClient, ccxtService ccxt.CCXTService, collectorService *services.CollectorService, cleanupService *services.CleanupService, cacheAnalyticsService *services.CacheAnalyticsService, signalAggregator *services.SignalAggregator, analyticsService *services.AnalyticsService, telegramConfig *config.TelegramConfig, authMiddleware *middleware.AuthMiddleware, walletValidator *services.WalletValidator) {
	// Initialize admin middleware
	adminMiddleware := middleware.NewAdminMiddleware()

	// Initialize exchange reliability tracker
	tracker := services.NewExchangeReliabilityTracker(nil, redis.Client)

	// Initialize health handler
	healthHandler := handlers.NewHealthHandler(db, redis, ccxtService.GetServiceURL(), cacheAnalyticsService, tracker)

	// Health check endpoints with telemetry
	healthGroup := router.Group("/")
	healthGroup.Use(middleware.HealthCheckTelemetryMiddleware())
	{
		healthGroup.GET("/health", gin.WrapF(healthHandler.HealthCheck))
		healthGroup.HEAD("/health", gin.WrapF(healthHandler.HealthCheck))
		healthGroup.GET("/ready", gin.WrapF(healthHandler.ReadinessCheck))
		healthGroup.GET("/live", gin.WrapF(healthHandler.LivenessCheck))
	}

	// Initialize user and telegram internal handlers early for internal routes
	userHandler := handlers.NewUserHandler(db, redis.Client, authMiddleware)
	telegramInternalHandler := handlers.NewTelegramInternalHandler(db, userHandler)

	// Internal service-to-service routes (no auth, network-isolated via Docker)
	// These endpoints are only accessible within the Docker network
	// Security: Relies on Docker network isolation - only internal services can access
	internal := router.Group("/internal")
	{
		// Telegram service endpoints - used by telegram-service for user operations
		internalTelegram := internal.Group("/telegram")
		{
			internalTelegram.GET("/users/:id", telegramInternalHandler.GetUserByChatID)
			internalTelegram.GET("/notifications/:userId", telegramInternalHandler.GetNotificationPreferences)
			internalTelegram.POST("/notifications/:userId", telegramInternalHandler.SetNotificationPreferences)
			internalTelegram.POST("/autonomous/begin", telegramInternalHandler.BeginAutonomous)
			internalTelegram.POST("/autonomous/pause", telegramInternalHandler.PauseAutonomous)
			internalTelegram.POST("/wallets/connect_exchange", telegramInternalHandler.ConnectExchange)
			internalTelegram.POST("/wallets/connect_polymarket", telegramInternalHandler.ConnectPolymarket)
			internalTelegram.POST("/wallets", telegramInternalHandler.AddWallet)
			internalTelegram.POST("/wallets/remove", telegramInternalHandler.RemoveWallet)
			internalTelegram.GET("/wallets", telegramInternalHandler.GetWallets)
			internalTelegram.GET("/doctor", telegramInternalHandler.GetDoctor)
		}
	}

	// Initialize notification service with Redis caching
	var notificationService *services.NotificationService
	if telegramConfig != nil {
		notificationService = services.NewNotificationService(db, redis, telegramConfig.ServiceURL, telegramConfig.GrpcAddress, telegramConfig.AdminAPIKey)
	} else {
		log.Printf("[TELEGRAM] WARNING: telegramConfig is nil, notification service will run with default settings")
		notificationService = services.NewNotificationService(db, redis, "http://telegram-service:3002", "telegram-service:50052", "")
	}

	// Initialize handlers
	marketHandler := handlers.NewMarketHandler(db, ccxtService, collectorService, redis, cacheAnalyticsService)
	arbitrageHandler := handlers.NewArbitrageHandler(db, ccxtService, notificationService, redis.Client)
	circuitBreakerHandler := handlers.NewCircuitBreakerHandler(collectorService)

	analysisHandler := handlers.NewAnalysisHandler(db, ccxtService, analyticsService)
	// userHandler and telegramInternalHandler already initialized above for internal routes
	alertHandler := handlers.NewAlertHandler(db)
	cleanupHandler := handlers.NewCleanupHandler(cleanupService)
	exchangeHandler := handlers.NewExchangeHandler(ccxtService, collectorService, redis.Client)
	cacheHandler := handlers.NewCacheHandler(cacheAnalyticsService)

	// Initialize order execution service (Polymarket CLOB)
	orderExecConfig := services.OrderExecutionConfig{
		BaseURL:    getEnvOrDefault("POLYMARKET_CLOB_URL", "https://clob.polymarket.com"),
		APIKey:     os.Getenv("POLYMARKET_API_KEY"),
		APISecret:  os.Getenv("POLYMARKET_API_SECRET"),
		WalletAddr: os.Getenv("POLYMARKET_WALLET_ADDRESS"),
	}
	orderExecutionService := services.NewOrderExecutionService(orderExecConfig)
	tradingHandler := handlers.NewTradingHandler(db, orderExecutionService)

	// Budget handler - configurable via environment variables with defaults from migration 054
	dailyBudgetStr := getEnvOrDefault("AI_DAILY_BUDGET", "10.00")
	monthlyBudgetStr := getEnvOrDefault("AI_MONTHLY_BUDGET", "200.00")

	dailyBudget, err := decimal.NewFromString(dailyBudgetStr)
	if err != nil {
		log.Printf("WARNING: Invalid AI_DAILY_BUDGET value '%s', using default 10.00", dailyBudgetStr)
		dailyBudget = decimal.NewFromFloat(10.00)
	}

	monthlyBudget, err := decimal.NewFromString(monthlyBudgetStr)
	if err != nil {
		log.Printf("WARNING: Invalid AI_MONTHLY_BUDGET value '%s', using default 200.00", monthlyBudgetStr)
		monthlyBudget = decimal.NewFromFloat(200.00)
	}

	budgetHandler := handlers.NewBudgetHandler(
		database.NewAIUsageRepository(db),
		dailyBudget,
		monthlyBudget,
	)

	questEngine := services.NewQuestEngine(services.NewInMemoryQuestStore())
	autonomousHandler := handlers.NewAutonomousHandler(questEngine)

	// Initialize wallet handler
	walletHandler := handlers.NewWalletHandler(walletValidator)

	// Initialize futures arbitrage handler with error handling
	var futuresArbitrageHandler *handlers.FuturesArbitrageHandler
	if db != nil {
		// Safely initialize the handler
		futuresArbitrageHandler = handlers.NewFuturesArbitrageHandlerWithQuerier(db)
		log.Printf("Futures arbitrage handler initialized successfully")
	} else {
		log.Printf("Database not available for futures arbitrage handler initialization")
	}

	// API v1 routes with telemetry
	v1 := router.Group("/api/v1")
	v1.Use(middleware.TelemetryMiddleware())
	{
		// Market data routes
		market := v1.Group("/market")
		{
			market.GET("/prices", marketHandler.GetMarketPrices)
			market.GET("/ticker/:exchange/:symbol", marketHandler.GetTicker)
			market.GET("/tickers/:exchange", marketHandler.GetBulkTickers)
			market.GET("/orderbook/:exchange/:symbol", marketHandler.GetOrderBook)
			market.GET("/workers/status", marketHandler.GetWorkerStatus)
		}

		// Arbitrage routes
		arbitrage := v1.Group("/arbitrage")
		{
			arbitrage.GET("/opportunities", arbitrageHandler.GetArbitrageOpportunities)
			arbitrage.GET("/history", arbitrageHandler.GetArbitrageHistory)
			arbitrage.GET("/stats", arbitrageHandler.GetArbitrageStats)
			// Funding rate arbitrage
			arbitrage.GET("/funding", arbitrageHandler.GetFundingRateArbitrage)
			arbitrage.GET("/funding-rates/:exchange", arbitrageHandler.GetFundingRates)
		}

		// Futures arbitrage routes (only if handler initialized successfully)
		if futuresArbitrageHandler != nil {
			futuresArbitrage := v1.Group("/futures-arbitrage")
			{
				futuresArbitrage.GET("/opportunities", futuresArbitrageHandler.GetFuturesArbitrageOpportunities)
				futuresArbitrage.POST("/calculate", futuresArbitrageHandler.CalculateFuturesArbitrage)
				futuresArbitrage.GET("/strategy/:id", futuresArbitrageHandler.GetFuturesArbitrageStrategy)
				futuresArbitrage.GET("/market-summary", futuresArbitrageHandler.GetFuturesMarketSummary)
				futuresArbitrage.POST("/position-sizing", futuresArbitrageHandler.GetPositionSizingRecommendation)
			}
			log.Printf("Futures arbitrage routes registered successfully")
		} else {
			log.Printf("Skipping futures arbitrage routes due to handler initialization failure")
		}

		// Technical analysis routes
		analysis := v1.Group("/analysis")
		{
			analysis.GET("/indicators", analysisHandler.GetTechnicalIndicators)
			analysis.GET("/signals", analysisHandler.GetTradingSignals)
			analysis.GET("/correlation", analysisHandler.GetCorrelationMatrix)
			analysis.GET("/regime", analysisHandler.GetMarketRegime)
			analysis.GET("/forecast", analysisHandler.GetForecast)
		}

		// Telegram internal routes - backward compatible (no auth for internal network)
		// Both new (/internal/telegram/*) and legacy (/api/v1/telegram/internal/*) paths work
		telegram := v1.Group("/telegram")
		{
			// Legacy paths kept for backward compatibility with older telegram-service versions
			telegram.GET("/internal/users/:id", telegramInternalHandler.GetUserByChatID)
			telegram.GET("/internal/notifications/:userId", telegramInternalHandler.GetNotificationPreferences)
			telegram.POST("/internal/notifications/:userId", telegramInternalHandler.SetNotificationPreferences)
			telegram.POST("/internal/autonomous/begin", telegramInternalHandler.BeginAutonomous)
			telegram.POST("/internal/autonomous/pause", telegramInternalHandler.PauseAutonomous)
			telegram.POST("/internal/wallets/connect_exchange", telegramInternalHandler.ConnectExchange)
			telegram.POST("/internal/wallets/connect_polymarket", telegramInternalHandler.ConnectPolymarket)
			telegram.POST("/internal/wallets", telegramInternalHandler.AddWallet)
			telegram.POST("/internal/wallets/remove", telegramInternalHandler.RemoveWallet)
			telegram.GET("/internal/wallets", telegramInternalHandler.GetWallets)
			telegram.GET("/internal/doctor", telegramInternalHandler.GetDoctor)
			telegram.POST("/internal/operators/bind", telegramInternalHandler.BindOperatorProfile)
			telegram.POST("/internal/operators/unbind", telegramInternalHandler.UnbindOperatorProfile)

			telegramInternal := telegram.Group("/internal")
			telegramInternal.Use(adminMiddleware.RequireAdminAuth())
			{
				telegramInternal.GET("/quests", autonomousHandler.GetQuests)
				telegramInternal.GET("/portfolio", autonomousHandler.GetPortfolio)
				telegramInternal.GET("/logs", autonomousHandler.GetLogs)
				telegramInternal.GET("/performance/summary", autonomousHandler.GetPerformanceSummary)
				telegramInternal.GET("/performance", autonomousHandler.GetPerformanceBreakdown)
				telegramInternal.POST("/liquidate", autonomousHandler.Liquidate)
				telegramInternal.POST("/liquidate/all", autonomousHandler.LiquidateAll)
			}
		}

		// User management
		users := v1.Group("/users")
		{
			users.POST("/register", userHandler.RegisterUser)
			users.POST("/login", userHandler.LoginUser)
			users.GET("/profile", authMiddleware.RequireAuth(), userHandler.GetUserProfile)
		}

		// Alerts management
		alerts := v1.Group("/alerts")
		alerts.Use(authMiddleware.RequireAuth())
		{
			alerts.GET("/", alertHandler.GetUserAlerts)
			alerts.POST("/", alertHandler.CreateAlert)
			alerts.PUT("/:id", alertHandler.UpdateAlert)
			alerts.DELETE("/:id", alertHandler.DeleteAlert)
		}

		// Data management
		data := v1.Group("/data")
		{
			data.GET("/stats", cleanupHandler.GetDataStats)
			data.POST("/cleanup", cleanupHandler.TriggerCleanup)
		}

		// Risk management
		risk := v1.Group("/risk")
		{
			risk.GET("/metrics", gin.WrapF(healthHandler.GetRiskMetrics))
			risk.POST("/validate_wallet", walletHandler.ValidateWallet)
		}

		trading := v1.Group("/trading")
		trading.Use(authMiddleware.RequireAuth())
		{
			trading.POST("/place_order", tradingHandler.PlaceOrder)
			trading.POST("/cancel_order", tradingHandler.CancelOrder)
			trading.POST("/liquidate", tradingHandler.Liquidate)
			trading.POST("/liquidate_all", tradingHandler.LiquidateAll)
			trading.GET("/positions", tradingHandler.ListPositions)
			trading.GET("/positions/snapshot", tradingHandler.GetPositionSnapshot)
			trading.GET("/positions/:position_id", tradingHandler.GetPosition)

			// Polymarket CLOB order execution (neura-qts, neura-1wi)
			trading.POST("/polymarket/place_order", tradingHandler.PlacePolymarketOrder)
			trading.DELETE("/polymarket/orders/:order_id", tradingHandler.CancelPolymarketOrder)
			trading.GET("/polymarket/orderbook/:token_id", tradingHandler.GetPolymarketOrderBook)
		}

		budget := v1.Group("/budget")
		budget.Use(authMiddleware.RequireAuth())
		{
			budget.GET("/status", budgetHandler.GetBudgetStatus)
			budget.GET("/check", budgetHandler.CheckBudget)
		}

		// Exchange management
		exchanges := v1.Group("/exchanges")
		{
			// Public endpoints (no admin auth required)
			exchanges.GET("/config", exchangeHandler.GetExchangeConfig)
			exchanges.GET("/supported", exchangeHandler.GetSupportedExchanges)
			exchanges.GET("/workers/status", exchangeHandler.GetWorkerStatus)

			// Admin-only endpoints (require admin authentication)
			adminExchanges := exchanges.Group("/")
			adminExchanges.Use(adminMiddleware.RequireAdminAuth())
			{
				adminExchanges.POST("/refresh", exchangeHandler.RefreshExchanges)
				adminExchanges.POST("/add/:exchange", exchangeHandler.AddExchange)
				adminExchanges.POST("/blacklist/:exchange", exchangeHandler.AddExchangeToBlacklist)
				adminExchanges.DELETE("/blacklist/:exchange", exchangeHandler.RemoveExchangeFromBlacklist)
				adminExchanges.POST("/workers/:exchange/restart", exchangeHandler.RestartWorker)
			}
		}

		// Cache monitoring and analytics
		cache := v1.Group("/cache")
		{
			cache.GET("/stats", cacheHandler.GetCacheStats)
			cache.GET("/stats/:category", cacheHandler.GetCacheStatsByCategory)
			cache.GET("/metrics", cacheHandler.GetCacheMetrics)
			cache.POST("/stats/reset", cacheHandler.ResetCacheStats)
			cache.POST("/hit", cacheHandler.RecordCacheHit)
			cache.POST("/miss", cacheHandler.RecordCacheMiss)
		}

		// Admin endpoints (require admin authentication)
		admin := v1.Group("/admin")
		admin.Use(adminMiddleware.RequireAdminAuth())
		{
			// Circuit breaker management
			circuitBreakers := admin.Group("/circuit-breakers")
			{
				circuitBreakers.GET("", circuitBreakerHandler.GetCircuitBreakerStats)
				circuitBreakers.POST("/:name/reset", circuitBreakerHandler.ResetCircuitBreaker)
				circuitBreakers.POST("/reset-all", circuitBreakerHandler.ResetAllCircuitBreakers)
			}
		}
	}
}

// Placeholder handlers - to be implemented

// Arbitrage handlers are now implemented in handlers/arbitrage.go
// Technical analysis handlers are now implemented in handlers/analysis.go
// Alert handlers are now implemented in handlers/alert.go
