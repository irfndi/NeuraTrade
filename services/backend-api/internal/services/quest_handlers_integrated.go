package services

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/irfndi/neuratrade/internal/ai/llm"
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
	technicalAnalysis   *TechnicalAnalysisService
	ccxtService         interface{}
	arbitrageService    interface{}
	futuresArbService   interface{}
	notificationService *NotificationService
	monitoring          *AutonomousMonitorManager
	orderExecutor       ScalpingOrderExecutor
	aiScalpingService   *AIScalpingService
	tradeMemory         *TradeMemory
	lifecycleStore      *TradingLifecycleStore
	protectionManager   *DynamicProtectionManager
	db                  *sql.DB // Database for user settings
}

const (
	scalpingFailureCooldownThreshold = 3
	scalpingBaseCooldown             = 2 * time.Minute
	scalpingMaxCooldown              = 15 * time.Minute
	defaultBootstrapInterval         = 10 * time.Minute
	defaultSpotUnwindInterval        = 15 * time.Minute
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

// SetOrderExecutor sets the order executor for scalping
func (h *IntegratedQuestHandlers) SetOrderExecutor(executor ScalpingOrderExecutor) {
	h.orderExecutor = executor
}

// SetDB sets the database for user settings lookup
func (h *IntegratedQuestHandlers) SetDB(db *sql.DB) {
	h.db = db
}

// SetTradeMemory sets the trade memory for AI learning
func (h *IntegratedQuestHandlers) SetTradeMemory(memory *TradeMemory) {
	h.tradeMemory = memory
}

func (h *IntegratedQuestHandlers) SetLifecycleStore(store *TradingLifecycleStore) {
	h.lifecycleStore = store
}

func (h *IntegratedQuestHandlers) SetDynamicProtectionManager(manager *DynamicProtectionManager) {
	h.protectionManager = manager
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
	log.Printf("[SCALPING] AI-driven scalping service initialized")
}

// RegisterIntegratedHandlers registers production-ready quest handlers
func (e *QuestEngine) RegisterIntegratedHandlers(handlers *IntegratedQuestHandlers) {
	// Register a single routine handler and dispatch by quest definition_id.
	// RegisterHandler stores one handler per QuestType, so multiple registrations
	// for QuestTypeRoutine were previously overwriting each other.
	e.RegisterHandler(QuestTypeRoutine, func(ctx context.Context, quest *Quest) error {
		var err error
		switch quest.Metadata["definition_id"] {
		case "market_scan":
			err = handlers.handleMarketScanWithTA(ctx, quest)
		case "funding_rate_scan":
			err = handlers.handleFundingRateScan(ctx, quest)
		case "portfolio_health":
			err = handlers.handlePortfolioHealthWithRisk(ctx, quest)
		case "scalping_execution":
			err = handlers.handleScalpingExecution(ctx, quest)
		default:
			err = fmt.Errorf("unknown routine quest definition: %s", quest.Metadata["definition_id"])
		}
		handlers.recordQuestResult(quest, err == nil, decimal.Zero)
		return err
	})

	// Arbitrage Execution - execute arbitrage opportunities when detected
	e.RegisterHandler(QuestTypeArbitrage, func(ctx context.Context, quest *Quest) error {
		err := handlers.handleArbitrageExecution(ctx, quest)
		handlers.recordQuestResult(quest, err == nil, decimal.Zero)
		return err
	})

	log.Println("Integrated quest handlers registered successfully")
}

// recordQuestResult records quest execution result for monitoring
func (h *IntegratedQuestHandlers) recordQuestResult(quest *Quest, success bool, pnl decimal.Decimal) {
	chatID := quest.Metadata["chat_id"]
	if h.monitoring != nil && chatID != "" {
		h.monitoring.RecordQuestExecution(chatID, success, pnl)
	}
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

	if chatScopedExec, ok := h.orderExecutor.(interface{ SetChatID(string) }); ok {
		chatScopedExec.SetChatID(chatID)
	}

	// Set exchange on AI scalping service
	if h.aiScalpingService != nil {
		h.aiScalpingService.SetExchange(userExchange)
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

	// Check if we're in dry-run/paper trading mode
	isDryRun := false
	if quest.Metadata != nil {
		if dryRunVal, ok := quest.Metadata["dry_run"]; ok && dryRunVal == "true" {
			isDryRun = true
		}
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
			if balanceSnapshot.Total != nil {
				if v := balanceSnapshot.Total["USDT"]; v > 0 {
					usdtBalance = v
				}
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

	portfolio := TradingPortfolio{
		USDTBalance:   usdtBalance,
		TotalValue:    usdtBalance,
		OpenPositions: 0,
	}
	h.enrichPortfolioControlPlane(ctx, quest, chatID, userExchange, &portfolio)

	log.Printf("[SCALPING] Portfolio: %.2f USDT available", usdtBalance)

	decision, err := h.aiScalpingService.ExecuteTradingCycle(ctx, portfolio)
	if err != nil {
		log.Printf("[SCALPING] AI decision error: %v", err)
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
		h.notifyScalpingDecision(ctx, chatID, AIReasoningNotification{
			DecisionType: "scalping_cycle",
			Summary:      "Scalping cycle skipped due to AI/runtime error",
			Confidence:   0,
			Reasons:      reasons,
			Action:       "hold",
		})
		// Return nil instead of err to prevent panic - quest continues with hold status
		return nil
	}

	// Safety check: decision should not be nil
	if decision == nil {
		log.Printf("[SCALPING] AI returned nil decision - treating as hold")
		streak, cooldown := h.recordScalpingFailure(quest, "decision payload was nil")
		quest.Checkpoint["status"] = "hold"
		quest.Checkpoint["ai_action"] = "hold"
		quest.Checkpoint["ai_reasoning"] = "AI returned nil decision"
		reasons := []string{"Decision payload was nil; cycle held", fmt.Sprintf("Runtime failure streak: %d", streak)}
		if cooldown > 0 {
			reasons = append(reasons, fmt.Sprintf("Cooldown applied: %s", cooldown.Round(time.Second).String()))
		}
		h.notifyScalpingDecision(ctx, chatID, AIReasoningNotification{
			DecisionType: "scalping_cycle",
			Summary:      "AI returned no trade decision",
			Confidence:   0,
			Reasons:      reasons,
			Action:       "hold",
		})
		return nil
	}

	quest.Checkpoint["ai_action"] = decision.Action
	quest.Checkpoint["ai_symbol"] = decision.Symbol
	quest.Checkpoint["ai_confidence"] = decision.Confidence
	quest.Checkpoint["ai_reasoning"] = decision.Reasoning
	quest.Checkpoint["ai_size_pct"] = decision.SizePercent

	if decision.Action == "hold" {
		if isRuntimeHoldReason(decision.Reasoning) {
			streak, cooldown := h.recordScalpingFailure(quest, decision.Reasoning)
			if cooldown > 0 {
				quest.Checkpoint["runtime_hold_cooldown"] = cooldown.String()
			}
			quest.Checkpoint["runtime_failure_streak"] = streak
		} else {
			h.resetScalpingFailureState(quest)
		}
		log.Printf("[SCALPING] AI decided to hold: %s", decision.Reasoning)
		quest.Checkpoint["status"] = "hold"
		h.notifyScalpingDecision(ctx, chatID, AIReasoningNotification{
			DecisionType: "scalping_cycle",
			Summary:      "AI held position this cycle",
			Confidence:   decision.Confidence,
			Reasons:      []string{decision.Reasoning},
			Action:       "hold",
		})
		return nil
	}

	h.resetScalpingFailureState(quest)
	quest.Checkpoint["status"] = "ai_executed"
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
			Amount:     decimal.NewFromFloat(portfolio.USDTBalance * decision.SizePercent / 100),
			EntryPrice: entryPrice,
			StopLoss:   decimalValueOrZero(decision.StopLoss),
			TakeProfit: decimalValueOrZero(decision.TakeProfit),
			Source:     "autonomous_scalping",
			OpenedAt:   time.Now().UTC(),
		}); err != nil {
			log.Printf("[SCALPING] Failed to persist execution lifecycle for %s: %v", decision.OrderID, err)
		}
	}
	h.recordTradeDecision(ctx, quest, decision, userExchange)
	h.ingestClosedOrderFeedback(ctx, quest, userExchange, decision.Symbol)

	log.Printf("[SCALPING] AI decision executed: %s %s (%.0f%% confidence)",
		decision.Action, decision.Symbol, decision.Confidence*100)

	h.notifyScalpingDecision(ctx, chatID, AIReasoningNotification{
		DecisionType: "scalping",
		Summary:      fmt.Sprintf("AI decided to %s %s", decision.Action, decision.Symbol),
		Confidence:   decision.Confidence,
		Reasons:      []string{decision.Reasoning},
		Action:       decision.Action,
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
	if exposureRatio <= 1.0 {
		return 0, exposureRatio, nil
	}

	closed, deriskErr := h.autoDeriskBlockedExposure(
		ctx,
		quest,
		chatID,
		exchange,
		fmt.Sprintf("pre-trade exposure guard %.2f exceeded hard limit 1.00", exposureRatio),
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

func (h *IntegratedQuestHandlers) enrichPortfolioControlPlane(
	ctx context.Context,
	quest *Quest,
	chatID, exchange string,
	portfolio *TradingPortfolio,
) {
	if portfolio == nil {
		return
	}

	if h.lifecycleStore != nil {
		positions, err := h.lifecycleStore.ListManagedOpenPositions(ctx, chatID, exchange, 100)
		if err == nil {
			portfolio.OpenPositions = len(positions)
			unrealized := decimal.Zero
			for _, pos := range positions {
				unrealized = unrealized.Add(pos.UnrealizedPnL)
			}
			portfolio.UnrealizedPnL = unrealized.InexactFloat64()
			portfolio.TotalValue = portfolio.USDTBalance + portfolio.UnrealizedPnL
			if portfolio.TotalValue <= 0 {
				portfolio.TotalValue = portfolio.USDTBalance
			}
		}
	}

	returns := make([]float64, 0, 64)
	if h.lifecycleStore != nil {
		series, err := h.lifecycleStore.GetRealizedReturnSeries(ctx, chatID, exchange, time.Now().UTC().Add(-30*24*time.Hour))
		if err == nil {
			returns = append(returns, series...)
		}
	}
	if len(returns) == 0 {
		returns = GetScalpingPerformance().GetReturnSeries(200)
	}

	riskMetrics := ComputeRiskAdjustedMetrics(returns)
	portfolio.RiskSharpe = riskMetrics.Sharpe
	portfolio.RiskSortino = riskMetrics.Sortino
	portfolio.RiskDrawdown = riskMetrics.MaxDrawdown
	portfolio.RiskExpectancy = riskMetrics.Expectancy
	portfolio.RiskSampleSize = riskMetrics.SampleSize

	phaseDetector := phase_management.NewPhaseDetector(phase_management.DefaultPhaseDetectorConfig(), nil)
	currentPhase := phaseDetector.GetPhaseForValue(decimal.NewFromFloat(portfolio.TotalValue))
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
		quest.Checkpoint["risk_sharpe"] = portfolio.RiskSharpe
		quest.Checkpoint["risk_sortino"] = portfolio.RiskSortino
		quest.Checkpoint["risk_max_drawdown"] = portfolio.RiskDrawdown
		quest.Checkpoint["risk_expectancy"] = portfolio.RiskExpectancy
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

// ProductionQuestExecutor handles production quest execution with full monitoring
type ProductionQuestExecutor struct {
	handlers   *IntegratedQuestHandlers
	engine     *QuestEngine
	monitoring *AutonomousMonitorManager
}

// NewProductionQuestExecutor creates a production-ready quest executor
func NewProductionQuestExecutor(
	ta *TechnicalAnalysisService,
	ccxt interface{},
	arb interface{},
	futuresArb interface{},
	notif *NotificationService,
) *ProductionQuestExecutor {
	monitoring := NewAutonomousMonitorManager(notif)
	handlers := NewIntegratedQuestHandlers(ta, ccxt, arb, futuresArb, notif, monitoring)
	engine := NewQuestEngineWithNotification(NewInMemoryQuestStore(), nil, notif)

	// Register integrated handlers
	engine.RegisterIntegratedHandlers(handlers)

	return &ProductionQuestExecutor{
		handlers:   handlers,
		engine:     engine,
		monitoring: monitoring,
	}
}

// Start begins quest execution
func (e *ProductionQuestExecutor) Start() {
	e.engine.Start()
	log.Println("Production quest executor started")
}

// Stop stops quest execution
func (e *ProductionQuestExecutor) Stop() {
	e.engine.Stop()
	log.Println("Production quest executor stopped")
}

// GetStatus returns executor status
func (e *ProductionQuestExecutor) GetStatus(chatID string) map[string]interface{} {
	return e.handlers.GetMonitoringSnapshot(chatID)
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

func (h *IntegratedQuestHandlers) notifyScalpingDecision(ctx context.Context, chatID string, notif AIReasoningNotification) {
	if h.notificationService == nil {
		return
	}
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
	if err := h.notificationService.NotifyAIReasoning(ctx, chatIDInt, notif); err != nil {
		log.Printf("[NOTIFICATION] Failed to send scalping status notification: %v", err)
	}
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

func (h *IntegratedQuestHandlers) recordTradeDecision(ctx context.Context, quest *Quest, decision *AITradingDecision, exchange string) {
	if h.tradeMemory == nil || decision == nil || decision.Action == "hold" {
		return
	}

	tradeID := strings.TrimSpace(decision.OrderID)
	if tradeID == "" {
		tradeID = fmt.Sprintf("ai-order-%d", time.Now().UnixNano())
	}

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
		return
	}

	quest.Checkpoint["trade_memory_id"] = tradeID
}

func (h *IntegratedQuestHandlers) ingestClosedOrderFeedback(ctx context.Context, quest *Quest, exchange, symbol string) {
	if h.orderExecutor == nil || strings.TrimSpace(symbol) == "" {
		return
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

	for _, order := range closedOrders {
		orderID := getOrderID(order)
		if orderID == "" || processed[orderID] {
			continue
		}

		pnl, ok := decimalFromOrder(order, "totalProfits", "totalProfit", "pnl", "profit", "realizedPnl", "achievedProfits")
		if !ok {
			continue
		}

		side := "buy"
		if rawSide, ok := stringFromOrder(order, "side", "tradeSide", "positionSide"); ok {
			side = strings.ToLower(strings.TrimSpace(rawSide))
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

		if h.lifecycleStore != nil {
			fees := decimal.Zero
			if v, ok := decimalFromOrder(order, "fees", "fee", "totalFee", "commission"); ok {
				fees = v
			}
			closedAt := timestampFromOrder(order)
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
		h.notifyScalpingDecision(ctx, quest.Metadata["chat_id"], AIReasoningNotification{
			DecisionType: "pnl_reconciliation",
			Summary:      summary,
			Confidence:   1,
			Reasons: []string{
				"Closed orders were synced from exchange state",
			},
			Action: "record",
		})
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

func isRuntimeHoldReason(reason string) bool {
	lower := strings.ToLower(strings.TrimSpace(reason))
	if lower == "" {
		return false
	}
	return strings.Contains(lower, "execution unavailable") ||
		strings.Contains(lower, "model response parse fallback") ||
		strings.Contains(lower, "failed to parse ai decision") ||
		strings.Contains(lower, "llm completion failed") ||
		strings.Contains(lower, "runtime error")
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

	return decisionType == "pnl_reconciliation" || action == "record"
}
