package services

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/irfndi/neuratrade/internal/telemetry"
	"github.com/shopspring/decimal"
)

// SubagentResult represents the result of a subagent execution.
type SubagentResult struct {
	AgentID   string
	AgentType string
	Success   bool
	Data      any
	Error     error
	Duration  time.Duration
}

// SubagentOptions contains options for spawning a subagent.
type SubagentOptions struct {
	// Timeout sets a timeout for the subagent execution.
	Timeout time.Duration
	// Tags adds tags for tracking/logging purposes.
	Tags map[string]string
	// Priority sets the execution priority.
	Priority int
}

// SubagentSpawner manages spawning and lifecycle of subagents.
type SubagentSpawner struct {
	mu             sync.RWMutex
	activeAgents   map[string]context.CancelFunc
	resultCh       chan SubagentResult
	logger         *slog.Logger
	defaultTimeout time.Duration
	maxConcurrent  int
	currentRunning atomic.Int32
	wg             sync.WaitGroup
	closeOnce      sync.Once
	closed         atomic.Bool
}

// NewSubagentSpawner creates a new subagent spawner.
func NewSubagentSpawner(defaultTimeout time.Duration, maxConcurrent int) *SubagentSpawner {
	return &SubagentSpawner{
		activeAgents:   make(map[string]context.CancelFunc),
		resultCh:       make(chan SubagentResult, 100),
		logger:         telemetry.Logger().With("component", "subagent_spawner"),
		defaultTimeout: defaultTimeout,
		maxConcurrent:  maxConcurrent,
	}
}

// SpawnAnalyst spawns an analyst agent for a specific symbol.
func (s *SubagentSpawner) SpawnAnalyst(ctx context.Context, symbol string, opts ...SubagentOptions) (*AnalystAgent, error) {
	agentID := fmt.Sprintf("analyst-%s-%s", symbol, uuid.New().String()[:8])

	options := s.mergeOptions(opts)

	// Check concurrency limit
	if !s.acquireSlot() {
		return nil, fmt.Errorf("max concurrent agents reached (%d)", s.maxConcurrent)
	}
	defer s.releaseSlot()

	s.logger.Info("Spawning analyst agent", "agent_id", agentID, "symbol", symbol)

	// Create agent with config matching the existing AnalystAgent
	config := AnalystAgentConfig{
		MinConfidence:    0.6,
		SignalThreshold:  0.5,
		MaxRiskScore:     0.8,
		AnalysisCooldown: 5 * time.Minute,
	}
	agent := NewAnalystAgent(config)

	// Create context with timeout
	agentCtx, cancel := context.WithTimeout(ctx, options.Timeout)
	s.registerAgent(agentID, cancel)

	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		defer s.unregisterAgent(agentID)
		defer cancel()

		start := time.Now()
		result := s.runAnalystAgent(agentCtx, agent, symbol)
		result.AgentID = agentID
		result.AgentType = "analyst"
		result.Duration = time.Since(start)

		select {
		case s.resultCh <- result:
		default:
			s.logger.Warn("Result channel full, dropping result")
		}
	}()

	return agent, nil
}

