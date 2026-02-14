package services

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/irfndi/neuratrade/internal/models"
	"github.com/shopspring/decimal"
)

// OHLCVBar represents a single OHLCV candle
type OHLCVBar struct {
	Symbol    string          `json:"symbol"`
	Timestamp time.Time       `json:"timestamp"`
	Open      decimal.Decimal `json:"open"`
	High      decimal.Decimal `json:"high"`
	Low       decimal.Decimal `json:"low"`
	Close     decimal.Decimal `json:"close"`
	Volume    decimal.Decimal `json:"volume"`
	Exchange  string          `json:"exchange,omitempty"`
}

// ReplayConfig holds configuration for OHLCV replay
type ReplayConfig struct {
	Symbol          string
	Exchange        string
	StartTime       time.Time
	EndTime         time.Time
	Timeframe       time.Duration
	SpeedMultiplier float64
	BufferSize      int
	Loop            bool
}

// DefaultReplayConfig returns default replay configuration
func DefaultReplayConfig() ReplayConfig {
	return ReplayConfig{
		Timeframe:       5 * time.Minute,
		SpeedMultiplier: 1.0,
		BufferSize:      1000,
		Loop:            false,
	}
}

// ReplayEvent represents an event during replay
type ReplayEvent struct {
	Type      string    `json:"type"`
	Bar       OHLCVBar  `json:"bar,omitempty"`
	Progress  float64   `json:"progress"`
	Timestamp time.Time `json:"timestamp"`
}

// ReplayStats holds statistics for a replay session
type ReplayStats struct {
	TotalBars      int           `json:"total_bars"`
	ProcessedBars  int           `json:"processed_bars"`
	StartTime      time.Time     `json:"start_time"`
	EndTime        *time.Time    `json:"end_time,omitempty"`
	Duration       time.Duration `json:"duration"`
	AvgBarInterval time.Duration `json:"avg_bar_interval"`
}

// OHLCVReplayEngine replays historical OHLCV data
type OHLCVReplayEngine struct {
	config     ReplayConfig
	bars       []OHLCVBar
	currentIdx int
	mu         sync.RWMutex
	isRunning  bool
	ctx        context.Context
	cancel     context.CancelFunc
	eventChan  chan ReplayEvent
	errChan    chan error
	stats      ReplayStats
}

// NewOHLCVReplayEngine creates a new replay engine
func NewOHLCVReplayEngine(config ReplayConfig) *OHLCVReplayEngine {
	ctx, cancel := context.WithCancel(context.Background())
	return &OHLCVReplayEngine{
		config:    config,
		bars:      make([]OHLCVBar, 0),
		ctx:       ctx,
		cancel:    cancel,
		eventChan: make(chan ReplayEvent, config.BufferSize),
		errChan:   make(chan error, 10),
		stats: ReplayStats{
			StartTime: time.Now(),
		},
	}
}

// LoadBars loads historical bars into the engine
func (r *OHLCVReplayEngine) LoadBars(bars []OHLCVBar) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.isRunning {
		return fmt.Errorf("cannot load bars while replay is running")
	}

	// Sort bars by timestamp
	sortedBars := make([]OHLCVBar, len(bars))
	copy(sortedBars, bars)

	// Simple bubble sort (for small datasets)
	for i := 0; i < len(sortedBars); i++ {
		for j := i + 1; j < len(sortedBars); j++ {
			if sortedBars[i].Timestamp.After(sortedBars[j].Timestamp) {
				sortedBars[i], sortedBars[j] = sortedBars[j], sortedBars[i]
			}
		}
	}

	r.bars = sortedBars
	r.stats.TotalBars = len(sortedBars)

	return nil
}

// LoadFromDB loads bars from database
func (r *OHLCVReplayEngine) LoadFromDB(db DBPool, symbol string, start, end time.Time) error {
	query := `
		SELECT symbol, timestamp, open, high, low, close, volume, exchange
		FROM ohlcv_data
		WHERE symbol = $1 AND timestamp >= $2 AND timestamp <= $3
		ORDER BY timestamp ASC
	`

	rows, err := db.Query(r.ctx, query, symbol, start, end)
	if err != nil {
		return fmt.Errorf("failed to query OHLCV data: %w", err)
	}
	defer rows.Close()

	bars := make([]OHLCVBar, 0)
	for rows.Next() {
		var bar OHLCVBar
		if err := rows.Scan(
			&bar.Symbol,
			&bar.Timestamp,
			&bar.Open,
			&bar.High,
			&bar.Low,
			&bar.Close,
			&bar.Volume,
			&bar.Exchange,
		); err != nil {
			return fmt.Errorf("failed to scan OHLCV row: %w", err)
		}
		bars = append(bars, bar)
	}

	return r.LoadBars(bars)
}

// Start begins the replay
func (r *OHLCVReplayEngine) Start() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.isRunning {
		return fmt.Errorf("replay already running")
	}

	if len(r.bars) == 0 {
		return fmt.Errorf("no bars loaded")
	}

	r.isRunning = true
	r.currentIdx = 0
	r.stats.StartTime = time.Now()
	r.stats.ProcessedBars = 0

	go r.replayLoop()

	return nil
}

