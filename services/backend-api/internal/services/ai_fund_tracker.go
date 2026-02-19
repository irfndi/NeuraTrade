package services

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/irfndi/neuratrade/internal/ai/llm"
	"github.com/irfndi/neuratrade/internal/ccxt"
)

type AIFundTrackerConfig struct {
	UpdateInterval  time.Duration `json:"update_interval"`
	TargetValue     float64       `json:"target_value"`
	MinMilestoneGap float64       `json:"min_milestone_gap"`
	EnableAI        bool          `json:"enable_ai"`
	LLMModel        string        `json:"llm_model"`
	MaxHistoryDays  int           `json:"max_history_days"`
}

func DefaultAIFundTrackerConfig() AIFundTrackerConfig {
	return AIFundTrackerConfig{
		UpdateInterval:  5 * time.Minute,
		TargetValue:     1000.0,
		MinMilestoneGap: 0.05,
		EnableAI:        true,
		LLMModel:        "default",
		MaxHistoryDays:  30,
	}
}

type FundSnapshot struct {
	Timestamp     time.Time       `json:"timestamp"`
	TotalValue    float64         `json:"total_value"`
	USDTBalance   float64         `json:"usdt_balance"`
	Positions     []PositionValue `json:"positions"`
	UnrealizedPnL float64         `json:"unrealized_pnl"`
	RealizedPnL   float64         `json:"realized_pnl"`
}

type PositionValue struct {
	Symbol        string  `json:"symbol"`
	Side          string  `json:"side"`
	Size          float64 `json:"size"`
	EntryPrice    float64 `json:"entry_price"`
	CurrentPrice  float64 `json:"current_price"`
	UnrealizedPnL float64 `json:"unrealized_pnl"`
}

type AIMilestone struct {
	ID            string     `json:"id"`
	TargetValue   float64    `json:"target_value"`
	TargetPercent float64    `json:"target_percent"`
	Description   string     `json:"description"`
	Reasoning     string     `json:"reasoning"`
	CreatedAt     time.Time  `json:"created_at"`
	AchievedAt    *time.Time `json:"achieved_at,omitempty"`
	Status        string     `json:"status"`
}

