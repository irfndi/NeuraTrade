package services

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/irfndi/neuratrade/internal/ai/llm"
	"github.com/irfndi/neuratrade/internal/autonomous"
	"github.com/irfndi/neuratrade/internal/ccxt"
	"github.com/irfndi/neuratrade/internal/skill"
	"github.com/shopspring/decimal"
)

type AIScalpingConfig struct {
	Exchange          string
	Model             string
	Leverage          int
	MaxTokens         int
	MaxCapitalPct     float64
	MinConfidence     float64
	MaxIterations     int
	Timeout           time.Duration
	AutoExecute       bool
	AllowSpotFallback bool
	MaxPairsToAnalyze int
	MaxCandidatePairs int
	OrderBookPairs    int
	EnforceFutures    bool
	SymbolCooldown    time.Duration
	FailureBudget     int
	FailureWindow     time.Duration
	StructuredRetries int
	LossStreakBudget  int
	LossCooldown      time.Duration
	LossWindow        time.Duration
	PreTradeGate      bool
	MinExpectancyEdge float64
	MinExpectancyN    int
	RegimeHighBand    float64
	RegimeLowBand     float64
}

func DefaultAIScalpingConfig() AIScalpingConfig {
	return AIScalpingConfig{
		Exchange:          "bitget", // Default, will be overridden by user settings
		Model:             "glm-5",
		Leverage:          5,
		MaxTokens:         1200,
		MaxCapitalPct:     5.0,
		MinConfidence:     0.65,
		MaxIterations:     3,
		Timeout:           90 * time.Second,
		AutoExecute:       true,
		AllowSpotFallback: false,
		MaxPairsToAnalyze: 8,
		MaxCandidatePairs: 120,
		OrderBookPairs:    4,
		EnforceFutures:    true,
		SymbolCooldown:    90 * time.Second,
		FailureBudget:     3,
		FailureWindow:     15 * time.Minute,
		StructuredRetries: 2,
		LossStreakBudget:  2,
		LossCooldown:      20 * time.Minute,
		LossWindow:        90 * time.Minute,
		PreTradeGate:      true,
		MinExpectancyEdge: 0,
		MinExpectancyN:    8,
		RegimeHighBand:    85,
		RegimeLowBand:     15,
	}
}

func ResolveAIScalpingConfigFromEnv(base AIScalpingConfig) AIScalpingConfig {
	cfg := applyAIScalpingConfigFromFile(base)

	if value := strings.TrimSpace(os.Getenv("NEURATRADE_SCALPING_EXCHANGE")); value != "" {
		cfg.Exchange = strings.ToLower(value)
	}
	if value := strings.TrimSpace(os.Getenv("NEURATRADE_SCALPING_MODEL")); value != "" {
		cfg.Model = value
	}
	if value := getEnvInt("NEURATRADE_SCALPING_LEVERAGE"); value > 0 {
		cfg.Leverage = clampInt(value, 1, 50)
	}
	if value := getEnvInt("NEURATRADE_SCALPING_MAX_TOKENS"); value > 0 {
		cfg.MaxTokens = clampInt(value, 128, 8192)
	}
	if value, ok := getEnvFloat("NEURATRADE_SCALPING_MAX_CAPITAL_PCT"); ok {
		cfg.MaxCapitalPct = clampFloat(value, 0.1, 100)
	}
	if value, ok := getEnvFloat("NEURATRADE_SCALPING_MIN_CONFIDENCE"); ok {
		cfg.MinConfidence = clampFloat(value, 0.05, 0.99)
	}
	if value := getEnvInt("NEURATRADE_SCALPING_MAX_ITERATIONS"); value > 0 {
		cfg.MaxIterations = clampInt(value, 1, 20)
	}
	if value := getEnvInt("NEURATRADE_SCALPING_TIMEOUT_SECONDS"); value > 0 {
		cfg.Timeout = time.Duration(clampInt(value, 10, 600)) * time.Second
	}
	if value, ok := getEnvBool("NEURATRADE_SCALPING_AUTO_EXECUTE"); ok {
		cfg.AutoExecute = value
	}
	if value, ok := getEnvBool("NEURATRADE_SCALPING_ALLOW_SPOT_FALLBACK"); ok {
		cfg.AllowSpotFallback = value
	}
	if value := getEnvInt("NEURATRADE_SCALPING_MAX_PAIRS"); value > 0 {
		cfg.MaxPairsToAnalyze = clampInt(value, 1, 64)
	}
	if value := getEnvInt("NEURATRADE_SCALPING_MAX_CANDIDATES"); value > 0 {
		cfg.MaxCandidatePairs = clampInt(value, cfg.MaxPairsToAnalyze, 2000)
	}
	if value := getEnvInt("NEURATRADE_SCALPING_ORDERBOOK_PAIRS"); value > 0 {
		cfg.OrderBookPairs = clampInt(value, 1, cfg.MaxPairsToAnalyze)
	}
	if value, ok := getEnvBool("NEURATRADE_SCALPING_ENFORCE_FUTURES_UNIVERSE"); ok {
		cfg.EnforceFutures = value
	}
	if value := getEnvInt("NEURATRADE_SCALPING_SYMBOL_COOLDOWN_SECONDS"); value > 0 {
		cfg.SymbolCooldown = time.Duration(clampInt(value, 5, 900)) * time.Second
	}
	if value := getEnvInt("NEURATRADE_SCALPING_SYMBOL_FAILURE_BUDGET"); value > 0 {
		cfg.FailureBudget = clampInt(value, 1, 20)
	}
	if value := getEnvInt("NEURATRADE_SCALPING_SYMBOL_FAILURE_WINDOW_SECONDS"); value > 0 {
		cfg.FailureWindow = time.Duration(clampInt(value, 30, 7200)) * time.Second
	}
	if value := getEnvInt("NEURATRADE_SCALPING_STRUCTURED_RETRIES"); value > 0 {
		cfg.StructuredRetries = clampInt(value, 1, 6)
	}
	if value := getEnvInt("NEURATRADE_SCALPING_SYMBOL_LOSS_STREAK_BUDGET"); value > 0 {
		cfg.LossStreakBudget = clampInt(value, 1, 10)
	}
	if value := getEnvInt("NEURATRADE_SCALPING_SYMBOL_LOSS_COOLDOWN_SECONDS"); value > 0 {
		cfg.LossCooldown = time.Duration(clampInt(value, 30, 14400)) * time.Second
	}
	if value := getEnvInt("NEURATRADE_SCALPING_SYMBOL_LOSS_WINDOW_SECONDS"); value > 0 {
		cfg.LossWindow = time.Duration(clampInt(value, 60, 43200)) * time.Second
	}
	if value, ok := getEnvBool("NEURATRADE_SCALPING_PRETRADE_GATE"); ok {
		cfg.PreTradeGate = value
	}
	if value, ok := getEnvFloat("NEURATRADE_SCALPING_MIN_EXPECTANCY_EDGE"); ok {
		cfg.MinExpectancyEdge = clampFloat(value, -10, 10)
	}
	if value := getEnvInt("NEURATRADE_SCALPING_MIN_EXPECTANCY_SAMPLES"); value > 0 {
		cfg.MinExpectancyN = clampInt(value, 1, 500)
	}
	if value, ok := getEnvFloat("NEURATRADE_SCALPING_REGIME_HIGH_BAND"); ok {
		cfg.RegimeHighBand = clampFloat(value, 55, 99)
	}
	if value, ok := getEnvFloat("NEURATRADE_SCALPING_REGIME_LOW_BAND"); ok {
		cfg.RegimeLowBand = clampFloat(value, 1, 45)
	}

	if cfg.MaxCandidatePairs < cfg.MaxPairsToAnalyze {
		cfg.MaxCandidatePairs = cfg.MaxPairsToAnalyze
	}
	if cfg.OrderBookPairs > cfg.MaxPairsToAnalyze {
		cfg.OrderBookPairs = cfg.MaxPairsToAnalyze
	}
	if cfg.OrderBookPairs <= 0 {
		cfg.OrderBookPairs = 1
	}
	if cfg.RegimeLowBand >= cfg.RegimeHighBand {
		cfg.RegimeLowBand = 15
		cfg.RegimeHighBand = 85
	}

	log.Printf(
		"[AI-SCALPING] Runtime config: exchange=%s model=%s leverage=%d max_tokens=%d max_capital_pct=%.2f min_confidence=%.2f timeout=%s auto_execute=%t allow_spot_fallback=%t max_pairs=%d max_candidates=%d orderbook_pairs=%d enforce_futures=%t symbol_cooldown=%s failure_budget=%d failure_window=%s structured_retries=%d loss_streak_budget=%d loss_cooldown=%s loss_window=%s pretrade_gate=%t min_expectancy_edge=%.4f min_expectancy_samples=%d regime_low=%.1f regime_high=%.1f",
		cfg.Exchange,
		cfg.Model,
		cfg.Leverage,
		cfg.MaxTokens,
		cfg.MaxCapitalPct,
		cfg.MinConfidence,
		cfg.Timeout,
		cfg.AutoExecute,
		cfg.AllowSpotFallback,
		cfg.MaxPairsToAnalyze,
		cfg.MaxCandidatePairs,
		cfg.OrderBookPairs,
		cfg.EnforceFutures,
		cfg.SymbolCooldown,
		cfg.FailureBudget,
		cfg.FailureWindow,
		cfg.StructuredRetries,
		cfg.LossStreakBudget,
		cfg.LossCooldown,
		cfg.LossWindow,
		cfg.PreTradeGate,
		cfg.MinExpectancyEdge,
		cfg.MinExpectancyN,
		cfg.RegimeLowBand,
		cfg.RegimeHighBand,
	)

	return cfg
}

type aiScalpingFileConfig struct {
	AI struct {
		MinConfidence *float64 `json:"min_confidence"`
		Scalping      struct {
			Exchange          string   `json:"exchange"`
			Model             string   `json:"model"`
			Leverage          *int     `json:"leverage"`
			MaxTokens         *int     `json:"max_tokens"`
			MaxCapitalPct     *float64 `json:"max_capital_pct"`
			MinConfidence     *float64 `json:"min_confidence"`
			MaxIterations     *int     `json:"max_iterations"`
			TimeoutSeconds    *int     `json:"timeout_seconds"`
			AutoExecute       *bool    `json:"auto_execute"`
			AllowSpotFallback *bool    `json:"allow_spot_fallback"`
			MaxPairs          *int     `json:"max_pairs"`
			MaxCandidates     *int     `json:"max_candidates"`
			OrderBookPairs    *int     `json:"orderbook_pairs"`
			EnforceFutures    *bool    `json:"enforce_futures_universe"`
			SymbolCooldownSec *int     `json:"symbol_cooldown_seconds"`
			FailureBudget     *int     `json:"symbol_failure_budget"`
			FailureWindowSec  *int     `json:"symbol_failure_window_seconds"`
			StructuredRetries *int     `json:"structured_retries"`
			LossStreakBudget  *int     `json:"symbol_loss_streak_budget"`
			LossCooldownSec   *int     `json:"symbol_loss_cooldown_seconds"`
			LossWindowSec     *int     `json:"symbol_loss_window_seconds"`
			PreTradeGate      *bool    `json:"pretrade_gate"`
			MinExpectancyEdge *float64 `json:"min_expectancy_edge"`
			MinExpectancyN    *int     `json:"min_expectancy_samples"`
			RegimeHighBand    *float64 `json:"regime_high_band"`
			RegimeLowBand     *float64 `json:"regime_low_band"`
		} `json:"scalping"`
	} `json:"ai"`
}

