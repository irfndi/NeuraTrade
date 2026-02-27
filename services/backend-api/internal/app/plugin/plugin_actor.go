package plugin

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/irfndi/neuratrade/internal/domain/plugin"
	"github.com/irfndi/neuratrade/internal/logging"
	zaplogrus "github.com/irfndi/neuratrade/internal/logging/zaplogrus"
	"github.com/irfndi/neuratrade/internal/platform/actor"
	"github.com/irfndi/neuratrade/internal/platform/eventbus"
)

const PluginActorID = "plugin-manager"

type PluginActor struct {
	logger   *zaplogrus.Logger
	registry *plugin.Registry
	loader   *plugin.Loader
	eventBus *eventbus.Bus
	mu       sync.RWMutex
}

func NewPluginActor(registry *plugin.Registry, loader *plugin.Loader, eventBus *eventbus.Bus) *PluginActor {
	logger := zaplogrus.New()
	logger.SetLevel(logging.ParseLogrusLevel("info"))
	return &PluginActor{
		logger:   logger,
		registry: registry,
		loader:   loader,
		eventBus: eventBus,
	}
}

func (a *PluginActor) ID() string { return PluginActorID }

func (a *PluginActor) Receive(ctx context.Context, env actor.Envelope) error {
	traceID := env.TraceID
	if traceID == "" {
		traceID = uuid.New().String()
	}
	switch msg := env.Message.(type) {
	case plugin.LoadPluginCommand:
		return a.handleLoadPlugin(ctx, traceID, msg)
	case plugin.UnloadPluginCommand:
		return a.handleUnloadPlugin(ctx, traceID, msg)
	case plugin.EnablePluginCommand:
		return a.handleEnablePlugin(ctx, traceID, msg)
	case plugin.DisablePluginCommand:
		return a.handleDisablePlugin(ctx, traceID, msg)
	case plugin.ListPluginsCommand:
		return a.handleListPlugins(ctx, traceID, msg, env)
	case plugin.GetPluginCommand:
		return a.handleGetPlugin(ctx, traceID, msg, env)
	default:
		return fmt.Errorf("unknown message type: %T", msg)
	}
}

func (a *PluginActor) handleLoadPlugin(ctx context.Context, traceID string, msg plugin.LoadPluginCommand) error {
	manifests, err := a.loader.LoadAll()
	if err != nil {
		return fmt.Errorf("failed to load plugins: %w", err)
	}
	for _, manifest := range manifests {
		info := plugin.PluginInfo{
			Manifest: manifest,
			State:    plugin.PluginStateLoading,
			LoadedAt: time.Now(),
		}
		if err := a.registry.Register(info); err != nil {
			a.logger.Warnf("Failed to register plugin %s: %v", manifest.ID, err)
			continue
		}
		if manifest.Enabled {
			a.registry.Enable(manifest.ID)
		}
	}
	a.logger.Infof("Loaded %d plugins", len(manifests))
	return nil
}

func (a *PluginActor) handleUnloadPlugin(ctx context.Context, traceID string, msg plugin.UnloadPluginCommand) error {
	return a.registry.Unregister(msg.PluginID)
}

func (a *PluginActor) handleEnablePlugin(ctx context.Context, traceID string, msg plugin.EnablePluginCommand) error {
	return a.registry.Enable(msg.PluginID)
}

func (a *PluginActor) handleDisablePlugin(ctx context.Context, traceID string, msg plugin.DisablePluginCommand) error {
	return a.registry.Disable(msg.PluginID)
}

func (a *PluginActor) handleListPlugins(ctx context.Context, traceID string, msg plugin.ListPluginsCommand, env actor.Envelope) error {
	plugins := a.registry.List()
	if env.Reply != nil {
		env.Reply <- plugin.ListPluginsResponse{Plugins: plugins}
	}
	return nil
}

func (a *PluginActor) handleGetPlugin(ctx context.Context, traceID string, msg plugin.GetPluginCommand, env actor.Envelope) error {
	info, err := a.registry.Get(msg.PluginID)
	if err != nil {
		return err
	}
	if env.Reply != nil {
		env.Reply <- plugin.GetPluginResponse{Plugin: *info}
	}
	return nil
}
