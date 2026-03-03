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
