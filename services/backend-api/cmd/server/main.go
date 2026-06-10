package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	sentrygin "github.com/getsentry/sentry-go/gin"
	"github.com/gin-gonic/gin"
	"github.com/irfndi/neuratrade/internal/api"
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

	apprisk "github.com/irfndi/neuratrade/internal/app/risk"
	"github.com/shopspring/decimal"
)

// main serves as the entry point for the application.
// It delegates execution to the run function and handles exit codes based on success or failure.
func main() {
	if handled, exitCode := handleServerCommand(os.Args, os.Stdout, os.Stderr); handled {
		if exitCode != 0 {
			os.Exit(exitCode)
		}
		return
	}

	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "Application failed: %v\n", err)
		os.Exit(1)
	}
}

func handleServerCommand(args []string, stdout io.Writer, stderr io.Writer) (bool, int) {
	if len(args) <= 1 {
		return false, 0
	}

	command := strings.TrimSpace(args[1])
	switch command {
	case "-h", "--help", "help":
		printServerUsage(stdout)
		return true, 0
	case "seed":
		if err := runSeeder(); err != nil {
			writeServerUsagef(stderr, "Seeding failed: %v\n", err)
			return true, 1
		}
		return true, 0
	case "ai":
		if err := runAICLI(); err != nil {
			writeServerUsagef(stderr, "AI command failed: %v\n", err)
			return true, 1
		}
		return true, 0
	default:
		if command == "" {
			writeServerUsageln(stderr, "Unknown command: <empty>")
			writeServerUsageln(stderr)
			printServerUsage(stderr)
			return true, 2
		}
		if strings.HasPrefix(command, "-") {
			writeServerUsagef(stderr, "Unknown option: %s\n\n", command)
			printServerUsage(stderr)
			return true, 2
		}
		writeServerUsagef(stderr, "Unknown command: %s\n\n", command)
		printServerUsage(stderr)
		return true, 2
	}
}

func printServerUsage(w io.Writer) {
	writeServerUsageln(w, "NeuraTrade Backend API")
	writeServerUsageln(w)
	writeServerUsageln(w, "Usage:")
	writeServerUsageln(w, "  neuratrade-server              Start the backend API server")
	writeServerUsageln(w, "  neuratrade-server seed         Seed configured database data")
	writeServerUsageln(w, "  neuratrade-server ai <command> Manage AI model registry")
	writeServerUsageln(w, "  neuratrade-server -h, --help   Show this help")
}

func writeServerUsagef(w io.Writer, format string, args ...any) {
	_, _ = fmt.Fprintf(w, format, args...)
}

func writeServerUsageln(w io.Writer, args ...any) {
	_, _ = fmt.Fprintln(w, args...)
}

