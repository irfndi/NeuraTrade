package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/irfndi/neuratrade/internal/database"
	"github.com/shopspring/decimal"
)

// PortfolioHandler handles portfolio operations for SQLite mode.
// It provides endpoints for portfolio data, performance metrics, summaries, and diagnostics.
type PortfolioHandler struct {
	db             *database.SQLiteDB
	ccxtServiceURL string
	httpClient     *http.Client
}

// NewPortfolioHandler creates a new SQLite portfolio handler.
//
// Parameters:
//   - db: SQLite database connection
//   - ccxtServiceURL: The URL of the CCXT service for health checks (optional)
//
// Returns:
//   - *PortfolioHandler: The initialized handler
func NewPortfolioHandler(db *database.SQLiteDB, ccxtServiceURL ...string) *PortfolioHandler {
	h := &PortfolioHandler{
		db: db,
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
	if len(ccxtServiceURL) > 0 && ccxtServiceURL[0] != "" {
		h.ccxtServiceURL = ccxtServiceURL[0]
	}
	return h
}

// ================== Request/Response Structs ==================

// PortfolioResponse represents the complete portfolio data.
type PortfolioResponse struct {
	Positions []Position       `json:"positions"`
	Trades    []TradeRecord    `json:"trades"`
	Summary   PortfolioSummary `json:"summary"`
	Timestamp time.Time        `json:"timestamp"`
}

// Position represents an open trading position.
type Position struct {
	ID            int             `json:"id"`
	QuestID       *int            `json:"quest_id,omitempty"`
	StrategyID    string          `json:"strategy_id"`
	Exchange      string          `json:"exchange"`
	Symbol        string          `json:"symbol"`
	Side          string          `json:"side"`
	EntryPrice    decimal.Decimal `json:"entry_price"`
	CurrentPrice  decimal.Decimal `json:"current_price,omitempty"`
	Size          decimal.Decimal `json:"size"`
	Fees          decimal.Decimal `json:"fees"`
	UnrealizedPNL decimal.Decimal `json:"unrealized_pnl,omitempty"`
	Status        string          `json:"status"`
	OpenedAt      time.Time       `json:"opened_at"`
}

// TradeRecord represents a completed or open trade.
type TradeRecord struct {
	ID         int             `json:"id"`
	QuestID    *int            `json:"quest_id,omitempty"`
	StrategyID string          `json:"strategy_id"`
	Exchange   string          `json:"exchange"`
	Symbol     string          `json:"symbol"`
	Side       string          `json:"side"`
	EntryPrice decimal.Decimal `json:"entry_price"`
	ExitPrice  decimal.Decimal `json:"exit_price,omitempty"`
	Size       decimal.Decimal `json:"size"`
	Fees       decimal.Decimal `json:"fees"`
	PNL        decimal.Decimal `json:"pnl,omitempty"`
	CostBasis  decimal.Decimal `json:"cost_basis,omitempty"`
	Status     string          `json:"status"`
	OpenedAt   time.Time       `json:"opened_at"`
	ClosedAt   *time.Time      `json:"closed_at,omitempty"`
}

// PortfolioSummary provides a quick overview of portfolio status.
type PortfolioSummary struct {
	TotalTrades   int             `json:"total_trades"`
	OpenPositions int             `json:"open_positions"`
	ClosedTrades  int             `json:"closed_trades"`
	TotalPNL      decimal.Decimal `json:"total_pnl"`
	TotalFees     decimal.Decimal `json:"total_fees"`
	TotalVolume   decimal.Decimal `json:"total_volume"`
	RealizedPNL   decimal.Decimal `json:"realized_pnl"`
	UnrealizedPNL decimal.Decimal `json:"unrealized_pnl,omitempty"`
}

// PerformanceResponse contains detailed performance metrics.
type PerformanceResponse struct {
	Metrics    PerformanceMetrics    `json:"metrics"`
	BySymbol   []SymbolPerformance   `json:"by_symbol"`
	ByExchange []ExchangePerformance `json:"by_exchange"`
	BySide     []SidePerformance     `json:"by_side"`
	Timestamp  time.Time             `json:"timestamp"`
}

// PerformanceMetrics contains aggregate trading performance data.
type PerformanceMetrics struct {
	TotalTrades      int             `json:"total_trades"`
	WinningTrades    int             `json:"winning_trades"`
	LosingTrades     int             `json:"losing_trades"`
	WinRate          decimal.Decimal `json:"win_rate"`
	AvgProfit        decimal.Decimal `json:"avg_profit"`
	AvgLoss          decimal.Decimal `json:"avg_loss"`
	ProfitFactor     decimal.Decimal `json:"profit_factor"`
	TotalPNL         decimal.Decimal `json:"total_pnl"`
	BestTrade        decimal.Decimal `json:"best_trade"`
	WorstTrade       decimal.Decimal `json:"worst_trade"`
	AvgTradeDuration time.Duration   `json:"avg_trade_duration,omitempty"`
	SharpeRatio      decimal.Decimal `json:"sharpe_ratio,omitempty"`
	MaxDrawdown      decimal.Decimal `json:"max_drawdown,omitempty"`
}

// SymbolPerformance shows metrics broken down by trading symbol.
type SymbolPerformance struct {
	Symbol      string          `json:"symbol"`
	TradeCount  int             `json:"trade_count"`
	WinCount    int             `json:"win_count"`
	LossCount   int             `json:"loss_count"`
	WinRate     decimal.Decimal `json:"win_rate"`
	TotalPNL    decimal.Decimal `json:"total_pnl"`
	TotalVolume decimal.Decimal `json:"total_volume"`
	AvgPNL      decimal.Decimal `json:"avg_pnl"`
}

// ExchangePerformance shows metrics broken down by exchange.
type ExchangePerformance struct {
	Exchange    string          `json:"exchange"`
	TradeCount  int             `json:"trade_count"`
	WinCount    int             `json:"win_count"`
	LossCount   int             `json:"loss_count"`
	WinRate     decimal.Decimal `json:"win_rate"`
	TotalPNL    decimal.Decimal `json:"total_pnl"`
	TotalVolume decimal.Decimal `json:"total_volume"`
	AvgPNL      decimal.Decimal `json:"avg_pnl"`
}

// SidePerformance shows metrics broken down by trade side (buy/sell).
type SidePerformance struct {
	Side        string          `json:"side"`
	TradeCount  int             `json:"trade_count"`
	WinCount    int             `json:"win_count"`
	LossCount   int             `json:"loss_count"`
	WinRate     decimal.Decimal `json:"win_rate"`
	TotalPNL    decimal.Decimal `json:"total_pnl"`
	TotalVolume decimal.Decimal `json:"total_volume"`
}

// SummaryResponse contains 24-hour trading summary.
type SummaryResponse struct {
	Period        string          `json:"period"`
	StartTime     time.Time       `json:"start_time"`
	EndTime       time.Time       `json:"end_time"`
	TradeCount    int             `json:"trade_count"`
	PNL           decimal.Decimal `json:"pnl"`
	PNLPercent    decimal.Decimal `json:"pnl_percent"`
	Volume        decimal.Decimal `json:"volume"`
	Fees          decimal.Decimal `json:"fees"`
	WinCount      int             `json:"win_count"`
	LossCount     int             `json:"loss_count"`
	WinRate       decimal.Decimal `json:"win_rate"`
	BestTrade     decimal.Decimal `json:"best_trade"`
	WorstTrade    decimal.Decimal `json:"worst_trade"`
	OpenPositions int             `json:"open_positions"`
}

// DoctorResponse contains system diagnostic information.
type DoctorResponse struct {
	Status          string                 `json:"status"`
	Mode            string                 `json:"mode"`
	Timestamp       time.Time              `json:"timestamp"`
	Checks          map[string]CheckResult `json:"checks"`
	Statistics      DBStatistics           `json:"statistics"`
	Recommendations []string               `json:"recommendations,omitempty"`
}

// CheckResult represents the result of a health check.
type CheckResult struct {
	Status  string `json:"status"`
	Message string `json:"message"`
	Latency string `json:"latency,omitempty"`
}

// DBStatistics contains database statistics.
type DBStatistics struct {
	TotalTrades  int    `json:"total_trades"`
	OpenTrades   int    `json:"open_trades"`
	ClosedTrades int    `json:"closed_trades"`
	TotalQuests  int    `json:"total_quests"`
	TotalUsers   int    `json:"total_users"`
	DatabaseSize string `json:"database_size,omitempty"`
	OldestTrade  string `json:"oldest_trade,omitempty"`
	NewestTrade  string `json:"newest_trade,omitempty"`
}

// ================== Handler Methods ==================

// GetPortfolio returns the user's complete portfolio with positions and trades.
//
// @Summary Get portfolio
// @Description Get user's portfolio including positions, trades, and summary
// @Tags portfolio
// @Produce json
// @Param chat_id query string false "Telegram chat ID for user identification"
// @Param limit query int false "Maximum number of trades to return" default(50)
// @Success 200 {object} PortfolioResponse
// @Failure 500 {object} ErrorResponse
// @Router /api/v1/portfolio [get]
func (h *PortfolioHandler) GetPortfolio(c *gin.Context) {
	ctx := c.Request.Context()

	// Get optional user filter
	chatID := c.Query("chat_id")
	limit := 50
	if l := c.Query("limit"); l != "" {
		if parsed, err := parsePositiveInt(l); err == nil && parsed > 0 {
			limit = parsed
		}
	}

	// Query all trades (no user filtering in SQLite mode for simplicity)
	trades, err := h.queryTrades(ctx, chatID, limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, ErrorResponse{
			Error:   "Failed to query trades",
			Message: err.Error(),
		})
		return
	}

	// Separate positions (open trades) from trade history
	var positions []Position
	var closedTrades []TradeRecord
	var totalPNL, totalFees, totalVolume decimal.Decimal

	for _, t := range trades {
		if t.Status == "open" {
			positions = append(positions, Position{
				ID:         t.ID,
				QuestID:    t.QuestID,
				StrategyID: t.StrategyID,
				Exchange:   t.Exchange,
				Symbol:     t.Symbol,
				Side:       t.Side,
				EntryPrice: t.EntryPrice,
				Size:       t.Size,
				Fees:       t.Fees,
				Status:     t.Status,
				OpenedAt:   t.OpenedAt,
			})
		} else {
			closedTrades = append(closedTrades, t)
		}
		totalPNL = totalPNL.Add(t.PNL)
		totalFees = totalFees.Add(t.Fees)
		totalVolume = totalVolume.Add(t.Size.Mul(t.EntryPrice))
	}

	// Calculate realized vs unrealized PNL
	realizedPNL := decimal.Zero
	for _, t := range closedTrades {
		realizedPNL = realizedPNL.Add(t.PNL)
	}

	summary := PortfolioSummary{
		TotalTrades:   len(trades),
		OpenPositions: len(positions),
		ClosedTrades:  len(closedTrades),
		TotalPNL:      totalPNL,
		TotalFees:     totalFees,
		TotalVolume:   totalVolume,
		RealizedPNL:   realizedPNL,
	}

	c.JSON(http.StatusOK, PortfolioResponse{
		Positions: positions,
		Trades:    closedTrades,
		Summary:   summary,
		Timestamp: time.Now(),
	})
}

