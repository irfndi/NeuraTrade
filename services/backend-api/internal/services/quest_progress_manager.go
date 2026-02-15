package services

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// QuestMilestone represents a milestone in a quest
type QuestMilestone struct {
	Percent       int                          `json:"percent"`
	Name          string                       `json:"name"`
	Description   string                       `json:"description"`
	ReachedAt     *time.Time                   `json:"reached_at,omitempty"`
	Notifications []QuestMilestoneNotification `json:"notifications"`
}

// QuestMilestoneNotification represents a notification for a milestone
type QuestMilestoneNotification struct {
	Type       string   `json:"type"` // email, telegram, webhook
	Template   string   `json:"template"`
	Recipients []string `json:"recipients,omitempty"`
}

// QuestProgressUpdate represents a comprehensive quest progress update
type QuestProgressUpdate struct {
	QuestID          string                 `json:"quest_id"`
	QuestName        string                 `json:"quest_name"`
	ChatID           int64                  `json:"chat_id"`
	PreviousCount    int                    `json:"previous_count"`
	CurrentCount     int                    `json:"current_count"`
	TargetCount      int                    `json:"target_count"`
	PercentComplete  int                    `json:"percent_complete"`
	Status           string                 `json:"status"`
	Milestones       []QuestMilestone       `json:"milestones,omitempty"`
	ReachedMilestone *QuestMilestone        `json:"reached_milestone,omitempty"`
	TimeRemaining    string                 `json:"time_remaining,omitempty"`
	ETA              *time.Time             `json:"eta,omitempty"`
	Checkpoint       map[string]interface{} `json:"checkpoint,omitempty"`
	Timestamp        time.Time              `json:"timestamp"`
}

// QuestProgressConfig holds configuration for quest progress tracking
type QuestProgressConfig struct {
	EnableMilestones       bool          `json:"enable_milestones"`
	MilestoneIntervals     []int         `json:"milestone_intervals"` // e.g., [25, 50, 75, 100]
	NotifyOnStart          bool          `json:"notify_on_start"`
	NotifyOnCompletion     bool          `json:"notify_on_completion"`
	NotifyOnMilestone      bool          `json:"notify_on_milestone"`
	ProgressUpdateInterval time.Duration `json:"progress_update_interval"`
}

// DefaultQuestProgressConfig returns default configuration
func DefaultQuestProgressConfig() QuestProgressConfig {
	return QuestProgressConfig{
		EnableMilestones:       true,
		MilestoneIntervals:     []int{25, 50, 75, 90, 100},
		NotifyOnStart:          true,
		NotifyOnCompletion:     true,
		NotifyOnMilestone:      true,
		ProgressUpdateInterval: 5 * time.Minute,
	}
}

// QuestProgressManager manages quest progress updates and milestone notifications
type QuestProgressManager struct {
	config     QuestProgressConfig
	engine     *QuestEngine
	mu         sync.RWMutex
	lastUpdate map[string]time.Time
	milestones map[string][]QuestMilestone
}

// NewQuestProgressManager creates a new quest progress manager
func NewQuestProgressManager(config QuestProgressConfig, engine *QuestEngine) *QuestProgressManager {
	return &QuestProgressManager{
		config:     config,
		engine:     engine,
		lastUpdate: make(map[string]time.Time),
		milestones: make(map[string][]QuestMilestone),
	}
}

// InitializeQuestProgress sets up progress tracking for a new quest
func (pm *QuestProgressManager) InitializeQuestProgress(questID string, targetCount int) error {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	// Create default milestones based on intervals
	if pm.config.EnableMilestones {
		milestones := make([]QuestMilestone, 0)
		for _, interval := range pm.config.MilestoneIntervals {
			if interval <= 100 {
				milestone := QuestMilestone{
					Percent:     interval,
					Name:        fmt.Sprintf("%d%% Complete", interval),
					Description: fmt.Sprintf("Quest is %d%% complete", interval),
				}
				milestones = append(milestones, milestone)
			}
		}
		pm.milestones[questID] = milestones
	}

	// Send start notification if enabled
	if pm.config.NotifyOnStart {
		pm.sendStartNotification(questID, targetCount)
	}

	return nil
}

