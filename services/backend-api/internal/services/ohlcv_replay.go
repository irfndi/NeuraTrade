package services

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/irfndi/neuratrade/internal/ccxt"
	"github.com/irfndi/neuratrade/internal/database"
	"github.com/shopspring/decimal"
)

type ReplayStatus string

const (
	ReplayStatusIdle     ReplayStatus = "idle"
	ReplayStatusPlaying  ReplayStatus = "playing"
	ReplayStatusPaused   ReplayStatus = "paused"
	ReplayStatusComplete ReplayStatus = "complete"
	ReplayStatusError    ReplayStatus = "error"
)

type ReplayConfig struct {
	Symbol     string          `json:"symbol"`
	Exchange   string          `json:"exchange"`
	Timeframe  string          `json:"timeframe"`
	StartTime  time.Time       `json:"start_time"`
	EndTime    time.Time       `json:"end_time"`
	Speed      float64         `json:"speed"`
	OnCandle   CandleHandler   `json:"-"`
	OnError    ErrorHandler    `json:"-"`
	OnComplete CompleteHandler `json:"-"`
}

type CandleHandler func(ctx context.Context, candle *ReplayCandle) error
type ErrorHandler func(err error)
type CompleteHandler func(summary *ReplaySummary)

type ReplayCandle struct {
	Timestamp time.Time       `json:"timestamp"`
	Open      decimal.Decimal `json:"open"`
	High      decimal.Decimal `json:"high"`
	Low       decimal.Decimal `json:"low"`
	Close     decimal.Decimal `json:"close"`
	Volume    decimal.Decimal `json:"volume"`
	Index     int             `json:"index"`
	Total     int             `json:"total"`
	Progress  float64         `json:"progress"`
}

type ReplaySummary struct {
	Symbol         string          `json:"symbol"`
	Exchange       string          `json:"exchange"`
	Timeframe      string          `json:"timeframe"`
	StartTime      time.Time       `json:"start_time"`
	EndTime        time.Time       `json:"end_time"`
	TotalCandles   int             `json:"total_candles"`
	Duration       time.Duration   `json:"duration"`
	CandlesPlayed  int             `json:"candles_played"`
	FirstPrice     decimal.Decimal `json:"first_price"`
	LastPrice      decimal.Decimal `json:"last_price"`
	PriceChange    decimal.Decimal `json:"price_change"`
	PriceChangePct decimal.Decimal `json:"price_change_pct"`
	HighestPrice   decimal.Decimal `json:"highest_price"`
	LowestPrice    decimal.Decimal `json:"lowest_price"`
	TotalVolume    decimal.Decimal `json:"total_volume"`
	AvgVolume      decimal.Decimal `json:"avg_volume"`
	Status         ReplayStatus    `json:"status"`
	ErrorMessage   string          `json:"error_message,omitempty"`
}

type OHLCVReplayEngine struct {
	db           *database.PostgresDB
	ccxtService  ccxt.CCXTService
	config       ReplayConfig
	status       ReplayStatus
	candles      []ReplayCandle
	currentIndex int
	startTime    time.Time
	mu           sync.RWMutex
	cancel       context.CancelFunc
}

func NewOHLCVReplayEngine(db *database.PostgresDB, ccxtService ccxt.CCXTService) *OHLCVReplayEngine {
	return &OHLCVReplayEngine{
		db:          db,
		ccxtService: ccxtService,
		status:      ReplayStatusIdle,
	}
}

func (e *OHLCVReplayEngine) Configure(config ReplayConfig) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.config = config
}

func (e *OHLCVReplayEngine) Load(ctx context.Context) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	candles, err := e.fetchHistoricalCandles(ctx, e.config)
	if err != nil {
		e.status = ReplayStatusError
		return fmt.Errorf("failed to load historical candles: %w", err)
	}

	if len(candles) == 0 {
		e.status = ReplayStatusError
		return fmt.Errorf("no candles found for %s %s from %s to %s",
			e.config.Exchange, e.config.Symbol,
			e.config.StartTime.Format(time.RFC3339),
			e.config.EndTime.Format(time.RFC3339))
	}

	e.candles = candles
	e.currentIndex = 0
	e.status = ReplayStatusIdle

	return nil
}