// GetPerformance returns detailed performance metrics.
//
// @Summary Get performance metrics
// @Description Get comprehensive trading performance metrics including win rate, profit factor, etc.
// @Tags portfolio
// @Produce json
// @Param period query string false "Time period: 24h, 7d, 30d, all" default(all)
// @Success 200 {object} PerformanceResponse
// @Failure 500 {object} ErrorResponse
// @Router /api/v1/portfolio/performance [get]
func (h *PortfolioHandler) GetPerformance(c *gin.Context) {
	ctx := c.Request.Context()

	// Parse time period
	period := c.DefaultQuery("period", "all")
	since := parsePeriodToTime(period)

	// Query trades for the period
	trades, err := h.queryTradesSince(ctx, since)
	if err != nil {
		c.JSON(http.StatusInternalServerError, ErrorResponse{
			Error:   "Failed to query trades",
			Message: err.Error(),
		})
		return
	}

	// If no trades, return mock data
	if len(trades) == 0 {
		c.JSON(http.StatusOK, h.getMockPerformance())
		return
	}

	// Calculate metrics
	metrics := h.calculateMetrics(trades)
	bySymbol := h.calculateBySymbol(trades)
	byExchange := h.calculateByExchange(trades)
	bySide := h.calculateBySide(trades)

	c.JSON(http.StatusOK, PerformanceResponse{
		Metrics:    metrics,
		BySymbol:   bySymbol,
		ByExchange: byExchange,
		BySide:     bySide,
		Timestamp:  time.Now(),
	})
}

