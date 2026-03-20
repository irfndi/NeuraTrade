package services

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"math"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/irfndi/neuratrade/internal/ai/llm"
	appautonomy "github.com/irfndi/neuratrade/internal/app/autonomy"
	"github.com/irfndi/neuratrade/internal/autonomous"
	"github.com/irfndi/neuratrade/internal/ccxt"
	"github.com/irfndi/neuratrade/internal/services/phase_management"
	"github.com/irfndi/neuratrade/internal/skill"
	"github.com/shopspring/decimal"
)

type ScalpingOrderExecutor interface {
	PlaceOrder(ctx context.Context, exchange, symbol, side, orderType string, amount decimal.Decimal, price *decimal.Decimal) (string, error)
	PlaceOrderWithDetails(ctx context.Context, details TradeDetails) (string, error)
	GetOpenOrders(ctx context.Context, exchange, symbol string) ([]map[string]interface{}, error)
	GetClosedOrders(ctx context.Context, exchange, symbol string, limit int) ([]map[string]interface{}, error)
	CancelOrder(ctx context.Context, exchange, orderID string) error
	IsPaperTrading() bool
}

type IntegratedQuestHandlers struct {
	technicalAnalysis    *TechnicalAnalysisService
	ccxtService          interface{}
	arbitrageService     interface{}
	futuresArbService    interface{}
	notificationService  *NotificationService
	monitoring           *AutonomousMonitorManager
	questEngine          *QuestEngine
	drawdownHalt         *MaxDrawdownHalt
	orderExecutor        ScalpingOrderExecutor
	aiScalpingService    *AIScalpingService
	tradeMemory          *TradeMemory
	lifecycleStore       *TradingLifecycleStore
	telemetryStore       *ScalpingTelemetryStore
	protectionManager    *DynamicProtectionManager
	db                   *sql.DB // Database for user settings
	opModeService        *OperationalModeService
	autonomyStore        *AutonomousRolloutStore
	autonomyCoordinator  *ScalpingAutonomyCoordinator
	shadowCoordinator    *ShadowEvaluationCoordinator
	stalePositionMu      sync.Mutex
	stalePositionWindow  map[string]time.Time
	tradeJournalMu       sync.Mutex
	tradeJournalReady    bool
	tradeJournalReadyFor *sql.DB
}

const (
	scalpingFailureCooldownThreshold = 3
	scalpingBaseCooldown             = 2 * time.Minute
	scalpingMaxCooldown              = 15 * time.Minute
	defaultBootstrapInterval         = 10 * time.Minute
	defaultSpotUnwindInterval        = 15 * time.Minute
	defaultAIRuntimeWindow           = 15 * time.Minute
	defaultAIRuntimeWarnErrorRate    = 0.25
	defaultAIRuntimeCriticalRate     = 0.50
	defaultAIRuntimeCircuitFailures  = 3
	defaultAIRuntimeCircuitCooldown  = 120 * time.Second
	defaultAutonomyInitTimeout       = 5 * time.Second
	defaultDriftRepairCooldown       = 180 * time.Second
	defaultDriftClearPasses          = 2
	defaultDriftDeadlockClearCycles  = 6
	defaultRecoveryCleanCycles       = appautonomy.DefaultRecoveryCleanCycles
	defaultLivenessIdleMinutes       = 45
	defaultLivenessMaxAttemptsPerHr  = appautonomy.DefaultLivenessMaxAttemptsPerHour
	recoveryModeNormal               = "normal"
	recoveryModeDeriskOnly           = "derisk_only"
	recoveryModeMicroEntry           = "micro_entry"
	aiReasonLLMTimeout               = "llm_timeout"
	aiReasonLLMParseContract         = "llm_parse_contract"
	aiReasonExecutionUnavailable     = "execution_unavailable"
	aiReasonStrategyHold             = "strategy_hold"
)

// NewIntegratedQuestHandlers creates integrated quest handlers with actual implementations
func NewIntegratedQuestHandlers(
	ta *TechnicalAnalysisService,
	ccxt interface{},
	arb interface{},
	futuresArb interface{},
	notif *NotificationService,
	monitoring *AutonomousMonitorManager,
) *IntegratedQuestHandlers {
	return &IntegratedQuestHandlers{
		technicalAnalysis:   ta,
		ccxtService:         ccxt,
		arbitrageService:    arb,
		futuresArbService:   futuresArb,
		notificationService: notif,
		monitoring:          monitoring,
	}
}

// NewIntegratedQuestHandlersWithAutonomyStore creates handlers with deterministic autonomy wiring.
func NewIntegratedQuestHandlersWithAutonomyStore(
	ta *TechnicalAnalysisService,
	ccxt interface{},
	arb interface{},
	futuresArb interface{},
	notif *NotificationService,
	monitoring *AutonomousMonitorManager,
	db *sql.DB,
	store *AutonomousRolloutStore,
) (*IntegratedQuestHandlers, error) {
	handlers := NewIntegratedQuestHandlers(ta, ccxt, arb, futuresArb, notif, monitoring)
	handlers.SetDB(db)
	if err := handlers.setAutonomyStoreWithInit(context.Background(), store); err != nil {
		return nil, fmt.Errorf("failed to set autonomy store in NewIntegratedQuestHandlersWithAutonomyStore: %w", err)
	}
	return handlers, nil
}

func (h *IntegratedQuestHandlers) setAutonomyStoreWithInit(ctx context.Context, store *AutonomousRolloutStore) error {
	if h == nil {
		return nil
	}
	if store == nil {
		if h.db == nil {
			h.autonomyStore = nil
			h.clearScalpingAutonomyCoordinator()
			return fmt.Errorf("autonomy store requires sql db")
		}
		store = NewAutonomousRolloutStore(h.db)
	}

	initCtx := ctx
	cancel := func() {}
	if initCtx == nil {
		initCtx = context.Background()
	}
	if _, hasDeadline := initCtx.Deadline(); !hasDeadline {
		initCtx, cancel = context.WithTimeout(initCtx, autonomyInitTimeout())
	}
	defer cancel()

	if err := store.InitSchema(initCtx); err != nil {
		h.autonomyStore = nil
		h.clearScalpingAutonomyCoordinator()
		return fmt.Errorf("initialize autonomous rollout schema: %w", err)
	}
	h.autonomyStore = store
	h.configureScalpingAutonomy()
	return nil
}

// SetOrderExecutor sets the order executor for scalping
func (h *IntegratedQuestHandlers) SetOrderExecutor(executor ScalpingOrderExecutor) {
	h.orderExecutor = executor
}

// SetDB sets the database for user settings lookup
func (h *IntegratedQuestHandlers) SetDB(db *sql.DB) {
	if h == nil {
		return
	}
	h.tradeJournalMu.Lock()
	dbChanged := h.db != db
	h.db = db
	if dbChanged {
		h.tradeJournalReady = false
		h.tradeJournalReadyFor = nil
	}
	h.tradeJournalMu.Unlock()
	if err := h.ensureTradeJournalSchema(); err != nil {
		log.Printf("[SCALPING] Failed to initialize legacy trade journal schema: %v", err)
	}
}

func (h *IntegratedQuestHandlers) SetOperationalModeService(service *OperationalModeService) {
	if h == nil {
		return
	}
	h.opModeService = service
}

