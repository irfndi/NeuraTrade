package services

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/irfndi/neuratrade/internal/ccxt"
	zaplogrus "github.com/irfndi/neuratrade/internal/logging/zaplogrus"
	"github.com/irfndi/neuratrade/internal/services/risk"
	"github.com/redis/go-redis/v9"
	"github.com/shopspring/decimal"
	"golang.org/x/sync/singleflight"
)

type PortfolioSafetyConfig struct {
	MaxPositionSizePct   float64       `json:"max_position_size_pct"`
	MaxExposurePct       float64       `json:"max_exposure_pct"`
	DefaultQuoteCurrency string        `json:"default_quote_currency"`
	CacheTTL             time.Duration `json:"cache_ttl"`
}

func DefaultPortfolioSafetyConfig() PortfolioSafetyConfig {
	return PortfolioSafetyConfig{
		MaxPositionSizePct:   0.10,
		MaxExposurePct:       0.50,
		DefaultQuoteCurrency: "USDT",
		CacheTTL:             30 * time.Second,
	}
}

type ExchangeExposure struct {
	Exchange         string          `json:"exchange"`
	TotalBalance     decimal.Decimal `json:"total_balance"`
	AvailableBalance decimal.Decimal `json:"available_balance"`
	UsedBalance      decimal.Decimal `json:"used_balance"`
	ExposurePct      float64         `json:"exposure_pct"`
}

type SafetyPortfolioSnapshot struct {
	TotalEquity       decimal.Decimal    `json:"total_equity"`
	AvailableFunds    decimal.Decimal    `json:"available_funds"`
	TotalExposure     decimal.Decimal    `json:"total_exposure"`
	ExposurePct       float64            `json:"exposure_pct"`
	UnrealizedPnL     decimal.Decimal    `json:"unrealized_pnl"`
	RealizedPnL       decimal.Decimal    `json:"realized_pnl"`
	OpenPositions     int                `json:"open_positions"`
	ExchangeExposures []ExchangeExposure `json:"exchange_exposures"`
	Positions         []SafetyPosition   `json:"positions"`
	CalculatedAt      time.Time          `json:"calculated_at"`
}

type SafetyPosition struct {
	Symbol        string `json:"symbol"`
	Side          string `json:"side"`
	Size          string `json:"size"`
	EntryPrice    string `json:"entry_price"`
	MarkPrice     string `json:"mark_price"`
	UnrealizedPnL string `json:"unrealized_pnl"`
}

type SafetyStatus struct {
	IsSafe           bool              `json:"is_safe"`
	TradingAllowed   bool              `json:"trading_allowed"`
	Reasons          []string          `json:"reasons,omitempty"`
	Warnings         []string          `json:"warnings,omitempty"`
	MaxPositionSize  decimal.Decimal   `json:"max_position_size"`
	CurrentDrawdown  float64           `json:"current_drawdown"`
	DailyLossUsed    decimal.Decimal   `json:"daily_loss_used"`
	DailyLossLimit   decimal.Decimal   `json:"daily_loss_limit"`
	PositionThrottle float64           `json:"position_throttle"`
	CheckedAt        time.Time         `json:"checked_at"`
	Details          map[string]string `json:"details,omitempty"`
}

type PortfolioSafetyService struct {
	config           PortfolioSafetyConfig
	ccxtService      ccxt.CCXTService
	positionTracker  *PositionTracker
	riskManager      *risk.RiskManagerAgent
	drawdownHalt     *MaxDrawdownHalt
	dailyLossTracker *risk.DailyLossTracker
	positionThrottle *risk.PositionSizeThrottle
	redis            *redis.Client
	logger           *zaplogrus.Logger

	lastSnapshot     *SafetyPortfolioSnapshot
	lastSnapshotTime time.Time
	mu               sync.RWMutex
	requestGroup     singleflight.Group
}

func NewPortfolioSafetyService(
	config PortfolioSafetyConfig,
	ccxtService ccxt.CCXTService,
	positionTracker *PositionTracker,
	riskManager *risk.RiskManagerAgent,
	drawdownHalt *MaxDrawdownHalt,
	dailyLossTracker *risk.DailyLossTracker,
	positionThrottle *risk.PositionSizeThrottle,
	redis *redis.Client,
	logger *zaplogrus.Logger,
) *PortfolioSafetyService {
	return &PortfolioSafetyService{
		config:           config,
		ccxtService:      ccxtService,
		positionTracker:  positionTracker,
		riskManager:      riskManager,
		drawdownHalt:     drawdownHalt,
		dailyLossTracker: dailyLossTracker,
		positionThrottle: positionThrottle,
		redis:            redis,
		logger:           logger,
	}
}

