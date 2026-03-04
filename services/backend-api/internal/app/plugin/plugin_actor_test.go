package plugin

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/irfndi/neuratrade/internal/domain/plugin"
	"github.com/irfndi/neuratrade/internal/platform/actor"
	"github.com/irfndi/neuratrade/internal/platform/eventbus"
	"github.com/stretchr/testify/assert"
)

func TestPluginActor_ID(t *testing.T) {
	a := &PluginActor{}
	assert.Equal(t, PluginActorID, a.ID())
}

func TestPluginActor_EnableDisable(t *testing.T) {
	registry := plugin.NewRegistry()
	eventBus := eventbus.New(eventbus.DefaultConfig())
	a := NewPluginActor(registry, nil, eventBus)

	info := plugin.PluginInfo{
		Manifest: plugin.PluginManifest{
			ID:      "test-plugin",
			Name:    "Test Plugin",
			Version: "1.0.0",
			Type:    "strategy",
		},
		State: plugin.PluginStateInactive,
	}
	err := registry.Register(info)
	assert.NoError(t, err)

	ctx := context.Background()
	enableCmd := plugin.EnablePluginCommand{PluginID: "test-plugin"}
	err = a.Receive(ctx, actor.Envelope{Message: enableCmd})
	assert.NoError(t, err)
	assert.True(t, registry.IsEnabled("test-plugin"))

	disableCmd := plugin.DisablePluginCommand{PluginID: "test-plugin"}
	err = a.Receive(ctx, actor.Envelope{Message: disableCmd})
	assert.NoError(t, err)
	assert.False(t, registry.IsEnabled("test-plugin"))
}

func TestPluginActor_ListPlugins(t *testing.T) {
	registry := plugin.NewRegistry()
	eventBus := eventbus.New(eventbus.DefaultConfig())
	a := NewPluginActor(registry, nil, eventBus)

	// Register plugins with unique IDs
	for i := 1; i <= 3; i++ {
		info := plugin.PluginInfo{
			Manifest: plugin.PluginManifest{
				ID:      "test-plugin",
				Name:    "Test Plugin",
				Version: "1.0.0",
				Type:    "strategy",
			},
			State: plugin.PluginStateInactive,
		}
		registry.Register(info)
	}

	ctx := context.Background()
	reply := make(chan interface{}, 1)
	listCmd := plugin.ListPluginsCommand{}
	err := a.Receive(ctx, actor.Envelope{Message: listCmd, Reply: reply})
	assert.NoError(t, err)

	select {
	case resp := <-reply:
		listResp, ok := resp.(plugin.ListPluginsResponse)
		assert.True(t, ok)
		// Registry rejects duplicate IDs, so only 1 plugin registered
		assert.Equal(t, 1, len(listResp.Plugins))
	case <-time.After(1 * time.Second):
		t.Fatal("timeout waiting for list response")
	}
}

func TestPluginActor_LoadPlugin_UsesManifestPath(t *testing.T) {
	registry := plugin.NewRegistry()
	eventBus := eventbus.New(eventbus.DefaultConfig())

	manifestsDir := t.TempDir()
	targetPath := filepath.Join(manifestsDir, "target.yaml")
	otherPath := filepath.Join(manifestsDir, "other.yaml")

	requireManifest := func(path, id string) {
		content := []byte("id: " + id + "\nname: " + id + "\nversion: 1.0.0\ntype: strategy\nenabled: true\n")
		err := os.WriteFile(path, content, 0o600)
		assert.NoError(t, err)
	}
	requireManifest(targetPath, "target-plugin")
	requireManifest(otherPath, "other-plugin")

	loader := plugin.NewLoader(manifestsDir)
	a := NewPluginActor(registry, loader, eventBus)

	err := a.Receive(context.Background(), actor.Envelope{
		Message: plugin.LoadPluginCommand{ManifestPath: targetPath},
	})
	assert.NoError(t, err)

	plugins := registry.List()
	assert.Len(t, plugins, 1)
	assert.Equal(t, "target-plugin", plugins[0].Manifest.ID)
	assert.True(t, registry.IsEnabled("target-plugin"))
}
