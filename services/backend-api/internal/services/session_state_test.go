package services

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/irfndi/neuratrade/internal/ai/llm"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSessionState_Serialization(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	state := &SessionState{
		ID:        "sess_test_123",
		Status:    SessionStatusActive,
		QuestID:   "quest_456",
		Symbol:    "BTC/USDT",
		CreatedAt: now,
		UpdatedAt: now,
		ConversationHistory: []llm.Message{
			{Role: llm.RoleUser, Content: "Analyze BTC"},
			{Role: llm.RoleAssistant, Content: "Analyzing..."},
		},
		ToolCallsMade: []ToolCallRecord{
			{
				ID:        "tc_1",
				Name:      "get_price",
				Arguments: json.RawMessage(`{"symbol":"BTC/USDT"}`),
				Result:    json.RawMessage(`{"price":50000}`),
				Timestamp: now,
			},
		},
		LoadedSkills: []string{"scalping", "arbitrage"},
		MarketSnapshot: &MarketContextSnapshot{
			Symbol:       "BTC/USDT",
			CurrentPrice: 50000.0,
			Volatility:   0.02,
			SnapshotTime: now,
		},
		IterationCount: 3,
		Metadata: map[string]string{
			"source": "telegram",
		},
	}

	serializer := NewSessionSerializer(nil)

	data, err := serializer.Serialize(state)
	require.NoError(t, err)
	assert.NotEmpty(t, data)
	assert.NotEmpty(t, state.Checksum)

	deserialized, err := serializer.Deserialize(data)
	require.NoError(t, err)

	assert.Equal(t, state.ID, deserialized.ID)
	assert.Equal(t, state.Status, deserialized.Status)
	assert.Equal(t, state.QuestID, deserialized.QuestID)
	assert.Equal(t, state.Symbol, deserialized.Symbol)
	assert.Equal(t, state.IterationCount, deserialized.IterationCount)
	assert.Len(t, deserialized.ConversationHistory, 2)
	assert.Len(t, deserialized.ToolCallsMade, 1)
	assert.Len(t, deserialized.LoadedSkills, 2)
	assert.Equal(t, 50000.0, deserialized.MarketSnapshot.CurrentPrice)
}

func TestSessionState_ChecksumValidation(t *testing.T) {
	state := &SessionState{
		ID:        "sess_test",
		Status:    SessionStatusActive,
		Symbol:    "BTC/USDT",
		CreatedAt: time.Now(),
	}

	serializer := NewSessionSerializer(nil)
	data, err := serializer.Serialize(state)
	require.NoError(t, err)

	var rawMap map[string]interface{}
	json.Unmarshal(data, &rawMap)
	rawMap["symbol"] = "ETH/USDT"
	corruptedData, _ := json.Marshal(rawMap)

	_, err = serializer.Deserialize(corruptedData)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "checksum mismatch")
}

func TestSessionState_CreateFromExecutionLoop(t *testing.T) {
	serializer := NewSessionSerializer(nil)

	marketCtx := MarketContext{
		Symbol:       "BTC/USDT",
		CurrentPrice: 50000.0,
		Volatility:   0.02,
		Trend:        "bullish",
	}

	portfolio := PortfolioState{
		TotalValue:      100000.0,
		AvailableCash:   50000.0,
		OpenPositions:   2,
		CurrentDrawdown: 0.05,
	}

	messages := []llm.Message{
		{Role: llm.RoleUser, Content: "Test message"},
	}

	toolCalls := []ToolCallResult{
		{
			ToolID:   "tc_1",
			ToolName: "get_price",
			Result:   json.RawMessage(`{"price":50000}`),
			Duration: 100 * time.Millisecond,
		},
	}

	result := &ExecutionLoopResult{
		LoopID:     "loop_123",
		Symbol:     "BTC/USDT",
		Decision:   ExecutionDecisionApprove,
		Iterations: 2,
		Metadata:   map[string]string{"test": "true"},
	}

	state := serializer.CreateFromExecutionLoop(
		"loop_123",
		"BTC/USDT",
		"quest_456",
		marketCtx,
		portfolio,
		messages,
		toolCalls,
		[]string{"scalping"},
		result,
	)

	assert.Equal(t, "loop_123", state.ID)
	assert.Equal(t, SessionStatusActive, state.Status)
	assert.Equal(t, "quest_456", state.QuestID)
	assert.Equal(t, "BTC/USDT", state.Symbol)
	assert.Len(t, state.ConversationHistory, 1)
	assert.Len(t, state.ToolCallsMade, 1)
	assert.Len(t, state.LoadedSkills, 1)
	assert.NotNil(t, state.MarketSnapshot)
	assert.NotNil(t, state.PortfolioSnapshot)
	assert.Equal(t, 2, state.IterationCount)
	assert.NotEmpty(t, state.Checksum)
}

