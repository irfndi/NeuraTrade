package services

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"
)

// AIStateSnapshot represents a serialized state of an AI session
type AIStateSnapshot struct {
	ID          string                 `json:"id"`
	SessionID   string                 `json:"session_id"`
	AgentType   string                 `json:"agent_type"`
	Timestamp   time.Time              `json:"timestamp"`
	State       map[string]interface{} `json:"state"`
	Context     map[string]interface{} `json:"context"`
	Memory      []ConversationTurn     `json:"memory,omitempty"`
	Checkpoints []Checkpoint           `json:"checkpoints,omitempty"`
	Metadata    map[string]string      `json:"metadata,omitempty"`
}

// ConversationTurn represents a single turn in the conversation
type ConversationTurn struct {
	Role      string    `json:"role"`
	Content   string    `json:"content"`
	Timestamp time.Time `json:"timestamp"`
}

// Checkpoint represents a recovery point in the session
type Checkpoint struct {
	ID        string    `json:"id"`
	Label     string    `json:"label"`
	Timestamp time.Time `json:"timestamp"`
	StateHash string    `json:"state_hash"`
}

// AISessionSerializer manages serialization of AI session states
type AISessionSerializer struct {
	store AISessionStore
	mu    sync.RWMutex
}

// AISessionStore defines the interface for session storage
type AISessionStore interface {
	Save(ctx context.Context, snapshot *AIStateSnapshot) error
	Load(ctx context.Context, sessionID string) (*AIStateSnapshot, error)
	List(ctx context.Context, agentType string, limit int) ([]*AIStateSnapshot, error)
	Delete(ctx context.Context, sessionID string) error
}

// NewAISessionSerializer creates a new session serializer
func NewAISessionSerializer(store AISessionStore) *AISessionSerializer {
	return &AISessionSerializer{
		store: store,
	}
}

// Serialize creates a snapshot of the current AI state
func (s *AISessionSerializer) Serialize(sessionID, agentType string, state, context map[string]interface{}, memory []ConversationTurn) (*AIStateSnapshot, error) {
	snapshot := &AIStateSnapshot{
		ID:        fmt.Sprintf("%s_%d", sessionID, time.Now().UnixNano()),
		SessionID: sessionID,
		AgentType: agentType,
		Timestamp: time.Now(),
		State:     state,
		Context:   context,
		Memory:    memory,
		Metadata: map[string]string{
			"version": "1.0",
		},
	}

	return snapshot, nil
}

// Save persists a snapshot to storage
func (s *AISessionSerializer) Save(ctx context.Context, snapshot *AIStateSnapshot) error {
	return s.store.Save(ctx, snapshot)
}

// Restore retrieves and restores a session from storage
func (s *AISessionSerializer) Restore(ctx context.Context, sessionID string) (*AIStateSnapshot, error) {
	return s.store.Load(ctx, sessionID)
}

// CreateCheckpoint creates a named checkpoint for the session
func (s *AISessionSerializer) CreateCheckpoint(sessionID, label string, stateHash string) Checkpoint {
	return Checkpoint{
		ID:        fmt.Sprintf("chk_%d", time.Now().UnixNano()),
		Label:     label,
		Timestamp: time.Now(),
		StateHash: stateHash,
	}
}

// InMemoryAISessionStore implements AISessionStore in memory
type InMemoryAISessionStore struct {
	data map[string]*AIStateSnapshot
	mu   sync.RWMutex
}

// NewInMemoryAISessionStore creates an in-memory session store
func NewInMemoryAISessionStore() *InMemoryAISessionStore {
	return &InMemoryAISessionStore{
		data: make(map[string]*AIStateSnapshot),
	}
}

func (s *InMemoryAISessionStore) Save(ctx context.Context, snapshot *AIStateSnapshot) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data[snapshot.SessionID] = snapshot
	return nil
}

func (s *InMemoryAISessionStore) Load(ctx context.Context, sessionID string) (*AIStateSnapshot, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	snapshot, exists := s.data[sessionID]
	if !exists {
		return nil, fmt.Errorf("session not found: %s", sessionID)
	}
	return snapshot, nil
}

func (s *InMemoryAISessionStore) List(ctx context.Context, agentType string, limit int) ([]*AIStateSnapshot, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	results := make([]*AIStateSnapshot, 0)
	for _, snapshot := range s.data {
		if agentType == "" || snapshot.AgentType == agentType {
			results = append(results, snapshot)
			if len(results) >= limit && limit > 0 {
				break
			}
		}
	}
	return results, nil
}

func (s *InMemoryAISessionStore) Delete(ctx context.Context, sessionID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.data, sessionID)
	return nil
}

// MarshalJSON serializes snapshot to JSON
func (s *AIStateSnapshot) MarshalJSON() ([]byte, error) {
	type Alias AIStateSnapshot
	return json.Marshal(&struct {
		*Alias
		Timestamp string `json:"timestamp"`
	}{
		Alias:     (*Alias)(s),
		Timestamp: s.Timestamp.Format(time.RFC3339),
	})
}