type AIFundTracker struct {
	config      AIFundTrackerConfig
	questEngine *QuestEngine
	ccxtService interface {
		FetchBalance(ctx context.Context, exchange string) (*ccxt.BalanceResponse, error)
	}
	llmClient llm.Client
	exchange  string

	currentValue float64
	startValue   float64
	milestones   []AIMilestone
	history      []FundSnapshot
	performance  *FundPerformance
	mu           sync.RWMutex

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

type FundPerformance struct {
	TotalReturn      float64 `json:"total_return"`
	TotalReturnPct   float64 `json:"total_return_pct"`
	DailyReturn      float64 `json:"daily_return"`
	WeeklyReturn     float64 `json:"weekly_return"`
	SharpeRatio      float64 `json:"sharpe_ratio"`
	MaxDrawdown      float64 `json:"max_drawdown"`
	WinRate          float64 `json:"win_rate"`
	TradesCount      int     `json:"trades_count"`
	ProfitableTrades int     `json:"profitable_trades"`
}

func NewAIFundTracker(
	config AIFundTrackerConfig,
	questEngine *QuestEngine,
	ccxtService interface {
		FetchBalance(ctx context.Context, exchange string) (*ccxt.BalanceResponse, error)
	},
	llmClient llm.Client,
	exchange string,
) *AIFundTracker {
	return &AIFundTracker{
		config:      config,
		questEngine: questEngine,
		ccxtService: ccxtService,
		llmClient:   llmClient,
		exchange:    exchange,
		milestones:  []AIMilestone{},
		history:     []FundSnapshot{},
		performance: &FundPerformance{},
	}
}

func (t *AIFundTracker) Start(ctx context.Context) error {
	t.ctx, t.cancel = context.WithCancel(ctx)

	snapshot, err := t.takeSnapshot(ctx)
	if err != nil {
		return fmt.Errorf("failed to take initial snapshot: %w", err)
	}
	t.startValue = snapshot.TotalValue
	t.currentValue = snapshot.TotalValue
	t.history = append(t.history, *snapshot)

	if t.config.EnableAI && t.llmClient != nil {
		milestones, err := t.generateAIMilestones(ctx, snapshot.TotalValue)
		if err != nil {
			log.Printf("[AI-FUND] Failed to generate AI milestones, using defaults: %v", err)
			t.milestones = t.generateDefaultMilestones(snapshot.TotalValue)
		} else {
			t.milestones = milestones
		}
	} else {
		t.milestones = t.generateDefaultMilestones(snapshot.TotalValue)
	}

	t.wg.Add(1)
	go t.runTrackingLoop()

	log.Printf("[AI-FUND] Started tracking: start_value=%.2f, target=%.2f, milestones=%d",
		t.startValue, t.config.TargetValue, len(t.milestones))
	return nil
}

func (t *AIFundTracker) Stop() {
	if t.cancel != nil {
		t.cancel()
	}
	t.wg.Wait()
	log.Printf("[AI-FUND] Stopped")
}

func (t *AIFundTracker) runTrackingLoop() {
	defer t.wg.Done()

	ticker := time.NewTicker(t.config.UpdateInterval)
	defer ticker.Stop()

	for {
		select {
		case <-t.ctx.Done():
			return
		case <-ticker.C:
			if err := t.updateFundValue(t.ctx); err != nil {
				log.Printf("[AI-FUND] Update failed: %v", err)
			}
		}
	}
}

func (t *AIFundTracker) updateFundValue(ctx context.Context) error {
	snapshot, err := t.takeSnapshot(ctx)
	if err != nil {
		return err
	}

	t.mu.Lock()
	t.currentValue = snapshot.TotalValue
	t.history = append(t.history, *snapshot)
	t.mu.Unlock()

	t.checkMilestones(snapshot.TotalValue)

	t.updateQuestProgress(snapshot.TotalValue)

	if t.config.EnableAI && t.llmClient != nil {
		go t.analyzeProgress(ctx, snapshot)
	}

	log.Printf("[AI-FUND] Updated: value=%.2f, progress=%.1f%%",
		snapshot.TotalValue, (snapshot.TotalValue/t.config.TargetValue)*100)

	return nil
}

func (t *AIFundTracker) takeSnapshot(ctx context.Context) (*FundSnapshot, error) {
	balance, err := t.ccxtService.FetchBalance(ctx, t.exchange)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch balance: %w", err)
	}

	usdtBalance := 0.0
	if balance.Total != nil {
		usdtBalance = balance.Total["USDT"]
	}

	snapshot := &FundSnapshot{
		Timestamp:   time.Now(),
		TotalValue:  usdtBalance,
		USDTBalance: usdtBalance,
		Positions:   []PositionValue{},
	}

	return snapshot, nil
}

func (t *AIFundTracker) checkMilestones(currentValue float64) {
	t.mu.Lock()
	defer t.mu.Unlock()

	for i := range t.milestones {
		if t.milestones[i].Status == "pending" && currentValue >= t.milestones[i].TargetValue {
			now := time.Now()
			t.milestones[i].Status = "achieved"
			t.milestones[i].AchievedAt = &now
			log.Printf("[AI-FUND] MILESTONE ACHIEVED: %s (%.2f)",
				t.milestones[i].Description, t.milestones[i].TargetValue)
		}
	}
}

func (t *AIFundTracker) updateQuestProgress(value float64) {
	if t.questEngine == nil {
		return
	}

	progress := int((value / t.config.TargetValue) * 100)

	quest, err := t.questEngine.GetQuest("fund_growth")
	if err != nil {
		return
	}

	if quest != nil {
		if quest.Checkpoint == nil {
			quest.Checkpoint = make(map[string]interface{})
		}
		quest.Checkpoint["current_value"] = value
		quest.Checkpoint["target_value"] = t.config.TargetValue
		quest.Checkpoint["progress_percent"] = progress
		quest.CurrentCount = progress
	}
}

