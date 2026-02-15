package services

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/irfndi/neuratrade/internal/ccxt"
	zaplogrus "github.com/irfndi/neuratrade/internal/logging/zaplogrus"
	"github.com/irfndi/neuratrade/pkg/interfaces"
	"github.com/redis/go-redis/v9"
	"github.com/shopspring/decimal"
)

// PositionTrackerConfig holds configuration for position tracking.
type PositionTrackerConfig struct {
	// SyncInterval is how often to reconcile positions with the exchange
	SyncInterval time.Duration
	// RedisKeyPrefix is the prefix for Redis keys
	RedisKeyPrefix string
	// EnableRealTimeSync enables real-time position synchronization
	EnableRealTimeSync bool
}

// DefaultPositionTrackerConfig returns default configuration.
func DefaultPositionTrackerConfig() PositionTrackerConfig {
	return PositionTrackerConfig{
		SyncInterval:       30 * time.Second,
		RedisKeyPrefix:     "position_tracker",
		EnableRealTimeSync: true,
	}
}

// TrackedPosition represents a position with real-time tracking state.
type TrackedPosition struct {
	Position     interfaces.Position `json:"position"`
	LastSyncAt   time.Time           `json:"last_sync_at"`
	PriceUpdated bool                `json:"price_updated"`
}

