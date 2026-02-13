package prompt

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/irfndi/neuratrade/internal/skill"
)

func TestBuilder_Build(t *testing.T) {
	tempDir := t.TempDir()
	skillContent := `---
id: test_skill
name: test_skill
description: A test skill for unit testing
version: 1.0.0
category: testing
tags: [test, unit]
parameters:
  value:
    type: string
    description: The input value
    required: true
examples:
  - name: basic_example
    description: A basic example
---
# Test Skill

This is the content of the test skill.

## Parameters

Use the parameters to customize the behavior.`

	if err := os.WriteFile(filepath.Join(tempDir, "test_skill.md"), []byte(skillContent), 0644); err != nil {
		t.Fatalf("failed to write skill file: %v", err)
	}

	registry := skill.NewRegistry(tempDir)
	if err := registry.LoadAll(); err != nil {
		t.Fatalf("failed to load skills: %v", err)
	}

	builder := NewBuilder(registry)

	ctx := Context{
		UserQuery:       "Test query",
		TaskType:        "testing",
		DisclosureLevel: 100,
		AvailableSkills: []string{"test_skill"},
	}

	prompt, err := builder.Build("test_skill", ctx)
	if err != nil {
		t.Fatalf("failed to build prompt: %v", err)
	}

	if prompt == "" {
		t.Fatal("prompt should not be empty")
	}
}

func TestBuilder_DisclosureLevel(t *testing.T) {
	tempDir := t.TempDir()
	skillContent := `---
id: disclosure_test
name: disclosure_test
description: Testing disclosure levels
version: 1.0.0
category: testing
---
# Disclosure Test Skill

## Overview

This is the overview section.

## Parameters

- param1: Required parameter

## Advanced

This is advanced content.

## Best Practices

Always follow best practices.`

	if err := os.WriteFile(filepath.Join(tempDir, "disclosure_test.md"), []byte(skillContent), 0644); err != nil {
		t.Fatalf("failed to write skill file: %v", err)
	}

	registry := skill.NewRegistry(tempDir)
	if err := registry.LoadAll(); err != nil {
		t.Fatalf("failed to load skills: %v", err)
	}

	builder := NewBuilder(registry)

	_, err := builder.Build("disclosure_test", Context{DisclosureLevel: 0})
	if err != nil {
		t.Fatalf("failed to build prompt: %v", err)
	}

	_, err = builder.Build("disclosure_test", Context{DisclosureLevel: 50})
	if err != nil {
		t.Fatalf("failed to build prompt: %v", err)
	}

	_, err = builder.Build("disclosure_test", Context{DisclosureLevel: 100})
	if err != nil {
		t.Fatalf("failed to build prompt: %v", err)
	}
}

func TestBuilder_BuildFromCategory(t *testing.T) {
	tempDir := t.TempDir()

	skills := []struct {
		filename string
		content  string
	}{
		{"skill1.md", "---\nid: skill1\nname: skill1\ncategory: trading\n---\n# Skill 1\n"},
		{"skill2.md", "---\nid: skill2\nname: skill2\ncategory: trading\n---\n# Skill 2\n"},
		{"skill3.md", "---\nid: skill3\nname: skill3\ncategory: analysis\n---\n# Skill 3\n"},
	}

	for _, s := range skills {
		if err := os.WriteFile(filepath.Join(tempDir, s.filename), []byte(s.content), 0644); err != nil {
			t.Fatalf("failed to write skill file: %v", err)
		}
	}

	registry := skill.NewRegistry(tempDir)
	if err := registry.LoadAll(); err != nil {
		t.Fatalf("failed to load skills: %v", err)
	}

	builder := NewBuilder(registry)

	prompt, err := builder.BuildFromCategory("trading", Context{})
	if err != nil {
		t.Fatalf("failed to build prompt from category: %v", err)
	}

	if prompt == "" {
		t.Fatal("prompt should not be empty")
	}
}

func TestBuilder_BuildSystemPrompt(t *testing.T) {
	tempDir := t.TempDir()
	skillContent := `---
id: sys_skill
name: sys_skill
description: A system skill
version: 1.0.0
category: system
---
# System Skill`

	if err := os.WriteFile(filepath.Join(tempDir, "sys_skill.md"), []byte(skillContent), 0644); err != nil {
		t.Fatalf("failed to write skill file: %v", err)
	}

	registry := skill.NewRegistry(tempDir)
	if err := registry.LoadAll(); err != nil {
		t.Fatalf("failed to load skills: %v", err)
	}

	builder := NewBuilder(registry)

	ctx := Context{
		TaskType:        "general",
		DisclosureLevel: 50,
		AvailableSkills: []string{"sys_skill"},
	}

	prompt, err := builder.BuildSystemPrompt(ctx)
	if err != nil {
		t.Fatalf("failed to build system prompt: %v", err)
	}

	if prompt == "" {
		t.Fatal("prompt should not be empty")
	}
}