// GetSummary returns 24-hour trading summary.
//
// @Summary Get 24h summary
// @Description Get trading summary for the last 24 hours
// @Tags portfolio
// @Produce json
// @Success 200 {object} SummaryResponse
// @Failure 500 {object} ErrorResponse
// @Router /api/v1/portfolio/summary [get]
func (h *PortfolioHandler) GetSummary(c *gin.Context) {
	ctx := c.Request.Context()

	// Calculate 24h window
	endTime := time.Now()
	startTime := endTime.Add(-24 * time.Hour)

	// Query trades in the period
	trades, err := h.queryTradesSince(ctx, startTime)
	if err != nil {
		c.JSON(http.StatusInternalServerError, ErrorResponse{
			Error:   "Failed to query trades",
			Message: err.Error(),
		})
		return
	}

	// If no trades, return empty summary
	if len(trades) == 0 {
		c.JSON(http.StatusOK, SummaryResponse{
			Period:     "24h",
			StartTime:  startTime,
			EndTime:    endTime,
			TradeCount: 0,
			PNL:        decimal.Zero,
			PNLPercent: decimal.Zero,
			Volume:     decimal.Zero,
			Fees:       decimal.Zero,
			WinCount:   0,
			LossCount:  0,
			WinRate:    decimal.Zero,
		})
		return
	}

	// Calculate summary metrics
	var pnl, volume, fees decimal.Decimal
	var winCount, lossCount int
	var bestTrade, worstTrade decimal.Decimal

	for _, t := range trades {
		pnl = pnl.Add(t.PNL)
		fees = fees.Add(t.Fees)
		volume = volume.Add(t.Size.Mul(t.EntryPrice))

		if t.PNL.IsPositive() {
			winCount++
			if bestTrade.IsZero() || t.PNL.GreaterThan(bestTrade) {
				bestTrade = t.PNL
			}
		} else if t.PNL.IsNegative() {
			lossCount++
			if worstTrade.IsZero() || t.PNL.LessThan(worstTrade) {
				worstTrade = t.PNL
			}
		}
	}

	// Count open positions
	openCount := 0
	for _, t := range trades {
		if t.Status == "open" {
			openCount++
		}
	}

	// Calculate win rate
	winRate := decimal.Zero
	if len(trades) > 0 {
		winRate = decimal.NewFromInt(int64(winCount)).Div(decimal.NewFromInt(int64(len(trades)))).Mul(decimal.NewFromInt(100))
	}

	// Calculate PNL percent (simplified - would need account balance for accurate calculation)
	pnlPercent := decimal.Zero
	if !volume.IsZero() {
		pnlPercent = pnl.Div(volume).Mul(decimal.NewFromInt(100))
	}

	c.JSON(http.StatusOK, SummaryResponse{
		Period:        "24h",
		StartTime:     startTime,
		EndTime:       endTime,
		TradeCount:    len(trades),
		PNL:           pnl,
		PNLPercent:    pnlPercent,
		Volume:        volume,
		Fees:          fees,
		WinCount:      winCount,
		LossCount:     lossCount,
		WinRate:       winRate,
		BestTrade:     bestTrade,
		WorstTrade:    worstTrade,
		OpenPositions: openCount,
	})
}

