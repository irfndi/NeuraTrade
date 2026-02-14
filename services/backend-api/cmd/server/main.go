package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	sentrygin "github.com/getsentry/sentry-go/gin"
	"github.com/gin-gonic/gin"
	"github.com/irfndi/neuratrade/internal/api"
	apiHandlers "github.com/irfndi/neuratrade/internal/api/handlers"
	"github.com/irfndi/neuratrade/internal/cache"
	"github.com/irfndi/neuratrade/internal/ccxt"
	"github.com/irfndi/neuratrade/internal/config"
	"github.com/irfndi/neuratrade/internal/database"
	"github.com/irfndi/neuratrade/internal/logging"
	zaplogrus "github.com/irfndi/neuratrade/internal/logging/zaplogrus"
	"github.com/irfndi/neuratrade/internal/middleware"
	"github.com/irfndi/neuratrade/internal/observability"
	"github.com/irfndi/neuratrade/internal/services"
	"github.com/redis/go-redis/v9"
	"github.com/shopspring/decimal"
)

// main serves as the entry point for the application.
// It delegates execution to the run function and handles exit codes based on success or failure.
func main() {
	// Check for CLI commands
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "seed":
			if err := runSeeder(); err != nil {
				fmt.Fprintf(os.Stderr, "Seeding failed: %v\n", err)
				os.Exit(1)
			}
			return
		case "ai":
			if err := runAICLI(); err != nil {
				fmt.Fprintf(os.Stderr, "AI command failed: %v\n", err)
				os.Exit(1)
			}
			return
		}
	}

	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "Application failed: %v\n", err)
		os.Exit(1)
	}
}

