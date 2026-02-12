package services

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/getsentry/sentry-go"

	"github.com/irfandi/celebrum-ai-go/internal/database"
	"github.com/irfandi/celebrum-ai-go/internal/models"
	"github.com/irfandi/celebrum-ai-go/internal/observability"
	"github.com/irfandi/celebrum-ai-go/internal/telemetry"
	"github.com/shopspring/decimal"
)

// BacktestConfig contains configuration for a backtest run.
type BacktestConfig struct {
	StartDate       time.Time       `json:"start_date"`
	EndDate         time.Time       `json:"end_date"`
	InitialCapital  decimal.Decimal `json:"initial_capital"`
	Symbols         []string        `json:"symbols,omitempty"`    // Empty = all symbols
	Exchanges       []string        `json:"exchanges,omitempty"`  // Empty = all exchanges
	MinAPY          decimal.Decimal `json:"min_apy"`              // Minimum APY to consider
	MaxRiskScore    decimal.Decimal `json:"max_risk_score"`       // Maximum risk score to accept
	PositionSizing  string          `json:"position_sizing"`      // "kelly", "fixed", "risk_adjusted"
	FixedSize       decimal.Decimal `json:"fixed_size,omitempty"` // For fixed position sizing
	MaxPositions    int             `json:"max_positions"`        // Max concurrent positions
	FundingFee      decimal.Decimal `json:"funding_fee"`          // Estimated funding collection fee
	TradingFee      decimal.Decimal `json:"trading_fee"`          // Per-trade fee percentage
	Slippage        decimal.Decimal `json:"slippage"`             // Expected slippage percentage
	HoldingPeriod   time.Duration   `json:"holding_period"`       // Target holding period
	ReinvestProfits bool            `json:"reinvest_profits"`     // Whether to compound returns
}

// BacktestResult contains the results of a backtest run.
type BacktestResult struct {
	Config           BacktestConfig  `json:"config"`
	TotalReturn      decimal.Decimal `json:"total_return"`            // Total percentage return
	TotalPnL         decimal.Decimal `json:"total_pnl"`               // Absolute profit/loss
	SharpeRatio      decimal.Decimal `json:"sharpe_ratio"`            // Risk-adjusted return
	SortinoRatio     decimal.Decimal `json:"sortino_ratio"`           // Downside risk-adjusted return
	MaxDrawdown      decimal.Decimal `json:"max_drawdown"`            // Maximum portfolio decline
	MaxDrawdownDate  time.Time       `json:"max_drawdown_date"`       // When max drawdown occurred
	WinRate          decimal.Decimal `json:"win_rate"`                // Percentage of winning trades
	LossRate         decimal.Decimal `json:"loss_rate"`               // Percentage of losing trades
	ProfitFactor     decimal.Decimal `json:"profit_factor"`           // Gross profit / Gross loss
	TotalTrades      int             `json:"total_trades"`            // Number of trades executed
	WinningTrades    int             `json:"winning_trades"`          // Number of profitable trades
	LosingTrades     int             `json:"losing_trades"`           // Number of losing trades
	AvgWin           decimal.Decimal `json:"avg_win"`                 // Average winning trade
	AvgLoss          decimal.Decimal `json:"avg_loss"`                // Average losing trade
	AvgHoldingTime   time.Duration   `json:"avg_holding_time"`        // Average position duration
	BestTrade        *BacktestTrade  `json:"best_trade,omitempty"`    // Most profitable trade
	WorstTrade       *BacktestTrade  `json:"worst_trade,omitempty"`   // Least profitable trade
	DailyReturns     []DailyReturn   `json:"daily_returns,omitempty"` // Day-by-day returns
	EquityCurve      []EquityPoint   `json:"equity_curve,omitempty"`  // Portfolio value over time
	TradesBySymbol   map[string]int  `json:"trades_by_symbol"`        // Trade count per symbol
	TradesByExchange map[string]int  `json:"trades_by_exchange"`      // Trade count per exchange
	StartedAt        time.Time       `json:"started_at"`
	CompletedAt      time.Time       `json:"completed_at"`
	Duration         time.Duration   `json:"duration"`
}

