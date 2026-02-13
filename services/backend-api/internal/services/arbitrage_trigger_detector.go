package services

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/getsentry/sentry-go"
	"github.com/irfndi/neuratrade/internal/config"
	"github.com/irfndi/neuratrade/internal/logging"
	"github.com/irfndi/neuratrade/internal/models"
	"github.com/irfndi/neuratrade/internal/observability"
	"github.com/redis/go-redis/v9"
	"github.com/shopspring/decimal"
)

// ArbitrageTriggerType defines the type of arbitrage trigger
type ArbitrageTriggerType string

const (
	TriggerTypeFundingRate ArbitrageTriggerType = "funding_rate"
	TriggerTypePriceGap    ArbitrageTriggerType = "price_gap"
	TriggerTypeBasis       ArbitrageTriggerType = "basis"
	TriggerTypeCross       ArbitrageTriggerType = "cross_exchange"
)

// TriggerStatus represents the status of a trigger
type TriggerStatus string

const (
	TriggerStatusPending   TriggerStatus = "pending"
	TriggerStatusTriggered TriggerStatus = "triggered"
	TriggerStatusExpired   TriggerStatus = "expired"
	TriggerStatusExecuted  TriggerStatus = "executed"
)

// ArbitrageTrigger represents a detected arbitrage trigger
type ArbitrageTrigger struct {
	ID              string               `json:"id"`
	Type            ArbitrageTriggerType `json:"type"`
	Symbol          string               `json:"symbol"`
	LongExchange    string               `json:"long_exchange"`
	ShortExchange   string               `json:"short_exchange"`
	NetFundingRate  decimal.Decimal      `json:"net_funding_rate"`
	ProfitPotential decimal.Decimal      `json:"profit_potential"`
	APY             decimal.Decimal      `json:"apy"`
	RiskScore       float64              `json:"risk_score"`
	Status          TriggerStatus        `json:"status"`
	TriggeredAt     time.Time            `json:"triggered_at"`
	ExpiresAt       time.Time            `json:"expires_at"`
	ExecutedAt      *time.Time           `json:"executed_at,omitempty"`
	Confidence      float64              `json:"confidence"`
	Reason          string               `json:"reason"`
	Metadata        map[string]string    `json:"metadata,omitempty"`
}

// TriggerRule defines a rule for trigger detection
type TriggerRule struct {
	ID                string               `json:"id"`
	Name              string               `json:"name"`
	Type              ArbitrageTriggerType `json:"type"`
	MinProfitAPY      decimal.Decimal      `json:"min_profit_apy"`
	MaxRiskScore      float64              `json:"max_risk_score"`
	MinConfidence     float64              `json:"min_confidence"`
	MinVolume         decimal.Decimal      `json:"min_volume"`
	ExcludedPairs     []string             `json:"excluded_pairs,omitempty"`
	ExcludedExchanges []string             `json:"excluded_exchanges,omitempty"`
	IsActive          bool                 `json:"is_active"`
}

// TriggerCallback is called when a trigger is detected
type TriggerCallback func(ctx context.Context, trigger *ArbitrageTrigger) error

// ArbitrageTriggerDetector detects and manages arbitrage triggers
type ArbitrageTriggerDetector struct {
	mu            sync.RWMutex
	db            DBPool
	redis         *redis.Client
	config        *config.Config
	rules         map[string]*TriggerRule
	callbacks     []TriggerCallback
	logger        logging.Logger
	ctx           context.Context
	cancel        context.CancelFunc
	wg            sync.WaitGroup
	running       bool
	checkInterval time.Duration
	triggerTTL    time.Duration
}

// ArbitrageTriggerDetectorConfig holds configuration for the trigger detector
type ArbitrageTriggerDetectorConfig struct {
	CheckInterval time.Duration
	TriggerTTL    time.Duration
}

