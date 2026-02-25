package database

import (
	"context"
	"time"
)

// MarketDataPoint represents a single market data record
type MarketDataPoint struct {
	Symbol    string
	Exchange  string
	Price     float64
	Volume    float64
	Timestamp time.Time
}

// ArbitrageOpportunityRecord represents a stored arbitrage opportunity
type ArbitrageOpportunityRecord struct {
	ID              int64
	Symbol          string
	BuyExchange     string
	SellExchange    string
	BuyPrice        float64
	SellPrice       float64
	ProfitPercent   float64
	Volume          float64
	OpportunityType string
	DetectedAt      time.Time
}

// ArbitrageStatsRecord represents arbitrage statistics
type ArbitrageStatsRecord struct {
	TotalOpportunities int64
	AvgProfitPercent   float64
	MaxProfitPercent   float64
	SymbolsCount       int64
	ExchangePairsCount int64
}

// ArbitrageRepository defines the interface for arbitrage data access
type ArbitrageRepository interface {
	// GetRecentMarketData retrieves recent market data for arbitrage analysis
	GetRecentMarketData(ctx context.Context, since time.Time, symbolFilter string) ([]MarketDataPoint, error)

	// GetMarketDataForAnalysis retrieves market data grouped by symbol/exchange for technical analysis
	GetMarketDataForAnalysis(ctx context.Context, since time.Time, symbolFilter string) (map[string]map[string][]MarketDataPoint, error)

	// GetArbitrageHistory retrieves historical arbitrage opportunities
	GetArbitrageHistory(ctx context.Context, limit, offset int, symbolFilter string) ([]ArbitrageOpportunityRecord, error)

	// GetArbitrageStats retrieves arbitrage statistics for a time window
	GetArbitrageStats(ctx context.Context, since time.Time) (*ArbitrageStatsRecord, error)

	// GetOpportunitiesByType retrieves opportunities filtered by type
	GetOpportunitiesByType(ctx context.Context, oppType string, limit int) ([]ArbitrageOpportunityRecord, error)

	// GetExchangePairs retrieves the most active exchange pairs
	GetExchangePairs(ctx context.Context, limit int) ([]struct {
		BuyExchange  string
		SellExchange string
		Count        int64
	}, error)

	// GetSymbolStats retrieves per-symbol arbitrage statistics
	GetSymbolStats(ctx context.Context) ([]struct {
		Symbol            string
		OpportunityCount  int64
		AvgProfitPercent  float64
		LastOpportunityAt time.Time
	}, error)
}

// arbitrageRepositoryImpl implements ArbitrageRepository
type arbitrageRepositoryImpl struct {
	db DBPool
}

// NewArbitrageRepository creates a new arbitrage repository
func NewArbitrageRepository(db DBPool) ArbitrageRepository {
	return &arbitrageRepositoryImpl{db: db}
}

// GetRecentMarketData retrieves recent market data for arbitrage analysis
func (r *arbitrageRepositoryImpl) GetRecentMarketData(ctx context.Context, since time.Time, symbolFilter string) ([]MarketDataPoint, error) {
	if r.db == nil {
		return []MarketDataPoint{}, nil
	}

	query := `
		SELECT tp.symbol, e.name as exchange, md.last_price, md.volume_24h, md.timestamp
		FROM market_data md
		JOIN exchanges e ON md.exchange_id = e.id
		JOIN trading_pairs tp ON md.trading_pair_id = tp.id
		WHERE md.timestamp > ?
		  AND md.last_price > 0
	`
	args := []interface{}{since}

	if symbolFilter != "" {
		query += " AND tp.symbol = ?"
		args = append(args, symbolFilter)
	}

	query += " ORDER BY md.timestamp DESC"

	rows, err := r.db.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []MarketDataPoint
	for rows.Next() {
		var dp MarketDataPoint
		if err := rows.Scan(&dp.Symbol, &dp.Exchange, &dp.Price, &dp.Volume, &dp.Timestamp); err != nil {
			continue
		}
		results = append(results, dp)
	}

	return results, nil
}

// GetMarketDataForAnalysis retrieves market data grouped for technical analysis
func (r *arbitrageRepositoryImpl) GetMarketDataForAnalysis(ctx context.Context, since time.Time, symbolFilter string) (map[string]map[string][]MarketDataPoint, error) {
	data, err := r.GetRecentMarketData(ctx, since, symbolFilter)
	if err != nil {
		return nil, err
	}

	// Group by symbol and exchange
	result := make(map[string]map[string][]MarketDataPoint)
	for _, dp := range data {
		if result[dp.Symbol] == nil {
			result[dp.Symbol] = make(map[string][]MarketDataPoint)
		}
		result[dp.Symbol][dp.Exchange] = append(result[dp.Symbol][dp.Exchange], dp)
	}

	return result, nil
}