// SpawnRiskCheck spawns a risk check agent.
func (s *SubagentSpawner) SpawnRiskCheck(ctx context.Context, decision *TradingDecision, opts ...SubagentOptions) (*RiskAgent, error) {
	agentID := fmt.Sprintf("risk-%s-%s", decision.Symbol, uuid.New().String()[:8])

	options := s.mergeOptions(opts)

	// Check concurrency limit
	if !s.acquireSlot() {
		return nil, fmt.Errorf("max concurrent agents reached (%d)", s.maxConcurrent)
	}
	defer s.releaseSlot()

	s.logger.Info("Spawning risk check agent", "agent_id", agentID, "symbol", decision.Symbol)

	agent := &RiskAgent{
		ID:       agentID,
		Decision: decision,
		Config: RiskAgentConfig{
			MaxPositionSize:      decimal.NewFromFloat(0.1),
			MaxRiskPerTrade:      decimal.NewFromFloat(0.02),
			MaxDailyLoss:         decimal.NewFromFloat(0.05),
			EnableCircuitBreaker: true,
		},
	}

	// Create context with timeout
	agentCtx, cancel := context.WithTimeout(ctx, options.Timeout)
	s.registerAgent(agentID, cancel)

	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		defer s.unregisterAgent(agentID)
		defer cancel()

		start := time.Now()
		result := s.runRiskAgent(agentCtx, agent)
		result.AgentID = agentID
		result.AgentType = "risk"
		result.Duration = time.Since(start)

		select {
		case s.resultCh <- result:
		default:
			s.logger.Warn("Result channel full, dropping result")
		}
	}()

	return agent, nil
}

// SpawnExecutor spawns an executor agent for a trading decision.
func (s *SubagentSpawner) SpawnExecutor(ctx context.Context, decision *TradingDecision, opts ...SubagentOptions) (*ExecutorAgent, error) {
	agentID := fmt.Sprintf("executor-%s-%s", decision.Symbol, uuid.New().String()[:8])

	options := s.mergeOptions(opts)

	// Check concurrency limit
	if !s.acquireSlot() {
		return nil, fmt.Errorf("max concurrent agents reached (%d)", s.maxConcurrent)
	}
	defer s.releaseSlot()

	s.logger.Info("Spawning executor agent", "agent_id", agentID, "symbol", decision.Symbol, "action", decision.Action)

	agent := &ExecutorAgent{
		ID:       agentID,
		Decision: decision,
		Config: ExecutorAgentConfig{
			RetryCount:        3,
			RetryDelay:        time.Second,
			SlippageTolerance: decimal.NewFromFloat(0.001),
		},
	}

	// Create context with timeout
	agentCtx, cancel := context.WithTimeout(ctx, options.Timeout)
	s.registerAgent(agentID, cancel)

	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		defer s.unregisterAgent(agentID)
		defer cancel()

		start := time.Now()
		result := s.runExecutorAgent(agentCtx, agent)
		result.AgentID = agentID
		result.AgentType = "executor"
		result.Duration = time.Since(start)

		select {
		case s.resultCh <- result:
		default:
			s.logger.Warn("Result channel full, dropping result")
		}
	}()

	return agent, nil
}

// Results returns a channel for receiving subagent results.
func (s *SubagentSpawner) Results() <-chan SubagentResult {
	return s.resultCh
}

// Cancel cancels a running subagent by ID.
func (s *SubagentSpawner) Cancel(agentID string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	if cancel, ok := s.activeAgents[agentID]; ok {
		s.logger.Info("Cancelling agent", "agent_id", agentID)
		cancel()
		return true
	}
	return false
}

// CancelAll cancels all running subagents.
func (s *SubagentSpawner) CancelAll() {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.logger.Info("Cancelling all agents", "count", len(s.activeAgents))
	for _, cancel := range s.activeAgents {
		cancel()
	}
}

// ActiveCount returns the number of currently running agents.
func (s *SubagentSpawner) ActiveCount() int32 {
	return s.currentRunning.Load()
}

// Wait waits for all spawned agents to complete.
func (s *SubagentSpawner) Wait() {
	s.wg.Wait()
}

// Close closes the spawner and waits for all agents to complete.
func (s *SubagentSpawner) Close() {
	s.closeOnce.Do(func() {
		s.closed.Store(true)
		s.CancelAll()
		s.wg.Wait()
		close(s.resultCh)
	})
}

