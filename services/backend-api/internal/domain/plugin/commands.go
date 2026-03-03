package plugin

import "github.com/irfndi/neuratrade/internal/platform/actor"

type LoadPluginCommand struct{ ManifestPath string }
type UnloadPluginCommand struct{ PluginID string }
type EnablePluginCommand struct{ PluginID string }
type DisablePluginCommand struct{ PluginID string }
type ListPluginsCommand struct{}
type ListPluginsResponse struct{ Plugins []PluginInfo }
type GetPluginCommand struct{ PluginID string }
type GetPluginResponse struct{ Plugin PluginInfo }

var _ actor.Message = (*LoadPluginCommand)(nil)
var _ actor.Message = (*UnloadPluginCommand)(nil)
var _ actor.Message = (*EnablePluginCommand)(nil)
var _ actor.Message = (*DisablePluginCommand)(nil)
var _ actor.Message = (*ListPluginsCommand)(nil)
var _ actor.Message = (*GetPluginCommand)(nil)

func (m LoadPluginCommand) MessageType() string    { return "plugin.load" }
func (m UnloadPluginCommand) MessageType() string  { return "plugin.unload" }
func (m EnablePluginCommand) MessageType() string  { return "plugin.enable" }
func (m DisablePluginCommand) MessageType() string { return "plugin.disable" }
func (m ListPluginsCommand) MessageType() string   { return "plugin.list" }
func (m GetPluginCommand) MessageType() string     { return "plugin.get" }
