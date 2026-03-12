package autonomous

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type mockPolicyValidator struct {
	policyPasses    bool
	policyReason    string
	policyErr       error
	safeModeEnabled bool
	safeModeErr     error
	killSwitchOn    bool
	killSwitchErr   error
}

func (m *mockPolicyValidator) ValidateProposal(ctx context.Context, proposal *StrategyProposal) (bool, string, error) {
	return m.policyPasses, m.policyReason, m.policyErr
}

func (m *mockPolicyValidator) IsSafeModeEnabled(ctx context.Context) (bool, error) {
	return m.safeModeEnabled, m.safeModeErr
}

func (m *mockPolicyValidator) IsKillSwitchEngaged(ctx context.Context) (bool, error) {
	return m.killSwitchOn, m.killSwitchErr
}

type mockRiskManager struct {
	budget     decimal.Decimal
	budgetErr  error
	riskPasses bool
	riskReason string
	riskErr    error
}

func (m *mockRiskManager) GetAvailableBudget(ctx context.Context, strategyID string) (decimal.Decimal, error) {
	return m.budget, m.budgetErr
}

func (m *mockRiskManager) CheckRiskLimits(ctx context.Context, proposal *StrategyProposal) (bool, string, error) {
	return m.riskPasses, m.riskReason, m.riskErr
}

type mockExchangeConnector struct {
	connected    bool
	connectedErr error
}

func (m *mockExchangeConnector) IsConnected(ctx context.Context, exchange string) (bool, error) {
	return m.connected, m.connectedErr
}

func (m *mockExchangeConnector) CancelAllOrders(ctx context.Context, strategyID, exchange string) error {
	return nil
}

func (m *mockExchangeConnector) FlattenPositions(ctx context.Context, strategyID, exchange string) error {
	return nil
}

type mockEventPublisher struct{}

func (m *mockEventPublisher) PublishRollbackEvent(ctx context.Context, event *RollbackEvent) error {
	return nil
}

func (m *mockEventPublisher) PublishStageTransition(ctx context.Context, transition *StageTransition) error {
	return nil
}

func (m *mockEventPublisher) PublishGateStateChange(ctx context.Context, state *GateState) error {
	return nil
}

type mockStrategyRepository struct {
	states  map[string]*RolloutState
	events  []RollbackEvent
	saveErr error
	getErr  error
}

func newMockRepo() *mockStrategyRepository {
	return &mockStrategyRepository{
		states: make(map[string]*RolloutState),
		events: []RollbackEvent{},
	}
}

func (m *mockStrategyRepository) SaveRolloutState(ctx context.Context, state *RolloutState) error {
	if m.saveErr != nil {
		return m.saveErr
	}
	m.states[state.StrategyID] = state
	return nil
}

func (m *mockStrategyRepository) GetRolloutState(ctx context.Context, strategyID string) (*RolloutState, error) {
	if m.getErr != nil {
		return nil, m.getErr
	}
	return m.states[strategyID], nil
}

func (m *mockStrategyRepository) SaveRollbackEvent(ctx context.Context, event *RollbackEvent) error {
	m.events = append(m.events, *event)
	return nil
}

func (m *mockStrategyRepository) GetRollbackHistory(ctx context.Context, strategyID string, limit int) ([]RollbackEvent, error) {
	result := make([]RollbackEvent, 0, len(m.events))
	for _, event := range m.events {
		if event.StrategyID != strategyID {
			continue
		}
		result = append(result, event)
		if limit > 0 && len(result) >= limit {
			break
		}
	}
	return result, nil
}

