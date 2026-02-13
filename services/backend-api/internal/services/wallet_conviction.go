package services

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/irfndi/neuratrade/internal/telemetry"
	"github.com/shopspring/decimal"
)

// WalletConvictionScore represents the conviction score for a wallet
type WalletConvictionScore struct {
	ChatID           string            `json:"chat_id"`
	UserID           string            `json:"user_id,omitempty"`
	Score            decimal.Decimal   `json:"score"`      // 0-100 score
	Level            ConvictionLevel   `json:"level"`      // low, medium, high, very_high
	Components       ScoreComponents   `json:"components"` // Individual component scores
	LastUpdated      time.Time         `json:"last_updated"`
	Trend            ScoreTrend        `json:"trend"`                       // improving, declining, stable
	HistoricalScores []decimal.Decimal `json:"historical_scores,omitempty"` // Last N scores for trend
}

// ConvictionLevel represents the conviction level category
type ConvictionLevel string

const (
	ConvictionLevelLow      ConvictionLevel = "low"
	ConvictionLevelMedium   ConvictionLevel = "medium"
	ConvictionLevelHigh     ConvictionLevel = "high"
	ConvictionLevelVeryHigh ConvictionLevel = "very_high"
)

// ScoreTrend represents the direction of score change
type ScoreTrend string

const (
	ScoreTrendImproving ScoreTrend = "improving"
	ScoreTrendDeclining ScoreTrend = "declining"
	ScoreTrendStable    ScoreTrend = "stable"
)

// ScoreComponents represents individual scoring components
type ScoreComponents struct {
	BalanceStability    decimal.Decimal `json:"balance_stability"`    // 0-25 points
	PortfolioDiversity  decimal.Decimal `json:"portfolio_diversity"`  // 0-25 points
	TradingActivity     decimal.Decimal `json:"trading_activity"`     // 0-20 points
	ExchangeReliability decimal.Decimal `json:"exchange_reliability"` // 0-15 points
	AccountAge          decimal.Decimal `json:"account_age"`          // 0-15 points
}

// WalletConvictionConfig holds configuration for conviction scoring
type WalletConvictionConfig struct {
	MaxHistoricalScores  int             `json:"max_historical_scores"`
	MinAccountAgeDays    int             `json:"min_account_age_days"`
	MinBalanceForHigh    decimal.Decimal `json:"min_balance_for_high"`
	DiversityBonusAssets int             `json:"diversity_bonus_assets"`
	ActivityLookbackDays int             `json:"activity_lookback_days"`
}

// DefaultWalletConvictionConfig returns default configuration
func DefaultWalletConvictionConfig() WalletConvictionConfig {
	return WalletConvictionConfig{
		MaxHistoricalScores:  10,
		MinAccountAgeDays:    30,
		MinBalanceForHigh:    decimal.NewFromInt(1000),
		DiversityBonusAssets: 3,
		ActivityLookbackDays: 30,
	}
}

// WalletConvictionScorer provides wallet conviction scoring functionality
type WalletConvictionScorer struct {
	config WalletConvictionConfig
	db     DBPool
	logger *slog.Logger
	mu     sync.RWMutex
	scores map[string]*WalletConvictionScore // chatID -> score
}

// NewWalletConvictionScorer creates a new wallet conviction scorer
func NewWalletConvictionScorer(db DBPool, config WalletConvictionConfig) *WalletConvictionScorer {
	return &WalletConvictionScorer{
		config: config,
		db:     db,
		logger: telemetry.Logger(),
		scores: make(map[string]*WalletConvictionScore),
	}
}

