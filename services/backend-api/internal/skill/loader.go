// Package skill provides functionality for loading and managing skill.md files.
// Skills are defined in Markdown format with YAML frontmatter for metadata.
package skill

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"gopkg.in/yaml.v3"
)

// Skill represents a loaded skill definition from a skill.md file.
type Skill struct {
	ID           string                 `yaml:"id" json:"id"`
	Name         string                 `yaml:"name" json:"name"`
	Version      string                 `yaml:"version" json:"version"`
	Description  string                 `yaml:"description" json:"description"`
	Category     string                 `yaml:"category" json:"category"`
	Author       string                 `yaml:"author" json:"author"`
	Tags         []string               `yaml:"tags" json:"tags"`
	Dependencies []string               `yaml:"dependencies" json:"dependencies"`
	Parameters   map[string]Param       `yaml:"parameters" json:"parameters"`
	Examples     []Example              `yaml:"examples" json:"examples"`
	Content      string                 `json:"content"`                  // Markdown content (after frontmatter)
	RawContent   string                 `json:"-"`                        // Full file content
	SourcePath   string                 `json:"source_path"`              // Path to source file
	LoadedAt     time.Time              `json:"loaded_at"`                // When the skill was loaded
	Metadata     map[string]interface{} `yaml:"metadata" json:"metadata"` // Additional metadata
}

// Param defines a parameter for a skill.
type Param struct {
	Type        string      `yaml:"type" json:"type"`
	Description string      `yaml:"description" json:"description"`
	Required    bool        `yaml:"required" json:"required"`
	Default     interface{} `yaml:"default" json:"default"`
	Enum        []string    `yaml:"enum" json:"enum,omitempty"`
}

// Example provides an example usage of a skill.
type Example struct {
	Name        string                 `yaml:"name" json:"name"`
	Description string                 `yaml:"description" json:"description"`
	Inputs      map[string]interface{} `yaml:"inputs" json:"inputs"`
	Expected    interface{}            `yaml:"expected" json:"expected"`
}

// Loader handles loading skills from the filesystem.
type Loader struct {
	skillsDir    string
	loadedSkills map[string]*Skill
	mu           sync.RWMutex
}

// NewLoader creates a new skill loader for the given directory.
func NewLoader(skillsDir string) *Loader {
	return &Loader{
		skillsDir:    skillsDir,
		loadedSkills: make(map[string]*Skill),
	}
}

// LoadAll loads all skill.md files from the skills directory.
func (l *Loader) LoadAll() ([]*Skill, error) {
	entries, err := os.ReadDir(l.skillsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return []*Skill{}, nil
		}
		return nil, fmt.Errorf("failed to read skills directory: %w", err)
	}

	var skills []*Skill
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		name := entry.Name()
		if !strings.HasSuffix(name, ".md") {
			continue
		}

		skill, err := l.LoadFile(filepath.Join(l.skillsDir, name))
		if err != nil {
			return nil, fmt.Errorf("failed to load skill %s: %w", name, err)
		}

		skills = append(skills, skill)
		l.loadedSkills[skill.ID] = skill
	}

	return skills, nil
}

// LoadFile loads a single skill.md file.
func (l *Loader) LoadFile(path string) (*Skill, error) {
	// #nosec G304 - Path is validated by caller or comes from controlled config
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read file: %w", err)
	}

	skill, err := ParseSkill(string(data))
	if err != nil {
		return nil, fmt.Errorf("failed to parse skill: %w", err)
	}

	skill.SourcePath = path
	skill.LoadedAt = time.Now()

	return skill, nil
}

// LoadByID loads a skill by its ID.
func (l *Loader) LoadByID(id string) (*Skill, error) {
	l.mu.RLock()
	if skill, ok := l.loadedSkills[id]; ok {
		l.mu.RUnlock()
		return skill, nil
	}
	l.mu.RUnlock()

	if strings.ContainsAny(id, `/\..`) {
		return nil, fmt.Errorf("invalid skill ID: path characters not allowed")
	}

	path := filepath.Join(l.skillsDir, id+".md")
	absPath, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve path: %w", err)
	}
	absDir, err := filepath.Abs(l.skillsDir)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve skills directory: %w", err)
	}
	if !strings.HasPrefix(absPath, absDir+string(filepath.Separator)) {
		return nil, fmt.Errorf("invalid skill ID: path traversal detected")
	}

	if _, err := os.Stat(path); os.IsNotExist(err) {
		return nil, fmt.Errorf("skill not found: %s", id)
	}

	skill, err := l.LoadFile(path)
	if err != nil {
		return nil, err
	}

	l.mu.Lock()
	l.loadedSkills[id] = skill
	l.mu.Unlock()

	return skill, nil
}

