package skill

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseSkill_WithFrontmatter(t *testing.T) {
	content := `---
id: test-skill
name: Test Skill
version: 1.0.0
description: A test skill for unit testing
category: testing
author: test-author
tags:
  - test
  - example
dependencies:
  - other-skill
parameters:
  input:
    type: string
    description: Input parameter
    required: true
  threshold:
    type: number
    description: Threshold value
    required: false
    default: 0.5
examples:
  - name: Basic usage
    description: Simple example
    inputs:
      input: "hello"
      threshold: 0.8
    expected: "result"
---

# Test Skill

This is the markdown content of the skill.

## Usage

Use this skill for testing purposes.
`

	skill, err := ParseSkill(content)
	require.NoError(t, err)

	assert.Equal(t, "test-skill", skill.ID)
	assert.Equal(t, "Test Skill", skill.Name)
	assert.Equal(t, "1.0.0", skill.Version)
	assert.Equal(t, "A test skill for unit testing", skill.Description)
	assert.Equal(t, "testing", skill.Category)
	assert.Equal(t, "test-author", skill.Author)
	assert.Equal(t, []string{"test", "example"}, skill.Tags)
	assert.Equal(t, []string{"other-skill"}, skill.Dependencies)
	assert.Contains(t, skill.Content, "# Test Skill")
	assert.Contains(t, skill.Content, "This is the markdown content")
}

func TestParseSkill_WithoutFrontmatter(t *testing.T) {
	content := `# Simple Skill

This skill has no frontmatter.
`

	skill, err := ParseSkill(content)
	require.NoError(t, err)

	assert.Equal(t, "unnamed-skill", skill.ID)
	assert.Equal(t, "Unnamed Skill", skill.Name)
	assert.Equal(t, "1.0.0", skill.Version)
	assert.Contains(t, skill.Content, "# Simple Skill")
}

func TestParseSkill_InvalidFrontmatter(t *testing.T) {
	content := `---
invalid yaml: [unclosed bracket
---

Content here.
`

	_, err := ParseSkill(content)
	assert.Error(t, err)
}

func TestSkill_GetParameter(t *testing.T) {
	skill := &Skill{
		Parameters: map[string]Param{
			"input": {
				Type:        "string",
				Description: "Input value",
				Required:    true,
			},
			"optional": {
				Type:        "number",
				Description: "Optional value",
				Required:    false,
				Default:     42,
			},
		},
	}

	param, ok := skill.GetParameter("input")
	assert.True(t, ok)
	assert.Equal(t, "string", param.Type)
	assert.True(t, param.Required)

	_, ok = skill.GetParameter("missing")
	assert.False(t, ok)
}

func TestSkill_HasTag(t *testing.T) {
	skill := &Skill{
		Tags: []string{"test", "EXAMPLE", "Demo"},
	}

	assert.True(t, skill.HasTag("test"))
	assert.True(t, skill.HasTag("example"))
	assert.True(t, skill.HasTag("EXAMPLE"))
	assert.True(t, skill.HasTag("Demo"))
	assert.False(t, skill.HasTag("missing"))
}

func TestSkill_HasDependency(t *testing.T) {
	skill := &Skill{
		Dependencies: []string{"skill-a", "skill-b"},
	}

	assert.True(t, skill.HasDependency("skill-a"))
	assert.True(t, skill.HasDependency("skill-b"))
	assert.False(t, skill.HasDependency("skill-c"))
}