// NewArbitrageTriggerDetector creates a new arbitrage trigger detector
func NewArbitrageTriggerDetector(
	db DBPool,
	redisClient *redis.Client,
	cfg *config.Config,
	customConfig *ArbitrageTriggerDetectorConfig,
	logger any,
) *ArbitrageTriggerDetector {
	serviceLogger, ok := logger.(logging.Logger)
	if !ok || serviceLogger == nil {
		serviceLogger = logging.NewStandardLogger("info", "production")
	}

	checkInterval := 15 * time.Second
	triggerTTL := 5 * time.Minute

	if customConfig != nil {
		if customConfig.CheckInterval > 0 {
			checkInterval = customConfig.CheckInterval
		}
		if customConfig.TriggerTTL > 0 {
			triggerTTL = customConfig.TriggerTTL
		}
	}

	detector := &ArbitrageTriggerDetector{
		db:            db,
		redis:         redisClient,
		config:        cfg,
		rules:         make(map[string]*TriggerRule),
		callbacks:     make([]TriggerCallback, 0),
		logger:        serviceLogger,
		checkInterval: checkInterval,
		triggerTTL:    triggerTTL,
	}

	detector.registerDefaultRules()

	return detector
}

func (d *ArbitrageTriggerDetector) registerDefaultRules() {
	d.rules["high_apy_low_risk"] = &TriggerRule{
		ID:            "high_apy_low_risk",
		Name:          "High APY Low Risk",
		Type:          TriggerTypeFundingRate,
		MinProfitAPY:  decimal.NewFromFloat(0.15),
		MaxRiskScore:  0.4,
		MinConfidence: 0.7,
		MinVolume:     decimal.NewFromInt(100000),
		IsActive:      true,
	}

	d.rules["medium_apy_medium_risk"] = &TriggerRule{
		ID:            "medium_apy_medium_risk",
		Name:          "Medium APY Medium Risk",
		Type:          TriggerTypeFundingRate,
		MinProfitAPY:  decimal.NewFromFloat(0.10),
		MaxRiskScore:  0.6,
		MinConfidence: 0.6,
		MinVolume:     decimal.NewFromInt(50000),
		IsActive:      true,
	}

	d.rules["aggressive"] = &TriggerRule{
		ID:            "aggressive",
		Name:          "Aggressive Strategy",
		Type:          TriggerTypeFundingRate,
		MinProfitAPY:  decimal.NewFromFloat(0.05),
		MaxRiskScore:  0.8,
		MinConfidence: 0.5,
		MinVolume:     decimal.NewFromInt(10000),
		IsActive:      false,
	}
}

// RegisterRule registers a new trigger rule
func (d *ArbitrageTriggerDetector) RegisterRule(rule *TriggerRule) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.rules[rule.ID] = rule
}

// RegisterCallback registers a callback for trigger events
func (d *ArbitrageTriggerDetector) RegisterCallback(callback TriggerCallback) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.callbacks = append(d.callbacks, callback)
}

// Start begins the trigger detection loop
func (d *ArbitrageTriggerDetector) Start() error {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.running {
		return fmt.Errorf("trigger detector is already running")
	}

	d.ctx, d.cancel = context.WithCancel(context.Background())
	d.running = true

	d.wg.Add(1)
	go d.detectionLoop()

	d.logger.Info("Arbitrage trigger detector started")
	observability.AddBreadcrumb(d.ctx, "trigger_detector", "Arbitrage trigger detector started", sentry.LevelInfo)
	return nil
}

// Stop gracefully stops the trigger detector
func (d *ArbitrageTriggerDetector) Stop() {
	d.mu.Lock()
	defer d.mu.Unlock()

	if !d.running {
		return
	}

	d.cancel()
	d.wg.Wait()
	d.running = false

	d.logger.Info("Arbitrage trigger detector stopped")
}

func (d *ArbitrageTriggerDetector) detectionLoop() {
	defer d.wg.Done()

	ticker := time.NewTicker(d.checkInterval)
	defer ticker.Stop()

	for {
		select {
		case <-d.ctx.Done():
			return
		case <-ticker.C:
			if err := d.detectTriggers(); err != nil {
				d.logger.WithError(err).Error("Failed to detect triggers")
			}
			if err := d.cleanupExpiredTriggers(); err != nil {
				d.logger.WithError(err).Error("Failed to cleanup expired triggers")
			}
		}
	}
}