// GetDoctor returns system diagnostics and health status.
//
// @Summary Get system diagnostics
// @Description Get comprehensive system health and diagnostics
// @Tags portfolio
// @Produce json
// @Success 200 {object} DoctorResponse
// @Router /api/v1/portfolio/doctor [get]
func (h *PortfolioHandler) GetDoctor(c *gin.Context) {
	ctx := c.Request.Context()

	checks := make(map[string]CheckResult)
	var hasIssues bool

	// Check database connectivity
	dbCheck := h.checkDatabase(ctx)
	checks["database"] = dbCheck
	if dbCheck.Status != "healthy" {
		hasIssues = true
	}

	// Check CCXT service (optional)
	ccxtCheck := h.checkCCXTService(ctx)
	checks["ccxt_service"] = ccxtCheck

	// Check Redis (optional in SQLite mode)
	redisCheck := CheckResult{
		Status:  "skipped",
		Message: "Redis check not applicable in SQLite mode",
	}
	checks["redis"] = redisCheck

	// Get database statistics
	stats := h.getDBStatistics(ctx)

	// Determine overall status
	status := "healthy"
	if hasIssues {
		status = "degraded"
	}

	// Generate recommendations
	recommendations := []string{
		"System running in SQLite mode - suitable for development and light usage",
		"Consider PostgreSQL for production deployments with high transaction volume",
	}

	if stats.TotalTrades == 0 {
		recommendations = append(recommendations, "No trades recorded yet - place orders to populate portfolio data")
	}

	c.JSON(http.StatusOK, DoctorResponse{
		Status:          status,
		Mode:            "sqlite",
		Timestamp:       time.Now(),
		Checks:          checks,
		Statistics:      stats,
		Recommendations: recommendations,
	})
}

