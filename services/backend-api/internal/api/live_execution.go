package api

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/irfndi/neuratrade/internal/app/execution"
	"github.com/irfndi/neuratrade/internal/app/execution/liveguard"
	"github.com/irfndi/neuratrade/internal/app/risk"
	"github.com/irfndi/neuratrade/internal/ccxt"
	"github.com/irfndi/neuratrade/internal/platform/actor"
	"github.com/irfndi/neuratrade/internal/platform/supervisor"
	"github.com/irfndi/neuratrade/internal/services"
	"github.com/shopspring/decimal"
)

type riskGatedLiveExecution struct {
	riskRef      *risk.RiskActorRef
	executionRef *actor.Ref
	riskActorRef *actor.Ref
	group        *supervisor.Group
	maxNotional  decimal.Decimal
	orderLookup  liveOrderLookup
}

type liveOrderLookup interface {
	FetchOrder(context.Context, string, string, string) (*ccxt.OrderResponse, error)
	FetchBalance(context.Context, string) (*ccxt.BalanceResponse, error)
}

func newRiskGatedLiveExecution(
	ctx context.Context,
	db *sql.DB,
	orderExecutor services.ScalpingOrderExecutor,
	killSwitch *risk.KillSwitchImpl,
	safeMode *risk.SafeModeImpl,
	liveGuard *liveguard.Guard,
	orderLookup liveOrderLookup,
) (*riskGatedLiveExecution, error) {
	if db == nil || orderExecutor == nil || killSwitch == nil || safeMode == nil || orderLookup == nil {
		return nil, errors.New("live execution requires database, executor, kill switch, safe mode, and order lookup")
	}
	maxNotional, err := decimal.NewFromString(strings.TrimSpace(os.Getenv("NEURATRADE_LIVE_MAX_ORDER_NOTIONAL")))
	if err != nil || !maxNotional.IsPositive() {
		return nil, errors.New("NEURATRADE_LIVE_MAX_ORDER_NOTIONAL must be a positive decimal")
	}

	policy := risk.NewEngine()
	if err := policy.AddRule(risk.NewMaxNotionalRule(maxNotional)); err != nil {
		return nil, fmt.Errorf("add live max-notional rule: %w", err)
	}
	riskActor, err := risk.NewRiskActor(risk.RiskActorConfig{
		ID:           "live-order-risk-actor",
		PolicyEngine: policy,
		KillSwitch:   killSwitch,
		SafeMode:     safeMode,
	})
	if err != nil {
		return nil, fmt.Errorf("create live order risk actor: %w", err)
	}
	idempotencyStore, err := execution.NewSQLIdempotencyStore(db)
	if err != nil {
		return nil, fmt.Errorf("create live order idempotency store: %w", err)
	}
	auditLog, err := execution.NewSQLAuditLogger(db)
	if err != nil {
		return nil, fmt.Errorf("create live order audit logger: %w", err)
	}
	gateway := &scalpingTradingGateway{executor: orderExecutor, orderLookup: orderLookup}
	executionActor := execution.NewExecutionActor(
		"live-order-execution-actor",
		gateway,
		nil,
		idempotencyStore,
		auditLog,
	).WithLiveGuard(liveGuard)

	riskActorRef := actor.NewRef(riskActor, actor.DefaultConfig())
	executionActorRef := actor.NewRef(executionActor, actor.DefaultConfig())
	group := supervisor.NewGroup()
	group.AddFunc("live-order-risk-actor", riskActorRef.Run)
	group.AddFunc("live-order-execution-actor", executionActorRef.Run)
	go func() {
		_ = group.Run(ctx)
	}()

	startCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		if riskActorRef.IsRunning() && executionActorRef.IsRunning() {
			return &riskGatedLiveExecution{
				riskRef:      risk.NewRiskActorRef(riskActorRef),
				executionRef: executionActorRef,
				riskActorRef: riskActorRef,
				group:        group,
				maxNotional:  maxNotional,
				orderLookup:  orderLookup,
			}, nil
		}
		select {
		case <-ticker.C:
		case <-startCtx.Done():
			_ = group.Shutdown(time.Second)
			return nil, fmt.Errorf("start live order actors: %w", startCtx.Err())
		}
	}
}

func (e *riskGatedLiveExecution) close() {
	if e == nil {
		return
	}
	_ = e.group.Shutdown(5 * time.Second)
	e.riskActorRef.Stop()
	e.executionRef.Stop()
}