func (t *AIFundTracker) generateDefaultMilestones(currentValue float64) []AIMilestone {
	target := t.config.TargetValue
	return []AIMilestone{
		{ID: "m25", TargetValue: target * 0.25, TargetPercent: 25, Description: "25% of target", Status: "pending", CreatedAt: time.Now()},
		{ID: "m50", TargetValue: target * 0.50, TargetPercent: 50, Description: "50% of target", Status: "pending", CreatedAt: time.Now()},
		{ID: "m75", TargetValue: target * 0.75, TargetPercent: 75, Description: "75% of target", Status: "pending", CreatedAt: time.Now()},
		{ID: "m90", TargetValue: target * 0.90, TargetPercent: 90, Description: "90% of target", Status: "pending", CreatedAt: time.Now()},
		{ID: "m100", TargetValue: target, TargetPercent: 100, Description: "Target reached!", Status: "pending", CreatedAt: time.Now()},
	}
}

func (t *AIFundTracker) generateAIMilestones(ctx context.Context, currentValue float64) ([]AIMilestone, error) {
	historyJSON, _ := json.Marshal(t.getRecentHistory(24))

	prompt := fmt.Sprintf(`You are managing a trading fund growth milestone system.

Current Fund Value: %.2f
Target Value: %.2f
Progress: %.1f%%

Recent Performance (last 24h):
%s

Task: Generate optimal milestones for reaching the target.
Consider:
1. Current progress velocity
2. Market conditions
3. Risk-adjusted targets
4. Realistic timeframes

Return JSON array:
[
  {
    "target_value": 250.00,
    "description": "Quarter milestone - establish consistent trading",
    "reasoning": "First milestone focuses on stability"
  }
]

Generate 3-5 milestones. Respond with JSON only.`,
		currentValue, t.config.TargetValue,
		(currentValue/t.config.TargetValue)*100,
		string(historyJSON))

	req := &llm.CompletionRequest{
		Messages: []llm.Message{
			{Role: "system", Content: "You are an AI fund manager. Respond with valid JSON only."},
			{Role: "user", Content: prompt},
		},
		MaxTokens: 1000,
	}

	resp, err := t.llmClient.Complete(ctx, req)
	if err != nil {
		return nil, err
	}

	var aiMilestones []struct {
		TargetValue float64 `json:"target_value"`
		Description string  `json:"description"`
		Reasoning   string  `json:"reasoning"`
	}

	if err := json.Unmarshal([]byte(resp.Message.Content), &aiMilestones); err != nil {
		return nil, fmt.Errorf("failed to parse AI milestones: %w", err)
	}

	milestones := make([]AIMilestone, len(aiMilestones))
	for i, m := range aiMilestones {
		milestones[i] = AIMilestone{
			ID:            fmt.Sprintf("ai_m%d", i+1),
			TargetValue:   m.TargetValue,
			TargetPercent: (m.TargetValue / t.config.TargetValue) * 100,
			Description:   m.Description,
			Reasoning:     m.Reasoning,
			Status:        "pending",
			CreatedAt:     time.Now(),
		}
	}

	return milestones, nil
}

func (t *AIFundTracker) analyzeProgress(ctx context.Context, snapshot *FundSnapshot) {
	if len(t.history) < 5 {
		return
	}

	recentHistory := t.getRecentHistory(24)
	performance := t.calculatePerformance(recentHistory)

	t.mu.Lock()
	t.performance = performance
	t.mu.Unlock()

	if performance.DailyReturn < -0.05 {
		log.Printf("[AI-FUND] WARNING: Daily return %.2f%% below threshold", performance.DailyReturn*100)
	}

	if performance.MaxDrawdown > 0.10 {
		log.Printf("[AI-FUND] ALERT: Max drawdown %.2f%% exceeds 10%% threshold", performance.MaxDrawdown*100)
	}
}

