package services

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestNewQuestProgressManager(t *testing.T) {
	engine := NewQuestEngine(NewInMemoryQuestStore())
	config := DefaultQuestProgressConfig()

	manager := NewQuestProgressManager(config, engine)

	assert.NotNil(t, manager)
	assert.NotNil(t, manager.engine)
	assert.NotNil(t, manager.lastUpdate)
	assert.NotNil(t, manager.milestones)
	assert.Equal(t, config.EnableMilestones, manager.config.EnableMilestones)
}

func TestDefaultQuestProgressConfig(t *testing.T) {
	config := DefaultQuestProgressConfig()

	assert.True(t, config.EnableMilestones)
	assert.Equal(t, []int{25, 50, 75, 90, 100}, config.MilestoneIntervals)
	assert.True(t, config.NotifyOnStart)
	assert.True(t, config.NotifyOnCompletion)
	assert.True(t, config.NotifyOnMilestone)
	assert.Equal(t, 5*time.Minute, config.ProgressUpdateInterval)
}

func TestQuestProgressManager_InitializeQuestProgress(t *testing.T) {
	engine := NewQuestEngine(NewInMemoryQuestStore())
	manager := NewQuestProgressManager(DefaultQuestProgressConfig(), engine)

	questID := "test-quest-1"
	targetCount := 100

	err := manager.InitializeQuestProgress(questID, targetCount)
	assert.NoError(t, err)

	// Check milestones were created
	milestones, exists := manager.milestones[questID]
	assert.True(t, exists)
	assert.Len(t, milestones, 5) // 25%, 50%, 75%, 90%, 100%

	// Check milestone values
	assert.Equal(t, 25, milestones[0].Percent)
	assert.Equal(t, 50, milestones[1].Percent)
	assert.Equal(t, 75, milestones[2].Percent)
	assert.Equal(t, 90, milestones[3].Percent)
	assert.Equal(t, 100, milestones[4].Percent)
}

func TestQuestProgressManager_shouldSendUpdate(t *testing.T) {
	engine := NewQuestEngine(NewInMemoryQuestStore())
	manager := NewQuestProgressManager(DefaultQuestProgressConfig(), engine)

	questID := "test-quest"

	// First update should always send
	assert.True(t, manager.shouldSendUpdate(questID))

	// Set last update time
	manager.lastUpdate[questID] = time.Now()

	// Should not send immediately
	assert.False(t, manager.shouldSendUpdate(questID))

	// Set last update to past the interval
	manager.lastUpdate[questID] = time.Now().Add(-6 * time.Minute)

	// Should send now
	assert.True(t, manager.shouldSendUpdate(questID))
}

func TestQuestProgressManager_checkMilestones(t *testing.T) {
	engine := NewQuestEngine(NewInMemoryQuestStore())
	manager := NewQuestProgressManager(DefaultQuestProgressConfig(), engine)

	questID := "test-quest"
	manager.InitializeQuestProgress(questID, 100)

	tests := []struct {
		name            string
		previousCount   int
		currentCount    int
		targetCount     int
		expectedResult  bool
		expectedPercent int
	}{
		{
			name:           "no milestone crossed",
			previousCount:  10,
			currentCount:   20,
			targetCount:    100,
			expectedResult: false,
		},
		{
			name:            "crossed 25%",
			previousCount:   20,
			currentCount:    30,
			targetCount:     100,
			expectedResult:  true,
			expectedPercent: 25,
		},
		{
			name:            "crossed 50%",
			previousCount:   40,
			currentCount:    55,
			targetCount:     100,
			expectedResult:  true,
			expectedPercent: 50,
		},
		{
			name:           "already passed milestone",
			previousCount:  60,
			currentCount:   70,
			targetCount:    100,
			expectedResult: false, // 50% already crossed
		},
		{
			name:            "crossed 100%",
			previousCount:   90,
			currentCount:    100,
			targetCount:     100,
			expectedResult:  true,
			expectedPercent: 100,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Reset milestones for each test
			manager.InitializeQuestProgress(questID, tt.targetCount)

			result := manager.checkMilestones(questID, tt.previousCount, tt.currentCount, tt.targetCount)
			if tt.expectedResult {
				assert.NotNil(t, result)
				assert.Equal(t, tt.expectedPercent, result.Percent)
				assert.NotNil(t, result.ReachedAt)
			} else {
				assert.Nil(t, result)
			}
		})
	}
}