// run orchestrates the startup sequence of the server.
// It loads configuration, initializes telemetry, databases, services, and the HTTP server.
// It also manages graceful shutdown upon receiving termination signals.
//
// Returns:
//   - An error if initialization fails at any critical step.
func run() error {
	lock, err := acquireServerProcessLock()
	if err != nil {
		return err
	}
	defer func() {
		if unlockErr := lock.Release(); unlockErr != nil {
			fmt.Fprintf(os.Stderr, "Failed to release backend process lock: %v\n", unlockErr)
		}
	}()

	// Load configuration
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("failed to load configuration: %w", err)
	}

	// Initialize Sentry for observability
	if err := observability.InitSentry(cfg.Sentry, cfg.Telemetry.ServiceVersion, cfg.Environment); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to initialize Sentry: %v\n", err)
	}
	flushTimeout := 5 * time.Second
	if d := os.Getenv("NEURATRADE_OBSERVABILITY_FLUSH_TIMEOUT"); d != "" {
		if parsed, err := time.ParseDuration(d); err == nil {
			flushTimeout = parsed
		}
	}
	defer func() {
		flushCtx, cancel := context.WithTimeout(context.Background(), flushTimeout)
		defer cancel()
		observability.Flush(flushCtx)
	}()

	// Initialize standard logger
	stdLogger := logging.NewStandardLogger(cfg.Telemetry.LogLevel, cfg.Environment)
	logger := logging.Logger(stdLogger)

	// Create logrus logger for services that require it (backward compatibility)
	logrusLogger := zaplogrus.New()
	logrusLogger.SetLevel(logging.ParseLogrusLevel(cfg.LogLevel))
	logrusLogger.SetFormatter(&zaplogrus.JSONFormatter{})

	warnLegacyHandlersPath(logrusLogger)

	// Initialize database
	db, err := database.NewDatabaseConnection(&cfg.Database)
	if err != nil {
		return fmt.Errorf("failed to connect to database: %w", err)
	}
	defer func() {
		if err := db.Close(); err != nil {
			logrusLogger.WithError(err).Error("Failed to close database connection")
		}
	}()

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
	authMiddleware, err := middleware.NewAuthMiddleware(cfg.Auth.JWTSecret)
	if err != nil {
		return fmt.Errorf("failed to initialize auth middleware: %w", err)
	}

	// Initialize cache analytics service
	cacheAnalyticsService := services.NewCacheAnalyticsService(getRedisClient())

	// Create cancellable context for service lifecycle management
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel() // Ensure context is cancelled on exit

	// Start periodic reporting of cache stats
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

	collectorStartErr := collectorService.Start()
	if collectorStartErr != nil {
		logger.WithError(collectorStartErr).Warn("Failed to start collector service - continuing without market data collection (AI scalping will use fallback data)")
		// Don't fail startup - AI scalping can work with direct exchange API calls
		// collectorService will be nil-safe for other operations
	}
	if collectorStartErr == nil {
		// Only wait for data if collector started successfully
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
	}

	// Initialize support services for futures arbitrage and cleanup
	resourceManager := services.NewResourceManager(getLogger("resource_manager"))
	defer resourceManager.Shutdown()
	performanceMonitor := services.NewPerformanceMonitor(getLogger("performance_monitor"), getRedisClient(), ctx)
	defer performanceMonitor.Stop()

	// Start historical data backfill in background only when explicitly enabled.
	if cfg.Backfill.Enabled && collectorService != nil {
		go func() {
			// Check if context is cancelled before/during long backfill operation
			if ctx.Err() != nil {
				logger.Info("Backfill skipped: context cancelled")
				return
			}
			logger.Info("Checking for historical data backfill requirements")
			if err := collectorService.PerformBackfillIfNeeded(); err != nil {
				logger.WithError(err).Warn("Backfill failed")
			} else {
				logger.Info("Historical data backfill check completed successfully")
			}
		}()
	} else {
		if !cfg.Backfill.Enabled {
			logger.Info("Historical data backfill disabled")
		} else {
			logger.Info("Skipping backfill - collector service not available")
		}
	}

	// Keep spot and futures arbitrage controls explicit.
	enableSpotArbitrageEngine := cfg.Features.EnableAIArbitrage && cfg.Arbitrage.Enabled
	if enableSpotArbitrageEngine {
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
		logger.Info("Spot arbitrage engine enabled")
	} else {
		logger.Info("Spot arbitrage engine disabled (enable with features.enable_ai_arbitrage=true and arbitrage.enabled=true)")
	}

	enableFuturesArbitrageEngine := cfg.Arbitrage.Enabled
	if enableFuturesArbitrageEngine {
		// Initialize futures arbitrage service
		futuresArbitrageService := services.NewFuturesArbitrageService(db, getRedisClient(), cfg, errorRecoveryManager, resourceManager, performanceMonitor, getLogger("futures_arbitrage_service"))
		if err := futuresArbitrageService.Start(); err != nil {
			logger.WithError(err).Fatal("Failed to start futures arbitrage service")
		}
		defer futuresArbitrageService.Stop()
		logger.Info("Futures arbitrage engine enabled")
	} else {
		logger.Info("Futures arbitrage engine disabled (enable with arbitrage.enabled=true)")
	}

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
	var redisClientHandle *redis.Client
	if redisClient != nil {
		redisClientHandle = redisClient.Client
	}
	positionTracker := services.NewPositionTracker(positionTrackerConfig, ccxtService, redisClientHandle, logrusLogger)
	positionTracker.Start()
	defer positionTracker.Stop()

	stopLossConfig := services.DefaultStopLossConfig()
	stopLossService := services.NewStopLossService(stopLossConfig, ccxtService, logrusLogger, nil, getRedisClient())
	reconcileCtx, reconcileCancel := context.WithTimeout(ctx, 10*time.Second)
	if err := stopLossService.ReconcileFromRedis(reconcileCtx); err != nil {
		logrusLogger.WithError(err).Warn("stop-loss: Failed to reconcile from Redis on startup")
	}
	reconcileCancel()

	stopLossAutoExecConfig := services.DefaultStopLossAutoExecutionConfig()
	stopLossAutoExecConfig.EnableNotifications = cfg.Telegram.BotToken != ""
	stopLossAutoExec := services.NewStopLossAutoExecution(stopLossAutoExecConfig, stopLossService, notificationService, logrusLogger)
	stopLossAutoExec.Start()
	defer stopLossAutoExec.Stop()

	maxLossMonitorConfig := services.DefaultMaxLossMonitorConfig()
	maxLossMonitor := services.NewMaxLossMonitorService(
		maxLossMonitorConfig,
		ccxtService,
		logrusLogger,
		func(_ context.Context, exchange, symbol, side string, quantity decimal.Decimal) (*services.MaxLossExecutionResult, error) {
			logrusLogger.Warnf("max-loss-monitor: CLOSE %s %s %s qty=%s (placeholder; ExecutionActor integration not yet wired)", exchange, side, symbol, quantity.String())
			return &services.MaxLossExecutionResult{Success: true, ExecutedAt: time.Now().UTC()}, nil
		},
	)
	maxLossMonitor.Start(ctx)
	defer maxLossMonitor.Stop()

	// Initialize heartbeat for continuous monitoring
	heartbeatConfig := services.DefaultHeartbeatConfig()
	heartbeatConfig.Enabled = true
	if value := getEnvIntWithDefault("NEURATRADE_HEARTBEAT_MAX_CONCURRENCY", heartbeatConfig.MaxConcurrency); value > 0 {
		heartbeatConfig.MaxConcurrency = value
	}
	heartbeatConfig.DegradedMultiplier = getEnvFloatWithDefault("NEURATRADE_HEARTBEAT_DEGRADED_MULTIPLIER", heartbeatConfig.DegradedMultiplier)
	heartbeatConfig.RiskMultiplier = getEnvFloatWithDefault("NEURATRADE_HEARTBEAT_RISK_MULTIPLIER", heartbeatConfig.RiskMultiplier)

	positionSync := &positionTrackerHeartbeatAdapter{tracker: positionTracker}
	stopLossCycle := &stopLossHeartbeatAdapter{service: stopLossService}
	connectivityCheck := &ccxtConnectivityHeartbeatAdapter{service: ccxtService}
	checkpointStore := &dbCheckpointStore{db: db}

	heartbeat := services.NewTradingHeartbeat(
		heartbeatConfig,
		positionSync,
		stopLossCycle,
		nil, // signalProcessor - interface{ScanForSignals(ctx context.Context) error}
		nil, // fundingCollector - interface{CheckFundingRates(ctx context.Context) error}
		connectivityCheck,
		checkpointStore,
		services.NewHeartbeatRiskBridgeAdapter(),
		notificationService,
	)
	services.RegisterHeartbeatRuntime(heartbeat)
	if err := heartbeat.Start(ctx); err != nil {
		logger.WithError(err).Warn("Failed to start heartbeat - continuing without it")
	} else {
		defer heartbeat.Stop()
		logger.Info("Trading heartbeat started")
	}

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

	// Initialize signal processor only when AI signals mode is enabled.
	if cfg.Features.EnableAISignals {
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

		if err := signalProcessor.Start(); err != nil {
			logger.WithError(err).Fatal("Failed to start signal processor")
		}
		defer func() {
			if err := signalProcessor.Stop(); err != nil {
				logger.WithError(err).Error("Failed to stop signal processor")
			}
		}()
		logger.Info("Signal processor enabled")
	} else {
		logger.Info("Signal processor disabled in scalping-first mode")
	}

	logger.Info("AI trading components ready for integration")

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

	// Create operational mode service
	opModeService := services.NewOperationalModeService(db, services.DefaultOperationalModeConfig(), logger.WithComponent("operational_mode"))

	// Start live-readiness reconciler when enabled and DB is available.
	if cfg.LiveReadiness.Enabled && db != nil {
		reconcilerStore := services.NewLiveReadinessManifestStore(db)
		initCtx, initCancel := context.WithTimeout(context.Background(), 5*time.Second)
		if err := reconcilerStore.InitSchema(initCtx); err != nil {
			logger.WithError(err).Warn("Failed to initialize live readiness manifest schema, reconciler may not persist manifests")
		}
		initCancel()
		reconcilerCfg := services.DefaultLiveReadinessReconcilerConfig()
		if cfg.LiveReadiness.IntervalHours > 0 {
			reconcilerCfg.Interval = time.Duration(cfg.LiveReadiness.IntervalHours) * time.Hour
		}
		if cfg.LiveReadiness.LookbackHours > 0 {
			reconcilerCfg.LookbackWindow = time.Duration(cfg.LiveReadiness.LookbackHours) * time.Hour
		}
		reconciler := services.NewLiveReadinessReconciler(db, reconcilerStore, &reconcilerLoggerAdapter{log: logger.WithComponent("live_readiness_reconciler")}, reconcilerCfg)
		if err := reconciler.Start(ctx); err != nil {
			logger.WithError(err).Warn("Failed to start live readiness reconciler")
		} else {
			defer reconciler.Stop()
			logger.Info("Live readiness reconciler started")
		}
	} else {
		if !cfg.LiveReadiness.Enabled {
			logger.Info("Live readiness reconciler disabled by configuration")
		} else {
			logger.Info("Live readiness reconciler skipped: no database available")
		}
	}

	// Initialize API key service for encrypted exchange credential storage (DB-backed Bitget creds)
	var apiKeyService *services.APIKeyService
	if strings.TrimSpace(cfg.Security.EncryptionKey) != "" {
		var initErr error
		apiKeyService, initErr = services.NewAPIKeyService(db, cfg.Security.EncryptionKey)
		if initErr != nil {
			logger.WithError(initErr).Warn("Failed to initialize API key service; Bitget creds will use config.json fallback only")
			apiKeyService = nil
		} else {
			logger.Info("API key service initialized for encrypted exchange credential storage")
		}
	} else {
		logger.Warn("Encryption key not configured; exchange API keys will not be available from encrypted DB storage")
	}

	// Backward-compat: if the legacy server.allowed_origins is set but
	// the new security.cors_allowed_origins is not, mirror the value so
	// existing deployments don't silently lose their CORS allowlist.
	if len(cfg.Security.CORSAllowedOrigins) == 0 && len(cfg.Server.AllowedOrigins) > 0 {
		cfg.Security.CORSAllowedOrigins = cfg.Server.AllowedOrigins
	}

	sharedKillSwitch := apprisk.NewKillSwitch()
	if db != nil {
		store := apprisk.NewSQLKillSwitchStore(db)
		sharedKillSwitch.SetStore(store)
		reconcileCtx, reconcileCancel := context.WithTimeout(context.Background(), 5*time.Second)
		if err := sharedKillSwitch.Reconcile(reconcileCtx); err != nil {
			zaplogrus.Warnf("kill switch reconcile: %v", err)
		}
		reconcileCancel()
	}
	sharedSafeMode := apprisk.NewSafeMode(apprisk.DefaultSafeModeConfig())

	// Setup routes and get cleanup function
	cleanupRoutes, err := api.SetupRoutes(router, db, redisClient, ccxtService, collectorService, cleanupService, cacheAnalyticsService, signalAggregator, analyticsService, &cfg.Telegram, &cfg.AI, &cfg.Features, authMiddleware, walletValidator, opModeService, technicalAnalysisService, &cfg.Security, apiKeyService, sharedKillSwitch, sharedSafeMode, &cfg.Testnet)
	if err != nil {
		return fmt.Errorf("failed to setup routes: %w", err)
	}
	defer cleanupRoutes()
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
		fmt.Printf("DEBUG: Starting server on address :%d\n", cfg.Server.Port)
		logger.LogStartup("celebrum-backend-api", "1.0.0", cfg.Server.Port)
		addr := fmt.Sprintf(":%d", cfg.Server.Port)
		fmt.Printf("DEBUG: Full address: %s\n", addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			fmt.Printf("DEBUG: Server error on address %s: %v\n", addr, err)
			logger.WithError(err).Fatal("Failed to start server")
		}
	}()

	// Wait for interrupt signal to gracefully shutdown the server
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	logger.LogShutdown("celebrum-backend-api", "signal received")

	// Cancel all service contexts to trigger graceful shutdown
	cancel()

	// Give outstanding requests a deadline for completion
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer shutdownCancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		logger.WithError(err).Fatal("Server forced to shutdown")
	}

	logger.Info("Server exited gracefully")
	return nil
}

