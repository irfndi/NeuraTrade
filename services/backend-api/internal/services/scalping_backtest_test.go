package services

import (
	"context"
	"fmt"
	"math"
	"testing"
	"time"

	"github.com/irfndi/neuratrade/internal/database"
	"github.com/pashagolub/pgxmock/v4"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDefaultScalpingBacktestUniverse_EnvVarOverride(t *testing.T) {
	t.Run("unset_returns_canonical_defaults", func(t *testing.T) {
		t.Setenv(envBacktestSymbols, "")
		assert.Equal(t, []string{"BTC/USDT", "ETH/USDT", "SOL/USDT", "BNB/USDT", "XRP/USDT"}, defaultScalpingBacktestUniverse())
	})

	t.Run("env_var_overrides_with_normalization", func(t *testing.T) {
		t.Setenv(envBacktestSymbols, "doge/usdt, PEPE/USDT , doge/usdt")
		assert.Equal(t, []string{"DOGE/USDT", "PEPE/USDT"}, defaultScalpingBacktestUniverse())
	})
}

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
		{
			name: "valid config with mode=ai",
			config: ScalpingBacktestConfig{
				StartTime:      time.Now().Add(-24 * time.Hour),
				EndTime:        time.Now(),
				InitialCapital: decimal.NewFromInt(10000),
				Mode:           "ai",
			},
			wantErr: false,
		},
		{
			name: "valid config with mode=deterministic",
			config: ScalpingBacktestConfig{
				StartTime:      time.Now().Add(-24 * time.Hour),
				EndTime:        time.Now(),
				InitialCapital: decimal.NewFromInt(10000),
				Mode:           "deterministic",
			},
			wantErr: false,
		},
		{
			name: "invalid mode value",
			config: ScalpingBacktestConfig{
				StartTime:      time.Now().Add(-24 * time.Hour),
				EndTime:        time.Now(),
				InitialCapital: decimal.NewFromInt(10000),
				Mode:           "neural",
			},
			wantErr: true,
			errMsg:  "invalid mode \"neural\"",
		},
		{
			name: "empty mode is valid (defaults to deterministic downstream)",
			config: ScalpingBacktestConfig{
				StartTime:      time.Now().Add(-24 * time.Hour),
				EndTime:        time.Now(),
				InitialCapital: decimal.NewFromInt(10000),
				Mode:           "",
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

// TestScalpingBacktestEngine_ComputeSignalHints covers the AI-mode sidecar
// metadata computation (PR-3). The function is called from evaluateSignal
// when the engine's Mode is "ai"; it must return nil for non-actionable
// decisions and produce non-negative SuggestedAction/ConfidenceHint/
// CandidateScore fields for actionable ones.
func TestScalpingBacktestEngine_ComputeSignalHints(t *testing.T) {
	engine := &ScalpingBacktestEngine{config: ScalpingBacktestConfig{
		DeterministicFallback: DefaultDeterministicFallbackConfig(),
		MaxBidAskSpreadPct:    0.5,
	}}

	t.Run("nil decision returns nil hints", func(t *testing.T) {
		hints := engine.computeSignalHints(MarketSignal{}, nil)
		assert.Nil(t, hints)
	})

	t.Run("hold decision returns nil hints", func(t *testing.T) {
		holdDecision := &AITradingDecision{Action: "hold", Confidence: 0.5}
		hints := engine.computeSignalHints(MarketSignal{}, holdDecision)
		assert.Nil(t, hints)
	})

	t.Run("buy decision with valid signal returns hints", func(t *testing.T) {
		signal := MarketSignal{
			Symbol:             "BTC/USDT",
			Price:              50000,
			High24h:            51000,
			Low24h:             49000,
			Volume24h:          1_000_000,
			BidAskSpread:       0.05,
			OrderBookImbalance: 0.40,
			RangePosition24h:   20,
		}
		buyDecision := &AITradingDecision{Action: "buy", Confidence: 0.75}
		hints := engine.computeSignalHints(signal, buyDecision)
		require.NotNil(t, hints, "buy decision should produce hints")
		assert.Equal(t, "buy", hints.SuggestedAction)
		assert.InDelta(t, 0.75, hints.ConfidenceHint, 1e-9)
		assert.GreaterOrEqual(t, hints.CandidateScore, 0.0,
			"score must be non-negative (imbalance+liquidity+volume components)")
	})

	t.Run("sell decision with valid signal returns hints", func(t *testing.T) {
		signal := MarketSignal{
			Symbol:             "ETH/USDT",
			Price:              3000,
			High24h:            3100,
			Low24h:             2900,
			Volume24h:          500_000,
			BidAskSpread:       0.05,
			OrderBookImbalance: -0.40,
			RangePosition24h:   80,
		}
		sellDecision := &AITradingDecision{Action: "sell", Confidence: 0.72}
		hints := engine.computeSignalHints(signal, sellDecision)
		require.NotNil(t, hints, "sell decision should produce hints")
		assert.Equal(t, "sell", hints.SuggestedAction)
		assert.InDelta(t, 0.72, hints.ConfidenceHint, 1e-9)
		assert.GreaterOrEqual(t, hints.CandidateScore, 0.0)
	})

	t.Run("buy with fee-fragile spread returns nil hints", func(t *testing.T) {
		// Mirrors the live AI hint path's scalpingBuySignalRejectionReason gate:
		// a buy the deterministic path picks but the AI pipeline would suppress
		// (fee-fragile spread) must not produce a hint.
		signal := MarketSignal{
			Symbol:             "BTC/USDT",
			Price:              50000,
			High24h:            51000,
			Low24h:             49000,
			Volume24h:          1_000_000,
			BidAskSpread:       0.10, // > scalpingRecentBuyMaxSpreadPct (0.04)
			OrderBookImbalance: 0.40,
			RangePosition24h:   20,
			PriceChange24h:     0.05,
			RecentChangeKnown:  true,
			RecentPriceChange:  0.05,
		}
		buyDecision := &AITradingDecision{Action: "buy", Confidence: 0.75, RangeAlignment: 0.6}
		hints := engine.computeSignalHints(signal, buyDecision)
		assert.Nil(t, hints, "buy with fee-fragile spread should be rejected at hint layer")
	})

	t.Run("whitespace action trimmed", func(t *testing.T) {
		signal := MarketSignal{
			Symbol:             "BTC/USDT",
			Price:              50000,
			High24h:            51000,
			Low24h:             49000,
			Volume24h:          1_000_000,
			BidAskSpread:       0.05,
			OrderBookImbalance: 0.40,
			RangePosition24h:   20,
		}
		decision := &AITradingDecision{Action: "  buy  ", Confidence: 0.7}
		hints := engine.computeSignalHints(signal, decision)
		require.NotNil(t, hints)
		assert.Equal(t, "buy", hints.SuggestedAction, "action should be lowercased + trimmed")
	})

	t.Run("uses decision.RangeAlignment from secondary branch", func(t *testing.T) {
		// Secondary branches (blowoff sell, proximity-adjusted, reversal buy,
		// sell-window, dual-proximity) compute rangeAlignment with a different
		// formula than the primary buy/sell branches. The hints must reuse
		// decision.RangeAlignment verbatim, not re-derive via the primary
		// formula (which was the prior bug that produced wrong scores for
		// signals entering through these branches).
		// RangePosition24h=96 lands inside the blowoff band [95,98], yielding
		// a non-zero alignment (1/3). Any prior value (e.g. 80) clamped to 0
		// and made the test unable to distinguish "uses decision.RangeAlignment"
		// from "ignores it and hardcodes 0".
		fallback := engine.config.DeterministicFallback.Normalized()
		signal := MarketSignal{
			Symbol:             "BTC/USDT",
			Price:              50000,
			Volume24h:          1_000_000,
			BidAskSpread:       0.05,
			OrderBookImbalance: -0.40,
			RangePosition24h:   96,
		}
		customAlignment := clampFloat(
			(signal.RangePosition24h-scalpingBlowoffSellRangeMin)/
				math.Max(scalpingBlowoffSellRangeMax-scalpingBlowoffSellRangeMin, 1),
			0, 1,
		)
		decision := &AITradingDecision{
			Action:         "sell",
			Confidence:     0.70,
			RangeAlignment: customAlignment,
		}
		hints := engine.computeSignalHints(signal, decision)
		require.NotNil(t, hints)

		effectiveMaxSpread := math.Max(fallback.MaxBidAskSpread, engine.config.MaxBidAskSpreadPct)
		liquidityScore := clampFloat(1-(signal.BidAskSpread/effectiveMaxSpread), 0, 1)
		volumeScore := clampFloat(math.Log10(signal.Volume24h+1)/fallback.VolumeLogScale, 0, 1)
		expectedScore := math.Abs(signal.OrderBookImbalance)*fallback.ImbalanceWeight +
			liquidityScore*fallback.LiquidityWeight +
			customAlignment*fallback.RangeWeight +
			volumeScore*fallback.VolumeWeight
		assert.InDelta(t, expectedScore, hints.CandidateScore, 1e-9,
			"CandidateScore must use decision.RangeAlignment, not a re-derived primary formula")
	})

	t.Run("zero RangeAlignment yields same score as primary buy branch with RangePosition=BuyRangeMax", func(t *testing.T) {
		// Regression guard: when the decision carries RangeAlignment=0, the
		// hints score drops the range component entirely. This pins the
		// contract that decision.RangeAlignment is the single source of truth.
		fallback := engine.config.DeterministicFallback.Normalized()
		signal := MarketSignal{
			Symbol:             "BTC/USDT",
			Price:              50000,
			Volume24h:          1_000_000,
			BidAskSpread:       0.05,
			OrderBookImbalance: 0.40,
			RangePosition24h:   20,
		}
		decision := &AITradingDecision{Action: "buy", Confidence: 0.7, RangeAlignment: 0}
		hints := engine.computeSignalHints(signal, decision)
		require.NotNil(t, hints)

		effectiveMaxSpread := math.Max(fallback.MaxBidAskSpread, engine.config.MaxBidAskSpreadPct)
		liquidityScore := clampFloat(1-(signal.BidAskSpread/effectiveMaxSpread), 0, 1)
		volumeScore := clampFloat(math.Log10(signal.Volume24h+1)/fallback.VolumeLogScale, 0, 1)
		expectedScore := math.Abs(signal.OrderBookImbalance)*fallback.ImbalanceWeight +
			liquidityScore*fallback.LiquidityWeight +
			0.0*fallback.RangeWeight +
			volumeScore*fallback.VolumeWeight
		assert.InDelta(t, expectedScore, hints.CandidateScore, 1e-9)
	})
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

func TestCompute24hWindowMetrics(t *testing.T) {
	base := time.Unix(0, 0).UTC()
	metrics := compute24hWindowMetrics([]scalpingOHLCVPoint{
		{symbol: "BTC/USDT", close: 100, high: 101, low: 99, volume: 10, timestamp: base},
		{symbol: "BTC/USDT", close: 101, high: 102, low: 98, volume: 20, timestamp: base.Add(12 * time.Hour)},
		{symbol: "BTC/USDT", close: 110, high: 103, low: 97, volume: 30, timestamp: base.Add(25 * time.Hour)},
	})

	require.Len(t, metrics, 3)
	assert.Equal(t, 101.0, metrics[0].High24h)
	assert.Equal(t, 99.0, metrics[0].Low24h)
	assert.Equal(t, 10.0, metrics[0].Volume24h)
	assert.Equal(t, 100.0, metrics[0].ReferenceClose24h)
	assert.True(t, metrics[0].HasReferenceClose)
	assert.Equal(t, 102.0, metrics[1].High24h)
	assert.Equal(t, 98.0, metrics[1].Low24h)
	assert.Equal(t, 30.0, metrics[1].Volume24h)
	assert.Equal(t, 100.0, metrics[1].ReferenceClose24h)
	assert.True(t, metrics[1].HasReferenceClose)
	assert.Equal(t, 103.0, metrics[2].High24h)
	assert.Equal(t, 97.0, metrics[2].Low24h)
	assert.Equal(t, 50.0, metrics[2].Volume24h)
	assert.Equal(t, 100.0, metrics[2].ReferenceClose24h)
	assert.True(t, metrics[2].HasReferenceClose)
}

func TestMapPointToHistoricalSignal(t *testing.T) {
	point := scalpingOHLCVPoint{
		symbol:    "BTC/USDT",
		exchange:  "binance",
		open:      100,
		high:      100.12,
		low:       99.88,
		close:     100,
		volume:    1000,
		timestamp: time.Unix(0, 0).UTC(),
	}
	metrics := scalping24hWindowMetrics{High24h: 101, Low24h: 99, Volume24h: 2400}

	signal := mapPointToHistoricalSignal(point, metrics, 1.25, DefaultScalpingBacktestSpreadMultiplier, 0, 0, 0, 0)

	assert.Equal(t, point.timestamp, signal.Timestamp)
	assert.Equal(t, point.symbol, signal.Symbol)
	assert.InDelta(t, 0.03, signal.Signal.BidAskSpread, 1e-9)
	assert.Equal(t, 101.0, signal.Signal.High24h)
	assert.Equal(t, 99.0, signal.Signal.Low24h)
	assert.Equal(t, 2400.0, signal.Signal.Volume24h)
	assert.Equal(t, 1.25, signal.Signal.PriceChange24h)
	assert.InDelta(t, 50.0, signal.Signal.RangePosition24h, 1e-9)
	assert.InDelta(t, 0.0, signal.Signal.OrderBookImbalance, 1e-9)
}

func TestBuildHistoricalSignalsFromOHLCV_SeparatesExchangeSeriesAndUses24hReference(t *testing.T) {
	base := time.Unix(0, 0).UTC()
	signals := buildHistoricalSignalsFromOHLCV([]scalpingOHLCVPoint{
		{symbol: "BTC/USDT", exchange: "binance", open: 100, high: 101, low: 99, close: 100, volume: 10, timestamp: base},
		{symbol: "BTC/USDT", exchange: "bybit", open: 200, high: 201, low: 199, close: 200, volume: 12, timestamp: base},
		{symbol: "BTC/USDT", exchange: "binance", open: 109, high: 111, low: 108, close: 110, volume: 14, timestamp: base.Add(24 * time.Hour)},
		{symbol: "BTC/USDT", exchange: "bybit", open: 189, high: 191, low: 188, close: 190, volume: 16, timestamp: base.Add(24 * time.Hour)},
	}, DefaultScalpingBacktestSpreadMultiplier)

	require.Len(t, signals, 4)
	require.Equal(t, "binance", signals[0].Exchange)
	require.Equal(t, "bybit", signals[1].Exchange)
	require.Equal(t, "binance", signals[2].Exchange)
	require.Equal(t, "bybit", signals[3].Exchange)
	assert.Equal(t, 0.0, signals[0].Signal.PriceChange24h)
	assert.Equal(t, 0.0, signals[1].Signal.PriceChange24h)
	assert.InDelta(t, 10.0, signals[2].Signal.PriceChange24h, 1e-9)
	assert.InDelta(t, -5.0, signals[3].Signal.PriceChange24h, 1e-9)
}

func TestResolveTradingPairIDs_AllowsInactiveHistoricalSymbols(t *testing.T) {
	mockPool, err := pgxmock.NewPool(pgxmock.QueryMatcherOption(pgxmock.QueryMatcherEqual))
	require.NoError(t, err)
	defer mockPool.Close()

	engine := NewScalpingBacktestEngine(database.NewMockDBPool(mockPool), ScalpingBacktestConfig{})
	mockPool.ExpectQuery("SELECT id\n\t\tFROM trading_pairs\n\t\tWHERE UPPER(REPLACE(\n\t\t\tCASE\n\t\t\t\tWHEN POSITION(':' IN symbol) > 0 THEN SUBSTRING(symbol FROM 1 FOR POSITION(':' IN symbol) - 1)\n\t\t\t\tELSE symbol\n\t\t\tEND,\n\t\t\t'-',\n\t\t\t'/'\n\t\t)) IN ($1)").
		WithArgs("FTM/USDT").
		WillReturnRows(
			pgxmock.NewRows([]string{"id"}).
				AddRow(2),
		)

	ids, err := engine.resolveTradingPairIDs(context.Background(), map[string]struct{}{
		normalizeSymbolForComparison("FTM/USDT"): {},
	})
	require.NoError(t, err)
	assert.Equal(t, []int{2}, ids)
	assert.NoError(t, mockPool.ExpectationsWereMet())
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

func TestScalpingBacktestEngine_RunSignalsProducesPaperTradeMetrics(t *testing.T) {
	now := time.Date(2026, 5, 12, 2, 45, 0, 0, time.UTC)
	engine := newRunSignalsTestEngine(now)

	result, err := engine.RunSignals(context.Background(), []HistoricalSignal{
		runSignalsTestSignal(now, "AAA/USDT", 100, 0.50, 35),
		runSignalsTestSignal(now.Add(30*time.Second), "BBB/USDT", 50, -0.45, 65),
		runSignalsTestSignal(now.Add(60*time.Second), "AAA/USDT", 108, 0.50, 35),
		runSignalsTestSignal(now.Add(90*time.Second), "BBB/USDT", 46, -0.45, 65),
	})

	require.NoError(t, err)
	require.Equal(t, 4, result.Summary.TotalSignals)
	require.Equal(t, 2, result.Summary.EligibleSignals)
	require.Equal(t, 2, result.Summary.TotalTrades)
	require.Equal(t, 2, result.Summary.WinningTrades)
	require.Equal(t, 2, result.Summary.RejectionByReason["entry_without_close_signal"])
	require.True(t, result.Summary.TotalPnL.GreaterThan(decimal.Zero))
	require.True(t, result.Summary.ProfitFactor.GreaterThan(decimal.Zero))
	require.Len(t, result.Trades, 2)
}

func TestScalpingBacktestEngine_RunSignalsAllowsMomentumContinuationSell(t *testing.T) {
	now := time.Date(2026, 5, 12, 2, 47, 0, 0, time.UTC)
	engine := newRunSignalsTestEngine(now)

	result, err := engine.RunSignals(context.Background(), []HistoricalSignal{{
		Timestamp: now,
		Symbol:    "WIF/USDT",
		Exchange:  "bitget",
		Signal: MarketSignal{
			Symbol:             "WIF/USDT",
			Price:              1.25,
			High24h:            1.50,
			Low24h:             1.00,
			Low:                1.24,
			High:               1.26,
			Volume24h:          1_500_000,
			BidAskSpread:       0.06,
			OrderBookImbalance: -0.23,
			RangePosition24h:   80,
			BBPercentB:         0.9,
			PriceChange24h:     -0.8,
			RecentPriceChange:  -0.10,
			RecentChangeKnown:  true,
		},
	}, {
		Timestamp: now.Add(time.Minute),
		Symbol:    "WIF/USDT",
		Exchange:  "bitget",
		Signal: MarketSignal{
			Symbol:             "WIF/USDT",
			Price:              1.10,
			High24h:            1.50,
			Low24h:             1.00,
			Low:                1.09,
			High:               1.11,
			Volume24h:          1_500_000,
			BidAskSpread:       0.06,
			OrderBookImbalance: -0.23,
			RangePosition24h:   80,
			BBPercentB:         0.9,
			PriceChange24h:     -0.8,
			RecentPriceChange:  -0.10,
			RecentChangeKnown:  true,
		},
	}})

	require.NoError(t, err)
	require.Equal(t, 2, result.Summary.TotalSignals)
	require.Equal(t, 1, result.Summary.EligibleSignals)
	require.Equal(t, 1, result.Summary.TotalTrades)
	require.Equal(t, 1, result.Summary.RejectionByReason["entry_without_close_signal"])
	require.Len(t, result.Trades, 1)
	require.Equal(t, "sell", result.Trades[0].Side)
	require.Equal(t, "take_profit", result.Trades[0].ExitReason)
	require.True(t, result.Summary.TotalPnL.GreaterThan(decimal.Zero))
}

func TestScalpingBacktestEngine_RunSignalsBlocksRecentMidRangeSellAfterObservedLosses(t *testing.T) {
	now := time.Date(2026, 5, 12, 2, 47, 15, 0, time.UTC)
	engine := newRunSignalsTestEngine(now)

	result, err := engine.RunSignals(context.Background(), []HistoricalSignal{
		{
			Timestamp: now,
			Symbol:    "AEVOUSDT",
			Exchange:  "bybit",
			Signal: MarketSignal{
				Symbol:             "AEVOUSDT",
				Price:              0.02562,
				High24h:            0.03,
				Low24h:             0.022,
				Volume24h:          1_500_000,
				BidAskSpread:       0.03903,
				OrderBookImbalance: -0.6375,
				RangePosition24h:   45.24,
				PriceChange24h:     -1.9517,
				RecentPriceChange:  -0.078,
				RecentChangeKnown:  true,
			},
		},
		{
			Timestamp: now.Add(30 * time.Second),
			Symbol:    "AEVOUSDT",
			Exchange:  "bybit",
			Signal: MarketSignal{
				Symbol:             "AEVOUSDT",
				Price:              0.02572,
				High24h:            0.03,
				Low24h:             0.022,
				Volume24h:          1_500_000,
				BidAskSpread:       0.03903,
				OrderBookImbalance: -0.6375,
				RangePosition24h:   45.24,
				PriceChange24h:     -1.9517,
				RecentPriceChange:  -0.078,
				RecentChangeKnown:  true,
			},
		},
	})

	require.NoError(t, err)
	require.Zero(t, result.Summary.EligibleSignals)
	require.Zero(t, result.Summary.TotalTrades)
}

func TestScalpingBacktestEngine_BuildDecisionBlocksSellWhenBroadTrendIsPositive(t *testing.T) {
	now := time.Date(2026, 5, 12, 2, 47, 30, 0, time.UTC)
	engine := newRunSignalsTestEngine(now)

	decision, _ := engine.buildDecisionFromSignal(context.Background(), MarketSignal{
		Symbol:             "ONDO/USDT",
		Price:              0.3855,
		High24h:            0.40,
		Low24h:             0.30,
		Volume24h:          1_500_000,
		BidAskSpread:       0.05188,
		OrderBookImbalance: -0.45264,
		RangePosition24h:   96.32,
		PriceChange24h:     0.1315,
		RecentPriceChange:  -0.3103,
		RecentChangeKnown:  true,
	})

	require.Nil(t, decision)
}

func TestScalpingBacktestEngine_BuildDecisionRejectsObservedPullbackBuy(t *testing.T) {
	now := time.Date(2026, 5, 12, 2, 47, 45, 0, time.UTC)
	engine := newRunSignalsTestEngine(now)

	decision, _ := engine.buildDecisionFromSignal(context.Background(), MarketSignal{
		Symbol:             "ONDO/USDT",
		Price:              0.379,
		High24h:            0.40,
		Low24h:             0.30,
		Volume24h:          1_500_000,
		BidAskSpread:       0.0264,
		OrderBookImbalance: -0.4007,
		RangePosition24h:   75.95,
		PriceChange24h:     0.1043,
		RecentPriceChange:  -0.551,
		RecentChangeKnown:  true,
	})

	require.Nil(t, decision)
}

func TestScalpingBacktestEngine_BuildDecisionAllowsBlowoffReversalSell(t *testing.T) {
	now := time.Date(2026, 5, 12, 2, 47, 50, 0, time.UTC)
	engine := newRunSignalsTestEngine(now)

	decision, _ := engine.buildDecisionFromSignal(context.Background(), MarketSignal{
		Symbol:             "CHZ/USDT",
		Price:              0.04983,
		High24h:            0.05,
		Low24h:             0.045,
		Volume24h:          1_500_000,
		BidAskSpread:       0.0602,
		OrderBookImbalance: -0.4513,
		RangePosition24h:   96.58,
		PriceChange24h:     0.0774,
		RecentPriceChange:  0.2817,
		RecentChangeKnown:  true,
	})

	require.NotNil(t, decision)
	require.Equal(t, "sell", decision.Action)
}

func TestScalpingBacktestEngine_BuildDecisionBlocksWeakBlowoffSellPressure(t *testing.T) {
	now := time.Date(2026, 5, 20, 18, 2, 30, 0, time.UTC)
	engine := newRunSignalsTestEngine(now)

	decision, _ := engine.buildDecisionFromSignal(context.Background(), MarketSignal{
		Symbol:             "DASH/USDT",
		Price:              123.9,
		High24h:            124.2,
		Low24h:             100.0,
		Volume24h:          1_500_000,
		BidAskSpread:       0.0423,
		OrderBookImbalance: -0.2947,
		RangePosition24h:   95.16,
		PriceChange24h:     12.0436,
		RecentPriceChange:  0.5104,
		RecentChangeKnown:  true,
	})

	require.Nil(t, decision)
}

func TestScalpingBacktestEngine_BuildDecisionAllowsValidatedReversalBuy(t *testing.T) {
	now := time.Date(2026, 5, 21, 2, 25, 0, 0, time.UTC)
	engine := newRunSignalsTestEngine(now)
	engine.config.RequireRecentMomentum = true
	engine.config.MinRecentMomentumPct = 0.05

	decision, _ := engine.buildDecisionFromSignal(context.Background(), MarketSignal{
		Symbol:             "REV/USDT",
		Price:              100,
		High24h:            115,
		Low24h:             95,
		Volume24h:          5_000_000,
		BidAskSpread:       0.05,
		OrderBookImbalance: 0.05,
		RangePosition24h:   18,
		PriceChange24h:     -0.10,
		RecentPriceChange:  -0.20,
		RecentChangeKnown:  true,
	})

	require.NotNil(t, decision)
	require.Equal(t, "buy", decision.Action)
	require.Equal(t, "REV/USDT", decision.Symbol)
}

func TestScalpingBacktestEngine_BuildDecisionAllowsValidatedSellWindow(t *testing.T) {
	now := time.Date(2026, 5, 21, 2, 25, 30, 0, time.UTC)
	engine := newRunSignalsTestEngine(now)
	engine.config.RequireRecentMomentum = true
	engine.config.MinRecentMomentumPct = 0.05
	engine.config.MaxBidAskSpreadPct = 0.08

	decision, _ := engine.buildDecisionFromSignal(context.Background(), MarketSignal{
		Symbol:             "SW/USDT",
		Price:              100,
		High24h:            115,
		Low24h:             95,
		Volume24h:          5_000_000,
		BidAskSpread:       0.09,
		OrderBookImbalance: -0.45,
		RangePosition24h:   55,
		PriceChange24h:     0.30,
		RecentPriceChange:  -0.10,
		RecentChangeKnown:  true,
	})

	require.NotNil(t, decision)
	require.Equal(t, "sell", decision.Action)
	require.Equal(t, "SW/USDT", decision.Symbol)
}

func TestScalpingBacktestEngine_RunSignalsAllowsBufferedMidRangeBuy(t *testing.T) {
	now := time.Date(2026, 5, 12, 2, 48, 0, 0, time.UTC)
	engine := newRunSignalsTestEngine(now)

	result, err := engine.RunSignals(context.Background(), []HistoricalSignal{{
		Timestamp: now,
		Symbol:    "FARTCOIN/USDT",
		Exchange:  "bitget",
		Signal: MarketSignal{
			Symbol:             "FARTCOIN/USDT",
			Price:              1.25,
			High24h:            1.50,
			Low24h:             1.00,
			Low:                1.24,
			High:               1.26,
			Volume24h:          1_500_000,
			BidAskSpread:       0.07,
			OrderBookImbalance: 0.22,
			RangePosition24h:   48,
			PriceChange24h:     0.8,
		},
	}, {
		Timestamp: now.Add(time.Minute),
		Symbol:    "FARTCOIN/USDT",
		Exchange:  "bitget",
		Signal: MarketSignal{
			Symbol:             "FARTCOIN/USDT",
			Price:              1.35,
			High24h:            1.50,
			Low24h:             1.00,
			Low:                1.34,
			High:               1.36,
			Volume24h:          1_500_000,
			BidAskSpread:       0.07,
			OrderBookImbalance: 0.22,
			RangePosition24h:   48,
			PriceChange24h:     0.8,
		},
	}})

	require.NoError(t, err)
	require.Equal(t, 1, result.Summary.EligibleSignals)
	require.Equal(t, 1, result.Summary.TotalTrades)
	require.Equal(t, 1, result.Summary.RejectionByReason["entry_without_close_signal"])
	require.Len(t, result.Trades, 1)
	require.Equal(t, "buy", result.Trades[0].Side)
	require.True(t, result.Summary.TotalPnL.GreaterThan(decimal.Zero))
}

func TestScalpingBacktestEngine_RunSignalsUsesRecentMomentumForFallback(t *testing.T) {
	now := time.Date(2026, 5, 12, 2, 49, 0, 0, time.UTC)
	engine := newRunSignalsTestEngine(now)

	result, err := engine.RunSignals(context.Background(), []HistoricalSignal{{
		Timestamp: now,
		Symbol:    "DOGE/USDT",
		Exchange:  "bitget",
		Signal: MarketSignal{
			Symbol:             "DOGE/USDT",
			Price:              1,
			High24h:            1.1,
			Low24h:             0.9,
			Low:                0.999,
			High:               1.001,
			Volume24h:          1_500_000,
			BidAskSpread:       0.035,
			OrderBookImbalance: 0.60,
			RangePosition24h:   18,
			PriceChange24h:     -4.0,
			RecentPriceChange:  0.15,
			RecentChangeKnown:  true,
		},
	}, {
		Timestamp: now.Add(time.Minute),
		Symbol:    "DOGE/USDT",
		Exchange:  "bitget",
		Signal: MarketSignal{
			Symbol:             "DOGE/USDT",
			Price:              1.08,
			High24h:            1.1,
			Low24h:             0.9,
			Low:                1.079,
			High:               1.081,
			Volume24h:          1_500_000,
			BidAskSpread:       0.035,
			OrderBookImbalance: 0.60,
			RangePosition24h:   18,
			PriceChange24h:     -4.0,
			RecentPriceChange:  0.20,
			RecentChangeKnown:  true,
		},
	}})

	require.NoError(t, err)
	require.Equal(t, 1, result.Summary.EligibleSignals)
	require.Equal(t, 1, result.Summary.TotalTrades)
	require.Equal(t, 1, result.Summary.RejectionByReason["entry_without_close_signal"])
	require.True(t, result.Summary.TotalPnL.GreaterThan(decimal.Zero))
}

func TestScalpingBacktestEngine_RunSignalsWithRecentMomentumRequiresDefaultImbalance(t *testing.T) {
	now := time.Date(2026, 5, 19, 8, 18, 0, 0, time.UTC)
	engine := newRunSignalsTestEngine(now)
	engine.config.RequireRecentMomentum = true
	engine.config.MinRecentMomentumPct = 0.05

	result, err := engine.RunSignals(context.Background(), []HistoricalSignal{{
		Timestamp: now,
		Symbol:    "GOAT/USDT",
		Exchange:  "bitget",
		Signal: MarketSignal{
			Symbol:             "GOAT/USDT",
			Price:              0.01944,
			High24h:            0.022,
			Low24h:             0.015,
			Volume24h:          1_500_000,
			BidAskSpread:       0.03544,
			OrderBookImbalance: 0.25456,
			RangePosition24h:   38.36,
			PriceChange24h:     0.05595,
			RecentPriceChange:  0.41322,
			RecentChangeKnown:  true,
		},
	}, {
		Timestamp: now.Add(time.Minute),
		Symbol:    "GOAT/USDT",
		Exchange:  "bitget",
		Signal: MarketSignal{
			Symbol:             "GOAT/USDT",
			Price:              0.01942,
			High24h:            0.022,
			Low24h:             0.015,
			Volume24h:          1_500_000,
			BidAskSpread:       0.03544,
			OrderBookImbalance: 0.25456,
			RangePosition24h:   38.0,
			PriceChange24h:     0.05595,
			RecentPriceChange:  0.30,
			RecentChangeKnown:  true,
		},
	}})

	require.NoError(t, err)
	require.Equal(t, 0, result.Summary.EligibleSignals)
	require.Equal(t, 0, result.Summary.TotalTrades)
	require.Equal(t, 2, result.Summary.RejectionByReason["recent_buy_range_too_high"])
}

func TestScalpingBacktestEngine_RunSignalsWithRecentMomentumRejectsNearMaxSpreadBuy(t *testing.T) {
	now := time.Date(2026, 5, 19, 8, 30, 0, 0, time.UTC)
	engine := newRunSignalsTestEngine(now)
	engine.config.RequireRecentMomentum = true
	engine.config.MinRecentMomentumPct = 0.05

	result, err := engine.RunSignals(context.Background(), []HistoricalSignal{{
		Timestamp: now,
		Symbol:    "PROS/USDT",
		Exchange:  "bitget",
		Signal: MarketSignal{
			Symbol:             "PROS/USDT",
			Price:              0.6286,
			High24h:            0.75,
			Low24h:             0.50,
			Volume24h:          1_500_000,
			BidAskSpread:       0.07954,
			OrderBookImbalance: 0.63331,
			RangePosition24h:   24.94,
			PriceChange24h:     -0.08872,
			RecentPriceChange:  0.12743,
			RecentChangeKnown:  true,
		},
	}, {
		Timestamp: now.Add(time.Minute),
		Symbol:    "PROS/USDT",
		Exchange:  "bitget",
		Signal: MarketSignal{
			Symbol:             "PROS/USDT",
			Price:              0.6290,
			High24h:            0.75,
			Low24h:             0.50,
			Volume24h:          1_500_000,
			BidAskSpread:       0.07954,
			OrderBookImbalance: 0.63331,
			RangePosition24h:   25.2,
			PriceChange24h:     -0.08872,
			RecentPriceChange:  0.10,
			RecentChangeKnown:  true,
		},
	}})

	require.NoError(t, err)
	require.Equal(t, 0, result.Summary.EligibleSignals)
	require.Equal(t, 0, result.Summary.TotalTrades)
	require.Equal(t, 2, result.Summary.RejectionByReason["recent_buy_spread_too_wide"])
}

func TestScalpingBacktestEngine_RunSignalsRejectsENJRecentBuyLossShape(t *testing.T) {
	now := time.Date(2026, 5, 20, 18, 8, 0, 0, time.UTC)
	engine := newRunSignalsTestEngine(now)
	engine.config.RequireRecentMomentum = true
	engine.config.MinRecentMomentumPct = 0.05

	result, err := engine.RunSignals(context.Background(), []HistoricalSignal{{
		Timestamp: now,
		Symbol:    "ENJ/USDT",
		Exchange:  "binance",
		Signal: MarketSignal{
			Symbol:             "ENJ/USDT",
			Price:              0.0875,
			High24h:            0.09,
			Low24h:             0.079,
			Volume24h:          1_500_000,
			BidAskSpread:       0.04410,
			OrderBookImbalance: 0.4130,
			RangePosition24h:   22.7160,
			PriceChange24h:     3.7757,
			RecentPriceChange:  0.1325,
			RecentChangeKnown:  true,
		},
	}})

	require.NoError(t, err)
	require.Equal(t, 0, result.Summary.EligibleSignals)
	require.Equal(t, 0, result.Summary.TotalTrades)
	require.Equal(t, 1, result.Summary.RejectionByReason["recent_buy_spread_too_wide"])
}

func TestScalpingBacktestEngine_RunSignalsWithRecentMomentumRejectsDowntrendBuy(t *testing.T) {
	now := time.Date(2026, 5, 19, 8, 41, 0, 0, time.UTC)
	engine := newRunSignalsTestEngine(now)
	engine.config.RequireRecentMomentum = true
	engine.config.MinRecentMomentumPct = 0.05

	result, err := engine.RunSignals(context.Background(), []HistoricalSignal{{
		Timestamp: now,
		Symbol:    "BILL/USDT",
		Exchange:  "bitget",
		Signal: MarketSignal{
			Symbol:             "BILL/USDT",
			Price:              0.118057,
			High24h:            0.13,
			Low24h:             0.118,
			Volume24h:          1_500_000,
			BidAskSpread:       0.03388,
			OrderBookImbalance: 0.55583,
			RangePosition24h:   1.96,
			PriceChange24h:     -0.18599,
			RecentPriceChange:  0.08817,
			RecentChangeKnown:  true,
		},
	}, {
		Timestamp: now.Add(time.Minute),
		Symbol:    "BILL/USDT",
		Exchange:  "bitget",
		Signal: MarketSignal{
			Symbol:             "BILL/USDT",
			Price:              0.11826,
			High24h:            0.13,
			Low24h:             0.118,
			Volume24h:          1_500_000,
			BidAskSpread:       0.03388,
			OrderBookImbalance: 0.55583,
			RangePosition24h:   2.5,
			PriceChange24h:     -0.18599,
			RecentPriceChange:  0.09,
			RecentChangeKnown:  true,
		},
	}})

	require.NoError(t, err)
	require.Equal(t, 0, result.Summary.EligibleSignals)
	require.Equal(t, 0, result.Summary.TotalTrades)
	require.Equal(t, 2, result.Summary.RejectionByReason["recent_buy_trend_too_low"])
}

func TestScalpingBacktestEngine_RunSignalsWithRecentMomentumRejectsFlatTrendRangeBuy(t *testing.T) {
	now := time.Date(2026, 5, 19, 9, 31, 0, 0, time.UTC)
	engine := newRunSignalsTestEngine(now)
	engine.config.RequireRecentMomentum = true
	engine.config.MinRecentMomentumPct = 0.05

	result, err := engine.RunSignals(context.Background(), []HistoricalSignal{{
		Timestamp: now,
		Symbol:    "DOGE/USDT",
		Exchange:  "bitget",
		Signal: MarketSignal{
			Symbol:             "DOGE/USDT",
			Price:              0.10404,
			High24h:            0.12,
			Low24h:             0.094,
			Volume24h:          1_500_000,
			BidAskSpread:       0.00961,
			OrderBookImbalance: 0.81364,
			RangePosition24h:   38.89,
			PriceChange24h:     0,
			RecentPriceChange:  0.16367,
			RecentChangeKnown:  true,
		},
	}, {
		Timestamp: now.Add(time.Minute),
		Symbol:    "DOGE/USDT",
		Exchange:  "bitget",
		Signal: MarketSignal{
			Symbol:             "DOGE/USDT",
			Price:              0.10396,
			High24h:            0.12,
			Low24h:             0.094,
			Volume24h:          1_500_000,
			BidAskSpread:       0.00961,
			OrderBookImbalance: 0.81364,
			RangePosition24h:   38.4,
			PriceChange24h:     0,
			RecentPriceChange:  0.10,
			RecentChangeKnown:  true,
		},
	}})

	require.NoError(t, err)
	require.Equal(t, 0, result.Summary.EligibleSignals)
	require.Equal(t, 0, result.Summary.TotalTrades)
	require.Equal(t, 2, result.Summary.RejectionByReason["recent_buy_trend_too_low"])
}

func TestScalpingBacktestEngine_RunSignalsWithRecentMomentumAllowsStrongBookBuy(t *testing.T) {
	now := time.Date(2026, 5, 19, 8, 19, 0, 0, time.UTC)
	engine := newRunSignalsTestEngine(now)
	engine.config.RequireRecentMomentum = true
	engine.config.MinRecentMomentumPct = 0.05

	result, err := engine.RunSignals(context.Background(), []HistoricalSignal{{
		Timestamp: now,
		Symbol:    "GOAT/USDT",
		Exchange:  "bitget",
		Signal: MarketSignal{
			Symbol:             "GOAT/USDT",
			Price:              0.01944,
			High24h:            0.022,
			Low24h:             0.015,
			Low:                0.01943,
			High:               0.01945,
			Volume24h:          1_500_000,
			BidAskSpread:       0.03544,
			OrderBookImbalance: 0.40572,
			RangePosition24h:   32.82,
			PriceChange24h:     0.025,
			RecentPriceChange:  0.0528,
			RecentChangeKnown:  true,
		},
	}, {
		Timestamp: now.Add(time.Minute),
		Symbol:    "GOAT/USDT",
		Exchange:  "bitget",
		Signal: MarketSignal{
			Symbol:             "GOAT/USDT",
			Price:              0.021,
			High24h:            0.022,
			Low24h:             0.015,
			Low:                0.0209,
			High:               0.0211,
			Volume24h:          1_500_000,
			BidAskSpread:       0.03544,
			OrderBookImbalance: 0.40572,
			RangePosition24h:   34.0,
			PriceChange24h:     0.025,
			RecentPriceChange:  0.06,
			RecentChangeKnown:  true,
		},
	}})

	require.NoError(t, err)
	require.Equal(t, 1, result.Summary.EligibleSignals)
	require.Equal(t, 1, result.Summary.TotalTrades)
	require.True(t, result.Summary.TotalPnL.GreaterThan(decimal.Zero))
}

func TestScalpingBacktestEngine_RunSignalsBlocksControlledBreakdownSellAfterObservedLosses(t *testing.T) {
	now := time.Date(2026, 5, 12, 2, 50, 0, 0, time.UTC)
	engine := newRunSignalsTestEngine(now)

	result, err := engine.RunSignals(context.Background(), []HistoricalSignal{{
		Timestamp: now,
		Symbol:    "WIF/USDT",
		Exchange:  "bitget",
		Signal: MarketSignal{
			Symbol:             "WIF/USDT",
			Price:              1.25,
			High24h:            1.50,
			Low24h:             1.00,
			Volume24h:          1_500_000,
			BidAskSpread:       0.06,
			OrderBookImbalance: -0.24,
			RangePosition24h:   24,
			PriceChange24h:     -0.8,
			RecentPriceChange:  -0.12,
			RecentChangeKnown:  true,
		},
	}, {
		Timestamp: now.Add(time.Minute),
		Symbol:    "WIF/USDT",
		Exchange:  "bitget",
		Signal: MarketSignal{
			Symbol:             "WIF/USDT",
			Price:              1.22,
			High24h:            1.50,
			Low24h:             1.00,
			Volume24h:          1_500_000,
			BidAskSpread:       0.06,
			OrderBookImbalance: -0.24,
			RangePosition24h:   24,
			PriceChange24h:     -0.8,
			RecentPriceChange:  -0.20,
			RecentChangeKnown:  true,
		},
	}})

	require.NoError(t, err)
	require.Equal(t, 0, result.Summary.EligibleSignals)
	require.Equal(t, 0, result.Summary.TotalTrades)
	require.Empty(t, result.Trades)
}

func TestScalpingBacktestEngine_RunSignalsEntryCutoffPreventsUnclosableEntries(t *testing.T) {
	now := time.Date(2026, 5, 12, 2, 51, 0, 0, time.UTC)
	engine := newRunSignalsTestEngine(now)
	engine.config.EntryCutoffTime = now.Add(30 * time.Second)

	result, err := engine.RunSignals(context.Background(), []HistoricalSignal{
		runSignalsTestSignal(now, "AAA/USDT", 100, 0.50, 35),
		runSignalsTestSignal(now.Add(time.Minute), "BBB/USDT", 50, -0.45, 65),
	})

	require.NoError(t, err)
	require.Equal(t, 2, result.Summary.TotalSignals)
	require.Equal(t, 0, result.Summary.EligibleSignals)
	require.Equal(t, 0, result.Summary.TotalTrades)
	require.Equal(t, 0, result.Summary.OpenPositions)
	require.Equal(t, 1, result.Summary.RejectionByReason["entry_without_close_signal"])
	require.Equal(t, 1, result.Summary.RejectionByReason["entry_cutoff_window"])
	require.Equal(t, "entry_cutoff", result.Signals[1].FunnelStage)
}

func TestScalpingBacktestEngine_RunSignalsCanRequireRecentMomentum(t *testing.T) {
	now := time.Date(2026, 5, 12, 2, 51, 30, 0, time.UTC)
	engine := newRunSignalsTestEngine(now)
	engine.config.RequireRecentMomentum = true
	engine.config.MinRecentMomentumPct = 0.05
	engine.config.EndTime = now.Add(5 * time.Minute)

	result, err := engine.RunSignals(context.Background(), []HistoricalSignal{{
		Timestamp: now,
		Symbol:    "AAA/USDT",
		Exchange:  "bitget",
		Signal: MarketSignal{
			Symbol:             "AAA/USDT",
			Price:              100,
			High24h:            110,
			Low24h:             90,
			Low:                99.9,
			High:               100.1,
			Volume24h:          1_500_000,
			BidAskSpread:       0.035,
			OrderBookImbalance: 0.50,
			RangePosition24h:   35,
			BBPercentB:         0.1,
			PriceChange24h:     1.2,
		},
	}, {
		Timestamp: now.Add(time.Minute),
		Symbol:    "AAA/USDT",
		Exchange:  "bitget",
		Signal: MarketSignal{
			Symbol:             "AAA/USDT",
			Price:              102,
			High24h:            110,
			Low24h:             90,
			Low:                101.9,
			High:               102.1,
			Volume24h:          1_500_000,
			BidAskSpread:       0.035,
			OrderBookImbalance: 0.50,
			RangePosition24h:   35,
			BBPercentB:         0.1,
			PriceChange24h:     1.2,
			RecentPriceChange:  0.03,
			RecentChangeKnown:  true,
		},
	}, {
		Timestamp: now.Add(2 * time.Minute),
		Symbol:    "AAA/USDT",
		Exchange:  "bitget",
		Signal: MarketSignal{
			Symbol:             "AAA/USDT",
			Price:              104,
			High24h:            110,
			Low24h:             90,
			Low:                103.9,
			High:               104.1,
			Volume24h:          1_500_000,
			BidAskSpread:       0.035,
			OrderBookImbalance: 0.50,
			RangePosition24h:   35,
			BBPercentB:         0.1,
			PriceChange24h:     1.2,
			RecentPriceChange:  0.08,
			RecentChangeKnown:  true,
		},
	}, {
		Timestamp: now.Add(3 * time.Minute),
		Symbol:    "AAA/USDT",
		Exchange:  "bitget",
		Signal: MarketSignal{
			Symbol:             "AAA/USDT",
			Price:              108,
			High24h:            110,
			Low24h:             90,
			Low:                107.9,
			High:               108.1,
			Volume24h:          1_500_000,
			BidAskSpread:       0.035,
			OrderBookImbalance: 0.50,
			RangePosition24h:   35,
			BBPercentB:         0.1,
			PriceChange24h:     1.2,
			RecentPriceChange:  0.08,
			RecentChangeKnown:  true,
		},
	}})

	require.NoError(t, err)
	require.Equal(t, 1, result.Summary.EligibleSignals)
	require.Equal(t, 1, result.Summary.TotalTrades)
	require.True(t, result.Summary.TotalPnL.GreaterThan(decimal.Zero))
}

func TestScalpingBacktestEngine_RunSignalsDoesNotCloseSingleSignalAtSyntheticProfit(t *testing.T) {
	now := time.Date(2026, 5, 12, 2, 49, 0, 0, time.UTC)
	engine := newRunSignalsTestEngine(now)

	result, err := engine.RunSignals(context.Background(), []HistoricalSignal{
		runSignalsTestSignal(now, "AAA/USDT", 100, 0.50, 35),
	})

	require.NoError(t, err)
	require.Equal(t, 1, result.Summary.TotalSignals)
	require.Equal(t, 0, result.Summary.EligibleSignals)
	require.Equal(t, 0, result.Summary.TotalTrades)
	require.Equal(t, 0, result.Summary.OpenPositions)
	require.Equal(t, 1, result.Summary.RejectionByReason["entry_without_close_signal"])
	require.Empty(t, result.Trades)
	require.True(t, result.Summary.TotalPnL.IsZero())
}

func TestScalpingBacktestEngine_RunSignalsDoesNotCloseBeforeHoldPeriod(t *testing.T) {
	now := time.Date(2026, 5, 12, 2, 49, 30, 0, time.UTC)
	engine := newRunSignalsTestEngine(now)

	result, err := engine.RunSignals(context.Background(), []HistoricalSignal{
		runSignalsTestSignal(now, "AAA/USDT", 100, 0.50, 35),
		runSignalsTestSignal(now.Add(30*time.Second), "AAA/USDT", 103, 0.50, 35),
	})

	require.NoError(t, err)
	require.Equal(t, 2, result.Summary.TotalSignals)
	require.Equal(t, 0, result.Summary.EligibleSignals)
	require.Equal(t, 0, result.Summary.TotalTrades)
	require.Equal(t, 0, result.Summary.OpenPositions)
	require.Equal(t, 2, result.Summary.RejectionByReason["entry_without_close_signal"])
	require.Empty(t, result.Trades)
	require.True(t, result.Summary.TotalPnL.IsZero())
}

func TestScalpingBacktestEngine_RunSignalsSortsInputsChronologically(t *testing.T) {
	now := time.Date(2026, 5, 12, 2, 50, 0, 0, time.UTC)
	engine := newRunSignalsTestEngine(now)

	result, err := engine.RunSignals(context.Background(), []HistoricalSignal{
		runSignalsTestSignal(now.Add(30*time.Second), "BBB/USDT", 50, -0.45, 65),
		runSignalsTestSignal(now, "AAA/USDT", 100, 0.50, 35),
	})

	require.NoError(t, err)
	require.Len(t, result.Signals, 2)
	require.True(t, result.Signals[0].Timestamp.Before(result.Signals[1].Timestamp))
	require.Equal(t, "AAA/USDT", result.Signals[0].Symbol)
}

func TestScalpingBacktestEngine_RunSignalsRespectsCanceledContext(t *testing.T) {
	now := time.Date(2026, 5, 12, 2, 55, 0, 0, time.UTC)
	engine := newRunSignalsTestEngine(now)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := engine.RunSignals(ctx, []HistoricalSignal{
		runSignalsTestSignal(now, "AAA/USDT", 100, 0.50, 35),
	})

	require.ErrorIs(t, err, context.Canceled)
}

func TestScalpingBacktestEngine_RunSignalsUsesConfiguredFallbackThresholds(t *testing.T) {
	now := time.Date(2026, 5, 12, 2, 58, 0, 0, time.UTC)
	engine := newRunSignalsTestEngine(now)
	engine.config.RequireRecentMomentum = true
	engine.config.DeterministicFallback = DeterministicFallbackConfig{
		MinImbalance:          0.60,
		MaxBidAskSpread:       0.08,
		BuyRangeMax:           45,
		BuyMinPriceChangePct:  0.05,
		SellMaxPriceChangePct: -0.05,
	}.Normalized()

	result, err := engine.RunSignals(context.Background(), []HistoricalSignal{
		runSignalsTestSignal(now, "AAA/USDT", 100, 0.50, 35),
		runSignalsTestSignal(now.Add(30*time.Second), "AAA/USDT", 101, 0.50, 35),
	})

	require.NoError(t, err)
	require.Zero(t, result.Summary.TotalTrades)
	require.Equal(t, 2, result.Summary.RejectionByReason["imbalance_too_weak"])
}

func newRunSignalsTestEngine(now time.Time) *ScalpingBacktestEngine {
	return NewScalpingBacktestEngine(nil, ScalpingBacktestConfig{
		StartTime:          now.Add(-time.Minute),
		EndTime:            now.Add(time.Minute),
		Symbols:            []string{"AAA/USDT", "BBB/USDT"},
		Exchange:           "bitget",
		InitialCapital:     decimal.NewFromInt(48),
		FeeRate:            decimal.NewFromFloat(0.0006),
		SlippagePct:        decimal.NewFromFloat(DefaultScalpingBacktestSlippage),
		MaxBidAskSpreadPct: 1,
		MinConfidence:      0.55,
		MinExpectancyN:     99,
		MaxCapitalPct:      5,
		DefaultHoldPeriod:  time.Minute,
	})
}

func runSignalsTestSignal(timestamp time.Time, symbol string, price, imbalance, rangePosition float64) HistoricalSignal {
	bbPercentB := 0.1
	if imbalance < 0 {
		bbPercentB = 0.9
	}
	return HistoricalSignal{
		Timestamp: timestamp,
		Symbol:    symbol,
		Exchange:  "bitget",
		Signal: MarketSignal{
			Symbol:             symbol,
			Price:              price,
			High24h:            price * 1.2,
			Low24h:             price * 0.8,
			Low:                price * 0.999,
			High:               price * 1.001,
			Volume24h:          1_000_000,
			BidAskSpread:       0.035,
			OrderBookImbalance: imbalance,
			RangePosition24h:   rangePosition,
			BBPercentB:         bbPercentB,
			PriceChange24h:     runSignalsTestPriceChange(imbalance),
			RecentPriceChange:  runSignalsTestPriceChange(imbalance),
			RecentChangeKnown:  true,
		},
	}
}

func runSignalsTestPriceChange(imbalance float64) float64 {
	if imbalance < 0 {
		return -2.5
	}
	if imbalance == 0 {
		return 0
	}
	return 2.5
}

func TestScalpingBacktestEngine_RunSignalsForceClosesOpenPositionsAtEnd(t *testing.T) {
	now := time.Date(2026, 5, 12, 3, 0, 0, 0, time.UTC)
	engine := newRunSignalsTestEngine(now)

	signals := []HistoricalSignal{
		runSignalsTestSignal(now, "AAA/USDT", 100, 0.50, 35),
		runSignalsTestSignal(now.Add(30*time.Second), "BBB/USDT", 50, -0.45, 65),
		runSignalsTestSignal(now.Add(90*time.Second), "AAA/USDT", 102, 0.50, 35),
		runSignalsTestSignal(now.Add(120*time.Second), "BBB/USDT", 49, -0.45, 65),
	}

	result, err := engine.RunSignals(context.Background(), signals)

	require.NoError(t, err)
	require.Equal(t, 4, result.Summary.TotalSignals)
	require.Len(t, result.Trades, 2)
	require.Equal(t, 0, len(engine.positions), "all positions closed after run completes")
	for _, trade := range result.Trades {
		require.NotEqual(t, "end_of_run", trade.ExitReason)
	}
}

func TestScalpingBacktestEngine_ForceCloseSweepEmptiesPositions(t *testing.T) {
	now := time.Date(2026, 5, 12, 3, 0, 0, 0, time.UTC)
	engine := newRunSignalsTestEngine(now)

	engine.positions["bitget|AAA/USDT"] = &SimulatedPosition{
		Symbol:     "AAA/USDT",
		Side:       "buy",
		Size:       decimal.NewFromInt(1),
		Notional:   decimal.NewFromInt(100),
		EntryPrice: decimal.NewFromInt(100),
		EntryTime:  now.Add(-time.Minute),
		Signal:     MarketSignal{Symbol: "AAA/USDT", Price: 100},
	}

	engine.sweepRemainingPositions(map[string]float64{"AAA/USDT": 102}, nil)

	require.Equal(t, 0, len(engine.positions), "no positions remain after sweep")
	require.Len(t, engine.tradeHistory, 1)
	require.Equal(t, "force_close_end_of_run", engine.tradeHistory[0].ExitReason)
	require.Equal(t, "AAA/USDT", engine.tradeHistory[0].Symbol)
}

// TestIsSQLiteTradingPairDB covers the production wrapping path: handlers
// pass the engine a readOnlyDBPoolAdapter (not a raw *SQLiteDB), so the
// type switch must unwrap the adapter or the engine emits PostgreSQL-flavored
// SQL against SQLite and returns 500.
//
// Regression for the bug found on 2026-06-10:
// "scalping backtest returns 500: near 'FROM': syntax error".
func TestIsSQLiteTradingPairDB(t *testing.T) {
	sqliteDB, err := database.NewSQLiteConnection(":memory:")
	require.NoError(t, err)
	defer sqliteDB.Close()

	t.Run("raw_sqlite", func(t *testing.T) {
		require.True(t, isSQLiteTradingPairDB(sqliteDB))
	})

	t.Run("wrapped_readonly_adapter", func(t *testing.T) {
		wrapped := readOnlyDBPoolAdapter{pool: sqliteDB}
		require.True(t, isSQLiteTradingPairDB(wrapped),
			"production callers pass a readOnlyDBPoolAdapter; must still detect SQLite")
	})

	t.Run("unknown_db_is_not_sqlite", func(t *testing.T) {
		var unknown DBPool = nil
		require.False(t, isSQLiteTradingPairDB(unknown))
	})
}

// TestIsSQLiteTradingPairDB_HandlersAdapterRegression covers the production
// wrapping path used by the HTTP handler: it passes a *handlers*-package
// read-only adapter wrapping *SQLiteDB. The services package cannot import
// the handlers package, so the fix introduces a sqlitePoolProbe marker
// interface that wraps can opt into.
//
// Regression for the bug found on 2026-06-10:
// "scalping backtest returns 500: near 'FROM': syntax error" caused by
// the engine emitting PostgreSQL-flavored SQL against SQLite.
func TestIsSQLiteTradingPairDB_HandlersAdapterRegression(t *testing.T) {
	sqliteDB, err := database.NewSQLiteConnection(":memory:")
	require.NoError(t, err)
	defer sqliteDB.Close()

	// Simulate the handlers package's adapter that implements the marker.
	shim := &markerSQLiteShim{pool: sqliteDB}
	require.True(t, isSQLiteTradingPairDB(shim),
		"shim implementing sqlitePoolProbe wrapping *SQLiteDB must be detected as SQLite")
	require.False(t, isSQLiteTradingPairDB(nil))
	require.False(t, isSQLiteTradingPairDB(markerNonSQLiteShim{}),
		"shim whose IsSQLitePool()=false must not match")
}

func TestScalpingBacktestConfig_AsymmetricExit(t *testing.T) {
	cfg := ScalpingBacktestConfig{
		AsymmetricExit: AsymmetricExitConfig{
			UseAsymmetricExits:  true,
			StopLossPct:         0.005,
			TakeProfitPct:       0.015,
			BreakevenEnabled:    true,
			TrailingStopEnabled: true,
		},
	}
	require.True(t, cfg.AsymmetricExit.UseAsymmetricExits)
	require.Equal(t, 0.005, cfg.AsymmetricExit.StopLossPct)
	require.Equal(t, 0.015, cfg.AsymmetricExit.TakeProfitPct)
	require.True(t, cfg.AsymmetricExit.BreakevenEnabled)
	require.True(t, cfg.AsymmetricExit.TrailingStopEnabled)

	engine := NewScalpingBacktestEngine(nil, cfg)
	require.True(t, engine.config.AsymmetricExit.UseAsymmetricExits)
	require.Equal(t, 0.005, engine.config.AsymmetricExit.StopLossPct)
	require.Equal(t, 0.015, engine.config.AsymmetricExit.TakeProfitPct)
}

func TestBacktestExitLevels_Asymmetric(t *testing.T) {
	price := 100.0
	cfg := AsymmetricExitConfig{
		UseAsymmetricExits: true,
		StopLossPct:        0.005,
		TakeProfitPct:      0.015,
	}

	t.Run("buy", func(t *testing.T) {
		sl, tp := backtestExitLevels(price, "buy", 10, cfg)
		// SL = 100 * (1 - 0.005/10) = 100 * 0.9995 = 99.95
		// TP = 100 * (1 + 0.015/10) = 100 * 1.0015 = 100.15
		require.True(t, sl.Equal(decimal.NewFromFloat(99.95)), "SL = %s", sl)
		require.True(t, tp.Equal(decimal.NewFromFloat(100.15)), "TP = %s", tp)
	})

	t.Run("sell", func(t *testing.T) {
		sl, tp := backtestExitLevels(price, "sell", 10, cfg)
		// SL = 100 * (1 + 0.005/10) = 100 * 1.0005 = 100.05
		// TP = 100 * (1 - 0.015/10) = 100 * 0.9985 = 99.85
		require.True(t, sl.Equal(decimal.NewFromFloat(100.05)), "SL = %s", sl)
		require.True(t, tp.Equal(decimal.NewFromFloat(99.85)), "TP = %s", tp)
	})

	t.Run("no_leverage", func(t *testing.T) {
		sl, tp := backtestExitLevels(price, "buy", 0, cfg)
		// leverage defaults to 1: SL = 99.5, TP = 101.5
		require.True(t, sl.Equal(decimal.NewFromFloat(99.5)), "SL = %s", sl)
		require.True(t, tp.Equal(decimal.NewFromFloat(101.5)), "TP = %s", tp)
	})
}

func TestBacktestExitLevels_Symmetric(t *testing.T) {
	price := 100.0
	cfg := AsymmetricExitConfig{UseAsymmetricExits: false}

	t.Run("buy", func(t *testing.T) {
		sl, tp := backtestExitLevels(price, "buy", 1, cfg)
		// Default symmetric: 0.8% stop, 1.2% target
		// SL = 100 * (1 - 0.008) = 99.2
		// TP = 100 * (1 + 0.012) = 101.2
		require.True(t, sl.Equal(decimal.NewFromFloat(99.2)), "SL = %s", sl)
		require.True(t, tp.Equal(decimal.NewFromFloat(101.2)), "TP = %s", tp)
	})

	t.Run("sell", func(t *testing.T) {
		sl, tp := backtestExitLevels(price, "sell", 1, cfg)
		// SL = 100 * (1 + 0.008) = 100.8
		// TP = 100 * (1 - 0.012) = 98.8
		require.True(t, sl.Equal(decimal.NewFromFloat(100.8)), "SL = %s", sl)
		require.True(t, tp.Equal(decimal.NewFromFloat(98.8)), "TP = %s", tp)
	})
}

type markerSQLiteShim struct {
	pool *database.SQLiteDB
}

func (s *markerSQLiteShim) Query(ctx context.Context, q string, args ...any) (database.Rows, error) {
	return s.pool.Query(ctx, q, args...)
}
func (s *markerSQLiteShim) QueryRow(ctx context.Context, q string, args ...any) database.Row {
	return s.pool.QueryRow(ctx, q, args...)
}
func (s *markerSQLiteShim) Exec(ctx context.Context, q string, args ...any) (database.Result, error) {
	return s.pool.Exec(ctx, q, args...)
}
func (s *markerSQLiteShim) Begin(ctx context.Context) (database.Tx, error) {
	return nil, fmt.Errorf("read-only shim")
}
func (s *markerSQLiteShim) IsSQLitePool() bool { return true }

type markerNonSQLiteShim struct{}

func (markerNonSQLiteShim) Query(context.Context, string, ...any) (database.Rows, error) {
	return nil, nil
}
func (markerNonSQLiteShim) QueryRow(context.Context, string, ...any) database.Row {
	return nil
}
func (markerNonSQLiteShim) Exec(context.Context, string, ...any) (database.Result, error) {
	return nil, nil
}
func (markerNonSQLiteShim) Begin(context.Context) (database.Tx, error) {
	return nil, nil
}
func (markerNonSQLiteShim) IsSQLitePool() bool { return false }