// PositionTracker manages real-time position tracking with exchange synchronization.
type PositionTracker struct {
	config      PositionTrackerConfig
	ccxtService ccxt.CCXTService
	redisClient *redis.Client
	logger      *zaplogrus.Logger

	// Local cache of tracked positions
	positions   map[string]*TrackedPosition // positionID -> tracked position
	positionsMu sync.RWMutex

	// Callbacks
	onFillCallback        func(ctx context.Context, positionID string, fill FillData) error
	onPriceUpdateCallback func(ctx context.Context, positionID string, newPrice decimal.Decimal) error
	callbacksMu           sync.RWMutex

	// Goroutine control
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// FillData represents a fill event.
type FillData struct {
	PositionID  string          `json:"position_id"`
	OrderID     string          `json:"order_id"`
	Symbol      string          `json:"symbol"`
	Exchange    string          `json:"exchange"`
	Side        string          `json:"side"`
	FillPrice   decimal.Decimal `json:"fill_price"`
	FillSize    decimal.Decimal `json:"fill_size"`
	RealizedPnL decimal.Decimal `json:"realized_pnl,omitempty"`
	Commission  decimal.Decimal `json:"commission,omitempty"`
	Timestamp   time.Time       `json:"timestamp"`
}

// NewPositionTracker creates a new position tracker service.
func NewPositionTracker(
	config PositionTrackerConfig,
	ccxtService ccxt.CCXTService,
	redisClient *redis.Client,
	logger *zaplogrus.Logger,
) *PositionTracker {
	ctx, cancel := context.WithCancel(context.Background())
	return &PositionTracker{
		config:      config,
		ccxtService: ccxtService,
		redisClient: redisClient,
		logger:      logger,
		positions:   make(map[string]*TrackedPosition),
		ctx:         ctx,
		cancel:      cancel,
	}
}

// Start begins the position tracking goroutines.
func (pt *PositionTracker) Start() {
	if !pt.config.EnableRealTimeSync {
		pt.logger.Info("Position tracker disabled")
		return
	}

	// Load positions from Redis
	if err := pt.loadPositionsFromRedis(pt.ctx); err != nil {
		pt.logger.WithError(err).Error("Failed to load positions from Redis")
	}

	// Start sync goroutine
	pt.wg.Add(1)
	go pt.syncLoop()

	pt.logger.Info("Position tracker started",
		"sync_interval", pt.config.SyncInterval)
}

// Stop stops the position tracker.
func (pt *PositionTracker) Stop() {
	pt.cancel()
	pt.wg.Wait()

	// Save positions to Redis before stopping - use fresh context
	saveCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := pt.savePositionsToRedis(saveCtx); err != nil {
		pt.logger.WithError(err).Error("Failed to save positions to Redis")
	}

	pt.logger.Info("Position tracker stopped")
}

// syncLoop periodically syncs positions with the exchange.
func (pt *PositionTracker) syncLoop() {
	defer pt.wg.Done()

	ticker := time.NewTicker(pt.config.SyncInterval)
	defer ticker.Stop()

	// Initial sync
	if err := pt.SyncWithExchange(pt.ctx); err != nil {
		pt.logger.WithError(err).Warn("Initial position sync failed")
	}

	for {
		select {
		case <-pt.ctx.Done():
			return
		case <-ticker.C:
			if err := pt.SyncWithExchange(pt.ctx); err != nil {
				pt.logger.WithError(err).Warn("Position sync failed")
			}
		}
	}
}

// SyncWithExchange reconciles all tracked positions with the exchange.
func (pt *PositionTracker) SyncWithExchange(ctx context.Context) error {
	pt.positionsMu.RLock()
	positionIDs := make([]string, 0, len(pt.positions))
	for id := range pt.positions {
		positionIDs = append(positionIDs, id)
	}
	pt.positionsMu.RUnlock()

	var lastErr error
	for _, positionID := range positionIDs {
		pt.positionsMu.RLock()
		tracked, exists := pt.positions[positionID]
		if !exists {
			pt.positionsMu.RUnlock()
			continue
		}

		// Make a copy to avoid holding lock during I/O
		position := tracked.Position
		pt.positionsMu.RUnlock()

		if position.Exchange == "" || position.Symbol == "" {
			continue
		}

		// Fetch current price from exchange
		ticker, err := pt.ccxtService.FetchSingleTicker(ctx, position.Exchange, position.Symbol)
		if err != nil {
			pt.logger.WithError(err).Warn("Failed to fetch ticker for position sync",
				"position_id", positionID,
				"symbol", position.Symbol)
			lastErr = err
			continue
		}

		currentPrice := decimal.NewFromFloat(ticker.GetPrice())

		// Update position with current price
		pt.positionsMu.Lock()
		if tracked, exists := pt.positions[positionID]; exists {
			tracked.Position.CurrentPrice = currentPrice
			tracked.Position.UpdatedAt = time.Now().UTC()
			tracked.LastSyncAt = time.Now().UTC()
			tracked.PriceUpdated = true

			// Calculate unrealized PnL
			pt.calculateUnrealizedPL(tracked)
		}

		// Copy needed values before releasing lock
		unrealizedPL := tracked.Position.UnrealizedPL
		symbol := position.Symbol

		pt.positionsMu.Unlock()

		// Copy callback reference with proper locking
		pt.callbacksMu.RLock()
		onPriceUpdateCb := pt.onPriceUpdateCallback
		pt.callbacksMu.RUnlock()

		// Trigger price update callback
		if onPriceUpdateCb != nil {
			if err := onPriceUpdateCb(ctx, positionID, currentPrice); err != nil {
				pt.logger.WithError(err).Error("Price update callback failed",
					"position_id", positionID)
			}
		}

		pt.logger.Debug("Position synced",
			"position_id", positionID,
			"symbol", symbol,
			"current_price", currentPrice,
			"unrealized_pl", unrealizedPL)
	}

	// Save to Redis after sync
	if err := pt.savePositionsToRedis(ctx); err != nil {
		pt.logger.WithError(err).Error("Failed to save positions to Redis after sync")
	}

	return lastErr
}

// OnFill handles fill events to update positions.
func (pt *PositionTracker) OnFill(ctx context.Context, fill FillData) error {
	pt.positionsMu.Lock()

	tracked, exists := pt.positions[fill.PositionID]
	if !exists {
		// New position - create tracking entry
		position := interfaces.Position{
			PositionID:   fill.PositionID,
			OrderID:      fill.OrderID,
			Exchange:     fill.Exchange,
			Symbol:       fill.Symbol,
			Side:         fill.Side,
			Size:         fill.FillSize,
			EntryPrice:   fill.FillPrice,
			CurrentPrice: fill.FillPrice,
			Status:       interfaces.PositionStatusOpen,
			OpenedAt:     fill.Timestamp,
			UpdatedAt:    fill.Timestamp,
		}

		tracked = &TrackedPosition{
			Position:     position,
			LastSyncAt:   time.Now().UTC(),
			PriceUpdated: false,
		}
		pt.positions[fill.PositionID] = tracked

		pt.logger.Info("New position tracked from fill",
			"position_id", fill.PositionID,
			"symbol", fill.Symbol,
			"size", fill.FillSize,
			"entry_price", fill.FillPrice)
	} else {
		// Existing position - update
		tracked.Position.OrderID = fill.OrderID
		tracked.Position.Size = fill.FillSize
		tracked.Position.EntryPrice = fill.FillPrice
		tracked.Position.UpdatedAt = fill.Timestamp
		tracked.LastSyncAt = time.Now().UTC()

		pt.logger.Info("Position updated from fill",
			"position_id", fill.PositionID,
			"new_size", fill.FillSize,
			"new_entry_price", fill.FillPrice)
	}

	// Copy callback reference and release lock before invoking
	pt.callbacksMu.RLock()
	onFillCb := pt.onFillCallback
	pt.callbacksMu.RUnlock()

	pt.positionsMu.Unlock()

	// Trigger fill callback outside of lock to avoid deadlock
	if onFillCb != nil {
		if err := onFillCb(ctx, fill.PositionID, fill); err != nil {
			pt.logger.WithError(err).Error("Fill callback failed",
				"position_id", fill.PositionID)
		}
	}

	// Save to Redis
	return pt.savePositionsToRedis(ctx)
}

// OnPriceUpdate updates a position's price and calculates unrealized PnL.
func (pt *PositionTracker) OnPriceUpdate(ctx context.Context, positionID string, newPrice decimal.Decimal) error {
	pt.positionsMu.Lock()
	defer pt.positionsMu.Unlock()

	tracked, exists := pt.positions[positionID]
	if !exists {
		return fmt.Errorf("position not found: %s", positionID)
	}

	oldPrice := tracked.Position.CurrentPrice
	tracked.Position.CurrentPrice = newPrice
	tracked.Position.UpdatedAt = time.Now().UTC()
	tracked.PriceUpdated = true

	// Calculate unrealized PnL
	pt.calculateUnrealizedPL(tracked)

	pt.logger.Debug("Position price updated",
		"position_id", positionID,
		"old_price", oldPrice,
		"new_price", newPrice,
		"unrealized_pl", tracked.Position.UnrealizedPL)

	// Trigger price update callback
	if pt.onPriceUpdateCallback != nil {
		return pt.onPriceUpdateCallback(ctx, positionID, newPrice)
	}

	return nil
}

// calculateUnrealizedPL calculates the unrealized profit/loss for a position.
func (pt *PositionTracker) calculateUnrealizedPL(tracked *TrackedPosition) {
	position := &tracked.Position

	if position.Size.IsZero() || position.EntryPrice.IsZero() || position.CurrentPrice.IsZero() {
		position.UnrealizedPL = decimal.Zero
		return
	}

	priceDiff := position.CurrentPrice.Sub(position.EntryPrice)

	if strings.EqualFold(position.Side, "BUY") || strings.EqualFold(position.Side, "long") {
		position.UnrealizedPL = priceDiff.Mul(position.Size)
	} else if strings.EqualFold(position.Side, "SELL") || strings.EqualFold(position.Side, "short") {
		position.UnrealizedPL = priceDiff.Mul(position.Size).Neg()
	} else {
		position.UnrealizedPL = decimal.Zero
	}
}

// GetPosition returns a tracked position by ID.
func (pt *PositionTracker) GetPosition(positionID string) (interfaces.Position, bool) {
	pt.positionsMu.RLock()
	defer pt.positionsMu.RUnlock()

	tracked, exists := pt.positions[positionID]
	if !exists {
		return interfaces.Position{}, false
	}

	return tracked.Position, true
}

// GetAllPositions returns all tracked positions.
func (pt *PositionTracker) GetAllPositions() []interfaces.Position {
	pt.positionsMu.RLock()
	defer pt.positionsMu.RUnlock()

	positions := make([]interfaces.Position, 0, len(pt.positions))
	for _, tracked := range pt.positions {
		positions = append(positions, tracked.Position)
	}

	return positions
}

// GetOpenPositions returns all open positions.
func (pt *PositionTracker) GetOpenPositions() []interfaces.Position {
	pt.positionsMu.RLock()
	defer pt.positionsMu.RUnlock()

	positions := make([]interfaces.Position, 0)
	for _, tracked := range pt.positions {
		if tracked.Position.Status == interfaces.PositionStatusOpen {
			positions = append(positions, tracked.Position)
		}
	}

	return positions
}

// ClosePosition marks a position as closed.
func (pt *PositionTracker) ClosePosition(ctx context.Context, positionID string) error {
	pt.positionsMu.Lock()

	tracked, exists := pt.positions[positionID]
	if !exists {
		pt.positionsMu.Unlock()
		return fmt.Errorf("position not found: %s", positionID)
	}

	tracked.Position.Status = interfaces.PositionStatusClosed
	tracked.Position.UpdatedAt = time.Now().UTC()

	// Copy needed values before releasing lock
	unrealizedPL := tracked.Position.UnrealizedPL

	pt.positionsMu.Unlock()

	pt.logger.Info("Position closed",
		"position_id", positionID,
		"realized_pl", unrealizedPL)

	return pt.savePositionsToRedis(ctx)
}

// LiquidatePosition marks a position as liquidated.
func (pt *PositionTracker) LiquidatePosition(ctx context.Context, positionID string) error {
	pt.positionsMu.Lock()

	tracked, exists := pt.positions[positionID]
	if !exists {
		pt.positionsMu.Unlock()
		return fmt.Errorf("position not found: %s", positionID)
	}

	tracked.Position.Status = interfaces.PositionStatusLiquidated
	tracked.Position.UpdatedAt = time.Now().UTC()

	// Copy needed values before releasing lock
	unrealizedPL := tracked.Position.UnrealizedPL

	pt.positionsMu.Unlock()

	pt.logger.Warn("Position liquidated",
		"position_id", positionID,
		"unrealized_pl", unrealizedPL)

	return pt.savePositionsToRedis(ctx)
}

// SetOnFillCallback sets the callback for fill events.
func (pt *PositionTracker) SetOnFillCallback(callback func(ctx context.Context, positionID string, fill FillData) error) {
	pt.callbacksMu.Lock()
	defer pt.callbacksMu.Unlock()
	pt.onFillCallback = callback
}

// SetOnPriceUpdateCallback sets the callback for price update events.
func (pt *PositionTracker) SetOnPriceUpdateCallback(callback func(ctx context.Context, positionID string, newPrice decimal.Decimal) error) {
	pt.callbacksMu.Lock()
	defer pt.callbacksMu.Unlock()
	pt.onPriceUpdateCallback = callback
}

// loadPositionsFromRedis loads tracked positions from Redis.
func (pt *PositionTracker) loadPositionsFromRedis(ctx context.Context) error {
	if pt.redisClient == nil {
		return nil
	}

	pattern := fmt.Sprintf("%s:*", pt.config.RedisKeyPrefix)

	// Load data from Redis without holding lock
	newPositions := make(map[string]*TrackedPosition)

	// Use SCAN instead of KEYS for non-blocking iteration
	var cursor uint64
	for {
		keys, nextCursor, err := pt.redisClient.Scan(ctx, cursor, pattern, 100).Result()
		if err != nil {
			return err
		}

		for _, key := range keys {
			data, err := pt.redisClient.Get(ctx, key).Result()
			if err != nil {
				continue
			}

			var tracked TrackedPosition
			if err := json.Unmarshal([]byte(data), &tracked); err != nil {
				pt.logger.WithError(err).Warn("Failed to unmarshal position from Redis", "key", key)
				continue
			}

			newPositions[tracked.Position.PositionID] = &tracked
		}

		cursor = nextCursor
		if cursor == 0 {
			break
		}
	}

	// Now acquire lock and merge
	pt.positionsMu.Lock()
	for id, tracked := range newPositions {
		pt.positions[id] = tracked
	}
	pt.positionsMu.Unlock()

	pt.logger.Info("Loaded positions from Redis", "count", len(newPositions))
	return nil
}

// savePositionsToRedis saves tracked positions to Redis.
func (pt *PositionTracker) savePositionsToRedis(ctx context.Context) error {
	if pt.redisClient == nil {
		return nil
	}

	pt.positionsMu.RLock()
	defer pt.positionsMu.RUnlock()

	for positionID, tracked := range pt.positions {
		key := fmt.Sprintf("%s:%s", pt.config.RedisKeyPrefix, positionID)
		data, err := json.Marshal(tracked)
		if err != nil {
			pt.logger.WithError(err).Error("Failed to marshal position", "position_id", positionID)
			continue
		}

		if err := pt.redisClient.Set(ctx, key, data, 24*time.Hour).Err(); err != nil {
			pt.logger.WithError(err).Error("Failed to save position to Redis", "position_id", positionID)
		}
	}

	return nil
}
