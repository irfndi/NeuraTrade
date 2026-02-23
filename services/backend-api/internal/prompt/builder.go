// Package prompt provides functionality for building prompts from skill.md files and context.
package prompt

import (
	"fmt"
	"strings"
	"text/template"

	"github.com/irfndi/neuratrade/internal/skill"
	"golang.org/x/text/cases"
	"golang.org/x/text/language"
)

var titleCaser = cases.Title(language.English)

// Builder constructs prompts from skill definitions and context.
type Builder struct {
	skillRegistry *skill.Registry
	funcMap       template.FuncMap
}

// NewBuilder creates a new prompt builder with the given skill registry.
func NewBuilder(skillRegistry *skill.Registry) *Builder {
	return &Builder{
		skillRegistry: skillRegistry,
		funcMap: template.FuncMap{
			"skill":    skillRegistry.Get,
			"skills":   skillRegistry.GetAll,
			"category": skillRegistry.GetByCategory,
			"tag":      skillRegistry.GetByTag,
			"find":     skillRegistry.Find,
			"upper":    strings.ToUpper,
			"lower":    strings.ToLower,
			"title":    titleCaser.String,
		},
	}
}

// Context holds the context for building a prompt.
type Context struct {
	// UserQuery is the user's input query.
	UserQuery string
	// TaskType is the type of task (e.g., "analysis", "trading", "arbitrage").
	TaskType string
	// AvailableSkills is a list of skill IDs that are available.
	AvailableSkills []string
	// AdditionalContext is any additional context to include.
	AdditionalContext map[string]interface{}
	// MaxTokens is the maximum number of tokens for the prompt.
	MaxTokens int
	// DisclosureLevel controls how much detail to reveal (0-100).
	DisclosureLevel int
}

// Build builds a prompt from a skill and context.
func (b *Builder) Build(skillID string, ctx Context) (string, error) {
	skl, ok := b.skillRegistry.Get(skillID)
	if !ok {
		return "", fmt.Errorf("skill not found: %s", skillID)
	}

	return b.buildFromSkill(skl, ctx)
}

// BuildFromMultiple builds a prompt from multiple skills.
func (b *Builder) BuildFromMultiple(skillIDs []string, ctx Context) (string, error) {
	var prompts []string

	for _, id := range skillIDs {
		p, err := b.Build(id, ctx)
		if err != nil {
			return "", fmt.Errorf("failed to build prompt for skill %s: %w", id, err)
		}
		prompts = append(prompts, p)
	}

	return b.combinePrompts(prompts), nil
}

// BuildFromCategory builds a prompt from all skills in a category.
func (b *Builder) BuildFromCategory(category string, ctx Context) (string, error) {
	skills := b.skillRegistry.GetByCategory(category)
	if len(skills) == 0 {
		return "", fmt.Errorf("no skills found in category: %s", category)
	}

	var prompts []string
	for _, skl := range skills {
		p, err := b.buildFromSkill(skl, ctx)
		if err != nil {
			return "", fmt.Errorf("failed to build prompt for skill %s: %w", skl.ID, err)
		}
		prompts = append(prompts, p)
	}

	return b.combinePrompts(prompts), nil
}

// buildFromSkill builds a prompt from a single skill with context.
func (b *Builder) buildFromSkill(skl *skill.Skill, ctx Context) (string, error) {
	var sb strings.Builder

	// Add skill metadata header
	fmt.Fprintf(&sb, "# Skill: %s\n", skl.Name)
	fmt.Fprintf(&sb, "## ID: %s\n", skl.ID)
	fmt.Fprintf(&sb, "## Version: %s\n", skl.Version)
	if skl.Description != "" {
		fmt.Fprintf(&sb, "## Description: %s\n", skl.Description)
	}
	sb.WriteString("\n")

	// Add content based on disclosure level
	content := b.applyDisclosureLevel(skl.Content, ctx.DisclosureLevel)
	sb.WriteString(content)
	sb.WriteString("\n")

	// Add parameters if available and disclosure level permits
	if len(skl.Parameters) > 0 && ctx.DisclosureLevel >= 50 {
		sb.WriteString("\n## Available Parameters:\n")
		for name, param := range skl.Parameters {
			required := ""
			if param.Required {
				required = " (required)"
			}
			fmt.Fprintf(&sb, "- `%s`%s: %s\n", name, required, param.Description)
			if param.Default != nil {
				fmt.Fprintf(&sb, "  - Default: %v\n", param.Default)
			}
			if len(param.Enum) > 0 {
				fmt.Fprintf(&sb, "  - Options: %s\n", strings.Join(param.Enum, ", "))
			}
		}
	}

	// Add examples if available and disclosure level permits
	if len(skl.Examples) > 0 && ctx.DisclosureLevel >= 75 {
		sb.WriteString("\n## Examples:\n")
		for _, ex := range skl.Examples {
			fmt.Fprintf(&sb, "### %s\n", ex.Name)
			fmt.Fprintf(&sb, "%s\n", ex.Description)
			if len(ex.Inputs) > 0 {
				sb.WriteString("Inputs:\n")
				for k, v := range ex.Inputs {
					fmt.Fprintf(&sb, "  - %s: %v\n", k, v)
				}
			}
			if ex.Expected != nil {
				fmt.Fprintf(&sb, "Expected: %v\n", ex.Expected)
			}
		}
	}

	// Add user query if provided
	if ctx.UserQuery != "" {
		fmt.Fprintf(&sb, "\n## User Request:\n%s\n", ctx.UserQuery)
	}

	// Add additional context if provided
	if len(ctx.AdditionalContext) > 0 && ctx.DisclosureLevel >= 50 {
		sb.WriteString("\n## Additional Context:\n")
		for k, v := range ctx.AdditionalContext {
			fmt.Fprintf(&sb, "- %s: %v\n", k, v)
		}
	}

	// Add task type context
	if ctx.TaskType != "" {
		fmt.Fprintf(&sb, "\n## Task Type: %s\n", ctx.TaskType)
	}

	return sb.String(), nil
}

