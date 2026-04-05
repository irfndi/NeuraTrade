package scalping

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

// OHLCVData provides candlestick data for signal composition.
type OHLCVData struct {
	Exchange  string
	Symbol    string
	Timeframe string
	Candles   []OHLCVCandle
}

// OHLCVCandle represents a single candle.
type OHLCVCandle struct {
	Timestamp time.Time
	Open      decimal.Decimal
	High      decimal.Decimal
	Low       decimal.Decimal
	Close     decimal.Decimal
	Volume    decimal.Decimal
}

// OrderBookMetrics provides order book metrics for signal composition.
type OrderBookMetrics interface {
	GetSpreadPct() decimal.Decimal
	GetImbalance1Pct() decimal.Decimal
	GetMidPrice() decimal.Decimal
	GetBestBid() decimal.Decimal
	GetBestAsk() decimal.Decimal
	GetLiquidityScore() decimal.Decimal
	GetBidDepth1Pct() decimal.Decimal
	GetAskDepth1Pct() decimal.Decimal
}

// SignalQualityScorer assesses signal quality.
type SignalQualityScorer interface {
	Score(ctx context.Context, signal *ScalpingSignal) *QualityAssessment
}

// ScalpingSignalComposer composes scalping signals from OHLCV data and order book metrics.
type ScalpingSignalComposer struct {
	qualityScorer SignalQualityScorer
}

// NewScalpingSignalComposer creates a new composer.
func NewScalpingComposer(qualityScorer SignalQualityScorer) *ScalpingSignalComposer {
	return &ScalpingSignalComposer{
		qualityScorer: qualityScorer,
	}
}

// ComposeSignal creates a scalping signal from OHLCV data and order book metrics.
func (c *ScalpingSignalComposer) ComposeSignal(ctx context.Context, ohlcv OHLCVData, obMetrics OrderBookMetrics) (*ScalpingSignal, error) {
	if len(ohlcv.Candles) == 0 {
		return nil, fmt.Errorf("insufficient OHLCV data for signal composition")
	}

	signal := &ScalpingSignal{
		ID:                 uuid.New().String(),
		Exchange:           ohlcv.Exchange,
		Symbol:             ohlcv.Symbol,
		GeneratedAt:        time.Now(),
		AttributionWeights: make(map[string]decimal.Decimal),
		Components:         []SignalComponent{},
	}

	var components []SignalComponent

	// 1. Spread factor
	if obMetrics != nil {
		spread := obMetrics.GetSpreadPct()
		spreadSignal, spreadStrength := classifySpread(spread)
		components = append(components, SignalComponent{
			Name:        "spread",
			Description: "bid-ask spread",
			Value:       spread,
			Signal:      spreadSignal,
			Strength:    spreadStrength,
			Weight:      decimal.NewFromFloat(0.15),
		})
	}

	// 2. Imbalance factor
	if obMetrics != nil {
		imbalance := obMetrics.GetImbalance1Pct()
		imbalanceDir, imbStrength := classifyImbalance(imbalance)
		components = append(components, SignalComponent{
			Name:        "imbalance",
			Description: "order book imbalance",
			Value:       imbalance,
			Signal:      imbalanceDir,
			Strength:    imbStrength,
			Weight:      decimal.NewFromFloat(0.20),
		})
	}

	// 3. Volatility factor
	volatility := calculateVolatility(ohlcv.Candles)
	volDirection, volStrength := classifyVolatility(volatility)
	components = append(components, SignalComponent{
		Name:        "volatility",
		Description: "recent price volatility",
		Value:       volatility,
		Signal:      volDirection,
		Strength:    volStrength,
		Weight:      decimal.NewFromFloat(0.15),
	})

	// 4. Trend factor
	if len(ohlcv.Candles) >= 2 {
		lastCandle := ohlcv.Candles[len(ohlcv.Candles)-1]
		prevCandle := ohlcv.Candles[len(ohlcv.Candles)-2]
		trendDir, trendStrength := classifyTrend(prevCandle, lastCandle)
		components = append(components, SignalComponent{
			Name:        "trend",
			Description: "short-term price trend",
			Value:       lastCandle.Close.Sub(prevCandle.Close).Div(prevCandle.Close).Mul(decimal.NewFromInt(100)),
			Signal:      trendDir,
			Strength:    trendStrength,
			Weight:      decimal.NewFromFloat(0.50),
		})
	}

	// 5. Liquidity factor
	if obMetrics != nil {
		liqScore := obMetrics.GetLiquidityScore()
		liqDir, liqStrength := classifyLiquidity(liqScore)
		components = append(components, SignalComponent{
			Name:        "liquidity",
			Description: "order book liquidity score",
			Value:       liqScore,
			Signal:      liqDir,
			Strength:    liqStrength,
			Weight:      decimal.NewFromFloat(0.10),
		})
	}

	totalWeight := decimal.Zero
	buyScore := decimal.Zero
	sellScore := decimal.Zero
	for _, comp := range components {
		if comp.Signal == DirectionHold {
			totalWeight = totalWeight.Add(comp.Weight)
			continue
		}

		strengthValue := decimal.NewFromFloat(0.3)
		switch comp.Strength {
		case StrengthMedium:
			strengthValue = decimal.NewFromFloat(0.6)
		case StrengthStrong:
			strengthValue = decimal.NewFromFloat(0.9)
		}

		totalWeight = totalWeight.Add(comp.Weight)
		if comp.Signal == DirectionBuy {
			buyScore = buyScore.Add(comp.Weight.Mul(strengthValue))
		} else if comp.Signal == DirectionSell {
			sellScore = sellScore.Add(comp.Weight.Mul(strengthValue))
		}
	}

	if totalWeight.IsZero() {
		signal.Direction = DirectionHold
		signal.Confidence = decimal.NewFromFloat(0.3)
	} else {
		if buyScore.GreaterThan(sellScore) {
			signal.Direction = DirectionBuy
			signal.Confidence = buyScore.Div(totalWeight)
		} else if sellScore.GreaterThan(buyScore) {
			signal.Direction = DirectionSell
			signal.Confidence = sellScore.Div(totalWeight)
		} else {
			signal.Direction = DirectionHold
			signal.Confidence = decimal.Zero
		}
	}

	if signal.Confidence.GreaterThan(decimal.NewFromFloat(1.0)) {
		signal.Confidence = decimal.NewFromFloat(1.0)
	}

	signal.Components = components

	// Build attribution weights
	for _, comp := range components {
		signal.AttributionWeights[comp.Name] = comp.Weight
	}

	// Build microstructure context
	if obMetrics != nil {
		signal.Microstructure = &MicrostructureContext{
			SpreadPct:      obMetrics.GetSpreadPct(),
			Imbalance1Pct:  obMetrics.GetImbalance1Pct(),
			MidPrice:       obMetrics.GetMidPrice(),
			BestBid:        obMetrics.GetBestBid(),
			BestAsk:        obMetrics.GetBestAsk(),
			LiquidityScore: obMetrics.GetLiquidityScore(),
			BidDepthUSD:    obMetrics.GetBidDepth1Pct(),
			AskDepthUSD:    obMetrics.GetAskDepth1Pct(),
		}
	}

	// Quality assessment
	if c.qualityScorer != nil {
		signal.Quality = c.qualityScorer.Score(ctx, signal)
	}

	return signal, nil
}

