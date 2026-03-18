package scalping

import (
	"time"

	"github.com/shopspring/decimal"
)

type Direction string

const (
	DirectionBuy  Direction = "buy"
	DirectionSell Direction = "sell"
	DirectionHold Direction = "hold"
)

type SignalStrength string

const (
	StrengthWeak   SignalStrength = "weak"
	StrengthMedium SignalStrength = "medium"
	StrengthStrong SignalStrength = "strong"
)

type SignalComponent struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Value       decimal.Decimal `json:"value"`
	Signal      Direction       `json:"signal"`
	Strength    SignalStrength  `json:"strength"`
	Weight      decimal.Decimal `json:"weight"`
}

type MicrostructureContext struct {
	SpreadPct      decimal.Decimal `json:"spread_pct"`
	Imbalance1Pct  decimal.Decimal `json:"imbalance_1pct"`
	Imbalance2Pct  decimal.Decimal `json:"imbalance_2pct"`
	BidDepthUSD    decimal.Decimal `json:"bid_depth_usd"`
	AskDepthUSD    decimal.Decimal `json:"ask_depth_usd"`
	MidPrice       decimal.Decimal `json:"mid_price"`
	BestBid        decimal.Decimal `json:"best_bid"`
	BestAsk        decimal.Decimal `json:"best_ask"`
	LiquidityScore decimal.Decimal `json:"liquidity_score"`
}

type QualityAssessment struct {
	OverallScore   decimal.Decimal `json:"overall_score"`
	DataFreshness  decimal.Decimal `json:"data_freshness"`
	LiquidityScore decimal.Decimal `json:"liquidity_score"`
	VolatilityOK   bool            `json:"volatility_ok"`
	PassReason     string          `json:"pass_reason"`
	FailReasons    []string        `json:"fail_reasons,omitempty"`
}

type ScalpingSignal struct {
	ID                 string                     `json:"id"`
	Exchange           string                     `json:"exchange"`
	Symbol             string                     `json:"symbol"`
	Direction          Direction                  `json:"direction"`
	Confidence         decimal.Decimal            `json:"confidence"`
	Components         []SignalComponent          `json:"components"`
	Microstructure     *MicrostructureContext     `json:"microstructure,omitempty"`
	Quality            *QualityAssessment         `json:"quality,omitempty"`
	StopLoss           decimal.Decimal            `json:"stop_loss,omitempty"`
	TakeProfit         decimal.Decimal            `json:"take_profit,omitempty"`
	AttributionWeights map[string]decimal.Decimal `json:"attribution_weights,omitempty"`
	Metadata           map[string]interface{}     `json:"metadata,omitempty"`
	GeneratedAt        time.Time                  `json:"generated_at"`
}

func (s *ScalpingSignal) ToMap() map[string]interface{} {
	m := map[string]interface{}{
		"id":           s.ID,
		"exchange":     s.Exchange,
		"symbol":       s.Symbol,
		"direction":    string(s.Direction),
		"confidence":   s.Confidence.StringFixed(4),
		"generated_at": s.GeneratedAt.UTC().Format(time.RFC3339),
	}
	if s.StopLoss.IsPositive() {
		m["stop_loss"] = s.StopLoss.StringFixed(8)
	}
	if s.TakeProfit.IsPositive() {
		m["take_profit"] = s.TakeProfit.StringFixed(8)
	}
	components := make([]map[string]interface{}, len(s.Components))
	for i, c := range s.Components {
		components[i] = map[string]interface{}{
			"name":     c.Name,
			"value":    c.Value.StringFixed(6),
			"signal":   string(c.Signal),
			"strength": string(c.Strength),
			"weight":   c.Weight.StringFixed(4),
		}
	}
	m["components"] = components
	if s.AttributionWeights != nil {
		weights := make(map[string]string, len(s.AttributionWeights))
		for k, v := range s.AttributionWeights {
			weights[k] = v.StringFixed(4)
		}
		m["attribution_weights"] = weights
	}
	return m
}

type SignalOutcomeRecord struct {
	SignalID   string
	Exchange   string
	Symbol     string
	Direction  string
	Confidence decimal.Decimal
	EntryPrice decimal.Decimal
	ExitPrice  decimal.Decimal
	PnL        decimal.Decimal
	Outcome    string
	Components map[string]decimal.Decimal
	RecordedAt time.Time
}

func (s *ScalpingSignal) RecordOutcome(entryPrice, exitPrice, pnl decimal.Decimal, outcome string) SignalOutcomeRecord {
	record := SignalOutcomeRecord{
		SignalID:   s.ID,
		Exchange:   s.Exchange,
		Symbol:     s.Symbol,
		Direction:  string(s.Direction),
		Confidence: s.Confidence,
		EntryPrice: entryPrice,
		ExitPrice:  exitPrice,
		PnL:        pnl,
		Outcome:    outcome,
		Components: make(map[string]decimal.Decimal),
		RecordedAt: time.Now(),
	}
	for _, comp := range s.Components {
		record.Components[comp.Name] = comp.Weight
	}
	return record
}
