package autonomy

import (
	"fmt"
	"time"
)

const (
	RecoveryModeNormal     = "normal"
	RecoveryModeDeriskOnly = "derisk_only"
	RecoveryModeMicroEntry = "micro_entry"
)

const (
	DefaultRecoveryCleanCycles        = 1
	DefaultRecoveryDeriskOnlyDrawdown = 0.40
	DefaultRecoveryMicroEntryDrawdown = 0.30
	DefaultRecoveryMicroEntryCapPct   = 0.50
	DefaultRecoveryLossStreakBudget   = 2
	DefaultLivenessIdleMinutes        = 45
	DefaultLivenessMaxAttemptsPerHour = 5
)

type RecoveryConfig struct {
	RequiredCleanCycles int
	DeriskOnlyThreshold float64
	MicroEntryThreshold float64
	MicroEntryCapPct    float64
	LossStreakBudget    int
}

func DefaultRecoveryConfig() RecoveryConfig {
	return RecoveryConfig{
		RequiredCleanCycles: DefaultRecoveryCleanCycles,
		DeriskOnlyThreshold: DefaultRecoveryDeriskOnlyDrawdown,
		MicroEntryThreshold: DefaultRecoveryMicroEntryDrawdown,
		MicroEntryCapPct:    DefaultRecoveryMicroEntryCapPct,
		LossStreakBudget:    DefaultRecoveryLossStreakBudget,
	}
}

func (c RecoveryConfig) Normalized() RecoveryConfig {
	if c.RequiredCleanCycles <= 0 {
		c.RequiredCleanCycles = DefaultRecoveryCleanCycles
	}
	c.RequiredCleanCycles = clampInt(c.RequiredCleanCycles, 1, 20)

	if c.DeriskOnlyThreshold <= 0 {
		c.DeriskOnlyThreshold = DefaultRecoveryDeriskOnlyDrawdown
	}
	if c.DeriskOnlyThreshold < 0.10 {
		c.DeriskOnlyThreshold = 0.10
	}

	if c.MicroEntryThreshold <= 0 {
		c.MicroEntryThreshold = DefaultRecoveryMicroEntryDrawdown
	}
	if c.MicroEntryThreshold < 0 {
		c.MicroEntryThreshold = 0
	}
	if c.MicroEntryThreshold > c.DeriskOnlyThreshold {
		c.MicroEntryThreshold = c.DeriskOnlyThreshold
	}

	if c.MicroEntryCapPct <= 0 {
		c.MicroEntryCapPct = DefaultRecoveryMicroEntryCapPct
	}
	if c.MicroEntryCapPct < 0.10 {
		c.MicroEntryCapPct = 0.10
	}

	if c.LossStreakBudget <= 0 {
		c.LossStreakBudget = DefaultRecoveryLossStreakBudget
	}
	c.LossStreakBudget = clampInt(c.LossStreakBudget, 1, 20)
	return c
}

type RecoveryGateInput struct {
	Drawdown              float64
	CleanCycles           int
	DriftActive           bool
	RuntimeFailureStreak  int
	RecentLossStreak      int
	RecentLossActive      bool
	RecentLossWindow      time.Duration
	RecentLossLastTradeAt time.Time
}

type RecoveryGateState struct {
	Mode                  string
	EntryAllowed          bool
	CleanCycles           int
	RequiredCleanCycles   int
	CyclesToEntry         int
	DeriskOnlyThreshold   float64
	MicroEntryThreshold   float64
	MicroEntryCapPct      float64
	RecentLossStreak      int
	RecentLossActive      bool
	RecentLossWindow      time.Duration
	RecentLossLastTradeAt time.Time
	GateReason            string
	NextCondition         string
}

type RecoveryGateDecision struct {
	State             RecoveryGateState
	ShouldResetCycles bool
	ResetReason       string
}