func applyAIScalpingConfigFromFile(base AIScalpingConfig) AIScalpingConfig {
	cfg := base

	configPath := strings.TrimSpace(os.Getenv("NEURATRADE_HOME"))
	if configPath != "" {
		configPath = filepath.Join(configPath, "config.json")
	} else {
		homeDir, err := os.UserHomeDir()
		if err != nil {
			return cfg
		}
		configPath = filepath.Join(homeDir, ".neuratrade", "config.json")
	}
	// #nosec G304,G703 -- config path is derived from NEURATRADE_HOME or user home
	content, err := os.ReadFile(configPath)
	if err != nil {
		return cfg
	}

	var fileConfig aiScalpingFileConfig
	if err := json.Unmarshal(content, &fileConfig); err != nil {
		log.Printf("[AI-SCALPING] Failed to parse %s: %v", configPath, err)
		return cfg
	}

	if fileConfig.AI.MinConfidence != nil {
		cfg.MinConfidence = clampFloat(*fileConfig.AI.MinConfidence, 0.05, 0.99)
	}
	if value := strings.TrimSpace(fileConfig.AI.Scalping.Exchange); value != "" {
		cfg.Exchange = strings.ToLower(value)
	}
	if value := strings.TrimSpace(fileConfig.AI.Scalping.Model); value != "" {
		cfg.Model = value
	}
	if fileConfig.AI.Scalping.Leverage != nil {
		cfg.Leverage = clampInt(*fileConfig.AI.Scalping.Leverage, 1, 50)
	}
	if fileConfig.AI.Scalping.MaxTokens != nil {
		cfg.MaxTokens = clampInt(*fileConfig.AI.Scalping.MaxTokens, 128, 8192)
	}
	if fileConfig.AI.Scalping.MaxCapitalPct != nil {
		cfg.MaxCapitalPct = clampFloat(*fileConfig.AI.Scalping.MaxCapitalPct, 0.1, 100)
	}
	if fileConfig.AI.Scalping.MinConfidence != nil {
		cfg.MinConfidence = clampFloat(*fileConfig.AI.Scalping.MinConfidence, 0.05, 0.99)
	}
	if fileConfig.AI.Scalping.MaxIterations != nil {
		cfg.MaxIterations = clampInt(*fileConfig.AI.Scalping.MaxIterations, 1, 20)
	}
	if fileConfig.AI.Scalping.TimeoutSeconds != nil {
		cfg.Timeout = time.Duration(clampInt(*fileConfig.AI.Scalping.TimeoutSeconds, 10, 600)) * time.Second
	}
	if fileConfig.AI.Scalping.AutoExecute != nil {
		cfg.AutoExecute = *fileConfig.AI.Scalping.AutoExecute
	}
	if fileConfig.AI.Scalping.AllowSpotFallback != nil {
		cfg.AllowSpotFallback = *fileConfig.AI.Scalping.AllowSpotFallback
	}
	if fileConfig.AI.Scalping.MaxPairs != nil {
		cfg.MaxPairsToAnalyze = clampInt(*fileConfig.AI.Scalping.MaxPairs, 1, 64)
	}
	if fileConfig.AI.Scalping.MaxCandidates != nil {
		cfg.MaxCandidatePairs = clampInt(*fileConfig.AI.Scalping.MaxCandidates, cfg.MaxPairsToAnalyze, 2000)
	}
	if fileConfig.AI.Scalping.OrderBookPairs != nil {
		cfg.OrderBookPairs = clampInt(*fileConfig.AI.Scalping.OrderBookPairs, 1, cfg.MaxPairsToAnalyze)
	}
	if fileConfig.AI.Scalping.EnforceFutures != nil {
		cfg.EnforceFutures = *fileConfig.AI.Scalping.EnforceFutures
	}
	if fileConfig.AI.Scalping.SymbolCooldownSec != nil {
		cfg.SymbolCooldown = time.Duration(clampInt(*fileConfig.AI.Scalping.SymbolCooldownSec, 5, 900)) * time.Second
	}
	if fileConfig.AI.Scalping.FailureBudget != nil {
		cfg.FailureBudget = clampInt(*fileConfig.AI.Scalping.FailureBudget, 1, 20)
	}
	if fileConfig.AI.Scalping.FailureWindowSec != nil {
		cfg.FailureWindow = time.Duration(clampInt(*fileConfig.AI.Scalping.FailureWindowSec, 30, 7200)) * time.Second
	}
	if fileConfig.AI.Scalping.StructuredRetries != nil {
		cfg.StructuredRetries = clampInt(*fileConfig.AI.Scalping.StructuredRetries, 1, 6)
	}
	if fileConfig.AI.Scalping.LossStreakBudget != nil {
		cfg.LossStreakBudget = clampInt(*fileConfig.AI.Scalping.LossStreakBudget, 1, 10)
	}
	if fileConfig.AI.Scalping.LossCooldownSec != nil {
		cfg.LossCooldown = time.Duration(clampInt(*fileConfig.AI.Scalping.LossCooldownSec, 30, 14400)) * time.Second
	}
	if fileConfig.AI.Scalping.LossWindowSec != nil {
		cfg.LossWindow = time.Duration(clampInt(*fileConfig.AI.Scalping.LossWindowSec, 60, 43200)) * time.Second
	}
	if fileConfig.AI.Scalping.PreTradeGate != nil {
		cfg.PreTradeGate = *fileConfig.AI.Scalping.PreTradeGate
	}
	if fileConfig.AI.Scalping.MinExpectancyEdge != nil {
		cfg.MinExpectancyEdge = clampFloat(*fileConfig.AI.Scalping.MinExpectancyEdge, -10, 10)
	}
	if fileConfig.AI.Scalping.MinExpectancyN != nil {
		cfg.MinExpectancyN = clampInt(*fileConfig.AI.Scalping.MinExpectancyN, 1, 500)
	}
	if fileConfig.AI.Scalping.RegimeHighBand != nil {
		cfg.RegimeHighBand = clampFloat(*fileConfig.AI.Scalping.RegimeHighBand, 55, 99)
	}
	if fileConfig.AI.Scalping.RegimeLowBand != nil {
		cfg.RegimeLowBand = clampFloat(*fileConfig.AI.Scalping.RegimeLowBand, 1, 45)
	}

	return cfg
}

type AITradingDecision struct {
	Action          string           `json:"action"`
	Symbol          string           `json:"symbol"`
	SizePercent     float64          `json:"size_pct"`
	Confidence      float64          `json:"confidence"`
	Reasoning       string           `json:"reasoning"`
	ReasonCategory  string           `json:"reason_category,omitempty"`
	ConfidenceKnown bool             `json:"confidence_known"`
	StopLoss        *decimal.Decimal `json:"stop_loss,omitempty"`
	TakeProfit      *decimal.Decimal `json:"take_profit,omitempty"`
	OrderID         string           `json:"order_id,omitempty"`
	EntryPrice      *decimal.Decimal `json:"-"`
}

type TradingPortfolio struct {
	USDTBalance        float64 `json:"usdt_balance"`
	TotalValue         float64 `json:"total_value"`
	OpenPositions      int     `json:"open_positions"`
	UnrealizedPnL      float64 `json:"unrealized_pnl"`
	CurrentDrawdown    float64 `json:"current_drawdown"`
	RiskSharpe         float64 `json:"risk_sharpe"`
	RiskSortino        float64 `json:"risk_sortino"`
	RiskDrawdown       float64 `json:"risk_drawdown"`
	RiskMaxDrawdown    float64 `json:"risk_max_drawdown"`
	RiskExpectancy     float64 `json:"risk_expectancy"`
	RiskSampleSize     int     `json:"risk_sample_size"`
	StrategyPhase      string  `json:"strategy_phase"`
	PhaseMinConfidence float64 `json:"phase_min_confidence"`
	PhaseMaxCapitalPct float64 `json:"phase_max_capital_pct"`
	MilestoneProgress  float64 `json:"milestone_progress"`
	NoFillMinutes      float64 `json:"no_fill_minutes"`
	DriftActive        bool    `json:"state_drift_active"`
	RecoveryMode       string  `json:"recovery_mode,omitempty"`
	RecoveryEntryOK    bool    `json:"recovery_entry_allowed"`
	RecoveryCleanCycle int     `json:"recovery_clean_cycles"`
}

type AIScalpingService struct {
	config        AIScalpingConfig
	llmClient     llm.Client
	skillRegistry *skill.Registry
	ccxtService   ccxt.CCXTService
	orderExecutor ScalpingOrderExecutor
	tradeMemory   *TradeMemory
	runtimeMu     sync.RWMutex
	runtimeState  AIScalpingRuntimeState
	pairCacheMu   sync.RWMutex
	cachedPairs   []string
	cacheExchange string
	cacheUpdated  time.Time
	symbolGuardMu sync.Mutex
	symbolGuards  map[string]symbolExecutionGuard
	autonomyMu    sync.RWMutex
	autonomy      *ScalpingAutonomyCoordinator
	autonomyState AIScalpingAutonomyState
}

type symbolExecutionGuard struct {
	LastSuccess       time.Time
	FailureWindowFrom time.Time
	FailureCount      int
	LossWindowFrom    time.Time
	LossStreak        int
	LastLoss          time.Time
}

const (
	reasonCategoryLLMTimeout           = "llm_timeout"
	reasonCategoryLLMParseContract     = "llm_parse_contract"
	reasonCategoryExecutionUnavailable = "execution_unavailable"
	reasonCategoryStrategyHold         = "strategy_hold"
)

type AIScalpingRuntimeState struct {
	LastProvider           string    `json:"last_provider"`
	LastSuccessfulProvider string    `json:"last_successful_provider"`
	LastError              string    `json:"last_error"`
	LastErrorAt            time.Time `json:"last_error_at"`
	LastSuccessAt          time.Time `json:"last_success_at"`
	LastReasonCategory     string    `json:"last_reason_category"`
	FailoverAttempted      bool      `json:"failover_attempted"`
	FailoverSucceeded      bool      `json:"failover_succeeded"`
	FailoverProviders      []string  `json:"failover_providers"`
	FailedProviders        []string  `json:"failed_providers"`
}

type AIScalpingAutonomyState struct {
	StrategyID          string
	RolloutStage        string
	RolloutStatus       string
	GateOpen            bool
	GateBlockReasons    []string
	GateChecks          map[string]bool
	LastEvaluated       time.Time
	LastError           string
	LastRollbackAt      time.Time
	LastRollbackReason  string
	LastRollbackTrigger string
}

// SetExchange updates the default exchange for legacy callers.
func (s *AIScalpingService) SetExchange(exchange string) {
	s.config.Exchange = exchange
	log.Printf("[AI-SCALPING] Exchange set to: %s", exchange)
}

func (s *AIScalpingService) exchangeForContext(ctx context.Context) string {
	if exchange := scalpingExchangeFromContext(ctx); exchange != "" {
		return exchange
	}
	return strings.TrimSpace(s.config.Exchange)
}

func NewAIScalpingService(
	config AIScalpingConfig,
	llmClient llm.Client,
	skillRegistry *skill.Registry,
	ccxtService ccxt.CCXTService,
	orderExecutor ScalpingOrderExecutor,
	tradeMemory *TradeMemory,
) *AIScalpingService {
	return &AIScalpingService{
		config:        config,
		llmClient:     llmClient,
		skillRegistry: skillRegistry,
		ccxtService:   ccxtService,
		orderExecutor: orderExecutor,
		tradeMemory:   tradeMemory,
		symbolGuards:  make(map[string]symbolExecutionGuard),
	}
}

// RuntimeDiagnostics returns the latest AI runtime diagnostics for operator visibility.
func (s *AIScalpingService) RuntimeDiagnostics() map[string]interface{} {
	s.runtimeMu.RLock()
	defer s.runtimeMu.RUnlock()

	result := map[string]interface{}{
		"last_provider":            s.runtimeState.LastProvider,
		"last_successful_provider": s.runtimeState.LastSuccessfulProvider,
		"last_error":               s.runtimeState.LastError,
		"last_error_at":            "",
		"last_success_at":          "",
		"last_reason_category":     s.runtimeState.LastReasonCategory,
		"failover_attempted":       s.runtimeState.FailoverAttempted,
		"failover_succeeded":       s.runtimeState.FailoverSucceeded,
		"failover_providers":       append([]string(nil), s.runtimeState.FailoverProviders...),
		"failed_providers":         append([]string(nil), s.runtimeState.FailedProviders...),
	}
	if !s.runtimeState.LastErrorAt.IsZero() {
		result["last_error_at"] = s.runtimeState.LastErrorAt.UTC().Format(time.RFC3339)
	}
	if !s.runtimeState.LastSuccessAt.IsZero() {
		result["last_success_at"] = s.runtimeState.LastSuccessAt.UTC().Format(time.RFC3339)
	}
	result["autonomy"] = s.AutonomyDiagnostics()
	return result
}

// SetAutonomyCoordinator wires staged rollout/live gate enforcement into scalping execution.
func (s *AIScalpingService) SetAutonomyCoordinator(coordinator *ScalpingAutonomyCoordinator) {
	s.autonomyMu.Lock()
	defer s.autonomyMu.Unlock()
	s.autonomy = coordinator
}

func (s *AIScalpingService) autonomyCoordinator() *ScalpingAutonomyCoordinator {
	s.autonomyMu.RLock()
	defer s.autonomyMu.RUnlock()
	return s.autonomy
}

// AutonomyDiagnostics returns latest rollout/gate status captured during scalping evaluation.
func (s *AIScalpingService) AutonomyDiagnostics() map[string]interface{} {
	s.autonomyMu.RLock()
	defer s.autonomyMu.RUnlock()

	result := map[string]interface{}{
		"strategy_id":           s.autonomyState.StrategyID,
		"rollout_stage":         s.autonomyState.RolloutStage,
		"rollout_status":        s.autonomyState.RolloutStatus,
		"gate_open":             s.autonomyState.GateOpen,
		"gate_block_reasons":    append([]string(nil), s.autonomyState.GateBlockReasons...),
		"gate_checks":           copyBoolMap(s.autonomyState.GateChecks),
		"last_evaluated_at":     "",
		"last_error":            s.autonomyState.LastError,
		"last_rollback_at":      "",
		"last_rollback_reason":  s.autonomyState.LastRollbackReason,
		"last_rollback_trigger": s.autonomyState.LastRollbackTrigger,
	}
	if !s.autonomyState.LastEvaluated.IsZero() {
		result["last_evaluated_at"] = s.autonomyState.LastEvaluated.UTC().Format(time.RFC3339)
	}
	if !s.autonomyState.LastRollbackAt.IsZero() {
		result["last_rollback_at"] = s.autonomyState.LastRollbackAt.UTC().Format(time.RFC3339)
	}
	return result
}

