package main

import (
	"testing"

	"github.com/irfndi/neuratrade/internal/services"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
)

func TestValidateAcceptanceGates(t *testing.T) {
	result := &services.ScalpingLivePaperSoakResult{
		Report: services.ScalpingSoakReport{
			TradeSummary: services.ScalpingSoakTradeSummary{
				ClosedTrades:       12,
				WinRate:            decimal.NewFromFloat(0.75),
				NetPnL:             decimal.NewFromFloat(0.12),
				AvgNetPnLPerTrade:  decimal.NewFromFloat(0.01),
				MaxDrawdown:        decimal.Zero,
				MaxDrawdownPct:     decimal.Zero,
				ProfitFactor:       decimal.NewFromInt(2),
				AvgHoldDurationSec: decimal.NewFromInt(300),
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
				MinTrades:    10,
				MinWinRate:   "0.7",
				MinNetPnL:    "0",
				MinAvgNetPnL: "0.005",
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

func TestValidateAcceptanceGatesRequiresResult(t *testing.T) {
	err := validateAcceptanceGates(nil, acceptanceGateOptions{MinTrades: 1})
	require.ErrorContains(t, err, "acceptance gates require soak result")
}
