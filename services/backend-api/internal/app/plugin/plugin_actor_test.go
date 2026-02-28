package plugin

import (
	"context"
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

	// Register a test plugin
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

	// Enable plugin
	ctx := context.Background()
	enableCmd := plugin.EnablePluginCommand{PluginID: "test-plugin"}
	err = a.Receive(ctx, actor.Envelope{Message: enableCmd})
	assert.NoError(t, err)
	assert.True(t, registry.IsEnabled("test-plugin"))

	// Disable plugin
	disableCmd := plugin.DisablePluginCommand{PluginID: "test-plugin"}
	err = a.Receive(ctx, actor.Envelope{Message: disableCmd})
	assert.NoError(t, err)
	assert.False(t, registry.IsEnabled("test-plugin"))
}

func TestPluginActor_ListPlugins(t *testing.T) {
	registry := plugin.NewRegistry()
	eventBus := eventbus.New(eventbus.DefaultConfig())
	a := NewPluginActor(registry, nil, eventBus)

	// Register test plugins with unique IDs
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

	// List plugins
	ctx := context.Background()
	reply := make(chan interface{}, 1)
	listCmd := plugin.ListPluginsCommand{}
	err := a.Receive(ctx, actor.Envelope{Message: listCmd, Reply: reply})
	assert.NoError(t, err)

	select {
	case resp := <-reply:
		listResp, ok := resp.(plugin.ListPluginsResponse)
		assert.True(t, ok)
		assert.Equal(t, 3, len(listResp.Plugins))
	case <-time.After(1 * time.Second):
		t.Fatal("timeout waiting for list response")
	}
}