func (s *AIScalpingService) updateAutonomyGateState(
	scope ScalpingAutonomyScope,
	rolloutState *autonomous.RolloutState,
	gateState *autonomous.GateState,
	evalErr error,
) {
	s.autonomyMu.Lock()
	defer s.autonomyMu.Unlock()

	if strings.TrimSpace(scope.StrategyID) != "" {
		s.autonomyState.StrategyID = strings.TrimSpace(scope.StrategyID)
	}
	if rolloutState != nil {
		s.autonomyState.RolloutStage = string(rolloutState.CurrentStage)
		s.autonomyState.RolloutStatus = string(rolloutState.Status)
	}
	if gateState != nil {
		s.autonomyState.GateOpen = gateState.IsOpen
		s.autonomyState.GateBlockReasons = append([]string(nil), gateState.BlockReasons...)
		s.autonomyState.GateChecks = map[string]bool{
			"safe_mode_off":         gateState.Checks.SafeModeOff,
			"kill_switch_off":       gateState.Checks.KillSwitchOff,
			"strategy_live":         gateState.Checks.StrategyLive,
			"risk_budget_available": gateState.Checks.RiskBudgetAvailable,
			"exchange_connected":    gateState.Checks.ExchangeConnected,
		}
		s.autonomyState.LastEvaluated = gateState.LastEvaluated
	} else {
		s.autonomyState.GateOpen = false
		s.autonomyState.GateBlockReasons = []string{}
		s.autonomyState.GateChecks = map[string]bool{}
		s.autonomyState.LastEvaluated = time.Time{}
	}
	if evalErr != nil {
		s.autonomyState.LastError = evalErr.Error()
	} else {
		s.autonomyState.LastError = ""
	}
}

func (s *AIScalpingService) updateAutonomyRollbackState(event *autonomous.RollbackEvent) {
	if event == nil {
		return
	}
	s.autonomyMu.Lock()
	defer s.autonomyMu.Unlock()
	s.autonomyState.LastRollbackAt = event.Timestamp.UTC()
	s.autonomyState.LastRollbackReason = strings.TrimSpace(event.Reason)
	s.autonomyState.LastRollbackTrigger = string(event.Trigger)
}

func (s *AIScalpingService) updateRuntimeState(
	reasonCategory string,
	err error,
	success bool,
	provider string,
	failoverInfo llm.FailoverAttemptInfo,
) {
	s.runtimeMu.Lock()
	defer s.runtimeMu.Unlock()

	provider = strings.TrimSpace(provider)
	if provider == "" {
		provider = strings.TrimSpace(failoverInfo.SuccessProvider)
	}
	if provider != "" {
		s.runtimeState.LastProvider = provider
	}
	if success {
		s.runtimeState.LastSuccessfulProvider = s.runtimeState.LastProvider
		s.runtimeState.LastSuccessAt = time.Now().UTC()
		s.runtimeState.LastError = ""
		s.runtimeState.LastErrorAt = time.Time{}
	} else if err != nil {
		s.runtimeState.LastError = err.Error()
		s.runtimeState.LastErrorAt = time.Now().UTC()
	}

	s.runtimeState.LastReasonCategory = strings.TrimSpace(reasonCategory)
	s.runtimeState.FailoverAttempted = failoverInfo.FailoverAttempted
	s.runtimeState.FailoverSucceeded = failoverInfo.FailoverSucceeded
	s.runtimeState.FailoverProviders = append([]string(nil), failoverInfo.AttemptedProviders...)
	s.runtimeState.FailedProviders = append([]string(nil), failoverInfo.FailedProviders...)
}

func (s *AIScalpingService) getLatestFailoverAttemptInfo() llm.FailoverAttemptInfo {
	type statsProvider interface {
		Stats() llm.FailoverStats
	}
	if provider, ok := s.llmClient.(statsProvider); ok {
		return provider.Stats().LastAttempt
	}
	return llm.FailoverAttemptInfo{}
}

func (s *AIScalpingService) ExecuteTradingCycle(ctx context.Context, portfolio TradingPortfolio) (*AITradingDecision, error) {
	log.Printf("[AI-SCALPING] Starting trading cycle for portfolio: %.2f USDT", portfolio.USDTBalance)
	ctx, cancel := context.WithTimeout(ctx, s.config.Timeout)
	defer cancel()

	signals, err := s.gatherMarketSignals(ctx)
	if err != nil {
		log.Printf("[AI-SCALPING] Failed to gather signals: %v", err)
		return nil, fmt.Errorf("failed to gather market signals: %w", err)
	}
	log.Printf("[AI-SCALPING] Gathered %d market signals", len(signals))

	decision, err := s.getAIDecision(ctx, signals, portfolio)
	if err != nil {
		log.Printf("[AI-SCALPING] Failed to get AI decision: %v", err)
		return fallbackHoldDecision(err.Error(), err), nil
	}

	decision.Action = strings.ToLower(strings.TrimSpace(decision.Action))
	decision.Symbol = normalizeSymbolForComparison(decision.Symbol)
	if decision.Action == "hold" {
		decision.ReasonCategory = normalizeHoldReasonCategory(decision.ReasonCategory, decision.Reasoning)
		if isRuntimeReasonCategory(decision.ReasonCategory) {
			decision.ConfidenceKnown = false
			decision.Confidence = 0
		} else if !decision.ConfidenceKnown {
			decision.ConfidenceKnown = true
		}
	} else {
		if decision.ReasonCategory == "" {
			decision.ReasonCategory = reasonCategoryStrategyHold
		}
		if !decision.ConfidenceKnown {
			decision.ConfidenceKnown = true
		}
	}

	log.Printf("[AI-SCALPING] AI decision: %s %s (confidence: %.2f)", decision.Action, decision.Symbol, decision.Confidence)

	if err := s.validateDecision(decision, signals); err != nil {
		if isDecisionContractValidationError(decision, err) {
			runtimeErr := fmt.Errorf("invalid model decision contract: %w", err)
			s.updateRuntimeState(
				reasonCategoryLLMParseContract,
				runtimeErr,
				false,
				"",
				s.getLatestFailoverAttemptInfo(),
			)
			return runtimeDegradedHoldDecision(runtimeErr.Error(), reasonCategoryLLMParseContract), nil
		}
		return strategyHoldDecision(err.Error(), decision.Confidence), nil
	}

	effectiveMinConfidence, effectiveMaxCapital := s.dynamicRiskThresholds(ctx, portfolio)
	if portfolio.PhaseMinConfidence > 0 && portfolio.PhaseMinConfidence > effectiveMinConfidence {
		effectiveMinConfidence = portfolio.PhaseMinConfidence
	}
	if portfolio.PhaseMaxCapitalPct > 0 && portfolio.PhaseMaxCapitalPct < effectiveMaxCapital {
		effectiveMaxCapital = portfolio.PhaseMaxCapitalPct
	}
	if portfolio.MilestoneProgress > 0 && portfolio.MilestoneProgress < 25 {
		effectiveMaxCapital = effectiveMaxCapital * 0.8
	}
	if portfolio.RiskSampleSize >= 10 && portfolio.RiskExpectancy < 0 {
		effectiveMaxCapital = effectiveMaxCapital * 0.75
	}
	if portfolio.RiskDrawdown > 0.12 {
		effectiveMinConfidence += 0.05
		effectiveMaxCapital = effectiveMaxCapital * 0.7
	}
	if effectiveMinConfidence > 0.95 {
		effectiveMinConfidence = 0.95
	}
	if effectiveMaxCapital < 0.1 {
		effectiveMaxCapital = 0.1
	}
	gate := s.evaluatePreTradeGate(ctx, decision, signals)
	if !gate.Allowed {
		attemptedAction := decision.Action
		decision.Action = "hold"
		decision.Confidence = 0
		decision.OrderID = ""
		decision.Reasoning = gate.Reason
		decision.ReasonCategory = reasonCategoryStrategyHold
		decision.ConfidenceKnown = true
		log.Printf(
			"[AI-SCALPING] Pre-trade gate blocked %s %s (regime=%s expectancy=%.4f sample=%d): %s",
			attemptedAction,
			decision.Symbol,
			gate.Regime,
			gate.NetExpectancy,
			gate.SampleSize,
			gate.Reason,
		)
		return decision, nil
	}
	if gate.CapitalMultiplier > 0 && gate.CapitalMultiplier < 1 {
		effectiveMaxCapital = effectiveMaxCapital * gate.CapitalMultiplier
		if effectiveMaxCapital < 0.1 {
			effectiveMaxCapital = 0.1
		}
	}
	log.Printf(
		"[AI-SCALPING] Dynamic thresholds: min_confidence=%.2f max_capital_pct=%.2f regime=%s expectancy=%.4f expectancy_n=%d phase=%s risk_drawdown=%.4f",
		effectiveMinConfidence,
		effectiveMaxCapital,
		gate.Regime,
		gate.NetExpectancy,
		gate.SampleSize,
		portfolio.StrategyPhase,
		portfolio.RiskDrawdown,
	)

	if decision.Action == "hold" {
		decision.ReasonCategory = normalizeHoldReasonCategory(decision.ReasonCategory, decision.Reasoning)
		if isRuntimeReasonCategory(decision.ReasonCategory) {
			decision.ConfidenceKnown = false
			decision.Confidence = 0
		} else {
			decision.ConfidenceKnown = true
		}
		log.Printf("[AI-SCALPING] AI decided to hold: %s", decision.Reasoning)
		return decision, nil
	}

	if decision.Confidence < effectiveMinConfidence {
		log.Printf("[AI-SCALPING] Confidence %.2f below minimum %.2f, skipping", decision.Confidence, effectiveMinConfidence)
		return strategyHoldDecision(
			fmt.Sprintf("confidence %.2f below dynamic threshold %.2f", decision.Confidence, effectiveMinConfidence),
			decision.Confidence,
		), nil
	}

	scope, hasScope := scalpingAutonomyScopeFromContext(ctx)
	if !hasScope {
		scope = ScalpingAutonomyScope{
			ExchangeConnected: true,
			ConnectionChecked: true,
		}
	}
	if strings.TrimSpace(scope.Exchange) == "" {
		scope.Exchange = s.exchangeForContext(ctx)
	}
	if strings.TrimSpace(scope.StrategyID) == "" {
		scope.StrategyID = ScalpingStrategyID(scope.ChatID)
	}

	if s.config.AutoExecute && s.orderExecutor != nil {
		autonomyCoordinator := s.autonomyCoordinator()
		strategyResolved := strings.TrimSpace(scope.StrategyID) != ""
		if autonomyCoordinator != nil && strategyResolved {
			gateState, rolloutState, gateErr := autonomyCoordinator.EvaluatePreExecution(
				ctx,
				scope,
				decision,
				portfolio,
				decimal.NewFromFloat(effectiveMaxCapital),
			)
			s.updateAutonomyGateState(scope, rolloutState, gateState, gateErr)
			if gateErr != nil {
				log.Printf("[AI-SCALPING] Autonomous gate evaluation failed: %v", gateErr)
				return strategyHoldDecision(
					fmt.Sprintf("autonomy gate evaluation failed: %v", gateErr),
					decision.Confidence,
				), nil
			}
			if gateState != nil && !gateState.IsOpen {
				reason := strings.Join(gateState.BlockReasons, "; ")
				if strings.TrimSpace(reason) == "" {
					reason = "autonomy live gate closed"
				}
				log.Printf("[AI-SCALPING] Autonomous live gate blocked execution for %s: %s", scope.StrategyID, reason)
				return strategyHoldDecision(
					fmt.Sprintf("autonomy live gate closed: %s", reason),
					decision.Confidence,
				), nil
			}
		}
		if autonomyCoordinator != nil && !strategyResolved {
			log.Printf(
				"[AI-SCALPING] Skipping autonomy gate: unresolved strategy_id (chat_id=%q)",
				strings.TrimSpace(scope.ChatID),
			)
		}

		executionErr := s.executeDecision(ctx, decision, portfolio, effectiveMaxCapital)
		if autonomyCoordinator != nil && strategyResolved {
			if recordErr := autonomyCoordinator.RecordExecutionResult(ctx, scope, decision, portfolio, executionErr); recordErr != nil {
				log.Printf("[AI-SCALPING] Failed to record autonomy rollout metrics: %v", recordErr)
			} else if rollback := autonomyCoordinator.LastRollback(scope.StrategyID); rollback != nil {
				s.updateAutonomyRollbackState(rollback)
			}
		}
		if autonomyCoordinator != nil && !strategyResolved {
			log.Printf(
				"[AI-SCALPING] Skipping autonomy metrics update: unresolved strategy_id (chat_id=%q)",
				strings.TrimSpace(scope.ChatID),
			)
		}
		if executionErr != nil {
			if shouldDowngradeExecutionErrorToHold(executionErr) {
				decision.Action = "hold"
				decision.Confidence = 0
				decision.OrderID = ""
				decision.Reasoning = buildExecutionFallbackReason(executionErr)
				decision.ReasonCategory = reasonCategoryExecutionUnavailable
				decision.ConfidenceKnown = false
				log.Printf("[AI-SCALPING] Downgrading execution issue to HOLD: %v", executionErr)
				return decision, nil
			}
			return decision, fmt.Errorf("execution failed: %w", executionErr)
		}
	}

	return decision, nil
}