// GetArbitrageHistory retrieves historical arbitrage opportunities
func (r *arbitrageRepositoryImpl) GetArbitrageHistory(ctx context.Context, limit, offset int, symbolFilter string) ([]ArbitrageOpportunityRecord, error) {
	if r.db == nil {
		return []ArbitrageOpportunityRecord{}, nil
	}

	query := `
		SELECT id, symbol, buy_exchange, sell_exchange, buy_price, sell_price,
		       profit_percent, volume, opportunity_type, detected_at
		FROM arbitrage_opportunities
		WHERE 1=1
	`
	args := []interface{}{}

	if symbolFilter != "" {
		query += " AND symbol = ?"
		args = append(args, symbolFilter)
	}

	query += " ORDER BY detected_at DESC LIMIT ? OFFSET ?"
	args = append(args, limit, offset)

	rows, err := r.db.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []ArbitrageOpportunityRecord
	for rows.Next() {
		var rec ArbitrageOpportunityRecord
		if err := rows.Scan(
			&rec.ID, &rec.Symbol, &rec.BuyExchange, &rec.SellExchange,
			&rec.BuyPrice, &rec.SellPrice, &rec.ProfitPercent, &rec.Volume,
			&rec.OpportunityType, &rec.DetectedAt,
		); err != nil {
			continue
		}
		results = append(results, rec)
	}

	return results, nil
}

// GetArbitrageStats retrieves arbitrage statistics for a time window
func (r *arbitrageRepositoryImpl) GetArbitrageStats(ctx context.Context, since time.Time) (*ArbitrageStatsRecord, error) {
	if r.db == nil {
		return &ArbitrageStatsRecord{}, nil
	}

	query := `
		SELECT 
			COUNT(*) as total_opportunities,
			COALESCE(AVG(profit_percent), 0) as avg_profit_percent,
			COALESCE(MAX(profit_percent), 0) as max_profit_percent,
			COUNT(DISTINCT symbol) as symbols_count,
			COUNT(DISTINCT buy_exchange || '-' || sell_exchange) as exchange_pairs_count
		FROM arbitrage_opportunities
		WHERE detected_at > ?
	`

	var stats ArbitrageStatsRecord
	err := r.db.QueryRow(ctx, query, since).Scan(
		&stats.TotalOpportunities,
		&stats.AvgProfitPercent,
		&stats.MaxProfitPercent,
		&stats.SymbolsCount,
		&stats.ExchangePairsCount,
	)
	if err != nil {
		return nil, err
	}

	return &stats, nil
}

// GetOpportunitiesByType retrieves opportunities filtered by type
func (r *arbitrageRepositoryImpl) GetOpportunitiesByType(ctx context.Context, oppType string, limit int) ([]ArbitrageOpportunityRecord, error) {
	if r.db == nil {
		return []ArbitrageOpportunityRecord{}, nil
	}

	query := `
		SELECT id, symbol, buy_exchange, sell_exchange, buy_price, sell_price,
		       profit_percent, volume, opportunity_type, detected_at
		FROM arbitrage_opportunities
		WHERE opportunity_type = ?
		ORDER BY detected_at DESC
		LIMIT ?
	`

	rows, err := r.db.Query(ctx, query, oppType, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []ArbitrageOpportunityRecord
	for rows.Next() {
		var rec ArbitrageOpportunityRecord
		if err := rows.Scan(
			&rec.ID, &rec.Symbol, &rec.BuyExchange, &rec.SellExchange,
			&rec.BuyPrice, &rec.SellPrice, &rec.ProfitPercent, &rec.Volume,
			&rec.OpportunityType, &rec.DetectedAt,
		); err != nil {
			continue
		}
		results = append(results, rec)
	}

	return results, nil
}

// GetExchangePairs retrieves the most active exchange pairs
func (r *arbitrageRepositoryImpl) GetExchangePairs(ctx context.Context, limit int) ([]struct {
	BuyExchange  string
	SellExchange string
	Count        int64
}, error) {
	if r.db == nil {
		return nil, nil
	}

	query := `
		SELECT buy_exchange, sell_exchange, COUNT(*) as count
		FROM arbitrage_opportunities
		GROUP BY buy_exchange, sell_exchange
		ORDER BY count DESC
		LIMIT ?
	`

	rows, err := r.db.Query(ctx, query, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []struct {
		BuyExchange  string
		SellExchange string
		Count        int64
	}

	for rows.Next() {
		var r struct {
			BuyExchange  string
			SellExchange string
			Count        int64
		}
		if err := rows.Scan(&r.BuyExchange, &r.SellExchange, &r.Count); err != nil {
			continue
		}
		results = append(results, r)
	}

	return results, nil
}

// GetSymbolStats retrieves per-symbol arbitrage statistics
func (r *arbitrageRepositoryImpl) GetSymbolStats(ctx context.Context) ([]struct {
	Symbol            string
	OpportunityCount  int64
	AvgProfitPercent  float64
	LastOpportunityAt time.Time
}, error) {
	if r.db == nil {
		return nil, nil
	}

	query := `
		SELECT 
			symbol,
			COUNT(*) as opportunity_count,
			AVG(profit_percent) as avg_profit_percent,
			MAX(detected_at) as last_opportunity_at
		FROM arbitrage_opportunities
		GROUP BY symbol
		ORDER BY opportunity_count DESC
	`

	rows, err := r.db.Query(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []struct {
		Symbol            string
		OpportunityCount  int64
		AvgProfitPercent  float64
		LastOpportunityAt time.Time
	}

	for rows.Next() {
		var r struct {
			Symbol            string
			OpportunityCount  int64
			AvgProfitPercent  float64
			LastOpportunityAt time.Time
		}
		if err := rows.Scan(&r.Symbol, &r.OpportunityCount, &r.AvgProfitPercent, &r.LastOpportunityAt); err != nil {
			continue
		}
		results = append(results, r)
	}

	return results, nil
}
