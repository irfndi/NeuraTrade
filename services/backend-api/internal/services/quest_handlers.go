package services

import (
	"context"
	"time"
)

// QuestHandlerFunc is a function that executes a quest
type QuestHandlerFunc func(ctx context.Context, quest *Quest) error

// ScalpingConfig holds configuration for scalping execution
type ScalpingConfig struct {
	MaxConcurrentPositions int           // Maximum concurrent scalp positions (default: 3)
	MaxCapitalPercent      float64       // Maximum % of capital to use (default: 5%)
	MinProfitPercent       float64       // Minimum profit % to enter (default: 0.3%)
	StopLossPercent        float64       // Stop loss % (default: 0.1%)
	TakeProfitPercent      float64       // Take profit % (default: 0.2%)
	CheckInterval          time.Duration // How often to check for opportunities
	TradingPairs           []string      // Pairs to trade (futures format: BTC/USDT:USDT)
	Exchange               string        // Exchange to trade on
	Leverage               int           // Leverage for futures (default: 5)
	TradeSizeUsd           float64       // Trade size in USDT (default: 15)
}

var DefaultScalpingConfig = ScalpingConfig{
	MaxConcurrentPositions: 3,
	MaxCapitalPercent:      5.0,
	MinProfitPercent:       0.1,
	StopLossPercent:        0.1,
	TakeProfitPercent:      0.2,
	CheckInterval:          1 * time.Minute,
	TradingPairs:           []string{"BTC/USDT:USDT", "ETH/USDT:USDT", "SOL/USDT:USDT", "BNB/USDT:USDT", "XRP/USDT:USDT"},
	Exchange:               "binance",
	Leverage:               5,
	TradeSizeUsd:           15.0,
}
