package ai

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

var fixedLearningFixtureTime = time.Date(1970, time.January, 1, 0, 0, 0, 0, time.UTC)

func newTestLearningSystem(t *testing.T) *InMemoryLearningSystem {
	t.Helper()
	t.Setenv("NEURATRADE_AI_LEARNING_DATA_DIR", filepath.Join(t.TempDir(), "ai_learning"))
	return NewInMemoryLearningSystem()
}

func TestNewInMemoryLearningSystem(t *testing.T) {
	// Use temp directory for testing
	tmpDir := filepath.Join(os.TempDir(), "ai_learning_test")
	defer os.RemoveAll(tmpDir)

	ls := newTestLearningSystem(t)

	if ls == nil {
		t.Fatal("Expected non-nil learning system")
	}
	if ls.decisions == nil {
		t.Error("Expected decisions map to be initialized")
	}
	if ls.optimalDB == nil {
		t.Error("Expected optimalDB to be initialized")
	}
}

func TestSanitizeLearningDataDir(t *testing.T) {
	configured := filepath.Join(t.TempDir(), "ai_learning")
	resolved, ok := sanitizeLearningDataDir(configured)
	if !ok {
		t.Fatal("expected configured temp dir to be accepted")
	}
	if resolved != configured {
		t.Fatalf("expected resolved dir %q, got %q", configured, resolved)
	}

	unsafePath := filepath.Join("..", "..", "etc", "neuratrade")
	if _, ok := sanitizeLearningDataDir(unsafePath); ok {
		t.Fatal("expected path traversal candidate to be rejected")
	}

	validDottedPath := filepath.Join("data..v2", "ai_learning")
	resolved, ok = sanitizeLearningDataDir(validDottedPath)
	if !ok {
		t.Fatal("expected dotted directory name to be accepted")
	}
	if resolved != validDottedPath {
		t.Fatalf("expected resolved dotted dir %q, got %q", validDottedPath, resolved)
	}
}

func TestInMemoryLearningSystem_RecordDecision(t *testing.T) {
	ls := newTestLearningSystem(t)

	record := &DecisionRecord{
		ID:        "test-decision-1",
		Timestamp: fixedLearningFixtureTime,
		Strategy:  "scalping",
		MarketState: MarketState{
			Symbol:    "BTC/USDT",
			Price:     45000.00,
			Timestamp: fixedLearningFixtureTime,
		},
		Decision: TradingDecision{
			ID:         "test-decision-1",
			Action:     ActionBuy,
			Symbol:     "BTC/USDT",
			Confidence: 0.85,
		},
		Reasoning:  "Test reasoning",
		Confidence: 0.85,
	}

	err := ls.RecordDecision(context.Background(), record)

	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	// Verify decision was stored
	if len(ls.decisions) != 1 {
		t.Errorf("Expected 1 decision, got %d", len(ls.decisions))
	}

	stored, ok := ls.decisions["test-decision-1"]
	if !ok {
		t.Fatal("Expected decision to be stored")
	}
	if stored.Strategy != "scalping" {
		t.Errorf("Expected strategy 'scalping', got '%s'", stored.Strategy)
	}
}

func TestInMemoryLearningSystem_RecordDecision_Concurrent(t *testing.T) {
	ls := newTestLearningSystem(t)

	// Record multiple decisions concurrently
	done := make(chan bool)
	for i := 0; i < 10; i++ {
		go func(id int) {
			record := &DecisionRecord{
				ID:        string(rune('A' + id)),
				Timestamp: fixedLearningFixtureTime,
				Strategy:  "scalping",
				MarketState: MarketState{
					Symbol:    "BTC/USDT",
					Timestamp: fixedLearningFixtureTime,
				},
			}
			ls.RecordDecision(context.Background(), record)
			done <- true
		}(i)
	}

	// Wait for all goroutines
	for i := 0; i < 10; i++ {
		<-done
	}

	if len(ls.decisions) != 10 {
		t.Errorf("Expected 10 decisions, got %d", len(ls.decisions))
	}
}

