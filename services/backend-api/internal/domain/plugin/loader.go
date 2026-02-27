package plugin

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
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
		files, err := filepath.Glob(filepath.Join(dir, "*.yaml"))
		if err != nil {
			return nil, fmt.Errorf("failed to glob manifests in %s: %w", dir, err)
		}
		ymlFiles, _ := filepath.Glob(filepath.Join(dir, "*.yml"))
		files = append(files, ymlFiles...)
		jsonFiles, _ := filepath.Glob(filepath.Join(dir, "*.json"))
		files = append(files, jsonFiles...)
		for _, file := range files {
			manifest, err := l.loadFile(file)
			if err != nil {
				return nil, fmt.Errorf("failed to load %s: %w", file, err)
			}
			manifests = append(manifests, manifest)
		}
	}
	return manifests, nil
}

func (l *Loader) loadFile(path string) (PluginManifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return PluginManifest{}, fmt.Errorf("failed to read file: %w", err)
	}
	var manifest PluginManifest
	ext := filepath.Ext(path)
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
