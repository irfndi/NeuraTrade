package services

import (
	"fmt"
	"math"
	"strings"

	appautonomy "github.com/irfndi/neuratrade/internal/app/autonomy"
	"github.com/shopspring/decimal"
	"go.uber.org/zap"
)

type ShadowMirroredDecision struct {
	VariantID         string
	VariantName       string
	LiveAction        string
	ShadowAction      string
	Symbol            string
	Confidence        float64
	SizePercent       float64
	GateAllowed       bool
	GateReason        string
	GateCode          string
	DivergenceSignals []string
	EntryPrice        *decimal.Decimal
	StopLoss          *decimal.Decimal
	TakeProfit        *decimal.Decimal
	DecisionTime      string
}

type ShadowDecisionMirror struct {
	logger *zap.Logger
}

func NewShadowDecisionMirror(logger *zap.Logger) *ShadowDecisionMirror {
	if logger == nil {
		logger = zap.NewNop()
	}
	return &ShadowDecisionMirror{logger: logger}
}

func (m *ShadowDecisionMirror) MirrorDecision(
	liveDecision *AITradingDecision,
	portfolio TradingPortfolio,
	policy appautonomy.ScalpingCyclePolicy,
	variant ShadowVariantConfig,
) ShadowMirroredDecision {
	_ = portfolio
	normalizedVariant, err := variant.Normalize()
	if err != nil {
		normalizedVariant = NewDefaultShadowVariant()
	}
	effectivePolicy := normalizedVariant.ApplyToPolicy(policy)
	result := ShadowMirroredDecision{
		VariantID:    normalizedVariant.VariantID,
		VariantName:  normalizedVariant.Name,
		ShadowAction: "hold",
		GateAllowed:  false,
		GateReason:   "live decision unavailable",
		GateCode:     appautonomy.CandidateRejectAutonomyRuntime,
	}
	if liveDecision == nil {
		return result
	}

	action := strings.ToLower(strings.TrimSpace(liveDecision.Action))
	if action == "" {
		action = "hold"
	}
	result.LiveAction = action
	result.ShadowAction = action
	result.Symbol = strings.TrimSpace(liveDecision.Symbol)
	result.Confidence = liveDecision.Confidence
	result.SizePercent = clampShadowFloat(liveDecision.SizePercent, 0, 100)
	result.GateAllowed = true
	result.GateCode = ""
	result.GateReason = ""
	result.EntryPrice = cloneDecimalPtr(liveDecision.EntryPrice)
	result.StopLoss = cloneDecimalPtr(liveDecision.StopLoss)
	result.TakeProfit = cloneDecimalPtr(liveDecision.TakeProfit)

	if gate := liveDecision.ExecutionGate; gate != nil {
		if !gate.Allowed {
			result.GateAllowed = false
			result.GateReason = strings.TrimSpace(gate.BlockReason)
			result.GateCode = strings.TrimSpace(gate.BlockCode)
		}
	}

	if action == "hold" {
		result.GateAllowed = false
		if result.GateReason == "" {
			result.GateReason = strings.TrimSpace(liveDecision.Reasoning)
		}
		if result.GateCode == "" {
			result.GateCode = strings.TrimSpace(liveDecision.ReasonCategory)
		}
		if canOverrideLiveHold(liveDecision, effectivePolicy) {
			result.DivergenceSignals = append(result.DivergenceSignals, "live_skip_shadow_opportunity")
			result.ShadowAction = "buy"
			result.GateAllowed = true
			result.GateReason = "shadow policy allows confidence threshold"
			if result.GateCode == "" {
				result.GateCode = appautonomy.CandidateRejectConfidenceBelowThreshold
			}
			if result.EntryPrice == nil || !result.EntryPrice.GreaterThan(decimal.Zero) {
				result.GateAllowed = false
				result.ShadowAction = "hold"
				result.GateReason = "no executable entry price for shadow override"
				result.GateCode = "no_entry_price"
			}
			if result.SizePercent <= 0 {
				result.SizePercent = effectivePolicy.EffectiveMaxCapitalPct
			}
			return result
		}
		return result
	}

	if action != "buy" && action != "sell" {
		result.ShadowAction = "hold"
		result.GateAllowed = false
		if result.GateReason == "" {
			result.GateReason = fmt.Sprintf("unsupported action %q", action)
		}
		if result.GateCode == "" {
			result.GateCode = appautonomy.CandidateRejectNoDirectionalEdge
		}
		return result
	}

	threshold := effectivePolicy.EffectiveMinConfidence
	if threshold <= 0 {
		threshold = 0.65
	}
	if liveDecision.Confidence < threshold {
		result.ShadowAction = "hold"
		result.GateAllowed = false
		result.GateCode = appautonomy.CandidateRejectConfidenceBelowThreshold
		result.GateReason = fmt.Sprintf("confidence %.2f below variant threshold %.2f", liveDecision.Confidence, threshold)
		result.DivergenceSignals = append(result.DivergenceSignals, "live_entry_shadow_skip")
	}

	maxCapital := effectivePolicy.EffectiveMaxCapitalPct
	if maxCapital <= 0 || math.IsNaN(maxCapital) || math.IsInf(maxCapital, 0) {
		maxCapital = liveDecision.EffectiveMaxCapitalPct
	}
	if maxCapital > 0 && result.SizePercent > maxCapital {
		result.SizePercent = maxCapital
		result.DivergenceSignals = append(result.DivergenceSignals, "position_size_divergence")
	}

	if liveDecision.ExecutionGate != nil && strings.TrimSpace(liveDecision.ExecutionGate.BlockCode) != "" {
		if strings.TrimSpace(liveDecision.ExecutionGate.BlockCode) != strings.TrimSpace(result.GateCode) {
			result.DivergenceSignals = append(result.DivergenceSignals, "gate_reason_divergence")
		}
	}

	if result.ShadowAction == action && result.GateAllowed {
		result.DivergenceSignals = append(result.DivergenceSignals, "mirrored")
	}

	return result
}

func canOverrideLiveHold(liveDecision *AITradingDecision, policy appautonomy.ScalpingCyclePolicy) bool {
	if liveDecision == nil {
		return false
	}
	if !strings.EqualFold(strings.TrimSpace(liveDecision.Action), "hold") {
		return false
	}
	if !liveDecision.ConfidenceKnown {
		return false
	}
	threshold := policy.EffectiveMinConfidence
	if threshold <= 0 {
		return false
	}
	if liveDecision.Confidence >= threshold {
		return true
	}
	return false
}

func cloneDecimalPtr(value *decimal.Decimal) *decimal.Decimal {
	if value == nil {
		return nil
	}
	copyValue := *value
	return &copyValue
}