func TestStrategyProposalEngine_GenerateProposal(t *testing.T) {
	config := DefaultStrategyProposalConfig()
	engine := NewStrategyProposalEngine(config, nil, nil)

	tests := []struct {
		name        string
		confidence  float64
		maxDrawdown decimal.Decimal
		params      map[string]any
		expectError bool
		expectedErr error
	}{
		{
			name:        "valid proposal",
			confidence:  0.8,
			maxDrawdown: decimal.NewFromFloat(0.5),
			params: map[string]any{
				"leverage":              2,
				"position_size_percent": json.Number("10"),
			},
			expectError: false,
		},
		{
			name:        "realistic scalping drawdown remains below risk threshold",
			confidence:  0.72,
			maxDrawdown: decimal.NewFromFloat(1.9),
			params: map[string]any{
				"position_size_percent": json.Number("12.7845"),
			},
			expectError: false,
		},
		{
			name:        "low confidence",
			confidence:  0.3,
			maxDrawdown: decimal.NewFromFloat(5),
			params:      map[string]any{},
			expectError: true,
			expectedErr: ErrLowConfidence,
		},
		{
			name:        "high risk",
			confidence:  0.9,
			maxDrawdown: decimal.NewFromFloat(50),
			params: map[string]any{
				"leverage":              int64(20),
				"position_size_percent": decimal.NewFromFloat(100),
			},
			expectError: true,
			expectedErr: ErrHighRisk,
		},
		{
			name:        "elevated drawdown alone can exceed risk threshold",
			confidence:  0.9,
			maxDrawdown: decimal.NewFromFloat(20),
			params:      map[string]any{},
			expectError: true,
			expectedErr: ErrHighRisk,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			proposal, err := engine.GenerateProposal(
				context.Background(),
				"strategy-1",
				"BTC/USDT",
				"binance",
				"buy",
				tt.confidence,
				"test reasoning",
				tt.params,
				decimal.NewFromFloat(10),
				tt.maxDrawdown,
			)

			if tt.expectError {
				assert.Error(t, err)
				assert.ErrorIs(t, err, tt.expectedErr)
				assert.Nil(t, proposal)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, proposal)
				assert.NotEmpty(t, proposal.ID)
				assert.Equal(t, "strategy-1", proposal.StrategyID)
				assert.Equal(t, "BTC/USDT", proposal.Symbol)
				assert.Equal(t, "binance", proposal.Exchange)
				assert.WithinDuration(t, time.Now(), proposal.CreatedAt, time.Second)
				assert.True(t, proposal.ExpiresAt.After(proposal.CreatedAt))
			}
		})
	}
}

func TestStrategyProposalEngine_ValidateProposal(t *testing.T) {
	config := DefaultStrategyProposalConfig()

	t.Run("nil proposal", func(t *testing.T) {
		engine := NewStrategyProposalEngine(config, nil, nil)
		err := engine.ValidateProposal(context.Background(), nil)
		assert.ErrorIs(t, err, ErrNilProposal)
	})

	t.Run("expired proposal", func(t *testing.T) {
		engine := NewStrategyProposalEngine(config, nil, nil)
		proposal := &StrategyProposal{
			ID:        "test-id",
			ExpiresAt: time.Now().Add(-1 * time.Hour),
		}
		err := engine.ValidateProposal(context.Background(), proposal)
		assert.ErrorIs(t, err, ErrProposalExpired)
	})

	t.Run("policy rejection", func(t *testing.T) {
		validator := &mockPolicyValidator{policyPasses: false, policyReason: "risk too high"}
		engine := NewStrategyProposalEngine(config, validator, nil)
		proposal := &StrategyProposal{
			ID:        "test-id",
			ExpiresAt: time.Now().Add(1 * time.Hour),
		}
		err := engine.ValidateProposal(context.Background(), proposal)
		assert.ErrorIs(t, err, ErrProposalRejected)
	})

	t.Run("risk rejection", func(t *testing.T) {
		validator := &mockPolicyValidator{policyPasses: true}
		risk := &mockRiskManager{riskPasses: false, riskReason: "budget exceeded"}
		engine := NewStrategyProposalEngine(config, validator, risk)
		proposal := &StrategyProposal{
			ID:        "test-id",
			ExpiresAt: time.Now().Add(1 * time.Hour),
		}
		err := engine.ValidateProposal(context.Background(), proposal)
		assert.ErrorIs(t, err, ErrProposalRejected)
	})

	t.Run("valid proposal", func(t *testing.T) {
		validator := &mockPolicyValidator{policyPasses: true}
		risk := &mockRiskManager{riskPasses: true, budget: decimal.NewFromFloat(1000)}
		engine := NewStrategyProposalEngine(config, validator, risk)
		proposal := &StrategyProposal{
			ID:        "test-id",
			ExpiresAt: time.Now().Add(1 * time.Hour),
		}
		err := engine.ValidateProposal(context.Background(), proposal)
		assert.NoError(t, err)
	})
}

