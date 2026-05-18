package autonomy

import (
	"fmt"
	"math"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/shopspring/decimal"
)

const (
	AccountTierMicro    = "micro"
	AccountTierSmall    = "small"
	AccountTierStandard = "standard"
)

const (
	DefaultMicroAccountMaxValue       = 250.0
	DefaultSmallAccountMaxValue       = 2500.0
	DefaultMicroMinConfidenceFloor    = 0.55
	DefaultMicroConfidenceCap         = 0.65
	DefaultMicroMaxCapitalPct         = 0.50
	DefaultMaxConcurrentPositions     = 3
	DefaultMicroMaxConcurrentPosition = 1
	DefaultScalpingProgressBlockAfter = 2 * time.Hour
)

const (
	CandidateRejectConfidenceBelowThreshold = "confidence_below_effective_threshold"
	CandidateRejectSpreadTooWide            = "spread_too_wide"
	CandidateRejectMissingOrderbookSignal   = "missing_orderbook_signal"
	CandidateRejectPreTradeExpectancy       = "pretrade_expectancy_block"
	CandidateRejectRolloutShadow            = "rollout_shadow_block"
	CandidateRejectRolloutPaper             = "rollout_paper_block"
	CandidateRejectMaxConcurrentPositions   = "max_concurrent_positions_reached"
	CandidateRejectAutonomyGateClosed       = "autonomy_gate_closed"
	CandidateRejectAutonomyRuntime          = "autonomy_runtime_unavailable"
	CandidateRejectSafeMode                 = "safe_mode_active"
	CandidateRejectKillSwitch               = "kill_switch_active"
	CandidateRejectConnectivity             = "connectivity_block"
	CandidateRejectRiskBudget               = "risk_budget_block"
	CandidateRejectNoDirectionalEdge        = "no_directional_edge"
)

const (
	DefaultScalpingMaxBidAskSpreadPct  = 0.22
	ScalpingMaxBidAskSpreadPctEnv      = "SCALPING_MAX_BID_ASK_SPREAD_PCT"
	NeuraScalpingMaxBidAskSpreadPctEnv = "NEURATRADE_SCALPING_MAX_BID_ASK_SPREAD_PCT"
	minScalpingMaxBidAskSpreadPct      = 0.0001
	maxScalpingMaxBidAskSpreadPct      = 5.0
	scalpingStrongImbalanceFloor       = 0.20
	scalpingNeutralImbalanceFloor      = 0.10
	scalpingBuyRangeMax                = 45.0
	scalpingSellRangeMin               = 55.0
	scalpingContinuationRangeBuffer    = 5.0
	scalpingConfidenceBase             = 0.50
	scalpingConfidenceImbalanceW       = 0.55
	scalpingConfidenceLiquidityW       = 0.20
	scalpingConfidenceRangeW           = 0.15
	scalpingConfidenceVolumeW          = 0.10
	scalpingConfidenceVolumeLogBase    = 8.0
)

type ScalpingPolicyConfig struct {
	MicroAccountMaxValue     decimal.Decimal
	SmallAccountMaxValue     decimal.Decimal
	MaxBidAskSpreadPct       float64
	MicroMinConfidenceFloor  float64
	MicroConfidenceCap       float64
	MicroMaxCapitalPct       float64
	MaxConcurrentPositions   int
	MicroMaxConcurrent       int
	NoFillRecoveryMinutes    int
	NoFillMinConfidenceFloor float64
	NoFillMaxCapitalPctCap   float64
	RecoveryMicroEntryCapPct float64
	ProgressBlockAfter       time.Duration
	LossStreakBudget         int
}

func DefaultScalpingPolicyConfig() ScalpingPolicyConfig {
	return ScalpingPolicyConfig{
		MicroAccountMaxValue:     decimal.NewFromFloat(DefaultMicroAccountMaxValue),
		SmallAccountMaxValue:     decimal.NewFromFloat(DefaultSmallAccountMaxValue),
		MaxBidAskSpreadPct:       ResolveScalpingMaxBidAskSpreadPctFromEnv(),
		MicroMinConfidenceFloor:  DefaultMicroMinConfidenceFloor,
		MicroConfidenceCap:       DefaultMicroConfidenceCap,
		MicroMaxCapitalPct:       DefaultMicroMaxCapitalPct,
		MaxConcurrentPositions:   DefaultMaxConcurrentPositions,
		MicroMaxConcurrent:       DefaultMicroMaxConcurrentPosition,
		NoFillRecoveryMinutes:    180,
		NoFillMinConfidenceFloor: 0.70,
		NoFillMaxCapitalPctCap:   1.50,
		RecoveryMicroEntryCapPct: DefaultRecoveryMicroEntryCapPct,
		ProgressBlockAfter:       DefaultScalpingProgressBlockAfter,
		LossStreakBudget:         DefaultRecoveryLossStreakBudget,
	}
}