type aiMarketSignal struct {
	Symbol             string  `json:"symbol"`
	Price              float64 `json:"price"`
	High24h            float64 `json:"high_24h"`
	Low24h             float64 `json:"low_24h"`
	Volume24h          float64 `json:"volume_24h"`
	BidAskSpread       float64 `json:"spread_pct"`
	OrderBookImbalance float64 `json:"ob_imbalance"`
	PriceChange24h     float64 `json:"price_change_24h_pct"`
	RangePosition24h   float64 `json:"range_pos_24h"`
}

func (s *AIScalpingService) discoverTradingPairs(ctx context.Context) ([]string, error) {
	exchange := s.exchangeForContext(ctx)
	if cached := s.getCachedPairs(exchange); len(cached) > 0 {
		return cached, nil
	}

	markets, err := s.ccxtService.FetchMarkets(ctx, exchange)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch markets: %w", err)
	}

	var candidates []string
	seen := make(map[string]struct{})
	for _, symbol := range markets.Symbols {
		comparison := normalizeSymbolForComparison(symbol)
		if comparison == "" {
			continue
		}
		// Support spot and perp variants like BTC/USDT and BTC/USDT:USDT.
		if !strings.Contains(comparison, "/USDT") {
			continue
		}
		if _, ok := seen[comparison]; ok {
			continue
		}
		seen[comparison] = struct{}{}
		candidates = append(candidates, symbol)
	}

	if len(candidates) == 0 {
		return nil, fmt.Errorf("no USDT pairs discovered")
	}

	// The executor is futures-first. Prefer symbols that are present in funding-rate
	// universe so trade candidates map to futures contracts.
	if exchange == "bitget" {
		if filtered := s.filterFuturesSymbols(ctx, exchange, candidates); len(filtered) > 0 {
			candidates = filtered
		} else if s.config.EnforceFutures {
			return nil, fmt.Errorf("futures universe prefilter returned no tradable symbols")
		}
	}

	// Bound the scoring universe to keep the AI loop responsive on exchanges with
	// thousands of pairs.
	maxCandidates := s.config.MaxCandidatePairs
	if maxCandidates <= 0 {
		maxCandidates = 200
	}
	if len(candidates) > maxCandidates {
		candidates = candidates[:maxCandidates]
	}

	// Dynamically rank discovered symbols by liquidity + spread + intraday movement.
	scored, err := s.ccxtService.FetchMarketData(ctx, []string{exchange}, candidates)
	if err != nil {
		var partialErr *ccxt.PartialMarketDataError
		if errors.As(err, &partialErr) && len(partialErr.Data) > 0 {
			scored = partialErr.Data
			log.Printf("[AI-SCALPING] Dynamic pair scoring using partial market data: %v", err)
		}
	}
	if len(scored) == 0 {
		limit := s.config.MaxPairsToAnalyze
		if limit > len(candidates) {
			limit = len(candidates)
		}
		log.Printf("[AI-SCALPING] Dynamic pair scoring unavailable (%v), using discovered subset", err)
		selected := candidates[:limit]
		s.updatePairCache(exchange, selected)
		return selected, nil
	}

	type pairScore struct {
		symbol string
		score  float64
	}
	pairs := make([]pairScore, 0, len(scored))
	for _, t := range scored {
		symbol := t.GetSymbol()
		price := t.GetPrice()
		if symbol == "" || price <= 0 {
			continue
		}
		vol := math.Max(t.GetVolume(), 0)
		spreadPct := 0.0
		if t.GetBid() > 0 && t.GetAsk() > 0 {
			spreadPct = ((t.GetAsk() - t.GetBid()) / price) * 100
		}
		rangePct := 0.0
		if t.GetHigh() > 0 && t.GetLow() > 0 {
			rangePct = ((t.GetHigh() - t.GetLow()) / price) * 100
		}
		liqScore := math.Log1p(vol)
		spreadPenalty := 1.0 / (1.0 + math.Max(spreadPct, 0))
		volatilityBoost := 1.0 + math.Max(rangePct, 0)
		score := liqScore * spreadPenalty * volatilityBoost
		pairs = append(pairs, pairScore{symbol: symbol, score: score})
	}

	sort.Slice(pairs, func(i, j int) bool {
		return pairs[i].score > pairs[j].score
	})

	limit := s.config.MaxPairsToAnalyze
	if limit > len(pairs) {
		limit = len(pairs)
	}
	selected := make([]string, 0, limit)
	for i := 0; i < limit; i++ {
		selected = append(selected, pairs[i].symbol)
	}

	log.Printf("[AI-SCALPING] Dynamically selected %d/%d pairs for AI analysis on %s", len(selected), len(candidates), exchange)
	s.updatePairCache(exchange, selected)
	return selected, nil
}

func (s *AIScalpingService) getCachedPairs(exchange string) []string {
	s.pairCacheMu.RLock()
	defer s.pairCacheMu.RUnlock()

	if len(s.cachedPairs) == 0 {
		return nil
	}
	if s.cacheExchange != exchange {
		return nil
	}
	if time.Since(s.cacheUpdated) > 2*time.Minute {
		return nil
	}

	result := make([]string, len(s.cachedPairs))
	copy(result, s.cachedPairs)
	return result
}

func (s *AIScalpingService) updatePairCache(exchange string, pairs []string) {
	if len(pairs) == 0 {
		return
	}
	s.pairCacheMu.Lock()
	defer s.pairCacheMu.Unlock()

	s.cachedPairs = append(s.cachedPairs[:0], pairs...)
	s.cacheExchange = exchange
	s.cacheUpdated = time.Now()
}

func (s *AIScalpingService) filterFuturesSymbols(ctx context.Context, exchange string, symbols []string) []string {
	rates, err := s.ccxtService.FetchAllFundingRates(ctx, exchange)
	if err != nil || len(rates) == 0 {
		if err != nil {
			log.Printf("[AI-SCALPING] Futures universe unavailable on %s: %v", exchange, err)
		}
		return nil
	}

	allowed := make(map[string]struct{}, len(rates)*2)
	for _, rate := range rates {
		normalized := normalizeFuturesSymbol(rate.Symbol)
		if normalized == "" {
			continue
		}
		allowed[normalized] = struct{}{}
		allowed[strings.ReplaceAll(normalized, "/", "")] = struct{}{}
	}

	filtered := make([]string, 0, len(symbols))
	for _, symbol := range symbols {
		normalized := normalizeSymbolForComparison(symbol)
		if normalized == "" {
			continue
		}
		if _, ok := allowed[normalized]; ok {
			filtered = append(filtered, symbol)
			continue
		}
		if _, ok := allowed[strings.ReplaceAll(normalized, "/", "")]; ok {
			filtered = append(filtered, symbol)
		}
	}

	if len(filtered) == 0 {
		log.Printf("[AI-SCALPING] Futures universe filter returned no overlap on %s; using discovered pairs", exchange)
		return nil
	}

	log.Printf("[AI-SCALPING] Futures universe filtered %d -> %d symbols on %s", len(symbols), len(filtered), exchange)
	return filtered
}

func (s *AIScalpingService) gatherMarketSignals(ctx context.Context) ([]aiMarketSignal, error) {
	var signals []aiMarketSignal
	exchange := s.exchangeForContext(ctx)

	pairs, err := s.discoverTradingPairs(ctx)
	if err != nil {
		log.Printf("[AI-SCALPING] Failed dynamic pair discovery: %v", err)
		return nil, fmt.Errorf("dynamic pair discovery unavailable: %w", err)
	}
	if len(pairs) == 0 {
		return nil, fmt.Errorf("dynamic pair discovery returned no symbols")
	}

	// Bulk ticker fetch keeps the cycle responsive under high symbol counts.
	tickerBySymbol := make(map[string]ccxt.MarketPriceInterface, len(pairs))
	marketData, bulkErr := s.ccxtService.FetchMarketData(ctx, []string{exchange}, pairs)
	if bulkErr != nil {
		var partialErr *ccxt.PartialMarketDataError
		if errors.As(bulkErr, &partialErr) && len(partialErr.Data) > 0 {
			marketData = partialErr.Data
			log.Printf("[AI-SCALPING] Bulk ticker fetch returned partial data: %v", bulkErr)
		}
	}
	if bulkErr == nil || len(marketData) > 0 {
		for _, t := range marketData {
			if t == nil {
				continue
			}
			key := normalizeSymbolForComparison(t.GetSymbol())
			if key == "" {
				continue
			}
			tickerBySymbol[key] = t
		}
	}
	if bulkErr != nil && len(marketData) == 0 {
		log.Printf("[AI-SCALPING] Bulk ticker fetch unavailable: %v", bulkErr)
	}

	orderBookPairs := s.config.OrderBookPairs
	if orderBookPairs <= 0 {
		orderBookPairs = 4
	}

	log.Printf("[AI-SCALPING] Analyzing %d pairs on %s", len(pairs), exchange)
	for idx, symbol := range pairs {
		normalizedSymbol := normalizeSymbolForComparison(symbol)
		tickerData, ok := tickerBySymbol[normalizedSymbol]
		if !ok {
			tickerData, err = s.ccxtService.FetchSingleTicker(ctx, exchange, symbol)
			if err != nil {
				log.Printf("[AI-SCALPING] Failed to fetch ticker for %s: %v", symbol, err)
				continue
			}
		}

		var obResp *ccxt.OrderBookResponse
		if idx < orderBookPairs {
			obResp, err = s.ccxtService.FetchOrderBook(ctx, exchange, symbol, 20)
			if err != nil {
				log.Printf("[AI-SCALPING] Failed to fetch orderbook for %s: %v", symbol, err)
			}
		}

		signal := aiMarketSignal{
			Symbol:    normalizeSymbolForComparison(tickerData.GetSymbol()),
			Price:     tickerData.GetPrice(),
			High24h:   tickerData.GetHigh(),
			Low24h:    tickerData.GetLow(),
			Volume24h: tickerData.GetVolume(),
		}
		if signal.Symbol == "" {
			signal.Symbol = normalizedSymbol
		}

		if signal.High24h > signal.Low24h {
			// This is the relative location of current price inside today's range.
			signal.RangePosition24h = ((signal.Price - signal.Low24h) / (signal.High24h - signal.Low24h)) * 100
		}

		if obResp != nil {
			ob := obResp.OrderBook
			if len(ob.Bids) > 0 && len(ob.Asks) > 0 {
				bidVol := sumDecimalOrderVolume(ob.Bids, 5)
				askVol := sumDecimalOrderVolume(ob.Asks, 5)
				total := bidVol + askVol
				if total > 0 {
					signal.OrderBookImbalance = (bidVol - askVol) / total
				}
				bestBid := ob.Bids[0].Price.InexactFloat64()
				bestAsk := ob.Asks[0].Price.InexactFloat64()
				if signal.Price > 0 {
					signal.BidAskSpread = (bestAsk - bestBid) / signal.Price * 100
				}
			}
		}

		signals = append(signals, signal)
	}

	if len(signals) == 0 {
		return nil, fmt.Errorf("no market signals available from exchange")
	}

	return signals, nil
}

func (s *AIScalpingService) getAIDecision(ctx context.Context, signals []aiMarketSignal, portfolio TradingPortfolio) (*AITradingDecision, error) {
	systemPrompt := s.buildSystemPrompt()
	userPrompt := s.buildUserPrompt(ctx, signals, portfolio)

	log.Printf("[AI-SCALPING] Calling LLM with %d signals", len(signals))
	log.Printf("[AI-SCALPING] === SYSTEM PROMPT ===\n%s", systemPrompt)
	log.Printf("[AI-SCALPING] === USER PROMPT ===\nPortfolio: %.2f USDT, Signals: %d", portfolio.USDTBalance, len(signals))

	req := &llm.CompletionRequest{
		Model: strings.TrimSpace(s.config.Model),
		Messages: []llm.Message{
			{Role: llm.RoleSystem, Content: systemPrompt},
			{Role: llm.RoleUser, Content: userPrompt},
		},
		Temperature:    floatPtr(0.3),
		MaxTokens:      s.config.MaxTokens,
		ResponseFormat: &llm.ResponseFormat{Type: "json_object"},
	}
	if req.Model == "" {
		req.Model = "glm-5"
	}
	if req.MaxTokens <= 0 {
		req.MaxTokens = 1200
	}

	log.Printf("[AI-SCALPING] Sending LLM request...")

	inferenceCtx, cancelInference := s.withInferenceBudget(ctx)
	defer cancelInference()

	resp, err := s.llmClient.Complete(inferenceCtx, req)
	if err != nil {
		s.updateRuntimeState(
			classifyReasonCategory(err, ""),
			err,
			false,
			"",
			s.getLatestFailoverAttemptInfo(),
		)
		log.Printf("[AI-SCALPING] LLM completion failed: %v", err)
		return nil, fmt.Errorf("LLM completion failed: %w", err)
	}

	log.Printf("[AI-SCALPING] === LLM RESPONSE ===\nLatency: %dms\nRaw: %s", resp.LatencyMs, resp.Message.Content)

	decision, parseErr := parseAIDecisionPayload(resp.Message.Content)
	if parseErr != nil || !isValidDecisionAction(decision.Action) {
		log.Printf("[AI-SCALPING] Failed to parse AI response: %s", resp.Message.Content)
		decision, err = s.parseDecisionWithRetries(ctx, resp.Message.Content)
		if err != nil {
			log.Printf("[AI-SCALPING] Structured-output retries exhausted: %v", err)
			hold := fallbackHoldDecision(resp.Message.Content, err)
			s.updateRuntimeState(
				hold.ReasonCategory,
				err,
				false,
				string(resp.Provider),
				s.getLatestFailoverAttemptInfo(),
			)
			return hold, nil
		}
	}
	if decision.Action == "hold" {
		decision.ReasonCategory = normalizeHoldReasonCategory(decision.ReasonCategory, decision.Reasoning)
		if isRuntimeReasonCategory(decision.ReasonCategory) {
			decision.ConfidenceKnown = false
			decision.Confidence = 0
		}
	} else {
		if decision.ReasonCategory == "" {
			decision.ReasonCategory = reasonCategoryStrategyHold
		}
		if !decision.ConfidenceKnown {
			decision.ConfidenceKnown = true
		}
	}
	s.updateRuntimeState(
		decision.ReasonCategory,
		nil,
		true,
		string(resp.Provider),
		s.getLatestFailoverAttemptInfo(),
	)

	stopLossFloat := 0.0
	takeProfitFloat := 0.0
	if decision.StopLoss != nil {
		stopLossFloat = decision.StopLoss.InexactFloat64()
	}
	if decision.TakeProfit != nil {
		takeProfitFloat = decision.TakeProfit.InexactFloat64()
	}
	log.Printf("[AI-SCALPING] === AI DECISION PARSED ===\nAction: %s, Symbol: %s, Confidence: %.0f%%, Size: %.1f%%, SL: %.4f, TP: %.4f\nReasoning: %s",
		decision.Action, decision.Symbol, decision.Confidence*100, decision.SizePercent,
		stopLossFloat, takeProfitFloat, decision.Reasoning)

	return decision, nil
}

