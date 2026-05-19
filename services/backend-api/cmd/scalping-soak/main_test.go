package main

import (
	"os"
	"testing"

	"github.com/irfndi/neuratrade/internal/services"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
)

func TestValidateAcceptanceGates(t *testing.T) {
	result := &services.ScalpingLivePaperSoakResult{
		Report: services.ScalpingSoakReport{
			SignalQuality: services.ScalpingSignalQualitySoakStats{
				Coverage: decimal.NewFromFloat(0.95),
			},
			TradeSummary: services.ScalpingSoakTradeSummary{
				ClosedTrades:       12,
				Wins:               9,
				Losses:             3,
				WinRate:            decimal.NewFromFloat(0.75),
				NetPnL:             decimal.NewFromFloat(0.12),
				AvgNetPnLPerTrade:  decimal.NewFromFloat(0.01),
				MaxDrawdown:        decimal.NewFromFloat(0.03),
				MaxDrawdownPct:     decimal.NewFromFloat(0.000625),
				ProfitFactor:       decimal.NewFromInt(2),
				AvgHoldDurationSec: decimal.NewFromInt(300),
			},
			AIProviderDegradation: services.ScalpingAIDegradationSoakStats{
				DegradedCycles: 1,
			},
			ActionSplit: map[string]decimal.Decimal{
				"hold": decimal.NewFromFloat(0.5),
			},
			BaselineComparison: &services.ScalpingSoakBaselineComparison{
				BaselineName:        "test",
				DeltaWinRate:        decimal.NewFromFloat(0.627),
				DeltaNetPnL:         decimal.NewFromFloat(0.30),
				DeltaAvgPnLPerTrade: decimal.NewFromFloat(0.013),
			},
			LiveTrialReadiness: services.ScalpingLiveTrialReadiness{
				Ready:           true,
				MinClosedTrades: services.DefaultScalpingLiveTrialMinClosedTrades,
			},
		},
	}

	tests := []struct {
		name    string
		options acceptanceGateOptions
		wantErr string
	}{
		{
			name: "passes all configured gates",
			options: acceptanceGateOptions{
				MinTrades:                10,
				MinWinRate:               "0.7",
				MinNetPnL:                "0",
				MinAvgNetPnL:             "0.005",
				MinSignalQualityCoverage: "0.9",
				MaxHoldRatio:             "0.745",
				MaxDrawdown:              "0.05",
				MaxDrawdownPct:           "0.001",
				MaxAIDegradedCycles:      "1",
				MaxPerfectWinTrades:      "20",
				MinBaselineWinRateDelta:  "0.6",
				MinBaselineNetPnLDelta:   "0.2",
				MinBaselineAvgPnLDelta:   "0.01",
				RequireLiveTrialReady:    true,
			},
		},
		{
			name:    "fails trade count",
			options: acceptanceGateOptions{MinTrades: 13},
			wantErr: "closed_trades=12 below min_trades=13",
		},
		{
			name:    "fails win rate",
			options: acceptanceGateOptions{MinWinRate: "0.8"},
			wantErr: "win_rate=0.75 below minimum=0.8",
		},
		{
			name:    "fails net pnl",
			options: acceptanceGateOptions{MinNetPnL: "0.2"},
			wantErr: "net_pnl=0.12 below minimum=0.2",
		},
		{
			name:    "fails avg net pnl",
			options: acceptanceGateOptions{MinAvgNetPnL: "0.02"},
			wantErr: "avg_net_pnl_per_trade=0.01 below minimum=0.02",
		},
		{
			name:    "invalid decimal threshold",
			options: acceptanceGateOptions{MinWinRate: "not-a-decimal"},
			wantErr: "parse --min-win-rate",
		},
		{
			name:    "fails signal quality coverage",
			options: acceptanceGateOptions{MinSignalQualityCoverage: "1"},
			wantErr: "signal_quality.coverage=0.95 below minimum=1",
		},
		{
			name:    "fails max hold ratio",
			options: acceptanceGateOptions{MaxHoldRatio: "0.49"},
			wantErr: `action_split.hold="0.5" above maximum="0.49"`,
		},
		{
			name:    "fails max drawdown",
			options: acceptanceGateOptions{MaxDrawdown: "0.02"},
			wantErr: `max_drawdown="0.03" above maximum="0.02"`,
		},
		{
			name:    "fails max drawdown pct",
			options: acceptanceGateOptions{MaxDrawdownPct: "0.0005"},
			wantErr: `max_drawdown_pct="0.000625" above maximum="0.0005"`,
		},
		{
			name:    "fails AI provider degraded cycles",
			options: acceptanceGateOptions{MaxAIDegradedCycles: "0"},
			wantErr: `ai_provider_degraded_cycles="1" above maximum="0"`,
		},
		{
			name:    "rejects invalid AI provider degraded cycle threshold",
			options: acceptanceGateOptions{MaxAIDegradedCycles: "not-an-int"},
			wantErr: "parse --max-ai-provider-degraded-cycles",
		},
		{
			name:    "rejects negative AI provider degraded cycle threshold",
			options: acceptanceGateOptions{MaxAIDegradedCycles: "-1"},
			wantErr: `invalid --max-ai-provider-degraded-cycles value "-1": must be zero or greater`,
		},
		{
			name:    "rejects invalid perfect win threshold",
			options: acceptanceGateOptions{MaxPerfectWinTrades: "not-an-int"},
			wantErr: "parse --max-perfect-win-trades",
		},
		{
			name:    "rejects negative perfect win threshold",
			options: acceptanceGateOptions{MaxPerfectWinTrades: "-1"},
			wantErr: `invalid --max-perfect-win-trades value "-1": must be zero or greater`,
		},
		{
			name:    "fails baseline win rate delta",
			options: acceptanceGateOptions{MinBaselineWinRateDelta: "0.7"},
			wantErr: "baseline.delta_win_rate=0.627 below minimum=0.7",
		},
		{
			name:    "fails baseline net pnl delta",
			options: acceptanceGateOptions{MinBaselineNetPnLDelta: "0.4"},
			wantErr: "baseline.delta_net_pnl=0.3 below minimum=0.4",
		},
		{
			name:    "fails baseline avg pnl delta",
			options: acceptanceGateOptions{MinBaselineAvgPnLDelta: "0.02"},
			wantErr: "baseline.delta_avg_pnl_per_trade=0.013 below minimum=0.02",
		},
		{
			name:    "invalid max decimal threshold",
			options: acceptanceGateOptions{MaxHoldRatio: "not-a-decimal"},
			wantErr: "parse --max-hold-ratio",
		},
		{
			name:    "rejects negative max decimal threshold",
			options: acceptanceGateOptions{MaxHoldRatio: "-0.1"},
			wantErr: `invalid --max-hold-ratio value "-0.1": must be zero or greater`,
		},
		{
			name:    "rejects max hold ratio above one",
			options: acceptanceGateOptions{MaxHoldRatio: "74.5"},
			wantErr: `invalid --max-hold-ratio value "74.5": must be at most 1`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateAcceptanceGates(result, tt.options)
			if tt.wantErr == "" {
				require.NoError(t, err)
				return
			}
			require.ErrorContains(t, err, tt.wantErr)
		})
	}
}

