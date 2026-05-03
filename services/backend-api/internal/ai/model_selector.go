package ai

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
)

var (
	ErrNoModelsConfigured = errors.New("no AI models configured")
	ErrModelNotFound      = errors.New("model not found in configured pool")
)

type ConfiguredModel struct {
	ProviderID      string  `json:"provider_id"`
	ModelID         string  `json:"model_id"`
	DisplayName     string  `json:"display_name"`
	APIKey          string  `json:"-"`
	BaseURL         string  `json:"base_url"`
	AuthMode        string  `json:"auth_mode"`
	CapabilityScore float64 `json:"capability_score"`
}

type ModelConfig struct {
	models      []ConfiguredModel
	routingMode RoutingMode
	mu          sync.RWMutex
}

type RoutingMode string

const (
	RoutingModePrimary    RoutingMode = "primary"
	RoutingModeFallback   RoutingMode = "fallback"
	RoutingModeRoundRobin RoutingMode = "round_robin"
)

func NewModelConfig() *ModelConfig {
	return &ModelConfig{
		models: make([]ConfiguredModel, 0),
	}
}

func NewModelConfigFromSingle(providerID, modelID, apiKey, baseURL string) *ModelConfig {
	mc := NewModelConfig()
	mc.models = []ConfiguredModel{
		{
			ProviderID: providerID,
			ModelID:    modelID,
			AuthMode:   "api_key",
			APIKey:     apiKey,
			BaseURL:    baseURL,
		},
	}
	return mc
}

func NewModelConfigFromList(models []ConfiguredModel) *ModelConfig {
	mc := NewModelConfig()
	mc.models = make([]ConfiguredModel, len(models))
	copy(mc.models, models)
	return mc
}

func (mc *ModelConfig) Models() []ConfiguredModel {
	mc.mu.RLock()
	defer mc.mu.RUnlock()

	result := make([]ConfiguredModel, len(mc.models))
	copy(result, mc.models)
	return result
}

func (mc *ModelConfig) PrimaryModel() (*ConfiguredModel, error) {
	mc.mu.RLock()
	defer mc.mu.RUnlock()

	if len(mc.models) == 0 {
		return nil, ErrNoModelsConfigured
	}
	m := mc.models[0]
	return &m, nil
}

func (mc *ModelConfig) AddModel(model ConfiguredModel) {
	mc.mu.Lock()
	defer mc.mu.Unlock()
	mc.models = append(mc.models, model)
}

func (mc *ModelConfig) RemoveModel(providerID, modelID string) bool {
	mc.mu.Lock()
	defer mc.mu.Unlock()

	for i, m := range mc.models {
		if m.ProviderID == providerID && m.ModelID == modelID {
			mc.models = append(mc.models[:i], mc.models[i+1:]...)
			return true
		}
	}
	return false
}

func (mc *ModelConfig) Count() int {
	mc.mu.RLock()
	defer mc.mu.RUnlock()
	return len(mc.models)
}

func (mc *ModelConfig) HasMultiple() bool {
	return mc.Count() > 1
}

func (mc *ModelConfig) SetModels(models []ConfiguredModel) {
	mc.mu.Lock()
	defer mc.mu.Unlock()
	mc.models = make([]ConfiguredModel, len(models))
	copy(mc.models, models)
}

func (mc *ModelConfig) SetRoutingMode(mode RoutingMode) {
	mc.mu.Lock()
	defer mc.mu.Unlock()
	mc.routingMode = mode
}

func (mc *ModelConfig) RoutingMode() RoutingMode {
	mc.mu.RLock()
	defer mc.mu.RUnlock()
	return mc.routingMode
}

func (mc *ModelConfig) FindModel(providerID, modelID string) (*ConfiguredModel, error) {
	mc.mu.RLock()
	defer mc.mu.RUnlock()

	for i := range mc.models {
		if mc.models[i].ProviderID == providerID && mc.models[i].ModelID == modelID {
			m := mc.models[i]
			return &m, nil
		}
	}
	return nil, ErrModelNotFound
}

func (mc *ModelConfig) RequiresFallback() bool {
	mc.mu.RLock()
	defer mc.mu.RUnlock()
	return mc.routingMode != RoutingModePrimary && len(mc.models) > 1
}

type ModelSelector struct {
	config    *ModelConfig
	ranking   *RankingService
	rrCounter uint64
}

