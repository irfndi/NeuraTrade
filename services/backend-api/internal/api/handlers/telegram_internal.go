package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/irfndi/neuratrade/internal/models"
	"github.com/irfndi/neuratrade/internal/services"
	"github.com/jackc/pgx/v5"
)

// TelegramInternalHandler handles internal API requests from the Telegram service.
type TelegramInternalHandler struct {
	db          services.DBPool
	userHandler *UserHandler
	questEngine *services.QuestEngine
	schemaOnce  sync.Once
	schemaErr   error
}

// NewTelegramInternalHandler creates a new instance of TelegramInternalHandler.
func NewTelegramInternalHandler(db any, userHandler *UserHandler, questEngine *services.QuestEngine) *TelegramInternalHandler {
	return &TelegramInternalHandler{
		db:          normalizeDBPool(db),
		userHandler: userHandler,
		questEngine: questEngine,
	}
}

// GetUserByChatID retrieves a user by their Telegram chat ID.
func (h *TelegramInternalHandler) GetUserByChatID(c *gin.Context) {
	chatID := c.Param("id")
	if chatID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Chat ID required"})
		return
	}

	user, err := h.userHandler.GetUserByTelegramChatID(c.Request.Context(), chatID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "User not found"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"user": gin.H{
			"id":                user.ID,
			"subscription_tier": user.SubscriptionTier,
			"created_at":        user.CreatedAt,
		},
	})
}

// GetNotificationPreferences retrieves notification settings for a user.
func (h *TelegramInternalHandler) GetNotificationPreferences(c *gin.Context) {
	userID := c.Param("userId")
	if userID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "User ID required"})
		return
	}

	// 1. Check if explicitly disabled
	queryDisabled := `
		SELECT COUNT(*) 
		FROM user_alerts 
		WHERE user_id = $1 
		  AND alert_type = 'arbitrage' 
		  AND is_active = false
	`
	var countDisabled int
	err := h.db.QueryRow(c.Request.Context(), queryDisabled, userID).Scan(&countDisabled)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch preferences"})
		return
	}
	enabled := countDisabled == 0

	// 2. Fetch active alert to get thresholds
	// If multiple, pick the most recent one
	queryActive := `
		SELECT conditions
		FROM user_alerts 
		WHERE user_id = $1 
		  AND alert_type = 'arbitrage' 
		  AND is_active = true
		ORDER BY created_at DESC
		LIMIT 1
	`
	var conditionsJSON []byte
	profitThreshold := 0.5 // Default

	// We ignore sql.ErrNoRows here, as we fall back to defaults
	row := h.db.QueryRow(c.Request.Context(), queryActive, userID)
	if err := row.Scan(&conditionsJSON); err == nil {
		var conditions models.AlertConditions
		if err := json.Unmarshal(conditionsJSON, &conditions); err == nil && conditions.ProfitThreshold != nil {
			profitThreshold, _ = conditions.ProfitThreshold.Float64()
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"enabled":          enabled,
		"profit_threshold": profitThreshold,
		"alert_frequency":  "Immediate (Periodic Scan 5m)", // Static for now as it's system config
	})
}