func (s *AIScalpingService) buildSystemPrompt() string {
	skillContent := ""
	if s.skillRegistry != nil {
		if sk, found := s.skillRegistry.Get("scalping"); found {
			skillContent = sk.Content
		}
	}

	return fmt.Sprintf(`You are an autonomous AI trading agent for cryptocurrency futures scalping.

## Your Role
You analyze market data and make trading decisions. You have access to real-time market signals and portfolio state.

## Trading Rules
1. Only trade when you have HIGH confidence (>%.1f)
2. Maximum position size: %.1f%% of portfolio
3. Use futures with %dx leverage
4. Always consider risk: set stop-loss and take-profit levels
5. If uncertain, return action: "hold" with reasoning

## Response Format
Return JSON only:
{
  "action": "buy" | "sell" | "hold",
  "symbol": "SYMBOL/USDT",
  "size_pct": 1-100,
  "confidence": 0.0-1.0,
  "reasoning": "explanation",
  "stop_loss": 123.45,
  "take_profit": 130.00
}

## Skill Guidelines
%s

## Signal Interpretation
- ob_imbalance > 0.2: Strong buy pressure (more bids)
- ob_imbalance < -0.2: Strong sell pressure (more asks)
- spread < 0.1%%: Good liquidity for execution
- range_pos_24h > 80: Price near daily high (avoid chasing late entries)
- range_pos_24h < 20: Price near daily low (avoid aggressive shorting into support)
`, s.config.MinConfidence, s.config.MaxCapitalPct, s.config.Leverage, skillContent)
}

func (s *AIScalpingService) buildUserPrompt(ctx context.Context, signals []aiMarketSignal, portfolio TradingPortfolio) string {
	signalsJSON, _ := json.MarshalIndent(signals, "", "  ")

	var memoryContext string
	var recoveryContext string
	if s.tradeMemory != nil {
		topSymbol := ""
		if len(signals) > 0 {
			topSymbol = signals[0].Symbol
		}
		currentContext := string(signalsJSON)
		if mem, err := s.tradeMemory.BuildMemoryContext(ctx, topSymbol, currentContext); err == nil {
			memoryContext = "\n" + mem
		}
		// Add recovery-specific context if in drawdown
		if portfolio.RiskDrawdown > 0.05 {
			recoveryContext = "\n" + s.tradeMemory.BuildRecoveryContext(ctx, portfolio.RiskDrawdown)
		}
	}

	return fmt.Sprintf(`Analyze these market signals and make a trading decision.

## Portfolio
- USDT Balance: %.2f
- Total Value: %.2f
- Open Positions: %d
- Unrealized PnL: %.4f

## Autonomous Control Plane
- Strategy Phase: %s
- Phase Min Confidence: %.2f
- Phase Max Capital %%: %.2f
- Fund Milestone Progress: %.2f%%
- No-fill Duration (minutes): %.1f
- State Drift Active: %t
- Risk Sharpe: %.4f
- Risk Sortino: %.4f
- Risk Drawdown: %.4f
- Risk Expectancy: %.6f (%d samples)

## Market Signals
%s%s%s
Based on the signals and past trading history, what is your trading decision? Learn from past mistakes. Adapt your strategy based on recovery context if provided. Return only valid JSON.`,
		portfolio.USDTBalance,
		portfolio.TotalValue,
		portfolio.OpenPositions,
		portfolio.UnrealizedPnL,
		portfolio.StrategyPhase,
		portfolio.PhaseMinConfidence,
		portfolio.PhaseMaxCapitalPct,
		portfolio.MilestoneProgress,
		portfolio.NoFillMinutes,
		portfolio.DriftActive,
		portfolio.RiskSharpe,
		portfolio.RiskSortino,
		portfolio.RiskDrawdown,
		portfolio.RiskExpectancy,
		portfolio.RiskSampleSize,
		string(signalsJSON),
		memoryContext,
		recoveryContext,
	)
}

func (s *AIScalpingService) executeDecision(ctx context.Context, decision *AITradingDecision, portfolio TradingPortfolio, maxCapitalPct float64) error {
	if s.orderExecutor == nil {
		return fmt.Errorf("no order executor configured")
	}
	exchange := s.exchangeForContext(ctx)

	if maxCapitalPct <= 0 {
		maxCapitalPct = s.config.MaxCapitalPct
	}
	if decision.SizePercent > maxCapitalPct {
		decision.SizePercent = maxCapitalPct
	}
	if decision.SizePercent <= 0 || decision.SizePercent > 100 {
		return fmt.Errorf("invalid size_pct %.4f", decision.SizePercent)
	}

	amount := decimal.NewFromFloat(portfolio.USDTBalance * decision.SizePercent / 100)
	if amount.LessThanOrEqual(decimal.Zero) {
		return fmt.Errorf("computed order amount is non-positive")
	}
	if err := s.enforceSymbolGuard(decision.Symbol); err != nil {
		return err
	}

	openOrders, err := s.orderExecutor.GetOpenOrders(ctx, exchange, decision.Symbol)
	if err != nil {
		log.Printf("[AI-SCALPING] Open-order check skipped for %s: %v", decision.Symbol, err)
	} else if len(openOrders) > 0 {
		return fmt.Errorf("open position/order already exists for %s (%d open orders)", decision.Symbol, len(openOrders))
	}

	log.Printf("[AI-SCALPING] Executing: %s %s (%s USDT)", decision.Action, decision.Symbol, amount.String())

	// Build detailed trade info for rich notification
	details := TradeDetails{
		Exchange:          exchange,
		Symbol:            decision.Symbol,
		Side:              decision.Action,
		OrderType:         "market",
		MarketType:        "futures",
		AllowSpotFallback: s.config.AllowSpotFallback,
		Leverage:          s.config.Leverage,
		AmountUSDT:        amount,
		WalletPercent:     decision.SizePercent,
		TakeProfit:        decision.TakeProfit,
		StopLoss:          decision.StopLoss,
		TradeType:         "scalping",
		Confidence:        decision.Confidence,
		Reasoning:         decision.Reasoning,
		EntryPrice:        decision.EntryPrice,
		IsPaperTrade:      s.orderExecutor.IsPaperTrading(),
	}

	// Use PlaceOrderWithDetails for rich notifications
	orderID, err := s.orderExecutor.PlaceOrderWithDetails(ctx, details)
	if err != nil {
		s.recordSymbolGuardResult(decision.Symbol, err)
		return fmt.Errorf("order failed: %w", err)
	}

	decision.OrderID = strings.TrimSpace(orderID)
	s.recordSymbolGuardResult(decision.Symbol, nil)
	log.Printf("[AI-SCALPING] Order placed: %s", orderID)
	return nil
}

func sumDecimalOrderVolume(orders []ccxt.OrderBookEntry, limit int) float64 {
	var total float64
	for i := 0; i < limit && i < len(orders); i++ {
		total += orders[i].Amount.InexactFloat64()
	}
	return total
}

func floatPtr(v float64) *float64 {
	return &v
}

func copyBoolMap(input map[string]bool) map[string]bool {
	if len(input) == 0 {
		return map[string]bool{}
	}
	cloned := make(map[string]bool, len(input))
	for key, value := range input {
		cloned[key] = value
	}
	return cloned
}

const (
	maxScalpingSpreadPct = 0.20
	minRiskRewardRatio   = 1.10
)

type preTradeGateResult struct {
	Allowed           bool
	Regime            string
	NetExpectancy     float64
	SampleSize        int
	CapitalMultiplier float64
	Reason            string
}

func defaultPreTradeGateResult() preTradeGateResult {
	return preTradeGateResult{
		Allowed:           true,
		Regime:            "neutral",
		CapitalMultiplier: 1,
	}
}

func (s *AIScalpingService) evaluatePreTradeGate(ctx context.Context, decision *AITradingDecision, signals []aiMarketSignal) preTradeGateResult {
	result := defaultPreTradeGateResult()
	if !s.config.PreTradeGate || decision == nil || decision.Action == "hold" {
		return result
	}

	known := make(map[string]aiMarketSignal, len(signals))
	for _, sig := range signals {
		known[normalizeSymbolForComparison(sig.Symbol)] = sig
	}
	signal, ok := resolveDecisionSymbol(decision.Symbol, known)
	if !ok {
		result.Allowed = false
		result.Reason = fmt.Sprintf("pre-trade gate: symbol %s missing from signal universe", decision.Symbol)
		return result
	}

	regime, regimeMultiplier, regimeBlockReason := s.classifyScalpingRegime(signal, decision.Action)
	result.Regime = regime
	result.CapitalMultiplier = regimeMultiplier
	if regimeBlockReason != "" {
		result.Allowed = false
		result.Reason = regimeBlockReason
		return result
	}

	expectancy, sample, hasExpectancy := s.estimateNetExpectancy(ctx, signal.Symbol, decision.Action)
	result.NetExpectancy = expectancy
	result.SampleSize = sample
	if hasExpectancy && sample >= s.config.MinExpectancyN && expectancy < s.config.MinExpectancyEdge {
		result.Allowed = false
		result.Reason = fmt.Sprintf(
			"pre-trade expectancy gate blocked %s: net expectancy %.4f below minimum %.4f (%d samples)",
			signal.Symbol,
			expectancy,
			s.config.MinExpectancyEdge,
			sample,
		)
		return result
	}

	// In choppy/non-directional conditions require either strong confidence or proven edge.
	if regime == "chop" && decision.Confidence < 0.65 && (!hasExpectancy || expectancy <= s.config.MinExpectancyEdge) {
		result.Allowed = false
		result.Reason = fmt.Sprintf(
			"pre-trade regime gate blocked %s: choppy market with low confidence (%.2f)",
			signal.Symbol,
			decision.Confidence,
		)
	}
	return result
}

func (s *AIScalpingService) classifyScalpingRegime(signal aiMarketSignal, action string) (string, float64, string) {
	action = strings.ToLower(strings.TrimSpace(action))
	highBand := s.config.RegimeHighBand
	lowBand := s.config.RegimeLowBand
	if highBand <= lowBand {
		highBand = 85
		lowBand = 15
	}

	if signal.BidAskSpread > maxScalpingSpreadPct*0.85 {
		return "illiquid", 0, fmt.Sprintf("pre-trade regime gate blocked %s: spread %.3f%% too wide", signal.Symbol, signal.BidAskSpread)
	}

	if action == "buy" && signal.RangePosition24h >= highBand && signal.OrderBookImbalance < 0 {
		return "blowoff_top", 0, fmt.Sprintf(
			"pre-trade regime gate blocked %s: late-range buy rejected (range_pos_24h=%.1f, ob_imbalance=%.3f)",
			signal.Symbol,
			signal.RangePosition24h,
			signal.OrderBookImbalance,
		)
	}
	if action == "sell" && signal.RangePosition24h <= lowBand && signal.OrderBookImbalance > 0 {
		return "capitulation_bottom", 0, fmt.Sprintf(
			"pre-trade regime gate blocked %s: low-range sell rejected (range_pos_24h=%.1f, ob_imbalance=%.3f)",
			signal.Symbol,
			signal.RangePosition24h,
			signal.OrderBookImbalance,
		)
	}

	if math.Abs(signal.OrderBookImbalance) >= 0.25 && signal.RangePosition24h > lowBand+5 && signal.RangePosition24h < highBand-5 {
		return "trend", 1, ""
	}
	if math.Abs(signal.OrderBookImbalance) < 0.10 {
		return "chop", 0.65, ""
	}
	return "neutral", 0.85, ""
}