func (s *PortfolioSafetyService) GetPortfolioSnapshot(ctx context.Context, chatID string, exchanges []string) (*SafetyPortfolioSnapshot, error) {
	s.mu.RLock()
	if s.lastSnapshot != nil && time.Since(s.lastSnapshotTime) < s.config.CacheTTL {
		snapshot := *s.lastSnapshot
		s.mu.RUnlock()
		return &snapshot, nil
	}
	s.mu.RUnlock()

	key := "snapshot_" + chatID
	result, err, _ := s.requestGroup.Do(key, func() (interface{}, error) {
		snap, err := s.calculateSnapshot(ctx, chatID, exchanges)
		if err != nil {
			return nil, err
		}

		s.mu.Lock()
		s.lastSnapshot = snap
		s.lastSnapshotTime = time.Now()
		s.mu.Unlock()

		return snap, nil
	})

	if err != nil {
		return nil, err
	}

	return result.(*SafetyPortfolioSnapshot), nil
}

func (s *PortfolioSafetyService) calculateSnapshot(ctx context.Context, chatID string, exchanges []string) (*SafetyPortfolioSnapshot, error) {
	snapshot := &SafetyPortfolioSnapshot{
		CalculatedAt:      time.Now().UTC(),
		ExchangeExposures: make([]ExchangeExposure, 0),
		Positions:         make([]SafetyPosition, 0),
	}

	totalBalance := decimal.Zero
	totalAvailable := decimal.Zero
	totalUsed := decimal.Zero

	for _, exchange := range exchanges {
		balance, err := s.ccxtService.FetchBalance(ctx, exchange)
		if err != nil {
			if s.logger != nil {
				s.logger.Warn("Failed to fetch balance from exchange",
					"exchange", exchange,
					"error", err)
			}
			continue
		}

		exchangeTotal := decimal.Zero
		exchangeAvailable := decimal.Zero
		exchangeUsed := decimal.Zero

		for currency, amount := range balance.Total {
			if amount <= 0 {
				continue
			}
			if currency == s.config.DefaultQuoteCurrency {
				exchangeTotal = exchangeTotal.Add(decimal.NewFromFloat(amount))
			}
		}

		for currency, amount := range balance.Free {
			if amount <= 0 {
				continue
			}
			if currency == s.config.DefaultQuoteCurrency {
				exchangeAvailable = exchangeAvailable.Add(decimal.NewFromFloat(amount))
			}
		}

		for currency, amount := range balance.Used {
			if amount <= 0 {
				continue
			}
			if currency == s.config.DefaultQuoteCurrency {
				exchangeUsed = exchangeUsed.Add(decimal.NewFromFloat(amount))
			}
		}

		totalBalance = totalBalance.Add(exchangeTotal)
		totalAvailable = totalAvailable.Add(exchangeAvailable)
		totalUsed = totalUsed.Add(exchangeUsed)

		snapshot.ExchangeExposures = append(snapshot.ExchangeExposures, ExchangeExposure{
			Exchange:         exchange,
			TotalBalance:     exchangeTotal,
			AvailableBalance: exchangeAvailable,
			UsedBalance:      exchangeUsed,
		})
	}

	if s.positionTracker != nil {
		positions := s.positionTracker.GetAllPositions()
		snapshot.OpenPositions = len(positions)

		positionValue := decimal.Zero
		for _, pos := range positions {
			positionValue = positionValue.Add(pos.Size.Mul(pos.CurrentPrice))
			snapshot.UnrealizedPnL = snapshot.UnrealizedPnL.Add(pos.UnrealizedPL)

			snapshot.Positions = append(snapshot.Positions, SafetyPosition{
				Symbol:        pos.Symbol,
				Side:          pos.Side,
				Size:          pos.Size.String(),
				EntryPrice:    pos.EntryPrice.String(),
				MarkPrice:     pos.CurrentPrice.String(),
				UnrealizedPnL: pos.UnrealizedPL.String(),
			})
		}

		snapshot.TotalExposure = positionValue
	}

	snapshot.TotalEquity = totalBalance.Add(snapshot.UnrealizedPnL)
	snapshot.AvailableFunds = totalAvailable

	if snapshot.TotalEquity.GreaterThan(decimal.Zero) {
		snapshot.ExposurePct, _ = snapshot.TotalExposure.Div(snapshot.TotalEquity).Float64()
	}

	for i := range snapshot.ExchangeExposures {
		if snapshot.TotalEquity.GreaterThan(decimal.Zero) {
			snapshot.ExchangeExposures[i].ExposurePct, _ = snapshot.ExchangeExposures[i].UsedBalance.
				Div(snapshot.TotalEquity).Float64()
		}
	}

	return snapshot, nil
}

