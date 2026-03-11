package services

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	appautonomy "github.com/irfndi/neuratrade/internal/app/autonomy"
	"github.com/redis/go-redis/v9"
)

// QuestType defines the type of quest
type QuestType string

const (
	QuestTypeRoutine   QuestType = "routine"   // Time-triggered quests
	QuestTypeTriggered QuestType = "triggered" // Event-driven quests
	QuestTypeGoal      QuestType = "goal"      // Milestone-driven quests
	QuestTypeArbitrage QuestType = "arbitrage" // Arbitrage execution quests
)

const (
	defaultQuestSchedulerPoll = 5 * time.Second

	defaultQuestCadenceNormal   = time.Minute
	defaultQuestCadenceActive   = 25 * time.Second
	defaultQuestCadenceDegraded = 90 * time.Second
	defaultQuestCadenceIdle     = 120 * time.Second
	minQuestCadenceInterval     = 10 * time.Second

	defaultQuestExecutionStale = 3 * time.Minute
	minQuestExecutionStale     = time.Minute
	// Derived stale timeout buffers: per structured-retry repair budget and global latency cushion.
	questExecutionRepairAttemptBuffer = 20 * time.Second
	questExecutionGlobalWatchdogSlack = 45 * time.Second
	questExecutionContextTail         = 20 * time.Second
	questExecutionLockTail            = 35 * time.Second
	defaultQuestLockReleaseTimeout    = 2 * time.Second
	maxQuestLockReleaseTimeout        = 10 * time.Second
	defaultQuestExecutionHeartbeat    = 15 * time.Second
	questLockOwnerHeartbeatTTL        = 30 * time.Second
	defaultQuestStoreWriteTimeout     = 5 * time.Second

	questExecutionStageLock    = "lock"
	questExecutionStageHandler = "handler"
	questExecutionStagePersist = "persist"
	questExecutionStageDone    = "done"
)

var releaseQuestLockIfOwnedScript = redis.NewScript(`
if redis.call("GET", KEYS[1]) == ARGV[1] then
	return redis.call("DEL", KEYS[1])
end
return 0
`)

var replaceStaleQuestLockScript = redis.NewScript(`
if redis.call("GET", KEYS[1]) ~= ARGV[1] then
	return 0
end
if redis.call("EXISTS", KEYS[2]) ~= 0 then
	return 0
end
redis.call("SET", KEYS[1], ARGV[2], "PX", ARGV[3])
return 1
`)

type questRuntimeBudget struct {
	ScalpingTimeout   time.Duration
	StructuredRetries int
	DerivedFloor      time.Duration
	StaleTimeout      time.Duration
	ExecutionTimeout  time.Duration
	LockTTL           time.Duration
}

// QuestCadence defines the frequency of routine quests
type QuestCadence string

const (
	CadenceMicro   QuestCadence = "micro"   // Every 1-5 minutes
	CadenceHourly  QuestCadence = "hourly"  // Every hour
	CadenceDaily   QuestCadence = "daily"   // Once per day
	CadenceWeekly  QuestCadence = "weekly"  // Once per week
	CadenceOnetime QuestCadence = "onetime" // One-time quest
)

// QuestStatus defines the current state of a quest
type QuestStatus string

const (
	QuestStatusPending   QuestStatus = "pending"
	QuestStatusActive    QuestStatus = "active"
	QuestStatusCompleted QuestStatus = "completed"
	QuestStatusFailed    QuestStatus = "failed"
	QuestStatusPaused    QuestStatus = "paused"
)

// Quest represents a schedulable task in the autonomous trading system
type Quest struct {
	ID             string                 `json:"id"`
	Name           string                 `json:"name"`
	Description    string                 `json:"description"`
	Type           QuestType              `json:"type"`
	Cadence        QuestCadence           `json:"cadence"`
	CronExpr       string                 `json:"cron_expr,omitempty"` // Optional cron expression for custom schedules
	Status         QuestStatus            `json:"status"`
	Prompt         string                 `json:"prompt"`
	TargetCount    int                    `json:"target_count"`
	CurrentCount   int                    `json:"current_count"`
	Checkpoint     map[string]interface{} `json:"checkpoint"`
	CreatedAt      time.Time              `json:"created_at"`
	UpdatedAt      time.Time              `json:"updated_at"`
	LastExecutedAt *time.Time             `json:"last_executed_at,omitempty"` // Tracks last execution to prevent double-runs
	CompletedAt    *time.Time             `json:"completed_at,omitempty"`
	LastError      string                 `json:"last_error,omitempty"`
	Metadata       map[string]string      `json:"metadata,omitempty"`
}

// QuestProgress represents the progress of a quest for API responses
type QuestProgress struct {
	QuestID       string `json:"quest_id"`
	QuestName     string `json:"quest_name"`
	Current       int    `json:"current"`
	Target        int    `json:"target"`
	Percent       int    `json:"percent"`
	Status        string `json:"status"`
	TimeRemaining string `json:"time_remaining,omitempty"`
}

// AutonomousState tracks the autonomous mode state per user
type AutonomousState struct {
	ChatID       string    `json:"chat_id"`
	IsActive     bool      `json:"is_active"`
	StartedAt    time.Time `json:"started_at,omitempty"`
	PausedAt     time.Time `json:"paused_at,omitempty"`
	ActiveQuests []string  `json:"active_quests"`
}

// QuestDefinition defines a quest template
type QuestDefinition struct {
	ID          string
	Name        string
	Description string
	Type        QuestType
	Cadence     QuestCadence
	Prompt      string
	TargetCount int
	Handler     QuestHandler
}

// QuestHandler is the function that executes a quest
type QuestHandler func(ctx context.Context, quest *Quest) error

// QuestEngine manages quest scheduling and execution
type QuestEngine struct {
	mu                        sync.RWMutex
	quests                    map[string]*Quest
	executing                 map[string]bool
	executionStarts           map[string]time.Time
	executionLastProgress     map[string]time.Time
	executionStage            map[string]string
	executionLockHeld         map[string]bool
	executionLockTTL          map[string]time.Duration
	executionLockCheckedAt    map[string]time.Time
	executionStaleResetReason map[string]string
	executionStaleResetAt     map[string]time.Time
	autonomousState           map[string]*AutonomousState
	definitions               map[string]*QuestDefinition
	handlers                  map[QuestType]QuestHandler
	store                     QuestStore
	redis                     *redis.Client
	stopCh                    chan struct{}
	stopChClosed              bool
	runCancel                 context.CancelFunc
	running                   bool
	lockOwnerID               string
	cadenceMode               string
	lastTickAt                time.Time
	runtimeBudget             questRuntimeBudget
	riskLockActive            bool
	riskLockSource            string
	riskLockReasons           []string
	aiProviderChainConfigured int
	aiProviderChainUsable     int
	// notificationService is used to send quest progress notifications
	notificationService *NotificationService
	// chatIDForQuest maps quest IDs to their owner's chat ID
	chatIDForQuest map[string]int64
}

// QuestProgressNotifier defines the interface for sending quest progress notifications
type QuestProgressNotifier interface {
	NotifyQuestProgress(ctx context.Context, chatID int64, progress QuestProgressNotification) error
}

// QuestStore defines the interface for quest persistence
type QuestStore interface {
	SaveQuest(ctx context.Context, quest *Quest) error
	GetQuest(ctx context.Context, id string) (*Quest, error)
	ListQuests(ctx context.Context, chatID string, status QuestStatus) ([]*Quest, error)
	UpdateQuestProgress(ctx context.Context, id string, current int, checkpoint map[string]interface{}) error
	UpdateLastExecuted(ctx context.Context, id string, executedAt time.Time) error
	SaveAutonomousState(ctx context.Context, state *AutonomousState) error
	GetAutonomousState(ctx context.Context, chatID string) (*AutonomousState, error)
}

// InMemoryQuestStore is an in-memory implementation of QuestStore
type InMemoryQuestStore struct {
	mu              sync.RWMutex
	quests          map[string]*Quest
	autonomousState map[string]*AutonomousState
}

// NewInMemoryQuestStore creates a new in-memory quest store
func NewInMemoryQuestStore() *InMemoryQuestStore {
	return &InMemoryQuestStore{
		quests:          make(map[string]*Quest),
		autonomousState: make(map[string]*AutonomousState),
	}
}

// SaveQuest saves a quest to the store
func (s *InMemoryQuestStore) SaveQuest(ctx context.Context, quest *Quest) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.quests[quest.ID] = quest
	return nil
}

// GetQuest retrieves a quest by ID
func (s *InMemoryQuestStore) GetQuest(ctx context.Context, id string) (*Quest, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	quest, ok := s.quests[id]
	if !ok {
		return nil, fmt.Errorf("quest not found: %s", id)
	}
	return quest, nil
}

// ListQuests lists quests filtered by status
func (s *InMemoryQuestStore) ListQuests(ctx context.Context, chatID string, status QuestStatus) ([]*Quest, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var result []*Quest
	for _, q := range s.quests {
		if status == "" || q.Status == status {
			result = append(result, q)
		}
	}
	return result, nil
}

// UpdateQuestProgress updates quest progress
func (s *InMemoryQuestStore) UpdateQuestProgress(ctx context.Context, id string, current int, checkpoint map[string]interface{}) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	quest, ok := s.quests[id]
	if !ok {
		return fmt.Errorf("quest not found: %s", id)
	}
	quest.CurrentCount = current
	quest.Checkpoint = checkpoint
	quest.UpdatedAt = time.Now()
	return nil
}

// UpdateLastExecuted updates the last execution time for a quest
func (s *InMemoryQuestStore) UpdateLastExecuted(ctx context.Context, id string, executedAt time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	quest, ok := s.quests[id]
	if !ok {
		return fmt.Errorf("quest not found: %s", id)
	}
	quest.LastExecutedAt = &executedAt
	quest.UpdatedAt = time.Now()
	return nil
}

// SaveAutonomousState saves autonomous state
func (s *InMemoryQuestStore) SaveAutonomousState(ctx context.Context, state *AutonomousState) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.autonomousState[state.ChatID] = state
	return nil
}

// GetAutonomousState retrieves autonomous state
func (s *InMemoryQuestStore) GetAutonomousState(ctx context.Context, chatID string) (*AutonomousState, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	state, ok := s.autonomousState[chatID]
	if !ok {
		return &AutonomousState{ChatID: chatID, IsActive: false}, nil
	}
	return state, nil
}

// NewQuestEngine creates a new quest engine
func NewQuestEngine(store QuestStore) *QuestEngine {
	return NewQuestEngineWithRedis(store, nil)
}

// NewQuestEngineWithRedis creates a new quest engine with Redis for distributed coordination
func NewQuestEngineWithRedis(store QuestStore, redisClient *redis.Client) *QuestEngine {
	engine := &QuestEngine{
		quests:                    make(map[string]*Quest),
		executing:                 make(map[string]bool),
		executionStarts:           make(map[string]time.Time),
		executionLastProgress:     make(map[string]time.Time),
		executionStage:            make(map[string]string),
		executionLockHeld:         make(map[string]bool),
		executionLockTTL:          make(map[string]time.Duration),
		executionLockCheckedAt:    make(map[string]time.Time),
		executionStaleResetReason: make(map[string]string),
		executionStaleResetAt:     make(map[string]time.Time),
		autonomousState:           make(map[string]*AutonomousState),
		definitions:               make(map[string]*QuestDefinition),
		handlers:                  make(map[QuestType]QuestHandler),
		store:                     store,
		redis:                     redisClient,
		stopCh:                    make(chan struct{}),
		lockOwnerID:               uuid.NewString(),
		chatIDForQuest:            make(map[string]int64),
		cadenceMode:               "normal",
		riskLockSource:            "none",
		aiProviderChainConfigured: 0,
		aiProviderChainUsable:     0,
	}
	engine.runtimeBudget = computeQuestRuntimeBudget()

	engine.registerDefaultDefinitions()

	return engine
}

// NewQuestEngineWithNotification creates a new quest engine with notification support
func NewQuestEngineWithNotification(store QuestStore, redisClient *redis.Client, notifier *NotificationService) *QuestEngine {
	engine := NewQuestEngineWithRedis(store, redisClient)
	engine.notificationService = notifier
	return engine
}