// CalculateScore calculates the conviction score for a wallet
func (wcs *WalletConvictionScorer) CalculateScore(ctx context.Context, chatID string) (*WalletConvictionScore, error) {
	wcs.mu.Lock()
	defer wcs.mu.Unlock()

	// Get or initialize score
	score, exists := wcs.scores[chatID]
	if !exists {
		score = &WalletConvictionScore{
			ChatID:           chatID,
			HistoricalScores: make([]decimal.Decimal, 0),
		}
	}

	// Calculate individual components
	balanceStability, err := wcs.calculateBalanceStability(ctx, chatID)
	if err != nil {
		wcs.logger.Warn("Failed to calculate balance stability", "chat_id", chatID, "error", err)
		balanceStability = decimal.Zero
	}

	portfolioDiversity, err := wcs.calculatePortfolioDiversity(ctx, chatID)
	if err != nil {
		wcs.logger.Warn("Failed to calculate portfolio diversity", "chat_id", chatID, "error", err)
		portfolioDiversity = decimal.Zero
	}

	tradingActivity, err := wcs.calculateTradingActivity(ctx, chatID)
	if err != nil {
		wcs.logger.Warn("Failed to calculate trading activity", "chat_id", chatID, "error", err)
		tradingActivity = decimal.Zero
	}

	exchangeReliability, err := wcs.calculateExchangeReliability(ctx, chatID)
	if err != nil {
		wcs.logger.Warn("Failed to calculate exchange reliability", "chat_id", chatID, "error", err)
		exchangeReliability = decimal.Zero
	}

	accountAge, err := wcs.calculateAccountAge(ctx, chatID)
	if err != nil {
		wcs.logger.Warn("Failed to calculate account age", "chat_id", chatID, "error", err)
		accountAge = decimal.Zero
	}

	// Update components
	score.Components = ScoreComponents{
		BalanceStability:    balanceStability,
		PortfolioDiversity:  portfolioDiversity,
		TradingActivity:     tradingActivity,
		ExchangeReliability: exchangeReliability,
		AccountAge:          accountAge,
	}

	// Calculate total score (max 100)
	totalScore := balanceStability.Add(portfolioDiversity).Add(tradingActivity).Add(exchangeReliability).Add(accountAge)
	score.Score = totalScore

	// Determine conviction level
	score.Level = wcs.determineLevel(totalScore)

	// Update trend
	score.Trend = wcs.calculateTrend(score)

	// Update historical scores
	wcs.updateHistoricalScores(score)

	// Update timestamp
	score.LastUpdated = time.Now().UTC()

	// Store in memory
	wcs.scores[chatID] = score

	wcs.logger.Info("Calculated wallet conviction score",
		"chat_id", chatID,
		"score", score.Score.String(),
		"level", score.Level,
		"trend", score.Trend,
	)

	return score, nil
}

// GetScore retrieves the cached conviction score for a wallet
func (wcs *WalletConvictionScorer) GetScore(chatID string) *WalletConvictionScore {
	wcs.mu.RLock()
	defer wcs.mu.RUnlock()
	return wcs.scores[chatID]
}

// GetAllScores returns all cached conviction scores
func (wcs *WalletConvictionScorer) GetAllScores() map[string]*WalletConvictionScore {
	wcs.mu.RLock()
	defer wcs.mu.RUnlock()
	result := make(map[string]*WalletConvictionScore)
	for k, v := range wcs.scores {
		result[k] = v
	}
	return result
}

// calculateBalanceStability scores based on balance stability (0-25 points)
func (wcs *WalletConvictionScorer) calculateBalanceStability(ctx context.Context, chatID string) (decimal.Decimal, error) {
	if wcs.db == nil {
		return decimal.Zero, fmt.Errorf("database connection is nil")
	}

	// Check for stable balance history
	// Score based on:
	// - Consistent balance above minimum (15 points)
	// - Low variance in balance (10 points)

	var avgBalance decimal.Decimal
	var balanceVariance decimal.Decimal
	var daysWithData int

	err := wcs.db.QueryRow(ctx, `
		SELECT 
			COALESCE(AVG(balance), 0),
			COALESCE(VARIANCE(balance), 0),
			COUNT(DISTINCT DATE(created_at))
		FROM wallet_balance_history
		WHERE chat_id = $1
		  AND created_at >= NOW() - INTERVAL '30 days'
	`, chatID).Scan(&avgBalance, &balanceVariance, &daysWithData)

	if err != nil {
		// If no history, give partial credit if wallet exists
		return decimal.NewFromInt(10), nil
	}

	score := decimal.Zero

	// Consistent balance above minimum (up to 15 points)
	if avgBalance.GreaterThanOrEqual(wcs.config.MinBalanceForHigh) {
		score = score.Add(decimal.NewFromInt(15))
	} else if avgBalance.GreaterThan(decimal.Zero) {
		// Partial credit for lower balance
		ratio := avgBalance.Div(wcs.config.MinBalanceForHigh)
		score = score.Add(decimal.NewFromInt(15).Mul(ratio))
	}

	// Low variance bonus (up to 10 points)
	if daysWithData >= 7 {
		if balanceVariance.IsZero() {
			score = score.Add(decimal.NewFromInt(10))
		} else {
			// Lower variance = higher score
			varianceScore := decimal.NewFromInt(10).Sub(balanceVariance.Div(avgBalance.Mul(decimal.NewFromInt(10))))
			if varianceScore.GreaterThan(decimal.Zero) {
				score = score.Add(varianceScore)
			}
		}
	}

	return score, nil
}

