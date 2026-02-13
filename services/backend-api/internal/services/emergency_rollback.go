package services

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
)

// EmergencyRollbackService manages skill version snapshots for emergency rollbacks.
type EmergencyRollbackService struct {
	redisClient *redis.Client
	snapshots   map[string][]SkillSnapshot
	mu          sync.RWMutex
}

// SkillSnapshot represents a stored version of a skill.
type SkillSnapshot struct {
	SkillID   string          `json:"skill_id"`
	Version   string          `json:"version"`
	Content   string          `json:"content"`
	CreatedAt time.Time       `json:"created_at"`
	Reason    string          `json:"reason"`
	Metadata  json.RawMessage `json:"metadata,omitempty"`
}

// NewEmergencyRollbackService creates a new emergency rollback service.
func NewEmergencyRollbackService(redisClient *redis.Client) *EmergencyRollbackService {
	return &EmergencyRollbackService{
		redisClient: redisClient,
		snapshots:   make(map[string][]SkillSnapshot),
	}
}

// CreateSnapshot creates a snapshot of the current skill version.
func (s *EmergencyRollbackService) CreateSnapshot(ctx context.Context, skillID, version, content, reason string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	snapshot := SkillSnapshot{
		SkillID:   skillID,
		Version:   version,
		Content:   content,
		CreatedAt: time.Now(),
		Reason:    reason,
	}

	s.snapshots[skillID] = append(s.snapshots[skillID], snapshot)

	if s.redisClient != nil {
		key := fmt.Sprintf("skill_snapshot:%s:%s", skillID, version)
		data, err := json.Marshal(snapshot)
		if err != nil {
			return fmt.Errorf("failed to marshal snapshot: %w", err)
		}
		s.redisClient.Set(ctx, key, data, 30*24*time.Hour)
	}

	return nil
}

// GetSnapshots returns all snapshots for a skill.
func (s *EmergencyRollbackService) GetSnapshots(skillID string) []SkillSnapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()

	snaps, ok := s.snapshots[skillID]
	if !ok {
		return nil
	}

	result := make([]SkillSnapshot, len(snaps))
	copy(result, snaps)
	return result
}

// GetLatestSnapshot returns the most recent snapshot for a skill.
func (s *EmergencyRollbackService) GetLatestSnapshot(skillID string) *SkillSnapshot {
	snaps := s.GetSnapshots(skillID)
	if len(snaps) == 0 {
		return nil
	}
	return &snaps[len(snaps)-1]
}

// Rollback rolls back a skill to a specific version.
func (s *EmergencyRollbackService) Rollback(ctx context.Context, skillID, targetVersion string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	snaps, ok := s.snapshots[skillID]
	if !ok {
		return "", fmt.Errorf("no snapshots found for skill %s", skillID)
	}

	for i := len(snaps) - 1; i >= 0; i-- {
		if snaps[i].Version == targetVersion {
			return snaps[i].Content, nil
		}
	}

	return "", fmt.Errorf("version %s not found for skill %s", targetVersion, skillID)
}

// RollbackToLastKnownGood rolls back to the last snapshot before a specific timestamp.
func (s *EmergencyRollbackService) RollbackToLastKnownGood(ctx context.Context, skillID string, before time.Time) (string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	snaps, ok := s.snapshots[skillID]
	if !ok {
		return "", fmt.Errorf("no snapshots found for skill %s", skillID)
	}

	for i := len(snaps) - 1; i >= 0; i-- {
		if snaps[i].CreatedAt.Before(before) {
			return snaps[i].Content, nil
		}
	}

	return "", fmt.Errorf("no snapshots found before %v for skill %s", before, skillID)
}

// DeleteOldSnapshots removes snapshots older than the retention period.
func (s *EmergencyRollbackService) DeleteOldSnapshots(ctx context.Context, skillID string, retentionPeriod time.Duration) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	cutoff := time.Now().Add(-retentionPeriod)
	snaps := s.snapshots[skillID]

	kept := make([]SkillSnapshot, 0)
	for _, snap := range snaps {
		if snap.CreatedAt.After(cutoff) {
			kept = append(kept, snap)
		}
	}

	s.snapshots[skillID] = kept
	return nil
}

// GetVersionHistory returns the version history for a skill.
func (s *EmergencyRollbackService) GetVersionHistory(skillID string) []SkillSnapshot {
	return s.GetSnapshots(skillID)
}