// applyDisclosureLevel applies progressive disclosure to the content.
func (b *Builder) applyDisclosureLevel(content string, level int) string {
	if level >= 100 {
		return content
	}

	// Split content into sections
	lines := strings.Split(content, "\n")
	var result []string
	inCodeBlock := false

	for _, line := range lines {
		// Track code blocks
		if strings.HasPrefix(strings.TrimSpace(line), "```") {
			inCodeBlock = !inCodeBlock
			result = append(result, line)
			continue
		}

		// At full disclosure, show everything
		if level >= 75 {
			result = append(result, line)
			continue
		}

		// At high disclosure, show everything except advanced sections
		if level >= 50 {
			trimmed := strings.TrimSpace(line)
			// Skip advanced sections at lower disclosure levels
			if strings.HasPrefix(trimmed, "## ") &&
				(strings.Contains(trimmed, "Advanced") ||
					strings.Contains(trimmed, "Expert") ||
					strings.Contains(trimmed, "Optimization")) {
				continue
			}
			result = append(result, line)
			continue
		}

		// At medium disclosure, show overview and basic usage
		if level >= 25 {
			trimmed := strings.TrimSpace(line)
			// Skip detailed sections at lower disclosure levels
			if strings.HasPrefix(trimmed, "## ") &&
				(strings.Contains(trimmed, "Parameters") ||
					strings.Contains(trimmed, "Examples") ||
					strings.Contains(trimmed, "Best Practices") ||
					strings.Contains(trimmed, "Advanced")) {
				continue
			}
			result = append(result, line)
			continue
		}

		// At low disclosure, only show title and description
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "# ") || strings.HasPrefix(trimmed, "## ") {
			result = append(result, line)
		} else if !strings.HasPrefix(trimmed, "- ") && !strings.HasPrefix(trimmed, "1. ") {
			// Skip bullets at lowest disclosure
			result = append(result, line)
		}
	}

	return strings.Join(result, "\n")
}

// combinePrompts combines multiple prompts into a single prompt.
func (b *Builder) combinePrompts(prompts []string) string {
	var sb strings.Builder
	sb.WriteString("# Combined Prompt\n\n")

	for i, p := range prompts {
		if i > 0 {
			sb.WriteString("\n---\n\n")
		}
		sb.WriteString(p)
	}

	return sb.String()
}

// BuildSystemPrompt builds a system prompt with skill context.
func (b *Builder) BuildSystemPrompt(ctx Context) (string, error) {
	var sb strings.Builder

	sb.WriteString("# NeuraTrade AI Assistant\n\n")
	sb.WriteString("You are an AI assistant for NeuraTrade, a cryptocurrency arbitrage and trading platform.\n\n")

	// Add available skills
	if len(ctx.AvailableSkills) > 0 {
		sb.WriteString("## Available Skills:\n")
		for _, id := range ctx.AvailableSkills {
			skl, ok := b.skillRegistry.Get(id)
			if ok {
				fmt.Fprintf(&sb, "- **%s**: %s\n", skl.Name, skl.Description)
			}
		}
		sb.WriteString("\n")
	}

	// Add disclosure level guidance
	if ctx.DisclosureLevel > 0 {
		fmt.Fprintf(&sb, "## Disclosure Level: %d%%\n", ctx.DisclosureLevel)
		sb.WriteString("Provide responses appropriate to this disclosure level.\n\n")
	}

	// Add task type context
	if ctx.TaskType != "" {
		fmt.Fprintf(&sb, "## Current Task: %s\n", ctx.TaskType)
	}

	return sb.String(), nil
}
