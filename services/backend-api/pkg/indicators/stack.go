package indicators

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/shopspring/decimal"
	"go.uber.org/zap"
)

// MultiIndicatorStack combines multiple technical indicators to produce
// aggregated trading signals with confidence scores.
type MultiIndicatorStack struct {
	provider IndicatorProvider
	config   *IndicatorConfig
	logger   *zap.Logger
}

// NewMultiIndicatorStack creates a new multi-indicator analysis stack.
func NewMultiIndicatorStack(provider IndicatorProvider, config *IndicatorConfig, logger *zap.Logger) *MultiIndicatorStack {
	if config == nil {
		config = DefaultIndicatorConfig()
	}
	if logger == nil {
		logger = zap.NewNop()
	}
	return &MultiIndicatorStack{
		provider: provider,
		config:   config,
		logger:   logger,
	}
}

// Analyze performs technical analysis on OHLCV data using all configured indicators.
func (s *MultiIndicatorStack) Analyze(ctx context.Context, data *OHLCVData) (*MultiIndicatorResult, error) {
	if err := data.Validate(); err != nil {
		return nil, err
	}

	if data.Length() < 50 {
		return nil, ErrInsufficientData
	}

	result := &MultiIndicatorResult{
		Symbol:       data.Symbol,
		Exchange:     data.Exchange,
		Timeframe:    data.Timeframe,
		Indicators:   make([]*IndicatorResult, 0),
		CalculatedAt: time.Now(),
	}

	var wg sync.WaitGroup
	var mu sync.Mutex
	errors := make([]error, 0)

	calculate := func(name string, fn func() (*IndicatorResult, error)) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			indicator, err := fn()
			if err != nil {
				mu.Lock()
				errors = append(errors, fmt.Errorf("%s: %w", name, err))
				mu.Unlock()
				return
			}
			mu.Lock()
			result.Indicators = append(result.Indicators, indicator)
			mu.Unlock()
		}()
	}

	for _, period := range s.config.SMAPeriods {
		p := period
		calculate(fmt.Sprintf("SMA_%d", p), func() (*IndicatorResult, error) {
			return s.calculateSMA(data, p)
		})
	}

	for _, period := range s.config.EMAPeriods {
		p := period
		calculate(fmt.Sprintf("EMA_%d", p), func() (*IndicatorResult, error) {
			return s.calculateEMA(data, p)
		})
	}

	calculate("RSI", func() (*IndicatorResult, error) {
		return s.calculateRSI(data, s.config.RSIPeriod)
	})

	calculate("MACD", func() (*IndicatorResult, error) {
		return s.calculateMACD(data, s.config.MACDFast, s.config.MACDSlow, s.config.MACDSignal)
	})

	calculate("BollingerBands", func() (*IndicatorResult, error) {
		return s.calculateBollingerBands(data, s.config.BBPeriod, s.config.BBStdDev)
	})

	calculate("ATR", func() (*IndicatorResult, error) {
		return s.calculateATR(data, s.config.ATRPeriod)
	})

	calculate("Stochastic", func() (*IndicatorResult, error) {
		return s.calculateStochastic(data, s.config.StochKPeriod, s.config.StochDPeriod)
	})

	if s.config.OBVEnabled {
		calculate("OBV", func() (*IndicatorResult, error) {
			return s.calculateOBV(data)
		})
	}

	if s.config.VWAPEnabled {
		calculate("VWAP", func() (*IndicatorResult, error) {
			return s.calculateVWAP(data)
		})
	}

	wg.Wait()

	if len(errors) > 0 {
		s.logger.Warn("some indicators failed to calculate",
			zap.Int("failed", len(errors)),
			zap.Int("succeeded", len(result.Indicators)))
	}

	result.OverallSignal, result.Confidence = s.determineOverallSignal(result.Indicators)

	return result, nil
}

