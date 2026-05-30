package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	"github.com/irfndi/neuratrade/internal/ai"
	"github.com/irfndi/neuratrade/internal/ai/llm"
	"github.com/irfndi/neuratrade/internal/api/handlers"
	autonomyruntime "github.com/irfndi/neuratrade/internal/app/autonomy/runtime"
	apprisk "github.com/irfndi/neuratrade/internal/app/risk"
	"github.com/irfndi/neuratrade/internal/autonomous"
	"github.com/irfndi/neuratrade/internal/ccxt"
	"github.com/irfndi/neuratrade/internal/config"
	"github.com/irfndi/neuratrade/internal/database"
	"github.com/irfndi/neuratrade/internal/middleware"
	"github.com/irfndi/neuratrade/internal/services"
	"github.com/irfndi/neuratrade/internal/services/risk"
	"github.com/irfndi/neuratrade/internal/skill"
	"github.com/irfndi/neuratrade/internal/telemetry"
	redisv9 "github.com/redis/go-redis/v9"
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

func diagnosticFloat(metrics map[string]interface{}, key string) (float64, bool) {
	value, ok := metrics[key]
	if !ok {
		return 0, false
	}
	switch value := value.(type) {
	case float64:
		return value, true
	case float32:
		return float64(value), true
	case int:
		return float64(value), true
	default:
		return 0, false
	}
}

func getEnvOrDefault(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func configureLiveReadinessGuard(opModeService *services.OperationalModeService) {
	if opModeService == nil {
		return
	}
	if liveReadinessGuardDisabled(os.Getenv("NEURATRADE_REQUIRE_LIVE_READINESS_PROOF")) {
		log.Printf("WARNING: real-money live readiness proof guard disabled by NEURATRADE_REQUIRE_LIVE_READINESS_PROOF")
		opModeService.SetLiveModeGuard(nil)
		return
	}

	requiredStrategies := services.DefaultLiveReadinessStrategies()
	if raw := strings.TrimSpace(os.Getenv("NEURATRADE_LIVE_READINESS_STRATEGIES")); raw != "" {
		parsed := strings.Split(raw, ",")
		filtered := make([]string, 0, len(parsed))
		for _, s := range parsed {
			s = strings.TrimSpace(s)
			if s != "" {
				filtered = append(filtered, s)
			}
		}
		if len(filtered) > 0 {
			requiredStrategies = filtered
		} else {
			log.Printf("WARNING: NEURATRADE_LIVE_READINESS_STRATEGIES contained only empty/whitespace entries; using defaults")
		}
	}
	manifestPath := strings.TrimSpace(os.Getenv(services.LiveReadinessManifestEnv))
	opModeService.SetLiveModeGuard(services.ManifestLiveModeGuard(manifestPath, requiredStrategies))
	if manifestPath != "" {
		log.Printf("Real-money live readiness proof guard enabled with manifest %s; live mode requires strategy evidence", manifestPath)
	} else {
		log.Printf("Real-money live readiness proof guard enabled; live mode requires %s with strategy evidence", services.LiveReadinessManifestEnv)
	}
}

func liveReadinessGuardDisabled(raw string) bool {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "0", "false", "no", "off", "disabled":
		return true
	default:
		return false
	}
}

type llmProviderNodeConfig struct {
	Provider      string
	APIKey        string
	BaseURL       string
	ModelOverride string
	DefaultModel  string
}

type zapNopServiceLogger struct{}

func (zapNopServiceLogger) WithFields(_ map[string]interface{}) services.Logger {
	return zapNopServiceLogger{}
}
func (zapNopServiceLogger) Info(_ string)  {}
func (zapNopServiceLogger) Warn(_ string)  {}
func (zapNopServiceLogger) Error(_ string) {}

var supportedAIProviders = map[string]struct{}{
	string(llm.ProviderOpenAI):    {},
	string(llm.ProviderAnthropic): {},
	string(llm.ProviderMLX):       {},
	"deepseek":                    {},
	"google":                      {},
	"kimi":                        {},
	"kimi-for-coding":             {},
	"moonshotai":                  {},
	"moonshotai-cn":               {},
	"minimax":                     {},
	"zai":                         {},
	"zai-coding-plan":             {},
	"zhipu":                       {},
}

func parseAIProviderChain(primary string) ([]string, error) {
	primary = strings.ToLower(strings.TrimSpace(primary))
	raw := strings.TrimSpace(os.Getenv("NEURATRADE_AI_PROVIDER_CHAIN"))
	parts := []string{}
	if raw != "" {
		parts = strings.Split(raw, ",")
	}
	if primary == "" && len(parts) == 0 {
		primary = "deepseek"
	}

	seen := make(map[string]struct{})
	chain := make([]string, 0, len(parts)+1)
	if primary != "" {
		if err := validateAIProviderName(primary); err != nil {
			return nil, err
		}
		seen[primary] = struct{}{}
		chain = append(chain, primary)
	}
	for _, part := range parts {
		provider := strings.ToLower(strings.TrimSpace(part))
		if provider == "" {
			continue
		}
		if err := validateAIProviderName(provider); err != nil {
			return nil, err
		}
		if _, exists := seen[provider]; exists {
			continue
		}
		seen[provider] = struct{}{}
		chain = append(chain, provider)
	}
	return chain, nil
}

func validateAIProviderName(provider string) error {
	provider = strings.ToLower(strings.TrimSpace(provider))
	if provider == "" {
		return nil
	}
	if _, ok := supportedAIProviders[provider]; ok {
		return nil
	}
	return fmt.Errorf("unsupported ai provider %q in parseAIProviderChain", provider)
}

func providerBaseURL(provider string) string {
	if baseURL, ok := ai.ProviderDefaultBaseURL(provider); ok {
		return baseURL
	}
	baseURL, _ := ai.ProviderDefaultBaseURL(string(llm.ProviderOpenAI))
	return baseURL
}