// ================== Database Query Methods ==================

func (h *PortfolioHandler) queryTrades(ctx context.Context, chatID string, limit int) ([]TradeRecord, error) {
	if h.db == nil || h.db.DB == nil {
		return nil, fmt.Errorf("database not initialized")
	}

	query := `
		SELECT id, quest_id, strategy_id, exchange, symbol, side, entry_price, 
		       COALESCE(exit_price, 0), size, fees, COALESCE(pnl, 0), 
		       COALESCE(cost_basis, 0), status, opened_at, closed_at
		FROM trades
		ORDER BY opened_at DESC
		LIMIT ?
	`

	rows, err := h.db.DB.QueryContext(ctx, query, limit)
	if err != nil {
		return nil, fmt.Errorf("query trades: %w", err)
	}
	defer func() { _ = rows.Close() }()

	return h.scanTrades(rows)
}

func (h *PortfolioHandler) queryTradesSince(ctx context.Context, since time.Time) ([]TradeRecord, error) {
	if h.db == nil || h.db.DB == nil {
		return nil, fmt.Errorf("database not initialized")
	}

	query := `
		SELECT id, quest_id, strategy_id, exchange, symbol, side, entry_price, 
		       COALESCE(exit_price, 0), size, fees, COALESCE(pnl, 0), 
		       COALESCE(cost_basis, 0), status, opened_at, closed_at
		FROM trades
		WHERE opened_at >= ?
		ORDER BY opened_at DESC
	`

	rows, err := h.db.DB.QueryContext(ctx, query, since)
	if err != nil {
		return nil, fmt.Errorf("query trades since: %w", err)
	}
	defer func() { _ = rows.Close() }()

	return h.scanTrades(rows)
}

func (h *PortfolioHandler) scanTrades(rows *sql.Rows) ([]TradeRecord, error) {
	var trades []TradeRecord

	for rows.Next() {
		var t TradeRecord
		var questID sql.NullInt64
		var closedAt sql.NullTime

		err := rows.Scan(
			&t.ID,
			&questID,
			&t.StrategyID,
			&t.Exchange,
			&t.Symbol,
			&t.Side,
			&t.EntryPrice,
			&t.ExitPrice,
			&t.Size,
			&t.Fees,
			&t.PNL,
			&t.CostBasis,
			&t.Status,
			&t.OpenedAt,
			&closedAt,
		)
		if err != nil {
			continue
		}

		if questID.Valid {
			id := int(questID.Int64)
			t.QuestID = &id
		}
		if closedAt.Valid {
			t.ClosedAt = &closedAt.Time
		}

		trades = append(trades, t)
	}

	if err := rows.Err(); err != nil {
		return trades, fmt.Errorf("scan trades: %w", err)
	}

	return trades, nil
}

