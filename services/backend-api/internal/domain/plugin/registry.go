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
	r.plugins[info.Manifest.ID] = &info
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

func (r *Registry) Get(pluginID string) (*PluginInfo, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	info, exists := r.plugins[pluginID]
	if !exists {
		return nil, fmt.Errorf("plugin %s not found", pluginID)
	}
	return info, nil
}

func (r *Registry) List() []PluginInfo {
	r.mu.RLock()
	defer r.mu.RUnlock()
	result := make([]PluginInfo, 0, len(r.plugins))
	for _, info := range r.plugins {
		result = append(result, *info)
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
