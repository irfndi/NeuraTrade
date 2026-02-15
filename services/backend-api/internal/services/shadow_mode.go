package services

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/shopspring/decimal"
	"go.uber.org/zap"
)

type ShadowModeConfig struct {
	Enabled           bool
	InitialCapital    decimal.Decimal
	CommissionPercent decimal.Decimal
	SlippagePercent   decimal.Decimal
	TrackPositions    bool
	RecordTrades      bool
}

func DefaultShadowModeConfig() ShadowModeConfig {
	return ShadowModeConfig{
		Enabled:           false,
		InitialCapital:    decimal.NewFromInt(10000),
		CommissionPercent: decimal.NewFromFloat(0.001),
		SlippagePercent:   decimal.NewFromFloat(0.0005),
		TrackPositions:    true,
		RecordTrades:      true,
	}
}

type ShadowPosition struct {
	Symbol        string          `json:"symbol"`
	Side          string          `json:"side"`
	Quantity      decimal.Decimal `json:"quantity"`
	EntryPrice    decimal.Decimal `json:"entry_price"`
	CurrentPrice  decimal.Decimal `json:"current_price"`
	UnrealizedPNL decimal.Decimal `json:"unrealized_pnl"`
	OpenedAt      time.Time       `json:"opened_at"`
}

type ShadowTrade struct {
	ID         string          `json:"id"`
	Symbol     string          `json:"symbol"`
	Side       string          `json:"side"`
	Quantity   decimal.Decimal `json:"quantity"`
	Price      decimal.Decimal `json:"price"`
	Commission decimal.Decimal `json:"commission"`
	Slippage   decimal.Decimal `json:"slippage"`
	TotalValue decimal.Decimal `json:"total_value"`
	ExecutedAt time.Time       `json:"executed_at"`
	RealTrade  bool            `json:"real_trade"`
}

type ShadowPortfolio struct {
	Cash          decimal.Decimal            `json:"cash"`
	Positions     map[string]*ShadowPosition `json:"positions"`
	TotalValue    decimal.Decimal            `json:"total_value"`
	RealizedPNL   decimal.Decimal            `json:"realized_pnl"`
	UnrealizedPNL decimal.Decimal            `json:"unrealized_pnl"`
	LastUpdated   time.Time                  `json:"last_updated"`
}

type ShadowModeEngine struct {
	config  ShadowModeConfig
	logger  *zap.Logger
	mu      sync.RWMutex
	enabled bool

	portfolio   ShadowPortfolio
	trades      []ShadowTrade
	history     []ShadowPortfolio
	nextTradeID uint64
}

func NewShadowModeEngine(config ShadowModeConfig, logger *zap.Logger) *ShadowModeEngine {
	if config.InitialCapital.IsZero() {
		config.InitialCapital = decimal.NewFromInt(10000)
	}

	if logger == nil {
		logger = zap.NewNop()
	}

	return &ShadowModeEngine{
		config: config,
		logger: logger,
		portfolio: ShadowPortfolio{
			Cash:      config.InitialCapital,
			Positions: make(map[string]*ShadowPosition),
		},
		trades:  make([]ShadowTrade, 0),
		history: make([]ShadowPortfolio, 0),
	}
}

func (e *ShadowModeEngine) Enable() {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.enabled = true
	e.config.Enabled = true
	e.logger.Info("Shadow mode enabled")
}

func (e *ShadowModeEngine) Disable() {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.enabled = false
	e.config.Enabled = false
	e.logger.Info("Shadow mode disabled")
}

func (e *ShadowModeEngine) IsEnabled() bool {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.enabled
}

func (e *ShadowModeEngine) ExecuteTrade(ctx context.Context, symbol, side string, quantity, price decimal.Decimal) (*ShadowTrade, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	if !e.enabled {
		return nil, fmt.Errorf("shadow mode is not enabled")
	}

	commission := price.Mul(quantity).Mul(e.config.CommissionPercent)
	slippage := price.Mul(quantity).Mul(e.config.SlippagePercent)
	totalValue := price.Mul(quantity).Add(commission).Add(slippage)

	trade := ShadowTrade{
		ID:         fmt.Sprintf("shadow_%d", e.nextTradeID),
		Symbol:     symbol,
		Side:       side,
		Quantity:   quantity,
		Price:      price,
		Commission: commission,
		Slippage:   slippage,
		TotalValue: totalValue,
		ExecutedAt: time.Now(),
		RealTrade:  false,
	}
	e.nextTradeID++

	switch side {
	case "buy":
		if e.portfolio.Cash.LessThan(totalValue) {
			return nil, fmt.Errorf("insufficient funds: have %s, need %s", e.portfolio.Cash.String(), totalValue.String())
		}

		e.portfolio.Cash = e.portfolio.Cash.Sub(totalValue)

		if pos, exists := e.portfolio.Positions[symbol]; exists {
			newQty := pos.Quantity.Add(quantity)
			newAvg := pos.EntryPrice.Mul(pos.Quantity).Add(price.Mul(quantity)).Div(newQty)
			pos.Quantity = newQty
			pos.EntryPrice = newAvg
			pos.UnrealizedPNL = price.Sub(pos.EntryPrice).Mul(pos.Quantity)
		} else {
			e.portfolio.Positions[symbol] = &ShadowPosition{
				Symbol:        symbol,
				Side:          "long",
				Quantity:      quantity,
				EntryPrice:    price,
				CurrentPrice:  price,
				UnrealizedPNL: decimal.Zero,
				OpenedAt:      time.Now(),
			}
		}
	case "sell":
		if pos, exists := e.portfolio.Positions[symbol]; exists {
			if pos.Quantity.LessThan(quantity) {
				return nil, fmt.Errorf("insufficient position: have %s, need %s", pos.Quantity.String(), quantity.String())
			}

			grossProceeds := price.Mul(quantity)
			e.portfolio.Cash = e.portfolio.Cash.Add(grossProceeds).Sub(commission).Sub(slippage)

			pnl := price.Sub(pos.EntryPrice).Mul(quantity)
			e.portfolio.RealizedPNL = e.portfolio.RealizedPNL.Add(pnl)

			pos.Quantity = pos.Quantity.Sub(quantity)
			if pos.Quantity.IsZero() {
				delete(e.portfolio.Positions, symbol)
			} else {
				pos.UnrealizedPNL = price.Sub(pos.EntryPrice).Mul(pos.Quantity)
			}
		} else {
			return nil, fmt.Errorf("no position to sell for %s", symbol)
		}
	default:
		return nil, fmt.Errorf("unknown side: %s", side)
	}

	e.trades = append(e.trades, trade)

	return &trade, nil
}