func (d *ArbitrageTriggerDetector) detectTriggers() error {
	spanCtx, span := observability.StartSpan(d.ctx, observability.SpanOpArbitrage, "ArbitrageTriggerDetector.detectTriggers")
	defer observability.FinishSpan(span, nil)

	opportunities, err := d.getActiveOpportunities(spanCtx)
	if err != nil {
		return fmt.Errorf("failed to get opportunities: %w", err)
	}

	d.mu.RLock()
	rules := make([]*TriggerRule, 0, len(d.rules))
	for _, rule := range d.rules {
		if rule.IsActive {
			rules = append(rules, rule)
		}
	}
	d.mu.RUnlock()

	triggersDetected := 0
	for _, opp := range opportunities {
		for _, rule := range rules {
			if d.matchesRule(opp, rule) {
				trigger := d.createTrigger(opp, rule)

				if err := d.storeTrigger(spanCtx, trigger); err != nil {
					d.logger.WithFields(map[string]interface{}{
						"trigger_id": trigger.ID,
						"symbol":     trigger.Symbol,
					}).WithError(err).Error("Failed to store trigger")
					continue
				}

				triggersDetected++

				d.notifyCallbacks(spanCtx, trigger)
				break
			}
		}
	}

	if triggersDetected > 0 {
		d.logger.WithFields(map[string]interface{}{
			"triggers_detected": triggersDetected,
			"opportunities":     len(opportunities),
		}).Info("Triggers detected")

		span.SetData("triggers_detected", triggersDetected)
	}

	return nil
}

func (d *ArbitrageTriggerDetector) getActiveOpportunities(ctx context.Context) ([]*models.FuturesArbitrageOpportunity, error) {
	if isNilDBPool(d.db) {
		return nil, fmt.Errorf("database pool is not available")
	}

	query := `
		SELECT 
			id, symbol, base_currency, quote_currency,
			long_exchange, short_exchange,
			long_funding_rate, short_funding_rate, net_funding_rate,
			funding_interval, long_mark_price, short_mark_price,
			price_difference, price_difference_percentage,
			hourly_rate, daily_rate, apy,
			estimated_profit_8h, estimated_profit_daily,
			estimated_profit_weekly, estimated_profit_monthly,
			risk_score, volatility_score, liquidity_score,
			recommended_position_size, max_leverage, recommended_leverage,
			stop_loss_percentage, min_position_size, max_position_size,
			optimal_position_size, detected_at, expires_at, next_funding_time,
			time_to_next_funding, is_active
		FROM futures_arbitrage_opportunities
		WHERE is_active = true
		  AND expires_at > NOW()
		ORDER BY apy DESC
		LIMIT 100
	`

	rows, err := d.db.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to query opportunities: %w", err)
	}
	defer rows.Close()

	var opportunities []*models.FuturesArbitrageOpportunity
	for rows.Next() {
		opp := &models.FuturesArbitrageOpportunity{}
		if err := scanArbitrageOpportunity(rows, opp); err != nil {
			return nil, fmt.Errorf("failed to scan opportunity: %w", err)
		}
		opportunities = append(opportunities, opp)
	}

	return opportunities, rows.Err()
}

func scanArbitrageOpportunity(rows interface{ Scan(...interface{}) error }, opp *models.FuturesArbitrageOpportunity) error {
	return rows.Scan(
		&opp.ID, &opp.Symbol, &opp.BaseCurrency, &opp.QuoteCurrency,
		&opp.LongExchange, &opp.ShortExchange,
		&opp.LongFundingRate, &opp.ShortFundingRate, &opp.NetFundingRate,
		&opp.FundingInterval, &opp.LongMarkPrice, &opp.ShortMarkPrice,
		&opp.PriceDifference, &opp.PriceDifferencePercentage,
		&opp.HourlyRate, &opp.DailyRate, &opp.APY,
		&opp.EstimatedProfit8h, &opp.EstimatedProfitDaily,
		&opp.EstimatedProfitWeekly, &opp.EstimatedProfitMonthly,
		&opp.RiskScore, &opp.VolatilityScore, &opp.LiquidityScore,
		&opp.RecommendedPositionSize, &opp.MaxLeverage, &opp.RecommendedLeverage,
		&opp.StopLossPercentage, &opp.MinPositionSize, &opp.MaxPositionSize,
		&opp.OptimalPositionSize, &opp.DetectedAt, &opp.ExpiresAt, &opp.NextFundingTime,
		&opp.TimeToNextFunding, &opp.IsActive,
	)
}

