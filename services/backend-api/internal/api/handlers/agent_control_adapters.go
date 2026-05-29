package handlers

import (
	"context"

	"github.com/irfndi/neuratrade/internal/app/risk"
	"github.com/irfndi/neuratrade/internal/services"
)

type collectorAdapter struct {
	service *services.CollectorService
}

// NewCollectorController wraps a CollectorService behind the CollectorController
// interface. Returns nil when svc is nil so that handler nil-guards (503) fire
// correctly instead of falling through to a generic 500.
func NewCollectorController(svc *services.CollectorService) CollectorController {
	if svc == nil {
		return nil
	}
	return &collectorAdapter{service: svc}
}

func (a *collectorAdapter) PauseExchange(exchangeID string) error {
	return a.service.PauseExchange(exchangeID)
}

func (a *collectorAdapter) ResumeExchange(exchangeID string) error {
	return a.service.ResumeExchange(exchangeID)
}

type riskAdapter struct {
	killSwitch *risk.KillSwitchImpl
	safeMode   *risk.SafeModeImpl
}

// NewRiskControllerAdapter creates a RiskController backed by the given
// KillSwitch and SafeMode. Returns nil when either dependency is nil so that
// handler nil-guards produce proper 503 responses.
func NewRiskControllerAdapter(killSwitch *risk.KillSwitchImpl, safeMode *risk.SafeModeImpl) RiskController {
	if killSwitch == nil || safeMode == nil {
		return nil
	}
	return &riskAdapter{
		killSwitch: killSwitch,
		safeMode:   safeMode,
	}
}

func (a *riskAdapter) EnableSafeMode(ctx context.Context, reason string) error {
	return a.safeMode.EnableWithReason(ctx, reason)
}

func (a *riskAdapter) DisableSafeMode(ctx context.Context) error {
	return a.safeMode.Disable(ctx)
}

func (a *riskAdapter) EngageKillSwitch(ctx context.Context, reason string) error {
	return a.killSwitch.Engage(ctx, reason)
}

func (a *riskAdapter) DisengageKillSwitch(ctx context.Context) error {
	return a.killSwitch.Disengage(ctx)
}

type riskStateAdapter struct {
	killSwitch *risk.KillSwitchImpl
	safeMode   *risk.SafeModeImpl
}

type staticRiskStateProvider struct {
	state services.ScalpingRiskControlState
}

// NewScalpingRiskControlStateProvider exposes shared app risk controls to the
// scalping live gate.
func NewScalpingRiskControlStateProvider(killSwitch *risk.KillSwitchImpl, safeMode *risk.SafeModeImpl) services.ScalpingRiskControlStateProvider {
	if killSwitch == nil || safeMode == nil {
		return staticRiskStateProvider{state: services.ScalpingRiskControlState{
			SafeModeEnabled:   true,
			KillSwitchEngaged: true,
		}}
	}
	return &riskStateAdapter{
		killSwitch: killSwitch,
		safeMode:   safeMode,
	}
}

func (a staticRiskStateProvider) ScalpingRiskControlState(ctx context.Context) services.ScalpingRiskControlState {
	_ = ctx
	return a.state
}

func (a *riskStateAdapter) ScalpingRiskControlState(ctx context.Context) services.ScalpingRiskControlState {
	_ = ctx
	return services.ScalpingRiskControlState{
		SafeModeEnabled:   a.safeMode.IsEnabled(),
		KillSwitchEngaged: a.killSwitch.IsEngaged(),
	}
}