func TestStagedRolloutManager_InitializeRollout(t *testing.T) {
	repo := newMockRepo()
	manager := NewStagedRolloutManager(repo, nil)

	state, err := manager.InitializeRollout(context.Background(), "strategy-1", DefaultPromotionCriteria())

	require.NoError(t, err)
	assert.Equal(t, "strategy-1", state.StrategyID)
	assert.Equal(t, StageShadow, state.CurrentStage)
	assert.Equal(t, StatusActive, state.Status)
	assert.WithinDuration(t, time.Now(), state.EnteredAt, time.Second)
}

func TestNewStagedRolloutManager_RequiresRepository(t *testing.T) {
	assert.PanicsWithValue(t, "strategy repository is required", func() {
		_ = NewStagedRolloutManager(nil, nil)
	})
}

func TestStagedRolloutManager_InitializeRollout_RejectsEmptyStrategyID(t *testing.T) {
	repo := newMockRepo()
	manager := NewStagedRolloutManager(repo, nil)

	state, err := manager.InitializeRollout(context.Background(), "   ", DefaultPromotionCriteria())

	require.Error(t, err)
	assert.Nil(t, state)
	assert.Contains(t, err.Error(), "strategy_id is required")
}

func TestStagedRolloutManager_Promote(t *testing.T) {
	repo := newMockRepo()
	manager := NewStagedRolloutManager(repo, &mockEventPublisher{})

	state, err := manager.InitializeRollout(context.Background(), "strategy-1", PromotionCriteria{
		MinTrades:        1,
		MinWinRate:       0.5,
		DurationRequired: 0,
	})
	require.NoError(t, err)
	assert.Equal(t, StageShadow, state.CurrentStage)

	metrics := RolloutMetrics{
		TotalTrades:   10,
		WinningTrades: 6,
		LosingTrades:  4,
		WinRate:       0.6,
		UptimePercent: 99.0,
		LastUpdated:   time.Now(),
	}
	err = manager.UpdateMetrics(context.Background(), "strategy-1", metrics)
	require.NoError(t, err)

	state, err = manager.Promote(context.Background(), "strategy-1", "criteria met")
	require.NoError(t, err)
	assert.Equal(t, StagePaper, state.CurrentStage)
	assert.Len(t, state.History, 1)
	assert.Equal(t, StageShadow, state.History[0].FromStage)
	assert.Equal(t, StagePaper, state.History[0].ToStage)
}

func TestStagedRolloutManager_CheckPromotionEligibility_NilState(t *testing.T) {
	repo := newMockRepo()
	manager := NewStagedRolloutManager(repo, nil)

	eligible, failed := manager.CheckPromotionEligibility(nil)

	assert.False(t, eligible)
	require.NotEmpty(t, failed)
	assert.Contains(t, failed[0], "rollout state is nil")
}

func TestStagedRolloutManager_Rollback(t *testing.T) {
	repo := newMockRepo()
	manager := NewStagedRolloutManager(repo, &mockEventPublisher{})

	state, err := manager.InitializeRollout(context.Background(), "strategy-1", PromotionCriteria{
		MinTrades:        0,
		DurationRequired: 0,
	})
	require.NoError(t, err)
	err = manager.UpdateMetrics(context.Background(), "strategy-1", RolloutMetrics{TotalTrades: 10, WinRate: 0.6, UptimePercent: 99})
	require.NoError(t, err)
	_, err = manager.Promote(context.Background(), "strategy-1", "test")
	require.NoError(t, err)
	state, err = manager.Rollback(context.Background(), "strategy-1", TriggerMaxDrawdown, "drawdown exceeded")
	require.NoError(t, err)
	assert.Equal(t, StageShadow, state.CurrentStage)
	assert.Equal(t, StatusRolledBack, state.Status)
}