// registerDefaultDefinitions registers the default quest templates
func (e *QuestEngine) registerDefaultDefinitions() {
	// Market scan quest - runs every 5 minutes
	e.RegisterDefinition(&QuestDefinition{
		ID:          "market_scan",
		Name:        "Market Scanner",
		Description: "Scan markets for arbitrage opportunities",
		Type:        QuestTypeRoutine,
		Cadence:     CadenceMicro,
		Prompt:      "Scan all configured exchanges for price discrepancies and arbitrage opportunities",
	})

	// Portfolio health check - runs hourly
	e.RegisterDefinition(&QuestDefinition{
		ID:          "portfolio_health",
		Name:        "Portfolio Health Check",
		Description: "Check portfolio balance and exposure",
		Type:        QuestTypeRoutine,
		Cadence:     CadenceHourly,
		Prompt:      "Verify portfolio balances, exposure limits, and position health",
	})

	// Daily PnL report
	e.RegisterDefinition(&QuestDefinition{
		ID:          "daily_report",
		Name:        "Daily Performance Report",
		Description: "Generate daily trading performance summary",
		Type:        QuestTypeRoutine,
		Cadence:     CadenceDaily,
		Prompt:      "Generate comprehensive daily report including PnL, win rate, and strategy performance",
	})

	// Funding rate check - runs every 5 minutes
	e.RegisterDefinition(&QuestDefinition{
		ID:          "funding_rate_scan",
		Name:        "Funding Rate Scanner",
		Description: "Scan for funding rate arbitrage opportunities",
		Type:        QuestTypeRoutine,
		Cadence:     CadenceMicro,
		Prompt:      "Check funding rates across futures exchanges for arbitrage opportunities",
	})

	// Volatility watch - triggered by market conditions
	e.RegisterDefinition(&QuestDefinition{
		ID:          "volatility_watch",
		Name:        "Volatility Watch",
		Description: "Monitor market volatility and trigger safe harbor if needed",
		Type:        QuestTypeTriggered,
		Cadence:     CadenceOnetime,
		Prompt:      "Monitor volatility levels and activate defensive measures when thresholds are exceeded",
	})

	// Scalping execution quest - runs every minute in scalping mode
	e.RegisterDefinition(&QuestDefinition{
		ID:          "scalping_execution",
		Name:        "Scalping Executor",
		Description: "Execute scalping trades based on skill parameters and market conditions",
		Type:        QuestTypeRoutine,
		Cadence:     CadenceMicro,
		Prompt:      "Scan for scalping opportunities using the scalping skill and execute trades when parameters are met",
	})

	// Fund growth milestone
	e.RegisterDefinition(&QuestDefinition{
		ID:          "fund_growth",
		Name:        "Fund Growth Target",
		Description: "Track progress toward fund growth milestone",
		Type:        QuestTypeGoal,
		Cadence:     CadenceOnetime,
		Prompt:      "Grow trading fund to target value using diversified strategies",
		TargetCount: 1000, // Default target, can be customized
	})
}

// RegisterDefinition registers a quest definition
func (e *QuestEngine) RegisterDefinition(def *QuestDefinition) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.definitions[def.ID] = def
}

// RegisterHandler registers a handler for a quest type
func (e *QuestEngine) RegisterHandler(qType QuestType, handler QuestHandler) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.handlers[qType] = handler
}

// CreateQuest creates a new quest from a definition
func (e *QuestEngine) CreateQuest(definitionID string, chatID string, customTarget ...float64) (*Quest, error) {
	e.mu.RLock()
	def, ok := e.definitions[definitionID]
	if !ok {
		e.mu.RUnlock()
		return nil, fmt.Errorf("quest definition not found: %s", definitionID)
	}
	e.mu.RUnlock()

	target := def.TargetCount
	if len(customTarget) > 0 {
		target = int(customTarget[0])
	}

	quest := &Quest{
		ID:           uuid.New().String(),
		Name:         def.Name,
		Description:  def.Description,
		Type:         def.Type,
		Cadence:      def.Cadence,
		Status:       QuestStatusPending,
		Prompt:       def.Prompt,
		TargetCount:  target,
		CurrentCount: 0,
		Checkpoint:   make(map[string]interface{}),
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
		Metadata: map[string]string{
			"chat_id":       chatID,
			"definition_id": definitionID,
		},
	}

	e.mu.Lock()
	e.quests[quest.ID] = quest
	chatIDInt, _ := strconv.ParseInt(chatID, 10, 64)
	e.chatIDForQuest[quest.ID] = chatIDInt
	e.mu.Unlock()

	if e.store != nil {
		if err := e.store.SaveQuest(context.Background(), quest); err != nil {
			log.Printf("Failed to persist quest %s: %v", quest.ID, err)
		}
	}

	return quest, nil
}

// Start begins the quest engine scheduler
func (e *QuestEngine) Start() {
	e.mu.Lock()
	if e.running {
		e.mu.Unlock()
		log.Println("[QUEST] Start called but already running")
		return
	}
	if e.runCancel != nil {
		e.runCancel()
		e.runCancel = nil
	}
	if e.stopCh != nil && !e.stopChClosed {
		close(e.stopCh)
		e.stopChClosed = true
	}
	e.stopCh = make(chan struct{})
	e.stopChClosed = false
	runStopCh := e.stopCh
	runCtx, runCancel := context.WithCancel(context.Background())
	e.runCancel = runCancel
	e.running = true
	e.runtimeBudget = computeQuestRuntimeBudget()
	e.mu.Unlock()

	// Load active quests from database
	e.loadActiveQuests()

	if e.redis != nil {
		go e.questLockOwnerHeartbeatLoop(runCtx)
	}

	go e.schedulerLoop(runStopCh)
	log.Printf("[QUEST] Quest engine started")
	log.Printf("[QUEST] Initial state: %d quests loaded, running=%v", len(e.quests), e.running)
	log.Printf(
		"[QUEST] Runtime budget: scalping_timeout=%s structured_retries=%d derived_floor=%s stale_timeout=%s execution_timeout=%s lock_ttl=%s",
		e.runtimeBudget.ScalpingTimeout,
		e.runtimeBudget.StructuredRetries,
		e.runtimeBudget.DerivedFloor,
		e.runtimeBudget.StaleTimeout,
		e.runtimeBudget.ExecutionTimeout,
		e.runtimeBudget.LockTTL,
	)
}

// loadActiveQuests loads active quests from the database into memory
func (e *QuestEngine) loadActiveQuests() {
	if e.store == nil {
		return
	}

	ctx := context.Background()
	quests, err := e.store.ListQuests(ctx, "", QuestStatusActive)
	if err != nil {
		log.Printf("Failed to load active quests: %v", err)
		return
	}

	selectedByChat := make(map[string]*Quest)
	pausedCount := 0
	now := time.Now()

	for _, quest := range quests {
		chatID := strings.TrimSpace(quest.Metadata["chat_id"])
		defID := strings.TrimSpace(quest.Metadata["definition_id"])

		// Scalping-first mode: only restore active scalping quests that have a valid chat owner.
		if chatID == "" || defID != "scalping_execution" {
			quest.Status = QuestStatusPaused
			quest.UpdatedAt = now
			pausedCount++
			if err := e.store.SaveQuest(ctx, quest); err != nil {
				log.Printf("Failed to pause legacy active quest %s: %v", quest.ID, err)
			}
			continue
		}

		// Keep only one active scalping quest per chat to prevent duplicate schedulers.
		if _, exists := selectedByChat[chatID]; exists {
			quest.Status = QuestStatusPaused
			quest.UpdatedAt = now
			pausedCount++
			if err := e.store.SaveQuest(ctx, quest); err != nil {
				log.Printf("Failed to pause duplicate active quest %s: %v", quest.ID, err)
			}
			continue
		}
		selectedByChat[chatID] = quest
	}

	e.mu.Lock()
	defer e.mu.Unlock()
	for _, quest := range selectedByChat {
		e.quests[quest.ID] = quest
		log.Printf("Loaded active scalping quest: %s (chat: %s)", quest.ID, quest.Metadata["chat_id"])
	}
	log.Printf("Loaded %d active scalping quests, paused %d stale active quests", len(selectedByChat), pausedCount)
}

// Stop stops the quest engine
func (e *QuestEngine) Stop() {
	e.mu.Lock()
	if !e.running {
		e.mu.Unlock()
		return
	}
	if e.runCancel != nil {
		e.runCancel()
		e.runCancel = nil
	}
	if e.stopCh != nil && !e.stopChClosed {
		close(e.stopCh)
		e.stopChClosed = true
	}
	e.running = false
	e.mu.Unlock()

	e.clearQuestLockOwnerHeartbeatIfIdle()
	log.Println("Quest engine stopped")
}

// schedulerLoop runs the periodic quest scheduling
func (e *QuestEngine) schedulerLoop(stopCh <-chan struct{}) {
	log.Println("[QUEST] Scheduler loop started")
	ticker := time.NewTicker(defaultQuestSchedulerPoll)
	defer ticker.Stop()
	log.Printf("[QUEST] Scheduler polling interval: %s", defaultQuestSchedulerPoll)

	// Run an immediate check so newly activated quests do not wait for the first interval.
	e.evaluateAndTick(time.Now().UTC(), true)

	for {
		select {
		case <-stopCh:
			log.Println("[QUEST] Scheduler loop stopped")
			return
		case <-ticker.C:
			e.evaluateAndTick(time.Now().UTC(), false)
		}
	}
}

func (e *QuestEngine) evaluateAndTick(now time.Time, force bool) {
	if !e.shouldRunTick(now, force) {
		return
	}
	e.tick()
}

func (e *QuestEngine) shouldRunTick(now time.Time, force bool) bool {
	e.mu.Lock()
	defer e.mu.Unlock()

	mode := e.determineCadenceModeLocked()
	interval := cadenceIntervalForMode(mode)
	if mode != e.cadenceMode {
		log.Printf("[QUEST] Cadence mode changed: %s -> %s (interval=%s)", e.cadenceMode, mode, interval)
		e.cadenceMode = mode
	}

	if force || e.lastTickAt.IsZero() || now.Sub(e.lastTickAt) >= interval {
		e.lastTickAt = now
		return true
	}
	return false
}

func (e *QuestEngine) determineCadenceModeLocked() string {
	if e.isRiskLockEnabledLocked() {
		return "risk_lock"
	}

	activeQuests := 0
	degraded := false
	driftActive := false
	for _, quest := range e.quests {
		if quest.Status != QuestStatusActive {
			continue
		}
		activeQuests++
		if readQuestMetricInt(quest.Checkpoint["runtime_failure_streak"]) > 0 {
			degraded = true
		}
		if readQuestMetricBool(quest.Checkpoint["state_drift_active"]) {
			driftActive = true
		}
	}

	if activeQuests == 0 {
		return "idle"
	}
	// During drift reconciliation keep cadence at 60s checks to speed gate clearing.
	if driftActive {
		return "normal"
	}
	if len(e.executing) > 0 {
		return "active_risk"
	}
	if degraded {
		return "degraded"
	}
	return "normal"
}

