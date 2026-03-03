package plugin

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestRegistry_EnableDisable_ThreadSafety(t *testing.T) {
	registry := NewRegistry()

	// Register test plugin
	info := PluginInfo{
		Manifest: PluginManifest{
			ID:      "test-plugin",
			Name:    "Test Plugin",
			Version: "1.0.0",
			Type:    "strategy",
		},
		State: PluginStateInactive,
	}
	err := registry.Register(info)
	assert.NoError(t, err)

	// Concurrent enable/disable operations
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			registry.Enable("test-plugin")
		}()
		go func() {
			defer wg.Done()
			registry.Disable("test-plugin")
		}()
	}

	wg.Wait()
	// Should not panic - thread-safe
	assert.True(t, true)
}

func TestRegistry_ConfigReloadSafe(t *testing.T) {
	registry := NewRegistry()

	// Register plugin
	info := PluginInfo{
		Manifest: PluginManifest{
			ID:      "test-plugin",
			Name:    "Test Plugin",
			Version: "1.0.0",
			Type:    "strategy",
			ConfigSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"scale_factor": map[string]interface{}{
						"type":    "number",
						"default": 1.0,
					},
				},
			},
		},
		State:  PluginStateActive,
		Config: map[string]interface{}{"scale_factor": 1.5},
	}
	err := registry.Register(info)
	assert.NoError(t, err)

	// Simulate config reload - enable/disable should preserve state
	registry.Disable("test-plugin")
	assert.False(t, registry.IsEnabled("test-plugin"))

	registry.Enable("test-plugin")
	assert.True(t, registry.IsEnabled("test-plugin"))

	// Config should be preserved
	pluginInfo, err := registry.Get("test-plugin")
	assert.NoError(t, err)
	assert.Equal(t, 1.5, pluginInfo.Config["scale_factor"])
}

func TestRegistry_Get_ReturnsCopy(t *testing.T) {
	registry := NewRegistry()

	info := PluginInfo{
		Manifest: PluginManifest{
			ID:      "copy-plugin",
			Name:    "Copy Plugin",
			Version: "1.0.0",
			Type:    "strategy",
			ConfigSchema: map[string]interface{}{
				"nested": map[string]interface{}{
					"enabled": true,
				},
			},
			Dependencies: []string{"dep-a"},
		},
		State:  PluginStateInactive,
		Config: map[string]interface{}{"threshold": 0.5},
	}
	err := registry.Register(info)
	assert.NoError(t, err)

	got, err := registry.Get("copy-plugin")
	assert.NoError(t, err)
	got.State = PluginStateActive
	got.Config["threshold"] = 0.9
	got.Manifest.ConfigSchema["nested"] = map[string]interface{}{"enabled": false}
	got.Manifest.Dependencies[0] = "dep-b"

	stored, err := registry.Get("copy-plugin")
	assert.NoError(t, err)
	assert.Equal(t, PluginStateInactive, stored.State)
	assert.Equal(t, 0.5, stored.Config["threshold"])
	assert.Equal(t, map[string]interface{}{"enabled": true}, stored.Manifest.ConfigSchema["nested"])
	assert.Equal(t, []string{"dep-a"}, stored.Manifest.Dependencies)
}