func (t *AIFundTracker) getRecentHistory(hours int) []FundSnapshot {
	t.mu.RLock()
	defer t.mu.RUnlock()

	cutoff := time.Now().Add(-time.Duration(hours) * time.Hour)
	var recent []FundSnapshot
	for _, h := range t.history {
		if h.Timestamp.After(cutoff) {
			recent = append(recent, h)
		}
	}
	return recent
}

func (t *AIFundTracker) calculatePerformance(history []FundSnapshot) *FundPerformance {
	if len(history) < 2 {
		return &FundPerformance{}
	}

	perf := &FundPerformance{}
	startVal := history[0].TotalValue
	endVal := history[len(history)-1].TotalValue

	perf.TotalReturn = endVal - startVal
	perf.TotalReturnPct = (perf.TotalReturn / startVal) * 100

	if len(history) >= 24 {
		dayStart := history[0].TotalValue
		dayEnd := history[len(history)-1].TotalValue
		perf.DailyReturn = (dayEnd - dayStart) / dayStart
	}

	var maxVal float64
	for _, h := range history {
		if h.TotalValue > maxVal {
			maxVal = h.TotalValue
		}
	}
	if maxVal > 0 {
		perf.MaxDrawdown = (maxVal - endVal) / maxVal
	}

	return perf
}

func (t *AIFundTracker) GetStatus() map[string]interface{} {
	t.mu.RLock()
	defer t.mu.RUnlock()

	achieved := 0
	for _, m := range t.milestones {
		if m.Status == "achieved" {
			achieved++
		}
	}

	return map[string]interface{}{
		"current_value":       t.currentValue,
		"start_value":         t.startValue,
		"target_value":        t.config.TargetValue,
		"progress_percent":    (t.currentValue / t.config.TargetValue) * 100,
		"milestones_total":    len(t.milestones),
		"milestones_achieved": achieved,
		"milestones":          t.milestones,
		"performance":         t.performance,
		"history_count":       len(t.history),
	}
}

func (t *AIFundTracker) AdjustTarget(ctx context.Context, newTarget float64, reason string) error {
	t.mu.Lock()
	t.config.TargetValue = newTarget
	t.mu.Unlock()

	if t.config.EnableAI && t.llmClient != nil {
		milestones, err := t.generateAIMilestones(ctx, t.currentValue)
		if err != nil {
			t.milestones = t.generateDefaultMilestones(t.currentValue)
		} else {
			t.milestones = milestones
		}
	} else {
		t.milestones = t.generateDefaultMilestones(t.currentValue)
	}

	log.Printf("[AI-FUND] Target adjusted to %.2f (reason: %s)", newTarget, reason)
	return nil
}

func (t *AIFundTracker) GetAIRecommendation(ctx context.Context) (string, error) {
	if t.llmClient == nil {
		return "AI not configured", nil
	}

	status := t.GetStatus()
	statusJSON, _ := json.Marshal(status)

	prompt := fmt.Sprintf(`Analyze fund growth progress and provide recommendations.

Current Status:
%s

Provide:
1. Progress assessment (1-2 sentences)
2. Strategy recommendation (what to do next)
3. Risk assessment (any concerns)
4. Target adjustment suggestion (if needed)

Keep response under 200 words.`, string(statusJSON))

	req := &llm.CompletionRequest{
		Messages: []llm.Message{
			{Role: "system", Content: "You are a fund management advisor. Be concise and actionable."},
			{Role: "user", Content: prompt},
		},
		MaxTokens: 500,
	}

	resp, err := t.llmClient.Complete(ctx, req)
	if err != nil {
		return "", err
	}

	return resp.Message.Content, nil
}

func (t *AIFundTracker) GetValue() float64 {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.currentValue
}