// AnalyzeSequential performs analysis sequentially (useful for debugging or when concurrency isn't needed).
func (s *MultiIndicatorStack) AnalyzeSequential(data *OHLCVData) (*MultiIndicatorResult, error) {
	if err := data.Validate(); err != nil {
		return nil, err
	}

	if data.Length() < 50 {
		return nil, ErrInsufficientData
	}

	result := &MultiIndicatorResult{
		Symbol:       data.Symbol,
		Exchange:     data.Exchange,
		Timeframe:    data.Timeframe,
		Indicators:   make([]*IndicatorResult, 0),
		CalculatedAt: time.Now(),
	}

	for _, period := range s.config.SMAPeriods {
		if indicator, err := s.calculateSMA(data, period); err == nil {
			result.Indicators = append(result.Indicators, indicator)
		}
	}

	for _, period := range s.config.EMAPeriods {
		if indicator, err := s.calculateEMA(data, period); err == nil {
			result.Indicators = append(result.Indicators, indicator)
		}
	}

	if indicator, err := s.calculateRSI(data, s.config.RSIPeriod); err == nil {
		result.Indicators = append(result.Indicators, indicator)
	}

	if indicator, err := s.calculateMACD(data, s.config.MACDFast, s.config.MACDSlow, s.config.MACDSignal); err == nil {
		result.Indicators = append(result.Indicators, indicator)
	}

	if indicator, err := s.calculateBollingerBands(data, s.config.BBPeriod, s.config.BBStdDev); err == nil {
		result.Indicators = append(result.Indicators, indicator)
	}

	if indicator, err := s.calculateATR(data, s.config.ATRPeriod); err == nil {
		result.Indicators = append(result.Indicators, indicator)
	}

	if indicator, err := s.calculateStochastic(data, s.config.StochKPeriod, s.config.StochDPeriod); err == nil {
		result.Indicators = append(result.Indicators, indicator)
	}

	if s.config.OBVEnabled {
		if indicator, err := s.calculateOBV(data); err == nil {
			result.Indicators = append(result.Indicators, indicator)
		}
	}

	if s.config.VWAPEnabled {
		if indicator, err := s.calculateVWAP(data); err == nil {
			result.Indicators = append(result.Indicators, indicator)
		}
	}

	result.OverallSignal, result.Confidence = s.determineOverallSignal(result.Indicators)

	return result, nil
}

func (s *MultiIndicatorStack) calculateSMA(data *OHLCVData, period int) (*IndicatorResult, error) {
	values := s.provider.SMA(data.Close, period)
	if values == nil {
		return nil, NewIndicatorError("SMA", "calculation returned nil", nil)
	}

	signal, strength := s.analyzeTrendSignal(data.Close, values)
	return &IndicatorResult{
		Name:       fmt.Sprintf("SMA_%d", period),
		Type:       IndicatorTypeTrend,
		Values:     values,
		Signal:     signal,
		Strength:   strength,
		Timestamp:  time.Now(),
		Parameters: map[string]any{"period": period},
	}, nil
}

func (s *MultiIndicatorStack) calculateEMA(data *OHLCVData, period int) (*IndicatorResult, error) {
	values := s.provider.EMA(data.Close, period)
	if values == nil {
		return nil, NewIndicatorError("EMA", "calculation returned nil", nil)
	}

	signal, strength := s.analyzeTrendSignal(data.Close, values)
	return &IndicatorResult{
		Name:       fmt.Sprintf("EMA_%d", period),
		Type:       IndicatorTypeTrend,
		Values:     values,
		Signal:     signal,
		Strength:   strength,
		Timestamp:  time.Now(),
		Parameters: map[string]any{"period": period},
	}, nil
}

func (s *MultiIndicatorStack) calculateRSI(data *OHLCVData, period int) (*IndicatorResult, error) {
	values := s.provider.RSI(data.Close, period)
	if values == nil {
		return nil, NewIndicatorError("RSI", "calculation returned nil", nil)
	}

	signal, strength := s.analyzeRSISignal(values)
	return &IndicatorResult{
		Name:       fmt.Sprintf("RSI_%d", period),
		Type:       IndicatorTypeMomentum,
		Values:     values,
		Signal:     signal,
		Strength:   strength,
		Timestamp:  time.Now(),
		Parameters: map[string]any{"period": period},
	}, nil
}