func TestSessionState_StatusTransitions(t *testing.T) {
	tests := []struct {
		name  string
		from  SessionStatus
		to    SessionStatus
		valid bool
	}{
		{"active to paused", SessionStatusActive, SessionStatusPaused, true},
		{"active to completed", SessionStatusActive, SessionStatusCompleted, true},
		{"active to failed", SessionStatusActive, SessionStatusFailed, true},
		{"paused to active", SessionStatusPaused, SessionStatusActive, true},
		{"completed to active", SessionStatusCompleted, SessionStatusActive, false},
		{"failed to active", SessionStatusFailed, SessionStatusActive, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			state := &SessionState{
				ID:     "test",
				Status: tt.from,
			}
			if tt.valid {
				state.Status = tt.to
				assert.Equal(t, tt.to, state.Status)
			}
		})
	}
}

func TestToolCallRecord_Serialization(t *testing.T) {
	record := ToolCallRecord{
		ID:        "tc_123",
		Name:      "place_order",
		Arguments: json.RawMessage(`{"symbol":"BTC/USDT","side":"buy","amount":0.1}`),
		Result:    json.RawMessage(`{"orderId":"ord_456","status":"filled"}`),
		Timestamp: time.Now(),
	}

	data, err := json.Marshal(record)
	require.NoError(t, err)

	var decoded ToolCallRecord
	err = json.Unmarshal(data, &decoded)
	require.NoError(t, err)

	assert.Equal(t, record.ID, decoded.ID)
	assert.Equal(t, record.Name, decoded.Name)
	assert.JSONEq(t, string(record.Arguments), string(decoded.Arguments))
	assert.JSONEq(t, string(record.Result), string(decoded.Result))
}

func TestMarketContextSnapshot_Serialization(t *testing.T) {
	snapshot := &MarketContextSnapshot{
		Symbol:       "BTC/USDT",
		Exchange:     "binance",
		CurrentPrice: 50000.0,
		Volatility:   0.02,
		Liquidity:    0.95,
		Trend:        "bullish",
		Volume24h:    1234567.89,
		SnapshotTime: time.Now(),
		SignalsJSON:  `[{"name":"rsi","value":70}]`,
	}

	data, err := json.Marshal(snapshot)
	require.NoError(t, err)

	var decoded MarketContextSnapshot
	err = json.Unmarshal(data, &decoded)
	require.NoError(t, err)

	assert.Equal(t, snapshot.Symbol, decoded.Symbol)
	assert.Equal(t, snapshot.Exchange, decoded.Exchange)
	assert.Equal(t, snapshot.CurrentPrice, decoded.CurrentPrice)
	assert.Equal(t, snapshot.Volatility, decoded.Volatility)
}

func TestPortfolioSnapshot_Serialization(t *testing.T) {
	snapshot := &PortfolioSnapshot{
		TotalEquity:      100000.0,
		AvailableCapital: 50000.0,
		CurrentDrawdown:  0.05,
		OpenPositions:    3,
		UnrealizedPnL:    1500.0,
		SnapshotTime:     time.Now(),
		PositionsJSON:    `[{"symbol":"BTC/USDT","size":0.5}]`,
	}

	data, err := json.Marshal(snapshot)
	require.NoError(t, err)

	var decoded PortfolioSnapshot
	err = json.Unmarshal(data, &decoded)
	require.NoError(t, err)

	assert.Equal(t, snapshot.TotalEquity, decoded.TotalEquity)
	assert.Equal(t, snapshot.AvailableCapital, decoded.AvailableCapital)
	assert.Equal(t, snapshot.CurrentDrawdown, decoded.CurrentDrawdown)
	assert.Equal(t, snapshot.OpenPositions, decoded.OpenPositions)
}

func TestGenerateSessionID(t *testing.T) {
	id1 := generateSessionID()
	id2 := generateSessionID()

	assert.NotEmpty(t, id1)
	assert.NotEmpty(t, id2)
	assert.NotEqual(t, id1, id2)
	assert.Contains(t, id1, "sess_")
	assert.Contains(t, id2, "sess_")
}

func TestGenerateRandomString(t *testing.T) {
	str1 := generateRandomString(8)
	str2 := generateRandomString(8)

	assert.Len(t, str1, 8)
	assert.Len(t, str2, 8)
	assert.NotEqual(t, str1, str2)
}

func TestNullString(t *testing.T) {
	tests := []struct {
		input    string
		expected interface{}
	}{
		{"", nil},
		{"value", "value"},
	}

	for _, tt := range tests {
		result := nullString(tt.input)
		assert.Equal(t, tt.expected, result)
	}
}

