package database

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/irfndi/neuratrade/internal/ports"
	"github.com/jackc/pgx/v5"
)

// orderBookSnapshotRepo implements ports.OrderBookSnapshotRepository.
type orderBookSnapshotRepo struct {
	db DBPool
}

// NewOrderBookSnapshotRepository creates a new order book snapshot repository.
func NewOrderBookSnapshotRepository(db DBPool) ports.OrderBookSnapshotRepository {
	return &orderBookSnapshotRepo{db: db}
}

func (r *orderBookSnapshotRepo) SaveSnapshot(ctx context.Context, snap ports.OrderBookSnapshot) error {
	if r.db == nil {
		return nil
	}

	query := `
		INSERT INTO order_book_snapshots
			(exchange, symbol, best_bid, best_ask, mid_price, bid_ask_spread_pct,
			 bid_depth_1pct, ask_depth_1pct, bid_depth_2pct, ask_depth_2pct,
			 imbalance_1pct, imbalance_2pct, bid_levels, ask_levels,
			 liquidity_score, snapshot_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16)
	`

	_, err := r.db.Exec(ctx, query,
		snap.Exchange, snap.Symbol, snap.BestBid, snap.BestAsk, snap.MidPrice,
		snap.BidAskSpreadPct, snap.BidDepth1Pct, snap.AskDepth1Pct,
		snap.BidDepth2Pct, snap.AskDepth2Pct, snap.Imbalance1Pct, snap.Imbalance2Pct,
		snap.BidLevels, snap.AskLevels, snap.LiquidityScore, snap.SnapshotAt,
	)
	if err != nil {
		return fmt.Errorf("save order book snapshot for %s:%s: %w", snap.Exchange, snap.Symbol, err)
	}
	return nil
}

func (r *orderBookSnapshotRepo) GetLatestSnapshot(ctx context.Context, exchange, symbol string) (*ports.OrderBookSnapshot, error) {
	if r.db == nil {
		return nil, nil
	}

	query := `
		SELECT exchange, symbol, best_bid, best_ask, mid_price, bid_ask_spread_pct,
			   bid_depth_1pct, ask_depth_1pct, bid_depth_2pct, ask_depth_2pct,
			   imbalance_1pct, imbalance_2pct, bid_levels, ask_levels,
			   liquidity_score, snapshot_at
		FROM order_book_snapshots
		WHERE exchange = $1 AND symbol = $2
		ORDER BY snapshot_at DESC
		LIMIT 1
	`

	var snap ports.OrderBookSnapshot
	err := r.db.QueryRow(ctx, query, exchange, symbol).Scan(
		&snap.Exchange, &snap.Symbol, &snap.BestBid, &snap.BestAsk, &snap.MidPrice,
		&snap.BidAskSpreadPct, &snap.BidDepth1Pct, &snap.AskDepth1Pct,
		&snap.BidDepth2Pct, &snap.AskDepth2Pct, &snap.Imbalance1Pct, &snap.Imbalance2Pct,
		&snap.BidLevels, &snap.AskLevels, &snap.LiquidityScore, &snap.SnapshotAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("get latest order book snapshot for %s:%s: %w", exchange, symbol, err)
	}
	return &snap, nil
}

func (r *orderBookSnapshotRepo) GetSnapshotsInRange(ctx context.Context, exchange, symbol string, from, to time.Time, limit int) ([]ports.OrderBookSnapshot, error) {
	if r.db == nil {
		return nil, nil
	}

	if limit <= 0 {
		limit = 100
	}

	query := `
		SELECT exchange, symbol, best_bid, best_ask, mid_price, bid_ask_spread_pct,
			   bid_depth_1pct, ask_depth_1pct, bid_depth_2pct, ask_depth_2pct,
			   imbalance_1pct, imbalance_2pct, bid_levels, ask_levels,
			   liquidity_score, snapshot_at
		FROM order_book_snapshots
		WHERE exchange = $1 AND symbol = $2 AND snapshot_at >= $3 AND snapshot_at <= $4
		ORDER BY snapshot_at DESC
		LIMIT $5
	`

	rows, err := r.db.Query(ctx, query, exchange, symbol, from, to, limit)
	if err != nil {
		return nil, fmt.Errorf("get order book snapshots for %s:%s: %w", exchange, symbol, err)
	}
	defer rows.Close()

	var results []ports.OrderBookSnapshot
	for rows.Next() {
		var snap ports.OrderBookSnapshot
		if err := rows.Scan(
			&snap.Exchange, &snap.Symbol, &snap.BestBid, &snap.BestAsk, &snap.MidPrice,
			&snap.BidAskSpreadPct, &snap.BidDepth1Pct, &snap.AskDepth1Pct,
			&snap.BidDepth2Pct, &snap.AskDepth2Pct, &snap.Imbalance1Pct, &snap.Imbalance2Pct,
			&snap.BidLevels, &snap.AskLevels, &snap.LiquidityScore, &snap.SnapshotAt,
		); err != nil {
			return nil, fmt.Errorf("scan order book snapshot row for %s:%s: %w", exchange, symbol, err)
		}
		results = append(results, snap)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate order book snapshots for %s:%s: %w", exchange, symbol, err)
	}

	return results, nil
}
