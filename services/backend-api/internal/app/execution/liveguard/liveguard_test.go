package liveguard

import (
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/shopspring/decimal"
)

func newTestGuard() *Guard {
	cfg := Config{
		Enabled:       true,
		ArmRequired:   true,
		SizeCapPct:    decimal.RequireFromString("0.10"),
		FirstNHold:    5,
		ConfirmPhrase: "test-phrase-1234",
	}
	return New(cfg)
}

func TestGuard_DisarmedByDefault(t *testing.T) {
	g := newTestGuard()
	if g.IsArmed() {
		t.Fatal("guard should start disarmed")
	}
	res, err := g.CheckOrder("i1", "chat-1", "strat", "BTC/USDT", "buy", "market", decimal.RequireFromString("100"), true)
	if err == nil {
		t.Fatal("expected ErrNotArmed")
	}
	if res.Allowed {
		t.Fatal("expected disallowed")
	}
	if !strings.Contains(err.Error(), "not armed") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestGuard_DisabledFlag(t *testing.T) {
	g := New(Config{Enabled: false, ArmRequired: false, SizeCapPct: decimal.NewFromInt(1), FirstNHold: 0})
	_, err := g.CheckOrder("i1", "chat-1", "strat", "BTC/USDT", "buy", "market", decimal.RequireFromString("1"), true)
	if err != ErrGuardDisabled {
		t.Fatalf("expected ErrGuardDisabled, got %v", err)
	}
}

func TestGuard_ArmRequiresCorrectPhrase(t *testing.T) {
	g := newTestGuard()
	if err := g.Arm("op", "wrong", "", false); err != ErrBadConfirmation {
		t.Fatalf("expected ErrBadConfirmation, got %v", err)
	}
	if g.IsArmed() {
		t.Fatal("must not arm with bad phrase")
	}
}

func TestGuard_ArmAndDisarm(t *testing.T) {
	g := newTestGuard()
	if err := g.Arm("op", "test-phrase-1234", "first-time", false); err != nil {
		t.Fatalf("arm failed: %v", err)
	}
	if !g.IsArmed() {
		t.Fatal("should be armed")
	}
	if err := g.Arm("op2", "test-phrase-1234", "redundant", false); err != ErrAlreadyArmed {
		t.Fatalf("expected ErrAlreadyArmed, got %v", err)
	}
	g.Disarm("op", "switch to paper")
	if g.IsArmed() {
		t.Fatal("should be disarmed")
	}
	// double-disarm is safe
	g.Disarm("op2", "noop")
}

func TestGuard_ChatMustBeLive(t *testing.T) {
	g := newTestGuard()
	if err := g.Arm("op", "test-phrase-1234", "", false); err != nil {
		t.Fatal(err)
	}
	_, err := g.CheckOrder("i1", "chat-1", "s", "BTC/USDT", "buy", "market", decimal.RequireFromString("1"), false)
	if err != ErrChatNotInLiveMode {
		t.Fatalf("expected ErrChatNotInLiveMode, got %v", err)
	}
}

func TestGuard_SizeCapApplied(t *testing.T) {
	g := newTestGuard()
	if err := g.Arm("op", "test-phrase-1234", "", false); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 5; i++ {
		intent := "prefill-" + string(rune('a'+i))
		_, _ = g.CheckOrder(intent, "chat-1", "s", "BTC/USDT", "buy", "market", decimal.NewFromInt(1), true)
		g.RecordPlaced(intent)
	}
	res, err := g.CheckOrder("i1", "chat-1", "s", "BTC/USDT", "buy", "market", decimal.RequireFromString("1000"), true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !res.Allowed {
		t.Fatal("expected allowed (past first-N)")
	}
	if !res.WasCapped {
		t.Fatal("expected size cap applied")
	}
	if !res.CappedAmount.Equal(decimal.RequireFromString("100")) {
		t.Fatalf("expected capped to 100, got %s", res.CappedAmount)
	}
}

func TestGuard_FirstNHoldRequiresApproval(t *testing.T) {
	g := newTestGuard()
	if err := g.Arm("op", "test-phrase-1234", "", false); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 5; i++ {
		intent := "intent-" + string(rune('a'+i))
		res, err := g.CheckOrder(intent, "chat-1", "s", "BTC/USDT", "buy", "market", decimal.RequireFromString("1"), true)
		if err != ErrOrderPending {
			t.Fatalf("intent %d: expected ErrOrderPending, got %v", i, err)
		}
		if !res.Pending {
			t.Fatalf("intent %d: expected pending=true", i)
		}
		if len(g.PendingOrders()) != i+1 {
			t.Fatalf("pending count: want %d, got %d", i+1, len(g.PendingOrders()))
		}
	}
	for i := 0; i < 5; i++ {
		intent := "intent-" + string(rune('a'+i))
		if _, err := g.ApproveOrder(intent, "op"); err != nil {
			t.Fatalf("approve %d: %v", i, err)
		}
		g.RecordPlaced(intent)
	}
	res, err := g.CheckOrder("intent-f", "chat-1", "s", "BTC/USDT", "buy", "market", decimal.RequireFromString("1"), true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !res.Allowed {
		t.Fatal("expected 6th order allowed after first 5 placed")
	}
}

func TestGuard_ApproveOrderReleases(t *testing.T) {
	g := newTestGuard()
	if err := g.Arm("op", "test-phrase-1234", "", false); err != nil {
		t.Fatal(err)
	}
	_, err := g.CheckOrder("intent-a", "chat-1", "s", "BTC/USDT", "buy", "market", decimal.RequireFromString("1"), true)
	if err != ErrOrderPending {
		t.Fatal(err)
	}
	po, err := g.ApproveOrder("intent-a", "operator1")
	if err != nil {
		t.Fatal(err)
	}
	if po.IntentID != "intent-a" {
		t.Fatalf("unexpected po: %+v", po)
	}
	if len(g.PendingOrders()) != 0 {
		t.Fatal("pending list should be empty after approval")
	}
	// Approving the same intent again is an error.
	if _, err := g.ApproveOrder("intent-a", "operator1"); err != ErrUnknownOrder {
		t.Fatalf("expected ErrUnknownOrder, got %v", err)
	}
}

func TestGuard_RejectPending(t *testing.T) {
	g := newTestGuard()
	if err := g.Arm("op", "test-phrase-1234", "", false); err != nil {
		t.Fatal(err)
	}
	_, _ = g.CheckOrder("intent-a", "chat-1", "s", "BTC/USDT", "buy", "market", decimal.RequireFromString("1"), true)
	if err := g.RejectPending("intent-a", "op2", "size too large"); err != nil {
		t.Fatal(err)
	}
	st := g.Status()
	if st.Rejected < 1 {
		t.Fatal("rejected counter should increment")
	}
	if len(st.RecentRejects) < 1 {
		t.Fatal("recent_rejects should record the rejection")
	}
}

func TestGuard_RecordPlacedAdvancesCount(t *testing.T) {
	g := newTestGuard()
	if err := g.Arm("op", "test-phrase-1234", "", false); err != nil {
		t.Fatal(err)
	}
	approveCount := 0
	for i := 0; i < 7; i++ {
		intent := "intent-" + string(rune('a'+i))
		_, err := g.CheckOrder(intent, "chat-1", "s", "BTC/USDT", "buy", "market", decimal.RequireFromString("1"), true)
		if err == ErrOrderPending {
			if _, err := g.ApproveOrder(intent, "op"); err != nil {
				t.Fatal(err)
			}
			approveCount++
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		g.RecordPlaced(intent)
	}
	st := g.Status()
	// Fix for NeuraTrade-sjm7: approved orders also advance placedLive so the
	// FirstNHold gate releases after N approvals (not just N placements).
	// Every order in the loop either was approved (advances placedLive) or
	// auto-allowed and then recorded as placed (also advances placedLive).
	if st.PlacedLive != 7 {
		t.Fatalf("PlacedLive: want 7 (all orders advance the counter), got %d (approved %d pending orders)", st.PlacedLive, approveCount)
	}
	if st.Capped != 7 {
		t.Fatalf("Capped: want 7 (all small orders get 10%% cap), got %d", st.Capped)
	}
}

func TestGuard_LoadConfigDefaults(t *testing.T) {
	t.Setenv("NEURATRADE_LIVE_GUARD_ENABLED", "")
	t.Setenv("NEURATRADE_LIVE_GUARD_ARM_REQUIRED", "")
	t.Setenv("NEURATRADE_LIVE_GUARD_SIZE_CAP_PCT", "")
	t.Setenv("NEURATRADE_LIVE_GUARD_FIRST_N_HOLD", "")
	t.Setenv("NEURATRADE_LIVE_GUARD_CONFIRM_PHRASE", "")
	cfg := LoadConfig()
	if !cfg.Enabled {
		t.Fatal("default Enabled should be true")
	}
	if !cfg.ArmRequired {
		t.Fatal("default ArmRequired should be true")
	}
	if !cfg.SizeCapPct.Equal(decimal.NewFromFloat(0.10)) {
		t.Fatalf("default SizeCapPct: %s", cfg.SizeCapPct)
	}
	if cfg.FirstNHold != 5 {
		t.Fatalf("default FirstNHold: %d", cfg.FirstNHold)
	}
	if len(cfg.ConfirmPhrase) < 32 {
		t.Fatalf("auto-generated phrase should be >= 32 chars, got %d", len(cfg.ConfirmPhrase))
	}
}

func TestGuard_LoadConfigOverrides(t *testing.T) {
	t.Setenv("NEURATRADE_LIVE_GUARD_ENABLED", "false")
	t.Setenv("NEURATRADE_LIVE_GUARD_ARM_REQUIRED", "false")
	t.Setenv("NEURATRADE_LIVE_GUARD_SIZE_CAP_PCT", "0.25")
	t.Setenv("NEURATRADE_LIVE_GUARD_FIRST_N_HOLD", "3")
	t.Setenv("NEURATRADE_LIVE_GUARD_CONFIRM_PHRASE", "custom-phrase-abc")
	cfg := LoadConfig()
	if cfg.Enabled {
		t.Fatal("override should disable")
	}
	if cfg.ArmRequired {
		t.Fatal("override should disable ArmRequired")
	}
	if !cfg.SizeCapPct.Equal(decimal.RequireFromString("0.25")) {
		t.Fatalf("SizeCapPct: %s", cfg.SizeCapPct)
	}
	if cfg.FirstNHold != 3 {
		t.Fatalf("FirstNHold: %d", cfg.FirstNHold)
	}
	if cfg.ConfirmPhrase != "custom-phrase-abc" {
		t.Fatalf("ConfirmPhrase: %s", cfg.ConfirmPhrase)
	}
}

func TestGuard_StatusShape(t *testing.T) {
	g := newTestGuard()
	if err := g.Arm("op", "test-phrase-1234", "go-live", false); err != nil {
		t.Fatal(err)
	}
	_, _ = g.CheckOrder("i1", "chat-1", "s", "BTC/USDT", "buy", "market", decimal.RequireFromString("500"), true)
	st := g.Status()
	if !st.Armed {
		t.Fatal("Armed should be true")
	}
	if st.ArmedBy != "op" {
		t.Fatalf("ArmedBy: %s", st.ArmedBy)
	}
	if st.ArmReason != "go-live" {
		t.Fatalf("ArmReason: %s", st.ArmReason)
	}
	if len(st.Pending) != 1 {
		t.Fatalf("Pending: want 1, got %d", len(st.Pending))
	}
	if !strings.HasPrefix(st.PhraseHint, "...") {
		t.Fatalf("PhraseHint: %s", st.PhraseHint)
	}
}

func TestGuard_ConcurrentAccess(t *testing.T) {
	g := newTestGuard()
	if err := g.Arm("op", "test-phrase-1234", "", false); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	const N = 50
	wg.Add(N)
	for i := 0; i < N; i++ {
		go func(i int) {
			defer wg.Done()
			intent := "intent-" + string(rune('a'+i%26)) + "-" + string(rune('0'+i%10))
			_, _ = g.CheckOrder(intent, "chat-1", "s", "BTC/USDT", "buy", "market", decimal.RequireFromString("1"), true)
		}(i)
	}
	wg.Wait()
	_ = g.Status() // must not panic under concurrent read
}

func TestGuard_NilSafe(t *testing.T) {
	var g *Guard
	if g.IsArmed() {
		t.Fatal("nil guard should not be armed")
	}
	_, err := g.CheckOrder("i1", "c", "s", "sym", "buy", "market", decimal.NewFromInt(1), true)
	if err == nil {
		t.Fatal("nil guard must return an error")
	}
}

func TestGuard_DisarmResetsHistoryVisible(t *testing.T) {
	g := newTestGuard()
	if err := g.Arm("op", "test-phrase-1234", "first", false); err != nil {
		t.Fatal(err)
	}
	g.Disarm("op", "pause")
	if err := g.Arm("op2", "test-phrase-1234", "second", false); err != nil {
		t.Fatal(err)
	}
	st := g.Status()
	if len(st.ArmDisarmEvents) < 2 {
		t.Fatalf("events: %+v", st.ArmDisarmEvents)
	}
	if st.ArmedBy != "op2" {
		t.Fatalf("ArmedBy: %s", st.ArmedBy)
	}
}

func TestGuard_TimeFieldsAreUTC(t *testing.T) {
	g := newTestGuard()
	if err := g.Arm("op", "test-phrase-1234", "t", false); err != nil {
		t.Fatal(err)
	}
	st := g.Status()
	if st.ArmedAt.Location() != time.UTC {
		t.Fatalf("ArmedAt not UTC: %s", st.ArmedAt.Location())
	}
}

// TestGuard_ApproveOrderAdvancesFirstNHold verifies the fix for NeuraTrade-sjm7:
// ApproveOrder must advance the FirstNHold gate, otherwise the same order
// remains pending forever (dead-loop: every order hits the FirstNHold gate and
// requires manual approval, even after the operator approved the previous N).
func TestGuard_ApproveOrderAdvancesFirstNHold(t *testing.T) {
	g := newTestGuard() // FirstNHold = 5
	if err := g.Arm("op", "test-phrase-1234", "", false); err != nil {
		t.Fatal(err)
	}

	// Approve 5 orders; the first 5 must each be pending, and after each
	// approval the FirstNHold gate must advance so the NEXT order is allowed.
	for i := 1; i <= 5; i++ {
		intentID := "intent-" + string(rune('a'+i-1))
		res, err := g.CheckOrder(intentID, "chat-1", "strat", "BTC/USDT", "buy", "market", decimal.RequireFromString("100"), true)
		if err != ErrOrderPending {
			t.Fatalf("order #%d: expected ErrOrderPending, got %v", i, err)
		}
		if res.Allowed {
			t.Fatalf("order #%d: expected not allowed", i)
		}
		if _, err := g.ApproveOrder(intentID, "operator"); err != nil {
			t.Fatalf("approve order #%d: %v", i, err)
		}
	}

	// The 6th order must be auto-allowed because the FirstNHold gate is
	// satisfied after 5 approved orders. Without the fix, this returns
	// ErrOrderPending forever (dead-loop regression of NeuraTrade-sjm7).
	res, err := g.CheckOrder("intent-6", "chat-1", "strat", "BTC/USDT", "buy", "market", decimal.RequireFromString("100"), true)
	if err != nil {
		t.Fatalf("order #6: expected no error after FirstNHold satisfied, got %v", err)
	}
	if !res.Allowed {
		t.Fatalf("order #6: expected allowed (dead-loop regression of NeuraTrade-sjm7), got %+v", res)
	}
}