func (s *PortfolioSafetyService) CheckSafety(ctx context.Context, chatID string, snapshot *SafetyPortfolioSnapshot) (*SafetyStatus, error) {
	status := &SafetyStatus{
		IsSafe:           true,
		TradingAllowed:   true,
		Reasons:          make([]string, 0),
		Warnings:         make([]string, 0),
		MaxPositionSize:  decimal.Zero,
		PositionThrottle: 1.0,
		CheckedAt:        time.Now().UTC(),
		Details:          make(map[string]string),
	}

	if s.drawdownHalt != nil {
		if s.drawdownHalt.IsTradingHalted(chatID) {
			status.TradingAllowed = false
			status.IsSafe = false
			status.Reasons = append(status.Reasons, "Trading halted due to max drawdown")
		}

		if state, exists := s.drawdownHalt.GetState(chatID); exists {
			status.CurrentDrawdown = state.CurrentDrawdown.InexactFloat64()
			status.Details["drawdown_status"] = string(state.Status)
		}
	}

	if s.dailyLossTracker != nil {
		cfg := s.dailyLossTracker.Config()
		status.DailyLossLimit = cfg.MaxDailyLoss

		exceeded, currentLoss, err := s.dailyLossTracker.CheckLossLimit(ctx, chatID)
		if err != nil {
			if s.logger != nil {
				s.logger.Warn("Failed to check daily loss limit", "error", err)
			}
		} else {
			status.DailyLossUsed = currentLoss
			if exceeded {
				status.TradingAllowed = false
				status.IsSafe = false
				status.Reasons = append(status.Reasons, fmt.Sprintf("Daily loss limit exceeded: %s/%s",
					currentLoss.StringFixed(2), cfg.MaxDailyLoss.StringFixed(2)))
			}
		}
	}

	if s.positionThrottle != nil {
		multiplier, err := s.positionThrottle.ApplyThrottle(ctx, chatID, decimal.NewFromInt(1))
		if err != nil {
			if s.logger != nil {
				s.logger.Warn("Failed to apply position throttle", "error", err)
			}
			status.PositionThrottle = 1.0
		} else {
			status.PositionThrottle = multiplier.InexactFloat64()
			if multiplier.LessThan(decimal.NewFromFloat(0.5)) {
				status.Warnings = append(status.Warnings,
					fmt.Sprintf("Position size reduced to %.0f%% due to consecutive losses", multiplier.InexactFloat64()*100))
			}
		}
	}

	if snapshot != nil && snapshot.TotalEquity.GreaterThan(decimal.Zero) {
		maxFromPct := snapshot.TotalEquity.Mul(decimal.NewFromFloat(s.config.MaxPositionSizePct))
		maxAfterThrottle := maxFromPct.Mul(decimal.NewFromFloat(status.PositionThrottle))

		if maxAfterThrottle.GreaterThan(snapshot.AvailableFunds) {
			maxAfterThrottle = snapshot.AvailableFunds
		}

		status.MaxPositionSize = maxAfterThrottle
		status.Details["max_position_pct"] = fmt.Sprintf("%.1f%%", s.config.MaxPositionSizePct*100)
		status.Details["throttle_applied"] = fmt.Sprintf("%.1f%%", status.PositionThrottle*100)
	}

	if snapshot != nil && snapshot.ExposurePct > s.config.MaxExposurePct {
		status.Warnings = append(status.Warnings,
			fmt.Sprintf("Total exposure (%.1f%%) exceeds limit (%.1f%%)",
				snapshot.ExposurePct*100, s.config.MaxExposurePct*100))
	}

	if s.riskManager != nil && snapshot != nil {
		signals := []risk.RiskSignal{
			{
				Name:        "drawdown",
				Value:       status.CurrentDrawdown,
				Weight:      0.3,
				Threshold:   0.15,
				Description: "Current portfolio drawdown",
			},
			{
				Name:        "exposure",
				Value:       snapshot.ExposurePct,
				Weight:      0.2,
				Threshold:   s.config.MaxExposurePct,
				Description: "Total portfolio exposure",
			},
			{
				Name:        "position_count",
				Value:       float64(snapshot.OpenPositions),
				Weight:      0.1,
				Threshold:   5.0,
				Description: "Number of open positions",
			},
		}

		assessment, err := s.riskManager.AssessPortfolioRisk(ctx, signals)
		if err != nil {
			if s.logger != nil {
				s.logger.Warn("Failed to assess portfolio risk", "error", err)
			}
		} else {
			status.Details["risk_level"] = string(assessment.RiskLevel)
			status.Details["risk_score"] = fmt.Sprintf("%.2f", assessment.Score)

			switch assessment.Action {
			case risk.RiskActionBlock:
				status.TradingAllowed = false
				status.IsSafe = false
				status.Reasons = append(status.Reasons, assessment.Recommendations...)
			case risk.RiskActionWarning:
				status.Warnings = append(status.Warnings, assessment.Recommendations...)
			}

			if assessment.MaxPositionSize.GreaterThan(decimal.Zero) &&
				assessment.MaxPositionSize.LessThan(status.MaxPositionSize) {
				status.MaxPositionSize = assessment.MaxPositionSize
			}
		}
	}

	return status, nil
}