// ================== Calculation Methods ==================

func (h *PortfolioHandler) calculateMetrics(trades []TradeRecord) PerformanceMetrics {
	var totalPNL, totalWins, totalLosses decimal.Decimal
	var winCount, lossCount int
	var bestTrade, worstTrade decimal.Decimal
	var totalDuration time.Duration
	var closedCount int

	for _, t := range trades {
		if t.Status != "closed" || t.ClosedAt == nil {
			continue
		}

		closedCount++
		totalPNL = totalPNL.Add(t.PNL)

		// Track duration
		duration := t.ClosedAt.Sub(t.OpenedAt)
		totalDuration += duration

		if t.PNL.IsPositive() {
			winCount++
			totalWins = totalWins.Add(t.PNL)
			if bestTrade.IsZero() || t.PNL.GreaterThan(bestTrade) {
				bestTrade = t.PNL
			}
		} else if t.PNL.IsNegative() {
			lossCount++
			totalLosses = totalLosses.Add(t.PNL.Abs())
			if worstTrade.IsZero() || t.PNL.LessThan(worstTrade) {
				worstTrade = t.PNL
			}
		}
	}

	// Calculate averages
	avgProfit := decimal.Zero
	avgLoss := decimal.Zero
	if winCount > 0 {
		avgProfit = totalWins.Div(decimal.NewFromInt(int64(winCount)))
	}
	if lossCount > 0 {
		avgLoss = totalLosses.Div(decimal.NewFromInt(int64(lossCount)))
	}

	// Calculate win rate
	winRate := decimal.Zero
	if closedCount > 0 {
		winRate = decimal.NewFromInt(int64(winCount)).Div(decimal.NewFromInt(int64(closedCount))).Mul(decimal.NewFromInt(100))
	}

	// Calculate profit factor
	profitFactor := decimal.Zero
	if !totalLosses.IsZero() {
		profitFactor = totalWins.Div(totalLosses)
	} else if !totalWins.IsZero() {
		profitFactor = decimal.NewFromInt(999) // Cap at high value
	}

	// Calculate average duration
	var avgDuration time.Duration
	if closedCount > 0 {
		avgDuration = totalDuration / time.Duration(closedCount)
	}

	return PerformanceMetrics{
		TotalTrades:      closedCount,
		WinningTrades:    winCount,
		LosingTrades:     lossCount,
		WinRate:          winRate,
		AvgProfit:        avgProfit,
		AvgLoss:          avgLoss,
		ProfitFactor:     profitFactor,
		TotalPNL:         totalPNL,
		BestTrade:        bestTrade,
		WorstTrade:       worstTrade,
		AvgTradeDuration: avgDuration,
	}
}

func (h *PortfolioHandler) calculateBySymbol(trades []TradeRecord) []SymbolPerformance {
	symbolMap := make(map[string]*symbolStats)

	for _, t := range trades {
		if t.Status != "closed" {
			continue
		}

		stats, exists := symbolMap[t.Symbol]
		if !exists {
			stats = &symbolStats{}
			symbolMap[t.Symbol] = stats
		}

		stats.count++
		stats.totalPNL = stats.totalPNL.Add(t.PNL)
		stats.totalVolume = stats.totalVolume.Add(t.Size.Mul(t.EntryPrice))

		if t.PNL.IsPositive() {
			stats.wins++
		} else if t.PNL.IsNegative() {
			stats.losses++
		}
	}

	var result []SymbolPerformance
	for symbol, stats := range symbolMap {
		winRate := decimal.Zero
		if stats.count > 0 {
			winRate = decimal.NewFromInt(int64(stats.wins)).Div(decimal.NewFromInt(int64(stats.count))).Mul(decimal.NewFromInt(100))
		}
		avgPNL := decimal.Zero
		if stats.count > 0 {
			avgPNL = stats.totalPNL.Div(decimal.NewFromInt(int64(stats.count)))
		}

		result = append(result, SymbolPerformance{
			Symbol:      symbol,
			TradeCount:  stats.count,
			WinCount:    stats.wins,
			LossCount:   stats.losses,
			WinRate:     winRate,
			TotalPNL:    stats.totalPNL,
			TotalVolume: stats.totalVolume,
			AvgPNL:      avgPNL,
		})
	}

	return result
}