func TestSkill_Validate(t *testing.T) {
	tests := []struct {
		name    string
		skill   *Skill
		wantErr bool
	}{
		{
			name: "valid skill",
			skill: &Skill{
				ID:      "test",
				Name:    "Test Skill",
				Content: "Some content",
			},
			wantErr: false,
		},
		{
			name: "missing ID",
			skill: &Skill{
				Name:    "Test",
				Content: "Content",
			},
			wantErr: true,
		},
		{
			name: "missing name",
			skill: &Skill{
				ID:      "test",
				Content: "Content",
			},
			wantErr: true,
		},
		{
			name: "missing content",
			skill: &Skill{
				ID:   "test",
				Name: "Test",
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.skill.Validate()
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestLoader_LoadFile(t *testing.T) {
	tmpDir := t.TempDir()
	skillContent := `---
id: test-loader
name: Test Loader Skill
---

# Content
`
	skillPath := filepath.Join(tmpDir, "test.md")
	err := os.WriteFile(skillPath, []byte(skillContent), 0644)
	require.NoError(t, err)

	loader := NewLoader(tmpDir)
	skill, err := loader.LoadFile(skillPath)
	require.NoError(t, err)

	assert.Equal(t, "test-loader", skill.ID)
	assert.Equal(t, skillPath, skill.SourcePath)
	assert.False(t, skill.LoadedAt.IsZero())
}

func TestLoader_LoadAll(t *testing.T) {
	tmpDir := t.TempDir()

	// Create multiple skill files
	skills := map[string]string{
		"skill1.md": `---
id: skill-1
name: Skill One
---

Content one.
`,
		"skill2.md": `---
id: skill-2
name: Skill Two
---

Content two.
`,
		"not-a-skill.txt": "This should be ignored",
	}

	for name, content := range skills {
		path := filepath.Join(tmpDir, name)
		err := os.WriteFile(path, []byte(content), 0644)
		require.NoError(t, err)
	}

	loader := NewLoader(tmpDir)
	loaded, err := loader.LoadAll()
	require.NoError(t, err)

	assert.Len(t, loaded, 2)
	assert.Contains(t, loader.loadedSkills, "skill-1")
	assert.Contains(t, loader.loadedSkills, "skill-2")
}

func TestLoader_LoadAll_NonExistentDir(t *testing.T) {
	loader := NewLoader("/nonexistent/path")
	skills, err := loader.LoadAll()
	require.NoError(t, err)
	assert.Empty(t, skills)
}

func TestLoader_LoadByID(t *testing.T) {
	tmpDir := t.TempDir()
	skillContent := `---
id: findable-skill
name: Findable Skill
---

Content.
`
	skillPath := filepath.Join(tmpDir, "findable-skill.md")
	err := os.WriteFile(skillPath, []byte(skillContent), 0644)
	require.NoError(t, err)

	loader := NewLoader(tmpDir)

	// First load - from file
	skill1, err := loader.LoadByID("findable-skill")
	require.NoError(t, err)
	assert.Equal(t, "findable-skill", skill1.ID)

	// Second load - from cache
	skill2, err := loader.LoadByID("findable-skill")
	require.NoError(t, err)
	assert.Equal(t, skill1, skill2)
}

func TestLoader_LoadByID_NotFound(t *testing.T) {
	loader := NewLoader(t.TempDir())
	_, err := loader.LoadByID("nonexistent")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestRegistry(t *testing.T) {
	tmpDir := t.TempDir()

	// Create test skills
	skills := []struct {
		filename string
		content  string
	}{
		{
			filename: "analysis.md",
			content: `---
id: market-analysis
name: Market Analysis
category: analysis
tags:
  - trading
---

Analyze market trends.
`,
		},
		{
			filename: "arbitrage.md",
			content: `---
id: arbitrage-detection
name: Arbitrage Detection
category: analysis
tags:
  - arbitrage
  - trading
---

Detect arbitrage opportunities.
`,
		},
		{
			filename: "risk.md",
			content: `---
id: risk-assessment
name: Risk Assessment
category: risk
tags:
  - risk
---

Assess trading risks.
`,
		},
	}

	for _, s := range skills {
		path := filepath.Join(tmpDir, s.filename)
		err := os.WriteFile(path, []byte(s.content), 0644)
		require.NoError(t, err)
	}

	registry := NewRegistry(tmpDir)
	err := registry.LoadAll()
	require.NoError(t, err)

	// Test Get
	skill, ok := registry.Get("market-analysis")
	require.True(t, ok)
	assert.Equal(t, "Market Analysis", skill.Name)

	_, ok = registry.Get("nonexistent")
	assert.False(t, ok)

	// Test GetAll
	all := registry.GetAll()
	assert.Len(t, all, 3)

	// Test GetByCategory
	analysisSkills := registry.GetByCategory("analysis")
	assert.Len(t, analysisSkills, 2)

	riskSkills := registry.GetByCategory("risk")
	assert.Len(t, riskSkills, 1)

	// Test GetByTag
	tradingSkills := registry.GetByTag("trading")
	assert.Len(t, tradingSkills, 2)

	arbitrageSkills := registry.GetByTag("arbitrage")
	assert.Len(t, arbitrageSkills, 1)

	// Test Find
	found := registry.Find("market")
	assert.Len(t, found, 1)
	assert.Equal(t, "market-analysis", found[0].ID)

	found = registry.Find("trading")
	assert.Len(t, found, 2)
}

func TestRegistry_FindCaseInsensitive(t *testing.T) {
	tmpDir := t.TempDir()
	skillContent := `---
id: test-skill
name: TEST SKILL NAME
---

Content.
`
	err := os.WriteFile(filepath.Join(tmpDir, "test.md"), []byte(skillContent), 0644)
	require.NoError(t, err)

	registry := NewRegistry(tmpDir)
	err = registry.LoadAll()
	require.NoError(t, err)

	found := registry.Find("test")
	assert.Len(t, found, 1)

	found = registry.Find("TEST")
	assert.Len(t, found, 1)
}