// SetNotificationPreferences updates notification settings for a user.
func (h *TelegramInternalHandler) SetNotificationPreferences(c *gin.Context) {
	userID := c.Param("userId")
	if userID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "User ID required"})
		return
	}

	var req struct {
		Enabled bool `json:"enabled"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request body"})
		return
	}

	tx, err := h.db.Begin(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to begin transaction"})
		return
	}
	defer func() { _ = tx.Rollback(context.Background()) }()

	if req.Enabled {
		// To enable, we remove any disabling records for 'arbitrage'
		query := `
			DELETE FROM user_alerts 
			WHERE user_id = $1 
			  AND alert_type = 'arbitrage' 
			  AND is_active = false
		`
		_, err := tx.Exec(c.Request.Context(), query, userID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update preferences"})
			return
		}
	} else {
		// To disable, ensure a disabling record exists
		deleteQuery := `
			DELETE FROM user_alerts 
			WHERE user_id = $1 
			  AND alert_type = 'arbitrage'
		`
		_, err := tx.Exec(c.Request.Context(), deleteQuery, userID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to clear old preferences"})
			return
		}

		insertQuery := `
			INSERT INTO user_alerts (id, user_id, alert_type, conditions, is_active, created_at)
			VALUES ($1, $2, 'arbitrage', $3, false, $4)
		`
		conditions := map[string]interface{}{
			"notifications_enabled": false,
		}
		conditionsJSON, _ := json.Marshal(conditions)

		newID := uuid.New().String()
		_, err = tx.Exec(c.Request.Context(), insertQuery, newID, userID, conditionsJSON, time.Now())
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to insert preference"})
			return
		}
	}

	if err := tx.Commit(c.Request.Context()); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to commit transaction"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"status":  "success",
		"enabled": req.Enabled,
	})
}

type autonomousStateRequest struct {
	ChatID string `json:"chat_id" binding:"required"`
}

type connectExchangeRequest struct {
	ChatID       string `json:"chat_id" binding:"required"`
	Exchange     string `json:"exchange" binding:"required"`
	AccountLabel string `json:"account_label"`
}

type connectPolymarketRequest struct {
	ChatID        string `json:"chat_id" binding:"required"`
	WalletAddress string `json:"wallet_address" binding:"required"`
}

type addWalletRequest struct {
	ChatID        string `json:"chat_id" binding:"required"`
	WalletAddress string `json:"wallet_address" binding:"required"`
	WalletType    string `json:"wallet_type"`
}

type removeWalletRequest struct {
	ChatID            string `json:"chat_id" binding:"required"`
	WalletIDOrAddress string `json:"wallet_id_or_address" binding:"required"`
}

type walletRecordInput struct {
	ChatID        string
	Provider      string
	WalletType    string
	WalletAddress string
	AccountLabel  string
}

func (h *TelegramInternalHandler) BeginAutonomous(c *gin.Context) {
	var req autonomousStateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request body"})
		return
	}

	chatID := strings.TrimSpace(req.ChatID)
	if chatID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "chat_id is required"})
		return
	}

	if err := h.ensureOperatorSchema(c.Request.Context()); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to initialize operator state"})
		return
	}

	failedChecks, err := h.collectReadinessFailures(c.Request.Context(), chatID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to evaluate readiness"})
		return
	}

	if len(failedChecks) > 0 {
		c.JSON(http.StatusOK, gin.H{
			"ok":               false,
			"status":           "blocked",
			"mode":             "autonomous",
			"readiness_passed": false,
			"failed_checks":    failedChecks,
			"message":          "Readiness gate blocked autonomous mode",
		})
		return
	}

	now := time.Now().UTC()
	_, err = h.db.Exec(
		c.Request.Context(),
		`INSERT INTO telegram_operator_state (chat_id, autonomous_enabled, updated_at)
		 VALUES ($1, true, $2)
		 ON CONFLICT (chat_id)
		 DO UPDATE SET autonomous_enabled = EXCLUDED.autonomous_enabled, updated_at = EXCLUDED.updated_at`,
		chatID,
		now,
	)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to persist autonomous state"})
		return
	}

	// Start quest engine for this user
	if h.questEngine != nil {
		_, err := h.questEngine.BeginAutonomous(chatID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to start quest engine: " + err.Error()})
			return
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"ok":               true,
		"status":           "active",
		"mode":             "autonomous",
		"readiness_passed": true,
		"message":          "Autonomous mode started",
	})
}

func (h *TelegramInternalHandler) PauseAutonomous(c *gin.Context) {
	var req autonomousStateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request body"})
		return
	}

	chatID := strings.TrimSpace(req.ChatID)
	if chatID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "chat_id is required"})
		return
	}

	if err := h.ensureOperatorSchema(c.Request.Context()); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to initialize operator state"})
		return
	}

	now := time.Now().UTC()
	_, err := h.db.Exec(
		c.Request.Context(),
		`INSERT INTO telegram_operator_state (chat_id, autonomous_enabled, updated_at)
		 VALUES ($1, false, $2)
		 ON CONFLICT (chat_id)
		 DO UPDATE SET autonomous_enabled = EXCLUDED.autonomous_enabled, updated_at = EXCLUDED.updated_at`,
		chatID,
		now,
	)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to persist autonomous state"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"ok":      true,
		"status":  "paused",
		"message": "Autonomous mode paused",
	})
}

func (h *TelegramInternalHandler) ConnectExchange(c *gin.Context) {
	var req connectExchangeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request body"})
		return
	}

	chatID := strings.TrimSpace(req.ChatID)
	exchange := strings.ToLower(strings.TrimSpace(req.Exchange))
	accountLabel := strings.TrimSpace(req.AccountLabel)
	if chatID == "" || exchange == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "chat_id and exchange are required"})
		return
	}

	walletAddress := fmt.Sprintf("exchange:%s", exchange)
	if accountLabel != "" {
		walletAddress = fmt.Sprintf("%s:%s", walletAddress, accountLabel)
	}

	if err := h.ensureOperatorSchema(c.Request.Context()); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to initialize wallet store"})
		return
	}

	_, err := h.upsertWallet(c.Request.Context(), walletRecordInput{
		ChatID:        chatID,
		Provider:      exchange,
		WalletType:    "exchange",
		WalletAddress: walletAddress,
		AccountLabel:  accountLabel,
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to connect exchange"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"ok":      true,
		"message": fmt.Sprintf("Exchange connected: %s", exchange),
	})
}

func (h *TelegramInternalHandler) ConnectPolymarket(c *gin.Context) {
	var req connectPolymarketRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request body"})
		return
	}

	chatID := strings.TrimSpace(req.ChatID)
	walletAddress := strings.TrimSpace(req.WalletAddress)
	if chatID == "" || walletAddress == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "chat_id and wallet_address are required"})
		return
	}

	if !isHexWalletAddress(walletAddress) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid wallet address format"})
		return
	}

	if err := h.ensureOperatorSchema(c.Request.Context()); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to initialize wallet store"})
		return
	}

	_, err := h.upsertWallet(c.Request.Context(), walletRecordInput{
		ChatID:        chatID,
		Provider:      "polymarket",
		WalletType:    "trading",
		WalletAddress: strings.ToLower(walletAddress),
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to connect Polymarket wallet"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"ok":      true,
		"message": "Polymarket wallet connected",
	})
}

func (h *TelegramInternalHandler) AddWallet(c *gin.Context) {
	var req addWalletRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request body"})
		return
	}

	chatID := strings.TrimSpace(req.ChatID)
	walletAddress := strings.TrimSpace(req.WalletAddress)
	walletType := strings.TrimSpace(req.WalletType)
	if walletType == "" {
		walletType = "external"
	}
	if chatID == "" || walletAddress == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "chat_id and wallet_address are required"})
		return
	}

	if !isHexWalletAddress(walletAddress) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid wallet address format"})
		return
	}

	if err := h.ensureOperatorSchema(c.Request.Context()); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to initialize wallet store"})
		return
	}

	_, err := h.upsertWallet(c.Request.Context(), walletRecordInput{
		ChatID:        chatID,
		Provider:      "wallet",
		WalletType:    strings.ToLower(walletType),
		WalletAddress: strings.ToLower(walletAddress),
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to add wallet"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"ok":      true,
		"message": "Wallet added",
	})
}

func (h *TelegramInternalHandler) RemoveWallet(c *gin.Context) {
	var req removeWalletRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request body"})
		return
	}

	chatID := strings.TrimSpace(req.ChatID)
	identifier := strings.TrimSpace(req.WalletIDOrAddress)
	if chatID == "" || identifier == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "chat_id and wallet_id_or_address are required"})
		return
	}

	if err := h.ensureOperatorSchema(c.Request.Context()); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to initialize wallet store"})
		return
	}

	result, err := h.db.Exec(
		c.Request.Context(),
		`DELETE FROM telegram_operator_wallets
		 WHERE chat_id = $1
		   AND (wallet_id = $2 OR LOWER(wallet_address) = LOWER($2))`,
		chatID,
		identifier,
	)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to remove wallet"})
		return
	}

	rowsAffected, rowsErr := result.RowsAffected()
	if rowsErr != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to remove wallet"})
		return
	}
	if rowsAffected == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "Wallet not found"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"ok":      true,
		"message": "Wallet removed",
	})
}

func (h *TelegramInternalHandler) GetWallets(c *gin.Context) {
	chatID := strings.TrimSpace(c.Query("chat_id"))
	if chatID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "chat_id is required"})
		return
	}

	if err := h.ensureOperatorSchema(c.Request.Context()); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to initialize wallet store"})
		return
	}

	rows, err := h.db.Query(
		c.Request.Context(),
		`SELECT wallet_id, wallet_type, provider, wallet_address, status, created_at
		 FROM telegram_operator_wallets
		 WHERE chat_id = $1
		 ORDER BY updated_at DESC`,
		chatID,
	)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch wallets"})
		return
	}
	defer rows.Close()

	wallets := make([]gin.H, 0)
	for rows.Next() {
		var walletID string
		var walletType string
		var provider string
		var walletAddress string
		var status string
		var createdAt time.Time

		if err := rows.Scan(&walletID, &walletType, &provider, &walletAddress, &status, &createdAt); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to parse wallet data"})
			return
		}

		wallets = append(wallets, gin.H{
			"wallet_id":      walletID,
			"type":           walletType,
			"provider":       provider,
			"address_masked": maskWalletAddress(walletAddress),
			"status":         status,
			"connected_at":   createdAt.UTC().Format(time.RFC3339),
		})
	}

	if rows.Err() != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch wallets"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"wallets": wallets})
}

func (h *TelegramInternalHandler) GetDoctor(c *gin.Context) {
	chatID := strings.TrimSpace(c.Query("chat_id"))
	if chatID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "chat_id is required"})
		return
	}

	if err := h.ensureOperatorSchema(c.Request.Context()); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to initialize diagnostics"})
		return
	}

	checks := make([]gin.H, 0, 4)
	overall := "healthy"

	if err := h.db.QueryRow(c.Request.Context(), "SELECT 1").Scan(new(int)); err != nil {
		checks = append(checks, gin.H{
			"name":     "database",
			"status":   "critical",
			"impact":   "core",
			"optional": false,
			"message":  "database query failed",
		})
		overall = "critical"
	} else {
		checks = append(checks, gin.H{
			"name":     "database",
			"status":   "healthy",
			"impact":   "core",
			"optional": false,
		})
	}

	polymarketCount, err := h.countConnectedWallets(c.Request.Context(), chatID, "provider = 'polymarket' AND status = 'connected'")
	if err != nil {
		checks = append(checks, gin.H{
			"name":     "polymarket-wallet",
			"status":   "warning",
			"impact":   "optional",
			"optional": true,
			"message":  "failed to verify polymarket wallet",
		})
	} else if polymarketCount == 0 {
		checks = append(checks, gin.H{
			"name":     "polymarket-wallet",
			"status":   "warning",
			"impact":   "optional",
			"optional": true,
			"message":  "connect one wallet with /connect_polymarket",
			"details":  gin.H{"count": "0"},
		})
	} else {
		checks = append(checks, gin.H{
			"name":     "polymarket-wallet",
			"status":   "healthy",
			"impact":   "optional",
			"optional": true,
			"details": gin.H{
				"count": fmt.Sprintf("%d", polymarketCount),
			},
		})
	}

	exchangeCount, err := h.countConnectedWallets(c.Request.Context(), chatID, "provider <> 'polymarket' AND wallet_type = 'exchange' AND status = 'connected'")
	if err != nil {
		exchangeCount = 0
	}

	// Also check config file for exchange API keys (fallback for CLI config).
	if config, err := loadOperatorConfigFile(); err == nil && hasConfiguredExchangeAPIKey(config) && exchangeCount == 0 {
		exchangeCount = 1
	}

	if exchangeCount == 0 {
		if overall != "critical" {
			overall = "warning"
		}
		checks = append(checks, gin.H{
			"name":     "exchange-connection",
			"status":   "warning",
			"impact":   "core",
			"optional": false,
			"message":  "connect one exchange with /connect_exchange",
			"details":  gin.H{"count": "0"},
		})
	} else {
		checks = append(checks, gin.H{
			"name":     "exchange-connection",
			"status":   "healthy",
			"impact":   "core",
			"optional": false,
			"details": gin.H{
				"count": fmt.Sprintf("%d", exchangeCount),
			},
		})
	}

	var autonomousEnabled bool
	if err := h.db.QueryRow(
		c.Request.Context(),
		"SELECT COALESCE((SELECT autonomous_enabled FROM telegram_operator_state WHERE chat_id = $1 LIMIT 1), false)",
		chatID,
	).Scan(&autonomousEnabled); err != nil {
		if overall != "critical" {
			overall = "warning"
		}
		checks = append(checks, gin.H{
			"name":     "autonomous-mode",
			"status":   "warning",
			"impact":   "core",
			"optional": false,
			"message":  "unable to determine mode state",
		})
	} else if autonomousEnabled {
		checks = append(checks, gin.H{
			"name":     "autonomous-mode",
			"status":   "healthy",
			"impact":   "core",
			"optional": false,
			"message":  "autonomous mode is running",
		})
	} else {
		if overall == "healthy" {
			overall = "warning"
		}
		checks = append(checks, gin.H{
			"name":     "autonomous-mode",
			"status":   "warning",
			"impact":   "core",
			"optional": false,
			"message":  "run /begin to start autonomous mode",
		})
	}

	aiRuntimeImpact := "optional"
	aiRuntimeOptional := true
	if autonomousEnabled {
		aiRuntimeImpact = "core"
		aiRuntimeOptional = false
	}
	if h.questEngine == nil {
		status := "warning"
		message := "quest runtime diagnostics unavailable"
		if aiRuntimeOptional {
			status = "warning"
		}
		checks = append(checks, gin.H{
			"name":     "ai-runtime",
			"status":   status,
			"impact":   aiRuntimeImpact,
			"optional": aiRuntimeOptional,
			"message":  message,
		})
		if !aiRuntimeOptional && overall != "critical" {
			overall = "warning"
		}
	} else {
		diagnostics := h.questEngine.GetChatRuntimeDiagnostics(chatID)
		rawRuntime, _ := diagnostics["ai_runtime"].(map[string]interface{})
		driftActive, _ := diagnostics["state_drift_active"].(bool)
		driftPositions := readIntFromRecord(diagnostics, "state_drift_positions")
		entryGateReason := ""
		if rawReason, ok := diagnostics["entry_gate_reason_current"].(string); ok {
			entryGateReason = strings.TrimSpace(rawReason)
		}
		entryGateType := ""
		if rawType, ok := diagnostics["entry_gate_type"].(string); ok {
			entryGateType = strings.TrimSpace(rawType)
		}
		nextUnblockCondition := ""
		if rawNext, ok := diagnostics["next_unblock_condition_current"].(string); ok {
			nextUnblockCondition = strings.TrimSpace(rawNext)
		}
		riskLockSource := ""
		if rawSource, ok := diagnostics["risk_lock_source"].(string); ok {
			riskLockSource = strings.TrimSpace(rawSource)
		}
		entryAttemptBlockReason := ""
		if rawBlock, ok := diagnostics["entry_attempt_block_reason"].(string); ok {
			entryAttemptBlockReason = strings.TrimSpace(rawBlock)
		}
		accountTier := ""
		if rawTier, ok := diagnostics["account_tier"].(string); ok {
			accountTier = strings.TrimSpace(rawTier)
		}
		effectiveMinConfidence := readFloatFromRecord(diagnostics, "effective_min_confidence")
		effectiveMaxCapitalPct := readFloatFromRecord(diagnostics, "effective_max_capital_pct")
		candidateViableCount := readIntFromRecord(diagnostics, "candidate_viable_count")
		rolloutStageCurrent := ""
		if rawStage, ok := diagnostics["rollout_stage_current"].(string); ok {
			rolloutStageCurrent = strings.TrimSpace(rawStage)
		}
		rolloutStatusCurrent := ""
		if rawStatus, ok := diagnostics["rollout_status_current"].(string); ok {
			rolloutStatusCurrent = strings.TrimSpace(rawStatus)
		}
		rolloutGateReason := ""
		if rawReason, ok := diagnostics["rollout_gate_reason_current"].(string); ok {
			rolloutGateReason = strings.TrimSpace(rawReason)
		}
		entryAttempts1h := readIntFromRecord(diagnostics, "entry_attempts_1h")
		lastEntryAttemptAt := ""
		if rawLast, ok := diagnostics["last_entry_attempt_at"].(string); ok {
			lastEntryAttemptAt = strings.TrimSpace(rawLast)
		}
		minutesSinceEntryAttempt := readFloatFromRecord(diagnostics, "minutes_since_entry_attempt")
		driftSignature := ""
		if rawSignature, ok := diagnostics["drift_signature"].(string); ok {
			driftSignature = strings.TrimSpace(rawSignature)
		}
		driftDeadlockCycles := readIntFromRecord(diagnostics, "drift_deadlock_cycles")
		executionStage := ""
		if rawStage, ok := diagnostics["execution_stage"].(string); ok {
			executionStage = strings.TrimSpace(rawStage)
		}
		executionLastProgressAt := ""
		if rawProgressAt, ok := diagnostics["execution_last_progress_at"].(string); ok {
			executionLastProgressAt = strings.TrimSpace(rawProgressAt)
		}
		executionInProgressAge := readFloatFromRecord(diagnostics, "execution_in_progress_age_seconds")
		runtimeStatus := "healthy"
		if statusRaw, ok := rawRuntime["status"].(string); ok && strings.TrimSpace(statusRaw) != "" {
			runtimeStatus = strings.ToLower(strings.TrimSpace(statusRaw))
		}
		providerChainUsable := readIntFromRecord(rawRuntime, "provider_chain_usable")
		providerChainConfigured := readIntFromRecord(rawRuntime, "provider_chain_configured")
		runtimeReason := ""
		if reason, ok := rawRuntime["runtime_degraded_reason"].(string); ok && strings.TrimSpace(reason) != "" {
			runtimeReason = strings.TrimSpace(reason)
		}
		if entryGateReason == "" {
			switch entryGateType {
			case "risk_lock":
				entryGateReason = "entry blocked by risk lock"
			case "state_drift":
				entryGateReason = fmt.Sprintf("entry blocked by state drift (%d mismatch(es))", driftPositions)
			case "runtime_circuit":
				entryGateReason = "entry blocked by AI runtime circuit breaker"
			case "recovery_gate":
				entryGateReason = "entry blocked by recovery clean-cycle gate"
			case "none":
				// Do not merge entryAttemptBlockReason into entryGateReason.
				// Candidate-level rejections are surfaced separately via entry_attempt_block_reason.
			}
		}
		if entryGateReason == "" && rolloutGateReason != "" && candidateViableCount > 0 {
			entryGateReason = rolloutGateReason
		}

		message := "AI runtime healthy"
		if driftActive {
			if runtimeStatus == "healthy" {
				runtimeStatus = "warning"
			}
			message = fmt.Sprintf(
				"Entry blocked by state drift: %d mismatch(es) pending reconcile",
				driftPositions,
			)
		} else {
			switch runtimeStatus {
			case "critical":
				message = "AI runtime degraded: high timeout/parse failure pressure"
			case "warning":
				message = "AI runtime warning: elevated error rate"
			}
		}
		if entryGateReason != "" {
			message = entryGateReason
			if nextUnblockCondition != "" {
				message = fmt.Sprintf("%s (next: %s)", message, nextUnblockCondition)
			}
		} else if runtimeReason != "" {
			message = runtimeReason
		}
		if providerChainConfigured > 0 && providerChainUsable <= 1 {
			if runtimeStatus == "healthy" {
				message = "AI runtime healthy (single-provider chain; failover redundancy limited)"
			}
		}
		details := gin.H{}
		if errorRate, ok := rawRuntime["error_rate"].(float64); ok {
			details["error_rate"] = fmt.Sprintf("%.2f", errorRate)
		}
		if timeouts, ok := rawRuntime["window_timeouts"].(int); ok {
			details["timeouts"] = fmt.Sprintf("%d", timeouts)
		}
		if parseFails, ok := rawRuntime["window_parse_fails"].(int); ok {
			details["parse_fails"] = fmt.Sprintf("%d", parseFails)
		}
		if circuitActive, ok := rawRuntime["circuit_active"].(bool); ok {
			details["circuit_active"] = fmt.Sprintf("%t", circuitActive)
		}
		if runtimeReason != "" {
			details["runtime_degraded_reason"] = runtimeReason
		}
		details["state_drift_active"] = fmt.Sprintf("%t", driftActive)
		details["state_drift_positions"] = fmt.Sprintf("%d", driftPositions)
		details["drift_deadlock_cycles"] = fmt.Sprintf("%d", driftDeadlockCycles)
		details["entry_attempts_1h"] = fmt.Sprintf("%d", entryAttempts1h)
		details["minutes_since_entry_attempt"] = fmt.Sprintf("%.1f", minutesSinceEntryAttempt)
		if entryGateReason != "" {
			details["entry_gate_reason"] = entryGateReason
		}
		if entryGateType != "" {
			details["entry_gate_type"] = entryGateType
		}
		if riskLockSource != "" {
			details["risk_lock_source"] = riskLockSource
		}
		if entryAttemptBlockReason != "" {
			details["entry_attempt_block_reason"] = entryAttemptBlockReason
		}
		if accountTier != "" {
			details["account_tier"] = accountTier
		}
		if effectiveMinConfidence > 0 {
			details["effective_min_confidence"] = fmt.Sprintf("%.2f", effectiveMinConfidence)
		}
		if effectiveMaxCapitalPct > 0 {
			details["effective_max_capital_pct"] = fmt.Sprintf("%.2f", effectiveMaxCapitalPct)
		}
		if walletBasisMode, ok := diagnostics["wallet_basis_mode"].(string); ok && strings.TrimSpace(walletBasisMode) != "" {
			details["wallet_basis_mode"] = strings.TrimSpace(walletBasisMode)
		}
		if walletBasisSource, ok := diagnostics["wallet_basis_source"].(string); ok && strings.TrimSpace(walletBasisSource) != "" {
			details["wallet_basis_source"] = strings.TrimSpace(walletBasisSource)
		}
		if walletBasisUSDT, ok := diagnostics["wallet_basis_usdt"]; ok {
			details["wallet_basis_usdt"] = fmt.Sprintf("%v", walletBasisUSDT)
		}
		if missingDetected, ok := diagnostics["protection_missing_detected"]; ok {
			details["protection_missing_detected"] = fmt.Sprintf("%v", missingDetected)
		}
		if missingRecovered, ok := diagnostics["protection_missing_recovered"]; ok {
			details["protection_missing_recovered"] = fmt.Sprintf("%v", missingRecovered)
		}
		if riskExpectancy, ok := diagnostics["risk_expectancy"]; ok {
			details["risk_expectancy"] = fmt.Sprintf("%v", riskExpectancy)
		}
		if riskExpectancyGross, ok := diagnostics["risk_expectancy_gross"]; ok {
			details["risk_expectancy_gross"] = fmt.Sprintf("%v", riskExpectancyGross)
		}
		if riskFeeDrag, ok := diagnostics["risk_fee_drag_expectancy"]; ok {
			details["risk_fee_drag_expectancy"] = fmt.Sprintf("%v", riskFeeDrag)
		}
		if metaPromotions, ok := diagnostics["runtime_ai_meta_hold_promotions"]; ok {
			details["runtime_ai_meta_hold_promotions"] = fmt.Sprintf("%v", metaPromotions)
		}
		if effectiveOpen, ok := diagnostics["managed_open_positions_effective"]; ok {
			details["managed_open_positions_effective"] = fmt.Sprintf("%v", effectiveOpen)
		}
		if ghostCleaned, ok := diagnostics["ghost_positions_cleaned"]; ok {
			details["ghost_positions_cleaned"] = fmt.Sprintf("%v", ghostCleaned)
		}
		details["candidate_viable_count"] = fmt.Sprintf("%d", candidateViableCount)
		if rolloutStageCurrent != "" {
			details["rollout_stage_current"] = rolloutStageCurrent
		}
		if rolloutStatusCurrent != "" {
			details["rollout_status_current"] = rolloutStatusCurrent
		}
		if rolloutGateReason != "" {
			details["rollout_gate_reason_current"] = rolloutGateReason
		}
		if nextUnblockCondition != "" {
			details["next_unblock_condition"] = nextUnblockCondition
		}
		if currentCycles, ok := diagnostics["recovery_clean_cycles_current"]; ok {
			details["recovery_clean_cycles_current"] = fmt.Sprintf("%v", currentCycles)
		}
		if requiredCycles, ok := diagnostics["recovery_clean_cycles_required"]; ok {
			details["recovery_clean_cycles_required"] = fmt.Sprintf("%v", requiredCycles)
		}
		if cyclesToEntry, ok := diagnostics["recovery_cycles_to_entry"]; ok {
			details["recovery_cycles_to_entry"] = fmt.Sprintf("%v", cyclesToEntry)
		}
		if rawEvalAt, ok := diagnostics["recovery_gate_eval_at"].(string); ok && strings.TrimSpace(rawEvalAt) != "" {
			details["recovery_gate_eval_at"] = strings.TrimSpace(rawEvalAt)
		}
		if lastEntryAttemptAt != "" {
			details["last_entry_attempt_at"] = lastEntryAttemptAt
		}
		if driftSignature != "" {
			details["drift_signature"] = driftSignature
		}
		if executionStage != "" {
			details["execution_stage"] = executionStage
		}
		if executionLastProgressAt != "" {
			details["execution_last_progress_at"] = executionLastProgressAt
			details["execution_in_progress_age_seconds"] = fmt.Sprintf("%.1f", executionInProgressAge)
		}
		details["provider_chain_configured"] = fmt.Sprintf("%d", providerChainConfigured)
		details["provider_chain_usable"] = fmt.Sprintf("%d", providerChainUsable)

		checks = append(checks, gin.H{
			"name":     "ai-runtime",
			"status":   runtimeStatus,
			"impact":   aiRuntimeImpact,
			"optional": aiRuntimeOptional,
			"message":  message,
			"details":  details,
		})

		if !aiRuntimeOptional {
			if runtimeStatus == "critical" {
				overall = "critical"
			} else if runtimeStatus == "warning" && overall != "critical" {
				overall = "warning"
			}
		}
	}

	selectedModel := ""
	if err := h.db.QueryRow(c.Request.Context(), `
		SELECT COALESCE(selected_ai_model, '')
		FROM users
		WHERE COALESCE(telegram_chat_id, '') = $1 OR COALESCE(telegram_id, '') = $1
		LIMIT 1
	`, chatID).Scan(&selectedModel); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			checks = append(checks, gin.H{
				"name":     "ai-snapshot",
				"status":   "healthy",
				"impact":   "optional",
				"optional": true,
				"message":  "No user record found",
			})
		} else {
			checks = append(checks, gin.H{
				"name":     "ai-snapshot",
				"status":   "warning",
				"impact":   "optional",
				"optional": true,
				"message":  "AI snapshot probe unavailable",
			})
		}
	} else {
		message := "AI snapshot probe successful"
		if strings.TrimSpace(selectedModel) != "" {
			message = fmt.Sprintf("Model selected: %s", selectedModel)
		}
		checks = append(checks, gin.H{
			"name":     "ai-snapshot",
			"status":   "healthy",
			"impact":   "optional",
			"optional": true,
			"message":  message,
		})
	}

	summary := "All checks healthy"
	switch overall {
	case "warning":
		summary = "One or more checks need attention"
	case "critical":
		summary = "Critical checks failed"
	}

	c.JSON(http.StatusOK, gin.H{
		"overall_status": overall,
		"summary":        summary,
		"checked_at":     time.Now().UTC().Format(time.RFC3339),
		"checks":         checks,
	})
}

func (h *TelegramInternalHandler) GetAIStatusByChatID(c *gin.Context) {
	chatID := strings.TrimSpace(c.Param("chatId"))
	if chatID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "chat_id is required"})
		return
	}

	selectedModel := ""
	lookupError := false
	query := `
		SELECT COALESCE(selected_ai_model, '')
		FROM users
		WHERE COALESCE(telegram_chat_id, '') = $1
		   OR COALESCE(telegram_id, '') = $1
		LIMIT 1
	`
	if err := h.db.QueryRow(c.Request.Context(), query, chatID).Scan(&selectedModel); err != nil {
		if !errors.Is(err, pgx.ErrNoRows) {
			lookupError = true
		}
		selectedModel = ""
	}

	provider := ""
	modelLower := strings.ToLower(strings.TrimSpace(selectedModel))
	switch {
	case strings.HasPrefix(modelLower, "gpt"):
		provider = "openai"
	case strings.HasPrefix(modelLower, "claude"):
		provider = "anthropic"
	case strings.Contains(modelLower, "glm"):
		provider = "zhipu"
	case strings.Contains(modelLower, "mini"):
		provider = "minimax"
	}

	runtimeReady := false
	providerChainConfigured := 0
	providerChainUsable := 0
	effectiveProvider := ""
	effectiveModel := ""
	autoRouting := false

	if h.questEngine != nil {
		diagnostics := h.questEngine.GetChatRuntimeDiagnostics(chatID)
		rawRuntime, _ := diagnostics["ai_runtime"].(map[string]interface{})
		if rawRuntime != nil {
			providerChainConfigured = readIntFromRecord(rawRuntime, "provider_chain_configured")
			providerChainUsable = readIntFromRecord(rawRuntime, "provider_chain_usable")

			if lastProvider, ok := rawRuntime["last_success_provider"].(string); ok && strings.TrimSpace(lastProvider) != "" {
				effectiveProvider = strings.TrimSpace(lastProvider)
			}
			if lastModel, ok := rawRuntime["last_success_model"].(string); ok && strings.TrimSpace(lastModel) != "" {
				effectiveModel = strings.TrimSpace(lastModel)
			}
		} else {
			providerChainConfigured = readIntFromRecord(diagnostics, "provider_chain_configured")
			providerChainUsable = readIntFromRecord(diagnostics, "provider_chain_usable")
		}

		if providerChainUsable > 0 {
			runtimeReady = true
		}
	}

	if selectedModel == "" && runtimeReady && !lookupError {
		autoRouting = true
	}

	readiness := "unavailable"
	switch {
	case lookupError:
		readiness = "unavailable"
	case providerChainUsable > 0 && selectedModel != "":
		readiness = "ready"
	case providerChainUsable > 0 && selectedModel == "":
		readiness = "ready_auto_route"
	case providerChainConfigured > 0 && providerChainUsable == 0:
		readiness = "degraded"
	}

	c.JSON(http.StatusOK, gin.H{
		"selected_model":            selectedModel,
		"provider":                  provider,
		"daily_spend":               "0.00",
		"monthly_spend":             "0.00",
		"budget_limit":              "Unlimited",
		"daily_budget_exceeded":     false,
		"runtime_ready":             runtimeReady,
		"provider_chain_configured": providerChainConfigured,
		"provider_chain_usable":     providerChainUsable,
		"effective_provider":        effectiveProvider,
		"effective_model":           effectiveModel,
		"auto_routing":              autoRouting,
		"readiness":                 readiness,
	})
}

func (h *TelegramInternalHandler) ensureOperatorSchema(ctx context.Context) error {
	h.schemaOnce.Do(func() {
		if h.db == nil {
			h.schemaErr = fmt.Errorf("database is not available")
			return
		}

		_, h.schemaErr = h.db.Exec(ctx, `CREATE TABLE IF NOT EXISTS telegram_operator_wallets (
			wallet_id TEXT PRIMARY KEY,
			chat_id TEXT NOT NULL,
			provider TEXT NOT NULL,
			wallet_type TEXT NOT NULL,
			wallet_address TEXT NOT NULL,
			account_label TEXT,
			status TEXT NOT NULL,
			created_at TIMESTAMP NOT NULL,
			updated_at TIMESTAMP NOT NULL,
			UNIQUE(chat_id, provider, wallet_address)
		)`)
		if h.schemaErr != nil {
			return
		}

		_, h.schemaErr = h.db.Exec(ctx, `CREATE TABLE IF NOT EXISTS telegram_operator_state (
			chat_id TEXT PRIMARY KEY,
			autonomous_enabled BOOLEAN NOT NULL,
			updated_at TIMESTAMP NOT NULL
		)`)
	})

	return h.schemaErr
}

func (h *TelegramInternalHandler) collectReadinessFailures(ctx context.Context, chatID string) ([]string, error) {
	failedChecks := make([]string, 0, 2)

	pmWalletCount, err := h.countConnectedWallets(ctx, chatID, "provider = 'polymarket' AND status = 'connected'")
	if err != nil {
		pmWalletCount = 0
	}

	exchangeCount, err := h.countConnectedWallets(ctx, chatID, "provider <> 'polymarket' AND wallet_type = 'exchange' AND status = 'connected'")
	if err != nil {
		exchangeCount = 0
	}

	if config, err := loadOperatorConfigFile(); err == nil && hasConfiguredExchangeAPIKey(config) && exchangeCount < 1 {
		exchangeCount = 1
	}

	totalWallets := pmWalletCount + exchangeCount
	if totalWallets < 1 {
		failedChecks = append(failedChecks, "wallet minimum")
	}

	if exchangeCount < 1 {
		failedChecks = append(failedChecks, "exchange minimum")
	}

	return failedChecks, nil
}

func (h *TelegramInternalHandler) countConnectedWallets(ctx context.Context, chatID, filter string) (int, error) {
	query := `SELECT COUNT(*) FROM telegram_operator_wallets WHERE chat_id = $1 AND ` + filter
	var count int
	err := h.db.QueryRow(ctx, query, chatID).Scan(&count)
	if err != nil {
		return 0, err
	}
	return count, nil
}

func loadOperatorConfigFile() (map[string]interface{}, error) {
	configPath := os.ExpandEnv("$HOME/.neuratrade/config.json")
	// #nosec G304 -- fixed operator config path under $HOME/.neuratrade
	content, err := os.ReadFile(configPath)
	if err != nil {
		return nil, err
	}

	var config map[string]interface{}
	if err := json.Unmarshal(content, &config); err != nil {
		return nil, err
	}

	return config, nil
}

func hasConfiguredExchangeAPIKey(config map[string]interface{}) bool {
	if ccxt, ok := config["ccxt"].(map[string]interface{}); ok {
		if hasExchangeAPIKeyInMap(ccxt["exchanges"]) {
			return true
		}
	}

	if serviceConfig, ok := config["services"].(map[string]interface{}); ok {
		if ccxt, ok := serviceConfig["ccxt"].(map[string]interface{}); ok {
			if hasExchangeAPIKeyInMap(ccxt["exchanges"]) {
				return true
			}
		}
	}

	return false
}

func hasExchangeAPIKeyInMap(rawExchanges interface{}) bool {
	exchanges, ok := rawExchanges.(map[string]interface{})
	if !ok {
		return false
	}

	for _, rawExchange := range exchanges {
		exchangeConfig, ok := rawExchange.(map[string]interface{})
		if !ok {
			continue
		}

		apiKey, ok := exchangeConfig["api_key"].(string)
		if ok && strings.TrimSpace(apiKey) != "" {
			return true
		}
	}

	return false
}

func (h *TelegramInternalHandler) upsertWallet(ctx context.Context, input walletRecordInput) (string, error) {
	now := time.Now().UTC()
	walletID := uuid.NewString()

	var storedWalletID string
	err := h.db.QueryRow(
		ctx,
		`INSERT INTO telegram_operator_wallets (
			wallet_id, chat_id, provider, wallet_type, wallet_address, account_label, status, created_at, updated_at
		 ) VALUES ($1, $2, $3, $4, $5, $6, 'connected', $7, $7)
		 ON CONFLICT (chat_id, provider, wallet_address)
		 DO UPDATE SET wallet_type = EXCLUDED.wallet_type, account_label = EXCLUDED.account_label, status = 'connected', updated_at = EXCLUDED.updated_at
		 RETURNING wallet_id`,
		walletID,
		input.ChatID,
		input.Provider,
		input.WalletType,
		input.WalletAddress,
		input.AccountLabel,
		now,
	).Scan(&storedWalletID)
	if err != nil {
		return "", err
	}

	return storedWalletID, nil
}

func isHexWalletAddress(walletAddress string) bool {
	if len(walletAddress) != 42 {
		return false
	}
	if !strings.HasPrefix(walletAddress, "0x") {
		return false
	}

	for _, c := range walletAddress[2:] {
		isDigit := c >= '0' && c <= '9'
		isLowerHex := c >= 'a' && c <= 'f'
		isUpperHex := c >= 'A' && c <= 'F'
		if !isDigit && !isLowerHex && !isUpperHex {
			return false
		}
	}

	return true
}

func maskWalletAddress(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if strings.HasPrefix(strings.ToLower(trimmed), "0x") && len(trimmed) >= 12 {
		return trimmed[:6] + "..." + trimmed[len(trimmed)-4:]
	}

	if len(trimmed) > 24 {
		return trimmed[:10] + "..." + trimmed[len(trimmed)-6:]
	}

	return trimmed
}

func readIntFromRecord(record map[string]interface{}, key string) int {
	if record == nil {
		return 0
	}
	switch value := record[key].(type) {
	case int:
		return value
	case int64:
		return int(value)
	case float64:
		return int(value)
	default:
		return 0
	}
}

func readFloatFromRecord(record map[string]interface{}, key string) float64 {
	if record == nil {
		return 0
	}
	switch value := record[key].(type) {
	case float64:
		return value
	case int:
		return float64(value)
	case int64:
		return float64(value)
	default:
		return 0
	}
}

// GetSummary returns a summary of trading performance
func (h *TelegramInternalHandler) GetSummary(c *gin.Context) {
	chatID := strings.TrimSpace(c.Query("chat_id"))
	if chatID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "chat_id is required"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"ok": true,
		"summary": gin.H{
			"total_trades":   1,
			"winning_trades": 0,
			"losing_trades":  0,
			"total_pnl":      "0.00",
			"win_rate":       "0.00%",
			"best_trade":     "0.00",
			"worst_trade":    "0.00",
		},
		"message": "Summary retrieved (test mode - no live trades yet)",
	})
}
