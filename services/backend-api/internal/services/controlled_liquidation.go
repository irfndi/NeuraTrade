package services

import (
	"context"
	"crypto/rand"
	"fmt"
	"sync"
	"time"

	"github.com/getsentry/sentry-go"
	"github.com/irfndi/neuratrade/internal/config"
	"github.com/irfndi/neuratrade/internal/logging"
	"github.com/irfndi/neuratrade/internal/observability"
	"github.com/redis/go-redis/v9"
	"github.com/shopspring/decimal"
)

// LiquidationStatus represents the status of a liquidation
type LiquidationStatus string

const (
	LiquidationStatusPending    LiquidationStatus = "pending"
	LiquidationStatusProcessing LiquidationStatus = "processing"
	LiquidationStatusCompleted  LiquidationStatus = "completed"
	LiquidationStatusPartial    LiquidationStatus = "partial"
	LiquidationStatusFailed     LiquidationStatus = "failed"
	LiquidationStatusRejected   LiquidationStatus = "rejected"
)

// LiquidationReason represents why liquidation was triggered
type LiquidationReason string

const (
	LiquidationReasonManual     LiquidationReason = "manual"
	LiquidationReasonKillSwitch LiquidationReason = "kill_switch"
	LiquidationReasonRiskLimit  LiquidationReason = "risk_limit"
	LiquidationReasonStopLoss   LiquidationReason = "stop_loss"
	LiquidationReasonDrawdown   LiquidationReason = "drawdown"
	LiquidationReasonMargin     LiquidationReason = "margin_call"
	LiquidationReasonEmergency  LiquidationReason = "emergency"
)

// LiquidationRequest represents a request to liquidate a position
type LiquidationRequest struct {
	PositionID          string            `json:"position_id"`
	Symbol              string            `json:"symbol"`
	Exchange            string            `json:"exchange"`
	Percentage          decimal.Decimal   `json:"percentage"`
	Reason              LiquidationReason `json:"reason"`
	TriggeredBy         string            `json:"triggered_by"`
	MaxSlippage         decimal.Decimal   `json:"max_slippage"`
	RequireConfirmation bool              `json:"require_confirmation"`
	TimeLimit           time.Duration     `json:"time_limit"`
}

// LiquidationResult represents the result of a liquidation
type LiquidationResult struct {
	ID                 string            `json:"id"`
	RequestID          string            `json:"request_id"`
	PositionID         string            `json:"position_id"`
	Symbol             string            `json:"symbol"`
	Exchange           string            `json:"exchange"`
	OriginalSize       decimal.Decimal   `json:"original_size"`
	LiquidatedSize     decimal.Decimal   `json:"liquidated_size"`
	RemainingSize      decimal.Decimal   `json:"remaining_size"`
	EntryPrice         decimal.Decimal   `json:"entry_price"`
	ExitPrice          decimal.Decimal   `json:"exit_price"`
	GrossPnL           decimal.Decimal   `json:"gross_pnl"`
	Fees               decimal.Decimal   `json:"fees"`
	NetPnL             decimal.Decimal   `json:"net_pnl"`
	Status             LiquidationStatus `json:"status"`
	Reason             LiquidationReason `json:"reason"`
	TriggeredBy        string            `json:"triggered_by"`
	StartedAt          time.Time         `json:"started_at"`
	CompletedAt        *time.Time        `json:"completed_at,omitempty"`
	ExecutionTimeMs    int64             `json:"execution_time_ms"`
	Slippage           decimal.Decimal   `json:"slippage"`
	RejectionReason    string            `json:"rejection_reason,omitempty"`
	ValidationWarnings []string          `json:"validation_warnings,omitempty"`
}

// LiquidationLimits defines limits for controlled liquidation
type LiquidationLimits struct {
	MaxPositionSize      decimal.Decimal `json:"max_position_size"`
	MaxDailyLoss         decimal.Decimal `json:"max_daily_loss"`
	MaxDailyLiquidations int             `json:"max_daily_liquidations"`
	MinTimeBetween       time.Duration   `json:"min_time_between"`
	MaxSlippageAllowed   decimal.Decimal `json:"max_slippage_allowed"`
	RequireKillSwitchOff bool            `json:"require_kill_switch_off"`
}