func (c ScalpingPolicyConfig) Normalized() ScalpingPolicyConfig {
	if c.MicroAccountMaxValue.LessThanOrEqual(decimal.Zero) {
		c.MicroAccountMaxValue = decimal.NewFromFloat(DefaultMicroAccountMaxValue)
	}
	if c.SmallAccountMaxValue.LessThanOrEqual(c.MicroAccountMaxValue) {
		c.SmallAccountMaxValue = decimal.NewFromFloat(DefaultSmallAccountMaxValue)
	}
	if c.MaxBidAskSpreadPct <= 0 || math.IsNaN(c.MaxBidAskSpreadPct) || math.IsInf(c.MaxBidAskSpreadPct, 0) {
		c.MaxBidAskSpreadPct = ResolveScalpingMaxBidAskSpreadPctFromEnv()
	}
	c.MaxBidAskSpreadPct = clampFloat(c.MaxBidAskSpreadPct, minScalpingMaxBidAskSpreadPct, maxScalpingMaxBidAskSpreadPct)
	if c.MicroMinConfidenceFloor <= 0 {
		c.MicroMinConfidenceFloor = DefaultMicroMinConfidenceFloor
	}
	c.MicroMinConfidenceFloor = clampFloat(c.MicroMinConfidenceFloor, 0.05, 0.95)
	if c.MicroConfidenceCap <= 0 {
		c.MicroConfidenceCap = DefaultMicroConfidenceCap
	}
	c.MicroConfidenceCap = clampFloat(c.MicroConfidenceCap, c.MicroMinConfidenceFloor, 0.95)
	if c.MicroMaxCapitalPct <= 0 {
		c.MicroMaxCapitalPct = DefaultMicroMaxCapitalPct
	}
	c.MicroMaxCapitalPct = clampFloat(c.MicroMaxCapitalPct, 0.10, 100.0)
	if c.MaxConcurrentPositions <= 0 {
		c.MaxConcurrentPositions = DefaultMaxConcurrentPositions
	}
	if c.MicroMaxConcurrent <= 0 {
		c.MicroMaxConcurrent = DefaultMicroMaxConcurrentPosition
	}
	if c.NoFillRecoveryMinutes <= 0 {
		c.NoFillRecoveryMinutes = 180
	}
	if c.NoFillMinConfidenceFloor <= 0 {
		c.NoFillMinConfidenceFloor = 0.70
	}
	c.NoFillMinConfidenceFloor = clampFloat(c.NoFillMinConfidenceFloor, 0.05, 0.95)
	if c.NoFillMaxCapitalPctCap <= 0 {
		c.NoFillMaxCapitalPctCap = 1.50
	}
	c.NoFillMaxCapitalPctCap = clampFloat(c.NoFillMaxCapitalPctCap, 0.10, 100.0)
	if c.RecoveryMicroEntryCapPct <= 0 {
		c.RecoveryMicroEntryCapPct = DefaultRecoveryMicroEntryCapPct
	}
	c.RecoveryMicroEntryCapPct = clampFloat(c.RecoveryMicroEntryCapPct, 0.10, 100.0)
	if c.ProgressBlockAfter <= 0 {
		c.ProgressBlockAfter = DefaultScalpingProgressBlockAfter
	}
	if c.LossStreakBudget <= 0 {
		c.LossStreakBudget = DefaultRecoveryLossStreakBudget
	}
	c.LossStreakBudget = clampInt(c.LossStreakBudget, 1, 20)
	return c
}

type PerformanceWindowInput struct {
	DecisiveTrades     int
	DecisiveWinRatePct float64
}