func EvaluateRecoveryGate(input RecoveryGateInput, cfg RecoveryConfig) RecoveryGateState {
	cfg = cfg.Normalized()
	driftBlocked := input.DriftActive

	state := RecoveryGateState{
		Mode:                  RecoveryModeNormal,
		EntryAllowed:          true,
		CleanCycles:           max(input.CleanCycles, 0),
		RequiredCleanCycles:   cfg.RequiredCleanCycles,
		DeriskOnlyThreshold:   cfg.DeriskOnlyThreshold,
		MicroEntryThreshold:   cfg.MicroEntryThreshold,
		MicroEntryCapPct:      cfg.MicroEntryCapPct,
		RecentLossStreak:      max(input.RecentLossStreak, 0),
		RecentLossActive:      input.RecentLossActive,
		RecentLossWindow:      input.RecentLossWindow,
		RecentLossLastTradeAt: input.RecentLossLastTradeAt,
	}

	if driftBlocked {
		state.EntryAllowed = false
	}
	switch {
	case input.Drawdown >= cfg.DeriskOnlyThreshold:
		state.Mode = RecoveryModeDeriskOnly
		state.EntryAllowed = false
		state.GateReason = fmt.Sprintf(
			"drawdown %.2f%% above %.2f%% threshold: de-risk only",
			input.Drawdown*100,
			cfg.DeriskOnlyThreshold*100,
		)
		state.NextCondition = fmt.Sprintf("Reduce drawdown below %.2f%%", cfg.DeriskOnlyThreshold*100)
	case input.Drawdown >= cfg.MicroEntryThreshold:
		state.Mode = RecoveryModeMicroEntry
		if state.CleanCycles < cfg.RequiredCleanCycles {
			state.EntryAllowed = false
			state.GateReason = fmt.Sprintf(
				"drawdown %.2f%% in recovery band: waiting for clean cycles before micro-entry",
				input.Drawdown*100,
			)
			state.NextCondition = fmt.Sprintf(
				"Reach %d clean cycle(s) (current %d)",
				cfg.RequiredCleanCycles,
				state.CleanCycles,
			)
		} else if recoveryLossStreakExceeded(state, cfg) {
			state.EntryAllowed = false
			applyRecentLossGate(&state, cfg)
		} else if driftBlocked {
			state.EntryAllowed = false
			applyDriftGate(&state, driftBlocked)
		}
	default:
		state.Mode = RecoveryModeNormal
	}
	if !state.EntryAllowed && state.GateReason == "" {
		applyDriftGate(&state, driftBlocked)
	}
	if !state.EntryAllowed && state.GateReason == "" {
		state.GateReason = "recovery entry gate is active"
	}

	if state.Mode == RecoveryModeMicroEntry {
		state.CyclesToEntry = max(cfg.RequiredCleanCycles-state.CleanCycles, 0)
	}
	return state
}

func DecideRecoveryCycleUpdate(state RecoveryGateState, cfg RecoveryConfig) RecoveryGateDecision {
	decision := RecoveryGateDecision{State: state}
	cfg = cfg.Normalized()

	switch {
	case state.Mode == RecoveryModeDeriskOnly:
		decision.ShouldResetCycles = true
		decision.ResetReason = "drawdown_derisk_only"
	case !state.EntryAllowed && state.Mode == RecoveryModeMicroEntry &&
		recoveryLossStreakExceeded(state, cfg):
		decision.ShouldResetCycles = true
		decision.ResetReason = "recent_loss_streak"
	}
	return decision
}

func recoveryLossStreakExceeded(state RecoveryGateState, cfg RecoveryConfig) bool {
	return state.RecentLossActive && state.RecentLossStreak >= cfg.LossStreakBudget
}

func applyRecentLossGate(state *RecoveryGateState, cfg RecoveryConfig) {
	if state == nil {
		return
	}
	window := roundDurationString(state.RecentLossWindow, time.Minute)
	if state.Mode == RecoveryModeMicroEntry {
		state.GateReason = fmt.Sprintf(
			"recent loss streak %d within %s exceeds threshold; micro-entry paused",
			state.RecentLossStreak,
			window,
		)
	} else {
		state.GateReason = fmt.Sprintf(
			"recent loss streak %d still active within %s window",
			state.RecentLossStreak,
			window,
		)
	}
	state.NextCondition = fmt.Sprintf(
		"Need loss streak below %d within %s window",
		cfg.LossStreakBudget,
		window,
	)
}

func applyDriftGate(state *RecoveryGateState, driftBlocked bool) {
	if state == nil || !driftBlocked {
		return
	}
	if state.Mode == RecoveryModeMicroEntry {
		state.GateReason = "recovery micro-entry paused until drift constraints clear"
	} else {
		state.GateReason = "recovery entry gate is active: drift constraints must clear"
	}
	state.NextCondition = "Clear drift state"
}

