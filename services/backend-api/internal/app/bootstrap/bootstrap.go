// Package bootstrap provides application initialization and dependency wiring.
package bootstrap

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/irfndi/neuratrade/internal/adapters/ccxt"
	"github.com/irfndi/neuratrade/internal/app/marketdata"
	"github.com/irfndi/neuratrade/internal/app/risk"
	ccxtservice "github.com/irfndi/neuratrade/internal/ccxt"
	"github.com/irfndi/neuratrade/internal/platform/actor"
	"github.com/irfndi/neuratrade/internal/platform/eventbus"
	"github.com/irfndi/neuratrade/internal/platform/retry"
	"github.com/irfndi/neuratrade/internal/platform/supervisor"
	"github.com/irfndi/neuratrade/internal/platform/timeout"
	"github.com/irfndi/neuratrade/internal/ports"
	"github.com/shopspring/decimal"
)

// Config holds bootstrap configuration.
type Config struct {
	// Timeout configuration
	Timeout timeout.Config

	// Retry configuration
	Retry retry.Config

	// Actor system configuration
	Actor actor.Config

	// EventBus configuration
	EventBus eventbus.Config

	// Risk configuration
	Risk RiskConfig
}

// RiskConfig holds risk system configuration.
type RiskConfig struct {
	// MaxDrawdown is the maximum allowed drawdown (e.g., 0.1 = 10%)
	MaxDrawdown decimal.Decimal

	// MaxDailyLoss is the maximum daily loss
	MaxDailyLoss decimal.Decimal

	// CooldownPeriod after consecutive losses
	CooldownPeriod time.Duration

	// CooldownAfterLosses triggers cooldown after this many losses
	CooldownAfterLosses int

	// SafeMode configures safe mode behavior
	SafeMode risk.SafeModeConfig
}

// DefaultRiskConfig returns default risk configuration.
func DefaultRiskConfig() RiskConfig {
	return RiskConfig{
		MaxDrawdown:         decimal.NewFromFloat(0.1),  // 10%
		MaxDailyLoss:        decimal.NewFromFloat(0.05), // 5%
		CooldownPeriod:      5 * time.Minute,
		CooldownAfterLosses: 3,
		SafeMode:            risk.DefaultSafeModeConfig(),
	}
}

// DefaultConfig returns a Config with sensible defaults.
func DefaultConfig() Config {
	return Config{
		Timeout:  timeout.DefaultConfig(),
		Retry:    retry.DefaultConfig(),
		Actor:    actor.DefaultConfig(),
		EventBus: eventbus.DefaultConfig(),
		Risk:     DefaultRiskConfig(),
	}
}

// Application holds all initialized components.
type Application struct {
	Config            Config
	Supervisor        *supervisor.Supervisor
	ActorSystem       *actor.System
	EventBus          *eventbus.Bus
	Timeout           *timeout.Config
	Retry             *retry.Policy
	Exchange          ports.ExchangeRegistry
	State             ports.StateStore
	Notifier          ports.Notifier
	Policy            ports.PolicyEngine
	KillSwitch        ports.KillSwitch
	SafeMode          *risk.SafeModeImpl
	RiskActor         *risk.RiskActor
	RiskRef           *risk.RiskActorRef
	CollectorActor    *marketdata.CollectorActor
	CollectorActorRef *actor.Ref
}

// Builder builds an Application.
type Builder struct {
	config     Config
	exchanges  ports.ExchangeRegistry
	state      ports.StateStore
	notifier   ports.Notifier
	policy     ports.PolicyEngine
	killSwitch ports.KillSwitch
	safeMode   *risk.SafeModeImpl
	collector  *marketdata.CollectorActor
}

// NewBuilder creates a new Builder.
func NewBuilder() *Builder {
	return &Builder{
		config: DefaultConfig(),
	}
}

// WithConfig sets the configuration.
func (b *Builder) WithConfig(config Config) *Builder {
	b.config = config
	return b
}

// WithExchanges sets the exchange registry.
func (b *Builder) WithExchanges(registry ports.ExchangeRegistry) *Builder {
	b.exchanges = registry
	return b
}

