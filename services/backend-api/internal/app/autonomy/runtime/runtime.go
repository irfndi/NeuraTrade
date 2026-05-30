package runtime

import (
	zaplogrus "github.com/irfndi/neuratrade/internal/logging/zaplogrus"
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/irfndi/neuratrade/internal/services"
)

type Dependencies struct {
	TechnicalAnalysis   *services.TechnicalAnalysisService //nolint:staticcheck // SA1019: deprecated, backward compat until scalping migration
	CCXTService         interface{}
	ArbitrageService    interface{}
	FuturesArbService   interface{}
	NotificationService *services.NotificationService
	MonitoringService   *services.AutonomousMonitorManager
	SQLDB               *sql.DB
}

func BuildLocalIntegratedHandlers(deps Dependencies) *services.IntegratedQuestHandlers {
	handlers := services.NewIntegratedQuestHandlers(
		deps.TechnicalAnalysis,
		deps.CCXTService,
		deps.ArbitrageService,
		deps.FuturesArbService,
		deps.NotificationService,
		deps.MonitoringService,
	)
	handlers.SetDB(deps.SQLDB)
	return handlers
}

func BuildIntegratedHandlers(deps Dependencies) (*services.IntegratedQuestHandlers, error) {
	if deps.SQLDB == nil {
		return nil, fmt.Errorf("build integrated autonomy handlers: sql db is nil")
	}

	schemaCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := EnsureAutonomySchema(schemaCtx, deps.SQLDB); err != nil {
		return nil, fmt.Errorf("build integrated autonomy handlers: ensure autonomy schema: %w", err)
	}

	handlers, err := services.NewIntegratedQuestHandlersWithAutonomyStore(
		deps.TechnicalAnalysis,
		deps.CCXTService,
		deps.ArbitrageService,
		deps.FuturesArbService,
		deps.NotificationService,
		deps.MonitoringService,
		deps.SQLDB,
		nil,
	)
	if err != nil {
		return nil, fmt.Errorf("build integrated autonomy handlers: %w", err)
	}
	return handlers, nil
}

func RegisterQuestRuntime(engine *services.QuestEngine, handlers *services.IntegratedQuestHandlers) error {
	if engine == nil {
		return fmt.Errorf("quest engine is nil")
	}
	if handlers == nil {
		return fmt.Errorf("integrated handlers are nil")
	}

	// Keep runtime registration owned by internal/app/autonomy/runtime.
	handlers.SetQuestEngine(engine)
	engine.RegisterHandler(services.QuestTypeRoutine, handlers.ExecuteRoutine)
	engine.RegisterHandler(services.QuestTypeTriggered, handlers.ExecuteRoutine)
	engine.RegisterHandler(services.QuestTypeGoal, handlers.ExecuteRoutine)
	engine.RegisterHandler(services.QuestTypeArbitrage, handlers.ExecuteArbitrage)
	zaplogrus.Info("[AUTONOMY-RUNTIME] integrated quest runtime registered")
	return nil
}

func EnsureAutonomySchema(ctx context.Context, db *sql.DB) error {
	if db == nil {
		return fmt.Errorf("autonomy schema requires sql db")
	}
	store := services.NewAutonomousRolloutStore(db)
	if err := store.InitSchema(ctx); err != nil {
		return fmt.Errorf("init autonomy schema: %w", err)
	}
	return nil
}
