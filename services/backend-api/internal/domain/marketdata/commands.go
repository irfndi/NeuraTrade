package marketdata

import (
	"time"

	"github.com/irfndi/neuratrade/internal/platform/actor"
)

// Commands sent to the MarketDataCollectorActor.

// StartExchangeCommand starts collection for an exchange.
type StartExchangeCommand struct {
	ExchangeID string
	Symbols    []string
	Interval   time.Duration
}

// StopExchangeCommand stops collection for an exchange.
type StopExchangeCommand struct {
	ExchangeID string
}

// PauseExchangeCommand pauses collection for an exchange.
type PauseExchangeCommand struct {
	ExchangeID string
}

// ResumeExchangeCommand resumes collection for an exchange.
type ResumeExchangeCommand struct {
	ExchangeID string
}

// UpdateSymbolsCommand updates the symbol list for an exchange.
type UpdateSymbolsCommand struct {
	ExchangeID string
	Symbols    []string
}

// SetIntervalCommand changes the collection interval.
type SetIntervalCommand struct {
	ExchangeID string
	Interval   time.Duration
}

// FetchNowCommand triggers immediate collection.
type FetchNowCommand struct {
	ExchangeID string
	Symbols    []string
}

// HealthCheckCommand requests health status.
type HealthCheckCommand struct {
	ExchangeID string
}

// HealthCheckResponse is the response to HealthCheckCommand.
type HealthCheckResponse struct {
	ExchangeID string
	Healthy    bool
	Ready      bool
	ErrorCount int
	LastError  string
	LastCheck  time.Time
}

// GetStatsCommand requests collection statistics.
type GetStatsCommand struct {
	ExchangeID string
}

// GetStatsResponse is the response to GetStatsCommand.
type GetStatsResponse struct {
	ExchangeID      string
	SymbolsCount    int
	LastCollection  time.Time
	CollectionCount int64
	ErrorCount      int64
	AvgLatencyMs    float64
}

// Ensure all commands implement actor.Message interface.
var _ actor.Message = (*StartExchangeCommand)(nil)
var _ actor.Message = (*StopExchangeCommand)(nil)
var _ actor.Message = (*PauseExchangeCommand)(nil)
var _ actor.Message = (*ResumeExchangeCommand)(nil)
var _ actor.Message = (*UpdateSymbolsCommand)(nil)
var _ actor.Message = (*SetIntervalCommand)(nil)
var _ actor.Message = (*FetchNowCommand)(nil)
var _ actor.Message = (*HealthCheckCommand)(nil)
var _ actor.Message = (*GetStatsCommand)(nil)

// MessageType implements actor.Message.
func (m StartExchangeCommand) MessageType() string {
	return "marketdata.start_exchange"
}

// MessageType implements actor.Message.
func (m StopExchangeCommand) MessageType() string {
	return "marketdata.stop_exchange"
}

// MessageType implements actor.Message.
func (m PauseExchangeCommand) MessageType() string {
	return "marketdata.pause_exchange"
}

// MessageType implements actor.Message.
func (m ResumeExchangeCommand) MessageType() string {
	return "marketdata.resume_exchange"
}

// MessageType implements actor.Message.
func (m UpdateSymbolsCommand) MessageType() string {
	return "marketdata.update_symbols"
}

// MessageType implements actor.Message.
func (m SetIntervalCommand) MessageType() string {
	return "marketdata.set_interval"
}

// MessageType implements actor.Message.
func (m FetchNowCommand) MessageType() string {
	return "marketdata.fetch_now"
}

// MessageType implements actor.Message.
func (m HealthCheckCommand) MessageType() string {
	return "marketdata.health_check"
}

// MessageType implements actor.Message.
func (m GetStatsCommand) MessageType() string {
	return "marketdata.get_stats"
}
