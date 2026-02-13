package services

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strconv"
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
)

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
	mu              sync.RWMutex
	quests          map[string]*Quest
	autonomousState map[string]*AutonomousState
	definitions     map[string]*QuestDefinition
	handlers        map[QuestType]QuestHandler
	store           QuestStore
	redis           *redis.Client
	stopCh          chan struct{}
	running         bool
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
		quests:          make(map[string]*Quest),
		autonomousState: make(map[string]*AutonomousState),
		definitions:     make(map[string]*QuestDefinition),
		handlers:        make(map[QuestType]QuestHandler),
		store:           store,
		redis:           redisClient,
		stopCh:          make(chan struct{}),
		chatIDForQuest:  make(map[string]int64),
	}

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
		return
	}
	e.running = true
	e.mu.Unlock()

	go e.schedulerLoop()
	log.Println("Quest engine started")
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
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-e.stopCh:
			return
		case <-ticker.C:
			e.tick()
		}
	}
}

// tick processes scheduled quests
func (e *QuestEngine) tick() {
	e.mu.RLock()
	defer e.mu.RUnlock()

	now := time.Now()
	for _, quest := range e.quests {
		if quest.Status != QuestStatusActive {
			continue
		}

		// Check if quest should execute based on cadence
		if e.shouldExecute(quest, now) {
			go e.executeQuest(quest)
		}
	}
}

func (e *QuestEngine) shouldExecute(quest *Quest, now time.Time) bool {
	minInterval := 1 * time.Minute

	if quest.LastExecutedAt != nil && now.Sub(*quest.LastExecutedAt) < minInterval {
		return false
	}

	switch quest.Cadence {
	case CadenceMicro:
		if now.Minute()%5 != 0 {
			return false
		}
		if quest.LastExecutedAt != nil {
			return now.Sub(*quest.LastExecutedAt) >= 5*time.Minute
		}
		return true
	case CadenceHourly:
		if now.Minute() != 0 {
			return false
		}
		if quest.LastExecutedAt != nil {
			return now.Sub(*quest.LastExecutedAt) >= 1*time.Hour
		}
		return true
	case CadenceDaily:
		if now.Hour() != 0 || now.Minute() != 0 {
			return false
		}
		if quest.LastExecutedAt != nil {
			return now.Sub(*quest.LastExecutedAt) >= 24*time.Hour
		}
		return true
	case CadenceWeekly:
		if now.Weekday() != time.Sunday || now.Hour() != 0 || now.Minute() != 0 {
			return false
		}
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

// executeQuest executes a single quest
func (e *QuestEngine) executeQuest(quest *Quest) {
	e.mu.RLock()
	handler, ok := e.handlers[quest.Type]
	e.mu.RUnlock()

	if !ok {
		log.Printf("No handler registered for quest type: %s", quest.Type)
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	lockKey := fmt.Sprintf("quest:lock:%s", quest.ID)
	locked := e.acquireLock(ctx, lockKey, 5*time.Minute)
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

	state := &AutonomousState{
		ChatID:    chatID,
		IsActive:  true,
		StartedAt: time.Now(),
	}

	// Create default quests for autonomous mode
	defaultQuests := []string{"market_scan", "funding_rate_scan", "portfolio_health"}
	for _, defID := range defaultQuests {
		quest, err := e.createQuestInternal(defID, chatID)
		if err != nil {
			log.Printf("Failed to create quest %s: %v", defID, err)
			continue
		}
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
	remaining := nextRun.Sub(time.Now())
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