type ScalpingCycleInput struct {
	TotalValue               decimal.Decimal
	OpenPositions            int
	DriftActive              bool
	BaseMinConfidence        float64
	BaseMaxCapitalPct        float64
	ExecutionMinCapitalPct   decimal.Decimal
	NonExecutableDueToWallet bool
	AdjustedMaxCapitalPct    float64
	ConsecutiveLosses        int
	Phase                    string
	PhaseMinConfidence       float64
	PhaseMaxCapitalPct       float64
	MilestoneProgress        float64
	NoFillMinutes            float64
	RiskDrawdown             float64
	RiskExpectancy           float64
	RiskSampleSize           int
	RecoveryMode             string
	PerformanceWindow        PerformanceWindowInput
}

type ScalpingCyclePolicy struct {
	AccountTier            string
	EffectiveMinConfidence float64
	EffectiveMaxCapitalPct float64
	MaxBidAskSpreadPct     float64
	MaxConcurrentPositions int
	LossStreakBudget       int
	PolicyAdjustments      []string
}

type CandidateSignal struct {
	Symbol             string
	Price              decimal.Decimal
	High24h            decimal.Decimal
	Low24h             decimal.Decimal
	Volume24h          decimal.Decimal
	BidAskSpread       float64
	OrderBookImbalance float64
	RangePosition24h   float64
	PriceChange24hPct  float64
}

type CandidateRejection struct {
	Symbol              string  `json:"symbol"`
	Reason              string  `json:"reason"`
	EstimatedConfidence float64 `json:"estimated_confidence,omitempty"`
	BidAskSpreadPct     float64 `json:"bid_ask_spread_pct"`
	OrderBookImbalance  float64 `json:"order_book_imbalance"`
	RangePosition24h    float64 `json:"range_position_24h"`
	PriceChange24hPct   float64 `json:"price_change_24h_pct"`
}

type CandidateFunnelSnapshot struct {
	CandidateUniverseCount int                  `json:"candidate_universe_count"`
	CandidateRankedCount   int                  `json:"candidate_ranked_count"`
	CandidateViableCount   int                  `json:"candidate_viable_count"`
	RejectionCounts        map[string]int       `json:"rejection_counts,omitempty"`
	TopCandidateRejections []CandidateRejection `json:"top_candidate_rejections,omitempty"`
}

type ExecutionGateSnapshot struct {
	Allowed              bool   `json:"allowed"`
	BlockReason          string `json:"block_reason,omitempty"`
	BlockCode            string `json:"block_code,omitempty"`
	RolloutStageCurrent  string `json:"rollout_stage_current,omitempty"`
	RolloutStatusCurrent string `json:"rollout_status_current,omitempty"`
	RolloutGateReason    string `json:"rollout_gate_reason_current,omitempty"`
}

type ProgressBlockState struct {
	Blocked bool   `json:"progress_blocked"`
	Reason  string `json:"progress_block_reason,omitempty"`
}

