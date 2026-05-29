package bootstrap

import (
	"testing"

	appstrategy "github.com/irfndi/neuratrade/internal/app/strategy"
	"github.com/irfndi/neuratrade/internal/platform/actor"
	"github.com/irfndi/neuratrade/internal/platform/eventbus"
	"github.com/irfndi/neuratrade/internal/platform/retry"
	"github.com/irfndi/neuratrade/internal/platform/supervisor"
	"go.uber.org/goleak"
)

func TestBootstrapRollbackScenarios(t *testing.T) {
	tests := []struct {
		name string
		run  func(t *testing.T)
	}{
		{
			name: "build returns nil app when strategy build fails after risk startup",
			run: func(t *testing.T) {
				defer goleak.VerifyNone(t)

				cfg := DefaultConfig()
				cfg.RiskActorID = "duplicate-risk-strategy-actor"
				strategyCfg := appstrategy.DefaultConfig()
				strategyCfg.ActorID = cfg.RiskActorID

				app, err := NewBuilder().
					WithConfig(cfg).
					WithStrategy(appstrategy.NewStrategyActor(strategyCfg, nil, nil)).
					Build()

				if err == nil {
					t.Fatal("expected duplicate actor id to fail strategy component build")
				}
				if app != nil {
					t.Fatalf("expected failed build to return nil app, got %#v", app)
				}
			},
		},
		{
			name: "rollbackPartialBuild stops and clears started risk actor",
			run: func(t *testing.T) {
				cfg := DefaultConfig()
				app := &Application{
					Config:      cfg,
					Supervisor:  supervisor.New(),
					ActorSystem: actor.NewSystem(cfg.Actor),
					EventBus:    eventbus.New(cfg.EventBus),
					Timeout:     &cfg.Timeout,
					Retry:       retry.NewPolicy(cfg.Retry),
				}

				if err := app.buildRiskComponents(NewBuilder().WithConfig(cfg)); err != nil {
					t.Fatalf("build risk components: %v", err)
				}

				ref, ok := app.ActorSystem.Get(riskActorID(cfg.RiskActorID))
				if !ok {
					t.Fatal("expected risk actor to be registered")
				}
				if !ref.IsRunning() {
					t.Fatal("expected risk actor to be running before rollback")
				}

				actorSystem := app.ActorSystem
				app.rollbackPartialBuild()

				if ref.IsRunning() {
					t.Fatal("expected risk actor to stop after rollback")
				}
				if _, ok := actorSystem.Get(riskActorID(cfg.RiskActorID)); ok {
					t.Fatal("expected risk actor to be removed from actor system after rollback")
				}
				if app.ActorSystem != nil || app.EventBus != nil {
					t.Fatalf("expected stopped runtime subsystems to be cleared, got ActorSystem=%#v EventBus=%#v", app.ActorSystem, app.EventBus)
				}
				if app.RiskRef != nil || app.RiskActor != nil || app.StrategyActorRef != nil || app.StrategyActor != nil ||
					app.CollectorActorRef != nil || app.CollectorActor != nil {
					t.Fatalf("expected actor references to be cleared, got app=%#v", app)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, tt.run)
	}
}
