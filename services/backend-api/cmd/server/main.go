package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	sentrygin "github.com/getsentry/sentry-go/gin"
	"github.com/gin-gonic/gin"
	"github.com/irfandi/celebrum-ai-go/internal/api"
	apiHandlers "github.com/irfandi/celebrum-ai-go/internal/api/handlers"
	"github.com/irfandi/celebrum-ai-go/internal/cache"
	"github.com/irfandi/celebrum-ai-go/internal/ccxt"
	"github.com/irfandi/celebrum-ai-go/internal/config"
	"github.com/irfandi/celebrum-ai-go/internal/database"
	"github.com/irfandi/celebrum-ai-go/internal/logging"
	zaplogrus "github.com/irfandi/celebrum-ai-go/internal/logging/zaplogrus"
	"github.com/irfandi/celebrum-ai-go/internal/middleware"
	"github.com/irfandi/celebrum-ai-go/internal/observability"
	"github.com/irfandi/celebrum-ai-go/internal/services"
	"github.com/redis/go-redis/v9"
)

// main serves as the entry point for the application.
// It delegates execution to the run function and handles exit codes based on success or failure.
func main() {
	// Check for CLI commands
	if len(os.Args) > 1 && os.Args[1] == "seed" {
		if err := runSeeder(); err != nil {
			fmt.Fprintf(os.Stderr, "Seeding failed: %v\n", err)
			os.Exit(1)
		}
		return
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
	logger := stdLogger.Logger()
	slog.SetDefault(logger)

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
		logrusLogger.WithError(err).Error("Failed to connect to Redis - continuing without cache")
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
	ccxtService := ccxt.NewService(&cfg.CCXT, logrusLogger, blacklistCache)

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
		logrusLogger.WithError(err).Warn("Cache warming failed")
		// Don't fail startup if cache warming fails, just log the warning
	}

	// Initialize collector service
	collectorService := services.NewCollectorService(db, ccxtService, cfg, getRedisClient(), blacklistCache)
	if err := collectorService.Start(); err != nil {
		logrusLogger.WithError(err).Fatal("Failed to start collector service")
	}
	defer collectorService.Stop()

	// Initialize support services for futures arbitrage and cleanup
	resourceManager := services.NewResourceManager(logrusLogger)
	defer resourceManager.Shutdown()
	performanceMonitor := services.NewPerformanceMonitor(logrusLogger, getRedisClient(), ctx)
	defer performanceMonitor.Stop()

	// Start historical data backfill in background if needed
	go func() {
		logrusLogger.Info("Checking for historical data backfill requirements")
		if err := collectorService.PerformBackfillIfNeeded(); err != nil {
			logrusLogger.WithError(err).Warn("Backfill failed")
		} else {
			logrusLogger.Info("Historical data backfill check completed successfully")
		}
	}()

	// Initialize futures arbitrage calculator
	arbitrageCalculator := services.NewFuturesArbitrageCalculator()

	// Initialize regular arbitrage service
	arbitrageService := services.NewArbitrageService(db, cfg, arbitrageCalculator)
	if err := arbitrageService.Start(); err != nil {
		logrusLogger.WithError(err).Fatal("Failed to start arbitrage service")
	}
	defer arbitrageService.Stop()

	// Initialize futures arbitrage service
	futuresArbitrageService := services.NewFuturesArbitrageService(db, getRedisClient(), cfg, errorRecoveryManager, resourceManager, performanceMonitor)
	if err := futuresArbitrageService.Start(); err != nil {
		logrusLogger.WithError(err).Fatal("Failed to start futures arbitrage service")
	}
	defer futuresArbitrageService.Stop()

	// Initialize signal aggregator service
	signalAggregator := services.NewSignalAggregator(cfg, db, logrusLogger)

	// Initialize technical analysis service
	technicalAnalysisService := services.NewTechnicalAnalysisService(
		cfg,
		db,
		logrusLogger,
		errorRecoveryManager,
		resourceManager,
		performanceMonitor,
	)

	// Initialize signal quality scorer
	signalQualityScorer := services.NewSignalQualityScorer(cfg, db, logrusLogger)

	// Initialize notification service
	notificationService := services.NewNotificationService(db, redisClient, cfg.Telegram.ServiceURL, cfg.Telegram.GrpcAddress, cfg.Telegram.AdminAPIKey)

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
		logrusLogger,
	)

	// Initialize signal processor
	signalProcessor := services.NewSignalProcessor(
		db,
		slog.New(slog.NewTextHandler(os.Stdout, nil)),
		signalAggregator,
		signalQualityScorer,
		technicalAnalysisService,
		notificationService,
		collectorService,
		signalProcessorCircuitBreaker,
	)

	// Start signal processor
	if err := signalProcessor.Start(); err != nil {
		logrusLogger.WithError(err).Fatal("Failed to start signal processor")
	}
	defer func() {
		if err := signalProcessor.Stop(); err != nil {
			logrusLogger.WithError(err).Error("Failed to stop signal processor")
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
	// REMOVED: router.Use(otelgin.Middleware("github.com/irfandi/celebrum-ai-go"))

	// Setup routes
	api.SetupRoutes(router, db, redisClient, ccxtService, collectorService, cleanupService, cacheAnalyticsService, signalAggregator, &cfg.Telegram, authMiddleware)

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
		logger.Info("Application startup",
			"service", "github.com/irfandi/celebrum-ai-go",
			"version", "1.0.0",
			"port", cfg.Server.Port,
			"event", "startup",
		)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logrusLogger.WithError(err).Fatal("Failed to start server")
		}
	}()

	// Wait for interrupt signal to gracefully shutdown the server
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	logger.Info("Application shutdown",
		"service", "github.com/irfandi/celebrum-ai-go",
		"event", "shutdown",
		"reason", "signal received",
	)

	// Give outstanding requests a deadline for completion
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		logrusLogger.WithError(err).Fatal("Server forced to shutdown")
	}

	logrusLogger.Info("Server exited gracefully")
	return nil
}

func runSQLiteBootstrapMode(cfg *config.Config, logger *slog.Logger, logrusLogger *zaplogrus.Logger) error {
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
		logger.Info("Application startup",
			"service", "github.com/irfandi/celebrum-ai-go",
			"version", "1.0.0",
			"port", cfg.Server.Port,
			"mode", "sqlite-bootstrap",
			"event", "startup",
		)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logrusLogger.WithError(err).Fatal("Failed to start sqlite bootstrap server")
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	logger.Info("Application shutdown",
		"service", "github.com/irfandi/celebrum-ai-go",
		"mode", "sqlite-bootstrap",
		"event", "shutdown",
		"reason", "signal received",
	)

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
