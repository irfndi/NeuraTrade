package scalping

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

const minComposerConfidenceSpread = 0.10

// ErrInsufficientOHLCVData is returned by ComposeSignal when the supplied
// OHLCV payload contains zero candles. Callers can detect it with
// errors.Is for retry/back-off decisions without parsing the message.
var ErrInsufficientOHLCVData = errors.New("insufficient OHLCV data for signal composition")

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
//
// Unit model (must match marketdata.OrderBookMetricsEvent):
//   - GetSpreadPct:       percentage (%). 0.05 == 0.05%, not 5%.
//   - GetImbalance1Pct:   ratio in [-1, 1] (raw (bid-ask)/(bid+ask), not %).
//   - GetLiquidityScore:  base-asset notional size, raw units.
//   - GetBidDepth1Pct,
//     GetAskDepth1Pct:    base-asset depth inside the 1% band, raw units.
//   - GetMidPrice, GetBestBid, GetBestAsk: absolute price in quote currency.
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

// ComponentWeights holds the per-component contribution weights used when
// composing a scalping signal. Weights are interpreted as ratios (0.10 == 10%).
// Sum should equal 1.0; Validate() rejects otherwise.
type ComponentWeights struct {
	Spread     decimal.Decimal
	Imbalance  decimal.Decimal
	Volatility decimal.Decimal
	Trend      decimal.Decimal
	Liquidity  decimal.Decimal
	RSI        decimal.Decimal
}

// DefaultComponentWeights returns the production default weights tuned against
// the paper-trading soak. Sum == 1.0.
func DefaultComponentWeights() ComponentWeights {
	return ComponentWeights{
		Spread:     decimal.NewFromFloat(0.10),
		Imbalance:  decimal.NewFromFloat(0.20),
		Volatility: decimal.NewFromFloat(0.10),
		Trend:      decimal.NewFromFloat(0.35),
		Liquidity:  decimal.NewFromFloat(0.10),
		RSI:        decimal.NewFromFloat(0.15),
	}
}

// Validate returns an error when any weight is negative, NaN, or the sum is
// not within sumTolerance of 1.0. A small tolerance is permitted to absorb
// float round-trip in config loaders.
func (w ComponentWeights) Validate() error {
	sumTolerance := decimal.NewFromFloat(0.001)
	one := decimal.NewFromInt(1)

	parts := []struct {
		name  string
		value decimal.Decimal
	}{
		{"spread", w.Spread},
		{"imbalance", w.Imbalance},
		{"volatility", w.Volatility},
		{"trend", w.Trend},
		{"liquidity", w.Liquidity},
		{"rsi", w.RSI},
	}
	var sum decimal.Decimal
	for _, p := range parts {
		if p.value.IsNegative() {
			return fmt.Errorf("component weight %q must be non-negative (got %s)", p.name, p.value.String())
		}
		sum = sum.Add(p.value)
	}
	diff := sum.Sub(one).Abs()
	if diff.GreaterThan(sumTolerance) {
		return fmt.Errorf("component weights must sum to 1.0 (got %s, tolerance %s)", sum.String(), sumTolerance.String())
	}
	return nil
}

// ScalpingSignalComposer composes scalping signals from OHLCV data and order book metrics.
type ScalpingSignalComposer struct {
	qualityScorer SignalQualityScorer
	weights       ComponentWeights
}

// NewScalpingSignalComposer creates a new composer with default component weights.
func NewScalpingComposer(qualityScorer SignalQualityScorer) *ScalpingSignalComposer {
	return &ScalpingSignalComposer{
		qualityScorer: qualityScorer,
		weights:       DefaultComponentWeights(),
	}
}

// NewScalpingComposerWithWeights creates a composer with caller-supplied
// component weights. Returns an error if the weights fail Validate().
func NewScalpingComposerWithWeights(qualityScorer SignalQualityScorer, weights ComponentWeights) (*ScalpingSignalComposer, error) {
	if err := weights.Validate(); err != nil {
		return nil, fmt.Errorf("invalid component weights: %w", err)
	}
	return &ScalpingSignalComposer{
		qualityScorer: qualityScorer,
		weights:       weights,
	}, nil
}

