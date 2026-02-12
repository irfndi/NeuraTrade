package services

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/irfandi/celebrum-ai-go/internal/database"
	"github.com/irfandi/celebrum-ai-go/internal/observability"
	"github.com/shopspring/decimal"
)

// FeeProvider defines access to exchange-specific fees.
type FeeProvider interface {
	GetTakerFee(ctx context.Context, exchange string, symbol string) (decimal.Decimal, error)
	GetMakerFee(ctx context.Context, exchange string, symbol string) (decimal.Decimal, error)
}

type feeCacheEntry struct {
	taker decimal.Decimal
	maker decimal.Decimal
	exp   time.Time
}

// DBFeeProvider retrieves fees from exchange_trading_pairs with an in-memory cache.
type DBFeeProvider struct {
	db              database.DatabasePool
	defaultTakerFee decimal.Decimal
	defaultMakerFee decimal.Decimal
	cacheTTL        time.Duration
	mu              sync.RWMutex
	cache           map[string]feeCacheEntry
}

// NewDBFeeProvider creates a fee provider backed by the database.
func NewDBFeeProvider(db any, defaultTakerFee decimal.Decimal, defaultMakerFee decimal.Decimal) *DBFeeProvider {
	var dbPool database.DatabasePool
	switch typed := db.(type) {
	case nil:
		dbPool = nil
	case database.DatabasePool:
		dbPool = typed
	case database.LegacyDBPool:
		dbPool = database.WrapLegacyDBPool(typed)
	case database.LegacyQuerier:
		dbPool = database.WrapLegacyQuerier(typed)
	default:
		dbPool = nil
	}

	if defaultTakerFee.IsZero() {
		defaultTakerFee = decimal.NewFromFloat(0.001)
	}
	if defaultMakerFee.IsZero() {
		defaultMakerFee = decimal.NewFromFloat(0.001)
	}

	return &DBFeeProvider{
		db:              dbPool,
		defaultTakerFee: defaultTakerFee,
		defaultMakerFee: defaultMakerFee,
		cacheTTL:        30 * time.Minute,
		cache:           make(map[string]feeCacheEntry),
	}
}

// GetTakerFee returns the taker fee for an exchange/symbol.
func (p *DBFeeProvider) GetTakerFee(ctx context.Context, exchange string, symbol string) (decimal.Decimal, error) {
	spanCtx, span := observability.StartSpan(ctx, observability.SpanOpDBQuery, "FeeProvider.GetTakerFee")
	defer observability.FinishSpan(span, nil)

	fees, err := p.getFees(spanCtx, exchange, symbol)
	if err != nil {
		observability.CaptureException(spanCtx, err)
		return p.defaultTakerFee, err
	}
	if !fees.taker.IsZero() {
		return fees.taker, nil
	}
	return p.defaultTakerFee, nil
}

// GetMakerFee returns the maker fee for an exchange/symbol.
func (p *DBFeeProvider) GetMakerFee(ctx context.Context, exchange string, symbol string) (decimal.Decimal, error) {
	spanCtx, span := observability.StartSpan(ctx, observability.SpanOpDBQuery, "FeeProvider.GetMakerFee")
	defer observability.FinishSpan(span, nil)

	fees, err := p.getFees(spanCtx, exchange, symbol)
	if err != nil {
		observability.CaptureException(spanCtx, err)
		return p.defaultMakerFee, err
	}
	if !fees.maker.IsZero() {
		return fees.maker, nil
	}
	return p.defaultMakerFee, nil
}

func (p *DBFeeProvider) getFees(ctx context.Context, exchange string, symbol string) (feeCacheEntry, error) {
	if p == nil || p.db == nil {
		return feeCacheEntry{}, fmt.Errorf("fee provider database is not available")
	}

	cacheKey := fmt.Sprintf("%s|%s", exchange, symbol)
	p.mu.RLock()
	entry, ok := p.cache[cacheKey]
	p.mu.RUnlock()
	if ok && time.Now().Before(entry.exp) {
		return entry, nil
	}

	// Try pair-specific fees first
	query := `
		SELECT etp.taker_fee, etp.maker_fee
		FROM exchange_trading_pairs etp
		JOIN exchanges e ON etp.exchange_id = e.id
		JOIN trading_pairs tp ON etp.trading_pair_id = tp.id
		WHERE e.name = $1 AND tp.symbol = $2
		LIMIT 1
	`

	var taker, maker decimal.Decimal
	err := p.db.QueryRow(ctx, query, exchange, symbol).Scan(&taker, &maker)

	// If not found or zero, try exchange-level default
	if err != nil || taker.IsZero() {
		fallbackQuery := `
			SELECT ef.taker_fee, ef.maker_fee
			FROM exchange_fees ef
			JOIN exchanges e ON ef.exchange_id = e.id
			WHERE e.name = $1
			LIMIT 1
		`
		var fTaker, fMaker decimal.Decimal
		if fErr := p.db.QueryRow(ctx, fallbackQuery, exchange).Scan(&fTaker, &fMaker); fErr == nil {
			taker = fTaker
			maker = fMaker
		}
	}

	// Still zero? Use configured defaults
	if taker.IsZero() {
		taker = p.defaultTakerFee
	}
	if maker.IsZero() {
		maker = p.defaultMakerFee
	}

	entry = feeCacheEntry{
		taker: taker,
		maker: maker,
		exp:   time.Now().Add(p.cacheTTL),
	}

	p.mu.Lock()
	p.cache[cacheKey] = entry
	p.mu.Unlock()

	return entry, nil
}