// WithStateStore sets the state store.
func (b *Builder) WithStateStore(store ports.StateStore) *Builder {
	b.state = store
	return b
}

// WithNotifier sets the notifier.
func (b *Builder) WithNotifier(notifier ports.Notifier) *Builder {
	b.notifier = notifier
	return b
}

// WithPolicyEngine sets the policy engine.
func (b *Builder) WithPolicyEngine(engine ports.PolicyEngine) *Builder {
	b.policy = engine
	return b
}

// WithKillSwitch sets the kill switch.
func (b *Builder) WithKillSwitch(ks ports.KillSwitch) *Builder {
	b.killSwitch = ks
	return b
}

// WithSafeMode sets the safe mode controller.
func (b *Builder) WithSafeMode(sm *risk.SafeModeImpl) *Builder {
	b.safeMode = sm
	return b
}

// WithCollector sets the collector actor.
func (b *Builder) WithCollector(collector *marketdata.CollectorActor) *Builder {
	b.collector = collector
	return b
}

// Build builds the Application.
func (b *Builder) Build() *Application {
	app := &Application{
		Config:      b.config,
		Supervisor:  supervisor.New(),
		ActorSystem: actor.NewSystem(b.config.Actor),
		EventBus:    eventbus.New(b.config.EventBus),
		Timeout:     &b.config.Timeout,
		Retry:       retry.NewPolicy(b.config.Retry),
		Exchange:    b.exchanges,
		State:       b.state,
		Notifier:    b.notifier,
	}

	// Build risk components if not provided
	app.buildRiskComponents(b)

	return app
}

// buildRiskComponents builds the risk system components.
func (a *Application) buildRiskComponents(b *Builder) {
	// Create safe mode if not provided
	if b.safeMode != nil {
		a.SafeMode = b.safeMode
	} else {
		a.SafeMode = risk.NewSafeMode(b.config.Risk.SafeMode)
	}

	// Create kill switch if not provided
	if b.killSwitch != nil {
		a.KillSwitch = b.killSwitch
	} else {
		a.KillSwitch = risk.NewKillSwitch()
	}

	// Create policy engine if not provided
	if b.policy != nil {
		a.Policy = b.policy
	} else {
		policyEngine := risk.NewEngine()
		// Add default rules from config
		if b.config.Risk.MaxDrawdown.GreaterThan(decimal.Zero) {
			threshold := b.config.Risk.MaxDrawdown
			if err := policyEngine.AddRule(risk.NewMaxDrawdownRule(threshold)); err != nil {
				wrappedErr := fmt.Errorf("adding max_drawdown rule (threshold=%s): %w", threshold.String(), err)
				log.Printf("[bootstrap] %v", wrappedErr)
			}
		}
		if b.config.Risk.MaxDailyLoss.GreaterThan(decimal.Zero) {
			threshold := b.config.Risk.MaxDailyLoss
			if err := policyEngine.AddRule(risk.NewMaxDailyLossRule(threshold)); err != nil {
				wrappedErr := fmt.Errorf("adding max_daily_loss rule (threshold=%s): %w", threshold.String(), err)
				log.Printf("[bootstrap] %v", wrappedErr)
			}
		}
		if b.config.Risk.CooldownPeriod > 0 && b.config.Risk.CooldownAfterLosses > 0 {
			if err := policyEngine.AddRule(risk.NewCooldownRule(
				b.config.Risk.CooldownPeriod,
				b.config.Risk.CooldownAfterLosses)); err != nil {
				wrappedErr := fmt.Errorf(
					"adding cooldown rule (period=%s, losses=%d): %w",
					b.config.Risk.CooldownPeriod,
					b.config.Risk.CooldownAfterLosses,
					err,
				)
				log.Printf("[bootstrap] %v", wrappedErr)
			}
		}
		a.Policy = policyEngine
	}

	// Create risk actor
	ks, ok := a.KillSwitch.(*risk.KillSwitchImpl)
	if !ok && a.KillSwitch != nil {
		log.Printf("[bootstrap] custom kill switch type %T provided; skipping RiskActor creation", a.KillSwitch)
		return
	}
	var policyEngine *risk.Engine
	if pe, ok := a.Policy.(*risk.Engine); ok {
		policyEngine = pe
	} else if a.Policy != nil {
		log.Printf("[bootstrap] custom policy engine type %T provided; RiskActor will use fallback policy", a.Policy)
	}
	a.RiskActor = risk.NewRiskActor(risk.RiskActorConfig{
		ID:                  "risk-actor",
		PolicyEngine:        policyEngine,
		KillSwitch:          ks,
		SafeMode:            a.SafeMode,
		EventBus:            a.EventBus,
		MaxDrawdown:         b.config.Risk.MaxDrawdown,
		MaxDailyLoss:        b.config.Risk.MaxDailyLoss,
		CooldownPeriod:      b.config.Risk.CooldownPeriod,
		CooldownAfterLosses: b.config.Risk.CooldownAfterLosses,
	})

	// Spawn risk actor in actor system
	ref, err := a.ActorSystem.Spawn(a.RiskActor, actor.DefaultConfig())
	if err != nil {
		log.Printf("[bootstrap] failed to spawn risk actor: %v", err)
		return
	}

	a.RiskRef = risk.NewRiskActorRef(ref)
	a.Supervisor.AddFunc("risk-actor", func(ctx context.Context) error {
		if runErr := ref.Run(ctx); runErr != nil && runErr != context.Canceled {
			return fmt.Errorf("run risk actor: %w", runErr)
		}
		return nil
	})
}

