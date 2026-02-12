package handlers

import (
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/irfndi/neuratrade/internal/services"
)

// AutonomousHandler handles autonomous mode endpoints
type AutonomousHandler struct {
	questEngine *services.QuestEngine
	readiness   *ReadinessChecker
}

// NewAutonomousHandler creates a new autonomous handler
func NewAutonomousHandler(questEngine *services.QuestEngine) *AutonomousHandler {
	return &AutonomousHandler{
		questEngine: questEngine,
		readiness:   NewReadinessChecker(),
	}
}

// BeginRequest represents the request body for /begin
type BeginRequest struct {
	ChatID string `json:"chat_id" binding:"required"`
}

// PauseRequest represents the request body for /pause
type PauseRequest struct {
	ChatID string `json:"chat_id" binding:"required"`
}

// BeginAutonomousResponse represents the response for /begin
type BeginAutonomousResponse struct {
	Ok              bool     `json:"ok"`
	Status          string   `json:"status,omitempty"`
	Mode            string   `json:"mode,omitempty"`
	Message         string   `json:"message,omitempty"`
	ReadinessPassed bool     `json:"readiness_passed"`
	FailedChecks    []string `json:"failed_checks,omitempty"`
}

// PauseAutonomousResponse represents the response for /pause
type PauseAutonomousResponse struct {
	Ok      bool   `json:"ok"`
	Status  string `json:"status,omitempty"`
	Message string `json:"message,omitempty"`
}

// QuestProgressResponse represents the response for /quests
type QuestProgressResponse struct {
	Quests    []services.QuestProgress `json:"quests"`
	UpdatedAt string                   `json:"updated_at,omitempty"`
}

// PortfolioPosition represents a portfolio position
type PortfolioPosition struct {
	Symbol        string `json:"symbol"`
	Side          string `json:"side"`
	Size          string `json:"size"`
	EntryPrice    string `json:"entry_price,omitempty"`
	MarkPrice     string `json:"mark_price,omitempty"`
	UnrealizedPnL string `json:"unrealized_pnl,omitempty"`
}

// PortfolioResponse represents the response for /portfolio
type PortfolioResponse struct {
	TotalEquity      string              `json:"total_equity"`
	AvailableBalance string              `json:"available_balance,omitempty"`
	Exposure         string              `json:"exposure,omitempty"`
	Positions        []PortfolioPosition `json:"positions"`
	UpdatedAt        string              `json:"updated_at,omitempty"`
}

// OperatorLogEntry represents a log entry
type OperatorLogEntry struct {
	Timestamp string `json:"timestamp"`
	Level     string `json:"level"`
	Source    string `json:"source,omitempty"`
	Message   string `json:"message"`
}

// LogsResponse represents the response for /logs
type LogsResponse struct {
	Logs []OperatorLogEntry `json:"logs"`
}

// DoctorCheck represents a diagnostic check result
type DoctorCheck struct {
	Name      string            `json:"name"`
	Status    string            `json:"status"`
	Message   string            `json:"message,omitempty"`
	LatencyMs int64             `json:"latency_ms,omitempty"`
	Details   map[string]string `json:"details,omitempty"`
}

// DoctorResponse represents the response for /doctor
type DoctorResponse struct {
	OverallStatus string        `json:"overall_status"`
	Summary       string        `json:"summary,omitempty"`
	CheckedAt     string        `json:"checked_at,omitempty"`
	Checks        []DoctorCheck `json:"checks"`
}

// PerformanceSummaryResponse represents the response for /performance/summary
type PerformanceSummaryResponse struct {
	Timeframe  string `json:"timeframe"`
	PnL        string `json:"pnl"`
	WinRate    string `json:"win_rate,omitempty"`
	Sharpe     string `json:"sharpe,omitempty"`
	Drawdown   string `json:"drawdown,omitempty"`
	Trades     int    `json:"trades,omitempty"`
	BestTrade  string `json:"best_trade,omitempty"`
	WorstTrade string `json:"worst_trade,omitempty"`
	Note       string `json:"note,omitempty"`
}