func (s *MultiIndicatorStack) calculateMACD(data *OHLCVData, fast, slow, signal int) (*IndicatorResult, error) {
	macd, signalLine, hist := s.provider.MACD(data.Close, fast, slow, signal)
	if macd == nil {
		return nil, NewIndicatorError("MACD", "calculation returned nil", nil)
	}

	sig, strength := s.analyzeMACDSignal(macd)
	return &IndicatorResult{
		Name:       "MACD",
		Type:       IndicatorTypeTrend,
		Values:     macd,
		Signal:     sig,
		Strength:   strength,
		Timestamp:  time.Now(),
		Parameters: map[string]any{"fast": fast, "slow": slow, "signal": signal},
		Metadata:   map[string]any{"signal_line": signalLine, "histogram": hist},
	}, nil
}

func (s *MultiIndicatorStack) calculateBollingerBands(data *OHLCVData, period int, stdDev float64) (*IndicatorResult, error) {
	upper, middle, lower := s.provider.BollingerBands(data.Close, period, stdDev)
	if middle == nil {
		return nil, NewIndicatorError("BollingerBands", "calculation returned nil", nil)
	}

	signal, strength := s.analyzeBollingerBands(data.Close, upper, middle, lower)
	return &IndicatorResult{
		Name:       "BB",
		Type:       IndicatorTypeVolatility,
		Values:     middle,
		Signal:     signal,
		Strength:   strength,
		Timestamp:  time.Now(),
		Parameters: map[string]any{"period": period, "std_dev": stdDev},
		Metadata:   map[string]any{"upper": upper, "lower": lower},
	}, nil
}

func (s *MultiIndicatorStack) calculateATR(data *OHLCVData, period int) (*IndicatorResult, error) {
	values := s.provider.ATR(data.High, data.Low, data.Close, period)
	if values == nil {
		return nil, NewIndicatorError("ATR", "calculation returned nil", nil)
	}

	return &IndicatorResult{
		Name:       fmt.Sprintf("ATR_%d", period),
		Type:       IndicatorTypeVolatility,
		Values:     values,
		Signal:     SignalHold,
		Strength:   decimal.NewFromFloat(0.5),
		Timestamp:  time.Now(),
		Parameters: map[string]any{"period": period},
	}, nil
}

func (s *MultiIndicatorStack) calculateStochastic(data *OHLCVData, kPeriod, dPeriod int) (*IndicatorResult, error) {
	k, d := s.provider.Stochastic(data.High, data.Low, data.Close, kPeriod, dPeriod)
	if k == nil {
		return nil, NewIndicatorError("Stochastic", "calculation returned nil", nil)
	}

	signal, strength := s.analyzeStochasticSignal(k)
	return &IndicatorResult{
		Name:       "STOCH",
		Type:       IndicatorTypeMomentum,
		Values:     k,
		Signal:     signal,
		Strength:   strength,
		Timestamp:  time.Now(),
		Parameters: map[string]any{"k_period": kPeriod, "d_period": dPeriod},
		Metadata:   map[string]any{"d": d},
	}, nil
}

func (s *MultiIndicatorStack) calculateOBV(data *OHLCVData) (*IndicatorResult, error) {
	values := s.provider.OBV(data.Close, data.Volume)
	if values == nil {
		return nil, NewIndicatorError("OBV", "calculation returned nil", nil)
	}

	signal, strength := s.analyzeOBVSignal(values, data.Close)
	return &IndicatorResult{
		Name:      "OBV",
		Type:      IndicatorTypeVolume,
		Values:    values,
		Signal:    signal,
		Strength:  strength,
		Timestamp: time.Now(),
	}, nil
}

func (s *MultiIndicatorStack) calculateVWAP(data *OHLCVData) (*IndicatorResult, error) {
	values := s.provider.VWAP(data.High, data.Low, data.Close, data.Volume)
	if values == nil {
		return nil, NewIndicatorError("VWAP", "calculation returned nil", nil)
	}

	signal, strength := s.analyzeVWAPSignal(data.Close, values)
	return &IndicatorResult{
		Name:      "VWAP",
		Type:      IndicatorTypeVolume,
		Values:    values,
		Signal:    signal,
		Strength:  strength,
		Timestamp: time.Now(),
	}, nil
}

// Signal analysis methods