func EvaluateScalpingPolicy(input ScalpingCycleInput, cfg ScalpingPolicyConfig) ScalpingCyclePolicy {
	cfg = cfg.Normalized()
	recoveryMode := strings.ToLower(strings.TrimSpace(input.RecoveryMode))

	policy := ScalpingCyclePolicy{
		AccountTier:            ResolveAccountTier(input.TotalValue, cfg),
		EffectiveMinConfidence: clampFloat(input.BaseMinConfidence, 0.05, 0.95),
		EffectiveMaxCapitalPct: clampFloat(input.BaseMaxCapitalPct, 0.10, 100.0),
		MaxBidAskSpreadPct:     cfg.MaxBidAskSpreadPct,
		MaxConcurrentPositions: cfg.MaxConcurrentPositions,
		LossStreakBudget:       cfg.LossStreakBudget,
	}
	if policy.EffectiveMaxCapitalPct <= 0 {
		policy.EffectiveMaxCapitalPct = 0.10
	}

	if input.AdjustedMaxCapitalPct > 0 && input.AdjustedMaxCapitalPct < policy.EffectiveMaxCapitalPct {
		policy.EffectiveMaxCapitalPct = input.AdjustedMaxCapitalPct
		policy.PolicyAdjustments = append(policy.PolicyAdjustments, "performance_cap_applied")
	}

	if input.PerformanceWindow.DecisiveTrades >= 10 &&
		input.PerformanceWindow.DecisiveWinRatePct > 0 &&
		input.PerformanceWindow.DecisiveWinRatePct < 35 {
		if policy.EffectiveMinConfidence < 0.70 {
			policy.EffectiveMinConfidence = 0.70
		}
		policy.EffectiveMaxCapitalPct = policy.EffectiveMaxCapitalPct * 0.60
		policy.PolicyAdjustments = append(policy.PolicyAdjustments, "weak_recent_win_rate")
	}
	if input.PerformanceWindow.DecisiveTrades >= 20 &&
		input.PerformanceWindow.DecisiveWinRatePct > 0 &&
		input.PerformanceWindow.DecisiveWinRatePct < 30 {
		if policy.EffectiveMinConfidence < 0.78 {
			policy.EffectiveMinConfidence = 0.78
		}
		policy.EffectiveMaxCapitalPct = policy.EffectiveMaxCapitalPct * 0.50
		policy.PolicyAdjustments = append(policy.PolicyAdjustments, "critical_recent_win_rate")
	}

	if input.ConsecutiveLosses >= 2 {
		policy.EffectiveMinConfidence += 0.05 * float64(input.ConsecutiveLosses-1)
		policy.PolicyAdjustments = append(policy.PolicyAdjustments, "loss_streak_confidence_tightening")
	}

	applyNoFillRecovery(&policy, input, cfg)

	phaseMinConfidence := input.PhaseMinConfidence
	if policy.AccountTier == AccountTierMicro && phaseMinConfidence > cfg.MicroMinConfidenceFloor {
		phaseMinConfidence = cfg.MicroMinConfidenceFloor
		policy.PolicyAdjustments = append(policy.PolicyAdjustments, "micro_tier_bootstrap_relaxation")
	}
	if phaseMinConfidence > policy.EffectiveMinConfidence {
		policy.EffectiveMinConfidence = phaseMinConfidence
	}

	if input.PhaseMaxCapitalPct > 0 && input.PhaseMaxCapitalPct < policy.EffectiveMaxCapitalPct {
		policy.EffectiveMaxCapitalPct = input.PhaseMaxCapitalPct
	}
	if input.MilestoneProgress > 0 && input.MilestoneProgress < 25 {
		policy.EffectiveMaxCapitalPct = policy.EffectiveMaxCapitalPct * 0.80
		policy.PolicyAdjustments = append(policy.PolicyAdjustments, "fund_milestone_cap")
	}
	if input.RiskSampleSize >= 10 && input.RiskExpectancy < 0 {
		policy.EffectiveMaxCapitalPct = policy.EffectiveMaxCapitalPct * 0.75
		policy.PolicyAdjustments = append(policy.PolicyAdjustments, "negative_expectancy_cap")
	}
	if input.RiskDrawdown > 0.12 {
		policy.EffectiveMinConfidence += 0.05
		policy.EffectiveMaxCapitalPct = policy.EffectiveMaxCapitalPct * 0.70
		policy.PolicyAdjustments = append(policy.PolicyAdjustments, "drawdown_tightening")
	}

	switch recoveryMode {
	case RecoveryModeMicroEntry:
		if policy.EffectiveMaxCapitalPct > cfg.RecoveryMicroEntryCapPct {
			policy.EffectiveMaxCapitalPct = cfg.RecoveryMicroEntryCapPct
		}
		policy.PolicyAdjustments = append(policy.PolicyAdjustments, "recovery_micro_entry_cap")
	case RecoveryModeDeriskOnly:
		if policy.EffectiveMaxCapitalPct > 0.10 {
			policy.EffectiveMaxCapitalPct = 0.10
		}
		if policy.EffectiveMinConfidence < 0.85 {
			policy.EffectiveMinConfidence = 0.85
		}
		policy.PolicyAdjustments = append(policy.PolicyAdjustments, "recovery_derisk_only")
	}

	if policy.AccountTier == AccountTierMicro {
		if recoveryMode == "" &&
			policy.EffectiveMinConfidence > cfg.MicroConfidenceCap &&
			!hasAnyPolicyAdjustment(policy.PolicyAdjustments,
				"weak_recent_win_rate",
				"critical_recent_win_rate",
				"loss_streak_confidence_tightening",
				"drawdown_tightening",
			) {
			policy.EffectiveMinConfidence = cfg.MicroConfidenceCap
			policy.PolicyAdjustments = append(policy.PolicyAdjustments, "micro_confidence_cap")
		}
		if policy.EffectiveMaxCapitalPct > cfg.MicroMaxCapitalPct {
			policy.EffectiveMaxCapitalPct = cfg.MicroMaxCapitalPct
		}
		policy.MaxConcurrentPositions = cfg.MicroMaxConcurrent
	}
	if input.NonExecutableDueToWallet {
		policy.EffectiveMaxCapitalPct = 0
		policy.PolicyAdjustments = append(policy.PolicyAdjustments, "exchange_min_notional_block")
	}
	if !input.NonExecutableDueToWallet && input.ExecutionMinCapitalPct.GreaterThan(decimal.NewFromInt(100)) {
		policy.EffectiveMaxCapitalPct = 0
		policy.PolicyAdjustments = append(policy.PolicyAdjustments, "exchange_min_notional_block")
	}
	if !input.NonExecutableDueToWallet &&
		input.ExecutionMinCapitalPct.GreaterThan(decimal.Zero) &&
		input.ExecutionMinCapitalPct.LessThanOrEqual(decimal.NewFromInt(100)) &&
		input.ExecutionMinCapitalPct.GreaterThan(decimal.NewFromFloat(policy.EffectiveMaxCapitalPct)) {
		policy.EffectiveMaxCapitalPct = input.ExecutionMinCapitalPct.InexactFloat64()
		policy.PolicyAdjustments = append(policy.PolicyAdjustments, "exchange_min_notional_floor")
	}

	policy.EffectiveMinConfidence = clampFloat(policy.EffectiveMinConfidence, 0.05, 0.95)
	if policy.EffectiveMaxCapitalPct > 0 {
		policy.EffectiveMaxCapitalPct = clampFloat(policy.EffectiveMaxCapitalPct, 0.10, 100.0)
	}
	return policy
}