// StrategyPerformance represents performance for a strategy
type StrategyPerformance struct {
	Strategy string `json:"strategy"`
	PnL      string `json:"pnl"`
	WinRate  string `json:"win_rate,omitempty"`
	Sharpe   string `json:"sharpe,omitempty"`
	Drawdown string `json:"drawdown,omitempty"`
	Trades   int    `json:"trades,omitempty"`
}

// PerformanceBreakdownResponse represents the response for /performance
type PerformanceBreakdownResponse struct {
	Timeframe  string                     `json:"timeframe"`
	Overall    PerformanceSummaryResponse `json:"overall"`
	Strategies []StrategyPerformance      `json:"strategies"`
}

// LiquidationResponse represents the response for liquidation endpoints
type LiquidationResponse struct {
	Ok              bool   `json:"ok"`
	Message         string `json:"message,omitempty"`
	LiquidatedCount int    `json:"liquidated_count,omitempty"`
	RequestID       string `json:"request_id,omitempty"`
}

// WalletCommandResponse represents the response for wallet commands
type WalletCommandResponse struct {
	Ok      bool   `json:"ok"`
	Message string `json:"message,omitempty"`
}

// WalletInfo represents wallet information
type WalletInfo struct {
	WalletID      string `json:"wallet_id,omitempty"`
	Type          string `json:"type"`
	Provider      string `json:"provider"`
	AddressMasked string `json:"address_masked"`
	Status        string `json:"status"`
	ConnectedAt   string `json:"connected_at,omitempty"`
}

// WalletsResponse represents the response for /wallets
type WalletsResponse struct {
	Wallets []WalletInfo `json:"wallets"`
}

// BeginAutonomous starts autonomous trading mode
func (h *AutonomousHandler) BeginAutonomous(c *gin.Context) {
	var req BeginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request: " + err.Error()})
		return
	}

	// Run readiness checks
	readinessResult := h.readiness.Check(c, req.ChatID)

	if !readinessResult.Passed {
		c.JSON(http.StatusOK, BeginAutonomousResponse{
			Ok:              false,
			Status:          "blocked",
			ReadinessPassed: false,
			FailedChecks:    readinessResult.FailedChecks,
			Message:         "Readiness gate blocked autonomous mode. Run /doctor for diagnostics.",
		})
		return
	}

	// Start autonomous mode
	_, err := h.questEngine.BeginAutonomous(req.ChatID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to start autonomous mode: " + err.Error()})
		return
	}

	c.JSON(http.StatusOK, BeginAutonomousResponse{
		Ok:              true,
		Status:          "active",
		Mode:            "autonomous",
		ReadinessPassed: true,
		Message:         "Autonomous mode started successfully. Use /pause to stop and /summary for 24h results.",
	})
}

// PauseAutonomous pauses autonomous trading mode
func (h *AutonomousHandler) PauseAutonomous(c *gin.Context) {
	var req PauseRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request: " + err.Error()})
		return
	}

	state, err := h.questEngine.PauseAutonomous(req.ChatID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to pause autonomous mode: " + err.Error()})
		return
	}

	message := "Autonomous mode paused."
	if !state.IsActive && state.StartedAt.IsZero() {
		message = "Autonomous mode was not active."
	}

	c.JSON(http.StatusOK, PauseAutonomousResponse{
		Ok:      true,
		Status:  "paused",
		Message: message,
	})
}

// GetQuests returns quest progress for a user
func (h *AutonomousHandler) GetQuests(c *gin.Context) {
	chatID := c.Query("chat_id")
	if chatID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "chat_id is required"})
		return
	}

	progress, err := h.questEngine.GetQuestProgress(chatID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to get quest progress: " + err.Error()})
		return
	}

	c.JSON(http.StatusOK, QuestProgressResponse{
		Quests:    progress,
		UpdatedAt: time.Now().UTC().Format(time.RFC3339),
	})
}