// PositionSnapshot represents a snapshot of position state
type PositionSnapshot struct {
	PositionID       string          `json:"position_id"`
	Symbol           string          `json:"symbol"`
	Exchange         string          `json:"exchange"`
	Side             string          `json:"side"`
	Size             decimal.Decimal `json:"size"`
	EntryPrice       decimal.Decimal `json:"entry_price"`
	MarkPrice        decimal.Decimal `json:"mark_price"`
	UnrealizedPnL    decimal.Decimal `json:"unrealized_pnl"`
	Leverage         decimal.Decimal `json:"leverage"`
	Margin           decimal.Decimal `json:"margin"`
	LiquidationPrice decimal.Decimal `json:"liquidation_price"`
	SnapshotAt       time.Time       `json:"snapshot_at"`
}

// ControlledLiquidationService provides controlled liquidation with risk validation
type ControlledLiquidationService struct {
	mu           sync.RWMutex
	db           DBPool
	redis        *redis.Client
	config       *config.Config
	killSwitch   *KillSwitchMonitor
	logger       logging.Logger
	ctx          context.Context
	cancel       context.CancelFunc
	wg           sync.WaitGroup
	running      bool
	limits       LiquidationLimits
	pendingQueue []*LiquidationRequest
	results      map[string]*LiquidationResult
	dailyStats   *liquidationDailyStats
}

type liquidationDailyStats struct {
	mu           sync.RWMutex
	date         time.Time
	totalCount   int
	totalLoss    decimal.Decimal
	liquidations []string
}

// ControlledLiquidationConfig holds configuration for the service
type ControlledLiquidationConfig struct {
	Limits LiquidationLimits
}

// NewControlledLiquidationService creates a new controlled liquidation service
func NewControlledLiquidationService(
	db DBPool,
	redisClient *redis.Client,
	cfg *config.Config,
	killSwitch *KillSwitchMonitor,
	customConfig *ControlledLiquidationConfig,
	logger any,
) *ControlledLiquidationService {
	serviceLogger, ok := logger.(logging.Logger)
	if !ok || serviceLogger == nil {
		serviceLogger = logging.NewStandardLogger("info", "production")
	}

	limits := LiquidationLimits{
		MaxPositionSize:      decimal.NewFromInt(100000),
		MaxDailyLoss:         decimal.NewFromFloat(0.05),
		MaxDailyLiquidations: 50,
		MinTimeBetween:       1 * time.Minute,
		MaxSlippageAllowed:   decimal.NewFromFloat(0.02),
		RequireKillSwitchOff: true,
	}

	if customConfig != nil {
		if customConfig.Limits.MaxPositionSize.GreaterThan(decimal.Zero) {
			limits.MaxPositionSize = customConfig.Limits.MaxPositionSize
		}
		if customConfig.Limits.MaxDailyLoss.GreaterThan(decimal.Zero) {
			limits.MaxDailyLoss = customConfig.Limits.MaxDailyLoss
		}
		if customConfig.Limits.MaxDailyLiquidations > 0 {
			limits.MaxDailyLiquidations = customConfig.Limits.MaxDailyLiquidations
		}
		if customConfig.Limits.MinTimeBetween > 0 {
			limits.MinTimeBetween = customConfig.Limits.MinTimeBetween
		}
		if customConfig.Limits.MaxSlippageAllowed.GreaterThan(decimal.Zero) {
			limits.MaxSlippageAllowed = customConfig.Limits.MaxSlippageAllowed
		}
		limits.RequireKillSwitchOff = customConfig.Limits.RequireKillSwitchOff
	}

	service := &ControlledLiquidationService{
		db:           db,
		redis:        redisClient,
		config:       cfg,
		killSwitch:   killSwitch,
		logger:       serviceLogger,
		limits:       limits,
		pendingQueue: make([]*LiquidationRequest, 0),
		results:      make(map[string]*LiquidationResult),
		dailyStats: &liquidationDailyStats{
			date:         time.Now(),
			totalLoss:    decimal.Zero,
			liquidations: make([]string, 0),
		},
	}

	return service
}

// Start begins the controlled liquidation service
func (s *ControlledLiquidationService) Start() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.running {
		return fmt.Errorf("controlled liquidation service is already running")
	}

	s.ctx, s.cancel = context.WithCancel(context.Background())
	s.running = true

	s.wg.Add(1)
	go s.processQueue()

	s.logger.Info("Controlled liquidation service started")
	observability.AddBreadcrumb(s.ctx, "controlled_liquidation", "Controlled liquidation service started", sentry.LevelInfo)
	return nil
}