func TestValidateAcceptanceGatesFailsImplausiblePerfectPaperProof(t *testing.T) {
	result := &services.ScalpingLivePaperSoakResult{
		Report: services.ScalpingSoakReport{
			TradeSummary: services.ScalpingSoakTradeSummary{
				ClosedTrades:   21,
				Wins:           21,
				Losses:         0,
				MaxDrawdown:    decimal.Zero,
				MaxDrawdownPct: decimal.Zero,
			},
		},
	}

	err := validateAcceptanceGates(result, acceptanceGateOptions{MaxPerfectWinTrades: "20"})
	require.ErrorContains(t, err, "paper realism gate failed")
	require.ErrorContains(t, err, "perfect paper wins without drawdown are insufficient proof")

	err = validateAcceptanceGates(result, acceptanceGateOptions{MaxPerfectWinTrades: ""})
	require.NoError(t, err)

	result.Report.TradeSummary.Losses = 1
	result.Report.TradeSummary.Wins = 20
	err = validateAcceptanceGates(result, acceptanceGateOptions{MaxPerfectWinTrades: "20"})
	require.NoError(t, err)
}

func TestValidateAcceptanceGatesRequiresResult(t *testing.T) {
	err := validateAcceptanceGates(nil, acceptanceGateOptions{MinTrades: 1})
	require.ErrorContains(t, err, "acceptance gates require soak result")
}