// BacktestTrade represents a simulated trade during backtesting.
type BacktestTrade struct {
	Symbol          string          `json:"symbol"`
	LongExchange    string          `json:"long_exchange"`
	ShortExchange   string          `json:"short_exchange"`
	EntryTime       time.Time       `json:"entry_time"`
	ExitTime        time.Time       `json:"exit_time"`
	PositionSize    decimal.Decimal `json:"position_size"`
	EntryAPY        decimal.Decimal `json:"entry_apy"`
	EntryRiskScore  decimal.Decimal `json:"entry_risk_score"`
	FundingReceived decimal.Decimal `json:"funding_received"`
	TradingFees     decimal.Decimal `json:"trading_fees"`
	Slippage        decimal.Decimal `json:"slippage"`
	GrossPnL        decimal.Decimal `json:"gross_pnl"`
	NetPnL          decimal.Decimal `json:"net_pnl"`
	ReturnPct       decimal.Decimal `json:"return_pct"`
	HoldingTime     time.Duration   `json:"holding_time"`
}

// DailyReturn represents the return for a single day.
type DailyReturn struct {
	Date       time.Time       `json:"date"`
	Return     decimal.Decimal `json:"return"`      // Daily percentage return
	PnL        decimal.Decimal `json:"pnl"`         // Daily absolute PnL
	Equity     decimal.Decimal `json:"equity"`      // End-of-day portfolio value
	TradeCount int             `json:"trade_count"` // Trades opened that day
}

// EquityPoint represents portfolio value at a point in time.
type EquityPoint struct {
	Timestamp time.Time       `json:"timestamp"`
	Equity    decimal.Decimal `json:"equity"`
}

// Backtester provides functionality to backtest arbitrage strategies.
type Backtester struct {
	db         *database.PostgresDB
	calculator *FuturesArbitrageCalculator
	mu         sync.Mutex
}

// NewBacktester creates a new backtester instance.
func NewBacktester(db *database.PostgresDB) *Backtester {
	return &Backtester{
		db:         db,
		calculator: NewFuturesArbitrageCalculator(),
	}
}

