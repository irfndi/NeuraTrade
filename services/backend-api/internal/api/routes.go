package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/irfndi/neuratrade/internal/ai"
	"github.com/irfndi/neuratrade/internal/ai/llm"
	"github.com/irfndi/neuratrade/internal/api/handlers"
	"github.com/irfndi/neuratrade/internal/ccxt"
	"github.com/irfndi/neuratrade/internal/config"
	"github.com/irfndi/neuratrade/internal/database"
	"github.com/irfndi/neuratrade/internal/middleware"
	"github.com/irfndi/neuratrade/internal/services"
	"github.com/irfndi/neuratrade/internal/skill"
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
//	aiConfig: Configuration for AI-driven trading.
//	featuresConfig: Feature flags for enabling/disabling features.
//	authMiddleware: Middleware for handling authentication.
//
// Returns a cleanup function that should be called on shutdown.
func SetupRoutes(router *gin.Engine, db routeDB, redis *database.RedisClient, ccxtService ccxt.CCXTService, collectorService *services.CollectorService, cleanupService *services.CleanupService, cacheAnalyticsService *services.CacheAnalyticsService, signalAggregator *services.SignalAggregator, analyticsService *services.AnalyticsService, telegramConfig *config.TelegramConfig, aiConfig *config.AIConfig, featuresConfig *config.FeaturesConfig, authMiddleware *middleware.AuthMiddleware, walletValidator *services.WalletValidator) func() {
	// Initialize admin middleware
	adminMiddleware := middleware.NewAdminMiddleware()

	// Initialize health handler
	healthHandler := handlers.NewHealthHandler(db, redis, ccxtService.GetServiceURL(), cacheAnalyticsService)

	// Health check endpoints with telemetry
	healthGroup := router.Group("/")
	healthGroup.Use(middleware.HealthCheckTelemetryMiddleware())
	{
		healthGroup.GET("/health", gin.WrapF(healthHandler.HealthCheck))
		healthGroup.HEAD("/health", gin.WrapF(healthHandler.HealthCheck))
		healthGroup.GET("/ready", gin.WrapF(healthHandler.ReadinessCheck))
		healthGroup.GET("/live", gin.WrapF(healthHandler.LivenessCheck))
	}

	// Initialize user handler early for internal routes
	userHandler := handlers.NewUserHandler(db, redis.Client, authMiddleware)

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

	// Sentiment handler - initialize with config from environment
	sentimentConfig := services.DefaultSentimentServiceConfig()
	sentimentConfig.RedditClientID = os.Getenv("REDDIT_CLIENT_ID")
	sentimentConfig.RedditClientSecret = os.Getenv("REDDIT_CLIENT_SECRET")
	sentimentConfig.CryptoPanicToken = os.Getenv("CRYPTOPANIC_TOKEN")
	sentimentService := services.NewSentimentService(sentimentConfig, db)
	sentimentHandler := handlers.NewSentimentHandler(sentimentService)

	// userHandler and telegramInternalHandler already initialized above for internal routes
	alertHandler := handlers.NewAlertHandler(db)
	cleanupHandler := handlers.NewCleanupHandler(cleanupService)
	exchangeHandler := handlers.NewExchangeHandler(ccxtService, collectorService, redis.Client)
	cacheHandler := handlers.NewCacheHandler(cacheAnalyticsService)
	webSocketHandler := handlers.NewWebSocketHandler(redis)

	// AI handler - uses registry from ai package
	aiRegistry := ai.NewRegistry(
		ai.WithRedis(redis.Client),
	)
	aiHandler := handlers.NewAIHandler(aiRegistry, db)

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

	questStore := services.NewInMemoryQuestStore()
	questEngine := services.NewQuestEngineWithNotification(questStore, nil, notificationService)

	// Legacy quest preload is opt-in only.
	// In scalping-first mode we avoid restoring old active rows without metadata/chat ownership.
	loadLegacyQuests := os.Getenv("NEURATRADE_LOAD_LEGACY_ACTIVE_QUESTS") == "1" ||
		os.Getenv("NEURATRADE_LOAD_LEGACY_ACTIVE_QUESTS") == "true"
	log.Printf("DEBUG: db is nil: %v", db == nil)
	if db != nil && loadLegacyQuests {
		log.Println("Loading legacy active quests from database into memory...")
		rows, err := db.Query(context.Background(), "SELECT id, type, cadence, status, target_value, checkpoint, created_at FROM quests WHERE status = 'active'")
		if err != nil {
			log.Printf("Failed to load quests from database: %v", err)
		} else {
			defer rows.Close()
			loadedCount := 0
			for rows.Next() {
				var id, questType, cadence, status string
				var targetValue float64
				var checkpoint []byte
				var createdAt time.Time
				if err := rows.Scan(&id, &questType, &cadence, &status, &targetValue, &checkpoint, &createdAt); err != nil {
					log.Printf("Failed to scan quest row: %v", err)
					continue
				}
				quest := &services.Quest{
					ID:          id,
					Type:        services.QuestType(questType),
					Cadence:     services.QuestCadence(cadence),
					Status:      services.QuestStatus(status),
					TargetCount: int(targetValue),
					CreatedAt:   createdAt,
					UpdatedAt:   time.Now(),
				}
				if len(checkpoint) > 0 {
					var cp map[string]interface{}
					if err := json.Unmarshal(checkpoint, &cp); err == nil {
						quest.Checkpoint = cp
					}
				}
				if err := questStore.SaveQuest(context.Background(), quest); err != nil {
					log.Printf("Failed to save quest %s: %v", id, err)
				}
				log.Printf("Loaded quest from DB: %s (type: %s, status: %s)", id, questType, status)
				loadedCount++
			}
			log.Printf("Loaded %d quests from database", loadedCount)
		}
	} else if db != nil {
		log.Println("Skipping legacy active quest preload (set NEURATRADE_LOAD_LEGACY_ACTIVE_QUESTS=1 to enable)")
	}

	// Initialize futures arbitrage handler first (needed for quest handlers)
	var futuresArbitrageHandler *handlers.FuturesArbitrageHandler
	if db != nil {
		futuresArbitrageHandler = handlers.NewFuturesArbitrageHandlerWithQuerier(db)
		log.Printf("Futures arbitrage handler initialized successfully")
	} else {
		log.Printf("Database not available for futures arbitrage handler initialization")
	}

	// Create autonomous monitoring for tracking quest execution
	autonomousMonitoring := services.NewAutonomousMonitorManager(notificationService)

	// Create integrated quest handlers with actual implementations
	integratedHandlers := services.NewIntegratedQuestHandlers(
		nil,                     // TA service - TODO: Initialize when ready
		ccxtService,             // CCXT service
		arbitrageHandler,        // Arbitrage service
		futuresArbitrageHandler, // Futures arbitrage
		notificationService,     // Notification service
		autonomousMonitoring,    // Monitoring service
	)

	// Wire order executor to integrated handlers for scalping execution
	adminAPIKey := os.Getenv("ADMIN_API_KEY")
	if adminAPIKey == "" {
		log.Printf("WARNING: ADMIN_API_KEY is not set; CCXT order executor requests will be unauthenticated")
	}
	ccxtServiceURL := os.Getenv("CCXT_SERVICE_URL")
	if ccxtServiceURL == "" {
		ccxtServiceURL = "http://localhost:3001"
	}
	log.Printf("CCXT Order Executor configured with URL: %s", ccxtServiceURL)
	ccxtOrderExec := services.NewCCXTOrderExecutor(services.CCXTOrderExecutorConfig{
		ServiceURL: ccxtServiceURL,
		APIKey:     adminAPIKey,
		Timeout:    30 * time.Second,
	})
	integratedHandlers.SetOrderExecutor(ccxtOrderExec)

	var sqlDB *sql.DB
	switch concreteDB := db.(type) {
	case *database.SQLiteDB:
		sqlDB = concreteDB.DB
	case *database.PostgresDB:
		sqlDB = concreteDB.SQL
	default:
		log.Printf("Warning: Unknown database type, AI learning disabled")
	}

	if sqlDB != nil {
		tradeMemory, err := services.NewTradeMemory(sqlDB)
		if err != nil {
			log.Printf("Warning: Failed to create trade memory: %v", err)
		} else {
			integratedHandlers.SetTradeMemory(tradeMemory)
			log.Printf("Trade memory initialized for AI learning")
		}
	}

	var aiAPIKey, aiBaseURL, aiProvider string
	if aiConfig != nil && aiConfig.APIKey != "" {
		aiAPIKey = aiConfig.APIKey
		aiBaseURL = aiConfig.BaseURL
		if aiBaseURL == "" {
			aiBaseURL = "https://api.minimax.chat/v1"
		}
		aiProvider = aiConfig.Provider
		if aiProvider == "" {
			aiProvider = "minimax"
		}
	}

	if aiAPIKey != "" {
		log.Printf("Initializing AI Scalping with provider: %s (base_url: %s)", aiProvider, aiBaseURL)

		llmConfig := llm.ClientConfig{
			APIKey:      aiAPIKey,
			BaseURL:     aiBaseURL,
			HTTPTimeout: 120,
		}

		var llmClient llm.Client
		switch aiProvider {
		case "openai":
			llmClient = llm.NewOpenAIClient(llmConfig)
		case "anthropic":
			llmClient = llm.NewAnthropicClient(llmConfig)
		case "mlx":
			llmClient = llm.NewMLXClient(llmConfig)
		default:
			llmClient = llm.NewOpenAIClient(llmConfig)
		}

		skillRegistry := skill.NewRegistry(filepath.Join(filepath.Dir(""), "skills"))
		if err := skillRegistry.LoadAll(); err != nil {
			log.Printf("Warning: Failed to load skills: %v", err)
		}
		integratedHandlers.SetAIScalping(llmClient, skillRegistry)
		log.Printf("AI Scalping service initialized successfully")
	} else {
		log.Printf("AI API key not configured in ~/.neuratrade/config.json, AI scalping disabled")
	}

	questEngine.Start() // Start the quest engine scheduler

	// Restore autonomous scalping for operator chats that were enabled via Telegram /begin.
	if db != nil {
		rows, err := db.Query(
			context.Background(),
			"SELECT chat_id FROM telegram_operator_state WHERE autonomous_enabled = TRUE ORDER BY updated_at DESC LIMIT 1",
		)
		if err != nil {
			log.Printf("Failed to restore autonomous-enabled chats: %v", err)
		} else {
			defer rows.Close()
			restored := 0
			for rows.Next() {
				var chatID string
				if err := rows.Scan(&chatID); err != nil {
					log.Printf("Failed to scan autonomous chat row: %v", err)
					continue
				}
				chatID = strings.TrimSpace(chatID)
				if chatID == "" {
					continue
				}
				if _, err := questEngine.BeginAutonomous(chatID); err != nil {
					log.Printf("Failed to restore autonomous mode for chat %s: %v", chatID, err)
					continue
				}
				restored++
			}
			log.Printf("Restored autonomous scalping for %d chat(s) from telegram_operator_state (latest enabled chat only)", restored)
		}
	}

	// Scalping-first mode: keep arbitrage execution bridge disabled by default.
	// It can be re-enabled only when AI arbitrage mode is explicitly turned on.
	if featuresConfig != nil && featuresConfig.EnableAIArbitrage {
		arbitrageBridge := services.NewArbitrageExecutionBridge(db, questEngine, signalAggregator, nil)
		go func() {
			if err := arbitrageBridge.Start(context.Background()); err != nil {
				log.Printf("Arbitrage execution bridge error: %v", err)
			}
		}()
		log.Printf("Arbitrage execution bridge enabled (features.enable_ai_arbitrage=true)")
	} else {
		log.Printf("Arbitrage execution bridge disabled in scalping-first mode")
	}

	// Register integrated handlers for production-ready quest execution
	questEngine.RegisterIntegratedHandlers(integratedHandlers)

	autonomousHandler := handlers.NewAutonomousHandler(questEngine)
	telegramInternalHandler := handlers.NewTelegramInternalHandler(db, userHandler, questEngine)

	// Internal service-to-service routes (no auth, network-isolated via Docker)
	internal := router.Group("/internal")
	{
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

	// Initialize wallet handler
	walletHandler := handlers.NewWalletHandler(walletValidator)

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
			market.GET("/orderbook/:exchange/:symbol/metrics", marketHandler.GetOrderBookMetrics)
			market.GET("/workers/status", marketHandler.GetWorkerStatus)
			market.GET("/ws", webSocketHandler.HandleWebSocket)
			market.GET("/ws/stats", func(c *gin.Context) {
				c.JSON(200, webSocketHandler.GetStats())
			})
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

		// Sentiment routes - news and reddit sentiment analysis
		sentiment := v1.Group("/sentiment")
		{
			sentiment.GET("/:symbol", sentimentHandler.GetSentiment)
			sentiment.POST("/refresh", sentimentHandler.RefreshSentiment)
			sentiment.GET("/sources", sentimentHandler.GetSentimentSources)
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
		}

		adminRisk := v1.Group("/admin/risk")
		adminRisk.Use(adminMiddleware.RequireAdminAuth())
		{
			adminRisk.POST("/validate_wallet", walletHandler.ValidateWallet)
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
		}

		budget := v1.Group("/budget")
		budget.Use(authMiddleware.RequireAuth())
		{
			budget.GET("/status", budgetHandler.GetBudgetStatus)
			budget.GET("/check", budgetHandler.CheckBudget)
		}

		// AI model routes
		ai := v1.Group("/ai")
		{
			ai.GET("/models", aiHandler.GetModels)
			ai.POST("/route", aiHandler.RouteModel)
			aiAuth := ai.Group("")
			aiAuth.Use(authMiddleware.RequireAuth())
			{
				aiAuth.POST("/select/:userId", aiHandler.SelectModel)
				aiAuth.GET("/status/:userId", aiHandler.GetModelStatus)
			}
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

	// Return cleanup function for WebSocket handler and other resources
	return func() {
		if webSocketHandler != nil {
			webSocketHandler.Stop()
		}
	}
}

// Placeholder handlers - to be implemented

// Arbitrage handlers are now implemented in handlers/arbitrage.go
// Technical analysis handlers are now implemented in handlers/analysis.go
// Alert handlers are now implemented in handlers/alert.go