func TestStagedRolloutManager_Rollback_InvalidStage(t *testing.T) {
	repo := newMockRepo()
	manager := NewStagedRolloutManager(repo, nil)
	repo.states["strategy-1"] = &RolloutState{
		StrategyID:   "strategy-1",
		CurrentStage: RolloutStage("invalid"),
		Status:       StatusActive,
		EnteredAt:    time.Now(),
	}

	state, err := manager.Rollback(context.Background(), "strategy-1", TriggerMaxDrawdown, "test")

	require.Error(t, err)
	assert.ErrorIs(t, err, ErrInvalidStage)
	assert.Nil(t, state)
}

func TestStagedRolloutManager_Rollback_NoPreviousStage(t *testing.T) {
	repo := newMockRepo()
	manager := NewStagedRolloutManager(repo, nil)
	repo.states["strategy-1"] = &RolloutState{
		StrategyID:   "strategy-1",
		CurrentStage: StageShadow,
		Status:       StatusActive,
		EnteredAt:    time.Now(),
	}

	state, err := manager.Rollback(context.Background(), "strategy-1", TriggerMaxDrawdown, "test")

	require.Error(t, err)
	assert.ErrorIs(t, err, ErrNoPreviousStage)
	assert.Nil(t, state)
}

func TestStagedRolloutManager_PauseResume(t *testing.T) {
	repo := newMockRepo()
	manager := NewStagedRolloutManager(repo, nil)

	_, err := manager.InitializeRollout(context.Background(), "strategy-1", DefaultPromotionCriteria())
	require.NoError(t, err)

	err = manager.Pause(context.Background(), "strategy-1", "maintenance")
	require.NoError(t, err)

	state, err := manager.GetRolloutState(context.Background(), "strategy-1")
	require.NoError(t, err)
	assert.Equal(t, StatusPaused, state.Status)

	err = manager.Resume(context.Background(), "strategy-1")
	require.NoError(t, err)

	state, err = manager.GetRolloutState(context.Background(), "strategy-1")
	require.NoError(t, err)
	assert.Equal(t, StatusActive, state.Status)
}

func TestAutoRollbackEngine_CheckTriggers(t *testing.T) {
	config := DefaultRollbackConfig()
	engine := NewAutoRollbackEngine(config, nil, nil, nil, nil)

	tests := []struct {
		name          string
		metrics       RolloutMetrics
		expectTrigger RollbackTrigger
	}{
		{
			name: "no trigger",
			metrics: RolloutMetrics{
				TotalPnL:    decimal.NewFromFloat(100),
				AvgSlippage: decimal.NewFromFloat(0.1),
				MaxDrawdown: decimal.NewFromFloat(5),
			},
			expectTrigger: "",
		},
		{
			name: "pnl breach",
			metrics: RolloutMetrics{
				TotalPnL: decimal.NewFromFloat(-600),
			},
			expectTrigger: TriggerPnLBreach,
		},
		{
			name: "slippage spike",
			metrics: RolloutMetrics{
				AvgSlippage: decimal.NewFromFloat(1.5),
			},
			expectTrigger: TriggerSlippageSpike,
		},
		{
			name: "max drawdown",
			metrics: RolloutMetrics{
				MaxDrawdown: decimal.NewFromFloat(15),
			},
			expectTrigger: TriggerMaxDrawdown,
		},
		{
			name: "net losses exceeded",
			metrics: RolloutMetrics{
				LosingTrades:  10,
				WinningTrades: 2,
			},
			expectTrigger: TriggerNetLoss,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			trigger, _ := engine.CheckTriggersOnly(tt.metrics)
			assert.Equal(t, tt.expectTrigger, trigger)
		})
	}
}