func (d *ArbitrageTriggerDetector) matchesRule(opp *models.FuturesArbitrageOpportunity, rule *TriggerRule) bool {
	if opp.APY.LessThan(rule.MinProfitAPY) {
		return false
	}

	if opp.RiskScore.GreaterThan(decimal.NewFromFloat(rule.MaxRiskScore)) {
		return false
	}

	// Check confidence threshold
	confidence := d.calculateConfidence(opp)
	if confidence < rule.MinConfidence {
		return false
	}

	pairKey := fmt.Sprintf("%s:%s", opp.LongExchange, opp.ShortExchange)
	for _, excluded := range rule.ExcludedPairs {
		if excluded == pairKey {
			return false
		}
	}

	for _, excluded := range rule.ExcludedExchanges {
		if opp.LongExchange == excluded || opp.ShortExchange == excluded {
			return false
		}
	}

	return true
}

func (d *ArbitrageTriggerDetector) createTrigger(opp *models.FuturesArbitrageOpportunity, rule *TriggerRule) *ArbitrageTrigger {
	confidence := d.calculateConfidence(opp)
	riskScoreF64, _ := opp.RiskScore.Float64()

	return &ArbitrageTrigger{
		ID:              generateTriggerID(opp, rule),
		Type:            rule.Type,
		Symbol:          opp.Symbol,
		LongExchange:    opp.LongExchange,
		ShortExchange:   opp.ShortExchange,
		NetFundingRate:  opp.NetFundingRate,
		ProfitPotential: opp.EstimatedProfit8h,
		APY:             opp.APY,
		RiskScore:       riskScoreF64,
		Status:          TriggerStatusPending,
		TriggeredAt:     time.Now(),
		ExpiresAt:       time.Now().Add(d.triggerTTL),
		Confidence:      confidence,
		Reason:          fmt.Sprintf("Matched rule: %s (APY: %s, Risk: %.2f)", rule.Name, opp.APY.String(), riskScoreF64),
		Metadata: map[string]string{
			"rule_id":        rule.ID,
			"opportunity_id": opp.ID,
		},
	}
}

func (d *ArbitrageTriggerDetector) calculateConfidence(opp *models.FuturesArbitrageOpportunity) float64 {
	confidence := 0.5

	liquidityThreshold1 := decimal.NewFromFloat(0.7)
	liquidityThreshold2 := decimal.NewFromFloat(0.5)

	if opp.LiquidityScore.GreaterThan(liquidityThreshold1) {
		confidence += 0.15
	} else if opp.LiquidityScore.GreaterThan(liquidityThreshold2) {
		confidence += 0.08
	}

	riskThreshold1 := decimal.NewFromFloat(0.3)
	riskThreshold2 := decimal.NewFromFloat(0.5)

	if opp.RiskScore.LessThan(riskThreshold1) {
		confidence += 0.2
	} else if opp.RiskScore.LessThan(riskThreshold2) {
		confidence += 0.1
	}

	if opp.APY.GreaterThan(decimal.NewFromFloat(0.2)) {
		confidence += 0.1
	}

	volThreshold := decimal.NewFromFloat(0.3)
	if opp.VolatilityScore.LessThan(volThreshold) {
		confidence += 0.05
	}

	if confidence > 1.0 {
		confidence = 1.0
	}

	return confidence
}

func generateTriggerID(opp *models.FuturesArbitrageOpportunity, rule *TriggerRule) string {
	return fmt.Sprintf("trg-%s-%s-%s-%s-%d",
		rule.ID,
		opp.Symbol,
		opp.LongExchange,
		opp.ShortExchange,
		time.Now().Unix(),
	)
}

func (d *ArbitrageTriggerDetector) storeTrigger(ctx context.Context, trigger *ArbitrageTrigger) error {
	if isNilDBPool(d.db) {
		return fmt.Errorf("database pool is not available")
	}

	query := `
		INSERT INTO arbitrage_triggers (
			id, type, symbol, long_exchange, short_exchange,
			net_funding_rate, profit_potential, apy, risk_score,
			status, triggered_at, expires_at, confidence, reason, metadata
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15)
		ON CONFLICT (id) DO UPDATE SET
			status = EXCLUDED.status,
			confidence = EXCLUDED.confidence,
			expires_at = EXCLUDED.expires_at
	`

	metadataJSON := "{}"
	if trigger.Metadata != nil {
		if metadataBytes, err := json.Marshal(trigger.Metadata); err == nil {
			metadataJSON = string(metadataBytes)
		}
	}

	_, err := d.db.Exec(ctx, query,
		trigger.ID, trigger.Type, trigger.Symbol, trigger.LongExchange, trigger.ShortExchange,
		trigger.NetFundingRate, trigger.ProfitPotential, trigger.APY, trigger.RiskScore,
		trigger.Status, trigger.TriggeredAt, trigger.ExpiresAt, trigger.Confidence, trigger.Reason, metadataJSON,
	)

	return err
}