func TestConvertMarketContext(t *testing.T) {
	ctx := MarketContext{
		Symbol:       "BTC/USDT",
		CurrentPrice: 50000.0,
		Volatility:   0.02,
		Liquidity:    0.95,
		Trend:        "bullish",
		Volume24h:    1234567.0,
		Signals: []TradingSignal{
			{Name: "rsi", Value: 70, Weight: 0.5, Direction: "bearish"},
		},
	}

	snapshot := convertMarketContext(ctx)

	assert.Equal(t, ctx.Symbol, snapshot.Symbol)
	assert.Equal(t, ctx.CurrentPrice, snapshot.CurrentPrice)
	assert.Equal(t, ctx.Volatility, snapshot.Volatility)
	assert.NotEmpty(t, snapshot.SignalsJSON)
}

func TestConvertPortfolioState(t *testing.T) {
	p := PortfolioState{
		TotalValue:      100000.0,
		AvailableCash:   50000.0,
		OpenPositions:   3,
		CurrentDrawdown: 0.05,
		UnrealizedPnL:   1500.0,
	}

	snapshot := convertPortfolioState(p)

	assert.Equal(t, p.TotalValue, snapshot.TotalEquity)
	assert.Equal(t, p.AvailableCash, snapshot.AvailableCapital)
	assert.Equal(t, p.OpenPositions, snapshot.OpenPositions)
	assert.Equal(t, p.CurrentDrawdown, snapshot.CurrentDrawdown)
}

type mockSessionRepository struct {
	sessions map[string]*SessionState
}

func newMockSessionRepository() *mockSessionRepository {
	return &mockSessionRepository{
		sessions: make(map[string]*SessionState),
	}
}

func (m *mockSessionRepository) Save(ctx context.Context, state *SessionState) error {
	m.sessions[state.ID] = state
	return nil
}

func (m *mockSessionRepository) Load(ctx context.Context, id string) (*SessionState, error) {
	state, ok := m.sessions[id]
	if !ok {
		return nil, fmt.Errorf("session not found")
	}
	return state, nil
}

func (m *mockSessionRepository) LoadByQuest(ctx context.Context, questID string) (*SessionState, error) {
	for _, state := range m.sessions {
		if state.QuestID == questID {
			return state, nil
		}
	}
	return nil, fmt.Errorf("session not found")
}

func (m *mockSessionRepository) ListActive(ctx context.Context, limit int) ([]*SessionState, error) {
	var active []*SessionState
	for _, state := range m.sessions {
		if state.Status == SessionStatusActive {
			active = append(active, state)
			if len(active) >= limit {
				break
			}
		}
	}
	return active, nil
}

func (m *mockSessionRepository) Delete(ctx context.Context, id string) error {
	delete(m.sessions, id)
	return nil
}

func (m *mockSessionRepository) UpdateStatus(ctx context.Context, id string, status SessionStatus) error {
	state, ok := m.sessions[id]
	if !ok {
		return fmt.Errorf("session not found")
	}
	state.Status = status
	return nil
}

func TestSessionSerializer_WithRepository(t *testing.T) {
	repo := newMockSessionRepository()
	serializer := NewSessionSerializer(repo)

	state := &SessionState{
		Symbol:    "BTC/USDT",
		Status:    SessionStatusActive,
		CreatedAt: time.Now(),
	}

	ctx := context.Background()
	err := serializer.Save(ctx, state)
	require.NoError(t, err)
	assert.NotEmpty(t, state.ID)
	assert.NotEmpty(t, state.Checksum)

	loaded, err := serializer.Load(ctx, state.ID)
	require.NoError(t, err)
	assert.Equal(t, state.ID, loaded.ID)
	assert.Equal(t, state.Symbol, loaded.Symbol)

	state.QuestID = "quest_123"
	serializer.Save(ctx, state)

	byQuest, err := serializer.LoadByQuest(ctx, "quest_123")
	require.NoError(t, err)
	assert.Equal(t, state.ID, byQuest.ID)

	active, err := repo.ListActive(ctx, 10)
	require.NoError(t, err)
	assert.Len(t, active, 1)

	err = repo.UpdateStatus(ctx, state.ID, SessionStatusPaused)
	require.NoError(t, err)

	loaded, _ = serializer.Load(ctx, state.ID)
	assert.Equal(t, SessionStatusPaused, loaded.Status)

	err = serializer.repo.Delete(ctx, state.ID)
	require.NoError(t, err)

	_, err = serializer.Load(ctx, state.ID)
	assert.Error(t, err)
}