func TestAutoRollbackEngine_GetRollbackHistory_ValidatesInput(t *testing.T) {
	engine := NewAutoRollbackEngine(DefaultRollbackConfig(), newMockRepo(), nil, nil, nil)

	history, err := engine.GetRollbackHistory(context.Background(), "   ", 5)
	require.Error(t, err)
	assert.Nil(t, history)
	assert.Contains(t, err.Error(), "strategy_id is required")

	history, err = engine.GetRollbackHistory(context.Background(), "strategy-1", 0)
	require.Error(t, err)
	assert.Nil(t, history)
	assert.Contains(t, err.Error(), "rollback history limit")
}

func TestAutoRollbackEngine_CheckTriggers_UsesNetLossCount(t *testing.T) {
	config := DefaultRollbackConfig()
	config.ConsecutiveLossLimit = 3
	engine := NewAutoRollbackEngine(config, nil, nil, nil, nil)

	tests := []struct {
		name          string
		metrics       RolloutMetrics
		expectTrigger RollbackTrigger
	}{
		{
			name: "net losses below threshold does not trigger",
			metrics: RolloutMetrics{
				WinningTrades: 4,
				LosingTrades:  6, // net = 2
			},
			expectTrigger: "",
		},
		{
			name: "net losses above threshold triggers",
			metrics: RolloutMetrics{
				WinningTrades: 2,
				LosingTrades:  6, // net = 4
			},
			expectTrigger: TriggerNetLoss,
		},
		{
			name: "all losses and no wins trigger",
			metrics: RolloutMetrics{
				WinningTrades: 0,
				LosingTrades:  3,
			},
			expectTrigger: TriggerNetLoss,
		},
		{
			name: "more wins than losses clamps to zero",
			metrics: RolloutMetrics{
				WinningTrades: 7,
				LosingTrades:  2,
			},
			expectTrigger: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			trigger, _ := engine.CheckTriggersOnly(tt.metrics)
			assert.Equal(t, tt.expectTrigger, trigger)
		})
	}
}

func TestAutoRollbackEngine_Cooldown(t *testing.T) {
	config := DefaultRollbackConfig()
	config.CooldownPeriod = 1 * time.Hour
	engine := NewAutoRollbackEngine(config, nil, nil, nil, nil)

	assert.False(t, engine.isOnCooldown("strategy-1"))

	engine.setCooldown("strategy-1")
	assert.True(t, engine.isOnCooldown("strategy-1"))

	engine.ClearCooldown("strategy-1")
	assert.False(t, engine.isOnCooldown("strategy-1"))
}

func TestAutoRollbackEngine_Evaluate_RequiresRolloutManager(t *testing.T) {
	engine := NewAutoRollbackEngine(DefaultRollbackConfig(), nil, nil, nil, nil)

	event, err := engine.Evaluate(context.Background(), "strategy-1", RolloutMetrics{})

	require.Error(t, err)
	assert.Nil(t, event)
	assert.Contains(t, err.Error(), "rollout manager")
}

func TestAutoRollbackEngine_Evaluate_RequiresStrategyID(t *testing.T) {
	engine := NewAutoRollbackEngine(DefaultRollbackConfig(), nil, nil, nil, nil)

	event, err := engine.Evaluate(context.Background(), "   ", RolloutMetrics{})

	require.Error(t, err)
	assert.Nil(t, event)
	assert.Contains(t, err.Error(), "strategyID is required")
}

func TestAutoRollbackEngine_ForceRollback_RequiresRolloutManager(t *testing.T) {
	engine := NewAutoRollbackEngine(DefaultRollbackConfig(), nil, nil, nil, nil)

	event, err := engine.ForceRollback(context.Background(), "strategy-1", TriggerMaxDrawdown, "manual")

	require.Error(t, err)
	assert.Nil(t, event)
	assert.Contains(t, err.Error(), "rollout manager")
}

