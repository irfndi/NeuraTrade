package autonomy

import (
	"strings"
	"testing"
	"time"
)

func TestEvaluateRecoveryGate_MicroEntryWaitThenUnlock(t *testing.T) {
	cfg := DefaultRecoveryConfig()
	cfg.RequiredCleanCycles = 1

	waitState := EvaluateRecoveryGate(RecoveryGateInput{
		Drawdown:    0.3728,
		CleanCycles: 0,
	}, cfg)
	if waitState.Mode != RecoveryModeMicroEntry {
		t.Fatalf("expected mode %q, got %q", RecoveryModeMicroEntry, waitState.Mode)
	}
	if waitState.EntryAllowed {
		t.Fatal("expected entry to be blocked while clean cycles are below requirement")
	}
	if waitState.CyclesToEntry != 1 {
		t.Fatalf("expected cycles_to_entry=1, got %d", waitState.CyclesToEntry)
	}

	unlocked := EvaluateRecoveryGate(RecoveryGateInput{
		Drawdown:    0.3728,
		CleanCycles: 1,
	}, cfg)
	if !unlocked.EntryAllowed {
		t.Fatalf("expected entry to be allowed after required clean cycles, reason=%q", unlocked.GateReason)
	}
	if unlocked.CyclesToEntry != 0 {
		t.Fatalf("expected cycles_to_entry=0, got %d", unlocked.CyclesToEntry)
	}
}

func TestEvaluateRecoveryGate_DeriskOnlyResets(t *testing.T) {
	cfg := DefaultRecoveryConfig()
	state := EvaluateRecoveryGate(RecoveryGateInput{
		Drawdown:    0.41,
		CleanCycles: 4,
	}, cfg)
	if state.Mode != RecoveryModeDeriskOnly {
		t.Fatalf("expected derisk mode, got %q", state.Mode)
	}
	if state.EntryAllowed {
		t.Fatal("expected entry to be blocked in derisk mode")
	}

	update := DecideRecoveryCycleUpdate(state, cfg)
	if !update.ShouldResetCycles {
		t.Fatal("expected derisk-only mode to reset clean cycles")
	}
	if update.ResetReason != "drawdown_derisk_only" {
		t.Fatalf("expected drawdown_derisk_only reset, got %q", update.ResetReason)
	}
}

func TestEvaluateRecoveryGate_RuntimeFailureStreakDoesNotBlockMicroEntryByItself(t *testing.T) {
	cfg := DefaultRecoveryConfig()

	state := EvaluateRecoveryGate(RecoveryGateInput{
		Drawdown:             0.3728,
		CleanCycles:          cfg.RequiredCleanCycles,
		RuntimeFailureStreak: 1,
	}, cfg)

	if state.Mode != RecoveryModeMicroEntry {
		t.Fatalf("expected micro-entry mode, got %q", state.Mode)
	}
	if !state.EntryAllowed {
		t.Fatalf("expected runtime failure streak alone not to block micro-entry, reason=%q next=%q", state.GateReason, state.NextCondition)
	}
}

func TestEvaluateRecoveryGate_DriftBlocksEntry(t *testing.T) {
	cfg := DefaultRecoveryConfig()

	state := EvaluateRecoveryGate(RecoveryGateInput{
		Drawdown:    0.3728,
		CleanCycles: cfg.RequiredCleanCycles,
		DriftActive: true,
	}, cfg)

	if state.Mode != RecoveryModeMicroEntry {
		t.Fatalf("expected micro-entry mode, got %q", state.Mode)
	}
	if state.EntryAllowed {
		t.Fatal("expected drift gate to block entry")
	}
	if state.GateReason == "" || state.NextCondition == "" {
		t.Fatalf("expected drift block messaging, got reason=%q next=%q", state.GateReason, state.NextCondition)
	}
	if state.NextCondition != "Clear drift state" {
		t.Fatalf("expected drift next condition, got %q", state.NextCondition)
	}
}

