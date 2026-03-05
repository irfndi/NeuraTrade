package autonomy

import (
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
	state := EvaluateRecoveryGate(RecoveryGateInput{
		Drawdown:    0.41,
		CleanCycles: 4,
	}, DefaultRecoveryConfig())
	if state.Mode != RecoveryModeDeriskOnly {
		t.Fatalf("expected derisk mode, got %q", state.Mode)
	}
	if state.EntryAllowed {
		t.Fatal("expected entry to be blocked in derisk mode")
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
