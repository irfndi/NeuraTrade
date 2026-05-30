package services

import (
	"context"
	"sync"
	"time"

	zaplogrus "github.com/irfndi/neuratrade/internal/logging/zaplogrus"

	"github.com/shopspring/decimal"
)

type ScalpingPerformance struct {
	mu                sync.RWMutex
	totalTrades       int
	profitableTrades  int
	losingTrades      int
	totalPnL          decimal.Decimal
	lastTradeTime     time.Time
	winRate           float64
	avgWinAmount      decimal.Decimal
	avgLossAmount     decimal.Decimal
	consecutiveWins   int
	consecutiveLosses int
	parameters        ScalpingConfig
	history           []TradeRecord
}

type TradeRecord struct {
	Timestamp    time.Time
	Symbol       string
	Side         string
	Amount       decimal.Decimal
	PnL          decimal.Decimal
	Profitable   bool
	ExitPrice    decimal.Decimal
	EntryPrice   decimal.Decimal
	Notional     decimal.Decimal
	HoldDuration time.Duration
}

func NewScalpingPerformance() *ScalpingPerformance {
	return &ScalpingPerformance{
		parameters: DefaultScalpingConfig,
		history:    make([]TradeRecord, 0),
	}
}

func (sp *ScalpingPerformance) RecordTrade(record TradeRecord) {
	sp.mu.Lock()
	defer sp.mu.Unlock()

	sp.totalTrades++
	sp.lastTradeTime = record.Timestamp

	if record.Profitable {
		sp.profitableTrades++
		sp.consecutiveWins++
		sp.consecutiveLosses = 0

		if sp.avgWinAmount.IsZero() {
			sp.avgWinAmount = record.PnL
		} else {
			sp.avgWinAmount = sp.avgWinAmount.Mul(decimal.NewFromFloat(0.9)).Add(record.PnL.Mul(decimal.NewFromFloat(0.1)))
		}
	} else {
		sp.losingTrades++
		sp.consecutiveLosses++
		sp.consecutiveWins = 0

		if sp.avgLossAmount.IsZero() {
			sp.avgLossAmount = record.PnL.Abs()
		} else {
			sp.avgLossAmount = sp.avgLossAmount.Mul(decimal.NewFromFloat(0.9)).Add(record.PnL.Abs().Mul(decimal.NewFromFloat(0.1)))
		}
	}

	sp.totalPnL = sp.totalPnL.Add(record.PnL)

	if sp.totalTrades > 0 {
		sp.winRate = float64(sp.profitableTrades) / float64(sp.totalTrades)
	}

	sp.history = append(sp.history, record)
	if len(sp.history) > 100 {
		sp.history = sp.history[len(sp.history)-100:]
	}

	zaplogrus.Infof("Scalping performance: trades=%d, win_rate=%.2f%%, pnl=%s, consecutive=%d/%d",
		sp.totalTrades, sp.winRate*100, sp.totalPnL.String(), sp.consecutiveWins, sp.consecutiveLosses)
}

func (sp *ScalpingPerformance) GetPerformance() map[string]interface{} {
	sp.mu.RLock()
	defer sp.mu.RUnlock()

	return map[string]interface{}{
		"total_trades":       sp.totalTrades,
		"profitable_trades":  sp.profitableTrades,
		"losing_trades":      sp.losingTrades,
		"win_rate":           sp.winRate,
		"total_pnl":          sp.totalPnL.String(),
		"avg_win":            sp.avgWinAmount.String(),
		"avg_loss":           sp.avgLossAmount.String(),
		"consecutive_wins":   sp.consecutiveWins,
		"consecutive_losses": sp.consecutiveLosses,
		"last_trade_time":    sp.lastTradeTime.Format(time.RFC3339),
	}
}