func (e *OHLCVReplayEngine) Play(ctx context.Context) error {
	e.mu.Lock()
	if e.status == ReplayStatusPlaying {
		e.mu.Unlock()
		return fmt.Errorf("replay already playing")
	}
	if len(e.candles) == 0 {
		e.mu.Unlock()
		return fmt.Errorf("no candles loaded, call Load() first")
	}
	e.status = ReplayStatusPlaying
	e.startTime = time.Now()
	e.mu.Unlock()

	replayCtx, cancel := context.WithCancel(ctx)
	e.mu.Lock()
	e.cancel = cancel
	e.mu.Unlock()

	defer func() {
		e.mu.Lock()
		e.status = ReplayStatusComplete
		e.mu.Unlock()
		if e.config.OnComplete != nil {
			e.config.OnComplete(e.GetSummary())
		}
	}()

	for {
		select {
		case <-replayCtx.Done():
			return replayCtx.Err()
		default:
			e.mu.Lock()
			if e.currentIndex >= len(e.candles) {
				e.mu.Unlock()
				return nil
			}
			candle := e.candles[e.currentIndex]
			status := e.status
			e.mu.Unlock()

			if status == ReplayStatusPaused {
				time.Sleep(100 * time.Millisecond)
				continue
			}

			if e.config.OnCandle != nil {
				if err := e.config.OnCandle(replayCtx, &candle); err != nil {
					e.mu.Lock()
					e.status = ReplayStatusError
					e.mu.Unlock()
					if e.config.OnError != nil {
						e.config.OnError(err)
					}
					return err
				}
			}

			e.mu.Lock()
			e.currentIndex++
			e.mu.Unlock()

			delay := e.calculateDelay()
			select {
			case <-time.After(delay):
			case <-replayCtx.Done():
				return replayCtx.Err()
			}
		}
	}
}

func (e *OHLCVReplayEngine) Pause() error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.status != ReplayStatusPlaying {
		return fmt.Errorf("cannot pause: current status is %s", e.status)
	}
	e.status = ReplayStatusPaused
	return nil
}

func (e *OHLCVReplayEngine) Resume() error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.status != ReplayStatusPaused {
		return fmt.Errorf("cannot resume: current status is %s", e.status)
	}
	e.status = ReplayStatusPlaying
	return nil
}

func (e *OHLCVReplayEngine) Stop() error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.cancel != nil {
		e.cancel()
		e.cancel = nil
	}
	e.status = ReplayStatusComplete
	e.currentIndex = 0
	return nil
}

func (e *OHLCVReplayEngine) Seek(index int) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if index < 0 || index >= len(e.candles) {
		return fmt.Errorf("invalid index %d (valid range: 0-%d)", index, len(e.candles)-1)
	}
	e.currentIndex = index
	return nil
}

func (e *OHLCVReplayEngine) SeekToTime(t time.Time) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	for i, c := range e.candles {
		if c.Timestamp.Equal(t) || c.Timestamp.After(t) {
			e.currentIndex = i
			return nil
		}
	}
	return fmt.Errorf("timestamp %s not found in replay range", t.Format(time.RFC3339))
}

func (e *OHLCVReplayEngine) GetStatus() ReplayStatus {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.status
}

func (e *OHLCVReplayEngine) GetProgress() float64 {
	e.mu.RLock()
	defer e.mu.RUnlock()

	if len(e.candles) == 0 {
		return 0
	}
	return float64(e.currentIndex) / float64(len(e.candles)) * 100
}

func (e *OHLCVReplayEngine) GetCurrentCandle() *ReplayCandle {
	e.mu.RLock()
	defer e.mu.RUnlock()

	if e.currentIndex >= len(e.candles) {
		return nil
	}
	candle := e.candles[e.currentIndex]
	return &candle
}

func (e *OHLCVReplayEngine) GetSummary() *ReplaySummary {
	e.mu.RLock()
	defer e.mu.RUnlock()

	summary := &ReplaySummary{
		Symbol:        e.config.Symbol,
		Exchange:      e.config.Exchange,
		Timeframe:     e.config.Timeframe,
		StartTime:     e.config.StartTime,
		EndTime:       e.config.EndTime,
		TotalCandles:  len(e.candles),
		CandlesPlayed: e.currentIndex,
		Status:        e.status,
	}

	if len(e.candles) == 0 {
		return summary
	}

	summary.FirstPrice = e.candles[0].Open
	summary.LastPrice = e.candles[len(e.candles)-1].Close
	summary.PriceChange = summary.LastPrice.Sub(summary.FirstPrice)
	if !summary.FirstPrice.IsZero() {
		summary.PriceChangePct = summary.PriceChange.Div(summary.FirstPrice).Mul(decimal.NewFromInt(100))
	}

	totalVolume := decimal.Zero
	highest := e.candles[0].High
	lowest := e.candles[0].Low

	for _, c := range e.candles {
		totalVolume = totalVolume.Add(c.Volume)
		if c.High.GreaterThan(highest) {
			highest = c.High
		}
		if c.Low.LessThan(lowest) {
			lowest = c.Low
		}
	}

	summary.HighestPrice = highest
	summary.LowestPrice = lowest
	summary.TotalVolume = totalVolume
	if len(e.candles) > 0 {
		summary.AvgVolume = totalVolume.Div(decimal.NewFromInt(int64(len(e.candles))))
	}

	if !e.startTime.IsZero() {
		summary.Duration = time.Since(e.startTime)
	}

	return summary
}