// UpdateQuestProgress updates quest progress and triggers milestone notifications
func (pm *QuestProgressManager) UpdateQuestProgress(ctx context.Context, questID string, currentCount int, checkpoint map[string]interface{}) (*QuestProgressUpdate, error) {
	quest, err := pm.engine.GetQuest(questID)
	if err != nil {
		return nil, fmt.Errorf("quest not found: %w", err)
	}

	previousCount := quest.CurrentCount
	targetCount := quest.TargetCount

	percentComplete := 0
	if targetCount > 0 {
		percentComplete = (currentCount * 100) / targetCount
		if percentComplete > 100 {
			percentComplete = 100
		}
	}

	shouldUpdate := pm.shouldSendUpdate(questID)

	pm.mu.RLock()
	milestones := pm.milestones[questID]
	milestonesCopy := make([]QuestMilestone, len(milestones))
	copy(milestonesCopy, milestones)
	enableMilestones := pm.config.EnableMilestones
	notifyOnMilestone := pm.config.NotifyOnMilestone
	notifyOnCompletion := pm.config.NotifyOnCompletion
	pm.mu.RUnlock()

	var reachedMilestone *QuestMilestone
	if enableMilestones {
		reachedMilestone = pm.checkMilestones(questID, previousCount, currentCount, targetCount, milestonesCopy)
	}

	update := &QuestProgressUpdate{
		QuestID:          questID,
		QuestName:        quest.Name,
		PreviousCount:    previousCount,
		CurrentCount:     currentCount,
		TargetCount:      targetCount,
		PercentComplete:  percentComplete,
		Status:           string(quest.Status),
		Milestones:       milestonesCopy,
		ReachedMilestone: reachedMilestone,
		Checkpoint:       checkpoint,
		Timestamp:        time.Now(),
	}

	if currentCount > previousCount && currentCount < targetCount {
		eta := pm.calculateETA(previousCount, currentCount, targetCount)
		update.ETA = eta
	}

	if reachedMilestone != nil && notifyOnMilestone {
		pm.sendMilestoneNotification(update)
	} else if shouldUpdate {
		pm.sendProgressUpdate(update)
	}

	if previousCount < targetCount && currentCount >= targetCount && notifyOnCompletion {
		pm.sendCompletionNotification(update)
	}

	pm.mu.Lock()
	pm.lastUpdate[questID] = time.Now()
	pm.mu.Unlock()

	return update, nil
}

// shouldSendUpdate checks if enough time has passed since last update
func (pm *QuestProgressManager) shouldSendUpdate(questID string) bool {
	pm.mu.RLock()
	lastUpdate, exists := pm.lastUpdate[questID]
	interval := pm.config.ProgressUpdateInterval
	pm.mu.RUnlock()

	if !exists {
		return true
	}
	return time.Since(lastUpdate) >= interval
}

// checkMilestones checks if any milestones were reached
func (pm *QuestProgressManager) checkMilestones(questID string, previousCount, currentCount, targetCount int, milestones []QuestMilestone) *QuestMilestone {
	if len(milestones) == 0 || targetCount == 0 {
		return nil
	}

	previousPercent := (previousCount * 100) / targetCount
	currentPercent := (currentCount * 100) / targetCount

	for i := range milestones {
		milestone := &milestones[i]
		if previousPercent < milestone.Percent && currentPercent >= milestone.Percent {
			now := time.Now()
			milestone.ReachedAt = &now
			return milestone
		}
	}

	return nil
}

// calculateETA estimates time to completion
func (pm *QuestProgressManager) calculateETA(previousCount, currentCount, targetCount int) *time.Time {
	if previousCount >= currentCount || currentCount >= targetCount {
		return nil
	}

	// Simple linear projection based on recent progress
	progressRate := float64(currentCount - previousCount) // progress per check
	if progressRate <= 0 {
		return nil
	}

	remaining := float64(targetCount - currentCount)
	intervalsNeeded := remaining / progressRate
	etaDuration := time.Duration(intervalsNeeded) * pm.config.ProgressUpdateInterval
	eta := time.Now().Add(etaDuration)

	return &eta
}

// sendStartNotification sends notification when quest starts
func (pm *QuestProgressManager) sendStartNotification(questID string, targetCount int) {
	// Implementation would integrate with notification service
	// For now, this is a placeholder
}

// sendMilestoneNotification sends notification when milestone is reached
func (pm *QuestProgressManager) sendMilestoneNotification(update *QuestProgressUpdate) {
	if update.ReachedMilestone == nil || pm.engine == nil || pm.engine.notificationService == nil {
		return
	}

	// Get chatID from quest metadata or progress manager
	chatID := pm.getChatIDForQuest(update.QuestID)
	if chatID == 0 {
		return
	}

	progressNotif := QuestProgressNotification{
		QuestID:       update.QuestID,
		QuestName:     update.QuestName,
		Current:       update.CurrentCount,
		Target:        update.TargetCount,
		Percent:       update.PercentComplete,
		Status:        update.Status,
		TimeRemaining: fmt.Sprintf("Milestone: %s", update.ReachedMilestone.Name),
	}

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = pm.engine.notificationService.NotifyQuestProgress(ctx, chatID, progressNotif)
	}()
}