func (e *ShadowModeEngine) UpdatePrices(prices map[string]decimal.Decimal) {
	e.mu.Lock()
	defer e.mu.Unlock()

	var totalUnrealized, sumMarketValue decimal.Decimal

	for symbol, pos := range e.portfolio.Positions {
		if price, exists := prices[symbol]; exists {
			pos.CurrentPrice = price
			pos.UnrealizedPNL = price.Sub(pos.EntryPrice).Mul(pos.Quantity)
		}
		// Always add market value for all positions (use current price)
		sumMarketValue = sumMarketValue.Add(pos.CurrentPrice.Mul(pos.Quantity))
		totalUnrealized = totalUnrealized.Add(pos.UnrealizedPNL)
	}

	e.portfolio.UnrealizedPNL = totalUnrealized
	e.portfolio.TotalValue = e.portfolio.Cash.Add(sumMarketValue)
	e.portfolio.LastUpdated = time.Now()
}

func (e *ShadowModeEngine) GetPortfolio() ShadowPortfolio {
	e.mu.RLock()
	defer e.mu.RUnlock()

	portfolio := e.portfolio
	positions := make(map[string]*ShadowPosition, len(e.portfolio.Positions))
	for k, v := range e.portfolio.Positions {
		posCopy := *v
		positions[k] = &posCopy
	}
	portfolio.Positions = positions

	return portfolio
}

func (e *ShadowModeEngine) GetTrades(limit int) []ShadowTrade {
	e.mu.RLock()
	defer e.mu.RUnlock()

	if limit <= 0 || limit > len(e.trades) {
		limit = len(e.trades)
	}

	result := make([]ShadowTrade, limit)
	copy(result, e.trades[len(e.trades)-limit:])
	return result
}

func (e *ShadowModeEngine) GetHistory() []ShadowPortfolio {
	e.mu.RLock()
	defer e.mu.RUnlock()

	history := make([]ShadowPortfolio, len(e.history))
	copy(history, e.history)
	return history
}

func (e *ShadowModeEngine) Reset() {
	e.mu.Lock()
	defer e.mu.Unlock()

	e.portfolio = ShadowPortfolio{
		Cash:       e.config.InitialCapital,
		Positions:  make(map[string]*ShadowPosition),
		TotalValue: e.config.InitialCapital,
	}
	e.trades = make([]ShadowTrade, 0)
	e.history = make([]ShadowPortfolio, 0)

	e.logger.Info("Shadow mode portfolio reset")
}

func (e *ShadowModeEngine) RecordSnapshot() {
	e.mu.Lock()
	defer e.mu.Unlock()

	// Deep copy the portfolio to avoid retaining pointers to live positions
	snapshot := e.portfolio
	snapshot.Positions = make(map[string]*ShadowPosition, len(e.portfolio.Positions))
	for k, v := range e.portfolio.Positions {
		posCopy := *v
		snapshot.Positions[k] = &posCopy
	}

	e.history = append(e.history, snapshot)

	if len(e.history) > 1000 {
		e.history = e.history[len(e.history)-1000:]
	}
}

func (e *ShadowModeEngine) GetStats() map[string]interface{} {
	e.mu.RLock()
	defer e.mu.RUnlock()

	return map[string]interface{}{
		"enabled":        e.enabled,
		"cash":           e.portfolio.Cash.String(),
		"total_value":    e.portfolio.TotalValue.String(),
		"realized_pnl":   e.portfolio.RealizedPNL.String(),
		"unrealized_pnl": e.portfolio.UnrealizedPNL.String(),
		"position_count": len(e.portfolio.Positions),
		"trade_count":    len(e.trades),
	}
}
