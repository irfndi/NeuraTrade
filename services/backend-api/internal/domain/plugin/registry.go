package plugin

import (
	"fmt"
	"sync"
)

type Registry struct {
	mu      sync.RWMutex
	plugins map[string]*PluginInfo
}

func NewRegistry() *Registry {
	return &Registry{
		plugins: make(map[string]*PluginInfo),
	}
}

func (r *Registry) Register(info PluginInfo) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.plugins[info.Manifest.ID]; exists {
		return fmt.Errorf("plugin %s already registered", info.Manifest.ID)
	}
	cloned := clonePluginInfo(info)
	r.plugins[info.Manifest.ID] = &cloned
	return nil
}

func (r *Registry) Unregister(pluginID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.plugins[pluginID]; !exists {
		return fmt.Errorf("plugin %s not found", pluginID)
	}
	delete(r.plugins, pluginID)
	return nil
}

func (r *Registry) Get(pluginID string) (PluginInfo, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	info, exists := r.plugins[pluginID]
	if !exists {
		return PluginInfo{}, fmt.Errorf("plugin %s not found", pluginID)
	}
	return clonePluginInfo(*info), nil
}

func (r *Registry) List() []PluginInfo {
	r.mu.RLock()
	defer r.mu.RUnlock()
	result := make([]PluginInfo, 0, len(r.plugins))
	for _, info := range r.plugins {
		result = append(result, clonePluginInfo(*info))
	}
	return result
}

func (r *Registry) Enable(pluginID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	info, exists := r.plugins[pluginID]
	if !exists {
		return fmt.Errorf("plugin %s not found", pluginID)
	}
	info.State = PluginStateActive
	return nil
}

func (r *Registry) Disable(pluginID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	info, exists := r.plugins[pluginID]
	if !exists {
		return fmt.Errorf("plugin %s not found", pluginID)
	}
	info.State = PluginStateInactive
	return nil
}

func (r *Registry) IsEnabled(pluginID string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	info, exists := r.plugins[pluginID]
	if !exists {
		return false
	}
	return info.State == PluginStateActive
}

func clonePluginInfo(in PluginInfo) PluginInfo {
	out := in
	out.Manifest = clonePluginManifest(in.Manifest)
	if in.Config != nil {
		out.Config = cloneMapStringAny(in.Config)
	}
	return out
}

func clonePluginManifest(in PluginManifest) PluginManifest {
	out := in
	if in.ConfigSchema != nil {
		out.ConfigSchema = cloneMapStringAny(in.ConfigSchema)
	}
	if in.Dependencies != nil {
		out.Dependencies = append([]string(nil), in.Dependencies...)
	}
	return out
}

func cloneMapStringAny(in map[string]interface{}) map[string]interface{} {
	out := make(map[string]interface{}, len(in))
	for k, v := range in {
		out[k] = cloneAny(v)
	}
	return out
}

func cloneAny(v interface{}) interface{} {
	switch typed := v.(type) {
	case map[string]interface{}:
		return cloneMapStringAny(typed)
	case []interface{}:
		out := make([]interface{}, len(typed))
		for i := range typed {
			out[i] = cloneAny(typed[i])
		}
		return out
	case []string:
		return append([]string(nil), typed...)
	default:
		return typed
	}
}