func TestQuestProgressManager_calculateETA(t *testing.T) {
	engine := NewQuestEngine(NewInMemoryQuestStore())
	config := DefaultQuestProgressConfig()
	config.ProgressUpdateInterval = 10 * time.Minute
	manager := NewQuestProgressManager(config, engine)

	tests := []struct {
		name          string
		previousCount int
		currentCount  int
		targetCount   int
		expectedNil   bool
	}{
		{
			name:          "no progress",
			previousCount: 10,
			currentCount:  10,
			targetCount:   100,
			expectedNil:   true,
		},
		{
			name:          "already complete",
			previousCount: 90,
			currentCount:  100,
			targetCount:   100,
			expectedNil:   true,
		},
		{
			name:          "valid progress",
			previousCount: 50,
			currentCount:  60,
			targetCount:   100,
			expectedNil:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := manager.calculateETA(tt.previousCount, tt.currentCount, tt.targetCount)
			if tt.expectedNil {
				assert.Nil(t, result)
			} else {
				assert.NotNil(t, result)
				assert.True(t, result.After(time.Now()))
			}
		})
	}
}

func TestQuestProgressManager_formatTimeRemaining(t *testing.T) {
	engine := NewQuestEngine(NewInMemoryQuestStore())
	manager := NewQuestProgressManager(DefaultQuestProgressConfig(), engine)

	tests := []struct {
		name     string
		update   *QuestProgressUpdate
		expected string
	}{
		{
			name: "completed",
			update: &QuestProgressUpdate{
				Status: "completed",
			},
			expected: "completed",
		},
		{
			name: "failed",
			update: &QuestProgressUpdate{
				Status: "failed",
			},
			expected: "failed",
		},
		{
			name: "due now",
			update: &QuestProgressUpdate{
				Status: "active",
				ETA:    makeTimePtr(time.Now().Add(-1 * time.Minute)),
			},
			expected: "due now",
		},
		{
			name: "less than minute",
			update: &QuestProgressUpdate{
				Status: "active",
				ETA:    makeTimePtr(time.Now().Add(30 * time.Second)),
			},
			expected: "<1m",
		},
		{
			name: "minutes only",
			update: &QuestProgressUpdate{
				Status: "active",
				ETA:    makeTimePtr(time.Now().Add(30 * time.Minute)),
			},
			expected: "30m",
		},
		{
			name: "hours and minutes",
			update: &QuestProgressUpdate{
				Status: "active",
				ETA:    makeTimePtr(time.Now().Add(2*time.Hour + 30*time.Minute)),
			},
			expected: "2h 30m",
		},
		{
			name: "days",
			update: &QuestProgressUpdate{
				Status: "active",
				ETA:    makeTimePtr(time.Now().Add(48 * time.Hour)),
			},
			expected: "2d",
		},
		{
			name: "no ETA",
			update: &QuestProgressUpdate{
				Status: "active",
				ETA:    nil,
			},
			expected: "calculating...",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := manager.formatTimeRemaining(tt.update)
			// For time-based tests, check for pattern rather than exact value due to timing
			switch tt.name {
			case "minutes only":
				assert.Contains(t, result, "m")
			case "hours and minutes":
				assert.Contains(t, result, "h")
				assert.Contains(t, result, "m")
			case "days":
				assert.Contains(t, result, "d")
			default:
				assert.Equal(t, tt.expected, result)
			}
		})
	}
}

func TestQuestProgressManager_GetQuestProgressSummary(t *testing.T) {
	engine := NewQuestEngine(NewInMemoryQuestStore())
	manager := NewQuestProgressManager(DefaultQuestProgressConfig(), engine)

	// Create a quest through the engine with custom target
	quest, err := engine.CreateQuest("fund_growth", "12345", 100)
	assert.NoError(t, err)

	// Initialize progress tracking
	manager.InitializeQuestProgress(quest.ID, 100)

	// Get summary
	summary, err := manager.GetQuestProgressSummary(quest.ID)
	assert.NoError(t, err)
	assert.NotNil(t, summary)
	assert.Equal(t, quest.ID, summary.QuestID)
	assert.Equal(t, quest.Name, summary.QuestName)
	assert.Equal(t, 0, summary.CurrentCount)
	// Target comes from the quest itself, not InitializeQuestProgress
	assert.Equal(t, 100, summary.TargetCount)
}

func TestQuestProgressManager_ListAllProgress(t *testing.T) {
	engine := NewQuestEngine(NewInMemoryQuestStore())
	manager := NewQuestProgressManager(DefaultQuestProgressConfig(), engine)

	// Initially empty (no quests exist yet)
	progress, err := manager.ListAllProgress()
	assert.NoError(t, err)
	assert.Empty(t, progress)

	// Create actual quests in the engine
	quest1, err := engine.CreateQuest("fund_growth", "12345", 100)
	assert.NoError(t, err)
	quest2, err := engine.CreateQuest("fund_growth", "67890", 200)
	assert.NoError(t, err)

	// Initialize progress tracking for the quests
	manager.InitializeQuestProgress(quest1.ID, 100)
	manager.InitializeQuestProgress(quest2.ID, 200)

	// ListAllProgress returns quests that have milestones initialized
	progress, err = manager.ListAllProgress()
	assert.NoError(t, err)
	assert.Len(t, progress, 2)
}

// Helper function to get pointer to time
func makeTimePtr(t time.Time) *time.Time {
	return &t
}