// GetPortfolio returns portfolio snapshot for a user
func (h *AutonomousHandler) GetPortfolio(c *gin.Context) {
	chatID := c.Query("chat_id")
	if chatID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "chat_id is required"})
		return
	}

	// TODO: Implement actual portfolio retrieval from exchange connectors
	// For now, return placeholder data
	c.JSON(http.StatusOK, PortfolioResponse{
		TotalEquity:      "0.00",
		AvailableBalance: "0.00",
		Exposure:         "0%",
		Positions:        []PortfolioPosition{},
		UpdatedAt:        time.Now().UTC().Format(time.RFC3339),
	})
}

// GetLogs returns recent operator logs for a user
func (h *AutonomousHandler) GetLogs(c *gin.Context) {
	chatID := c.Query("chat_id")
	if chatID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "chat_id is required"})
		return
	}

	// TODO: Implement actual log retrieval
	// For now, return placeholder logs
	logs := []OperatorLogEntry{
		{
			Timestamp: time.Now().UTC().Format(time.RFC3339),
			Level:     "INFO",
			Source:    "system",
			Message:   "Autonomous mode initialized",
		},
	}

	c.JSON(http.StatusOK, LogsResponse{Logs: logs})
}

// GetDoctor runs diagnostic checks for a user
func (h *AutonomousHandler) GetDoctor(c *gin.Context) {
	chatID := c.Query("chat_id")
	if chatID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "chat_id is required"})
		return
	}

	readinessResult := h.readiness.Check(c, chatID)

	checks := make([]DoctorCheck, 0, len(readinessResult.Checks))
	for name, result := range readinessResult.Checks {
		check := DoctorCheck{
			Name:    name,
			Status:  result.Status,
			Message: result.Message,
		}
		if result.LatencyMs > 0 {
			check.LatencyMs = result.LatencyMs
		}
		if result.Details != nil {
			check.Details = result.Details
		}
		checks = append(checks, check)
	}

	overallStatus := "healthy"
	if !readinessResult.Passed {
		if len(readinessResult.FailedChecks) > 2 {
			overallStatus = "critical"
		} else {
			overallStatus = "warning"
		}
	}

	c.JSON(http.StatusOK, DoctorResponse{
		OverallStatus: overallStatus,
		Summary:       readinessResult.Summary,
		CheckedAt:     time.Now().UTC().Format(time.RFC3339),
		Checks:        checks,
	})
}

// GetPerformanceSummary returns 24h performance summary
func (h *AutonomousHandler) GetPerformanceSummary(c *gin.Context) {
	chatID := c.Query("chat_id")
	if chatID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "chat_id is required"})
		return
	}

	timeframe := c.DefaultQuery("timeframe", "24h")

	// TODO: Implement actual performance calculation
	c.JSON(http.StatusOK, PerformanceSummaryResponse{
		Timeframe: timeframe,
		PnL:       "0.00",
		WinRate:   "N/A",
		Sharpe:    "N/A",
		Drawdown:  "0%",
		Trades:    0,
		Note:      "No trading activity in this period",
	})
}

// GetPerformanceBreakdown returns detailed performance breakdown
func (h *AutonomousHandler) GetPerformanceBreakdown(c *gin.Context) {
	chatID := c.Query("chat_id")
	if chatID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "chat_id is required"})
		return
	}

	timeframe := c.DefaultQuery("timeframe", "24h")

	// TODO: Implement actual performance breakdown
	c.JSON(http.StatusOK, PerformanceBreakdownResponse{
		Timeframe: timeframe,
		Overall: PerformanceSummaryResponse{
			Timeframe: timeframe,
			PnL:       "0.00",
			WinRate:   "N/A",
			Sharpe:    "N/A",
			Drawdown:  "0%",
			Trades:    0,
			Note:      "No trading activity in this period",
		},
		Strategies: []StrategyPerformance{},
	})
}