type LivenessConfig struct {
	IdleMinutes        int
	MaxAttemptsPerHour int
}

func DefaultLivenessConfig() LivenessConfig {
	return LivenessConfig{
		IdleMinutes:        DefaultLivenessIdleMinutes,
		MaxAttemptsPerHour: DefaultLivenessMaxAttemptsPerHour,
	}
}

func (c LivenessConfig) Normalized() LivenessConfig {
	if c.IdleMinutes <= 0 {
		c.IdleMinutes = DefaultLivenessIdleMinutes
	}
	c.IdleMinutes = clampInt(c.IdleMinutes, 5, 24*60)

	if c.MaxAttemptsPerHour <= 0 {
		c.MaxAttemptsPerHour = DefaultLivenessMaxAttemptsPerHour
	}
	c.MaxAttemptsPerHour = clampInt(c.MaxAttemptsPerHour, 1, 60)
	return c
}

type EntryAttemptGateInput struct {
	Now                    time.Time
	DeployableBalanceRatio float64
	OpenPositions          int
	DriftActive            bool
	RecoveryEntryAllowed   bool
	NoFillMinutes          float64
	HasWindow              bool
	WindowStartedAt        time.Time
	AttemptsInWindow       int
}

type EntryAttemptGateState struct {
	Forced                 bool
	AllowNow               bool
	AttemptsInWindow       int
	MaxAttemptsPerHour     int
	DeployableBalanceRatio float64
	BlockReason            string
	NextCondition          string
	AttemptWindowProgress  string
	WindowStartedAt        time.Time
}

func EvaluateEntryAttemptGate(input EntryAttemptGateInput, cfg LivenessConfig) EntryAttemptGateState {
	cfg = cfg.Normalized()
	now := input.Now.UTC()
	if now.IsZero() {
		now = time.Now().UTC()
	}
	attempts := max(input.AttemptsInWindow, 0)
	windowStart := input.WindowStartedAt.UTC()
	if !input.HasWindow || windowStart.IsZero() || now.Sub(windowStart) >= time.Hour {
		windowStart = now
		attempts = 0
	}

	state := EntryAttemptGateState{
		Forced:                 false,
		AllowNow:               true,
		AttemptsInWindow:       attempts,
		MaxAttemptsPerHour:     cfg.MaxAttemptsPerHour,
		DeployableBalanceRatio: input.DeployableBalanceRatio,
		WindowStartedAt:        windowStart,
	}
	state.AttemptWindowProgress = fmt.Sprintf("%d/%d in current 1h window", attempts, cfg.MaxAttemptsPerHour)

	state.Forced = input.DeployableBalanceRatio >= 0.05 &&
		input.OpenPositions == 0 &&
		!input.DriftActive &&
		input.RecoveryEntryAllowed &&
		input.NoFillMinutes >= float64(cfg.IdleMinutes)

	if !state.Forced {
		state.NextCondition = fmt.Sprintf(
			"Force liveness mode starts after %dm no-fill with deployable balance >=5%%",
			cfg.IdleMinutes,
		)
		return state
	}

	if attempts >= cfg.MaxAttemptsPerHour {
		state.AllowNow = false
		state.BlockReason = fmt.Sprintf(
			"liveness entry-attempt budget reached: %d/%d in current 1h window",
			attempts,
			cfg.MaxAttemptsPerHour,
		)
		state.NextCondition = fmt.Sprintf(
			"Next entry-attempt window opens at %s",
			windowStart.Add(time.Hour).UTC().Format(time.RFC3339),
		)
		return state
	}

	state.NextCondition = fmt.Sprintf(
		"Liveness attempt budget available: %d/%d used in current 1h window",
		attempts,
		cfg.MaxAttemptsPerHour,
	)
	return state
}

func roundDurationString(d time.Duration, roundTo time.Duration) string {
	if d <= 0 {
		return "0s"
	}
	if roundTo <= 0 {
		return d.String()
	}
	return d.Round(roundTo).String()
}

func clampInt(value, min, max int) int {
	if value < min {
		return min
	}
	if value > max {
		return max
	}
	return value
}
