package services

import (
	"context"
	"testing"
	"time"
)

func TestDBQuestStore_InitSchema(t *testing.T) {
	store := NewDBQuestStore(nil)

	err := store.InitSchema(context.Background())
	if err == nil {
		t.Error("expected error with nil database")
	}
}

func TestDBQuestStore_SaveQuest_NilDB(t *testing.T) {
	store := NewDBQuestStore(nil)

	quest := &Quest{
		ID:        "test-quest",
		Name:      "Test Quest",
		Type:      QuestTypeRoutine,
		Cadence:   CadenceHourly,
		Status:    QuestStatusActive,
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}

	err := store.SaveQuest(context.Background(), quest)
	if err == nil {
		t.Error("expected error with nil database")
	}
}

func TestDBQuestStore_GetQuest_NilDB(t *testing.T) {
	store := NewDBQuestStore(nil)

	_, err := store.GetQuest(context.Background(), "test-quest")
	if err == nil {
		t.Error("expected error with nil database")
	}
}

func TestDBQuestStore_ListQuests_NilDB(t *testing.T) {
	store := NewDBQuestStore(nil)

	_, err := store.ListQuests(context.Background(), "chat-123", QuestStatusActive)
	if err == nil {
		t.Error("expected error with nil database")
	}
}

func TestDBQuestStore_UpdateQuestProgress_NilDB(t *testing.T) {
	store := NewDBQuestStore(nil)

	err := store.UpdateQuestProgress(context.Background(), "test-quest", 5, map[string]interface{}{"step": 1})
	if err == nil {
		t.Error("expected error with nil database")
	}
}

func TestDBQuestStore_UpdateLastExecuted_NilDB(t *testing.T) {
	store := NewDBQuestStore(nil)

	err := store.UpdateLastExecuted(context.Background(), "test-quest", time.Now().UTC())
	if err == nil {
		t.Error("expected error with nil database")
	}
}

func TestDBQuestStore_SaveAutonomousState_NilDB(t *testing.T) {
	store := NewDBQuestStore(nil)

	state := &AutonomousState{
		ChatID:    "chat-123",
		IsActive:  true,
		StartedAt: time.Now().UTC(),
	}

	err := store.SaveAutonomousState(context.Background(), state)
	if err == nil {
		t.Error("expected error with nil database")
	}
}

func TestDBQuestStore_GetAutonomousState_NilDB(t *testing.T) {
	store := NewDBQuestStore(nil)

	state, err := store.GetAutonomousState(context.Background(), "chat-123")
	if err == nil {
		t.Error("expected error with nil database")
	}
	if state != nil {
		t.Error("expected nil state with nil database")
	}
}

func TestDBQuestStore_DeleteQuest_NilDB(t *testing.T) {
	store := NewDBQuestStore(nil)

	err := store.DeleteQuest(context.Background(), "test-quest")
	if err == nil {
		t.Error("expected error with nil database")
	}
}

func TestDBQuestStore_CountQuests_NilDB(t *testing.T) {
	store := NewDBQuestStore(nil)

	_, err := store.CountQuests(context.Background(), QuestStatusActive)
	if err == nil {
		t.Error("expected error with nil database")
	}
}