func TestValidateAcceptanceGatesTreatsMissingHoldSplitAsZeroForMaxHoldRatio(t *testing.T) {
	result := &services.ScalpingLivePaperSoakResult{
		Report: services.ScalpingSoakReport{
			ActionSplit: map[string]decimal.Decimal{
				"buy": decimal.NewFromInt(1),
			},
		},
	}

	err := validateAcceptanceGates(result, acceptanceGateOptions{MaxHoldRatio: "0.745"})
	require.NoError(t, err)
}

func TestValidateAcceptanceGatesCanRequireLiveTrialReadiness(t *testing.T) {
	result := &services.ScalpingLivePaperSoakResult{
		Report: services.ScalpingSoakReport{
			LiveTrialReadiness: services.ScalpingLiveTrialReadiness{
				Ready: false,
				Reasons: []string{
					"closed_trades_below_live_trial_minimum",
					"drawdown_not_observed",
				},
				MinClosedTrades: services.DefaultScalpingLiveTrialMinClosedTrades,
			},
		},
	}

	err := validateAcceptanceGates(result, acceptanceGateOptions{RequireLiveTrialReady: true})
	require.ErrorContains(t, err, "live_trial_readiness.ready=false")
	require.ErrorContains(t, err, "closed_trades_below_live_trial_minimum")
}

func TestValidateAcceptanceGatesRequiresBaselineForMaxDrawdownPct(t *testing.T) {
	result := &services.ScalpingLivePaperSoakResult{
		Report: services.ScalpingSoakReport{
			TradeSummary: services.ScalpingSoakTradeSummary{
				ClosedTrades:   1,
				MaxDrawdownPct: decimal.Zero,
			},
		},
	}

	err := validateAcceptanceGates(result, acceptanceGateOptions{MaxDrawdownPct: "0.01"})
	require.ErrorContains(t, err, "--max-drawdown-pct requires --baseline=true")
}

func TestValidateAcceptanceGatesRequiresBaselineForDeltaGates(t *testing.T) {
	result := &services.ScalpingLivePaperSoakResult{
		Report: services.ScalpingSoakReport{
			TradeSummary: services.ScalpingSoakTradeSummary{
				ClosedTrades: 1,
				NetPnL:       decimal.NewFromFloat(0.1),
			},
		},
	}

	err := validateAcceptanceGates(result, acceptanceGateOptions{MinBaselineNetPnLDelta: "0"})
	require.ErrorContains(t, err, "baseline delta gates require --baseline=true")
}

func TestRunRejectsNegativeHoldPeriod(t *testing.T) {
	t.Setenv("NEURATRADE_SCALPING_SOAK_CHAT_ID", "test")
	originalArgs := os.Args
	t.Cleanup(func() { os.Args = originalArgs })
	os.Args = []string{"scalping-soak", "--hold-period-seconds", "-1"}

	err := run()
	require.ErrorContains(t, err, "invalid --hold-period-seconds value -1")
}

func TestEnvInt(t *testing.T) {
	t.Setenv("SCALPING_SOAK_TEST_INT", "17")
	require.Equal(t, 17, envInt("SCALPING_SOAK_TEST_INT", 3))

	t.Setenv("SCALPING_SOAK_TEST_INT", "not-an-int")
	require.Equal(t, 3, envInt("SCALPING_SOAK_TEST_INT", 3))

	t.Setenv("SCALPING_SOAK_TEST_INT", "")
	require.Equal(t, 3, envInt("SCALPING_SOAK_TEST_INT", 3))
}
