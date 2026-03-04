package plugin

import "time"

type PluginState string

const (
	PluginStateLoading   PluginState = "loading"
	PluginStateActive    PluginState = "active"
	PluginStateInactive  PluginState = "inactive"
	PluginStateError     PluginState = "error"
	PluginStateUnloading PluginState = "unloading"
)

type PluginManifest struct {
	ID           string                 `json:"id" yaml:"id"`
	Name         string                 `json:"name" yaml:"name"`
	Version      string                 `json:"version" yaml:"version"`
	Description  string                 `json:"description" yaml:"description"`
	Type         string                 `json:"type" yaml:"type"`
	Author       string                 `json:"author,omitempty" yaml:"author,omitempty"`
	Enabled      bool                   `json:"enabled" yaml:"enabled"`
	ConfigSchema map[string]interface{} `json:"config_schema,omitempty" yaml:"config_schema,omitempty"`
	Dependencies []string               `json:"dependencies,omitempty" yaml:"dependencies,omitempty"`
	CreatedAt    time.Time              `json:"created_at" yaml:"created_at"`
	UpdatedAt    time.Time              `json:"updated_at" yaml:"updated_at"`
}

type PluginInfo struct {
	Manifest PluginManifest         `json:"manifest"`
	State    PluginState            `json:"state"`
	Error    string                 `json:"error,omitempty"`
	LoadedAt time.Time              `json:"loaded_at,omitempty"`
	Config   map[string]interface{} `json:"config,omitempty"`
}