type positionTrackerHeartbeatAdapter struct {
	tracker *services.PositionTracker
}

func (a *positionTrackerHeartbeatAdapter) SyncPositions(ctx context.Context) error {
	if a == nil || a.tracker == nil {
		return fmt.Errorf("position tracker is not configured")
	}
	return a.tracker.SyncWithExchange(ctx)
}

// reconcilerLoggerAdapter wraps logging.Logger to satisfy services.Logger.
type reconcilerLoggerAdapter struct {
	log logging.Logger
}

func (a *reconcilerLoggerAdapter) WithFields(fields map[string]interface{}) services.Logger {
	return &reconcilerLoggerAdapter{log: a.log.WithFields(fields)}
}

func (a *reconcilerLoggerAdapter) Info(msg string)  { a.log.Info(msg) }
func (a *reconcilerLoggerAdapter) Warn(msg string)  { a.log.Warn(msg) }
func (a *reconcilerLoggerAdapter) Error(msg string) { a.log.Error(msg) }

type stopLossHeartbeatAdapter struct {
	service *services.StopLossService
}

func (a *stopLossHeartbeatAdapter) UpdateAllStopLosses(ctx context.Context) error {
	if a == nil || a.service == nil {
		return fmt.Errorf("stop-loss service is not configured")
	}
	_, err := a.service.Evaluate(ctx)
	return err
}