func TestInMemoryLearningSystem_GetSimilarDecisions(t *testing.T) {
	ls := newTestLearningSystem(t)

	// Add decisions with different symbols and outcomes
	decisions := []*DecisionRecord{
		{
			ID:        "dec-1",
			Timestamp: time.Now().Add(-2 * time.Hour),
			Strategy:  "scalping",
			MarketState: MarketState{
				Symbol:    "BTC/USDT",
				Timestamp: time.Now(),
			},
			Outcome: "win",
		},
		{
			ID:        "dec-2",
			Timestamp: time.Now().Add(-1 * time.Hour),
			Strategy:  "scalping",
			MarketState: MarketState{
				Symbol:    "BTC/USDT",
				Timestamp: time.Now(),
			},
			Outcome: "loss",
		},
		{
			ID:        "dec-3",
			Timestamp: time.Now(),
			Strategy:  "scalping",
			MarketState: MarketState{
				Symbol:    "ETH/USDT",
				Timestamp: time.Now(),
			},
			Outcome: "win",
		},
		{
			ID:        "dec-4",
			Timestamp: time.Now(),
			Strategy:  "scalping",
			MarketState: MarketState{
				Symbol:    "BTC/USDT",
				Timestamp: time.Now(),
			},
			Outcome: "", // No outcome - should not be returned
		},
	}

	for _, d := range decisions {
		ls.decisions[d.ID] = d
	}

	// Get similar decisions for BTC/USDT
	similar, err := ls.GetSimilarDecisions(context.Background(), "BTC/USDT", 10)

	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	// Should return only 2 (dec-1, dec-2) since dec-4 has no outcome
	if len(similar) != 2 {
		t.Errorf("Expected 2 similar decisions, got %d", len(similar))
	}

	// Verify results are sorted by most recent
	if len(similar) >= 2 {
		if similar[0].Timestamp.Before(similar[1].Timestamp) {
			t.Error("Expected decisions sorted by most recent first")
		}
	}
}

func TestInMemoryLearningSystem_GetSimilarDecisions_Limit(t *testing.T) {
	ls := newTestLearningSystem(t)

	// Add 5 decisions for BTC/USDT
	for i := 0; i < 5; i++ {
		ls.decisions[string(rune('A'+i))] = &DecisionRecord{
			ID:        string(rune('A' + i)),
			Timestamp: time.Now().Add(time.Duration(i) * time.Hour),
			Strategy:  "scalping",
			MarketState: MarketState{
				Symbol:    "BTC/USDT",
				Timestamp: time.Now(),
			},
			Outcome: "win",
		}
	}

	// Get with limit 3
	similar, err := ls.GetSimilarDecisions(context.Background(), "BTC/USDT", 3)

	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	if len(similar) != 3 {
		t.Errorf("Expected 3 decisions (limited), got %d", len(similar))
	}
}

func TestInMemoryLearningSystem_RecordOutcome(t *testing.T) {
	ls := newTestLearningSystem(t)

	// First record a decision
	ls.decisions["test-id"] = &DecisionRecord{
		ID:        "test-id",
		Timestamp: fixedLearningFixtureTime,
		Strategy:  "scalping",
		MarketState: MarketState{
			Symbol:    "BTC/USDT",
			Timestamp: fixedLearningFixtureTime,
		},
	}

	// Record outcome
	outcome := &TradeOutcome{
		DecisionID: "test-id",
		Result:     "win",
		PnL:        100.50,
	}

	err := ls.RecordOutcome(context.Background(), "test-id", outcome)

	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	// Verify outcome was recorded
	decision := ls.decisions["test-id"]
	if decision.Outcome != "win" {
		t.Errorf("Expected outcome 'win', got '%s'", decision.Outcome)
	}
	if decision.PnL != 100.50 {
		t.Errorf("Expected PnL 100.50, got %f", decision.PnL)
	}
	if decision.CompletedAt == nil {
		t.Error("Expected CompletedAt to be set")
	}
}

func TestInMemoryLearningSystem_RecordOutcome_NotFound(t *testing.T) {
	ls := newTestLearningSystem(t)

	outcome := &TradeOutcome{
		DecisionID: "nonexistent",
		Result:     "win",
	}

	err := ls.RecordOutcome(context.Background(), "nonexistent", outcome)

	if err == nil {
		t.Error("Expected error for non-existent decision")
	}
}

func TestInMemoryLearningSystem_GetOptimalStrategy(t *testing.T) {
	ls := newTestLearningSystem(t)

	// Add optimal strategy
	key := "scalping_BTC/USDT"
	ls.optimalDB.strategies[key] = &OptimalStrategy{
		Strategy:  "scalping",
		Symbol:    "BTC/USDT",
		WinRate:   0.65,
		AvgProfit: 50.0,
		UpdatedAt: time.Now(),
	}

	strategy := ls.GetOptimalStrategy("scalping", "BTC/USDT")

	if strategy == nil {
		t.Fatal("Expected non-nil strategy")
	}
	if strategy.WinRate != 0.65 {
		t.Errorf("Expected win rate 0.65, got %f", strategy.WinRate)
	}
}

func TestInMemoryLearningSystem_GetOptimalStrategy_NotFound(t *testing.T) {
	ls := newTestLearningSystem(t)

	strategy := ls.GetOptimalStrategy("nonexistent", "XXX/USDT")

	if strategy != nil {
		t.Error("Expected nil for non-existent strategy")
	}
}

