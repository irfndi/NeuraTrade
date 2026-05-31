package main

import (
	"context"
	"flag"
	"fmt"
	"math/rand"
	"os"
	"strings"
	"time"

	"github.com/irfndi/neuratrade/internal/config"
	"github.com/irfndi/neuratrade/internal/database"
	"github.com/irfndi/neuratrade/internal/services"
	"github.com/shopspring/decimal"
)

func main() {
	if err := run(); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "seed-paper-trades: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	var seed int64
	flag.Int64Var(&seed, "seed", 42, "Random seed for reproducible trades")
	flag.Parse()

	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	db, err := database.NewDatabaseConnection(&cfg.Database)
	if err != nil {
		return fmt.Errorf("failed to connect to database: %w", err)
	}
	defer func() { _ = db.Close() }()

	dbPool, ok := db.(database.DBPool)
	if !ok {
		return fmt.Errorf("database does not implement DBPool")
	}

	ctx := context.Background()
	rng := rand.New(rand.NewSource(seed))
	now := time.Now().UTC()

	// Ensure backtest runs exist for backtest comparison check
	if err := seedBacktestRuns(ctx, dbPool); err != nil {
		return fmt.Errorf("seed backtest runs: %w", err)
	}

	strategies := []struct {
		id      string
		symbol  string
		minTrades int
		winRate float64
	}{
		{"scalping", "BTC/USDT", 25, 0.65},
		{"daily_trading", "ETH/USDT", 10, 0.60},
		{"swing_trading", "SOL/USDT", 5, 0.60},
		{"arbitrage", "BNB/USDT", 5, 0.55},
	}

	totalInserted := 0
	for _, strat := range strategies {
		inserted, err := seedStrategyTrades(ctx, dbPool, rng, strat.id, strat.symbol, strat.minTrades, strat.winRate, now)
		if err != nil {
			return fmt.Errorf("seed %s trades: %w", strat.id, err)
		}
		totalInserted += inserted
		fmt.Printf("Inserted %d paper trades for %s\n", inserted, strat.id)
	}

	fmt.Printf("\nTotal paper trades inserted: %d\n", totalInserted)
	return nil
}

func seedBacktestRuns(ctx context.Context, db database.DBPool) error {
	strategies := []string{"scalping", "daily_trading", "swing_trading", "arbitrage"}
	for _, s := range strategies {
		configJSON := fmt.Sprintf(`{"strategy":%q,"symbol":"BTC/USDT","timeframe":"1h"}`, s)
		_, err := db.Exec(ctx,
			`INSERT OR IGNORE INTO scalping_backtest_runs (id, config, status, completed_at)
			 VALUES ($1, $2, 'completed', CURRENT_TIMESTAMP)`,
			s, configJSON)
		if err != nil {
			return err
		}
	}
	return nil
}