type ccxtConnectivityHeartbeatAdapter struct {
	service ccxt.CCXTService
}

func (a *ccxtConnectivityHeartbeatAdapter) CheckConnectivity(ctx context.Context) error {
	if a == nil || a.service == nil {
		return fmt.Errorf("ccxt service is not configured")
	}
	if !a.service.IsHealthy(ctx) {
		return fmt.Errorf("ccxt connectivity check failed")
	}
	return nil
}

type dbCheckpointStore struct {
	db database.DBPool
}

func (s *dbCheckpointStore) Checkpoint(ctx context.Context) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("database checkpoint store is not configured")
	}
	var one int
	return s.db.QueryRow(ctx, "SELECT 1").Scan(&one)
}

func getEnvIntWithDefault(key string, fallback int) int {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(raw)
	if err != nil {
		return fallback
	}
	return parsed
}

func getEnvFloatWithDefault(key string, fallback float64) float64 {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback
	}
	parsed, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return fallback
	}
	return parsed
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

type serverProcessLock struct {
	file *os.File
}

func acquireServerProcessLock() (*serverProcessLock, error) {
	lockPath, err := serverLockFilePath()
	if err != nil {
		return nil, err
	}

	// #nosec G304 -- lock path is derived from NEURATRADE_HOME or user home, not request input
	lockFile, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("failed to open backend lock file %q: %w", lockPath, err)
	}

	fd, err := safeFD(lockFile)
	if err != nil {
		_ = lockFile.Close()
		return nil, err
	}
	if err := syscall.Flock(fd, syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = lockFile.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) || errors.Is(err, syscall.EAGAIN) {
			return nil, fmt.Errorf("backend server already running (lock: %s)", lockPath)
		}
		return nil, fmt.Errorf("failed to acquire backend lock %q: %w", lockPath, err)
	}

	if truncateErr := lockFile.Truncate(0); truncateErr == nil {
		_, _ = lockFile.Seek(0, 0)
		_, _ = lockFile.WriteString(strconv.Itoa(os.Getpid()))
	}

	return &serverProcessLock{file: lockFile}, nil
}