// calculatePortfolioDiversity scores based on asset diversity (0-25 points)
func (wcs *WalletConvictionScorer) calculatePortfolioDiversity(ctx context.Context, chatID string) (decimal.Decimal, error) {
	if wcs.db == nil {
		return decimal.Zero, fmt.Errorf("database connection is nil")
	}

	// Count unique assets and exchanges
	var assetCount int
	var exchangeCount int

	err := wcs.db.QueryRow(ctx, `
		SELECT COUNT(DISTINCT asset)
		FROM wallet_balances
		WHERE chat_id = $1 AND balance > 0
	`, chatID).Scan(&assetCount)
	if err != nil {
		assetCount = 0
	}

	err = wcs.db.QueryRow(ctx, `
		SELECT COUNT(DISTINCT provider)
		FROM telegram_operator_wallets
		WHERE chat_id = $1 AND status = 'connected'
	`, chatID).Scan(&exchangeCount)
	if err != nil {
		exchangeCount = 0
	}

	score := decimal.Zero

	// Asset diversity (up to 15 points)
	if assetCount >= wcs.config.DiversityBonusAssets {
		score = score.Add(decimal.NewFromInt(15))
	} else if assetCount > 0 {
		score = score.Add(decimal.NewFromInt(15).Mul(decimal.NewFromInt(int64(assetCount))).Div(decimal.NewFromInt(int64(wcs.config.DiversityBonusAssets))))
	}

	// Exchange diversity (up to 10 points)
	if exchangeCount >= 2 {
		score = score.Add(decimal.NewFromInt(10))
	} else if exchangeCount == 1 {
		score = score.Add(decimal.NewFromInt(5))
	}

	return score, nil
}

// calculateTradingActivity scores based on recent trading activity (0-20 points)
func (wcs *WalletConvictionScorer) calculateTradingActivity(ctx context.Context, chatID string) (decimal.Decimal, error) {
	if wcs.db == nil {
		return decimal.Zero, fmt.Errorf("database connection is nil")
	}

	// Count recent trades/signals
	var tradeCount int
	var signalCount int

	err := wcs.db.QueryRow(ctx, `
		SELECT COUNT(*)
		FROM trades
		WHERE chat_id = $1
		  AND created_at >= NOW() - INTERVAL '%d days'
	`, chatID, wcs.config.ActivityLookbackDays).Scan(&tradeCount)
	if err != nil {
		tradeCount = 0
	}

	err = wcs.db.QueryRow(ctx, `
		SELECT COUNT(*)
		FROM signals_processed
		WHERE chat_id = $1
		  AND created_at >= NOW() - INTERVAL '%d days'
	`, chatID, wcs.config.ActivityLookbackDays).Scan(&signalCount)
	if err != nil {
		signalCount = 0
	}

	score := decimal.Zero

	// Trading activity (up to 20 points)
	// More trades = higher score, with diminishing returns
	totalActivity := tradeCount + signalCount
	if totalActivity >= 50 {
		score = decimal.NewFromInt(20)
	} else if totalActivity > 0 {
		// Logarithmic scaling for activity
		activityScore := decimal.NewFromFloat(float64(totalActivity) / 50.0 * 20.0)
		score = activityScore
	}

	return score, nil
}

// calculateExchangeReliability scores based on exchange health (0-15 points)
func (wcs *WalletConvictionScorer) calculateExchangeReliability(ctx context.Context, chatID string) (decimal.Decimal, error) {
	if wcs.db == nil {
		return decimal.Zero, fmt.Errorf("database connection is nil")
	}

	// Get connected exchanges and their reliability
	rows, err := wcs.db.Query(ctx, `
		SELECT provider, status
		FROM telegram_operator_wallets
		WHERE chat_id = $1 AND status = 'connected'
	`, chatID)
	if err != nil {
		return decimal.Zero, nil
	}
	defer rows.Close()

	exchangeCount := 0
	reliableCount := 0

	for rows.Next() {
		var provider, status string
		if err := rows.Scan(&provider, &status); err != nil {
			continue
		}
		exchangeCount++
		// Consider exchanges with connected status as reliable
		if status == "connected" {
			reliableCount++
		}
	}

	if exchangeCount == 0 {
		return decimal.Zero, nil
	}

	// Score based on reliability ratio (up to 15 points)
	reliabilityRatio := float64(reliableCount) / float64(exchangeCount)
	score := decimal.NewFromFloat(reliabilityRatio * 15.0)

	return score, nil
}