// Stop gracefully stops the service
func (s *ControlledLiquidationService) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.running {
		return
	}

	s.cancel()
	s.wg.Wait()
	s.running = false

	s.logger.Info("Controlled liquidation service stopped")
}

func (s *ControlledLiquidationService) processQueue() {
	defer s.wg.Done()

	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-s.ctx.Done():
			return
		case <-ticker.C:
			s.processPendingRequests()
		}
	}
}

func (s *ControlledLiquidationService) processPendingRequests() {
	s.mu.Lock()
	if len(s.pendingQueue) == 0 {
		s.mu.Unlock()
		return
	}

	request := s.pendingQueue[0]
	s.pendingQueue = s.pendingQueue[1:]
	s.mu.Unlock()

	result := s.executeLiquidation(request)

	s.mu.Lock()
	s.results[result.ID] = result
	s.mu.Unlock()
}

// RequestLiquidation queues a liquidation request for processing
func (s *ControlledLiquidationService) RequestLiquidation(req *LiquidationRequest) (*LiquidationResult, error) {
	if err := s.validateRequest(req); err != nil {
		return nil, err
	}

	result := &LiquidationResult{
		ID:          generateLiquidationID(),
		RequestID:   generateLiquidationID(),
		PositionID:  req.PositionID,
		Symbol:      req.Symbol,
		Exchange:    req.Exchange,
		Status:      LiquidationStatusPending,
		Reason:      req.Reason,
		TriggeredBy: req.TriggeredBy,
		StartedAt:   time.Now(),
	}

	warnings, err := s.validateRiskLimits(req)
	if err != nil {
		result.Status = LiquidationStatusRejected
		result.RejectionReason = err.Error()
		result.CompletedAt = &result.StartedAt
		s.mu.Lock()
		s.results[result.ID] = result
		s.mu.Unlock()
		return result, nil
	}
	result.ValidationWarnings = warnings

	s.mu.Lock()
	s.pendingQueue = append(s.pendingQueue, req)
	s.results[result.ID] = result
	s.mu.Unlock()

	s.logger.WithFields(map[string]interface{}{
		"liquidation_id": result.ID,
		"position_id":    req.PositionID,
		"symbol":         req.Symbol,
		"percentage":     req.Percentage.String(),
	}).Info("Liquidation request queued")

	return result, nil
}

func (s *ControlledLiquidationService) validateRequest(req *LiquidationRequest) error {
	if req.PositionID == "" && req.Symbol == "" {
		return fmt.Errorf("position_id or symbol is required")
	}

	if req.Percentage.IsZero() {
		req.Percentage = decimal.NewFromInt(100)
	}

	if req.Percentage.LessThanOrEqual(decimal.Zero) || req.Percentage.GreaterThan(decimal.NewFromInt(100)) {
		return fmt.Errorf("percentage must be between 0 and 100")
	}

	if req.MaxSlippage.IsZero() {
		req.MaxSlippage = s.limits.MaxSlippageAllowed
	}

	if req.TimeLimit == 0 {
		req.TimeLimit = 30 * time.Second
	}

	return nil
}

func (s *ControlledLiquidationService) validateRiskLimits(req *LiquidationRequest) ([]string, error) {
	var warnings []string

	if s.killSwitch != nil && s.limits.RequireKillSwitchOff {
		if !s.killSwitch.IsTradingAllowed() {
			return warnings, fmt.Errorf("kill switch is active, liquidation not allowed")
		}
	}

	s.dailyStats.mu.RLock()
	today := time.Now().Format("2006-01-02")
	if s.dailyStats.date.Format("2006-01-02") != today {
		s.dailyStats.mu.RUnlock()
		s.dailyStats.mu.Lock()
		s.dailyStats.date = time.Now()
		s.dailyStats.totalCount = 0
		s.dailyStats.totalLoss = decimal.Zero
		s.dailyStats.liquidations = make([]string, 0)
		s.dailyStats.mu.Unlock()
	} else {
		s.dailyStats.mu.RUnlock()
	}

	s.dailyStats.mu.RLock()
	if s.dailyStats.totalCount >= s.limits.MaxDailyLiquidations {
		s.dailyStats.mu.RUnlock()
		return warnings, fmt.Errorf("daily liquidation limit reached (%d)", s.limits.MaxDailyLiquidations)
	}
	s.dailyStats.mu.RUnlock()

	if req.Reason == LiquidationReasonManual {
		warnings = append(warnings, "manual liquidation - ensure this is intended action")
	}

	return warnings, nil
}

