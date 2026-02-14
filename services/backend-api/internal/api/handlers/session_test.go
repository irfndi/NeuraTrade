package handlers

import (
	"context"
	"testing"
	"time"

	"github.com/irfndi/neuratrade/internal/services"
)

type mockSessionRepository struct {
	sessions map[string]*services.SessionState
}

func (m *mockSessionRepository) Save(ctx context.Context, state *services.SessionState) error {
	m.sessions[state.ID] = state
	return nil
}

func (m *mockSessionRepository) Load(ctx context.Context, id string) (*services.SessionState, error) {
	if s, ok := m.sessions[id]; ok {
		return s, nil
	}
	return nil, nil
}

func (m *mockSessionRepository) LoadByQuest(ctx context.Context, questID string) (*services.SessionState, error) {
	for _, s := range m.sessions {
		if s.QuestID == questID {
			return s, nil
		}
	}
	return nil, nil
}

func (m *mockSessionRepository) ListActive(ctx context.Context, limit int) ([]*services.SessionState, error) {
	var result []*services.SessionState
	count := 0
	for _, s := range m.sessions {
		if s.Status == services.SessionStatusActive && count < limit {
			result = append(result, s)
			count++
		}
	}
	return result, nil
}

func (m *mockSessionRepository) Delete(ctx context.Context, id string) error {
	delete(m.sessions, id)
	return nil
}

func (m *mockSessionRepository) UpdateStatus(ctx context.Context, id string, status services.SessionStatus) error {
	if s, ok := m.sessions[id]; ok {
		s.Status = status
		s.UpdatedAt = time.Now()
	}
	return nil
}

func TestSessionHandler_ResumeSession(t *testing.T) {
	repo := &mockSessionRepository{
		sessions: make(map[string]*services.SessionState),
	}
	serializer := services.NewSessionSerializer(repo)
	_ = NewSessionHandler(serializer, repo)

	sessionID := "test-session-123"
	repo.sessions[sessionID] = &services.SessionState{
		ID:        sessionID,
		QuestID:   "quest-456",
		Symbol:    "BTC/USD",
		Status:    services.SessionStatusPaused,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	if repo.sessions[sessionID].Status != services.SessionStatusPaused {
		t.Error("expected session status to be paused")
	}
}