func (s *PortfolioSafetyService) CanExecuteTrade(ctx context.Context, chatID string, exchange string, symbol string, size decimal.Decimal) (bool, string, error) {
	exchanges := []string{}
	if exchange != "" {
		exchanges = []string{exchange}
	}
	snapshot, err := s.GetPortfolioSnapshot(ctx, chatID, exchanges)
	if err != nil {
		return false, "", fmt.Errorf("failed to get portfolio snapshot: %w", err)
	}

	status, err := s.CheckSafety(ctx, chatID, snapshot)
	if err != nil {
		return false, "", fmt.Errorf("failed to check safety: %w", err)
	}

	if !status.TradingAllowed {
		return false, fmt.Sprintf("Trading not allowed: %v", status.Reasons), nil
	}

	if size.GreaterThan(status.MaxPositionSize) {
		return false, fmt.Sprintf("Position size %s exceeds maximum allowed %s (throttled to %.0f%%)",
			size.StringFixed(2), status.MaxPositionSize.StringFixed(2), status.PositionThrottle*100), nil
	}

	return true, "", nil
}

func (s *PortfolioSafetyService) GetSafetyDiagnostics(ctx context.Context, chatID string, exchanges []string) (map[string]interface{}, error) {
	diagnostics := make(map[string]interface{})

	snapshot, err := s.GetPortfolioSnapshot(ctx, chatID, exchanges)
	if err != nil {
		return nil, fmt.Errorf("failed to get portfolio snapshot: %w", err)
	}

	status, err := s.CheckSafety(ctx, chatID, snapshot)
	if err != nil {
		return nil, fmt.Errorf("failed to check safety: %w", err)
	}

	diagnostics["portfolio"] = map[string]interface{}{
		"total_equity":    snapshot.TotalEquity.StringFixed(2),
		"available_funds": snapshot.AvailableFunds.StringFixed(2),
		"total_exposure":  snapshot.TotalExposure.StringFixed(2),
		"exposure_pct":    fmt.Sprintf("%.2f%%", snapshot.ExposurePct*100),
		"unrealized_pnl":  snapshot.UnrealizedPnL.StringFixed(2),
		"open_positions":  snapshot.OpenPositions,
	}

	diagnostics["safety"] = map[string]interface{}{
		"is_safe":           status.IsSafe,
		"trading_allowed":   status.TradingAllowed,
		"max_position_size": status.MaxPositionSize.StringFixed(2),
		"current_drawdown":  fmt.Sprintf("%.2f%%", status.CurrentDrawdown*100),
		"daily_loss_used":   status.DailyLossUsed.StringFixed(2),
		"daily_loss_limit":  status.DailyLossLimit.StringFixed(2),
		"position_throttle": fmt.Sprintf("%.0f%%", status.PositionThrottle*100),
		"reasons":           status.Reasons,
		"warnings":          status.Warnings,
	}

	diagnostics["exchanges"] = snapshot.ExchangeExposures
	diagnostics["details"] = status.Details

	return diagnostics, nil
}

func (s *PortfolioSafetyService) InvalidateCache() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.lastSnapshot = nil
	s.lastSnapshotTime = time.Time{}
}

func (s *PortfolioSafetyService) SetConfig(config PortfolioSafetyConfig) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.config = config
}

func (s *PortfolioSafetyService) GetConfig() PortfolioSafetyConfig {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.config
}
