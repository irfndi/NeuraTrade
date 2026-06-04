package services

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/irfndi/neuratrade/internal/database"
	"github.com/pashagolub/pgxmock/v4"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewPaperTradingBackfillValidation(t *testing.T) {
	mockDB, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mockDB.Close()

	dbPool := database.NewMockDBPool(mockDB)
	executor := NewPaperExecutionSimulator(DefaultPaperExecutionConfig())
	recorder := NewPaperTradeRecorder(dbPool, nil)

	tests := []struct {
		name   string
		config PaperTradingBackfillConfig
	}{
		{
			name:   "default config",
			config: PaperTradingBackfillConfig{},
		},
		{
			name: "custom config",
			config: PaperTradingBackfillConfig{
				StartTime:          time.Now().Add(-7 * 24 * time.Hour),
				EndTime:            time.Now(),
				Exchange:           "binance",
				InitialCapital:     decimal.NewFromInt(50000),
				MinContinuousHours: 168,
				MinStrategies:      2,
				MinClosedTrades:    20,
				MinWinRatePct:      40,
				MaxDrawdownPct:     30,
				Strategies: []PaperTradingStrategy{
					{ID: "scalping", Symbols: []string{"BTC/USDT"}, Timeframe: "5m", HoldCandles: 3},
					{ID: "daily", Symbols: []string{"ETH/USDT"}, Timeframe: "1h", HoldCandles: 4},
				},
			},
		},
		{
			name: "nil logger defaults to nop",
			config: PaperTradingBackfillConfig{
				StartTime: time.Now().Add(-24 * time.Hour),
				EndTime:   time.Now(),
				Strategies: []PaperTradingStrategy{
					{ID: "test", Symbols: []string{"BTC/USDT"}, Timeframe: "1h"},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v := NewPaperTradingBackfillValidation(dbPool, executor, recorder, tt.config, nil)
			require.NotNil(t, v)
			assert.NotNil(t, v.config)
			assert.NotNil(t, v.logger)
			assert.True(t, v.config.InitialCapital.GreaterThan(decimal.Zero))
			assert.Greater(t, v.config.MinContinuousHours, 0.0)
			assert.Greater(t, v.config.MinStrategies, 0)
			assert.NotEmpty(t, v.config.Strategies)
		})
	}
}

func TestPaperTradingBackfillValidation_ValidateConfig(t *testing.T) {
	tests := []struct {
		name    string
		config  PaperTradingBackfillConfig
		wantErr bool
		errMsg  string
	}{
		{
			name: "empty config - missing times (no normalization)",
			config: PaperTradingBackfillConfig{
				StartTime: time.Time{},
				EndTime:   time.Time{},
			},
			wantErr: true,
			errMsg:  "start_time and end_time are required",
		},
		{
			name: "start after end",
			config: PaperTradingBackfillConfig{
				StartTime:      time.Now(),
				EndTime:        time.Now().Add(-24 * time.Hour),
				InitialCapital: decimal.NewFromInt(10000),
				Strategies: []PaperTradingStrategy{
					{ID: "test", Symbols: []string{"BTC/USDT"}, Timeframe: "1h"},
				},
			},
			wantErr: true,
			errMsg:  "start_time must be before end_time",
		},
		{
			name: "too short window",
			config: PaperTradingBackfillConfig{
				StartTime:      time.Now().Add(-1 * time.Hour),
				EndTime:        time.Now(),
				InitialCapital: decimal.NewFromInt(10000),
				Strategies: []PaperTradingStrategy{
					{ID: "test", Symbols: []string{"BTC/USDT"}, Timeframe: "1h"},
				},
			},
			wantErr: true,
			errMsg:  "validation window must be at least 24 hours",
		},
		{
			name: "zero capital (no normalization)",
			config: PaperTradingBackfillConfig{
				StartTime:      time.Now().Add(-48 * time.Hour),
				EndTime:        time.Now(),
				InitialCapital: decimal.Zero,
				Exchange:       "binance",
				Strategies: []PaperTradingStrategy{
					{ID: "test", Symbols: []string{"BTC/USDT"}, Timeframe: "1h"},
				},
			},
			wantErr: true,
			errMsg:  "initial capital must be positive",
		},
		{
			name: "no strategies - test directly without normalization",
			config: PaperTradingBackfillConfig{
				StartTime:      time.Now().Add(-48 * time.Hour),
				EndTime:        time.Now(),
				InitialCapital: decimal.NewFromInt(10000),
				Exchange:       "binance",
				Strategies:     nil,
			},
			wantErr: true,
			errMsg:  "at least one strategy is required",
		},
		{
			name: "valid config",
			config: PaperTradingBackfillConfig{
				StartTime:      time.Now().Add(-7 * 24 * time.Hour),
				EndTime:        time.Now(),
				Exchange:       "binance",
				InitialCapital: decimal.NewFromInt(10000),
				Strategies: []PaperTradingStrategy{
					{ID: "scalping", Symbols: []string{"BTC/USDT"}, Timeframe: "5m"},
				},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockDB, err := pgxmock.NewPool()
			require.NoError(t, err)
			defer mockDB.Close()

			dbPool := database.NewMockDBPool(mockDB)
			executor := NewPaperExecutionSimulator(DefaultPaperExecutionConfig())
			recorder := NewPaperTradeRecorder(dbPool, nil)

			var v *PaperTradingBackfillValidation
			if tt.name == "no strategies - test directly without normalization" ||
				tt.name == "empty config - missing times (no normalization)" ||
				tt.name == "zero capital (no normalization)" {
				v = &PaperTradingBackfillValidation{
					db:     dbPool,
					config: tt.config,
					logger: &backfillNopLogger{},
				}
			} else {
				v = NewPaperTradingBackfillValidation(dbPool, executor, recorder, tt.config, nil)
			}

			err = v.validateConfig()
			if tt.wantErr {
				require.Error(t, err)
				if tt.errMsg != "" {
					assert.Contains(t, err.Error(), tt.errMsg)
				}
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func makeTrendCloses(start, step int64, count int) []decimal.Decimal {
	closes := make([]decimal.Decimal, count)
	for i := 0; i < count; i++ {
		closes[i] = decimal.NewFromInt(start + step*int64(i))
	}
	return closes
}

func TestPaperTradingBackfillValidation_EvaluateCandleSignal(t *testing.T) {
	v := &PaperTradingBackfillValidation{
		config: normalizePaperTradingBackfillConfig(PaperTradingBackfillConfig{}),
	}

	strat := &PaperTradingStrategy{
		ID:             "test",
		MinConfidence:  0.01,
		MaxPositionPct: decimal.NewFromFloat(0.05),
	}

	uptrendCloses := makeTrendCloses(80, 1, 20)    // 80..99, last=99 > first=80
	downtrendCloses := makeTrendCloses(99, -1, 20) // 99..80, last=80 < first=99

	tests := []struct {
		name         string
		candle       backfillCandle
		recentCloses []decimal.Decimal
		wantAction   string
		wantSide     string
	}{
		{
			name: "uptrend green above sma should buy",
			candle: backfillCandle{
				Open:   decimal.NewFromInt(100),
				High:   decimal.NewFromInt(110),
				Low:    decimal.NewFromInt(90),
				Close:  decimal.NewFromInt(105),
				Volume: decimal.NewFromInt(2000),
			},
			recentCloses: uptrendCloses,
			wantAction:   "buy",
			wantSide:     "long",
		},
		{
			name: "uptrend green below sma should hold",
			candle: backfillCandle{
				Open:   decimal.NewFromInt(90),
				High:   decimal.NewFromInt(105),
				Low:    decimal.NewFromInt(85),
				Close:  decimal.NewFromInt(93),
				Volume: decimal.NewFromInt(2000),
			},
			recentCloses: uptrendCloses,
			wantAction:   "hold",
			wantSide:     "",
		},
		{
			name: "uptrend red candle should hold",
			candle: backfillCandle{
				Open:   decimal.NewFromInt(100),
				High:   decimal.NewFromInt(105),
				Low:    decimal.NewFromInt(85),
				Close:  decimal.NewFromInt(93),
				Volume: decimal.NewFromInt(2000),
			},
			recentCloses: uptrendCloses,
			wantAction:   "hold",
			wantSide:     "",
		},
		{
			name: "downtrend red below sma should sell",
			candle: backfillCandle{
				Open:   decimal.NewFromInt(85),
				High:   decimal.NewFromInt(90),
				Low:    decimal.NewFromInt(70),
				Close:  decimal.NewFromInt(75),
				Volume: decimal.NewFromInt(2000),
			},
			recentCloses: downtrendCloses,
			wantAction:   "sell",
			wantSide:     "short",
		},
		{
			name: "downtrend green candle should hold",
			candle: backfillCandle{
				Open:   decimal.NewFromInt(88),
				High:   decimal.NewFromInt(105),
				Low:    decimal.NewFromInt(85),
				Close:  decimal.NewFromInt(93),
				Volume: decimal.NewFromInt(2000),
			},
			recentCloses: downtrendCloses,
			wantAction:   "hold",
			wantSide:     "",
		},
		{
			name: "flat candle should hold",
			candle: backfillCandle{
				Open:  decimal.NewFromInt(100),
				High:  decimal.NewFromInt(100),
				Low:   decimal.NewFromInt(100),
				Close: decimal.NewFromInt(100),
			},
			recentCloses: uptrendCloses,
			wantAction:   "hold",
			wantSide:     "",
		},
	}

	recentVolumes := make([]decimal.Decimal, 20)
	for i := range recentVolumes {
		recentVolumes[i] = decimal.NewFromInt(1000)
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, action, side := v.evaluateCandleSignal(tt.candle, strat, tt.recentCloses, recentVolumes)
			assert.Equal(t, tt.wantAction, action)
			assert.Equal(t, tt.wantSide, side)
		})
	}
}

func TestPaperTradingBackfillValidation_EvaluateCandleSignal_HighConfidence(t *testing.T) {
	v := &PaperTradingBackfillValidation{
		config: normalizePaperTradingBackfillConfig(PaperTradingBackfillConfig{}),
	}

	strat := &PaperTradingStrategy{
		ID:             "test",
		MinConfidence:  0.01,
		MaxPositionPct: decimal.NewFromFloat(0.05),
	}

	recentCloses := makeTrendCloses(80, 1, 20) // uptrend: 80..99
	recentVolumes := make([]decimal.Decimal, 20)
	for i := range recentVolumes {
		recentVolumes[i] = decimal.NewFromInt(1000)
	}

	// Valid trend-following signal: green candle in uptrend above SMA-5
	candle := backfillCandle{
		Open:   decimal.NewFromInt(100),
		High:   decimal.NewFromInt(110),
		Low:    decimal.NewFromInt(90),
		Close:  decimal.NewFromInt(108),
		Volume: decimal.NewFromInt(2000),
	}

	confidence, action, side := v.evaluateCandleSignal(candle, strat, recentCloses, recentVolumes)
	assert.GreaterOrEqual(t, confidence, strat.MinConfidence)
	assert.Equal(t, "buy", action)
	assert.Equal(t, "long", side)

	// Red candle below SMA should be ignored in uptrend (only longs allowed)
	redCandle := backfillCandle{
		Open:   decimal.NewFromInt(100),
		High:   decimal.NewFromInt(105),
		Low:    decimal.NewFromInt(90),
		Close:  decimal.NewFromInt(93),
		Volume: decimal.NewFromInt(2000),
	}
	confidence, action, side = v.evaluateCandleSignal(redCandle, strat, recentCloses, recentVolumes)
	assert.Equal(t, 0.0, confidence)
	assert.Equal(t, "hold", action)
	assert.Equal(t, "", side)
}

func TestPaperTradingReadinessEvidenceBlockers(t *testing.T) {
	blockers := PaperTradingReadinessEvidenceBlockers()
	assert.Len(t, blockers, 11)
	assert.Contains(t, blockers, BlockerContinuousValidation)
	assert.Contains(t, blockers, BlockerMultiStrategyCoverage)
	assert.Contains(t, blockers, BlockerClosedTradeCount)
	assert.Contains(t, blockers, BlockerWinRateThreshold)
	assert.Contains(t, blockers, BlockerMaxDrawdownLimit)
	assert.Contains(t, blockers, BlockerRiskEnforcementEvidence)
	assert.Contains(t, blockers, BlockerBacktestComparison)
	assert.Contains(t, blockers, BlockerTradeDensity)
	assert.Contains(t, blockers, BlockerOrderExecutionPath)
	assert.Contains(t, blockers, BlockerPnLDistribution)
	assert.Contains(t, blockers, BlockerNonDiagnosticManifest)
}

func TestPaperTradingBackfillValidation_EvaluateBlockers(t *testing.T) {
	v := &PaperTradingBackfillValidation{
		config: PaperTradingBackfillConfig{
			MinContinuousHours: 168,
			MinStrategies:      2,
			MinClosedTrades:    10,
			MinWinRatePct:      30,
			MaxDrawdownPct:     50,
			StartTime:          time.Now().Add(-7 * 24 * time.Hour),
			EndTime:            time.Now(),
		},
	}

	result := &PaperTradingBackfillResult{
		ContinuousValidationHours: 170,
		StrategyCount:             3,
		CoveredStrategies:         []string{"scalping", "daily", "swing"},
		ClosedTrades:              50,
		WinRate:                   decimal.NewFromFloat(45.0),
		MaxDrawdownPct:            decimal.NewFromFloat(15.0),
		RiskEvents: []PaperTradingRiskEvent{
			{EventType: "drawdown_warning", Description: "Test drawdown"},
			{EventType: "loss_streak", Description: "Test streak"},
		},
		EvidenceArtifact: &PaperTradingValidationEvidence{
			RunID: "test",
		},
		Config: v.config,
	}

	blockers := v.evaluateBlockers(result)
	assert.Len(t, blockers, 11)

	allSatisfied := true
	for _, b := range blockers {
		if b.BlockerID == BlockerWinRateThreshold || b.BlockerID == BlockerMaxDrawdownLimit ||
			b.BlockerID == BlockerContinuousValidation || b.BlockerID == BlockerMultiStrategyCoverage {
			assert.True(t, b.Satisfied, "blocker %s should be satisfied", b.BlockerID)
		}
		if !b.Satisfied {
			allSatisfied = false
		}
	}
	assert.True(t, allSatisfied, "all blockers should be satisfied with valid result")
}

func TestPaperTradingBackfillValidation_EvaluateBlockers_Failures(t *testing.T) {
	v := &PaperTradingBackfillValidation{
		config: PaperTradingBackfillConfig{
			MinContinuousHours: 168,
			MinStrategies:      2,
			MinClosedTrades:    10,
			MinWinRatePct:      50,
			MaxDrawdownPct:     10,
			StartTime:          time.Now().Add(-3 * 24 * time.Hour),
			EndTime:            time.Now(),
		},
	}

	result := &PaperTradingBackfillResult{
		ContinuousValidationHours: 72,
		StrategyCount:             1,
		CoveredStrategies:         []string{"scalping"},
		ClosedTrades:              5,
		WinRate:                   decimal.NewFromFloat(20.0),
		MaxDrawdownPct:            decimal.NewFromFloat(35.0),
		RiskEvents:                []PaperTradingRiskEvent{},
		Config:                    v.config,
	}

	blockers := v.evaluateBlockers(result)
	assert.Len(t, blockers, 11)

	expectedFailures := map[PaperTradingBlockerID]bool{
		BlockerContinuousValidation:    false,
		BlockerMultiStrategyCoverage:   false,
		BlockerClosedTradeCount:        false,
		BlockerWinRateThreshold:        false,
		BlockerMaxDrawdownLimit:        false,
		BlockerRiskEnforcementEvidence: false,
	}

	for _, b := range blockers {
		if expected, exists := expectedFailures[b.BlockerID]; exists {
			assert.Equal(t, expected, b.Satisfied, "blocker %s satisfaction mismatch", b.BlockerID)
		}
	}
}

func TestPaperTradingBackfillValidation_BuildEvidenceArtifact(t *testing.T) {
	v := &PaperTradingBackfillValidation{
		config: PaperTradingBackfillConfig{
			StartTime: time.Now().Add(-7 * 24 * time.Hour),
			EndTime:   time.Now(),
		},
	}

	result := &PaperTradingBackfillResult{
		Config:                    v.config,
		ContinuousValidationHours: 168,
		StrategyCount:             2,
		CoveredStrategies:         []string{"scalping", "daily"},
		ClosedTrades:              30,
		NetPnL:                    decimal.NewFromFloat(150.5),
		WinRate:                   decimal.NewFromFloat(55.0),
		MaxDrawdownPct:            decimal.NewFromFloat(12.0),
		RiskEvents: []PaperTradingRiskEvent{
			{EventType: "drawdown_warning", Description: "test"},
		},
	}
	result.BlockerStatuses = v.evaluateBlockers(result)

	evidence := v.buildEvidenceArtifact(result, "test-run-id")
	require.NotNil(t, evidence)
	assert.Equal(t, "test-run-id", evidence.RunID)
	assert.Equal(t, 168.0, evidence.ContinuousHours)
	assert.Equal(t, int64(30), evidence.ClosedTrades)
	assert.True(t, evidence.NonDiagnostic)
	assert.NotEmpty(t, evidence.ArtifactDigest)
	assert.Equal(t, "backfill-test-run", evidence.ArtifactDigest)
}

func TestPaperTradingBackfillValidation_Manifests(t *testing.T) {
	v := &PaperTradingBackfillValidation{}
	result := &PaperTradingBackfillResult{
		EvidenceArtifact: &PaperTradingValidationEvidence{
			GeneratedAt:          time.Now(),
			RunID:                "test-run",
			AllBlockersSatisfied: true,
			NonDiagnostic:        true,
		},
	}

	data, err := v.Manifests(result)
	require.NoError(t, err)
	assert.NotEmpty(t, data)
	assert.Contains(t, string(data), "test-run")

	result.EvidenceArtifact = nil
	_, err = v.Manifests(result)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no evidence artifact available")
}

func TestPaperTradingBackfillValidation_Run_DBRequired(t *testing.T) {
	executor := NewPaperExecutionSimulator(DefaultPaperExecutionConfig())
	recorder := NewPaperTradeRecorder(nil, nil)
	v := NewPaperTradingBackfillValidation(nil, executor, recorder, DefaultPaperTradingBackfillConfig(), nil)

	_, err := v.Run(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "database pool is required")
}

func TestDefaultPaperTradingStrategies(t *testing.T) {
	strategies := DefaultPaperTradingStrategies()
	assert.Len(t, strategies, 4)

	strategyIDs := make(map[string]bool)
	symbolSets := make(map[string][]string)
	for _, s := range strategies {
		strategyIDs[s.ID] = true
		symbolSets[s.ID] = s.Symbols
		assert.NotEmpty(t, s.Name)
		assert.NotEmpty(t, s.Symbols)
		assert.NotEmpty(t, s.Timeframe)
		assert.True(t, s.MaxPositionPct.GreaterThan(decimal.Zero))
		assert.Greater(t, s.MinConfidence, 0.0)
	}

	assert.True(t, strategyIDs["scalping"])
	assert.True(t, strategyIDs["daily_trading"])
	assert.True(t, strategyIDs["swing_trading"])
	assert.True(t, strategyIDs["arbitrage"])

	// swing_trading must cover all 5 paper trading symbols (BTC, ETH, SOL, BNB, XRP).
	swingSymbols := symbolSets["swing_trading"]
	assert.Contains(t, swingSymbols, "BTC/USDT")
	assert.Contains(t, swingSymbols, "ETH/USDT")
	assert.Contains(t, swingSymbols, "SOL/USDT")
	assert.Contains(t, swingSymbols, "BNB/USDT")
	assert.Contains(t, swingSymbols, "XRP/USDT")
	assert.Len(t, swingSymbols, 5)
}

func TestDefaultPaperTradingStrategies_EnvOverride(t *testing.T) {
	t.Setenv(envPaperSymbols, "BTC/USDT,SOL/USDT")
	t.Cleanup(func() { t.Setenv(envPaperSymbols, "") })

	strategies := DefaultPaperTradingStrategies()
	assert.Len(t, strategies, 4)

	for _, s := range strategies {
		assert.Equal(t, []string{"BTC/USDT", "SOL/USDT"}, s.Symbols,
			"strategy %q should use env var symbols", s.ID)
	}
}

func TestPaperSymbolsFromEnv(t *testing.T) {
	t.Run("unset", func(t *testing.T) {
		t.Setenv(envPaperSymbols, "")
		assert.Nil(t, paperSymbolsFromEnv())
	})

	t.Run("single", func(t *testing.T) {
		t.Setenv(envPaperSymbols, "BTC/USDT")
		assert.Equal(t, []string{"BTC/USDT"}, paperSymbolsFromEnv())
	})

	t.Run("multiple", func(t *testing.T) {
		t.Setenv(envPaperSymbols, "BTC/USDT,ETH/USDT,SOL/USDT")
		assert.Equal(t, []string{"BTC/USDT", "ETH/USDT", "SOL/USDT"}, paperSymbolsFromEnv())
	})

	t.Run("dedup", func(t *testing.T) {
		t.Setenv(envPaperSymbols, "BTC/USDT,BTC/USDT,ETH/USDT")
		assert.Equal(t, []string{"BTC/USDT", "ETH/USDT"}, paperSymbolsFromEnv())
	})

	t.Run("whitespace", func(t *testing.T) {
		t.Setenv(envPaperSymbols, " BTC/USDT , ETH/USDT ")
		assert.Equal(t, []string{"BTC/USDT", "ETH/USDT"}, paperSymbolsFromEnv())
	})

	t.Run("order_preserved", func(t *testing.T) {
		t.Setenv(envPaperSymbols, "XRP/USDT,BTC/USDT,ETH/USDT")
		result := paperSymbolsFromEnv()
		assert.Equal(t, "XRP/USDT", result[0])
		assert.Equal(t, "BTC/USDT", result[1])
		assert.Equal(t, "ETH/USDT", result[2])
	})
}

func TestDefaultPaperTradingBackfillConfig(t *testing.T) {
	cfg := DefaultPaperTradingBackfillConfig()
	assert.False(t, cfg.StartTime.IsZero())
	assert.False(t, cfg.EndTime.IsZero())
	assert.Equal(t, 168.0, cfg.MinContinuousHours)
	assert.Equal(t, 2, cfg.MinStrategies)
	assert.Equal(t, int64(10), cfg.MinClosedTrades)
	assert.Equal(t, 50.0, cfg.MaxDrawdownPct)
	assert.Len(t, cfg.Strategies, 4)
	assert.Equal(t, decimal.NewFromInt(10000).String(), cfg.InitialCapital.String())
}

func TestNormalizePaperTradingBackfillConfig(t *testing.T) {
	cfg := normalizePaperTradingBackfillConfig(PaperTradingBackfillConfig{})
	assert.True(t, cfg.InitialCapital.GreaterThan(decimal.Zero))
	assert.Greater(t, cfg.MinContinuousHours, 0.0)
	assert.Greater(t, cfg.MinStrategies, 0)
	assert.Greater(t, cfg.MinClosedTrades, int64(0))
	assert.NotEmpty(t, cfg.Strategies)
	assert.NotEmpty(t, cfg.Exchange)
}

func TestPaperTradingBackfillValidation_BackfillNopLogger(t *testing.T) {
	l := &backfillNopLogger{}
	assert.NotNil(t, l)
	l.Info("test")
	l.Warn("test")
	l.Error("test")
	result := l.WithFields(nil)
	assert.NotNil(t, result)

	var logger Logger = &backfillNopLogger{}
	assert.NotNil(t, logger)
}

func TestPaperTradingBackfillValidation_Run_WithMockData(t *testing.T) {
	mockDB, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mockDB.Close()

	dbPool := database.NewMockDBPool(mockDB)

	now := time.Now()
	startTime := now.Add(-7 * 24 * time.Hour)

	execConfig := DefaultPaperExecutionConfig()
	execConfig.EnableRandomness = false
	execConfig.RejectionProbability = decimal.Zero

	executor := NewPaperExecutionSimulator(execConfig)
	recorder := NewPaperTradeRecorder(dbPool, nil)

	cfg := PaperTradingBackfillConfig{
		StartTime:      startTime,
		EndTime:        now,
		Exchange:       "binance",
		InitialCapital: decimal.NewFromInt(10000),
		Strategies: []PaperTradingStrategy{
			{
				ID:             "scalping",
				Symbols:        []string{"BTC/USDT"},
				Timeframe:      "5m",
				MaxPositionPct: decimal.NewFromFloat(0.05),
				MinConfidence:  0.01,
				HoldCandles:    3,
			},
		},
	}

	v := NewPaperTradingBackfillValidation(dbPool, executor, recorder, cfg, nil)

	candleTime := startTime.Add(1 * time.Hour)
	candleColumns := []string{"timestamp", "symbol", "exchange", "open", "high", "low", "close", "volume"}

	ohlcvRows := pgxmock.NewRows(candleColumns).
		AddRow(candleTime, "BTC/USDT", "binance", 50000.0, 50500.0, 49800.0, 50200.0, 100.5).
		AddRow(candleTime.Add(5*time.Minute), "BTC/USDT", "binance", 50200.0, 50800.0, 50100.0, 50500.0, 120.0).
		AddRow(candleTime.Add(10*time.Minute), "BTC/USDT", "binance", 50500.0, 51200.0, 50400.0, 51000.0, 150.0).
		AddRow(candleTime.Add(15*time.Minute), "BTC/USDT", "binance", 51000.0, 51500.0, 50800.0, 51200.0, 130.0).
		AddRow(candleTime.Add(20*time.Minute), "BTC/USDT", "binance", 51200.0, 51800.0, 51000.0, 51100.0, 140.0).
		AddRow(candleTime.Add(25*time.Minute), "BTC/USDT", "binance", 51100.0, 51400.0, 50800.0, 50900.0, 110.0).
		AddRow(candleTime.Add(30*time.Minute), "BTC/USDT", "binance", 50900.0, 51500.0, 50600.0, 51300.0, 160.0).
		AddRow(candleTime.Add(35*time.Minute), "BTC/USDT", "binance", 51300.0, 52000.0, 51100.0, 51800.0, 180.0).
		AddRow(candleTime.Add(40*time.Minute), "BTC/USDT", "binance", 51800.0, 52200.0, 51600.0, 51700.0, 120.0).
		AddRow(candleTime.Add(45*time.Minute), "BTC/USDT", "binance", 51700.0, 51900.0, 51400.0, 51500.0, 100.0)

	mockDB.ExpectQuery("SELECT od.timestamp, tp.symbol, COALESCE").
		WithArgs(startTime, now, "5m", "BTC/USDT", "binance").
		WillReturnRows(ohlcvRows)

	tradeColumns := []string{"id", "user_id", "quest_id", "strategy_id", "exchange", "symbol", "side",
		"entry_price", "exit_price", "size", "fees", "pnl", "cost_basis",
		"status", "opened_at", "closed_at", "created_at", "updated_at"}

	openRow := pgxmock.NewRows(tradeColumns).
		AddRow(int64(1), "backfill_user", nil, "scalping", "binance", "BTC/USDT", "buy",
			decimal.NewFromFloat(50200), decimal.Zero, decimal.NewFromFloat(0.01), decimal.Zero,
			decimal.Zero, decimal.NewFromFloat(502.0),
			"open", candleTime, nil, candleTime, candleTime)
	mockDB.ExpectQuery("INSERT INTO paper_trades").
		WillReturnRows(openRow)

	closeRow := pgxmock.NewRows(tradeColumns).
		AddRow(int64(1), "backfill_user", nil, "scalping", "binance", "BTC/USDT", "buy",
			decimal.NewFromFloat(50200), decimal.NewFromFloat(50500), decimal.NewFromFloat(0.01),
			decimal.Zero, decimal.NewFromFloat(3.0), decimal.NewFromFloat(502.0),
			"closed", candleTime, candleTime.Add(15*time.Minute), candleTime, candleTime)
	mockDB.ExpectQuery("UPDATE paper_trades").
		WillReturnRows(closeRow)

	fetchRow := pgxmock.NewRows(tradeColumns).
		AddRow(int64(1), "backfill_user", nil, "scalping", "binance", "BTC/USDT", "buy",
			decimal.NewFromFloat(50200), decimal.Zero, decimal.NewFromFloat(0.01), decimal.Zero,
			decimal.Zero, decimal.NewFromFloat(502.0),
			"open", candleTime, nil, candleTime, candleTime)
	mockDB.ExpectQuery("SELECT id, user_id").
		WillReturnRows(fetchRow)

	result, runErr := v.Run(context.Background())
	require.NoError(t, runErr)
	require.NotNil(t, result)

	assert.NotEmpty(t, result.RunID)
	assert.GreaterOrEqual(t, result.ContinuousValidationHours, 0.0)
	assert.NotNil(t, result.EvidenceArtifact)
	assert.Greater(t, result.CandlesProcessed, int64(0))
}

func TestPaperTradingBackfillValidation_StrategyCoversSymbol(t *testing.T) {
	v := &PaperTradingBackfillValidation{}

	strat := &PaperTradingStrategy{
		Symbols: []string{"BTC/USDT", "ETH/USDT"},
	}

	assert.True(t, v.strategyCoversSymbol(strat, "BTC/USDT"))
	assert.True(t, v.strategyCoversSymbol(strat, "ETH/USDT"))
	assert.False(t, v.strategyCoversSymbol(strat, "SOL/USDT"))
	assert.True(t, v.strategyCoversSymbol(strat, "btc/usdt"))
}

func TestPaperTradingBackfillValidation_CalculateEntryExitPrice(t *testing.T) {
	v := &PaperTradingBackfillValidation{
		config: PaperTradingBackfillConfig{
			ExecutionConfig: DefaultPaperExecutionConfig(),
		},
	}

	candle := backfillCandle{
		Close: decimal.NewFromInt(50000),
	}

	entryBuy := v.calculateEntryPrice(candle, PaperOrderSideBuy)
	assert.True(t, entryBuy.GreaterThan(candle.Close))

	entrySell := v.calculateEntryPrice(candle, PaperOrderSideSell)
	assert.True(t, entrySell.LessThan(candle.Close))

	exitBuy := v.calculateExitPrice(candle, PaperOrderSideBuy)
	assert.True(t, exitBuy.LessThan(candle.Close))

	exitSell := v.calculateExitPrice(candle, PaperOrderSideSell)
	assert.True(t, exitSell.GreaterThan(candle.Close))
}

func TestPaperTradingBackfillValidation_CollectRiskEvents(t *testing.T) {
	v := &PaperTradingBackfillValidation{
		config: PaperTradingBackfillConfig{
			InitialCapital: decimal.NewFromInt(10000),
			Strategies: []PaperTradingStrategy{
				{ID: "test"},
				{ID: "test2"},
			},
		},
	}

	result := &PaperTradingBackfillResult{
		RiskEvents: make([]PaperTradingRiskEvent, 0),
	}

	trades := []decimal.Decimal{
		decimal.NewFromFloat(100),
		decimal.NewFromFloat(-5000),
		decimal.NewFromFloat(-3000),
		decimal.NewFromFloat(-2000),
		decimal.NewFromFloat(200),
	}

	v.collectRiskEvents(result, &trades)
	assert.NotEmpty(t, result.RiskEvents)
	assert.GreaterOrEqual(t, len(result.RiskEvents), 1)
}

func TestPaperTradingBackfillValidation_EvidenceArtifact_JSON(t *testing.T) {
	evidence := &PaperTradingValidationEvidence{
		GeneratedAt:          time.Now(),
		RunID:                "test-run-123",
		StartTime:            time.Now().Add(-7 * 24 * time.Hour),
		EndTime:              time.Now(),
		ContinuousHours:      168,
		StrategiesCovered:    []string{"scalping", "daily"},
		TotalTrades:          100,
		ClosedTrades:         80,
		NetPnL:               decimal.NewFromFloat(250.75),
		WinRate:              decimal.NewFromFloat(55.5),
		MaxDrawdownPct:       decimal.NewFromFloat(12.3),
		AllBlockersSatisfied: true,
		NonDiagnostic:        true,
		ArtifactDigest:       "backfill-test-r",
	}

	jsonData, err := json.MarshalIndent(evidence, "", "  ")
	require.NoError(t, err)
	assert.Contains(t, string(jsonData), "test-run-123")
	assert.Contains(t, string(jsonData), "168")
	assert.Contains(t, string(jsonData), "scalping")

	var parsed PaperTradingValidationEvidence
	err = json.Unmarshal(jsonData, &parsed)
	require.NoError(t, err)
	assert.Equal(t, evidence.RunID, parsed.RunID)
	assert.True(t, parsed.AllBlockersSatisfied)
}