func ResolveAccountTier(totalValue decimal.Decimal, cfg ScalpingPolicyConfig) string {
	cfg = cfg.Normalized()
	switch {
	case totalValue.Cmp(cfg.MicroAccountMaxValue) <= 0:
		return AccountTierMicro
	case totalValue.Cmp(cfg.SmallAccountMaxValue) <= 0:
		return AccountTierSmall
	default:
		return AccountTierStandard
	}
}

func BuildCandidateFunnel(signals []CandidateSignal, policy ScalpingCyclePolicy) CandidateFunnelSnapshot {
	snapshot := CandidateFunnelSnapshot{
		CandidateUniverseCount: len(signals),
		RejectionCounts:        make(map[string]int),
	}
	if len(signals) == 0 {
		return snapshot
	}

	type evaluatedCandidate struct {
		rejection CandidateRejection
		ranked    bool
	}

	rejections := make([]evaluatedCandidate, 0, len(signals))
	for _, signal := range signals {
		ranked, viable, rejection := evaluateCandidateSignal(signal, policy)
		if ranked {
			snapshot.CandidateRankedCount++
		}
		if viable {
			snapshot.CandidateViableCount++
			continue
		}
		snapshot.RejectionCounts[rejection.Reason]++
		rejections = append(rejections, evaluatedCandidate{
			rejection: rejection,
			ranked:    ranked,
		})
	}

	sort.SliceStable(rejections, func(i, j int) bool {
		if rejections[i].rejection.EstimatedConfidence == rejections[j].rejection.EstimatedConfidence {
			return rejections[i].rejection.Symbol < rejections[j].rejection.Symbol
		}
		return rejections[i].rejection.EstimatedConfidence > rejections[j].rejection.EstimatedConfidence
	})

	limit := 3
	if len(rejections) < limit {
		limit = len(rejections)
	}
	snapshot.TopCandidateRejections = make([]CandidateRejection, 0, limit)
	for i := 0; i < limit; i++ {
		snapshot.TopCandidateRejections = append(snapshot.TopCandidateRejections, rejections[i].rejection)
	}
	return snapshot
}

func EvaluateProgressBlock(lastEntryAttempt time.Time, now time.Time, cfg ScalpingPolicyConfig) ProgressBlockState {
	cfg = cfg.Normalized()
	if lastEntryAttempt.IsZero() || now.IsZero() {
		return ProgressBlockState{}
	}
	if now.Sub(lastEntryAttempt) < cfg.ProgressBlockAfter {
		return ProgressBlockState{}
	}
	return ProgressBlockState{
		Blocked: true,
		Reason:  fmt.Sprintf("no real entry attempt for %s", cfg.ProgressBlockAfter.Round(time.Minute)),
	}
}