func TestEvaluateRecoveryGate_RecentLossStreakGatesMicroEntry(t *testing.T) {
	cfg := DefaultRecoveryConfig()

	belowBudget := EvaluateRecoveryGate(RecoveryGateInput{
		Drawdown:         0.3728,
		CleanCycles:      cfg.RequiredCleanCycles,
		RecentLossActive: true,
		RecentLossStreak: cfg.LossStreakBudget - 1,
		RecentLossWindow: 90 * time.Minute,
	}, cfg)
	if !belowBudget.EntryAllowed {
		t.Fatalf("expected entry to be allowed below loss-streak budget, reason=%q", belowBudget.GateReason)
	}

	blocked := EvaluateRecoveryGate(RecoveryGateInput{
		Drawdown:         0.3728,
		CleanCycles:      cfg.RequiredCleanCycles,
		RecentLossActive: true,
		RecentLossStreak: cfg.LossStreakBudget,
		RecentLossWindow: 90 * time.Minute,
	}, cfg)
	if blocked.EntryAllowed {
		t.Fatal("expected entry to be blocked at loss-streak budget")
	}
	if !strings.Contains(blocked.GateReason, "micro-entry paused") {
		t.Fatalf("expected micro-entry recent-loss gate reason, got %q", blocked.GateReason)
	}
	if !strings.Contains(blocked.NextCondition, "Need loss streak below") {
		t.Fatalf("expected recent-loss next condition, got %q", blocked.NextCondition)
	}
}

func TestEvaluateRecoveryGate_RecentLossStreakDoesNotBlockNormalMode(t *testing.T) {
	cfg := DefaultRecoveryConfig()

	state := EvaluateRecoveryGate(RecoveryGateInput{
		Drawdown:         0.10,
		CleanCycles:      cfg.RequiredCleanCycles,
		RecentLossActive: true,
		RecentLossStreak: cfg.LossStreakBudget,
		RecentLossWindow: 90 * time.Minute,
	}, cfg)

	if state.Mode != RecoveryModeNormal {
		t.Fatalf("expected normal mode, got %q", state.Mode)
	}
	if !state.EntryAllowed {
		t.Fatalf("expected normal mode to stay eligible and rely on policy tightening, reason=%q", state.GateReason)
	}
	if state.GateReason != "" {
		t.Fatalf("expected no hard recovery gate reason in normal mode, got %q", state.GateReason)
	}
}

func TestDecideRecoveryCycleUpdate_UsesConfiguredLossStreakBudget(t *testing.T) {
	cfg := DefaultRecoveryConfig()
	cfg.LossStreakBudget = 4

	noReset := DecideRecoveryCycleUpdate(RecoveryGateState{
		Mode:             RecoveryModeMicroEntry,
		EntryAllowed:     false,
		RecentLossActive: true,
		RecentLossStreak: 3,
	}, cfg)
	if noReset.ShouldResetCycles {
		t.Fatalf("expected configured loss budget to suppress reset, got %+v", noReset)
	}

	reset := DecideRecoveryCycleUpdate(RecoveryGateState{
		Mode:             RecoveryModeMicroEntry,
		EntryAllowed:     false,
		RecentLossActive: true,
		RecentLossStreak: 4,
	}, cfg)
	if !reset.ShouldResetCycles {
		t.Fatal("expected reset when recent losses meet configured budget")
	}
	if reset.ResetReason != "recent_loss_streak" {
		t.Fatalf("expected recent_loss_streak reset, got %q", reset.ResetReason)
	}
}

func TestEvaluateEntryAttemptGate_DefaultBudget(t *testing.T) {
	now := time.Now().UTC()
	state := EvaluateEntryAttemptGate(EntryAttemptGateInput{
		Now:                    now,
		DeployableBalanceRatio: 0.12,
		OpenPositions:          0,
		DriftActive:            false,
		RecoveryEntryAllowed:   true,
		NoFillMinutes:          120,
		HasWindow:              true,
		WindowStartedAt:        now.Add(-10 * time.Minute),
		AttemptsInWindow:       5,
	}, DefaultLivenessConfig())
	if !state.Forced {
		t.Fatal("expected forced liveness mode")
	}
	if state.AllowNow {
		t.Fatal("expected attempt budget to block new attempt at 5/5")
	}
}