func (s *AIScalpingService) estimateNetExpectancy(ctx context.Context, symbol, action string) (float64, int, bool) {
	normalizedSymbol := normalizeSymbolForComparison(symbol)
	normalizedAction := strings.ToLower(strings.TrimSpace(action))
	minSamples := s.config.MinExpectancyN
	if minSamples <= 0 {
		minSamples = 8
	}

	if s.tradeMemory != nil {
		trades, err := s.tradeMemory.GetRecentTrades(ctx, 250)
		if err == nil && len(trades) > 0 {
			sumWin := 0.0
			sumLoss := 0.0
			wins := 0
			losses := 0
			for _, trade := range trades {
				if normalizeSymbolForComparison(trade.Symbol) != normalizedSymbol {
					continue
				}
				if normalizedAction != "" && strings.ToLower(strings.TrimSpace(trade.Action)) != normalizedAction {
					continue
				}
				outcome := strings.ToLower(strings.TrimSpace(trade.Outcome))
				if outcome != "win" && outcome != "loss" {
					continue
				}
				pnl := trade.PnL.Abs().InexactFloat64()
				if pnl <= 0 {
					continue
				}
				if outcome == "win" {
					wins++
					sumWin += pnl
				} else {
					losses++
					sumLoss += pnl
				}
			}
			if wins+losses >= minSamples {
				return calculateNetExpectancy(wins, losses, sumWin, sumLoss), wins + losses, true
			}
		}
	}

	perf := GetScalpingPerformance().GetPerformance()
	total := readIntMetric(perf["total_trades"])
	if total <= 0 {
		return 0, 0, false
	}

	winRate := readFloatMetric(perf["win_rate"])
	if winRate > 1 {
		winRate = winRate / 100
	}
	if winRate < 0 {
		winRate = 0
	}
	if winRate > 1 {
		winRate = 1
	}

	avgWin := math.Abs(readFloatMetric(perf["avg_win"]))
	avgLoss := math.Abs(readFloatMetric(perf["avg_loss"]))
	if avgWin == 0 && avgLoss == 0 {
		return 0, total, false
	}
	return winRate*avgWin - (1-winRate)*avgLoss, total, true
}

func calculateNetExpectancy(wins, losses int, sumWin, sumLoss float64) float64 {
	total := wins + losses
	if total == 0 {
		return 0
	}
	winRate := float64(wins) / float64(total)
	avgWin := 0.0
	if wins > 0 {
		avgWin = sumWin / float64(wins)
	}
	avgLoss := 0.0
	if losses > 0 {
		avgLoss = sumLoss / float64(losses)
	}
	return winRate*avgWin - (1-winRate)*avgLoss
}

func (s *AIScalpingService) validateDecision(decision *AITradingDecision, signals []aiMarketSignal) error {
	if decision == nil {
		return fmt.Errorf("decision is nil")
	}
	if decision.Action != "buy" && decision.Action != "sell" && decision.Action != "hold" {
		return fmt.Errorf("unsupported action: %s", decision.Action)
	}
	if decision.Confidence < 0 || decision.Confidence > 1 {
		return fmt.Errorf("confidence out of range: %.4f", decision.Confidence)
	}
	if decision.Action == "hold" {
		decision.Symbol = ""
		decision.SizePercent = 0
		if strings.TrimSpace(decision.Reasoning) == "" {
			decision.Reasoning = "model selected hold (no detailed reasoning)"
		}
		return nil
	}
	if decision.Symbol == "" {
		return fmt.Errorf("symbol is required for action %s", decision.Action)
	}
	known := make(map[string]aiMarketSignal, len(signals))
	for _, sig := range signals {
		known[normalizeSymbolForComparison(sig.Symbol)] = sig
	}
	resolved, ok := resolveDecisionSymbol(decision.Symbol, known)
	if !ok {
		return fmt.Errorf("symbol %s not in current analyzed universe", decision.Symbol)
	}
	decision.Symbol = resolved.Symbol
	if resolved.Price > 0 {
		entry := decimal.NewFromFloat(resolved.Price)
		decision.EntryPrice = &entry
	}
	if resolved.BidAskSpread > maxScalpingSpreadPct {
		return fmt.Errorf("spread %.3f%% too wide for scalping on %s", resolved.BidAskSpread, decision.Symbol)
	}
	if resolved.BidAskSpread == 0 && resolved.OrderBookImbalance == 0 {
		return fmt.Errorf("missing orderbook quality signals for %s", decision.Symbol)
	}
	if decision.SizePercent <= 0 {
		return fmt.Errorf("size_pct must be > 0")
	}
	if decision.StopLoss == nil || decision.TakeProfit == nil {
		defaultSL, defaultTP := defaultExitLevels(resolved.Price, decision.Action)
		if decision.StopLoss == nil {
			decision.StopLoss = &defaultSL
		}
		if decision.TakeProfit == nil {
			decision.TakeProfit = &defaultTP
		}
		log.Printf("[AI-SCALPING] Applied default SL/TP for %s %s due to incomplete model output", decision.Action, decision.Symbol)
	}
	if decision.StopLoss.LessThanOrEqual(decimal.Zero) || decision.TakeProfit.LessThanOrEqual(decimal.Zero) {
		return fmt.Errorf("stop_loss and take_profit must be positive")
	}
	if resolved.Price <= 0 {
		return fmt.Errorf("invalid market price for symbol %s", resolved.Symbol)
	}
	stopLoss := decision.StopLoss.InexactFloat64()
	takeProfit := decision.TakeProfit.InexactFloat64()
	switch decision.Action {
	case "buy":
		if stopLoss >= resolved.Price || takeProfit <= resolved.Price {
			return fmt.Errorf("buy decision requires stop_loss < price < take_profit")
		}
		if resolved.RangePosition24h >= 85 && decision.Confidence < 0.75 {
			return fmt.Errorf("buy decision rejected near day high (range_pos_24h=%.1f)", resolved.RangePosition24h)
		}
		reward := takeProfit - resolved.Price
		risk := resolved.Price - stopLoss
		if risk <= 0 || reward/risk < minRiskRewardRatio {
			return fmt.Errorf("buy risk/reward %.2f below minimum %.2f", reward/risk, minRiskRewardRatio)
		}
	case "sell":
		if stopLoss <= resolved.Price || takeProfit >= resolved.Price {
			return fmt.Errorf("sell decision requires stop_loss > price > take_profit")
		}
		if resolved.RangePosition24h <= 15 && decision.Confidence < 0.75 {
			return fmt.Errorf("sell decision rejected near day low (range_pos_24h=%.1f)", resolved.RangePosition24h)
		}
		reward := resolved.Price - takeProfit
		risk := stopLoss - resolved.Price
		if risk <= 0 || reward/risk < minRiskRewardRatio {
			return fmt.Errorf("sell risk/reward %.2f below minimum %.2f", reward/risk, minRiskRewardRatio)
		}
	}
	return nil
}

func normalizeSymbolForComparison(symbol string) string {
	normalized := strings.ToUpper(strings.TrimSpace(symbol))
	if normalized == "" {
		return ""
	}
	normalized = strings.ReplaceAll(normalized, "-", "/")
	if idx := strings.Index(normalized, ":"); idx >= 0 {
		normalized = normalized[:idx]
	}
	return normalized
}

func normalizeFuturesSymbol(symbol string) string {
	normalized := strings.ToUpper(strings.TrimSpace(symbol))
	if normalized == "" {
		return ""
	}
	if idx := strings.Index(normalized, ":"); idx >= 0 {
		normalized = normalized[:idx]
	}
	normalized = strings.ReplaceAll(normalized, "-", "")
	normalized = strings.ReplaceAll(normalized, "_", "")
	normalized = strings.ReplaceAll(normalized, "/", "")
	if strings.HasSuffix(normalized, "USDT") && len(normalized) > len("USDT") {
		base := normalized[:len(normalized)-len("USDT")]
		return base + "/USDT"
	}
	return normalized
}

func resolveDecisionSymbol(raw string, known map[string]aiMarketSignal) (aiMarketSignal, bool) {
	normalized := normalizeSymbolForComparison(raw)
	if sig, ok := known[normalized]; ok {
		return sig, true
	}

	compact := strings.ReplaceAll(normalized, "/", "")
	compact = strings.TrimSuffix(compact, "USDT")
	compact = strings.TrimSpace(compact)
	if compact == "" {
		return aiMarketSignal{}, false
	}

	matches := make([]aiMarketSignal, 0, 1)
	for key, sig := range known {
		base := strings.TrimSuffix(strings.ReplaceAll(key, "/", ""), "USDT")
		if strings.HasPrefix(base, compact) || strings.HasPrefix(compact, base) {
			matches = append(matches, sig)
		}
	}

	if len(matches) == 1 {
		return matches[0], true
	}
	return aiMarketSignal{}, false
}

func parseAIDecisionPayload(content string) (*AITradingDecision, error) {
	raw := strings.TrimSpace(content)
	if raw == "" {
		return nil, fmt.Errorf("empty decision payload")
	}

	candidates := make([]string, 0, 4)
	appendCandidate := func(candidate string) {
		candidate = strings.TrimSpace(candidate)
		if candidate == "" {
			return
		}
		for _, existing := range candidates {
			if existing == candidate {
				return
			}
		}
		candidates = append(candidates, candidate)
	}

	cleaned := stripDecisionCodeFence(raw)
	appendCandidate(cleaned)
	if extracted, ok := extractBalancedJSONObject(cleaned); ok {
		appendCandidate(extracted)
	}
	if extracted, ok := extractBalancedJSONObject(raw); ok {
		appendCandidate(extracted)
	}
	appendCandidate(raw)

	var lastErr error
	for _, candidate := range candidates {
		decision, err := parseAIDecisionJSONObject(candidate)
		if err == nil {
			return decision, nil
		}
		lastErr = err
	}

	if lastErr != nil {
		return nil, lastErr
	}
	return nil, fmt.Errorf("unable to parse AI decision payload")
}

func stripDecisionCodeFence(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if !strings.HasPrefix(trimmed, "```") {
		return trimmed
	}

	trimmed = strings.TrimPrefix(trimmed, "```json")
	trimmed = strings.TrimPrefix(trimmed, "```JSON")
	trimmed = strings.TrimPrefix(trimmed, "```")
	trimmed = strings.TrimSuffix(trimmed, "```")
	return strings.TrimSpace(trimmed)
}

func parseAIDecisionJSONObject(raw string) (*AITradingDecision, error) {
	payload := make(map[string]json.RawMessage)
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		return nil, err
	}

	action, err := parseStringRaw(selectDecisionField(payload, "action", "decision", "trade_action"))
	if err != nil {
		return nil, fmt.Errorf("invalid action: %w", err)
	}
	symbol, _ := parseStringRaw(selectDecisionField(payload, "symbol", "pair", "ticker"))
	sizePct, _ := parseFloatRaw(selectDecisionField(payload, "size_pct", "size_percent", "size"))
	confidence, _ := parseFloatRaw(selectDecisionField(payload, "confidence", "probability", "score"))
	reasoning, _ := parseStringRaw(selectDecisionField(payload, "reasoning", "reason", "analysis", "summary"))

	stopLoss, err := parseOptionalDecimal(selectDecisionField(payload, "stop_loss", "sl", "stoploss"))
	if err != nil {
		return nil, fmt.Errorf("invalid stop_loss: %w", err)
	}
	takeProfit, err := parseOptionalDecimal(selectDecisionField(payload, "take_profit", "tp", "takeprofit"))
	if err != nil {
		return nil, fmt.Errorf("invalid take_profit: %w", err)
	}
	reasonCategory, _ := parseStringRaw(selectDecisionField(payload, "reason_category", "hold_category"))
	confidenceKnown, confidenceKnownErr := parseBoolRaw(selectDecisionField(payload, "confidence_known"))
	if confidenceKnownErr != nil {
		confidenceKnown = false
	}

	return &AITradingDecision{
		Action:          action,
		Symbol:          symbol,
		SizePercent:     sizePct,
		Confidence:      confidence,
		Reasoning:       reasoning,
		ReasonCategory:  strings.TrimSpace(strings.ToLower(reasonCategory)),
		ConfidenceKnown: confidenceKnown,
		StopLoss:        stopLoss,
		TakeProfit:      takeProfit,
	}, nil
}

func selectDecisionField(payload map[string]json.RawMessage, keys ...string) json.RawMessage {
	for _, key := range keys {
		if raw, ok := payload[key]; ok {
			return raw
		}
	}
	return nil
}

func parseStringRaw(raw json.RawMessage) (string, error) {
	if len(raw) == 0 {
		return "", fmt.Errorf("field missing")
	}

	var asString string
	if err := json.Unmarshal(raw, &asString); err == nil {
		return strings.TrimSpace(asString), nil
	}

	var asNumber float64
	if err := json.Unmarshal(raw, &asNumber); err == nil {
		return strings.TrimSpace(strconv.FormatFloat(asNumber, 'f', -1, 64)), nil
	}

	return "", fmt.Errorf("unsupported string field %s", string(raw))
}