func NewModelSelector(config *ModelConfig, ranking *RankingService) *ModelSelector {
	return &ModelSelector{
		config:  config,
		ranking: ranking,
	}
}

type SelectionConstraints struct {
	RequireTools     bool
	RequireReasoning bool
	RequireVision    bool
}

type SelectionResult struct {
	Model ConfiguredModel
	Mode  RoutingMode
	Rank  int
	Total int
}

func (ms *ModelSelector) Select(ctx context.Context, constraints SelectionConstraints) (*SelectionResult, error) {
	models := ms.config.Models()
	if len(models) == 0 {
		return nil, ErrNoModelsConfigured
	}

	if ms.config.RequiresFallback() {
		routingMode := ms.config.RoutingMode()
		switch routingMode {
		case RoutingModeRoundRobin:
			return ms.SelectRoundRobin(ctx, constraints)
		default:
			return ms.selectWithFallback(ctx, models, constraints)
		}
	}

	return &SelectionResult{
		Model: models[0],
		Mode:  RoutingModePrimary,
		Rank:  1,
		Total: len(models),
	}, nil
}

func (ms *ModelSelector) SelectFallbackChain(ctx context.Context, constraints SelectionConstraints) ([]ConfiguredModel, error) {
	models := ms.config.Models()
	if len(models) == 0 {
		return nil, ErrNoModelsConfigured
	}

	ranked, err := ms.ranking.RankModels(ctx, models)
	if err != nil {
		return models, nil
	}

	return ranked, nil
}

func (ms *ModelSelector) SelectRoundRobin(ctx context.Context, constraints SelectionConstraints) (*SelectionResult, error) {
	models := ms.config.Models()
	if len(models) == 0 {
		return nil, ErrNoModelsConfigured
	}

	idx := atomic.AddUint64(&ms.rrCounter, 1)
	// #nosec G115 -- modulo result always fits in int
	selectedIdx := int(uint(idx-1) % uint(len(models)))

	return &SelectionResult{
		Model: models[selectedIdx],
		Mode:  RoutingModeRoundRobin,
		Rank:  selectedIdx + 1,
		Total: len(models),
	}, nil
}

func (ms *ModelSelector) selectWithFallback(ctx context.Context, models []ConfiguredModel, constraints SelectionConstraints) (*SelectionResult, error) {
	ranked, err := ms.ranking.RankModels(ctx, models)
	if err != nil {
		return &SelectionResult{
			Model: models[0],
			Mode:  RoutingModeFallback,
			Rank:  1,
			Total: len(models),
		}, nil
	}

	for i, model := range ranked {
		if ms.matchesConstraints(model, constraints) {
			return &SelectionResult{
				Model: model,
				Mode:  RoutingModeFallback,
				Rank:  i + 1,
				Total: len(ranked),
			}, nil
		}
	}

	if len(ranked) > 0 {
		return &SelectionResult{
			Model: ranked[0],
			Mode:  RoutingModeFallback,
			Rank:  1,
			Total: len(ranked),
		}, nil
	}

	return nil, ErrNoModelsConfigured
}

// TODO: Implement constraint-based filtering (RequireTools, RequireReasoning, RequireVision)
// once model capability metadata is available from the registry.
func (ms *ModelSelector) matchesConstraints(model ConfiguredModel, constraints SelectionConstraints) bool {
	return true
}

func (ms *ModelSelector) ReselectNext(ctx context.Context, failedProvider, failedModel string, constraints SelectionConstraints) (*SelectionResult, error) {
	chain, err := ms.SelectFallbackChain(ctx, constraints)
	if err != nil {
		return nil, err
	}

	for i, model := range chain {
		if model.ProviderID == failedProvider && model.ModelID == failedModel {
			if i+1 < len(chain) {
				return &SelectionResult{
					Model: chain[i+1],
					Mode:  RoutingModeFallback,
					Rank:  i + 2,
					Total: len(chain),
				}, nil
			}
			return nil, fmt.Errorf("all models in fallback chain exhausted")
		}
	}

	return nil, ErrModelNotFound
}

func (ms *ModelSelector) RankingSummary(ctx context.Context) ([]ModelRanking, error) {
	rankings, err := ms.ranking.GetRankings(ctx)
	if err != nil {
		return nil, err
	}
	return rankings.Rankings, nil
}

func (ms *ModelSelector) ActiveModels() []ConfiguredModel {
	return ms.config.Models()
}

func (ms *ModelSelector) IsSingleModel() bool {
	return !ms.config.RequiresFallback()
}
