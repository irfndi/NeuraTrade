// Package ports defines the application's port interfaces.
package ports

import (
	"context"
)

// ============================================================
// Plugin System - Extensibility contract
// ============================================================

// PluginType represents the type of a plugin.
type PluginType string

const (
	PluginTypeIndicator PluginType = "indicator"
	PluginTypeStrategy  PluginType = "strategy"
	PluginTypeRiskModel PluginType = "risk_model"
)

// PluginStatus represents the status of a plugin.
type PluginStatus string

const (
	PluginStatusLoaded   PluginStatus = "loaded"
	PluginStatusActive   PluginStatus = "active"
	PluginStatusInactive PluginStatus = "inactive"
	PluginStatusError    PluginStatus = "error"
)

// PluginManifest describes a plugin.
type PluginManifest struct {
	ID          string
	Name        string
	Version     string
	Type        PluginType
	Description string
	Author      string
	Config      map[string]any
	Enabled     bool
}

// PluginContext provides context for plugin execution.
type PluginContext struct {
	Exchange   string
	Symbol     string
	MarketData MarketDataContext
	Portfolio  PortfolioContext
	Config     map[string]any
}

// MarketDataContext provides market data for plugins.
type MarketDataContext struct {
	CurrentPrice float64
	Volume24h    float64
	High24h      float64
	Low24h       float64
	PriceChange  float64
	OrderBook    OrderBookContext
}

// OrderBookContext provides order book data.
type OrderBookContext struct {
	BidPrice  float64
	AskPrice  float64
	BidVolume float64
	AskVolume float64
	Spread    float64
	Imbalance float64
}

// PortfolioContext provides portfolio data.
type PortfolioContext struct {
	Positions     []PositionContext
	TotalValue    float64
	UnrealizedPnL float64
	RealizedPnL   float64
	Exposure      float64
}

// PositionContext provides position data.
type PositionContext struct {
	Symbol        string
	Side          string
	Amount        float64
	EntryPrice    float64
	CurrentPrice  float64
	UnrealizedPnL float64
}

// PluginResult represents the result of plugin execution.
type PluginResult struct {
	Value    float64
	Signal   *PluginSignal
	Metadata map[string]any
	Error    string
}

// PluginSignal represents a signal from a strategy plugin.
type PluginSignal struct {
	Side       string  // "buy", "sell", "hold"
	Confidence float64 // 0-1
	StopLoss   float64 // Optional
	TakeProfit float64 // Optional
	Reason     string
}

// IndicatorPlugin provides technical indicators.
type IndicatorPlugin interface {
	// ID returns the plugin ID.
	ID() string

	// Name returns the plugin name.
	Name() string

	// Type returns the plugin type.
	Type() PluginType

	// Calculate calculates the indicator value.
	Calculate(ctx context.Context, input PluginContext) (PluginResult, error)

	// Configure configures the plugin.
	Configure(config map[string]any) error

	// Validate validates the plugin configuration.
	Validate() error
}

// StrategyPlugin provides trading strategies.
type StrategyPlugin interface {
	IndicatorPlugin

	// GenerateSignal generates a trading signal.
	GenerateSignal(ctx context.Context, input PluginContext) (*PluginSignal, error)

	// RequiredIndicators returns the indicators this strategy depends on.
	RequiredIndicators() []string
}

// RiskModelPlugin provides risk models.
type RiskModelPlugin interface {
	IndicatorPlugin

	// Evaluate evaluates a trading signal for risk.
	Evaluate(ctx context.Context, signal PluginSignal, portfolio PortfolioContext) (RiskEvaluation, error)
}

// RiskEvaluation represents a risk evaluation result.
type RiskEvaluation struct {
	Approved     bool
	Reason       string
	RiskScore    float64 // 0-1, higher is riskier
	AdjustedSize float64 // Suggested position size adjustment
	LimitHit     string  // If rejected, which limit was hit
}

// ============================================================
// Plugin Registry
// ============================================================

// PluginRegistry manages plugins.
type PluginRegistry interface {
	// Register registers a plugin.
	Register(plugin IndicatorPlugin) error

	// Unregister unregisters a plugin.
	Unregister(pluginID string) error

	// Get retrieves a plugin by ID.
	Get(pluginID string) (IndicatorPlugin, error)

	// GetByType retrieves plugins by type.
	GetByType(pluginType PluginType) ([]IndicatorPlugin, error)

	// List lists all registered plugins.
	List() []PluginManifest

	// Load loads a plugin from a manifest.
	Load(manifest PluginManifest) error

	// Unload unloads a plugin.
	Unload(pluginID string) error

	// Enable enables a plugin.
	Enable(pluginID string) error

	// Disable disables a plugin.
	Disable(pluginID string) error

	// IsEnabled checks if a plugin is enabled.
	IsEnabled(pluginID string) bool
}

// ============================================================
// Plugin Executor
// ============================================================

// PluginExecutor executes plugins safely.
type PluginExecutor interface {
	// Execute executes a plugin with timeout and resource limits.
	Execute(ctx context.Context, pluginID string, input PluginContext) (PluginResult, error)

	// ExecuteBatch executes multiple plugins in parallel.
	ExecuteBatch(ctx context.Context, plugins []string, input PluginContext) (map[string]PluginResult, error)
}