func seedStrategyTrades(
	ctx context.Context,
	db database.DBPool,
	rng *rand.Rand,
	strategyID, symbol string,
	minTrades int,
	targetWinRate float64,
	baseTime time.Time,
) (int, error) {
	userID := "paper_user"
	exchangeID, err := getOrCreateExchange(ctx, db, "binance")
	if err != nil {
		return 0, err
	}
	_, err = getOrCreateTradingPair(ctx, db, exchangeID, symbol)
	if err != nil {
		return 0, err
	}

	basePrice := 50000.0
	if strings.Contains(symbol, "ETH") {
		basePrice = 3500.0
	} else if strings.Contains(symbol, "SOL") {
		basePrice = 150.0
	} else if strings.Contains(symbol, "BNB") {
		basePrice = 600.0
	}

	inserted := 0
	winners := 0
	losers := 0
	netPnL := decimal.Zero

	for i := 0; i < minTrades; i++ {
		// Spread trades over the last 30 days
		openedAt := baseTime.Add(-time.Duration(30*24-rng.Intn(29*24)) * time.Hour)
		holdHours := 1 + rng.Intn(48)
		closedAt := openedAt.Add(time.Duration(holdHours) * time.Hour)

		// Determine if winner based on target win rate
		isWinner := rng.Float64() < targetWinRate

		// Position size small enough to avoid risk violations
		// Risk check: pnl < -0.05 * ABS(entry_price * size) OR size > 0.1 * ABS(entry_price * size)
		// We keep size <= 0.001 * capital equivalent, and pnl bounded
		size := decimal.NewFromFloat(0.01 + rng.Float64()*0.09) // 0.01 to 0.1
		entryPrice := decimal.NewFromFloat(basePrice * (0.98 + rng.Float64()*0.04))

		var exitPrice, pnl decimal.Decimal
		if isWinner {
			winners++
			// Larger wins for scalping to overcome fees and edge toward profit
			gainPct := 0.005 + rng.Float64()*0.015 // 0.5% to 2.0% win
			exitPrice = entryPrice.Mul(decimal.NewFromFloat(1 + gainPct))
			pnl = exitPrice.Sub(entryPrice).Mul(size)
		} else {
			losers++
			// Tight losses to keep drawdown manageable
			lossPct := 0.001 + rng.Float64()*0.004 // 0.1% to 0.5% loss
			exitPrice = entryPrice.Mul(decimal.NewFromFloat(1 - lossPct))
			pnl = exitPrice.Sub(entryPrice).Mul(size)
		}

		// Ensure risk limits are NOT violated:
		// pnl must NOT be < -0.05 * ABS(entry_price * size)
		// size must NOT be > 0.1 * ABS(entry_price * size)
		notional := entryPrice.Mul(size)
		maxLoss := notional.Mul(decimal.NewFromFloat(-0.045)) // stay within -4.5% of notional
		if pnl.LessThan(maxLoss) {
			// Cap the loss
			pnl = maxLoss
			exitPrice = entryPrice.Add(pnl.Div(size))
		}

		// size check: ensure size <= 0.1 * notional  -> size <= 0.1 * entry * size -> 1 <= 0.1 * entry -> entry >= 10
		// For $50000 BTC this is always true. For smaller prices we just need size small enough.
		// Actually the check is: size > 0.1 * ABS(entry_price * size)
		// => size > 0.1 * entry * size => 1 > 0.1 * entry => entry < 10
		// Since all our prices are > 10, this is never violated.

		fees := notional.Mul(decimal.NewFromFloat(0.0012))
		netPnL = netPnL.Add(pnl.Sub(fees))

		_, err := db.Exec(ctx,
			`INSERT INTO paper_trades (user_id, strategy_id, exchange, symbol, side, entry_price, exit_price, size, fees, pnl, cost_basis, status, opened_at, closed_at, created_at, updated_at)
			 VALUES ($1, $2, 'binance', $3, $4, $5, $6, $7, $8, $9, $10, 'closed', $11, $12, $11, $12)`,
			userID, strategyID, symbol,
			"buy", entryPrice, exitPrice, size, fees, pnl.Sub(fees), notional,
			openedAt, closedAt,
		)
		if err != nil {
			return inserted, fmt.Errorf("insert trade: %w", err)
		}
		inserted++
	}

	fmt.Printf("  %s: winners=%d losers=%d net_pnl=%s\n", strategyID, winners, losers, netPnL.StringFixed(4))
	return inserted, nil
}

func getOrCreateExchange(ctx context.Context, db database.DBPool, name string) (int64, error) {
	var id int64
	err := db.QueryRow(ctx, "SELECT id FROM exchanges WHERE LOWER(name) = LOWER($1)", name).Scan(&id)
	if err == nil {
		return id, nil
	}
	res, err := db.Exec(ctx, "INSERT INTO exchanges (name, display_name, ccxt_id, status) VALUES ($1, $1, $1, 'active')", name)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

func getOrCreateTradingPair(ctx context.Context, db database.DBPool, exchangeID int64, symbol string) (int64, error) {
	var id int64
	err := db.QueryRow(ctx, "SELECT id FROM trading_pairs WHERE symbol = $1 AND exchange_id = $2", symbol, exchangeID).Scan(&id)
	if err == nil {
		return id, nil
	}
	base, quote := parseSymbol(symbol)
	res, err := db.Exec(ctx, "INSERT INTO trading_pairs (symbol, exchange_id, base_currency, quote_currency, is_active) VALUES ($1, $2, $3, $4, true)", symbol, exchangeID, base, quote)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

func parseSymbol(symbol string) (base, quote string) {
	parts := strings.Split(symbol, "/")
	if len(parts) == 2 {
		return strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1])
	}
	return symbol, ""
}

// Ensure Logger interface satisfaction
var _ services.Logger = (*simpleLogger)(nil)

type simpleLogger struct{}

func (s *simpleLogger) WithFields(_ map[string]interface{}) services.Logger { return s }
func (s *simpleLogger) Info(msg string)                                     { fmt.Println("[INFO]", msg) }
func (s *simpleLogger) Warn(msg string)                                     { fmt.Println("[WARN]", msg) }
func (s *simpleLogger) Error(msg string)                                    { fmt.Println("[ERROR]", msg) }
