package prompt

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

type DisclosureLevel int

const (
	LevelMinimal  DisclosureLevel = 25
	LevelBasic    DisclosureLevel = 50
	LevelDetailed DisclosureLevel = 75
	LevelFull     DisclosureLevel = 100
)

type SkillVersion struct {
	SkillID      string          `json:"skill_id"`
	Version      string          `json:"version"`
	Hash         string          `json:"hash"`
	ReleasedAt   time.Time       `json:"released_at"`
	Disclosure   DisclosureLevel `json:"disclosure_level"`
	Deprecates   string          `json:"deprecates,omitempty"`
	Dependencies []string        `json:"dependencies,omitempty"`
}

type DisclosureManager struct {
	versions        map[string]SkillVersion
	mu              sync.RWMutex
	levelThresholds map[DisclosureLevel]int
}

func NewDisclosureManager() *DisclosureManager {
	return &DisclosureManager{
		versions: make(map[string]SkillVersion),
		levelThresholds: map[DisclosureLevel]int{
			LevelMinimal:  25,
			LevelBasic:    50,
			LevelDetailed: 75,
			LevelFull:     100,
		},
	}
}

func ComputeHash(content string) string {
	hash := sha256.Sum256([]byte(content))
	return hex.EncodeToString(hash[:])
}

func (dm *DisclosureManager) RegisterSkill(skillID, version, content string, disclosure DisclosureLevel, deps []string) {
	dm.mu.Lock()
	defer dm.mu.Unlock()

	dm.versions[skillID] = SkillVersion{
		SkillID:      skillID,
		Version:      version,
		Hash:         ComputeHash(content),
		ReleasedAt:   time.Now(),
		Disclosure:   disclosure,
		Dependencies: deps,
	}
}

func (dm *DisclosureManager) GetVersion(skillID string) (SkillVersion, bool) {
	dm.mu.RLock()
	defer dm.mu.RUnlock()

	v, ok := dm.versions[skillID]
	return v, ok
}

func (dm *DisclosureManager) GetAllVersions() []SkillVersion {
	dm.mu.RLock()
	defer dm.mu.RUnlock()

	versions := make([]SkillVersion, 0, len(dm.versions))
	for _, v := range dm.versions {
		versions = append(versions, v)
	}

	sort.Slice(versions, func(i, j int) bool {
		if versions[i].ReleasedAt.Equal(versions[j].ReleasedAt) {
			return versions[i].SkillID < versions[j].SkillID
		}
		return versions[i].ReleasedAt.After(versions[j].ReleasedAt)
	})

	return versions
}

func (dm *DisclosureManager) CheckCompatibility(skillID, contentHash string) (bool, string) {
	dm.mu.RLock()
	defer dm.mu.RUnlock()

	v, ok := dm.versions[skillID]
	if !ok {
		return false, "skill not registered"
	}

	if v.Hash != contentHash {
		return false, fmt.Sprintf("hash mismatch: expected %s, got %s", v.Hash, contentHash)
	}

	return true, "compatible"
}

func (dm *DisclosureManager) GetDisclosureLevel(userExperience int) DisclosureLevel {
	switch {
	case userExperience < 30:
		return LevelMinimal
	case userExperience < 60:
		return LevelBasic
	case userExperience < 85:
		return LevelDetailed
	default:
		return LevelFull
	}
}

func (dm *DisclosureManager) FilterByDisclosureLevel(level DisclosureLevel) []SkillVersion {
	dm.mu.RLock()
	defer dm.mu.RUnlock()

	var result []SkillVersion
	for _, v := range dm.versions {
		if v.Disclosure <= level {
			result = append(result, v)
		}
	}

	return result
}

type DisclosureRequest struct {
	SkillIDs       []string        `json:"skill_ids"`
	UserExperience int             `json:"user_experience"`
	Context        string          `json:"context"`
	RequestedLevel DisclosureLevel `json:"requested_level,omitempty"`
}

type DisclosureResponse struct {
	Skills       []SkillVersion  `json:"skills"`
	AppliedLevel DisclosureLevel `json:"applied_level"`
	Warnings     []string        `json:"warnings,omitempty"`
}

func (dm *DisclosureManager) ProcessRequest(req DisclosureRequest) DisclosureResponse {
	appliedLevel := req.RequestedLevel
	if appliedLevel == 0 {
		appliedLevel = dm.GetDisclosureLevel(req.UserExperience)
	}

	skills := dm.FilterByDisclosureLevel(appliedLevel)

	var warnings []string
	for _, skillID := range req.SkillIDs {
		found := false
		for _, s := range skills {
			if s.SkillID == skillID {
				found = true
				break
			}
		}
		if !found {
			warnings = append(warnings, fmt.Sprintf("skill %s requires higher disclosure level", skillID))
		}
	}

	return DisclosureResponse{
		Skills:       skills,
		AppliedLevel: appliedLevel,
		Warnings:     warnings,
	}
}

type ProgressiveDisclosure struct {
	manager      *DisclosureManager
	currentLevel DisclosureLevel
	history      []DisclosureEvent
	mu           sync.Mutex
}

type DisclosureEvent struct {
	Timestamp time.Time       `json:"timestamp"`
	SkillID   string          `json:"skill_id"`
	FromLevel DisclosureLevel `json:"from_level"`
	ToLevel   DisclosureLevel `json:"to_level"`
	Reason    string          `json:"reason"`
}

func NewProgressiveDisclosure() *ProgressiveDisclosure {
	return &ProgressiveDisclosure{
		manager:      NewDisclosureManager(),
		currentLevel: LevelMinimal,
		history:      make([]DisclosureEvent, 0),
	}
}

func (pd *ProgressiveDisclosure) Initialize(skills []string, contentGetter func(string) string) {
	pd.mu.Lock()
	defer pd.mu.Unlock()

	for _, skillID := range skills {
		content := contentGetter(skillID)

		level := LevelFull
		if strings.Contains(content, "## Advanced") {
			level = LevelDetailed
		}
		if strings.Contains(content, "## Parameters") {
			level = LevelBasic
		}

		pd.manager.RegisterSkill(skillID, "1.0.0", content, level, nil)
	}
}

func (pd *ProgressiveDisclosure) RequestDisclosure(skillID string, reason string) DisclosureLevel {
	pd.mu.Lock()
	defer pd.mu.Unlock()

	version, ok := pd.manager.GetVersion(skillID)
	if !ok {
		return pd.currentLevel
	}

	newLevel := version.Disclosure
	if newLevel > pd.currentLevel {
		event := DisclosureEvent{
			Timestamp: time.Now(),
			SkillID:   skillID,
			FromLevel: pd.currentLevel,
			ToLevel:   newLevel,
			Reason:    reason,
		}
		pd.history = append(pd.history, event)
		pd.currentLevel = newLevel
	}

	return pd.currentLevel
}

func (pd *ProgressiveDisclosure) GetCurrentLevel() DisclosureLevel {
	pd.mu.Lock()
	defer pd.mu.Unlock()

	return pd.currentLevel
}

func (pd *ProgressiveDisclosure) GetHistory() []DisclosureEvent {
	pd.mu.Lock()
	defer pd.mu.Unlock()

	history := make([]DisclosureEvent, len(pd.history))
	copy(history, pd.history)

	return history
}

func (pd *ProgressiveDisclosure) Reset() {
	pd.mu.Lock()
	defer pd.mu.Unlock()

	pd.currentLevel = LevelMinimal
	pd.history = make([]DisclosureEvent, 0)
}