func (sp *ScalpingPerformance) GetReturnSeries(limit int) []decimal.Decimal {
	sp.mu.RLock()
	defer sp.mu.RUnlock()

	if len(sp.history) == 0 {
		return []decimal.Decimal{}
	}
	if limit <= 0 || limit > len(sp.history) {
		limit = len(sp.history)
	}
	start := len(sp.history) - limit
	series := make([]decimal.Decimal, 0, limit)
	for _, trade := range sp.history[start:] {
		notional := trade.Notional.Abs()
		if notional.LessThanOrEqual(decimal.Zero) {
			notional = trade.EntryPrice.Abs().Mul(trade.Amount.Abs())
		}
		if notional.LessThanOrEqual(decimal.Zero) {
			notional = trade.ExitPrice.Abs().Mul(trade.Amount.Abs())
		}
		if notional.LessThanOrEqual(decimal.Zero) {
			continue
		}
		ret := trade.PnL.Div(notional)
		series = append(series, ret)
	}
	return series
}

func (sp *ScalpingPerformance) GetAdjustedParameters() ScalpingConfig {
	sp.mu.Lock()
	defer sp.mu.Unlock()

	adjusted := sp.parameters

	if sp.totalTrades < 10 {
		return adjusted
	}

	if sp.consecutiveLosses >= 3 {
		adjusted.MinProfitPercent = sp.parameters.MinProfitPercent * 1.5
		adjusted.MaxCapitalPercent = sp.parameters.MaxCapitalPercent * 0.5
		zaplogrus.Infof("Self-learning: Consecutive losses detected - tightening parameters")
	} else if sp.consecutiveWins >= 3 {
		adjusted.MinProfitPercent = sp.parameters.MinProfitPercent * 0.8
		adjusted.MaxCapitalPercent = sp.parameters.MaxCapitalPercent * 1.2
		if adjusted.MaxCapitalPercent > 20 {
			adjusted.MaxCapitalPercent = 20
		}
		zaplogrus.Infof("Self-learning: Consecutive wins detected - loosening parameters")
	}

	if sp.winRate < 0.3 && sp.totalTrades >= 20 {
		adjusted.MinProfitPercent = sp.parameters.MinProfitPercent * 2
		adjusted.MaxConcurrentPositions = sp.parameters.MaxConcurrentPositions / 2
		if adjusted.MaxConcurrentPositions < 1 {
			adjusted.MaxConcurrentPositions = 1
		}
		zaplogrus.Infof("Self-learning: Low win rate (%.1f%%) - dramatically tightening parameters", sp.winRate*100)
	} else if sp.winRate > 0.6 && sp.totalTrades >= 20 {
		adjusted.MinProfitPercent = sp.parameters.MinProfitPercent * 0.7
		adjusted.MaxConcurrentPositions = sp.parameters.MaxConcurrentPositions + 1
		zaplogrus.Infof("Self-learning: High win rate (%.1f%%) - more aggressive parameters", sp.winRate*100)
	}

	sp.parameters = adjusted
	return adjusted
}

var globalScalpingPerformance = NewScalpingPerformance()

func GetScalpingPerformance() *ScalpingPerformance {
	return globalScalpingPerformance
}

func RecordScalpingTrade(ctx context.Context, symbol, side string, amount, entryPrice, exitPrice decimal.Decimal, profitable bool) {
	notional := entryPrice.Abs().Mul(amount.Abs())
	if notional.LessThanOrEqual(decimal.Zero) {
		notional = exitPrice.Abs().Mul(amount.Abs())
	}
	record := TradeRecord{
		Timestamp:    time.Now(),
		Symbol:       symbol,
		Side:         side,
		Amount:       amount,
		EntryPrice:   entryPrice,
		ExitPrice:    exitPrice,
		Notional:     notional,
		Profitable:   profitable,
		HoldDuration: time.Minute * 5,
	}

	if profitable {
		record.PnL = exitPrice.Sub(entryPrice).Mul(amount)
	} else {
		record.PnL = entryPrice.Sub(exitPrice).Mul(amount).Neg()
	}

	globalScalpingPerformance.RecordTrade(record)
}