func BuildRolloutShadowGate(stage, status, reason string) ExecutionGateSnapshot {
	stage = strings.TrimSpace(stage)
	status = strings.TrimSpace(status)
	reason = strings.TrimSpace(reason)
	if reason == "" {
		reason = fmt.Sprintf("strategy_not_live (stage: %s, status: %s)", stage, status)
	}
	blockCode := CandidateRejectRolloutShadow
	if strings.EqualFold(stage, "paper") {
		blockCode = CandidateRejectRolloutPaper
	}
	return ExecutionGateSnapshot{
		Allowed:              false,
		BlockReason:          reason,
		BlockCode:            blockCode,
		RolloutStageCurrent:  stage,
		RolloutStatusCurrent: status,
		RolloutGateReason:    reason,
	}
}

func NextUnblockCondition(reason string, policy ScalpingCyclePolicy) string {
	spreadThreshold := resolvePolicySpreadThreshold(policy)
	switch strings.TrimSpace(reason) {
	case CandidateRejectConfidenceBelowThreshold:
		return fmt.Sprintf("Await candidate confidence >= %.2f", policy.EffectiveMinConfidence)
	case CandidateRejectSpreadTooWide:
		return fmt.Sprintf("Await candidate spread <= %.2f%%", spreadThreshold)
	case CandidateRejectMissingOrderbookSignal:
		return "Await candidate with valid orderbook imbalance and executable spread"
	case CandidateRejectPreTradeExpectancy:
		return "Await candidate with positive expectancy and stronger regime alignment"
	case CandidateRejectRolloutShadow:
		return "Promote strategy to live via /api/v1/agent/strategy-mode"
	case CandidateRejectRolloutPaper:
		return "Promote strategy from paper to live via /api/v1/agent/strategy-mode"
	case CandidateRejectMaxConcurrentPositions:
		return fmt.Sprintf("Reduce open positions below %d", policy.MaxConcurrentPositions)
	case CandidateRejectAutonomyGateClosed:
		return "Inspect autonomy gate diagnostics and clear the operator block"
	case CandidateRejectAutonomyRuntime:
		return "Retry after autonomy coordinator/runtime connectivity is restored"
	case CandidateRejectSafeMode:
		return "Disable safe mode or clear the rollback trigger before re-enabling entries"
	case CandidateRejectKillSwitch:
		return "Release the kill switch before re-enabling entries"
	case CandidateRejectConnectivity:
		return "Restore exchange/provider connectivity before retrying execution"
	case CandidateRejectRiskBudget:
		return "Reduce exposure or replenish the configured risk budget before retrying"
	default:
		return "Await candidate that passes pretrade validity/liquidity filters"
	}
}

func applyNoFillRecovery(policy *ScalpingCyclePolicy, input ScalpingCycleInput, cfg ScalpingPolicyConfig) {
	if policy == nil {
		return
	}
	if input.ConsecutiveLosses >= 2 {
		return
	}
	if input.DriftActive || input.OpenPositions > 0 {
		return
	}
	if input.NoFillMinutes < float64(cfg.NoFillRecoveryMinutes) {
		return
	}

	minConfidenceFloor := cfg.NoFillMinConfidenceFloor
	if policy.AccountTier == AccountTierMicro &&
		cfg.MicroMinConfidenceFloor > 0 &&
		cfg.MicroMinConfidenceFloor < minConfidenceFloor {
		minConfidenceFloor = cfg.MicroMinConfidenceFloor
	}

	step := int(input.NoFillMinutes / float64(cfg.NoFillRecoveryMinutes))
	switch {
	case step <= 1:
		if policy.EffectiveMinConfidence > 0.75 {
			policy.EffectiveMinConfidence = 0.75
		}
		if policy.EffectiveMaxCapitalPct < 0.50 {
			policy.EffectiveMaxCapitalPct = 0.50
		}
	case step == 2:
		if policy.EffectiveMinConfidence > minConfidenceFloor {
			policy.EffectiveMinConfidence = minConfidenceFloor
		}
		if policy.EffectiveMaxCapitalPct < 1.00 {
			policy.EffectiveMaxCapitalPct = 1.00
		}
	default:
		if policy.EffectiveMinConfidence > minConfidenceFloor {
			policy.EffectiveMinConfidence = minConfidenceFloor
		}
		if policy.EffectiveMaxCapitalPct < cfg.NoFillMaxCapitalPctCap {
			policy.EffectiveMaxCapitalPct = cfg.NoFillMaxCapitalPctCap
		}
	}
	policy.PolicyAdjustments = append(policy.PolicyAdjustments, "controlled_no_fill_recovery")
}

