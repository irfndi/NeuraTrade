package services

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/shopspring/decimal"
)

type AntiManipulationFilterConfig struct {
	WashTradeThreshold   decimal.Decimal
	SpoofingOrderSize    decimal.Decimal
	SpoofingMinOrders    int
	SpoofingCancelWindow time.Duration
	LayeringMinLevels    int
	MinDataPoints        int
}

type AntiManipulationResult struct {
	IsWashTrade      bool                   `json:"is_wash_trade"`
	WashTradeScore   decimal.Decimal        `json:"wash_trade_score"`
	IsSpoofing       bool                   `json:"is_spoofing"`
	SpoofingScore    decimal.Decimal        `json:"spoofing_score"`
	IsLayering       bool                   `json:"is_layering"`
	LayeringScore    decimal.Decimal        `json:"layering_score"`
	IsManipulated    bool                   `json:"is_manipulated"`
	ManipulationType string                 `json:"manipulation_type"`
	Confidence       decimal.Decimal        `json:"confidence"`
	Details          map[string]interface{} `json:"details"`
}

type AntiManipulationFilter struct {
	config *AntiManipulationFilterConfig
	db     DBPool
	logger Logger
	mu     sync.RWMutex
}

func NewAntiManipulationFilter(db DBPool, logger Logger, config *AntiManipulationFilterConfig) *AntiManipulationFilter {
	if config == nil {
		config = &AntiManipulationFilterConfig{
			WashTradeThreshold:   decimal.NewFromFloat(5.0),
			SpoofingOrderSize:    decimal.NewFromFloat(10000),
			SpoofingMinOrders:    3,
			SpoofingCancelWindow: 5 * time.Minute,
			LayeringMinLevels:    3,
			MinDataPoints:        10,
		}
	}

	return &AntiManipulationFilter{
		config: config,
		db:     db,
		logger: logger,
	}
}

func (amf *AntiManipulationFilter) FilterSignal(
	ctx context.Context,
	symbol string,
	exchange string,
	volume decimal.Decimal,
	price decimal.Decimal,
	orderBookData map[string]interface{},
) (*AntiManipulationResult, error) {
	result := &AntiManipulationResult{
		Details: make(map[string]interface{}),
	}

	historicalVolume, err := amf.getAverageVolume(ctx, symbol, exchange, 24*time.Hour)
	if err != nil {
		amf.logger.WithFields(map[string]interface{}{
			"symbol":   symbol,
			"exchange": exchange,
		}).Warn("Failed to get historical volume data")
		return result, nil
	}

	washTradeResult := amf.detectWashTrading(volume, historicalVolume)
	result.IsWashTrade = washTradeResult.IsDetected
	result.WashTradeScore = washTradeResult.Score
	result.Details["wash_trade"] = washTradeResult

	if orderBookData != nil {
		spoofingResult := amf.detectSpoofing(orderBookData)
		result.IsSpoofing = spoofingResult.IsDetected
		result.SpoofingScore = spoofingResult.Score
		result.Details["spoofing"] = spoofingResult
	}

	if orderBookData != nil {
		layeringResult := amf.detectLayering(orderBookData)
		result.IsLayering = layeringResult.IsDetected
		result.LayeringScore = layeringResult.Score
		result.Details["layering"] = layeringResult
	}

	result.IsManipulated = result.IsWashTrade || result.IsSpoofing || result.IsLayering

	if result.IsWashTrade {
		result.ManipulationType = "wash_trade"
	} else if result.IsSpoofing {
		result.ManipulationType = "spoofing"
	} else if result.IsLayering {
		result.ManipulationType = "layering"
	} else {
		result.ManipulationType = "none"
	}

	var totalScore decimal.Decimal
	var count int
	if result.WashTradeScore.GreaterThan(decimal.Zero) {
		totalScore = totalScore.Add(result.WashTradeScore)
		count++
	}
	if result.SpoofingScore.GreaterThan(decimal.Zero) {
		totalScore = totalScore.Add(result.SpoofingScore)
		count++
	}
	if result.LayeringScore.GreaterThan(decimal.Zero) {
		totalScore = totalScore.Add(result.LayeringScore)
		count++
	}

	if count > 0 {
		result.Confidence = totalScore.Div(decimal.NewFromInt(int64(count)))
	} else {
		result.Confidence = decimal.Zero
	}

	return result, nil
}

type WashTradeDetection struct {
	IsDetected    bool            `json:"is_detected"`
	Score         decimal.Decimal `json:"score"`
	VolumeRatio   decimal.Decimal `json:"volume_ratio"`
	CurrentVolume decimal.Decimal `json:"current_volume"`
	AvgVolume     decimal.Decimal `json:"avg_volume"`
}

func (amf *AntiManipulationFilter) detectWashTrading(
	currentVolume decimal.Decimal,
	avgVolume24h decimal.Decimal,
) *WashTradeDetection {
	result := &WashTradeDetection{
		CurrentVolume: currentVolume,
		AvgVolume:     avgVolume24h,
	}

	if avgVolume24h.IsZero() {
		result.IsDetected = false
		result.Score = decimal.Zero
		return result
	}

	volumeRatio := currentVolume.Div(avgVolume24h)
	result.VolumeRatio = volumeRatio

	if volumeRatio.GreaterThanOrEqual(amf.config.WashTradeThreshold) {
		result.IsDetected = true
		excess := volumeRatio.Div(amf.config.WashTradeThreshold)
		if excess.GreaterThan(decimal.NewFromFloat(1.0)) {
			excess = decimal.NewFromFloat(1.0)
		}
		result.Score = excess
	} else {
		result.IsDetected = false
		result.Score = decimal.Zero
	}

	return result
}

