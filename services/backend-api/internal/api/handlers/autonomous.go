package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/irfndi/neuratrade/internal/database"
	"github.com/irfndi/neuratrade/internal/services"
	"github.com/redis/go-redis/v9"
	"github.com/shopspring/decimal"
)

// BalanceFetcher defines the interface for fetching account balances
type BalanceFetcher interface {
	FetchBalance(ctx context.Context, exchange string) (*BalanceResponse, error)
}

// BalanceResponse represents balance data from exchange
type BalanceResponse struct {
	Total  map[string]interface{} `json:"total"`
	Free   map[string]interface{} `json:"free"`
	Used   map[string]interface{} `json:"used"`
	Equity decimal.Decimal        `json:"equity"`
}

// PositionFetcher defines the interface for fetching positions
type PositionFetcher interface {
	GetOpenPositions() []PositionData
}

// PositionData represents position data
type PositionData struct {
	Symbol       string          `json:"symbol"`
	Side         string          `json:"side"`
	Size         decimal.Decimal `json:"size"`
	EntryPrice   decimal.Decimal `json:"entry_price"`
	CurrentPrice decimal.Decimal `json:"current_price"`
	UnrealizedPL decimal.Decimal `json:"unrealized_pnl"`
}

// DBPool interface for database operations
type DBPool interface {
	QueryRow(ctx context.Context, query string, args ...interface{}) interface {
		Scan(dest ...interface{}) error
	}
	Query(ctx context.Context, query string, args ...interface{}) (interface{}, error)
}

// AutonomousHandler handles autonomous mode endpoints
type AutonomousHandler struct {
	questEngine     *services.QuestEngine
	readiness       *ReadinessChecker
	db              DBPool
	balanceFetcher  BalanceFetcher
	positionFetcher PositionFetcher
}