// Weights returns the active component weights (copy; safe to inspect).
func (c *ScalpingSignalComposer) Weights() ComponentWeights {
	return c.weights
}

// ComposeSignal creates a scalping signal from OHLCV data and order book metrics.
func (c *ScalpingSignalComposer) ComposeSignal(ctx context.Context, ohlcv OHLCVData, obMetrics OrderBookMetrics) (*ScalpingSignal, error) {
	if len(ohlcv.Candles) == 0 {
		return nil, fmt.Errorf("%w", ErrInsufficientOHLCVData)
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
	if comp := c.buildSpreadComponent(obMetrics); comp != nil {
		components = append(components, *comp)
	}
	if comp := c.buildImbalanceComponent(obMetrics); comp != nil {
		components = append(components, *comp)
	}
	if comp := c.buildVolatilityComponent(ohlcv.Candles); comp != nil {
		components = append(components, *comp)
	}
	if comp := c.buildTrendComponent(ohlcv.Candles); comp != nil {
		components = append(components, *comp)
	}
	if comp := c.buildLiquidityComponent(obMetrics); comp != nil {
		components = append(components, *comp)
	}
	if comp := c.buildRSIComponent(ohlcv.Candles); comp != nil {
		components = append(components, *comp)
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
		switch comp.Signal {
		case DirectionBuy:
			buyScore = buyScore.Add(comp.Weight.Mul(strengthValue))
		case DirectionSell:
			sellScore = sellScore.Add(comp.Weight.Mul(strengthValue))
		}
	}

	if totalWeight.IsZero() {
		signal.Direction = DirectionHold
		signal.Confidence = decimal.NewFromFloat(0.3)
	} else {
		buyConfidence := decimal.Zero
		sellConfidence := decimal.Zero
		if !totalWeight.IsZero() {
			buyConfidence = buyScore.Div(totalWeight)
			sellConfidence = sellScore.Div(totalWeight)
		}
		marginThreshold := decimal.NewFromFloat(minComposerConfidenceSpread)
		if buyScore.GreaterThan(sellScore) && buyConfidence.Sub(sellConfidence).GreaterThanOrEqual(marginThreshold) {
			signal.Direction = DirectionBuy
			signal.Confidence = buyConfidence
		} else if sellScore.GreaterThan(buyScore) && sellConfidence.Sub(buyConfidence).GreaterThanOrEqual(marginThreshold) {
			signal.Direction = DirectionSell
			signal.Confidence = sellConfidence
		} else {
			signal.Direction = DirectionHold
			signal.Confidence = decimal.Zero
		}
	}

	if signal.Confidence.GreaterThan(decimal.NewFromFloat(1.0)) {
		signal.Confidence = decimal.NewFromFloat(1.0)
	}

	signal.Components = components

	for _, comp := range components {
		strengthValue := decimal.NewFromFloat(0.3)
		switch comp.Strength {
		case StrengthMedium:
			strengthValue = decimal.NewFromFloat(0.6)
		case StrengthStrong:
			strengthValue = decimal.NewFromFloat(0.9)
		}

		contribution := comp.Weight.Mul(strengthValue)
		switch comp.Signal {
		case DirectionSell:
			contribution = contribution.Neg()
		case DirectionHold:
			contribution = decimal.Zero
		}
		signal.AttributionWeights[comp.Name] = contribution
	}

	if obMetrics != nil {
		signal.Microstructure = &MicrostructureContext{
			SpreadPct:      obMetrics.GetSpreadPct(),
			Imbalance1Pct:  obMetrics.GetImbalance1Pct(),
			MidPrice:       obMetrics.GetMidPrice(),
			BestBid:        obMetrics.GetBestBid(),
			BestAsk:        obMetrics.GetBestAsk(),
			LiquidityScore: obMetrics.GetLiquidityScore(),
			BidDepth1Pct:   obMetrics.GetBidDepth1Pct(),
			AskDepth1Pct:   obMetrics.GetAskDepth1Pct(),
		}
	}

	if c.qualityScorer != nil {
		signal.Quality = c.qualityScorer.Score(ctx, signal)
	}

	return signal, nil
}

func (c *ScalpingSignalComposer) buildSpreadComponent(obMetrics OrderBookMetrics) *SignalComponent {
	if obMetrics == nil {
		return nil
	}
	spread := obMetrics.GetSpreadPct()
	dir, strength := classifySpread(spread)
	return &SignalComponent{
		Name:        "spread",
		Description: "bid-ask spread",
		Value:       spread,
		Signal:      dir,
		Strength:    strength,
		Weight:      c.weights.Spread,
	}
}

func (c *ScalpingSignalComposer) buildImbalanceComponent(obMetrics OrderBookMetrics) *SignalComponent {
	if obMetrics == nil {
		return nil
	}
	imbalance := obMetrics.GetImbalance1Pct()
	dir, strength := classifyImbalance(imbalance)
	return &SignalComponent{
		Name:        "imbalance",
		Description: "order book imbalance",
		Value:       imbalance,
		Signal:      dir,
		Strength:    strength,
		Weight:      c.weights.Imbalance,
	}
}

func (c *ScalpingSignalComposer) buildVolatilityComponent(candles []OHLCVCandle) *SignalComponent {
	volatility := calculateVolatility(candles)
	dir, strength := classifyVolatility(volatility)
	return &SignalComponent{
		Name:        "volatility",
		Description: "recent price volatility",
		Value:       volatility,
		Signal:      dir,
		Strength:    strength,
		Weight:      c.weights.Volatility,
	}
}

func (c *ScalpingSignalComposer) buildTrendComponent(candles []OHLCVCandle) *SignalComponent {
	if len(candles) < 5 {
		return nil
	}
	dir, strength, value := classifyTrendEMA(candles)
	return &SignalComponent{
		Name:        "trend",
		Description: "EMA crossover trend (3/5 period)",
		Value:       value,
		Signal:      dir,
		Strength:    strength,
		Weight:      c.weights.Trend,
	}
}

func (c *ScalpingSignalComposer) buildLiquidityComponent(obMetrics OrderBookMetrics) *SignalComponent {
	if obMetrics == nil {
		return nil
	}
	score := obMetrics.GetLiquidityScore()
	dir, strength := classifyLiquidity(score)
	return &SignalComponent{
		Name:        "liquidity",
		Description: "order book liquidity score",
		Value:       score,
		Signal:      dir,
		Strength:    strength,
		Weight:      c.weights.Liquidity,
	}
}

func (c *ScalpingSignalComposer) buildRSIComponent(candles []OHLCVCandle) *SignalComponent {
	if len(candles) < 15 {
		return nil
	}
	rsi := calculateRSI(candles)
	dir, strength := classifyRSI(rsi)
	return &SignalComponent{
		Name:        "rsi",
		Description: "RSI (14-period)",
		Value:       rsi,
		Signal:      dir,
		Strength:    strength,
		Weight:      c.weights.RSI,
	}
}

func classifySpread(spread decimal.Decimal) (Direction, SignalStrength) {
	// spread is in percentage form (e.g., 0.05 means 0.05%), matching GetSpreadPct output
	// Tight spreads are favorable conditions (slight bullish lean) but NOT a strong directional signal.
	// Wide spreads indicate unfavorable conditions for scalping.
	if spread.LessThan(decimal.NewFromFloat(0.05)) {
		return DirectionBuy, StrengthMedium
	}
	if spread.LessThan(decimal.NewFromFloat(0.10)) {
		return DirectionHold, StrengthWeak
	}
	if spread.LessThan(decimal.NewFromFloat(0.20)) {
		return DirectionHold, StrengthWeak
	}
	return DirectionSell, StrengthWeak
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
	if prev.Close.IsZero() {
		return decimal.Zero
	}
	priceChange := last.Close.Sub(prev.Close).Div(prev.Close).Abs()
	return priceChange
}

func classifyVolatility(vol decimal.Decimal) (Direction, SignalStrength) {
	// Vol > 5% = dangerous for scalping; < 0.5% = too quiet
	if vol.LessThan(decimal.NewFromFloat(0.005)) {
		return DirectionHold, StrengthWeak
	}
	if vol.LessThan(decimal.NewFromFloat(0.02)) {
		return DirectionBuy, StrengthMedium
	}
	if vol.LessThan(decimal.NewFromFloat(0.05)) {
		return DirectionHold, StrengthMedium
	}
	return DirectionSell, StrengthStrong
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

func classifyTrendEMA(candles []OHLCVCandle) (Direction, SignalStrength, decimal.Decimal) {
	if len(candles) < 5 {
		return DirectionHold, StrengthWeak, decimal.Zero
	}
	shortEMA := calculateEMA(candles, 3)
	longEMA := calculateEMA(candles, 5)
	if longEMA.IsZero() {
		return DirectionHold, StrengthWeak, decimal.Zero
	}
	trendValue := shortEMA.Sub(longEMA).Div(longEMA)
	magnitude := trendValue.Abs()
	strongThreshold := decimal.NewFromFloat(0.005)
	mediumThreshold := decimal.NewFromFloat(0.001)
	if trendValue.IsPositive() {
		if magnitude.GreaterThanOrEqual(strongThreshold) {
			return DirectionBuy, StrengthStrong, trendValue
		}
		if magnitude.GreaterThanOrEqual(mediumThreshold) {
			return DirectionBuy, StrengthMedium, trendValue
		}
		return DirectionBuy, StrengthWeak, trendValue
	}
	if magnitude.GreaterThanOrEqual(strongThreshold) {
		return DirectionSell, StrengthStrong, trendValue
	}
	if magnitude.GreaterThanOrEqual(mediumThreshold) {
		return DirectionSell, StrengthMedium, trendValue
	}
	return DirectionSell, StrengthWeak, trendValue
}

func calculateEMA(candles []OHLCVCandle, period int) decimal.Decimal {
	if len(candles) < period {
		return decimal.Zero
	}
	start := len(candles) - period
	sum := decimal.Zero
	for i := start; i < len(candles); i++ {
		sum = sum.Add(candles[i].Close)
	}
	sma := sum.Div(decimal.NewFromInt(int64(period)))
	multiplier := decimal.NewFromFloat(2.0).Div(decimal.NewFromInt(int64(period + 1)))
	ema := sma
	for i := start + 1; i < len(candles); i++ {
		ema = candles[i].Close.Sub(ema).Mul(multiplier).Add(ema)
	}
	return ema
}

func calculateRSI(candles []OHLCVCandle) decimal.Decimal {
	period := 14
	if len(candles) < period+1 {
		return decimal.NewFromFloat(50)
	}
	gains := decimal.Zero
	losses := decimal.Zero
	start := len(candles) - period
	for i := start; i < len(candles); i++ {
		change := candles[i].Close.Sub(candles[i-1].Close)
		if change.IsPositive() {
			gains = gains.Add(change)
		} else {
			losses = losses.Add(change.Abs())
		}
	}
	avgGain := gains.Div(decimal.NewFromInt(int64(period)))
	avgLoss := losses.Div(decimal.NewFromInt(int64(period)))
	if avgLoss.IsZero() {
		return decimal.NewFromFloat(100)
	}
	rs := avgGain.Div(avgLoss)
	rsi := decimal.NewFromFloat(100).Sub(decimal.NewFromFloat(100).Div(decimal.NewFromInt(1).Add(rs)))
	if rsi.GreaterThan(decimal.NewFromFloat(100)) {
		return decimal.NewFromFloat(100)
	}
	if rsi.LessThan(decimal.Zero) {
		return decimal.Zero
	}
	return rsi
}

func classifyRSI(rsi decimal.Decimal) (Direction, SignalStrength) {
	if rsi.LessThan(decimal.NewFromFloat(30)) {
		return DirectionBuy, StrengthStrong
	}
	if rsi.LessThan(decimal.NewFromFloat(40)) {
		return DirectionBuy, StrengthMedium
	}
	if rsi.GreaterThan(decimal.NewFromFloat(70)) {
		return DirectionSell, StrengthStrong
	}
	if rsi.GreaterThan(decimal.NewFromFloat(60)) {
		return DirectionSell, StrengthMedium
	}
	return DirectionHold, StrengthWeak
}