// tick processes scheduled quests
func (e *QuestEngine) tick() {
	now := time.Now()

	// First, cleanup old completed/failed quests (need write lock)
	e.mu.Lock()
	cleanupThreshold := 24 * time.Hour
	for id, quest := range e.quests {
		if quest.Status == QuestStatusCompleted || quest.Status == QuestStatusFailed {
			if quest.UpdatedAt.Before(now.Add(-cleanupThreshold)) {
				delete(e.quests, id)
				delete(e.executing, id)
				delete(e.executionStarts, id)
				delete(e.executionLastProgress, id)
				delete(e.executionStage, id)
				delete(e.executionLockHeld, id)
				delete(e.executionLockTTL, id)
				delete(e.executionLockCheckedAt, id)
				delete(e.executionStaleResetReason, id)
				delete(e.executionStaleResetAt, id)
				delete(e.chatIDForQuest, id)
				log.Printf("[QUEST] Cleaned up old quest: %s (status: %s)", id, quest.Status)
			}
		}
	}
	e.mu.Unlock()

	// Then, check quests for execution.
	e.mu.Lock()
	log.Printf("[QUEST] Tick: checking %d quests for execution", len(e.quests))
	activeCount := 0
	scheduledCount := 0
	for _, quest := range e.quests {
		if quest.Status != QuestStatusActive {
			log.Printf("[QUEST] Quest %s (%s) skipped - status: %s", quest.ID, quest.Name, quest.Status)
			continue
		}
		activeCount++
		if e.executing[quest.ID] {
			startedAt := e.executionStarts[quest.ID]
			if startedAt.IsZero() {
				startedAt = now
				e.executionStarts[quest.ID] = startedAt
			}
			lastProgressAt := e.executionLastProgress[quest.ID]
			if lastProgressAt.IsZero() {
				lastProgressAt = startedAt
				e.executionLastProgress[quest.ID] = lastProgressAt
			}
			stage := strings.TrimSpace(e.executionStage[quest.ID])
			if stage == "" {
				stage = questExecutionStageLock
				e.executionStage[quest.ID] = stage
			}

			age := now.Sub(startedAt)
			progressAge := now.Sub(lastProgressAt)
			staleAfter := e.runtimeBudget.StaleTimeout
			executionTimedOut := age > e.runtimeBudget.ExecutionTimeout
			if progressAge > staleAfter || executionTimedOut {
				lockKey := fmt.Sprintf("quest:lock:%s", quest.ID)
				lockHeld := false
				lockTTL := time.Duration(0)
				lockCheckErr := error(nil)
				if e.redis != nil {
					lockCheckCtx, cancelLockCheck := context.WithTimeout(context.Background(), 750*time.Millisecond)
					lockHeld, lockTTL, lockCheckErr = e.readLockState(lockCheckCtx, lockKey)
					cancelLockCheck()
				}
				if lockCheckErr != nil {
					delete(e.executing, quest.ID)
					delete(e.executionStarts, quest.ID)
					delete(e.executionLastProgress, quest.ID)
					delete(e.executionStage, quest.ID)
					e.executionLockHeld[quest.ID] = false
					e.executionLockTTL[quest.ID] = 0
					e.executionLockCheckedAt[quest.ID] = now.UTC()
					e.executionStaleResetReason[quest.ID] = fmt.Sprintf("stale_reset_lock_check_failed:%v", lockCheckErr)
					e.executionStaleResetAt[quest.ID] = now.UTC()
					log.Printf(
						"[QUEST] Quest %s (%s) stale reset after lock check failure (%v)",
						quest.ID,
						quest.Name,
						lockCheckErr,
					)
					continue
				}
				e.executionLockHeld[quest.ID] = lockHeld
				e.executionLockTTL[quest.ID] = lockTTL
				e.executionLockCheckedAt[quest.ID] = now.UTC()
				if lockHeld {
					e.executionStaleResetReason[quest.ID] = fmt.Sprintf("stale_detected_but_lock_active(ttl=%s)", lockTTL.Round(time.Second))
					e.executionStaleResetAt[quest.ID] = now.UTC()
					log.Printf(
						"[QUEST] Quest %s (%s) stale marker retained because lock is still active (ttl=%s progress_age=%s start_age=%s stage=%s)",
						quest.ID,
						quest.Name,
						lockTTL.Round(time.Second),
						progressAge.Round(time.Second),
						age.Round(time.Second),
						stage,
					)
					continue
				}

				log.Printf(
					"[QUEST] Quest %s (%s) execution stale after progress_age=%s (start_age=%s stage=%s reset_after=%s), resetting in-progress marker",
					quest.ID,
					quest.Name,
					progressAge.Round(time.Second),
					age.Round(time.Second),
					stage,
					staleAfter.Round(time.Second),
				)
				delete(e.executing, quest.ID)
				delete(e.executionStarts, quest.ID)
				delete(e.executionLastProgress, quest.ID)
				delete(e.executionStage, quest.ID)
				e.executionStaleResetReason[quest.ID] = fmt.Sprintf("stale_reset_progress_age=%s", progressAge.Round(time.Second))
				e.executionStaleResetAt[quest.ID] = now.UTC()
			} else {
				log.Printf(
					"[QUEST] Quest %s (%s) skipped - execution already in progress for %s (stage=%s last_progress=%s ago)",
					quest.ID,
					quest.Name,
					age.Round(time.Second),
					stage,
					progressAge.Round(time.Second),
				)
				continue
			}
		}

		riskBlocked := e.shouldBlockQuestEntryByRiskLockLocked(quest)
		driftBlocked := e.shouldBlockQuestEntryByStateDriftLocked(quest)
		if riskBlocked {
			if quest.Checkpoint == nil {
				quest.Checkpoint = make(map[string]interface{})
			}
			quest.Checkpoint["runtime_entry_blocked_by_risk_lock"] = true
			quest.Checkpoint["runtime_entry_blocked_at"] = now.UTC().Format(time.RFC3339)
			quest.Checkpoint["runtime_entry_gate_reason"] = "risk lock active: drawdown/exposure guardrail blocking new entries"
			log.Printf("[QUEST] Entry decisions for quest %s are gated by risk lock", quest.ID)
		} else if quest.Checkpoint != nil {
			delete(quest.Checkpoint, "runtime_entry_blocked_by_risk_lock")
		}

		if driftBlocked {
			if quest.Checkpoint == nil {
				quest.Checkpoint = make(map[string]interface{})
			}
			quest.Checkpoint["runtime_entry_blocked_by_state_drift"] = true
			if _, exists := quest.Checkpoint["runtime_entry_blocked_at"]; !exists {
				quest.Checkpoint["runtime_entry_blocked_at"] = now.UTC().Format(time.RFC3339)
			}
			if strings.TrimSpace(readQuestMetricString(quest.Checkpoint["runtime_entry_gate_reason"])) == "" {
				quest.Checkpoint["runtime_entry_gate_reason"] = "state drift detected: reconcile/repair pending"
			}
			log.Printf("[QUEST] Entry decisions for quest %s are gated by state drift", quest.ID)
		} else if quest.Checkpoint != nil {
			delete(quest.Checkpoint, "runtime_entry_blocked_by_state_drift")
			if !readQuestMetricBool(quest.Checkpoint["runtime_entry_blocked_by_risk_lock"]) {
				delete(quest.Checkpoint, "runtime_entry_blocked_at")
				delete(quest.Checkpoint, "runtime_entry_gate_reason")
			}
		}

		// Check if quest should execute based on cadence
		if e.shouldExecute(quest, now) {
			log.Printf("[QUEST] Executing quest: %s (type: %s, def: %s, chat: %s)", quest.ID, quest.Type, quest.Metadata["definition_id"], quest.Metadata["chat_id"])
			e.executing[quest.ID] = true
			e.executionStarts[quest.ID] = now
			e.executionLastProgress[quest.ID] = now
			e.executionStage[quest.ID] = questExecutionStageLock
			scheduledCount++
			go e.executeQuest(quest)
		} else {
			log.Printf("[QUEST] Quest %s not ready (cadence: %s, last: %v)", quest.ID, quest.Cadence, quest.LastExecutedAt)
		}
	}
	log.Printf("[QUEST] Tick complete: %d active quests, %d sent for execution", activeCount, scheduledCount)
	e.mu.Unlock()
}

func (e *QuestEngine) shouldExecute(quest *Quest, now time.Time) bool {
	mode := strings.TrimSpace(e.cadenceMode)
	if mode == "" {
		mode = "normal"
	}
	minInterval := cadenceIntervalForMode(mode)
	activeGoalOnetime := quest.Cadence == CadenceOnetime && quest.Type == QuestTypeGoal && quest.Status == QuestStatusActive

	if !activeGoalOnetime && quest.LastExecutedAt != nil && now.Sub(*quest.LastExecutedAt) < minInterval {
		return false
	}

	switch quest.Cadence {
	case CadenceMicro:
		if quest.LastExecutedAt != nil {
			return now.Sub(*quest.LastExecutedAt) >= minInterval
		}
		return true
	case CadenceHourly:
		if quest.LastExecutedAt != nil {
			return now.Sub(*quest.LastExecutedAt) >= 1*time.Hour
		}
		return true
	case CadenceDaily:
		if quest.LastExecutedAt != nil {
			return now.Sub(*quest.LastExecutedAt) >= 24*time.Hour
		}
		return true
	case CadenceWeekly:
		if quest.LastExecutedAt != nil {
			return now.Sub(*quest.LastExecutedAt) >= 7*24*time.Hour
		}
		return true
	case CadenceOnetime:
		if quest.Type == QuestTypeGoal && quest.Status == QuestStatusActive {
			return !questGoalReached(quest)
		}
		return quest.LastExecutedAt == nil
	default:
		return false
	}
}

func cadenceIntervalForMode(mode string) time.Duration {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "active_risk", "risk_lock":
		return cadenceFromEnv("NEURATRADE_QUEST_ACTIVE_CADENCE_SECONDS", defaultQuestCadenceActive)
	case "degraded":
		return cadenceFromEnv("NEURATRADE_QUEST_DEGRADED_CADENCE_SECONDS", defaultQuestCadenceDegraded)
	case "idle":
		return cadenceFromEnv("NEURATRADE_QUEST_IDLE_CADENCE_SECONDS", defaultQuestCadenceIdle)
	default:
		return cadenceFromEnv("NEURATRADE_MICRO_CADENCE_SECONDS", defaultQuestCadenceNormal)
	}
}

func cadenceFromEnv(envKey string, fallback time.Duration) time.Duration {
	raw := strings.TrimSpace(os.Getenv(envKey))
	if raw == "" {
		return fallback
	}
	seconds, err := strconv.Atoi(raw)
	if err != nil || seconds <= 0 {
		log.Printf("[QUEST] Invalid %s=%q, using default %s", envKey, raw, fallback)
		return fallback
	}
	interval := time.Duration(seconds) * time.Second
	if interval < minQuestCadenceInterval {
		log.Printf("[QUEST] %s too low (%ds), clamping to %s", envKey, seconds, minQuestCadenceInterval)
		return minQuestCadenceInterval
	}
	return interval
}

func computeQuestRuntimeBudget() questRuntimeBudget {
	budget := questRuntimeBudget{
		ScalpingTimeout:   90 * time.Second,
		StructuredRetries: 2,
	}

	if timeoutRaw := strings.TrimSpace(os.Getenv("NEURATRADE_SCALPING_TIMEOUT_SECONDS")); timeoutRaw != "" {
		if timeoutSec, err := strconv.Atoi(timeoutRaw); err == nil && timeoutSec > 0 {
			budget.ScalpingTimeout = time.Duration(timeoutSec) * time.Second
		}
	}
	if retriesRaw := strings.TrimSpace(os.Getenv("NEURATRADE_SCALPING_STRUCTURED_RETRIES")); retriesRaw != "" {
		if retries, err := strconv.Atoi(retriesRaw); err == nil && retries > 0 {
			budget.StructuredRetries = retries
		}
	}

	budget.DerivedFloor = budget.ScalpingTimeout +
		time.Duration(budget.StructuredRetries+1)*questExecutionRepairAttemptBuffer +
		questExecutionGlobalWatchdogSlack
	if budget.DerivedFloor < defaultQuestExecutionStale {
		budget.DerivedFloor = defaultQuestExecutionStale
	}
	if budget.DerivedFloor < minQuestExecutionStale {
		budget.DerivedFloor = minQuestExecutionStale
	}

	budget.StaleTimeout = budget.DerivedFloor
	if raw := strings.TrimSpace(os.Getenv("NEURATRADE_QUEST_EXECUTION_STALE_SECONDS")); raw != "" {
		if seconds, err := strconv.Atoi(raw); err == nil && seconds > 0 {
			candidate := time.Duration(seconds) * time.Second
			if candidate < budget.DerivedFloor {
				budget.StaleTimeout = budget.DerivedFloor
			} else {
				budget.StaleTimeout = candidate
			}
		}
	}
	if budget.StaleTimeout < minQuestExecutionStale {
		budget.StaleTimeout = minQuestExecutionStale
	}

	budget.ExecutionTimeout = budget.StaleTimeout + questExecutionContextTail
	budget.LockTTL = budget.ExecutionTimeout + questExecutionLockTail
	return budget
}

//nolint:unused // Helper for future risk gate expansion
func (e *QuestEngine) shouldBlockQuestEntryLocked(quest *Quest) bool {
	return e.shouldBlockQuestEntryByRiskLockLocked(quest) || e.shouldBlockQuestEntryByStateDriftLocked(quest)
}

func (e *QuestEngine) shouldBlockQuestEntryByRiskLockLocked(quest *Quest) bool {
	if quest == nil || !e.isRiskLockEnabledLocked() {
		return false
	}
	definitionID := strings.TrimSpace(quest.Metadata["definition_id"])
	// Entry gate: block new scalping entries while risk-lock is active.
	return definitionID == "scalping_execution"
}

func (e *QuestEngine) shouldBlockQuestEntryByStateDriftLocked(quest *Quest) bool {
	if quest == nil {
		return false
	}
	definitionID := strings.TrimSpace(quest.Metadata["definition_id"])
	if definitionID != "scalping_execution" {
		return false
	}
	if quest.Checkpoint == nil {
		return false
	}
	return readQuestMetricBool(quest.Checkpoint["state_drift_active"])
}

func (e *QuestEngine) isRiskLockEnabledLocked() bool {
	if e.riskLockActive {
		return true
	}
	return envEnabled("NEURATRADE_QUEST_FORCE_RISK_LOCK")
}

func (e *QuestEngine) currentRiskLockSourceLocked() string {
	if envEnabled("NEURATRADE_QUEST_FORCE_RISK_LOCK") {
		return "manual_env"
	}
	if !e.riskLockActive {
		return "none"
	}
	source := normalizeRiskLockSource(e.riskLockSource)
	if source == "none" {
		source = inferRiskLockSource(e.riskLockReasons)
	}
	return source
}

func envEnabled(key string) bool {
	raw := strings.ToLower(strings.TrimSpace(os.Getenv(key)))
	return raw == "1" || raw == "true" || raw == "yes" || raw == "on"
}

func readQuestMetricInt(v interface{}) int {
	switch n := v.(type) {
	case int:
		return n
	case int32:
		return int(n)
	case int64:
		return int(n)
	case float32:
		return int(n)
	case float64:
		return int(n)
	case string:
		if parsed, err := strconv.Atoi(strings.TrimSpace(n)); err == nil {
			return parsed
		}
	}
	return 0
}

func readQuestMetricIntWithFallback(checkpoint map[string]interface{}, primary, fallback string) int {
	if checkpoint == nil {
		return 0
	}
	if value, ok := checkpoint[primary]; ok {
		return readQuestMetricInt(value)
	}
	return readQuestMetricInt(checkpoint[fallback])
}

func readQuestMetricBool(v interface{}) bool {
	switch value := v.(type) {
	case bool:
		return value
	case string:
		normalized := strings.ToLower(strings.TrimSpace(value))
		return normalized == "1" || normalized == "true" || normalized == "yes" || normalized == "on"
	case int:
		return value != 0
	case int64:
		return value != 0
	case float64:
		return value != 0
	default:
		return false
	}
}