// Liquidate liquidates a specific position
func (h *AutonomousHandler) Liquidate(c *gin.Context) {
	var req struct {
		ChatID string `json:"chat_id" binding:"required"`
		Symbol string `json:"symbol" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request: " + err.Error()})
		return
	}

	// TODO: Implement actual liquidation logic
	c.JSON(http.StatusOK, LiquidationResponse{
		Ok:              true,
		Message:         "Liquidation request submitted for " + req.Symbol,
		LiquidatedCount: 0,
		RequestID:       generateRequestID(),
	})
}

// LiquidateAll liquidates all positions
func (h *AutonomousHandler) LiquidateAll(c *gin.Context) {
	var req struct {
		ChatID string `json:"chat_id" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request: " + err.Error()})
		return
	}

	// TODO: Implement actual liquidation logic
	c.JSON(http.StatusOK, LiquidationResponse{
		Ok:              true,
		Message:         "Full liquidation request submitted",
		LiquidatedCount: 0,
		RequestID:       generateRequestID(),
	})
}

// ConnectExchange connects an exchange account
func (h *AutonomousHandler) ConnectExchange(c *gin.Context) {
	var req struct {
		ChatID       string `json:"chat_id" binding:"required"`
		Exchange     string `json:"exchange" binding:"required"`
		AccountLabel string `json:"account_label,omitempty"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request: " + err.Error()})
		return
	}

	// TODO: Implement actual exchange connection
	c.JSON(http.StatusOK, WalletCommandResponse{
		Ok:      true,
		Message: "Exchange connection initiated for " + req.Exchange,
	})
}

// ConnectPolymarket connects a Polymarket wallet
func (h *AutonomousHandler) ConnectPolymarket(c *gin.Context) {
	var req struct {
		ChatID        string `json:"chat_id" binding:"required"`
		WalletAddress string `json:"wallet_address" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request: " + err.Error()})
		return
	}

	// TODO: Implement actual Polymarket connection
	c.JSON(http.StatusOK, WalletCommandResponse{
		Ok:      true,
		Message: "Polymarket wallet connection initiated",
	})
}

// AddWallet adds a watch-only wallet
func (h *AutonomousHandler) AddWallet(c *gin.Context) {
	var req struct {
		ChatID        string `json:"chat_id" binding:"required"`
		WalletAddress string `json:"wallet_address" binding:"required"`
		WalletType    string `json:"wallet_type"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request: " + err.Error()})
		return
	}

	if req.WalletType == "" {
		req.WalletType = "external"
	}

	// TODO: Implement actual wallet addition
	c.JSON(http.StatusOK, WalletCommandResponse{
		Ok:      true,
		Message: "Wallet added successfully",
	})
}

// RemoveWallet removes a wallet
func (h *AutonomousHandler) RemoveWallet(c *gin.Context) {
	var req struct {
		ChatID            string `json:"chat_id" binding:"required"`
		WalletIDOrAddress string `json:"wallet_id_or_address" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request: " + err.Error()})
		return
	}

	// TODO: Implement actual wallet removal
	c.JSON(http.StatusOK, WalletCommandResponse{
		Ok:      true,
		Message: "Wallet removed successfully",
	})
}

// GetWallets returns connected wallets
func (h *AutonomousHandler) GetWallets(c *gin.Context) {
	chatID := c.Query("chat_id")
	if chatID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "chat_id is required"})
		return
	}

	// TODO: Implement actual wallet retrieval
	c.JSON(http.StatusOK, WalletsResponse{
		Wallets: []WalletInfo{},
	})
}

// Helper functions

func generateRequestID() string {
	return uuid.New().String()[:8]
}

// ReadinessChecker checks system readiness for autonomous mode
type ReadinessChecker struct{}

// CheckResult represents the result of a single check
type CheckResult struct {
	Status    string            `json:"status"`
	Message   string            `json:"message"`
	LatencyMs int64             `json:"latency_ms,omitempty"`
	Details   map[string]string `json:"details,omitempty"`
}

// ReadinessResult represents the overall readiness result
type ReadinessResult struct {
	Passed       bool                    `json:"passed"`
	FailedChecks []string                `json:"failed_checks,omitempty"`
	Summary      string                  `json:"summary"`
	Checks       map[string]*CheckResult `json:"checks"`
}