func parseFloatRaw(raw json.RawMessage) (float64, error) {
	if len(raw) == 0 {
		return 0, fmt.Errorf("field missing")
	}

	var asNumber float64
	if err := json.Unmarshal(raw, &asNumber); err == nil {
		return asNumber, nil
	}

	var asText string
	if err := json.Unmarshal(raw, &asText); err == nil {
		value, parseErr := strconv.ParseFloat(strings.TrimSpace(asText), 64)
		if parseErr != nil {
			return 0, parseErr
		}
		return value, nil
	}

	return 0, fmt.Errorf("unsupported numeric field %s", string(raw))
}

func parseBoolRaw(raw json.RawMessage) (bool, error) {
	if len(raw) == 0 {
		return false, fmt.Errorf("field missing")
	}

	var asBool bool
	if err := json.Unmarshal(raw, &asBool); err == nil {
		return asBool, nil
	}

	var asString string
	if err := json.Unmarshal(raw, &asString); err == nil {
		switch strings.ToLower(strings.TrimSpace(asString)) {
		case "true", "1", "yes", "on":
			return true, nil
		case "false", "0", "no", "off":
			return false, nil
		default:
			return false, fmt.Errorf("unsupported bool string %q", asString)
		}
	}

	return false, fmt.Errorf("unsupported bool field %s", string(raw))
}

func fallbackHoldDecision(content string, parseErr error) *AITradingDecision {
	sanitized := strings.Join(strings.Fields(strings.TrimSpace(content)), " ")
	if len(sanitized) > 180 {
		sanitized = sanitized[:177] + "..."
	}
	if sanitized == "" {
		sanitized = "model response was empty"
	}

	reason := fmt.Sprintf("model response parse fallback: %s", sanitized)
	if parseErr != nil {
		reason = fmt.Sprintf("model response parse fallback (%v): %s", parseErr, sanitized)
	}

	reasonCategory := classifyReasonCategory(parseErr, sanitized)
	if reasonCategory == "" {
		reasonCategory = reasonCategoryLLMParseContract
	}

	return &AITradingDecision{
		Action:          "hold",
		Confidence:      0,
		Reasoning:       reason,
		ReasonCategory:  reasonCategory,
		ConfidenceKnown: false,
		SizePercent:     0,
	}
}

func strategyHoldDecision(reason string, confidence float64) *AITradingDecision {
	if confidence < 0 {
		confidence = 0
	}
	if confidence > 1 {
		confidence = 1
	}
	return &AITradingDecision{
		Action:          "hold",
		Confidence:      confidence,
		Reasoning:       strings.TrimSpace(reason),
		ReasonCategory:  reasonCategoryStrategyHold,
		ConfidenceKnown: true,
		SizePercent:     0,
	}
}

func runtimeDegradedHoldDecision(reason string, category string) *AITradingDecision {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		reason = "runtime-degraded decision fallback"
	}
	category = strings.TrimSpace(category)
	if category == "" {
		category = reasonCategoryExecutionUnavailable
	}
	return &AITradingDecision{
		Action:          "hold",
		Confidence:      0,
		Reasoning:       reason,
		ReasonCategory:  category,
		ConfidenceKnown: false,
		SizePercent:     0,
	}
}

func isDecisionContractValidationError(decision *AITradingDecision, err error) bool {
	if err == nil {
		return false
	}
	if decision == nil {
		return true
	}
	action := strings.ToLower(strings.TrimSpace(decision.Action))
	if action == "hold" {
		return false
	}

	lower := strings.ToLower(strings.TrimSpace(err.Error()))
	switch {
	case strings.Contains(lower, "decision is nil"),
		strings.Contains(lower, "unsupported action"),
		strings.Contains(lower, "confidence out of range"),
		strings.Contains(lower, "symbol is required for action"),
		strings.Contains(lower, "size_pct must be > 0"),
		strings.Contains(lower, "symbol ") && strings.Contains(lower, "not in current analyzed universe"),
		strings.Contains(lower, "invalid market price"),
		strings.Contains(lower, "stop_loss and take_profit must be positive"),
		strings.Contains(lower, "buy decision requires stop_loss < price < take_profit"),
		strings.Contains(lower, "sell decision requires stop_loss > price > take_profit"):
		return true
	default:
		return false
	}
}

func classifyReasonCategory(err error, content string) string {
	msg := ""
	if err != nil {
		msg = strings.ToLower(err.Error())
	}
	if content != "" {
		msg = strings.TrimSpace(msg + " " + strings.ToLower(content))
	}
	switch {
	case strings.Contains(msg, "context deadline exceeded"),
		strings.Contains(msg, "timeout"),
		strings.Contains(msg, "deadline exceeded"):
		return reasonCategoryLLMTimeout
	case strings.Contains(msg, "failed to parse ai decision"),
		strings.Contains(msg, "model response parse fallback"),
		strings.Contains(msg, "invalid model decision contract"),
		strings.Contains(msg, "invalid character"),
		strings.Contains(msg, "json"):
		return reasonCategoryLLMParseContract
	case strings.Contains(msg, "execution unavailable"),
		strings.Contains(msg, "request failed"),
		strings.Contains(msg, "futures-only mode prevented spot fallback"),
		strings.Contains(msg, "failed to get ticker"),
		strings.Contains(msg, "symbol cooldown active"),
		strings.Contains(msg, "symbol loss cooldown active"),
		strings.Contains(msg, "symbol failure budget reached"):
		return reasonCategoryExecutionUnavailable
	default:
		if strings.TrimSpace(msg) == "" {
			return reasonCategoryStrategyHold
		}
		return reasonCategoryExecutionUnavailable
	}
}

func isRuntimeReasonCategory(category string) bool {
	switch strings.ToLower(strings.TrimSpace(category)) {
	case reasonCategoryLLMTimeout, reasonCategoryLLMParseContract, reasonCategoryExecutionUnavailable:
		return true
	default:
		return false
	}
}

func normalizeHoldReasonCategory(category, reasoning string) string {
	category = strings.ToLower(strings.TrimSpace(category))
	inferredRuntime := classifyRuntimeReasoning(reasoning)
	if category == "" {
		if inferredRuntime != "" {
			category = inferredRuntime
		} else {
			category = reasonCategoryStrategyHold
		}
	}
	// Runtime-degraded holds must never be surfaced as strategy holds.
	if category == reasonCategoryStrategyHold && inferredRuntime != "" {
		category = inferredRuntime
	}
	if category == "" {
		category = reasonCategoryStrategyHold
	}
	return category
}

func classifyRuntimeReasoning(reasoning string) string {
	lower := strings.ToLower(strings.TrimSpace(reasoning))
	if lower == "" {
		return ""
	}
	switch {
	case strings.Contains(lower, "context deadline exceeded"),
		strings.Contains(lower, "timeout"),
		strings.Contains(lower, "deadline exceeded"):
		return reasonCategoryLLMTimeout
	case strings.Contains(lower, "model response parse fallback"),
		strings.Contains(lower, "invalid model decision contract"),
		strings.Contains(lower, "failed to parse ai decision"),
		strings.Contains(lower, "invalid character"),
		strings.Contains(lower, "json"):
		return reasonCategoryLLMParseContract
	case strings.Contains(lower, "execution unavailable"),
		strings.Contains(lower, "request failed"),
		strings.Contains(lower, "futures-only mode prevented spot fallback"),
		strings.Contains(lower, "failed to get ticker"),
		strings.Contains(lower, "symbol cooldown active"),
		strings.Contains(lower, "symbol loss cooldown active"),
		strings.Contains(lower, "symbol failure budget reached"),
		strings.Contains(lower, "runtime error"):
		return reasonCategoryExecutionUnavailable
	default:
		return ""
	}
}

func isValidDecisionAction(action string) bool {
	normalized := strings.ToLower(strings.TrimSpace(action))
	return normalized == "buy" || normalized == "sell" || normalized == "hold"
}

func shouldDowngradeExecutionErrorToHold(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "futures-only mode prevented spot fallback") ||
		strings.Contains(msg, "parameter") && strings.Contains(msg, "does not exist") ||
		strings.Contains(msg, "context deadline exceeded") ||
		strings.Contains(msg, "request failed") ||
		strings.Contains(msg, "failed to get ticker") ||
		strings.Contains(msg, "symbol cooldown active") ||
		strings.Contains(msg, "symbol loss cooldown active") ||
		strings.Contains(msg, "symbol failure budget reached") ||
		strings.Contains(msg, "open position/order already exists")
}

func buildExecutionFallbackReason(err error) string {
	if err == nil {
		return "execution unavailable, held this cycle"
	}
	sanitized := strings.Join(strings.Fields(err.Error()), " ")
	if len(sanitized) > 220 {
		sanitized = sanitized[:217] + "..."
	}
	return fmt.Sprintf("execution unavailable, held this cycle: %s", sanitized)
}

func (s *AIScalpingService) repairDecisionJSON(ctx context.Context, raw string) (string, error) {
	modelID := strings.TrimSpace(s.config.Model)
	if modelID == "" {
		modelID = "glm-5"
	}

	maxTokens := 320
	if s.config.MaxTokens > 0 && s.config.MaxTokens < maxTokens {
		maxTokens = s.config.MaxTokens
	}

	repairCtx, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()

	req := &llm.CompletionRequest{
		Model: modelID,
		Messages: []llm.Message{
			{
				Role: llm.RoleSystem,
				Content: `Convert the provided trading analysis into strict JSON only.
Schema:
{
  "action": "buy" | "sell" | "hold",
  "symbol": "SYMBOL/USDT",
  "size_pct": number,
  "confidence": number,
  "reasoning": "text",
  "stop_loss": number|null,
  "take_profit": number|null
}
Do not include markdown or extra text.`,
			},
			{
				Role:    llm.RoleUser,
				Content: raw,
			},
		},
		Temperature:    floatPtr(0),
		MaxTokens:      maxTokens,
		ResponseFormat: &llm.ResponseFormat{Type: "json_object"},
	}

	resp, err := s.llmClient.Complete(repairCtx, req)
	if err != nil {
		return "", err
	}

	content := strings.TrimSpace(resp.Message.Content)
	if content == "" {
		return "", fmt.Errorf("empty repair response")
	}
	log.Printf("[AI-SCALPING] Repaired non-JSON decision payload")
	return content, nil
}

func (s *AIScalpingService) parseDecisionWithRetries(ctx context.Context, raw string) (*AITradingDecision, error) {
	decision, err := parseAIDecisionPayload(raw)
	if err == nil && isValidDecisionAction(decision.Action) {
		return decision, nil
	}
	if err == nil {
		err = fmt.Errorf("unsupported action: %s", strings.TrimSpace(decision.Action))
	}

	maxRetries := s.config.StructuredRetries
	if maxRetries <= 0 {
		maxRetries = 2
	}

	if repairedLocal, localErr := repairDecisionJSONLocally(raw); localErr == nil {
		localDecision, parseErr := parseAIDecisionPayload(repairedLocal)
		if parseErr == nil && isValidDecisionAction(localDecision.Action) {
			log.Printf("[AI-SCALPING] Structured-output recovered via deterministic local repair")
			return localDecision, nil
		}
	}

	lastErr := err
	current := raw
	for attempt := 1; attempt <= maxRetries; attempt++ {
		remaining, hasBudget := s.remainingRepairBudget(ctx)
		if !hasBudget {
			lastErr = fmt.Errorf("repair attempt %d skipped: remaining context budget %s below floor %s", attempt, remaining.Round(time.Millisecond), s.repairMinRemainingBudget())
			break
		}

		repaired, repairErr := s.repairDecisionJSON(ctx, current)
		if repairErr != nil {
			lastErr = fmt.Errorf("repair attempt %d failed: %w", attempt, repairErr)
			continue
		}
		decision, parseErr := parseAIDecisionPayload(repaired)
		if parseErr == nil && isValidDecisionAction(decision.Action) {
			log.Printf("[AI-SCALPING] Structured-output retry succeeded on attempt %d", attempt)
			return decision, nil
		}
		if parseErr == nil {
			parseErr = fmt.Errorf("unsupported action: %s", strings.TrimSpace(decision.Action))
		}
		lastErr = fmt.Errorf("repair attempt %d parse failed: %w", attempt, parseErr)
		current = repaired
	}

	return nil, lastErr
}

func (s *AIScalpingService) withInferenceBudget(ctx context.Context) (context.Context, context.CancelFunc) {
	deadline, ok := ctx.Deadline()
	if !ok {
		return ctx, func() {}
	}

	remaining := time.Until(deadline)
	reserve := s.repairMinRemainingBudget()
	// Keep a small tail buffer so outer context cancellation can propagate cleanly.
	minTail := 2 * time.Second
	if remaining <= reserve+minTail {
		return ctx, func() {}
	}

	inferenceBudget := remaining - reserve
	if inferenceBudget <= minTail {
		return ctx, func() {}
	}
	return context.WithTimeout(ctx, inferenceBudget)
}