func resolveProviderNode(primaryProvider string, primaryAPIKey string, primaryBaseURL string, provider string) llmProviderNodeConfig {
	provider = strings.ToLower(strings.TrimSpace(provider))
	node := llmProviderNodeConfig{
		Provider: provider,
	}

	if provider == strings.ToLower(strings.TrimSpace(primaryProvider)) {
		node.APIKey = strings.TrimSpace(primaryAPIKey)
		node.BaseURL = strings.TrimSpace(primaryBaseURL)
	}

	for _, envKey := range ai.ProviderAPIKeyEnvVars(provider) {
		if value := strings.TrimSpace(os.Getenv(envKey)); value != "" {
			node.APIKey = value
			break
		}
	}

	for _, envKey := range ai.ProviderBaseURLEnvVars(provider) {
		if value := strings.TrimSpace(os.Getenv(envKey)); value != "" {
			node.BaseURL = value
			break
		}
	}
	if node.BaseURL == "" {
		node.BaseURL = providerBaseURL(provider)
	}

	for _, envKey := range ai.ProviderModelEnvVars(provider) {
		if value := strings.TrimSpace(os.Getenv(envKey)); value != "" {
			node.ModelOverride = value
			break
		}
	}
	if node.ModelOverride == "" {
		if model, ok := ai.ProviderDefaultModel(provider); ok {
			node.DefaultModel = model
		}
	}

	return node
}

// buildLLMProviderClient creates an llm.Client configured for the given provider node.
// It selects the client implementation appropriate for node.Provider (e.g., "anthropic", "mlx", "minimax", "zai"/"zai-coding-plan", "zhipu") and applies the provided HTTP timeout and max retries; unknown providers use an OpenAI-compatible client by default.
func buildLLMProviderClient(node llmProviderNodeConfig, timeout time.Duration, maxRetries int) llm.Client {
	config := llm.ClientConfig{
		Provider:    llm.Provider(node.Provider),
		APIKey:      node.APIKey,
		BaseURL:     node.BaseURL,
		HTTPTimeout: timeout,
		MaxRetries:  maxRetries,
	}

	switch node.Provider {
	case "anthropic":
		return llm.NewAnthropicClient(config)
	case "mlx":
		return llm.NewMLXClient(config)
	case "minimax":
		// MiniMax exposes an Anthropic-compatible endpoint.
		return llm.NewAnthropicClient(config)
	case "zai-coding-plan", "zai":
		// ZAI (models.dev) exposes an OpenAI-compatible endpoint.
		return llm.NewOpenAIClient(config)
	case "zhipu":
		// Zhipu exposes an OpenAI-compatible endpoint.
		return llm.NewOpenAIClient(config)
	default:
		return llm.NewOpenAIClient(config)
	}
}

func providerRequiresAPIKey(provider string) bool {
	return ai.ProviderRequiresAPIKey(provider)
}

type routeRuntimeConfigFile struct {
	CCXT struct {
		Exchanges map[string]struct {
			APIKey     string `json:"api_key"`
			Secret     string `json:"secret"`
			Passphrase string `json:"passphrase"`
		} `json:"exchanges"`
	} `json:"ccxt"`
	Telegram struct {
		ChatID string `json:"chat_id"`
	} `json:"telegram"`
	Services struct {
		Telegram struct {
			ChatID string `json:"chat_id"`
		} `json:"telegram"`
	} `json:"services"`
}

func loadRouteRuntimeConfig() (bitgetAPIKey, bitgetSecret, bitgetPassphrase, chatID string) {
	configPath := strings.TrimSpace(os.Getenv("NEURATRADE_HOME"))
	if configPath != "" {
		configPath = filepath.Join(configPath, "config.json")
	} else {
		homeDir, err := os.UserHomeDir()
		if err != nil {
			return "", "", "", ""
		}
		configPath = filepath.Join(homeDir, ".neuratrade", "config.json")
	}

	// #nosec G304,G703 -- config path is derived from NEURATRADE_HOME or user home
	configFile, err := os.ReadFile(configPath)
	if err != nil {
		return "", "", "", ""
	}

	var cfg routeRuntimeConfigFile
	if err := json.Unmarshal(configFile, &cfg); err != nil {
		log.Printf("WARNING: failed to parse runtime config %s: %v", configPath, err)
		return "", "", "", ""
	}

	if bitget, ok := cfg.CCXT.Exchanges["bitget"]; ok {
		bitgetAPIKey = strings.TrimSpace(bitget.APIKey)
		bitgetSecret = strings.TrimSpace(bitget.Secret)
		bitgetPassphrase = strings.TrimSpace(bitget.Passphrase)
	}

	chatID = strings.TrimSpace(cfg.Telegram.ChatID)
	if chatID == "" {
		chatID = strings.TrimSpace(cfg.Services.Telegram.ChatID)
	}

	return bitgetAPIKey, bitgetSecret, bitgetPassphrase, chatID
}

func hasConnectedExchangeWallet(db *sql.DB, chatID, provider string) bool {
	if db == nil || strings.TrimSpace(chatID) == "" || strings.TrimSpace(provider) == "" {
		return false
	}

	var exists int
	query := `
		SELECT 1
		FROM telegram_operator_wallets
		WHERE chat_id = $1
		  AND LOWER(provider) = LOWER($2)
		  AND status = 'connected'
		LIMIT 1
	`
	if err := db.QueryRow(query, strings.TrimSpace(chatID), strings.TrimSpace(provider)).Scan(&exists); err != nil {
		return false
	}
	return exists == 1
}

func listConnectedExchangeProviders(ctx context.Context, db routeDB, chatID string) []string {
	if db == nil || strings.TrimSpace(chatID) == "" {
		return nil
	}

	rows, err := db.Query(ctx, `
		SELECT DISTINCT LOWER(TRIM(provider))
		FROM telegram_operator_wallets
		WHERE chat_id = $1
		  AND status = 'connected'
		  AND TRIM(provider) <> ''
	`, strings.TrimSpace(chatID))
	if err != nil {
		return nil
	}
	defer rows.Close()

	providers := make([]string, 0, 4)
	for rows.Next() {
		var provider string
		if scanErr := rows.Scan(&provider); scanErr != nil {
			continue
		}
		provider = strings.TrimSpace(strings.ToLower(provider))
		if provider == "" {
			continue
		}
		providers = append(providers, provider)
	}
	return providers
}

