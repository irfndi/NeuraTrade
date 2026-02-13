package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/irfndi/neuratrade/internal/models"
	"github.com/irfndi/neuratrade/internal/services"
)

// TelegramInternalHandler handles internal API requests from the Telegram service.
type TelegramInternalHandler struct {
	db          services.DBPool
	userHandler *UserHandler
	schemaOnce  sync.Once
	schemaErr   error
}

// NewTelegramInternalHandler creates a new instance of TelegramInternalHandler.
func NewTelegramInternalHandler(db any, userHandler *UserHandler) *TelegramInternalHandler {
	return &TelegramInternalHandler{
		db:          normalizeDBPool(db),
		userHandler: userHandler,
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
			"name":    "database",
			"status":  "critical",
			"message": "database query failed",
		})
		overall = "critical"
	} else {
		checks = append(checks, gin.H{
			"name":   "database",
			"status": "healthy",
		})
	}

	polymarketCount, err := h.countConnectedWallets(c.Request.Context(), chatID, "polymarket")
	if err != nil {
		checks = append(checks, gin.H{
			"name":    "polymarket-wallet",
			"status":  "critical",
			"message": "failed to verify polymarket wallet",
		})
		overall = "critical"
	} else if polymarketCount == 0 {
		if overall != "critical" {
			overall = "warning"
		}
		checks = append(checks, gin.H{
			"name":    "polymarket-wallet",
			"status":  "warning",
			"message": "connect one wallet with /connect_polymarket",
			"details": gin.H{"count": "0"},
		})
	} else {
		checks = append(checks, gin.H{
			"name":   "polymarket-wallet",
			"status": "healthy",
			"details": gin.H{
				"count": fmt.Sprintf("%d", polymarketCount),
			},
		})
	}

	exchangeCount, err := h.countConnectedWallets(c.Request.Context(), chatID, "exchange")
	if err != nil {
		checks = append(checks, gin.H{
			"name":    "exchange-connection",
			"status":  "critical",
			"message": "failed to verify exchange connections",
		})
		overall = "critical"
	} else if exchangeCount == 0 {
		if overall != "critical" {
			overall = "warning"
		}
		checks = append(checks, gin.H{
			"name":    "exchange-connection",
			"status":  "warning",
			"message": "connect one exchange with /connect_exchange",
			"details": gin.H{"count": "0"},
		})
	} else {
		checks = append(checks, gin.H{
			"name":   "exchange-connection",
			"status": "healthy",
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
			"name":    "autonomous-mode",
			"status":  "warning",
			"message": "unable to determine mode state",
		})
	} else if autonomousEnabled {
		checks = append(checks, gin.H{
			"name":    "autonomous-mode",
			"status":  "healthy",
			"message": "autonomous mode is running",
		})
	} else {
		if overall == "healthy" {
			overall = "warning"
		}
		checks = append(checks, gin.H{
			"name":    "autonomous-mode",
			"status":  "warning",
			"message": "run /begin to start autonomous mode",
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

	polymarketCount, err := h.countConnectedWallets(ctx, chatID, "polymarket")
	if err != nil {
		return nil, err
	}
	if polymarketCount < 1 {
		failedChecks = append(failedChecks, "wallet minimum")
	}

	exchangeCount, err := h.countConnectedWallets(ctx, chatID, "exchange")
	if err != nil {
		return nil, err
	}
	if exchangeCount < 1 {
		failedChecks = append(failedChecks, "exchange minimum")
	}

	return failedChecks, nil
}

func (h *TelegramInternalHandler) countConnectedWallets(ctx context.Context, chatID string, filterType string) (int, error) {
	var query string
	var count int

	switch filterType {
	case "polymarket":
		query = `SELECT COUNT(*) FROM telegram_operator_wallets WHERE chat_id = $1 AND provider = 'polymarket' AND status = 'connected'`
	case "exchange":
		query = `SELECT COUNT(*) FROM telegram_operator_wallets WHERE chat_id = $1 AND provider <> 'polymarket' AND wallet_type = 'exchange' AND status = 'connected'`
	default:
		query = `SELECT COUNT(*) FROM telegram_operator_wallets WHERE chat_id = $1 AND status = 'connected'`
	}

	err := h.db.QueryRow(ctx, query, chatID).Scan(&count)
	if err != nil {
		return 0, err
	}
	return count, nil
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

// BindOperatorProfile binds a Telegram chat to an operator profile using an auth code.
func (h *TelegramInternalHandler) BindOperatorProfile(c *gin.Context) {
	var req struct {
		ChatID           string  `json:"chat_id" binding:"required"`
		TelegramUserID   string  `json:"telegram_user_id" binding:"required"`
		TelegramUsername *string `json:"telegram_username"`
		AuthCode         string  `json:"auth_code" binding:"required"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "Invalid request: " + err.Error()})
		return
	}

	if err := h.ensureOperatorSchema(c.Request.Context()); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": "Failed to initialize operator store"})
		return
	}

	_, err := h.db.Exec(c.Request.Context(), `
		CREATE TABLE IF NOT EXISTS telegram_operator_bindings (
			binding_id TEXT PRIMARY KEY,
			chat_id TEXT UNIQUE NOT NULL,
			telegram_user_id TEXT NOT NULL,
			telegram_username TEXT,
			operator_name TEXT,
			auth_code TEXT,
			bound_at TIMESTAMP NOT NULL,
			updated_at TIMESTAMP NOT NULL
		)`)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": "Failed to initialize bindings store"})
		return
	}

	if len(req.AuthCode) < 6 || len(req.AuthCode) > 32 {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "Invalid auth code format"})
		return
	}

	var existingBindingID string
	err = h.db.QueryRow(c.Request.Context(),
		"SELECT binding_id FROM telegram_operator_bindings WHERE chat_id = $1",
		req.ChatID).Scan(&existingBindingID)
	if err == nil {
		c.JSON(http.StatusConflict, gin.H{"success": false, "error": "Chat is already bound to an operator profile"})
		return
	}

	bindingID := uuid.NewString()
	now := time.Now().UTC()
	operatorName := "Operator"
	if req.TelegramUsername != nil && *req.TelegramUsername != "" {
		operatorName = *req.TelegramUsername
	}

	_, err = h.db.Exec(c.Request.Context(), `
		INSERT INTO telegram_operator_bindings (binding_id, chat_id, telegram_user_id, telegram_username, operator_name, auth_code, bound_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $7)`,
		bindingID, req.ChatID, req.TelegramUserID, req.TelegramUsername, operatorName, req.AuthCode, now)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": "Failed to create binding: " + err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success":       true,
		"operator_name": operatorName,
	})
}

// UnbindOperatorProfile removes the binding between a Telegram chat and an operator profile.
func (h *TelegramInternalHandler) UnbindOperatorProfile(c *gin.Context) {
	var req struct {
		ChatID         string `json:"chat_id" binding:"required"`
		TelegramUserID string `json:"telegram_user_id" binding:"required"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "Invalid request: " + err.Error()})
		return
	}

	if err := h.ensureOperatorSchema(c.Request.Context()); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": "Failed to initialize operator store"})
		return
	}

	result, err := h.db.Exec(c.Request.Context(),
		"DELETE FROM telegram_operator_bindings WHERE chat_id = $1 AND telegram_user_id = $2",
		req.ChatID, req.TelegramUserID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": "Failed to unbind: " + err.Error()})
		return
	}

	rowsAffected, _ := result.RowsAffected()
	if rowsAffected == 0 {
		c.JSON(http.StatusNotFound, gin.H{"success": false, "error": "No binding found for this chat"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"success": true})
}
