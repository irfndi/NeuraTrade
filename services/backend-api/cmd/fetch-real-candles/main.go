package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/irfndi/neuratrade/internal/ccxt"
	"github.com/irfndi/neuratrade/internal/config"
	"github.com/irfndi/neuratrade/internal/database"
)

func main() {
	if err := run(); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "fetch-real-candles: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	var (
		symbols    string
		timeframes string
		days       int
	)
	flag.StringVar(&symbols, "symbols", "BTC/USDT,ETH/USDT,SOL/USDT,BNB/USDT", "Comma-separated symbols")
	flag.StringVar(&timeframes, "timeframes", "5m,1h,4h", "Comma-separated timeframes")
	flag.IntVar(&days, "days", 30, "Number of days of historical data to fetch")
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

	// Use native CCXT service to fetch directly from exchange APIs
	ccxtSvc := ccxt.NewNativeCCXTService(30*time.Second, 3)
	ctx := context.Background()
	if initErr := ccxtSvc.Initialize(ctx); initErr != nil {
		return fmt.Errorf("initialize native ccxt service: %w", initErr)
	}
	defer func() { _ = ccxtSvc.Close() }()

	totalInserted := 0
	for _, symbol := range symbolList {
		for _, tf := range tfList {
			inserted, fetchErr := fetchAndStore(ctx, dbPool, ccxtSvc, cfg, "binance", symbol, tf, days)
			if fetchErr != nil {
				fmt.Fprintf(os.Stderr, "WARN: failed to fetch %s %s: %v\n", symbol, tf, fetchErr)
				continue
			}
			totalInserted += inserted
			fmt.Printf("Fetched %d real candles for %s %s\n", inserted, symbol, tf)
		}
	}

	fmt.Printf("\nTotal real candles inserted: %d\n", totalInserted)
	return nil
}

func fetchAndStore(
	ctx context.Context,
	db database.DBPool,
	ccxtSvc *ccxt.NativeCCXTService,
	cfg *config.Config,
	exchange, symbol, timeframe string,
	days int,
) (int, error) {
	resp, err := ccxtSvc.FetchOHLCV(ctx, exchange, symbol, timeframe, 1000)
	if err != nil {
		return 0, fmt.Errorf("fetch OHLCV: %w", err)
	}
	if resp == nil || len(resp.OHLCV) == 0 {
		return 0, fmt.Errorf("no OHLCV data returned")
	}

	exchangeID, err := getOrCreateExchange(ctx, db, exchange)
	if err != nil {
		return 0, fmt.Errorf("lookup exchange: %w", err)
	}

	pairID, err := getOrCreateTradingPair(ctx, db, exchangeID, symbol)
	if err != nil {
		return 0, fmt.Errorf("lookup trading pair: %w", err)
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

	cutoff := time.Now().Add(-time.Duration(days*24) * time.Hour)
	inserted := 0
	for _, candle := range resp.OHLCV {
		if candle.Timestamp.Before(cutoff) {
			continue
		}

		_, err := db.Exec(ctx, query,
			exchangeID, pairID, timeframe,
			candle.Open, candle.High, candle.Low, candle.Close, candle.Volume,
			candle.Timestamp,
		)
		if err != nil {
			return inserted, fmt.Errorf("insert candle: %w", err)
		}
		inserted++
	}

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