// RunBacktest executes a backtest simulation with the given configuration.
func (b *Backtester) RunBacktest(ctx context.Context, config BacktestConfig) (*BacktestResult, error) {
	spanCtx, span := observability.StartSpanWithTags(ctx, observability.SpanOpArbitrage, "Backtester.RunBacktest", map[string]string{
		"start_date":      config.StartDate.Format(time.RFC3339),
		"end_date":        config.EndDate.Format(time.RFC3339),
		"position_sizing": config.PositionSizing,
	})
	defer func() {
		observability.RecoverAndCapture(spanCtx, "RunBacktest")
	}()

	b.mu.Lock()
	defer b.mu.Unlock()

	startTime := time.Now()
	observability.AddBreadcrumb(spanCtx, "backtest", "Starting backtest simulation", sentry.LevelInfo)

	// Validate configuration
	if err := b.validateConfig(config); err != nil {
		observability.CaptureExceptionWithContext(spanCtx, err, "backtest_validation", nil)
		observability.FinishSpan(span, err)
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	// Initialize result
	result := &BacktestResult{
		Config:           config,
		DailyReturns:     make([]DailyReturn, 0),
		EquityCurve:      make([]EquityPoint, 0),
		TradesBySymbol:   make(map[string]int),
		TradesByExchange: make(map[string]int),
		StartedAt:        startTime,
	}

	// Fetch historical opportunities from database
	opportunities, err := b.fetchHistoricalOpportunities(spanCtx, config)
	if err != nil {
		observability.CaptureExceptionWithContext(spanCtx, err, "fetch_historical_data", map[string]interface{}{
			"start_date": config.StartDate,
			"end_date":   config.EndDate,
		})
		observability.FinishSpan(span, err)
		return nil, fmt.Errorf("failed to fetch historical data: %w", err)
	}

	if len(opportunities) == 0 {
		err := fmt.Errorf("no historical opportunities found for the given criteria")
		observability.FinishSpan(span, err)
		return nil, err
	}

	span.SetData("opportunities_found", len(opportunities))
	telemetry.Logger().Info("Backtesting",
		"start_date", config.StartDate,
		"end_date", config.EndDate,
		"opportunities", len(opportunities))

	// Run simulation
	trades, equityCurve := b.simulateTrades(config, opportunities)
	span.SetData("trades_executed", len(trades))

	// Calculate results
	b.calculateResults(result, trades, equityCurve, config)

	result.CompletedAt = time.Now()
	result.Duration = result.CompletedAt.Sub(result.StartedAt)

	observability.AddBreadcrumbWithData(spanCtx, "backtest", "Backtest completed", sentry.LevelInfo, map[string]interface{}{
		"trades":       len(trades),
		"duration_ms":  result.Duration.Milliseconds(),
		"total_return": result.TotalReturn.String(),
	})
	observability.FinishSpan(span, nil)

	return result, nil
}

// validateConfig validates the backtest configuration.
func (b *Backtester) validateConfig(config BacktestConfig) error {
	if config.StartDate.IsZero() {
		return fmt.Errorf("start_date is required")
	}
	if config.EndDate.IsZero() {
		return fmt.Errorf("end_date is required")
	}
	if config.StartDate.After(config.EndDate) {
		return fmt.Errorf("start_date must be before end_date")
	}
	if config.InitialCapital.IsZero() || config.InitialCapital.IsNegative() {
		return fmt.Errorf("initial_capital must be positive")
	}
	if config.PositionSizing == "" {
		config.PositionSizing = "kelly"
	}
	if config.MaxPositions <= 0 {
		config.MaxPositions = 5
	}
	if config.TradingFee.IsZero() {
		config.TradingFee = decimal.NewFromFloat(0.001) // 0.1% default
	}
	if config.HoldingPeriod == 0 {
		config.HoldingPeriod = 8 * time.Hour // Default to one funding period
	}
	return nil
}

// fetchHistoricalOpportunities retrieves historical arbitrage opportunities from the database.
func (b *Backtester) fetchHistoricalOpportunities(
	ctx context.Context,
	config BacktestConfig,
) ([]models.FuturesArbitrageOpportunity, error) {
	if b.db == nil {
		return nil, fmt.Errorf("database connection is nil")
	}

	query := `
		SELECT id, symbol, base_currency, quote_currency,
			   long_exchange, short_exchange,
			   long_funding_rate, short_funding_rate, net_funding_rate,
			   funding_interval, long_mark_price, short_mark_price,
			   price_difference, price_difference_percentage,
			   hourly_rate, daily_rate, apy,
			   estimated_profit_8h, estimated_profit_daily,
			   estimated_profit_weekly, estimated_profit_monthly,
			   risk_score, volatility_score, liquidity_score,
			   recommended_position_size, max_leverage, recommended_leverage,
			   stop_loss_percentage, min_position_size, max_position_size,
			   optimal_position_size, detected_at, expires_at,
			   next_funding_time, time_to_next_funding, is_active,
			   market_trend, volume_24h, open_interest
		FROM futures_arbitrage_opportunities
		WHERE detected_at >= $1 AND detected_at <= $2
		  AND ($3::text[] IS NULL OR symbol = ANY($3))
		  AND ($4::text[] IS NULL OR long_exchange = ANY($4) OR short_exchange = ANY($4))
		  AND apy >= $5
		  AND risk_score <= $6
		ORDER BY detected_at ASC
	`

	var symbols, exchanges interface{}
	if len(config.Symbols) > 0 {
		symbols = config.Symbols
	}
	if len(config.Exchanges) > 0 {
		exchanges = config.Exchanges
	}

	rows, err := b.db.Pool.Query(ctx, query,
		config.StartDate,
		config.EndDate,
		symbols,
		exchanges,
		config.MinAPY,
		config.MaxRiskScore,
	)
	if err != nil {
		return nil, fmt.Errorf("query failed: %w", err)
	}
	defer rows.Close()

	var opportunities []models.FuturesArbitrageOpportunity
	for rows.Next() {
		var opp models.FuturesArbitrageOpportunity
		err := rows.Scan(
			&opp.ID, &opp.Symbol, &opp.BaseCurrency, &opp.QuoteCurrency,
			&opp.LongExchange, &opp.ShortExchange,
			&opp.LongFundingRate, &opp.ShortFundingRate, &opp.NetFundingRate,
			&opp.FundingInterval, &opp.LongMarkPrice, &opp.ShortMarkPrice,
			&opp.PriceDifference, &opp.PriceDifferencePercentage,
			&opp.HourlyRate, &opp.DailyRate, &opp.APY,
			&opp.EstimatedProfit8h, &opp.EstimatedProfitDaily,
			&opp.EstimatedProfitWeekly, &opp.EstimatedProfitMonthly,
			&opp.RiskScore, &opp.VolatilityScore, &opp.LiquidityScore,
			&opp.RecommendedPositionSize, &opp.MaxLeverage, &opp.RecommendedLeverage,
			&opp.StopLossPercentage, &opp.MinPositionSize, &opp.MaxPositionSize,
			&opp.OptimalPositionSize, &opp.DetectedAt, &opp.ExpiresAt,
			&opp.NextFundingTime, &opp.TimeToNextFunding, &opp.IsActive,
			&opp.MarketTrend, &opp.Volume24h, &opp.OpenInterest,
		)
		if err != nil {
			telemetry.Logger().Warn("Failed to scan opportunity", "error", err)
			continue
		}
		opportunities = append(opportunities, opp)
	}

	return opportunities, nil
}

// simulateTrades simulates trade execution based on historical opportunities.
func (b *Backtester) simulateTrades(
	config BacktestConfig,
	opportunities []models.FuturesArbitrageOpportunity,
) ([]BacktestTrade, []EquityPoint) {
	trades := make([]BacktestTrade, 0)
	equityCurve := make([]EquityPoint, 0)

	currentEquity := config.InitialCapital
	openPositions := make(map[string]*BacktestTrade) // symbol -> trade

	// Record initial equity
	equityCurve = append(equityCurve, EquityPoint{
		Timestamp: config.StartDate,
		Equity:    currentEquity,
	})

	for _, opp := range opportunities {
		// Skip if max positions reached
		if len(openPositions) >= config.MaxPositions {
			// Check if we can close any positions
			for symbol, trade := range openPositions {
				if opp.DetectedAt.Sub(trade.EntryTime) >= config.HoldingPeriod {
					// Close position
					closedTrade := b.closeTrade(trade, opp.DetectedAt, config)
					trades = append(trades, *closedTrade)
					delete(openPositions, symbol)

					// Update equity
					currentEquity = currentEquity.Add(closedTrade.NetPnL)
					equityCurve = append(equityCurve, EquityPoint{
						Timestamp: opp.DetectedAt,
						Equity:    currentEquity,
					})
				}
			}
		}

		// Skip if position already open for this symbol
		if _, exists := openPositions[opp.Symbol]; exists {
			continue
		}

		// Skip if not enough equity for minimum position
		minPosition := decimal.NewFromInt(100) // Minimum $100
		if currentEquity.LessThan(minPosition) {
			continue
		}

		// Determine position size
		positionSize := b.calculatePositionSize(config, currentEquity, opp)
		if positionSize.IsZero() || positionSize.IsNegative() {
			continue
		}

		// Cap position size at available equity
		if positionSize.GreaterThan(currentEquity) {
			positionSize = currentEquity.Mul(decimal.NewFromFloat(0.5)) // Max 50% per trade
		}

		// Open new position
		trade := &BacktestTrade{
			Symbol:         opp.Symbol,
			LongExchange:   opp.LongExchange,
			ShortExchange:  opp.ShortExchange,
			EntryTime:      opp.DetectedAt,
			PositionSize:   positionSize,
			EntryAPY:       opp.APY,
			EntryRiskScore: opp.RiskScore,
		}

		// Calculate entry fees
		trade.TradingFees = positionSize.Mul(config.TradingFee).Mul(decimal.NewFromInt(2)) // Entry + exit
		trade.Slippage = positionSize.Mul(config.Slippage).Mul(decimal.NewFromInt(2))

		openPositions[opp.Symbol] = trade
	}

	// Close any remaining open positions at end date
	for _, trade := range openPositions {
		closedTrade := b.closeTrade(trade, config.EndDate, config)
		trades = append(trades, *closedTrade)

		currentEquity = currentEquity.Add(closedTrade.NetPnL)
		equityCurve = append(equityCurve, EquityPoint{
			Timestamp: config.EndDate,
			Equity:    currentEquity,
		})
	}

	return trades, equityCurve
}

// calculatePositionSize determines position size based on strategy.
func (b *Backtester) calculatePositionSize(
	config BacktestConfig,
	equity decimal.Decimal,
	opp models.FuturesArbitrageOpportunity,
) decimal.Decimal {
	switch config.PositionSizing {
	case "fixed":
		if !config.FixedSize.IsZero() {
			return config.FixedSize
		}
		return equity.Mul(decimal.NewFromFloat(0.1)) // Default 10%

	case "risk_adjusted":
		// Scale position based on risk score (lower risk = larger position)
		riskFactor := decimal.NewFromInt(100).Sub(opp.RiskScore).Div(decimal.NewFromInt(100))
		return equity.Mul(decimal.NewFromFloat(0.2)).Mul(riskFactor)

	case "kelly":
		fallthrough
	default:
		// Use recommended position size from opportunity if available
		if !opp.RecommendedPositionSize.IsZero() {
			return opp.RecommendedPositionSize
		}
		// Otherwise use simple Kelly-inspired sizing
		apyFloat, _ := opp.APY.Float64()
		riskFloat, _ := opp.RiskScore.Float64()

		// Higher APY = higher allocation, higher risk = lower allocation
		winProb := 0.6 + (apyFloat-5)/100 // Base 60% + bonus for high APY
		if winProb > 0.8 {
			winProb = 0.8
		}
		riskAdjustment := (100 - riskFloat) / 100

		kellyFraction := winProb * riskAdjustment * 0.25 // Quarter Kelly for safety
		return equity.Mul(decimal.NewFromFloat(kellyFraction))
	}
}

// closeTrade calculates the PnL for a closed trade.
func (b *Backtester) closeTrade(
	trade *BacktestTrade,
	exitTime time.Time,
	config BacktestConfig,
) *BacktestTrade {
	trade.ExitTime = exitTime
	trade.HoldingTime = exitTime.Sub(trade.EntryTime)

	// Calculate funding received
	// Calculate funding received

	// Estimate funding received based on entry APY
	hourlyRate := trade.EntryAPY.Div(decimal.NewFromFloat(365 * 24 * 100))
	trade.FundingReceived = trade.PositionSize.Mul(hourlyRate).Mul(
		decimal.NewFromFloat(trade.HoldingTime.Hours()),
	)

	// Subtract funding fee
	trade.FundingReceived = trade.FundingReceived.Sub(
		trade.FundingReceived.Mul(config.FundingFee),
	)

	// Calculate PnL
	trade.GrossPnL = trade.FundingReceived
	trade.NetPnL = trade.GrossPnL.Sub(trade.TradingFees).Sub(trade.Slippage)

	// Calculate return percentage
	if !trade.PositionSize.IsZero() {
		trade.ReturnPct = trade.NetPnL.Div(trade.PositionSize).Mul(decimal.NewFromInt(100))
	}

	return trade
}

// calculateResults computes the final backtest statistics.
func (b *Backtester) calculateResults(
	result *BacktestResult,
	trades []BacktestTrade,
	equityCurve []EquityPoint,
	config BacktestConfig,
) {
	if len(trades) == 0 {
		return
	}

	result.TotalTrades = len(trades)
	result.EquityCurve = equityCurve

	var (
		totalPnL     = decimal.Zero
		grossProfit  = decimal.Zero
		grossLoss    = decimal.Zero
		totalWinPnL  = decimal.Zero
		totalLossPnL = decimal.Zero
		totalTime    time.Duration
		returns      = make([]float64, 0)
	)

	for i, trade := range trades {
		totalPnL = totalPnL.Add(trade.NetPnL)
		totalTime += trade.HoldingTime

		// Track trades by symbol and exchange
		result.TradesBySymbol[trade.Symbol]++
		result.TradesByExchange[trade.LongExchange]++
		result.TradesByExchange[trade.ShortExchange]++

		// Track wins and losses
		if trade.NetPnL.IsPositive() {
			result.WinningTrades++
			grossProfit = grossProfit.Add(trade.NetPnL)
			totalWinPnL = totalWinPnL.Add(trade.NetPnL)
		} else if trade.NetPnL.IsNegative() {
			result.LosingTrades++
			grossLoss = grossLoss.Add(trade.NetPnL.Abs())
			totalLossPnL = totalLossPnL.Add(trade.NetPnL.Abs())
		}

		// Track best and worst trades
		if result.BestTrade == nil || trade.NetPnL.GreaterThan(result.BestTrade.NetPnL) {
			tradeCopy := trades[i]
			result.BestTrade = &tradeCopy
		}
		if result.WorstTrade == nil || trade.NetPnL.LessThan(result.WorstTrade.NetPnL) {
			tradeCopy := trades[i]
			result.WorstTrade = &tradeCopy
		}

		// Collect returns for Sharpe calculation
		returnFloat, _ := trade.ReturnPct.Float64()
		returns = append(returns, returnFloat)
	}

	result.TotalPnL = totalPnL
	result.TotalReturn = totalPnL.Div(config.InitialCapital).Mul(decimal.NewFromInt(100))

	// Win/Loss rates
	if result.TotalTrades > 0 {
		result.WinRate = decimal.NewFromInt(int64(result.WinningTrades)).
			Div(decimal.NewFromInt(int64(result.TotalTrades))).
			Mul(decimal.NewFromInt(100))
		result.LossRate = decimal.NewFromInt(int64(result.LosingTrades)).
			Div(decimal.NewFromInt(int64(result.TotalTrades))).
			Mul(decimal.NewFromInt(100))
	}

	// Average win/loss
	if result.WinningTrades > 0 {
		result.AvgWin = totalWinPnL.Div(decimal.NewFromInt(int64(result.WinningTrades)))
	}
	if result.LosingTrades > 0 {
		result.AvgLoss = totalLossPnL.Div(decimal.NewFromInt(int64(result.LosingTrades)))
	}

	// Profit factor
	if !grossLoss.IsZero() {
		result.ProfitFactor = grossProfit.Div(grossLoss)
	} else if grossProfit.IsPositive() {
		result.ProfitFactor = decimal.NewFromInt(999) // Infinite (no losses)
	}

	// Average holding time
	if result.TotalTrades > 0 {
		result.AvgHoldingTime = totalTime / time.Duration(result.TotalTrades)
	}

	// Calculate max drawdown
	result.MaxDrawdown, result.MaxDrawdownDate = b.calculateMaxDrawdown(equityCurve)

	// Calculate Sharpe ratio (assuming risk-free rate of 0%)
	result.SharpeRatio = b.calculateSharpeRatio(returns)

	// Calculate Sortino ratio
	result.SortinoRatio = b.calculateSortinoRatio(returns)

	// Generate daily returns
	result.DailyReturns = b.generateDailyReturns(trades, config)
}

// calculateMaxDrawdown finds the maximum peak-to-trough decline.
func (b *Backtester) calculateMaxDrawdown(equityCurve []EquityPoint) (decimal.Decimal, time.Time) {
	if len(equityCurve) == 0 {
		return decimal.Zero, time.Time{}
	}

	maxDrawdown := decimal.Zero
	maxDrawdownDate := time.Time{}
	peak := equityCurve[0].Equity

	for _, point := range equityCurve {
		if point.Equity.GreaterThan(peak) {
			peak = point.Equity
		}

		if !peak.IsZero() {
			drawdown := peak.Sub(point.Equity).Div(peak).Mul(decimal.NewFromInt(100))
			if drawdown.GreaterThan(maxDrawdown) {
				maxDrawdown = drawdown
				maxDrawdownDate = point.Timestamp
			}
		}
	}

	return maxDrawdown, maxDrawdownDate
}

// calculateSharpeRatio calculates the Sharpe ratio from returns.
func (b *Backtester) calculateSharpeRatio(returns []float64) decimal.Decimal {
	if len(returns) < 2 {
		return decimal.Zero
	}

	// Calculate mean return
	sum := 0.0
	for _, r := range returns {
		sum += r
	}
	mean := sum / float64(len(returns))

	// Calculate standard deviation
	sumSquaredDiff := 0.0
	for _, r := range returns {
		diff := r - mean
		sumSquaredDiff += diff * diff
	}
	stdDev := sumSquaredDiff / float64(len(returns)-1)
	if stdDev <= 0 {
		return decimal.Zero
	}

	// Annualize (assuming 365 trading days)
	annualizedReturn := mean * 365
	annualizedStdDev := stdDev * 365

	if annualizedStdDev == 0 {
		return decimal.Zero
	}

	sharpe := annualizedReturn / annualizedStdDev
	return decimal.NewFromFloat(sharpe)
}

// calculateSortinoRatio calculates the Sortino ratio (downside deviation only).
func (b *Backtester) calculateSortinoRatio(returns []float64) decimal.Decimal {
	if len(returns) < 2 {
		return decimal.Zero
	}

	// Calculate mean return
	sum := 0.0
	for _, r := range returns {
		sum += r
	}
	mean := sum / float64(len(returns))

	// Calculate downside deviation (only negative returns)
	sumSquaredDownside := 0.0
	downsideCount := 0
	for _, r := range returns {
		if r < 0 {
			sumSquaredDownside += r * r
			downsideCount++
		}
	}

	if downsideCount == 0 {
		return decimal.NewFromInt(999) // No downside
	}

	downsideDeviation := sumSquaredDownside / float64(downsideCount)
	if downsideDeviation <= 0 {
		return decimal.Zero
	}

	// Annualize
	annualizedReturn := mean * 365
	annualizedDownside := downsideDeviation * 365

	if annualizedDownside == 0 {
		return decimal.Zero
	}

	sortino := annualizedReturn / annualizedDownside
	return decimal.NewFromFloat(sortino)
}

// generateDailyReturns groups trades into daily returns.
func (b *Backtester) generateDailyReturns(
	trades []BacktestTrade,
	config BacktestConfig,
) []DailyReturn {
	if len(trades) == 0 {
		return nil
	}

	// Group trades by date
	dailyPnL := make(map[string]decimal.Decimal)
	dailyTrades := make(map[string]int)

	for _, trade := range trades {
		dateKey := trade.ExitTime.Format("2006-01-02")
		if pnl, exists := dailyPnL[dateKey]; exists {
			dailyPnL[dateKey] = pnl.Add(trade.NetPnL)
		} else {
			dailyPnL[dateKey] = trade.NetPnL
		}
		dailyTrades[dateKey]++
	}

	// Convert to sorted list
	var dates []string
	for date := range dailyPnL {
		dates = append(dates, date)
	}
	sort.Strings(dates)

	equity := config.InitialCapital
	dailyReturns := make([]DailyReturn, 0, len(dates))

	for _, dateStr := range dates {
		pnl := dailyPnL[dateStr]
		date, _ := time.Parse("2006-01-02", dateStr)

		var returnPct decimal.Decimal
		if !equity.IsZero() {
			returnPct = pnl.Div(equity).Mul(decimal.NewFromInt(100))
		}

		equity = equity.Add(pnl)

		dailyReturns = append(dailyReturns, DailyReturn{
			Date:       date,
			Return:     returnPct,
			PnL:        pnl,
			Equity:     equity,
			TradeCount: dailyTrades[dateStr],
		})
	}

	return dailyReturns
}

// DefaultBacktestConfig returns a sensible default configuration.
func DefaultBacktestConfig() BacktestConfig {
	return BacktestConfig{
		StartDate:       time.Now().AddDate(0, -1, 0), // 1 month ago
		EndDate:         time.Now(),
		InitialCapital:  decimal.NewFromInt(10000),
		MinAPY:          decimal.NewFromInt(5),
		MaxRiskScore:    decimal.NewFromInt(70),
		PositionSizing:  "kelly",
		MaxPositions:    5,
		TradingFee:      decimal.NewFromFloat(0.001), // 0.1%
		Slippage:        decimal.NewFromFloat(0.001), // 0.1%
		HoldingPeriod:   8 * time.Hour,
		ReinvestProfits: true,
	}
}