func serverLockFilePath() (string, error) {
	homeDir := strings.TrimSpace(os.Getenv("NEURATRADE_HOME"))
	if homeDir == "" {
		var err error
		homeDir, err = os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("failed to resolve home directory for backend lock: %w", err)
		}
		if strings.TrimSpace(homeDir) == "" {
			return "", fmt.Errorf("failed to resolve home directory for backend lock: empty home directory")
		}
		homeDir = filepath.Join(homeDir, ".neuratrade")
	}

	runDir := filepath.Join(homeDir, "run")
	// #nosec G703 -- runDir is derived from operator-controlled home path
	if err := os.MkdirAll(runDir, 0o700); err != nil {
		return "", fmt.Errorf("failed to create runtime directory %q: %w", runDir, err)
	}

	return filepath.Join(runDir, "backend.lock"), nil
}

func (l *serverProcessLock) Release() error {
	if l == nil || l.file == nil {
		return nil
	}

	fd, err := safeFD(l.file)
	if err != nil {
		closeErr := l.file.Close()
		l.file = nil
		if closeErr != nil {
			return fmt.Errorf("%v; additionally failed to close lock file: %w", err, closeErr)
		}
		return err
	}
	unlockErr := syscall.Flock(fd, syscall.LOCK_UN)
	closeErr := l.file.Close()
	l.file = nil

	if unlockErr != nil {
		return unlockErr
	}
	return closeErr
}

func safeFD(file *os.File) (int, error) {
	if file == nil {
		return 0, fmt.Errorf("lock file is nil")
	}
	fd := file.Fd()
	if fd > uintptr(math.MaxInt) {
		return 0, fmt.Errorf("lock file descriptor exceeds int range: %d", fd)
	}
	return int(fd), nil
}