// Run starts the application and blocks until context is cancelled.
func (a *Application) Run(ctx context.Context) error {
	// Start event bus
	a.Supervisor.AddFunc("eventbus", func(ctx context.Context) error {
		<-ctx.Done()
		a.EventBus.Stop()
		return nil
	})

	// Run supervisor
	return a.Supervisor.Run(ctx)
}

func (a *Application) Shutdown(timeoutMs int) error {
	return a.Supervisor.Shutdown(time.Duration(timeoutMs) * time.Millisecond)
}

// HealthCheck performs a health check on all components.
func (a *Application) HealthCheck(ctx context.Context) error {
	// Check state store
	if a.State != nil {
		if err := a.State.Health(ctx); err != nil {
			return fmt.Errorf("state store unhealthy: %w", err)
		}
	}

	// Check exchanges
	if a.Exchange != nil {
		exchanges, err := a.Exchange.ListExchanges(ctx)
		if err != nil {
			return fmt.Errorf("failed to list exchanges for health check: %w", err)
		}
		for _, ex := range exchanges {
			gw, err := a.Exchange.GetMarketDataGateway(ex.ID)
			if err != nil {
				continue
			}
			if gw != nil && !gw.IsHealthy(ctx) {
				return fmt.Errorf("exchange %s unhealthy", ex.ID)
			}
		}
	}

	// Check kill switch
	if a.KillSwitch != nil && a.KillSwitch.IsEngaged() {
		return fmt.Errorf("kill switch is engaged")
	}

	return nil
}

// IsHealthy returns true if the application is healthy.
func (a *Application) IsHealthy(ctx context.Context) bool {
	return a.HealthCheck(ctx) == nil
}

// IsReady returns true if the application is ready to serve requests.
func (a *Application) IsReady(ctx context.Context) bool {
	// Ready implies healthy + not in safe mode + kill switch not engaged
	if !a.IsHealthy(ctx) {
		return false
	}

	if a.KillSwitch != nil && a.KillSwitch.IsEngaged() {
		return false
	}

	return true
}

// ExchangeRegistryBuilder helps build an exchange registry.
type ExchangeRegistryBuilder struct {
	registry *ccxt.Registry
}

// NewExchangeRegistryBuilder creates a new exchange registry builder.
func NewExchangeRegistryBuilder() *ExchangeRegistryBuilder {
	return &ExchangeRegistryBuilder{
		registry: ccxt.NewRegistry(),
	}
}

func (b *ExchangeRegistryBuilder) AddExchange(exchange string, service ccxtservice.CCXTService) *ExchangeRegistryBuilder {
	adapter := ccxt.NewAdapter(service)
	b.registry.Register(exchange, adapter)
	return b
}

// Build returns the built registry.
func (b *ExchangeRegistryBuilder) Build() ports.ExchangeRegistry {
	return b.registry
}
