package runtime

import (
	"context"
	"database/sql"
	"fmt"
	"log"

	"github.com/irfndi/neuratrade/internal/services"
)

type Dependencies struct {
	TechnicalAnalysis   *services.TechnicalAnalysisService
	CCXTService         interface{}
	ArbitrageService    interface{}
	FuturesArbService   interface{}
	NotificationService *services.NotificationService
	MonitoringService   *services.AutonomousMonitorManager
	SQLDB               *sql.DB
}

func BuildIntegratedHandlers(deps Dependencies) (*services.IntegratedQuestHandlers, error) {
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
	engine.RegisterHandler(services.QuestTypeArbitrage, handlers.ExecuteArbitrage)
	log.Println("[AUTONOMY-RUNTIME] integrated quest runtime registered")
	return nil
}

func EnsureAutonomySchema(ctx context.Context, db *sql.DB) error {
	if db == nil {
		return nil
	}
	store := services.NewAutonomousRolloutStore(db)
	return store.InitSchema(ctx)
}