// replayLoop runs the replay
func (r *OHLCVReplayEngine) replayLoop() {
	defer func() {
		r.mu.Lock()
		r.isRunning = false
		now := time.Now()
		r.stats.EndTime = &now
		r.stats.Duration = now.Sub(r.stats.StartTime)
		if r.stats.ProcessedBars > 0 {
			r.stats.AvgBarInterval = r.stats.Duration / time.Duration(r.stats.ProcessedBars)
		}
		r.mu.Unlock()
		close(r.eventChan)
	}()

	for {
		select {
		case <-r.ctx.Done():
			return
		default:
		}

		r.mu.Lock()
		if r.currentIdx >= len(r.bars) {
			r.mu.Unlock()
			if r.config.Loop {
				r.mu.Lock()
				r.currentIdx = 0
				r.mu.Unlock()
				continue
			}
			return
		}

		bar := r.bars[r.currentIdx]
		r.currentIdx++
		r.stats.ProcessedBars++
		progress := float64(r.stats.ProcessedBars) / float64(r.stats.TotalBars) * 100
		r.mu.Unlock()

		// Send bar event
		event := ReplayEvent{
			Type:      "bar",
			Bar:       bar,
			Progress:  progress,
			Timestamp: time.Now(),
		}

		select {
		case r.eventChan <- event:
		case <-r.ctx.Done():
			return
		}

		// Calculate sleep duration based on speed multiplier
		if r.currentIdx < len(r.bars) {
			r.mu.RLock()
			nextBar := r.bars[r.currentIdx]
			r.mu.RUnlock()

			interval := nextBar.Timestamp.Sub(bar.Timestamp)
			if r.config.SpeedMultiplier > 0 {
				interval = time.Duration(float64(interval) / r.config.SpeedMultiplier)
			}

			if interval > 0 {
				select {
				case <-time.After(interval):
				case <-r.ctx.Done():
					return
				}
			}
		}
	}
}

// Stop stops the replay
func (r *OHLCVReplayEngine) Stop() {
	r.cancel()
}

// GetEventChannel returns the event channel
func (r *OHLCVReplayEngine) GetEventChannel() <-chan ReplayEvent {
	return r.eventChan
}

// GetStats returns current replay statistics
func (r *OHLCVReplayEngine) GetStats() ReplayStats {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.stats
}

// IsRunning returns true if replay is active
func (r *OHLCVReplayEngine) IsRunning() bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.isRunning
}

// GetProgress returns current progress percentage
func (r *OHLCVReplayEngine) GetProgress() float64 {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if r.stats.TotalBars == 0 {
		return 0
	}
	return float64(r.stats.ProcessedBars) / float64(r.stats.TotalBars) * 100
}

// Seek seeks to a specific bar index
func (r *OHLCVReplayEngine) Seek(index int) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.isRunning {
		return fmt.Errorf("cannot seek while replay is running")
	}

	if index < 0 || index >= len(r.bars) {
		return fmt.Errorf("invalid index: %d", index)
	}

	r.currentIdx = index
	return nil
}

// SeekToTime seeks to the bar closest to the specified time
func (r *OHLCVReplayEngine) SeekToTime(target time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.isRunning {
		return fmt.Errorf("cannot seek while replay is running")
	}

	for i, bar := range r.bars {
		if bar.Timestamp.Equal(target) || bar.Timestamp.After(target) {
			r.currentIdx = i
			return nil
		}
	}

	return fmt.Errorf("time not found in replay data")
}

// ConvertMarketPricesToOHLCV converts market price data to OHLCV bars
func ConvertMarketPricesToOHLCV(prices []models.MarketPrice, timeframe time.Duration) []OHLCVBar {
	if len(prices) == 0 {
		return nil
	}

	bars := make([]OHLCVBar, 0)
	var currentBar *OHLCVBar

	for _, price := range prices {
		ts := price.Timestamp.Truncate(timeframe)

		if currentBar == nil || !currentBar.Timestamp.Equal(ts) {
			if currentBar != nil {
				bars = append(bars, *currentBar)
			}
			currentBar = &OHLCVBar{
				Symbol:    price.Symbol,
				Timestamp: ts,
				Open:      price.Price,
				High:      price.Price,
				Low:       price.Price,
				Close:     price.Price,
				Volume:    price.Volume,
				Exchange:  price.ExchangeName,
			}
		} else {
			// Update bar
			if price.Price.GreaterThan(currentBar.High) {
				currentBar.High = price.Price
			}
			if price.Price.LessThan(currentBar.Low) {
				currentBar.Low = price.Price
			}
			currentBar.Close = price.Price
			currentBar.Volume = currentBar.Volume.Add(price.Volume)
		}
	}

	if currentBar != nil {
		bars = append(bars, *currentBar)
	}

	return bars
}