func routeEnvEnabled(key string) bool {
	raw := strings.ToLower(strings.TrimSpace(os.Getenv(key)))
	return raw == "1" || raw == "true" || raw == "yes" || raw == "on"
}

func riskLockSourcePriority(source string) int {
	switch strings.TrimSpace(source) {
	case "manual_env":
		return 3
	case "portfolio_safety":
		return 2
	case "drawdown_threshold":
		return 1
	default:
		return 0
	}
}

// SetupRoutes configures HTTP routes, middleware, and application handlers, and initializes runtime services used by the API.
// It returns a cleanup function that should be called on shutdown to stop background resources (for example, the WebSocket handler).
//
//nolint:staticcheck // SA1019: SignalAggregator and TechnicalAnalysisService are deprecated but required for backward compatibility until scalping composer migration completes.
func SetupRoutes(router *gin.Engine, db routeDB, redis *database.RedisClient, ccxtService ccxt.CCXTService, collectorService *services.CollectorService, cleanupService *services.CleanupService, cacheAnalyticsService *services.CacheAnalyticsService, signalAggregator *services.SignalAggregator, analyticsService *services.AnalyticsService, telegramConfig *config.TelegramConfig, aiConfig *config.AIConfig, featuresConfig *config.FeaturesConfig, authMiddleware *middleware.AuthMiddleware, walletValidator *services.WalletValidator, opModeService *services.OperationalModeService, technicalAnalysisService *services.TechnicalAnalysisService) (func(), error) {
	configureLiveReadinessGuard(opModeService)

	// Apply CORS middleware globally
	router.Use(cors.New(cors.Config{
		AllowOrigins:     []string{"*"},
		AllowMethods:     []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
		AllowHeaders:     []string{"Origin", "Content-Type", "Accept", "Authorization", "X-Requested-With"},
		ExposeHeaders:    []string{"Content-Length", "Content-Type"},
		AllowCredentials: false,
		MaxAge:           12 * time.Hour,
	}))

	// Initialize rate limiter (Redis-backed if available, otherwise in-memory)
	var redisClientRaw *redisv9.Client
	if redis != nil {
		redisClientRaw = redis.Client
	}
	rateLimiter := middleware.NewRateLimiter(middleware.DefaultRateLimitConfig(), redisClientRaw, nil)
	router.Use(rateLimiter.Middleware())

	// Initialize admin middleware
	adminMiddleware := middleware.MustNewAdminMiddleware()

	// Initialize health handler
	telegramHealth := handlers.TelegramHealthConfig{}
	if telegramConfig != nil {
		telegramHealth = handlers.TelegramHealthConfig{
			ServiceURL:  telegramConfig.ServiceURL,
			GrpcAddress: telegramConfig.GrpcAddress,
			BotToken:    telegramConfig.BotToken,
		}
	}
	healthHandler := handlers.NewHealthHandlerWithTelegram(db, redis, ccxtService.GetServiceURL(), telegramHealth, cacheAnalyticsService, nil)

	// Health check endpoints with telemetry
	healthGroup := router.Group("/")
	healthGroup.Use(middleware.HealthCheckTelemetryMiddleware())
	{
		healthGroup.GET("/health", gin.WrapF(healthHandler.HealthCheck))
		healthGroup.HEAD("/health", gin.WrapF(healthHandler.HealthCheck))
		healthGroup.GET("/ready", gin.WrapF(healthHandler.ReadinessCheck))
		healthGroup.GET("/live", gin.WrapF(healthHandler.LivenessCheck))
	}

	// Prometheus metrics endpoint
	metricsHandler := handlers.NewPrometheusMetricsHandler()
	metricsHandler.RegisterRoutes(healthGroup)

	// Initialize user handler early for internal routes
	userHandler := handlers.NewUserHandler(db, redisClientRaw, authMiddleware)

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
	arbitrageHandler := handlers.NewArbitrageHandler(db, ccxtService, notificationService, redisClientRaw)
	circuitBreakerHandler := handlers.NewCircuitBreakerHandler(collectorService)

	analysisHandler := handlers.NewAnalysisHandler(db, ccxtService, analyticsService)
	scalpingBacktestHandler := handlers.NewScalpingBacktestHandler(db)

	// Sentiment handler - initialize with config from environment
	sentimentConfig := services.DefaultSentimentServiceConfig()
	sentimentConfig.RedditClientID = os.Getenv("REDDIT_CLIENT_ID")
	sentimentConfig.RedditClientSecret = os.Getenv("REDDIT_CLIENT_SECRET")
	sentimentConfig.CryptoPanicToken = os.Getenv("CRYPTOPANIC_TOKEN")
	sentimentConfig.DBDriver = os.Getenv("DATABASE_DRIVER")
	if sentimentConfig.DBDriver == "" {
		sentimentConfig.DBDriver = "sqlite" // Default to SQLite
	}
	sentimentService := services.NewSentimentService(sentimentConfig, db)
	sentimentHandler := handlers.NewSentimentHandler(sentimentService)

	// userHandler and telegramInternalHandler already initialized above for internal routes
	alertHandler := handlers.NewAlertHandler(db)
	cleanupHandler := handlers.NewCleanupHandler(cleanupService)
	exchangeHandler := handlers.NewExchangeHandler(ccxtService, collectorService, redisClientRaw)
	cacheHandler := handlers.NewCacheHandler(cacheAnalyticsService)
	wsOrigins := []string{"http://localhost", "https://localhost"}
	if o := getEnvOrDefault("NEURATRADE_WS_ALLOWED_ORIGINS", ""); o != "" {
		wsOrigins = strings.Split(o, ",")
		for i := range wsOrigins {
			wsOrigins[i] = strings.TrimSpace(wsOrigins[i])
		}
	}
	webSocketHandler := handlers.NewWebSocketHandler(redis, wsOrigins)

	// AI handler - uses registry from ai package
	aiRegistry := ai.NewRegistry(
		ai.WithRedis(redisClientRaw),
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

	var questStore services.QuestStore
	questStoreMode := "in-memory"
	questStore = services.NewInMemoryQuestStore()
	if db != nil {
		dbQuestStore := services.NewDBQuestStore(db)
		if err := dbQuestStore.InitSchema(context.Background()); err != nil {
			log.Printf("Failed to initialize runtime quest persistence schema, falling back to in-memory store: %v", err)
		} else {
			questStore = dbQuestStore
			questStoreMode = "database"
		}
	}
	questEngine := services.NewQuestEngineWithNotification(questStore, redisClientRaw, notificationService)
	log.Printf("Quest runtime store initialized in %s mode", questStoreMode)

	riskConfig := risk.DefaultRiskManagerConfig()
	riskManager := risk.NewRiskManagerAgent(riskConfig)
	drawdownHalt := services.NewMaxDrawdownHalt(db, services.ResolveMaxDrawdownConfigFromEnv(services.DefaultMaxDrawdownConfig()))

	var dailyLossTracker *risk.DailyLossTracker
	var positionThrottle *risk.PositionSizeThrottle
	if redisClientRaw != nil {
		dailyLossTracker = risk.NewDailyLossTracker(redisClientRaw, risk.DailyLossCapConfig{
			MaxDailyLoss: riskConfig.MaxDailyLoss,
		})
		positionThrottle = risk.NewPositionSizeThrottle(redisClientRaw, risk.DefaultPositionSizeThrottleConfig())
	}

	portfolioSafetyConfig := services.DefaultPortfolioSafetyConfig()
	portfolioSafety := services.NewPortfolioSafetyService(
		portfolioSafetyConfig,
		ccxtService,
		nil,
		riskManager,
		drawdownHalt,
		dailyLossTracker,
		positionThrottle,
		redisClientRaw,
		nil,
	)
	riskLockThreshold := 0.40
	if raw := strings.TrimSpace(os.Getenv("NEURATRADE_RECOVERY_DERISK_ONLY_DRAWDOWN")); raw != "" {
		if parsed, err := strconv.ParseFloat(raw, 64); err == nil && parsed > 0 {
			riskLockThreshold = parsed
		}
	}
	services.SetHeartbeatRiskBridge(func(ctx context.Context) map[string]interface{} {
		reasons := make([]string, 0, 4)
		active := false
		source := "none"
		setSource := func(candidate string) {
			if riskLockSourcePriority(candidate) > riskLockSourcePriority(source) {
				source = candidate
			}
		}

		if routeEnvEnabled("NEURATRADE_QUEST_FORCE_RISK_LOCK") {
			active = true
			setSource("manual_env")
			reasons = append(reasons, "manual_env: NEURATRADE_QUEST_FORCE_RISK_LOCK enabled")
		}

		for _, chatID := range questEngine.ListActiveAutonomousChatIDs() {
			diagnostics := questEngine.GetChatRuntimeDiagnostics(chatID)
			drawdown, ok := diagnosticFloat(diagnostics, "risk_current_drawdown")
			if !ok {
				drawdown, _ = diagnosticFloat(diagnostics, "risk_max_drawdown")
			}
			if drawdown >= riskLockThreshold {
				active = true
				setSource("drawdown_threshold")
				reasons = append(reasons, fmt.Sprintf(
					"drawdown_threshold: chat %s current drawdown %.2f%% >= %.2f%%",
					chatID,
					drawdown*100,
					riskLockThreshold*100,
				))
			}

			exchanges := listConnectedExchangeProviders(ctx, db, chatID)
			if len(exchanges) == 0 {
				continue
			}
			safetyCtx, cancelSafety := context.WithTimeout(ctx, 8*time.Second)
			snapshot, snapshotErr := portfolioSafety.GetPortfolioSnapshot(safetyCtx, chatID, exchanges)
			if snapshotErr != nil {
				cancelSafety()
				log.Printf("[QUEST] Risk bridge snapshot check failed for chat %s: %v", chatID, snapshotErr)
				continue
			}
			safety, safetyErr := portfolioSafety.CheckSafety(safetyCtx, chatID, snapshot)
			cancelSafety()
			if safetyErr != nil {
				log.Printf("[QUEST] Risk bridge safety check failed for chat %s: %v", chatID, safetyErr)
				continue
			}
			if !safety.TradingAllowed {
				active = true
				setSource("portfolio_safety")
				var reason string
				if len(safety.Reasons) > 0 {
					reason = fmt.Sprintf("portfolio_safety: chat %s trading_allowed=false (%s)", chatID, strings.Join(safety.Reasons, "; "))
				} else {
					reason = fmt.Sprintf("portfolio_safety: chat %s trading_allowed=false", chatID)
				}
				reasons = append(reasons, reason)

			}
		}

		questEngine.SetRiskLockStateWithSource(active, source, reasons)
		result := map[string]interface{}{
			"risk_lock":        active,
			"risk_lock_source": source,
			"trading_allowed":  !active,
		}
		if len(reasons) > 0 {
			result["reason"] = reasons[0]
			result["reasons"] = reasons
		}
		return result
	})

	// Legacy quest preload is opt-in only.
	// In scalping-first mode we avoid restoring old active rows without metadata/chat ownership.
	loadLegacyQuests := os.Getenv("NEURATRADE_LOAD_LEGACY_ACTIVE_QUESTS") == "1" ||
		os.Getenv("NEURATRADE_LOAD_LEGACY_ACTIVE_QUESTS") == "true"
	log.Printf("DEBUG: db is nil: %v", db == nil)
	if db != nil && loadLegacyQuests {
		if questStoreMode == "database" {
			log.Println("Skipping legacy active quest preload to keep runtime tables isolated from legacy quests")
		} else {
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
	autonomousMonitoring := services.NewAutonomousMonitorManager(notificationService, telemetry.Logger())

	var sqlDB *sql.DB
	switch concreteDB := db.(type) {
	case *database.SQLiteDB:
		sqlDB = concreteDB.DB
	case *database.PostgresDB:
		sqlDB = concreteDB.SQL
	default:
		log.Printf("Warning: Unknown database type, AI learning disabled")
	}

	runtimeDeps := autonomyruntime.Dependencies{
		TechnicalAnalysis:   technicalAnalysisService,
		CCXTService:         ccxtService,
		ArbitrageService:    arbitrageHandler,
		FuturesArbService:   futuresArbitrageHandler,
		NotificationService: notificationService,
		MonitoringService:   autonomousMonitoring,
		SQLDB:               sqlDB,
	}

	// Create integrated quest runtime handlers through app/autonomy module.
	integratedHandlers, integratedHandlersErr := autonomyruntime.BuildIntegratedHandlers(runtimeDeps)
	if integratedHandlersErr != nil {
		log.Printf("Warning: autonomy runtime rollout store unavailable, using local fallback handlers: %v", integratedHandlersErr)
		integratedHandlers = autonomyruntime.BuildLocalIntegratedHandlers(runtimeDeps)
	}

	var telemetryStore *services.ScalpingTelemetryStore
	if sqlDB != nil {
		telemetryStore = services.NewScalpingTelemetryStoreFromSQLDB(sqlDB, nil)
		if telemetryStore == nil {
			log.Printf("Warning: scalping telemetry store unavailable due to nil SQL database")
		} else {
			schemaCtx, schemaCancel := context.WithTimeout(context.Background(), 5*time.Second)
			err := telemetryStore.EnsureSchema(schemaCtx)
			schemaCancel()
			if err != nil {
				log.Printf("Warning: failed to initialize scalping telemetry store: %v", err)
				telemetryStore = nil
			} else {
				integratedHandlers.SetTelemetryStore(telemetryStore)
			}
		}
	}

	// Wire order executor to integrated handlers for scalping execution
	adminAPIKey := os.Getenv("ADMIN_API_KEY")
	if adminAPIKey == "" {
		log.Printf("WARNING: ADMIN_API_KEY is not set; CCXT order executor requests will be unauthenticated")
	}

	log.Printf("Using Bitget Order Executor for real exchange API calls")

	// Load runtime credentials/chat context from ~/.neuratrade/config.json first.
	bitgetAPIKey, bitgetSecret, bitgetPassphrase, chatID := loadRouteRuntimeConfig()

	// Optional env overrides for ops.
	if val := strings.TrimSpace(os.Getenv("BITGET_API_KEY")); val != "" {
		bitgetAPIKey = val
	}
	if val := strings.TrimSpace(os.Getenv("BITGET_SECRET")); val != "" {
		bitgetSecret = val
	}
	if val := strings.TrimSpace(os.Getenv("BITGET_PASSPHRASE")); val != "" {
		bitgetPassphrase = val
	}
	if val := strings.TrimSpace(os.Getenv("TELEGRAM_CHAT_ID")); val != "" {
		chatID = val
	}

	if chatID == "" {
		log.Printf("WARNING: TELEGRAM_CHAT_ID is not configured in env or ~/.neuratrade/config.json; trade notifications disabled")
	}

	var orderExecutor services.ScalpingOrderExecutor
	var liveOrderExecutor services.ScalpingOrderExecutor
	canUseBitgetCreds := bitgetAPIKey != "" && bitgetSecret != "" && bitgetPassphrase != ""
	if bitgetAPIKey != "" && bitgetSecret != "" && bitgetPassphrase == "" {
		log.Printf("⚠️ Bitget API key/secret detected but passphrase is missing; falling back to paper trading")
	}
	if canUseBitgetCreds && chatID != "" && sqlDB != nil && !hasConnectedExchangeWallet(sqlDB, chatID, "bitget") {
		log.Printf("⚠️ Runtime Bitget credentials do not map to an active Bitget wallet for chat %s; falling back to paper trading", chatID)
		canUseBitgetCreds = false
	}
	if canUseBitgetCreds {
		bitgetExec := services.NewBitgetOrderExecutor(bitgetAPIKey, bitgetSecret, bitgetPassphrase)
		bitgetExec.SetNotificationService(notificationService)
		bitgetExec.SetChatID(chatID)
		liveOrderExecutor = bitgetExec
		log.Printf("✅ Real order execution enabled on Bitget")
	}
	paperOrderExec := services.NewNativeOrderExecutor(ccxtService, bitgetAPIKey, bitgetSecret)
	paperOrderExec.SetNotificationService(notificationService)
	paperOrderExec.SetChatID(chatID)
	if liveOrderExecutor == nil {
		log.Printf("⚠️ Real order execution unavailable at startup; live mode will be blocked until Bitget credentials and wallet mapping are valid")
	}
	orderExecutor = services.NewModeAwareOrderExecutor(liveOrderExecutor, paperOrderExec, opModeService)

	orderExecutor = services.NewSafeOrderExecutor(orderExecutor, portfolioSafety, chatID)
	log.Printf("Portfolio safety gate enabled for scalping order execution")

	integratedHandlers.SetDrawdownHalt(drawdownHalt)
	integratedHandlers.SetOrderExecutor(orderExecutor)
	integratedHandlers.SetOperationalModeService(opModeService)
	questEngine.SetOperationalModeService(opModeService)

	// Set database for user settings lookup
	var lifecycleStore *services.TradingLifecycleStore
	if sqlDB != nil {
		log.Printf("Database available for integrated handlers")
	}
	if db != nil {
		var lifecycleErr error
		lifecycleStore, lifecycleErr = services.NewTradingLifecycleStore(db, log.Default())
		if lifecycleErr != nil {
			log.Printf("Warning: failed to initialize trading lifecycle store: %v", lifecycleErr)
		} else {
			integratedHandlers.SetLifecycleStore(lifecycleStore)
			log.Printf("Trading lifecycle store initialized for autonomous execution persistence")
		}
	}

	var shadowRecorder *services.PaperTradeRecorder
	if db != nil {
		shadowRecorder = services.NewPaperTradeRecorder(db, zapNopServiceLogger{})
	}
	shadowCoordinator := services.NewShadowEvaluationCoordinator(
		db,
		nil,
		nil,
		shadowRecorder,
		nil,
	)
	integratedHandlers.SetShadowEvaluationCoordinator(shadowCoordinator)

	if sqlDB != nil {
		tradeMemory, err := services.NewTradeMemoryWithConfig(
			sqlDB,
			services.ResolveTradeMemoryConfigFromEnv(services.DefaultTradeMemoryConfig()),
		)
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
			aiBaseURL = providerBaseURL(aiConfig.Provider)
		}
		aiProvider = aiConfig.Provider
		if aiProvider == "" {
			aiProvider = "deepseek"
		}
	}

	if aiAPIKey != "" {
		log.Printf("Initializing AI Scalping with primary provider: %s (base_url: %s)", aiProvider, aiBaseURL)

		httpTimeout := 300 * time.Second
		if raw := strings.TrimSpace(os.Getenv("NEURATRADE_AI_HTTP_TIMEOUT_SECONDS")); raw != "" {
			if seconds, parseErr := strconv.Atoi(raw); parseErr == nil && seconds > 0 {
				httpTimeout = time.Duration(seconds) * time.Second
			}
		}
		maxRetries := 5
		if raw := strings.TrimSpace(os.Getenv("NEURATRADE_AI_MAX_RETRIES")); raw != "" {
			if retries, parseErr := strconv.Atoi(raw); parseErr == nil && retries >= 0 {
				maxRetries = retries
			}
		}

		providerChain, chainErr := parseAIProviderChain(aiProvider)
		if chainErr != nil {
			log.Printf("AI provider chain invalid; falling back to primary provider only: %v", chainErr)
			questEngine.SetAIProviderChainStats(0, 0)
			providerChain = nil
			if validateAIProviderName(aiProvider) == nil {
				providerChain = []string{strings.ToLower(strings.TrimSpace(aiProvider))}
			}
		}
		failoverNodes := make([]llm.FailoverNode, 0, len(providerChain))
		for _, provider := range providerChain {
			nodeConfig := resolveProviderNode(aiProvider, aiAPIKey, aiBaseURL, provider)
			if strings.TrimSpace(nodeConfig.APIKey) == "" && providerRequiresAPIKey(provider) {
				log.Printf("Skipping AI provider %s in failover chain: missing API key", provider)
				continue
			}

			client := buildLLMProviderClient(nodeConfig, httpTimeout, maxRetries)
			failoverNodes = append(failoverNodes, llm.FailoverNode{
				Client:        client,
				Provider:      llm.Provider(provider),
				ModelOverride: nodeConfig.ModelOverride,
				DefaultModel:  nodeConfig.DefaultModel,
			})
			overrideMsg := "none"
			if strings.TrimSpace(nodeConfig.ModelOverride) != "" {
				overrideMsg = nodeConfig.ModelOverride
			}
			log.Printf(
				"AI failover node enabled: provider=%s base_url=%s model_override=%s",
				provider,
				nodeConfig.BaseURL,
				overrideMsg,
			)
		}
		questEngine.SetAIProviderChainStats(len(providerChain), len(failoverNodes))

		var llmClient llm.Client
		switch len(failoverNodes) {
		case 0:
			log.Printf("AI provider chain has no usable nodes; AI scalping disabled")
		case 1:
			llmClient = failoverNodes[0].Client
			log.Printf("AI provider chain active with single node: %s", failoverNodes[0].Provider)
		default:
			maxHops := 1
			if raw := strings.TrimSpace(os.Getenv("NEURATRADE_AI_FAILOVER_MAX_HOPS")); raw != "" {
				if parsed, parseErr := strconv.Atoi(raw); parseErr == nil && parsed >= 0 {
					maxHops = parsed
				}
			}
			llmClient = llm.NewFailoverClient(failoverNodes, maxHops)
			log.Printf("AI provider failover enabled: nodes=%d max_hops=%d", len(failoverNodes), maxHops)
		}

		skillRegistry := skill.NewRegistry(filepath.Join(filepath.Dir(""), "skills"))
		if err := skillRegistry.LoadAll(); err != nil {
			log.Printf("Warning: Failed to load skills: %v", err)
		}
		if llmClient != nil {
			integratedHandlers.SetAIScalping(llmClient, skillRegistry)
			log.Printf("AI Scalping service initialized successfully")
		}
	} else {
		log.Printf("AI API key not configured in ~/.neuratrade/config.json, AI scalping disabled")
	}

	if opModeService != nil {
		scalpingLiveProofRequired := aiAPIKey != ""
		opModeService.SetLiveModeGuard(func(ctx context.Context, chatID string) error {
			coordinator := integratedHandlers.AutonomyCoordinator()
			if coordinator == nil {
				if scalpingLiveProofRequired {
					return fmt.Errorf("scalping autonomy coordinator unavailable")
				}
				return nil
			}
			return coordinator.ValidateStrategyMode(ctx, services.ScalpingStrategyID(chatID), autonomous.ModeLive)
		})
		for chatID, err := range opModeService.RevalidateLiveModeGuard(context.Background(), "startup_live_mode_guard") {
			log.Printf("⚠️ Demoted persisted live mode after proof gate revalidation: chat_id=%s err=%v", chatID, err)
		}
	}

	// Register quest runtime via app/autonomy entrypoint before scheduler start.
	if err := autonomyruntime.RegisterQuestRuntime(questEngine, integratedHandlers); err != nil {
		return nil, fmt.Errorf("failed to register quest runtime handlers: %w", err)
	}
	questEngine.Start() // Start the quest engine scheduler

	// Initialize exchange reconciler for position/order resumability
	var reconciler *services.ExchangePositionReconciler
	if db != nil && ccxtService != nil {
		reconciler = services.NewExchangePositionReconciler(
			db,
			ccxtService,
			services.DefaultReconcilerConfig(),
			log.Default(),
		)
		log.Printf("Exchange position reconciler initialized")

		if gin.Mode() == gin.TestMode {
			log.Printf("Startup reconciliation skipped in test mode")
		} else {
			// Mandatory startup reconciliation in non-test runtime.
			// This runs before autonomous chat restore so resumability/protection is refreshed first.
			ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
			results, err := reconciler.ReconcileAll(ctx, services.ReconciliationStartup, "")
			cancel()
			if err != nil {
				log.Printf("Startup reconciliation error: %v", err)
			} else {
				for _, r := range results {
					log.Printf("Startup reconciliation: %s", reconciler.GetReconciliationSummary(&r))
				}
			}
		}

		// Periodic reconciliation defaults to enabled at 300s.
		// Set NEURATRADE_PERIODIC_RECONCILIATION_SECONDS=0 to disable explicitly.
		periodicSeconds := 300
		if periodicRaw := strings.TrimSpace(os.Getenv("NEURATRADE_PERIODIC_RECONCILIATION_SECONDS")); periodicRaw != "" {
			seconds, parseErr := strconv.Atoi(periodicRaw)
			if parseErr != nil {
				log.Printf("Invalid NEURATRADE_PERIODIC_RECONCILIATION_SECONDS=%q, using default %ds", periodicRaw, periodicSeconds)
			} else if seconds == 0 {
				periodicSeconds = 0
			} else if seconds > 0 {
				periodicSeconds = seconds
			} else {
				log.Printf("Invalid NEURATRADE_PERIODIC_RECONCILIATION_SECONDS=%q, using default %ds", periodicRaw, periodicSeconds)
			}
		}
		if periodicSeconds > 0 {
			interval := time.Duration(periodicSeconds) * time.Second
			log.Printf("Periodic reconciliation enabled (interval=%s)", interval)
			go func() {
				ticker := time.NewTicker(interval)
				defer ticker.Stop()
				for range ticker.C {
					ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
					results, err := reconciler.ReconcileAll(ctx, services.ReconciliationPeriodic, "")
					cancel()
					if err != nil {
						log.Printf("Periodic reconciliation error: %v", err)
						continue
					}
					for _, r := range results {
						log.Printf("Periodic reconciliation: %s", reconciler.GetReconciliationSummary(&r))
					}
				}
			}()
		} else {
			log.Printf("Periodic reconciliation disabled (NEURATRADE_PERIODIC_RECONCILIATION_SECONDS=0)")
		}
	}

	// Restore autonomous scalping for operator chats that were enabled via Telegram /begin.
	if db != nil {
		restoreAllChats := strings.EqualFold(strings.TrimSpace(os.Getenv("NEURATRADE_RESTORE_ALL_AUTONOMOUS_CHATS")), "true") ||
			strings.TrimSpace(os.Getenv("NEURATRADE_RESTORE_ALL_AUTONOMOUS_CHATS")) == "1"
		query := "SELECT chat_id FROM telegram_operator_state WHERE autonomous_enabled = TRUE ORDER BY updated_at DESC"
		if !restoreAllChats {
			query += " LIMIT 1"
		}

		rows, err := db.Query(
			context.Background(),
			query,
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
			mode := "latest enabled chat only"
			if restoreAllChats {
				mode = "all enabled chats"
			}
			log.Printf("Restored autonomous scalping for %d chat(s) from telegram_operator_state (%s)", restored, mode)
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
	var autonomousHandler *handlers.AutonomousHandler
	if reconciler != nil {
		autonomousHandler = handlers.NewAutonomousHandlerWithReconciler(questEngine, portfolioSafety, ccxtService.GetSupportedExchanges(), reconciler)
	} else {
		autonomousHandler = handlers.NewAutonomousHandler(questEngine, portfolioSafety, ccxtService.GetSupportedExchanges())
	}
	if lifecycleStore != nil {
		autonomousHandler.SetLifecycleStore(lifecycleStore)
	}
	if telemetryStore != nil {
		autonomousHandler.SetTelemetryStore(telemetryStore)
	}
	telegramInternalHandler := handlers.NewTelegramInternalHandler(db, userHandler, questEngine)

	// Internal service-to-service routes (admin auth required for defense-in-depth)
	internal := router.Group("/internal", adminMiddleware.RequireAdminAuth())
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

	// Initialize operational mode handler (if service provided)
	var opModeHandler *handlers.OperationalModeHandler
	if opModeService != nil {
		opModeHandler = handlers.NewOperationalModeHandler(opModeService)
		log.Printf("Operational mode handler initialized")
	} else {
		log.Printf("WARNING: OperationalModeService is nil, trading mode endpoints disabled")
	}

	sharedKillSwitch := apprisk.NewKillSwitch()
	sharedSafeMode := apprisk.NewSafeMode(apprisk.DefaultSafeModeConfig())

	agentControlHandler := handlers.NewAgentControlHandler(handlers.AgentControlDeps{
		Autonomy:  integratedHandlers.AutonomyCoordinator(),
		Collector: handlers.NewCollectorController(collectorService),
		Risk:      handlers.NewRiskControllerAdapter(sharedKillSwitch, sharedSafeMode),
		Orders:    handlers.NewCCXTOrderCanceller(ccxtService),
	})
	shadowHandler := handlers.NewShadowHandler(integratedHandlers.ShadowEvaluationCoordinator())

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
		// Internal Telegram routes under /api/v1 remain admin-authenticated.
		telegram := v1.Group("/telegram")
		{
			telegramInternal := telegram.Group("/internal")
			telegramInternal.Use(adminMiddleware.RequireAdminAuth())
			{
				telegramInternal.GET("/quests", autonomousHandler.GetQuests)
				telegramInternal.GET("/quests/diagnostics", autonomousHandler.GetQuestDiagnostics)
				telegramInternal.GET("/quests/investigation", autonomousHandler.GetQuestInvestigation)
				telegramInternal.GET("/portfolio", autonomousHandler.GetPortfolio)
				telegramInternal.GET("/logs", autonomousHandler.GetLogs)
				telegramInternal.GET("/performance/summary", autonomousHandler.GetPerformanceSummary)
				telegramInternal.GET("/performance", autonomousHandler.GetPerformanceBreakdown)
				telegramInternal.GET("/ai/status/:chatId", telegramInternalHandler.GetAIStatusByChatID)
				telegramInternal.GET("/users/:id", telegramInternalHandler.GetUserByChatID)
				telegramInternal.GET("/notifications/:userId", telegramInternalHandler.GetNotificationPreferences)
				telegramInternal.POST("/notifications/:userId", telegramInternalHandler.SetNotificationPreferences)
				telegramInternal.POST("/autonomous/begin", telegramInternalHandler.BeginAutonomous)
				telegramInternal.POST("/autonomous/pause", telegramInternalHandler.PauseAutonomous)
				telegramInternal.GET("/doctor", telegramInternalHandler.GetDoctor)

				// Trading mode routes (dry/live toggle)
				if opModeHandler != nil {
					telegramInternal.GET("/mode/:chatId", opModeHandler.GetTradingMode)
					telegramInternal.GET("/mode/:chatId/info", opModeHandler.GetTradingModeInfo)
					telegramInternal.POST("/mode/:chatId", opModeHandler.SetTradingMode)
					telegramInternal.POST("/mode/:chatId/confirm", opModeHandler.AddTradingModeConfirmation)
					telegramInternal.DELETE("/mode/:chatId/confirmations", opModeHandler.ResetTradingModeConfirmations)
				}
			}

			telegramInternalTrade := telegram.Group("/internal")
			telegramInternalTrade.Use(adminMiddleware.RequireAdminAuth())
			{
				telegramInternalTrade.POST("/liquidate", autonomousHandler.Liquidate)
				telegramInternalTrade.POST("/liquidate/all", autonomousHandler.LiquidateAll)
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

		// Paper trading readiness endpoints
		if dbPool, ok := db.(database.DBPool); ok {
			readinessHandler := handlers.NewReadinessHandler(dbPool)
			readiness := v1.Group("/readiness")
			{
				readiness.GET("/paper-trading", readinessHandler.PaperTradingManifest)
				readiness.GET("/paper-trading/evidence", readinessHandler.PaperTradingEvidence)
			}
		}

		// Agent control plane command and event stream APIs (admin-only).
		agent := v1.Group("/agent")
		agent.Use(adminMiddleware.RequireAdminAuth())
		{
			agent.GET("/events", agentControlHandler.StreamEvents)
			agent.POST("/events", agentControlHandler.PublishEvent)
			agent.POST("/pause-exchange", agentControlHandler.PauseExchange)
			agent.POST("/resume-exchange", agentControlHandler.ResumeExchange)
			agent.POST("/enable-safe-mode", agentControlHandler.EnableSafeMode)
			agent.POST("/disable-safe-mode", agentControlHandler.DisableSafeMode)
			agent.POST("/kill-switch", agentControlHandler.EngageKillSwitch)
			agent.POST("/cancel-all-orders", agentControlHandler.CancelAllOrders)
			agent.POST("/strategy-mode", agentControlHandler.SetStrategyMode)
		}

		scalpingShadow := v1.Group("/scalping/shadow")
		scalpingShadow.Use(adminMiddleware.RequireAdminAuth())
		{
			scalpingShadow.GET("/variants", shadowHandler.ListVariants)
			scalpingShadow.GET("/variants/:id/diagnostics", shadowHandler.VariantDiagnostics)
			scalpingShadow.GET("/comparison", shadowHandler.Comparison)
			scalpingShadow.POST("/variants", shadowHandler.CreateVariant)
			scalpingShadow.DELETE("/variants/:id", shadowHandler.DeleteVariant)
		}

		adminRisk := v1.Group("/admin/risk")
		adminRisk.Use(adminMiddleware.RequireAdminAuth())
		{
			adminRisk.POST("/validate_wallet", walletHandler.ValidateWallet)
			adminRisk.POST("/force_resume", func(c *gin.Context) {
				resumed := drawdownHalt.ForceResumeAll(c.Request.Context())
				// Also clear the quest engine risk lock
				questEngine.SetRiskLockState(false, nil)
				c.JSON(http.StatusOK, gin.H{
					"success":       true,
					"message":       "Trading resumed for all halted accounts",
					"resumed_count": len(resumed),
					"accounts":      resumed,
				})
			})
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

		backtest := v1.Group("/backtest")
		backtest.Use(authMiddleware.RequireAuth())
		{
			scalpingBacktest := backtest.Group("/scalping")
			scalpingBacktest.POST("/run", scalpingBacktestHandler.RunScalpingBacktest)
			scalpingBacktest.GET("/:run_id", scalpingBacktestHandler.GetScalpingBacktest)
			scalpingBacktest.GET("/", scalpingBacktestHandler.ListScalpingBacktests)
			scalpingBacktest.POST("/compare", scalpingBacktestHandler.CompareScalpingBacktests)
		}

		// AI model routes
		ai := v1.Group("/ai")
		{
			// Public read/routing endpoints used by external clients.
			ai.GET("/models", aiHandler.GetModels)
			ai.GET("/providers", aiHandler.GetProviders)
			ai.GET("/providers/:providerId/models", aiHandler.GetProviderModels)
			ai.POST("/route", aiHandler.RouteModel)

			// User-specific model operations require authentication.
			aiProtected := ai.Group("/")
			aiProtected.Use(authMiddleware.RequireAuth())
			{
				aiProtected.POST("/select/:userId", aiHandler.SelectModel)
				aiProtected.GET("/status/:userId", aiHandler.GetModelStatus)
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
	}, nil
}

// Placeholder handlers - to be implemented

// Arbitrage handlers are now implemented in handlers/arbitrage.go
// Technical analysis handlers are now implemented in handlers/analysis.go
// Alert handlers are now implemented in handlers/alert.go