func evaluateCandidateSignal(signal CandidateSignal, policy ScalpingCyclePolicy) (ranked bool, viable bool, rejection CandidateRejection) {
	spreadThreshold := resolvePolicySpreadThreshold(policy)
	symbol := strings.TrimSpace(signal.Symbol)
	rejection = CandidateRejection{
		Symbol:             symbol,
		BidAskSpreadPct:    signal.BidAskSpread,
		OrderBookImbalance: signal.OrderBookImbalance,
		RangePosition24h:   signal.RangePosition24h,
		PriceChange24hPct:  signal.PriceChange24hPct,
	}
	if symbol == "" || signal.Price.LessThanOrEqual(decimal.Zero) {
		rejection.Reason = CandidateRejectMissingOrderbookSignal
		return false, false, rejection
	}

	if hasInvalidCandidateMetrics(signal) || signal.BidAskSpread <= 0 {
		rejection.Reason = CandidateRejectMissingOrderbookSignal
		return false, false, rejection
	}
	if signal.BidAskSpread > spreadThreshold {
		rejection.Reason = CandidateRejectSpreadTooWide
		return true, false, rejection
	}

	action, estimatedConfidence, ok := estimateCandidateConfidence(signal, spreadThreshold)
	rejection.EstimatedConfidence = estimatedConfidence
	if !ok {
		rejection.Reason = CandidateRejectNoDirectionalEdge
		return true, false, rejection
	}
	_ = action
	if estimatedConfidence < policy.EffectiveMinConfidence {
		rejection.Reason = CandidateRejectConfidenceBelowThreshold
		return true, false, rejection
	}
	return true, true, CandidateRejection{}
}

func estimateCandidateConfidence(signal CandidateSignal, spreadThreshold float64) (string, float64, bool) {
	if hasInvalidCandidateMetrics(signal) {
		return "", 0, false
	}

	imbalance := math.Abs(signal.OrderBookImbalance)
	if imbalance < scalpingStrongImbalanceFloor {
		if imbalance < scalpingNeutralImbalanceFloor {
			return "", 0, false
		}
	}

	action := ""
	rangeAlignment := 0.0
	switch {
	case signal.OrderBookImbalance > 0 && signal.RangePosition24h <= scalpingBuyRangeMax:
		action = "buy"
		rangeAlignment = clampFloat((scalpingBuyRangeMax-signal.RangePosition24h)/scalpingBuyRangeMax, 0, 1)
	case signal.OrderBookImbalance < 0 && signal.RangePosition24h >= scalpingSellRangeMin:
		action = "sell"
		rangeAlignment = clampFloat((signal.RangePosition24h-scalpingSellRangeMin)/(100.0-scalpingSellRangeMin), 0, 1)
	case signal.OrderBookImbalance > 0 && signal.RangePosition24h <= scalpingBuyRangeMax+scalpingContinuationRangeBuffer:
		action = "buy"
		rangeAlignment = clampFloat((scalpingBuyRangeMax+scalpingContinuationRangeBuffer-signal.RangePosition24h)/(scalpingBuyRangeMax+scalpingContinuationRangeBuffer), 0, 1)
	case signal.OrderBookImbalance < 0 && signal.RangePosition24h >= scalpingSellRangeMin-scalpingContinuationRangeBuffer:
		action = "sell"
		rangeAlignment = clampFloat((signal.RangePosition24h-(scalpingSellRangeMin-scalpingContinuationRangeBuffer))/(100.0-(scalpingSellRangeMin-scalpingContinuationRangeBuffer)), 0, 1)
	case signal.OrderBookImbalance >= scalpingStrongImbalanceFloor &&
		signal.PriceChange24hPct > 0 &&
		signal.RangePosition24h <= scalpingSellRangeMin+scalpingContinuationRangeBuffer:
		action = "buy"
		rangeAlignment = continuationRangeAlignment(
			scalpingBuyRangeMax,
			scalpingSellRangeMin+scalpingContinuationRangeBuffer,
			signal.RangePosition24h,
			false,
		)
	case signal.OrderBookImbalance <= -scalpingStrongImbalanceFloor &&
		signal.PriceChange24hPct < 0 &&
		signal.RangePosition24h >= scalpingBuyRangeMax-scalpingContinuationRangeBuffer:
		action = "sell"
		rangeAlignment = continuationRangeAlignment(
			scalpingBuyRangeMax-scalpingContinuationRangeBuffer,
			scalpingSellRangeMin,
			signal.RangePosition24h,
			true,
		)
	default:
		return "", 0, false
	}

	liquidityScore := clampFloat(1-(signal.BidAskSpread/spreadThreshold), 0, 1)
	volumeScore := clampFloat(math.Log10(signal.Volume24h.InexactFloat64()+1)/scalpingConfidenceVolumeLogBase, 0, 1)
	confidence := scalpingConfidenceBase +
		imbalance*scalpingConfidenceImbalanceW +
		liquidityScore*scalpingConfidenceLiquidityW +
		rangeAlignment*scalpingConfidenceRangeW +
		volumeScore*scalpingConfidenceVolumeW
	confidence = clampFloat(confidence, 0, 0.99)
	return action, confidence, true
}