func (s *SubagentSpawner) mergeOptions(opts []SubagentOptions) SubagentOptions {
	if len(opts) == 0 {
		return SubagentOptions{
			Timeout:  s.defaultTimeout,
			Tags:     make(map[string]string),
			Priority: 0,
		}
	}

	opt := opts[0]
	if opt.Timeout == 0 {
		opt.Timeout = s.defaultTimeout
	}
	if opt.Tags == nil {
		opt.Tags = make(map[string]string)
	}
	return opt
}

func (s *SubagentSpawner) acquireSlot() bool {
	maxConcurrent := s.maxConcurrent
	if maxConcurrent <= 0 {
		maxConcurrent = 1
	}
	currentMax := int32(maxConcurrent)
	for {
		current := s.currentRunning.Load()
		if current >= currentMax {
			return false
		}
		if s.currentRunning.CompareAndSwap(current, current+1) {
			return true
		}
	}
}

func (s *SubagentSpawner) releaseSlot() {
	s.currentRunning.Add(-1)
}

func (s *SubagentSpawner) registerAgent(agentID string, cancel context.CancelFunc) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.activeAgents[agentID] = cancel
}

func (s *SubagentSpawner) unregisterAgent(agentID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.activeAgents, agentID)
}

// runAnalystAgent runs the analyst agent logic.
// TODO: Replace hardcoded AnalystSignal values with live market data
func (s *SubagentSpawner) runAnalystAgent(ctx context.Context, agent *AnalystAgent, symbol string) SubagentResult {
	// Use the existing AnalystAgent.Analyze method
	// Create some sample signals for the analysis
	signals := []AnalystSignal{
		{
			Name:        "rsi",
			Value:       0.65,
			Weight:      0.3,
			Direction:   DirectionBullish,
			Description: "RSI showing oversold conditions",
		},
		{
			Name:        "macd",
			Value:       0.5,
			Weight:      0.3,
			Direction:   DirectionBullish,
			Description: "MACD crossing above signal line",
		},
		{
			Name:        "volume",
			Value:       0.7,
			Weight:      0.2,
			Direction:   DirectionNeutral,
			Description: "Volume spike detected",
		},
		{
			Name:        "trend",
			Value:       0.6,
			Weight:      0.2,
			Direction:   DirectionBullish,
			Description: "Uptrend detected",
		},
	}

	select {
	case <-ctx.Done():
		return SubagentResult{
			Success: false,
			Error:   ctx.Err(),
		}
	default:
		// Perform analysis using the actual AnalystAgent
		analysis, err := agent.Analyze(ctx, symbol, AnalystRoleTechnical, signals)
		if err != nil {
			return SubagentResult{
				Success: false,
				Error:   err,
			}
		}

		return SubagentResult{
			Success: true,
			Data:    analysis,
		}
	}
}

// runRiskAgent runs the risk agent logic.
func (s *SubagentSpawner) runRiskAgent(ctx context.Context, agent *RiskAgent) SubagentResult {
	select {
	case <-ctx.Done():
		return SubagentResult{
			Success: false,
			Error:   ctx.Err(),
		}
	default:
		// Perform risk checks
		riskAssessment := map[string]any{
			"decision":     agent.Decision,
			"approved":     true,
			"risk_score":   agent.Decision.RiskScore,
			"max_position": agent.Config.MaxPositionSize,
			"warnings":     []string{},
		}

		// Basic risk validations
		if decimal.NewFromFloat(agent.Decision.SizePercent).GreaterThan(agent.Config.MaxPositionSize) {
			riskAssessment["approved"] = false
			riskAssessment["warnings"] = append(riskAssessment["warnings"].([]string), "position size exceeds max")
		}

		if decimal.NewFromFloat(agent.Decision.RiskScore).GreaterThan(agent.Config.MaxRiskPerTrade) {
			riskAssessment["approved"] = false
			riskAssessment["warnings"] = append(riskAssessment["warnings"].([]string), "risk score exceeds threshold")
		}

		return SubagentResult{
			Success: riskAssessment["approved"].(bool),
			Data:    riskAssessment,
		}
	}
}