func (s *MultiIndicatorStack) analyzeTrendSignal(prices, ma []decimal.Decimal) (SignalType, decimal.Decimal) {
	if len(prices) < 2 || len(ma) < 2 {
		return SignalHold, decimal.NewFromFloat(0.5)
	}

	currentPrice := prices[len(prices)-1]
	currentMA := ma[len(ma)-1]
	prevPrice := prices[len(prices)-2]
	prevMA := ma[len(ma)-2]

	if currentMA.IsZero() {
		return SignalHold, decimal.NewFromFloat(0.5)
	}

	distance := currentPrice.Sub(currentMA).Div(currentMA).Abs()

	if currentPrice.GreaterThan(currentMA) && prevPrice.LessThanOrEqual(prevMA) {
		strength := decimal.Min(decimal.NewFromFloat(0.8), decimal.NewFromFloat(0.6).Add(distance))
		return SignalBuy, strength
	}
	if currentPrice.LessThan(currentMA) && prevPrice.GreaterThanOrEqual(prevMA) {
		strength := decimal.Min(decimal.NewFromFloat(0.8), decimal.NewFromFloat(0.6).Add(distance))
		return SignalSell, strength
	}

	if currentPrice.GreaterThan(currentMA) {
		return SignalBuy, decimal.Min(decimal.NewFromFloat(0.6), decimal.NewFromFloat(0.4).Add(distance))
	}
	if currentPrice.LessThan(currentMA) {
		return SignalSell, decimal.Min(decimal.NewFromFloat(0.6), decimal.NewFromFloat(0.4).Add(distance))
	}

	return SignalHold, decimal.NewFromFloat(0.5)
}

func (s *MultiIndicatorStack) analyzeRSISignal(rsi []decimal.Decimal) (SignalType, decimal.Decimal) {
	if len(rsi) == 0 {
		return SignalHold, decimal.NewFromFloat(0.5)
	}

	current := rsi[len(rsi)-1]
	oversold := decimal.NewFromFloat(30)
	overbought := decimal.NewFromFloat(70)

	if current.LessThan(oversold) {
		return SignalBuy, decimal.NewFromFloat(0.8)
	}
	if current.GreaterThan(overbought) {
		return SignalSell, decimal.NewFromFloat(0.8)
	}
	if current.LessThan(decimal.NewFromFloat(40)) {
		return SignalBuy, decimal.NewFromFloat(0.6)
	}
	if current.GreaterThan(decimal.NewFromFloat(60)) {
		return SignalSell, decimal.NewFromFloat(0.6)
	}

	return SignalHold, decimal.NewFromFloat(0.5)
}

func (s *MultiIndicatorStack) analyzeMACDSignal(macd []decimal.Decimal) (SignalType, decimal.Decimal) {
	if len(macd) < 2 {
		return SignalHold, decimal.NewFromFloat(0.5)
	}

	current := macd[len(macd)-1]
	prev := macd[len(macd)-2]

	if current.GreaterThan(decimal.Zero) && prev.LessThanOrEqual(decimal.Zero) {
		return SignalBuy, decimal.NewFromFloat(0.8)
	}
	if current.LessThan(decimal.Zero) && prev.GreaterThanOrEqual(decimal.Zero) {
		return SignalSell, decimal.NewFromFloat(0.8)
	}
	if current.GreaterThan(decimal.Zero) {
		return SignalBuy, decimal.NewFromFloat(0.6)
	}
	if current.LessThan(decimal.Zero) {
		return SignalSell, decimal.NewFromFloat(0.6)
	}

	return SignalHold, decimal.NewFromFloat(0.5)
}

func (s *MultiIndicatorStack) analyzeBollingerBands(prices, upper, middle, lower []decimal.Decimal) (SignalType, decimal.Decimal) {
	if len(prices) == 0 || len(upper) == 0 || len(lower) == 0 {
		return SignalHold, decimal.NewFromFloat(0.5)
	}

	price := prices[len(prices)-1]
	u := upper[len(upper)-1]
	l := lower[len(lower)-1]
	bandWidth := u.Sub(l)

	if price.LessThanOrEqual(l.Mul(decimal.NewFromFloat(1.02))) {
		return SignalBuy, decimal.NewFromFloat(0.75)
	}
	if price.GreaterThanOrEqual(u.Mul(decimal.NewFromFloat(0.98))) {
		return SignalSell, decimal.NewFromFloat(0.75)
	}

	position := price.Sub(l).Div(bandWidth)
	if position.GreaterThan(decimal.NewFromFloat(0.7)) {
		return SignalBuy, decimal.NewFromFloat(0.55)
	}
	if position.LessThan(decimal.NewFromFloat(0.3)) {
		return SignalSell, decimal.NewFromFloat(0.55)
	}

	return SignalHold, decimal.NewFromFloat(0.5)
}