// sendProgressUpdate sends regular progress update notification
func (pm *QuestProgressManager) sendProgressUpdate(update *QuestProgressUpdate) {
	if pm.engine == nil || pm.engine.notificationService == nil {
		return
	}

	chatID := pm.getChatIDForQuest(update.QuestID)
	if chatID == 0 {
		return
	}

	progressNotif := QuestProgressNotification{
		QuestID:       update.QuestID,
		QuestName:     update.QuestName,
		Current:       update.CurrentCount,
		Target:        update.TargetCount,
		Percent:       update.PercentComplete,
		Status:        update.Status,
		TimeRemaining: pm.formatTimeRemaining(update),
	}

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = pm.engine.notificationService.NotifyQuestProgress(ctx, chatID, progressNotif)
	}()
}

// sendCompletionNotification sends notification when quest completes
func (pm *QuestProgressManager) sendCompletionNotification(update *QuestProgressUpdate) {
	if pm.engine == nil || pm.engine.notificationService == nil {
		return
	}

	chatID := pm.getChatIDForQuest(update.QuestID)
	if chatID == 0 {
		return
	}

	progressNotif := QuestProgressNotification{
		QuestID:       update.QuestID,
		QuestName:     update.QuestName,
		Current:       update.CurrentCount,
		Target:        update.TargetCount,
		Percent:       100,
		Status:        "completed",
		TimeRemaining: "completed",
	}

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = pm.engine.notificationService.NotifyQuestProgress(ctx, chatID, progressNotif)
	}()
}

// getChatIDForQuest retrieves chatID for a quest
func (pm *QuestProgressManager) getChatIDForQuest(questID string) int64 {
	// Get from engine's chatIDForQuest map
	if pm.engine != nil {
		pm.engine.mu.RLock()
		chatID := pm.engine.chatIDForQuest[questID]
		pm.engine.mu.RUnlock()
		return chatID
	}
	return 0
}

// formatTimeRemaining formats time remaining for display
func (pm *QuestProgressManager) formatTimeRemaining(update *QuestProgressUpdate) string {
	if update.Status == "completed" {
		return "completed"
	}
	if update.Status == "failed" {
		return "failed"
	}
	if update.ETA != nil {
		duration := time.Until(*update.ETA)
		if duration <= 0 {
			return "due now"
		}
		if duration < time.Minute {
			return "<1m"
		}
		if duration < time.Hour {
			return fmt.Sprintf("%dm", int(duration.Minutes()))
		}
		if duration < 24*time.Hour {
			hours := int(duration.Hours())
			mins := int(duration.Minutes()) % 60
			if mins > 0 {
				return fmt.Sprintf("%dh %dm", hours, mins)
			}
			return fmt.Sprintf("%dh", hours)
		}
		days := int(duration.Hours() / 24)
		return fmt.Sprintf("%dd", days)
	}
	return "calculating..."
}

// GetQuestProgressSummary returns a summary of quest progress
func (pm *QuestProgressManager) GetQuestProgressSummary(questID string) (*QuestProgressUpdate, error) {
	quest, err := pm.engine.GetQuest(questID)
	if err != nil {
		return nil, err
	}

	percentComplete := 0
	if quest.TargetCount > 0 {
		percentComplete = (quest.CurrentCount * 100) / quest.TargetCount
	}

	pm.mu.RLock()
	milestones := pm.milestones[questID]
	pm.mu.RUnlock()

	milestonesCopy := make([]QuestMilestone, len(milestones))
	copy(milestonesCopy, milestones)

	summary := &QuestProgressUpdate{
		QuestID:         questID,
		QuestName:       quest.Name,
		CurrentCount:    quest.CurrentCount,
		TargetCount:     quest.TargetCount,
		PercentComplete: percentComplete,
		Status:          string(quest.Status),
		Milestones:      milestonesCopy,
		Timestamp:       time.Now(),
	}

	return summary, nil
}

// ListAllProgress returns progress for all active quests
func (pm *QuestProgressManager) ListAllProgress() ([]*QuestProgressUpdate, error) {
	pm.mu.RLock()
	questIDs := make([]string, 0, len(pm.milestones))
	for questID := range pm.milestones {
		questIDs = append(questIDs, questID)
	}
	pm.mu.RUnlock()

	updates := make([]*QuestProgressUpdate, 0)

	for _, questID := range questIDs {
		summary, err := pm.GetQuestProgressSummary(questID)
		if err != nil {
			continue
		}
		updates = append(updates, summary)
	}

	return updates, nil
}
