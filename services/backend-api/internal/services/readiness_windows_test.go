package services

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/irfndi/neuratrade/internal/database"
	"github.com/shopspring/decimal"
)

// TestRealMoney_ReadinessBacktests runs the post-fix backtest engine
// across the windows the readiness assessment used so we can compare
// against the pre-fix numbers in READINESS_ASSESSMENT_2026_06_08.md:
//
//   30d:   808 trades, 46.29% win, +138.34 USDT
//   90d:     8 trades,  0%     win,   -8.73 USDT
//   180d:    9 trades, 33.33%  win,   -1.03 USDT
//   5yr:   16 trades,  0%     win,  -15.08 USDT
//
// Run with: go test -v -count=1 -run TestRealMoney_ReadinessBacktests ./internal/services/
func TestRealMoney_ReadinessBacktests(t *testing.T) {
	if os.Getenv("RUN_READINESS") != "1" {
		t.Skip("set RUN_READINESS=1 to run multi-window readiness backtests (~minutes)")
	}

	home := os.Getenv("NEURATRADE_HOME")
	if home == "" {
		home = os.ExpandEnv("$HOME/.neuratrade")
	}
	db, err := database.NewSQLiteConnection(home + "/data/neuratrade.db")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()
	pool := readOnlyDBPoolAdapter{pool: db}

	windows := []struct {
		name      string
		start     time.Time
		end       time.Time
		expectMin int
	}{
		{"30d", time.Date(2026, 4, 15, 0, 0, 0, 0, time.UTC), time.Date(2026, 5, 15, 0, 0, 0, 0, time.UTC), 100},
		{"90d", time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC), time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC), 5},
		{"180d", time.Date(2025, 12, 1, 0, 0, 0, 0, time.UTC), time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC), 5},
		{"1yr", time.Date(2025, 6, 10, 0, 0, 0, 0, time.UTC), time.Date(2026, 6, 10, 0, 0, 0, 0, time.UTC), 5},
	}

	type result struct {
		window   string
		signals  int
		trades   int
		wins     int
		losses   int
		winRate  decimal.Decimal
		pnl      decimal.Decimal
		pnlPct   decimal.Decimal
		maxDDPct decimal.Decimal
		elapsed  time.Duration
	}
	results := make([]result, 0, len(windows))

	for _, w := range windows {
		t.Run(w.name, func(t *testing.T) {
			cfg := ScalpingBacktestConfig{
				StartTime:          w.start,
				EndTime:            w.end,
				Exchange:           "binance",
				InitialCapital:     decimal.NewFromInt(10000),
				MaxBidAskSpreadPct: 0.22,
				MinConfidence:      0.6,
				SpreadMultiplier:   1.5,
				FeeRate:            decimal.NewFromFloat(0.0002),
				Mode:               "deterministic",
			}
			engine := NewScalpingBacktestEngine(pool, cfg)
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
			defer cancel()
			start := time.Now()
			res, err := engine.Run(ctx)
			elapsed := time.Since(start)
			if err != nil {
				t.Fatalf("engine.Run error: %v", err)
			}
			summary, _ := json.Marshal(res.Summary)
			fmt.Printf("\n=== %s backtest (%s) ===\n%s\n", w.name, elapsed.Round(time.Millisecond), string(summary))
			results = append(results, result{
				window:   w.name,
				signals:  res.Summary.TotalSignals,
				trades:   res.Summary.TotalTrades,
				wins:     res.Summary.WinningTrades,
				losses:   res.Summary.LosingTrades,
				winRate:  res.Summary.WinRate,
				pnl:      res.Summary.TotalPnL,
				pnlPct:   res.Summary.TotalReturnPct,
				maxDDPct: res.Summary.MaxDrawdownPct,
				elapsed:  elapsed,
			})
		})
	}

	// Final table
	fmt.Println("\n\n=== READINESS COMPARISON ===")
	fmt.Printf("%-8s %10s %8s %8s %10s %12s %12s %10s %s\n",
		"Window", "Signals", "Trades", "WinRate", "PnL(USDT)", "PnL%", "MaxDD%", "ExpectTr", "Elapsed")
	for i, r := range results {
		expect := windows[i].expectMin
		mark := "OK"
		if r.trades < expect {
			mark = fmt.Sprintf("LOW(<%d)", expect)
		}
		fmt.Printf("%-8s %10d %8d %8s%% %12s %12s %10s %-9s %s\n",
			r.window, r.signals, r.trades, r.winRate.String(), r.pnl.String(), r.pnlPct.String(), r.maxDDPct.String(), mark, r.elapsed.Round(time.Millisecond))
	}
}