func (h *PortfolioHandler) calculateByExchange(trades []TradeRecord) []ExchangePerformance {
	exchangeMap := make(map[string]*symbolStats)

	for _, t := range trades {
		if t.Status != "closed" {
			continue
		}

		stats, exists := exchangeMap[t.Exchange]
		if !exists {
			stats = &symbolStats{}
			exchangeMap[t.Exchange] = stats
		}

		stats.count++
		stats.totalPNL = stats.totalPNL.Add(t.PNL)
		stats.totalVolume = stats.totalVolume.Add(t.Size.Mul(t.EntryPrice))

		if t.PNL.IsPositive() {
			stats.wins++
		} else if t.PNL.IsNegative() {
			stats.losses++
		}
	}

	var result []ExchangePerformance
	for exchange, stats := range exchangeMap {
		winRate := decimal.Zero
		if stats.count > 0 {
			winRate = decimal.NewFromInt(int64(stats.wins)).Div(decimal.NewFromInt(int64(stats.count))).Mul(decimal.NewFromInt(100))
		}
		avgPNL := decimal.Zero
		if stats.count > 0 {
			avgPNL = stats.totalPNL.Div(decimal.NewFromInt(int64(stats.count)))
		}

		result = append(result, ExchangePerformance{
			Exchange:    exchange,
			TradeCount:  stats.count,
			WinCount:    stats.wins,
			LossCount:   stats.losses,
			WinRate:     winRate,
			TotalPNL:    stats.totalPNL,
			TotalVolume: stats.totalVolume,
			AvgPNL:      avgPNL,
		})
	}

	return result
}

func (h *PortfolioHandler) calculateBySide(trades []TradeRecord) []SidePerformance {
	sideMap := make(map[string]*symbolStats)

	for _, t := range trades {
		if t.Status != "closed" {
			continue
		}

		stats, exists := sideMap[t.Side]
		if !exists {
			stats = &symbolStats{}
			sideMap[t.Side] = stats
		}

		stats.count++
		stats.totalPNL = stats.totalPNL.Add(t.PNL)
		stats.totalVolume = stats.totalVolume.Add(t.Size.Mul(t.EntryPrice))

		if t.PNL.IsPositive() {
			stats.wins++
		} else if t.PNL.IsNegative() {
			stats.losses++
		}
	}

	var result []SidePerformance
	for side, stats := range sideMap {
		winRate := decimal.Zero
		if stats.count > 0 {
			winRate = decimal.NewFromInt(int64(stats.wins)).Div(decimal.NewFromInt(int64(stats.count))).Mul(decimal.NewFromInt(100))
		}

		result = append(result, SidePerformance{
			Side:        side,
			TradeCount:  stats.count,
			WinCount:    stats.wins,
			LossCount:   stats.losses,
			WinRate:     winRate,
			TotalPNL:    stats.totalPNL,
			TotalVolume: stats.totalVolume,
		})
	}

	return result
}

type symbolStats struct {
	count       int
	wins        int
	losses      int
	totalPNL    decimal.Decimal
	totalVolume decimal.Decimal
}

// ================== Health Check Methods ==================

func (h *PortfolioHandler) checkDatabase(ctx context.Context) CheckResult {
	start := time.Now()

	if h.db == nil || h.db.DB == nil {
		return CheckResult{
			Status:  "unhealthy",
			Message: "Database connection not initialized",
		}
	}

	if err := h.db.HealthCheck(ctx); err != nil {
		return CheckResult{
			Status:  "unhealthy",
			Message: fmt.Sprintf("Database health check failed: %v", err),
		}
	}

	latency := time.Since(start)
	return CheckResult{
		Status:  "healthy",
		Message: "SQLite database connected and responsive",
		Latency: latency.String(),
	}
}