func readQuestMetricString(v interface{}) string {
	switch value := v.(type) {
	case string:
		return strings.TrimSpace(value)
	case fmt.Stringer:
		return strings.TrimSpace(value.String())
	default:
		return ""
	}
}

func readQuestMetricFloat(v interface{}) float64 {
	switch value := v.(type) {
	case float64:
		return value
	case float32:
		return float64(value)
	case int:
		return float64(value)
	case int64:
		return float64(value)
	case string:
		parsed, err := strconv.ParseFloat(strings.TrimSpace(value), 64)
		if err == nil {
			return parsed
		}
	}
	return 0
}

func readCandidateRejections(v interface{}) []map[string]interface{} {
	switch value := v.(type) {
	case []map[string]interface{}:
		return append([]map[string]interface{}(nil), value...)
	case []interface{}:
		converted := make([]map[string]interface{}, 0, len(value))
		for _, item := range value {
			entry, ok := item.(map[string]interface{})
			if !ok || len(entry) == 0 {
				continue
			}
			converted = append(converted, entry)
		}
		return converted
	default:
		return nil
	}
}

func envFloatOrDefault(key string, fallback, min, max float64) float64 {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback
	}
	value, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return fallback
	}
	if value < min {
		return min
	}
	if value > max {
		return max
	}
	return value
}

func questLockReleaseTimeout() time.Duration {
	seconds := getEnvInt("NEURATRADE_QUEST_LOCK_RELEASE_TIMEOUT_SECONDS")
	if seconds <= 0 {
		return defaultQuestLockReleaseTimeout
	}
	timeout := time.Duration(seconds) * time.Second
	if timeout > maxQuestLockReleaseTimeout {
		return maxQuestLockReleaseTimeout
	}
	return timeout
}

func questExecutionHeartbeatInterval() time.Duration {
	seconds := getEnvInt("NEURATRADE_QUEST_EXECUTION_HEARTBEAT_SECONDS")
	if seconds <= 0 {
		return defaultQuestExecutionHeartbeat
	}
	interval := time.Duration(seconds) * time.Second
	if interval < 5*time.Second {
		return 5 * time.Second
	}
	if interval > time.Minute {
		return time.Minute
	}
	return interval
}

func questLockOwnerHeartbeatKey(ownerID string) string {
	return fmt.Sprintf("quest:lock-owner:%s", strings.TrimSpace(ownerID))
}

func (e *QuestEngine) questLockOwnerHeartbeatLoop(runCtx context.Context) {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for {
		if e.refreshQuestLockOwnerHeartbeatCycle(runCtx) {
			return
		}
		select {
		case <-runCtx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (e *QuestEngine) refreshQuestLockOwnerHeartbeatCycle(runCtx context.Context) bool {
	select {
	case <-runCtx.Done():
		return true
	default:
	}

	e.mu.RLock()
	redisClient := e.redis
	ownerID := strings.TrimSpace(e.lockOwnerID)
	running := e.running
	executing := len(e.executing)
	e.mu.RUnlock()

	if redisClient == nil || ownerID == "" {
		return true
	}

	ctx, cancel := context.WithTimeout(runCtx, time.Second)
	defer cancel()
	if ctx.Err() != nil {
		return true
	}

	if !running && executing == 0 {
		if err := redisClient.Del(ctx, questLockOwnerHeartbeatKey(ownerID)).Err(); err != nil {
			log.Printf("[QUEST] Failed to clear quest lock owner heartbeat: %v", err)
		}
		return true
	}

	if err := redisClient.Set(ctx, questLockOwnerHeartbeatKey(ownerID), time.Now().UTC().Format(time.RFC3339), questLockOwnerHeartbeatTTL).Err(); err != nil {
		log.Printf("[QUEST] Failed to refresh quest lock owner heartbeat: %v", err)
	}

	return false
}

func (e *QuestEngine) clearQuestLockOwnerHeartbeatIfIdle() {
	e.mu.RLock()
	redisClient := e.redis
	ownerID := strings.TrimSpace(e.lockOwnerID)
	running := e.running
	executing := len(e.executing)
	e.mu.RUnlock()

	if redisClient == nil || ownerID == "" || running || executing > 0 {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := redisClient.Del(ctx, questLockOwnerHeartbeatKey(ownerID)).Err(); err != nil {
		log.Printf("[QUEST] Failed to clear quest lock owner heartbeat: %v", err)
	}
}

func (e *QuestEngine) refreshQuestLockOwnerHeartbeat(ctx context.Context) error {
	if e.redis == nil {
		return nil
	}
	ownerID := strings.TrimSpace(e.lockOwnerID)
	if ownerID == "" {
		return nil
	}
	return e.redis.Set(ctx, questLockOwnerHeartbeatKey(ownerID), time.Now().UTC().Format(time.RFC3339), questLockOwnerHeartbeatTTL).Err()
}

func (e *QuestEngine) startExecutionHeartbeat(ctx context.Context, questID string) func() {
	interval := questExecutionHeartbeatInterval()
	done := make(chan struct{})
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-done:
				return
			case <-ticker.C:
				e.markQuestExecutionProgress(questID, questExecutionStageHandler)
			}
		}
	}()

	return func() {
		close(done)
	}
}

func (e *QuestEngine) recordExecutionLockState(questID string, held bool, ttl time.Duration, checkedAt time.Time) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.executionLockHeld[questID] = held
	e.executionLockTTL[questID] = ttl
	e.executionLockCheckedAt[questID] = checkedAt.UTC()
}

func (e *QuestEngine) readLockState(ctx context.Context, key string) (bool, time.Duration, error) {
	if e.redis == nil {
		return false, 0, nil
	}
	exists, err := e.redis.Exists(ctx, key).Result()
	if err != nil {
		return false, 0, err
	}
	if exists == 0 {
		return false, 0, nil
	}
	ttl, err := e.redis.TTL(ctx, key).Result()
	if err != nil {
		return false, 0, err
	}
	switch ttl {
	case time.Duration(-2):
		return false, 0, nil
	case time.Duration(-1):
		return true, 0, nil
	default:
		if ttl <= 0 {
			return true, 0, nil
		}
		return true, ttl, nil
	}
}

func (e *QuestEngine) reclaimStaleLock(ctx context.Context, key string, ttl time.Duration) (bool, error) {
	if e.redis == nil {
		return false, nil
	}

	currentOwner, err := e.redis.Get(ctx, key).Result()
	switch {
	case err == redis.Nil:
		return e.redis.SetNX(ctx, key, e.lockOwnerID, ttl).Result()
	case err != nil:
		return false, err
	}

	currentOwner = strings.TrimSpace(currentOwner)
	if currentOwner == "" || currentOwner == strings.TrimSpace(e.lockOwnerID) {
		return false, nil
	}

	replaced, err := replaceStaleQuestLockScript.Run(
		ctx,
		e.redis,
		[]string{key, questLockOwnerHeartbeatKey(currentOwner)},
		currentOwner,
		e.lockOwnerID,
		strconv.FormatInt(ttl.Milliseconds(), 10),
	).Int()
	if err != nil {
		return false, err
	}
	if replaced == 1 {
		log.Printf("[QUEST] Reclaimed stale quest lock %s from inactive owner %s", key, currentOwner)
		return true, nil
	}
	return false, nil
}

// executeQuest executes a single quest
func (e *QuestEngine) executeQuest(quest *Quest) {
	defer e.markQuestExecutionFinished(quest.ID)
	e.markQuestExecutionProgress(quest.ID, questExecutionStageLock)

	e.mu.RLock()
	handler, ok := e.handlers[quest.Type]
	e.mu.RUnlock()

	if !ok {
		log.Printf("No handler registered for quest type: %s", quest.Type)
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), e.runtimeBudget.ExecutionTimeout)
	defer cancel()

	lockKey := fmt.Sprintf("quest:lock:%s", quest.ID)
	locked := e.acquireLock(ctx, lockKey, e.runtimeBudget.LockTTL)
	if !locked {
		e.mu.Lock()
		e.executionLockHeld[quest.ID] = true
		e.executionLockTTL[quest.ID] = e.runtimeBudget.LockTTL
		e.executionLockCheckedAt[quest.ID] = time.Now().UTC()
		e.executionStaleResetReason[quest.ID] = "lock_acquire_failed_already_owned"
		e.executionStaleResetAt[quest.ID] = time.Now().UTC()
		e.mu.Unlock()
		log.Printf("Quest %s skipped: could not acquire lock (another instance may be running)", quest.ID)
		return
	}
	e.recordExecutionLockState(quest.ID, true, e.runtimeBudget.LockTTL, time.Now().UTC())
	defer func() {
		e.markQuestExecutionProgress(quest.ID, questExecutionStageDone)
		releaseCtx, cancelRelease := context.WithTimeout(context.Background(), questLockReleaseTimeout())
		defer cancelRelease()
		e.releaseLock(releaseCtx, lockKey)
		e.recordExecutionLockState(quest.ID, false, 0, time.Now().UTC())
	}()

	e.markQuestExecutionProgress(quest.ID, questExecutionStageHandler)
	stopHeartbeat := e.startExecutionHeartbeat(ctx, quest.ID)
	defer stopHeartbeat()
	if err := handler(ctx, quest); err != nil {
		log.Printf("Quest %s (%s) failed: %v", quest.ID, quest.Name, err)
		e.markQuestExecutionProgress(quest.ID, questExecutionStagePersist)
		e.finalizeQuestExecution(quest, err)
	} else {
		log.Printf("Quest %s (%s) completed successfully", quest.ID, quest.Name)
		e.markQuestExecutionProgress(quest.ID, questExecutionStagePersist)
		e.finalizeQuestExecution(quest, nil)
	}
}

func (e *QuestEngine) finalizeQuestExecution(quest *Quest, execErr error) {
	if quest == nil {
		return
	}

	now := time.Now()
	var snapshot *Quest

	e.mu.Lock()
	if quest.Checkpoint == nil {
		quest.Checkpoint = make(map[string]interface{})
	}
	quest.UpdatedAt = now
	if execErr != nil {
		quest.Status = QuestStatusFailed
		quest.LastError = execErr.Error()
		quest.CompletedAt = nil
	} else {
		quest.LastExecutedAt = &now
		quest.LastError = ""
		if quest.Type == QuestTypeRoutine || (quest.Type == QuestTypeGoal && !questGoalReached(quest)) {
			quest.Status = QuestStatusActive
			quest.CompletedAt = nil
		} else {
			quest.Status = QuestStatusCompleted
			quest.CompletedAt = &now
		}
	}
	e.quests[quest.ID] = quest
	if chatID, err := strconv.ParseInt(strings.TrimSpace(quest.Metadata["chat_id"]), 10, 64); err == nil && chatID > 0 {
		e.chatIDForQuest[quest.ID] = chatID
	}
	snapshot = cloneQuestForPersistence(quest)
	e.mu.Unlock()

	if e.store != nil && snapshot != nil {
		saveCtx, cancel := context.WithTimeout(context.Background(), defaultQuestStoreWriteTimeout)
		defer cancel()
		if err := e.store.SaveQuest(saveCtx, snapshot); err != nil {
			log.Printf("Failed to persist final quest snapshot %s: %v", quest.ID, err)
		}
	}
}

func (e *QuestEngine) markQuestExecutionFinished(questID string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	delete(e.executing, questID)
	delete(e.executionStarts, questID)
	delete(e.executionLastProgress, questID)
	delete(e.executionStage, questID)
	if !e.running && len(e.executing) == 0 {
		go e.clearQuestLockOwnerHeartbeatIfIdle()
	}
}

func (e *QuestEngine) markQuestExecutionProgress(questID, stage string) {
	now := time.Now()
	e.mu.Lock()
	defer e.mu.Unlock()
	if !e.executing[questID] {
		return
	}
	if _, exists := e.executionStarts[questID]; !exists {
		e.executionStarts[questID] = now
	}
	e.executionLastProgress[questID] = now
	if strings.TrimSpace(stage) == "" {
		stage = questExecutionStageLock
	}
	e.executionStage[questID] = stage
}

func (e *QuestEngine) acquireLock(ctx context.Context, key string, ttl time.Duration) bool {
	if e.redis == nil {
		return true
	}
	if err := e.refreshQuestLockOwnerHeartbeat(ctx); err != nil {
		log.Printf("Failed to refresh quest lock owner heartbeat: %v", err)
		return false
	}
	ok, err := e.redis.SetNX(ctx, key, e.lockOwnerID, ttl).Result()
	if err != nil {
		log.Printf("Failed to acquire lock %s: %v", key, err)
		return false
	}
	if ok {
		return true
	}
	reclaimed, err := e.reclaimStaleLock(ctx, key, ttl)
	if err != nil {
		log.Printf("Failed to reclaim stale lock %s: %v", key, err)
		return false
	}
	if reclaimed {
		return true
	}
	return ok
}