// runExecutorAgent runs the executor agent logic.
// TODO: This is simulation scaffolding - replace with real order placement before production
func (s *SubagentSpawner) runExecutorAgent(ctx context.Context, agent *ExecutorAgent) SubagentResult {
	select {
	case <-ctx.Done():
		return SubagentResult{
			Success: false,
			Error:   ctx.Err(),
		}
	default:
		// Simulate order execution
		execution := map[string]interface{}{
			"decision":      agent.Decision,
			"order_id":      fmt.Sprintf("order-%s", uuid.New().String()[:8]),
			"status":        "filled",
			"filled_price":  agent.Decision.EntryPrice,
			"filled_amount": agent.Decision.SizePercent,
			"fees":          0.001,
		}

		return SubagentResult{
			Success: true,
			Data:    execution,
		}
	}
}

// RiskAgent represents a risk checking agent.
type RiskAgent struct {
	ID       string
	Decision *TradingDecision
	Config   RiskAgentConfig
}

// RiskAgentConfig contains configuration for risk agent.
type RiskAgentConfig struct {
	MaxPositionSize      decimal.Decimal
	MaxRiskPerTrade      decimal.Decimal
	MaxDailyLoss         decimal.Decimal
	EnableCircuitBreaker bool
}

// ExecutorAgent represents a trade execution agent.
type ExecutorAgent struct {
	ID       string
	Decision *TradingDecision
	Config   ExecutorAgentConfig
}

// ExecutorAgentConfig contains configuration for executor agent.
type ExecutorAgentConfig struct {
	RetryCount        int
	RetryDelay        time.Duration
	SlippageTolerance decimal.Decimal
}

// SubagentAggregator collects and aggregates results from multiple subagents.
type SubagentAggregator struct {
	mu      sync.Mutex
	results []SubagentResult
	logger  *slog.Logger
}

// NewSubagentAggregator creates a new subagent aggregator.
func NewSubagentAggregator() *SubagentAggregator {
	return &SubagentAggregator{
		results: make([]SubagentResult, 0),
		logger:  telemetry.Logger().With("component", "subagent_aggregator"),
	}
}

// Add adds a result to the aggregator.
func (a *SubagentAggregator) Add(result SubagentResult) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.results = append(a.results, result)
}

// Results returns all collected results.
func (a *SubagentAggregator) Results() []SubagentResult {
	a.mu.Lock()
	defer a.mu.Unlock()
	results := make([]SubagentResult, len(a.results))
	copy(results, a.results)
	return results
}

// Successful returns all successful results.
func (a *SubagentAggregator) Successful() []SubagentResult {
	a.mu.Lock()
	defer a.mu.Unlock()

	var successful []SubagentResult
	for _, r := range a.results {
		if r.Success {
			successful = append(successful, r)
		}
	}
	return successful
}

// Failed returns all failed results.
func (a *SubagentAggregator) Failed() []SubagentResult {
	a.mu.Lock()
	defer a.mu.Unlock()

	var failed []SubagentResult
	for _, r := range a.results {
		if !r.Success {
			failed = append(failed, r)
		}
	}
	return failed
}

// CollectWithTimeout collects results from a channel with a timeout.
// timeout parameter controls the wait time between results.
func (a *SubagentAggregator) CollectWithTimeout(ctx context.Context, resultCh <-chan SubagentResult, expectedCount int, timeout time.Duration) error {
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	timer := time.After(timeout)

	for i := 0; i < expectedCount; i++ {
		select {
		case result, ok := <-resultCh:
			if !ok {
				return fmt.Errorf("channel closed (got %d of %d)", i, expectedCount)
			}
			a.Add(result)
		case <-timer:
			return fmt.Errorf("timeout waiting for results (got %d of %d)", i, expectedCount)
		case <-ctx.Done():
			return ctx.Err()
		}
		timer = time.After(timeout)
	}

	return nil
}