func (h *PortfolioHandler) checkCCXTService(ctx context.Context) CheckResult {
	if h.ccxtServiceURL == "" {
		return CheckResult{
			Status:  "skipped",
			Message: "CCXT service URL not configured",
		}
	}

	start := time.Now()
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "GET", h.ccxtServiceURL+"/health", nil)
	if err != nil {
		return CheckResult{
			Status:  "unhealthy",
			Message: fmt.Sprintf("Failed to create health check request: %v", err),
		}
	}

	resp, err := h.httpClient.Do(req)
	if err != nil {
		return CheckResult{
			Status:  "unhealthy",
			Message: fmt.Sprintf("CCXT service unavailable: %v", err),
		}
	}
	defer func() { _ = resp.Body.Close() }()

	latency := time.Since(start)

	if resp.StatusCode == http.StatusOK {
		return CheckResult{
			Status:  "healthy",
			Message: "CCXT service available",
			Latency: latency.String(),
		}
	}

	return CheckResult{
		Status:  "degraded",
		Message: fmt.Sprintf("CCXT service returned status %d", resp.StatusCode),
		Latency: latency.String(),
	}
}

func (h *PortfolioHandler) getDBStatistics(ctx context.Context) DBStatistics {
	stats := DBStatistics{}

	if h.db == nil || h.db.DB == nil {
		return stats
	}

	// Get trade counts (errors ignored as these are non-critical statistics)
	_ = h.db.DB.QueryRowContext(ctx, "SELECT COUNT(*) FROM trades").Scan(&stats.TotalTrades)
	_ = h.db.DB.QueryRowContext(ctx, "SELECT COUNT(*) FROM trades WHERE status = 'open'").Scan(&stats.OpenTrades)
	_ = h.db.DB.QueryRowContext(ctx, "SELECT COUNT(*) FROM trades WHERE status = 'closed'").Scan(&stats.ClosedTrades)
	_ = h.db.DB.QueryRowContext(ctx, "SELECT COUNT(*) FROM quests").Scan(&stats.TotalQuests)
	_ = h.db.DB.QueryRowContext(ctx, "SELECT COUNT(*) FROM users").Scan(&stats.TotalUsers)

	// Get date range
	var oldest, newest sql.NullString
	_ = h.db.DB.QueryRowContext(ctx, "SELECT MIN(opened_at) FROM trades").Scan(&oldest)
	_ = h.db.DB.QueryRowContext(ctx, "SELECT MAX(opened_at) FROM trades").Scan(&newest)

	if oldest.Valid {
		stats.OldestTrade = oldest.String
	}
	if newest.Valid {
		stats.NewestTrade = newest.String
	}

	return stats
}

// ================== Helper Methods ==================

func (h *PortfolioHandler) getMockPerformance() PerformanceResponse {
	return PerformanceResponse{
		Metrics: PerformanceMetrics{
			TotalTrades:   0,
			WinningTrades: 0,
			LosingTrades:  0,
			WinRate:       decimal.Zero,
			AvgProfit:     decimal.Zero,
			AvgLoss:       decimal.Zero,
			ProfitFactor:  decimal.Zero,
			TotalPNL:      decimal.Zero,
			BestTrade:     decimal.Zero,
			WorstTrade:    decimal.Zero,
		},
		BySymbol:   []SymbolPerformance{},
		ByExchange: []ExchangePerformance{},
		BySide:     []SidePerformance{},
		Timestamp:  time.Now(),
	}
}

func parsePeriodToTime(period string) time.Time {
	switch period {
	case "24h":
		return time.Now().Add(-24 * time.Hour)
	case "7d":
		return time.Now().Add(-7 * 24 * time.Hour)
	case "30d":
		return time.Now().AddDate(0, 0, -30)
	default:
		return time.Time{} // All time
	}
}

func parsePositiveInt(s string) (int, error) {
	var result int
	_, err := fmt.Sscanf(s, "%d", &result)
	if err != nil {
		return 0, err
	}
	if result <= 0 {
		return 0, fmt.Errorf("value must be positive")
	}
	return result, nil
}