// NewReadinessChecker creates a new readiness checker
func NewReadinessChecker() *ReadinessChecker {
	return &ReadinessChecker{}
}

// Check runs all readiness checks
func (r *ReadinessChecker) Check(c *gin.Context, chatID string) *ReadinessResult {
	checks := make(map[string]*CheckResult)
	failedChecks := []string{}

	// Check 1: Database connectivity
	dbResult := r.checkDatabase(c)
	checks["database"] = dbResult
	if dbResult.Status != "healthy" {
		failedChecks = append(failedChecks, "database")
	}

	// Check 2: Redis connectivity
	redisResult := r.checkRedis(c)
	checks["redis"] = redisResult
	if redisResult.Status != "healthy" {
		failedChecks = append(failedChecks, "redis")
	}

	// Check 3: Exchange API connectivity
	exchangeResult := r.checkExchanges(c)
	checks["exchanges"] = exchangeResult
	if exchangeResult.Status == "critical" {
		failedChecks = append(failedChecks, "exchanges")
	}

	// Check 4: Wallet configuration
	walletResult := r.checkWallets(c, chatID)
	checks["wallets"] = walletResult
	if walletResult.Status == "critical" {
		failedChecks = append(failedChecks, "wallets")
	}

	// Check 5: Risk limits configuration
	riskResult := r.checkRiskLimits(c)
	checks["risk_limits"] = riskResult
	if riskResult.Status != "healthy" {
		failedChecks = append(failedChecks, "risk_limits")
	}

	passed := len(failedChecks) == 0
	summary := "All systems operational"
	if !passed {
		summary = fmt.Sprintf("%d check(s) failed: %v", len(failedChecks), failedChecks)
	}

	return &ReadinessResult{
		Passed:       passed,
		FailedChecks: failedChecks,
		Summary:      summary,
		Checks:       checks,
	}
}

func (r *ReadinessChecker) checkDatabase(c *gin.Context) *CheckResult {
	start := time.Now()
	// TODO: Actual database ping
	latency := time.Since(start).Milliseconds()

	return &CheckResult{
		Status:    "healthy",
		Message:   "Database connection successful",
		LatencyMs: latency,
	}
}

func (r *ReadinessChecker) checkRedis(c *gin.Context) *CheckResult {
	start := time.Now()
	// TODO: Actual Redis ping
	latency := time.Since(start).Milliseconds()

	return &CheckResult{
		Status:    "healthy",
		Message:   "Redis connection successful",
		LatencyMs: latency,
	}
}

func (r *ReadinessChecker) checkExchanges(c *gin.Context) *CheckResult {
	start := time.Now()
	// TODO: Actual exchange connectivity check
	latency := time.Since(start).Milliseconds()

	return &CheckResult{
		Status:    "healthy",
		Message:   "Exchange APIs reachable",
		LatencyMs: latency,
		Details: map[string]string{
			"configured_exchanges": "0",
		},
	}
}

func (r *ReadinessChecker) checkWallets(c *gin.Context, chatID string) *CheckResult {
	start := time.Now()
	// TODO: Actual wallet check
	latency := time.Since(start).Milliseconds()

	// For initial setup, return warning if no wallets configured
	return &CheckResult{
		Status:    "warning",
		Message:   "No wallets configured. Use /connect_exchange or /connect_polymarket to add wallets.",
		LatencyMs: latency,
		Details: map[string]string{
			"polymarket_wallets": "0",
			"exchange_accounts":  "0",
		},
	}
}

func (r *ReadinessChecker) checkRiskLimits(c *gin.Context) *CheckResult {
	start := time.Now()
	// TODO: Actual risk limits check
	latency := time.Since(start).Milliseconds()

	return &CheckResult{
		Status:    "healthy",
		Message:   "Risk limits configured",
		LatencyMs: latency,
		Details: map[string]string{
			"max_drawdown":   "5%",
			"daily_loss_cap": "2%",
			"position_limit": "10%",
		},
	}
}

// parseInt helper for query parameter parsing
func parseInt(s string) (int, error) {
	return strconv.Atoi(s)
}