func TestInMemoryLearningSystem_GenerateInsights(t *testing.T) {
	ls := newTestLearningSystem(t)

	// Add decisions with outcomes
	ls.decisions["dec-1"] = &DecisionRecord{
		ID:        "dec-1",
		Timestamp: time.Now(),
		Strategy:  "scalping",
		MarketState: MarketState{
			Symbol:    "BTC/USDT",
			Timestamp: time.Now(),
		},
		Outcome: "win",
		PnL:     100.0,
	}
	ls.decisions["dec-2"] = &DecisionRecord{
		ID:        "dec-2",
		Timestamp: time.Now(),
		Strategy:  "scalping",
		MarketState: MarketState{
			Symbol:    "BTC/USDT",
			Timestamp: time.Now(),
		},
		Outcome: "win",
		PnL:     50.0,
	}
	ls.decisions["dec-3"] = &DecisionRecord{
		ID:        "dec-3",
		Timestamp: time.Now(),
		Strategy:  "scalping",
		MarketState: MarketState{
			Symbol:    "BTC/USDT",
			Timestamp: time.Now(),
		},
		Outcome: "loss",
		PnL:     -75.0,
	}

	insights := ls.GenerateInsights("BTC/USDT")

	if insights == nil {
		t.Fatal("Expected non-nil insights")
	}
	if insights.Symbol != "BTC/USDT" {
		t.Errorf("Expected symbol 'BTC/USDT', got '%s'", insights.Symbol)
	}
	if insights.TotalTrades != 3 {
		t.Errorf("Expected 3 total trades, got %d", insights.TotalTrades)
	}
	// Win rate should be 2/3 = 66.67%
	if insights.WinRate < 66 || insights.WinRate > 67 {
		t.Errorf("Expected win rate ~66.67%%, got %f%%", insights.WinRate)
	}
	// Total PnL = 100 + 50 - 75 = 75
	if insights.TotalPnL != 75 {
		t.Errorf("Expected total PnL 75, got %f", insights.TotalPnL)
	}
}

func TestInMemoryLearningSystem_GenerateInsights_NoHistory(t *testing.T) {
	ls := newTestLearningSystem(t)

	insights := ls.GenerateInsights("UNKNOWN/USDT")

	if insights == nil {
		t.Fatal("Expected non-nil insights even with no history")
	}
	if insights.TotalTrades != 0 {
		t.Errorf("Expected 0 trades, got %d", insights.TotalTrades)
	}
	if insights.Message == "" {
		t.Error("Expected message for no history")
	}
}

func TestInMemoryLearningSystem_generateInsightMessage(t *testing.T) {
	ls := &InMemoryLearningSystem{}

	tests := []struct {
		winRate   float64
		totalPnL  float64
		expectMsg string
	}{
		{70.0, 100.0, "Strong performance"},
		{55.0, 50.0, "Good performance"},
		{35.0, -50.0, "needs improvement"},
		{45.0, 0.0, "neutral"},
	}

	for _, tt := range tests {
		msg := ls.generateInsightMessage(tt.winRate, tt.totalPnL)
		if !contains(msg, tt.expectMsg) {
			t.Errorf("Expected message to contain '%s', got '%s'", tt.expectMsg, msg)
		}
	}
}

func TestInMemoryLearningSystem_generateRecommendation(t *testing.T) {
	ls := &InMemoryLearningSystem{}

	tests := []struct {
		winRate float64
		expect  string
	}{
		{75.0, "Increase position"},
		{60.0, "Continue current"},
		{45.0, "Review and adjust"},
		{30.0, "pausing"},
	}

	for _, tt := range tests {
		rec := ls.generateRecommendation(tt.winRate)
		if !contains(rec, tt.expect) {
			t.Errorf("Expected recommendation to contain '%s', got '%s'", tt.expect, rec)
		}
	}
}

func TestInMemoryLearningSystem_updateOptimalStrategy(t *testing.T) {
	ls := newTestLearningSystem(t)

	// Record a winning decision
	decision := &DecisionRecord{
		ID:        "test-id",
		Timestamp: time.Now(),
		Strategy:  "scalping",
		MarketState: MarketState{
			Symbol:    "BTC/USDT",
			Timestamp: time.Now(),
		},
		Outcome: "win",
		PnL:     100.0,
	}

	ls.updateOptimalStrategy(decision)

	// Verify optimal strategy was created
	strategy := ls.GetOptimalStrategy("scalping", "BTC/USDT")
	if strategy == nil {
		t.Fatal("Expected optimal strategy to be created")
	}
	if strategy.Strategy != "scalping" {
		t.Errorf("Expected strategy 'scalping', got '%s'", strategy.Strategy)
	}
}
