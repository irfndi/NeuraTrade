// Package ai provides AI provider registry and routing functionality.
// It integrates with models.dev API for unified model metadata management.
package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/shopspring/decimal"
	"go.uber.org/zap"
)

const (
	// ModelsDevAPIURL is the endpoint for models.dev API
	ModelsDevAPIURL = "https://models.dev/api.json"

	// CacheKey is the Redis key for caching model registry
	CacheKey = "ai:model_registry"

	// CacheTTL is the cache duration for model registry
	CacheTTL = 24 * time.Hour
)

// ModelCapability represents model capability flags
type ModelCapability struct {
	SupportsTools     bool `json:"supports_tools"`
	SupportsVision    bool `json:"supports_vision"`
	SupportsReasoning bool `json:"supports_reasoning"`
}

// ModelCost represents cost metadata for a model
type ModelCost struct {
	InputCost       decimal.Decimal `json:"input_cost"`
	OutputCost      decimal.Decimal `json:"output_cost"`
	ReasoningCost   decimal.Decimal `json:"reasoning_cost,omitempty"`
	CacheReadCost   decimal.Decimal `json:"cache_read_cost,omitempty"`
	CacheWriteCost  decimal.Decimal `json:"cache_write_cost,omitempty"`
	AudioInputCost  decimal.Decimal `json:"audio_input_cost,omitempty"`
	AudioOutputCost decimal.Decimal `json:"audio_output_cost,omitempty"`
}

// ModelLimits represents token limits for a model
type ModelLimits struct {
	ContextLimit int `json:"context_limit"`
	InputLimit   int `json:"input_limit"`
	OutputLimit  int `json:"output_limit"`
}

// ModelInfo represents a single model's metadata from models.dev
type ModelInfo struct {
	ProviderID       string          `json:"provider_id"`
	ProviderLabel    string          `json:"provider_label"`
	ModelID          string          `json:"model_id"`
	DisplayName      string          `json:"display_name"`
	Aliases          []string        `json:"aliases,omitempty"`
	Family           string          `json:"family,omitempty"`
	Capabilities     ModelCapability `json:"capabilities"`
	Cost             ModelCost       `json:"cost"`
	Limits           ModelLimits     `json:"limits"`
	Tier             string          `json:"tier"`
	LatencyClass     string          `json:"latency_class"`
	Status           string          `json:"status"`
	DefaultAllowed   bool            `json:"default_allowed"`
	RiskLevel        string          `json:"risk_level"`
	Temperature      bool            `json:"temperature"`
	StructuredOutput bool            `json:"structured_output"`
	Knowledge        string          `json:"knowledge,omitempty"`
	ReleaseDate      string          `json:"release_date,omitempty"`
	LastUpdated      string          `json:"last_updated"`
}

// ProviderInfo represents provider metadata
type ProviderInfo struct {
	ID      string      `json:"id"`
	Name    string      `json:"name"`
	NPM     string      `json:"npm,omitempty"`
	EnvVars []string    `json:"env_vars"`
	Models  []ModelInfo `json:"models"`
}

// ModelRegistry represents the full models.dev registry
type ModelRegistry struct {
	Providers []ProviderInfo `json:"providers"`
	Models    []ModelInfo    `json:"models"`
	FetchedAt time.Time      `json:"fetched_at"`
}

// Registry provides AI provider and model registry functionality
type Registry struct {
	client       *http.Client
	redis        *redis.Client
	logger       *zap.Logger
	modelsDevURL string
	cacheTTL     time.Duration

	mu         sync.RWMutex
	localCache *ModelRegistry
}

// RegistryOption configures the Registry
type RegistryOption func(*Registry)

// WithHTTPClient sets a custom HTTP client
func WithHTTPClient(client *http.Client) RegistryOption {
	return func(r *Registry) {
		r.client = client
	}
}

// WithRedis sets the Redis client for caching
func WithRedis(client *redis.Client) RegistryOption {
	return func(r *Registry) {
		r.redis = client
	}
}

// WithLogger sets the logger
func WithLogger(logger *zap.Logger) RegistryOption {
	return func(r *Registry) {
		r.logger = logger
	}
}

// WithCacheTTL sets the cache TTL
func WithCacheTTL(ttl time.Duration) RegistryOption {
	return func(r *Registry) {
		r.cacheTTL = ttl
	}
}

// NewRegistry creates a new AI provider registry
func NewRegistry(opts ...RegistryOption) *Registry {
	r := &Registry{
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
		modelsDevURL: ModelsDevAPIURL,
		cacheTTL:     CacheTTL,
	}

	for _, opt := range opts {
		opt(r)
	}

	if r.logger == nil {
		r.logger = zap.NewNop()
	}

	return r
}