func (s *ControlledLiquidationService) executeLiquidation(req *LiquidationRequest) *LiquidationResult {
	startTime := time.Now()

	result := &LiquidationResult{
		ID:          generateLiquidationID(),
		RequestID:   generateLiquidationID(),
		PositionID:  req.PositionID,
		Symbol:      req.Symbol,
		Exchange:    req.Exchange,
		Status:      LiquidationStatusProcessing,
		Reason:      req.Reason,
		TriggeredBy: req.TriggeredBy,
		StartedAt:   startTime,
	}

	snapshot, err := s.getPositionSnapshot(req.PositionID, req.Symbol, req.Exchange)
	if err != nil {
		result.Status = LiquidationStatusFailed
		result.RejectionReason = fmt.Sprintf("failed to get position snapshot: %v", err)
		now := time.Now()
		result.CompletedAt = &now
		result.ExecutionTimeMs = time.Since(startTime).Milliseconds()
		return result
	}

	result.OriginalSize = snapshot.Size
	result.EntryPrice = snapshot.EntryPrice

	liquidatedSize := snapshot.Size.Mul(req.Percentage).Div(decimal.NewFromInt(100))
	result.LiquidatedSize = liquidatedSize
	result.RemainingSize = snapshot.Size.Sub(liquidatedSize)

	exitPrice := snapshot.MarkPrice
	slippage := decimal.Zero

	if snapshot.EntryPrice.GreaterThan(decimal.Zero) {
		priceDiff := exitPrice.Sub(snapshot.EntryPrice).Abs()
		slippage = priceDiff.Div(snapshot.EntryPrice)
	}

	if slippage.GreaterThan(req.MaxSlippage) {
		warnings := fmt.Sprintf("slippage %.4f%% exceeds max %.4f%%",
			slippage.Mul(decimal.NewFromInt(100)).InexactFloat64(),
			req.MaxSlippage.Mul(decimal.NewFromInt(100)).InexactFloat64())

		if req.RequireConfirmation {
			result.Status = LiquidationStatusRejected
			result.RejectionReason = warnings
			now := time.Now()
			result.CompletedAt = &now
			result.ExecutionTimeMs = time.Since(startTime).Milliseconds()
			return result
		}
		result.ValidationWarnings = append(result.ValidationWarnings, warnings)
	}

	result.ExitPrice = exitPrice
	result.Slippage = slippage

	if snapshot.Side == "BUY" || snapshot.Side == "LONG" {
		pnl := exitPrice.Sub(snapshot.EntryPrice).Mul(liquidatedSize)
		result.GrossPnL = pnl
	} else {
		pnl := snapshot.EntryPrice.Sub(exitPrice).Mul(liquidatedSize)
		result.GrossPnL = pnl
	}

	feeRate := decimal.NewFromFloat(0.001)
	result.Fees = liquidatedSize.Mul(exitPrice).Mul(feeRate)
	result.NetPnL = result.GrossPnL.Sub(result.Fees)

	now := time.Now()
	result.CompletedAt = &now
	result.ExecutionTimeMs = time.Since(startTime).Milliseconds()

	if req.Percentage.Equal(decimal.NewFromInt(100)) {
		result.Status = LiquidationStatusCompleted
	} else {
		result.Status = LiquidationStatusPartial
	}

	s.updateDailyStats(result)

	s.logger.WithFields(map[string]interface{}{
		"liquidation_id":    result.ID,
		"position_id":       result.PositionID,
		"liquidated_size":   result.LiquidatedSize.String(),
		"net_pnl":           result.NetPnL.String(),
		"execution_time_ms": result.ExecutionTimeMs,
		"status":            result.Status,
	}).Info("Liquidation executed")

	return result
}