// run orchestrates the startup sequence of the server.
// It loads configuration, initializes telemetry, databases, services, and the HTTP server.
// It also manages graceful shutdown upon receiving termination signals.
//
// Returns:
//   - An error if initialization fails at any critical step.
func run() error {
	// Load configuration
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("failed to load configuration: %w", err)
	}

	// Initialize Sentry for observability
	if err := observability.InitSentry(cfg.Sentry, cfg.Telemetry.ServiceVersion, cfg.Environment); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to initialize Sentry: %v\n", err)
	}
	defer observability.Flush(context.Background())

	// Initialize standard logger
	stdLogger := logging.NewStandardLogger(cfg.Telemetry.LogLevel, cfg.Environment)
	logger := logging.Logger(stdLogger)

	// Create logrus logger for services that require it (backward compatibility)
	logrusLogger := zaplogrus.New()
	logrusLogger.SetLevel(logging.ParseLogrusLevel(cfg.LogLevel))
	logrusLogger.SetFormatter(&zaplogrus.JSONFormatter{})

	warnLegacyHandlersPath(logrusLogger)

	// Initialize database
	driver := strings.ToLower(strings.TrimSpace(cfg.Database.Driver))
	if driver == "" {
		driver = "postgres"
	}

	if driver == "sqlite" {
		return runSQLiteBootstrapMode(cfg, logger, logrusLogger)
	}

	db, err := database.NewPostgresConnection(&cfg.Database)
	if err != nil {
		return fmt.Errorf("failed to connect to database: %w", err)
	}
	defer db.Close()

	// Initialize error recovery manager for Redis connection
	errorRecoveryManager := services.NewErrorRecoveryManager(logrusLogger)

	// Register retry policies for Redis operations
	retryPolicies := services.DefaultRetryPolicies()
	for name, policy := range retryPolicies {
		errorRecoveryManager.RegisterRetryPolicy(name, policy)
	}

	// Initialize Redis with retry mechanism
	redisClient, err := database.NewRedisConnectionWithRetry(cfg.Redis, errorRecoveryManager)
	if err != nil {
		logger.WithError(err).Error("Failed to connect to Redis - continuing without cache")
		// Don't fail startup on Redis connection issues, continue without cache
		redisClient = nil
	} else {
		defer redisClient.Close()
	}

	// Helper function to safely get Redis client
	getRedisClient := func() *redis.Client {
		if redisClient != nil {
			return redisClient.Client
		}
		return nil
	}

	// Helper to get Logger with component context
	getLogger := func(component string) *zaplogrus.Logger {
		_ = component
		return logrusLogger
	}

	// Initialize blacklist cache with database persistence
	blacklistRepo := database.NewBlacklistRepository(db)
	var blacklistCache cache.BlacklistCache
	if redisClient != nil {
		blacklistCache = cache.NewRedisBlacklistCache(redisClient.Client, blacklistRepo)
	} else {
		// Fallback to in-memory cache if Redis is not available
		blacklistCache = cache.NewInMemoryBlacklistCache()
	}

	// Initialize CCXT service with blacklist cache
	ccxtService := ccxt.NewService(&cfg.CCXT, getLogger("ccxt_service"), blacklistCache)

	// Initialize JWT authentication middleware
	authMiddleware := middleware.NewAuthMiddleware(cfg.Auth.JWTSecret)

	// Initialize cache analytics service
	cacheAnalyticsService := services.NewCacheAnalyticsService(getRedisClient())

	// Start periodic reporting of cache stats
	ctx := context.Background()
	cacheAnalyticsService.StartPeriodicReporting(ctx, 5*time.Minute)

	// Initialize and perform cache warming
	cacheWarmingService := services.NewCacheWarmingService(getRedisClient(), ccxtService, db)
	if err := cacheWarmingService.WarmCache(ctx); err != nil {
		logger.WithError(err).Warn("Cache warming failed")
		// Don't fail startup if cache warming fails, just log the warning
	}

	// Initialize collector service
	collectorService := services.NewCollectorService(db, ccxtService, cfg, getRedisClient(), blacklistCache)

	// Verify database has required seed data before starting collection
	if err := collectorService.VerifyDatabaseSeeding(); err != nil {
		logger.WithError(err).Warn("Database seeding verification failed - collection may not work correctly")
		// Don't fail startup, but log warning - exchanges may be created dynamically
	}

	if err := collectorService.Start(); err != nil {
		logger.WithError(err).Fatal("Failed to start collector service")
	}
	defer collectorService.Stop()

	// Wait for first market data before starting dependent services
	// This prevents arbitrage from running with no data (exchanges=0 issue)
	logger.Info("Waiting for initial market data collection...")
	if err := collectorService.WaitForFirstData(2 * time.Minute); err != nil {
		logger.WithError(err).Warn("Timeout waiting for first market data - starting dependent services anyway")
		// Don't fail startup, but log warning - services will retry on next collection
	} else {
		logger.Info("Initial market data collected successfully")
	}

	// Initialize support services for futures arbitrage and cleanup
	resourceManager := services.NewResourceManager(getLogger("resource_manager"))
	defer resourceManager.Shutdown()
	performanceMonitor := services.NewPerformanceMonitor(getLogger("performance_monitor"), getRedisClient(), ctx)
	defer performanceMonitor.Stop()

	// Start historical data backfill in background if needed
	go func() {
		logger.Info("Checking for historical data backfill requirements")
		if err := collectorService.PerformBackfillIfNeeded(); err != nil {
			logger.WithError(err).Warn("Backfill failed")
		} else {
			logger.Info("Historical data backfill check completed successfully")
		}
	}()

	// Initialize futures arbitrage calculator
	arbitrageCalculator := services.NewFuturesArbitrageCalculator()

	// Initialize fee provider for exchange-specific fees
	feeProvider := services.NewDBFeeProvider(
		db,
		decimal.NewFromFloat(cfg.Fees.DefaultTakerFee),
		decimal.NewFromFloat(cfg.Fees.DefaultMakerFee),
	)

	arbitrageCalculator.WithFeeProvider(feeProvider, decimal.NewFromFloat(cfg.Fees.DefaultTakerFee))

	// Initialize regular arbitrage service
	arbitrageService := services.NewArbitrageService(db, cfg, arbitrageCalculator)
	if err := arbitrageService.Start(); err != nil {
		logger.WithError(err).Fatal("Failed to start arbitrage service")
	}
	defer arbitrageService.Stop()

	// Initialize futures arbitrage service
	futuresArbitrageService := services.NewFuturesArbitrageService(db, getRedisClient(), cfg, errorRecoveryManager, resourceManager, performanceMonitor, getLogger("futures_arbitrage_service"))
	if err := futuresArbitrageService.Start(); err != nil {
		logger.WithError(err).Fatal("Failed to start futures arbitrage service")
	}
	defer futuresArbitrageService.Stop()

	// Initialize signal aggregator service
	signalAggregator := services.NewSignalAggregator(cfg, db, getLogger("signal_aggregator"))

	// Initialize analytics service
	analyticsService := services.NewAnalyticsService(db, cfg.Analytics)

	// Initialize technical analysis service
	technicalAnalysisService := services.NewTechnicalAnalysisService(
		cfg,
		db,
		getLogger("technical_analysis"),
		errorRecoveryManager,
		resourceManager,
		performanceMonitor,
	)

	// Initialize signal quality scorer
	signalQualityScorer := services.NewSignalQualityScorer(cfg, db, getLogger("signal_quality_scorer"))

	notificationService := services.NewNotificationService(db, redisClient, cfg.Telegram.ServiceURL, cfg.Telegram.GrpcAddress, cfg.Telegram.AdminAPIKey)

	positionTrackerConfig := services.DefaultPositionTrackerConfig()
	positionTracker := services.NewPositionTracker(positionTrackerConfig, ccxtService, redisClient.Client, logrusLogger)
	positionTracker.Start()
	defer positionTracker.Stop()

	stopLossConfig := services.DefaultStopLossConfig()
	stopLossService := services.NewStopLossService(stopLossConfig, ccxtService, logrusLogger, nil)

	stopLossAutoExecConfig := services.DefaultStopLossAutoExecutionConfig()
	stopLossAutoExecConfig.EnableNotifications = cfg.Telegram.BotToken != ""
	stopLossAutoExec := services.NewStopLossAutoExecution(stopLossAutoExecConfig, stopLossService, notificationService, logrusLogger)
	stopLossAutoExec.Start()
	defer stopLossAutoExec.Stop()

	// Initialize wallet validator
	walletValidatorConfig := services.WalletValidatorConfig{
		MinimumUSDCBalance:         decimal.NewFromFloat(cfg.Wallet.MinimumUSDCBalance),
		MinimumPortfolioValue:      decimal.NewFromFloat(cfg.Wallet.MinimumPortfolioValue),
		MinimumExchangeConnections: cfg.Wallet.MinimumExchangeConnections,
	}
	walletValidator := services.NewWalletValidator(db, walletValidatorConfig)

	// Initialize circuit breaker for signal processing
	signalProcessorCircuitBreaker := services.NewCircuitBreaker(
		"signal_processor",
		services.CircuitBreakerConfig{
			FailureThreshold: 5,
			SuccessThreshold: 3,
			Timeout:          30 * time.Second,
			ResetTimeout:     60 * time.Second,
			MaxRequests:      10,
		},
		getLogger("circuit_breaker"),
	)

	// Initialize signal processor
	signalProcessor := services.NewSignalProcessor(
		db,
		logger.WithComponent("signal_processor"),
		signalAggregator,
		signalQualityScorer,
		technicalAnalysisService,
		notificationService,
		collectorService,
		signalProcessorCircuitBreaker,
	)

	// Start signal processor
	if err := signalProcessor.Start(); err != nil {
		logger.WithError(err).Fatal("Failed to start signal processor")
	}
	defer func() {
		if err := signalProcessor.Stop(); err != nil {
			logger.WithError(err).Error("Failed to stop signal processor")
		}
	}()

	// Initialize cleanup service
	cleanupService := services.NewCleanupService(db, errorRecoveryManager, resourceManager, performanceMonitor)

	// Start cleanup service with configuration
	cleanupConfig := cfg.Cleanup
	go cleanupService.Start(cleanupConfig)
	defer cleanupService.Stop()

	// Setup Gin router
	router := gin.New()
	router.Use(gin.Logger())
	if cfg.Sentry.Enabled && cfg.Sentry.DSN != "" {
		router.Use(sentrygin.New(sentrygin.Options{
			Repanic:         true,
			WaitForDelivery: false,
			Timeout:         2 * time.Second,
		}))
	}
	router.Use(gin.Recovery())

	// Setup routes
	api.SetupRoutes(router, db, redisClient, ccxtService, collectorService, cleanupService, cacheAnalyticsService, signalAggregator, analyticsService, &cfg.Telegram, authMiddleware, walletValidator)

	// Create HTTP server with security timeouts
	srv := &http.Server{
		Addr:              fmt.Sprintf(":%d", cfg.Server.Port),
		Handler:           router,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       15 * time.Second,
	}

	// Start server in a goroutine
	go func() {
		logger.LogStartup("celebrum-backend-api", "1.0.0", cfg.Server.Port)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.WithError(err).Fatal("Failed to start server")
		}
	}()

	// Wait for interrupt signal to gracefully shutdown the server
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	logger.LogShutdown("celebrum-backend-api", "signal received")

	// Give outstanding requests a deadline for completion
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		logger.WithError(err).Fatal("Server forced to shutdown")
	}

	logger.Info("Server exited gracefully")
	return nil
}