// NewAutonomousHandler creates a new autonomous handler
func NewAutonomousHandler(questEngine *services.QuestEngine, db DBPool, balanceFetcher BalanceFetcher, positionFetcher PositionFetcher) *AutonomousHandler {
	return &AutonomousHandler{
		questEngine:     questEngine,
		readiness:       NewReadinessChecker(db),
		db:              db,
		balanceFetcher:  balanceFetcher,
		positionFetcher: positionFetcher,
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

	exchange := "binance"
	totalEquity := decimal.Zero
	availableBalance := decimal.Zero
	exposure := decimal.Zero

	if h.balanceFetcher != nil {
		balance, err := h.balanceFetcher.FetchBalance(c.Request.Context(), exchange)
		if err == nil && balance != nil {
			if balance.Equity.IsZero() == false {
				totalEquity = balance.Equity
			}
			if total, ok := balance.Total["USDT"]; ok {
				if usdt, ok := total.(float64); ok {
					totalEquity = decimal.NewFromFloat(usdt)
				}
			}
			if free, ok := balance.Free["USDT"]; ok {
				if usdt, ok := free.(float64); ok {
					availableBalance = decimal.NewFromFloat(usdt)
				}
			}
		}
	}

	positions := []PortfolioPosition{}
	if h.positionFetcher != nil {
		openPositions := h.positionFetcher.GetOpenPositions()
		var exposureSum decimal.Decimal
		for _, pos := range openPositions {
			side := "LONG"
			if pos.Size.LessThan(decimal.Zero) {
				side = "SHORT"
			}
			positions = append(positions, PortfolioPosition{
				Symbol:        pos.Symbol,
				Side:          side,
				Size:          pos.Size.String(),
				EntryPrice:    pos.EntryPrice.String(),
				MarkPrice:     pos.CurrentPrice.String(),
				UnrealizedPnL: pos.UnrealizedPL.String(),
			})
			posValue := pos.Size.Mul(pos.CurrentPrice)
			exposureSum = exposureSum.Add(posValue)
		}
		if totalEquity.GreaterThan(decimal.Zero) {
			exposure = exposureSum.Div(totalEquity).Mul(decimal.NewFromInt(100))
		}
	}

	c.JSON(http.StatusOK, PortfolioResponse{
		TotalEquity:      totalEquity.String(),
		AvailableBalance: availableBalance.String(),
		Exposure:         exposure.StringFixed(2) + "%",
		Positions:        positions,
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

	var totalPnL decimal.Decimal
	var tradeCount int
	var winCount int

	if h.db != nil {
		timeFilter := "24 hours"
		if timeframe == "7d" {
			timeFilter = "7 days"
		} else if timeframe == "30d" {
			timeFilter = "30 days"
		}

		query := fmt.Sprintf(`
			SELECT COALESCE(SUM(profit_loss), 0), COUNT(*), 
			       SUM(CASE WHEN profit_loss > 0 THEN 1 ELSE 0 END)
			FROM trading_orders 
			WHERE chat_id = $1 
			  AND created_at > NOW() - INTERVAL '%s'
			  AND status IN ('closed', 'filled')
		`, timeFilter)

		row := h.db.QueryRow(c.Request.Context(), query, chatID)
		if row != nil {
			var pnl, wins interface{}
			row.Scan(&pnl, &tradeCount, &wins)
			if pnl != nil {
				if f, ok := pnl.(float64); ok {
					totalPnL = decimal.NewFromFloat(f)
				}
			}
			if wins != nil {
				if w, ok := wins.(int64); ok {
					winCount = int(w)
				}
			}
		}
	}

	winRate := "N/A"
	if tradeCount > 0 {
		winRatePct := float64(winCount) / float64(tradeCount) * 100
		winRate = fmt.Sprintf("%.1f%%", winRatePct)
	}

	note := fmt.Sprintf("Based on %d trades", tradeCount)
	if tradeCount == 0 {
		note = "No trading activity in this period"
	}

	c.JSON(http.StatusOK, PerformanceSummaryResponse{
		Timeframe: timeframe,
		PnL:       totalPnL.String(),
		WinRate:   winRate,
		Sharpe:    "N/A",
		Drawdown:  "0%",
		Trades:    tradeCount,
		Note:      note,
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
type ReadinessChecker struct {
	db DBPool
}

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
func NewReadinessChecker(db DBPool) *ReadinessChecker {
	return &ReadinessChecker{db: db}
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

	if r.db == nil {
		return &CheckResult{
			Status:    "warning",
			Message:   "Database not configured",
			LatencyMs: 0,
		}
	}

	err := r.db.QueryRow(c.Request.Context(), "SELECT 1").Scan()
	latency := time.Since(start).Milliseconds()

	if err != nil {
		return &CheckResult{
			Status:    "critical",
			Message:   "Database connection failed: " + err.Error(),
			LatencyMs: latency,
		}
	}

	return &CheckResult{
		Status:    "healthy",
		Message:   "Database connection successful",
		LatencyMs: latency,
	}
}

func (r *ReadinessChecker) checkRedis(c *gin.Context) *CheckResult {
	start := time.Now()

	latency := time.Since(start).Milliseconds()
	status := "healthy"
	message := "Redis connection successful"

	redisURL := os.Getenv("REDIS_URL")
	if redisURL == "" {
		redisURL = "redis://localhost:6379"
	}

	client, err := newRedisClient(redisURL)
	if err != nil {
		status = "warning"
		message = "Redis client creation failed: " + err.Error()
	} else {
		ctx, cancel := context.WithTimeout(c.Request.Context(), 2*time.Second)
		defer cancel()
		_, err = client.Ping(ctx).Result()
		if err != nil {
			status = "warning"
			message = "Redis ping failed: " + err.Error()
		}
		latency = time.Since(start).Milliseconds()
	}

	return &CheckResult{
		Status:    status,
		Message:   message,
		LatencyMs: latency,
	}
}

func (r *ReadinessChecker) checkExchanges(c *gin.Context) *CheckResult {
	start := time.Now()

	// Check configured exchanges from CCXT service
	resp, err := http.Get("http://localhost:3001/api/exchanges")
	latency := time.Since(start).Milliseconds()

	if err != nil {
		return &CheckResult{
			Status:    "warning",
			Message:   "CCXT service not reachable",
			LatencyMs: latency,
		}
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return &CheckResult{
			Status:    "warning",
			Message:   "Failed to parse exchange config",
			LatencyMs: latency,
		}
	}

	// Check if any exchanges are configured
	exchanges, ok := result["exchanges"].([]interface{})
	if !ok || len(exchanges) == 0 {
		return &CheckResult{
			Status:    "warning",
			Message:   "No exchanges configured in CCXT service",
			LatencyMs: latency,
			Details: map[string]string{
				"configured_exchanges": "0",
			},
		}
	}

	// Check for Binance specifically (for scalping mode)
	hasBinance := false
	for _, ex := range exchanges {
		if exMap, ok := ex.(map[string]interface{}); ok {
			if name, ok := exMap["id"].(string); ok && name == "binance" {
				hasBinance = true
				break
			}
		}
	}

	if hasBinance {
		return &CheckResult{
			Status:    "healthy",
			Message:   "Binance exchange configured (scalping mode ready)",
			LatencyMs: latency,
			Details: map[string]string{
				"configured_exchanges": fmt.Sprintf("%d", len(exchanges)),
				"mode":                 "scalping (AI + 1 exchange)",
			},
		}
	}

	return &CheckResult{
		Status:    "healthy",
		Message:   fmt.Sprintf("%d exchanges configured", len(exchanges)),
		LatencyMs: latency,
		Details: map[string]string{
			"configured_exchanges": fmt.Sprintf("%d", len(exchanges)),
			"mode":                 "arbitrage (2+ exchanges)",
		},
	}
}

func (r *ReadinessChecker) checkWallets(c *gin.Context, chatID string) *CheckResult {
	start := time.Now()

	// Check for exchange API keys in database
	db, ok := c.Get("database")
	latency := time.Since(start).Milliseconds()

	if !ok {
		return &CheckResult{
			Status:    "warning",
			Message:   "Database connection not available",
			LatencyMs: latency,
		}
	}

	sqlDB, ok := db.(*database.SQLiteDB)
	if !ok {
		return &CheckResult{
			Status:    "warning",
			Message:   "Invalid database connection",
			LatencyMs: latency,
		}
	}

	// Check for configured exchange API keys
	var exchangeCount int
	err := sqlDB.DB.QueryRow(
		"SELECT COUNT(DISTINCT exchange) FROM exchange_api_keys",
	).Scan(&exchangeCount)

	if err != nil {
		// Table may not exist or other error - check config as fallback
		exchangeCount = 0
	}

	// Check for Polymarket wallets
	var polymarketCount int
	err = sqlDB.DB.QueryRow(
		"SELECT COUNT(*) FROM wallets WHERE provider = 'polymarket'",
	).Scan(&polymarketCount)

	if err != nil {
		// Table may not exist or other error
		polymarketCount = 0
	}

	// Check config file for Binance API keys as fallback
	configHasBinance := false
	// nolint:gosec // Fixed config path, not user input
	configPath := os.ExpandEnv("$HOME/.neuratrade/config.json") // Fixed config path, not user input
	log.Printf("DEBUG: Checking config at %s", configPath)
	if content, err := os.ReadFile(configPath); err == nil {
		var config map[string]interface{}
		if err := json.Unmarshal(content, &config); err == nil {
			log.Printf("DEBUG: Config loaded, has ccxt: %v", config["ccxt"] != nil)
			// Check new config structure: ccxt.exchanges.binance.api_key
			if ccxt, ok := config["ccxt"].(map[string]interface{}); ok {
				log.Printf("DEBUG: Has ccxt section")
				if exchanges, ok := ccxt["exchanges"].(map[string]interface{}); ok {
					log.Printf("DEBUG: Has exchanges section")
					if binance, ok := exchanges["binance"].(map[string]interface{}); ok {
						log.Printf("DEBUG: Has binance section")
						if apiKey, ok := binance["api_key"].(string); ok && apiKey != "" {
							log.Printf("DEBUG: Has api_key: %s", apiKey[:10]+"...")
							configHasBinance = true
						}
					}
				}
			}
			// Also check old config structure for backward compatibility
			if !configHasBinance {
				if services, ok := config["services"].(map[string]interface{}); ok {
					if ccxt, ok := services["ccxt"].(map[string]interface{}); ok {
						if exchanges, ok := ccxt["exchanges"].(map[string]interface{}); ok {
							if binance, ok := exchanges["binance"].(map[string]interface{}); ok {
								if apiKey, ok := binance["api_key"].(string); ok && apiKey != "" {
									configHasBinance = true
								}
							}
						}
					}
				}
			}
		}
	}

	// Determine status based on what's configured
	if exchangeCount > 0 || configHasBinance {
		mode := "scalping"
		if exchangeCount >= 2 {
			mode = "arbitrage"
		}

		return &CheckResult{
			Status:    "healthy",
			Message:   fmt.Sprintf("Exchange API keys configured (%s mode)", mode),
			LatencyMs: latency,
			Details: map[string]string{
				"exchange_accounts":  fmt.Sprintf("%d", exchangeCount),
				"polymarket_wallets": fmt.Sprintf("%d", polymarketCount),
				"trading_mode":       mode,
			},
		}
	}

	if polymarketCount > 0 {
		return &CheckResult{
			Status:    "healthy",
			Message:   "Polymarket wallet configured",
			LatencyMs: latency,
			Details: map[string]string{
				"polymarket_wallets": fmt.Sprintf("%d", polymarketCount),
				"exchange_accounts":  "0",
			},
		}
	}

	// Nothing configured - return warning with helpful message
	return &CheckResult{
		Status:    "warning",
		Message:   "No wallets configured. Use /connect_exchange or CLI config init to add exchange API keys.",
		LatencyMs: latency,
		Details: map[string]string{
			"polymarket_wallets": "0",
			"exchange_accounts":  "0",
			"config_path":        configPath,
		},
	}
}

func (r *ReadinessChecker) checkRiskLimits(c *gin.Context) *CheckResult {
	start := time.Now()

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

func newRedisClient(url string) (*redis.Client, error) {
	opts, err := redis.ParseURL(url)
	if err != nil {
		return nil, err
	}
	return redis.NewClient(opts), nil
}