type SpoofingDetection struct {
	IsDetected  bool              `json:"is_detected"`
	Score       decimal.Decimal   `json:"score"`
	LargeOrders []decimal.Decimal `json:"large_orders"`
	CancelRate  decimal.Decimal   `json:"cancel_rate"`
	OrderCount  int               `json:"order_count"`
}

func (amf *AntiManipulationFilter) detectSpoofing(orderBookData map[string]interface{}) *SpoofingDetection {
	result := &SpoofingDetection{}

	bids, _ := orderBookData["bids"].([]interface{})
	asks, _ := orderBookData["asks"].([]interface{})

	if bids == nil && asks == nil {
		return result
	}

	var largeOrders []decimal.Decimal

	for _, bid := range bids {
		bidMap, ok := bid.(map[string]interface{})
		if !ok {
			continue
		}
		size, _ := bidMap["size"].(float64)
		if size >= amf.config.SpoofingOrderSize.InexactFloat64() {
			largeOrders = append(largeOrders, decimal.NewFromFloat(size))
		}
	}

	for _, ask := range asks {
		askMap, ok := ask.(map[string]interface{})
		if !ok {
			continue
		}
		size, _ := askMap["size"].(float64)
		if size >= amf.config.SpoofingOrderSize.InexactFloat64() {
			largeOrders = append(largeOrders, decimal.NewFromFloat(size))
		}
	}

	result.LargeOrders = largeOrders
	result.OrderCount = len(largeOrders)

	if len(largeOrders) >= amf.config.SpoofingMinOrders {
		result.IsDetected = true
		ratio := decimal.NewFromInt(int64(len(largeOrders))).Div(decimal.NewFromInt(10))
		if ratio.GreaterThan(decimal.NewFromFloat(1.0)) {
			ratio = decimal.NewFromFloat(1.0)
		}
		result.Score = ratio
	}

	return result
}

type LayeringDetection struct {
	IsDetected  bool            `json:"is_detected"`
	Score       decimal.Decimal `json:"score"`
	LevelCount  int             `json:"level_count"`
	OrderCounts map[int]int     `json:"order_counts"`
}

func (amf *AntiManipulationFilter) detectLayering(orderBookData map[string]interface{}) *LayeringDetection {
	result := &LayeringDetection{
		OrderCounts: make(map[int]int),
	}

	bids, _ := orderBookData["bids"].([]interface{})
	asks, _ := orderBookData["asks"].([]interface{})

	bidLevels := amf.analyzePriceLevels(bids)
	for level, count := range bidLevels {
		result.OrderCounts[level] = count
		if count >= amf.config.LayeringMinLevels {
			result.LevelCount++
		}
	}

	askLevels := amf.analyzePriceLevels(asks)
	for level, count := range askLevels {
		result.OrderCounts[level] = count
		if count >= amf.config.LayeringMinLevels {
			result.LevelCount++
		}
	}

	if result.LevelCount >= amf.config.LayeringMinLevels {
		result.IsDetected = true
		ratio := decimal.NewFromInt(int64(result.LevelCount)).Div(decimal.NewFromInt(10))
		if ratio.GreaterThan(decimal.NewFromFloat(1.0)) {
			ratio = decimal.NewFromFloat(1.0)
		}
		result.Score = ratio
	}

	return result
}

func (amf *AntiManipulationFilter) analyzePriceLevels(orders []interface{}) map[int]int {
	levels := make(map[int]int)

	for _, order := range orders {
		orderMap, ok := order.(map[string]interface{})
		if !ok {
			continue
		}

		price, ok := orderMap["price"].(float64)
		if !ok {
			continue
		}

		level := int(price / 100)
		levels[level]++
	}

	return levels
}

func (amf *AntiManipulationFilter) getAverageVolume(ctx context.Context, symbol, exchange string, duration time.Duration) (decimal.Decimal, error) {
	since := time.Now().Add(-duration)

	query := `
		SELECT CAST(AVG(md.volume_24h) AS VARCHAR) as avg_volume
		FROM market_data md
		JOIN trading_pairs tp ON md.trading_pair_id = tp.id
		JOIN exchanges e ON md.exchange_id = e.id
		WHERE tp.symbol = $1 AND e.ccxt_id = $2 AND md.timestamp >= $3
		LIMIT 1
	`

	var avgVolumeStr *string
	err := amf.db.QueryRow(ctx, query, symbol, exchange, since).Scan(&avgVolumeStr)
	if err != nil {
		return decimal.Zero, fmt.Errorf("get average volume for %s/%s: %w", symbol, exchange, err)
	}
	if avgVolumeStr == nil || *avgVolumeStr == "" {
		return decimal.Zero, nil
	}

	return decimal.NewFromString(*avgVolumeStr)
}

func (amf *AntiManipulationFilter) GetConfig() *AntiManipulationFilterConfig {
	amf.mu.RLock()
	defer amf.mu.RUnlock()
	return amf.config
}

func (amf *AntiManipulationFilter) UpdateConfig(config *AntiManipulationFilterConfig) {
	amf.mu.Lock()
	defer amf.mu.Unlock()
	amf.config = config
}