func continuationRangeAlignment(low, high, value float64, rising bool) float64 {
	if high <= low {
		return 0
	}
	if rising {
		return clampFloat((value-low)/(high-low), 0, 1)
	}
	return clampFloat((high-value)/(high-low), 0, 1)
}

func hasInvalidCandidateMetrics(signal CandidateSignal) bool {
	volume := signal.Volume24h.InexactFloat64()
	return invalidCandidateFloat(signal.BidAskSpread) ||
		invalidCandidateFloat(signal.OrderBookImbalance) ||
		invalidCandidateFloat(signal.RangePosition24h) ||
		invalidCandidateFloat(signal.PriceChange24hPct) ||
		math.IsNaN(volume) ||
		math.IsInf(volume, 0) ||
		volume < 0
}

func invalidCandidateFloat(value float64) bool {
	return math.IsNaN(value) || math.IsInf(value, 0)
}

func resolvePolicySpreadThreshold(policy ScalpingCyclePolicy) float64 {
	if !math.IsNaN(policy.MaxBidAskSpreadPct) && !math.IsInf(policy.MaxBidAskSpreadPct, 0) && policy.MaxBidAskSpreadPct > 0 {
		return NormalizeScalpingMaxBidAskSpreadPct(policy.MaxBidAskSpreadPct)
	}
	return ResolveScalpingMaxBidAskSpreadPctFromEnv()
}

func NormalizeScalpingMaxBidAskSpreadPct(value float64) float64 {
	return clampFloat(value, minScalpingMaxBidAskSpreadPct, maxScalpingMaxBidAskSpreadPct)
}

func ResolveScalpingMaxBidAskSpreadPctFromEnv() float64 {
	raw := strings.TrimSpace(os.Getenv(NeuraScalpingMaxBidAskSpreadPctEnv))
	if raw == "" {
		raw = strings.TrimSpace(os.Getenv(ScalpingMaxBidAskSpreadPctEnv))
	}
	if raw == "" {
		return DefaultScalpingMaxBidAskSpreadPct
	}
	value, err := strconv.ParseFloat(raw, 64)
	if err != nil || math.IsNaN(value) || math.IsInf(value, 0) || value <= 0 {
		return DefaultScalpingMaxBidAskSpreadPct
	}
	return NormalizeScalpingMaxBidAskSpreadPct(value)
}

func clampFloat(value, minValue, maxValue float64) float64 {
	if math.IsNaN(value) {
		return minValue
	}
	if math.IsInf(value, -1) {
		return minValue
	}
	if math.IsInf(value, 1) {
		return maxValue
	}
	if value < minValue {
		return minValue
	}
	if value > maxValue {
		return maxValue
	}
	return value
}

func hasAnyPolicyAdjustment(adjustments []string, targets ...string) bool {
	for _, adjustment := range adjustments {
		for _, target := range targets {
			if adjustment == target {
				return true
			}
		}
	}
	return false
}