func questGoalReached(quest *Quest) bool {
	if quest == nil {
		return false
	}
	if quest.TargetCount > 0 && quest.CurrentCount >= quest.TargetCount {
		return true
	}
	if quest.Checkpoint != nil && readQuestMetricBool(quest.Checkpoint["goal_reached"]) {
		return true
	}
	return false
}

func (e *QuestEngine) releaseLock(ctx context.Context, key string) {
	if e.redis == nil {
		return
	}
	if _, err := releaseQuestLockIfOwnedScript.Run(ctx, e.redis, []string{key}, e.lockOwnerID).Int(); err != nil && err != redis.Nil {
		log.Printf("Failed to release lock %s: %v", key, err)
	}
}

// BeginAutonomous starts autonomous mode for a user
func (e *QuestEngine) BeginAutonomous(chatID string) (*AutonomousState, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	// Pause existing active quests for this chat, and any unowned legacy active quests.
	for _, q := range e.quests {
		questChatID := strings.TrimSpace(q.Metadata["chat_id"])
		if q.Status == QuestStatusActive && (questChatID == chatID || questChatID == "") {
			q.Status = QuestStatusPaused
			q.UpdatedAt = time.Now()
			if e.store != nil {
				if err := e.store.SaveQuest(context.Background(), q); err != nil {
					log.Printf("Failed to persist paused quest %s: %v", q.ID, err)
				}
			}
		}
	}

	state := &AutonomousState{
		ChatID:    chatID,
		IsActive:  true,
		StartedAt: time.Now(),
	}

	// Create default quests for autonomous mode.
	// Current operating mode is scalping-first, so only enable scalping execution.
	defaultQuests := []string{"scalping_execution"}
	for _, defID := range defaultQuests {
		quest, err := e.ensureQuestForChatInternal(defID, chatID)
		if err != nil {
			log.Printf("Failed to create quest %s: %v", defID, err)
			continue
		}
		// Set dry-run mode from config (default to false for live trading)
		if quest.Metadata == nil {
			quest.Metadata = make(map[string]string)
		}
		quest.Metadata["dry_run"] = "false"
		quest.Metadata["paper_trading"] = "false"
		log.Printf("[QUEST] Created quest %s with dry_run=false (LIVE TRADING MODE)", quest.ID)

		quest.Status = QuestStatusActive
		quest.UpdatedAt = time.Now()
		if e.store != nil {
			if err := e.store.SaveQuest(context.Background(), quest); err != nil {
				log.Printf("Failed to persist active quest %s: %v", quest.ID, err)
			}
		}
		state.ActiveQuests = append(state.ActiveQuests, quest.ID)
	}

	e.autonomousState[chatID] = state

	if e.store != nil {
		if err := e.store.SaveAutonomousState(context.Background(), state); err != nil {
			log.Printf("Failed to persist autonomous state: %v", err)
		}
	}

	return state, nil
}

func (e *QuestEngine) ensureQuestForChatInternal(definitionID, chatID string) (*Quest, error) {
	for _, quest := range e.quests {
		if quest == nil {
			continue
		}
		if strings.TrimSpace(quest.Metadata["chat_id"]) != strings.TrimSpace(chatID) {
			continue
		}
		if strings.TrimSpace(quest.Metadata["definition_id"]) != strings.TrimSpace(definitionID) {
			continue
		}
		quest.UpdatedAt = time.Now()
		if quest.Checkpoint == nil {
			quest.Checkpoint = make(map[string]interface{})
		}
		return quest, nil
	}
	return e.createQuestInternal(definitionID, chatID)
}

// PauseAutonomous pauses autonomous mode for a user
func (e *QuestEngine) PauseAutonomous(chatID string) (*AutonomousState, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	state, ok := e.autonomousState[chatID]
	if !ok {
		state = &AutonomousState{ChatID: chatID, IsActive: false}
	} else {
		state.IsActive = false
		state.PausedAt = time.Now()

		// Pause all active quests
		for _, questID := range state.ActiveQuests {
			if quest, ok := e.quests[questID]; ok {
				quest.Status = QuestStatusPaused
				quest.UpdatedAt = time.Now()
			}
		}
		state.ActiveQuests = nil
	}

	e.autonomousState[chatID] = state

	if e.store != nil {
		if err := e.store.SaveAutonomousState(context.Background(), state); err != nil {
			log.Printf("Failed to persist autonomous state: %v", err)
		}
	}

	return state, nil
}

// GetAutonomousState retrieves the autonomous state for a user
func (e *QuestEngine) GetAutonomousState(chatID string) (*AutonomousState, error) {
	e.mu.RLock()
	defer e.mu.RUnlock()

	state, ok := e.autonomousState[chatID]
	if !ok {
		return &AutonomousState{ChatID: chatID, IsActive: false}, nil
	}
	return state, nil
}

// SetRiskLockState sets global risk-lock state for quest entry gating.
func (e *QuestEngine) SetRiskLockState(active bool, reasons []string) {
	e.SetRiskLockStateWithSource(active, inferRiskLockSource(reasons), reasons)
}

// SetRiskLockStateWithSource sets global risk-lock state with an explicit source tag.
func (e *QuestEngine) SetRiskLockStateWithSource(active bool, source string, reasons []string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.riskLockActive = active
	if !active {
		e.riskLockSource = "none"
		e.riskLockReasons = nil
		return
	}
	e.riskLockSource = normalizeRiskLockSource(source)
	if len(reasons) == 0 {
		e.riskLockReasons = nil
		return
	}
	copied := make([]string, 0, len(reasons))
	for _, reason := range reasons {
		reason = strings.TrimSpace(reason)
		if reason != "" {
			copied = append(copied, reason)
		}
	}
	e.riskLockReasons = copied
}

func normalizeRiskLockSource(source string) string {
	switch strings.TrimSpace(source) {
	case "manual_env":
		return "manual_env"
	case "portfolio_safety":
		return "portfolio_safety"
	case "drawdown_threshold":
		return "drawdown_threshold"
	default:
		return "none"
	}
}

func inferRiskLockSource(reasons []string) string {
	for _, reason := range reasons {
		normalized := strings.ToLower(strings.TrimSpace(reason))
		switch {
		case strings.HasPrefix(normalized, "manual_env:"):
			return "manual_env"
		case strings.HasPrefix(normalized, "portfolio_safety:"):
			return "portfolio_safety"
		case strings.HasPrefix(normalized, "drawdown_threshold:"):
			return "drawdown_threshold"
		}
	}
	return "none"
}

// SetAIProviderChainStats records provider-chain readiness for diagnostics.
func (e *QuestEngine) SetAIProviderChainStats(configured, usable int) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if configured < 0 {
		configured = 0
	}
	if usable < 0 {
		usable = 0
	}
	e.aiProviderChainConfigured = configured
	e.aiProviderChainUsable = usable
}

// ListActiveAutonomousChatIDs returns active autonomous chat IDs known by the runtime.
func (e *QuestEngine) ListActiveAutonomousChatIDs() []string {
	e.mu.RLock()
	defer e.mu.RUnlock()
	ids := make([]string, 0, len(e.autonomousState))
	for chatID, state := range e.autonomousState {
		chatID = strings.TrimSpace(chatID)
		if chatID == "" || state == nil || !state.IsActive {
			continue
		}
		ids = append(ids, chatID)
	}
	sort.Strings(ids)
	return ids
}

// GetRuntimeDiagnostics returns quest scheduler runtime diagnostics for operators.
func (e *QuestEngine) GetRuntimeDiagnostics() map[string]interface{} {
	e.mu.RLock()
	defer e.mu.RUnlock()

	riskLockSource := e.currentRiskLockSourceLocked()
	executionStage := questExecutionStageDone
	executionLastProgress := ""
	executionInProgressAge := 0.0
	executionLockHeld := false
	executionLockTTL := ""
	executionLockCheckedAt := ""
	executionStaleReason := ""
	executionStaleAt := ""
	now := time.Now().UTC()
	for questID := range e.executing {
		stage := strings.TrimSpace(e.executionStage[questID])
		if stage == "" {
			stage = questExecutionStageLock
		}
		executionStage = stage
		progressAt := e.executionLastProgress[questID]
		if progressAt.IsZero() {
			progressAt = e.executionStarts[questID]
		}
		if !progressAt.IsZero() {
			executionLastProgress = progressAt.UTC().Format(time.RFC3339)
			executionInProgressAge = now.Sub(progressAt).Seconds()
		}
		executionLockHeld = e.executionLockHeld[questID]
		if ttl, ok := e.executionLockTTL[questID]; ok {
			executionLockTTL = ttl.String()
		}
		if checkedAt, ok := e.executionLockCheckedAt[questID]; ok && !checkedAt.IsZero() {
			executionLockCheckedAt = checkedAt.UTC().Format(time.RFC3339)
		}
		if reason, ok := e.executionStaleResetReason[questID]; ok {
			executionStaleReason = strings.TrimSpace(reason)
		}
		if staleAt, ok := e.executionStaleResetAt[questID]; ok && !staleAt.IsZero() {
			executionStaleAt = staleAt.UTC().Format(time.RFC3339)
		}
		break
	}

	return map[string]interface{}{
		"cadence_mode":                      e.cadenceMode,
		"active_quests":                     len(e.quests),
		"executing_quests":                  len(e.executing),
		"risk_lock_active":                  e.isRiskLockEnabledLocked(),
		"risk_lock_source":                  riskLockSource,
		"risk_lock_reasons":                 append([]string(nil), e.riskLockReasons...),
		"execution_stage":                   executionStage,
		"execution_last_progress_at":        executionLastProgress,
		"execution_in_progress_age_seconds": executionInProgressAge,
		"execution_lock_held":               executionLockHeld,
		"execution_lock_ttl":                executionLockTTL,
		"execution_lock_checked_at":         executionLockCheckedAt,
		"execution_stale_reset_reason":      executionStaleReason,
		"execution_stale_reset_at":          executionStaleAt,
		"provider_chain_configured":         e.aiProviderChainConfigured,
		"provider_chain_usable":             e.aiProviderChainUsable,
		"watchdog_stale":                    e.runtimeBudget.StaleTimeout.String(),
		"execution_timeout":                 e.runtimeBudget.ExecutionTimeout.String(),
		"lock_ttl":                          e.runtimeBudget.LockTTL.String(),
		"budget_floor":                      e.runtimeBudget.DerivedFloor.String(),
	}
}

