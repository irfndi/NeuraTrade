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
)

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
	autonomousState           map[string]*AutonomousState
	definitions               map[string]*QuestDefinition
	handlers                  map[QuestType]QuestHandler
	store                     QuestStore
	redis                     *redis.Client
	stopCh                    chan struct{}
	running                   bool
	cadenceMode               string
	lastTickAt                time.Time
	runtimeBudget             questRuntimeBudget
	riskLockActive            bool
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
		autonomousState:           make(map[string]*AutonomousState),
		definitions:               make(map[string]*QuestDefinition),
		handlers:                  make(map[QuestType]QuestHandler),
		store:                     store,
		redis:                     redisClient,
		stopCh:                    make(chan struct{}),
		chatIDForQuest:            make(map[string]int64),
		cadenceMode:               "normal",
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
	e.running = true
	e.runtimeBudget = computeQuestRuntimeBudget()
	e.mu.Unlock()

	// Load active quests from database
	e.loadActiveQuests()

	go e.schedulerLoop()
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
	defer e.mu.Unlock()
	if !e.running {
		return
	}
	close(e.stopCh)
	e.running = false
	log.Println("Quest engine stopped")
}

// schedulerLoop runs the periodic quest scheduling
func (e *QuestEngine) schedulerLoop() {
	log.Println("[QUEST] Scheduler loop started")
	ticker := time.NewTicker(defaultQuestSchedulerPoll)
	defer ticker.Stop()
	log.Printf("[QUEST] Scheduler polling interval: %s", defaultQuestSchedulerPoll)

	// Run an immediate check so newly activated quests do not wait for the first interval.
	e.evaluateAndTick(time.Now().UTC(), true)

	for {
		select {
		case <-e.stopCh:
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
	for _, quest := range e.quests {
		if quest.Status != QuestStatusActive {
			continue
		}
		activeQuests++
		if readQuestMetricInt(quest.Checkpoint["runtime_failure_streak"]) > 0 {
			degraded = true
		}
	}

	if activeQuests == 0 {
		return "idle"
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

			age := now.Sub(startedAt)
			// Never clear the in-progress marker before the distributed lock can expire.
			// This prevents stale-reset -> immediate lock contention loops.
			staleAfter := e.runtimeBudget.StaleTimeout
			if e.runtimeBudget.LockTTL > staleAfter {
				staleAfter = e.runtimeBudget.LockTTL
			}
			if age > staleAfter {
				log.Printf(
					"[QUEST] Quest %s (%s) execution stale after %s (reset_after=%s stale=%s lock_ttl=%s), resetting in-progress marker",
					quest.ID,
					quest.Name,
					age.Round(time.Second),
					staleAfter.Round(time.Second),
					e.runtimeBudget.StaleTimeout.Round(time.Second),
					e.runtimeBudget.LockTTL.Round(time.Second),
				)
				delete(e.executing, quest.ID)
				delete(e.executionStarts, quest.ID)
			} else {
				log.Printf(
					"[QUEST] Quest %s (%s) skipped - execution already in progress for %s",
					quest.ID,
					quest.Name,
					age.Round(time.Second),
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

	if quest.LastExecutedAt != nil && now.Sub(*quest.LastExecutedAt) < minInterval {
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
		return false
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

// executeQuest executes a single quest
func (e *QuestEngine) executeQuest(quest *Quest) {
	defer e.markQuestExecutionFinished(quest.ID)

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
		log.Printf("Quest %s skipped: could not acquire lock (another instance may be running)", quest.ID)
		return
	}
	defer e.releaseLock(ctx, lockKey)

	if err := handler(ctx, quest); err != nil {
		log.Printf("Quest %s (%s) failed: %v", quest.ID, quest.Name, err)
		e.updateQuestStatus(quest.ID, QuestStatusFailed)
		quest.LastError = err.Error()
	} else {
		log.Printf("Quest %s (%s) completed successfully", quest.ID, quest.Name)
		now := time.Now()
		e.updateLastExecuted(quest.ID, now)
		if quest.Type == QuestTypeRoutine {
			e.updateQuestStatus(quest.ID, QuestStatusActive)
		} else {
			e.updateQuestStatus(quest.ID, QuestStatusCompleted)
		}
	}
}

func (e *QuestEngine) markQuestExecutionFinished(questID string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	delete(e.executing, questID)
	delete(e.executionStarts, questID)
}

func (e *QuestEngine) acquireLock(ctx context.Context, key string, ttl time.Duration) bool {
	if e.redis == nil {
		return true
	}
	ok, err := e.redis.SetNX(ctx, key, "locked", ttl).Result()
	if err != nil {
		log.Printf("Failed to acquire lock %s: %v", key, err)
		return false
	}
	return ok
}

func (e *QuestEngine) releaseLock(ctx context.Context, key string) {
	if e.redis == nil {
		return
	}
	e.redis.Del(ctx, key)
}

func (e *QuestEngine) updateLastExecuted(questID string, executedAt time.Time) {
	e.mu.Lock()
	defer e.mu.Unlock()

	if quest, ok := e.quests[questID]; ok {
		quest.LastExecutedAt = &executedAt
		quest.UpdatedAt = time.Now()

		if e.store != nil {
			if err := e.store.UpdateLastExecuted(context.Background(), questID, executedAt); err != nil {
				log.Printf("Failed to persist last executed time: %v", err)
			}
		}
	}
}

// updateQuestStatus updates a quest's status
func (e *QuestEngine) updateQuestStatus(questID string, status QuestStatus) {
	e.mu.Lock()
	defer e.mu.Unlock()

	if quest, ok := e.quests[questID]; ok {
		quest.Status = status
		quest.UpdatedAt = time.Now()
		if status == QuestStatusCompleted {
			now := time.Now()
			quest.CompletedAt = &now
		}

		if e.store != nil {
			if err := e.store.SaveQuest(context.Background(), quest); err != nil {
				log.Printf("Failed to persist quest status update: %v", err)
			}
		}
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
		quest, err := e.createQuestInternal(defID, chatID)
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
	e.mu.Lock()
	defer e.mu.Unlock()
	e.riskLockActive = active
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

	return map[string]interface{}{
		"cadence_mode":              e.cadenceMode,
		"active_quests":             len(e.quests),
		"executing_quests":          len(e.executing),
		"risk_lock_active":          e.isRiskLockEnabledLocked(),
		"risk_lock_reasons":         append([]string(nil), e.riskLockReasons...),
		"provider_chain_configured": e.aiProviderChainConfigured,
		"provider_chain_usable":     e.aiProviderChainUsable,
		"watchdog_stale":            e.runtimeBudget.StaleTimeout.String(),
		"execution_timeout":         e.runtimeBudget.ExecutionTimeout.String(),
		"lock_ttl":                  e.runtimeBudget.LockTTL.String(),
		"budget_floor":              e.runtimeBudget.DerivedFloor.String(),
	}
}

// GetChatRuntimeDiagnostics returns chat-scoped runtime checkpoint diagnostics for operators.
func (e *QuestEngine) GetChatRuntimeDiagnostics(chatID string) map[string]interface{} {
	e.mu.RLock()
	defer e.mu.RUnlock()

	chatID = strings.TrimSpace(chatID)
	result := map[string]interface{}{
		"chat_id":                     chatID,
		"active_scalping_quests":      0,
		"hold_streak":                 0,
		"unlock_cycles":               0,
		"recovery_mode":               "normal",
		"recovery_clean_cycles":       0,
		"recovery_entry_allowed":      true,
		"recovery_next_condition":     "",
		"state_drift_active":          false,
		"state_drift_positions":       0,
		"state_drift_count":           0,
		"state_drift_last_checked_at": "",
		"entry_gate_reason":           "",
		"entry_gate_type":             "none",
		"last_drift_repair_at":        "",
		"last_clean_reconcile_at":     "",
		"last_startup_reconcile":      "",
		"last_spot_unwind":            "",
		"last_hold_digest":            "",
		"runtime_no_fill_since":       "",
		"runtime_failure_streak":      0,
		"runtime_last_failure_at":     "",
		"provider_chain_configured":   0,
		"provider_chain_usable":       0,
	}
	if chatID == "" {
		return result
	}

	var (
		lastStartupReconcile  time.Time
		lastSpotUnwind        time.Time
		lastHoldDigest        time.Time
		latestFailureAt       time.Time
		holdStreak            int
		unlockCycles          int
		failureStreak         int
		noFillSince           time.Time
		activeScalping        int
		recoveryMode          string
		recoveryNextCondition string
		recoveryCleanCycles   int
		recoveryEntryAllowed  = true
		stateDriftActive      bool
		stateDriftPositions   int
		stateDriftLastChecked time.Time
		lastDriftRepair       time.Time
		lastCleanReconcile    time.Time
		entryGateReason       string
		entryGateType         string
		riskMaxDrawdown       float64
		aiWindowTotal         int
		aiWindowSuccess       int
		aiWindowErrors        int
		aiWindowTimeouts      int
		aiWindowParseFails    int
		aiWindowStarted       time.Time
		aiLastEventAt         time.Time
		aiLastCategory        string
		aiLastProvider        string
		aiLastSuccessProvider string
		aiLastError           string
		aiLastErrorAt         time.Time
		aiLastSuccessAt       time.Time
		aiCircuitUntil        time.Time
		aiCircuitReason       string
		aiCircuitTrips        int
		aiFailoverAttempts    int
		aiFailoverSuccesses   int
		aiFailoverFailures    int
	)

	for _, quest := range e.quests {
		if strings.TrimSpace(quest.Metadata["chat_id"]) != chatID {
			continue
		}
		if strings.TrimSpace(quest.Metadata["definition_id"]) == "scalping_execution" && quest.Status == QuestStatusActive {
			activeScalping++
		}

		cp := quest.Checkpoint
		if cp == nil {
			continue
		}
		holdStreak = maxInt(holdStreak, readQuestMetricInt(cp["runtime_hold_streak"]))
		unlockCycles = maxInt(unlockCycles, readQuestMetricInt(cp["runtime_unlock_cycles"]))
		failureStreak = maxInt(failureStreak, readQuestMetricInt(cp["runtime_failure_streak"]))
		recoveryCleanCycles = maxInt(recoveryCleanCycles, readQuestMetricInt(cp["recovery_clean_cycles"]))
		if mode := readQuestMetricString(cp["recovery_mode"]); mode != "" {
			recoveryMode = mode
		}
		if nextCondition := readQuestMetricString(cp["recovery_next_condition"]); nextCondition != "" {
			recoveryNextCondition = nextCondition
		}
		if _, exists := cp["recovery_entry_allowed"]; exists {
			recoveryEntryAllowed = readQuestMetricBool(cp["recovery_entry_allowed"])
		}
		if gateType := readQuestMetricString(cp["entry_gate_type"]); gateType != "" {
			entryGateType = gateType
		}
		if drawdown := readQuestMetricFloat(cp["risk_max_drawdown"]); drawdown > riskMaxDrawdown {
			riskMaxDrawdown = drawdown
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
		if ts := readCheckpointTime(cp["runtime_last_failure_at"]); ts.After(latestFailureAt) {
			latestFailureAt = ts
		}
		if ts := readCheckpointTime(cp["runtime_no_fill_since"]); ts.After(noFillSince) {
			noFillSince = ts
		}
		if readQuestMetricBool(cp["state_drift_active"]) {
			stateDriftActive = true
		}
		stateDriftPositions = maxInt(stateDriftPositions, readQuestMetricInt(cp["state_drift_positions"]))
		stateDriftPositions = maxInt(stateDriftPositions, readQuestMetricInt(cp["state_drift_count"]))
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
			entryGateReason = reason
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
	result["recovery_mode"] = recoveryMode
	result["recovery_clean_cycles"] = recoveryCleanCycles
	result["recovery_entry_allowed"] = recoveryEntryAllowed
	if strings.TrimSpace(recoveryNextCondition) != "" {
		result["recovery_next_condition"] = recoveryNextCondition
	}
	result["risk_max_drawdown"] = riskMaxDrawdown
	result["state_drift_active"] = stateDriftActive
	result["state_drift_positions"] = stateDriftPositions
	result["state_drift_count"] = stateDriftPositions
	if !stateDriftLastChecked.IsZero() {
		result["state_drift_last_checked_at"] = stateDriftLastChecked.Format(time.RFC3339)
	}
	if !lastDriftRepair.IsZero() {
		result["last_drift_repair_at"] = lastDriftRepair.Format(time.RFC3339)
	}
	if !lastCleanReconcile.IsZero() {
		result["last_clean_reconcile_at"] = lastCleanReconcile.Format(time.RFC3339)
	}
	if strings.TrimSpace(entryGateReason) == "" && e.isRiskLockEnabledLocked() {
		if len(e.riskLockReasons) > 0 {
			entryGateReason = e.riskLockReasons[0]
		} else {
			entryGateReason = "risk lock active"
		}
	}
	if entryGateType == "" {
		switch {
		case e.isRiskLockEnabledLocked():
			entryGateType = "risk_lock"
		case stateDriftActive:
			entryGateType = "state_drift"
		case aiCircuitUntil.After(time.Now().UTC()):
			entryGateType = "runtime_circuit"
		default:
			entryGateType = "none"
		}
	}
	if strings.TrimSpace(entryGateReason) != "" {
		result["entry_gate_reason"] = strings.TrimSpace(entryGateReason)
	}
	result["entry_gate_type"] = entryGateType
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
			reason = strings.TrimSpace(entryGateReason)
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