func runSQLiteBootstrapMode(cfg *config.Config, logger logging.Logger, logrusLogger *zaplogrus.Logger) error {
	warnLegacyHandlersPath(logrusLogger)

	sqliteDB, err := database.NewSQLiteConnectionWithExtension(cfg.Database.SQLitePath, cfg.Database.SQLiteVectorExtensionPath)
	if err != nil {
		return fmt.Errorf("failed to connect to sqlite database: %w", err)
	}
	defer func() {
		_ = sqliteDB.Close()
	}()

	errorRecoveryManager := services.NewErrorRecoveryManager(logrusLogger)
	retryPolicies := services.DefaultRetryPolicies()
	for name, policy := range retryPolicies {
		errorRecoveryManager.RegisterRetryPolicy(name, policy)
	}

	redisClient, err := database.NewRedisConnectionWithRetry(cfg.Redis, errorRecoveryManager)
	if err != nil {
		logrusLogger.WithError(err).Warn("Failed to connect to Redis in sqlite bootstrap mode - continuing without cache")
		redisClient = nil
	} else {
		defer redisClient.Close()
	}

	getRedisClient := func() *redis.Client {
		if redisClient != nil {
			return redisClient.Client
		}
		return nil
	}

	cacheAnalyticsService := services.NewCacheAnalyticsService(getRedisClient())
	cacheAnalyticsService.StartPeriodicReporting(context.Background(), 5*time.Minute)

	router := gin.New()
	router.Use(gin.Logger())
	if cfg.Sentry.Enabled && cfg.Sentry.DSN != "" {
		router.Use(sentrygin.New(sentrygin.Options{
			Repanic:         true,
			WaitForDelivery: false,
			Timeout:         2 * time.Second,
		}))
	}
	router.Use(gin.Recovery())

	healthHandler := apiHandlers.NewHealthHandler(sqliteDB, redisClient, cfg.CCXT.ServiceURL, cacheAnalyticsService)
	healthGroup := router.Group("/")
	healthGroup.Use(middleware.HealthCheckTelemetryMiddleware())
	{
		healthGroup.GET("/health", gin.WrapF(healthHandler.HealthCheck))
		healthGroup.HEAD("/health", gin.WrapF(healthHandler.HealthCheck))
		healthGroup.GET("/ready", gin.WrapF(healthHandler.ReadinessCheck))
		healthGroup.GET("/live", gin.WrapF(healthHandler.LivenessCheck))
	}

	router.GET("/api/v1/bootstrap/status", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"status": "ok",
			"mode":   "sqlite-bootstrap",
		})
	})

	srv := &http.Server{
		Addr:              fmt.Sprintf(":%d", cfg.Server.Port),
		Handler:           router,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       15 * time.Second,
	}

	go func() {
		logger.WithFields(map[string]interface{}{
			"service": "github.com/irfndi/neuratrade",
			"version": "1.0.0",
			"port":    cfg.Server.Port,
			"mode":    "sqlite-bootstrap",
			"event":   "startup",
		}).Info("Application startup")
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logrusLogger.WithError(err).Fatal("Failed to start sqlite bootstrap server")
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	logger.WithFields(map[string]interface{}{
		"service": "github.com/irfndi/neuratrade",
		"mode":    "sqlite-bootstrap",
		"event":   "shutdown",
		"reason":  "signal received",
	}).Info("Application shutdown")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		logrusLogger.WithError(err).Fatal("SQLite bootstrap server forced to shutdown")
	}

	logrusLogger.Info("SQLite bootstrap server exited gracefully")
	return nil
}

func warnLegacyHandlersPath(logger *zaplogrus.Logger) {
	if logger == nil {
		return
	}

	if _, err := os.Stat("internal/handlers"); err == nil {
		logger.Warn("Legacy handlers path detected at internal/handlers; continuing with canonical internal/api/handlers")
		return
	} else if !os.IsNotExist(err) {
		logger.WithError(err).Warn("Failed to inspect legacy handlers path")
	}
}