// GetChatRuntimeDiagnostics returns chat-scoped runtime checkpoint diagnostics for operators.
func (e *QuestEngine) GetChatRuntimeDiagnostics(chatID string) map[string]interface{} {
	e.mu.RLock()
	defer e.mu.RUnlock()

	chatID = strings.TrimSpace(chatID)
	result := map[string]interface{}{
		"chat_id":                           chatID,
		"active_scalping_quests":            0,
		"hold_streak":                       0,
		"unlock_cycles":                     0,
		"recovery_mode":                     "normal",
		"recovery_clean_cycles_current":     0,
		"recovery_clean_cycles_required":    1,
		"recovery_cycles_to_entry":          0,
		"recovery_entry_allowed":            true,
		"recovery_next_condition":           "",
		"state_drift_active":                false,
		"state_drift_positions":             0,
		"state_drift_count":                 0,
		"state_drift_last_checked_at":       "",
		"entry_gate_reason_current":         "",
		"entry_gate_type":                   "none",
		"last_entry_attempt_at":             "",
		"minutes_since_entry_attempt":       0.0,
		"entry_attempts_1h":                 0,
		"entry_attempt_block_reason":        "",
		"next_unblock_condition_current":    "",
		"account_tier":                      "",
		"effective_min_confidence":          0.0,
		"effective_max_capital_pct":         0.0,
		"candidate_universe_count":          0,
		"candidate_ranked_count":            0,
		"candidate_viable_count":            0,
		"top_candidate_rejections":          []map[string]interface{}{},
		"progress_blocked":                  false,
		"progress_block_reason":             "",
		"rollout_stage_current":             "",
		"rollout_status_current":            "",
		"rollout_gate_reason_current":       "",
		"last_drift_repair_at":              "",
		"last_clean_reconcile_at":           "",
		"drift_signature":                   "",
		"drift_deadlock_cycles":             0,
		"last_startup_reconcile":            "",
		"last_spot_unwind":                  "",
		"last_hold_digest":                  "",
		"runtime_no_fill_since":             "",
		"runtime_failure_streak":            0,
		"runtime_last_failure_at":           "",
		"risk_lock_source":                  "none",
		"execution_stage":                   questExecutionStageDone,
		"execution_last_progress_at":        "",
		"execution_in_progress_age_seconds": 0.0,
		"execution_lock_held":               false,
		"execution_lock_ttl":                "",
		"execution_lock_checked_at":         "",
		"stale_reset_reason":                "",
		"stale_reset_at":                    "",
		"autonomy_strategy_id":              "",
		"autonomy_rollout_stage":            "",
		"autonomy_rollout_status":           "",
		"autonomy_gate_open":                false,
		"autonomy_gate_block_reasons":       []string{},
		"recovery_recent_loss_streak":       0,
		"recovery_recent_loss_active":       false,
		"recovery_recent_loss_window":       "",
		"provider_chain_configured":         0,
		"provider_chain_usable":             0,
	}
	if chatID == "" {
		return result
	}

	var (
		lastStartupReconcile           time.Time
		lastSpotUnwind                 time.Time
		lastHoldDigest                 time.Time
		lastEntryAttempt               time.Time
		latestFailureAt                time.Time
		holdStreak                     int
		unlockCycles                   int
		failureStreak                  int
		noFillSince                    time.Time
		activeScalping                 int
		recoveryMode                   string
		recoveryNextCondition          string
		recoveryNextConditionAt        time.Time
		hasActiveRecoveryNextCondition bool
		recoveryCleanCycles            int
		recoveryCleanRequired          int
		recoveryCyclesToEntry          int
		recoveryGateEvalAt             time.Time
		entryAttempts1h                int
		entryAttemptBlock              string
		nextUnblockCondition           string
		nextUnblockConditionAt         time.Time
		hasActiveNextUnblockCondition  bool
		accountTier                    string
		effectiveMinConfidence         float64
		effectiveMaxCapitalPct         float64
		candidateUniverseCount         int
		candidateRankedCount           int
		candidateViableCount           int
		topCandidateRejections         []map[string]interface{}
		progressBlockReason            string
		driftSignature                 string
		driftDeadlockCycles            int
		recoveryEntryAllowed           = true
		stateDriftActive               bool
		stateDriftPositions            int
		stateDriftLastChecked          time.Time
		lastDriftRepair                time.Time
		lastCleanReconcile             time.Time
		entryGateReasonCurrent         string
		entryGateReasonCurrentAt       time.Time
		hasActiveEntryGateReason       bool
		entryGateType                  string
		entryGateTypeAt                time.Time
		hasActiveEntryGateType         bool
		riskCurrentDrawdown            float64
		riskCurrentDrawdownAt          time.Time
		hasActiveScalpingRisk          bool
		hasRiskCurrentDrawdown         bool
		riskMaxDrawdown                float64
		riskExpectancy                 float64
		riskExpectancyGross            float64
		riskFeeDragExpectancy          float64
		aiWindowTotal                  int
		aiWindowSuccess                int
		aiWindowErrors                 int
		aiWindowTimeouts               int
		aiWindowParseFails             int
		aiWindowStarted                time.Time
		aiLastEventAt                  time.Time
		aiLastCategory                 string
		aiLastProvider                 string
		aiLastSuccessProvider          string
		aiLastError                    string
		aiLastErrorAt                  time.Time
		aiLastSuccessAt                time.Time
		aiCircuitUntil                 time.Time
		aiCircuitReason                string
		aiCircuitTrips                 int
		aiFailoverAttempts             int
		aiFailoverSuccesses            int
		aiFailoverFailures             int
		executionProgressAt            time.Time
		executionStage                 string
		executionLockHeld              bool
		executionLockTTL               time.Duration
		executionLockChecked           time.Time
		staleResetReason               string
		staleResetAt                   time.Time
		autonomyStrategyID             string
		autonomyRolloutStage           string
		autonomyRolloutStatus          string
		autonomyGateOpen               bool
		autonomyGateReasons            []string
		rolloutStageCurrent            string
		rolloutStatusCurrent           string
		rolloutGateReason              string
		recentLossStreak               int
		recentLossActive               bool
		recentLossWindowSec            int
		walletBasisMode                string
		walletBasisSource              string
		walletBasisUSDT                float64
		protectionMissingDetected      int
		protectionMissingRecovered     int
	)

	for _, quest := range e.quests {
		if strings.TrimSpace(quest.Metadata["chat_id"]) != chatID {
			continue
		}
		cp := quest.Checkpoint
		selectionAt := quest.UpdatedAt.UTC()
		checkpointAt := readCheckpointTime(cp["recovery_gate_eval_at"])
		if checkpointAt.After(selectionAt) {
			selectionAt = checkpointAt
		}
		isActiveScalpingQuest := strings.TrimSpace(quest.Metadata["definition_id"]) == "scalping_execution" &&
			quest.Status == QuestStatusActive
		if isActiveScalpingQuest {
			activeScalping++
		}
		if e.executing[quest.ID] {
			startedAt := e.executionStarts[quest.ID]
			progressAt := e.executionLastProgress[quest.ID]
			if progressAt.IsZero() {
				progressAt = startedAt
			}
			if progressAt.After(executionProgressAt) {
				executionProgressAt = progressAt
				stage := strings.TrimSpace(e.executionStage[quest.ID])
				if stage == "" {
					stage = questExecutionStageLock
				}
				executionStage = stage
			}
		}

		// Prefer lock/stale fields with the latest check/reset timestamps for deterministic diagnostics.
		if checkedAt, ok := e.executionLockCheckedAt[quest.ID]; ok && checkedAt.After(executionLockChecked) {
			executionLockChecked = checkedAt
			if lockTTL, ok := e.executionLockTTL[quest.ID]; ok {
				executionLockTTL = lockTTL
			} else {
				executionLockTTL = 0
			}
			if lockHeld, ok := e.executionLockHeld[quest.ID]; ok {
				executionLockHeld = lockHeld
			} else {
				executionLockHeld = false
			}
		}
		if resetAt, ok := e.executionStaleResetAt[quest.ID]; ok && resetAt.After(staleResetAt) {
			staleResetAt = resetAt
			staleResetReason = strings.TrimSpace(e.executionStaleResetReason[quest.ID])
		}

		holdStreak = maxInt(holdStreak, readQuestMetricInt(cp["runtime_hold_streak"]))
		unlockCycles = maxInt(unlockCycles, readQuestMetricInt(cp["runtime_unlock_cycles"]))
		failureStreak = maxInt(failureStreak, readQuestMetricInt(cp["runtime_failure_streak"]))
		recoveryCleanCycles = maxInt(recoveryCleanCycles, readQuestMetricIntWithFallback(cp, "recovery_clean_cycles_current", "recovery_clean_cycles"))
		recoveryCleanRequired = maxInt(recoveryCleanRequired, readQuestMetricInt(cp["recovery_clean_cycles_required"]))
		recoveryCyclesToEntry = maxInt(recoveryCyclesToEntry, readQuestMetricInt(cp["recovery_cycles_to_entry"]))
		if mode := readQuestMetricString(cp["recovery_mode"]); mode != "" {
			recoveryMode = mode
		}
		if checkpointAt.After(recoveryGateEvalAt) {
			recoveryGateEvalAt = checkpointAt
		}
		if nextCondition := readQuestMetricString(cp["recovery_next_condition"]); nextCondition != "" {
			switch {
			case isActiveScalpingQuest:
				recoveryNextCondition = nextCondition
				recoveryNextConditionAt = selectionAt
				hasActiveRecoveryNextCondition = true
			case !hasActiveRecoveryNextCondition && selectionAt.After(recoveryNextConditionAt):
				recoveryNextCondition = nextCondition
				recoveryNextConditionAt = selectionAt
			}
		}
		if unblock := readQuestMetricString(cp["runtime_next_unblock_condition"]); unblock != "" {
			switch {
			case isActiveScalpingQuest:
				nextUnblockCondition = unblock
				nextUnblockConditionAt = selectionAt
				hasActiveNextUnblockCondition = true
			case !hasActiveNextUnblockCondition && selectionAt.After(nextUnblockConditionAt):
				nextUnblockCondition = unblock
				nextUnblockConditionAt = selectionAt
			}
		}
		if _, exists := cp["recovery_entry_allowed"]; exists {
			recoveryEntryAllowed = readQuestMetricBool(cp["recovery_entry_allowed"])
		}
		if gateType := readQuestMetricString(cp["entry_gate_type"]); gateType != "" {
			switch {
			case isActiveScalpingQuest:
				entryGateType = gateType
				entryGateTypeAt = selectionAt
				hasActiveEntryGateType = true
			case !hasActiveEntryGateType && selectionAt.After(entryGateTypeAt):
				entryGateType = gateType
				entryGateTypeAt = selectionAt
			}
		}
		if raw, exists := cp["risk_current_drawdown"]; exists {
			drawdown := readQuestMetricFloat(raw)
			switch {
			case isActiveScalpingQuest && (!hasActiveScalpingRisk || selectionAt.After(riskCurrentDrawdownAt)):
				riskCurrentDrawdown = drawdown
				riskCurrentDrawdownAt = selectionAt
				hasActiveScalpingRisk = true
				hasRiskCurrentDrawdown = true
			case !hasActiveScalpingRisk && selectionAt.After(riskCurrentDrawdownAt):
				riskCurrentDrawdown = drawdown
				riskCurrentDrawdownAt = selectionAt
				hasRiskCurrentDrawdown = true
			}
		}
		if drawdown := readQuestMetricFloat(cp["risk_max_drawdown"]); drawdown > riskMaxDrawdown {
			riskMaxDrawdown = drawdown
		}
		if expectancy := readQuestMetricFloat(cp["risk_expectancy"]); expectancy != 0 || cp["risk_expectancy"] != nil {
			riskExpectancy = expectancy
		}
		if gross := readQuestMetricFloat(cp["risk_expectancy_gross"]); gross != 0 || cp["risk_expectancy_gross"] != nil {
			riskExpectancyGross = gross
		}
		if drag := readQuestMetricFloat(cp["risk_fee_drag_expectancy"]); drag != 0 || cp["risk_fee_drag_expectancy"] != nil {
			riskFeeDragExpectancy = drag
		}
		recentLossStreak = maxInt(recentLossStreak, readQuestMetricInt(cp["recovery_recent_loss_streak"]))
		if readQuestMetricBool(cp["recovery_recent_loss_active"]) {
			recentLossActive = true
		}
		if windowSec := readQuestMetricInt(cp["recovery_recent_loss_window_seconds"]); windowSec > recentLossWindowSec {
			recentLossWindowSec = windowSec
		}
		if value := readQuestMetricString(cp["autonomy_strategy_id"]); value != "" {
			autonomyStrategyID = value
		}
		if value := readQuestMetricString(cp["autonomy_rollout_stage"]); value != "" {
			autonomyRolloutStage = value
		}
		if value := readQuestMetricString(cp["autonomy_rollout_status"]); value != "" {
			autonomyRolloutStatus = value
		}
		if value := readQuestMetricString(cp["rollout_stage_current"]); value != "" {
			rolloutStageCurrent = value
		}
		if value := readQuestMetricString(cp["rollout_status_current"]); value != "" {
			rolloutStatusCurrent = value
		}
		if value := readQuestMetricString(cp["rollout_gate_reason_current"]); value != "" {
			rolloutGateReason = value
		}
		if mode := readQuestMetricString(cp["wallet_basis_mode"]); mode != "" {
			walletBasisMode = mode
		}
		if source := readQuestMetricString(cp["wallet_basis_source"]); source != "" {
			walletBasisSource = source
		}
		if basis := readQuestMetricFloat(cp["wallet_basis_usdt"]); basis > walletBasisUSDT {
			walletBasisUSDT = basis
		}
		protectionMissingDetected = maxInt(protectionMissingDetected, readQuestMetricInt(cp["protection_missing_detected"]))
		protectionMissingRecovered = maxInt(protectionMissingRecovered, readQuestMetricInt(cp["protection_missing_recovered"]))
		if _, exists := cp["autonomy_gate_open"]; exists {
			autonomyGateOpen = readQuestMetricBool(cp["autonomy_gate_open"])
		}
		if raw, ok := cp["autonomy_gate_block_reasons"].([]string); ok {
			autonomyGateReasons = append([]string(nil), raw...)
		} else if raw, ok := cp["autonomy_gate_block_reasons"].([]interface{}); ok {
			converted := make([]string, 0, len(raw))
			for _, item := range raw {
				text := readQuestMetricString(item)
				if text != "" {
					converted = append(converted, text)
				}
			}
			if len(converted) > 0 {
				autonomyGateReasons = converted
			}
		}

		if ts := readCheckpointTime(cp["runtime_bootstrap_synced_at"]); ts.After(lastStartupReconcile) {
			lastStartupReconcile = ts
		}
		if ts := readCheckpointTime(cp["spot_unwind_checked_at"]); ts.After(lastSpotUnwind) {
			lastSpotUnwind = ts
		}
		if ts := readCheckpointTime(cp["runtime_last_hold_digest_at"]); ts.After(lastHoldDigest) {
			lastHoldDigest = ts
		}
		if ts := readCheckpointTime(cp["runtime_last_entry_attempt_at"]); ts.After(lastEntryAttempt) {
			lastEntryAttempt = ts
		}
		if ts := readCheckpointTime(cp["runtime_last_failure_at"]); ts.After(latestFailureAt) {
			latestFailureAt = ts
		}
		if ts := readCheckpointTime(cp["runtime_no_fill_since"]); ts.After(noFillSince) {
			noFillSince = ts
		}
		entryAttempts1h = maxInt(entryAttempts1h, readQuestMetricInt(cp["runtime_entry_attempts_1h"]))
		if reason := readQuestMetricString(cp["runtime_entry_attempt_block_reason"]); reason != "" {
			entryAttemptBlock = reason
		}
		if tier := readQuestMetricString(cp["account_tier"]); tier != "" {
			accountTier = tier
		}
		if value := readQuestMetricFloat(cp["effective_min_confidence"]); value > effectiveMinConfidence {
			effectiveMinConfidence = value
		}
		if value := readQuestMetricFloat(cp["effective_max_capital_pct"]); value > effectiveMaxCapitalPct {
			effectiveMaxCapitalPct = value
		}
		candidateUniverseCount = maxInt(candidateUniverseCount, readQuestMetricInt(cp["candidate_universe_count"]))
		candidateRankedCount = maxInt(candidateRankedCount, readQuestMetricInt(cp["candidate_ranked_count"]))
		candidateViableCount = maxInt(candidateViableCount, readQuestMetricInt(cp["candidate_viable_count"]))
		if rejections := readCandidateRejections(cp["top_candidate_rejections"]); len(rejections) > 0 {
			topCandidateRejections = rejections
		}
		if readQuestMetricBool(cp["state_drift_active"]) {
			stateDriftActive = true
		}
		stateDriftPositions = maxInt(stateDriftPositions, readQuestMetricInt(cp["state_drift_positions"]))
		stateDriftPositions = maxInt(stateDriftPositions, readQuestMetricInt(cp["state_drift_count"]))
		driftDeadlockCycles = maxInt(driftDeadlockCycles, readQuestMetricInt(cp["state_drift_deadlock_cycles"]))
		if sig := readQuestMetricString(cp["state_drift_signature"]); sig != "" {
			driftSignature = sig
		}
		if ts := readCheckpointTime(cp["state_drift_last_checked_at"]); ts.After(stateDriftLastChecked) {
			stateDriftLastChecked = ts
		}
		if ts := readCheckpointTime(cp["state_drift_last_repair_at"]); ts.After(lastDriftRepair) {
			lastDriftRepair = ts
		}
		if ts := readCheckpointTime(cp["state_drift_last_clean_reconcile_at"]); ts.After(lastCleanReconcile) {
			lastCleanReconcile = ts
		}
		if reason := readQuestMetricString(cp["runtime_entry_gate_reason"]); reason != "" {
			switch {
			case isActiveScalpingQuest:
				entryGateReasonCurrent = reason
				entryGateReasonCurrentAt = selectionAt
				hasActiveEntryGateReason = true
			case !hasActiveEntryGateReason && selectionAt.After(entryGateReasonCurrentAt):
				entryGateReasonCurrent = reason
				entryGateReasonCurrentAt = selectionAt
			}
		}

		aiWindowTotal = maxInt(aiWindowTotal, readQuestMetricInt(cp["runtime_ai_window_total"]))
		aiWindowSuccess = maxInt(aiWindowSuccess, readQuestMetricInt(cp["runtime_ai_window_success"]))
		aiWindowErrors = maxInt(aiWindowErrors, readQuestMetricInt(cp["runtime_ai_window_errors"]))
		aiWindowTimeouts = maxInt(aiWindowTimeouts, readQuestMetricInt(cp["runtime_ai_window_timeouts"]))
		aiWindowParseFails = maxInt(aiWindowParseFails, readQuestMetricInt(cp["runtime_ai_window_parse_fails"]))
		aiFailoverAttempts = maxInt(aiFailoverAttempts, readQuestMetricInt(cp["runtime_ai_window_failover_attempts"]))
		aiFailoverSuccesses = maxInt(aiFailoverSuccesses, readQuestMetricInt(cp["runtime_ai_window_failover_successes"]))
		aiFailoverFailures = maxInt(aiFailoverFailures, readQuestMetricInt(cp["runtime_ai_window_failover_failures"]))
		aiCircuitTrips = maxInt(aiCircuitTrips, readQuestMetricInt(cp["runtime_ai_circuit_trips"]))

		if ts := readCheckpointTime(cp["runtime_ai_window_started_at"]); ts.After(aiWindowStarted) {
			aiWindowStarted = ts
		}
		if ts := readCheckpointTime(cp["runtime_ai_last_event_at"]); ts.After(aiLastEventAt) {
			aiLastEventAt = ts
			if category, ok := cp["runtime_ai_last_category"].(string); ok {
				aiLastCategory = strings.TrimSpace(category)
			}
			if provider, ok := cp["runtime_ai_last_provider"].(string); ok {
				aiLastProvider = strings.TrimSpace(provider)
			}
			if provider, ok := cp["runtime_ai_last_success_provider"].(string); ok {
				aiLastSuccessProvider = strings.TrimSpace(provider)
			}
			if msg, ok := cp["runtime_ai_last_error"].(string); ok {
				aiLastError = strings.TrimSpace(msg)
			}
		}
		if ts := readCheckpointTime(cp["runtime_ai_last_error_at"]); ts.After(aiLastErrorAt) {
			aiLastErrorAt = ts
		}
		if ts := readCheckpointTime(cp["runtime_ai_last_success_at"]); ts.After(aiLastSuccessAt) {
			aiLastSuccessAt = ts
		}
		if ts := readCheckpointTime(cp["runtime_ai_circuit_until"]); ts.After(aiCircuitUntil) {
			aiCircuitUntil = ts
			if reason, ok := cp["runtime_ai_circuit_reason"].(string); ok {
				aiCircuitReason = strings.TrimSpace(reason)
			}
		}
	}

	result["active_scalping_quests"] = activeScalping
	result["hold_streak"] = holdStreak
	result["unlock_cycles"] = unlockCycles
	if recoveryMode == "" {
		recoveryMode = "normal"
	}
	if recoveryCleanRequired <= 0 {
		recoveryCleanRequired = 1
	}
	result["recovery_mode"] = recoveryMode
	result["recovery_clean_cycles_current"] = recoveryCleanCycles
	result["recovery_clean_cycles_required"] = recoveryCleanRequired
	result["recovery_cycles_to_entry"] = recoveryCyclesToEntry
	result["recovery_entry_allowed"] = recoveryEntryAllowed
	if !recoveryGateEvalAt.IsZero() {
		result["recovery_gate_eval_at"] = recoveryGateEvalAt.Format(time.RFC3339)
	}
	if strings.TrimSpace(recoveryNextCondition) != "" {
		result["recovery_next_condition"] = recoveryNextCondition
	}
	if strings.TrimSpace(nextUnblockCondition) == "" {
		nextUnblockCondition = recoveryNextCondition
	}
	if strings.TrimSpace(nextUnblockCondition) != "" {
		result["next_unblock_condition_current"] = strings.TrimSpace(nextUnblockCondition)
	}
	if strings.TrimSpace(accountTier) != "" {
		result["account_tier"] = strings.TrimSpace(accountTier)
	}
	if strings.TrimSpace(walletBasisMode) != "" {
		result["wallet_basis_mode"] = strings.TrimSpace(walletBasisMode)
	}
	if strings.TrimSpace(walletBasisSource) != "" {
		result["wallet_basis_source"] = strings.TrimSpace(walletBasisSource)
	}
	if walletBasisUSDT > 0 {
		result["wallet_basis_usdt"] = walletBasisUSDT
	}
	result["protection_missing_detected"] = protectionMissingDetected
	result["protection_missing_recovered"] = protectionMissingRecovered
	if effectiveMinConfidence > 0 {
		result["effective_min_confidence"] = effectiveMinConfidence
	}
	if effectiveMaxCapitalPct > 0 {
		result["effective_max_capital_pct"] = effectiveMaxCapitalPct
	}
	result["candidate_universe_count"] = candidateUniverseCount
	result["candidate_ranked_count"] = candidateRankedCount
	result["candidate_viable_count"] = candidateViableCount
	if len(topCandidateRejections) > 0 {
		result["top_candidate_rejections"] = topCandidateRejections
	}
	if hasRiskCurrentDrawdown {
		result["risk_current_drawdown"] = riskCurrentDrawdown
	}
	result["risk_max_drawdown"] = riskMaxDrawdown
	result["risk_expectancy"] = riskExpectancy
	result["risk_expectancy_gross"] = riskExpectancyGross
	result["risk_fee_drag_expectancy"] = riskFeeDragExpectancy
	result["state_drift_active"] = stateDriftActive
	result["state_drift_positions"] = stateDriftPositions
	result["state_drift_count"] = stateDriftPositions
	result["drift_deadlock_cycles"] = driftDeadlockCycles
	if strings.TrimSpace(driftSignature) != "" {
		result["drift_signature"] = strings.TrimSpace(driftSignature)
	}
	if !stateDriftLastChecked.IsZero() {
		result["state_drift_last_checked_at"] = stateDriftLastChecked.Format(time.RFC3339)
	}
	if !lastDriftRepair.IsZero() {
		result["last_drift_repair_at"] = lastDriftRepair.Format(time.RFC3339)
	}
	if !lastCleanReconcile.IsZero() {
		result["last_clean_reconcile_at"] = lastCleanReconcile.Format(time.RFC3339)
	}
	result["risk_lock_source"] = e.currentRiskLockSourceLocked()
	if strings.TrimSpace(entryGateReasonCurrent) == "" && e.isRiskLockEnabledLocked() {
		if len(e.riskLockReasons) > 0 {
			entryGateReasonCurrent = e.riskLockReasons[0]
		} else {
			entryGateReasonCurrent = "risk lock active"
		}
	}
	nowForGate := time.Now().UTC()
	switch {
	case e.isRiskLockEnabledLocked():
		entryGateType = "risk_lock"
	case stateDriftActive:
		entryGateType = "state_drift"
	case aiCircuitUntil.After(nowForGate):
		entryGateType = "runtime_circuit"
	case strings.TrimSpace(entryGateType) == "":
		entryGateType = "none"
	}
	if strings.TrimSpace(entryGateReasonCurrent) != "" {
		result["entry_gate_reason_current"] = strings.TrimSpace(entryGateReasonCurrent)
	}
	result["entry_gate_type"] = entryGateType
	if strings.TrimSpace(executionStage) != "" {
		result["execution_stage"] = executionStage
	}
	if !executionProgressAt.IsZero() {
		nowForExecution := time.Now().UTC()
		result["execution_last_progress_at"] = executionProgressAt.Format(time.RFC3339)
		result["execution_in_progress_age_seconds"] = nowForExecution.Sub(executionProgressAt).Seconds()
	}
	result["execution_lock_held"] = executionLockHeld
	if executionLockTTL > 0 {
		result["execution_lock_ttl"] = executionLockTTL.String()
	}
	if !executionLockChecked.IsZero() {
		result["execution_lock_checked_at"] = executionLockChecked.Format(time.RFC3339)
	}
	if strings.TrimSpace(staleResetReason) != "" {
		result["stale_reset_reason"] = strings.TrimSpace(staleResetReason)
	}
	if !staleResetAt.IsZero() {
		result["stale_reset_at"] = staleResetAt.Format(time.RFC3339)
	}
	if strings.TrimSpace(autonomyStrategyID) != "" {
		result["autonomy_strategy_id"] = autonomyStrategyID
	}
	if strings.TrimSpace(autonomyRolloutStage) != "" {
		result["autonomy_rollout_stage"] = autonomyRolloutStage
	}
	if strings.TrimSpace(autonomyRolloutStatus) != "" {
		result["autonomy_rollout_status"] = autonomyRolloutStatus
	}
	if strings.TrimSpace(rolloutStageCurrent) == "" {
		rolloutStageCurrent = autonomyRolloutStage
	}
	if strings.TrimSpace(rolloutStatusCurrent) == "" {
		rolloutStatusCurrent = autonomyRolloutStatus
	}
	if strings.TrimSpace(rolloutGateReason) == "" && len(autonomyGateReasons) > 0 {
		rolloutGateReason = autonomyGateReasons[0]
	}
	if strings.TrimSpace(rolloutStageCurrent) != "" {
		result["rollout_stage_current"] = rolloutStageCurrent
	}
	if strings.TrimSpace(rolloutStatusCurrent) != "" {
		result["rollout_status_current"] = rolloutStatusCurrent
	}
	if strings.TrimSpace(rolloutGateReason) != "" {
		result["rollout_gate_reason_current"] = rolloutGateReason
	}
	result["autonomy_gate_open"] = autonomyGateOpen
	if len(autonomyGateReasons) > 0 {
		result["autonomy_gate_block_reasons"] = autonomyGateReasons
	}
	result["recovery_recent_loss_streak"] = recentLossStreak
	result["recovery_recent_loss_active"] = recentLossActive
	if recentLossWindowSec > 0 {
		result["recovery_recent_loss_window"] = (time.Duration(recentLossWindowSec) * time.Second).String()
	}
	result["entry_attempts_1h"] = entryAttempts1h
	if strings.TrimSpace(entryAttemptBlock) != "" {
		result["entry_attempt_block_reason"] = strings.TrimSpace(entryAttemptBlock)
	}
	result["runtime_failure_streak"] = failureStreak
	if !lastStartupReconcile.IsZero() {
		result["last_startup_reconcile"] = lastStartupReconcile.Format(time.RFC3339)
	}
	if !lastSpotUnwind.IsZero() {
		result["last_spot_unwind"] = lastSpotUnwind.Format(time.RFC3339)
	}
	if !lastHoldDigest.IsZero() {
		result["last_hold_digest"] = lastHoldDigest.Format(time.RFC3339)
	}
	if !lastEntryAttempt.IsZero() {
		nowForAttempt := time.Now().UTC()
		result["last_entry_attempt_at"] = lastEntryAttempt.Format(time.RFC3339)
		result["minutes_since_entry_attempt"] = nowForAttempt.Sub(lastEntryAttempt).Minutes()
	}
	progressBlock := appautonomy.EvaluateProgressBlock(lastEntryAttempt, time.Now().UTC(), scalpingPolicyConfigFromEnv(0))
	result["progress_blocked"] = progressBlock.Blocked
	if strings.TrimSpace(progressBlock.Reason) != "" {
		progressBlockReason = progressBlock.Reason
	}
	if strings.TrimSpace(progressBlockReason) != "" {
		result["progress_block_reason"] = strings.TrimSpace(progressBlockReason)
	}
	if !latestFailureAt.IsZero() {
		result["runtime_last_failure_at"] = latestFailureAt.Format(time.RFC3339)
	}
	if !noFillSince.IsZero() {
		result["runtime_no_fill_since"] = noFillSince.Format(time.RFC3339)
	}

	windowErrorRate := 0.0
	if aiWindowTotal > 0 {
		windowErrorRate = float64(aiWindowErrors) / float64(aiWindowTotal)
	}
	warnRate := envFloatOrDefault("NEURATRADE_AI_RUNTIME_WARN_ERROR_RATE", 0.25, 0.01, 1)
	criticalRate := envFloatOrDefault("NEURATRADE_AI_RUNTIME_CRITICAL_ERROR_RATE", 0.50, 0.01, 1)
	now := time.Now().UTC()
	circuitActive := aiCircuitUntil.After(now)

	status := "healthy"
	if circuitActive || windowErrorRate >= criticalRate {
		status = "critical"
	} else if windowErrorRate >= warnRate {
		status = "warning"
	}

	aiRuntime := map[string]interface{}{
		"status":                    status,
		"window_started_at":         "",
		"window_total":              aiWindowTotal,
		"window_success":            aiWindowSuccess,
		"window_errors":             aiWindowErrors,
		"window_timeouts":           aiWindowTimeouts,
		"window_parse_fails":        aiWindowParseFails,
		"error_rate":                windowErrorRate,
		"warn_rate":                 warnRate,
		"critical_rate":             criticalRate,
		"circuit_active":            circuitActive,
		"circuit_until":             "",
		"circuit_reason":            aiCircuitReason,
		"circuit_trips":             aiCircuitTrips,
		"last_category":             aiLastCategory,
		"last_provider":             aiLastProvider,
		"last_success_provider":     aiLastSuccessProvider,
		"last_error":                aiLastError,
		"last_error_at":             "",
		"last_success_at":           "",
		"last_event_at":             "",
		"failover_attempts":         aiFailoverAttempts,
		"failover_successes":        aiFailoverSuccesses,
		"failover_failures":         aiFailoverFailures,
		"provider_chain_configured": e.aiProviderChainConfigured,
		"provider_chain_usable":     e.aiProviderChainUsable,
	}
	if !aiWindowStarted.IsZero() {
		aiRuntime["window_started_at"] = aiWindowStarted.Format(time.RFC3339)
	}
	if circuitActive {
		aiRuntime["circuit_until"] = aiCircuitUntil.Format(time.RFC3339)
	}
	if !aiLastErrorAt.IsZero() {
		aiRuntime["last_error_at"] = aiLastErrorAt.Format(time.RFC3339)
	}
	if !aiLastSuccessAt.IsZero() {
		aiRuntime["last_success_at"] = aiLastSuccessAt.Format(time.RFC3339)
	}
	if !aiLastEventAt.IsZero() {
		aiRuntime["last_event_at"] = aiLastEventAt.Format(time.RFC3339)
	}
	if status != "healthy" {
		reason := strings.TrimSpace(aiLastCategory)
		if reason == "" {
			reason = strings.TrimSpace(entryGateReasonCurrent)
		}
		if reason != "" {
			aiRuntime["runtime_degraded_reason"] = reason
		}
	}
	result["provider_chain_configured"] = e.aiProviderChainConfigured
	result["provider_chain_usable"] = e.aiProviderChainUsable
	result["ai_runtime"] = aiRuntime

	return result
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func readCheckpointTime(v interface{}) time.Time {
	switch raw := v.(type) {
	case time.Time:
		return raw.UTC()
	case *time.Time:
		if raw != nil {
			return raw.UTC()
		}
	case string:
		text := strings.TrimSpace(raw)
		if text == "" {
			return time.Time{}
		}
		if parsed, err := time.Parse(time.RFC3339, text); err == nil {
			return parsed.UTC()
		}
		if unixSec, err := strconv.ParseInt(text, 10, 64); err == nil {
			if unixSec > 0 {
				return time.Unix(unixSec, 0).UTC()
			}
		}
	case int:
		if raw > 0 {
			return time.Unix(int64(raw), 0).UTC()
		}
	case int64:
		if raw > 0 {
			return time.Unix(raw, 0).UTC()
		}
	case float64:
		if raw > 0 {
			return time.Unix(int64(raw), 0).UTC()
		}
	}
	return time.Time{}
}

// GetQuestProgress returns progress for all active quests for a user
func (e *QuestEngine) GetQuestProgress(chatID string) ([]QuestProgress, error) {
	e.mu.RLock()
	defer e.mu.RUnlock()

	var progress []QuestProgress
	for _, quest := range e.quests {
		if quest.Metadata["chat_id"] != chatID {
			continue
		}
		if quest.Status != QuestStatusActive {
			continue
		}

		p := QuestProgress{
			QuestID:   quest.ID,
			QuestName: quest.Name,
			Current:   quest.CurrentCount,
			Target:    quest.TargetCount,
			Status:    string(quest.Status),
		}

		if quest.TargetCount > 0 {
			p.Percent = (quest.CurrentCount * 100) / quest.TargetCount
			if p.Percent > 100 {
				p.Percent = 100
			}
		}

		progress = append(progress, p)
	}

	return progress, nil
}

// createQuestInternal creates a quest without locking (internal use)
func (e *QuestEngine) createQuestInternal(definitionID string, chatID string) (*Quest, error) {
	def, ok := e.definitions[definitionID]
	if !ok {
		return nil, fmt.Errorf("quest definition not found: %s", definitionID)
	}

	quest := &Quest{
		ID:           uuid.New().String(),
		Name:         def.Name,
		Description:  def.Description,
		Type:         def.Type,
		Cadence:      def.Cadence,
		Status:       QuestStatusPending,
		Prompt:       def.Prompt,
		TargetCount:  def.TargetCount,
		CurrentCount: 0,
		Checkpoint:   make(map[string]interface{}),
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
		Metadata: map[string]string{
			"chat_id":       chatID,
			"definition_id": definitionID,
		},
	}

	e.quests[quest.ID] = quest

	if e.store != nil {
		if err := e.store.SaveQuest(context.Background(), quest); err != nil {
			log.Printf("Failed to persist quest %s: %v", quest.ID, err)
		}
	}

	return quest, nil
}

// UpdateQuestProgress updates the progress of a quest
func (e *QuestEngine) UpdateQuestProgress(questID string, current int, checkpoint map[string]interface{}) error {
	e.mu.Lock()

	quest, ok := e.quests[questID]
	if !ok {
		e.mu.Unlock()
		return fmt.Errorf("quest not found: %s", questID)
	}

	previousCount := quest.CurrentCount
	quest.CurrentCount = current
	quest.Checkpoint = checkpoint
	quest.UpdatedAt = time.Now()

	if current >= quest.TargetCount && quest.TargetCount > 0 {
		now := time.Now()
		quest.Status = QuestStatusCompleted
		quest.CompletedAt = &now
	}

	chatID := e.chatIDForQuest[questID]
	e.mu.Unlock()

	if e.store != nil {
		if err := e.store.SaveQuest(context.Background(), quest); err != nil {
			log.Printf("Failed to persist quest %s: %v", quest.ID, err)
		}
	}

	if e.notificationService != nil && chatID > 0 && current > previousCount {
		percent := 0
		if quest.TargetCount > 0 {
			percent = (current * 100) / quest.TargetCount
		}
		timeRemaining := calculateTimeRemaining(quest)
		progressNotif := QuestProgressNotification{
			QuestID:       questID,
			QuestName:     quest.Name,
			Current:       current,
			Target:        quest.TargetCount,
			Percent:       percent,
			Status:        string(quest.Status),
			TimeRemaining: timeRemaining,
		}
		go func() {
			if err := e.notificationService.NotifyQuestProgress(context.Background(), chatID, progressNotif); err != nil {
				log.Printf("Failed to send quest progress notification for %s: %v", questID, err)
			}
		}()
	}

	return nil
}

// GetQuest retrieves a quest by ID
func (e *QuestEngine) GetQuest(questID string) (*Quest, error) {
	e.mu.RLock()
	defer e.mu.RUnlock()

	quest, ok := e.quests[questID]
	if !ok {
		return nil, fmt.Errorf("quest not found: %s", questID)
	}
	return quest, nil
}

// ListQuests lists all quests for a user
func (e *QuestEngine) ListQuests(chatID string, status QuestStatus) ([]*Quest, error) {
	e.mu.RLock()
	defer e.mu.RUnlock()

	var result []*Quest
	for _, quest := range e.quests {
		if quest.Metadata["chat_id"] != chatID {
			continue
		}
		if status != "" && quest.Status != status {
			continue
		}
		result = append(result, quest)
	}

	return result, nil
}

// MarshalCheckpoint serializes checkpoint data
func MarshalCheckpoint(data map[string]interface{}) ([]byte, error) {
	return json.Marshal(data)
}

// UnmarshalCheckpoint deserializes checkpoint data
func UnmarshalCheckpoint(data []byte) (map[string]interface{}, error) {
	var result map[string]interface{}
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, err
	}
	return result, nil
}