func TestAutoRollbackEngine_ForceRollback_RequiresStrategyID(t *testing.T) {
	engine := NewAutoRollbackEngine(DefaultRollbackConfig(), nil, nil, nil, nil)

	event, err := engine.ForceRollback(context.Background(), "\t", TriggerMaxDrawdown, "manual")

	require.Error(t, err)
	assert.Nil(t, event)
	assert.Contains(t, err.Error(), "strategyID is required")
}

func TestLiveTradingGate_Evaluate(t *testing.T) {
	config := DefaultGateConfig()

	// Setup mock rollout manager with a live strategy
	liveRepo := newMockRepo()
	liveManager := NewStagedRolloutManager(liveRepo, nil)
	_, err := liveManager.InitializeRollout(context.Background(), "strategy-1", DefaultPromotionCriteria())
	require.NoError(t, err)
	// Promote to live for testing
	liveRepo.states["strategy-1"].CurrentStage = StageLive
	liveRepo.states["strategy-1"].Status = StatusActive

	shadowRepo := newMockRepo()
	shadowManager := NewStagedRolloutManager(shadowRepo, nil)
	_, err = shadowManager.InitializeRollout(context.Background(), "strategy-1", DefaultPromotionCriteria())
	require.NoError(t, err)

	tests := []struct {
		name       string
		validator  *mockPolicyValidator
		risk       *mockRiskManager
		exchange   *mockExchangeConnector
		rollout    *StagedRolloutManager
		expectOpen bool
		blockCount int
	}{
		{
			name:       "all checks pass",
			validator:  &mockPolicyValidator{policyPasses: true, safeModeEnabled: false, killSwitchOn: false},
			risk:       &mockRiskManager{budget: decimal.NewFromFloat(1000), riskPasses: true},
			exchange:   &mockExchangeConnector{connected: true},
			rollout:    liveManager,
			expectOpen: true,
			blockCount: 0,
		},
		{
			name:       "safe mode blocks",
			validator:  &mockPolicyValidator{policyPasses: true, safeModeEnabled: true, killSwitchOn: false},
			risk:       &mockRiskManager{budget: decimal.NewFromFloat(1000)},
			exchange:   &mockExchangeConnector{connected: true},
			rollout:    liveManager,
			expectOpen: false,
			blockCount: 1,
		},
		{
			name:       "kill switch blocks",
			validator:  &mockPolicyValidator{policyPasses: true, safeModeEnabled: false, killSwitchOn: true},
			risk:       &mockRiskManager{budget: decimal.NewFromFloat(1000)},
			exchange:   &mockExchangeConnector{connected: true},
			rollout:    liveManager,
			expectOpen: false,
			blockCount: 1,
		},
		{
			name:       "no budget blocks",
			validator:  &mockPolicyValidator{policyPasses: true, safeModeEnabled: false, killSwitchOn: false},
			risk:       &mockRiskManager{budget: decimal.NewFromFloat(0), riskPasses: true},
			exchange:   &mockExchangeConnector{connected: true},
			rollout:    liveManager,
			expectOpen: false,
			blockCount: 1,
		},
		{
			name:       "exchange disconnected blocks",
			validator:  &mockPolicyValidator{policyPasses: true, safeModeEnabled: false, killSwitchOn: false},
			risk:       &mockRiskManager{budget: decimal.NewFromFloat(1000), riskPasses: true},
			exchange:   &mockExchangeConnector{connected: false},
			rollout:    liveManager,
			expectOpen: false,
			blockCount: 1,
		},
		{
			name:       "shadow stage blocks live execution",
			validator:  &mockPolicyValidator{policyPasses: true, safeModeEnabled: false, killSwitchOn: false},
			risk:       &mockRiskManager{budget: decimal.NewFromFloat(1000), riskPasses: true},
			exchange:   &mockExchangeConnector{connected: true},
			rollout:    shadowManager,
			expectOpen: false,
			blockCount: 1,
		},
		{
			name:       "rollout manager missing blocks by default",
			validator:  &mockPolicyValidator{policyPasses: true, safeModeEnabled: false, killSwitchOn: false},
			risk:       &mockRiskManager{budget: decimal.NewFromFloat(1000), riskPasses: true},
			exchange:   &mockExchangeConnector{connected: true},
			rollout:    nil,
			expectOpen: false,
			blockCount: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gate := NewLiveTradingGate(config, tt.validator, tt.risk, tt.exchange, tt.rollout)
			state, err := gate.Evaluate(context.Background(), "strategy-1")

			require.NoError(t, err)
			assert.Equal(t, tt.expectOpen, state.IsOpen)
			assert.Len(t, state.BlockReasons, tt.blockCount)
		})
	}
}