func (d *ArbitrageTriggerDetector) notifyCallbacks(ctx context.Context, trigger *ArbitrageTrigger) {
	d.mu.RLock()
	callbacks := make([]TriggerCallback, len(d.callbacks))
	copy(callbacks, d.callbacks)
	d.mu.RUnlock()

	for _, callback := range callbacks {
		go func(cb TriggerCallback) {
			if err := cb(ctx, trigger); err != nil {
				d.logger.WithFields(map[string]interface{}{
					"trigger_id": trigger.ID,
				}).WithError(err).Error("Trigger callback failed")
			}
		}(callback)
	}
}

func (d *ArbitrageTriggerDetector) cleanupExpiredTriggers() error {
	if isNilDBPool(d.db) {
		return fmt.Errorf("database pool is not available")
	}

	query := `
		UPDATE arbitrage_triggers
		SET status = 'expired'
		WHERE status = 'pending'
		  AND expires_at < NOW()
	`

	_, err := d.db.Exec(d.ctx, query)
	return err
}

// GetPendingTriggers returns all pending triggers
func (d *ArbitrageTriggerDetector) GetPendingTriggers(ctx context.Context) ([]*ArbitrageTrigger, error) {
	if isNilDBPool(d.db) {
		return nil, fmt.Errorf("database pool is not available")
	}

	query := `
		SELECT id, type, symbol, long_exchange, short_exchange,
			net_funding_rate, profit_potential, apy, risk_score,
			status, triggered_at, expires_at, confidence, reason
		FROM arbitrage_triggers
		WHERE status = 'pending'
		  AND expires_at > NOW()
		ORDER BY confidence DESC, apy DESC
	`

	rows, err := d.db.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to query triggers: %w", err)
	}
	defer rows.Close()

	var triggers []*ArbitrageTrigger
	for rows.Next() {
		trigger := &ArbitrageTrigger{}
		if err := rows.Scan(
			&trigger.ID, &trigger.Type, &trigger.Symbol, &trigger.LongExchange, &trigger.ShortExchange,
			&trigger.NetFundingRate, &trigger.ProfitPotential, &trigger.APY, &trigger.RiskScore,
			&trigger.Status, &trigger.TriggeredAt, &trigger.ExpiresAt, &trigger.Confidence, &trigger.Reason,
		); err != nil {
			return nil, fmt.Errorf("failed to scan trigger: %w", err)
		}
		triggers = append(triggers, trigger)
	}

	return triggers, rows.Err()
}

// MarkTriggerExecuted marks a trigger as executed
func (d *ArbitrageTriggerDetector) MarkTriggerExecuted(ctx context.Context, triggerID string) error {
	if isNilDBPool(d.db) {
		return fmt.Errorf("database pool is not available")
	}

	now := time.Now()
	query := `
		UPDATE arbitrage_triggers
		SET status = 'executed', executed_at = $1
		WHERE id = $2 AND status = 'pending'
	`

	_, err := d.db.Exec(ctx, query, now, triggerID)
	return err
}

// GetRules returns all registered rules
func (d *ArbitrageTriggerDetector) GetRules() []*TriggerRule {
	d.mu.RLock()
	defer d.mu.RUnlock()

	rules := make([]*TriggerRule, 0, len(d.rules))
	for _, rule := range d.rules {
		rules = append(rules, rule)
	}
	return rules
}

// UpdateRule updates a trigger rule
func (d *ArbitrageTriggerDetector) UpdateRule(ruleID string, isActive bool, minAPY float64, maxRisk float64) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	rule, ok := d.rules[ruleID]
	if !ok {
		return fmt.Errorf("rule not found: %s", ruleID)
	}

	rule.IsActive = isActive
	rule.MinProfitAPY = decimal.NewFromFloat(minAPY)
	rule.MaxRiskScore = maxRisk

	return nil
}

// IsRunning returns whether the detector is running
func (d *ArbitrageTriggerDetector) IsRunning() bool {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.running
}