func cloneQuestForPersistence(quest *Quest) *Quest {
	if quest == nil {
		return nil
	}

	cloned := *quest
	cloned.Checkpoint = cloneCheckpointMap(quest.Checkpoint)
	cloned.Metadata = cloneStringMap(quest.Metadata)
	if quest.LastExecutedAt != nil {
		lastExecuted := *quest.LastExecutedAt
		cloned.LastExecutedAt = &lastExecuted
	}
	if quest.CompletedAt != nil {
		completedAt := *quest.CompletedAt
		cloned.CompletedAt = &completedAt
	}
	return &cloned
}

func cloneCheckpointMap(input map[string]interface{}) map[string]interface{} {
	if input == nil {
		return nil
	}
	cloned := make(map[string]interface{}, len(input))
	for key, value := range input {
		cloned[key] = cloneCheckpointValue(value)
	}
	return cloned
}

func cloneCheckpointValue(value interface{}) interface{} {
	switch typed := value.(type) {
	case map[string]interface{}:
		return cloneCheckpointMap(typed)
	case []interface{}:
		cloned := make([]interface{}, len(typed))
		for i, item := range typed {
			cloned[i] = cloneCheckpointValue(item)
		}
		return cloned
	case []string:
		return append([]string(nil), typed...)
	case []int:
		return append([]int(nil), typed...)
	case []float64:
		return append([]float64(nil), typed...)
	case []bool:
		return append([]bool(nil), typed...)
	case map[string]string:
		return cloneStringMap(typed)
	case map[string]bool:
		cloned := make(map[string]bool, len(typed))
		for key, item := range typed {
			cloned[key] = item
		}
		return cloned
	default:
		return value
	}
}