func (e *OHLCVReplayEngine) SetSpeed(speed float64) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.config.Speed = speed
}

func (e *OHLCVReplayEngine) fetchHistoricalCandles(ctx context.Context, config ReplayConfig) ([]ReplayCandle, error) {
	var candles []ReplayCandle

	dbCandles, err := e.fetchFromDatabase(ctx, config)
	if err == nil && len(dbCandles) > 0 {
		candles = dbCandles
	} else {
		apiCandles, err := e.fetchFromAPI(ctx, config)
		if err != nil {
			return nil, fmt.Errorf("failed to fetch from both database and API: %w", err)
		}
		candles = apiCandles
	}

	for i := range candles {
		candles[i].Index = i
		candles[i].Total = len(candles)
		candles[i].Progress = float64(i+1) / float64(len(candles)) * 100
	}

	return candles, nil
}

func (e *OHLCVReplayEngine) fetchFromDatabase(ctx context.Context, config ReplayConfig) ([]ReplayCandle, error) {
	if e.db == nil {
		return nil, fmt.Errorf("database not configured")
	}

	query := `
		SELECT timestamp, open, high, low, close, volume
		FROM ohlcv_data
		WHERE symbol = $1 AND exchange_id = $2 AND timeframe = $3
			AND timestamp >= $4 AND timestamp <= $5
		ORDER BY timestamp ASC
	`

	rows, err := e.db.Pool.Query(ctx, query,
		config.Symbol, config.Exchange, config.Timeframe,
		config.StartTime, config.EndTime)
	if err != nil {
		return nil, fmt.Errorf("database query failed: %w", err)
	}
	defer rows.Close()

	var candles []ReplayCandle
	for rows.Next() {
		var c ReplayCandle
		var open, high, low, close, volume float64
		var timestamp time.Time

		if err := rows.Scan(&timestamp, &open, &high, &low, &close, &volume); err != nil {
			return nil, fmt.Errorf("failed to scan OHLCV row: %w", err)
		}

		c.Timestamp = timestamp
		c.Open = decimal.NewFromFloat(open)
		c.High = decimal.NewFromFloat(high)
		c.Low = decimal.NewFromFloat(low)
		c.Close = decimal.NewFromFloat(close)
		c.Volume = decimal.NewFromFloat(volume)

		candles = append(candles, c)
	}

	if len(candles) == 0 {
		return nil, fmt.Errorf("no candles found in database")
	}

	return candles, nil
}

func (e *OHLCVReplayEngine) fetchFromAPI(ctx context.Context, config ReplayConfig) ([]ReplayCandle, error) {
	if e.ccxtService == nil {
		return nil, fmt.Errorf("CCXT service not configured")
	}

	limit := int(config.EndTime.Sub(config.StartTime).Hours() / e.timeframeToHours(config.Timeframe))
	if limit > 1000 {
		limit = 1000
	}
	if limit < 1 {
		limit = 100
	}

	resp, err := e.ccxtService.FetchOHLCV(ctx, config.Exchange, config.Symbol, config.Timeframe, limit)
	if err != nil {
		return nil, fmt.Errorf("CCXT API call failed: %w", err)
	}

	var candles []ReplayCandle
	for _, ohlcv := range resp.OHLCV {
		if ohlcv.Timestamp.Before(config.StartTime) || ohlcv.Timestamp.After(config.EndTime) {
			continue
		}

		candles = append(candles, ReplayCandle{
			Timestamp: ohlcv.Timestamp,
			Open:      ohlcv.Open,
			High:      ohlcv.High,
			Low:       ohlcv.Low,
			Close:     ohlcv.Close,
			Volume:    ohlcv.Volume,
		})
	}

	if len(candles) == 0 {
		return nil, fmt.Errorf("no candles returned from API in time range")
	}

	return candles, nil
}

func (e *OHLCVReplayEngine) calculateDelay() time.Duration {
	if e.config.Speed <= 0 {
		return 0
	}

	interval := e.timeframeToDuration(e.config.Timeframe)
	delay := time.Duration(float64(interval) / e.config.Speed)

	if delay > 5*time.Second {
		delay = 5 * time.Second
	}
	if delay < 10*time.Millisecond {
		delay = 10 * time.Millisecond
	}

	return delay
}

func (e *OHLCVReplayEngine) timeframeToDuration(timeframe string) time.Duration {
	switch timeframe {
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
	case "1w":
		return 7 * 24 * time.Hour
	default:
		return time.Hour
	}
}

func (e *OHLCVReplayEngine) timeframeToHours(timeframe string) float64 {
	return float64(e.timeframeToDuration(timeframe).Hours())
}