func (s *MultiIndicatorStack) analyzeStochasticSignal(k []decimal.Decimal) (SignalType, decimal.Decimal) {
	if len(k) == 0 {
		return SignalHold, decimal.NewFromFloat(0.5)
	}

	current := k[len(k)-1]
	oversold := decimal.NewFromFloat(20)
	overbought := decimal.NewFromFloat(80)

	if current.LessThan(oversold) {
		return SignalBuy, decimal.NewFromFloat(0.75)
	}
	if current.GreaterThan(overbought) {
		return SignalSell, decimal.NewFromFloat(0.75)
	}

	return SignalHold, decimal.NewFromFloat(0.5)
}

func (s *MultiIndicatorStack) analyzeOBVSignal(obv, prices []decimal.Decimal) (SignalType, decimal.Decimal) {
	if len(obv) < 2 || len(prices) < 2 {
		return SignalHold, decimal.NewFromFloat(0.5)
	}

	currentOBV := obv[len(obv)-1]
	prevOBV := obv[len(obv)-2]
	currentPrice := prices[len(prices)-1]
	prevPrice := prices[len(prices)-2]

	if currentPrice.GreaterThan(prevPrice) && currentOBV.GreaterThan(prevOBV) {
		return SignalBuy, decimal.NewFromFloat(0.7)
	}
	if currentPrice.LessThan(prevPrice) && currentOBV.LessThan(prevOBV) {
		return SignalSell, decimal.NewFromFloat(0.7)
	}

	return SignalHold, decimal.NewFromFloat(0.5)
}

func (s *MultiIndicatorStack) analyzeVWAPSignal(prices, vwap []decimal.Decimal) (SignalType, decimal.Decimal) {
	if len(prices) == 0 || len(vwap) == 0 {
		return SignalHold, decimal.NewFromFloat(0.5)
	}

	price := prices[len(prices)-1]
	v := vwap[len(vwap)-1]

	if price.LessThan(v) {
		return SignalBuy, decimal.NewFromFloat(0.6)
	}
	if price.GreaterThan(v) {
		return SignalSell, decimal.NewFromFloat(0.6)
	}

	return SignalHold, decimal.NewFromFloat(0.5)
}

func (s *MultiIndicatorStack) determineOverallSignal(indicators []*IndicatorResult) (SignalType, decimal.Decimal) {
	if len(indicators) == 0 {
		return SignalHold, decimal.NewFromFloat(0.5)
	}

	buyScore := decimal.Zero
	sellScore := decimal.Zero
	totalWeight := decimal.Zero

	for _, indicator := range indicators {
		weight := indicator.Strength
		totalWeight = totalWeight.Add(weight)

		switch indicator.Signal {
		case SignalBuy:
			buyScore = buyScore.Add(weight)
		case SignalSell:
			sellScore = sellScore.Add(weight)
		}
	}

	if totalWeight.IsZero() {
		return SignalHold, decimal.NewFromFloat(0.5)
	}

	buyRatio := buyScore.Div(totalWeight)
	sellRatio := sellScore.Div(totalWeight)

	if buyRatio.GreaterThan(decimal.NewFromFloat(0.6)) {
		return SignalBuy, buyRatio
	}
	if sellRatio.GreaterThan(decimal.NewFromFloat(0.6)) {
		return SignalSell, sellRatio
	}

	return SignalHold, decimal.NewFromFloat(0.5)
}

// SetConfig updates the indicator configuration.
func (s *MultiIndicatorStack) SetConfig(config *IndicatorConfig) {
	if config != nil {
		s.config = config
	}
}

// GetConfig returns the current indicator configuration.
func (s *MultiIndicatorStack) GetConfig() *IndicatorConfig {
	return s.config
}

// GetProvider returns the underlying indicator provider.
func (s *MultiIndicatorStack) GetProvider() IndicatorProvider {
	return s.provider
}
