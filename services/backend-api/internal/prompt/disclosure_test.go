package prompt

import (
	"testing"
	"time"
)

func TestDisclosureManager_RegisterAndGet(t *testing.T) {
	dm := NewDisclosureManager()

	dm.RegisterSkill("skill1", "1.0.0", "content here", LevelFull, nil)

	v, ok := dm.GetVersion("skill1")
	if !ok {
		t.Fatal("should find registered skill")
	}

	if v.Version != "1.0.0" {
		t.Errorf("expected version 1.0.0, got %s", v.Version)
	}

	if v.SkillID != "skill1" {
		t.Errorf("expected skillID skill1, got %s", v.SkillID)
	}
}

func TestDisclosureManager_CheckCompatibility(t *testing.T) {
	dm := NewDisclosureManager()

	content := "test content"
	hash := ComputeHash(content)
	dm.RegisterSkill("skill1", "1.0.0", content, LevelFull, nil)

	ok, msg := dm.CheckCompatibility("skill1", hash)
	if !ok {
		t.Errorf("should be compatible: %s", msg)
	}

	ok, msg = dm.CheckCompatibility("skill1", "wrong_hash")
	if ok {
		t.Error("should not be compatible with wrong hash")
	}

	ok, msg = dm.CheckCompatibility("nonexistent", hash)
	if ok {
		t.Error("should not be compatible with nonexistent skill")
	}
}

func TestDisclosureManager_GetDisclosureLevel(t *testing.T) {
	dm := NewDisclosureManager()

	tests := []struct {
		experience int
		expected   DisclosureLevel
	}{
		{0, LevelMinimal},
		{20, LevelMinimal},
		{30, LevelBasic},
		{50, LevelBasic},
		{60, LevelDetailed},
		{80, LevelDetailed},
		{85, LevelFull},
		{100, LevelFull},
	}

	for _, tt := range tests {
		level := dm.GetDisclosureLevel(tt.experience)
		if level != tt.expected {
			t.Errorf("experience %d: expected %d, got %d", tt.experience, tt.expected, level)
		}
	}
}

func TestDisclosureManager_FilterByDisclosureLevel(t *testing.T) {
	dm := NewDisclosureManager()

	dm.RegisterSkill("minimal", "1.0.0", "content", LevelMinimal, nil)
	dm.RegisterSkill("basic", "1.0.0", "content", LevelBasic, nil)
	dm.RegisterSkill("detailed", "1.0.0", "content", LevelDetailed, nil)
	dm.RegisterSkill("full", "1.0.0", "content", LevelFull, nil)

	skills := dm.FilterByDisclosureLevel(LevelBasic)
	if len(skills) != 2 {
		t.Errorf("expected 2 skills at level Basic, got %d", len(skills))
	}

	skills = dm.FilterByDisclosureLevel(LevelFull)
	if len(skills) != 4 {
		t.Errorf("expected 4 skills at level Full, got %d", len(skills))
	}
}

func TestDisclosureManager_ProcessRequest(t *testing.T) {
	dm := NewDisclosureManager()

	dm.RegisterSkill("skill1", "1.0.0", "content", LevelBasic, nil)
	dm.RegisterSkill("skill2", "1.0.0", "content", LevelDetailed, nil)

	req := DisclosureRequest{
		SkillIDs:       []string{"skill1", "skill2"},
		UserExperience: 50,
		RequestedLevel: 0,
	}

	resp := dm.ProcessRequest(req)
	if resp.AppliedLevel != LevelBasic {
		t.Errorf("expected level Basic, got %d", resp.AppliedLevel)
	}

	if len(resp.Warnings) != 1 {
		t.Errorf("expected 1 warning, got %d", len(resp.Warnings))
	}
}

func TestProgressiveDisclosure_Basic(t *testing.T) {
	pd := NewProgressiveDisclosure()

	skills := []string{"skill1", "skill2"}
	contentGetter := func(id string) string {
		if id == "skill1" {
			return "# Skill 1\n## Basic content"
		}
		return "# Skill 2\n## Advanced content"
	}

	pd.Initialize(skills, contentGetter)

	level := pd.GetCurrentLevel()
	if level != LevelMinimal {
		t.Errorf("expected initial level Minimal, got %d", level)
	}

	requestedLevel := pd.RequestDisclosure("skill2", "user requested")
	if requestedLevel != LevelDetailed {
		t.Errorf("expected level Detailed after request, got %d", requestedLevel)
	}
}

func TestProgressiveDisclosure_History(t *testing.T) {
	pd := NewProgressiveDisclosure()

	skills := []string{"skill1"}
	contentGetter := func(id string) string {
		return "# Skill 1\n"
	}

	pd.Initialize(skills, contentGetter)

	pd.RequestDisclosure("skill1", "first request")

	history := pd.GetHistory()
	if len(history) != 1 {
		t.Errorf("expected 1 event in history, got %d", len(history))
	}

	if history[0].Reason != "first request" {
		t.Errorf("expected reason 'first request', got %s", history[0].Reason)
	}
}

func TestProgressiveDisclosure_Reset(t *testing.T) {
	pd := NewProgressiveDisclosure()

	skills := []string{"skill1"}
	contentGetter := func(id string) string {
		return "# Skill 1\n"
	}

	pd.Initialize(skills, contentGetter)
	pd.RequestDisclosure("skill1", "test")

	pd.Reset()

	if pd.GetCurrentLevel() != LevelMinimal {
		t.Errorf("expected level Minimal after reset, got %d", pd.GetCurrentLevel())
	}

	if len(pd.GetHistory()) != 0 {
		t.Error("expected empty history after reset")
	}
}

func TestComputeHash(t *testing.T) {
	hash1 := ComputeHash("test content")
	hash2 := ComputeHash("test content")
	hash3 := ComputeHash("different content")

	if hash1 != hash2 {
		t.Error("same content should produce same hash")
	}

	if hash1 == hash3 {
		t.Error("different content should produce different hash")
	}

	if len(hash1) != 64 {
		t.Errorf("expected SHA256 hash length 64, got %d", len(hash1))
	}
}

func TestSkillVersion(t *testing.T) {
	v := SkillVersion{
		SkillID:    "test",
		Version:    "1.0.0",
		Hash:       ComputeHash("content"),
		ReleasedAt: time.Now(),
		Disclosure: LevelFull,
	}

	if v.SkillID != "test" {
		t.Errorf("expected SkillID test, got %s", v.SkillID)
	}

	if v.Disclosure != LevelFull {
		t.Errorf("expected LevelFull, got %d", v.Disclosure)
	}
}