func cloneStringMap(input map[string]string) map[string]string {
	if input == nil {
		return nil
	}
	cloned := make(map[string]string, len(input))
	for key, value := range input {
		cloned[key] = value
	}
	return cloned
}

func calculateTimeRemaining(quest *Quest) string {
	if quest.Status == QuestStatusCompleted {
		return "completed"
	}
	if quest.Status == QuestStatusFailed {
		return "failed"
	}

	lastExec := time.Now()
	if quest.LastExecutedAt != nil {
		lastExec = *quest.LastExecutedAt
	}

	var duration time.Duration
	switch quest.Cadence {
	case CadenceMicro:
		duration = 5 * time.Minute
	case CadenceHourly:
		duration = time.Hour
	case CadenceDaily:
		duration = 24 * time.Hour
	case CadenceWeekly:
		duration = 7 * 24 * time.Hour
	case CadenceOnetime:
		return "one-time"
	}

	nextRun := lastExec.Add(duration)
	remaining := time.Until(nextRun)
	if remaining <= 0 {
		return "due now"
	}

	if remaining < time.Minute {
		return "<1m"
	}
	if remaining < time.Hour {
		mins := int(remaining.Minutes())
		return fmt.Sprintf("%dm", mins)
	}
	if remaining < 24*time.Hour {
		hours := int(remaining.Hours())
		mins := int(remaining.Minutes()) % 60
		if mins > 0 {
			return fmt.Sprintf("%dh %dm", hours, mins)
		}
		return fmt.Sprintf("%dh", hours)
	}
	days := int(remaining.Hours() / 24)
	return fmt.Sprintf("%dd", days)
}
