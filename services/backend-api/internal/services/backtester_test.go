package services

import (
	"testing"
	"time"

	"github.com/irfandi/celebrum-ai-go/internal/models"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewBacktester(t *testing.T) {
	backtester := NewBacktester(nil)

	require.NotNil(t, backtester)
	assert.NotNil(t, backtester.calculator)
	assert.Nil(t, backtester.db) // We're passing nil for db in this test
}

func TestDefaultBacktestConfig(t *testing.T) {
	config := DefaultBacktestConfig()

	assert.False(t, config.StartDate.IsZero())
	assert.False(t, config.EndDate.IsZero())
	assert.True(t, config.StartDate.Before(config.EndDate))
	assert.Equal(t, decimal.NewFromInt(10000), config.InitialCapital)
	assert.Equal(t, decimal.NewFromInt(5), config.MinAPY)
	assert.Equal(t, decimal.NewFromInt(70), config.MaxRiskScore)
	assert.Equal(t, "kelly", config.PositionSizing)
	assert.Equal(t, 5, config.MaxPositions)
	assert.Equal(t, decimal.NewFromFloat(0.001), config.TradingFee)
	assert.Equal(t, decimal.NewFromFloat(0.001), config.Slippage)
	assert.Equal(t, 8*time.Hour, config.HoldingPeriod)
	assert.True(t, config.ReinvestProfits)
}

func TestValidateConfig(t *testing.T) {
	backtester := NewBacktester(nil)

	tests := []struct {
		name    string
		config  BacktestConfig
		wantErr bool
		errMsg  string
	}{
		{
			name:    "empty config",
			config:  BacktestConfig{},
			wantErr: true,
			errMsg:  "start_date is required",
		},
		{
			name: "missing end date",
			config: BacktestConfig{
				StartDate: time.Now().Add(-24 * time.Hour),
			},
			wantErr: true,
			errMsg:  "end_date is required",
		},
		{
			name: "start after end",
			config: BacktestConfig{
				StartDate: time.Now(),
				EndDate:   time.Now().Add(-24 * time.Hour),
			},
			wantErr: true,
			errMsg:  "start_date must be before end_date",
		},
		{
			name: "zero initial capital",
			config: BacktestConfig{
				StartDate:      time.Now().Add(-24 * time.Hour),
				EndDate:        time.Now(),
				InitialCapital: decimal.Zero,
			},
			wantErr: true,
			errMsg:  "initial_capital must be positive",
		},
		{
			name: "negative initial capital",
			config: BacktestConfig{
				StartDate:      time.Now().Add(-24 * time.Hour),
				EndDate:        time.Now(),
				InitialCapital: decimal.NewFromInt(-1000),
			},
			wantErr: true,
			errMsg:  "initial_capital must be positive",
		},
		{
			name: "valid config",
			config: BacktestConfig{
				StartDate:      time.Now().Add(-24 * time.Hour),
				EndDate:        time.Now(),
				InitialCapital: decimal.NewFromInt(10000),
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := backtester.validateConfig(tt.config)
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

func TestCalculateMaxDrawdown(t *testing.T) {
	backtester := NewBacktester(nil)

	tests := []struct {
		name        string
		equityCurve []EquityPoint
		expectedDD  decimal.Decimal
		tolerance   float64
	}{
		{
			name:        "empty curve",
			equityCurve: []EquityPoint{},
			expectedDD:  decimal.Zero,
		},
		{
			name: "single point",
			equityCurve: []EquityPoint{
				{Timestamp: time.Now(), Equity: decimal.NewFromInt(10000)},
			},
			expectedDD: decimal.Zero,
		},
		{
			name: "no drawdown - monotonic increase",
			equityCurve: []EquityPoint{
				{Timestamp: time.Now(), Equity: decimal.NewFromInt(10000)},
				{Timestamp: time.Now().Add(time.Hour), Equity: decimal.NewFromInt(11000)},
				{Timestamp: time.Now().Add(2 * time.Hour), Equity: decimal.NewFromInt(12000)},
			},
			expectedDD: decimal.Zero,
		},
		{
			name: "10% drawdown",
			equityCurve: []EquityPoint{
				{Timestamp: time.Now(), Equity: decimal.NewFromInt(10000)},
				{Timestamp: time.Now().Add(time.Hour), Equity: decimal.NewFromInt(11000)},
				{Timestamp: time.Now().Add(2 * time.Hour), Equity: decimal.NewFromInt(9900)}, // 10% from peak
				{Timestamp: time.Now().Add(3 * time.Hour), Equity: decimal.NewFromInt(10500)},
			},
			expectedDD: decimal.NewFromFloat(10),
			tolerance:  1.0,
		},
		{
			name: "20% drawdown",
			equityCurve: []EquityPoint{
				{Timestamp: time.Now(), Equity: decimal.NewFromInt(10000)},
				{Timestamp: time.Now().Add(time.Hour), Equity: decimal.NewFromInt(12000)},    // Peak
				{Timestamp: time.Now().Add(2 * time.Hour), Equity: decimal.NewFromInt(9600)}, // 20% from peak
				{Timestamp: time.Now().Add(3 * time.Hour), Equity: decimal.NewFromInt(11000)},
			},
			expectedDD: decimal.NewFromFloat(20),
			tolerance:  1.0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dd, _ := backtester.calculateMaxDrawdown(tt.equityCurve)

			if tt.expectedDD.IsZero() {
				assert.True(t, dd.IsZero())
			} else {
				diff := dd.Sub(tt.expectedDD).Abs()
				tolerance := decimal.NewFromFloat(tt.tolerance)
				assert.True(t, diff.LessThanOrEqual(tolerance),
					"expected drawdown ~%s, got %s", tt.expectedDD.String(), dd.String())
			}
		})
	}
}

func TestCalculateSharpeRatio(t *testing.T) {
	backtester := NewBacktester(nil)

	tests := []struct {
		name         string
		returns      []float64
		expectedSign int // 1 for positive, -1 for negative, 0 for zero
	}{
		{
			name:         "empty returns",
			returns:      []float64{},
			expectedSign: 0,
		},
		{
			name:         "single return",
			returns:      []float64{0.05},
			expectedSign: 0,
		},
		{
			name:         "positive consistent returns",
			returns:      []float64{0.01, 0.02, 0.01, 0.015, 0.01, 0.02, 0.01},
			expectedSign: 1,
		},
		{
			name:         "negative consistent returns",
			returns:      []float64{-0.01, -0.02, -0.01, -0.015, -0.01, -0.02, -0.01},
			expectedSign: -1,
		},
		{
			name:         "mixed returns",
			returns:      []float64{0.05, -0.02, 0.03, -0.01, 0.04, -0.01, 0.02},
			expectedSign: 1, // Net positive
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sharpe := backtester.calculateSharpeRatio(tt.returns)

			switch tt.expectedSign {
			case 0:
				assert.True(t, sharpe.IsZero() || sharpe.Abs().LessThan(decimal.NewFromFloat(0.01)),
					"expected ~0, got %s", sharpe.String())
			case 1:
				assert.True(t, sharpe.IsPositive(),
					"expected positive sharpe, got %s", sharpe.String())
			case -1:
				assert.True(t, sharpe.IsNegative(),
					"expected negative sharpe, got %s", sharpe.String())
			}
		})
	}
}

func TestCalculateSortinoRatio(t *testing.T) {
	backtester := NewBacktester(nil)

	tests := []struct {
		name         string
		returns      []float64
		expectedSign int // 1 for positive, -1 for negative
	}{
		{
			name:         "all positive returns - high sortino",
			returns:      []float64{0.01, 0.02, 0.03, 0.02, 0.01, 0.02, 0.01},
			expectedSign: 1, // Will be high/infinite since no downside
		},
		{
			name:         "mixed with some negative",
			returns:      []float64{0.05, -0.01, 0.03, -0.02, 0.04, 0.02, -0.01},
			expectedSign: 1, // Net positive with limited downside
		},
		{
			name:         "mostly negative",
			returns:      []float64{-0.05, 0.01, -0.03, 0.02, -0.04, -0.02, 0.01},
			expectedSign: -1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sortino := backtester.calculateSortinoRatio(tt.returns)

			if tt.expectedSign == 1 {
				assert.True(t, sortino.IsPositive() || sortino.Equal(decimal.NewFromInt(999)),
					"expected positive sortino, got %s", sortino.String())
			} else {
				assert.True(t, sortino.IsNegative(),
					"expected negative sortino, got %s", sortino.String())
			}
		})
	}
}

func TestCalculatePositionSize(t *testing.T) {
	backtester := NewBacktester(nil)

	equity := decimal.NewFromInt(10000)

	tests := []struct {
		name           string
		config         BacktestConfig
		apy            decimal.Decimal
		riskScore      decimal.Decimal
		expectedMinPct float64
		expectedMaxPct float64
	}{
		{
			name: "fixed sizing",
			config: BacktestConfig{
				PositionSizing: "fixed",
				FixedSize:      decimal.NewFromInt(1000),
			},
			apy:            decimal.NewFromInt(20),
			riskScore:      decimal.NewFromInt(30),
			expectedMinPct: 10,
			expectedMaxPct: 10,
		},
		{
			name: "risk adjusted - low risk",
			config: BacktestConfig{
				PositionSizing: "risk_adjusted",
			},
			apy:            decimal.NewFromInt(20),
			riskScore:      decimal.NewFromInt(20), // Low risk
			expectedMinPct: 10,
			expectedMaxPct: 20,
		},
		{
			name: "risk adjusted - high risk",
			config: BacktestConfig{
				PositionSizing: "risk_adjusted",
			},
			apy:            decimal.NewFromInt(20),
			riskScore:      decimal.NewFromInt(80), // High risk
			expectedMinPct: 1,
			expectedMaxPct: 10,
		},
		{
			name: "kelly sizing",
			config: BacktestConfig{
				PositionSizing: "kelly",
			},
			apy:            decimal.NewFromInt(30),
			riskScore:      decimal.NewFromInt(30),
			expectedMinPct: 5,
			expectedMaxPct: 25,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create mock opportunity using models.FuturesArbitrageOpportunity
			opp := models.FuturesArbitrageOpportunity{
				Symbol:                  "BTC/USDT",
				APY:                     tt.apy,
				RiskScore:               tt.riskScore,
				RecommendedPositionSize: decimal.Zero,
			}

			positionSize := backtester.calculatePositionSize(tt.config, equity, opp)

			positionPct := positionSize.Div(equity).Mul(decimal.NewFromInt(100))

			t.Logf("Position size: %s, Percentage: %s%%",
				positionSize.String(), positionPct.StringFixed(2))

			assert.True(t, positionPct.GreaterThanOrEqual(decimal.NewFromFloat(tt.expectedMinPct)),
				"position should be at least %f%%, got %s%%", tt.expectedMinPct, positionPct.String())
			assert.True(t, positionPct.LessThanOrEqual(decimal.NewFromFloat(tt.expectedMaxPct)),
				"position should be at most %f%%, got %s%%", tt.expectedMaxPct, positionPct.String())
		})
	}
}

func TestCloseTrade(t *testing.T) {
	backtester := NewBacktester(nil)

	config := BacktestConfig{
		TradingFee: decimal.NewFromFloat(0.001),
		Slippage:   decimal.NewFromFloat(0.001),
		FundingFee: decimal.NewFromFloat(0.1),
	}

	// Use fixed times to avoid nanosecond precision issues
	exitTime := time.Date(2025, 1, 15, 16, 0, 0, 0, time.UTC)
	entryTime := exitTime.Add(-8 * time.Hour)

	trade := &BacktestTrade{
		Symbol:         "BTC/USDT",
		LongExchange:   "binance",
		ShortExchange:  "okx",
		EntryTime:      entryTime,
		PositionSize:   decimal.NewFromInt(10000),
		EntryAPY:       decimal.NewFromInt(50), // 50% APY
		EntryRiskScore: decimal.NewFromInt(30),
		TradingFees:    decimal.NewFromInt(20), // Pre-calculated entry fees
		Slippage:       decimal.NewFromInt(20), // Pre-calculated slippage
	}

	closedTrade := backtester.closeTrade(trade, exitTime, config)

	assert.Equal(t, exitTime, closedTrade.ExitTime)
	assert.Equal(t, 8*time.Hour, closedTrade.HoldingTime)
	assert.True(t, closedTrade.FundingReceived.IsPositive())
	assert.True(t, closedTrade.GrossPnL.IsPositive())

	// Net PnL should be less than gross due to fees and slippage
	assert.True(t, closedTrade.NetPnL.LessThan(closedTrade.GrossPnL))

	t.Logf("Funding received: %s, Gross PnL: %s, Net PnL: %s",
		closedTrade.FundingReceived.String(),
		closedTrade.GrossPnL.String(),
		closedTrade.NetPnL.String())
}

func TestGenerateDailyReturns(t *testing.T) {
	backtester := NewBacktester(nil)

	config := BacktestConfig{
		InitialCapital: decimal.NewFromInt(10000),
	}

	now := time.Now()
	trades := []BacktestTrade{
		{
			ExitTime: now.Add(-48 * time.Hour),
			NetPnL:   decimal.NewFromInt(100),
		},
		{
			ExitTime: now.Add(-48 * time.Hour),
			NetPnL:   decimal.NewFromInt(50),
		},
		{
			ExitTime: now.Add(-24 * time.Hour),
			NetPnL:   decimal.NewFromInt(-30),
		},
		{
			ExitTime: now,
			NetPnL:   decimal.NewFromInt(200),
		},
	}

	dailyReturns := backtester.generateDailyReturns(trades, config)

	assert.Len(t, dailyReturns, 3) // 3 different days

	// Check first day has combined returns from 2 trades
	assert.Equal(t, 2, dailyReturns[0].TradeCount)
	assert.Equal(t, decimal.NewFromInt(150), dailyReturns[0].PnL)

	// Check equity progression
	expectedEquity := decimal.NewFromInt(10000).Add(decimal.NewFromInt(150))
	assert.True(t, dailyReturns[0].Equity.Equal(expectedEquity))
}

func TestBacktestResultCalculations(t *testing.T) {
	backtester := NewBacktester(nil)

	config := DefaultBacktestConfig()

	// Create mock trades
	trades := []BacktestTrade{
		{
			Symbol:        "BTC/USDT",
			LongExchange:  "binance",
			ShortExchange: "okx",
			EntryTime:     time.Now().Add(-48 * time.Hour),
			ExitTime:      time.Now().Add(-40 * time.Hour),
			PositionSize:  decimal.NewFromInt(1000),
			NetPnL:        decimal.NewFromInt(50), // Win
			ReturnPct:     decimal.NewFromFloat(5),
			HoldingTime:   8 * time.Hour,
		},
		{
			Symbol:        "ETH/USDT",
			LongExchange:  "binance",
			ShortExchange: "bybit",
			EntryTime:     time.Now().Add(-36 * time.Hour),
			ExitTime:      time.Now().Add(-28 * time.Hour),
			PositionSize:  decimal.NewFromInt(1000),
			NetPnL:        decimal.NewFromInt(-20), // Loss
			ReturnPct:     decimal.NewFromFloat(-2),
			HoldingTime:   8 * time.Hour,
		},
		{
			Symbol:        "SOL/USDT",
			LongExchange:  "okx",
			ShortExchange: "binance",
			EntryTime:     time.Now().Add(-24 * time.Hour),
			ExitTime:      time.Now().Add(-16 * time.Hour),
			PositionSize:  decimal.NewFromInt(1000),
			NetPnL:        decimal.NewFromInt(100), // Win
			ReturnPct:     decimal.NewFromFloat(10),
			HoldingTime:   8 * time.Hour,
		},
	}

	equityCurve := []EquityPoint{
		{Timestamp: time.Now().Add(-48 * time.Hour), Equity: decimal.NewFromInt(10000)},
		{Timestamp: time.Now().Add(-40 * time.Hour), Equity: decimal.NewFromInt(10050)},
		{Timestamp: time.Now().Add(-28 * time.Hour), Equity: decimal.NewFromInt(10030)},
		{Timestamp: time.Now().Add(-16 * time.Hour), Equity: decimal.NewFromInt(10130)},
	}

	result := &BacktestResult{
		Config:           config,
		DailyReturns:     make([]DailyReturn, 0),
		EquityCurve:      make([]EquityPoint, 0),
		TradesBySymbol:   make(map[string]int),
		TradesByExchange: make(map[string]int),
	}

	backtester.calculateResults(result, trades, equityCurve, config)

	// Verify basic counts
	assert.Equal(t, 3, result.TotalTrades)
	assert.Equal(t, 2, result.WinningTrades)
	assert.Equal(t, 1, result.LosingTrades)

	// Verify win rate (~66.67%)
	expectedWinRate := decimal.NewFromFloat(66.67)
	assert.True(t, result.WinRate.GreaterThan(decimal.NewFromFloat(60)))
	assert.True(t, result.WinRate.LessThan(decimal.NewFromFloat(70)))
	t.Logf("Win rate: %s%% (expected ~%s%%)", result.WinRate.String(), expectedWinRate.String())

	// Verify total PnL
	expectedPnL := decimal.NewFromInt(130) // 50 - 20 + 100
	assert.True(t, result.TotalPnL.Equal(expectedPnL))

	// Verify profit factor (gross profit / gross loss)
	// Gross profit: 50 + 100 = 150
	// Gross loss: 20
	// Profit factor: 150 / 20 = 7.5
	assert.True(t, result.ProfitFactor.GreaterThan(decimal.NewFromInt(5)))

	// Verify trades by symbol tracking
	assert.Equal(t, 1, result.TradesBySymbol["BTC/USDT"])
	assert.Equal(t, 1, result.TradesBySymbol["ETH/USDT"])
	assert.Equal(t, 1, result.TradesBySymbol["SOL/USDT"])

	// Verify best/worst trades
	assert.NotNil(t, result.BestTrade)
	assert.NotNil(t, result.WorstTrade)
	assert.Equal(t, "SOL/USDT", result.BestTrade.Symbol)
	assert.Equal(t, "ETH/USDT", result.WorstTrade.Symbol)
}