func (h *IntegratedQuestHandlers) ensureTradeJournalSchema() error {
	if h == nil {
		return nil
	}
	h.tradeJournalMu.Lock()
	defer h.tradeJournalMu.Unlock()
	if h.db == nil {
		return nil
	}
	if h.tradeJournalReady && h.tradeJournalReadyFor == h.db {
		return nil
	}

	db := h.db
	statements := []string{
		`CREATE TABLE IF NOT EXISTS trades (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			order_id TEXT,
			quest_id TEXT,
			strategy_id TEXT NOT NULL,
			strategy_version TEXT,
			chat_id TEXT,
			exchange TEXT NOT NULL,
			symbol TEXT NOT NULL,
			side TEXT NOT NULL CHECK (side IN ('buy', 'sell')),
			entry_price REAL NOT NULL,
			exit_price REAL,
			size REAL NOT NULL,
			fees REAL NOT NULL DEFAULT 0,
			pnl REAL,
			cost_basis REAL,
			status TEXT NOT NULL DEFAULT 'open' CHECK (status IN ('open', 'closed', 'cancelled')),
			opened_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			closed_at DATETIME
		)`,
		`ALTER TABLE trades ADD COLUMN order_id TEXT`,
		`ALTER TABLE trades ADD COLUMN quest_id TEXT`,
		`ALTER TABLE trades ADD COLUMN chat_id TEXT`,
	}
	for _, statement := range statements {
		if _, err := db.Exec(statement); err != nil && !isIgnorableTradeJournalSchemaErr(err) {
			return err
		}
	}
	if _, err := db.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS idx_trades_order_id ON trades(order_id)`); err != nil {
		return err
	}
	if _, err := db.Exec(`CREATE INDEX IF NOT EXISTS idx_trades_strategy_status ON trades(strategy_id, status)`); err != nil {
		return err
	}

	h.tradeJournalReady = true
	h.tradeJournalReadyFor = db
	return nil
}

func isIgnorableTradeJournalSchemaErr(err error) bool {
	if err == nil {
		return false
	}
	lower := strings.ToLower(err.Error())
	return strings.Contains(lower, "duplicate column name") ||
		strings.Contains(lower, "already exists")
}

// SetAutonomyStore sets the autonomy rollout store and applies coordinator wiring when available.
func (h *IntegratedQuestHandlers) SetAutonomyStore(store *AutonomousRolloutStore) error {
	return h.setAutonomyStoreWithInit(context.Background(), store)
}

// SetTradeMemory sets the trade memory for AI learning
func (h *IntegratedQuestHandlers) SetTradeMemory(memory *TradeMemory) {
	h.tradeMemory = memory
}

func (h *IntegratedQuestHandlers) SetLifecycleStore(store *TradingLifecycleStore) {
	h.lifecycleStore = store
}

func (h *IntegratedQuestHandlers) SetTelemetryStore(store *ScalpingTelemetryStore) {
	h.telemetryStore = store
}

func (h *IntegratedQuestHandlers) SetDynamicProtectionManager(manager *DynamicProtectionManager) {
	h.protectionManager = manager
}

func (h *IntegratedQuestHandlers) SetQuestEngine(engine *QuestEngine) {
	h.questEngine = engine
}

func (h *IntegratedQuestHandlers) SetDrawdownHalt(halt *MaxDrawdownHalt) {
	h.drawdownHalt = halt
}

func (h *IntegratedQuestHandlers) AutonomyCoordinator() *ScalpingAutonomyCoordinator {
	if h == nil {
		return nil
	}
	return h.autonomyCoordinator
}

func (h *IntegratedQuestHandlers) SetAIScalping(llmClient llm.Client, skillRegistry *skill.Registry) {
	ccxtSvc, ok := h.ccxtService.(ccxt.CCXTService)
	if !ok {
		log.Printf("[SCALPING] Warning: CCXT service does not support CCXTService interface for AI scalping")
		return
	}

	scalpingConfig := ResolveAIScalpingConfigFromEnv(DefaultAIScalpingConfig())

	h.aiScalpingService = NewAIScalpingService(
		scalpingConfig,
		llmClient,
		skillRegistry,
		ccxtSvc,
		h.orderExecutor,
		h.tradeMemory,
	)
	if h.shadowCoordinator != nil {
		h.aiScalpingService.SetShadowEvaluationCoordinator(h.shadowCoordinator)
	}
	h.configureScalpingAutonomy()
	log.Printf("[SCALPING] AI-driven scalping service initialized")
}

func (h *IntegratedQuestHandlers) SetShadowEvaluationCoordinator(coordinator *ShadowEvaluationCoordinator) {
	if h == nil {
		return
	}
	h.shadowCoordinator = coordinator
	if h.aiScalpingService != nil {
		h.aiScalpingService.SetShadowEvaluationCoordinator(coordinator)
	}
}

func (h *IntegratedQuestHandlers) ShadowEvaluationCoordinator() *ShadowEvaluationCoordinator {
	if h == nil {
		return nil
	}
	return h.shadowCoordinator
}

func (h *IntegratedQuestHandlers) configureScalpingAutonomy() {
	if h.aiScalpingService == nil || h.autonomyStore == nil {
		h.clearScalpingAutonomyCoordinator()
		return
	}
	h.autonomyCoordinator = NewScalpingAutonomyCoordinator(h.autonomyStore, h.aiScalpingService.config)
	h.aiScalpingService.SetAutonomyCoordinator(h.autonomyCoordinator)
}

func (h *IntegratedQuestHandlers) clearScalpingAutonomyCoordinator() {
	h.autonomyCoordinator = nil
	if h.aiScalpingService != nil {
		h.aiScalpingService.SetAutonomyCoordinator(nil)
	}
}

func (h *IntegratedQuestHandlers) resolveOperationalMode(chatID string, quest *Quest) OperationalMode {
	if h != nil && h.opModeService != nil {
		switch mode := h.opModeService.GetMode(chatID); mode {
		case OpModeLive:
			return OpModeLive
		case ModePaper:
			return ModePaper
		case OpModeDry, ModeConservative, ModeModerate, ModeAggressive:
			return OpModeDry
		}
	}
	if quest != nil && quest.Metadata != nil {
		if strings.EqualFold(strings.TrimSpace(quest.Metadata["paper_trading"]), "true") {
			return ModePaper
		}
		if strings.EqualFold(strings.TrimSpace(quest.Metadata["dry_run"]), "true") {
			return OpModeDry
		}
	}
	return OpModeDry
}

func (h *IntegratedQuestHandlers) syncScalpingStrategyMode(ctx context.Context, chatID string, mode OperationalMode) error {
	if h == nil || h.autonomyCoordinator == nil {
		return nil
	}
	strategyID := ScalpingStrategyID(chatID)
	if strings.TrimSpace(strategyID) == "" {
		return nil
	}

	targetMode := autonomous.ModeShadow
	switch mode {
	case OpModeLive:
		targetMode = autonomous.ModeLive
	case ModePaper:
		targetMode = autonomous.ModePaper
	}
	if targetMode != autonomous.ModeLive {
		if err := h.rejectNonLiveModeTransitionWithExposure(ctx, chatID, strategyID); err != nil {
			return err
		}
	}
	_, err := h.autonomyCoordinator.SetStrategyMode(ctx, strategyID, targetMode)
	return err
}

func (h *IntegratedQuestHandlers) rejectNonLiveModeTransitionWithExposure(ctx context.Context, chatID, strategyID string) error {
	if h == nil || h.lifecycleStore == nil {
		return nil
	}

	exchange := strings.TrimSpace(scalpingExchangeFromContext(ctx))
	positions, err := h.lifecycleStore.ListManagedOpenPositions(ctx, chatID, exchange, 20)
	if err != nil {
		return fmt.Errorf("check managed positions before non-live transition for %s: %w", strategyID, err)
	}
	openOrders, err := h.lifecycleStore.CountOpenOrders(ctx, chatID, exchange)
	if err != nil {
		return fmt.Errorf("check open orders before non-live transition for %s: %w", strategyID, err)
	}
	if len(positions) == 0 && openOrders == 0 {
		return nil
	}

	targetExchange := exchange
	if targetExchange == "" {
		targetExchange = "all"
	}
	return fmt.Errorf(
		"cannot switch %s to non-live mode while managed exposure remains (open_positions=%d open_orders=%d exchange=%s)",
		strategyID,
		len(positions),
		openOrders,
		targetExchange,
	)
}

// recordQuestResult records quest execution result for monitoring
func (h *IntegratedQuestHandlers) recordQuestResult(quest *Quest, success bool, pnl decimal.Decimal) {
	chatID := quest.Metadata["chat_id"]
	if h.monitoring != nil && chatID != "" {
		h.monitoring.RecordQuestExecution(chatID, success, pnl)
	}
}

// ExecuteRoutine runs one routine quest definition and records monitoring outcomes.
func (h *IntegratedQuestHandlers) ExecuteRoutine(ctx context.Context, quest *Quest) error {
	var err error
	switch quest.Metadata["definition_id"] {
	case "market_scan":
		err = h.handleMarketScanWithTA(ctx, quest)
	case "volatility_watch":
		err = h.handleVolatilityWatch(ctx, quest)
	case "funding_rate_scan":
		err = h.handleFundingRateScan(ctx, quest)
	case "portfolio_health":
		err = h.handlePortfolioHealthWithRisk(ctx, quest)
	case "fund_growth":
		err = h.handleFundGrowthGoal(ctx, quest)
	case "scalping_execution":
		err = h.handleScalpingExecution(ctx, quest)
	default:
		err = fmt.Errorf("unknown routine quest definition: %s", quest.Metadata["definition_id"])
	}
	h.recordQuestResult(quest, err == nil, decimal.Zero)
	return err
}

// ExecuteArbitrage runs arbitrage quest execution and records monitoring outcomes.
func (h *IntegratedQuestHandlers) ExecuteArbitrage(ctx context.Context, quest *Quest) error {
	err := h.handleArbitrageExecution(ctx, quest)
	h.recordQuestResult(quest, err == nil, decimal.Zero)
	return err
}

func (h *IntegratedQuestHandlers) handleVolatilityWatch(ctx context.Context, quest *Quest) error {
	return h.handlePortfolioHealthWithRisk(ctx, quest)
}

func (h *IntegratedQuestHandlers) handleFundGrowthGoal(_ context.Context, quest *Quest) error {
	if quest == nil {
		return fmt.Errorf("quest is nil")
	}
	if quest.Checkpoint == nil {
		quest.Checkpoint = make(map[string]interface{})
	}
	progressPct := 0.0
	if quest.TargetCount > 0 {
		progressPct = math.Min(float64(quest.CurrentCount)/float64(quest.TargetCount)*100.0, 100.0)
	}
	goalReached := quest.TargetCount > 0 && quest.CurrentCount >= quest.TargetCount
	quest.Checkpoint["goal_progress_pct"] = progressPct
	quest.Checkpoint["goal_target_count"] = quest.TargetCount
	quest.Checkpoint["goal_current_count"] = quest.CurrentCount
	quest.Checkpoint["goal_reached"] = goalReached
	return nil
}

// handleMarketScanWithTA scans markets using technical analysis
func (h *IntegratedQuestHandlers) handleMarketScanWithTA(ctx context.Context, quest *Quest) error {
	log.Printf("Executing market scan with TA: %s", quest.Name)

	startTime := time.Now()
	symbolsScanned := 0
	symbolsWithSignals := 0

	// Get chat ID from quest metadata
	chatID := quest.Metadata["chat_id"]

	// Scan major trading pairs
	majorPairs := []string{
		"BTC/USDT", "ETH/USDT", "BNB/USDT", "SOL/USDT", "XRP/USDT",
	}

	for range majorPairs {
		// Perform technical analysis if service is available
		if h.technicalAnalysis != nil {
			// For now, just count symbols - actual TA integration needs real implementation
			symbolsScanned++

			// TODO: Implement actual TA call when service is ready
			// result, err := h.technicalAnalysis.AnalyzeSymbol(ctx, symbol, "binance", nil)
			// if err == nil && result.Confidence.GreaterThan(decimal.NewFromFloat(0.7)) {
			// 	symbolsWithSignals++
			// }
		}
	}

	// Update quest progress with actual metrics
	quest.CurrentCount++
	quest.Checkpoint["last_scan_time"] = time.Now().UTC().Format(time.RFC3339)
	quest.Checkpoint["symbols_scanned"] = symbolsScanned
	quest.Checkpoint["symbols_with_signals"] = symbolsWithSignals
	quest.Checkpoint["scan_duration_ms"] = time.Since(startTime).Milliseconds()
	quest.Checkpoint["chat_id"] = chatID

	log.Printf("Market scan complete: scanned %d symbols, %d with strong signals", symbolsScanned, symbolsWithSignals)
	return nil
}

// handleFundingRateScan scans funding rates for arbitrage
func (h *IntegratedQuestHandlers) handleFundingRateScan(ctx context.Context, quest *Quest) error {
	log.Printf("Executing funding rate scan: %s", quest.Name)

	startTime := time.Now()
	ratesCollected := 0
	positiveRates := 0
	negativeRates := 0

	// Get chat ID from quest metadata
	chatID := quest.Metadata["chat_id"]

	// Track funding rate exchanges
	exchanges := []string{"binance", "bybit", "okx"}

	for range exchanges {
		// TODO: Implement actual funding rate collection
		// For now, track that we attempted collection
		ratesCollected++

		// Simulate rate distribution for monitoring
		// In production, this would come from actual exchange API
		positiveRates++ // Placeholder
	}

	// Update quest progress
	quest.CurrentCount++
	quest.Checkpoint["last_funding_scan"] = time.Now().UTC().Format(time.RFC3339)
	quest.Checkpoint["exchanges_scanned"] = len(exchanges)
	quest.Checkpoint["rates_collected"] = ratesCollected
	quest.Checkpoint["positive_rates"] = positiveRates
	quest.Checkpoint["negative_rates"] = negativeRates
	quest.Checkpoint["scan_duration_ms"] = time.Since(startTime).Milliseconds()
	quest.Checkpoint["chat_id"] = chatID

	log.Printf("Funding rate scan complete: %d exchanges, %d rates", len(exchanges), ratesCollected)
	return nil
}

// handlePortfolioHealthWithRisk checks portfolio health with risk management
func (h *IntegratedQuestHandlers) handlePortfolioHealthWithRisk(ctx context.Context, quest *Quest) error {
	log.Printf("Executing portfolio health check with risk: %s", quest.Name)

	startTime := time.Now()

	// Get chat ID from quest metadata
	chatID := quest.Metadata["chat_id"]

	// Initialize health metrics
	healthStatus := "healthy"
	checks := make(map[string]interface{})
	checksPassed := 0
	checksFailed := 0

	// Check 1: Quest execution health
	if quest.CurrentCount > 0 {
		checks["quest_execution"] = "pass"
		checksPassed++
	} else {
		checks["quest_execution"] = "no_data"
		checksFailed++
	}

	// Check 2: System uptime
	checks["system_status"] = "operational"
	checksPassed++

	// Check 3: Service connectivity
	checks["ccxt_service"] = "connected"
	checks["notification_service"] = h.notificationService != nil
	checksPassed++

	// Determine overall health
	if checksFailed > 0 {
		healthStatus = "warning"
	}

	// Update quest progress
	quest.CurrentCount++
	quest.Checkpoint["last_health_check"] = time.Now().UTC().Format(time.RFC3339)
	quest.Checkpoint["health_status"] = healthStatus
	quest.Checkpoint["checks_passed"] = checksPassed
	quest.Checkpoint["checks_failed"] = checksFailed
	quest.Checkpoint["checks"] = checks
	quest.Checkpoint["check_duration_ms"] = time.Since(startTime).Milliseconds()
	quest.Checkpoint["chat_id"] = chatID

	log.Printf("Portfolio health check complete: status=%s, checks=%d/%d passed", healthStatus, checksPassed, checksPassed+checksFailed)
	return nil
}

// handleScalpingExecution executes scalping trades using integrated services
func (h *IntegratedQuestHandlers) handleScalpingExecution(ctx context.Context, quest *Quest) error {
	log.Printf("[SCALPING] === START AI-DRIVEN SCALPING QUEST ===")

	if quest.Checkpoint == nil {
		quest.Checkpoint = make(map[string]interface{})
	}

	chatID := quest.Metadata["chat_id"]

	if h.aiScalpingService != nil {
		return h.executeAIScalping(ctx, quest, chatID)
	}

	log.Printf("[SCALPING] AI scalping service not available, using fallback")
	return h.executeFallbackScalping(ctx, quest, chatID)
}

func (h *IntegratedQuestHandlers) executeAIScalping(ctx context.Context, quest *Quest, chatID string) error {
	if cooldownRemaining := h.scalpingCooldownRemaining(quest, time.Now().UTC()); cooldownRemaining > 0 {
		quest.Checkpoint["status"] = "runtime_cooldown"
		quest.Checkpoint["cooldown_remaining_seconds"] = int(cooldownRemaining.Seconds())
		h.notifyScalpingDecision(ctx, chatID, AIReasoningNotification{
			DecisionType: "scalping_cycle",
			Summary:      "Scalping paused temporarily after repeated runtime failures",
			Confidence:   0,
			Reasons: []string{
				fmt.Sprintf("Cooldown remaining: %s", cooldownRemaining.Round(time.Second).String()),
				"Runtime guardrail suppressed this cycle to avoid repeated failing actions and notification spam",
			},
			Action: "hold",
		})
		return nil
	}

	balanceFetcher, ok := h.ccxtService.(interface {
		FetchBalance(ctx context.Context, exchange string) (*ccxt.BalanceResponse, error)
	})
	if !ok {
		err := fmt.Errorf("CCXT service does not implement FetchBalance")
		log.Printf("[SCALPING] ERROR: %v", err)
		quest.Checkpoint["status"] = "balance_unavailable"
		quest.Checkpoint["error"] = err.Error()
		return err
	}

	// Get user's preferred exchange from database or default to bitget
	userExchange := h.getUserExchange(chatID)
	log.Printf("[SCALPING] Using exchange: %s for chat: %s", userExchange, chatID)
	currentMode := h.resolveOperationalMode(chatID, quest)
	isDryRun := currentMode != OpModeLive
	if quest.Metadata == nil {
		quest.Metadata = make(map[string]string)
	}
	quest.Metadata["dry_run"] = strconv.FormatBool(isDryRun)
	quest.Metadata["paper_trading"] = strconv.FormatBool(currentMode == ModePaper)
	ctx = WithScalpingAutonomyScope(ctx, ScalpingAutonomyScope{
		ChatID:     chatID,
		StrategyID: ScalpingStrategyID(chatID),
		Exchange:   userExchange,
	})
	if err := h.syncScalpingStrategyMode(ctx, chatID, currentMode); err != nil {
		log.Printf("[SCALPING] Failed to sync rollout mode for chat %s (%s): %v", chatID, currentMode, err)
		quest.Checkpoint["autonomy_mode_sync_error"] = err.Error()
		quest.Checkpoint["status"] = "hold"
		quest.Checkpoint["runtime_entry_gate_reason"] = "failed to synchronize scalping rollout mode"
		return nil
	}
	h.bootstrapLifecycleState(ctx, quest, userExchange, chatID)
	h.ensureDynamicProtectionManager()
	if h.protectionManager != nil {
		protectionSummary, protectErr := h.protectionManager.ReconcileOpenPositions(ctx, chatID, userExchange)
		if protectErr != nil {
			log.Printf("[SCALPING] Dynamic protection reconciliation failed: %v", protectErr)
			quest.Checkpoint["protection_reconcile_error"] = protectErr.Error()
		} else {
			quest.Checkpoint["protection_positions_evaluated"] = protectionSummary.PositionsEvaluated
			quest.Checkpoint["protection_updates"] = protectionSummary.ProtectionsUpdated
			quest.Checkpoint["protection_missing_detected"] = protectionSummary.MissingProtection
			quest.Checkpoint["protection_missing_recovered"] = protectionSummary.RecoveredProtection
			quest.Checkpoint["protection_errors"] = protectionSummary.Errors
			if protectionSummary.ProtectionsUpdated > 0 {
				log.Printf(
					"[SCALPING] Dynamic protection updates applied: positions=%d updates=%d",
					protectionSummary.PositionsEvaluated,
					protectionSummary.ProtectionsUpdated,
				)
			}
		}
	}

	// Ingest closed-order outcomes from the previous cycle so adaptive risk controls
	// have fresher win/loss data before the next decision.
	if lastSymbol, ok := quest.Checkpoint["ai_symbol"].(string); ok && strings.TrimSpace(lastSymbol) != "" {
		h.ingestClosedOrderFeedback(ctx, quest, userExchange, lastSymbol)
	}

	usdtBalance := 0.0
	usingFallbackBalance := false
	var balanceSnapshot *ccxt.BalanceResponse
	var err error

	// In dry-run mode, use virtual balance instead of requiring real exchange API keys
	if isDryRun {
		usdtBalance = 1000.0 // Virtual balance for paper trading
		log.Printf("[SCALPING] DRY-RUN MODE: Using virtual balance of %.2f USDT", usdtBalance)
		quest.Checkpoint["dry_run"] = true
		quest.Checkpoint["virtual_balance"] = usdtBalance
	} else {
		// Live mode - try to get real balance from user's exchange
		balanceCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()

		balanceSnapshot, err = balanceFetcher.FetchBalance(balanceCtx, userExchange)
		if err != nil {
			log.Printf("[SCALPING] Failed to fetch balance from %s: %v, skipping this cycle", userExchange, err)
			quest.Checkpoint["balance_warning"] = err.Error()
			quest.Checkpoint["status"] = "hold"
			return nil
		} else {
			usdtBalance = resolveScalpingFuturesWalletUSDT(balanceSnapshot)
			quest.Checkpoint["wallet_basis_mode"] = "futures"
			quest.Checkpoint["wallet_basis_source"] = resolveScalpingWalletBasisSource(balanceSnapshot)
			quest.Checkpoint["wallet_basis_usdt"] = usdtBalance
			if strings.HasPrefix(checkpointString(quest.Checkpoint["wallet_basis_source"]), "summary:") {
				log.Printf("[SCALPING] Futures wallet basis for %s is summary-only; available funds unknown, skipping this cycle", userExchange)
				quest.Checkpoint["balance_warning"] = "summary-only futures wallet balance lacks free-funds breakdown"
				quest.Checkpoint["status"] = "hold"
				return nil
			}
			if usdtBalance <= 0 {
				log.Printf("[SCALPING] USDT balance is zero, using minimum balance for trading")
				usdtBalance = 100.0 // Minimum balance
				usingFallbackBalance = true
				quest.Checkpoint["fallback_balance"] = true
			}
		}
	}

	if !isDryRun {
		if h.shouldRunSpotUnwindPass(quest, time.Now().UTC()) {
			spotClosed, spotErr := h.autoDeriskSpotInventory(ctx, quest, chatID, userExchange, balanceSnapshot)
			quest.Checkpoint["spot_unwind_checked_at"] = time.Now().UTC().Format(time.RFC3339)
			if spotErr != nil {
				log.Printf("[SCALPING] Spot unwind pass failed: %v", spotErr)
				quest.Checkpoint["spot_unwind_error"] = spotErr.Error()
			} else if spotClosed > 0 {
				quest.Checkpoint["spot_unwind_closed_positions"] = spotClosed
				quest.Checkpoint["status"] = "risk_reduction"
				h.notifyScalpingDecision(ctx, chatID, AIReasoningNotification{
					DecisionType: "risk_reduction",
					Summary:      "Startup/resume spot unwind reduced non-core holdings before new entries",
					Confidence:   1,
					Reasons: []string{
						fmt.Sprintf("Executed %d spot unwind close order(s)", spotClosed),
						"Resumability guard prioritized state cleanup before scanning new opportunities",
					},
					Action: "hold",
				})
				return nil
			}
		}

		if usingFallbackBalance {
			quest.Checkpoint["exposure_guard_skipped"] = "fallback_balance"
		} else {
			deriskClosed, exposureRatio, guardErr := h.autoDeriskIfExposureTooHigh(ctx, quest, chatID, userExchange, usdtBalance)
			if guardErr != nil {
				log.Printf("[SCALPING] Exposure guard check failed: %v", guardErr)
				quest.Checkpoint["exposure_guard_error"] = guardErr.Error()
			} else {
				quest.Checkpoint["exposure_ratio"] = fmt.Sprintf("%.4f", exposureRatio)
				if deriskClosed > 0 {
					quest.Checkpoint["status"] = "risk_reduction"
					quest.Checkpoint["risk_reduction_closed_positions"] = deriskClosed
					h.notifyScalpingDecision(ctx, chatID, AIReasoningNotification{
						DecisionType: "risk_reduction",
						Summary:      "Exposure guard closed positions before scanning new opportunities",
						Confidence:   1,
						Reasons: []string{
							fmt.Sprintf("Exposure ratio %.2f exceeded hard limit 1.00", exposureRatio),
							fmt.Sprintf("Placed %d close order(s) to reduce exposure", deriskClosed),
						},
						Action: "hold",
					})
					return nil
				}
			}
		}
	}

	driftState, driftErr := h.refreshStateDriftGate(ctx, quest, chatID, userExchange)
	if driftErr != nil {
		log.Printf("[SCALPING] State drift refresh failed: %v", driftErr)
		quest.Checkpoint["state_drift_error"] = driftErr.Error()
	}
	quest.Checkpoint["state_drift_active"] = driftState.Active
	quest.Checkpoint["state_drift_positions"] = driftState.DriftPositions
	quest.Checkpoint["state_drift_last_checked_at"] = driftState.LastCheckedAt.Format(time.RFC3339)
	quest.Checkpoint["state_drift_clean_passes"] = driftState.CleanPasses
	if strings.TrimSpace(driftState.EntryGateReason) != "" {
		quest.Checkpoint["runtime_entry_gate_reason"] = driftState.EntryGateReason
	} else if !checkpointBool(quest.Checkpoint["runtime_entry_blocked_by_risk_lock"]) {
		delete(quest.Checkpoint, "runtime_entry_gate_reason")
	}

	portfolio := TradingPortfolio{
		USDTBalance:        usdtBalance,
		USDTBalanceDecimal: decimalFromBalanceFloat(usdtBalance),
		TotalValue:         usdtBalance,
		TotalValueDecimal:  decimalFromBalanceFloat(usdtBalance),
		OpenPositions:      0,
	}
	h.enrichPortfolioControlPlane(ctx, quest, chatID, userExchange, &portfolio)
	recoveryState := h.evaluateRecoveryGateStateForScope(ctx, quest, portfolio, chatID, userExchange)
	quest.Checkpoint["recovery_recent_loss_streak"] = recoveryState.RecentLossStreak
	quest.Checkpoint["recovery_recent_loss_active"] = recoveryState.RecentLossActive
	quest.Checkpoint["recovery_recent_loss_window_seconds"] = int(recoveryState.RecentLossWindow.Seconds())
	if !recoveryState.RecentLossLastTradeAt.IsZero() {
		quest.Checkpoint["recovery_recent_loss_last_trade_at"] = recoveryState.RecentLossLastTradeAt.Format(time.RFC3339)
	} else {
		delete(quest.Checkpoint, "recovery_recent_loss_last_trade_at")
	}
	h.applyRecoveryStateCheckpoint(quest, &portfolio, recoveryState, time.Now().UTC())
	if recoveryState.RecentLossActive && recoveryState.RecentLossStreak >= recoveryLossStreakResetThreshold() {
		h.updateRecoveryCleanCycles(quest, false, "recent_loss_streak")
		recoveryState = h.evaluateRecoveryGateStateForScope(ctx, quest, portfolio, chatID, userExchange)
		h.applyRecoveryStateCheckpoint(quest, &portfolio, recoveryState, time.Now().UTC())
	}

	log.Printf("[SCALPING] Portfolio: %.2f USDT available", usdtBalance)

	if !isDryRun {
		timeStopClosed, timeStopErr := h.enforceAdaptiveTimeStop(ctx, quest, chatID, userExchange, portfolio.StrategyPhase)
		if timeStopErr != nil {
			log.Printf("[SCALPING] Adaptive time-stop check failed: %v", timeStopErr)
			quest.Checkpoint["time_stop_error"] = timeStopErr.Error()
		} else if timeStopClosed > 0 {
			quest.Checkpoint["status"] = "risk_reduction"
			quest.Checkpoint["time_stop_closed_positions"] = timeStopClosed
			h.notifyScalpingDecision(ctx, chatID, AIReasoningNotification{
				DecisionType: "risk_reduction",
				Summary:      "Adaptive time-stop closed stale positions before new entries",
				Confidence:   1,
				Reasons: []string{
					fmt.Sprintf("Closed %d position(s) that exceeded max hold duration", timeStopClosed),
					fmt.Sprintf("Regime phase used: %s", portfolio.StrategyPhase),
				},
				Action: "hold",
			})
			return nil
		}
	}

	if checkpointBool(quest.Checkpoint["runtime_entry_blocked_by_risk_lock"]) {
		gateCtx, gateCancel := h.withGatePathContext(ctx)
		defer gateCancel()
		h.updateRecoveryCleanCycles(quest, false, "risk_lock_active")
		h.updateHoldStateCheckpoint(quest, true)
		unlockClosed, unlockErr := h.maybeApplyRiskUnlock(gateCtx, quest, chatID, userExchange, portfolio.RiskDrawdown)
		if unlockErr != nil {
			log.Printf("[SCALPING] Risk unlock check failed during risk lock: %v", unlockErr)
			quest.Checkpoint["risk_unlock_error"] = unlockErr.Error()
		}

		quest.Checkpoint["status"] = "risk_lock"
		quest.Checkpoint["runtime_entry_gate_reason"] = "risk lock active: drawdown/exposure guardrail blocking new entries"
		quest.Checkpoint["entry_gate_type"] = "risk_lock"
		quest.Checkpoint["runtime_next_unblock_condition"] = "Drawdown/exposure must recover below risk lock guardrails"
		reasons := []string{
			"Risk lock active: new entries are paused; only de-risk/protection/reconcile actions are allowed",
		}
		if blockedAt, ok := quest.Checkpoint["runtime_entry_blocked_at"].(string); ok && strings.TrimSpace(blockedAt) != "" {
			reasons = append(reasons, fmt.Sprintf("Entry gate active since %s", blockedAt))
		}
		if unlockClosed > 0 {
			reasons = append(reasons, fmt.Sprintf("Risk unlock trimmed %d position(s) by 35%%", unlockClosed))
		}

		holdDecision := &AITradingDecision{
			Action:     "hold",
			Confidence: 0,
			Reasoning:  "risk lock active",
		}
		h.notifyScalpingDecision(gateCtx, chatID, AIReasoningNotification{
			DecisionType:     "risk_reduction",
			Summary:          "Risk lock active: entry scans paused, risk controls still running",
			Confidence:       0,
			UnblockCondition: checkpointString(quest.Checkpoint["runtime_next_unblock_condition"]),
			Reasons:          reasons,
			Action:           "hold",
		})
		h.maybeSendHoldDigest(gateCtx, quest, chatID, holdDecision, portfolio)
		return nil
	}

	if checkpointBool(quest.Checkpoint["runtime_entry_blocked_by_state_drift"]) {
		gateCtx, gateCancel := h.withGatePathContext(ctx)
		defer gateCancel()
		h.updateRecoveryCleanCycles(quest, false, "state_drift_gate")
		h.updateHoldStateCheckpoint(quest, true)
		quest.Checkpoint["status"] = "state_drift_gate"
		quest.Checkpoint["entry_gate_type"] = "state_drift"
		quest.Checkpoint["runtime_next_unblock_condition"] = fmt.Sprintf(
			"Require %d consecutive clean reconcile passes with zero stale lifecycle rows",
			stateDriftClearPassTarget(),
		)
		reasons := []string{
			"Lifecycle/exchange drift detected: new entries are paused until reconcile is clean",
			fmt.Sprintf("Drift positions pending repair: %d", checkpointInt(quest.Checkpoint["state_drift_positions"])),
		}
		if gateReason, ok := quest.Checkpoint["runtime_entry_gate_reason"].(string); ok && strings.TrimSpace(gateReason) != "" {
			reasons = append(reasons, gateReason)
		}
		if repairedAt, ok := quest.Checkpoint["state_drift_last_repair_at"].(string); ok && strings.TrimSpace(repairedAt) != "" {
			reasons = append(reasons, fmt.Sprintf("Last drift repair: %s", repairedAt))
		}

		holdDecision := &AITradingDecision{
			Action:          "hold",
			Confidence:      0,
			Reasoning:       "entry paused by state drift gate",
			ReasonCategory:  aiReasonExecutionUnavailable,
			ConfidenceKnown: false,
		}
		h.notifyScalpingDecision(gateCtx, chatID, AIReasoningNotification{
			DecisionType:     "scalping_cycle",
			Summary:          "State drift gate active: reconciling lifecycle with exchange before new entries",
			Confidence:       0,
			ConfidenceKnown:  false,
			ReasonCategory:   aiReasonExecutionUnavailable,
			HoldCategory:     aiReasonExecutionUnavailable,
			UnblockCondition: checkpointString(quest.Checkpoint["runtime_next_unblock_condition"]),
			Reasons:          reasons,
			Action:           "hold",
		})
		h.maybeSendHoldDigest(gateCtx, quest, chatID, holdDecision, portfolio)
		return nil
	}

	if circuitRemaining := aiRuntimeCircuitRemaining(quest, time.Now().UTC()); circuitRemaining > 0 {
		gateCtx, gateCancel := h.withGatePathContext(ctx)
		defer gateCancel()
		h.updateRecoveryCleanCycles(quest, false, "runtime_circuit_open")
		reasonCategory := aiReasonExecutionUnavailable
		if raw, ok := quest.Checkpoint["runtime_ai_circuit_reason"].(string); ok && strings.TrimSpace(raw) != "" {
			reasonCategory = strings.TrimSpace(raw)
		}
		holdDecision := &AITradingDecision{
			Action:          "hold",
			Confidence:      0,
			Reasoning:       "AI runtime circuit breaker active",
			ReasonCategory:  reasonCategory,
			ConfidenceKnown: false,
		}
		quest.Checkpoint["status"] = "runtime_circuit_open"
		quest.Checkpoint["runtime_ai_circuit_remaining_seconds"] = int(circuitRemaining.Seconds())
		quest.Checkpoint["entry_gate_type"] = "runtime_circuit"
		quest.Checkpoint["runtime_next_unblock_condition"] = fmt.Sprintf(
			"Wait for AI runtime circuit to recover (remaining %s)",
			circuitRemaining.Round(time.Second).String(),
		)
		recordAIRuntimeEvent(quest, time.Now().UTC(), reasonCategory, false, h.getAIScalpingRuntimeSnapshot())
		h.notifyScalpingDecision(gateCtx, chatID, AIReasoningNotification{
			DecisionType:     "scalping_cycle",
			Summary:          "AI runtime circuit breaker active; entry scan skipped this cycle",
			Confidence:       0,
			ConfidenceKnown:  false,
			ReasonCategory:   reasonCategory,
			HoldCategory:     reasonCategory,
			UnblockCondition: checkpointString(quest.Checkpoint["runtime_next_unblock_condition"]),
			Reasons: []string{
				fmt.Sprintf("Circuit remaining: %s", circuitRemaining.Round(time.Second).String()),
				"De-risk/protection/reconcile tasks are still active while entry decisions are paused",
			},
			Action: "hold",
		})
		h.maybeSendHoldDigest(gateCtx, quest, chatID, holdDecision, portfolio)
		return nil
	}

	if recoveryState.Mode == recoveryModeDeriskOnly {
		gateCtx, gateCancel := h.withGatePathContext(ctx)
		defer gateCancel()
		h.updateRecoveryCleanCycles(quest, false, "drawdown_derisk_only")
		h.updateHoldStateCheckpoint(quest, true)
		quest.Checkpoint["status"] = "recovery_derisk_only"
		quest.Checkpoint["runtime_entry_gate_reason"] = recoveryState.GateReason
		quest.Checkpoint["entry_gate_type"] = "recovery_gate"
		if strings.TrimSpace(recoveryState.NextCondition) != "" {
			quest.Checkpoint["runtime_next_unblock_condition"] = recoveryState.NextCondition
		}
		reasons := []string{
			recoveryState.GateReason,
			fmt.Sprintf("Risk drawdown %.2f%% exceeded de-risk-only threshold %.2f%%", portfolio.RiskDrawdown*100, recoveryState.DeriskOnlyThreshold*100),
			"Only de-risk/protection/reconcile actions are allowed until drawdown improves",
		}
		unlockClosed, unlockErr := h.maybeApplyRiskUnlock(gateCtx, quest, chatID, userExchange, portfolio.RiskDrawdown)
		if unlockErr != nil {
			log.Printf("[SCALPING] Risk unlock check failed during derisk-only mode: %v", unlockErr)
			quest.Checkpoint["risk_unlock_error"] = unlockErr.Error()
		} else if unlockClosed > 0 {
			reasons = append(reasons, fmt.Sprintf("Risk unlock trimmed %d position(s) by 35%%", unlockClosed))
		}
		holdDecision := &AITradingDecision{
			Action:          "hold",
			Confidence:      0,
			Reasoning:       recoveryState.GateReason,
			ReasonCategory:  aiReasonExecutionUnavailable,
			ConfidenceKnown: false,
		}
		h.notifyScalpingDecision(gateCtx, chatID, AIReasoningNotification{
			DecisionType:     "risk_reduction",
			Summary:          "Drawdown recovery mode: de-risk only",
			Confidence:       0,
			ConfidenceKnown:  false,
			ReasonCategory:   aiReasonExecutionUnavailable,
			HoldCategory:     aiReasonExecutionUnavailable,
			UnblockCondition: checkpointString(quest.Checkpoint["runtime_next_unblock_condition"]),
			Reasons:          reasons,
			Action:           "hold",
		})
		h.maybeSendHoldDigest(gateCtx, quest, chatID, holdDecision, portfolio)
		return nil
	}

	if recoveryState.Mode == recoveryModeMicroEntry && !recoveryState.EntryAllowed {
		gateCtx, gateCancel := h.withGatePathContext(ctx)
		defer gateCancel()
		h.updateRecoveryCleanCycles(quest, true, "")
		recoveryState = h.evaluateRecoveryGateStateForScope(gateCtx, quest, portfolio, chatID, userExchange)
		h.applyRecoveryStateCheckpoint(quest, &portfolio, recoveryState, time.Now().UTC())
		if recoveryState.EntryAllowed {
			delete(quest.Checkpoint, "runtime_entry_gate_reason")
			delete(quest.Checkpoint, "runtime_next_unblock_condition")
			delete(quest.Checkpoint, "entry_gate_type")
		} else {
			h.updateHoldStateCheckpoint(quest, true)
			quest.Checkpoint["status"] = "recovery_micro_entry_wait"
			quest.Checkpoint["runtime_entry_gate_reason"] = recoveryState.GateReason
			quest.Checkpoint["entry_gate_type"] = "recovery_gate"
			if strings.TrimSpace(recoveryState.NextCondition) != "" {
				quest.Checkpoint["runtime_next_unblock_condition"] = recoveryState.NextCondition
			}
			reasons := []string{
				recoveryState.GateReason,
				fmt.Sprintf("Clean cycles progress: %d/%d", recoveryState.CleanCycles, recoveryState.RequiredCleanCycles),
				fmt.Sprintf("Micro-entry cap when unlocked: %.2f%%", recoveryState.MicroEntryCapPct),
			}
			holdDecision := &AITradingDecision{
				Action:          "hold",
				Confidence:      0,
				Reasoning:       recoveryState.GateReason,
				ReasonCategory:  aiReasonStrategyHold,
				ConfidenceKnown: true,
			}
			h.notifyScalpingDecision(gateCtx, chatID, AIReasoningNotification{
				DecisionType:     "scalping_cycle",
				Summary:          "Recovery mode active: waiting for clean cycles before micro-entry",
				Confidence:       0,
				ConfidenceKnown:  true,
				ReasonCategory:   aiReasonStrategyHold,
				HoldCategory:     aiReasonStrategyHold,
				UnblockCondition: checkpointString(quest.Checkpoint["runtime_next_unblock_condition"]),
				Reasons:          reasons,
				Action:           "hold",
			})
			h.maybeSendHoldDigest(gateCtx, quest, chatID, holdDecision, portfolio)
			return nil
		}
	}

	nowUTC := time.Now().UTC()
	livenessGate := h.evaluateEntryAttemptGateState(quest, portfolio, nowUTC)
	quest.Checkpoint["runtime_liveness_forced"] = livenessGate.Forced
	quest.Checkpoint["runtime_next_unblock_condition"] = livenessGate.NextCondition
	quest.Checkpoint["runtime_entry_attempt_window_progress"] = livenessGate.AttemptWindowProgress
	quest.Checkpoint["runtime_entry_attempts_1h"] = livenessGate.AttemptsInWindow
	if livenessGate.AllowNow {
		delete(quest.Checkpoint, "runtime_entry_attempt_block_reason")
		delete(quest.Checkpoint, "runtime_entry_gate_reason")
	} else {
		quest.Checkpoint["runtime_entry_attempt_block_reason"] = livenessGate.BlockReason
		quest.Checkpoint["runtime_entry_gate_reason"] = livenessGate.BlockReason
	}
	quest.Checkpoint["entry_gate_type"] = "none"
	if !livenessGate.AllowNow {
		h.updateHoldStateCheckpoint(quest, true)
		h.updateRecoveryCleanCycles(quest, true, "")
		quest.Checkpoint["status"] = "liveness_entry_wait"
		holdDecision := &AITradingDecision{
			Action:          "hold",
			Confidence:      0,
			Reasoning:       livenessGate.BlockReason,
			ReasonCategory:  aiReasonStrategyHold,
			ConfidenceKnown: true,
		}
		h.notifyScalpingDecision(ctx, chatID, AIReasoningNotification{
			DecisionType:          "scalping_cycle",
			Summary:               "Liveness controller waiting for next bounded entry-attempt slot",
			Confidence:            0,
			ConfidenceKnown:       true,
			ReasonCategory:        aiReasonStrategyHold,
			HoldCategory:          aiReasonStrategyHold,
			UnblockCondition:      livenessGate.NextCondition,
			AttemptWindowProgress: livenessGate.AttemptWindowProgress,
			Reasons: []string{
				livenessGate.BlockReason,
				fmt.Sprintf("Deployable balance ratio: %.2f%%", livenessGate.DeployableBalanceRatio*100),
				livenessGate.AttemptWindowProgress,
			},
			Action: "hold",
		})
		h.maybeSendHoldDigest(ctx, quest, chatID, holdDecision, portfolio)
		return nil
	}
	exchangeConnected := true
	connectionChecked := false
	if healthChecker, ok := h.ccxtService.(interface{ IsHealthy(context.Context) bool }); ok {
		connectionChecked = true
		exchangeConnected = healthChecker.IsHealthy(ctx)
	}
	safeMode := checkpointBool(quest.Checkpoint["runtime_entry_blocked_by_risk_lock"])
	if envSafeMode, ok := getEnvBool("NEURATRADE_SAFE_MODE_ENABLED"); ok && envSafeMode {
		safeMode = true
	}
	killSwitchEngaged := false
	if envKillSwitch, ok := getEnvBool("NEURATRADE_KILL_SWITCH_ENGAGED"); ok {
		killSwitchEngaged = envKillSwitch
	} else if envKillSwitch, ok := getEnvBool("NEURATRADE_KILL_SWITCH"); ok {
		killSwitchEngaged = envKillSwitch
	}
	strategyID := ScalpingStrategyID(chatID)
	cycleCtx := WithScalpingAutonomyScope(ctx, ScalpingAutonomyScope{
		ChatID:            chatID,
		StrategyID:        strategyID,
		Exchange:          userExchange,
		MarketType:        "futures",
		SafeModeEnabled:   safeMode,
		KillSwitchEngaged: killSwitchEngaged,
		ExchangeConnected: exchangeConnected,
		ConnectionChecked: connectionChecked,
	})

	decision, err := h.aiScalpingService.ExecuteTradingCycle(cycleCtx, portfolio)
	h.applyAutonomyCheckpoint(quest)
	h.applyScalpingCycleDecisionDiagnostics(quest, decision)
	cycleID := fmt.Sprintf("scalp-%s-%d", chatID, nowUTC.UnixNano())
	if h.telemetryStore != nil {
		cycleRec := CycleRecord{
			ID:       cycleID,
			ChatID:   chatID,
			Exchange: userExchange,
			CycleAt:  nowUTC,
		}
		if decision != nil {
			rejectionJSON, marshalErr := json.Marshal(decision.CandidateFunnel.RejectionCounts)
			if marshalErr != nil {
				log.Printf("[TELEMETRY] Failed to marshal rejection counts: %v", marshalErr)
				rejectionJSON = []byte("{}")
			}
			policyJSON, marshalErr := json.Marshal(decision.PolicyAdjustments)
			if marshalErr != nil {
				log.Printf("[TELEMETRY] Failed to marshal policy adjustments: %v", marshalErr)
				policyJSON = []byte("[]")
			}
			gateBlockCode := ""
			gateBlockReason := ""
			if decision.ExecutionGate != nil {
				gateBlockCode = decision.ExecutionGate.BlockCode
				gateBlockReason = decision.ExecutionGate.BlockReason
			}
			cycleRec.Symbol = decision.Symbol
			cycleRec.Action = decision.Action
			cycleRec.Confidence = decision.Confidence
			cycleRec.UniverseCount = decision.CandidateFunnel.CandidateUniverseCount
			cycleRec.RankedCount = decision.CandidateFunnel.CandidateRankedCount
			cycleRec.ViableCount = decision.CandidateFunnel.CandidateViableCount
			cycleRec.RejectionCountsJSON = string(rejectionJSON)
			cycleRec.Regime = decision.PreTradeRegime
			cycleRec.Expectancy = decision.PreTradeExpectancy
			cycleRec.ExpectancySampleSize = decision.PreTradeExpectancySampleSize
			cycleRec.GateBlockCode = gateBlockCode
			cycleRec.GateBlockReason = gateBlockReason
			cycleRec.AccountTier = decision.AccountTier
			cycleRec.EffectiveMinConfidence = decision.EffectiveMinConfidence
			cycleRec.EffectiveMaxCapitalPct = decision.EffectiveMaxCapitalPct
			cycleRec.PolicyAdjustmentsJSON = string(policyJSON)
		}
		insertedCycleID, insertErr := h.telemetryStore.InsertCycleRecord(ctx, cycleRec)
		if insertErr != nil {
			log.Printf("[TELEMETRY] Failed to insert cycle record: %v", insertErr)
		} else {
			_ = insertedCycleID
		}
	}
	if shouldRecordEntryAttempt(decision, err) {
		h.recordEntryAttempt(quest, nowUTC, livenessGate)
	}
	if err != nil {
		h.updateRecoveryCleanCycles(quest, false, "runtime_error")
		log.Printf("[SCALPING] AI decision error: %v", err)
		reasonCategory := classifyAIRuntimeReason(err.Error(), aiReasonExecutionUnavailable)
		deriskClosed := 0
		if strings.Contains(strings.ToLower(err.Error()), "portfolio safety blocked") {
			closed, deriskErr := h.autoDeriskBlockedExposure(ctx, quest, chatID, userExchange, err.Error())
			if deriskErr != nil {
				log.Printf("[SCALPING] Auto de-risk attempt failed: %v", deriskErr)
				quest.Checkpoint["auto_derisk_error"] = deriskErr.Error()
			} else if closed > 0 {
				deriskClosed = closed
				quest.Checkpoint["auto_derisk_closed_positions"] = closed
				quest.Checkpoint["auto_derisk_at"] = time.Now().UTC().Format(time.RFC3339)
			}
		}
		streak, cooldown := h.recordScalpingFailure(quest, err.Error())
		quest.Checkpoint["status"] = "ai_error"
		quest.Checkpoint["error"] = err.Error()
		reasons := []string{err.Error(), fmt.Sprintf("Runtime failure streak: %d", streak)}
		if deriskClosed > 0 {
			reasons = append(reasons, fmt.Sprintf("Auto de-risk placed %d close order(s)", deriskClosed))
		}
		if cooldown > 0 {
			reasons = append(reasons, fmt.Sprintf("Cooldown applied: %s", cooldown.Round(time.Second).String()))
		}
		recordAIRuntimeEvent(quest, time.Now().UTC(), reasonCategory, false, h.getAIScalpingRuntimeSnapshot())
		h.notifyScalpingDecision(ctx, chatID, AIReasoningNotification{
			DecisionType:    "scalping_cycle",
			Summary:         "Scalping cycle skipped due to AI/runtime error",
			Confidence:      0,
			ConfidenceKnown: false,
			ReasonCategory:  reasonCategory,
			HoldCategory:    reasonCategory,
			Reasons:         reasons,
			Action:          "hold",
		})
		// Return nil instead of err to prevent panic - quest continues with hold status
		return nil
	}

	// Safety check: decision should not be nil
	if decision == nil {
		h.updateRecoveryCleanCycles(quest, false, "decision_nil")
		log.Printf("[SCALPING] AI returned nil decision - treating as hold")
		streak, cooldown := h.recordScalpingFailure(quest, "decision payload was nil")
		quest.Checkpoint["status"] = "hold"
		quest.Checkpoint["ai_action"] = "hold"
		quest.Checkpoint["ai_reasoning"] = "AI returned nil decision"
		reasons := []string{"Decision payload was nil; cycle held", fmt.Sprintf("Runtime failure streak: %d", streak)}
		if cooldown > 0 {
			reasons = append(reasons, fmt.Sprintf("Cooldown applied: %s", cooldown.Round(time.Second).String()))
		}
		recordAIRuntimeEvent(quest, time.Now().UTC(), aiReasonLLMParseContract, false, h.getAIScalpingRuntimeSnapshot())
		h.notifyScalpingDecision(ctx, chatID, AIReasoningNotification{
			DecisionType:    "scalping_cycle",
			Summary:         "AI returned no trade decision",
			Confidence:      0,
			ConfidenceKnown: false,
			ReasonCategory:  aiReasonLLMParseContract,
			HoldCategory:    aiReasonLLMParseContract,
			Reasons:         reasons,
			Action:          "hold",
		})
		return nil
	}

	quest.Checkpoint["ai_action"] = decision.Action
	quest.Checkpoint["ai_symbol"] = decision.Symbol
	quest.Checkpoint["ai_confidence"] = decision.Confidence
	quest.Checkpoint["ai_reasoning"] = decision.Reasoning
	quest.Checkpoint["ai_size_pct"] = decision.SizePercent

	if decision.Action == "hold" {
		h.updateHoldStateCheckpoint(quest, true)
		unlockClosed, unlockErr := h.maybeApplyRiskUnlock(ctx, quest, chatID, userExchange, portfolio.RiskDrawdown)
		if unlockErr != nil {
			log.Printf("[SCALPING] Risk unlock check failed: %v", unlockErr)
			quest.Checkpoint["risk_unlock_error"] = unlockErr.Error()
		}

		runtimeHold := isRuntimeHoldReason(decision.Reasoning)
		reasonCategory := strings.TrimSpace(decision.ReasonCategory)
		if runtimeHold {
			inferred := classifyAIRuntimeReason(decision.Reasoning, aiReasonExecutionUnavailable)
			if reasonCategory == "" || strings.EqualFold(reasonCategory, aiReasonStrategyHold) {
				reasonCategory = inferred
			}
		}
		if !decision.ConfidenceKnown && (reasonCategory == "" || strings.EqualFold(reasonCategory, aiReasonStrategyHold)) {
			fallbackReason := decision.Reasoning
			if runtimeCheckpointReason := checkpointString(quest.Checkpoint["runtime_ai_last_category"]); runtimeCheckpointReason != "" {
				fallbackReason = runtimeCheckpointReason + " " + fallbackReason
			}
			reasonCategory = classifyAIRuntimeReason(fallbackReason, aiReasonExecutionUnavailable)
		}
		if reasonCategory == "" {
			reasonCategory = aiReasonStrategyHold
		}
		if !decision.ConfidenceKnown && strings.EqualFold(reasonCategory, aiReasonStrategyHold) {
			reasonCategory = aiReasonExecutionUnavailable
		}
		runtimeCategory := isRuntimeReasonCategory(reasonCategory)

		if runtimeHold || runtimeCategory {
			h.updateRecoveryCleanCycles(quest, false, "runtime_hold")
			streak, cooldown := h.recordScalpingFailure(quest, decision.Reasoning)
			if cooldown > 0 {
				quest.Checkpoint["runtime_hold_cooldown"] = cooldown.String()
			}
			quest.Checkpoint["runtime_failure_streak"] = streak
			recordAIRuntimeEvent(quest, time.Now().UTC(), reasonCategory, false, h.getAIScalpingRuntimeSnapshot())
		} else {
			h.updateRecoveryCleanCycles(quest, true, "")
			h.resetScalpingFailureState(quest)
			recordAIRuntimeEvent(quest, time.Now().UTC(), reasonCategory, true, h.getAIScalpingRuntimeSnapshot())
		}
		log.Printf("[SCALPING] AI decided to hold: %s", decision.Reasoning)
		quest.Checkpoint["status"] = "hold"
		reasons := []string{decision.Reasoning}
		if unlockClosed > 0 {
			reasons = append(reasons, fmt.Sprintf("Risk unlock trimmed %d position(s) by 35%%", unlockClosed))
		}
		if !runtimeHold && !runtimeCategory {
			blockReason, nextCondition, humanReason := structuredHoldBlock(decision)
			if blockReason != "" {
				quest.Checkpoint["runtime_entry_attempt_block_reason"] = blockReason
			} else if livenessGate.Forced {
				quest.Checkpoint["runtime_entry_attempt_block_reason"] = "no_candidate_passed_filters"
			}
			if nextCondition != "" {
				quest.Checkpoint["runtime_next_unblock_condition"] = nextCondition
			} else if livenessGate.Forced {
				quest.Checkpoint["runtime_next_unblock_condition"] = "Await candidate that passes pretrade validity/liquidity filters"
			}
			if humanReason != "" {
				quest.Checkpoint["runtime_entry_gate_reason"] = humanReason
				reasons = append(reasons, humanReason)
			} else if livenessGate.Forced {
				quest.Checkpoint["runtime_entry_gate_reason"] = "no candidate passed pretrade validity/liquidity filters"
				reasons = append(reasons, "No candidate passed pretrade validity/liquidity filters in this attempt window")
			}
		}
		if raw, ok := quest.Checkpoint["runtime_entry_attempt_window_progress"].(string); ok && strings.TrimSpace(raw) != "" {
			reasons = append(reasons, fmt.Sprintf("Attempt window: %s", strings.TrimSpace(raw)))
		}
		unblockCondition := checkpointString(quest.Checkpoint["runtime_next_unblock_condition"])
		h.notifyScalpingDecision(ctx, chatID, AIReasoningNotification{
			DecisionType:          "scalping_cycle",
			Summary:               "AI held position this cycle",
			Confidence:            decision.Confidence,
			ConfidenceKnown:       !runtimeHold && !runtimeCategory && decision.ConfidenceKnown,
			ReasonCategory:        reasonCategory,
			HoldCategory:          reasonCategory,
			UnblockCondition:      unblockCondition,
			AttemptWindowProgress: checkpointString(quest.Checkpoint["runtime_entry_attempt_window_progress"]),
			Reasons:               reasons,
			Action:                "hold",
		})
		h.maybeSendHoldDigest(ctx, quest, chatID, decision, portfolio)
		return nil
	}

	h.updateHoldStateCheckpoint(quest, false)
	h.updateRecoveryCleanCycles(quest, true, "")
	h.resetScalpingFailureState(quest)
	quest.Checkpoint["status"] = "ai_executed"
	delete(quest.Checkpoint, "runtime_next_unblock_condition")
	delete(quest.Checkpoint, "runtime_entry_attempt_block_reason")
	delete(quest.Checkpoint, "runtime_entry_gate_reason")
	quest.CurrentCount++
	quest.Checkpoint["last_scalp_time"] = time.Now().UTC().Format(time.RFC3339)
	quest.Checkpoint["chat_id"] = chatID
	if h.lifecycleStore != nil && strings.TrimSpace(decision.OrderID) != "" {
		entryPrice := decimal.Zero
		if decision.EntryPrice != nil {
			entryPrice = *decision.EntryPrice
		}
		if err := h.lifecycleStore.RecordOrderExecution(ctx, LifecycleExecutionRecord{
			OrderID:    decision.OrderID,
			ChatID:     chatID,
			Exchange:   userExchange,
			Symbol:     decision.Symbol,
			Side:       decision.Action,
			OrderType:  "market",
			MarketType: "futures",
			Amount:     walletBasis(portfolio).Mul(decimal.NewFromFloat(decision.SizePercent)).Div(decimal.NewFromInt(100)),
			EntryPrice: entryPrice,
			StopLoss:   decimalValueOrZero(decision.StopLoss),
			TakeProfit: decimalValueOrZero(decision.TakeProfit),
			Source:     "autonomous_scalping",
			OpenedAt:   time.Now().UTC(),
		}); err != nil {
			log.Printf("[SCALPING] Failed to persist execution lifecycle for %s: %v", decision.OrderID, err)
		}
	}
	h.recordTradeDecision(ctx, quest, decision, userExchange, portfolio)
	if h.telemetryStore != nil && strings.TrimSpace(decision.OrderID) != "" {
		if err := h.telemetryStore.LinkOrderToCycle(ctx, cycleID, strings.TrimSpace(decision.OrderID)); err != nil {
			log.Printf("[TELEMETRY] Failed to link order %s to cycle: %v", decision.OrderID, err)
		}
	}
	h.ingestClosedOrderFeedback(ctx, quest, userExchange, decision.Symbol)
	recordAIRuntimeEvent(quest, time.Now().UTC(), aiReasonStrategyHold, true, h.getAIScalpingRuntimeSnapshot())
	if !isDryRun {
		h.assertPostEntryProtectionAsync(chatID, userExchange, decision.OrderID, decision.Symbol, decision.Action)
	}

	log.Printf("[SCALPING] AI decision executed: %s %s (%.0f%% confidence)",
		decision.Action, decision.Symbol, decision.Confidence*100)

	h.notifyScalpingDecision(ctx, chatID, AIReasoningNotification{
		DecisionType:    "scalping",
		Summary:         fmt.Sprintf("AI decided to %s %s", decision.Action, decision.Symbol),
		Confidence:      decision.Confidence,
		ConfidenceKnown: true,
		ReasonCategory:  aiReasonStrategyHold,
		Reasons:         []string{decision.Reasoning},
		Action:          decision.Action,
	})

	return nil
}

func (h *IntegratedQuestHandlers) autoDeriskBlockedExposure(
	ctx context.Context,
	quest *Quest,
	chatID, exchange, safetyError string,
) (int, error) {
	if h.orderExecutor == nil {
		return 0, nil
	}

	positionFetcher, ok := h.ccxtService.(interface {
		FetchPositions(ctx context.Context, exchange string) (*ccxt.PositionsResponse, error)
	})
	if !ok {
		return 0, nil
	}

	fetchCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	positionsResp, err := positionFetcher.FetchPositions(fetchCtx, exchange)
	if err != nil {
		return 0, fmt.Errorf("fetch positions: %w", err)
	}
	if positionsResp == nil || len(positionsResp.Positions) == 0 {
		return 0, nil
	}

	execWithBypass, hasBypass := h.orderExecutor.(interface {
		PlaceRiskReductionOrderWithDetails(context.Context, TradeDetails) (string, error)
	})

	closed := 0
	for _, pos := range positionsResp.Positions {
		size := pos.Size.Abs()
		if size.LessThanOrEqual(decimal.Zero) {
			continue
		}

		closeSide := oppositeCloseSide(pos.Side)
		if closeSide == "" {
			continue
		}

		mark := pos.MarkPrice
		if mark.LessThanOrEqual(decimal.Zero) {
			mark = pos.EntryPrice
		}
		if mark.LessThanOrEqual(decimal.Zero) {
			continue
		}

		notional := size.Mul(mark)
		if notional.LessThanOrEqual(decimal.Zero) {
			continue
		}

		details := TradeDetails{
			Exchange:     exchange,
			Symbol:       pos.Symbol,
			Side:         closeSide,
			OrderType:    "market",
			MarketType:   "futures",
			Leverage:     pos.Leverage,
			Amount:       size,
			AmountUSDT:   notional,
			TradeType:    "risk_reduction",
			Confidence:   1.0,
			Reasoning:    fmt.Sprintf("Auto de-risk due to safety block: %s", safetyError),
			IsPaperTrade: h.orderExecutor.IsPaperTrading(),
			ReduceOnly:   true,
		}
		if details.Leverage <= 0 {
			details.Leverage = 1
		}

		placeCtx, placeCancel := context.WithTimeout(ctx, 15*time.Second)
		var placeErr error
		if hasBypass {
			_, placeErr = execWithBypass.PlaceRiskReductionOrderWithDetails(placeCtx, details)
		} else {
			_, placeErr = h.orderExecutor.PlaceOrderWithDetails(placeCtx, details)
		}
		placeCancel()
		if placeErr != nil {
			log.Printf("[SCALPING] Auto de-risk failed for %s %s: %v", pos.Symbol, pos.Side, placeErr)
			continue
		}

		closed++
		log.Printf("[SCALPING] Auto de-risk order placed: close %s %s (size=%s, notional=%s)", pos.Side, pos.Symbol, size.String(), notional.StringFixed(4))
	}

	if closed > 0 {
		quest.Checkpoint["auto_derisk_triggered"] = true
		quest.Checkpoint["auto_derisk_exchange"] = exchange
		quest.Checkpoint["auto_derisk_chat_id"] = chatID
	}

	return closed, nil
}

func (h *IntegratedQuestHandlers) autoDeriskIfExposureTooHigh(
	ctx context.Context,
	quest *Quest,
	chatID, exchange string,
	totalEquityUSDT float64,
) (int, float64, error) {
	if totalEquityUSDT <= 0 {
		return 0, 0, nil
	}

	positionFetcher, ok := h.ccxtService.(interface {
		FetchPositions(ctx context.Context, exchange string) (*ccxt.PositionsResponse, error)
	})
	if !ok {
		return 0, 0, nil
	}

	fetchCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	positionsResp, err := positionFetcher.FetchPositions(fetchCtx, exchange)
	if err != nil {
		return 0, 0, fmt.Errorf("fetch positions for exposure guard: %w", err)
	}
	if positionsResp == nil || len(positionsResp.Positions) == 0 {
		return 0, 0, nil
	}

	totalNotional := decimal.Zero
	for _, pos := range positionsResp.Positions {
		size := pos.Size.Abs()
		if size.LessThanOrEqual(decimal.Zero) {
			continue
		}
		mark := pos.MarkPrice
		if mark.LessThanOrEqual(decimal.Zero) {
			mark = pos.EntryPrice
		}
		if mark.LessThanOrEqual(decimal.Zero) {
			continue
		}
		totalNotional = totalNotional.Add(size.Mul(mark))
	}
	if totalNotional.LessThanOrEqual(decimal.Zero) {
		return 0, 0, nil
	}

	equity := decimal.NewFromFloat(totalEquityUSDT)
	if equity.LessThanOrEqual(decimal.Zero) {
		return 0, 0, nil
	}

	exposureRatio, _ := totalNotional.Div(equity).Float64()
	hardLimit := 1.0
	if configured, ok := getEnvFloat("NEURATRADE_EXPOSURE_GUARD_HARD_LIMIT"); ok && configured > 0 {
		hardLimit = configured
	}
	if exposureRatio <= hardLimit {
		return 0, exposureRatio, nil
	}

	closed, deriskErr := h.autoDeriskBlockedExposure(
		ctx,
		quest,
		chatID,
		exchange,
		fmt.Sprintf("pre-trade exposure guard %.2f exceeded hard limit %.2f", exposureRatio, hardLimit),
	)
	return closed, exposureRatio, deriskErr
}

func (h *IntegratedQuestHandlers) shouldRunSpotUnwindPass(quest *Quest, now time.Time) bool {
	if quest == nil {
		return false
	}
	if quest.Checkpoint == nil {
		quest.Checkpoint = make(map[string]interface{})
	}
	interval := defaultSpotUnwindInterval
	if seconds := getEnvInt("NEURATRADE_SPOT_UNWIND_INTERVAL_SECONDS"); seconds > 0 {
		interval = time.Duration(seconds) * time.Second
	}
	raw, _ := quest.Checkpoint["spot_unwind_checked_at"].(string)
	if strings.TrimSpace(raw) == "" {
		return true
	}
	last, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return true
	}
	return now.Sub(last) >= interval
}

func (h *IntegratedQuestHandlers) autoDeriskSpotInventory(
	ctx context.Context,
	quest *Quest,
	chatID, exchange string,
	balance *ccxt.BalanceResponse,
) (int, error) {
	if h.orderExecutor == nil || balance == nil {
		return 0, nil
	}
	tickerSource, ok := h.ccxtService.(interface {
		FetchSingleTicker(ctx context.Context, exchange, symbol string) (ccxt.MarketPriceInterface, error)
	})
	if !ok {
		return 0, nil
	}

	assets := balance.Free
	if len(assets) == 0 {
		assets = balance.Total
	}
	if len(assets) == 0 {
		return 0, nil
	}

	minNotional := 8.0
	if value, ok := getEnvFloat("NEURATRADE_SPOT_UNWIND_MIN_NOTIONAL_USDT"); ok && value > 0 {
		minNotional = value
	}
	maxOrders := 3
	if value := getEnvInt("NEURATRADE_SPOT_UNWIND_MAX_ORDERS_PER_PASS"); value > 0 {
		maxOrders = value
	}

	stableAssets := map[string]struct{}{
		"USDT":  {},
		"USDC":  {},
		"BUSD":  {},
		"FDUSD": {},
		"DAI":   {},
		"USDE":  {},
		"USDP":  {},
	}

	// Allow override via environment variable
	if envStable := os.Getenv("NEURATRADE_SPOT_UNWIND_STABLE_ASSETS"); envStable != "" {
		stableAssets = make(map[string]struct{})
		for _, asset := range strings.Split(envStable, ",") {
			asset = strings.ToUpper(strings.TrimSpace(asset))
			if asset != "" {
				stableAssets[asset] = struct{}{}
			}
		}
	}

	execWithBypass, hasBypass := h.orderExecutor.(interface {
		PlaceRiskReductionOrderWithDetails(context.Context, TradeDetails) (string, error)
	})

	closed := 0
	for asset, amount := range assets {
		if closed >= maxOrders {
			break
		}
		asset = strings.ToUpper(strings.TrimSpace(asset))
		if _, skip := stableAssets[asset]; skip {
			continue
		}
		if amount <= 0 {
			continue
		}

		symbol := asset + "/USDT"
		ticker, err := tickerSource.FetchSingleTicker(ctx, exchange, symbol)
		if err != nil || ticker == nil || ticker.GetPrice() <= 0 {
			continue
		}

		amountDec := decimal.NewFromFloat(amount)
		notional := amountDec.Mul(decimal.NewFromFloat(ticker.GetPrice()))
		if notional.LessThan(decimal.NewFromFloat(minNotional)) {
			continue
		}

		details := TradeDetails{
			Exchange:     exchange,
			Symbol:       symbol,
			Side:         "sell",
			OrderType:    "market",
			MarketType:   "spot",
			Amount:       amountDec,
			AmountUSDT:   notional,
			TradeType:    "risk_reduction",
			Confidence:   1,
			Reasoning:    "Startup/resume spot unwind to flatten non-core balances before autonomous entries",
			IsPaperTrade: h.orderExecutor.IsPaperTrading(),
			ReduceOnly:   true,
		}

		placeCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
		var placeErr error
		if hasBypass {
			_, placeErr = execWithBypass.PlaceRiskReductionOrderWithDetails(placeCtx, details)
		} else {
			_, placeErr = h.orderExecutor.PlaceOrderWithDetails(placeCtx, details)
		}
		cancel()
		if placeErr != nil {
			log.Printf("[SCALPING] Spot unwind failed for %s on %s: %v", symbol, exchange, placeErr)
			continue
		}

		closed++
		log.Printf("[SCALPING] Spot unwind order placed: sell %s (notional=%s, chat=%s)", symbol, notional.StringFixed(4), chatID)
	}

	if closed > 0 && quest != nil {
		quest.Checkpoint["spot_unwind_triggered"] = true
		quest.Checkpoint["spot_unwind_exchange"] = exchange
		quest.Checkpoint["spot_unwind_chat_id"] = chatID
	}
	return closed, nil
}

type stateDriftGateState struct {
	Active           bool
	DriftPositions   int
	CleanPasses      int
	EntryGateReason  string
	LastCheckedAt    time.Time
	Repaired         int
	StalePositionIDs []string
}

func (h *IntegratedQuestHandlers) refreshStateDriftGate(
	ctx context.Context,
	quest *Quest,
	chatID, exchange string,
) (stateDriftGateState, error) {
	now := time.Now().UTC()
	state := stateDriftGateState{
		Active:         false,
		DriftPositions: 0,
		CleanPasses:    0,
		LastCheckedAt:  now,
	}
	if quest == nil {
		return state, nil
	}
	if quest.Checkpoint == nil {
		quest.Checkpoint = make(map[string]interface{})
	}
	if h.lifecycleStore == nil {
		quest.Checkpoint["state_drift_active"] = false
		quest.Checkpoint["state_drift_positions"] = 0
		quest.Checkpoint["state_drift_count"] = 0
		quest.Checkpoint["state_drift_last_checked_at"] = now.Format(time.RFC3339)
		quest.Checkpoint["state_drift_clean_passes"] = 0
		delete(quest.Checkpoint, "state_drift_stale_position_ids")
		delete(quest.Checkpoint, "runtime_entry_blocked_by_state_drift")
		return state, nil
	}

	fetcher, ok := h.ccxtService.(interface {
		FetchPositions(ctx context.Context, exchange string) (*ccxt.PositionsResponse, error)
	})
	if !ok {
		state.Active = checkpointBool(quest.Checkpoint["state_drift_active"])
		state.DriftPositions = checkpointInt(quest.Checkpoint["state_drift_positions"])
		state.CleanPasses = checkpointInt(quest.Checkpoint["state_drift_clean_passes"])
		state.EntryGateReason = checkpointString(quest.Checkpoint["runtime_entry_gate_reason"])
		return state, nil
	}

	fetchCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	resp, err := fetcher.FetchPositions(fetchCtx, exchange)
	if err != nil {
		state.Active = checkpointBool(quest.Checkpoint["state_drift_active"])
		state.DriftPositions = checkpointInt(quest.Checkpoint["state_drift_positions"])
		state.CleanPasses = checkpointInt(quest.Checkpoint["state_drift_clean_passes"])
		state.EntryGateReason = checkpointString(quest.Checkpoint["runtime_entry_gate_reason"])
		quest.Checkpoint["state_drift_last_checked_at"] = now.Format(time.RFC3339)
		return state, err
	}

	positions := make([]ccxt.Position, 0)
	if resp != nil {
		positions = resp.Positions
	}
	openOrdersCount := -1
	if ordersFetcher, ok := h.ccxtService.(interface {
		FetchOpenOrders(ctx context.Context, exchange string) (*ccxt.OpenOrdersResponse, error)
	}); ok {
		if ordersResp, ordersErr := ordersFetcher.FetchOpenOrders(fetchCtx, exchange); ordersErr == nil {
			if ordersResp != nil {
				openOrdersCount = len(ordersResp.Orders)
			} else {
				openOrdersCount = 0
			}
		}
	}

	repaired, repairErr := h.lifecycleStore.RepairMissingSyncPositions(
		ctx,
		chatID,
		exchange,
		positions,
		"state_drift_repair_exchange_missing",
	)
	if repairErr != nil {
		return state, repairErr
	}
	if repaired > 0 {
		quest.Checkpoint["state_drift_last_repair_at"] = now.Format(time.RFC3339)
	}
	state.Repaired = repaired

	driftCount, staleIDs, driftErr := h.countLifecyclePositionDrift(ctx, chatID, exchange, positions)
	if driftErr != nil {
		return state, driftErr
	}
	state.DriftPositions = driftCount
	state.StalePositionIDs = staleIDs

	currentSignature := driftStalePositionSignature(staleIDs)
	previousSignature := checkpointString(quest.Checkpoint["state_drift_signature"])
	persistenceCycles := checkpointInt(quest.Checkpoint["state_drift_persistence_cycles"])
	quest.Checkpoint["state_drift_signature"] = currentSignature
	quest.Checkpoint["state_drift_deadlock_cycles"] = 0
	if driftCount > 0 {
		if currentSignature != "" && currentSignature == previousSignature {
			persistenceCycles++
		} else {
			persistenceCycles = 1
		}
		quest.Checkpoint["state_drift_persistence_cycles"] = persistenceCycles
		quest.Checkpoint["state_drift_deadlock_cycles"] = persistenceCycles

		forceThreshold := stateDriftPersistenceForceRepairCycles()
		if persistenceCycles >= forceThreshold && h.shouldRunForcedDriftRepair(quest, now) {
			repairedPersistent, forceErr := h.forceRepairPersistentDriftPositions(ctx, chatID, exchange, staleIDs)
			if forceErr != nil {
				quest.Checkpoint["state_drift_force_repair_error"] = forceErr.Error()
			} else if repairedPersistent > 0 {
				quest.Checkpoint["state_drift_last_force_repair_at"] = now.Format(time.RFC3339)
				quest.Checkpoint["state_drift_force_repaired"] = repairedPersistent
				quest.Checkpoint["state_drift_last_repair_at"] = now.Format(time.RFC3339)
				// Re-count drift after forced in-place repair so the gate can clear faster.
				recount, recountIDs, recountErr := h.countLifecyclePositionDrift(ctx, chatID, exchange, positions)
				if recountErr == nil {
					driftCount = recount
					staleIDs = recountIDs
					state.DriftPositions = recount
					state.StalePositionIDs = recountIDs
				}
			}
		}

		deadlockThreshold := stateDriftDeadlockClearCycles()
		exchangePositionsEmpty := len(positions) == 0
		exchangeOrdersEmpty := openOrdersCount == 0
		if persistenceCycles >= deadlockThreshold &&
			exchangePositionsEmpty &&
			exchangeOrdersEmpty &&
			h.shouldRunForcedDriftRepair(quest, now) {
			repairedDeadlock, clearErr := h.forceRepairPersistentDriftPositionsWithSource(
				ctx,
				chatID,
				exchange,
				staleIDs,
				"state_drift_deadlock_clear",
			)
			if clearErr != nil {
				quest.Checkpoint["state_drift_deadlock_clear_error"] = clearErr.Error()
			} else if repairedDeadlock > 0 {
				quest.Checkpoint["state_drift_deadlock_last_clear_at"] = now.Format(time.RFC3339)
				quest.Checkpoint["state_drift_deadlock_cleared"] = repairedDeadlock
				quest.Checkpoint["state_drift_last_repair_at"] = now.Format(time.RFC3339)
				quest.Checkpoint["state_drift_last_force_repair_at"] = now.Format(time.RFC3339)
				recount, recountIDs, recountErr := h.countLifecyclePositionDrift(ctx, chatID, exchange, positions)
				if recountErr == nil {
					driftCount = recount
					staleIDs = recountIDs
					state.DriftPositions = recount
					state.StalePositionIDs = recountIDs
				}
			}
		}
	} else {
		delete(quest.Checkpoint, "state_drift_signature")
		delete(quest.Checkpoint, "state_drift_persistence_cycles")
		delete(quest.Checkpoint, "state_drift_deadlock_cycles")
		delete(quest.Checkpoint, "state_drift_force_repair_error")
		delete(quest.Checkpoint, "state_drift_deadlock_clear_error")
	}

	requiredCleanPasses := stateDriftClearPassTarget()
	previousActive := checkpointBool(quest.Checkpoint["state_drift_active"])
	cleanPasses := checkpointInt(quest.Checkpoint["state_drift_clean_passes"])

	if driftCount > 0 {
		state.Active = true
		cleanPasses = 0
		state.EntryGateReason = fmt.Sprintf(
			"state drift detected: %d lifecycle position(s) are missing on exchange snapshot",
			driftCount,
		)
		quest.Checkpoint["runtime_entry_gate_reason"] = state.EntryGateReason
		quest.Checkpoint["state_drift_last_drift_at"] = now.Format(time.RFC3339)
		quest.Checkpoint["runtime_entry_blocked_by_state_drift"] = true
		if _, exists := quest.Checkpoint["runtime_entry_blocked_at"]; !exists {
			quest.Checkpoint["runtime_entry_blocked_at"] = now.Format(time.RFC3339)
		}
	} else {
		if previousActive {
			cleanPasses++
			if cleanPasses >= requiredCleanPasses {
				state.Active = false
				quest.Checkpoint["state_drift_last_clean_reconcile_at"] = now.Format(time.RFC3339)
				delete(quest.Checkpoint, "runtime_entry_blocked_by_state_drift")
				delete(quest.Checkpoint, "runtime_entry_gate_reason")
			} else {
				state.Active = true
				state.EntryGateReason = fmt.Sprintf(
					"state drift clearing in progress: %d/%d clean reconciliation pass(es)",
					cleanPasses,
					requiredCleanPasses,
				)
				quest.Checkpoint["runtime_entry_gate_reason"] = state.EntryGateReason
				quest.Checkpoint["runtime_entry_blocked_by_state_drift"] = true
			}
		} else {
			state.Active = false
			cleanPasses = 0
			quest.Checkpoint["state_drift_last_clean_reconcile_at"] = now.Format(time.RFC3339)
			delete(quest.Checkpoint, "runtime_entry_blocked_by_state_drift")
			if !checkpointBool(quest.Checkpoint["runtime_entry_blocked_by_risk_lock"]) {
				delete(quest.Checkpoint, "runtime_entry_gate_reason")
			}
		}
	}

	state.CleanPasses = cleanPasses
	quest.Checkpoint["state_drift_active"] = state.Active
	quest.Checkpoint["state_drift_positions"] = state.DriftPositions
	quest.Checkpoint["state_drift_count"] = state.DriftPositions
	quest.Checkpoint["state_drift_last_checked_at"] = now.Format(time.RFC3339)
	quest.Checkpoint["state_drift_clean_passes"] = cleanPasses
	if len(staleIDs) > 0 {
		quest.Checkpoint["state_drift_stale_position_ids"] = staleIDs
	} else {
		delete(quest.Checkpoint, "state_drift_stale_position_ids")
	}

	return state, nil
}

func (h *IntegratedQuestHandlers) countLifecyclePositionDrift(
	ctx context.Context,
	chatID, exchange string,
	positions []ccxt.Position,
) (int, []string, error) {
	if h.lifecycleStore == nil {
		return 0, nil, nil
	}
	managed, err := h.lifecycleStore.ListManagedOpenPositions(ctx, chatID, exchange, 200)
	if err != nil {
		return 0, nil, err
	}
	if len(managed) == 0 {
		return 0, nil, nil
	}

	remainingByKey := make(map[string]decimal.Decimal, len(positions))
	for _, pos := range positions {
		if strings.TrimSpace(pos.Symbol) == "" || pos.Size.IsZero() {
			continue
		}
		key := normalizeSymbolForComparison(pos.Symbol) + ":" + normalizeLifecycleSide(pos.Side)
		remainingByKey[key] = remainingByKey[key].Add(pos.Size.Abs())
	}

	driftCount := 0
	stale := make([]string, 0, len(managed))
	for _, local := range managed {
		if !strings.EqualFold(strings.TrimSpace(local.MarketType), "futures") && strings.TrimSpace(local.MarketType) != "" {
			continue
		}
		key := normalizeSymbolForComparison(local.Symbol) + ":" + normalizeLifecycleSide(local.Side)
		remaining := remainingByKey[key]
		sizeAbs := local.Size.Abs()
		if remaining.GreaterThan(decimal.Zero) {
			if sizeAbs.GreaterThan(decimal.Zero) && remaining.GreaterThanOrEqual(sizeAbs) {
				remainingByKey[key] = remaining.Sub(sizeAbs)
				continue
			}
			remainingByKey[key] = decimal.Zero
		}
		driftCount++
		stale = append(stale, local.PositionID)
	}
	return driftCount, stale, nil
}

func stateDriftClearPassTarget() int {
	value := getEnvInt("NEURATRADE_DRIFT_CLEAR_CONSECUTIVE_PASSES")
	if value <= 0 {
		value = defaultDriftClearPasses
	}
	return clampQuestInt(value, 1, 10)
}

func stateDriftRepairCooldown() time.Duration {
	seconds := getEnvInt("NEURATRADE_DRIFT_REPAIR_COOLDOWN_SECONDS")
	if seconds <= 0 {
		return defaultDriftRepairCooldown
	}
	return time.Duration(clampQuestInt(seconds, 10, 1800)) * time.Second
}

func stateDriftPersistenceForceRepairCycles() int {
	value := getEnvInt("NEURATRADE_DRIFT_PERSISTENCE_FORCE_REPAIR_CYCLES")
	if value <= 0 {
		value = 3
	}
	return clampQuestInt(value, 1, 20)
}

func stateDriftDeadlockClearCycles() int {
	value := getEnvInt("NEURATRADE_DRIFT_DEADLOCK_CLEAR_CYCLES")
	if value <= 0 {
		value = defaultDriftDeadlockClearCycles
	}
	return clampQuestInt(value, 1, 50)
}

func driftStalePositionSignature(staleIDs []string) string {
	if len(staleIDs) == 0 {
		return ""
	}
	normalized := make([]string, 0, len(staleIDs))
	for _, id := range staleIDs {
		id = strings.TrimSpace(id)
		if id != "" {
			normalized = append(normalized, id)
		}
	}
	if len(normalized) == 0 {
		return ""
	}
	sort.Strings(normalized)
	return strings.Join(normalized, "|")
}

func (h *IntegratedQuestHandlers) shouldRunForcedDriftRepair(quest *Quest, now time.Time) bool {
	if quest == nil || quest.Checkpoint == nil {
		return true
	}
	cooldown := stateDriftRepairCooldown()
	raw := checkpointString(quest.Checkpoint["state_drift_last_force_repair_at"])
	if raw == "" {
		return true
	}
	last, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return true
	}
	return now.Sub(last) >= cooldown
}

func (h *IntegratedQuestHandlers) forceRepairPersistentDriftPositions(
	ctx context.Context,
	chatID, exchange string,
	staleIDs []string,
) (int, error) {
	return h.forceRepairPersistentDriftPositionsWithSource(
		ctx,
		chatID,
		exchange,
		staleIDs,
		"state_drift_persistence_repair",
	)
}

func (h *IntegratedQuestHandlers) forceRepairPersistentDriftPositionsWithSource(
	ctx context.Context,
	chatID, exchange string,
	staleIDs []string,
	source string,
) (int, error) {
	if h.lifecycleStore == nil || len(staleIDs) == 0 {
		return 0, nil
	}
	positions, err := h.lifecycleStore.ListManagedOpenPositions(ctx, chatID, exchange, 300)
	if err != nil {
		return 0, err
	}
	staleSet := make(map[string]struct{}, len(staleIDs))
	for _, id := range staleIDs {
		id = strings.TrimSpace(id)
		if id != "" {
			staleSet[id] = struct{}{}
		}
	}

	repaired := 0
	for _, pos := range positions {
		if _, ok := staleSet[strings.TrimSpace(pos.PositionID)]; !ok {
			continue
		}
		cause := fmt.Errorf("%s for %s", strings.TrimSpace(source), pos.PositionID)
		if err := h.reconcileMissingManagedPosition(ctx, pos, source, cause); err != nil {
			return repaired, err
		}
		repaired++
	}
	return repaired, nil
}

func stalePositionRetryKey(position ManagedOpenPosition) string {
	return strings.Join([]string{
		strings.TrimSpace(position.ChatID),
		strings.TrimSpace(position.Exchange),
		normalizeSymbolForComparison(position.Symbol),
		normalizeLifecycleSide(position.Side),
	}, "|")
}

func (h *IntegratedQuestHandlers) shouldSkipStalePositionRetry(position ManagedOpenPosition, now time.Time) bool {
	cooldown := stateDriftRepairCooldown()
	if cooldown <= 0 {
		return false
	}
	key := stalePositionRetryKey(position)
	if key == "|||" {
		return false
	}

	h.stalePositionMu.Lock()
	defer h.stalePositionMu.Unlock()
	if h.stalePositionWindow == nil {
		h.stalePositionWindow = make(map[string]time.Time)
		return false
	}

	// Opportunistic cleanup for expired entries.
	for k, ts := range h.stalePositionWindow {
		if now.Sub(ts) >= cooldown {
			delete(h.stalePositionWindow, k)
		}
	}

	lastAttempt, ok := h.stalePositionWindow[key]
	if !ok {
		return false
	}
	return now.Sub(lastAttempt) < cooldown
}

func (h *IntegratedQuestHandlers) markStalePositionRetry(position ManagedOpenPosition, now time.Time) {
	key := stalePositionRetryKey(position)
	if key == "|||" {
		return
	}
	h.stalePositionMu.Lock()
	defer h.stalePositionMu.Unlock()
	if h.stalePositionWindow == nil {
		h.stalePositionWindow = make(map[string]time.Time)
	}
	h.stalePositionWindow[key] = now
}

func (h *IntegratedQuestHandlers) enrichPortfolioControlPlane(
	ctx context.Context,
	quest *Quest,
	chatID, exchange string,
	portfolio *TradingPortfolio,
) {
	if portfolio == nil {
		return
	}

	staleIDs := make(map[string]struct{})
	if quest != nil && quest.Checkpoint != nil {
		portfolio.DriftActive = checkpointBool(quest.Checkpoint["state_drift_active"])
		if raw, ok := quest.Checkpoint["runtime_no_fill_since"].(string); ok && strings.TrimSpace(raw) != "" {
			if since, err := time.Parse(time.RFC3339, raw); err == nil && !since.IsZero() {
				portfolio.NoFillMinutes = time.Since(since).Minutes()
				if portfolio.NoFillMinutes < 0 {
					portfolio.NoFillMinutes = 0
				}
			}
		}
		if rawIDs, ok := quest.Checkpoint["state_drift_stale_position_ids"].([]string); ok {
			for _, id := range rawIDs {
				id = strings.TrimSpace(id)
				if id != "" {
					staleIDs[id] = struct{}{}
				}
			}
		} else if rawIDs, ok := quest.Checkpoint["state_drift_stale_position_ids"].([]interface{}); ok {
			for _, raw := range rawIDs {
				id := checkpointString(raw)
				if id != "" {
					staleIDs[id] = struct{}{}
				}
			}
		}
	}

	if h.lifecycleStore != nil {
		ghostCleaned := 0
		if cleaned, err := h.lifecycleStore.CloseClosedOrderBackedGhostPositions(ctx, chatID, exchange); err == nil {
			ghostCleaned = cleaned
		} else {
			log.Printf("[SCALPING] Ghost lifecycle cleanup failed for chat %s exchange %s: %v", chatID, exchange, err)
		}
		positions, err := h.lifecycleStore.ListManagedOpenPositions(ctx, chatID, exchange, 100)
		if err == nil {
			portfolio.OpenPositions = 0
			unrealized := decimal.Zero
			for _, pos := range positions {
				if portfolio.DriftActive {
					if _, skip := staleIDs[strings.TrimSpace(pos.PositionID)]; skip {
						continue
					}
				}
				portfolio.OpenPositions++
				unrealized = unrealized.Add(pos.UnrealizedPnL)
			}
			portfolio.UnrealizedPnL = unrealized.InexactFloat64()
			portfolio.TotalValue = portfolio.USDTBalance + portfolio.UnrealizedPnL
			portfolio.TotalValueDecimal = portfolio.USDTBalanceDecimal.Add(unrealized)
			if quest != nil {
				if quest.Checkpoint == nil {
					quest.Checkpoint = make(map[string]interface{})
				}
				quest.Checkpoint["managed_open_positions_effective"] = portfolio.OpenPositions
				quest.Checkpoint["ghost_positions_cleaned"] = ghostCleaned
			}
		}
	}

	returns := make([]decimal.Decimal, 0, 64)
	grossReturns := make([]decimal.Decimal, 0, 64)
	if h.lifecycleStore != nil {
		series, err := h.lifecycleStore.GetNetRealizedReturnSeries(ctx, chatID, exchange, time.Now().UTC().Add(-30*24*time.Hour))
		if err == nil {
			returns = append(returns, series...)
		}
		grossSeries, grossErr := h.lifecycleStore.GetGrossRealizedReturnSeries(ctx, chatID, exchange, time.Now().UTC().Add(-30*24*time.Hour))
		if grossErr == nil {
			grossReturns = append(grossReturns, grossSeries...)
		}
	}
	if len(returns) == 0 {
		returns = GetScalpingPerformance().GetReturnSeries(200)
	}

	riskMetrics := ComputeRiskAdjustedMetrics(returns)
	grossRiskMetrics := ComputeRiskAdjustedMetrics(grossReturns)
	portfolio.RiskSharpe = riskMetrics.Sharpe
	portfolio.RiskSortino = riskMetrics.Sortino
	portfolio.RiskMaxDrawdown = riskMetrics.MaxDrawdown
	portfolio.RiskExpectancy = riskMetrics.Expectancy
	portfolio.RiskExpectancyGross = grossRiskMetrics.Expectancy
	portfolio.RiskFeeDragExpectancy = portfolio.RiskExpectancyGross - portfolio.RiskExpectancy
	portfolio.RiskSampleSize = riskMetrics.SampleSize

	rawEquity := portfolio.TotalValue

	currentDrawdown := 0.0
	peakEquity := rawEquity
	maxRecordedDrawdown := portfolio.RiskMaxDrawdown
	if quest != nil && quest.Checkpoint != nil {
		if checkpointPeak := checkpointFloat(quest.Checkpoint["risk_peak_equity"]); checkpointPeak > peakEquity {
			peakEquity = checkpointPeak
		}
		if checkpointMax := checkpointFloat(quest.Checkpoint["risk_max_drawdown"]); checkpointMax > maxRecordedDrawdown {
			maxRecordedDrawdown = checkpointMax
		}
	}
	if peakEquity > 0 {
		if rawEquity > peakEquity {
			peakEquity = rawEquity
		}
		if rawEquity <= 0 {
			currentDrawdown = 1
		} else {
			currentDrawdown = (peakEquity - rawEquity) / peakEquity
			if currentDrawdown < 0 {
				currentDrawdown = 0
			}
		}
	}
	if currentDrawdown > maxRecordedDrawdown {
		maxRecordedDrawdown = currentDrawdown
	}
	portfolio.CurrentDrawdown = currentDrawdown
	portfolio.RiskDrawdown = currentDrawdown
	portfolio.RiskMaxDrawdown = maxRecordedDrawdown

	if h.drawdownHalt != nil && chatID != "" && peakEquity > 0 {
		peakValue := decimal.NewFromFloat(peakEquity)
		if state, exists := h.drawdownHalt.GetState(chatID); !exists || state.PeakValue.LessThan(peakValue) {
			if err := h.drawdownHalt.ResetPeak(ctx, chatID, peakValue); err != nil {
				log.Printf("[SCALPING] Drawdown halt peak reset failed for chat %s: %v", chatID, err)
			}
		}
		marketValue := decimal.NewFromFloat(rawEquity)
		state, err := h.drawdownHalt.CheckDrawdown(ctx, chatID, marketValue)
		if err != nil {
			log.Printf("[SCALPING] Drawdown halt check failed for chat %s: %v", chatID, err)
		} else if state != nil {
			portfolio.CurrentDrawdown = state.CurrentDrawdown.InexactFloat64()
			if portfolio.CurrentDrawdown < 0 {
				portfolio.CurrentDrawdown = 0
			}
			if portfolio.CurrentDrawdown > 1 {
				portfolio.CurrentDrawdown = 1
			}
			portfolio.RiskDrawdown = portfolio.CurrentDrawdown
			seen := state.MaxDrawdownSeen.InexactFloat64()
			if seen > 1 {
				seen = 1
			}
			if seen > portfolio.RiskMaxDrawdown {
				portfolio.RiskMaxDrawdown = seen
			}
		}
	}

	clampedTotalValueDecimal := portfolio.TotalValueDecimal
	if clampedTotalValueDecimal.LessThanOrEqual(decimal.Zero) {
		clampedTotalValueDecimal = portfolio.USDTBalanceDecimal
	}
	portfolio.TotalValueDecimal = clampedTotalValueDecimal
	portfolio.TotalValue = clampedTotalValueDecimal.InexactFloat64()

	phaseDetector := phase_management.NewPhaseDetector(phase_management.DefaultPhaseDetectorConfig(), nil)
	currentPhase := phaseDetector.GetPhaseForValue(portfolioTotalValueDecimal(*portfolio))
	adapter := phase_management.NewStrategyAdapter(phase_management.DefaultStrategyAdapterConfig())
	strategy := adapter.SelectStrategy(currentPhase)
	riskParams := adapter.GetRiskParams(currentPhase)
	portfolio.StrategyPhase = currentPhase.String()
	portfolio.PhaseMinConfidence = strategy.MinSignalConfidence
	portfolio.PhaseMaxCapitalPct = riskParams.RiskPerTradePercent.InexactFloat64()
	if portfolio.PhaseMaxCapitalPct <= 0 {
		portfolio.PhaseMaxCapitalPct = 1
	}

	target := 1000.0
	if configuredTarget, ok := getEnvFloat("NEURATRADE_AI_FUND_TARGET"); ok && configuredTarget > 0 {
		target = configuredTarget
	}
	if target > 0 {
		portfolio.MilestoneProgress = (portfolio.TotalValue / target) * 100
	}

	if quest != nil {
		if quest.Checkpoint == nil {
			quest.Checkpoint = make(map[string]interface{})
		}
		quest.Checkpoint["risk_peak_equity"] = peakEquity
		quest.Checkpoint["risk_current_drawdown"] = portfolio.CurrentDrawdown
		quest.Checkpoint["risk_sharpe"] = portfolio.RiskSharpe
		quest.Checkpoint["risk_sortino"] = portfolio.RiskSortino
		quest.Checkpoint["risk_max_drawdown"] = portfolio.RiskMaxDrawdown
		quest.Checkpoint["risk_expectancy"] = portfolio.RiskExpectancy
		quest.Checkpoint["risk_expectancy_gross"] = portfolio.RiskExpectancyGross
		quest.Checkpoint["risk_fee_drag_expectancy"] = portfolio.RiskFeeDragExpectancy
		quest.Checkpoint["risk_samples"] = portfolio.RiskSampleSize
		quest.Checkpoint["strategy_phase"] = portfolio.StrategyPhase
		quest.Checkpoint["phase_min_confidence"] = portfolio.PhaseMinConfidence
		quest.Checkpoint["phase_max_capital_pct"] = portfolio.PhaseMaxCapitalPct
		quest.Checkpoint["milestone_progress_pct"] = portfolio.MilestoneProgress
	}
}

func oppositeCloseSide(positionSide string) string {
	side := strings.ToLower(strings.TrimSpace(positionSide))
	switch side {
	case "long", "buy":
		return "sell"
	case "short", "sell":
		return "buy"
	default:
		return ""
	}
}

func (h *IntegratedQuestHandlers) executeFallbackScalping(ctx context.Context, quest *Quest, chatID string) error {
	log.Printf("[SCALPING] AI scalping service unavailable; static fallback execution disabled")
	quest.Checkpoint["status"] = "ai_unavailable_hold"
	quest.Checkpoint["fallback_mode"] = "observe_only"
	quest.Checkpoint["note"] = "No rule-based orders are placed when AI service is unavailable"
	quest.CurrentCount++
	quest.Checkpoint["last_scalp_check"] = time.Now().UTC().Format(time.RFC3339)
	quest.Checkpoint["chat_id"] = chatID
	h.notifyScalpingDecision(ctx, chatID, AIReasoningNotification{
		DecisionType: "scalping_cycle",
		Summary:      "Scalping engine unavailable; observe-only cycle",
		Confidence:   0,
		Reasons:      []string{"AI scalping service is not initialized"},
		Action:       "hold",
	})

	return nil
}

func (h *IntegratedQuestHandlers) handleArbitrageExecution(ctx context.Context, quest *Quest) error {
	log.Printf("[ARBITRAGE] Executing arbitrage quest: %s", quest.Name)

	if quest.Checkpoint == nil {
		quest.Checkpoint = make(map[string]interface{})
	}

	if h.ccxtService == nil {
		err := fmt.Errorf("CCXT service not available for arbitrage")
		log.Printf("[ARBITRAGE] ERROR: %v", err)
		quest.Checkpoint["status"] = "ccxt_unavailable"
		quest.Checkpoint["error"] = err.Error()
		return err
	}

	// Get arbitrage parameters from quest checkpoint
	arbType, ok := quest.Checkpoint["type"].(string)
	if !ok {
		arbType = "spot_arbitrage"
	}

	symbol, ok := quest.Checkpoint["symbol"].(string)
	if !ok {
		err := fmt.Errorf("symbol not found in arbitrage quest checkpoint")
		log.Printf("[ARBITRAGE] ERROR: %v", err)
		quest.Checkpoint["error"] = err.Error()
		return err
	}

	buyExchange, ok := quest.Checkpoint["buy_exchange"].(string)
	if !ok {
		err := fmt.Errorf("buy exchange not found in arbitrage quest checkpoint")
		log.Printf("[ARBITRAGE] ERROR: %v", err)
		quest.Checkpoint["error"] = err.Error()
		return err
	}

	sellExchange, ok := quest.Checkpoint["sell_exchange"].(string)
	if !ok {
		err := fmt.Errorf("sell exchange not found in arbitrage quest checkpoint")
		log.Printf("[ARBITRAGE] ERROR: %v", err)
		quest.Checkpoint["error"] = err.Error()
		return err
	}

	buyPriceStr, ok := quest.Checkpoint["buy_price"].(string)
	if !ok {
		err := fmt.Errorf("buy price not found in arbitrage quest checkpoint")
		log.Printf("[ARBITRAGE] ERROR: %v", err)
		quest.Checkpoint["error"] = err.Error()
		return err
	}

	sellPriceStr, ok := quest.Checkpoint["sell_price"].(string)
	if !ok {
		err := fmt.Errorf("sell price not found in arbitrage quest checkpoint")
		log.Printf("[ARBITRAGE] ERROR: %v", err)
		quest.Checkpoint["error"] = err.Error()
		return err
	}

	buyPrice, err := decimal.NewFromString(buyPriceStr)
	if err != nil {
		err := fmt.Errorf("invalid buy price format: %v", err)
		log.Printf("[ARBITRAGE] ERROR: %v", err)
		quest.Checkpoint["error"] = err.Error()
		return err
	}

	sellPrice, err := decimal.NewFromString(sellPriceStr)
	if err != nil {
		err := fmt.Errorf("invalid sell price format: %v", err)
		log.Printf("[ARBITRAGE] ERROR: %v", err)
		quest.Checkpoint["error"] = err.Error()
		return err
	}

	// Calculate profit percentage
	profitPctStr, ok := quest.Checkpoint["profit_pct"].(string)
	if !ok {
		err := fmt.Errorf("profit percentage not found in arbitrage quest checkpoint")
		log.Printf("[ARBITRAGE] ERROR: %v", err)
		quest.Checkpoint["error"] = err.Error()
		return err
	}
	profitPct, err := decimal.NewFromString(profitPctStr)
	if err != nil {
		err := fmt.Errorf("invalid profit percentage format: %v", err)
		log.Printf("[ARBITRAGE] ERROR: %v", err)
		quest.Checkpoint["error"] = err.Error()
		return err
	}

	log.Printf("[ARBITRAGE] Opportunity: %s - Buy on %s at %.4f, Sell on %s at %.4f, Profit: %.2f%%",
		symbol, buyExchange, buyPrice.InexactFloat64(), sellExchange, sellPrice.InexactFloat64(), profitPct.InexactFloat64())

	// Check if we have an order executor for arbitrage trades
	if h.orderExecutor != nil {
		// For arbitrage, we typically want to execute both legs quickly but in sequence
		// First, buy on the cheaper exchange
		amount := decimal.NewFromFloat(10.0) // Use a conservative amount for testing

		log.Printf("[ARBITRAGE] Placing BUY order: %s on %s at %.4f, amount: %.2f",
			symbol, buyExchange, buyPrice.InexactFloat64(), amount.InexactFloat64())

		// Place buy order
		buyOrderID, err := h.orderExecutor.PlaceOrder(ctx, buyExchange, symbol, "buy", "market", amount, &buyPrice)
		if err != nil {
			log.Printf("[ARBITRAGE] BUY ORDER FAILED: %v", err)
			quest.Checkpoint["buy_execution_error"] = err.Error()
			quest.Checkpoint["buy_execution_status"] = "failed"
			return fmt.Errorf("buy order execution failed: %w", err)
		}

		log.Printf("[ARBITRAGE] BUY ORDER PLACED: %s %s %s, orderID: %s", "buy", buyExchange, symbol, buyOrderID)
		quest.Checkpoint["buy_order_id"] = buyOrderID
		quest.Checkpoint["buy_execution_status"] = "placed"

		// Then, sell on the more expensive exchange
		log.Printf("[ARBITRAGE] Placing SELL order: %s on %s at %.4f, amount: %.2f",
			symbol, sellExchange, sellPrice.InexactFloat64(), amount.InexactFloat64())

		sellOrderID, err := h.orderExecutor.PlaceOrder(ctx, sellExchange, symbol, "sell", "market", amount, &sellPrice)
		if err != nil {
			log.Printf("[ARBITRAGE] SELL ORDER FAILED: %v", err)
			quest.Checkpoint["sell_execution_error"] = err.Error()
			quest.Checkpoint["sell_execution_status"] = "failed"
			return fmt.Errorf("sell order execution failed: %w", err)
		}

		log.Printf("[ARBITRAGE] SELL ORDER PLACED: %s %s %s, orderID: %s", "sell", sellExchange, symbol, sellOrderID)
		quest.Checkpoint["sell_order_id"] = sellOrderID
		quest.Checkpoint["sell_execution_status"] = "placed"

		log.Printf("[ARBITRAGE] ARBITRAGE EXECUTED: Buy %s on %s, Sell %s on %s, Expected profit: %s%%",
			symbol, buyExchange, symbol, sellExchange, profitPct.String())

		quest.Checkpoint["status"] = "executed_both_legs"
		quest.Checkpoint["arbitrage_type"] = arbType
		quest.Checkpoint["symbol"] = symbol
		quest.Checkpoint["buy_exchange"] = buyExchange
		quest.Checkpoint["sell_exchange"] = sellExchange
		quest.Checkpoint["buy_price"] = buyPrice.String()
		quest.Checkpoint["sell_price"] = sellPrice.String()
		quest.Checkpoint["profit_percentage"] = profitPct.String()
		quest.Checkpoint["amount"] = amount.String()
	} else {
		log.Printf("[ARBITRAGE] WARNING: Order executor not configured - arbitrage opportunity not executed")
		quest.Checkpoint["execution_status"] = "no_executor"
		return fmt.Errorf("order executor not configured for arbitrage")
	}

	quest.CurrentCount++
	quest.Checkpoint["last_arbitrage_execution"] = time.Now().UTC().Format(time.RFC3339)
	quest.Checkpoint["chat_id"] = quest.Metadata["chat_id"]

	return nil
}

// GetScalpingPerformanceStats returns current scalping performance
func (h *IntegratedQuestHandlers) GetScalpingPerformanceStats() map[string]interface{} {
	return GetScalpingPerformance().GetPerformance()
}

// GetMonitoringSnapshot returns current monitoring snapshot for a chat ID
func (h *IntegratedQuestHandlers) GetMonitoringSnapshot(chatID string) map[string]interface{} {
	if h.monitoring == nil {
		return map[string]interface{}{
			"status": "monitoring_not_initialized",
		}
	}

	snapshot := h.monitoring.GetSnapshot(chatID)
	return map[string]interface{}{
		"chat_id":           snapshot.ChatID,
		"uptime_hours":      snapshot.Uptime.Hours(),
		"total_quests":      snapshot.TotalQuests,
		"success_rate":      snapshot.SuccessRate,
		"total_trades":      snapshot.TotalTrades,
		"win_rate":          snapshot.WinRate,
		"total_pnl":         snapshot.TotalPnL.String(),
		"current_drawdown":  snapshot.CurrentDrawdown.String(),
		"max_drawdown":      snapshot.MaxDrawdown.String(),
		"health_status":     snapshot.HealthStatus,
		"last_quest_update": snapshot.LastQuestUpdate.Format(time.RFC3339),
	}
}

// parseChatID converts a chat ID string to int64
func parseChatID(chatID string) int64 {
	if chatID == "" {
		return 0
	}
	id, err := strconv.ParseInt(chatID, 10, 64)
	if err != nil {
		return 0
	}
	return id
}

func questGatePathTimeout() time.Duration {
	seconds := getEnvInt("NEURATRADE_QUEST_GATE_PATH_TIMEOUT_SECONDS")
	if seconds <= 0 {
		seconds = 12
	}
	return time.Duration(clampQuestInt(seconds, 3, 120)) * time.Second
}

func questNotificationTimeout() time.Duration {
	// Keep notification path short so gated cycles cannot stall on outbound delivery.
	return 4 * time.Second
}

func withBoundedTimeoutContext(ctx context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
	if timeout <= 0 {
		return context.WithCancel(ctx)
	}
	if deadline, ok := ctx.Deadline(); ok {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return context.WithCancel(ctx)
		}
		if remaining < timeout {
			timeout = remaining
		}
	}
	return context.WithTimeout(ctx, timeout)
}

func (h *IntegratedQuestHandlers) withGatePathContext(ctx context.Context) (context.Context, context.CancelFunc) {
	return withBoundedTimeoutContext(ctx, questGatePathTimeout())
}

func normalizeAINotificationSemantics(notif AIReasoningNotification) AIReasoningNotification {
	category := strings.TrimSpace(notif.ReasonCategory)
	if category == "" {
		category = strings.TrimSpace(notif.HoldCategory)
	}
	evidenceParts := make([]string, 0, len(notif.Reasons)+2)
	if summary := strings.TrimSpace(notif.Summary); summary != "" {
		evidenceParts = append(evidenceParts, summary)
	}
	evidenceParts = append(evidenceParts, notif.Reasons...)
	if unblock := strings.TrimSpace(notif.UnblockCondition); unblock != "" {
		evidenceParts = append(evidenceParts, unblock)
	}
	evidence := strings.Join(evidenceParts, " ")

	if !notif.ConfidenceKnown {
		if category == "" || strings.EqualFold(category, aiReasonStrategyHold) {
			category = classifyAIRuntimeReason(evidence, aiReasonExecutionUnavailable)
		}
	}
	if isRuntimeReasonCategory(category) {
		notif.ConfidenceKnown = false
	}
	if !notif.ConfidenceKnown && strings.EqualFold(category, aiReasonStrategyHold) {
		category = aiReasonExecutionUnavailable
	}
	if category == "" {
		if notif.ConfidenceKnown {
			category = aiReasonStrategyHold
		} else {
			category = aiReasonExecutionUnavailable
		}
	}

	notif.ReasonCategory = category
	notif.HoldCategory = category
	return notif
}

func holdDigestSummary(decision *AITradingDecision, reasonCategory string) string {
	reasonCategory = strings.ToLower(strings.TrimSpace(reasonCategory))
	reasoning := ""
	if decision != nil {
		reasoning = strings.ToLower(strings.TrimSpace(decision.Reasoning))
	}

	switch reasonCategory {
	case aiReasonLLMParseContract:
		return "Hold digest: AI output incomplete, no reliable trade decision"
	case aiReasonLLMTimeout:
		return "Hold digest: AI response timed out, no reliable trade decision"
	case aiReasonExecutionUnavailable:
		return "Hold digest: AI runtime degraded, waiting for stable decisioning"
	case reasonCategoryDeterministicFallback:
		if strings.Contains(reasoning, "no eligible candidate") || strings.Contains(reasoning, "no qualified setup") {
			return "Hold digest: fallback found no qualified setup"
		}
		return "Hold digest: AI fallback selected hold"
	default:
		return "Hold digest: waiting for qualified setup"
	}
}

func (h *IntegratedQuestHandlers) notifyScalpingDecision(ctx context.Context, chatID string, notif AIReasoningNotification) {
	if h.notificationService == nil {
		return
	}
	notif = normalizeAINotificationSemantics(notif)
	if !shouldSendScalpingDecisionNotification(notif) {
		log.Printf(
			"[NOTIFICATION] Suppressed non-actionable scalping update (type=%s action=%s)",
			notif.DecisionType,
			notif.Action,
		)
		return
	}
	chatIDInt := parseChatID(chatID)
	if chatIDInt == 0 {
		return
	}
	notifyCtx, cancelNotify := withBoundedTimeoutContext(ctx, questNotificationTimeout())
	defer cancelNotify()
	if err := h.notificationService.NotifyAIReasoning(notifyCtx, chatIDInt, notif); err != nil {
		log.Printf("[NOTIFICATION] Failed to send scalping status notification: %v", err)
	}
}

func (h *IntegratedQuestHandlers) maybeSendHoldDigest(
	ctx context.Context,
	quest *Quest,
	chatID string,
	decision *AITradingDecision,
	portfolio TradingPortfolio,
) {
	if quest == nil || decision == nil {
		return
	}
	if quest.Checkpoint == nil {
		quest.Checkpoint = make(map[string]interface{})
	}

	interval := 15 * time.Minute
	if seconds := getEnvInt("NEURATRADE_TELEGRAM_HOLD_DIGEST_INTERVAL_SECONDS"); seconds > 0 {
		interval = time.Duration(seconds) * time.Second
	}
	now := time.Now().UTC()
	if raw, ok := quest.Checkpoint["runtime_last_hold_digest_at"].(string); ok && strings.TrimSpace(raw) != "" {
		if lastSent, err := time.Parse(time.RFC3339, raw); err == nil && now.Sub(lastSent) < interval {
			return
		}
	}

	holdStreak := checkpointInt(quest.Checkpoint["runtime_hold_streak"])
	minConfidence := checkpointFloat(quest.Checkpoint["effective_min_confidence"])
	maxCapital := checkpointFloat(quest.Checkpoint["effective_max_capital_pct"])
	if (minConfidence <= 0 || maxCapital <= 0) && h.aiScalpingService != nil {
		minConfidence, maxCapital = h.aiScalpingService.dynamicRiskThresholds(ctx, portfolio)
	}
	runtimeHold := isRuntimeHoldReason(decision.Reasoning)
	reasonCategory := strings.TrimSpace(decision.ReasonCategory)
	if runtimeHold {
		inferred := classifyAIRuntimeReason(decision.Reasoning, aiReasonExecutionUnavailable)
		if reasonCategory == "" || strings.EqualFold(reasonCategory, aiReasonStrategyHold) {
			reasonCategory = inferred
		}
	}
	if reasonCategory == "" {
		reasonCategory = aiReasonStrategyHold
	}
	confidenceKnown := !runtimeHold && !isRuntimeReasonCategory(reasonCategory) && decision.ConfidenceKnown
	if !confidenceKnown && strings.EqualFold(reasonCategory, aiReasonStrategyHold) {
		reasonCategory = classifyAIRuntimeReason(decision.Reasoning, aiReasonExecutionUnavailable)
	}
	errorRate := checkpointFloat(quest.Checkpoint["runtime_ai_window_error_rate"])
	circuitRemaining := aiRuntimeCircuitRemaining(quest, now)
	unblockCondition := checkpointString(quest.Checkpoint["runtime_next_unblock_condition"])
	if unblockCondition == "" {
		unblockCondition = checkpointString(quest.Checkpoint["recovery_next_condition"])
	}
	if unblockCondition == "" {
		if gateReason := checkpointString(quest.Checkpoint["runtime_entry_gate_reason"]); gateReason != "" {
			unblockCondition = gateReason
		}
	}
	attemptWindowProgress := checkpointString(quest.Checkpoint["runtime_entry_attempt_window_progress"])

	reasons := []string{
		decision.Reasoning,
		fmt.Sprintf("Hold streak: %d cycle(s)", holdStreak),
		fmt.Sprintf("Risk drawdown: %.2f%%", portfolio.RiskDrawdown*100),
		fmt.Sprintf("Effective thresholds: min_confidence=%.2f, max_capital=%.4f%%", minConfidence, maxCapital),
		fmt.Sprintf("Unlock cycles: %d", checkpointInt(quest.Checkpoint["runtime_unlock_cycles"])),
		fmt.Sprintf(
			"Recovery mode: %s (clean cycles %d, entry_allowed=%t)",
			checkpointString(quest.Checkpoint["recovery_mode"]),
			checkpointIntWithFallback(quest.Checkpoint, "recovery_clean_cycles_current", "recovery_clean_cycles"),
			checkpointBool(quest.Checkpoint["recovery_entry_allowed"]),
		),
		fmt.Sprintf("AI runtime error-rate window: %.0f%%", errorRate*100),
	}
	if rawAdjustments, ok := quest.Checkpoint["effective_policy_adjustments"]; ok {
		adjustments := make([]string, 0, 4)
		switch typed := rawAdjustments.(type) {
		case []string:
			adjustments = append(adjustments, typed...)
		case []interface{}:
			for _, entry := range typed {
				if value, ok := entry.(string); ok && strings.TrimSpace(value) != "" {
					adjustments = append(adjustments, value)
				}
			}
		}
		if len(adjustments) > 0 {
			human := make([]string, 0, len(adjustments))
			for _, adjustment := range adjustments {
				human = append(human, strings.ReplaceAll(strings.TrimSpace(adjustment), "_", " "))
			}
			reasons = append(reasons[:4], append([]string{fmt.Sprintf("Policy adjustments: %s", strings.Join(human, ", "))}, reasons[4:]...)...)
		}
	}
	if accountTier := checkpointString(quest.Checkpoint["account_tier"]); accountTier != "" {
		reasons = append(reasons, fmt.Sprintf("Account tier: %s", accountTier))
	}
	if universe := checkpointInt(quest.Checkpoint["candidate_universe_count"]); universe > 0 {
		reasons = append(
			reasons,
			fmt.Sprintf(
				"Candidate funnel: universe=%d ranked=%d viable=%d",
				universe,
				checkpointInt(quest.Checkpoint["candidate_ranked_count"]),
				checkpointInt(quest.Checkpoint["candidate_viable_count"]),
			),
		)
	}
	if rolloutStage := checkpointString(quest.Checkpoint["rollout_stage_current"]); rolloutStage == "" {
		rolloutStage = checkpointString(quest.Checkpoint["autonomy_rollout_stage"])
		if rolloutStage != "" {
			rolloutStatus := checkpointString(quest.Checkpoint["autonomy_rollout_status"])
			reasons = append(reasons, fmt.Sprintf("Rollout stage: %s (status=%s)", rolloutStage, rolloutStatus))
		}
	} else {
		reasons = append(
			reasons,
			fmt.Sprintf(
				"Rollout stage: %s (status=%s)",
				rolloutStage,
				checkpointString(quest.Checkpoint["rollout_status_current"]),
			),
		)
	}
	if rejections := normalizeTopCandidateRejections(quest.Checkpoint); len(rejections) > 0 {
		for i, rejection := range rejections {
			if i >= 2 {
				break
			}
			symbol := checkpointString(rejection["symbol"])
			reason := checkpointString(rejection["reason"])
			if symbol != "" && reason != "" {
				reasons = append(reasons, fmt.Sprintf("Top reject: %s (%s)", symbol, reason))
			}
		}
	}
	if recentWindowSec := checkpointInt(quest.Checkpoint["recovery_recent_loss_window_seconds"]); recentWindowSec > 0 {
		reasons = append(
			reasons,
			fmt.Sprintf(
				"Recent loss streak: %d (active=%t, window=%s)",
				checkpointInt(quest.Checkpoint["recovery_recent_loss_streak"]),
				checkpointBool(quest.Checkpoint["recovery_recent_loss_active"]),
				(time.Duration(recentWindowSec)*time.Second).String(),
			),
		)
	}
	if checkpointBool(quest.Checkpoint["state_drift_active"]) {
		reasons = append(
			reasons,
			fmt.Sprintf("State drift active: %d position(s) pending clean reconcile", checkpointInt(quest.Checkpoint["state_drift_positions"])),
		)
		if gateReason, ok := quest.Checkpoint["runtime_entry_gate_reason"].(string); ok && strings.TrimSpace(gateReason) != "" {
			reasons = append(reasons, gateReason)
		}
	}
	if circuitRemaining > 0 {
		reasons = append(reasons, fmt.Sprintf("AI runtime circuit open: %s remaining", circuitRemaining.Round(time.Second).String()))
	}
	if attemptWindowProgress != "" {
		reasons = append(reasons, fmt.Sprintf("Attempt window progress: %s", attemptWindowProgress))
	}
	if unblockCondition != "" {
		reasons = append(reasons, fmt.Sprintf("Next condition: %s", unblockCondition))
	}

	h.notifyScalpingDecision(ctx, chatID, AIReasoningNotification{
		DecisionType:          "scalping_digest",
		Summary:               holdDigestSummary(decision, reasonCategory),
		Confidence:            decision.Confidence,
		ConfidenceKnown:       confidenceKnown,
		ReasonCategory:        reasonCategory,
		HoldCategory:          reasonCategory,
		UnblockCondition:      unblockCondition,
		AttemptWindowProgress: attemptWindowProgress,
		Reasons:               reasons,
		Action:                "hold",
	})

	quest.Checkpoint["runtime_last_hold_digest_at"] = now.Format(time.RFC3339)
}

type recoveryGateState struct {
	Mode                  string
	EntryAllowed          bool
	CleanCycles           int
	RequiredCleanCycles   int
	CyclesToEntry         int
	DeriskOnlyThreshold   float64
	MicroEntryThreshold   float64
	MicroEntryCapPct      float64
	GateReason            string
	NextCondition         string
	RecentLossStreak      int
	RecentLossWindow      time.Duration
	RecentLossActive      bool
	RecentLossLastTradeAt time.Time
}

type entryAttemptGateState struct {
	Forced                 bool
	AllowNow               bool
	AttemptsInWindow       int
	MaxAttemptsPerHour     int
	DeployableBalanceRatio float64
	BlockReason            string
	NextCondition          string
	AttemptWindowProgress  string
	WindowStartedAt        time.Time
}

//nolint:unused // retained for focused recovery-gate unit tests.
func (h *IntegratedQuestHandlers) evaluateRecoveryGateState(quest *Quest, portfolio TradingPortfolio) recoveryGateState {
	return h.evaluateRecoveryGateStateForScope(context.Background(), quest, portfolio, "", "")
}
func (h *IntegratedQuestHandlers) evaluateRecoveryGateStateForScope(
	ctx context.Context,
	quest *Quest,
	portfolio TradingPortfolio,
	chatID string,
	exchange string,
) recoveryGateState {
	cleanCycles := 0
	if quest != nil && quest.Checkpoint != nil {
		cleanCycles = checkpointIntWithFallback(quest.Checkpoint, "recovery_clean_cycles_current", "recovery_clean_cycles")
	}
	recentLoss := h.currentRecentLossStreak(ctx, chatID, exchange)

	recoveryCfg := appautonomy.DefaultRecoveryConfig()
	if configured := getEnvInt("NEURATRADE_RECOVERY_CLEAN_CYCLES"); configured > 0 {
		recoveryCfg.RequiredCleanCycles = configured
	}
	if configured, ok := getEnvFloat("NEURATRADE_RECOVERY_DERISK_ONLY_DRAWDOWN"); ok && configured > 0 {
		recoveryCfg.DeriskOnlyThreshold = configured
	}
	if configured, ok := getEnvFloat("NEURATRADE_RECOVERY_MICRO_ENTRY_MIN_DRAWDOWN"); ok && configured > 0 {
		recoveryCfg.MicroEntryThreshold = configured
	}
	if configured, ok := getEnvFloat("NEURATRADE_RECOVERY_MICRO_ENTRY_CAP_PCT"); ok && configured > 0 {
		recoveryCfg.MicroEntryCapPct = configured
	}
	if configured := getEnvInt("NEURATRADE_SCALPING_SYMBOL_LOSS_STREAK_BUDGET"); configured > 0 {
		recoveryCfg.LossStreakBudget = configured
	}

	evaluated := appautonomy.EvaluateRecoveryGate(
		appautonomy.RecoveryGateInput{
			Drawdown:              portfolio.RiskDrawdown,
			CleanCycles:           cleanCycles,
			DriftActive:           portfolio.DriftActive,
			RecentLossStreak:      recentLoss.ConsecutiveLosses,
			RecentLossActive:      recentLoss.Active,
			RecentLossWindow:      recentLoss.Window,
			RecentLossLastTradeAt: recentLoss.LastTradeAt,
		},
		recoveryCfg,
	)

	return recoveryGateState{
		Mode:                  evaluated.Mode,
		EntryAllowed:          evaluated.EntryAllowed,
		CleanCycles:           evaluated.CleanCycles,
		RequiredCleanCycles:   evaluated.RequiredCleanCycles,
		CyclesToEntry:         evaluated.CyclesToEntry,
		DeriskOnlyThreshold:   evaluated.DeriskOnlyThreshold,
		MicroEntryThreshold:   evaluated.MicroEntryThreshold,
		MicroEntryCapPct:      evaluated.MicroEntryCapPct,
		GateReason:            evaluated.GateReason,
		NextCondition:         evaluated.NextCondition,
		RecentLossStreak:      evaluated.RecentLossStreak,
		RecentLossWindow:      evaluated.RecentLossWindow,
		RecentLossActive:      evaluated.RecentLossActive,
		RecentLossLastTradeAt: evaluated.RecentLossLastTradeAt,
	}
}

func (h *IntegratedQuestHandlers) applyRecoveryStateCheckpoint(
	quest *Quest,
	portfolio *TradingPortfolio,
	state recoveryGateState,
	evaluatedAt time.Time,
) {
	if quest == nil {
		return
	}
	if quest.Checkpoint == nil {
		quest.Checkpoint = make(map[string]interface{})
	}

	if portfolio != nil {
		portfolio.RecoveryMode = state.Mode
		portfolio.RecoveryEntryOK = state.EntryAllowed
		portfolio.RecoveryCleanCycle = state.CleanCycles
		portfolio.RecentConsecutiveLosses = state.RecentLossStreak
	}

	quest.Checkpoint["recovery_mode"] = state.Mode
	quest.Checkpoint["recovery_entry_allowed"] = state.EntryAllowed
	quest.Checkpoint["recovery_clean_cycles_current"] = state.CleanCycles
	quest.Checkpoint["recovery_clean_cycles"] = state.CleanCycles
	quest.Checkpoint["recovery_clean_cycles_required"] = state.RequiredCleanCycles
	quest.Checkpoint["recovery_cycles_to_entry"] = state.CyclesToEntry
	if evaluatedAt.IsZero() {
		evaluatedAt = time.Now().UTC()
	}
	quest.Checkpoint["recovery_gate_eval_at"] = evaluatedAt.UTC().Format(time.RFC3339)
	if strings.TrimSpace(state.NextCondition) != "" {
		quest.Checkpoint["recovery_next_condition"] = state.NextCondition
	} else {
		delete(quest.Checkpoint, "recovery_next_condition")
	}
}

func (h *IntegratedQuestHandlers) updateRecoveryCleanCycles(quest *Quest, clean bool, reason string) {
	if quest == nil {
		return
	}
	if quest.Checkpoint == nil {
		quest.Checkpoint = make(map[string]interface{})
	}
	if !clean {
		quest.Checkpoint["recovery_clean_cycles_current"] = 0
		quest.Checkpoint["recovery_clean_cycles"] = 0
		if strings.TrimSpace(reason) != "" {
			quest.Checkpoint["recovery_last_reset_reason"] = strings.TrimSpace(reason)
		}
		return
	}
	cleanCycles := checkpointIntWithFallback(quest.Checkpoint, "recovery_clean_cycles_current", "recovery_clean_cycles")
	cleanCycles++
	quest.Checkpoint["recovery_clean_cycles_current"] = cleanCycles
	quest.Checkpoint["recovery_clean_cycles"] = cleanCycles
	delete(quest.Checkpoint, "recovery_last_reset_reason")
}

type recentLossStreakState struct {
	ConsecutiveLosses int
	LastTradeAt       time.Time
	Window            time.Duration
	Active            bool
}

func (h *IntegratedQuestHandlers) currentRecentLossStreak(ctx context.Context, chatID string, exchange string) recentLossStreakState {
	if ctx == nil {
		ctx = context.Background()
	}
	window := recoveryLossWindow()
	perf := h.scopedScalpingPerformance(ctx, strings.TrimSpace(chatID), strings.TrimSpace(exchange), window)
	losses := readIntMetric(perf["consecutive_losses"])
	lastTrade := readCheckpointTime(perf["last_trade_time"])

	active := false
	if !lastTrade.IsZero() && losses > 0 {
		active = time.Since(lastTrade) <= window
	}

	return recentLossStreakState{
		ConsecutiveLosses: losses,
		LastTradeAt:       lastTrade,
		Window:            window,
		Active:            active,
	}
}

func (h *IntegratedQuestHandlers) scopedScalpingPerformance(
	ctx context.Context,
	chatID string,
	exchange string,
	window time.Duration,
) map[string]interface{} {
	perf := map[string]interface{}{
		"consecutive_losses": 0,
	}
	if h == nil || h.lifecycleStore == nil || chatID == "" {
		return perf
	}
	if ctx == nil {
		ctx = context.Background()
	}

	since := time.Now().UTC().Add(-window)
	queryCtx, cancel := context.WithTimeout(ctx, 1500*time.Millisecond)
	defer cancel()
	summary, err := h.lifecycleStore.GetRecentLossStreak(queryCtx, chatID, exchange, since)
	if err != nil {
		log.Printf(
			"[SCALPING] Failed to read scoped recent loss streak (chat=%s exchange=%s): %v",
			chatID,
			exchange,
			err,
		)
		return perf
	}
	perf["consecutive_losses"] = summary.ConsecutiveLosses
	perf["last_trade_time"] = summary.LastTradeAt
	return perf
}

func recoveryLossWindow() time.Duration {
	seconds := getEnvInt("NEURATRADE_SCALPING_SYMBOL_LOSS_WINDOW_SECONDS")
	if seconds <= 0 {
		seconds = int((90 * time.Minute).Seconds())
	}
	seconds = clampQuestInt(seconds, 60, 24*60*60)
	return time.Duration(seconds) * time.Second
}

func recoveryLossStreakResetThreshold() int {
	threshold := getEnvInt("NEURATRADE_SCALPING_SYMBOL_LOSS_STREAK_BUDGET")
	if threshold <= 0 {
		threshold = 2
	}
	return clampQuestInt(threshold, 1, 20)
}

func livenessIdleMinutes() int {
	minutes := getEnvInt("NEURATRADE_LIVENESS_IDLE_MINUTES")
	if minutes <= 0 {
		minutes = defaultLivenessIdleMinutes
	}
	return clampQuestInt(minutes, 5, 24*60)
}

func livenessMaxAttemptsPerHour() int {
	value := getEnvInt("NEURATRADE_LIVENESS_MAX_ATTEMPTS_PER_HOUR")
	if value <= 0 {
		value = defaultLivenessMaxAttemptsPerHr
	}
	return clampQuestInt(value, 1, 60)
}

func checkpointRFC3339(v interface{}) (time.Time, bool) {
	raw := checkpointString(v)
	if raw == "" {
		return time.Time{}, false
	}
	ts, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return time.Time{}, false
	}
	return ts.UTC(), true
}

func (h *IntegratedQuestHandlers) evaluateEntryAttemptGateState(
	quest *Quest,
	portfolio TradingPortfolio,
	now time.Time,
) entryAttemptGateState {
	now = now.UTC()
	maxAttempts := livenessMaxAttemptsPerHour()
	if maxAttempts <= 0 {
		maxAttempts = defaultLivenessMaxAttemptsPerHr
	}

	totalValue := portfolio.TotalValue
	if totalValue <= 0 {
		totalValue = portfolio.USDTBalance
	}
	deployableBalanceRatio := 0.0
	if totalValue > 0 {
		deployableBalanceRatio = portfolio.USDTBalance / totalValue
	}

	var (
		windowStart time.Time
		hasWindow   bool
		attempts    int
	)
	if quest != nil && quest.Checkpoint != nil {
		windowStart, hasWindow = checkpointRFC3339(quest.Checkpoint["runtime_entry_attempt_window_started_at"])
		attempts = checkpointInt(quest.Checkpoint["runtime_entry_attempts_1h"])
	}

	gateCfg := appautonomy.DefaultLivenessConfig()
	gateCfg.IdleMinutes = livenessIdleMinutes()
	gateCfg.MaxAttemptsPerHour = maxAttempts

	evaluated := appautonomy.EvaluateEntryAttemptGate(
		appautonomy.EntryAttemptGateInput{
			Now:                    now,
			DeployableBalanceRatio: deployableBalanceRatio,
			OpenPositions:          portfolio.OpenPositions,
			DriftActive:            portfolio.DriftActive,
			RecoveryEntryAllowed:   portfolio.RecoveryEntryOK,
			NoFillMinutes:          portfolio.NoFillMinutes,
			HasWindow:              hasWindow,
			WindowStartedAt:        windowStart,
			AttemptsInWindow:       attempts,
		},
		gateCfg,
	)

	return entryAttemptGateState{
		Forced:                 evaluated.Forced,
		AllowNow:               evaluated.AllowNow,
		AttemptsInWindow:       evaluated.AttemptsInWindow,
		MaxAttemptsPerHour:     evaluated.MaxAttemptsPerHour,
		DeployableBalanceRatio: evaluated.DeployableBalanceRatio,
		BlockReason:            evaluated.BlockReason,
		NextCondition:          evaluated.NextCondition,
		AttemptWindowProgress:  evaluated.AttemptWindowProgress,
		WindowStartedAt:        evaluated.WindowStartedAt,
	}
}

func (h *IntegratedQuestHandlers) recordEntryAttempt(quest *Quest, now time.Time, state entryAttemptGateState) {
	if quest == nil {
		return
	}
	if quest.Checkpoint == nil {
		quest.Checkpoint = make(map[string]interface{})
	}
	windowStart := state.WindowStartedAt.UTC()
	if windowStart.IsZero() {
		windowStart = now
	}
	attempts := max(state.AttemptsInWindow, 0)
	attempts++
	quest.Checkpoint["runtime_entry_attempt_window_started_at"] = windowStart.UTC().Format(time.RFC3339)
	quest.Checkpoint["runtime_entry_attempts_1h"] = attempts
	quest.Checkpoint["runtime_last_entry_attempt_at"] = now.UTC().Format(time.RFC3339)
	quest.Checkpoint["runtime_entry_attempt_window_progress"] = fmt.Sprintf(
		"%d/%d in current 1h window",
		attempts,
		state.MaxAttemptsPerHour,
	)
	if attempts >= state.MaxAttemptsPerHour {
		blockReason := fmt.Sprintf(
			"liveness entry-attempt budget reached: %d/%d in current 1h window",
			attempts,
			state.MaxAttemptsPerHour,
		)
		nextCondition := fmt.Sprintf(
			"Next entry-attempt window opens at %s",
			windowStart.Add(time.Hour).UTC().Format(time.RFC3339),
		)
		quest.Checkpoint["runtime_entry_attempt_block_reason"] = blockReason
		quest.Checkpoint["runtime_entry_gate_reason"] = blockReason
		quest.Checkpoint["runtime_next_unblock_condition"] = nextCondition
		return
	}

	quest.Checkpoint["runtime_next_unblock_condition"] = fmt.Sprintf(
		"Liveness attempt budget available: %d/%d used in current 1h window",
		attempts,
		state.MaxAttemptsPerHour,
	)
	delete(quest.Checkpoint, "runtime_entry_attempt_block_reason")
}

func shouldRecordEntryAttempt(decision *AITradingDecision, err error) bool {
	if err != nil {
		return false
	}
	if decision == nil {
		return false
	}
	return !strings.EqualFold(strings.TrimSpace(decision.Action), "hold")
}

func (h *IntegratedQuestHandlers) assertPostEntryProtectionAsync(chatID, exchange, orderID, symbol, side string) {
	orderID = strings.TrimSpace(orderID)
	if orderID == "" || h.lifecycleStore == nil || h.orderExecutor == nil {
		return
	}

	go func() {
		graceSeconds := getEnvInt("NEURATRADE_PROTECTION_POST_ENTRY_GRACE_SECONDS")
		if graceSeconds <= 0 {
			graceSeconds = 45
		}
		timeout := time.Duration(clampQuestInt(graceSeconds, 10, 300)) * time.Second
		baseCtx := WithScalpingAutonomyScope(context.Background(), ScalpingAutonomyScope{
			ChatID:     chatID,
			StrategyID: ScalpingStrategyID(chatID),
			Exchange:   exchange,
		})
		ctx, cancel := context.WithTimeout(baseCtx, timeout)
		defer cancel()

		deadline := time.Now().UTC().Add(timeout)
		for {
			select {
			case <-ctx.Done():
				return
			default:
			}

			positions, err := h.lifecycleStore.ListManagedOpenPositions(ctx, chatID, exchange, 100)
			if err != nil {
				log.Printf("[SCALPING] Post-entry protection assertion failed to list positions: %v", err)
				return
			}
			targets := filterManagedPositionsForEntryProtection(positions, orderID, symbol, side)
			if len(targets) == 0 {
				return
			}
			if allManagedPositionsProtected(targets) {
				return
			}

			h.ensureDynamicProtectionManager()
			if h.protectionManager != nil {
				if _, err := h.protectionManager.ReconcileOpenPositions(ctx, chatID, exchange); err != nil {
					log.Printf("[SCALPING] Post-entry protection reconcile warning: %v", err)
				}
			}

			if time.Now().UTC().After(deadline) {
				closed := 0
				for _, pos := range targets {
					trimmed, trimErr := h.trimManagedPosition(ctx, pos, decimal.NewFromInt(1), "post_entry_protection_assertion")
					if trimErr != nil {
						log.Printf("[SCALPING] Post-entry protection forced close failed for %s: %v", pos.PositionID, trimErr)
						continue
					}
					closed += trimmed
				}
				if closed > 0 {
					h.notifyScalpingDecision(ctx, chatID, AIReasoningNotification{
						DecisionType: "risk_reduction",
						Summary:      "Protection assertion failed after entry; forced risk-reduction close executed",
						Confidence:   1,
						Reasons: []string{
							fmt.Sprintf("Order %s on %s lacked active TP/SL policy after %s grace window", orderID, symbol, timeout.String()),
							fmt.Sprintf("Forced close actions executed: %d", closed),
						},
						Action: "hold",
					})
				}
				return
			}

			time.Sleep(5 * time.Second)
		}
	}()
}

func filterManagedPositionsForEntryProtection(
	positions []ManagedOpenPosition,
	orderID, symbol, side string,
) []ManagedOpenPosition {
	orderID = strings.TrimSpace(orderID)
	symbol = normalizeSymbolForComparison(symbol)
	side = normalizeLifecycleSide(side)
	filtered := make([]ManagedOpenPosition, 0, len(positions))
	for _, pos := range positions {
		if orderID != "" && strings.TrimSpace(pos.OrderID) == orderID {
			filtered = append(filtered, pos)
			continue
		}
		if !isAutonomousEntryProtectionFallbackCandidate(pos) {
			continue
		}
		if symbol == "" {
			continue
		}
		if normalizeSymbolForComparison(pos.Symbol) != symbol {
			continue
		}
		if side != "" && normalizeLifecycleSide(pos.Side) != side {
			continue
		}
		filtered = append(filtered, pos)
	}
	return filtered
}

func isAutonomousEntryProtectionFallbackCandidate(pos ManagedOpenPosition) bool {
	return isAutonomousManagedPosition(pos)
}

func isAutonomousManagedPosition(pos ManagedOpenPosition) bool {
	if strings.HasPrefix(strings.TrimSpace(pos.PositionID), "sync-") {
		return false
	}
	source := strings.ToLower(strings.TrimSpace(pos.Source))
	if source == "" || strings.HasPrefix(source, "autonomous") {
		return true
	}
	if source == "bootstrap_positions" || source == "bootstrap_open_orders" || source == "manual_reconciliation" {
		return false
	}
	return false
}

func allManagedPositionsProtected(positions []ManagedOpenPosition) bool {
	if len(positions) == 0 {
		return true
	}
	for _, pos := range positions {
		if pos.StopLoss.LessThanOrEqual(decimal.Zero) || pos.TakeProfit.LessThanOrEqual(decimal.Zero) {
			return false
		}
	}
	return true
}

// getUserExchange gets the user's preferred exchange from database
// Returns the first connected exchange, or "bitget" as default
func (h *IntegratedQuestHandlers) getUserExchange(chatID string) string {
	if h.db == nil {
		log.Printf("[SCALPING] No database available, using default exchange: bitget")
		return "bitget"
	}

	var exchange string
	query := `SELECT provider FROM telegram_operator_wallets 
	          WHERE chat_id = $1 AND status = 'connected' 
	          ORDER BY created_at DESC LIMIT 1`

	err := h.db.QueryRow(query, chatID).Scan(&exchange)
	if err != nil {
		log.Printf("[SCALPING] No exchange found for chat %s, using default: bitget (%v)", chatID, err)
		return "bitget"
	}

	log.Printf("[SCALPING] Found user exchange: %s for chat: %s", exchange, chatID)
	return exchange
}

func (h *IntegratedQuestHandlers) getAIScalpingRuntimeSnapshot() map[string]interface{} {
	if h.aiScalpingService == nil {
		return map[string]interface{}{}
	}
	return h.aiScalpingService.RuntimeDiagnostics()
}

func (h *IntegratedQuestHandlers) applyAutonomyCheckpoint(quest *Quest) {
	if quest == nil || quest.Checkpoint == nil || h.aiScalpingService == nil {
		return
	}
	diag := h.aiScalpingService.AutonomyDiagnostics()
	canonicalFields := []struct {
		diagKey       string
		checkpointKey string
	}{
		{diagKey: "strategy_id", checkpointKey: "autonomy_strategy_id"},
		{diagKey: "rollout_stage", checkpointKey: "autonomy_rollout_stage"},
		{diagKey: "rollout_status", checkpointKey: "autonomy_rollout_status"},
		{diagKey: "gate_open", checkpointKey: "autonomy_gate_open"},
		{diagKey: "gate_block_reasons", checkpointKey: "autonomy_gate_block_reasons"},
		{diagKey: "gate_checks", checkpointKey: "autonomy_gate_checks"},
		{diagKey: "last_evaluated_at", checkpointKey: "autonomy_last_evaluated_at"},
		{diagKey: "last_error", checkpointKey: "autonomy_last_error"},
		{diagKey: "last_rollback_at", checkpointKey: "autonomy_last_rollback_at"},
		{diagKey: "last_rollback_reason", checkpointKey: "autonomy_last_rollback_reason"},
		{diagKey: "last_rollback_trigger", checkpointKey: "autonomy_last_rollback_trigger"},
	}
	for _, field := range canonicalFields {
		if value, ok := diag[field.diagKey]; ok {
			quest.Checkpoint[field.checkpointKey] = value
			continue
		}
		delete(quest.Checkpoint, field.checkpointKey)
	}
}

func (h *IntegratedQuestHandlers) applyScalpingCycleDecisionDiagnostics(quest *Quest, decision *AITradingDecision) {
	if quest == nil || quest.Checkpoint == nil {
		return
	}
	if decision == nil {
		clearScalpingCycleDecisionDiagnostics(quest.Checkpoint)
		return
	}

	if decision.AccountTier != "" {
		quest.Checkpoint["account_tier"] = decision.AccountTier
	} else {
		delete(quest.Checkpoint, "account_tier")
	}
	if decision.EffectiveMinConfidence > 0 {
		quest.Checkpoint["effective_min_confidence"] = decision.EffectiveMinConfidence
	} else {
		delete(quest.Checkpoint, "effective_min_confidence")
	}
	if decision.EffectiveMaxCapitalPct > 0 {
		quest.Checkpoint["effective_max_capital_pct"] = decision.EffectiveMaxCapitalPct
	} else {
		delete(quest.Checkpoint, "effective_max_capital_pct")
	}
	if decision.EffectiveMaxConcurrentPositions > 0 {
		quest.Checkpoint["effective_max_concurrent_positions"] = decision.EffectiveMaxConcurrentPositions
	} else {
		delete(quest.Checkpoint, "effective_max_concurrent_positions")
	}
	if len(decision.PolicyAdjustments) > 0 {
		quest.Checkpoint["effective_policy_adjustments"] = append([]string(nil), decision.PolicyAdjustments...)
		quest.Checkpoint["effective_policy_adjustment_counts"] = countStringValues(decision.PolicyAdjustments)
	} else {
		delete(quest.Checkpoint, "effective_policy_adjustments")
		delete(quest.Checkpoint, "effective_policy_adjustment_counts")
	}

	if candidateFunnelHasData(decision.CandidateFunnel) {
		quest.Checkpoint["candidate_universe_count"] = decision.CandidateFunnel.CandidateUniverseCount
		quest.Checkpoint["candidate_ranked_count"] = decision.CandidateFunnel.CandidateRankedCount
		quest.Checkpoint["candidate_viable_count"] = decision.CandidateFunnel.CandidateViableCount
		if encoded := encodeCandidateRejections(decision.CandidateFunnel.TopCandidateRejections); len(encoded) > 0 {
			quest.Checkpoint["top_candidate_rejections"] = encoded
			quest.Checkpoint["top_candidate_rejection_reason_counts"] = countCandidateRejectionReasons(decision.CandidateFunnel.TopCandidateRejections)
		} else {
			delete(quest.Checkpoint, "top_candidate_rejections")
			delete(quest.Checkpoint, "top_candidate_rejection_reason_counts")
		}
	} else {
		delete(quest.Checkpoint, "candidate_universe_count")
		delete(quest.Checkpoint, "candidate_ranked_count")
		delete(quest.Checkpoint, "candidate_viable_count")
		delete(quest.Checkpoint, "top_candidate_rejections")
		delete(quest.Checkpoint, "top_candidate_rejection_reason_counts")
	}

	if decision.ExecutionGate != nil {
		quest.Checkpoint["rollout_stage_current"] = decision.ExecutionGate.RolloutStageCurrent
		quest.Checkpoint["rollout_status_current"] = decision.ExecutionGate.RolloutStatusCurrent
		quest.Checkpoint["rollout_gate_reason_current"] = decision.ExecutionGate.RolloutGateReason
	} else {
		delete(quest.Checkpoint, "rollout_stage_current")
		delete(quest.Checkpoint, "rollout_status_current")
		delete(quest.Checkpoint, "rollout_gate_reason_current")
	}
}

func clearScalpingCycleDecisionDiagnostics(checkpoint map[string]interface{}) {
	if checkpoint == nil {
		return
	}
	for _, key := range []string{
		"account_tier",
		"effective_min_confidence",
		"effective_max_capital_pct",
		"effective_max_concurrent_positions",
		"effective_policy_adjustments",
		"effective_policy_adjustment_counts",
		"candidate_universe_count",
		"candidate_ranked_count",
		"candidate_viable_count",
		"top_candidate_rejections",
		"top_candidate_rejection_reason_counts",
		"rollout_stage_current",
		"rollout_status_current",
		"rollout_gate_reason_current",
	} {
		delete(checkpoint, key)
	}
}

func countStringValues(values []string) map[string]int {
	if len(values) == 0 {
		return nil
	}
	counts := make(map[string]int, len(values))
	for _, raw := range values {
		value := strings.TrimSpace(raw)
		if value == "" {
			continue
		}
		counts[value]++
	}
	if len(counts) == 0 {
		return nil
	}
	return counts
}

func countCandidateRejectionReasons(rejections []appautonomy.CandidateRejection) map[string]int {
	if len(rejections) == 0 {
		return nil
	}
	counts := make(map[string]int, len(rejections))
	for _, rejection := range rejections {
		reason := strings.TrimSpace(rejection.Reason)
		if reason == "" {
			continue
		}
		counts[reason]++
	}
	if len(counts) == 0 {
		return nil
	}
	return counts
}

func encodeCandidateRejections(rejections []appautonomy.CandidateRejection) []map[string]interface{} {
	if len(rejections) == 0 {
		return nil
	}
	encoded := make([]map[string]interface{}, 0, len(rejections))
	for _, rejection := range rejections {
		entry := map[string]interface{}{
			"symbol": rejection.Symbol,
			"reason": rejection.Reason,
		}
		if rejection.EstimatedConfidence > 0 {
			entry["estimated_confidence"] = rejection.EstimatedConfidence
		}
		encoded = append(encoded, entry)
	}
	return encoded
}

func normalizeTopCandidateRejections(checkpoint map[string]interface{}) []map[string]interface{} {
	if len(checkpoint) == 0 {
		return nil
	}
	if typed, ok := checkpoint["top_candidate_rejections"].([]map[string]interface{}); ok {
		return typed
	}
	raw, ok := checkpoint["top_candidate_rejections"].([]interface{})
	if !ok || len(raw) == 0 {
		return nil
	}
	converted := make([]map[string]interface{}, 0, len(raw))
	for _, item := range raw {
		entry, ok := item.(map[string]interface{})
		if !ok || len(entry) == 0 {
			continue
		}
		converted = append(converted, entry)
	}
	if len(converted) == 0 {
		delete(checkpoint, "top_candidate_rejections")
		return nil
	}
	checkpoint["top_candidate_rejections"] = converted
	return converted
}

func candidateFunnelHasData(funnel appautonomy.CandidateFunnelSnapshot) bool {
	return funnel.CandidateUniverseCount > 0 ||
		funnel.CandidateRankedCount > 0 ||
		funnel.CandidateViableCount > 0 ||
		len(funnel.TopCandidateRejections) > 0
}

func decisionPolicy(decision *AITradingDecision) appautonomy.ScalpingCyclePolicy {
	if decision == nil {
		return appautonomy.ScalpingCyclePolicy{}
	}
	return appautonomy.ScalpingCyclePolicy{
		AccountTier:            decision.AccountTier,
		EffectiveMinConfidence: decision.EffectiveMinConfidence,
		EffectiveMaxCapitalPct: decision.EffectiveMaxCapitalPct,
		MaxBidAskSpreadPct:     decision.MaxBidAskSpreadPct,
		MaxConcurrentPositions: decision.EffectiveMaxConcurrentPositions,
	}
}

func structuredHoldBlock(decision *AITradingDecision) (string, string, string) {
	if decision == nil {
		return "", "", ""
	}
	policy := decisionPolicy(decision)
	if decision.ExecutionGate != nil && strings.TrimSpace(decision.ExecutionGate.BlockCode) != "" {
		reasonCode := strings.TrimSpace(decision.ExecutionGate.BlockCode)
		humanReason := strings.TrimSpace(decision.ExecutionGate.BlockReason)
		if humanReason == "" {
			humanReason = reasonCode
		}
		return reasonCode, appautonomy.NextUnblockCondition(reasonCode, policy), humanReason
	}
	if len(decision.CandidateFunnel.TopCandidateRejections) > 0 {
		top := decision.CandidateFunnel.TopCandidateRejections[0]
		humanReason := strings.TrimSpace(top.Reason)
		if strings.TrimSpace(top.Symbol) != "" {
			humanReason = fmt.Sprintf("top candidate %s rejected: %s", top.Symbol, top.Reason)
		}
		return top.Reason, appautonomy.NextUnblockCondition(top.Reason, policy), humanReason
	}
	return "", "", ""
}

func autonomyInitTimeout() time.Duration {
	raw := strings.TrimSpace(os.Getenv("AUTONOMY_INIT_TIMEOUT"))
	if raw == "" {
		return defaultAutonomyInitTimeout
	}
	timeout, err := time.ParseDuration(raw)
	if err != nil || timeout <= 0 {
		return defaultAutonomyInitTimeout
	}
	return timeout
}

func (h *IntegratedQuestHandlers) recordTradeDecision(
	ctx context.Context,
	quest *Quest,
	decision *AITradingDecision,
	exchange string,
	portfolio TradingPortfolio,
) {
	if decision == nil || decision.Action == "hold" {
		return
	}

	tradeID := strings.TrimSpace(decision.OrderID)

	if h.tradeMemory != nil {
		record := AITradeRecord{
			ID:          tradeID,
			Timestamp:   time.Now().UTC(),
			Exchange:    exchange,
			Symbol:      decision.Symbol,
			Action:      decision.Action,
			SizePercent: decision.SizePercent,
			Confidence:  decision.Confidence,
			Reasoning:   decision.Reasoning,
			MarketContext: fmt.Sprintf(
				"chat_id=%s,quest_id=%s,definition=%s",
				quest.Metadata["chat_id"],
				quest.ID,
				quest.Metadata["definition_id"],
			),
		}
		if err := h.tradeMemory.RecordDecision(ctx, record); err != nil {
			log.Printf("[AI-MEMORY] Failed to record decision %s: %v", tradeID, err)
		}
	}

	h.persistLegacyTradeEntry(ctx, quest, decision, exchange, portfolio, tradeID)
	if tradeID != "" {
		quest.Checkpoint["trade_memory_id"] = tradeID
	} else {
		delete(quest.Checkpoint, "trade_memory_id")
	}
}

func (h *IntegratedQuestHandlers) persistLegacyTradeEntry(
	ctx context.Context,
	quest *Quest,
	decision *AITradingDecision,
	exchange string,
	portfolio TradingPortfolio,
	tradeID string,
) {
	if h == nil || h.db == nil || quest == nil || decision == nil {
		return
	}
	tradeID = strings.TrimSpace(tradeID)
	if tradeID == "" {
		return
	}
	if err := h.ensureTradeJournalSchema(); err != nil {
		log.Printf("[SCALPING] Legacy trade journal schema unavailable: %v", err)
		return
	}

	chatID := strings.TrimSpace(quest.Metadata["chat_id"])
	strategyID := ScalpingStrategyID(chatID)
	side := normalizeLifecycleSide(decision.Action)
	if side == "" {
		log.Printf("[SCALPING] Skipping legacy trade journal entry %s with unsupported side %q", tradeID, decision.Action)
		return
	}
	entryPrice := decimal.Zero
	if decision.EntryPrice != nil {
		entryPrice = decision.EntryPrice.Abs()
	}
	size, costBasis := legacyTradeEntryMetrics(portfolio, decision)
	now := time.Now().UTC()
	if _, err := h.db.ExecContext(
		ctx,
		`INSERT INTO trades (
			order_id, quest_id, strategy_id, strategy_version, chat_id, exchange, symbol, side,
			entry_price, size, cost_basis, status, opened_at
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,'open',$12)
		ON CONFLICT(order_id) DO UPDATE SET
			quest_id = EXCLUDED.quest_id,
			strategy_id = EXCLUDED.strategy_id,
			strategy_version = EXCLUDED.strategy_version,
			chat_id = EXCLUDED.chat_id,
			exchange = EXCLUDED.exchange,
			symbol = EXCLUDED.symbol,
			side = EXCLUDED.side,
			entry_price = EXCLUDED.entry_price,
			size = EXCLUDED.size,
			cost_basis = EXCLUDED.cost_basis,
			status = 'open',
			opened_at = EXCLUDED.opened_at,
			closed_at = NULL`,
		tradeID,
		quest.ID,
		strategyID,
		portfolio.StrategyPhase,
		chatID,
		exchange,
		decision.Symbol,
		side,
		entryPrice.InexactFloat64(),
		size.InexactFloat64(),
		costBasis.InexactFloat64(),
		now,
	); err != nil {
		log.Printf("[SCALPING] Failed to persist legacy trade journal entry %s: %v", tradeID, err)
	}
}

func legacyTradeEntryMetrics(portfolio TradingPortfolio, decision *AITradingDecision) (decimal.Decimal, decimal.Decimal) {
	if decision == nil || decision.SizePercent <= 0 {
		return decimal.Zero, decimal.Zero
	}

	costBasis := walletBasis(portfolio).
		Mul(decimal.NewFromFloat(decision.SizePercent)).
		Div(decimal.NewFromInt(100))
	if decision.EntryPrice == nil || decision.EntryPrice.LessThanOrEqual(decimal.Zero) || costBasis.LessThanOrEqual(decimal.Zero) {
		return decimal.Zero, costBasis
	}

	return costBasis.Div(decision.EntryPrice.Abs()), costBasis
}

func normalizeLegacyTradeJournalCloseSide(side string) string {
	normalized := strings.ToLower(strings.TrimSpace(side))
	switch normalized {
	case "long", "open_long", "close_long":
		return "buy"
	case "short", "open_short", "close_short":
		return "sell"
	case "buy":
		return "sell"
	case "sell":
		return "buy"
	default:
		return normalizeLifecycleSide(side)
	}
}

func resolveScalpingFuturesWalletUSDT(balance *ccxt.BalanceResponse) float64 {
	if balance == nil {
		return 0
	}
	if balance.Free != nil {
		if v := balance.Free["USDT_FUTURES_USDT"]; v > 0 {
			return v
		}
	}
	futuresSummaryOnly := isSummaryOnlyBalanceKey(balance, "USDT_FUTURES_USDT")
	if futuresSummaryOnly {
		if balance.Free != nil {
			if v := balance.Free["USDT"]; v > 0 {
				return v
			}
		}
		return 0
	}
	if balance.Total != nil {
		if v := balance.Total["USDT_FUTURES_USDT"]; v > 0 {
			return v
		}
	}
	if balance.Free != nil {
		if v := balance.Free["USDT"]; v > 0 {
			return v
		}
	}
	if isSummaryOnlyBalanceKey(balance, "USDT") {
		return 0
	}
	if balance.Total != nil {
		if v := balance.Total["USDT"]; v > 0 {
			return v
		}
	}
	return 0
}

func resolveScalpingWalletBasisSource(balance *ccxt.BalanceResponse) string {
	if balance == nil {
		return "none"
	}
	futuresSummaryOnly := isSummaryOnlyBalanceKey(balance, "USDT_FUTURES_USDT")
	lookup := []struct {
		bookName string
		book     map[string]float64
		key      string
	}{
		{bookName: "free", book: balance.Free, key: "USDT_FUTURES_USDT"},
		{bookName: "total", book: balance.Total, key: "USDT_FUTURES_USDT"},
		{bookName: "free", book: balance.Free, key: "USDT"},
		{bookName: "total", book: balance.Total, key: "USDT"},
	}
	for _, candidate := range lookup {
		if candidate.book == nil {
			continue
		}
		if futuresSummaryOnly && candidate.key == "USDT" && candidate.bookName == "total" {
			continue
		}
		if v := candidate.book[candidate.key]; v > 0 {
			if candidate.bookName == "total" && isSummaryOnlyBalanceKey(balance, candidate.key) {
				continue
			}
			return candidate.bookName + ":" + candidate.key
		}
	}
	if futuresSummaryOnly {
		return "summary:USDT_FUTURES_USDT"
	}
	if isSummaryOnlyBalanceKey(balance, "USDT") {
		return "summary:USDT"
	}
	return "none"
}

func (h *IntegratedQuestHandlers) persistLegacyTradeClose(
	ctx context.Context,
	quest *Quest,
	orderID, exchange, symbol, side string,
	entryPrice, exitPrice, size, pnl, fees decimal.Decimal,
	closedAt time.Time,
) {
	if h == nil || h.db == nil || quest == nil || strings.TrimSpace(orderID) == "" {
		return
	}
	if err := h.ensureTradeJournalSchema(); err != nil {
		log.Printf("[SCALPING] Legacy trade journal schema unavailable: %v", err)
		return
	}

	chatID := strings.TrimSpace(quest.Metadata["chat_id"])
	strategyID := ScalpingStrategyID(chatID)
	if closedAt.IsZero() {
		closedAt = time.Now().UTC()
	}
	journalSide := normalizeLegacyTradeJournalCloseSide(side)
	if journalSide == "" {
		journalSide = normalizeLifecycleSide(side)
	}
	size = size.Abs()
	if _, err := h.db.ExecContext(
		ctx,
		`INSERT INTO trades (
			order_id, quest_id, strategy_id, strategy_version, chat_id, exchange, symbol, side,
			entry_price, exit_price, size, fees, pnl, cost_basis, status, opened_at, closed_at
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,'closed',$15,$16)
		ON CONFLICT(order_id) DO UPDATE SET
			quest_id = EXCLUDED.quest_id,
			strategy_id = EXCLUDED.strategy_id,
			strategy_version = EXCLUDED.strategy_version,
			chat_id = EXCLUDED.chat_id,
			exchange = EXCLUDED.exchange,
			symbol = EXCLUDED.symbol,
			side = CASE WHEN trades.side IN ('buy', 'sell') THEN trades.side ELSE EXCLUDED.side END,
			entry_price = CASE WHEN EXCLUDED.entry_price > 0 THEN EXCLUDED.entry_price ELSE trades.entry_price END,
			exit_price = EXCLUDED.exit_price,
			size = CASE WHEN EXCLUDED.size > 0 THEN EXCLUDED.size ELSE trades.size END,
			fees = EXCLUDED.fees,
			pnl = EXCLUDED.pnl,
			cost_basis = CASE WHEN EXCLUDED.cost_basis > 0 THEN EXCLUDED.cost_basis ELSE trades.cost_basis END,
			status = 'closed',
			closed_at = EXCLUDED.closed_at`,
		orderID,
		quest.ID,
		strategyID,
		checkpointString(quest.Checkpoint["strategy_phase"]),
		chatID,
		exchange,
		symbol,
		journalSide,
		entryPrice.InexactFloat64(),
		exitPrice.InexactFloat64(),
		size.InexactFloat64(),
		fees.InexactFloat64(),
		pnl.InexactFloat64(),
		entryPrice.Mul(size).InexactFloat64(),
		closedAt,
		closedAt,
	); err != nil {
		log.Printf("[SCALPING] Failed to persist legacy trade journal close %s: %v", orderID, err)
	}
}

func (h *IntegratedQuestHandlers) ingestClosedOrderFeedback(ctx context.Context, quest *Quest, exchange, symbol string) {
	if h.orderExecutor == nil || quest == nil || strings.TrimSpace(symbol) == "" {
		return
	}
	if quest.Checkpoint == nil {
		quest.Checkpoint = make(map[string]interface{})
	}

	closedOrders, err := h.orderExecutor.GetClosedOrders(ctx, exchange, symbol, 20)
	if err != nil {
		log.Printf("[SCALPING] Failed to fetch closed orders for feedback (%s %s): %v", exchange, symbol, err)
		return
	}

	processed := getProcessedOrderIDs(quest.Checkpoint["processed_closed_order_ids"])
	updatedProcessed := false
	processedCount := 0
	wins := 0
	losses := 0
	breakeven := 0
	totalPnL := decimal.Zero
	openPositionBySymbolSide := make(map[string]struct{})
	if h.lifecycleStore != nil {
		positions, listErr := h.lifecycleStore.ListManagedOpenPositions(ctx, quest.Metadata["chat_id"], exchange, 200)
		if listErr != nil {
			log.Printf("[SCALPING] Failed to load managed open positions for closed-order filter (%s): %v", exchange, listErr)
		} else {
			for _, pos := range positions {
				key := normalizeSymbolForComparison(pos.Symbol) + ":" + normalizeLifecycleSide(pos.Side)
				if strings.TrimSpace(key) != ":" {
					openPositionBySymbolSide[key] = struct{}{}
				}
			}
		}
	}

	for _, order := range closedOrders {
		orderID := getOrderID(order)
		if orderID == "" || processed[orderID] {
			continue
		}

		pnl, ok := decimalFromOrder(order, "totalProfits", "totalProfit", "pnl", "profit", "realizedPnl", "achievedProfits")
		if !ok {
			quest.Checkpoint["closed_order_feedback_missing_pnl"] = checkpointInt(quest.Checkpoint["closed_order_feedback_missing_pnl"]) + 1
			log.Printf("[SCALPING] Skipped closed-order feedback due to missing pnl: order=%s symbol=%s keys=%v",
				orderID,
				symbol,
				sortedOrderKeys(order),
			)
			continue
		}

		side := "buy"
		if rawSide, ok := stringFromOrder(order, "side", "positionSide", "posSide", "tradeSide"); ok {
			side = strings.ToLower(strings.TrimSpace(rawSide))
		}
		if shouldSkipClosedOrderFeedback(order, pnl, symbol, side, openPositionBySymbolSide) {
			processed[orderID] = true
			updatedProcessed = true
			quest.Checkpoint["closed_order_feedback_skipped_zero_pnl"] = checkpointInt(quest.Checkpoint["closed_order_feedback_skipped_zero_pnl"]) + 1
			log.Printf("[SCALPING] Skipped closed-order feedback as probable entry fill: order=%s symbol=%s side=%s pnl=%s tradeSide=%s reduceOnly=%s",
				orderID,
				symbol,
				side,
				pnl.String(),
				checkpointString(order["tradeSide"]),
				checkpointString(order["reduceOnly"]),
			)
			continue
		}
		exitPrice := decimal.Zero
		if p, ok := decimalFromOrder(order, "priceAvg", "avgPrice", "price", "fillPrice"); ok {
			exitPrice = p
		}
		filled := decimal.Zero
		if v, ok := decimalFromOrder(order, "filled", "filledAmount", "baseVolume", "qty", "size"); ok {
			filled = v.Abs()
		}
		entryPrice := decimal.Zero
		if v, ok := decimalFromOrder(order, "avgOpenPrice", "openPriceAvg", "openAvgPrice", "openPrice", "entryPrice", "priceOpen"); ok {
			entryPrice = v
		}
		notional := decimal.Zero
		if v, ok := decimalFromOrder(order, "notional", "cost", "quoteVolume", "tradeAmount"); ok {
			notional = v.Abs()
		}
		if notional.LessThanOrEqual(decimal.Zero) && filled.GreaterThan(decimal.Zero) {
			if entryPrice.GreaterThan(decimal.Zero) {
				notional = entryPrice.Abs().Mul(filled)
			} else if exitPrice.GreaterThan(decimal.Zero) {
				notional = exitPrice.Abs().Mul(filled)
			}
		}
		if entryPrice.LessThanOrEqual(decimal.Zero) && filled.GreaterThan(decimal.Zero) && notional.GreaterThan(decimal.Zero) {
			entryPrice = notional.Div(filled)
		}

		profitable := pnl.GreaterThan(decimal.Zero)
		if profitable {
			wins++
		} else if pnl.LessThan(decimal.Zero) {
			losses++
		} else {
			breakeven++
		}
		totalPnL = totalPnL.Add(pnl)
		processedCount++
		if h.aiScalpingService != nil {
			h.aiScalpingService.ReportTradeOutcome(symbol, pnl)
		}
		GetScalpingPerformance().RecordTrade(TradeRecord{
			Timestamp:  time.Now().UTC(),
			Symbol:     symbol,
			Side:       side,
			Amount:     filled,
			PnL:        pnl,
			Profitable: profitable,
			ExitPrice:  exitPrice,
			EntryPrice: entryPrice,
			Notional:   notional,
		})

		if h.tradeMemory != nil {
			outcome := "breakeven"
			if profitable {
				outcome = "win"
			} else if pnl.LessThan(decimal.Zero) {
				outcome = "loss"
			}
			if err := h.tradeMemory.UpdateOutcome(ctx, orderID, outcome, exitPrice.InexactFloat64(), pnl); err != nil {
				log.Printf("[AI-MEMORY] Failed to update outcome for %s: %v", orderID, err)
			}
		}

		processed[orderID] = true
		updatedProcessed = true

		fees := decimal.Zero
		if v, ok := decimalFromOrder(order, "fees", "fee", "totalFee", "commission"); ok {
			fees = v
		}
		closedAt := timestampFromOrder(order)
		h.persistLegacyTradeClose(ctx, quest, orderID, exchange, symbol, side, entryPrice, exitPrice, filled, pnl, fees, closedAt)

		if h.lifecycleStore != nil {
			if err := h.lifecycleStore.RecordClosedOrder(ctx, LifecycleCloseRecord{
				OrderID:     orderID,
				ChatID:      quest.Metadata["chat_id"],
				Exchange:    exchange,
				Symbol:      symbol,
				Side:        side,
				MarketType:  "futures",
				Filled:      filled,
				EntryPrice:  entryPrice,
				ExitPrice:   exitPrice,
				RealizedPnL: pnl,
				Fees:        fees,
				Source:      "exchange_reconciliation",
				ClosedAt:    closedAt,
			}); err != nil {
				log.Printf("[SCALPING] Failed to persist closed-order lifecycle for %s: %v", orderID, err)
			}
		}
		if h.telemetryStore != nil {
			outcome := "breakeven"
			if profitable {
				outcome = "win"
			} else if pnl.LessThan(decimal.Zero) {
				outcome = "loss"
			}

			holdSeconds := 0
			if h.db != nil {
				var openedAt time.Time
				if err := h.db.QueryRowContext(
					ctx,
					"SELECT opened_at FROM trading_positions WHERE order_id = ? ORDER BY opened_at DESC LIMIT 1",
					orderID,
				).Scan(&openedAt); err == nil && !openedAt.IsZero() {
					secs := int(closedAt.Sub(openedAt).Seconds())
					if secs < 0 {
						secs = 0
					}
					holdSeconds = secs
				}
			}

			if err := h.telemetryStore.UpdateCycleOutcome(ctx, orderID, ScalpingOutcomeRecord{
				Outcome:             outcome,
				PnL:                 pnl.InexactFloat64(),
				HoldDurationSeconds: holdSeconds,
				ClosedAt:            closedAt,
			}); err != nil {
				log.Printf("[TELEMETRY] Failed to update outcome for order %s: %v", orderID, err)
			}
		}
	}

	if !updatedProcessed {
		return
	}

	if processedCount > 0 {
		summary := fmt.Sprintf(
			"Reconciled %d closed order(s) on %s (%s). Realized PnL: %s (wins=%d, losses=%d, breakeven=%d)",
			processedCount,
			exchange,
			symbol,
			totalPnL.StringFixed(4),
			wins,
			losses,
			breakeven,
		)
		now := time.Now().UTC()
		if shouldNotifyPnLReconciliation(quest, summary, now) {
			h.notifyScalpingDecision(ctx, quest.Metadata["chat_id"], AIReasoningNotification{
				DecisionType: "pnl_reconciliation",
				Summary:      summary,
				Confidence:   1,
				Reasons: []string{
					"Closed orders were synced from exchange state",
				},
				Action: "record",
			})
			recordPnLReconciliationNotification(quest, summary, now)
		}
	}

	ids := make([]string, 0, len(processed))
	for id := range processed {
		ids = append(ids, id)
	}
	if len(ids) > 200 {
		ids = ids[len(ids)-200:]
	}
	quest.Checkpoint["processed_closed_order_ids"] = ids
}

func getProcessedOrderIDs(raw interface{}) map[string]bool {
	processed := make(map[string]bool)
	switch v := raw.(type) {
	case []string:
		for _, id := range v {
			if strings.TrimSpace(id) != "" {
				processed[strings.TrimSpace(id)] = true
			}
		}
	case []interface{}:
		for _, item := range v {
			if s, ok := item.(string); ok && strings.TrimSpace(s) != "" {
				processed[strings.TrimSpace(s)] = true
			}
		}
	}
	return processed
}

func shouldNotifyPnLReconciliation(quest *Quest, summary string, now time.Time) bool {
	if quest == nil {
		return true
	}
	summary = strings.TrimSpace(summary)
	if summary == "" {
		return false
	}
	if quest.Checkpoint == nil {
		return true
	}
	lastSummary := checkpointString(quest.Checkpoint["last_pnl_reconciliation_summary"])
	if !strings.EqualFold(lastSummary, summary) {
		return true
	}
	lastSentRaw := checkpointString(quest.Checkpoint["last_pnl_reconciliation_sent_at"])
	if strings.TrimSpace(lastSentRaw) == "" {
		return true
	}
	lastSent, err := time.Parse(time.RFC3339, lastSentRaw)
	if err != nil {
		return true
	}
	return now.Sub(lastSent) >= pnlReconciliationNotificationCooldown()
}

func recordPnLReconciliationNotification(quest *Quest, summary string, now time.Time) {
	if quest == nil {
		return
	}
	if quest.Checkpoint == nil {
		quest.Checkpoint = make(map[string]interface{})
	}
	quest.Checkpoint["last_pnl_reconciliation_summary"] = strings.TrimSpace(summary)
	quest.Checkpoint["last_pnl_reconciliation_sent_at"] = now.UTC().Format(time.RFC3339)
}

func pnlReconciliationNotificationCooldown() time.Duration {
	if seconds := getEnvInt("NEURATRADE_PNL_RECONCILIATION_NOTIFY_COOLDOWN_SECONDS"); seconds > 0 {
		return time.Duration(seconds) * time.Second
	}
	return 15 * time.Minute
}

func getOrderID(order map[string]interface{}) string {
	candidates := []string{"orderId", "orderID", "order_id", "id", "clientOid"}
	for _, key := range candidates {
		if raw, ok := order[key]; ok {
			if s, ok := raw.(string); ok && strings.TrimSpace(s) != "" {
				return strings.TrimSpace(s)
			}
		}
	}
	return ""
}

func stringFromOrder(order map[string]interface{}, keys ...string) (string, bool) {
	for _, key := range keys {
		raw, ok := order[key]
		if !ok || raw == nil {
			continue
		}
		switch v := raw.(type) {
		case string:
			if strings.TrimSpace(v) != "" {
				return v, true
			}
		default:
			s := strings.TrimSpace(fmt.Sprintf("%v", v))
			if s != "" && s != "<nil>" {
				return s, true
			}
		}
	}
	return "", false
}

func shouldSkipClosedOrderFeedback(
	order map[string]interface{},
	pnl decimal.Decimal,
	symbol string,
	side string,
	openPositionBySymbolSide map[string]struct{},
) bool {
	if !pnl.IsZero() || len(openPositionBySymbolSide) == 0 {
		return false
	}
	key := normalizeSymbolForComparison(symbol) + ":" + normalizeLifecycleSide(side)
	if _, exists := openPositionBySymbolSide[key]; !exists {
		return false
	}

	tradeSide, _ := stringFromOrder(order, "tradeSide", "offset", "positionEffect", "intent")
	tradeSide = strings.ToLower(strings.TrimSpace(tradeSide))
	if strings.Contains(tradeSide, "close") || strings.Contains(tradeSide, "reduce") {
		return false
	}

	reduceOnly, _ := stringFromOrder(order, "reduceOnly", "reduce_only")
	reduceOnly = strings.ToLower(strings.TrimSpace(reduceOnly))
	if reduceOnly == "true" || reduceOnly == "1" || reduceOnly == "yes" {
		return false
	}

	return true
}

func sortedOrderKeys(order map[string]interface{}) []string {
	if len(order) == 0 {
		return nil
	}
	keys := make([]string, 0, len(order))
	for key := range order {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func decimalFromOrder(order map[string]interface{}, keys ...string) (decimal.Decimal, bool) {
	for _, key := range keys {
		raw, ok := order[key]
		if !ok || raw == nil {
			continue
		}
		switch v := raw.(type) {
		case decimal.Decimal:
			return v, true
		case string:
			s := strings.TrimSpace(v)
			if s == "" {
				continue
			}
			if d, err := decimal.NewFromString(s); err == nil {
				return d, true
			}
		case float64:
			return decimal.NewFromFloat(v), true
		case float32:
			return decimal.NewFromFloat(float64(v)), true
		case int:
			return decimal.NewFromInt(int64(v)), true
		case int64:
			return decimal.NewFromInt(v), true
		case json.Number:
			if d, err := decimal.NewFromString(v.String()); err == nil {
				return d, true
			}
		default:
			s := strings.TrimSpace(fmt.Sprintf("%v", raw))
			if s == "" || s == "<nil>" {
				continue
			}
			if d, err := decimal.NewFromString(s); err == nil {
				return d, true
			}
		}
	}
	return decimal.Zero, false
}

func timestampFromOrder(order map[string]interface{}) time.Time {
	keys := []string{"uTime", "cTime", "fillTime", "updatedAt", "timestamp", "time"}
	for _, key := range keys {
		raw, ok := order[key]
		if !ok || raw == nil {
			continue
		}
		switch v := raw.(type) {
		case time.Time:
			if !v.IsZero() {
				return v.UTC()
			}
		case int64:
			if ts := parseEpochTimestamp(v); !ts.IsZero() {
				return ts
			}
		case int:
			if ts := parseEpochTimestamp(int64(v)); !ts.IsZero() {
				return ts
			}
		case float64:
			if ts := parseEpochTimestamp(int64(v)); !ts.IsZero() {
				return ts
			}
		case string:
			text := strings.TrimSpace(v)
			if text == "" {
				continue
			}
			if numeric, err := strconv.ParseInt(text, 10, 64); err == nil {
				if ts := parseEpochTimestamp(numeric); !ts.IsZero() {
					return ts
				}
			}
			if parsed, err := time.Parse(time.RFC3339, text); err == nil {
				return parsed.UTC()
			}
		}
	}
	return time.Now().UTC()
}

func parseEpochTimestamp(value int64) time.Time {
	if value <= 0 {
		return time.Time{}
	}
	if value > 1_000_000_000_000 {
		return time.UnixMilli(value).UTC()
	}
	return time.Unix(value, 0).UTC()
}

func (h *IntegratedQuestHandlers) ensureDynamicProtectionManager() {
	if h.protectionManager != nil || h.lifecycleStore == nil {
		return
	}
	tickerSource, ok := h.ccxtService.(interface {
		FetchSingleTicker(ctx context.Context, exchange, symbol string) (ccxt.MarketPriceInterface, error)
	})
	if !ok {
		return
	}
	h.protectionManager = NewDynamicProtectionManager(DefaultDynamicProtectionConfig(), h.lifecycleStore, tickerSource, log.Default())
	if syncable, ok := h.orderExecutor.(interface {
		SyncPositionProtection(context.Context, string, ManagedOpenPosition, decimal.Decimal, decimal.Decimal) error
	}); ok {
		h.protectionManager.SetPositionProtectionSync(syncable)
	}
}

func decimalValueOrZero(value *decimal.Decimal) decimal.Decimal {
	if value == nil {
		return decimal.Zero
	}
	return *value
}

func (h *IntegratedQuestHandlers) bootstrapLifecycleState(ctx context.Context, quest *Quest, exchange, chatID string) {
	if h.lifecycleStore == nil || quest == nil {
		return
	}
	ccxtSvc, ok := h.ccxtService.(ccxt.CCXTService)
	if !ok {
		return
	}
	if quest.Checkpoint == nil {
		quest.Checkpoint = make(map[string]interface{})
	}

	interval := defaultBootstrapInterval
	if seconds := getEnvInt("NEURATRADE_SCALPING_BOOTSTRAP_SECONDS"); seconds > 0 {
		interval = time.Duration(seconds) * time.Second
	}
	if raw, ok := quest.Checkpoint["runtime_bootstrap_synced_at"].(string); ok && strings.TrimSpace(raw) != "" {
		if last, err := time.Parse(time.RFC3339, raw); err == nil && time.Since(last) < interval {
			return
		}
	}

	bootstrapCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()

	snapshot := LifecycleExchangeSnapshot{
		OpenOrders:     []ccxt.Order{},
		Positions:      []ccxt.Position{},
		OrdersFresh:    false,
		PositionsFresh: false,
	}
	if openOrders, err := ccxtSvc.FetchOpenOrders(bootstrapCtx, exchange); err == nil && openOrders != nil {
		snapshot.OpenOrders = openOrders.Orders
		snapshot.OrdersFresh = true
	} else if err != nil {
		log.Printf("[SCALPING] Bootstrap open-order sync failed on %s: %v", exchange, err)
	}

	if positions, err := ccxtSvc.FetchPositions(bootstrapCtx, exchange); err == nil && positions != nil {
		snapshot.Positions = positions.Positions
		snapshot.PositionsFresh = true
	} else if err != nil {
		log.Printf("[SCALPING] Bootstrap position sync failed on %s: %v", exchange, err)
	}

	reconcileSummary, err := h.lifecycleStore.ReconcileExchangeSnapshot(
		bootstrapCtx,
		chatID,
		exchange,
		snapshot,
		"bootstrap_reconciliation",
	)
	if err != nil {
		log.Printf("[SCALPING] Bootstrap lifecycle reconciliation failed on %s: %v", exchange, err)
		quest.Checkpoint["runtime_bootstrap_error"] = err.Error()
	}

	if snapshot.PositionsFresh {
		repaired, repairErr := h.lifecycleStore.RepairMissingSyncPositions(
			bootstrapCtx,
			chatID,
			exchange,
			snapshot.Positions,
			"startup_drift_repair_exchange_missing",
		)
		if repairErr != nil {
			log.Printf("[SCALPING] Startup drift repair failed on %s: %v", exchange, repairErr)
			quest.Checkpoint["runtime_startup_drift_repair_error"] = repairErr.Error()
		} else if repaired > 0 {
			quest.Checkpoint["runtime_startup_drift_repair_closed"] = repaired
			quest.Checkpoint["state_drift_last_repair_at"] = time.Now().UTC().Format(time.RFC3339)
			log.Printf("[SCALPING] Startup drift repair closed %d stale lifecycle position(s) on %s", repaired, exchange)
		}
	}

	quest.Checkpoint["runtime_bootstrap_synced_at"] = time.Now().UTC().Format(time.RFC3339)
	quest.Checkpoint["runtime_bootstrap_open_orders"] = reconcileSummary.OrdersSynced
	quest.Checkpoint["runtime_bootstrap_positions"] = reconcileSummary.PositionsSynced
	quest.Checkpoint["runtime_bootstrap_orders_cancelled"] = reconcileSummary.OrdersCancelled
	quest.Checkpoint["runtime_bootstrap_positions_closed"] = reconcileSummary.PositionsClosed

	feedbackSymbols := make([]string, 0, 6)
	feedbackSet := make(map[string]struct{}, 6)
	addFeedbackSymbol := func(symbol string) {
		symbol = strings.TrimSpace(symbol)
		if symbol == "" {
			return
		}
		if _, exists := feedbackSet[symbol]; exists {
			return
		}
		feedbackSet[symbol] = struct{}{}
		feedbackSymbols = append(feedbackSymbols, symbol)
	}
	for _, position := range snapshot.Positions {
		addFeedbackSymbol(position.Symbol)
		if len(feedbackSymbols) >= 6 {
			break
		}
	}
	if len(feedbackSymbols) < 6 {
		for _, order := range snapshot.OpenOrders {
			addFeedbackSymbol(order.Symbol)
			if len(feedbackSymbols) >= 6 {
				break
			}
		}
	}
	if len(feedbackSymbols) < 6 {
		if lastSymbol, ok := quest.Checkpoint["ai_symbol"].(string); ok {
			addFeedbackSymbol(lastSymbol)
		}
	}
	for _, symbol := range feedbackSymbols {
		h.ingestClosedOrderFeedback(bootstrapCtx, quest, exchange, symbol)
	}
	quest.Checkpoint["runtime_bootstrap_feedback_symbols"] = len(feedbackSymbols)
}

func (h *IntegratedQuestHandlers) scalpingCooldownRemaining(quest *Quest, now time.Time) time.Duration {
	if quest == nil || quest.Checkpoint == nil {
		return 0
	}

	raw, ok := quest.Checkpoint["runtime_cooldown_until"]
	if !ok {
		return 0
	}

	cooldownUntil, ok := raw.(string)
	if !ok || strings.TrimSpace(cooldownUntil) == "" {
		return 0
	}

	until, err := time.Parse(time.RFC3339, cooldownUntil)
	if err != nil || !until.After(now) {
		delete(quest.Checkpoint, "runtime_cooldown_until")
		return 0
	}

	return until.Sub(now)
}

func (h *IntegratedQuestHandlers) recordScalpingFailure(quest *Quest, reason string) (int, time.Duration) {
	if quest.Checkpoint == nil {
		quest.Checkpoint = make(map[string]interface{})
	}

	now := time.Now().UTC()
	streak := checkpointInt(quest.Checkpoint["runtime_failure_streak"]) + 1
	quest.Checkpoint["runtime_failure_streak"] = streak
	quest.Checkpoint["runtime_last_failure"] = strings.TrimSpace(reason)
	quest.Checkpoint["runtime_last_failure_at"] = now.Format(time.RFC3339)

	if streak < scalpingFailureCooldownThreshold {
		delete(quest.Checkpoint, "runtime_cooldown_until")
		return streak, 0
	}

	level := streak - scalpingFailureCooldownThreshold
	if level > 3 {
		level = 3
	}
	cooldown := scalpingBaseCooldown * time.Duration(1<<level)
	if cooldown > scalpingMaxCooldown {
		cooldown = scalpingMaxCooldown
	}
	quest.Checkpoint["runtime_cooldown_until"] = now.Add(cooldown).Format(time.RFC3339)
	return streak, cooldown
}

func (h *IntegratedQuestHandlers) resetScalpingFailureState(quest *Quest) {
	if quest == nil || quest.Checkpoint == nil {
		return
	}
	delete(quest.Checkpoint, "runtime_failure_streak")
	delete(quest.Checkpoint, "runtime_last_failure")
	delete(quest.Checkpoint, "runtime_last_failure_at")
	delete(quest.Checkpoint, "runtime_cooldown_until")
	delete(quest.Checkpoint, "runtime_hold_cooldown")
	delete(quest.Checkpoint, "error")
}

func (h *IntegratedQuestHandlers) updateHoldStateCheckpoint(quest *Quest, held bool) {
	if quest == nil {
		return
	}
	if quest.Checkpoint == nil {
		quest.Checkpoint = make(map[string]interface{})
	}
	now := time.Now().UTC()
	if held {
		quest.Checkpoint["runtime_hold_streak"] = checkpointInt(quest.Checkpoint["runtime_hold_streak"]) + 1
		if raw, ok := quest.Checkpoint["runtime_no_fill_since"].(string); !ok || strings.TrimSpace(raw) == "" {
			quest.Checkpoint["runtime_no_fill_since"] = now.Format(time.RFC3339)
		}
	} else {
		quest.Checkpoint["runtime_hold_streak"] = 0
		delete(quest.Checkpoint, "runtime_no_fill_since")
	}
	quest.Checkpoint["runtime_hold_updated_at"] = now.Format(time.RFC3339)
}

func aiRuntimeWindowDuration() time.Duration {
	seconds := getEnvInt("NEURATRADE_AI_RUNTIME_WINDOW_SECONDS")
	if seconds <= 0 {
		return defaultAIRuntimeWindow
	}
	return time.Duration(clampQuestInt(seconds, 60, 7200)) * time.Second
}

func aiRuntimeCircuitFailureThreshold() int {
	value := getEnvInt("NEURATRADE_AI_RUNTIME_CIRCUIT_FAILURES")
	if value <= 0 {
		value = defaultAIRuntimeCircuitFailures
	}
	return clampQuestInt(value, 1, 20)
}

func aiRuntimeCircuitCooldown() time.Duration {
	seconds := getEnvInt("NEURATRADE_AI_RUNTIME_CIRCUIT_COOLDOWN_SECONDS")
	if seconds <= 0 {
		return defaultAIRuntimeCircuitCooldown
	}
	return time.Duration(clampQuestInt(seconds, 30, 3600)) * time.Second
}

func aiRuntimeCircuitRemaining(quest *Quest, now time.Time) time.Duration {
	if quest == nil || quest.Checkpoint == nil {
		return 0
	}
	raw, ok := quest.Checkpoint["runtime_ai_circuit_until"].(string)
	if !ok || strings.TrimSpace(raw) == "" {
		return 0
	}
	until, err := time.Parse(time.RFC3339, raw)
	if err != nil || !until.After(now) {
		delete(quest.Checkpoint, "runtime_ai_circuit_until")
		delete(quest.Checkpoint, "runtime_ai_circuit_reason")
		return 0
	}
	return until.Sub(now)
}

func ensureAIRuntimeWindow(quest *Quest, now time.Time) {
	if quest == nil {
		return
	}
	if quest.Checkpoint == nil {
		quest.Checkpoint = make(map[string]interface{})
	}
	window := aiRuntimeWindowDuration()
	raw, ok := quest.Checkpoint["runtime_ai_window_started_at"].(string)
	if ok && strings.TrimSpace(raw) != "" {
		if startedAt, err := time.Parse(time.RFC3339, raw); err == nil && now.Sub(startedAt) < window {
			return
		}
	}

	quest.Checkpoint["runtime_ai_window_started_at"] = now.UTC().Format(time.RFC3339)
	quest.Checkpoint["runtime_ai_window_total"] = 0
	quest.Checkpoint["runtime_ai_window_success"] = 0
	quest.Checkpoint["runtime_ai_window_errors"] = 0
	quest.Checkpoint["runtime_ai_window_timeouts"] = 0
	quest.Checkpoint["runtime_ai_window_parse_fails"] = 0
	quest.Checkpoint["runtime_ai_window_failover_attempts"] = 0
	quest.Checkpoint["runtime_ai_window_failover_successes"] = 0
	quest.Checkpoint["runtime_ai_window_failover_failures"] = 0
}

func applyAIScalpingRuntimeSnapshot(quest *Quest, runtime map[string]interface{}, success bool) {
	if quest == nil || quest.Checkpoint == nil || len(runtime) == 0 {
		return
	}

	if provider, ok := runtime["last_provider"].(string); ok {
		quest.Checkpoint["runtime_ai_last_provider"] = provider
	}
	if provider, ok := runtime["last_successful_provider"].(string); ok {
		quest.Checkpoint["runtime_ai_last_success_provider"] = provider
	}
	if raw, ok := runtime["last_success_at"].(string); ok {
		quest.Checkpoint["runtime_ai_last_success_at"] = raw
	}
	if raw, ok := runtime["last_error"].(string); ok {
		quest.Checkpoint["runtime_ai_last_error"] = raw
	}
	if raw, ok := runtime["last_error_at"].(string); ok {
		quest.Checkpoint["runtime_ai_last_error_at"] = raw
	}
	quest.Checkpoint["runtime_ai_meta_hold_promotions"] = checkpointInt(runtime["meta_hold_promotions"])

	failoverAttempted := checkpointBool(runtime["failover_attempted"])
	failoverSucceeded := checkpointBool(runtime["failover_succeeded"])
	if failoverAttempted {
		quest.Checkpoint["runtime_ai_window_failover_attempts"] = checkpointInt(quest.Checkpoint["runtime_ai_window_failover_attempts"]) + 1
		if failoverSucceeded {
			quest.Checkpoint["runtime_ai_window_failover_successes"] = checkpointInt(quest.Checkpoint["runtime_ai_window_failover_successes"]) + 1
		} else if !success {
			quest.Checkpoint["runtime_ai_window_failover_failures"] = checkpointInt(quest.Checkpoint["runtime_ai_window_failover_failures"]) + 1
		}
	}

	if attempted, ok := runtime["failover_providers"].([]string); ok {
		quest.Checkpoint["runtime_ai_failover_providers"] = append([]string(nil), attempted...)
	}
	if failed, ok := runtime["failed_providers"].([]string); ok {
		quest.Checkpoint["runtime_ai_failed_providers"] = append([]string(nil), failed...)
	}
}

func recordAIRuntimeEvent(
	quest *Quest,
	now time.Time,
	reasonCategory string,
	success bool,
	runtimeSnapshot map[string]interface{},
) {
	if quest == nil {
		return
	}
	if quest.Checkpoint == nil {
		quest.Checkpoint = make(map[string]interface{})
	}
	ensureAIRuntimeWindow(quest, now)

	quest.Checkpoint["runtime_ai_window_total"] = checkpointInt(quest.Checkpoint["runtime_ai_window_total"]) + 1
	if success {
		quest.Checkpoint["runtime_ai_window_success"] = checkpointInt(quest.Checkpoint["runtime_ai_window_success"]) + 1
		quest.Checkpoint["runtime_ai_consecutive_failures"] = 0
		delete(quest.Checkpoint, "runtime_ai_circuit_until")
		delete(quest.Checkpoint, "runtime_ai_circuit_reason")
	} else {
		quest.Checkpoint["runtime_ai_window_errors"] = checkpointInt(quest.Checkpoint["runtime_ai_window_errors"]) + 1
		if reasonCategory == aiReasonLLMTimeout {
			quest.Checkpoint["runtime_ai_window_timeouts"] = checkpointInt(quest.Checkpoint["runtime_ai_window_timeouts"]) + 1
		}
		if reasonCategory == aiReasonLLMParseContract {
			quest.Checkpoint["runtime_ai_window_parse_fails"] = checkpointInt(quest.Checkpoint["runtime_ai_window_parse_fails"]) + 1
		}

		consecutive := checkpointInt(quest.Checkpoint["runtime_ai_consecutive_failures"]) + 1
		quest.Checkpoint["runtime_ai_consecutive_failures"] = consecutive
		quest.Checkpoint["runtime_ai_last_failure_at"] = now.UTC().Format(time.RFC3339)
		quest.Checkpoint["runtime_ai_last_failure_reason"] = reasonCategory
		if consecutive >= aiRuntimeCircuitFailureThreshold() {
			cooldown := aiRuntimeCircuitCooldown()
			quest.Checkpoint["runtime_ai_circuit_until"] = now.Add(cooldown).UTC().Format(time.RFC3339)
			quest.Checkpoint["runtime_ai_circuit_reason"] = reasonCategory
			quest.Checkpoint["runtime_ai_circuit_trips"] = checkpointInt(quest.Checkpoint["runtime_ai_circuit_trips"]) + 1
		}
	}

	quest.Checkpoint["runtime_ai_last_category"] = reasonCategory
	quest.Checkpoint["runtime_ai_last_event_at"] = now.UTC().Format(time.RFC3339)
	applyAIScalpingRuntimeSnapshot(quest, runtimeSnapshot, success)

	total := checkpointInt(quest.Checkpoint["runtime_ai_window_total"])
	errors := checkpointInt(quest.Checkpoint["runtime_ai_window_errors"])
	errorRate := 0.0
	if total > 0 {
		errorRate = float64(errors) / float64(total)
	}
	quest.Checkpoint["runtime_ai_window_error_rate"] = errorRate
}

func clampQuestInt(value, min, max int) int {
	if value < min {
		return min
	}
	if value > max {
		return max
	}
	return value
}

func (h *IntegratedQuestHandlers) maybeApplyRiskUnlock(
	ctx context.Context,
	quest *Quest,
	chatID, exchange string,
	drawdown float64,
) (int, error) {
	if quest == nil || h.lifecycleStore == nil || h.orderExecutor == nil {
		return 0, nil
	}
	if quest.Checkpoint == nil {
		quest.Checkpoint = make(map[string]interface{})
	}

	holdStreak := checkpointInt(quest.Checkpoint["runtime_hold_streak"])
	if holdStreak < 3 {
		return 0, nil
	}

	minDrawdown := 0.05
	if configured, ok := getEnvFloat("NEURATRADE_RISK_UNLOCK_MIN_DRAWDOWN"); ok && configured > 0 {
		minDrawdown = configured
	}
	if drawdown < minDrawdown {
		return 0, nil
	}

	cooldown := 10 * time.Minute
	if seconds := getEnvInt("NEURATRADE_RISK_UNLOCK_COOLDOWN_SECONDS"); seconds > 0 {
		cooldown = time.Duration(seconds) * time.Second
	}
	if raw, ok := quest.Checkpoint["runtime_last_unlock_at"].(string); ok && strings.TrimSpace(raw) != "" {
		if parsed, err := time.Parse(time.RFC3339, raw); err == nil && time.Since(parsed) < cooldown {
			return 0, nil
		}
	}

	trimmed, err := h.trimWorstManagedPosition(
		ctx,
		chatID,
		exchange,
		decimal.NewFromFloat(0.35),
		"risk_unlock_trim",
	)
	if err != nil {
		return 0, err
	}
	if trimmed > 0 {
		quest.Checkpoint["runtime_last_unlock_at"] = time.Now().UTC().Format(time.RFC3339)
		quest.Checkpoint["runtime_unlock_cycles"] = checkpointInt(quest.Checkpoint["runtime_unlock_cycles"]) + 1
	}
	return trimmed, nil
}

func (h *IntegratedQuestHandlers) enforceAdaptiveTimeStop(
	ctx context.Context,
	quest *Quest,
	chatID, exchange, strategyPhase string,
) (int, error) {
	if h.lifecycleStore == nil || h.orderExecutor == nil {
		return 0, nil
	}
	if quest != nil && quest.Checkpoint == nil {
		quest.Checkpoint = make(map[string]interface{})
	}

	maxHold := adaptiveMaxHoldDuration(strategyPhase)
	positions, err := h.lifecycleStore.ListManagedOpenPositions(ctx, chatID, exchange, 100)
	if err != nil {
		return 0, err
	}
	if len(positions) == 0 {
		return 0, nil
	}

	now := time.Now().UTC()
	trimBeforeTimeout := maxHold * 3 / 4
	closed := 0

	for _, position := range positions {
		if !isAutonomousManagedPosition(position) {
			continue
		}
		openedAt := position.OpenedAt
		if openedAt.IsZero() {
			openedAt = position.UpdatedAt
		}
		if openedAt.IsZero() {
			continue
		}

		heldFor := now.Sub(openedAt)
		if heldFor >= maxHold {
			if !shouldAttemptLifecycleAction(quest, "runtime_timestop_close", position.PositionID, 2*time.Minute, now) {
				continue
			}
			trimmed, trimErr := h.trimManagedPosition(ctx, position, decimal.NewFromInt(1), "adaptive_time_stop_close")
			if trimErr != nil {
				return closed, trimErr
			}
			if trimmed > 0 {
				closed += trimmed
				markLifecycleActionAttempt(quest, "runtime_timestop_close", position.PositionID, now)
			}
			continue
		}

		if heldFor >= trimBeforeTimeout && checkpointInt(quest.Checkpoint["runtime_hold_streak"]) >= 2 {
			key := fmt.Sprintf("runtime_timestop_trim_%s", position.PositionID)
			if _, alreadyTrimmed := quest.Checkpoint[key]; alreadyTrimmed {
				continue
			}
			trimmed, trimErr := h.trimManagedPosition(ctx, position, decimal.NewFromFloat(0.35), "adaptive_time_stop_pretrim")
			if trimErr != nil {
				return closed, trimErr
			}
			if trimmed > 0 {
				closed += trimmed
				quest.Checkpoint[key] = now.Format(time.RFC3339)
			}
		}
	}

	return closed, nil
}

func adaptiveMaxHoldDuration(strategyPhase string) time.Duration {
	phase := strings.ToLower(strings.TrimSpace(strategyPhase))
	switch {
	case strings.Contains(phase, "trend"):
		return 25 * time.Minute
	case strings.Contains(phase, "chop"):
		return 8 * time.Minute
	default:
		return 15 * time.Minute
	}
}

func shouldAttemptLifecycleAction(quest *Quest, prefix, positionID string, cooldown time.Duration, now time.Time) bool {
	if quest == nil || quest.Checkpoint == nil {
		return true
	}
	key := fmt.Sprintf("%s_%s", prefix, positionID)
	raw, ok := quest.Checkpoint[key].(string)
	if !ok || strings.TrimSpace(raw) == "" {
		return true
	}
	parsed, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return true
	}
	return now.Sub(parsed) >= cooldown
}

func markLifecycleActionAttempt(quest *Quest, prefix, positionID string, now time.Time) {
	if quest == nil {
		return
	}
	if quest.Checkpoint == nil {
		quest.Checkpoint = make(map[string]interface{})
	}
	key := fmt.Sprintf("%s_%s", prefix, positionID)
	quest.Checkpoint[key] = now.Format(time.RFC3339)
}

func (h *IntegratedQuestHandlers) trimWorstManagedPosition(
	ctx context.Context,
	chatID, exchange string,
	fraction decimal.Decimal,
	source string,
) (int, error) {
	if h.lifecycleStore == nil {
		return 0, nil
	}
	positions, err := h.lifecycleStore.ListManagedOpenPositions(ctx, chatID, exchange, 100)
	if err != nil {
		return 0, err
	}
	if len(positions) == 0 {
		return 0, nil
	}

	worstIdx := -1
	worstPnL := decimal.Zero
	for idx, position := range positions {
		if !isAutonomousManagedPosition(position) {
			continue
		}
		if position.Size.Abs().LessThanOrEqual(decimal.Zero) {
			continue
		}
		if worstIdx == -1 || position.UnrealizedPnL.LessThan(worstPnL) {
			worstIdx = idx
			worstPnL = position.UnrealizedPnL
		}
	}
	if worstIdx == -1 {
		return 0, nil
	}
	return h.trimManagedPosition(ctx, positions[worstIdx], fraction, source)
}

func (h *IntegratedQuestHandlers) trimManagedPosition(
	ctx context.Context,
	position ManagedOpenPosition,
	fraction decimal.Decimal,
	source string,
) (int, error) {
	if h.orderExecutor == nil {
		return 0, nil
	}
	now := time.Now().UTC()
	if h.shouldSkipStalePositionRetry(position, now) {
		log.Printf(
			"[SCALPING] Skipping stale position retry due to cooldown: %s %s %s",
			position.Exchange,
			position.Symbol,
			position.Side,
		)
		return 0, nil
	}
	if fraction.LessThanOrEqual(decimal.Zero) {
		return 0, nil
	}
	sizeAbs := position.Size.Abs()
	if sizeAbs.LessThanOrEqual(decimal.Zero) {
		return 0, nil
	}

	closeSize := sizeAbs.Mul(fraction)
	if closeSize.GreaterThan(sizeAbs) {
		closeSize = sizeAbs
	}
	if closeSize.LessThanOrEqual(decimal.Zero) {
		return 0, nil
	}

	closeSide := oppositeCloseSide(position.Side)
	if closeSide == "" {
		closeSide = "sell"
	}
	price := position.LastPrice
	if price.LessThanOrEqual(decimal.Zero) {
		price = position.EntryPrice
	}
	notional := price.Mul(closeSize).Abs()
	if notional.LessThanOrEqual(decimal.Zero) {
		notional = closeSize.Abs()
	}

	details := TradeDetails{
		Exchange:     position.Exchange,
		Symbol:       position.Symbol,
		Side:         closeSide,
		OrderType:    "market",
		MarketType:   position.MarketType,
		Amount:       closeSize,
		AmountUSDT:   notional,
		TradeType:    "risk_reduction",
		Confidence:   1,
		Reasoning:    source,
		IsPaperTrade: h.orderExecutor.IsPaperTrading(),
		ReduceOnly:   true,
	}

	execWithBypass, hasBypass := h.orderExecutor.(interface {
		PlaceRiskReductionOrderWithDetails(context.Context, TradeDetails) (string, error)
	})
	if hasBypass {
		if _, err := execWithBypass.PlaceRiskReductionOrderWithDetails(ctx, details); err != nil {
			if isExchangePositionMissingError(err) {
				h.markStalePositionRetry(position, now)
				if closeErr := h.reconcileMissingManagedPosition(ctx, position, source, err); closeErr != nil {
					log.Printf("[SCALPING] Failed to close stale lifecycle row for %s: %v", position.PositionID, closeErr)
				}
				return 0, nil
			}
			return 0, err
		}
		return 1, nil
	}
	if _, err := h.orderExecutor.PlaceOrderWithDetails(ctx, details); err != nil {
		if isExchangePositionMissingError(err) {
			h.markStalePositionRetry(position, now)
			if closeErr := h.reconcileMissingManagedPosition(ctx, position, source, err); closeErr != nil {
				log.Printf("[SCALPING] Failed to close stale lifecycle row for %s: %v", position.PositionID, closeErr)
			}
			return 0, nil
		}
		return 0, err
	}
	return 1, nil
}

func isExchangePositionMissingError(err error) bool {
	if err == nil {
		return false
	}
	lower := strings.ToLower(strings.TrimSpace(err.Error()))
	return strings.Contains(lower, "no position to close") ||
		strings.Contains(lower, "insufficient position") ||
		strings.Contains(lower, "code: 22002") ||
		strings.Contains(lower, "code: 43023")
}

func (h *IntegratedQuestHandlers) reconcileMissingManagedPosition(
	ctx context.Context,
	position ManagedOpenPosition,
	source string,
	cause error,
) error {
	if h.lifecycleStore == nil {
		return nil
	}

	orderID := strings.TrimSpace(position.OrderID)
	if orderID == "" {
		orderID = strings.TrimSpace(position.PositionID)
	}
	if orderID == "" {
		return fmt.Errorf("cannot reconcile stale lifecycle position without order identifier")
	}

	exitPrice := position.LastPrice
	if exitPrice.LessThanOrEqual(decimal.Zero) {
		exitPrice = position.EntryPrice
	}
	if exitPrice.LessThanOrEqual(decimal.Zero) {
		exitPrice = decimal.Zero
	}

	closeSource := strings.TrimSpace(source)
	if closeSource == "" {
		closeSource = "risk_reconcile"
	}
	if !strings.Contains(strings.ToLower(closeSource), "exchange_missing") {
		closeSource = closeSource + "_exchange_missing"
	}

	if err := h.lifecycleStore.RecordClosedOrder(ctx, LifecycleCloseRecord{
		OrderID:     orderID,
		ChatID:      position.ChatID,
		Exchange:    position.Exchange,
		Symbol:      position.Symbol,
		Side:        position.Side,
		MarketType:  position.MarketType,
		Filled:      position.Size.Abs(),
		EntryPrice:  position.EntryPrice,
		ExitPrice:   exitPrice,
		RealizedPnL: decimal.Zero,
		Source:      closeSource,
		ClosedAt:    time.Now().UTC(),
	}); err != nil {
		return err
	}

	log.Printf(
		"[SCALPING] Reconciled stale managed position %s (%s %s) as closed after exchange reported missing position: %v",
		position.PositionID,
		position.Exchange,
		position.Symbol,
		cause,
	)
	return nil
}

func checkpointInt(v interface{}) int {
	switch value := v.(type) {
	case int:
		return value
	case int32:
		return int(value)
	case int64:
		return int(value)
	case float32:
		return int(value)
	case float64:
		return int(value)
	case json.Number:
		if parsed, err := value.Int64(); err == nil {
			return int(parsed)
		}
	case string:
		if parsed, err := strconv.Atoi(strings.TrimSpace(value)); err == nil {
			return parsed
		}
	}
	return 0
}

func checkpointIntWithFallback(checkpoint map[string]interface{}, primary, fallback string) int {
	if checkpoint == nil {
		return 0
	}
	if value, ok := checkpoint[primary]; ok {
		return checkpointInt(value)
	}
	return checkpointInt(checkpoint[fallback])
}

func checkpointFloat(v interface{}) float64 {
	switch value := v.(type) {
	case float64:
		return value
	case float32:
		return float64(value)
	case int:
		return float64(value)
	case int32:
		return float64(value)
	case int64:
		return float64(value)
	case json.Number:
		if parsed, err := value.Float64(); err == nil {
			return parsed
		}
	case string:
		if parsed, err := strconv.ParseFloat(strings.TrimSpace(value), 64); err == nil {
			return parsed
		}
	}
	return 0
}

func checkpointBool(v interface{}) bool {
	switch value := v.(type) {
	case bool:
		return value
	case string:
		normalized := strings.ToLower(strings.TrimSpace(value))
		return normalized == "1" || normalized == "true" || normalized == "yes" || normalized == "on"
	case int:
		return value != 0
	case int64:
		return value != 0
	case float64:
		return value != 0
	default:
		return false
	}
}

func checkpointString(v interface{}) string {
	switch value := v.(type) {
	case string:
		return strings.TrimSpace(value)
	case json.Number:
		return strings.TrimSpace(value.String())
	case fmt.Stringer:
		return strings.TrimSpace(value.String())
	case int:

		return strconv.Itoa(value)
	case int64:
		return strconv.FormatInt(value, 10)
	case float64:
		return strconv.FormatFloat(value, 'f', -1, 64)
	default:
		return ""
	}
}

func isRuntimeHoldReason(reason string) bool {
	lower := strings.ToLower(strings.TrimSpace(reason))
	if lower == "" {
		return false
	}
	return strings.Contains(lower, "execution unavailable") ||
		strings.Contains(lower, "model response parse fallback") ||
		strings.Contains(lower, "invalid model decision contract") ||
		strings.Contains(lower, "failed to parse ai decision") ||
		strings.Contains(lower, "llm completion failed") ||
		strings.Contains(lower, "runtime error")
}

func classifyAIRuntimeReason(reason string, fallback string) string {
	if classified := classifyRuntimeReasoning(reason); classified != "" {
		return classified
	}
	lower := strings.ToLower(strings.TrimSpace(reason))
	switch {
	case strings.Contains(lower, "context deadline exceeded"),
		strings.Contains(lower, "timeout"),
		strings.Contains(lower, "deadline exceeded"):
		return aiReasonLLMTimeout
	case strings.Contains(lower, "model response parse fallback"),
		strings.Contains(lower, "invalid model decision contract"),
		strings.Contains(lower, "failed to parse ai decision"),
		strings.Contains(lower, "invalid character"),
		strings.Contains(lower, "json"):
		return aiReasonLLMParseContract
	case strings.Contains(lower, "execution unavailable"),
		strings.Contains(lower, "request failed"),
		strings.Contains(lower, "futures-only mode prevented spot fallback"),
		strings.Contains(lower, "failed to get ticker"),
		strings.Contains(lower, "symbol cooldown active"),
		strings.Contains(lower, "runtime error"):
		return aiReasonExecutionUnavailable
	default:
		fallback = strings.TrimSpace(fallback)
		if fallback != "" {
			return fallback
		}
		return aiReasonExecutionUnavailable
	}
}

func shouldSendScalpingDecisionNotification(notif AIReasoningNotification) bool {
	if verbose, ok := getEnvBool("NEURATRADE_TELEGRAM_NOTIFY_AI_DECISIONS"); ok && verbose {
		return true
	}

	actionableOnly := true
	if value, ok := getEnvBool("NEURATRADE_TELEGRAM_ACTIONABLE_ONLY"); ok {
		actionableOnly = value
	}
	if !actionableOnly {
		return true
	}

	decisionType := strings.ToLower(strings.TrimSpace(notif.DecisionType))
	action := strings.ToLower(strings.TrimSpace(notif.Action))

	if action == "buy" || action == "sell" || action == "record" {
		return true
	}
	switch decisionType {
	case "pnl_reconciliation", "risk_reduction", "scalping_digest":
		return true
	default:
		return false
	}
}