func (s *AIScalpingService) repairMinRemainingBudget() time.Duration {
	seconds := getEnvInt("NEURATRADE_AI_REPAIR_MIN_REMAINING_SECONDS")
	if seconds <= 0 {
		seconds = 15
	}
	seconds = clampInt(seconds, 5, 180)
	return time.Duration(seconds) * time.Second
}

func (s *AIScalpingService) remainingRepairBudget(ctx context.Context) (time.Duration, bool) {
	deadline, ok := ctx.Deadline()
	if !ok {
		return 999 * time.Hour, true
	}
	remaining := time.Until(deadline)
	if remaining < 0 {
		remaining = 0
	}
	return remaining, remaining >= s.repairMinRemainingBudget()
}

func repairDecisionJSONLocally(raw string) (string, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", fmt.Errorf("empty model response")
	}

	cleaned := strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(trimmed, "```json"), "```"))
	if cleaned == "" {
		cleaned = trimmed
	}

	if candidate, ok := extractBalancedJSONObject(cleaned); ok {
		return candidate, nil
	}
	if candidate, ok := extractBalancedJSONObject(trimmed); ok {
		return candidate, nil
	}

	return "", fmt.Errorf("no JSON object candidate found in model response")
}

func extractBalancedJSONObject(raw string) (string, bool) {
	inString := false
	escapeNext := false
	depth := 0
	start := -1

	for idx, ch := range raw {
		if escapeNext {
			escapeNext = false
			continue
		}
		switch ch {
		case '\\':
			if inString {
				escapeNext = true
			}
		case '"':
			inString = !inString
		case '{':
			if inString {
				continue
			}
			if depth == 0 {
				start = idx
			}
			depth++
		case '}':
			if inString || depth == 0 {
				continue
			}
			depth--
			if depth == 0 && start >= 0 {
				return strings.TrimSpace(raw[start : idx+1]), true
			}
		}
	}

	return "", false
}

func parseOptionalDecimal(raw json.RawMessage) (*decimal.Decimal, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}

	// Handle numeric JSON values first.
	var numeric float64
	if err := json.Unmarshal(raw, &numeric); err == nil {
		dec := decimal.NewFromFloat(numeric)
		return &dec, nil
	}

	// Handle quoted values (including empty string placeholders).
	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		trimmed := strings.TrimSpace(text)
		if trimmed == "" {
			return nil, nil
		}
		dec, err := decimal.NewFromString(trimmed)
		if err != nil {
			return nil, err
		}
		return &dec, nil
	}

	return nil, fmt.Errorf("unsupported value %s", string(raw))
}

func defaultExitLevels(price float64, action string) (decimal.Decimal, decimal.Decimal) {
	entry := decimal.NewFromFloat(price)
	stopPct := decimal.NewFromFloat(0.008) // 0.8%
	takePct := decimal.NewFromFloat(0.012) // 1.2%

	switch strings.ToLower(strings.TrimSpace(action)) {
	case "sell":
		stopLoss := entry.Mul(decimal.NewFromInt(1).Add(stopPct))
		takeProfit := entry.Mul(decimal.NewFromInt(1).Sub(takePct))
		return stopLoss, takeProfit
	default:
		stopLoss := entry.Mul(decimal.NewFromInt(1).Sub(stopPct))
		takeProfit := entry.Mul(decimal.NewFromInt(1).Add(takePct))
		return stopLoss, takeProfit
	}
}

func (s *AIScalpingService) dynamicRiskThresholds(ctx context.Context, portfolio TradingPortfolio) (minConfidence float64, maxCapitalPct float64) {
	minConfidence = s.config.MinConfidence
	maxCapitalPct = s.config.MaxCapitalPct

	adjusted := GetScalpingPerformance().GetAdjustedParameters()
	if adjusted.MaxCapitalPercent > 0 && adjusted.MaxCapitalPercent < maxCapitalPct {
		maxCapitalPct = adjusted.MaxCapitalPercent
	}

	perf := GetScalpingPerformance().GetPerformance()
	consecutiveLosses := readIntMetric(perf["consecutive_losses"])
	if consecutiveLosses >= 2 {
		minConfidence += 0.05 * float64(consecutiveLosses-1)
	}
	if minConfidence > 0.95 {
		minConfidence = 0.95
	}
	if s.tradeMemory != nil {
		lookbackHours := getEnvInt("NEURATRADE_SCALPING_PERF_LOOKBACK_HOURS")
		if lookbackHours <= 0 {
			lookbackHours = 24 * 30
		}
		stats, err := s.tradeMemory.GetPerformanceStatsWindow(ctx, lookbackHours)
		if err == nil {
			decisiveTrades := stats.DecisiveTrades
			winRate := stats.DecisiveWinRatePct
			if decisiveTrades >= 10 && winRate > 0 && winRate < 35 {
				if minConfidence < 0.70 {
					minConfidence = 0.70
				}
				maxCapitalPct = maxCapitalPct * 0.6
			}
			if decisiveTrades >= 20 && winRate > 0 && winRate < 30 {
				if minConfidence < 0.78 {
					minConfidence = 0.78
				}
				maxCapitalPct = maxCapitalPct * 0.5
			}
		}

	}

	s.applyControlledNoFillRecovery(&minConfidence, &maxCapitalPct, portfolio, consecutiveLosses)
	switch strings.ToLower(strings.TrimSpace(portfolio.RecoveryMode)) {
	case "micro_entry":
		microCap := 0.50
		if value, ok := getEnvFloat("NEURATRADE_RECOVERY_MICRO_ENTRY_CAP_PCT"); ok && value > 0 {
			microCap = value
		}
		if maxCapitalPct > microCap {
			maxCapitalPct = microCap
		}
	case "derisk_only":
		if maxCapitalPct > 0.10 {
			maxCapitalPct = 0.10
		}
		if minConfidence < 0.85 {
			minConfidence = 0.85
		}
	}
	if maxCapitalPct < 0.1 {
		maxCapitalPct = 0.1
	}

	return minConfidence, maxCapitalPct
}

func (s *AIScalpingService) applyControlledNoFillRecovery(
	minConfidence *float64,
	maxCapitalPct *float64,
	portfolio TradingPortfolio,
	consecutiveLosses int,
) {
	if minConfidence == nil || maxCapitalPct == nil {
		return
	}
	if consecutiveLosses >= 2 {
		return
	}
	if portfolio.DriftActive || portfolio.OpenPositions > 0 {
		return
	}

	recoveryMinutes := getEnvInt("NEURATRADE_NOFILL_RECOVERY_MINUTES")
	if recoveryMinutes <= 0 {
		recoveryMinutes = getEnvInt("NEURATRADE_SCALPING_NO_FILL_RECOVERY_MINUTES")
	}
	if recoveryMinutes <= 0 {
		recoveryMinutes = 180
	}
	if portfolio.NoFillMinutes < float64(recoveryMinutes) {
		return
	}

	minFloor := 0.70
	if value, ok := getEnvFloat("NEURATRADE_NOFILL_MIN_CONF_FLOOR"); ok && value > 0 {
		minFloor = value
	}
	if minFloor < 0.50 {
		minFloor = 0.50
	}
	if minFloor > 0.90 {
		minFloor = 0.90
	}

	maxCapTarget := 1.50
	if value, ok := getEnvFloat("NEURATRADE_NOFILL_MAX_CAP_PCT_CAP"); ok && value > 0 {
		maxCapTarget = value
	}
	if maxCapTarget < 0.50 {
		maxCapTarget = 0.50
	}

	step := int(portfolio.NoFillMinutes / float64(recoveryMinutes))
	if step < 1 {
		return
	}

	switch {
	case step >= 2:
		if *minConfidence > minFloor {
			*minConfidence = minFloor
		}
	default:
		if *minConfidence > 0.75 {
			*minConfidence = 0.75
		}
	}

	switch {
	case step >= 3:
		if *maxCapitalPct < maxCapTarget {
			*maxCapitalPct = maxCapTarget
		}
	case step >= 2:
		if *maxCapitalPct < 1.00 {
			*maxCapitalPct = 1.00
		}
	default:
		if *maxCapitalPct < 0.50 {
			*maxCapitalPct = 0.50
		}
	}

	if *maxCapitalPct > maxCapTarget {
		*maxCapitalPct = maxCapTarget
	}
}

func readIntMetric(v interface{}) int {
	switch n := v.(type) {
	case int:
		return n
	case int64:
		return int(n)
	case float64:
		return int(n)
	default:
		return 0
	}
}

func readFloatMetric(v interface{}) float64 {
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

func (s *AIScalpingService) enforceSymbolGuard(symbol string) error {
	normalized := normalizeSymbolForComparison(symbol)
	if normalized == "" {
		return nil
	}
	now := time.Now().UTC()

	s.symbolGuardMu.Lock()
	defer s.symbolGuardMu.Unlock()

	state := s.symbolGuards[normalized]
	cooldown := s.config.SymbolCooldown
	if cooldown > 0 && !state.LastSuccess.IsZero() && now.Sub(state.LastSuccess) < cooldown {
		remaining := cooldown - now.Sub(state.LastSuccess)
		return fmt.Errorf("symbol cooldown active for %s (%s remaining)", normalized, remaining.Round(time.Second))
	}

	window := s.config.FailureWindow
	if window <= 0 {
		window = 15 * time.Minute
	}
	if !state.FailureWindowFrom.IsZero() && now.Sub(state.FailureWindowFrom) > window {
		state.FailureWindowFrom = time.Time{}
		state.FailureCount = 0
	}
	if s.config.FailureBudget > 0 && state.FailureCount >= s.config.FailureBudget {
		return fmt.Errorf("symbol failure budget reached for %s (%d failures in %s)", normalized, state.FailureCount, window.Round(time.Second))
	}
	lossCooldown := s.config.LossCooldown
	if lossCooldown <= 0 {
		lossCooldown = 20 * time.Minute
	}
	if s.config.LossStreakBudget > 0 && state.LossStreak >= s.config.LossStreakBudget && !state.LastLoss.IsZero() && now.Sub(state.LastLoss) < lossCooldown {
		return fmt.Errorf(
			"symbol loss cooldown active for %s (%d consecutive losses, %s remaining)",
			normalized,
			state.LossStreak,
			(lossCooldown - now.Sub(state.LastLoss)).Round(time.Second),
		)
	}

	return nil
}

func (s *AIScalpingService) recordSymbolGuardResult(symbol string, execErr error) {
	normalized := normalizeSymbolForComparison(symbol)
	if normalized == "" {
		return
	}
	now := time.Now().UTC()

	s.symbolGuardMu.Lock()
	defer s.symbolGuardMu.Unlock()

	state := s.symbolGuards[normalized]
	window := s.config.FailureWindow
	if window <= 0 {
		window = 15 * time.Minute
	}

	if execErr == nil {
		state.LastSuccess = now
		state.FailureWindowFrom = time.Time{}
		state.FailureCount = 0
		s.symbolGuards[normalized] = state
		return
	}

	if state.FailureWindowFrom.IsZero() || now.Sub(state.FailureWindowFrom) > window {
		state.FailureWindowFrom = now
		state.FailureCount = 1
	} else {
		state.FailureCount++
	}
	s.symbolGuards[normalized] = state
}

// ReportTradeOutcome feeds realized PnL back into symbol-level guardrails.
func (s *AIScalpingService) ReportTradeOutcome(symbol string, pnl decimal.Decimal) {
	normalized := normalizeSymbolForComparison(symbol)
	if normalized == "" {
		return
	}
	now := time.Now().UTC()
	window := s.config.LossWindow
	if window <= 0 {
		window = 90 * time.Minute
	}

	s.symbolGuardMu.Lock()
	defer s.symbolGuardMu.Unlock()

	state := s.symbolGuards[normalized]
	if pnl.LessThan(decimal.Zero) {
		if state.LossWindowFrom.IsZero() || now.Sub(state.LossWindowFrom) > window {
			state.LossWindowFrom = now
			state.LossStreak = 1
		} else {
			state.LossStreak++
		}
		state.LastLoss = now
	} else if pnl.GreaterThanOrEqual(decimal.Zero) {
		state.LossWindowFrom = time.Time{}
		state.LossStreak = 0
	}
	s.symbolGuards[normalized] = state
}

func getEnvInt(key string) int {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return 0
	}
	value, err := strconv.Atoi(raw)
	if err != nil {
		log.Printf("[AI-SCALPING] Invalid integer %s=%q", key, raw)
		return 0
	}
	return value
}

func getEnvFloat(key string) (float64, bool) {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return 0, false
	}
	value, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		log.Printf("[AI-SCALPING] Invalid float %s=%q", key, raw)
		return 0, false
	}
	return value, true
}

func getEnvBool(key string) (bool, bool) {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return false, false
	}
	switch strings.ToLower(raw) {
	case "1", "true", "yes", "on":
		return true, true
	case "0", "false", "no", "off":
		return false, true
	default:
		log.Printf("[AI-SCALPING] Invalid boolean %s=%q", key, raw)
		return false, false
	}
}

func clampInt(value, min, max int) int {
	if value < min {
		return min
	}
	if value > max {
		return max
	}
	return value
}

func clampFloat(value, min, max float64) float64 {
	if value < min {
		return min
	}
	if value > max {
		return max
	}
	return value
}
