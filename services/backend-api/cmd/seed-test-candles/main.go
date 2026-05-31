package main

import (
	"context"
	"flag"
	"fmt"
	"math"
	"math/rand"
	"os"
	"strings"
	"time"

	"github.com/irfndi/neuratrade/internal/config"
	"github.com/irfndi/neuratrade/internal/database"
	"github.com/shopspring/decimal"
)

func main() {
	if err := run(); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "seed-test-candles: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	var (
		symbols    string
		timeframes string
		days       int
		seed       int64
	)

	flag.StringVar(&symbols, "symbols", "BTC/USDT,ETH/USDT,SOL/USDT,BNB/USDT", "Comma-separated symbols")
	flag.StringVar(&timeframes, "timeframes", "5m,1h,4h", "Comma-separated timeframes")
	flag.IntVar(&days, "days", 30, "Number of days of synthetic data to generate")
	flag.Int64Var(&seed, "seed", 42, "Random seed for reproducible data")
	flag.Parse()

	symbolList := splitComma(symbols)
	tfList := splitComma(timeframes)

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
	totalInserted := 0

	for _, symbol := range symbolList {
		for _, tf := range tfList {
			inserted, genErr := generateAndStoreCandles(ctx, dbPool, cfg, symbol, tf, days, seed)
			if genErr != nil {
				fmt.Fprintf(os.Stderr, "WARN: failed to generate %s %s: %v\n", symbol, tf, genErr)
				continue
			}
			totalInserted += inserted
			fmt.Printf("Generated %d synthetic candles for %s %s\n", inserted, symbol, tf)
		}
	}

	fmt.Printf("\nTotal synthetic candles inserted: %d\n", totalInserted)
	return nil
}

func generateAndStoreCandles(
	ctx context.Context,
	db database.DBPool,
	cfg *config.Config,
	symbol, timeframe string,
	days int,
	seed int64,
) (int, error) {
	exchangeID, err := getOrCreateExchange(ctx, db, "binance")
	if err != nil {
		return 0, fmt.Errorf("lookup exchange: %w", err)
	}

	pairID, err := getOrCreateTradingPair(ctx, db, exchangeID, symbol)
	if err != nil {
		return 0, fmt.Errorf("lookup trading pair: %w", err)
	}

	candles := generateSyntheticCandles(symbol, timeframe, days, seed)
	if len(candles) == 0 {
		return 0, fmt.Errorf("no candles generated")
	}

	dbType := database.DetectDBType(cfg.Database.Driver)
	var query string
	if dbType == database.DBTypeSQLite {
		query = `INSERT OR IGNORE INTO ohlcv_data (exchange_id, trading_pair_id, timeframe, open_price, high_price, low_price, close_price, volume, timestamp)
				 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`
	} else {
		query = `INSERT INTO ohlcv_data (exchange_id, trading_pair_id, timeframe, open_price, high_price, low_price, close_price, volume, timestamp)
				 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
				 ON CONFLICT (exchange_id, trading_pair_id, timeframe, timestamp) DO NOTHING`
	}

	inserted := 0
	for _, c := range candles {
		_, err := db.Exec(ctx, query,
			exchangeID, pairID, timeframe,
			c.Open, c.High, c.Low, c.Close, c.Volume,
			c.Timestamp,
		)
		if err != nil {
			return inserted, fmt.Errorf("insert candle: %w", err)
		}
		inserted++
	}

	return inserted, nil
}

type syntheticCandle struct {
	Timestamp time.Time
	Open      decimal.Decimal
	High      decimal.Decimal
	Low       decimal.Decimal
	Close     decimal.Decimal
	Volume    decimal.Decimal
}

// generateSyntheticCandles creates realistic-looking price action that includes
// both trending and choppy periods so the backfill simulator produces both
// winning and losing trades.
func generateSyntheticCandles(symbol, timeframe string, days int, seed int64) []syntheticCandle {
	rng := rand.New(rand.NewSource(seed + int64(hashString(symbol)) + int64(hashString(timeframe))))

	// Base price depends on symbol
	basePrice := 50000.0
	switch {
	case strings.Contains(symbol, "BTC"):
		basePrice = 65000.0
	case strings.Contains(symbol, "ETH"):
		basePrice = 3500.0
	case strings.Contains(symbol, "SOL"):
		basePrice = 150.0
	case strings.Contains(symbol, "BNB"):
		basePrice = 600.0
	}

	interval := timeframeToDuration(timeframe)
	if interval <= 0 {
		interval = time.Hour
	}

	numCandles := int(time.Duration(days*24) * time.Hour / interval)
	if numCandles <= 0 {
		return nil
	}

	candles := make([]syntheticCandle, 0, numCandles)
	price := basePrice
	volatility := basePrice * 0.008 // 0.8% base volatility

	// Create several market regimes: bull, bear, sideways, volatile
	regimeLength := numCandles / 4
	regimes := []struct {
		drift   float64
		volMult float64
	}{
		{0.0003, 1.0},  // bull - slight upward drift
		{-0.0005, 1.2}, // bear - downward drift, higher vol
		{0.0001, 0.6},  // sideways - low vol
		{-0.0002, 1.5}, // volatile chop
	}

	now := time.Now().UTC()
	startTime := now.Add(-time.Duration(days*24) * time.Hour)

	for i := range numCandles {
		regime := regimes[i/regimeLength]
		if regimeLength == 0 {
			regime = regimes[0]
		}

		// Random walk with regime drift
		changePct := rng.NormFloat64()*0.005*regime.volMult + regime.drift
		change := price * changePct

		open := price
		close := price + change
		if close <= 0 {
			close = price * 0.999
		}

		// High and low based on volatility
		high := math.Max(open, close) + rng.Float64()*volatility*regime.volMult
		low := math.Min(open, close) - rng.Float64()*volatility*regime.volMult
		if low <= 0 {
			low = close * 0.999
		}

		volume := basePrice * 100 * (0.5 + rng.Float64())

		timestamp := startTime.Add(time.Duration(i) * interval)

		candles = append(candles, syntheticCandle{
			Timestamp: timestamp,
			Open:      decimal.NewFromFloat(open),
			High:      decimal.NewFromFloat(high),
			Low:       decimal.NewFromFloat(low),
			Close:     decimal.NewFromFloat(close),
			Volume:    decimal.NewFromFloat(volume),
		})

		price = close
	}

	return candles
}

func timeframeToDuration(tf string) time.Duration {
	switch tf {
	case "1m":
		return time.Minute
	case "5m":
		return 5 * time.Minute
	case "15m":
		return 15 * time.Minute
	case "30m":
		return 30 * time.Minute
	case "1h":
		return time.Hour
	case "4h":
		return 4 * time.Hour
	case "1d":
		return 24 * time.Hour
	default:
		return 0
	}
}

func hashString(s string) int {
	h := 0
	for i := 0; i < len(s); i++ {
		h = h*31 + int(s[i])
	}
	return h
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

func splitComma(s string) []string {
	parts := strings.Split(s, ",")
	result := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			result = append(result, p)
		}
	}
	return result
}