// GetLoadedSkills returns all currently loaded skills.
func (l *Loader) GetLoadedSkills() map[string]*Skill {
	l.mu.RLock()
	defer l.mu.RUnlock()
	result := make(map[string]*Skill, len(l.loadedSkills))
	for k, v := range l.loadedSkills {
		result[k] = v
	}
	return result
}

// ParseSkill parses skill content from a string.
func ParseSkill(content string) (*Skill, error) {
	skill := &Skill{
		RawContent: content,
		Parameters: make(map[string]Param),
		Metadata:   make(map[string]interface{}),
	}

	// Check for YAML frontmatter
	if !strings.HasPrefix(content, "---") {
		// No frontmatter, treat entire content as markdown
		skill.Content = strings.TrimSpace(content)
		skill.ID = "unnamed-skill"
		skill.Name = "Unnamed Skill"
		skill.Version = "1.0.0"
		return skill, nil
	}

	// Extract frontmatter
	parts := strings.SplitN(content[3:], "---", 2)
	if len(parts) != 2 {
		return nil, fmt.Errorf("invalid frontmatter: missing closing ---")
	}

	frontmatter := strings.TrimSpace(parts[0])
	markdownContent := strings.TrimSpace(parts[1])

	// Parse YAML frontmatter
	if err := yaml.Unmarshal([]byte(frontmatter), skill); err != nil {
		return nil, fmt.Errorf("failed to parse frontmatter: %w", err)
	}

	skill.Content = markdownContent

	// Set defaults if not provided
	if skill.ID == "" {
		skill.ID = "unnamed-skill"
	}
	if skill.Name == "" {
		skill.Name = skill.ID
	}
	if skill.Version == "" {
		skill.Version = "1.0.0"
	}

	return skill, nil
}

// GetParameter returns a parameter definition by name.
func (s *Skill) GetParameter(name string) (Param, bool) {
	param, ok := s.Parameters[name]
	return param, ok
}

// HasTag checks if the skill has a specific tag.
func (s *Skill) HasTag(tag string) bool {
	for _, t := range s.Tags {
		if strings.EqualFold(t, tag) {
			return true
		}
	}
	return false
}

// HasDependency checks if the skill depends on another skill.
func (s *Skill) HasDependency(skillID string) bool {
	for _, dep := range s.Dependencies {
		if dep == skillID {
			return true
		}
	}
	return false
}

// Validate checks if the skill definition is valid.
func (s *Skill) Validate() error {
	if s.ID == "" {
		return fmt.Errorf("skill ID is required")
	}
	if s.Name == "" {
		return fmt.Errorf("skill name is required")
	}
	if s.Content == "" {
		return fmt.Errorf("skill content is required")
	}
	return nil
}

// Registry provides a centralized registry of loaded skills.
type Registry struct {
	skills map[string]*Skill
	loader *Loader
	mu     sync.RWMutex
}

// NewRegistry creates a new skill registry.
func NewRegistry(skillsDir string) *Registry {
	return &Registry{
		skills: make(map[string]*Skill),
		loader: NewLoader(skillsDir),
	}
}

// LoadAll loads all skills into the registry.
func (r *Registry) LoadAll() error {
	skills, err := r.loader.LoadAll()
	if err != nil {
		return err
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	for _, skill := range skills {
		r.skills[skill.ID] = skill
	}

	return nil
}

// Get retrieves a skill by ID.
func (r *Registry) Get(id string) (*Skill, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	skill, ok := r.skills[id]
	return skill, ok
}

// GetAll returns all skills in the registry.
func (r *Registry) GetAll() map[string]*Skill {
	r.mu.RLock()
	defer r.mu.RUnlock()
	result := make(map[string]*Skill, len(r.skills))
	for k, v := range r.skills {
		result[k] = v
	}
	return result
}

// GetByCategory returns all skills in a specific category.
func (r *Registry) GetByCategory(category string) []*Skill {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var result []*Skill
	for _, skill := range r.skills {
		if strings.EqualFold(skill.Category, category) {
			result = append(result, skill)
		}
	}
	return result
}

// GetByTag returns all skills with a specific tag.
func (r *Registry) GetByTag(tag string) []*Skill {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var result []*Skill
	for _, skill := range r.skills {
		if skill.HasTag(tag) {
			result = append(result, skill)
		}
	}
	return result
}

// Find searches for skills matching a query string.
func (r *Registry) Find(query string) []*Skill {
	r.mu.RLock()
	defer r.mu.RUnlock()
	query = strings.ToLower(query)
	var result []*Skill
	for _, skill := range r.skills {
		if strings.Contains(strings.ToLower(skill.Name), query) ||
			strings.Contains(strings.ToLower(skill.Description), query) ||
			skill.HasTag(query) {
			result = append(result, skill)
		}
	}
	return result
}
