package services

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	appautonomy "github.com/irfndi/neuratrade/internal/app/autonomy"
	"github.com/irfndi/neuratrade/internal/ccxt"
	zaplogrus "github.com/irfndi/neuratrade/internal/logging/zaplogrus"
	"github.com/irfndi/neuratrade/internal/services/risk"
	"github.com/redis/go-redis/v9"
	"github.com/shopspring/decimal"
	"golang.org/x/sync/singleflight"
)

type PortfolioSafetyConfig struct {
	MaxPositionSizePct       float64       `json:"max_position_size_pct"`
	MaxPositionFloorPct      float64       `json:"max_position_floor_pct"`
	MaxExposurePct           float64       `json:"max_exposure_pct"`
	DefaultQuoteCurrency     string        `json:"default_quote_currency"`
	CacheTTL                 time.Duration `json:"cache_ttl"`
	StaleSnapshotFallbackTTL time.Duration `json:"stale_snapshot_fallback_ttl"`
}

func DefaultPortfolioSafetyConfig() PortfolioSafetyConfig {
	return PortfolioSafetyConfig{
		MaxPositionSizePct:       0.10,
		MaxPositionFloorPct:      0.20,
		MaxExposurePct:           0.50,
		DefaultQuoteCurrency:     "USDT",
		CacheTTL:                 30 * time.Second,
		StaleSnapshotFallbackTTL: 10 * time.Minute,
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
	TotalEquity        decimal.Decimal    `json:"total_equity"`
	AvailableFunds     decimal.Decimal    `json:"available_funds"`
	TotalExposure      decimal.Decimal    `json:"total_exposure"`
	ExposurePct        float64            `json:"exposure_pct"`
	UnrealizedPnL      decimal.Decimal    `json:"unrealized_pnl"`
	RealizedPnL        decimal.Decimal    `json:"realized_pnl"`
	OpenPositions      int                `json:"open_positions"`
	ExchangeExposures  []ExchangeExposure `json:"exchange_exposures"`
	Positions          []SafetyPosition   `json:"positions"`
	CalculatedAt       time.Time          `json:"calculated_at"`
	balancesByExchange map[string]*ccxt.BalanceResponse
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

type TradeSafetyDecision struct {
	Allowed                  bool
	Reason                   string
	EffectiveMaxPosition     decimal.Decimal
	EffectiveThrottlePct     float64
	MinNotional              decimal.Decimal
	ZeroMaxMinNotionalBypass bool
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

	lastSnapshotKey  string
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
		config:           normalizePortfolioSafetyConfig(config),
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
	key := buildSnapshotCacheKey(chatID, exchanges)

	s.mu.RLock()
	if s.lastSnapshot != nil &&
		s.lastSnapshotKey == key &&
		time.Since(s.lastSnapshotTime) < s.config.CacheTTL {
		snapshot := cloneSafetyPortfolioSnapshot(s.lastSnapshot)
		s.mu.RUnlock()
		return &snapshot, nil
	}
	s.mu.RUnlock()

	result, err, _ := s.requestGroup.Do(key, func() (interface{}, error) {
		snap, err := s.calculateSnapshot(ctx, chatID, exchanges)
		if err != nil {
			return nil, err
		}

		s.mu.Lock()
		s.lastSnapshotKey = key
		s.lastSnapshot = snap
		s.lastSnapshotTime = time.Now()
		s.mu.Unlock()

		return snap, nil
	})

	if err != nil {
		s.mu.RLock()
		staleAge := time.Since(s.lastSnapshotTime)
		canFallback := s.lastSnapshot != nil &&
			s.lastSnapshotKey == key &&
			staleAge <= s.config.StaleSnapshotFallbackTTL
		if canFallback {
			snapshot := cloneSafetyPortfolioSnapshot(s.lastSnapshot)
			s.mu.RUnlock()
			if s.logger != nil {
				s.logger.Warn("Using stale portfolio snapshot after refresh failure",
					"chat_id", chatID,
					"exchanges", strings.Join(exchanges, ","),
					"error", err,
					"stale_age", staleAge.Round(time.Second).String())
			}
			return &snapshot, nil
		}
		s.mu.RUnlock()
		return nil, err
	}

	snapshot := cloneSafetyPortfolioSnapshot(result.(*SafetyPortfolioSnapshot))
	return &snapshot, nil
}

func buildSnapshotCacheKey(chatID string, exchanges []string) string {
	if len(exchanges) == 0 {
		return "snapshot_" + chatID
	}

	exchangesCopy := append([]string(nil), exchanges...)
	sort.Strings(exchangesCopy)
	return "snapshot_" + chatID + "_" + strings.Join(exchangesCopy, ",")
}

func (s *PortfolioSafetyService) calculateSnapshot(ctx context.Context, chatID string, exchanges []string) (*SafetyPortfolioSnapshot, error) {
	snapshot := &SafetyPortfolioSnapshot{
		CalculatedAt:       time.Now().UTC(),
		ExchangeExposures:  make([]ExchangeExposure, 0),
		Positions:          make([]SafetyPosition, 0),
		balancesByExchange: make(map[string]*ccxt.BalanceResponse),
	}

	totalBalance := decimal.Zero
	totalAvailable := decimal.Zero
	totalUsed := decimal.Zero
	successfulBalanceFetches := 0
	var lastBalanceErr error

	for _, exchange := range exchanges {
		balance, err := s.ccxtService.FetchBalance(ctx, exchange)
		if err != nil {
			lastBalanceErr = err
			if s.logger != nil {
				s.logger.Warn("Failed to fetch balance from exchange",
					"exchange", exchange,
					"error", err)
			}
			continue
		}
		successfulBalanceFetches++
		snapshot.balancesByExchange[exchange] = balance

		exchangeTotal := decimal.Zero
		exchangeAvailable := decimal.Zero
		exchangeUsed := decimal.Zero

		for currency, amount := range balance.Total {
			if amount <= 0 {
				continue
			}
			if isQuoteCurrency(currency, s.config.DefaultQuoteCurrency) {
				exchangeTotal = exchangeTotal.Add(decimal.NewFromFloat(amount))
			}
		}

		for currency, amount := range balance.Free {
			if amount <= 0 {
				continue
			}
			if isQuoteCurrency(currency, s.config.DefaultQuoteCurrency) {
				exchangeAvailable = exchangeAvailable.Add(decimal.NewFromFloat(amount))
			}
		}

		for currency, amount := range balance.Used {
			if amount <= 0 {
				continue
			}
			if isQuoteCurrency(currency, s.config.DefaultQuoteCurrency) {
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

	if len(exchanges) > 0 && successfulBalanceFetches == 0 {
		if lastBalanceErr != nil {
			return nil, fmt.Errorf("failed to fetch balance from all requested exchanges: %w", lastBalanceErr)
		}
		return nil, fmt.Errorf("failed to fetch balance from all requested exchanges")
	}

	trackerHasPositions := false
	if s.positionTracker != nil {
		positions := s.positionTracker.GetAllPositions()
		snapshot.OpenPositions = len(positions)
		trackerHasPositions = len(positions) > 0

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
	if !trackerHasPositions && s.ccxtService != nil {
		exchangeExposure := decimal.Zero
		for _, exchange := range exchanges {
			resp, err := s.ccxtService.FetchPositions(ctx, exchange)
			if err != nil {
				if s.logger != nil {
					s.logger.Warn("Failed to fetch positions from exchange",
						"exchange", exchange,
						"error", err)
				}
				continue
			}
			if resp == nil || len(resp.Positions) == 0 {
				continue
			}

			for _, pos := range resp.Positions {
				size := pos.Size.Abs()
				if size.IsZero() {
					continue
				}
				markPrice := pos.MarkPrice
				if markPrice.IsZero() {
					markPrice = pos.EntryPrice
				}
				exchangeExposure = exchangeExposure.Add(size.Mul(markPrice))
				snapshot.UnrealizedPnL = snapshot.UnrealizedPnL.Add(pos.UnrealizedPnl)

				snapshot.Positions = append(snapshot.Positions, SafetyPosition{
					Symbol:        pos.Symbol,
					Side:          pos.Side,
					Size:          size.String(),
					EntryPrice:    pos.EntryPrice.String(),
					MarkPrice:     markPrice.String(),
					UnrealizedPnL: pos.UnrealizedPnl.String(),
				})
			}
		}
		if len(snapshot.Positions) > 0 {
			snapshot.OpenPositions = len(snapshot.Positions)
			snapshot.TotalExposure = exchangeExposure
		}
	}

	snapshot.TotalEquity = totalBalance.Add(snapshot.UnrealizedPnL)
	snapshot.AvailableFunds = totalAvailable
	// Fallback to used-balance exposure when position tracker data is unavailable.
	if snapshot.TotalExposure.IsZero() {
		snapshot.TotalExposure = totalUsed
	}

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

		// This is the policy cap before market-specific liquidity or leverage-aware
		// execution checks are applied in EvaluateTradeWithLeverage.
		status.MaxPositionSize = maxAfterThrottle
		status.Details["max_position_pct"] = fmt.Sprintf("%.1f%%", s.config.MaxPositionSizePct*100)
		status.Details["throttle_applied"] = fmt.Sprintf("%.1f%%", status.PositionThrottle*100)
		status.Details["max_position_basis"] = "policy_pre_liquidity"
	}

	if snapshot != nil && snapshot.ExposurePct > s.config.MaxExposurePct {
		status.Warnings = append(status.Warnings,
			fmt.Sprintf("Total exposure (%.1f%%) exceeds limit (%.1f%%)",
				snapshot.ExposurePct*100, s.config.MaxExposurePct*100))

		// Hard-stop only when exposure is materially beyond policy, otherwise keep this as warning.
		if snapshot.ExposurePct > s.config.MaxExposurePct*2 {
			status.TradingAllowed = false
			status.IsSafe = false
			status.Reasons = append(status.Reasons,
				fmt.Sprintf("Exposure hard limit breached: %.1f%% exceeds %.1f%%", snapshot.ExposurePct*100, s.config.MaxExposurePct*200))
		}
	}

	if s.riskManager != nil && snapshot != nil {
		drawdownSignal := normalizeRiskSignal(status.CurrentDrawdown, 0.15)
		exposureSignal := normalizeRiskSignal(snapshot.ExposurePct, s.config.MaxExposurePct)
		positionCountSignal := normalizeRiskSignal(float64(snapshot.OpenPositions), 5.0)

		signals := []risk.RiskSignal{
			{
				Name:        "drawdown",
				Value:       drawdownSignal,
				Weight:      0.3,
				Threshold:   1.0,
				Description: "Current portfolio drawdown",
			},
			{
				Name:        "exposure",
				Value:       exposureSignal,
				Weight:      0.2,
				Threshold:   1.0,
				Description: "Total portfolio exposure",
			},
			{
				Name:        "position_count",
				Value:       positionCountSignal,
				Weight:      0.1,
				Threshold:   1.0,
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

func (s *PortfolioSafetyService) CanExecuteTrade(ctx context.Context, chatID string, exchange string, symbol string, marketType string, size decimal.Decimal) (bool, string, error) {
	decision, err := s.EvaluateTradeWithLeverage(ctx, chatID, exchange, symbol, marketType, scalpingLeverageFromContext(ctx), size)
	if err != nil {
		return false, "", err
	}
	return decision.Allowed, decision.Reason, nil
}

func (s *PortfolioSafetyService) CanExecuteTradeWithLeverage(ctx context.Context, chatID string, exchange string, symbol string, marketType string, leverage int, size decimal.Decimal) (bool, string, error) {
	decision, err := s.EvaluateTradeWithLeverage(ctx, chatID, exchange, symbol, marketType, leverage, size)
	if err != nil {
		return false, "", err
	}
	return decision.Allowed, decision.Reason, nil
}

func (s *PortfolioSafetyService) EvaluateTradeWithLeverage(ctx context.Context, chatID string, exchange string, symbol string, marketType string, leverage int, size decimal.Decimal) (TradeSafetyDecision, error) {
	exchanges := []string{}
	if exchange != "" {
		exchanges = []string{exchange}
	}
	snapshot, err := s.GetPortfolioSnapshot(ctx, chatID, exchanges)
	if err != nil {
		return TradeSafetyDecision{}, fmt.Errorf("failed to get portfolio snapshot: %w", err)
	}

	status, err := s.CheckSafety(ctx, chatID, snapshot)
	if err != nil {
		return TradeSafetyDecision{}, fmt.Errorf("failed to check safety: %w", err)
	}

	if !status.TradingAllowed {
		return TradeSafetyDecision{
			Allowed: false,
			Reason:  fmt.Sprintf("Trading not allowed: %v", status.Reasons),
		}, nil
	}

	scopedTotal, scopedAvailable, hasScopedFunds := s.resolveScopedMarketFunds(snapshot, exchange, marketType)
	equityRef := snapshot.TotalEquity
	availableRef := snapshot.AvailableFunds
	if hasScopedFunds {
		if scopedTotal.GreaterThan(decimal.Zero) {
			equityRef = scopedTotal
		}
		if scopedAvailable.GreaterThan(decimal.Zero) {
			availableRef = scopedAvailable
		}
	}
	minNotional := exchangeMinExecutableNotional(strings.TrimSpace(strings.ToLower(exchange)), symbol, marketType)
	if minNotional.GreaterThan(decimal.Zero) && size.GreaterThan(decimal.Zero) && size.LessThan(minNotional) {
		return TradeSafetyDecision{
			Allowed:     false,
			Reason:      fmt.Sprintf("Position size %s is below exchange minimum notional %s", size.StringFixed(2), minNotional.StringFixed(2)),
			MinNotional: minNotional,
		}, nil
	}

	effectiveMaxPosition := s.resolveEffectiveMaxPositionSize(exchange, symbol, marketType, equityRef, status.MaxPositionSize)
	if strings.EqualFold(strings.TrimSpace(marketType), "futures") {
		if leverage <= 0 {
			leverage = 1
		}
		if minNotional.GreaterThan(effectiveMaxPosition) &&
			equityRef.GreaterThan(decimal.Zero) &&
			s.config.MaxPositionFloorPct > 0 {
			requiredPct := minNotional.Div(equityRef)
			if requiredPct.LessThanOrEqual(decimal.NewFromFloat(s.config.MaxPositionFloorPct)) {
				requiredMargin := minNotional.Div(decimal.NewFromInt(int64(leverage)))
				if availableRef.GreaterThanOrEqual(requiredMargin) {
					effectiveMaxPosition = minNotional
				}
			}
		}
		if availableRef.GreaterThan(decimal.Zero) {
			liquidityCap := availableRef.Mul(decimal.NewFromInt(int64(leverage)))
			if liquidityCap.GreaterThan(decimal.Zero) && effectiveMaxPosition.GreaterThan(liquidityCap) {
				effectiveMaxPosition = liquidityCap
			}
		}
	} else if availableRef.GreaterThan(decimal.Zero) && effectiveMaxPosition.GreaterThan(availableRef) {
		effectiveMaxPosition = availableRef
	}
	if !effectiveMaxPosition.GreaterThan(decimal.Zero) &&
		minNotional.GreaterThan(decimal.Zero) &&
		snapshot != nil &&
		equityRef.GreaterThanOrEqual(minNotional) {
		effectiveMaxPosition = minNotional
		if s.logger != nil {
			s.logger.Warn("Applying exchange minimum notional fallback for zero max position",
				"chat_id", chatID,
				"exchange", exchange,
				"symbol", symbol,
				"market_type", marketType,
				"min_notional", minNotional.StringFixed(2),
				"total_equity", equityRef.StringFixed(2),
				"available_funds", availableRef.StringFixed(2))
		}
	}
	effectiveThrottlePct := resolveEffectiveThrottlePct(status.MaxPositionSize, effectiveMaxPosition)
	throttleLabel := formatEffectiveThrottleLabel(effectiveThrottlePct)

	if !effectiveMaxPosition.GreaterThan(decimal.Zero) &&
		minNotional.GreaterThan(decimal.Zero) &&
		strings.EqualFold(strings.TrimSpace(exchange), "bitget") &&
		strings.EqualFold(strings.TrimSpace(marketType), "futures") &&
		size.LessThanOrEqual(minNotional) {
		// Exchange-side margin/notional validation is treated as the final safety
		// net when the balance snapshot is temporarily unreliable and produced a
		// zero effective max for the smallest executable futures order. This path
		// intentionally allows the minimum executable Bitget futures order even if
		// the cached equity snapshot is still reading as zero.
		return TradeSafetyDecision{
			Allowed:                  true,
			Reason:                   "",
			EffectiveMaxPosition:     effectiveMaxPosition,
			EffectiveThrottlePct:     effectiveThrottlePct,
			MinNotional:              minNotional,
			ZeroMaxMinNotionalBypass: true,
		}, nil
	}
	if strings.EqualFold(strings.TrimSpace(marketType), "futures") &&
		futuresSizeWithinRoundedEffectiveMax(size, effectiveMaxPosition) {
		return TradeSafetyDecision{
			Allowed:              true,
			Reason:               "",
			EffectiveMaxPosition: effectiveMaxPosition,
			EffectiveThrottlePct: effectiveThrottlePct,
			MinNotional:          minNotional,
		}, nil
	}

	if size.GreaterThan(effectiveMaxPosition) {
		return TradeSafetyDecision{
			Allowed:              false,
			Reason:               fmt.Sprintf("Position size %s exceeds maximum allowed %s (%s %.0f%%)", size.StringFixed(2), effectiveMaxPosition.StringFixed(2), throttleLabel, effectiveThrottlePct),
			EffectiveMaxPosition: effectiveMaxPosition,
			EffectiveThrottlePct: effectiveThrottlePct,
			MinNotional:          minNotional,
		}, nil
	}

	return TradeSafetyDecision{
		Allowed:              true,
		Reason:               "",
		EffectiveMaxPosition: effectiveMaxPosition,
		EffectiveThrottlePct: effectiveThrottlePct,
		MinNotional:          minNotional,
	}, nil
}

func (s *PortfolioSafetyService) resolveScopedMarketFunds(snapshot *SafetyPortfolioSnapshot, exchange string, marketType string) (decimal.Decimal, decimal.Decimal, bool) {
	if snapshot == nil || strings.TrimSpace(exchange) == "" {
		return decimal.Zero, decimal.Zero, false
	}
	balance := snapshot.balancesByExchange[exchange]
	if balance == nil {
		return decimal.Zero, decimal.Zero, false
	}

	normalizedExchange := strings.ToLower(strings.TrimSpace(exchange))
	normalizedMarketType := strings.ToLower(strings.TrimSpace(marketType))
	keys := []string{"USDT"}
	if normalizedExchange == "bitget" {
		switch normalizedMarketType {
		case "futures":
			keys = []string{"USDT_FUTURES_USDT", "USDT"}
		case "spot":
			keys = []string{"SPOT_USDT", "USDT"}
		}
	}

	for _, key := range keys {
		total := decimalFromFloatMap(balance.Total, key)
		free := decimalFromFloatMap(balance.Free, key)
		if total.GreaterThan(decimal.Zero) || free.GreaterThan(decimal.Zero) {
			if !free.GreaterThan(decimal.Zero) && !isSummaryOnlyBalanceKey(balance, key) {
				free = total
			}
			return total, free, true
		}
	}

	return decimal.Zero, decimal.Zero, false
}

func decimalFromFloatMap(values map[string]float64, key string) decimal.Decimal {
	if values == nil {
		return decimal.Zero
	}
	v, ok := values[key]
	if !ok || v <= 0 {
		return decimal.Zero
	}
	return decimal.NewFromFloat(v)
}

func cloneSafetyPortfolioSnapshot(snapshot *SafetyPortfolioSnapshot) SafetyPortfolioSnapshot {
	if snapshot == nil {
		return SafetyPortfolioSnapshot{}
	}
	clone := *snapshot
	if snapshot.ExchangeExposures != nil {
		clone.ExchangeExposures = append([]ExchangeExposure(nil), snapshot.ExchangeExposures...)
	}
	if snapshot.Positions != nil {
		clone.Positions = append([]SafetyPosition(nil), snapshot.Positions...)
	}
	if snapshot.balancesByExchange != nil {
		clone.balancesByExchange = make(map[string]*ccxt.BalanceResponse, len(snapshot.balancesByExchange))
		for exchange, balance := range snapshot.balancesByExchange {
			clone.balancesByExchange[exchange] = cloneBalanceResponse(balance)
		}
	}
	return clone
}

func cloneBalanceResponse(balance *ccxt.BalanceResponse) *ccxt.BalanceResponse {
	if balance == nil {
		return nil
	}
	clone := *balance
	clone.Total = cloneFloatMap(balance.Total)
	clone.Free = cloneFloatMap(balance.Free)
	clone.Used = cloneFloatMap(balance.Used)
	clone.Raw = cloneRawMap(balance.Raw)
	return &clone
}

func cloneFloatMap(values map[string]float64) map[string]float64 {
	if values == nil {
		return nil
	}
	clone := make(map[string]float64, len(values))
	for key, value := range values {
		clone[key] = value
	}
	return clone
}

func cloneRawMap(values map[string]interface{}) map[string]interface{} {
	if values == nil {
		return nil
	}
	clone := make(map[string]interface{}, len(values))
	for key, value := range values {
		switch typed := value.(type) {
		case map[string]interface{}:
			nested := make(map[string]interface{}, len(typed))
			for nestedKey, nestedValue := range typed {
				nested[nestedKey] = nestedValue
			}
			clone[key] = nested
		default:
			clone[key] = value
		}
	}
	return clone
}

func isSummaryOnlyBalanceKey(balance *ccxt.BalanceResponse, key string) bool {
	if balance == nil || balance.Raw == nil || strings.TrimSpace(key) == "" {
		return false
	}
	rawMap, ok := balance.Raw["summary_only_balance_keys"].(map[string]interface{})
	if !ok || rawMap == nil {
		return false
	}
	v, exists := rawMap[key]
	if !exists {
		return false
	}
	b, ok := v.(bool)
	return ok && b
}

func futuresSizeWithinRoundedEffectiveMax(size, effectiveMax decimal.Decimal) bool {
	if !size.GreaterThan(decimal.Zero) || !effectiveMax.GreaterThan(decimal.Zero) {
		return false
	}
	return size.Round(2).LessThanOrEqual(effectiveMax.Round(2))
}

// resolveEffectiveMaxPositionSize applies MaxPositionFloorPct as a guarded
// override for exchange minimum notionals. defaultMax and minNotional are both
// absolute quote-currency amounts, while the floor comparison is percentage
// based: requiredPct = minNotional / equityRef must stay within floorCapPct for
// the exchange minimum to replace the smaller policy cap.
func (s *PortfolioSafetyService) resolveEffectiveMaxPositionSize(exchange string, symbol string, marketType string, equityRef decimal.Decimal, defaultMax decimal.Decimal) decimal.Decimal {
	if !defaultMax.GreaterThan(decimal.Zero) {
		return defaultMax
	}

	minNotional := exchangeMinExecutableNotional(strings.TrimSpace(strings.ToLower(exchange)), symbol, marketType)
	if !minNotional.GreaterThan(decimal.Zero) || !minNotional.GreaterThan(defaultMax) {
		return defaultMax
	}
	if !equityRef.GreaterThan(decimal.Zero) {
		return defaultMax
	}

	requiredPct := minNotional.Div(equityRef)
	floorCapPct := s.config.MaxPositionFloorPct
	if floorCapPct <= 0 {
		return defaultMax
	}
	if requiredPct.GreaterThan(decimal.NewFromFloat(floorCapPct)) {
		return defaultMax
	}

	return minNotional
}

func exchangeMinExecutableNotional(exchange string, symbol string, marketType string) decimal.Decimal {
	switch exchange {
	case "bitget":
		if !strings.EqualFold(strings.TrimSpace(marketType), "futures") && !isLikelyFuturesInstrument(symbol) {
			return decimal.Zero
		}
		return appautonomy.BitgetFuturesMinNotional()
	default:
		return decimal.Zero
	}
}

// isLikelyFuturesInstrument is a best-effort heuristic for exchange symbols.
// It currently treats ':', 'PERP', and 'SWAP' patterns as futures-oriented,
// primarily validated against Bitget conventions; it can still misclassify some
// spot symbols or miss nonstandard futures tickers, so callers must not rely on
// it for strict market-type correctness. Callers should pass the original
// exchange symbol rather than a normalized comparison key when futures-specific
// minimum-notional handling is required.
func isLikelyFuturesInstrument(symbol string) bool {
	normalized := strings.ToUpper(strings.TrimSpace(symbol))
	if normalized == "" {
		return false
	}
	return strings.Contains(normalized, ":") || strings.Contains(normalized, "PERP") || strings.Contains(normalized, "SWAP")
}

// RecordTradeResult feeds a completed trade outcome into the position-size
// throttle so that subsequent CheckSafety calls reflect the updated multiplier.
// On a loss the throttle reduces the allowed position size; on a win it
// partially recovers. The caller should pass the current consecutive-loss
// count as tracked by ScalpingPerformance (or equivalent).
func (s *PortfolioSafetyService) RecordTradeResult(ctx context.Context, chatID string, profitable bool, consecutiveLosses int) {
	if s.positionThrottle == nil {
		return
	}
	if profitable {
		if _, err := s.positionThrottle.RecordWin(ctx, chatID); err != nil {
			if s.logger != nil {
				s.logger.Warn("Failed to record win in position throttle", "chat_id", chatID, "error", err)
			}
		}
		return
	}
	if _, err := s.positionThrottle.RecordLoss(ctx, chatID, consecutiveLosses); err != nil {
		if s.logger != nil {
			s.logger.Warn("Failed to record loss in position throttle", "chat_id", chatID, "error", err)
		}
	}
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
	s.lastSnapshotKey = ""
	s.lastSnapshot = nil
	s.lastSnapshotTime = time.Time{}
}

func (s *PortfolioSafetyService) SetConfig(config PortfolioSafetyConfig) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.config = normalizePortfolioSafetyConfig(config)
}

func (s *PortfolioSafetyService) GetConfig() PortfolioSafetyConfig {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.config
}

func normalizePortfolioSafetyConfig(config PortfolioSafetyConfig) PortfolioSafetyConfig {
	defaults := DefaultPortfolioSafetyConfig()
	if config.MaxPositionSizePct <= 0 {
		config.MaxPositionSizePct = defaults.MaxPositionSizePct
	}
	if config.MaxExposurePct <= 0 {
		config.MaxExposurePct = defaults.MaxExposurePct
	}
	if strings.TrimSpace(config.DefaultQuoteCurrency) == "" {
		config.DefaultQuoteCurrency = defaults.DefaultQuoteCurrency
	}
	if config.CacheTTL <= 0 {
		config.CacheTTL = defaults.CacheTTL
	}
	if config.StaleSnapshotFallbackTTL <= 0 {
		config.StaleSnapshotFallbackTTL = defaults.StaleSnapshotFallbackTTL
	}
	// MaxPositionFloorPct differs intentionally: negative values normalize to
	// default, while zero remains an explicit disable of the min-notional floor
	// override; see
	// TestPortfolioSafetyService_SetConfig_NormalizesDefaultsButPreservesZeroFloor.
	if config.MaxPositionFloorPct < 0 {
		config.MaxPositionFloorPct = defaults.MaxPositionFloorPct
	}
	return config
}

func resolveEffectiveThrottlePct(defaultMax decimal.Decimal, effectiveMax decimal.Decimal) float64 {
	if !defaultMax.GreaterThan(decimal.Zero) || !effectiveMax.GreaterThan(decimal.Zero) {
		return 0
	}
	return effectiveMax.Div(defaultMax).Mul(decimal.NewFromInt(100)).InexactFloat64()
}

func formatEffectiveThrottleLabel(pct float64) string {
	if pct > 100 {
		return "set to"
	}
	return "throttled to"
}

func normalizeRiskSignal(value, threshold float64) float64 {
	if threshold <= 0 {
		if value <= 0 {
			return 0
		}
		return 1
	}
	normalized := value / threshold
	switch {
	case normalized < 0:
		return 0
	case normalized > 1:
		return 1
	default:
		return normalized
	}
}

func isQuoteCurrency(currency string, quoteCurrency string) bool {
	return strings.EqualFold(strings.TrimSpace(currency), strings.TrimSpace(quoteCurrency))
}