// calculateAccountAge scores based on account age (0-15 points)
func (wcs *WalletConvictionScorer) calculateAccountAge(ctx context.Context, chatID string) (decimal.Decimal, error) {
	if wcs.db == nil {
		return decimal.Zero, fmt.Errorf("database connection is nil")
	}

	var createdAt time.Time
	var accountAge int

	err := wcs.db.QueryRow(ctx, `
		SELECT COALESCE(MIN(created_at), NOW())
		FROM telegram_operator_wallets
		WHERE chat_id = $1
	`, chatID).Scan(&createdAt)
	if err != nil {
		return decimal.Zero, nil
	}

	// Calculate days since account creation
	days := time.Since(createdAt).Hours() / 24
	accountAge = int(days)

	score := decimal.Zero

	// Account age scoring (up to 15 points)
	minDays := wcs.config.MinAccountAgeDays
	if accountAge >= minDays*3 {
		score = decimal.NewFromInt(15) // Max score for 3x minimum age
	} else if accountAge >= minDays {
		// Linear scaling between min and 3x min
		score = decimal.NewFromInt(15).Mul(decimal.NewFromInt(int64(accountAge))).Div(decimal.NewFromInt(int64(minDays * 3)))
	} else if accountAge > 0 {
		// Partial credit for newer accounts
		score = decimal.NewFromInt(5).Mul(decimal.NewFromInt(int64(accountAge))).Div(decimal.NewFromInt(int64(minDays)))
	}

	return score, nil
}

// determineLevel converts numeric score to conviction level
func (wcs *WalletConvictionScorer) determineLevel(score decimal.Decimal) ConvictionLevel {
	if score.GreaterThanOrEqual(decimal.NewFromInt(80)) {
		return ConvictionLevelVeryHigh
	} else if score.GreaterThanOrEqual(decimal.NewFromInt(60)) {
		return ConvictionLevelHigh
	} else if score.GreaterThanOrEqual(decimal.NewFromInt(40)) {
		return ConvictionLevelMedium
	}
	return ConvictionLevelLow
}

// calculateTrend determines score trend from historical data
func (wcs *WalletConvictionScorer) calculateTrend(score *WalletConvictionScore) ScoreTrend {
	if len(score.HistoricalScores) < 2 {
		return ScoreTrendStable
	}

	// Compare current score to average of historical scores
	var total decimal.Decimal
	for _, s := range score.HistoricalScores {
		total = total.Add(s)
	}
	avg := total.Div(decimal.NewFromInt(int64(len(score.HistoricalScores))))

	threshold := decimal.NewFromFloat(5.0) // 5% threshold for significance
	changePercent := score.Score.Sub(avg).Div(avg).Mul(decimal.NewFromInt(100))

	if changePercent.GreaterThan(threshold) {
		return ScoreTrendImproving
	} else if changePercent.LessThan(threshold.Neg()) {
		return ScoreTrendDeclining
	}
	return ScoreTrendStable
}

// updateHistoricalScores maintains the historical score list
func (wcs *WalletConvictionScorer) updateHistoricalScores(score *WalletConvictionScore) {
	score.HistoricalScores = append(score.HistoricalScores, score.Score)

	// Trim to max size
	if len(score.HistoricalScores) > wcs.config.MaxHistoricalScores {
		score.HistoricalScores = score.HistoricalScores[1:]
	}
}

// RefreshScore forces recalculation of a wallet's conviction score
func (wcs *WalletConvictionScorer) RefreshScore(ctx context.Context, chatID string) (*WalletConvictionScore, error) {
	return wcs.CalculateScore(ctx, chatID)
}

// GetCohortScores returns scores grouped by conviction level
func (wcs *WalletConvictionScorer) GetCohortScores() map[ConvictionLevel][]*WalletConvictionScore {
	wcs.mu.RLock()
	defer wcs.mu.RUnlock()

	cohorts := make(map[ConvictionLevel][]*WalletConvictionScore)
	for _, score := range wcs.scores {
		cohorts[score.Level] = append(cohorts[score.Level], score)
	}

	return cohorts
}