// FetchModels fetches the model registry from models.dev API
func (r *Registry) FetchModels(ctx context.Context) (*ModelRegistry, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, r.modelsDevURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Accept", "application/json")

	// #nosec G704 - Intentional external API call to models.dev
	resp, err := r.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch models from models.dev: %w", err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("models.dev API returned status %d", resp.StatusCode)
	}

	var registry ModelRegistry
	if err := json.NewDecoder(resp.Body).Decode(&registry); err != nil {
		return nil, fmt.Errorf("failed to decode models.dev response: %w", err)
	}

	registry.FetchedAt = time.Now().UTC()

	// Update local cache
	r.mu.Lock()
	r.localCache = &registry
	r.mu.Unlock()

	// Cache to Redis if available
	if r.redis != nil {
		if err := r.cacheToRedis(ctx, &registry); err != nil {
			r.logger.Warn("Failed to cache models to Redis", zap.Error(err))
		}
	}

	return &registry, nil
}

// GetRegistry returns the current model registry, using cache if available
func (r *Registry) GetRegistry(ctx context.Context) (*ModelRegistry, error) {
	// Try local cache first
	r.mu.RLock()
	if r.localCache != nil && time.Since(r.localCache.FetchedAt) < r.cacheTTL {
		cache := r.localCache
		r.mu.RUnlock()
		return cache, nil
	}
	r.mu.RUnlock()

	// Try Redis cache
	if r.redis != nil {
		cached, err := r.getFromRedis(ctx)
		if err == nil && cached != nil {
			// Update local cache
			r.mu.Lock()
			r.localCache = cached
			r.mu.Unlock()
			return cached, nil
		}
	}

	// Fetch from API
	return r.FetchModels(ctx)
}

// cacheToRedis caches the registry to Redis
func (r *Registry) cacheToRedis(ctx context.Context, registry *ModelRegistry) error {
	data, err := json.Marshal(registry)
	if err != nil {
		return fmt.Errorf("failed to marshal registry: %w", err)
	}

	return r.redis.Set(ctx, CacheKey, data, r.cacheTTL).Err()
}

// getFromRedis retrieves the registry from Redis
func (r *Registry) getFromRedis(ctx context.Context) (*ModelRegistry, error) {
	data, err := r.redis.Get(ctx, CacheKey).Bytes()
	if err != nil {
		return nil, err
	}

	var registry ModelRegistry
	if err := json.Unmarshal(data, &registry); err != nil {
		return nil, fmt.Errorf("failed to unmarshal cached registry: %w", err)
	}

	return &registry, nil
}

// FindModel finds a model by ID across all providers
func (r *Registry) FindModel(ctx context.Context, modelID string) (*ModelInfo, error) {
	registry, err := r.GetRegistry(ctx)
	if err != nil {
		return nil, err
	}

	for _, model := range registry.Models {
		if model.ModelID == modelID {
			return &model, nil
		}
		// Check aliases
		for _, alias := range model.Aliases {
			if alias == modelID {
				return &model, nil
			}
		}
	}

	return nil, fmt.Errorf("model %s not found", modelID)
}

// GetModelsByProvider returns all models for a specific provider
func (r *Registry) GetModelsByProvider(ctx context.Context, providerID string) ([]ModelInfo, error) {
	registry, err := r.GetRegistry(ctx)
	if err != nil {
		return nil, err
	}

	var models []ModelInfo
	for _, model := range registry.Models {
		if model.ProviderID == providerID {
			models = append(models, model)
		}
	}

	if len(models) == 0 {
		return nil, fmt.Errorf("no models found for provider %s", providerID)
	}

	return models, nil
}

// FindModelsByCapability returns models that support specific capabilities
func (r *Registry) FindModelsByCapability(ctx context.Context, caps ModelCapability) ([]ModelInfo, error) {
	registry, err := r.GetRegistry(ctx)
	if err != nil {
		return nil, err
	}

	var models []ModelInfo
	for _, model := range registry.Models {
		if model.Status != "active" {
			continue
		}

		match := true
		if caps.SupportsTools && !model.Capabilities.SupportsTools {
			match = false
		}
		if caps.SupportsVision && !model.Capabilities.SupportsVision {
			match = false
		}
		if caps.SupportsReasoning && !model.Capabilities.SupportsReasoning {
			match = false
		}

		if match {
			models = append(models, model)
		}
	}

	return models, nil
}

// GetActiveProviders returns all active providers
func (r *Registry) GetActiveProviders(ctx context.Context) ([]ProviderInfo, error) {
	registry, err := r.GetRegistry(ctx)
	if err != nil {
		return nil, err
	}

	return registry.Providers, nil
}

// Refresh forces a refresh of the model registry from the API
func (r *Registry) Refresh(ctx context.Context) (*ModelRegistry, error) {
	// Clear caches
	r.mu.Lock()
	r.localCache = nil
	r.mu.Unlock()

	if r.redis != nil {
		if err := r.redis.Del(ctx, CacheKey).Err(); err != nil {
			r.logger.Warn("Failed to clear Redis cache", zap.Error(err))
		}
	}

	return r.FetchModels(ctx)
}