func TestLiveTradingGate_ForceOpenClose(t *testing.T) {
	config := DefaultGateConfig()
	gate := NewLiveTradingGate(config, nil, nil, nil, nil)

	err := gate.ForceOpen(context.Background(), "strategy-1")
	require.NoError(t, err)

	isOpen, _ := gate.IsOpen(context.Background(), "strategy-1")
	assert.True(t, isOpen)

	err = gate.ForceClose(context.Background(), "strategy-1", "emergency")
	require.NoError(t, err)

	isOpen, _ = gate.IsOpen(context.Background(), "strategy-1")
	assert.False(t, isOpen)
}

func TestLiveTradingGate_ForceOverridesDoNotExpire(t *testing.T) {
	config := DefaultGateConfig()
	config.CacheDuration = 10 * time.Millisecond
	gate := NewLiveTradingGate(config, nil, nil, nil, nil)

	require.NoError(t, gate.ForceOpen(context.Background(), "strategy-1"))
	time.Sleep(25 * time.Millisecond)
	isOpen, err := gate.IsOpen(context.Background(), "strategy-1")
	require.NoError(t, err)
	assert.True(t, isOpen)

	require.NoError(t, gate.ForceClose(context.Background(), "strategy-1", "test"))
	time.Sleep(25 * time.Millisecond)
	isOpen, err = gate.IsOpen(context.Background(), "strategy-1")
	require.NoError(t, err)
	assert.False(t, isOpen)
}

func TestLiveTradingGate_Cache(t *testing.T) {
	config := DefaultGateConfig()
	config.CacheDuration = 5 * time.Second

	validator := &mockPolicyValidator{policyPasses: true, safeModeEnabled: false, killSwitchOn: false}
	gate := NewLiveTradingGate(config, validator, nil, nil, nil)

	state1, err := gate.Evaluate(context.Background(), "strategy-1")
	require.NoError(t, err)

	state2, err := gate.Evaluate(context.Background(), "strategy-1")
	require.NoError(t, err)
	assert.Equal(t, state1.LastEvaluated, state2.LastEvaluated)

	gate.ClearCache("strategy-1")

	state3, err := gate.Evaluate(context.Background(), "strategy-1")
	require.NoError(t, err)
	assert.True(t, state3.LastEvaluated.After(state1.LastEvaluated))
}

func TestLiveTradingGate_CacheReturnsDefensiveCopy(t *testing.T) {
	config := DefaultGateConfig()
	gate := NewLiveTradingGate(config, nil, nil, nil, nil)

	require.NoError(t, gate.ForceClose(context.Background(), "strategy-1", "immutable"))

	state1, err := gate.Evaluate(context.Background(), "strategy-1")
	require.NoError(t, err)
	require.NotEmpty(t, state1.BlockReasons)
	state1.BlockReasons[0] = "mutated"

	state2, err := gate.Evaluate(context.Background(), "strategy-1")
	require.NoError(t, err)
	require.NotEmpty(t, state2.BlockReasons)
	assert.Equal(t, "force_closed: immutable", state2.BlockReasons[0])
}