func TestEvaluateEntryAttemptGate_NonForcedMode(t *testing.T) {
	now := time.Now().UTC()
	state := EvaluateEntryAttemptGate(EntryAttemptGateInput{
		Now:                    now,
		DeployableBalanceRatio: 0.12,
		OpenPositions:          1,
		RecoveryEntryAllowed:   true,
		NoFillMinutes:          120,
		HasWindow:              true,
		WindowStartedAt:        now.Add(-10 * time.Minute),
		AttemptsInWindow:       2,
	}, DefaultLivenessConfig())

	if state.Forced {
		t.Fatal("expected non-forced mode when positions are open")
	}
	if !state.AllowNow {
		t.Fatal("expected attempts to remain allowed in non-forced mode")
	}
	if !strings.Contains(state.NextCondition, "Force liveness mode starts after") {
		t.Fatalf("expected non-forced next condition, got %q", state.NextCondition)
	}
}

func TestEvaluateEntryAttemptGate_WindowResetWithoutWindow(t *testing.T) {
	now := time.Now().UTC()
	state := EvaluateEntryAttemptGate(EntryAttemptGateInput{
		Now:                    now,
		DeployableBalanceRatio: 0.12,
		OpenPositions:          0,
		RecoveryEntryAllowed:   true,
		NoFillMinutes:          120,
		HasWindow:              false,
		AttemptsInWindow:       3,
	}, DefaultLivenessConfig())

	if !state.Forced {
		t.Fatal("expected forced mode when no-fill conditions are met")
	}
	if !state.AllowNow {
		t.Fatal("expected a fresh window to allow entry")
	}
	if state.AttemptsInWindow != 0 {
		t.Fatalf("expected attempts to reset to 0, got %d", state.AttemptsInWindow)
	}
	assertTimeNear(t, state.WindowStartedAt, now)
}

func TestEvaluateEntryAttemptGate_WindowResetExpiredWindow(t *testing.T) {
	now := time.Now().UTC()
	state := EvaluateEntryAttemptGate(EntryAttemptGateInput{
		Now:                    now,
		DeployableBalanceRatio: 0.12,
		OpenPositions:          0,
		RecoveryEntryAllowed:   true,
		NoFillMinutes:          120,
		HasWindow:              true,
		WindowStartedAt:        now.Add(-2 * time.Hour),
		AttemptsInWindow:       4,
	}, DefaultLivenessConfig())

	if !state.Forced {
		t.Fatal("expected forced mode after window rollover")
	}
	if !state.AllowNow {
		t.Fatal("expected a rolled-over window to allow entry")
	}
	if state.AttemptsInWindow != 0 {
		t.Fatalf("expected attempts to reset to 0 on rollover, got %d", state.AttemptsInWindow)
	}
	assertTimeNear(t, state.WindowStartedAt, now)
}

func TestEvaluateEntryAttemptGate_ForcedModeRemainingCapacity(t *testing.T) {
	now := time.Now().UTC()
	state := EvaluateEntryAttemptGate(EntryAttemptGateInput{
		Now:                    now,
		DeployableBalanceRatio: 0.12,
		OpenPositions:          0,
		RecoveryEntryAllowed:   true,
		NoFillMinutes:          120,
		HasWindow:              true,
		WindowStartedAt:        now.Add(-10 * time.Minute),
		AttemptsInWindow:       2,
	}, DefaultLivenessConfig())

	if !state.Forced {
		t.Fatal("expected forced mode")
	}
	if !state.AllowNow {
		t.Fatal("expected entry to remain allowed while budget is available")
	}
	if !strings.Contains(state.NextCondition, "Liveness attempt budget available") {
		t.Fatalf("expected remaining-budget next condition, got %q", state.NextCondition)
	}
}

func assertTimeNear(t *testing.T, got time.Time, want time.Time) {
	t.Helper()
	if got.Before(want.Add(-time.Second)) || got.After(want.Add(time.Second)) {
		t.Fatalf("expected time near %s, got %s", want, got)
	}
}