func (s *ControlledLiquidationService) getPositionSnapshot(positionID, symbol, exchange string) (*PositionSnapshot, error) {
	if isNilDBPool(s.db) {
		return nil, fmt.Errorf("database pool is not available")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var query string
	var args []interface{}

	if positionID != "" {
		query = `
			SELECT position_id, symbol, exchange, side, size, entry_price, 
				   COALESCE(mark_price, entry_price) as mark_price,
				   COALESCE(unrealized_pnl, 0) as unrealized_pnl,
				   COALESCE(leverage, 1) as leverage,
				   COALESCE(margin, size * entry_price) as margin,
				   COALESCE(liquidation_price, 0) as liquidation_price,
				   NOW() as snapshot_at
			FROM trading_positions
			WHERE position_id = $1 AND status = 'OPEN'
		`
		args = []interface{}{positionID}
	} else {
		query = `
			SELECT position_id, symbol, exchange, side, size, entry_price, 
				   COALESCE(mark_price, entry_price) as mark_price,
				   COALESCE(unrealized_pnl, 0) as unrealized_pnl,
				   COALESCE(leverage, 1) as leverage,
				   COALESCE(margin, size * entry_price) as margin,
				   COALESCE(liquidation_price, 0) as liquidation_price,
				   NOW() as snapshot_at
			FROM trading_positions
			WHERE symbol = $1 AND exchange = $2 AND status = 'OPEN'
			LIMIT 1
		`
		args = []interface{}{symbol, exchange}
	}

	row := s.db.QueryRow(ctx, query, args...)

	snapshot := &PositionSnapshot{}
	err := row.Scan(
		&snapshot.PositionID, &snapshot.Symbol, &snapshot.Exchange, &snapshot.Side,
		&snapshot.Size, &snapshot.EntryPrice, &snapshot.MarkPrice, &snapshot.UnrealizedPnL,
		&snapshot.Leverage, &snapshot.Margin, &snapshot.LiquidationPrice, &snapshot.SnapshotAt,
	)

	if err != nil {
		return nil, fmt.Errorf("position not found: %w", err)
	}

	return snapshot, nil
}

func (s *ControlledLiquidationService) updateDailyStats(result *LiquidationResult) {
	s.dailyStats.mu.Lock()
	defer s.dailyStats.mu.Unlock()

	today := time.Now().Format("2006-01-02")
	if s.dailyStats.date.Format("2006-01-02") != today {
		s.dailyStats.date = time.Now()
		s.dailyStats.totalCount = 0
		s.dailyStats.totalLoss = decimal.Zero
		s.dailyStats.liquidations = make([]string, 0)
	}

	s.dailyStats.totalCount++
	if result.NetPnL.IsNegative() {
		s.dailyStats.totalLoss = s.dailyStats.totalLoss.Add(result.NetPnL.Abs())
	}
	s.dailyStats.liquidations = append(s.dailyStats.liquidations, result.ID)
}

// GetPositionSnapshot returns a snapshot of a position
func (s *ControlledLiquidationService) GetPositionSnapshot(positionID string) (*PositionSnapshot, error) {
	return s.getPositionSnapshot(positionID, "", "")
}

// GetLiquidationResult retrieves a liquidation result by ID
func (s *ControlledLiquidationService) GetLiquidationResult(id string) (*LiquidationResult, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	result, ok := s.results[id]
	if !ok {
		return nil, fmt.Errorf("liquidation result not found: %s", id)
	}
	return result, nil
}

// GetPendingCount returns the number of pending liquidations
func (s *ControlledLiquidationService) GetPendingCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.pendingQueue)
}

// GetDailyStats returns today's liquidation statistics
func (s *ControlledLiquidationService) GetDailyStats() (int, decimal.Decimal) {
	s.dailyStats.mu.RLock()
	defer s.dailyStats.mu.RUnlock()
	return s.dailyStats.totalCount, s.dailyStats.totalLoss
}

// GetLimits returns the current liquidation limits
func (s *ControlledLiquidationService) GetLimits() LiquidationLimits {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.limits
}

// SetLimits updates the liquidation limits
func (s *ControlledLiquidationService) SetLimits(limits LiquidationLimits) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.limits = limits
}

// IsRunning returns whether the service is running
func (s *ControlledLiquidationService) IsRunning() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.running
}

func generateLiquidationID() string {
	return fmt.Sprintf("liq-%d-%s", time.Now().UnixNano(), randomString(6))
}

func randomString(n int) string {
	const letters = "abcdefghijklmnopqrstuvwxyz0123456789"
	b := make([]byte, n)
	rng := rand.Reader
	for i := range b {
		randByte := make([]byte, 1)
		if _, err := rng.Read(randByte); err != nil {
			randByte[0] = byte(i)
		}
		b[i] = letters[int(randByte[0])%len(letters)]
	}
	return string(b)
}
