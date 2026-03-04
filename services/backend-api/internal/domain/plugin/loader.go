package plugin

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

type Loader struct {
	manifestDirs []string
}

func NewLoader(manifestDirs ...string) *Loader {
	return &Loader{manifestDirs: manifestDirs}
}

func (l *Loader) LoadAll() ([]PluginManifest, error) {
	var manifests []PluginManifest
	for _, dir := range l.manifestDirs {
		files, err := l.globManifestFiles(dir)
		if err != nil {
			return nil, err
		}
		for _, file := range files {
			manifest, err := l.LoadManifest(file)
			if err != nil {
				return nil, fmt.Errorf("failed to load %s: %w", file, err)
			}
			manifests = append(manifests, manifest)
		}
	}
	return manifests, nil
}

func (l *Loader) LoadManifest(path string) (PluginManifest, error) {
	if !l.isAllowedManifestPath(path) {
		return PluginManifest{}, fmt.Errorf("manifest path %s is outside configured directories", path)
	}

	cleanPath := filepath.Clean(path)
	data, err := os.ReadFile(cleanPath) // #nosec G304 -- path is validated against configured manifest dirs
	if err != nil {
		return PluginManifest{}, fmt.Errorf("failed to read file: %w", err)
	}
	var manifest PluginManifest
	ext := filepath.Ext(cleanPath)
	switch ext {
	case ".yaml", ".yml":
		if err := yaml.Unmarshal(data, &manifest); err != nil {
			return PluginManifest{}, fmt.Errorf("failed to parse YAML: %w", err)
		}
	case ".json":
		if err := json.Unmarshal(data, &manifest); err != nil {
			return PluginManifest{}, fmt.Errorf("failed to parse JSON: %w", err)
		}
	default:
		return PluginManifest{}, fmt.Errorf("unsupported file extension: %s", ext)
	}
	now := time.Now()
	if manifest.CreatedAt.IsZero() {
		manifest.CreatedAt = now
	}
	if manifest.UpdatedAt.IsZero() {
		manifest.UpdatedAt = now
	}
	return manifest, nil
}

func (l *Loader) globManifestFiles(dir string) ([]string, error) {
	patterns := []string{"*.yaml", "*.yml", "*.json"}
	files := make([]string, 0)
	for _, pattern := range patterns {
		matches, err := filepath.Glob(filepath.Join(dir, pattern))
		if err != nil {
			return nil, fmt.Errorf("failed to glob %s in %s: %w", pattern, dir, err)
		}
		files = append(files, matches...)
	}
	return files, nil
}

func (l *Loader) isAllowedManifestPath(path string) bool {
	if len(l.manifestDirs) == 0 {
		return false
	}

	cleanPath := filepath.Clean(path)
	absPath, err := filepath.Abs(cleanPath)
	if err != nil {
		return false
	}

	for _, dir := range l.manifestDirs {
		cleanDir := filepath.Clean(dir)
		absDir, err := filepath.Abs(cleanDir)
		if err != nil {
			continue
		}

		if absPath == absDir {
			return true
		}
		prefix := absDir + string(os.PathSeparator)
		if strings.HasPrefix(absPath, prefix) {
			return true
		}
	}

	return false
}
