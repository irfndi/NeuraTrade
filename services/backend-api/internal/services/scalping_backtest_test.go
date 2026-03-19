package services

import (
	"context"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewScalpingBacktestEngine(t *testing.T) {
	tests := []struct {
		name   string
		config ScalpingBacktestConfig
	}{
		{
			name:   "empty config gets defaults",
			config: ScalpingBacktestConfig{},
		},
		{
			name: "valid config",
			config: ScalpingBacktestConfig{
				StartTime:      time.Now().Add(-24 * time.Hour),
				EndTime:        time.Now(),
				InitialCapital: decimal.NewFromInt(10000),
			},
		},
		{
			name: "custom config",
			config: ScalpingBacktestConfig{
				StartTime:          time.Now().Add(-48 * time.Hour),
				EndTime:            time.Now(),
				InitialCapital:     decimal.NewFromInt(50000),
				FeeRate:            decimal.NewFromFloat(0.001),
				SlippagePct:        decimal.NewFromFloat(0.002),
				MaxBidAskSpreadPct: 0.5,
				MinConfidence:      0.70,
				MinExpectancyN:     10,
				MaxCapitalPct:      3.0,
				Symbols:            []string{"BTC/USDT", "ETH/USDT"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			engine := NewScalpingBacktestEngine(nil, tt.config)

			require.NotNil(t, engine)
			assert.NotNil(t, engine.config)
			assert.NotNil(t, engine.positions)
			assert.NotNil(t, engine.gateStats)
			assert.True(t, engine.config.InitialCapital.GreaterThan(decimal.Zero))
			assert.True(t, engine.config.FeeRate.GreaterThanOrEqual(decimal.Zero))
			assert.True(t, engine.config.SlippagePct.GreaterThan(decimal.Zero))
			assert.Greater(t, engine.config.MaxBidAskSpreadPct, 0.0)
			assert.Greater(t, engine.config.MinConfidence, 0.0)
			assert.Greater(t, engine.config.MaxCapitalPct, 0.0)
			assert.NotEmpty(t, engine.config.Symbols)
		})
	}
}

func TestScalpingBacktestEngine_ValidateConfig(t *testing.T) {
	tests := []struct {
		name    string
		config  ScalpingBacktestConfig
		wantErr bool
		errMsg  string
	}{
		{
			name:    "empty config - missing times",
			config:  ScalpingBacktestConfig{},
			wantErr: true,
			errMsg:  "start_time and end_time are required",
		},
		{
			name: "missing end time",
			config: ScalpingBacktestConfig{
				StartTime: time.Now().Add(-24 * time.Hour),
			},
			wantErr: true,
			errMsg:  "start_time and end_time are required",
		},
		{
			name: "start after end",
			config: ScalpingBacktestConfig{
				StartTime: time.Now(),
				EndTime:   time.Now().Add(-24 * time.Hour),
			},
			wantErr: true,
			errMsg:  "start_time must be before end_time",
		},
		{
			name: "zero initial capital",
			config: ScalpingBacktestConfig{
				StartTime:      time.Now().Add(-24 * time.Hour),
				EndTime:        time.Now(),
				InitialCapital: decimal.Zero,
			},
			wantErr: true,
			errMsg:  "initial capital must be positive",
		},
		{
			name: "negative initial capital",
			config: ScalpingBacktestConfig{
				StartTime:      time.Now().Add(-24 * time.Hour),
				EndTime:        time.Now(),
				InitialCapital: decimal.NewFromInt(-1000),
			},
			wantErr: true,
			errMsg:  "initial capital must be positive",
		},
		{
			name: "valid config",
			config: ScalpingBacktestConfig{
				StartTime:      time.Now().Add(-24 * time.Hour),
				EndTime:        time.Now(),
				InitialCapital: decimal.NewFromInt(10000),
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			engine := &ScalpingBacktestEngine{config: tt.config}
			err := engine.validateConfig()

			if tt.wantErr {
				assert.Error(t, err)
				if tt.errMsg != "" {
					assert.Contains(t, err.Error(), tt.errMsg)
				}
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestScalpingBacktestEngine_ClassifyRegime(t *testing.T) {
	engine := &ScalpingBacktestEngine{
		config: ScalpingBacktestConfig{
			MaxBidAskSpreadPct: 0.5,
		},
	}

	tests := []struct {
		name     string
		signal   MarketSignal
		expected string
	}{
		{
			name: "illiquid - spread too high",
			signal: MarketSignal{
				BidAskSpread:       1.0,
				OrderBookImbalance: 0.3,
				RangePosition24h:   50,
			},
			expected: "illiquid",
		},
		{
			name: "trend - strong positive imbalance",
			signal: MarketSignal{
				BidAskSpread:       0.1,
				OrderBookImbalance: 0.30,
				RangePosition24h:   40,
			},
			expected: "trend",
		},
		{
			name: "trend - strong negative imbalance",
			signal: MarketSignal{
				BidAskSpread:       0.1,
				OrderBookImbalance: -0.30,
				RangePosition24h:   60,
			},
			expected: "trend",
		},
		{
			name: "chop - weak imbalance",
			signal: MarketSignal{
				BidAskSpread:       0.1,
				OrderBookImbalance: 0.05,
				RangePosition24h:   50,
			},
			expected: "chop",
		},
		{
			name: "neutral - moderate imbalance",
			signal: MarketSignal{
				BidAskSpread:       0.1,
				OrderBookImbalance: 0.15,
				RangePosition24h:   50,
			},
			expected: "neutral",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			regime := engine.classifyRegime(tt.signal)
			assert.Equal(t, tt.expected, regime)
		})
	}
}

func TestNormalizeScalpingBacktestConfig(t *testing.T) {
	tests := []struct {
		name  string
		input ScalpingBacktestConfig
	}{
		{
			name:  "empty config gets all defaults",
			input: ScalpingBacktestConfig{},
		},
		{
			name: "partial config preserves values and fills gaps",
			input: ScalpingBacktestConfig{
				InitialCapital: decimal.NewFromInt(50000),
				Symbols:        []string{"BTC/USDT"},
			},
		},
		{
			name: "negative values get replaced with defaults",
			input: ScalpingBacktestConfig{
				InitialCapital: decimal.NewFromInt(-1000),
				FeeRate:        decimal.NewFromFloat(-0.01),
				SlippagePct:    decimal.NewFromFloat(-0.01),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := normalizeScalpingBacktestConfig(tt.input)

			assert.True(t, result.InitialCapital.GreaterThan(decimal.Zero))
			assert.True(t, result.FeeRate.GreaterThanOrEqual(decimal.Zero))
			assert.True(t, result.SlippagePct.GreaterThan(decimal.Zero))
			assert.Greater(t, result.MaxBidAskSpreadPct, 0.0)
			assert.Greater(t, result.MinConfidence, 0.0)
			assert.Greater(t, result.MinExpectancyN, 0)
			assert.Greater(t, result.MaxCapitalPct, 0.0)
			assert.Greater(t, result.DefaultHoldPeriod, time.Duration(0))
			assert.NotEmpty(t, result.Symbols)

			if tt.input.InitialCapital.GreaterThan(decimal.Zero) {
				assert.True(t, result.InitialCapital.Equal(tt.input.InitialCapital))
			}
			if len(tt.input.Symbols) > 0 {
				assert.Equal(t, tt.input.Symbols, result.Symbols)
			}
		})
	}
}

func TestBuildHistoricalSignalsFromOHLCV_SpreadUsesEffectiveSpreadEstimate(t *testing.T) {
	signals := buildHistoricalSignalsFromOHLCV([]scalpingOHLCVPoint{
		{
			symbol:    "BTC/USDT",
			exchange:  "binance",
			open:      100,
			high:      100.12,
			low:       99.88,
			close:     100,
			volume:    1000,
			timestamp: time.Unix(0, 0).UTC(),
		},
	}, backtestSpreadMultiplier)

	require.Len(t, signals, 1)
	assert.InDelta(t, 0.03, signals[0].Signal.BidAskSpread, 1e-9)
}

func TestClampFloat(t *testing.T) {
	tests := []struct {
		name     string
		value    float64
		min      float64
		max      float64
		expected float64
	}{
		{"below min", 0.5, 1.0, 10.0, 1.0},
		{"above max", 15.0, 1.0, 10.0, 10.0},
		{"in range", 5.0, 1.0, 10.0, 5.0},
		{"at min", 1.0, 1.0, 10.0, 1.0},
		{"at max", 10.0, 1.0, 10.0, 10.0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := clampFloat(tt.value, tt.min, tt.max)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestOutcomeFromPnL(t *testing.T) {
	tests := []struct {
		name     string
		pnl      decimal.Decimal
		expected string
	}{
		{
			name:     "positive PnL is win",
			pnl:      decimal.NewFromInt(100),
			expected: "win",
		},
		{
			name:     "negative PnL is loss",
			pnl:      decimal.NewFromInt(-100),
			expected: "loss",
		},
		{
			name:     "zero PnL is breakeven",
			pnl:      decimal.Zero,
			expected: "breakeven",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := outcomeFromPnL(tt.pnl)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestScalpingBacktestEngine_Run_NilEngine(t *testing.T) {
	var engine *ScalpingBacktestEngine
	_, err := engine.Run(context.Background())
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "nil")
}

func TestScalpingBacktestEngine_Run_InvalidConfig(t *testing.T) {
	engine := &ScalpingBacktestEngine{
		config: ScalpingBacktestConfig{},
	}
	_, err := engine.Run(context.Background())
	assert.Error(t, err)
}
