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
		_, _ = fmt.Fprintf(os.Stderr, "collect-candles: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	var (
		exchange   string
		symbols    string
		timeframes string
		startStr   string
		endStr     string
		limit      int
	)

	flag.StringVar(&exchange, "exchange", "binance", "Exchange ID (e.g. binance)")
	flag.StringVar(&symbols, "symbols", "BTC/USDT,ETH/USDT", "Comma-separated trading symbols")
	flag.StringVar(&timeframes, "timeframes", "5m,1h,4h", "Comma-separated timeframes")
	flag.StringVar(&startStr, "start", "", "Start time (RFC3339); defaults to 7 days ago")
	flag.StringVar(&endStr, "end", "", "End time (RFC3339); defaults to now")
	flag.IntVar(&limit, "limit", 1000, "Max candles per request (exchange limit)")
	flag.Parse()

	startTime := time.Now().Add(-7 * 24 * time.Hour)
	if startStr != "" {
		var err error
		startTime, err = time.Parse(time.RFC3339, startStr)
		if err != nil {
			return fmt.Errorf("invalid start time: %w", err)
		}
	}

	endTime := time.Now()
	if endStr != "" {
		var err error
		endTime, err = time.Parse(time.RFC3339, endStr)
		if err != nil {
			return fmt.Errorf("invalid end time: %w", err)
		}
	}

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

	ccxtClient := ccxt.NewClient(&cfg.CCXT)

	dbPool, ok := db.(database.DBPool)
	if !ok {
		return fmt.Errorf("database does not implement DBPool")
	}

	ctx := context.Background()
	totalInserted := 0

	for _, symbol := range symbolList {
		for _, tf := range tfList {
			inserted, fetchErr := collectTimeframe(ctx, dbPool, ccxtClient, cfg, exchange, symbol, tf, startTime, endTime, limit)
			if fetchErr != nil {
				fmt.Fprintf(os.Stderr, "WARN: failed to collect %s %s: %v\n", symbol, tf, fetchErr)
				continue
			}
			totalInserted += inserted
			fmt.Printf("Collected %d candles for %s %s (%s)\n", inserted, symbol, tf, exchange)
		}
	}

	fmt.Printf("\nTotal candles inserted: %d\n", totalInserted)
	return nil
}

func collectTimeframe(
	ctx context.Context,
	db database.DBPool,
	ccxtClient *ccxt.Client,
	cfg *config.Config,
	exchange, symbol, timeframe string,
	startTime, endTime time.Time,
	limit int,
) (int, error) {
	resp, err := ccxtClient.GetOHLCV(ctx, exchange, symbol, timeframe, limit)
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

	inserted := 0
	for _, candle := range resp.OHLCV {
		if candle.Timestamp.Before(startTime) || candle.Timestamp.After(endTime) {
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

	res, err := db.Exec(ctx, "INSERT INTO exchanges (name, ccxt_id, status) VALUES ($1, $1, 'active')", name)
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

	res, err := db.Exec(ctx, "INSERT INTO trading_pairs (symbol, exchange_id, is_active) VALUES ($1, $2, true)", symbol, exchangeID)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
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
