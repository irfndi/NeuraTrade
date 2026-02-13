package risk

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

func setupTestRedis(t *testing.T) (*redis.Client, func()) {
	mr, err := miniredis.Run()
	if err != nil {
		t.Skip("miniredis cannot bind in this environment; skipping Redis-backed tests")
	}

	client := redis.NewClient(&redis.Options{
		Addr: mr.Addr(),
	})

	return client, func() {
		client.Close()
		mr.Close()
	}
}

func TestConsecutiveLossConfig_Defaults(t *testing.T) {
	config := DefaultConsecutiveLossConfig()

	if config.MaxConsecutiveLosses != 3 {
		t.Errorf("expected MaxConsecutiveLosses to be 3, got %d", config.MaxConsecutiveLosses)
	}

	if config.PauseDuration != 15*time.Minute {
		t.Errorf("expected PauseDuration to be 15 minutes, got %v", config.PauseDuration)
	}
}

func TestConsecutiveLossTracker_NewTracker(t *testing.T) {
	client, cleanup := setupTestRedis(t)
	defer cleanup()

	tracker := NewConsecutiveLossTracker(client, DefaultConsecutiveLossConfig())

	if tracker == nil {
		t.Fatal("expected tracker to not be nil")
	}
}

func TestConsecutiveLossTracker_RecordLoss(t *testing.T) {
	client, cleanup := setupTestRedis(t)
	defer cleanup()

	tracker := NewConsecutiveLossTracker(client, DefaultConsecutiveLossConfig())

	err := tracker.RecordLoss(context.Background(), "user1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	losses, err := tracker.GetConsecutiveLosses(context.Background(), "user1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if losses != 1 {
		t.Errorf("expected 1 loss, got %d", losses)
	}
}

func TestConsecutiveLossTracker_RecordWin(t *testing.T) {
	client, cleanup := setupTestRedis(t)
	defer cleanup()

	tracker := NewConsecutiveLossTracker(client, DefaultConsecutiveLossConfig())

	_ = tracker.RecordLoss(context.Background(), "user1")
	_ = tracker.RecordLoss(context.Background(), "user1")

	err := tracker.RecordWin(context.Background(), "user1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	losses, err := tracker.GetConsecutiveLosses(context.Background(), "user1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if losses != 0 {
		t.Errorf("expected 0 losses after win, got %d", losses)
	}
}

func TestConsecutiveLossTracker_RecordMultipleLosses(t *testing.T) {
	client, cleanup := setupTestRedis(t)
	defer cleanup()

	tracker := NewConsecutiveLossTracker(client, DefaultConsecutiveLossConfig())

	for i := 0; i < 3; i++ {
		err := tracker.RecordLoss(context.Background(), "user1")
		if err != nil {
			t.Fatalf("unexpected error on loss %d: %v", i+1, err)
		}
	}

	losses, err := tracker.GetConsecutiveLosses(context.Background(), "user1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if losses != 3 {
		t.Errorf("expected 3 losses, got %d", losses)
	}
}

func TestConsecutiveLossTracker_PauseAfterMaxLosses(t *testing.T) {
	client, cleanup := setupTestRedis(t)
	defer cleanup()

	config := DefaultConsecutiveLossConfig()
	tracker := NewConsecutiveLossTracker(client, config)

	for i := 0; i < 3; i++ {
		_ = tracker.RecordLoss(context.Background(), "user1")
	}

	isPaused, _, err := tracker.IsPaused(context.Background(), "user1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !isPaused {
		t.Error("expected user to be paused after 3 consecutive losses")
	}
}

func TestConsecutiveLossTracker_CanTrade_BeforePause(t *testing.T) {
	client, cleanup := setupTestRedis(t)
	defer cleanup()

	tracker := NewConsecutiveLossTracker(client, DefaultConsecutiveLossConfig())

	_ = tracker.RecordLoss(context.Background(), "user1")

	canTrade, message, err := tracker.CanTrade(context.Background(), "user1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !canTrade {
		t.Error("expected user to be able to trade with 1 loss")
	}

	if message == "" {
		t.Error("expected warning message")
	}
}

func TestConsecutiveLossTracker_CanTrade_AfterPause(t *testing.T) {
	client, cleanup := setupTestRedis(t)
	defer cleanup()

	config := DefaultConsecutiveLossConfig()
	tracker := NewConsecutiveLossTracker(client, config)

	for i := 0; i < 3; i++ {
		_ = tracker.RecordLoss(context.Background(), "user1")
	}

	canTrade, message, err := tracker.CanTrade(context.Background(), "user1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if canTrade {
		t.Error("expected user to be blocked when paused")
	}

	if message == "" {
		t.Error("expected pause message")
	}
}

func TestConsecutiveLossTracker_Reset(t *testing.T) {
	client, cleanup := setupTestRedis(t)
	defer cleanup()

	tracker := NewConsecutiveLossTracker(client, DefaultConsecutiveLossConfig())

	_ = tracker.RecordLoss(context.Background(), "user1")
	_ = tracker.RecordLoss(context.Background(), "user1")

	err := tracker.Reset(context.Background(), "user1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	losses, err := tracker.GetConsecutiveLosses(context.Background(), "user1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if losses != 0 {
		t.Errorf("expected 0 losses after reset, got %d", losses)
	}
}

func TestConsecutiveLossTracker_GetStats(t *testing.T) {
	client, cleanup := setupTestRedis(t)
	defer cleanup()

	tracker := NewConsecutiveLossTracker(client, DefaultConsecutiveLossConfig())

	_ = tracker.RecordLoss(context.Background(), "user1")
	_ = tracker.RecordLoss(context.Background(), "user1")

	losses, isPaused, remaining, err := tracker.GetStats(context.Background(), "user1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if losses != 2 {
		t.Errorf("expected 2 losses, got %d", losses)
	}

	if isPaused {
		t.Error("expected not to be paused")
	}

	if remaining != 0 {
		t.Errorf("expected remaining to be 0, got %v", remaining)
	}
}

func TestConsecutiveLossTracker_SetPaused(t *testing.T) {
	client, cleanup := setupTestRedis(t)
	defer cleanup()

	tracker := NewConsecutiveLossTracker(client, DefaultConsecutiveLossConfig())

	err := tracker.SetPaused(context.Background(), "user1", true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	isPaused, _, err := tracker.IsPaused(context.Background(), "user1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !isPaused {
		t.Error("expected user to be paused")
	}

	err = tracker.SetPaused(context.Background(), "user1", false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	isPaused, _, err = tracker.IsPaused(context.Background(), "user1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if isPaused {
		t.Error("expected user to be unpaused")
	}
}