func classifySpread(spread decimal.Decimal) (Direction, SignalStrength) {
	if spread.LessThan(decimal.NewFromFloat(0.0005)) {
		return DirectionBuy, StrengthStrong
	}
	if spread.LessThan(decimal.NewFromFloat(0.002)) {
		return DirectionBuy, StrengthMedium
	}
	return DirectionHold, StrengthWeak
}

func classifyImbalance(imbalance decimal.Decimal) (Direction, SignalStrength) {
	threshold := decimal.NewFromFloat(0.20)
	if imbalance.GreaterThan(threshold) {
		return DirectionBuy, StrengthStrong
	}
	if imbalance.GreaterThan(decimal.NewFromFloat(0.05)) {
		return DirectionBuy, StrengthMedium
	}
	if imbalance.LessThan(threshold.Neg()) {
		return DirectionSell, StrengthStrong
	}
	if imbalance.LessThan(decimal.NewFromFloat(-0.05)) {
		return DirectionSell, StrengthMedium
	}
	return DirectionHold, StrengthWeak
}

func calculateVolatility(candles []OHLCVCandle) decimal.Decimal {
	if len(candles) < 2 {
		return decimal.Zero
	}
	last := candles[len(candles)-1]
	prev := candles[len(candles)-2]
	priceChange := last.Close.Sub(prev.Close).Div(prev.Close).Abs()
	return priceChange.Mul(decimal.NewFromInt(100))
}

func classifyVolatility(vol decimal.Decimal) (Direction, SignalStrength) {
	if vol.LessThan(decimal.NewFromFloat(0.005)) {
		return DirectionHold, StrengthWeak
	}
	if vol.LessThan(decimal.NewFromFloat(0.02)) {
		return DirectionBuy, StrengthMedium
	}
	if vol.LessThan(decimal.NewFromFloat(0.05)) {
		return DirectionBuy, StrengthStrong
	}
	return DirectionSell, StrengthStrong
}

func classifyTrend(prev, curr OHLCVCandle) (Direction, SignalStrength) {
	change := curr.Close.Sub(prev.Close)

	if change.IsZero() {
		return DirectionHold, StrengthWeak
	}

	var magnitude decimal.Decimal
	if !prev.Close.IsZero() {
		magnitude = change.Div(prev.Close).Abs()
	} else {
		magnitude = change.Abs()
	}

	strongThreshold := decimal.NewFromFloat(0.005)
	mediumThreshold := decimal.NewFromFloat(0.001)

	if change.IsPositive() {
		if magnitude.GreaterThanOrEqual(strongThreshold) {
			return DirectionBuy, StrengthStrong
		}
		if magnitude.GreaterThanOrEqual(mediumThreshold) {
			return DirectionBuy, StrengthMedium
		}
		return DirectionBuy, StrengthWeak
	}

	if magnitude.GreaterThanOrEqual(strongThreshold) {
		return DirectionSell, StrengthStrong
	}
	if magnitude.GreaterThanOrEqual(mediumThreshold) {
		return DirectionSell, StrengthMedium
	}
	return DirectionSell, StrengthWeak
}

func classifyLiquidity(score decimal.Decimal) (Direction, SignalStrength) {
	if score.GreaterThan(decimal.NewFromFloat(70)) {
		return DirectionBuy, StrengthStrong
	}
	if score.GreaterThan(decimal.NewFromFloat(40)) {
		return DirectionBuy, StrengthMedium
	}
	return DirectionHold, StrengthWeak
}
